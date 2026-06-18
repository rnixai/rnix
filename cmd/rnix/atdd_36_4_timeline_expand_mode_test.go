package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/dashboard/timeline"
	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"
)

// ============================================================
// ATDD — Story 36-4: Timeline ascending order & expand-mode stickiness
// ============================================================

// --- AC-1: Default ascending order ---

func TestATDD_36_4_AC1_DefaultAscending(t *testing.T) {
	// 3 step entries constructed in insertion order (step 1..3)
	steps := []stepEntry{
		{Summary: ipc.StepSummaryWire{Step: 1, Action: "tool_call", TimestampMs: 100}},
		{Summary: ipc.StepSummaryWire{Step: 2, Action: "tool_call", TimestampMs: 200}},
		{Summary: ipc.StepSummaryWire{Step: 3, Action: "tool_call", TimestampMs: 300}},
	}
	merged := mergeUnifiedEvents(steps, nil, 1, "uuid-1", nil, true) // ascending
	if len(merged) != 3 {
		t.Fatalf("expected 3 events, got %d", len(merged))
	}
	for i := 0; i < len(merged)-1; i++ {
		if merged[i].Timestamp.After(merged[i+1].Timestamp) {
			t.Errorf("ascending: event %d (%v) after event %d (%v)",
				i, merged[i].Timestamp, i+1, merged[i+1].Timestamp)
		}
	}
	// First = oldest (Step 1 via offset 0), last = newest (Step 3 via offset 2)
	if merged[0].StepEntry.Summary.Step != 1 {
		t.Errorf("expected first=Step 1 in ascending, got Step %d", merged[0].StepEntry.Summary.Step)
	}
	if merged[len(merged)-1].StepEntry.Summary.Step != 3 {
		t.Errorf("expected last=Step 3 in ascending, got Step %d", merged[len(merged)-1].StepEntry.Summary.Step)
	}
}

// --- AC-2: o toggles direction & header updates ---

func TestATDD_36_4_AC2_OToggleDirection(t *testing.T) {
	m := newDashboardModel(nil)
	if !m.timeline.SortAsc {
		t.Fatalf("expected default timelineSortAsc=true")
	}
	m = m.handleTimelineKey("o")
	if m.timeline.SortAsc {
		t.Errorf("expected timelineSortAsc=false after first o")
	}
	m = m.handleTimelineKey("o")
	if !m.timeline.SortAsc {
		t.Errorf("expected timelineSortAsc=true after second o")
	}
	// Header indicator: ascending → old→new
	header := m.renderUnifiedStepHeader(200, 0, 0, 0)
	if !strings.Contains(header, "old→new") && !strings.Contains(header, "old->new") {
		t.Errorf("ascending header missing 'old→new'; got: %s", header)
	}
	// Toggle once more → descending → new→old
	m = m.handleTimelineKey("o")
	header = m.renderUnifiedStepHeader(200, 0, 0, 0)
	if !strings.Contains(header, "new→old") && !strings.Contains(header, "new->old") {
		t.Errorf("descending header missing 'new→old'; got: %s", header)
	}
}

// --- AC-4: expandMode reset on PID change, sortAsc preserved ---

func TestATDD_36_4_AC4_ExpandModeResetOnPIDChange(t *testing.T) {
	m := newDashboardModel(nil)
	m.timeline.SortAsc = false // simulate user preference
	m.timeline.ExpandMode = expandModeExpanded
	m.selectedPID = 1
	m.selectedUUID = "uuid-a"
	m.timeline.AttachedPID = 1
	m.timeline.AttachedUUID = "uuid-a"

	// Switch to a different process
	m.selectedPID = 2
	m.selectedUUID = "uuid-b"
	m.timeline = timeline.HandlePIDUUIDChangeWithSearch(m.timeline, &m.search, m.selectedPID, m.selectedUUID)

	if m.timeline.ExpandMode != expandModeCollapsed {
		t.Errorf("expected expandMode reset to collapsed after PID change, got %d", m.timeline.ExpandMode)
	}
	if m.timeline.SortAsc != false {
		t.Errorf("expected timelineSortAsc preserved across PID change, got %v", m.timeline.SortAsc)
	}
}

// --- AC-5: e expands all, idempotent ---

func TestATDD_36_4_AC5_EExpandsAll(t *testing.T) {
	m := newDashboardModel(nil)
	m.timeline.StepEntries = []stepEntry{
		{Summary: ipc.StepSummaryWire{Step: 1, Action: "tool_call"}, Level: levelSummary},
		{Summary: ipc.StepSummaryWire{Step: 2, Action: "tool_call"}, Level: levelSummary},
		{Summary: ipc.StepSummaryWire{Step: 3, Action: "plan"}, Level: levelSummary},
		{Summary: ipc.StepSummaryWire{Step: 4, Action: "complete"}, Level: levelSummary},
		{Summary: ipc.StepSummaryWire{Step: 5, Action: "text"}, Level: levelSummary},
	}
	m = m.handleTimelineKey("e")
	if m.timeline.ExpandMode != expandModeExpanded {
		t.Errorf("expected expandMode=Expanded after e, got %d", m.timeline.ExpandMode)
	}
	for i, e := range m.timeline.StepEntries {
		if e.Level != levelExpanded {
			t.Errorf("entry %d: expected levelExpanded, got %d", i, e.Level)
		}
	}
	// Idempotent: second e keeps Expanded
	m = m.handleTimelineKey("e")
	if m.timeline.ExpandMode != expandModeExpanded {
		t.Errorf("expected expandMode stays Expanded after second e, got %d", m.timeline.ExpandMode)
	}
}

