package main

// Research threads decide who may book which robot. A prof creates a thread
// and adds its members; the thread asks for access to robots and a lab admin
// signs it off, optionally with a weekly allowance of hours. Lab admins can
// restrict or withdraw access at any time. Members then book those robots
// for the thread.

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type Thread struct {
	gorm.Model
	Name        string `json:"name"`
	Description string `json:"description"`
	LeadID      uint   `json:"lead_id" gorm:"index"`
	Active      bool   `json:"active" gorm:"default:true"`
}

type ThreadMember struct {
	ID       uint
	ThreadID uint `gorm:"uniqueIndex:idx_thread_member"`
	UserID   uint `gorm:"uniqueIndex:idx_thread_member;index"`
}

const (
	AccessPending  = "pending"
	AccessApproved = "approved"
)

// ThreadResource is a thread's access to one robot. A declined request is
// deleted rather than kept.
type ThreadResource struct {
	ID           uint   `json:"id"`
	ThreadID     uint   `json:"thread_id" gorm:"uniqueIndex:idx_thread_resource"`
	ResourceID   uint   `json:"resource_id" gorm:"uniqueIndex:idx_thread_resource"`
	Status       string `json:"status"`
	HoursPerWeek int    `json:"hours_per_week"` // 0 means no limit
	DecidedBy    string `json:"decided_by"`
}

// canManageThread: the lead runs their thread; lab admins can restrict any.
func canManageThread(me authUser, t Thread) bool {
	return t.LeadID == me.ID || me.can(PermApproveMechBookings)
}

func isThreadMember(db *gorm.DB, threadID, userID uint) bool {
	var n int64
	db.Model(&ThreadMember{}).Where("thread_id = ? AND user_id = ?", threadID, userID).Count(&n)
	return n > 0
}

// weekStart is Monday 00:00 local time of the week containing t; weekly
// allowances reset then.
func weekStart(t time.Time) time.Time {
	t = t.In(time.Local)
	d := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.Local)
	return d.AddDate(0, 0, -((int(d.Weekday()) + 6) % 7))
}

// threadHoursInWeek is how many hours a thread has booked (or asked for) on
// a robot in the week containing at. A booking counts in the week it starts.
func threadHoursInWeek(db *gorm.DB, threadID, resourceID uint, at time.Time) float64 {
	from := weekStart(at)
	var hours float64
	db.Model(&Booking{}).
		Where("thread_id = ? AND resource_id = ? AND status IN ? AND start_time >= ? AND start_time < ?",
			threadID, resourceID, []string{BookingPending, BookingApproved}, from, from.AddDate(0, 0, 7)).
		Select("COALESCE(SUM(EXTRACT(EPOCH FROM end_time - start_time)), 0) / 3600").Scan(&hours)
	return hours
}

type threadPerson struct {
	ID   uint   `json:"id"`
	Name string `json:"name"`
}

type threadAccessView struct {
	ThreadResource
	ResourceName string  `json:"resource_name"`
	ResourceSlug string  `json:"resource_slug"`
	UsedThisWeek float64 `json:"used_this_week"`
}

type threadView struct {
	Thread
	LeadName  string             `json:"lead_name"`
	Members   []threadPerson     `json:"members"`
	Access    []threadAccessView `json:"access"`
	Mine      bool               `json:"mine"`
	CanManage bool               `json:"can_manage"`
}

// threadViews builds the views for threads, with members and robot access.
func threadViews(db *gorm.DB, me authUser, threads []Thread) []threadView {
	ids := make([]uint, 0, len(threads))
	for _, t := range threads {
		ids = append(ids, t.ID)
	}
	var members []struct {
		ThreadID uint
		ID       uint
		Name     string
	}
	db.Model(&ThreadMember{}).Select("thread_members.thread_id, users.id, users.name").
		Joins("JOIN users ON users.id = thread_members.user_id AND users.deleted_at IS NULL").
		Where("thread_members.thread_id IN ?", ids).Order("users.name").Scan(&members)
	var access []ThreadResource
	db.Where("thread_id IN ?", ids).Find(&access)
	var resources []Resource
	db.Find(&resources)
	resource := map[uint]Resource{}
	for _, r := range resources {
		resource[r.ID] = r
	}
	var leads []threadPerson
	db.Model(&User{}).Select("id, name").Scan(&leads)
	leadName := map[uint]string{}
	for _, l := range leads {
		leadName[l.ID] = l.Name
	}

	now := time.Now()
	out := make([]threadView, 0, len(threads))
	for _, t := range threads {
		v := threadView{Thread: t, LeadName: leadName[t.LeadID], Members: []threadPerson{}, Access: []threadAccessView{}, CanManage: canManageThread(me, t)}
		for _, m := range members {
			if m.ThreadID == t.ID {
				v.Members = append(v.Members, threadPerson{m.ID, m.Name})
				v.Mine = v.Mine || m.ID == me.ID
			}
		}
		for _, a := range access {
			if a.ThreadID == t.ID {
				r := resource[a.ResourceID]
				v.Access = append(v.Access, threadAccessView{a, r.Name, r.Slug, threadHoursInWeek(db, t.ID, r.ID, now)})
			}
		}
		out = append(out, v)
	}
	return out
}

