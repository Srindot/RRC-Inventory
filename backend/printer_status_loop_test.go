package main

// The MQTT connect loop, the offline view of a printer, and access code
// checks - the paths an admin relies on when a printer's code has changed.

import (
	"strings"
	"testing"
	"time"
)

func waitFor(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func (p *printer) statusConnected() bool {
	p.mu.RLock()
	client := p.client
	p.mu.RUnlock()
	return client != nil && client.IsConnected()
}

// The bug this pins: with paho's ConnectRetry on, a printer rejecting the code
// kept Connect().Wait() blocked forever, so a code fixed from the admin page
// never reached MQTT. The new code must be tried at once - well inside the
// backoff - and the session must come up with it.
func TestStatusLoopPicksUpNewAccessCode(t *testing.T) {
	broker := newFakePrinterMQTT(t, "SERIAL1", "newcode")

	p := &printer{
		cfg: PrinterConfig{
			ID: "fake", Name: "fake", Host: "127.0.0.1",
			Serial: "SERIAL1", AccessCode: "oldcode",
		},
		mqttPort: broker.port(),
		restart:  make(chan struct{}, 1),
		quit:     make(chan struct{}),
		// Long enough that only a restart can explain a quick reconnect
		statusRetry: time.Minute,
	}

	done := make(chan struct{})
	go func() {
		p.runStatus()
		close(done)
	}()

	waitFor(t, 10*time.Second, "an attempt with the old code", func() bool {
		return len(broker.tried()) > 0
	})
	if p.statusConnected() {
		t.Fatal("connected with a code the printer rejected")
	}

	p.setAccessCode("newcode")

	waitFor(t, 10*time.Second, "a connection with the new code", p.statusConnected)
	tried := broker.tried()
	if tried[len(tried)-1] != "newcode" {
		t.Errorf("last code tried was %q, want newcode", tried[len(tried)-1])
	}

	// The report the broker pushes on subscribe must land
	waitFor(t, 5*time.Second, "the first report", p.online)

	close(p.quit)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("runStatus did not stop on shutdown")
	}
	if p.statusConnected() {
		t.Error("client still connected after shutdown")
	}
}

// A dropped connection comes back on its own: paho's auto-reconnect is off,
// so the loop has to rebuild the session itself.
func TestStatusLoopReconnectsAfterDrop(t *testing.T) {
	broker := newFakePrinterMQTT(t, "SERIAL1", "code1234")

	p := &printer{
		cfg: PrinterConfig{
			ID: "fake", Name: "fake", Host: "127.0.0.1",
			Serial: "SERIAL1", AccessCode: "code1234",
		},
		mqttPort:    broker.port(),
		restart:     make(chan struct{}, 1),
		quit:        make(chan struct{}),
		statusRetry: 100 * time.Millisecond,
	}

	done := make(chan struct{})
	go func() {
		p.runStatus()
		close(done)
	}()
	defer func() {
		close(p.quit)
		<-done
	}()

	waitFor(t, 10*time.Second, "the first connection", p.statusConnected)

	broker.dropAll()
	waitFor(t, 10*time.Second, "a reconnect after the connection dropped", func() bool {
		return broker.acceptedCount() >= 2 && p.statusConnected()
	})
}

// Shutdown must also interrupt the backoff wait after a failed attempt.
func TestStatusLoopStopsDuringBackoff(t *testing.T) {
	broker := newFakePrinterMQTT(t, "SERIAL1", "right")

	p := &printer{
		cfg: PrinterConfig{
			ID: "fake", Name: "fake", Host: "127.0.0.1",
			Serial: "SERIAL1", AccessCode: "wrong",
		},
		mqttPort:    broker.port(),
		restart:     make(chan struct{}, 1),
		quit:        make(chan struct{}),
		statusRetry: time.Minute,
	}

	done := make(chan struct{})
	go func() {
		p.runStatus()
		close(done)
	}()

	waitFor(t, 10*time.Second, "a rejected attempt", func() bool {
		return len(broker.tried()) > 0
	})
	close(p.quit)

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("runStatus stayed in its backoff after shutdown")
	}
}

