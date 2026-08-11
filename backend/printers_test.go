package main

// Tests for the printer client. The camera test runs against a mock printer
// that speaks the real wire format, so the framing logic is verified without
// needing a printer on the desk.

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/binary"
	"math/big"
	"net"
	"testing"
	"time"
)

func TestParsePrinterConfig(t *testing.T) {
	configs, err := parsePrinterConfig(
		"3DP-01P-279|192.168.2.101|01P00A411600279|89a8541a," +
			" 3DP-01P-739 | 192.168.2.102 | 01P00C580301739 | abcd1234 ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(configs) != 2 {
		t.Fatalf("expected 2 printers, got %d", len(configs))
	}

	if configs[0].ID != "3dp-01p-279" {
		t.Errorf("unexpected id: %q", configs[0].ID)
	}
	if configs[0].Serial != "01P00A411600279" {
		t.Errorf("unexpected serial: %q", configs[0].Serial)
	}
	// Whitespace around fields should be tolerated
	if configs[1].Host != "192.168.2.102" || configs[1].AccessCode != "abcd1234" {
		t.Errorf("whitespace not trimmed: %+v", configs[1])
	}
}

func TestParsePrinterConfigEmptyAndInvalid(t *testing.T) {
	configs, err := parsePrinterConfig("")
	if err != nil || len(configs) != 0 {
		t.Fatalf("empty config should yield no printers, got %v / %v", configs, err)
	}

	if _, err := parsePrinterConfig("only|three|fields"); err == nil {
		t.Error("expected an error for a malformed entry")
	}

	if _, err := parsePrinterConfig("name||serial|code"); err == nil {
		t.Error("expected an error for an empty field")
	}
}

// The printer sends partial reports. A later report that omits a field must
// not wipe the value we already have - this is the bug that would show up as
// temperatures flickering to zero.
func TestApplyReportMergesPartialUpdates(t *testing.T) {
	p := &printer{}

	p.applyReport([]byte(`{"print":{
		"gcode_state":"RUNNING",
		"mc_percent":42,
		"mc_remaining_time":73,
		"subtask_name":"bracket.gcode.3mf",
		"nozzle_temper":218.5,
		"bed_temper":60.0,
		"chamber_temper":31.2
	}}`))

	status := p.status()
	if !status.Online {
		t.Fatal("printer should be online after a report")
	}
	if status.State != "RUNNING" || status.Progress != 42 || status.RemainingMinutes != 73 {
		t.Errorf("unexpected status: %+v", status)
	}
	if status.FileName != "bracket.gcode.3mf" {
		t.Errorf("unexpected file name: %q", status.FileName)
	}
	if status.NozzleTemp != 218.5 || status.BedTemp != 60.0 || status.ChamberTemp != 31.2 {
		t.Errorf("unexpected temperatures: %+v", status)
	}

	// A temperature-only update, as the printer really sends
	p.applyReport([]byte(`{"print":{"nozzle_temper":219.9}}`))

	status = p.status()
	if status.NozzleTemp != 219.9 {
		t.Errorf("nozzle temp not updated: %v", status.NozzleTemp)
	}
	if status.Progress != 42 {
		t.Errorf("progress was clobbered by a partial update: %v", status.Progress)
	}
	if status.FileName != "bracket.gcode.3mf" {
		t.Errorf("file name was clobbered by a partial update: %q", status.FileName)
	}
	if status.BedTemp != 60.0 {
		t.Errorf("bed temp was clobbered by a partial update: %v", status.BedTemp)
	}
}

func TestApplyReportIgnoresGarbage(t *testing.T) {
	p := &printer{}
	p.applyReport([]byte("not json at all"))

	if p.status().Online {
		t.Error("garbage payload should not mark the printer online")
	}
}

// Real values captured from the lab's printer, to be sure the field names match.
func TestApplyReportRealPayload(t *testing.T) {
	p := &printer{}
	p.applyReport([]byte(`{"print":{
		"gcode_state":"FINISH",
		"mc_percent":100,
		"mc_remaining_time":0,
		"subtask_name":"print_file_srinath.gcode.3mf",
		"nozzle_temper":28.875,
		"bed_temper":25.46875
	}}`))

	status := p.status()
	if status.State != "FINISH" || status.Progress != 100 {
		t.Errorf("unexpected status from real payload: %+v", status)
	}
	if status.FileName != "print_file_srinath.gcode.3mf" {
		t.Errorf("unexpected file: %q", status.FileName)
	}
	if status.NozzleTemp != 28.875 || status.BedTemp != 25.46875 {
		t.Errorf("unexpected temps: %v / %v", status.NozzleTemp, status.BedTemp)
	}
}

func TestStatusGoesStale(t *testing.T) {
	p := &printer{}
	p.applyReport([]byte(`{"print":{"gcode_state":"RUNNING"}}`))

	// Pretend the last report arrived long ago
	p.mu.Lock()
	p.lastReport = time.Now().Add(-statusStaleAfter - time.Minute)
	p.mu.Unlock()

	status := p.status()
	if status.Online {
		t.Error("printer should read as offline once reports stop")
	}
	if status.State != "" {
		t.Error("stale status should not report a state")
	}
}

func TestCameraAuthPacket(t *testing.T) {
	packet := cameraAuthPacket("bblp", "89a8541a")

	if len(packet) != cameraAuthSize {
		t.Fatalf("auth packet must be %d bytes, got %d", cameraAuthSize, len(packet))
	}
	if got := binary.LittleEndian.Uint32(packet[0:4]); got != 0x40 {
		t.Errorf("first word = %#x, want 0x40", got)
	}
	if got := binary.LittleEndian.Uint32(packet[4:8]); got != 0x3000 {
		t.Errorf("second word = %#x, want 0x3000", got)
	}
	if string(packet[16:20]) != "bblp" {
		t.Errorf("username field = %q", packet[16:20])
	}
	if packet[20] != 0 {
		t.Error("username field must be zero padded")
	}
	if string(packet[48:56]) != "89a8541a" {
		t.Errorf("access code field = %q", packet[48:56])
	}
}

