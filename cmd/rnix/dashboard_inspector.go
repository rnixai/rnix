package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
	"unicode/utf8"

	tea "charm.land/bubbletea/v2"
	"charm.land/bubbles/v2/viewport"
	"github.com/charmbracelet/lipgloss"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"
)

// --- Step Inspector (Story 36.1) ---

// enterStepInspector enters the unified Step Inspector overlay.
func (m dashboardModel) enterStepInspector() (tea.Model, tea.Cmd) {
	if m.selectedPID == 0 {
		m.statusMsg = "No process selected"
		m.statusMsgTTL = statusMsgDefaultTTL
		return m, nil
	}
	m.inspectorPrevMode = m.viewMode
	m.viewMode = viewStepInspector
	m.inspectorPID = m.selectedPID
	m.inspectorUUID = m.selectedUUID
	m.inspectorStep = 0
	m.inspectorStepMax = 0
	m.inspectorSteps = nil
	m.inspectorDetail = nil
	m.inspectorPrevDetail = nil
	m.inspectorPrevStep = 0
	m.inspectorCurDetailStep = 0
	m.inspectorLens = lensConversation
	m.inspectorFetching = false
	m.inspectorSystemExpanded = false
	// Story 36-5 fix: reset cross-pane search state when entering Inspector to
	// avoid stale searchQuery carried over from Timeline.
	m.searchMode = false
	m.searchQuery = ""
	m.searchMatches = nil
	m.searchMatchIdx = 0
	m.searchReverse = false
	m.searchCrossLens = false
	// Story 36-6: reset diff/follow state on entry.
	m.inspectorDiffMode = false
	m.inspectorDiffBase = 0
	m.inspectorDiffDelta = 0
	m.inspectorDiffUnfolded = nil
	m.inspectorDiffPicker = false
	m.inspectorDiffPickerCursor = 0
	m.inspectorFollowLive = false

	contentH := m.inspectorContentHeight()
	for i := range m.inspectorViewports {
		m.inspectorViewports[i] = viewport.New(
			viewport.WithWidth(m.width),
			viewport.WithHeight(contentH),
		)
		m.inspectorContents[i] = ""
	}

	return m, fetchInspectorStepListCmd(m.selectedPID, m.selectedUUID)
}

func (m dashboardModel) inspectorContentHeight() int {
	// Story 38-3 AC#6: when the terminal is tall enough (h≥20) the Step
	// Inspector reserves an extra two-line block for the thumbnail bar
	// (glyph row + step-number row). Below that threshold the thumbnail
	// is suppressed and the legacy 4-line chrome (rail+tabs+footer) is
	// preserved.
	if m.height >= 20 {
		// stepRail(1) + thumbnailBar(2) + lensTabs(1) + footer(1) + spacing(1) = 6
		return max(m.height-6, 1)
	}
	// stepRail(2) + lensTabs(1) + footer(1) = 4
	return max(m.height-4, 1)
}

// fetchInspectorStepListCmd fetches the step summary list for the Inspector.
func fetchInspectorStepListCmd(pid types.PID, uuid string) tea.Cmd {
	return func() tea.Msg {
		client, err := ipc.Dial(ipc.SocketPath())
		if err != nil {
			return inspectorStepListMsg{pid: pid, uuid: uuid, err: err}
		}
		defer client.Close()
		_ = client.SetReadDeadline(time.Now().Add(3 * time.Second))

		var resp *ipc.ListStepsResponse
		if uuid != "" {
			resp, err = client.ListStepsByUUID(uuid, 0)
		} else {
			resp, err = client.ListSteps(pid, 0)
		}
		if err != nil {
			return inspectorStepListMsg{pid: pid, uuid: uuid, err: err}
		}
		return inspectorStepListMsg{pid: pid, uuid: uuid, steps: resp.Steps, total: resp.Total}
	}
}

// fetchInspectorDetailCmd fetches a specific step's detail for the Inspector.
func fetchInspectorDetailCmd(pid types.PID, uuid string, step int) tea.Cmd {
	return func() tea.Msg {
		client, err := ipc.Dial(ipc.SocketPath())
		if err != nil {
			return inspectorDetailMsg{pid: pid, uuid: uuid, step: step, err: err}
		}
		defer client.Close()
		_ = client.SetReadDeadline(time.Now().Add(3 * time.Second))

		var detail *ipc.GetStepDetailResponse
		if uuid != "" {
			detail, err = client.GetStepDetailByUUID(uuid, step)
		} else {
			detail, err = client.GetStepDetail(pid, step)
		}
		return inspectorDetailMsg{pid: pid, uuid: uuid, step: step, detail: detail, err: err}
	}
}

