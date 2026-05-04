// Package event — helpers_test.go (Story 38-5 PR11 Step 4(a-2) timeline event-related
// helpers 测试 · 续 timeline/helpers_test.go pure helpers · 与 helpers.go 同包)
//
// 测试覆盖：
//   - AggThreshold 常量值（= 3 · 与 cmd/rnix 等价）
//   - BuildToolAggGroups 6 项（empty / 单步 / 双步 / 三步 (达 threshold) /
//     混合 / nil StepEntry skip / 跨工具切换 / 空 ToolPath skip）
//   - SysEventStyle 7 项（每个 Event* 类型 + EventExit Sev 分支 + default）
//   - DefaultStepFilters 4 项（key 存在性 + 全 true + sys_spawn key + EventSyscall key）
//   - IsEventVisible 8 项（empty filter / step action visible/hidden / nil StepEntry /
//     sys_spawn vs step-action "spawn" 区分 / 其他 system event）
package event

import (
	"testing"

	"github.com/charmbracelet/lipgloss"

	"github.com/rnixai/rnix/internal/dashboard/timeline"
	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"
)

// =============================================================================
// AggThreshold
// =============================================================================

func TestAggThreshold_EqualsThree(t *testing.T) {
	if AggThreshold != 3 {
		t.Errorf("AggThreshold = %d, want 3 (与 cmd/rnix.timelineAggThreshold 等价)", AggThreshold)
	}
}

// =============================================================================
// BuildToolAggGroups (8 项)
// =============================================================================

func mkStepEv(step int, toolPath string) UnifiedEvent {
	return UnifiedEvent{
		Type: EventStep,
		StepEntry: &timeline.StepEntry{
			Summary: ipc.StepSummaryWire{Step: step, ToolPath: toolPath},
		},
	}
}

func TestBuildToolAggGroups_Empty(t *testing.T) {
	if got := BuildToolAggGroups(nil); got != nil {
		t.Errorf("nil input: want nil, got %v", got)
	}
	if got := BuildToolAggGroups([]UnifiedEvent{}); got != nil {
		t.Errorf("empty input: want nil, got %v", got)
	}
}

func TestBuildToolAggGroups_SingleStep(t *testing.T) {
	events := []UnifiedEvent{mkStepEv(1, "Read")}
	if got := BuildToolAggGroups(events); got != nil {
		t.Errorf("1 step (< AggThreshold=3): want nil, got %v", got)
	}
}

func TestBuildToolAggGroups_TwoSteps(t *testing.T) {
	events := []UnifiedEvent{mkStepEv(1, "Read"), mkStepEv(2, "Read")}
	if got := BuildToolAggGroups(events); got != nil {
		t.Errorf("2 steps (< AggThreshold=3): want nil, got %v", got)
	}
}

func TestBuildToolAggGroups_ThreeStepsTriggers(t *testing.T) {
	events := []UnifiedEvent{mkStepEv(1, "Read"), mkStepEv(2, "Read"), mkStepEv(3, "Read")}
	got := BuildToolAggGroups(events)
	if len(got) != 1 {
		t.Fatalf("3 steps (= AggThreshold): want 1 group, got %d", len(got))
	}
	g := got[0]
	if g.StartIdx != 0 || g.EndIdx != 3 {
		t.Errorf("group bounds: want [0,3), got [%d,%d)", g.StartIdx, g.EndIdx)
	}
	if g.ToolPath != "Read" {
		t.Errorf("ToolPath: want 'Read', got %q", g.ToolPath)
	}
	if len(g.StepNums) != 3 || g.StepNums[0] != 1 || g.StepNums[2] != 3 {
		t.Errorf("StepNums: want [1,2,3], got %v", g.StepNums)
	}
}

func TestBuildToolAggGroups_NilStepEntrySkipped(t *testing.T) {
	events := []UnifiedEvent{
		{Type: EventStep, StepEntry: nil},
		mkStepEv(1, "Read"),
		mkStepEv(2, "Read"),
		mkStepEv(3, "Read"),
	}
	got := BuildToolAggGroups(events)
	if len(got) != 1 {
		t.Fatalf("nil-StepEntry should be skipped: want 1 group, got %d", len(got))
	}
	if got[0].StartIdx != 1 {
		t.Errorf("group should start after nil entry: want 1, got %d", got[0].StartIdx)
	}
}

