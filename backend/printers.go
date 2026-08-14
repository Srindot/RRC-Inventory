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
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
	"gorm.io/gorm"
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
	ID               string     `json:"id"`
	Name             string     `json:"name"`
	Online           bool       `json:"online"`
	State            string     `json:"state"`    // IDLE, RUNNING, FINISH, FAILED, PAUSE
	Progress         int        `json:"progress"` // percent
	RemainingMinutes int        `json:"remaining_minutes"`
	FileName         string     `json:"file_name"`
	NozzleTemp       float64    `json:"nozzle_temp"`
	BedTemp          float64    `json:"bed_temp"`
	ChamberTemp      float64    `json:"chamber_temp"`
	LightOn          bool       `json:"light_on"`
	Faults           []HMSFault `json:"faults"`
	AMS              []AMSUnit  `json:"ams"`
	ExternalSpool    *AMSSlot   `json:"external_spool"`
	CameraOnline     bool       `json:"camera_online"`
	// Reachable but rejecting our credentials - almost always a changed
	// access code, which the printer does when LAN mode is toggled.
	AccessCodeProblem bool    `json:"access_code_problem"`
	UpdatedAt         *string `json:"updated_at"`
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

		// Chamber light state, reported as a list of nodes
		LightsReport *[]struct {
			Node string `json:"node"`
			Mode string `json:"mode"`
		} `json:"lights_report"`

		// The AMS spool changer, when one is fitted. Bambu reports most of
		// these as strings, including the numbers.
		AMS *struct {
			AMS []struct {
				ID       string `json:"id"`
				Humidity string `json:"humidity"`
				Temp     string `json:"temp"`
				Tray     []struct {
					ID        string `json:"id"`
					Type      string `json:"tray_type"`
					Color     string `json:"tray_color"`
					SubBrands string `json:"tray_sub_brands"`
					Remain    int    `json:"remain"`
				} `json:"tray"`
			} `json:"ams"`
			TrayNow string `json:"tray_now"`
		} `json:"ams"`

		// The spool feeding directly into the printer, bypassing the AMS
		VTTray *struct {
			ID        string `json:"id"`
			Type      string `json:"tray_type"`
			Color     string `json:"tray_color"`
			SubBrands string `json:"tray_sub_brands"`
			Remain    int    `json:"remain"`
		} `json:"vt_tray"`

		// Health Management System - the printer's own fault list. An empty
		// array means "no faults", which is different from the field being
		// absent, so this stays a pointer.
		HMS *[]struct {
			Attr uint32 `json:"attr"`
			Code uint32 `json:"code"`
		} `json:"hms"`
	} `json:"print"`
}

// AMSSlot is one filament position, either in an AMS unit or the external
// spool holder.
type AMSSlot struct {
	Slot     int    `json:"slot"`
	Material string `json:"material"`
	Color    string `json:"color"`  // #RRGGBB, empty when unknown
	Remain   int    `json:"remain"` // percent, -1 when the printer does not know
	Active   bool   `json:"active"`
	Empty    bool   `json:"empty"`
}

// AMSUnit is one spool changer.
type AMSUnit struct {
	ID       int       `json:"id"`
	Humidity string    `json:"humidity"`
	Temp     string    `json:"temp"`
	Slots    []AMSSlot `json:"slots"`
}

// trayColor turns Bambu's RRGGBBAA into a CSS colour, dropping the alpha.
func trayColor(raw string) string {
	if len(raw) < 6 {
		return ""
	}
	return "#" + strings.ToUpper(raw[:6])
}

// trayMaterial prefers the detailed name ("PLA Basic") over the bare type.
func trayMaterial(subBrands, trayType string) string {
	if strings.TrimSpace(subBrands) != "" {
		return strings.TrimSpace(subBrands)
	}
	return strings.TrimSpace(trayType)
}

// HMSFault is one fault, formatted the way Bambu's own documentation writes it.
type HMSFault struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	URL      string `json:"url"`
}

// hmsSeverity reads the severity out of the top half of the code word.
func hmsSeverity(code uint32) string {
	switch (code >> 16) & 0xFFFF {
	case 1:
		return "fatal"
	case 2:
		return "serious"
	case 3:
		return "common"
	case 4:
		return "info"
	default:
		return "unknown"
	}
}

