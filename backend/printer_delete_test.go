package main

// Deleting files off a printer, including the names this code did not choose.

import (
	"bytes"
	"strings"
	"testing"
)

// The bug this pins: DeleteFile used to push the name through
// sanitizeUploadName, so a file Bambu Studio left with a space or a bracket in
// its name was never the file that got deleted. The listing kept showing it.
func TestDeleteFileWithNameThisCodeWouldNotChoose(t *testing.T) {
	fake := newFakePrinterFTP(t)
	p := fakePrinter(fake)

	for _, name := range []string{
		"Benchy (1).gcode.3mf",
		"bracket v2.gcode.3mf",
		"Nozzle#3.3mf",
	} {
		fake.put(name, []byte("plate"))

		if err := p.DeleteFile(name); err != nil {
			t.Errorf("DeleteFile(%q) failed: %v", name, err)
			continue
		}
		if _, still := fake.stored(name); still {
			t.Errorf("%q is still on the printer after a successful delete", name)
		}
	}
}

func TestDeleteFileReportsAMissingFile(t *testing.T) {
	fake := newFakePrinterFTP(t)
	p := fakePrinter(fake)

	err := p.DeleteFile("never-existed.gcode.3mf")
	if err == nil || !strings.Contains(err.Error(), "not on the printer") {
		t.Errorf("expected a missing-file error, got %v", err)
	}
}

// A name has to match a real listing entry, so nothing outside the directory
// can be reached even though the name is no longer sanitised.
func TestDeleteFileRefusesPaths(t *testing.T) {
	fake := newFakePrinterFTP(t)
	p := fakePrinter(fake)

	for _, bad := range []string{
		"../../etc/passwd", "..", ".", "", "sub/dir.3mf", `..\win.3mf`,
	} {
		if err := p.DeleteFile(bad); err == nil {
			t.Errorf("DeleteFile(%q) was allowed", bad)
		}
	}
}

// Dotfiles and non-printable files are not shown, so they must not be
// deletable through this route either.
func TestDeleteFileIgnoresUnlistedFiles(t *testing.T) {
	fake := newFakePrinterFTP(t)
	p := fakePrinter(fake)

	fake.put("._sidecar.3mf", []byte("x"))
	fake.put("notes.txt", []byte("x"))

	for _, name := range []string{"._sidecar.3mf", "notes.txt"} {
		if err := p.DeleteFile(name); err == nil {
			t.Errorf("DeleteFile(%q) was allowed", name)
		}
		if _, still := fake.stored(name); !still {
			t.Errorf("%q was deleted even though it is not printable", name)
		}
	}
}

func TestDeleteAllFiles(t *testing.T) {
	fake := newFakePrinterFTP(t)
	p := fakePrinter(fake)
	p.state = "IDLE"

	if err := p.UploadFile("a.gcode.3mf", bytes.NewReader([]byte("x")), 0); err != nil {
		t.Fatalf("upload failed: %v", err)
	}
	fake.put("Old Plate (2).gcode.3mf", []byte("x"))
	fake.put("notes.txt", []byte("keep me"))
	fake.put("._sidecar.3mf", []byte("keep me"))

	deleted, err := p.DeleteAllFiles()
	if err != nil {
		t.Fatalf("clear failed: %v", err)
	}
	if deleted != 2 {
		t.Errorf("deleted %d, want 2", deleted)
	}

	// Everything printable gone, everything else untouched
	if fake.count() != 2 {
		t.Errorf("%d files left, want the 2 non-printable ones", fake.count())
	}
	for _, keep := range []string{"notes.txt", "._sidecar.3mf"} {
		if _, ok := fake.stored(keep); !ok {
			t.Errorf("%q should not have been deleted", keep)
		}
	}
}

// Clearing the card mid-job would pull the file out from under the printer.
func TestDeleteAllFilesRefusedWhilePrinting(t *testing.T) {
	fake := newFakePrinterFTP(t)
	p := fakePrinter(fake)
	fake.put("a.gcode.3mf", []byte("x"))

	for _, state := range []string{"RUNNING", "PAUSE", "PREPARE"} {
		p.state = state
		if _, err := p.DeleteAllFiles(); err == nil {
			t.Errorf("clear allowed while %s", state)
		}
		if _, still := fake.stored("a.gcode.3mf"); !still {
			t.Fatalf("file deleted while %s", state)
		}
	}
}

// Deleting the running job's file through the manager must be refused too.
func TestDeleteFileRefusedWhilePrintingThatFile(t *testing.T) {
	fake := newFakePrinterFTP(t)
	p := fakePrinter(fake)
	p.state = "RUNNING"
	p.fileName = "bracket"
	fake.put("bracket.gcode.3mf", []byte("x"))
	fake.put("other.gcode.3mf", []byte("x"))

	m := &PrinterManager{byID: map[string]*printer{"p1": p}}

	if err := m.DeleteFile("p1", "bracket.gcode.3mf"); err == nil {
		t.Error("deleting the running job's file was allowed")
	}
	if err := m.DeleteFile("p1", "other.gcode.3mf"); err != nil {
		t.Errorf("deleting an unrelated file was blocked: %v", err)
	}
}
