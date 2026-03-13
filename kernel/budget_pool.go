package kernel

import (
	"fmt"
	"strings"
	"sync"

	"github.com/rnixai/rnix/internal/types"
)

// Priority defines agent priority for budget allocation.
// Higher values receive proportionally more quota.
type Priority int

const (
	PriorityLow    Priority = 1
	PriorityNormal Priority = 5
	PriorityHigh   Priority = 10
)

// ParsePriority converts a priority string to a Priority value.
// Returns PriorityNormal for empty or unrecognized strings.
func ParsePriority(s string) Priority {
	switch strings.ToLower(s) {
	case "high":
		return PriorityHigh
	case "normal":
		return PriorityNormal
	case "low":
		return PriorityLow
	default:
		return PriorityNormal
	}
}

// AgentQuota records a single agent's budget quota and consumption.
type AgentQuota struct {
	PID      types.PID
	Name     string
	Priority Priority
	Quota    int // allocated quota
	Consumed int // tokens consumed so far
}

// BudgetPoolStatus is a point-in-time snapshot of the pool state.
type BudgetPoolStatus struct {
	TotalBudget int
	Allocated   int
	Consumed    int
	Remaining   int
	Quotas      []AgentQuota
}

// BudgetPool manages a Compose orchestration's total token budget.
// Thread-safe via sync.RWMutex.
type BudgetPool struct {
	mu          sync.RWMutex
	totalBudget int
	consumed    int
	quotas      map[types.PID]*AgentQuota
}

// NewBudgetPool creates a budget pool with the given total token budget.
func NewBudgetPool(totalBudget int) *BudgetPool {
	return &BudgetPool{
		totalBudget: totalBudget,
		quotas:      make(map[types.PID]*AgentQuota),
	}
}

// AllocateQuota registers an agent and recalculates proportional quotas
// for all registered agents. Returns the quota for this agent.
//
// The quota is calculated as: totalBudget * agentWeight / totalWeight
// where totalWeight is the sum of all registered agents' priorities.
// Each call recalculates ALL quotas to maintain proportional fairness.
func (bp *BudgetPool) AllocateQuota(pid types.PID, name string, priority Priority) int {
	bp.mu.Lock()
	defer bp.mu.Unlock()

	if bp.totalBudget <= 0 {
		bp.quotas[pid] = &AgentQuota{PID: pid, Name: name, Priority: priority, Quota: 0}
		return 0
	}

	bp.quotas[pid] = &AgentQuota{PID: pid, Name: name, Priority: priority}
	bp.recalcLocked()
	return bp.quotas[pid].Quota
}

// recalcLocked recalculates all quotas proportionally. Caller must hold bp.mu.
func (bp *BudgetPool) recalcLocked() {
	totalWeight := 0
	for _, q := range bp.quotas {
		totalWeight += int(q.Priority)
	}
	if totalWeight == 0 {
		return
	}

	allocated := 0
	for _, q := range bp.quotas {
		q.Quota = bp.totalBudget * int(q.Priority) / totalWeight
		allocated += q.Quota
	}
}

// GetQuota returns the current allocated quota for a specific agent.
func (bp *BudgetPool) GetQuota(pid types.PID) (int, bool) {
	bp.mu.RLock()
	defer bp.mu.RUnlock()
	q, ok := bp.quotas[pid]
	if !ok {
		return 0, false
	}
	return q.Quota, true
}

// RecordUsage records token consumption for an agent.
// Returns an error if the PID is not registered.
func (bp *BudgetPool) RecordUsage(pid types.PID, tokens int) error {
	bp.mu.Lock()
	defer bp.mu.Unlock()

	q, ok := bp.quotas[pid]
	if !ok {
		return fmt.Errorf("budget pool: unknown PID %d", pid)
	}
	q.Consumed += tokens
	bp.consumed += tokens
	return nil
}

// IsExhausted returns true when total consumption >= total budget.
// A zero-budget pool is always exhausted. A negative-budget pool is never exhausted.
func (bp *BudgetPool) IsExhausted() bool {
	bp.mu.RLock()
	defer bp.mu.RUnlock()
	if bp.totalBudget < 0 {
		return false
	}
	return bp.consumed >= bp.totalBudget
}

// Remaining returns the number of unspent tokens in the pool.
func (bp *BudgetPool) Remaining() int {
	bp.mu.RLock()
	defer bp.mu.RUnlock()
	r := bp.totalBudget - bp.consumed
	if r < 0 {
		return 0
	}
	return r
}

// GetStatus returns a point-in-time snapshot of the budget pool state.
func (bp *BudgetPool) GetStatus() BudgetPoolStatus {
	bp.mu.RLock()
	defer bp.mu.RUnlock()

	allocated := 0
	quotas := make([]AgentQuota, 0, len(bp.quotas))
	for _, q := range bp.quotas {
		allocated += q.Quota
		quotas = append(quotas, AgentQuota{
			PID:      q.PID,
			Name:     q.Name,
			Priority: q.Priority,
			Quota:    q.Quota,
			Consumed: q.Consumed,
		})
	}

	remaining := max(bp.totalBudget-bp.consumed, 0)

	return BudgetPoolStatus{
		TotalBudget: bp.totalBudget,
		Allocated:   allocated,
		Consumed:    bp.consumed,
		Remaining:   remaining,
		Quotas:      quotas,
	}
}