// formatHMS builds the canonical HMS_XXXX_XXXX_XXXX_XXXX string from the two
// words the printer reports.
func formatHMS(attr, code uint32) string {
	return fmt.Sprintf("HMS_%04X_%04X_%04X_%04X",
		attr>>16, attr&0xFFFF, code>>16, code&0xFFFF)
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

	lightOn  bool
	faults   []HMSFault
	amsUnits []AMSUnit
	external *AMSSlot

	// The job currently being tracked for the print log
	currentJob *PrintJob
	jobs       *gorm.DB

	// Credential health, derived from the camera handshake: the printer
	// accepts the TCP connection and then hangs up when the code is wrong.
	authFailed bool

	// Closed and replaced when the access code changes, to force a reconnect
	cameraConn net.Conn
	restart    chan struct{}

	// Set once the MQTT client is running, so commands can be published
	client mqtt.Client

	// Overridable so tests can point at a mock printer
	cameraPort int
	// FTP settings, also overridable for tests
	ftpPort      int
	ftpPlaintext bool
	// Zero means ftpShutTimeout. Tests shorten it so the hang case does not
	// take half a minute to prove.
	ftpShut time.Duration

	// Commands carry an incrementing sequence id
	sequence int
}

// PrinterManager owns the connections to every configured printer.
type PrinterManager struct {
	printers []*printer
	byID     map[string]*printer
	db       *gorm.DB
}

// PrintJob is one print, recorded automatically from the printer's own state
// changes. Nobody fills in a form - the file name carries who it belongs to,
// by the naming convention in the printer guidelines.
type PrintJob struct {
	gorm.Model
	PrinterID   string     `json:"printer_id" gorm:"index"`
	PrinterName string     `json:"printer_name"`
	FileName    string     `json:"file_name"`
	StartedAt   time.Time  `json:"started_at"`
	EndedAt     *time.Time `json:"ended_at"`
	// finished, failed, stopped, or running while still in progress
	Result      string `json:"result"`
	StoppedBy   string `json:"stopped_by"`
	LastPercent int    `json:"last_percent"`
}

// PrinterCredential stores an access code changed from the admin page, so the
// new code survives a restart without anyone editing .env on the server.
// It overrides whatever PRINTERS specifies.
type PrinterCredential struct {
	gorm.Model
	PrinterID  string `gorm:"uniqueIndex"`
	AccessCode string
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
func NewPrinterManager(configs []PrinterConfig, db *gorm.DB) *PrinterManager {
	m := &PrinterManager{byID: make(map[string]*printer), db: db}

	// A code changed from the admin page wins over the one in PRINTERS
	if db != nil {
		var saved []PrinterCredential
		if err := db.Find(&saved).Error; err == nil {
			overrides := make(map[string]string, len(saved))
			for _, credential := range saved {
				overrides[credential.PrinterID] = credential.AccessCode
			}
			for i := range configs {
				if code, ok := overrides[configs[i].ID]; ok && code != "" {
					configs[i].AccessCode = code
					log.Printf("Printer %s: using the access code saved from the admin page",
						configs[i].Name)
				}
			}
		}
	}

	for _, cfg := range configs {
		p := &printer{
			cfg:        cfg,
			cameraPort: printerCameraPort,
			restart:    make(chan struct{}, 1),
			jobs:       db,
		}
		m.printers = append(m.printers, p)
		m.byID[cfg.ID] = p

		go p.runStatus()
		go p.runCamera()
	}

	return m
}

// accessCode returns the current code, which an admin can change at runtime.
func (p *printer) accessCode() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.cfg.AccessCode
}

// setAccessCode swaps the credential and forces both connections to restart.
func (p *printer) setAccessCode(code string) {
	p.mu.Lock()
	p.cfg.AccessCode = code
	// Assume the new code works until the printer says otherwise
	p.authFailed = false
	conn := p.cameraConn
	p.cameraConn = nil
	p.mu.Unlock()

	// Dropping the camera socket makes the reader loop reconnect with the
	// new code; the MQTT loop is signalled separately.
	if conn != nil {
		conn.Close()
	}

	select {
	case p.restart <- struct{}{}:
	default:
	}
}

// --- status over MQTT ---------------------------------------------------

