// Package intent — keys.go (Story 38-5 PR6 Step 2)
//
// Layer 2 KeyLayer 工厂函数 · 迁出自 cmd/rnix/dashboard_keylayers.go::registerLayer2Intent。
// 设计模式与 PR2 tree.KeyLayer / PR3 heatmap.KeyLayer / PR4 timeline.KeyLayer / PR5 detail.KeyLayer 一致：
//
//   - 工厂函数 KeyLayer(fallback) 返回完整的 *ui.KeyLayer；
//   - 通过 StateProvider interface 让 dashboardModel 通过 IntentState() getter 满足
//     ActiveModesFn 的 cast，避免直接依赖 cmd/rnix.dashboardModel（包边界硬约束）；
//   - 1 个 Docs 键（enter · drill-in，与 38-4 落地行为对齐）。
//
// 注：本工厂只覆盖 Layer 2 注册器。`enter` 键的实际处理逻辑仍在 cmd/rnix/dashboard_pane_dispatcher.go
// 内（涉及 38-4 落地的 header toggle / 节点 PID drill-in 行为，依赖 cmd/rnix 端 setSelectedPID
// 和 collapse map 维护）。本 KeyLayer 仅注册 Docs 让 KeyHelp 显示正确说明，实际 handler 通过
// Fallback → dispatchPaneKey 路径分发。
package intent

import (
	"fmt"

	"github.com/rnixai/rnix/internal/ui"
)

// StateProvider 让 KeyLayer 在不直接依赖 cmd/rnix.dashboardModel 的前提下读取最新 IntentState。
//
// dashboardModel 通过 `IntentState() intent.IntentState` getter（PR6 Step 1 落地的 deprecated
// transitional getter）自然满足该接口。包边界硬约束的实践参见 spec § Risk 3 / spec § 04 风险 3。
type StateProvider interface {
	IntentState() IntentState
}

// KeyLayer 构造 Intent pane 的 Layer 2 KeyLayer。
//
// 参数：
//   - fallback：无 binding 命中时的回落 handler（cmd/rnix 端传 paneFallback · 完成跨 pane 通用键如 1-8 / Tab 等）。
//
// 返回：
//   - *ui.KeyLayer · Name="Intent Pane" · Bindings 空 · Docs={enter: "Drill in to process timeline"}
//     · ActiveModesFn 通过 ctx.(StateProvider) 读取最新 IntentState 报告 view 模式 + 节点总数。
//
// 行为契约（与 cmd/rnix/dashboard_keylayers.go::registerLayer2Intent 38-1 落地状态完全等价）：
//   - view: "tree" 总是显示（IntentTree 是当前唯一视图模式）；
//   - nodes: N 仅在 FlatNodes 非空时追加（避免在空状态下显示 nodes:0 噪声）；
//   - 当 ctx 不实现 StateProvider 时返回 nil（防御性）；
//   - 当 IntentState 未初始化（零值）时 FlatNodes len==0，nodes mode 不出现。
//
// nil 安全：fallback 可为 nil（dispatcher 自身处理 nil fallback 路径）。
func KeyLayer(fallback ui.KeyHandler) *ui.KeyLayer {
	l := &ui.KeyLayer{
		Name:     "Intent Pane",
		Bindings: map[string]ui.KeyHandler{},
		Fallback: fallback,
		Docs:     map[string]ui.KeyDoc{},
		ActiveModesFn: func(ctx ui.KeyContext) []ui.Mode {
			provider, ok := ctx.(StateProvider)
			if !ok {
				return nil
			}
			state := provider.IntentState()
			modes := []ui.Mode{{Name: "view", Value: "tree"}}
			if total := len(state.FlatNodes); total > 0 {
				modes = append(modes, ui.Mode{Name: "nodes", Value: fmt.Sprintf("%d", total)})
			}
			return modes
		},
	}
	// Story 38-1 M4: enter 触发 drill-in（节点行）/ header toggle（树头行 · 38-4 P1 落地）。
	// 实际 handler 在 cmd/rnix/dashboard_pane_dispatcher.go 内，本 Docs 仅供 KeyHelp 显示。
	l.Docs["enter"] = ui.KeyDoc{Key: "enter", Description: "Drill in to process timeline"}
	return l
}
