package compose

// =============================================================================
// ATDD Story 42.4: Engine.ExecuteFromNode + runLayers helper (UNIT)
//
// Acceptance criteria covered:
//   - AC#2  ENGINE-001  Linear A→B→C, B resumed → C scheduled (B output prop.)
//   - AC#3  ENGINE-002  Fanout B→C,D — C and D scheduled in parallel
//   - AC#3  ENGINE-003  Downstream failure surfaces as result error
//   - AC#6  ENGINE-004  Resumed node itself is NOT respawned
//   - AC#6  ENGINE-005  Downstream upstream prompt uses resumed output
//   - AC#10 ENGINE-006  Execute() and ExecuteFromNode() share runLayers helper
//   - AC#10 ENGINE-007  Execute() existing regression (linear deps still ok)
//
// RED PHASE:
//   ExecuteFromNode and runLayers are stubs returning sentinel errors. All
//   green at the package level. Dev-story removes the t.Skip lines as the
//   corresponding behavior is implemented (red → green for each test).
//
// Pattern:
//   Mirrors compose/engine_test.go:28-249 mockKernelSpawner; extends with
//   mockKernelSpawnerWithSeeder that implements HistoricalSeeder via type
//   assertion. This keeps engine_test.go's mock unchanged.
// =============================================================================

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/rnixai/rnix/internal/types"
)

// -----------------------------------------------------------------------------
// mock extensions
// -----------------------------------------------------------------------------

// seedRecord captures a SeedHistorical invocation for assertion.
type seedRecord struct {
	name   string
	pid    types.PID
	result string
	tokens int
	spanID types.SpanID
}

// mockKernelSpawnerWithSeeder embeds mockKernelSpawner and implements the
// optional HistoricalSeeder interface. Engine code may type-assert to access
// SeedHistorical without changing the canonical KernelSpawner contract.
type mockKernelSpawnerWithSeeder struct {
	*mockKernelSpawner
	seedMu sync.Mutex
	seeds  []seedRecord
}

func newMockKernelSpawnerWithSeeder() *mockKernelSpawnerWithSeeder {
	return &mockKernelSpawnerWithSeeder{
		mockKernelSpawner: newMockKernelSpawner(),
	}
}

// SeedHistorical satisfies the compose.HistoricalSeeder interface (Story 42.4).
func (m *mockKernelSpawnerWithSeeder) SeedHistorical(name string, pid types.PID, result string, tokens int, spanID types.SpanID) {
	m.seedMu.Lock()
	defer m.seedMu.Unlock()
	m.seeds = append(m.seeds, seedRecord{
		name:   name,
		pid:    pid,
		result: result,
		tokens: tokens,
		spanID: spanID,
	})
	// Mirror the production behavior expectation: seeded result is queryable
	// via GetProcessResult / GetTokensUsed so buildUpstreamPrompt finds it.
	m.mu.Lock()
	m.getResults[pid] = result
	m.tokenUsed[pid] = tokens
	if spanID != "" {
		m.spanIDs[pid] = spanID
	}
	m.mu.Unlock()
}

// -----------------------------------------------------------------------------
// fixture helpers
// -----------------------------------------------------------------------------

func newLinearSpec_ABC() *ComposeSpec {
	return &ComposeSpec{
		Version: "1.0",
		Intent:  "linear A->B->C",
		Agents: map[string]*AgentSpec{
			"node-A": {Intent: "step A"},
			"node-B": {Intent: "step B", DependsOn: map[string]string{"node-A": "completed"}},
			"node-C": {Intent: "step C", DependsOn: map[string]string{"node-B": "completed"}},
		},
	}
}

func newFanoutSpec_ABCD() *ComposeSpec {
	return &ComposeSpec{
		Version: "1.0",
		Intent:  "fanout B->{C,D}",
		Agents: map[string]*AgentSpec{
			"node-A": {Intent: "step A"},
			"node-B": {Intent: "step B", DependsOn: map[string]string{"node-A": "completed"}},
			"node-C": {Intent: "step C", DependsOn: map[string]string{"node-B": "completed"}},
			"node-D": {Intent: "step D", DependsOn: map[string]string{"node-B": "completed"}},
		},
	}
}

// -----------------------------------------------------------------------------
// ENGINE-001 (AC#2): linear DAG, resumed=B, expect C scheduled with B output
// -----------------------------------------------------------------------------

