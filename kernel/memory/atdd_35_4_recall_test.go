package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// =============================================================================
// ATDD Tests for Story 35.4: Recall Cross-Process Search
// TDD RED PHASE — Tests for kernel/memory/recall.go
// =============================================================================

// --- Mock LLMCaller for recall summary testing ---

type mockRecallLLMCaller struct {
	mu       sync.Mutex
	response string
	err      error
	calls    int
}

func (m *mockRecallLLMCaller) Call(ctx context.Context, systemPrompt, userPrompt string, maxTokens int) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls++
	return m.response, m.err
}

func (m *mockRecallLLMCaller) CallCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls
}

// --- Helper: create steps directory with steps.jsonl ---

func createStepsDir(t *testing.T, baseDir, uuid string, steps []map[string]any) string {
	t.Helper()
	stepsDir := filepath.Join(baseDir, "data", "steps", uuid)
	if err := os.MkdirAll(stepsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(filepath.Join(stepsDir, "steps.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, step := range steps {
		data, _ := json.Marshal(step)
		fmt.Fprintf(f, "%s\n", data)
	}
	return stepsDir
}

// --- Helper: create typical process steps ---

func typicalProcessSteps(summary string, actions ...string) []map[string]any {
	var steps []map[string]any
	for i, action := range actions {
		steps = append(steps, map[string]any{
			"step":    i + 1,
			"action":  action,
			"summary": fmt.Sprintf("step %d: %s - %s", i+1, action, summary),
		})
	}
	return steps
}

// =============================================================================
// RecallIndex Initialization & Background Building Tests (AC-1)
// =============================================================================

// 35.4-UNIT-001: RecallIndex BuildFromDisk scans steps directories and builds inverted index
func TestRecallIndex_BuildFromDisk_ScansSteps(t *testing.T) {
	dir := t.TempDir()

	// Create two process step directories
	createStepsDir(t, dir, "uuid-1", typicalProcessSteps(
		"yaml config merge",
		"tool_call", "tool_call", "plan", "tool_call", "complete",
	))
	createStepsDir(t, dir, "uuid-2", typicalProcessSteps(
		"shell command execution",
		"tool_call", "tool_call", "complete",
	))

	idx := NewRecallIndex()
	err := idx.BuildFromDisk(filepath.Join(dir, "data", "steps"))
	if err != nil {
		t.Fatalf("BuildFromDisk failed: %v", err)
	}

	// Index should contain entries from both processes
	if idx.ProcessCount() < 2 {
		t.Errorf("expected at least 2 indexed processes, got %d", idx.ProcessCount())
	}
}

// 35.4-UNIT-002: RecallIndex BuildFromDisk with empty directory returns no error
func TestRecallIndex_BuildFromDisk_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	stepsDir := filepath.Join(dir, "data", "steps")
	if err := os.MkdirAll(stepsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	idx := NewRecallIndex()
	err := idx.BuildFromDisk(stepsDir)
	if err != nil {
		t.Fatalf("BuildFromDisk with empty dir should not error: %v", err)
	}
	if idx.ProcessCount() != 0 {
		t.Errorf("expected 0 indexed processes, got %d", idx.ProcessCount())
	}
}

// 35.4-UNIT-003: RecallIndex BuildFromDisk with nonexistent directory returns error
func TestRecallIndex_BuildFromDisk_NonexistentDir(t *testing.T) {
	idx := NewRecallIndex()
	err := idx.BuildFromDisk("/nonexistent/path/steps")
	if err == nil {
		t.Error("expected error for nonexistent directory")
	}
}

// 35.4-UNIT-004: RecallIndex BuildFromDisk ignores corrupted steps.jsonl gracefully
func TestRecallIndex_BuildFromDisk_CorruptedStepsFile(t *testing.T) {
	dir := t.TempDir()

	// Create a valid process
	createStepsDir(t, dir, "uuid-good", typicalProcessSteps(
		"valid process",
		"tool_call", "complete",
	))

	// Create a corrupted steps.jsonl
	badDir := filepath.Join(dir, "data", "steps", "uuid-bad")
	if err := os.MkdirAll(badDir, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(badDir, "steps.jsonl"), []byte("not json at all\n{broken"), 0o644)

	idx := NewRecallIndex()
	err := idx.BuildFromDisk(filepath.Join(dir, "data", "steps"))
	// Should not fail entirely — gracefully skip bad files
	if err != nil {
		t.Fatalf("BuildFromDisk should gracefully skip corrupted files: %v", err)
	}
	if idx.ProcessCount() < 1 {
		t.Error("expected at least 1 indexed process (the valid one)")
	}
}

// 35.4-UNIT-005: RecallIndex background build does not block caller
func TestRecallIndex_BuildFromDiskAsync_NonBlocking(t *testing.T) {
	dir := t.TempDir()

	// Create multiple process directories
	for i := range 10 {
		createStepsDir(t, dir, fmt.Sprintf("uuid-%d", i), typicalProcessSteps(
			fmt.Sprintf("process %d doing work", i),
			"tool_call", "tool_call", "complete",
		))
	}

	idx := NewRecallIndex()

	// BuildFromDiskAsync should return immediately
	start := time.Now()
	idx.BuildFromDiskAsync(filepath.Join(dir, "data", "steps"))
	elapsed := time.Since(start)

	// Should return nearly instantly (not block while scanning)
	if elapsed > 500*time.Millisecond {
		t.Errorf("BuildFromDiskAsync blocked for %v, expected non-blocking", elapsed)
	}

	// Wait for background completion
	idx.WaitReady(5 * time.Second)

	if idx.ProcessCount() < 10 {
		t.Errorf("expected 10 indexed processes after async build, got %d", idx.ProcessCount())
	}
}

// =============================================================================
// Incremental Index Update Tests (AC-2)
// =============================================================================

// 35.4-UNIT-006: IndexProcess incrementally adds a new process to the index
func TestRecallIndex_IndexProcess_Incremental(t *testing.T) {
	dir := t.TempDir()

	// Build initial index with one process
	createStepsDir(t, dir, "uuid-1", typicalProcessSteps(
		"initial process yaml config",
		"tool_call", "complete",
	))

	idx := NewRecallIndex()
	if err := idx.BuildFromDisk(filepath.Join(dir, "data", "steps")); err != nil {
		t.Fatal(err)
	}
	initialCount := idx.ProcessCount()

	// Add new process directory and index incrementally
	newStepsDir := createStepsDir(t, dir, "uuid-2", typicalProcessSteps(
		"new process shell execution",
		"tool_call", "tool_call", "complete",
	))

	err := idx.IndexProcess("uuid-2", newStepsDir)
	if err != nil {
		t.Fatalf("IndexProcess failed: %v", err)
	}
	if idx.ProcessCount() != initialCount+1 {
		t.Errorf("expected %d processes after IndexProcess, got %d", initialCount+1, idx.ProcessCount())
	}
}

// 35.4-UNIT-007: IndexProcess with nonexistent steps directory returns error
func TestRecallIndex_IndexProcess_NonexistentDir(t *testing.T) {
	idx := NewRecallIndex()
	err := idx.IndexProcess("uuid-x", "/nonexistent/steps/dir")
	if err == nil {
		t.Error("expected error for nonexistent steps directory")
	}
}

// 35.4-UNIT-008: IndexProcess latency under 50ms for single process
func TestRecallIndex_IndexProcess_Latency(t *testing.T) {
	dir := t.TempDir()
	stepsDir := createStepsDir(t, dir, "uuid-perf", typicalProcessSteps(
		"performance test process with some content about yaml configuration and shell commands",
		"tool_call", "plan", "tool_call", "tool_call", "tool_call", "complete",
	))

	idx := NewRecallIndex()
	start := time.Now()
	err := idx.IndexProcess("uuid-perf", stepsDir)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("IndexProcess failed: %v", err)
	}
	if elapsed > 50*time.Millisecond {
		t.Errorf("IndexProcess latency %v exceeds 50ms NFR", elapsed)
	}
}

// 35.4-UNIT-009: IndexProcess is idempotent — re-indexing same UUID does not duplicate
func TestRecallIndex_IndexProcess_Idempotent(t *testing.T) {
	dir := t.TempDir()
	stepsDir := createStepsDir(t, dir, "uuid-dup", typicalProcessSteps(
		"duplicate check process",
		"tool_call", "complete",
	))

	idx := NewRecallIndex()
	_ = idx.IndexProcess("uuid-dup", stepsDir)
	countAfterFirst := idx.ProcessCount()

	_ = idx.IndexProcess("uuid-dup", stepsDir)
	countAfterSecond := idx.ProcessCount()

	if countAfterSecond != countAfterFirst {
		t.Errorf("expected idempotent IndexProcess: first=%d, second=%d", countAfterFirst, countAfterSecond)
	}
}

// =============================================================================
// Search Tests (AC-3)
// =============================================================================

// 35.4-UNIT-010: Search returns matching results for keyword query
func TestRecallIndex_Search_KeywordMatch(t *testing.T) {
	dir := t.TempDir()

	createStepsDir(t, dir, "uuid-yaml", typicalProcessSteps(
		"yaml config merge deep merge",
		"tool_call", "tool_call", "complete",
	))
	createStepsDir(t, dir, "uuid-shell", typicalProcessSteps(
		"shell command subprocess execution",
		"tool_call", "complete",
	))

	idx := NewRecallIndex()
	if err := idx.BuildFromDisk(filepath.Join(dir, "data", "steps")); err != nil {
		t.Fatal(err)
	}

	results := idx.Search("yaml config", 20)
	if len(results) == 0 {
		t.Error("expected search results for 'yaml config'")
	}

	// Results for "yaml" query should include the yaml process
	found := false
	for _, r := range results {
		if r.UUID == "uuid-yaml" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected uuid-yaml in search results for 'yaml config'")
	}
}

// 35.4-UNIT-011: Search returns empty results for non-matching query
func TestRecallIndex_Search_NoMatch(t *testing.T) {
	dir := t.TempDir()

	createStepsDir(t, dir, "uuid-1", typicalProcessSteps(
		"yaml config",
		"tool_call", "complete",
	))

	idx := NewRecallIndex()
	if err := idx.BuildFromDisk(filepath.Join(dir, "data", "steps")); err != nil {
		t.Fatal(err)
	}

	results := idx.Search("kubernetes deployment", 20)
	if len(results) != 0 {
		t.Errorf("expected 0 results for non-matching query, got %d", len(results))
	}
}

// 35.4-UNIT-012: Search respects maxResults limit
func TestRecallIndex_Search_MaxResults(t *testing.T) {
	dir := t.TempDir()

	// Create many processes with similar content
	for i := range 30 {
		createStepsDir(t, dir, fmt.Sprintf("uuid-%d", i), typicalProcessSteps(
			fmt.Sprintf("process %d common keyword shared", i),
			"tool_call", "complete",
		))
	}

	idx := NewRecallIndex()
	if err := idx.BuildFromDisk(filepath.Join(dir, "data", "steps")); err != nil {
		t.Fatal(err)
	}

	results := idx.Search("common keyword", 5)
	if len(results) > 5 {
		t.Errorf("expected at most 5 results, got %d", len(results))
	}
}

// 35.4-UNIT-013: Search results are ordered by recency (newest first)
func TestRecallIndex_Search_OrderedByRecency(t *testing.T) {
	dir := t.TempDir()

	// Create processes in known order
	for i := range 5 {
		createStepsDir(t, dir, fmt.Sprintf("uuid-ordered-%d", i), typicalProcessSteps(
			"recency test common content",
			"tool_call", "complete",
		))
		// Small delay to create different timestamps
		time.Sleep(5 * time.Millisecond)
	}

	idx := NewRecallIndex()
	if err := idx.BuildFromDisk(filepath.Join(dir, "data", "steps")); err != nil {
		t.Fatal(err)
	}

	results := idx.Search("recency test", 20)
	if len(results) < 2 {
		t.Fatal("need at least 2 results to verify ordering")
	}

	// Results should be ordered newest first (higher index = newer)
	for i := 1; i < len(results); i++ {
		if results[i].Timestamp.After(results[i-1].Timestamp) {
			t.Errorf("results not ordered by recency: result[%d].Time=%v > result[%d].Time=%v",
				i, results[i].Timestamp, i-1, results[i-1].Timestamp)
		}
	}
}

// 35.4-UNIT-014: Search latency under 500ms with 1000 indexed records
func TestRecallIndex_Search_Latency(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping latency test in short mode")
	}

	dir := t.TempDir()

	// Create 100 processes (representative of 1000 indexed records when each has ~10 steps)
	for i := range 100 {
		createStepsDir(t, dir, fmt.Sprintf("uuid-lat-%d", i), typicalProcessSteps(
			fmt.Sprintf("process %d with keyword-%d configuration and various tool calls", i, i%10),
			"tool_call", "plan", "tool_call", "tool_call", "complete",
		))
	}

	idx := NewRecallIndex()
	if err := idx.BuildFromDisk(filepath.Join(dir, "data", "steps")); err != nil {
		t.Fatal(err)
	}

	start := time.Now()
	results := idx.Search("configuration tool", 20)
	elapsed := time.Since(start)

	_ = results // use results to avoid compiler optimization
	if elapsed > 500*time.Millisecond {
		t.Errorf("Search latency %v exceeds 500ms NFR", elapsed)
	}
}

