package kernel

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// =============================================================================
// ATDD 64.1 — daemon 启动历史状态归一化（liveness reconciliation）
//
// Story spec (64-1):
//   - LoadHistory 装载路径对 state ∈ {created, running, zombie} 的磁盘快照归一化
//     为 dead：dead_at 空补 proc-info.json mtime（stat 失败 fallback 装载时刻）；
//     exit_reason 空置 "interrupted"、非空保留原值、任何分支不写 "completed"；
//     suspend_reason 清空；产物过 ValidateProcInfoInvariant。
//   - 归一化结果回写 proc-info.json（复用 SaveProcInfo，仅变更条目，best-effort）；
//     二次 LoadHistory 零回写（幂等）。
//   - 豁免：suspended 不归一化；UUID 在 procTable 中跳过。
//   - 归一化不破坏 resumability：条目仍在 ListResumable；zombie(context_full) 的
//     ExitReason 保留使 resume.go compaction 检测路径不变。
//   - synthetic running 样本同样被归一化。
//
// Fixtures are constructed via SaveProcInfo (real disk schema) + a manual
// steps.jsonl companion so ListResumable's filter is satisfied. This exercises
// the exact procInfoToDisk/procInfoFromDisk round-trip production uses.
// =============================================================================

// write64_1Fixture writes a proc-info.json (via the real SaveProcInfo path) plus
// a placeholder steps.jsonl into <baseDir>/steps/<uuid>/, so both LoadHistory
// and ListResumable observe the entry. Returns the proc-info.json path so
// callers can os.Chtimes it for mtime-based dead_at assertions.
func write64_1Fixture(t *testing.T, baseDir string, info vfs.ProcInfo) string {
	t.Helper()
	if info.UUID == "" {
		t.Fatal("write64_1Fixture: empty UUID")
	}
	if err := SaveProcInfo(baseDir, info); err != nil {
		t.Fatalf("SaveProcInfo(%s): %v", info.UUID, err)
	}
	dir := filepath.Join(baseDir, "steps", info.UUID)
	if err := os.WriteFile(filepath.Join(dir, "steps.jsonl"), []byte(`{"step":1,"messages":[]}`+"\n"), 0o644); err != nil {
		t.Fatalf("write steps.jsonl: %v", err)
	}
	return filepath.Join(dir, procInfoFilename)
}

// readDiskState64_1 reads the raw JSON of a fixture back so tests can assert on
// the persisted (回写) state independently of the in-memory ring buffer.
func readDiskState64_1(t *testing.T, baseDir, uuid string) map[string]any {
	t.Helper()
	path := filepath.Join(baseDir, "steps", uuid, procInfoFilename)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal %s: %v", path, err)
	}
	return raw
}

