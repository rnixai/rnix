// Package inspector — keys.go (Story 38-5 PR10 Step 2)
//
// Layer 1 KeyLayer 工厂函数 · 迁出自 cmd/rnix/dashboard_keylayers.go::registerLayer1StepInspector。
// 设计模式与 PR2-PR9 一致（Layer 2 panes），但本 KeyLayer 注册到 Dispatcher.Layer1
// 而非 Layer2（Step Inspector 是 view-level overlay 而非 pane-level）。
//
// Step Inspector Docs（与 38-1 落地完全一致 · 11 项）：
//   - 1-5     — Switch lens
//   - h/l     — Prev / next step
//   - H/L     — First / last step
//   - j/k     — Scroll lens content
//   - /       — Search
//   - n/N     — Next / previous match
//   - d       — Diff mode (dd to pick base)
//   - F       — Follow live
//   - y       — Copy to clipboard
//   - o       — Open in pager
//   - esc     — Close inspector
//
// **行为契约**：注册体仅文档化键集；实际处理逻辑保留在 cmd/rnix/dashboard_inspector.go::
// inspectorKey（与 38-1 落地一致 · "We register a single catch-all that delegates
// so the chain is uniform"）。本 PR10 Step 2 不改 inspectorKey 主体，仅迁出 Docs
// 注册位置。
//
// **ActiveModesFn**：通过 StateProvider 读取 InspectorState 报告 diff/follow 子模式
// （36-6 落地）。当 ctx 不实现 StateProvider 时返回 nil（防御性 · 与 PR4-PR9 模式）。
package inspector

import "github.com/rnixai/rnix/internal/ui"

// StateProvider 让 KeyLayer 在不直接依赖 cmd/rnix.dashboardModel 的前提下读取最新 InspectorState。
//
// dashboardModel 通过 PR10 Step 1 落地的 deprecated getter `InspectorState() InspectorState`
// 自然满足该 interface（PR11 删除 getter 时需重新评估实现路径）。
type StateProvider interface {
	InspectorState() InspectorState
}

// KeyLayer 构造 Step Inspector 的 Layer 1 KeyLayer。
//
// 行为契约（与原 registerLayer1StepInspector 完全等价）：
//   - Name="Step Inspector"
//   - Bindings 空（由 inspectorKey catch-all 处理 · 38-1 落地）
//   - Docs={1-5, h/l, H/L, j/k, /, n/N, d, F, y, o, esc} 11 项
//   - Fallback 用于 catch-all（38-1 single-catch-all-delegates 模式）
//   - ActiveModesFn 通过 ctx.(StateProvider) 读取 DiffMode/FollowLive 报告 diff/follow 子模式；
//     无任何子模式激活时返回 nil（默认状态）。
//
// nil 安全：fallback 可为 nil。
func KeyLayer(fallback ui.KeyHandler) *ui.KeyLayer {
	l := &ui.KeyLayer{
		Name:     "Step Inspector",
		Bindings: map[string]ui.KeyHandler{},
		Fallback: fallback,
		Docs:     map[string]ui.KeyDoc{},
		ActiveModesFn: func(ctx ui.KeyContext) []ui.Mode {
			provider, ok := ctx.(StateProvider)
			if !ok {
				return nil
			}
			s := provider.InspectorState()
			var modes []ui.Mode
			if s.DiffMode {
				modes = append(modes, ui.Mode{Name: "diff", Value: "on"})
			}
			if s.FollowLive {
				modes = append(modes, ui.Mode{Name: "follow", Value: "live"})
			}
			return modes
		},
	}
	l.Docs["1-5"] = ui.KeyDoc{Key: "1-5", Description: "Switch lens"}
	l.Docs["h/l"] = ui.KeyDoc{Key: "h/l", Description: "Prev / next step"}
	l.Docs["H/L"] = ui.KeyDoc{Key: "H/L", Description: "First / last step"}
	l.Docs["j/k"] = ui.KeyDoc{Key: "j/k", Description: "Scroll lens content"}
	l.Docs["/"] = ui.KeyDoc{Key: "/", Description: "Search"}
	l.Docs["n/N"] = ui.KeyDoc{Key: "n/N", Description: "Next / previous match"}
	l.Docs["d"] = ui.KeyDoc{Key: "d", Description: "Diff mode (dd to pick base)"}
	l.Docs["F"] = ui.KeyDoc{Key: "F", Description: "Follow live"}
	l.Docs["y"] = ui.KeyDoc{Key: "y", Description: "Copy to clipboard"}
	l.Docs["o"] = ui.KeyDoc{Key: "o", Description: "Open in pager"}
	l.Docs["esc"] = ui.KeyDoc{Key: "esc", Description: "Close inspector"}
	return l
}