// runStatus keeps an MQTT session alive, rebuilding it whenever the access
// code changes (paho fixes credentials at client construction).
func (p *printer) runStatus() {
	for {
		client := p.connectStatus()

		requestTopic := fmt.Sprintf("device/%s/request", p.cfg.Serial)
		ticker := time.NewTicker(60 * time.Second)

		// Nudge the printer for a fresh dump periodically; some fields are only
		// sent on change and we want to recover after a reconnect.
	inner:
		for {
			select {
			case <-ticker.C:
				if client.IsConnected() {
					client.Publish(requestTopic, 0, false, `{"pushing":{"command":"pushall"}}`)
				}
			case <-p.restart:
				log.Printf("printer %s: reconnecting with the new access code", p.cfg.Name)
				break inner
			}
		}

		ticker.Stop()
		client.Disconnect(250)

		p.mu.Lock()
		p.client = nil
		p.mu.Unlock()
	}
}

func (p *printer) connectStatus() mqtt.Client {
	opts := mqtt.NewClientOptions()
	opts.AddBroker(fmt.Sprintf("ssl://%s:%d", p.cfg.Host, printerMQTTPort))
	opts.SetClientID(fmt.Sprintf("rrc-inventory-%s", p.cfg.ID))
	opts.SetUsername("bblp")
	opts.SetPassword(p.accessCode())
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

	return client
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

	previousState := p.state
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

	if info.LightsReport != nil {
		for _, light := range *info.LightsReport {
			if light.Node == "chamber_light" {
				p.lightOn = strings.EqualFold(light.Mode, "on")
			}
		}
	}

	if info.AMS != nil {
		units := make([]AMSUnit, 0, len(info.AMS.AMS))
		for _, raw := range info.AMS.AMS {
			unit := AMSUnit{Humidity: raw.Humidity, Temp: raw.Temp}
			if id, err := strconv.Atoi(raw.ID); err == nil {
				unit.ID = id
			}
			for i, tray := range raw.Tray {
				material := trayMaterial(tray.SubBrands, tray.Type)
				slot := AMSSlot{
					Slot:     i + 1,
					Material: material,
					Color:    trayColor(tray.Color),
					Remain:   tray.Remain,
					Active:   tray.ID != "" && tray.ID == info.AMS.TrayNow,
					Empty:    material == "",
				}
				if slot.Empty {
					slot.Remain = -1
				}
				unit.Slots = append(unit.Slots, slot)
			}
			units = append(units, unit)
		}
		p.amsUnits = units
	}

	if info.VTTray != nil {
		material := trayMaterial(info.VTTray.SubBrands, info.VTTray.Type)
		p.external = &AMSSlot{
			Material: material,
			Color:    trayColor(info.VTTray.Color),
			Remain:   info.VTTray.Remain,
			Empty:    material == "",
		}
		if p.external.Empty {
			p.external.Remain = -1
		}
	}

	if info.HMS != nil {
		faults := make([]HMSFault, 0, len(*info.HMS))
		for _, fault := range *info.HMS {
			if fault.Attr == 0 && fault.Code == 0 {
				continue
			}
			code := formatHMS(fault.Attr, fault.Code)
			faults = append(faults, HMSFault{
				Code:     code,
				Severity: hmsSeverity(fault.Code),
				// Bambu documents each code on its wiki under this path
				URL: "https://wiki.bambulab.com/en/hms/" + code,
			})
		}
		p.faults = faults
	}

	p.trackJobLocked(previousState)
}

// trackJobLocked opens a print job when a machine starts running and closes it
// when it stops. Called with the lock held.
func (p *printer) trackJobLocked(previousState string) {
	if p.jobs == nil {
		return
	}

	now := time.Now()
	running := p.state == "RUNNING" || p.state == "PAUSE"

	// A new job, or the same printer moving on to a different file
	if running && (p.currentJob == nil || p.currentJob.FileName != p.fileName) {
		if p.currentJob != nil {
			p.closeJobLocked("interrupted", now)
		}
		job := &PrintJob{
			PrinterID:   p.cfg.ID,
			PrinterName: p.cfg.Name,
			FileName:    p.fileName,
			StartedAt:   now,
			Result:      "running",
			LastPercent: p.progress,
		}
		if err := p.jobs.Create(job).Error; err == nil {
			p.currentJob = job
		}
		return
	}

	if p.currentJob == nil {
		return
	}

	p.currentJob.LastPercent = p.progress

	if !running && previousState != p.state {
		switch p.state {
		case "FINISH":
			p.closeJobLocked("finished", now)
		case "FAILED":
			p.closeJobLocked("failed", now)
		case "IDLE", "PREPARE":
			// A job that leaves RUNNING for idle was cancelled somehow
			p.closeJobLocked("stopped", now)
		}
	}
}

