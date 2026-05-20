// ATDD Story 43.3 - Timeline Renderer for Script Trace Events
//
// Red-phase tests for dashboardModel-level Script event handling:
//   - compactEventsMsg 处理路径扩展：5 类 Script* syscall 也写入 m.sysEvents
//   - lastScriptEventMs 单调 watermark 去重（与 lastCompactEventMs 同模式）
//   - UUID 注入（防 PID-reuse cross-contamination · Dev Notes #5）
//   - handlePIDChange 重置 lastScriptEventMs = 0
//   - eventTargetPane(EventScript) → (paneTimeline, true)
//
// Acceptance Criteria covered:
//   - AC#1: Timeline pane 拉取 script-runner 的 events.jsonl（receive 端）
//   - AC#4: EventScript routing 到 paneTimeline（cross-pane unread mark）
//   - AC#8: 性能保护（不重复处理已收到的事件 · watermark 去重）
//   - Dev Notes #5: UUID 注入防 PID-reuse 混淆
//
// RED 信号（dev-story 实施前 `go test -tags atdd_red ./cmd/rnix/...` 应失败）：
//   - undefined field: dashboardModel.lastScriptEventMs
//   - eventTargetPane(EventScript) returns (paneTimeline, false) — script case 不存在
//   - compactEventsMsg handler 不处理 Script* syscall → sysEvents 不含 EventScript
//   - handlePIDChange 不重置 lastScriptEventMs
//
// 实施完成后应能编译并通过；最后移除 build tag 让 `make all` 接管。

package main

import (
	"testing"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/ipc"
)

// =============================================================================
// eventTargetPane — AC#4 cross-pane routing
// =============================================================================

// TestEventTargetPane_ScriptRoutesToTimeline — AC#4 EventScript 路由到 Timeline pane
// （与 Step/Compact/Budget/Stall/Error/Syscall/Spawn/Exit 同 paneTimeline routing ·
// 让 Story 38-4 cross-pane unread 标记正常工作）。
func TestEventTargetPane_ScriptRoutesToTimeline(t *testing.T) {
	pane, ok := eventTargetPane(EventScript)
	if !ok {
		t.Error("eventTargetPane(EventScript) ok=false (want true so cross-pane unread fires)")
	}
	if pane != paneTimeline {
		t.Errorf("eventTargetPane(EventScript) pane = %v, want paneTimeline (%v)", pane, paneTimeline)
	}
}

// TestEventTargetPane_OtherEventsUnchanged — 添加 EventScript 不应破坏已有事件的 routing.
// 守门 spec § Decision Notes #2「不影响 EventCompact 等已有处理」。
func TestEventTargetPane_OtherEventsUnchanged(t *testing.T) {
	cases := []struct {
		eventType string
		wantPane  paneType
		wantOK    bool
	}{
		{EventStep, paneTimeline, true},
		{EventCompact, paneTimeline, true},
		{EventBudget, paneTimeline, true},
		{EventSpawn, paneTimeline, true},
		{EventExit, paneTimeline, true},
		{EventStall, paneTimeline, true},
		{EventSyscall, paneTimeline, true},
		{EventImmune, paneSecurity, true},
		{"unknown_type", paneTimeline, false}, // default branch
	}
	for _, tc := range cases {
		t.Run(tc.eventType, func(t *testing.T) {
			pane, ok := eventTargetPane(tc.eventType)
			if ok != tc.wantOK {
				t.Errorf("eventTargetPane(%q) ok = %v, want %v", tc.eventType, ok, tc.wantOK)
			}
			if pane != tc.wantPane {
				t.Errorf("eventTargetPane(%q) pane = %v, want %v", tc.eventType, pane, tc.wantPane)
			}
		})
	}
}

// =============================================================================
// dashboardModel.lastScriptEventMs — AC#1 watermark 去重
// =============================================================================

