package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"

	tea "charm.land/bubbletea/v2"
)

// =============================================================================
// Trace Pane (Story 27-9)
// =============================================================================

func fetchTraceListCmd() tea.Cmd {
	return func() tea.Msg {
		client, err := ipc.Dial(ipc.SocketPath())
		if err != nil {
			return traceListMsg{err: err}
		}
		defer client.Close()
		summaries, err := client.TraceList()
		return traceListMsg{summaries: summaries, err: err}
	}
}

func fetchTraceTreeCmd(traceID string) tea.Cmd {
	return func() tea.Msg {
		client, err := ipc.Dial(ipc.SocketPath())
		if err != nil {
			return traceTreeMsg{traceID: traceID, err: err}
		}
		defer client.Close()
		tree, err := client.TraceTree(traceID)
		return traceTreeMsg{traceID: traceID, tree: tree, err: err}
	}
}

func (m dashboardModel) handleTraceKey(key string) (tea.Model, tea.Cmd) {
	if m.traceViewMode == 0 {
		// List mode
		switch key {
		case "down", "j":
			if len(m.traceSummaries) > 0 && m.traceCursor < len(m.traceSummaries)-1 {
				m.traceCursor++
				traceAdjustScroll(&m)
			}
			return m, nil
		case "up", "k":
			if m.traceCursor > 0 {
				m.traceCursor--
				traceAdjustScroll(&m)
			}
			return m, nil
		case "enter":
			if len(m.traceSummaries) > 0 && m.traceCursor < len(m.traceSummaries) {
				traceID := m.traceSummaries[m.traceCursor].TraceID
				m.selectedTraceID = traceID
				return m, fetchTraceTreeCmd(traceID)
			}
			return m, nil
		}
	} else {
		// Tree mode
		switch key {
		case "down", "j":
			if len(m.spanFlatNodes) > 0 && m.spanCursor < len(m.spanFlatNodes)-1 {
				m.spanCursor++
				spanAdjustScroll(&m)
			}
			return m, nil
		case "up", "k":
			if m.spanCursor > 0 {
				m.spanCursor--
				spanAdjustScroll(&m)
			}
			return m, nil
		case "enter":
			if len(m.spanFlatNodes) > 0 && m.spanCursor < len(m.spanFlatNodes) {
				node := m.spanFlatNodes[m.spanCursor]
				if node.pid > 0 {
					targetPID := node.pid
					pidFound := false
					var targetUUID string
					for _, p := range m.processes {
						if p.PID == targetPID {
							pidFound = true
							targetUUID = p.UUID
							break
						}
					}
					if !pidFound {
						m.statusMsg = "该进程已不存在"
						m.statusMsgTTL = statusMsgDefaultTTL
						return m, nil
					}
					m.selectedPID = targetPID
					m.selectedUUID = targetUUID
					m.activePane = paneTimeline
					m2, cmd := m.handlePIDChange()
					return m2, cmd
				}
			}
			return m, nil
		case "esc", "escape":
			m.traceViewMode = 0
			// Reset scroll and clamp cursor to current list bounds
			if m.traceCursor >= len(m.traceSummaries) {
				m.traceCursor = max(0, len(m.traceSummaries)-1)
			}
			m.traceScrollOffset = 0
			traceAdjustScroll(&m)
			return m, nil
		}
	}
	return m, nil
}

func flattenSpanTree(tree *ipc.SpanTreeWire) []spanFlatNode {
	if tree == nil || tree.Root == nil {
		return nil
	}
	ascii := os.Getenv("RNIX_ASCII") == "1" || os.Getenv("RNIX_ASCII") == "true"
	var nodes []spanFlatNode
	flattenSpanNode(tree.Root, 0, true, "", ascii, &nodes)
	return nodes
}

func flattenSpanNode(node *ipc.SpanNodeWire, depth int, isLast bool, parentPrefix string, ascii bool, out *[]spanFlatNode) {
	var prefix string
	if depth == 0 {
		if ascii {
			prefix = "+-- "
		} else {
			prefix = "┌─ "
		}
	} else {
		if isLast {
			if ascii {
				prefix = parentPrefix + "`-- "
			} else {
				prefix = parentPrefix + "└─ "
			}
		} else {
			if ascii {
				prefix = parentPrefix + "|-- "
			} else {
				prefix = parentPrefix + "├─ "
			}
		}
	}
	*out = append(*out, spanFlatNode{
		spanID: node.SpanID,
		pid:    types.PID(node.PID),
		name:   node.Name,
		durMs:  node.DurationMs,
		tokens: node.TokensUsed,
		status: node.Status,
		depth:  depth,
		prefix: prefix,
		isRoot: depth == 0,
	})
	childPrefix := parentPrefix
	if depth > 0 {
		if isLast {
			childPrefix += "   "
		} else {
			if ascii {
				childPrefix += "|  "
			} else {
				childPrefix += "│  "
			}
		}
	}
	for i, child := range node.Children {
		flattenSpanNode(&child, depth+1, i == len(node.Children)-1, childPrefix, ascii, out)
	}
}

func spanStatusColor(status string) lipgloss.Color {
	switch status {
	case "ok":
		return lipgloss.Color("42")
	case "error":
		return lipgloss.Color("196")
	case "timeout":
		return lipgloss.Color("208")
	default:
		return lipgloss.Color("240")
	}
}

