package kernel

import (
	"testing"
	"time"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// TestReasonStep_MaxStepsZero_CompleteTerminates verifies that with maxSteps=0,
// the loop continues until a complete action (not limited by step count).
func TestReasonStep_MaxStepsZero_CompleteTerminates(t *testing.T) {
	// Use tool_call responses (non-terminating) followed by complete
	responses := [][]byte{
		makeToolCallResponse("/dev/fs", map[string]any{"path": "/tmp/a"}, 10),
		makeToolCallResponse("/dev/fs", map[string]any{"path": "/tmp/b"}, 10),
		makeCompleteResponse("done", 10),
	}
	llm := &sequenceLLMFile{responses: responses}
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return llm, nil
	})
	// Register /dev/fs so tool calls don't fail on missing device
	_ = reg.Register("/dev/fs", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &mockLLMFile{readData: []byte("file content")}, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()

	var lastStep int
	cb := &s301Callbacks{
		onStep: func(_ types.PID, step, _ int) { lastStep = step },
	}
	k := NewKernel(v, ctxMgr, cb)
	defer k.Shutdown()

	pid, err := k.Spawn("test infinite", nil, SpawnOpts{Model: "test"})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	proc, _ := k.GetProcess(pid)
	select {
	case exit := <-proc.Done:
		if exit.Code != 0 {
			t.Errorf("expected exit code 0, got %d (reason: %s)", exit.Code, exit.Reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout: process did not complete")
	}

	if lastStep < 3 {
		t.Errorf("expected at least 3 steps, got %d", lastStep)
	}
}

// TestReasonStep_MaxStepsPositive_Exceeds verifies that maxSteps>0 terminates with "max steps exceeded".
func TestReasonStep_MaxStepsPositive_Exceeds(t *testing.T) {
	// Use different tool_call responses each step to avoid loop detection
	step := 0
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		step++
		return &mockLLMFile{
			readData: makeToolCallResponse("/dev/fs", map[string]any{"path": "/tmp/step", "n": step}, 1),
		}, nil
	})
	_ = reg.Register("/dev/fs", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &mockLLMFile{readData: []byte("ok")}, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()

	var lastStep int
	cb := &s301Callbacks{
		onStep: func(_ types.PID, step, _ int) { lastStep = step },
	}
	k := NewKernel(v, ctxMgr, cb)
	defer k.Shutdown()

	pid, err := k.Spawn("test max steps", nil, SpawnOpts{Model: "test", MaxTurns: 5})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	proc, _ := k.GetProcess(pid)
	select {
	case exit := <-proc.Done:
		if exit.Reason != "max steps exceeded" {
			t.Errorf("expected 'max steps exceeded', got %q", exit.Reason)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}

	if lastStep != 5 {
		t.Errorf("expected 5 steps, got %d", lastStep)
	}
}

// s301Callbacks is a minimal KernelCallbacks for Story 30.1 tests.
type s301Callbacks struct {
	onStep func(pid types.PID, step int, total int)
}

func (c *s301Callbacks) OnSpawn(_ types.PID, _, _, _, _ string)                                    {}
func (c *s301Callbacks) OnStep(pid types.PID, step, total int) {
	if c.onStep != nil {
		c.onStep(pid, step, total)
	}
}
func (c *s301Callbacks) OnStepComplete(_ types.PID, _ int, _ string, _ string, _ bool, _ float64) {}
func (c *s301Callbacks) OnComplete(_ types.PID, _ string, _ ExitStatus)                            {}
func (c *s301Callbacks) OnError(_ types.PID, _ error)                                              {}
func (c *s301Callbacks) OnAskUser(_ types.PID, _ string, _ []byte) ([]byte, error)                 { return nil, nil }