func (p *printer) closeJobLocked(result string, at time.Time) {
	if p.currentJob == nil {
		return
	}
	p.currentJob.Result = result
	p.currentJob.EndedAt = &at
	if result == "stopped" && p.lastActionBy != "" &&
		time.Since(p.lastActionAt) < 2*time.Minute {
		p.currentJob.StoppedBy = p.lastActionBy
	}
	p.jobs.Save(p.currentJob)
	p.currentJob = nil
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

	if _, err := conn.Write(cameraAuthPacket("bblp", p.accessCode())); err != nil {
		return fmt.Errorf("auth: %w", err)
	}

	// Hold the connection so a credential change can drop it immediately
	p.mu.Lock()
	p.cameraConn = conn
	p.mu.Unlock()

	framesThisConnection := 0
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
			// The printer accepts the connection and then hangs up when the
			// access code is wrong. Nothing received at all points at the
			// credentials rather than the network.
			if framesThisConnection == 0 &&
				(errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF)) {
				p.setAuthFailed(true)
				return fmt.Errorf("printer rejected our access code")
			}
			return fmt.Errorf("read header: %w", err)
		}

		payloadSize := binary.LittleEndian.Uint32(header[0:4])
		if payloadSize == 0 || payloadSize > 8*1024*1024 {
			// Garbage instead of a length header means the handshake was not
			// accepted the way we expect
			if framesThisConnection == 0 {
				p.setAuthFailed(true)
			}
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

		framesThisConnection++

		p.mu.Lock()
		p.lastFrame = frame
		p.lastFrameAt = time.Now()
		p.frameVersion++
		// Frames are proof the credentials are good
		p.authFailed = false
		p.mu.Unlock()
	}
}

func (p *printer) setAuthFailed(failed bool) {
	p.mu.Lock()
	p.authFailed = failed
	p.mu.Unlock()
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
		ID:                p.cfg.ID,
		Name:              p.cfg.Name,
		Online:            online,
		CameraOnline:      cameraOnline,
		AccessCodeProblem: p.authFailed,
	}

	if p.lastActionBy != "" {
		by := p.lastActionBy
		at := p.lastActionAt.Format(time.RFC3339)
		status.LastActionBy = &by
		status.LastActionAt = &at
	}

	status.LightOn = p.lightOn
	status.AMS = p.amsUnits
	if status.AMS == nil {
		status.AMS = []AMSUnit{}
	}
	status.ExternalSpool = p.external
	status.Faults = p.faults
	if status.Faults == nil {
		status.Faults = []HMSFault{}
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

// publishCommand sends one command on the printer's request topic. Every
// write the system can make goes through here.
func (p *printer) publishCommand(payload string) error {
	p.mu.Lock()
	client := p.client
	online := !p.lastReport.IsZero() && time.Since(p.lastReport) < statusStaleAfter
	p.mu.Unlock()

	if client == nil || !client.IsConnected() || !online {
		return fmt.Errorf("printer is not reachable")
	}

	topic := fmt.Sprintf("device/%s/request", p.cfg.Serial)
	token := client.Publish(topic, 0, false, payload)
	if !token.WaitTimeout(10*time.Second) || token.Error() != nil {
		if token.Error() != nil {
			return fmt.Errorf("could not send the command: %w", token.Error())
		}
		return fmt.Errorf("timed out sending the command")
	}
	return nil
}

func (p *printer) nextSequence() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sequence++
	return p.sequence
}

// pauseStates are the states where pausing makes sense.
var pauseStates = map[string]bool{"RUNNING": true, "PREPARE": true}

