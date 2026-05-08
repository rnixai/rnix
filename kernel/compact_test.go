package kernel

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// compactMockLLMFactory creates a VFSFileFactory that returns a mock LLM file
// producing a canned compact summary response.
func compactMockLLMFactory() vfs.VFSFileFactory {
	return func(subpath string, flags vfs.OpenFlag, workDir string) (vfs.VFSFile, error) {
		resp, _ := json.Marshal(llmResponse{Content: "<summary>\nCompact summary.\n</summary>"})
		return &mockLLMFile{readData: resp}, nil
	}
}

func setupCompactKernel(t *testing.T, maxSize int) (*KernelImpl, *rnixctx.Manager, *Process, types.CtxID) {
	t.Helper()
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/mock", compactMockLLMFactory())
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	t.Cleanup(k.Shutdown)

	cid, err := ctxMgr.CtxAlloc(maxSize)
	if err != nil {
		t.Fatalf("CtxAlloc: %v", err)
	}

	proc := NewProcess(0, "compact test", nil)
	proc.CtxID = cid
	proc.PrimaryDevice = "/dev/llm/mock"
	proc.toolMap = map[string]toolMapping{}

	return k, ctxMgr, proc, cid
}

func fillContext(t *testing.T, ctxMgr *rnixctx.Manager, cid types.CtxID, n int) {
	t.Helper()
	for i := range n {
		role := rnixctx.RoleUser
		if i%2 == 1 {
			role = rnixctx.RoleAssistant
		}
		if err := ctxMgr.AppendMessage(cid, role, "x"); err != nil {
			t.Fatalf("AppendMessage %d: %v", i, err)
		}
	}
}

func TestAutoCompactIfNeeded_SlotThresholdTrigger(t *testing.T) {
	// MaxSize=10, fill 9 messages (90% slots) with minimal token content.
	// Token% well below 80%, but slot% > 80% → should trigger compact.
	k, ctxMgr, proc, cid := setupCompactKernel(t, 10)
	fillContext(t, ctxMgr, cid, 9)

	used, max, _ := ctxMgr.SlotUsage(cid)
	if used != 9 || max != 10 {
		t.Fatalf("precondition: used=%d max=%d, want 9/10", used, max)
	}
	usage, _ := ctxMgr.TokenUsage(cid)
	if usage.Percentage > 80.0 {
		t.Fatalf("precondition: token%% %.1f > 80, expected low", usage.Percentage)
	}

	k.autoCompactIfNeeded(proc, 1)

	usedAfter, _, _ := ctxMgr.SlotUsage(cid)
	if usedAfter >= 9 {
		t.Errorf("slot-triggered compact did not reduce messages: still %d", usedAfter)
	}
}

func TestAutoCompactIfNeeded_BothBelowThreshold_NoCompact(t *testing.T) {
	// MaxSize=100, fill 5 messages (5% slots), minimal tokens → no compact.
	k, ctxMgr, proc, cid := setupCompactKernel(t, 100)
	fillContext(t, ctxMgr, cid, 5)

	k.autoCompactIfNeeded(proc, 1)

	used, _, _ := ctxMgr.SlotUsage(cid)
	if used != 5 {
		t.Errorf("expected 5 messages unchanged, got %d", used)
	}
}

func TestAutoCompactIfNeeded_TokenThresholdStillWorks(t *testing.T) {
	// Regression: token% threshold still triggers compact.
	k, ctxMgr, proc, cid := setupCompactKernel(t, 1000)

	largeContent := strings.Repeat("word ", 5000)
	for i := range 4 {
		role := rnixctx.RoleUser
		if i%2 == 1 {
			role = rnixctx.RoleAssistant
		}
		_ = ctxMgr.AppendMessage(cid, role, largeContent)
	}

	usage, _ := ctxMgr.TokenUsage(cid)
	if usage.Percentage <= 80.0 {
		t.Skipf("token%% %.1f not above threshold, cannot test token trigger", usage.Percentage)
	}

	preUsed, _, _ := ctxMgr.SlotUsage(cid)
	k.autoCompactIfNeeded(proc, 1)
	postUsed, _, _ := ctxMgr.SlotUsage(cid)

	if postUsed >= preUsed {
		t.Errorf("token-triggered compact didn't reduce messages: %d → %d", preUsed, postUsed)
	}
}

func TestPreCompactForToolCalls_SlotsSufficient(t *testing.T) {
	k, ctxMgr, proc, cid := setupCompactKernel(t, 10)
	fillContext(t, ctxMgr, cid, 3) // 7 available, need 1+2=3

	err := k.preCompactForToolCalls(proc, 2, 1)
	if err != nil {
		t.Fatalf("expected nil, got: %v", err)
	}

	used, _, _ := ctxMgr.SlotUsage(cid)
	if used != 3 {
		t.Errorf("messages changed unexpectedly: %d", used)
	}
}

