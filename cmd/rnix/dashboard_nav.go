// Package main — dashboard_nav.go (Story 38.1)
//
// 键位主入口。实际调度由 internal/ui/keylayer.go 的 3 层 Dispatcher 完成
// （Layer 0 Global / Layer 1 View / Layer 2 Pane）；Layer 2 Fallback 委托
// 给 dispatchPaneKey（dashboard_pane_dispatcher.go）。
// 见 dashboard_keylayers.go 各层 KeyLayer 注册。
// modal 守卫（confirmKill / helpOverlay / viewDebug 优先）在
// dashboard_modal_guards.go。
package main

import (
	tea "charm.land/bubbletea/v2"

	"github.com/rnixai/rnix/internal/ui"
)

// dashboardKey 是键位主入口。守卫顺序：
//
//	inspector / replay → modal guards → 3 层 Dispatcher
func (m dashboardModel) dashboardKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.viewMode == viewStepInspector {
		return m.inspectorKey(msg)
	}
	if m.replayMode {
		return m.handleReplayKey(msg.String())
	}
	if mm, cmd, handled := m.applyModalGuards(msg); handled {
		return mm, cmd
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
	return m, nil
}