// pause halts the current job. Unlike stop this is reversible, though a long
// pause can still spoil a print.
func (p *printer) pause(adminName string) error {
	p.mu.RLock()
	state := strings.ToUpper(p.state)
	p.mu.RUnlock()

	if !pauseStates[state] {
		return fmt.Errorf("nothing is printing right now (state: %s)", state)
	}

	payload := fmt.Sprintf(`{"print":{"sequence_id":"%d","command":"pause"}}`, p.nextSequence())
	if err := p.publishCommand(payload); err != nil {
		return err
	}

	p.recordAction(adminName)
	log.Printf("printer %s: pause requested by %s", p.cfg.Name, adminName)
	return nil
}

// resume continues a paused job.
func (p *printer) resume(adminName string) error {
	p.mu.RLock()
	state := strings.ToUpper(p.state)
	p.mu.RUnlock()

	if state != "PAUSE" {
		return fmt.Errorf("the printer is not paused (state: %s)", state)
	}

	payload := fmt.Sprintf(`{"print":{"sequence_id":"%d","command":"resume"}}`, p.nextSequence())
	if err := p.publishCommand(payload); err != nil {
		return err
	}

	p.recordAction(adminName)
	log.Printf("printer %s: resume requested by %s", p.cfg.Name, adminName)
	return nil
}

// setLight switches the chamber light. Harmless, and it makes the camera
// usable when someone has left the light off.
func (p *printer) setLight(on bool) error {
	mode := "off"
	if on {
		mode = "on"
	}

	payload := fmt.Sprintf(
		`{"system":{"sequence_id":"%d","command":"ledctrl","led_node":"chamber_light","led_mode":"%s","led_on_time":500,"led_off_time":500,"loop_times":0,"interval_time":0}}`,
		p.nextSequence(), mode)

	if err := p.publishCommand(payload); err != nil {
		return err
	}

	// Reflect it immediately; the printer confirms in its next report
	p.mu.Lock()
	p.lightOn = on
	p.mu.Unlock()
	return nil
}

func (p *printer) recordAction(adminName string) {
	p.mu.Lock()
	p.lastActionBy = adminName
	p.lastActionAt = time.Now()
	p.mu.Unlock()
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

// Pause halts the current job.
func (m *PrinterManager) Pause(id, adminName string) error {
	p, ok := m.byID[id]
	if !ok {
		return fmt.Errorf("unknown printer")
	}
	return p.pause(adminName)
}

// Resume continues a paused job.
func (m *PrinterManager) Resume(id, adminName string) error {
	p, ok := m.byID[id]
	if !ok {
		return fmt.Errorf("unknown printer")
	}
	return p.resume(adminName)
}

// SetLight switches a printer's chamber light.
func (m *PrinterManager) SetLight(id string, on bool) error {
	p, ok := m.byID[id]
	if !ok {
		return fmt.Errorf("unknown printer")
	}
	return p.setLight(on)
}

// UpdateAccessCode changes a printer's access code, saves it so it survives a
// restart, and reconnects. No downtime, no editing files on the server.
func (m *PrinterManager) UpdateAccessCode(id, code string) error {
	p, ok := m.byID[id]
	if !ok {
		return fmt.Errorf("unknown printer")
	}

	code = strings.TrimSpace(code)
	if code == "" {
		return fmt.Errorf("access code cannot be empty")
	}

	if m.db != nil {
		credential := PrinterCredential{PrinterID: id, AccessCode: code}
		err := m.db.Where(PrinterCredential{PrinterID: id}).
			Assign(PrinterCredential{AccessCode: code}).
			FirstOrCreate(&credential).Error
		if err != nil {
			return fmt.Errorf("could not save the new access code: %w", err)
		}
	}

	p.setAccessCode(code)
	log.Printf("printer %s: access code updated", p.cfg.Name)
	return nil
}

// Configured reports whether any printers are set up at all.
func (m *PrinterManager) Configured() bool {
	return len(m.printers) > 0
}

// loadPrinterManager builds the manager from the environment.
func loadPrinterManager(db *gorm.DB) *PrinterManager {
	configs, err := parsePrinterConfig(os.Getenv("PRINTERS"))
	if err != nil {
		log.Printf("Ignoring PRINTERS setting: %v", err)
		return NewPrinterManager(nil, db)
	}

	if len(configs) == 0 {
		log.Println("No printers configured (set PRINTERS to enable the printer page)")
	} else {
		for _, cfg := range configs {
			log.Printf("Printer configured: %s at %s", cfg.Name, cfg.Host)
		}
	}

	return NewPrinterManager(configs, db)
}
