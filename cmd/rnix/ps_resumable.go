package main

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/rnixai/rnix/ipc"
)

// =============================================================================
// Story 42.2 — psCmd --resumable rendering
//
// RED PHASE: render functions are stubs that emit minimal output so tests can
// compile and exercise the shape of the rendering layer. dev-story will replace
// these with real table / JSON / quiet renderers that consume
// ipc.ResumableProcessWire and format LastActive as a relative time
// ("5m ago"), trim UUID, and align CJK-safe columns.
// =============================================================================

// flagResumable is the boolean backing the --resumable flag on psCmd.
//
// RED PHASE: declared but not yet bound to psCmd.Flags() — dev-story will wire
// it during Task 5.1.
var flagResumable bool

// renderResumableJSON writes a JSON envelope of resumable processes.
//
// RED PHASE: stub writes empty JSON list.
func renderResumableJSON(w io.Writer, procs []ipc.ResumableProcessWire) {
	resp := JSONResponse{OK: true, Data: map[string]any{"processes": procs}}
	data, _ := json.Marshal(resp)
	fmt.Fprintln(w, string(data))
}

// renderResumableQuiet writes one UUID per line.
//
// RED PHASE: stub writes UUIDs only.
func renderResumableQuiet(w io.Writer, procs []ipc.ResumableProcessWire) {
	for _, p := range procs {
		fmt.Fprintln(w, p.UUID)
	}
}

// renderResumableTable writes a human-readable table.
//
// RED PHASE: stub writes one summary line per row + a leading header. Empty
// list writes "No resumable processes." (AC#6).
func renderResumableTable(w io.Writer, procs []ipc.ResumableProcessWire) {
	if len(procs) == 0 {
		fmt.Fprintln(w, "No resumable processes.")
		return
	}
	fmt.Fprintln(w, "UUID\tAGENT\tLAST_STEP\tLAST_ACTIVE")
	for _, p := range procs {
		fmt.Fprintf(w, "%s\t%s\t%d\t%s\n",
			truncateUUIDForPs(p.UUID),
			p.Agent,
			p.LastStep,
			formatRelativeTimeForPs(p.LastActive),
		)
	}
}

// truncateUUIDForPs shortens a UUID to its first 8 characters for compact display.
//
// RED PHASE: returns the first 8 chars; dev-story may tweak width.
func truncateUUIDForPs(uuid string) string {
	if len(uuid) <= 8 {
		return uuid
	}
	return uuid[:8]
}

// formatRelativeTimeForPs renders a millisecond-unix timestamp as "5m ago" /
// "2h ago" / "just now". Zero input returns "-".
//
// RED PHASE: stub returns "-" until dev-story implements duration math.
func formatRelativeTimeForPs(_ int64) string {
	return "-"
}

// nowForPs is a test seam so tests can stub time.Now without depending on
// monkey patching. Production callers use the real clock.
var nowForPs = time.Now
