// Package plugin — help.go (Story 38-5 PR1)
//
// HelpPlugin 自动从 Dispatcher.HelpGroupedFor() 派生 help overlay 内容，避免在多个
// pane 文件里重复维护"按键说明字符串"。曾在 dashboardModel 占 1 字段（helpOverlay bool）。
//
// PR1 阶段仅落地骨架；PR11 在 App Model 瘦身时把 dashboard_help.go 中 buildHelpOverlay
// 的逻辑收纳进来。
package plugin

import (
	"github.com/rnixai/rnix/internal/ui"
)

// HelpPlugin 持有 help overlay 渲染所需的状态（PR11 完善）。
type HelpPlugin struct {
	Visible bool
}

// Render 输入 Dispatcher 与当前 view/pane，返回分组好的 KeyDoc 列表，由 caller 渲染。
//
// PR1 仅作 dispatcher.HelpGroupedFor 的薄包装；PR11 在此处加上 lipgloss 渲染。
func (p *HelpPlugin) Render(d *ui.Dispatcher, view ui.ViewID, pane ui.PaneID) []ui.HelpGroup {
	if p == nil || d == nil {
		return nil
	}
	return d.HelpGroupedFor(view, pane)
}
