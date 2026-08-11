package main

// Talks to the lab's Bambu Lab P1S printers over their local network protocol:
//
//   * status - MQTT over TLS on port 8883, user "bblp" + the printer's LAN
//     access code. The printer publishes JSON on device/<serial>/report.
//   * camera - a plain TLS socket on port 6000. After an 80 byte auth packet
//     the printer streams JPEG frames, each preceded by a 16 byte header whose
//     first 4 bytes are the payload length. Roughly one frame every 2 seconds.
//
// Everything here is read-only: we never send print commands.

import (
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

const (
	printerMQTTPort   = 8883
	printerCameraPort = 6000
	cameraAuthSize    = 80
	cameraHeaderSize  = 16
	// A frame older than this means the camera has gone quiet
	cameraStaleAfter = 30 * time.Second
	// Reports stop arriving if the printer is switched off or leaves the network
	statusStaleAfter = 2 * time.Minute
)

// PrinterConfig is one printer, as configured in the PRINTERS env var.
type PrinterConfig struct {
	ID         string
	Name       string
	Host       string
	Serial     string
	AccessCode string
}

// PrinterStatus is what the frontend sees. Access codes never appear here.
type PrinterStatus struct {
	ID               string  `json:"id"`
	Name             string  `json:"name"`
	Online           bool    `json:"online"`
	State            string  `json:"state"`    // IDLE, RUNNING, FINISH, FAILED, PAUSE
	Progress         int     `json:"progress"` // percent
	RemainingMinutes int     `json:"remaining_minutes"`
	FileName         string  `json:"file_name"`
	NozzleTemp       float64 `json:"nozzle_temp"`
	BedTemp          float64 `json:"bed_temp"`
	ChamberTemp      float64 `json:"chamber_temp"`
	CameraOnline     bool    `json:"camera_online"`
	UpdatedAt        *string `json:"updated_at"`
	// Set when an admin has stopped a job, so the action is visible rather
	// than silent.
	LastActionBy *string `json:"last_action_by"`
	LastActionAt *string `json:"last_action_at"`
}

// printerReport mirrors the fields we care about from the printer's JSON.
// Every field is a pointer because the printer sends partial updates - a
// missing field means "unchanged", not "zero".
type printerReport struct {
	Print struct {
		GcodeState    *string  `json:"gcode_state"`
		Percent       *int     `json:"mc_percent"`
		RemainingTime *int     `json:"mc_remaining_time"`
		SubtaskName   *string  `json:"subtask_name"`
		NozzleTemper  *float64 `json:"nozzle_temper"`
		BedTemper     *float64 `json:"bed_temper"`
		ChamberTemper *float64 `json:"chamber_temper"`
	} `json:"print"`
}

// printer holds the live state of one machine.
type printer struct {
	cfg PrinterConfig

	mu           sync.RWMutex
	state        string
	progress     int
	remaining    int
	fileName     string
	nozzleTemp   float64
	bedTemp      float64
	chamberTemp  float64
	lastReport   time.Time
	lastFrame    []byte
	lastFrameAt  time.Time
	frameVersion uint64

	lastActionBy string
	lastActionAt time.Time

	// Set once the MQTT client is running, so commands can be published
	client mqtt.Client

	// Overridable so tests can point at a mock printer
	cameraPort int

	// Commands carry an incrementing sequence id
	sequence int
}

// PrinterManager owns the connections to every configured printer.
type PrinterManager struct {
	printers []*printer
	byID     map[string]*printer
}

// parsePrinterConfig reads the PRINTERS environment variable. Format:
//
//	PRINTERS="Name|host|serial|accesscode,Name2|host2|serial2|accesscode2"
//
// Returns an empty slice when unset, so the feature simply stays switched off.
func parsePrinterConfig(raw string) ([]PrinterConfig, error) {
	var configs []PrinterConfig

	for _, entry := range strings.Split(raw, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}

		parts := strings.Split(entry, "|")
		if len(parts) != 4 {
			return nil, fmt.Errorf(
				"printer entry %q must be Name|host|serial|accesscode", entry)
		}

		for i := range parts {
			parts[i] = strings.TrimSpace(parts[i])
		}
		if parts[0] == "" || parts[1] == "" || parts[2] == "" || parts[3] == "" {
			return nil, fmt.Errorf("printer entry %q has an empty field", entry)
		}

		configs = append(configs, PrinterConfig{
			ID:         slugifyPrinterName(parts[0]),
			Name:       parts[0],
			Host:       parts[1],
			Serial:     parts[2],
			AccessCode: parts[3],
		})
	}

	return configs, nil
}

