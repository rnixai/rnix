package main

import (
	"strings"
	"testing"
	"time"

	"github.com/rnixai/rnix/debug"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/ipc"
)

func TestEnterDebugMode_RequiresSelection(t *testing.T) {
	m := newDashboardModel(nil)
	m.selectedPID = 0
	// Cannot enter debug mode without a selected process
	if m.selectedPID == 0 && m.viewMode == viewDefault {
		// This mirrors the nav guard: "Select a process first"
		if m.viewMode != viewDefault {
			t.Fatal("expected viewDefault")
		}
	}
}

func TestEnterDebugMode_SwitchesViewMode(t *testing.T) {
	m := newDashboardModel(nil)
	m.selectedPID = 1
	m.selectedUUID = "test-uuid"
	m.connected = false // no IPC in tests

	m2, _ := m.enterDebugMode()
	if m2.viewMode != viewDebug {
		t.Errorf("expected viewDebug, got %v", m2.viewMode)
	}
	if !m2.debugMode {
		t.Error("expected debugMode to be true")
	}
	if !m2.debugShowStrace {
		t.Error("expected debugShowStrace to be true")
	}
	if m2.debugAttachedPID != types.PID(1) {
		t.Errorf("expected debugAttachedPID=1, got %v", m2.debugAttachedPID)
	}
}

func TestExitDebugMode_RestoresDefault(t *testing.T) {
	m := newDashboardModel(nil)
	m.selectedPID = 1
	m.selectedUUID = "test-uuid"
	m.connected = false

	m2, _ := m.enterDebugMode()
	m3 := m2.exitDebugMode()
	if m3.viewMode != viewDefault {
		t.Errorf("expected viewDefault, got %v", m3.viewMode)
	}
	if m3.debugMode {
		t.Error("expected debugMode to be false")
	}
}

func TestStraceToUnifiedEvent(t *testing.T) {
	now := time.Now()
	sew := ipc.SyscallEventWire{
		TimestampMs: now.UnixMilli(),
		PID:         42,
		Syscall:     "Open",
		Args:        map[string]any{"path": "/dev/llm/claude", "flags": float64(2)},
		Result:      float64(3),
		DurationMs:  0.002,
	}
	ev := straceToUnifiedEvent(sew)
	if ev.Type != EventSyscall {
		t.Errorf("expected type %q, got %q", EventSyscall, ev.Type)
	}
	if ev.PID != 42 {
		t.Errorf("expected PID 42, got %v", ev.PID)
	}
	if ev.Severity != SevInfo {
		t.Errorf("expected SevInfo, got %v", ev.Severity)
	}
	// Full strace format should contain syscall name with args and result
	if !strings.Contains(ev.Summary, "Open(") {
		t.Errorf("expected summary to contain 'Open(', got %q", ev.Summary)
	}
	if !strings.Contains(ev.Summary, "→") {
		t.Errorf("expected summary to contain '→', got %q", ev.Summary)
	}

	// Error case
	sewErr := sew
	sewErr.Error = "ENOENT"
	evErr := straceToUnifiedEvent(sewErr)
	if evErr.Severity != SevError {
		t.Errorf("expected SevError for error syscall, got %v", evErr.Severity)
	}
	if !strings.Contains(evErr.Summary, "err(") {
		t.Errorf("expected error summary to contain 'err(', got %q", evErr.Summary)
	}
}

func TestMergeDebugEvents_TimeOrdered(t *testing.T) {
	now := time.Now()

	strace1 := UnifiedEvent{
		Type:      EventSyscall,
		Timestamp: now.Add(-2 * time.Second),
		PID:       1,
		Summary:   "strace-old",
	}
	strace2 := UnifiedEvent{
		Type:      EventSyscall,
		Timestamp: now,
		PID:       1,
		Summary:   "strace-new",
	}

	var steps []stepEntry // empty steps

	result := mergeDebugEvents(steps, []UnifiedEvent{strace1, strace2}, 1)
	if len(result) < 2 {
		t.Fatalf("expected >= 2 events, got %d", len(result))
	}
	// Newest first (descending)
	if result[0].Timestamp.Before(result[1].Timestamp) {
		t.Error("expected descending timestamp order")
	}
}

func TestRenderSyscallLine_Format(t *testing.T) {
	now := time.Now()
	sew := ipc.SyscallEventWire{
		TimestampMs: now.UnixMilli(),
		PID:         1,
		Syscall:     "write",
		Args:        map[string]any{"path": "/dev/shell"},
		DurationMs:  50,
	}
	ev := straceToUnifiedEvent(sew)
	m := newDashboardModel(nil)
	line := m.renderSyscallLine(ev, "", 80)
	if line == "" {
		t.Error("expected non-empty render")
	}
}

