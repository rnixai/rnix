// Package alertstrip — AlertStripState 与 AlertStripModel 实现 OverlayModel 接口
// （Story 38-5 PR12，从 god struct dashboardModel 抽出 alert strip 状态字段）。
//
// 设计决策演进:
//
//   - PR12 Step 1（2026-05-04）：仅抽出 2 个标量字段（Expanded/Cursor）。
//     Events/JumpTarget 因 cmd/rnix-private UnifiedEvent 类型 cascade 暂保留。
//
//   - **PR11 Step 4(a) cascade 解除**（2026-05-04 同会话延伸）：UnifiedEvent
//     已迁至 internal/dashboard/event 包（commit a08ae3d）· 本包现可直接引用
//     event.UnifiedEvent · 4 字段全部抽出至 AlertStripState（与 spec § Tasks 11.3
//     line 187 「4 字段 Expanded/Cursor/Events/JumpTarget」最终对齐）。
//
//   - alertstrip 包**不**反向 import cmd/rnix（go module 边界 + spec § Risk 3
//     SearchPlugin 解耦同精神）。
//
// 38-4 Alert Immune 路由行为契约 · spec § AC#2 全部保留：
//
//   - synthSecurityAlerts → buildAlertEventsWith 路径（cmd/rnix.dashboardTick 内）
//     不变；
//   - alertJumpTarget *event.UnifiedEvent（PID change 后的延迟跳转）行为不变。
//
// 字段命名约定（与其他子 Model 一致）：
//
//   - 公开字段（首字母大写）支持 cmd/rnix 端通过类型化字段直接读写。
//   - 包外可见的 AlertStripState 类型支持 dashboardModel 的 deprecated getter
//     `AlertStripState() alertstrip.AlertStripState` 给旧测试过渡（PR11 Step 4
//     架构调整时统一删除）。
package alertstrip

import (
	"github.com/rnixai/rnix/internal/dashboard/event"
)

// AlertStripState holds the alert strip state (Story 38-5 PR12 Step 1 + PR11 Step 4(a) cascade fix).
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
//   - Events: cached alerts (Severity >= SevWarn, sorted) · buildAlertEventsWith
//     在 cmd/rnix.dashboardTick 内重建 · alert strip 渲染 + cursor clamp 都用此字段。
//
//   - JumpTarget: pending alert jump after PID change (38-4 AC#2 路由) · enter
//     键触发 PID 切换时把当前选中的 alert 存入 JumpTarget · PID 切换完成后
//     handlePIDChange 消费它并跳转到对应 pane (Immune → Security / 其他 →
//     Timeline)。
//
// Zero value: Expanded=false, Cursor=0, Events=nil, JumpTarget=nil — alert strip
// 折叠且无 alerts，是合法初始状态（newDashboardModel 直接零值即可）。
//
// nil-safety: AlertStripState 是值类型，所有字段值类型语义；Events nil slice
// 安全（len(nil) == 0 / range 安全）；JumpTarget nil 是常态（90%+ tick）。
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

	// Events is the cached list of alerts (Severity >= SevWarn, sorted by
	// severity desc then timestamp desc). Rebuilt by buildAlertEventsWith
	// in cmd/rnix.dashboardTick from m.unifiedEvents + m.security.Alerts.
	//
	// Story 38-4 AC#4: includes synthetic security alerts (IsSynthetic=true)
	// produced by synthSecurityAlerts; those bypass the TTL filter (38-4 P0
	// patch · see internal/dashboard/event/event.go::UnifiedEvent godoc).
	//
	// Migrated from dashboardModel.alertEvents in PR11 Step 4(a) cascade fix
	// (commit a08ae3d unblocked UnifiedEvent type cascade).
	Events []event.UnifiedEvent

	// JumpTarget is the pending alert jump target after a PID change (Story
	// 38-4 AC#2 routing protocol). When the user presses enter on an alert
	// while the strip is expanded:
	//   1. handleAlertEnter sets JumpTarget = &alerts[Cursor];
	//   2. selectedPID is updated to alert.PID (triggers handlePIDChange);
	//   3. handlePIDChange consumes JumpTarget and routes to the right pane
	//      (Immune → Security / others → Timeline) and clears JumpTarget.
	//
	// Migrated from dashboardModel.alertJumpTarget in PR11 Step 4(a) cascade fix.
	JumpTarget *event.UnifiedEvent
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
