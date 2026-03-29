package main

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"charm.land/bubbles/v2/viewport"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"

	tea "charm.land/bubbletea/v2"
)

// --- Step-unified Timeline (UX Timeline Unification) ---

// actionColor returns the display color for a step action type.
func actionColor(action string) lipgloss.Color {
	switch action {
	case "tool_call":
		return lipgloss.Color("#6BCB77")
	case "plan":
		return lipgloss.Color("#5B9BD5")
	case "text":
		return lipgloss.Color("#FFFFFF")
	case "complete":
		return lipgloss.Color("#6BCB77")
	case "spawn":
		return lipgloss.Color("#9B59B6")
	case "specialize":
		return lipgloss.Color("#4EC9B0")
	case "replan":
		return lipgloss.Color("#E5C07B")
	default:
		return lipgloss.Color("#FFFFFF")
	}
}

// actionAbbrev returns a shortened action name for narrow screens.
// "text" is displayed as "asst" (assistant) to match the conversation role.
func actionAbbrev(action string) string {
	switch action {
	case "tool_call":
		return "tool"
	case "complete":
		return "done"
	case "specialize":
		return "spec"
	case "text":
		return "x"
	default:
		return action
	}
}

// defaultStepFilters returns a map with all action types enabled.
func defaultStepFilters() map[string]bool {
	return map[string]bool{
		"tool_call":  true,
		"plan":       true,
		"text":       true,
		"complete":   true,
		"spawn":      true,
		"replan":     true,
		"specialize": true,
	}
}

// handleTimelinePIDChange resets timeline state when selected process changes.
// Uses UUID for reliable identification (PIDs can be reused).
func (m dashboardModel) handleTimelinePIDChange() dashboardModel {
	if m.selectedUUID == m.timelineAttachedUUID {
		return m
	}
	m.timelineAttachedPID = m.selectedPID
	m.timelineAttachedUUID = m.selectedUUID
	m.stepEntries = nil
	m.stepCursor = 0
	m.stepScrollTop = 0
	m.stepDetailCache = make(map[int]*ipc.GetStepDetailResponse)
	m.lastFetchedStep = 0
	m.fetchingDetail = false
	m.promptPager = false
	m.stepFilterMode = false
	m.stepExpandedIdx = -1
	return m
}

// handleTimelineKey dispatches keys for the unified Step timeline.
func (m dashboardModel) handleTimelineKey(key string) dashboardModel {
	if m.stepFilterMode {
		return m.handleStepFilterKey(key)
	}
	filtered := m.filteredStepEntries()
	switch key {
	case "up", "k":
		if m.stepCursor > 0 {
			m.stepCursor--
		}
	case "down", "j":
		if m.stepCursor < len(filtered)-1 {
			m.stepCursor++
		}
	case "pgdown":
		pageSize := max(m.dashboardVisibleLines()-4, 1)
		m.stepCursor = min(m.stepCursor+pageSize, max(len(filtered)-1, 0))
	case "pgup":
		pageSize := max(m.dashboardVisibleLines()-4, 1)
		m.stepCursor = max(m.stepCursor-pageSize, 0)
	case "home", "g":
		m.stepCursor = 0
		m.stepScrollTop = 0
	case "end", "G", "shift+G":
		if len(filtered) > 0 {
			m.stepCursor = len(filtered) - 1
		}
	case "f":
		m.stepFilterMode = true
		m.statusMsg = "过滤模式: 按 T/P/A/C/S/R/Z 切换, * 全选, Esc 退出"
		m.statusMsgTTL = statusMsgDefaultTTL
	case "e":
		// Expand all visible (filtered) steps that have expandable content
		expanded := 0
		for _, fi := range filtered {
			if fi >= 0 && fi < len(m.stepEntries) {
				entry := &m.stepEntries[fi]
				if entry.level < levelExpanded {
					detail := m.stepDetailCache[entry.summary.Step]
					if detail == nil || hasExpandableContent(detail, entry.summary) {
						entry.level = levelExpanded
						expanded++
					}
				}
			}
		}
		if expanded == 0 {
			m.statusMsg = "No expandable steps"
			m.statusMsgTTL = statusMsgDefaultTTL
		}
	case "E":
		// Collapse all visible (filtered) steps to Level 1
		for _, fi := range filtered {
			if fi >= 0 && fi < len(m.stepEntries) {
				m.stepEntries[fi].level = levelSummary
			}
		}
	case "n":
		// Jump to next error step
		found := false
		for i := m.stepCursor + 1; i < len(filtered); i++ {
			if m.stepEntries[filtered[i]].summary.HasError {
				m.stepCursor = i
				found = true
				break
			}
		}
		if !found {
			m.statusMsg = "No more errors"
			m.statusMsgTTL = statusMsgDefaultTTL
		}
	case "N", "shift+N":
		// Jump to previous error step
		found := false
		for i := m.stepCursor - 1; i >= 0; i-- {
			if m.stepEntries[filtered[i]].summary.HasError {
				m.stepCursor = i
				found = true
				break
			}
		}
		if !found {
			m.statusMsg = "No more errors"
			m.statusMsgTTL = statusMsgDefaultTTL
		}
	}
	m.ensureStepCursorVisible(max(m.dashboardVisibleLines()-4, 1))
	return m
}

