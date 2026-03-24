package main

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"charm.land/bubbles/v2/viewport"
	"github.com/charmbracelet/lipgloss"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"

	tea "charm.land/bubbletea/v2"
)

// --- Timeline logic (Story 17-2) ---

func classifySyscall(ev ipc.SyscallEventWire) eventCategory {
	if ev.Error != "" {
		return catError
	}
	if isLLMEvent(ev) {
		return catLLM
	}
	switch ev.Syscall {
	case "Send", "Recv", "Pipe", "Signal", "SigBlock", "SigUnblock",
		"JoinGroup", "LeaveGroup", "GetProcGroup", "SignalGroup":
		return catIPC
	case "Spawn", "Kill", "Wait", "Reparent", "SpawnThread", "JoinThread",
		"SpawnSupervisor", "SpawnCoroutine", "Yield", "ResumeCoroutine":
		return catTool
	}
	if isToolPathEvent(ev) {
		return catTool
	}
	return catVFS
}

func isLLMEvent(ev ipc.SyscallEventWire) bool {
	for _, key := range []string{"path", "tool"} {
		if v, ok := ev.Args[key]; ok {
			if s, ok := v.(string); ok && strings.Contains(s, "/dev/llm/") {
				return true
			}
		}
	}
	return false
}

func isToolPathEvent(ev ipc.SyscallEventWire) bool {
	if v, ok := ev.Args["path"]; ok {
		if s, ok := v.(string); ok {
			return strings.Contains(s, "/dev/shell/") || strings.Contains(s, "/dev/fs/")
		}
	}
	return false
}

func categoryColor(cat eventCategory) string {
	switch cat {
	case catLLM:
		return ui.ColorAgent
	case catTool:
		return ui.ColorSuccess
	case catIPC:
		return colorIPC
	case catVFS:
		return ui.ColorWarning
	case catError:
		return ui.ColorError
	default:
		return ui.ColorMuted
	}
}

func categoryLabel(cat eventCategory) string {
	switch cat {
	case catLLM:
		return "LLM"
	case catTool:
		return "Tool"
	case catIPC:
		return "IPC"
	case catVFS:
		return "VFS"
	case catError:
		return "Err"
	default:
		return "?"
	}
}

func defaultTimelineFilters() map[eventCategory]bool {
	return map[eventCategory]bool{
		catLLM:   true,
		catTool:  true,
		catIPC:   true,
		catVFS:   true,
		catError: true,
	}
}

func (m dashboardModel) handleTimelineEvent(msg timelineEventMsg) dashboardModel {
	ev := timelineEvent{
		wire:     msg.event,
		category: classifySyscall(msg.event),
	}
	m.timelineEvents = append(m.timelineEvents, ev)
	if len(m.timelineEvents) > maxTimelineEvents {
		m.timelineEvents = m.timelineEvents[len(m.timelineEvents)-maxTimelineEvents:]
	}
	return m
}

func startTimelineCmd(pid types.PID) tea.Cmd {
	return func() tea.Msg {
		client, err := ipc.Dial(ipc.SocketPath())
		if err != nil {
			return timelineStreamDoneMsg{}
		}
		ch := make(chan ipc.SyscallEventWire, 64)
		stopCh := make(chan struct{})
		go func() {
			defer close(ch)
			defer client.Close()
			_ = client.AttachDebug(pid, func(ev ipc.SyscallEventWire) {
				select {
				case ch <- ev:
				case <-stopCh:
				}
			})
		}()
		return timelineStreamStartedMsg{eventCh: ch, stopCh: stopCh}
	}
}

func waitTimelineEventCmd(ch <-chan ipc.SyscallEventWire) tea.Cmd {
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return timelineStreamDoneMsg{}
		}
		return timelineEventMsg{event: ev}
	}
}

func (m dashboardModel) stopTimelineStream() dashboardModel {
	if m.timelineStopCh != nil {
		close(m.timelineStopCh)
		m.timelineStopCh = nil
	}
	m.timelineEventCh = nil
	return m
}

func (m dashboardModel) handleTimelinePIDChange() dashboardModel {
	if m.selectedPID == m.timelineAttachedPID {
		return m
	}
	m.timelineEvents = nil
	m.timelineZoomLevel = 0
	m.timelineViewStart = 0
	m.timelineEventCursor = 0
	m.timelineAttachedPID = m.selectedPID
	m.stepEntries = nil
	m.stepCursor = 0
	m.stepDetailCache = make(map[int]*ipc.GetStepDetailResponse)
	m.lastFetchedStep = 0
	m.fetchingDetail = false
	m.promptPager = false
	return m
}

