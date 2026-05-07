// Package tree — keys.go (Story 38-5 PR2 Step 2)
//
// Tree pane 的 Layer 2 KeyLayer 注册体。从 cmd/rnix/dashboard_keylayers.go::registerLayer2Tree
// 整体迁入，零行为变化。
//
// 包边界约束：
//   - 本文件位于 internal/dashboard/tree，**禁止反向依赖** cmd/rnix；
//   - 通过 StateProvider interface 让 cmd/rnix.dashboardModel 提供最新 TreeState
//     （dashboardModel.TreeState() 方法已在 dashboard.go:302 实现）；
//   - 实际键位处理逻辑仍由 cmd/rnix 端的 paneFallback 路由（dispatchPaneKey），
//     本包仅注册 Docs + ActiveModesFn 元数据（与 38-1 落地行为完全等价）；
//   - PR2 Step 3 之后部分 pane-specific 键位会迁入本包 Bindings，但 PR2 Step 2 仅是注册体迁移。
//
// 调用方式（cmd/rnix/dashboard_keylayers.go::newDispatcher）：
//
//	d.Layer2[ui.PaneID(paneTree)] = tree.KeyLayer(paneFallback)
package tree

import (
	"strings"

	"github.com/rnixai/rnix/internal/ui"
)

// SortLabels 是 Tree pane sort 模式的可读标签。
//
// 索引语义（与 TreeState.SortMode 一致）：
//
//	0 = "Time"  — 按最近 LastEventByPID 排序（默认）
//	1 = "PID"   — 按进程 PID 升降序
//	2 = "State" — 按 ProcessState（Running/Stopped/Zombie/Dead）
//
// 该变量同时被 cmd/rnix/dashboard_tree.go::treeSortLabels 引用（type alias 兼容）；
// ATDD `atdd_29_5_dashboard_history_view_test.go::TestRenderDashboardHistory_*` 通过 grep
// `treeSortLabels` 字符串验证 dashboard_tree.go 包含该 var，因此 cmd/rnix 端必须保留 var 名。
var SortLabels = []string{"Time", "PID", "State"}

// StateProvider 让任何持有 TreeState 的 model 都能被本包的 KeyLayer 消费。
//
// dashboardModel 在 cmd/rnix 端通过 TreeState() 方法实现该 interface。
// 设计目标：让 ActiveModesFn 在不直接依赖 dashboardModel 的前提下读取最新状态，
// 满足 spec § Dev Notes #1（PaneModel 不反向依赖 cmd/rnix）。
//
// ⚠️ 包边界硬约束：本 interface 是 KeyLayer ↔ App Model 之间的**唯一**契约。
// 任何子 Model 想读 TreeState 都应通过该接口（值拷贝），禁止读 dashboardModel 任何
// 私有字段。
type StateProvider interface {
	// TreeState 返回当前 TreeState 值快照。
	// 每次 ActiveModesFn 调用时，cmd/rnix 端会传入最新 dashboardModel
	// （Bubble Tea 值传递语义保证 m.tree 是最新值），通过此方法读出。
	TreeState() TreeState
}

// KeyLayer 返回 Tree pane 的 Layer 2 KeyLayer 注册体。
//
// 参数：
//   - fallback: pane-level 键位 fallback handler（cmd/rnix 端提供 paneFallback，
//     路由到现有 dispatchPaneKey）。本 PR2 Step 2 阶段，**所有** Tree pane 键位
//     行为由 fallback 处理（保留 38-1 落地行为）。
//
// 返回值：
//   - 一个填好 Docs + ActiveModesFn + Fallback 的 KeyLayer，可直接注册到
//     ui.Dispatcher.Layer2[paneTree]。
//
// Docs 注册的 5 个键（与 38-1 落地完全一致）：
//
//	K     — Kill process
//	s     — Cycle sort mode (Time/PID/State)
//	o     — Toggle sort direction
//	enter — Select / collapse dead subtree
//	/     — Search tree (in expanded view)
//
// ActiveModesFn 返回 3 类 Mode（与 38-1 落地完全一致）：
//
//	{Name: "sort",  Value: "time"|"pid"|"state"}     — 当前 sort mode（小写）
//	{Name: "dir",   Value: "asc"|"desc"}              — 当前 sort direction
//	{Name: "search", Value: "on"}                     — search mode 激活时（默认不显示）
//
// nil 安全：fallback 为 nil 时 KeyLayer 仍可注册，但 pane 键不会被处理（仅 Layer 0/1 生效）。
//
// 性能上界：ActiveModesFn 每次调用 O(1) 字符串拼接，无锁，可在 render 路径调用。
//
// 38-1 等价性：与 cmd/rnix/dashboard_keylayers.go::registerLayer2Tree（pre-Story 38-5）
// 函数体逐字段对照零差异；唯一的不同是 ctx 类型断言从 dashboardModel 改为 StateProvider
// （dashboardModel 通过 TreeState() 方法满足该 interface）。
func KeyLayer(fallback ui.KeyHandler) *ui.KeyLayer {
	l := &ui.KeyLayer{
		Name:     "Tree Pane",
		Bindings: map[string]ui.KeyHandler{},
		Fallback: fallback,
		Docs:     map[string]ui.KeyDoc{},
		ActiveModesFn: func(ctx ui.KeyContext) []ui.Mode {
			sp, ok := ctx.(StateProvider)
			if !ok {
				return nil
			}
			s := sp.TreeState()
			modes := []ui.Mode{}
			label := "time"
			if s.SortMode >= 0 && s.SortMode < len(SortLabels) {
				label = strings.ToLower(SortLabels[s.SortMode])
			}
			modes = append(modes, ui.Mode{Name: "sort", Value: label})
			dir := "desc"
			if s.SortAsc {
				dir = "asc"
			}
			modes = append(modes, ui.Mode{Name: "dir", Value: dir})
			if s.SearchMode || s.SearchQuery != "" {
				modes = append(modes, ui.Mode{Name: "search", Value: "on"})
			}
			return modes
		},
	}
	l.Docs["K"] = ui.KeyDoc{Key: "K", Description: "Kill process"}
	l.Docs["s"] = ui.KeyDoc{Key: "s", Description: "Cycle sort mode (Time/PID/State)"}
	l.Docs["o"] = ui.KeyDoc{Key: "o", Description: "Toggle sort direction"}
	l.Docs["enter"] = ui.KeyDoc{Key: "enter", Description: "Select / collapse dead subtree"}
	l.Docs["/"] = ui.KeyDoc{Key: "/", Description: "Search tree (in expanded view)"}
	return l
}
