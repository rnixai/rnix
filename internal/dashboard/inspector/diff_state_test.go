// Package inspector — diff_state_test.go (Story 38-5 PR11 Step 4(c))
package inspector

import (
	"strings"
	"testing"
	"time"

	"github.com/rnixai/rnix/ipc"
)

func TestExitDiffMode_ClearsAllSevenFields(t *testing.T) {
	state := InspectorState{
		DiffMode:         true,
		DiffBase:         5,
		DiffDelta:        2,
		DiffUnfolded:     map[int]bool{0: true, 1: true},
		DiffPicker:       true,
		DiffPickerCursor: 3,
		DiffDdDeadline:   time.Now().Add(time.Hour),
	}
	got := ExitDiffMode(state)

	if got.DiffMode {
		t.Error("DiffMode 应为 false")
	}
	if got.DiffBase != 0 {
		t.Errorf("DiffBase = %d, want 0", got.DiffBase)
	}
	if got.DiffDelta != 0 {
		t.Errorf("DiffDelta = %d, want 0", got.DiffDelta)
	}
	if got.DiffUnfolded != nil {
		t.Errorf("DiffUnfolded = %v, want nil", got.DiffUnfolded)
	}
	if got.DiffPicker {
		t.Error("DiffPicker 应为 false")
	}
	if got.DiffPickerCursor != 0 {
		t.Errorf("DiffPickerCursor = %d, want 0", got.DiffPickerCursor)
	}
	if !got.DiffDdDeadline.IsZero() {
		t.Errorf("DiffDdDeadline 应为零值, got %v", got.DiffDdDeadline)
	}
}

func TestExitDiffMode_PreservesNonDiffFields(t *testing.T) {
	state := InspectorState{
		DiffMode:       true,
		Lens:           2,
		Step:           7,
		StepMax:        10,
		FollowLive:     true,
		SystemExpanded: true,
	}
	got := ExitDiffMode(state)

	if got.Lens != 2 {
		t.Errorf("Lens 应保留为 2, got %d", got.Lens)
	}
	if got.Step != 7 {
		t.Errorf("Step 应保留为 7, got %d", got.Step)
	}
	if got.StepMax != 10 {
		t.Errorf("StepMax 应保留为 10, got %d", got.StepMax)
	}
	if !got.FollowLive {
		t.Error("FollowLive 应保留")
	}
	if !got.SystemExpanded {
		t.Error("SystemExpanded 应保留")
	}
}

func TestExitDiffMode_ZeroValueState_NoChange(t *testing.T) {
	got := ExitDiffMode(InspectorState{})
	if got.DiffMode || got.DiffBase != 0 || got.DiffDelta != 0 {
		t.Error("零值 state 应返回零值")
	}
	if got.DiffUnfolded != nil {
		t.Errorf("零值 DiffUnfolded 应为 nil, got %v", got.DiffUnfolded)
	}
}

func TestExitDiffMode_AlreadyExited_Idempotent(t *testing.T) {
	state := InspectorState{
		DiffMode: false, // 已退出
		Lens:     1,
	}
	got := ExitDiffMode(state)
	if got.DiffMode {
		t.Error("idempotent: 仍应为 false")
	}
	if got.Lens != 1 {
		t.Error("idempotent: 其他字段应保留")
	}
}

// --- SlideDiffBase ---

func mkSteps(stepNums ...int) []ipc.StepSummaryWire {
	out := make([]ipc.StepSummaryWire, len(stepNums))
	for i, n := range stepNums {
		out[i] = ipc.StepSummaryWire{Step: n}
	}
	return out
}

func TestSlideDiffBase_EmptySteps_ReturnsFalse(t *testing.T) {
	got, clamped := SlideDiffBase(InspectorState{}, 5)
	if clamped {
		t.Error("空 Steps: clamped 应为 false")
	}
	if got.DiffBase != 0 {
		t.Errorf("DiffBase 应保持 0, got %d", got.DiffBase)
	}
}

func TestSlideDiffBase_WithinRange_NoClampNeeded(t *testing.T) {
	state := InspectorState{
		Steps:     mkSteps(1, 2, 3, 4, 5),
		DiffDelta: 2,
	}
	// newCurrent=5 → target = 5 - 2 = 3（在范围内）
	got, clamped := SlideDiffBase(state, 5)
	if clamped {
		t.Error("不应触发 clamp")
	}
	if got.DiffBase != 3 {
		t.Errorf("DiffBase = %d, want 3", got.DiffBase)
	}
}

func TestSlideDiffBase_BelowFirst_ClampsAndSignals(t *testing.T) {
	state := InspectorState{
		Steps:     mkSteps(10, 20, 30),
		DiffDelta: 100, // 让 target 远低于 first
	}
	got, clamped := SlideDiffBase(state, 50)
	if !clamped {
		t.Error("应触发 clamp")
	}
	if got.DiffBase != 10 {
		t.Errorf("DiffBase = %d, want 10 (Steps[0].Step)", got.DiffBase)
	}
}

func TestSlideDiffBase_AboveLast_ClampsAndSignals(t *testing.T) {
	state := InspectorState{
		Steps:     mkSteps(1, 2, 3),
		DiffDelta: -100, // 让 target 远高于 last
	}
	got, clamped := SlideDiffBase(state, 50)
	if !clamped {
		t.Error("应触发 clamp")
	}
	if got.DiffBase != 3 {
		t.Errorf("DiffBase = %d, want 3 (Steps[last].Step)", got.DiffBase)
	}
}

func TestSlideDiffBase_ResetsDiffUnfolded(t *testing.T) {
	state := InspectorState{
		Steps:        mkSteps(1, 2, 3, 4, 5),
		DiffDelta:    2,
		DiffUnfolded: map[int]bool{0: true, 5: true},
	}
	got, _ := SlideDiffBase(state, 5)
	if got.DiffUnfolded == nil {
		t.Fatal("DiffUnfolded 应被重置为非 nil 空 map")
	}
	if len(got.DiffUnfolded) != 0 {
		t.Errorf("DiffUnfolded len = %d, want 0", len(got.DiffUnfolded))
	}
}

func TestSlideDiffBase_PreservesOtherDiffFields(t *testing.T) {
	state := InspectorState{
		Steps:            mkSteps(1, 2, 3),
		DiffMode:         true,
		DiffDelta:        1,
		DiffPicker:       true,
		DiffPickerCursor: 7,
	}
	got, _ := SlideDiffBase(state, 2)
	if !got.DiffMode {
		t.Error("DiffMode 应保留")
	}
	if got.DiffDelta != 1 {
		t.Error("DiffDelta 应保留")
	}
	if !got.DiffPicker {
		t.Error("DiffPicker 应保留")
	}
	if got.DiffPickerCursor != 7 {
		t.Error("DiffPickerCursor 应保留")
	}
}

func TestDiffBaseClampedMsg_Constant(t *testing.T) {
	if DiffBaseClampedMsg == "" {
		t.Error("DiffBaseClampedMsg 不应为空")
	}
	if !strings.Contains(DiffBaseClampedMsg, "clamped") {
		t.Errorf("文案应含 clamped, got %q", DiffBaseClampedMsg)
	}
}
