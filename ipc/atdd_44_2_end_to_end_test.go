package ipc

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/kernel"
	"github.com/rnixai/rnix/shell"
)

// =============================================================================
// ATDD 44.2 — AC#8: end-to-end "Ctrl+C = SignalTree(SIGPAUSE)" closed loop.
//
// Walks the abbreviated Decker 6-step reproduction at the IPC + executor
// level (skipping the dashboard/CLI surfaces):
//
//   1. Spawn a script-runner (SkipReasonLoop=true).
//   2. Wire ScriptExecutor with the production processSuspendController
//      adapter (44.2 Task 3.1) — exactly as handleExecScript will after
//      the patch.
//   3. Start executing a 2-statement script in a goroutine.
//   4. Between the two statements, fire `kern.SignalTree(scriptPID, SIGPAUSE)`
//      to emulate the conn.Read goroutine reacting to client disconnect.
//   5. Assert that the script-runner process and its children are in
//      StateSuspended, scriptCtx remains alive, and the executor has NOT
//      returned yet — it is parked at the statement boundary.
//   6. Fire `kern.ResumeSubtree(scriptPID)` (emulates dashboard 'R' /
//      `rnix resume <uuid>` from Story 44.3).
//   7. Assert the executor finishes within 1s; ScriptStmtPaused +
//      ScriptStmtResumed events bookend the wait.
//
// RED phase: requires `processSuspendController` (Task 3.1), the
// adapter's compile-time interface check, and 44.1's SignalTree +
// ResumeSubtree to exist on KernelImpl. The first two do not yet exist.
// =============================================================================

// captureEvents is a thread-safe ScriptExecutor.OnEvent collector — the
// production adapter forwards events on the parallel-worker goroutine,
// so the hook contract requires concurrency safety.
type captureEvents struct {
	mu     sync.Mutex
	events []shell.ScriptEvent
}

func (c *captureEvents) record(ev shell.ScriptEvent) {
	c.mu.Lock()
	c.events = append(c.events, ev)
	c.mu.Unlock()
}

func (c *captureEvents) findKind(kind shell.ScriptEventKind) (int, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i, ev := range c.events {
		if ev.Kind == kind {
			return i, true
		}
	}
	return -1, false
}

// e2eSpawner is a minimal shell.KernelSpawner that does NOT actually spawn
// children — it returns ("ok", 0, 0, nil) for every call. We avoid pulling
// in the real ipcKernelSpawner machinery (which depends on
// resolveProjectContext) so the test scope stays on the suspend/resume
// boundary behaviour.
type e2eSpawner struct {
	calls atomic.Int32
	// delay each SpawnAndWait so the test has a chance to observe the
	// "between statements" window.
	delay time.Duration
}

func (e *e2eSpawner) SpawnAndWait(ctx context.Context, _, _, _ string) (string, int, int, error) {
	e.calls.Add(1)
	if e.delay > 0 {
		select {
		case <-time.After(e.delay):
		case <-ctx.Done():
			return "", 1, 0, ctx.Err()
		}
	}
	return "ok", 0, 0, nil
}

func (e *e2eSpawner) Wait(_ context.Context, _ int) (int, error) {
	return 0, errors.New("e2eSpawner: Wait not supported")
}

