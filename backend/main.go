package main

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/csv"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// --- HELPER FUNCTIONS ---

// maxPhotoBytes is the largest item photo accepted.
const maxPhotoBytes = 10 * 1024 * 1024

// allowedImageTypes maps the content types we accept, as sniffed from the
// file itself, to the extension the photo is stored under. Only raster formats
// that browsers render as plain images are allowed: an SVG is a document that
// can carry script, so it would be stored XSS on our own origin.
var allowedImageTypes = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/gif":  ".gif",
	"image/webp": ".webp",
	"image/bmp":  ".bmp",
}

// validateImageFile checks that the upload really is a supported image and
// returns the extension it should be stored with. The extension the client
// sent is ignored - only the file's content counts.
func validateImageFile(file *multipart.FileHeader) (string, error) {
	if file.Size > maxPhotoBytes {
		return "", fmt.Errorf("file size too large. Maximum allowed size is 10MB")
	}

	f, err := file.Open()
	if err != nil {
		return "", fmt.Errorf("could not read the uploaded file")
	}
	defer f.Close()

	head := make([]byte, 512)
	n, err := io.ReadFull(f, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return "", fmt.Errorf("could not read the uploaded file")
	}

	ext, ok := allowedImageTypes[http.DetectContentType(head[:n])]
	if !ok {
		return "", fmt.Errorf("unsupported file format. Allowed formats: JPG, PNG, WEBP, GIF, BMP")
	}
	return ext, nil
}

// --- REQUEST LIMITS ---

// Body size limits. The photo limit leaves room for the form fields and the
// multipart framing around a 10 MB image.
const (
	defaultBodyLimit = 12 * 1024 * 1024
	printerBodyLimit = maxUploadBytes + 1024*1024
)

// limitBody caps the request body at limit bytes. It has to run before
// anything parses the body, so an oversized upload is cut off as it arrives
// instead of being spooled to disk first.
func limitBody(limit int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.ContentLength > limit {
			c.AbortWithStatusJSON(http.StatusRequestEntityTooLarge, gin.H{"error": "Upload is too large"})
			return
		}
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, limit)
		c.Next()
	}
}

// bodyTooLarge reports whether err came from hitting the limitBody cap.
func bodyTooLarge(err error) bool {
	var maxErr *http.MaxBytesError
	return errors.As(err, &maxErr)
}

// photoHeaders stops an uploaded file from ever being treated as anything but
// an image, even if something slipped past validateImageFile.
func photoHeaders(c *gin.Context) {
	c.Header("X-Content-Type-Options", "nosniff")
	c.Header("Content-Security-Policy", "default-src 'none'; img-src 'self'; sandbox")
	c.Next()
}

// --- PUBLIC VALIDATION ---

// normalizePhone reduces a phone number to its digits, dropping a country
// code or trunk prefix so "+91 98765 43210" and "9876543210" compare equal.
func normalizePhone(phone string) string {
	var digits strings.Builder
	for _, r := range phone {
		if r >= '0' && r <= '9' {
			digits.WriteRune(r)
		}
	}
	d := digits.String()
	if len(d) > 10 {
		d = d[len(d)-10:]
	}
	return d
}

// phonesMatch compares two phone numbers by their digits. A number with no
// digits never matches anything, so an empty stored phone is not a free pass.
func phonesMatch(a, b string) bool {
	na, nb := normalizePhone(a), normalizePhone(b)
	return na != "" && subtle.ConstantTimeCompare([]byte(na), []byte(nb)) == 1
}

// errReturn* are the reasons a self-service return can be refused.
var (
	errReturnNotActive     = errors.New("only items that are currently borrowed can be returned here")
	errReturnAlreadyDone   = errors.New("item has already been returned")
	errReturnPhoneMismatch = errors.New("that phone number does not match the one this item was borrowed with")
)

// checkReturn decides whether the person returning a loan may do so: the loan
// must be active and they must know the phone number it was borrowed with.
// Missing items go through an admin, not the public page.
func checkReturn(loan Loan, phone string) error {
	switch loan.Status {
	case "active":
	case "returned":
		return errReturnAlreadyDone
	default:
		return errReturnNotActive
	}
	if !phonesMatch(loan.BorrowerPhone, phone) {
		return errReturnPhoneMismatch
	}
	return nil
}

// PublicLoan is what the public dashboard sees of a loan. It deliberately
// leaves out the borrower's phone number and purpose: the list is open to
// anyone, and the phone is what proves who may return the item.
type PublicLoan struct {
	ID                 uint       `json:"ID"`
	ItemID             *uint      `json:"item_id"`
	UserID             *uint      `json:"user_id"`
	Overdue            bool       `json:"overdue,omitempty"`
	DaysOverdue        int        `json:"days_overdue,omitempty"`
	CreatedAt          time.Time  `json:"CreatedAt"`
	BorrowerName       string     `json:"borrower_name"`
	ItemName           string     `json:"item_name"`
	LabLocation        string     `json:"lab_location"`
	QuantityBorrowed   int        `json:"quantity_borrowed"`
	ExpectedReturnDate string     `json:"expected_return_date"`
	PhotoFilename      string     `json:"photo_filename"`
	Status             string     `json:"status"`
	ReturnedAt         *time.Time `json:"returned_at"`
}