// TestATDD_64_1_010_FourStateMatrix_MemoryAndDisk exercises the four on-disk
// state fixtures (running / created / zombie / suspended) through a single
// LoadHistory and asserts both the in-memory history view and the on-disk
// (回写) state. running/created/zombie → dead; suspended untouched (AC1/AC2/AC6).
func TestATDD_64_1_010_FourStateMatrix_MemoryAndDisk(t *testing.T) {
	k, base := newReloadKernel(t)

	runUUID := uuidForTest("run0110")
	creUUID := uuidForTest("cre0110")
	zomUUID := uuidForTest("zom0110")
	susUUID := uuidForTest("sus0110")

	created := staticTime(t, -3*time.Hour)

	// running: exit_reason 必空（invariant）→ 归一化后补 interrupted。
	write64_1Fixture(t, base, vfs.ProcInfo{
		PID: 11, UUID: runUUID, State: types.StateRunning,
		Intent: "running leftover", Provider: "claude", CreatedAt: created,
	})
	// created: 同 running 语义。
	write64_1Fixture(t, base, vfs.ProcInfo{
		PID: 12, UUID: creUUID, State: types.StateCreated,
		Intent: "created leftover", Provider: "claude", CreatedAt: created,
	})
	// zombie: exit_reason 非空（invariant 要求）→ 保留原值。
	write64_1Fixture(t, base, vfs.ProcInfo{
		PID: 13, UUID: zomUUID, State: types.StateZombie, ExitReason: "killed",
		Intent: "zombie leftover", Provider: "claude", CreatedAt: created,
	})
	// suspended: 合法冻结态 → 完全不动。
	write64_1Fixture(t, base, vfs.ProcInfo{
		PID: 14, UUID: susUUID, State: types.StateSuspended, SuspendReason: "user_paused",
		Intent: "suspended live", Provider: "claude", CreatedAt: created,
	})

	if err := k.LoadHistory(); err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}

	// --- memory view assertions ---
	assertMemState64_1 := func(uuid string, want types.ProcessState, wantExit string) {
		t.Helper()
		info := k.FindHistoryByUUID(uuid)
		if info == nil {
			t.Fatalf("uuid %s missing from in-memory history", uuid)
		}
		if info.State != want {
			t.Errorf("mem[%s].State = %s, want %s", uuid, info.State, want)
		}
		if info.ExitReason != wantExit {
			t.Errorf("mem[%s].ExitReason = %q, want %q", uuid, info.ExitReason, wantExit)
		}
		if info.State == types.StateDead {
			if info.SuspendReason != "" {
				t.Errorf("mem[%s] Dead must have empty SuspendReason, got %q", uuid, info.SuspendReason)
			}
			if err := ValidateProcInfoInvariant(info); err != nil {
				t.Errorf("mem[%s] fails ValidateProcInfoInvariant: %v", uuid, err)
			}
		}
	}
	assertMemState64_1(runUUID, types.StateDead, exitReasonInterrupted)
	assertMemState64_1(creUUID, types.StateDead, exitReasonInterrupted)
	assertMemState64_1(zomUUID, types.StateDead, "killed") // 保留原值
	assertMemState64_1(susUUID, types.StateSuspended, "")  // 未归一化

	// --- disk (回写) view assertions ---
	assertDiskState64_1 := func(uuid, wantState, wantExit string) {
		t.Helper()
		raw := readDiskState64_1(t, base, uuid)
		if got, _ := raw["state"].(string); got != wantState {
			t.Errorf("disk[%s].state = %q, want %q", uuid, got, wantState)
		}
		gotExit, _ := raw["exit_reason"].(string)
		if gotExit != wantExit {
			t.Errorf("disk[%s].exit_reason = %q, want %q", uuid, gotExit, wantExit)
		}
		if gotExit == "completed" {
			t.Errorf("disk[%s].exit_reason must never be fabricated as completed", uuid)
		}
	}
	assertDiskState64_1(runUUID, "dead", exitReasonInterrupted)
	assertDiskState64_1(creUUID, "dead", exitReasonInterrupted)
	assertDiskState64_1(zomUUID, "dead", "killed")
	// suspended 回写不应发生：state 保持 suspended、suspend_reason 保留。
	susRaw := readDiskState64_1(t, base, susUUID)
	if got, _ := susRaw["state"].(string); got != "suspended" {
		t.Errorf("disk[suspended].state = %q, want suspended (must not be normalized)", got)
	}
	if got, _ := susRaw["suspend_reason"].(string); got != "user_paused" {
		t.Errorf("disk[suspended].suspend_reason = %q, want user_paused", got)
	}
}

// TestATDD_64_1_020_DeadAtFromMtime asserts dead_at 空时补 proc-info.json 的
// mtime（裁决 3），用 os.Chtimes 精确控制 mtime。
func TestATDD_64_1_020_DeadAtFromMtime(t *testing.T) {
	k, base := newReloadKernel(t)
	uuid := uuidForTest("mtim020")

	path := write64_1Fixture(t, base, vfs.ProcInfo{
		PID: 21, UUID: uuid, State: types.StateRunning,
		Intent: "mtime test", Provider: "claude", CreatedAt: staticTime(t, -5*time.Hour),
	})

	// 冻结 mtime 到一个确定时刻（截到秒，规避文件系统 mtime 精度差异）。
	wantMtime := staticTime(t, -90*time.Minute).Truncate(time.Second)
	if err := os.Chtimes(path, wantMtime, wantMtime); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	if err := k.LoadHistory(); err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}

	info := k.FindHistoryByUUID(uuid)
	if info == nil {
		t.Fatalf("uuid %s missing from history", uuid)
	}
	if info.DeadAt.IsZero() {
		t.Fatal("DeadAt must be non-empty after normalization")
	}
	if !info.DeadAt.Truncate(time.Second).Equal(wantMtime) {
		t.Errorf("DeadAt = %v, want mtime %v", info.DeadAt, wantMtime)
	}
}

