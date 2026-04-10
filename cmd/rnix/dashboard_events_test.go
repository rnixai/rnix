package main

import (
	"sort"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/ipc"
	"github.com/rnixai/rnix/vfs"
)

// --- Task 4.1: TestUnifiedEvent_SortByTimestamp ---

func TestUnifiedEvent_SortByTimestamp(t *testing.T) {
	now := time.Now()
	events := UnifiedEventSlice{
		{Type: EventStep, Timestamp: now.Add(2 * time.Second), Summary: "third"},
		{Type: EventCompact, Timestamp: now, Summary: "first"},
		{Type: EventBudget, Timestamp: now.Add(1 * time.Second), Summary: "second"},
	}
	sort.Sort(events)

	if events[0].Summary != "first" {
		t.Errorf("expected first event 'first', got %q", events[0].Summary)
	}
	if events[1].Summary != "second" {
		t.Errorf("expected second event 'second', got %q", events[1].Summary)
	}
	if events[2].Summary != "third" {
		t.Errorf("expected third event 'third', got %q", events[2].Summary)
	}
}

// --- Task 4.2: TestStepToUnifiedEvent ---

func TestStepToUnifiedEvent(t *testing.T) {
	t.Run("normal step", func(t *testing.T) {
		e := &stepEntry{
			summary: ipc.StepSummaryWire{
				Step:       1,
				Action:     "tool_call",
				Summary:    "read file",
				DurationMs: 500,
				HasError:   false,
			},
		}
		ue := stepToUnifiedEvent(e)
		if ue.Type != EventStep {
			t.Errorf("expected type %q, got %q", EventStep, ue.Type)
		}
		if ue.Severity != SevInfo {
			t.Errorf("expected severity %d, got %d", SevInfo, ue.Severity)
		}
		if ue.StepEntry != e {
			t.Error("StepEntry pointer mismatch")
		}
		if ue.Timestamp.IsZero() {
			t.Error("timestamp should not be zero")
		}
	})

	t.Run("error step", func(t *testing.T) {
		e := &stepEntry{
			summary: ipc.StepSummaryWire{
				Step:     2,
				Action:   "tool_call",
				Summary:  "failed write",
				HasError: true,
			},
		}
		ue := stepToUnifiedEvent(e)
		if ue.Severity != SevError {
			t.Errorf("expected severity %d for error step, got %d", SevError, ue.Severity)
		}
	})
}

// --- Task 4.3: TestCompactEventFromSyscall ---

func TestCompactEventFromSyscall(t *testing.T) {
	ev := ipc.SyscallEventWire{
		TimestampMs: time.Now().UnixMilli(),
		PID:         42,
		Syscall:     "Compact",
		Args: map[string]any{
			"pre_tokens":     float64(180000),
			"post_tokens":    float64(52000),
			"restored_items": float64(3),
			"duration_ms":    float64(1200),
		},
	}

	ue := compactEventFromSyscall(ev)
	if ue.Type != EventCompact {
		t.Errorf("expected type %q, got %q", EventCompact, ue.Type)
	}
	if ue.Severity != SevInfo {
		t.Errorf("expected severity %d, got %d", SevInfo, ue.Severity)
	}
	if ue.PID != 42 {
		t.Errorf("expected PID 42, got %d", ue.PID)
	}
	if ue.RawEvent == nil {
		t.Error("RawEvent should not be nil")
	}
	// Check summary contains key info
	if ue.Summary == "" {
		t.Error("summary should not be empty")
	}
}

// --- Task 4.4: TestDetectSpawnExitEvents ---

