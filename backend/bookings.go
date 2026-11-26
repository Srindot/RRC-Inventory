package main

// Bookable resources - the Motion Capture Lab and each Mech Lab robot - and
// their bookings. A robot can be limited to research threads a lab admin has
// given access (see threads.go), and can require each booking to be
// approved. Rules are enforced here, not by people: booking limits per robot
// and per thread, and an automatic block for anyone with repeated no-shows.

import (
	"errors"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

const (
	LabMocap = "mocap"
	LabMech  = "mech"
)

// Resource is something that can be booked by time.
type Resource struct {
	gorm.Model
	Slug              string `json:"slug" gorm:"uniqueIndex"`
	Name              string `json:"name"`
	Lab               string `json:"lab" gorm:"index"`
	RequiresApproval  bool   `json:"requires_approval"`
	RequiresThread    bool   `json:"requires_thread"`     // only threads with access may book it
	MaxHours          int    `json:"max_hours"`           // longest single booking; 0 means 12
	MaxFutureBookings int    `json:"max_future_bookings"` // per user; 0 means no limit
	Active            bool   `json:"active" gorm:"default:true"`
}

// Notification is an in-app message, shown under the bell.
type Notification struct {
	ID        uint       `json:"id"`
	UserID    uint       `json:"-" gorm:"index"`
	Text      string     `json:"text"`
	Link      string     `json:"link"`
	CreatedAt time.Time  `json:"created_at"`
	ReadAt    *time.Time `json:"read_at"`
}

const (
	BookingPending  = "pending"
	BookingApproved = "approved"
	BookingRejected = "rejected"
)

// errBookingClash marks a booking refused because the slot is taken.
var errBookingClash = errors.New("that slot overlaps an existing booking")

// bookingLockKey with the resource id serialises bookings per resource with a
// Postgres advisory lock; without it two overlapping requests can both pass
// the overlap check before either inserts.
const bookingLockKey = 7_311_001

const (
	defaultMaxBookingHours = 12
	noShowWindow           = 30 * 24 * time.Hour
	noShowLimit            = 2
	noShowGrace            = 15 * time.Minute // a booking counts as missed this long after it starts
)

var slugPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)

// PublicBooking is what anyone may see of a booking: no phone number.
type PublicBooking struct {
	ID         uint      `json:"ID"`
	ResourceID uint      `json:"resource_id"`
	UserID     *uint     `json:"user_id"`
	ThreadID   *uint     `json:"thread_id"`
	ThreadName string    `json:"thread_name"`
	BookedBy   string    `json:"booked_by"`
	Purpose    string    `json:"purpose"`
	StartTime  time.Time `json:"start_time"`
	EndTime    time.Time `json:"end_time"`
	Status     string    `json:"status"`
	NoShow     bool      `json:"no_show"`
}

func toPublicBookings(bookings []Booking) []PublicBooking {
	out := make([]PublicBooking, 0, len(bookings))
	for _, b := range bookings {
		out = append(out, PublicBooking{
			ID:         b.ID,
			ResourceID: b.ResourceID,
			UserID:     b.UserID,
			ThreadID:   b.ThreadID,
			ThreadName: b.ThreadName,
			BookedBy:   b.BookedBy,
			Purpose:    b.Purpose,
			StartTime:  b.StartTime,
			EndTime:    b.EndTime,
			Status:     b.Status,
			NoShow:     b.NoShow,
		})
	}
	return out
}

// ownedBy reports whether a record belongs to userID. Approvers may not
// approve their own requests.
func ownedBy(owner *uint, userID uint) bool {
	return owner != nil && *owner == userID
}

// bookPermFor is the permission needed to book anything in lab.
func bookPermFor(lab string) string {
	if lab == LabMocap {
		return PermBookMocap
	}
	return PermBookMech
}

// checkBookingTimes enforces the rules every booking shares, plus the
// resource's own maximum length.
func checkBookingTimes(start, end, now time.Time, maxHours int) error {
	if maxHours <= 0 {
		maxHours = defaultMaxBookingHours
	}
	if !end.After(start) {
		return errors.New("End time must be after the start time")
	}
	if end.Sub(start) > time.Duration(maxHours)*time.Hour {
		return fmt.Errorf("A single booking cannot be longer than %d hours", maxHours)
	}
	if start.Before(now.Add(-time.Hour)) {
		return errors.New("Cannot book a slot in the past")
	}
	return nil
}

