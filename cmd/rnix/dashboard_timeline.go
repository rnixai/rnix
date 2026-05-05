package main

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	dashboardevent "github.com/rnixai/rnix/internal/dashboard/event"
	"github.com/rnixai/rnix/internal/dashboard/timeline"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"

	tea "charm.land/bubbletea/v2"
)

// --- Step-unified Timeline (UX Timeline Unification) ---

// actionColor — thin wrapper · 见 internal/dashboard/timeline.ActionColor
//
// Story 38-5 PR11 Step 4(a-2)：迁出至 internal/dashboard/timeline/helpers.go.
// 保留函数名让 ATDD 29-1 文件拆分契约 + dashboard_test.go grep 字符串通过.
func actionColor(action string) lipgloss.Color {
	return timeline.ActionColor(action)
}

// actionAbbrev — thin wrapper · 见 internal/dashboard/timeline.ActionAbbrev
//
// Story 38-5 PR11 Step 4(c) renderAggregatedTimeline 迁出后 cmd/rnix 端无 caller ·
// 保留 wrapper 满足 ATDD 27-3 line 817 注释引用 + 潜在 grep 契约（与 dashboard_tree.go
// stateRank / longestCommonPrefix unused wrapper pattern 同模式）。
//
//nolint:unused // ATDD 27-3 grep 契约保留（renderAggregatedTimeline 迁出后无 caller）
func actionAbbrev(action string) string {
	return timeline.ActionAbbrev(action)
}

// timelineAggThreshold — alias of dashboardevent.AggThreshold (thin wrapper · Story 38-5 PR11 Step 4(a-2))
//
//nolint:unused // ATDD 36-3 grep 契约保留 (timeline IA refactor 测试 + 历史调用)
const timelineAggThreshold = dashboardevent.AggThreshold

// toolAggGroup — alias of dashboardevent.ToolAggGroup (thin wrapper)
type toolAggGroup = dashboardevent.ToolAggGroup

// buildToolAggGroups — thin wrapper · 见 internal/dashboard/event.BuildToolAggGroups
func buildToolAggGroups(events []UnifiedEvent) []toolAggGroup {
	return dashboardevent.BuildToolAggGroups(events)
}

// shortenArgs — thin wrapper · 见 internal/dashboard/timeline.ShortenArgs
func shortenArgs(input string, maxLen int) string {
	return timeline.ShortenArgs(input, maxLen)
}

// formatDefaultLine — thin wrapper · 见 internal/dashboard/timeline.FormatDefaultLine
func formatDefaultLine(s ipc.StepSummaryWire, detail *ipc.GetStepDetailResponse) (action, summary string) {
	return timeline.FormatDefaultLine(s, detail)
}

// sysEventStyle — thin wrapper · 见 internal/dashboard/event.SysEventStyle
func sysEventStyle(ev UnifiedEvent) lipgloss.Style {
	return dashboardevent.SysEventStyle(ev)
}

// defaultStepFilters — thin wrapper · 见 internal/dashboard/event.DefaultStepFilters
func defaultStepFilters() map[string]bool {
	return dashboardevent.DefaultStepFilters()
}

// maybeShowTimelineMigrationNotice shows a one-time status bar notice when the
// user enters升序 Timeline for the first time. Persists the shown flag to
// ~/.config/rnix/ui-state.json so the notice never repeats across sessions.
// Story 36-4 AC-3.
//
// Story 38-5 PR11 Step 4(c)：纯逻辑迁出至 internal/dashboard/timeline.MigrationCheck
// （懒分配 UIState + saver 调用 + 不阻塞写入失败 · 与历史行为等价）。cmd/rnix
// wrapper 仅保留 m.statusMsg / m.statusMsgTTL 副作用（dashboardModel-private）。
func (m dashboardModel) maybeShowTimelineMigrationNotice() dashboardModel {
	var show bool
	m.timeline, show = timeline.MigrationCheck(m.timeline, nil)
	if show {
		m.statusMsg = "Timeline 已改为升序（最新在底）。按 o 切换。"
		m.statusMsgTTL = 5
	}
	return m
}

