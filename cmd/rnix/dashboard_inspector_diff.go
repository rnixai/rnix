package main

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"
)

// diffKind tags each line in a line-level diff.
type diffKind int

const (
	diffEqual diffKind = iota
	diffAdd
	diffDel
)

// diffLine is a single unified-diff line.
type diffLine struct {
	kind diffKind
	text string
}

const (
	diffFoldThreshold = 3    // consecutive equal lines >= this are folded
	diffMaxLines      = 5000 // refuse to diff beyond this; show "too large" notice
)

// computeLineDiff returns a unified line diff of base→current using the
// standard LCS dynamic-programming algorithm. Output is ordered from the top
// of the two inputs to the bottom, with deletes attached to their base
// position and adds attached to their current position.
//
// Complexity: O(len(base) * len(current)) time and space. Acceptable for
// Lens contents up to a few thousand lines; callers above diffMaxLines
// should render a "content too large" placeholder instead.
func computeLineDiff(base, current []string) []diffLine {
	n, m := len(base), len(current)
	if n == 0 && m == 0 {
		return nil
	}
	if n == 0 {
		out := make([]diffLine, 0, m)
		for _, line := range current {
			out = append(out, diffLine{kind: diffAdd, text: line})
		}
		return out
	}
	if m == 0 {
		out := make([]diffLine, 0, n)
		for _, line := range base {
			out = append(out, diffLine{kind: diffDel, text: line})
		}
		return out
	}

	// LCS DP table
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := 1; i <= n; i++ {
		for j := 1; j <= m; j++ {
			if base[i-1] == current[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else if dp[i-1][j] >= dp[i][j-1] {
				dp[i][j] = dp[i-1][j]
			} else {
				dp[i][j] = dp[i][j-1]
			}
		}
	}

	// Backtrack from (n, m) to (0, 0). Produces reversed output; flip at the end.
	out := make([]diffLine, 0, n+m)
	i, j := n, m
	for i > 0 || j > 0 {
		switch {
		case i > 0 && j > 0 && base[i-1] == current[j-1]:
			out = append(out, diffLine{kind: diffEqual, text: base[i-1]})
			i--
			j--
		case j > 0 && (i == 0 || dp[i][j-1] >= dp[i-1][j]):
			out = append(out, diffLine{kind: diffAdd, text: current[j-1]})
			j--
		default:
			out = append(out, diffLine{kind: diffDel, text: base[i-1]})
			i--
		}
	}
	for a, b := 0, len(out)-1; a < b; a, b = a+1, b-1 {
		out[a], out[b] = out[b], out[a]
	}
	return out
}

// renderDiff formats a sequence of diff lines into a display string. Consecutive
// equal runs of length >= diffFoldThreshold are replaced by a single fold
// placeholder unless the caller has marked that region expanded in `unfolded`
// (keyed by the start-index of the run within `lines`). asciiMode drops
// lipgloss colour styling, keeping the `+ / - / ` prefixes intact.
func renderDiff(lines []diffLine, unfolded map[int]bool, asciiMode bool) string {
	if len(lines) == 0 {
		return ""
	}

	addStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorSuccess))
	delStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError))
	eqStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted))
	foldStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted))
	if asciiMode {
		addStyle = lipgloss.NewStyle()
		delStyle = lipgloss.NewStyle()
		eqStyle = lipgloss.NewStyle()
		foldStyle = lipgloss.NewStyle()
	}

	var b strings.Builder
	i := 0
	for i < len(lines) {
		if lines[i].kind == diffEqual {
			j := i
			for j < len(lines) && lines[j].kind == diffEqual {
				j++
			}
			run := j - i
			if run >= diffFoldThreshold && (unfolded == nil || !unfolded[i]) {
				b.WriteString(foldStyle.Render(fmt.Sprintf("  ... %d unchanged lines (Enter 展开) ...", run)))
				b.WriteString("\n")
			} else {
				for k := i; k < j; k++ {
					b.WriteString(eqStyle.Render(" " + lines[k].text))
					b.WriteString("\n")
				}
			}
			i = j
			continue
		}
		if lines[i].kind == diffAdd {
			b.WriteString(addStyle.Render("+" + lines[i].text))
		} else {
			b.WriteString(delStyle.Render("-" + lines[i].text))
		}
		b.WriteString("\n")
		i++
	}
	return b.String()
}

