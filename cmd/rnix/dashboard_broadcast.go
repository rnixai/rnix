package main

import (
	"time"

	tea "charm.land/bubbletea/v2"

	dashboardmodel "github.com/rnixai/rnix/internal/dashboard/model"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/ipc"
)

// dashboard_broadcast.go — Story 38-5 PR11 Step 4(b)（Phase 1 + Phase 2）
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
// Phase 1（commit 4ed715f）落地：消息通道（SelectPIDMsg）+ 11 个 *PaneModel 字段
// 注入 + broadcastSelectPIDImpl 拆出让测试可注入 mock。所有 OnSelectPID 仍是
// nil-safe stub · 通道建立但实际不影响 cmd/rnix 端 source-of-truth 字段。
//
// Phase 2（本 commit）落地：SetState/State 双向同步通道。
//   - syncStatesToModels: cmd/rnix.<state> → m.<panel>M.SetState（broadcast 前 push）
//   - syncStatesFromModels: m.<panel>M.State() → cmd/rnix.<state>（broadcast 后 pull）
//   - 当前 OnSelectPID 全部幂等（pid == state.PID 时 noop）· dashboardTick 已通过
//     handlePIDChange 修改 cmd/rnix.<state>.PID 为新值 · 因此 SetState 推到 *PaneModel
//     时 PID 已是新值 · OnSelectPID noop · State 取回原状 → **零行为变化**。
//   - 双向通道建立后，下一步逐 pane 删除 cmd/rnix 端 handleXxxPIDChange 散点 ·
//     让 OnSelectPID 成为唯一 mutation 路径（Phase 3 dashboardTick 解构基础）。
//
// 测试策略：dashboardModel.<panel>M 字段是具体类型 *<pkg>.XxxModel · 无法直接
// 替换为 mock interface。因此把核心逻辑拆出 broadcastSelectPIDImpl 自由函数
// 接受 panes/overlays slice · 让测试可注入 mock interface 列表验证调用计数。
// dashboardModel.broadcastSelectPID 内含 SetState/State 同步 · 测试通过构造
// dashboardModel + 调 broadcastSelectPID 验证 round-trip 行为。

// broadcastSelectPID — spec § AC11 broadcast 入口（Update.SelectPIDMsg 路由）。
//
// 输入：pid types.PID — 新选中的进程 ID（来自 SelectPIDMsg.PID）。
// 输出：(dashboardModel, tea.Cmd) — 双向同步后的 dashboardModel + tea.Batch 收集到的非 nil cmd。
//
// 流程：
//  1. syncStatesToModels — cmd/rnix.<state> → m.<panel>M.SetState（push）
//  2. broadcastSelectPIDImpl — 调用 11 个子 Model.OnSelectPID（spec § AC11）
//  3. syncStatesFromModels — m.<panel>M.State() → cmd/rnix.<state>（pull）
//
// panes 顺序稳定（test 依赖）：tree, timeline, heatmap, detail, intent,
// security, trace, eval（与 spec § 02 子 Model 表格一致）；
// overlays：inspector, debug, alertStrip。
//
// 返回值：sync 后的新 dashboardModel + cmd/rnix 端 caller 必须将其作为新 model（值
// 拷贝语义 · 与现有 (m dashboardModel) handlePIDChange 同模式）。
func (m dashboardModel) broadcastSelectPID(pid types.PID) (dashboardModel, tea.Cmd) {
	m = m.syncStatesToModels()
	panes := []dashboardmodel.PaneModel{
		m.treeM, m.timelineM, m.heatmapM, m.detailM,
		m.intentM, m.securityM, m.traceM, m.evalM,
	}
	overlays := []dashboardmodel.OverlayModel{m.inspectorM, m.debugM, m.alertStripM}
	cmd := broadcastSelectPIDImpl(pid, panes, overlays)
	m = m.syncStatesFromModels()
	return m, cmd
}

