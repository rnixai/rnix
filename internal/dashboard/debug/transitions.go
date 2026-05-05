// Package debug — transitions.go (Story 38-5 PR11 Step 4(b) Phase 2)
//
// HandlePIDChange 把 cmd/rnix.handleDebugPIDChange 中 Debug pane 的 9 字段状态
// 重置部分迁入 debug 包，让 dashboard_debug.go 不再持有 9 行 inline 散点
// （行为契约保留 + Story 34.6 strace fusion 行为契约保留 + IPC 调度边界清晰）。
//
// 与 model.OnSelectPID 的区别（详见 detail/transitions.go 同模式）：
//   - OnSelectPID(pid) 当前是 Phase 1 stub（broadcast 通道接管时使用）；
//   - HandlePIDChange(state, newPID) 是 Phase 2 cmd/rnix 端 inline 入口（已知
//     新 PID · 重置 9 个字段 · 不含 IPC 调用）；两者并存是 Story 38-5 渐进迁移
//     产物（Phase 3 完成后 OnSelectPID 接管 · 删除本函数）。
//
// 包边界硬约束：本函数**不**调用 stopStraceStream / loadHistoricalStraceCmd /
// startStraceStreamCmd（IPC 调用属 cmd/rnix 端职责 · 与 spec § 04 风险 6 共享
// EventStream / IPC 命令保留在 App Model 一致）。本函数仅做纯状态重置。
//
// 调用约定：
//
//	// cmd/rnix/dashboard_debug.go::handleDebugPIDChange 内：
//	if !m.debugState.Mode { return m, nil }
//	m.stopStraceStream()                                      // IPC: 关闭旧连接
//	m.debugState = debug.HandlePIDChange(m.debugState, m.selectedPID)  // 9 字段重置
//	if m.selectedPID == 0 { return m, nil }
//	if m.isSelectedProcessDead() {
//	    return m, m.loadHistoricalStraceCmd()                 // IPC: 仅历史
//	}
//	return m, tea.Batch(m.loadHistoricalStraceCmd(), m.startStraceStreamCmd())  // IPC
//
// 行为契约（与原 cmd/rnix.handleDebugPIDChange line 397-405 byte-for-byte 等价）：
//   - StraceEvents = nil（清空 strace ring buffer）
//   - Events = nil（清空 merged debug timeline events）
//   - DeviceLatency = make(map[string]*DeviceLatencyStats)（fresh empty map · 不复用旧 map）
//   - CtxProfile = nil（清空 Context Profile 数据）
//   - ScrollTop = 0 / Cursor = 0（重置视口 + 选中位置）
//   - AttachedPID = newPID（attached 跟随新选中 · 与 selectedPID 解耦防 PID 切换中断）
//   - AutoReloaded = false（重新允许 auto-reload）
//   - HistWatermark = 0（重置历史加载 watermark · 让新 PID 的所有事件重新加载）
//   - 其他字段（Mode / Client / StraceCh / ShowStrace / AutoScroll）保留不变
package debug

import (
	"github.com/rnixai/rnix/internal/types"
)

// HandlePIDChange 处理 PID 切换：重置 9 个 state 字段（不含 IPC 调用）。
//
// 与 OnSelectPID(pid) 的关系：OnSelectPID 是 Phase 3 broadcast 通道入口（当前
// stub）；本函数是 Phase 2 cmd/rnix 端 inline 入口（接受新 PID · 立即重置）。
// 两者并存到 Phase 3 broadcast 完全接管为止。
//
// 调用方应在 dashboard_debug.go::handleDebugPIDChange 内 stopStraceStream
// 之后、IPC 调度之前调用：
//
//	m.debugState = debug.HandlePIDChange(m.debugState, m.selectedPID)
//
// nil safety：state 任意字段为 nil 时安全（Go 零值语义 · DeviceLatency 一定
// 写为 fresh map · 即使原值为 nil 也不 panic）。
func HandlePIDChange(state DebugState, newPID types.PID) DebugState {
	state.StraceEvents = nil
	state.Events = nil
	state.DeviceLatency = make(map[string]*DeviceLatencyStats)
	state.CtxProfile = nil
	state.ScrollTop = 0
	state.Cursor = 0
	state.AttachedPID = newPID
	state.AutoReloaded = false
	state.HistWatermark = 0
	return state
}