// TestDashboardModel_HasLastScriptEventMsField — AC#1 编译期保证 dashboardModel
// 含 lastScriptEventMs int64 字段（与 lastCompactEventMs 同模式 · spec Task 3.1）。
//
// 这个测试本身仅验证字段存在性（赋值即触发字段查找）；若字段不存在或类型不是 int64，
// 编译会失败 → RED 信号清晰。
func TestDashboardModel_HasLastScriptEventMsField(t *testing.T) {
	m := dashboardModel{}
	m.lastScriptEventMs = int64(0)
	if m.lastScriptEventMs != 0 {
		t.Errorf("default lastScriptEventMs = %d, want 0", m.lastScriptEventMs)
	}
	m.lastScriptEventMs = int64(1234567890)
	if m.lastScriptEventMs != 1234567890 {
		t.Errorf("assignment failed: got %d", m.lastScriptEventMs)
	}
}

// =============================================================================
// compactEventsMsg 处理路径 — AC#1, AC#4, Dev Notes #5（UUID 注入）
// =============================================================================

// TestCompactEventsMsg_ScriptEventsAppendedToSysEvents — AC#1 5 类 Script* syscall
// 同 batch 处理时被翻译为 EventScript 类型 UnifiedEvent 写入 m.sysEvents。
//
// 模拟：Selected PID=101 UUID="uuid-101"，msg 含 1 条 ScriptStmtBegin + 1 条 Compact。
// 期望：m.sysEvents 同时获得 EventScript 和 EventCompact 各 1 条。
func TestCompactEventsMsg_ScriptEventsAppendedToSysEvents(t *testing.T) {
	m := dashboardModel{
		selectedPID:     types.PID(101),
		selectedUUID:    "uuid-101",
		fetchingCompact: true,
		sysEventSeen:    map[string]struct{}{},
	}
	msg := compactEventsMsg{
		pid:  types.PID(101),
		uuid: "uuid-101",
		events: []ipc.SyscallEventWire{
			{
				TimestampMs: 100,
				PID:         types.PID(101),
				Syscall:     "ScriptStmtBegin",
				Args:        map[string]any{"line": 47, "stmt_kind": "spawn"},
			},
			{
				TimestampMs: 200,
				PID:         types.PID(101),
				Syscall:     "Compact",
				Args:        map[string]any{"pre_tokens": 10000, "post_tokens": 5000},
			},
		},
	}
	updated, _ := m.Update(msg)
	um, ok := updated.(dashboardModel)
	if !ok {
		t.Fatalf("Update returned non-dashboardModel: %T", updated)
	}

	var sawScript, sawCompact bool
	for _, ev := range um.sysEvents {
		if ev.Type == EventScript {
			sawScript = true
		}
		if ev.Type == EventCompact {
			sawCompact = true
		}
	}
	if !sawScript {
		t.Error("sysEvents missing EventScript after compactEventsMsg with ScriptStmtBegin")
	}
	if !sawCompact {
		t.Error("sysEvents missing EventCompact (regression: existing path broken)")
	}
}

// TestCompactEventsMsg_ScriptEventUUIDInjected — Dev Notes #5 CRITICAL:
// scriptEvent.UUID 必须从 msg.uuid 注入（防 PID-reuse cross-contamination）。
//
// 与 compactEventFromSyscall 当前行为不同（后者依赖 mergeUnifiedEvents fallback）。
func TestCompactEventsMsg_ScriptEventUUIDInjected(t *testing.T) {
	m := dashboardModel{
		selectedPID:     types.PID(101),
		selectedUUID:    "uuid-A",
		fetchingCompact: true,
		sysEventSeen:    map[string]struct{}{},
	}
	msg := compactEventsMsg{
		pid:  types.PID(101),
		uuid: "uuid-A",
		events: []ipc.SyscallEventWire{
			{
				TimestampMs: 100,
				PID:         types.PID(101),
				Syscall:     "ScriptSpawn",
				Args:        map[string]any{"line": 47, "intent": "build"},
			},
		},
	}
	updated, _ := m.Update(msg)
	um := updated.(dashboardModel)

	var found bool
	for _, ev := range um.sysEvents {
		if ev.Type != EventScript {
			continue
		}
		found = true
		if ev.UUID != "uuid-A" {
			t.Errorf("EventScript.UUID = %q, want \"uuid-A\" (msg.uuid injection · Dev Notes #5)", ev.UUID)
		}
	}
	if !found {
		t.Fatal("no EventScript found in sysEvents")
	}
}

