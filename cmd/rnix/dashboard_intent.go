package main

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"

	tea "charm.land/bubbletea/v2"
)

// --- Intent Tree Pane (Story 27-7) ---

func intentStateColor(state string) lipgloss.Color {
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

func intentStateIcon(state string) string {
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

func isIntentTreeTerminal(state string) bool {
	return state == "completed" || state == "failed"
}

func flattenIntentTrees(trees []*ipc.IntentTreeWire) []intentFlatNode {
	var result []intentFlatNode

	// AC-2: Sort trees — active first, completed/failed at bottom
	sorted := make([]*ipc.IntentTreeWire, len(trees))
	copy(sorted, trees)
	sort.SliceStable(sorted, func(i, j int) bool {
		ti := isIntentTreeTerminal(sorted[i].State)
		tj := isIntentTreeTerminal(sorted[j].State)
		if ti != tj {
			return !ti // active before terminal
		}
		return false
	})

	for treeIdx, tree := range sorted {
		collapsed := isIntentTreeTerminal(tree.State)
		result = append(result, intentFlatNode{
			treeIndex:    treeIdx,
			isTreeHeader: true,
			isCollapsed:  collapsed,
			treeWire:     tree,
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
			result = append(result, intentFlatNode{
				treeIndex: treeIdx,
				nodeID:    n.id,
				indent:    n.indent,
				node:      n.node,
				treeWire:  tree,
			})
		}
	}

	return result
}

func fetchIntentTreesCmd() tea.Cmd {
	return func() tea.Msg {
		client, err := ipc.Dial(ipc.SocketPath())
		if err != nil {
			return intentTreesMsg{err: err}
		}
		defer client.Close()
		resp, err := client.IntentList()
		return intentTreesMsg{trees: resp, err: err}
	}
}

func (m dashboardModel) renderIntentPane(width, height int) string {
	isActive := m.activePane == paneIntent

	borderColor := lipgloss.Color(ui.ColorMuted)
	if isActive {
		borderColor = lipgloss.Color(ui.ColorAgent)
	}

	innerH := max(height-2, 1)

	var b strings.Builder
	b.WriteString(" Intent Tree\n")

	if len(m.intentFlatNodes) == 0 {
		b.WriteString("\n    当前无意图分解任务。使用 rnix apply 创建声明式意图。")
		return renderFixedPanel(b.String(), width, height, borderColor)
	}

	// Determine the treeIndex of the currently selected cursor
	cursorTreeIndex := -1
	if m.intentCursor < len(m.intentFlatNodes) {
		cursorTreeIndex = m.intentFlatNodes[m.intentCursor].treeIndex
	}

	// Viewport: render only visible range (innerH - 1 for the header line)
	visibleLines := max(innerH-1, 1)
	startIdx := m.intentScrollOffset
	endIdx := min(startIdx+visibleLines, len(m.intentFlatNodes))

	for i := startIdx; i < endIdx; i++ {
		n := m.intentFlatNodes[i]
		cursor := "  "
		if i == m.intentCursor {
			cursor = "▸ "
		}

		if n.isTreeHeader {
			// AC-6: separator between trees (skip first tree)
			if n.treeIndex > 0 {
				b.WriteString("  ───\n")
			}
			icon := intentStateIcon(n.treeWire.State)
			headerStyle := lipgloss.NewStyle().Foreground(intentStateColor(n.treeWire.State))
			// AC-6: highlight tree title if cursor is in this tree
			if n.treeIndex == cursorTreeIndex {
				headerStyle = headerStyle.Bold(true)
			}
			// AC-2: collapsed indicator for terminal trees
			arrow := "▶"
			if n.isCollapsed {
				arrow = "▷"
			}
			line := fmt.Sprintf("%s%s %s [%s] %s", cursor, arrow, truncateStr(n.treeWire.RootIntent, 40), n.treeWire.State, icon)
			b.WriteString(headerStyle.Render(line))
			b.WriteString("\n")

			// Fix #8: Empty Nodes map shows "(分解中...)"
			if len(n.treeWire.Nodes) == 0 {
				b.WriteString("    (分解中...)\n")
			}
			continue
		}

		if n.node == nil {
			continue
		}

		indentStr := strings.Repeat("  ", n.indent+1)
		icon := intentStateIcon(n.node.State)
		intent := truncateStr(n.node.Intent, 40)

		var pidStr string
		if n.node.PID > 0 {
			pidStr = fmt.Sprintf(" (PID:%d)", n.node.PID)
		}

		stateColor := intentStateColor(n.node.State)
		nodeStyle := lipgloss.NewStyle().Foreground(stateColor)

		line := fmt.Sprintf("%s%s%s: %s %s%s", cursor, indentStr, n.nodeID, intent, icon, pidStr)
		b.WriteString(nodeStyle.Render(line))
		b.WriteString("\n")
	}

	return renderFixedPanel(b.String(), width, height, borderColor)
}

// intentAdjustScroll ensures intentCursor is visible within the viewport.
func intentAdjustScroll(m *dashboardModel) {
	visibleLines := max(m.height/2-3, 1) // approximate bottom-right pane visible area
	if m.intentCursor < m.intentScrollOffset {
		m.intentScrollOffset = m.intentCursor
	}
	if m.intentCursor >= m.intentScrollOffset+visibleLines {
		m.intentScrollOffset = m.intentCursor - visibleLines + 1
	}
}
