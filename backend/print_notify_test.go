package main

import (
	"os"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestPrintNameKey(t *testing.T) {
	for in, want := range map[string]string{
		"asha_bracket.gcode.3mf":     "asha_bracket",
		"Asha_Bracket.3MF":           "asha_bracket",
		"/sdcard/asha_bracket.gcode": "asha_bracket",
		" asha_bracket ":             "asha_bracket",
		"nav_finleftupper.gcode.3mf": "nav_finleftupper",
		"Benchy (1).gcode.3mf":       "benchy (1)",
	} {
		if got := printNameKey(in); got != want {
			t.Errorf("printNameKey(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPrintEndMessage(t *testing.T) {
	job := PrintJob{PrinterName: "Printer 2", FileName: "asha_bracket.gcode.3mf", LastPercent: 42}
	for result, want := range map[string]string{
		"finished":    "Printer 2 finished asha_bracket",
		"failed":      "Printer 2 failed while printing asha_bracket (at 42%)",
		"stopped":     "Printer 2 stopped printing asha_bracket at 42%",
		"interrupted": "",
		"running":     "",
	} {
		job.Result = result
		got := printEndMessage(job)
		if want == "" && got != "" || want != "" && !strings.Contains(got, want) {
			t.Errorf("%s: got %q, want it to contain %q", result, got, want)
		}
	}
	job.Result, job.StoppedBy = "stopped", "Srinath"
	if got := printEndMessage(job); !strings.Contains(got, "Srinath stopped asha_bracket on Printer 2") {
		t.Errorf("stopped by someone: got %q", got)
	}
}

// TestAnnouncePrintEnd needs a scratch Postgres database:
// TEST_DATABASE_URL=postgresql://... go test -run TestAnnouncePrintEnd
func TestAnnouncePrintEnd(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&PrintUpload{}, &Notification{}, &User{}, &Group{}, &GroupPermission{}, &UserPermission{}); err != nil {
		t.Fatal(err)
	}
	db.Exec("TRUNCATE print_uploads, notifications, users, groups, group_permissions, user_permissions RESTART IDENTITY CASCADE")

	admins := Group{Name: "Lab Admin"}
	db.Create(&admins)
	students := Group{Name: "Student"}
	db.Create(&students)
	db.Create(&GroupPermission{GroupID: admins.ID, Perm: PermManagePrinters})
	sender := User{Name: "Asha", Email: "asha@x", Status: UserActive, GroupID: students.ID}
	keeper := User{Name: "Keeper", Email: "keeper@x", Status: UserActive, GroupID: admins.ID}
	db.Create(&sender)
	db.Create(&keeper)

	recordPrintUpload(db, "printer-2", "asha_bracket.gcode.3mf", sender.ID)
	old := PrintUpload{PrinterID: "printer-2", NameKey: "old_part", FileName: "old_part.3mf", UserID: sender.ID}
	db.Create(&old)
	db.Model(&old).Update("created_at", time.Now().Add(-30*24*time.Hour))

	count := func(userID uint) (n int64) {
		db.Model(&Notification{}).Where("user_id = ?", userID).Count(&n)
		return
	}

	announcePrintEnd(db, PrintJob{PrinterID: "printer-2", PrinterName: "Printer 2", FileName: "asha_bracket", Result: "finished"})
	if count(sender.ID) != 1 || count(keeper.ID) != 0 {
		t.Fatalf("finished: sender %d, keeper %d notifications; want 1, 0", count(sender.ID), count(keeper.ID))
	}

	announcePrintEnd(db, PrintJob{PrinterID: "printer-2", PrinterName: "Printer 2", FileName: "asha_bracket.gcode.3mf", Result: "failed", LastPercent: 30})
	if count(sender.ID) != 2 || count(keeper.ID) != 1 {
		t.Fatalf("failed: sender %d, keeper %d; want 2, 1", count(sender.ID), count(keeper.ID))
	}

	// Another printer, an unknown file, and an upload past the window notify
	// nobody but the printer admins on failure.
	announcePrintEnd(db, PrintJob{PrinterID: "printer-1", PrinterName: "Printer 1", FileName: "asha_bracket", Result: "finished"})
	announcePrintEnd(db, PrintJob{PrinterID: "printer-2", PrinterName: "Printer 2", FileName: "old_part", Result: "finished"})
	if count(sender.ID) != 2 {
		t.Fatalf("other printer or old upload notified the sender: %d", count(sender.ID))
	}
}