func registerThreadRoutes(api *gin.RouterGroup, db *gorm.DB, requireLogin gin.HandlerFunc) {
	user := api.Group("/threads", limitBody(defaultBodyLimit), requireLogin)

	// loadThread fetches :id and checks the caller may manage it.
	loadThread := func(c *gin.Context) (Thread, bool) {
		var t Thread
		if err := db.First(&t, "id = ?", c.Param("id")).Error; err != nil {
			c.JSON(404, gin.H{"error": "Research thread not found"})
			return t, false
		}
		if !canManageThread(currentUser(c), t) {
			c.JSON(403, gin.H{"error": "Only the thread's lead or a lab admin can change it"})
			return t, false
		}
		return t, true
	}

	// Every thread, so people can see who works on what; lab admins also see
	// inactive ones.
	user.GET("", func(c *gin.Context) {
		me := currentUser(c)
		q := db.Order("name")
		if !me.can(PermApproveMechBookings) {
			q = q.Where("active = ?", true)
		}
		var threads []Thread
		if err := q.Find(&threads).Error; err != nil {
			c.JSON(500, gin.H{"error": "Failed to load research threads"})
			return
		}
		c.JSON(200, threadViews(db, me, threads))
	})

	user.POST("", requirePerm(PermCreateThreads), func(c *gin.Context) {
		var req struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, gin.H{"error": "Invalid data"})
			return
		}
		req.Name, req.Description = strings.TrimSpace(req.Name), strings.TrimSpace(req.Description)
		if req.Name == "" || len(req.Name) > 100 || len(req.Description) > 1000 {
			c.JSON(400, gin.H{"error": "Give the thread a name (up to 100 characters)"})
			return
		}
		me := currentUser(c)
		t := Thread{Name: req.Name, Description: req.Description, LeadID: me.ID, Active: true}
		err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Create(&t).Error; err != nil {
				return err
			}
			return tx.Create(&ThreadMember{ThreadID: t.ID, UserID: me.ID}).Error
		})
		if err != nil {
			c.JSON(500, gin.H{"error": "Failed to create the thread"})
			return
		}
		c.JSON(200, gin.H{"message": "Created " + t.Name, "id": t.ID})
	})

	user.PUT("/:id", func(c *gin.Context) {
		var req struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Active      *bool  `json:"active"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, gin.H{"error": "Invalid data"})
			return
		}
		t, ok := loadThread(c)
		if !ok {
			return
		}
		req.Name, req.Description = strings.TrimSpace(req.Name), strings.TrimSpace(req.Description)
		if req.Name == "" || len(req.Name) > 100 || len(req.Description) > 1000 {
			c.JSON(400, gin.H{"error": "Give the thread a name (up to 100 characters)"})
			return
		}
		updates := map[string]interface{}{"name": req.Name, "description": req.Description}
		// Only lab admins switch threads on and off, so a lead can't undo it.
		if req.Active != nil && currentUser(c).can(PermApproveMechBookings) {
			updates["active"] = *req.Active
		}
		if err := db.Model(&t).Updates(updates).Error; err != nil {
			c.JSON(500, gin.H{"error": "Failed to save"})
			return
		}
		c.JSON(200, gin.H{"message": "Saved"})
	})

	// Replace the member list. The lead always stays a member.
	user.PUT("/:id/members", func(c *gin.Context) {
		var req struct {
			UserIDs []uint `json:"user_ids"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, gin.H{"error": "Invalid data"})
			return
		}
		t, ok := loadThread(c)
		if !ok {
			return
		}
		added := slices.Compact(slices.Sorted(slices.Values(req.UserIDs)))
		var found int64
		db.Model(&User{}).Where("id IN ? AND status = ?", added, UserActive).Count(&found)
		if found != int64(len(added)) {
			c.JSON(400, gin.H{"error": "Members must be active users"})
			return
		}
		// The lead stays even if their account was since disabled.
		ids := slices.Compact(slices.Sorted(slices.Values(append(added, t.LeadID))))
		err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Where("thread_id = ?", t.ID).Delete(&ThreadMember{}).Error; err != nil {
				return err
			}
			for _, id := range ids {
				if err := tx.Create(&ThreadMember{ThreadID: t.ID, UserID: id}).Error; err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			c.JSON(500, gin.H{"error": "Failed to save members"})
			return
		}
		c.JSON(200, gin.H{"message": fmt.Sprintf("%d members", len(ids))})
	})

	// Ask for access to a robot. Lab admins asking for a thread they run get
	// it straight away; everyone else waits for one to sign it off.
	user.POST("/:id/resources", func(c *gin.Context) {
		var req struct {
			ResourceID uint `json:"resource_id"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, gin.H{"error": "Invalid data"})
			return
		}
		t, ok := loadThread(c)
		if !ok {
			return
		}
		var res Resource
		if err := db.Where("active = ?", true).First(&res, req.ResourceID).Error; err != nil {
			c.JSON(404, gin.H{"error": "No such robot"})
			return
		}
		me := currentUser(c)
		access := ThreadResource{ThreadID: t.ID, ResourceID: res.ID, Status: AccessPending}
		if me.can(PermApproveMechBookings) {
			access.Status, access.DecidedBy = AccessApproved, me.Name
		}
		if err := db.Create(&access).Error; err != nil {
			c.JSON(409, gin.H{"error": t.Name + " already has or has asked for " + res.Name})
			return
		}
		if access.Status == AccessPending {
			notify(db, usersWithPerm(db, PermApproveMechBookings), fmt.Sprintf("%s asks for %s to use %s", me.Name, t.Name, res.Name), "/threads")
			c.JSON(200, gin.H{"message": "Asked - a lab admin will sign it off"})
			return
		}
		c.JSON(200, gin.H{"message": t.Name + " can now book " + res.Name})
	})

	// A lab admin signs access off, sets or changes the weekly allowance, or
	// declines the request.
	user.POST("/:id/resources/:rid", requirePerm(PermApproveMechBookings), func(c *gin.Context) {
		var req struct {
			Decision     string `json:"decision"` // approve, decline
			HoursPerWeek int    `json:"hours_per_week"`
		}
		if err := c.ShouldBindJSON(&req); err != nil || req.HoursPerWeek < 0 || req.HoursPerWeek > 24*7 {
			c.JSON(400, gin.H{"error": "Invalid data"})
			return
		}
		var access ThreadResource
		if err := db.Where("thread_id = ? AND resource_id = ?", c.Param("id"), c.Param("rid")).First(&access).Error; err != nil {
			c.JSON(404, gin.H{"error": "No such request"})
			return
		}
		var t Thread
		var res Resource
		db.First(&t, access.ThreadID)
		db.First(&res, access.ResourceID)
		me := currentUser(c)
		var err error
		var text string
		switch req.Decision {
		case "approve":
			err = db.Model(&access).Updates(map[string]interface{}{"status": AccessApproved, "hours_per_week": req.HoursPerWeek, "decided_by": me.Name}).Error
			text = fmt.Sprintf("%s can book %s", t.Name, res.Name)
			if req.HoursPerWeek > 0 {
				text += fmt.Sprintf(" (up to %d hours a week)", req.HoursPerWeek)
			}
		case "decline":
			err = db.Delete(&access).Error
			text = fmt.Sprintf("%s declined %s's request for %s", me.Name, t.Name, res.Name)
		default:
			c.JSON(400, gin.H{"error": "Decide approve or decline"})
			return
		}
		if err != nil {
			c.JSON(500, gin.H{"error": "Failed to save"})
			return
		}
		notify(db, []uint{t.LeadID}, text, "/threads")
		c.JSON(200, gin.H{"message": text})
	})

	// Withdraw access: the lead giving it up, or a lab admin restricting it.
	// Bookings already made stay; new ones are refused.
	user.DELETE("/:id/resources/:rid", func(c *gin.Context) {
		t, ok := loadThread(c)
		if !ok {
			return
		}
		res := db.Where("thread_id = ? AND resource_id = ?", t.ID, c.Param("rid")).Delete(&ThreadResource{})
		if res.Error != nil {
			c.JSON(500, gin.H{"error": "Failed to remove"})
			return
		}
		if res.RowsAffected == 0 {
			c.JSON(404, gin.H{"error": "No such access"})
			return
		}
		c.JSON(200, gin.H{"message": "Access removed"})
	})
}
