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
	ftpTimeout     = 60 * time.Second
)

// allowedUploadExtensions are the only things worth putting on a printer.
var allowedUploadExtensions = map[string]bool{
	".3mf":   true,
	".gcode": true,
}

// sanitizeUploadName reduces whatever the browser sent to a plain, safe file
// name. Directory components are stripped, so an upload cannot escape the
// printer's upload directory.
func sanitizeUploadName(raw string) (string, error) {
	name := strings.TrimSpace(raw)

	// Drop any path the browser included, both separators
	name = strings.ReplaceAll(name, "\\", "/")
	name = path.Base(name)

	if name == "" || name == "." || name == ".." || name == "/" {
		return "", fmt.Errorf("that file name is not usable")
	}

	// Keep the extension, check it against the allow list
	lower := strings.ToLower(name)
	ext := path.Ext(lower)
	if strings.HasSuffix(lower, ".gcode.3mf") {
		ext = ".3mf"
	}
	if !allowedUploadExtensions[ext] {
		return "", fmt.Errorf("only .3mf and .gcode files can be sent to a printer")
	}

	// Anything outside this set risks confusing the printer's own file browser
	var cleaned strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			cleaned.WriteRune(r)
		case r == '.' || r == '-' || r == '_':
			cleaned.WriteRune(r)
		case r == ' ':
			cleaned.WriteRune('_')
		}
	}

	result := strings.Trim(cleaned.String(), "._-")

	// Re-check the result rather than the input: stripping characters can
	// leave something like ".3mf" as "3mf", which has no extension at all.
	finalExt := strings.ToLower(path.Ext(result))
	base := strings.Trim(strings.TrimSuffix(result, path.Ext(result)), "._-")
	if result == "" || base == "" || !allowedUploadExtensions[finalExt] {
		return "", fmt.Errorf("that file name is not usable")
	}

	if len(result) > 120 {
		// Trim the middle, never the extension
		keep := path.Ext(result)
		result = result[:120-len(keep)] + keep
	}

	return result, nil
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
	options := []ftp.DialOption{
		ftp.DialWithTimeout(ftpTimeout),
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

// UploadFile sends one sliced file to the printer's storage.
func (p *printer) UploadFile(name string, contents io.Reader) error {
	conn, err := p.connectFTP()
	if err != nil {
		return err
	}
	defer conn.Quit()

	if err := conn.Stor(name, contents); err != nil {
		return fmt.Errorf("the printer refused the file: %w", err)
	}
	return nil
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
		if entry.Type != ftp.EntryTypeFile {
			continue
		}
		lower := strings.ToLower(entry.Name)
		if !strings.HasSuffix(lower, ".3mf") && !strings.HasSuffix(lower, ".gcode") {
			continue
		}
		// macOS sidecar files ("._something.3mf") are not printable
		if strings.HasPrefix(entry.Name, "._") || strings.HasPrefix(entry.Name, ".") {
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

// DeleteFile removes a file from the printer, so the storage does not fill up
// with everyone's old plates.
func (p *printer) DeleteFile(name string) error {
	safe, err := sanitizeUploadName(name)
	if err != nil {
		return err
	}

	conn, err := p.connectFTP()
	if err != nil {
		return err
	}
	defer conn.Quit()

	if err := conn.Delete(safe); err != nil {
		return fmt.Errorf("could not delete %s: %w", safe, err)
	}
	return nil
}

// --- manager wrappers ---------------------------------------------------

func (m *PrinterManager) UploadFile(id, name string, contents io.Reader) (string, error) {
	p, ok := m.byID[id]
	if !ok {
		return "", fmt.Errorf("unknown printer")
	}

	safe, err := sanitizeUploadName(name)
	if err != nil {
		return "", err
	}

	if err := p.UploadFile(safe, contents); err != nil {
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
	return p.DeleteFile(name)
}
