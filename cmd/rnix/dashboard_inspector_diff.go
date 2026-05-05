package main

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"

	"github.com/rnixai/rnix/internal/dashboard/inspector"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"
)

// diffLine / followLiveTickMsg are type aliases for the migrated
// inspector package types (Story 38-5 PR11 Step 4(a-2) inspector_diff
// helpers extracted to internal/dashboard/inspector/diff.go). Aliases let
// existing struct literals, type switches and field accesses continue to
// work zero-modification — the field names are now exported (`Kind`/`Text`
// / `PID`/`UUID`/`Gen`) and callsites have been updated accordingly.
//
// Note: the underlying `inspector.DiffKind` type alias is intentionally
// not re-exported here because cmd/rnix only needs the const aliases
// (`diffEqual` / `diffAdd` / `diffDel`); the type name itself is unused
// in this package.
type (
	diffLine          = inspector.DiffLine
	followLiveTickMsg = inspector.FollowLiveTickMsg
)

// diffEqual / diffAdd / diffDel — DiffKind constants re-exported as
// internal aliases so existing handler code reads naturally.
const (
	diffEqual = inspector.DiffEqual
	diffAdd   = inspector.DiffAdd
	diffDel   = inspector.DiffDel
)

// diffFoldThreshold / diffMaxLines / ddWindow are re-exported timing /
// threshold constants used by handler methods in this file and elsewhere
// in cmd/rnix. (followLiveTickInterval is intentionally not re-aliased
// here — followLiveTickCmd delegates directly to the inspector package
// implementation, which uses the canonical constant.)
const (
	diffFoldThreshold = inspector.DiffFoldThreshold
	diffMaxLines      = inspector.DiffMaxLines
	ddWindow          = inspector.DDWindow
)

// computeLineDiff is a thin wrapper delegating to inspector.ComputeLineDiff.
// See package internal/dashboard/inspector for the canonical implementation
// + behaviour contract. Story 38-5 PR11 Step 4(a-2) helper migration.
func computeLineDiff(base, current []string) []diffLine {
	return inspector.ComputeLineDiff(base, current)
}

// renderDiff is a thin wrapper delegating to inspector.RenderDiff.
func renderDiff(lines []diffLine, unfolded map[int]bool, asciiMode bool) string {
	return inspector.RenderDiff(lines, unfolded, asciiMode)
}

// renderDiffBasePicker is a thin wrapper delegating to
// inspector.RenderDiffBasePicker.
func renderDiffBasePicker(steps []ipc.StepSummaryWire, cursor int, width int) string {
	return inspector.RenderDiffBasePicker(steps, cursor, width)
}

// followLiveTickCmd is a thin wrapper delegating to
// inspector.FollowLiveTickCmd. tea.Cmd return type and time.Tick semantics
// preserved.
func followLiveTickCmd(pid types.PID, uuid string, gen int) tea.Cmd {
	return inspector.FollowLiveTickCmd(pid, uuid, gen)
}

// _ time.Duration use-anchor — keep ddWindow accounted as a real
// time.Duration value (compile-time check; lint will not warn since the
// const alias is consumed by handlers below).
var _ time.Duration = ddWindow

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
	// Story 38-5 PR11 Step 4(c)：7 字段重置主体迁出至 inspector.ExitDiffMode ·
	// wrapper 仅保留 viewport YOffset 保存/恢复 + rebuildInspectorContents 副作用.
	m.inspector = inspector.ExitDiffMode(m.inspector)
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
		if lines[i].Kind != diffEqual {
			i++
			continue
		}
		j := i
		for j < len(lines) && lines[j].Kind == diffEqual {
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
		if lines[i].Kind != diffEqual {
			i++
			continue
		}
		j := i
		for j < len(lines) && lines[j].Kind == diffEqual {
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
	if msg.PID != m.inspector.PID || msg.UUID != m.inspector.UUID {
		return m, nil
	}
	if msg.Gen != m.inspector.FollowGen {
		return m, nil
	}
	return m, tea.Batch(
		fetchInspectorStepListCmd(m.inspector.PID, m.inspector.UUID),
		followLiveTickCmd(m.inspector.PID, m.inspector.UUID, m.inspector.FollowGen),
	)
}
