package kernel

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ============================================================
// ATDD RED PHASE — Story 21.3: 声誉系统与自动选择
//
// ReputationStore 声誉分数计算测试。
// 测试引用 ReputationSummary, GetSummary, ListAgents, GetAllSummaries,
// SelectBest 等类型和方法，这些类型/方法尚不存在。
// 测试将无法编译直到实现完成。
//
// RED → GREEN: 实现 kernel/reputation.go 中的新类型和方法，测试编译并通过。
// ============================================================

// --- 21.3-UNIT-001: [P0] GetSummary no records returns default neutral summary (AC1/AC5) ---

func TestReputationStore_GetSummary_NoRecords(t *testing.T) {
	// Given: a ReputationStore with no records for the agent
	dir := t.TempDir()
	store := NewReputationStore(dir)

	// When: getting summary for an agent with no history
	summary, err := store.GetSummary("unknown-agent")

	// Then: returns default neutral summary (Score=0.5, TotalRecords=0, Trend="stable")
	if err != nil {
		t.Fatalf("GetSummary failed: %v", err)
	}
	if summary == nil {
		t.Fatal("expected non-nil summary for agent with no records")
	}
	if summary.AgentName != "unknown-agent" {
		t.Fatalf("expected AgentName 'unknown-agent', got %q", summary.AgentName)
	}
	if summary.Score != 0.5 {
		t.Fatalf("expected default Score 0.5, got %f", summary.Score)
	}
	if summary.TotalRecords != 0 {
		t.Fatalf("expected TotalRecords 0, got %d", summary.TotalRecords)
	}
	if summary.RecentTrend != "stable" {
		t.Fatalf("expected RecentTrend 'stable', got %q", summary.RecentTrend)
	}
}

// --- 21.3-UNIT-002: [P0] GetSummary all passed - Score close to 1.0 (AC1) ---

func TestReputationStore_GetSummary_AllPassed(t *testing.T) {
	// Given: an agent with all-passed SLA evaluations
	dir := t.TempDir()
	store := NewReputationStore(dir)

	for i := range 10 {
		result := &SLAResult{
			AgentName:   "perfect-agent",
			Passed:      true,
			Checks:      []SLACheckResult{{Name: "max_tokens", Passed: true, Actual: "5000", Limit: "10000"}},
			EvaluatedAt: time.Now(),
			TokensUsed:  5000,
			DurationMs:  int64(30000 + i*100),
		}
		if err := store.RecordResult("perfect-agent", result); err != nil {
			t.Fatalf("RecordResult[%d] failed: %v", i, err)
		}
	}

	// When: getting summary
	summary, err := store.GetSummary("perfect-agent")

	// Then: Score should be high (close to 1.0), SuccessRate = 1.0
	if err != nil {
		t.Fatalf("GetSummary failed: %v", err)
	}
	if summary.SuccessRate != 1.0 {
		t.Fatalf("expected SuccessRate 1.0, got %f", summary.SuccessRate)
	}
	if summary.Score < 0.9 {
		t.Fatalf("expected Score >= 0.9 for all-passed agent, got %f", summary.Score)
	}
	if summary.TotalRecords != 10 {
		t.Fatalf("expected TotalRecords 10, got %d", summary.TotalRecords)
	}
	if summary.AvgTokens != 5000 {
		t.Fatalf("expected AvgTokens 5000, got %d", summary.AvgTokens)
	}
}

// --- 21.3-UNIT-003: [P0] GetSummary mixed results - Score between 0 and 1 (AC1) ---

func TestReputationStore_GetSummary_MixedResults(t *testing.T) {
	// Given: an agent with mixed SLA results (7 passed, 3 failed)
	dir := t.TempDir()
	store := NewReputationStore(dir)

	for i := range 10 {
		passed := i < 7 // first 7 pass, last 3 fail
		tokens := 5000
		if !passed {
			tokens = 15000 // failed agents used more tokens
		}
		result := &SLAResult{
			AgentName:   "mixed-agent",
			Passed:      passed,
			Checks:      []SLACheckResult{{Name: "max_tokens", Passed: passed, Actual: "5000", Limit: "10000"}},
			EvaluatedAt: time.Now(),
			TokensUsed:  tokens,
			DurationMs:  30000,
		}
		if err := store.RecordResult("mixed-agent", result); err != nil {
			t.Fatalf("RecordResult[%d] failed: %v", i, err)
		}
	}

	// When: getting summary
	summary, err := store.GetSummary("mixed-agent")

	// Then: Score in (0,1), SuccessRate = 0.7
	if err != nil {
		t.Fatalf("GetSummary failed: %v", err)
	}
	if summary.SuccessRate < 0.69 || summary.SuccessRate > 0.71 {
		t.Fatalf("expected SuccessRate ~0.7, got %f", summary.SuccessRate)
	}
	if summary.Score <= 0.0 || summary.Score >= 1.0 {
		t.Fatalf("expected Score in (0,1), got %f", summary.Score)
	}
	if summary.TotalRecords != 10 {
		t.Fatalf("expected TotalRecords 10, got %d", summary.TotalRecords)
	}
}

