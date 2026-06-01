package kernel

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/rnixai/rnix/internal/types"
)

// =============================================================================
// ATDD Epic 42 fix: 验证 ListResumable 新语义 + Spawn 立写 proc-info.json
//
// Scenarios:
//   - FIX-001: ListResumable 同时包含 daemon 崩溃残留 + 自然退出的 Dead/Zombie
//   - FIX-002: ListResumable 要求 steps.jsonl 或 checkpoint.json 至少一个
//   - FIX-003: KernelImpl.ListResumable 不过滤 procTable 内的 Dead/Zombie/Suspended
//   - FIX-004: ListResumable 排序为 most-recent-first
// =============================================================================

// --- FIX-001: 多状态混合时 ListResumable 全部返回 ---

func TestATDD_42_Fix_001_ListResumable_AllStatesResumable(t *testing.T) {
	baseDir := t.TempDir()
	writeProcInfoOnly(t, baseDir, "fix-running-aaaa-bbbb-cccc-dddd-000000000001", "running", "daemon crash leftover")
	writeProcInfoOnly(t, baseDir, "fix-dead-aaaa-bbbb-cccc-dddd-000000000002", "dead", "naturally completed")
	writeProcInfoOnly(t, baseDir, "fix-zombie-aaaa-bbbb-cccc-dddd-000000000003", "zombie", "killed")
	writeProcInfoOnly(t, baseDir, "fix-suspended-aaaa-bbbb-cccc-dddd-000000000004", "suspended", "loop detected")

	got, err := ListResumable(baseDir)
	if err != nil {
		t.Fatalf("ListResumable: %v", err)
	}
	if len(got) != 4 {
		t.Fatalf("got %d entries, want 4 — Epic 42 fix: all states resumable", len(got))
	}
}

// --- FIX-002: 没有 steps.jsonl 和 checkpoint.json 的条目被忽略 ---

func TestATDD_42_Fix_002_ListResumable_RequiresHistoryOrCheckpoint(t *testing.T) {
	baseDir := t.TempDir()

	// Entry A: proc-info.json only — no history, should be skipped.
	uuidA := "fix-no-history-aaaa-bbbb-cccc-dddd-000000000010"
	dirA := filepath.Join(baseDir, "steps", uuidA)
	if err := os.MkdirAll(dirA, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	info := map[string]any{
		"pid": 1, "uuid": uuidA, "state": "running",
		"intent": "no history", "provider": "claude", "model": "claude-4",
		"created_at": "2026-05-16T10:00:00Z",
	}
	data, _ := json.Marshal(info)
	if err := os.WriteFile(filepath.Join(dirA, "proc-info.json"), data, 0o600); err != nil {
		t.Fatalf("write proc-info.json: %v", err)
	}

	// Entry B: proc-info.json + checkpoint.json (no steps.jsonl) → resumable.
	uuidB := "fix-cp-only-aaaa-bbbb-cccc-dddd-000000000020"
	dirB := filepath.Join(baseDir, "steps", uuidB)
	if err := os.MkdirAll(dirB, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	info["uuid"] = uuidB
	info["intent"] = "checkpoint-resumable"
	dataB, _ := json.Marshal(info)
	if err := os.WriteFile(filepath.Join(dirB, "proc-info.json"), dataB, 0o600); err != nil {
		t.Fatalf("write proc-info.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dirB, "checkpoint.json"), []byte("{}"), 0o600); err != nil {
		t.Fatalf("write checkpoint.json: %v", err)
	}

	// Entry C: proc-info.json + steps.jsonl (no checkpoint) → resumable.
	writeProcInfoOnly(t, baseDir, "fix-steps-only-aaaa-bbbb-cccc-dddd-000000000030", "dead", "history-resumable")

	got, err := ListResumable(baseDir)
	if err != nil {
		t.Fatalf("ListResumable: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2 (A skipped, B+C resumable). entries=%+v", len(got), got)
	}
	uuids := map[string]bool{got[0].UUID: true, got[1].UUID: true}
	if uuids[uuidA] {
		t.Errorf("entry A (no history) should be filtered out")
	}
	if !uuids[uuidB] {
		t.Errorf("entry B (checkpoint-only) should be resumable")
	}
}

// --- FIX-003: KernelImpl.ListResumable 保留 procTable 中 Dead/Zombie/Suspended ---

func TestATDD_42_Fix_003_KernelListResumable_KeepsDeadInProcTable(t *testing.T) {
	k := newThrottleTestKernel(t)
	projBase := TestProjectBaseDir(k.GetDataDir())

	uuidDead := "fix-dead-in-table-aaaa-bbbb-cccc-dddd-000000000001"
	uuidRunning := "fix-running-in-table-aaaa-bbbb-cccc-dddd-000000000002"
	writeProcInfoOnly(t, projBase, uuidDead, "dead", "dead in procTable")
	writeProcInfoOnly(t, projBase, uuidRunning, "running", "running in procTable")

	// Add Running process — should be filtered.
	running := NewProcess(0, "running proc", nil)
	running.UUID = uuidRunning
	_ = running.Start()
	k.AddProcess(running)
	t.Cleanup(func() { _ = k.Kill(running.PID, types.SIGKILL) })

	// Add Dead process — should NOT be filtered (per Epic 42 fix).
	dead := NewProcess(0, "dead proc", nil)
	dead.UUID = uuidDead
	_ = dead.Start()
	// Force Dead state (Running → Zombie → Dead).
	if err := dead.Transition(types.StateZombie); err != nil {
		t.Fatalf("transition zombie: %v", err)
	}
	if err := dead.Transition(types.StateDead); err != nil {
		t.Fatalf("transition dead: %v", err)
	}
	k.AddProcess(dead)

	got, err := k.ListResumable()
	if err != nil {
		t.Fatalf("ListResumable: %v", err)
	}
	uuids := map[string]bool{}
	for _, p := range got {
		uuids[p.UUID] = true
	}
	if uuids[uuidRunning] {
		t.Errorf("running UUID %q should be filtered (cannot resume a running proc)", uuidRunning)
	}
	if !uuids[uuidDead] {
		t.Errorf("dead UUID %q should remain resumable (Epic 42 fix)", uuidDead)
	}
}
