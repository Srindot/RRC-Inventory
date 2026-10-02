package main

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
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

// borrowRequest is the validated text part of a borrow form.
type borrowRequest struct {
	BorrowerName       string
	BorrowerPhone      string
	ItemName           string
	LabLocation        string
	Quantity           int
	ExpectedReturnDate string
	Purpose            string
}

// parseBorrowForm validates the fields of a borrow form. Purpose is optional -
// asking for it every time was friction people were routing around.
func parseBorrowForm(values map[string][]string) (borrowRequest, error) {
	get := func(key string) string {
		if v := values[key]; len(v) > 0 {
			return strings.TrimSpace(v[0])
		}
		return ""
	}

	req := borrowRequest{
		BorrowerName:       get("borrower_name"),
		BorrowerPhone:      get("borrower_phone"),
		ItemName:           get("item_name"),
		LabLocation:        get("lab_location"),
		ExpectedReturnDate: get("expected_return_date"),
		Purpose:            get("purpose"),
	}

	if req.BorrowerName == "" || req.BorrowerPhone == "" || req.ItemName == "" || req.LabLocation == "" || req.ExpectedReturnDate == "" {
		return req, errors.New("Name, phone, item, lab and return date are required")
	}
	if normalizePhone(req.BorrowerPhone) == "" {
		return req, errors.New("Enter a valid phone number")
	}
	if _, ok := parseReturnDate(req.ExpectedReturnDate); !ok {
		return req, errors.New("Invalid return date")
	}

	qty, err := strconv.Atoi(get("quantity_borrowed"))
	if err != nil || qty < 1 {
		return req, errors.New("Quantity must be a whole number of at least 1")
	}
	req.Quantity = qty

	if req.Purpose == "" {
		req.Purpose = "Not specified"
	}
	return req, nil
}

