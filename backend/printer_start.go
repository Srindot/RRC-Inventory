package main

// Starting a print.
//
// Everything else this system sends a printer is either read-only or a way to
// stop something. This is the one command that makes a machine move on its own,
// so it is admin-only, the printer must be idle, and the file has to be there
// already. Whoever presses the button is responsible for the plate being clear;
// the camera on the admin page is there to be looked at first.

import (
	"fmt"
	"log"
	"strings"
)

// startableStates are the states where a new job can be dispatched. Sending a
// project_file to a printer that is already running is rejected by the
// firmware, and on some models cancels the running job.
var startableStates = map[string]bool{
	"IDLE":   true,
	"FINISH": true,
	"FAILED": true,
}

// bedTypes are the plate names the firmware accepts. An unknown string gets the
// job refused with nothing useful said about why.
var bedTypes = map[string]bool{
	"cool_plate":      true,
	"eng_plate":       true,
	"high_temp_plate": true,
	"textured_plate":  true,
	"supertack_plate": true,
}

// StartRequest is one dispatch, as the admin page describes it.
type StartRequest struct {
	// FileName is a file already on the printer, as ListFiles reports it.
	FileName string `json:"file_name"`
	// Plate is the plate inside the 3MF. A file exported with "export plate
	// sliced file" holds a single plate, numbered 1.
	Plate int `json:"plate"`
	// BedType must match the plate actually fitted. The printer checks it, and
	// a mismatch is what raises the bed-detect fault.
	BedType string `json:"bed_type"`
	// UseAMS false prints from the external spool holder instead.
	UseAMS bool `json:"use_ams"`
	// AMSSlot is the global slot index to print from, zero based and counted
	// across units: unit 1 holds 0-3, unit 2 holds 4-7. Only read when UseAMS
	// is set.
	AMSSlot int `json:"ams_slot"`
	// BedLevelling is worth leaving on: it is the recovery for a marginal bed
	// probe, which is the fault this lab keeps hitting.
	BedLevelling bool `json:"bed_levelling"`
	FlowCali     bool `json:"flow_cali"`
	Timelapse    bool `json:"timelapse"`
}

// projectFilePayload builds the MQTT command that starts a print.
//
// The shape matches what Bambu Studio sends for a file already on the printer's
// storage. Two details are easy to get wrong and silent when wrong: `url` is an
// ftp URL with an empty host, so it carries three slashes, and `param` names the
// plate's gcode inside the 3MF rather than the file itself. `subtask_name` is
// what the printer puts on its screen; without it the job shows as unnamed.
//
// Split out from publishing so it can be asserted in tests without a printer.
func projectFilePayload(sequence int, req StartRequest) string {
	amsMapping := "[]"
	if req.UseAMS {
		amsMapping = fmt.Sprintf("[%d]", req.AMSSlot)
	}

	subtask := strings.TrimSuffix(req.FileName, ".gcode.3mf")
	subtask = strings.TrimSuffix(subtask, ".3mf")
	subtask = strings.TrimSuffix(subtask, ".gcode")

	return fmt.Sprintf(`{"print":{`+
		`"sequence_id":"%d",`+
		`"command":"project_file",`+
		`"param":"Metadata/plate_%d.gcode",`+
		`"url":"ftp:///%s",`+
		`"subtask_name":"%s",`+
		`"bed_type":"%s",`+
		`"bed_leveling":%t,`+
		`"flow_cali":%t,`+
		`"vibration_cali":true,`+
		`"layer_inspect":false,`+
		`"timelapse":%t,`+
		`"use_ams":%t,`+
		`"ams_mapping":%s,`+
		`"profile_id":"0","project_id":"0","subtask_id":"0","task_id":"0"`+
		`}}`,
		sequence, req.Plate, req.FileName, subtask, req.BedType,
		req.BedLevelling, req.FlowCali, req.Timelapse, req.UseAMS, amsMapping)
}

// validateStart checks and normalises a request before anything is published.
func validateStart(req *StartRequest) error {
	if strings.TrimSpace(req.FileName) == "" {
		return fmt.Errorf("choose a file to print")
	}

	// The name has to survive the same cleaning an upload goes through, or it
	// is not a name that can be on the printer in the first place.
	safe, err := sanitizeUploadName(req.FileName)
	if err != nil {
		return err
	}
	req.FileName = safe

	if req.Plate == 0 {
		req.Plate = 1
	}
	if req.Plate < 1 || req.Plate > 64 {
		return fmt.Errorf("plate number %d is not valid", req.Plate)
	}

	if req.BedType == "" {
		return fmt.Errorf("choose the plate that is on the printer")
	}
	if !bedTypes[req.BedType] {
		return fmt.Errorf("unknown plate type %q", req.BedType)
	}

	if req.UseAMS && (req.AMSSlot < 0 || req.AMSSlot > 15) {
		return fmt.Errorf("AMS slot %d is not valid", req.AMSSlot)
	}
	return nil
}

// startPrint dispatches one job, checking every guard before publishing.
func (p *printer) startPrint(req StartRequest, adminName string) error {
	if err := validateStart(&req); err != nil {
		return err
	}

	p.mu.RLock()
	state := strings.ToUpper(p.state)
	p.mu.RUnlock()

	if !startableStates[state] {
		return fmt.Errorf("the printer is busy (state: %s) - wait for it to finish", state)
	}

	// Refuse a file that is not actually there. Without this the firmware
	// accepts the command, fails to open the file, and drops back to idle with
	// nothing said about why - which is indistinguishable from the job simply
	// never starting.
	files, err := p.ListFiles()
	if err != nil {
		return fmt.Errorf("could not check the printer's files: %w", err)
	}
	found := false
	for _, f := range files {
		if f.Name == req.FileName {
			found = true
			break
		}
	}
	if !found {
		return fmt.Errorf("%s is not on the printer - send it first", req.FileName)
	}

	if err := p.publishCommand(projectFilePayload(p.nextSequence(), req)); err != nil {
		return err
	}

	p.recordAction(adminName)
	log.Printf("printer %s: start %s (plate %d, bed %s, ams %v slot %d) requested by %s",
		p.cfg.Name, req.FileName, req.Plate, req.BedType, req.UseAMS, req.AMSSlot, adminName)
	return nil
}

// StartPrint dispatches a job on one printer.
func (m *PrinterManager) StartPrint(id string, req StartRequest, adminName string) error {
	p, ok := m.byID[id]
	if !ok {
		return fmt.Errorf("unknown printer")
	}
	return p.startPrint(req, adminName)
}
