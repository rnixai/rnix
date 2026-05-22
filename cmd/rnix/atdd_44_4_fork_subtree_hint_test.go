package main

import (
	"strings"
	"testing"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/ipc"
	"github.com/rnixai/rnix/vfs"
)

// =============================================================================
// ATDD 44.4 — AC#5: forking a process that has Suspended descendants surfaces
// an explicit "(N suspended descendants left intact — resume them separately)"
// hint (Decision D2: fork does NOT copy the subtree; kernel resume.go:850
// `if !opts.Fork` guard is unchanged — the dashboard merely informs the user).
//
// RED expression (behavioural): handleForkResult currently formats only
// "Forked UUID … → PID … (from …)" with no descendant scan. Asserting the
// statusMsg mentions "suspended descendants" fails today → RED; passes once
// dev-story Task 5.2 adds the m.processes scan for ParentUUID/PPID-linked
// Suspended nodes.
//
// The scan must run BEFORE handleForkResult reassigns m.selectedPID/UUID to the
// new fork, so the fixtures keep the ORIGIN process selected.
// =============================================================================

const forkOriginUUID = "uuid-44-4-fork-origin"

func forkHintModel(withSuspendedDescendants bool) dashboardModel {
	m := newDashboardModel(nil)
	m.connected = true
	m.selectedPID = types.PID(400)
	m.selectedUUID = forkOriginUUID

	procs := []vfs.ProcInfo{{
		PID:    types.PID(400),
		UUID:   forkOriginUUID,
		State:  types.StateDead, // fork source is Dead/Zombie (forkProcessHandler guard)
		Intent: "fork origin",
	}}
	if withSuspendedDescendants {
		procs = append(procs,
			vfs.ProcInfo{
				PID:        types.PID(401),
				UUID:       "uuid-44-4-fork-childA",
				PPID:       types.PID(400),
				ParentUUID: forkOriginUUID,
				State:      types.StateSuspended,
				Intent:     "suspended descendant A",
			},
			vfs.ProcInfo{
				PID:        types.PID(402),
				UUID:       "uuid-44-4-fork-childB",
				PPID:       types.PID(400),
				ParentUUID: forkOriginUUID,
				State:      types.StateSuspended,
				Intent:     "suspended descendant B",
			},
		)
	}
	m.processes = procs
	return m
}

func TestATDD_44_4_050_ForkResult_WithSuspendedDescendants_ShowsHint(t *testing.T) {
	m := forkHintModel(true)

	got, _ := handleForkResult(m, forkResultMsg{result: &ipc.ResumeResponse{
		UUID: "uuid-44-4-fork-new",
		PID:  types.PID(999),
	}})
	if !strings.Contains(got.statusMsg, "suspended descendants") {
		t.Errorf("handleForkResult statusMsg = %q, want it to contain \"suspended descendants\" "+
			"(AC#5: fork left the Suspended subtree intact, inform the user)", got.statusMsg)
	}
}

func TestATDD_44_4_051_ForkResult_NoSuspendedDescendants_NoHint(t *testing.T) {
	m := forkHintModel(false)

	got, _ := handleForkResult(m, forkResultMsg{result: &ipc.ResumeResponse{
		UUID: "uuid-44-4-fork-new",
		PID:  types.PID(999),
	}})
	if strings.Contains(got.statusMsg, "suspended descendants") {
		t.Errorf("handleForkResult statusMsg = %q, must NOT mention \"suspended descendants\" "+
			"when the fork source has none (AC#5: avoid noise)", got.statusMsg)
	}
}
