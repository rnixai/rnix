package main

import (
	"fmt"
	"strings"
	"unicode/utf8"

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

// timelineAggThreshold is the minimum consecutive steps with the same ToolPath
// required to trigger semantic aggregation (non-bulk mode, <100 steps).
const timelineAggThreshold = 3

// toolAggGroup represents a consecutive run of steps sharing the same ToolPath.
type toolAggGroup struct {
	startIdx int    // index in filtered unified events
	endIdx   int    // exclusive
	toolPath string // shared ToolPath for the group
	stepNums []int  // step numbers for display
}

// buildToolAggGroups scans unified events and identifies consecutive runs of
// step events with the same ToolPath that meet the aggregation threshold.
func buildToolAggGroups(events []UnifiedEvent) []toolAggGroup {
	var groups []toolAggGroup
	n := len(events)
	i := 0
	for i < n {
		ev := events[i]
		if ev.StepEntry == nil || ev.StepEntry.Summary.ToolPath == "" {
			i++
			continue
		}
		tp := ev.StepEntry.Summary.ToolPath
		runStart := i
		var stepNums []int
		stepNums = append(stepNums, ev.StepEntry.Summary.Step)
		j := i + 1
		for j < n {
			ej := events[j]
			if ej.StepEntry == nil || ej.StepEntry.Summary.ToolPath != tp {
				break
			}
			stepNums = append(stepNums, ej.StepEntry.Summary.Step)
			j++
		}
		if len(stepNums) >= timelineAggThreshold {
			groups = append(groups, toolAggGroup{
				startIdx: runStart,
				endIdx:   j,
				toolPath: tp,
				stepNums: stepNums,
			})
		}
		i = j
	}
	return groups
}

// shortenArgs takes the first line of input and truncates to maxLen rune-width.
func shortenArgs(input string, maxLen int) string {
	line, _, _ := strings.Cut(input, "\n")
	if runewidth.StringWidth(line) > maxLen {
		return runewidth.Truncate(line, maxLen-1, "…")
	}
	return line
}

// formatDefaultLine derives the display action and summary for a timeline step.
// When detail is available, it uses the richer fields; otherwise falls back to
// StepSummaryWire fields.
func formatDefaultLine(s ipc.StepSummaryWire, detail *ipc.GetStepDetailResponse) (action, summary string) {
	if detail != nil {
		action = detail.Action
		if detail.ToolPath != "" {
			action = detail.ToolPath
		}
		summary = detail.Summary
		if summary == "" && detail.ToolInput != "" {
			summary = shortenArgs(detail.ToolInput, 60)
		}
	}
	// Fallback: use StepSummaryWire fields
	if action == "" {
		action = s.Action
		if s.ToolPath != "" {
			action = s.ToolPath
		}
	}
	if summary == "" {
		summary = s.Summary
	}
	return action, summary
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

// maybeShowTimelineMigrationNotice shows a one-time status bar notice when the
// user enters升序 Timeline for the first time. Persists the shown flag to
// ~/.config/rnix/ui-state.json so the notice never repeats across sessions.
// Story 36-4 AC-3.
func (m dashboardModel) maybeShowTimelineMigrationNotice() dashboardModel {
	if m.timeline.MigrationChecked {
		return m
	}
	m.timeline.MigrationChecked = true
	if m.timeline.UIState == nil {
		m.timeline.UIState = &ui.UIState{}
	}
	if m.timeline.UIState.TimelineSortMigrationShown || !m.timeline.SortAsc {
		return m
	}
	m.statusMsg = "Timeline 已改为升序（最新在底）。按 o 切换。"
	m.statusMsgTTL = 5
	m.timeline.UIState.TimelineSortMigrationShown = true
	_ = ui.SaveUIState(m.timeline.UIState) // 写入失败不阻塞 UI
	return m
}

// handleTimelinePIDChange resets timeline state when selected process changes.
// Uses UUID for reliable identification (PIDs can be reused).
func (m dashboardModel) handleTimelinePIDChange() dashboardModel {
	if m.selectedUUID == m.timeline.AttachedUUID {
		return m
	}
	m.timeline.AttachedPID = m.selectedPID
	m.timeline.AttachedUUID = m.selectedUUID
	m.timeline.StepEntries = nil
	m.timeline.StepCursor = 0
	m.timeline.StepScrollTop = 0
	m.timeline.StepDetailCache = make(map[int]*ipc.GetStepDetailResponse)
	m.timeline.LastFetchedStep = 0
	m.timeline.FetchingDetail = false
	m.timeline.StepFilterMode = false
	m.timeline.StepExpandedIdx = -1
	m.timeline.ExpandedAggGroups = make(map[int]bool)
	// Story 36-4: expandMode 按进程作用域，切 PID 重置为 collapsed（sortAsc 不重置）
	m.timeline.ExpandMode = expandModeCollapsed
	// Story 36-5 P-1: search state is per-process; reset to avoid carrying a
	// stale "ghost input mode" or stale matches across PID switches.
	m.search.Mode = false
	m.search.Query = ""
	m.search.Matches = nil
	m.search.MatchIdx = 0
	return m
}

// handleTimelineKey dispatches keys for the unified Step timeline.
func (m dashboardModel) handleTimelineKey(key string) dashboardModel {
	if m.timeline.StepFilterMode {
		return m.handleStepFilterKey(key)
	}
	// Story 36-5: search input mode for Timeline
	if m.search.Mode {
		return m.handleTimelineSearchKey(key)
	}
	filtered := m.filteredUnifiedEvents()
	// Story 36-5: 统一导航键集合
	pageSize := max(m.dashboardVisibleLines()-4, 1)
	// Story 36-5 P-4: itemCount must reflect the list the cursor indexes into.
	// Use len(filtered) in steady state. Only fall back to len(stepEntries) when
	// unifiedEvents has not been built yet (early frame / test setup), NOT when
	// the user's filters happen to exclude everything. This prevents stepCursor
	// from being driven past the filtered range during interactive filtering.
	itemCount := len(filtered)
	if itemCount == 0 && len(m.unifiedEvents) == 0 {
		itemCount = len(m.timeline.StepEntries)
	}
	navOpts := ui.ListNavOpts{
		PageSize: pageSize,
		OnCursorChange: func(int) {
			m.ensureStepCursorVisible(pageSize)
		},
	}
	if ui.HandleListKey(key, nil, &m.timeline.StepCursor, itemCount, navOpts) {
		// g/home 额外重置 stepScrollTop（与 Tree 对齐）
		if key == "g" || key == "home" {
			m.timeline.StepScrollTop = 0
		}
		m.ensureStepCursorVisible(pageSize)
		return m
	}
	switch key {
	case "/":
		// Story 36-5: enter search input mode (Timeline).
		m.search.Mode = true
		m.search.Query = ""
		return m
	case "f":
		m.timeline.StepFilterMode = true
		m.statusMsg = "Filter: t/p/a/c/s/r/z (step) | C/b/x/X/T/i (sys) | * all | Esc exit"
		m.statusMsgTTL = statusMsgDefaultTTL
	case "e":
		// Story 36-4: Sticky expand mode — 切换到 Expanded，幂等。
		m.timeline.ExpandMode = expandModeExpanded
		expanded := 0
		for i := range m.timeline.StepEntries {
			entry := &m.timeline.StepEntries[i]
			if entry.Level < levelExpanded {
				detail := m.timeline.StepDetailCache[entry.Summary.Step]
				if detail == nil || hasExpandableContent(detail, entry.Summary) {
					entry.Level = levelExpanded
					expanded++
				}
			}
		}
		m.statusMsg = "Expand mode: all"
		m.statusMsgTTL = statusMsgDefaultTTL
		if expanded == 0 && len(m.timeline.StepEntries) == 0 {
			m.statusMsg = "Expand mode: all (no steps yet)"
		}
	case "E":
		// Story 36-4: ErrorsOnly mode — 仅展开 HasError=true 的 step。
		m.timeline.ExpandMode = expandModeErrorsOnly
		for i := range m.timeline.StepEntries {
			entry := &m.timeline.StepEntries[i]
			if entry.Summary.HasError {
				entry.Level = levelExpanded
			} else {
				entry.Level = levelSummary
			}
		}
		m.statusMsg = "Expand mode: errors only"
		m.statusMsgTTL = statusMsgDefaultTTL
	case "C":
		// Story 36-4: Collapsed mode — 全部折叠到 summary。
		// 仅非 filter 模式生效（filter 模式下的 C 由 handleStepFilterKey 处理）。
		m.timeline.ExpandMode = expandModeCollapsed
		for i := range m.timeline.StepEntries {
			m.timeline.StepEntries[i].Level = levelSummary
		}
		m.statusMsg = "Expand mode: collapsed"
		m.statusMsgTTL = statusMsgDefaultTTL
	case "o":
		// Story 36-4: 切换 Timeline 排序方向（升 ↔ 降）
		m.timeline.SortAsc = !m.timeline.SortAsc
		if m.timeline.SortAsc {
			m.statusMsg = "Timeline 已切换到升序（旧→新）"
		} else {
			m.statusMsg = "Timeline 已切换到降序（新→旧）"
		}
		m.statusMsgTTL = statusMsgDefaultTTL
	case "n":
		// Story 36-5 P-7: when a search is active, n cycles to the next match.
		// Otherwise, fall back to the legacy "next error" semantics (AC-5).
		if len(m.search.Matches) > 0 {
			n := len(m.search.Matches)
			m.search.MatchIdx = (m.search.MatchIdx + 1) % n
			m.timeline.StepCursor = m.search.Matches[m.search.MatchIdx]
			m.ensureStepCursorVisible(max(m.dashboardVisibleLines()-4, 1))
			break
		}
		// Jump to next error: step event with HasError or system event with Severity >= SevError
		found := false
		for i := m.timeline.StepCursor + 1; i < len(filtered); i++ {
			ev := filtered[i]
			if ev.StepEntry != nil && ev.StepEntry.Summary.HasError {
				m.timeline.StepCursor = i
				found = true
				break
			}
			if ev.StepEntry == nil && ev.Severity >= SevError {
				m.timeline.StepCursor = i
				found = true
				break
			}
		}
		if !found {
			m.statusMsg = "No more errors"
			m.statusMsgTTL = statusMsgDefaultTTL
		}
	case "N", "shift+N":
		// Story 36-5 P-7: same modal split for N (previous match vs previous error).
		if len(m.search.Matches) > 0 {
			n := len(m.search.Matches)
			m.search.MatchIdx = ((m.search.MatchIdx-1)%n + n) % n
			m.timeline.StepCursor = m.search.Matches[m.search.MatchIdx]
			m.ensureStepCursorVisible(max(m.dashboardVisibleLines()-4, 1))
			break
		}
		// Jump to previous error
		found := false
		for i := m.timeline.StepCursor - 1; i >= 0; i-- {
			ev := filtered[i]
			if ev.StepEntry != nil && ev.StepEntry.Summary.HasError {
				m.timeline.StepCursor = i
				found = true
				break
			}
			if ev.StepEntry == nil && ev.Severity >= SevError {
				m.timeline.StepCursor = i
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

// handleTimelineSearchKey handles search input in Timeline. Story 36-5 AC-12.
func (m dashboardModel) handleTimelineSearchKey(key string) dashboardModel {
	switch key {
	case "esc":
		m.search.Mode = false
		m.search.Query = ""
	case "enter":
		m.search.Mode = false
		if m.search.Query == "" {
			return m
		}
		// Story 36-5 P-7: collect ALL match indices (not just the first), so n/N
		// can cycle through them per the modal n/N semantics.
		q := strings.ToLower(m.search.Query)
		filtered := m.filteredUnifiedEvents()
		var matches []int
		for i, ev := range filtered {
			hay := strings.ToLower(ev.Summary + " " + ev.Detail)
			if ev.StepEntry != nil {
				hay += " " + strings.ToLower(ev.StepEntry.Summary.Action+" "+ev.StepEntry.Summary.Summary+" "+ev.StepEntry.Summary.ToolPath)
			}
			if strings.Contains(hay, q) {
				matches = append(matches, i)
			}
		}
		if len(matches) == 0 {
			m.statusMsg = fmt.Sprintf("No matches for %q", m.search.Query)
			m.statusMsgTTL = statusMsgDefaultTTL
			return m
		}
		m.search.Matches = matches
		m.search.MatchIdx = 0
		m.timeline.StepCursor = matches[0]
		m.ensureStepCursorVisible(max(m.dashboardVisibleLines()-4, 1))
	case "backspace":
		runes := []rune(m.search.Query)
		if len(runes) > 0 {
			m.search.Query = string(runes[:len(runes)-1])
		}
	case " ", "space":
		m.search.Query += " "
	default:
		if len([]rune(key)) == 1 {
			m.search.Query += key
		}
	}
	return m
}

// handleStepFilterKey handles keys in filter editing mode.
func (m dashboardModel) handleStepFilterKey(key string) dashboardModel {
	if m.timeline.StepFilters == nil {
		m.timeline.StepFilters = defaultStepFilters()
	}
	switch key {
	// Row 1: Step action types (lowercase)
	case "t":
		m.timeline.StepFilters["tool_call"] = !m.timeline.StepFilters["tool_call"]
	case "p":
		m.timeline.StepFilters["plan"] = !m.timeline.StepFilters["plan"]
	case "a":
		m.timeline.StepFilters["text"] = !m.timeline.StepFilters["text"]
	case "c":
		m.timeline.StepFilters["complete"] = !m.timeline.StepFilters["complete"]
	case "s":
		m.timeline.StepFilters["spawn"] = !m.timeline.StepFilters["spawn"]
	case "r":
		m.timeline.StepFilters["replan"] = !m.timeline.StepFilters["replan"]
	case "z":
		m.timeline.StepFilters["specialize"] = !m.timeline.StepFilters["specialize"]
	// Row 2: System event types (uppercase or distinct keys)
	// F7: Use 'x' for system spawn and 'X' for exit per spec Task 5.4
	case "C":
		m.timeline.StepFilters[EventCompact] = !m.timeline.StepFilters[EventCompact]
	case "b":
		m.timeline.StepFilters[EventBudget] = !m.timeline.StepFilters[EventBudget]
	case "x":
		m.timeline.StepFilters["sys_spawn"] = !m.timeline.StepFilters["sys_spawn"]
	case "X":
		m.timeline.StepFilters[EventExit] = !m.timeline.StepFilters[EventExit]
	case "T":
		m.timeline.StepFilters[EventStall] = !m.timeline.StepFilters[EventStall]
	case "i":
		m.timeline.StepFilters[EventImmune] = !m.timeline.StepFilters[EventImmune]
	case "*":
		m.timeline.StepFilters = defaultStepFilters()
	case "f", "esc":
		m.timeline.StepFilterMode = false
	default:
		m.statusMsg = "Filter: t/p/a/c/s/r/z (step) | C/b/x/X/T/i (sys) | * all | Esc exit"
		m.statusMsgTTL = statusMsgDefaultTTL
	}
	return m
}

// filteredStepEntries returns step entries matching current filters.
func (m dashboardModel) filteredStepEntries() []int {
	if len(m.timeline.StepFilters) == 0 {
		indices := make([]int, len(m.timeline.StepEntries))
		for i := range m.timeline.StepEntries {
			indices[i] = i
		}
		return indices
	}
	// Check if all filters are on
	allOn := true
	for _, v := range m.timeline.StepFilters {
		if !v {
			allOn = false
			break
		}
	}
	if allOn {
		indices := make([]int, len(m.timeline.StepEntries))
		for i := range m.timeline.StepEntries {
			indices[i] = i
		}
		return indices
	}
	var result []int
	for i, e := range m.timeline.StepEntries {
		if m.timeline.StepFilters[e.Summary.Action] {
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
			return filters[ev.StepEntry.Summary.Action]
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
	if len(m.timeline.StepFilters) == 0 {
		return m.unifiedEvents
	}
	allOn := true
	for _, v := range m.timeline.StepFilters {
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
		if isEventVisible(ev, m.timeline.StepFilters) {
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
	if entry.Level >= levelExpanded {
		detail := m.timeline.StepDetailCache[entry.Summary.Step]
		if detail == nil {
			n++
		} else {
			n += m.estimateExpandedLines(detail, entry.Summary)
		}
	}
	if entry.Level >= levelDebug {
		detail := m.timeline.StepDetailCache[entry.Summary.Step]
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
	if m.timeline.StepCursor < 0 || m.timeline.StepCursor >= len(filtered) {
		return -1
	}
	ev := filtered[m.timeline.StepCursor]
	if ev.StepEntry == nil {
		return -1
	}
	for i := range m.timeline.StepEntries {
		if &m.timeline.StepEntries[i] == ev.StepEntry {
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
	total := len(m.timeline.StepEntries)
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
	if m.timeline.StepFilterMode {
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

	cursor := min(m.timeline.StepCursor, max(len(filtered)-1, 0))

	// F4: Filter bar is 2 lines when active; account for extra line
	headerLines := 1
	if m.timeline.StepFilterMode {
		headerLines = 2
	}
	listLines := max(height-headerLines-1, 1)

	// Variable-height scroll: ensure cursor is visible via stepScrollTop
	startIdx := m.timeline.StepScrollTop
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
				for i := range m.timeline.StepEntries {
					if &m.timeline.StepEntries[i] == ev.StepEntry {
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

	// Build semantic tool aggregation groups for non-bulk mode
	aggGroups := buildToolAggGroups(filtered)
	// Map: filtered index → group pointer (for startIdx of each group)
	aggGroupByStart := make(map[int]*toolAggGroup, len(aggGroups))
	// Set of filtered indices that belong to a collapsed group (non-start)
	aggSkipSet := make(map[int]bool)
	// Set of filtered indices inside expanded groups (for indent detection)
	aggExpandedSet := make(map[int]bool)
	for i := range aggGroups {
		g := &aggGroups[i]
		aggGroupByStart[g.startIdx] = g
		expanded := m.timeline.ExpandedAggGroups[g.stepNums[0]]
		if !expanded {
			for fi := g.startIdx + 1; fi < g.endIdx; fi++ {
				aggSkipSet[fi] = true
			}
		} else {
			for fi := g.startIdx + 1; fi < g.endIdx; fi++ {
				aggExpandedSet[fi] = true
			}
		}
	}

	for fi := startIdx; fi < endIdx && linesUsed < listLines; fi++ {
		ev := filtered[fi]

		// System event: single-line rendering (plus optional detail line when selected)
		if ev.StepEntry == nil {
			cursorMark := "  "
			if fi == m.timeline.StepCursor {
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
			if fi == m.timeline.StepCursor {
				line = lipgloss.NewStyle().
					Background(lipgloss.Color("#2D2D3D")).
					Foreground(lipgloss.Color("#FFFFFF")).
					Render(line)
			}
			b.WriteString(truncateAnsi(line, truncW))
			b.WriteString("\n")
			linesUsed++
			// Show full Detail on a second line when cursor is on a sys event with detail
			if fi == m.timeline.StepCursor && ev.Detail != "" && linesUsed < listLines {
				detailStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
				detail := strings.ReplaceAll(ev.Detail, "\n", " ")
				detailLine := "    ┊ " + detail
				b.WriteString(truncateAnsi(detailStyle.Render(detailLine), truncW))
				b.WriteString("\n")
				linesUsed++
			}
			continue
		}

		// Semantic tool aggregation: skip items inside collapsed groups
		if aggSkipSet[fi] {
			continue
		}

		// Semantic tool aggregation: render group header at startIdx
		if g, ok := aggGroupByStart[fi]; ok {
			expanded := m.timeline.ExpandedAggGroups[g.stepNums[0]]
			cursorInGroup := m.timeline.StepCursor >= g.startIdx && m.timeline.StepCursor < g.endIdx

			if !expanded {
				// Collapsed group header line
				cursorMark := "  "
				if cursorInGroup {
					cursorMark = "▸ "
				}
				dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted))
				marker := "▼"
				if ui.IsASCIIMode() {
					marker = "v"
				}

				// Calculate avg duration
				var totalDur float64
				for idx := g.startIdx; idx < g.endIdx; idx++ {
					if filtered[idx].StepEntry != nil {
						totalDur += filtered[idx].StepEntry.Summary.DurationMs
					}
				}
				avgDur := totalDur / float64(len(g.stepNums))
				avgLabel := formatTimelineDuration(avgDur)

				aggSep := "▸"
				if ui.IsASCIIMode() {
					aggSep = ">"
				}
				headerLine := fmt.Sprintf("%s%s %d–%d %s %s × %d  avg %s",
					cursorMark, marker,
					g.stepNums[0], g.stepNums[len(g.stepNums)-1],
					aggSep, g.toolPath, len(g.stepNums), avgLabel)

				// Add offset range if available
				if showStepOffset {
					var startOff, endOff string
					for _, p := range m.processes {
						if p.PID == m.selectedPID && (m.selectedUUID == "" || p.UUID == m.selectedUUID) {
							if !p.CreatedAt.IsZero() {
								startEv := filtered[g.startIdx]
								endEv := filtered[g.endIdx-1]
								if !startEv.Timestamp.IsZero() {
									startOff = ui.FormatOffsetFromStart(startEv.Timestamp.Sub(p.CreatedAt))
								}
								if !endEv.Timestamp.IsZero() {
									endOff = ui.FormatOffsetFromStart(endEv.Timestamp.Sub(p.CreatedAt))
								}
							}
							break
						}
					}
					if startOff != "" && endOff != "" {
						headerLine += dimStyle.Render(fmt.Sprintf("  %s–%s", startOff, endOff))
					}
				}

				if cursorInGroup {
					headerLine = lipgloss.NewStyle().
						Background(lipgloss.Color("#2D2D3D")).
						Foreground(lipgloss.Color("#FFFFFF")).
						Render(headerLine)
				}

				b.WriteString(truncateAnsi(headerLine, truncW))
				b.WriteString("\n")
				linesUsed++
				// Skip to end of group (startIdx handled, rest in aggSkipSet)
				continue
			}
			// Expanded group: render header then fall through to individual steps
			dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted))
			marker := "▽"
			if ui.IsASCIIMode() {
				marker = "V"
			}
			aggSep := "▸"
			if ui.IsASCIIMode() {
				aggSep = ">"
			}
			expandHeader := fmt.Sprintf("  %s %d–%d %s %s × %d",
				marker, g.stepNums[0], g.stepNums[len(g.stepNums)-1],
				aggSep, g.toolPath, len(g.stepNums))
			b.WriteString(dimStyle.Render(expandHeader))
			b.WriteString("\n")
			linesUsed++
			if linesUsed >= listLines {
				continue
			}
			// Fall through to render individual steps below (they are not in aggSkipSet when expanded)
		}

		// Step event: full Level 1/2/3 rendering
		entry := ev.StepEntry
		s := entry.Summary

		// Check if this step is inside an expanded aggregation group (needs indent)
		inExpandedGroup := aggExpandedSet[fi]

		cursorMark := "  "
		if inExpandedGroup {
			cursorMark = "    " // 4-char indent for sub-steps
		}
		if fi == m.timeline.StepCursor {
			if inExpandedGroup {
				cursorMark = "  ▸ "
			} else {
				cursorMark = "▸ "
			}
		}

		levelMark := " "
		if entry.Level >= levelExpanded {
			detail := m.timeline.StepDetailCache[s.Step]
			if detail == nil || hasExpandableContent(detail, s) {
				levelMark = "▾"
			}
		} else {
			if detail, ok := m.timeline.StepDetailCache[s.Step]; ok && hasExpandableContent(detail, s) {
				levelMark = "▸"
			}
		}

		dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted))

		// New layout: step# ▸ action · summary  duration  +relTime
		detail := m.timeline.StepDetailCache[s.Step]
		actionText, summaryText := formatDefaultLine(s, detail)
		stepNumStr := fmt.Sprintf("%d", s.Step)

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

		// Calculate available width for summary
		// Fixed parts: cursor(cursorW) + levelMark(1) + space(1) + step#(len) + " ▸ "(3) + action(len)
		actionW := runewidth.StringWidth(actionText)
		stepNumW := utf8.RuneCountInString(stepNumStr)
		cursorW := 2
		if inExpandedGroup {
			cursorW = 4
		}
		fixedWidth := cursorW + 1 + 1 + stepNumW + 3 + actionW
		if summaryText != "" {
			fixedWidth += 3 // " · " separator
		}
		if showDuration {
			fixedWidth += 7 // space + 6-char duration
		}
		if offsetLabel != "" {
			fixedWidth += 9 // space + 8-char offset
		}
		if hasError {
			fixedWidth += 2
		}

		summaryW := max(truncW-fixedWidth, 0)
		var summaryPart string
		if summaryW >= 10 && summaryText != "" {
			sep := " · "
			if ui.IsASCIIMode() {
				sep = " - "
			}
			summaryPart = sep + truncateRuneWidth(summaryText, summaryW)
		}

		if hasError && width >= 80 {
			if cached := m.timeline.StepDetailCache[s.Step]; cached != nil && cached.ToolError != "" {
				errLine := strings.SplitN(cached.ToolError, "\n", 2)[0]
				// errPreviewW: remaining width after summary (avoid double-counting)
				errPreviewW := max(summaryW-2, 10)
				if summaryPart != "" {
					errPreviewW = max(summaryW-runewidth.StringWidth(summaryText)-2, 10)
				}
				if runewidth.StringWidth(errLine) > errPreviewW {
					errLine = runewidth.Truncate(errLine, errPreviewW-1, "…")
				}
				errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorError))
				errMark = errStyle.Render(" ✗ " + errLine)
			}
		}

		// Build line: cursorMark levelMark step# ▸ action[· summary] [duration] [+offset] [errMark]
		actionStyle := lipgloss.NewStyle().Foreground(actionColor(s.Action))
		stepSep := "▸"
		if ui.IsASCIIMode() {
			stepSep = ">"
		}
		var line string
		lineCore := fmt.Sprintf("%s%s %s %s %s%s",
			cursorMark, levelMark, stepNumStr, stepSep,
			actionStyle.Render(actionText), summaryPart)
		parts := []string{lineCore}
		if showDuration {
			parts = append(parts, durLabel)
		}
		if offsetLabel != "" {
			parts = append(parts, offsetLabel)
		}
		parts = append(parts, errMark)
		line = strings.Join(parts, " ")

		if hasError {
			line = lipgloss.NewStyle().Background(lipgloss.Color("#3D1F1F")).Render(line)
		} else if fi == m.timeline.StepCursor {
			line = lipgloss.NewStyle().
				Background(lipgloss.Color("#2D2D3D")).
				Foreground(lipgloss.Color("#FFFFFF")).
				Render(line)
		}

		b.WriteString(truncateAnsi(line, truncW))
		b.WriteString("\n")
		linesUsed++

		// Level 2: Expanded detail
		if entry.Level >= levelExpanded && linesUsed < listLines {
			expandDetail := m.timeline.StepDetailCache[s.Step]
			if expandDetail == nil {
				b.WriteString(dimStyle.Render("   ┊ Loading…") + "\n")
				linesUsed++
			} else {
				linesUsed += m.renderExpandedDetail(&b, expandDetail, s, truncW, listLines-linesUsed)
			}
		}

		// Level 3: Debug detail
		if entry.Level >= levelDebug && linesUsed < listLines {
			detail := m.timeline.StepDetailCache[s.Step]
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

	// Story 36-4: 排序方向 & expandMode 指示（dim 颜色，放在 steps 数量之后）
	{
		dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted))
		ascii := ui.IsASCIIMode()
		var dirText string
		if m.timeline.SortAsc {
			if ascii {
				dirText = "^ old->new"
			} else {
				dirText = "↑ 旧→新"
			}
		} else {
			if ascii {
				dirText = "v new->old"
			} else {
				dirText = "↓ 新→旧"
			}
		}
		fmt.Fprintf(&b, " %s", dimStyle.Render("│ "+dirText))
		switch m.timeline.ExpandMode {
		case expandModeExpanded:
			sep := "·"
			if ascii {
				sep = "-"
			}
			fmt.Fprintf(&b, " %s", dimStyle.Render(sep+" all"))
		case expandModeErrorsOnly:
			sep := "·"
			if ascii {
				sep = "-"
			}
			fmt.Fprintf(&b, " %s", dimStyle.Render(sep+" errors"))
		}
	}

	// Total tokens from step summaries
	totalTok := 0
	for _, e := range m.timeline.StepEntries {
		totalTok += e.Summary.TokenCount
	}
	if totalTok > 0 {
		fmt.Fprintf(&b, " │ %s tok", formatTokenCount(totalTok))
	}

	// Stage statistics (wide screens only)
	if maxW >= 100 && totalSteps > 0 {
		counts := make(map[string]int)
		errCount := 0
		for _, e := range m.timeline.StepEntries {
			counts[e.Summary.Action]++
			if e.Summary.HasError {
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
		pos := min(m.timeline.StepCursor+1, filteredCount)
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
			if !m.timeline.StepFilters[t.key] {
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
		on := m.timeline.StepFilters == nil || m.timeline.StepFilters[t.action]
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
		on := m.timeline.StepFilters == nil || m.timeline.StepFilters[t.eventType]
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
			entry := m.timeline.StepEntries[filtered[fi]]
			s := entry.Summary
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
	cursorFilterIdx := min(m.timeline.StepCursor, len(filtered)-1)

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
		if m.timeline.ExpandedAggGroups[gi] {
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

		isExpanded := m.timeline.ExpandedAggGroups[gi]
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
				entry := m.timeline.StepEntries[idx]
				s := entry.Summary

				cursorMark := "  "
				if fi == m.timeline.StepCursor {
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
				} else if fi == m.timeline.StepCursor {
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
		m.timeline.StepScrollTop = 0
		return
	}
	cursor := min(m.timeline.StepCursor, len(filtered)-1)

	// If cursor is above scroll top, snap to cursor
	if cursor < m.timeline.StepScrollTop {
		m.timeline.StepScrollTop = cursor
		return
	}

	// Walk from scrollTop counting lines; if cursor fits, done
	linesUsed := 0
	for fi := m.timeline.StepScrollTop; fi <= cursor && fi < len(filtered); fi++ {
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
	m.timeline.StepScrollTop = newTop
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
	known := make(map[int]struct{}, len(m.timeline.StepEntries))
	for _, e := range m.timeline.StepEntries {
		known[e.Summary.Step] = struct{}{}
	}

	for _, s := range steps {
		if _, dup := known[s.Step]; dup {
			continue
		}
		known[s.Step] = struct{}{}
		// Story 36-4: 按 expandMode 决定新 step 的初始 level。
		level := levelSummary
		autoExpand := false
		switch m.timeline.ExpandMode {
		case expandModeExpanded:
			level = levelExpanded
			autoExpand = true
		case expandModeErrorsOnly:
			if s.HasError {
				level = levelExpanded
				autoExpand = true
			}
		default: // expandModeCollapsed — safety net：错误始终展开
			if s.HasError {
				level = levelExpanded
				autoExpand = true
			}
		}
		m.timeline.StepEntries = append(m.timeline.StepEntries, stepEntry{
			Summary:    s,
			Level:      level,
			AutoExpand: autoExpand,
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
	// Process not in the live list — if debug mode has loaded events for this PID,
	// it was a dead process whose events were fetched from disk. Treat as dead to
	// prevent the periodic running-process refresh from triggering unnecessary reloads.
	if m.debugMode && m.debugAttachedPID == m.selectedPID && len(m.debugEvents) > 0 {
		return true
	}
	return false
}

// --- Auto-fetch for expanded steps ---

// fetchNextExpandedDetail returns a Cmd to fetch the next expanded step that has no cached detail.
// Returns nil if nothing to fetch or already fetching.
func (m dashboardModel) fetchNextExpandedDetail() tea.Cmd {
	if m.timeline.FetchingDetail || m.selectedPID == 0 {
		return nil
	}
	// Priority 1: expanded steps without cached detail
	for _, entry := range m.timeline.StepEntries {
		if entry.Level >= levelExpanded && m.timeline.StepDetailCache[entry.Summary.Step] == nil {
			return fetchStepDetailCmd(m.selectedPID, entry.Summary.Step)
		}
	}
	// Priority 2: visible collapsed steps without cached detail (for ▸ indicator)
	filteredEvs := m.filteredUnifiedEvents()
	pageSize := max(m.dashboardVisibleLines()-4, 1)
	visStart := m.timeline.StepScrollTop
	visEnd := min(visStart+pageSize, len(filteredEvs))
	for i := visStart; i < visEnd; i++ {
		ev := filteredEvs[i]
		if ev.StepEntry == nil {
			continue
		}
		entry := ev.StepEntry
		if entry.Level < levelExpanded && m.timeline.StepDetailCache[entry.Summary.Step] == nil {
			return fetchStepDetailCmd(m.selectedPID, entry.Summary.Step)
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

// --- Helper functions (retained from Story 27-4, used by Step Inspector) ---

// formatRoleTag renders the bracketed role label for a conversation message,
// with role-specific colors and Bold. Story 38-3 AC#1 splits the legacy "tool"
// branch into two visually distinct tags:
//   - tool_use   (msg.Role=="tool" && msg.ToolCallID=="")  → ColorSuccess green + Bold
//     (rare boundary case: tool invocation recorded as a standalone message
//     without a ToolCallID; shown plain `[tool_use]`)
//   - tool_result (msg.Role=="tool" && msg.ToolCallID!="") → ColorReplay orange + Bold
//     (the common case; suffixed with the tool name when a name is mapped via
//     toolCallNames, falling back to the raw ToolCallID)
//
// The existing user/assistant/system branches preserve their pre-Story 38-3
// styles (greens/blues/grey) — see Dev Notes 1 of Story 38-3 for the decision
// to keep the existing color language and only enrich the tool branch.
//
// ASCII fallback: lipgloss auto-degrades colors when the terminal profile
// lacks color support; the bracketed text always remains readable.
func formatRoleTag(msg ipc.MessageWire, toolCallNames map[string]string) string {
	switch msg.Role {
	case "system":
		return promptRoleSystem.Render("[system]")
	case "user":
		return promptRoleUser.Bold(true).Render("[user]")
	case "assistant":
		return promptRoleAssistant.Bold(true).Render("[assistant]")
	case "tool":
		// Story 38-3 AC#1: tool_use vs tool_result distinction by ToolCallID.
		if msg.ToolCallID == "" {
			toolUseStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorSuccess)).Bold(true)
			return toolUseStyle.Render("[tool_use]")
		}
		label := ""
		if name, ok := toolCallNames[msg.ToolCallID]; ok && name != "" {
			label = ":" + name
		} else {
			label = ":" + msg.ToolCallID
		}
		toolResultStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorReplay)).Bold(true)
		return toolResultStyle.Render("[tool_result" + label + "]")
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
