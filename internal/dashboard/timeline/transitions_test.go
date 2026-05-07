// Package timeline — transitions_test.go (Story 38-5 PR11 Step 4(c))
//
// HandlePIDUUIDChange 行为契约测试（与 cmd/rnix.handleTimelinePIDChange 等价）。
package timeline

import (
	"testing"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/ipc"
)

func sampleAttachedState() TimelineState {
	return TimelineState{
		AttachedPID:       types.PID(42),
		AttachedUUID:      "uuid-old",
		StepEntries:       []StepEntry{{}, {}},
		StepCursor:        5,
		StepScrollTop:     3,
		StepDetailCache:   map[int]*ipc.GetStepDetailResponse{1: {}},
		LastFetchedStep:   7,
		FetchingDetail:    true,
		StepFilters:       map[string]bool{"llm": true},
		StepFilterMode:    true,
		StepExpandedIdx:   2,
		ExpandedAggGroups: map[int]bool{0: true},
		ExpandMode:        ExpandModeExpanded,
		SortAsc:           true,
		MigrationChecked:  true,
	}
}

func TestHandlePIDUUIDChange_SameUUID_Noop(t *testing.T) {
	state := sampleAttachedState()
	got := HandlePIDUUIDChange(state, types.PID(99), "uuid-old")
	if got.AttachedPID != types.PID(42) {
		t.Errorf("same UUID: AttachedPID changed (%v → %v)", types.PID(42), got.AttachedPID)
	}
	if got.StepCursor != 5 {
		t.Errorf("same UUID: StepCursor reset (%d → %d)", 5, got.StepCursor)
	}
	if got.StepFilterMode != true {
		t.Errorf("same UUID: StepFilterMode reset")
	}
	if got.ExpandMode != ExpandModeExpanded {
		t.Errorf("same UUID: ExpandMode reset (%v → %v)", ExpandModeExpanded, got.ExpandMode)
	}
}

func TestHandlePIDUUIDChange_DifferentUUID_ResetsStepState(t *testing.T) {
	state := sampleAttachedState()
	got := HandlePIDUUIDChange(state, types.PID(99), "uuid-new")

	if got.AttachedPID != types.PID(99) {
		t.Errorf("AttachedPID = %v, want 99", got.AttachedPID)
	}
	if got.AttachedUUID != "uuid-new" {
		t.Errorf("AttachedUUID = %q, want uuid-new", got.AttachedUUID)
	}
	if got.StepEntries != nil {
		t.Errorf("StepEntries = %v, want nil", got.StepEntries)
	}
	if got.StepCursor != 0 {
		t.Errorf("StepCursor = %d, want 0", got.StepCursor)
	}
	if got.StepScrollTop != 0 {
		t.Errorf("StepScrollTop = %d, want 0", got.StepScrollTop)
	}
	if got.StepDetailCache == nil {
		t.Error("StepDetailCache is nil; want fresh empty map")
	}
	if len(got.StepDetailCache) != 0 {
		t.Errorf("StepDetailCache len = %d, want 0", len(got.StepDetailCache))
	}
	if got.LastFetchedStep != 0 {
		t.Errorf("LastFetchedStep = %d, want 0", got.LastFetchedStep)
	}
	if got.FetchingDetail {
		t.Error("FetchingDetail = true, want false")
	}
	if got.StepFilterMode {
		t.Error("StepFilterMode = true, want false")
	}
	if got.StepExpandedIdx != -1 {
		t.Errorf("StepExpandedIdx = %d, want -1", got.StepExpandedIdx)
	}
	if got.ExpandedAggGroups == nil {
		t.Error("ExpandedAggGroups is nil; want fresh empty map")
	}
	if len(got.ExpandedAggGroups) != 0 {
		t.Errorf("ExpandedAggGroups len = %d, want 0", len(got.ExpandedAggGroups))
	}
	if got.ExpandMode != ExpandModeCollapsed {
		t.Errorf("ExpandMode = %v, want ExpandModeCollapsed (Story 36-4)", got.ExpandMode)
	}
}

func TestHandlePIDUUIDChange_PreservesUserPrefs(t *testing.T) {
	state := sampleAttachedState()
	got := HandlePIDUUIDChange(state, types.PID(99), "uuid-new")

	if !got.SortAsc {
		t.Error("SortAsc reset, want preserved (user pref)")
	}
	if !got.MigrationChecked {
		t.Error("MigrationChecked reset, want preserved")
	}
	if len(got.StepFilters) != 1 || !got.StepFilters["llm"] {
		t.Errorf("StepFilters reset (%v), want preserved (user pref)", got.StepFilters)
	}
}

func TestHandlePIDUUIDChange_ZeroValueState(t *testing.T) {
	got := HandlePIDUUIDChange(TimelineState{}, types.PID(1), "uuid-1")
	if got.AttachedUUID != "uuid-1" {
		t.Errorf("AttachedUUID = %q, want uuid-1", got.AttachedUUID)
	}
	if got.StepDetailCache == nil {
		t.Error("StepDetailCache nil after zero-value transition")
	}
	if got.ExpandedAggGroups == nil {
		t.Error("ExpandedAggGroups nil after zero-value transition")
	}
	if got.StepExpandedIdx != -1 {
		t.Errorf("StepExpandedIdx = %d, want -1 (sentinel)", got.StepExpandedIdx)
	}
}

func TestHandlePIDUUIDChange_EmptyUUIDToEmptyUUID_Noop(t *testing.T) {
	state := TimelineState{StepCursor: 7}
	got := HandlePIDUUIDChange(state, types.PID(0), "")
	if got.StepCursor != 7 {
		t.Errorf("empty→empty UUID: StepCursor reset (want noop)")
	}
}

func TestHandlePIDUUIDChange_ReturnsFreshCacheReferences(t *testing.T) {
	state := sampleAttachedState()
	origCache := state.StepDetailCache
	origAgg := state.ExpandedAggGroups

	got := HandlePIDUUIDChange(state, types.PID(99), "uuid-new")

	if &got.StepDetailCache == &origCache {
		t.Error("StepDetailCache shares pointer with original (悬挂引用)")
	}
	if &got.ExpandedAggGroups == &origAgg {
		t.Error("ExpandedAggGroups shares pointer with original")
	}
}
