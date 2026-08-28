package main

// Sending sliced files to a printer.
//
// Bambu printers run an FTPS server on port 990 using implicit TLS - the
// connection is encrypted from the first byte rather than starting plain and
// upgrading. Logging in with the LAN access code puts a file on the printer's
// storage, which is exactly what Bambu Studio does behind its Print button.
//
// This deliberately only *uploads*. Nothing here starts a print: somebody
// walks to the machine, checks the plate is clear, and picks the file on the
// screen. That keeps a human in the loop for the one action that can crash a
// toolhead into somebody else's finished print.

import (
	"crypto/tls"
	"fmt"
	"io"
	"log"
	"path"
	"strings"
	"time"

	"github.com/jlaffaye/ftp"
)

const (
	printerFTPPort = 990
	// A sliced plate is usually a few MB; 3mf files with embedded previews can
	// reach tens. This is a sanity limit, not a target.
	maxUploadBytes = 200 * 1024 * 1024
	// Time allowed to reach the printer. This caps connecting only - the FTP
	// library applies it to the dial, not to the transfer.
	ftpTimeout = 60 * time.Second
	// Time allowed for the printer to acknowledge a finished transfer.
	//
	// The control connection sits idle while the file goes over the data
	// connection, and the printer drops it if that takes long enough. The
	// client then waits for a "closing data connection" reply that is never
	// coming, with no deadline of its own: the upload reaches 100%, and hangs
	// there. Small files beat the idle timeout and work, which is why this
	// looks intermittent.
	ftpShutTimeout = 30 * time.Second
)

// allowedUploadSuffixes are the only things worth putting on a printer, longest
// first so ".gcode.3mf" is recognised before the ".3mf" it ends with.
//
// ".gcode.3mf" is one suffix, not an extension sitting on a base name ending in
// ".gcode". A sliced plate whose name loses the ".gcode" part still uploads and
// still lists on the printer's screen, but the screen browser reads it as an
// unsliced project and selecting it does nothing.
var allowedUploadSuffixes = []string{".gcode.3mf", ".3mf", ".gcode"}

// maxUploadNameLength is what the printer's own file browser copes with.
const maxUploadNameLength = 120

// splitUploadSuffix separates a file name into its base and one of the allowed
// suffixes, matching the suffix case-insensitively but returning it as written.
func splitUploadSuffix(name string) (base, suffix string, ok bool) {
	lower := strings.ToLower(name)
	for _, candidate := range allowedUploadSuffixes {
		if strings.HasSuffix(lower, candidate) {
			cut := len(name) - len(candidate)
			return name[:cut], name[cut:], true
		}
	}
	return "", "", false
}

// sanitizeUploadName reduces whatever the browser sent to a plain, safe file
// name. Directory components are stripped, so an upload cannot escape the
// printer's upload directory.
//
// Only the base name is cleaned or shortened; the suffix is carried through
// untouched.
func sanitizeUploadName(raw string) (string, error) {
	name := strings.TrimSpace(raw)

	// Drop any path the browser included, both separators
	name = strings.ReplaceAll(name, "\\", "/")
	name = path.Base(name)

	if name == "" || name == "." || name == ".." || name == "/" {
		return "", fmt.Errorf("that file name is not usable")
	}

	base, suffix, ok := splitUploadSuffix(name)
	if !ok {
		return "", fmt.Errorf("only .3mf and .gcode files can be sent to a printer")
	}

	// Anything outside this set risks confusing the printer's own file browser
	var cleaned strings.Builder
	for _, r := range base {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			cleaned.WriteRune(r)
		case r == '.' || r == '-' || r == '_':
			cleaned.WriteRune(r)
		case r == ' ':
			cleaned.WriteRune('_')
		}
	}

	base = strings.Trim(cleaned.String(), "._-")

	// A name written entirely in characters we strip - any non-Latin script -
	// would otherwise clean down to nothing and leave every such upload sharing
	// the bare suffix as its name, each one overwriting the last.
	if base == "" {
		return "", fmt.Errorf(
			"that file name is not usable - please rename it using letters and numbers")
	}

	// Shorten the base, never the suffix
	if len(base)+len(suffix) > maxUploadNameLength {
		base = strings.Trim(base[:maxUploadNameLength-len(suffix)], "._-")
		if base == "" {
			return "", fmt.Errorf("that file name is not usable")
		}
	}

	return base + suffix, nil
}

