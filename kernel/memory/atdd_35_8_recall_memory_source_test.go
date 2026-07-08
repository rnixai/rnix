package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/config"
)

// =============================================================================
// ATDD Tests for Story 35.8: RecallIndex MEMORY.md Source Indexing
// =============================================================================

// --- IndexMemorySource Tests (AC1, AC3) ---

// 35.8-UNIT-001: IndexMemorySource adds entries and makes them searchable
func TestRecallIndex_IndexMemorySource_BasicSearch(t *testing.T) {
	idx := NewRecallIndex()
	idx.IndexMemorySource("memory:global", []string{
		"API endpoint for user authentication uses JWT tokens",
		"Database migration strategy prefers blue-green deployment",
	}, time.Now())

	results := idx.Search("JWT authentication", 20)
	if len(results) == 0 {
		t.Fatal("expected search results for 'JWT authentication'")
	}
	found := false
	for _, r := range results {
		if r.Source == "memory" && r.UUID == "memory:global" {
			found = true
			if r.Summary != "API endpoint for user authentication uses JWT tokens" {
				t.Errorf("unexpected summary: %q", r.Summary)
			}
		}
	}
	if !found {
		t.Error("expected memory source hit with UUID='memory:global'")
	}
}

// 35.8-UNIT-002: IndexMemorySource whole-source replacement semantics
func TestRecallIndex_IndexMemorySource_ReplaceSemantics(t *testing.T) {
	idx := NewRecallIndex()

	idx.IndexMemorySource("memory:global", []string{
		"old knowledge about terraform configuration",
	}, time.Now())

	results := idx.Search("terraform configuration", 20)
	if len(results) == 0 {
		t.Fatal("expected initial hit for 'terraform'")
	}

	// Replace with new entries
	idx.IndexMemorySource("memory:global", []string{
		"new knowledge about kubernetes deployment",
	}, time.Now())

	// Old entry should no longer match
	results = idx.Search("terraform configuration", 20)
	for _, r := range results {
		if r.Source == "memory" {
			t.Error("old entry 'terraform' should not be found after replacement")
		}
	}

	// New entry should match
	results = idx.Search("kubernetes deployment", 20)
	found := false
	for _, r := range results {
		if r.Source == "memory" {
			found = true
		}
	}
	if !found {
		t.Error("new entry 'kubernetes' should be found after replacement")
	}
}

// 35.8-UNIT-003: IndexMemorySource remove semantics (empty entries clears source)
func TestRecallIndex_IndexMemorySource_RemoveSemantics(t *testing.T) {
	idx := NewRecallIndex()

	idx.IndexMemorySource("memory:global", []string{
		"removable knowledge about caching strategies",
	}, time.Now())

	results := idx.Search("caching strategies", 20)
	if len(results) == 0 {
		t.Fatal("expected initial hit")
	}

	// Remove by indexing empty entries
	idx.IndexMemorySource("memory:global", nil, time.Now())

	results = idx.Search("caching strategies", 20)
	for _, r := range results {
		if r.Source == "memory" {
			t.Error("entry should not be found after removal")
		}
	}
}

// 35.8-UNIT-004: Intersection semantics — partial token match does not return
func TestRecallIndex_IndexMemorySource_IntersectionSemantics(t *testing.T) {
	idx := NewRecallIndex()
	idx.IndexMemorySource("memory:global", []string{
		"yaml configuration file parsing",
	}, time.Now())

	// "yaml" alone matches
	results := idx.Search("yaml", 20)
	memHits := 0
	for _, r := range results {
		if r.Source == "memory" {
			memHits++
		}
	}
	if memHits == 0 {
		t.Error("expected hit for single token 'yaml'")
	}

	// "yaml kubernetes" should NOT match (entry has no 'kubernetes')
	results = idx.Search("yaml kubernetes", 20)
	for _, r := range results {
		if r.Source == "memory" {
			t.Error("partial match should not return — entry lacks 'kubernetes'")
		}
	}
}

// 35.8-UNIT-005: CJK bigram indexing works for memory entries
func TestRecallIndex_IndexMemorySource_CJK(t *testing.T) {
	idx := NewRecallIndex()
	idx.IndexMemorySource("memory:global", []string{
		"数据库迁移使用蓝绿部署策略",
	}, time.Now())

	results := idx.Search("数据库", 20)
	found := false
	for _, r := range results {
		if r.Source == "memory" {
			found = true
		}
	}
	if !found {
		t.Error("expected CJK memory entry to be searchable")
	}
}