func TestBuildToolAggGroups_DistinctToolPathsBreaksRun(t *testing.T) {
	events := []UnifiedEvent{
		mkStepEv(1, "Read"),
		mkStepEv(2, "Read"),
		mkStepEv(3, "Write"), // breaks the Read run
		mkStepEv(4, "Write"),
		mkStepEv(5, "Write"),
	}
	got := BuildToolAggGroups(events)
	// Read run = 2 (below threshold) → no group
	// Write run = 3 (at threshold) → 1 group
	if len(got) != 1 {
		t.Fatalf("want 1 Write group only (Read 2 < 3 threshold): got %d groups", len(got))
	}
	if got[0].ToolPath != "Write" {
		t.Errorf("group ToolPath: want 'Write', got %q", got[0].ToolPath)
	}
	if got[0].StartIdx != 2 || got[0].EndIdx != 5 {
		t.Errorf("Write group bounds: want [2,5), got [%d,%d)", got[0].StartIdx, got[0].EndIdx)
	}
}

func TestBuildToolAggGroups_EmptyToolPathSkipped(t *testing.T) {
	events := []UnifiedEvent{
		mkStepEv(1, ""), // no ToolPath
		mkStepEv(2, "Read"),
		mkStepEv(3, "Read"),
		mkStepEv(4, "Read"),
	}
	got := BuildToolAggGroups(events)
	if len(got) != 1 {
		t.Fatalf("empty ToolPath should be skipped: want 1 group, got %d", len(got))
	}
	if got[0].StartIdx != 1 {
		t.Errorf("group should start after empty-ToolPath entry: want 1, got %d", got[0].StartIdx)
	}
}

// =============================================================================
// SysEventStyle (7 项)
// =============================================================================

func TestSysEventStyle_Compact(t *testing.T) {
	s := SysEventStyle(UnifiedEvent{Type: EventCompact})
	if got := s.GetForeground(); got != lipgloss.Color("#00CED1") {
		t.Errorf("Compact: want #00CED1, got %q", got)
	}
	if !s.GetBold() {
		t.Error("Compact should be Bold")
	}
}

func TestSysEventStyle_Budget(t *testing.T) {
	s := SysEventStyle(UnifiedEvent{Type: EventBudget})
	if got := s.GetForeground(); got != lipgloss.Color(ui.ColorWarning) {
		t.Errorf("Budget: want ColorWarning, got %q", got)
	}
}

func TestSysEventStyle_Spawn(t *testing.T) {
	s := SysEventStyle(UnifiedEvent{Type: EventSpawn})
	if got := s.GetForeground(); got != lipgloss.Color(ui.ColorSuccess) {
		t.Errorf("Spawn: want ColorSuccess, got %q", got)
	}
}

func TestSysEventStyle_Exit_SevError(t *testing.T) {
	s := SysEventStyle(UnifiedEvent{Type: EventExit, Severity: SevError})
	if got := s.GetForeground(); got != lipgloss.Color(ui.ColorError) {
		t.Errorf("Exit SevError: want ColorError, got %q", got)
	}
}

func TestSysEventStyle_Exit_BelowSevError(t *testing.T) {
	s := SysEventStyle(UnifiedEvent{Type: EventExit, Severity: SevWarn})
	if got := s.GetForeground(); got != lipgloss.Color(ui.ColorMuted) {
		t.Errorf("Exit SevWarn: want ColorMuted, got %q", got)
	}
}

func TestSysEventStyle_Stall(t *testing.T) {
	s := SysEventStyle(UnifiedEvent{Type: EventStall})
	if got := s.GetForeground(); got != lipgloss.Color(ui.ColorWarning) {
		t.Errorf("Stall: want ColorWarning, got %q", got)
	}
	if !s.GetBold() {
		t.Error("Stall should be Bold")
	}
}

func TestSysEventStyle_Immune(t *testing.T) {
	s := SysEventStyle(UnifiedEvent{Type: EventImmune})
	if got := s.GetForeground(); got != lipgloss.Color("#9B59B6") {
		t.Errorf("Immune: want #9B59B6, got %q", got)
	}
}

func TestSysEventStyle_Default(t *testing.T) {
	s := SysEventStyle(UnifiedEvent{Type: "unknown"})
	if got := s.GetForeground(); got != lipgloss.Color(ui.ColorMuted) {
		t.Errorf("default: want ColorMuted, got %q", got)
	}
}

// =============================================================================
// DefaultStepFilters (4 项)
// =============================================================================

