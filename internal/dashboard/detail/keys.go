// Package detail — keys.go (Story 38-5 PR5 Step 2)
//
// Detail pane 的 Layer 2 KeyLayer 注册体。从 cmd/rnix/dashboard_keylayers.go::registerLayer2Detail
// 整体迁入，零行为变化。
//
// 包边界约束（与 PR3 Step 2 heatmap/keys.go 模式一致）：
//   - 本文件位于 internal/dashboard/detail，**禁止反向依赖** cmd/rnix；
//   - 通过 StateProvider interface 让 cmd/rnix.dashboardModel 提供最新 DetailState
//     （dashboardModel.DetailState() 方法已在 dashboard.go 实现 · PR5 Step 1 落地）；
//   - 实际键位处理逻辑仍由 cmd/rnix 端 paneFallback 路由（dispatchPaneKey），
//     本包仅注册 Docs + ActiveModesFn 元数据；
//   - PR5 Step 3 之后 pane-specific 键位（若新增）会迁入本包 Bindings，但 PR5 Step 2
//     仅是注册体迁移（38-1 落地的 Detail pane 在原 registerLayer2Detail 中本就只注册了
//     ActiveModesFn 没有 Bindings · v/y 占位键已删除）。
//
// 调用方式（cmd/rnix/dashboard_keylayers.go::newDispatcher）：
//
//	d.Layer2[ui.PaneID(paneDetail)] = detail.KeyLayer(paneFallback)
package detail

import (
	"fmt"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/ui"
)

// SelectedPIDProvider 让 KeyLayer 在不直接依赖 dashboardModel 的前提下读取
// 当前 selectedPID（dashboardModel 的字段，不在 DetailState 中）。
//
// 设计目标：与 StateProvider 解耦——StateProvider 提供 DetailState，
// SelectedPIDProvider 提供 dashboardModel.selectedPID。Detail pane 的 ActiveModesFn
// 当前实现需要 selectedPID 来生成 `pid: %d` 模式标签（37-1 落地的视觉规则）。
//
// dashboardModel 在 cmd/rnix 端通过现有 selectedPID 字段 + 新增 Getter 满足该
// interface（细节由 cmd/rnix 端 wire-up 决定，本包仅声明契约）。
//
// ⚠️ 包边界硬约束：本 interface 是 KeyLayer ↔ App Model 之间的**唯一**契约。
// 任何子 Model 想读 selectedPID 都应通过该接口（值拷贝），禁止读 dashboardModel 任何
// 私有字段。
type SelectedPIDProvider interface {
	// SelectedPID 返回当前选中的进程 PID（0 表示未选中）。
	SelectedPID() types.PID
}

// StateProvider 让任何持有 DetailState 的 model 都能被本包的 KeyLayer 消费。
//
// 当前 detail.KeyLayer 的 ActiveModesFn 实现并不直接读 DetailState，而是
// 读 selectedPID（见 SelectedPIDProvider）。本 interface 保留供 PR5 Step 3
// + PR11 阶段扩展用（例如 Detail 视图模式 full/compact 切换 · 见 dashboard_keylayers.go:691
// 的 v/y 占位说明 · 当前未实现）。
//
// dashboardModel 在 cmd/rnix 端通过 DetailState() 方法实现该 interface（PR5 Step 1 落地）。
//
//nolint:unused // 保留供 PR5 Step 3 / PR11 扩展使用
type StateProvider interface {
	DetailState() DetailState
}

// KeyLayer 返回 Detail pane 的 Layer 2 KeyLayer 注册体。
//
// 参数：
//   - fallback: pane-level 键位 fallback handler（cmd/rnix 端提供 paneFallback，
//     路由到现有 dispatchPaneKey）。本 PR5 Step 2 阶段，**所有** Detail pane 键位
//     行为由 fallback 处理（保留 38-1 落地行为）。
//
// 返回值：
//   - 一个填好 Docs（空）+ ActiveModesFn + Fallback 的 KeyLayer，可直接注册到
//     ui.Dispatcher.Layer2[paneDetail]。
//
// Docs 注册：**空** Bindings + 空 Docs（与 38-1 落地完全一致）。
//   - 原 dashboard_keylayers.go:691 注释明确：detail pane 不接受 v/y 键
//     （dispatchPaneKey 末尾 switch 没有 paneDetail case），原 doc 中的
//     v=Toggle full/compact 与 y=Copy 是占位，**未实现**，已删除以避免误导用户。
//
// ActiveModesFn 返回 1 类 Mode（与 38-1 落地完全一致）：
//
//	{Name: "pid", Value: "<pid>"}        — selectedPID > 0 时显示 PID
//	{Name: "view", Value: "no selection"} — selectedPID == 0 时占位
//
// nil 安全：
//   - fallback 为 nil 时 KeyLayer 仍可注册（pane 键不被处理 · 仅 Layer 0/1 生效）；
//   - ctx 不实现 SelectedPIDProvider 时返回 nil（无 modes 显示）。
//
// 性能上界：ActiveModesFn 每次调用 O(1)，无锁，可在 render 路径调用。
//
// 38-1 等价性：与 cmd/rnix/dashboard_keylayers.go::registerLayer2Detail（pre-Story 38-5）
// 函数体逐字段对照零差异；唯一不同是 ctx 类型断言从 dashboardModel 改为
// SelectedPIDProvider（dashboardModel 通过 SelectedPID() 方法满足该 interface）。
func KeyLayer(fallback ui.KeyHandler) *ui.KeyLayer {
	return &ui.KeyLayer{
		Name:     "Detail Pane",
		Bindings: map[string]ui.KeyHandler{},
		Fallback: fallback,
		Docs:     map[string]ui.KeyDoc{},
		ActiveModesFn: func(ctx ui.KeyContext) []ui.Mode {
			sp, ok := ctx.(SelectedPIDProvider)
			if !ok {
				return nil
			}
			pid := sp.SelectedPID()
			if pid > 0 {
				return []ui.Mode{{Name: "pid", Value: fmt.Sprintf("%d", pid)}}
			}
			return []ui.Mode{{Name: "view", Value: "no selection"}}
		},
	}
}