// handleTimelinePIDChange resets timeline state when selected process changes.
// Uses UUID for reliable identification (PIDs can be reused).
//
// Story 38-5 PR11 Step 4(c)：timeline state 重置主体迁出至
// internal/dashboard/timeline.HandlePIDUUIDChange（含 9 字段重置 +
// ExpandMode=collapsed · 与历史行为 byte-for-byte 等价）。cmd/rnix wrapper 仅
// 保留 m.search 跨 plugin 重置（Story 36-5 P-1 · search state per-process）。
func (m dashboardModel) handleTimelinePIDChange() dashboardModel {
	prevUUID := m.timeline.AttachedUUID
	m.timeline = timeline.HandlePIDUUIDChange(m.timeline, m.selectedPID, m.selectedUUID)
	if m.selectedUUID == prevUUID {
		return m
	}
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
		//
		// Story 38-5 PR11 Step 4(c)：expand mode mutation 主体迁出至
		// internal/dashboard/timeline.ApplyExpandMode（含 detail==nil 短路径 +
		// hasExpandable 双判断 + empty list NoSteps 提示 · 与历史等价）。
		var msg string
		m.timeline, msg = timeline.ApplyExpandMode(m.timeline, expandModeExpanded, hasExpandableContent)
		m.statusMsg = msg
		m.statusMsgTTL = statusMsgDefaultTTL
	case "E":
		// Story 36-4: ErrorsOnly mode — 仅展开 HasError=true 的 step。
		var msg string
		m.timeline, msg = timeline.ApplyExpandMode(m.timeline, expandModeErrorsOnly, hasExpandableContent)
		m.statusMsg = msg
		m.statusMsgTTL = statusMsgDefaultTTL
	case "C":
		// Story 36-4: Collapsed mode — 全部折叠到 summary。
		// 仅非 filter 模式生效（filter 模式下的 C 由 handleStepFilterKey 处理）。
		var msg string
		m.timeline, msg = timeline.ApplyExpandMode(m.timeline, expandModeCollapsed, hasExpandableContent)
		m.statusMsg = msg
		m.statusMsgTTL = statusMsgDefaultTTL
	case "o":
		// Story 36-4: 切换 Timeline 排序方向（升 ↔ 降）
		//
		// Story 38-5 PR11 Step 4(c)：sort direction toggle 主体迁出至
		// internal/dashboard/timeline.ToggleSortDirection。
		var msg string
		m.timeline, msg = timeline.ToggleSortDirection(m.timeline)
		m.statusMsg = msg
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
		// Story 38-5 PR11 Step 4(c)：error 跳转 lookup 主体迁出至
		// internal/dashboard/event.FindNextErrorIdx（HasError + Severity≥SevError
		// 双判定 · 与历史等价）。
		if idx, ok := dashboardevent.FindNextErrorIdx(filtered, m.timeline.StepCursor); ok {
			m.timeline.StepCursor = idx
		} else {
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
		// Story 38-5 PR11 Step 4(c)：见 FindNextErrorIdx 同模式。
		if idx, ok := dashboardevent.FindPrevErrorIdx(filtered, m.timeline.StepCursor); ok {
			m.timeline.StepCursor = idx
		} else {
			m.statusMsg = "No more errors"
			m.statusMsgTTL = statusMsgDefaultTTL
		}
	}
	m.ensureStepCursorVisible(max(m.dashboardVisibleLines()-4, 1))
	return m
}

