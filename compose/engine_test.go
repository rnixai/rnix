package compose

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gonewx/crux/agents"
	"github.com/gonewx/crux/internal/types"
)

// --- Story 7.1: Engine Scheduling Tests ---
// These tests verify AC #3 (topological scheduling), AC #4 (dependency triggering),
// and the NFR21 performance requirement.
// All tests are in RED phase: they reference Engine, KernelSpawner, etc. which do not exist yet.

// --- Mock KernelSpawner ---

type mockSpawnRecord struct {
	intent string
	agent  *agents.AgentInfo
	opts   ComposeSpawnOpts
}

type mockKernelSpawner struct {
	mu         sync.Mutex
	spawned    []mockSpawnRecord
	pidAlloc   uint64
	results    map[types.PID]mockExecResult
	getResults map[types.PID]string
	spawnErr   error // if set, all Spawn calls return this error
}

type mockExecResult struct {
	exitCode int
	reason   string
	err      error
	delay    time.Duration
}

func newMockKernelSpawner() *mockKernelSpawner {
	return &mockKernelSpawner{
		results:    make(map[types.PID]mockExecResult),
		getResults: make(map[types.PID]string),
	}
}

func (m *mockKernelSpawner) Spawn(intent string, agent *agents.AgentInfo, opts ComposeSpawnOpts) (types.PID, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.spawnErr != nil {
		return 0, m.spawnErr
	}
	m.pidAlloc++
	pid := types.PID(m.pidAlloc)
	m.spawned = append(m.spawned, mockSpawnRecord{intent: intent, agent: agent, opts: opts})
	return pid, nil
}

func (m *mockKernelSpawner) Wait(pid types.PID) (ComposeExitStatus, error) {
	m.mu.Lock()
	result, ok := m.results[pid]
	m.mu.Unlock()

	if ok && result.delay > 0 {
		time.Sleep(result.delay)
	}
	if ok && result.err != nil {
		return ComposeExitStatus{Code: result.exitCode, Reason: result.reason}, result.err
	}
	if ok {
		return ComposeExitStatus{Code: result.exitCode, Reason: result.reason}, nil
	}
	return ComposeExitStatus{Code: 0, Reason: "ok"}, nil
}

func (m *mockKernelSpawner) GetProcessResult(pid types.PID) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.getResults[pid]
	return r, ok
}

func (m *mockKernelSpawner) getSpawnOrder() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	order := make([]string, len(m.spawned))
	for i, rec := range m.spawned {
		order[i] = rec.intent
	}
	return order
}

// --- Mock AgentLoaderFunc ---

func mockAgentLoader(name string) (*agents.AgentInfo, error) {
	return &agents.AgentInfo{
		Manifest: agents.AgentManifest{
			Name:        name,
			Description: "mock agent " + name,
		},
	}, nil
}

// --- Engine Tests ---

func TestNewEngine_Valid(t *testing.T) {
	// Given: a valid ComposeSpec
	spec := &ComposeSpec{
		Version: "1.0",
		Intent:  "test",
		Agents: map[string]*AgentSpec{
			"a": {Intent: "task A"},
		},
	}
	ks := newMockKernelSpawner()

	// When: creating a new Engine
	engine, err := NewEngine(spec, ks, mockAgentLoader)

	// Then: engine is created successfully
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	if engine == nil {
		t.Fatal("expected non-nil engine")
	}
}

func TestNewEngine_CyclicSpec(t *testing.T) {
	// Given: a ComposeSpec with cyclic dependencies
	spec := &ComposeSpec{
		Version: "1.0",
		Intent:  "cyclic",
		Agents: map[string]*AgentSpec{
			"a": {Intent: "a", DependsOn: map[string]string{"b": "completed"}},
			"b": {Intent: "b", DependsOn: map[string]string{"a": "completed"}},
		},
	}
	ks := newMockKernelSpawner()

	// When: creating a new Engine
	_, err := NewEngine(spec, ks, mockAgentLoader)

	// Then: error due to cycle
	if err == nil {
		t.Fatal("expected error for cyclic spec, got nil")
	}
}