// --- 21.3-UNIT-004: [P1] GetSummary RecentTrend improving (AC1) ---

func TestReputationStore_GetSummary_RecentTrend_Improving(t *testing.T) {
	// Given: an agent whose recent 5 records are mostly passed but overall is mixed
	dir := t.TempDir()
	store := NewReputationStore(dir)

	// First 10 records: all failed
	for i := range 10 {
		result := &SLAResult{
			AgentName:   "improving-agent",
			Passed:      false,
			EvaluatedAt: time.Now(),
			TokensUsed:  10000 + i*100,
			DurationMs:  50000,
		}
		if err := store.RecordResult("improving-agent", result); err != nil {
			t.Fatalf("RecordResult[%d] failed: %v", i, err)
		}
	}

	// Recent 5 records: all passed
	for i := range 5 {
		result := &SLAResult{
			AgentName:   "improving-agent",
			Passed:      true,
			EvaluatedAt: time.Now(),
			TokensUsed:  3000 + i*100,
			DurationMs:  20000,
		}
		if err := store.RecordResult("improving-agent", result); err != nil {
			t.Fatalf("RecordResult[%d] failed: %v", i, err)
		}
	}

	// When: getting summary
	summary, err := store.GetSummary("improving-agent")

	// Then: RecentTrend should be "improving"
	if err != nil {
		t.Fatalf("GetSummary failed: %v", err)
	}
	if summary.RecentTrend != "improving" {
		t.Fatalf("expected RecentTrend 'improving', got %q", summary.RecentTrend)
	}
}

// --- 21.3-UNIT-005: [P1] GetSummary RecentTrend declining (AC1) ---

func TestReputationStore_GetSummary_RecentTrend_Declining(t *testing.T) {
	// Given: an agent whose recent 5 records are mostly failed but overall is mostly passed
	dir := t.TempDir()
	store := NewReputationStore(dir)

	// First 10 records: all passed
	for i := range 10 {
		result := &SLAResult{
			AgentName:   "declining-agent",
			Passed:      true,
			EvaluatedAt: time.Now(),
			TokensUsed:  3000 + i*100,
			DurationMs:  20000,
		}
		if err := store.RecordResult("declining-agent", result); err != nil {
			t.Fatalf("RecordResult[%d] failed: %v", i, err)
		}
	}

	// Recent 5 records: all failed
	for i := range 5 {
		result := &SLAResult{
			AgentName:   "declining-agent",
			Passed:      false,
			EvaluatedAt: time.Now(),
			TokensUsed:  15000 + i*100,
			DurationMs:  80000,
		}
		if err := store.RecordResult("declining-agent", result); err != nil {
			t.Fatalf("RecordResult[%d] failed: %v", i, err)
		}
	}

	// When: getting summary
	summary, err := store.GetSummary("declining-agent")

	// Then: RecentTrend should be "declining"
	if err != nil {
		t.Fatalf("GetSummary failed: %v", err)
	}
	if summary.RecentTrend != "declining" {
		t.Fatalf("expected RecentTrend 'declining', got %q", summary.RecentTrend)
	}
}

// --- 21.3-UNIT-006: [P0] ListAgents returns all agents with files (AC1) ---

func TestReputationStore_ListAgents(t *testing.T) {
	// Given: a ReputationStore with records for 3 agents
	dir := t.TempDir()
	store := NewReputationStore(dir)

	agentNames := []string{"alpha", "beta", "gamma"}
	for _, name := range agentNames {
		result := &SLAResult{
			AgentName:   name,
			Passed:      true,
			EvaluatedAt: time.Now(),
			TokensUsed:  1000,
			DurationMs:  5000,
		}
		if err := store.RecordResult(name, result); err != nil {
			t.Fatalf("RecordResult(%s) failed: %v", name, err)
		}
	}

	// When: listing agents
	agents, err := store.ListAgents()

	// Then: all 3 agents are listed
	if err != nil {
		t.Fatalf("ListAgents failed: %v", err)
	}
	if len(agents) != 3 {
		t.Fatalf("expected 3 agents, got %d", len(agents))
	}

	// Verify all agent names are present (order may vary)
	nameSet := make(map[string]bool)
	for _, a := range agents {
		nameSet[a] = true
	}
	for _, name := range agentNames {
		if !nameSet[name] {
			t.Fatalf("expected agent %q in list, not found", name)
		}
	}
}

