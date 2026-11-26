package main

// Accounts for everyone who uses the lab - students, interns, profs and the
// server admins. Each user is in one group whose permissions are the
// defaults; per-user overrides grant or revoke on top. New accounts wait for
// a server admin to approve them.

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"os"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// --- PERMISSIONS ---

const (
	PermBorrowItems         = "borrow_items"
	PermBookMocap           = "book_mocap"
	PermBookMech            = "book_mech"
	PermApproveMechBookings = "approve_mech_bookings"
	PermApproveLoans        = "approve_loans"
	PermAutoApprove         = "auto_approve" // own bookings and loans need no sign-off
	PermCreateThreads       = "create_threads"
	PermUploadPrints        = "upload_prints"
	PermManageInventory     = "manage_inventory"
	PermManagePrinters      = "manage_printers"
	PermManageBookings      = "manage_bookings"
	PermManageUsers         = "manage_users"
)

var allPerms = []string{
	PermBorrowItems, PermBookMocap, PermBookMech, PermApproveMechBookings, PermApproveLoans, PermAutoApprove, PermCreateThreads, PermUploadPrints,
	PermManageInventory, PermManagePrinters, PermManageBookings, PermManageUsers,
}

func validPerm(p string) bool {
	return slices.Contains(allPerms, p)
}

// effectivePerms is the group's permissions plus the user's grants, minus
// the user's revokes.
func effectivePerms(groupPerms []string, overrides []UserPermission) map[string]bool {
	perms := make(map[string]bool, len(groupPerms)+len(overrides))
	for _, p := range groupPerms {
		perms[p] = true
	}
	for _, o := range overrides {
		if o.Allow {
			perms[o.Perm] = true
		} else {
			delete(perms, o.Perm)
		}
	}
	return perms
}

// --- MODELS ---

const (
	UserPending  = "pending"
	UserActive   = "active"
	UserDisabled = "disabled"
)

type User struct {
	gorm.Model
	Name     string `json:"name"`
	Email    string `json:"email" gorm:"uniqueIndex"`
	Phone    string `json:"phone"`
	Password string `json:"-"`
	GroupID  uint   `json:"group_id"`
	Status   string `json:"status" gorm:"default:'pending'"`
}

type Group struct {
	gorm.Model
	Name string `json:"name" gorm:"uniqueIndex"`
}

type GroupPermission struct {
	ID      uint
	GroupID uint   `gorm:"uniqueIndex:idx_group_perm"`
	Perm    string `gorm:"uniqueIndex:idx_group_perm"`
}

type UserPermission struct {
	ID     uint   `json:"-"`
	UserID uint   `json:"-" gorm:"uniqueIndex:idx_user_perm"`
	Perm   string `json:"perm" gorm:"uniqueIndex:idx_user_perm"`
	Allow  bool   `json:"allow"`
}

// Session is a login. Only a hash of the token is stored, so a database
// dump does not hand out working logins.
type Session struct {
	ID        uint
	TokenHash string `gorm:"uniqueIndex"`
	UserID    uint   `gorm:"index"`
	ExpiresAt time.Time
}

const serverAdminGroup = "Server Admin"

// labAdminGroup is the designated people who sign off on robot bookings and
// loans and run the lab's hardware. Admins from before accounts who were not
// super admins land here too. Profs do not sign off.
const labAdminGroup = "Lab Admin"

var studentPerms = []string{PermBorrowItems, PermBookMocap, PermBookMech, PermUploadPrints}

var labAdminPerms = append(slices.Clone(studentPerms), PermAutoApprove, PermCreateThreads,
	PermApproveMechBookings, PermApproveLoans, PermManageInventory, PermManagePrinters, PermManageBookings)

// staffPerms are what nobody may give themselves by picking a group at
// sign-up; a server admin moves people into groups that hold them.
var staffPerms = []string{PermApproveMechBookings, PermApproveLoans, PermManageInventory,
	PermManagePrinters, PermManageBookings, PermManageUsers}

// defaultGroups are created on first start; after that the admin UI owns them.
var defaultGroups = []struct {
	Name  string
	Perms []string
}{
	{"Student", studentPerms},
	{"Intern", studentPerms},
	{"Prof", append(slices.Clone(studentPerms), PermAutoApprove, PermCreateThreads)}, // run threads; don't sign off for others
	{labAdminGroup, labAdminPerms},
	{serverAdminGroup, allPerms},
}

func normalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// setupAccounts creates the default groups, carries the old admin accounts
// over as Server Admins, and bootstraps the first one on an empty database.
func setupAccounts(db *gorm.DB) error {
	// Missing default groups are (re)created, so a first start that died
	// halfway finishes the job next time. Existing groups are left alone:
	// after the first start the admin UI owns them.
	for _, g := range defaultGroups {
		if _, err := ensureGroup(db, g.Name, g.Perms); err != nil {
			return err
		}
	}

	var admins Group
	if err := db.Where("name = ?", serverAdminGroup).First(&admins).Error; err != nil {
		return errors.New("the Server Admin group is missing")
	}

	// Old admins log in with their username, so it becomes their email field.
	var legacy []Admin
	db.Find(&legacy)
	for _, a := range legacy {
		login := normalizeEmail(a.Username)
		var n int64
		db.Unscoped().Model(&User{}).Where("email = ?", login).Count(&n)
		if n > 0 {
			continue
		}
		groupID := admins.ID
		if !a.IsSuperAdmin {
			g, err := ensureGroup(db, labAdminGroup, labAdminPerms)
			if err != nil {
				return err
			}
			groupID = g.ID
		}
		u := User{Name: a.Name, Email: login, Password: a.Password, GroupID: groupID, Status: UserActive}
		if err := db.Create(&u).Error; err != nil {
			return err
		}
		log.Printf("Moved admin %q to an account (super admin: %v)", a.Username, a.IsSuperAdmin)
	}

	var users int64
	db.Model(&User{}).Count(&users)
	if users > 0 {
		return nil
	}
	// Credentials come from the environment - never hardcode them, this repo is public.
	login := normalizeEmail(os.Getenv("ADMIN_USERNAME"))
	if login == "" {
		login = "admin"
	}
	password := os.Getenv("ADMIN_PASSWORD")
	generated := password == ""
	if generated {
		buf := make([]byte, 12)
		if _, err := rand.Read(buf); err != nil {
			return err
		}
		password = base64.RawURLEncoding.EncodeToString(buf)
	}
	hash, err := hashPassword(password)
	if err != nil {
		return err
	}
	if err := db.Create(&User{Name: login + " (Server Admin)", Email: login, Password: hash, GroupID: admins.ID, Status: UserActive}).Error; err != nil {
		return err
	}
	log.Printf("Bootstrap server admin created: %s", login)
	if generated {
		log.Printf("ADMIN_PASSWORD was not set. Generated one-time password: %s", password)
		log.Println("Log in and change it immediately.")
	}
	return nil
}

// ensureGroup returns the named group, creating it with perms if missing.
func ensureGroup(db *gorm.DB, name string, perms []string) (Group, error) {
	var g Group
	err := db.Where("name = ?", name).First(&g).Error
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return g, err
	}
	g = Group{Name: name}
	err = db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&g).Error; err != nil {
			return err
		}
		for _, p := range perms {
			if err := tx.Create(&GroupPermission{GroupID: g.ID, Perm: p}).Error; err != nil {
				return err
			}
		}
		return nil
	})
	return g, err
}

// --- SESSIONS ---

// sessionTTL is how long a login lasts without being used; every use within
// it pushes the expiry out again, so regular users stay logged in.
const sessionTTL = 30 * 24 * time.Hour

type sessionStore struct {
	db *gorm.DB
}