// canReportNoShow: only an approved booking that started a while ago and has
// not ended can be reported, so a no-show is not reported before the person
// had a chance to turn up.
func canReportNoShow(b Booking, now time.Time) bool {
	return b.Status == BookingApproved && !b.NoShow && now.After(b.StartTime.Add(noShowGrace)) && now.Before(b.EndTime)
}

// bookingBlock returns why userID may not make new bookings, or "".
// The block lifts on its own as no-shows age out of the window.
func bookingBlock(db *gorm.DB, userID uint, now time.Time) string {
	var missed int64
	db.Model(&Booking{}).Where("user_id = ? AND no_show = ? AND start_time > ?", userID, true, now.Add(-noShowWindow)).Count(&missed)
	if missed >= noShowLimit {
		return fmt.Sprintf("You missed %d bookings in the last 30 days, so booking is paused until the oldest one is 30 days old. If a no-show was reported by mistake, ask a lab admin to clear it.", missed)
	}
	return ""
}

func notify(db *gorm.DB, userIDs []uint, text, link string) {
	for _, id := range userIDs {
		if err := db.Create(&Notification{UserID: id, Text: text, Link: link}).Error; err != nil {
			log.Printf("notify %d: %v", id, err)
		}
	}
}

// usersWithPerm lists active users who effectively hold perm.
// It mirrors effectivePerms in one query: granted by the group and not
// revoked for the person, or granted to the person directly.
func usersWithPerm(db *gorm.DB, perm string) []uint {
	var ids []uint
	db.Model(&User{}).Where("status = ?", UserActive).Where(`(
		(EXISTS (SELECT 1 FROM group_permissions gp WHERE gp.group_id = users.group_id AND gp.perm = ?)
		 AND NOT EXISTS (SELECT 1 FROM user_permissions up WHERE up.user_id = users.id AND up.perm = ? AND up.allow = false))
		OR EXISTS (SELECT 1 FROM user_permissions up WHERE up.user_id = users.id AND up.perm = ? AND up.allow = true))`,
		perm, perm, perm).Pluck("id", &ids)
	return ids
}

// labPage is where a resource's calendar lives, for notification links.
func labPage(res Resource) string {
	if res.Lab == LabMocap {
		return "/mocap"
	}
	return "/mechlab?robot=" + res.Slug
}

// setupResources creates the Motion Capture Lab resource and moves the
// bookings made before resources existed onto it.
func setupResources(db *gorm.DB) error {
	var mocap Resource
	err := db.Where("slug = ?", LabMocap).First(&mocap).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		mocap = Resource{Slug: LabMocap, Name: "Motion Capture Lab", Lab: LabMocap, Active: true}
		err = db.Create(&mocap).Error
	}
	if err != nil {
		return err
	}
	return db.Model(&Booking{}).Unscoped().Where("resource_id = 0 OR resource_id IS NULL").
		Updates(map[string]interface{}{"resource_id": mocap.ID, "status": BookingApproved}).Error
}

// parseRange reads ?from= and ?to= (RFC3339), defaulting to two weeks back
// and eight weeks ahead.
func parseRange(c *gin.Context) (time.Time, time.Time) {
	from := time.Now().AddDate(0, 0, -14)
	to := time.Now().AddDate(0, 0, 56)
	if t, err := time.Parse(time.RFC3339, c.Query("from")); err == nil {
		from = t
	}
	if t, err := time.Parse(time.RFC3339, c.Query("to")); err == nil {
		to = t
	}
	return from, to
}

// bookingQuery narrows a booking query by ?resource=<slug> or ?lab=<lab>.
func bookingQuery(db *gorm.DB, c *gin.Context) *gorm.DB {
	q := db.Model(&Booking{})
	if slug := c.Query("resource"); slug != "" {
		q = q.Where("resource_id = (?)", db.Model(&Resource{}).Select("id").Where("slug = ?", slug))
	} else if lab := c.Query("lab"); lab != "" {
		q = q.Where("resource_id IN (?)", db.Model(&Resource{}).Select("id").Where("lab = ?", lab))
	}
	return q
}