// A printer that has gone quiet must not keep showing what it last said: the
// light, spools and faults are as stale as the temperatures.
func TestStatusHidesStaleFieldsWhenOffline(t *testing.T) {
	p := &printer{}
	p.applyReport([]byte(`{"print":{
		"gcode_state":"RUNNING",
		"lights_report":[{"node":"chamber_light","mode":"on"}],
		"ams":{"tray_now":"0","ams":[{"id":"0","humidity":"3","temp":"25","tray":[
			{"id":"0","tray_type":"PLA","tray_color":"FF0000FF","remain":50}]}]},
		"vt_tray":{"id":"254","tray_type":"PETG","tray_color":"00FF00FF","remain":10},
		"hms":[{"attr":50331904,"code":131073}]
	}}`))

	online := p.status()
	if !online.LightOn || len(online.AMS) != 1 || online.ExternalSpool == nil || len(online.Faults) != 1 {
		t.Fatalf("online status is missing fields: %+v", online)
	}

	p.mu.Lock()
	p.lastReport = time.Now().Add(-statusStaleAfter - time.Minute)
	p.mu.Unlock()

	offline := p.status()
	if offline.Online {
		t.Fatal("printer should be offline")
	}
	if offline.LightOn {
		t.Error("light shown on for an offline printer")
	}
	if offline.AMS == nil || len(offline.AMS) != 0 {
		t.Errorf("AMS should be an empty list when offline, got %+v", offline.AMS)
	}
	if offline.ExternalSpool != nil {
		t.Errorf("external spool shown for an offline printer: %+v", offline.ExternalSpool)
	}
	if offline.Faults == nil || len(offline.Faults) != 0 {
		t.Errorf("faults should be an empty list when offline, got %+v", offline.Faults)
	}
}

func TestValidateAccessCode(t *testing.T) {
	if code, err := validateAccessCode("  89a8541a \n"); err != nil || code != "89a8541a" {
		t.Errorf("a normal code was refused or not trimmed: %q %v", code, err)
	}
	if _, err := validateAccessCode(strings.Repeat("a", maxAccessCodeLength)); err != nil {
		t.Errorf("a code exactly at the limit was refused: %v", err)
	}

	for _, bad := range []string{
		"",
		"   ",
		strings.Repeat("a", maxAccessCodeLength+1),
		"89a8 541a",
		"89a8\x00541a",
		"89a8\t541a",
		"cöde1234",
	} {
		if _, err := validateAccessCode(bad); err == nil {
			t.Errorf("validateAccessCode(%q) was accepted", bad)
		}
	}
}

// The handler shows UpdateAccessCode's error as it is, and a refused code must
// leave the working one in place.
func TestUpdateAccessCodeRejectsBadCodes(t *testing.T) {
	p := &printer{
		cfg:     PrinterConfig{ID: "p1", Name: "p1", AccessCode: "good1234"},
		restart: make(chan struct{}, 1),
	}
	m := &PrinterManager{byID: map[string]*printer{"p1": p}}

	err := m.UpdateAccessCode("p1", strings.Repeat("x", 40))
	if err == nil || !strings.Contains(err.Error(), "too long") {
		t.Errorf("an over-long code got: %v", err)
	}
	if p.accessCode() != "good1234" {
		t.Errorf("a refused code replaced the working one: %q", p.accessCode())
	}
	select {
	case <-p.restart:
		t.Error("a refused code still forced a reconnect")
	default:
	}

	if err := m.UpdateAccessCode("p1", " new12345 "); err != nil {
		t.Fatalf("a good code was refused: %v", err)
	}
	if p.accessCode() != "new12345" {
		t.Errorf("code = %q, want new12345", p.accessCode())
	}
}
