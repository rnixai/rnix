// Package main — dashboard_trace.go
//
// Story 38-5 PR11 Step 4(c) (2026-05-04): renderTracePane main body migrated
// to internal/dashboard/trace/render.go (Render(state, ctx, innerW, innerH)
// string · 与 alertstrip / detail / security / intent 同模式)。本文件保留：
//   - fetchTraceListCmd / fetchTraceTreeCmd: IPC 调用（依赖 ipc.Dial · 属 cmd/rnix
//     端职责）；
//   - handleTraceKey: 依赖 dashboardModel.processes / handlePIDChange / statusMsg
//     等 cmd/rnix 端状态 · 暂不迁出（Step 4(b) PaneModel 接口扩展持 client 时再考虑）；
//   - renderTracePane wrapper: 注入 RenderContext + renderFixedPanel border 包裹；
//   - traceAdjustScroll / spanAdjustScroll / traceBottomInnerH 改为 thin wrapper
//     委托 trace.AdjustListScroll/AdjustSpanScroll/BottomInnerH；
//   - flattenSpanTree / spanStatusColor / renderWaterfallBar / waterfallBarWidth
//     改为 thin wrapper / alias 委托 trace.FlattenSpanTree / SpanStatusColor /
//     RenderWaterfallBar / WaterfallBarWidth（保留旧名让 ATDD 27-9 + 38-4
//     dashboard_cross_pane_test grep 契约零行为变化）。
//
// 行为契约保留（与 PR2-PR12 同模式 · 零行为变更）：
//   - Story 27-9 Trace pane (AC1-AC6)
//   - Story 38-4 AC#5 waterfall bar (20-char · degraded plan A · status colors · ASCII fallback)
package main

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/rnixai/rnix/internal/dashboard/trace"
	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"

	tea "charm.land/bubbletea/v2"
)

// =============================================================================
// Trace Pane (Story 27-9)
// =============================================================================