// TestATDD_44_2_080_EndToEnd_SignalTreeSIGPAUSE_ParkExecutor_Resume_Continues
//
// AC#8 closed-loop: SIGPAUSE between statements parks the executor at
// the next statement boundary; ResumeSubtree wakes it; the executor
// completes the remaining statement.
func TestATDD_44_2_080_EndToEnd_SignalTreeSIGPAUSE_ParkExecutor_Resume_Continues(t *testing.T) {
	t.Parallel()

	// Use the public kernel constructor — matches the helper pattern from
	// `ipc/atdd_43_2_script_trace_test.go`.
	k := kernel.NewKernel(nil, rnixctx.NewManager(), nil)
	t.Cleanup(func() { k.Shutdown() })

	// Spawn the script-runner. SkipReasonLoop matches handleExecScript.
	scriptPID, err := k.Spawn("run: e2e.ash", nil, kernel.SpawnOpts{SkipReasonLoop: true})
	if err != nil {
		t.Fatalf("Spawn script-runner: %v", err)
	}
	scriptProc, ok := k.GetProcess(scriptPID)
	if !ok {
		t.Fatalf("script-runner vanished after Spawn")
	}

	// Wire the production-shape SuspendController adapter (Task 3.1).
	ctrl := processSuspendController{proc: scriptProc}

	// Parse a minimal 2-statement script — first statement runs, second
	// statement must wait at the boundary until ResumeSubtree fires.
	src := "export PHASE=one\nexport PHASE=two\n"
	script, err := shell.ParseScript(src)
	if err != nil {
		t.Fatalf("ParseScript: %v", err)
	}

	spawner := &e2eSpawner{delay: 10 * time.Millisecond}
	env := shell.NewEnvironment()
	executor := shell.NewScriptExecutor(spawner, env)
	executor.SetSuspendController(ctrl)

	cap := &captureEvents{}
	executor.OnEvent = cap.record

	scriptCtx, scriptCancel := context.WithCancelCause(context.Background())
	defer scriptCancel(nil)

	execDone := make(chan error, 1)
	go func() {
		_, err := executor.Execute(scriptCtx, script)
		execDone <- err
	}()

	// Allow the first export to land, then fire SIGPAUSE on the subtree —
	// emulates conn.Read goroutine's new behaviour from AC#1.
	time.Sleep(30 * time.Millisecond)
	if _, err := k.SignalTree(scriptPID, types.SIGPAUSE); err != nil {
		t.Fatalf("SignalTree(SIGPAUSE): %v", err)
	}

	// AC#1 assertion: scriptCtx remains alive — conn.Read goroutine MUST
	// NOT cancel it. (The test does not run the watcher; we assert the
	// scriptCtx invariant directly.)
	if err := scriptCtx.Err(); err != nil {
		t.Errorf("scriptCtx.Err() = %v after SignalTree(SIGPAUSE); want nil (suspend MUST NOT cancel ctx)", err)
	}

	// Wait up to 500ms for the script-runner to transition to Suspended.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if scriptProc.GetState() == types.StateSuspended {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := scriptProc.GetState(); got != types.StateSuspended {
		t.Fatalf("script-runner state = %s, want Suspended after SignalTree(SIGPAUSE)", got)
	}

	// The executor must NOT have completed yet — it is parked at the
	// boundary before "export PHASE=two".
	select {
	case got := <-execDone:
		t.Fatalf("Execute returned prematurely (err=%v) while parent was Suspended", got)
	case <-time.After(150 * time.Millisecond):
	}

	// Executor must have emitted ScriptStmtPaused before parking.
	if _, ok := cap.findKind(shell.ScriptStmtPaused); !ok {
		t.Error("ScriptStmtPaused was not emitted before park; Timeline observability broken")
	}

	// Resume the subtree — emulates dashboard 'R' / `rnix resume <uuid>`.
	if _, _, err := k.ResumeSubtree(scriptPID); err != nil {
		t.Fatalf("ResumeSubtree: %v", err)
	}

	select {
	case err := <-execDone:
		if err != nil {
			t.Fatalf("Execute after resume: %v", err)
		}
	case <-time.After(1500 * time.Millisecond):
		t.Fatal("Execute did not finish within 1.5s after ResumeSubtree")
	}

	// Final-state assertions.
	if v, _ := env.Get("PHASE"); v != "two" {
		t.Errorf("PHASE = %q after resume, want \"two\" (the second statement did not run)", v)
	}
	if _, ok := cap.findKind(shell.ScriptStmtResumed); !ok {
		t.Error("ScriptStmtResumed was not emitted after resume")
	}
	if got := scriptProc.GetState(); got != types.StateRunning {
		t.Errorf("script-runner state = %s, want Running after ResumeSubtree", got)
	}
}

// TestATDD_44_2_081_EndToEnd_AdapterImplementsSuspendController
//
// Static guard: the production adapter `processSuspendController` MUST
// implement `shell.SuspendController`. Task 3.1 also requires this exact
// `var _ ...` assertion in `ipc/script_suspend_controller.go` (or
// equivalent). Duplicating the assertion in the test fails-fast if the
// adapter's signature drifts.
func TestATDD_44_2_081_EndToEnd_AdapterImplementsSuspendController(t *testing.T) {
	var _ shell.SuspendController = processSuspendController{}
	var _ shell.SuspendController = (*processSuspendController)(nil)

	// Behavioural smoke: a fresh process should report IsSuspendRequested=false.
	p := kernel.NewProcess(0, "smoke", nil)
	if err := p.Start(); err != nil {
		t.Fatalf("Start: %v", err)
	}
	ctrl := processSuspendController{proc: p}
	if ctrl.IsSuspendRequested() {
		t.Error("IsSuspendRequested = true on freshly Started process; want false")
	}
	ch := ctrl.ResumedCh()
	if ch == nil {
		t.Error("ResumedCh = nil on Running process; want non-nil open chan")
	}
}