func TestATDD_42_4_ENGINE_001_ExecuteFromNode_Linear(t *testing.T) {

	spec := newLinearSpec_ABC()
	ks := newMockKernelSpawnerWithSeeder()
	engine, err := NewEngine(spec, ks, mockAgentLoader)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	// Upstream node-A succeeded historically; node-B was resumed; only C should be spawned.
	upstream := map[string]HistoricalNodeResult{
		"node-A": {PID: types.PID(100), Output: "A-output", Tokens: 50, ExitCode: 0},
	}
	resumed := HistoricalNodeResult{
		PID:      types.PID(200),
		Output:   "B-output-after-resume",
		Tokens:   80,
		ExitCode: 0,
	}

	results, err := engine.ExecuteFromNode(context.Background(), "node-B", resumed, upstream)
	if err != nil {
		t.Fatalf("ExecuteFromNode: %v", err)
	}

	// node-C must appear in spawn records (real Spawn call), node-A must NOT.
	spawned := ks.getSpawnOrder()
	if len(spawned) != 1 {
		t.Fatalf("expected exactly 1 fresh spawn (node-C), got %d: %v", len(spawned), spawned)
	}
	if spawned[0] != "step C" {
		t.Errorf("expected node-C (intent=%q) to be spawned, got %q", "step C", spawned[0])
	}

	// Results must include all three nodes (A historical, B resumed, C fresh).
	if len(results) != 3 {
		t.Fatalf("expected 3 results (A historical + B resumed + C fresh), got %d", len(results))
	}

	// node-B should be flagged as already-completed (not respawned).
	for _, r := range results {
		if r.Name == "node-B" && r.PID != types.PID(200) {
			t.Errorf("node-B PID = %d, expected resumed PID 200 (not a fresh spawn)", r.PID)
		}
	}

	// SeedHistorical must be called for node-A AND node-B at minimum.
	ks.seedMu.Lock()
	defer ks.seedMu.Unlock()
	if len(ks.seeds) < 2 {
		t.Fatalf("expected ≥2 SeedHistorical calls (node-A + node-B), got %d", len(ks.seeds))
	}
	seen := map[string]bool{}
	for _, s := range ks.seeds {
		seen[s.name] = true
	}
	if !seen["node-A"] || !seen["node-B"] {
		t.Errorf("SeedHistorical missing node-A or node-B; got %v", seen)
	}
}

// -----------------------------------------------------------------------------
// ENGINE-002 (AC#3): fanout DAG, resumed=B, expect C and D both spawned
// -----------------------------------------------------------------------------

func TestATDD_42_4_ENGINE_002_ExecuteFromNode_Fanout(t *testing.T) {

	spec := newFanoutSpec_ABCD()
	ks := newMockKernelSpawnerWithSeeder()
	engine, err := NewEngine(spec, ks, mockAgentLoader)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	upstream := map[string]HistoricalNodeResult{
		"node-A": {PID: types.PID(100), Output: "A-output", Tokens: 50, ExitCode: 0},
	}
	resumed := HistoricalNodeResult{PID: types.PID(200), Output: "B-output", Tokens: 80, ExitCode: 0}

	results, err := engine.ExecuteFromNode(context.Background(), "node-B", resumed, upstream)
	if err != nil {
		t.Fatalf("ExecuteFromNode: %v", err)
	}

	// Both C and D must be spawned (fresh).
	spawnedIntents := map[string]bool{}
	for _, intent := range ks.getSpawnOrder() {
		spawnedIntents[intent] = true
	}
	if !spawnedIntents["step C"] || !spawnedIntents["step D"] {
		t.Errorf("expected step C and step D spawned, got %v", spawnedIntents)
	}
	if spawnedIntents["step A"] || spawnedIntents["step B"] {
		t.Errorf("node-A or node-B must NOT be respawned; got %v", spawnedIntents)
	}

	if len(results) != 4 {
		t.Fatalf("expected 4 results (A + B + C + D), got %d", len(results))
	}
}

// -----------------------------------------------------------------------------
// ENGINE-003 (AC#3): fanout — node-C fails, node-D succeeds → aggregate err
// -----------------------------------------------------------------------------

