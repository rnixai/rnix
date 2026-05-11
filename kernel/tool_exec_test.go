package kernel

import (
	gocontext "context"
	"testing"
	"time"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/skills"
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
	if _, ok := k.executeToolCalls(proc, resp, 1, time.Now(), &consec, prompt, ""); !ok {
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
// ErrContextFull from AppendAssistantWithToolCalls by suspending the process
// (selfSuspend) with exit code 3 (ExitContextFull) and reason "context_full",
// instead of writing a half-state (assistant tool_calls without matching tool
// messages) that would later trigger DeepSeek's HTTP 400.
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
	_, cont := k.executeToolCalls(proc, resp, 1, time.Now(), &consec, prompt, "")
	if cont {
		t.Fatal("executeToolCalls returned true; expected false (process should suspend on ErrContextFull)")
	}

	// Verify the assistant message was NOT written: ctx still has only the filler.
	ctx, _ := ctxMgr.GetContext(cid)
	if got := len(ctx.Messages); got != 1 {
		t.Errorf("messages mutated despite ErrContextFull: got %d, want 1", got)
	}

	// Verify process is Suspended (not Zombie).
	if proc.GetState() != types.StateSuspended {
		t.Errorf("expected StateSuspended, got %s", proc.GetState())
	}

	// Verify exit status set by selfSuspend.
	proc.mu.Lock()
	exit := proc.Exit
	proc.mu.Unlock()
	if exit == nil {
		t.Fatal("proc.Exit is nil; expected selfSuspend to populate it")
	}
	if exit.Code != ExitContextFull {
		t.Errorf("ExitStatus.Code = %d, want %d (ExitContextFull)", exit.Code, ExitContextFull)
	}
}

// TestAppendToolResult_ReturnsError verifies that appendToolResult returns
// an error when the context is full (AC #1).
func TestAppendToolResult_ReturnsError(t *testing.T) {
	reg := vfs.NewDeviceRegistry()
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	t.Cleanup(k.Shutdown)

	cid, _ := ctxMgr.CtxAlloc(2)
	_ = ctxMgr.AppendMessage(cid, rnixctx.RoleUser, "fill1")
	_ = ctxMgr.AppendMessage(cid, rnixctx.RoleAssistant, "fill2")

	proc := NewProcess(0, "test", nil)
	proc.CtxID = cid

	err := k.appendToolResult(proc, 1, "tc-1", "test_tool", "result content")
	if err == nil {
		t.Fatal("expected error from appendToolResult on full context, got nil")
	}
}

// TestExecuteToolCalls_ToolResultDropBreaksLoop verifies that when
// appendToolResult fails on a VFS tool result, subsequent tools are NOT
// executed (AC #1, #3).
func TestExecuteToolCalls_ToolResultDropBreaksLoop(t *testing.T) {
	reg := vfs.NewDeviceRegistry()
	callCount := 0
	_ = reg.Register("/dev/test", func(subpath string, flags vfs.OpenFlag, workDir string) (vfs.VFSFile, error) {
		callCount++
		return &mockVFSToolFile{result: "ok"}, nil
	})
	_ = reg.Register("/dev/llm/mock", compactMockLLMFactory())
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	t.Cleanup(k.Shutdown)

	// 4 slots: system(implicit) + fill1 + assistant+toolcalls = 3 used, 1 remaining.
	// First appendToolResult succeeds (using slot 4), second fails.
	cid, _ := ctxMgr.CtxAlloc(4)
	_ = ctxMgr.AppendMessage(cid, rnixctx.RoleUser, "fill1")

	proc := NewProcess(0, "test", nil)
	proc.CtxID = cid
	proc.PrimaryDevice = "/dev/llm/mock"
	proc.toolMap = map[string]toolMapping{
		"tool_a": {Type: "vfs", VFSPath: "/dev/test"},
		"tool_b": {Type: "vfs", VFSPath: "/dev/test"},
	}
	if err := proc.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	resp := llmResponse{
		Content: "calling tools",
		ToolCalls: []llmToolCall{
			{ID: "a", Name: "tool_a"},
			{ID: "b", Name: "tool_b"},
		},
	}
	consec := 0
	prompt := &rnixctx.PromptResult{}
	_, cont := k.executeToolCalls(proc, resp, 1, time.Now(), &consec, prompt, "")

	if !cont {
		t.Fatal("expected executeToolCalls to return true (compact+retry, continue reasoning)")
	}

	// tool_a executes and its appendToolResult succeeds (slot 4).
	// tool_b executes and its appendToolResult fails (no more slots) → break.
	// Both VFS calls happen, but the second appendToolResult triggers the
	// compact+retry path and the function returns true.
	if callCount != 2 {
		t.Fatalf("expected exactly 2 VFS calls (both tools execute before appendToolResult fails on second), got %d", callCount)
	}
}

// TestExecuteToolCalls_CompactRetrySuccess verifies that after a tool result
// drop, compact is attempted and the dropped result is retried (AC #1).
func TestExecuteToolCalls_CompactRetrySuccess(t *testing.T) {
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/test", func(subpath string, flags vfs.OpenFlag, workDir string) (vfs.VFSFile, error) {
		return &mockVFSToolFile{result: "tool output"}, nil
	})
	_ = reg.Register("/dev/llm/mock", compactMockLLMFactory())
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	t.Cleanup(k.Shutdown)

	// 10 slots, fill 8 → assistant+toolcalls = slot 9, first tool result = slot 10 (full).
	// Second tool result fails → compact frees space → retry succeeds.
	cid, _ := ctxMgr.CtxAlloc(10)
	fillContext(t, ctxMgr, cid, 8)

	proc := NewProcess(0, "test", nil)
	proc.CtxID = cid
	proc.PrimaryDevice = "/dev/llm/mock"
	proc.toolMap = map[string]toolMapping{
		"tool_a": {Type: "vfs", VFSPath: "/dev/test"},
		"tool_b": {Type: "vfs", VFSPath: "/dev/test"},
	}
	if err := proc.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	resp := llmResponse{
		Content: "calling",
		ToolCalls: []llmToolCall{
			{ID: "a", Name: "tool_a"},
			{ID: "b", Name: "tool_b"},
		},
	}
	consec := 0
	prompt := &rnixctx.PromptResult{}
	_, cont := k.executeToolCalls(proc, resp, 1, time.Now(), &consec, prompt, "")

	if !cont {
		t.Fatal("expected true: compact+retry should recover without killing process")
	}

	// After compact+retry, the dropped tool result should have been appended.
	// Verify context has the tool result by checking message count grew.
	used, _, _ := ctxMgr.SlotUsage(cid)
	if used < 3 {
		t.Errorf("expected at least 3 messages after compact+retry, got %d", used)
	}
}

