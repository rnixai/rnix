// Package intent — render.go (Story 38-5 PR11 Step 4(c) · 第 4 个 pane render
// 主体迁出 · 同 alertstrip / detail / security 模式)
//
// Render() 主体迁出自 cmd/rnix/dashboard_intent.go::renderIntentPane · 1:1
// 行为等价（Story 27-7 IntentTree pane + 38-4 P1 stable RootIntent collapse 行为契约保留）。
//
// 接口签名变化（与 alertstrip / detail / security 同模式）：
//   - cmd/rnix renderIntentPane(*dashboardModel, width, height) string
//   - intent Render(state IntentState, ctx RenderContext, innerH int) string
//
// 解耦理由：renderIntentPane 原依赖 dashboardModel.{intent, activePane} ·
// activePane 由 wrapper 注入 RenderContext.IsActive · intent 字段已抽出至
// IntentState（PR6 Step 1 落地）。
//
// helpers (StateColor / StateIcon / IsTreeTerminal / FlattenTrees /
// FlattenTreesWithCollapse / PruneCollapse / AdjustScroll) 一并迁入本包
// （pure functions · 无 cmd/rnix 依赖）。
//
// renderFixedPanel 外层 border 由 cmd/rnix wrapper 包裹（同 detail/security pattern ·
// border / activePane 是 cmd/rnix 内部状态，不属于本 pane 内容渲染职责）。
//
// **38-4 P1 行为契约（关键）保留**：
//   - flattened TreeIndex 仍按 sort 顺序填充；
//   - userCollapsed map keyed by tree.RootIntent（stable across reordering）；
//   - terminal trees (completed/failed) collapsed by default；
//   - PruneCollapse 清除 stale entries（38-4 P4）。
package intent

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/rnixai/rnix/internal/dashboard/detail"
	"github.com/rnixai/rnix/ipc"
)

// RenderContext 注入 cmd/rnix 端运行时上下文（与 detail/security/alertstrip 同模式）。
//
// 字段语义：
//   - IsActive: 当前 pane 是否为 activePane（影响 border 颜色 · 由 wrapper 应用 ·
//     Render() 本身不输出 border，只输出 inner content）。
type RenderContext struct {
	IsActive bool
}

// StateColor 返回 intent state 对应的 lipgloss 颜色（与 cmd/rnix.intentStateColor 等价）。
//
// Story 27-7 mapping (preserved from cmd/rnix):
//   - pending / decomposing / await_confirm → 220 (gray/yellow)
//   - executing → 39 (blue)
//   - completed → 42 (green)
//   - failed → 196 (red)
//   - retrying → 208 (orange)
//   - default → 240 (gray)
func StateColor(state string) lipgloss.Color {
	switch state {
	case "pending":
		return lipgloss.Color("240")
	case "decomposing":
		return lipgloss.Color("220")
	case "await_confirm":
		return lipgloss.Color("220")
	case "executing":
		return lipgloss.Color("39")
	case "completed":
		return lipgloss.Color("42")
	case "failed":
		return lipgloss.Color("196")
	case "retrying":
		return lipgloss.Color("208")
	default:
		return lipgloss.Color("240")
	}
}

// StateIcon 返回 intent state 对应的图标（RNIX_ASCII=1 模式 fallback 到 ASCII）。
//
// 行为等价于 cmd/rnix.intentStateIcon · ASCII fallback 通过 RNIX_ASCII 环境变量
// 检测（与 internal/ui.IsASCIIMode 同模式 · 这里直接读 env 保留原行为）。
func StateIcon(state string) string {
	ascii := os.Getenv("RNIX_ASCII") == "1" || os.Getenv("RNIX_ASCII") == "true"
	switch state {
	case "completed":
		if ascii {
			return "+"
		}
		return "✓"
	case "executing":
		if ascii {
			return ">"
		}
		return "⟳"
	case "pending":
		if ascii {
			return "o"
		}
		return "○"
	case "failed":
		if ascii {
			return "x"
		}
		return "✗"
	case "retrying":
		if ascii {
			return "r"
		}
		return "↻"
	case "await_confirm":
		return "?"
	case "decomposing":
		if ascii {
			return "."
		}
		return "…"
	default:
		if ascii {
			return "o"
		}
		return "○"
	}
}

// IsTreeTerminal 返回该 state 是否为终态（completed 或 failed）。
//
// 终态树默认折叠（Story 38-4 P1 · userCollapsed map 无法让其重新展开）。
func IsTreeTerminal(state string) bool {
	return state == "completed" || state == "failed"
}

// FlattenTrees 是 FlattenTreesWithCollapse(trees, nil) 的便捷封装，保留与
// cmd/rnix.flattenIntentTrees 相同签名供 ATDD 27-7 测试使用。
func FlattenTrees(trees []*ipc.IntentTreeWire) []IntentFlatNode {
	return FlattenTreesWithCollapse(trees, nil)
}