// ftpAddress is the printer's FTPS endpoint. The port is a field so tests can
// point at a local server.
func (p *printer) ftpAddress() string {
	port := p.ftpPort
	if port == 0 {
		port = printerFTPPort
	}
	return fmt.Sprintf("%s:%d", p.cfg.Host, port)
}

// connectFTP opens an authenticated FTP session with the printer.
func (p *printer) connectFTP() (*ftp.ServerConn, error) {
	shut := p.ftpShut
	if shut == 0 {
		shut = ftpShutTimeout
	}

	options := []ftp.DialOption{
		ftp.DialWithTimeout(ftpTimeout),
		// Without this an upload that outlives the printer's control-connection
		// idle timeout blocks forever instead of failing.
		ftp.DialWithShutTimeout(shut),
	}

	// Printers use implicit TLS with a self-signed certificate. Tests run
	// against a plain server, so this is switchable.
	if !p.ftpPlaintext {
		options = append(options, ftp.DialWithTLS(&tls.Config{
			InsecureSkipVerify: true, // #nosec G402 - self-signed printer cert
		}))
	}

	conn, err := ftp.Dial(p.ftpAddress(), options...)
	if err != nil {
		return nil, fmt.Errorf("could not connect to the printer's file service: %w", err)
	}

	if err := conn.Login("bblp", p.accessCode()); err != nil {
		conn.Quit()
		return nil, fmt.Errorf("the printer rejected our access code: %w", err)
	}

	return conn, nil
}

// countingReader records how many bytes were handed to the FTP session, so the
// upload can be checked against what actually landed on the printer.
type countingReader struct {
	inner io.Reader
	n     int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	read, err := c.inner.Read(p)
	c.n += int64(read)
	return read, err
}

// removePartial deletes a half-written upload, best effort. It opens its own
// connection because the one that failed cannot be trusted to carry a command.
func (p *printer) removePartial(name string) {
	conn, err := p.connectFTP()
	if err != nil {
		return
	}
	defer conn.Quit()

	_ = conn.Delete(name)
}

// UploadFile sends one sliced file to the printer's storage.
//
// A dropped session part way through leaves a short file under the right name,
// which the printer lists happily and then chokes on, so the size is checked
// afterwards and a partial file is removed rather than left to be printed.
// expected is the size the browser said the file is. Checking against that
// rather than against what we managed to read catches a body that was cut short
// on its way to us: counting our own reads only ever proves we copied whatever
// we got, however little that was.
func (p *printer) UploadFile(name string, contents io.Reader, expected int64) error {
	conn, err := p.connectFTP()
	if err != nil {
		return err
	}
	defer conn.Quit()

	counted := &countingReader{inner: contents}
	if err := conn.Stor(name, counted); err != nil {
		// A transfer that died part way still leaves what arrived on the card,
		// under the name somebody is about to pick on the printer's screen. It
		// shows "--" for time and filament because the metadata never made it,
		// and the job exits the moment it starts. Clear it out on a fresh
		// connection, since this one is in an unknown state.
		p.removePartial(name)
		return fmt.Errorf("the printer refused the file: %w", err)
	}

	if expected > 0 && counted.n != expected {
		p.removePartial(name)
		return fmt.Errorf(
			"only %d of %d bytes arrived from the browser, so the file was removed - please send it again",
			counted.n, expected)
	}

	stored, ok := p.storedSize(conn, name)
	if !ok {
		// Neither SIZE nor the listing would tell us. Better to say so than to
		// let a half file sit on the card looking finished.
		log.Printf("printer %s: could not verify the size of %s after upload",
			p.cfg.Name, name)
		return nil
	}

	want := counted.n
	if expected > 0 {
		want = expected
	}

	if stored != want {
		if delErr := conn.Delete(name); delErr != nil {
			return fmt.Errorf(
				"only %d of %d bytes reached the printer, and the partial file "+
					"could not be removed - delete %s from the printer before printing: %w",
				stored, want, name, delErr)
		}
		return fmt.Errorf(
			"only %d of %d bytes reached the printer, so the file was removed - please send it again",
			stored, want)
	}

	return nil
}

