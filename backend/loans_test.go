package main

// The CSV export used to parse the return date with a timestamp layout while
// the borrow endpoint stores a plain date, so every row came out as overdue by
// about 106,751 days. These tests pin the behaviour down.

import (
	"testing"
	"time"
)

func mustTime(t *testing.T, value string) time.Time {
	t.Helper()
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		t.Fatalf("bad test time %q: %v", value, err)
	}
	return parsed
}

func TestLoanOverdue(t *testing.T) {
	now := mustTime(t, "2026-08-20T10:00:00Z")

	cases := []struct {
		name        string
		loan        Loan
		wantOverdue bool
		wantDays    int
	}{
		{
			name:        "due in the future",
			loan:        Loan{Status: "active", ExpectedReturnDate: "2026-08-25"},
			wantOverdue: false,
		},
		{
			// The whole due day is still fair game
			name:        "due today is not late yet",
			loan:        Loan{Status: "active", ExpectedReturnDate: "2026-08-20"},
			wantOverdue: false,
		},
		{
			// Due yesterday, so late as of midnight - but only just
			name:        "due yesterday is late by zero full days",
			loan:        Loan{Status: "active", ExpectedReturnDate: "2026-08-19"},
			wantOverdue: true,
			wantDays:    0,
		},
		{
			name:        "three days past due",
			loan:        Loan{Status: "active", ExpectedReturnDate: "2026-08-16"},
			wantOverdue: true,
			wantDays:    3,
		},
		{
			name:        "unparseable date is never overdue",
			loan:        Loan{Status: "active", ExpectedReturnDate: "not a date"},
			wantOverdue: false,
		},
		{
			name:        "empty date is never overdue",
			loan:        Loan{Status: "active", ExpectedReturnDate: ""},
			wantOverdue: false,
		},
		{
			name:        "legacy full timestamp still parses",
			loan:        Loan{Status: "active", ExpectedReturnDate: "2026-08-10T00:00:00Z"},
			wantOverdue: true,
			wantDays:    9,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			overdue, days := loanOverdue(tc.loan, now)
			if overdue != tc.wantOverdue {
				t.Errorf("overdue = %v, want %v", overdue, tc.wantOverdue)
			}
			if overdue && days != tc.wantDays {
				t.Errorf("days = %d, want %d", days, tc.wantDays)
			}
		})
	}
}

func TestLoanOverdueForReturnedItems(t *testing.T) {
	now := mustTime(t, "2026-08-30T10:00:00Z")

	returnedLate := mustTime(t, "2026-08-24T09:00:00Z")
	loan := Loan{
		Status:             "returned",
		ExpectedReturnDate: "2026-08-20",
		ReturnedAt:         &returnedLate,
	}
	overdue, days := loanOverdue(loan, now)
	if !overdue || days != 3 {
		t.Errorf("late return: got overdue=%v days=%d, want true/3", overdue, days)
	}

	// Returned on time - must not be counted as overdue just because time has
	// passed since then
	returnedOnTime := mustTime(t, "2026-08-19T09:00:00Z")
	loan.ReturnedAt = &returnedOnTime
	if overdue, _ := loanOverdue(loan, now); overdue {
		t.Error("an item returned on time must never read as overdue")
	}
}

// Plain dates must be read in the server's timezone, otherwise an item due
// today reads as overdue from midnight UTC - 05:30 in the lab.
func TestLoanOverdueUsesLocalTimezone(t *testing.T) {
	ist, err := time.LoadLocation("Asia/Kolkata")
	if err != nil {
		t.Skip("no timezone database in this environment")
	}

	original := time.Local
	time.Local = ist
	defer func() { time.Local = original }()

	loan := Loan{Status: "active", ExpectedReturnDate: "2026-08-20"}

	// 02:00 IST on the due date - still the 20th here, so not late
	earlyOnDueDate := time.Date(2026, 8, 20, 2, 0, 0, 0, ist)
	if overdue, _ := loanOverdue(loan, earlyOnDueDate); overdue {
		t.Error("an item due today must not read as overdue at 02:00 local time")
	}

	// 23:00 IST on the due date - still fair game
	lateOnDueDate := time.Date(2026, 8, 20, 23, 0, 0, 0, ist)
	if overdue, _ := loanOverdue(loan, lateOnDueDate); overdue {
		t.Error("an item due today must not read as overdue at 23:00 local time")
	}

	// 00:30 IST the next day - now it is late
	afterMidnight := time.Date(2026, 8, 21, 0, 30, 0, 0, ist)
	if overdue, _ := loanOverdue(loan, afterMidnight); !overdue {
		t.Error("an item should read as overdue once its due date has passed locally")
	}
}