// renderDiffBasePicker draws a horizontal base-picker overlay listing the
// available step numbers with the current cursor position highlighted.
// width is the available display width; output is a single line.
func renderDiffBasePicker(steps []ipc.StepSummaryWire, cursor int, width int) string {
	if len(steps) == 0 {
		return ""
	}
	if cursor < 0 {
		cursor = 0
	}
	if cursor >= len(steps) {
		cursor = len(steps) - 1
	}

	ascii := ui.IsASCIIMode()
	arrowL, arrowR := "←", "→"
	if ascii {
		arrowL, arrowR = "<", ">"
	}

	activeStyle := lipgloss.NewStyle().Bold(true).Reverse(true)
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted))

	var b strings.Builder
	b.WriteString(dimStyle.Render(" Pick base: "))
	b.WriteString(dimStyle.Render(arrowL + " "))
	for i, s := range steps {
		label := fmt.Sprintf("#%d", s.Step)
		if i == cursor {
			b.WriteString(activeStyle.Render("[" + label + "]"))
		} else {
			b.WriteString(dimStyle.Render(" " + label + " "))
		}
	}
	b.WriteString(dimStyle.Render(" " + arrowR))
	b.WriteString(dimStyle.Render("  Enter=select  Esc=cancel"))

	_ = width
	return b.String()
}

// ddWindow is the inter-tap window within which two `d` presses are treated
// as the `dd` sequence that opens the diff base picker.
const ddWindow = 200 * time.Millisecond

// followLiveTickInterval is the polling cadence for auto-following new steps
// while Follow live is active. Chosen to feel responsive without spamming IPC.
const followLiveTickInterval = 800 * time.Millisecond

// followLiveTickMsg wakes the Update loop so we can refresh the step list and
// schedule the next tick. Follow auto-cancels itself by returning a nil cmd
// when inspectorFollowLive is false at tick time. The `gen` field identifies
// the Follow activation generation — stale ticks scheduled during a previous
// on-period are discarded to avoid tick multiplication under rapid F toggles.
type followLiveTickMsg struct {
	pid  types.PID
	uuid string
	gen  int
}

// followLiveTickCmd schedules a single follow-live tick. Callers should issue
// this only while inspectorFollowLive is true; the handler re-arms the timer.
func followLiveTickCmd(pid types.PID, uuid string, gen int) tea.Cmd {
	return tea.Tick(followLiveTickInterval, func(time.Time) tea.Msg {
		return followLiveTickMsg{pid: pid, uuid: uuid, gen: gen}
	})
}

// handleInspectorDiffKey implements the `d` / `dd` behaviour per Story 36-6
// AC-1, AC-3 and AC-4. Outside of diff mode, the first `d` enters diff mode
// and captures the delta from current step to the previous step as base. In
// diff mode, the second `d` within ddWindow opens the base picker; a lone `d`
// after ddWindow exits diff mode entirely.
func (m dashboardModel) handleInspectorDiffKey() (tea.Model, tea.Cmd) {
	now := time.Now()
	if m.inspector.DiffMode {
		// Within the dd window? open picker. Otherwise, exit diff.
		if !m.inspector.DiffDdDeadline.IsZero() && now.Before(m.inspector.DiffDdDeadline) {
			m.inspector.DiffPicker = true
			m.inspector.DiffPickerCursor = max(m.findStepIndex(m.inspector.DiffBase), 0)
			m.inspector.DiffDdDeadline = time.Time{}
			return m, nil
		}
		// Record a new dd deadline and also exit diff mode on timeout —
		// per AC-4, a lone `d` after the window exits diff mode.
		m = m.exitInspectorDiff()
		return m, nil
	}

	// Story 36-6: Follow ↔ Diff mutual exclusion
	m = m.stopFollowLiveWithStatus()

	// Entering diff mode: base defaults to previous step, delta captured.
	idx := m.findStepIndex(m.inspector.Step)
	if idx < 0 || len(m.inspector.Steps) == 0 {
		m.statusMsg = "No previous step to diff"
		m.statusMsgTTL = statusMsgDefaultTTL
		return m, nil
	}
	base := m.inspector.Step
	if idx > 0 {
		base = m.inspector.Steps[idx-1].Step
	} else {
		m.statusMsg = "No previous step to diff"
		m.statusMsgTTL = statusMsgDefaultTTL
	}
	m.inspector.DiffMode = true
	m.inspector.DiffBase = base
	m.inspector.DiffDelta = m.inspector.Step - base
	m.inspector.DiffUnfolded = make(map[int]bool)
	m.inspector.DiffDdDeadline = now.Add(ddWindow)

	// Story 38-3 AC#7: prime diff-mark cache for tab indicators.
	m.refreshInspectorDiffLensMarks()

	// If base detail is already cached, re-render diff now; otherwise request it.
	cmd := m.ensureDiffBaseDetailCmd()
	m.rebuildInspectorContents()
	return m, cmd
}