// inspectorKey handles key presses in the Step Inspector overlay.
func (m dashboardModel) inspectorKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := msg.String()

	// Story 36-6: Diff base picker intercepts most keys while active.
	if m.inspectorDiffPicker {
		return m.handleDiffPickerKey(key)
	}

	// Story 36-5: search mode input handling takes priority.
	if m.searchMode {
		return m.handleInspectorSearchKey(key)
	}

	switch key {
	case "q":
		return m, tea.Quit
	case "esc":
		// Clear active search highlights before closing
		if m.searchQuery != "" {
			m.searchQuery = ""
			m.searchMatches = nil
			m.searchMatchIdx = 0
			m.searchReverse = false
			m.rebuildInspectorContents()
			return m, nil
		}
		// Story 36-6: Esc also exits diff mode if active
		if m.inspectorDiffMode {
			m = m.exitInspectorDiff()
			return m, nil
		}
		// Story 36-6: Esc also stops follow live (via helper so the user sees
		// the standard off-status line, consistent with other exit paths).
		if m.inspectorFollowLive {
			m = m.stopFollowLiveWithStatus()
		}
		m.viewMode = m.inspectorPrevMode
		return m, nil

	// Lens switching: 1-5 (doesn't stop follow — just changing viewpoint)
	case "1":
		return m.switchInspectorLens(lensConversation), nil
	case "2":
		return m.switchInspectorLens(lensSystem), nil
	case "3":
		return m.switchInspectorLens(lensToolIO), nil
	case "4":
		return m.switchInspectorLens(lensMeta), nil
	case "5":
		return m.switchInspectorLens(lensRawJSON), nil

	// Step navigation (index-based for sparse step support)
	case "h", "left":
		// Story 36-6: Follow auto-off on back-step
		m = m.stopFollowLiveWithStatus()
		if m.inspectorFetching || len(m.inspectorSteps) == 0 {
			return m, nil
		}
		idx := m.findStepIndex(m.inspectorStep)
		if idx <= 0 {
			return m, nil
		}
		newStep := m.inspectorSteps[idx-1].Step
		m.inspectorStep = newStep
		// Story 36-6: keep diff base relative
		if m.inspectorDiffMode {
			m = m.slideDiffBase(newStep)
		}
		m.inspectorFetching = true
		return m, fetchInspectorDetailCmd(m.inspectorPID, m.inspectorUUID, newStep)
	case "l", "right":
		if m.inspectorFetching || len(m.inspectorSteps) == 0 {
			return m, nil
		}
		idx := m.findStepIndex(m.inspectorStep)
		if idx < 0 || idx >= len(m.inspectorSteps)-1 {
			return m, nil
		}
		newStep := m.inspectorSteps[idx+1].Step
		// Story 36-6: follow auto-off unless advancing to latest step
		if newStep != m.inspectorSteps[len(m.inspectorSteps)-1].Step {
			m = m.stopFollowLiveWithStatus()
		}
		m.inspectorStep = newStep
		if m.inspectorDiffMode {
			m = m.slideDiffBase(newStep)
		}
		m.inspectorFetching = true
		return m, fetchInspectorDetailCmd(m.inspectorPID, m.inspectorUUID, newStep)
	case "H", "home":
		// Story 36-6: Follow auto-off on back-step
		m = m.stopFollowLiveWithStatus()
		if len(m.inspectorSteps) == 0 || m.inspectorFetching {
			return m, nil
		}
		firstStep := m.inspectorSteps[0].Step
		if firstStep == m.inspectorStep {
			return m, nil
		}
		m.inspectorStep = firstStep
		if m.inspectorDiffMode {
			m = m.slideDiffBase(firstStep)
		}
		m.inspectorFetching = true
		return m, fetchInspectorDetailCmd(m.inspectorPID, m.inspectorUUID, firstStep)
	case "L", "end":
		if len(m.inspectorSteps) == 0 || m.inspectorFetching {
			return m, nil
		}
		lastStep := m.inspectorSteps[len(m.inspectorSteps)-1].Step
		if lastStep == m.inspectorStep {
			return m, nil
		}
		m.inspectorStep = lastStep
		if m.inspectorDiffMode {
			m = m.slideDiffBase(lastStep)
		}
		m.inspectorFetching = true
		return m, fetchInspectorDetailCmd(m.inspectorPID, m.inspectorUUID, lastStep)

	// Copy
	case "y":
		content := m.inspectorContents[m.inspectorLens]
		charCount := utf8.RuneCountInString(content)
		m.statusMsg = fmt.Sprintf("copied %s chars", formatCharCount(charCount))
		m.statusMsgTTL = statusMsgDefaultTTL
		return m, tea.SetClipboard(content)

	// Open full in $PAGER
	case "o":
		return m.openInspectorInPager()

	// Enter: toggle diff fold, expand system lens, or scroll
	case "enter":
		// Story 36-6: in diff mode, Enter toggles all fold regions at once
		if m.inspectorDiffMode {
			m = m.toggleAllDiffFolds()
			return m, nil
		}
		if m.inspectorLens == lensSystem && !m.inspectorSystemExpanded {
			m.inspectorSystemExpanded = true
			if m.inspectorDetail != nil {
				content := m.buildLensContent(lensSystem, m.inspectorDetail, m.inspectorPrevDetail)
				m.inspectorContents[lensSystem] = content
				m.inspectorViewports[lensSystem].SetContent(content)
				m.inspectorViewports[lensSystem].GotoTop()
			}
			return m, nil
		}
		// Fall through to viewport scroll
		lens := m.inspectorLens
		var cmd tea.Cmd
		m.inspectorViewports[lens], cmd = m.inspectorViewports[lens].Update(msg)
		return m, cmd

	// Story 36-6: Diff mode toggle / dd picker
	case "d":
		return m.handleInspectorDiffKey()

	// Story 36-6: Follow live toggle
	case "F", "shift+F":
		return m.toggleFollowLive()

	// Story 36-5: Search entry for Conversation / Tool I/O / System / Meta / Raw lenses
	case "/":
		// Story 36-6: stop follow when entering search
		m = m.stopFollowLiveWithStatus()
		// Story 36-5 P-2: clear stale highlight from previous search BEFORE
		// resetting searchQuery, so rebuildInspectorContents sees the no-query
		// path and renders raw content. Without this, the next FindMatches runs
		// over content that already contains reverse-render ANSI escapes from
		// the prior search, causing phantom matches on `\x1b[7m`.
		hadPrior := m.searchQuery != ""
		m.searchMode = true
		m.searchQuery = ""
		m.searchMatches = nil
		m.searchMatchIdx = 0
		m.searchReverse = false
		if hadPrior {
			m.rebuildInspectorContents()
		}
		return m, nil

	// Story 36-6: Reverse search `?`
	case "?":
		m = m.stopFollowLiveWithStatus()
		hadPrior := m.searchQuery != ""
		m.searchMode = true
		m.searchQuery = ""
		m.searchMatches = nil
		m.searchMatchIdx = 0
		m.searchReverse = true
		if hadPrior {
			m.rebuildInspectorContents()
		}
		return m, nil

	// Story 36-6: Ctrl-/ cross-lens search placeholder (Story 36-7)
	case "ctrl+_", "ctrl+/":
		m.searchCrossLens = true
		m.statusMsg = "Cross-lens search: TODO (Story 36-7)"
		m.statusMsgTTL = statusMsgDefaultTTL
		return m, nil

	// Story 36-5: Cycle through current search matches
	case "n":
		return m.inspectorJumpSearchMatch(+1), nil
	case "N", "shift+N":
		return m.inspectorJumpSearchMatch(-1), nil

	default:
		// Story 36-5: j/k/PgUp/PgDn/Ctrl-d/u/g/G/home/end → viewport scroll.
		// Story 36-6: back-scrolling stops follow live.
		if isBackScrollKey(key) {
			m = m.stopFollowLiveWithStatus()
		}
		lens := m.inspectorLens
		vp := m.inspectorViewports[lens]
		if ui.HandleListKey(key, &vp, nil, 0, ui.ListNavOpts{}) {
			m.inspectorViewports[lens] = vp
			return m, nil
		}
		var cmd tea.Cmd
		m.inspectorViewports[lens], cmd = m.inspectorViewports[lens].Update(msg)
		return m, cmd
	}
}

// isBackScrollKey reports whether the given key scrolls the viewport in the
// "back" direction (upwards / toward earlier content), triggering Follow live
// auto-off per Story 36-6 AC-13.
func isBackScrollKey(key string) bool {
	switch key {
	case "k", "up", "pgup", "pageup", "ctrl+u", "ctrl+b", "g":
		return true
	}
	return false
}

// stopFollowLiveWithStatus disables inspectorFollowLive and emits the
// user-facing status line described in Story 36-6 AC-13. No-op if follow is
// already off.
func (m dashboardModel) stopFollowLiveWithStatus() dashboardModel {
	if !m.inspectorFollowLive {
		return m
	}
	m.inspectorFollowLive = false
	m.statusMsg = "Follow live: off (F 恢复)"
	m.statusMsgTTL = statusMsgDefaultTTL
	return m
}

// handleInspectorSearchKey handles keystrokes while the Inspector is in
// search-input mode. Story 36-5 AC-12; Story 36-6 adds reverse flag.
func (m dashboardModel) handleInspectorSearchKey(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "esc":
		m.searchMode = false
		m.searchQuery = ""
		m.searchMatches = nil
		m.searchMatchIdx = 0
		m.searchReverse = false
		m.rebuildInspectorContents()
		return m, nil
	case "enter":
		m.searchMode = false
		// Story 36-5 P-3: empty-query short-circuit (parity with Timeline). Without
		// this guard, FindMatches("") returns nil and we'd spam "No matches for """.
		if m.searchQuery == "" {
			return m, nil
		}
		m.refreshInspectorSearchMatches()
		if len(m.searchMatches) == 0 {
			m.statusMsg = fmt.Sprintf("No matches for %q", m.searchQuery)
			m.statusMsgTTL = statusMsgDefaultTTL
			// Story 36-6: TTL for "No matches" notice
			m.searchNoMatchExpireAt = time.Now().Add(3 * time.Second)
			return m, nil
		}
		// Story 36-6 AC-6: reverse search jumps to the last match first.
		if m.searchReverse {
			m.searchMatchIdx = len(m.searchMatches) - 1
		} else {
			m.searchMatchIdx = 0
		}
		m.rebuildInspectorContents()
		m.scrollInspectorToCurrentMatch()
		return m, nil
	case "backspace":
		runes := []rune(m.searchQuery)
		if len(runes) > 0 {
			m.searchQuery = string(runes[:len(runes)-1])
		}
		return m, nil
	case " ", "space":
		m.searchQuery += " "
		return m, nil
	default:
		if len([]rune(key)) == 1 {
			m.searchQuery += key
		}
		return m, nil
	}
}