// handleStepFilterKey handles keys in filter editing mode.
func (m dashboardModel) handleStepFilterKey(key string) dashboardModel {
	if m.stepFilters == nil {
		m.stepFilters = defaultStepFilters()
	}
	switch key {
	case "t":
		m.stepFilters["tool_call"] = !m.stepFilters["tool_call"]
	case "p":
		m.stepFilters["plan"] = !m.stepFilters["plan"]
	case "a":
		m.stepFilters["text"] = !m.stepFilters["text"]
	case "c":
		m.stepFilters["complete"] = !m.stepFilters["complete"]
	case "s":
		m.stepFilters["spawn"] = !m.stepFilters["spawn"]
	case "r":
		m.stepFilters["replan"] = !m.stepFilters["replan"]
	case "z":
		m.stepFilters["specialize"] = !m.stepFilters["specialize"]
	case "*":
		m.stepFilters = defaultStepFilters()
	case "f", "esc":
		m.stepFilterMode = false
	default:
		m.statusMsg = "过滤模式: 按 T/P/A/C/S/R/Z 切换, * 全选, Esc 退出"
		m.statusMsgTTL = statusMsgDefaultTTL
	}
	return m
}

// filteredStepEntries returns step entries matching current filters.
func (m dashboardModel) filteredStepEntries() []int {
	if len(m.stepFilters) == 0 {
		indices := make([]int, len(m.stepEntries))
		for i := range m.stepEntries {
			indices[i] = i
		}
		return indices
	}
	// Check if all filters are on
	allOn := true
	for _, v := range m.stepFilters {
		if !v {
			allOn = false
			break
		}
	}
	if allOn {
		indices := make([]int, len(m.stepEntries))
		for i := range m.stepEntries {
			indices[i] = i
		}
		return indices
	}
	var result []int
	for i, e := range m.stepEntries {
		if m.stepFilters[e.summary.Action] {
			result = append(result, i)
		}
	}
	return result
}

// resolveStepIndex converts cursor position in filtered view to actual stepEntries index.
func (m dashboardModel) resolveStepIndex() int {
	filtered := m.filteredStepEntries()
	if m.stepCursor < 0 || m.stepCursor >= len(filtered) {
		return -1
	}
	return filtered[m.stepCursor]
}

// --- Rendering ---

func (m dashboardModel) renderTimelinePane(width, height int) string {
	isActive := m.activePane == paneTimeline

	borderColor := lipgloss.Color(ui.ColorMuted)
	if isActive {
		borderColor = lipgloss.Color(ui.ColorAgent)
	}

	innerW := max(width-2, 1)
	innerH := max(height-2, 1)

	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Width(innerW).
		Height(innerH)

	return style.Render(m.renderStepTimeline(innerW, innerH))
}

