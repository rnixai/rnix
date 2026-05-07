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
		{PID: 1, TokensUsed: 50, ContextBudget: 100},
		{PID: 2, TokensUsed: 200, ContextBudget: 1000},
	}
	if pct := ComputeCtxPercent(1, procs); pct != 50 {
		t.Errorf("PID=1 expected 50, got %d", pct)
	}
	if pct := ComputeCtxPercent(2, procs); pct != 20 {
		t.Errorf("PID=2 expected 20, got %d", pct)
	}
}

// TestComputeCtxPercent_SelectedPIDZeroBudget covers ContextBudget == 0
// (process not yet configured) → returns 0 instead of dividing by zero.
func TestComputeCtxPercent_SelectedPIDZeroBudget(t *testing.T) {
	procs := []vfs.ProcInfo{{PID: 1, TokensUsed: 50, ContextBudget: 0}}
	if pct := ComputeCtxPercent(1, procs); pct != 0 {
		t.Errorf("zero budget expected 0, got %d", pct)
	}
}

// TestComputeCtxPercent_SelectedPIDNotFound covers reaped PID — selectedPID
// no longer present in slice (reaper between ticks) → returns 0.
func TestComputeCtxPercent_SelectedPIDNotFound(t *testing.T) {
	procs := []vfs.ProcInfo{{PID: 1, TokensUsed: 50, ContextBudget: 100}}
	if pct := ComputeCtxPercent(999, procs); pct != 0 {
		t.Errorf("missing PID expected 0, got %d", pct)
	}
}

// TestComputeCtxPercent_NoSelectionAggregate covers selectedPID == 0 (no
// selection) — aggregates Running + Created processes with positive budget.
// Dead/Zombie processes excluded.
func TestComputeCtxPercent_NoSelectionAggregate(t *testing.T) {
	procs := []vfs.ProcInfo{
		{PID: 1, TokensUsed: 30, ContextBudget: 100, State: types.StateRunning},
		{PID: 2, TokensUsed: 20, ContextBudget: 100, State: types.StateCreated},
		{PID: 3, TokensUsed: 50, ContextBudget: 100, State: types.StateDead}, // excluded
	}
	// (30+20)/(100+100) = 25%
	if pct := ComputeCtxPercent(0, procs); pct != 25 {
		t.Errorf("aggregate expected 25, got %d", pct)
	}
}

// TestComputeCtxPercent_NoSelectionEmpty covers selectedPID == 0 + no running
// processes → returns 0.
func TestComputeCtxPercent_NoSelectionEmpty(t *testing.T) {
	if pct := ComputeCtxPercent(0, nil); pct != 0 {
		t.Errorf("empty slice expected 0, got %d", pct)
	}
	procs := []vfs.ProcInfo{
		{PID: 1, TokensUsed: 50, ContextBudget: 0, State: types.StateRunning}, // budget zero excluded
	}
	if pct := ComputeCtxPercent(0, procs); pct != 0 {
		t.Errorf("zero-budget aggregate expected 0, got %d", pct)
	}
}

// TestComputeCtxPercent_ClampApplied covers ClampPercent integration —
// usage > 100% (over budget) is clamped to displayable range.
func TestComputeCtxPercent_ClampApplied(t *testing.T) {
	procs := []vfs.ProcInfo{
		{PID: 1, TokensUsed: 1500, ContextBudget: 100}, // 1500%
	}
	pct := ComputeCtxPercent(1, procs)
	if pct != 999 {
		t.Errorf("over-budget clamp expected 999, got %d", pct)
	}
}

// TestComputeBudgetPercent_SelectedPIDLEZero covers selectedPID <= 0 →
// returns 0 (no Title Bar budget % display when no selection).
// Note: types.PID is unsigned; only PID == 0 represents "no selection" in
// practice. The function still uses <= 0 in its signature for symmetry with
// the cmd/rnix wrapper.
func TestComputeBudgetPercent_SelectedPIDLEZero(t *testing.T) {
	procs := []vfs.ProcInfo{
		{PID: 1, MaxCost: 1.0, UsedCost: 0.5},
	}
	if pct := ComputeBudgetPercent(0, procs); pct != 0 {
		t.Errorf("PID=0 expected 0, got %d", pct)
	}
}

// TestComputeBudgetPercent_CostBased covers MaxCost > 0 path (cost-based
// priority over token-based).
func TestComputeBudgetPercent_CostBased(t *testing.T) {
	procs := []vfs.ProcInfo{
		{PID: 1, UsedCost: 0.6, MaxCost: 1.0, TokensUsed: 9999, MaxTokens: 100},
	}
	if pct := ComputeBudgetPercent(1, procs); pct != 60 {
		t.Errorf("cost-based expected 60, got %d", pct)
	}
}

// TestComputeBudgetPercent_TokenFallback covers MaxCost == 0 path (token-based
// fallback when cost not configured).
func TestComputeBudgetPercent_TokenFallback(t *testing.T) {
	procs := []vfs.ProcInfo{
		{PID: 1, MaxCost: 0, UsedCost: 0, TokensUsed: 50, MaxTokens: 100},
	}
	if pct := ComputeBudgetPercent(1, procs); pct != 50 {
		t.Errorf("token fallback expected 50, got %d", pct)
	}
}

// TestComputeBudgetPercent_NeitherConfigured covers MaxCost == 0 + MaxTokens == 0
// → returns 0 (no budget configured).
func TestComputeBudgetPercent_NeitherConfigured(t *testing.T) {
	procs := []vfs.ProcInfo{
		{PID: 1, MaxCost: 0, MaxTokens: 0, TokensUsed: 50},
	}
	if pct := ComputeBudgetPercent(1, procs); pct != 0 {
		t.Errorf("no-budget expected 0, got %d", pct)
	}
}

// TestComputeBudgetPercent_PIDNotFound covers selected PID no longer in slice
// (reaped) → returns 0.
func TestComputeBudgetPercent_PIDNotFound(t *testing.T) {
	procs := []vfs.ProcInfo{
		{PID: 1, MaxCost: 1.0, UsedCost: 0.5},
	}
	if pct := ComputeBudgetPercent(999, procs); pct != 0 {
		t.Errorf("missing PID expected 0, got %d", pct)
	}
}

// TestComputeBudgetPercent_ClampApplied covers ClampPercent integration —
// over-budget (e.g. 200% cost burned) clamped to 999.
func TestComputeBudgetPercent_ClampApplied(t *testing.T) {
	procs := []vfs.ProcInfo{
		{PID: 1, UsedCost: 20.0, MaxCost: 1.0}, // 2000%
	}
	if pct := ComputeBudgetPercent(1, procs); pct != 999 {
		t.Errorf("over-cost clamp expected 999, got %d", pct)
	}
}
