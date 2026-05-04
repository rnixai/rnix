package main

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/bubbles/v2/viewport"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"
)

// ============================================================
// ATDD — Story 36-6: Inspector 进阶能力（Diff / Search 强化 / Follow live）
// ============================================================

// ---- helpers ----

// newInspectorModelWithSteps builds a dashboard model pre-positioned in the
// Step Inspector with the supplied step summaries and the cursor at the last
// step. The active lens is Conversation with initial content set on the
// viewport so inspectorKey can scroll it.
func newInspectorModelWithSteps(t *testing.T, steps []ipc.StepSummaryWire, cur int, state types.ProcessState) dashboardModel {
	t.Helper()
	m := newTestDashboardModel(mockDashboardProcs())
	m.viewMode = viewStepInspector
	m.inspector.PID = 2
	m.inspector.UUID = "uuid-mock-002"
	m.inspector.Lens = lensConversation
	m.inspector.Steps = steps
	if len(steps) > 0 {
		m.inspector.StepMax = steps[len(steps)-1].Step
	}
	m.inspector.Step = cur
	// Give the active lens a viewport so scroll keys don't crash.
	vp := viewport.New(viewport.WithHeight(10), viewport.WithWidth(40))
	vp.SetContent("line 1\nline 2\nline 3\n")
	m.inspector.Viewports[lensConversation] = vp
	// Place the target PID into the process table with the requested state so
	// Follow live state checks behave.
	found := false
	for i := range m.processes {
		if m.processes[i].PID == m.inspector.PID {
			m.processes[i].State = state
			m.processes[i].UUID = m.inspector.UUID
			found = true
		}
	}
	if !found {
		t.Fatalf("mockDashboardProcs missing PID %d", m.inspector.PID)
	}
	return m
}

// makeDetail returns a minimal GetStepDetailResponse whose Conversation Lens
// renders to the supplied body so the line-level diff is driven by body alone.
func makeDetail(step int, body string) *ipc.GetStepDetailResponse {
	return &ipc.GetStepDetailResponse{
		Step:   step,
		Action: "text",
		Messages: []ipc.MessageWire{
			{Role: "assistant", Content: body},
		},
		MessageCount: 1,
	}
}

// --- AC-1: Enter diff mode ---

func TestATDD_36_6_AC1_EnterDiff(t *testing.T) {
	steps := []ipc.StepSummaryWire{{Step: 1}, {Step: 2}, {Step: 3}}
	m := newInspectorModelWithSteps(t, steps, 3, types.StateRunning)
	m.inspector.Detail = makeDetail(3, "alpha\nbeta\ngamma")
	m.inspector.PrevDetail = makeDetail(2, "alpha\nbeta\ngammA")
	m.inspector.PrevStep = 2
	m.timeline.StepDetailCache = map[int]*ipc.GetStepDetailResponse{
		2: m.inspector.PrevDetail,
		3: m.inspector.Detail,
	}

	m2, _ := m.inspectorKey(tea.KeyPressMsg{Code: 'd', Text: "d"})
	mm := m2.(dashboardModel)

	if !mm.inspector.DiffMode {
		t.Fatal("inspectorDiffMode should be true after `d`")
	}
	if mm.inspector.DiffBase != 2 {
		t.Errorf("default base should be prev step = 2, got %d", mm.inspector.DiffBase)
	}
	if mm.inspector.DiffDelta != 1 {
		t.Errorf("captured delta should be 1 (cur-base), got %d", mm.inspector.DiffDelta)
	}
	footer := mm.renderInspectorFooter()
	if !strings.Contains(footer, "Diff: step 3 vs 2") {
		t.Errorf("footer should show `Diff: step 3 vs 2`, got %q", footer)
	}
	rail := mm.renderStepRail(200)
	if !strings.Contains(rail, "vs #2") {
		t.Errorf("step rail should show `vs #2`, got %q", rail)
	}
}

