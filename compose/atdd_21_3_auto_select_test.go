package compose

import (
	"context"
	"testing"

	"github.com/rnixai/rnix/agents"
	"github.com/rnixai/rnix/kernel"
)

// ============================================================
// ATDD RED PHASE — Story 21.3: 声誉系统与自动选择
//
// Compose 引擎自动选择集成测试 & Candidates 字段解析测试。
// 测试引用 AgentSpec.Candidates 字段和 Engine 中的自动选择逻辑，
// 这些类型/字段尚不存在。测试将无法编译直到实现完成。
//
// RED → GREEN: 添加 Candidates 字段到 compose/types.go 和
//              engine.go 中的自动选择逻辑，测试编译并通过。
// ============================================================

// --- 21.3-INT-001: [P0] ParseBytes parses candidates in compose.yaml (AC4) ---

func TestParseBytes_WithCandidates(t *testing.T) {
	data := []byte(`
version: "1.0"
intent: "auto-select workflow"
token_budget: 50000
agents:
  reviewer:
    intent: "review code"
    priority: high
    candidates:
      - code-reviewer-v2
      - code-reviewer-v1
      - code-reviewer-legacy
    sla:
      max_tokens: 15000
      max_duration_ms: 60000
`)

	spec, err := ParseBytes(data)
	if err != nil {
		t.Fatalf("ParseBytes failed: %v", err)
	}

	reviewer := spec.Agents["reviewer"]
	if reviewer.Candidates == nil {
		t.Fatal("expected reviewer to have Candidates")
	}
	if len(reviewer.Candidates) != 3 {
		t.Fatalf("expected 3 candidates, got %d", len(reviewer.Candidates))
	}
	if reviewer.Candidates[0] != "code-reviewer-v2" {
		t.Fatalf("expected first candidate 'code-reviewer-v2', got %q", reviewer.Candidates[0])
	}
	if reviewer.Candidates[1] != "code-reviewer-v1" {
		t.Fatalf("expected second candidate 'code-reviewer-v1', got %q", reviewer.Candidates[1])
	}
	if reviewer.Candidates[2] != "code-reviewer-legacy" {
		t.Fatalf("expected third candidate 'code-reviewer-legacy', got %q", reviewer.Candidates[2])
	}
}

// --- 21.3-INT-002: [P0] ParseBytes without candidates - backward compat (AC5) ---

func TestParseBytes_WithoutCandidates(t *testing.T) {
	data := []byte(`
version: "1.0"
intent: "no candidates workflow"
agents:
  worker:
    intent: "do work"
    agent: specific-agent
`)

	spec, err := ParseBytes(data)
	if err != nil {
		t.Fatalf("ParseBytes failed: %v", err)
	}

	worker := spec.Agents["worker"]
	if len(worker.Candidates) > 0 {
		t.Fatalf("expected nil/empty Candidates when not defined, got %v", worker.Candidates)
	}
}

// --- 21.3-INT-003: [P0] Engine Execute with Candidates selects best agent (AC4) ---

func TestEngine_Execute_WithCandidates_SelectBest(t *testing.T) {
	dir := t.TempDir()
	repStore := kernel.NewReputationStore(dir)

	// Pre-populate reputation: "good-agent" has all passed, "bad-agent" has all failed
	for range 5 {
		if err := repStore.RecordResult("good-agent", &kernel.SLAResult{
			AgentName: "good-agent", Passed: true, TokensUsed: 3000, DurationMs: 20000,
		}); err != nil {
			t.Fatalf("RecordResult failed: %v", err)
		}
		if err := repStore.RecordResult("bad-agent", &kernel.SLAResult{
			AgentName: "bad-agent", Passed: false, TokensUsed: 15000, DurationMs: 80000,
		}); err != nil {
			t.Fatalf("RecordResult failed: %v", err)
		}
	}

	spec := &ComposeSpec{
		Version:     "1.0",
		Intent:      "auto-select test",
		TokenBudget: 50000,
		Agents: map[string]*AgentSpec{
			"selector": {
				Intent:     "review code",
				Priority:   "high",
				Candidates: []string{"bad-agent", "good-agent"},
			},
		},
	}

	mock := newMockKernelSpawner()
	loader := func(name string) (*agents.AgentInfo, error) {
		return &agents.AgentInfo{
			Manifest: agents.AgentManifest{Name: name},
		}, nil
	}

	engine, err := NewEngineWithReputation(spec, mock, loader, repStore)
	if err != nil {
		t.Fatalf("NewEngineWithReputation failed: %v", err)
	}

	results, err := engine.Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	// The engine should have loaded "good-agent" because it has higher score
	// Verify by checking the spawn records -- the agent loader should have been called
	// with "good-agent" (the best candidate)
	mock.mu.Lock()
	defer mock.mu.Unlock()
	if len(mock.spawned) != 1 {
		t.Fatalf("expected 1 spawn call, got %d", len(mock.spawned))
	}
	spawnedAgent := mock.spawned[0].agent
	if spawnedAgent == nil {
		t.Fatal("expected non-nil agent info from spawn")
	}
	if spawnedAgent.Manifest.Name != "good-agent" {
		t.Fatalf("expected engine to select 'good-agent', got %q", spawnedAgent.Manifest.Name)
	}
}

// --- 21.3-INT-004: [P0] Engine Execute with Candidates no ReputationStore fallback (AC4/AC5) ---

