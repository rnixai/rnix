package main

// =============================================================================
// ATDD Story 34.8: dashboard 翻页累积合并 + 实时监控消费者隔离（cmd 层）
// =============================================================================
//
// Test Strategy（详见 _bmad-output/test-artifacts/atdd-checklist-34-8-*.md）:
//   - 红灯机制 [[atdd-code-story-red-mechanism-preference]]：RED 用骨架 + t.Skip
//     保 ATDD 提交期 make all 全绿；dev 移 skip 填实现验 RED→GREEN。
//   - green-guard 回归红线不 skip：当前即应 PASS，dev 改动后须保持。
//
// 本文件覆盖：
//   34.8-UNIT-006 (AC4, P1, RED)         mergeAccumulatedProcs：UUID 去重累积（最新覆盖 + 新增追加 + 不丢条目）
//   34.8-UNIT-007 (AC5, P0, RED)         跨页树补全：累积 page1(子,父缺)+page2(父) → buildProcessTree 无 missing-parent synthetic
//   34.8-UNIT-008 (AC6, P0, RED)         monitorInputProcs：实时监控只吃 active 子集（排除 Dead/历史 PID=0）
//   34.8-UNIT-009 (AC6, P1, green-guard) detectSpawnExitEvents trap-safe：PID 同在 prev&curr 不报 SPAWN（钉死 active 子集喂入为何安全）

import (
	"strings"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/ipc"
	"github.com/rnixai/rnix/vfs"
)

// ---------------------------------------------------------------------------
// 34.8-UNIT-006 (AC4, P1, RED) — mergeAccumulatedProcs UUID 去重累积
// ---------------------------------------------------------------------------
//
// dashboard tick 从「每 tick 全量替换 m.processes」改为「按 UUID 去重 merge 累积」：
//   - 同 UUID：以最新页数据覆盖（State/Result 等可能变化）
//   - 新 UUID：追加
//   - 不丢之前累积的条目（累积语义，非替换）
//
// RED 来源：mergeAccumulatedProcs 当前为 no-op 骨架返回 page（直接丢弃 acc）
//   → 「保留 acc 中未在 page 出现的 UUID」断言 FAIL。
func TestATDD_34_8_UNIT_006_MergeAccumulated_DedupByUUID(t *testing.T) {
	now := time.Now()
	// 已累积：u1(Running) + u2(Running)
	acc := []vfs.ProcInfo{
		{UUID: "u1", Intent: "first", State: types.StateRunning, CreatedAt: now},
		{UUID: "u2", Intent: "second", State: types.StateRunning, CreatedAt: now.Add(time.Minute)},
	}
	// 新一页：u1 状态更新为 Dead（最新覆盖）+ 新 UUID u3
	page := []vfs.ProcInfo{
		{UUID: "u1", Intent: "first", State: types.StateDead, CreatedAt: now},
		{UUID: "u3", Intent: "third", State: types.StateRunning, CreatedAt: now.Add(2 * time.Minute)},
	}

	merged := mergeAccumulatedProcs(acc, page)

	byUUID := make(map[string]vfs.ProcInfo, len(merged))
	for _, p := range merged {
		byUUID[p.UUID] = p
	}

	// 全集：u1/u2/u3 各一条（去重 + 累积，不丢 u2）
	if len(merged) != 3 {
		t.Fatalf("AC4: 累积合并后应为 3 条（u1/u2/u3），got %d", len(merged))
	}
	if _, ok := byUUID["u2"]; !ok {
		t.Error("AC4: 累积条目 u2 不在新页中，merge 后不得丢失（累积非替换）")
	}
	// 同 UUID 以最新覆盖：u1 应为新页的 Dead
	if byUUID["u1"].State != types.StateDead {
		t.Errorf("AC4: 同 UUID u1 应以最新页覆盖为 Dead，got %v", byUUID["u1"].State)
	}
	// 新 UUID 追加
	if _, ok := byUUID["u3"]; !ok {
		t.Error("AC4: 新 UUID u3 应被追加")
	}
}