func (m dashboardModel) handleTimelineKey(key string) dashboardModel {
	if m.stepTimelineMode {
		return m.handleStepTimelineKey(key)
	}
	if m.timelineFilters == nil {
		m.timelineFilters = defaultTimelineFilters()
	}
	switch key {
	case "+", "=":
		if m.timelineZoomLevel < 5 {
			m.timelineZoomLevel++
		}
	case "-":
		if m.timelineZoomLevel > 0 {
			m.timelineZoomLevel--
		}
	case "h", "left":
		step := m.timelineScrollStep()
		if m.timelineViewStart > step {
			m.timelineViewStart -= step
		} else {
			m.timelineViewStart = 0
		}
	case "l", "right":
		m.timelineViewStart += m.timelineScrollStep()
	case "up", "k":
		if m.timelineEventCursor > 0 {
			m.timelineEventCursor--
		}
	case "down", "j":
		visible := m.filteredTimelineEvents()
		if m.timelineEventCursor < len(visible)-1 {
			m.timelineEventCursor++
		}
	case "!":
		m.timelineFilters[catLLM] = !m.timelineFilters[catLLM]
	case "@":
		m.timelineFilters[catTool] = !m.timelineFilters[catTool]
	case "#":
		m.timelineFilters[catIPC] = !m.timelineFilters[catIPC]
	case "$":
		m.timelineFilters[catVFS] = !m.timelineFilters[catVFS]
	case "s":
		m.stepTimelineMode = true
	}
	return m
}

func (m dashboardModel) handleStepTimelineKey(key string) dashboardModel {
	switch key {
	case "up", "k":
		if m.stepCursor > 0 {
			m.stepCursor--
		}
	case "down", "j":
		if m.stepCursor < len(m.stepEntries)-1 {
			m.stepCursor++
		}
	case "s":
		m.stepTimelineMode = false
	case "!":
		if m.timelineFilters == nil {
			m.timelineFilters = defaultTimelineFilters()
		}
		m.timelineFilters[catLLM] = !m.timelineFilters[catLLM]
	case "@":
		if m.timelineFilters == nil {
			m.timelineFilters = defaultTimelineFilters()
		}
		m.timelineFilters[catTool] = !m.timelineFilters[catTool]
	case "#":
		if m.timelineFilters == nil {
			m.timelineFilters = defaultTimelineFilters()
		}
		m.timelineFilters[catIPC] = !m.timelineFilters[catIPC]
	case "$":
		if m.timelineFilters == nil {
			m.timelineFilters = defaultTimelineFilters()
		}
		m.timelineFilters[catVFS] = !m.timelineFilters[catVFS]
	}
	return m
}

func (m dashboardModel) timelineScrollStep() int64 {
	if m.timelineZoomLevel >= 1 && m.timelineZoomLevel < len(zoomWindowMs) {
		return zoomWindowMs[m.timelineZoomLevel] / 10
	}
	return 500
}

func (m dashboardModel) filteredTimelineEvents() []timelineEvent {
	if m.timelineFilters == nil {
		return m.timelineEvents
	}
	var result []timelineEvent
	for _, ev := range m.timelineEvents {
		if m.timelineFilters[ev.category] {
			result = append(result, ev)
		}
	}
	return result
}

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

	if m.stepTimelineMode {
		return style.Render(m.renderStepTimeline(innerW, innerH))
	}

	var b strings.Builder

	b.WriteString(" Timeline")
	if m.selectedPID > 0 {
		fmt.Fprintf(&b, " | PID %d", m.selectedPID)
	}
	fmt.Fprintf(&b, " | %d events", len(m.timelineEvents))

	b.WriteString("  ")
	for _, cat := range []eventCategory{catLLM, catTool, catIPC, catVFS} {
		label := categoryLabel(cat)
		if m.timelineFilters != nil && !m.timelineFilters[cat] {
			b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted)).Render("[" + label + "]"))
		} else {
			b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(categoryColor(cat))).Render("[" + label + "]"))
		}
	}
	b.WriteString("\n")

	if m.selectedPID == 0 {
		b.WriteString("\n    Select an agent to view timeline")
		return style.Render(b.String())
	}

	filtered := m.filteredTimelineEvents()

	// AC-10: Auto-filter for Dead processes — only show events for selectedPID
	pidFiltered := false
	if m.isSelectedProcessDead() && m.selectedPID > 0 {
		filtered = filterTimelineByPID(filtered, m.selectedPID)
		pidFiltered = true
	}

	if len(filtered) == 0 {
		if pidFiltered {
			b.WriteString("\n    No events for this process")
		} else {
			b.WriteString("\n    Waiting for events...")
		}
		return style.Render(b.String())
	}

	barWidth := max(innerW-2, 10)
	b.WriteString(m.renderTimelineBar(filtered, barWidth))
	b.WriteString("\n")

	listLines := max(innerH-3, 1)
	if m.timelineEventCursor >= len(filtered) {
		m.timelineEventCursor = len(filtered) - 1
	}

	startIdx := 0
	if m.timelineEventCursor >= listLines {
		startIdx = m.timelineEventCursor - listLines + 1
	}
	endIdx := min(startIdx+listLines, len(filtered))

	for i := startIdx; i < endIdx; i++ {
		ev := filtered[i]
		cursor := "  "
		if i == m.timelineEventCursor {
			cursor = "▸ "
		}

		ts := fmt.Sprintf("[%7.3fs]", float64(ev.wire.TimestampMs)/1000.0)
		dur := formatTimelineDuration(ev.wire.DurationMs)
		catColor := categoryColor(ev.category)
		catStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(catColor))

		args := formatTimelineArgs(ev.wire.Args, 30)
		line := fmt.Sprintf("%s%s %s(%s) %s",
			cursor, ts, catStyle.Render(ev.wire.Syscall), args, dur)
		if ev.wire.Error != "" {
			line += " " + lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError)).Render("ERR:"+ev.wire.Error)
		}
		b.WriteString(line)
		b.WriteString("\n")
	}

	return style.Render(b.String())
}