func (m dashboardModel) renderStepTimeline(width, height int) string {
	var b strings.Builder
	truncW := max(width-1, 1)
	total := len(m.stepEntries)
	filtered := m.filteredStepEntries()

	// Header
	if m.stepFilterMode {
		b.WriteString(m.renderStepFilterBar(truncW))
	} else {
		b.WriteString(m.renderStepHeader(truncW, total, filtered))
	}
	b.WriteString("\n")

	if m.selectedPID == 0 {
		b.WriteString("\n    Select an agent to view timeline")
		return b.String()
	}

	if total == 0 {
		b.WriteString("\n    Waiting for steps…")
		return b.String()
	}

	if len(filtered) == 0 {
		b.WriteString("\n    No steps match filter")
		return b.String()
	}

	cursor := min(m.stepCursor, max(len(filtered)-1, 0))

	listLines := max(height-2, 1)

	// Variable-height scroll: ensure cursor is visible via stepScrollTop
	startIdx := m.stepScrollTop
	if startIdx < 0 || startIdx >= len(filtered) {
		startIdx = 0
	}
	// If cursor is above scrollTop, snap up
	if cursor < startIdx {
		startIdx = cursor
	}
	// Check if cursor fits in viewport from startIdx
	{
		linesUsed := 0
		cursorVisible := false
		for fi := startIdx; fi < len(filtered); fi++ {
			h := m.stepItemHeight(filtered[fi])
			if fi == cursor && linesUsed+h <= listLines {
				cursorVisible = true
				break
			}
			linesUsed += h
			if linesUsed >= listLines {
				break
			}
		}
		if !cursorVisible {
			// Scroll down: walk backward from cursor to fill viewport
			linesUsed := m.stepItemHeight(filtered[cursor])
			startIdx = cursor
			for startIdx > 0 {
				h := m.stepItemHeight(filtered[startIdx-1])
				if linesUsed+h > listLines {
					break
				}
				linesUsed += h
				startIdx--
			}
		}
	}
	endIdx := len(filtered) // render loop uses linesUsed to stop

	// Layout params — new structure: summary is primary, step+action are compact suffix
	// Before: [cursor][collapse][Step N  ][action     ] summary    [token] [dur] [err]
	// After:  [cursor][collapse] summary                                [N abbr] [token] [dur] [err]
	showDuration := width >= 90
	showToken := width >= 70

	linesUsed := 0
	for fi := startIdx; fi < endIdx && linesUsed < listLines; fi++ {
		idx := filtered[fi]
		entry := m.stepEntries[idx]
		s := entry.summary

		cursor := "  "
		if fi == m.stepCursor {
			cursor = "▸ "
		}

		// Collapse indicator
		//   ▾ = expanded
		//   ▸ = collapsed, detail loaded, has expandable content
		//     = collapsed, detail not loaded or no content
		levelMark := " "
		if entry.level >= levelExpanded {
			detail := m.stepDetailCache[s.Step]
			if detail == nil || hasExpandableContent(detail, s) {
				levelMark = "▾"
			}
		} else {
			if detail, ok := m.stepDetailCache[s.Step]; ok && hasExpandableContent(detail, s) {
				levelMark = "▸"
			}
		}

		dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))

		// Compact step+action suffix: "3 tool" / "1 plan" / "4 done"
		stepAction := dimStyle.Render(fmt.Sprintf("%d %s", s.Step, actionAbbrev(s.Action)))

		// Token
		var tokenLabel string
		if showToken {
			if s.TokenCount == 0 {
				tokenLabel = dimStyle.Render("    —")
			} else {
				tokenLabel = dimStyle.Render(fmt.Sprintf("%5s", formatTokenCount(s.TokenCount)))
			}
		}

		// Duration
		var durLabel string
		if showDuration {
			dur := formatTimelineDuration(s.DurationMs)
			durStyle := dimStyle
			if s.DurationMs > slowStepThresholdMs {
				durStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#E5C07B"))
			}
			durLabel = durStyle.Render(fmt.Sprintf("%6s", dur))
		}

		// Error mark
		hasError := s.HasError
		errMark := ""
		if hasError {
			errMark = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError)).Render(" ✗")
		}

		// Summary is the primary content — gets maximum width.
		// For CLI driver steps, Summary is often just the tool name (e.g. "read").
		// ToolPath contains richer context (e.g. "read:/path/to/file").
		// Prefer ToolPath when Summary is short (< 8 chars) and ToolPath is available.
		displaySummary := s.Summary
		if s.ToolPath != "" && len(s.Summary) < 8 {
			displaySummary = s.ToolPath
		}
		fixedWidth := 2 + 1 + 1 + 5 // cursor(2) + collapse(1) + space(1) + stepAction(5)
		if showToken {
			fixedWidth += 6
		}
		if showDuration {
			fixedWidth += 7
		}
		if hasError {
			fixedWidth += 2
		}
		summaryW := max(truncW-fixedWidth, 10)
		summaryText := truncateRuneWidth(displaySummary, summaryW)

		// Inline error preview from cached detail (after summary width is known)
		if hasError && width >= 80 {
			if cached := m.stepDetailCache[s.Step]; cached != nil && cached.ToolError != "" {
				errLine := strings.SplitN(cached.ToolError, "\n", 2)[0]
				errPreviewW := max(truncW-fixedWidth-runewidth.StringWidth(summaryText)-4, 10)
				if runewidth.StringWidth(errLine) > errPreviewW {
					errLine = runewidth.Truncate(errLine, errPreviewW-1, "…")
				}
				errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError))
				errMark = errStyle.Render(" ✗ " + errLine)
			}
		}

		// Build line
		var line string
		parts := []string{cursor, levelMark, summaryText}
		if showDuration {
			parts = append(parts, tokenLabel, durLabel, stepAction, errMark)
		} else if showToken {
			parts = append(parts, tokenLabel, stepAction, errMark)
		} else {
			parts = append(parts, stepAction, errMark)
		}
		line = strings.Join(parts, " ")

		if hasError {
			line = lipgloss.NewStyle().Background(lipgloss.Color("#3D1F1F")).Render(line)
		} else if fi == m.stepCursor {
			line = lipgloss.NewStyle().
				Background(lipgloss.Color("#2D2D3D")).
				Foreground(lipgloss.Color("#FFFFFF")).
				Render(line)
		}

		b.WriteString(truncateAnsi(line, truncW))
		b.WriteString("\n")
		linesUsed++

		// Level 2: Expanded detail
		if entry.level >= levelExpanded && linesUsed < listLines {
			detail := m.stepDetailCache[s.Step]
			if detail == nil {
				b.WriteString(dimStyle.Render("   ┊ Loading…") + "\n")
				linesUsed++
			} else {
				linesUsed += m.renderExpandedDetail(&b, detail, s, truncW, listLines-linesUsed)
			}
		}

		// Level 3: Debug detail
		if entry.level >= levelDebug && linesUsed < listLines {
			detail := m.stepDetailCache[s.Step]
			if detail != nil {
				linesUsed += m.renderDebugDetail(&b, detail, truncW, listLines-linesUsed)
			}
		}
	}

	return b.String()
}

