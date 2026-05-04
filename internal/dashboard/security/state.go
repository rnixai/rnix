// Package security — state.go (Story 38-5 PR7 Step 1)
//
// SecurityState 字段抽离自 cmd/rnix/dashboard.go::dashboardModel 的 5 个 security 字段
// （immuneStatus / immuneErr / securityAlerts / securityCursor / securityScrollOffset）。
//
// 设计原则与 PR2-PR6 同模式：
//   - 字段公开（导出）以让 cmd/rnix wrapper 直接访问；
//   - 值类型（dashboardModel 嵌入 `m.security security.SecurityState`）；
//   - 不持有 IPC client / goroutine；纯数据；
//   - nil safety：ImmuneStatus / Alerts 均允许 nil（renderSecurityPane 现有行为）。
//
// **38-4 Alert Immune 路由保留**（关键 · spec § AC5 PR7 验收点）：
//   - SecurityState.Alerts 是 38-4 落地的 synthSecurityAlerts → buildAlertEventsWith 路径的输入；
//   - cmd/rnix/dashboard.go::immuneStatusMsg 处理时调用 sortAlertsByDeviation(msg.status.Alerts)
//     填充 SecurityState.Alerts；
//   - cmd/rnix/dashboard_events.go 通过 buildAlertEventsWith(unifiedEvents, securityAlerts)
//     合成 Alert Strip 事件，IsSynthetic flag 在 38-4 落地保留；
//   - 本 PR 不改变上述路径，仅做字段位置迁移。
package security

import "github.com/rnixai/rnix/ipc"

// SecurityState 持有 Security pane 的完整状态。
//
// 字段语义（与原 dashboardModel 的 immune*/security* 字段完全等价 · 行为不变性保证）：
//   - ImmuneStatus：最近一次 IPC 取回的 immune 状态响应（nil 表示未取得 / 未连接）；
//   - ImmuneErr：最近一次 IPC 错误（nil 表示成功）；
//   - Alerts：sortAlertsByDeviation 排序后的 alert 列表（用于 Alert Strip · 38-4 落地）；
//   - Cursor：当前选中的 Alerts 下标（0-based · 可超过 len-1 表示越界）；
//   - ScrollOffset：viewport 起始下标（cursor 移动时同步维护）。
type SecurityState struct {
	ImmuneStatus *ipc.ImmuneStatusResponse
	ImmuneErr    error
	Alerts       []ipc.AlertWire
	Cursor       int
	ScrollOffset int
}
