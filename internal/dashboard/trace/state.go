// Package trace — state.go (Story 38-5 PR8 Step 1)
//
// TraceState 字段抽离自 cmd/rnix/dashboard.go::dashboardModel 的 10 个 trace 字段
// （traceSummaries / traceErr / traceCursor / traceScrollOffset / traceViewMode +
// selectedTraceID / selectedSpanTree / spanFlatNodes / spanCursor / spanScrollOffset）。
//
// 设计原则与 PR2-PR7 同模式（值类型 · 字段公开 · nil 安全）。
//
// **38-4 waterfall bar 行为契约保留**（关键 · spec § AC5 PR8 验收点）：
//   - SpanFlatNodes 字段是 38-4 落地的 renderWaterfallBar 输入（status 颜色 + 20-char 长度 + ASCII fallback）；
//   - flattenSpanTree → SpanFlatNodes 路径不变 · 仅做字段位置迁移；
//   - traceViewMode（0=overview / 1=spans）控制视图切换 · 38-4 waterfall 仅在 spans 视图下出现。
package trace

import "github.com/rnixai/rnix/ipc"

// TraceState 持有 Trace pane 的完整状态。
//
// 字段语义（与原 dashboardModel 的 trace*/selectedTraceID/selectedSpanTree/span* 字段完全等价）：
//   - Summaries：最近一次 IPC 取回的 trace 摘要列表（按 StartTimeMs 倒序）；
//   - Err：最近一次 IPC 错误（nil 表示成功）；
//   - Cursor：当前选中的 Summaries 下标（overview 视图）；
//   - ScrollOffset：overview 视图 viewport 起始下标；
//   - ViewMode：0=overview list / 1=spans tree；
//   - SelectedTraceID：当前 drill-in 的 trace ID（spans 视图来源）；
//   - SelectedSpanTree：当前 drill-in 的 span 树（IPC wire）；
//   - SpanFlatNodes：扁平化后的可见 span 行列表（38-4 waterfall 渲染依据）；
//   - SpanCursor：spans 视图当前选中的 SpanFlatNodes 下标；
//   - SpanScrollOffset：spans 视图 viewport 起始下标。
type TraceState struct {
	Summaries        []ipc.TraceSummaryWire
	Err              error
	Cursor           int
	ScrollOffset     int
	ViewMode         int // 0=list, 1=tree
	SelectedTraceID  string
	SelectedSpanTree *ipc.SpanTreeWire
	SpanFlatNodes    []SpanFlatNode
	SpanCursor       int
	SpanScrollOffset int
}