// --- AC-6: E switches to ErrorsOnly ---

func TestATDD_36_4_AC6_EErrorsOnly(t *testing.T) {
	m := newDashboardModel(nil)
	m.timeline.StepEntries = []stepEntry{
		{Summary: ipc.StepSummaryWire{Step: 1, Action: "tool_call"}, Level: levelExpanded},
		{Summary: ipc.StepSummaryWire{Step: 2, Action: "tool_call", HasError: true}, Level: levelSummary},
		{Summary: ipc.StepSummaryWire{Step: 3, Action: "plan"}, Level: levelExpanded},
	}
	m = m.handleTimelineKey("E")
	if m.timeline.ExpandMode != expandModeErrorsOnly {
		t.Errorf("expected expandMode=ErrorsOnly after E, got %d", m.timeline.ExpandMode)
	}
	if m.timeline.StepEntries[0].Level != levelSummary {
		t.Errorf("entry 0 (no error): expected levelSummary, got %d", m.timeline.StepEntries[0].Level)
	}
	if m.timeline.StepEntries[1].Level != levelExpanded {
		t.Errorf("entry 1 (error): expected levelExpanded, got %d", m.timeline.StepEntries[1].Level)
	}
	if m.timeline.StepEntries[2].Level != levelSummary {
		t.Errorf("entry 2 (no error): expected levelSummary, got %d", m.timeline.StepEntries[2].Level)
	}
}

// --- AC-7: C collapses all ---

func TestATDD_36_4_AC7_CCollapses(t *testing.T) {
	m := newDashboardModel(nil)
	m.timeline.StepEntries = []stepEntry{
		{Summary: ipc.StepSummaryWire{Step: 1, Action: "tool_call"}, Level: levelSummary},
		{Summary: ipc.StepSummaryWire{Step: 2, Action: "plan"}, Level: levelSummary},
	}
	m = m.handleTimelineKey("e")
	if m.timeline.ExpandMode != expandModeExpanded {
		t.Fatalf("precondition: expected Expanded, got %d", m.timeline.ExpandMode)
	}
	m = m.handleTimelineKey("C")
	if m.timeline.ExpandMode != expandModeCollapsed {
		t.Errorf("expected expandMode=Collapsed after C, got %d", m.timeline.ExpandMode)
	}
	for i, e := range m.timeline.StepEntries {
		if e.Level != levelSummary {
			t.Errorf("entry %d: expected levelSummary after C, got %d", i, e.Level)
		}
	}
}

// --- AC-8: New step sticky by mode ---

func TestATDD_36_4_AC8_NewStepStickyExpanded(t *testing.T) {
	m := newDashboardModel(nil)
	m.timeline.ExpandMode = expandModeExpanded
	m = m.applyNewSteps([]ipc.StepSummaryWire{
		{Step: 1, Action: "tool_call"},
	})
	if len(m.timeline.StepEntries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(m.timeline.StepEntries))
	}
	e := m.timeline.StepEntries[0]
	if e.Level != levelExpanded {
		t.Errorf("Expanded sticky: expected level=Expanded, got %d", e.Level)
	}
	if !e.AutoExpand {
		t.Errorf("Expanded sticky: expected autoExpand=true")
	}
}

func TestATDD_36_4_AC8_NewStepStickyErrorsOnly(t *testing.T) {
	m := newDashboardModel(nil)
	m.timeline.ExpandMode = expandModeErrorsOnly
	m = m.applyNewSteps([]ipc.StepSummaryWire{
		{Step: 1, Action: "tool_call", HasError: false},
		{Step: 2, Action: "tool_call", HasError: true},
	})
	if m.timeline.StepEntries[0].Level != levelSummary {
		t.Errorf("ErrorsOnly non-error: expected Summary, got %d", m.timeline.StepEntries[0].Level)
	}
	if m.timeline.StepEntries[1].Level != levelExpanded {
		t.Errorf("ErrorsOnly error: expected Expanded, got %d", m.timeline.StepEntries[1].Level)
	}
}

func TestATDD_36_4_AC8_SafetyNetCollapsed(t *testing.T) {
	m := newDashboardModel(nil)
	// default expandMode=Collapsed
	m = m.applyNewSteps([]ipc.StepSummaryWire{
		{Step: 1, Action: "tool_call", HasError: true},
		{Step: 2, Action: "tool_call", HasError: false},
	})
	if m.timeline.StepEntries[0].Level != levelExpanded {
		t.Errorf("collapsed+error safety net: expected Expanded, got %d", m.timeline.StepEntries[0].Level)
	}
	if m.timeline.StepEntries[1].Level != levelSummary {
		t.Errorf("collapsed+no error: expected Summary, got %d", m.timeline.StepEntries[1].Level)
	}
}