func (m *dashboardModel) refreshInspectorSearchMatches() {
	content := m.inspectorContents[m.inspectorLens]
	m.searchMatches = ui.FindMatches(content, m.searchQuery)
}

// clearSearchState resets dashboard search-related fields. Used when leaving
// search context due to pane / mode change. Story 36-5 P-1.
func (m dashboardModel) clearSearchState() dashboardModel {
	m.searchMode = false
	m.searchQuery = ""
	m.searchMatches = nil
	m.searchMatchIdx = 0
	m.searchReverse = false
	m.searchCrossLens = false
	return m
}

func (m dashboardModel) inspectorJumpSearchMatch(dir int) dashboardModel {
	if len(m.searchMatches) == 0 {
		return m
	}
	// Story 36-6 AC-6: reverse search flips n/N semantics.
	if m.searchReverse {
		dir = -dir
	}
	n := len(m.searchMatches)
	m.searchMatchIdx = ((m.searchMatchIdx+dir)%n + n) % n
	m.scrollInspectorToCurrentMatch()
	return m
}

func (m *dashboardModel) scrollInspectorToCurrentMatch() {
	if len(m.searchMatches) == 0 {
		return
	}
	line := m.searchMatches[m.searchMatchIdx]
	vp := m.inspectorViewports[m.inspectorLens]
	vp.SetYOffset(line)
	m.inspectorViewports[m.inspectorLens] = vp
}

func (m dashboardModel) switchInspectorLens(lens inspectorLens) dashboardModel {
	m.inspectorLens = lens
	// Story 36-6 fix (AC-6): when diff mode is active, recompute diff for the new
	// lens. Diff-line indices are lens-specific, so stale unfold keys must drop.
	if m.inspectorDiffMode {
		m.inspectorDiffUnfolded = make(map[int]bool)
		// Story 38-3 AC#7: refresh diff-mark cache for tab indicators.
		m.refreshInspectorDiffLensMarks()
	}
	// Story 36-6 fix (AC-9): search matches are line-indexed per lens; rebuild
	// them before rebuildInspectorContents so highlights stay correct.
	if m.searchQuery != "" {
		// Rebuild without highlights first so FindMatches sees raw content; the
		// subsequent rebuildInspectorContents re-applies highlights.
		content := m.buildLensContent(lens, m.inspectorDetail, m.inspectorPrevDetail)
		m.inspectorContents[lens] = content
		m.searchMatches = ui.FindMatches(content, m.searchQuery)
		if len(m.searchMatches) == 0 {
			m.searchMatchIdx = 0
		} else if m.searchMatchIdx >= len(m.searchMatches) {
			m.searchMatchIdx = len(m.searchMatches) - 1
		}
	}
	m.rebuildInspectorContents()
	return m
}

// openInspectorInPager writes current lens full content to a temp file and opens $PAGER.
func (m dashboardModel) openInspectorInPager() (tea.Model, tea.Cmd) {
	detail := m.inspectorDetail
	if detail == nil {
		m.statusMsg = "No step data to open"
		m.statusMsgTTL = statusMsgDefaultTTL
		return m, nil
	}

	lensNames := [inspectorLensCount]string{"conversation", "system", "toolio", "meta", "rawjson"}
	content := m.buildFullLensContent(m.inspectorLens, detail, m.inspectorPrevDetail)

	tmpFile := fmt.Sprintf("/tmp/rnix-step-%s-%d-%s.txt",
		m.inspectorUUID, m.inspectorStep, lensNames[m.inspectorLens])

	if err := os.WriteFile(tmpFile, []byte(content), 0o600); err != nil {
		m.statusMsg = fmt.Sprintf("write error: %v", err)
		m.statusMsgTTL = statusMsgDefaultTTL
		return m, nil
	}

	pager := os.Getenv("PAGER")
	if pager == "" {
		pager = "less"
	}

	// Split PAGER to support flags (e.g. "less -R")
	parts := strings.Fields(pager)
	args := append(parts[1:], tmpFile)
	c := exec.Command(parts[0], args...)
	return m, tea.ExecProcess(c, func(err error) tea.Msg {
		os.Remove(tmpFile) // clean up temp file after pager exits
		return execResultMsg{err: err}
	})
}

// renderStepInspector renders the full-screen Step Inspector overlay.
func (m dashboardModel) renderStepInspector(w, h int) string {
	var b strings.Builder

	// Step Rail (top)
	b.WriteString(m.renderStepRail(w))
	b.WriteString("\n")

	// Story 38-3 AC#6: thumbnail bar (2 lines: glyph row + number row) only
	// when the terminal is tall enough; below 20 rows the legacy 4-line chrome
	// is preserved (no thumbnail).
	if m.height >= 20 {
		thumb := m.renderStepThumbnailBar(w)
		if thumb != "" {
			b.WriteString(thumb)
			b.WriteString("\n")
		}
	}

	// Lens Tabs
	b.WriteString(m.renderLensTabs(w))
	b.WriteString("\n")

	// Story 36-6: Diff base picker overlay — rendered above the content area
	if m.inspectorDiffPicker {
		b.WriteString(renderDiffBasePicker(m.inspectorSteps, m.inspectorDiffPickerCursor, w))
		b.WriteString("\n")
	}

	// Content area — current lens viewport
	lens := m.inspectorLens
	content := m.inspectorContents[lens]
	if content == "" && m.inspectorDetail == nil {
		if m.inspectorFetching {
			content = "  (loading...)"
		} else if len(m.inspectorSteps) == 0 {
			content = "  No step data recorded for this process.\n  (Process may have failed before completing any reasoning step)"
		}
		// Set viewport content so it renders through viewport.View()
		if content != "" && m.inspectorViewports[lens].Width() > 0 {
			m.inspectorViewports[lens].SetContent(content)
		}
	}

	if m.inspectorViewports[lens].Width() > 0 {
		b.WriteString(m.inspectorViewports[lens].View())
	} else {
		contentH := max(h-4, 1)
		lines := strings.Split(content, "\n")
		for i, line := range lines {
			if i >= contentH {
				break
			}
			b.WriteString(line)
			b.WriteString("\n")
		}
	}

	// Footer
	b.WriteString("\n")
	b.WriteString(m.renderInspectorFooter())

	return b.String()
}