// 35.8-UNIT-006: Source field distinguishes memory and session results
func TestRecallIndex_Search_SourceField(t *testing.T) {
	dir := t.TempDir()
	createStepsDir(t, dir, "uuid-src", typicalProcessSteps(
		"session knowledge about deployment",
		"tool_call", "complete",
	))

	idx := NewRecallIndex()
	if err := idx.BuildFromDisk(filepath.Join(dir, "data", "steps")); err != nil {
		t.Fatal(err)
	}
	idx.IndexMemorySource("memory:global", []string{
		"memory knowledge about deployment pipelines",
	}, time.Now())

	results := idx.Search("deployment", 20)
	hasSession := false
	hasMemory := false
	for _, r := range results {
		switch r.Source {
		case "session":
			hasSession = true
		case "memory":
			hasMemory = true
		}
	}
	if !hasSession {
		t.Error("expected session source hit")
	}
	if !hasMemory {
		t.Error("expected memory source hit")
	}
}

// 35.8-UNIT-007: Memory and session results merge-sorted by timestamp desc, shared maxResults
func TestRecallIndex_Search_MergeSortAndMaxResults(t *testing.T) {
	dir := t.TempDir()

	// Session data with older timestamp
	createStepsDir(t, dir, "uuid-old", typicalProcessSteps(
		"shared keyword orchestration pipeline",
		"tool_call", "complete",
	))

	idx := NewRecallIndex()
	if err := idx.BuildFromDisk(filepath.Join(dir, "data", "steps")); err != nil {
		t.Fatal(err)
	}

	// Memory source with newer timestamp
	idx.IndexMemorySource("memory:global", []string{
		"shared keyword orchestration improvement notes",
	}, time.Now().Add(time.Hour))

	results := idx.Search("orchestration", 1)
	if len(results) != 1 {
		t.Fatalf("expected exactly 1 result with maxResults=1, got %d", len(results))
	}
	// Newest should win
	if results[0].Source != "memory" {
		t.Errorf("expected memory (newer) to win maxResults=1 sort, got source=%q", results[0].Source)
	}
}

// --- IndexMemoryFile Tests (AC1, AC5) ---

// 35.8-UNIT-008: IndexMemoryFile reads and indexes a real MEMORY.md file
func TestRecallIndex_IndexMemoryFile_RealFile(t *testing.T) {
	dir := t.TempDir()
	memDir := filepath.Join(dir, "memory")
	if err := os.MkdirAll(memDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "§API uses REST with JSON payloads\n§Database is PostgreSQL 15\n"
	if err := os.WriteFile(filepath.Join(memDir, "MEMORY.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	idx := NewRecallIndex()
	idx.IndexMemoryFile("memory:global", filepath.Join(memDir, "MEMORY.md"))

	results := idx.Search("PostgreSQL", 20)
	found := false
	for _, r := range results {
		if r.Source == "memory" {
			found = true
		}
	}
	if !found {
		t.Error("expected memory hit for 'PostgreSQL' from indexed file")
	}
}

// 35.8-UNIT-009: IndexMemoryFile silently skips missing file
func TestRecallIndex_IndexMemoryFile_MissingFile(t *testing.T) {
	idx := NewRecallIndex()
	// Should not panic or error
	idx.IndexMemoryFile("memory:global", "/nonexistent/path/MEMORY.md")
	// No entries indexed
	results := idx.Search("anything", 20)
	if len(results) != 0 {
		t.Errorf("expected 0 results for missing file, got %d", len(results))
	}
}

// 35.8-UNIT-010: IndexMemoryFile skips empty file
func TestRecallIndex_IndexMemoryFile_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "MEMORY.md")
	os.WriteFile(path, []byte(""), 0o644)

	idx := NewRecallIndex()
	idx.IndexMemoryFile("memory:global", path)

	results := idx.Search("anything", 20)
	if len(results) != 0 {
		t.Errorf("expected 0 results for empty file, got %d", len(results))
	}
}

// --- MemoryStore + RecallIndex Roundtrip Tests (AC2, AC4, AC5, AC7) ---

// 35.8-UNIT-011: MemoryStore.Add → Search immediately visible
func TestMemoryStore_Add_RecallRoundtrip(t *testing.T) {
	dir := t.TempDir()
	globalDir := filepath.Join(dir, "global", "memory")
	os.MkdirAll(globalDir, 0o755)
	cfg := DefaultMemoryConfig()
	store := NewMemoryStore(globalDir, dir, cfg)
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}

	idx := NewRecallIndex()
	store.SetRecallIndex(idx)

	if err := store.Add("global_memory", "critical production alert handling procedure", ""); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	results := idx.Search("production alert", 20)
	found := false
	for _, r := range results {
		if r.Source == "memory" {
			found = true
			if r.Summary != "critical production alert handling procedure" {
				t.Errorf("unexpected summary: %q", r.Summary)
			}
		}
	}
	if !found {
		t.Error("expected recall hit immediately after Add")
	}
}

