package main

// Tests for taking over someone else's loan and for the sign-up rate limit.

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCheckHandover(t *testing.T) {
	holderID, otherID := uint(1), uint(2)
	student := authUser{User: User{}, Perms: map[string]bool{PermBorrowItems: true}}
	student.ID = otherID
	staff := authUser{User: User{}, Perms: map[string]bool{PermBorrowItems: true, PermManageInventory: true}}
	staff.ID = 3
	holder := authUser{User: User{}, Perms: map[string]bool{PermBorrowItems: true}}
	holder.ID = holderID

	accountLoan := Loan{UserID: &holderID, BorrowerPhone: "+91 98765 43210"}
	legacyLoan := Loan{BorrowerPhone: "9876543210"} // made before accounts existed

	tests := []struct {
		name  string
		me    authUser
		phone string
		loan  Loan
		want  error // nil, errHandoverPhone, or any loanUserError
	}{
		{"no phone is refused", student, "", accountLoan, errHandoverPhone},
		{"wrong phone is refused", student, "9999999999", accountLoan, errHandoverPhone},
		{"holder's phone is accepted", student, "98765 43210", accountLoan, nil},
		{"legacy loan needs the phone too", student, "", legacyLoan, errHandoverPhone},
		{"legacy loan with phone is accepted", student, "+91-9876543210", legacyLoan, nil},
		{"staff need no phone", staff, "", accountLoan, nil},
		{"own loan is refused", holder, "9876543210", accountLoan, loanUserError("")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := checkHandover(tt.me, tt.phone, tt.loan)
			var userErr loanUserError
			switch {
			case tt.want == nil && err != nil:
				t.Errorf("want allowed, got %v", err)
			case tt.want == errHandoverPhone && !errors.Is(err, errHandoverPhone):
				t.Errorf("want phone mismatch, got %v", err)
			case errors.As(tt.want, &userErr) && !errors.As(err, &userErr):
				t.Errorf("want a user-facing refusal, got %v", err)
			}
		})
	}
}

func TestRegisterIsRateLimited(t *testing.T) {
	registerLimits = newLoginLimiter()
	t.Cleanup(func() { registerLimits = newLoginLimiter() })
	router := newRouter(nil, nil, nil, newLoginLimiter())

	post := func(ip string) int {
		req := httptest.NewRequest(http.MethodPost, "/api/auth/register", strings.NewReader("not json"))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = ip + ":1234"
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w.Code
	}

	// Malformed sign-ups are rejected before the database, but still count.
	for i := 0; i < loginMaxFailures; i++ {
		if code := post("192.0.2.1"); code != http.StatusBadRequest {
			t.Fatalf("attempt %d: want 400, got %d", i+1, code)
		}
	}
	if code := post("192.0.2.1"); code != http.StatusTooManyRequests {
		t.Fatalf("over the limit: want 429, got %d", code)
	}
	if code := post("192.0.2.2"); code != http.StatusBadRequest {
		t.Fatalf("another address is not limited: want 400, got %d", code)
	}
}

func TestAnonymousBorrower(t *testing.T) {
	form := func(name, phone string) map[string][]string {
		return map[string][]string{"borrower_name": {name}, "borrower_phone": {phone}}
	}
	tests := []struct {
		name, inName, inPhone string
		wantPhone             string
		wantErr               bool
	}{
		{"valid", " Asha Rao ", "+91 98765 43210", "9876543210", false},
		{"dashes", "Asha", "98765-43210", "9876543210", false},
		{"no name", "  ", "9876543210", "", true},
		{"long name", strings.Repeat("a", 101), "9876543210", "", true},
		{"short phone", "Asha", "12345", "", true},
		{"no phone", "Asha", "", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, phone, err := anonymousBorrower(form(tt.inName, tt.inPhone))
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if !tt.wantErr && (phone != tt.wantPhone || name != strings.TrimSpace(tt.inName)) {
				t.Errorf("got (%q, %q), want (%q, %q)", name, phone, strings.TrimSpace(tt.inName), tt.wantPhone)
			}
		})
	}
}