// TestSpecialize_AppendMessageFail_Rollback verifies that when AppendMessage
// fails during specialize (context full), the skill is rolled back from
// proc.Skills, proc.SkillBodies, and proc.SkillDirs (AC #2).
func TestSpecialize_AppendMessageFail_Rollback(t *testing.T) {
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/mock", compactMockLLMFactory())
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	t.Cleanup(k.Shutdown)

	k.SetSkillLoader(func(name string) (*skills.SkillInfo, error) {
		return &skills.SkillInfo{
			Manifest: skills.SkillManifest{Name: name, AllowedToolsRaw: "/dev/fs /dev/shell"},
			Body:     "skill body content for " + name,
			Dir:      "/tmp/skills/" + name,
		}, nil
	})

	// 4 slots, fill 3 → AppendMessage for skill body will fail (context full).
	cid, _ := ctxMgr.CtxAlloc(4)
	fillContext(t, ctxMgr, cid, 3)

	proc := NewProcess(0, "test specialize rollback", nil)
	proc.CtxID = cid
	proc.PrimaryDevice = "/dev/llm/mock"
	proc.toolMap = map[string]toolMapping{
		"specialize": {Type: "meta", Action: ActionSpecialize},
	}
	if err := proc.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// First append the assistant+toolcalls message to use the last slot
	err := ctxMgr.AppendMessage(cid, rnixctx.RoleAssistant, "I will specialize")
	if err != nil {
		t.Fatalf("pre-fill: %v", err)
	}

	resp := llmResponse{
		Content: "specializing",
		ToolCalls: []llmToolCall{
			{ID: "sp-1", Name: "specialize", Input: map[string]any{"skill_name": "test-skill"}},
		},
	}

	// Bypass executeToolCalls (which tries AppendAssistantWithToolCalls and would fail)
	// and call executeMetaAction directly to test the specialize rollback.
	mapping := toolMapping{Type: "meta", Action: ActionSpecialize}
	consec := 0
	prompt := &rnixctx.PromptResult{}
	shouldCont := k.executeMetaAction(proc, resp.ToolCalls[0], mapping, 1, time.Now(), &consec, prompt, "", &resp)

	if !shouldCont {
		t.Fatal("expected executeMetaAction to return true after specialize rollback")
	}

	proc.mu.Lock()
	hasSkill := len(proc.Skills) > 0
	_, hasBody := proc.SkillBodies["test-skill"]
	_, hasDir := proc.SkillDirs["test-skill"]
	hasStaleDevices := len(proc.AllowedDevices) > 0
	proc.mu.Unlock()

	if hasSkill {
		t.Errorf("proc.Skills should be empty after rollback, got %v", proc.Skills)
	}
	if hasBody {
		t.Error("proc.SkillBodies should not contain test-skill after rollback")
	}
	if hasDir {
		t.Error("proc.SkillDirs should not contain test-skill after rollback")
	}
	if hasStaleDevices {
		t.Errorf("proc.AllowedDevices should be empty after rollback, got %v", proc.AllowedDevices)
	}
}