// TestCompactEventsMsg_ScriptWatermarkDedup — AC#1 增量拉取必须用 lastScriptEventMs
// 单调 watermark 去重（与 lastCompactEventMs 同模式 · Task 3.2）。
//
// 模拟：先收到 ts=100 的 ScriptStmtBegin，再次收到 ts=100 的同事件不应重复添加。
func TestCompactEventsMsg_ScriptWatermarkDedup(t *testing.T) {
	m := dashboardModel{
		selectedPID:     types.PID(101),
		selectedUUID:    "uuid-A",
		fetchingCompact: true,
		sysEventSeen:    map[string]struct{}{},
	}
	event := ipc.SyscallEventWire{
		TimestampMs: 100,
		PID:         types.PID(101),
		Syscall:     "ScriptStmtBegin",
		Args:        map[string]any{"line": 1, "stmt_kind": "assign"},
	}

	// Tick 1: 收到事件
	m.fetchingCompact = true
	updated1, _ := m.Update(compactEventsMsg{pid: 101, uuid: "uuid-A", events: []ipc.SyscallEventWire{event}})
	m1 := updated1.(dashboardModel)
	countAfter1 := 0
	for _, ev := range m1.sysEvents {
		if ev.Type == EventScript {
			countAfter1++
		}
	}
	if countAfter1 != 1 {
		t.Fatalf("after tick 1: want 1 EventScript, got %d", countAfter1)
	}

	// Tick 2: 同事件再次到达（watermark 应阻挡）
	m1.fetchingCompact = true
	updated2, _ := m1.Update(compactEventsMsg{pid: 101, uuid: "uuid-A", events: []ipc.SyscallEventWire{event}})
	m2 := updated2.(dashboardModel)
	countAfter2 := 0
	for _, ev := range m2.sysEvents {
		if ev.Type == EventScript {
			countAfter2++
		}
	}
	if countAfter2 != 1 {
		t.Errorf("after tick 2 (duplicate ts=%d): want 1 EventScript (watermark dedup), got %d", event.TimestampMs, countAfter2)
	}
}

// TestCompactEventsMsg_ScriptWatermarkAdvances — AC#1 新事件 ts > watermark 时通过；
// lastScriptEventMs 应推进到最新 ts。
func TestCompactEventsMsg_ScriptWatermarkAdvances(t *testing.T) {
	m := dashboardModel{
		selectedPID:     types.PID(101),
		selectedUUID:    "uuid-A",
		fetchingCompact: true,
		sysEventSeen:    map[string]struct{}{},
	}
	events := []ipc.SyscallEventWire{
		{TimestampMs: 100, PID: 101, Syscall: "ScriptStmtBegin", Args: map[string]any{"line": 1, "stmt_kind": "assign"}},
		{TimestampMs: 200, PID: 101, Syscall: "ScriptStmtEnd", Args: map[string]any{"line": 1, "stmt_kind": "assign"}},
		{TimestampMs: 300, PID: 101, Syscall: "ScriptSpawn", Args: map[string]any{"line": 2, "intent": "x"}},
	}
	updated, _ := m.Update(compactEventsMsg{pid: 101, uuid: "uuid-A", events: events})
	um := updated.(dashboardModel)
	if um.lastScriptEventMs != 300 {
		t.Errorf("lastScriptEventMs = %d, want 300 (max of batch)", um.lastScriptEventMs)
	}
	count := 0
	for _, ev := range um.sysEvents {
		if ev.Type == EventScript {
			count++
		}
	}
	if count != 3 {
		t.Errorf("want 3 EventScript in sysEvents, got %d", count)
	}
}

