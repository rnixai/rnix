package ipc

// =============================================================================
// ATDD Story 34.8: ListAllProcs 分页 + scanner buffer 兜底（IPC 层）
// =============================================================================
//
// Test Strategy（详见 _bmad-output/test-artifacts/atdd-checklist-34-8-*.md）:
//   - 红灯机制 [[atdd-code-story-red-mechanism-preference]]：RED 用骨架 + t.Skip
//     保 ATDD 提交期 make all 全绿；dev 移 skip 填实现验 RED→GREEN。
//   - green-guard 回归红线不 skip：当前即应 PASS，dev 改动后须保持（GREEN-stays-GREEN）。
//
// 本文件覆盖：
//   34.8-INT-001 (AC1, P0, RED)         分页 round-trip + 元数据(total/hasMore)
//   34.8-INT-002 (AC2, P0, RED)         「最近优先」语义 + 超界不 panic + Limit≤0=全量
//   34.8-INT-003 (AC8, P0, green-guard) 无参 ListAllProcs() 全量路径不回归（dedup/排序/PID=0 经 wire 保持）
//   34.8-UNIT-004(AC3/7,P0, RED)        scanner buffer 兜底：>1 MB 单条响应完整解析
//   34.8-INT-005 (AC7, P1, RED)         单页 wire < 1 MB（server 端真分页）
//
// 注入范式（已核实既有测试惯例）：
//   - active：kernel.NewProcess + proc.Start() + srv.kern.AddProcess(proc)
//   - historical：srv.kern.ProcHistory().Add(vfs.ProcInfo{PID:0, State:Dead, CreatedAt: ...})
//     —— CreatedAt 完全可控，是「最近优先」排序断言的确定性来源；ListAllProcs 对
//     active+historical 并集按 CreatedAt 升序排序，分页逻辑只关心序，与 active/historical 无关。

import (
	"strings"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/kernel"
	"github.com/rnixai/rnix/vfs"
)

// mustStartProc 构造一个 Running 的 active 进程（UUID/CreatedAt 可控）。
// State 由 mu 守护、ipc 包无法直接设，故经 NewProcess + Start()（→ Running）；
// CreatedAt 是导出字段可直接赋值，供「最近优先」/排序断言用确定性时间。
func mustStartProc(t *testing.T, intent, uuid string, createdAt time.Time) *kernel.Process {
	t.Helper()
	p := kernel.NewProcess(0, intent, nil)
	p.UUID = uuid
	p.CreatedAt = createdAt
	if err := p.Start(); err != nil {
		t.Fatalf("proc.Start: %v", err)
	}
	return p
}

// ---------------------------------------------------------------------------
// 34.8-INT-001 (AC1, P0, RED) — 分页 round-trip + 元数据
// ---------------------------------------------------------------------------
//
// Given 守护进程持有 5 条进程
// When  client.ListAllProcsPaged(0, 2) 经 socket round-trip
// Then  返回恰好 2 条 + total=5 + hasMore=true（offset/limit 真正抵达 handler 并切片）。
//
// RED 来源：ListAllProcsPaged 当前为 no-op 骨架返回 (nil,0,false,nil)
//   → len=0≠2 / total=0≠5 / hasMore=false≠true 三项全 FAIL。
func TestATDD_34_8_INT_001_Paged_Roundtrip_Metadata(t *testing.T) {

	client, srv, _ := setupClientTest(t)

	now := time.Now()
	for i := range 5 {
		srv.kern.ProcHistory().Add(vfs.ProcInfo{
			PID:       0,
			UUID:      "int001-" + string(rune('a'+i)),
			State:     types.StateDead,
			Intent:    "proc",
			CreatedAt: now.Add(time.Duration(i) * time.Minute),
		})
	}

	procs, total, hasMore, err := client.ListAllProcsPaged(0, 2)
	if err != nil {
		t.Fatalf("ListAllProcsPaged: %v", err)
	}
	if len(procs) != 2 {
		t.Errorf("AC1: page size = %d, want 2", len(procs))
	}
	if total != 5 {
		t.Errorf("AC1: total = %d, want 5 (去重后总条数)", total)
	}
	if !hasMore {
		t.Errorf("AC1: hasMore = false, want true (5 条取 2 条后仍有下一页)")
	}
}