func TestEngine_Execute_NoDeps(t *testing.T) {
	// Given: 3 independent agents (all should run in parallel)
	spec := &ComposeSpec{
		Version: "1.0",
		Intent:  "parallel",
		Agents: map[string]*AgentSpec{
			"a": {Intent: "task A"},
			"b": {Intent: "task B"},
			"c": {Intent: "task C"},
		},
	}
	ks := newMockKernelSpawner()
	engine, err := NewEngine(spec, ks, mockAgentLoader)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	// When: executing the engine
	results, err := engine.Execute(context.Background())

	// Then: all 3 agents are spawned and succeed
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// All should have exit code 0
	for _, r := range results {
		if r.Err != nil {
			t.Fatalf("agent %q had error: %v", r.Name, r.Err)
		}
	}

	// All 3 should have been spawned
	order := ks.getSpawnOrder()
	if len(order) != 3 {
		t.Fatalf("expected 3 spawns, got %d", len(order))
	}
}

func TestEngine_Execute_LinearDeps(t *testing.T) {
	// Given: linear chain A -> B -> C
	spec := &ComposeSpec{
		Version: "1.0",
		Intent:  "sequential",
		Agents: map[string]*AgentSpec{
			"a": {Intent: "step 1"},
			"b": {Intent: "step 2", DependsOn: map[string]string{"a": "completed"}},
			"c": {Intent: "step 3", DependsOn: map[string]string{"b": "completed"}},
		},
	}
	ks := newMockKernelSpawner()
	engine, err := NewEngine(spec, ks, mockAgentLoader)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	// When: executing
	results, err := engine.Execute(context.Background())

	// Then: all 3 succeed in order
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// Verify execution order: A before B before C
	order := ks.getSpawnOrder()
	if len(order) != 3 {
		t.Fatalf("expected 3 spawns, got %d", len(order))
	}
	if order[0] != "step 1" {
		t.Fatalf("expected first spawn 'step 1', got %q", order[0])
	}
	if order[1] != "step 2" {
		t.Fatalf("expected second spawn 'step 2', got %q", order[1])
	}
	if order[2] != "step 3" {
		t.Fatalf("expected third spawn 'step 3', got %q", order[2])
	}
}

func TestEngine_Execute_DiamondDeps(t *testing.T) {
	// Given: diamond A -> B, A -> C, B -> D, C -> D
	spec := &ComposeSpec{
		Version: "1.0",
		Intent:  "diamond",
		Agents: map[string]*AgentSpec{
			"a": {Intent: "root"},
			"b": {Intent: "left", DependsOn: map[string]string{"a": "completed"}},
			"c": {Intent: "right", DependsOn: map[string]string{"a": "completed"}},
			"d": {Intent: "join", DependsOn: map[string]string{"b": "completed", "c": "completed"}},
		},
	}
	ks := newMockKernelSpawner()
	engine, err := NewEngine(spec, ks, mockAgentLoader)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	// When: executing
	results, err := engine.Execute(context.Background())

	// Then: all 4 succeed
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if len(results) != 4 {
		t.Fatalf("expected 4 results, got %d", len(results))
	}

	// Verify ordering: A must be first, D must be last
	order := ks.getSpawnOrder()
	if order[0] != "root" {
		t.Fatalf("expected first spawn 'root', got %q", order[0])
	}
	if order[3] != "join" {
		t.Fatalf("expected last spawn 'join', got %q", order[3])
	}
}

func TestEngine_Execute_FailurePropagation(t *testing.T) {
	// Given: linear A -> B -> C, where A fails
	spec := &ComposeSpec{
		Version: "1.0",
		Intent:  "failure chain",
		Agents: map[string]*AgentSpec{
			"a": {Intent: "will fail"},
			"b": {Intent: "depends on a", DependsOn: map[string]string{"a": "completed"}},
			"c": {Intent: "depends on b", DependsOn: map[string]string{"b": "completed"}},
		},
	}
	ks := newMockKernelSpawner()
	// PID 1 (agent A) will fail
	ks.results[types.PID(1)] = mockExecResult{exitCode: 1, reason: "crashed", err: fmt.Errorf("process failed")}

	engine, err := NewEngine(spec, ks, mockAgentLoader)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	// When: executing
	results, err := engine.Execute(context.Background())

	// Then: A fails, B and C are skipped with error
	if err != nil {
		// Execute itself may or may not return an error; the individual results should show failure
		_ = err
	}

	// Find results by name
	resultMap := make(map[string]*ScheduleResult)
	for i := range results {
		resultMap[results[i].Name] = &results[i]
	}

	// Agent A should have an error
	if ra, ok := resultMap["a"]; ok {
		if ra.Err == nil {
			t.Fatal("agent 'a' should have error")
		}
	}

	// Agent B should be skipped/failed due to upstream failure
	if rb, ok := resultMap["b"]; ok {
		if rb.Err == nil {
			t.Fatal("agent 'b' should have error (upstream dependency failed)")
		}
	}

	// Agent C should be skipped/failed due to upstream failure
	if rc, ok := resultMap["c"]; ok {
		if rc.Err == nil {
			t.Fatal("agent 'c' should have error (upstream dependency failed)")
		}
	}

	// Only A should have been spawned (B and C skipped)
	order := ks.getSpawnOrder()
	if len(order) != 1 {
		t.Fatalf("expected only 1 spawn (agent a), got %d: %v", len(order), order)
	}
}

