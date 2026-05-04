// Package eval — keys.go (Story 38-5 PR9 Step 2)
//
// Layer 2 KeyLayer 工厂函数 · 迁出自 cmd/rnix/dashboard_keylayers.go::registerLayer2Eval。
// 设计模式与 PR2-PR8 一致。
//
// Eval pane Docs（与 38-1 落地完全一致）：
//   - 1/2/3 — Switch sub-view
//   - o     — Sort by score
//
// ActiveModes：view: reputation | topology | synergy（依赖 EvalState.SubView）。
package eval

import "github.com/rnixai/rnix/internal/ui"

// StateProvider 让 KeyLayer 在不直接依赖 cmd/rnix.dashboardModel 的前提下读取最新 EvalState。
type StateProvider interface {
	EvalState() EvalState
}

// KeyLayer 构造 Eval pane 的 Layer 2 KeyLayer。
//
// 行为契约（与原 registerLayer2Eval 完全等价）：
//   - Name="Eval Pane" · Bindings 空 · Docs={1/2/3, o} 2 项；
//   - ActiveModesFn 通过 ctx.(StateProvider) 读取 SubView 报告 reputation/topology/synergy；
//   - SubView 越界（< 0 或 > 2）fallback 为 reputation；
//   - 当 ctx 不实现 StateProvider 时返回 nil（防御性）。
//
// nil 安全：fallback 可为 nil。
func KeyLayer(fallback ui.KeyHandler) *ui.KeyLayer {
	l := &ui.KeyLayer{
		Name:     "Eval Pane",
		Bindings: map[string]ui.KeyHandler{},
		Fallback: fallback,
		Docs:     map[string]ui.KeyDoc{},
		ActiveModesFn: func(ctx ui.KeyContext) []ui.Mode {
			provider, ok := ctx.(StateProvider)
			if !ok {
				return nil
			}
			view := "reputation"
			switch provider.EvalState().SubView {
			case 1:
				view = "topology"
			case 2:
				view = "synergy"
			}
			return []ui.Mode{{Name: "view", Value: view}}
		},
	}
	l.Docs["1/2/3"] = ui.KeyDoc{Key: "1/2/3", Description: "Switch sub-view"}
	l.Docs["o"] = ui.KeyDoc{Key: "o", Description: "Sort by score"}
	return l
}
