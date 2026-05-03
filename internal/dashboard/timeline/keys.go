// Package timeline — keys.go (Story 38-5 PR4 Step 2)
//
// Timeline pane 的 Layer 2 KeyLayer 注册体。从 cmd/rnix/dashboard_keylayers.go::registerLayer2Timeline
// 整体迁入，零行为变化。
//
// 包边界约束（与 PR2 Step 2 tree/keys.go + PR3 Step 2 heatmap/keys.go 模式一致）：
//   - 本文件位于 internal/dashboard/timeline，**禁止反向依赖** cmd/rnix；
//   - 通过 StateProvider interface 让 cmd/rnix.dashboardModel 提供最新 TimelineState
//     （dashboardModel.TimelineState() 方法已在 dashboard.go 实现 · PR4 Step 1 落地）；
//   - 实际键位处理逻辑仍由 cmd/rnix 端 paneFallback 路由（dispatchPaneKey → handleTimelineKey），
//     本包仅注册 Docs + ActiveModesFn 元数据；
//   - PR4 Step 3 之后部分 pane-specific 键位会迁入本包 Bindings，但 PR4 Step 2 仅是注册体迁移。
//
// 调用方式（cmd/rnix/dashboard_keylayers.go::newDispatcher）：
//
//	d.Layer2[ui.PaneID(paneTimeline)] = timeline.KeyLayer(paneFallback)
package timeline

import (
	"github.com/rnixai/rnix/internal/ui"
)

// StateProvider 让任何持有 TimelineState 的 model 都能被本包的 KeyLayer 消费。
//
// dashboardModel 在 cmd/rnix 端通过 TimelineState() 方法实现该 interface（PR4 Step 1 落地）。
// 设计目标：让 ActiveModesFn 在不直接依赖 dashboardModel 的前提下读取最新状态，
// 满足 spec § Dev Notes #1（PaneModel 不反向依赖 cmd/rnix）。
//
// ⚠️ 包边界硬约束：本 interface 是 KeyLayer ↔ App Model 之间的**唯一**契约。
// 任何子 Model 想读 TimelineState 都应通过该接口（值拷贝），禁止读 dashboardModel 任何私有字段。
type StateProvider interface {
	// TimelineState 返回当前 TimelineState 值快照。
	// 每次 ActiveModesFn 调用时，cmd/rnix 端会传入最新 dashboardModel
	// （Bubble Tea 值传递语义保证 m.timeline 是最新值），通过此方法读出。
	TimelineState() TimelineState
}

// ExpandLabels 是 TimelineExpandMode 的标签字符串，用于 ActiveModesFn 的「expand」mode 取值。
//
// 顺序与 TimelineExpandMode 取值（Collapsed=0, Expanded=1, ErrorsOnly=2）严格对齐。
// cmd/rnix 端可直接以 ExpandLabels[mode] 形式取标签（越界回退由 KeyLayer 内部处理）。
var ExpandLabels = []string{"collapsed", "all", "errors"}

// KeyLayer 返回 Timeline pane 的 Layer 2 KeyLayer 注册体。
//
// 参数：
//   - fallback: pane-level 键位 fallback handler（cmd/rnix 端提供 paneFallback，
//     路由到现有 dispatchPaneKey）。本 PR4 Step 2 阶段，**所有** Timeline pane 键位
//     行为由 fallback 处理（保留 38-1 + handleTimelineKey 落地行为）。
//
// 返回值：
//   - 一个填好 Docs + ActiveModesFn + Fallback 的 KeyLayer，可直接注册到
//     ui.Dispatcher.Layer2[paneTimeline]。
//
// Docs 注册的 10 个键（与 38-1 落地完全一致）：
//
//	f     — Filter mode (event types)
//	F     — Toggle follow live
//	v     — Expand step detail (L2)
//	V     — Debug detail (L3)
//	e     — Expand mode: all
//	E     — Expand mode: errors only
//	C     — Expand mode: collapsed
//	o     — Toggle sort direction
//	P     — Step Inspector (System lens)
//	enter — Expand / collapse
//
// ActiveModesFn 返回最多 2 类 Mode（与 38-1 落地完全一致）：
//
//	{Name: "filter", Value: "on"}                                 — 仅当 StepFilterMode==true 时
//	{Name: "expand", Value: "collapsed"|"all"|"errors"}           — 始终（按 ExpandMode 映射）
//
// nil 安全：fallback 为 nil 时 KeyLayer 仍可注册，但 pane 键不会被处理（仅 Layer 0/1 生效）。
//
// 性能上界：ActiveModesFn 每次调用 O(1)，无锁，可在 render 路径调用。
//
// 38-1 等价性：与 cmd/rnix/dashboard_keylayers.go::registerLayer2Timeline（pre-Story 38-5 PR4）
// 函数体逐字段对照零差异；唯一不同是 ctx 类型断言从 dashboardModel 改为 StateProvider
// （dashboardModel 通过 TimelineState() 方法满足该 interface）。
func KeyLayer(fallback ui.KeyHandler) *ui.KeyLayer {
	l := &ui.KeyLayer{
		Name:     "Timeline Pane",
		Bindings: map[string]ui.KeyHandler{},
		Fallback: fallback,
		Docs:     map[string]ui.KeyDoc{},
		ActiveModesFn: func(ctx ui.KeyContext) []ui.Mode {
			sp, ok := ctx.(StateProvider)
			if !ok {
				return nil
			}
			s := sp.TimelineState()
			modes := []ui.Mode{}
			if s.StepFilterMode {
				modes = append(modes, ui.Mode{Name: "filter", Value: "on"})
			}
			expandLabel := expandLabel(s.ExpandMode)
			modes = append(modes, ui.Mode{Name: "expand", Value: expandLabel})
			return modes
		},
	}
	l.Docs["f"] = ui.KeyDoc{Key: "f", Description: "Filter mode (event types)"}
	l.Docs["F"] = ui.KeyDoc{Key: "F", Description: "Toggle follow live"}
	l.Docs["v"] = ui.KeyDoc{Key: "v", Description: "Expand step detail (L2)"}
	l.Docs["V"] = ui.KeyDoc{Key: "V", Description: "Debug detail (L3)"}
	l.Docs["e"] = ui.KeyDoc{Key: "e", Description: "Expand mode: all"}
	l.Docs["E"] = ui.KeyDoc{Key: "E", Description: "Expand mode: errors only"}
	l.Docs["C"] = ui.KeyDoc{Key: "C", Description: "Expand mode: collapsed"}
	l.Docs["o"] = ui.KeyDoc{Key: "o", Description: "Toggle sort direction"}
	l.Docs["P"] = ui.KeyDoc{Key: "P", Description: "Step Inspector (System lens)"}
	l.Docs["enter"] = ui.KeyDoc{Key: "enter", Description: "Expand / collapse"}
	return l
}

// expandLabel 把 TimelineExpandMode 映射为 ActiveModesFn 中「expand」mode 的字符串值。
//
// 越界保护：未知值回退为 "collapsed"（与 cmd/rnix 端 pre-PR4 默认值语义一致）。
func expandLabel(mode TimelineExpandMode) string {
	switch mode {
	case ExpandModeExpanded:
		return "all"
	case ExpandModeErrorsOnly:
		return "errors"
	case ExpandModeCollapsed:
		return "collapsed"
	default:
		return "collapsed"
	}
}