// exitInspectorDiff clears all diff-mode state and restores the lens viewport
// to its normal (non-diff) content. Called on Esc from diff mode and on the
// trailing lone `d` that falls outside the dd window.
//
// Per AC-4, the active lens viewport's Y offset is preserved across the exit
// (rebuildInspectorContents normally snaps the active lens to the top).
func (m dashboardModel) exitInspectorDiff() dashboardModel {
	savedYOffset := m.inspector.Viewports[m.inspector.Lens].YOffset()
	m.inspector.DiffMode = false
	m.inspector.DiffBase = 0
	m.inspector.DiffDelta = 0
	m.inspector.DiffUnfolded = nil
	m.inspector.DiffPicker = false
	m.inspector.DiffPickerCursor = 0
	m.inspector.DiffDdDeadline = time.Time{}
	m.rebuildInspectorContents()
	vp := m.inspector.Viewports[m.inspector.Lens]
	vp.SetYOffset(savedYOffset)
	m.inspector.Viewports[m.inspector.Lens] = vp
	return m
}

// slideDiffBase keeps the diff base at the captured delta distance from the
// newly selected current step. If the computed base would fall outside the
// recorded step range, it is clamped and a status-bar notice is shown (per
// AC-5). Caller must have verified m.inspector.DiffMode == true.
func (m dashboardModel) slideDiffBase(newCurrent int) dashboardModel {
	if len(m.inspector.Steps) == 0 {
		return m
	}
	target := newCurrent - m.inspector.DiffDelta
	first := m.inspector.Steps[0].Step
	last := m.inspector.Steps[len(m.inspector.Steps)-1].Step
	if target < first {
		target = first
		m.statusMsg = "Diff base clamped"
		m.statusMsgTTL = statusMsgDefaultTTL
	} else if target > last {
		target = last
		m.statusMsg = "Diff base clamped"
		m.statusMsgTTL = statusMsgDefaultTTL
	}
	m.inspector.DiffBase = target
	// Fold state is index-based over newly computed diff output; reset.
	m.inspector.DiffUnfolded = make(map[int]bool)
	// Story 38-3 AC#7: refresh diff-mark cache for tab indicators.
	m.refreshInspectorDiffLensMarks()
	return m
}

// handleDiffPickerKey handles input while the base-picker overlay is active.
// h/l/←/→ move the cursor, Enter selects the new base, Esc cancels and keeps
// the current base unchanged. Other keys are swallowed.
func (m dashboardModel) handleDiffPickerKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc":
		m.inspector.DiffPicker = false
		return m, nil
	case "h", "left":
		if m.inspector.DiffPickerCursor > 0 {
			m.inspector.DiffPickerCursor--
		}
		return m, nil
	case "l", "right":
		if m.inspector.DiffPickerCursor < len(m.inspector.Steps)-1 {
			m.inspector.DiffPickerCursor++
		}
		return m, nil
	case "enter":
		if m.inspector.DiffPickerCursor >= 0 && m.inspector.DiffPickerCursor < len(m.inspector.Steps) {
			m.inspector.DiffBase = m.inspector.Steps[m.inspector.DiffPickerCursor].Step
			// Capture new delta so subsequent step moves slide correctly.
			m.inspector.DiffDelta = m.inspector.Step - m.inspector.DiffBase
			m.inspector.DiffUnfolded = make(map[int]bool)
			// Story 38-3 AC#7: refresh diff-mark cache for tab indicators.
			m.refreshInspectorDiffLensMarks()
		}
		m.inspector.DiffPicker = false
		cmd := m.ensureDiffBaseDetailCmd()
		m.rebuildInspectorContents()
		return m, cmd
	}
	return m, nil
}

