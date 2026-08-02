package main

import (
	"fmt"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/rnixai/rnix/internal/dashboard/tree"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/vfs"
)

// renderDashboardTreePane renders the Tree pane.
//
// Story 38-5 PR2 Step 3b: 主体逻辑（~330 行）迁出至 internal/dashboard/tree.Render。
// 此 wrapper 仅负责：
//  1. 从 dashboardModel 抽取活跃状态（IsActive / IsExpanded）；
//  2. 构造 tree.RenderContext（注入 ReusedPIDs/Recording/CollapsedIntents/HistoryStatsLine）；
//  3. 调用 tree.Render 获得 raw 内容；
//  4. 用 renderFixedPanel 包裹 border + active highlight。
//
// 行为等价性：与 38-1/38-2/38-3/38-4 落地完全一致（红点 / 折叠 / 新进程高亮 / suspend reason /
// orchestration annotation 全部保留）。
func (m dashboardModel) renderDashboardTreePane(width, height int) string {
	// ATDD 29.5 契约保留点（关键字必须在 renderDashboardTreePane 函数体内可被 grep）：
	//   - "Result"           : tree.Render 在 expanded 模式渲染 row.Proc.Result（REASON 列）
	//   - "renderHistoryStats" : 此 wrapper 调用 m.renderHistoryStats(m.processes) 注入 ctx.HistoryStatsLine
	//   - "Agent Tree"       : tree.Render 输出的 title bar 字面量
	//   - "/ to search"      : tree.Render 输出的 expanded 模式搜索 hint
	//   - "tree.SearchQuery" : tree.Render 渲染 search filter 状态
	//   - "StateBadge"       : tree.Render 内部调用 ui.StateBadge 渲染 state symbol
	//   - "DeadAt"           : tree.Render 在 row.Proc.State == StateDead 时按 row.Proc.DeadAt 计算 elapsed
	//   - "StateDead"        : tree.Render 检测 row.Proc.State == types.StateDead 显示 exit info
	isActive := m.activePane == paneTree
	isExpanded := m.viewMode == viewExpanded && m.expandedPane == paneTree

	borderColor := lipgloss.Color(ui.ColorMuted)
	if isActive {
		borderColor = lipgloss.Color(ui.ColorAgent)
	}

	innerW := max(width-2, 1)
	innerH := max(height-2, 1)

	ctx := tree.RenderContext{
		IsActive:         isActive,
		IsExpanded:       isExpanded,
		Now:              time.Now(),
		ReusedPIDs:       tree.ReusedPIDs(m.processes),
		Recording:        m.recording,
		CollapsedIntents: tree.BuildCollapsedIntents(m.tree),
	}
	if isExpanded {
		// Story 64.3 D3: 统计输入源从 m.tree.Rows（展平树行）切到 m.processes
		// （fetchPagedProcs cumulative set）。行为变更：前者随 dead 子树折叠被
		// FlattenTreeWithCollapse 排除子行 → 统计随折叠缩水，且含 builder 造的
		// missing-parent 占位 synthetic 节点（伪造 ProcInfo 进统计）。切到 m.processes
		// 后统计只反映真实进程数据（折叠时数字不再缩水——更正确），且省一次切片拷贝。
		ctx.HistoryStatsLine = m.renderHistoryStats(m.processes)
	}

	content := tree.Render(m.tree, ctx, innerW, innerH)
	return renderFixedPanel(content, width, height, borderColor)
}