// handleTimelineSearchKey handles search input in Timeline. Story 36-5 AC-12.
//
// Story 38-5 PR11 Step 4(c)：esc/backspace/space/单字符 输入累积主体迁出至
// internal/dashboard/plugin.SearchPlugin.HandleInputKey（含 nil safety + Unicode
// rune 边界 + multi-char 键 fallback · 与 cmd/rnix 历史 byte-for-byte 等价）。
// cmd/rnix wrapper 仅保留 enter 分支（FindMatches 已迁 · 但触发 + statusMsg +
// StepCursor 跳转 + ensureStepCursorVisible 副作用必须留 cmd/rnix）。
func (m dashboardModel) handleTimelineSearchKey(key string) dashboardModel {
	if key == "enter" {
		m.search.Mode = false
		if m.search.Query == "" {
			return m
		}
		// Story 36-5 P-7: collect ALL match indices (not just the first), so n/N
		// can cycle through them per the modal n/N semantics.
		matches := dashboardevent.FindMatches(m.filteredUnifiedEvents(), m.search.Query)
		if len(matches) == 0 {
			m.statusMsg = fmt.Sprintf("No matches for %q", m.search.Query)
			m.statusMsgTTL = statusMsgDefaultTTL
			return m
		}
		m.search.Matches = matches
		m.search.MatchIdx = 0
		m.timeline.StepCursor = matches[0]
		m.ensureStepCursorVisible(max(m.dashboardVisibleLines()-4, 1))
		return m
	}
	// 非 enter 按键由 SearchPlugin.HandleInputKey 处理（esc/backspace/space/单字符）
	m.search.HandleInputKey(key)
	return m
}

// handleStepFilterKey handles keys in filter editing mode.
//
// Story 38-5 PR11 Step 4(c)：纯 filter map 切换主体迁出至
// internal/dashboard/event.ApplyStepFilterKey（含 13 个 filter key + 重置
// + 退出 filter 模式 + showHelp 信号 · 与 cmd/rnix 历史 byte-for-byte 等价）。
// cmd/rnix wrapper 仅保留 m.statusMsg / m.statusMsgTTL 副作用。
func (m dashboardModel) handleStepFilterKey(key string) dashboardModel {
	var help bool
	m.timeline.StepFilters, m.timeline.StepFilterMode, help = dashboardevent.ApplyStepFilterKey(
		m.timeline.StepFilters, m.timeline.StepFilterMode, key)
	if help {
		m.statusMsg = dashboardevent.FilterHelpMsg
		m.statusMsgTTL = statusMsgDefaultTTL
	}
	return m
}

// filteredStepEntries — thin wrapper 委托 timeline.FilteredStepEntries
// (Story 38-5 PR11 Step 4(a-2) timeline pure helpers 迁出)
//
// 保留 (m dashboardModel) receiver 让 dashboard_pane_dispatcher.go::F3 计算
// 「step-only count from filtered unified events, not filteredStepEntries」（行 39
// 注释）的 callsite `m.filteredStepEntries()` 写法零修改 · 函数体不读 m 字段，
// 仅传 m.timeline 给 timeline.FilteredStepEntries · 行为完全等价。
func (m dashboardModel) filteredStepEntries() []int {
	return timeline.FilteredStepEntries(m.timeline)
}

// isEventVisible — thin wrapper · 见 internal/dashboard/event.IsEventVisible.
//
// Story 38-5 PR11 Step 4(c) FilterDebugEvents 迁出后 cmd/rnix 端 caller 减少（仅
// 保留 ATDD 契约与潜在外部 caller）· nolint:unused 标注（spec § Risk 4 与
// dashboard_tree.go::stateRank / dashboard_eval.go::renderEvalTopologyView 同
// 模式 · 不删除 wrapper 避免破坏潜在 grep 契约）。
//
//nolint:unused // 保留供 ATDD 契约与潜在外部 caller。
func isEventVisible(ev UnifiedEvent, filters map[string]bool) bool {
	return dashboardevent.IsEventVisible(ev, filters)
}

