package main

// Tests for account rules that decide who may do what: how group defaults
// and per-user overrides combine, which permission names are accepted, and
// what a sign-up must contain.

import (
	"slices"
	"strings"
	"testing"
)

func TestEffectivePerms(t *testing.T) {
	group := []string{PermBorrowItems, PermBookMech}
	overrides := []UserPermission{
		{Perm: "robot:ur5-1", Allow: true},    // clearance granted on top
		{Perm: PermBookMech, Allow: false},    // revoked from the group
		{Perm: PermManageUsers, Allow: false}, // revoking what the group lacks is harmless
	}
	got := effectivePerms(group, overrides)

	for _, want := range []string{PermBorrowItems, "robot:ur5-1"} {
		if !got[want] {
			t.Errorf("missing %s", want)
		}
	}
	for _, not := range []string{PermBookMech, PermManageUsers} {
		if got[not] {
			t.Errorf("unexpected %s", not)
		}
	}
}

func TestValidPerm(t *testing.T) {
	for _, p := range allPerms {
		if !validPerm(p) {
			t.Errorf("%q should be valid", p)
		}
	}
	// Robot access comes from research threads now, not per-person perms.
	for _, p := range []string{"", "robot:ur5-1", "is_super_admin", "Borrow_Items"} {
		if validPerm(p) {
			t.Errorf("%q should be invalid", p)
		}
	}
}

func TestDefaultGroups(t *testing.T) {
	perms := map[string][]string{}
	for _, g := range defaultGroups {
		perms[g.Name] = g.Perms
	}
	if !slices.Equal(perms[serverAdminGroup], allPerms) {
		t.Error("Server Admin must hold every permission")
	}
	for _, name := range []string{"Student", "Intern", "Prof", labAdminGroup} {
		if slices.Contains(perms[name], PermManageUsers) {
			t.Errorf("%s must not manage users", name)
		}
	}
	// Building Prof's list must not have written into Student's backing array.
	if slices.Contains(perms["Student"], PermAutoApprove) || slices.Contains(perms["Student"], PermManageInventory) {
		t.Error("Student picked up another group's permissions")
	}
	// Lab Admins sign off; Profs don't, but need no sign-off themselves.
	if !slices.Contains(perms[labAdminGroup], PermApproveMechBookings) || !slices.Contains(perms[labAdminGroup], PermApproveLoans) {
		t.Error("Lab Admin should approve bookings and loans")
	}
	if slices.Contains(perms["Prof"], PermApproveMechBookings) || slices.Contains(perms["Prof"], PermApproveLoans) {
		t.Error("Profs must not sign off")
	}
	for _, g := range []string{"Prof", labAdminGroup, serverAdminGroup} {
		if !slices.Contains(perms[g], PermAutoApprove) {
			t.Errorf("%s should skip sign-off for their own requests", g)
		}
	}
	for _, g := range []string{"Student", "Intern"} {
		if slices.Contains(perms[g], PermAutoApprove) {
			t.Errorf("%s should need sign-off", g)
		}
	}
}

func TestValidateRegistration(t *testing.T) {
	ok := func() registerRequest {
		return registerRequest{Name: " Asha ", Email: " Asha@Students.IIIT.ac.in ", Phone: "+91 98765 43210", Password: "longenough", GroupID: 1}
	}

	req := ok()
	if err := validateRegistration(&req); err != nil {
		t.Fatalf("valid sign-up rejected: %v", err)
	}
	if req.Name != "Asha" || req.Email != "asha@students.iiit.ac.in" || req.Phone != "9876543210" {
		t.Errorf("not normalised: %+v", req)
	}

	cases := map[string]func(*registerRequest){
		"no name":         func(r *registerRequest) { r.Name = "  " },
		"no at":           func(r *registerRequest) { r.Email = "asha" },
		"nothing after @": func(r *registerRequest) { r.Email = "asha@" },
		"short phone":     func(r *registerRequest) { r.Phone = "12345" },
		"no group":        func(r *registerRequest) { r.GroupID = 0 },
		"short password":  func(r *registerRequest) { r.Password = "short" },
		"long password":   func(r *registerRequest) { r.Password = strings.Repeat("x", 73) },
	}
	for name, mutate := range cases {
		req := ok()
		mutate(&req)
		if validateRegistration(&req) == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestHashTokenIsStableAndHidesToken(t *testing.T) {
	if hashToken("abc") != hashToken("abc") {
		t.Fatal("hash must be deterministic for lookups")
	}
	if hashToken("abc") == hashToken("abd") || strings.Contains(hashToken("abc"), "abc") {
		t.Fatal("hash must differ per token and not contain it")
	}
}