// renderStepRail renders the top step navigation rail.
//
// Story 38-3 AC#6 redesigns the rail into a single line of pipe-separated
// (`│` / ASCII `|`) field groups, each in a role-specific color:
//
//	Step Inspector | PID 12 | 14:32 | Step 5/12 | tool_call | ⧖42ms | ⇅3.2k→1.1k tok
//	  └── orange ──┘ └ dim ┘ └────┘ └─ accent ─┘ └─ green ──┘ └────┘ └─────────────┘
//
// Diff base badge (`vs #<n>` yellow) and Follow live (`● FOLLOW` red) append
// to the line when the corresponding mode is active. ASCII mode degrades the
// `│` separator and the `⧖`/`⇅`/`●` glyphs.
func (m dashboardModel) renderStepRail(w int) string {
	var b strings.Builder

	ascii := ui.IsASCIIMode()
	sep := " │ "
	if ascii {
		sep = " | "
	}

	titleStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorReplay)).Bold(true)
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted))
	accent := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorReplay)).Bold(true)
	actionStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorSuccess))
	warn := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorWarning)).Bold(true)
	follow := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError)).Bold(true)

	b.WriteString(" ")
	b.WriteString(titleStyle.Render("Step Inspector"))
	b.WriteString(sep)
	b.WriteString(dim.Render("PID "))
	fmt.Fprintf(&b, "%d", m.inspectorPID)

	// Wall clock (HH:MM only, sourced from process CreatedAt)
	for _, p := range m.processes {
		if p.PID == m.inspectorPID && (m.inspectorUUID == "" || p.UUID == m.inspectorUUID) {
			if !p.CreatedAt.IsZero() {
				b.WriteString(sep)
				b.WriteString(ui.FormatWallClockShort(p.CreatedAt))
			}
			break
		}
	}

	if m.inspectorDetail != nil {
		step := m.inspectorStep
		maxStep := m.inspectorStepMax
		b.WriteString(sep)
		b.WriteString(dim.Render("Step "))
		b.WriteString(accent.Render(fmt.Sprintf("%d", step)))
		b.WriteString(dim.Render(fmt.Sprintf("/%d", maxStep)))

		// Story 36-6: diff base badge
		if m.inspectorDiffMode {
			b.WriteString(" ")
			b.WriteString(warn.Render(fmt.Sprintf("vs #%d", m.inspectorDiffBase)))
		}

		if m.inspectorDetail.Action != "" {
			b.WriteString(sep)
			b.WriteString(actionStyle.Render(m.inspectorDetail.Action))
		}

		// Duration (from tool call)
		if m.inspectorDetail.ToolDurationMs > 0 {
			b.WriteString(sep)
			if ascii {
				fmt.Fprintf(&b, "%.0fms", m.inspectorDetail.ToolDurationMs)
			} else {
				fmt.Fprintf(&b, "⧖%.0fms", m.inspectorDetail.ToolDurationMs)
			}
		}

		// Token counts
		reqTok := m.inspectorDetail.RequestTokens
		respTok := m.inspectorDetail.ResponseTokens
		if reqTok > 0 || respTok > 0 {
			b.WriteString(sep)
			if ascii {
				fmt.Fprintf(&b, "%s->%s tok", formatTokenCount(reqTok), formatTokenCount(respTok))
			} else {
				fmt.Fprintf(&b, "⇅%s→%s tok", formatTokenCount(reqTok), formatTokenCount(respTok))
			}
		}
	}

	// Story 36-6: Follow live indicator
	if m.inspectorFollowLive {
		label := " ● FOLLOW"
		if ascii {
			label = " [FOLLOW]"
		}
		b.WriteString(follow.Render(label))
	}

	// Truncate to width
	result := b.String()
	if w > 0 && utf8.RuneCountInString(stripANSIApprox(result)) > w {
		runes := []rune(result)
		result = string(runes[:w])
	}

	return result
}

// renderStepThumbnailBar emits the two-line thumbnail strip below the Step
// Rail. Story 38-3 AC#6:
//
//	Line 1 — glyphs: ◆ for loaded steps, ◇ for un-loaded steps, ◈ for the
//	         current diff base. The currently focused step uses ColorReplay
//	         orange + Bold; the others tint by step kind (error → red,
//	         tool → green, reasoning → blue, unknown → dim).
//	Line 2 — right-aligned 1-2 digit step numbers with the current step
//	         highlighted in ColorReplay + Bold.
//
// When the loaded list exceeds 50 steps the bar collapses to a window of
// ~20-step head/tail with a centered current step. The bar returns "" when
// the terminal is too short (handled by the caller via inspectorContentHeight
// branching on m.height ≥ 20).
//
// ASCII mode: ◆/◇/◈ degrade to */./+.
func (m dashboardModel) renderStepThumbnailBar(w int) string {
	if len(m.inspectorSteps) == 0 {
		return ""
	}
	ascii := ui.IsASCIIMode()
	cur := m.inspectorStep
	steps := m.inspectorSteps

	// Compress window for >50 steps (AC#6)
	if len(steps) > 50 {
		steps = compressThumbnailWindow(m.inspectorSteps, cur, 20)
	}

	current := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorReplay)).Bold(true)
	red := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError))
	green := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorSuccess))
	blue := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAgent))
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted))
	warn := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorWarning))

	loaded := "◆"
	unloaded := "◇"
	diff := "◈"
	if ascii {
		loaded = "*"
		unloaded = "."
		diff = "+"
	}

	var glyphRow strings.Builder
	var numRow strings.Builder
	glyphRow.WriteString(" ")
	numRow.WriteString(" ")

	for i, s := range steps {
		if i > 0 {
			glyphRow.WriteString(" ")
			numRow.WriteString(" ")
		}
		// "..." compression sentinel: we tag with Step=-1 inside compressThumbnailWindow
		if s.Step == -1 {
			glyphRow.WriteString(muted.Render("…"))
			numRow.WriteString(muted.Render(" "))
			continue
		}

		isCurrent := s.Step == cur
		isDiffBase := m.inspectorDiffMode && s.Step == m.inspectorDiffBase

		// Glyph color (current > diff > kind)
		var glyph string
		switch {
		case isCurrent:
			glyph = current.Render(loaded)
		case isDiffBase:
			glyph = warn.Render(diff)
		case s.HasError:
			glyph = red.Render(loaded)
		case s.ToolPath != "":
			glyph = green.Render(loaded)
		case s.Step == 0:
			glyph = muted.Render(unloaded)
		default:
			glyph = blue.Render(loaded)
		}
		glyphRow.WriteString(glyph)

		// Number row: 1-2 digit, current highlighted
		numStr := fmt.Sprintf("%d", s.Step)
		if isCurrent {
			numRow.WriteString(current.Render(numStr))
		} else {
			numRow.WriteString(muted.Render(numStr))
		}
	}

	combined := glyphRow.String() + "\n" + numRow.String()
	_ = w
	return combined
}

// compressThumbnailWindow returns a sub-slice of steps centered around the
// current step when total count exceeds the threshold. Story 38-3 AC#6:
// ≤20 head + current + ≤20 tail with `…` markers (Step==-1 sentinel).
func compressThumbnailWindow(all []ipc.StepSummaryWire, cur, side int) []ipc.StepSummaryWire {
	curIdx := -1
	for i, s := range all {
		if s.Step == cur {
			curIdx = i
			break
		}
	}
	if curIdx < 0 {
		// Current step not present (shouldn't happen). Return head 50 as fallback.
		if len(all) > 50 {
			return all[:50]
		}
		return all
	}

	start := max(curIdx-side, 0)
	end := min(curIdx+side+1, len(all))

	out := make([]ipc.StepSummaryWire, 0, end-start+2)
	if start > 0 {
		out = append(out, ipc.StepSummaryWire{Step: -1}) // sentinel
	}
	out = append(out, all[start:end]...)
	if end < len(all) {
		out = append(out, ipc.StepSummaryWire{Step: -1}) // sentinel
	}
	return out
}

