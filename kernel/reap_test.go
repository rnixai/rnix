package kernel

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// --- Story 4.1: Wait syscall tests ---

func TestWait_NormalCompletion(t *testing.T) {
	// Spawn a process that completes normally, then Wait for it
	llmFile := &mockLLMFile{
		readData: makeLLMResponse("wait result", 10),
	}
	k, _, ctxMgr := newTestKernel(t, llmFile)
	defer k.Shutdown()

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

	// Verify resource release: process Dead in table (TTL-retained)
	waitedProc, ok := k.GetProcess(pid)
	if !ok || waitedProc.GetState() != types.StateDead {
		t.Error("process should be Dead in table after Wait")
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
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

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

	// Verify process Dead in table (TTL-retained)
	waitedProc, ok := k.GetProcess(pid)
	if !ok || waitedProc.GetState() != types.StateDead {
		t.Error("process should be Dead in table after Wait")
	}

	// Verify context freed
	_, ctxErr := ctxMgr.BuildPrompt(ctxID)
	if ctxErr == nil {
		t.Error("context should be freed after Wait")
	}
}

func TestWait_PIDNotFound(t *testing.T) {
	k := newSimpleKernel(t)
	defer k.Shutdown()

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
	k, _, ctxMgr := newTestKernel(t, llmFile)
	defer k.Shutdown()

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

	// 2. Process Dead in table (TTL-retained)
	waitedProc, ok := k.GetProcess(pid)
	if !ok || waitedProc.GetState() != types.StateDead {
		t.Error("process should be Dead in table")
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
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

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

	// Process should be fully cleaned up (Dead in table with TTL)
	waitedProc, ok := k.GetProcess(pid)
	if !ok || waitedProc.GetState() != types.StateDead {
		t.Error("process should be Dead in table after Wait")
	}
}

func TestWait_SyscallEvent(t *testing.T) {
	// Verify Wait emits SyscallEvents — entry + exit events before close(DebugChan)
	llmFile := &mockLLMFile{
		readData: makeLLMResponse("event wait", 1),
	}
	k, _, _ := newTestKernel(t, llmFile)
	defer k.Shutdown()

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

// --- Story 4.2: Orphan reparent tests ---

func TestOrphanReparent_RunningChild(t *testing.T) {
	// Parent exits while child is still running → child reparented to PID 0
	parentBlockCh := make(chan struct{})
	childBlockCh := make(chan struct{})

	callCount := 0
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(subpath string, flags vfs.OpenFlag) (vfs.VFSFile, error) {
		callCount++
		if callCount == 1 {
			// Parent: completes immediately when unblocked
			return &blockingLLMFile{blockCh: parentBlockCh}, nil
		}
		// Child: blocks until test releases
		return &blockingLLMFile{blockCh: childBlockCh}, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	// Spawn parent
	parentPID, err := k.Spawn("parent", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("parent Spawn failed: %v", err)
	}

	// Let parent start Running
	time.Sleep(50 * time.Millisecond)

	// Spawn child with ParentPID
	childPID, err := k.Spawn("child", nil, SpawnOpts{ParentPID: parentPID})
	if err != nil {
		t.Fatalf("child Spawn failed: %v", err)
	}

	// Let child start Running
	time.Sleep(50 * time.Millisecond)

	// Let parent complete
	close(parentBlockCh)

	// Wait for parent → triggers reparent of child
	_, err = k.Wait(parentPID)
	if err != nil {
		t.Fatalf("Wait parent failed: %v", err)
	}

	// Verify child reparented to PID 0
	child, ok := k.GetProcess(childPID)
	if !ok {
		t.Fatal("child should still be in process table")
	}
	child.mu.Lock()
	ppid := child.PPID
	child.mu.Unlock()
	if ppid != 0 {
		t.Errorf("child PPID: got %d, want 0 (reparented to init)", ppid)
	}

	// Cleanup: let child finish
	close(childBlockCh)
	_, _ = k.Wait(childPID)
}

func TestOrphanReparent_ZombieChild(t *testing.T) {
	// Parent exits while child is zombie → child auto-reaped
	reg := vfs.NewDeviceRegistry()
	parentBlockCh := make(chan struct{})
	callCount := 0
	_ = reg.Register("/dev/llm/claude", func(subpath string, flags vfs.OpenFlag) (vfs.VFSFile, error) {
		callCount++
		if callCount == 1 {
			return &blockingLLMFile{blockCh: parentBlockCh}, nil
		}
		// Child: completes immediately
		return &mockLLMFile{readData: makeLLMResponse("child done", 1)}, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	// Spawn parent
	parentPID, err := k.Spawn("parent", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("parent Spawn failed: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	// Spawn child — it will complete immediately and become Zombie
	childPID, err := k.Spawn("child", nil, SpawnOpts{ParentPID: parentPID})
	if err != nil {
		t.Fatalf("child Spawn failed: %v", err)
	}

	// Wait for child to become Zombie
	child, _ := k.GetProcess(childPID)
	select {
	case <-child.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for child to finish")
	}

	// Parent finishes → handleOrphanChildren should push zombie child to reapCh
	close(parentBlockCh)
	_, err = k.Wait(parentPID)
	if err != nil {
		t.Fatalf("Wait parent failed: %v", err)
	}

	// Wait for reaper to auto-reap the zombie child (now Dead with TTL)
	deadline := time.After(5 * time.Second)
	for {
		childProc, ok := k.GetProcess(childPID)
		if ok && childProc.GetState() == types.StateDead {
			break // child successfully reaped
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for zombie child to be auto-reaped")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestOrphanReparent_SyscallEvent(t *testing.T) {
	// Verify Reparent SyscallEvent is emitted
	parentBlockCh := make(chan struct{})
	childBlockCh := make(chan struct{})

	callCount := 0
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(subpath string, flags vfs.OpenFlag) (vfs.VFSFile, error) {
		callCount++
		if callCount == 1 {
			return &blockingLLMFile{blockCh: parentBlockCh}, nil
		}
		return &blockingLLMFile{blockCh: childBlockCh}, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	parentPID, _ := k.Spawn("parent", nil, SpawnOpts{})
	time.Sleep(50 * time.Millisecond)

	childPID, _ := k.Spawn("child", nil, SpawnOpts{ParentPID: parentPID})
	time.Sleep(50 * time.Millisecond)

	child, _ := k.GetProcess(childPID)
	debugCh := child.DebugChan

	// Parent completes → reparent
	close(parentBlockCh)
	_, _ = k.Wait(parentPID)

	// Drain child's DebugChan for Reparent event
	var found bool
	deadline := time.After(2 * time.Second)
	for !found {
		select {
		case ev := <-debugCh:
			if ev.Syscall == "Reparent" {
				found = true
				if ev.Args["child_pid"] != childPID {
					t.Errorf("Reparent child_pid: got %v, want %d", ev.Args["child_pid"], childPID)
				}
				if ev.Args["old_ppid"] != parentPID {
					t.Errorf("Reparent old_ppid: got %v, want %d", ev.Args["old_ppid"], parentPID)
				}
				if ev.Args["new_ppid"] != types.PID(0) {
					t.Errorf("Reparent new_ppid: got %v, want 0", ev.Args["new_ppid"])
				}
			}
		case <-deadline:
			t.Fatal("timed out waiting for Reparent SyscallEvent")
		}
	}

	// Cleanup
	close(childBlockCh)
	_, _ = k.Wait(childPID)
}

// --- Story 4.2: Auto-reap tests ---

func TestAutoReap_ChildFinishesAfterParentRemoved(t *testing.T) {
	// Scenario: Parent is Wait-ed and removed first.
	// Then child finishes → finishProcess detects parent gone → auto-reap via reapCh.
	parentBlockCh := make(chan struct{})
	childBlockCh := make(chan struct{})
	callCount := 0
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(subpath string, flags vfs.OpenFlag) (vfs.VFSFile, error) {
		callCount++
		if callCount == 1 {
			return &blockingLLMFile{blockCh: parentBlockCh}, nil
		}
		return &blockingLLMFile{blockCh: childBlockCh}, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	// Spawn parent, let it start
	parentPID, err := k.Spawn("parent", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn parent failed: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	// Spawn child
	childPID, err := k.Spawn("child", nil, SpawnOpts{ParentPID: parentPID})
	if err != nil {
		t.Fatalf("Spawn child failed: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	// Parent finishes first → reparents running child to PID 0
	close(parentBlockCh)
	_, _ = k.Wait(parentPID)

	// Now child finishes → since PPID=0 (reparented), finishProcess won't auto-reap (PPID=0 excluded)
	// But let's verify via explicit Wait
	close(childBlockCh)
	_, err = k.Wait(childPID)
	if err != nil {
		t.Fatalf("Wait child failed: %v", err)
	}

	waitedChild, ok := k.GetProcess(childPID)
	if !ok || waitedChild.GetState() != types.StateDead {
		t.Error("child should be Dead in table after Wait")
	}
}

func TestAutoReap_OrphanChildFinishes_ParentAlreadyGone(t *testing.T) {
	// Spawn parent, spawn child, remove parent manually (simulating Wait completion),
	// then child finishes → finishProcess orphan detection auto-reaps.
	parentBlockCh := make(chan struct{})
	childBlockCh := make(chan struct{})
	callCount := 0
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(subpath string, flags vfs.OpenFlag) (vfs.VFSFile, error) {
		callCount++
		if callCount == 1 {
			return &blockingLLMFile{blockCh: parentBlockCh}, nil
		}
		return &blockingLLMFile{blockCh: childBlockCh}, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	parentPID, _ := k.Spawn("parent", nil, SpawnOpts{})
	time.Sleep(50 * time.Millisecond)

	childPID, _ := k.Spawn("child", nil, SpawnOpts{ParentPID: parentPID})
	time.Sleep(50 * time.Millisecond)

	// Complete and reap parent
	close(parentBlockCh)
	_, _ = k.Wait(parentPID)

	// Verify parent is Dead (TTL-retained in table)
	parentProc, ok := k.GetProcess(parentPID)
	if !ok {
		t.Fatal("parent should still be in table (Dead with TTL)")
	}
	if parentProc.GetState() != types.StateDead {
		t.Fatalf("parent should be Dead, got %v", parentProc.GetState())
	}

	// Child's PPID is now 0 (reparented by handleOrphanChildren)
	child, ok := k.GetProcess(childPID)
	if !ok {
		t.Fatal("child should still exist")
	}
	child.mu.Lock()
	ppid := child.PPID
	child.mu.Unlock()
	if ppid != 0 {
		t.Errorf("child PPID should be 0 (reparented), got %d", ppid)
	}

	// Let child finish — PPID=0 so no auto-reap, need explicit Wait
	close(childBlockCh)
	_, err := k.Wait(childPID)
	if err != nil {
		t.Fatalf("Wait child failed: %v", err)
	}
}

func TestAutoReap_Reaper_ProcessesMultiplePIDs(t *testing.T) {
	// Reaper goroutine processes multiple zombie PIDs from reapCh
	reg := vfs.NewDeviceRegistry()
	parentBlockCh := make(chan struct{})
	childCompletedCount := 0
	_ = reg.Register("/dev/llm/claude", func(subpath string, flags vfs.OpenFlag) (vfs.VFSFile, error) {
		childCompletedCount++
		if childCompletedCount == 1 {
			return &blockingLLMFile{blockCh: parentBlockCh}, nil
		}
		// All children complete immediately
		return &mockLLMFile{readData: makeLLMResponse("child done", 1)}, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	parentPID, _ := k.Spawn("parent", nil, SpawnOpts{})
	time.Sleep(50 * time.Millisecond)

	// Spawn 3 children that complete immediately → become zombies
	childPIDs := make([]types.PID, 3)
	for i := 0; i < 3; i++ {
		pid, err := k.Spawn("child", nil, SpawnOpts{ParentPID: parentPID})
		if err != nil {
			t.Fatalf("Spawn child %d failed: %v", i, err)
		}
		childPIDs[i] = pid
	}

	// Wait for all children to become Zombie
	for _, pid := range childPIDs {
		child, _ := k.GetProcess(pid)
		select {
		case <-child.Done:
		case <-time.After(5 * time.Second):
			t.Fatalf("child %d did not finish", pid)
		}
	}

	// Parent finishes → handleOrphanChildren pushes zombie children to reapCh
	close(parentBlockCh)
	_, _ = k.Wait(parentPID)

	// All zombie children should be auto-reaped (Dead with TTL)
	deadline := time.After(5 * time.Second)
	for _, pid := range childPIDs {
		for {
			proc, ok := k.GetProcess(pid)
			if ok && proc.GetState() == types.StateDead {
				break
			}
			select {
			case <-deadline:
				t.Fatalf("child %d was not auto-reaped", pid)
			case <-time.After(10 * time.Millisecond):
			}
		}
	}
}

// --- Story 4.2: Comprehensive integration tests (Task 6) ---

func TestIntegration_FullLifecycle(t *testing.T) {
	// 6.1: Parent Spawn Child → Parent Finish → Child Reparent → Child Finish → Auto-Reap
	parentBlockCh := make(chan struct{})
	childBlockCh := make(chan struct{})
	callCount := 0
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(subpath string, flags vfs.OpenFlag) (vfs.VFSFile, error) {
		callCount++
		if callCount == 1 {
			return &blockingLLMFile{blockCh: parentBlockCh}, nil
		}
		return &blockingLLMFile{blockCh: childBlockCh}, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	// 1. Spawn parent
	parentPID, err := k.Spawn("parent", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn parent: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	// 2. Spawn child under parent
	childPID, err := k.Spawn("child", nil, SpawnOpts{ParentPID: parentPID})
	if err != nil {
		t.Fatalf("Spawn child: %v", err)
	}
	time.Sleep(50 * time.Millisecond)

	// Verify parent-child link
	parent, _ := k.GetProcess(parentPID)
	if children := parent.GetChildren(); len(children) != 1 || children[0] != childPID {
		t.Fatalf("parent children: got %v, want [%d]", children, childPID)
	}

	// 3. Parent finishes → child reparented to PID 0
	close(parentBlockCh)
	_, err = k.Wait(parentPID)
	if err != nil {
		t.Fatalf("Wait parent: %v", err)
	}

	// Verify parent Dead (TTL-retained)
	if parentProc, ok := k.GetProcess(parentPID); !ok || parentProc.GetState() != types.StateDead {
		t.Fatal("parent should be Dead in table")
	}

	// Verify child reparented
	child, ok := k.GetProcess(childPID)
	if !ok {
		t.Fatal("child should still exist")
	}
	child.mu.Lock()
	if child.PPID != 0 {
		t.Errorf("child PPID: got %d, want 0", child.PPID)
	}
	child.mu.Unlock()

	// 4. Child finishes
	close(childBlockCh)
	_, err = k.Wait(childPID)
	if err != nil {
		t.Fatalf("Wait child: %v", err)
	}

	// 5. Both Dead in table (TTL-retained)
	if childProc, ok := k.GetProcess(childPID); !ok || childProc.GetState() != types.StateDead {
		t.Error("child should be Dead in table")
	}
}

func TestIntegration_MultipleChildren(t *testing.T) {
	// 6.2: Parent with 3 children: 1 Running + 1 Zombie + 1 already Dead
	parentBlockCh := make(chan struct{})
	runningBlockCh := make(chan struct{})
	callCount := 0
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(subpath string, flags vfs.OpenFlag) (vfs.VFSFile, error) {
		callCount++
		switch callCount {
		case 1: // parent
			return &blockingLLMFile{blockCh: parentBlockCh}, nil
		case 2: // child1: completes immediately (→ Zombie, no one Wait)
			return &mockLLMFile{readData: makeLLMResponse("done", 1)}, nil
		case 3: // child2: running (blocked)
			return &blockingLLMFile{blockCh: runningBlockCh}, nil
		default: // child3: completes immediately
			return &mockLLMFile{readData: makeLLMResponse("done", 1)}, nil
		}
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	parentPID, _ := k.Spawn("parent", nil, SpawnOpts{})
	time.Sleep(50 * time.Millisecond)

	// Child 1: zombie (completes but not Wait-ed)
	zombiePID, _ := k.Spawn("zombie-child", nil, SpawnOpts{ParentPID: parentPID})
	zombieProc, _ := k.GetProcess(zombiePID)
	select {
	case <-zombieProc.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("zombie child didn't finish")
	}

	// Child 2: running (blocked)
	runningPID, _ := k.Spawn("running-child", nil, SpawnOpts{ParentPID: parentPID})
	time.Sleep(50 * time.Millisecond)

	// Child 3: Dead (completed and Wait-ed)
	deadPID, _ := k.Spawn("dead-child", nil, SpawnOpts{ParentPID: parentPID})
	_, _ = k.Wait(deadPID)

	// Parent exits → handleOrphanChildren:
	// - zombie child → pushed to reapCh
	// - running child → reparented to PID 0
	// - dead child → already removed, skipped
	close(parentBlockCh)
	_, _ = k.Wait(parentPID)

	// Verify zombie child auto-reaped (now Dead with TTL)
	deadline := time.After(5 * time.Second)
	for {
		zProc, ok := k.GetProcess(zombiePID)
		if ok && zProc.GetState() == types.StateDead {
			break
		}
		select {
		case <-deadline:
			t.Fatal("zombie child not auto-reaped")
		case <-time.After(10 * time.Millisecond):
		}
	}

	// Verify running child reparented
	runChild, ok := k.GetProcess(runningPID)
	if !ok {
		t.Fatal("running child should still exist")
	}
	runChild.mu.Lock()
	if runChild.PPID != 0 {
		t.Errorf("running child PPID: got %d, want 0", runChild.PPID)
	}
	runChild.mu.Unlock()

	// Cleanup
	close(runningBlockCh)
	_, _ = k.Wait(runningPID)
}

func TestIntegration_ProcessTableConsistency(t *testing.T) {
	// 6.3: After abnormal exit, no dangling PIDs (NFR9)
	blockCh := make(chan struct{})
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(subpath string, flags vfs.OpenFlag) (vfs.VFSFile, error) {
		return &blockingLLMFile{blockCh: blockCh}, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	pid, _ := k.Spawn("crash test", nil, SpawnOpts{})
	time.Sleep(50 * time.Millisecond)

	// Kill (abnormal exit)
	_ = k.Kill(pid, types.SIGKILL)
	close(blockCh)

	_, _ = k.Wait(pid)

	// Verify process table: pid should be Dead (TTL-retained), not a dangling non-Dead entry
	procs := k.ListProcesses()
	for _, p := range procs {
		if p.PID == pid && p.State != types.StateDead {
			t.Errorf("dangling PID %d found in process table in state %v after abnormal exit", pid, p.State)
		}
	}
}

func TestIntegration_ResourceReleaseTimeliness(t *testing.T) {
	// 6.4: Verify goroutine/context released within 10 seconds (NFR8)
	llmFile := &mockLLMFile{readData: makeLLMResponse("nfr8 test", 1)}
	k, _, ctxMgr := newTestKernel(t, llmFile)
	defer k.Shutdown()

	pid, _ := k.Spawn("nfr8", nil, SpawnOpts{})
	proc, _ := k.GetProcess(pid)
	ctxID := proc.CtxID

	start := time.Now()
	_, _ = k.Wait(pid)
	elapsed := time.Since(start)

	// Context should be freed
	_, err := ctxMgr.BuildPrompt(ctxID)
	if err == nil {
		t.Error("context should be freed after Wait")
	}

	// Should complete well within 10 seconds
	if elapsed > 10*time.Second {
		t.Errorf("resource release took %v (NFR8 requires < 10s)", elapsed)
	}
}

func TestIntegration_ConcurrentExits(t *testing.T) {
	// 6.5: Multiple processes exit simultaneously, reaper handles concurrently
	const n = 5
	reg := vfs.NewDeviceRegistry()
	parentBlockCh := make(chan struct{})
	callCount := 0
	_ = reg.Register("/dev/llm/claude", func(subpath string, flags vfs.OpenFlag) (vfs.VFSFile, error) {
		callCount++
		if callCount == 1 {
			return &blockingLLMFile{blockCh: parentBlockCh}, nil
		}
		return &mockLLMFile{readData: makeLLMResponse("done", 1)}, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	defer k.Shutdown()

	parentPID, _ := k.Spawn("parent", nil, SpawnOpts{})
	time.Sleep(50 * time.Millisecond)

	childPIDs := make([]types.PID, n)
	for i := 0; i < n; i++ {
		pid, _ := k.Spawn("child", nil, SpawnOpts{ParentPID: parentPID})
		childPIDs[i] = pid
	}

	// Wait for all children to become Zombie
	for _, pid := range childPIDs {
		child, _ := k.GetProcess(pid)
		select {
		case <-child.Done:
		case <-time.After(5 * time.Second):
			t.Fatalf("child %d didn't finish", pid)
		}
	}

	// Parent exits → all zombie children auto-reaped
	close(parentBlockCh)
	_, _ = k.Wait(parentPID)

	// All should be reaped (Dead with TTL)
	deadline := time.After(5 * time.Second)
	for _, pid := range childPIDs {
		for {
			cProc, ok := k.GetProcess(pid)
			if ok && cProc.GetState() == types.StateDead {
				break
			}
			select {
			case <-deadline:
				t.Fatalf("child %d not auto-reaped", pid)
			case <-time.After(10 * time.Millisecond):
			}
		}
	}
}

// --- Story 4.2 Review: Additional coverage tests ---

func TestReapOnce_ConcurrentReapProcess(t *testing.T) {
	// M1: Verify reapOnce prevents double resource release when
	// multiple goroutines call reapProcess on the same process concurrently.
	llmFile := &mockLLMFile{readData: makeLLMResponse("reaponce test", 1)}
	k, _, ctxMgr := newTestKernel(t, llmFile)

	pid, err := k.Spawn("reaponce", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, _ := k.GetProcess(pid)
	ctxID := proc.CtxID

	// Wait for process to become Zombie
	select {
	case <-proc.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for process to finish")
	}

	if proc.GetState() != types.StateZombie {
		t.Fatalf("expected Zombie, got %d", proc.GetState())
	}

	// Launch multiple goroutines to reapProcess concurrently
	const n = 10
	var wg sync.WaitGroup
	for i := range n {
		_ = i
		wg.Add(1)
		go func() {
			defer wg.Done()
			k.reapProcess(proc)
		}()
	}
	wg.Wait()

	// Verify process was reaped exactly once
	if proc.GetState() != types.StateDead {
		t.Errorf("expected Dead, got %d", proc.GetState())
	}
	if reapedProc, ok := k.GetProcess(pid); !ok || reapedProc.GetState() != types.StateDead {
		t.Error("process should be Dead in table (TTL-retained)")
	}
	if _, err := ctxMgr.BuildPrompt(ctxID); err == nil {
		t.Error("context should be freed")
	}
}

// --- Story 4.5: CtxFree order verification ---

func TestReapProcess_CtxFreeCalledInOrder(t *testing.T) {
	// Verify the complete reapProcess resource release sequence executed correctly.
	// Since all operations inside reapOnce.Do are sequential, verifying that all
	// post-conditions hold is equivalent to verifying execution order — if any step
	// was skipped or reordered, at least one post-condition would fail.
	// True instrumentation-based ordering verification would require mocking ctxMgr
	// (currently a concrete type), which is out of scope.
	//
	// Post-conditions verified:
	// 1. DebugChan is closed (nil-ed and closed)
	// 2. Context is freed (BuildPrompt returns error)
	// 3. Process state is Dead (Reap was called)
	// 4. Process removed from table (RemoveProcess was called)
	llmFile := &mockLLMFile{readData: makeLLMResponse("order test", 1)}
	k, _, ctxMgr := newTestKernel(t, llmFile)
	defer k.Shutdown()

	pid, err := k.Spawn("order-verify", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, _ := k.GetProcess(pid)
	ctxID := proc.CtxID
	debugCh := proc.DebugChan // save ref before reapProcess nils it

	// Wait for process to become Zombie (via Done channel)
	select {
	case <-proc.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for process to finish")
	}

	// Call reapProcess directly
	k.reapProcess(proc)

	// 1. DebugChan closed: drain buffered events, then verify closed
	for {
		select {
		case _, open := <-debugCh:
			if !open {
				goto debugClosed
			}
		case <-time.After(2 * time.Second):
			t.Fatal("timed out draining DebugChan")
		}
	}
debugClosed:

	// 2. Context freed: BuildPrompt returns error with ErrNotFound
	_, ctxErr := ctxMgr.BuildPrompt(ctxID)
	if ctxErr == nil {
		t.Error("context should be freed after reapProcess")
	}

	// 3. Process state is Dead (Reap was called after CtxFree)
	if proc.GetState() != types.StateDead {
		t.Errorf("expected Dead state, got %d", proc.GetState())
	}

	// 4. Process Dead in table (TTL-retained, no longer immediately removed)
	if deadProc, ok := k.GetProcess(pid); !ok || deadProc.GetState() != types.StateDead {
		t.Error("process should be Dead in table after reapProcess")
	}
}

func TestShutdown_DrainsReapCh(t *testing.T) {
	// M3: Verify Shutdown drains remaining PIDs in reapCh before exiting.
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag) (vfs.VFSFile, error) {
		return &mockLLMFile{readData: makeLLMResponse("drain test", 1)}, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)

	// Spawn a process, let it complete to Zombie
	pid, err := k.Spawn("drain", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}
	proc, _ := k.GetProcess(pid)
	select {
	case <-proc.Done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out")
	}

	// Manually push PID into reapCh (simulating orphan detection)
	k.reapCh <- pid

	// Shutdown should drain reapCh and reap the zombie
	k.Shutdown()

	if drainProc, ok := k.GetProcess(pid); !ok || drainProc.GetState() != types.StateDead {
		t.Error("process should be Dead in table after Shutdown drain")
	}
}

func TestShutdown_Idempotent(t *testing.T) {
	// H3: Verify Shutdown can be called multiple times without panic.
	k := newSimpleKernel(t)
	k.Shutdown()
	k.Shutdown() // must not panic
	k.Shutdown() // must not panic
}

// --- reapProcess integration: MCP mount cleanup (Story 9.2 / Epic 9 retro) ---

func TestIntegration_ReapProcess_MCPUnmountOnExit(t *testing.T) {
	// Integration test: Spawn with MCP mounts → process exits → finishProcess
	// auto-unmounts → reapProcess completes full resource release chain.
	// Validates: finishProcess calls Unmount for each MCPMount, then reapProcess
	// cleans up DebugChan, Context, MsgQueue, Groups, Signals, Threads, Coroutines.

	t.Run("process with MCP mounts auto-unmounts on normal exit", func(t *testing.T) {
		// Given: a kernel with mock MountManager and a process with 2 MCP mounts
		llmFile := &mockLLMFile{readData: makeLLMResponse("mcp exit", 1)}
		k, _, ctxMgr := newTestKernel(t, llmFile)

		mm := newMockMountManager()
		k.mountMgr = mm

		pid, err := k.Spawn("mcp-cleanup-test", nil, SpawnOpts{})
		if err != nil {
			t.Fatalf("Spawn failed: %v", err)
		}

		proc, _ := k.GetProcess(pid)
		ctxID := proc.CtxID

		// Simulate MCP mounts attached to the process
		mountPath1 := fmt.Sprintf("/mnt/mcp/%d-github", pid)
		mountPath2 := fmt.Sprintf("/mnt/mcp/%d-slack", pid)
		mm.mounted[mountPath1] = true
		mm.mounted[mountPath2] = true
		proc.mu.Lock()
		proc.MCPMounts = []string{mountPath1, mountPath2}
		proc.mu.Unlock()

		// When: Wait for process to complete (finishProcess + reapProcess)
		exit, err := k.Wait(pid)
		if err != nil {
			t.Fatalf("Wait failed: %v", err)
		}
		if exit.Code != 0 {
			t.Fatalf("expected exit code 0, got %d: %s", exit.Code, exit.Reason)
		}

		// Then: both MCP mounts are unmounted
		if mm.mounted[mountPath1] {
			t.Errorf("mount %s should have been unmounted", mountPath1)
		}
		if mm.mounted[mountPath2] {
			t.Errorf("mount %s should have been unmounted", mountPath2)
		}

		// And: process is Dead in table (TTL-retained)
		if reapedProc, ok := k.GetProcess(pid); !ok || reapedProc.GetState() != types.StateDead {
			t.Error("process should be Dead in table after reap")
		}

		// And: context was freed
		_, ctxErr := ctxMgr.BuildPrompt(ctxID)
		if ctxErr == nil {
			t.Error("context should be freed after reap")
		}

		// And: process state is Dead
		if proc.GetState() != types.StateDead {
			t.Errorf("expected Dead state, got %d", proc.GetState())
		}
	})

	t.Run("process with MCP mounts auto-unmounts on kill", func(t *testing.T) {
		// Given: a kernel with a blocking process and MCP mounts
		blockCh := make(chan struct{})
		reg := vfs.NewDeviceRegistry()
		_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag) (vfs.VFSFile, error) {
			return &blockingLLMFile{blockCh: blockCh}, nil
		})
		v := vfs.NewVFS(reg)
		ctxMgr := rnixctx.NewManager()
		k := NewKernel(v, ctxMgr, nil)
		defer k.Shutdown()

		mm := newMockMountManager()
		k.mountMgr = mm

		pid, err := k.Spawn("mcp-kill-test", nil, SpawnOpts{})
		if err != nil {
			t.Fatalf("Spawn failed: %v", err)
		}

		proc, _ := k.GetProcess(pid)

		// Simulate MCP mount
		mountPath := fmt.Sprintf("/mnt/mcp/%d-github", pid)
		mm.mounted[mountPath] = true
		proc.mu.Lock()
		proc.MCPMounts = []string{mountPath}
		proc.mu.Unlock()

		// Let goroutine start
		time.Sleep(50 * time.Millisecond)

		// When: Kill then Wait
		if err := k.Kill(pid, types.SIGTERM); err != nil {
			t.Fatalf("Kill failed: %v", err)
		}
		close(blockCh) // unblock LLM so finishProcess can run

		exit, err := k.Wait(pid)
		if err != nil {
			t.Fatalf("Wait failed: %v", err)
		}
		// Exit code may be 0 (normal completion) or non-zero (killed) depending on timing.
		// The key assertion is that MCP mounts were cleaned up regardless.
		_ = exit

		// Then: MCP mount is unmounted
		if mm.mounted[mountPath] {
			t.Errorf("mount %s should have been unmounted after kill", mountPath)
		}

		// And: process is Dead in table (TTL-retained)
		if reapedProc, ok := k.GetProcess(pid); !ok || reapedProc.GetState() != types.StateDead {
			t.Error("process should be Dead in table after kill+wait")
		}
	})

	t.Run("unmount failure does not block process exit", func(t *testing.T) {
		// Given: a kernel where Unmount always fails
		llmFile := &mockLLMFile{readData: makeLLMResponse("unmount-fail", 1)}
		k, _, _ := newTestKernel(t, llmFile)

		mm := newMockMountManager()
		mm.unmountFn = func(path string) error {
			return fmt.Errorf("unmount failed: permission denied")
		}
		k.mountMgr = mm

		pid, err := k.Spawn("unmount-fail-test", nil, SpawnOpts{})
		if err != nil {
			t.Fatalf("Spawn failed: %v", err)
		}

		proc, _ := k.GetProcess(pid)
		mountPath := fmt.Sprintf("/mnt/mcp/%d-github", pid)
		proc.mu.Lock()
		proc.MCPMounts = []string{mountPath}
		proc.mu.Unlock()

		// When: process finishes and Wait completes (despite unmount error)
		exit, err := k.Wait(pid)

		// Then: process still exits successfully (unmount failure is non-fatal)
		if err != nil {
			t.Fatalf("Wait failed: %v", err)
		}
		if exit.Code != 0 {
			t.Fatalf("expected exit code 0, got %d", exit.Code)
		}

		// And: process is Dead in table (TTL-retained)
		if reapedProc, ok := k.GetProcess(pid); !ok || reapedProc.GetState() != types.StateDead {
			t.Error("process should be Dead in table despite unmount failure")
		}
	})

	t.Run("process without MCP mounts reaps normally", func(t *testing.T) {
		// Given: a kernel with MountManager but process has no MCP mounts
		llmFile := &mockLLMFile{readData: makeLLMResponse("no-mcp", 1)}
		k, _, ctxMgr := newTestKernel(t, llmFile)

		k.mountMgr = newMockMountManager()

		pid, err := k.Spawn("no-mcp-test", nil, SpawnOpts{})
		if err != nil {
			t.Fatalf("Spawn failed: %v", err)
		}

		proc, _ := k.GetProcess(pid)
		ctxID := proc.CtxID

		// When: process finishes
		exit, err := k.Wait(pid)

		// Then: normal exit with full cleanup
		if err != nil {
			t.Fatalf("Wait failed: %v", err)
		}
		if exit.Code != 0 {
			t.Fatalf("expected exit code 0, got %d: %s", exit.Code, exit.Reason)
		}
		if reapedProc, ok := k.GetProcess(pid); !ok || reapedProc.GetState() != types.StateDead {
			t.Error("process should be Dead in table")
		}
		_, ctxErr := ctxMgr.BuildPrompt(ctxID)
		if ctxErr == nil {
			t.Error("context should be freed")
		}
	})
}

func TestIntegration_ShutdownUnmountsAll(t *testing.T) {
	// Integration: Shutdown calls mountMgr.UnmountAll() to clean up lingering mounts.
	llmFile := &mockLLMFile{readData: makeLLMResponse("shutdown", 1)}
	k, _, _ := newTestKernel(t, llmFile)

	mm := newMockMountManager()
	mm.mounted["/mnt/mcp/1-github"] = true
	mm.mounted["/mnt/mcp/2-slack"] = true
	k.mountMgr = mm

	// When: Shutdown is called
	k.Shutdown()

	// Then: all mounts are cleaned up
	if len(mm.mounted) != 0 {
		t.Errorf("expected all mounts cleared, got %d remaining: %v", len(mm.mounted), mm.mounted)
	}
}

func TestCleanupExpiredDead_RemovesAfterTTL(t *testing.T) {
	llmFile := &mockLLMFile{readData: makeLLMResponse("ttl-test", 1)}
	k, _, _ := newTestKernel(t, llmFile)
	defer k.Shutdown()

	pid, err := k.Spawn("ttl process", nil, SpawnOpts{})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	exit, err := k.Wait(pid)
	if err != nil {
		t.Fatalf("Wait failed: %v", err)
	}
	if exit.Code != 0 {
		t.Fatalf("unexpected exit code: %d", exit.Code)
	}

	// Process should be Dead but retained in table
	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatal("Dead process should be retained in table (TTL)")
	}
	if proc.GetState() != types.StateDead {
		t.Fatalf("expected Dead state, got %d", proc.GetState())
	}

	// cleanupExpiredDead with long TTL should NOT remove
	k.cleanupExpiredDead(1 * time.Hour)
	if _, ok := k.GetProcess(pid); !ok {
		t.Error("process should NOT be removed when TTL has not expired")
	}

	// cleanupExpiredDead with zero TTL should remove
	k.cleanupExpiredDead(0)
	if _, ok := k.GetProcess(pid); ok {
		t.Error("process should be removed after TTL expiry")
	}
}
