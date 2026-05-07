package kernel

import (
	"testing"
	"time"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/vfs"
)

// TestExecuteToolCalls_PreservesReasoning verifies the kernel layer transparently
// forwards llmResponse.Reasoning into context.Message.Reasoning via
// AppendAssistantWithToolCalls. Without this, the next reasonStep would build a
// prompt missing reasoning_content and DeepSeek thinking-mode would 400.
func TestExecuteToolCalls_PreservesReasoning(t *testing.T) {
	reg := vfs.NewDeviceRegistry()
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	t.Cleanup(k.Shutdown)

	cid, err := ctxMgr.CtxAlloc(16)
	if err != nil {
		t.Fatalf("CtxAlloc: %v", err)
	}
	proc := NewProcess(0, "test intent", nil)
	proc.CtxID = cid
	proc.toolMap = map[string]toolMapping{}

	resp := llmResponse{
		Content:   "I will think and then act",
		Reasoning: "step1: examine the directory before listing",
		ToolCalls: nil,
	}

	consec := 0
	prompt := &rnixctx.PromptResult{}
	if !k.executeToolCalls(proc, resp, 1, time.Now(), &consec, prompt, "") {
		t.Fatalf("executeToolCalls returned false; expected continuation for empty tool_calls")
	}

	pr, err := ctxMgr.BuildPrompt(cid)
	if err != nil {
		t.Fatalf("BuildPrompt: %v", err)
	}
	if len(pr.Messages) != 1 {
		t.Fatalf("len(messages) = %d, want 1", len(pr.Messages))
	}
	got := pr.Messages[0]
	if got.Role != rnixctx.RoleAssistant {
		t.Fatalf("role = %q, want assistant", got.Role)
	}
	if got.Reasoning != resp.Reasoning {
		t.Fatalf("reasoning dropped on kernel→context boundary: got %q, want %q", got.Reasoning, resp.Reasoning)
	}
}

// TestExecuteToolCalls_ContextFullExitsCleanly verifies the kernel handles
// ErrContextFull from AppendAssistantWithToolCalls by terminating the process
// with exit code 2 and reason "context_full" instead of writing a half-state
// (assistant tool_calls without matching tool messages) that would later
// trigger DeepSeek's HTTP 400 "insufficient tool messages following tool_calls".
func TestExecuteToolCalls_ContextFullExitsCleanly(t *testing.T) {
	reg := vfs.NewDeviceRegistry()
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	t.Cleanup(k.Shutdown)

	// Tiny capacity: 2 messages. Pre-fill 1, then attempt assistant + 2 tool_calls
	// (needs 3 slots) → ErrContextFull.
	cid, _ := ctxMgr.CtxAlloc(2)
	_ = ctxMgr.AppendMessage(cid, rnixctx.RoleUser, "filler")

	proc := NewProcess(0, "test", nil)
	proc.CtxID = cid
	proc.toolMap = map[string]toolMapping{}
	if err := proc.Start(); err != nil {
		t.Fatalf("proc.Start: %v", err)
	}

	resp := llmResponse{
		Content: "calling",
		ToolCalls: []llmToolCall{
			{ID: "a", Name: "noop"},
			{ID: "b", Name: "noop"},
		},
	}
	consec := 0
	prompt := &rnixctx.PromptResult{}
	cont := k.executeToolCalls(proc, resp, 1, time.Now(), &consec, prompt, "")
	if cont {
		t.Fatal("executeToolCalls returned true; expected false (process should terminate on ErrContextFull)")
	}

	// Verify the assistant message was NOT written: ctx still has only the filler.
	ctx, _ := ctxMgr.GetContext(cid)
	if got := len(ctx.Messages); got != 1 {
		t.Errorf("messages mutated despite ErrContextFull: got %d, want 1", got)
	}

	// Verify exit reason via Process.Exit (set by finishProcess).
	proc.mu.Lock()
	exit := proc.Exit
	proc.mu.Unlock()
	if exit == nil {
		t.Fatal("proc.Exit is nil; expected finishProcess to populate it")
	}
	if exit.Reason != "context_full" {
		t.Errorf("ExitStatus.Reason = %q, want %q", exit.Reason, "context_full")
	}
	if exit.Code != 2 {
		t.Errorf("ExitStatus.Code = %d, want 2", exit.Code)
	}
}
