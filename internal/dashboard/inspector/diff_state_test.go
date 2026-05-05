// Package inspector — diff_state_test.go (Story 38-5 PR11 Step 4(c))
package inspector

import (
	"testing"
	"time"
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
