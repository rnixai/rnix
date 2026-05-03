package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/lipgloss"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"
)

// Story 38-3 AC#5: package-level compiled regexes for Raw JSON lens syntax
// highlighting. RE2-safe (linear time, no backreferences). Order matters:
// keys are tagged first, then string values, then numbers, then booleans —
// see Dev Notes 6 for why a single-pass tokenizer (replacing the prior 4
// regex passes) prevents nested-coloring corruption — bool/null literals
// and key-shaped substrings inside string values would otherwise be
// re-coloured by later passes (Story 38-3 code review P1+P6).

// searchMatchPos identifies the byte-range location of a single search hit
// inside the Inspector's currently active lens content. Story 38-3 AC#8:
// stored alongside the legacy `searchMatches []int` (line numbers) so the
// Timeline path is unaffected — this struct is Inspector-only.
type searchMatchPos struct {
	lineIdx   int
	byteStart int
	byteEnd   int
}

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
	m.inspectorSearchPos = nil
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
			m.inspectorSearchPos = nil
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
		m.inspectorSearchPos = nil
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
		m.inspectorSearchPos = nil
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
		m.inspectorSearchPos = nil
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
	// Story 38-3 AC#8: also collect byte positions for word-level highlighting.
	m.inspectorSearchPos = findInspectorMatchesByPos(content, m.searchQuery)
}

// findInspectorMatchesByPos locates every case-insensitive substring match of
// `query` inside `content` and returns the per-occurrence byte ranges along
// with the line index. Story 38-3 AC#8: this is the byte-position counterpart
// to `ui.FindMatches` (which returns only line numbers). Lives inside the
// inspector package so the listnav search contract stays unchanged.
//
// Empty query returns nil. Search uses Go's regexp engine with the (?i)
// case-insensitive flag, which performs Unicode-aware case folding while
// reporting byte offsets in the original content. This avoids the per-line
// `strings.ToLower` round-trip that previously corrupted byte offsets when a
// rune's lowercase form had a different byte length (e.g. `İ` U+0130 → `i`,
// 2 bytes → 1 byte) — Story 38-3 code review P5.
//
// Byte offsets are reported in the original `content` so subsequent highlight
// rendering can reuse them directly.
func findInspectorMatchesByPos(content, query string) []searchMatchPos {
	if query == "" {
		return nil
	}
	re, err := regexp.Compile("(?i)" + regexp.QuoteMeta(query))
	if err != nil {
		return nil
	}
	var out []searchMatchPos
	lineStart := 0
	lineIdx := 0
	for i := 0; i <= len(content); i++ {
		if i == len(content) || (i < len(content) && content[i] == '\n') {
			line := content[lineStart:i]
			for _, m := range re.FindAllStringIndex(line, -1) {
				out = append(out, searchMatchPos{
					lineIdx:   lineIdx,
					byteStart: lineStart + m[0],
					byteEnd:   lineStart + m[1],
				})
			}
			lineStart = i + 1
			lineIdx++
		}
	}
	return out
}

// clearSearchState resets dashboard search-related fields. Used when leaving
// search context due to pane / mode change. Story 36-5 P-1.
func (m dashboardModel) clearSearchState() dashboardModel {
	m.searchMode = false
	m.searchQuery = ""
	m.searchMatches = nil
	m.inspectorSearchPos = nil
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
		// Story 38-3 AC#8: keep word-level positions in sync with line matches.
		m.inspectorSearchPos = findInspectorMatchesByPos(content, m.searchQuery)
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

	// Truncate to width.
	//
	// Story 38-3 review P19: prior implementation cast the full string
	// (including ANSI SGR sequences) to a rune slice and sliced at `w`
	// runes. When the cut landed inside an escape sequence the trailing
	// `m` byte was lost and colour leaked through to subsequent rail
	// content / thumbnail / tabs. truncateANSIRunes below tracks ANSI
	// state so cuts always land between visible runes, and emits an
	// `\x1b[0m` reset when an SGR was open at the cut.
	result := b.String()
	if w > 0 {
		result = truncateANSIRunes(result, w)
	}
	return result
}

