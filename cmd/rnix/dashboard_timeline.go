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
	case "DriverToolCall":
		return catTool
	case "DriverThinking":
		return catLLM
	case "DriverInit", "DriverUser", "DriverEvent":
		return catVFS
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

// fetchEventsFromDiskCmd loads persisted syscall events for dead processes.
func fetchEventsFromDiskCmd(pid types.PID, uuid string) tea.Cmd {
	return func() tea.Msg {
		client, err := ipc.Dial(ipc.SocketPath())
		if err != nil {
			return timelineDiskEventsMsg{err: err}
		}
		defer client.Close()
		events, err := client.ListEvents(pid, uuid)
		return timelineDiskEventsMsg{events: events, err: err}
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
	m.timelineExpandedIdx = noExpandedEvent
	m.timelineFilterMode = false
	m.stepEntries = nil
	m.stepCursor = 0
	m.stepDetailCache = make(map[int]*ipc.GetStepDetailResponse)
	m.lastFetchedStep = 0
	m.fetchingDetail = false
	m.promptPager = false
	return m
}

func (m dashboardModel) handleTimelineKey(key string) dashboardModel {
	// 过滤模式拦截
	if m.timelineFilterMode {
		return m.handleTimelineFilterKey(key)
	}
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
	case "pgdown":
		visible := m.filteredTimelineEvents()
		pageSize := max(m.dashboardVisibleLines()-4, 1)
		m.timelineEventCursor = min(m.timelineEventCursor+pageSize, len(visible)-1)
	case "pgup":
		pageSize := max(m.dashboardVisibleLines()-4, 1)
		m.timelineEventCursor = max(m.timelineEventCursor-pageSize, 0)
	case "home", "g":
		m.timelineEventCursor = 0
	case "end", "G", "shift+G":
		visible := m.filteredTimelineEvents()
		if len(visible) > 0 {
			m.timelineEventCursor = len(visible) - 1
		}
	case "enter":
		if m.timelineExpandedIdx == m.timelineEventCursor {
			m.timelineExpandedIdx = noExpandedEvent
		} else {
			m.timelineExpandedIdx = m.timelineEventCursor
		}
	case "f":
		m.timelineFilterMode = true
	case "s":
		m.stepTimelineMode = true
		m.timelineExpandedIdx = noExpandedEvent
	}
	return m
}

func (m dashboardModel) handleStepTimelineKey(key string) dashboardModel {
	// 过滤模式拦截
	if m.timelineFilterMode {
		return m.handleTimelineFilterKey(key)
	}
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
	case "f":
		m.timelineFilterMode = true
	}
	return m
}

