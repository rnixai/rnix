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

// sysEventStyle returns a lipgloss style for a system event type.
func sysEventStyle(ev UnifiedEvent) lipgloss.Style {
	switch ev.Type {
	case EventCompact:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#00CED1")).Bold(true)
	case EventBudget:
		return lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorWarning))
	case EventSpawn:
		return lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorSuccess))
	case EventExit:
		if ev.Severity >= SevError {
			return lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError))
		}
		return lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted))
	case EventStall:
		return lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorWarning)).Bold(true)
	case EventImmune:
		return lipgloss.NewStyle().Foreground(lipgloss.Color("#9B59B6"))
	default:
		return lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted))
	}
}

// defaultStepFilters returns a map with all action types and system event types enabled.
func defaultStepFilters() map[string]bool {
	return map[string]bool{
		"tool_call":    true,
		"plan":         true,
		"text":         true,
		"complete":     true,
		"spawn":        true,
		"replan":       true,
		"specialize":   true,
		EventCompact:   true,
		EventBudget:    true,
		"sys_spawn":    true, // F7: distinct from step-action "spawn" filter key
		EventExit:      true,
		EventStall:     true,
		EventImmune:    true,
		EventSyscall:   true, // Story 34.6: strace events in debug mode
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
	m.expandedAggGroups = make(map[int]bool)
	return m
}

// handleTimelineKey dispatches keys for the unified Step timeline.
func (m dashboardModel) handleTimelineKey(key string) dashboardModel {
	if m.stepFilterMode {
		return m.handleStepFilterKey(key)
	}
	filtered := m.filteredUnifiedEvents()
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
		m.statusMsg = "Filter: t/p/a/c/s/r/z (step) | C/b/x/X/T/i (sys) | * all | Esc exit"
		m.statusMsgTTL = statusMsgDefaultTTL
	case "e":
		// Expand all visible step events that have expandable content
		expanded := 0
		for _, ev := range filtered {
			if ev.StepEntry != nil {
				entry := ev.StepEntry
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
		// Collapse all visible step events to Level 1
		for _, ev := range filtered {
			if ev.StepEntry != nil {
				ev.StepEntry.level = levelSummary
			}
		}
	case "n":
		// Jump to next error: step event with HasError or system event with Severity >= SevError
		found := false
		for i := m.stepCursor + 1; i < len(filtered); i++ {
			ev := filtered[i]
			if ev.StepEntry != nil && ev.StepEntry.summary.HasError {
				m.stepCursor = i
				found = true
				break
			}
			if ev.StepEntry == nil && ev.Severity >= SevError {
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
		// Jump to previous error
		found := false
		for i := m.stepCursor - 1; i >= 0; i-- {
			ev := filtered[i]
			if ev.StepEntry != nil && ev.StepEntry.summary.HasError {
				m.stepCursor = i
				found = true
				break
			}
			if ev.StepEntry == nil && ev.Severity >= SevError {
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
	// Row 1: Step action types (lowercase)
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
	// Row 2: System event types (uppercase or distinct keys)
	// F7: Use 'x' for system spawn and 'X' for exit per spec Task 5.4
	case "C":
		m.stepFilters[EventCompact] = !m.stepFilters[EventCompact]
	case "b":
		m.stepFilters[EventBudget] = !m.stepFilters[EventBudget]
	case "x":
		m.stepFilters["sys_spawn"] = !m.stepFilters["sys_spawn"]
	case "X":
		m.stepFilters[EventExit] = !m.stepFilters[EventExit]
	case "T":
		m.stepFilters[EventStall] = !m.stepFilters[EventStall]
	case "i":
		m.stepFilters[EventImmune] = !m.stepFilters[EventImmune]
	case "*":
		m.stepFilters = defaultStepFilters()
	case "f", "esc":
		m.stepFilterMode = false
	default:
		m.statusMsg = "Filter: t/p/a/c/s/r/z (step) | C/b/x/X/T/i (sys) | * all | Esc exit"
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

// isEventVisible checks if a unified event passes the current filters.
func isEventVisible(ev UnifiedEvent, filters map[string]bool) bool {
	if len(filters) == 0 {
		return true
	}
	if ev.Type == EventStep {
		if ev.StepEntry != nil {
			return filters[ev.StepEntry.summary.Action]
		}
		return true
	}
	// F7: system spawn uses distinct filter key "sys_spawn" to avoid collision with step-action "spawn"
	if ev.Type == EventSpawn {
		return filters["sys_spawn"]
	}
	return filters[ev.Type]
}

// filteredUnifiedEvents returns unified events matching current filters.
func (m dashboardModel) filteredUnifiedEvents() []UnifiedEvent {
	if len(m.stepFilters) == 0 {
		return m.unifiedEvents
	}
	allOn := true
	for _, v := range m.stepFilters {
		if !v {
			allOn = false
			break
		}
	}
	if allOn {
		return m.unifiedEvents
	}
	var result []UnifiedEvent
	for _, ev := range m.unifiedEvents {
		if isEventVisible(ev, m.stepFilters) {
			result = append(result, ev)
		}
	}
	return result
}

// unifiedItemHeight returns the number of lines a unified event occupies.
func (m dashboardModel) unifiedItemHeight(ev UnifiedEvent) int {
	if ev.StepEntry == nil {
		return 1 // system events are always single-line
	}
	entry := ev.StepEntry
	n := 1
	if entry.level >= levelExpanded {
		detail := m.stepDetailCache[entry.summary.Step]
		if detail == nil {
			n++
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

// resolveStepIndex converts cursor position in filtered unified view to actual stepEntries index.
// Returns -1 if cursor points to a system event or is out of range.
func (m dashboardModel) resolveStepIndex() int {
	filtered := m.filteredUnifiedEvents()
	if m.stepCursor < 0 || m.stepCursor >= len(filtered) {
		return -1
	}
	ev := filtered[m.stepCursor]
	if ev.StepEntry == nil {
		return -1
	}
	for i := range m.stepEntries {
		if &m.stepEntries[i] == ev.StepEntry {
			return i
		}
	}
	return -1
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

	return renderFixedPanel(m.renderStepTimeline(innerW, innerH), width, height, borderColor)
}

func (m dashboardModel) renderStepTimeline(width, height int) string {
	var b strings.Builder
	truncW := max(width-1, 1)
	total := len(m.stepEntries)
	filtered := m.filteredUnifiedEvents()

	// Count step events and system events in filtered list
	stepCount := 0
	sysCount := 0
	for _, ev := range filtered {
		if ev.Type == EventStep {
			stepCount++
		} else {
			sysCount++
		}
	}

	// Header
	if m.stepFilterMode {
		b.WriteString(m.renderStepFilterBar(truncW))
	} else {
		b.WriteString(m.renderUnifiedStepHeader(truncW, total, len(filtered), sysCount))
	}
	b.WriteString("\n")

	if m.selectedPID == 0 {
		b.WriteString("\n    Select an agent to view timeline")
		return b.String()
	}

	if total == 0 && len(m.unifiedEvents) == 0 {
		if m.isSelectedProcessDead() {
			b.WriteString("\n    No steps recorded.")
			for _, p := range m.processes {
				if p.PID == m.selectedPID && (m.selectedUUID == "" || p.UUID == m.selectedUUID) {
					if p.Result != "" {
						b.WriteString("\n    Exit: " + p.Result)
					}
					break
				}
			}
		} else {
			b.WriteString("\n    Waiting for steps…")
		}
		return b.String()
	}

	if len(filtered) == 0 {
		b.WriteString("\n    No events match filter")
		return b.String()
	}

	// Aggregation mode: when >100 step events, group step events into chunks (Story 30.8 AC#5)
	// F2: System events are rendered inline between aggregation groups.
	const aggThreshold = 100
	useAggregation := stepCount > aggThreshold

	cursor := min(m.stepCursor, max(len(filtered)-1, 0))

	// F4: Filter bar is 2 lines when active; account for extra line
	headerLines := 1
	if m.stepFilterMode {
		headerLines = 2
	}
	listLines := max(height-headerLines-1, 1)

	// Variable-height scroll: ensure cursor is visible via stepScrollTop
	startIdx := m.stepScrollTop
	if startIdx < 0 || startIdx >= len(filtered) {
		startIdx = 0
	}
	if cursor < startIdx {
		startIdx = cursor
	}
	{
		linesUsed := 0
		cursorVisible := false
		for fi := startIdx; fi < len(filtered); fi++ {
			h := m.unifiedItemHeight(filtered[fi])
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
			linesUsed := m.unifiedItemHeight(filtered[cursor])
			startIdx = cursor
			for startIdx > 0 {
				h := m.unifiedItemHeight(filtered[startIdx-1])
				if linesUsed+h > listLines {
					break
				}
				linesUsed += h
				startIdx--
			}
		}
	}
	endIdx := len(filtered)

	showDuration := width >= 90
	showToken := width >= 70
	showStepOffset := width >= 110

	linesUsed := 0

	// Aggregation rendering path (Story 30.8 AC#5)
	if useAggregation {
		// F2: Extract step indices for aggregation; render system events inline after
		var stepIndices []int
		var sysEventsForAgg []UnifiedEvent
		for _, ev := range filtered {
			if ev.StepEntry != nil {
				for i := range m.stepEntries {
					if &m.stepEntries[i] == ev.StepEntry {
						stepIndices = append(stepIndices, i)
						break
					}
				}
			} else {
				sysEventsForAgg = append(sysEventsForAgg, ev)
			}
		}
		aggLines := m.renderAggregatedTimeline(&b, stepIndices, truncW, listLines, showToken, showDuration)
		// Render system events after aggregation groups (always visible)
		remaining := listLines - aggLines
		for _, ev := range sysEventsForAgg {
			if remaining <= 0 {
				break
			}
			icon := ui.EventTypeIcon(ev.Type)
			style := sysEventStyle(ev)
			line := fmt.Sprintf("   %s %s", style.Render(icon), style.Render(ev.Summary))
			b.WriteString(truncateAnsi(line, truncW))
			b.WriteString("\n")
			remaining--
		}
		return b.String()
	}

	for fi := startIdx; fi < endIdx && linesUsed < listLines; fi++ {
		ev := filtered[fi]

		// System event: single-line rendering (plus optional detail line when selected)
		if ev.StepEntry == nil {
			cursorMark := "  "
			if fi == m.stepCursor {
				cursorMark = "▸ "
			}
			icon := ui.EventTypeIcon(ev.Type)
			style := sysEventStyle(ev)
			tsLabel := ""
			if !ev.Timestamp.IsZero() {
				tsLabel = lipgloss.NewStyle().Foreground(lipgloss.Color("#888888")).
					Render(ui.FormatWallClock(ev.Timestamp)) + " "
			}
			line := fmt.Sprintf("%s%s%s %s", cursorMark, tsLabel, style.Render(icon), style.Render(ev.Summary))
			if fi == m.stepCursor {
				line = lipgloss.NewStyle().
					Background(lipgloss.Color("#2D2D3D")).
					Foreground(lipgloss.Color("#FFFFFF")).
					Render(line)
			}
			b.WriteString(truncateAnsi(line, truncW))
			b.WriteString("\n")
			linesUsed++
			// Show full Detail on a second line when cursor is on a sys event with detail
			if fi == m.stepCursor && ev.Detail != "" && linesUsed < listLines {
				detailStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
				detail := strings.ReplaceAll(ev.Detail, "\n", " ")
				detailLine := "    ┊ " + detail
				b.WriteString(truncateAnsi(detailStyle.Render(detailLine), truncW))
				b.WriteString("\n")
				linesUsed++
			}
			continue
		}

		// Step event: full Level 1/2/3 rendering (existing logic)
		entry := ev.StepEntry
		s := entry.summary

		cursorMark := "  "
		if fi == m.stepCursor {
			cursorMark = "▸ "
		}

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

		stepAction := dimStyle.Render(fmt.Sprintf("%d %s", s.Step, actionAbbrev(s.Action)))

		var tokenLabel string
		if showToken {
			if s.TokenCount == 0 {
				tokenLabel = dimStyle.Render("    —")
			} else {
				tokenLabel = dimStyle.Render(fmt.Sprintf("%5s", formatTokenCount(s.TokenCount)))
			}
		}

		var durLabel string
		if showDuration {
			dur := formatTimelineDuration(s.DurationMs)
			durStyle := dimStyle
			if s.DurationMs > slowStepThresholdMs {
				durStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#E5C07B"))
			}
			durLabel = durStyle.Render(fmt.Sprintf("%6s", dur))
		}

		// Step offset from process start (wide screens only)
		var offsetLabel string
		if showStepOffset && !ev.Timestamp.IsZero() {
			for _, p := range m.processes {
				if p.PID == m.selectedPID && (m.selectedUUID == "" || p.UUID == m.selectedUUID) {
					if !p.CreatedAt.IsZero() {
						offset := ev.Timestamp.Sub(p.CreatedAt)
						if offset >= 0 {
							offsetLabel = dimStyle.Render(fmt.Sprintf("%8s", ui.FormatOffsetFromStart(offset)))
						}
					}
					break
				}
			}
		}

		hasError := s.HasError
		errMark := ""
		if hasError {
			errMark = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError)).Render(" ✗")
		}

		displaySummary := s.Summary
		if s.ToolPath != "" && len(s.Summary) < 8 {
			displaySummary = s.ToolPath
		}
		fixedWidth := 2 + 1 + 1 + 5
		if showToken {
			fixedWidth += 6
		}
		if showDuration {
			fixedWidth += 7
		}
		if offsetLabel != "" {
			fixedWidth += 9
		}
		if hasError {
			fixedWidth += 2
		}
		summaryW := max(truncW-fixedWidth, 10)
		summaryText := truncateRuneWidth(displaySummary, summaryW)

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

		var line string
		parts := []string{cursorMark, levelMark, summaryText}
		if offsetLabel != "" {
			parts = append(parts, offsetLabel)
		}
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

// renderUnifiedStepHeader renders the timeline header with unified event counts.
func (m dashboardModel) renderUnifiedStepHeader(maxW, totalSteps, filteredCount, sysCount int) string {
	var b strings.Builder
	b.WriteString(" Timeline")
	if m.selectedPID > 0 {
		fmt.Fprintf(&b, " │ PID %d", m.selectedPID)
	}
	// Wall-clock start time for selected process
	for _, p := range m.processes {
		if p.PID == m.selectedPID && (m.selectedUUID == "" || p.UUID == m.selectedUUID) {
			if !p.CreatedAt.IsZero() {
				fmt.Fprintf(&b, " │ %s", ui.FormatWallClock(p.CreatedAt))
			}
			break
		}
	}
	fmt.Fprintf(&b, " │ %d steps", totalSteps)
	if sysCount > 0 {
		fmt.Fprintf(&b, " + %d events", sysCount)
	}

	// Total tokens from step summaries
	totalTok := 0
	for _, e := range m.stepEntries {
		totalTok += e.summary.TokenCount
	}
	if totalTok > 0 {
		fmt.Fprintf(&b, " │ %s tok", formatTokenCount(totalTok))
	}

	// Stage statistics (wide screens only)
	if maxW >= 100 && totalSteps > 0 {
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
	if maxW >= 80 && filteredCount > 0 {
		pos := min(m.stepCursor+1, filteredCount)
		fmt.Fprintf(&b, " │ %d/%d", pos, filteredCount)
	}

	// Filter indicator
	totalEvents := len(m.unifiedEvents)
	if filteredCount < totalEvents {
		// Build list of disabled type names for quick insight
		allTypes := []struct{ key, label string }{
			{"tool_call", "tool"}, {"plan", "plan"}, {"text", "txt"},
			{"complete", "done"}, {"spawn", "spn"}, {"replan", "rpl"}, {"specialize", "spec"},
			{EventCompact, "cmp"}, {EventBudget, "bgt"}, {"sys_spawn", "sspn"},
			{EventExit, "exit"}, {EventStall, "stl"}, {EventImmune, "imm"},
		}
		var hidden []string
		for _, t := range allTypes {
			if !m.stepFilters[t.key] {
				hidden = append(hidden, t.label)
			}
		}
		dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted))
		label := fmt.Sprintf("filter: %d/%d", filteredCount, totalEvents)
		if len(hidden) > 0 {
			label += " -" + strings.Join(hidden, ",")
		}
		fmt.Fprintf(&b, "  %s", dimStyle.Render(label))
	}

	return truncateAnsi(b.String(), maxW)
}

// renderStepFilterBar renders the filter editing mode header with two rows.
func (m dashboardModel) renderStepFilterBar(maxW int) string {
	var b strings.Builder
	b.WriteString(" Step:  ")

	// Row 1: Step action types
	stepTypes := []struct {
		key    string
		label  string
		action string
	}{
		{"t", actionAbbrev("tool_call"), "tool_call"},
		{"p", actionAbbrev("plan"), "plan"},
		{"a", actionAbbrev("text"), "text"},
		{"c", actionAbbrev("complete"), "complete"},
		{"s", actionAbbrev("spawn"), "spawn"},
		{"r", actionAbbrev("replan"), "replan"},
		{"z", actionAbbrev("specialize"), "specialize"},
	}

	for _, t := range stepTypes {
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

	// Row 2: System event types
	b.WriteString("\n Events:")

	sysTypes := []struct {
		key       string
		label     string
		eventType string
	}{
		{"C", "compact", EventCompact},
		{"b", "budget", EventBudget},
		{"x", "spawn", "sys_spawn"},
		{"X", "exit", EventExit},
		{"T", "stall", EventStall},
		{"i", "immune", EventImmune},
	}

	for _, t := range sysTypes {
		on := m.stepFilters == nil || m.stepFilters[t.eventType]
		mark := "✓"
		color := lipgloss.Color("#00CED1") // default system event color
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

// renderAggregatedTimeline renders a timeline with aggregation groups for long tasks (>100 steps).
// Steps are grouped into chunks of 50. Collapsed groups show a summary line;
// expanded groups show individual step lines.
func (m dashboardModel) renderAggregatedTimeline(b *strings.Builder, filtered []int, truncW, listLines int, showToken, showDuration bool) int {
	const aggGroupSize = 50
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))

	type aggGroup struct {
		startIdx   int // index in filtered array
		endIdx     int // exclusive
		firstStep  int // step number of first entry
		lastStep   int // step number of last entry
		actionCounts map[string]int
		errCount   int
		totalTokens int
	}

	// Build groups
	var groups []aggGroup
	for i := 0; i < len(filtered); i += aggGroupSize {
		end := min(i+aggGroupSize, len(filtered))
		g := aggGroup{
			startIdx:     i,
			endIdx:       end,
			actionCounts: make(map[string]int),
		}
		for fi := i; fi < end; fi++ {
			entry := m.stepEntries[filtered[fi]]
			s := entry.summary
			if fi == i {
				g.firstStep = s.Step
			}
			g.lastStep = s.Step
			g.actionCounts[s.Action]++
			if s.HasError {
				g.errCount++
			}
			g.totalTokens += s.TokenCount
		}
		groups = append(groups, g)
	}

	linesUsed := 0
	cursorFilterIdx := min(m.stepCursor, len(filtered)-1)

	// F3: Calculate group heights and find start group for viewport scrolling
	cursorGroupIdx := 0
	if cursorFilterIdx >= 0 && aggGroupSize > 0 {
		cursorGroupIdx = cursorFilterIdx / aggGroupSize
	}
	if cursorGroupIdx >= len(groups) {
		cursorGroupIdx = max(len(groups)-1, 0)
	}

	groupHeights := make([]int, len(groups))
	for gi, g := range groups {
		if m.expandedAggGroups[gi] {
			groupHeights[gi] = 1 + (g.endIdx - g.startIdx) // header + entries
		} else {
			groupHeights[gi] = 1
		}
	}

	// Find start group: scroll forward until cursor group fits in view
	startGi := 0
	for startGi < cursorGroupIdx {
		linesFromStart := 0
		for gi := startGi; gi <= cursorGroupIdx; gi++ {
			linesFromStart += groupHeights[gi]
		}
		if linesFromStart <= listLines {
			break
		}
		startGi++
	}

	for gi := startGi; gi < len(groups) && linesUsed < listLines; gi++ {
		g := groups[gi]
		if linesUsed >= listLines {
			break
		}

		isExpanded := m.expandedAggGroups[gi]
		// Check if cursor is in this group
		cursorInGroup := cursorFilterIdx >= g.startIdx && cursorFilterIdx < g.endIdx

		if !isExpanded {
			// Render aggregation summary line
			marker := "▸"
			cursorMark := "  "
			if cursorInGroup {
				cursorMark = "▸ "
				marker = " " // cursor mark replaces fold marker to avoid double ▸
			}

			// Build action summary
			var actionParts []string
			for _, action := range []string{"tool_call", "plan", "text", "spawn", "specialize", "replan", "complete"} {
				if c, ok := g.actionCounts[action]; ok && c > 0 {
					color := actionColor(action)
					label := actionAbbrev(action)
					actionParts = append(actionParts, lipgloss.NewStyle().Foreground(color).Render(fmt.Sprintf("%d %s", c, label)))
				}
			}
			actionSummary := strings.Join(actionParts, ", ")

			errPart := ""
			if g.errCount > 0 {
				noun := "error"
				if g.errCount > 1 {
					noun = "errors"
				}
				errPart = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError)).Render(fmt.Sprintf(", %d %s", g.errCount, noun))
			}

			line := fmt.Sprintf("%s%s Steps %d-%d: %s%s",
				cursorMark, marker, g.firstStep, g.lastStep, actionSummary, errPart)

			if showToken && g.totalTokens > 0 {
				line += dimStyle.Render(fmt.Sprintf("  %s", formatTokenCount(g.totalTokens)))
			}

			if cursorInGroup {
				line = lipgloss.NewStyle().
					Background(lipgloss.Color("#2D2D3D")).
					Foreground(lipgloss.Color("#FFFFFF")).
					Render(line)
			}

			b.WriteString(truncateAnsi(line, truncW))
			b.WriteString("\n")
			linesUsed++
		} else {
			// Render expanded: group header + individual steps
			header := fmt.Sprintf("  ▾ Steps %d-%d", g.firstStep, g.lastStep)
			b.WriteString(dimStyle.Render(header))
			b.WriteString("\n")
			linesUsed++

			for fi := g.startIdx; fi < g.endIdx && linesUsed < listLines; fi++ {
				idx := filtered[fi]
				entry := m.stepEntries[idx]
				s := entry.summary

				cursorMark := "  "
				if fi == m.stepCursor {
					cursorMark = "▸ "
				}

				stepAction := dimStyle.Render(fmt.Sprintf("%d %s", s.Step, actionAbbrev(s.Action)))

				var tokenLabel string
				if showToken {
					if s.TokenCount == 0 {
						tokenLabel = dimStyle.Render("    —")
					} else {
						tokenLabel = dimStyle.Render(fmt.Sprintf("%5s", formatTokenCount(s.TokenCount)))
					}
				}

				var durLabel string
				if showDuration {
					dur := formatTimelineDuration(s.DurationMs)
					durStyle := dimStyle
					if s.DurationMs > slowStepThresholdMs {
						durStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#E5C07B"))
					}
					durLabel = durStyle.Render(fmt.Sprintf("%6s", dur))
				}

				errMark := ""
				if s.HasError {
					errMark = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError)).Render(" ✗")
				}

				displaySummary := s.Summary
				if s.ToolPath != "" && len(s.Summary) < 8 {
					displaySummary = s.ToolPath
				}
				fixedWidth := 2 + 1 + 1 + 5
				if showToken {
					fixedWidth += 6
				}
				if showDuration {
					fixedWidth += 7
				}
				if s.HasError {
					fixedWidth += 2
				}
				summaryW := max(truncW-fixedWidth, 10)
				summaryText := truncateRuneWidth(displaySummary, summaryW)

				var parts []string
				parts = append(parts, cursorMark, " ", summaryText)
				if showDuration {
					parts = append(parts, tokenLabel, durLabel, stepAction, errMark)
				} else if showToken {
					parts = append(parts, tokenLabel, stepAction, errMark)
				} else {
					parts = append(parts, stepAction, errMark)
				}
				line := strings.Join(parts, " ")

				if s.HasError {
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
			}
		}
	}

	return linesUsed
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
	filtered := m.filteredUnifiedEvents()
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
		h := m.unifiedItemHeight(filtered[fi])
		if fi == cursor && linesUsed+h <= viewportLines {
			return // cursor is visible
		}
		linesUsed += h
	}

	// Cursor not visible: scroll down until it fits
	// Walk backward from cursor filling the viewport
	linesUsed = m.unifiedItemHeight(filtered[cursor])
	newTop := cursor
	for newTop > 0 {
		h := m.unifiedItemHeight(filtered[newTop-1])
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
		if s.HasError {
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
	filteredEvs := m.filteredUnifiedEvents()
	pageSize := max(m.dashboardVisibleLines()-4, 1)
	visStart := m.stepScrollTop
	visEnd := min(visStart+pageSize, len(filteredEvs))
	for i := visStart; i < visEnd; i++ {
		ev := filteredEvs[i]
		if ev.StepEntry == nil {
			continue
		}
		entry := ev.StepEntry
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