// FlattenTreesWithCollapse flattens trees into a single visible node sequence,
// applying the legacy "terminal trees collapsed" rule plus an optional user
// per-tree collapse map keyed by stable RootIntent (Story 38-4 P1).
//
// 关键行为（与 cmd/rnix.flattenIntentTreesWithCollapse 1:1 等价 · Story 38-4 P1/P4 落地）：
//   - sorted: active trees first, terminal (completed/failed) at bottom (stable sort)；
//   - terminal trees stay collapsed unconditionally；
//   - non-terminal trees honour userCollapsed[tree.RootIntent]；
//   - indent: longest-path BFS over DependsOn (cycles → 0)；
//   - children sorted by (indent, NodeID) for deterministic output。
//
// userCollapsed may be nil — 行为等价于单参 FlattenTrees。
//
// Performance: O(N + E) over Nodes + DependsOn edges per tree · 实际 N ≤ ~50 / tree。
func FlattenTreesWithCollapse(trees []*ipc.IntentTreeWire, userCollapsed map[string]bool) []IntentFlatNode {
	var result []IntentFlatNode

	// AC-2: Sort trees — active first, completed/failed at bottom (stable)
	sorted := make([]*ipc.IntentTreeWire, len(trees))
	copy(sorted, trees)
	sort.SliceStable(sorted, func(i, j int) bool {
		ti := IsTreeTerminal(sorted[i].State)
		tj := IsTreeTerminal(sorted[j].State)
		if ti != tj {
			return !ti // active before terminal
		}
		return false
	})

	for treeIdx, tree := range sorted {
		// Terminal trees collapse unconditionally; non-terminal trees honour
		// the user's per-tree toggle keyed by stable RootIntent (Story
		// 38-4 AC#3 / patch P1).
		collapsed := IsTreeTerminal(tree.State)
		if !collapsed && userCollapsed != nil && userCollapsed[tree.RootIntent] {
			collapsed = true
		}
		result = append(result, IntentFlatNode{
			TreeIndex:    treeIdx,
			IsTreeHeader: true,
			IsCollapsed:  collapsed,
			TreeWire:     tree,
		})

		// AC-2: Terminal trees shown collapsed (header only)
		if collapsed || len(tree.Nodes) == 0 {
			continue
		}

		// Compute indent levels via longest path from roots
		indent := make(map[string]int)

		// Identify root nodes (no valid dependencies)
		for _, node := range tree.Nodes {
			validDeps := 0
			for _, dep := range node.DependsOn {
				if _, exists := tree.Nodes[dep]; exists {
					validDeps++
				}
			}
			if validDeps == 0 {
				indent[node.ID] = 0
			}
		}

		// Iteratively resolve indent levels
		changed := true
		for changed {
			changed = false
			for _, node := range tree.Nodes {
				if _, done := indent[node.ID]; done {
					continue
				}
				maxDepIndent := -1
				allResolved := true
				for _, dep := range node.DependsOn {
					if _, exists := tree.Nodes[dep]; !exists {
						continue
					}
					depIndent, resolved := indent[dep]
					if !resolved {
						allResolved = false
						break
					}
					if depIndent > maxDepIndent {
						maxDepIndent = depIndent
					}
				}
				if allResolved && maxDepIndent >= 0 {
					indent[node.ID] = maxDepIndent + 1
					changed = true
				} else if allResolved {
					indent[node.ID] = 0
					changed = true
				}
			}
		}

		// Handle any unresolved nodes (cycles)
		for _, node := range tree.Nodes {
			if _, ok := indent[node.ID]; !ok {
				indent[node.ID] = 0
			}
		}

		// Collect and sort by (indent, nodeID)
		type nodeEntry struct {
			id     string
			indent int
			node   *ipc.IntentNodeWire
		}
		var nodes []nodeEntry
		for id, node := range tree.Nodes {
			nodes = append(nodes, nodeEntry{id: id, indent: indent[id], node: node})
		}
		sort.Slice(nodes, func(i, j int) bool {
			if nodes[i].indent != nodes[j].indent {
				return nodes[i].indent < nodes[j].indent
			}
			return nodes[i].id < nodes[j].id
		})

		for _, n := range nodes {
			result = append(result, IntentFlatNode{
				TreeIndex: treeIdx,
				NodeID:    n.id,
				Indent:    n.indent,
				Node:      n.node,
				TreeWire:  tree,
			})
		}
	}

	return result
}

// PruneCollapse drops collapse-map entries pointing at trees that are no
// longer in the IPC list (Story 38-4 patch P4). Called from the dispatcher
// after each toggle so the map cannot grow unbounded across long sessions
// where intents come and go.
//
// 行为等价于 cmd/rnix.pruneIntentCollapse（in-place mutation · returns same map）。
func PruneCollapse(userCollapsed map[string]bool, trees []*ipc.IntentTreeWire) map[string]bool {
	if len(userCollapsed) == 0 {
		return userCollapsed
	}
	live := make(map[string]struct{}, len(trees))
	for _, t := range trees {
		live[t.RootIntent] = struct{}{}
	}
	for k := range userCollapsed {
		if _, ok := live[k]; !ok {
			delete(userCollapsed, k)
		}
	}
	return userCollapsed
}