// --- AC-2: Diff renders +/-/ prefixes + fold ---

func TestATDD_36_6_AC2_DiffRender(t *testing.T) {
	// 6-line run of equals >= 3 → folded placeholder.
	base := []string{"eq1", "eq2", "eq3", "eq4", "eq5", "old"}
	cur := []string{"eq1", "eq2", "eq3", "eq4", "eq5", "new"}
	lines := computeLineDiff(base, cur)

	var adds, dels, eqs int
	for _, l := range lines {
		switch l.kind {
		case diffAdd:
			adds++
		case diffDel:
			dels++
		case diffEqual:
			eqs++
		}
	}
	if adds != 1 || dels != 1 {
		t.Errorf("expected 1 add + 1 del, got %d/%d", adds, dels)
	}
	if eqs != 5 {
		t.Errorf("expected 5 equal lines, got %d", eqs)
	}

	// Ascii render: + / - / space prefixes and a fold placeholder.
	out := renderDiff(lines, nil, true)
	if !strings.Contains(out, "-old") {
		t.Errorf("expected `-old` line, got %q", out)
	}
	if !strings.Contains(out, "+new") {
		t.Errorf("expected `+new` line, got %q", out)
	}
	if !strings.Contains(out, "... 5 unchanged lines") {
		t.Errorf("expected fold placeholder, got %q", out)
	}

	// Unfolded → 5 equal lines visible, no fold placeholder.
	out2 := renderDiff(lines, map[int]bool{0: true}, true)
	if strings.Contains(out2, "unchanged lines") {
		t.Errorf("unfolded output should not contain fold placeholder: %q", out2)
	}
	if !strings.Contains(out2, " eq3") {
		t.Errorf("unfolded output should show eq3, got %q", out2)
	}
}

// --- AC-3: dd base picker ---

func TestATDD_36_6_AC3_DdPickBase(t *testing.T) {
	steps := []ipc.StepSummaryWire{{Step: 1}, {Step: 2}, {Step: 3}, {Step: 4}}
	m := newInspectorModelWithSteps(t, steps, 4, types.StateRunning)
	m.inspector.Detail = makeDetail(4, "x")
	m.inspector.PrevDetail = makeDetail(3, "y")
	m.inspector.PrevStep = 3
	m.timeline.StepDetailCache = map[int]*ipc.GetStepDetailResponse{
		3: m.inspector.PrevDetail,
		4: m.inspector.Detail,
	}

	// First `d` enters diff mode; second `d` within 200ms opens picker.
	m2, _ := m.inspectorKey(tea.KeyPressMsg{Code: 'd', Text: "d"})
	mm := m2.(dashboardModel)
	if !mm.inspector.DiffMode {
		t.Fatal("diff mode should be on after first d")
	}
	m3, _ := mm.inspectorKey(tea.KeyPressMsg{Code: 'd', Text: "d"})
	mm = m3.(dashboardModel)
	if !mm.inspector.DiffPicker {
		t.Fatal("picker should be open after dd")
	}

	// Move left twice → cursor should be at index 1 (cursor starts at base=3 → idx 2).
	m4, _ := mm.inspectorKey(tea.KeyPressMsg{Code: 'h', Text: "h"})
	mm = m4.(dashboardModel)
	if mm.inspector.DiffPickerCursor != 1 {
		t.Errorf("cursor after h should be 1; got %d", mm.inspector.DiffPickerCursor)
	}

	// Enter → base becomes step 2.
	m5, _ := mm.inspectorKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	mm = m5.(dashboardModel)
	if mm.inspector.DiffPicker {
		t.Error("picker should close on Enter")
	}
	if mm.inspector.DiffBase != 2 {
		t.Errorf("base after pick should be 2; got %d", mm.inspector.DiffBase)
	}
}

// --- AC-4: Esc / single d exit diff ---