func (m dashboardModel) renderTracePane(width, height int) string {
	isActive := m.activePane == paneTrace

	borderColor := lipgloss.Color(ui.ColorMuted)
	if isActive {
		borderColor = lipgloss.Color(ui.ColorAgent)
	}

	innerW := max(width-2, 1)
	innerH := max(height-2, 1)

	if m.traceViewMode == 1 {
		return renderFixedPanel(m.renderTraceTreeView(innerW, innerH), width, height, borderColor)
	}
	return renderFixedPanel(m.renderTraceListView(innerW, innerH), width, height, borderColor)
}

func (m dashboardModel) renderTraceListView(width, height int) string {
	var b strings.Builder
	b.WriteString(" Traces\n")

	if m.traceErr != nil {
		fmt.Fprintf(&b, "\n    Error: %v\n", m.traceErr)
		return b.String()
	}

	if len(m.traceSummaries) == 0 {
		b.WriteString("\n    无活跃的 Compose 追踪数据。使用 rnix compose up 启动编排以生成追踪。\n")
		return b.String()
	}

	// Header
	fmt.Fprintf(&b, " %-16s  %-12s  %5s  %8s\n", "TRACE ID", "ROOT", "SPANS", "DUR")

	visibleLines := max(height-3, 1)
	startIdx := m.traceScrollOffset
	endIdx := min(startIdx+visibleLines, len(m.traceSummaries))

	for i := startIdx; i < endIdx; i++ {
		ts := m.traceSummaries[i]
		cursor := "  "
		if i == m.traceCursor {
			if os.Getenv("RNIX_ASCII") == "1" || os.Getenv("RNIX_ASCII") == "true" {
				cursor = "> "
			} else {
				cursor = "▸ "
			}
		}
		tid := ts.TraceID
		if len(tid) > 16 {
			tid = tid[:16]
		}
		dur := formatTimelineDuration(float64(ts.TotalDurationMs))
		fmt.Fprintf(&b, "%s%-16s  %-12s  %5d  %8s\n", cursor, tid, ts.RootSpanName, ts.SpanCount, dur)
	}

	return b.String()
}

func (m dashboardModel) renderTraceTreeView(width, height int) string {
	var b strings.Builder

	if m.selectedSpanTree == nil || len(m.spanFlatNodes) == 0 {
		b.WriteString(" Trace: (loading...)\n")
		return b.String()
	}

	meta := m.selectedSpanTree.Metadata
	tid := m.selectedTraceID
	if len(tid) > 16 {
		tid = tid[:16]
	}
	dur := formatTimelineDuration(float64(meta.TotalDurationMs))
	fmt.Fprintf(&b, " Trace: %s  %d spans  %s  %d tok\n", tid, meta.TotalSpans, dur, meta.TotalTokens)

	visibleLines := max(height-2, 1)
	startIdx := m.spanScrollOffset
	endIdx := min(startIdx+visibleLines, len(m.spanFlatNodes))

	for i := startIdx; i < endIdx; i++ {
		node := m.spanFlatNodes[i]
		cursor := "  "
		if i == m.spanCursor {
			if os.Getenv("RNIX_ASCII") == "1" || os.Getenv("RNIX_ASCII") == "true" {
				cursor = "> "
			} else {
				cursor = "▸ "
			}
		}

		dur := formatTimelineDuration(float64(node.durMs))
		tokStr := fmt.Sprintf("%dtok", node.tokens)
		statusStyle := lipgloss.NewStyle().Foreground(spanStatusColor(node.status))

		line := fmt.Sprintf("%s%s%s (PID %d)  %s  %s  %s",
			cursor, node.prefix, node.name, node.pid, dur, tokStr, statusStyle.Render(node.status))
		b.WriteString(line)
		b.WriteString("\n")
	}

	return b.String()
}

// traceBottomInnerH computes the inner height of the bottom-right pane from terminal height.
func traceBottomInnerH(termHeight int) int {
	contentHeight := max(termHeight-4, 3)
	bottomRightH := contentHeight - contentHeight/2
	return max(bottomRightH-2, 1) // subtract border
}

// traceAdjustScroll ensures traceCursor is visible within the viewport.
func traceAdjustScroll(m *dashboardModel) {
	visibleLines := max(traceBottomInnerH(m.height)-3, 1) // match renderTraceListView
	if m.traceCursor < m.traceScrollOffset {
		m.traceScrollOffset = m.traceCursor
	}
	if m.traceCursor >= m.traceScrollOffset+visibleLines {
		m.traceScrollOffset = m.traceCursor - visibleLines + 1
	}
}

// spanAdjustScroll ensures spanCursor is visible within the viewport.
func spanAdjustScroll(m *dashboardModel) {
	visibleLines := max(traceBottomInnerH(m.height)-2, 1) // match renderTraceTreeView
	if m.spanCursor < m.spanScrollOffset {
		m.spanScrollOffset = m.spanCursor
	}
	if m.spanCursor >= m.spanScrollOffset+visibleLines {
		m.spanScrollOffset = m.spanCursor - visibleLines + 1
	}
}
