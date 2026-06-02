package title

import (
	"testing"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// TestComputeCtxPercent_SelectedPIDHit covers the happy path where the
// selected process is in the slice with positive ContextBudget.
func TestComputeCtxPercent_SelectedPIDHit(t *testing.T) {
	procs := []vfs.ProcInfo{
		{PID: 1, LastInputTokens: 50, ContextBudget: 100},
		{PID: 2, LastInputTokens: 200, ContextBudget: 1000},
	}
	if pct := ComputeCtxPercent(1, "", procs); pct != 50 {
		t.Errorf("PID=1 expected 50, got %d", pct)
	}
	if pct := ComputeCtxPercent(2, "", procs); pct != 20 {
		t.Errorf("PID=2 expected 20, got %d", pct)
	}
}

// TestComputeCtxPercent_SelectedPIDZeroBudget covers ContextBudget == 0
// (process not yet configured) → returns 0 instead of dividing by zero.
func TestComputeCtxPercent_SelectedPIDZeroBudget(t *testing.T) {
	procs := []vfs.ProcInfo{{PID: 1, LastInputTokens: 50, ContextBudget: 0}}
	if pct := ComputeCtxPercent(1, "", procs); pct != 0 {
		t.Errorf("zero budget expected 0, got %d", pct)
	}
}

// TestComputeCtxPercent_SelectedPIDNotFound covers reaped PID — selectedPID
// no longer present in slice (reaper between ticks) → returns 0.
func TestComputeCtxPercent_SelectedPIDNotFound(t *testing.T) {
	procs := []vfs.ProcInfo{{PID: 1, LastInputTokens: 50, ContextBudget: 100}}
	if pct := ComputeCtxPercent(999, "", procs); pct != 0 {
		t.Errorf("missing PID expected 0, got %d", pct)
	}
}

// TestComputeCtxPercent_NoSelectionAggregate covers selectedPID == 0 (no
// selection) — takes max per-step input usage across Running + Created processes.
// Dead/Zombie processes excluded.
func TestComputeCtxPercent_NoSelectionAggregate(t *testing.T) {
	procs := []vfs.ProcInfo{
		{PID: 1, LastInputTokens: 30, ContextBudget: 100, State: types.StateRunning},
		{PID: 2, LastInputTokens: 20, ContextBudget: 100, State: types.StateCreated},
		{PID: 3, LastInputTokens: 50, ContextBudget: 100, State: types.StateDead},
	}
	// max(30/100, 20/100) = 30%
	if pct := ComputeCtxPercent(0, "", procs); pct != 30 {
		t.Errorf("aggregate expected 30, got %d", pct)
	}
}

// TestComputeCtxPercent_NoSelectionEmpty covers selectedPID == 0 + no running
// processes → returns 0.
func TestComputeCtxPercent_NoSelectionEmpty(t *testing.T) {
	if pct := ComputeCtxPercent(0, "", nil); pct != 0 {
		t.Errorf("empty slice expected 0, got %d", pct)
	}
	procs := []vfs.ProcInfo{
		{PID: 1, LastInputTokens: 50, ContextBudget: 0, State: types.StateRunning},
	}
	if pct := ComputeCtxPercent(0, "", procs); pct != 0 {
		t.Errorf("zero-budget aggregate expected 0, got %d", pct)
	}
}

// TestComputeCtxPercent_ClampApplied covers ClampPercent integration —
// usage > 100% (over budget) is clamped to displayable range.
func TestComputeCtxPercent_ClampApplied(t *testing.T) {
	procs := []vfs.ProcInfo{
		{PID: 1, LastInputTokens: 1500, ContextBudget: 100},
	}
	pct := ComputeCtxPercent(1, "", procs)
	if pct != 999 {
		t.Errorf("over-budget clamp expected 999, got %d", pct)
	}
}

// TestComputeBudgetPercent_SelectedPIDLEZero covers selectedPID <= 0 →
// returns 0 (no Title Bar budget % display when no selection).
func TestComputeBudgetPercent_SelectedPIDLEZero(t *testing.T) {
	procs := []vfs.ProcInfo{
		{PID: 1, MaxCost: 1.0, UsedCost: 0.5},
	}
	if pct := ComputeBudgetPercent(0, "", procs); pct != 0 {
		t.Errorf("PID=0 expected 0, got %d", pct)
	}
}

// TestComputeBudgetPercent_CostBased covers MaxCost > 0 path (cost-based
// priority over token-based).
func TestComputeBudgetPercent_CostBased(t *testing.T) {
	procs := []vfs.ProcInfo{
		{PID: 1, MaxCost: 1.0, UsedCost: 0.5, MaxTokens: 1000, TokensUsed: 800},
	}
	if pct := ComputeBudgetPercent(1, "", procs); pct != 50 {
		t.Errorf("cost-based expected 50, got %d", pct)
	}
}

// TestComputeBudgetPercent_TokenBased covers MaxTokens > 0 path (when no cost budget).
func TestComputeBudgetPercent_TokenBased(t *testing.T) {
	procs := []vfs.ProcInfo{
		{PID: 1, MaxTokens: 1000, TokensUsed: 800},
	}
	if pct := ComputeBudgetPercent(1, "", procs); pct != 80 {
		t.Errorf("token-based expected 80, got %d", pct)
	}
}

// TestComputeBudgetPercent_NoBudget covers no budget configured → 0.
func TestComputeBudgetPercent_NoBudget(t *testing.T) {
	procs := []vfs.ProcInfo{
		{PID: 1, TokensUsed: 500},
	}
	if pct := ComputeBudgetPercent(1, "", procs); pct != 0 {
		t.Errorf("no budget expected 0, got %d", pct)
	}
}
