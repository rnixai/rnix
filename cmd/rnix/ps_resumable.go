package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"
)

// =============================================================================
// Story 42.2 — `rnix ps --resumable` rendering and runner.
//
// Renders crash-leftover processes (state=running on disk, UUID not in
// procTable). Supports default table, --json, and --quiet modes. Empty list
// emits "No resumable processes." in non-quiet modes (AC#6).
// =============================================================================

// flagResumable backs the --resumable flag on psCmd.
var flagResumable bool

// nowForPs is a test seam so tests can stub time.Now without depending on
// monkey patching. Production callers use the real clock.
var nowForPs = time.Now

// runPsResumable dials the daemon, fetches resumable processes, and renders
// them according to the active output mode.
func runPsResumable(renderer *ui.Renderer, mode ui.OutputMode) error {
	client, err := ipc.Dial(ipc.SocketPath())
	if err != nil {
		// Daemon not running: behave the same as ps default — empty result.
		switch mode {
		case ui.ModeJSON:
			renderResumableJSON(renderer.Writer, nil)
		case ui.ModeQuiet:
			// no output
		default:
			fmt.Fprintln(renderer.Writer, "No resumable processes.")
		}
		return nil
	}
	defer client.Close()

	resp, err := client.ListResumable()
	if err != nil {
		if mode != ui.ModeQuiet {
			fmt.Fprintf(os.Stderr, "✗ failed to list resumable processes: %v\n", err)
		}
		exitCode = 1
		return nil
	}

	procs := resp.Processes
	switch mode {
	case ui.ModeJSON:
		renderResumableJSON(renderer.Writer, procs)
	case ui.ModeQuiet:
		renderResumableQuiet(renderer.Writer, procs)
	default:
		renderResumableTable(renderer.Writer, procs)
	}
	return nil
}

// renderResumableJSON writes a JSON envelope of resumable processes.
func renderResumableJSON(w io.Writer, procs []ipc.ResumableProcessWire) {
	if procs == nil {
		procs = []ipc.ResumableProcessWire{}
	}
	resp := JSONResponse{OK: true, Data: map[string]any{"processes": procs}}
	data, _ := json.Marshal(resp)
	fmt.Fprintln(w, string(data))
}

// renderResumableQuiet writes one UUID per line.
func renderResumableQuiet(w io.Writer, procs []ipc.ResumableProcessWire) {
	for _, p := range procs {
		fmt.Fprintln(w, p.UUID)
	}
}

// renderResumableTable writes a human-readable table.
//
//	UUID       AGENT             LAST_STEP  LAST_ACTIVE
//	abcdef12   code-analyst      12         5m ago
func renderResumableTable(w io.Writer, procs []ipc.ResumableProcessWire) {
	if len(procs) == 0 {
		fmt.Fprintln(w, "No resumable processes.")
		return
	}
	fmt.Fprintf(w, "%-10s  %-20s  %9s  %s\n", "UUID", "AGENT", "LAST_STEP", "LAST_ACTIVE")
	for _, p := range procs {
		agent := p.Agent
		if agent == "" {
			agent = "-"
		}
		fmt.Fprintf(w, "%-10s  %-20s  %9d  %s\n",
			truncateUUIDForPs(p.UUID),
			truncateAgentForPs(agent),
			p.LastStep,
			formatRelativeTimeForPs(p.LastActive),
		)
	}
}

// truncateUUIDForPs shortens a UUID to its first 8 characters.
func truncateUUIDForPs(uuid string) string {
	if len(uuid) <= 8 {
		return uuid
	}
	return uuid[:8]
}

// truncateAgentForPs trims agent labels to 20 chars (by rune to stay CJK-safe).
func truncateAgentForPs(agent string) string {
	runes := []rune(agent)
	if len(runes) <= 20 {
		return agent
	}
	return string(runes[:19]) + "…"
}

// formatRelativeTimeForPs renders a millisecond-unix timestamp as "5m ago",
// "2h ago", "3d ago", or "just now". Zero or future input returns "-".
func formatRelativeTimeForPs(activeMs int64) string {
	if activeMs <= 0 {
		return "-"
	}
	t := time.UnixMilli(activeMs)
	d := nowForPs().Sub(t)
	if d < 0 {
		return "-"
	}
	switch {
	case d < 30*time.Second:
		return "just now"
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}

// toInt extracts an int from a YAML-decoded scalar (handles int, int64, float64).
// Returns false when the value cannot be losslessly represented as int.
func toInt(v any) (int, bool) {
	switch x := v.(type) {
	case int:
		return x, true
	case int64:
		return int(x), true
	case float64:
		if x != float64(int(x)) {
			return 0, false
		}
		return int(x), true
	}
	return 0, false
}
