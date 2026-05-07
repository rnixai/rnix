// Package timeline — expand_mode_test.go (Story 38-5 PR11 Step 4(c))
package timeline

import (
	"testing"

	"github.com/rnixai/rnix/ipc"
)

func entry(step int, level StepDetailLevel, hasError bool) StepEntry {
	return StepEntry{
		Summary: ipc.StepSummaryWire{Step: step, HasError: hasError},
		Level:   level,
	}
}

func TestApplyExpandMode_Expanded_AllUpgraded(t *testing.T) {
	state := TimelineState{
		StepEntries:     []StepEntry{entry(1, LevelSummary, false), entry(2, LevelSummary, false)},
		StepDetailCache: map[int]*ipc.GetStepDetailResponse{},
	}
	got, msg := ApplyExpandMode(state, ExpandModeExpanded, func(*ipc.GetStepDetailResponse, ipc.StepSummaryWire) bool { return true })
	if got.ExpandMode != ExpandModeExpanded {
		t.Errorf("ExpandMode = %v, want Expanded", got.ExpandMode)
	}
	for i, e := range got.StepEntries {
		if e.Level != LevelExpanded {
			t.Errorf("entry[%d].Level = %v, want Expanded", i, e.Level)
		}
	}
	if msg != ExpandStatusAll {
		t.Errorf("msg = %q, want %q", msg, ExpandStatusAll)
	}
}

func TestApplyExpandMode_Expanded_DetailNilFallsThrough(t *testing.T) {
	// detail==nil 路径：不调用 hasExpandable，直接升级（cmd/rnix 历史语义）
	state := TimelineState{
		StepEntries:     []StepEntry{entry(1, LevelSummary, false)},
		StepDetailCache: map[int]*ipc.GetStepDetailResponse{}, // step 1 无 cache
	}
	hasExpandableCalled := false
	got, _ := ApplyExpandMode(state, ExpandModeExpanded, func(*ipc.GetStepDetailResponse, ipc.StepSummaryWire) bool {
		hasExpandableCalled = true
		return false
	})
	if hasExpandableCalled {
		t.Error("detail==nil 时不应调用 hasExpandable（早期 short-circuit）")
	}
	if got.StepEntries[0].Level != LevelExpanded {
		t.Errorf("detail==nil 应升级为 Expanded, got %v", got.StepEntries[0].Level)
	}
}

func TestApplyExpandMode_Expanded_DetailNotExpandable_Skipped(t *testing.T) {
	state := TimelineState{
		StepEntries: []StepEntry{entry(1, LevelSummary, false)},
		StepDetailCache: map[int]*ipc.GetStepDetailResponse{
			1: {}, // detail exists
		},
	}
	got, _ := ApplyExpandMode(state, ExpandModeExpanded, func(*ipc.GetStepDetailResponse, ipc.StepSummaryWire) bool { return false })
	if got.StepEntries[0].Level != LevelSummary {
		t.Errorf("detail 不可展开时应保持 Summary, got %v", got.StepEntries[0].Level)
	}
}

func TestApplyExpandMode_Expanded_AlreadyExpanded_Skipped(t *testing.T) {
	state := TimelineState{
		StepEntries:     []StepEntry{entry(1, LevelExpanded, false)},
		StepDetailCache: map[int]*ipc.GetStepDetailResponse{},
	}
	calls := 0
	_, msg := ApplyExpandMode(state, ExpandModeExpanded, func(*ipc.GetStepDetailResponse, ipc.StepSummaryWire) bool {
		calls++
		return true
	})
	if calls != 0 {
		t.Errorf("已 Expanded 不应重新评估 hasExpandable, calls=%d", calls)
	}
	// expanded==0 但 len(entries)>0 → 仍返回 ExpandStatusAll（不返回 NoSteps）
	if msg != ExpandStatusAll {
		t.Errorf("msg = %q, want ExpandStatusAll", msg)
	}
}