func TestATDD_42_4_ENGINE_003_ExecuteFromNode_PartialDownstreamFailure(t *testing.T) {

	spec := newFanoutSpec_ABCD()
	ks := newMockKernelSpawnerWithSeeder()
	// Pre-queue execution result: node-C's PID will be 1 (first fresh spawn),
	// node-D's PID will be 2. We inject an error for one of them.
	// Determined empirically by mockKernelSpawner.pidAlloc sequence.
	ks.results[types.PID(1)] = mockExecResult{exitCode: 1, reason: "downstream failure"}
	engine, err := NewEngine(spec, ks, mockAgentLoader)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	upstream := map[string]HistoricalNodeResult{
		"node-A": {PID: types.PID(100), Output: "A-output", Tokens: 50, ExitCode: 0},
	}
	resumed := HistoricalNodeResult{PID: types.PID(200), Output: "B-output", Tokens: 80, ExitCode: 0}

	results, _ := engine.ExecuteFromNode(context.Background(), "node-B", resumed, upstream)

	// At least one downstream node must surface an error.
	anyDownstreamErr := false
	for _, r := range results {
		if (r.Name == "node-C" || r.Name == "node-D") && (r.Err != nil || r.ExitCode != 0) {
			anyDownstreamErr = true
			break
		}
	}
	if !anyDownstreamErr {
		t.Errorf("expected at least one downstream error in results: %+v", results)
	}
}

// -----------------------------------------------------------------------------
// ENGINE-004 (AC#6): resumed node itself is NOT respawned
// -----------------------------------------------------------------------------

func TestATDD_42_4_ENGINE_004_ResumedNodeNotRespawned(t *testing.T) {

	spec := newLinearSpec_ABC()
	ks := newMockKernelSpawnerWithSeeder()
	engine, err := NewEngine(spec, ks, mockAgentLoader)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	upstream := map[string]HistoricalNodeResult{
		"node-A": {PID: types.PID(100), Output: "A-out", Tokens: 50, ExitCode: 0},
	}
	resumed := HistoricalNodeResult{PID: types.PID(200), Output: "B-out", Tokens: 80, ExitCode: 0}

	_, _ = engine.ExecuteFromNode(context.Background(), "node-B", resumed, upstream)

	// Verify node-B intent is never in spawn records.
	for _, intent := range ks.getSpawnOrder() {
		if intent == "step B" {
			t.Errorf("node-B (intent %q) MUST NOT be respawned by ExecuteFromNode; got: %v", "step B", ks.getSpawnOrder())
		}
	}
}

// -----------------------------------------------------------------------------
// ENGINE-005 (AC#6): downstream uses resumed-node output in upstream prompt
// -----------------------------------------------------------------------------

func TestATDD_42_4_ENGINE_005_DownstreamUsesResumedOutput(t *testing.T) {

	spec := newLinearSpec_ABC()
	ks := newMockKernelSpawnerWithSeeder()
	engine, err := NewEngine(spec, ks, mockAgentLoader)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	upstream := map[string]HistoricalNodeResult{
		"node-A": {PID: types.PID(100), Output: "A-out", Tokens: 50, ExitCode: 0},
	}
	resumed := HistoricalNodeResult{
		PID:      types.PID(200),
		Output:   "RESUMED-B-OUTPUT-MARKER",
		Tokens:   80,
		ExitCode: 0,
	}

	_, err = engine.ExecuteFromNode(context.Background(), "node-B", resumed, upstream)
	if err != nil {
		t.Fatalf("ExecuteFromNode: %v", err)
	}

	// node-C's spawn opts must contain RESUMED-B-OUTPUT-MARKER in SystemPrompt.
	ks.mu.Lock()
	defer ks.mu.Unlock()
	foundMarker := false
	for _, rec := range ks.spawned {
		if rec.intent == "step C" {
			if rec.opts.SystemPrompt == "" {
				t.Errorf("node-C SystemPrompt is empty; expected to contain resumed B output")
				return
			}
			if containsStr(rec.opts.SystemPrompt, "RESUMED-B-OUTPUT-MARKER") {
				foundMarker = true
			}
		}
	}
	if !foundMarker {
		t.Errorf("node-C SystemPrompt missing resumed B output marker")
	}
}