// stripANSIApprox removes common ANSI escape codes for width measurement.
// This is a minimal approximation (full ANSI parsing is in lipgloss); it is
// adequate for the step rail where we only insert a single styled label.
func stripANSIApprox(s string) string {
	var b strings.Builder
	inEsc := false
	for _, r := range s {
		if r == 0x1b {
			inEsc = true
			continue
		}
		if inEsc {
			if r == 'm' {
				inEsc = false
			}
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// renderLensTabs renders the lens selector tabs.
//
// Story 38-3 AC#7 visual changes:
//   - Inactive tabs keep the numeric prefix (was just label) so 1-5 stays
//     visible regardless of focus.
//   - Tab separator widened from 1 → 2 spaces to reduce visual crowding.
//   - When `inspectorDiffMode` is active, lenses whose content differs
//     between current and base step append a yellow `*` to the label.
//     The marks are pre-computed in `inspectorDiffLensMarks` and refreshed
//     only on lens / base / current transitions to keep render cost flat.
//
// ASCII mode: numeric prefix degrades from ❶❷❸❹❺ to 1-5; `*` marker stays as
// is. Active tab keeps its `[label]` brackets + Bold.
func (m dashboardModel) renderLensTabs(w int) string {
	ascii := ui.IsASCIIMode()

	type lensInfo struct {
		key   string
		label string
	}

	var lenses []lensInfo
	if ascii {
		lenses = []lensInfo{
			{"1", "1 Conv"},
			{"2", "2 Sys"},
			{"3", "3 Tool"},
			{"4", "4 Meta"},
			{"5", "5 JSON"},
		}
	} else {
		lenses = []lensInfo{
			{"1", "❶ Conversation"},
			{"2", "❷ System"},
			{"3", "❸ Tool I/O"},
			{"4", "❹ Meta"},
			{"5", "❺ Raw JSON"},
		}
	}

	activeBold := lipgloss.NewStyle().Bold(true)
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted))
	warn := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorWarning)).Bold(true)

	var b strings.Builder
	b.WriteString(" ")
	for i, l := range lenses {
		label := l.label
		var tab string
		if inspectorLens(i) == m.inspectorLens {
			tab = activeBold.Render("[" + label + "]")
		} else {
			tab = dimStyle.Render(" " + label + " ")
		}
		b.WriteString(tab)
		// Story 38-3 AC#7 diff change marker
		if m.inspectorDiffMode && m.inspectorDiffLensMarks[i] {
			b.WriteString(warn.Render("*"))
		}
		// Story 38-3 AC#7 widened separator (2 spaces)
		if i < len(lenses)-1 {
			b.WriteString("  ")
		}
	}

	result := b.String()
	_ = w
	return result
}

// refreshInspectorDiffLensMarks recomputes the per-lens diff marker cache
// (Story 38-3 AC#7). Called by `switchInspectorLens`, `enterInspectorDiff`,
// `slideDiffBase`, and `handleDiffPickerKey` so renderLensTabs can read the
// pre-computed booleans rather than rebuilding lens content on every frame.
//
// Performance guard: when a lens content exceeds 100KB the diff is skipped
// (mark stays `false`) to keep the dashboard responsive on huge prompts.
func (m *dashboardModel) refreshInspectorDiffLensMarks() {
	for i := range inspectorLensCount {
		m.inspectorDiffLensMarks[i] = false
	}
	if !m.inspectorDiffMode || m.inspectorDetail == nil {
		return
	}
	baseDetail := m.lookupDiffBaseDetail()
	if baseDetail == nil {
		return
	}
	for i := range inspectorLensCount {
		baseContent := m.buildLensContent(inspectorLens(i), baseDetail, nil)
		curContent := m.buildLensContent(inspectorLens(i), m.inspectorDetail, m.inspectorPrevDetail)
		if len(baseContent) > 100*1024 || len(curContent) > 100*1024 {
			continue
		}
		m.inspectorDiffLensMarks[i] = baseContent != curContent
	}
}

// renderInspectorFooter renders the bottom shortcut hints.
func (m dashboardModel) renderInspectorFooter() string {
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
	// Story 36-5: search overlay takes over the footer while active.
	if m.searchMode {
		prefix := "/"
		if m.searchReverse {
			prefix = "?"
		}
		return fmt.Sprintf(" Search: %s%s_", prefix, m.searchQuery)
	}
	// Story 36-6: diff-mode status line
	if m.inspectorDiffMode {
		return dimStyle.Render(fmt.Sprintf(" Diff: step %d vs %d (dd to pick base, Esc/d to exit)", m.inspectorStep, m.inspectorDiffBase))
	}
	// Story 36-6: show Match X/Y counter when a search is active
	if m.searchQuery != "" && len(m.searchMatches) > 0 {
		return dimStyle.Render(fmt.Sprintf(" /%s  Match %d/%d  n/N next/prev · Esc clear",
			m.searchQuery, m.searchMatchIdx+1, len(m.searchMatches)))
	}
	// Story 36-6: show "No matches" TTL notice
	if !m.searchNoMatchExpireAt.IsZero() && time.Now().Before(m.searchNoMatchExpireAt) && m.searchQuery != "" {
		return dimStyle.Render(fmt.Sprintf(" /%s  No matches · Esc clear", m.searchQuery))
	}
	return dimStyle.Render(" h/l step · 1-5 lens · j/k scroll · / ? search · d diff · F follow · y copy · o open · Esc back")
}

// rebuildInspectorContents rebuilds all lens content from the current detail.
func (m *dashboardModel) rebuildInspectorContents() {
	reverse := lipgloss.NewStyle().Reverse(true)
	// Story 36-6: when diff mode is active, render diff for the current lens
	// against the captured base step; other lenses still render their normal
	// contents (user sees diff for the lens they've focused, switching lenses
	// recomputes diff for the new lens via switchInspectorLens).
	var baseDetail *ipc.GetStepDetailResponse
	if m.inspectorDiffMode {
		baseDetail = m.lookupDiffBaseDetail()
	}

	for i := range inspectorLensCount {
		var content string
		if m.inspectorDiffMode && inspectorLens(i) == m.inspectorLens && baseDetail != nil {
			content = m.buildDiffLensContent(inspectorLens(i), baseDetail, m.inspectorDetail)
		} else {
			content = m.buildLensContent(inspectorLens(i), m.inspectorDetail, m.inspectorPrevDetail)
		}
		// Story 36-5: Apply reverse-video highlight to matched lines for the active lens.
		if inspectorLens(i) == m.inspectorLens && m.searchQuery != "" {
			lines := strings.Split(content, "\n")
			matchSet := make(map[int]struct{}, len(m.searchMatches))
			for _, ln := range m.searchMatches {
				matchSet[ln] = struct{}{}
			}
			for idx, line := range lines {
				if _, ok := matchSet[idx]; ok {
					lines[idx] = reverse.Render(line)
				}
			}
			content = strings.Join(lines, "\n")
		}
		m.inspectorContents[i] = content
		m.inspectorViewports[i].SetContent(content)
		// Story 36-5 fix: only reset scroll on the active lens; preserve per-lens
		// scroll position on other lenses (Story 36-1 invariant).
		if inspectorLens(i) == m.inspectorLens {
			m.inspectorViewports[i].GotoTop()
		}
	}
}

// buildLensContent builds display content for a specific lens.
func (m dashboardModel) buildLensContent(lens inspectorLens, detail, prevDetail *ipc.GetStepDetailResponse) string {
	if detail == nil {
		return "  (loading...)"
	}
	switch lens {
	case lensConversation:
		return m.buildConversationLens(detail)
	case lensSystem:
		return m.buildSystemLens(detail, prevDetail)
	case lensToolIO:
		return m.buildToolIOLens(detail)
	case lensMeta:
		return m.buildMetaLens(detail)
	case lensRawJSON:
		return m.buildRawJSONLens(detail)
	default:
		return ""
	}
}