// toggleAllDiffFolds toggles every fold region (runs of diffFoldThreshold+
// consecutive equal lines) between collapsed and expanded in one shot. This
// is a documented simplification of AC-2's per-segment contract (see Dev
// Notes in the story spec): users typically want to expand/collapse all
// unchanged blocks together, and tracking per-segment state across lens
// switches is lens-specific. Callers: Enter key in diff mode.
func (m dashboardModel) toggleAllDiffFolds() dashboardModel {
	if m.inspector.DiffUnfolded == nil {
		m.inspector.DiffUnfolded = make(map[int]bool)
	}
	// Simplest and deterministic: toggle every existing fold region — users
	// usually want to expand/collapse all unchanged blocks at once.
	base := m.lookupDiffBaseDetail()
	if base == nil || m.inspector.Detail == nil {
		return m
	}
	lines := computeLineDiff(
		strings.Split(m.buildFullLensContent(m.inspector.Lens, base, nil), "\n"),
		strings.Split(m.buildFullLensContent(m.inspector.Lens, m.inspector.Detail, m.inspector.PrevDetail), "\n"),
	)
	// Walk lines finding fold starts (runs of >= diffFoldThreshold equal lines)
	// and toggle each one.
	anyFolded := false
	i := 0
	for i < len(lines) {
		if lines[i].kind != diffEqual {
			i++
			continue
		}
		j := i
		for j < len(lines) && lines[j].kind == diffEqual {
			j++
		}
		if j-i >= diffFoldThreshold {
			if !m.inspector.DiffUnfolded[i] {
				anyFolded = true
			}
		}
		i = j
	}
	// Toggle: if at least one is still folded, expand all; else collapse all.
	i = 0
	for i < len(lines) {
		if lines[i].kind != diffEqual {
			i++
			continue
		}
		j := i
		for j < len(lines) && lines[j].kind == diffEqual {
			j++
		}
		if j-i >= diffFoldThreshold {
			m.inspector.DiffUnfolded[i] = anyFolded
		}
		i = j
	}
	m.rebuildInspectorContents()
	return m
}

// lookupDiffBaseDetail returns the cached detail for the current diff base
// step, or nil if we don't have it yet (in which case callers should issue
// ensureDiffBaseDetailCmd and re-render once the fetch completes).
func (m *dashboardModel) lookupDiffBaseDetail() *ipc.GetStepDetailResponse {
	if m.inspector.DiffBase == 0 {
		return nil
	}
	if m.timeline.StepDetailCache != nil {
		if d, ok := m.timeline.StepDetailCache[m.inspector.DiffBase]; ok && d != nil {
			return d
		}
	}
	// prev-detail is often adjacent; use it if it matches the base step.
	if m.inspector.PrevDetail != nil && m.inspector.PrevStep == m.inspector.DiffBase {
		return m.inspector.PrevDetail
	}
	return nil
}

// ensureDiffBaseDetailCmd issues a fetch for the diff base detail if we don't
// already have it cached. Returns nil if the cache already satisfies us.
func (m dashboardModel) ensureDiffBaseDetailCmd() tea.Cmd {
	if m.inspector.DiffBase == 0 {
		return nil
	}
	if m.lookupDiffBaseDetail() != nil {
		return nil
	}
	return fetchInspectorDetailCmd(m.inspector.PID, m.inspector.UUID, m.inspector.DiffBase)
}

// buildDiffLensContent produces the diff rendering of the current lens by
// diffing the base and current lens contents line-by-line. Falls back to the
// non-diff build if either side is nil.
func (m dashboardModel) buildDiffLensContent(lens inspectorLens, base, current *ipc.GetStepDetailResponse) string {
	if base == nil || current == nil {
		return m.buildLensContent(lens, current, nil)
	}
	baseLines := strings.Split(m.buildFullLensContent(lens, base, nil), "\n")
	curLines := strings.Split(m.buildFullLensContent(lens, current, nil), "\n")
	if len(baseLines) > diffMaxLines || len(curLines) > diffMaxLines {
		return lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted)).
			Render(fmt.Sprintf("  content too large to diff (>%d lines) — exit diff to view", diffMaxLines))
	}
	lines := computeLineDiff(baseLines, curLines)
	return renderDiff(lines, m.inspector.DiffUnfolded, ui.IsASCIIMode())
}