// ---------------------------------------------------------------------------
// 34.8-INT-002 (AC2, P0, RED) — 「最近优先」+ 超界不 panic + Limit≤0=全量
// ---------------------------------------------------------------------------
//
// 分页方向 = 最近优先：ListAllProcs 结果按 CreatedAt 升序（oldest first），
// dashboard 最关心最新进程，故 Offset=0 应返回「最新」Limit 条（对升序结果从尾部倒数）。
//
// RED 来源：no-op 骨架返回 (nil,0,false)：
//   - 「最新 2 条」断言：返回 nil → FAIL
//   - 「Limit≤0=全量」断言：返回 0 条 ≠ 5 → FAIL
//   - 超界子例：骨架恰好返回空切片 + hasMore=false → 可能平凡 PASS，
//     交付前必须移 skip 实跑确认「真分页实现下仍 PASS」而非依赖骨架巧合（54-5 假 RED 教训）。
func TestATDD_34_8_INT_002_RecentFirst_OutOfRange_AllWhenLimitZero(t *testing.T) {

	client, srv, _ := setupClientTest(t)

	now := time.Now()
	// 5 条，CreatedAt 严格递增：p0(最旧) … p4(最新)
	for i := range 5 {
		srv.kern.ProcHistory().Add(vfs.ProcInfo{
			PID:       0,
			UUID:      "int002-p" + string(rune('0'+i)),
			State:     types.StateDead,
			Intent:    "p" + string(rune('0'+i)),
			CreatedAt: now.Add(time.Duration(i) * time.Minute),
		})
	}

	t.Run("offset 0 returns most recent Limit", func(t *testing.T) {
		procs, total, hasMore, err := client.ListAllProcsPaged(0, 2)
		if err != nil {
			t.Fatalf("ListAllProcsPaged: %v", err)
		}
		if total != 5 {
			t.Errorf("total = %d, want 5", total)
		}
		if !hasMore {
			t.Errorf("hasMore = false, want true")
		}
		if len(procs) != 2 {
			t.Fatalf("page size = %d, want 2", len(procs))
		}
		// 最近优先：应是 p4(最新) 与 p3，顺序由 dev 在 dev notes 固化；此处断言「这两条 UUID 在集合内」。
		gotUUIDs := map[string]bool{procs[0].UUID: true, procs[1].UUID: true}
		if !gotUUIDs["int002-p4"] || !gotUUIDs["int002-p3"] {
			t.Errorf("offset 0 应返回最新 2 条 {p4,p3}，got %q,%q", procs[0].UUID, procs[1].UUID)
		}
	})

	t.Run("offset out of range returns empty no panic", func(t *testing.T) {
		procs, _, hasMore, err := client.ListAllProcsPaged(100, 2)
		if err != nil {
			t.Fatalf("超界 ListAllProcsPaged 不应返回 err: %v", err)
		}
		if len(procs) != 0 {
			t.Errorf("超界应返回空切片，got %d 条", len(procs))
		}
		if hasMore {
			t.Errorf("超界 hasMore 应为 false")
		}
	})

	t.Run("limit <= 0 returns all", func(t *testing.T) {
		procs, _, _, err := client.ListAllProcsPaged(0, 0)
		if err != nil {
			t.Fatalf("ListAllProcsPaged(0,0): %v", err)
		}
		if len(procs) != 5 {
			t.Errorf("Limit<=0 应返回全量 5 条，got %d", len(procs))
		}
	})
}

// ---------------------------------------------------------------------------
// 34.8-INT-003 (AC8, P0, green-guard) — 无参全量路径不回归
// ---------------------------------------------------------------------------
//
// 无参 ListAllProcs()（5 个全量调用方共用）必须保持：active+historical 并集、
// 同 UUID 去重（active 胜 historical）、CreatedAt 升序、historical PID=0，且经 wire 完整保持。
// nil-payload 抵达 handler 时 lenient 回落全量（向后兼容）。
//
// green-guard：当前即应 PASS；dev 给 handleListAllProcs 加 payload 解析后，nil/空 payload
// 仍须回落全量，本测试钉死该向后兼容契约。
func TestATDD_34_8_INT_003_FullList_BackwardCompat_NoRegression(t *testing.T) {
	client, srv, _ := setupClientTest(t)

	const dupUUID = "int003-dup"
	now := time.Now()

	// active：UUID=dupUUID，Running
	active := mustStartProc(t, "active-wins", dupUUID, now)
	srv.kern.AddProcess(active)

	// historical 同 UUID（模拟漏清）—— 应被 active 去重盖掉
	srv.kern.ProcHistory().Add(vfs.ProcInfo{
		PID:       9999,
		UUID:      dupUUID,
		State:     types.StateDead,
		Intent:    "stale-history",
		CreatedAt: now.Add(-time.Hour),
	})
	// historical 独立 UUID，更旧 —— 应保留，PID 经 wire 必须为 0
	srv.kern.ProcHistory().Add(vfs.ProcInfo{
		PID:       8888,
		UUID:      "int003-hist-only",
		State:     types.StateDead,
		Intent:    "older-history",
		CreatedAt: now.Add(-2 * time.Hour),
	})

	all, err := client.ListAllProcs()
	if err != nil {
		t.Fatalf("ListAllProcs (无参全量): %v", err)
	}

	// 去重：dupUUID 恰好一条，且为 active（Running / intent=active-wins）
	dupCount := 0
	var kept vfs.ProcInfo
	for _, p := range all {
		if p.UUID == dupUUID {
			dupCount++
			kept = p
		}
	}
	if dupCount != 1 {
		t.Fatalf("AC8: dupUUID 去重后应恰好 1 条，got %d", dupCount)
	}
	if kept.State != types.StateRunning || kept.Intent != "active-wins" {
		t.Errorf("AC8: active 应胜 historical，got state=%v intent=%q", kept.State, kept.Intent)
	}

	// historical-only：保留且 PID=0（经 wire 保持）
	foundHist := false
	for _, p := range all {
		if p.UUID == "int003-hist-only" {
			foundHist = true
			if p.PID != 0 {
				t.Errorf("AC8: historical PID 经 wire 应为 0，got %d", p.PID)
			}
		}
	}
	if !foundHist {
		t.Error("AC8: historical-only 进程在全量结果中缺失")
	}

	// CreatedAt 升序（oldest first）
	for i := 1; i < len(all); i++ {
		if all[i].CreatedAt.Before(all[i-1].CreatedAt) {
			t.Errorf("AC8: 结果应按 CreatedAt 升序，位置 %d 早于 %d", i, i-1)
		}
	}
}