// buildFullLensContent builds FULL (non-truncated) content for pager output.
func (m dashboardModel) buildFullLensContent(lens inspectorLens, detail, prevDetail *ipc.GetStepDetailResponse) string {
	if detail == nil {
		return ""
	}
	switch lens {
	case lensConversation:
		return m.buildConversationLensFull(detail)
	case lensSystem:
		return detail.SystemPrompt
	case lensToolIO:
		return m.buildToolIOLensFull(detail)
	case lensMeta:
		return m.buildMetaLens(detail)
	case lensRawJSON:
		return m.buildRawJSONLens(detail)
	default:
		return ""
	}
}

// buildConversationLens builds Lens ❶: full message flow with explicit truncation.
func (m dashboardModel) buildConversationLens(detail *ipc.GetStepDetailResponse) string {
	if len(detail.Messages) == 0 {
		dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
		return dimStyle.Render("No message history available.\n\n" +
			"CLI driver processes manage their conversation history internally.\n" +
			"For native-driver processes, this lens shows the complete message flow.")
	}

	var b strings.Builder
	toolCallNames := buildToolCallNameMap(detail.Messages)
	separator := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888")).Render(strings.Repeat("─", 70))

	for i, msg := range detail.Messages {
		if i > 0 {
			b.WriteString(separator + "\n")
		}
		roleTag := formatRoleTag(msg, toolCallNames)
		b.WriteString(roleTag + "\n")

		content := msg.Content
		totalLen := utf8.RuneCountInString(content)
		if totalLen > inspectorTruncateThreshold {
			runes := []rune(content)
			content = string(runes[:inspectorTruncateThreshold])
			b.WriteString(content)
			b.WriteString(renderTruncationNotice(inspectorTruncateThreshold, totalLen))
		} else {
			b.WriteString(content)
		}
		b.WriteString("\n\n")
	}

	return b.String()
}

// buildConversationLensFull builds full (non-truncated) conversation for pager.
func (m dashboardModel) buildConversationLensFull(detail *ipc.GetStepDetailResponse) string {
	if len(detail.Messages) == 0 {
		return "No message history available."
	}
	var b strings.Builder
	for i, msg := range detail.Messages {
		if i > 0 {
			b.WriteString(strings.Repeat("─", 70) + "\n")
		}
		b.WriteString("[" + msg.Role + "]\n")
		b.WriteString(msg.Content)
		b.WriteString("\n\n")
	}
	return b.String()
}

// buildSystemLens builds Lens ❷: system prompt with diff indication.
//
// Story 38-3 AC#3 adds a third visual state alongside the pre-existing
// unchanged-collapsed / unchanged-expanded states:
//
//   - **changed**: when prevDetail.SystemPrompt != detail.SystemPrompt the
//     lens prepends a yellow `⚠ changed from step N (+/-X chars)` header,
//     where X = utf8.RuneCountInString(current) - utf8.RuneCountInString(prev),
//     friendly-formatted via formatCharCount. The full prompt is shown
//     immediately (no expand action required).
//
// ASCII fallback: the `⚠` glyph degrades to `!` so the warning remains visible
// in `RNIX_ASCII=1` terminals. The unchanged paths preserve their pre-Story
// 38-3 wording verbatim so 27-4 regression assertions stay green.
func (m dashboardModel) buildSystemLens(detail, prevDetail *ipc.GetStepDetailResponse) string {
	var b strings.Builder

	// First step (or no prior detail) — no diff annotation, just show the prompt.
	if prevDetail == nil || m.inspectorStep == 0 {
		return renderSystemPromptBody(detail.SystemPrompt)
	}

	isUnchanged := prevDetail.SystemPrompt == detail.SystemPrompt

	if isUnchanged && !m.inspectorSystemExpanded {
		dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
		b.WriteString(dimStyle.Render(fmt.Sprintf("unchanged from step %d [press Enter to expand]", m.inspectorPrevStep)))
		b.WriteString("\n")
		return b.String()
	}

	if isUnchanged {
		dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
		b.WriteString(dimStyle.Render(fmt.Sprintf("(unchanged from step %d)", m.inspectorPrevStep)))
		b.WriteString("\n\n")
		b.WriteString(renderSystemPromptBody(detail.SystemPrompt))
		return b.String()
	}

	// Story 38-3 AC#3: changed path with `⚠ changed from step N (+/-X chars)` header.
	delta := utf8.RuneCountInString(detail.SystemPrompt) - utf8.RuneCountInString(prevDetail.SystemPrompt)
	warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorWarning)).Bold(true)
	icon := "⚠"
	if ui.IsASCIIMode() {
		icon = "!"
	}
	b.WriteString(warnStyle.Render(fmt.Sprintf("%s changed from step %d (%s chars)", icon, m.inspectorPrevStep, formatSignedCharCount(delta))))
	b.WriteString("\n\n")
	b.WriteString(renderSystemPromptBody(detail.SystemPrompt))
	return b.String()
}

// formatSignedCharCount returns "+1.2k" / "-272" / "+0" style strings used by
// the System lens changed header and any future delta indicators. Story 38-3
// AC#3 specifies the explicit sign for non-zero deltas; zero is shown as `+0`
// for visual consistency rather than the empty string.
func formatSignedCharCount(delta int) string {
	if delta < 0 {
		return "-" + formatCharCount(-delta)
	}
	return "+" + formatCharCount(delta)
}

// renderSystemPromptBody emits the canonical "═══ System Prompt (X chars) ═══"
// header followed by the prompt body, applying inspector truncation. Extracted
// so all three System-lens code paths (first-step / unchanged-expanded /
// changed) share the same body rendering.
func renderSystemPromptBody(prompt string) string {
	var b strings.Builder
	sysLen := utf8.RuneCountInString(prompt)
	fmt.Fprintf(&b, "═══ System Prompt (%s chars) ═══\n\n", formatCharCount(sysLen))
	if sysLen > inspectorTruncateThreshold {
		runes := []rune(prompt)
		b.WriteString(string(runes[:inspectorTruncateThreshold]))
		b.WriteString(renderTruncationNotice(inspectorTruncateThreshold, sysLen))
	} else {
		b.WriteString(prompt)
	}
	return b.String()
}

// buildToolIOLens builds Lens ❸: tool call details.
//
// Story 38-3 AC#2: Input / Result / Error are framed by box-drawing borders
// (`┌─ Input ──┐ … └─┘`) using `renderBoxedSection`. The Error box uses
// ColorError red for both border and content. Box width auto-shrinks for
// narrow terminals via `min(70, m.width-4)`. ASCII mode degrades to
// `+`/`|`/`-`. The pre-Story-38-3 contract for the no-tool case is preserved:
// when `Action=="" && ToolPath==""`, the lens still shows
// `No tool information for this step.`
func (m dashboardModel) buildToolIOLens(detail *ipc.GetStepDetailResponse) string {
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
	nameStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorSuccess)).Bold(true)
	ascii := ui.IsASCIIMode()

	if detail.Action == "" && detail.ToolPath == "" {
		return dimStyle.Render("No tool information for this step.")
	}

	boxWidth := inspectorBoxWidth(m.width)

	var b strings.Builder

	// Header: "<ToolName> — <summary>" + ⧖<duration>ms (separate line, AC#2)
	if detail.Action != "" {
		b.WriteString(nameStyle.Render(detail.Action))
		if detail.Summary != "" {
			b.WriteString(" — " + dimStyle.Render(detail.Summary))
		}
		b.WriteString("\n")
	}
	if detail.ToolDurationMs > 0 {
		if ascii {
			fmt.Fprintf(&b, "Duration: %.0fms\n", detail.ToolDurationMs)
		} else {
			fmt.Fprintf(&b, "%s ⧖%.0fms\n", dimStyle.Render("Duration:"), detail.ToolDurationMs)
		}
	}
	if detail.ToolPath != "" {
		b.WriteString(dimStyle.Render("Path: ") + detail.ToolPath + "\n")
	}
	b.WriteString("\n")

	if detail.ToolInput != "" {
		b.WriteString(renderBoxedSection("Input", truncateBoxContent(detail.ToolInput), ui.ColorMuted, false, boxWidth))
		b.WriteString("\n")
	}

	if detail.ToolResult != "" {
		b.WriteString(renderBoxedSection("Result", truncateBoxContent(detail.ToolResult), ui.ColorMuted, false, boxWidth))
		b.WriteString("\n")
	}

	if detail.ToolError != "" {
		b.WriteString(renderBoxedSection("Error", truncateBoxContent(detail.ToolError), ui.ColorError, true, boxWidth))
		b.WriteString("\n")
	}

	return b.String()
}

