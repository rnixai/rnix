// Package intent — types.go (Story 38-5 PR6 Step 1)
//
// IntentFlatNode 公开类型迁出自 cmd/rnix/dashboard_types.go::intentFlatNode（PR1 占位被本文件替换）。
// 之所以"必要扩边界"把类型一并迁入：IntentState.FlatNodes 字段类型必须能容纳 FlatNode，
// 而 cmd/rnix.intentFlatNode 是包私有类型，若仅迁字段不迁类型，会形成 internal/dashboard/intent
// 反向 import cmd/rnix 的循环依赖（与 PR2 flatRow/PR4 stepEntry 同模式）。
//
// 字段全部公开（PR1 设计决策）以满足跨包访问；cmd/rnix 端通过 type alias
// `type intentFlatNode = intent.IntentFlatNode` 保留旧名让外部 caller 零行为变化。
//
// Story 38-4 P1：treeIndex 仍按 sort 顺序填充（与 cmd/rnix 端原行为对齐）；
// userCollapsed map 通过 IntentState.TreeCollapsed keyed by tree.RootIntent。
package intent

import "github.com/rnixai/rnix/ipc"

// IntentFlatNode 表示意图树扁平化后的一行（树头或节点）。
//
// 用于 IntentState.FlatNodes — flattenIntentTrees / flattenIntentTreesWithCollapse 的输出元素。
// renderIntentPane 按 ScrollOffset/visibleLines viewport 范围渲染该 slice。
//
// 字段语义（与原 cmd/rnix/dashboard_types.go::intentFlatNode 完全等价）：
//   - TreeIndex：当前节点所属树在排序后列表中的下标（active < terminal）；
//   - NodeID：节点 ID（IsTreeHeader=true 时为空）；
//   - Indent：基于 DependsOn 计算的缩进层级（IsTreeHeader=true 时为 0）；
//   - Node：节点 wire（IsTreeHeader=true 时为 nil · 调用方需 nil 检查）；
//   - IsTreeHeader：true 表示该行是树头标题行（树边界 + 状态图标）；
//   - IsCollapsed：true 表示该树以折叠形式渲染（终态默认折叠 + 用户 toggle）；
//   - TreeWire：所属树的 wire（始终非 nil · 用于读 RootIntent / State）。
type IntentFlatNode struct {
	TreeIndex    int
	NodeID       string
	Indent       int
	Node         *ipc.IntentNodeWire
	IsTreeHeader bool
	IsCollapsed  bool // terminal tree shown collapsed
	TreeWire     *ipc.IntentTreeWire
}
