package main

// End-to-end checks of the upload path against a fake printer, covering the
// failure modes seen in the lab: uploads hanging at 100%, and files that reach
// the card incomplete and then show "--" for time and filament.

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"io"
	"strings"
	"sync"
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

// markOnline makes a test printer look as though it has just reported.
func markOnline(p *printer) {
	p.mu.Lock()
	p.lastReport = time.Now()
	p.mu.Unlock()
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

	if err := p.UploadFile("plate.gcode.3mf", bytes.NewReader(contents), 0); err != nil {
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
		done <- p.UploadFile("plate.gcode.3mf", bytes.NewReader([]byte("sliced plate")), 0)
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
	err := p.UploadFile("plate.gcode.3mf", bytes.NewReader(contents), 0)
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
	err := p.UploadFile("plate.gcode.3mf", bytes.NewReader(contents), 0)
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
	p := fakePrinter(fake)
	markOnline(p)
	m := &PrinterManager{byID: map[string]*printer{"p1": p}}

	long := strings.Repeat("Quadcopter_Arm_", 12) + "final.gcode.3mf"
	name, err := m.UploadFile("p1", "C:\\Users\\srinath\\Desktop\\"+long,
		bytes.NewReader([]byte("sliced plate")), 0)
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

	if _, err := m.UploadFile("p1", "bracket.gcode.3mf", bytes.NewReader([]byte("x")), 0); err == nil {
		t.Error("overwriting the running job was allowed")
	}

	// A different file is fine
	if _, err := m.UploadFile("p1", "other.gcode.3mf", bytes.NewReader([]byte("x")), 0); err != nil {
		t.Errorf("unrelated upload blocked: %v", err)
	}

	// And so is the same name once the printer is idle
	p.state = "IDLE"
	if _, err := m.UploadFile("p1", "bracket.gcode.3mf", bytes.NewReader([]byte("x")), 0); err != nil {
		t.Errorf("upload blocked on an idle printer: %v", err)
	}
}

// The half-printed job: a body that was cut short on its way to the server.
// Everything downstream is consistent - we copy what we got, the printer stores
// exactly that, and the sizes agree - so only the size the browser declared
// catches it. Left unchecked the printer runs the gcode it has and reports the
// job finished, part way up the model.
func TestUploadRejectsShortBrowserBody(t *testing.T) {
	fake := newFakePrinterFTP(t)
	p := fakePrinter(fake)

	full := int64(8192)
	arrived := bytes.Repeat([]byte("z"), 4096) // the browser leg died at half

	err := p.UploadFile("plate.gcode.3mf", bytes.NewReader(arrived), full)
	if err == nil {
		t.Fatal("a half sized body was accepted")
	}
	if !strings.Contains(err.Error(), "from the browser") {
		t.Errorf("error should name the browser leg, got: %v", err)
	}

	if _, ok := fake.stored("plate.gcode.3mf"); ok {
		t.Error("half file left on the printer, ready to print half a model")
	}
}

// A printer that refuses SIZE must not silently skip verification: the listing
// carries the size too, and that is what the fallback uses.
func TestUploadVerifiesViaListingWhenSizeUnsupported(t *testing.T) {
	fake := newFakePrinterFTP(t)
	fake.refuseSize = true
	fake.truncateAfter = 100

	p := fakePrinter(fake)

	err := p.UploadFile("plate.gcode.3mf", bytes.NewReader(bytes.Repeat([]byte("q"), 4096)), 0)
	if err == nil {
		t.Fatal("short file accepted because SIZE was unsupported")
	}
	if !strings.Contains(err.Error(), "reached the printer") {
		t.Errorf("unexpected error: %v", err)
	}
}

// An upload must never replace a file already on the card - usually somebody
// else's plate that happens to share a generic name. The FAT card ignores
// case, so neither may the check.
func TestUploadRefusesToOverwrite(t *testing.T) {
	fake := newFakePrinterFTP(t)
	p := fakePrinter(fake)
	fake.put("bracket.gcode.3mf", []byte("someone else's plate"))

	for _, name := range []string{"bracket.gcode.3mf", "Bracket.gcode.3mf"} {
		err := p.UploadFile(name, bytes.NewReader([]byte("mine")), 4)
		if !errors.Is(err, ErrFileExists) {
			t.Errorf("upload of %q over an existing file: got %v, want ErrFileExists", name, err)
			continue
		}
		// The HTTP layer shows this message as it is
		if !strings.Contains(err.Error(), "rename") {
			t.Errorf("error does not say what to do: %v", err)
		}
	}

	if got, _ := fake.stored("bracket.gcode.3mf"); string(got) != "someone else's plate" {
		t.Errorf("existing file was changed to %q", got)
	}
	if deleted := fake.deleted(); len(deleted) != 0 {
		t.Errorf("refusing an overwrite must not delete anything, saw %v", deleted)
	}

	// Through the manager too, which is what the handler calls
	m := &PrinterManager{byID: map[string]*printer{"p1": p}}
	if _, err := m.UploadFile("p1", "bracket.gcode.3mf", bytes.NewReader([]byte("x")), 1); !errors.Is(err, ErrFileExists) {
		t.Errorf("manager upload: got %v, want ErrFileExists", err)
	}
}

// PREPARE (heating and levelling) and SLICING come before RUNNING, and the
// printer already has the file open in both.
func TestPreparingCountsAsPrinting(t *testing.T) {
	fake := newFakePrinterFTP(t)
	p := fakePrinter(fake)
	p.fileName = "bracket"
	m := &PrinterManager{byID: map[string]*printer{"p1": p}}

	for _, state := range []string{"PREPARE", "SLICING", "RUNNING", "PAUSE"} {
		p.state = state
		if !p.isPrinting("bracket.gcode.3mf") {
			t.Errorf("%s should count as printing", state)
		}
		if _, err := m.UploadFile("p1", "bracket.gcode.3mf", bytes.NewReader([]byte("x")), 1); err == nil {
			t.Errorf("upload over the job's file allowed while %s", state)
		}
	}

	for _, state := range []string{"IDLE", "FINISH", "FAILED", ""} {
		p.state = state
		if p.isPrinting("bracket.gcode.3mf") {
			t.Errorf("%s should not count as printing", state)
		}
	}
}

// zeroReader is an endless upload that costs no memory.
type zeroReader struct{}

func (zeroReader) Read(b []byte) (int, error) {
	clear(b)
	return len(b), nil
}

// A printer whose network disappears mid-transfer stops reading. Without an
// idle deadline the upload sits in the kernel for a quarter of an hour; with
// one it fails once nothing has moved for ftpIdle.
func TestUploadFailsWhenTransferStalls(t *testing.T) {
	fake := newFakePrinterFTP(t)
	fake.stallStor = true

	p := fakePrinter(fake)
	p.ftpIdle = time.Second

	done := make(chan error, 1)
	go func() {
		// Far more than the socket buffers can absorb
		body := io.LimitReader(zeroReader{}, 1<<30)
		done <- p.UploadFile("plate.gcode.3mf", body, 0)
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("a stalled transfer reported success")
		}
	case <-time.After(15 * time.Second):
		t.Fatal("upload hung on a stalled transfer - the idle deadline is not applied")
	}
}