// --- mock printer camera ------------------------------------------------

func selfSignedCert(t *testing.T) tls.Certificate {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("key: %v", err)
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "mock-printer"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}

	der, err := x509.CreateCertificate(rand.Reader, &template, &template,
		&key.PublicKey, key)
	if err != nil {
		t.Fatalf("cert: %v", err)
	}

	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

// fakeJPEG builds a byte blob with valid JPEG start and end markers.
func fakeJPEG(size int, fill byte) []byte {
	frame := make([]byte, size)
	for i := range frame {
		frame[i] = fill
	}
	frame[0], frame[1] = 0xFF, 0xD8
	frame[2], frame[3] = 0xFF, 0xE0
	frame[size-2], frame[size-1] = 0xFF, 0xD9
	return frame
}

// startMockCamera serves frames exactly the way the printer does: a 16 byte
// header with the payload length, then the JPEG. It deliberately writes the
// payload in small chunks to prove the reader reassembles them.
func startMockCamera(t *testing.T, frames [][]byte, gotAuth chan<- []byte) net.Listener {
	t.Helper()

	listener, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{selfSignedCert(t)},
	})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		auth := make([]byte, cameraAuthSize)
		if _, err := readFullBytes(conn, auth); err != nil {
			return
		}
		select {
		case gotAuth <- auth:
		default:
		}

		for _, frame := range frames {
			header := make([]byte, cameraHeaderSize)
			binary.LittleEndian.PutUint32(header[0:4], uint32(len(frame)))
			if _, err := conn.Write(header); err != nil {
				return
			}
			// Chunked writes, like the real printer
			for offset := 0; offset < len(frame); offset += 4096 {
				end := offset + 4096
				if end > len(frame) {
					end = len(frame)
				}
				if _, err := conn.Write(frame[offset:end]); err != nil {
					return
				}
				time.Sleep(2 * time.Millisecond)
			}
		}

		time.Sleep(2 * time.Second)
	}()

	return listener
}

func readFullBytes(conn net.Conn, buf []byte) (int, error) {
	total := 0
	for total < len(buf) {
		n, err := conn.Read(buf[total:])
		if err != nil {
			return total, err
		}
		total += n
	}
	return total, nil
}

func TestStreamCameraAgainstMockPrinter(t *testing.T) {
	first := fakeJPEG(20_000, 0x11)
	second := fakeJPEG(35_000, 0x22)

	gotAuth := make(chan []byte, 1)
	listener := startMockCamera(t, [][]byte{first, second}, gotAuth)
	defer listener.Close()

	host, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatalf("addr: %v", err)
	}

	p := &printer{
		cfg:        PrinterConfig{Name: "mock", Host: host, AccessCode: "89a8541a"},
		cameraPort: atoiOrZero(port),
	}

	go p.streamCamera() //nolint:errcheck // errors surface as a missing frame

	// The printer must receive a well-formed auth packet
	select {
	case auth := <-gotAuth:
		if len(auth) != cameraAuthSize {
			t.Fatalf("auth packet was %d bytes", len(auth))
		}
		if string(auth[16:20]) != "bblp" {
			t.Errorf("username not sent: %q", auth[16:20])
		}
		if string(auth[48:56]) != "89a8541a" {
			t.Errorf("access code not sent: %q", auth[48:56])
		}
	case <-time.After(5 * time.Second):
		t.Fatal("mock printer never received the auth packet")
	}

	// Both frames should be reassembled byte for byte
	if !waitForFrame(p, first, 5*time.Second) {
		t.Fatal("first frame was not received intact")
	}
	if !waitForFrame(p, second, 5*time.Second) {
		t.Fatal("second frame was not received intact")
	}

	frame, version, fresh := p.latestFrame()
	if !fresh {
		t.Fatal("latest frame should be fresh")
	}
	if len(frame) != len(second) {
		t.Errorf("latest frame is %d bytes, want %d", len(frame), len(second))
	}
	if version != 2 {
		t.Errorf("frame version = %d, want 2", version)
	}
}

// A frame whose declared length is absurd means we are out of sync or the
// access code was refused; the reader must bail instead of allocating wildly.
func TestStreamCameraRejectsImplausibleFrameSize(t *testing.T) {
	listener, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{selfSignedCert(t)},
	})
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer listener.Close()

	go func() {
		conn, err := listener.Accept()
		if err != nil {
			return
		}
		defer conn.Close()

		auth := make([]byte, cameraAuthSize)
		if _, err := readFullBytes(conn, auth); err != nil {
			return
		}

		header := make([]byte, cameraHeaderSize)
		binary.LittleEndian.PutUint32(header[0:4], 500_000_000) // 500 MB
		conn.Write(header)
		time.Sleep(2 * time.Second)
	}()

	host, port, _ := net.SplitHostPort(listener.Addr().String())
	p := &printer{
		cfg:        PrinterConfig{Name: "mock", Host: host, AccessCode: "x"},
		cameraPort: atoiOrZero(port),
	}

	done := make(chan error, 1)
	go func() { done <- p.streamCamera() }()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected an error for an implausible frame size")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("streamCamera did not reject the bad frame size")
	}
}

func waitForFrame(p *printer, want []byte, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		frame, _, fresh := p.latestFrame()
		if fresh && len(frame) == len(want) && bytesEqual(frame, want) {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func atoiOrZero(s string) int {
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int(r-'0')
	}
	return n
}
