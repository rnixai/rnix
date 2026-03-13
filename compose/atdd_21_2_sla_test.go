package compose

import (
	"context"
	"testing"

	"github.com/rnixai/rnix/agents"
	"github.com/rnixai/rnix/kernel"
)

// ============================================================
// ATDD RED PHASE — Story 21.2: 合约 SLA 与评估
//
// Compose 引擎 SLA 集成测试 & 配置解析测试。
// 测试引用 AgentSpec.SLA, ScheduleResult.SLAResult 等类型和方法，
// 这些类型/字段尚不存在。测试将无法编译直到实现完成。
//
// RED → GREEN: 添加 SLA 字段到 compose/types.go 和 engine.go，测试编译并通过。
// ============================================================

// --- 21.2-INT-001: [P0] ParseBytes parses SLA in compose.yaml (AC1) ---

func TestParseBytes_WithSLA(t *testing.T) {
	data := []byte(`
version: "1.0"
intent: "SLA test workflow"
token_budget: 50000
agents:
  reviewer:
    intent: "review code"
    priority: high
    sla:
      max_tokens: 15000
      max_duration_ms: 60000
      output_format: "json"
  summarizer:
    intent: "summarize review"
    priority: normal
    sla:
      max_tokens: 8000
      max_duration_ms: 30000
      output_format: "markdown"
    depends_on:
      reviewer: completed
`)

	spec, err := ParseBytes(data)
	if err != nil {
		t.Fatalf("ParseBytes failed: %v", err)
	}

	// Verify reviewer SLA
	reviewer := spec.Agents["reviewer"]
	if reviewer.SLA == nil {
		t.Fatal("expected reviewer to have SLA")
	}
	if reviewer.SLA.MaxTokens != 15000 {
		t.Fatalf("expected reviewer SLA max_tokens 15000, got %d", reviewer.SLA.MaxTokens)
	}
	if reviewer.SLA.MaxDurationMs != 60000 {
		t.Fatalf("expected reviewer SLA max_duration_ms 60000, got %d", reviewer.SLA.MaxDurationMs)
	}
	if reviewer.SLA.OutputFormat != "json" {
		t.Fatalf("expected reviewer SLA output_format 'json', got %q", reviewer.SLA.OutputFormat)
	}

	// Verify summarizer SLA
	summarizer := spec.Agents["summarizer"]
	if summarizer.SLA == nil {
		t.Fatal("expected summarizer to have SLA")
	}
	if summarizer.SLA.MaxTokens != 8000 {
		t.Fatalf("expected summarizer SLA max_tokens 8000, got %d", summarizer.SLA.MaxTokens)
	}
	if summarizer.SLA.OutputFormat != "markdown" {
		t.Fatalf("expected summarizer SLA output_format 'markdown', got %q", summarizer.SLA.OutputFormat)
	}
}

// --- 21.2-INT-002: [P0] ParseBytes without SLA field - backward compat (AC5) ---

func TestParseBytes_WithoutSLA(t *testing.T) {
	data := []byte(`
version: "1.0"
intent: "no SLA workflow"
agents:
  worker:
    intent: "do work"
`)

	spec, err := ParseBytes(data)
	if err != nil {
		t.Fatalf("ParseBytes failed: %v", err)
	}

	worker := spec.Agents["worker"]
	if worker.SLA != nil {
		t.Fatalf("expected nil SLA when not defined, got %+v", worker.SLA)
	}
}

// --- 21.2-INT-003: [P0] Engine Execute with SLA - all checks pass (AC2/AC4) ---

func TestEngine_Execute_WithSLA_AllPassed(t *testing.T) {
	spec := &ComposeSpec{
		Version:     "1.0",
		Intent:      "SLA pass test",
		TokenBudget: 50000,
		Agents: map[string]*AgentSpec{
			"agent-a": {
				Intent:   "test task",
				Priority: "normal",
				SLA: &kernel.SLASpec{
					MaxTokens:    20000,
					MaxDurationMs: 60000,
					OutputFormat:  "json",
				},
			},
		},
	}

	mock := newMockKernelSpawner()
	mock.tokenUsed[1] = 5000                          // within SLA
	mock.getResults[1] = `{"result": "success"}`       // valid JSON

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

	r := results[0]
	if r.SLAResult == nil {
		t.Fatal("expected non-nil SLAResult when SLA is defined")
	}
	if !r.SLAResult.Passed {
		t.Fatal("expected SLAResult.Passed to be true when all constraints met")
	}
	if r.SLAResult.AgentName != "agent-a" {
		t.Fatalf("expected SLAResult.AgentName 'agent-a', got %q", r.SLAResult.AgentName)
	}
}