func TestATDD_36_6_AC4_ExitDiff(t *testing.T) {
	steps := []ipc.StepSummaryWire{{Step: 1}, {Step: 2}}
	m := newInspectorModelWithSteps(t, steps, 2, types.StateRunning)
	m.inspector.Detail = makeDetail(2, "a")
	m.inspector.PrevDetail = makeDetail(1, "b")
	m.inspector.PrevStep = 1
	m.timeline.StepDetailCache = map[int]*ipc.GetStepDetailResponse{1: m.inspector.PrevDetail, 2: m.inspector.Detail}

	// Enter diff via d.
	m2, _ := m.inspectorKey(tea.KeyPressMsg{Code: 'd', Text: "d"})
	mm := m2.(dashboardModel)
	if !mm.inspector.DiffMode {
		t.Fatal("expected diff mode on")
	}

	// Esc (no picker, no search) → exits diff.
	m3, _ := mm.inspectorKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	mm = m3.(dashboardModel)
	if mm.inspector.DiffMode {
		t.Error("Esc should exit diff mode")
	}
	if mm.inspector.DiffBase != 0 || mm.inspector.DiffDelta != 0 {
		t.Errorf("diff fields should reset; base=%d delta=%d", mm.inspector.DiffBase, mm.inspector.DiffDelta)
	}

	// Re-enter diff, then simulate a lone `d` after the dd window expired.
	m4, _ := mm.inspectorKey(tea.KeyPressMsg{Code: 'd', Text: "d"})
	mm = m4.(dashboardModel)
	mm.inspector.DiffDdDeadline = time.Now().Add(-1 * time.Second) // force window to be closed
	m5, _ := mm.inspectorKey(tea.KeyPressMsg{Code: 'd', Text: "d"})
	mm = m5.(dashboardModel)
	if mm.inspector.DiffMode {
		t.Error("lone `d` after dd window should exit diff mode")
	}
}

// --- AC-5: Diff cross-lens ---

func TestATDD_36_6_AC5_DiffCrossLens(t *testing.T) {
	steps := []ipc.StepSummaryWire{{Step: 1}, {Step: 2}}
	m := newInspectorModelWithSteps(t, steps, 2, types.StateRunning)
	m.inspector.Detail = makeDetail(2, "a")
	m.inspector.PrevDetail = makeDetail(1, "b")
	m.inspector.PrevStep = 1
	m.timeline.StepDetailCache = map[int]*ipc.GetStepDetailResponse{1: m.inspector.PrevDetail, 2: m.inspector.Detail}

	// Enter diff.
	m2, _ := m.inspectorKey(tea.KeyPressMsg{Code: 'd', Text: "d"})
	mm := m2.(dashboardModel)

	// Switch to Lens 2 (System).
	m3, _ := mm.inspectorKey(tea.KeyPressMsg{Code: '2', Text: "2"})
	mm = m3.(dashboardModel)
	if mm.inspector.Lens != lensSystem {
		t.Fatalf("lens should be System after `2`, got %v", mm.inspector.Lens)
	}
	if !mm.inspector.DiffMode {
		t.Error("diff should persist across lens switch")
	}
	if mm.inspector.DiffBase != 1 {
		t.Errorf("diff base should be unchanged (1), got %d", mm.inspector.DiffBase)
	}
}

// --- AC-6: Reverse search `?` ---