// filteredUnifiedEvents — thin wrapper 委托 dashboardevent.FilterUnifiedEvents
// (Story 38-5 PR11 Step 4(a-2) timeline event helpers 迁出)
//
// 保留 (m dashboardModel) receiver 让 4 个 callsite 零修改通过：
//   - dashboard_timeline.go × 4（line 127/289 + 1607/1741 等）
//   - dashboard_events.go × 1（line 581）
//   - dashboard_keylayers.go × 1（line 254）
//
// 函数体仅传 m.unifiedEvents（共享 EventStream · spec § Dev Notes 关键架构决策 6
// 保留在 App Model）+ m.timeline.StepFilters · 行为完全等价。
//
// **包归属**：放在 event 包而非 timeline 包 · 避免 timeline → event 反向 import 循环。
func (m dashboardModel) filteredUnifiedEvents() []UnifiedEvent {
	return dashboardevent.FilterUnifiedEvents(m.unifiedEvents, m.timeline.StepFilters)
}

// unifiedItemHeight — thin wrapper 委托 timeline.UnifiedItemHeight
// (Story 38-5 PR11 Step 4(a-2) timeline pure helpers 迁出)
//
// 解开 ev.StepEntry 避免 timeline 包反向 import event 包（event 包已 import
// timeline.StepEntry · 循环依赖防御 · 与 commit 7d1964c event helpers 同模式但
// 反向解耦）· 保留 (m dashboardModel) receiver 让 6 个 callsite 零修改通过。
func (m dashboardModel) unifiedItemHeight(ev UnifiedEvent) int {
	return timeline.UnifiedItemHeight(ev.StepEntry, m.timeline.StepDetailCache)
}