// slugifyPrinterName turns "3DP-01P-279" into a URL-safe id.
func slugifyPrinterName(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_':
			b.WriteRune('-')
		case r == ' ':
			b.WriteRune('-')
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		slug = "printer"
	}
	return slug
}

// NewPrinterManager starts background connections to every configured printer.
// Printers that are switched off simply show as offline and keep retrying.
func NewPrinterManager(configs []PrinterConfig) *PrinterManager {
	m := &PrinterManager{byID: make(map[string]*printer)}

	for _, cfg := range configs {
		p := &printer{cfg: cfg, cameraPort: printerCameraPort}
		m.printers = append(m.printers, p)
		m.byID[cfg.ID] = p

		go p.runStatus()
		go p.runCamera()
	}

	return m
}

// --- status over MQTT ---------------------------------------------------

func (p *printer) runStatus() {
	opts := mqtt.NewClientOptions()
	opts.AddBroker(fmt.Sprintf("ssl://%s:%d", p.cfg.Host, printerMQTTPort))
	opts.SetClientID(fmt.Sprintf("rrc-inventory-%s", p.cfg.ID))
	opts.SetUsername("bblp")
	opts.SetPassword(p.cfg.AccessCode)
	// The printer uses a self-signed certificate; there is no CA to check it
	// against, and this traffic never leaves the printer network.
	opts.SetTLSConfig(&tls.Config{InsecureSkipVerify: true}) // #nosec G402
	opts.SetAutoReconnect(true)
	opts.SetConnectRetry(true)
	opts.SetConnectRetryInterval(15 * time.Second)
	opts.SetConnectTimeout(10 * time.Second)

	reportTopic := fmt.Sprintf("device/%s/report", p.cfg.Serial)
	requestTopic := fmt.Sprintf("device/%s/request", p.cfg.Serial)

	opts.SetOnConnectHandler(func(c mqtt.Client) {
		// Subscribing to anything other than this printer's own topic gets the
		// connection dropped by the printer, so only ever use the exact topic.
		if token := c.Subscribe(reportTopic, 0, func(_ mqtt.Client, msg mqtt.Message) {
			p.applyReport(msg.Payload())
		}); token.Wait() && token.Error() != nil {
			log.Printf("printer %s: subscribe failed: %v", p.cfg.Name, token.Error())
			return
		}

		// Ask for a full state dump rather than waiting for the next periodic
		// push, which can be minutes away on an idle printer.
		c.Publish(requestTopic, 0, false,
			`{"pushing":{"command":"pushall"}}`)
	})

	client := mqtt.NewClient(opts)

	p.mu.Lock()
	p.client = client
	p.mu.Unlock()

	if token := client.Connect(); token.Wait() && token.Error() != nil {
		log.Printf("printer %s: initial connect failed: %v", p.cfg.Name, token.Error())
	}

	// Nudge the printer for a fresh dump periodically; some fields are only
	// sent on change and we want to recover after a reconnect.
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		if client.IsConnected() {
			client.Publish(requestTopic, 0, false, `{"pushing":{"command":"pushall"}}`)
		}
	}
}

