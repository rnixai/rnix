// Package tree — types.go (Story 38-5 PR2 Step 1)
//
// FlatRow / TreeNode 类型迁出自 cmd/rnix/top.go（其中 flatRow/treeNode 字段是私有的）。
// 字段公开以让 cmd/rnix 通过 type alias 继续使用：
//
//	// cmd/rnix/top.go
//	type flatRow = tree.FlatRow
//	type treeNode = tree.TreeNode
package tree

import (
	"github.com/rnixai/rnix/vfs"
)

// FlatRow 是渲染好的进程行，含树形 prefix（"├─ " / "└─ "）+ depth + collapsed 标记。
//
// 字段语义：
//   - Proc:      原始 ProcInfo（PID/PPID/State/Skills/Tokens/...）；
//   - Prefix:    树形 ASCII 前缀（依据 isLast/isRoot 计算 + ParentPrefix 累积）；
//   - Depth:     节点深度（root=0，每层 +1）；
//   - Collapsed: 该 row 对应的 dead subtree 是否折叠（spec § 34.3 AC3）。
type FlatRow struct {
	Proc      vfs.ProcInfo
	Prefix    string
	Depth     int
	Collapsed bool
}

// TreeNode 表示进程树中的一个节点，含父子关系与子节点列表。
//
// 字段语义：
//   - Proc:     原始 ProcInfo；
//   - Children: 子节点指针列表（按 PID 升序排列，由 BuildTree 保证）。
type TreeNode struct {
	Proc     vfs.ProcInfo
	Children []*TreeNode
}