func TestEngine_Execute_WithCandidates_NoReputationStore(t *testing.T) {
	spec := &ComposeSpec{
		Version:     "1.0",
		Intent:      "fallback test",
		TokenBudget: 50000,
		Agents: map[string]*AgentSpec{
			"selector": {
				Intent:     "review code",
				Priority:   "normal",
				Candidates: []string{"first-agent", "second-agent"},
			},
		},
	}

	mock := newMockKernelSpawner()
	loader := func(name string) (*agents.AgentInfo, error) {
		return &agents.AgentInfo{
			Manifest: agents.AgentManifest{Name: name},
		}, nil
	}

	// Engine without ReputationStore
	engine, err := NewEngine(spec, mock, loader)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	results, err := engine.Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	// Without ReputationStore, should fallback to first candidate
	mock.mu.Lock()
	defer mock.mu.Unlock()
	if len(mock.spawned) != 1 {
		t.Fatalf("expected 1 spawn call, got %d", len(mock.spawned))
	}
	spawnedAgent := mock.spawned[0].agent
	if spawnedAgent == nil {
		t.Fatal("expected non-nil agent info from spawn")
	}
	if spawnedAgent.Manifest.Name != "first-agent" {
		t.Fatalf("expected fallback to first candidate 'first-agent', got %q", spawnedAgent.Manifest.Name)
	}
}

// --- 21.3-INT-005: [P0] Engine Execute without Candidates uses Agent field (AC5) ---

func TestEngine_Execute_WithoutCandidates_UsesAgentField(t *testing.T) {
	spec := &ComposeSpec{
		Version: "1.0",
		Intent:  "no candidates backward compat test",
		Agents: map[string]*AgentSpec{
			"worker": {
				Intent: "simple task",
				Agent:  "specific-agent",
			},
		},
	}

	mock := newMockKernelSpawner()
	loader := func(name string) (*agents.AgentInfo, error) {
		return &agents.AgentInfo{
			Manifest: agents.AgentManifest{Name: name},
		}, nil
	}

	engine, err := NewEngine(spec, mock, loader)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	results, err := engine.Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	// Should use the explicit Agent field, not candidates
	mock.mu.Lock()
	defer mock.mu.Unlock()
	if len(mock.spawned) != 1 {
		t.Fatalf("expected 1 spawn call, got %d", len(mock.spawned))
	}
	spawnedAgent := mock.spawned[0].agent
	if spawnedAgent == nil {
		t.Fatal("expected non-nil agent info from spawn")
	}
	if spawnedAgent.Manifest.Name != "specific-agent" {
		t.Fatalf("expected 'specific-agent' from Agent field, got %q", spawnedAgent.Manifest.Name)
	}
}

// --- 21.3-INT-006: [P1] ParseBytes with both candidates and SLA (AC4) ---

func TestParseBytes_WithCandidatesAndSLA(t *testing.T) {
	data := []byte(`
version: "1.0"
intent: "candidates with SLA"
agents:
  reviewer:
    intent: "review"
    candidates:
      - reviewer-v2
      - reviewer-v1
    sla:
      max_tokens: 15000
      max_duration_ms: 60000
      output_format: "json"
`)

	spec, err := ParseBytes(data)
	if err != nil {
		t.Fatalf("ParseBytes failed: %v", err)
	}

	reviewer := spec.Agents["reviewer"]
	if len(reviewer.Candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(reviewer.Candidates))
	}
	if reviewer.SLA == nil {
		t.Fatal("expected SLA to be defined alongside Candidates")
	}
	if reviewer.SLA.MaxTokens != 15000 {
		t.Fatalf("expected SLA max_tokens 15000, got %d", reviewer.SLA.MaxTokens)
	}
}

// --- 21.3-INT-007: [P1] Engine Execute with Candidates and SLA evaluates selected agent (AC4) ---

func TestEngine_Execute_WithCandidates_SLAEvaluated(t *testing.T) {
	dir := t.TempDir()
	repStore := kernel.NewReputationStore(dir)

	// Pre-populate: "fast-v2" has better reputation
	for range 5 {
		if err := repStore.RecordResult("fast-v2", &kernel.SLAResult{
			AgentName: "fast-v2", Passed: true, TokensUsed: 3000, DurationMs: 20000,
		}); err != nil {
			t.Fatalf("RecordResult failed: %v", err)
		}
	}

	spec := &ComposeSpec{
		Version:     "1.0",
		Intent:      "candidates + SLA test",
		TokenBudget: 50000,
		Agents: map[string]*AgentSpec{
			"reviewer": {
				Intent:     "review code",
				Priority:   "high",
				Candidates: []string{"slow-v1", "fast-v2"},
				SLA: &kernel.SLASpec{
					MaxTokens: 20000,
				},
			},
		},
	}

	mock := newMockKernelSpawner()
	mock.tokenUsed[1] = 5000 // within SLA
	loader := func(name string) (*agents.AgentInfo, error) {
		return &agents.AgentInfo{
			Manifest: agents.AgentManifest{Name: name},
		}, nil
	}

	engine, err := NewEngineWithReputation(spec, mock, loader, repStore)
	if err != nil {
		t.Fatalf("NewEngineWithReputation failed: %v", err)
	}

	results, err := engine.Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}

	// SLA should still be evaluated on the selected agent
	r := results[0]
	if r.SLAResult == nil {
		t.Fatal("expected SLAResult when SLA is defined with candidates")
	}
	if !r.SLAResult.Passed {
		t.Fatal("expected SLAResult to pass")
	}
}
