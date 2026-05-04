// Package main — dashboard_intent.go
//
// Story 38-5 PR11 Step 4(c) (2026-05-04): renderIntentPane main body migrated
// to internal/dashboard/intent/render.go (Render(state, ctx, innerH) string ·
// 与 alertstrip / detail / security Step 4(c) 同模式)。本文件保留：
//   - fetchIntentTreesCmd: IPC 调用（依赖 ipc.Dial · 属 cmd/rnix 端职责）；
//   - renderIntentPane wrapper: 注入 RenderContext + renderFixedPanel border 包裹；
//   - intentStateColor / intentStateIcon / isIntentTreeTerminal / flattenIntentTrees /
//     flattenIntentTreesWithCollapse / pruneIntentCollapse / intentAdjustScroll
//     全部改为 thin wrapper 委托 internal/dashboard/intent 公开 API（保留旧名让
//     ATDD 27-7 / 36-5 / 29-1 / dashboard_cross_pane_test.go / dashboard.go /
//     dashboard_pane_dispatcher.go callsite 零行为变化）。
//
// 行为契约保留（与 PR2-PR12 同模式 · 零行为变更）：
//   - Story 27-7 IntentTree pane (AC1-AC6)
//   - Story 38-4 P1 stable RootIntent collapse (TreeCollapsed map)
//   - Story 38-4 P4 stale entry pruning (pruneIntentCollapse)
//   - Story 36-5 universal list navigation (intentAdjustScroll)
package main

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/rnixai/rnix/internal/dashboard/intent"
	"github.com/rnixai/rnix/internal/ui"
	"github.com/rnixai/rnix/ipc"

	tea "charm.land/bubbletea/v2"
)

// --- Intent Tree Pane (Story 27-7) ---

// intentStateColor is a thin wrapper around intent.StateColor (Story 38-5 PR11
// Step 4(c)). Preserved for ATDD 27-7 AC-3.3 callsite + atdd_29_1 file
// splitting grep contract.
func intentStateColor(state string) lipgloss.Color {
	return intent.StateColor(state)
}

// intentStateIcon is a thin wrapper around intent.StateIcon (Story 38-5 PR11
// Step 4(c)). Preserved for atdd_29_1 file splitting grep contract.
//
//nolint:unused // 保留供 ATDD grep 契约 / 潜在外部 caller
func intentStateIcon(state string) string {
	return intent.StateIcon(state)
}

// isIntentTreeTerminal is a thin wrapper around intent.IsTreeTerminal (Story
// 38-5 PR11 Step 4(c)). Preserved for dashboard_pane_dispatcher.go callsite
// (line 289 · header collapse toggle guard).
func isIntentTreeTerminal(state string) bool {
	return intent.IsTreeTerminal(state)
}

// flattenIntentTrees is a thin wrapper around intent.FlattenTrees (Story 38-5
// PR11 Step 4(c)). Preserved for ATDD 27-7 AC-3.1/3.2/3.5/6.3 callsites +
// atdd_29_1 file splitting grep contract.
func flattenIntentTrees(trees []*ipc.IntentTreeWire) []intentFlatNode {
	return intent.FlattenTrees(trees)
}

// flattenIntentTreesWithCollapse is a thin wrapper around
// intent.FlattenTreesWithCollapse (Story 38-5 PR11 Step 4(c)). Preserved for
// dashboard.go (line 352) + dashboard_pane_dispatcher.go (line 298) +
// dashboard_cross_pane_test.go (Story 38-4 P1/P4 testing) callsites.
func flattenIntentTreesWithCollapse(trees []*ipc.IntentTreeWire, userCollapsed map[string]bool) []intentFlatNode {
	return intent.FlattenTreesWithCollapse(trees, userCollapsed)
}

// pruneIntentCollapse is a thin wrapper around intent.PruneCollapse (Story
// 38-5 PR11 Step 4(c)). Preserved for dashboard_pane_dispatcher.go (line 297)
// + dashboard_cross_pane_test.go (line 496) callsites.
func pruneIntentCollapse(userCollapsed map[string]bool, trees []*ipc.IntentTreeWire) map[string]bool {
	return intent.PruneCollapse(userCollapsed, trees)
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

// renderIntentPane is a thin wrapper around intent.Render (Story 38-5 PR11 Step 4(c)).
//
// cmd/rnix wrapper responsibilities:
//  1. Compute isActive + borderColor (depends on m.activePane / paneIntent · cmd/rnix 端状态)
//  2. Call intent.Render(state, ctx, innerH) for inner content
//  3. Wrap with renderFixedPanel(content, width, height, borderColor) outer border
//
// 与 alertstrip / detail / security pattern 一致。
func (m dashboardModel) renderIntentPane(width, height int) string {
	isActive := m.activePane == paneIntent

	borderColor := lipgloss.Color(ui.ColorMuted)
	if isActive {
		borderColor = lipgloss.Color(ui.ColorAgent)
	}

	innerH := max(height-2, 1)

	content := intent.Render(m.intent, intent.RenderContext{
		IsActive: isActive,
	}, innerH)

	return renderFixedPanel(content, width, height, borderColor)
}

// intentAdjustScroll is a thin wrapper around intent.AdjustScroll (Story 38-5
// PR11 Step 4(c)). Preserved for dashboard_pane_dispatcher.go (line 274) +
// atdd_36_5 universal list nav comment grep contract.
//
// visibleLines computed identically to original (m.height/2-3 · approximate
// bottom-right pane visible area · zero behavior change).
func intentAdjustScroll(m *dashboardModel) {
	if m == nil {
		return
	}
	visibleLines := max(m.height/2-3, 1)
	intent.AdjustScroll(&m.intent, visibleLines)
}