// toggleFollowLive toggles the Follow live auto-jump behaviour. Follow is
// refused on processes in StateDead per AC-11 (status message, no state
// change). Enabling Follow auto-jumps to the latest step, starts the polling
// tick, and — per AC-15 — exits diff mode first if active.
func (m dashboardModel) toggleFollowLive() (tea.Model, tea.Cmd) {
	if m.inspector.FollowLive {
		m.inspector.FollowLive = false
		m.statusMsg = "Follow live: off (F 恢复)"
		m.statusMsgTTL = statusMsgDefaultTTL
		return m, nil
	}

	state, known := m.inspectorProcessState()
	if known && state == types.StateDead {
		// Story 36-6 AC-11: status bar 3s notice; no Follow state change.
		m.statusMsg = "Process ended — Follow live unavailable"
		m.statusMsgTTL = 3
		return m, nil
	}

	// AC-15: exit diff first if active.
	if m.inspector.DiffMode {
		m = m.exitInspectorDiff()
	}

	m.inspector.FollowLive = true
	// Story 36-6 fix: bump generation so stale ticks (scheduled during a prior
	// on-period) see a mismatch in handleFollowLiveTickMsg and self-terminate.
	m.inspector.FollowGen++
	m.statusMsg = "Follow live: on (F 关闭)"
	m.statusMsgTTL = statusMsgDefaultTTL

	// Jump to latest step immediately if one exists.
	var cmds []tea.Cmd
	if len(m.inspector.Steps) > 0 {
		latest := m.inspector.Steps[len(m.inspector.Steps)-1].Step
		if latest != m.inspector.Step || m.inspector.Detail == nil {
			m.inspector.Step = latest
			m.inspector.Fetching = true
			cmds = append(cmds, fetchInspectorDetailCmd(m.inspector.PID, m.inspector.UUID, latest))
		}
	}
	cmds = append(cmds, followLiveTickCmd(m.inspector.PID, m.inspector.UUID, m.inspector.FollowGen))
	return m, tea.Batch(cmds...)
}

// inspectorProcessState returns the current process state of the inspected
// PID, if we know it. The second return is false if the PID is not in the
// live process table (e.g. already reaped).
func (m dashboardModel) inspectorProcessState() (types.ProcessState, bool) {
	for _, p := range m.processes {
		if p.PID == m.inspector.PID && (m.inspector.UUID == "" || p.UUID == m.inspector.UUID) {
			return p.State, true
		}
	}
	// If the process is not in the live table, treat it as Dead — the daemon
	// has already reaped it, so Follow live has nothing to follow.
	if m.inspector.PID != 0 {
		return types.StateDead, true
	}
	return types.StateCreated, false
}

// handleInspectorDetailMsg absorbs a single-step detail IPC response. The
// response is cached whenever PID+UUID match (even if msg.step differs from
// the current cursor) so that diff-base prefetches from ensureDiffBaseDetailCmd
// reach lookupDiffBaseDetail later. When msg.step matches the current step,
// the Inspector foreground state is advanced; when it matches the active
// diff base, the current lens is re-rendered to show the now-available diff.
func (m dashboardModel) handleInspectorDetailMsg(msg inspectorDetailMsg) (dashboardModel, tea.Cmd) {
	if msg.err != nil {
		m.inspector.Fetching = false
		m.statusMsg = fmt.Sprintf("✗ Inspector: %v", msg.err)
		m.statusMsgTTL = statusMsgDefaultTTL
		return m, nil
	}
	if msg.detail == nil || msg.pid != m.inspector.PID || msg.uuid != m.inspector.UUID {
		return m, nil
	}
	if m.timeline.StepDetailCache != nil {
		m.timeline.StepDetailCache[msg.step] = msg.detail
	}
	if msg.step == m.inspector.Step {
		m.inspector.Fetching = false
		m.inspector.PrevStep = m.inspector.CurDetailStep
		m.inspector.PrevDetail = m.inspector.Detail
		m.inspector.Detail = msg.detail
		m.inspector.CurDetailStep = msg.step
		m.inspector.Step = msg.step
		m.inspector.SystemExpanded = false
		// Story 38-3 review P9: when the focused step changes, the diff
		// markers (base vs current) must be recomputed before lens content
		// is rebuilt — otherwise the cached marks reflect the previous
		// current step and stay visually stale.
		if m.inspector.DiffMode {
			m.refreshInspectorDiffLensMarks()
		}
		m.rebuildInspectorContents()
	} else if m.inspector.DiffMode && msg.step == m.inspector.DiffBase {
		// Story 38-3 review P9: when the async base detail finally arrives,
		// recompute marks now that lookupDiffBaseDetail() returns non-nil.
		// Without this, the initial enterInspectorDiff call zeroed marks and
		// nothing recomputed them once the fetch resolved.
		m.refreshInspectorDiffLensMarks()
		m.rebuildInspectorContents()
	}
	return m, nil
}

