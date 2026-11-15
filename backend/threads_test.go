package main

// Tests for research-thread rules that need no database: when a week
// starts (weekly allowances reset then), who may manage a thread, and how
// dashboard usage is totalled.

import (
	"testing"
	"time"
)

func TestWeekStart(t *testing.T) {
	cases := map[string]string{
		"2026-10-19 00:00": "2026-10-19", // Monday midnight is the start
		"2026-10-21 15:30": "2026-10-19", // Wednesday
		"2026-10-25 23:59": "2026-10-19", // Sunday night is still that week
		"2026-10-26 00:00": "2026-10-26", // next Monday
	}
	for in, want := range cases {
		at, _ := time.ParseInLocation("2006-01-02 15:04", in, time.Local)
		got := weekStart(at)
		if got.Format("2006-01-02 15:04") != want+" 00:00" {
			t.Errorf("weekStart(%s) = %s, want %s 00:00", in, got.Format("2006-01-02 15:04"), want)
		}
	}
}

func TestCanManageThread(t *testing.T) {
	thread := Thread{LeadID: 5}
	lead := authUser{User: User{}, Perms: map[string]bool{}}
	lead.ID = 5
	other := authUser{User: User{}, Perms: map[string]bool{PermCreateThreads: true}}
	other.ID = 6
	labAdmin := authUser{User: User{}, Perms: map[string]bool{PermApproveMechBookings: true}}
	labAdmin.ID = 7

	if !canManageThread(lead, thread) {
		t.Error("the lead runs their thread")
	}
	if canManageThread(other, thread) {
		t.Error("another prof must not change someone else's thread")
	}
	if !canManageThread(labAdmin, thread) {
		t.Error("lab admins can restrict any thread")
	}
}

func TestHoursBy(t *testing.T) {
	at := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	b := func(thread string, hours int) Booking {
		return Booking{ThreadName: thread, StartTime: at, EndTime: at.Add(time.Duration(hours) * time.Hour)}
	}
	rows := hoursBy([]Booking{b("Grasping", 2), b("Locomotion", 5), b("Grasping", 4), b("", 9)},
		func(x Booking) string { return x.ThreadName })
	if len(rows) != 2 || rows[0].Name != "Grasping" || rows[0].Hours != 6 || rows[1].Hours != 5 {
		t.Errorf("got %+v, want Grasping 6h then Locomotion 5h, unthreaded left out", rows)
	}
}

func TestPlural(t *testing.T) {
	if plural(1, "loan") != "1 loan" || plural(3, "loan") != "3 loans" {
		t.Error("plural")
	}
}