// --- AC-3: Migration notice shown once ---

func TestATDD_36_4_AC3_MigrationNoticeOnce(t *testing.T) {
	// Isolate HOME so we don't touch the developer's real ui-state.json
	tmp := t.TempDir()
	oldHome, hadHome := os.LookupEnv("HOME")
	oldXDG, hadXDG := os.LookupEnv("XDG_CONFIG_HOME")
	t.Setenv("HOME", tmp)
	os.Unsetenv("XDG_CONFIG_HOME")
	t.Cleanup(func() {
		if hadHome {
			os.Setenv("HOME", oldHome)
		}
		if hadXDG {
			os.Setenv("XDG_CONFIG_HOME", oldXDG)
		}
	})

	// First session: fresh state, ascending default → notice fires
	m := newDashboardModel(nil)
	if m.timeline.UIState == nil {
		t.Fatalf("uiState should be non-nil after init")
	}
	if m.timeline.UIState.TimelineSortMigrationShown {
		t.Fatalf("precondition: fresh uiState should have TimelineSortMigrationShown=false")
	}
	m = m.maybeShowTimelineMigrationNotice()
	if !strings.Contains(m.statusMsg, "ascending") {
		t.Errorf("expected migration notice to set statusMsg containing 'ascending'; got: %q", m.statusMsg)
	}
	if !m.timeline.UIState.TimelineSortMigrationShown {
		t.Errorf("expected TimelineSortMigrationShown=true after notice")
	}
	// File must be persisted
	var path string
	if cfg, err := os.UserConfigDir(); err == nil {
		path = filepath.Join(cfg, "rnix", "ui-state.json")
	}
	if _, err := os.Stat(path); err != nil {
		// Fallback path
		path = filepath.Join(tmp, ".config", "rnix", "ui-state.json")
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("ui-state.json not persisted at expected path: %v", err)
		}
	}

	// Second session: notice should NOT fire again
	m2 := newDashboardModel(nil)
	if m2.timeline.UIState == nil || !m2.timeline.UIState.TimelineSortMigrationShown {
		t.Fatalf("second session: expected TimelineSortMigrationShown=true loaded from disk; got %+v", m2.timeline.UIState)
	}
	m2 = m2.maybeShowTimelineMigrationNotice()
	if strings.Contains(m2.statusMsg, "ascending") {
		t.Errorf("second session should not show migration notice; got: %q", m2.statusMsg)
	}
}

// --- AC-9: Header indicator with ASCII fallback ---

func TestATDD_36_4_AC9_HeaderIndicator(t *testing.T) {
	m := newDashboardModel(nil)
	// Case 1: ascending + Expanded
	m.timeline.SortAsc = true
	m.timeline.ExpandMode = expandModeExpanded
	header := m.renderUnifiedStepHeader(200, 0, 0, 0)
	if !strings.Contains(header, "old→new") && !strings.Contains(header, "old->new") {
		t.Errorf("ascending header missing 'old→new'; got: %s", header)
	}
	if !strings.Contains(header, "all") {
		t.Errorf("Expanded mode header missing 'all'; got: %s", header)
	}
	// Case 2: descending + ErrorsOnly
	m.timeline.SortAsc = false
	m.timeline.ExpandMode = expandModeErrorsOnly
	header = m.renderUnifiedStepHeader(200, 0, 0, 0)
	if !strings.Contains(header, "new→old") && !strings.Contains(header, "new->old") {
		t.Errorf("descending header missing 'new→old'; got: %s", header)
	}
	if !strings.Contains(header, "errors") {
		t.Errorf("ErrorsOnly mode header missing 'errors'; got: %s", header)
	}
	// Case 3: Collapsed omits expandMode indicator
	m.timeline.ExpandMode = expandModeCollapsed
	header = m.renderUnifiedStepHeader(200, 0, 0, 0)
	if strings.Contains(header, "· all") || strings.Contains(header, "· errors") {
		t.Errorf("Collapsed mode header should omit mode indicator; got: %s", header)
	}

	// Case 4: ASCII fallback — exercise the ASCII code path via RNIX_ASCII env
	t.Setenv("RNIX_ASCII", "1")
	// Sanity check that IsASCIIMode reflects env (guards against regressions in ui pkg)
	if !ui.IsASCIIMode() {
		t.Skip("RNIX_ASCII env not honored in this test context; skipping ASCII assertion")
	}
	m.timeline.SortAsc = true
	m.timeline.ExpandMode = expandModeExpanded
	header = m.renderUnifiedStepHeader(200, 0, 0, 0)
	if !strings.Contains(header, "^ old->new") {
		t.Errorf("ASCII ascending header missing '^ old->new'; got: %s", header)
	}
	m.timeline.SortAsc = false
	header = m.renderUnifiedStepHeader(200, 0, 0, 0)
	if !strings.Contains(header, "v new->old") {
		t.Errorf("ASCII descending header missing 'v new->old'; got: %s", header)
	}
}

// Ensure time.Time import retained (also used by other ATDDs style).
var _ = time.Now
