package main

// The FTPS layer to a real printer cannot be tested here - that needs the
// hardware. What is tested is everything around it: name sanitisation (the
// security-relevant part) and the actual FTP command sequence, run against a
// real FTP server in plaintext mode.

import (
	"bytes"
	"strings"
	"testing"
)

func TestSanitizeUploadName(t *testing.T) {
	good := map[string]string{
		"bracket.3mf":               "bracket.3mf",
		"srinath_bracket.gcode.3mf": "srinath_bracket.gcode.3mf",
		"plate 1.gcode":             "plate_1.gcode",
		"  spaced name.3mf  ":       "spaced_name.3mf",
		"weird!@#$chars.3mf":        "weirdchars.3mf",
	}
	for input, want := range good {
		got, err := sanitizeUploadName(input)
		if err != nil {
			t.Errorf("sanitizeUploadName(%q) errored: %v", input, err)
			continue
		}
		if got != want {
			t.Errorf("sanitizeUploadName(%q) = %q, want %q", input, got, want)
		}
	}

	// Path traversal must never survive - this is the one that matters
	traversal := []string{
		"../../etc/passwd.3mf",
		"..\\..\\windows\\evil.3mf",
		"/absolute/path/thing.3mf",
		"subdir/nested.gcode",
	}
	for _, input := range traversal {
		got, err := sanitizeUploadName(input)
		if err != nil {
			continue // rejecting outright is fine too
		}
		if strings.ContainsAny(got, "/\\") {
			t.Errorf("sanitizeUploadName(%q) = %q, still contains a path", input, got)
		}
	}

	// Wrong types and nonsense must be refused
	for _, input := range []string{
		"notes.txt", "image.png", "script.sh", "firmware.bin",
		"", "   ", "..", "/", ".3mf",
	} {
		if got, err := sanitizeUploadName(input); err == nil {
			t.Errorf("sanitizeUploadName(%q) should have failed, got %q", input, got)
		}
	}
}

func TestSanitizeUploadNameLength(t *testing.T) {
	long := strings.Repeat("a", 300) + ".3mf"
	got, err := sanitizeUploadName(long)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) > 120 {
		t.Errorf("name not shortened: %d chars", len(got))
	}
	if !strings.HasSuffix(got, ".3mf") {
		t.Errorf("shortening must keep the extension, got %q", got)
	}
}

// A long sliced plate must keep the whole ".gcode.3mf" suffix. Shortening into
// the suffix leaves something like "....g.3mf", which the printer's screen
// browser lists as an unsliced project and refuses to start.
func TestSanitizeUploadNameLongSlicedPlateKeepsGcodeSuffix(t *testing.T) {
	long := strings.Repeat("a", 300) + ".gcode.3mf"
	got, err := sanitizeUploadName(long)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) > 120 {
		t.Errorf("name not shortened: %d chars", len(got))
	}
	if !strings.HasSuffix(got, ".gcode.3mf") {
		t.Errorf("shortening ate the .gcode suffix, got %q", got)
	}
}

// A name with nothing left after cleaning must be refused rather than collapse
// to the bare suffix, where every such upload would overwrite the last one.
func TestSanitizeUploadNameRejectsEmptyBase(t *testing.T) {
	for _, input := range []string{
		"पुर्जा.gcode.3mf",
		"零件.3mf",
		"___.gcode.3mf",
		".gcode.3mf",
	} {
		got, err := sanitizeUploadName(input)
		if err == nil {
			t.Errorf("sanitizeUploadName(%q) should have failed, got %q", input, got)
		}
	}
}

// Exercises the real FTP command sequence (login, STOR, LIST, DELE) against a
// live server. Skipped unless RRC_FTP_TEST_ADDR points at one.
func TestUploadListDeleteAgainstFTPServer(t *testing.T) {
	addr := ftpTestAddr(t)
	host, port := splitHostPort(t, addr)

	p := &printer{
		cfg:          PrinterConfig{Name: "mock", Host: host, AccessCode: "89a8541a"},
		ftpPort:      port,
		ftpPlaintext: true,
	}

	contents := []byte("sliced plate, pretend this is a 3mf")
	if err := p.UploadFile("srinath_bracket.gcode.3mf", bytes.NewReader(contents)); err != nil {
		t.Fatalf("upload failed: %v", err)
	}

	files, err := p.ListFiles()
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}

	var found *PrinterFile
	for i := range files {
		if files[i].Name == "srinath_bracket.gcode.3mf" {
			found = &files[i]
		}
	}
	if found == nil {
		t.Fatalf("uploaded file missing from the listing: %+v", files)
	}
	if found.Size != uint64(len(contents)) {
		t.Errorf("listed size %d, uploaded %d bytes", found.Size, len(contents))
	}

	// Files that are not printable should not be listed
	for _, f := range files {
		lower := strings.ToLower(f.Name)
		if !strings.HasSuffix(lower, ".3mf") && !strings.HasSuffix(lower, ".gcode") {
			t.Errorf("listing included a non-printable file: %q", f.Name)
		}
	}

	if err := p.DeleteFile("srinath_bracket.gcode.3mf"); err != nil {
		t.Fatalf("delete failed: %v", err)
	}

	after, err := p.ListFiles()
	if err != nil {
		t.Fatalf("second list failed: %v", err)
	}
	for _, f := range after {
		if f.Name == "srinath_bracket.gcode.3mf" {
			t.Error("file still present after delete")
		}
	}
}

// Goes through the manager, which is what the HTTP layer actually calls -
// including the sanitisation step and the unknown-printer case.
func TestManagerUploadPath(t *testing.T) {
	addr := ftpTestAddr(t)
	host, port := splitHostPort(t, addr)

	m := &PrinterManager{byID: map[string]*printer{
		"printer-1": {
			cfg:          PrinterConfig{Name: "Printer 1", Host: host, AccessCode: "89a8541a"},
			ftpPort:      port,
			ftpPlaintext: true,
		},
	}}

	// A browser sends the whole path on some platforms; the manager must
	// reduce it to a bare name before it reaches the printer
	name, err := m.UploadFile("printer-1", "C:\\Users\\srinath\\Desktop\\srinath bracket.3mf",
		bytes.NewReader([]byte("plate")))
	if err != nil {
		t.Fatalf("upload through the manager failed: %v", err)
	}
	if name != "srinath_bracket.3mf" {
		t.Errorf("stored name = %q, want srinath_bracket.3mf", name)
	}

	files, err := m.ListFiles("printer-1")
	if err != nil {
		t.Fatalf("list failed: %v", err)
	}

	var names []string
	for _, f := range files {
		names = append(names, f.Name)
	}
	if !contains(names, "srinath_bracket.3mf") {
		t.Errorf("uploaded file missing: %v", names)
	}
	// The junk the real printer had in its listing must not show up
	if contains(names, "notes.txt") || contains(names, "._junk.gcode.3mf") {
		t.Errorf("listing should hide non-printable and sidecar files: %v", names)
	}

	if err := m.DeleteFile("printer-1", "srinath_bracket.3mf"); err != nil {
		t.Errorf("delete failed: %v", err)
	}

	// Unknown printers must be refused rather than panicking
	if _, err := m.UploadFile("nope", "x.3mf", bytes.NewReader(nil)); err == nil {
		t.Error("expected an error for an unknown printer")
	}

	// The wrong sort of file never reaches the printer at all
	if _, err := m.UploadFile("printer-1", "notes.txt", bytes.NewReader(nil)); err == nil {
		t.Error("expected .txt to be refused")
	}
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}
