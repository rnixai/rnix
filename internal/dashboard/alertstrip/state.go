// Package alertstrip — AlertStripState 与 AlertStripModel 实现 OverlayModel 接口
// （Story 38-5 PR12，从 god struct dashboardModel 抽出 alert strip 状态字段）。
//
// 设计决策（PR12 Step 1，2026-05-04）：
//
//   - 仅抽出 2 个标量字段（Expanded/Cursor）。Events []UnifiedEvent 与
//     JumpTarget *UnifiedEvent 因 cmd/rnix-private UnifiedEvent 类型 cascade
//     暂保留在 dashboardModel（与 PR11 DebugState 抽出 12 标量但保留
//     Events/StraceEvents 的决策同模式）。
//
//   - UnifiedEvent 类型本身依赖 cmd/rnix-private *stepEntry / *ipc.SyscallEventWire；
//     若把 UnifiedEvent 完整迁出至 internal/dashboard 包，需要先把 stepEntry 真实类型
//     从 timeline.StepEntry 解耦或抽到独立的 internal/dashboard/event 包。这是
//     比 PR12 大的子任务，留待 Story 38-5 retro 后单独评估。
//
//   - alertstrip 包**不**反向 import cmd/rnix（go module 边界 + spec § Risk 3
//     SearchPlugin 解耦同精神）。因此 Events/JumpTarget 不能在此包定义为类型化字段。
//
// 38-4 Alert Immune 路由行为契约 · spec § AC#2：
//
//   - synthSecurityAlerts → buildAlertEventsWith 路径（cmd/rnix.dashboardTick 内）
//     不变，只是 m.alertEvents 的归属字段名从 dashboardModel.alertEvents 暂保留
//     不动（详见上述决策）。
//   - alertJumpTarget *UnifiedEvent（PID change 后的延迟跳转）同上。
//
// 字段命名约定（与其他子 Model 一致）：
//
//   - 公开字段（首字母大写）支持 cmd/rnix 端通过类型化字段直接读写。
//   - 包外可见的 AlertStripState 类型支持 dashboardModel 的 deprecated getter
//     `AlertStripState() alertstrip.AlertStripState` 给旧测试过渡（PR11 Step 4
//     架构调整时统一删除）。
package alertstrip

// AlertStripState holds the scalar state for the alert strip (Story 38-5 PR12 Step 1).
//
// Field semantics:
//
//   - Expanded: alert strip 是否处于展开模式（按 `a` 切换）。展开时显示 maxLines=8
//     行 + 行光标高亮 + `[`/`]` 按键导航 cursor；折叠时仅显示前 2 行 + count badge。
//
//   - Cursor: 展开模式下当前光标行（0-based · clamp 到 [0, visible-1] · 折叠时
//     重置为 0）。enter 键根据 Cursor 选中的 alert 路由到 Timeline / Security
//     pane（38-4 AC#2）。
//
// Zero value: Expanded=false, Cursor=0 — alert strip 折叠且光标在首行，是合法初始
// 状态（newDashboardModel 直接零值即可，不需要显式初始化）。
//
// nil-safety: AlertStripState 是值类型，不需要 make/new。所有字段都是值类型字段
// （bool/int），跨 PID 切换时不需要清空（与 IntentModel/SecurityModel 同模式 —
// alert strip 是跨进程全局视图）。
type AlertStripState struct {
	// Expanded controls whether the alert strip is in expanded mode (8 lines + cursor)
	// or collapsed mode (2 lines + count badge). Toggled by Layer 0 `a` key
	// (cmd/rnix/dashboard_keylayers.go:135-154).
	Expanded bool

	// Cursor is the currently focused alert row in expanded mode (0-based).
	// Layer 0 `[`/`]` keys move it; clamped to [0, visible-1] where visible
	// = alertStripHeight(len(events), expanded). Reset to 0 on collapse.
	//
	// Story 38-4 AC#2: enter key uses Cursor to pick the alert that drives
	// the cross-pane jump routing (Immune → Security pane / others → Timeline).
	Cursor int
}

// StateProvider is the interface that dashboardModel satisfies through its
// `AlertStripState() AlertStripState` deprecated getter, allowing alertstrip
// helpers / KeyLayer factories (if any · PR12 Step 2 评估) to read state via
// `ctx.(StateProvider)` cast without importing cmd/rnix.
//
// Implemented by both *AlertStripModel (PR12 Step 3) and dashboardModel (via
// the transitional getter; Deprecated: removed in 38-5 PR11 Step 4).
type StateProvider interface {
	AlertStripState() AlertStripState
}