func TestEngine_Execute_ContextCancel(t *testing.T) {
	// Given: 3 independent agents with delays
	spec := &ComposeSpec{
		Version: "1.0",
		Intent:  "cancellable",
		Agents: map[string]*AgentSpec{
			"a": {Intent: "slow task A"},
			"b": {Intent: "slow task B"},
		},
	}
	ks := newMockKernelSpawner()
	// Make all agents take a long time
	ks.results[types.PID(1)] = mockExecResult{delay: 5 * time.Second}
	ks.results[types.PID(2)] = mockExecResult{delay: 5 * time.Second}

	engine, err := NewEngine(spec, ks, mockAgentLoader)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	// When: executing with a context that cancels quickly
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, execErr := engine.Execute(ctx)

	// Then: execution should return context error
	if execErr == nil {
		t.Fatal("expected context cancellation error, got nil")
	}
}

func TestEngine_Execute_OutputPassthrough(t *testing.T) {
	// Given: A -> B, where A produces output that should be injected into B's context
	spec := &ComposeSpec{
		Version: "1.0",
		Intent:  "output passing",
		Agents: map[string]*AgentSpec{
			"a": {Intent: "produce output"},
			"b": {Intent: "consume output", DependsOn: map[string]string{"a": "completed"}},
		},
	}
	ks := newMockKernelSpawner()
	ks.getResults[types.PID(1)] = "analysis result from A"

	engine, err := NewEngine(spec, ks, mockAgentLoader)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	// When: executing
	results, err := engine.Execute(context.Background())

	// Then: B's spawn opts should contain A's output in system prompt
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}

	// Verify that the second spawn (B) had upstream output in its SystemPrompt
	order := ks.spawned
	if len(order) < 2 {
		t.Fatalf("expected at least 2 spawns, got %d", len(order))
	}
	bOpts := order[1].opts
	if bOpts.SystemPrompt == "" {
		t.Fatal("expected B's SystemPrompt to contain upstream output, got empty")
	}
	if !strings.Contains(bOpts.SystemPrompt, "analysis result from A") {
		t.Fatalf("B's SystemPrompt should contain A's output, got: %q", bOpts.SystemPrompt)
	}
}

func TestEngine_Execute_Performance(t *testing.T) {
	// Given: 10 independent agents (NFR21: <= 10 agents, startup delay <= 2s excluding LLM)
	agentSpecs := make(map[string]*AgentSpec)
	for i := range 10 {
		name := fmt.Sprintf("agent%d", i)
		agentSpecs[name] = &AgentSpec{Intent: fmt.Sprintf("task %d", i)}
	}
	spec := &ComposeSpec{
		Version: "1.0",
		Intent:  "performance test",
		Agents:  agentSpecs,
	}
	ks := newMockKernelSpawner()

	engine, err := NewEngine(spec, ks, mockAgentLoader)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	// When: executing and measuring elapsed time
	start := time.Now()
	results, err := engine.Execute(context.Background())
	elapsed := time.Since(start)

	// Then: all 10 agents succeed and total time <= 2s
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if len(results) != 10 {
		t.Fatalf("expected 10 results, got %d", len(results))
	}
	if elapsed > 2*time.Second {
		t.Fatalf("NFR21 violation: 10 agents took %v (max 2s)", elapsed)
	}
}