// --- 21.3-UNIT-007: [P1] ListAgents no directory returns empty (AC5) ---

func TestReputationStore_ListAgents_NoDir(t *testing.T) {
	// Given: a ReputationStore with a non-existent base directory
	store := NewReputationStore(filepath.Join(t.TempDir(), "nonexistent"))

	// When: listing agents
	agents, err := store.ListAgents()

	// Then: returns empty slice without error
	if err != nil {
		t.Fatalf("ListAgents should not error for missing dir: %v", err)
	}
	if agents == nil {
		t.Fatal("expected non-nil empty slice, got nil")
	}
	if len(agents) != 0 {
		t.Fatalf("expected 0 agents, got %d", len(agents))
	}
}

// --- 21.3-UNIT-008: [P0] GetAllSummaries returns sorted by Score desc (AC1) ---

func TestReputationStore_GetAllSummaries(t *testing.T) {
	// Given: agents with different performance levels
	dir := t.TempDir()
	store := NewReputationStore(dir)

	// "good-agent": all passed, low tokens
	for range 5 {
		result := &SLAResult{
			AgentName:   "good-agent",
			Passed:      true,
			EvaluatedAt: time.Now(),
			TokensUsed:  3000,
			DurationMs:  20000,
		}
		if err := store.RecordResult("good-agent", result); err != nil {
			t.Fatalf("RecordResult failed: %v", err)
		}
	}

	// "bad-agent": all failed, high tokens
	for range 5 {
		result := &SLAResult{
			AgentName:   "bad-agent",
			Passed:      false,
			EvaluatedAt: time.Now(),
			TokensUsed:  20000,
			DurationMs:  80000,
		}
		if err := store.RecordResult("bad-agent", result); err != nil {
			t.Fatalf("RecordResult failed: %v", err)
		}
	}

	// When: getting all summaries
	summaries, err := store.GetAllSummaries()

	// Then: sorted by Score descending, good-agent first
	if err != nil {
		t.Fatalf("GetAllSummaries failed: %v", err)
	}
	if len(summaries) != 2 {
		t.Fatalf("expected 2 summaries, got %d", len(summaries))
	}
	if summaries[0].AgentName != "good-agent" {
		t.Fatalf("expected first summary to be 'good-agent', got %q", summaries[0].AgentName)
	}
	if summaries[1].AgentName != "bad-agent" {
		t.Fatalf("expected second summary to be 'bad-agent', got %q", summaries[1].AgentName)
	}
	if summaries[0].Score <= summaries[1].Score {
		t.Fatalf("expected good-agent Score > bad-agent Score, got %f <= %f", summaries[0].Score, summaries[1].Score)
	}
}

// --- 21.3-UNIT-009: [P0] SelectBest returns highest-scored agent (AC4) ---

func TestReputationStore_SelectBest_HighestScore(t *testing.T) {
	// Given: agents with different scores
	dir := t.TempDir()
	store := NewReputationStore(dir)

	// "best-agent": all passed
	for range 5 {
		if err := store.RecordResult("best-agent", &SLAResult{
			AgentName: "best-agent", Passed: true, EvaluatedAt: time.Now(), TokensUsed: 3000, DurationMs: 20000,
		}); err != nil {
			t.Fatalf("RecordResult failed: %v", err)
		}
	}

	// "mid-agent": mixed
	for i := range 5 {
		if err := store.RecordResult("mid-agent", &SLAResult{
			AgentName: "mid-agent", Passed: i < 3, EvaluatedAt: time.Now(), TokensUsed: 8000, DurationMs: 40000,
		}); err != nil {
			t.Fatalf("RecordResult failed: %v", err)
		}
	}

	// "worst-agent": all failed
	for range 5 {
		if err := store.RecordResult("worst-agent", &SLAResult{
			AgentName: "worst-agent", Passed: false, EvaluatedAt: time.Now(), TokensUsed: 20000, DurationMs: 80000,
		}); err != nil {
			t.Fatalf("RecordResult failed: %v", err)
		}
	}

	// When: selecting best from candidates
	best, err := store.SelectBest([]string{"worst-agent", "mid-agent", "best-agent"})

	// Then: best-agent is selected
	if err != nil {
		t.Fatalf("SelectBest failed: %v", err)
	}
	if best != "best-agent" {
		t.Fatalf("expected 'best-agent', got %q", best)
	}
}

