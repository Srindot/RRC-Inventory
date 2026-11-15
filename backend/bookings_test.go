package main

// Tests for the booking rules that do not need a database: how long a
// booking may be, when a no-show may be reported, which permission each lab
// needs, and what a robot id may look like.

import (
	"testing"
	"time"
)

func TestCheckBookingTimes(t *testing.T) {
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	at := func(h float64) time.Time { return now.Add(time.Duration(h * float64(time.Hour))) }

	cases := []struct {
		name       string
		start, end time.Time
		maxHours   int
		ok         bool
	}{
		{"normal", at(1), at(3), 0, true},
		{"just started", at(-0.5), at(1), 0, true},
		{"end before start", at(3), at(1), 0, false},
		{"zero length", at(1), at(1), 0, false},
		{"default cap is 12h", at(1), at(14), 0, false},
		{"exactly 12h", at(1), at(13), 0, true},
		{"robot cap", at(1), at(4), 2, false},
		{"within robot cap", at(1), at(3), 2, true},
		{"long in the past", at(-3), at(-1), 0, false},
	}
	for _, tc := range cases {
		err := checkBookingTimes(tc.start, tc.end, now, tc.maxHours)
		if (err == nil) != tc.ok {
			t.Errorf("%s: got err=%v, want ok=%v", tc.name, err, tc.ok)
		}
	}
}

func TestCanReportNoShow(t *testing.T) {
	start := time.Date(2026, 10, 20, 10, 0, 0, 0, time.UTC)
	b := Booking{Status: BookingApproved, StartTime: start, EndTime: start.Add(2 * time.Hour)}

	if canReportNoShow(b, start.Add(5*time.Minute)) {
		t.Error("too early: they may just be late")
	}
	if !canReportNoShow(b, start.Add(20*time.Minute)) {
		t.Error("should be reportable 20 minutes in")
	}
	if canReportNoShow(b, start.Add(3*time.Hour)) {
		t.Error("should not be reportable after it ended")
	}
	pending := b
	pending.Status = BookingPending
	if canReportNoShow(pending, start.Add(20*time.Minute)) {
		t.Error("a pending request never held the slot")
	}
	reported := b
	reported.NoShow = true
	if canReportNoShow(reported, start.Add(20*time.Minute)) {
		t.Error("already reported")
	}
}

func TestBookPermFor(t *testing.T) {
	if bookPermFor(LabMocap) != PermBookMocap || bookPermFor(LabMech) != PermBookMech {
		t.Error("wrong permission for lab")
	}
}

func TestSlugPattern(t *testing.T) {
	for _, s := range []string{"ur5-1", "franka", "mocap", "a"} {
		if !slugPattern.MatchString(s) {
			t.Errorf("%q should be allowed", s)
		}
	}
	for _, s := range []string{"", "UR5", "-ur5", "ur5 1", "robot:ur5", "ur5/1"} {
		if slugPattern.MatchString(s) {
			t.Errorf("%q should be refused", s)
		}
	}
}

func TestOwnedBy(t *testing.T) {
	id := uint(7)
	if !ownedBy(&id, 7) || ownedBy(&id, 8) || ownedBy(nil, 7) {
		t.Error("ownedBy: a record without an owner belongs to nobody; otherwise only its owner")
	}
}