func TestDetectSpawnExitEvents(t *testing.T) {
	now := time.Now()

	t.Run("detect spawn", func(t *testing.T) {
		prev := map[types.PID]vfs.ProcInfo{
			1: {PID: 1, State: types.StateRunning, Intent: "init", CreatedAt: now},
		}
		curr := []vfs.ProcInfo{
			{PID: 1, State: types.StateRunning, Intent: "init", CreatedAt: now},
			{PID: 2, State: types.StateRunning, Intent: "review", CreatedAt: now},
		}
		events := detectSpawnExitEvents(prev, curr)
		if len(events) != 1 {
			t.Fatalf("expected 1 spawn event, got %d", len(events))
		}
		if events[0].Type != EventSpawn {
			t.Errorf("expected type %q, got %q", EventSpawn, events[0].Type)
		}
		if events[0].PID != 2 {
			t.Errorf("expected PID 2, got %d", events[0].PID)
		}
	})

	t.Run("detect exit by state change", func(t *testing.T) {
		prev := map[types.PID]vfs.ProcInfo{
			1: {PID: 1, State: types.StateRunning, Intent: "init", CreatedAt: now},
		}
		curr := []vfs.ProcInfo{
			{PID: 1, State: types.StateDead, Intent: "init", CreatedAt: now, DeadAt: now},
		}
		events := detectSpawnExitEvents(prev, curr)
		if len(events) != 1 {
			t.Fatalf("expected 1 exit event, got %d", len(events))
		}
		if events[0].Type != EventExit {
			t.Errorf("expected type %q, got %q", EventExit, events[0].Type)
		}
		if events[0].Severity != SevInfo {
			t.Errorf("expected info severity for clean exit, got %d", events[0].Severity)
		}
	})

	t.Run("detect exit with error", func(t *testing.T) {
		prev := map[types.PID]vfs.ProcInfo{
			1: {PID: 1, State: types.StateRunning, Intent: "build", CreatedAt: now},
		}
		curr := []vfs.ProcInfo{
			{PID: 1, State: types.StateDead, Intent: "build", Result: "error: timeout", CreatedAt: now, DeadAt: now},
		}
		events := detectSpawnExitEvents(prev, curr)
		if len(events) != 1 {
			t.Fatalf("expected 1 exit event, got %d", len(events))
		}
		if events[0].Severity != SevError {
			t.Errorf("expected error severity for failed exit, got %d", events[0].Severity)
		}
	})

	t.Run("detect exit by disappearance", func(t *testing.T) {
		prev := map[types.PID]vfs.ProcInfo{
			1: {PID: 1, State: types.StateRunning, Intent: "init", CreatedAt: now},
			2: {PID: 2, State: types.StateRunning, Intent: "build", CreatedAt: now},
		}
		curr := []vfs.ProcInfo{
			{PID: 1, State: types.StateRunning, Intent: "init", CreatedAt: now},
		}
		events := detectSpawnExitEvents(prev, curr)
		if len(events) != 1 {
			t.Fatalf("expected 1 exit event, got %d", len(events))
		}
		if events[0].Type != EventExit {
			t.Errorf("expected type %q, got %q", EventExit, events[0].Type)
		}
		if events[0].PID != 2 {
			t.Errorf("expected PID 2, got %d", events[0].PID)
		}
	})

	t.Run("no events when stable", func(t *testing.T) {
		prev := map[types.PID]vfs.ProcInfo{
			1: {PID: 1, State: types.StateRunning, Intent: "init", CreatedAt: now},
		}
		curr := []vfs.ProcInfo{
			{PID: 1, State: types.StateRunning, Intent: "init", CreatedAt: now},
		}
		events := detectSpawnExitEvents(prev, curr)
		if len(events) != 0 {
			t.Errorf("expected 0 events, got %d", len(events))
		}
	})

	t.Run("empty previous", func(t *testing.T) {
		prev := map[types.PID]vfs.ProcInfo{}
		curr := []vfs.ProcInfo{
			{PID: 1, State: types.StateRunning, Intent: "init", CreatedAt: now},
		}
		events := detectSpawnExitEvents(prev, curr)
		if len(events) != 1 {
			t.Fatalf("expected 1 spawn event, got %d", len(events))
		}
	})
}

// --- Task 4.5: TestDetectBudgetEvents ---