func TestPreCompactForToolCalls_SlotsInsufficient_CompactSucceeds(t *testing.T) {
	k, ctxMgr, proc, cid := setupCompactKernel(t, 10)
	fillContext(t, ctxMgr, cid, 9) // 1 available, need 1+2=3

	err := k.preCompactForToolCalls(proc, 2, 1)
	if err != nil {
		t.Fatalf("precompact should succeed: %v", err)
	}

	used, _, _ := ctxMgr.SlotUsage(cid)
	if used >= 9 {
		t.Errorf("compact didn't reduce messages: %d", used)
	}
}

func TestPreCompactForToolCalls_TryLockFails_Skips(t *testing.T) {
	k, ctxMgr, proc, cid := setupCompactKernel(t, 10)
	fillContext(t, ctxMgr, cid, 9)

	proc.compactMu.Lock()
	err := k.preCompactForToolCalls(proc, 2, 1)
	proc.compactMu.Unlock()

	if err != nil {
		t.Fatalf("expected nil (skip on lock contention), got: %v", err)
	}

	used, _, _ := ctxMgr.SlotUsage(cid)
	if used != 9 {
		t.Errorf("messages should be unchanged: got %d, want 9", used)
	}
}

func TestPreCompactForToolCalls_CompactInsufficientPostCompact(t *testing.T) {
	// MaxSize=3, fill 3 messages. Compact shrinks to ~2 (boundary+summary).
	// Need 1+2=3 slots → post-compact still insufficient.
	k, ctxMgr, proc, cid := setupCompactKernel(t, 3)
	fillContext(t, ctxMgr, cid, 3)

	err := k.preCompactForToolCalls(proc, 2, 1)
	if err == nil {
		// If compact somehow freed enough space, that's also fine.
		avail, _ := ctxMgr.AvailableSlots(cid)
		if avail < 3 {
			t.Fatal("expected error for insufficient post-compact space, got nil")
		}
	}
}

func TestExecuteToolCalls_PrecompactSavesFromContextFull(t *testing.T) {
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/mock", compactMockLLMFactory())
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	t.Cleanup(k.Shutdown)

	cid, _ := ctxMgr.CtxAlloc(10)
	fillContext(t, ctxMgr, cid, 9)

	proc := NewProcess(0, "test", nil)
	proc.CtxID = cid
	proc.PrimaryDevice = "/dev/llm/mock"
	proc.toolMap = map[string]toolMapping{}
	if err := proc.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	resp := llmResponse{
		Content:   "calling tool",
		ToolCalls: []llmToolCall{{ID: "a", Name: "noop"}},
	}
	consec := 0
	prompt := &rnixctx.PromptResult{}
	cont := k.executeToolCalls(proc, resp, 1, time.Now(), &consec, prompt, "")

	if !cont {
		proc.mu.Lock()
		exit := proc.Exit
		proc.mu.Unlock()
		if exit != nil && exit.Reason == "context_full" {
			t.Fatal("process terminated with context_full despite precompact being available")
		}
	}
}

func TestExecuteToolCalls_ContextFullAfterPrecompact(t *testing.T) {
	// MaxSize=2, prefill 1 message. Attempt append with 2 tool calls (needs 3 slots).
	// Precompact cannot help (too small). Process should still terminate properly.
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/mock", compactMockLLMFactory())
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	t.Cleanup(k.Shutdown)

	cid, _ := ctxMgr.CtxAlloc(2)
	_ = ctxMgr.AppendMessage(cid, rnixctx.RoleUser, "filler")

	proc := NewProcess(0, "test", nil)
	proc.CtxID = cid
	proc.PrimaryDevice = "/dev/llm/mock"
	proc.toolMap = map[string]toolMapping{}
	if err := proc.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	resp := llmResponse{
		Content:   "calling",
		ToolCalls: []llmToolCall{{ID: "a", Name: "noop"}, {ID: "b", Name: "noop"}},
	}
	consec := 0
	prompt := &rnixctx.PromptResult{}
	cont := k.executeToolCalls(proc, resp, 1, time.Now(), &consec, prompt, "")

	if cont {
		t.Fatal("expected false (process should terminate on ErrContextFull)")
	}

	proc.mu.Lock()
	exit := proc.Exit
	proc.mu.Unlock()
	if exit == nil {
		t.Fatal("proc.Exit is nil")
	}
	if exit.Reason != "context_full" {
		t.Errorf("exit reason = %q, want context_full", exit.Reason)
	}
	if exit.Code != 2 {
		t.Errorf("exit code = %d, want 2", exit.Code)
	}
}
