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

// writeProcInfoOnly writes a minimal proc-info.json for a given state.
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
}

// --- 42.2-UNIT-006: ListResumable 只返回 state=running 残留 (AC#4) ---

func TestATDD_42_2_006_ListResumable_FiltersByRunning(t *testing.T) {


	baseDir := t.TempDir()
	writeProcInfoOnly(t, baseDir, "running-uuid-0000-0000-000000000001", "running", "running intent")
	writeProcInfoOnly(t, baseDir, "zombie-uuid-0000-0000-000000000002", "zombie", "zombie intent")
	writeProcInfoOnly(t, baseDir, "dead-uuid-0000-0000-000000000003", "dead", "dead intent")

	infos, err := ListResumable(baseDir)
	if err != nil {
		t.Fatalf("ListResumable: %v", err)
	}
	if len(infos) != 1 {
		t.Fatalf("got %d entries, want 1 (only running)", len(infos))
	}
	if infos[0].State != types.StateRunning {
		t.Errorf("state = %s, want running", infos[0].State)
	}
	if infos[0].UUID != "running-uuid-0000-0000-000000000001" {
		t.Errorf("UUID = %q, want running-uuid-...", infos[0].UUID)
	}
	if infos[0].Intent != "running intent" {
		t.Errorf("Intent = %q, want %q", infos[0].Intent, "running intent")
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

// --- 42.2-UNIT-008: KernelImpl.ListResumable 过滤已在 procTable 的 UUID (AC#10) ---

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
		t.Fatalf("got %d entries, want 1 (orphan only)", len(got))
	}
	if got[0].UUID != uuidOrphan {
		t.Errorf("UUID = %q, want %q (AC#10 must filter active UUIDs)", got[0].UUID, uuidOrphan)
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