func (m dashboardModel) renderStepTimeline(width, height int) string {
	var b strings.Builder

	b.WriteString(" Timeline")
	if m.selectedPID > 0 {
		fmt.Fprintf(&b, " | PID %d", m.selectedPID)
	}
	total := len(m.stepEntries)
	fmt.Fprintf(&b, " | %d/%d steps", total, total)
	b.WriteString("\n")

	if total == 0 {
		b.WriteString("\n    Waiting for steps...")
		return b.String()
	}

	listLines := max(height-2, 1)
	startIdx := 0
	if m.stepCursor >= listLines {
		startIdx = m.stepCursor - listLines + 1
	}
	endIdx := min(startIdx+listLines, total)

	for i := startIdx; i < endIdx; i++ {
		entry := m.stepEntries[i]
		s := entry.summary

		cursor := "  "
		if i == m.stepCursor {
			cursor = "▸ "
		}

		errMark := ""
		if s.HasError {
			errMark = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError)).Render(" ✗")
		}

		dur := formatTimelineDuration(s.DurationMs)
		line := fmt.Sprintf("%sStep %d/%d  %s  %s  %s%s",
			cursor, s.Step, total, s.Action, s.Summary, dur, errMark)
		b.WriteString(line)
		b.WriteString("\n")

		if entry.level >= levelExpanded {
			detail := m.stepDetailCache[s.Step]
			if detail != nil {
				if detail.ToolInput != "" {
					input := detail.ToolInput
					if utf8.RuneCountInString(input) > 60 {
						runes := []rune(input)
						input = string(runes[:57]) + "..."
					}
					fmt.Fprintf(&b, "      Input: %s\n", input)
				}
				if detail.ToolResult != "" {
					result := detail.ToolResult
					if utf8.RuneCountInString(result) > 60 {
						runes := []rune(result)
						result = string(runes[:57]) + "..."
					}
					fmt.Fprintf(&b, "      Result: %s\n", result)
				}
				if detail.ToolError != "" {
					fmt.Fprintf(&b, "      Error: %s\n",
						lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError)).Render(detail.ToolError))
				}
				fmt.Fprintf(&b, "      Tokens: %d req / %d resp\n", detail.RequestTokens, detail.ResponseTokens)
			}
		}

		if entry.level >= levelDebug {
			detail := m.stepDetailCache[s.Step]
			if detail != nil {
				fmt.Fprintf(&b, "      Messages: %d  Tokens: %s\n",
					detail.MessageCount, formatTokenCount(detail.TokenCount))
				if len(detail.Messages) > 0 {
					preview := detail.Messages[0].Content
					if utf8.RuneCountInString(preview) > 60 {
						runes := []rune(preview)
						preview = string(runes[:57]) + "..."
					}
					fmt.Fprintf(&b, "      [%s] %s\n", detail.Messages[0].Role, preview)
				}
			}
		}
	}

	return b.String()
}

func formatTokenCount(tokens int) string {
	if tokens >= 1000 {
		return fmt.Sprintf("%.1fk", float64(tokens)/1000.0)
	}
	return fmt.Sprintf("%d", tokens)
}

