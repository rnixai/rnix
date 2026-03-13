package kernel

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// ============================================================
// ATDD RED PHASE — Story 21.2: 合约 SLA 与评估
//
// ReputationStore 声誉持久化测试。
// 测试引用 ReputationStore, ReputationRecord 等类型和方法，
// 这些类型尚不存在。测试将无法编译直到实现完成。
//
// RED → GREEN: 实现 kernel/reputation.go 中的类型和方法，测试编译并通过。
// ============================================================

// --- 21.2-UNIT-017: [P0] ReputationStore RecordResult then GetHistory (AC3) ---

func TestReputationStore_RecordResult(t *testing.T) {
	// Given: a ReputationStore with a temp directory
	dir := t.TempDir()
	store := NewReputationStore(dir)

	slaResult := &SLAResult{
		AgentName:   "reviewer",
		Passed:      true,
		Checks:      []SLACheckResult{{Name: "max_tokens", Passed: true, Actual: "5000", Limit: "10000"}},
		EvaluatedAt: time.Now(),
		TokensUsed:  5000,
		DurationMs:  30000,
	}

	// When: recording a result
	err := store.RecordResult("reviewer", slaResult)
	if err != nil {
		t.Fatalf("RecordResult failed: %v", err)
	}

	// Then: GetHistory returns the recorded result
	history, err := store.GetHistory("reviewer")
	if err != nil {
		t.Fatalf("GetHistory failed: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("expected 1 record, got %d", len(history))
	}
	if history[0].SLAResult.AgentName != "reviewer" {
		t.Fatalf("expected agent name 'reviewer', got %q", history[0].SLAResult.AgentName)
	}
	if !history[0].SLAResult.Passed {
		t.Fatal("expected SLAResult.Passed to be true")
	}
	if history[0].Timestamp.IsZero() {
		t.Fatal("expected non-zero timestamp")
	}
}

// --- 21.2-UNIT-018: [P0] ReputationStore multiple records append correctly (AC3) ---

func TestReputationStore_RecordResult_Multiple(t *testing.T) {
	dir := t.TempDir()
	store := NewReputationStore(dir)

	// Record 3 results
	for i := range 3 {
		passed := i%2 == 0
		result := &SLAResult{
			AgentName:   "agent-a",
			Passed:      passed,
			Checks:      []SLACheckResult{{Name: "max_tokens", Passed: passed, Actual: "1000", Limit: "5000"}},
			EvaluatedAt: time.Now(),
			TokensUsed:  1000 * (i + 1),
			DurationMs:  int64(10000 * (i + 1)),
		}
		if err := store.RecordResult("agent-a", result); err != nil {
			t.Fatalf("RecordResult[%d] failed: %v", i, err)
		}
	}

	// Verify all 3 records
	history, err := store.GetHistory("agent-a")
	if err != nil {
		t.Fatalf("GetHistory failed: %v", err)
	}
	if len(history) != 3 {
		t.Fatalf("expected 3 records, got %d", len(history))
	}

	// Verify token usage order matches insertion order
	if history[0].SLAResult.TokensUsed != 1000 {
		t.Fatalf("expected first record TokensUsed 1000, got %d", history[0].SLAResult.TokensUsed)
	}
	if history[2].SLAResult.TokensUsed != 3000 {
		t.Fatalf("expected third record TokensUsed 3000, got %d", history[2].SLAResult.TokensUsed)
	}
}

// --- 21.2-UNIT-019: [P0] ReputationStore GetHistory no file returns empty (AC3/AC5) ---

func TestReputationStore_GetHistory_NoFile(t *testing.T) {
	dir := t.TempDir()
	store := NewReputationStore(dir)

	// When: getting history for an agent with no file
	history, err := store.GetHistory("nonexistent-agent")

	// Then: returns empty slice with no error
	if err != nil {
		t.Fatalf("expected no error for missing file, got: %v", err)
	}
	if history == nil {
		t.Fatal("expected non-nil empty slice, got nil")
	}
	if len(history) != 0 {
		t.Fatalf("expected 0 records, got %d", len(history))
	}
}

// --- 21.2-UNIT-020: [P1] ReputationStore auto-creates directory (AC3) ---

func TestReputationStore_RecordResult_CreateDir(t *testing.T) {
	// Given: a base dir that doesn't exist yet
	dir := filepath.Join(t.TempDir(), "nested", "reputation")
	store := NewReputationStore(dir)

	result := &SLAResult{
		AgentName:   "agent-x",
		Passed:      true,
		Checks:      nil,
		EvaluatedAt: time.Now(),
		TokensUsed:  100,
		DurationMs:  500,
	}

	// When: recording result (directory should be auto-created)
	err := store.RecordResult("agent-x", result)
	if err != nil {
		t.Fatalf("RecordResult should auto-create directory: %v", err)
	}

	// Then: directory exists and file was created
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Fatal("expected directory to be auto-created")
	}

	history, err := store.GetHistory("agent-x")
	if err != nil {
		t.Fatalf("GetHistory failed: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("expected 1 record after auto-create, got %d", len(history))
	}
}

// --- 21.2-UNIT-021: [P1] ReputationStore concurrent writes are safe (AC3) ---

func TestReputationStore_ConcurrentWrite(t *testing.T) {
	dir := t.TempDir()
	store := NewReputationStore(dir)

	const goroutines = 20
	var wg sync.WaitGroup

	for i := range goroutines {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			result := &SLAResult{
				AgentName:   "concurrent-agent",
				Passed:      true,
				Checks:      []SLACheckResult{{Name: "max_tokens", Passed: true, Actual: "100", Limit: "1000"}},
				EvaluatedAt: time.Now(),
				TokensUsed:  100 * (idx + 1),
				DurationMs:  int64(1000 * (idx + 1)),
			}
			if err := store.RecordResult("concurrent-agent", result); err != nil {
				t.Errorf("concurrent RecordResult[%d] failed: %v", idx, err)
			}
		}(i)
	}

	wg.Wait()

	// Verify all records were written
	history, err := store.GetHistory("concurrent-agent")
	if err != nil {
		t.Fatalf("GetHistory failed: %v", err)
	}
	if len(history) != goroutines {
		t.Fatalf("expected %d records from concurrent writes, got %d", goroutines, len(history))
	}
}