// handleTimelineFilterKey 处理过滤模式下的按键
func (m dashboardModel) handleTimelineFilterKey(key string) dashboardModel {
	if m.timelineFilters == nil {
		m.timelineFilters = defaultTimelineFilters()
	}
	switch key {
	case "l":
		m.timelineFilters[catLLM] = !m.timelineFilters[catLLM]
	case "t":
		m.timelineFilters[catTool] = !m.timelineFilters[catTool]
	case "i":
		m.timelineFilters[catIPC] = !m.timelineFilters[catIPC]
	case "v":
		m.timelineFilters[catVFS] = !m.timelineFilters[catVFS]
	case "a":
		m.timelineFilters[catLLM] = true
		m.timelineFilters[catTool] = true
		m.timelineFilters[catIPC] = true
		m.timelineFilters[catVFS] = true
		m.timelineFilters[catError] = true
	case "f", "esc":
		m.timelineFilterMode = false
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
	truncW := max(innerW-1, 1)

	// 标题栏：根据过滤模式显示不同内容
	if m.timelineFilterMode {
		b.WriteString(m.renderFilterBar(truncW))
	} else {
		b.WriteString(m.renderTimelineHeader(truncW))
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

	linesUsed := 0
	for i := startIdx; i < endIdx && linesUsed < listLines; i++ {
		ev := filtered[i]
		cursor := "  "
		if i == m.timelineEventCursor {
			cursor = "▸ "
		}

		line := cursor + formatEventLine(ev, truncW-2)
		b.WriteString(truncateAnsi(line, truncW))
		b.WriteString("\n")
		linesUsed++

		// 展开详情
		if m.timelineExpandedIdx == i && linesUsed < listLines {
			detailLines := renderEventDetail(ev, truncW)
			for _, dl := range detailLines {
				if linesUsed >= listLines {
					break
				}
				b.WriteString(truncateAnsi(dl, truncW))
				b.WriteString("\n")
				linesUsed++
			}
		}
	}

	return style.Render(b.String())
}

func (m dashboardModel) renderStepTimeline(width, height int) string {
	var b strings.Builder
	truncW := max(width-1, 1)
	total := len(m.stepEntries)

	if m.timelineFilterMode {
		b.WriteString(m.renderFilterBar(truncW))
	} else {
		b.WriteString(" Timeline")
		if m.selectedPID > 0 {
			fmt.Fprintf(&b, " │ PID %d", m.selectedPID)
		}
		fmt.Fprintf(&b, " │ %d steps", total)
		b.WriteString("  [s]syscall  f:filter")
	}
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
		b.WriteString(truncateAnsi(line, truncW))
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

// --- Resource-first event display ---

// renderTimelineHeader 渲染正常模式标题栏
func (m dashboardModel) renderTimelineHeader(maxW int) string {
	var b strings.Builder
	b.WriteString(" Timeline")
	if m.selectedPID > 0 {
		fmt.Fprintf(&b, " │ PID %d", m.selectedPID)
	}
	fmt.Fprintf(&b, " │ %d events", len(m.timelineEvents))

	// 过滤状态指示
	active := 0
	total := 4
	for _, cat := range []eventCategory{catLLM, catTool, catIPC, catVFS} {
		if m.timelineFilters == nil || m.timelineFilters[cat] {
			active++
		}
	}
	if active < total {
		dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted))
		fmt.Fprintf(&b, "  %s", dimStyle.Render(fmt.Sprintf("filter:%d/%d", active, total)))
	}
	b.WriteString("  f:filter")
	return truncateAnsi(b.String(), maxW)
}

// renderFilterBar 渲染过滤编辑模式标题栏
func (m dashboardModel) renderFilterBar(maxW int) string {
	var b strings.Builder
	b.WriteString(" Filter:")

	cats := []struct {
		key   string
		label string
		cat   eventCategory
	}{
		{"L", "LLM", catLLM},
		{"T", "Tool", catTool},
		{"I", "IPC", catIPC},
		{"V", "VFS", catVFS},
	}

	for _, c := range cats {
		on := m.timelineFilters == nil || m.timelineFilters[c.cat]
		mark := "✓"
		color := categoryColor(c.cat)
		if !on {
			mark = "·"
			color = ui.ColorMuted
		}
		catStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(color))
		fmt.Fprintf(&b, " [%s]%s %s", c.key, catStyle.Render(c.label), mark)
	}
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted))
	b.WriteString("  [A]ll  " + dimStyle.Render("f/Esc:done"))
	return truncateAnsi(b.String(), maxW)
}

// extractResource 从事件 Args 中提取最有意义的资源标识
func extractResource(ev ipc.SyscallEventWire) string {
	// 优先从 path 提取
	if path, ok := ev.Args["path"].(string); ok {
		return extractPathResource(path)
	}
	// tool 字段（如 DriverToolCall）
	if tool, ok := ev.Args["tool"].(string); ok {
		return extractPathResource(tool)
	}
	// Spawn: 用 intent
	if intent, ok := ev.Args["intent"].(string); ok {
		if utf8.RuneCountInString(intent) > 16 {
			return string([]rune(intent)[:13]) + "..."
		}
		return intent
	}
	// IPC: target_pid
	if target, ok := ev.Args["target_pid"]; ok {
		return fmt.Sprintf("→pid:%v", target)
	}
	if target, ok := ev.Args["target"]; ok {
		return fmt.Sprintf("→%v", target)
	}
	// Context: cid
	if cid, ok := ev.Args["cid"]; ok {
		return fmt.Sprintf("ctx:%v", cid)
	}
	// FD
	if fd, ok := ev.Args["fd"]; ok {
		return fmt.Sprintf("fd:%v", fd)
	}
	// size (CtxAlloc)
	if size, ok := ev.Args["size"]; ok {
		return fmt.Sprintf("size:%v", size)
	}
	return ""
}

