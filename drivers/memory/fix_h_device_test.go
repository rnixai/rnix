package memory

import (
	"context"
	"strings"
	"testing"

	kernelmemory "github.com/rnixai/rnix/kernel/memory"
)

// Fix H (device layer): MemoryCommitFile threads its workDir (= caller's
// ProjectConfig.ProjectDir) into the store, so two files opened with different
// workDirs write to isolated per-project memory. Proves the factory→file→store
// workDir path is wired end-to-end.
func TestMemoryCommitFile_PerProjectWorkDir(t *testing.T) {
	globalDir := t.TempDir()
	dataDir := t.TempDir()
	cfg := kernelmemory.DefaultMemoryConfig()
	cfg.Store.MemoryCharLimit = 4096
	store := kernelmemory.NewMemoryStore(globalDir, dataDir, cfg)
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}
	driver := NewDriver(store)
	factory := FileFactory(driver)

	projA := t.TempDir()
	projB := t.TempDir()

	fileA, err := factory("", 0, projA)
	if err != nil {
		t.Fatal(err)
	}
	fileB, err := factory("", 0, projB)
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()
	if err := fileA.Write(ctx, []byte(`{"action":"add","content":"dev-entry-A","target":"memory"}`)); err != nil {
		t.Fatalf("write A: %v", err)
	}
	if err := fileB.Write(ctx, []byte(`{"action":"add","content":"dev-entry-B","target":"memory"}`)); err != nil {
		t.Fatalf("write B: %v", err)
	}

	snapA := store.Snapshot("memory", projA)
	snapB := store.Snapshot("memory", projB)
	if !strings.Contains(snapA, "dev-entry-A") || strings.Contains(snapA, "dev-entry-B") {
		t.Errorf("projA not isolated via device workDir: %q", snapA)
	}
	if !strings.Contains(snapB, "dev-entry-B") || strings.Contains(snapB, "dev-entry-A") {
		t.Errorf("projB not isolated via device workDir: %q", snapB)
	}
}