// TestATDD_64_1_021_PreexistingDeadAtPreserved asserts 磁盘已有非空 dead_at 的脏
// 非终态条目只改 state/exit_reason，保留原 dead_at（裁决 3）。
func TestATDD_64_1_021_PreexistingDeadAtPreserved(t *testing.T) {
	k, base := newReloadKernel(t)
	uuid := uuidForTest("ddat021")

	origDead := staticTime(t, -10*time.Minute)
	write64_1Fixture(t, base, vfs.ProcInfo{
		PID: 22, UUID: uuid, State: types.StateRunning,
		Intent: "dirty dead_at", Provider: "claude",
		CreatedAt: staticTime(t, -2*time.Hour), DeadAt: origDead,
	})

	if err := k.LoadHistory(); err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	info := k.FindHistoryByUUID(uuid)
	if info == nil {
		t.Fatalf("uuid %s missing", uuid)
	}
	if info.State != types.StateDead {
		t.Errorf("State = %s, want Dead", info.State)
	}
	if !info.DeadAt.Equal(origDead) {
		t.Errorf("DeadAt = %v, want preserved %v", info.DeadAt, origDead)
	}
}

// TestATDD_64_1_030_ZombieContextFullPreserved asserts a zombie(context_full)
// snapshot preserves ExitReason verbatim — the resume.go:1025 compaction path
// must still key on "context_full" after normalization (AC4 决定性论据，裁决 2）。
func TestATDD_64_1_030_ZombieContextFullPreserved(t *testing.T) {
	k, base := newReloadKernel(t)
	uuid := uuidForTest("ctxf030")

	write64_1Fixture(t, base, vfs.ProcInfo{
		PID: 31, UUID: uuid, State: types.StateZombie, ExitReason: "context_full",
		Intent: "zombie ctxfull", Provider: "claude", CreatedAt: staticTime(t, -1*time.Hour),
	})

	if err := k.LoadHistory(); err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	info := k.FindHistoryByUUID(uuid)
	if info == nil {
		t.Fatalf("uuid %s missing", uuid)
	}
	if info.State != types.StateDead {
		t.Errorf("State = %s, want Dead", info.State)
	}
	if info.ExitReason != "context_full" {
		t.Errorf("ExitReason = %q, want preserved context_full", info.ExitReason)
	}

	// AC4: 归一化后条目仍在 ListResumable，且磁盘 exit_reason 仍是 context_full。
	resumable, err := k.ListResumable()
	if err != nil {
		t.Fatalf("ListResumable: %v", err)
	}
	found := false
	for _, r := range resumable {
		if r.UUID == uuid {
			found = true
			if r.ExitReason != "context_full" {
				t.Errorf("ListResumable[%s].ExitReason = %q, want context_full (resume compaction path)", uuid, r.ExitReason)
			}
		}
	}
	if !found {
		t.Errorf("uuid %s missing from ListResumable after normalization (Epic 42: Dead is resumable)", uuid)
	}
}

// TestATDD_64_1_031_ZombieEmptyExitReasonFloor asserts 防御兜底：zombie 快照
// exit_reason 意外为空（脏数据）→ 补 interrupted，产物仍过 invariant（裁决 2）。
func TestATDD_64_1_031_ZombieEmptyExitReasonFloor(t *testing.T) {
	k, base := newReloadKernel(t)
	uuid := uuidForTest("zdrt031")

	write64_1Fixture(t, base, vfs.ProcInfo{
		PID: 32, UUID: uuid, State: types.StateZombie, // ExitReason 故意留空
		Intent: "dirty zombie", Provider: "claude", CreatedAt: staticTime(t, -1*time.Hour),
	})

	if err := k.LoadHistory(); err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	info := k.FindHistoryByUUID(uuid)
	if info == nil {
		t.Fatalf("uuid %s missing", uuid)
	}
	if info.ExitReason != exitReasonInterrupted {
		t.Errorf("ExitReason = %q, want interrupted (defensive floor)", info.ExitReason)
	}
	if err := ValidateProcInfoInvariant(info); err != nil {
		t.Errorf("normalized product fails invariant: %v", err)
	}
}