// The public file listing must not spend the dial timeout on a printer that
// is plainly switched off.
func TestListFilesFailsFastWhenOffline(t *testing.T) {
	fake := newFakePrinterFTP(t)
	p := fakePrinter(fake)
	m := &PrinterManager{byID: map[string]*printer{"p1": p}}

	_, err := m.ListFiles("p1")
	if !errors.Is(err, ErrPrinterOffline) {
		t.Errorf("got %v, want ErrPrinterOffline", err)
	}
	if fake.loginCount() != 0 {
		t.Errorf("an offline printer was still dialled (%d logins)", fake.loginCount())
	}
}

// Listings are shared for a few seconds, so a room full of browsers does not
// take every FTP session the printer has - but anything that changes the card
// must show up at once.
func TestListFilesCacheAndInvalidation(t *testing.T) {
	fake := newFakePrinterFTP(t)
	p := fakePrinter(fake)
	markOnline(p)
	m := &PrinterManager{byID: map[string]*printer{"p1": p}}

	fake.put("a.gcode.3mf", []byte("x"))

	// Many at once: one session between them
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			files, err := m.ListFiles("p1")
			if err != nil || len(files) != 1 {
				t.Errorf("listing: %v %+v", err, files)
			}
		}()
	}
	wg.Wait()
	if n := fake.loginCount(); n != 1 {
		t.Errorf("%d concurrent listings made %d FTP logins, want 1", 8, n)
	}

	// A file that appears behind our back is not seen until the cache expires
	fake.put("b.gcode.3mf", []byte("x"))
	if files, _ := m.ListFiles("p1"); len(files) != 1 {
		t.Errorf("cached listing should still have 1 file, has %d", len(files))
	}

	// An upload of our own invalidates it
	if _, err := m.UploadFile("p1", "c.gcode.3mf", bytes.NewReader([]byte("x")), 1); err != nil {
		t.Fatalf("upload: %v", err)
	}
	files, err := m.ListFiles("p1")
	if err != nil || len(files) != 3 {
		t.Errorf("after upload want 3 files, got %d (%v)", len(files), err)
	}

	// And so does a delete
	if err := m.DeleteFile("p1", "a.gcode.3mf"); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if files, _ := m.ListFiles("p1"); len(files) != 2 {
		t.Errorf("after delete want 2 files, got %d", len(files))
	}
}

// Every session holds a slot; with them all taken a request gives up with a
// clear message instead of piling onto the printer.
func TestFTPSessionsAreLimited(t *testing.T) {
	fake := newFakePrinterFTP(t)
	p := fakePrinter(fake)

	var held []*ftpSession
	for i := 0; i < ftpMaxSessions; i++ {
		conn, err := p.connectFTP()
		if err != nil {
			t.Fatalf("session %d: %v", i, err)
		}
		held = append(held, conn)
	}

	// Releasing one lets the next through without waiting out ftpSlotWait
	go func() {
		time.Sleep(200 * time.Millisecond)
		held[0].Close()
	}()
	start := time.Now()
	conn, err := p.connectFTP()
	if err != nil {
		t.Fatalf("waiting for a free slot failed: %v", err)
	}
	if time.Since(start) < 150*time.Millisecond {
		t.Error("a session was opened while every slot was taken")
	}
	conn.Close()
	for _, c := range held[1:] {
		c.Close()
	}
}