func newSessionStore(db *gorm.DB) *sessionStore {
	return &sessionStore{db: db}
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (s *sessionStore) create(userID uint) (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(buf)
	now := time.Now()
	s.db.Where("expires_at < ?", now).Delete(&Session{})
	err := s.db.Create(&Session{TokenHash: hashToken(token), UserID: userID, ExpiresAt: now.Add(sessionTTL)}).Error
	return token, err
}

func (s *sessionStore) lookup(token string) (uint, bool) {
	if token == "" {
		return 0, false
	}
	var sess Session
	now := time.Now()
	if err := s.db.Where("token_hash = ? AND expires_at > ?", hashToken(token), now).First(&sess).Error; err != nil {
		return 0, false
	}
	// Extend at most once a day, not on every request.
	if sess.ExpiresAt.Before(now.Add(sessionTTL - 24*time.Hour)) {
		s.db.Model(&sess).Update("expires_at", now.Add(sessionTTL))
	}
	return sess.UserID, true
}

func (s *sessionStore) revoke(token string) {
	s.db.Where("token_hash = ?", hashToken(token)).Delete(&Session{})
}

// revokeUser ends every session of userID except keep (empty keeps none).
func (s *sessionStore) revokeUser(userID uint, keep string) {
	q := s.db.Where("user_id = ?", userID)
	if keep != "" {
		q = q.Where("token_hash <> ?", hashToken(keep))
	}
	q.Delete(&Session{})
}

// --- REQUEST AUTH ---

// authUser is the logged-in user with their effective permissions.
type authUser struct {
	User
	GroupName string
	Perms     map[string]bool
}

func (u authUser) can(perm string) bool { return u.Perms[perm] }

func (u authUser) json() gin.H {
	perms := make([]string, 0, len(u.Perms))
	for p := range u.Perms {
		perms = append(perms, p)
	}
	sort.Strings(perms)
	return gin.H{
		"id":          u.ID,
		"name":        u.Name,
		"email":       u.Email,
		"phone":       u.Phone,
		"group_id":    u.GroupID,
		"group":       u.GroupName,
		"status":      u.Status,
		"permissions": perms,
	}
}

func loadAuthUser(db *gorm.DB, u User) authUser {
	var group Group
	db.First(&group, u.GroupID)
	var groupPerms []string
	db.Model(&GroupPermission{}).Where("group_id = ?", u.GroupID).Pluck("perm", &groupPerms)
	var overrides []UserPermission
	db.Where("user_id = ?", u.ID).Find(&overrides)
	return authUser{User: u, GroupName: group.Name, Perms: effectivePerms(groupPerms, overrides)}
}

// authMiddleware lets through only requests carrying the session token of an
// active user, and puts that user in the context for currentUser.
func authMiddleware(db *gorm.DB, sessions *sessionStore) gin.HandlerFunc {
	return func(c *gin.Context) {
		u, ok := requestUser(db, sessions, c)
		if !ok {
			c.AbortWithStatusJSON(401, gin.H{"error": "Authentication required"})
			return
		}
		c.Set("user", u)
		c.Next()
	}
}

// requirePerm must run after authMiddleware.
func requirePerm(perm string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !currentUser(c).can(perm) {
			c.AbortWithStatusJSON(403, gin.H{"error": "You don't have permission to do this"})
			return
		}
		c.Next()
	}
}

// requireAnyPerm lets through users holding at least one of perms.
func requireAnyPerm(perms ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		u := currentUser(c)
		if !slices.ContainsFunc(perms, u.can) {
			c.AbortWithStatusJSON(403, gin.H{"error": "You don't have permission to do this"})
			return
		}
		c.Next()
	}
}

func currentUser(c *gin.Context) authUser {
	return c.MustGet("user").(authUser)
}

// --- VALIDATION ---

const maxPasswordBytes = 72 // bcrypt ignores anything past this

func checkNewPassword(p string) error {
	if len(p) < 8 {
		return errors.New("Password must be at least 8 characters long")
	}
	if len(p) > maxPasswordBytes {
		return errors.New("Password must be at most 72 characters long")
	}
	return nil
}

type registerRequest struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Phone    string `json:"phone"`
	Password string `json:"password"`
	GroupID  uint   `json:"group_id"`
}

// validateRegistration checks the fields and normalises email and phone.
func validateRegistration(req *registerRequest) error {
	req.Name = strings.TrimSpace(req.Name)
	req.Email = normalizeEmail(req.Email)
	if req.Name == "" || len(req.Name) > 100 {
		return errors.New("Enter your name")
	}
	if at := strings.Index(req.Email, "@"); at < 1 || at == len(req.Email)-1 || len(req.Email) > 254 {
		return errors.New("Enter a valid email address")
	}
	if req.Phone = normalizePhone(req.Phone); len(req.Phone) != 10 {
		return errors.New("Enter a 10-digit phone number")
	}
	if req.GroupID == 0 {
		return errors.New("Pick whether you are a student, intern or prof")
	}
	return checkNewPassword(req.Password)
}

