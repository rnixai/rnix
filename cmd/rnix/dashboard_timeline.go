package main

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"

	dashboardevent "github.com/rnixai/rnix/internal/dashboard/event"
	"github.com/rnixai/rnix/internal/dashboard/inspector"
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

// scriptAggGroup — alias of dashboardevent.ScriptAggGroup (Story 43-3 review patch P11).
type scriptAggGroup = dashboardevent.ScriptAggGroup

// buildScriptAggGroups — thin wrapper · 见 internal/dashboard/event.BuildScriptAggGroups.
// Story 43-3 review patch P11 (D2 follow-through).
func buildScriptAggGroups(events []UnifiedEvent) []scriptAggGroup {
	return dashboardevent.BuildScriptAggGroups(events)
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
		m.statusMsg = "Timeline switched to ascending (newest at bottom). Press o to toggle."
		m.statusMsgTTL = 5
	}
	return m
}

// handleTimelinePIDChange wrapper 已于 Story 38-5 PR11 Step 4(b) Phase 2 删除。
//
// 历史：cmd/rnix wrapper 调用 timeline.HandlePIDUUIDChange + 重置 m.search 4 字段（Story 36-5 P-1）。
//
// 删除理由：spec § 04 风险 5 中断保险原则 · 把 wrapper 整体迁至
// internal/dashboard/timeline.HandlePIDUUIDChangeWithSearch（接受 SearchResetter
// 接口避免硬依赖 plugin 包）· cmd/rnix 端调用站点零修改通过 ATDD 27-3 / 36-4 /
// 36-5 / 36-6 + 38-1/2/3/4 全部测试。

// toFoldGroups converts event-package ToolAggGroups to timeline-package FoldGroups.
// Needed because timeline cannot import event (circular dependency).
func toFoldGroups(groups []toolAggGroup) []timeline.FoldGroup {
	fg := make([]timeline.FoldGroup, len(groups))
	for i, g := range groups {
		fg[i] = timeline.FoldGroup{
			StartIdx: g.StartIdx,
			EndIdx:   g.EndIdx,
			Key:      g.StepNums[0],
		}
	}
	return fg
}

// unifiedCursorToStepOrd 把 filtered unified 事件索引空间的 cursor 换算成 step-only
// 序号（cursor 之前及其位置的 StepEntry 计数 · 0-based）。cursor 落在 sys 事件上时
// 投影到其之前的最近 step 序号。与 dashboard_pane_dispatcher.go Enter 分支的 stepIdx
// 计算同源（spec-timeline-agg-nav-fix Finding 8：aggregation 渲染/导航统一 step-only 空间）。
func unifiedCursorToStepOrd(filtered []UnifiedEvent, cursor int) int {
	cursorPos := min(cursor, len(filtered)-1)
	ord := 0
	for i := 0; i < cursorPos && i < len(filtered); i++ {
		if filtered[i].StepEntry != nil {
			ord++
		}
	}
	return ord
}

// stepOrdToUnifiedCursor 把 step-only 序号（0-based）映射回 filtered unified 索引空间的
// cursor（第 ord 个 StepEntry 的 unified 下标）。ord 越界时 clamp 到末尾 unified 索引。
func stepOrdToUnifiedCursor(filtered []UnifiedEvent, ord int) int {
	if len(filtered) == 0 {
		return 0
	}
	count := 0
	for i, ev := range filtered {
		if ev.StepEntry != nil {
			if count == ord {
				return i
			}
			count++
		}
	}
	return len(filtered) - 1
}

// aggStepAnchor 把一个 step-only 序号 ord 归一化为该组的「导航锚点」序号：
// 折叠组内的任何 ord 都锚定到组起点（group*AggGroupSize），展开组内 ord 原样保留。
// 这让折叠组作为单一可见单元、展开组按行——j/k 的移动因此对称（Finding 缺陷 2）。
func aggStepAnchor(ord int, expanded map[int]bool) int {
	group := ord / timeline.AggGroupSize
	if expanded[group] {
		return ord
	}
	return group * timeline.AggGroupSize
}