// truncateANSIRunes returns the prefix of `s` whose visible width (ANSI
// escape sequences ignored) does not exceed `maxCols` runes. Open SGR
// sequences are closed with `\x1b[0m` to prevent colour leak. Used by
// `renderStepRail` (Story 38-3 review P19) but generally applicable to any
// single-line ANSI-styled string that needs hard truncation.
func truncateANSIRunes(s string, maxCols int) string {
	if maxCols <= 0 {
		return ""
	}
	visible := 0
	inEsc := false
	hasOpenSGR := false
	var b strings.Builder
	for _, r := range s {
		if r == 0x1b {
			inEsc = true
			b.WriteRune(r)
			continue
		}
		if inEsc {
			b.WriteRune(r)
			if r == 'm' {
				inEsc = false
				snapshot := b.String()
				if idx := strings.LastIndex(snapshot, "\x1b["); idx >= 0 {
					seq := snapshot[idx:]
					if seq == "\x1b[0m" || seq == "\x1b[m" {
						hasOpenSGR = false
					} else {
						hasOpenSGR = true
					}
				}
			}
			continue
		}
		if visible >= maxCols {
			if hasOpenSGR {
				b.WriteString("\x1b[0m")
			}
			return b.String()
		}
		b.WriteRune(r)
		visible++
	}
	if hasOpenSGR {
		b.WriteString("\x1b[0m")
	}
	return b.String()
}

// renderStepThumbnailBar emits the two-line thumbnail strip below the Step
// Rail. Story 38-3 AC#6:
//
//	Line 1 — glyphs: ◆ for loaded steps, ◈ for the current diff base.
//	         The currently focused step uses ColorReplay orange + Bold;
//	         the others tint by step kind (error → red, tool → green,
//	         reasoning → blue, unknown → dim).
//	Line 2 — right-aligned 2-column step numbers (e.g. " 1", "10") with the
//	         current step highlighted in ColorReplay + Bold.
//
// Each thumbnail occupies a fixed 2-column slot so that the glyph row and
// number row stay column-aligned even when step numbers cross 1↔2 digits.
// Thumbnails are separated by a single space, giving 3 columns per slot
// (slot + separator). When the bar would exceed `w` columns, the trailing
// thumbnails are dropped so the row never wraps.
//
// When the loaded list exceeds 50 steps the bar collapses to a window of
// ~20-step head/tail with a centered current step (compressThumbnailWindow).
// The bar returns "" when the terminal is too short (handled by the caller
// via inspectorContentHeight branching on m.height ≥ 20) or when there are
// no steps loaded.
//
// ASCII mode: ◆/◇/◈ degrade to */./+.
//
// Story 38-3 code review fixes:
//   - P3: removed the misleading `case s.Step == 0 → unloaded` branch;
//     Step==0 is a valid first-step number, not a sentinel for not-loaded
//     (the sentinel is Step==-1, set by compressThumbnailWindow).
//   - P4: glyph row and number row are now padded to a uniform 2-column
//     slot so multi-digit steps don't shear the visual alignment.
//   - P10: the previously-discarded `w` parameter now caps how many
//     thumbnails are emitted, preventing the bar from overflowing narrow
//     terminals after compression (~41 visible thumbnails @ 50+ steps).
func (m dashboardModel) renderStepThumbnailBar(w int) string {
	if len(m.inspectorSteps) == 0 {
		return ""
	}
	ascii := ui.IsASCIIMode()
	cur := m.inspectorStep
	steps := m.inspectorSteps

	// Compress window for >50 steps (AC#6).
	if len(steps) > 50 {
		steps = compressThumbnailWindow(m.inspectorSteps, cur, 20)
	}

	// Cap visible thumbnails to fit width: leading space + N slots of 2 cols
	// each + (N-1) single-space separators ≤ w. Solve: 1 + 3N - 1 ≤ w ⇒
	// N ≤ w / 3. We keep N≥1 so the current step is always visible.
	if w > 3 && len(steps)*3 > w {
		maxSlots := max(w/3, 1)
		steps = trimThumbnailToWidth(steps, cur, maxSlots)
	}

	current := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorReplay)).Bold(true)
	red := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError))
	green := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorSuccess))
	blue := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAgent))
	muted := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted))
	warn := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorWarning))

	loaded := "◆"
	diff := "◈"
	if ascii {
		loaded = "*"
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
		// Compression sentinel (Step==-1) inserted by compressThumbnailWindow.
		if s.Step == -1 {
			glyphRow.WriteString(muted.Render("… "))
			numRow.WriteString("  ")
			continue
		}

		isCurrent := s.Step == cur
		isDiffBase := m.inspectorDiffMode && s.Step == m.inspectorDiffBase

		// Glyph color (current > diff > kind). Each glyph is followed by a
		// trailing space so the slot occupies exactly 2 columns (matching
		// the 2-column right-aligned step number below).
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
		default:
			glyph = blue.Render(loaded)
		}
		glyphRow.WriteString(glyph + " ")

		// Number row: 2-column right-aligned, current highlighted.
		numStr := fmt.Sprintf("%2d", s.Step)
		if isCurrent {
			numRow.WriteString(current.Render(numStr))
		} else {
			numRow.WriteString(muted.Render(numStr))
		}
	}

	return glyphRow.String() + "\n" + numRow.String()
}

