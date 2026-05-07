// Package security — keys.go (Story 38-5 PR7 Step 2)
//
// Layer 2 KeyLayer 工厂函数 · 迁出自 cmd/rnix/dashboard_keylayers.go::registerLayer2Security。
// 设计模式与 PR2 tree.KeyLayer / PR3 heatmap.KeyLayer / PR4 timeline.KeyLayer / PR5 detail.KeyLayer /
// PR6 intent.KeyLayer 一致：
//
//   - 工厂函数 KeyLayer(fallback) 返回完整的 *ui.KeyLayer；
//   - 通过 StateProvider interface 让 dashboardModel 通过 SecurityState() getter 满足
//     ActiveModesFn 的 cast，避免直接依赖 cmd/rnix.dashboardModel（包边界硬约束）；
//   - 1 个 Docs 键（enter · drill-in，与 38-4 Alert Immune 路由 + 现有 dispatcher 行为对齐）。
//
// 38-4 Alert Immune 路由保留：当 alert 类型为 EventImmune 时，dashboard_keylayers.go::Layer 0
// 的 enter handler 会路由到 paneSecurity（而非 paneTimeline），本 KeyLayer 仅注册 Docs，
// 实际跳转逻辑保持在 cmd/rnix 端不变。
package security

import (
	"fmt"

	"github.com/rnixai/rnix/internal/ui"
)

// StateProvider 让 KeyLayer 在不直接依赖 cmd/rnix.dashboardModel 的前提下读取最新 SecurityState。
//
// dashboardModel 通过 `SecurityState() security.SecurityState` getter（PR7 Step 1 落地的
// deprecated transitional getter）自然满足该接口。
type StateProvider interface {
	SecurityState() SecurityState
}

// KeyLayer 构造 Security pane 的 Layer 2 KeyLayer。
//
// 参数：
//   - fallback：无 binding 命中时的回落 handler（cmd/rnix 端传 paneFallback）。
//
// 返回：
//   - *ui.KeyLayer · Name="Security Pane" · Bindings 空 · Docs={enter: "Drill in to process timeline"}
//     · ActiveModesFn 通过 ctx.(StateProvider) 读取最新 SecurityState 报告 view 模式 + alert 总数。
//
// 行为契约（与 cmd/rnix/dashboard_keylayers.go::registerLayer2Security 38-1 落地状态完全等价）：
//   - view: "list" 总是显示（Security pane 当前唯一视图模式）；
//   - alerts: N 仅在 Alerts 非空时追加（避免在空状态下显示 alerts:0 噪声）；
//   - 当 ctx 不实现 StateProvider 时返回 nil（防御性）；
//   - 当 SecurityState 未初始化（零值）时 Alerts len==0，alerts mode 不出现。
//
// nil 安全：fallback 可为 nil（dispatcher 自身处理 nil fallback 路径）。
func KeyLayer(fallback ui.KeyHandler) *ui.KeyLayer {
	l := &ui.KeyLayer{
		Name:     "Security Pane",
		Bindings: map[string]ui.KeyHandler{},
		Fallback: fallback,
		Docs:     map[string]ui.KeyDoc{},
		ActiveModesFn: func(ctx ui.KeyContext) []ui.Mode {
			provider, ok := ctx.(StateProvider)
			if !ok {
				return nil
			}
			state := provider.SecurityState()
			modes := []ui.Mode{{Name: "view", Value: "list"}}
			if alerts := len(state.Alerts); alerts > 0 {
				modes = append(modes, ui.Mode{Name: "alerts", Value: fmt.Sprintf("%d", alerts)})
			}
			return modes
		},
	}
	// Story 38-1 M4: enter 触发 drill-in（alert 行）/ Layer 0 alertExpanded 路由优先级保留。
	// 实际 handler 在 cmd/rnix/dashboard_pane_dispatcher.go 内，本 Docs 仅供 KeyHelp 显示。
	l.Docs["enter"] = ui.KeyDoc{Key: "enter", Description: "Drill in to process timeline"}
	return l
}