// renderExpandedDetail renders Level 2 detail lines for a step.
func (m dashboardModel) renderExpandedDetail(b *strings.Builder, detail *ipc.GetStepDetailResponse, s ipc.StepSummaryWire, maxW, maxLines int) int {
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
	errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError))
	lines := 0
	contentW := max(maxW-14, 20) // 3 indent + "┊" + " " + 8 label + " " padding

	// ToolPath — skip when Level 1 already shows it as displaySummary
	displayedAsSummary := s.Summary
	if s.ToolPath != "" && len(s.Summary) < 8 {
		displayedAsSummary = s.ToolPath
	}
	if detail.ToolPath != "" && detail.ToolPath != displayedAsSummary && lines < maxLines {
		pathLabel := detail.ToolPath
		if runewidth.StringWidth(pathLabel) > contentW {
			pathLabel = runewidth.Truncate(pathLabel, contentW-1, "…")
		}
		pathStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#4EC9B0"))
		fmt.Fprintf(b, "   %s %s %s\n", dimStyle.Render("┊"), dimStyle.Render("   Path"), pathStyle.Render(pathLabel))
		lines++
	}

	// Input (tool_call only)
	if detail.ToolInput != "" && lines < maxLines {
		input := detail.ToolInput
		if runewidth.StringWidth(input) > contentW {
			totalBytes := len(detail.ToolInput)
			input = runewidth.Truncate(input, contentW-15, "") + fmt.Sprintf("… (%d bytes)", totalBytes)
		}
		fmt.Fprintf(b, "   %s %s %s\n", dimStyle.Render("┊"), dimStyle.Render("  Input"), input)
		lines++
	}

	// Result or Error
	if detail.ToolError != "" && lines < maxLines {
		errMsg := detail.ToolError
		errLines := strings.Split(errMsg, "\n")
		if len(errLines) > 3 {
			errMsg = strings.Join(errLines[:3], "\n") + fmt.Sprintf("\n… (%d more lines)", len(errLines)-3)
		}
		for i, el := range strings.Split(errMsg, "\n") {
			if lines >= maxLines {
				break
			}
			if i == 0 {
				fmt.Fprintf(b, "   %s %s %s\n", dimStyle.Render("┊"), dimStyle.Render("  Error"), errStyle.Render(el))
			} else {
				fmt.Fprintf(b, "   %s %s %s\n", dimStyle.Render("┊"), dimStyle.Render("       "), errStyle.Render(el))
			}
			lines++
		}
	} else if detail.ToolResult != "" && lines < maxLines {
		result := detail.ToolResult
		resultLines := strings.Split(result, "\n")
		if len(resultLines) > 3 {
			result = strings.Join(resultLines[:3], "\n") + fmt.Sprintf("\n… (%d more lines)", len(resultLines)-3)
		}
		for i, rl := range strings.Split(result, "\n") {
			if lines >= maxLines {
				break
			}
			if i == 0 {
				fmt.Fprintf(b, "   %s %s %s\n", dimStyle.Render("┊"), dimStyle.Render(" Result"), rl)
			} else {
				fmt.Fprintf(b, "   %s %s %s\n", dimStyle.Render("┊"), dimStyle.Render("       "), rl)
			}
			lines++
		}
	} else if detail.RawResponse != "" && lines < maxLines {
		// Show RawResponse snippet as fallback for any action type
		rawLines := strings.Split(detail.RawResponse, "\n")
		showLines := min(3, len(rawLines))
		for i := range showLines {
			if lines >= maxLines {
				break
			}
			fmt.Fprintf(b, "   %s %s\n", dimStyle.Render("┊"), rawLines[i])
			lines++
		}
		if len(rawLines) > 3 && lines < maxLines {
			fmt.Fprintf(b, "   %s %s\n", dimStyle.Render("┊"), dimStyle.Render(fmt.Sprintf("… (%d more lines)", len(rawLines)-3)))
			lines++
		}
	}

	// Token breakdown — skip when total already shown in Level 1 and breakdown matches
	if (detail.RequestTokens > 0 || detail.ResponseTokens > 0) && (s.TokenCount == 0 || detail.RequestTokens+detail.ResponseTokens != s.TokenCount) && lines < maxLines {
		fmt.Fprintf(b, "   %s %s %d req → %d resp\n", dimStyle.Render("┊"), dimStyle.Render("  Token"), detail.RequestTokens, detail.ResponseTokens)
		lines++
	}

	// Fallback: if all fields were deduped or empty (should be rare — hasExpandableContent prevents most cases)
	if lines == 0 {
		// Try full ToolPath if Level 1 showed a truncated Summary
		if detail.ToolPath != "" && detail.ToolPath != displayedAsSummary {
			tp := detail.ToolPath
			if runewidth.StringWidth(tp) > contentW {
				tp = runewidth.Truncate(tp, contentW-1, "…")
			}
			fmt.Fprintf(b, "   %s %s\n", dimStyle.Render("┊"), dimStyle.Render(tp))
			lines++
		}
		// Try RawResponse as last resort
		if lines == 0 && detail.RawResponse != "" {
			raw := detail.RawResponse
			if runewidth.StringWidth(raw) > contentW {
				raw = runewidth.Truncate(raw, contentW-1, "…")
			}
			fmt.Fprintf(b, "   %s %s\n", dimStyle.Render("┊"), dimStyle.Render(raw))
			lines++
		}
	}

	return lines
}