// 35.4-UNIT-015: Search with empty query returns empty results
func TestRecallIndex_Search_EmptyQuery(t *testing.T) {
	dir := t.TempDir()

	createStepsDir(t, dir, "uuid-1", typicalProcessSteps(
		"some content",
		"tool_call", "complete",
	))

	idx := NewRecallIndex()
	if err := idx.BuildFromDisk(filepath.Join(dir, "data", "steps")); err != nil {
		t.Fatal(err)
	}

	results := idx.Search("", 20)
	if len(results) != 0 {
		t.Errorf("expected 0 results for empty query, got %d", len(results))
	}
}

// 35.4-UNIT-016: Search performs keyword tokenization (multi-word query uses intersection)
func TestRecallIndex_Search_TokenIntersection(t *testing.T) {
	dir := t.TempDir()

	createStepsDir(t, dir, "uuid-yaml-only", typicalProcessSteps(
		"yaml parsing only",
		"tool_call", "complete",
	))
	createStepsDir(t, dir, "uuid-yaml-merge", typicalProcessSteps(
		"yaml config merge deep merge",
		"tool_call", "complete",
	))
	createStepsDir(t, dir, "uuid-merge-only", typicalProcessSteps(
		"git merge conflict resolution",
		"tool_call", "complete",
	))

	idx := NewRecallIndex()
	if err := idx.BuildFromDisk(filepath.Join(dir, "data", "steps")); err != nil {
		t.Fatal(err)
	}

	// "yaml merge" should match uuid-yaml-merge (has both keywords)
	results := idx.Search("yaml merge", 20)
	found := false
	for _, r := range results {
		if r.UUID == "uuid-yaml-merge" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected uuid-yaml-merge in search results for 'yaml merge'")
	}
}