// syncStatesToModels — Phase 2 双向同步 push 阶段。
//
// 把 cmd/rnix 端的 source-of-truth state（dashboardModel.<state>）通过 SetState
// 推到对应 *PaneModel.state。让 broadcast 调用 OnSelectPID 时操作的是 cmd/rnix
// 端最新状态而不是 *PaneModel 内部独立副本。
//
// nil 安全：每个 *PaneModel 字段为 nil 时跳过（不 panic）。SetState 实现已包含
// receiver nil 检查。
//
// 性能：11 次结构体值拷贝 · 字段最大的 InspectorState 含 25 字段 · 总开销 < 1us。
func (m dashboardModel) syncStatesToModels() dashboardModel {
	if m.treeM != nil {
		m.treeM.SetState(m.tree)
	}
	if m.timelineM != nil {
		m.timelineM.SetState(m.timeline)
	}
	if m.heatmapM != nil {
		m.heatmapM.SetState(m.heatmap)
	}
	if m.detailM != nil {
		m.detailM.SetState(m.detail)
	}
	if m.intentM != nil {
		m.intentM.SetState(m.intent)
	}
	if m.securityM != nil {
		m.securityM.SetState(m.security)
	}
	if m.traceM != nil {
		m.traceM.SetState(m.trace)
	}
	if m.evalM != nil {
		m.evalM.SetState(m.eval)
	}
	if m.inspectorM != nil {
		m.inspectorM.SetState(m.inspector)
	}
	if m.debugM != nil {
		m.debugM.SetState(m.debugState)
	}
	if m.alertStripM != nil {
		m.alertStripM.SetState(m.alertStrip)
	}
	return m
}

// syncStatesFromModels — Phase 2 双向同步 pull 阶段。
//
// 把 *PaneModel.state 通过 State() 取回到 cmd/rnix 端 source-of-truth state。
// broadcast 调用 OnSelectPID 后任何状态变化都会通过本函数回写。
//
// nil 安全：每个 *PaneModel 字段为 nil 时跳过 · cmd/rnix 端字段保持原状。
//
// 与 syncStatesToModels 的对偶关系：toModels 是 push（cmd/rnix → *PaneModel）·
// fromModels 是 pull（*PaneModel → cmd/rnix）· 两者搭配使用让 broadcast 真正
// 参与状态修改链而不是只触达独立副本。
func (m dashboardModel) syncStatesFromModels() dashboardModel {
	if m.treeM != nil {
		m.tree = m.treeM.State()
	}
	if m.timelineM != nil {
		m.timeline = m.timelineM.State()
	}
	if m.heatmapM != nil {
		m.heatmap = m.heatmapM.State()
	}
	if m.detailM != nil {
		m.detail = m.detailM.State()
	}
	if m.intentM != nil {
		m.intent = m.intentM.State()
	}
	if m.securityM != nil {
		m.security = m.securityM.State()
	}
	if m.traceM != nil {
		m.trace = m.traceM.State()
	}
	if m.evalM != nil {
		m.eval = m.evalM.State()
	}
	if m.inspectorM != nil {
		m.inspector = m.inspectorM.State()
	}
	if m.debugM != nil {
		m.debugState = m.debugM.State()
	}
	if m.alertStripM != nil {
		m.alertStrip = m.alertStripM.State()
	}
	return m
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

// makeTickCtx 构造 dashboardmodel.OnTickContext 给 PaneModel.OnTick 用（Story 38-5
// PR11 Step 4(b) Phase 3）。
//
// 调用方：dashboardTick 内调 m.heatmapM/m.intentM/etc.OnTick(m.makeTickCtx(paneXxx))
// 触发 IPC fetch · ctx 字段全部从 cmd/rnix-side dashboardModel 顶层状态构造（spec
// § 04 风险 6 + ADR-40 § 4.4 「IPC 编排数据保留 App Model · 通过 ctx 注入子 Model」）。
//
// 字段语义：
//   - Active: pane 是否 == activePane（PaneModel 用于决定是否激活）
//   - Connected: IPC client 已连接
//   - SelectedPID/UUID: 当前选中进程
//   - SelectedProcessIsDead: 死进程守卫（避免对 dead 进程发起 fetch）
//   - TickCount: 全局节流计数器（多个 pane 共用 · m.heatmap.TickCount 维护）
//   - ViewMode: int 形式（解耦 cmd/rnix.viewMode 类型）
//   - SocketPath: IPC socket 路径（PaneModel.OnTick 内部 ipc.Dial 创建临时 client）
func (m dashboardModel) makeTickCtx(activePane paneType) dashboardmodel.OnTickContext {
	return dashboardmodel.OnTickContext{
		Now:                   time.Now(),
		Active:                m.activePane == activePane,
		Connected:             m.connected,
		SelectedPID:           m.selectedPID,
		SelectedUUID:          m.selectedUUID,
		SelectedProcessIsDead: m.isSelectedProcessDead(),
		TickCount:             m.heatmap.TickCount,
		ViewMode:              int(m.viewMode),
		SocketPath:            ipc.SocketPath(),
	}
}