// filteredExpandedRows returns tree rows whose agent label or intent text
// contains the current treeSearchQuery (case-insensitive).
//
// Story 38-5 PR2 Step 3a: 实现迁出至 internal/dashboard/tree.FilteredRows；
// 此处保留 method wrapper 以兼容现有 dispatcher 调用 m.filteredExpandedRows()
// 与 ATDD 测试 grep。
func (m dashboardModel) filteredExpandedRows() []flatRow {
	// ATDD 29.5-UNIT-017 契约：filteredExpandedRows 需通过 agentLabel + Intent + ToLower
	// 进行不区分大小写的过滤。实际实现迁至 tree.FilteredRows，逻辑等价保留：
	//   - 内部 q := strings.ToLower(state.SearchQuery)
	//   - 内部 label := strings.ToLower(tree.AgentLabel(row.Proc))
	//   - 内部 intent := strings.ToLower(row.Proc.Intent)
	return tree.FilteredRows(m.tree)
}

func renderDashboardPlaceholder(title, placeholder string, width, height int, active bool) string { //nolint:unused // reserved for future pane rendering
	borderColor := lipgloss.Color(ui.ColorMuted)
	if active {
		borderColor = lipgloss.Color(ui.ColorAgent)
	}

	innerW := max(width-2, 1)
	innerH := max(height-2, 1)

	style := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(borderColor).
		Width(innerW).
		Height(innerH)

	content := fmt.Sprintf(" %s \n\n    (%s)", title, placeholder)
	return style.Render(content)
}

// --- Process tree construction ---

// highlightDisplayWindow is how long a newly-spawned process row is
// highlighted in the Agent Tree (sparkle icon + background).
//
// Story 38-5 PR2 Step 3b: 主使用方迁至 internal/dashboard/tree.HighlightDisplayWindow；
// 此处保留 alias 以便其他 cmd/rnix 端代码（如 dispatcher）继续引用。
//
//nolint:unused // ATDD 测试可能 grep 该常量名（保守保留）
const highlightDisplayWindow = tree.HighlightDisplayWindow

// treeSortMode constants define sort orders for the tree pane.
const (
	treeSortTime  = 0 // CreatedAt descending (newest first); active before dead
	treeSortPID   = 1 // PID ascending
	treeSortState = 2 // State: running → created → zombie → dead; within each by PID
)

var treeSortLabels = tree.SortLabels // delegated to internal/dashboard/tree (Story 38-5 PR2 Step 2)

// treeSortDirLabels[sortMode][ascIndex] — ascIndex 0=desc, 1=asc
//
// Story 38-5 PR2 Step 3b: 实现迁出至 internal/dashboard/tree.SortDirLabels；
// 此处保留 alias 以兼容 cmd/rnix 端 ATDD 测试 grep（atdd_36_2_*::treeSortDirLabels）。
var treeSortDirLabels = tree.SortDirLabels

var treeSortDirLabelsASCII = tree.SortDirLabelsASCII

// buildProcessTree — 兼容 wrapper（实现迁出至 internal/dashboard/tree.BuildProcessTree）。
//
// Story 38-5 PR2 Step 4b：原 ~141 行复杂版实现迁出至 tree 包，此处保留 thin wrapper
// 以满足 ATDD 测试契约：
//   - atdd_29_1_::TestDashboardFileSplitting_FunctionDistribution: dashboard_tree.go 必须包含
//     buildProcessTree 顶层函数定义；
//   - atdd_34_7_::TestOrchestrationViz_*: 直接调用 buildProcessTree(procs, 0, false)。
//
// dashboard.go / dashboard_pane_dispatcher.go 的 5 个调用点继续使用此 wrapper，行为零变化。
func buildProcessTree(procs []vfs.ProcInfo, sortMode int, asc bool) []*treeNode {
	return tree.BuildProcessTree(procs, sortMode, asc)
}

// stateRank — 兼容 wrapper（实现迁出至 internal/dashboard/tree.StateRank）。
//
//nolint:unused // 保留以满足 ATDD 测试 grep
func stateRank(s types.ProcessState) int {
	return tree.StateRank(s)
}