func TestEngine_Execute_AgentWithSkills(t *testing.T) {
	// Given: an agent spec with skills list
	spec := &ComposeSpec{
		Version: "1.0",
		Intent:  "skills test",
		Agents: map[string]*AgentSpec{
			"worker": {Intent: "do work", Skills: []string{"code-analyst", "reviewer"}},
		},
	}
	ks := newMockKernelSpawner()
	engine, err := NewEngine(spec, ks, mockAgentLoader)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	// When: executing
	results, err := engine.Execute(context.Background())

	// Then: execution succeeds
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
}

func TestEngine_Execute_ModelPassthrough(t *testing.T) {
	// Given: agents with different model specifications
	spec := &ComposeSpec{
		Version: "1.0",
		Intent:  "model test",
		Agents: map[string]*AgentSpec{
			"fast":    {Intent: "quick task", Model: "haiku"},
			"precise": {Intent: "deep analysis", Model: "opus"},
			"default": {Intent: "normal task"},
		},
	}
	ks := newMockKernelSpawner()
	engine, err := NewEngine(spec, ks, mockAgentLoader)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	// When: executing
	results, err := engine.Execute(context.Background())

	// Then: all agents succeed and models are passed through to Spawn
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// Verify models were passed correctly in spawn opts
	modelByIntent := make(map[string]string)
	for _, rec := range ks.spawned {
		modelByIntent[rec.intent] = rec.opts.Model
	}
	if modelByIntent["quick task"] != "haiku" {
		t.Errorf("expected model 'haiku' for 'quick task', got %q", modelByIntent["quick task"])
	}
	if modelByIntent["deep analysis"] != "opus" {
		t.Errorf("expected model 'opus' for 'deep analysis', got %q", modelByIntent["deep analysis"])
	}
	if modelByIntent["normal task"] != "" {
		t.Errorf("expected empty model for 'normal task', got %q", modelByIntent["normal task"])
	}
}

func TestEngine_Execute_GlobalModelDefault(t *testing.T) {
	// Given: a spec with global model and agents with/without overrides
	spec := &ComposeSpec{
		Version: "1.0",
		Intent:  "global model test",
		Model:   "haiku",
		Agents: map[string]*AgentSpec{
			"fast":     {Intent: "cheap task"},                  // inherits global "haiku"
			"precise":  {Intent: "deep analysis", Model: "opus"}, // overrides to "opus"
			"standard": {Intent: "normal task"},                  // inherits global "haiku"
		},
	}
	ks := newMockKernelSpawner()
	engine, err := NewEngine(spec, ks, mockAgentLoader)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	// When: executing
	results, err := engine.Execute(context.Background())

	// Then: all agents succeed
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// Verify model resolution: agent model > global model
	modelByIntent := make(map[string]string)
	for _, rec := range ks.spawned {
		modelByIntent[rec.intent] = rec.opts.Model
	}
	if modelByIntent["cheap task"] != "haiku" {
		t.Errorf("expected global model 'haiku' for 'cheap task', got %q", modelByIntent["cheap task"])
	}
	if modelByIntent["deep analysis"] != "opus" {
		t.Errorf("expected agent model 'opus' for 'deep analysis', got %q", modelByIntent["deep analysis"])
	}
	if modelByIntent["normal task"] != "haiku" {
		t.Errorf("expected global model 'haiku' for 'normal task', got %q", modelByIntent["normal task"])
	}
}

func TestEngine_Execute_AgentWithRef(t *testing.T) {
	// Given: an agent spec with agent reference field
	spec := &ComposeSpec{
		Version: "1.0",
		Intent:  "agent ref test",
		Agents: map[string]*AgentSpec{
			"worker": {Intent: "do work", Agent: "my-agent"},
		},
	}
	ks := newMockKernelSpawner()
	engine, err := NewEngine(spec, ks, mockAgentLoader)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	// When: executing
	results, err := engine.Execute(context.Background())

	// Then: execution succeeds and the agent loader was used
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
}

func TestEngine_Execute_PartialFailure(t *testing.T) {
	// Given: diamond where B fails but C succeeds; D depends on both
	spec := &ComposeSpec{
		Version: "1.0",
		Intent:  "partial failure",
		Agents: map[string]*AgentSpec{
			"a": {Intent: "root"},
			"b": {Intent: "will fail", DependsOn: map[string]string{"a": "completed"}},
			"c": {Intent: "will succeed", DependsOn: map[string]string{"a": "completed"}},
			"d": {Intent: "needs both", DependsOn: map[string]string{"b": "completed", "c": "completed"}},
		},
	}
	ks := newMockKernelSpawner()
	// PID 2 (second spawn = B) will fail
	ks.results[types.PID(2)] = mockExecResult{exitCode: 1, reason: "failed", err: fmt.Errorf("process crashed")}

	engine, err := NewEngine(spec, ks, mockAgentLoader)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	// When: executing
	results, err := engine.Execute(context.Background())

	// Then: A and C succeed, B fails, D is skipped
	_ = results
	_ = err
	// D should not be spawned because B (one of its deps) failed
	order := ks.getSpawnOrder()
	for _, intent := range order {
		if intent == "needs both" {
			t.Fatal("agent D should NOT have been spawned (upstream B failed)")
		}
	}
}

