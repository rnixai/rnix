package kernel

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// =============================================================================
// ATDD 42.2: 韧性层 — ListResumable 扫描与过滤（AC#4, AC#10）
//
// Covers package-level ListResumable (disk scan) and KernelImpl.ListResumable
// (procTable filtering).
//
// RED PHASE: kernel.ListResumable returns nil, nil.
// =============================================================================

// writeProcInfoOnly writes a minimal proc-info.json + a placeholder steps.jsonl
// for a given state. Epic 42 fix: ListResumable now requires steps.jsonl to be
// present (no history = nothing to resume), so the helper writes both files.
// Used to set up disk fixtures without going through SaveProcInfo (which
// requires a live Process snapshot).
func writeProcInfoOnly(t *testing.T, baseDir, uuid, state, intent string) {
	t.Helper()
	dir := filepath.Join(baseDir, "data", "steps", uuid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	info := map[string]any{
		"pid":          1,
		"uuid":         uuid,
		"state":        state,
		"intent":       intent,
		"provider":     "claude",
		"model":        "claude-4",
		"tokens_used":  100,
		"created_at":   "2026-05-16T10:00:00Z",
		"max_steps":    20,
	}
	data, _ := json.Marshal(info)
	if err := os.WriteFile(filepath.Join(dir, "proc-info.json"), data, 0o600); err != nil {
		t.Fatalf("write proc-info.json: %v", err)
	}
	// steps.jsonl placeholder — content does not matter for ListResumable filter,
	// only file presence (per Epic 42 fix).
	if err := os.WriteFile(filepath.Join(dir, "steps.jsonl"), []byte("{\"step\":1}\n"), 0o600); err != nil {
		t.Fatalf("write steps.jsonl: %v", err)
	}
}

// --- 42.2-UNIT-006: ListResumable 返回所有可恢复状态 (Epic 42 fix) ---
//
// Renamed semantics: ListResumable now returns ANY UUID with sufficient history
// (proc-info.json + steps.jsonl), regardless of state — daemon crash is no
// longer the only resumable scenario. Order: most-recent-first by DeadAt/CreatedAt.

func TestATDD_42_2_006_ListResumable_FiltersByRunning(t *testing.T) {


	baseDir := t.TempDir()
	writeProcInfoOnly(t, baseDir, "running-uuid-0000-0000-000000000001", "running", "running intent")
	writeProcInfoOnly(t, baseDir, "zombie-uuid-0000-0000-000000000002", "zombie", "zombie intent")
	writeProcInfoOnly(t, baseDir, "dead-uuid-0000-0000-000000000003", "dead", "dead intent")

	infos, err := ListResumable(baseDir)
	if err != nil {
		t.Fatalf("ListResumable: %v", err)
	}
	// Epic 42 fix: all three are resumable (running daemon-crash leftover +
	// naturally exited zombie + dead). Previously only state=running passed.
	if len(infos) != 3 {
		t.Fatalf("got %d entries, want 3 (all resumable)", len(infos))
	}
	seen := map[string]types.ProcessState{}
	for _, info := range infos {
		seen[info.UUID] = info.State
	}
	if seen["running-uuid-0000-0000-000000000001"] != types.StateRunning {
		t.Errorf("running UUID missing or wrong state: %+v", seen)
	}
	if seen["zombie-uuid-0000-0000-000000000002"] != types.StateZombie {
		t.Errorf("zombie UUID missing or wrong state: %+v", seen)
	}
	if seen["dead-uuid-0000-0000-000000000003"] != types.StateDead {
		t.Errorf("dead UUID missing or wrong state: %+v", seen)
	}
}

// --- 42.2-UNIT-007: ListResumable 容错（损坏 + 不存在）(AC#4) ---

func TestATDD_42_2_007_ListResumable_SkipsCorruptAndMissing(t *testing.T) {


	// Missing baseDir → nil, nil
	infos, err := ListResumable(filepath.Join(t.TempDir(), "does", "not", "exist"))
	if err != nil {
		t.Fatalf("missing baseDir: got err %v, want nil", err)
	}
	if infos != nil {
		t.Errorf("missing baseDir: got %v entries, want nil", len(infos))
	}

	// Empty baseDir string → nil, nil
	infos, err = ListResumable("")
	if err != nil {
		t.Fatalf("empty baseDir: got err %v, want nil", err)
	}
	if infos != nil {
		t.Errorf("empty baseDir: got %v entries, want nil", len(infos))
	}

	// Corrupt JSON file should be skipped (logged), not abort the scan.
	baseDir := t.TempDir()
	writeProcInfoOnly(t, baseDir, "good-uuid-0000-0000-000000000001", "running", "good")
	corruptDir := filepath.Join(baseDir, "data", "steps", "bad-uuid-0000-0000-000000000002")
	_ = os.MkdirAll(corruptDir, 0o755)
	_ = os.WriteFile(filepath.Join(corruptDir, "proc-info.json"), []byte("{not json"), 0o600)

	infos, err = ListResumable(baseDir)
	if err != nil {
		t.Fatalf("scan with corrupt: %v", err)
	}
	if len(infos) != 1 {
		t.Errorf("expected 1 valid entry (corrupt skipped), got %d", len(infos))
	}
	if len(infos) > 0 && infos[0].UUID != "good-uuid-0000-0000-000000000001" {
		t.Errorf("got UUID %q, want good-uuid-...", infos[0].UUID)
	}
}

// --- 42.2-UNIT-008: KernelImpl.ListResumable 过滤 procTable 内 Running 进程 (Epic 42 fix) ---
//
// Epic 42 fix: instead of filtering ALL UUIDs that appear in procTable, the
// kernel only filters Running ones — resuming a Running process is illegal
// (it's already running), but Dead/Zombie/Suspended in the table remain
// resumable from the user's POV.

func TestATDD_42_2_008_KernelListResumable_FiltersProcTable(t *testing.T) {


	k := newThrottleTestKernel(t)
	baseDir := k.GetStepDataDir()

	uuidActive := "active-uuid-0000-0000-000000000001"
	uuidOrphan := "orphan-uuid-0000-0000-000000000002"
	writeProcInfoOnly(t, baseDir, uuidActive, "running", "active in procTable")
	writeProcInfoOnly(t, baseDir, uuidOrphan, "running", "orphan crash leftover")

	// Put uuidActive into procTable so it should be filtered.
	active := NewProcess(0, "active proc", nil)
	active.UUID = uuidActive
	_ = active.Start()
	k.AddProcess(active)
	t.Cleanup(func() { _ = k.Kill(active.PID, types.SIGKILL) })

	got, err := k.ListResumable()
	if err != nil {
		t.Fatalf("KernelImpl.ListResumable: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d entries, want 1 (orphan only — Running in procTable filtered)", len(got))
	}
	if got[0].UUID != uuidOrphan {
		t.Errorf("UUID = %q, want %q (procTable Running must be filtered)", got[0].UUID, uuidOrphan)
	}
}

// --- 42.2-UNIT-008b: KernelImpl.ListResumable stepDataDir 为空时返回 nil ---

func TestATDD_42_2_008b_KernelListResumable_EmptyDataDir(t *testing.T) {


	// Build a kernel without SetStepDataDir.
	reg := vfs.NewDeviceRegistry()
	v := vfs.NewVFS(reg)
	// nil ctxMgr is fine for this query test - no reasonStep involvement.
	k := NewKernel(v, nil, nil)
	t.Cleanup(k.Shutdown)

	got, err := k.ListResumable()
	if err != nil {
		t.Errorf("empty stepDataDir: got err %v, want nil", err)
	}
	if len(got) != 0 {
		t.Errorf("empty stepDataDir: got %d entries, want 0", len(got))
	}}
