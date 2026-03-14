package kernel

import (
	"sync"
	"testing"

	"github.com/rnixai/rnix/internal/types"
)

// ============================================================
// ATDD RED PHASE — Story 22.4: 能力迁移与相似度矩阵
//
// SimilarityMatrix, CapabilitySimilarity, MigrationResult 以及
// ImmuneDaemon 集成相似度矩阵和能力迁移引擎的测试。
//
// 测试引用的类型和方法尚不存在，测试将无法编译直到实现完成。
//
// RED → GREEN: 在 kernel/immune.go 中实现所有新增类型和方法。
// ============================================================

// --- 22.4-UNIT-001: [P0] SimilarityMatrix Compute basic skill overlap (AC1) ---

func TestSimilarityMatrix_Compute_BasicSkillOverlap(t *testing.T) {
	// Given: two agents with partially overlapping skills
	agents := map[string][]string{
		"code-analyst":  {"code-analysis", "testing", "debugging"},
		"code-reviewer": {"code-analysis", "testing", "documentation"},
	}
	coopHistory := map[string]map[string]int{} // no cooperation history

	// When: computing the similarity matrix
	matrix := NewSimilarityMatrix()
	matrix.Compute(agents, coopHistory)

	// Then: Jaccard similarity = |{code-analysis, testing}| / |{code-analysis, testing, debugging, documentation}| = 2/4 = 0.5
	sim := matrix.Get("code-analyst", "code-reviewer")
	if sim == nil {
		t.Fatal("expected non-nil CapabilitySimilarity between code-analyst and code-reviewer")
	}
	if sim.SkillScore != 0.5 {
		t.Errorf("SkillScore = %f, want 0.5", sim.SkillScore)
	}
	if sim.CoopScore != 0.0 {
		t.Errorf("CoopScore = %f, want 0.0 (no cooperation history)", sim.CoopScore)
	}
	// Score = 0.7 * 0.5 + 0.3 * 0.0 = 0.35
	expectedScore := 0.7*0.5 + 0.3*0.0
	if sim.Score != expectedScore {
		t.Errorf("Score = %f, want %f", sim.Score, expectedScore)
	}
}

// --- 22.4-UNIT-002: [P0] SimilarityMatrix Compute no overlap (AC1) ---

func TestSimilarityMatrix_Compute_NoOverlap(t *testing.T) {
	// Given: two agents with completely different skills
	agents := map[string][]string{
		"frontend-dev": {"react", "css", "html"},
		"dba":          {"sql", "postgres", "backup"},
	}
	coopHistory := map[string]map[string]int{}

	// When: computing the similarity matrix
	matrix := NewSimilarityMatrix()
	matrix.Compute(agents, coopHistory)

	// Then: Jaccard similarity = 0/6 = 0.0
	sim := matrix.Get("frontend-dev", "dba")
	if sim == nil {
		t.Fatal("expected non-nil CapabilitySimilarity")
	}
	if sim.SkillScore != 0.0 {
		t.Errorf("SkillScore = %f, want 0.0", sim.SkillScore)
	}
	if sim.Score != 0.0 {
		t.Errorf("Score = %f, want 0.0", sim.Score)
	}
}

// --- 22.4-UNIT-003: [P0] SimilarityMatrix Compute identical skills (AC1) ---

func TestSimilarityMatrix_Compute_IdenticalSkills(t *testing.T) {
	// Given: two agents with identical skills
	agents := map[string][]string{
		"agent-a": {"code-analysis", "testing"},
		"agent-b": {"code-analysis", "testing"},
	}
	coopHistory := map[string]map[string]int{}

	// When: computing the similarity matrix
	matrix := NewSimilarityMatrix()
	matrix.Compute(agents, coopHistory)

	// Then: Jaccard similarity = 2/2 = 1.0
	sim := matrix.Get("agent-a", "agent-b")
	if sim == nil {
		t.Fatal("expected non-nil CapabilitySimilarity")
	}
	if sim.SkillScore != 1.0 {
		t.Errorf("SkillScore = %f, want 1.0", sim.SkillScore)
	}
}

// --- 22.4-UNIT-004: [P0] SimilarityMatrix Compute with cooperation history (AC2) ---