func TestApplyExpandMode_Expanded_EmptyList_NoStepsMessage(t *testing.T) {
	state := TimelineState{
		StepEntries:     nil,
		StepDetailCache: map[int]*ipc.GetStepDetailResponse{},
	}
	_, msg := ApplyExpandMode(state, ExpandModeExpanded, nil)
	if msg != ExpandStatusAllNoSteps {
		t.Errorf("空列表 msg = %q, want %q", msg, ExpandStatusAllNoSteps)
	}
}

func TestApplyExpandMode_ErrorsOnly_BifurcatesByHasError(t *testing.T) {
	state := TimelineState{
		StepEntries: []StepEntry{
			entry(1, LevelSummary, false),
			entry(2, LevelSummary, true),
			entry(3, LevelExpanded, false), // 应被降到 Summary
			entry(4, LevelSummary, true),
		},
	}
	got, msg := ApplyExpandMode(state, ExpandModeErrorsOnly, nil)
	if got.ExpandMode != ExpandModeErrorsOnly {
		t.Errorf("ExpandMode = %v, want ErrorsOnly", got.ExpandMode)
	}
	wantLevels := []StepDetailLevel{LevelSummary, LevelExpanded, LevelSummary, LevelExpanded}
	for i, want := range wantLevels {
		if got.StepEntries[i].Level != want {
			t.Errorf("entry[%d].Level = %v, want %v", i, got.StepEntries[i].Level, want)
		}
	}
	if msg != ExpandStatusErrorsOnly {
		t.Errorf("msg = %q, want %q", msg, ExpandStatusErrorsOnly)
	}
}

func TestApplyExpandMode_Collapsed_AllSummary(t *testing.T) {
	state := TimelineState{
		StepEntries: []StepEntry{
			entry(1, LevelExpanded, true),
			entry(2, LevelExpanded, false),
		},
	}
	got, msg := ApplyExpandMode(state, ExpandModeCollapsed, nil)
	if got.ExpandMode != ExpandModeCollapsed {
		t.Errorf("ExpandMode = %v, want Collapsed", got.ExpandMode)
	}
	for i, e := range got.StepEntries {
		if e.Level != LevelSummary {
			t.Errorf("entry[%d].Level = %v, want Summary", i, e.Level)
		}
	}
	if msg != ExpandStatusCollapsed {
		t.Errorf("msg = %q, want %q", msg, ExpandStatusCollapsed)
	}
}

func TestApplyExpandMode_NilHasExpandable_UsesDefault(t *testing.T) {
	// 仅验证 nil fallback 不 panic（实际使用包内 HasExpandableContent）
	state := TimelineState{
		StepEntries:     []StepEntry{entry(1, LevelSummary, false)},
		StepDetailCache: map[int]*ipc.GetStepDetailResponse{},
	}
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("nil hasExpandable fallback panic: %v", r)
		}
	}()
	got, _ := ApplyExpandMode(state, ExpandModeExpanded, nil)
	if got.StepEntries[0].Level != LevelExpanded {
		t.Error("nil fallback 应 detail==nil 短路径升级为 Expanded")
	}
}

func TestToggleSortDirection_ToAscending(t *testing.T) {
	got, msg := ToggleSortDirection(TimelineState{SortAsc: false})
	if !got.SortAsc {
		t.Error("应切换到 SortAsc=true")
	}
	if msg != SortStatusAscending {
		t.Errorf("msg = %q, want %q", msg, SortStatusAscending)
	}
}

func TestToggleSortDirection_ToDescending(t *testing.T) {
	got, msg := ToggleSortDirection(TimelineState{SortAsc: true})
	if got.SortAsc {
		t.Error("应切换到 SortAsc=false")
	}
	if msg != SortStatusDescending {
		t.Errorf("msg = %q, want %q", msg, SortStatusDescending)
	}
}

func TestToggleSortDirection_PreservesOtherFields(t *testing.T) {
	state := TimelineState{
		SortAsc:     true,
		StepCursor:  7,
		StepEntries: []StepEntry{entry(1, LevelExpanded, false)},
	}
	got, _ := ToggleSortDirection(state)
	if got.StepCursor != 7 {
		t.Error("StepCursor 应保留")
	}
	if len(got.StepEntries) != 1 {
		t.Error("StepEntries 应保留")
	}
}
