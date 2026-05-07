// Package main — dashboard_modal_guards.go (Story 38.1 Code Review C1/C2/C3 fix)
//
// 提取 confirmKill / helpOverlay / viewDebug 三个 modal-like 上下文的入口
// 守卫，确保它们在 3 层 Dispatcher 之前执行。
//
// 守卫顺序（applyModalGuards 内部）：
//  1. confirmKill 守卫（C2）：仅 y/n/esc 通过；其他键 cancel modal
//  2. helpOverlay 守卫（C3）：仅 ?/esc/q 通过；其他键忽略
//  3. viewDebug 优先（C1）：handleDebugKey 优先，未处理才走 dispatcher
//
// 设计说明：modal 守卫 必须 在 dispatcher 之前执行——重构前 nav.go 旧 Layer
// 1.5/3 的语义是 "modal 期间任意键都被吞噬"，避免 confirmKill / helpOverlay
// 显示时按 q 直接 quit 或按 1-8 切 pane 的行为回归。
package main

import (
	tea "charm.land/bubbletea/v2"
)

// applyModalGuards 在 3 层 Dispatcher 调用之前应用 modal 守卫。
//
// 返回 (newModel, cmd, handled)：
//   - handled=true  : 调用方应直接返回 (newModel, cmd)，跳过 Dispatcher
//   - handled=false : 调用方应继续走 Dispatcher（可能 m 已被守卫修改）
func (m dashboardModel) applyModalGuards(msg tea.KeyPressMsg) (dashboardModel, tea.Cmd, bool) {
	key := msg.String()

	// C2: confirmKill modal 守卫。仅 y/n/esc 通过到下层；其他键 cancel modal。
	// 与重构前 nav.go 旧 Layer 3 default 分支一致："任何非 y 键取消"。
	if m.confirmKill {
		switch key {
		case "y", "n", "esc":
			// Layer 0 中的 y/n/esc handler 处理具体逻辑
		default:
			m.confirmKill = false
			m.confirmPID = 0
			return m, nil, true
		}
	}

	// C3: helpOverlay modal 守卫。仅 ?/esc/q 通过到下层；其他键忽略（不修改状态）。
	// 与重构前 nav.go 旧 Layer 1.5 一致："overlay 期间一切返回，不处理后续 layer"。
	if m.helpOverlay {
		switch key {
		case "?", "esc", "q":
			// Layer 0 中的对应 handler 处理 close 逻辑
		default:
			return m, nil, true
		}
	}

	// C1: viewDebug 优先 handleDebugKey。paneFallback 总返回 consumed=true，
	// 若 dispatcher 在前会让 handleDebugKey 成为死代码。
	if m.viewMode == viewDebug {
		if m2, cmd, handled := m.handleDebugKey(key); handled {
			return m2, cmd, true
		}
	}

	return m, nil, false
}