// aggNextVisibleOrd 从当前 step-only 序号 ord 沿 dir（+1 前进 / -1 后退）移动一个
// 「可见单元」：折叠组整体算 1 个单元（跳到相邻组起点），展开组内每行算 1 个单元。
// clamp 到 [0, stepCount-1]。这是 aggregation 导航的对称移动核心。
func aggNextVisibleOrd(ord, dir, stepCount int, expanded map[int]bool) int {
	ord = aggStepAnchor(ord, expanded)
	group := ord / timeline.AggGroupSize
	if dir > 0 {
		if expanded[group] {
			// 展开组内：逐行前进。next 若跨入新组则恰为下一组锚点起点，语义一致。
			return min(ord+1, stepCount-1)
		}
		// 折叠组：跳到下一组起点。
		return min((group+1)*timeline.AggGroupSize, stepCount-1)
	}
	// dir < 0
	if group == 0 && ord == 0 {
		return 0
	}
	if expanded[group] && ord > group*timeline.AggGroupSize {
		// 展开组内且不在组起点：逐行后退。
		return ord - 1
	}
	// 位于组起点（或折叠组）：退到上一组的「末可见单元」。
	prevGroup := group - 1
	if prevGroup < 0 {
		return 0
	}
	if expanded[prevGroup] {
		// 上一组展开：落到其最后一行（组末 ord），clamp 到 stepCount-1。
		return min((prevGroup+1)*timeline.AggGroupSize-1, stepCount-1)
	}
	// 上一组折叠：落到其起点。
	return prevGroup * timeline.AggGroupSize
}

// aggNavigate 处理 aggregation 模式（step 数 >100）下的导航键。不同于普通折叠导航，
// 它在 step-only 序号空间以「可见单元」为粒度移动（折叠 chunk 组整体 1 单元 · 展开组内
// 逐行），再经 stepOrdToUnifiedCursor 映射回 StepCursor —— 彻底跳过 ToolPath 折叠 vis
// 路径，修复 spec-timeline-agg-nav-fix 的 j/k 弹跳/冻结（Finding 6/7）。
func (m dashboardModel) aggNavigate(key string, filtered []UnifiedEvent, stepCount, pageSize int) dashboardModel {
	curOrd := unifiedCursorToStepOrd(filtered, m.timeline.StepCursor)
	expanded := m.timeline.ExpandedChunkGroups
	newOrd := aggStepAnchor(curOrd, expanded)
	switch key {
	case "j", "down":
		newOrd = aggNextVisibleOrd(curOrd, +1, stepCount, expanded)
	case "k", "up":
		newOrd = aggNextVisibleOrd(curOrd, -1, stepCount, expanded)
	case "g", "home":
		newOrd = 0
		m.timeline.StepScrollTop = 0
	case "G", "end", "shift+G":
		newOrd = stepCount - 1
	case "pgdown":
		// 翻页 = 连续前进 pageSize 个可见单元（每单元 clamp，对折叠/展开一致）。
		for range max(pageSize, 1) {
			newOrd = aggNextVisibleOrd(newOrd, +1, stepCount, expanded)
		}
	case "pgup":
		for range max(pageSize, 1) {
			newOrd = aggNextVisibleOrd(newOrd, -1, stepCount, expanded)
		}
	}
	newOrd = max(0, min(newOrd, stepCount-1))
	m.timeline.StepCursor = stepOrdToUnifiedCursor(filtered, newOrd)
	m.ensureStepCursorVisible(pageSize)
	return m
}