// --- 21.3-UNIT-010: [P0] SelectBest all default scores returns first candidate (AC4/AC5) ---

func TestReputationStore_SelectBest_AllDefault(t *testing.T) {
	// Given: no reputation data for any candidate (all get default 0.5)
	dir := t.TempDir()
	store := NewReputationStore(dir)

	// When: selecting best from candidates with no history
	best, err := store.SelectBest([]string{"agent-x", "agent-y", "agent-z"})

	// Then: returns first candidate (deterministic)
	if err != nil {
		t.Fatalf("SelectBest failed: %v", err)
	}
	if best != "agent-x" {
		t.Fatalf("expected first candidate 'agent-x' when all have default score, got %q", best)
	}
}

// --- 21.3-UNIT-011: [P0] SelectBest empty candidates returns error (AC4) ---

func TestReputationStore_SelectBest_EmptyCandidates(t *testing.T) {
	dir := t.TempDir()
	store := NewReputationStore(dir)

	// When: calling SelectBest with empty list
	_, err := store.SelectBest([]string{})

	// Then: returns error
	if err == nil {
		t.Fatal("expected error for empty candidates list")
	}
}

// --- 21.3-UNIT-012: [P1] GetSummary AvgDurationMs calculated correctly (AC1) ---

func TestReputationStore_GetSummary_AvgDurationMs(t *testing.T) {
	dir := t.TempDir()
	store := NewReputationStore(dir)

	durations := []int64{10000, 20000, 30000, 40000, 50000}
	for _, d := range durations {
		result := &SLAResult{
			AgentName:   "duration-agent",
			Passed:      true,
			EvaluatedAt: time.Now(),
			TokensUsed:  5000,
			DurationMs:  d,
		}
		if err := store.RecordResult("duration-agent", result); err != nil {
			t.Fatalf("RecordResult failed: %v", err)
		}
	}

	summary, err := store.GetSummary("duration-agent")
	if err != nil {
		t.Fatalf("GetSummary failed: %v", err)
	}

	// AvgDurationMs should be (10000+20000+30000+40000+50000)/5 = 30000
	if summary.AvgDurationMs != 30000 {
		t.Fatalf("expected AvgDurationMs 30000, got %d", summary.AvgDurationMs)
	}
}

// --- 21.3-UNIT-013: [P2] GetSummary token efficiency when all tokens equal (AC1) ---

func TestReputationStore_GetSummary_TokenEfficiency_AllEqual(t *testing.T) {
	// Given: all records have the same token usage
	dir := t.TempDir()
	store := NewReputationStore(dir)

	for range 5 {
		result := &SLAResult{
			AgentName:   "equal-token-agent",
			Passed:      true,
			EvaluatedAt: time.Now(),
			TokensUsed:  5000,
			DurationMs:  20000,
		}
		if err := store.RecordResult("equal-token-agent", result); err != nil {
			t.Fatalf("RecordResult failed: %v", err)
		}
	}

	summary, err := store.GetSummary("equal-token-agent")
	if err != nil {
		t.Fatalf("GetSummary failed: %v", err)
	}

	// When all tokens are equal, token efficiency = 1.0
	// Score = 0.7 * 1.0 + 0.3 * 1.0 = 1.0
	if summary.Score != 1.0 {
		t.Fatalf("expected Score 1.0 when all passed with equal tokens, got %f", summary.Score)
	}
}

// --- 21.3-UNIT-014: [P1] ListAgents ignores non-.json files (AC1) ---

func TestReputationStore_ListAgents_IgnoresNonJSON(t *testing.T) {
	// Given: a directory with both .json files and other files
	dir := t.TempDir()
	store := NewReputationStore(dir)

	// Create a valid reputation record
	if err := store.RecordResult("valid-agent", &SLAResult{
		AgentName: "valid-agent", Passed: true, EvaluatedAt: time.Now(), TokensUsed: 1000, DurationMs: 5000,
	}); err != nil {
		t.Fatalf("RecordResult failed: %v", err)
	}

	// Create a non-.json file in the same directory
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("not a reputation file"), 0o644); err != nil {
		t.Fatalf("WriteFile failed: %v", err)
	}

	// When: listing agents
	agents, err := store.ListAgents()

	// Then: only returns the valid agent
	if err != nil {
		t.Fatalf("ListAgents failed: %v", err)
	}
	if len(agents) != 1 {
		t.Fatalf("expected 1 agent (ignoring non-.json), got %d", len(agents))
	}
	if agents[0] != "valid-agent" {
		t.Fatalf("expected 'valid-agent', got %q", agents[0])
	}
}