// =============================================================================
// LLM Summary Tests (AC-4)
// =============================================================================

// 35.4-UNIT-017: SummarizeResults calls auxiliary LLM and returns summary
func TestRecallIndex_SummarizeResults_Normal(t *testing.T) {
	caller := &mockRecallLLMCaller{response: "Summary: yaml config merge is used for deep merging configurations"}

	results := []RecallResult{
		{UUID: "uuid-1", Summary: "yaml config merge deep merge", Timestamp: time.Now()},
		{UUID: "uuid-2", Summary: "config file processing", Timestamp: time.Now()},
	}

	summary, err := SummarizeRecallResults(context.Background(), caller, "yaml config", results)
	if err != nil {
		t.Fatalf("SummarizeRecallResults failed: %v", err)
	}
	if summary == "" {
		t.Error("expected non-empty summary")
	}
	if caller.CallCount() != 1 {
		t.Errorf("expected 1 LLM call, got %d", caller.CallCount())
	}
}

// 35.4-UNIT-018: SummarizeResults with LLM failure returns error
func TestRecallIndex_SummarizeResults_LLMFailure(t *testing.T) {
	caller := &mockRecallLLMCaller{err: fmt.Errorf("network timeout")}

	results := []RecallResult{
		{UUID: "uuid-1", Summary: "some content", Timestamp: time.Now()},
	}

	_, err := SummarizeRecallResults(context.Background(), caller, "query", results)
	if err == nil {
		t.Error("expected error when LLM fails")
	}
}

