// Package model — messages.go (Story 38-5 PR11 Step 4(b) Phase 1)
//
// 跨 pane PID 同步广播消息（spec § AC11 硬约束）。
//
// 设计动机：god struct 拆分后，11 个子 Model（8 PaneModel + 3 OverlayModel）需要
// 在 selected PID 变化时同步内部状态（cursor / attached PID / 缓存重置等）。
// spec § AC11 要求 App Model 通过统一消息类型 selectPIDMsg 把 PID 变化广播给
// 所有子 Model，由 App Model 用 tea.Batch 收集每个子 Model.OnSelectPID 返回的
// tea.Cmd（spec § 04 风险 1 缓解：tea.Cmd 上传链零沉默失败）。
//
// 当前阶段（Phase 1）所有子 Model 的 OnSelectPID 仍是 nil stub；后续 Phase 2
// 会逐 pane 把 cmd/rnix 端 handleXxxPIDChange 主体迁入对应 OnSelectPID。
//
// 包归属：定义在 internal/dashboard/model 子包，避免 cmd/rnix 反向依赖（go module
// 边界硬约束）。dashboardTick 通过 model.EmitSelectPID(pid) 触发广播。
package model

import (
	tea "charm.land/bubbletea/v2"

	"github.com/rnixai/rnix/internal/types"
)

// SelectPIDMsg 是跨 pane PID 同步的广播消息（spec § AC11）。
//
// 触发点：dashboardTick 检测到 selectedPID 变化时 emit；App Model.Update 收到
// 后调用 broadcastSelectPID 把消息分发给 8 PaneModel + 3 OverlayModel（仅在
// IsActive() == true 时分发给 Overlay，spec § 04 风险 2 缓解）。
//
// 字段语义：PID 是新选中的进程 ID（types.PID 即 int32 别名）。
type SelectPIDMsg struct {
	PID types.PID
}

// EmitSelectPID 包装为 tea.Cmd，让 dashboardTick 调用点可一行 cmds = append(...,
// EmitSelectPID(m.selectedPID)) 与现有 emit*Cmd 习惯一致。
//
// 返回的 cmd 在 Bubble Tea runtime 调度时被执行，发出 SelectPIDMsg；
// App Model.Update 通过 case 路由到 broadcastSelectPID 完成广播。
func EmitSelectPID(pid types.PID) tea.Cmd {
	return func() tea.Msg {
		return SelectPIDMsg{PID: pid}
	}
}