// TestCompactEventsMsg_UnknownScriptSyscallIgnored — AC#2 helper bool=false 让
// caller 跳过；不在 5 类内的 syscall（如 "ScriptUnknown"）不应进入 sysEvents.
func TestCompactEventsMsg_UnknownScriptSyscallIgnored(t *testing.T) {
	m := dashboardModel{
		selectedPID:     types.PID(101),
		selectedUUID:    "uuid-A",
		fetchingCompact: true,
		sysEventSeen:    map[string]struct{}{},
	}
	msg := compactEventsMsg{
		pid:  101,
		uuid: "uuid-A",
		events: []ipc.SyscallEventWire{
			{TimestampMs: 100, PID: 101, Syscall: "ScriptUnknown", Args: map[string]any{}},
			{TimestampMs: 200, PID: 101, Syscall: "ScriptStmtBegin", Args: map[string]any{"line": 1, "stmt_kind": "x"}},
		},
	}
	updated, _ := m.Update(msg)
	um := updated.(dashboardModel)
	count := 0
	for _, ev := range um.sysEvents {
		if ev.Type == EventScript {
			count++
		}
	}
	if count != 1 {
		t.Errorf("want 1 EventScript (only ScriptStmtBegin counts · ScriptUnknown should be dropped), got %d", count)
	}
}

// TestCompactEventsMsg_NormalProcessUnaffected — AC#1 关键回归断言：
// 普通 reasoning 进程（无 Script* syscall）的 Timeline 数据流完全不变。
// 即：msg 全部是非 Script 事件时，sysEvents 不应出现任何 EventScript。
func TestCompactEventsMsg_NormalProcessUnaffected(t *testing.T) {
	m := dashboardModel{
		selectedPID:     types.PID(50),
		selectedUUID:    "uuid-50",
		fetchingCompact: true,
		sysEventSeen:    map[string]struct{}{},
	}
	msg := compactEventsMsg{
		pid:  50,
		uuid: "uuid-50",
		events: []ipc.SyscallEventWire{
			{TimestampMs: 100, PID: 50, Syscall: "Compact", Args: map[string]any{"pre_tokens": 1, "post_tokens": 1}},
			{TimestampMs: 200, PID: 50, Syscall: "Read", Args: map[string]any{"path": "/x"}},
		},
	}
	updated, _ := m.Update(msg)
	um := updated.(dashboardModel)
	for _, ev := range um.sysEvents {
		if ev.Type == EventScript {
			t.Errorf("unexpected EventScript on normal-process tick (no Script* in batch): %+v", ev)
		}
	}
}

// =============================================================================
// handlePIDChange — AC#1 watermark 重置
// =============================================================================

// TestHandlePIDChange_ResetsLastScriptEventMs — AC#1 PID 切换时 lastScriptEventMs
// 必须重置为 0（与 lastCompactEventMs 同模式 · spec Task 3.3）。
func TestHandlePIDChange_ResetsLastScriptEventMs(t *testing.T) {
	m := dashboardModel{
		selectedPID:        types.PID(101),
		selectedUUID:       "uuid-101",
		lastScriptEventMs:  9999, // 模拟之前进程的 watermark
		lastCompactEventMs: 9999,
	}
	updated, _ := m.handlePIDChange()
	if updated.lastScriptEventMs != 0 {
		t.Errorf("after handlePIDChange: lastScriptEventMs = %d, want 0", updated.lastScriptEventMs)
	}
	// 验证 lastCompactEventMs 也被重置（不引入回归）
	if updated.lastCompactEventMs != 0 {
		t.Errorf("regression: lastCompactEventMs = %d, want 0", updated.lastCompactEventMs)
	}
}

// TestHandlePIDChange_ToZeroAlsoResets — handlePIDChange 在 PID→0 路径下也应重置.
func TestHandlePIDChange_ToZeroAlsoResets(t *testing.T) {
	m := dashboardModel{
		selectedPID:       0,
		lastScriptEventMs: 9999,
	}
	updated, _ := m.handlePIDChange()
	if updated.lastScriptEventMs != 0 {
		t.Errorf("after PID→0 handlePIDChange: lastScriptEventMs = %d, want 0", updated.lastScriptEventMs)
	}
}