// handleTimelineKey dispatches keys for the unified Step timeline.
func (m dashboardModel) handleTimelineKey(key string) dashboardModel {
	if m.timeline.StepFilterMode {
		return m.handleStepFilterKey(key)
	}
	if m.search.Mode {
		return m.handleTimelineSearchKey(key)
	}
	filtered := m.filteredUnifiedEvents()
	pageSize := max(m.dashboardVisibleLines()-4, 1)

	// spec-timeline-agg-nav-fix: aggregation 模式（step 数 >100 · 与 Enter 分支和
	// renderStepTimeline 的 useAggregation 同条件）下导航键走 chunk-group 专属路径，
	// 跳过 ToolPath 折叠 vis（否则巨型同 ToolPath 组会把 vis 收缩到 2-3 个索引 →
	// j/k 弹跳/冻结 · Finding 6/7）。非导航键（/ [ ] f e E C o n N）仍落到下方 switch。
	//
	// 阈值口径与渲染层同源（filteredStepCount = filtered unified 里 EventStep 计数 ·
	// 见 filteredStepCount godoc）——渲染层是显示模式的真相源，导航/Enter 必须匹配它，
	// 否则 100 边界处会出现「导航走 chunk 空间但渲染走普通折叠」的模式失配（评审观察项 1）。
	stepCount := m.filteredStepCount()
	if stepCount > 100 {
		switch key {
		case "j", "down", "k", "up", "g", "home", "G", "end", "shift+G", "pgdown", "pgup":
			return m.aggNavigate(key, filtered, stepCount, pageSize)
		}
	}

	// Story 41-3: build fold navigation data for visible-index navigation
	aggGroups := buildToolAggGroups(filtered)
	foldGroups := toFoldGroups(aggGroups)
	vis := timeline.BuildVisibleIndices(len(filtered), m.timeline.ExpandedAggGroups, foldGroups)
	hasFolding := len(vis) > 0 && len(vis) < len(filtered)

	if hasFolding {
		// Story 41-3 AC1: j/k/↑/↓ navigate by visible rows, skipping collapsed interiors
		switch key {
		case "j", "down", "k", "up":
			delta := 1
			if key == "k" || key == "up" {
				delta = -1
			}
			m.timeline.StepCursor = timeline.NextVisibleIndex(m.timeline.StepCursor, vis, delta)
			m.ensureStepCursorVisible(pageSize)
			return m
		}
		// g/G/PgDn/PgUp/Home/End: map cursor to visible-index space, delegate
		// to HandleListKey, then map back to filtered-index space.
		visCursor := timeline.VisiblePosition(m.timeline.StepCursor, vis)
		navOpts := ui.ListNavOpts{PageSize: pageSize}
		if ui.HandleListKey(key, nil, &visCursor, len(vis), navOpts) {
			visCursor = max(0, min(visCursor, len(vis)-1))
			m.timeline.StepCursor = vis[visCursor]
			if key == "g" || key == "home" {
				m.timeline.StepScrollTop = 0
			}
			m.ensureStepCursorVisible(pageSize)
			return m
		}
	} else {
		// No folding active: original HandleListKey behavior
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
			if key == "g" || key == "home" {
				m.timeline.StepScrollTop = 0
			}
			m.ensureStepCursorVisible(pageSize)
			return m
		}
	}

	switch key {
	case "/":
		m.search.EnterSearchMode(false)
		return m
	case "[":
		// Story 41-3 AC6: dual-mode — search active → prev match; else → prev group
		if len(m.search.Matches) > 0 {
			n := len(m.search.Matches)
			m.search.MatchIdx = ((m.search.MatchIdx-1)%n + n) % n
			m.timeline.StepCursor = m.search.Matches[m.search.MatchIdx]
			m.autoExpandGroupForCursor()
		} else if len(foldGroups) > 0 {
			prev := m.timeline.StepCursor
			m.timeline.StepCursor = timeline.FindGroupBoundary(m.timeline.StepCursor, foldGroups, -1)
			if m.timeline.StepCursor == prev {
				m.statusMsg = "No more groups"
				m.statusMsgTTL = statusMsgDefaultTTL
			}
		}
	case "]":
		// Story 41-3 AC6: dual-mode — search active → next match; else → next group
		if len(m.search.Matches) > 0 {
			n := len(m.search.Matches)
			m.search.MatchIdx = (m.search.MatchIdx + 1) % n
			m.timeline.StepCursor = m.search.Matches[m.search.MatchIdx]
			m.autoExpandGroupForCursor()
		} else if len(foldGroups) > 0 {
			prev := m.timeline.StepCursor
			m.timeline.StepCursor = timeline.FindGroupBoundary(m.timeline.StepCursor, foldGroups, 1)
			if m.timeline.StepCursor == prev {
				m.statusMsg = "No more groups"
				m.statusMsgTTL = statusMsgDefaultTTL
			}
		}
	case "f":
		m.timeline.StepFilterMode = true
		m.statusMsg = dashboardevent.FilterHelpMsg
		m.statusMsgTTL = statusMsgDefaultTTL
	case "e":
		var msg string
		m.timeline, msg = timeline.ApplyExpandMode(m.timeline, expandModeExpanded, hasExpandableContent)
		m.statusMsg = msg
		m.statusMsgTTL = statusMsgDefaultTTL
	case "E":
		var msg string
		m.timeline, msg = timeline.ApplyExpandMode(m.timeline, expandModeErrorsOnly, hasExpandableContent)
		m.statusMsg = msg
		m.statusMsgTTL = statusMsgDefaultTTL
	case "C":
		var msg string
		m.timeline, msg = timeline.ApplyExpandMode(m.timeline, expandModeCollapsed, hasExpandableContent)
		m.statusMsg = msg
		m.statusMsgTTL = statusMsgDefaultTTL
	case "o":
		var msg string
		m.timeline, msg = timeline.ToggleSortDirection(m.timeline)
		m.statusMsg = msg
		m.statusMsgTTL = statusMsgDefaultTTL
	case "n":
		if len(m.search.Matches) > 0 {
			n := len(m.search.Matches)
			m.search.MatchIdx = (m.search.MatchIdx + 1) % n
			m.timeline.StepCursor = m.search.Matches[m.search.MatchIdx]
			m.autoExpandGroupForCursor()
			m.ensureStepCursorVisible(pageSize)
			break
		}
		if idx, ok := dashboardevent.FindNextErrorIdx(filtered, m.timeline.StepCursor); ok {
			m.timeline.StepCursor = idx
		} else {
			m.statusMsg = "No more errors"
			m.statusMsgTTL = statusMsgDefaultTTL
		}
	case "N", "shift+N":
		if len(m.search.Matches) > 0 {
			n := len(m.search.Matches)
			m.search.MatchIdx = ((m.search.MatchIdx-1)%n + n) % n
			m.timeline.StepCursor = m.search.Matches[m.search.MatchIdx]
			m.autoExpandGroupForCursor()
			m.ensureStepCursorVisible(pageSize)
			break
		}
		if idx, ok := dashboardevent.FindPrevErrorIdx(filtered, m.timeline.StepCursor); ok {
			m.timeline.StepCursor = idx
		} else {
			m.statusMsg = "No more errors"
			m.statusMsgTTL = statusMsgDefaultTTL
		}
	}
	m.ensureStepCursorVisible(pageSize)
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