// renderDebugDetail renders Level 3 debug lines (messages preview).
func (m dashboardModel) renderDebugDetail(b *strings.Builder, detail *ipc.GetStepDetailResponse, maxW, maxLines int) int {
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
	lines := 0

	// Separator
	if lines < maxLines {
		sep := strings.Repeat("─", min(maxW-6, 60))
		fmt.Fprintf(b, "   %s\n", dimStyle.Render("┊"+sep))
		lines++
	}

	msgCount := len(detail.Messages)

	if msgCount == 0 {
		// CLI driver process: no message history available
		if lines < maxLines {
			fmt.Fprintf(b, "   %s %s\n", dimStyle.Render("┊"), dimStyle.Render("CLI driver — no message history"))
			lines++
		}
		if lines < maxLines {
			fmt.Fprintf(b, "   %s %s\n", dimStyle.Render("┊"), dimStyle.Render("                                      ↳ 按 p 查看 system prompt"))
			lines++
		}
		return lines
	}

	// Messages header
	if lines < maxLines {
		totalTok := formatTokenCount(detail.TokenCount)
		fmt.Fprintf(b, "   %s Messages (%d)%s%s\n",
			dimStyle.Render("┊"),
			detail.MessageCount,
			strings.Repeat(" ", max(maxW-30-len(totalTok), 2)),
			dimStyle.Render("累计 "+totalTok+" tok"))
		lines++
	}

	// Message preview
	showCount := min(msgCount, min(maxLines-lines, 6))
	showCount = max(showCount, min(msgCount, 2))
	// Show the last N messages
	startMsg := max(msgCount-showCount, 0)
	for i := startMsg; i < msgCount && lines < maxLines; i++ {
		msg := detail.Messages[i]
		roleStyle := promptRoleForRole(msg.Role)
		content := msg.Content
		if runewidth.StringWidth(content) > 60 {
			content = runewidth.Truncate(content, 57, "…")
		}
		fmt.Fprintf(b, "   %s  %s %s\n", dimStyle.Render("┊"), roleStyle, content)
		lines++
	}

	// Hint
	if lines < maxLines {
		fmt.Fprintf(b, "   %s %s\n", dimStyle.Render("┊"), dimStyle.Render("                                      ↳ 按 p 查看完整 prompt"))
		lines++
	}

	return lines
}

func promptRoleForRole(role string) string {
	switch role {
	case "system":
		return promptRoleSystem.Render("[system]")
	case "user":
		return promptRoleUser.Render("[user]  ")
	case "assistant":
		return promptRoleAssistant.Render("[asst]  ")
	case "tool":
		return promptRoleTool.Render("[tool]  ")
	default:
		return "[" + role + "]"
	}
}

// renderStepHeader renders the normal mode header.
func (m dashboardModel) renderStepHeader(maxW, total int, filtered []int) string {
	var b strings.Builder
	b.WriteString(" Timeline")
	if m.selectedPID > 0 {
		fmt.Fprintf(&b, " │ PID %d", m.selectedPID)
	}
	fmt.Fprintf(&b, " │ %d steps", total)

	// Total tokens from step summaries
	totalTok := 0
	for _, e := range m.stepEntries {
		totalTok += e.summary.TokenCount
	}
	if totalTok > 0 {
		fmt.Fprintf(&b, " │ %s tok", formatTokenCount(totalTok))
	}

	// Stage statistics (wide screens only)
	if maxW >= 100 && total > 0 {
		counts := make(map[string]int)
		errCount := 0
		for _, e := range m.stepEntries {
			counts[e.summary.Action]++
			if e.summary.HasError {
				errCount++
			}
		}
		b.WriteString(" │")
		for _, action := range []string{"plan", "tool_call", "spawn", "specialize", "replan", "text", "complete"} {
			if c, ok := counts[action]; ok && c > 0 {
				color := actionColor(action)
				label := actionAbbrev(action)
				fmt.Fprintf(&b, " %s", lipgloss.NewStyle().Foreground(color).Render(fmt.Sprintf("%s:%d", label, c)))
			}
		}
		if errCount > 0 {
			fmt.Fprintf(&b, " %s", lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError)).Render(fmt.Sprintf("err:%d", errCount)))
		}
	}

	// Scroll position (medium+ screens)
	if maxW >= 80 && len(filtered) > 0 {
		pos := min(m.stepCursor+1, len(filtered))
		fmt.Fprintf(&b, " │ %d/%d", pos, len(filtered))
	}

	// Filter indicator
	if len(filtered) < total {
		dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted))
		fmt.Fprintf(&b, "  %s", dimStyle.Render(fmt.Sprintf("filter: %d/%d", len(filtered), total)))
	}

	return truncateAnsi(b.String(), maxW)
}

// renderStepFilterBar renders the filter editing mode header.
func (m dashboardModel) renderStepFilterBar(maxW int) string {
	var b strings.Builder
	b.WriteString(" Filter:")

	types := []struct {
		key    string
		label  string
		action string
	}{
		{"T", actionAbbrev("tool_call"), "tool_call"},
		{"P", actionAbbrev("plan"), "plan"},
		{"A", actionAbbrev("text"), "text"},
		{"C", actionAbbrev("complete"), "complete"},
		{"S", actionAbbrev("spawn"), "spawn"},
		{"R", actionAbbrev("replan"), "replan"},
		{"Z", actionAbbrev("specialize"), "specialize"},
	}

	for _, t := range types {
		on := m.stepFilters == nil || m.stepFilters[t.action]
		mark := "✓"
		color := actionColor(t.action)
		if !on {
			mark = "·"
			color = lipgloss.Color(ui.ColorMuted)
		}
		catStyle := lipgloss.NewStyle().Foreground(color)
		fmt.Fprintf(&b, " [%s]%s %s", t.key, catStyle.Render(t.label), mark)
	}
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted))
	b.WriteString("  [*]all  " + dimStyle.Render("f/Esc:done"))
	return truncateAnsi(b.String(), maxW)
}