// resolveStepIndex converts cursor position in filtered unified view to actual stepEntries index.
// Returns -1 if cursor points to a system event or is out of range.
//
// Story 38-5 PR11 Step 4(c)：内部指针查找逻辑迁至
// internal/dashboard/timeline.FindStepEntryIndex（按地址比较 · 0 cmd/rnix 反向依赖）·
// cmd/rnix wrapper 仅保留 cursor 范围检查 + filtered 提取（依赖 UnifiedEvent · 无法迁出）.
// 保留 (m dashboardModel) receiver 让 dashboard_pane_dispatcher.go::78 callsite 零修改.
func (m dashboardModel) resolveStepIndex() int {
	filtered := m.filteredUnifiedEvents()
	if m.timeline.StepCursor < 0 || m.timeline.StepCursor >= len(filtered) {
		return -1
	}
	return timeline.FindStepEntryIndex(m.timeline, filtered[m.timeline.StepCursor].StepEntry)
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
		aggGroupByStart[g.StartIdx] = g
		expanded := m.timeline.ExpandedAggGroups[g.StepNums[0]]
		if !expanded {
			for fi := g.StartIdx + 1; fi < g.EndIdx; fi++ {
				aggSkipSet[fi] = true
			}
		} else {
			for fi := g.StartIdx + 1; fi < g.EndIdx; fi++ {
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
			expanded := m.timeline.ExpandedAggGroups[g.StepNums[0]]
			cursorInGroup := m.timeline.StepCursor >= g.StartIdx && m.timeline.StepCursor < g.EndIdx

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
				for idx := g.StartIdx; idx < g.EndIdx; idx++ {
					if filtered[idx].StepEntry != nil {
						totalDur += filtered[idx].StepEntry.Summary.DurationMs
					}
				}
				avgDur := totalDur / float64(len(g.StepNums))
				avgLabel := formatTimelineDuration(avgDur)

				aggSep := "▸"
				if ui.IsASCIIMode() {
					aggSep = ">"
				}
				headerLine := fmt.Sprintf("%s%s %d–%d %s %s × %d  avg %s",
					cursorMark, marker,
					g.StepNums[0], g.StepNums[len(g.StepNums)-1],
					aggSep, g.ToolPath, len(g.StepNums), avgLabel)

				// Add offset range if available
				if showStepOffset {
					var startOff, endOff string
					for _, p := range m.processes {
						if p.PID == m.selectedPID && (m.selectedUUID == "" || p.UUID == m.selectedUUID) {
							if !p.CreatedAt.IsZero() {
								startEv := filtered[g.StartIdx]
								endEv := filtered[g.EndIdx-1]
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
				marker, g.StepNums[0], g.StepNums[len(g.StepNums)-1],
				aggSep, g.ToolPath, len(g.StepNums))
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
// renderExpandedDetail — thin wrapper · 见 internal/dashboard/timeline.RenderExpandedDetail
//
// Story 38-5 PR11 Step 4(c)：迁出至 internal/dashboard/timeline/render.go.
// 保留 (m dashboardModel) receiver 让 ATDD 27-3 / 36-3 grep 字符串通过.
// 函数体本身不依赖 dashboardModel 状态（receiver 是死的 · 完全 pure helper）.
func (m dashboardModel) renderExpandedDetail(b *strings.Builder, detail *ipc.GetStepDetailResponse, s ipc.StepSummaryWire, maxW, maxLines int) int {
	return timeline.RenderExpandedDetail(b, detail, s, maxW, maxLines)
}

// renderDebugDetail renders Level 3 debug lines (messages preview).
// renderDebugDetail — thin wrapper · 见 internal/dashboard/timeline.RenderDebugDetail
//
// Story 38-5 PR11 Step 4(c)：迁出至 internal/dashboard/timeline/render.go.
// 保留 (m dashboardModel) receiver 让 ATDD 27-3 / 27-4 grep 字符串通过.
// promptRoleForRole 通过依赖注入传入 · 避免 timeline 包持有 promptRole* style 拷贝.
func (m dashboardModel) renderDebugDetail(b *strings.Builder, detail *ipc.GetStepDetailResponse, maxW, maxLines int) int {
	return timeline.RenderDebugDetail(b, detail, maxW, maxLines, promptRoleForRole)
}

// promptRoleForRole — thin wrapper · 见 internal/dashboard/timeline.PromptRoleForRole
//
// Story 38-5 PR11 Step 4(c)：迁出至 internal/dashboard/timeline/role.go.
// 保留函数名让 RenderDebugDetail roleStyle 依赖注入闭包通过.
func promptRoleForRole(role string) string {
	return timeline.PromptRoleForRole(role)
}

// renderUnifiedStepHeader renders the timeline header with unified event counts.
// renderUnifiedStepHeader — thin wrapper · 见 internal/dashboard/timeline.RenderUnifiedStepHeader
//
// Story 38-5 PR11 Step 4(c)：迁出至 internal/dashboard/timeline/render.go.
// 保留 (m dashboardModel) receiver 让 ATDD 27-3 / 36-3 / 36-4 grep 字符串通过.
// HeaderContext 注入运行时数据（State + Processes + SelectedPID/UUID + TotalEvents）·
// 与 RenderDebugDetail roleStyle / RenderTraceTreeView 共享数据注入同模式.
func (m dashboardModel) renderUnifiedStepHeader(maxW, totalSteps, filteredCount, sysCount int) string {
	return timeline.RenderUnifiedStepHeader(timeline.HeaderContext{
		State:        m.timeline,
		Processes:    m.processes,
		SelectedPID:  m.selectedPID,
		SelectedUUID: m.selectedUUID,
		TotalEvents:  len(m.unifiedEvents),
	}, maxW, totalSteps, filteredCount, sysCount)
}

// renderStepFilterBar — thin wrapper · 见 internal/dashboard/timeline.RenderStepFilterBar
//
// Story 38-5 PR11 Step 4(c)：迁出至 internal/dashboard/timeline/render.go.
// 保留函数名让 ATDD 27-3 / 36-3 / 38-3 grep 字符串通过 · 行为完全等价.
func (m dashboardModel) renderStepFilterBar(maxW int) string {
	return timeline.RenderStepFilterBar(m.timeline.StepFilters, maxW)
}

// renderAggregatedTimeline — thin wrapper 委托 timeline.RenderAggregatedTimeline
// (Story 38-5 PR11 Step 4(c) timeline render body 第 5 个迁出 commit · 同
// renderStepFilterBar / renderDebugDetail / renderExpandedDetail /
// renderUnifiedStepHeader 同模式)
//
// 保留 (m dashboardModel) receiver 让 dashboard_timeline.go::renderStepTimeline
// line ~470 的 callsite 零修改通过 ATDD 27-3 + 36-3 + 36-4 契约。函数体仅依赖
// m.timeline (TimelineState) · 直接委托至 internal/dashboard/timeline 包。
func (m dashboardModel) renderAggregatedTimeline(b *strings.Builder, filtered []int, truncW, listLines int, showToken, showDuration bool) int {
	return timeline.RenderAggregatedTimeline(b, m.timeline, filtered, truncW, listLines, showToken, showDuration)
}

// hasExpandableContent — thin wrapper 委托 timeline.HasExpandableContent
// (Story 38-5 PR11 Step 4(a-2) timeline pure helpers 迁出 · 同 hasExpandableContent
// 函数体完全等价 · 仅依赖 detail/s 参数 · 无 dashboardModel 状态依赖)
//
// 保留全局函数旧名让 cmd/rnix 端 callsites（dashboard_timeline.go × 3 +
// dashboard_pane_dispatcher.go × 1 = 4 处）零修改通过 ATDD 契约。
func hasExpandableContent(detail *ipc.GetStepDetailResponse, s ipc.StepSummaryWire) bool {
	return timeline.HasExpandableContent(detail, s)
}

// --- Step item height estimation (for scroll) ---
//
// estimateExpandedLines / estimateDebugLines wrapper 已删除（Story 38-5 PR11 Step
// 4(a-2) timeline.UnifiedItemHeight 迁出后 · 这两个 wrapper 失去唯一 caller 变 unused
// · 直接调 timeline.EstimateExpandedLines / timeline.EstimateDebugLines 即可 · 这是
// 重构推进中 wrapper 自然消解 · 与 PR2/PR3 「保留 wrapper 直到无 caller」模式一致）。

// ensureStepCursorVisible — thin wrapper 委托 timeline.EnsureStepCursorVisible
// (Story 38-5 PR11 Step 4(c) 第 6 个 timeline render block 迁出 · 续 commit
// fb28dfa renderAggregatedTimeline 节奏 · 仍严格遵循 spec § 04 风险 5 中断保险原则)
//
// 保留 (m *dashboardModel) receiver 让 cmd/rnix 端 ~10 处 callsite（dashboard_timeline.go
// 6 处 + dashboard_pane_dispatcher.go 4 处）零修改通过。callback closure 捕获
// filtered + m.unifiedItemHeight · 让 timeline 包零 UnifiedEvent 依赖（避免循环）。
func (m *dashboardModel) ensureStepCursorVisible(viewportLines int) {
	filtered := m.filteredUnifiedEvents()
	m.timeline = timeline.EnsureStepCursorVisible(m.timeline, len(filtered), viewportLines, func(idx int) int {
		return m.unifiedItemHeight(filtered[idx])
	})
}

// --- Step timeline helpers ---

// formatTokenCount — thin wrapper · 见 internal/dashboard/timeline.FormatTokenCount
func formatTokenCount(tokens int) string {
	return timeline.FormatTokenCount(tokens)
}

// formatTimelineDuration — thin wrapper · 见 internal/dashboard/timeline.FormatDurationMs
func formatTimelineDuration(ms float64) string {
	return timeline.FormatDurationMs(ms)
}

// truncateRuneWidth — thin wrapper · 见 internal/dashboard/timeline.TruncateRuneWidth
func truncateRuneWidth(s string, maxWidth int) string {
	return timeline.TruncateRuneWidth(s, maxWidth)
}

// truncateAnsi — thin wrapper · 见 internal/dashboard/timeline.TruncateAnsi
func truncateAnsi(s string, maxWidth int) string {
	return timeline.TruncateAnsi(s, maxWidth)
}

// --- Step data flow ---

// applyNewSteps — thin wrapper · 见 internal/dashboard/timeline.ApplyNewSteps
//
// Story 38-5 PR11 Step 4(c)：迁出至 internal/dashboard/timeline/apply.go.
// 保留 (m dashboardModel) receiver 让 cmd/rnix 端调用方零修改通过.
// Story 36-4 expandMode + safety net 行为契约保留（迁出包内已显式测试 7 项）.
func (m dashboardModel) applyNewSteps(steps []ipc.StepSummaryWire) dashboardModel {
	m.timeline = timeline.ApplyNewSteps(m.timeline, steps)
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
	if m.debugState.Mode && m.debugState.AttachedPID == m.selectedPID && len(m.debugState.Events) > 0 {
		return true
	}
	return false
}

// --- Auto-fetch for expanded steps ---

// fetchNextExpandedDetail returns a Cmd to fetch the next expanded step that has no cached detail.
// Returns nil if nothing to fetch or already fetching.
//
// Story 38-5 PR11 Step 4(c)：纯 lookup 算法主体迁出至
// internal/dashboard/timeline.FindNextDetailToFetch（Priority 1 expanded
// uncached + Priority 2 visible collapsed uncached + nil entry skip · 与历史
// byte-for-byte 等价）。cmd/rnix wrapper 仅保留 IPC fetch 副作用 +
// fetching/PID guards + filtered StepEntry 解包.
func (m dashboardModel) fetchNextExpandedDetail() tea.Cmd {
	if m.timeline.FetchingDetail || m.selectedPID == 0 {
		return nil
	}
	filteredEvs := m.filteredUnifiedEvents()
	pageSize := max(m.dashboardVisibleLines()-4, 1)
	visStart := m.timeline.StepScrollTop
	visEnd := min(visStart+pageSize, len(filteredEvs))

	// 解开 ev.StepEntry → []*timeline.StepEntry（避免 timeline 包反向 import event）
	filteredEntries := make([]*timeline.StepEntry, len(filteredEvs))
	for i, ev := range filteredEvs {
		filteredEntries[i] = ev.StepEntry
	}

	step := timeline.FindNextDetailToFetch(m.timeline, filteredEntries, visStart, visEnd)
	if step < 0 {
		return nil
	}
	return fetchStepDetailCmd(m.selectedPID, step)
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

// formatRoleTag — thin wrapper · 见 internal/dashboard/timeline.FormatRoleTag
//
// Story 38-5 PR11 Step 4(c)：迁出至 internal/dashboard/timeline/role.go（含
// promptRoleSystem/User/Assistant/Tool 4 个 lipgloss.Style · 包内私有 ·
// dashboard_types.go 端旧定义随之删除）.
//
// **Story 38-3 AC#1 行为契约保留**：tool_use / tool_result 拆分 + ColorSuccess /
// ColorReplay + Bold + tool name fallback 逻辑全部迁出但等价 · ATDD 27-4 / 36.1 /
// inspector visual TestFormatRoleTag_ToolUseVsToolResult 测试通过 thin wrapper
// 间接覆盖（cmd/rnix 端 caller 行为零变化）.
func formatRoleTag(msg ipc.MessageWire, toolCallNames map[string]string) string {
	return timeline.FormatRoleTag(msg, toolCallNames)
}

// formatCharCount — thin wrapper · 见 internal/dashboard/timeline.FormatCharCount
func formatCharCount(n int) string {
	return timeline.FormatCharCount(n)
}
