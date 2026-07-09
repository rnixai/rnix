package kernel

import (
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// =============================================================================
// ATDD 64.2 — procHistory 同 UUID 单快照·终态优先
//
// Story spec (64-2, 案卷 R3):
//   - 两个内存落史/装载口（reap.go cleanupExpiredDead + proc_query.go LoadHistory）
//     由 Add 改为 Upsert，使同一 UUID 只保留一条快照。
//   - ListAllProcs historical 段去重从 first-wins 改为比较式：终态(Dead)严格胜过
//     非终态；同级 last-wins；active 仍无条件优先；空 UUID 不去重。
//
// 双层正交回归网：
//   - AC3（机制级）：直接 Add 双条目 [running(旧), dead(新)] 绕过 LoadHistory →
//     ListAllProcs 须返回恰一条且 dead。锁定裁决 2（比较式去重）。修复前 first-wins
//     返回 running → FAIL。
//   - AC4（端到端级）：磁盘 suspended 快照 → LoadHistory（装载 suspended，豁免归一化）
//     → LoadSuspendedFromDisk（复活进 procTable）→ 推进到 Dead → cleanupExpiredDead
//     → ListAllProcs 该 UUID 恰一条且 dead，且 procHistory 内条目数==1。断言②锁定
//     裁决 1（Upsert 切换）；修复前双条目且 suspended 胜出 → FAIL。
//   - AC6：Upsert 契约首次直接单测（原地替换不增长且位置不变、append 超 cap 驱逐最旧、
//     空 UUID no-op）。
// =============================================================================

// TestATDD_64_2_010_HistoricalDoubleEntry_TerminalWins locks 裁决 2 (比较式去重).
// Directly injects two same-UUID history entries via Add — first a stale running
// snapshot, then a dead terminal snapshot — deliberately bypassing LoadHistory so
// the test reproduces the 64-1-era history shape (and any future Add leak path).
// ListAllProcs must return exactly one entry for that UUID, and it must be dead.
//
// RED on baseline: first-wins dedup returns the stale running entry.
func TestATDD_64_2_010_HistoricalDoubleEntry_TerminalWins(t *testing.T) {
	k := NewKernel(nil, nil, nil)
	defer k.Shutdown()

	const dupUUID = "dbl-entry-terminal-wins-0210"

	// 旧快照：running（装载/泄漏形态），CreatedAt 更早 → FIFO 序在前。
	k.procHistory.Add(vfs.ProcInfo{
		PID:        7001,
		UUID:       dupUUID,
		State:      types.StateRunning,
		Intent:     "stale-running-snapshot",
		TokensUsed: 100,
		CreatedAt:  time.Now().Add(-2 * time.Hour),
	})
	// 新快照：dead（reap 终态），FIFO 序在后。
	k.procHistory.Add(vfs.ProcInfo{
		PID:        7001,
		UUID:       dupUUID,
		State:      types.StateDead,
		ExitReason: "completed",
		Intent:     "reap-dead-snapshot",
		TokensUsed: 500,
		CreatedAt:  time.Now().Add(-2 * time.Hour),
	})

	all := k.ListAllProcs()

	count := 0
	var kept vfs.ProcInfo
	for _, p := range all {
		if p.UUID == dupUUID {
			count++
			kept = p
		}
	}
	if count != 1 {
		t.Fatalf("expected UUID %q to appear exactly once, got %d", dupUUID, count)
	}
	if kept.State != types.StateDead {
		t.Errorf("terminal (dead) must win over non-terminal (running): got State=%v", kept.State)
	}
	if kept.Intent != "reap-dead-snapshot" {
		t.Errorf("dead snapshot fields must win: got Intent=%q, want reap-dead-snapshot", kept.Intent)
	}
}

// TestATDD_64_2_011_HistoricalSameLevel_LastWins verifies that when two same-UUID
// entries share terminal-ness (both Dead), the later-written (FIFO-later) entry
// wins — the decisive rule for the 64-1 normalized-load case where the loaded
// entry (dead+interrupted, stale fields) and the reap entry (dead+real exit_reason,
// fresh fields) are BOTH Dead. Only last-wins lets the reap side's new facts win.
func TestATDD_64_2_011_HistoricalSameLevel_LastWins(t *testing.T) {
	k := NewKernel(nil, nil, nil)
	defer k.Shutdown()

	const dupUUID = "same-level-last-wins-0211"

	// 装载侧：dead + interrupted（64-1 归一化后旧字段）。
	k.procHistory.Add(vfs.ProcInfo{
		PID:        7002,
		UUID:       dupUUID,
		State:      types.StateDead,
		ExitReason: exitReasonInterrupted,
		Intent:     "loaded-normalized",
		TokensUsed: 10,
		CreatedAt:  time.Now().Add(-time.Hour),
	})
	// reap 侧：dead + 真实 exit_reason（新字段）。
	k.procHistory.Add(vfs.ProcInfo{
		PID:        7002,
		UUID:       dupUUID,
		State:      types.StateDead,
		ExitReason: "completed",
		Intent:     "reap-real-exit",
		TokensUsed: 999,
		CreatedAt:  time.Now().Add(-time.Hour),
	})

	all := k.ListAllProcs()

	count := 0
	var kept vfs.ProcInfo
	for _, p := range all {
		if p.UUID == dupUUID {
			count++
			kept = p
		}
	}
	if count != 1 {
		t.Fatalf("expected UUID %q exactly once, got %d", dupUUID, count)
	}
	if kept.ExitReason != "completed" || kept.Intent != "reap-real-exit" {
		t.Errorf("same-level last-wins: expected reap side to win, got ExitReason=%q Intent=%q",
			kept.ExitReason, kept.Intent)
	}
}

// TestATDD_64_2_012_TerminalNotReplacedByNonTerminal verifies rule 3: once a dead
// entry is in history for a UUID, a later non-terminal (running) entry for the same
// UUID does NOT replace it. Encodes the state-machine invariant "Dead → any state is
// illegal" defensively, independent of the Upsert fix.
func TestATDD_64_2_012_TerminalNotReplacedByNonTerminal(t *testing.T) {
	k := NewKernel(nil, nil, nil)
	defer k.Shutdown()

	const dupUUID = "terminal-not-replaced-0212"

	k.procHistory.Add(vfs.ProcInfo{
		PID:        7003,
		UUID:       dupUUID,
		State:      types.StateDead,
		ExitReason: "completed",
		Intent:     "dead-first",
		CreatedAt:  time.Now().Add(-time.Hour),
	})
	// 之后泄漏一条 stale running（如 crash-recovery 重放）——不应取代 dead。
	k.procHistory.Add(vfs.ProcInfo{
		PID:       7003,
		UUID:      dupUUID,
		State:     types.StateRunning,
		Intent:    "stale-running-after-dead",
		CreatedAt: time.Now().Add(-time.Hour),
	})

	all := k.ListAllProcs()

	count := 0
	var kept vfs.ProcInfo
	for _, p := range all {
		if p.UUID == dupUUID {
			count++
			kept = p
		}
	}
	if count != 1 {
		t.Fatalf("expected UUID %q exactly once, got %d", dupUUID, count)
	}
	if kept.State != types.StateDead || kept.Intent != "dead-first" {
		t.Errorf("dead must not be replaced by later non-terminal: got State=%v Intent=%q",
			kept.State, kept.Intent)
	}
}

// TestATDD_64_2_020_EndToEnd_SuspendedRevive_SingleSnapshot locks 裁决 1 (Upsert
// at cleanupExpiredDead + LoadHistory) via exposure-face-A:
//
//	disk suspended fixture → LoadHistory (loads suspended into procHistory, exempt
//	from 64-1 normalization) → LoadSuspendedFromDisk (revives placeholder into
//	procTable, does NOT clean procHistory) → advance placeholder to Dead →
//	cleanupExpiredDead(0) → assert:
//	  ① ListAllProcs returns exactly one entry for the UUID and it is dead;
//	  ② procHistory holds exactly ONE entry for that UUID.
//
// Assertion ② is the discriminating gate for 裁决 1: with Add (baseline), reap
// appends a second entry alongside the loaded suspended one → count==2 → FAIL.
// With Upsert, reap replaces the suspended entry in place → count==1.
func TestATDD_64_2_020_EndToEnd_SuspendedRevive_SingleSnapshot(t *testing.T) {
	k, base := newReloadKernel(t)
	uuid := uuidForTest("e2e0220")

	// 磁盘 suspended 快照 fixture（含 steps.jsonl + process-meta.json 满足复活路径）。
	writeSuspendProcInfoFixture(t, base, suspendDiskInfo{
		PID:           55,
		UUID:          uuid,
		State:         "suspended",
		Intent:        "suspended-then-completed",
		Provider:      "claude",
		Model:         "claude-opus-4-7",
		MaxSteps:      100,
		CreatedAt:     staticTime(t, -2*time.Hour).Format(time.RFC3339Nano),
		CtxID:         1,
		ContextWindow: 100000,
		SuspendReason: "user_paused",
		PausedAt:      staticTime(t, -30*time.Minute).Format(time.RFC3339Nano),
		IsPaused:      true,
	}, true /*withStepsJSONL*/, true /*withMeta*/)

	// 1) LoadHistory 装载 suspended 条目进 procHistory（suspended 豁免归一化）。
	if err := k.LoadHistory(); err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	if got := countHistoryEntries64_2(k, uuid); got != 1 {
		t.Fatalf("after LoadHistory: expected 1 history entry for %s, got %d", uuid, got)
	}

	// 2) LoadSuspendedFromDisk 复活进 procTable（不清 procHistory —— 主暴露面）。
	loaded, err := k.LoadSuspendedFromDisk()
	if err != nil {
		t.Fatalf("LoadSuspendedFromDisk: %v", err)
	}
	if loaded < 1 {
		t.Fatalf("LoadSuspendedFromDisk loaded=%d, want >=1", loaded)
	}
	proc, ok := k.GetProcessByUUID(uuid)
	if !ok {
		t.Fatalf("UUID %s not revived into procTable", uuid)
	}

	// 3) 将复活的进程推进到 Dead（模拟完成），DeadAt 过 TTL。
	proc.mu.Lock()
	proc.State = types.StateDead
	proc.DeadAt = time.Now().Add(-time.Hour)
	proc.Exit = &ExitStatus{Code: 0, Reason: "completed"}
	proc.mu.Unlock()

	// 4) 直接调 cleanupExpiredDead(0) 立即落史。
	k.cleanupExpiredDead(0)

	// --- 断言 ① ListAllProcs 该 UUID 恰一条且 dead ---
	all := k.ListAllProcs()
	count := 0
	var kept vfs.ProcInfo
	for _, p := range all {
		if p.UUID == uuid {
			count++
			kept = p
		}
	}
	if count != 1 {
		t.Fatalf("ListAllProcs: expected UUID %s exactly once, got %d", uuid, count)
	}
	if kept.State != types.StateDead {
		t.Errorf("ListAllProcs: expected dead terminal snapshot, got State=%v", kept.State)
	}

	// --- 断言 ② procHistory 内该 UUID 条目数 == 1（锁定 Upsert）---
	if got := countHistoryEntries64_2(k, uuid); got != 1 {
		t.Errorf("procHistory must hold exactly 1 entry for %s after reap (Upsert), got %d", uuid, got)
	}
}

// countHistoryEntries64_2 counts how many entries in procHistory carry the given
// UUID — the explicit positive count assertion 64-1 review P7 demands (do not
// rely solely on ListAllProcs output, which dedups).
func countHistoryEntries64_2(k *KernelImpl, uuid string) int {
	n := 0
	for _, e := range k.procHistory.List() {
		if e.UUID == uuid {
			n++
		}
	}
	return n
}

// TestATDD_64_2_030_Upsert_InPlaceReplace_PositionStable locks the Upsert contract
// (AC6): a same-UUID Upsert replaces in place without growing Len and without
// moving the entry's FIFO position — the precondition ListAllProcs's stable sort
// depends on ("procHistory.List() preserves FIFO insertion order").
func TestATDD_64_2_030_Upsert_InPlaceReplace_PositionStable(t *testing.T) {
	h := NewProcessHistory(10)
	h.Add(vfs.ProcInfo{UUID: "A", Intent: "a0"})
	h.Add(vfs.ProcInfo{UUID: "B", Intent: "b0"})
	h.Add(vfs.ProcInfo{UUID: "C", Intent: "c0"})

	h.Upsert(vfs.ProcInfo{UUID: "B", Intent: "b1", TokensUsed: 42})

	if got := h.Len(); got != 3 {
		t.Fatalf("Upsert of existing UUID must not grow Len: got %d, want 3", got)
	}
	list := h.List()
	if len(list) != 3 {
		t.Fatalf("List len = %d, want 3", len(list))
	}
	// 位置不变：A, B(替换), C。
	wantOrder := []string{"A", "B", "C"}
	for i, want := range wantOrder {
		if list[i].UUID != want {
			t.Errorf("position %d: UUID=%q, want %q (FIFO position must be stable)", i, list[i].UUID, want)
		}
	}
	if list[1].Intent != "b1" || list[1].TokensUsed != 42 {
		t.Errorf("in-place entry must carry new fields: got Intent=%q Tokens=%d, want b1/42",
			list[1].Intent, list[1].TokensUsed)
	}
}

// TestATDD_64_2_031_Upsert_AppendEvictsOldest verifies the FIFO cap contract
// (Story 56.6 doc comment, now under direct test): Upsert with no matching UUID
// appends, and when over cap evicts the oldest — identical to Add.
func TestATDD_64_2_031_Upsert_AppendEvictsOldest(t *testing.T) {
	h := NewProcessHistory(2)
	h.Upsert(vfs.ProcInfo{UUID: "X", Intent: "x"})
	h.Upsert(vfs.ProcInfo{UUID: "Y", Intent: "y"})
	h.Upsert(vfs.ProcInfo{UUID: "Z", Intent: "z"}) // no match → append → over cap → evict X

	if got := h.Len(); got != 2 {
		t.Fatalf("Len = %d, want 2 (FIFO cap)", got)
	}
	list := h.List()
	if list[0].UUID != "Y" || list[1].UUID != "Z" {
		t.Errorf("expected oldest (X) evicted, got [%s, %s], want [Y, Z]", list[0].UUID, list[1].UUID)
	}
}

// TestATDD_64_2_032_Upsert_EmptyUUID_NoOp verifies the empty-UUID no-op guard.
func TestATDD_64_2_032_Upsert_EmptyUUID_NoOp(t *testing.T) {
	h := NewProcessHistory(10)
	h.Add(vfs.ProcInfo{UUID: "A", Intent: "a"})
	before := h.Len()

	h.Upsert(vfs.ProcInfo{UUID: "", Intent: "should-be-ignored"})

	if got := h.Len(); got != before {
		t.Errorf("empty-UUID Upsert must be a no-op: Len %d → %d", before, got)
	}
}

// TestATDD_64_2_033_Upsert_TerminalGuard verifies the storage-layer terminal
// guard (code-review patch): a stored Dead snapshot is never replaced by a
// non-terminal one. This closes the LoadHistory cross-baseDir exposure — a
// stale suspended copy in a later-scanned baseDir (data-migration leftover)
// must not erase real exit facts, mirroring shouldReplaceHistoryEntry rule ③
// at the storage layer. The forward direction (non-terminal → Dead finalize,
// e.g. CLI-subagent Running → Dead) must keep working.
func TestATDD_64_2_033_Upsert_TerminalGuard(t *testing.T) {
	h := NewProcessHistory(10)

	// Dead 不被非终态取代（stale suspended 覆盖方向被拒）。
	h.Upsert(vfs.ProcInfo{UUID: "A", State: types.StateDead, ExitReason: "completed"})
	h.Upsert(vfs.ProcInfo{UUID: "A", State: types.StateSuspended, Intent: "stale-copy"})
	if got := h.Len(); got != 1 {
		t.Fatalf("Len = %d, want 1", got)
	}
	if e := h.List()[0]; e.State != types.StateDead || e.ExitReason != "completed" {
		t.Errorf("Dead snapshot must survive non-terminal Upsert: got State=%v ExitReason=%q",
			e.State, e.ExitReason)
	}

	// 正向 finalize（Running → Dead）不受守卫影响。
	h.Upsert(vfs.ProcInfo{UUID: "B", State: types.StateRunning})
	h.Upsert(vfs.ProcInfo{UUID: "B", State: types.StateDead, ExitReason: "completed"})
	list := h.List()
	if len(list) != 2 {
		t.Fatalf("Len = %d, want 2", len(list))
	}
	if list[1].UUID != "B" || list[1].State != types.StateDead {
		t.Errorf("Running → Dead finalize must replace: got UUID=%s State=%v",
			list[1].UUID, list[1].State)
	}

	// 同级非终态 last-wins 不受守卫影响（suspended → running 仍替换）。
	h.Upsert(vfs.ProcInfo{UUID: "C", State: types.StateSuspended})
	h.Upsert(vfs.ProcInfo{UUID: "C", State: types.StateRunning, Intent: "newer"})
	list = h.List()
	if list[2].State != types.StateRunning || list[2].Intent != "newer" {
		t.Errorf("same-level non-terminal Upsert must still replace: got State=%v Intent=%q",
			list[2].State, list[2].Intent)
	}
}
