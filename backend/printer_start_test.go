package main

// The start-print command is the only thing here that makes a machine move, so
// the payload and every guard in front of it are pinned.

import (
	"encoding/json"
	"strings"
	"testing"
)

func decodePayload(t *testing.T, payload string) map[string]any {
	t.Helper()
	var wrapper struct {
		Print map[string]any `json:"print"`
	}
	if err := json.Unmarshal([]byte(payload), &wrapper); err != nil {
		t.Fatalf("payload is not valid JSON: %v\n%s", err, payload)
	}
	return wrapper.Print
}

func TestProjectFilePayloadShape(t *testing.T) {
	req := StartRequest{
		FileName:     "bracket.gcode.3mf",
		Plate:        1,
		BedType:      "textured_plate",
		UseAMS:       true,
		AMSSlot:      2,
		BedLevelling: true,
	}

	print := decodePayload(t, projectFilePayload(7, req))

	checks := map[string]any{
		"sequence_id":   "7",
		"command":       "project_file",
		"param":         "Metadata/plate_1.gcode",
		"url":           "ftp:///bracket.gcode.3mf",
		"subtask_name":  "bracket",
		"bed_type":      "textured_plate",
		"bed_leveling":  true,
		"flow_cali":     false,
		"layer_inspect": false,
		"timelapse":     false,
		"use_ams":       true,
	}
	for key, want := range checks {
		if got := print[key]; got != want {
			t.Errorf("%s = %v (%T), want %v", key, got, got, want)
		}
	}

	mapping, ok := print["ams_mapping"].([]any)
	if !ok || len(mapping) != 1 || mapping[0] != float64(2) {
		t.Errorf("ams_mapping = %v, want [2]", print["ams_mapping"])
	}

	// The firmware wants these present even for a local file
	for _, key := range []string{"profile_id", "project_id", "subtask_id", "task_id"} {
		if print[key] != "0" {
			t.Errorf("%s = %v, want \"0\"", key, print[key])
		}
	}
}

// The url must carry three slashes: an ftp URL with an empty host. Two slashes
// makes the file name the host and the printer finds nothing.
func TestProjectFileURLHasEmptyHost(t *testing.T) {
	payload := projectFilePayload(1, StartRequest{
		FileName: "a.gcode.3mf", Plate: 1, BedType: "cool_plate",
	})
	if !strings.Contains(payload, `"url":"ftp:///a.gcode.3mf"`) {
		t.Errorf("wrong url in payload: %s", payload)
	}
}

// Printing from the external spool must not send a slot mapping.
func TestProjectFileWithoutAMS(t *testing.T) {
	print := decodePayload(t, projectFilePayload(1, StartRequest{
		FileName: "a.gcode.3mf", Plate: 1, BedType: "cool_plate", UseAMS: false, AMSSlot: 3,
	}))

	if print["use_ams"] != false {
		t.Errorf("use_ams = %v, want false", print["use_ams"])
	}
	if mapping, _ := print["ams_mapping"].([]any); len(mapping) != 0 {
		t.Errorf("ams_mapping = %v, want empty when the AMS is not used", print["ams_mapping"])
	}
}

func TestProjectFilePlateNumberIsUsed(t *testing.T) {
	print := decodePayload(t, projectFilePayload(1, StartRequest{
		FileName: "a.gcode.3mf", Plate: 4, BedType: "cool_plate",
	}))
	if print["param"] != "Metadata/plate_4.gcode" {
		t.Errorf("param = %v", print["param"])
	}
}

func TestValidateStart(t *testing.T) {
	// Defaults to plate 1 rather than plate 0, which names a file that does
	// not exist inside any 3MF.
	req := StartRequest{FileName: "a.gcode.3mf", BedType: "cool_plate"}
	if err := validateStart(&req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if req.Plate != 1 {
		t.Errorf("plate defaulted to %d, want 1", req.Plate)
	}

	// A path from a browser must be reduced the same way an upload is
	req = StartRequest{FileName: "../../etc/a.gcode.3mf", BedType: "cool_plate"}
	if err := validateStart(&req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.ContainsAny(req.FileName, "/\\") {
		t.Errorf("path survived validation: %q", req.FileName)
	}

	for _, bad := range []StartRequest{
		{FileName: "", BedType: "cool_plate"},
		{FileName: "a.gcode.3mf", BedType: ""},
		{FileName: "a.gcode.3mf", BedType: "banana_plate"},
		{FileName: "notes.txt", BedType: "cool_plate"},
		{FileName: "a.gcode.3mf", BedType: "cool_plate", Plate: -1},
		{FileName: "a.gcode.3mf", BedType: "cool_plate", UseAMS: true, AMSSlot: -1},
		{FileName: "a.gcode.3mf", BedType: "cool_plate", UseAMS: true, AMSSlot: 99},
	} {
		req := bad
		if err := validateStart(&req); err == nil {
			t.Errorf("validateStart accepted %+v", bad)
		}
	}
}

// A printer part way through a job must never be sent a new one.
func TestStartRefusedWhileBusy(t *testing.T) {
	fake := newFakePrinterFTP(t)
	p := fakePrinter(fake)

	for _, state := range []string{"RUNNING", "PAUSE", "PREPARE", "SLICING"} {
		p.state = state
		err := p.startPrint(StartRequest{
			FileName: "a.gcode.3mf", BedType: "cool_plate",
		}, "tester")
		if err == nil {
			t.Errorf("start allowed while %s", state)
		} else if !strings.Contains(err.Error(), "busy") {
			t.Errorf("unclear error for %s: %v", state, err)
		}
	}
}

// Starting a file that is not on the printer fails fast, rather than leaving
// the firmware to drop back to idle with no explanation.
func TestStartRefusedWhenFileMissing(t *testing.T) {
	fake := newFakePrinterFTP(t)
	p := fakePrinter(fake)
	p.state = "IDLE"

	err := p.startPrint(StartRequest{
		FileName: "missing.gcode.3mf", BedType: "cool_plate",
	}, "tester")
	if err == nil || !strings.Contains(err.Error(), "not on the printer") {
		t.Errorf("expected a missing-file error, got: %v", err)
	}
}
