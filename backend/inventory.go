package main

// Hardware inventory that keeps itself up to date. The catalogue grows as
// people borrow ("add it as a new item"), availability comes from open loans
// rather than counts someone has to maintain, gear can be handed straight
// from one person to the next, and overdue loans ask their borrower what
// happened. Maintainers only tidy up: merge duplicates, mark a few valuable
// items as tracked, and answer a monthly "is it still there?" for those.

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"mime/multipart"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	LoanPending  = "pending" // waiting for approval (items that need it)
	LoanActive   = "active"
	LoanReturned = "returned"
	LoanMissing  = "not_found"
	LoanDenied   = "denied" // a request an approver declined; never left the lab
)

const (
	overdueBlockDays = 7                   // a loan this many days overdue pauses borrowing and booking
	verifyEvery      = 30 * 24 * time.Hour // how often a tracked item's custodian is asked about it
	handoverDays     = 7                   // a handed-over loan is due this long after the hand-over, at least
	itemLockKey      = 7_311_002           // with hashtext(name_key): serialises borrows of one item
)

// httpError carries a status and message out of a transaction.
type httpError struct {
	code  int
	msg   string
	extra gin.H
}

func (e httpError) Error() string { return e.msg }

// itemKey is how item names are compared: case, spaces and punctuation are
// ignored, so "RealSense D435i" and "realsense-d435i" are the same item.
func itemKey(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// borrowRequest is the validated text part of a borrow form. Who is
// borrowing comes from the account, not the form.
type borrowRequest struct {
	ItemID             uint
	ItemName           string
	LabLocation        string
	Quantity           int
	ExpectedReturnDate string
	Purpose            string
	Accept             bool // took responsibility for a tracked item
	ConfirmOver        bool // borrowing more than the lab is known to have
}

func parseBorrowForm(values map[string][]string) (borrowRequest, error) {
	get := func(key string) string {
		if v := values[key]; len(v) > 0 {
			return strings.TrimSpace(v[0])
		}
		return ""
	}

	req := borrowRequest{
		ItemName:           get("item_name"),
		LabLocation:        get("lab_location"),
		ExpectedReturnDate: get("expected_return_date"),
		Purpose:            get("purpose"),
		Accept:             get("accept") == "true",
		ConfirmOver:        get("confirm_over") == "true",
	}
	if id := get("item_id"); id != "" {
		n, err := strconv.ParseUint(id, 10, 32)
		if err != nil || n == 0 {
			return req, errors.New("Invalid item")
		}
		req.ItemID = uint(n)
	} else if itemKey(req.ItemName) == "" || req.LabLocation == "" {
		return req, errors.New("Pick an item, or name a new one and say which lab it lives in")
	}
	if len(req.ItemName) > 100 {
		return req, errors.New("Item name is too long")
	}
	if req.ExpectedReturnDate == "" {
		return req, errors.New("Return date is required")
	}
	due, ok := parseReturnDate(req.ExpectedReturnDate)
	if !ok {
		return req, errors.New("Invalid return date")
	}
	// A far-off date would opt the loan out of reminders and the overdue pause.
	if due.After(time.Now().AddDate(1, 0, 0)) {
		return req, errors.New("Return date can be at most a year away")
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

// userBlock returns why userID may not borrow or book right now, or "".
// Both rules lift on their own once the cause is dealt with.
func userBlock(db *gorm.DB, userID uint, now time.Time) string {
	var loans []Loan
	db.Where("user_id = ? AND status = ?", userID, LoanActive).Find(&loans)
	for _, l := range loans {
		if overdue, days := loanOverdue(l, now); overdue && days >= overdueBlockDays {
			return fmt.Sprintf("%s is %d days overdue. Answer the overdue notice on your account page (still using / returned / lost) to carry on.", l.ItemName, days)
		}
	}
	return bookingBlock(db, userID, now)
}

// anonBorrowLimits caps borrows without an account per client IP, so the open
// form cannot be used to flood the loan list.
var anonBorrowLimits = newLoginLimiter()

// anonymousBorrower reads and checks the name and phone a borrow without an
// account is made with. The phone is what they return the item with.
func anonymousBorrower(values map[string][]string) (name, phone string, err error) {
	get := func(k string) string {
		if v := values[k]; len(v) > 0 {
			return v[0]
		}
		return ""
	}
	name = strings.TrimSpace(get("borrower_name"))
	if name == "" || len(name) > 100 {
		return "", "", errors.New("Enter your name")
	}
	if phone = normalizePhone(get("borrower_phone")); len(phone) != 10 {
		return "", "", errors.New("Enter a 10-digit phone number")
	}
	return name, phone, nil
}

// phoneBlock is userBlock for borrowers without an account: a loan made with
// this phone number that is long overdue pauses borrowing until it is back.
func phoneBlock(db *gorm.DB, phone string, now time.Time) string {
	var loans []Loan
	db.Where("user_id IS NULL AND status = ?", LoanActive).Find(&loans)
	for _, l := range loans {
		if !phonesMatch(l.BorrowerPhone, phone) {
			continue
		}
		if overdue, days := loanOverdue(l, now); overdue && days >= overdueBlockDays {
			return fmt.Sprintf("%s borrowed with this phone number is %d days overdue. Please return it first.", l.ItemName, days)
		}
	}
	return ""
}

// itemView is an item as the catalogue shows it: with who has it now.
type itemView struct {
	Item
	Out []PublicLoan `json:"out"`
}

// setupInventory fills in the comparison key for items created before it
// existed.
func setupInventory(db *gorm.DB) error {
	var items []Item
	if err := db.Where("name_key = '' OR name_key IS NULL").Find(&items).Error; err != nil {
		return err
	}
	for _, it := range items {
		if err := db.Model(&it).Update("name_key", itemKey(it.Name)).Error; err != nil {
			return err
		}
	}

	// Link loans from before the catalogue to an item, creating it from the
	// loan's name, so the catalogue starts with everything people have
	// actually borrowed and availability counts the old loans too.
	var loans []Loan
	if err := db.Where("item_id IS NULL").Order("created_at").Find(&loans).Error; err != nil {
		return err
	}
	ids := map[string]uint{}
	for _, l := range loans {
		key := itemKey(l.ItemName)
		if key == "" {
			continue
		}
		id, ok := ids[key]
		if !ok {
			var item Item
			err := db.Where("name_key = ?", key).First(&item).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				item = Item{Name: strings.TrimSpace(l.ItemName), NameKey: key, HomeLab: l.LabLocation}
				err = db.Create(&item).Error
			}
			if err != nil {
				return err
			}
			id, ids[key] = item.ID, item.ID
		}
		if err := db.Model(&Loan{}).Where("id = ?", l.ID).Update("item_id", id).Error; err != nil {
			return err
		}
	}
	if len(loans) > 0 {
		log.Printf("Linked %d old loans to %d catalogue items", len(loans), len(ids))
	}
	return nil
}

// savePhoto stores an optional borrow photo and returns its file name.
func savePhoto(c *gin.Context, files []*multipart.FileHeader) (string, error) {
	if len(files) == 0 {
		return "", nil
	}
	ext, err := validateImageFile(files[0])
	if err != nil {
		return "", fmt.Errorf("Invalid image file: %w", err)
	}
	// The random suffix keeps two uploads in the same second apart.
	suffix := make([]byte, 6)
	if _, err := rand.Read(suffix); err != nil {
		return "", errors.New("Failed to store photo")
	}
	name := fmt.Sprintf("%d-%s%s", time.Now().Unix(), hex.EncodeToString(suffix), ext)
	if err := c.SaveUploadedFile(files[0], "./uploads/"+name); err != nil {
		log.Printf("borrow: saving photo: %v", err)
		return "", errors.New("Failed to save photo")
	}
	return name, nil
}

// errHandoverPhone means the caller did not give the current holder's phone.
var errHandoverPhone = errors.New("handover phone mismatch")

// loanUserError is a refusal meant to be read by the person who asked, so it
// goes back in the response as written. Anything else is logged instead,
// because a database or lock error says nothing useful to them.
type loanUserError string

func (e loanUserError) Error() string { return string(e) }

// checkHandover decides whether me may take previous over. The phone has to be
// the current holder's, as for a return; staff who manage the inventory may
// move a loan without it.
func checkHandover(me authUser, phone string, previous Loan) error {
	if previous.UserID != nil && *previous.UserID == me.ID {
		return loanUserError("This is already yours")
	}
	if !me.can(PermManageInventory) && !phonesMatch(phone, previous.BorrowerPhone) {
		return errHandoverPhone
	}
	return nil
}

func registerInventoryRoutes(api *gin.RouterGroup, db *gorm.DB, sessions *sessionStore, requireLogin gin.HandlerFunc) {
	api = api.Group("", limitBody(defaultBodyLimit))

	// The catalogue, with who has what right now. Public, like the loans list.
	api.GET("/items", func(c *gin.Context) {
		var items []Item
		if err := db.Order("name").Find(&items).Error; err != nil {
			c.JSON(500, gin.H{"error": "Failed to retrieve items"})
			return
		}
		var loans []Loan
		db.Where("item_id IS NOT NULL AND status IN ?", []string{LoanActive, LoanPending}).Order("created_at").Find(&loans)
		out := map[uint][]PublicLoan{}
		for _, pl := range toPublicLoans(loans) {
			out[*pl.ItemID] = append(out[*pl.ItemID], pl)
		}
		views := make([]itemView, 0, len(items))
		for _, it := range items {
			views = append(views, itemView{Item: it, Out: append([]PublicLoan{}, out[it.ID]...)})
		}
		c.JSON(200, views)
	})

	user := api.Group("", requireLogin)

	// Borrow. Anyone may borrow, as before accounts existed: without an
	// account the borrower gives a name and phone number, and returns with
	// that phone number. With an account, those come from the account.
	api.POST("/borrow", func(c *gin.Context) {
		me, loggedIn := requestUser(db, sessions, c)
		if loggedIn && !me.can(PermBorrowItems) {
			c.JSON(403, gin.H{"error": "Your account is not allowed to borrow equipment"})
			return
		}
		if !loggedIn {
			ip, now := c.ClientIP(), time.Now()
			if anonBorrowLimits.blocked(ip, now) {
				c.JSON(429, gin.H{"error": "Too many borrows from this network. Try again in a few minutes."})
				return
			}
			anonBorrowLimits.fail(ip, now)
		}
		form, err := c.MultipartForm()
		if err != nil {
			if bodyTooLarge(err) {
				c.JSON(413, gin.H{"error": "Upload is too large. Photos can be up to 10MB."})
				return
			}
			c.JSON(400, gin.H{"error": "Invalid form data"})
			return
		}
		req, err := parseBorrowForm(form.Value)
		if err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		borrowerName, borrowerPhone := me.Name, me.Phone
		if loggedIn {
			if reason := userBlock(db, me.ID, time.Now()); reason != "" {
				c.JSON(403, gin.H{"error": reason})
				return
			}
		} else {
			borrowerName, borrowerPhone, err = anonymousBorrower(form.Value)
			if err != nil {
				c.JSON(400, gin.H{"error": err.Error()})
				return
			}
			if reason := phoneBlock(db, borrowerPhone, time.Now()); reason != "" {
				c.JSON(403, gin.H{"error": reason})
				return
			}
		}

		// Borrows of one item take turns (locked on its name), so two people
		// can't both take the last one or both add the same new item.
		key := itemKey(req.ItemName)
		if req.ItemID != 0 {
			var known Item
			if err := db.First(&known, req.ItemID).Error; err != nil {
				c.JSON(404, gin.H{"error": "That item no longer exists"})
				return
			}
			key = known.NameKey
		}
		photo, err := savePhoto(c, form.File["item_photo"])
		if err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}

		var item Item
		var loan Loan
		err = db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Exec("SELECT pg_advisory_xact_lock(?::int, hashtext(?))", itemLockKey, key).Error; err != nil {
				return err
			}
			// Find the item, or add it to the catalogue: the catalogue is
			// built by the people using it.
			if req.ItemID != 0 {
				if err := tx.First(&item, req.ItemID).Error; err != nil {
					return httpError{404, "That item no longer exists", nil}
				}
			} else if err := tx.Where("name_key = ?", key).First(&item).Error; err != nil {
				if !errors.Is(err, gorm.ErrRecordNotFound) {
					return err
				}
				item = Item{Name: req.ItemName, NameKey: key, HomeLab: req.LabLocation}
				if err := tx.Create(&item).Error; err != nil {
					return err
				}
			}
			if item.Tracked && !req.Accept {
				return httpError{400, item.Name + " is a tracked item: tick that you take responsibility for it", nil}
			}
			if item.TotalQuantity > 0 && !req.ConfirmOver {
				var out int64
				tx.Model(&Loan{}).Where("item_id = ? AND status IN ?", item.ID, []string{LoanActive, LoanPending}).
					Select("COALESCE(SUM(quantity_borrowed), 0)").Scan(&out)
				if free := int64(item.TotalQuantity) - out; int64(req.Quantity) > free {
					return httpError{409, fmt.Sprintf("The lab should only have %d of %s free. Borrow anyway?", max(free, 0), item.Name), gin.H{"confirm_over": true}}
				}
			}
			lab := item.HomeLab
			if lab == "" {
				lab = req.LabLocation
			}
			status := LoanActive
			if item.NeedsApproval && !me.can(PermAutoApprove) {
				// A request needs someone to follow up on it, which needs
				// an account to notify.
				if !loggedIn {
					return httpError{403, item.Name + " needs a lab admin's approval. Log in to request it.", nil}
				}
				status = LoanPending
			}
			iid := item.ID
			var uid *uint
			if loggedIn {
				uid = &me.ID
			}
			loan = Loan{
				ItemID: &iid, UserID: uid, BorrowerName: borrowerName, BorrowerPhone: borrowerPhone,
				ItemName: item.Name, LabLocation: lab, QuantityBorrowed: req.Quantity,
				ExpectedReturnDate: req.ExpectedReturnDate, Purpose: req.Purpose, PhotoFilename: photo,
				Status: status, ApprovalStatus: "approved",
			}
			if req.Accept {
				now := time.Now()
				loan.AcceptedAt = &now
			}
			return tx.Create(&loan).Error
		})
		if err != nil {
			if photo != "" {
				os.Remove("./uploads/" + photo)
			}
			var he httpError
			if errors.As(err, &he) {
				body := gin.H{"error": he.msg}
				for k, v := range he.extra {
					body[k] = v
				}
				c.JSON(he.code, body)
				return
			}
			log.Printf("borrow: %v", err)
			c.JSON(500, gin.H{"error": "Failed to process borrow request"})
			return
		}
		status := loan.Status
		if status == LoanPending {
			notify(db, usersWithPerm(db, PermApproveLoans), fmt.Sprintf("%s asked to borrow %s", borrowerName, item.Name), "/items")
			c.JSON(200, gin.H{"message": item.Name + " needs approval - you'll get a notification once a lab admin approves it.", "loan_id": loan.ID})
			return
		}
		c.JSON(200, gin.H{"message": "Borrowed! Please return it by the expected date.", "loan_id": loan.ID})
	})

	// Return. Loans made with an account are returned by that account (or an
	// inventory manager); loans from before accounts still use the phone
	// number they were made with.
	api.POST("/return/:id", func(c *gin.Context) {
		var req struct {
			Phone string `json:"phone"`
		}
		c.ShouldBindJSON(&req)
		me, loggedIn := requestUser(db, sessions, c)

		err := db.Transaction(func(tx *gorm.DB) error {
			var loan Loan
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&loan, "id = ?", c.Param("id")).Error; err != nil {
				return err
			}
			if loan.UserID != nil {
				if !loggedIn {
					return errReturnLogin
				}
				if *loan.UserID != me.ID && !me.can(PermManageInventory) {
					return errReturnNotYours
				}
				if loan.Status != LoanActive && loan.Status != LoanPending {
					return checkReturn(loan, "")
				}
			} else if err := checkReturn(loan, req.Phone); err != nil {
				return err
			}
			return markReturned(tx, &loan)
		})
		switch {
		case err == nil:
			c.JSON(200, gin.H{"message": "Item marked as returned. Thank you!"})
		case errors.Is(err, gorm.ErrRecordNotFound):
			c.JSON(404, gin.H{"error": "Loan not found"})
		case errors.Is(err, errReturnLogin):
			c.JSON(401, gin.H{"error": err.Error()})
		case errors.Is(err, errReturnPhoneMismatch), errors.Is(err, errReturnNotYours):
			c.JSON(403, gin.H{"error": err.Error()})
		case errors.Is(err, errReturnAlreadyDone), errors.Is(err, errReturnNotActive):
			c.JSON(409, gin.H{"error": err.Error()})
		default:
			log.Printf("return %s: %v", c.Param("id"), err)
			c.JSON(500, gin.H{"error": "Failed to return item"})
		}
	})

	user.GET("/loans/mine", func(c *gin.Context) {
		var loans []Loan
		db.Where("user_id = ? AND (status IN ? OR (status = ? AND returned_at > ?))", currentUser(c).ID,
			[]string{LoanActive, LoanPending, LoanMissing}, LoanReturned, time.Now().AddDate(0, 0, -30)).
			Order("CASE WHEN status = 'returned' THEN 1 ELSE 0 END, expected_return_date").Find(&loans)
		views := toPublicLoans(loans)
		now := time.Now()
		for i := range views {
			views[i].Overdue, views[i].DaysOverdue = loanOverdue(loans[i], now)
		}
		c.JSON(200, views)
	})

	// "I have this now": the gear went from one person to the next without
	// going back to the shelf. Their loan closes and one opens for the caller.
	//
	// The caller must give the current holder's phone number, the same proof a
	// return asks for - otherwise any account could close anyone's loan and
	// claim the item. Staff who manage the inventory can move loans without it.
	user.POST("/loans/:id/handover", requirePerm(PermBorrowItems), func(c *gin.Context) {
		var req struct {
			Accept bool   `json:"accept"`
			Phone  string `json:"phone"`
		}
		c.ShouldBindJSON(&req)
		me := currentUser(c)
		if reason := userBlock(db, me.ID, time.Now()); reason != "" {
			c.JSON(403, gin.H{"error": reason})
			return
		}
		var previous Loan
		var mine Loan
		err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&previous, "id = ?", c.Param("id")).Error; err != nil {
				return err
			}
			if previous.Status != LoanActive {
				return errReturnNotActive
			}
			if err := checkHandover(me, req.Phone, previous); err != nil {
				return err
			}
			if previous.ItemID != nil {
				var item Item
				if tx.First(&item, *previous.ItemID).Error == nil {
					if item.NeedsApproval && !me.can(PermAutoApprove) {
						return loanUserError(item.Name + " needs approval - borrow it instead so a lab admin can approve it")
					}
					if item.Tracked && !req.Accept {
						return loanUserError(item.Name + " is a tracked item: confirm you take responsibility for it")
					}
				}
			}
			now := time.Now()
			due := now.AddDate(0, 0, handoverDays).Format("2006-01-02")
			if d, ok := parseReturnDate(previous.ExpectedReturnDate); ok && d.After(now.AddDate(0, 0, handoverDays)) {
				due = previous.ExpectedReturnDate
			}
			previous.Purpose = strings.TrimSpace(previous.Purpose + " · handed to " + me.Name)
			if err := markReturned(tx, &previous); err != nil {
				return err
			}
			uid := me.ID
			mine = Loan{
				ItemID: previous.ItemID, UserID: &uid, BorrowerName: me.Name, BorrowerPhone: me.Phone,
				ItemName: previous.ItemName, LabLocation: previous.LabLocation, QuantityBorrowed: previous.QuantityBorrowed,
				ExpectedReturnDate: due, Purpose: "Handed over from " + previous.BorrowerName,
				Status: LoanActive, ApprovalStatus: "approved", AcceptedAt: &now,
			}
			return tx.Create(&mine).Error
		})
		switch {
		case err == nil:
		case errors.Is(err, gorm.ErrRecordNotFound):
			c.JSON(404, gin.H{"error": "Loan not found"})
			return
		case errors.Is(err, errReturnNotActive):
			c.JSON(409, gin.H{"error": "That loan is no longer open"})
			return
		case errors.Is(err, errHandoverPhone):
			c.JSON(403, gin.H{"error": "That phone number does not match the person who has it"})
			return
		default:
			var userErr loanUserError
			if errors.As(err, &userErr) {
				c.JSON(400, gin.H{"error": string(userErr)})
				return
			}
			log.Printf("handover of loan %s failed: %v", c.Param("id"), err)
			c.JSON(500, gin.H{"error": "Could not hand the loan over"})
			return
		}
		if previous.UserID != nil {
			notify(db, []uint{*previous.UserID}, fmt.Sprintf("%s now has the %s you borrowed - it's off your account", me.Name, previous.ItemName), "/account")
		}
		c.JSON(200, gin.H{"message": "It's on your account now, due " + mine.ExpectedReturnDate})
	})

	// The borrower's answer to an overdue notice.
	user.POST("/loans/:id/overdue-answer", func(c *gin.Context) {
		var req struct {
			Answer string `json:"answer"` // extend, returned, lost
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, gin.H{"error": "Invalid data"})
			return
		}
		me := currentUser(c)
		var loan Loan
		if err := db.Where("user_id = ?", me.ID).First(&loan, "id = ?", c.Param("id")).Error; err != nil {
			c.JSON(404, gin.H{"error": "Loan not found"})
			return
		}
		if loan.Status != LoanActive {
			c.JSON(409, gin.H{"error": "That loan is no longer open"})
			return
		}
		var err error
		msg := ""
		switch req.Answer {
		case "extend":
			if overdue, _ := loanOverdue(loan, time.Now()); !overdue {
				c.JSON(400, gin.H{"error": "Only an overdue loan can be extended here"})
				return
			}
			// From today, not from the old date, so an old loan does not stay overdue.
			due := time.Now().AddDate(0, 0, 7).Format("2006-01-02")
			err = db.Model(&loan).Updates(map[string]interface{}{"expected_return_date": due, "due_reminder_sent": false, "overdue_reminder_sent": false}).Error
			msg = "Extended to " + due
		case "returned":
			err = db.Transaction(func(tx *gorm.DB) error {
				// Re-read under lock so a concurrent hand-over is not overwritten.
				if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&loan, loan.ID).Error; err != nil {
					return err
				}
				if loan.Status != LoanActive {
					return errReturnNotActive
				}
				return markReturned(tx, &loan)
			})
			msg = "Marked as returned"
		case "lost":
			upd := db.Model(&Loan{}).Where("id = ? AND status = ?", loan.ID, LoanActive).
				Updates(map[string]interface{}{"status": LoanMissing, "approved_by": me.Name + " (reported lost)"})
			err = upd.Error
			if err == nil && upd.RowsAffected == 0 {
				c.JSON(409, gin.H{"error": "That loan is no longer open"})
				return
			}
			msg = "Thanks for saying - it's marked missing"
			notify(db, usersWithPerm(db, PermManageInventory), fmt.Sprintf("%s reported %s as lost", me.Name, loan.ItemName), "/admin?tab=missing")
		default:
			c.JSON(400, gin.H{"error": "Answer extend, returned or lost"})
			return
		}
		if err != nil {
			c.JSON(500, gin.H{"error": "Failed to save"})
			return
		}
		c.JSON(200, gin.H{"message": msg})
	})

	// Anyone can flag a problem with an item (or clear it once fixed).
	user.POST("/items/:id/issue", func(c *gin.Context) {
		var req struct {
			Issue string `json:"issue"`
		}
		if err := c.ShouldBindJSON(&req); err != nil || len(req.Issue) > 300 {
			c.JSON(400, gin.H{"error": "Describe the problem in under 300 characters"})
			return
		}
		issue := strings.TrimSpace(req.Issue)
		by := ""
		if issue != "" {
			by = currentUser(c).Name
		}
		res := db.Model(&Item{}).Where("id = ?", c.Param("id")).Updates(map[string]interface{}{"issue": issue, "issue_reported_by": by})
		if res.Error != nil || res.RowsAffected == 0 {
			c.JSON(404, gin.H{"error": "Item not found"})
			return
		}
		c.JSON(200, gin.H{"message": "Saved"})
	})

	// The custodian's answer to "is this tracked item where the system says?"
	user.POST("/items/:id/verify", func(c *gin.Context) {
		var req struct {
			Answer string `json:"answer"` // yes, no
		}
		c.ShouldBindJSON(&req)
		me := currentUser(c)
		var item Item
		if err := db.First(&item, "id = ?", c.Param("id")).Error; err != nil {
			c.JSON(404, gin.H{"error": "Item not found"})
			return
		}
		if (item.CustodianID == nil || *item.CustodianID != me.ID) && !me.can(PermManageInventory) {
			c.JSON(403, gin.H{"error": "Only the item's custodian can confirm it"})
			return
		}
		switch req.Answer {
		case "yes":
			db.Model(&item).Update("last_verified_at", time.Now())
		case "no":
			notify(db, usersWithPerm(db, PermManageInventory), fmt.Sprintf("%s could not find %s where the system says it is", me.Name, item.Name), "/admin?tab=items")
		default:
			c.JSON(400, gin.H{"error": "Answer yes or no"})
			return
		}
		c.JSON(200, gin.H{"message": "Thanks"})
	})

	// Tracked items the caller looks after.
	user.GET("/items/custodian", func(c *gin.Context) {
		var items []Item
		db.Where("custodian_id = ?", currentUser(c).ID).Order("name").Find(&items)
		c.JSON(200, items)
	})

	// --- LOAN APPROVALS (items that need them) ---

	approver := user.Group("", requirePerm(PermApproveLoans))

	approver.GET("/loans/pending", func(c *gin.Context) {
		var loans []Loan
		db.Where("status = ?", LoanPending).Order("created_at").Find(&loans)
		c.JSON(200, toPublicLoans(loans))
	})

	decideLoan := func(c *gin.Context, decision string) {
		var loan Loan
		if err := db.First(&loan, "id = ?", c.Param("id")).Error; err != nil || loan.Status != LoanPending {
			c.JSON(404, gin.H{"error": "No such request"})
			return
		}
		me := currentUser(c)
		if decision == "approve" && ownedBy(loan.UserID, me.ID) {
			c.JSON(403, gin.H{"error": "Someone else has to approve your own request"})
			return
		}
		now := time.Now()
		updates := map[string]interface{}{"status": LoanActive, "approved_by": me.Name, "approved_at": now}
		text := fmt.Sprintf("%s approved your request for %s - go ahead", me.Name, loan.ItemName)
		if decision == "reject" {
			updates = map[string]interface{}{"status": LoanDenied, "approval_status": "denied", "approved_by": me.Name, "denied_at": now}
			text = fmt.Sprintf("%s declined your request for %s", me.Name, loan.ItemName)
		}
		// Still pending? The borrower may have withdrawn it, or another
		// approver got there first.
		upd := db.Model(&Loan{}).Where("id = ? AND status = ?", loan.ID, LoanPending).Updates(updates)
		if upd.Error != nil {
			c.JSON(500, gin.H{"error": "Failed to save"})
			return
		}
		if upd.RowsAffected == 0 {
			c.JSON(409, gin.H{"error": "That request was already handled or withdrawn"})
			return
		}
		if loan.UserID != nil {
			notify(db, []uint{*loan.UserID}, text, "/account")
		}
		c.JSON(200, gin.H{"message": "Done"})
	}
	approver.POST("/loans/:id/approve", func(c *gin.Context) { decideLoan(c, "approve") })
	approver.POST("/loans/:id/reject", func(c *gin.Context) { decideLoan(c, "reject") })

	// --- CATALOGUE UPKEEP (manage_inventory) ---

	manage := api.Group("/admin/items", requireLogin, requirePerm(PermManageInventory))

	manage.PUT("/:id", func(c *gin.Context) {
		var req struct {
			Name          string `json:"name"`
			HomeLab       string `json:"home_lab"`
			TotalQuantity int    `json:"total_quantity"`
			Tracked       bool   `json:"tracked"`
			CustodianID   *uint  `json:"custodian_id"`
			LabOnly       bool   `json:"lab_only"`
			NeedsApproval bool   `json:"needs_approval"`
			Notes         string `json:"notes"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, gin.H{"error": "Invalid data"})
			return
		}
		req.Name = strings.TrimSpace(req.Name)
		if itemKey(req.Name) == "" || len(req.Name) > 100 || req.TotalQuantity < 0 || len(req.Notes) > 500 {
			c.JSON(400, gin.H{"error": "Give a name, and a quantity of zero (unknown) or more"})
			return
		}
		var item Item
		if err := db.First(&item, "id = ?", c.Param("id")).Error; err != nil {
			c.JSON(404, gin.H{"error": "Item not found"})
			return
		}
		var clash int64
		db.Model(&Item{}).Where("name_key = ? AND id <> ?", itemKey(req.Name), item.ID).Count(&clash)
		if clash > 0 {
			c.JSON(409, gin.H{"error": "Another item already has that name - merge them instead"})
			return
		}
		err := db.Model(&item).Updates(map[string]interface{}{
			"name": req.Name, "name_key": itemKey(req.Name), "home_lab": strings.TrimSpace(req.HomeLab),
			"total_quantity": req.TotalQuantity, "tracked": req.Tracked, "custodian_id": req.CustodianID,
			"lab_only": req.LabOnly, "needs_approval": req.NeedsApproval, "notes": strings.TrimSpace(req.Notes),
		}).Error
		if err != nil {
			c.JSON(500, gin.H{"error": "Failed to save"})
			return
		}
		// Loans keep the item's name for history and search.
		db.Model(&Loan{}).Where("item_id = ?", item.ID).Update("item_name", req.Name)
		c.JSON(200, gin.H{"message": "Saved"})
	})

	// Fold a duplicate into another item: its loans move over and it goes.
	manage.POST("/:id/merge", func(c *gin.Context) {
		var req struct {
			IntoID uint `json:"into_id"`
		}
		if err := c.ShouldBindJSON(&req); err != nil || req.IntoID == 0 {
			c.JSON(400, gin.H{"error": "Pick an item to merge into"})
			return
		}
		var from, into Item
		if db.First(&from, "id = ?", c.Param("id")).Error != nil || db.First(&into, req.IntoID).Error != nil {
			c.JSON(404, gin.H{"error": "Item not found"})
			return
		}
		if from.ID == into.ID {
			c.JSON(400, gin.H{"error": "Pick a different item to merge into"})
			return
		}
		err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Model(&Loan{}).Where("item_id = ?", from.ID).
				Updates(map[string]interface{}{"item_id": into.ID, "item_name": into.Name}).Error; err != nil {
				return err
			}
			return tx.Delete(&from).Error
		})
		if err != nil {
			c.JSON(500, gin.H{"error": "Failed to merge"})
			return
		}
		c.JSON(200, gin.H{"message": fmt.Sprintf("Merged %s into %s", from.Name, into.Name)})
	})

	manage.DELETE("/:id", func(c *gin.Context) {
		var open int64
		db.Model(&Loan{}).Where("item_id = ? AND status IN ?", c.Param("id"), []string{LoanActive, LoanPending}).Count(&open)
		if open > 0 {
			c.JSON(409, gin.H{"error": "Someone still has this item - it can be deleted once it's back"})
			return
		}
		if err := db.Delete(&Item{}, "id = ?", c.Param("id")).Error; err != nil {
			c.JSON(500, gin.H{"error": "Failed to delete"})
			return
		}
		c.JSON(200, gin.H{"message": "Deleted"})
	})
}

var (
	errReturnLogin    = errors.New("log in to return this - it was borrowed with an account")
	errReturnNotYours = errors.New("only the person who borrowed this can return it")
)

func markReturned(tx *gorm.DB, loan *Loan) error {
	now := time.Now()
	loan.Status = LoanReturned
	loan.ReturnRequested = true
	loan.ReturnApprovalStatus = "approved"
	loan.ReturnRequestedAt = &now
	loan.ReturnedAt = &now
	return tx.Save(loan).Error
}

// runReminders sends the in-app reminders once an hour: loans due tomorrow,
// loans that just became overdue, and tracked items whose custodian has not
// confirmed them for a month. Each reminder is sent once.
func runReminders(db *gorm.DB) {
	for {
		sendReminders(db, time.Now())
		time.Sleep(time.Hour)
	}
}

func sendReminders(db *gorm.DB, now time.Time) {
	var loans []Loan
	db.Where("user_id IS NOT NULL AND status = ? AND (due_reminder_sent = ? OR overdue_reminder_sent = ?)", LoanActive, false, false).Find(&loans)
	tomorrow := now.AddDate(0, 0, 1).Format("2006-01-02")
	for _, l := range loans {
		if overdue, _ := loanOverdue(l, now); overdue && !l.OverdueReminderSent {
			notify(db, []uint{*l.UserID}, fmt.Sprintf("%s is overdue. Tell us what happened: still using it, returned, or lost?", l.ItemName), "/account")
			db.Model(&l).Updates(map[string]interface{}{"overdue_reminder_sent": true, "due_reminder_sent": true})
		} else if !l.DueReminderSent && strings.HasPrefix(l.ExpectedReturnDate, tomorrow) {
			notify(db, []uint{*l.UserID}, fmt.Sprintf("%s is due back tomorrow", l.ItemName), "/account")
			db.Model(&l).Update("due_reminder_sent", true)
		}
	}

	var items []Item
	db.Where("tracked = ? AND custodian_id IS NOT NULL AND (last_verified_at IS NULL OR last_verified_at < ?) AND (verify_asked_at IS NULL OR verify_asked_at < ?)",
		true, now.Add(-verifyEvery), now.Add(-verifyEvery)).Find(&items)
	for _, it := range items {
		notify(db, []uint{*it.CustodianID}, fmt.Sprintf("Monthly check: is %s where the system says it is?", it.Name), "/account")
		db.Model(&it).Update("verify_asked_at", now)
	}
}

// requestUser is the logged-in user if the request carries a valid session,
// for routes that also work without one.
func requestUser(db *gorm.DB, sessions *sessionStore, c *gin.Context) (authUser, bool) {
	id, ok := sessions.lookup(bearerToken(c))
	var u User
	if !ok || db.First(&u, id).Error != nil || u.Status != UserActive {
		return authUser{}, false
	}
	return loadAuthUser(db, u), true
}