// 35.8-UNIT-012: MemoryStore.Replace → old text gone, new text found
func TestMemoryStore_Replace_RecallRoundtrip(t *testing.T) {
	dir := t.TempDir()
	globalDir := filepath.Join(dir, "global", "memory")
	os.MkdirAll(globalDir, 0o755)
	cfg := DefaultMemoryConfig()
	store := NewMemoryStore(globalDir, dir, cfg)
	store.Load()

	idx := NewRecallIndex()
	store.SetRecallIndex(idx)

	store.Add("global_memory", "old deployment procedure using ansible", "")

	// Verify initial hit
	results := idx.Search("ansible deployment", 20)
	if len(results) == 0 {
		t.Fatal("expected initial hit")
	}

	// Replace
	store.Replace("global_memory", "old deployment procedure using ansible", "new deployment procedure using terraform", "")

	// Old should be gone
	results = idx.Search("ansible deployment", 20)
	for _, r := range results {
		if r.Source == "memory" {
			t.Error("old entry 'ansible' should not be found after replace")
		}
	}

	// New should be found
	results = idx.Search("terraform deployment", 20)
	found := false
	for _, r := range results {
		if r.Source == "memory" {
			found = true
		}
	}
	if !found {
		t.Error("new entry 'terraform' should be found after replace")
	}
}

// 35.8-UNIT-013: MemoryStore.Remove → entry no longer found
func TestMemoryStore_Remove_RecallRoundtrip(t *testing.T) {
	dir := t.TempDir()
	globalDir := filepath.Join(dir, "global", "memory")
	os.MkdirAll(globalDir, 0o755)
	cfg := DefaultMemoryConfig()
	store := NewMemoryStore(globalDir, dir, cfg)
	store.Load()

	idx := NewRecallIndex()
	store.SetRecallIndex(idx)

	store.Add("global_memory", "temporary debugging notes for memory leak", "")

	results := idx.Search("debugging memory", 20)
	if len(results) == 0 {
		t.Fatal("expected initial hit")
	}

	store.Remove("global_memory", "temporary debugging notes for memory leak", "")

	results = idx.Search("debugging memory", 20)
	for _, r := range results {
		if r.Source == "memory" {
			t.Error("removed entry should not be found")
		}
	}
}

// 35.8-UNIT-014: user target does NOT trigger recall index update (AC4)
func TestMemoryStore_UserTarget_NoRecallUpdate(t *testing.T) {
	dir := t.TempDir()
	globalDir := filepath.Join(dir, "global", "memory")
	os.MkdirAll(globalDir, 0o755)

	projDir := filepath.Join(dir, "myproject")
	os.MkdirAll(projDir, 0o755)

	cfg := DefaultMemoryConfig()
	store := NewMemoryStore(globalDir, dir, cfg)
	store.Load()

	idx := NewRecallIndex()
	store.SetRecallIndex(idx)

	// Write to user target
	err := store.Add("user", "user prefers concise explanations", projDir)
	if err != nil {
		t.Fatalf("Add user failed: %v", err)
	}

	// Should not appear in recall
	results := idx.Search("concise explanations", 20)
	for _, r := range results {
		if r.Source == "memory" {
			t.Error("user target should NOT trigger recall index update")
		}
	}
}

// 35.8-UNIT-015: nil RecallIndex on MemoryStore → zero panic (AC5)
func TestMemoryStore_NilRecallIndex_ZeroPanic(t *testing.T) {
	dir := t.TempDir()
	globalDir := filepath.Join(dir, "global", "memory")
	os.MkdirAll(globalDir, 0o755)
	cfg := DefaultMemoryConfig()
	store := NewMemoryStore(globalDir, dir, cfg)
	store.Load()

	// Do NOT call SetRecallIndex — ri stays nil
	if err := store.Add("global_memory", "test content without recall", ""); err != nil {
		t.Fatalf("Add should succeed without recall index: %v", err)
	}
	if err := store.Replace("global_memory", "test content without recall", "replaced content", ""); err != nil {
		t.Fatalf("Replace should succeed without recall index: %v", err)
	}
	if err := store.Remove("global_memory", "replaced content", ""); err != nil {
		t.Fatalf("Remove should succeed without recall index: %v", err)
	}
}