// TestATDD_64_1_040_Idempotent asserts 二次 LoadHistory 零回写（AC2 幂等）：首启
// 后磁盘已是终态，第二次不再触碰文件（mtime 不变）。
func TestATDD_64_1_040_Idempotent(t *testing.T) {
	k, base := newReloadKernel(t)
	uuid := uuidForTest("idem040")

	path := write64_1Fixture(t, base, vfs.ProcInfo{
		PID: 41, UUID: uuid, State: types.StateRunning,
		Intent: "idempotent", Provider: "claude", CreatedAt: staticTime(t, -2*time.Hour),
	})

	// 首启归一化 + 回写。
	if err := k.LoadHistory(); err != nil {
		t.Fatalf("LoadHistory#1: %v", err)
	}
	// 正向断言：首次 LoadHistory 确实回写了（磁盘已归一化为 dead）。没有这一步，
	// 若首次回写压根没发生，下面的 mtime 幂等断言会空转绿灯（review P5）。
	if got := readDiskState64_1(t, base, uuid)["state"]; got != "dead" {
		t.Fatalf("disk state after LoadHistory#1 = %v, want dead (首次回写未发生)", got)
	}

	// 把 mtime 推到一个固定的过去时刻；若第二次 LoadHistory 回写，rename 会刷新 mtime。
	past := staticTime(t, -30*time.Minute).Truncate(time.Second)
	if err := os.Chtimes(path, past, past); err != nil {
		t.Fatalf("Chtimes: %v", err)
	}

	// 二次 LoadHistory：磁盘已 dead，helper 应短路，不回写。
	k2 := newSimpleKernel(t)
	k2.SetDataDir(k.dataDir)
	if err := k2.LoadHistory(); err != nil {
		t.Fatalf("LoadHistory#2: %v", err)
	}
	fi2, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat after#2: %v", err)
	}
	if !fi2.ModTime().Equal(past) {
		t.Errorf("second LoadHistory rewrote proc-info.json (mtime %v != %v) — not idempotent", fi2.ModTime(), past)
	}
	// 且内存态仍正确。
	if info := k2.FindHistoryByUUID(uuid); info == nil || info.State != types.StateDead {
		t.Errorf("second LoadHistory memory state wrong: %+v", info)
	}
}

// TestATDD_64_1_050_ProcTableGuardExempts asserts UUID 在 procTable 中的条目跳过
// 归一化（AC3 guard），直接单测 helper 以精确控制 procTable。
func TestATDD_64_1_050_ProcTableGuardExempts(t *testing.T) {
	k, base := newReloadKernel(t)
	uuid := uuidForTest("guard50")

	info := vfs.ProcInfo{
		PID: 51, UUID: uuid, State: types.StateRunning,
		Intent: "live in table", Provider: "claude", CreatedAt: staticTime(t, -1*time.Hour),
	}

	// 塞一个同 UUID 的 live Process 进 procTable。
	proc := NewProcess(0, "live", nil)
	proc.UUID = uuid
	k.procTable.Store(proc.PID, proc)

	out, changed := k.reconcileStaleHistoryEntry(base, info)
	if changed {
		t.Error("changed = true, want false (UUID live in procTable must be exempt)")
	}
	if out.State != types.StateRunning {
		t.Errorf("out.State = %s, want unchanged Running", out.State)
	}
}

// TestATDD_64_1_051_SuspendedExempt asserts suspended 状态不归一化（AC3），单测 helper。
func TestATDD_64_1_051_SuspendedExempt(t *testing.T) {
	k, base := newReloadKernel(t)
	info := vfs.ProcInfo{
		PID: 52, UUID: uuidForTest("susp051"), State: types.StateSuspended,
		SuspendReason: "user_paused", Intent: "frozen", Provider: "claude",
		CreatedAt: staticTime(t, -1*time.Hour),
	}
	out, changed := k.reconcileStaleHistoryEntry(base, info)
	if changed {
		t.Error("changed = true, want false (suspended is a legitimate frozen state)")
	}
	if out.State != types.StateSuspended || out.SuspendReason != "user_paused" {
		t.Errorf("suspended entry mutated: state=%s reason=%q", out.State, out.SuspendReason)
	}
}