// trimThumbnailToWidth narrows a step slice down to `maxSlots` entries,
// preserving the current step in the visible window. When the window has to
// drop entries on either side it inserts a `…` sentinel (Step==-1) to make
// the truncation visible. Story 38-3 review P10.
func trimThumbnailToWidth(steps []ipc.StepSummaryWire, cur, maxSlots int) []ipc.StepSummaryWire {
	if len(steps) <= maxSlots {
		return steps
	}
	// Locate the current step inside the slice — fall back to centring if
	// it isn't present (e.g. async load race).
	curIdx := -1
	for i, s := range steps {
		if s.Step == cur {
			curIdx = i
			break
		}
	}
	if curIdx < 0 {
		curIdx = len(steps) / 2
	}
	side := max(maxSlots/2, 1)
	start := max(curIdx-side, 0)
	end := min(start+maxSlots, len(steps))
	start = max(end-maxSlots, 0)

	out := make([]ipc.StepSummaryWire, 0, maxSlots+2)
	if start > 0 {
		out = append(out, ipc.StepSummaryWire{Step: -1})
	}
	out = append(out, steps[start:end]...)
	if end < len(steps) {
		out = append(out, ipc.StepSummaryWire{Step: -1})
	}
	return out
}

// compressThumbnailWindow returns a sub-slice of steps centered around the
// current step when total count exceeds the threshold. Story 38-3 AC#6:
// `side` head + current + `side` tail with `…` markers (Step==-1 sentinel).
//
// When the current step is not present in `all` (e.g. an async load race
// during step navigation), the function returns the *tail* of the list with
// a leading `…` sentinel rather than silently dropping the latest steps —
// the user almost always wants the most recent context, not the first 50
// historical entries (Story 38-3 review P14).
func compressThumbnailWindow(all []ipc.StepSummaryWire, cur, side int) []ipc.StepSummaryWire {
	curIdx := -1
	for i, s := range all {
		if s.Step == cur {
			curIdx = i
			break
		}
	}
	if curIdx < 0 {
		// Current step not present — surface the most recent tail with a
		// leading sentinel so the user can see truncation happened.
		windowLen := 2*side + 1
		if len(all) <= windowLen {
			return all
		}
		out := make([]ipc.StepSummaryWire, 0, windowLen+1)
		out = append(out, ipc.StepSummaryWire{Step: -1}) // sentinel
		out = append(out, all[len(all)-windowLen:]...)
		return out
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
		// Story 38-3 review P2: build both sides with prevDetail=nil so neither
		// side renders a "⚠ changed from step N" header (which depends on the
		// prior step, not on the diff base). Without this, the System lens
		// would erroneously mark `*` whenever current ≠ prev *even when*
		// base.SystemPrompt == current.SystemPrompt — the header itself
		// differed between sides.
		baseContent := m.buildLensContent(inspectorLens(i), baseDetail, nil)
		curContent := m.buildLensContent(inspectorLens(i), m.inspectorDetail, nil)
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
//
// Story 38-3 AC#8 changes the search highlight from line-level reverse video
// to word-level: only the matched substring(s) are reverse-rendered. The
// current match (the one `searchMatchIdx` points to) uses ColorWarning yellow,
// other matches use ColorMuted grey, both with reverse video. The current
// match is identified by mapping `searchMatchIdx` (a line-number index into
// `searchMatches`) to the corresponding line, then highlighting any
// `inspectorSearchPos` entry on that line with the current style.
func (m *dashboardModel) rebuildInspectorContents() {
	curStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorWarning)).Reverse(true)
	otherStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted)).Reverse(true)
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
		// Story 38-3 AC#8: word-level reverse-video highlight on the active lens.
		if inspectorLens(i) == m.inspectorLens && m.searchQuery != "" && len(m.inspectorSearchPos) > 0 {
			content = applyWordLevelHighlight(content, m.inspectorSearchPos, m.searchMatches, m.searchMatchIdx, curStyle, otherStyle)
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

// applyWordLevelHighlight wraps each entry in `positions` with the
// appropriate reverse-video style. Positions whose `lineIdx` matches the
// current line (resolved from `searchMatches[matchIdx]`) get `curStyle`;
// every other match gets `otherStyle`. The function processes positions in
// reverse byte order so earlier insertions don't shift later byte offsets.
// Story 38-3 AC#8.
func applyWordLevelHighlight(content string, positions []searchMatchPos, searchMatches []int, matchIdx int, curStyle, otherStyle lipgloss.Style) string {
	currentLine := -1
	if matchIdx >= 0 && matchIdx < len(searchMatches) {
		currentLine = searchMatches[matchIdx]
	}
	// Sort-stable positions by descending byteStart by walking the slice in
	// reverse — they are already produced in ascending order by line.
	out := []byte(content)
	for i := len(positions) - 1; i >= 0; i-- {
		p := positions[i]
		if p.byteStart < 0 || p.byteEnd > len(out) || p.byteStart >= p.byteEnd {
			continue
		}
		matched := string(out[p.byteStart:p.byteEnd])
		var styled string
		if p.lineIdx == currentLine {
			styled = curStyle.Render(matched)
		} else {
			styled = otherStyle.Render(matched)
		}
		out = append(out[:p.byteStart], append([]byte(styled), out[p.byteEnd:]...)...)
	}
	return string(out)
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

	// First step (or no prior detail) — no diff annotation, just show the
	// prompt. Story 38-3 review P18: use inspectorPrevStep to align with the
	// spec L61 condition `inspectorPrevStep == 0`. inspectorStep tracks the
	// *current* step, not the prior; using it here would mis-classify async
	// load races where current step ≥ 1 but prevDetail hasn't yet arrived.
	if prevDetail == nil || m.inspectorPrevStep == 0 {
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

	// Header: "<ToolName> — <summary>" + ⧖<duration>ms (separate line, AC#2).
	// Story 38-3 review D2=b: drop the previous "Duration:" label so the
	// header strictly matches spec L48 `<ToolName> — <summary> + ⧖<ms>ms`.
	if detail.Action != "" {
		b.WriteString(nameStyle.Render(detail.Action))
		if detail.Summary != "" {
			b.WriteString(" — " + dimStyle.Render(detail.Summary))
		}
		b.WriteString("\n")
	}
	if detail.ToolDurationMs > 0 {
		if ascii {
			fmt.Fprintf(&b, "%.0fms\n", detail.ToolDurationMs)
		} else {
			fmt.Fprintf(&b, "⧖%.0fms\n", detail.ToolDurationMs)
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
//
// Story 38-3 review P12: when `mWidth` is very small (≤24) the lower bound
// caps at the actual terminal width rather than the previous fixed 20-col
// floor — a 20-col box still overflows a 10-col TTY.
func inspectorBoxWidth(mWidth int) int {
	if mWidth <= 0 {
		return 70
	}
	if mWidth-4 < 70 {
		w := mWidth - 4
		if w < 20 {
			return min(20, mWidth)
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
//     lines also adopt this color (used by the Error box)
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

	// Top edge: `┌─ <title> ──...─┐`. Story 38-3 review P15: when the title
	// itself is wider than the inner area, truncate it so the top edge stays
	// within `innerWidth+2` columns (matching the body line width). Without
	// this, the top edge would push past the right border, producing a
	// non-rectangular box.
	maxTitleRunes := max(innerWidth-2, 1) // reserve 2 cols for "h + space" prefix and trailing space
	titleRunes := []rune(title)
	if len(titleRunes) > maxTitleRunes {
		title = string(titleRunes[:maxTitleRunes])
	}
	titleSegment := h + " " + title + " "
	titleSegRunes := utf8.RuneCountInString(titleSegment)
	fillCount := max(innerWidth+2-titleSegRunes, 1)
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

// chunkRunes splits `s` into display-width-bounded chunks, ignoring ANSI
// escape codes when measuring width. Used by `renderBoxedSection` so that
// wide JSON blobs or stack traces wrap inside the box rather than overflow.
//
// Story 38-3 review fixes:
//   - P16: when an ANSI SGR sequence (e.g. `\x1b[33m`) is open at a chunk
//     boundary, emit `\x1b[0m` to close the chunk and re-open the same SGR
//     at the start of the next chunk so colour does not bleed across box
//     body lines.
//   - P17: rune width is measured via `lipgloss.Width` so CJK / emoji /
//     other wide characters consume the 2 display columns they actually
//     occupy on the terminal — previously a wide rune was counted as 1
//     column, which mis-aligned the right border.
func chunkRunes(s string, maxCols int) []string {
	if maxCols <= 0 {
		return []string{s}
	}
	if lipgloss.Width(s) <= maxCols {
		return []string{s}
	}
	var out []string
	var cur strings.Builder
	cols := 0
	inEsc := false
	openSGR := "" // last open SGR sequence, to re-open on next chunk
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
				// Track the last SGR so we can re-open it after a chunk
				// boundary; treat `\x1b[0m` as a reset that clears the carry.
				snapshot := cur.String()
				if idx := strings.LastIndex(snapshot, "\x1b["); idx >= 0 {
					seq := snapshot[idx:]
					if seq == "\x1b[0m" || seq == "\x1b[m" {
						openSGR = ""
					} else {
						openSGR = seq
					}
				}
			}
			continue
		}
		cur.WriteRune(r)
		cols += lipgloss.Width(string(r))
		if cols >= maxCols {
			if openSGR != "" {
				cur.WriteString("\x1b[0m")
			}
			out = append(out, cur.String())
			cur.Reset()
			if openSGR != "" {
				cur.WriteString(openSGR)
			}
			cols = 0
		}
	}
	if cur.Len() > 0 {
		// Tail chunk: also reset any still-open SGR so the next box border
		// renders without inherited colour.
		if openSGR != "" {
			cur.WriteString("\x1b[0m")
		}
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
	headerWidth := metaSectionHeaderWidth(m.width)
	b.WriteString(renderMetaSectionHeader("Tokens", headerWidth))
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
		b.WriteString(renderMetaSectionHeader("Action", headerWidth))
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
	b.WriteString(renderMetaSectionHeader("Counts", headerWidth))
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

// metaSectionHeaderWidth returns the dynamic width for the Meta lens section
// dividers. Story 38-3 review P11: previously the three callsites passed a
// fixed `70` literal which overflowed sub-70-col terminals. Cap at 70 cols
// for readability and shrink to `m.width-2` (allowing for the inspector's
// 1-col left padding) on narrower terminals.
func metaSectionHeaderWidth(mWidth int) int {
	if mWidth <= 0 {
		return 70
	}
	return min(70, max(mWidth-2, 16))
}

// renderTokenLine emits a single Tokens-section row:
//
//	   Request: 1234   ████████░░░░░░░░░░░░  6.2%
//
// totalLine=true switches to the no-bar Total form:
//
//	     Total: 2300 of 200,000 context
//
// ASCII fallback: `█` → `#`, `░` → `.`. The label is right-aligned to 10
// chars so all three rows align; the bar width is fixed at 20 chars.
//
// Story 38-3 AC#4 + code review fixes:
//   - P7: label uses `%10s` (right-aligned) instead of the previous
//     `%-10s` (left-aligned) so the colons of `Request:` / `Response:` /
//     `Total:` align vertically.
//   - P8: total form prints the context cap with a thousands separator
//     (`200,000`) per spec L76 example.
//   - P13: total form drops the redundant leading `<count>` repetition —
//     the count appears exactly once in `<count> of <total> context`.
//
// We render the raw integer count (instead of formatTokenCount) so the
// existing 27-4 regression tests asserting on literal counts (e.g. `1500`,
// `800`, `2300`) continue to match — the bar already conveys the visual
// scale.
func renderTokenLine(label string, count, total int, totalLine bool) string {
	dim := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted))
	labelPadded := fmt.Sprintf("%10s", label)
	if totalLine {
		return fmt.Sprintf("%s %s",
			dim.Render(labelPadded),
			dim.Render(fmt.Sprintf("%d of %s context", count, formatThousands(total))))
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

// formatThousands inserts thousands separators (US locale: `,`) into a
// non-negative integer. Story 38-3 review P8: used by the Meta lens Total
// row to render `200,000` instead of `200000`.
func formatThousands(n int) string {
	if n < 0 {
		return "-" + formatThousands(-n)
	}
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var b strings.Builder
	pre := len(s) % 3
	if pre > 0 {
		b.WriteString(s[:pre])
		if len(s) > pre {
			b.WriteByte(',')
		}
	}
	for i := pre; i < len(s); i += 3 {
		b.WriteString(s[i : i+3])
		if i+3 < len(s) {
			b.WriteByte(',')
		}
	}
	return b.String()
}

// renderTokenBar returns a fixed-width unicode block-char bar showing the
// fill ratio of `count / total`. Width is in display columns. ASCII fallback
// uses `#` (filled) and `.` (empty). Story 38-3 AC#4.
//
// Story 38-3 review P20: ratio is computed in floating point and clamped to
// [0, 1] before scaling to width — this avoids 32-bit overflow when count is
// very large (`count*width` would otherwise exceed int range) and prevents
// the bar from drawing more cells than `width` when `count > total`
// (e.g. tokens exceeding the 200K default context window).
func renderTokenBar(count, total, width int) string {
	if width < 1 {
		width = 1
	}
	filled := 0
	if total > 0 {
		ratio := float64(count) / float64(total)
		if ratio < 0 {
			ratio = 0
		} else if ratio > 1 {
			ratio = 1
		}
		filled = int(ratio * float64(width))
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
//
// Story 38-3 AC#5 adds 5-token syntax highlighting:
//
//	keys                ColorAgent  blue
//	string values       ColorSuccess green
//	numbers             ColorWarning yellow
//	booleans / null     ColorReplay  orange
//	punctuation { } [ ] , :  default ColorMuted via lipgloss reset
//
// Implementation uses 4 RE2-safe regexes applied in a fixed order to the
// json.MarshalIndent output. ANSI escape codes from earlier passes are skipped
// by later passes because the regex anchors (`"\w_-"`, `\b`) won't match
// across the inserted `\x1b[...m` sequences.
//
// Performance guard: when the marshaled JSON exceeds 100KB the highlighter is
// bypassed entirely and the raw indented JSON is returned (Story 38-3 AC#5).
// This keeps very large step details responsive at the cost of plain text.
func (m dashboardModel) buildRawJSONLens(detail *ipc.GetStepDetailResponse) string {
	data, err := json.MarshalIndent(detail, "", "  ")
	if err != nil {
		return fmt.Sprintf("JSON marshal error: %v", err)
	}
	raw := string(data)
	if len(raw) > 100*1024 {
		return raw
	}
	return highlightJSON(raw)
}

// highlightJSON applies the single-pass JSON syntax highlighter. Story 38-3
// AC#5 covers all 5 token classes:
//
//	keys                ColorAgent   blue
//	string values       ColorSuccess green
//	numbers             ColorWarning yellow
//	booleans / null     ColorReplay  orange
//	punctuation { } [ ] , :  ColorMuted dim
//
// Implementation walks `raw` as a byte stream, classifying tokens by their
// leading character. This avoids the prior 4-regex pipeline's nested-coloring
// bug (Story 38-3 review P1): a `\b(true|false|null)\b` Pass 4 would match
// inside already-coloured string values like `"the truth is true"`, injecting
// a stray `\x1b[0m` that prematurely terminated the green span and
// miscoloured a substring. The forward scan keeps per-token colouring
// strictly local to each lexical token, so ANSI sequences from one token
// never affect classification of the next.
//
// Exported as a package-private helper so tests can exercise it directly
// without a full dashboardModel.
func highlightJSON(raw string) string {
	keyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorAgent))
	stringStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorSuccess))
	numberStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorWarning))
	boolStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorReplay))
	punctStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted))

	var b strings.Builder
	b.Grow(len(raw))

	i := 0
	for i < len(raw) {
		c := raw[i]
		switch {
		case c == '"':
			// String literal — find the unescaped closing quote.
			end := i + 1
			for end < len(raw) {
				if raw[end] == '\\' && end+1 < len(raw) {
					end += 2
					continue
				}
				if raw[end] == '"' {
					end++
					break
				}
				end++
			}
			// A string is treated as a key when followed (after optional
			// whitespace) by a colon — this is what JSON.MarshalIndent always
			// emits for object keys. Any other string is a value.
			j := end
			for j < len(raw) && (raw[j] == ' ' || raw[j] == '\t') {
				j++
			}
			text := raw[i:end]
			if j < len(raw) && raw[j] == ':' {
				b.WriteString(keyStyle.Render(text))
			} else {
				b.WriteString(stringStyle.Render(text))
			}
			i = end

		case c == '-' || (c >= '0' && c <= '9'):
			// Numeric literal: optional sign, integer, optional fractional /
			// exponent.
			end := i
			if c == '-' {
				end++
			}
			for end < len(raw) && raw[end] >= '0' && raw[end] <= '9' {
				end++
			}
			if end < len(raw) && raw[end] == '.' {
				end++
				for end < len(raw) && raw[end] >= '0' && raw[end] <= '9' {
					end++
				}
			}
			if end < len(raw) && (raw[end] == 'e' || raw[end] == 'E') {
				end++
				if end < len(raw) && (raw[end] == '+' || raw[end] == '-') {
					end++
				}
				for end < len(raw) && raw[end] >= '0' && raw[end] <= '9' {
					end++
				}
			}
			b.WriteString(numberStyle.Render(raw[i:end]))
			i = end

		case c == 't' || c == 'f' || c == 'n':
			// bool/null. Read a contiguous lowercase identifier and check for
			// the three reserved literals. Anything else is emitted verbatim.
			end := i
			for end < len(raw) && raw[end] >= 'a' && raw[end] <= 'z' {
				end++
			}
			word := raw[i:end]
			if word == "true" || word == "false" || word == "null" {
				b.WriteString(boolStyle.Render(word))
			} else {
				b.WriteString(word)
			}
			i = end

		case c == '{' || c == '}' || c == '[' || c == ']' || c == ',' || c == ':':
			b.WriteString(punctStyle.Render(string(c)))
			i++

		default:
			// Whitespace, newlines, or any other byte the JSON encoder might
			// emit — preserve verbatim.
			b.WriteByte(c)
			i++
		}
	}
	return b.String()
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