func TestSimilarityMatrix_Compute_WithCoopHistory(t *testing.T) {
	// Given: two agents with cooperation history
	agents := map[string][]string{
		"agent-a": {"skill-1", "skill-2"},
		"agent-b": {"skill-2", "skill-3"},
	}
	coopHistory := map[string]map[string]int{
		"agent-a": {"agent-b": 10},
		"agent-b": {"agent-a": 10},
	}

	// When: computing the similarity matrix
	matrix := NewSimilarityMatrix()
	matrix.Compute(agents, coopHistory)

	// Then: cooperation history is factored into the score
	sim := matrix.Get("agent-a", "agent-b")
	if sim == nil {
		t.Fatal("expected non-nil CapabilitySimilarity")
	}

	// SkillScore = Jaccard({skill-1,skill-2}, {skill-2,skill-3}) = 1/3
	expectedSkill := 1.0 / 3.0
	if absDiff(sim.SkillScore, expectedSkill) > 0.01 {
		t.Errorf("SkillScore = %f, want ~%f", sim.SkillScore, expectedSkill)
	}

	// CoopScore > 0 because there's cooperation history
	if sim.CoopScore <= 0.0 {
		t.Errorf("CoopScore = %f, want > 0.0 with cooperation history", sim.CoopScore)
	}

	// Score = 0.7 * SkillScore + 0.3 * CoopScore
	expectedScore := 0.7*sim.SkillScore + 0.3*sim.CoopScore
	if absDiff(sim.Score, expectedScore) > 0.01 {
		t.Errorf("Score = %f, want ~%f (0.7*%.3f + 0.3*%.3f)", sim.Score, expectedScore, sim.SkillScore, sim.CoopScore)
	}
}

// --- 22.4-UNIT-005: [P0] SimilarityMatrix GetSimilar sorted by score (AC1) ---

func TestSimilarityMatrix_GetSimilar_SortedByScore(t *testing.T) {
	// Given: three agents with varying similarity
	agents := map[string][]string{
		"target":    {"a", "b", "c", "d"},
		"high-sim":  {"a", "b", "c"},    // Jaccard = 3/4 = 0.75
		"low-sim":   {"a"},              // Jaccard = 1/4 = 0.25
		"mid-sim":   {"a", "b"},         // Jaccard = 2/4 = 0.50
	}
	coopHistory := map[string]map[string]int{}

	matrix := NewSimilarityMatrix()
	matrix.Compute(agents, coopHistory)

	// When: getting similar agents for "target"
	similar := matrix.GetSimilar("target", 0.0)

	// Then: results are sorted by Score descending
	if len(similar) < 3 {
		t.Fatalf("expected at least 3 similar agents, got %d", len(similar))
	}
	for i := 1; i < len(similar); i++ {
		if similar[i].Score > similar[i-1].Score {
			t.Errorf("results not sorted descending: index %d (Score=%.3f) > index %d (Score=%.3f)",
				i, similar[i].Score, i-1, similar[i-1].Score)
		}
	}
}

// --- 22.4-UNIT-006: [P0] SimilarityMatrix GetSimilar minScore filter (AC1) ---

func TestSimilarityMatrix_GetSimilar_MinScoreFilter(t *testing.T) {
	// Given: agents with varying similarity
	agents := map[string][]string{
		"target":   {"a", "b", "c", "d"},
		"high":     {"a", "b", "c"},     // Jaccard = 3/4 = 0.75 -> Score = 0.7*0.75 = 0.525
		"low":      {"z"},               // Jaccard = 0/5 = 0.0 -> Score = 0.0
	}
	coopHistory := map[string]map[string]int{}

	matrix := NewSimilarityMatrix()
	matrix.Compute(agents, coopHistory)

	// When: filtering with minScore = 0.3
	similar := matrix.GetSimilar("target", 0.3)

	// Then: only "high" should be returned (Score > 0.3)
	for _, s := range similar {
		if s.Score < 0.3 {
			t.Errorf("got agent with Score=%f below minScore=0.3", s.Score)
		}
	}
}

// --- 22.4-UNIT-007: [P1] SimilarityMatrix Compute empty input (AC1) ---

func TestSimilarityMatrix_Compute_EmptyInput(t *testing.T) {
	// Given: empty agents map
	agents := map[string][]string{}
	coopHistory := map[string]map[string]int{}

	// When: computing the similarity matrix
	matrix := NewSimilarityMatrix()
	matrix.Compute(agents, coopHistory)

	// Then: GetSimilar returns empty results
	similar := matrix.GetSimilar("nonexistent", 0.0)
	if len(similar) != 0 {
		t.Errorf("expected 0 similar agents for empty matrix, got %d", len(similar))
	}
}

// --- 22.4-UNIT-008: [P0] SimilarityMatrix Get is symmetric (AC1) ---