// filteredStepCount 返回 filtered unified 事件里 EventStep 的数量——这是 aggregation
// 显示模式（renderStepTimeline 的 useAggregation）的判据真相源。spec-timeline-agg-nav-fix
// 观察项 1：导航守卫（handleTimelineKey）、Enter 分支（dashboard_pane_dispatcher.go）
// 与渲染层三处必须用同一计数口径，否则数据合并时序窗口下可能在 100 边界出现
// 「导航走 chunk 空间但渲染走普通折叠」的分叉。统一走此 helper 杜绝漂移。
func (m dashboardModel) filteredStepCount() int {
	n := 0
	for _, ev := range m.filteredUnifiedEvents() {
		if ev.Type == EventStep {
			n++
		}
	}
	return n
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

	// Count step events and system events in filtered list.
	// stepCount 走 filteredStepCount helper（与导航守卫/Enter 分支同源 · 观察项 1）。
	stepCount := m.filteredStepCount()
	sysCount := len(filtered) - stepCount

	// Header
	if m.timeline.StepFilterMode {
		b.WriteString(m.renderStepFilterBar(truncW))
	} else {
		b.WriteString(m.renderUnifiedStepHeader(truncW, total, len(filtered), sysCount))
	}
	b.WriteString("\n")

	if m.selectedPID == 0 && m.selectedUUID == "" {
		b.WriteString("\n    Select an agent to view timeline")
		return b.String()
	}

	if total == 0 && len(m.unifiedEvents) == 0 {
		if m.isSelectedProcessDead() {
			b.WriteString("\n    No steps recorded.")
			for _, p := range m.processes {
				match := false
				if m.selectedPID > 0 {
					match = p.PID == m.selectedPID && (m.selectedUUID == "" || p.UUID == m.selectedUUID)
				} else if m.selectedUUID != "" {
					match = p.UUID == m.selectedUUID
				}
				if match {
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
		// spec-timeline-agg-nav-fix Finding 8: StepCursor 是 filtered unified 索引空间，
		// 换算成 step-only 序号后传给渲染层，修正 cursor 高亮 off-by-N。
		cursorStepOrd := unifiedCursorToStepOrd(filtered, m.timeline.StepCursor)
		aggLines := m.renderAggregatedTimeline(&b, stepIndices, cursorStepOrd, truncW, listLines, showToken, showDuration)
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

	// Story 43-3 review patch P11 (D2 follow-through): build ScriptAggGroups
	// for EventScript runs (≥3 same stmt_kind). Independent namespace from
	// ToolAggGroup — keyed by StartIdx in filtered slice.
	scriptGroups := buildScriptAggGroups(filtered)
	scriptAggByStart := make(map[int]*scriptAggGroup, len(scriptGroups))
	scriptSkipSet := make(map[int]bool)
	scriptExpandedSet := make(map[int]bool)
	for i := range scriptGroups {
		g := &scriptGroups[i]
		scriptAggByStart[g.StartIdx] = g
		expanded := m.timeline.ExpandedScriptAggGroups[g.StartIdx]
		if !expanded {
			for fi := g.StartIdx + 1; fi < g.EndIdx; fi++ {
				scriptSkipSet[fi] = true
			}
		} else {
			for fi := g.StartIdx + 1; fi < g.EndIdx; fi++ {
				scriptExpandedSet[fi] = true
			}
		}
	}

	for fi := startIdx; fi < endIdx && linesUsed < listLines; fi++ {
		ev := filtered[fi]

		// System event: single-line rendering (plus optional detail line when selected)
		if ev.StepEntry == nil {
			// Story 43-3 review patch P11: ScriptAggGroup fold handling.
			// Skip individual events that live inside a *collapsed* group.
			if scriptSkipSet[fi] {
				continue
			}
			// Render ScriptAggGroup header at the start index. For collapsed
			// groups this is the only row; for expanded groups it precedes
			// the individual events (which fall through to normal sys-event
			// rendering with indentation indicated by scriptExpandedSet).
			if g, ok := scriptAggByStart[fi]; ok {
				expanded := m.timeline.ExpandedScriptAggGroups[g.StartIdx]
				cursorInGroup := m.timeline.StepCursor >= g.StartIdx && m.timeline.StepCursor < g.EndIdx
				ascii := ui.IsASCIIMode()
				kindGlyph := scriptGlyph("begin", ascii)
				var marker string
				if expanded {
					if ascii {
						marker = "v"
					} else {
						marker = "▼"
					}
				} else {
					if ascii {
						marker = ">"
					} else {
						marker = "▶"
					}
				}
				lineRange := fmt.Sprintf("L%d-L%d", g.FirstLine, g.LastLine)
				header := fmt.Sprintf("%s %s %s %s × %d", marker, lineRange, kindGlyph, g.StmtKind, g.Count)
				cursorMark := "  "
				if cursorInGroup && !expanded {
					cursorMark = "▸ "
				}
				headerLine := cursorMark + header
				if cursorInGroup && !expanded {
					headerLine = lipgloss.NewStyle().
						Background(lipgloss.Color("#2D2D3D")).
						Foreground(lipgloss.Color("#FFFFFF")).
						Render(headerLine)
				} else if expanded {
					headerLine = lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted)).Render(headerLine)
				}
				b.WriteString(truncateAnsi(headerLine, truncW))
				b.WriteString("\n")
				linesUsed++
				if !expanded {
					continue
				}
				if linesUsed >= listLines {
					continue
				}
				// Fall through to render the first event of the expanded group
				// using the normal sys-event rendering below (indented via
				// scriptExpandedSet marker).
			}
			cursorMark := "  "
			if scriptExpandedSet[fi] || (scriptAggByStart[fi] != nil && m.timeline.ExpandedScriptAggGroups[fi]) {
				cursorMark = "    " // 4-char indent for events inside an expanded ScriptAggGroup
			}
			if fi == m.timeline.StepCursor {
				if scriptExpandedSet[fi] || (scriptAggByStart[fi] != nil && m.timeline.ExpandedScriptAggGroups[fi]) {
					cursorMark = "  ▸ "
				} else {
					cursorMark = "▸ "
				}
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
				// Story 41-3 AC3/AC4: collapsed group with aggregated summary
				cursorMark := "  "
				if cursorInGroup {
					cursorMark = "▸ "
				}
				marker := "▶"
				if ui.IsASCIIMode() {
					marker = ">"
				}

				// Aggregate summary with three-level degradation
				var totalFreshTok int
				var totalDur float64
				var errCount int
				loadedCount := 0
				for idx := g.StartIdx; idx < g.EndIdx; idx++ {
					if filtered[idx].StepEntry != nil {
						s := filtered[idx].StepEntry.Summary
						totalDur += s.DurationMs
						if s.HasError {
							errCount++
						}
						if detail := m.timeline.StepDetailCache[s.Step]; detail != nil {
							// Show "fresh" tokens (non-cached input + output) so high
							// cache-hit scenarios don't display misleading megaton-scale
							// numbers. openai-family semantics include cached inside
							// InputTokens; Anthropic semantics exclude it (see CLAUDE.md
							// "Driver Token Semantics").
							freshInput := detail.InputTokens
							if !inspector.IsAnthropicDriver(detail.DriverType) {
								freshInput = max(0, detail.InputTokens-detail.CachedInputTokens)
							}
							totalFreshTok += freshInput + detail.OutputTokens
							loadedCount++
						}
					}
				}

				stepRange := fmt.Sprintf("step %d-%d", g.StepNums[0], g.StepNums[len(g.StepNums)-1])
				var summaryParts []string
				summaryParts = append(summaryParts, stepRange)

				allLoaded := loadedCount == len(g.StepNums)
				if loadedCount > 0 {
					tokLabel := formatTokenCount(totalFreshTok)
					if !allLoaded {
						tokLabel = "~" + tokLabel
					}
					summaryParts = append(summaryParts, tokLabel+" fresh tok")
				}
				if errCount > 0 {
					summaryParts = append(summaryParts, fmt.Sprintf("%d err", errCount))
				}
				if allLoaded {
					summaryParts = append(summaryParts, formatTimelineDuration(totalDur))
				}

				bracketContent := strings.Join(summaryParts, " · ")
				headerLine := fmt.Sprintf("%s%s %s x%d [%s]",
					cursorMark, marker, g.ToolPath, len(g.StepNums), bracketContent)

				if cursorInGroup {
					headerLine = lipgloss.NewStyle().
						Background(lipgloss.Color("#2D2D3D")).
						Foreground(lipgloss.Color("#FFFFFF")).
						Render(headerLine)
				}

				b.WriteString(truncateAnsi(headerLine, truncW))
				b.WriteString("\n")
				linesUsed++
				continue
			}
			// Story 41-3 AC4: expanded group header — ▼ marker
			dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(ui.ColorMuted))
			marker := "▼"
			if ui.IsASCIIMode() {
				marker = "v"
			}
			expandHeader := fmt.Sprintf("  %s %s x%d [step %d-%d]",
				marker, g.ToolPath, len(g.StepNums),
				g.StepNums[0], g.StepNums[len(g.StepNums)-1])
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
func (m dashboardModel) renderAggregatedTimeline(b *strings.Builder, filtered []int, cursorStepOrd, truncW, listLines int, showToken, showDuration bool) int {
	return timeline.RenderAggregatedTimeline(b, m.timeline, filtered, cursorStepOrd, truncW, listLines, showToken, showDuration)
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

// autoExpandGroupForCursor auto-expands any collapsed group that contains
// the current StepCursor, ensuring the target step is visible after a
// cross-pane jump (Story 41-3 AC8).
func (m *dashboardModel) autoExpandGroupForCursor() {
	filtered := m.filteredUnifiedEvents()
	if len(filtered) == 0 {
		return
	}
	cursor := m.timeline.StepCursor
	for _, g := range buildToolAggGroups(filtered) {
		if cursor > g.StartIdx && cursor < g.EndIdx {
			key := g.StepNums[0]
			if !m.timeline.ExpandedAggGroups[key] {
				if m.timeline.ExpandedAggGroups == nil {
					m.timeline.ExpandedAggGroups = make(map[int]bool)
				}
				m.timeline.ExpandedAggGroups[key] = true
			}
			break
		}
	}
}

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
	// AC7: sticky mode decides initial fold state for new groups.
	// When ExpandMode=all, auto-expand groups that have no explicit override.
	if m.timeline.ExpandMode == expandModeExpanded {
		filtered := m.filteredUnifiedEvents()
		for _, g := range buildToolAggGroups(filtered) {
			key := g.StepNums[0]
			if _, exists := m.timeline.ExpandedAggGroups[key]; !exists {
				if m.timeline.ExpandedAggGroups == nil {
					m.timeline.ExpandedAggGroups = make(map[int]bool)
				}
				m.timeline.ExpandedAggGroups[key] = true
			}
		}
	}
	return m
}

// isSelectedProcessDead returns true if the currently selected process is in Dead state.
func (m dashboardModel) isSelectedProcessDead() bool {
	if m.selectedPID == 0 && m.selectedUUID == "" {
		return false
	}
	for _, p := range m.processes {
		if m.selectedPID > 0 {
			if p.PID == m.selectedPID && (m.selectedUUID == "" || p.UUID == m.selectedUUID) {
				return p.State == types.StateDead
			}
		} else if m.selectedUUID != "" && p.UUID == m.selectedUUID {
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
	if m.timeline.FetchingDetail || !m.hasProcessSelected() {
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
	return fetchStepDetailCmd(m.selectedPID, m.selectedUUID, step)
}

// --- Fetch commands ---

func fetchStepsCmd(uuid string, pid types.PID, afterStep int) tea.Cmd {
	return func() tea.Msg {
		client, err := ipc.Dial(ipc.SocketPath())
		if err != nil {
			return stepListMsg{UUID: uuid, PID: pid, Err: err}
		}
		defer client.Close()
		var resp *ipc.ListStepsResponse
		if uuid != "" {
			resp, err = client.ListStepsByUUID(uuid, afterStep)
		} else {
			resp, err = client.ListSteps(pid, afterStep)
		}
		if err != nil {
			return stepListMsg{UUID: uuid, PID: pid, Err: err}
		}
		return stepListMsg{UUID: uuid, PID: pid, Steps: resp.Steps, Total: resp.Total}
	}
}

func fetchStepDetailCmd(pid types.PID, uuid string, step int) tea.Cmd {
	return func() tea.Msg {
		client, err := ipc.Dial(ipc.SocketPath())
		if err != nil {
			return stepDetailResultMsg{step: step, err: err}
		}
		defer client.Close()
		var detail *ipc.GetStepDetailResponse
		if uuid != "" {
			detail, err = client.GetStepDetailByUUID(uuid, step)
		} else {
			detail, err = client.GetStepDetail(pid, step)
		}
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
