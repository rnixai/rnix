package kernel

import (
	"testing"

	"github.com/rnixai/rnix/internal/types"
)

// ============================================================
// ATDD RED PHASE — Story 21.2: 合约 SLA 与评估
//
// Kernel 级 SLA 结果注册/查询测试 + IPC SLA 查询测试。
// 测试引用 KernelImpl.RecordSLAResult, KernelImpl.GetSLAResults 等方法，
// 这些方法尚不存在。测试将无法编译直到实现完成。
//
// RED → GREEN: 实现 kernel.go 中的 slaResults SyncMap 和相关方法。
// ============================================================

// --- 21.2-KINT-001: [P0] Kernel RecordSLAResult and GetSLAResults (AC4) ---

func TestKernel_RecordSLAResult(t *testing.T) {
	k := newSimpleKernel(t)

	groupID := types.PGID(300)
	result := &SLAResult{
		AgentName:  "test-agent",
		Passed:     true,
		Checks:     []SLACheckResult{{Name: "max_tokens", Passed: true, Actual: "5000", Limit: "10000"}},
		TokensUsed: 5000,
		DurationMs: 30000,
	}

	// When: recording SLA result
	k.RecordSLAResult(groupID, result)

	// Then: can retrieve it
	results, err := k.GetSLAResults(groupID)
	if err != nil {
		t.Fatalf("GetSLAResults failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 SLA result, got %d", len(results))
	}
	if results[0].AgentName != "test-agent" {
		t.Fatalf("expected agent name 'test-agent', got %q", results[0].AgentName)
	}
	if !results[0].Passed {
		t.Fatal("expected SLA result to pass")
	}
}

// --- 21.2-KINT-002: [P0] Kernel GetSLAResults returns error for unknown group (AC4) ---

func TestKernel_GetSLAResults_NoGroup(t *testing.T) {
	k := newSimpleKernel(t)

	// When: querying SLA results for non-existent group
	results, err := k.GetSLAResults(types.PGID(999))

	// Then: returns error
	if err == nil {
		t.Fatal("expected error for non-existent group SLA results")
	}
	if results != nil {
		t.Fatalf("expected nil results for non-existent group, got %v", results)
	}
}

// --- 21.2-KINT-003: [P1] Kernel RecordSLAResult multiple results for same group (AC4) ---

func TestKernel_RecordSLAResult_Multiple(t *testing.T) {
	k := newSimpleKernel(t)

	groupID := types.PGID(400)

	// Record two SLA results for the same group
	r1 := &SLAResult{AgentName: "agent-1", Passed: true, TokensUsed: 3000, DurationMs: 10000}
	r2 := &SLAResult{AgentName: "agent-2", Passed: false, TokensUsed: 20000, DurationMs: 90000}

	k.RecordSLAResult(groupID, r1)
	k.RecordSLAResult(groupID, r2)

	results, err := k.GetSLAResults(groupID)
	if err != nil {
		t.Fatalf("GetSLAResults failed: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 SLA results, got %d", len(results))
	}

	// Verify both agents are present
	names := make(map[string]bool)
	for _, r := range results {
		names[r.AgentName] = true
	}
	if !names["agent-1"] || !names["agent-2"] {
		t.Fatalf("expected both agent-1 and agent-2, got %v", names)
	}
}
