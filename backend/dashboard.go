package main

// One dashboard for everyone, built server-side for the caller. It shows
// only things to act on ("needs you"), the caller's research threads, and -
// for lab and server admins - lab-wide usage. Nothing on it needs setting
// up or maintaining, so it stays useful without anyone tending it.

import (
	"fmt"
	"sort"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type actionItem struct {
	Text string `json:"text"`
	Link string `json:"link"`
}

type usageRow struct {
	Name  string  `json:"name"`
	Hours float64 `json:"hours"`
}

type threadDashboard struct {
	threadView
	Upcoming []PublicBooking `json:"upcoming"`
	Gear     []PublicLoan    `json:"gear"`
}

// plural is "1 loan" / "3 loans".
func plural(n int64, word string) string {
	if n == 1 {
		return fmt.Sprintf("1 %s", word)
	}
	return fmt.Sprintf("%d %ss", n, word)
}

// hoursBy totals booking hours by a key, largest first.
func hoursBy(bookings []Booking, key func(Booking) string) []usageRow {
	totals := map[string]float64{}
	for _, b := range bookings {
		if k := key(b); k != "" {
			totals[k] += b.EndTime.Sub(b.StartTime).Hours()
		}
	}
	rows := make([]usageRow, 0, len(totals))
	for k, h := range totals {
		rows = append(rows, usageRow{k, h})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Hours != rows[j].Hours {
			return rows[i].Hours > rows[j].Hours
		}
		return rows[i].Name < rows[j].Name
	})
	return rows
}

func needsYou(db *gorm.DB, me authUser, now time.Time) []actionItem {
	items := []actionItem{}
	add := func(n int64, text, link string) {
		if n > 0 {
			items = append(items, actionItem{text, link})
		}
	}

	var mine []Loan
	db.Where("user_id = ? AND status = ?", me.ID, LoanActive).Find(&mine)
	var myOverdue int64
	for _, l := range mine {
		if overdue, _ := loanOverdue(l, now); overdue {
			myOverdue++
		}
	}
	add(myOverdue, fmt.Sprintf("You have %s overdue - say what happened", plural(myOverdue, "loan")), "/account")

	stale := now.Add(-verifyEvery)
	var checks int64
	db.Model(&Item{}).Where("custodian_id = ? AND tracked = ? AND (last_verified_at IS NULL OR last_verified_at < ?)", me.ID, true, stale).Count(&checks)
	add(checks, fmt.Sprintf("%s you look after to confirm", plural(checks, "tracked item")), "/account")

	if me.can(PermApproveMechBookings) {
		var n int64
		db.Model(&Booking{}).Where("status = ? AND start_time > ? AND resource_id IN (?)", BookingPending, now,
			db.Model(&Resource{}).Select("id").Where("lab = ?", LabMech)).Count(&n)
		add(n, fmt.Sprintf("%s to approve", plural(n, "robot booking")), "/mechlab")
		db.Model(&Booking{}).Where("status = ? AND start_time > ? AND resource_id IN (?)", BookingPending, now,
			db.Model(&Resource{}).Select("id").Where("lab = ?", LabMocap)).Count(&n)
		add(n, fmt.Sprintf("%s to approve", plural(n, "Mocap Lab booking")), "/mocap")
		db.Model(&ThreadResource{}).Where("status = ?", AccessPending).Count(&n)
		add(n, fmt.Sprintf("%s for robot access to sign off", plural(n, "thread request")), "/threads")
	}
	if me.can(PermApproveLoans) {
		var n int64
		db.Model(&Loan{}).Where("status = ?", LoanPending).Count(&n)
		add(n, fmt.Sprintf("%s to approve", plural(n, "loan request")), "/items")
	}
	if me.can(PermManageInventory) {
		var active []Loan
		db.Where("status = ?", LoanActive).Find(&active)
		var overdue int64
		for _, l := range active {
			if o, _ := loanOverdue(l, now); o {
				overdue++
			}
		}
		add(overdue, fmt.Sprintf("%s overdue across the lab", plural(overdue, "loan")), "/admin?tab=overview")
		var n int64
		db.Model(&Item{}).Where("issue <> ''").Count(&n)
		add(n, fmt.Sprintf("%s with a reported problem", plural(n, "item")), "/items")
		db.Model(&Item{}).Where("tracked = ? AND (last_verified_at IS NULL OR last_verified_at < ?)", true, stale).Count(&n)
		add(n, fmt.Sprintf("%s not confirmed in a month", plural(n, "tracked item")), "/admin?tab=items")
	}
	if me.can(PermManageUsers) {
		var n int64
		db.Model(&User{}).Where("status = ?", UserPending).Count(&n)
		add(n, fmt.Sprintf("%s waiting for approval", plural(n, "sign-up")), "/admin?tab=users")
		if admins := len(usersWithPerm(db, PermManageUsers)); admins < 2 {
			items = append(items, actionItem{"Only one server admin - make a second so the lab is never locked out", "/admin?tab=users"})
		}
	}
	return items
}

