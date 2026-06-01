package compose

import (
	"context"
	"testing"

	"github.com/rnixai/rnix/agents"
	"github.com/rnixai/rnix/internal/types"
)

// ============================================================
// ATDD RED PHASE — Story 21.1: Compose Engine BudgetPool Integration
// Supplementary tests (unique tests not in engine_test.go or budget_pool_integration_test.go)
// ============================================================

// --- 21.1-INT-004: [P1] Pool quota and agent context_budget take min ---

func TestEngine_Execute_QuotaAndMaxTokensMin(t *testing.T) {
	spec := &ComposeSpec{
		Version:     "1.0",
		Intent:      "min budget test",
		TokenBudget: 50000,
		Agents: map[string]*AgentSpec{
			"limited": {
				Intent:        "limited agent",
				Priority:      "high",
				MaxTokens: 3000, // agent's own limit is lower than pool quota
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

	_, err = engine.Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if len(mock.spawned) != 1 {
		t.Fatalf("expected 1 spawn, got %d", len(mock.spawned))
	}

	// Pool would allocate full budget to single agent (50000),
	// but agent's own context_budget (3000) should cap it
	if mock.spawned[0].opts.MaxTokens != 3000 {
		t.Errorf("expected MaxTokens 3000 (min of pool quota and agent limit), got %d",
			mock.spawned[0].opts.MaxTokens)
	}
}

// --- 21.1-INT-006: [P0] KernelSpawner.GetTokensUsed returns process consumption ---

func TestKernelSpawner_GetTokensUsed(t *testing.T) {
	mock := newMockKernelSpawnerWithTokens()
	mock.tokenUsed[types.PID(1)] = 3500

	used, ok := mock.GetTokensUsed(types.PID(1))
	if !ok {
		t.Fatal("expected GetTokensUsed to return true for known PID")
	}
	if used != 3500 {
		t.Errorf("expected 3500 tokens used, got %d", used)
	}

	_, ok = mock.GetTokensUsed(types.PID(999))
	if ok {
		t.Error("expected GetTokensUsed to return false for unknown PID")
	}
}

// ============================================================
// Mock extension for BudgetPool integration tests
// ============================================================

type mockKernelSpawnerWithTokensImpl struct {
	*mockKernelSpawner
	tokenUsed map[types.PID]int
}

func newMockKernelSpawnerWithTokens() *mockKernelSpawnerWithTokensImpl {
	return &mockKernelSpawnerWithTokensImpl{
		mockKernelSpawner: newMockKernelSpawner(),
		tokenUsed:         make(map[types.PID]int),
	}
}

func (m *mockKernelSpawnerWithTokensImpl) GetTokensUsed(pid types.PID) (int, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	used, ok := m.tokenUsed[pid]
	return used, ok
}
