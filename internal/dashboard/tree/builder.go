// Package tree — builder.go (Story 38-5 PR2 Step 1 + Step 4)
//
// BuildTree / FlattenTree / FlattenTreeWithCollapse 迁出自 cmd/rnix/top.go 与
// cmd/rnix/dashboard_tree.go（Step 1）。BuildProcessTree / SortNodes / StateRank
// 迁出自 cmd/rnix/dashboard_tree.go 复杂版（Step 4）。
//
// 函数命名差异说明：
//   - BuildTree(procs)                       — 简化版（PID 排序）· 被 cmd/rnix/top.go 用
//   - BuildProcessTree(procs, sortMode, asc) — 复杂版（UUID+ParentUUID + sortMode/asc + synthetic parent）· 被 dashboard 用
//
// 二者并存：top 命令对快速 PID 列表展示需求简单；dashboard 需要 UUID 去重 +
// 多种 sortMode + ParentUUID 跨 daemon-restart 父子关系保持 + missing parent
// 合成节点占位等高级功能。
package tree

import (
	"fmt"
	"sort"
	"time"

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

// --- Story 38-5 PR2 Step 4: 复杂版 BuildProcessTree / SortNodes / StateRank ---
//
// 这些函数从 cmd/rnix/dashboard_tree.go 迁出，提供 dashboard 端使用的
// UUID-keyed 进程树构建 + 多 sortMode 排序 + missing parent synthetic node。

// StateRank 把进程状态映射为排序优先级（数值越小优先级越高）。
//
// 排序优先级（与 cmd/rnix/dashboard_tree.go::stateRank 等价）：
//
//	StateRunning   = 0  (最优先 · 活跃进程置顶)
//	StateCreated   = 1
//	StateSuspended = 2
//	StateZombie    = 3
//	StateDead      = 4
//	其他            = 5  (兜底)
//
// 由 SortNodes 在 sortMode=Time/State 时使用作为次级 key，确保活跃进程
// 在同 CreatedAt 时排在 Zombie/Dead 之前。
func StateRank(s types.ProcessState) int {
	switch s {
	case types.StateRunning:
		return 0
	case types.StateCreated:
		return 1
	case types.StateSuspended:
		return 2
	case types.StateZombie:
		return 3
	case types.StateDead:
		return 4
	default:
		return 5
	}
}

// SortNodes 按指定 sortMode 与方向（asc=true 升序 / false 降序）原地排序 TreeNode 切片。
//
// 所有 sort 模式都用 PID 作为最末次级 key 防止位置抖动。
//
// 三种 sortMode（与 cmd/rnix/dashboard_tree.go::sortTreeNodes 完全等价）：
//
//	0 (Time):   primary=CreatedAt, secondary=StateRank, tertiary=PID asc
//	1 (PID):    primary=PID,       secondary=CreatedAt asc
//	2 (State):  primary=StateRank, secondary=PID asc
//
// 默认（其他 sortMode 值）：按 PID 排序。
//
// 复杂度：O(n log n)（标准 sort.SliceStable 保稳定）。
func SortNodes(ns []*TreeNode, sortMode int, asc bool) {
	cmp := func(a, b int) bool {
		if asc {
			return a < b
		}
		return a > b
	}
	cmpTime := func(a, b time.Time) bool {
		if asc {
			return a.Before(b)
		}
		return a.After(b)
	}

	switch sortMode {
	case 0: // Time
		sort.SliceStable(ns, func(i, j int) bool {
			ai, aj := ns[i].Proc, ns[j].Proc
			if !ai.CreatedAt.Equal(aj.CreatedAt) {
				return cmpTime(ai.CreatedAt, aj.CreatedAt)
			}
			ri, rj := StateRank(ai.State), StateRank(aj.State)
			if ri != rj {
				return cmp(ri, rj)
			}
			return ai.PID < aj.PID
		})
	case 2: // State
		sort.SliceStable(ns, func(i, j int) bool {
			ai, aj := ns[i].Proc, ns[j].Proc
			ri, rj := StateRank(ai.State), StateRank(aj.State)
			if ri != rj {
				return cmp(ri, rj)
			}
			return ai.PID < aj.PID
		})
	default: // PID (and any unknown mode)
		sort.SliceStable(ns, func(i, j int) bool {
			pi, pj := ns[i].Proc.PID, ns[j].Proc.PID
			if pi != pj {
				return cmp(int(pi), int(pj))
			}
			return ns[i].Proc.CreatedAt.Before(ns[j].Proc.CreatedAt)
		})
	}
}

// BuildProcessTree 用复杂查找规则（UUID + ParentUUID + PPID fallback + missing parent
// synthetic 占位）构造进程树。是 dashboard 渲染主用接口。
//
// 与简化版 BuildTree 的差异：
//
//	BuildTree(procs):
//	  - 仅按 PID 查找父节点（PPID lookup）
//	  - 仅按 PID 升序排序
//	  - 不处理 UUID 复用 / missing parent
//
//	BuildProcessTree(procs, sortMode, asc):
//	  - UUID-keyed 节点（PID 复用时多个进程共存而不互覆盖）
//	  - ParentUUID 优先（跨 daemon-restart 精确父子关系）
//	  - PPID 回退查找（兼容老进程无 ParentUUID）
//	  - StateDead 进程的 dead→dead 父子关系（spec § Story 34.3 AC3）
//	  - missing ParentUUID 的孤儿合成 synthetic placeholder node
//	  - 多种 sortMode（Time/PID/State）+ 升降序参数
//
// 使用场景：dashboard 在 dashboardTick / handlePIDChange / pane_dispatcher 等地方
// 多次调用此函数构建 m.tree.Rows。
//
// 复杂度：O(n²) 最坏情况（嵌套 PPID lookup loop），typical n=10-100 可在 1ms 内完成。
//
// 参数：
//   - procs:    完整进程列表（包含 dead/zombie/running 等所有状态）
//   - sortMode: 排序模式（0=Time, 1=PID, 2=State）
//   - asc:      升降序（false=desc 默认 / true=asc）
//
// 返回：root TreeNode 列表（每个 root 包含递归子节点）。
func BuildProcessTree(procs []vfs.ProcInfo, sortMode int, asc bool) []*TreeNode {
	if len(procs) == 0 {
		return nil
	}

	nodes := make(map[string]*TreeNode, len(procs))
	pidToKey := make(map[types.PID]string, len(procs))
	allPidToKey := make(map[types.PID]string, len(procs))

	nodeKey := func(p vfs.ProcInfo) string {
		if p.UUID != "" {
			return p.UUID
		}
		return fmt.Sprintf("!pid:%d", p.PID)
	}

	for i := range procs {
		p := procs[i]
		key := nodeKey(p)
		nodes[key] = &TreeNode{Proc: p}
		if p.State != types.StateDead {
			pidToKey[p.PID] = key
		}
		if existing, ok := allPidToKey[p.PID]; ok {
			if existingNode, ok2 := nodes[existing]; ok2 {
				if p.CreatedAt.Before(existingNode.Proc.CreatedAt) {
					allPidToKey[p.PID] = key
				}
			}
		} else {
			allPidToKey[p.PID] = key
		}
	}

	var roots []*TreeNode
	orphansByParent := make(map[string][]*TreeNode)
	for _, n := range nodes {
		p := n.Proc
		myKey := nodeKey(p)
		if p.PID == p.PPID {
			roots = append(roots, n)
			continue
		}

		if p.PPID == 0 && p.ParentUUID != "" {
			if parent, ok := nodes[p.ParentUUID]; ok {
				parent.Children = append(parent.Children, n)
				continue
			}
			orphansByParent[p.ParentUUID] = append(orphansByParent[p.ParentUUID], n)
			continue
		}

		if p.PPID == 0 {
			roots = append(roots, n)
			continue
		}

		if p.ParentUUID != "" {
			if parent, ok := nodes[p.ParentUUID]; ok {
				parent.Children = append(parent.Children, n)
				continue
			}
			orphansByParent[p.ParentUUID] = append(orphansByParent[p.ParentUUID], n)
			continue
		}

		if parentKey, ok := pidToKey[p.PPID]; ok {
			if parentKey != myKey {
				if parent, ok := nodes[parentKey]; ok {
					parent.Children = append(parent.Children, n)
					continue
				}
			}
		}
		if p.State == types.StateDead {
			if parentKey, ok := allPidToKey[p.PPID]; ok {
				if parentKey != myKey {
					if parent, ok := nodes[parentKey]; ok {
						parent.Children = append(parent.Children, n)
						continue
					}
				}
			}
		}
		roots = append(roots, n)
	}

	for parentUUID, children := range orphansByParent {
		if len(children) == 1 {
			roots = append(roots, children[0])
			continue
		}
		uuidShort := parentUUID
		if len(uuidShort) > 8 {
			uuidShort = uuidShort[:8]
		}
		synthetic := &TreeNode{
			Proc: vfs.ProcInfo{
				PID:    children[0].Proc.PPID,
				UUID:   parentUUID,
				State:  types.StateDead,
				Intent: fmt.Sprintf("[missing parent %s…]", uuidShort),
			},
			Children: children,
		}
		roots = append(roots, synthetic)
	}

	SortNodes(roots, sortMode, asc)
	for _, n := range nodes {
		if len(n.Children) > 1 {
			SortNodes(n.Children, sortMode, asc)
		}
	}

	return roots
}
