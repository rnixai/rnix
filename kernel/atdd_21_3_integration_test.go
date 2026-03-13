package kernel

import (
	"testing"
	"time"
)

// ============================================================
// ATDD RED PHASE — Story 21.3: 声誉系统与自动选择
//
// 端到端集成测试：验证 SLA 结果记录 → 声誉查询 → 自动选择 完整链路。
// 测试引用 ReputationSummary, GetSummary, GetAllSummaries, SelectBest
// 等类型和方法，这些类型/方法尚不存在。
//
// RED → GREEN: 实现所有新增方法后，测试编译并通过。
// ============================================================

// --- 21.3-E2E-001: [P0] Record SLA results then query reputation summary (AC all) ---

func TestE2E_Reputation_RecordAndQuery(t *testing.T) {
	// Given: a ReputationStore and multiple SLA evaluations
	dir := t.TempDir()
	store := NewReputationStore(dir)

	// Record 5 passed, 2 failed
	for i := range 7 {
		result := &SLAResult{
			AgentName:   "e2e-agent",
			Passed:      i < 5,
			EvaluatedAt: time.Now(),
			TokensUsed:  5000 + i*1000,
			DurationMs:  int64(20000 + i*5000),
		}
		if err := store.RecordResult("e2e-agent", result); err != nil {
			t.Fatalf("RecordResult[%d] failed: %v", i, err)
		}
	}

	// When: querying reputation summary
	summary, err := store.GetSummary("e2e-agent")
	if err != nil {
		t.Fatalf("GetSummary failed: %v", err)
	}

	// Then: summary reflects the recorded data
	if summary.AgentName != "e2e-agent" {
		t.Fatalf("expected AgentName 'e2e-agent', got %q", summary.AgentName)
	}
	if summary.TotalRecords != 7 {
		t.Fatalf("expected TotalRecords 7, got %d", summary.TotalRecords)
	}
	// SuccessRate = 5/7 ≈ 0.714
	if summary.SuccessRate < 0.70 || summary.SuccessRate > 0.72 {
		t.Fatalf("expected SuccessRate ~0.714, got %f", summary.SuccessRate)
	}
	// Score should be in valid range
	if summary.Score < 0.0 || summary.Score > 1.0 {
		t.Fatalf("expected Score in [0,1], got %f", summary.Score)
	}
	// AvgTokens should be (5000+6000+7000+8000+9000+10000+11000)/7 = 8000
	if summary.AvgTokens != 8000 {
		t.Fatalf("expected AvgTokens 8000, got %d", summary.AvgTokens)
	}
}

// --- 21.3-E2E-002: [P0] Multiple agents - score calculation and ordering (AC all) ---

func TestE2E_Reputation_ScoreCalculation(t *testing.T) {
	dir := t.TempDir()
	store := NewReputationStore(dir)

	// "star-agent": 10/10 passed, low tokens
	for range 10 {
		if err := store.RecordResult("star-agent", &SLAResult{
			AgentName: "star-agent", Passed: true, EvaluatedAt: time.Now(),
			TokensUsed: 2000, DurationMs: 15000,
		}); err != nil {
			t.Fatalf("RecordResult failed: %v", err)
		}
	}

	// "avg-agent": 5/10 passed, medium tokens
	for i := range 10 {
		if err := store.RecordResult("avg-agent", &SLAResult{
			AgentName: "avg-agent", Passed: i < 5, EvaluatedAt: time.Now(),
			TokensUsed: 8000, DurationMs: 40000,
		}); err != nil {
			t.Fatalf("RecordResult failed: %v", err)
		}
	}

	// Verify ordering via GetAllSummaries
	summaries, err := store.GetAllSummaries()
	if err != nil {
		t.Fatalf("GetAllSummaries failed: %v", err)
	}
	if len(summaries) != 2 {
		t.Fatalf("expected 2 summaries, got %d", len(summaries))
	}

	// star-agent should have higher score
	if summaries[0].AgentName != "star-agent" {
		t.Fatalf("expected star-agent first (highest score), got %q", summaries[0].AgentName)
	}
	if summaries[0].Score <= summaries[1].Score {
		t.Fatalf("expected star-agent Score > avg-agent Score, got %f <= %f",
			summaries[0].Score, summaries[1].Score)
	}
}

// --- 21.3-E2E-003: [P1] Trend detection across record history (AC1) ---

func TestE2E_Reputation_TrendDetection(t *testing.T) {
	dir := t.TempDir()
	store := NewReputationStore(dir)

	// 10 failed records, then 5 passed -- trend should be "improving"
	for range 10 {
		if err := store.RecordResult("trend-agent", &SLAResult{
			AgentName: "trend-agent", Passed: false, EvaluatedAt: time.Now(),
			TokensUsed: 15000, DurationMs: 60000,
		}); err != nil {
			t.Fatalf("RecordResult failed: %v", err)
		}
	}
	for range 5 {
		if err := store.RecordResult("trend-agent", &SLAResult{
			AgentName: "trend-agent", Passed: true, EvaluatedAt: time.Now(),
			TokensUsed: 3000, DurationMs: 20000,
		}); err != nil {
			t.Fatalf("RecordResult failed: %v", err)
		}
	}

	summary, err := store.GetSummary("trend-agent")
	if err != nil {
		t.Fatalf("GetSummary failed: %v", err)
	}
	if summary.RecentTrend != "improving" {
		t.Fatalf("expected trend 'improving', got %q", summary.RecentTrend)
	}
}

// --- 21.3-E2E-004: [P0] Auto-select uses recorded reputation data (AC4) ---

func TestE2E_Reputation_AutoSelect(t *testing.T) {
	dir := t.TempDir()
	store := NewReputationStore(dir)

	// Record different quality for different agents
	// "elite": 10/10 passed
	for range 10 {
		if err := store.RecordResult("elite", &SLAResult{
			AgentName: "elite", Passed: true, EvaluatedAt: time.Now(),
			TokensUsed: 2000, DurationMs: 15000,
		}); err != nil {
			t.Fatalf("RecordResult failed: %v", err)
		}
	}

	// "mediocre": 3/10 passed
	for i := range 10 {
		if err := store.RecordResult("mediocre", &SLAResult{
			AgentName: "mediocre", Passed: i < 3, EvaluatedAt: time.Now(),
			TokensUsed: 12000, DurationMs: 55000,
		}); err != nil {
			t.Fatalf("RecordResult failed: %v", err)
		}
	}

	// "rookie": no records (default score 0.5)

	// When: selecting best from all three
	best, err := store.SelectBest([]string{"mediocre", "rookie", "elite"})
	if err != nil {
		t.Fatalf("SelectBest failed: %v", err)
	}

	// Then: elite should be selected (highest score)
	if best != "elite" {
		t.Fatalf("expected 'elite' to be selected, got %q", best)
	}
}