func extractPathResource(path string) string {
	parts := strings.Split(path, "/")
	if len(parts) >= 4 && parts[0] == "" && parts[1] == "dev" {
		switch parts[2] {
		case "llm":
			return parts[3]
		case "shell":
			return parts[3]
		case "fs":
			rest := strings.Join(parts[3:], "/")
			if utf8.RuneCountInString(rest) > 20 {
				return string([]rune(rest)[:17]) + "..."
			}
			return rest
		case "mcp":
			return "mcp:" + parts[3]
		default:
			return strings.Join(parts[2:], "/")
		}
	}
	if utf8.RuneCountInString(path) > 20 {
		return string([]rune(path)[:17]) + "..."
	}
	return path
}

// formatEventLine 生成资源优先的事件行：TAG RESOURCE OP DUR [ERR]
func formatEventLine(ev timelineEvent, maxW int) string {
	catColor := categoryColor(ev.category)
	catStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(catColor))
	tag := catStyle.Render(fmt.Sprintf("%-4s", categoryLabel(ev.category)))

	resource := extractResource(ev.wire)
	if resource == "" {
		resource = ev.wire.Syscall
	}

	op := ev.wire.Syscall
	dur := formatTimelineDuration(ev.wire.DurationMs)
	durStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted))

	var line string
	if ev.wire.Error != "" {
		errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError))
		errMsg := ev.wire.Error
		if utf8.RuneCountInString(errMsg) > 20 {
			errMsg = string([]rune(errMsg)[:17]) + "..."
		}
		line = fmt.Sprintf("%s %-14s %-10s %s  %s",
			tag, resource, op, durStyle.Render(dur), errStyle.Render(errMsg))
	} else {
		line = fmt.Sprintf("%s %-14s %-10s %s",
			tag, resource, op, durStyle.Render(dur))
	}
	return line
}

// renderEventDetail 渲染展开事件的详情行
func renderEventDetail(ev timelineEvent, maxW int) []string {
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted))
	var lines []string

	// Path
	if path, ok := ev.wire.Args["path"].(string); ok {
		lines = append(lines, dimStyle.Render(fmt.Sprintf("  ├ Path:   %s", path)))
	}

	// 其他 Args
	args := make(map[string]any)
	for k, v := range ev.wire.Args {
		if k == "path" || k == "tool" {
			continue
		}
		args[k] = v
	}
	if len(args) > 0 {
		argStr := formatTimelineArgs(args, max(maxW-12, 20))
		lines = append(lines, dimStyle.Render(fmt.Sprintf("  ├ Args:   %s", argStr)))
	}

	// Result
	if ev.wire.Result != nil {
		result := fmt.Sprintf("%v", ev.wire.Result)
		if utf8.RuneCountInString(result) > 40 {
			result = string([]rune(result)[:37]) + "..."
		}
		lines = append(lines, dimStyle.Render(fmt.Sprintf("  ├ Result: %s", result)))
	}

	// Error
	if ev.wire.Error != "" {
		errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError))
		lines = append(lines, fmt.Sprintf("  ├ %s %s", dimStyle.Render("Error:"), errStyle.Render(ev.wire.Error)))
	}

	// Timestamp
	ts := fmt.Sprintf("%.3fs", float64(ev.wire.TimestampMs)/1000.0)
	last := "└"
	if ev.wire.TraceID != "" {
		last = "├"
	}
	lines = append(lines, dimStyle.Render(fmt.Sprintf("  %s Time:   %s", last, ts)))

	// TraceID
	if ev.wire.TraceID != "" {
		lines = append(lines, dimStyle.Render(fmt.Sprintf("  └ Trace:  %s", ev.wire.TraceID)))
	}

	return lines
}

// truncateAnsi 按可见宽度截断包含 ANSI 转义码的字符串
func truncateAnsi(s string, maxWidth int) string {
	if maxWidth <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= maxWidth {
		return s
	}
	return lipgloss.NewStyle().MaxWidth(maxWidth).Render(s)
}

// --- Step timeline logic (Story 27-3) ---

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
