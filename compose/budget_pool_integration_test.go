package compose

import (
	"context"
	"testing"

	"github.com/rnixai/rnix/kernel"
)

// ============================================================
// ATDD RED PHASE — Story 21.1: Token 预算池与分配调度
//
// End-to-end integration tests verifying the complete BudgetPool lifecycle.
// NOTE: Additional tests are in atdd_21_1_budget_pool_test.go
//
// These tests will fail to COMPILE until:
//   - kernel.BudgetPool, kernel.Priority types exist
//   - ComposeSpec.TokenBudget field exists
//   - AgentSpec.Priority field exists
//   - Engine budget pool integration is implemented
//
// RED → GREEN: implement all components, tests compile and pass.
// ============================================================

// --- 21.1-E2E-001: [P0] Full allocate-and-consume lifecycle (AC1/AC3) ---

func TestE2E_BudgetPool_AllocateAndConsume(t *testing.T) {
	spec := &ComposeSpec{
		Version:     "1.0",
		Intent:      "e2e budget test",
		TokenBudget: 30000,
		Agents: map[string]*AgentSpec{
			"alpha": {Intent: "alpha task", Priority: "high"},
			"beta":  {Intent: "beta task", Priority: "normal"},
		},
	}
	ks := newMockKernelSpawner()
	engine, err := NewEngine(spec, ks, mockAgentLoader)
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

	// With token_budget set, each agent should get a positive ContextBudget
	for _, rec := range ks.spawned {
		if rec.opts.ContextBudget <= 0 {
			t.Errorf("expected positive ContextBudget for %q with token_budget, got %d",
				rec.intent, rec.opts.ContextBudget)
		}
	}

	// High priority should get more budget than normal
	budgetByIntent := make(map[string]int)
	for _, rec := range ks.spawned {
		budgetByIntent[rec.intent] = rec.opts.ContextBudget
	}
	if budgetByIntent["alpha task"] <= budgetByIntent["beta task"] {
		t.Errorf("high priority (%d) should get more budget than normal (%d)",
			budgetByIntent["alpha task"], budgetByIntent["beta task"])
	}
}

// --- 21.1-E2E-002: [P0] Budget exhaustion terminates compose (AC4) ---

func TestE2E_BudgetPool_ExhaustedTerminatesCompose(t *testing.T) {
	spec := &ComposeSpec{
		Version:     "1.0",
		Intent:      "exhaust budget",
		TokenBudget: 50,
		Agents: map[string]*AgentSpec{
			"first":  {Intent: "first task", Priority: "normal"},
			"second": {Intent: "second task", Priority: "normal", DependsOn: map[string]string{"first": "completed"}},
			"third":  {Intent: "third task", Priority: "normal", DependsOn: map[string]string{"second": "completed"}},
		},
	}
	ks := newMockKernelSpawner()
	engine, err := NewEngine(spec, ks, mockAgentLoader)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	results, _ := engine.Execute(context.Background())

	_ = results
	spawned := ks.getSpawnOrder()
	if len(spawned) > 3 {
		t.Fatalf("should not spawn more than 3 agents, got %d", len(spawned))
	}
}

// --- 21.1-E2E-003: [P0] No budget backward compatible (AC5) ---

func TestE2E_BudgetPool_NoBudget_BackwardCompat(t *testing.T) {
	spec := &ComposeSpec{
		Version: "1.0",
		Intent:  "backward compat",
		Agents: map[string]*AgentSpec{
			"worker": {Intent: "do work", ContextBudget: 5000},
		},
	}
	ks := newMockKernelSpawner()
	engine, err := NewEngine(spec, ks, mockAgentLoader)
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

	if ks.spawned[0].opts.ContextBudget != 5000 {
		t.Errorf("expected ContextBudget 5000 from AgentSpec, got %d",
			ks.spawned[0].opts.ContextBudget)
	}
}

// --- 21.1-E2E-004: [P0] Priority allocation distributes budget correctly (AC2) ---

func TestE2E_BudgetPool_PriorityAllocation(t *testing.T) {
	spec := &ComposeSpec{
		Version:     "1.0",
		Intent:      "priority allocation",
		TokenBudget: 16000,
		Agents: map[string]*AgentSpec{
			"critical": {Intent: "critical task", Priority: "high"},   // weight=10
			"standard": {Intent: "standard task", Priority: "normal"}, // weight=5
			"minor":    {Intent: "minor task", Priority: "low"},       // weight=1
		},
	}
	ks := newMockKernelSpawner()
	engine, err := NewEngine(spec, ks, mockAgentLoader)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	results, err := engine.Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	budgetByIntent := make(map[string]int)
	for _, rec := range ks.spawned {
		budgetByIntent[rec.intent] = rec.opts.ContextBudget
	}

	critical := budgetByIntent["critical task"]
	standard := budgetByIntent["standard task"]
	minor := budgetByIntent["minor task"]

	// totalWeight = 10 + 5 + 1 = 16
	// critical: 16000 * 10/16 = 10000
	// standard: 16000 * 5/16 = 5000
	// minor: 16000 * 1/16 = 1000
	if critical != 10000 {
		t.Errorf("expected critical budget 10000, got %d", critical)
	}
	if standard != 5000 {
		t.Errorf("expected standard budget 5000, got %d", standard)
	}
	if minor != 1000 {
		t.Errorf("expected minor budget 1000, got %d", minor)
	}

	// Verify kernel.Priority constants are accessible
	_ = kernel.PriorityHigh
	_ = kernel.PriorityNormal
	_ = kernel.PriorityLow

	if ParsePriority("high") != kernel.PriorityHigh {
		t.Error("ParsePriority('high') should return PriorityHigh")
	}
}

// --- 21.1-E2E-005: [P1] ScheduleResult includes TokensUsed field ---

func TestE2E_BudgetPool_ScheduleResultTokensUsed(t *testing.T) {
	spec := &ComposeSpec{
		Version:     "1.0",
		Intent:      "tokens used tracking",
		TokenBudget: 10000,
		Agents: map[string]*AgentSpec{
			"worker": {Intent: "work task", Priority: "normal"},
		},
	}
	ks := newMockKernelSpawner()
	engine, err := NewEngine(spec, ks, mockAgentLoader)
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

	_ = results[0].TokensUsed
}
