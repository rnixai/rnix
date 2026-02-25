package kernel

import (
	"errors"
	"testing"
	"time"

	cruxctx "github.com/gonewx/crux/context"
	"github.com/gonewx/crux/internal/types"
	"github.com/gonewx/crux/vfs"
)

// --- Story 4.1: Wait syscall tests ---

func TestWait_NormalCompletion(t *testing.T) {
	// Spawn a process that completes normally, then Wait for it
	llmFile := &mockLLMFile{
		readData: makeLLMResponse("wait result", 10),
	}
	k, _, ctxMgr := newTestKernel(llmFile)

	pid, err := k.Spawn("wait test", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	// Save ctxID before Wait removes process from table
	proc, _ := k.GetProcess(pid)
	ctxID := proc.CtxID

	// Wait should block until completion and return ExitStatus
	exit, err := k.Wait(pid)
	if err != nil {
		t.Fatalf("Wait failed: %v", err)
	}
	if exit.Code != 0 {
		t.Errorf("expected exit code 0, got %d: %s", exit.Code, exit.Reason)
	}

	// Verify resource release: process removed from table
	_, ok := k.GetProcess(pid)
	if ok {
		t.Error("process should be removed from table after Wait")
	}

	// Verify context was freed
	_, ctxErr := ctxMgr.BuildPrompt(ctxID)
	if ctxErr == nil {
		t.Error("context should be freed after Wait")
	}
}

func TestWait_KillThenWait(t *testing.T) {
	// Full lifecycle: Spawn → Kill → Wait → verify cleanup
	blockCh := make(chan struct{})
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(subpath string, flags vfs.OpenFlag) (vfs.VFSFile, error) {
		return &blockingLLMFile{blockCh: blockCh}, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := cruxctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)

	pid, err := k.Spawn("kill-wait test", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, _ := k.GetProcess(pid)
	ctxID := proc.CtxID

	// Let goroutine start
	time.Sleep(50 * time.Millisecond)

	// Kill
	if err := k.Kill(pid, types.SIGTERM); err != nil {
		t.Fatalf("Kill failed: %v", err)
	}

	// Unblock LLM
	close(blockCh)

	// Wait for full cleanup
	exit, err := k.Wait(pid)
	if err != nil {
		t.Fatalf("Wait failed: %v", err)
	}

	// Exit code should be non-zero (cancelled)
	t.Logf("exit code: %d, reason: %s", exit.Code, exit.Reason)

	// Verify process removed from table
	_, ok := k.GetProcess(pid)
	if ok {
		t.Error("process should be removed from table after Wait")
	}

	// Verify context freed
	_, ctxErr := ctxMgr.BuildPrompt(ctxID)
	if ctxErr == nil {
		t.Error("context should be freed after Wait")
	}
}

func TestWait_PIDNotFound(t *testing.T) {
	k := newSimpleKernel()

	_, err := k.Wait(9999)
	if err == nil {
		t.Fatal("expected error for non-existent PID")
	}

	var syscallErr *SyscallError
	if !errors.As(err, &syscallErr) {
		t.Fatalf("expected *SyscallError, got %T", err)
	}
	if syscallErr.Code != types.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %s", syscallErr.Code)
	}
	if syscallErr.Syscall != "Wait" {
		t.Errorf("expected syscall 'Wait', got %q", syscallErr.Syscall)
	}
}

func TestWait_ResourceRelease(t *testing.T) {
	// Verify the complete resource release sequence
	llmFile := &mockLLMFile{
		readData: makeLLMResponse("release test", 5),
	}
	k, _, ctxMgr := newTestKernel(llmFile)

	pid, err := k.Spawn("resource test", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, _ := k.GetProcess(pid)
	ctxID := proc.CtxID
	debugCh := proc.DebugChan // save ref before Wait nils it

	// Wait for full completion + cleanup
	exit, err := k.Wait(pid)
	if err != nil {
		t.Fatalf("Wait failed: %v", err)
	}
	if exit.Code != 0 {
		t.Errorf("expected exit code 0, got %d", exit.Code)
	}

	// 1. Process state should be Dead
	if proc.GetState() != types.StateDead {
		t.Errorf("expected Dead state, got %d", proc.GetState())
	}

	// 2. Process removed from table
	_, ok := k.GetProcess(pid)
	if ok {
		t.Error("process should be removed from table")
	}

	// 3. Context freed
	_, ctxErr := ctxMgr.BuildPrompt(ctxID)
	if ctxErr == nil {
		t.Error("context should be freed")
	}

	// 4. DebugChan closed — drain buffered events then verify closed
	for {
		select {
		case _, open := <-debugCh:
			if !open {
				goto debugChClosed
			}
		case <-time.After(2 * time.Second):
			t.Fatal("timed out draining DebugChan — channel may not be closed")
		}
	}
debugChClosed:
}

func TestWait_ConcurrentSafe(t *testing.T) {
	// Verify Wait doesn't race with Kill
	blockCh := make(chan struct{})
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(subpath string, flags vfs.OpenFlag) (vfs.VFSFile, error) {
		return &blockingLLMFile{blockCh: blockCh}, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := cruxctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)

	pid, err := k.Spawn("concurrent test", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	// Let goroutine start
	time.Sleep(50 * time.Millisecond)

	// Kill and Wait concurrently
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = k.Wait(pid)
	}()

	// Kill after short delay
	time.Sleep(10 * time.Millisecond)
	_ = k.Kill(pid, types.SIGKILL)
	close(blockCh)

	select {
	case <-done:
		// Wait completed successfully
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for concurrent Kill+Wait")
	}

	// Process should be fully cleaned up
	_, ok := k.GetProcess(pid)
	if ok {
		t.Error("process should be removed after Wait")
	}
}

func TestWait_SyscallEvent(t *testing.T) {
	// Verify Wait emits SyscallEvents — entry + exit events before close(DebugChan)
	llmFile := &mockLLMFile{
		readData: makeLLMResponse("event wait", 1),
	}
	k, _, _ := newTestKernel(llmFile)

	pid, err := k.Spawn("wait event test", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, _ := k.GetProcess(pid)
	debugCh := proc.DebugChan // save ref before Wait nils it

	// Wait (this also closes DebugChan)
	exit, err := k.Wait(pid)
	if err != nil {
		t.Fatalf("Wait failed: %v", err)
	}
	if exit.Code != 0 {
		t.Errorf("expected exit code 0, got %d", exit.Code)
	}

	// Drain closed DebugChan — buffered events are still readable
	var waitEvents []types.SyscallEvent
	for {
		select {
		case ev, open := <-debugCh:
			if !open {
				goto drained
			}
			if ev.Syscall == "Wait" {
				waitEvents = append(waitEvents, ev)
			}
		case <-time.After(2 * time.Second):
			t.Fatal("timed out draining DebugChan")
		}
	}
drained:

	if len(waitEvents) < 2 {
		t.Fatalf("expected at least 2 Wait events (entry + exit), got %d", len(waitEvents))
	}

	// Verify entry event
	entryArgs := waitEvents[0].Args
	if entryArgs["pid"] != pid {
		t.Errorf("entry event pid: got %v, want %d", entryArgs["pid"], pid)
	}

	// Verify exit event has "completed" action
	exitArgs := waitEvents[len(waitEvents)-1].Args
	if exitArgs["action"] != "completed" {
		t.Errorf("exit event action: got %v, want %q", exitArgs["action"], "completed")
	}
}