// hasExpandableContent checks whether expanding this step would show any new information.
// Returns true if detail is not yet loaded (unknown), or if at least one field survives dedup.
func hasExpandableContent(detail *ipc.GetStepDetailResponse, s ipc.StepSummaryWire) bool {
	if detail == nil {
		return true // not loaded yet — treat as potentially expandable
	}
	// ToolPath — new info if different from Level 1 display
	displayedAsSummary := s.Summary
	if s.ToolPath != "" && len(s.Summary) < 8 {
		displayedAsSummary = s.ToolPath
	}
	if detail.ToolPath != "" && detail.ToolPath != displayedAsSummary {
		return true
	}
	// ToolInput
	if detail.ToolInput != "" {
		return true
	}
	// ToolError or ToolResult
	if detail.ToolError != "" || detail.ToolResult != "" {
		return true
	}
	// RawResponse (fallback for any action type)
	if detail.RawResponse != "" {
		return true
	}
	// Token breakdown (only if it differs from Level 1 total)
	if (detail.RequestTokens > 0 || detail.ResponseTokens > 0) && (s.TokenCount == 0 || detail.RequestTokens+detail.ResponseTokens != s.TokenCount) {
		return true
	}
	return false
}

// --- Step item height estimation (for scroll) ---

// stepItemHeight returns the estimated number of lines a filtered item occupies.
func (m dashboardModel) stepItemHeight(entryIdx int) int {
	entry := m.stepEntries[entryIdx]
	n := 1 // Level 1 line always present
	if entry.level >= levelExpanded {
		detail := m.stepDetailCache[entry.summary.Step]
		if detail == nil {
			n++ // "Loading…"
		} else {
			n += m.estimateExpandedLines(detail, entry.summary)
		}
	}
	if entry.level >= levelDebug {
		detail := m.stepDetailCache[entry.summary.Step]
		if detail != nil {
			n += m.estimateDebugLines(detail)
		}
	}
	return n
}

func (m dashboardModel) estimateExpandedLines(detail *ipc.GetStepDetailResponse, s ipc.StepSummaryWire) int {
	n := 0
	// Path — skip when Level 1 already shows it as displaySummary
	displayedAsSummary := s.Summary
	if s.ToolPath != "" && len(s.Summary) < 8 {
		displayedAsSummary = s.ToolPath
	}
	if detail.ToolPath != "" && detail.ToolPath != displayedAsSummary {
		n++ // Path line
	}
	if detail.ToolInput != "" {
		n++ // Input line
	}
	if detail.ToolError != "" {
		errLines := strings.Count(detail.ToolError, "\n") + 1
		n += min(errLines, 4) // Error: up to 3 lines + overflow
	} else if detail.ToolResult != "" {
		resLines := strings.Count(detail.ToolResult, "\n") + 1
		n += min(resLines, 4) // Result: up to 3 lines + overflow
	} else if detail.RawResponse != "" {
		rawLines := strings.Count(detail.RawResponse, "\n") + 1
		n += min(rawLines, 4) // RawResponse fallback: up to 3 lines + overflow
	}
	// Token breakdown — skip when total already shown in Level 1 and breakdown matches
	if (detail.RequestTokens > 0 || detail.ResponseTokens > 0) && (s.TokenCount == 0 || detail.RequestTokens+detail.ResponseTokens != s.TokenCount) {
		n++ // Token line
	}
	// Fallback line when all content was deduped
	if n == 0 {
		n++ // always at least 1 line for fallback (ToolPath/RawResponse/no-detail)
	}
	return n
}

func (m dashboardModel) estimateDebugLines(detail *ipc.GetStepDetailResponse) int {
	n := 2 // separator + messages header
	msgCount := len(detail.Messages)
	n += min(msgCount, 6) // message preview lines
	n++                   // hint line
	return n
}

// ensureStepCursorVisible adjusts stepScrollTop so the cursor item is within the viewport.
func (m *dashboardModel) ensureStepCursorVisible(viewportLines int) {
	filtered := m.filteredStepEntries()
	if len(filtered) == 0 {
		m.stepScrollTop = 0
		return
	}
	cursor := min(m.stepCursor, len(filtered)-1)

	// If cursor is above scroll top, snap to cursor
	if cursor < m.stepScrollTop {
		m.stepScrollTop = cursor
		return
	}

	// Walk from scrollTop counting lines; if cursor fits, done
	linesUsed := 0
	for fi := m.stepScrollTop; fi <= cursor && fi < len(filtered); fi++ {
		h := m.stepItemHeight(filtered[fi])
		if fi == cursor && linesUsed+h <= viewportLines {
			return // cursor is visible
		}
		linesUsed += h
	}

	// Cursor not visible: scroll down until it fits
	// Walk backward from cursor filling the viewport
	linesUsed = m.stepItemHeight(filtered[cursor])
	newTop := cursor
	for newTop > 0 {
		h := m.stepItemHeight(filtered[newTop-1])
		if linesUsed+h > viewportLines {
			break
		}
		linesUsed += h
		newTop--
	}
	m.stepScrollTop = newTop
}

// --- Step timeline helpers ---

func formatTokenCount(tokens int) string {
	if tokens >= 1000 {
		return fmt.Sprintf("%.1fk", float64(tokens)/1000.0)
	}
	return fmt.Sprintf("%d", tokens)
}