func registerBookingRoutes(api *gin.RouterGroup, db *gorm.DB, requireLogin gin.HandlerFunc) {
	// A group of its own, so the body limit does not reach routes registered
	// on api later (printer uploads are far bigger).
	api = api.Group("", limitBody(defaultBodyLimit))

	// --- PUBLIC ---

	api.GET("/resources", func(c *gin.Context) {
		var resources []Resource
		q := db.Where("active = ?", true).Order("name")
		if lab := c.Query("lab"); lab != "" {
			q = q.Where("lab = ?", lab)
		}
		if err := q.Find(&resources).Error; err != nil {
			c.JSON(500, gin.H{"error": "Failed to load resources"})
			return
		}
		out := make([]gin.H, 0, len(resources))
		for _, r := range resources {
			out = append(out, gin.H{
				"id": r.ID, "slug": r.Slug, "name": r.Name, "lab": r.Lab,
				"requires_approval": r.RequiresApproval, "requires_thread": r.RequiresThread,
				"max_hours": r.MaxHours, "max_future_bookings": r.MaxFutureBookings,
			})
		}
		c.JSON(200, out)
	})

	// Bookings in a time range. Rejected ones are left out; pending ones are
	// shown so people can see a slot has been asked for.
	api.GET("/bookings", func(c *gin.Context) {
		from, to := parseRange(c)
		var bookings []Booking
		if err := bookingQuery(db, c).Where("status IN ? AND start_time < ? AND end_time > ?",
			[]string{BookingPending, BookingApproved}, to, from).
			Order("start_time ASC").Find(&bookings).Error; err != nil {
			c.JSON(500, gin.H{"error": "Failed to retrieve bookings"})
			return
		}
		c.JSON(200, toPublicBookings(bookings))
	})

	// --- LOGGED IN ---

	user := api.Group("", requireLogin)

	user.GET("/bookings/mine", func(c *gin.Context) {
		var bookings []Booking
		db.Where("user_id = ? AND end_time > ?", currentUser(c).ID, time.Now().AddDate(0, 0, -30)).
			Order("start_time DESC").Find(&bookings)
		c.JSON(200, toPublicBookings(bookings))
	})

	user.POST("/bookings", func(c *gin.Context) {
		var req struct {
			Resource  string `json:"resource" binding:"required"`
			Purpose   string `json:"purpose"` // optional; defaults to the thread or "Booking"
			StartTime string `json:"start_time" binding:"required"`
			EndTime   string `json:"end_time" binding:"required"`
			ThreadID  uint   `json:"thread_id"` // which research thread this is for
			// Booking managers book for people who ask in person.
			OnBehalfName  string `json:"on_behalf_name"`
			OnBehalfPhone string `json:"on_behalf_phone"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, gin.H{"error": "All fields are required"})
			return
		}
		var res Resource
		if err := db.Where("slug = ? AND active = ?", req.Resource, true).First(&res).Error; err != nil {
			c.JSON(404, gin.H{"error": "No such lab or robot"})
			return
		}
		me := currentUser(c)
		now := time.Now()
		onBehalf := strings.TrimSpace(req.OnBehalfName) != ""
		if onBehalf && !me.can(PermManageBookings) {
			c.JSON(403, gin.H{"error": "Only booking managers can book for someone else"})
			return
		}
		// A manager booking for someone vouches for them, so the per-person
		// rules (permission, limits, no-show pause) apply only to self-booking.
		if !onBehalf {
			if !me.can(bookPermFor(res.Lab)) {
				c.JSON(403, gin.H{"error": "Your account can't book this. Ask a server admin."})
				return
			}
			if reason := userBlock(db, me.ID, now); reason != "" {
				c.JSON(403, gin.H{"error": reason})
				return
			}
		}
		// Robots are booked for a research thread that has access to them.
		var thread Thread
		var access ThreadResource
		if res.RequiresThread || req.ThreadID != 0 {
			if req.ThreadID == 0 {
				c.JSON(400, gin.H{"error": "Pick the research thread this booking is for"})
				return
			}
			if db.Where("active = ?", true).First(&thread, req.ThreadID).Error != nil {
				c.JSON(404, gin.H{"error": "No such research thread"})
				return
			}
			if !onBehalf && !isThreadMember(db, thread.ID, me.ID) {
				c.JSON(403, gin.H{"error": "You're not a member of " + thread.Name + ". Ask its lead to add you."})
				return
			}
			err := db.Where("thread_id = ? AND resource_id = ? AND status = ?", thread.ID, res.ID, AccessApproved).First(&access).Error
			if res.RequiresThread && err != nil {
				c.JSON(403, gin.H{"error": fmt.Sprintf("%s doesn't have access to %s yet. The thread lead can request it; a lab admin signs it off.", thread.Name, res.Name)})
				return
			}
		}
		start, err1 := time.Parse(time.RFC3339, req.StartTime)
		end, err2 := time.Parse(time.RFC3339, req.EndTime)
		if err1 != nil || err2 != nil {
			c.JSON(400, gin.H{"error": "Invalid start or end time"})
			return
		}
		if err := checkBookingTimes(start, end, now, res.MaxHours); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}

		purpose := strings.TrimSpace(req.Purpose)
		if purpose == "" {
			purpose = "Booking"
			if thread.ID != 0 {
				purpose = thread.Name
			}
		}
		status := BookingApproved
		if res.RequiresApproval && !onBehalf && !me.can(PermAutoApprove) {
			status = BookingPending
		}
		uid := me.ID
		booking := Booking{
			ResourceID: res.ID, UserID: &uid, BookedBy: me.Name, Phone: me.Phone,
			Purpose: purpose, StartTime: start, EndTime: end, Status: status,
		}
		if thread.ID != 0 {
			tid := thread.ID
			booking.ThreadID, booking.ThreadName = &tid, thread.Name
		}
		if onBehalf {
			booking.UserID = nil
			booking.BookedBy = strings.TrimSpace(req.OnBehalfName) + " (via " + me.Name + ")"
			booking.Phone = strings.TrimSpace(req.OnBehalfPhone)
		}
		err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Exec("SELECT pg_advisory_xact_lock(?::int, ?::int)", bookingLockKey, res.ID).Error; err != nil {
				return err
			}
			if res.MaxFutureBookings > 0 && !onBehalf {
				var upcoming int64
				tx.Model(&Booking{}).Where("resource_id = ? AND user_id = ? AND status IN ? AND end_time > ?",
					res.ID, me.ID, []string{BookingPending, BookingApproved}, now).Count(&upcoming)
				if upcoming >= int64(res.MaxFutureBookings) {
					return fmt.Errorf("%w: you already have %d upcoming bookings on %s, the most allowed", errBookingClash, upcoming, res.Name)
				}
			}
			// A thread's weekly allowance on this robot. Lab admins, who set
			// the allowances, may go over it.
			if access.HoursPerWeek > 0 && !me.can(PermApproveMechBookings) {
				used := threadHoursInWeek(tx, thread.ID, res.ID, start)
				if used+end.Sub(start).Hours() > float64(access.HoursPerWeek) {
					return fmt.Errorf("%w: %s has used %.1f of its %d hours on %s that week", errBookingClash,
						thread.Name, used, access.HoursPerWeek, res.Name)
				}
			}
			// Only approved bookings hold a slot; pending requests may overlap
			// and the approver picks one.
			var clash Booking
			err := tx.Where("resource_id = ? AND status = ? AND start_time < ? AND end_time > ?",
				res.ID, BookingApproved, end, start).First(&clash).Error
			if err == nil {
				return fmt.Errorf("%w by %s (%s - %s)", errBookingClash, clash.BookedBy,
					clash.StartTime.Local().Format("Mon 2 Jan 15:04"), clash.EndTime.Local().Format("15:04"))
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			return tx.Create(&booking).Error
		})
		if err != nil {
			if errors.Is(err, errBookingClash) {
				c.JSON(409, gin.H{"error": err.Error()})
				return
			}
			log.Printf("booking: %v", err)
			c.JSON(500, gin.H{"error": "Failed to book"})
			return
		}
		msg := res.Name + " booked!"
		if status == BookingPending {
			msg = "Requested - it's yours once an approver accepts it."
			notify(db, usersWithPerm(db, PermApproveMechBookings),
				fmt.Sprintf("%s asked for %s on %s", me.Name, res.Name, start.Local().Format("Mon 2 Jan 15:04")), labPage(res))
		}
		c.JSON(200, gin.H{"message": msg, "booking": toPublicBookings([]Booking{booking})[0]})
	})

	// Cancel: the person who booked, or anyone who manages bookings.
	user.POST("/bookings/:id/cancel", func(c *gin.Context) {
		var b Booking
		if err := db.First(&b, "id = ?", c.Param("id")).Error; err != nil {
			c.JSON(404, gin.H{"error": "Booking not found"})
			return
		}
		me := currentUser(c)
		if !me.can(PermManageBookings) {
			if b.UserID == nil || *b.UserID != me.ID {
				c.JSON(403, gin.H{"error": "Only the person who booked can cancel this"})
				return
			}
			// Otherwise cancelling a missed booking would erase the no-show.
			if b.NoShow || !b.StartTime.After(time.Now()) {
				c.JSON(409, gin.H{"error": "A booking that has started can't be cancelled - ask a booking manager"})
				return
			}
		}
		if err := db.Delete(&b).Error; err != nil {
			c.JSON(500, gin.H{"error": "Failed to cancel booking"})
			return
		}
		c.JSON(200, gin.H{"message": "Booking cancelled"})
	})

	// Report that whoever booked a slot did not turn up. Anyone who can book
	// in that lab may report, and their name is kept with the report.
	user.POST("/bookings/:id/no-show", func(c *gin.Context) {
		var b Booking
		if err := db.First(&b, "id = ?", c.Param("id")).Error; err != nil {
			c.JSON(404, gin.H{"error": "Booking not found"})
			return
		}
		var res Resource
		db.First(&res, b.ResourceID)
		me := currentUser(c)
		if !me.can(bookPermFor(res.Lab)) {
			c.JSON(403, gin.H{"error": "You don't have permission to do this"})
			return
		}
		if ownedBy(b.UserID, me.ID) {
			c.JSON(400, gin.H{"error": "You can't report your own booking"})
			return
		}
		// Only people who'd know: lab admins, booking managers, or someone with
		// their own booking on it that day. Stops a stranger from pausing
		// someone with two false reports.
		if !me.can(PermManageBookings) && !me.can(PermApproveMechBookings) {
			var mine int64
			db.Model(&Booking{}).Where("resource_id = ? AND user_id = ? AND status = ? AND start_time < ? AND end_time > ?",
				b.ResourceID, me.ID, BookingApproved, b.EndTime.Add(12*time.Hour), b.StartTime.Add(-12*time.Hour)).Count(&mine)
			if mine == 0 {
				c.JSON(403, gin.H{"error": "Only someone with their own booking on it that day, or a lab admin, can report a no-show"})
				return
			}
		}
		if !canReportNoShow(b, time.Now()) {
			c.JSON(400, gin.H{"error": "A no-show can be reported from 15 minutes after the start until the booking ends"})
			return
		}
		db.Model(&b).Updates(map[string]interface{}{"no_show": true, "no_show_reported_by": me.Name})
		if b.UserID != nil {
			notify(db, []uint{*b.UserID}, fmt.Sprintf("%s reported that you didn't turn up for %s at %s. Two in 30 days pauses booking.",
				me.Name, res.Name, b.StartTime.Local().Format("Mon 2 Jan 15:04")), "/account")
		}
		c.JSON(200, gin.H{"message": "No-show recorded"})
	})

	// --- APPROVALS ---

	approver := user.Group("", requirePerm(PermApproveMechBookings))

	approver.GET("/bookings/pending", func(c *gin.Context) {
		var bookings []Booking
		bookingQuery(db, c).Where("status = ? AND start_time > ?", BookingPending, time.Now()).
			Order("start_time ASC").Find(&bookings)
		c.JSON(200, toPublicBookings(bookings))
	})

	approver.POST("/bookings/:id/approve", func(c *gin.Context) {
		var b Booking
		if err := db.First(&b, "id = ?", c.Param("id")).Error; err != nil {
			c.JSON(404, gin.H{"error": "Booking not found"})
			return
		}
		if b.Status != BookingPending || !b.StartTime.After(time.Now()) {
			c.JSON(400, gin.H{"error": "Only upcoming requests can be approved"})
			return
		}
		me := currentUser(c)
		if ownedBy(b.UserID, me.ID) {
			c.JSON(403, gin.H{"error": "Someone else has to approve your own request"})
			return
		}
		var losers []Booking
		err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Exec("SELECT pg_advisory_xact_lock(?::int, ?::int)", bookingLockKey, b.ResourceID).Error; err != nil {
				return err
			}
			var clash Booking
			err := tx.Where("resource_id = ? AND status = ? AND start_time < ? AND end_time > ?",
				b.ResourceID, BookingApproved, b.EndTime, b.StartTime).First(&clash).Error
			if err == nil {
				return fmt.Errorf("%w by %s", errBookingClash, clash.BookedBy)
			}
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			now := time.Now()
			// Still pending? It may have been declined or cancelled meanwhile.
			res := tx.Model(&Booking{}).Where("id = ? AND status = ?", b.ID, BookingPending).
				Updates(map[string]interface{}{"status": BookingApproved, "reviewed_by": me.Name, "reviewed_at": now})
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected == 0 {
				return fmt.Errorf("%w: that request was already handled", errBookingClash)
			}
			// Requests for the same slot lose to this one.
			tx.Where("resource_id = ? AND status = ? AND id <> ? AND start_time < ? AND end_time > ?",
				b.ResourceID, BookingPending, b.ID, b.EndTime, b.StartTime).Find(&losers)
			for _, l := range losers {
				if err := tx.Model(&l).Updates(map[string]interface{}{"status": BookingRejected, "reviewed_by": me.Name, "reviewed_at": now}).Error; err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			if errors.Is(err, errBookingClash) {
				c.JSON(409, gin.H{"error": err.Error()})
				return
			}
			c.JSON(500, gin.H{"error": "Failed to approve"})
			return
		}
		var res Resource
		db.First(&res, b.ResourceID)
		when := b.StartTime.Local().Format("Mon 2 Jan 15:04")
		if b.UserID != nil {
			notify(db, []uint{*b.UserID}, fmt.Sprintf("Your booking of %s on %s was approved", res.Name, when), labPage(res))
		}
		for _, l := range losers {
			if l.UserID != nil {
				notify(db, []uint{*l.UserID}, fmt.Sprintf("Your request for %s on %s was declined: the slot went to another booking", res.Name, l.StartTime.Local().Format("Mon 2 Jan 15:04")), labPage(res))
			}
		}
		c.JSON(200, gin.H{"message": "Approved", "rejected_overlapping": len(losers)})
	})

	approver.POST("/bookings/:id/reject", func(c *gin.Context) {
		var b Booking
		if err := db.First(&b, "id = ?", c.Param("id")).Error; err != nil {
			c.JSON(404, gin.H{"error": "Booking not found"})
			return
		}
		if b.Status != BookingPending {
			c.JSON(400, gin.H{"error": "Only pending requests can be declined"})
			return
		}
		me := currentUser(c)
		// Still pending? An approve may have won the race.
		upd := db.Model(&Booking{}).Where("id = ? AND status = ?", b.ID, BookingPending).
			Updates(map[string]interface{}{"status": BookingRejected, "reviewed_by": me.Name, "reviewed_at": time.Now()})
		if upd.Error != nil || upd.RowsAffected == 0 {
			c.JSON(409, gin.H{"error": "That request was already handled"})
			return
		}
		var res Resource
		db.First(&res, b.ResourceID)
		if b.UserID != nil {
			notify(db, []uint{*b.UserID}, fmt.Sprintf("Your request for %s on %s was declined by %s", res.Name, b.StartTime.Local().Format("Mon 2 Jan 15:04"), me.Name), labPage(res))
		}
		c.JSON(200, gin.H{"message": "Declined"})
	})

	// --- NOTIFICATIONS ---

	user.GET("/notifications", func(c *gin.Context) {
		var notes []Notification
		db.Where("user_id = ?", currentUser(c).ID).Order("created_at DESC").Limit(30).Find(&notes)
		c.JSON(200, notes)
	})

	user.POST("/notifications/read", func(c *gin.Context) {
		db.Model(&Notification{}).Where("user_id = ? AND read_at IS NULL", currentUser(c).ID).Update("read_at", time.Now())
		c.JSON(200, gin.H{"message": "ok"})
	})

	// --- MANAGING RESOURCES (manage_bookings) ---

	manage := api.Group("/admin", requireLogin, requirePerm(PermManageBookings))

	// Names of active users, to pick thread members and custodians from.
	api.GET("/admin/people", requireLogin, requireAnyPerm(PermManageBookings, PermManageInventory, PermManageUsers, PermCreateThreads), func(c *gin.Context) {
		var people []struct {
			ID   uint   `json:"id"`
			Name string `json:"name"`
		}
		db.Model(&User{}).Where("status = ?", UserActive).Order("name").Select("id, name").Scan(&people)
		c.JSON(200, people)
	})

	manage.GET("/resources", func(c *gin.Context) {
		var resources []Resource
		db.Order("lab, name").Find(&resources)
		out := make([]gin.H, 0, len(resources))
		for _, r := range resources {
			out = append(out, gin.H{"resource": r})
		}
		c.JSON(200, out)
	})

	saveResource := func(c *gin.Context, res *Resource) {
		var req struct {
			Slug              string `json:"slug"`
			Name              string `json:"name"`
			Lab               string `json:"lab"`
			RequiresApproval  bool   `json:"requires_approval"`
			RequiresThread    bool   `json:"requires_thread"`
			MaxHours          int    `json:"max_hours"`
			MaxFutureBookings int    `json:"max_future_bookings"`
			Active            *bool  `json:"active"`
		}
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(400, gin.H{"error": "Invalid data"})
			return
		}
		req.Slug = strings.ToLower(strings.TrimSpace(req.Slug))
		req.Name = strings.TrimSpace(req.Name)
		if res.ID == 0 && !slugPattern.MatchString(req.Slug) {
			c.JSON(400, gin.H{"error": "The id must be lowercase letters, digits and dashes, e.g. ur5-1"})
			return
		}
		if req.Name == "" || (req.Lab != LabMocap && req.Lab != LabMech) || req.MaxHours < 0 || req.MaxHours > 24*7 || req.MaxFutureBookings < 0 {
			c.JSON(400, gin.H{"error": "Give a name, a lab, and limits of zero or more"})
			return
		}
		if res.ID == 0 {
			res.Slug = req.Slug // the slug is the clearance key, so it never changes
			res.Active = true
		}
		res.Name, res.Lab = req.Name, req.Lab
		res.RequiresApproval, res.RequiresThread = req.RequiresApproval, req.RequiresThread
		res.MaxHours, res.MaxFutureBookings = req.MaxHours, req.MaxFutureBookings
		if req.Active != nil {
			res.Active = *req.Active
		}
		err := db.Save(res).Error
		if err != nil {
			c.JSON(409, gin.H{"error": "Could not save - is that id already used?"})
			return
		}
		c.JSON(200, gin.H{"message": "Saved", "resource": res})
	}

	manage.POST("/resources", func(c *gin.Context) {
		saveResource(c, &Resource{})
	})

	manage.PUT("/resources/:id", func(c *gin.Context) {
		var res Resource
		if err := db.First(&res, "id = ?", c.Param("id")).Error; err != nil {
			c.JSON(404, gin.H{"error": "Not found"})
			return
		}
		saveResource(c, &res)
	})

	manage.POST("/bookings/:id/clear-no-show", func(c *gin.Context) {
		if err := db.Model(&Booking{}).Where("id = ?", c.Param("id")).
			Updates(map[string]interface{}{"no_show": false, "no_show_reported_by": ""}).Error; err != nil {
			c.JSON(500, gin.H{"error": "Failed to clear"})
			return
		}
		c.JSON(200, gin.H{"message": "No-show cleared"})
	})
}