// applyReport merges a report into the printer's state. Reports are partial,
// so only fields actually present are updated.
func (p *printer) applyReport(payload []byte) {
	var report printerReport
	if err := json.Unmarshal(payload, &report); err != nil {
		return
	}

	info := report.Print

	p.mu.Lock()
	defer p.mu.Unlock()

	// Any well-formed report means the printer is alive and talking
	p.lastReport = time.Now()

	if info.GcodeState != nil {
		p.state = *info.GcodeState
	}
	if info.Percent != nil {
		p.progress = *info.Percent
	}
	if info.RemainingTime != nil {
		p.remaining = *info.RemainingTime
	}
	if info.SubtaskName != nil {
		p.fileName = *info.SubtaskName
	}
	if info.NozzleTemper != nil {
		p.nozzleTemp = *info.NozzleTemper
	}
	if info.BedTemper != nil {
		p.bedTemp = *info.BedTemper
	}
	if info.ChamberTemper != nil {
		p.chamberTemp = *info.ChamberTemper
	}
}

// --- camera over TLS ----------------------------------------------------

// cameraAuthPacket builds the 80 byte handshake the printer expects:
// two little-endian magic values, two zero words, then username and access
// code in fixed 32 byte fields.
func cameraAuthPacket(username, accessCode string) []byte {
	packet := make([]byte, cameraAuthSize)
	binary.LittleEndian.PutUint32(packet[0:4], 0x40)
	binary.LittleEndian.PutUint32(packet[4:8], 0x3000)
	// packet[8:16] stays zero
	copy(packet[16:48], username)
	copy(packet[48:80], accessCode)
	return packet
}

func (p *printer) runCamera() {
	for {
		if err := p.streamCamera(); err != nil {
			log.Printf("printer %s: camera: %v", p.cfg.Name, err)
		}
		time.Sleep(10 * time.Second)
	}
}

func (p *printer) streamCamera() error {
	port := p.cameraPort
	if port == 0 {
		port = printerCameraPort
	}
	address := net.JoinHostPort(p.cfg.Host, fmt.Sprint(port))

	dialer := &net.Dialer{Timeout: 10 * time.Second}
	conn, err := tls.DialWithDialer(dialer, "tcp", address,
		&tls.Config{InsecureSkipVerify: true}) // #nosec G402 - self-signed printer cert
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer conn.Close()

	if _, err := conn.Write(cameraAuthPacket("bblp", p.cfg.AccessCode)); err != nil {
		return fmt.Errorf("auth: %w", err)
	}

	header := make([]byte, cameraHeaderSize)
	for {
		// Frames arrive every couple of seconds; allow plenty of slack before
		// deciding the connection is dead.
		if err := conn.SetReadDeadline(time.Now().Add(60 * time.Second)); err != nil {
			return err
		}

		// Read the fixed-size header, then exactly as many payload bytes as it
		// declares. Reading exact lengths avoids the frame corruption you get
		// from trying to detect boundaries in a byte stream.
		if _, err := io.ReadFull(conn, header); err != nil {
			return fmt.Errorf("read header: %w", err)
		}

		payloadSize := binary.LittleEndian.Uint32(header[0:4])
		if payloadSize == 0 || payloadSize > 8*1024*1024 {
			return fmt.Errorf("implausible frame size %d (wrong access code?)", payloadSize)
		}

		frame := make([]byte, payloadSize)
		if _, err := io.ReadFull(conn, frame); err != nil {
			return fmt.Errorf("read frame: %w", err)
		}

		// Sanity check the JPEG markers before showing it to anyone
		if len(frame) < 4 || frame[0] != 0xFF || frame[1] != 0xD8 ||
			frame[len(frame)-2] != 0xFF || frame[len(frame)-1] != 0xD9 {
			continue
		}

		p.mu.Lock()
		p.lastFrame = frame
		p.lastFrameAt = time.Now()
		p.frameVersion++
		p.mu.Unlock()
	}
}

// latestFrame returns the most recent JPEG and its version counter.
func (p *printer) latestFrame() ([]byte, uint64, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	if p.lastFrame == nil || time.Since(p.lastFrameAt) > cameraStaleAfter {
		return nil, p.frameVersion, false
	}
	return p.lastFrame, p.frameVersion, true
}

// --- public accessors ---------------------------------------------------