func toPublicLoans(loans []Loan) []PublicLoan {
	out := make([]PublicLoan, 0, len(loans))
	for _, l := range loans {
		out = append(out, PublicLoan{
			ID:                 l.ID,
			ItemID:             l.ItemID,
			UserID:             l.UserID,
			CreatedAt:          l.CreatedAt,
			BorrowerName:       l.BorrowerName,
			ItemName:           l.ItemName,
			LabLocation:        l.LabLocation,
			QuantityBorrowed:   l.QuantityBorrowed,
			ExpectedReturnDate: l.ExpectedReturnDate,
			PhotoFilename:      l.PhotoFilename,
			Status:             l.Status,
			ReturnedAt:         l.ReturnedAt,
		})
	}
	return out
}

// extendReturnDate pushes a loan's due date out. Return dates are whole days
// (an item is due at the end of its date), so an extension in hours rounds up
// to the next whole day rather than silently disappearing.
func extendReturnDate(current string, days, hours int) (string, error) {
	if days < 0 || hours < 0 || days*24+hours <= 0 {
		return "", errors.New("Extension must be a positive number of days or hours")
	}
	due, ok := parseReturnDate(current)
	if !ok {
		return "", errors.New("Invalid current return date format")
	}
	totalHours := days*24 + hours
	addDays := (totalHours + 23) / 24
	return due.AddDate(0, 0, addDays).Format("2006-01-02"), nil
}

// --- LOGIN RATE LIMITING ---

const (
	loginMaxFailures = 10
	loginWindow      = 15 * time.Minute
)

type loginAttempts struct {
	failures int
	first    time.Time
}

// loginLimiter counts failed logins per client IP in a fixed window.
type loginLimiter struct {
	mu        sync.Mutex
	attempts  map[string]*loginAttempts
	lastSweep time.Time
}

func newLoginLimiter() *loginLimiter {
	return &loginLimiter{attempts: make(map[string]*loginAttempts)}
}

// blocked reports whether ip has used up its failed attempts for now.
func (l *loginLimiter) blocked(ip string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	a, ok := l.attempts[ip]
	if !ok {
		return false
	}
	if now.Sub(a.first) > loginWindow {
		delete(l.attempts, ip)
		return false
	}
	return a.failures >= loginMaxFailures
}

// fail records a failed login from ip.
func (l *loginLimiter) fail(ip string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	// Opportunistically drop windows that have expired.
	if now.Sub(l.lastSweep) > loginWindow {
		for k, a := range l.attempts {
			if now.Sub(a.first) > loginWindow {
				delete(l.attempts, k)
			}
		}
		l.lastSweep = now
	}
	a, ok := l.attempts[ip]
	if !ok || now.Sub(a.first) > loginWindow {
		l.attempts[ip] = &loginAttempts{failures: 1, first: now}
		return
	}
	a.failures++
}

// succeed clears the failure count for ip.
func (l *loginLimiter) succeed(ip string) {
	l.mu.Lock()
	delete(l.attempts, ip)
	l.mu.Unlock()
}

// dummyPasswordHash is compared against when a login names an unknown user, so
// that a wrong username takes as long to reject as a wrong password.
var dummyPasswordHash, _ = bcrypt.GenerateFromPassword([]byte("not-a-real-password"), bcrypt.DefaultCost)

// --- PASSWORD HASHING ---

// hashPassword returns a bcrypt hash of the given plaintext password.
func hashPassword(plain string) (string, error) {
	h, err := bcrypt.GenerateFromPassword([]byte(plain), bcrypt.DefaultCost)
	return string(h), err
}

// legacySHA256 reproduces the old (insecure) password hashing so existing
// accounts can still log in once and be transparently upgraded to bcrypt.
func legacySHA256(plain string) string {
	hasher := sha256.New()
	hasher.Write([]byte(plain))
	return hex.EncodeToString(hasher.Sum(nil))
}

// verifyPassword checks a plaintext password against a stored hash.
// The second return value is true when the stored hash is a legacy SHA-256
// hash and should be re-hashed with bcrypt.
func verifyPassword(stored, plain string) (ok bool, needsUpgrade bool) {
	if strings.HasPrefix(stored, "$2") {
		return bcrypt.CompareHashAndPassword([]byte(stored), []byte(plain)) == nil, false
	}
	match := subtle.ConstantTimeCompare([]byte(stored), []byte(legacySHA256(plain))) == 1
	return match, match
}

// bearerToken extracts the token from an "Authorization: Bearer <token>" header.
func bearerToken(c *gin.Context) string {
	header := c.GetHeader("Authorization")
	if len(header) > 7 && strings.EqualFold(header[:7], "Bearer ") {
		return strings.TrimSpace(header[7:])
	}
	return ""
}

// --- DATABASE MODELS ---

type Item struct {
	gorm.Model
	Name            string     `json:"name"`
	NameKey         string     `json:"-" gorm:"index"` // itemKey(Name), for matching names
	HomeLab         string     `json:"home_lab"`
	TotalQuantity   int        `json:"total_quantity"`   // 0 when nobody has counted
	QuantityOnHand  int        `json:"quantity_on_hand"` // unused; availability comes from open loans
	Notes           string     `json:"notes"`            // e.g. "comes with a USB-C cable and a tripod plate"
	Issue           string     `json:"issue"`            // a problem someone reported, until cleared
	IssueReportedBy string     `json:"issue_reported_by"`
	LabOnly         bool       `json:"lab_only"`       // a warning, not enforced
	NeedsApproval   bool       `json:"needs_approval"` // loans start pending
	Tracked         bool       `json:"tracked"`        // valuable: custodian confirms it monthly
	CustodianID     *uint      `json:"custodian_id"`
	LastVerifiedAt  *time.Time `json:"last_verified_at"`
	VerifyAskedAt   *time.Time `json:"-"`
}

