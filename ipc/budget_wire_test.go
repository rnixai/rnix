package ipc

import (
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

func TestProcInfoWire_BudgetFields_RoundTrip(t *testing.T) {
	original := vfs.ProcInfo{
		PID:           42,
		UUID:          "test-uuid",
		State:         types.StateRunning,
		Intent:        "test",
		Skills:        []string{"skill1"},
		TokensUsed:    12345,
		ContextBudget: 50000,
		MaxTokens:     100000,
		MaxCost:       5.0,
		UsedCost:      1.23,
		MaxSteps:      100,
		CreatedAt:     time.Now(),
		Provider:      "claude",
		Model:         "sonnet",
		StepTimeout:   5 * time.Minute,
	}

	wire := ProcInfoToWire(original)
	if wire.MaxTokens != 100000 {
		t.Errorf("wire MaxTokens: expected 100000, got %d", wire.MaxTokens)
	}
	if wire.MaxCost != 5.0 {
		t.Errorf("wire MaxCost: expected 5.0, got %f", wire.MaxCost)
	}
	if wire.UsedCost != 1.23 {
		t.Errorf("wire UsedCost: expected 1.23, got %f", wire.UsedCost)
	}

	roundTripped := WireToProcInfo(wire)
	if roundTripped.MaxTokens != original.MaxTokens {
		t.Errorf("round-trip MaxTokens: expected %d, got %d", original.MaxTokens, roundTripped.MaxTokens)
	}
	if roundTripped.MaxCost != original.MaxCost {
		t.Errorf("round-trip MaxCost: expected %f, got %f", original.MaxCost, roundTripped.MaxCost)
	}
	if roundTripped.UsedCost != original.UsedCost {
		t.Errorf("round-trip UsedCost: expected %f, got %f", original.UsedCost, roundTripped.UsedCost)
	}
}
