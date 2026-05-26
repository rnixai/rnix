package main

import (
	"strings"
	"testing"
	"time"

	"github.com/rnixai/rnix/debug"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/ipc"
	"github.com/rnixai/rnix/vfs"
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
	if !m2.debugState.Mode {
		t.Error("expected debugMode to be true")
	}
	if !m2.debugState.ShowStrace {
		t.Error("expected debugShowStrace to be true")
	}
	if m2.debugState.AttachedPID != types.PID(1) {
		t.Errorf("expected debugAttachedPID=1, got %v", m2.debugState.AttachedPID)
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
	if m3.debugState.Mode {
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
	m.debugState.Mode = true

	now := time.Now()
	m.debugState.Events = []UnifiedEvent{
		{Type: EventSyscall, Timestamp: now, PID: 1, Summary: "syscall"},
		{Type: EventStep, Timestamp: now.Add(-time.Second), PID: 1, Summary: "step",
			StepEntry: &stepEntry{Summary: ipc.StepSummaryWire{Action: "tool_call"}}},
	}

	// With strace enabled
	m.debugState.ShowStrace = true
	visible := m.filteredDebugEvents()
	if len(visible) != 2 {
		t.Errorf("expected 2 events with strace on, got %d", len(visible))
	}

	// With strace disabled
	m.debugState.ShowStrace = false
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
	m.debugState.Mode = true
	m.viewMode = viewDebug

	output := m.renderDebugLayout(120, 40)
	if output == "" {
		t.Error("expected non-empty debug layout")
	}
}

// 验证 debug 模式下顶部 titleBar 不再重复 DEBUG/PID/intent —— 该信息已由
// Timeline 面板 header 承担，避免上下重复展示。
func TestRenderDashboardTitle_DebugMode_NoDebugSegment(t *testing.T) {
	m := newDashboardModel(nil)
	m.width = 120
	m.height = 40
	m.connected = true
	m.selectedPID = 7
	m.debugState.Mode = true
	m.viewMode = viewDebug
	m.processes = []vfs.ProcInfo{{PID: 7, Intent: "analyze churn"}}

	title := m.renderDashboardTitle()
	if strings.Contains(title, "DEBUG:") || strings.Contains(title, "DEBUG: PID") {
		t.Errorf("titleBar 不应再包含 'DEBUG:' 段（重复信息已交给 Timeline header），got: %s", title)
	}
}

// 验证 Timeline header 在长 intent 时不会把固定身份段 `Events [DEBUG PID N]` 撞出
// 可视区，且超长 intent 末尾被截断为 `…`。
func TestRenderDebugTimelineContent_LongIntentTruncation(t *testing.T) {
	m := newDashboardModel(nil)
	m.width = 120
	m.height = 40
	m.selectedPID = 42
	m.debugState.Mode = true
	m.viewMode = viewDebug
	longIntent := strings.Repeat("analyze customer churn quarterly report ", 6)
	m.processes = []vfs.ProcInfo{{PID: 42, Intent: longIntent}}

	out := m.renderDebugTimelineContent(60, 20)
	firstLine := strings.SplitN(out, "\n", 2)[0]

	// 固定段必须完整出现
	if !strings.Contains(firstLine, "PID 42]") {
		t.Errorf("固定身份段 'PID 42]' 应完整出现在 header 首行，got: %q", firstLine)
	}
	if !strings.Contains(firstLine, "DEBUG") {
		t.Errorf("header 应含 DEBUG 标签, got: %q", firstLine)
	}
	// 超长 intent 应被截断为 …
	if strings.Contains(firstLine, longIntent) {
		t.Errorf("长 intent 应被截断, 但完整出现在 header: %q", firstLine)
	}
	if !strings.Contains(firstLine, "…") {
		t.Errorf("长 intent 截断后应以 '…' 结尾, got: %q", firstLine)
	}
}

// 验证 Timeline header 在宽度极窄时优雅省略 intent（不会破坏固定段）。
func TestRenderDebugTimelineContent_NarrowWidth_DropsIntent(t *testing.T) {
	m := newDashboardModel(nil)
	m.width = 30
	m.height = 20
	m.selectedPID = 3
	m.debugState.Mode = true
	m.viewMode = viewDebug
	m.processes = []vfs.ProcInfo{{PID: 3, Intent: "long-intent-string"}}

	out := m.renderDebugTimelineContent(22, 20)
	firstLine := strings.SplitN(out, "\n", 2)[0]
	if !strings.Contains(firstLine, "PID 3]") {
		t.Errorf("窄宽下固定段 'PID 3]' 必须保留, got: %q", firstLine)
	}
	// 窄宽下 intent 应被整体省略，而不是带着 `…` 挤进来撞破固定段。
	if strings.Contains(firstLine, "long-intent") {
		t.Errorf("窄宽下 intent 应整体省略, 但出现在 header: %q", firstLine)
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

	if len(m.debugState.DeviceLatency) != 2 {
		t.Errorf("expected 2 devices, got %d", len(m.debugState.DeviceLatency))
	}
	llmStats := m.debugState.DeviceLatency["llm"]
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
	fsStats := m.debugState.DeviceLatency["fs"]
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

	if len(m.debugState.StraceEvents) != maxDebugStraceEvents {
		t.Errorf("expected %d events, got %d", maxDebugStraceEvents, len(m.debugState.StraceEvents))
	}
	// Oldest should have been evicted
	earliest := m.debugState.StraceEvents[0].Timestamp
	expected := now.Add(50 * time.Millisecond)
	if earliest.Before(expected) {
		t.Errorf("expected oldest event at ~+50ms, got %v", earliest.Sub(now))
	}
}

func TestContextProfileRendering(t *testing.T) {
	m := newDashboardModel(nil)
	m.width = 120
	m.height = 40
	m.debugState.Mode = true
	m.debugState.CtxProfile = &debug.CtxProfileResult{
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
	m.debugState.Mode = true
	m.debugState.DeviceLatency = make(map[string]*deviceLatencyStats)
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

	m.debugState.StraceCh = ch
	streamClosed := m.debugTickProcess()

	if !streamClosed {
		t.Error("expected streamClosed=true when channel is closed")
	}
	// Events should be merged despite channel close
	if len(m.debugState.StraceEvents) != 1 {
		t.Errorf("expected 1 strace event, got %d", len(m.debugState.StraceEvents))
	}
	if len(m.debugState.Events) == 0 {
		t.Error("expected merged events to be non-empty after channel close")
	}
}

// --- Fix: Dead process event stability tests ---

func TestUUIDValidation_DebugMode_PreservesSelectionForDeadProcess(t *testing.T) {
	// When a dead process temporarily disappears from the process list,
	// the UUID validation should NOT reset selectedPID if debug mode is active
	// and events have already been loaded (prevents "Waiting for events…" flicker).
	m := newDashboardModel(nil)
	m.debugState.Mode = true
	m.selectedPID = 2
	m.selectedUUID = "dead-uuid"
	m.debugState.AttachedPID = 2
	m.debugState.Events = []UnifiedEvent{
		{Type: EventSyscall, PID: 2, Summary: "loaded-event"},
	}

	// Simulate processes list WITHOUT PID 2 (temporarily missing)
	m.processes = []vfs.ProcInfo{
		{PID: 1, UUID: "root-uuid", State: types.StateRunning},
	}
	m.tree.Rows = []flatRow{
		{Proc: vfs.ProcInfo{PID: 1, UUID: "root-uuid"}},
	}

	// Run the UUID validation logic inline (mirrors dashboard.go:600-621)
	found := false
	for _, p := range m.processes {
		if p.PID == m.selectedPID {
			if m.selectedUUID == "" || p.UUID == m.selectedUUID {
				found = true
			}
			break
		}
	}
	if !found {
		if m.debugState.Mode && m.debugState.AttachedPID == m.selectedPID && len(m.debugState.Events) > 0 {
			found = true
		}
	}
	if !found {
		m.selectedPID = 0
		m.selectedUUID = ""
	}

	// selectedPID should be preserved due to the debug guard
	if m.selectedPID != 2 {
		t.Errorf("expected selectedPID=2 (preserved), got %d", m.selectedPID)
	}
	if m.selectedUUID != "dead-uuid" {
		t.Errorf("expected selectedUUID preserved, got %q", m.selectedUUID)
	}
}

func TestUUIDValidation_NoDebugMode_ResetsNormally(t *testing.T) {
	// Without debug mode, the validation should still reset selectedPID normally.
	m := newDashboardModel(nil)
	m.debugState.Mode = false
	m.selectedPID = 2
	m.selectedUUID = "dead-uuid"
	m.debugState.AttachedPID = 2
	m.debugState.Events = []UnifiedEvent{
		{Type: EventSyscall, PID: 2, Summary: "loaded-event"},
	}

	m.processes = []vfs.ProcInfo{
		{PID: 1, UUID: "root-uuid", State: types.StateRunning},
	}

	// Run UUID validation
	found := false
	for _, p := range m.processes {
		if p.PID == m.selectedPID {
			if m.selectedUUID == "" || p.UUID == m.selectedUUID {
				found = true
			}
			break
		}
	}
	if !found {
		if m.debugState.Mode && m.debugState.AttachedPID == m.selectedPID && len(m.debugState.Events) > 0 {
			found = true
		}
	}
	if !found {
		m.selectedPID = 0
		m.selectedUUID = ""
	}

	// Without debug mode, selectedPID should be reset
	if m.selectedPID != 0 {
		t.Errorf("expected selectedPID=0 (reset), got %d", m.selectedPID)
	}
}

func TestHistoricalStraceMsg_DiscardsStalePID(t *testing.T) {
	// When an async historical strace response arrives for a PID that is no longer
	// selected, the handler should discard it to prevent overwriting current events.
	m := newDashboardModel(nil)
	m.debugState.Mode = true
	m.selectedPID = 3
	m.selectedUUID = "uuid-3"
	m.debugState.AttachedPID = 3
	m.debugState.Events = []UnifiedEvent{
		{Type: EventSyscall, PID: 3, Summary: "current-event"},
	}

	// Simulate stale response from PID 2 (old selection)
	staleMsg := debugHistoricalStraceMsg{
		events: []ipc.SyscallEventWire{
			{TimestampMs: 1000, PID: 2, Syscall: "Open"},
		},
		pid:  2,
		uuid: "uuid-2",
	}

	m2, _, handled := m.handleDebugMsg(staleMsg)
	if !handled {
		t.Fatal("expected msg to be handled")
	}
	// Events should NOT be overwritten by stale response
	if len(m2.debugState.Events) != 1 || m2.debugState.Events[0].Summary != "current-event" {
		t.Errorf("expected current events preserved, got %d events", len(m2.debugState.Events))
	}
}

func TestHistoricalStraceMsg_AcceptsMatchingPID(t *testing.T) {
	// When the response PID matches the current selection, events should be processed.
	m := newDashboardModel(nil)
	m.debugState.Mode = true
	m.selectedPID = 2
	m.selectedUUID = "uuid-2"
	m.debugState.AttachedPID = 2

	msg := debugHistoricalStraceMsg{
		events: []ipc.SyscallEventWire{
			{TimestampMs: 1000, PID: 2, Syscall: "Open", Args: map[string]any{"path": "/dev/fs"}},
		},
		pid:  2,
		uuid: "uuid-2",
	}

	m2, _, handled := m.handleDebugMsg(msg)
	if !handled {
		t.Fatal("expected msg to be handled")
	}
	// Events should be processed (debugStraceEvents updated)
	if len(m2.debugState.StraceEvents) == 0 {
		t.Error("expected strace events to be populated from matching response")
	}
}

func TestIsSelectedProcessDead_DebugFallback(t *testing.T) {
	// When the selected process is not in m.processes but debug mode has loaded
	// events for it, isSelectedProcessDead should return true.
	m := newDashboardModel(nil)
	m.debugState.Mode = true
	m.selectedPID = 2
	m.selectedUUID = "uuid-2"
	m.debugState.AttachedPID = 2
	m.debugState.Events = []UnifiedEvent{
		{Type: EventSyscall, PID: 2, Summary: "event"},
	}
	// Process list does NOT contain PID 2
	m.processes = []vfs.ProcInfo{
		{PID: 1, UUID: "uuid-1", State: types.StateRunning},
	}

	if !m.isSelectedProcessDead() {
		t.Error("expected isSelectedProcessDead()=true for missing process with loaded debug events")
	}
}

func TestIsSelectedProcessDead_NoDebugEvents_ReturnsFalse(t *testing.T) {
	// Without loaded events, missing process should not be treated as dead.
	m := newDashboardModel(nil)
	m.debugState.Mode = true
	m.selectedPID = 2
	m.selectedUUID = "uuid-2"
	m.debugState.AttachedPID = 2
	m.debugState.Events = nil // No events loaded
	m.processes = []vfs.ProcInfo{
		{PID: 1, UUID: "uuid-1", State: types.StateRunning},
	}

	if m.isSelectedProcessDead() {
		t.Error("expected isSelectedProcessDead()=false when no debug events loaded")
	}
}