// --- 21.2-INT-004: [P0] Engine Execute with SLA - token exceeded (AC2/AC4) ---

func TestEngine_Execute_WithSLA_TokenExceeded(t *testing.T) {
	spec := &ComposeSpec{
		Version:     "1.0",
		Intent:      "SLA token exceeded test",
		TokenBudget: 50000,
		Agents: map[string]*AgentSpec{
			"greedy-agent": {
				Intent:   "consume many tokens",
				Priority: "normal",
				SLA: &kernel.SLASpec{
					MaxTokens: 5000,
				},
			},
		},
	}

	mock := newMockKernelSpawner()
	mock.tokenUsed[1] = 15000 // exceeds SLA max_tokens of 5000

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

	r := results[0]
	if r.SLAResult == nil {
		t.Fatal("expected non-nil SLAResult when SLA is defined")
	}
	if r.SLAResult.Passed {
		t.Fatal("expected SLAResult.Passed to be false when token exceeded")
	}
}

// --- 21.2-INT-005: [P0] Engine Execute without SLA - backward compat (AC5) ---

func TestEngine_Execute_WithoutSLA(t *testing.T) {
	spec := &ComposeSpec{
		Version: "1.0",
		Intent:  "no SLA test",
		Agents: map[string]*AgentSpec{
			"simple-agent": {
				Intent: "simple task",
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

	r := results[0]
	if r.SLAResult != nil {
		t.Fatalf("expected nil SLAResult when no SLA defined, got %+v", r.SLAResult)
	}
}

// --- 21.2-INT-006: [P1] Engine Execute with SLA records to ReputationStore (AC3/AC4) ---

func TestEngine_Execute_WithSLA_RecordReputation(t *testing.T) {
	dir := t.TempDir()
	repStore := kernel.NewReputationStore(dir)

	spec := &ComposeSpec{
		Version:     "1.0",
		Intent:      "reputation test",
		TokenBudget: 50000,
		Agents: map[string]*AgentSpec{
			"tracked-agent": {
				Intent:   "tracked task",
				Priority: "high",
				SLA: &kernel.SLASpec{
					MaxTokens: 20000,
				},
			},
		},
	}

	mock := newMockKernelSpawner()
	mock.tokenUsed[1] = 8000

	loader := func(name string) (*agents.AgentInfo, error) {
		return &agents.AgentInfo{
			Manifest: agents.AgentManifest{Name: name},
		}, nil
	}

	engine, err := NewEngineWithReputation(spec, mock, loader, repStore)
	if err != nil {
		t.Fatalf("NewEngineWithReputation failed: %v", err)
	}

	_, err = engine.Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// Verify reputation was recorded
	history, err := repStore.GetHistory("tracked-agent")
	if err != nil {
		t.Fatalf("GetHistory failed: %v", err)
	}
	if len(history) != 1 {
		t.Fatalf("expected 1 reputation record, got %d", len(history))
	}
	if history[0].SLAResult.AgentName != "tracked-agent" {
		t.Fatalf("expected agent name 'tracked-agent', got %q", history[0].SLAResult.AgentName)
	}
}

// --- 21.2-INT-007: [P1] Engine Execute without ReputationStore - no panic (AC4/AC5) ---

func TestEngine_Execute_WithSLA_NilReputationStore(t *testing.T) {
	spec := &ComposeSpec{
		Version:     "1.0",
		Intent:      "nil reputation test",
		TokenBudget: 50000,
		Agents: map[string]*AgentSpec{
			"agent": {
				Intent:   "task",
				Priority: "normal",
				SLA: &kernel.SLASpec{
					MaxTokens: 10000,
				},
			},
		},
	}

	mock := newMockKernelSpawner()
	mock.tokenUsed[1] = 3000

	loader := func(name string) (*agents.AgentInfo, error) {
		return &agents.AgentInfo{
			Manifest: agents.AgentManifest{Name: name},
		}, nil
	}

	// NewEngine without reputation store -- should still work
	engine, err := NewEngine(spec, mock, loader)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	results, err := engine.Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// SLA evaluation still happens, just no persistence
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].SLAResult == nil {
		t.Fatal("expected SLAResult even without reputation store")
	}
}

// --- 21.2-INT-008: [P0] Multiple agents each get SLA evaluation (AC2/AC4) ---

func TestEngine_Execute_MultipleAgents_EachSLA(t *testing.T) {
	spec := &ComposeSpec{
		Version:     "1.0",
		Intent:      "multi-agent SLA test",
		TokenBudget: 50000,
		Agents: map[string]*AgentSpec{
			"fast-agent": {
				Intent:   "fast task",
				Priority: "high",
				SLA: &kernel.SLASpec{
					MaxTokens:    20000,
					MaxDurationMs: 60000,
				},
			},
			"slow-agent": {
				Intent:   "slow task",
				Priority: "low",
				SLA: &kernel.SLASpec{
					MaxTokens: 5000,
				},
			},
		},
	}

	mock := newMockKernelSpawner()
	// Both PIDs use 8000 tokens. fast-agent SLA is 20000 (passes), slow-agent SLA is 5000 (fails).
	// This avoids depending on map iteration order for PID assignment.
	mock.tokenUsed[1] = 8000
	mock.tokenUsed[2] = 8000

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

	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// Find each result
	slaResults := make(map[string]*kernel.SLAResult)
	for _, r := range results {
		if r.SLAResult != nil {
			slaResults[r.Name] = r.SLAResult
		}
	}

	if len(slaResults) != 2 {
		t.Fatalf("expected 2 SLA results, got %d", len(slaResults))
	}

	// fast-agent should pass SLA
	if fast, ok := slaResults["fast-agent"]; ok {
		if !fast.Passed {
			t.Fatal("expected fast-agent SLA to pass")
		}
	} else {
		t.Fatal("missing SLA result for fast-agent")
	}

	// slow-agent should fail SLA (8000 > 5000)
	if slow, ok := slaResults["slow-agent"]; ok {
		if slow.Passed {
			t.Fatal("expected slow-agent SLA to fail (token exceeded)")
		}
	} else {
		t.Fatal("missing SLA result for slow-agent")
	}
}

// --- 21.2-INT-009: [P1] SLA evaluation uses ScheduleResult duration (AC2) ---

func TestEngine_Execute_SLAUsesActualDuration(t *testing.T) {
	spec := &ComposeSpec{
		Version:     "1.0",
		Intent:      "duration SLA test",
		TokenBudget: 50000,
		Agents: map[string]*AgentSpec{
			"timed-agent": {
				Intent:   "timed task",
				Priority: "normal",
				SLA: &kernel.SLASpec{
					MaxDurationMs: 5000, // 5 seconds max
				},
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

	r := results[0]
	if r.SLAResult == nil {
		t.Fatal("expected non-nil SLAResult")
	}
	// Duration should be captured in SLAResult
	if r.SLAResult.DurationMs < 0 {
		t.Fatalf("expected non-negative DurationMs, got %d", r.SLAResult.DurationMs)
	}
}

// --- 21.2-INT-010: [P1] SLA output_format check uses process output (AC2) ---

func TestEngine_Execute_SLAChecksOutputFormat(t *testing.T) {
	spec := &ComposeSpec{
		Version:     "1.0",
		Intent:      "output format SLA test",
		TokenBudget: 50000,
		Agents: map[string]*AgentSpec{
			"json-agent": {
				Intent:   "produce JSON",
				Priority: "normal",
				SLA: &kernel.SLASpec{
					OutputFormat: "json",
				},
			},
		},
	}

	mock := newMockKernelSpawner()
	mock.getResults[1] = "this is not valid json" // Will fail output_format check

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

	r := results[0]
	if r.SLAResult == nil {
		t.Fatal("expected non-nil SLAResult")
	}
	if r.SLAResult.Passed {
		t.Fatal("expected SLAResult to fail for invalid JSON output")
	}
}