// Admin is the pre-accounts admin table, read only by setupAccounts to carry
// old admins over as Server Admin users.
type Admin struct {
	gorm.Model
	Username     string `json:"username" gorm:"unique"`
	Password     string `json:"-"` // Don't include in JSON responses
	Name         string `json:"name"`
	IsSuperAdmin bool   `json:"is_super_admin" gorm:"default:false"`
}

type Loan struct {
	gorm.Model
	ItemID               *uint      `json:"item_id" gorm:"index"` // nil for loans made before the catalogue
	UserID               *uint      `json:"user_id" gorm:"index"` // nil for loans made before accounts
	AcceptedAt           *time.Time `json:"accepted_at"`          // took responsibility for a tracked item
	DueReminderSent      bool       `json:"-"`
	OverdueReminderSent  bool       `json:"-"`
	BorrowerName         string     `json:"borrower_name"`
	BorrowerPhone        string     `json:"borrower_phone"`
	ItemName             string     `json:"item_name"`
	LabLocation          string     `json:"lab_location"`
	QuantityBorrowed     int        `json:"quantity_borrowed"`
	ExpectedReturnDate   string     `json:"expected_return_date"`
	Purpose              string     `json:"purpose"`
	PhotoFilename        string     `json:"photo_filename"`
	Status               string     `json:"status" gorm:"default:'active'"`            // active, returned, not_found
	ApprovalStatus       string     `json:"approval_status" gorm:"default:'approved'"` // kept for historical records; borrowing no longer needs approval
	ApprovedBy           string     `json:"approved_by"`                               // admin who last acted on the loan (marked missing/found)
	ApprovedAt           *time.Time `json:"approved_at"`
	DeniedAt             *time.Time `json:"denied_at"` // legacy, kept so old records stay readable
	ReturnRequested      bool       `json:"return_requested" gorm:"default:false"`
	ReturnApprovalStatus string     `json:"return_approval_status" gorm:"default:'not_requested'"` // not_requested, approved, not_found
	ReturnRequestedAt    *time.Time `json:"return_requested_at"`
	ReturnedAt           *time.Time `json:"returned_at"`
}

// Booking is a reservation of the Motion Capture Lab. No approval needed -
// a slot is either free or taken.
type Booking struct {
	gorm.Model
	ResourceID       uint       `json:"resource_id" gorm:"index"`
	UserID           *uint      `json:"user_id" gorm:"index"` // nil for bookings made before accounts
	ThreadID         *uint      `json:"thread_id" gorm:"index"`
	ThreadName       string     `json:"thread_name"` // kept as booked, for history
	BookedBy         string     `json:"booked_by"`
	Phone            string     `json:"phone"`
	Purpose          string     `json:"purpose"`
	StartTime        time.Time  `json:"start_time"`
	EndTime          time.Time  `json:"end_time"`
	Status           string     `json:"status" gorm:"default:'approved'"` // pending, approved, rejected
	ReviewedBy       string     `json:"reviewed_by"`
	ReviewedAt       *time.Time `json:"reviewed_at"`
	NoShow           bool       `json:"no_show"`
	NoShowReportedBy string     `json:"no_show_reported_by"`
}

// parseReturnDate reads the expected return date. It is stored as a plain
// date ("2026-08-20"), though older rows may carry a full timestamp.
//
// Plain dates are interpreted in the server's local timezone, so "due on the
// 20th" means the end of the 20th here in the lab - not in UTC. Set TZ in the
// environment (docker-compose defaults it to Asia/Kolkata); otherwise the
// browser and the backend disagree about the date for a few hours each night.
func parseReturnDate(value string) (time.Time, bool) {
	for _, layout := range []string{"2006-01-02", time.RFC3339} {
		if parsed, err := time.ParseInLocation(layout, value, time.Local); err == nil {
			return parsed, true
		}
	}
	return time.Time{}, false
}

// loanOverdue reports whether a loan is late, and by how many days. An item is
// due at the END of its return date, so something due today is not yet late.
// A returned item is judged on when it actually came back.
func loanOverdue(loan Loan, now time.Time) (bool, int) {
	due, ok := parseReturnDate(loan.ExpectedReturnDate)
	if !ok {
		return false, 0
	}
	deadline := due.AddDate(0, 0, 1)

	compareAt := now
	if loan.Status == "returned" {
		if loan.ReturnedAt == nil {
			return false, 0
		}
		compareAt = *loan.ReturnedAt
	}

	if !compareAt.After(deadline) {
		return false, 0
	}
	return true, int(compareAt.Sub(deadline).Hours() / 24)
}

// Helper function to format time pointers for CSV
func formatTimePtr(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format("2006-01-02 15:04:05")
}

// --- MAIN APPLICATION ---