// ============================================================
// ATDD RED PHASE — Story 10.3: Token 预算管理 (AC2)
//
// Tests reference AgentSpec.ContextBudget and ComposeSpawnOpts.ContextBudget
// which do NOT exist yet → compile failure = RED phase.
// ============================================================

// --- 10.3-UNIT-020: [P0] AgentSpec.ContextBudget passed to ComposeSpawnOpts ---

func TestEngine_Execute_ContextBudgetPassthrough(t *testing.T) {
	spec := &ComposeSpec{
		Version: "1.0",
		Intent:  "budget passthrough",
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

	if len(ks.spawned) != 1 {
		t.Fatalf("expected 1 spawn, got %d", len(ks.spawned))
	}
	if ks.spawned[0].opts.ContextBudget != 5000 {
		t.Errorf("expected ContextBudget 5000, got %d", ks.spawned[0].opts.ContextBudget)
	}
}

// --- 10.3-UNIT-021: [P1] AgentSpec without budget passes 0 ---

func TestEngine_Execute_NoBudgetPassesZero(t *testing.T) {
	spec := &ComposeSpec{
		Version: "1.0",
		Intent:  "no budget",
		Agents: map[string]*AgentSpec{
			"worker": {Intent: "do work"},
		},
	}
	ks := newMockKernelSpawner()
	engine, err := NewEngine(spec, ks, mockAgentLoader)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	_, err = engine.Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if len(ks.spawned) != 1 {
		t.Fatalf("expected 1 spawn, got %d", len(ks.spawned))
	}
	if ks.spawned[0].opts.ContextBudget != 0 {
		t.Errorf("expected ContextBudget 0 for no-budget agent, got %d", ks.spawned[0].opts.ContextBudget)
	}
}

// --- 10.3-UNIT-022: [P1] Multiple agents with different budgets ---

func TestEngine_Execute_MultipleBudgets(t *testing.T) {
	spec := &ComposeSpec{
		Version: "1.0",
		Intent:  "mixed budgets",
		Agents: map[string]*AgentSpec{
			"cheap":     {Intent: "cheap task", ContextBudget: 1000},
			"expensive": {Intent: "expensive task", ContextBudget: 50000},
			"unlimited": {Intent: "unlimited task"},
		},
	}
	ks := newMockKernelSpawner()
	engine, err := NewEngine(spec, ks, mockAgentLoader)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	_, err = engine.Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	budgetByIntent := make(map[string]int)
	for _, rec := range ks.spawned {
		budgetByIntent[rec.intent] = rec.opts.ContextBudget
	}
	if budgetByIntent["cheap task"] != 1000 {
		t.Errorf("expected 1000 for cheap, got %d", budgetByIntent["cheap task"])
	}
	if budgetByIntent["expensive task"] != 50000 {
		t.Errorf("expected 50000 for expensive, got %d", budgetByIntent["expensive task"])
	}
	if budgetByIntent["unlimited task"] != 0 {
		t.Errorf("expected 0 for unlimited, got %d", budgetByIntent["unlimited task"])
	}
}

func TestEngine_Execute_EmptyAfterCancel(t *testing.T) {
	// Given: immediate context cancellation
	spec := &ComposeSpec{
		Version: "1.0",
		Intent:  "instant cancel",
		Agents: map[string]*AgentSpec{
			"a": {Intent: "task A"},
		},
	}
	ks := newMockKernelSpawner()
	ks.results[types.PID(1)] = mockExecResult{delay: 5 * time.Second}

	engine, err := NewEngine(spec, ks, mockAgentLoader)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}

	// When: executing with already-cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, execErr := engine.Execute(ctx)

	// Then: context error returned
	if execErr == nil {
		t.Fatal("expected context cancellation error")
	}
}