func TestSimilarityMatrix_Get_Symmetric(t *testing.T) {
	// Given: two agents
	agents := map[string][]string{
		"agent-a": {"x", "y"},
		"agent-b": {"y", "z"},
	}
	coopHistory := map[string]map[string]int{}

	matrix := NewSimilarityMatrix()
	matrix.Compute(agents, coopHistory)

	// When: querying in both directions
	simAB := matrix.Get("agent-a", "agent-b")
	simBA := matrix.Get("agent-b", "agent-a")

	// Then: results are symmetric
	if simAB == nil || simBA == nil {
		t.Fatal("expected non-nil similarity in both directions")
	}
	if simAB.Score != simBA.Score {
		t.Errorf("asymmetric scores: Get(a,b).Score=%f != Get(b,a).Score=%f", simAB.Score, simBA.Score)
	}
}

// --- 22.4-UNIT-009: [P1] SimilarityMatrix self-similarity not stored (AC1) ---

func TestSimilarityMatrix_Compute_NoSelfSimilarity(t *testing.T) {
	// Given: agents
	agents := map[string][]string{
		"agent-a": {"x", "y"},
		"agent-b": {"y", "z"},
	}
	coopHistory := map[string]map[string]int{}

	matrix := NewSimilarityMatrix()
	matrix.Compute(agents, coopHistory)

	// When: querying self-similarity
	sim := matrix.Get("agent-a", "agent-a")

	// Then: self-similarity is nil (not stored)
	if sim != nil {
		t.Error("self-similarity should not be stored")
	}
}

// --- 22.4-UNIT-010: [P0] ImmuneDaemon UpdateSimilarityMatrix (AC1) ---

func TestImmuneDaemon_UpdateSimilarityMatrix(t *testing.T) {
	// Given: a running ImmuneDaemon
	dir := t.TempDir()
	store := NewImmuneStore(dir)
	daemon := NewImmuneDaemon(store)
	if err := daemon.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer daemon.Stop()

	// When: updating the similarity matrix
	agents := map[string][]string{
		"analyzer": {"code-analysis", "testing"},
		"reviewer": {"code-analysis", "documentation"},
	}
	daemon.UpdateSimilarityMatrix(agents)

	// Then: similarity can be queried
	sim := daemon.GetSimilarity("analyzer", "reviewer")
	if sim == nil {
		t.Fatal("expected non-nil similarity after UpdateSimilarityMatrix")
	}
	if sim.SkillScore <= 0 {
		t.Errorf("SkillScore = %f, want > 0", sim.SkillScore)
	}
}

// --- 22.4-UNIT-011: [P0] ImmuneDaemon RecordCooperation (AC2) ---

func TestImmuneDaemon_RecordCooperation(t *testing.T) {
	// Given: a running ImmuneDaemon
	dir := t.TempDir()
	store := NewImmuneStore(dir)
	daemon := NewImmuneDaemon(store)
	if err := daemon.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer daemon.Stop()

	// When: recording cooperation events
	daemon.RecordCooperation("agent-a", "agent-b")
	daemon.RecordCooperation("agent-a", "agent-b")
	daemon.RecordCooperation("agent-a", "agent-b")

	// And: updating the matrix with these agents
	agents := map[string][]string{
		"agent-a": {"skill-1"},
		"agent-b": {"skill-2"},
	}
	daemon.UpdateSimilarityMatrix(agents)

	// Then: similarity is affected by cooperation history
	sim := daemon.GetSimilarity("agent-a", "agent-b")
	if sim == nil {
		t.Fatal("expected non-nil similarity")
	}
	// No skill overlap, but cooperation history should give CoopScore > 0
	if sim.CoopScore <= 0.0 {
		t.Errorf("CoopScore = %f, want > 0 after cooperation events", sim.CoopScore)
	}
}

// --- 22.4-UNIT-012: [P0] ImmuneDaemon GetSimilarAgents nil daemon (AC4) ---

func TestImmuneDaemon_GetSimilarAgents_NilDaemon(t *testing.T) {
	// Given: a nil ImmuneDaemon
	var daemon *ImmuneDaemon

	// When: querying similar agents
	result := daemon.GetSimilarAgents("any-agent", 0.0)

	// Then: returns nil without panic
	if result != nil {
		t.Errorf("expected nil from nil daemon, got %v", result)
	}
}

// --- 22.4-UNIT-013: [P0] ImmuneDaemon GetSimilarity nil daemon ---

