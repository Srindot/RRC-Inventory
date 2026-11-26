package main

// Notifications when a print ends. Prints are started on the printer, so the
// website only knows whose a print is if they sent the file from the website:
// each upload is remembered here, and when the print log closes a job with
// the same file on the same printer, its sender hears how it went. Failures
// also go to the people who look after the printers.

import (
	"fmt"
	"log"
	"path"
	"strings"
	"time"

	"gorm.io/gorm"
)

// PrintUpload records who sent which file to which printer.
type PrintUpload struct {
	gorm.Model
	PrinterID string `gorm:"index:idx_print_upload_key"`
	NameKey   string `gorm:"index:idx_print_upload_key"`
	FileName  string
	UserID    uint
}

// An upload older than this is not taken as the sender of a print, so a file
// printed again weeks later doesn't notify whoever first sent it.
const printUploadWindow = 14 * 24 * time.Hour

// printNameKey reduces a file name to what the print log and the upload
// have in common: the printer reports the plain name, the upload carries the
// extension, and either may differ in case.
func printNameKey(name string) string {
	key := strings.ToLower(path.Base(strings.TrimSpace(name)))
	for _, ext := range []string{".gcode.3mf", ".3mf", ".gcode"} {
		if strings.HasSuffix(key, ext) {
			return strings.TrimSuffix(key, ext)
		}
	}
	return key
}

func recordPrintUpload(db *gorm.DB, printerID, fileName string, userID uint) {
	upload := PrintUpload{PrinterID: printerID, NameKey: printNameKey(fileName), FileName: fileName, UserID: userID}
	if err := db.Create(&upload).Error; err != nil {
		log.Printf("printer %s: could not remember who sent %s: %v", printerID, fileName, err)
	}
}

// printEndMessage is the notification text for a job that has ended, or ""
// for results nobody needs telling about.
func printEndMessage(job PrintJob) string {
	name := printNameKey(job.FileName)
	switch job.Result {
	case "finished":
		return fmt.Sprintf("✅ %s finished %s - collect it and clear the plate", job.PrinterName, name)
	case "failed":
		return fmt.Sprintf("⚠️ %s failed while printing %s (at %d%%) - check the printer", job.PrinterName, name, job.LastPercent)
	case "stopped":
		if job.StoppedBy != "" {
			return fmt.Sprintf("⏹️ %s stopped %s on %s at %d%%", job.StoppedBy, name, job.PrinterName, job.LastPercent)
		}
		return fmt.Sprintf("⏹️ %s stopped printing %s at %d%%", job.PrinterName, name, job.LastPercent)
	}
	return ""
}

// announcePrintEnd tells the sender of the file (if it came through the
// website) how the print ended, and printer admins about failures.
func announcePrintEnd(db *gorm.DB, job PrintJob) {
	text := printEndMessage(job)
	if text == "" {
		return
	}

	var recipients []uint
	var upload PrintUpload
	err := db.Where("printer_id = ? AND name_key = ? AND created_at > ?",
		job.PrinterID, printNameKey(job.FileName), time.Now().Add(-printUploadWindow)).
		Order("created_at DESC").First(&upload).Error
	if err == nil {
		recipients = append(recipients, upload.UserID)
	}
	if job.Result == "failed" {
		for _, id := range usersWithPerm(db, PermManagePrinters) {
			if id != upload.UserID {
				recipients = append(recipients, id)
			}
		}
	}
	notify(db, recipients, text, "/printers")
}