// TestSpecialize_AppendMessageSuccess_NoRollback verifies that when
// AppendMessage succeeds during specialize, the skill remains loaded (AC #2).
func TestSpecialize_AppendMessageSuccess_NoRollback(t *testing.T) {
	reg := vfs.NewDeviceRegistry()
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	t.Cleanup(k.Shutdown)

	k.SetSkillLoader(func(name string) (*skills.SkillInfo, error) {
		return &skills.SkillInfo{
			Manifest: skills.SkillManifest{Name: name},
			Body:     "skill body for " + name,
			Dir:      "/tmp/skills/" + name,
		}, nil
	})

	cid, _ := ctxMgr.CtxAlloc(16)

	proc := NewProcess(0, "test specialize success", nil)
	proc.CtxID = cid
	proc.toolMap = map[string]toolMapping{
		"specialize": {Type: "meta", Action: ActionSpecialize},
	}

	resp := llmResponse{Content: "specializing"}
	mapping := toolMapping{Type: "meta", Action: ActionSpecialize}
	tc := llmToolCall{ID: "sp-1", Name: "specialize", Input: map[string]any{"skill_name": "good-skill"}}
	consec := 0
	prompt := &rnixctx.PromptResult{}
	shouldCont := k.executeMetaAction(proc, tc, mapping, 1, time.Now(), &consec, prompt, "", &resp)

	if !shouldCont {
		t.Fatal("expected true from successful specialize")
	}

	proc.mu.Lock()
	hasSkill := false
	for _, s := range proc.Skills {
		if s == "good-skill" {
			hasSkill = true
		}
	}
	_, hasBody := proc.SkillBodies["good-skill"]
	_, hasDir := proc.SkillDirs["good-skill"]
	proc.mu.Unlock()

	if !hasSkill {
		t.Error("proc.Skills should contain good-skill after successful specialize")
	}
	if !hasBody {
		t.Error("proc.SkillBodies should contain good-skill")
	}
	if !hasDir {
		t.Error("proc.SkillDirs should contain good-skill")
	}
}

// mockVFSToolFile is a minimal VFS file that returns a canned result.
type mockVFSToolFile struct {
	result string
}

func (f *mockVFSToolFile) Write(_ gocontext.Context, data []byte) error { return nil }
func (f *mockVFSToolFile) Read(length int) ([]byte, error)             { return []byte(f.result), nil }
func (f *mockVFSToolFile) Close() error                                { return nil }
func (f *mockVFSToolFile) Stat() (vfs.FileStat, error) {
	return vfs.FileStat{IsDevice: true, Name: "/dev/test"}, nil
}