func TestATDD_36_6_AC6_ReverseSearch(t *testing.T) {
	m := newTestDashboardModel(mockDashboardProcs())
	m.viewMode = viewStepInspector
	m.inspector.PID = 2
	m.inspector.UUID = "uuid-mock-002"
	m.inspector.Lens = lensConversation
	content := "alpha\nfoo line\nbeta\nfoo again\ngamma"
	m.inspector.Contents[lensConversation] = content
	vp := viewport.New(viewport.WithHeight(10), viewport.WithWidth(40))
	vp.SetContent(content)
	m.inspector.Viewports[lensConversation] = vp

	m2, _ := m.inspectorKey(tea.KeyPressMsg{Code: '?', Text: "?"})
	mm := m2.(dashboardModel)
	if !mm.searchMode {
		t.Fatal("searchMode should be true after `?`")
	}
	if !mm.searchReverse {
		t.Error("searchReverse should be true after `?`")
	}
	for _, c := range "foo" {
		m3, _ := mm.inspectorKey(tea.KeyPressMsg{Code: c, Text: string(c)})
		mm = m3.(dashboardModel)
	}
	m4, _ := mm.inspectorKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	mm = m4.(dashboardModel)
	if mm.searchMode {
		t.Fatal("searchMode should clear after Enter")
	}
	if len(mm.searchMatches) != 2 {
		t.Fatalf("expected 2 matches; got %d", len(mm.searchMatches))
	}
	// Reverse search starts at the last match.
	if mm.searchMatchIdx != 1 {
		t.Errorf("expected matchIdx=1 (last match) on reverse, got %d", mm.searchMatchIdx)
	}
	// `n` in reverse mode jumps to the previous match (idx 0).
	m5, _ := mm.inspectorKey(tea.KeyPressMsg{Code: 'n', Text: "n"})
	mm = m5.(dashboardModel)
	if mm.searchMatchIdx != 0 {
		t.Errorf("reverse `n` should decrement matchIdx; got %d", mm.searchMatchIdx)
	}
}

// --- AC-7: Match X/Y counter ---

func TestATDD_36_6_AC7_MatchCounter(t *testing.T) {
	m := newTestDashboardModel(mockDashboardProcs())
	m.viewMode = viewStepInspector
	m.inspector.PID = 2
	m.inspector.Lens = lensConversation
	m.inspector.Contents[lensConversation] = "foo\nbar\nfoo"
	vp := viewport.New(viewport.WithHeight(10), viewport.WithWidth(40))
	vp.SetContent(m.inspector.Contents[lensConversation])
	m.inspector.Viewports[lensConversation] = vp

	// Forward search for "foo".
	m2, _ := m.inspectorKey(tea.KeyPressMsg{Code: '/'})
	mm := m2.(dashboardModel)
	for _, c := range "foo" {
		m3, _ := mm.inspectorKey(tea.KeyPressMsg{Code: c, Text: string(c)})
		mm = m3.(dashboardModel)
	}
	m4, _ := mm.inspectorKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	mm = m4.(dashboardModel)
	footer := mm.renderInspectorFooter()
	if !strings.Contains(footer, "Match 1/2") {
		t.Errorf("footer should contain `Match 1/2`, got %q", footer)
	}

	// 0-match search: footer gains a "No matches" TTL notice.
	m5, _ := mm.inspectorKey(tea.KeyPressMsg{Code: '/'})
	mm = m5.(dashboardModel)
	for _, c := range "xyz" {
		m6, _ := mm.inspectorKey(tea.KeyPressMsg{Code: c, Text: string(c)})
		mm = m6.(dashboardModel)
	}
	m7, _ := mm.inspectorKey(tea.KeyPressMsg{Code: tea.KeyEnter})
	mm = m7.(dashboardModel)
	footer = mm.renderInspectorFooter()
	if !strings.Contains(footer, "No matches") {
		t.Errorf("footer should show No matches for empty result, got %q", footer)
	}
}

// --- AC-10: Follow live on for active process ---

func TestATDD_36_6_AC10_FollowOn(t *testing.T) {
	steps := []ipc.StepSummaryWire{{Step: 1}, {Step: 2}}
	m := newInspectorModelWithSteps(t, steps, 1, types.StateRunning)
	m.inspector.Detail = makeDetail(1, "a")

	m2, _ := m.inspectorKey(tea.KeyPressMsg{Code: 'F', Text: "F"})
	mm := m2.(dashboardModel)
	if !mm.inspector.FollowLive {
		t.Fatal("Follow live should be ON after `F` on active process")
	}
	if mm.inspector.Step != 2 {
		t.Errorf("expected cursor jump to latest step=2; got %d", mm.inspector.Step)
	}
	rail := mm.renderStepRail(200)
	if !ui.IsASCIIMode() {
		if !strings.Contains(rail, "● FOLLOW") {
			t.Errorf("rail should show `● FOLLOW`; got %q", rail)
		}
	} else {
		if !strings.Contains(rail, "[FOLLOW]") {
			t.Errorf("ASCII rail should show `[FOLLOW]`; got %q", rail)
		}
	}
}