func (p *printer) status() PrinterStatus {
	p.mu.RLock()
	defer p.mu.RUnlock()

	online := !p.lastReport.IsZero() && time.Since(p.lastReport) < statusStaleAfter
	cameraOnline := p.lastFrame != nil && time.Since(p.lastFrameAt) < cameraStaleAfter

	status := PrinterStatus{
		ID:           p.cfg.ID,
		Name:         p.cfg.Name,
		Online:       online,
		CameraOnline: cameraOnline,
	}

	if p.lastActionBy != "" {
		by := p.lastActionBy
		at := p.lastActionAt.Format(time.RFC3339)
		status.LastActionBy = &by
		status.LastActionAt = &at
	}

	if online {
		status.State = p.state
		status.Progress = p.progress
		status.RemainingMinutes = p.remaining
		status.FileName = p.fileName
		status.NozzleTemp = p.nozzleTemp
		status.BedTemp = p.bedTemp
		status.ChamberTemp = p.chamberTemp

		updated := p.lastReport.Format(time.RFC3339)
		status.UpdatedAt = &updated
	}

	return status
}

// Statuses returns the current state of every configured printer.
func (m *PrinterManager) Statuses() []PrinterStatus {
	statuses := make([]PrinterStatus, 0, len(m.printers))
	for _, p := range m.printers {
		statuses = append(statuses, p.status())
	}
	return statuses
}

// Frame returns the latest camera frame for one printer.
func (m *PrinterManager) Frame(id string) ([]byte, bool) {
	p, ok := m.byID[id]
	if !ok {
		return nil, false
	}
	frame, _, fresh := p.latestFrame()
	return frame, fresh
}

// stoppableStates are the states where stopping a job makes sense.
var stoppableStates = map[string]bool{
	"RUNNING": true,
	"PAUSE":   true,
	"PREPARE": true,
	"SLICING": true,
}

// stop asks the printer to abort the current job. Read-only everywhere else,
// this is the single command the system can send, and only admins reach it.
func (p *printer) stop(adminName string) error {
	p.mu.Lock()
	client := p.client
	online := !p.lastReport.IsZero() && time.Since(p.lastReport) < statusStaleAfter
	state := p.state
	p.sequence++
	sequence := p.sequence
	p.mu.Unlock()

	if client == nil || !client.IsConnected() || !online {
		return fmt.Errorf("printer is not reachable")
	}

	if !stoppableStates[strings.ToUpper(state)] {
		return fmt.Errorf("nothing is printing right now (state: %s)", state)
	}

	payload := fmt.Sprintf(
		`{"print":{"sequence_id":"%d","command":"stop"}}`, sequence)

	topic := fmt.Sprintf("device/%s/request", p.cfg.Serial)
	token := client.Publish(topic, 0, false, payload)
	if !token.WaitTimeout(10*time.Second) || token.Error() != nil {
		if token.Error() != nil {
			return fmt.Errorf("could not send the stop command: %w", token.Error())
		}
		return fmt.Errorf("timed out sending the stop command")
	}

	p.mu.Lock()
	p.lastActionBy = adminName
	p.lastActionAt = time.Now()
	p.mu.Unlock()

	log.Printf("printer %s: stop requested by %s", p.cfg.Name, adminName)
	return nil
}

// Stop aborts the current job on one printer.
func (m *PrinterManager) Stop(id, adminName string) error {
	p, ok := m.byID[id]
	if !ok {
		return fmt.Errorf("unknown printer")
	}
	return p.stop(adminName)
}

// Configured reports whether any printers are set up at all.
func (m *PrinterManager) Configured() bool {
	return len(m.printers) > 0
}

// loadPrinterManager builds the manager from the environment.
func loadPrinterManager() *PrinterManager {
	configs, err := parsePrinterConfig(os.Getenv("PRINTERS"))
	if err != nil {
		log.Printf("Ignoring PRINTERS setting: %v", err)
		return NewPrinterManager(nil)
	}

	if len(configs) == 0 {
		log.Println("No printers configured (set PRINTERS to enable the printer page)")
	} else {
		for _, cfg := range configs {
			log.Printf("Printer configured: %s at %s", cfg.Name, cfg.Host)
		}
	}

	return NewPrinterManager(configs)
}
