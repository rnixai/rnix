// Package heatmap — keys.go (Story 38-5 PR3 Step 2)
//
// Heatmap pane 的 Layer 2 KeyLayer 注册体。从 cmd/rnix/dashboard_keylayers.go::registerLayer2Heatmap
// 整体迁入，零行为变化。
//
// 包边界约束（与 PR2 Step 2 tree/keys.go 模式一致）：
//   - 本文件位于 internal/dashboard/heatmap，**禁止反向依赖** cmd/rnix；
//   - 通过 StateProvider interface 让 cmd/rnix.dashboardModel 提供最新 HeatmapState
//     （dashboardModel.HeatmapState() 方法已在 dashboard.go 实现 · PR3 Step 1 落地）；
//   - 实际键位处理逻辑仍由 cmd/rnix 端 paneFallback 路由（dispatchPaneKey → handleHeatmapKey），
//     本包仅注册 Docs + ActiveModesFn 元数据；
//   - PR3 Step 3 之后部分 pane-specific 键位会迁入本包 Bindings，但 PR3 Step 2 仅是注册体迁移。
//
// 调用方式（cmd/rnix/dashboard_keylayers.go::newDispatcher）：
//
//	d.Layer2[ui.PaneID(paneHeatmap)] = heatmap.KeyLayer(paneFallback)
package heatmap

import (
	"github.com/rnixai/rnix/internal/ui"
)

// StateProvider 让任何持有 HeatmapState 的 model 都能被本包的 KeyLayer 消费。
//
// dashboardModel 在 cmd/rnix 端通过 HeatmapState() 方法实现该 interface（PR3 Step 1 落地）。
// 设计目标：让 ActiveModesFn 在不直接依赖 dashboardModel 的前提下读取最新状态，
// 满足 spec § Dev Notes #1（PaneModel 不反向依赖 cmd/rnix）。
//
// ⚠️ 包边界硬约束：本 interface 是 KeyLayer ↔ App Model 之间的**唯一**契约。
// 任何子 Model 想读 HeatmapState 都应通过该接口（值拷贝），禁止读 dashboardModel 任何私有字段。
type StateProvider interface {
	// HeatmapState 返回当前 HeatmapState 值快照。
	// 每次 ActiveModesFn 调用时，cmd/rnix 端会传入最新 dashboardModel
	// （Bubble Tea 值传递语义保证 m.heatmap 是最新值），通过此方法读出。
	HeatmapState() HeatmapState
}

// KeyLayer 返回 Heatmap pane 的 Layer 2 KeyLayer 注册体。
//
// 参数：
//   - fallback: pane-level 键位 fallback handler（cmd/rnix 端提供 paneFallback，
//     路由到现有 dispatchPaneKey）。本 PR3 Step 2 阶段，**所有** Heatmap pane 键位
//     行为由 fallback 处理（保留 38-1 + handleHeatmapKey 落地行为）。
//
// 返回值：
//   - 一个填好 Docs + ActiveModesFn + Fallback 的 KeyLayer，可直接注册到
//     ui.Dispatcher.Layer2[paneHeatmap]。
//
// Docs 注册的 4 个键（与 38-1 落地完全一致）：
//
//	= — Absolute scale
//	% — Relative scale
//	t — Toggle totals
//	f — Filter by segment kind
//
// ActiveModesFn 返回 1 类 Mode（与 38-1 落地完全一致）：
//
//	{Name: "view", Value: "expanded"|"summary"}  — 当前展开/概览模式
//
// nil 安全：fallback 为 nil 时 KeyLayer 仍可注册，但 pane 键不会被处理（仅 Layer 0/1 生效）。
//
// 性能上界：ActiveModesFn 每次调用 O(1)，无锁，可在 render 路径调用。
//
// 38-1 等价性：与 cmd/rnix/dashboard_keylayers.go::registerLayer2Heatmap（pre-Story 38-5）
// 函数体逐字段对照零差异；唯一不同是 ctx 类型断言从 dashboardModel 改为 StateProvider
// （dashboardModel 通过 HeatmapState() 方法满足该 interface）。
func KeyLayer(fallback ui.KeyHandler) *ui.KeyLayer {
	l := &ui.KeyLayer{
		Name:     "Heatmap Pane",
		Bindings: map[string]ui.KeyHandler{},
		Fallback: fallback,
		Docs:     map[string]ui.KeyDoc{},
		ActiveModesFn: func(ctx ui.KeyContext) []ui.Mode {
			sp, ok := ctx.(StateProvider)
			if !ok {
				return nil
			}
			s := sp.HeatmapState()
			modes := []ui.Mode{}
			if s.Expanded {
				modes = append(modes, ui.Mode{Name: "view", Value: "expanded"})
			} else {
				modes = append(modes, ui.Mode{Name: "view", Value: "summary"})
			}
			return modes
		},
	}
	l.Docs["="] = ui.KeyDoc{Key: "=", Description: "Absolute scale"}
	l.Docs["%"] = ui.KeyDoc{Key: "%", Description: "Relative scale"}
	l.Docs["t"] = ui.KeyDoc{Key: "t", Description: "Toggle totals"}
	l.Docs["f"] = ui.KeyDoc{Key: "f", Description: "Filter by segment kind"}
	return l
}