// --- AC-11: Dead process rejects F ---

func TestATDD_36_6_AC11_FollowDeadReject(t *testing.T) {
	steps := []ipc.StepSummaryWire{{Step: 1}, {Step: 2}}
	m := newInspectorModelWithSteps(t, steps, 2, types.StateDead)
	m.inspector.Detail = makeDetail(2, "a")

	m2, _ := m.inspectorKey(tea.KeyPressMsg{Code: 'F', Text: "F"})
	mm := m2.(dashboardModel)
	if mm.inspector.FollowLive {
		t.Error("Follow live must not activate on dead process")
	}
	if !strings.Contains(mm.statusMsg, "Process ended") {
		t.Errorf("status should mention Process ended; got %q", mm.statusMsg)
	}
}

// --- AC-12: New step auto-follow via inspectorStepListMsg ---

func TestATDD_36_6_AC12_FollowAppend(t *testing.T) {
	steps := []ipc.StepSummaryWire{{Step: 1}, {Step: 2}}
	m := newInspectorModelWithSteps(t, steps, 2, types.StateRunning)
	m.inspector.Detail = makeDetail(2, "a")
	m.inspector.FollowLive = true

	// Simulate a new step arriving.
	newSteps := []ipc.StepSummaryWire{{Step: 1}, {Step: 2}, {Step: 3}}
	m2, _ := m.Update(inspectorStepListMsg{pid: m.inspector.PID, uuid: m.inspector.UUID, steps: newSteps})
	mm := m2.(dashboardModel)
	if mm.inspector.Step != 3 {
		t.Errorf("follow should auto-jump to new latest=3; got %d", mm.inspector.Step)
	}
}

// --- AC-13: Back-scroll auto-off Follow ---

func TestATDD_36_6_AC13_FollowAutoOff_BackScroll(t *testing.T) {
	steps := []ipc.StepSummaryWire{{Step: 1}, {Step: 2}, {Step: 3}}
	m := newInspectorModelWithSteps(t, steps, 3, types.StateRunning)
	m.inspector.Detail = makeDetail(3, "a")
	m.inspector.FollowLive = true

	// `k` (back-scroll) → Follow off.
	m2, _ := m.inspectorKey(tea.KeyPressMsg{Code: 'k', Text: "k"})
	mm := m2.(dashboardModel)
	if mm.inspector.FollowLive {
		t.Error("`k` back-scroll must disable Follow live")
	}

	// Reset and verify `h` also disables.
	m.inspector.FollowLive = true
	m3, _ := m.inspectorKey(tea.KeyPressMsg{Code: 'h', Text: "h"})
	mm = m3.(dashboardModel)
	if mm.inspector.FollowLive {
		t.Error("`h` step-back must disable Follow live")
	}
}

// --- AC-13 (variant): Lens switch keeps Follow ---

func TestATDD_36_6_AC13_FollowKeepOnLensSwitch(t *testing.T) {
	steps := []ipc.StepSummaryWire{{Step: 1}, {Step: 2}}
	m := newInspectorModelWithSteps(t, steps, 2, types.StateRunning)
	m.inspector.Detail = makeDetail(2, "a")
	m.inspector.FollowLive = true

	m2, _ := m.inspectorKey(tea.KeyPressMsg{Code: '2', Text: "2"})
	mm := m2.(dashboardModel)
	if !mm.inspector.FollowLive {
		t.Error("switching lens should NOT disable Follow live")
	}
	if mm.inspector.Lens != lensSystem {
		t.Errorf("lens should be System; got %v", mm.inspector.Lens)
	}
}

