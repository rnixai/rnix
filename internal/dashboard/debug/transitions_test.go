// Package debug — transitions_test.go (Story 38-5 PR11 Step 4(b) Phase 2)
//
// HandlePIDChange 行为契约测试（与 cmd/rnix.handleDebugPIDChange line 397-405
// byte-for-byte 等价 · Story 34.6 strace fusion 行为契约保留）。
package debug

import (
	"testing"
	"time"

	"github.com/rnixai/rnix/debug"
	"github.com/rnixai/rnix/internal/dashboard/event"
	"github.com/rnixai/rnix/internal/types"
)

// TestHandlePIDChange_StraceEventsCleared 验证 strace ring buffer 清空。
func TestHandlePIDChange_StraceEventsCleared(t *testing.T) {
	state := DebugState{
		StraceEvents: []event.UnifiedEvent{
			{Type: event.EventSyscall, Summary: "old1"},
			{Type: event.EventSyscall, Summary: "old2"},
		},
	}

	out := HandlePIDChange(state, types.PID(42))

	if out.StraceEvents != nil {
		t.Errorf("StraceEvents = %v, want nil (cleared)", out.StraceEvents)
	}
}

// TestHandlePIDChange_EventsCleared 验证 merged debug timeline events 清空。
func TestHandlePIDChange_EventsCleared(t *testing.T) {
	state := DebugState{
		Events: []event.UnifiedEvent{
			{Type: event.EventStep, Summary: "step1"},
			{Type: event.EventSyscall, Summary: "syscall1"},
		},
	}

	out := HandlePIDChange(state, types.PID(42))

	if out.Events != nil {
		t.Errorf("Events = %v, want nil (cleared)", out.Events)
	}
}

// TestHandlePIDChange_DeviceLatencyFreshMap 验证 DeviceLatency 是 fresh empty map（不复用旧 map）。
func TestHandlePIDChange_DeviceLatencyFreshMap(t *testing.T) {
	oldMap := map[string]*DeviceLatencyStats{
		"llm": {Count: 10, TotalMs: 100, ErrorCount: 1},
	}
	state := DebugState{
		DeviceLatency: oldMap,
	}

	out := HandlePIDChange(state, types.PID(42))

	if out.DeviceLatency == nil {
		t.Fatal("DeviceLatency = nil, want fresh empty map")
	}
	if len(out.DeviceLatency) != 0 {
		t.Errorf("len(DeviceLatency) = %d, want 0 (fresh empty)", len(out.DeviceLatency))
	}
	// 验证不是同一个 map（fresh allocation · 防止外部引用污染）
	out.DeviceLatency["test"] = &DeviceLatencyStats{Count: 1}
	if _, ok := oldMap["test"]; ok {
		t.Error("modifying out.DeviceLatency affected oldMap (not fresh)")
	}
}

// TestHandlePIDChange_DeviceLatencyFromNil 验证从 nil DeviceLatency 也能正确创建 fresh map。
func TestHandlePIDChange_DeviceLatencyFromNil(t *testing.T) {
	state := DebugState{DeviceLatency: nil}

	out := HandlePIDChange(state, types.PID(42))

	if out.DeviceLatency == nil {
		t.Error("DeviceLatency = nil, want fresh map (even from nil input)")
	}
	if len(out.DeviceLatency) != 0 {
		t.Errorf("len(DeviceLatency) = %d, want 0", len(out.DeviceLatency))
	}
}

// TestHandlePIDChange_CtxProfileCleared 验证 Context Profile 清空。
func TestHandlePIDChange_CtxProfileCleared(t *testing.T) {
	state := DebugState{
		CtxProfile: &debug.CtxProfileResult{
			TotalTokens: 12345,
		},
	}

	out := HandlePIDChange(state, types.PID(42))

	if out.CtxProfile != nil {
		t.Errorf("CtxProfile = %v, want nil (cleared)", out.CtxProfile)
	}
}

// TestHandlePIDChange_VisualStateReset 验证 ScrollTop + Cursor 重置为 0。
func TestHandlePIDChange_VisualStateReset(t *testing.T) {
	state := DebugState{
		ScrollTop: 50,
		Cursor:    7,
	}

	out := HandlePIDChange(state, types.PID(42))

	if out.ScrollTop != 0 {
		t.Errorf("ScrollTop = %d, want 0", out.ScrollTop)
	}
	if out.Cursor != 0 {
		t.Errorf("Cursor = %d, want 0", out.Cursor)
	}
}

