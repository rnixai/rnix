// Package trace — types.go (Story 38-5 PR8 Step 1)
//
// SpanFlatNode 公开类型迁出自 cmd/rnix/dashboard_types.go::spanFlatNode（PR1 占位被本文件替换）。
//
// 之所以"必要扩边界"把类型一并迁入：TraceState.SpanFlatNodes 字段类型必须能容纳 SpanFlatNode，
// 而 cmd/rnix.spanFlatNode 是包私有类型，若仅迁字段不迁类型，会形成 internal/dashboard/trace
// 反向 import cmd/rnix 的循环依赖（与 PR2 flatRow / PR3 heatmapSegment / PR4 stepEntry /
// PR6 intentFlatNode 同模式）。
//
// 字段全部公开（PR1 设计决策）以满足跨包访问；cmd/rnix 端通过 type alias
// `type spanFlatNode = trace.SpanFlatNode` 保留旧名让外部 caller 零行为变化。
package trace

import "github.com/rnixai/rnix/internal/types"

// SpanFlatNode 表示 Span 树扁平化后的一行（用于 38-4 落地的 waterfall bar 渲染）。
//
// 用于 TraceState.SpanFlatNodes — flattenSpanTree 的输出元素。renderTracePane / renderTraceTreeView
// 按 ScrollOffset/visibleLines viewport 范围渲染该 slice，并在 spans 视图下绘制 waterfall bar。
//
// 字段语义（与原 cmd/rnix/dashboard_types.go::spanFlatNode 完全等价）：
//   - SpanID：span 唯一 ID（IPC wire）；
//   - PID：span 所属进程 PID；
//   - Name：span 名称（驱动名 / 操作名）；
//   - DurMs：span 持续时间（毫秒 · 38-4 waterfall 长度计算依据）；
//   - Tokens：token 累计；
//   - Status：状态字符串（success / error / pending · 38-4 waterfall 状态颜色映射）；
//   - Depth：树深度（缩进层级）；
//   - Prefix：渲染前缀字符串（└─ / ├─ 等）；
//   - IsRoot：是否为根 span。
type SpanFlatNode struct {
	SpanID string
	PID    types.PID
	Name   string
	DurMs  int64
	Tokens int
	Status string
	Depth  int
	Prefix string
	IsRoot bool
}