// ---------------------------------------------------------------------------
// 34.8-UNIT-004 (AC3/AC7, P0, RED) — scanner buffer 兜底：>1 MB 单条响应
// ---------------------------------------------------------------------------
//
// 注入一条 Intent≈1.3 MB 的 historical 进程，使无参 ListAllProcs() 的单行 NDJSON 响应
// 越过现有 1 MB（1<<20）scanner buffer 上限。
//
// RED 来源：现 client.go:34 `scanner.Buffer(make([]byte,0,1<<20), 1<<20)` → readResponse 的
//   c.scanner.Scan() 在 >1 MB 行上返回 false 且 Err()==bufio.ErrTooLong → ListAllProcs 返回
//   "ipc: read response: token too long" → err != nil → FAIL。
//   dev 调高 buffer（如 16 MB）或改 bufio.Reader.ReadBytes('\n') 后完整解析 → PASS。
//
// 注：此测试同时是 AC8「buffer 兜底同时保护全量调用方」的回归依据——它走的正是无参全量路径。
func TestATDD_34_8_UNIT_004_ScannerBuffer_OversizeResponse(t *testing.T) {

	client, srv, _ := setupClientTest(t)

	bigIntent := strings.Repeat("X", 1300*1024) // ≈1.3 MB，单条即越 1 MB 帧上限
	srv.kern.ProcHistory().Add(vfs.ProcInfo{
		PID:       0,
		UUID:      "unit004-oversize",
		State:     types.StateDead,
		Intent:    bigIntent,
		CreatedAt: time.Now(),
	})

	all, err := client.ListAllProcs()
	if err != nil {
		t.Fatalf("AC3: >1 MB 响应应被完整解析（buffer 兜底），got err: %v", err)
	}
	found := false
	for _, p := range all {
		if p.UUID == "unit004-oversize" {
			found = true
			if len(p.Intent) != len(bigIntent) {
				t.Errorf("AC3: Intent 长度 = %d, want %d（不得截断）", len(p.Intent), len(bigIntent))
			}
		}
	}
	if !found {
		t.Error("AC3: 超限响应解析后未找到注入进程")
	}
}

// ---------------------------------------------------------------------------
// 34.8-INT-005 (AC7, P1, RED) — 单页 wire < 1 MB（server 端真分页）
// ---------------------------------------------------------------------------
//
// 注入 50 条 historical，各 Result≈30 KB（全量 wire ≈1.5 MB 必越限）。
// ListAllProcsPaged(0, 10) 必须只返回 10 条（server 端切片，而非客户端裁剪全量），
// 从而单页响应远低于 1 MB。
//
// RED 来源：no-op 骨架返回 0 条 ≠ 10 → FAIL。real 实现须在 server 端先切片再 marshal。
func TestATDD_34_8_INT_005_SinglePageUnderOneMB(t *testing.T) {

	client, srv, _ := setupClientTest(t)

	bigResult := strings.Repeat("R", 30*1024) // ≈30 KB/条
	now := time.Now()
	for i := range 50 {
		srv.kern.ProcHistory().Add(vfs.ProcInfo{
			PID:       0,
			UUID:      "int005-" + string(rune('A'+i%26)) + string(rune('0'+i/26)),
			State:     types.StateDead,
			Intent:    "heavy",
			Result:    bigResult,
			CreatedAt: now.Add(time.Duration(i) * time.Second),
		})
	}

	procs, total, hasMore, err := client.ListAllProcsPaged(0, 10)
	if err != nil {
		t.Fatalf("ListAllProcsPaged: %v", err)
	}
	if total != 50 {
		t.Errorf("AC7: total = %d, want 50", total)
	}
	if !hasMore {
		t.Errorf("AC7: hasMore 应为 true（50 取 10 后仍有更多）")
	}
	if len(procs) != 10 {
		t.Errorf("AC7: 单页应恰好 10 条（server 端切片），got %d —— 若得 50 则说明客户端裁剪全量、传输未瘦身", len(procs))
	}
}