// TestHandlePIDChange_AttachedPIDUpdated 验证 AttachedPID 更新为新 PID。
func TestHandlePIDChange_AttachedPIDUpdated(t *testing.T) {
	state := DebugState{
		AttachedPID: types.PID(10),
	}

	out := HandlePIDChange(state, types.PID(42))

	if out.AttachedPID != types.PID(42) {
		t.Errorf("AttachedPID = %d, want 42 (new)", out.AttachedPID)
	}
}

// TestHandlePIDChange_AttachedPIDZero 验证 newPID=0 时 AttachedPID=0。
func TestHandlePIDChange_AttachedPIDZero(t *testing.T) {
	state := DebugState{
		AttachedPID: types.PID(99),
	}

	out := HandlePIDChange(state, types.PID(0))

	if out.AttachedPID != types.PID(0) {
		t.Errorf("AttachedPID = %d, want 0", out.AttachedPID)
	}
}

// TestHandlePIDChange_AutoReloadedReset 验证 AutoReloaded 重置为 false。
func TestHandlePIDChange_AutoReloadedReset(t *testing.T) {
	state := DebugState{
		AutoReloaded: true,
	}

	out := HandlePIDChange(state, types.PID(42))

	if out.AutoReloaded != false {
		t.Errorf("AutoReloaded = %v, want false (reset)", out.AutoReloaded)
	}
}

// TestHandlePIDChange_HistWatermarkReset 验证 HistWatermark 重置为 0。
func TestHandlePIDChange_HistWatermarkReset(t *testing.T) {
	state := DebugState{
		HistWatermark: time.Now().UnixMilli(),
	}

	out := HandlePIDChange(state, types.PID(42))

	if out.HistWatermark != 0 {
		t.Errorf("HistWatermark = %d, want 0 (reset)", out.HistWatermark)
	}
}

// TestHandlePIDChange_PreservesOtherFields 验证其他字段（Mode/Client/StraceCh/ShowStrace/AutoScroll）保留。
func TestHandlePIDChange_PreservesOtherFields(t *testing.T) {
	state := DebugState{
		Mode:       true,
		ShowStrace: true,
		AutoScroll: true,
		// Client / StraceCh 字段类型为 *ipc.Client / <-chan ipc.SyscallEventWire，
		// 在测试中保留 nil 即可（验证 nil 字段不被修改也是契约一部分）。
	}

	out := HandlePIDChange(state, types.PID(42))

	if out.Mode != true {
		t.Errorf("Mode = %v, want true (preserved)", out.Mode)
	}
	if out.ShowStrace != true {
		t.Errorf("ShowStrace = %v, want true (preserved)", out.ShowStrace)
	}
	if out.AutoScroll != true {
		t.Errorf("AutoScroll = %v, want true (preserved)", out.AutoScroll)
	}
	if out.Client != nil {
		t.Errorf("Client = %v, want nil (preserved · was nil)", out.Client)
	}
	if out.StraceCh != nil {
		t.Errorf("StraceCh = %v, want nil (preserved · was nil)", out.StraceCh)
	}
}

// TestHandlePIDChange_DoesNotMutateInput 验证函数式语义（输入不被修改）。
func TestHandlePIDChange_DoesNotMutateInput(t *testing.T) {
	originalEvents := []event.UnifiedEvent{
		{Type: event.EventStep, Summary: "step1"},
	}
	state := DebugState{
		StraceEvents:  []event.UnifiedEvent{{Type: event.EventSyscall, Summary: "old"}},
		Events:        originalEvents,
		DeviceLatency: map[string]*DeviceLatencyStats{"llm": {Count: 5}},
		ScrollTop:     10,
		Cursor:        3,
		AttachedPID:   types.PID(99),
	}

	_ = HandlePIDChange(state, types.PID(42))

	// 输入 state 不应被修改（值传递语义 · 仅 slice / map header 拷贝 · 但 backing
	// array 仍共享 · 测试不触发 backing 修改即可证 functional 性质）。
	if state.ScrollTop != 10 {
		t.Errorf("input state.ScrollTop = %d, want 10 (input mutated)", state.ScrollTop)
	}
	if state.AttachedPID != types.PID(99) {
		t.Errorf("input state.AttachedPID = %d, want 99 (input mutated)", state.AttachedPID)
	}
	// originalEvents 切片 backing array 也不应被修改
	if len(originalEvents) != 1 || originalEvents[0].Summary != "step1" {
		t.Errorf("originalEvents mutated: %v", originalEvents)
	}
}