// 35.4-UNIT-019: SummarizeResults with empty results returns empty summary
func TestRecallIndex_SummarizeResults_EmptyResults(t *testing.T) {
	caller := &mockRecallLLMCaller{response: "no results"}

	summary, err := SummarizeRecallResults(context.Background(), caller, "query", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if summary != "" {
		t.Errorf("expected empty summary for empty results, got %q", summary)
	}
	if caller.CallCount() != 0 {
		t.Error("expected no LLM call for empty results")
	}
}

// 35.4-UNIT-020: SummarizeResults does not pollute main process context
func TestRecallIndex_SummarizeResults_IndependentContext(t *testing.T) {
	caller := &mockRecallLLMCaller{response: "independent summary"}

	results := []RecallResult{
		{UUID: "uuid-1", Summary: "content", Timestamp: time.Now()},
	}

	// Use a separate context to verify independence
	ctx := t.Context()

	summary, err := SummarizeRecallResults(ctx, caller, "query", results)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if summary == "" {
		t.Error("expected non-empty summary")
	}
}

// =============================================================================
// Config & Enable/Disable Tests (AC-6)
// =============================================================================

// 35.4-UNIT-021: RecallConfig enabled=true → index builds
func TestRecallConfig_Enabled(t *testing.T) {
	cfg := RecallConfig{Enabled: true, MaxResults: 20}
	if !cfg.Enabled {
		t.Error("expected RecallConfig.Enabled=true")
	}
	if cfg.MaxResults != 20 {
		t.Errorf("expected MaxResults=20, got %d", cfg.MaxResults)
	}
}

// 35.4-UNIT-022: RecallConfig enabled=false → ShouldBuildIndex returns false
func TestRecallConfig_Disabled_ShouldNotBuild(t *testing.T) {
	cfg := RecallConfig{Enabled: false}
	result := ShouldBuildRecallIndex(cfg)
	if result {
		t.Error("expected ShouldBuildRecallIndex=false when disabled")
	}
}

// 35.4-UNIT-023: RecallConfig enabled=true → ShouldBuildIndex returns true
func TestRecallConfig_Enabled_ShouldBuild(t *testing.T) {
	cfg := RecallConfig{Enabled: true}
	result := ShouldBuildRecallIndex(cfg)
	if !result {
		t.Error("expected ShouldBuildRecallIndex=true when enabled")
	}
}

// 35.4-UNIT-024: DefaultMemoryConfig has recall enabled with MaxResults=20
func TestDefaultMemoryConfig_RecallDefaults(t *testing.T) {
	cfg := DefaultMemoryConfig()
	if !cfg.Recall.Enabled {
		t.Error("expected default Recall.Enabled=true")
	}
	if cfg.Recall.MaxResults != 20 {
		t.Errorf("expected default Recall.MaxResults=20, got %d", cfg.Recall.MaxResults)
	}
}

// =============================================================================
// RecallResult Data Structure Tests
// =============================================================================

// 35.4-UNIT-025: RecallResult struct contains required fields (UUID, Summary, Timestamp)
func TestRecallResult_Structure(t *testing.T) {
	now := time.Now()
	r := RecallResult{
		UUID:      "test-uuid",
		Summary:   "test summary content",
		Timestamp: now,
	}
	if r.UUID != "test-uuid" {
		t.Errorf("expected UUID='test-uuid', got %q", r.UUID)
	}
	if r.Summary != "test summary content" {
		t.Errorf("expected Summary='test summary content', got %q", r.Summary)
	}
	if !r.Timestamp.Equal(now) {
		t.Errorf("expected Timestamp=%v, got %v", now, r.Timestamp)
	}
}

// =============================================================================
// Index Persistence Tests (Optional, AC-1 extended)
// =============================================================================

// 35.4-UNIT-026: SaveIndex persists index to disk as JSON
func TestRecallIndex_SaveIndex(t *testing.T) {
	dir := t.TempDir()

	createStepsDir(t, dir, "uuid-save", typicalProcessSteps(
		"persist test content",
		"tool_call", "complete",
	))

	idx := NewRecallIndex()
	if err := idx.BuildFromDisk(filepath.Join(dir, "data", "steps")); err != nil {
		t.Fatal(err)
	}

	indexPath := filepath.Join(dir, "data", "recall", "index.json")
	err := idx.SaveIndex(indexPath)
	if err != nil {
		t.Fatalf("SaveIndex failed: %v", err)
	}

	// Verify file exists and is valid JSON
	data, err := os.ReadFile(indexPath)
	if err != nil {
		t.Fatalf("failed to read saved index: %v", err)
	}
	if !json.Valid(data) {
		t.Error("saved index is not valid JSON")
	}
}

// 35.4-UNIT-027: LoadIndex restores index from disk
func TestRecallIndex_LoadIndex(t *testing.T) {
	dir := t.TempDir()

	createStepsDir(t, dir, "uuid-load", typicalProcessSteps(
		"load test content",
		"tool_call", "complete",
	))

	// Build and save
	idx1 := NewRecallIndex()
	if err := idx1.BuildFromDisk(filepath.Join(dir, "data", "steps")); err != nil {
		t.Fatal(err)
	}
	indexPath := filepath.Join(dir, "data", "recall", "index.json")
	if err := idx1.SaveIndex(indexPath); err != nil {
		t.Fatal(err)
	}

	// Load into new index
	idx2 := NewRecallIndex()
	err := idx2.LoadIndex(indexPath)
	if err != nil {
		t.Fatalf("LoadIndex failed: %v", err)
	}
	if idx2.ProcessCount() != idx1.ProcessCount() {
		t.Errorf("loaded index has %d processes, expected %d", idx2.ProcessCount(), idx1.ProcessCount())
	}

	// Search should still work after load
	results := idx2.Search("load test", 20)
	if len(results) == 0 {
		t.Error("expected search results after LoadIndex")
	}
}

// 35.4-UNIT-028: LoadIndex with nonexistent file returns error
func TestRecallIndex_LoadIndex_Nonexistent(t *testing.T) {
	idx := NewRecallIndex()
	err := idx.LoadIndex("/nonexistent/index.json")
	if err == nil {
		t.Error("expected error for nonexistent index file")
	}
}

// 35.4-UNIT-029: LoadIndex with corrupted file returns error
func TestRecallIndex_LoadIndex_Corrupted(t *testing.T) {
	dir := t.TempDir()
	indexPath := filepath.Join(dir, "index.json")
	os.WriteFile(indexPath, []byte("not json"), 0o644)

	idx := NewRecallIndex()
	err := idx.LoadIndex(indexPath)
	if err == nil {
		t.Error("expected error for corrupted index file")
	}
}

// =============================================================================
// Concurrency Safety Tests
// =============================================================================

// 35.4-UNIT-030: Concurrent Search and IndexProcess do not race
func TestRecallIndex_ConcurrentSearchAndIndex(t *testing.T) {
	dir := t.TempDir()

	// Seed with some initial data
	for i := range 5 {
		createStepsDir(t, dir, fmt.Sprintf("uuid-init-%d", i), typicalProcessSteps(
			"initial concurrent content",
			"tool_call", "complete",
		))
	}

	idx := NewRecallIndex()
	if err := idx.BuildFromDisk(filepath.Join(dir, "data", "steps")); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	const goroutines = 20

	// Half goroutines search, half add new processes
	for i := range goroutines {
		wg.Add(1)
		if i%2 == 0 {
			go func(n int) {
				defer wg.Done()
				_ = idx.Search("concurrent content", 10)
			}(i)
		} else {
			go func(n int) {
				defer wg.Done()
				sd := createStepsDir(t, dir, fmt.Sprintf("uuid-conc-%d", n), typicalProcessSteps(
					"concurrent add content",
					"tool_call", "complete",
				))
				_ = idx.IndexProcess(fmt.Sprintf("uuid-conc-%d", n), sd)
			}(i)
		}
	}

	wg.Wait()
	// No data race detected = success (run with -race)
}

// =============================================================================
// Keyword Tokenization Tests
// =============================================================================

// 35.4-UNIT-031: tokenizeQuery splits on whitespace and lowercases
func TestTokenizeQuery_Basic(t *testing.T) {
	tokens := tokenizeQuery("YAML Config Merge")
	if len(tokens) != 3 {
		t.Fatalf("expected 3 tokens, got %d: %v", len(tokens), tokens)
	}
	for _, tok := range tokens {
		if tok != strings.ToLower(tok) {
			t.Errorf("expected lowercase token, got %q", tok)
		}
	}
}

// 35.4-UNIT-032: tokenizeQuery deduplicates tokens
func TestTokenizeQuery_Dedup(t *testing.T) {
	tokens := tokenizeQuery("yaml yaml YAML config")
	seen := make(map[string]bool)
	for _, tok := range tokens {
		if seen[tok] {
			t.Errorf("duplicate token: %q", tok)
		}
		seen[tok] = true
	}
}

// 35.4-UNIT-033: tokenizeQuery filters empty tokens and short words
func TestTokenizeQuery_FilterShort(t *testing.T) {
	tokens := tokenizeQuery("  a  an  the  yaml  ")
	// Should filter out very short tokens (stop words)
	for _, tok := range tokens {
		if len(tok) <= 1 {
			t.Errorf("expected short token to be filtered: %q", tok)
		}
	}
	if len(tokens) == 0 {
		t.Error("expected at least 'yaml' token after filtering")
	}
}

// =============================================================================
// VFS Device ToolDef Metadata Tests (AC-5)
// =============================================================================

// 35.4-UNIT-034: RecallDeviceToolDef declares correct metadata
func TestRecallDeviceToolDef(t *testing.T) {
	def := RecallDeviceToolDef()
	if !def.IsReadOnly {
		t.Error("expected IsReadOnly=true for recall device")
	}
	if !def.IsConcurrencySafe {
		t.Error("expected IsConcurrencySafe=true for recall device")
	}
	if def.IsDestructive {
		t.Error("expected IsDestructive=false for recall device")
	}
	if !def.ShouldDefer {
		t.Error("expected ShouldDefer=true for recall device (non-core, deferred discovery)")
	}
}

// 35.4-UNIT-035: RecallDeviceToolDef has search hint
func TestRecallDeviceToolDef_SearchHint(t *testing.T) {
	def := RecallDeviceToolDef()
	if def.SearchHint == "" {
		t.Error("expected non-empty SearchHint for recall device")
	}
}
