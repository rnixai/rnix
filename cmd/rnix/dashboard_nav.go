// Package main — dashboard_nav.go (Story 38.1)
//
// 键位主入口。实际调度由 internal/ui/keylayer.go 的 3 层 Dispatcher 完成
// （Layer 0 Global / Layer 1 View / Layer 2 Pane）；Layer 2 Fallback 委托
// 给 dispatchPaneKey（dashboard_pane_dispatcher.go）。
// 见 dashboard_keylayers.go 各层 KeyLayer 注册。
package main

import (
	tea "charm.land/bubbletea/v2"

	"github.com/rnixai/rnix/internal/ui"
)

// dashboardKey 是键位主入口。Inspector / Replay 走自有内部 dispatcher；
// 其余键交 3 层 Dispatcher，未消费则交 handleDebugKey 兜底（仅 viewDebug）。
func (m dashboardModel) dashboardKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.viewMode == viewStepInspector {
		return m.inspectorKey(msg)
	}
	if m.replayMode {
		return m.handleReplayKey(msg.String())
	}
	if m.dispatcher != nil {
		newCtx, cmd, consumed := m.dispatcher.Handle(msg, ui.ViewID(m.viewMode), ui.PaneID(m.activePane), m)
		if mm, ok := newCtx.(dashboardModel); ok {
			m = mm
		}
		if consumed {
			return m, cmd
		}
	}
	if m.viewMode == viewDebug {
		if m2, cmd, handled := m.handleDebugKey(msg.String()); handled {
			return m2, cmd
		}
	}
	return m, nil
}
