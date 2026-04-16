package kernel

import (
	"testing"
	"time"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// --- BUG-004: Tool Error Propagation Tests ---

func TestReasonStep_ToolOpenFails_SetsHasToolError(t *testing.T) {
	// When LLM requests a tool whose VFS device Open fails,
	// the error is caught → HasToolError is set → exit code is 1.
	reg := vfs.NewDeviceRegistry()

	seqFile := &sequenceLLMFile{
		responses: [][]byte{
			// Step 1: LLM requests a tool_call to /dev/nonexistent (Open will fail)
			makeToolCallResponse("/dev/nonexistent", map[string]any{"query": "test"}, 50),
			// Step 2: LLM returns a text response (completing the reasoning)
			makeLLMResponse("I encountered a tool error but here is my best answer", 30),
		},
	}
	_ = reg.Register("/dev/llm/claude", func(subpath string, flags vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return seqFile, nil
	})
	// Register /dev/nonexistent with a factory that always fails on Open,
	// so the tool appears in toolMap but VFS Open returns an error.
	registerMockTool(reg, "/dev/nonexistent", func(subpath string, flags vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return nil, types.NewDriverError("Open", "/dev/nonexistent", nil, types.ErrInternal)
	})

	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	pid, err := k.Spawn("tool error test", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, _ := k.GetProcess(pid)
	select {
	case exit := <-proc.Done:
		if exit.Code != 1 {
			t.Fatalf("expected exit code 1 (HasToolError), got %d: reason=%q", exit.Code, exit.Reason)
		}
		if exit.Reason != "completed_with_tool_errors" {
			t.Fatalf("expected reason 'completed_with_tool_errors', got %q", exit.Reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for process to complete")
	}

	// Verify HasToolError flag is set
	proc.mu.Lock()
	hasErr := proc.HasToolError
	proc.mu.Unlock()
	if !hasErr {
		t.Error("expected HasToolError to be true")
	}

	// Verify process still produced a result (LLM continued reasoning)
	if proc.Result == "" {
		t.Error("expected non-empty result despite tool error")
	}
}

func TestReasonStep_ToolWriteFails_SetsHasToolError(t *testing.T) {
	// When a tool device is registered but Write fails,
	// the error is caught, FD is closed, and HasToolError is set.
	reg := vfs.NewDeviceRegistry()

	seqFile := &sequenceLLMFile{
		responses: [][]byte{
			makeToolCallResponse("/dev/tools/failing", map[string]any{"data": "test"}, 40),
			makeLLMResponse("recovered from write error", 20),
		},
	}
	_ = reg.Register("/dev/llm/claude", func(subpath string, flags vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return seqFile, nil
	})

	// Register a tool device that fails on Write
	registerMockTool(reg, "/dev/tools/failing", func(subpath string, flags vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &mockLLMFile{
			writeErr: errMockWrite,
		}, nil
	})

	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	pid, err := k.Spawn("tool write error test", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, _ := k.GetProcess(pid)
	select {
	case exit := <-proc.Done:
		if exit.Code != 1 {
			t.Fatalf("expected exit code 1 (HasToolError), got %d: reason=%q", exit.Code, exit.Reason)
		}
		if exit.Reason != "completed_with_tool_errors" {
			t.Fatalf("expected reason 'completed_with_tool_errors', got %q", exit.Reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	proc.mu.Lock()
	hasErr := proc.HasToolError
	proc.mu.Unlock()
	if !hasErr {
		t.Error("expected HasToolError to be true")
	}
}

func TestReasonStep_ToolReadFails_SetsHasToolError(t *testing.T) {
	// When a tool device Write succeeds but Read fails,
	// error is caught, FD is closed, and HasToolError is set.
	reg := vfs.NewDeviceRegistry()

	seqFile := &sequenceLLMFile{
		responses: [][]byte{
			makeToolCallResponse("/dev/tools/read-fail", map[string]any{"data": "test"}, 40),
			makeLLMResponse("recovered from read error", 20),
		},
	}
	_ = reg.Register("/dev/llm/claude", func(subpath string, flags vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return seqFile, nil
	})

	// Register a tool device that succeeds on Write but fails on Read
	registerMockTool(reg, "/dev/tools/read-fail", func(subpath string, flags vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &mockLLMFile{
			readErr: errMockRead,
		}, nil
	})

	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	pid, err := k.Spawn("tool read error test", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, _ := k.GetProcess(pid)
	select {
	case exit := <-proc.Done:
		if exit.Code != 1 {
			t.Fatalf("expected exit code 1 (HasToolError), got %d: reason=%q", exit.Code, exit.Reason)
		}
		if exit.Reason != "completed_with_tool_errors" {
			t.Fatalf("expected reason 'completed_with_tool_errors', got %q", exit.Reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	proc.mu.Lock()
	hasErr := proc.HasToolError
	proc.mu.Unlock()
	if !hasErr {
		t.Error("expected HasToolError to be true")
	}
}

func TestReasonStep_NoToolError_ExitCodeZero(t *testing.T) {
	// When all tool calls succeed, HasToolError remains false and exit code is 0.
	reg := vfs.NewDeviceRegistry()

	seqFile := &sequenceLLMFile{
		responses: [][]byte{
			makeToolCallResponse("/dev/tools/ok", map[string]any{"data": "test"}, 40),
			makeLLMResponse("all good", 20),
		},
	}
	_ = reg.Register("/dev/llm/claude", func(subpath string, flags vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return seqFile, nil
	})
	registerMockTool(reg, "/dev/tools/ok", func(subpath string, flags vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &mockToolFile{readData: []byte("tool result")}, nil
	})

	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	pid, err := k.Spawn("no tool error test", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, _ := k.GetProcess(pid)
	select {
	case exit := <-proc.Done:
		if exit.Code != 0 {
			t.Fatalf("expected exit code 0, got %d: reason=%q", exit.Code, exit.Reason)
		}
		if exit.Reason != "completed" {
			t.Fatalf("expected reason 'completed', got %q", exit.Reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	proc.mu.Lock()
	hasErr := proc.HasToolError
	proc.mu.Unlock()
	if hasErr {
		t.Error("expected HasToolError to be false when no tool errors occurred")
	}
}

// Sentinel errors for mock tool devices.
var (
	errMockWrite = types.NewDriverError("Write", "/dev/tools/failing", nil, types.ErrInternal)
	errMockRead  = types.NewDriverError("Read", "/dev/tools/read-fail", nil, types.ErrInternal)
)

func TestReasonStep_ToolErrorRecovered_ExitCodeZero(t *testing.T) {
	// When a tool call fails but the LLM recovers by making a subsequent
	// successful tool call, HasToolError should be cleared and exit code should be 0.
	reg := vfs.NewDeviceRegistry()

	seqFile := &sequenceLLMFile{
		responses: [][]byte{
			// Step 1: LLM requests a tool at a non-existent path (will fail → HasToolError = true)
			makeToolCallResponse("/dev/nonexistent", map[string]any{"query": "test"}, 50),
			// Step 2: LLM recovers by calling a working tool (should clear HasToolError)
			makeToolCallResponse("/dev/tools/ok", map[string]any{"data": "recovery"}, 40),
			// Step 3: LLM produces final text response
			makeLLMResponse("recovered successfully", 20),
		},
	}
	_ = reg.Register("/dev/llm/claude", func(subpath string, flags vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return seqFile, nil
	})
	// /dev/nonexistent is NOT registered → Open will fail
	// /dev/tools/ok IS registered → will succeed
	registerMockTool(reg, "/dev/tools/ok", func(subpath string, flags vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &mockToolFile{readData: []byte("tool result")}, nil
	})

	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	pid, err := k.Spawn("tool error recovery test", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, _ := k.GetProcess(pid)
	select {
	case exit := <-proc.Done:
		if exit.Code != 0 {
			t.Fatalf("expected exit code 0 (error recovered), got %d: reason=%q", exit.Code, exit.Reason)
		}
		if exit.Reason != "completed" {
			t.Fatalf("expected reason 'completed', got %q", exit.Reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for process to complete")
	}

	// Verify HasToolError was cleared by the successful recovery
	proc.mu.Lock()
	hasErr := proc.HasToolError
	proc.mu.Unlock()
	if hasErr {
		t.Error("expected HasToolError to be false after successful recovery")
	}

	// Verify process produced a result
	if proc.Result == "" {
		t.Error("expected non-empty result after recovery")
	}
}