func main() {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		log.Fatal("DATABASE_URL environment variable not set")
	}

	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		log.Fatalf("failed to connect to database: %v", err)
	}

	log.Println("Running database migrations...")
	if err := db.AutoMigrate(&Item{}, &Loan{}, &Admin{}, &Booking{}, &PrinterCredential{}, &PrintJob{}, &PrintUpload{},
		&User{}, &Group{}, &GroupPermission{}, &UserPermission{}, &Session{},
		&Resource{}, &Notification{}, &Thread{}, &ThreadMember{}, &ThreadResource{}); err != nil {
		log.Fatalf("database migration failed: %v", err)
	}

	// Approvals were removed. Bring records created under the old flow into the
	// new states so nothing is stranded in a status the app no longer uses.
	if res := db.Model(&Loan{}).
		Where("status IN ?", []string{"approved", "borrowed"}).
		Updates(map[string]interface{}{"status": "active", "approval_status": "approved"}); res.RowsAffected > 0 {
		log.Printf("Migrated %d loans from the old approval flow to 'active'", res.RowsAffected)
	}
	// Returns that were awaiting approval are treated as returned.
	if res := db.Model(&Loan{}).
		Where("return_requested = ? AND return_approval_status = ?", true, "pending").
		Updates(map[string]interface{}{
			"status":                 "returned",
			"return_approval_status": "approved",
			"returned_at":            time.Now(),
		}); res.RowsAffected > 0 {
		log.Printf("Migrated %d pending return requests to 'returned'", res.RowsAffected)
	}
	log.Println("Migrations complete.")

	// Create uploads directory if it doesn't exist
	if err := os.MkdirAll("./uploads", 0755); err != nil {
		log.Printf("Warning: Could not create uploads directory: %v", err)
	}

	if err := setupAccounts(db); err != nil {
		log.Fatalf("failed to set up accounts: %v", err)
	}
	if err := setupResources(db); err != nil {
		log.Fatalf("failed to set up bookable resources: %v", err)
	}
	if err := setupInventory(db); err != nil {
		log.Fatalf("failed to set up inventory: %v", err)
	}
	go runReminders(db)

	// Connect to the lab's 3D printers, if any are configured
	printers := loadPrinterManager(db)
	router := newRouter(db, printers, newSessionStore(db), newLoginLimiter())

	// --- START SERVER ---
	// Only the header read is timed: a whole-request or write timeout would
	// cut off large printer uploads and long-running camera streams.
	server := &http.Server{
		Addr:              ":8080",
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	log.Println("Starting server on port 8080...")
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("server stopped: %v", err)
	}
}