// --- ROUTES ---

// selfServiceGroups are the groups someone may ask for when signing up:
// those with no approving or managing permissions.
func selfServiceGroups(db *gorm.DB) []Group {
	var groups []Group
	db.Where("id NOT IN (?)", db.Model(&GroupPermission{}).Select("group_id").Where("perm IN ?", staffPerms)).
		Order("name").Find(&groups)
	return groups
}

// registerLimits caps sign-ups per client IP, with the same window and count
// as failed logins.
var registerLimits = newLoginLimiter()

func registerAccountRoutes(api *gin.RouterGroup, db *gorm.DB, sessions *sessionStore, limits *loginLimiter, requireLogin gin.HandlerFunc) {
	auth := api.Group("/auth", limitBody(defaultBodyLimit))

	auth.GET("/groups", func(c *gin.Context) {
		groups := selfServiceGroups(db)
		out := make([]gin.H, 0, len(groups))
		for _, g := range groups {
			out = append(out, gin.H{"id": g.ID, "name": g.Name})
		}
		c.JSON(200, out)
	})

	auth.POST("/register", func(c *gin.Context) {
		// Every attempt counts, successful or not: each one costs a bcrypt hash
		// and a notification per server admin, so an unlimited form is an easy
		// way to flood the approval queue.
		ip, now := c.ClientIP(), time.Now()
		if registerLimits.blocked(ip, now) {
			c.JSON(429, gin.H{"error": "Too many sign-ups from this network. Try again in a few minutes."})
			return
		}
		registerLimits.fail(ip, now)

		var req registerRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, gin.H{"error": "Invalid sign-up data"})
			return
		}
		if err := validateRegistration(&req); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		if !slices.ContainsFunc(selfServiceGroups(db), func(g Group) bool { return g.ID == req.GroupID }) {
			c.JSON(400, gin.H{"error": "Pick whether you are a student, intern or prof"})
			return
		}
		var n int64
		db.Unscoped().Model(&User{}).Where("email = ?", req.Email).Count(&n)
		if n > 0 {
			c.JSON(409, gin.H{"error": "An account with this email already exists"})
			return
		}
		hash, err := hashPassword(req.Password)
		if err != nil {
			c.JSON(500, gin.H{"error": "Failed to create account"})
			return
		}
		u := User{Name: req.Name, Email: req.Email, Phone: req.Phone, Password: hash, GroupID: req.GroupID, Status: UserPending}
		if err := db.Create(&u).Error; err != nil {
			c.JSON(500, gin.H{"error": "Failed to create account"})
			return
		}
		var group Group
		db.First(&group, req.GroupID)
		notify(db, usersWithPerm(db, PermManageUsers), fmt.Sprintf("%s signed up as %s - approve or reject", req.Name, group.Name), "/admin?tab=users")
		c.JSON(201, gin.H{"message": "Account created. A server admin will approve it soon."})
	})

	auth.POST("/login", func(c *gin.Context) {
		var req struct {
			Email    string `json:"email"`
			Password string `json:"password" binding:"required"`
		}
		ip := c.ClientIP()
		if limits.blocked(ip, time.Now()) {
			c.JSON(429, gin.H{"error": "Too many failed login attempts. Try again in a few minutes."})
			return
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, gin.H{"error": "Invalid login data"})
			return
		}
		var u User
		if err := db.Where("email = ?", normalizeEmail(req.Email)).First(&u).Error; err != nil {
			// Spend the same time as a real check, so response time does not
			// reveal which accounts exist.
			bcrypt.CompareHashAndPassword(dummyPasswordHash, []byte(req.Password))
			limits.fail(ip, time.Now())
			c.JSON(401, gin.H{"error": "Wrong email or password"})
			return
		}
		ok, needsUpgrade := verifyPassword(u.Password, req.Password)
		if !ok {
			limits.fail(ip, time.Now())
			c.JSON(401, gin.H{"error": "Wrong email or password"})
			return
		}
		limits.succeed(ip)
		switch u.Status {
		case UserPending:
			c.JSON(403, gin.H{"error": "Your account is waiting for a server admin to approve it"})
			return
		case UserDisabled:
			c.JSON(403, gin.H{"error": "Your account has been disabled"})
			return
		}
		// Transparently migrate legacy SHA-256 hashes to bcrypt on login.
		if needsUpgrade {
			if newHash, err := hashPassword(req.Password); err == nil {
				db.Model(&u).Update("password", newHash)
			}
		}
		token, err := sessions.create(u.ID)
		if err != nil {
			c.JSON(500, gin.H{"error": "Failed to create session"})
			return
		}
		c.JSON(200, gin.H{"token": token, "user": loadAuthUser(db, u).json()})
	})

	auth.POST("/logout", requireLogin, func(c *gin.Context) {
		sessions.revoke(bearerToken(c))
		c.JSON(200, gin.H{"message": "Logged out"})
	})

	auth.GET("/me", requireLogin, func(c *gin.Context) {
		c.JSON(200, currentUser(c).json())
	})

	auth.POST("/change-password", requireLogin, func(c *gin.Context) {
		var req struct {
			OldPassword string `json:"old_password" binding:"required"`
			NewPassword string `json:"new_password" binding:"required"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, gin.H{"error": "Invalid password change data"})
			return
		}
		if err := checkNewPassword(req.NewPassword); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		u := currentUser(c)
		if ok, _ := verifyPassword(u.Password, req.OldPassword); !ok {
			// Not 401: the session is fine, and the browser drops it on a 401.
			c.JSON(400, gin.H{"error": "Current password is incorrect"})
			return
		}
		hash, err := hashPassword(req.NewPassword)
		if err != nil || db.Model(&User{}).Where("id = ?", u.ID).Update("password", hash).Error != nil {
			c.JSON(500, gin.H{"error": "Failed to update password"})
			return
		}
		// Anyone else signed in with the old password is logged out; this session stays.
		sessions.revokeUser(u.ID, bearerToken(c))
		c.JSON(200, gin.H{"message": "Password changed successfully"})
	})

	// --- USER MANAGEMENT (server admins) ---
	manage := api.Group("/admin", limitBody(defaultBodyLimit), requireLogin, requirePerm(PermManageUsers))

	manage.GET("/users", func(c *gin.Context) {
		var users []User
		if err := db.Order("CASE WHEN status = 'pending' THEN 0 ELSE 1 END, name").Find(&users).Error; err != nil {
			c.JSON(500, gin.H{"error": "Failed to load users"})
			return
		}
		var overrides []UserPermission
		db.Find(&overrides)
		byUser := map[uint][]UserPermission{}
		for _, o := range overrides {
			byUser[o.UserID] = append(byUser[o.UserID], o)
		}
		out := make([]gin.H, 0, len(users))
		for _, u := range users {
			out = append(out, gin.H{
				"id": u.ID, "name": u.Name, "email": u.Email, "phone": u.Phone,
				"group_id": u.GroupID, "status": u.Status, "created_at": u.CreatedAt,
				"overrides": append([]UserPermission{}, byUser[u.ID]...),
			})
		}
		c.JSON(200, out)
	})

	// findOtherUser loads the :id user, refusing the caller's own account so
	// an admin cannot lock themselves out.
	findOtherUser := func(c *gin.Context) (User, bool) {
		var u User
		if err := db.First(&u, "id = ?", c.Param("id")).Error; err != nil {
			c.JSON(404, gin.H{"error": "User not found"})
			return u, false
		}
		if u.ID == currentUser(c).ID {
			c.JSON(400, gin.H{"error": "You can't change your own account here"})
			return u, false
		}
		return u, true
	}

	// Approve, disable or move a user to another group.
	manage.PATCH("/users/:id", func(c *gin.Context) {
		var req struct {
			Status  *string `json:"status"`
			GroupID *uint   `json:"group_id"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, gin.H{"error": "Invalid data"})
			return
		}
		u, ok := findOtherUser(c)
		if !ok {
			return
		}
		updates := map[string]interface{}{}
		if req.Status != nil {
			if !slices.Contains([]string{UserPending, UserActive, UserDisabled}, *req.Status) {
				c.JSON(400, gin.H{"error": "Invalid status"})
				return
			}
			updates["status"] = *req.Status
		}
		if req.GroupID != nil {
			if db.First(&Group{}, *req.GroupID).Error != nil {
				c.JSON(400, gin.H{"error": "No such group"})
				return
			}
			updates["group_id"] = *req.GroupID
		}
		if len(updates) == 0 {
			c.JSON(400, gin.H{"error": "Nothing to change"})
			return
		}
		if err := db.Model(&u).Updates(updates).Error; err != nil {
			c.JSON(500, gin.H{"error": "Failed to update user"})
			return
		}
		if u.Status != UserActive {
			sessions.revokeUser(u.ID, "")
		}
		c.JSON(200, gin.H{"message": "User updated"})
	})

	// Replace a user's overrides, including robot clearances.
	manage.PUT("/users/:id/permissions", func(c *gin.Context) {
		var req struct {
			Overrides []UserPermission `json:"overrides"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, gin.H{"error": "Invalid data"})
			return
		}
		u, ok := findOtherUser(c)
		if !ok {
			return
		}
		seen := map[string]bool{}
		for i := range req.Overrides {
			p := req.Overrides[i].Perm
			if !validPerm(p) || seen[p] {
				c.JSON(400, gin.H{"error": "Invalid permission: " + p})
				return
			}
			seen[p] = true
			req.Overrides[i].ID = 0
			req.Overrides[i].UserID = u.ID
		}
		err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Where("user_id = ?", u.ID).Delete(&UserPermission{}).Error; err != nil {
				return err
			}
			if len(req.Overrides) == 0 {
				return nil
			}
			return tx.Create(&req.Overrides).Error
		})
		if err != nil {
			c.JSON(500, gin.H{"error": "Failed to save permissions"})
			return
		}
		c.JSON(200, gin.H{"message": "Permissions saved"})
	})

	// Set a new password for someone who forgot theirs.
	manage.POST("/users/:id/password", func(c *gin.Context) {
		var req struct {
			Password string `json:"password"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, gin.H{"error": "Invalid data"})
			return
		}
		if err := checkNewPassword(req.Password); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		u, ok := findOtherUser(c)
		if !ok {
			return
		}
		hash, err := hashPassword(req.Password)
		if err != nil || db.Model(&u).Update("password", hash).Error != nil {
			c.JSON(500, gin.H{"error": "Failed to reset password"})
			return
		}
		sessions.revokeUser(u.ID, "")
		c.JSON(200, gin.H{"message": "Password reset"})
	})

	manage.GET("/groups", func(c *gin.Context) {
		var groups []Group
		db.Order("name").Find(&groups)
		var gps []GroupPermission
		db.Find(&gps)
		out := make([]gin.H, 0, len(groups))
		for _, g := range groups {
			perms := []string{}
			for _, gp := range gps {
				if gp.GroupID == g.ID {
					perms = append(perms, gp.Perm)
				}
			}
			sort.Strings(perms)
			out = append(out, gin.H{"id": g.ID, "name": g.Name, "permissions": perms})
		}
		c.JSON(200, gin.H{"groups": out, "all_permissions": allPerms})
	})

	manage.PUT("/groups/:id/permissions", func(c *gin.Context) {
		var req struct {
			Permissions []string `json:"permissions"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, gin.H{"error": "Invalid data"})
			return
		}
		var g Group
		if err := db.First(&g, "id = ?", c.Param("id")).Error; err != nil {
			c.JSON(404, gin.H{"error": "Group not found"})
			return
		}
		perms := slices.Compact(slices.Sorted(slices.Values(req.Permissions)))
		for _, p := range perms {
			if !validPerm(p) {
				c.JSON(400, gin.H{"error": "Invalid permission: " + p})
				return
			}
		}
		me := currentUser(c)
		if g.ID == me.GroupID {
			var mine []UserPermission
			db.Where("user_id = ?", me.ID).Find(&mine)
			if !effectivePerms(perms, mine)[PermManageUsers] {
				c.JSON(400, gin.H{"error": "That would remove your own access to user management"})
				return
			}
		}
		err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Where("group_id = ?", g.ID).Delete(&GroupPermission{}).Error; err != nil {
				return err
			}
			for _, p := range perms {
				if err := tx.Create(&GroupPermission{GroupID: g.ID, Perm: p}).Error; err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			c.JSON(500, gin.H{"error": "Failed to save permissions"})
			return
		}
		c.JSON(200, gin.H{"message": "Group permissions saved"})
	})
}
