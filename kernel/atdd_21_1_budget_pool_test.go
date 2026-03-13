package kernel

import (
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
)

// ============================================================
// ATDD RED PHASE — Story 21.1: Token 预算池与分配调度
// Supplementary tests (unique tests not in budget_pool_test.go)
// ============================================================

// --- 21.1-UNIT-010: [P1] Priority constants have correct values ---

func TestPriority_Values(t *testing.T) {
	if PriorityHigh <= PriorityNormal {
		t.Errorf("PriorityHigh (%d) should be > PriorityNormal (%d)", PriorityHigh, PriorityNormal)
	}
	if PriorityNormal <= PriorityLow {
		t.Errorf("PriorityNormal (%d) should be > PriorityLow (%d)", PriorityNormal, PriorityLow)
	}
}

// --- 21.1-UNIT-011: [P1] ParsePriority converts strings correctly ---

func TestParsePriority(t *testing.T) {
	tests := []struct {
		input    string
		expected Priority
	}{
		{"high", PriorityHigh},
		{"normal", PriorityNormal},
		{"low", PriorityLow},
		{"", PriorityNormal},   // default
		{"HIGH", PriorityHigh}, // case insensitive
	}
	for _, tt := range tests {
		got := ParsePriority(tt.input)
		if got != tt.expected {
			t.Errorf("ParsePriority(%q) = %d, want %d", tt.input, got, tt.expected)
		}
	}
}

// --- 21.1-UNIT-012: [P1] Allocation decision within 100ms (NFR43) ---

func TestBudgetPool_AllocationLatency(t *testing.T) {
	pool := NewBudgetPool(1000000)

	// Pre-allocate many agents
	for i := range 50 {
		pool.AllocateQuota(types.PID(i+1), "agent", PriorityNormal)
	}

	start := time.Now()
	for i := range 100 {
		_ = pool.RecordUsage(types.PID((i%50)+1), 10)
	}
	elapsed := time.Since(start)

	if elapsed > 100*time.Millisecond {
		t.Errorf("100 RecordUsage calls took %v, must be <= 100ms (NFR43)", elapsed)
	}
}

// --- 21.1-UNIT-013: [P0] Single agent gets full budget ---

func TestBudgetPool_SingleAgent(t *testing.T) {
	pool := NewBudgetPool(10000)
	q := pool.AllocateQuota(1, "solo", PriorityNormal)
	if q != 10000 {
		t.Errorf("single agent should get full budget: expected 10000, got %d", q)
	}
}

// --- 21.1-UNIT-014: [P0] Remaining returns correct value ---

func TestBudgetPool_Remaining(t *testing.T) {
	pool := NewBudgetPool(5000)
	pool.AllocateQuota(1, "agent", PriorityHigh)
	_ = pool.RecordUsage(1, 3000)

	remaining := pool.Remaining()
	if remaining != 2000 {
		t.Errorf("expected remaining 2000, got %d", remaining)
	}
}