// TestATDD_64_1_052_DeadEntryUntouched asserts 已终态 dead 条目不进归一化分支（幂等基石）。
func TestATDD_64_1_052_DeadEntryUntouched(t *testing.T) {
	k, base := newReloadKernel(t)
	info := vfs.ProcInfo{
		PID: 53, UUID: uuidForTest("dead052"), State: types.StateDead, ExitReason: "completed",
		Intent: "already dead", Provider: "claude",
		CreatedAt: staticTime(t, -2*time.Hour), DeadAt: staticTime(t, -1*time.Hour),
	}
	out, changed := k.reconcileStaleHistoryEntry(base, info)
	if changed {
		t.Error("changed = true, want false (Dead is terminal — no normalization)")
	}
	if out.ExitReason != "completed" {
		t.Errorf("Dead entry ExitReason mutated to %q", out.ExitReason)
	}
}

// TestATDD_64_1_060_SyntheticRunningNormalized asserts synthetic running 子代理
// 观察节点同样被归一化（AC6）——不豁免（synthetic 不进 ListResumable，无 resume 顾虑）。
func TestATDD_64_1_060_SyntheticRunningNormalized(t *testing.T) {
	k, base := newReloadKernel(t)
	uuid := uuidForTest("synt060")

	write64_1Fixture(t, base, vfs.ProcInfo{
		PID: 61, UUID: uuid, State: types.StateRunning, Synthetic: true,
		Intent: "synthetic subagent node", Provider: "claude", CreatedAt: staticTime(t, -1*time.Hour),
	})

	if err := k.LoadHistory(); err != nil {
		t.Fatalf("LoadHistory: %v", err)
	}
	info := k.FindHistoryByUUID(uuid)
	if info == nil {
		t.Fatalf("uuid %s missing", uuid)
	}
	if info.State != types.StateDead {
		t.Errorf("synthetic State = %s, want Dead (归一化同样覆盖)", info.State)
	}
	if info.ExitReason != exitReasonInterrupted {
		t.Errorf("synthetic ExitReason = %q, want interrupted", info.ExitReason)
	}
	// 磁盘回写确认。
	raw := readDiskState64_1(t, base, uuid)
	if got, _ := raw["state"].(string); got != "dead" {
		t.Errorf("disk synthetic.state = %q, want dead", got)
	}
	// synthetic 仍被 ListResumable 排除（归一化不改变这一点）。
	resumable, err := k.ListResumable()
	if err != nil {
		t.Fatalf("ListResumable: %v", err)
	}
	for _, r := range resumable {
		if r.UUID == uuid {
			t.Errorf("synthetic uuid %s must remain excluded from ListResumable", uuid)
		}
	}
}

// TestATDD_64_1_070_WritebackFailureDoesNotBlock asserts 回写失败仅告警、不阻塞
// 启动、内存态仍归一化正确（AC2 best-effort，裁决 5）。用只读 steps 目录模拟
// SaveProcInfo 的 MkdirAll/WriteFile 失败。
func TestATDD_64_1_070_WritebackFailureDoesNotBlock(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: chmod 0 does not deny writes")
	}
	k, base := newReloadKernel(t)
	uuid := uuidForTest("robk070")

	write64_1Fixture(t, base, vfs.ProcInfo{
		PID: 71, UUID: uuid, State: types.StateRunning,
		Intent: "readonly writeback", Provider: "claude", CreatedAt: staticTime(t, -1*time.Hour),
	})

	// 把该条目的 steps/<uuid> 目录设为只读，使 SaveProcInfo 的 .tmp 写入失败。
	uuidDir := filepath.Join(base, "steps", uuid)
	if err := os.Chmod(uuidDir, 0o500); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(uuidDir, 0o755) })

	// LoadHistory 不得因回写失败返回 error。
	if err := k.LoadHistory(); err != nil {
		t.Fatalf("LoadHistory must not fail on writeback error: %v", err)
	}
	// 内存态仍是归一化后的 dead（AC2：内存与磁盘允许短暂不一致）。
	info := k.FindHistoryByUUID(uuid)
	if info == nil || info.State != types.StateDead {
		t.Errorf("in-memory state must be normalized despite writeback failure: %+v", info)
	}
}
