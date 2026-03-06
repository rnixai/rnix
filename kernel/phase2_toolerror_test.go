package kernel

import (
	"testing"
	"time"

	cruxctx "github.com/usecrux/crux/context"
	"github.com/usecrux/crux/internal/types"
	"github.com/usecrux/crux/vfs"
)

// --- BUG-004: Tool Error Propagation Tests ---

func TestReasonStep_ToolOpenFails_SetsHasToolError(t *testing.T) {
	// When LLM requests a tool at a non-existent device path,
	// VFS Open fails → HasToolError is set → exit code is 1.
	reg := vfs.NewDeviceRegistry()

	seqFile := &sequenceLLMFile{
		responses: [][]byte{
			// Step 1: LLM requests a tool_call to /dev/nonexistent (not registered)
			makeToolCallResponse("/dev/nonexistent", map[string]any{"query": "test"}, 50),
			// Step 2: LLM returns a text response (completing the reasoning)
			makeLLMResponse("I encountered a tool error but here is my best answer", 30),
		},
	}
	_ = reg.Register("/dev/llm/claude", func(subpath string, flags vfs.OpenFlag) (vfs.VFSFile, error) {
		return seqFile, nil
	})
	// NOTE: /dev/nonexistent is intentionally NOT registered → Open will fail

	v := vfs.NewVFS(reg)
	ctxMgr := cruxctx.NewManager()
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
	_ = reg.Register("/dev/llm/claude", func(subpath string, flags vfs.OpenFlag) (vfs.VFSFile, error) {
		return seqFile, nil
	})

	// Register a tool device that fails on Write
	_ = reg.Register("/dev/tools/failing", func(subpath string, flags vfs.OpenFlag) (vfs.VFSFile, error) {
		return &mockLLMFile{
			writeErr: errMockWrite,
		}, nil
	})

	v := vfs.NewVFS(reg)
	ctxMgr := cruxctx.NewManager()
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
	_ = reg.Register("/dev/llm/claude", func(subpath string, flags vfs.OpenFlag) (vfs.VFSFile, error) {
		return seqFile, nil
	})

	// Register a tool device that succeeds on Write but fails on Read
	_ = reg.Register("/dev/tools/read-fail", func(subpath string, flags vfs.OpenFlag) (vfs.VFSFile, error) {
		return &mockLLMFile{
			readErr: errMockRead,
		}, nil
	})

	v := vfs.NewVFS(reg)
	ctxMgr := cruxctx.NewManager()
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
	_ = reg.Register("/dev/llm/claude", func(subpath string, flags vfs.OpenFlag) (vfs.VFSFile, error) {
		return seqFile, nil
	})
	_ = reg.Register("/dev/tools/ok", func(subpath string, flags vfs.OpenFlag) (vfs.VFSFile, error) {
		return &mockToolFile{readData: []byte("tool result")}, nil
	})

	v := vfs.NewVFS(reg)
	ctxMgr := cruxctx.NewManager()
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