// handleInspectorStepListMsg absorbs the step-list IPC response and, when
// Follow live is active, auto-jumps to the latest step as new entries arrive.
// Extracted from the dashboard Update switch to keep dashboard.go compact.
// Story 36-6 fix: PID+UUID both matched — a stale step-list fetch (from a
// prior inspector session on the same PID but different UUID) must not
// overwrite the current session's step list.
func (m dashboardModel) handleInspectorStepListMsg(msg inspectorStepListMsg) (dashboardModel, tea.Cmd) {
	if msg.err != nil {
		m.statusMsg = fmt.Sprintf("✗ Inspector steps: %v", msg.err)
		m.statusMsgTTL = statusMsgDefaultTTL
		return m, nil
	}
	if msg.pid != m.inspector.PID || msg.uuid != m.inspector.UUID {
		return m, nil
	}
	if len(msg.steps) > 0 {
		prevLen := len(m.inspector.Steps)
		m.inspector.Steps = msg.steps
		m.inspector.StepMax = msg.steps[len(msg.steps)-1].Step
		if m.inspector.FollowLive && len(msg.steps) > prevLen && m.viewMode == viewStepInspector {
			latest := msg.steps[len(msg.steps)-1].Step
			if latest != m.inspector.Step {
				m.inspector.Step = latest
				m.inspector.Fetching = true
				vp := m.inspector.Viewports[m.inspector.Lens]
				vp.GotoTop()
				m.inspector.Viewports[m.inspector.Lens] = vp
				return m, fetchInspectorDetailCmd(m.inspector.PID, m.inspector.UUID, latest)
			}
		}
		if m.inspector.Detail == nil && m.viewMode == viewStepInspector && !m.inspector.Fetching {
			firstStep := msg.steps[0].Step
			m.inspector.Step = firstStep
			m.inspector.Fetching = true
			return m, fetchInspectorDetailCmd(m.inspector.PID, m.inspector.UUID, firstStep)
		}
		return m, nil
	}
	if m.viewMode == viewStepInspector {
		noData := "  No step data recorded for this process.\n  (Process may have failed before completing any reasoning step)\n"
		for i := range m.inspector.Contents {
			m.inspector.Contents[i] = noData
			m.inspector.Viewports[i].SetContent(noData)
		}
	}
	return m, nil
}

// handleFollowLiveTickMsg re-arms the poll if Follow live is still active and
// issues a fresh step-list fetch. Returns a no-op cmd when Follow has been
// disabled since the last tick was scheduled, when the inspector has moved on
// to a different process (PID or UUID changed), or when the tick belongs to a
// prior Follow activation generation (rapid F-toggle dedup).
func (m dashboardModel) handleFollowLiveTickMsg(msg followLiveTickMsg) (dashboardModel, tea.Cmd) {
	if !m.inspector.FollowLive || m.viewMode != viewStepInspector {
		return m, nil
	}
	if msg.pid != m.inspector.PID || msg.uuid != m.inspector.UUID {
		return m, nil
	}
	if msg.gen != m.inspector.FollowGen {
		return m, nil
	}
	return m, tea.Batch(
		fetchInspectorStepListCmd(m.inspector.PID, m.inspector.UUID),
		followLiveTickCmd(m.inspector.PID, m.inspector.UUID, m.inspector.FollowGen),
	)
}