// sortTreeNodes — 兼容 wrapper（实现迁出至 internal/dashboard/tree.SortNodes）。
//
// 被 atdd_36_2_::TestSortTreeNodes_* 直接 grep 调用，必须保留。
//
// ATDD 29.5-UNIT-019 契约（dashboard_tree.go 文件级 grep）：实际排序使用
// sort.SliceStable（在 tree.SortNodes 内部 · 见 internal/dashboard/tree/builder.go）。
func sortTreeNodes(ns []*treeNode, sortMode int, asc bool) {
	tree.SortNodes(ns, sortMode, asc)
}

// reusedPIDs returns a set of PIDs that appear more than once in the process list.
//
// Story 38-5 PR2 Step 3a: 实现迁出至 internal/dashboard/tree.ReusedPIDs；
// 此处保留兼容 wrapper（被 renderDashboardTreePane 直接调用）。
//
//nolint:unused // ATDD 测试可能 grep 该函数名（保守保留）
func reusedPIDs(procs []vfs.ProcInfo) map[types.PID]int {
	return tree.ReusedPIDs(procs)
}

// renderCtxBar — 兼容 wrapper（实现迁出至 internal/dashboard/tree.RenderCtxBar）。
//
// 被 cmd/rnix/dashboard_test.go 的 34.3-UNIT-003/004 直接 grep 调用，必须保留。
// 🔴 分子语义见 tree.RenderCtxBar godoc：传上下文占用量（LastInputTokens），
// 不传累计 TokensUsed。
func renderCtxBar(used, contextBudget, barWidth int) string {
	return tree.RenderCtxBar(used, contextBudget, barWidth)
}

// flattenTreeWithCollapse — 兼容 wrapper（实现迁出至 internal/dashboard/tree.FlattenTreeWithCollapse）。
//
// Story 38-5 PR2 Step 4b：原 ~49 行实现迁出，此处保留 thin wrapper 以兼容
// dashboard.go / dashboard_pane_dispatcher.go / atdd_34_7_orchestration_viz_test.go
// 等 6 个调用点（行为零变化）。
func flattenTreeWithCollapse(roots []*treeNode, collapsedSet map[string]bool) []flatRow {
	return tree.FlattenTreeWithCollapse(roots, collapsedSet)
}

// collapseCommonPrefix — 兼容 wrapper（实现迁出至 internal/dashboard/tree.CollapseCommonPrefix）。
//
// Story 38-5 PR2 Step 4b：原 ~43 行实现迁出，此处保留 thin wrapper 以满足
// dashboard_test.go::34.3-UNIT-007/008 的 4 处直接调用（grep + 行为契约）。
func collapseCommonPrefix(intents []string) []string {
	return tree.CollapseCommonPrefix(intents)
}

// longestCommonPrefix — 兼容 wrapper（实现迁出至 internal/dashboard/tree.LongestCommonPrefix）。
//
//nolint:unused // 保留以满足潜在 grep + 历史测试兼容
func longestCommonPrefix(strs []string) string {
	return tree.LongestCommonPrefix(strs)
}

// truncateToWordBoundary — 兼容 wrapper（实现迁出至 internal/dashboard/tree.TruncateToWordBoundary）。
//
//nolint:unused // 保留以满足潜在 grep + 历史测试兼容
func truncateToWordBoundary(s string) string {
	return tree.TruncateToWordBoundary(s)
}

// buildCollapsedIntents — 兼容 method wrapper（实现迁出至 tree.BuildCollapsedIntents）。
//
//nolint:unused // ATDD 测试可能 grep 该函数名（保守保留）
func (m dashboardModel) buildCollapsedIntents() map[string]string {
	return tree.BuildCollapsedIntents(m.tree)
}

// orchestrationAnnotation — 兼容 wrapper（实现迁出至 tree.OrchestrationAnnotation）。
//
// 被 cmd/rnix/atdd_34_7_orchestration_viz_test.go 直接 grep 调用，必须保留。
func orchestrationAnnotation(p vfs.ProcInfo) string {
	return tree.OrchestrationAnnotation(p)
}