// storedSize asks the printer how big a file ended up.
//
// SIZE is optional in FTP and not every printer answers it, so fall back to the
// listing, which this code already relies on elsewhere and which carries the
// size too. Giving up on SIZE alone left the check silently doing nothing.
func (p *printer) storedSize(conn *ftp.ServerConn, name string) (int64, bool) {
	if size, err := conn.FileSize(name); err == nil {
		return size, true
	}

	entries, err := conn.List("")
	if err != nil {
		return 0, false
	}
	for _, entry := range entries {
		if entry.Type == ftp.EntryTypeFile && entry.Name == name {
			return int64(entry.Size), true
		}
	}
	return 0, false
}

// PrinterFile is one file already on a printer.
type PrinterFile struct {
	Name string `json:"name"`
	Size uint64 `json:"size"`
	Time string `json:"time"`
}

// ListFiles reports the printable files already on the printer.
func (p *printer) ListFiles() ([]PrinterFile, error) {
	conn, err := p.connectFTP()
	if err != nil {
		return nil, err
	}
	defer conn.Quit()

	entries, err := conn.List("")
	if err != nil {
		return nil, fmt.Errorf("could not list the printer's files: %w", err)
	}

	files := make([]PrinterFile, 0, len(entries))
	for _, entry := range entries {
		if !printableEntry(entry) {
			continue
		}
		files = append(files, PrinterFile{
			Name: entry.Name,
			Size: entry.Size,
			Time: entry.Time.Local().Format("2006-01-02 15:04"),
		})
	}

	return files, nil
}

// printableEntry reports whether a listing entry is a file somebody could
// actually print, and therefore one this code will show or delete.
func printableEntry(entry *ftp.Entry) bool {
	if entry.Type != ftp.EntryTypeFile {
		return false
	}
	lower := strings.ToLower(entry.Name)
	if !strings.HasSuffix(lower, ".3mf") && !strings.HasSuffix(lower, ".gcode") {
		return false
	}
	// macOS sidecar files ("._something.3mf") are not printable
	return !strings.HasPrefix(entry.Name, ".")
}