// PublicLoan is what the public dashboard sees of a loan. It deliberately
// leaves out the borrower's phone number and purpose: the list is open to
// anyone, and the phone is what proves who may return the item.
type PublicLoan struct {
	ID                 uint       `json:"ID"`
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

// PublicBooking is the public calendar's view of a booking - no phone number,
// since that is what proves who may cancel it.
type PublicBooking struct {
	ID        uint      `json:"ID"`
	BookedBy  string    `json:"booked_by"`
	Purpose   string    `json:"purpose"`
	StartTime time.Time `json:"start_time"`
	EndTime   time.Time `json:"end_time"`
}

func toPublicBookings(bookings []Booking) []PublicBooking {
	out := make([]PublicBooking, 0, len(bookings))
	for _, b := range bookings {
		out = append(out, PublicBooking{
			ID:        b.ID,
			BookedBy:  b.BookedBy,
			Purpose:   b.Purpose,
			StartTime: b.StartTime,
			EndTime:   b.EndTime,
		})
	}
	return out
}

// errBookingClash marks a booking refused because the slot is taken.
var errBookingClash = errors.New("that slot overlaps an existing booking")

// bookingLockKey serialises bookings with a Postgres advisory lock. There is
// only one lab, so one key; without it two overlapping requests can both pass
// the overlap check before either inserts.
const bookingLockKey = 7_311_001

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

// --- ADMIN SESSIONS ---

const sessionTTL = 12 * time.Hour

type session struct {
	Username  string
	ExpiresAt time.Time
}

type sessionStore struct {
	mu       sync.RWMutex
	sessions map[string]session
}

func newSessionStore() *sessionStore {
	return &sessionStore{sessions: make(map[string]session)}
}

func (s *sessionStore) create(username string) (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(buf)

	s.mu.Lock()
	defer s.mu.Unlock()
	// Opportunistically drop expired sessions.
	now := time.Now()
	for t, sess := range s.sessions {
		if now.After(sess.ExpiresAt) {
			delete(s.sessions, t)
		}
	}
	s.sessions[token] = session{Username: username, ExpiresAt: now.Add(sessionTTL)}
	return token, nil
}

func (s *sessionStore) lookup(token string) (string, bool) {
	s.mu.RLock()
	sess, ok := s.sessions[token]
	s.mu.RUnlock()
	if !ok || time.Now().After(sess.ExpiresAt) {
		return "", false
	}
	return sess.Username, true
}

func (s *sessionStore) revoke(token string) {
	s.mu.Lock()
	delete(s.sessions, token)
	s.mu.Unlock()
}

// revokeUser ends every session belonging to username except keep, so a
// password change logs out anyone else who had the old password.
func (s *sessionStore) revokeUser(username, keep string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for t, sess := range s.sessions {
		if sess.Username == username && t != keep {
			delete(s.sessions, t)
		}
	}
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
	Name           string `json:"name"`
	HomeLab        string `json:"home_lab"`
	TotalQuantity  int    `json:"total_quantity"`
	QuantityOnHand int    `json:"quantity_on_hand"`
}

type Admin struct {
	gorm.Model
	Username     string `json:"username" gorm:"unique"`
	Password     string `json:"-"` // Don't include in JSON responses
	Name         string `json:"name"`
	IsSuperAdmin bool   `json:"is_super_admin" gorm:"default:false"`
}

type Loan struct {
	gorm.Model
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
	BookedBy  string    `json:"booked_by"`
	Phone     string    `json:"phone"`
	Purpose   string    `json:"purpose"`
	StartTime time.Time `json:"start_time"`
	EndTime   time.Time `json:"end_time"`
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
	db.AutoMigrate(&Item{}, &Loan{}, &Admin{}, &Booking{}, &PrinterCredential{}, &PrintJob{})

	// Approvals were removed. Bring records created under the old flow into the
	// new states so nothing is stranded in a status the app no longer uses.
	if res := db.Model(&Loan{}).
		Where("status IN ?", []string{"pending", "approved", "borrowed"}).
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

	// Create the bootstrap super admin if no admin exists yet.
	// Credentials come from the environment - never hardcode them, this repo is public.
	var adminCount int64
	db.Model(&Admin{}).Count(&adminCount)
	if adminCount == 0 {
		username := os.Getenv("ADMIN_USERNAME")
		if username == "" {
			username = "admin"
		}
		password := os.Getenv("ADMIN_PASSWORD")
		generated := false
		if password == "" {
			buf := make([]byte, 12)
			if _, err := rand.Read(buf); err != nil {
				log.Fatalf("failed to generate bootstrap admin password: %v", err)
			}
			password = base64.RawURLEncoding.EncodeToString(buf)
			generated = true
		}

		hashedPassword, err := hashPassword(password)
		if err != nil {
			log.Fatalf("failed to hash bootstrap admin password: %v", err)
		}

		defaultAdmin := Admin{
			Username:     username,
			Password:     hashedPassword,
			Name:         username + " (Super Admin)",
			IsSuperAdmin: true,
		}
		if err := db.Create(&defaultAdmin).Error; err != nil {
			log.Fatalf("failed to create bootstrap admin: %v", err)
		}
		log.Printf("Bootstrap super admin created: %s", username)
		if generated {
			log.Printf("ADMIN_PASSWORD was not set. Generated one-time password: %s", password)
			log.Println("Log in and change it immediately.")
		}
	}

	// Connect to the lab's 3D printers, if any are configured
	printers := loadPrinterManager(db)

	sessions := newSessionStore()
	loginLimits := newLoginLimiter()

	// requireAdmin authenticates admin API calls with a bearer session token.
	requireAdmin := func(c *gin.Context) {
		username, ok := sessions.lookup(bearerToken(c))
		if !ok {
			c.AbortWithStatusJSON(401, gin.H{"error": "Authentication required"})
			return
		}

		var admin Admin
		if err := db.Where("username = ?", username).First(&admin).Error; err != nil {
			c.AbortWithStatusJSON(401, gin.H{"error": "Authentication required"})
			return
		}

		c.Set("admin", admin)
		c.Next()
	}

	// currentAdmin returns the admin authenticated by requireAdmin.
	currentAdmin := func(c *gin.Context) Admin {
		return c.MustGet("admin").(Admin)
	}

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
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
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

		// Get a list of all items
		api.GET("/items", func(c *gin.Context) {
			var items []Item
			if err := db.Find(&items).Error; err != nil {
				c.JSON(500, gin.H{"error": "Failed to retrieve items"})
				return
			}
			c.JSON(200, items)
		})

		// --- NEW ENDPOINT TO CREATE ITEMS ---
		api.POST("/items", requireAdmin, func(c *gin.Context) {
			type ItemRequest struct {
				Name          string `json:"name"`
				HomeLab       string `json:"home_lab"`
				TotalQuantity int    `json:"total_quantity"`
			}

			var req ItemRequest
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(400, gin.H{"error": "Invalid data"})
				return
			}
			if strings.TrimSpace(req.Name) == "" {
				c.JSON(400, gin.H{"error": "Item name is required"})
				return
			}
			if req.TotalQuantity < 0 {
				c.JSON(400, gin.H{"error": "Quantity cannot be negative"})
				return
			}

			// Set quantity on hand to be the total quantity initially
			newItem := Item{
				Name:           strings.TrimSpace(req.Name),
				HomeLab:        strings.TrimSpace(req.HomeLab),
				TotalQuantity:  req.TotalQuantity,
				QuantityOnHand: req.TotalQuantity,
			}

			if err := db.Create(&newItem).Error; err != nil {
				c.JSON(500, gin.H{"error": "Failed to create item"})
				return
			}
			c.JSON(200, newItem)
		})
		// --- END OF NEW ENDPOINT ---

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

		// Endpoint for borrowing an item
		api.POST("/borrow", limitBody(defaultBodyLimit), func(c *gin.Context) {
			// Handle multipart form data for file upload
			form, err := c.MultipartForm()
			if err != nil {
				if bodyTooLarge(err) {
					c.JSON(413, gin.H{"error": "Upload is too large. Photos can be up to 10MB."})
					return
				}
				log.Printf("borrow: bad form: %v", err)
				c.JSON(400, gin.H{"error": "Invalid form data"})
				return
			}

			req, err := parseBorrowForm(form.Value)
			if err != nil {
				c.JSON(400, gin.H{"error": err.Error()})
				return
			}

			// Handle file upload
			var photoFilename string
			if files := form.File["item_photo"]; len(files) > 0 {
				file := files[0]

				// Validate image file. The stored extension comes from what
				// the content actually is, not from the name it was sent with.
				ext, err := validateImageFile(file)
				if err != nil {
					c.JSON(400, gin.H{"error": "Invalid image file: " + err.Error()})
					return
				}

				// Generate unique filename. The random suffix keeps two uploads
				// in the same second from overwriting each other.
				suffix := make([]byte, 6)
				if _, err := rand.Read(suffix); err != nil {
					c.JSON(500, gin.H{"error": "Failed to store photo"})
					return
				}
				photoFilename = fmt.Sprintf("%d-%s%s", time.Now().Unix(), hex.EncodeToString(suffix), ext)

				// Save file to uploads directory
				if err := c.SaveUploadedFile(file, "./uploads/"+photoFilename); err != nil {
					log.Printf("borrow: saving photo: %v", err)
					c.JSON(500, gin.H{"error": "Failed to save photo"})
					return
				}
			}

			// Borrowing is self-service: the loan is active immediately, no approval needed.
			newLoan := Loan{
				BorrowerName:       req.BorrowerName,
				BorrowerPhone:      req.BorrowerPhone,
				ItemName:           req.ItemName,
				LabLocation:        req.LabLocation,
				QuantityBorrowed:   req.Quantity,
				ExpectedReturnDate: req.ExpectedReturnDate,
				Purpose:            req.Purpose,
				PhotoFilename:      photoFilename,
				Status:             "active",
				ApprovalStatus:     "approved",
			}

			if err := db.Create(&newLoan).Error; err != nil {
				log.Printf("borrow: creating loan: %v", err)
				c.JSON(500, gin.H{"error": "Failed to process borrow request"})
				return
			}

			c.JSON(200, gin.H{"message": "Item borrowed successfully! Please return it by the expected date.", "loan_id": newLoan.ID})
		})

		// Endpoint for returning an item, identified by its loan ID. The
		// borrower confirms the phone number it was borrowed with, so a
		// stranger cannot mark someone else's item as back.
		api.POST("/return/:id", func(c *gin.Context) {
			loanID, err := strconv.Atoi(c.Param("id"))
			if err != nil {
				c.JSON(400, gin.H{"error": "Invalid loan ID"})
				return
			}

			type ReturnRequest struct {
				Phone string `json:"phone"`
			}
			var req ReturnRequest
			if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Phone) == "" {
				c.JSON(400, gin.H{"error": "Enter the phone number you borrowed the item with"})
				return
			}

			// Returning is self-service: mark the loan returned right away.
			err = db.Transaction(func(tx *gorm.DB) error {
				var loan Loan
				if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&loan, loanID).Error; err != nil {
					return err
				}

				if err := checkReturn(loan, req.Phone); err != nil {
					return err
				}

				now := time.Now()
				loan.Status = "returned"
				loan.ReturnRequested = true
				loan.ReturnApprovalStatus = "approved"
				loan.ReturnRequestedAt = &now
				loan.ReturnedAt = &now
				return tx.Save(&loan).Error
			})

			switch {
			case err == nil:
			case errors.Is(err, gorm.ErrRecordNotFound):
				c.JSON(404, gin.H{"error": "Loan not found"})
				return
			case errors.Is(err, errReturnPhoneMismatch):
				c.JSON(403, gin.H{"error": "That phone number does not match the one this item was borrowed with"})
				return
			case errors.Is(err, errReturnAlreadyDone), errors.Is(err, errReturnNotActive):
				c.JSON(409, gin.H{"error": err.Error()})
				return
			default:
				log.Printf("return %d: %v", loanID, err)
				c.JSON(500, gin.H{"error": "Failed to return item"})
				return
			}

			c.JSON(200, gin.H{"message": "Item marked as returned. Thank you!"})
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

		// Send a sliced file to a printer. Anyone on the site can do this,
		// because avoiding a wifi switch is the whole point - but it only
		// *uploads*. Starting the print still needs somebody at the machine
		// who can see the plate is clear.
		api.POST("/printers/:id/files", limitBody(printerBodyLimit), func(c *gin.Context) {
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

			c.JSON(200, gin.H{
				"message": fmt.Sprintf(
					"%s sent. Start it from the printer's screen.", name),
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

		// --- MOTION CAPTURE LAB BOOKINGS ---

		// List bookings in a time range (defaults to the next 8 weeks). Public,
		// so phone numbers are left out - admins use /admin/bookings.
		api.GET("/bookings", func(c *gin.Context) {
			from := time.Now().AddDate(0, 0, -14)
			to := time.Now().AddDate(0, 0, 56)

			if v := c.Query("from"); v != "" {
				if t, err := time.Parse(time.RFC3339, v); err == nil {
					from = t
				}
			}
			if v := c.Query("to"); v != "" {
				if t, err := time.Parse(time.RFC3339, v); err == nil {
					to = t
				}
			}

			var bookings []Booking
			if err := db.Where("start_time < ? AND end_time > ?", to, from).
				Order("start_time ASC").Find(&bookings).Error; err != nil {
				c.JSON(500, gin.H{"error": "Failed to retrieve bookings"})
				return
			}
			c.JSON(200, toPublicBookings(bookings))
		})

		// Book the lab. No approval - the slot just has to be free.
		api.POST("/bookings", func(c *gin.Context) {
			type BookingRequest struct {
				BookedBy  string `json:"booked_by" binding:"required"`
				Phone     string `json:"phone" binding:"required"`
				Purpose   string `json:"purpose" binding:"required"`
				StartTime string `json:"start_time" binding:"required"`
				EndTime   string `json:"end_time" binding:"required"`
			}

			var req BookingRequest
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(400, gin.H{"error": "All fields are required"})
				return
			}

			start, err := time.Parse(time.RFC3339, req.StartTime)
			if err != nil {
				c.JSON(400, gin.H{"error": "Invalid start time"})
				return
			}
			end, err := time.Parse(time.RFC3339, req.EndTime)
			if err != nil {
				c.JSON(400, gin.H{"error": "Invalid end time"})
				return
			}

			if !end.After(start) {
				c.JSON(400, gin.H{"error": "End time must be after the start time"})
				return
			}
			if end.Sub(start) > 12*time.Hour {
				c.JSON(400, gin.H{"error": "A single booking cannot be longer than 12 hours"})
				return
			}
			if start.Before(time.Now().Add(-1 * time.Hour)) {
				c.JSON(400, gin.H{"error": "Cannot book a slot in the past"})
				return
			}

			newBooking := Booking{
				BookedBy:  strings.TrimSpace(req.BookedBy),
				Phone:     strings.TrimSpace(req.Phone),
				Purpose:   strings.TrimSpace(req.Purpose),
				StartTime: start,
				EndTime:   end,
			}

			// Reject overlaps, checked inside the transaction that inserts the
			// row. The advisory lock makes concurrent bookings take turns, so
			// two overlapping requests cannot both see a free slot.
			err = db.Transaction(func(tx *gorm.DB) error {
				if err := tx.Exec("SELECT pg_advisory_xact_lock(?)", bookingLockKey).Error; err != nil {
					return err
				}

				var clash Booking
				err := tx.Where("start_time < ? AND end_time > ?", end, start).First(&clash).Error
				if err == nil {
					return fmt.Errorf("%w by %s (%s - %s)", errBookingClash,
						clash.BookedBy,
						clash.StartTime.Local().Format("Mon 2 Jan 15:04"),
						clash.EndTime.Local().Format("15:04"))
				}
				if !errors.Is(err, gorm.ErrRecordNotFound) {
					return err
				}
				return tx.Create(&newBooking).Error
			})

			if err != nil {
				if errors.Is(err, errBookingClash) {
					c.JSON(409, gin.H{"error": err.Error()})
					return
				}
				log.Printf("booking: %v", err)
				c.JSON(500, gin.H{"error": "Failed to book the lab"})
				return
			}

			c.JSON(200, gin.H{"message": "Motion Capture Lab booked!", "booking": toPublicBookings([]Booking{newBooking})[0]})
		})

		// Cancel your own booking by confirming the phone number it was made with
		api.POST("/bookings/:id/cancel", func(c *gin.Context) {
			type CancelRequest struct {
				Phone string `json:"phone" binding:"required"`
			}

			var req CancelRequest
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(400, gin.H{"error": "Phone number is required"})
				return
			}

			var booking Booking
			if err := db.First(&booking, c.Param("id")).Error; err != nil {
				c.JSON(404, gin.H{"error": "Booking not found"})
				return
			}

			if !phonesMatch(req.Phone, booking.Phone) {
				c.JSON(403, gin.H{"error": "That phone number does not match this booking"})
				return
			}

			if err := db.Delete(&booking).Error; err != nil {
				c.JSON(500, gin.H{"error": "Failed to cancel booking"})
				return
			}

			c.JSON(200, gin.H{"message": "Booking cancelled"})
		})

		// --- ADMIN ROUTES ---
		admin := api.Group("/admin")
		{
			// Admin login
			admin.POST("/login", func(c *gin.Context) {
				type LoginRequest struct {
					Username string `json:"username" binding:"required"`
					Password string `json:"password" binding:"required"`
				}

				ip := c.ClientIP()
				if loginLimits.blocked(ip, time.Now()) {
					c.JSON(429, gin.H{"error": "Too many failed login attempts. Try again in a few minutes."})
					return
				}

				var req LoginRequest
				if err := c.ShouldBindJSON(&req); err != nil {
					c.JSON(400, gin.H{"error": "Invalid login data"})
					return
				}

				var admin Admin
				if err := db.Where("username = ?", req.Username).First(&admin).Error; err != nil {
					// Spend the same time as a real check, so response time
					// does not reveal which usernames exist.
					bcrypt.CompareHashAndPassword(dummyPasswordHash, []byte(req.Password))
					loginLimits.fail(ip, time.Now())
					c.JSON(401, gin.H{"error": "Invalid credentials"})
					return
				}

				ok, needsUpgrade := verifyPassword(admin.Password, req.Password)
				if !ok {
					loginLimits.fail(ip, time.Now())
					c.JSON(401, gin.H{"error": "Invalid credentials"})
					return
				}
				loginLimits.succeed(ip)

				// Transparently migrate legacy SHA-256 hashes to bcrypt on login.
				if needsUpgrade {
					if newHash, err := hashPassword(req.Password); err == nil {
						db.Model(&admin).Update("password", newHash)
					}
				}

				token, err := sessions.create(admin.Username)
				if err != nil {
					c.JSON(500, gin.H{"error": "Failed to create session"})
					return
				}

				c.JSON(200, gin.H{
					"message": "Login successful",
					"token":   token,
					"admin": gin.H{
						"name":           admin.Name,
						"username":       admin.Username,
						"is_super_admin": admin.IsSuperAdmin,
					},
				})
			})

			// Every route below requires a valid admin session.
			admin.Use(requireAdmin)

			// Log out - invalidate the current session token
			admin.POST("/logout", func(c *gin.Context) {
				sessions.revoke(bearerToken(c))
				c.JSON(200, gin.H{"message": "Logged out"})
			})

			// Confirm the stored session is still valid
			admin.GET("/me", func(c *gin.Context) {
				admin := currentAdmin(c)
				c.JSON(200, gin.H{
					"name":           admin.Name,
					"username":       admin.Username,
					"is_super_admin": admin.IsSuperAdmin,
				})
			})

			// Get loans by lab with status filtering and smart ordering
			admin.GET("/loans/by-lab/:lab", func(c *gin.Context) {
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
			admin.POST("/loans/:id/extend", func(c *gin.Context) {
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
				if err := db.First(&loan, loanID).Error; err != nil {
					c.JSON(404, gin.H{"error": "Loan not found"})
					return
				}

				newDate, err := extendReturnDate(loan.ExpectedReturnDate, req.ExtendDays, req.ExtendHours)
				if err != nil {
					c.JSON(400, gin.H{"error": err.Error()})
					return
				}

				loan.ExpectedReturnDate = newDate
				if err := db.Save(&loan).Error; err != nil {
					c.JSON(500, gin.H{"error": "Failed to extend loan"})
					return
				}

				c.JSON(200, gin.H{"message": "Loan extended successfully"})
			})

			// Mark an item as missing - the admin cannot find it in the lab
			admin.POST("/loans/:id/mark-missing", func(c *gin.Context) {
				loanID := c.Param("id")

				var loan Loan
				if err := db.First(&loan, loanID).Error; err != nil {
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
				loan.ApprovedBy = currentAdmin(c).Name
				loan.ApprovedAt = &now

				if err := db.Save(&loan).Error; err != nil {
					c.JSON(500, gin.H{"error": "Failed to mark item as missing"})
					return
				}

				c.JSON(200, gin.H{"message": "Item marked as missing"})
			})

			// Mark item as found (restore from not_found status)
			admin.POST("/loans/:id/mark-found", func(c *gin.Context) {
				loanID := c.Param("id")

				var loan Loan
				if err := db.First(&loan, loanID).Error; err != nil {
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
				loan.ApprovedBy = currentAdmin(c).Name
				loan.ApprovedAt = &now

				if err := db.Save(&loan).Error; err != nil {
					c.JSON(500, gin.H{"error": "Failed to mark item as found"})
					return
				}

				c.JSON(200, gin.H{"message": "Item successfully marked as found and restored to borrowed status"})
			})

			// Get missing items
			admin.GET("/loans/lost-missing", func(c *gin.Context) {
				var loans []Loan
				if err := db.Where("status = ?", "not_found").Order("updated_at DESC").Find(&loans).Error; err != nil {
					c.JSON(500, gin.H{"error": "Failed to retrieve missing items"})
					return
				}
				c.JSON(200, loans)
			})

			// Stop the current print job. Admins only - a stray click here
			// destroys someone's work, so it is deliberately not public.
			admin.POST("/printers/:id/stop", func(c *gin.Context) {
				adminName := currentAdmin(c).Name
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
			admin.POST("/printers/:id/print", func(c *gin.Context) {
				var req StartRequest
				if err := c.ShouldBindJSON(&req); err != nil {
					c.JSON(400, gin.H{"error": "Invalid print request"})
					return
				}

				adminName := currentAdmin(c).Name
				if err := printers.StartPrint(c.Param("id"), req, adminName); err != nil {
					c.JSON(400, gin.H{"error": err.Error()})
					return
				}
				c.JSON(200, gin.H{"message": "Print started"})
			})

			// Tidy up old plates - deleting other people's files is an
			// admin job, uploading is not.
			admin.DELETE("/printers/:id/files/:name", func(c *gin.Context) {
				if err := printers.DeleteFile(c.Param("id"), c.Param("name")); err != nil {
					c.JSON(400, gin.H{"error": err.Error()})
					return
				}
				c.JSON(200, gin.H{"message": "File deleted from the printer"})
			})

			// Clear the whole card, for when a term's worth of plates has
			// filled it up. Refuses while a job is running.
			admin.DELETE("/printers/:id/files", func(c *gin.Context) {
				deleted, err := printers.DeleteAllFiles(c.Param("id"), currentAdmin(c).Name)
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
			admin.POST("/printers/:id/pause", func(c *gin.Context) {
				if err := printers.Pause(c.Param("id"), currentAdmin(c).Name); err != nil {
					c.JSON(400, gin.H{"error": err.Error()})
					return
				}
				c.JSON(200, gin.H{"message": "Pause command sent to the printer"})
			})

			// Resume a paused job
			admin.POST("/printers/:id/resume", func(c *gin.Context) {
				if err := printers.Resume(c.Param("id"), currentAdmin(c).Name); err != nil {
					c.JSON(400, gin.H{"error": err.Error()})
					return
				}
				c.JSON(200, gin.H{"message": "Resume command sent to the printer"})
			})

			// Chamber light. Harmless in itself, but it commands hardware, so
			// it sits behind the same admin gate as the rest.
			admin.POST("/printers/:id/light", func(c *gin.Context) {
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
			admin.GET("/print-jobs", func(c *gin.Context) {
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
			admin.GET("/export-print-jobs-csv", func(c *gin.Context) {
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
			admin.PUT("/printers/:id/access-code", func(c *gin.Context) {
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
			admin.GET("/bookings", func(c *gin.Context) {
				from := time.Now().AddDate(0, 0, -14)
				to := time.Now().AddDate(0, 0, 56)

				if v := c.Query("from"); v != "" {
					if t, err := time.Parse(time.RFC3339, v); err == nil {
						from = t
					}
				}
				if v := c.Query("to"); v != "" {
					if t, err := time.Parse(time.RFC3339, v); err == nil {
						to = t
					}
				}

				var bookings []Booking
				if err := db.Where("start_time < ? AND end_time > ?", to, from).
					Order("start_time ASC").Find(&bookings).Error; err != nil {
					c.JSON(500, gin.H{"error": "Failed to retrieve bookings"})
					return
				}
				c.JSON(200, bookings)
			})

			// Delete any Motion Capture Lab booking
			admin.DELETE("/bookings/:id", func(c *gin.Context) {
				var booking Booking
				if err := db.First(&booking, c.Param("id")).Error; err != nil {
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
			admin.GET("/loans/archived", func(c *gin.Context) {
				twoWeeksAgo := time.Now().AddDate(0, 0, -14)
				var loans []Loan
				if err := db.Where("status = ? AND updated_at <= ?", "returned", twoWeeksAgo).Order("updated_at DESC").Find(&loans).Error; err != nil {
					c.JSON(500, gin.H{"error": "Failed to retrieve archived items"})
					return
				}
				c.JSON(200, loans)
			})

			// Get complete item history - all items chronologically
			admin.GET("/loans/history", func(c *gin.Context) {
				var loans []Loan
				// Get all loans ordered by latest activity (updated_at DESC, then created_at DESC)
				if err := db.Order("CASE WHEN updated_at > created_at THEN updated_at ELSE created_at END DESC").Find(&loans).Error; err != nil {
					c.JSON(500, gin.H{"error": "Failed to retrieve item history"})
					return
				}
				c.JSON(200, loans)
			})

			// Export all data as CSV
			admin.GET("/export-csv", func(c *gin.Context) {
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
			admin.GET("/export-bookings-csv", func(c *gin.Context) {
				var bookings []Booking
				if err := db.Order("start_time ASC").Find(&bookings).Error; err != nil {
					c.JSON(500, gin.H{"error": "Failed to retrieve bookings for export"})
					return
				}

				c.Header("Content-Type", "text/csv")
				c.Header("Content-Disposition", "attachment; filename=motion_capture_lab_bookings.csv")

				writer := csv.NewWriter(c.Writer)
				defer writer.Flush()

				writer.Write([]string{"ID", "Booked By", "Phone", "Purpose", "Start Time", "End Time", "Hours", "Created At"})

				for _, b := range bookings {
					writer.Write([]string{
						strconv.Itoa(int(b.ID)),
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

			// === NEW ADMIN MANAGEMENT ROUTES ===

			// Change password for any admin
			admin.POST("/change-password", func(c *gin.Context) {
				type ChangePasswordRequest struct {
					OldPassword string `json:"old_password" binding:"required"`
					NewPassword string `json:"new_password" binding:"required"`
				}

				var req ChangePasswordRequest
				if err := c.ShouldBindJSON(&req); err != nil {
					c.JSON(400, gin.H{"error": "Invalid password change data"})
					return
				}

				// Validate new password length
				if len(req.NewPassword) < 8 {
					c.JSON(400, gin.H{"error": "New password must be at least 8 characters long"})
					return
				}

				// Only the logged-in admin's own password can be changed here
				admin := currentAdmin(c)
				if ok, _ := verifyPassword(admin.Password, req.OldPassword); !ok {
					c.JSON(401, gin.H{"error": "Current password is incorrect"})
					return
				}

				hashedNewPassword, err := hashPassword(req.NewPassword)
				if err != nil {
					c.JSON(500, gin.H{"error": "Failed to update password"})
					return
				}

				if err := db.Model(&admin).Update("password", hashedNewPassword).Error; err != nil {
					c.JSON(500, gin.H{"error": "Failed to update password"})
					return
				}

				// Anyone else signed in with the old password is logged out;
				// this session stays.
				sessions.revokeUser(admin.Username, bearerToken(c))

				c.JSON(200, gin.H{"message": "Password changed successfully"})
			})

			// Get all admins (only super admin can access)
			admin.GET("/list", func(c *gin.Context) {
				if !currentAdmin(c).IsSuperAdmin {
					c.JSON(403, gin.H{"error": "Only super admin can view admin list"})
					return
				}

				// Get all admins
				var admins []Admin
				if err := db.Find(&admins).Error; err != nil {
					c.JSON(500, gin.H{"error": "Failed to retrieve admins"})
					return
				}

				// Return admins without passwords
				var adminList []gin.H
				for _, admin := range admins {
					adminList = append(adminList, gin.H{
						"id":             admin.ID,
						"username":       admin.Username,
						"name":           admin.Name,
						"is_super_admin": admin.IsSuperAdmin,
						"created_at":     admin.CreatedAt,
					})
				}

				c.JSON(200, adminList)
			})

			// Create new admin (only super admin can create)
			admin.POST("/create", func(c *gin.Context) {
				type CreateAdminRequest struct {
					Username     string `json:"username" binding:"required"`
					Password     string `json:"password" binding:"required"`
					Name         string `json:"name" binding:"required"`
					IsSuperAdmin bool   `json:"is_super_admin"`
				}

				var req CreateAdminRequest
				if err := c.ShouldBindJSON(&req); err != nil {
					c.JSON(400, gin.H{"error": "Invalid admin creation data"})
					return
				}

				if !currentAdmin(c).IsSuperAdmin {
					c.JSON(403, gin.H{"error": "Only super admin can create new admins"})
					return
				}

				// Validate password length
				if len(req.Password) < 8 {
					c.JSON(400, gin.H{"error": "Password must be at least 8 characters long"})
					return
				}

				// Check if username already exists
				var existingAdmin Admin
				if err := db.Where("username = ?", req.Username).First(&existingAdmin).Error; err == nil {
					c.JSON(400, gin.H{"error": "Username already exists"})
					return
				}

				hashedPassword, err := hashPassword(req.Password)
				if err != nil {
					c.JSON(500, gin.H{"error": "Failed to create admin"})
					return
				}

				// Create new admin
				newAdmin := Admin{
					Username:     req.Username,
					Password:     hashedPassword,
					Name:         req.Name,
					IsSuperAdmin: req.IsSuperAdmin,
				}

				if err := db.Create(&newAdmin).Error; err != nil {
					c.JSON(500, gin.H{"error": "Failed to create admin"})
					return
				}

				c.JSON(200, gin.H{
					"message": "Admin created successfully",
					"admin": gin.H{
						"id":             newAdmin.ID,
						"username":       newAdmin.Username,
						"name":           newAdmin.Name,
						"is_super_admin": newAdmin.IsSuperAdmin,
					},
				})
			})

			// Delete admin (only super admin can delete other admins, except themselves)
			admin.DELETE("/delete/:id", func(c *gin.Context) {
				// Get admin ID from URL parameter
				adminId := c.Param("id")
				if adminId == "" {
					c.JSON(400, gin.H{"error": "Admin ID is required"})
					return
				}

				requestingAdmin := currentAdmin(c)
				if !requestingAdmin.IsSuperAdmin {
					c.JSON(403, gin.H{"error": "Only super admin can delete admins"})
					return
				}

				// Get the admin to be deleted
				var adminToDelete Admin
				if err := db.First(&adminToDelete, adminId).Error; err != nil {
					c.JSON(404, gin.H{"error": "Admin not found"})
					return
				}

				// Prevent self-deletion
				if adminToDelete.Username == requestingAdmin.Username {
					c.JSON(400, gin.H{"error": "Cannot delete yourself"})
					return
				}

				// Never leave the system without a super admin
				var superAdminCount int64
				db.Model(&Admin{}).Where("is_super_admin = ?", true).Count(&superAdminCount)
				if adminToDelete.IsSuperAdmin && superAdminCount <= 1 {
					c.JSON(400, gin.H{"error": "Cannot delete the last super admin account"})
					return
				}

				// Delete the admin
				if err := db.Delete(&adminToDelete).Error; err != nil {
					c.JSON(500, gin.H{"error": "Failed to delete admin"})
					return
				}

				c.JSON(200, gin.H{
					"message": "Admin deleted successfully",
					"deleted_admin": gin.H{
						"id":       adminToDelete.ID,
						"username": adminToDelete.Username,
						"name":     adminToDelete.Name,
					},
				})
			})

			// Delete all items data (only super admin can delete all data)
			admin.DELETE("/delete-all-items", func(c *gin.Context) {
				type DeleteAllRequest struct {
					ConfirmDelete bool `json:"confirm_delete" binding:"required"`
				}

				var req DeleteAllRequest
				if err := c.ShouldBindJSON(&req); err != nil {
					c.JSON(400, gin.H{"error": "Invalid delete request data"})
					return
				}

				if !currentAdmin(c).IsSuperAdmin {
					c.JSON(403, gin.H{"error": "Only super admin can delete all items data"})
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