// ---------------------------------------------------------------------------
// 34.8-UNIT-007 (AC5, P0, RED) — 跨页进程树补全（核心验收点）
// ---------------------------------------------------------------------------
//
// 子进程在前页、父进程在后页：累积合并后用全集重建树，孤儿应被随后到达的父收养，
// 不残留 `[missing parent …]` synthetic 占位根。
//
// 注：树构建器（tree.BuildProcessTree）的孤儿容错本身已由 tree/builder_test.go 覆盖；
//     本测试只验证「累积补全新语义」——部分页 → 全集后 synthetic 消解，组合 merge helper
//     与既有 buildProcessTree wrapper，不重复测树构建本身（Duplicate Coverage Guard）。
//
// RED 来源：mergeAccumulatedProcs no-op 返回 page → 只剩 page2(父，无子) 或丢失子，
//   合并集不含完整父子 → 单独重建任一页都会产生孤儿/synthetic → 断言 FAIL。
func TestATDD_34_8_UNIT_007_CrossPageTree_SyntheticDissolved(t *testing.T) {
	now := time.Now()
	// page1：子进程 child（PPID=0 + ParentUUID → 走 builder.go:300 的跨页 UUID 关联分支）。
	// 注（dev 34.8 修正）：child.PID 必须非零。真实进程中 active 子进程 PID>0；historical
	// 子进程 PID=0 但 PPID 保留父原 PID（kernel/proc_query.go:522 仅清 PID 不清 PPID）——
	// 两种情形 PID 与 PPID 都不相等。原 ATDD 数据 child/parent 的 PID 均未设(=0)且 PPID=0，
	// 会先命中 builder.go:295 `p.PID == p.PPID`(0==0) 自引用根判定，短路 :300 的 ParentUUID
	// 收养逻辑（AC5 核心），与真实进程语义不符。改为真实 PID 以测真正的跨页 UUID 关联。
	page1 := []vfs.ProcInfo{
		{PID: 11, UUID: "child", ParentUUID: "parent", State: types.StateRunning, Intent: "child-proc", CreatedAt: now.Add(time.Minute)},
	}
	// page2：父进程 parent 到达（PID=10 顶层 active、PPID=0 → builder.go:309 提升为根）
	page2 := []vfs.ProcInfo{
		{PID: 10, UUID: "parent", PPID: 0, State: types.StateRunning, Intent: "parent-proc", CreatedAt: now},
	}

	merged := mergeAccumulatedProcs(page1, page2)
	roots := buildProcessTree(merged, 0, false)

	// 不得残留 missing-parent synthetic 占位根
	for _, r := range roots {
		if strings.Contains(r.Proc.Intent, "missing parent") {
			t.Fatalf("AC5: 累积全集重建后不应残留 synthetic 占位根，got root intent=%q", r.Proc.Intent)
		}
	}
	// 父子关系完整：parent 为根且收养 child
	var parent *treeNode
	for _, r := range roots {
		if r.Proc.UUID == "parent" {
			parent = r
		}
	}
	if parent == nil {
		t.Fatalf("AC5: parent 应作为根存在，roots=%d", len(roots))
	}
	childAdopted := false
	for _, c := range parent.Children {
		if c.Proc.UUID == "child" {
			childAdopted = true
		}
	}
	if !childAdopted {
		t.Errorf("AC5: child 应被随后到达的 parent 收养（跨页补全）")
	}
}

// ---------------------------------------------------------------------------
// 34.8-UNIT-008 (AC6, P0, RED) — 实时监控只吃 active 子集
// ---------------------------------------------------------------------------
//
// m.processes 改为累积全集后，实时监控消费者（detectSpawnExitEvents / detectBudgetEvents /
// computeHealthCounts）若直接吃累积全集会误判：历史 dead 进程虚增 E/W、active 跨页误报 SPAWN。
// monitorInputProcs 把累积全集收窄为 active 子集（排除 Dead；historical 在 kernel 侧 PID=0）。
//
// RED 来源：monitorInputProcs no-op 返回入参全集 → 含 Dead 条目 → FAIL。
func TestATDD_34_8_UNIT_008_MonitorInput_ActiveSubsetOnly(t *testing.T) {
	now := time.Now()
	accumulated := []vfs.ProcInfo{
		{PID: 11, UUID: "active-1", State: types.StateRunning, CreatedAt: now},
		{PID: 0, UUID: "hist-a", State: types.StateDead, CreatedAt: now.Add(-time.Hour)},
		{PID: 0, UUID: "hist-b", State: types.StateDead, CreatedAt: now.Add(-2 * time.Hour)},
		{PID: 12, UUID: "active-2", State: types.StateRunning, CreatedAt: now.Add(time.Minute)},
	}

	active := monitorInputProcs(accumulated)

	if len(active) != 2 {
		t.Fatalf("AC6: active 子集应为 2 条（active-1/active-2），got %d", len(active))
	}
	for _, p := range active {
		if p.State == types.StateDead {
			t.Errorf("AC6: active 子集不得含 Dead 进程，got UUID=%q", p.UUID)
		}
	}
}

