package timeline

import (
	"testing"

	"github.com/rnixai/rnix/ipc"
)

func TestApplyNewSteps_AppendsToEmpty(t *testing.T) {
	state := TimelineState{}
	steps := []ipc.StepSummaryWire{
		{Step: 1, Action: "tool_call"},
		{Step: 2, Action: "plan"},
	}
	got := ApplyNewSteps(state, steps)
	if len(got.StepEntries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(got.StepEntries))
	}
	if got.StepEntries[0].Summary.Step != 1 || got.StepEntries[1].Summary.Step != 2 {
		t.Errorf("steps mis-ordered: %+v", got.StepEntries)
	}
}

func TestApplyNewSteps_DeduplicatesByStepNumber(t *testing.T) {
	state := TimelineState{
		StepEntries: []StepEntry{
			{Summary: ipc.StepSummaryWire{Step: 1, Action: "tool_call"}},
			{Summary: ipc.StepSummaryWire{Step: 2, Action: "plan"}},
		},
	}
	steps := []ipc.StepSummaryWire{
		{Step: 2, Action: "plan"},  // duplicate
		{Step: 3, Action: "spawn"}, // new
	}
	got := ApplyNewSteps(state, steps)
	if len(got.StepEntries) != 3 {
		t.Fatalf("expected 3 entries (2 known + 1 new), got %d", len(got.StepEntries))
	}
	if got.StepEntries[2].Summary.Step != 3 {
		t.Errorf("new entry should be step 3, got %d", got.StepEntries[2].Summary.Step)
	}
}

func TestApplyNewSteps_ExpandModeCollapsed_DefaultLevel(t *testing.T) {
	state := TimelineState{ExpandMode: ExpandModeCollapsed}
	steps := []ipc.StepSummaryWire{{Step: 1, Action: "tool_call"}}
	got := ApplyNewSteps(state, steps)
	if got.StepEntries[0].Level != LevelSummary {
		t.Errorf("collapsed mode: new step should default to LevelSummary, got %d", got.StepEntries[0].Level)
	}
	if got.StepEntries[0].AutoExpand {
		t.Errorf("collapsed mode: new step should not be AutoExpand")
	}
}

func TestApplyNewSteps_ExpandModeExpanded_AllExpanded(t *testing.T) {
	state := TimelineState{ExpandMode: ExpandModeExpanded}
	steps := []ipc.StepSummaryWire{
		{Step: 1, Action: "tool_call"},
		{Step: 2, Action: "plan"},
	}
	got := ApplyNewSteps(state, steps)
	for i, e := range got.StepEntries {
		if e.Level != LevelExpanded {
			t.Errorf("expanded mode: entry %d should be LevelExpanded, got %d", i, e.Level)
		}
		if !e.AutoExpand {
			t.Errorf("expanded mode: entry %d should be AutoExpand", i)
		}
	}
}

func TestApplyNewSteps_ExpandModeErrorsOnly_OnlyErrorsExpanded(t *testing.T) {
	state := TimelineState{ExpandMode: ExpandModeErrorsOnly}
	steps := []ipc.StepSummaryWire{
		{Step: 1, HasError: false, Action: "tool_call"},
		{Step: 2, HasError: true, Action: "tool_call"},
	}
	got := ApplyNewSteps(state, steps)
	if got.StepEntries[0].Level != LevelSummary || got.StepEntries[0].AutoExpand {
		t.Errorf("errors-only: non-error step should be Summary/!AutoExpand, got %+v", got.StepEntries[0])
	}
	if got.StepEntries[1].Level != LevelExpanded || !got.StepEntries[1].AutoExpand {
		t.Errorf("errors-only: error step should be Expanded/AutoExpand, got %+v", got.StepEntries[1])
	}
}

func TestApplyNewSteps_CollapsedSafetyNet_ErrorAlwaysExpanded(t *testing.T) {
	state := TimelineState{ExpandMode: ExpandModeCollapsed}
	steps := []ipc.StepSummaryWire{
		{Step: 1, HasError: false, Action: "tool_call"},
		{Step: 2, HasError: true, Action: "tool_call"},
	}
	got := ApplyNewSteps(state, steps)
	if got.StepEntries[0].Level != LevelSummary {
		t.Errorf("collapsed safety net: non-error stays Summary")
	}
	if got.StepEntries[1].Level != LevelExpanded || !got.StepEntries[1].AutoExpand {
		t.Errorf("collapsed safety net: error step must auto-expand, got %+v", got.StepEntries[1])
	}
}

func TestApplyNewSteps_PureFunction_DoesNotMutateInput(t *testing.T) {
	steps := []ipc.StepSummaryWire{
		{Step: 1, Action: "tool_call"},
	}
	state := TimelineState{
		StepEntries: []StepEntry{
			{Summary: ipc.StepSummaryWire{Step: 100, Action: "preexisting"}},
		},
	}
	original := state.StepEntries[0].Summary.Step
	got := ApplyNewSteps(state, steps)

	// Original entries must not be replaced (zero-mutation contract).
	if got.StepEntries[0].Summary.Step != original {
		t.Errorf("preexisting entry mutated: original=%d, got=%d", original, got.StepEntries[0].Summary.Step)
	}
	if got.StepEntries[len(got.StepEntries)-1].Summary.Step != 1 {
		t.Errorf("new entry not appended at tail")
	}
}

func TestApplyNewSteps_EmptyInput_NoOp(t *testing.T) {
	state := TimelineState{
		StepEntries: []StepEntry{
			{Summary: ipc.StepSummaryWire{Step: 1}},
		},
	}
	got := ApplyNewSteps(state, nil)
	if len(got.StepEntries) != 1 {
		t.Errorf("nil steps should be no-op, got %d entries", len(got.StepEntries))
	}
	got2 := ApplyNewSteps(state, []ipc.StepSummaryWire{})
	if len(got2.StepEntries) != 1 {
		t.Errorf("empty steps should be no-op, got %d entries", len(got2.StepEntries))
	}
}