// fetchTraceListCmd — thin wrapper · Story 38-5 PR11 Step 4(b) Phase 3
//
// IPC fetch closure 已迁出至 internal/dashboard/trace.FetchListCmd。
//
//nolint:unused // 保留供潜在 caller / 测试 grep（current callers 已迁至 TraceModel.OnTick）
func fetchTraceListCmd() tea.Cmd {
	return trace.FetchListCmd(ipc.SocketPath())
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
	if m.trace.ViewMode == 0 {
		// List mode
		switch key {
		case "down", "j":
			if len(m.trace.Summaries) > 0 && m.trace.Cursor < len(m.trace.Summaries)-1 {
				m.trace.Cursor++
				traceAdjustScroll(&m)
			}
			return m, nil
		case "up", "k":
			if m.trace.Cursor > 0 {
				m.trace.Cursor--
				traceAdjustScroll(&m)
			}
			return m, nil
		case "enter":
			if len(m.trace.Summaries) > 0 && m.trace.Cursor < len(m.trace.Summaries) {
				traceID := m.trace.Summaries[m.trace.Cursor].TraceID
				m.trace.SelectedTraceID = traceID
				return m, fetchTraceTreeCmd(traceID)
			}
			return m, nil
		}
	} else {
		// Tree mode
		switch key {
		case "down", "j":
			if len(m.trace.SpanFlatNodes) > 0 && m.trace.SpanCursor < len(m.trace.SpanFlatNodes)-1 {
				m.trace.SpanCursor++
				spanAdjustScroll(&m)
			}
			return m, nil
		case "up", "k":
			if m.trace.SpanCursor > 0 {
				m.trace.SpanCursor--
				spanAdjustScroll(&m)
			}
			return m, nil
		case "enter":
			if len(m.trace.SpanFlatNodes) > 0 && m.trace.SpanCursor < len(m.trace.SpanFlatNodes) {
				node := m.trace.SpanFlatNodes[m.trace.SpanCursor]
				if node.PID > 0 {
					targetPID := node.PID
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
			m.trace.ViewMode = 0
			// Reset scroll and clamp cursor to current list bounds
			if m.trace.Cursor >= len(m.trace.Summaries) {
				m.trace.Cursor = max(0, len(m.trace.Summaries)-1)
			}
			m.trace.ScrollOffset = 0
			traceAdjustScroll(&m)
			return m, nil
		}
	}
	return m, nil
}

// flattenSpanTree is a thin wrapper around trace.FlattenSpanTree (Story 38-5
// PR11 Step 4(c)). Preserved for ATDD 27-9 AC-4.3/4.4/4.5/4.6 callsites +
// atdd_29_1 file splitting grep contract.
func flattenSpanTree(tree *ipc.SpanTreeWire) []spanFlatNode {
	return trace.FlattenSpanTree(tree)
}

// spanStatusColor is a thin wrapper around trace.SpanStatusColor (Story 38-5
// PR11 Step 4(c)). Preserved for ATDD 27-9 AC-4.7 callsite + dashboard_eval.go
// (which depends on this color routing for test results).
//
//nolint:unused // 保留供 ATDD grep 契约 / 潜在外部 caller
func spanStatusColor(status string) lipgloss.Color {
	return trace.SpanStatusColor(status)
}

// waterfallBarWidth is preserved as a const alias for the public
// trace.WaterfallBarWidth (Story 38-5 PR11 Step 4(c)). Preserved for
// dashboard_cross_pane_test.go (Story 38-4 AC#5 testing) callsites.
const waterfallBarWidth = trace.WaterfallBarWidth

// renderWaterfallBar is a thin wrapper around trace.RenderWaterfallBar
// (Story 38-5 PR11 Step 4(c)). Preserved for dashboard_cross_pane_test.go
// (Story 38-4 AC#5 testing) callsites.
func renderWaterfallBar(traceTotalMs, spanDurMs int64, status string, ascii bool) string {
	return trace.RenderWaterfallBar(traceTotalMs, spanDurMs, status, ascii)
}

// renderTraceTreeView is a thin wrapper kept for ATDD 38-4 cross-pane tests
// that exercise tree-mode rendering directly (Story 38-5 PR11 Step 4(c) ·
// preserved for dashboard_cross_pane_test.go callsites at lines 821/830/1024).
//
// width/height are the inner area (caller already accounts for border).
func (m dashboardModel) renderTraceTreeView(width, height int) string {
	// Force ViewMode==1 by mutating a local state copy so the test does not
	// have to set ViewMode manually before calling tree-view rendering.
	st := m.trace
	st.ViewMode = 1
	return trace.Render(st, trace.RenderContext{
		IsActive: m.activePane == paneTrace,
		ASCII:    ui.IsASCIIMode(),
	}, width, height)
}

// renderTracePane is a thin wrapper around trace.Render (Story 38-5 PR11 Step 4(c)).
//
// cmd/rnix wrapper responsibilities:
//  1. Compute isActive + borderColor (depends on m.activePane / paneTrace · cmd/rnix 端状态)
//  2. Compute innerW/innerH (subtract border)
//  3. Call trace.Render(state, ctx, innerW, innerH) for inner content
//  4. Wrap with renderFixedPanel(content, width, height, borderColor) outer border
//
// 与 alertstrip / detail / security / intent pattern 一致。
func (m dashboardModel) renderTracePane(width, height int) string {
	isActive := m.activePane == paneTrace

	borderColor := lipgloss.Color(ui.ColorMuted)
	if isActive {
		borderColor = lipgloss.Color(ui.ColorAgent)
	}

	innerW := max(width-2, 1)
	innerH := max(height-2, 1)

	content := trace.Render(m.trace, trace.RenderContext{
		IsActive: isActive,
		ASCII:    ui.IsASCIIMode(),
	}, innerW, innerH)

	return renderFixedPanel(content, width, height, borderColor)
}

// traceBottomInnerH is a thin wrapper around trace.BottomInnerH (Story 38-5
// PR11 Step 4(c)). Preserved for traceAdjustScroll / spanAdjustScroll callers.
//
//nolint:unused // 保留供 atdd grep 契约
func traceBottomInnerH(termHeight int) int {
	return trace.BottomInnerH(termHeight)
}

// traceAdjustScroll is a thin wrapper around trace.AdjustListScroll (Story
// 38-5 PR11 Step 4(c)). Preserved for handleTraceKey + ATDD 27-9 AC-3.7
// callsites.
func traceAdjustScroll(m *dashboardModel) {
	if m == nil {
		return
	}
	visibleLines := max(trace.BottomInnerH(m.height)-3, 1) // match renderTraceListView
	trace.AdjustListScroll(&m.trace, visibleLines)
}

// spanAdjustScroll is a thin wrapper around trace.AdjustSpanScroll (Story
// 38-5 PR11 Step 4(c)). Preserved for handleTraceKey + ATDD 27-9 AC-4.11
// callsites.
func spanAdjustScroll(m *dashboardModel) {
	if m == nil {
		return
	}
	visibleLines := max(trace.BottomInnerH(m.height)-2, 1) // match renderTraceTreeView
	trace.AdjustSpanScroll(&m.trace, visibleLines)
}
