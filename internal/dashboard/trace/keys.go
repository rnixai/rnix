// Package trace — keys.go (Story 38-5 PR8 Step 2)
//
// Layer 2 KeyLayer 工厂函数 · 迁出自 cmd/rnix/dashboard_keylayers.go::registerLayer2Trace。
// 设计模式与 PR2-PR7 一致。
//
// Trace pane Docs（与 38-1 落地完全一致）：
//   - enter — Drill in to span tree
//   - c     — Collapse
//   - f     — Filter by status
//
// ActiveModes：view: overview | spans（依赖 TraceState.ViewMode 0=list, 1=tree）。
package trace

import "github.com/rnixai/rnix/internal/ui"

// StateProvider 让 KeyLayer 在不直接依赖 cmd/rnix.dashboardModel 的前提下读取最新 TraceState。
type StateProvider interface {
	TraceState() TraceState
}

// KeyLayer 构造 Trace pane 的 Layer 2 KeyLayer。
//
// 行为契约（与原 registerLayer2Trace 完全等价）：
//   - Name="Trace Pane" · Bindings 空 · Docs={enter, c, f} 3 项；
//   - ActiveModesFn 通过 ctx.(StateProvider) 读取 ViewMode；ViewMode==1 报告 spans，否则 overview；
//   - 当 ctx 不实现 StateProvider 时返回 nil（防御性）。
//
// nil 安全：fallback 可为 nil。
func KeyLayer(fallback ui.KeyHandler) *ui.KeyLayer {
	l := &ui.KeyLayer{
		Name:     "Trace Pane",
		Bindings: map[string]ui.KeyHandler{},
		Fallback: fallback,
		Docs:     map[string]ui.KeyDoc{},
		ActiveModesFn: func(ctx ui.KeyContext) []ui.Mode {
			provider, ok := ctx.(StateProvider)
			if !ok {
				return nil
			}
			view := "overview"
			if provider.TraceState().ViewMode == 1 {
				view = "spans"
			}
			return []ui.Mode{{Name: "view", Value: view}}
		},
	}
	l.Docs["enter"] = ui.KeyDoc{Key: "enter", Description: "Drill in to span tree"}
	l.Docs["c"] = ui.KeyDoc{Key: "c", Description: "Collapse"}
	l.Docs["f"] = ui.KeyDoc{Key: "f", Description: "Filter by status"}
	return l
}
