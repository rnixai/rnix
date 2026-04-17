package kernel

import (
	"encoding/json"
	"testing"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/vfs"
)

// makeLLMResponseMaxTurns builds an LLM response carrying stop_reason=max_turns,
// matching what claude_cli.go now surfaces to the kernel.
func makeLLMResponseMaxTurns(tokens int) []byte {
	resp := llmResponse{TokensUsed: tokens, StopReason: "max_turns"}
	data, _ := json.Marshal(resp)
	return data
}

// TestProcessBudget_PrefersDriverReportedCost verifies R4 integration: when
// resp.CostUSD > 0, the kernel uses it directly and does NOT call costPerToken.
// A driver reporting $0.15/step should trip a $0.25 budget after the 2nd step,
// even though costPerToken returns 0.
func TestProcessBudget_PrefersDriverReportedCost(t *testing.T) {
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &sequenceLLMFile{
			responses: [][]byte{
				// Step 1: cost $0.15 via driver
				makeToolCallResponseWithCost("/dev/tools/echo", map[string]any{}, 1000, 0.15),
				// Step 2: cost $0.15 via driver → total $0.30 > $0.25 → suspend
				makeToolCallResponseWithCost("/dev/tools/echo", map[string]any{}, 1000, 0.15),
				makeLLMResponse("should not reach", 100),
			},
		}, nil
	})
	registerMockTool(reg, "/dev/tools/echo", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &mockToolFile{readData: []byte("ok")}, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	costPerTokenCalled := false
	k.SetCostPerToken(func(_ string) float64 {
		costPerTokenCalled = true
		return 0 // intentionally 0: if driver reports cost, this should never be consulted
	})

	pid, err := k.Spawn("cost-usd driver budget test", nil, SpawnOpts{MaxCost: 0.25})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	exit, err := k.Wait(pid)
	if err != nil {
		t.Fatalf("Wait failed: %v", err)
	}

	if exit.Code != 2 {
		t.Errorf("expected exit code 2 (suspended on budget), got %d", exit.Code)
	}
	if exit.Reason != "suspended: budget_exhausted" {
		t.Errorf("expected reason 'suspended: budget_exhausted', got %q", exit.Reason)
	}
	if costPerTokenCalled {
		t.Error("costPerToken should NOT be called when driver reports CostUSD > 0")
	}

	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatal("process not found")
	}
	// UsedCost should be ~$0.30 (2 steps × $0.15), not 0
	if got := proc.Budget.UsedCost; got < 0.29 || got > 0.31 {
		t.Errorf("UsedCost = %f, want ~0.30 (driver-reported sum)", got)
	}
}

// TestReason_MaxTurns_Reached verifies R2 integration: a response carrying
// StopReason=max_turns exits with reason="max_turns_reached" instead of
// being misclassified as empty_llm_response.
func TestReason_MaxTurns_Reached(t *testing.T) {
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &sequenceLLMFile{
			responses: [][]byte{
				makeLLMResponseMaxTurns(500),
			},
		}, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	pid, err := k.Spawn("max turns test", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	exit, err := k.Wait(pid)
	if err != nil {
		t.Fatalf("Wait failed: %v", err)
	}

	if exit.Code != 2 {
		t.Errorf("expected exit code 2, got %d (reason=%q)", exit.Code, exit.Reason)
	}
	if exit.Reason != "max_turns_reached" {
		t.Errorf("expected reason 'max_turns_reached', got %q", exit.Reason)
	}
}

// makeToolCallResponseWithCost mirrors makeToolCallResponse but attaches a
// driver-reported CostUSD value.
func makeToolCallResponseWithCost(toolName string, toolInput map[string]any, tokens int, costUSD float64) []byte {
	resp := llmResponse{
		TokensUsed: tokens,
		CostUSD:    costUSD,
		ToolCalls: []llmToolCall{{
			ID:    "call_" + toolName,
			Name:  toolName,
			Input: toolInput,
		}},
	}
	data, _ := json.Marshal(resp)
	return data
}
