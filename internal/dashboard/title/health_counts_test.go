package title

import (
	"testing"

	"github.com/rnixai/rnix/internal/dashboard/event"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/ipc"
	"github.com/rnixai/rnix/vfs"
)

// TestComputeHealthCounts_Empty covers nil/empty inputs → both 0.
func TestComputeHealthCounts_Empty(t *testing.T) {
	e, w := ComputeHealthCounts(nil, nil, nil)
	if e != 0 || w != 0 {
		t.Errorf("empty inputs expected (0,0), got (%d,%d)", e, w)
	}
	procs := []vfs.ProcInfo{}
	e, w = ComputeHealthCounts(procs, nil, nil)
	if e != 0 || w != 0 {
		t.Errorf("empty procs expected (0,0), got (%d,%d)", e, w)
	}
}

// TestComputeHealthCounts_DeadFailedCounted covers Dead+IsFailedResult → E++.
// IsFailedResult returns true for non-empty Result strings starting with
// known failure prefixes (e.g. "error:" / "FAILED:"). Empty Result == success.
func TestComputeHealthCounts_DeadFailedCounted(t *testing.T) {
	procs := []vfs.ProcInfo{
		{PID: 1, State: types.StateDead, Result: "error: crash"},
	}
	e, w := ComputeHealthCounts(procs, nil, nil)
	if e != 1 {
		t.Errorf("dead+failed expected E=1, got %d", e)
	}
	if w != 0 {
		t.Errorf("dead+failed expected W=0, got %d", w)
	}
}

// TestComputeHealthCounts_DeadSuccessNotCounted covers Dead with success
// Result → not counted (success exit). Note: ui.IsFailedResult("") returns
// true (empty result == failure), so we use a non-empty success-marker
// string to exercise the green/healthy branch.
func TestComputeHealthCounts_DeadSuccessNotCounted(t *testing.T) {
	procs := []vfs.ProcInfo{
		{PID: 1, State: types.StateDead, Result: "ok"},
	}
	e, w := ComputeHealthCounts(procs, nil, nil)
	if e != 0 {
		t.Errorf("dead+success expected E=0, got %d", e)
	}
	if w != 0 {
		t.Errorf("dead+success expected W=0, got %d", w)
	}
}

// TestComputeHealthCounts_ErrorEventCounted covers EventError type → E++ (PID-keyed).
func TestComputeHealthCounts_ErrorEventCounted(t *testing.T) {
	events := []event.UnifiedEvent{
		{PID: 5, Type: event.EventError, Severity: event.SevError},
	}
	e, _ := ComputeHealthCounts(nil, events, nil)
	if e != 1 {
		t.Errorf("error event expected E=1, got %d", e)
	}
}

// TestComputeHealthCounts_ExitErrorEventCounted covers EventExit with Severity
// >= SevError → E++ (process exited with error).
func TestComputeHealthCounts_ExitErrorEventCounted(t *testing.T) {
	events := []event.UnifiedEvent{
		{PID: 5, Type: event.EventExit, Severity: event.SevError},
		{PID: 6, Type: event.EventExit, Severity: event.SevCritical},
		{PID: 7, Type: event.EventExit, Severity: event.SevWarn}, // not counted
	}
	e, _ := ComputeHealthCounts(nil, events, nil)
	if e != 2 {
		t.Errorf("exit-error events expected E=2, got %d", e)
	}
}

// TestComputeHealthCounts_DedupesByPID covers PID-dedup contract — multiple
// errors from same PID counted once.
func TestComputeHealthCounts_DedupesByPID(t *testing.T) {
	procs := []vfs.ProcInfo{
		{PID: 1, State: types.StateDead, Result: "error: crash"},
	}
	events := []event.UnifiedEvent{
		{PID: 1, Type: event.EventError, Severity: event.SevError},
		{PID: 1, Type: event.EventExit, Severity: event.SevError},
	}
	e, _ := ComputeHealthCounts(procs, events, nil)
	if e != 1 {
		t.Errorf("PID-deduped expected E=1, got %d", e)
	}
}

// TestComputeHealthCounts_HighCtxWarn covers Running with ctx >= 80% → W++.
func TestComputeHealthCounts_HighCtxWarn(t *testing.T) {
	procs := []vfs.ProcInfo{
		{PID: 1, State: types.StateRunning, TokensUsed: 800, ContextBudget: 1000},
	}
	_, w := ComputeHealthCounts(procs, nil, nil)
	if w != 1 {
		t.Errorf("high-ctx expected W=1, got %d", w)
	}
}

// TestComputeHealthCounts_LowCtxNoWarn covers Running with ctx < 80% → no W.
func TestComputeHealthCounts_LowCtxNoWarn(t *testing.T) {
	procs := []vfs.ProcInfo{
		{PID: 1, State: types.StateRunning, TokensUsed: 700, ContextBudget: 1000},
	}
	_, w := ComputeHealthCounts(procs, nil, nil)
	if w != 0 {
		t.Errorf("low-ctx expected W=0, got %d", w)
	}
}