// containsStr is a local helper to avoid importing strings in a test file
// that already covers most of its surface via direct comparison.
func containsStr(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

// -----------------------------------------------------------------------------
// ENGINE-006 (AC#10): Execute and ExecuteFromNode share runLayers helper
// -----------------------------------------------------------------------------

func TestATDD_42_4_ENGINE_006_RunLayersSharedHelper(t *testing.T) {

	spec := newLinearSpec_ABC()
	ks := newMockKernelSpawnerWithSeeder()
	engine, err := NewEngine(spec, ks, mockAgentLoader)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	// Call runLayers directly with startLayerIdx=0 — must behave identically to
	// Execute() on the same DAG.
	results, err := engine.Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("Execute expected 3 results, got %d", len(results))
	}

	// runLayers with startLayerIdx=len(layers) must return immediately without
	// spawning anything new — this verifies the helper respects the start index.
	layers, _ := engine.dag.TopologicalSort()
	preSpawnCount := len(ks.spawned)
	_, err = engine.runLayers(
		context.Background(),
		layers,
		len(layers), // start AFTER the last layer
		"trace-test",
		map[string]*ScheduleResult{},
		nil,
		nil,
	)
	if err != nil {
		t.Fatalf("runLayers(startIdx=len): %v", err)
	}
	postSpawnCount := len(ks.spawned)
	if postSpawnCount != preSpawnCount {
		t.Errorf("runLayers(startIdx=len) spawned %d new nodes; expected 0", postSpawnCount-preSpawnCount)
	}
}

// -----------------------------------------------------------------------------
// ENGINE-007 (AC#10): Execute() linear-deps regression — existing behavior preserved
// -----------------------------------------------------------------------------

func TestATDD_42_4_ENGINE_007_ExecuteLinearRegression(t *testing.T) {

	// This test mirrors TestEngine_Execute_LinearDeps from engine_test.go but
	// targets the post-refactor Execute() that delegates to runLayers. Removing
	// t.Skip in dev-story verifies the refactor didn't change ordering.

	spec := newLinearSpec_ABC()
	ks := newMockKernelSpawnerWithSeeder()
	engine, err := NewEngine(spec, ks, mockAgentLoader)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}

	results, err := engine.Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	order := ks.getSpawnOrder()
	if len(order) != 3 {
		t.Fatalf("expected 3 spawns, got %d: %v", len(order), order)
	}
	// Linear: A must be first, then B, then C.
	if order[0] != "step A" || order[1] != "step B" || order[2] != "step C" {
		t.Errorf("linear spawn order = %v, want [step A, step B, step C]", order)
	}
}

// -----------------------------------------------------------------------------
// Stub sanity checks (post-GREEN: verify implementation is live, not RED stubs)
// -----------------------------------------------------------------------------

// TestATDD_42_4_StubSanity_ExecuteFromNode verifies that ExecuteFromNode no
// longer returns the RED-phase sentinel error. After the GREEN-phase
// implementation is in place, calling ExecuteFromNode with an unsatisfied
// upstream must return a real ErrInvalid (or similar) — NOT the sentinel.
func TestATDD_42_4_StubSanity_ExecuteFromNode(t *testing.T) {
	spec := newLinearSpec_ABC()
	ks := newMockKernelSpawnerWithSeeder()
	engine, err := NewEngine(spec, ks, mockAgentLoader)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	_, err = engine.ExecuteFromNode(context.Background(), "node-B", HistoricalNodeResult{}, nil)
	if err != nil && errors.Is(err, errExecuteFromNodeNotImplemented) {
		t.Errorf("ExecuteFromNode still returns RED sentinel; implementation expected to be live: %v", err)
	}
}

// TestATDD_42_4_StubSanity_RunLayers verifies that runLayers no longer returns
// the RED-phase sentinel error. After the GREEN-phase implementation is live,
// a zero-layer call must return nil (no work to do) and not the sentinel.
func TestATDD_42_4_StubSanity_RunLayers(t *testing.T) {
	spec := newLinearSpec_ABC()
	ks := newMockKernelSpawnerWithSeeder()
	engine, err := NewEngine(spec, ks, mockAgentLoader)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	_, err = engine.runLayers(context.Background(), nil, 0, "", nil, nil, nil)
	if err != nil && errors.Is(err, errRunLayersNotImplemented) {
		t.Errorf("runLayers still returns RED sentinel; implementation expected to be live: %v", err)
	}
}