func TestDefaultStepFilters_AllActionTypesPresent(t *testing.T) {
	f := DefaultStepFilters()
	for _, k := range []string{"tool_call", "plan", "text", "complete", "spawn", "replan", "specialize"} {
		if !f[k] {
			t.Errorf("step action filter %q should be true by default", k)
		}
	}
}

func TestDefaultStepFilters_AllSystemEventsPresent(t *testing.T) {
	f := DefaultStepFilters()
	for _, k := range []string{
		EventCompact, EventBudget, "sys_spawn",
		EventExit, EventStall, EventImmune, EventSyscall,
	} {
		if !f[k] {
			t.Errorf("system event filter %q should be true by default", k)
		}
	}
}

func TestDefaultStepFilters_SysSpawnDistinctFromSpawn(t *testing.T) {
	f := DefaultStepFilters()
	// Both keys must exist independently (F7 contract)
	if _, ok := f["sys_spawn"]; !ok {
		t.Error("'sys_spawn' filter key must exist (F7 distinct from step-action 'spawn')")
	}
	if _, ok := f["spawn"]; !ok {
		t.Error("'spawn' (step action) filter key must exist")
	}
}

func TestDefaultStepFilters_EventSyscallPresent(t *testing.T) {
	// Story 34.6: strace events in debug mode
	f := DefaultStepFilters()
	if !f[EventSyscall] {
		t.Errorf("EventSyscall filter must be true by default (Story 34.6)")
	}
}

// =============================================================================
// IsEventVisible (8 项)
// =============================================================================

func TestIsEventVisible_NilFilter(t *testing.T) {
	if !IsEventVisible(UnifiedEvent{Type: EventStep}, nil) {
		t.Error("nil filter: want visible=true")
	}
}

func TestIsEventVisible_EmptyFilter(t *testing.T) {
	if !IsEventVisible(UnifiedEvent{Type: EventStep}, map[string]bool{}) {
		t.Error("empty filter: want visible=true")
	}
}

func TestIsEventVisible_StepActionVisible(t *testing.T) {
	ev := UnifiedEvent{Type: EventStep, StepEntry: &timeline.StepEntry{Summary: ipc.StepSummaryWire{Action: "tool_call"}}}
	if !IsEventVisible(ev, map[string]bool{"tool_call": true}) {
		t.Error("tool_call enabled: want visible=true")
	}
}

func TestIsEventVisible_StepActionHidden(t *testing.T) {
	ev := UnifiedEvent{Type: EventStep, StepEntry: &timeline.StepEntry{Summary: ipc.StepSummaryWire{Action: "plan"}}}
	if IsEventVisible(ev, map[string]bool{"plan": false}) {
		t.Error("plan disabled: want visible=false")
	}
}

func TestIsEventVisible_NilStepEntryFallback(t *testing.T) {
	ev := UnifiedEvent{Type: EventStep, StepEntry: nil}
	if !IsEventVisible(ev, map[string]bool{"tool_call": true}) {
		t.Error("nil StepEntry: want visible=true (fallback)")
	}
}

func TestIsEventVisible_SysSpawnUsesDistinctKey(t *testing.T) {
	ev := UnifiedEvent{Type: EventSpawn}
	// "spawn" key (step action) should NOT control sys spawn visibility
	if IsEventVisible(ev, map[string]bool{"spawn": true, "sys_spawn": false}) {
		t.Error("EventSpawn uses 'sys_spawn' key: want visible=false (sys_spawn=false)")
	}
	if !IsEventVisible(ev, map[string]bool{"spawn": false, "sys_spawn": true}) {
		t.Error("EventSpawn: want visible=true (sys_spawn=true · F7 contract)")
	}
}

func TestIsEventVisible_OtherSystemEvent(t *testing.T) {
	ev := UnifiedEvent{Type: EventCompact}
	if !IsEventVisible(ev, map[string]bool{EventCompact: true}) {
		t.Error("EventCompact enabled: want visible=true")
	}
	if IsEventVisible(ev, map[string]bool{EventCompact: false}) {
		t.Error("EventCompact disabled: want visible=false")
	}
}

func TestIsEventVisible_StallEvent(t *testing.T) {
	ev := UnifiedEvent{Type: EventStall}
	if !IsEventVisible(ev, map[string]bool{EventStall: true}) {
		t.Error("EventStall enabled: want visible=true")
	}
}
