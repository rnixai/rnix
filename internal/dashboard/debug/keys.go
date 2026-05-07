// Package debug — keys.go (Story 38-5 PR11 Step 2)
//
// Layer 1 KeyLayer 工厂函数 · 迁出自 cmd/rnix/dashboard_keylayers.go::registerLayer1Debug。
// 设计模式与 PR2-PR10 一致；Debug 是 Layer 1 view-level overlay（与 Step Inspector 同级 ·
// 与 Layer 2 panes 不同 · 注册到 Dispatcher.Layer1）。
//
// Debug View Docs（与 38-1 落地完全一致 · 5 项）：
//   - s   — Toggle strace
//   - f   — Filter events
//   - v   — Expand detail
//   - j/k — Navigate events
//   - d   — Exit debug mode
//
// **行为契约**：注册体仅文档化键集；实际处理逻辑保留在 cmd/rnix/dashboard_debug.go::
// handleDebugKey（与 38-1 落地一致 · "Documentation only; actual handlers remain in
// handleDebugKey for now"）。本 PR Step 2 不改 handleDebugKey 主体，仅迁出 Docs 注册位置。
//
// **ActiveModesFn**：通过 StateProvider 读取 DebugState 报告 strace 子模式
// （Story 34.6 落地）。当 ctx 不实现 StateProvider 时返回 nil（防御性 · 与 PR4-PR10 模式）。
package debug

import "github.com/rnixai/rnix/internal/ui"

// StateProvider 让 KeyLayer 在不直接依赖 cmd/rnix.dashboardModel 的前提下读取最新 DebugState。
//
// dashboardModel 通过 PR11 Step 1 落地的 deprecated getter（PR11 Step 3 落地）自然满足该
// interface。
type StateProvider interface {
	DebugState() DebugState
}

// KeyLayer 构造 Debug View 的 Layer 1 KeyLayer。
//
// 行为契约（与原 registerLayer1Debug 完全等价 + 新增 ActiveModes）：
//   - Name="Debug View"
//   - Bindings 空（由 handleDebugKey catch-all 处理 · 38-1 落地）
//   - Docs={s, f, v, j/k, d} 5 项
//   - Fallback 用于 catch-all
//   - ActiveModesFn 通过 ctx.(StateProvider) 读取 ShowStrace + AutoScroll 报告子模式
//     （strace:on/off + scroll:auto/manual）；ctx 非 StateProvider 时返回 nil。
//
// nil 安全：fallback 可为 nil。
func KeyLayer(fallback ui.KeyHandler) *ui.KeyLayer {
	l := &ui.KeyLayer{
		Name:     "Debug View",
		Bindings: map[string]ui.KeyHandler{},
		Fallback: fallback,
		Docs:     map[string]ui.KeyDoc{},
		ActiveModesFn: func(ctx ui.KeyContext) []ui.Mode {
			provider, ok := ctx.(StateProvider)
			if !ok {
				return nil
			}
			s := provider.DebugState()
			var modes []ui.Mode
			straceVal := "off"
			if s.ShowStrace {
				straceVal = "on"
			}
			modes = append(modes, ui.Mode{Name: "strace", Value: straceVal})
			scrollVal := "manual"
			if s.AutoScroll {
				scrollVal = "auto"
			}
			modes = append(modes, ui.Mode{Name: "scroll", Value: scrollVal})
			return modes
		},
	}
	l.Docs["s"] = ui.KeyDoc{Key: "s", Description: "Toggle strace"}
	l.Docs["f"] = ui.KeyDoc{Key: "f", Description: "Filter events"}
	l.Docs["v"] = ui.KeyDoc{Key: "v", Description: "Expand detail"}
	l.Docs["j/k"] = ui.KeyDoc{Key: "j/k", Description: "Navigate events"}
	l.Docs["d"] = ui.KeyDoc{Key: "d", Description: "Exit debug mode"}
	return l
}