func formatTimelineDuration(ms float64) string {
	if ms < 1 {
		return fmt.Sprintf("%.0fµs", ms*1000)
	}
	if ms < 1000 {
		return fmt.Sprintf("%.0fms", ms)
	}
	return fmt.Sprintf("%.2fs", ms/1000)
}

// truncateRuneWidth truncates a string to fit within maxWidth display columns.
func truncateRuneWidth(s string, maxWidth int) string {
	if runewidth.StringWidth(s) <= maxWidth {
		return s
	}
	return runewidth.Truncate(s, maxWidth-1, "…")
}

// truncateAnsi truncates ANSI-styled string by visible width.
func truncateAnsi(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= maxWidth {
		return s
	}
	return lipgloss.NewStyle().MaxWidth(maxWidth).Render(s)
}

// --- Step data flow ---

func (m dashboardModel) applyNewSteps(steps []ipc.StepSummaryWire) dashboardModel {
	// Build a set of already-known step numbers for dedup protection.
	// Concurrent fetches (e.g. PID-change fetch + tick fetch) can return
	// overlapping step ranges; appending duplicates causes the Timeline to
	// show the same entry twice.
	known := make(map[int]struct{}, len(m.stepEntries))
	for _, e := range m.stepEntries {
		known[e.summary.Step] = struct{}{}
	}

	for _, s := range steps {
		if _, dup := known[s.Step]; dup {
			continue
		}
		known[s.Step] = struct{}{}
		level := levelSummary
		autoExpand := false
		if s.HasError || s.DurationMs > slowStepThresholdMs {
			level = levelExpanded
			autoExpand = true
		}
		m.stepEntries = append(m.stepEntries, stepEntry{
			summary:    s,
			level:      level,
			autoExpand: autoExpand,
		})
	}
	return m
}

// isSelectedProcessDead returns true if the currently selected process is in Dead state.
func (m dashboardModel) isSelectedProcessDead() bool {
	if m.selectedPID == 0 {
		return false
	}
	for _, p := range m.processes {
		if p.PID == m.selectedPID && (m.selectedUUID == "" || p.UUID == m.selectedUUID) {
			return p.State == types.StateDead
		}
	}
	return false
}

// --- Auto-fetch for expanded steps ---

// fetchNextExpandedDetail returns a Cmd to fetch the next expanded step that has no cached detail.
// Returns nil if nothing to fetch or already fetching.
func (m dashboardModel) fetchNextExpandedDetail() tea.Cmd {
	if m.fetchingDetail || m.selectedPID == 0 {
		return nil
	}
	// Priority 1: expanded steps without cached detail
	for _, entry := range m.stepEntries {
		if entry.level >= levelExpanded && m.stepDetailCache[entry.summary.Step] == nil {
			return fetchStepDetailCmd(m.selectedPID, entry.summary.Step)
		}
	}
	// Priority 2: visible collapsed steps without cached detail (for ▸ indicator)
	filtered := m.filteredStepEntries()
	pageSize := max(m.dashboardVisibleLines()-4, 1)
	visStart := m.stepScrollTop
	visEnd := min(visStart+pageSize, len(filtered))
	for i := visStart; i < visEnd; i++ {
		idx := filtered[i]
		entry := m.stepEntries[idx]
		if entry.level < levelExpanded && m.stepDetailCache[entry.summary.Step] == nil {
			return fetchStepDetailCmd(m.selectedPID, entry.summary.Step)
		}
	}
	return nil
}

// --- Fetch commands ---

func fetchStepsCmd(uuid string, pid types.PID, afterStep int) tea.Cmd {
	return func() tea.Msg {
		client, err := ipc.Dial(ipc.SocketPath())
		if err != nil {
			return stepListMsg{uuid: uuid, pid: pid, err: err}
		}
		defer client.Close()
		var resp *ipc.ListStepsResponse
		if uuid != "" {
			resp, err = client.ListStepsByUUID(uuid, afterStep)
		} else {
			resp, err = client.ListSteps(pid, afterStep)
		}
		if err != nil {
			return stepListMsg{uuid: uuid, pid: pid, err: err}
		}
		return stepListMsg{uuid: uuid, pid: pid, steps: resp.Steps, total: resp.Total}
	}
}

func fetchStepDetailCmd(pid types.PID, step int) tea.Cmd {
	return func() tea.Msg {
		client, err := ipc.Dial(ipc.SocketPath())
		if err != nil {
			return stepDetailResultMsg{step: step, err: err}
		}
		defer client.Close()
		detail, err := client.GetStepDetail(pid, step)
		return stepDetailResultMsg{step: step, detail: detail, err: err}
	}
}

// --- Prompt Pager (Story 27-4, enhanced with Tab) ---

func formatPromptContent(detail *ipc.GetStepDetailResponse, _ int, tab promptPagerTab) string {
	switch tab {
	case promptTabSystem:
		return formatPromptSystemTab(detail)
	case promptTabTools:
		return formatPromptToolsTab(detail)
	default:
		return formatPromptMessagesTab(detail)
	}
}