func TestDetectBudgetEvents(t *testing.T) {
	t.Run("above 80 pct", func(t *testing.T) {
		procs := []vfs.ProcInfo{
			{PID: 1, TokensUsed: 85000, ContextBudget: 100000},
		}
		alertSeen := make(map[types.PID]int)
		events := detectBudgetEvents(procs, alertSeen)
		if len(events) != 1 {
			t.Fatalf("expected 1 budget event, got %d", len(events))
		}
		if events[0].Severity != SevWarn {
			t.Errorf("expected severity %d, got %d", SevWarn, events[0].Severity)
		}
	})

	t.Run("above 95 pct", func(t *testing.T) {
		procs := []vfs.ProcInfo{
			{PID: 1, TokensUsed: 96000, ContextBudget: 100000},
		}
		alertSeen := make(map[types.PID]int)
		events := detectBudgetEvents(procs, alertSeen)
		if len(events) != 1 {
			t.Fatalf("expected 1 budget event, got %d", len(events))
		}
		if events[0].Severity != SevError {
			t.Errorf("expected severity %d, got %d", SevError, events[0].Severity)
		}
	})

	t.Run("below threshold", func(t *testing.T) {
		procs := []vfs.ProcInfo{
			{PID: 1, TokensUsed: 50000, ContextBudget: 100000},
		}
		alertSeen := make(map[types.PID]int)
		events := detectBudgetEvents(procs, alertSeen)
		if len(events) != 0 {
			t.Errorf("expected 0 budget events, got %d", len(events))
		}
	})

	t.Run("no duplicate at same severity", func(t *testing.T) {
		procs := []vfs.ProcInfo{
			{PID: 1, TokensUsed: 85000, ContextBudget: 100000},
		}
		alertSeen := map[types.PID]int{1: SevWarn}
		events := detectBudgetEvents(procs, alertSeen)
		if len(events) != 0 {
			t.Errorf("expected 0 events (same severity), got %d", len(events))
		}
	})

	t.Run("escalation emits new event", func(t *testing.T) {
		procs := []vfs.ProcInfo{
			{PID: 1, TokensUsed: 96000, ContextBudget: 100000},
		}
		alertSeen := map[types.PID]int{1: SevWarn}
		events := detectBudgetEvents(procs, alertSeen)
		if len(events) != 1 {
			t.Fatalf("expected 1 escalation event, got %d", len(events))
		}
		if events[0].Severity != SevError {
			t.Errorf("expected severity %d, got %d", SevError, events[0].Severity)
		}
	})

	t.Run("zero budget ignored", func(t *testing.T) {
		procs := []vfs.ProcInfo{
			{PID: 1, TokensUsed: 5000, ContextBudget: 0},
		}
		alertSeen := make(map[types.PID]int)
		events := detectBudgetEvents(procs, alertSeen)
		if len(events) != 0 {
			t.Errorf("expected 0 events for zero budget, got %d", len(events))
		}
	})
}

// --- Task 4.6: TestDetectStallEvents ---

func TestDetectStallEvents(t *testing.T) {
	t.Run("detects stall", func(t *testing.T) {
		hb := &ipc.HeartbeatStatusResponse{
			Running: true,
			CurrentStalled: []ipc.StalledProcWire{
				{PID: 42, StalledDurationMs: 15000, ConsecutiveStalls: 3, LastAction: "tool_call"},
			},
		}
		stallSeen := make(map[types.PID]struct{})
		events := detectStallEvents(hb, stallSeen)
		if len(events) != 1 {
			t.Fatalf("expected 1 stall event, got %d", len(events))
		}
		if events[0].Type != EventStall {
			t.Errorf("expected type %q, got %q", EventStall, events[0].Type)
		}
		if events[0].Severity != SevError {
			t.Errorf("expected severity %d, got %d", SevError, events[0].Severity)
		}
		if events[0].PID != 42 {
			t.Errorf("expected PID 42, got %d", events[0].PID)
		}
	})

	t.Run("no duplicate stall", func(t *testing.T) {
		hb := &ipc.HeartbeatStatusResponse{
			Running: true,
			CurrentStalled: []ipc.StalledProcWire{
				{PID: 42, StalledDurationMs: 15000},
			},
		}
		stallSeen := map[types.PID]struct{}{42: {}}
		events := detectStallEvents(hb, stallSeen)
		if len(events) != 0 {
			t.Errorf("expected 0 events (already seen), got %d", len(events))
		}
	})

	t.Run("cleans up resolved stalls", func(t *testing.T) {
		hb := &ipc.HeartbeatStatusResponse{
			Running:        true,
			CurrentStalled: []ipc.StalledProcWire{},
		}
		stallSeen := map[types.PID]struct{}{42: {}}
		detectStallEvents(hb, stallSeen)
		if _, exists := stallSeen[42]; exists {
			t.Error("expected PID 42 to be removed from stallSeen")
		}
	})

	t.Run("nil heartbeat status", func(t *testing.T) {
		stallSeen := make(map[types.PID]struct{})
		events := detectStallEvents(nil, stallSeen)
		if len(events) != 0 {
			t.Errorf("expected 0 events for nil status, got %d", len(events))
		}
	})
}