// inspectorBoxWidth returns the effective box width for Tool I/O sections,
// shrinking automatically on narrow terminals. Story 38-3 AC#2 / AC#9: cap at
// 70 cols; `mWidth - 4` accounts for left/right padding (2 cols each).
func inspectorBoxWidth(mWidth int) int {
	if mWidth <= 0 {
		return 70
	}
	if mWidth-4 < 70 {
		w := mWidth - 4
		if w < 20 {
			return 20
		}
		return w
	}
	return 70
}

// truncateBoxContent applies the inspector's standard rune-count truncation
// with the Story 27-4 truncation notice. Used by `renderBoxedSection` callers
// so the truncation marker lands inside the framed area.
func truncateBoxContent(content string) string {
	totalLen := utf8.RuneCountInString(content)
	if totalLen <= inspectorTruncateThreshold {
		return content
	}
	runes := []rune(content)
	return string(runes[:inspectorTruncateThreshold]) + renderTruncationNotice(inspectorTruncateThreshold, totalLen)
}

// boxChar maps a logical box-drawing component name to its rune (or ASCII
// fallback when `RNIX_ASCII=1`). Story 38-3 Dev Notes 4: this helper is kept
// local to the inspector and intentionally does NOT reuse
// `ui.AlertSeverityIcon` (which returns severity glyphs, not box runes).
//
// Names: tl/tr/bl/br = corners; h/v = horizontal/vertical edge.
func boxChar(name string) string {
	if ui.IsASCIIMode() {
		switch name {
		case "tl", "tr", "bl", "br":
			return "+"
		case "h":
			return "-"
		case "v":
			return "|"
		}
		return "?"
	}
	switch name {
	case "tl":
		return "┌"
	case "tr":
		return "┐"
	case "bl":
		return "└"
	case "br":
		return "┘"
	case "h":
		return "─"
	case "v":
		return "│"
	}
	return "?"
}

// renderBoxedSection draws a titled box around `content`. Story 38-3 AC#2.
//
// Layout (Unicode mode):
//
//	┌─ <title> ──...──┐
//	│ <content line>  │
//	│ <content line>  │
//	└─────────────────┘
//
// Parameters:
//   - title:       short header rendered into the top edge
//   - content:     pre-truncated multiline body (caller handles truncation)
//   - color:       hex color for the border; when colorBody is true, body
//                  lines also adopt this color (used by the Error box)
//   - colorBody:   whether to apply `color` to body content
//   - width:       total box width (outer); inner usable width is width-4
//
// ASCII mode: borders degrade to `+ - |`.
func renderBoxedSection(title, content, color string, colorBody bool, width int) string {
	if width < 8 {
		width = 8
	}
	innerWidth := width - 4 // 1 left edge + 1 space + content + 1 space + 1 right edge

	borderStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(color))
	bodyStyle := lipgloss.NewStyle()
	if colorBody {
		bodyStyle = bodyStyle.Foreground(lipgloss.Color(color))
	}

	tl := boxChar("tl")
	tr := boxChar("tr")
	bl := boxChar("bl")
	br := boxChar("br")
	h := boxChar("h")
	v := boxChar("v")

	// Top edge: `┌─ <title> ──...─┐`
	titleSegment := h + " " + title + " "
	titleRunes := utf8.RuneCountInString(titleSegment)
	fillCount := max(innerWidth+2-titleRunes, 1)
	top := tl + titleSegment + strings.Repeat(h, fillCount) + tr

	var out strings.Builder
	out.WriteString(borderStyle.Render(top))
	out.WriteString("\n")

	// Body lines (each split / wrapped to innerWidth as best-effort)
	for line := range strings.SplitSeq(content, "\n") {
		// Word-agnostic truncation: split overlong lines into chunks of innerWidth runes.
		if utf8.RuneCountInString(line) == 0 {
			out.WriteString(borderStyle.Render(v))
			out.WriteString(strings.Repeat(" ", innerWidth+2))
			out.WriteString(borderStyle.Render(v))
			out.WriteString("\n")
			continue
		}
		chunks := chunkRunes(line, innerWidth)
		for _, chunk := range chunks {
			padCount := max(innerWidth-utf8.RuneCountInString(stripANSIApprox(chunk)), 0)
			out.WriteString(borderStyle.Render(v))
			out.WriteString(" ")
			if colorBody {
				out.WriteString(bodyStyle.Render(chunk))
			} else {
				out.WriteString(chunk)
			}
			out.WriteString(strings.Repeat(" ", padCount))
			out.WriteString(" ")
			out.WriteString(borderStyle.Render(v))
			out.WriteString("\n")
		}
	}

	// Bottom edge
	bottom := bl + strings.Repeat(h, innerWidth+2) + br
	out.WriteString(borderStyle.Render(bottom))
	return out.String()
}