func TestImmuneDaemon_GetSimilarity_NilDaemon(t *testing.T) {
	// Given: a nil ImmuneDaemon
	var daemon *ImmuneDaemon

	// When: querying similarity
	result := daemon.GetSimilarity("a", "b")

	// Then: returns nil without panic
	if result != nil {
		t.Errorf("expected nil from nil daemon, got %v", result)
	}
}

// --- 22.4-UNIT-014: [P0] ImmuneDaemon AttemptMigration success (AC3, AC4) ---

func TestImmuneDaemon_AttemptMigration_Success(t *testing.T) {
	// Given: a running ImmuneDaemon with similarity matrix and migrate function
	dir := t.TempDir()
	store := NewImmuneStore(dir)
	daemon := NewImmuneDaemon(store)
	if err := daemon.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer daemon.Stop()

	// Setup similarity matrix
	agents := map[string][]string{
		"failing-agent": {"code-analysis", "testing", "debugging"},
		"backup-agent":  {"code-analysis", "testing"},
		"unrelated":     {"cooking"},
	}
	daemon.UpdateSimilarityMatrix(agents)

	// Setup reputation store with backup-agent having good reputation
	repDir := t.TempDir()
	repStore := NewReputationStore(repDir)
	_ = repStore.RecordResult("backup-agent", &SLAResult{AgentName: "backup-agent", Passed: true, DurationMs: 100, TokensUsed: 50})
	daemon.SetReputationStore(repStore)

	// Setup migrate function that succeeds
	daemon.SetMigrateFunc(func(intent string, agentName string, contextMessages []string) (types.PID, error) {
		return types.PID(999), nil
	})

	// When: attempting migration
	result := daemon.AttemptMigration(types.PID(42), "failing-agent", "analyze code", []string{"msg1", "msg2"})

	// Then: migration succeeds
	if result == nil {
		t.Fatal("expected non-nil MigrationResult")
	}
	if !result.Success {
		t.Errorf("migration failed: %s", result.Reason)
	}
	if result.TargetAgent != "backup-agent" {
		t.Errorf("TargetAgent = %q, want %q", result.TargetAgent, "backup-agent")
	}
	if result.NewPID != types.PID(999) {
		t.Errorf("NewPID = %d, want 999", result.NewPID)
	}
	if result.OriginalPID != types.PID(42) {
		t.Errorf("OriginalPID = %d, want 42", result.OriginalPID)
	}
}

// --- 22.4-UNIT-015: [P0] ImmuneDaemon AttemptMigration no candidate (AC4) ---

func TestImmuneDaemon_AttemptMigration_NoCandidate(t *testing.T) {
	// Given: a running ImmuneDaemon with no similar agents above threshold
	dir := t.TempDir()
	store := NewImmuneStore(dir)
	daemon := NewImmuneDaemon(store)
	if err := daemon.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer daemon.Stop()

	// Agents with no overlap
	agents := map[string][]string{
		"failing-agent": {"unique-skill-a"},
		"other-agent":   {"unique-skill-b"},
	}
	daemon.UpdateSimilarityMatrix(agents)

	daemon.SetMigrateFunc(func(intent string, agentName string, contextMessages []string) (types.PID, error) {
		return types.PID(0), nil
	})

	// When: attempting migration
	result := daemon.AttemptMigration(types.PID(42), "failing-agent", "do something", nil)

	// Then: migration fails because no candidate meets threshold
	if result == nil {
		t.Fatal("expected non-nil MigrationResult")
	}
	if result.Success {
		t.Error("migration should fail when no candidate meets similarity threshold")
	}
}

// --- 22.4-UNIT-016: [P0] ImmuneDaemon AttemptMigration below threshold (AC4) ---

func TestImmuneDaemon_AttemptMigration_BelowThreshold(t *testing.T) {
	// Given: a running ImmuneDaemon where all agents have similarity < MinMigrationSimilarity (0.3)
	dir := t.TempDir()
	store := NewImmuneStore(dir)
	daemon := NewImmuneDaemon(store)
	if err := daemon.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer daemon.Stop()

	// Very low overlap: Jaccard = 0/6 = 0.0 -> Score = 0.0
	agents := map[string][]string{
		"failing-agent": {"a", "b", "c"},
		"other-agent":   {"d", "e", "f"},
	}
	daemon.UpdateSimilarityMatrix(agents)

	daemon.SetMigrateFunc(func(intent string, agentName string, contextMessages []string) (types.PID, error) {
		return types.PID(100), nil
	})

	// When: attempting migration
	result := daemon.AttemptMigration(types.PID(42), "failing-agent", "do something", nil)

	// Then: migration fails (all candidates below 0.3 threshold)
	if result == nil {
		t.Fatal("expected non-nil MigrationResult")
	}
	if result.Success {
		t.Error("migration should fail when all candidates below MinMigrationSimilarity threshold")
	}
}