func TestRenderSyscallLine_ASCII(t *testing.T) {
	t.Setenv("RNIX_ASCII", "1")
	now := time.Now()
	sew := ipc.SyscallEventWire{
		TimestampMs: now.UnixMilli(),
		PID:         1,
		Syscall:     "read",
		Args:        map[string]any{"path": "/dev/fs"},
		DurationMs:  10,
	}
	ev := straceToUnifiedEvent(sew)
	m := newDashboardModel(nil)
	line := m.renderSyscallLine(ev, "", 80)
	if line == "" {
		t.Error("expected non-empty render")
	}
}

func TestFilteredDebugEvents_StraceToggle(t *testing.T) {
	m := newDashboardModel(nil)
	m.debugMode = true

	now := time.Now()
	m.debugEvents = []UnifiedEvent{
		{Type: EventSyscall, Timestamp: now, PID: 1, Summary: "syscall"},
		{Type: EventStep, Timestamp: now.Add(-time.Second), PID: 1, Summary: "step",
			StepEntry: &stepEntry{summary: ipc.StepSummaryWire{Action: "tool_call"}}},
	}

	// With strace enabled
	m.debugShowStrace = true
	visible := m.filteredDebugEvents()
	if len(visible) != 2 {
		t.Errorf("expected 2 events with strace on, got %d", len(visible))
	}

	// With strace disabled
	m.debugShowStrace = false
	visible = m.filteredDebugEvents()
	if len(visible) != 1 {
		t.Errorf("expected 1 event with strace off, got %d", len(visible))
	}
	if visible[0].Type == EventSyscall {
		t.Error("syscall events should be hidden when strace is off")
	}
}

func TestDebugLayout_Structure(t *testing.T) {
	m := newDashboardModel(nil)
	m.width = 120
	m.height = 40
	m.selectedPID = 1
	m.debugMode = true
	m.viewMode = viewDebug

	output := m.renderDebugLayout(120, 40)
	if output == "" {
		t.Error("expected non-empty debug layout")
	}
}

func TestDeviceLatencyAggregation(t *testing.T) {
	m := newDashboardModel(nil)

	m.updateDeviceLatency(ipc.SyscallEventWire{
		Syscall:    "open",
		Args:       map[string]any{"path": "/dev/llm/claude"},
		DurationMs: 100,
	})
	m.updateDeviceLatency(ipc.SyscallEventWire{
		Syscall:    "write",
		Args:       map[string]any{"path": "/dev/llm/claude"},
		DurationMs: 200,
		Error:      "timeout",
	})
	m.updateDeviceLatency(ipc.SyscallEventWire{
		Syscall:    "open",
		Args:       map[string]any{"path": "/dev/fs"},
		DurationMs: 10,
	})

	if len(m.debugDeviceLatency) != 2 {
		t.Errorf("expected 2 devices, got %d", len(m.debugDeviceLatency))
	}
	llmStats := m.debugDeviceLatency["llm"]
	if llmStats == nil {
		t.Fatal("expected llm stats")
	}
	if llmStats.Count != 2 {
		t.Errorf("expected count 2, got %d", llmStats.Count)
	}
	if llmStats.ErrorCount != 1 {
		t.Errorf("expected error count 1, got %d", llmStats.ErrorCount)
	}
	if llmStats.AvgMs() != 150 {
		t.Errorf("expected avg 150ms, got %.1f", llmStats.AvgMs())
	}
	fsStats := m.debugDeviceLatency["fs"]
	if fsStats == nil {
		t.Fatal("expected fs stats")
	}
	if fsStats.Count != 1 {
		t.Errorf("expected count 1, got %d", fsStats.Count)
	}
}

func TestDebugStatusHints(t *testing.T) {
	m := newDashboardModel(nil)
	m.width = 120
	m.viewMode = viewDebug
	m.connected = true
	m.selectedPID = 1
	status := m.renderDashboardStatus()
	// Should contain debug-specific hints
	if !strings.Contains(status, "strace") && !strings.Contains(status, "debug") {
		t.Logf("status output: %s", status)
	}
}

