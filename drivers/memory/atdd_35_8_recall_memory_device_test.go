package memory

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	kernelmemory "github.com/rnixai/rnix/kernel/memory"
)

// =============================================================================
// ATDD Tests for Story 35.8: Device-Level E2E — commit → recall roundtrip
// =============================================================================

// 35.8-VFS-001: Full roundtrip: /dev/memory/commit Write add → /dev/memory/recall
// Write query → Read returns results with source="memory" (AC7)
func TestDevice_CommitRecall_Roundtrip(t *testing.T) {
	dir := t.TempDir()
	globalMemDir := filepath.Join(dir, "global", "memory")
	if err := os.MkdirAll(globalMemDir, 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := kernelmemory.DefaultMemoryConfig()
	store := kernelmemory.NewMemoryStore(globalMemDir, dir, cfg)
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}

	idx := kernelmemory.NewRecallIndex()
	store.SetRecallIndex(idx)

	// --- Commit device: Write add ---
	commitDriver := NewDriver(store)
	commitFile, err := FileFactory(commitDriver)("", 0, "")
	if err != nil {
		t.Fatal(err)
	}

	addReq, _ := json.Marshal(map[string]string{
		"action":  "add",
		"target":  "global_memory",
		"content": "hello-memory test knowledge about caching strategies",
	})
	if err := commitFile.Write(context.Background(), addReq); err != nil {
		t.Fatalf("commit Write failed: %v", err)
	}

	// Verify commit succeeded
	commitData, err := commitFile.Read(0)
	if err != nil {
		t.Fatal(err)
	}
	var commitResp struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
	}
	json.Unmarshal(commitData, &commitResp)
	if !commitResp.OK {
		t.Fatalf("commit not OK: %s", commitResp.Error)
	}

	// --- Recall device: Write query + Read results ---
	readySearcher := &readyRecallSearcherWrapper{idx: idx}
	recallDriver := NewRecallDriver(readySearcher, nil)
	recallFile := &MemoryRecallFile{driver: recallDriver}

	recallReq, _ := json.Marshal(recallRequest{Query: "caching strategies"})
	if err := recallFile.Write(context.Background(), recallReq); err != nil {
		t.Fatalf("recall Write failed: %v", err)
	}

	recallData, _ := recallFile.Read(0)
	var recallResp recallResponse
	if err := json.Unmarshal(recallData, &recallResp); err != nil {
		t.Fatalf("unmarshal recall response: %v", err)
	}

	if !recallResp.OK {
		t.Fatalf("recall not OK: %s", recallResp.Error)
	}
	if recallResp.Count == 0 {
		t.Fatal("expected at least 1 recall result")
	}

	// Verify source="memory" in results
	found := false
	for _, r := range recallResp.Results {
		if r.Source == "memory" {
			found = true
			if r.Summary != "hello-memory test knowledge about caching strategies" {
				t.Errorf("unexpected summary: %q", r.Summary)
			}
		}
	}
	if !found {
		t.Error("expected result with source='memory' in recall response")
	}
}

// 35.8-VFS-002: Commit replace → recall reflects updated entry
func TestDevice_CommitReplace_RecallReflects(t *testing.T) {
	dir := t.TempDir()
	globalMemDir := filepath.Join(dir, "global", "memory")
	os.MkdirAll(globalMemDir, 0o755)

	cfg := kernelmemory.DefaultMemoryConfig()
	store := kernelmemory.NewMemoryStore(globalMemDir, dir, cfg)
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}

	idx := kernelmemory.NewRecallIndex()
	store.SetRecallIndex(idx)

	commitDriver := NewDriver(store)

	// Add initial entry
	commitFile1, err := FileFactory(commitDriver)("", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	addReq, _ := json.Marshal(map[string]string{
		"action": "add", "target": "global_memory",
		"content": "initial knowledge about redis caching",
	})
	if err := commitFile1.Write(context.Background(), addReq); err != nil {
		t.Fatalf("commit add Write failed: %v", err)
	}

	// Replace
	commitFile2, err := FileFactory(commitDriver)("", 0, "")
	if err != nil {
		t.Fatal(err)
	}
	replaceReq, _ := json.Marshal(map[string]string{
		"action": "replace",
		"target": "global_memory",
		"old":    "initial knowledge about redis caching",
		"new":    "updated knowledge about memcached caching",
	})
	if err := commitFile2.Write(context.Background(), replaceReq); err != nil {
		t.Fatalf("commit replace Write failed: %v", err)
	}

	// Search for old term should miss
	readySearcher := &readyRecallSearcherWrapper{idx: idx}
	recallDriver := NewRecallDriver(readySearcher, nil)
	recallFile := &MemoryRecallFile{driver: recallDriver}

	recallReq, _ := json.Marshal(recallRequest{Query: "redis caching"})
	if err := recallFile.Write(context.Background(), recallReq); err != nil {
		t.Fatalf("recall Write failed: %v", err)
	}
	data, err := recallFile.Read(0)
	if err != nil {
		t.Fatal(err)
	}
	var resp recallResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatal(err)
	}

	for _, r := range resp.Results {
		if r.Source == "memory" {
			t.Error("old 'redis' entry should not be found after replace")
		}
	}

	// Search for new term should hit
	recallFile2 := &MemoryRecallFile{driver: recallDriver}
	recallReq2, _ := json.Marshal(recallRequest{Query: "memcached caching"})
	if err := recallFile2.Write(context.Background(), recallReq2); err != nil {
		t.Fatalf("recall Write failed: %v", err)
	}
	data2, err := recallFile2.Read(0)
	if err != nil {
		t.Fatal(err)
	}
	var resp2 recallResponse
	if err := json.Unmarshal(data2, &resp2); err != nil {
		t.Fatal(err)
	}

	found := false
	for _, r := range resp2.Results {
		if r.Source == "memory" {
			found = true
		}
	}
	if !found {
		t.Error("new 'memcached' entry should be found after replace")
	}
}