// --- 21.2-UNIT-022: [P1] ReputationStore isolates agents by file (AC3) ---

func TestReputationStore_AgentIsolation(t *testing.T) {
	dir := t.TempDir()
	store := NewReputationStore(dir)

	// Record for two different agents
	resultA := &SLAResult{AgentName: "agent-a", Passed: true, EvaluatedAt: time.Now(), TokensUsed: 100, DurationMs: 1000}
	resultB := &SLAResult{AgentName: "agent-b", Passed: false, EvaluatedAt: time.Now(), TokensUsed: 200, DurationMs: 2000}

	if err := store.RecordResult("agent-a", resultA); err != nil {
		t.Fatalf("RecordResult(agent-a) failed: %v", err)
	}
	if err := store.RecordResult("agent-b", resultB); err != nil {
		t.Fatalf("RecordResult(agent-b) failed: %v", err)
	}

	// Verify isolation
	histA, _ := store.GetHistory("agent-a")
	histB, _ := store.GetHistory("agent-b")

	if len(histA) != 1 {
		t.Fatalf("expected 1 record for agent-a, got %d", len(histA))
	}
	if len(histB) != 1 {
		t.Fatalf("expected 1 record for agent-b, got %d", len(histB))
	}
	if histA[0].SLAResult.AgentName != "agent-a" {
		t.Fatalf("agent-a history contains wrong agent: %q", histA[0].SLAResult.AgentName)
	}
	if histB[0].SLAResult.AgentName != "agent-b" {
		t.Fatalf("agent-b history contains wrong agent: %q", histB[0].SLAResult.AgentName)
	}
}

// --- 21.2-UNIT-023: [P2] ReputationStore file uses JSON Lines format (AC3) ---

func TestReputationStore_JSONLinesFormat(t *testing.T) {
	dir := t.TempDir()
	store := NewReputationStore(dir)

	// Record two results
	for i := range 2 {
		result := &SLAResult{
			AgentName:   "format-test",
			Passed:      true,
			EvaluatedAt: time.Now(),
			TokensUsed:  1000 * (i + 1),
			DurationMs:  int64(5000 * (i + 1)),
		}
		if err := store.RecordResult("format-test", result); err != nil {
			t.Fatalf("RecordResult[%d] failed: %v", i, err)
		}
	}

	// Read the raw file and verify it's JSON Lines (each line is valid JSON)
	filePath := filepath.Join(dir, "format-test.json")
	data, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read reputation file: %v", err)
	}

	content := string(data)
	if content == "" {
		t.Fatal("expected non-empty reputation file")
	}

	// Count lines (JSON Lines = one JSON object per line)
	lines := 0
	for _, line := range splitLines(content) {
		if line != "" {
			lines++
		}
	}
	if lines != 2 {
		t.Fatalf("expected 2 JSON lines, got %d", lines)
	}
}

// splitLines splits a string by newlines, filtering empty trailing lines.
func splitLines(s string) []string {
	var result []string
	start := 0
	for i := range len(s) {
		if s[i] == '\n' {
			line := s[start:i]
			if line != "" {
				result = append(result, line)
			}
			start = i + 1
		}
	}
	if start < len(s) {
		result = append(result, s[start:])
	}
	return result
}
