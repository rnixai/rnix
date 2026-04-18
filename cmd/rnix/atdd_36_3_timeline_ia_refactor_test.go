package main

// =============================================================================
// ATDD Story 36.3: Timeline Information Architecture Refactor
// Tests for: default line layout, degraded display, tool aggregation
// =============================================================================
//
// Test Strategy:
//   AC-1: Default line shows step# ▸ action · summary
//   AC-2: Degraded display when detail is nil
//   AC-3: Consecutive tool aggregation (≥3 same ToolPath)
//   AC-4: Aggregation expand/collapse
//   AC-5: Aggregation coexists with bulk aggregation
//   AC-6: Test coverage
//   AC-7: Build passes (make all)

import (
	"strings"
	"testing"
	"time"

	"github.com/mattn/go-runewidth"
	"github.com/rnixai/rnix/ipc"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func newTimeline36_3Model() dashboardModel {
	m := newDashboardModel(nil)
	m.width = 120
	m.height = 40
	m.connected = true
	m.selectedPID = 1
	m.activePane = paneTimeline

	m.stepEntries = []stepEntry{
		{summary: ipc.StepSummaryWire{Step: 1, Action: "tool_call", Summary: "read config", ToolPath: "/dev/fs", DurationMs: 50, TokenCount: 100}},
		{summary: ipc.StepSummaryWire{Step: 2, Action: "tool_call", Summary: "run build", ToolPath: "/dev/shell", DurationMs: 120, TokenCount: 200}},
		{summary: ipc.StepSummaryWire{Step: 3, Action: "plan", Summary: "planning next step", DurationMs: 30, TokenCount: 80}},
	}

	now := time.Now()
	for i := range m.stepEntries {
		m.unifiedEvents = append(m.unifiedEvents, UnifiedEvent{
			Type:      EventStep,
			Timestamp: now.Add(time.Duration(i) * time.Second),
			PID:       1,
			Summary:   m.stepEntries[i].summary.Summary,
			StepEntry: &m.stepEntries[i],
		})
	}
	m.stepCursor = 0
	m.stepDetailCache = make(map[int]*ipc.GetStepDetailResponse)
	return m
}

// newTimeline36_3AggModel creates a model with consecutive same-ToolPath steps for aggregation tests.
func newTimeline36_3AggModel() dashboardModel {
	m := newDashboardModel(nil)
	m.width = 120
	m.height = 40
	m.connected = true
	m.selectedPID = 1
	m.activePane = paneTimeline

	// 5 consecutive shell.exec steps (should aggregate)
	// then 1 plan step (breaks the run)
	// then 2 fs.read steps (below threshold, should NOT aggregate)
	m.stepEntries = []stepEntry{
		{summary: ipc.StepSummaryWire{Step: 1, Action: "tool_call", Summary: "ls ~/project", ToolPath: "/dev/shell", DurationMs: 100, TokenCount: 50}},
		{summary: ipc.StepSummaryWire{Step: 2, Action: "tool_call", Summary: "cat /etc/hosts", ToolPath: "/dev/shell", DurationMs: 200, TokenCount: 60}},
		{summary: ipc.StepSummaryWire{Step: 3, Action: "tool_call", Summary: "git status", ToolPath: "/dev/shell", DurationMs: 150, TokenCount: 70}},
		{summary: ipc.StepSummaryWire{Step: 4, Action: "tool_call", Summary: "pwd", ToolPath: "/dev/shell", DurationMs: 80, TokenCount: 40}},
		{summary: ipc.StepSummaryWire{Step: 5, Action: "tool_call", Summary: "echo hello", ToolPath: "/dev/shell", DurationMs: 90, TokenCount: 30}},
		{summary: ipc.StepSummaryWire{Step: 6, Action: "plan", Summary: "planning next", DurationMs: 20, TokenCount: 100}},
		{summary: ipc.StepSummaryWire{Step: 7, Action: "tool_call", Summary: "read main.go", ToolPath: "/dev/fs", DurationMs: 50, TokenCount: 50}},
		{summary: ipc.StepSummaryWire{Step: 8, Action: "tool_call", Summary: "read go.mod", ToolPath: "/dev/fs", DurationMs: 40, TokenCount: 40}},
	}

	now := time.Now()
	for i := range m.stepEntries {
		m.unifiedEvents = append(m.unifiedEvents, UnifiedEvent{
			Type:      EventStep,
			Timestamp: now.Add(time.Duration(i) * time.Second),
			PID:       1,
			Summary:   m.stepEntries[i].summary.Summary,
			StepEntry: &m.stepEntries[i],
		})
	}
	m.stepCursor = 0
	m.stepDetailCache = make(map[int]*ipc.GetStepDetailResponse)
	m.expandedAggGroups = make(map[int]bool)
	return m
}

// ---------------------------------------------------------------------------
// AC-1: Default line information architecture — step# ▸ action · summary
// ---------------------------------------------------------------------------

func TestATDD_36_3_AC1_DefaultLine_WithDetail(t *testing.T) {
	m := newTimeline36_3Model()
	// Populate detail cache for step 1
	m.stepDetailCache[1] = &ipc.GetStepDetailResponse{
		Step:     1,
		Action:   "tool_call",
		ToolPath: "/dev/fs",
		Summary:  "read config from disk",
	}

	output := m.renderTimelinePane(120, 20)

	// New layout: step# ▸ action · summary
	if !strings.Contains(output, "1 ▸") {
		t.Errorf("AC-1: default line should contain step number '1 ▸', got:\n%s", output)
	}
	if !strings.Contains(output, "/dev/fs") {
		t.Errorf("AC-1: default line should contain action '/dev/fs', got:\n%s", output)
	}
	if !strings.Contains(output, "read config from disk") {
		t.Errorf("AC-1: default line should contain summary from detail 'read config from disk', got:\n%s", output)
	}
}

func TestATDD_36_3_AC1_DefaultLine_ActionSummaryLayout(t *testing.T) {
	m := newTimeline36_3Model()
	m.stepDetailCache[2] = &ipc.GetStepDetailResponse{
		Step:     2,
		Action:   "tool_call",
		ToolPath: "/dev/shell",
		Summary:  "run go build ./...",
	}

	output := m.renderTimelinePane(120, 20)

	// Step 2 should show: 2 ▸ /dev/shell · run go build ./...
	if !strings.Contains(output, "2 ▸") {
		t.Errorf("AC-1: step 2 line should contain '2 ▸', got:\n%s", output)
	}
	if !strings.Contains(output, "/dev/shell") {
		t.Errorf("AC-1: step 2 line should contain '/dev/shell', got:\n%s", output)
	}
	if !strings.Contains(output, "run go build") {
		t.Errorf("AC-1: step 2 should show detail summary 'run go build', got:\n%s", output)
	}
}

func TestATDD_36_3_AC1_DefaultLine_DurationVisible(t *testing.T) {
	m := newTimeline36_3Model()
	m.width = 120

	output := m.renderTimelinePane(120, 20)

	// Duration should be visible at wide widths
	if !strings.Contains(output, "50ms") && !strings.Contains(output, "120ms") {
		t.Errorf("AC-1: default line should show duration at width=120, got:\n%s", output)
	}
}

// ---------------------------------------------------------------------------
// AC-2: Degraded display when detail not loaded
// ---------------------------------------------------------------------------

func TestATDD_36_3_AC2_DegradedDisplay_NoDetail(t *testing.T) {
	m := newTimeline36_3Model()
	// No detail cache populated - should fallback to StepSummaryWire fields

	output := m.renderTimelinePane(120, 20)

	// Fallback: should show summary from StepSummaryWire
	if !strings.Contains(output, "read config") {
		t.Errorf("AC-2: degraded line should show StepSummaryWire summary 'read config', got:\n%s", output)
	}
	// Fallback: should show ToolPath as action
	if !strings.Contains(output, "/dev/fs") {
		t.Errorf("AC-2: degraded line should show ToolPath '/dev/fs' as action, got:\n%s", output)
	}
}

func TestATDD_36_3_AC2_DegradedDisplay_FallbackAction(t *testing.T) {
	m := newTimeline36_3Model()
	// Step 3 has Action="plan" but no ToolPath — should show action name

	output := m.renderTimelinePane(120, 20)

	if !strings.Contains(output, "plan") {
		t.Errorf("AC-2: step 3 (plan) should show action 'plan', got:\n%s", output)
	}
	if !strings.Contains(output, "planning next step") {
		t.Errorf("AC-2: step 3 should show summary 'planning next step', got:\n%s", output)
	}
}

// ---------------------------------------------------------------------------
// AC-3: Consecutive tool aggregation
// ---------------------------------------------------------------------------

func TestATDD_36_3_AC3_BuildToolAggGroups_ThreeConsecutive(t *testing.T) {
	entries := make([]UnifiedEvent, 4)
	steps := []stepEntry{
		{summary: ipc.StepSummaryWire{Step: 1, ToolPath: "/dev/shell"}},
		{summary: ipc.StepSummaryWire{Step: 2, ToolPath: "/dev/shell"}},
		{summary: ipc.StepSummaryWire{Step: 3, ToolPath: "/dev/shell"}},
		{summary: ipc.StepSummaryWire{Step: 4, ToolPath: "/dev/fs"}},
	}
	for i := range steps {
		entries[i] = UnifiedEvent{Type: EventStep, StepEntry: &steps[i]}
	}

	groups := buildToolAggGroups(entries)

	if len(groups) != 1 {
		t.Fatalf("AC-3: expected 1 aggregation group, got %d", len(groups))
	}
	g := groups[0]
	if g.toolPath != "/dev/shell" {
		t.Errorf("AC-3: group toolPath = %q, want '/dev/shell'", g.toolPath)
	}
	if len(g.stepNums) != 3 {
		t.Errorf("AC-3: group should contain 3 steps, got %d", len(g.stepNums))
	}
}

func TestATDD_36_3_AC3_BuildToolAggGroups_BelowThreshold(t *testing.T) {
	entries := make([]UnifiedEvent, 3)
	steps := []stepEntry{
		{summary: ipc.StepSummaryWire{Step: 1, ToolPath: "/dev/shell"}},
		{summary: ipc.StepSummaryWire{Step: 2, ToolPath: "/dev/shell"}},
		{summary: ipc.StepSummaryWire{Step: 3, ToolPath: "/dev/fs"}},
	}
	for i := range steps {
		entries[i] = UnifiedEvent{Type: EventStep, StepEntry: &steps[i]}
	}

	groups := buildToolAggGroups(entries)

	if len(groups) != 0 {
		t.Errorf("AC-3: 2 consecutive same ToolPath should NOT aggregate, got %d groups", len(groups))
	}
}

func TestATDD_36_3_AC3_BuildToolAggGroups_InterruptedRun(t *testing.T) {
	steps := []stepEntry{
		{summary: ipc.StepSummaryWire{Step: 1, ToolPath: "/dev/shell"}},
		{summary: ipc.StepSummaryWire{Step: 2, ToolPath: "/dev/shell"}},
		{summary: ipc.StepSummaryWire{Step: 3, ToolPath: "/dev/shell"}},
		{summary: ipc.StepSummaryWire{Step: 4, ToolPath: "/dev/fs"}},
		{summary: ipc.StepSummaryWire{Step: 5, ToolPath: "/dev/shell"}},
		{summary: ipc.StepSummaryWire{Step: 6, ToolPath: "/dev/shell"}},
		{summary: ipc.StepSummaryWire{Step: 7, ToolPath: "/dev/shell"}},
	}
	entries := make([]UnifiedEvent, len(steps))
	for i := range steps {
		entries[i] = UnifiedEvent{Type: EventStep, StepEntry: &steps[i]}
	}

	groups := buildToolAggGroups(entries)

	if len(groups) != 2 {
		t.Fatalf("AC-3: interrupted run should form 2 groups, got %d", len(groups))
	}
	if groups[0].stepNums[0] != 1 || groups[0].stepNums[2] != 3 {
		t.Errorf("AC-3: first group should be steps 1-3, got %v", groups[0].stepNums)
	}
	if groups[1].stepNums[0] != 5 || groups[1].stepNums[2] != 7 {
		t.Errorf("AC-3: second group should be steps 5-7, got %v", groups[1].stepNums)
	}
}

func TestATDD_36_3_AC3_AggregatedDisplay_ShowsMultiplier(t *testing.T) {
	m := newTimeline36_3AggModel()

	output := m.renderTimelinePane(120, 30)

	// Collapsed group should show "× 5" for the 5 consecutive shell steps
	if !strings.Contains(output, "× 5") {
		t.Errorf("AC-3: aggregated group should show '× 5', got:\n%s", output)
	}
	// Group header should contain the tool path
	if !strings.Contains(output, "/dev/shell") {
		t.Errorf("AC-3: aggregated group header should contain '/dev/shell', got:\n%s", output)
	}
}

func TestATDD_36_3_AC3_AggregatedDisplay_StepRange(t *testing.T) {
	m := newTimeline36_3AggModel()

	output := m.renderTimelinePane(120, 30)

	// Group header should show step range "1–5"
	if !strings.Contains(output, "1–5") {
		t.Errorf("AC-3: aggregated group header should show step range '1–5', got:\n%s", output)
	}
}

// ---------------------------------------------------------------------------
// AC-4: Aggregation expand
// ---------------------------------------------------------------------------

func TestATDD_36_3_AC4_ExpandedGroup_ShowsSubSteps(t *testing.T) {
	m := newTimeline36_3AggModel()
	// Expand the group (key = first step number = 1)
	m.expandedAggGroups[1] = true

	output := m.renderTimelinePane(120, 30)

	// When expanded, individual steps should be visible
	if !strings.Contains(output, "ls ~/project") {
		t.Errorf("AC-4: expanded group should show sub-step 'ls ~/project', got:\n%s", output)
	}
	if !strings.Contains(output, "cat /etc/hosts") {
		t.Errorf("AC-4: expanded group should show sub-step 'cat /etc/hosts', got:\n%s", output)
	}
	if !strings.Contains(output, "git status") {
		t.Errorf("AC-4: expanded group should show sub-step 'git status', got:\n%s", output)
	}
}

func TestATDD_36_3_AC4_CollapsedGroup_HidesSubSteps(t *testing.T) {
	m := newTimeline36_3AggModel()
	// Default: collapsed

	output := m.renderTimelinePane(120, 30)

	// When collapsed, individual sub-step summaries should NOT appear
	// But the summary for "ls ~/project" might appear if it's just the fallback...
	// Actually, collapsed group shows only the header line.
	// The individual step "cat /etc/hosts" should NOT be visible
	// (step 1 might show since it's the group start, but cat is step 2)
	if strings.Contains(output, "cat /etc/hosts") {
		t.Errorf("AC-4: collapsed group should NOT show sub-step 'cat /etc/hosts', got:\n%s", output)
	}
}

// ---------------------------------------------------------------------------
// AC-5: Aggregation coexists with bulk aggregation
// ---------------------------------------------------------------------------

func TestATDD_36_3_AC5_BulkAggregation_NotAffected(t *testing.T) {
	// Semantic aggregation only activates when stepCount <= 100
	// Verify that >100 steps still uses bulk aggregation path
	m := newDashboardModel(nil)
	m.width = 120
	m.height = 40
	m.connected = true
	m.selectedPID = 1
	m.activePane = paneTimeline

	// Create 110 steps
	now := time.Now()
	for i := range 110 {
		entry := stepEntry{
			summary: ipc.StepSummaryWire{
				Step:     i + 1,
				Action:   "tool_call",
				Summary:  "step operation",
				ToolPath: "/dev/shell",
			},
		}
		m.stepEntries = append(m.stepEntries, entry)
	}
	for i := range m.stepEntries {
		m.unifiedEvents = append(m.unifiedEvents, UnifiedEvent{
			Type:      EventStep,
			Timestamp: now.Add(time.Duration(i) * time.Second),
			PID:       1,
			Summary:   m.stepEntries[i].summary.Summary,
			StepEntry: &m.stepEntries[i],
		})
	}
	m.stepDetailCache = make(map[int]*ipc.GetStepDetailResponse)
	m.expandedAggGroups = make(map[int]bool)

	output := m.renderTimelinePane(120, 30)

	// Bulk aggregation shows "Steps X-Y:" pattern
	if !strings.Contains(output, "Steps") {
		t.Errorf("AC-5: >100 steps should use bulk aggregation with 'Steps' label, got:\n%s", output)
	}
	// Should NOT show semantic aggregation "×" pattern (that's for <100 steps)
	// (bulk aggregation doesn't use ×)
}

// ---------------------------------------------------------------------------
// shortenArgs tests
// ---------------------------------------------------------------------------

func TestATDD_36_3_ShortenArgs_LongInput(t *testing.T) {
	input := "this is a very long argument string that should be truncated at the specified maximum length"
	result := shortenArgs(input, 20)

	if runewidth.StringWidth(result) > 20 {
		t.Errorf("shortenArgs: result rune-width %d exceeds maxLen 20: %q", runewidth.StringWidth(result), result)
	}
	if !strings.Contains(result, "…") {
		t.Errorf("shortenArgs: truncated result should contain ellipsis, got: %q", result)
	}
}

func TestATDD_36_3_ShortenArgs_ShortInput(t *testing.T) {
	input := "short"
	result := shortenArgs(input, 20)

	if result != "short" {
		t.Errorf("shortenArgs: short input should be unchanged, got: %q", result)
	}
}

func TestATDD_36_3_ShortenArgs_MultiLine(t *testing.T) {
	input := "first line\nsecond line\nthird line"
	result := shortenArgs(input, 60)

	if result != "first line" {
		t.Errorf("shortenArgs: multiline should return first line only, got: %q", result)
	}
}

// ---------------------------------------------------------------------------
// formatDefaultLine tests
// ---------------------------------------------------------------------------

func TestATDD_36_3_FormatDefaultLine_WithDetail(t *testing.T) {
	s := ipc.StepSummaryWire{Step: 1, Action: "tool_call", Summary: "fallback", ToolPath: "/dev/fs"}
	detail := &ipc.GetStepDetailResponse{
		Step:     1,
		Action:   "tool_call",
		ToolPath: "/dev/fs",
		Summary:  "read main.go from disk",
	}

	action, summary := formatDefaultLine(s, detail)

	if action != "/dev/fs" {
		t.Errorf("formatDefaultLine: action = %q, want '/dev/fs'", action)
	}
	if summary != "read main.go from disk" {
		t.Errorf("formatDefaultLine: summary = %q, want 'read main.go from disk'", summary)
	}
}

func TestATDD_36_3_FormatDefaultLine_WithoutDetail(t *testing.T) {
	s := ipc.StepSummaryWire{Step: 1, Action: "tool_call", Summary: "run build", ToolPath: "/dev/shell"}

	action, summary := formatDefaultLine(s, nil)

	if action != "/dev/shell" {
		t.Errorf("formatDefaultLine: action = %q, want '/dev/shell'", action)
	}
	if summary != "run build" {
		t.Errorf("formatDefaultLine: summary = %q, want 'run build'", summary)
	}
}

func TestATDD_36_3_FormatDefaultLine_DetailWithToolInput(t *testing.T) {
	s := ipc.StepSummaryWire{Step: 1, Action: "tool_call", Summary: "fallback"}
	detail := &ipc.GetStepDetailResponse{
		Step:      1,
		Action:    "tool_call",
		ToolPath:  "/dev/shell",
		ToolInput: `{"command": "ls -la /home/user/project"}`,
	}

	action, summary := formatDefaultLine(s, detail)

	if action != "/dev/shell" {
		t.Errorf("formatDefaultLine: action = %q, want '/dev/shell'", action)
	}
	// Summary should be derived from ToolInput via shortenArgs
	if summary == "" || summary == "fallback" {
		t.Errorf("formatDefaultLine: summary should derive from ToolInput, got: %q", summary)
	}
}
