// Package tree — builder.go (Story 38-5 PR2 Step 1)
//
// BuildTree / FlattenTree / FlattenTreeWithCollapse 迁出自 cmd/rnix/top.go 与
// cmd/rnix/dashboard_tree.go。函数签名保持等价（仅类型大小写转公开）。
package tree

import (
	"sort"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// BuildTree constructs a process tree from a flat list of ProcInfo.
// Processes whose PPID is not in the list become root nodes.
// Children within each node are sorted by PID.
func BuildTree(procs []vfs.ProcInfo) []*TreeNode {
	if len(procs) == 0 {
		return nil
	}

	nodes := make(map[types.PID]*TreeNode, len(procs))
	for i := range procs {
		nodes[procs[i].PID] = &TreeNode{Proc: procs[i]}
	}

	var roots []*TreeNode
	for _, n := range nodes {
		if parent, ok := nodes[n.Proc.PPID]; ok {
			parent.Children = append(parent.Children, n)
		} else {
			roots = append(roots, n)
		}
	}

	sortNodes := func(ns []*TreeNode) {
		sort.Slice(ns, func(i, j int) bool {
			return ns[i].Proc.PID < ns[j].Proc.PID
		})
	}
	sortNodes(roots)
	for _, n := range nodes {
		if len(n.Children) > 1 {
			sortNodes(n.Children)
		}
	}

	return roots
}

// FlattenTree converts a tree into a flat list with indentation prefixes
// suitable for terminal display (├── for non-last children, └── for last child).
func FlattenTree(roots []*TreeNode) []FlatRow {
	if len(roots) == 0 {
		return nil
	}

	var rows []FlatRow
	var walk func(node *TreeNode, depth int, parentPrefix string, isLast bool, isRoot bool)
	walk = func(node *TreeNode, depth int, parentPrefix string, isLast bool, isRoot bool) {
		var prefix string
		if isRoot {
			prefix = ""
		} else if isLast {
			prefix = parentPrefix + "└─ "
		} else {
			prefix = parentPrefix + "├─ "
		}

		rows = append(rows, FlatRow{
			Proc:   node.Proc,
			Prefix: prefix,
			Depth:  depth,
		})

		var childPrefix string
		if isRoot {
			childPrefix = parentPrefix
		} else if isLast {
			childPrefix = parentPrefix + "   "
		} else {
			childPrefix = parentPrefix + "│  "
		}

		for i, child := range node.Children {
			walk(child, depth+1, childPrefix, i == len(node.Children)-1, false)
		}
	}

	for _, root := range roots {
		walk(root, 0, "", true, true)
	}
	return rows
}

// FlattenTreeWithCollapse converts a tree into a flat list, skipping children
// of collapsed dead subtrees. Collapsed nodes get the Collapsed flag set.
func FlattenTreeWithCollapse(roots []*TreeNode, collapsedSet map[string]bool) []FlatRow {
	if len(roots) == 0 {
		return nil
	}

	var rows []FlatRow
	var walk func(node *TreeNode, depth int, parentPrefix string, isLast bool, isRoot bool)
	walk = func(node *TreeNode, depth int, parentPrefix string, isLast bool, isRoot bool) {
		var prefix string
		if isRoot {
			prefix = ""
		} else if isLast {
			prefix = parentPrefix + "└─ "
		} else {
			prefix = parentPrefix + "├─ "
		}

		isCollapsed := node.Proc.UUID != "" && collapsedSet[node.Proc.UUID] && len(node.Children) > 0

		rows = append(rows, FlatRow{
			Proc:      node.Proc,
			Prefix:    prefix,
			Depth:     depth,
			Collapsed: isCollapsed,
		})

		if isCollapsed {
			return // skip children
		}

		var childPrefix string
		if isRoot {
			childPrefix = parentPrefix
		} else if isLast {
			childPrefix = parentPrefix + "   "
		} else {
			childPrefix = parentPrefix + "│  "
		}

		for i, child := range node.Children {
			walk(child, depth+1, childPrefix, i == len(node.Children)-1, false)
		}
	}

	for _, root := range roots {
		walk(root, 0, "", true, true)
	}
	return rows
}