// plainFileName rejects anything that could step outside the printer's upload
// directory. Names are otherwise taken as they come, because they have to match
// what is on the printer rather than what this code would have named it.
func plainFileName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		return "", fmt.Errorf("that file name is not usable")
	}
	if strings.ContainsAny(name, `/\`) {
		return "", fmt.Errorf("that file name is not usable")
	}
	return name, nil
}

// DeleteFile removes a file from the printer, so the storage does not fill up
// with everyone's old plates.
//
// The name is matched against the printer's own listing rather than pushed
// through sanitizeUploadName. Sanitising is right for an upload, where this
// code chooses the name, and wrong here: a file put on the card by Bambu Studio
// keeps its spaces and brackets, and cleaning "Benchy (1).gcode.3mf" into
// "Benchy_1.gcode.3mf" sent DELE after a file that was never there. The listing
// showed it, the delete reported success, and the file stayed put.
//
// Matching a real entry is also what keeps this safe: nothing outside the
// directory can ever be named.
func (p *printer) DeleteFile(name string) error {
	wanted, err := plainFileName(name)
	if err != nil {
		return err
	}

	conn, err := p.connectFTP()
	if err != nil {
		return err
	}
	defer conn.Quit()

	entries, err := conn.List("")
	if err != nil {
		return fmt.Errorf("could not list the printer's files: %w", err)
	}

	for _, entry := range entries {
		if printableEntry(entry) && entry.Name == wanted {
			if err := conn.Delete(entry.Name); err != nil {
				return fmt.Errorf("could not delete %s: %w", entry.Name, err)
			}
			return nil
		}
	}

	return fmt.Errorf("%s is not on the printer", wanted)
}

// isPrinting reports whether the printer is part way through name, so an upload
// does not rewrite a file the printer is still reading. The printer reports the
// job by subtask name, which usually carries no extension, so the comparison is
// made on the base name.
func (p *printer) isPrinting(name string) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.state != "RUNNING" && p.state != "PAUSE" {
		return false
	}

	current, _, _ := splitUploadSuffix(p.fileName)
	if current == "" {
		current = p.fileName
	}
	candidate, _, _ := splitUploadSuffix(name)

	return current != "" && strings.EqualFold(current, candidate)
}

// --- manager wrappers ---------------------------------------------------

func (m *PrinterManager) UploadFile(id, name string, contents io.Reader, expected int64) (string, error) {
	p, ok := m.byID[id]
	if !ok {
		return "", fmt.Errorf("unknown printer")
	}

	safe, err := sanitizeUploadName(name)
	if err != nil {
		return "", err
	}

	if p.isPrinting(safe) {
		return "", fmt.Errorf(
			"%s is printing right now - rename your file or wait for it to finish", safe)
	}

	if err := p.UploadFile(safe, contents, expected); err != nil {
		return "", err
	}
	return safe, nil
}

func (m *PrinterManager) ListFiles(id string) ([]PrinterFile, error) {
	p, ok := m.byID[id]
	if !ok {
		return nil, fmt.Errorf("unknown printer")
	}
	return p.ListFiles()
}

func (m *PrinterManager) DeleteFile(id, name string) error {
	p, ok := m.byID[id]
	if !ok {
		return fmt.Errorf("unknown printer")
	}

	if p.isPrinting(name) {
		return fmt.Errorf("%s is printing right now - stop the job first", name)
	}
	return p.DeleteFile(name)
}

// DeleteAllFiles clears every printable file off the printer's storage and
// reports how many went.
//
// Deleting one file at a time is fine for tidying; this is for a card that has
// filled up with a term's worth of everyone's plates. It refuses outright while
// a job is running rather than trying to skip the file in use: the printer is
// reading from the card, and the name it reports for the job does not always
// match the file exactly enough to be sure which one to spare.
func (p *printer) DeleteAllFiles() (int, error) {
	p.mu.RLock()
	state := strings.ToUpper(p.state)
	p.mu.RUnlock()

	if state == "RUNNING" || state == "PAUSE" || state == "PREPARE" {
		return 0, fmt.Errorf(
			"the printer is busy (state: %s) - wait for the job to finish", state)
	}

	conn, err := p.connectFTP()
	if err != nil {
		return 0, err
	}
	defer conn.Quit()

	entries, err := conn.List("")
	if err != nil {
		return 0, fmt.Errorf("could not list the printer's files: %w", err)
	}

	deleted := 0
	var failed []string
	for _, entry := range entries {
		if !printableEntry(entry) {
			continue
		}
		if err := conn.Delete(entry.Name); err != nil {
			failed = append(failed, entry.Name)
			continue
		}
		deleted++
	}

	// Report what did go, even when something would not: a partial clear is
	// still worth knowing about, and the caller can show both numbers.
	if len(failed) > 0 {
		return deleted, fmt.Errorf("deleted %d, but could not delete: %s",
			deleted, strings.Join(failed, ", "))
	}
	return deleted, nil
}

// DeleteAllFiles clears one printer's storage.
func (m *PrinterManager) DeleteAllFiles(id, adminName string) (int, error) {
	p, ok := m.byID[id]
	if !ok {
		return 0, fmt.Errorf("unknown printer")
	}

	deleted, err := p.DeleteAllFiles()
	if deleted > 0 {
		log.Printf("printer %s: %d files cleared by %s", p.cfg.Name, deleted, adminName)
	}
	return deleted, err
}
