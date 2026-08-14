package main

// End-to-end checks of the upload path against a fake printer, covering the
// failure modes seen in the lab: uploads hanging at 100%, and files that reach
// the card incomplete and then show "--" for time and filament.

import (
	"bytes"
	"crypto/sha256"
	"strings"
	"testing"
	"time"
)

func fakePrinter(f *fakePrinterFTP) *printer {
	return &printer{
		cfg:          PrinterConfig{Name: "fake", Host: "127.0.0.1", AccessCode: "0000"},
		ftpPort:      f.port(),
		ftpPlaintext: true,
		ftpShut:      2 * time.Second,
	}
}

// A sliced plate must arrive byte for byte. Anything else and the printer reads
// garbage metadata.
func TestUploadDeliversIdenticalBytes(t *testing.T) {
	fake := newFakePrinterFTP(t)
	p := fakePrinter(fake)

	// Big enough to span many writes, and deliberately full of bytes that a
	// text-mode transfer would mangle
	contents := make([]byte, 512*1024)
	for i := range contents {
		contents[i] = byte(i % 256)
	}

	if err := p.UploadFile("plate.gcode.3mf", bytes.NewReader(contents)); err != nil {
		t.Fatalf("upload failed: %v", err)
	}

	got, ok := fake.stored("plate.gcode.3mf")
	if !ok {
		t.Fatal("nothing was stored")
	}
	if sha256.Sum256(got) != sha256.Sum256(contents) {
		t.Errorf("contents differ: stored %d bytes, sent %d", len(got), len(contents))
	}
}

// The bug behind "the bar sits at 100% forever": the printer never acknowledges
// the finished transfer, and without a shut timeout the upload never returns.
func TestUploadFailsInsteadOfHangingWhenPrinterGoesQuiet(t *testing.T) {
	fake := newFakePrinterFTP(t)
	fake.withholdShutStatus = true

	p := fakePrinter(fake)

	done := make(chan error, 1)
	go func() {
		done <- p.UploadFile("plate.gcode.3mf", bytes.NewReader([]byte("sliced plate")))
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected an error when the printer never acknowledges the transfer")
		}
	case <-time.After(20 * time.Second):
		t.Fatal("upload hung - the shut timeout is not being applied")
	}
}

// A transfer that dies part way must not leave the half file sitting on the
// card under a name somebody is about to pick on the printer's screen.
func TestUploadRemovesPartialFile(t *testing.T) {
	fake := newFakePrinterFTP(t)
	fake.truncateAfter = 64
	fake.withholdShutStatus = true

	p := fakePrinter(fake)

	contents := bytes.Repeat([]byte("x"), 8192)
	err := p.UploadFile("plate.gcode.3mf", bytes.NewReader(contents))
	if err == nil {
		t.Fatal("a truncated upload must report an error")
	}

	deleted := fake.deleted()
	found := false
	for _, name := range deleted {
		if name == "plate.gcode.3mf" {
			found = true
		}
	}
	if !found {
		t.Errorf("partial file was left on the printer; deletes seen: %v", deleted)
	}
}

// When the printer does answer but stored fewer bytes than we sent, the size
// check must catch it and clear the file.
func TestUploadDetectsSizeMismatch(t *testing.T) {
	fake := newFakePrinterFTP(t)
	fake.truncateAfter = 100

	p := fakePrinter(fake)

	contents := bytes.Repeat([]byte("y"), 4096)
	err := p.UploadFile("plate.gcode.3mf", bytes.NewReader(contents))
	if err == nil {
		t.Fatal("expected a size mismatch to be reported")
	}
	if !strings.Contains(err.Error(), "reached the printer") {
		t.Errorf("unhelpful error for a short upload: %v", err)
	}

	if _, ok := fake.stored("plate.gcode.3mf"); ok {
		t.Error("short file is still on the printer")
	}
}

// The whole path the HTTP handler uses, including sanitisation.
func TestManagerUploadEndToEnd(t *testing.T) {
	fake := newFakePrinterFTP(t)
	m := &PrinterManager{byID: map[string]*printer{"p1": fakePrinter(fake)}}

	long := strings.Repeat("Quadcopter_Arm_", 12) + "final.gcode.3mf"
	name, err := m.UploadFile("p1", "C:\\Users\\srinath\\Desktop\\"+long,
		bytes.NewReader([]byte("sliced plate")))
	if err != nil {
		t.Fatalf("upload failed: %v", err)
	}

	if !strings.HasSuffix(name, ".gcode.3mf") {
		t.Errorf("sliced plate lost its suffix on the way to the printer: %q", name)
	}
	if strings.ContainsAny(name, "/\\") {
		t.Errorf("path survived sanitisation: %q", name)
	}
	if _, ok := fake.stored(name); !ok {
		t.Errorf("file %q never reached the printer", name)
	}

	// It must be listed back under the same name, which is what the user checks
	// against the printer's screen
	files, err := m.ListFiles("p1")
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}
	found := false
	for _, f := range files {
		if f.Name == name {
			found = true
		}
	}
	if !found {
		t.Errorf("%q missing from the listing: %+v", name, files)
	}
}

// An upload must not overwrite the file the printer is reading right now.
func TestUploadRefusedWhilePrinting(t *testing.T) {
	fake := newFakePrinterFTP(t)
	p := fakePrinter(fake)
	p.state = "RUNNING"
	p.fileName = "bracket"

	m := &PrinterManager{byID: map[string]*printer{"p1": p}}

	if _, err := m.UploadFile("p1", "bracket.gcode.3mf", bytes.NewReader([]byte("x"))); err == nil {
		t.Error("overwriting the running job was allowed")
	}

	// A different file is fine
	if _, err := m.UploadFile("p1", "other.gcode.3mf", bytes.NewReader([]byte("x"))); err != nil {
		t.Errorf("unrelated upload blocked: %v", err)
	}

	// And so is the same name once the printer is idle
	p.state = "IDLE"
	if _, err := m.UploadFile("p1", "bracket.gcode.3mf", bytes.NewReader([]byte("x"))); err != nil {
		t.Errorf("upload blocked on an idle printer: %v", err)
	}
}
