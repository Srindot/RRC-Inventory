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
	"log"
	"mime/multipart"
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
)

// --- HELPER FUNCTIONS ---

// validateImageFile checks if the uploaded file is a valid image format
func validateImageFile(file *multipart.FileHeader) error {
	// Check file size (max 10MB)
	maxSize := int64(10 * 1024 * 1024) // 10MB
	if file.Size > maxSize {
		return fmt.Errorf("file size too large. Maximum allowed size is 10MB")
	}

	// Get file extension
	ext := strings.ToLower(filepath.Ext(file.Filename))

	// Allowed image extensions (common smartphone formats)
	allowedExts := map[string]bool{
		".jpg":  true,
		".jpeg": true,
		".png":  true,
		".webp": true,
		".heic": true,
		".heif": true,
		".gif":  true,
		".bmp":  true,
		".svg":  true, // For testing purposes
	}

	if !allowedExts[ext] {
		return fmt.Errorf("unsupported file format. Allowed formats: JPG, JPEG, PNG, WEBP, HEIC, HEIF, GIF, BMP")
	}

	return nil
}

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
	BookedBy string `json:"booked_by"`
	Phone    string `json:"phone"`
	Purpose  string `json:"purpose"`
	StartTime time.Time `json:"start_time"`
	EndTime   time.Time `json:"end_time"`
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
	db.AutoMigrate(&Item{}, &Loan{}, &Admin{}, &Booking{})

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

	sessions := newSessionStore()

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
		// Serve uploaded photos
		api.Static("/photos", "./uploads")

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
			var newItem Item
			if err := c.ShouldBindJSON(&newItem); err != nil {
				c.JSON(400, gin.H{"error": "Invalid data"})
				return
			}

			// Set quantity on hand to be the total quantity initially
			newItem.QuantityOnHand = newItem.TotalQuantity

			if err := db.Create(&newItem).Error; err != nil {
				c.JSON(500, gin.H{"error": "Failed to create item"})
				return
			}
			c.JSON(200, newItem)
		})
		// --- END OF NEW ENDPOINT ---

		// Get a list of all active loans (for the dashboard)
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
			c.JSON(200, loans)
		})

		// Endpoint for borrowing an item
		api.POST("/borrow", func(c *gin.Context) {
			// Handle multipart form data for file upload
			form, err := c.MultipartForm()
			if err != nil {
				c.JSON(400, gin.H{"error": "Invalid form data: " + err.Error()})
				return
			}

			// Extract form fields
			borrowerName := ""
			borrowerPhone := ""
			itemName := ""
			labLocation := ""
			quantityBorrowed := 0
			expectedReturnDate := ""
			purpose := ""

			if values := form.Value["borrower_name"]; len(values) > 0 {
				borrowerName = values[0]
			}
			if values := form.Value["borrower_phone"]; len(values) > 0 {
				borrowerPhone = values[0]
			}
			if values := form.Value["item_name"]; len(values) > 0 {
				itemName = values[0]
			}
			if values := form.Value["lab_location"]; len(values) > 0 {
				labLocation = values[0]
			}
			if values := form.Value["quantity_borrowed"]; len(values) > 0 {
				if qty, err := strconv.Atoi(values[0]); err == nil {
					quantityBorrowed = qty
				}
			}
			if values := form.Value["expected_return_date"]; len(values) > 0 {
				expectedReturnDate = values[0]
			}
			if values := form.Value["purpose"]; len(values) > 0 {
				purpose = values[0]
			}

			// Validate required fields. Purpose is optional - asking for it every
			// time was friction people were routing around.
			if borrowerName == "" || borrowerPhone == "" || itemName == "" || labLocation == "" || expectedReturnDate == "" {
				c.JSON(400, gin.H{"error": "Name, phone, item, lab and return date are required"})
				return
			}
			if purpose == "" {
				purpose = "Not specified"
			}

			// Handle file upload
			var photoFilename string
			if files := form.File["item_photo"]; len(files) > 0 {
				file := files[0]

				// Validate image file
				if err := validateImageFile(file); err != nil {
					c.JSON(400, gin.H{"error": "Invalid image file: " + err.Error()})
					return
				}

				// Generate unique filename with original extension. The random
				// suffix keeps two uploads in the same second from overwriting
				// each other.
				ext := strings.ToLower(filepath.Ext(file.Filename))
				suffix := make([]byte, 6)
				if _, err := rand.Read(suffix); err != nil {
					c.JSON(500, gin.H{"error": "Failed to store photo"})
					return
				}
				photoFilename = fmt.Sprintf("%d-%s%s", time.Now().Unix(), hex.EncodeToString(suffix), ext)

				// Save file to uploads directory
				if err := c.SaveUploadedFile(file, "./uploads/"+photoFilename); err != nil {
					c.JSON(500, gin.H{"error": "Failed to save photo: " + err.Error()})
					return
				}
			}

			// Borrowing is self-service: the loan is active immediately, no approval needed.
			newLoan := Loan{
				BorrowerName:       borrowerName,
				BorrowerPhone:      borrowerPhone,
				ItemName:           itemName,
				LabLocation:        labLocation,
				QuantityBorrowed:   quantityBorrowed,
				ExpectedReturnDate: expectedReturnDate,
				Purpose:            purpose,
				PhotoFilename:      photoFilename,
				Status:             "active",
				ApprovalStatus:     "approved",
			}

			if err := db.Create(&newLoan).Error; err != nil {
				c.JSON(500, gin.H{"error": "Failed to process borrow request: " + err.Error()})
				return
			}

			c.JSON(200, gin.H{"message": "Item borrowed successfully! Please return it by the expected date.", "loan_id": newLoan.ID})
		})

		// Endpoint for returning an item, identified by its loan ID
		api.POST("/return/:id", func(c *gin.Context) {
			loanID, err := strconv.Atoi(c.Param("id"))
			if err != nil {
				c.JSON(400, gin.H{"error": "Invalid loan ID"})
				return
			}

			// Returning is self-service: mark the loan returned right away.
			err = db.Transaction(func(tx *gorm.DB) error {
				var loan Loan
				if err := tx.First(&loan, loanID).Error; err != nil {
					if errors.Is(err, gorm.ErrRecordNotFound) {
						return fmt.Errorf("loan not found")
					}
					return err
				}

				if loan.Status == "returned" {
					return fmt.Errorf("item has already been returned")
				}

				now := time.Now()
				loan.Status = "returned"
				loan.ReturnRequested = true
				loan.ReturnApprovalStatus = "approved"
				loan.ReturnRequestedAt = &now
				loan.ReturnedAt = &now
				return tx.Save(&loan).Error
			})

			if err != nil {
				c.JSON(400, gin.H{"error": err.Error()})
				return
			}

			c.JSON(200, gin.H{"message": "Item marked as returned. Thank you!"})
		})

		// --- MOTION CAPTURE LAB BOOKINGS ---

		// List bookings in a time range (defaults to the next 8 weeks)
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
			c.JSON(200, bookings)
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

			// Reject overlaps, checked inside the transaction that inserts the row.
			err = db.Transaction(func(tx *gorm.DB) error {
				var clash Booking
				err := tx.Where("start_time < ? AND end_time > ?", end, start).First(&clash).Error
				if err == nil {
					return fmt.Errorf("that slot overlaps an existing booking by %s (%s - %s)",
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
				c.JSON(409, gin.H{"error": err.Error()})
				return
			}

			c.JSON(200, gin.H{"message": "Motion Capture Lab booked!", "booking": newBooking})
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

			if strings.TrimSpace(req.Phone) != booking.Phone {
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

				var req LoginRequest
				if err := c.ShouldBindJSON(&req); err != nil {
					c.JSON(400, gin.H{"error": "Invalid login data"})
					return
				}

				var admin Admin
				if err := db.Where("username = ?", req.Username).First(&admin).Error; err != nil {
					c.JSON(401, gin.H{"error": "Invalid credentials"})
					return
				}

				ok, needsUpgrade := verifyPassword(admin.Password, req.Password)
				if !ok {
					c.JSON(401, gin.H{"error": "Invalid credentials"})
					return
				}

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
				orderClause := `
					CASE
						WHEN status = 'not_found' THEN 3
						WHEN status = 'active' AND expected_return_date::date < CURRENT_DATE THEN 1
						ELSE 2
					END ASC,
					expected_return_date ASC
				`

				if err := query.Order(orderClause).Find(&loans).Error; err != nil {
					c.JSON(500, gin.H{"error": "Failed to retrieve loans"})
					return
				}
				c.JSON(200, loans)
			})

			// Extend loan return date
			admin.POST("/loans/:id/extend", func(c *gin.Context) {
				loanID := c.Param("id")

				type ExtendRequest struct {
					ExtendDays  int    `json:"extend_days"`
					ExtendHours int    `json:"extend_hours"`
					AdminName   string `json:"admin_name" binding:"required"`
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

				// Parse current return date and extend it
				currentDate, err := time.Parse("2006-01-02", loan.ExpectedReturnDate)
				if err != nil {
					c.JSON(400, gin.H{"error": "Invalid current return date format"})
					return
				}

				// Add the extension
				newDate := currentDate.AddDate(0, 0, req.ExtendDays)
				newDate = newDate.Add(time.Duration(req.ExtendHours) * time.Hour)

				loan.ExpectedReturnDate = newDate.Format("2006-01-02")
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

					expectedReturnTime, _ := time.Parse("2006-01-02T15:04:05Z07:00", loan.ExpectedReturnDate)
					isOverdue := time.Now().After(expectedReturnTime)
					daysOverdue := 0
					if isOverdue {
						daysOverdue = int(time.Since(expectedReturnTime).Hours() / 24)
					}

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
	log.Println("Starting server on port 8080...")
	router.Run(":8080")
}
