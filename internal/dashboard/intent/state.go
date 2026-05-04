// Package intent — state.go (Story 38-5 PR6 Step 1)
//
// IntentState 字段抽离自 cmd/rnix/dashboard.go::dashboardModel 的 6 个 intent 字段
// （intentTrees / intentTreeErr / intentFlatNodes / intentCursor / intentScrollOffset
// + intentTreeCollapsed · 38-4 P1 落地）。
//
// 设计原则与 PR2 TreeState / PR3 HeatmapState / PR4 TimelineState / PR5 DetailState 一致：
//   - 字段公开（导出）以让 cmd/rnix wrapper 直接访问；
//   - 值类型（dashboardModel 嵌入 `m.intent intent.IntentState`）；
//   - 不持有 IPC client / goroutine；纯数据；
//   - nil safety：FlatNodes / Trees / TreeCollapsed 均允许 nil（renderIntentPane 现有行为）。
package intent

import "github.com/rnixai/rnix/ipc"

// IntentState 持有 Intent pane 的完整状态。
//
// 字段语义（与原 dashboardModel 的 intent* 字段完全等价 · 行为不变性保证）：
//   - Trees：最近一次 IPC 取回的意图树列表（fetchIntentTreesCmd → intentTreesMsg.trees.Intents）；
//   - TreeErr：最近一次 IPC 错误（nil 表示成功）；
//   - FlatNodes：扁平化后的可见行列表（flattenIntentTreesWithCollapse 输出）；
//   - Cursor：当前选中的 FlatNodes 下标（0-based · 可超过 len-1 表示初始或越界）；
//   - ScrollOffset：viewport 起始下标（intentAdjustScroll 维护）；
//   - TreeCollapsed：用户级折叠 map · keyed by tree.RootIntent（38-4 AC#3 / P1 stable across reordering）。
//     pruneIntentCollapse 会清除已不在列表的 stale key（38-4 P4）；nil 安全。
//
// **38-4 落地行为保留**（关键）：
//   - TreeCollapsed 由 RootIntent 字符串作 key，重排后状态仍稳定；
//   - 终态树（completed/failed）默认折叠，TreeCollapsed 中的 toggle 不能让其重新展开；
//   - dashboard_pane_dispatcher.go 在每次 toggle 后跑 pruneIntentCollapse + 重新 flatten。
type IntentState struct {
	Trees         []*ipc.IntentTreeWire
	TreeErr       error
	FlatNodes     []IntentFlatNode
	Cursor        int
	ScrollOffset  int
	TreeCollapsed map[string]bool // Story 38-4 P1: keyed by tree.RootIntent (stable across reordering); nil-safe
}