// --- 22.4-UNIT-017: [P1] ImmuneDaemon AttemptMigration reputation weighted (AC4) ---

func TestImmuneDaemon_AttemptMigration_ReputationWeighted(t *testing.T) {
	// Given: two equally similar agents but different reputation scores
	dir := t.TempDir()
	store := NewImmuneStore(dir)
	daemon := NewImmuneDaemon(store)
	if err := daemon.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer daemon.Stop()

	agents := map[string][]string{
		"failing-agent":  {"a", "b", "c"},
		"good-rep-agent": {"a", "b", "c"}, // identical skills = same similarity
		"bad-rep-agent":  {"a", "b", "c"}, // identical skills = same similarity
	}
	daemon.UpdateSimilarityMatrix(agents)

	// Good rep agent has better reputation
	repDir := t.TempDir()
	repStore := NewReputationStore(repDir)
	for range 10 {
		_ = repStore.RecordResult("good-rep-agent", &SLAResult{AgentName: "good-rep-agent", Passed: true, DurationMs: 100, TokensUsed: 50})
	}
	for range 10 {
		_ = repStore.RecordResult("bad-rep-agent", &SLAResult{AgentName: "bad-rep-agent", Passed: false, DurationMs: 100, TokensUsed: 50})
	}
	daemon.SetReputationStore(repStore)

	var migratedTo string
	daemon.SetMigrateFunc(func(intent string, agentName string, contextMessages []string) (types.PID, error) {
		migratedTo = agentName
		return types.PID(200), nil
	})

	// When: attempting migration
	result := daemon.AttemptMigration(types.PID(42), "failing-agent", "analyze", nil)

	// Then: migration selects the agent with better reputation
	if result == nil {
		t.Fatal("expected non-nil MigrationResult")
	}
	if !result.Success {
		t.Fatalf("migration should succeed, got reason: %s", result.Reason)
	}
	if migratedTo != "good-rep-agent" {
		t.Errorf("should select good-rep-agent (better reputation), but got %q", migratedTo)
	}
}

// --- 22.4-UNIT-018: [P0] ImmuneDaemon AttemptMigration nil daemon (AC3) ---

func TestImmuneDaemon_AttemptMigration_NilDaemon(t *testing.T) {
	// Given: a nil ImmuneDaemon
	var daemon *ImmuneDaemon

	// When: attempting migration
	result := daemon.AttemptMigration(types.PID(42), "agent", "intent", nil)

	// Then: returns nil without panic
	if result != nil {
		t.Errorf("expected nil from nil daemon, got %v", result)
	}
}

// --- 22.4-UNIT-019: [P1] SimilarityMatrix concurrent access (AC1) ---

func TestSimilarityMatrix_ConcurrentAccess(t *testing.T) {
	// Given: a computed similarity matrix
	agents := map[string][]string{
		"a": {"x", "y"},
		"b": {"y", "z"},
		"c": {"x", "z"},
	}
	matrix := NewSimilarityMatrix()
	matrix.Compute(agents, map[string]map[string]int{})

	// When: multiple goroutines access concurrently
	const goroutines = 10
	var wg sync.WaitGroup

	for range goroutines {
		wg.Go(func() {
			_ = matrix.Get("a", "b")
			_ = matrix.GetSimilar("a", 0.0)
		})
	}
	wg.Wait()

	// Then: no data race (test runs with -race detector)
}

// --- 22.4-UNIT-020: [P0] MinMigrationSimilarity constant exists (AC4) ---

func TestMinMigrationSimilarity_Value(t *testing.T) {
	// Given/When: the constant is defined
	// Then: it equals 0.3
	if MinMigrationSimilarity != 0.3 {
		t.Errorf("MinMigrationSimilarity = %f, want 0.3", MinMigrationSimilarity)
	}
}

// --- 22.4-UNIT-021: [P0] CapabilitySimilarity struct has required fields ---

