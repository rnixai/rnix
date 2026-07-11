package kernel

import (
	"sync"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
)

// ============================================================
// ATDD RED PHASE — Story 21.1: Token 预算池与分配调度
//
// Comprehensive BudgetPool tests covering all Acceptance Criteria.
// Tests reference BudgetPool, AgentQuota, Priority types and methods
// that do NOT exist yet. They will fail to COMPILE until implementation.
//
// NOTE: Additional BudgetPool tests are in atdd_21_1_budget_pool_test.go
// This file contains the core AC verification tests.
//
// RED → GREEN: implement the types and methods, tests compile and pass.
// ============================================================

// --- 21.1-UNIT-001: [P0] BudgetPool creation (AC1) ---

func TestBudgetPool_NewPool(t *testing.T) {
	pool := NewBudgetPool(50000)

	if pool == nil {
		t.Fatal("NewBudgetPool returned nil")
	}
	status := pool.GetStatus()
	if status.TotalBudget != 50000 {
		t.Fatalf("expected TotalBudget 50000, got %d", status.TotalBudget)
	}
	if status.Allocated != 0 {
		t.Fatalf("expected Allocated 0 for new pool, got %d", status.Allocated)
	}
	if status.Consumed != 0 {
		t.Fatalf("expected Consumed 0 for new pool, got %d", status.Consumed)
	}
	if pool.IsExhausted() {
		t.Fatal("new pool should not be exhausted")
	}
}

// --- 21.1-UNIT-002: [P0] Priority-based allocation (AC1/AC2) ---

func TestBudgetPool_AllocateByPriority_Ratio(t *testing.T) {
	pool := NewBudgetPool(50000)

	// high=10, normal=5, low=1 → totalWeight=16
	highQuota := pool.AllocateQuota(1, "reviewer", PriorityHigh)
	normalQuota := pool.AllocateQuota(2, "summarizer", PriorityNormal)
	lowQuota := pool.AllocateQuota(3, "formatter", PriorityLow)

	if highQuota <= normalQuota {
		t.Fatalf("high quota (%d) should be > normal (%d)", highQuota, normalQuota)
	}
	if normalQuota <= lowQuota {
		t.Fatalf("normal quota (%d) should be > low (%d)", normalQuota, lowQuota)
	}
	if highQuota <= 0 || normalQuota <= 0 || lowQuota <= 0 {
		t.Fatalf("all quotas should be positive: high=%d, normal=%d, low=%d", highQuota, normalQuota, lowQuota)
	}

	status := pool.GetStatus()
	if status.Allocated > status.TotalBudget {
		t.Fatalf("allocated (%d) exceeds total budget (%d)", status.Allocated, status.TotalBudget)
	}
}

// --- 21.1-UNIT-003: [P1] Equal priority agents get equal quotas ---

func TestBudgetPool_EqualPriority_EqualQuotas(t *testing.T) {
	pool := NewBudgetPool(30000)

	// Register all agents first, then check final quotas via GetStatus
	pool.AllocateQuota(1, "agent-a", PriorityNormal)
	pool.AllocateQuota(2, "agent-b", PriorityNormal)
	pool.AllocateQuota(3, "agent-c", PriorityNormal)

	status := pool.GetStatus()
	quotas := make(map[string]int)
	for _, q := range status.Quotas {
		quotas[q.Name] = q.Quota
	}

	if quotas["agent-a"] != quotas["agent-b"] || quotas["agent-b"] != quotas["agent-c"] {
		t.Fatalf("equal priority agents should get equal quotas: %d, %d, %d",
			quotas["agent-a"], quotas["agent-b"], quotas["agent-c"])
	}
	if quotas["agent-a"] <= 0 {
		t.Fatalf("quota should be positive, got %d", quotas["agent-a"])
	}
}

// --- 21.1-UNIT-004: [P0] RecordUsage accumulates (AC3) ---

func TestBudgetPool_RecordUsage_Accumulates(t *testing.T) {
	pool := NewBudgetPool(10000)
	pool.AllocateQuota(1, "agent", PriorityNormal)

	err := pool.RecordUsage(1, 3000)
	if err != nil {
		t.Fatalf("RecordUsage failed: %v", err)
	}

	status := pool.GetStatus()
	if status.Consumed != 3000 {
		t.Fatalf("expected consumed 3000, got %d", status.Consumed)
	}

	err = pool.RecordUsage(1, 2000)
	if err != nil {
		t.Fatalf("RecordUsage(2) failed: %v", err)
	}

	status = pool.GetStatus()
	if status.Consumed != 5000 {
		t.Fatalf("expected consumed 5000, got %d", status.Consumed)
	}
}