func (m dashboardModel) renderTimelineBar(events []timelineEvent, width int) string {
	if len(events) == 0 || width <= 0 {
		return ""
	}

	minTs := events[0].wire.TimestampMs
	maxTs := events[len(events)-1].wire.TimestampMs
	if m.timelineZoomLevel > 0 && m.timelineZoomLevel < len(zoomWindowMs) {
		windowMs := zoomWindowMs[m.timelineZoomLevel]
		minTs = m.timelineViewStart
		maxTs = m.timelineViewStart + windowMs
	}

	span := maxTs - minTs
	if span <= 0 {
		span = 1
	}

	bar := make([]eventCategory, width)
	barSet := make([]bool, width)
	for _, ev := range events {
		pos := int((ev.wire.TimestampMs - minTs) * int64(width) / span)
		if pos < 0 || pos >= width {
			continue
		}
		if !barSet[pos] || ev.category == catError {
			bar[pos] = ev.category
			barSet[pos] = true
		}
	}

	var b strings.Builder
	for i := range width {
		if !barSet[i] {
			b.WriteString("·")
		} else {
			catStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(categoryColor(bar[i])))
			b.WriteString(catStyle.Render("█"))
		}
	}
	return b.String()
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

func formatTimelineArgs(args map[string]any, maxLen int) string {
	if len(args) == 0 {
		return ""
	}
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		v := fmt.Sprintf("%v", args[k])
		if utf8.RuneCountInString(v) > 20 {
			runes := []rune(v)
			v = string(runes[:17]) + "..."
		}
		parts = append(parts, k+"="+v)
	}
	result := strings.Join(parts, ", ")
	if utf8.RuneCountInString(result) > maxLen {
		runes := []rune(result)
		result = string(runes[:maxLen-3]) + "..."
	}
	return result
}

// --- Step timeline logic (Story 27-3) ---

// isSelectedProcessDead returns true if the currently selected process is in Dead state.
func (m dashboardModel) isSelectedProcessDead() bool {
	if m.selectedPID == 0 {
		return false
	}
	for _, p := range m.processes {
		if p.PID == m.selectedPID {
			return p.State == types.StateDead
		}
	}
	return false
}

// filterTimelineByPID returns only events whose PID matches the given PID.
func filterTimelineByPID(events []timelineEvent, pid types.PID) []timelineEvent {
	var result []timelineEvent
	for _, ev := range events {
		if ev.wire.PID == pid {
			result = append(result, ev)
		}
	}
	return result
}

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

// --- Prompt Pager (Story 27-4) ---

func formatPromptContent(detail *ipc.GetStepDetailResponse, step int) string {
	var b strings.Builder

	sysLen := utf8.RuneCountInString(detail.SystemPrompt)
	fmt.Fprintf(&b, "═══ System Prompt (%s chars) ═══\n\n", formatCharCount(sysLen))
	b.WriteString(detail.SystemPrompt)
	b.WriteString("\n\n")

	tokenLabel := formatTokenCount(detail.TokenCount)
	fmt.Fprintf(&b, "═══ Messages (%d msgs, ~%s tokens) ═══\n\n", detail.MessageCount, tokenLabel)

	toolCallNames := make(map[string]string)
	for _, msg := range detail.Messages {
		for _, tc := range msg.ToolCalls {
			toolCallNames[tc.ID] = tc.Name
		}
	}

	for _, msg := range detail.Messages {
		roleTag := formatRoleTag(msg, toolCallNames)
		content := msg.Content
		contentLen := utf8.RuneCountInString(content)
		if contentLen > promptContentTruncateLimit {
			runes := []rune(content)
			content = string(runes[:promptContentTruncateLimit]) + fmt.Sprintf("\n... (truncated, %d chars total)", contentLen)
		}
		fmt.Fprintf(&b, "%s %s\n\n", roleTag, content)
	}

	fmt.Fprintf(&b, "═══ Tools (%d) ═══\n\n", len(detail.Tools))
	for _, tool := range detail.Tools {
		desc := tool.Description
		if desc == "" {
			desc = "(no description)"
		}
		fmt.Fprintf(&b, "• %s — %s\n", tool.Name, desc)
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
	content := formatPromptContent(detail, step)
	vp := viewport.New(viewport.WithWidth(m.width), viewport.WithHeight(max(m.height-2, 1)))
	vp.SetContent(content)
	m.promptViewport = vp
	m.promptContent = content
	m.promptStep = step
	m.promptPager = true
}

func (m dashboardModel) renderPromptPager() string {
	detail := m.stepDetailCache[m.promptStep]
	msgCount := 0
	tokenLabel := "0"
	if detail != nil {
		msgCount = detail.MessageCount
		tokenLabel = formatTokenCount(detail.TokenCount)
	}

	title := fmt.Sprintf("  Prompt View | PID %d Step %d | %d msgs ~%s tokens",
		m.selectedPID, m.promptStep, msgCount, tokenLabel)
	help := "  j/k:scroll  PgUp/PgDn:page  Home/End:jump  q:back"

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