// AdjustScroll ensures state.Cursor is visible within the viewport.
//
// visibleLines 由 caller 传入（cmd/rnix wrapper 用 m.height/2-3 近似 bottom-right
// pane 可见区域 · 与 cmd/rnix.intentAdjustScroll 同语义）。state 为指针接收以便
// in-place 修改 ScrollOffset（与 cmd/rnix wrapper signature 等价）。
func AdjustScroll(state *IntentState, visibleLines int) {
	if state == nil {
		return
	}
	visibleLines = max(visibleLines, 1)
	if state.Cursor < state.ScrollOffset {
		state.ScrollOffset = state.Cursor
	}
	if state.Cursor >= state.ScrollOffset+visibleLines {
		state.ScrollOffset = state.Cursor - visibleLines + 1
	}
}

// Render renders the Intent pane inner content (excluding outer border).
//
// Behaviour contract (preserved from cmd/rnix.renderIntentPane · zero behavior
// change · spec § AC5 intent pane preserved · Story 27-7 AC1-AC6 + 38-4 P1):
//   - state.FlatNodes empty → "当前无意图分解任务..." prompt；
//   - 否则按 viewport (ScrollOffset / innerH-1) 范围渲染 FlatNodes：
//   - tree header 行：cursor + arrow (▶/▷ collapsed) + RootIntent + state + icon；
//     若 cursor 在该 tree 内则 Bold；空 Nodes map 显示 "(分解中...)"；
//   - node 行：cursor + indent + NodeID + intent + icon + (PID:N)；
//   - 树间分隔符 "  ───\n"（从第二棵树起）。
//
// Performance: O(visibleLines) · 实际渲染 ≤ ~30 lines per pane。
//
// Return value: inner content string · caller (cmd/rnix wrapper) 用
// renderFixedPanel(content, width, height, borderColor) 包裹外层 border。
func Render(state IntentState, ctx RenderContext, innerH int) string {
	_ = ctx // RenderContext 当前仅 IsActive · 由 wrapper 决定 border 颜色 · render 主体不消费

	innerH = max(innerH, 1)

	var b strings.Builder
	b.WriteString(" Intent Tree\n")

	if len(state.FlatNodes) == 0 {
		b.WriteString("\n    当前无意图分解任务。使用 rnix apply 创建声明式意图。")
		return b.String()
	}

	// Determine the treeIndex of the currently selected cursor
	cursorTreeIndex := -1
	if state.Cursor >= 0 && state.Cursor < len(state.FlatNodes) {
		cursorTreeIndex = state.FlatNodes[state.Cursor].TreeIndex
	}

	// Viewport: render only visible range (innerH - 1 for the header line)
	visibleLines := max(innerH-1, 1)
	startIdx := max(state.ScrollOffset, 0)
	endIdx := min(startIdx+visibleLines, len(state.FlatNodes))

	for i := startIdx; i < endIdx; i++ {
		n := state.FlatNodes[i]
		cursor := "  "
		if i == state.Cursor {
			cursor = "▸ "
		}

		if n.IsTreeHeader {
			// AC-6: separator between trees (skip first tree)
			if n.TreeIndex > 0 {
				b.WriteString("  ───\n")
			}
			icon := StateIcon(n.TreeWire.State)
			headerStyle := lipgloss.NewStyle().Foreground(StateColor(n.TreeWire.State))
			// AC-6: highlight tree title if cursor is in this tree
			if n.TreeIndex == cursorTreeIndex {
				headerStyle = headerStyle.Bold(true)
			}
			// AC-2: collapsed indicator for terminal trees
			arrow := "▶"
			if n.IsCollapsed {
				arrow = "▷"
			}
			line := fmt.Sprintf("%s%s %s [%s] %s", cursor, arrow, detail.TruncateStr(n.TreeWire.RootIntent, 40), n.TreeWire.State, icon)
			b.WriteString(headerStyle.Render(line))
			b.WriteString("\n")

			// Fix #8: Empty Nodes map shows "(分解中...)"
			if len(n.TreeWire.Nodes) == 0 {
				b.WriteString("    (分解中...)\n")
			}
			continue
		}

		if n.Node == nil {
			continue
		}

		indentStr := strings.Repeat("  ", n.Indent+1)
		icon := StateIcon(n.Node.State)
		intent := detail.TruncateStr(n.Node.Intent, 40)

		var pidStr string
		if n.Node.PID > 0 {
			pidStr = fmt.Sprintf(" (PID:%d)", n.Node.PID)
		}

		stateColor := StateColor(n.Node.State)
		nodeStyle := lipgloss.NewStyle().Foreground(stateColor)

		line := fmt.Sprintf("%s%s%s: %s %s%s", cursor, indentStr, n.NodeID, intent, icon, pidStr)
		b.WriteString(nodeStyle.Render(line))
		b.WriteString("\n")
	}

	return b.String()
}