func formatPromptMessagesTab(detail *ipc.GetStepDetailResponse) string {
	if len(detail.Messages) == 0 {
		dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
		return dimStyle.Render("No message history available.\n\n" +
			"CLI driver processes (Claude CLI / Cursor CLI) manage their\n" +
			"conversation history internally. Rnix can only observe tool\n" +
			"calls and their inputs/outputs, not the full prompt context.\n\n" +
			"For native-driver processes, this tab shows the complete\n" +
			"message history snapshot at this step.")
	}

	var b strings.Builder

	toolCallNames := make(map[string]string)
	for _, msg := range detail.Messages {
		for _, tc := range msg.ToolCalls {
			toolCallNames[tc.ID] = tc.Name
		}
	}

	for i, msg := range detail.Messages {
		if i > 0 {
			b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#888888")).Render(strings.Repeat("─", 70)) + "\n")
		}
		roleTag := formatRoleTag(msg, toolCallNames)
		fmt.Fprintf(&b, "%s\n", roleTag)
		b.WriteString(msg.Content)
		b.WriteString("\n\n")
	}

	return b.String()
}

func formatPromptSystemTab(detail *ipc.GetStepDetailResponse) string {
	var b strings.Builder
	sysLen := utf8.RuneCountInString(detail.SystemPrompt)
	fmt.Fprintf(&b, "═══ System Prompt (%s chars) ═══\n\n", formatCharCount(sysLen))
	b.WriteString(detail.SystemPrompt)
	return b.String()
}

func formatPromptToolsTab(detail *ipc.GetStepDetailResponse) string {
	var b strings.Builder
	nameStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#6BCB77")).Bold(true)
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))

	if detail.Action == "" {
		return dimStyle.Render("No tool information for this step.")
	}

	b.WriteString(nameStyle.Render(detail.Action))
	if detail.Summary != "" {
		b.WriteString(" — " + detail.Summary + "\n")
	} else {
		b.WriteString("\n")
	}
	if detail.ToolPath != "" {
		b.WriteString(dimStyle.Render("  Path: ") + detail.ToolPath + "\n")
	}
	if detail.ToolInput != "" {
		b.WriteString(dimStyle.Render("  Input: ") + detail.ToolInput + "\n")
	}
	if detail.ToolResult != "" {
		result := detail.ToolResult
		if len(result) > 500 {
			result = result[:500] + "..."
		}
		b.WriteString(dimStyle.Render("  Result: ") + result + "\n")
	}
	if detail.ToolError != "" {
		b.WriteString(dimStyle.Render("  Error: ") + detail.ToolError + "\n")
	}
	if detail.ToolDurationMs > 0 {
		b.WriteString(dimStyle.Render(fmt.Sprintf("  Duration: %.0fms", detail.ToolDurationMs)) + "\n")
	}
	return b.String()
}

func formatRoleTag(msg ipc.MessageWire, toolCallNames map[string]string) string {
	switch msg.Role {
	case "system":
		return promptRoleSystem.Render("[system]")
	case "user":
		return promptRoleUser.Render("[user]")
	case "assistant":
		return promptRoleAssistant.Render("[assistant]")
	case "tool":
		label := ""
		if name, ok := toolCallNames[msg.ToolCallID]; ok && name != "" {
			label = ":" + name
		} else if msg.ToolCallID != "" {
			label = ":" + msg.ToolCallID
		}
		return promptRoleTool.Render("[tool" + label + "]")
	default:
		return "[" + msg.Role + "]"
	}
}

func formatCharCount(n int) string {
	if n >= 1000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000.0)
	}
	return fmt.Sprintf("%d", n)
}

func (m *dashboardModel) enterPromptPager(detail *ipc.GetStepDetailResponse, step int) {
	content := formatPromptContent(detail, step, promptTabMessages)
	vp := viewport.New(viewport.WithWidth(m.width), viewport.WithHeight(max(m.height-2, 1)))
	vp.SetContent(content)
	m.promptViewport = vp
	m.promptContent = content
	m.promptStep = step
	m.promptPager = true
	m.promptTab = promptTabMessages
}

func (m dashboardModel) renderPromptPager() string {
	detail := m.stepDetailCache[m.promptStep]
	msgCount := 0
	tokenLabel := "0"
	if detail != nil {
		msgCount = detail.MessageCount
		tokenLabel = formatTokenCount(detail.TokenCount)
	}

	tabNames := []string{"Messages", "System", "Tools"}
	var tabs strings.Builder
	for i, name := range tabNames {
		if promptPagerTab(i) == m.promptTab {
			tabs.WriteString(lipgloss.NewStyle().Bold(true).Render("[" + name + "]"))
		} else {
			tabs.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#888888")).Render(" " + name + " "))
		}
		tabs.WriteString(" ")
	}

	title := fmt.Sprintf("  Prompt Viewer │ Step %d │ %d messages │ %s tok ─── %s",
		m.promptStep, msgCount, tokenLabel, tabs.String())
	help := "  j/k:scroll  PgUp/PgDn:page  Home/End:jump  Tab:switch  p/q:back"

	return lipgloss.JoinVertical(lipgloss.Left, title, m.promptViewport.View(), help)
}

func fetchStepDetailForPagerCmd(pid types.PID, step int) tea.Cmd {
	return func() tea.Msg {
		client, err := ipc.Dial(ipc.SocketPath())
		if err != nil {
			return promptPagerMsg{pid: pid, step: step, err: err}
		}
		defer client.Close()
		detail, err := client.GetStepDetail(pid, step)
		return promptPagerMsg{pid: pid, step: step, detail: detail, err: err}
	}
}