func registerDashboardRoutes(api *gin.RouterGroup, db *gorm.DB, requireLogin gin.HandlerFunc) {
	api.GET("/dashboard", requireLogin, func(c *gin.Context) {
		me := currentUser(c)
		now := time.Now()

		// The caller's threads: ones they lead or belong to.
		var threads []Thread
		db.Where("active = ? AND (lead_id = ? OR id IN (?))", true, me.ID,
			db.Model(&ThreadMember{}).Select("thread_id").Where("user_id = ?", me.ID)).Order("name").Find(&threads)
		mine := []threadDashboard{}
		for _, v := range threadViews(db, me, threads) {
			d := threadDashboard{threadView: v, Upcoming: []PublicBooking{}, Gear: []PublicLoan{}}
			var upcoming []Booking
			db.Where("thread_id = ? AND status IN ? AND end_time > ?", v.ID, []string{BookingPending, BookingApproved}, now).
				Order("start_time").Limit(10).Find(&upcoming)
			d.Upcoming = toPublicBookings(upcoming)
			memberIDs := make([]uint, 0, len(v.Members))
			for _, m := range v.Members {
				memberIDs = append(memberIDs, m.ID)
			}
			var gear []Loan
			db.Where("user_id IN ? AND status = ?", memberIDs, LoanActive).Order("expected_return_date").Find(&gear)
			d.Gear = toPublicLoans(gear)
			for i := range d.Gear {
				d.Gear[i].Overdue, d.Gear[i].DaysOverdue = loanOverdue(gear[i], now)
			}
			mine = append(mine, d)
		}

		out := gin.H{"needs_you": needsYou(db, me, now), "threads": mine}

		// Lab-wide usage, for the people who run the lab.
		if me.can(PermApproveMechBookings) || me.can(PermManageUsers) {
			var recent []Booking
			db.Where("status = ? AND start_time > ? AND start_time < ?", BookingApproved, now.AddDate(0, 0, -28), now).Find(&recent)
			var resources []Resource
			db.Unscoped().Find(&resources)
			resourceName := map[uint]string{}
			for _, r := range resources {
				resourceName[r.ID] = r.Name
			}
			topItems := []struct { // empty list, not null, before anything is borrowed
				Name  string `json:"name"`
				Loans int    `json:"loans"`
			}{}
			db.Model(&Loan{}).Select("item_name AS name, COUNT(*) AS loans").Where("created_at > ?", now.AddDate(0, 0, -90)).
				Group("item_name").Order("loans DESC").Limit(5).Scan(&topItems)
			var noShows int64
			db.Model(&Booking{}).Where("no_show = ? AND start_time > ?", true, now.AddDate(0, 0, -28)).Count(&noShows)
			out["usage"] = gin.H{
				"robot_hours":  hoursBy(recent, func(b Booking) string { return resourceName[b.ResourceID] }),
				"thread_hours": hoursBy(recent, func(b Booking) string { return b.ThreadName }),
				"top_items":    topItems,
				"no_shows":     noShows,
			}
		}
		c.JSON(200, out)
	})
}
