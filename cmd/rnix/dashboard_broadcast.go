package main

import (
	tea "charm.land/bubbletea/v2"

	dashboardmodel "github.com/rnixai/rnix/internal/dashboard/model"
	"github.com/rnixai/rnix/internal/types"
)

// dashboard_broadcast.go — Story 38-5 PR11 Step 4(b) Phase 1
//
// 跨 pane PID 同步广播实现（spec § AC11 硬约束）。
//
// 设计原则：
//   - broadcastSelectPID 是 spec § AC11 「11 个 hook」的实现入口；
//   - tea.Batch 收集所有非 nil cmd · 不会让任一子 Model 的异步操作沉默失败
//     （spec § 04 风险 1 缓解）；
//   - 8 PaneModel 全部接收 broadcast；
//   - 3 OverlayModel 仅在 IsActive() == true 时接收（spec § 04 风险 2 缓解）；
//   - 11 字段任一为 nil 时跳过该字段（防御性 · 让 mock 测试可注入部分字段）。
//
// 测试策略：dashboardModel.<panel>M 字段是具体类型 *<pkg>.XxxModel · 无法直接
// 替换为 mock interface。因此把核心逻辑拆出 broadcastSelectPIDImpl 自由函数
// 接受 panes/overlays slice · 让测试可注入 mock interface 列表验证调用计数。
// dashboardModel.broadcastSelectPID 仅是构建 11 字段 slice 的 thin wrapper。
//
// 现状（Phase 1）：所有子 Model.OnSelectPID 当前是 nil-safe stub · 此函数
// 的 broadcast 实质上只是验证通道建立 + tea.Batch 路由正确。
// Phase 2 后续会话逐 pane 把 cmd/rnix 端 handleXxxPIDChange 主体迁入对应
// OnSelectPID · 此函数无需修改即可承载 cmd 收集。

// broadcastSelectPID — spec § AC11 broadcast 入口（dashboardTick / Update 调用）。
//
// 输入：pid types.PID — 新选中的进程 ID（来自 SelectPIDMsg.PID）。
// 输出：tea.Cmd — tea.Batch 收集到的所有子 Model.OnSelectPID 返回的非 nil cmd。
//
// panes 顺序稳定（test 依赖）：tree, timeline, heatmap, detail, intent,
// security, trace, eval（与 spec § 02 子 Model 表格一致）；
// overlays：inspector, debug, alertStrip。
func (m dashboardModel) broadcastSelectPID(pid types.PID) tea.Cmd {
	panes := []dashboardmodel.PaneModel{
		m.treeM, m.timelineM, m.heatmapM, m.detailM,
		m.intentM, m.securityM, m.traceM, m.evalM,
	}
	overlays := []dashboardmodel.OverlayModel{m.inspectorM, m.debugM, m.alertStripM}
	return broadcastSelectPIDImpl(pid, panes, overlays)
}

// broadcastSelectPIDImpl — 测试入口（注入 mock panes/overlays）。
//
// 与 broadcastSelectPID 实质等价 · 拆出让测试可绕过 dashboardModel 具体字段类型
// 限制。nil-safe：panes/overlays 中任一元素为 nil 时跳过 · 不调用 hook · 不 panic。
//
// 性能：O(len(panes) + len(overlays)) 顺序遍历开销可忽略；tea.Batch 内部
// 并发执行 cmd。
func broadcastSelectPIDImpl(
	pid types.PID,
	panes []dashboardmodel.PaneModel,
	overlays []dashboardmodel.OverlayModel,
) tea.Cmd {
	cmds := make([]tea.Cmd, 0, len(panes)+len(overlays))
	for _, p := range panes {
		if p == nil {
			continue
		}
		if c := p.OnSelectPID(pid); c != nil {
			cmds = append(cmds, c)
		}
	}
	for _, o := range overlays {
		if o == nil || !o.IsActive() {
			continue
		}
		if c := o.OnSelectPID(pid); c != nil {
			cmds = append(cmds, c)
		}
	}
	return tea.Batch(cmds...)
}

// emitSelectPIDCmd 是 dashboardmodel.EmitSelectPID 的 cmd/rnix 端别名，让
// dashboardTick 调用点保持简洁（cmds = append(cmds, emitSelectPIDCmd(pid))
// 与既有 emit*Cmd 习惯一致）。
//
// 触发后 Bubble Tea runtime 调度执行返回 SelectPIDMsg → Update.case routing
// → broadcastSelectPID。
func emitSelectPIDCmd(pid types.PID) tea.Cmd {
	return dashboardmodel.EmitSelectPID(pid)
}