// --- Task 4.7: TestMergeUnifiedEvents_Dedup ---

func TestMergeUnifiedEvents_Dedup(t *testing.T) {
	now := time.Now()
	seen := make(map[string]struct{})

	events := []UnifiedEvent{
		{Type: EventSpawn, PID: 1, Timestamp: now},
		{Type: EventSpawn, PID: 1, Timestamp: now}, // duplicate
		{Type: EventSpawn, PID: 2, Timestamp: now},  // different PID
	}

	result := sysEventDedup(events, seen)
	if len(result) != 2 {
		t.Errorf("expected 2 events after dedup, got %d", len(result))
	}
}

// --- Task 4.8: TestMergeUnifiedEvents_FIFOEviction ---

func TestMergeUnifiedEvents_FIFOEviction(t *testing.T) {
	now := time.Now()
	events := make([]UnifiedEvent, 250)
	for i := range events {
		events[i] = UnifiedEvent{
			Type:      EventCompact,
			PID:       types.PID(i),
			Timestamp: now.Add(time.Duration(i) * time.Second),
			Summary:   "event",
		}
	}

	result := sysEventFIFO(events)
	if len(result) != maxSysEvents {
		t.Errorf("expected %d events after FIFO, got %d", maxSysEvents, len(result))
	}
	// Oldest events should be dropped, newest kept
	if result[0].PID != types.PID(50) {
		t.Errorf("expected first event PID 50 (oldest kept), got %d", result[0].PID)
	}
	if result[len(result)-1].PID != types.PID(249) {
		t.Errorf("expected last event PID 249 (newest), got %d", result[len(result)-1].PID)
	}
}

func TestMergeUnifiedEvents_MergesAndSorts(t *testing.T) {
	now := time.Now()
	steps := []stepEntry{
		{summary: ipc.StepSummaryWire{Step: 1, Action: "tool_call", Summary: "read"}},
		{summary: ipc.StepSummaryWire{Step: 2, Action: "complete", Summary: "done"}},
	}
	sysEvts := []UnifiedEvent{
		{Type: EventSpawn, PID: 5, Timestamp: now.Add(-10 * time.Second), Summary: "spawn"},
	}

	merged := mergeUnifiedEvents(steps, sysEvts, 1)
	if len(merged) != 3 {
		t.Fatalf("expected 3 merged events, got %d", len(merged))
	}
	// Should be sorted by timestamp descending
	for i := 0; i < len(merged)-1; i++ {
		if merged[i].Timestamp.Before(merged[i+1].Timestamp) {
			t.Errorf("event %d timestamp before event %d — expected descending", i, i+1)
		}
	}
}

// --- Verify backward compatibility ---

func TestUnifiedEvents_BackwardCompat(t *testing.T) {
	// Verify newDashboardModel initializes unified event fields
	m := newDashboardModel(nil)
	if m.prevProcessPIDs == nil {
		t.Error("prevProcessPIDs should be initialized")
	}
	if m.budgetAlertSeen == nil {
		t.Error("budgetAlertSeen should be initialized")
	}
	if m.stallSeen == nil {
		t.Error("stallSeen should be initialized")
	}
	if m.sysEventSeen == nil {
		t.Error("sysEventSeen should be initialized")
	}
	// Step entries and timeline should be unaffected
	if m.stepEntries != nil {
		t.Error("stepEntries should be nil initially")
	}
}

func TestSysEventFIFO_UnderLimit(t *testing.T) {
	events := []UnifiedEvent{
		{Type: EventSpawn, Summary: "a"},
		{Type: EventExit, Summary: "b"},
	}
	result := sysEventFIFO(events)
	if len(result) != 2 {
		t.Errorf("expected 2 events (under limit), got %d", len(result))
	}
}