// ---------------------------------------------------------------------------
// 34.8-UNIT-009 (AC6, P1, green-guard) — detectSpawnExitEvents trap-safe 契约
// ---------------------------------------------------------------------------
//
// 钉死「为何 active 子集喂入安全」的不变量：当一个 active PID 同时在 prev 和 curr 中
// （即正常存活、非新生），detectSpawnExitEvents 不得报 SPAWN。
// 这正是累积模式下「让监控只看恒含全部 active 的稳定子集」能避免误报的根据——
// 若改为吃逐页增长的累积全集、后续页带入的 active PID（prev 无）会被误判为 SPAWN。
//
// green-guard：当前即应 PASS；dev 实现 active 子集隔离后须保持。
func TestATDD_34_8_UNIT_009_DetectSpawnExit_NoFalseSpawn_TrapSafe(t *testing.T) {
	now := time.Now()
	p := vfs.ProcInfo{PID: 21, UUID: "stable", State: types.StateRunning, Intent: "stable-proc", CreatedAt: now}

	// prev 与 curr 都含同一存活 PID → 不应产生 SPAWN
	prev := map[types.PID]vfs.ProcInfo{p.PID: p}
	curr := []vfs.ProcInfo{p}

	events := detectSpawnExitEvents(prev, curr)
	for _, ev := range events {
		if ev.Type == EventSpawn {
			t.Errorf("AC6 trap-safe: PID %d 同在 prev&curr（存活）不应报 SPAWN，got %q", p.PID, ev.Summary)
		}
	}
}

// ---------------------------------------------------------------------------
// 34.8 Glue — fetchPagedProcs 多页循环累积（dashboard tick 接线，dev 补充）
// ---------------------------------------------------------------------------
//
// ATDD 阶段把 tick glue（调分页 + 滚动到底 Offset+=Limit + 累积）定为「编译期
// 校验 + 手工验证」。dev 补一条端到端自动化：经真实 socket round-trip 验证
// dashboard 侧 fetchPagedProcs 正确「循环拉取已加载页 + 按 UUID 累积 merge +
// 最近优先」。单页 ListAllProcsPaged round-trip 已由 ipc 层 INT-001/005 覆盖；
// 本测试只覆盖 dashboard 多页循环累积这一未被 ipc 层触及的 glue 语义。
func Test_34_8_Glue_FetchPagedProcs_MultiPageAccumulate(t *testing.T) {
	sockPath, kern := setupTestIPCServer(t)

	now := time.Now()
	// 5 条 historical，CreatedAt 严格递增：p0(最旧) … p4(最新)
	for i := range 5 {
		kern.ProcHistory().Add(vfs.ProcInfo{
			PID:       0,
			UUID:      "glue006-p" + string(rune('0'+i)),
			State:     types.StateDead,
			Intent:    "p" + string(rune('0'+i)),
			CreatedAt: now.Add(time.Duration(i) * time.Minute),
		})
	}

	client, err := ipc.Dial(sockPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()

	// PageSize=2 + LoadedPages=2 → 循环拉 page0(最新 {p4,p3}) + page1({p2,p1})
	// → 累积 4 条；total=5；hasMore=true（p0 尚未加载）。
	m := dashboardModel{client: client, procPaging: procPaging{PageSize: 2, LoadedPages: 2}}
	procs, total, hasMore, ferr := m.fetchPagedProcs()
	if ferr != nil {
		t.Fatalf("fetchPagedProcs: %v", ferr)
	}
	if total != 5 {
		t.Errorf("total = %d, want 5", total)
	}
	if !hasMore {
		t.Errorf("hasMore = false, want true（累积 4/5 条后仍有更旧的 p0）")
	}
	if len(procs) != 4 {
		t.Fatalf("两页累积应为 4 条，got %d", len(procs))
	}
	seen := make(map[string]bool, len(procs))
	for _, p := range procs {
		if seen[p.UUID] {
			t.Errorf("重复 UUID %q（累积应按 UUID 去重）", p.UUID)
		}
		seen[p.UUID] = true
	}
	// 最近优先：累积应含最新 4 条 {p4,p3,p2,p1}，不含最旧的 p0。
	if !seen["glue006-p4"] || !seen["glue006-p1"] || seen["glue006-p0"] {
		t.Errorf("最近优先累积应含 p4..p1、不含 p0，got %v", seen)
	}
}
