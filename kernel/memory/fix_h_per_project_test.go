package memory

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/rnixai/rnix/internal/config"
)

// =============================================================================
// Fix H: project memory resolves per-project under {dataDir}/projects/<id>/memory
// (same root as steps/events), independent of daemon CWD, and persists across
// MemoryStore instances (the daemon-restart scenario the memory eval exercises).
// =============================================================================

func fixHConfig() MemoryConfig {
	cfg := DefaultMemoryConfig()
	cfg.Store.MemoryCharLimit = 100000
	cfg.Store.UserCharLimit = 2048
	return cfg
}

func newFixHStore(t *testing.T, dataDir string) *MemoryStore {
	t.Helper()
	globalDir := filepath.Join(t.TempDir(), "global", "memory")
	store := NewMemoryStore(globalDir, dataDir, fixHConfig())
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}
	return store
}

// FIXH-001: 不同 projectDir 的 project memory 互相隔离，且落盘到 ProjectDataDir/memory。
func TestMemoryStore_PerProjectIsolation(t *testing.T) {
	dataDir := t.TempDir()
	projA := t.TempDir()
	projB := t.TempDir()
	store := newFixHStore(t, dataDir)

	if err := store.Add("memory", "entry-from-A", projA); err != nil {
		t.Fatalf("Add projA: %v", err)
	}
	if err := store.Add("memory", "entry-from-B", projB); err != nil {
		t.Fatalf("Add projB: %v", err)
	}

	snapA := store.Snapshot("memory", projA)
	snapB := store.Snapshot("memory", projB)
	if !strings.Contains(snapA, "entry-from-A") || strings.Contains(snapA, "entry-from-B") {
		t.Errorf("projA snapshot not isolated: %q", snapA)
	}
	if !strings.Contains(snapB, "entry-from-B") || strings.Contains(snapB, "entry-from-A") {
		t.Errorf("projB snapshot not isolated: %q", snapB)
	}

	// 落盘位置应在 {dataDir}/projects/<id>/memory/MEMORY.md（CWD 无关）。
	wantA := filepath.Join(config.ProjectDataDir(dataDir, projA), "memory", "MEMORY.md")
	if _, err := os.Stat(wantA); err != nil {
		t.Errorf("expected projA memory file at %s: %v", wantA, err)
	}
}

// FIXH-002: 同 dataDir 新建 store 能读回 project memory —— 单测级模拟 daemon restart，
// 即 memory/hello-memory eval 的核心跨 session 场景。
func TestMemoryStore_PerProjectPersistsAcrossInstances(t *testing.T) {
	dataDir := t.TempDir()
	proj := t.TempDir()

	store1 := newFixHStore(t, dataDir)
	if err := store1.Add("memory", "persist-me", proj); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// 模拟 daemon restart：全新 MemoryStore，同一 dataDir、同一 projectDir。
	store2 := NewMemoryStore(filepath.Join(t.TempDir(), "global", "memory"), dataDir, fixHConfig())
	if err := store2.Load(); err != nil {
		t.Fatal(err)
	}
	if snap := store2.Snapshot("memory", proj); !strings.Contains(snap, "persist-me") {
		t.Errorf("project memory did not persist across store instances (daemon restart): %q", snap)
	}
}

// FIXH-003: projectDir="" 回落到 GlobalDataDir(dataDir)/memory（与 ResolveStepBaseDir nil 分支一致）。
func TestMemoryStore_EmptyProjectDirFallsBackToGlobalData(t *testing.T) {
	dataDir := t.TempDir()
	store := newFixHStore(t, dataDir)

	if err := store.Add("memory", "fallback-entry", ""); err != nil {
		t.Fatalf("Add: %v", err)
	}
	want := filepath.Join(config.GlobalDataDir(dataDir), "memory", "MEMORY.md")
	if _, err := os.Stat(want); err != nil {
		t.Errorf("expected fallback memory file at %s: %v", want, err)
	}
	if snap := store.Snapshot("memory", ""); !strings.Contains(snap, "fallback-entry") {
		t.Errorf("fallback snapshot missing entry: %q", snap)
	}
}

// FIXH-004: dataDir="" 退化场景回落 global provider，不 panic、不写相对路径。
func TestMemoryStore_DataDirEmptyFallsBackToGlobalProvider(t *testing.T) {
	store := newFixHStore(t, "")
	if err := store.Add("memory", "degenerate-entry", "/some/proj"); err != nil {
		t.Fatalf("Add with empty dataDir: %v", err)
	}
	if snap := store.Snapshot("memory", "/some/proj"); !strings.Contains(snap, "degenerate-entry") {
		t.Errorf("degenerate snapshot missing entry: %q", snap)
	}
}

// FIXH-005: 同 project 并发 Add 共享一个缓存 provider，线程安全。
func TestMemoryStore_ConcurrentSameProject(t *testing.T) {
	dataDir := t.TempDir()
	proj := t.TempDir()
	store := newFixHStore(t, dataDir)

	var wg sync.WaitGroup
	n := 20
	wg.Add(n)
	for i := range n {
		go func(idx int) {
			defer wg.Done()
			_ = store.Add("memory", strings.Repeat("y", 10), proj)
			_ = idx
		}(i)
	}
	wg.Wait()
	snap := store.Snapshot("memory", proj)
	if got := strings.Count(snap, "yyyyyyyyyy"); got != n {
		t.Errorf("expected %d concurrent entries, got %d", n, got)
	}
}

// FIXH-006: "user" target 保持 project-scoped（跟随项目 provider 的 USER.md），跨项目隔离。
func TestMemoryStore_UserTargetPerProject(t *testing.T) {
	dataDir := t.TempDir()
	projA := t.TempDir()
	projB := t.TempDir()
	store := newFixHStore(t, dataDir)

	if err := store.Add("user", "profile-A", projA); err != nil {
		t.Fatalf("Add user projA: %v", err)
	}
	if snap := store.Snapshot("user", projB); strings.Contains(snap, "profile-A") {
		t.Errorf("user target should be per-project isolated, projB got: %q", snap)
	}
	if snap := store.Snapshot("user", projA); !strings.Contains(snap, "profile-A") {
		t.Errorf("user target projA missing entry: %q", snap)
	}
}
