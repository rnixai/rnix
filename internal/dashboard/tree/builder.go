// Package tree — builder.go (Story 38-5 PR2 Step 1 + Step 4)
//
// BuildTree / FlattenTree / FlattenTreeWithCollapse 迁出自 cmd/rnix/top.go 与
// cmd/rnix/dashboard_tree.go（Step 1）。BuildProcessTree / SortNodes / StateRank
// 迁出自 cmd/rnix/dashboard_tree.go 复杂版（Step 4）。
//
// 构树统一为 UUID（spec-agent-tree-uuid-build）：
//   - BuildProcessTree(procs, sortMode, asc) — 唯一实现：UUID+ParentUUID 构树，
//     PPID 回退对重用 PID 安全（仅当目标 PID 在输入中唯一才按 PPID 挂载），支持
//     sortMode/asc + synthetic missing-parent 占位。dashboard 直接调用。
//   - BuildTree(procs) — 薄委托：以 PID 升序模式调 BuildProcessTree，供 `rnix top`
//     保持既有 PID 列表展示。不再有独立的纯 PID 构树逻辑。
package tree

import (
	"fmt"
	"sort"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// BuildTree constructs a process tree sorted by PID ascending.
//
// It is a thin delegation to BuildProcessTree — the single UUID/ParentUUID-keyed,
// PID-reuse-safe implementation — with the PID sort mode. Callers like `rnix top`
// thus get accurate parent linkage across daemon-restart PID reuse and never
// cross-link unrelated processes that happen to share a reused PID, while keeping
// the historical PID-ascending display order.
func BuildTree(procs []vfs.ProcInfo) []*TreeNode {
	return BuildProcessTree(procs, 1 /* sortMode: PID */, true)
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
// 所有 sort 模式的比较链都以 PID → UUID（固定升序）收尾构成**全序**：历史进程
// PID 全为 0，仅靠 PID tiebreak 会失效，而 BuildProcessTree 以 map 随机迭代序
// 组装输入——UUID 终极 tiebreak 保证输出顺序与输入/迭代顺序无关（消除 Dashboard
// 行跳动）。asc/desc 方向只作用于 primary/secondary key，tiebreak 恒为升序。
//
// 三种 sortMode（与 cmd/rnix/dashboard_tree.go::sortTreeNodes 完全等价）：
//
//	0 (Time):   primary=CreatedAt, secondary=StateRank, tertiary=PID asc, final=UUID asc
//	1 (PID):    primary=PID,       secondary=CreatedAt asc,               final=UUID asc
//	2 (State):  primary=StateRank, secondary=PID asc,                     final=UUID asc
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
			if ai.PID != aj.PID {
				return ai.PID < aj.PID
			}
			return ai.UUID < aj.UUID
		})
	case 2: // State
		sort.SliceStable(ns, func(i, j int) bool {
			ai, aj := ns[i].Proc, ns[j].Proc
			ri, rj := StateRank(ai.State), StateRank(aj.State)
			if ri != rj {
				return cmp(ri, rj)
			}
			if ai.PID != aj.PID {
				return ai.PID < aj.PID
			}
			return ai.UUID < aj.UUID
		})
	default: // PID (and any unknown mode)
		sort.SliceStable(ns, func(i, j int) bool {
			ai, aj := ns[i].Proc, ns[j].Proc
			if ai.PID != aj.PID {
				return cmp(int(ai.PID), int(aj.PID))
			}
			if !ai.CreatedAt.Equal(aj.CreatedAt) {
				return ai.CreatedAt.Before(aj.CreatedAt)
			}
			return ai.UUID < aj.UUID
		})
	}
}

// BuildProcessTree 是唯一的进程树构造实现（UUID + ParentUUID + PID-reuse-safe PPID
// fallback + missing parent synthetic 占位）。dashboard 直接调用，`rnix top` 经
// BuildTree 薄委托（sortMode=PID/asc）调用。
//
// 查找规则：
//	  - UUID-keyed 节点（PID 复用时多个进程共存而不互覆盖）
//	  - ParentUUID 优先（跨 daemon-restart 精确父子关系）
//	  - PPID 回退查找（兼容老进程无 ParentUUID）——仅当目标 PID 在输入中唯一时才
//	    挂载；PID 被重用则放弃回退、该节点降级为 root，避免跨代误挂（ReusedPIDs 守卫）
//	  - StateDead 进程的 dead→dead 父子关系（spec § Story 34.3 AC3），同受重用守卫
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

	// reused holds PIDs occupied by more than one process in this input — i.e.
	// recycled across daemon generations (PID counter resets on restart). The
	// PPID fallback below must NOT link by such a PID (spec-agent-tree-uuid-build).
	reused := ReusedPIDs(procs)

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

		// PID-based fallback for legacy data without ParentUUID. Only trust the
		// parent PID when it is UNAMBIGUOUS — not reused across daemon
		// generations. A reused PID cannot be disambiguated by number alone, so
		// linking by it would cross-attach unrelated trees; leave the node a root
		// instead (spec-agent-tree-uuid-build).
		if reused[p.PPID] == 0 {
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