// chunkRunes splits `s` into rune-count-bounded chunks, ignoring ANSI escape
// codes when measuring width. Used by `renderBoxedSection` so that wide JSON
// blobs or stack traces wrap inside the box rather than overflow.
func chunkRunes(s string, max int) []string {
	if max <= 0 {
		return []string{s}
	}
	if utf8.RuneCountInString(stripANSIApprox(s)) <= max {
		return []string{s}
	}
	var out []string
	var cur strings.Builder
	count := 0
	inEsc := false
	for _, r := range s {
		if r == 0x1b {
			inEsc = true
			cur.WriteRune(r)
			continue
		}
		if inEsc {
			cur.WriteRune(r)
			if r == 'm' {
				inEsc = false
			}
			continue
		}
		cur.WriteRune(r)
		count++
		if count >= max {
			out = append(out, cur.String())
			cur.Reset()
			count = 0
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

// buildToolIOLensFull builds full (non-truncated) tool I/O for pager.
func (m dashboardModel) buildToolIOLensFull(detail *ipc.GetStepDetailResponse) string {
	var b strings.Builder

	if detail.Action != "" {
		b.WriteString("Action: " + detail.Action + "\n")
	}
	if detail.Summary != "" {
		b.WriteString("Summary: " + detail.Summary + "\n")
	}
	if detail.ToolPath != "" {
		b.WriteString("Path: " + detail.ToolPath + "\n")
	}
	if detail.ToolInput != "" {
		b.WriteString("\nInput:\n" + detail.ToolInput + "\n")
	}
	if detail.ToolResult != "" {
		b.WriteString("\nResult:\n" + detail.ToolResult + "\n")
	}
	if detail.ToolError != "" {
		b.WriteString("\nError:\n" + detail.ToolError + "\n")
	}
	if detail.ToolDurationMs > 0 {
		fmt.Fprintf(&b, "\nDuration: %.0fms\n", detail.ToolDurationMs)
	}

	return b.String()
}

// metaTokenContextWindow is the assumed default context-window cap (Claude
// 3.5/4.x default). Used by the Meta lens token bar to compute fill percent.
// Story 38-3 Dev Notes 2.6: kept as a package-level constant rather than
// reading dynamically from per-process config — KISS, revisit in retro if
// multi-provider context windows become a concern.
const metaTokenContextWindow = 200000

// buildMetaLens builds Lens ❹: metadata.
//
// Story 38-3 AC#4 reorganizes the metadata into three labeled sections:
//
//	── Tokens ──    Request / Response / Total + a 20-char █░ bar chart
//	── Action ──    Action / Summary / Tool Path / Duration (when populated)
//	── Counts ──    Messages / Step (now with `<step> / <max>` form)
//
// When all three token counts are zero, the Tokens section collapses to a
// single `Tokens: (no data)` line to avoid misleading 0-bars. ASCII mode
// degrades the bar runes `█` → `#`, `░` → `.` while preserving labels and
// numerics. Existing 27-4 assertions on token counts (e.g. `1500`, `800`)
// remain satisfied because the integer counts are still printed.
func (m dashboardModel) buildMetaLens(detail *ipc.GetStepDetailResponse) string {
	var b strings.Builder
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted))

	// ── Tokens ──
	b.WriteString(renderMetaSectionHeader("Tokens", 70))
	b.WriteString("\n")
	if detail.RequestTokens == 0 && detail.ResponseTokens == 0 && detail.TokenCount == 0 {
		b.WriteString("Tokens: (no data)\n")
	} else {
		b.WriteString(renderTokenLine("Request:", detail.RequestTokens, metaTokenContextWindow, false))
		b.WriteString("\n")
		b.WriteString(renderTokenLine("Response:", detail.ResponseTokens, metaTokenContextWindow, false))
		b.WriteString("\n")
		b.WriteString(renderTokenLine("Total:", detail.TokenCount, metaTokenContextWindow, true))
		b.WriteString("\n")
	}
	b.WriteString("\n")

	// ── Action ──
	if detail.Action != "" || detail.Summary != "" || detail.ToolPath != "" || detail.ToolDurationMs > 0 {
		b.WriteString(renderMetaSectionHeader("Action", 70))
		b.WriteString("\n")
		actionStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorSuccess))
		if detail.Action != "" {
			fmt.Fprintf(&b, "%s %s\n", dimStyle.Render("Action:"), actionStyle.Render(detail.Action))
		}
		if detail.Summary != "" {
			fmt.Fprintf(&b, "%s %s\n", dimStyle.Render("Summary:"), detail.Summary)
		}
		if detail.ToolPath != "" {
			fmt.Fprintf(&b, "%s %s\n", dimStyle.Render("Tool Path:"), detail.ToolPath)
		}
		if detail.ToolDurationMs > 0 {
			fmt.Fprintf(&b, "%s %.0fms\n", dimStyle.Render("Duration:"), detail.ToolDurationMs)
		}
		b.WriteString("\n")
	}

	// ── Counts ──
	b.WriteString(renderMetaSectionHeader("Counts", 70))
	b.WriteString("\n")
	fmt.Fprintf(&b, "%s %d\n", dimStyle.Render("Messages:"), detail.MessageCount)
	if m.inspectorStepMax > 0 {
		fmt.Fprintf(&b, "%s %d / %d\n", dimStyle.Render("Step:"), detail.Step, m.inspectorStepMax)
	} else {
		fmt.Fprintf(&b, "%s %d\n", dimStyle.Render("Step:"), detail.Step)
	}
	// Story 27-4 regression: keep the legacy "Message Count: <n>" literal so the
	// existing assertion (TestInspector_MetaLensContainsMessageCount-like) sees
	// the same string as before. Same data, dual label, minor footprint.
	fmt.Fprintf(&b, "%s %d\n", dimStyle.Render("Message Count:"), detail.MessageCount)

	return b.String()
}

// renderMetaSectionHeader returns "── <title> ────...───" padded to width.
// Used by Meta lens to delimit the three sections.
func renderMetaSectionHeader(title string, width int) string {
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted))
	prefix := "── " + title + " "
	used := utf8.RuneCountInString(prefix)
	fillCount := max(width-used, 3)
	return dim.Render(prefix + strings.Repeat("─", fillCount))
}

// renderTokenLine emits a single Tokens-section row:
//
//	Request:   1234   ████████░░░░░░░░░░░░  6.2%
//
// totalLine=true switches to the no-bar Total form:
//
//	Total:     2300   2300 of 200000 context
//
// ASCII fallback: `█` → `#`, `░` → `.`. The label is right-aligned to 10
// chars so all three rows align; the bar width is fixed at 20 chars. Story
// 38-3 AC#4. We render the raw integer (instead of formatTokenCount) so the
// 27-4 regression tests asserting on literal counts (e.g. `1500`, `800`,
// `2300`) continue to match — the bar already provides the visual
// scale at a glance.
func renderTokenLine(label string, count, total int, totalLine bool) string {
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted))
	labelPadded := fmt.Sprintf("%-10s", label)
	if totalLine {
		return fmt.Sprintf("%s %d  %s",
			dim.Render(labelPadded),
			count,
			dim.Render(fmt.Sprintf("%d of %d context", count, total)))
	}
	pct := 0.0
	if total > 0 {
		pct = float64(count) / float64(total) * 100
	}
	return fmt.Sprintf("%s %d  %s  %.1f%%",
		dim.Render(labelPadded),
		count,
		renderTokenBar(count, total, 20),
		pct)
}

// renderTokenBar returns a fixed-width unicode block-char bar showing the
// fill ratio of `count / total`. Width is in display columns. ASCII fallback
// uses `#` (filled) and `.` (empty). Story 38-3 AC#4.
func renderTokenBar(count, total, width int) string {
	if width < 1 {
		width = 1
	}
	filled := 0
	if total > 0 {
		filled = (count * width) / total
		filled = min(filled, width)
		filled = max(filled, 0)
	}
	full := "█"
	empty := "░"
	if ui.IsASCIIMode() {
		full = "#"
		empty = "."
	}
	return strings.Repeat(full, filled) + strings.Repeat(empty, width-filled)
}

// buildRawJSONLens builds Lens ❺: raw JSON with 2-space indent.
func (m dashboardModel) buildRawJSONLens(detail *ipc.GetStepDetailResponse) string {
	data, err := json.MarshalIndent(detail, "", "  ")
	if err != nil {
		return fmt.Sprintf("JSON marshal error: %v", err)
	}
	return string(data)
}

// renderTruncationNotice renders an explicit truncation notice.
func renderTruncationNotice(shown, total int) string {
	sep := " · "
	if ui.IsASCIIMode() {
		sep = " - "
	}
	return fmt.Sprintf("\n(truncated %s / total %s%so open full)",
		formatCharCount(shown), formatCharCount(total), sep)
}

// findStepIndex finds the index of a step number in inspectorSteps, or -1 if not found.
func (m dashboardModel) findStepIndex(step int) int {
	for i, s := range m.inspectorSteps {
		if s.Step == step {
			return i
		}
	}
	return -1
}

// buildToolCallNameMap maps tool call IDs to names.
func buildToolCallNameMap(msgs []ipc.MessageWire) map[string]string {
	names := make(map[string]string)
	for _, msg := range msgs {
		for _, tc := range msg.ToolCalls {
			names[tc.ID] = tc.Name
		}
	}
	return names
}