// --- 21.1-UNIT-005: [P0] IsExhausted boundary check (AC4) ---

func TestBudgetPool_IsExhausted_Boundary(t *testing.T) {
	pool := NewBudgetPool(5000)
	pool.AllocateQuota(1, "agent", PriorityNormal)

	if pool.IsExhausted() {
		t.Fatal("pool should not be exhausted before usage")
	}

	_ = pool.RecordUsage(1, 4999)
	if pool.IsExhausted() {
		t.Fatal("pool should not be exhausted at 4999/5000")
	}

	_ = pool.RecordUsage(1, 1)
	if !pool.IsExhausted() {
		t.Fatal("pool should be exhausted at 5000/5000 (>= check)")
	}

	_ = pool.RecordUsage(1, 100)
	if !pool.IsExhausted() {
		t.Fatal("pool should remain exhausted at 5100/5000")
	}
}

// --- 21.1-UNIT-006: [P1] Concurrent RecordUsage safety ---

func TestBudgetPool_ConcurrentRecordUsage(t *testing.T) {
	pool := NewBudgetPool(100000)
	pool.AllocateQuota(1, "agent-1", PriorityNormal)
	pool.AllocateQuota(2, "agent-2", PriorityNormal)

	var wg sync.WaitGroup
	for i := range 100 {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			pid := types.PID(1 + idx%2)
			_ = pool.RecordUsage(pid, 100)
		}(i)
	}
	wg.Wait()

	status := pool.GetStatus()
	if status.Consumed != 10000 {
		t.Fatalf("expected consumed 10000 (100*100), got %d", status.Consumed)
	}
}

// --- 21.1-UNIT-007: [P0] GetStatus complete snapshot (AC3) ---

func TestBudgetPool_GetStatus_Complete(t *testing.T) {
	pool := NewBudgetPool(20000)
	pool.AllocateQuota(1, "alpha", PriorityHigh)
	pool.AllocateQuota(2, "beta", PriorityLow)
	_ = pool.RecordUsage(1, 3000)

	status := pool.GetStatus()

	if status.TotalBudget != 20000 {
		t.Fatalf("expected TotalBudget 20000, got %d", status.TotalBudget)
	}
	if status.Consumed != 3000 {
		t.Fatalf("expected Consumed 3000, got %d", status.Consumed)
	}
	if status.Remaining != 17000 {
		t.Fatalf("expected Remaining 17000, got %d", status.Remaining)
	}
	if len(status.Quotas) != 2 {
		t.Fatalf("expected 2 quotas, got %d", len(status.Quotas))
	}
}

// --- 21.1-UNIT-008: [P1] Zero budget returns 0 quotas ---

func TestBudgetPool_ZeroBudget_ZeroQuota(t *testing.T) {
	pool := NewBudgetPool(0)
	quota := pool.AllocateQuota(1, "agent", PriorityHigh)
	if quota != 0 {
		t.Fatalf("expected 0 quota for zero budget, got %d", quota)
	}
}

// --- 21.1-UNIT-009: [P2] Negative budget treated as 0 ---

func TestBudgetPool_NegativeBudget_AsZero(t *testing.T) {
	pool := NewBudgetPool(-100)
	quota := pool.AllocateQuota(1, "agent", PriorityNormal)
	if quota != 0 {
		t.Fatalf("expected 0 quota for negative budget, got %d", quota)
	}
}

// --- 21.1-UNIT-010a: [P1] RecordUsage with unknown PID returns error ---

func TestBudgetPool_RecordUsage_UnknownPID_Error(t *testing.T) {
	pool := NewBudgetPool(10000)
	pool.AllocateQuota(1, "agent", PriorityNormal)

	err := pool.RecordUsage(999, 100)
	if err == nil {
		t.Fatal("expected error for unknown PID, got nil")
	}
}

// --- 21.1-UNIT-011a: [P1] Allocation performance NFR43 ---

func TestBudgetPool_AllocationPerformance_100ms(t *testing.T) {
	pool := NewBudgetPool(1000000)

	start := time.Now()
	for i := range 100 {
		pool.AllocateQuota(types.PID(i+1), "agent", PriorityNormal)
	}
	elapsed := time.Since(start)

	if elapsed > 100*time.Millisecond {
		t.Fatalf("NFR43 violation: allocation of 100 agents took %v (max 100ms)", elapsed)
	}
}
