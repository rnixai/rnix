// Package tree — builder_test.go (Story 38-5 PR2 Step 4c)
//
// BuildProcessTree / SortNodes / StateRank 行为契约测试。覆盖：
//   - StateRank 6 个状态映射 + 默认 fallback
//   - SortNodes 三种 sortMode（Time/PID/State） + asc/desc 双向 + 同 key 二级排序
//   - BuildProcessTree 关键路径：空输入 / 单根 / UUID + ParentUUID / PPID fallback /
//     dead→dead 父子 / missing parent synthetic / PID 复用 / self-parent root
package tree

import (
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// --- StateRank ---

func TestStateRank_Mapping(t *testing.T) {
	t.Parallel()
	tests := []struct {
		state types.ProcessState
		want  int
	}{
		{types.StateRunning, 0},
		{types.StateCreated, 1},
		{types.StateSuspended, 2},
		{types.StateZombie, 3},
		{types.StateDead, 4},
		{types.ProcessState(99), 5}, // unknown → 5
	}
	for _, tc := range tests {
		t.Run("", func(t *testing.T) {
			if got := StateRank(tc.state); got != tc.want {
				t.Errorf("StateRank(%v) = %d, want %d", tc.state, got, tc.want)
			}
		})
	}
}

// --- SortNodes ---

func TestSortNodes_TimeSortDesc(t *testing.T) {
	t.Parallel()
	older := &TreeNode{Proc: vfs.ProcInfo{PID: 1, State: types.StateRunning, CreatedAt: time.Now().Add(-10 * time.Second)}}
	newer := &TreeNode{Proc: vfs.ProcInfo{PID: 2, State: types.StateRunning, CreatedAt: time.Now()}}
	nodes := []*TreeNode{older, newer}
	SortNodes(nodes, 0, false) // Time, desc
	if nodes[0].Proc.PID != 2 {
		t.Errorf("desc time sort: expected newest PID=2 first, got PID=%d", nodes[0].Proc.PID)
	}
}

func TestSortNodes_TimeSortAsc(t *testing.T) {
	t.Parallel()
	older := &TreeNode{Proc: vfs.ProcInfo{PID: 1, State: types.StateRunning, CreatedAt: time.Now().Add(-10 * time.Second)}}
	newer := &TreeNode{Proc: vfs.ProcInfo{PID: 2, State: types.StateRunning, CreatedAt: time.Now()}}
	nodes := []*TreeNode{newer, older}
	SortNodes(nodes, 0, true) // Time, asc
	if nodes[0].Proc.PID != 1 {
		t.Errorf("asc time sort: expected oldest PID=1 first, got PID=%d", nodes[0].Proc.PID)
	}
}

func TestSortNodes_TimeSort_StateAsSecondary(t *testing.T) {
	t.Parallel()
	now := time.Now()
	running := &TreeNode{Proc: vfs.ProcInfo{PID: 5, State: types.StateRunning, CreatedAt: now}}
	created := &TreeNode{Proc: vfs.ProcInfo{PID: 3, State: types.StateCreated, CreatedAt: now}}
	nodes := []*TreeNode{created, running}
	// Same CreatedAt, different State — stateRank is secondary key.
	// In desc mode, cmp(0,1)=false → Created before Running.
	SortNodes(nodes, 0, false)
	if nodes[0].Proc.State != types.StateCreated {
		t.Errorf("desc time + same time: expected Created first (rank 1, larger), got %v", nodes[0].Proc.State)
	}
}

func TestSortNodes_StateSort(t *testing.T) {
	t.Parallel()
	dead := &TreeNode{Proc: vfs.ProcInfo{PID: 1, State: types.StateDead}}
	running := &TreeNode{Proc: vfs.ProcInfo{PID: 2, State: types.StateRunning}}
	nodes := []*TreeNode{dead, running}
	SortNodes(nodes, 2, true) // State, asc → lower rank first
	if nodes[0].Proc.State != types.StateRunning {
		t.Errorf("asc state sort: expected Running first (rank 0), got %v", nodes[0].Proc.State)
	}
}

func TestSortNodes_PIDSort(t *testing.T) {
	t.Parallel()
	a := &TreeNode{Proc: vfs.ProcInfo{PID: 5}}
	b := &TreeNode{Proc: vfs.ProcInfo{PID: 1}}
	c := &TreeNode{Proc: vfs.ProcInfo{PID: 3}}
	nodes := []*TreeNode{a, b, c}
	SortNodes(nodes, 1, true) // PID, asc
	if nodes[0].Proc.PID != 1 || nodes[1].Proc.PID != 3 || nodes[2].Proc.PID != 5 {
		t.Errorf("asc PID sort failed: got %d, %d, %d", nodes[0].Proc.PID, nodes[1].Proc.PID, nodes[2].Proc.PID)
	}
}

func TestSortNodes_PIDSort_Desc(t *testing.T) {
	t.Parallel()
	a := &TreeNode{Proc: vfs.ProcInfo{PID: 5}}
	b := &TreeNode{Proc: vfs.ProcInfo{PID: 1}}
	c := &TreeNode{Proc: vfs.ProcInfo{PID: 3}}
	nodes := []*TreeNode{a, b, c}
	SortNodes(nodes, 1, false) // PID, desc
	if nodes[0].Proc.PID != 5 || nodes[1].Proc.PID != 3 || nodes[2].Proc.PID != 1 {
		t.Errorf("desc PID sort failed: got %d, %d, %d", nodes[0].Proc.PID, nodes[1].Proc.PID, nodes[2].Proc.PID)
	}
}

func TestSortNodes_UnknownMode_FallbackToPID(t *testing.T) {
	t.Parallel()
	a := &TreeNode{Proc: vfs.ProcInfo{PID: 5}}
	b := &TreeNode{Proc: vfs.ProcInfo{PID: 1}}
	nodes := []*TreeNode{a, b}
	SortNodes(nodes, 99, true) // unknown mode → PID asc
	if nodes[0].Proc.PID != 1 {
		t.Errorf("unknown sortMode should fallback to PID asc, got PID=%d", nodes[0].Proc.PID)
	}
}

// --- BuildProcessTree ---

func TestBuildProcessTree_Empty(t *testing.T) {
	t.Parallel()
	got := BuildProcessTree(nil, 0, false)
	if got != nil {
		t.Errorf("BuildProcessTree(nil) = %v, want nil", got)
	}
}

func TestBuildProcessTree_SingleRoot(t *testing.T) {
	t.Parallel()
	procs := []vfs.ProcInfo{{PID: 1, PPID: 0, UUID: "u1"}}
	roots := BuildProcessTree(procs, 0, false)
	if len(roots) != 1 || roots[0].Proc.PID != 1 {
		t.Errorf("single root: expected 1 root with PID=1, got %v", roots)
	}
}

func TestBuildProcessTree_ParentUUIDLookup(t *testing.T) {
	t.Parallel()
	procs := []vfs.ProcInfo{
		{PID: 1, PPID: 0, UUID: "u1"},
		{PID: 2, PPID: 1, UUID: "u2", ParentUUID: "u1"},
	}
	roots := BuildProcessTree(procs, 0, false)
	if len(roots) != 1 {
		t.Fatalf("expected 1 root (parent u1), got %d", len(roots))
	}
	if len(roots[0].Children) != 1 {
		t.Fatalf("expected 1 child of u1, got %d", len(roots[0].Children))
	}
	if roots[0].Children[0].Proc.PID != 2 {
		t.Errorf("expected child PID=2, got %d", roots[0].Children[0].Proc.PID)
	}
}

func TestBuildProcessTree_PPIDFallback_NoParentUUID(t *testing.T) {
	t.Parallel()
	procs := []vfs.ProcInfo{
		{PID: 1, PPID: 0, UUID: "u1"},
		{PID: 2, PPID: 1, UUID: "u2"}, // no ParentUUID, fallback to PPID
	}
	roots := BuildProcessTree(procs, 0, false)
	if len(roots) != 1 || len(roots[0].Children) != 1 {
		t.Errorf("PPID fallback failed: got %d roots, %d children", len(roots), len(roots[0].Children))
	}
}

func TestBuildProcessTree_SelfParent_Root(t *testing.T) {
	t.Parallel()
	// PID == PPID: pseudo-root (rare but valid)
	procs := []vfs.ProcInfo{{PID: 1, PPID: 1}}
	roots := BuildProcessTree(procs, 0, false)
	if len(roots) != 1 {
		t.Errorf("self-parent should be root, got %d roots", len(roots))
	}
}

func TestBuildProcessTree_MissingParent_SyntheticPlaceholder(t *testing.T) {
	t.Parallel()
	// 2+ orphans share missing ParentUUID → synthetic placeholder
	procs := []vfs.ProcInfo{
		{PID: 2, PPID: 99, UUID: "u2", ParentUUID: "missing"},
		{PID: 3, PPID: 99, UUID: "u3", ParentUUID: "missing"},
	}
	roots := BuildProcessTree(procs, 0, false)
	if len(roots) != 1 {
		t.Fatalf("expected 1 synthetic root, got %d", len(roots))
	}
	if !contains(roots[0].Proc.Intent, "missing parent") {
		t.Errorf("synthetic root intent should mention 'missing parent', got %q", roots[0].Proc.Intent)
	}
	if len(roots[0].Children) != 2 {
		t.Errorf("synthetic root should have 2 children, got %d", len(roots[0].Children))
	}
}

func TestBuildProcessTree_MissingParent_SingleOrphan_NoSynthetic(t *testing.T) {
	t.Parallel()
	// Single orphan: just becomes root, no synthetic wrapper
	procs := []vfs.ProcInfo{
		{PID: 2, PPID: 99, UUID: "u2", ParentUUID: "missing"},
	}
	roots := BuildProcessTree(procs, 0, false)
	if len(roots) != 1 {
		t.Fatalf("expected 1 root (single orphan promoted), got %d", len(roots))
	}
	if contains(roots[0].Proc.Intent, "missing parent") {
		t.Errorf("single orphan should NOT trigger synthetic, got intent %q", roots[0].Proc.Intent)
	}
}

func TestBuildProcessTree_DeadDeadParenting(t *testing.T) {
	t.Parallel()
	// Both dead, no ParentUUID → fallback to allPidToKey for dead→dead lookup
	procs := []vfs.ProcInfo{
		{PID: 1, PPID: 0, UUID: "u1", State: types.StateDead},
		{PID: 2, PPID: 1, UUID: "u2", State: types.StateDead},
	}
	roots := BuildProcessTree(procs, 0, false)
	if len(roots) != 1 || len(roots[0].Children) != 1 {
		t.Errorf("dead→dead parenting failed: %d roots, %d children", len(roots), len(roots[0].Children))
	}
}

func TestBuildProcessTree_PIDReuse_UUIDKeyed(t *testing.T) {
	t.Parallel()
	// Two processes with same PID but different UUIDs (one dead old + one live new)
	now := time.Now()
	procs := []vfs.ProcInfo{
		{PID: 5, PPID: 0, UUID: "old", State: types.StateDead, CreatedAt: now.Add(-1 * time.Hour)},
		{PID: 5, PPID: 0, UUID: "new", State: types.StateRunning, CreatedAt: now},
	}
	roots := BuildProcessTree(procs, 0, false)
	if len(roots) != 2 {
		t.Errorf("PID reuse should keep both as separate UUID-keyed nodes, got %d roots", len(roots))
	}
}

// TestBuildProcessTree_PIDReuse_NoParentUUID_NoCrossMerge locks the
// spec-agent-tree-uuid-build invariant: when a process has no ParentUUID and its
// PPID points to a PID that is REUSED across daemon generations, the builder must
// NOT cross-link it under an unrelated process holding that PID — it stays a root.
// Contrast (control): an unambiguous (non-reused) PID still links via PPID fallback.
func TestBuildProcessTree_PIDReuse_NoParentUUID_NoCrossMerge(t *testing.T) {
	t.Parallel()
	now := time.Now()
	procs := []vfs.ProcInfo{
		// Tree A: root PID 5 (live) + child linked by ParentUUID.
		{PID: 5, PPID: 0, UUID: "treeA-root", State: types.StateRunning, CreatedAt: now},
		{PID: 6, PPID: 5, UUID: "treeA-child", ParentUUID: "treeA-root", State: types.StateRunning, CreatedAt: now},
		// A DIFFERENT generation that also used PID 5 → PID 5 is now reused.
		{PID: 5, PPID: 0, UUID: "old-gen", State: types.StateDead, CreatedAt: now.Add(-1 * time.Hour)},
		// Orphan with empty ParentUUID whose PPID==5 (the reused PID).
		{PID: 9, PPID: 5, UUID: "orphan-noPUUID", ParentUUID: "", State: types.StateDead, CreatedAt: now},
		// A RUNNING orphan also sharing the reused PPID 5 → must also stay root
		// (covers the non-dead fallback branch, not just dead→dead).
		{PID: 10, PPID: 5, UUID: "orphan-running", ParentUUID: "", State: types.StateRunning, CreatedAt: now},
		// Control: unique PID 20 parent + child with empty ParentUUID → must still link.
		{PID: 20, PPID: 0, UUID: "uniq-parent", State: types.StateRunning, CreatedAt: now},
		{PID: 21, PPID: 20, UUID: "uniq-child", ParentUUID: "", State: types.StateRunning, CreatedAt: now},
	}
	roots := BuildProcessTree(procs, 1 /* PID */, true)

	var find func(ns []*TreeNode, uuid string) *TreeNode
	find = func(ns []*TreeNode, uuid string) *TreeNode {
		for _, n := range ns {
			if n.Proc.UUID == uuid {
				return n
			}
			if got := find(n.Children, uuid); got != nil {
				return got
			}
		}
		return nil
	}
	isRoot := func(uuid string) bool {
		for _, r := range roots {
			if r.Proc.UUID == uuid {
				return true
			}
		}
		return false
	}

	// The reused-PID orphans (dead AND running) must NOT attach to any PID-5 holder.
	if !isRoot("orphan-noPUUID") {
		t.Errorf("dead orphan with reused PPID must stay a root, but it was attached elsewhere")
	}
	if !isRoot("orphan-running") {
		t.Errorf("running orphan with reused PPID must stay a root, but it was attached elsewhere")
	}
	for _, uuid := range []string{"treeA-root", "old-gen"} {
		n := find(roots, uuid)
		if n == nil {
			t.Fatalf("%s missing from tree", uuid)
		}
		for _, c := range n.Children {
			if c.Proc.UUID == "orphan-noPUUID" || c.Proc.UUID == "orphan-running" {
				t.Errorf("%s wrongly adopted reused-PID orphan %s", uuid, c.Proc.UUID)
			}
		}
	}
	// treeA-root keeps exactly its ParentUUID-linked child.
	if n := find(roots, "treeA-root"); n == nil || len(n.Children) != 1 || n.Children[0].Proc.UUID != "treeA-child" {
		t.Errorf("treeA-root should have exactly 1 ParentUUID child (treeA-child), got %+v", n)
	}
	// Control: unique-PID fallback still links the empty-ParentUUID child.
	if isRoot("uniq-child") {
		t.Errorf("unique-PID PPID fallback must still link uniq-child, but it became a root")
	}
	if n := find(roots, "uniq-parent"); n == nil || len(n.Children) != 1 || n.Children[0].Proc.UUID != "uniq-child" {
		t.Errorf("uniq-parent should adopt uniq-child via unambiguous PPID fallback, got %+v", n)
	}
}

func TestBuildProcessTree_NodeKey_FallbackToPID(t *testing.T) {
	t.Parallel()
	// Empty UUID → fallback to "!pid:N" key
	procs := []vfs.ProcInfo{
		{PID: 1, PPID: 0, UUID: ""},
	}
	roots := BuildProcessTree(procs, 0, false)
	if len(roots) != 1 {
		t.Errorf("empty UUID should still build root, got %d roots", len(roots))
	}
}

func TestBuildProcessTree_ChildrenSorted(t *testing.T) {
	t.Parallel()
	procs := []vfs.ProcInfo{
		{PID: 1, PPID: 0, UUID: "p", CreatedAt: time.Now()},
		{PID: 2, PPID: 1, UUID: "c2", ParentUUID: "p", CreatedAt: time.Now().Add(-10 * time.Second)},
		{PID: 3, PPID: 1, UUID: "c3", ParentUUID: "p", CreatedAt: time.Now()},
	}
	roots := BuildProcessTree(procs, 0, false) // Time desc
	if len(roots) != 1 || len(roots[0].Children) != 2 {
		t.Fatalf("expected 1 root with 2 children, got %d roots / %d children", len(roots), len(roots[0].Children))
	}
	// Time desc: newer (PID=3) first
	if roots[0].Children[0].Proc.PID != 3 {
		t.Errorf("expected child PID=3 first (desc time), got PID=%d", roots[0].Children[0].Proc.PID)
	}
}

// TestBuildProcessTree_PausedChildResumedAfterParentDead 复现用户实际报告的拓扑：
// 父进程 dev-auto.ash 已 Dead/Failed（procHistory，PID=1 UUID 前缀 019e3e），
// 子进程 bmad 在新 daemon 中由历史恢复后处于 Running+IsPaused（procTable，PID=1
// 因 PID 计数器复位，UUID 前缀偶然同为 019e3e 因 UUIDv7 时间戳邻近），
// 子的 ParentUUID 指向父的完整 UUID。期望：1 root（父）+ 1 child（子）。
//
// 此用例本身就是"先红再绿"的探针——若直接 PASS，说明 builder 拓扑逻辑无 bug，
// 真实 bug 在数据层（ListAllProcs / Resume 持久化 / Reap 漏存 ParentUUID）。
func TestBuildProcessTree_PausedChildResumedAfterParentDead(t *testing.T) {
	t.Parallel()
	parentUUID := "019e3e-aaaaaaaa-aaaa-aaaa-aaaaaaaaaaaa"
	childUUID := "019e3e-bbbbbbbb-bbbb-bbbb-bbbbbbbbbbbb"
	now := time.Now()
	procs := []vfs.ProcInfo{
		{
			PID:        1,
			PPID:       0,
			UUID:       parentUUID,
			ParentUUID: "",
			State:      types.StateDead,
			Intent:     "run: dev-auto.ash",
			CreatedAt:  now.Add(-3 * time.Hour),
			DeadAt:     now.Add(-1 * time.Minute),
			Result:     "exit 1",
		},
		{
			PID:        1,
			PPID:       0,
			UUID:       childUUID,
			ParentUUID: parentUUID,
			State:      types.StateRunning,
			IsPaused:   true,
			Intent:     "bmad-help loop",
			CreatedAt:  now.Add(-1 * time.Minute),
		},
	}
	roots := BuildProcessTree(procs, 0, false)
	if len(roots) != 1 {
		t.Fatalf("expected 1 root (dead parent), got %d roots", len(roots))
	}
	if roots[0].Proc.UUID != parentUUID {
		t.Fatalf("expected root UUID=%s, got %s", parentUUID, roots[0].Proc.UUID)
	}
	if len(roots[0].Children) != 1 {
		t.Fatalf("expected 1 child under parent, got %d", len(roots[0].Children))
	}
	if roots[0].Children[0].Proc.UUID != childUUID {
		t.Errorf("expected child UUID=%s, got %s", childUUID, roots[0].Children[0].Proc.UUID)
	}
	if !roots[0].Children[0].Proc.IsPaused {
		t.Errorf("expected child IsPaused=true to survive tree build, got false")
	}
}

// TestBuildProcessTree_DuplicateUUIDInInput 验证 BuildProcessTree 对输入中同 UUID
// 多条记录的鲁棒性：nodes map 以 UUID 为 key，后写入覆盖前者。即便 ListAllProcs
// 漏清重复，builder 也不应输出两个同 UUID 节点。
func TestBuildProcessTree_DuplicateUUIDInInput(t *testing.T) {
	t.Parallel()
	procs := []vfs.ProcInfo{
		{PID: 1, PPID: 0, UUID: "u1", State: types.StateDead, Intent: "older"},
		{PID: 1, PPID: 0, UUID: "u1", State: types.StateRunning, Intent: "newer"},
	}
	roots := BuildProcessTree(procs, 0, false)
	if len(roots) != 1 {
		t.Fatalf("duplicate UUID in input should produce 1 node, got %d roots", len(roots))
	}
	// Last-writer-wins by map semantics; not asserting which entry wins —
	// the contract here is "at most one node per UUID", not ordering.
}

// contains is a tiny strings.Contains alias to avoid import in test (style choice).
func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

// TestBuildProcessTree_DeterministicOrder_EqualCreatedAtPIDZero 验证排序全序契约
// （spec-dashboard-tree-stable-sort-identity）：CreatedAt 完全相同、PID 全为 0（历史
// 进程，PID tiebreak 失效）、仅 UUID 不同的节点，无论输入切片顺序如何（原序 vs
// 逆序）、无论 BuildProcessTree 内部 map 迭代顺序如何，展平后的输出序恒定。
func TestBuildProcessTree_DeterministicOrder_EqualCreatedAtPIDZero(t *testing.T) {
	t.Parallel()
	tie := time.Date(2026, 7, 4, 11, 0, 0, 0, time.UTC)
	uuids := []string{
		"019f2d24-0000-7000-8000-00000000000e",
		"019f2d24-0000-7000-8000-00000000000a",
		"019f2d24-0000-7000-8000-00000000000c",
		"019f2d24-0000-7000-8000-00000000000f",
		"019f2d24-0000-7000-8000-00000000000b",
		"019f2d24-0000-7000-8000-00000000000d",
	}
	makeProcs := func(order []string) []vfs.ProcInfo {
		procs := make([]vfs.ProcInfo, 0, len(order))
		for _, u := range order {
			// PID=0 且 PPID=0 命中 self-parent 根短路，全部为 root。
			procs = append(procs, vfs.ProcInfo{
				PID: 0, PPID: 0, UUID: u,
				State: types.StateDead, Intent: "tie", CreatedAt: tie,
			})
		}
		return procs
	}
	flattenUUIDs := func(procs []vfs.ProcInfo) []string {
		rows := FlattenTree(BuildProcessTree(procs, 0, false))
		out := make([]string, len(rows))
		for i, r := range rows {
			out[i] = r.Proc.UUID
		}
		return out
	}

	forward := makeProcs(uuids)
	reversed := make([]string, len(uuids))
	for i, u := range uuids {
		reversed[len(uuids)-1-i] = u
	}
	backward := makeProcs(reversed)

	seqA := flattenUUIDs(forward)
	seqB := flattenUUIDs(backward)
	if len(seqA) != len(uuids) || len(seqB) != len(uuids) {
		t.Fatalf("expected %d rows, got forward=%d backward=%d", len(uuids), len(seqA), len(seqB))
	}
	for i := range seqA {
		if seqA[i] != seqB[i] {
			t.Fatalf("output order depends on input slice order at index %d: forward=%q backward=%q",
				i, seqA[i], seqB[i])
		}
	}

	// 同一输入连跑 ≥5 次输出恒定——覆盖 BuildProcessTree 内部 map 迭代随机性。
	for run := range 5 {
		got := flattenUUIDs(forward)
		for i := range got {
			if got[i] != seqA[i] {
				t.Fatalf("run %d: output order drifted at index %d: got %q, want %q",
					run, i, got[i], seqA[i])
			}
		}
	}
}