// --- AC-15: Follow ↔ Diff mutual exclusion ---

func TestATDD_36_6_AC15_FollowDiffMutex(t *testing.T) {
	steps := []ipc.StepSummaryWire{{Step: 1}, {Step: 2}, {Step: 3}}
	m := newInspectorModelWithSteps(t, steps, 3, types.StateRunning)
	m.inspector.Detail = makeDetail(3, "a")
	m.inspector.PrevDetail = makeDetail(2, "b")
	m.inspector.PrevStep = 2
	m.timeline.StepDetailCache = map[int]*ipc.GetStepDetailResponse{2: m.inspector.PrevDetail, 3: m.inspector.Detail}
	m.inspector.FollowLive = true

	// `d` while Follow is on: disables Follow then enters Diff.
	m2, _ := m.inspectorKey(tea.KeyPressMsg{Code: 'd', Text: "d"})
	mm := m2.(dashboardModel)
	if mm.inspector.FollowLive {
		t.Error("entering diff must disable Follow")
	}
	if !mm.inspector.DiffMode {
		t.Error("diff should be on after d")
	}

	// `F` while Diff is on: exits Diff, then enables Follow.
	m3, _ := mm.inspectorKey(tea.KeyPressMsg{Code: 'F', Text: "F"})
	mm = m3.(dashboardModel)
	if mm.inspector.DiffMode {
		t.Error("entering Follow must exit Diff")
	}
	if !mm.inspector.FollowLive {
		t.Error("Follow should be on after F")
	}
}

// --- AC-17: Regression smoke for existing Inspector keys ---

func TestATDD_36_6_AC17_Regression(t *testing.T) {
	steps := []ipc.StepSummaryWire{{Step: 1}, {Step: 3}, {Step: 5}}
	m := newInspectorModelWithSteps(t, steps, 3, types.StateRunning)

	// 1-5 lens switch.
	for i, keyCh := range []rune{'1', '2', '3', '4', '5'} {
		m2, _ := m.inspectorKey(tea.KeyPressMsg{Code: keyCh, Text: string(keyCh)})
		mm := m2.(dashboardModel)
		if int(mm.inspector.Lens) != i {
			t.Errorf("key `%c` should switch to lens %d; got %d", keyCh, i, mm.inspector.Lens)
		}
	}

	// h/l navigate steps (index-based over sparse list).
	m2, _ := m.inspectorKey(tea.KeyPressMsg{Code: 'h', Text: "h"})
	mm := m2.(dashboardModel)
	if mm.inspector.Step != 1 {
		t.Errorf("h should go to step 1; got %d", mm.inspector.Step)
	}
	m3, _ := m.inspectorKey(tea.KeyPressMsg{Code: 'l', Text: "l"})
	mm = m3.(dashboardModel)
	if mm.inspector.Step != 5 {
		t.Errorf("l from step 3 should go to 5; got %d", mm.inspector.Step)
	}

	// L/H end/home.
	m4, _ := m.inspectorKey(tea.KeyPressMsg{Code: 'L', Text: "L"})
	mm = m4.(dashboardModel)
	if mm.inspector.Step != 5 {
		t.Errorf("L should jump to last=5; got %d", mm.inspector.Step)
	}
	m5, _ := m.inspectorKey(tea.KeyPressMsg{Code: 'H', Text: "H"})
	mm = m5.(dashboardModel)
	if mm.inspector.Step != 1 {
		t.Errorf("H should jump to first=1; got %d", mm.inspector.Step)
	}

	// Esc closes inspector when no overlays active.
	m6, _ := m.inspectorKey(tea.KeyPressMsg{Code: tea.KeyEscape})
	mm = m6.(dashboardModel)
	if mm.viewMode == viewStepInspector {
		t.Error("Esc should leave inspector view")
	}
}