// newRouter builds every API route. Kept out of main so a test can build it
// without a database: gin panics on conflicting paths at registration.
func newRouter(db *gorm.DB, printers *PrinterManager, sessions *sessionStore, loginLimits *loginLimiter) *gin.Engine {
	requireLogin := authMiddleware(db, sessions)

	router := gin.Default()

	// CORS. Set ALLOWED_ORIGINS (comma separated) to restrict which sites may
	// call the API from a browser; defaults to allowing any origin so that the
	// "login via IP" fallback keeps working on the lab network.
	allowedOrigins := map[string]bool{}
	for _, o := range strings.Split(os.Getenv("ALLOWED_ORIGINS"), ",") {
		if o = strings.TrimSpace(o); o != "" {
			allowedOrigins[o] = true
		}
	}

	router.Use(func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if len(allowedOrigins) == 0 {
			c.Header("Access-Control-Allow-Origin", "*")
		} else if allowedOrigins[origin] {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Vary", "Origin")
		}
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Origin, Content-Type, Authorization")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	})

	// --- API ROUTES ---
	api := router.Group("/api")
	{
		// Serve uploaded photos, locked down so a file can only ever be an image
		photos := api.Group("/photos", photoHeaders)
		photos.Static("/", "./uploads")

		// Get a list of all active loans (for the dashboard). This is public, so
		// it returns PublicLoan - no phone numbers.
		api.GET("/loans/active", func(c *gin.Context) {
			var loans []Loan
			// Include:
			// 1. Items currently borrowed (status = 'active')
			// 2. Items marked as missing by an admin (status = 'not_found')
			// 3. Recently returned items (status = 'returned' AND returned_at within last 24 hours)
			if err := db.Where(`
				status = ? OR
				status = ? OR
				(status = ? AND returned_at > ?)
			`, "active", "not_found", "returned", time.Now().Add(-24*time.Hour)).
				Order(`
					CASE
						WHEN status = 'not_found' THEN 0
						WHEN status = 'returned' THEN 1
						ELSE 2
					END, created_at DESC
				`).
				Find(&loans).Error; err != nil {
				c.JSON(500, gin.H{"error": "Failed to retrieve active loans"})
				return
			}
			c.JSON(200, toPublicLoans(loans))
		})

		// --- 3D PRINTERS (read-only status and camera) ---

		// Live status of every configured printer
		api.GET("/printers", func(c *gin.Context) {
			c.JSON(200, printers.Statuses())
		})

		// Latest camera frame as a single JPEG
		api.GET("/printers/:id/snapshot", func(c *gin.Context) {
			frame, ok := printers.Frame(c.Param("id"))
			if !ok {
				c.JSON(404, gin.H{"error": "No camera image available"})
				return
			}
			c.Header("Cache-Control", "no-store")
			c.Data(200, "image/jpeg", frame)
		})

		// Send a sliced file to a printer. Anyone with upload_prints can do
		// this, because avoiding a wifi switch is the whole point - but it only
		// *uploads*. Starting the print still needs somebody at the machine
		// who can see the plate is clear.
		api.POST("/printers/:id/files", limitBody(printerBodyLimit), requireLogin, requirePerm(PermUploadPrints), func(c *gin.Context) {
			file, err := c.FormFile("file")
			if err != nil {
				if bodyTooLarge(err) {
					c.JSON(413, gin.H{"error": fmt.Sprintf(
						"That file is too large. The limit is %d MB.", maxUploadBytes/(1024*1024))})
					return
				}
				c.JSON(400, gin.H{"error": "Choose a sliced file to send"})
				return
			}

			if file.Size > maxUploadBytes {
				c.JSON(400, gin.H{"error": fmt.Sprintf(
					"That file is %d MB. The limit is %d MB.",
					file.Size/(1024*1024), maxUploadBytes/(1024*1024))})
				return
			}

			opened, err := file.Open()
			if err != nil {
				c.JSON(400, gin.H{"error": "Could not read the uploaded file"})
				return
			}
			defer opened.Close()

			name, err := printers.UploadFile(c.Param("id"), file.Filename, opened, file.Size)
			if err != nil {
				c.JSON(400, gin.H{"error": err.Error()})
				return
			}

			log.Printf("printer %s: received upload %s", c.Param("id"), name)
			// So the sender hears how the print goes (see print_notify.go)
			recordPrintUpload(db, c.Param("id"), name, currentUser(c).ID)

			c.JSON(200, gin.H{
				"message": fmt.Sprintf(
					"%s sent. Start it from the printer's screen - you'll get a notification when it finishes.", name),
				"file_name": name,
			})
		})

		// What is already on the printer, so people can confirm their file
		// arrived and find it on the screen
		api.GET("/printers/:id/files", func(c *gin.Context) {
			files, err := printers.ListFiles(c.Param("id"))
			if err != nil {
				c.JSON(400, gin.H{"error": err.Error()})
				return
			}
			c.JSON(200, files)
		})

		// --- ADMIN ROUTES ---
		registerAccountRoutes(api, db, sessions, loginLimits, requireLogin)
		registerBookingRoutes(api, db, requireLogin)
		registerInventoryRoutes(api, db, sessions, requireLogin)
		registerThreadRoutes(api, db, requireLogin)
		registerDashboardRoutes(api, db, requireLogin)

		admin := api.Group("/admin")
		{
			// Every route below needs a logged-in user; each one also checks
			// the permission for its area.
			admin.Use(requireLogin)
			manageInventory := requirePerm(PermManageInventory)
			managePrinters := requirePerm(PermManagePrinters)
			manageBookings := requirePerm(PermManageBookings)

			// Get loans by lab with status filtering and smart ordering
			admin.GET("/loans/by-lab/:lab", manageInventory, func(c *gin.Context) {
				lab := c.Param("lab")
				statusFilter := c.DefaultQuery("status", "all")

				var loans []Loan
				query := db.Where("lab_location = ?", lab)

				// Apply status filter
				if statusFilter == "borrowed" {
					query = query.Where("status = ?", "active")
				} else if statusFilter == "returned" {
					// Show returned items from the last 2 weeks only
					twoWeeksAgo := time.Now().AddDate(0, 0, -14)
					query = query.Where("status = ? AND updated_at > ?", "returned", twoWeeksAgo)
				} else if statusFilter == "not_found" {
					query = query.Where("status = ?", "not_found")
				} else {
					// For "all" status, exclude returned items older than 2 weeks
					twoWeeksAgo := time.Now().AddDate(0, 0, -14)
					query = query.Where(`
						(status != 'returned') OR 
						(status = 'returned' AND updated_at > ?)
					`, twoWeeksAgo)
				}

				// Smart ordering:
				// 1. Overdue items first (red background)
				// 2. Active loans by return date
				// 3. Rejected items at bottom
				//
				// "Today" is worked out here, in the app's timezone, rather than
				// with CURRENT_DATE, which follows the database's timezone.
				today := time.Now().In(time.Local).Format("2006-01-02")
				orderClause := clause.OrderBy{Expression: clause.Expr{SQL: `
					CASE
						WHEN status = 'not_found' THEN 3
						WHEN status = 'active' AND expected_return_date::date < ?::date THEN 1
						ELSE 2
					END ASC,
					expected_return_date ASC
				`, Vars: []interface{}{today}}}

				if err := query.Order(orderClause).Find(&loans).Error; err != nil {
					c.JSON(500, gin.H{"error": "Failed to retrieve loans"})
					return
				}
				c.JSON(200, loans)
			})

			// Extend loan return date
			admin.POST("/loans/:id/extend", manageInventory, func(c *gin.Context) {
				loanID := c.Param("id")

				// The acting admin comes from the session; the page still sends
				// admin_name, which is ignored.
				type ExtendRequest struct {
					ExtendDays  int `json:"extend_days"`
					ExtendHours int `json:"extend_hours"`
				}

				var req ExtendRequest
				if err := c.ShouldBindJSON(&req); err != nil {
					c.JSON(400, gin.H{"error": "Invalid extend data"})
					return
				}

				var loan Loan
				if err := db.First(&loan, "id = ?", loanID).Error; err != nil {
					c.JSON(404, gin.H{"error": "Loan not found"})
					return
				}

				newDate, err := extendReturnDate(loan.ExpectedReturnDate, req.ExtendDays, req.ExtendHours)
				if err != nil {
					c.JSON(400, gin.H{"error": err.Error()})
					return
				}

				loan.ExpectedReturnDate = newDate
				// A new date gets its own reminders.
				loan.DueReminderSent, loan.OverdueReminderSent = false, false
				if err := db.Save(&loan).Error; err != nil {
					c.JSON(500, gin.H{"error": "Failed to extend loan"})
					return
				}

				c.JSON(200, gin.H{"message": "Loan extended successfully"})
			})

			// Mark an item as missing - the admin cannot find it in the lab
			admin.POST("/loans/:id/mark-missing", manageInventory, func(c *gin.Context) {
				loanID := c.Param("id")

				var loan Loan
				if err := db.First(&loan, "id = ?", loanID).Error; err != nil {
					c.JSON(404, gin.H{"error": "Loan not found"})
					return
				}

				if loan.Status == "not_found" {
					c.JSON(400, gin.H{"error": "Item is already marked as missing"})
					return
				}
				if loan.Status != "active" {
					c.JSON(400, gin.H{"error": "Only borrowed items can be marked as missing"})
					return
				}

				now := time.Now()
				loan.Status = "not_found"
				loan.ReturnApprovalStatus = "not_found"
				loan.ApprovedBy = currentUser(c).Name
				loan.ApprovedAt = &now

				if err := db.Save(&loan).Error; err != nil {
					c.JSON(500, gin.H{"error": "Failed to mark item as missing"})
					return
				}

				c.JSON(200, gin.H{"message": "Item marked as missing"})
			})

			// Mark item as found (restore from not_found status)
			admin.POST("/loans/:id/mark-found", manageInventory, func(c *gin.Context) {
				loanID := c.Param("id")

				var loan Loan
				if err := db.First(&loan, "id = ?", loanID).Error; err != nil {
					c.JSON(404, gin.H{"error": "Loan not found"})
					return
				}

				// Check if the item is currently marked as not found
				if loan.Status != "not_found" {
					c.JSON(400, gin.H{"error": fmt.Sprintf("Item is not marked as missing. Current status: %s", loan.Status)})
					return
				}

				// Restore the item to borrowed status
				now := time.Now()
				loan.Status = "active"
				loan.ReturnRequested = false
				loan.ReturnApprovalStatus = "not_requested"
				loan.ApprovedBy = currentUser(c).Name
				loan.ApprovedAt = &now

				if err := db.Save(&loan).Error; err != nil {
					c.JSON(500, gin.H{"error": "Failed to mark item as found"})
					return
				}

				c.JSON(200, gin.H{"message": "Item successfully marked as found and restored to borrowed status"})
			})

			// Get missing items
			admin.GET("/loans/lost-missing", manageInventory, func(c *gin.Context) {
				var loans []Loan
				if err := db.Where("status = ?", "not_found").Order("updated_at DESC").Find(&loans).Error; err != nil {
					c.JSON(500, gin.H{"error": "Failed to retrieve missing items"})
					return
				}
				c.JSON(200, loans)
			})

			// Stop the current print job. Admins only - a stray click here
			// destroys someone's work, so it is deliberately not public.
			admin.POST("/printers/:id/stop", managePrinters, func(c *gin.Context) {
				adminName := currentUser(c).Name
				if err := printers.Stop(c.Param("id"), adminName); err != nil {
					c.JSON(400, gin.H{"error": err.Error()})
					return
				}
				c.JSON(200, gin.H{"message": "Stop command sent to the printer"})
			})

			// Start a print from a file already on the printer.
			//
			// This is the one command that makes a machine move on its own, so
			// it is admin-only and the printer must be idle. Whoever presses it
			// is responsible for the plate being clear - the camera on this page
			// is there to be looked at first.
			admin.POST("/printers/:id/print", managePrinters, func(c *gin.Context) {
				var req StartRequest
				if err := c.ShouldBindJSON(&req); err != nil {
					c.JSON(400, gin.H{"error": "Invalid print request"})
					return
				}

				adminName := currentUser(c).Name
				if err := printers.StartPrint(c.Param("id"), req, adminName); err != nil {
					c.JSON(400, gin.H{"error": err.Error()})
					return
				}
				c.JSON(200, gin.H{"message": "Print started"})
			})

			// Tidy up old plates - deleting other people's files is an
			// admin job, uploading is not.
			admin.DELETE("/printers/:id/files/:name", managePrinters, func(c *gin.Context) {
				if err := printers.DeleteFile(c.Param("id"), c.Param("name")); err != nil {
					c.JSON(400, gin.H{"error": err.Error()})
					return
				}
				c.JSON(200, gin.H{"message": "File deleted from the printer"})
			})

			// Clear the whole card, for when a term's worth of plates has
			// filled it up. Refuses while a job is running.
			admin.DELETE("/printers/:id/files", managePrinters, func(c *gin.Context) {
				deleted, err := printers.DeleteAllFiles(c.Param("id"), currentUser(c).Name)
				if err != nil {
					c.JSON(400, gin.H{"error": err.Error(), "deleted": deleted})
					return
				}
				c.JSON(200, gin.H{
					"message": fmt.Sprintf("Cleared %d file(s) from the printer", deleted),
					"deleted": deleted,
				})
			})

			// Pause the current job - reversible, unlike stop
			admin.POST("/printers/:id/pause", managePrinters, func(c *gin.Context) {
				if err := printers.Pause(c.Param("id"), currentUser(c).Name); err != nil {
					c.JSON(400, gin.H{"error": err.Error()})
					return
				}
				c.JSON(200, gin.H{"message": "Pause command sent to the printer"})
			})

			// Resume a paused job
			admin.POST("/printers/:id/resume", managePrinters, func(c *gin.Context) {
				if err := printers.Resume(c.Param("id"), currentUser(c).Name); err != nil {
					c.JSON(400, gin.H{"error": err.Error()})
					return
				}
				c.JSON(200, gin.H{"message": "Resume command sent to the printer"})
			})

			// Chamber light. Harmless in itself, but it commands hardware, so
			// it sits behind the same admin gate as the rest.
			admin.POST("/printers/:id/light", managePrinters, func(c *gin.Context) {
				type LightRequest struct {
					On *bool `json:"on" binding:"required"`
				}

				var req LightRequest
				if err := c.ShouldBindJSON(&req); err != nil {
					c.JSON(400, gin.H{"error": "Specify whether the light should be on"})
					return
				}

				if err := printers.SetLight(c.Param("id"), *req.On); err != nil {
					c.JSON(400, gin.H{"error": err.Error()})
					return
				}

				state := "off"
				if *req.On {
					state = "on"
				}
				c.JSON(200, gin.H{"message": "Chamber light turned " + state})
			})

			// The automatic print log
			admin.GET("/print-jobs", managePrinters, func(c *gin.Context) {
				var jobs []PrintJob
				query := db.Order("started_at DESC").Limit(200)
				if id := c.Query("printer_id"); id != "" {
					query = query.Where("printer_id = ?", id)
				}
				if err := query.Find(&jobs).Error; err != nil {
					c.JSON(500, gin.H{"error": "Failed to retrieve print jobs"})
					return
				}
				c.JSON(200, jobs)
			})

			// Print log as CSV
			admin.GET("/export-print-jobs-csv", managePrinters, func(c *gin.Context) {
				var jobs []PrintJob
				if err := db.Order("started_at DESC").Find(&jobs).Error; err != nil {
					c.JSON(500, gin.H{"error": "Failed to retrieve print jobs"})
					return
				}

				c.Header("Content-Type", "text/csv")
				c.Header("Content-Disposition", "attachment; filename=print_jobs.csv")

				writer := csv.NewWriter(c.Writer)
				defer writer.Flush()
				writer.Write([]string{"ID", "Printer", "File", "Started", "Ended",
					"Minutes", "Result", "Stopped By", "Last Percent"})

				for _, job := range jobs {
					minutes := ""
					ended := ""
					if job.EndedAt != nil {
						ended = job.EndedAt.Local().Format("2006-01-02 15:04:05")
						minutes = strconv.Itoa(int(job.EndedAt.Sub(job.StartedAt).Minutes()))
					}
					writer.Write([]string{
						strconv.Itoa(int(job.ID)),
						job.PrinterName,
						job.FileName,
						job.StartedAt.Local().Format("2006-01-02 15:04:05"),
						ended,
						minutes,
						job.Result,
						job.StoppedBy,
						strconv.Itoa(job.LastPercent),
					})
				}
			})

			// Update a printer's access code. Printers regenerate their code
			// when LAN mode is toggled, and this avoids editing .env and
			// restarting the site to recover.
			admin.PUT("/printers/:id/access-code", managePrinters, func(c *gin.Context) {
				type AccessCodeRequest struct {
					AccessCode string `json:"access_code" binding:"required"`
				}

				var req AccessCodeRequest
				if err := c.ShouldBindJSON(&req); err != nil {
					c.JSON(400, gin.H{"error": "An access code is required"})
					return
				}

				if err := printers.UpdateAccessCode(c.Param("id"), req.AccessCode); err != nil {
					c.JSON(400, gin.H{"error": err.Error()})
					return
				}

				c.JSON(200, gin.H{
					"message": "Access code updated. Reconnecting to the printer...",
				})
			})

			// Bookings with contact details, for the admin calendar. Same range
			// rules as the public list.
			admin.GET("/bookings", manageBookings, func(c *gin.Context) {
				from, to := parseRange(c)
				var bookings []Booking
				if err := bookingQuery(db, c).Where("status <> ? AND start_time < ? AND end_time > ?", BookingRejected, to, from).
					Order("start_time ASC").Find(&bookings).Error; err != nil {
					c.JSON(500, gin.H{"error": "Failed to retrieve bookings"})
					return
				}
				c.JSON(200, bookings)
			})

			// Delete any Motion Capture Lab booking
			admin.DELETE("/bookings/:id", manageBookings, func(c *gin.Context) {
				var booking Booking
				if err := db.First(&booking, "id = ?", c.Param("id")).Error; err != nil {
					c.JSON(404, gin.H{"error": "Booking not found"})
					return
				}

				if err := db.Delete(&booking).Error; err != nil {
					c.JSON(500, gin.H{"error": "Failed to delete booking"})
					return
				}

				c.JSON(200, gin.H{"message": "Booking deleted"})
			})

			// Get archived (old returned) items - older than 2 weeks
			admin.GET("/loans/archived", manageInventory, func(c *gin.Context) {
				twoWeeksAgo := time.Now().AddDate(0, 0, -14)
				var loans []Loan
				if err := db.Where("status = ? AND updated_at <= ?", "returned", twoWeeksAgo).Order("updated_at DESC").Find(&loans).Error; err != nil {
					c.JSON(500, gin.H{"error": "Failed to retrieve archived items"})
					return
				}
				c.JSON(200, loans)
			})

			// Get complete item history - all items chronologically
			admin.GET("/loans/history", manageInventory, func(c *gin.Context) {
				var loans []Loan
				// Get all loans ordered by latest activity (updated_at DESC, then created_at DESC)
				if err := db.Order("CASE WHEN updated_at > created_at THEN updated_at ELSE created_at END DESC").Find(&loans).Error; err != nil {
					c.JSON(500, gin.H{"error": "Failed to retrieve item history"})
					return
				}
				c.JSON(200, loans)
			})

			// Export all data as CSV
			admin.GET("/export-csv", manageInventory, func(c *gin.Context) {
				var loans []Loan
				if err := db.Find(&loans).Error; err != nil {
					c.JSON(500, gin.H{"error": "Failed to retrieve data for export"})
					return
				}

				// Set CSV headers
				c.Header("Content-Type", "text/csv")
				c.Header("Content-Disposition", "attachment; filename=robotics_research_centre_loans.csv")

				writer := csv.NewWriter(c.Writer)
				defer writer.Flush()

				// Write CSV header
				header := []string{
					"ID", "Created At", "Updated At", "Borrower Name", "Borrower Phone",
					"Item Name", "Lab Location", "Quantity Borrowed", "Expected Return Date",
					"Purpose", "Photo Filename", "Status", "Approval Status",
					"Approved By", "Approved At", "Denied At", "Return Requested",
					"Return Approval Status", "Return Requested At", "Days Since Borrowed",
					"Is Overdue", "Days Overdue",
				}
				writer.Write(header)

				// Write data rows
				for _, loan := range loans {
					// Calculate additional fields
					daysSinceBorrowed := int(time.Since(loan.CreatedAt).Hours() / 24)

					isOverdue, daysOverdue := loanOverdue(loan, time.Now())

					record := []string{
						strconv.Itoa(int(loan.ID)),
						loan.CreatedAt.Format("2006-01-02 15:04:05"),
						loan.UpdatedAt.Format("2006-01-02 15:04:05"),
						loan.BorrowerName,
						loan.BorrowerPhone,
						loan.ItemName,
						loan.LabLocation,
						strconv.Itoa(loan.QuantityBorrowed),
						loan.ExpectedReturnDate,
						loan.Purpose,
						loan.PhotoFilename,
						loan.Status,
						loan.ApprovalStatus,
						loan.ApprovedBy,
						formatTimePtr(loan.ApprovedAt),
						formatTimePtr(loan.DeniedAt),
						strconv.FormatBool(loan.ReturnRequested),
						loan.ReturnApprovalStatus,
						formatTimePtr(loan.ReturnRequestedAt),
						strconv.Itoa(daysSinceBorrowed),
						strconv.FormatBool(isOverdue),
						strconv.Itoa(daysOverdue),
					}
					writer.Write(record)
				}
			})

			// Export Motion Capture Lab bookings as CSV
			admin.GET("/export-bookings-csv", manageBookings, func(c *gin.Context) {
				var bookings []Booking
				if err := bookingQuery(db, c).Order("start_time ASC").Find(&bookings).Error; err != nil {
					c.JSON(500, gin.H{"error": "Failed to retrieve bookings for export"})
					return
				}
				var resources []Resource
				db.Unscoped().Find(&resources)
				resourceName := map[uint]string{}
				for _, r := range resources {
					resourceName[r.ID] = r.Name
				}

				c.Header("Content-Type", "text/csv")
				c.Header("Content-Disposition", "attachment; filename=bookings.csv")

				writer := csv.NewWriter(c.Writer)
				defer writer.Flush()

				writer.Write([]string{"ID", "Resource", "Thread", "Status", "No-show", "Booked By", "Phone", "Purpose", "Start Time", "End Time", "Hours", "Created At"})

				for _, b := range bookings {
					writer.Write([]string{
						strconv.Itoa(int(b.ID)),
						resourceName[b.ResourceID],
						b.ThreadName,
						b.Status,
						strconv.FormatBool(b.NoShow),
						b.BookedBy,
						b.Phone,
						b.Purpose,
						b.StartTime.Local().Format("2006-01-02 15:04:05"),
						b.EndTime.Local().Format("2006-01-02 15:04:05"),
						fmt.Sprintf("%.1f", b.EndTime.Sub(b.StartTime).Hours()),
						b.CreatedAt.Format("2006-01-02 15:04:05"),
					})
				}
			})

			// Delete all loan records (server admins only)
			admin.DELETE("/delete-all-items", requirePerm(PermManageUsers), func(c *gin.Context) {
				type DeleteAllRequest struct {
					ConfirmDelete bool `json:"confirm_delete" binding:"required"`
				}

				var req DeleteAllRequest
				if err := c.ShouldBindJSON(&req); err != nil {
					c.JSON(400, gin.H{"error": "Invalid delete request data"})
					return
				}

				if !req.ConfirmDelete {
					c.JSON(400, gin.H{"error": "Delete confirmation is required"})
					return
				}

				// Count total loans before deletion
				var loanCount int64
				db.Model(&Loan{}).Count(&loanCount)

				// Delete all loan records (this is what contains the "items" data)
				if err := db.Exec("DELETE FROM loans").Error; err != nil {
					c.JSON(500, gin.H{"error": "Failed to delete loan records"})
					return
				}

				// Also delete any orphaned photos
				photoDir := "./uploads"
				if files, err := os.ReadDir(photoDir); err == nil {
					for _, file := range files {
						if !file.IsDir() {
							os.Remove(filepath.Join(photoDir, file.Name()))
						}
					}
				}

				c.JSON(200, gin.H{
					"message":       "All loan records deleted successfully",
					"deleted_count": loanCount,
				})
			})
		}
	}

	return router
}