// 35.8-UNIT-016: Multi-project sources don't interfere
func TestRecallIndex_IndexMemorySource_MultiProject(t *testing.T) {
	idx := NewRecallIndex()
	idx.IndexMemorySource("project:alpha", []string{
		"alpha project uses GraphQL API",
	}, time.Now())
	idx.IndexMemorySource("project:beta", []string{
		"beta project uses REST API",
	}, time.Now())

	results := idx.Search("GraphQL", 20)
	for _, r := range results {
		if r.Source == "memory" && r.UUID != "project:alpha" {
			t.Errorf("GraphQL should only be found in alpha, got UUID=%q", r.UUID)
		}
	}

	results = idx.Search("REST", 20)
	for _, r := range results {
		if r.Source == "memory" && r.UUID != "project:beta" {
			t.Errorf("REST should only be found in beta, got UUID=%q", r.UUID)
		}
	}
}

// 35.8-UNIT-017: SaveIndex/LoadIndex does not persist memory sources (防灾点 6)
func TestRecallIndex_SaveLoadIndex_MemorySourcesNotPersisted(t *testing.T) {
	dir := t.TempDir()

	idx1 := NewRecallIndex()
	idx1.IndexMemorySource("memory:global", []string{
		"important knowledge that should not persist in index.json",
	}, time.Now())

	indexPath := filepath.Join(dir, "index.json")
	if err := idx1.SaveIndex(indexPath); err != nil {
		t.Fatal(err)
	}

	idx2 := NewRecallIndex()
	if err := idx2.LoadIndex(indexPath); err != nil {
		t.Fatal(err)
	}

	// Memory sources should not appear after load
	results := idx2.Search("important knowledge", 20)
	for _, r := range results {
		if r.Source == "memory" {
			t.Error("memory sources should not be persisted in index.json")
		}
	}
}

// 35.8-UNIT-018: LoadIndex does not clear existing memory sources
func TestRecallIndex_LoadIndex_PreservesMemorySources(t *testing.T) {
	dir := t.TempDir()

	// Build a session-only index and save
	createStepsDir(t, dir, "uuid-sess", typicalProcessSteps(
		"session data content",
		"tool_call", "complete",
	))
	idxSave := NewRecallIndex()
	idxSave.BuildFromDisk(filepath.Join(dir, "data", "steps"))
	indexPath := filepath.Join(dir, "index.json")
	idxSave.SaveIndex(indexPath)

	// Create a new index with memory sources, then load session index on top
	idx := NewRecallIndex()
	idx.IndexMemorySource("memory:global", []string{
		"pre-loaded memory knowledge",
	}, time.Now())

	idx.LoadIndex(indexPath)

	// Memory sources should still be present
	results := idx.Search("pre-loaded knowledge", 20)
	found := false
	for _, r := range results {
		if r.Source == "memory" {
			found = true
		}
	}
	if !found {
		t.Error("memory sources should survive LoadIndex")
	}
}

// 35.8-UNIT-019: project-scoped memory target Add → recall roundtrip
func TestMemoryStore_ProjectMemory_RecallRoundtrip(t *testing.T) {
	dir := t.TempDir()
	globalDir := filepath.Join(dir, "global", "memory")
	os.MkdirAll(globalDir, 0o755)

	projDir := filepath.Join(dir, "myproject")
	os.MkdirAll(projDir, 0o755)

	cfg := DefaultMemoryConfig()
	store := NewMemoryStore(globalDir, dir, cfg)
	store.Load()

	idx := NewRecallIndex()
	store.SetRecallIndex(idx)

	if err := store.Add("memory", "project specific deployment configuration", projDir); err != nil {
		t.Fatalf("Add project memory failed: %v", err)
	}

	results := idx.Search("deployment configuration", 20)
	found := false
	for _, r := range results {
		if r.Source == "memory" {
			found = true
		}
	}
	if !found {
		t.Error("expected recall hit for project memory Add")
	}
}

// --- Review-hardening tests (code review 2026-07-08) ---