// TestComputeHealthCounts_HighCostWarn covers cost budget >= 80% → W++.
func TestComputeHealthCounts_HighCostWarn(t *testing.T) {
	procs := []vfs.ProcInfo{
		{PID: 1, State: types.StateRunning, UsedCost: 0.85, MaxCost: 1.0},
	}
	_, w := ComputeHealthCounts(procs, nil, nil)
	if w != 1 {
		t.Errorf("high-cost expected W=1, got %d", w)
	}
}

// TestComputeHealthCounts_HighTokenWarn covers token budget >= 80% → W++.
func TestComputeHealthCounts_HighTokenWarn(t *testing.T) {
	procs := []vfs.ProcInfo{
		{PID: 1, State: types.StateRunning, TokensUsed: 800, MaxTokens: 1000},
	}
	_, w := ComputeHealthCounts(procs, nil, nil)
	if w != 1 {
		t.Errorf("high-token expected W=1, got %d", w)
	}
}

// TestComputeHealthCounts_DeadDoesNotWarn covers Dead state — no warning even
// if budget thresholds hit (only Running/Created counted as warnings).
func TestComputeHealthCounts_DeadDoesNotWarn(t *testing.T) {
	procs := []vfs.ProcInfo{
		{PID: 1, State: types.StateDead, TokensUsed: 800, ContextBudget: 1000},
	}
	_, w := ComputeHealthCounts(procs, nil, nil)
	if w != 0 {
		t.Errorf("dead+high-ctx expected W=0, got %d", w)
	}
}

// TestComputeHealthCounts_HeartbeatStalledWarn covers heartbeat-stalled PIDs → W++.
func TestComputeHealthCounts_HeartbeatStalledWarn(t *testing.T) {
	heartbeat := &ipc.HeartbeatStatusResponse{
		CurrentStalled: []ipc.StalledProcWire{
			{PID: 1},
			{PID: 2},
		},
	}
	_, w := ComputeHealthCounts(nil, nil, heartbeat)
	if w != 2 {
		t.Errorf("stalled expected W=2, got %d", w)
	}
}

// TestComputeHealthCounts_HeartbeatNilSafe covers nil heartbeat → no panic.
func TestComputeHealthCounts_HeartbeatNilSafe(t *testing.T) {
	procs := []vfs.ProcInfo{
		{PID: 1, State: types.StateRunning},
	}
	e, w := ComputeHealthCounts(procs, nil, nil)
	if e != 0 || w != 0 {
		t.Errorf("nil heartbeat expected (0,0), got (%d,%d)", e, w)
	}
}

// TestComputeHealthCounts_WarnDedupesByPID covers PID-dedup for warnings —
// same PID hitting multiple thresholds counted once.
func TestComputeHealthCounts_WarnDedupesByPID(t *testing.T) {
	procs := []vfs.ProcInfo{
		{
			PID:           1,
			State:         types.StateRunning,
			TokensUsed:    900,
			ContextBudget: 1000, // ctx 90%
			UsedCost:      0.95,
			MaxCost:       1.0, // cost 95%
			MaxTokens:     1000, // token 90%
		},
	}
	heartbeat := &ipc.HeartbeatStatusResponse{
		CurrentStalled: []ipc.StalledProcWire{{PID: 1}}, // also stalled
	}
	_, w := ComputeHealthCounts(procs, nil, heartbeat)
	if w != 1 {
		t.Errorf("PID-deduped warnings expected W=1, got %d", w)
	}
}

// TestComputeHealthCounts_ComplexScenario covers a realistic multi-process
// dashboard state with various error and warn sources.
func TestComputeHealthCounts_ComplexScenario(t *testing.T) {
	procs := []vfs.ProcInfo{
		{PID: 1, State: types.StateDead, Result: "error: crash"},                              // E
		{PID: 2, State: types.StateRunning, TokensUsed: 800, ContextBudget: 1000},             // W (ctx)
		{PID: 3, State: types.StateRunning, UsedCost: 0.5, MaxCost: 1.0},                      // healthy
		{PID: 4, State: types.StateCreated, TokensUsed: 850, MaxTokens: 1000},                 // W (token)
		{PID: 5, State: types.StateDead, Result: "ok"},                                          // healthy dead
	}
	events := []event.UnifiedEvent{
		{PID: 6, Type: event.EventError, Severity: event.SevError}, // E
	}
	heartbeat := &ipc.HeartbeatStatusResponse{
		CurrentStalled: []ipc.StalledProcWire{{PID: 7}}, // W (stalled)
	}
	e, w := ComputeHealthCounts(procs, events, heartbeat)
	if e != 2 {
		t.Errorf("complex E expected 2, got %d", e)
	}
	if w != 3 {
		t.Errorf("complex W expected 3, got %d", w)
	}
}
