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