// 35.8-UNIT-020: runtime recallSourceKey matches the startup-wiring derivation
// for every target form (防灾点 4 guard: both paths must share one function).
func TestMemoryStore_RecallSourceKey_MatchesStartupWiring(t *testing.T) {
	dir := t.TempDir()
	globalDir := filepath.Join(dir, "global", "memory")
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	store := NewMemoryStore(globalDir, dir, DefaultMemoryConfig())

	// global_memory target → fixed global key
	if got := store.recallSourceKey("global_memory", "/any/project"); got != MemorySourceKeyGlobal {
		t.Errorf("global_memory key = %q, want %q", got, MemorySourceKeyGlobal)
	}

	// memory target + projectDir → startup wiring indexes AllBaseDirs entries,
	// which for a project are config.ProjectDataDir(dataDir, projectDir)
	projDir := filepath.Join(dir, "myproject")
	want := MemorySourceKeyForBaseDir(config.ProjectDataDir(dir, projDir))
	if got := store.recallSourceKey("memory", projDir); got != want {
		t.Errorf("project memory key = %q, want startup-derived %q", got, want)
	}

	// memory target + empty projectDir → {dataDir}/global fallback dir,
	// also enumerated by AllBaseDirs at startup
	wantGlobalFallback := MemorySourceKeyForBaseDir(config.GlobalDataDir(dir))
	if got := store.recallSourceKey("memory", ""); got != wantGlobalFallback {
		t.Errorf("fallback memory key = %q, want %q", got, wantGlobalFallback)
	}
}

// 35.8-UNIT-021: failed writes (capacity exceeded / replace miss) do NOT
// update the recall index (组合矩阵: 写失败不触发).
func TestMemoryStore_FailedWrite_NoIndexUpdate(t *testing.T) {
	dir := t.TempDir()
	globalDir := filepath.Join(dir, "global", "memory")
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := DefaultMemoryConfig()
	store := NewMemoryStore(globalDir, dir, cfg)
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}
	idx := NewRecallIndex()
	store.SetRecallIndex(idx)

	// Replace of a nonexistent entry must fail and leave the index untouched
	if err := store.Replace("global_memory", "no such entry exists", "phantom replacement text", ""); err == nil {
		t.Fatal("expected Replace of nonexistent entry to fail")
	}
	for _, r := range idx.Search("phantom replacement", 20) {
		if r.Source == "memory" {
			t.Error("failed Replace must not update recall index")
		}
	}

	// Capacity-exceeding Add must fail and leave the index untouched
	huge := "oversized capacity probe " + strings.Repeat("z", cfg.Store.MemoryCharLimit+16)
	if err := store.Add("global_memory", huge, ""); err == nil {
		t.Fatal("expected capacity-exceeding Add to fail")
	}
	for _, r := range idx.Search("oversized capacity", 20) {
		if r.Source == "memory" {
			t.Error("failed Add must not update recall index")
		}
	}
}

// 35.8-UNIT-022: two projects writing through the real MemoryStore key
// derivation land in distinct synthetic sources without cross-talk
// (组合矩阵: Fix H 多项目 × 源 key, real recallSourceKey chain).
func TestMemoryStore_MultiProject_RealKeyDerivation(t *testing.T) {
	dir := t.TempDir()
	globalDir := filepath.Join(dir, "global", "memory")
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	projA := filepath.Join(dir, "proj-alpha")
	projB := filepath.Join(dir, "proj-beta")
	for _, d := range []string{projA, projB} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	store := NewMemoryStore(globalDir, dir, DefaultMemoryConfig())
	if err := store.Load(); err != nil {
		t.Fatal(err)
	}
	idx := NewRecallIndex()
	store.SetRecallIndex(idx)

	if err := store.Add("memory", "alpha project uses graphql interface", projA); err != nil {
		t.Fatal(err)
	}
	if err := store.Add("memory", "beta project uses rest interface", projB); err != nil {
		t.Fatal(err)
	}

	wantA := MemorySourceKeyForBaseDir(config.ProjectDataDir(dir, projA))
	wantB := MemorySourceKeyForBaseDir(config.ProjectDataDir(dir, projB))
	if wantA == wantB {
		t.Fatal("distinct projects must derive distinct source keys")
	}

	foundA := false
	for _, r := range idx.Search("graphql interface", 20) {
		if r.Source != "memory" {
			continue
		}
		foundA = true
		if r.UUID != wantA {
			t.Errorf("graphql hit UUID = %q, want %q", r.UUID, wantA)
		}
		if !strings.HasPrefix(r.UUID, "memory:project:") {
			t.Errorf("project hit UUID %q must carry synthetic memory:project: prefix (AC3)", r.UUID)
		}
	}
	if !foundA {
		t.Error("expected memory hit for project alpha")
	}

	for _, r := range idx.Search("rest interface", 20) {
		if r.Source == "memory" && r.UUID != wantB {
			t.Errorf("rest hit UUID = %q, want %q (no cross-talk)", r.UUID, wantB)
		}
	}
}