func TestCapabilitySimilarity_Fields(t *testing.T) {
	// Given: a CapabilitySimilarity instance
	cs := CapabilitySimilarity{
		AgentA:     "agent-a",
		AgentB:     "agent-b",
		SkillScore: 0.5,
		CoopScore:  0.3,
		Score:      0.44,
	}

	// Then: all fields are accessible and correct
	if cs.AgentA != "agent-a" {
		t.Errorf("AgentA = %q, want %q", cs.AgentA, "agent-a")
	}
	if cs.AgentB != "agent-b" {
		t.Errorf("AgentB = %q, want %q", cs.AgentB, "agent-b")
	}
	if cs.SkillScore != 0.5 {
		t.Errorf("SkillScore = %f, want 0.5", cs.SkillScore)
	}
	if cs.CoopScore != 0.3 {
		t.Errorf("CoopScore = %f, want 0.3", cs.CoopScore)
	}
	if cs.Score != 0.44 {
		t.Errorf("Score = %f, want 0.44", cs.Score)
	}
}

// --- 22.4-UNIT-022: [P0] MigrationResult struct has required fields ---

func TestMigrationResult_Fields(t *testing.T) {
	// Given: a MigrationResult instance
	mr := MigrationResult{
		OriginalPID:   types.PID(42),
		OriginalAgent: "failing-agent",
		TargetAgent:   "backup-agent",
		NewPID:        types.PID(999),
		Similarity:    0.85,
		DurationMs:    1500,
		Success:       true,
		Reason:        "",
	}

	// Then: all fields are accessible and correct
	if mr.OriginalPID != types.PID(42) {
		t.Errorf("OriginalPID = %d, want 42", mr.OriginalPID)
	}
	if mr.TargetAgent != "backup-agent" {
		t.Errorf("TargetAgent = %q, want %q", mr.TargetAgent, "backup-agent")
	}
	if mr.NewPID != types.PID(999) {
		t.Errorf("NewPID = %d, want 999", mr.NewPID)
	}
	if !mr.Success {
		t.Error("Success should be true")
	}
}

// --- 22.4-UNIT-023: [P1] ImmuneDaemon RecordCooperation bidirectional (AC2) ---

func TestImmuneDaemon_RecordCooperation_Bidirectional(t *testing.T) {
	// Given: a running ImmuneDaemon
	dir := t.TempDir()
	store := NewImmuneStore(dir)
	daemon := NewImmuneDaemon(store)
	if err := daemon.Start(); err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer daemon.Stop()

	// When: recording cooperation A -> B
	daemon.RecordCooperation("agent-a", "agent-b")

	// And: updating matrix with agents that have identical skills
	agents := map[string][]string{
		"agent-a": {"skill-1"},
		"agent-b": {"skill-1"},
	}
	daemon.UpdateSimilarityMatrix(agents)

	// Then: both directions should have coop score
	simAB := daemon.GetSimilarity("agent-a", "agent-b")
	simBA := daemon.GetSimilarity("agent-b", "agent-a")

	if simAB == nil || simBA == nil {
		t.Fatal("expected non-nil similarity in both directions")
	}
	if simAB.CoopScore <= 0 {
		t.Errorf("CoopScore(A->B) = %f, want > 0", simAB.CoopScore)
	}
	if simBA.CoopScore <= 0 {
		t.Errorf("CoopScore(B->A) = %f, want > 0", simBA.CoopScore)
	}
}

// --- 22.4-UNIT-024: [P1] ImmuneDaemon UpdateSimilarityMatrix nil daemon ---

func TestImmuneDaemon_UpdateSimilarityMatrix_NilDaemon(t *testing.T) {
	// Given: a nil ImmuneDaemon
	var daemon *ImmuneDaemon

	// When/Then: no panic
	daemon.UpdateSimilarityMatrix(map[string][]string{"a": {"b"}})
}

// --- 22.4-UNIT-025: [P1] SimilarityMatrix Compute single agent (AC1) ---

func TestSimilarityMatrix_Compute_SingleAgent(t *testing.T) {
	// Given: only one agent
	agents := map[string][]string{
		"solo-agent": {"skill-a", "skill-b"},
	}
	coopHistory := map[string]map[string]int{}

	// When: computing the similarity matrix
	matrix := NewSimilarityMatrix()
	matrix.Compute(agents, coopHistory)

	// Then: no entries in the matrix (nothing to compare)
	similar := matrix.GetSimilar("solo-agent", 0.0)
	if len(similar) != 0 {
		t.Errorf("expected 0 similar agents for single-agent matrix, got %d", len(similar))
	}
}

// helper
func absDiff(a, b float64) float64 {
	d := a - b
	if d < 0 {
		return -d
	}
	return d
}
