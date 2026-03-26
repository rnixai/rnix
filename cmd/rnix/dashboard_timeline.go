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
func actionAbbrev(action string) string {
	switch action {
	case "tool_call":
		return "tool"
	case "complete":
		return "done"
	case "specialize":
		return "spec"
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

// handleTimelinePIDChange resets timeline state when selected PID changes.
func (m dashboardModel) handleTimelinePIDChange() dashboardModel {
	if m.selectedPID == m.timelineAttachedPID {
		return m
	}
	m.timelineAttachedPID = m.selectedPID
	m.stepEntries = nil
	m.stepCursor = 0
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
	case "end", "G", "shift+G":
		if len(filtered) > 0 {
			m.stepCursor = len(filtered) - 1
		}
	case "f":
		m.stepFilterMode = true
	}
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
	case "x":
		m.stepFilters["text"] = !m.stepFilters["text"]
	case "c":
		m.stepFilters["complete"] = !m.stepFilters["complete"]
	case "s":
		m.stepFilters["spawn"] = !m.stepFilters["spawn"]
	case "r":
		m.stepFilters["replan"] = !m.stepFilters["replan"]
	case "z":
		m.stepFilters["specialize"] = !m.stepFilters["specialize"]
	case "a":
		m.stepFilters = defaultStepFilters()
	case "f", "esc":
		m.stepFilterMode = false
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
	startIdx := 0
	if cursor >= listLines {
		startIdx = cursor - listLines + 1
	}
	endIdx := min(startIdx+listLines, len(filtered))

	// Layout params
	showDuration := width >= 100
	showTokenFull := width >= 90
	showToken := width >= 80
	useAbbrev := width < 80

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
		levelMark := " "
		if entry.level >= levelExpanded {
			levelMark = "▾"
		}

		// Step number
		dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
		stepLabel := dimStyle.Render(fmt.Sprintf("Step %-3d", s.Step))

		// Action with color
		actionStr := s.Action
		if useAbbrev {
			actionStr = actionAbbrev(actionStr)
		}
		actionStyle := lipgloss.NewStyle().Foreground(actionColor(s.Action))
		if s.Action == "complete" {
			actionStyle = actionStyle.Bold(true)
		}
		actionLabel := actionStyle.Render(fmt.Sprintf("%-12s", actionStr))

		// Token
		var tokenLabel string
		if showToken {
			if s.TokenCount == 0 {
				tokenLabel = dimStyle.Render("     —")
			} else if showTokenFull {
				tokenLabel = dimStyle.Render(fmt.Sprintf("%6s tok", formatTokenCount(s.TokenCount)))
			} else {
				tokenLabel = dimStyle.Render(fmt.Sprintf("%6stok", formatTokenCount(s.TokenCount)))
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
		errMark := ""
		if s.HasError {
			errMark = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError)).Render(" ✗")
		}

		// Summary (elastic width)
		fixedWidth := 2 + 1 + 8 + 1 + 12 + 1 // cursor + level + step + space + action + space
		if showToken {
			fixedWidth += 10
		}
		if showDuration {
			fixedWidth += 7
		}
		if s.HasError {
			fixedWidth += 2
		}
		summaryW := max(truncW-fixedWidth, 20)
		summary := truncateRuneWidth(s.Summary, summaryW)

		// Error step: highlight entire line
		var line string
		if showDuration {
			line = fmt.Sprintf("%s%s%s %s %s  %s  %s%s", cursor, levelMark, stepLabel, actionLabel, summary, tokenLabel, durLabel, errMark)
		} else if showToken {
			line = fmt.Sprintf("%s%s%s %s %s  %s%s", cursor, levelMark, stepLabel, actionLabel, summary, tokenLabel, errMark)
		} else {
			line = fmt.Sprintf("%s%s%s %s %s%s", cursor, levelMark, stepLabel, actionLabel, summary, errMark)
		}

		if s.HasError {
			line = lipgloss.NewStyle().Background(lipgloss.Color("#3D1F1F")).Render(line)
		} else if fi == m.stepCursor {
			line = lipgloss.NewStyle().Reverse(true).Render(line)
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
	} else if s.Action == "plan" || s.Action == "text" || s.Action == "complete" {
		// Show RawResponse snippet for non-tool actions
		if detail.RawResponse != "" && lines < maxLines {
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
	}

	// Token breakdown
	if lines < maxLines {
		fmt.Fprintf(b, "   %s %s %d req → %d resp\n", dimStyle.Render("┊"), dimStyle.Render("  Token"), detail.RequestTokens, detail.ResponseTokens)
		lines++
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
	msgCount := len(detail.Messages)
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
		{"T", "tool_call", "tool_call"},
		{"P", "plan", "plan"},
		{"X", "text", "text"},
		{"C", "complete", "complete"},
		{"S", "spawn", "spawn"},
		{"R", "replan", "replan"},
		{"Z", "specialize", "specialize"},
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
	b.WriteString("  [A]ll  " + dimStyle.Render("f/Esc:done"))
	return truncateAnsi(b.String(), maxW)
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
	for _, s := range steps {
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

// --- Fetch commands ---

func fetchStepsCmd(pid types.PID, afterStep int) tea.Cmd {
	return func() tea.Msg {
		client, err := ipc.Dial(ipc.SocketPath())
		if err != nil {
			return stepListMsg{err: err}
		}
		defer client.Close()
		resp, err := client.ListSteps(pid, afterStep)
		if err != nil {
			return stepListMsg{err: err}
		}
		return stepListMsg{steps: resp.Steps, total: resp.Total}
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
	fmt.Fprintf(&b, "═══ Tools (%d) ═══\n\n", len(detail.Tools))
	for i, tool := range detail.Tools {
		if i > 0 {
			b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("#888888")).Render(strings.Repeat("─", 70)) + "\n")
		}
		nameStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#6BCB77")).Bold(true)
		b.WriteString(nameStyle.Render(tool.Name))
		desc := tool.Description
		if desc == "" {
			desc = "(no description)"
		}
		b.WriteString(" — " + desc + "\n")
		if len(tool.Parameters) > 0 {
			dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
			b.WriteString(dimStyle.Render(fmt.Sprintf("  Parameters: %v", tool.Parameters)) + "\n")
		}
		b.WriteString("\n")
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