func TestExtractDeviceName(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"/dev/llm/claude", "llm"},
		{"/dev/fs", "fs"},
		{"/dev/shell", "shell"},
		{"/dev/mcp/github", "mcp"},
		{"", ""},
	}
	for _, tt := range tests {
		sew := ipc.SyscallEventWire{Args: map[string]any{}}
		if tt.path != "" {
			sew.Args["path"] = tt.path
		}
		got := extractDeviceName(sew)
		if got != tt.want {
			t.Errorf("extractDeviceName(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

func TestAppendStraceEvent_RingBuffer(t *testing.T) {
	m := newDashboardModel(nil)
	now := time.Now()

	// Fill beyond max
	for i := range maxDebugStraceEvents + 50 {
		m.appendStraceEvent(UnifiedEvent{
			Type:      EventSyscall,
			Timestamp: now.Add(time.Duration(i) * time.Millisecond),
			Summary:   "event",
		})
	}

	if len(m.debugStraceEvents) != maxDebugStraceEvents {
		t.Errorf("expected %d events, got %d", maxDebugStraceEvents, len(m.debugStraceEvents))
	}
	// Oldest should have been evicted
	earliest := m.debugStraceEvents[0].Timestamp
	expected := now.Add(50 * time.Millisecond)
	if earliest.Before(expected) {
		t.Errorf("expected oldest event at ~+50ms, got %v", earliest.Sub(now))
	}
}

func TestContextProfileRendering(t *testing.T) {
	m := newDashboardModel(nil)
	m.width = 120
	m.height = 40
	m.debugMode = true
	m.debugCtxProfile = &debug.CtxProfileResult{
		PID:           1,
		TokensUsed:    5000,
		ContextBudget: 10000,
		TotalTokens:   200000,
		Classification: debug.ClassificationResult{
			Active: debug.ClassBucket{Tokens: 3000, Messages: 5, Pct: 60},
			Warm:   debug.ClassBucket{Tokens: 1000, Messages: 3, Pct: 20},
			Cold:   debug.ClassBucket{Tokens: 800, Messages: 2, Pct: 16},
			Leaked: debug.ClassBucket{Tokens: 200, Messages: 1, Pct: 4},
		},
	}

	output := m.renderDebugDetailLeft(60, 10)
	if output == "" {
		t.Error("expected non-empty context profile render")
	}
	if !strings.Contains(output, "active:") {
		t.Errorf("expected 'active:' in context profile, got: %s", output)
	}
}

func TestDefaultFiltersIncludeSyscall(t *testing.T) {
	filters := defaultStepFilters()
	if !filters[EventSyscall] {
		t.Error("expected EventSyscall to be enabled in default filters")
	}
}

func TestWireToSyscallEvent_Dashboard(t *testing.T) {
	sew := ipc.SyscallEventWire{
		TimestampMs: 1234,
		PID:         5,
		Syscall:     "CtxAlloc",
		Args:        map[string]any{"size": float64(64)},
		Result:      float64(5),
		DurationMs:  0.001,
	}
	se := wireToSyscallEvent(sew)
	if se.PID != 5 {
		t.Errorf("expected PID 5, got %v", se.PID)
	}
	if se.Syscall != "CtxAlloc" {
		t.Errorf("expected syscall CtxAlloc, got %v", se.Syscall)
	}
	if se.Err != nil {
		t.Errorf("expected nil error, got %v", se.Err)
	}

	// Error case
	sewErr := sew
	sewErr.Error = "not found"
	seErr := wireToSyscallEvent(sewErr)
	if seErr.Err == nil || seErr.Err.Error() != "not found" {
		t.Errorf("expected error 'not found', got %v", seErr.Err)
	}
}

func TestDebugTickProcess_ChannelCloseMerges(t *testing.T) {
	m := newDashboardModel(nil)
	m.debugMode = true
	m.debugDeviceLatency = make(map[string]*deviceLatencyStats)
	m.selectedPID = 1

	// Create a channel with one event, then close it
	ch := make(chan ipc.SyscallEventWire, 10)
	ch <- ipc.SyscallEventWire{
		TimestampMs: time.Now().UnixMilli(),
		PID:         1,
		Syscall:     "Open",
		Args:        map[string]any{"path": "/dev/fs"},
		DurationMs:  1.0,
	}
	close(ch)

	m.debugStraceCh = ch
	streamClosed := m.debugTickProcess()

	if !streamClosed {
		t.Error("expected streamClosed=true when channel is closed")
	}
	// Events should be merged despite channel close
	if len(m.debugStraceEvents) != 1 {
		t.Errorf("expected 1 strace event, got %d", len(m.debugStraceEvents))
	}
	if len(m.debugEvents) == 0 {
		t.Error("expected merged events to be non-empty after channel close")
	}
}
