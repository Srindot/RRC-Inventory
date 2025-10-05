package main

import (
	"crypto/sha256"
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
	"time"

	"github.com/gin-gonic/gin"
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
	Username string `json:"username" gorm:"unique"`
	Password string `json:"-"` // Don't include in JSON responses
	Name     string `json:"name"`
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
	Status               string     `json:"status" gorm:"default:'pending'"`          // pending, approved, active, returned, denied
	ApprovalStatus       string     `json:"approval_status" gorm:"default:'pending'"` // pending, approved, denied
	ApprovedBy           string     `json:"approved_by"`
	ApprovedAt           *time.Time `json:"approved_at"`
	DeniedAt             *time.Time `json:"denied_at"`
	ReturnRequested      bool       `json:"return_requested" gorm:"default:false"`
	ReturnApprovalStatus string     `json:"return_approval_status" gorm:"default:'not_requested'"` // not_requested, pending, approved, not_found
	ReturnRequestedAt    *time.Time `json:"return_requested_at"`
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
	db.AutoMigrate(&Item{}, &Loan{}, &Admin{})
	log.Println("Migrations complete.")

	// Create uploads directory if it doesn't exist
	if err := os.MkdirAll("./uploads", 0755); err != nil {
		log.Printf("Warning: Could not create uploads directory: %v", err)
	}

	// Create default admin if not exists
	var adminCount int64
	db.Model(&Admin{}).Count(&adminCount)
	if adminCount == 0 {
		// Hash the password
		hasher := sha256.New()
		hasher.Write([]byte("rrc@srinath"))
		hashedPassword := hex.EncodeToString(hasher.Sum(nil))

		defaultAdmin := Admin{
			Username: "Srinath",
			Password: hashedPassword,
			Name:     "Srinath (Main Admin)",
		}
		db.Create(&defaultAdmin)
		log.Println("Default admin created: Srinath")
	}

	router := gin.Default()

	// Add CORS middleware for development
	router.Use(func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
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
		api.POST("/items", func(c *gin.Context) {
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
			// Include both active loans and items marked as not found (to show at top of list)
			if err := db.Where("(status = ? AND approval_status = ?) OR status = ?", "active", "approved", "not_found").
				Order("CASE WHEN status = 'not_found' THEN 0 ELSE 1 END, created_at DESC").
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

			// Validate required fields
			if borrowerName == "" || borrowerPhone == "" || itemName == "" || labLocation == "" || expectedReturnDate == "" || purpose == "" {
				c.JSON(400, gin.H{"error": "All fields are required"})
				return
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

				// Generate unique filename with original extension
				ext := strings.ToLower(filepath.Ext(file.Filename))
				photoFilename = fmt.Sprintf("%d%s", time.Now().Unix(), ext)

				// Save file to uploads directory
				if err := c.SaveUploadedFile(file, "./uploads/"+photoFilename); err != nil {
					c.JSON(500, gin.H{"error": "Failed to save photo: " + err.Error()})
					return
				}
			}

			// Create a new loan record with pending status for admin approval
			newLoan := Loan{
				BorrowerName:       borrowerName,
				BorrowerPhone:      borrowerPhone,
				ItemName:           itemName,
				LabLocation:        labLocation,
				QuantityBorrowed:   quantityBorrowed,
				ExpectedReturnDate: expectedReturnDate,
				Purpose:            purpose,
				PhotoFilename:      photoFilename,
				Status:             "pending",
				ApprovalStatus:     "pending",
			}

			if err := db.Create(&newLoan).Error; err != nil {
				c.JSON(500, gin.H{"error": "Failed to process borrow request: " + err.Error()})
				return
			}

			c.JSON(200, gin.H{"message": "Borrow request submitted successfully! Please wait for admin approval.", "loan_id": newLoan.ID})
		})

		// Endpoint for returning an item, identified by its loan ID
		api.POST("/return/:id", func(c *gin.Context) {
			loanID, err := strconv.Atoi(c.Param("id"))
			if err != nil {
				c.JSON(400, gin.H{"error": "Invalid loan ID"})
				return
			}

			// Mark the loan as return requested, requiring admin approval
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

				if loan.ReturnRequested {
					return fmt.Errorf("return request already submitted for this item")
				}

				if loan.ApprovalStatus != "approved" {
					return fmt.Errorf("cannot return item that hasn't been approved for borrowing")
				}

				now := time.Now()
				loan.ReturnRequested = true
				loan.ReturnApprovalStatus = "pending"
				loan.ReturnRequestedAt = &now
				if err := tx.Save(&loan).Error; err != nil {
					return err
				}
				return nil
			})

			if err != nil {
				c.JSON(400, gin.H{"error": err.Error()})
				return
			}

			c.JSON(200, gin.H{"message": "Return request submitted! Please wait for admin approval before physically returning the item."})
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

				// Hash the provided password
				hasher := sha256.New()
				hasher.Write([]byte(req.Password))
				hashedPassword := hex.EncodeToString(hasher.Sum(nil))

				var admin Admin
				if err := db.Where("username = ? AND password = ?", req.Username, hashedPassword).First(&admin).Error; err != nil {
					c.JSON(401, gin.H{"error": "Invalid credentials"})
					return
				}

				c.JSON(200, gin.H{"message": "Login successful", "admin": gin.H{"name": admin.Name, "username": admin.Username}})
			})

			// Get pending loan requests for approval
			admin.GET("/loans/pending", func(c *gin.Context) {
				var loans []Loan
				if err := db.Where("approval_status = ?", "pending").Find(&loans).Error; err != nil {
					c.JSON(500, gin.H{"error": "Failed to retrieve pending loans"})
					return
				}
				c.JSON(200, loans)
			})

			// Get loans by lab with status filtering and smart ordering
			admin.GET("/loans/by-lab/:lab", func(c *gin.Context) {
				lab := c.Param("lab")
				statusFilter := c.DefaultQuery("status", "all")

				var loans []Loan
				query := db.Where("lab_location = ?", lab)

				// Apply status filter
				if statusFilter == "borrowed" {
					query = query.Where("approval_status = ? AND status = ?", "approved", "active")
				} else if statusFilter == "rejected" {
					query = query.Where("approval_status = ?", "denied")
				} else if statusFilter == "returned" {
					// Show returned items from the last 2 weeks only
					twoWeeksAgo := time.Now().AddDate(0, 0, -14)
					query = query.Where("status = ? AND updated_at > ?", "returned", twoWeeksAgo)
				} else if statusFilter == "pending" {
					query = query.Where("approval_status = ?", "pending")
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
						WHEN approval_status = 'denied' THEN 4
						WHEN status = 'not_found' THEN 3
						WHEN approval_status = 'approved' AND status = 'active' AND expected_return_date::date < CURRENT_DATE THEN 1
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

			// Approve or deny a loan request
			admin.POST("/loans/:id/approve", func(c *gin.Context) {
				loanID := c.Param("id")

				type ApprovalRequest struct {
					Action    string `json:"action" binding:"required"` // "approve" or "deny"
					AdminName string `json:"admin_name" binding:"required"`
				}

				var req ApprovalRequest
				if err := c.ShouldBindJSON(&req); err != nil {
					c.JSON(400, gin.H{"error": "Invalid approval data"})
					return
				}

				var loan Loan
				if err := db.First(&loan, loanID).Error; err != nil {
					c.JSON(404, gin.H{"error": "Loan not found"})
					return
				}

				now := time.Now()
				if req.Action == "approve" {
					loan.ApprovalStatus = "approved"
					loan.Status = "active"
					loan.ApprovedBy = req.AdminName
					loan.ApprovedAt = &now
				} else if req.Action == "deny" {
					loan.ApprovalStatus = "denied"
					loan.Status = "denied"
					loan.ApprovedBy = req.AdminName
					loan.ApprovedAt = &now
					loan.DeniedAt = &now
				} else {
					c.JSON(400, gin.H{"error": "Invalid action"})
					return
				}

				if err := db.Save(&loan).Error; err != nil {
					c.JSON(500, gin.H{"error": "Failed to update loan"})
					return
				}

				c.JSON(200, gin.H{"message": "Loan " + req.Action + "d successfully"})
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

			// Approve return request
			admin.POST("/loans/:id/approve-return", func(c *gin.Context) {
				loanID := c.Param("id")

				type ReturnApprovalRequest struct {
					Action    string `json:"action" binding:"required"` // "approved" or "not_found"
					AdminName string `json:"admin_name" binding:"required"`
				}

				var req ReturnApprovalRequest
				if err := c.ShouldBindJSON(&req); err != nil {
					c.JSON(400, gin.H{"error": "Invalid approval data: " + err.Error()})
					return
				}

				var loan Loan
				if err := db.First(&loan, loanID).Error; err != nil {
					c.JSON(404, gin.H{"error": "Loan not found"})
					return
				}

				// Debug logging
				log.Printf("Return approval request for loan %s: ReturnRequested=%v, ReturnApprovalStatus=%s",
					loanID, loan.ReturnRequested, loan.ReturnApprovalStatus)

				if !loan.ReturnRequested {
					c.JSON(400, gin.H{"error": "No return request has been made for this loan"})
					return
				}

				if loan.ReturnApprovalStatus != "pending" {
					c.JSON(400, gin.H{"error": fmt.Sprintf("Return request is not pending. Current status: %s", loan.ReturnApprovalStatus)})
					return
				}

				now := time.Now()
				loan.ApprovedBy = req.AdminName
				loan.ApprovedAt = &now

				if req.Action == "approved" {
					loan.ReturnApprovalStatus = "approved"
					loan.Status = "returned"
				} else if req.Action == "not_found" {
					loan.ReturnApprovalStatus = "not_found"
					loan.Status = "not_found"
				} else {
					c.JSON(400, gin.H{"error": "Invalid action. Use 'approved' or 'not_found'"})
					return
				}

				if err := db.Save(&loan).Error; err != nil {
					c.JSON(500, gin.H{"error": "Failed to process return"})
					return
				}

				c.JSON(200, gin.H{"message": fmt.Sprintf("Return %s successfully", req.Action)})
			})

			// Mark item as found (restore from not_found status)
			admin.POST("/loans/:id/mark-found", func(c *gin.Context) {
				loanID := c.Param("id")

				type MarkFoundRequest struct {
					AdminName string `json:"admin_name" binding:"required"`
				}

				var req MarkFoundRequest
				if err := c.ShouldBindJSON(&req); err != nil {
					c.JSON(400, gin.H{"error": "Invalid request data: " + err.Error()})
					return
				}

				var loan Loan
				if err := db.First(&loan, loanID).Error; err != nil {
					c.JSON(404, gin.H{"error": "Loan not found"})
					return
				}

				// Check if the item is currently marked as not found
				if loan.Status != "not_found" {
					c.JSON(400, gin.H{"error": fmt.Sprintf("Item is not marked as not found. Current status: %s", loan.Status)})
					return
				}

				// Restore the item to borrowed status
				now := time.Now()
				loan.Status = "borrowed"
				loan.ReturnApprovalStatus = ""
				loan.ApprovedBy = req.AdminName
				loan.ApprovedAt = &now

				if err := db.Save(&loan).Error; err != nil {
					c.JSON(500, gin.H{"error": "Failed to mark item as found"})
					return
				}

				c.JSON(200, gin.H{"message": "Item successfully marked as found and restored to borrowed status"})
			})

			// Get pending return requests
			admin.GET("/loans/pending-returns", func(c *gin.Context) {
				var loans []Loan
				if err := db.Where("return_requested = ? AND return_approval_status = ?", true, "pending").Find(&loans).Error; err != nil {
					c.JSON(500, gin.H{"error": "Failed to retrieve pending returns"})
					return
				}
				c.JSON(200, loans)
			})

			// Get lost/missing items (not found + pending returns)
			admin.GET("/loans/lost-missing", func(c *gin.Context) {
				var loans []Loan
				if err := db.Where("status = ? OR (return_requested = ? AND return_approval_status = ?)", "not_found", true, "pending").Find(&loans).Error; err != nil {
					c.JSON(500, gin.H{"error": "Failed to retrieve lost/missing items"})
					return
				}
				c.JSON(200, loans)
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

			// Cleanup denied loans (called periodically)
			admin.POST("/cleanup-denied", func(c *gin.Context) {
				// Delete loans that were denied more than 24 hours ago
				oneDayAgo := time.Now().Add(-24 * time.Hour)

				result := db.Where("approval_status = ? AND denied_at < ?", "denied", oneDayAgo).Delete(&Loan{})
				if result.Error != nil {
					c.JSON(500, gin.H{"error": "Failed to cleanup denied loans"})
					return
				}

				c.JSON(200, gin.H{"message": "Cleanup completed", "deleted_count": result.RowsAffected})
			})
		}
	}

	// --- BACKGROUND CLEANUP TASK ---
	// Start background task to automatically cleanup denied loans after 24 hours
	go func() {
		ticker := time.NewTicker(1 * time.Hour) // Check every hour
		defer ticker.Stop()

		for range ticker.C {
			oneDayAgo := time.Now().Add(-24 * time.Hour)
			result := db.Where("approval_status = ? AND denied_at < ?", "denied", oneDayAgo).Delete(&Loan{})
			if result.Error != nil {
				log.Printf("Auto-cleanup error: %v", result.Error)
			} else if result.RowsAffected > 0 {
				log.Printf("Auto-cleanup: removed %d denied loans older than 24 hours", result.RowsAffected)
			}
		}
	}()

	// --- START SERVER ---
	log.Println("Starting server on port 8080...")
	router.Run(":8080")
}
