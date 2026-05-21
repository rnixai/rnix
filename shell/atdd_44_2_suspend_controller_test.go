package shell

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// =============================================================================
// ATDD 44.2 — Task 2: shell.SuspendController interface + ScriptExecutor
//                     statement-boundary suspend awareness.
//
// Drives Story 44.2 AC#2 / AC#3 / AC#4. RED phase: this file references
// the not-yet-existing `SuspendController` interface, the
// `ScriptExecutor.SetSuspendController` method, and the
// `ScriptStmtPaused` / `ScriptStmtResumed` event kinds → compile failure
// is the intentional RED signal.
//
// Spec (from 44.2 §AC2 / §AC3 / §Tasks/Task 2):
//
//   - New interface in shell (NOT importing kernel — see project-context.md
//     "依赖方向（严格单向）" shell → 独立):
//
//       type SuspendController interface {
//           IsSuspendRequested() bool
//           ResumedCh() <-chan struct{}
//       }
//
//   - ScriptExecutor gains a SetSuspendController(SuspendController) setter
//     and an internal nil-tolerant field.
//
//   - executeBlock checks ctrl.IsSuspendRequested() BEFORE running each
//     statement; if true: emit ScriptStmtPaused → wait on ResumedCh() (or
//     ctx.Done()) → emit ScriptStmtResumed → fall through and execute.
//
//   - Two new ScriptEventKind constants: ScriptStmtPaused / ScriptStmtResumed.
//
//   - Fast-path: if ctrl is nil, behave exactly as today (so the existing
//     174 shell tests remain green).
// =============================================================================

// fakeSuspendCtrl is a minimal SuspendController implementation for tests.
// `suspended` atomic flips between "running" and "paused", and ResumedCh
// returns a chan that the test closes via Resume() to wake the executor.
type fakeSuspendCtrl struct {
	suspended atomic.Bool
	mu        sync.Mutex
	ch        chan struct{}
}

func newFakeSuspendCtrl() *fakeSuspendCtrl {
	return &fakeSuspendCtrl{ch: make(chan struct{})}
}

func (f *fakeSuspendCtrl) IsSuspendRequested() bool {
	return f.suspended.Load()
}

func (f *fakeSuspendCtrl) ResumedCh() <-chan struct{} {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.ch
}

// Pause flips the controller into Suspended state — readers of
// IsSuspendRequested will see true; ResumedCh waiters will block on ch.
func (f *fakeSuspendCtrl) Pause() {
	f.suspended.Store(true)
}

// Resume clears the suspended flag (in the order specified by 44.2
// §Dev Notes — store(false) BEFORE close so any goroutine that wakes and
// re-checks IsSuspendRequested sees false), then closes ch so all
// ResumedCh waiters wake. Rotates ch for the next pause cycle.
func (f *fakeSuspendCtrl) Resume() {
	f.suspended.Store(false)
	f.mu.Lock()
	oldCh := f.ch
	f.ch = make(chan struct{})
	f.mu.Unlock()
	close(oldCh)
}

// Compile-time interface satisfaction guard. SuspendController must exist
// in the shell package (added by Task 2.1) — if it doesn't, this whole
// file fails to compile, which is the intended RED-phase signal.
var _ SuspendController = (*fakeSuspendCtrl)(nil)

// --- AC#2 / AC#3: ScriptExecutor.SetSuspendController + statement-boundary wait ---

// TestATDD_44_2_020_ScriptExecutor_AcceptsSuspendController
//
// Smoke test: the setter exists and a nil controller leaves the fast-path
// intact (no-op behaviour identical to executors built today).
func TestATDD_44_2_020_ScriptExecutor_AcceptsSuspendController(t *testing.T) {
	spawner := &mockSpawner{}
	env := NewEnvironment()
	executor := NewScriptExecutor(spawner, env)

	// Setter must exist (compile-fail = RED) and must accept nil.
	executor.SetSuspendController(nil)
	executor.SetSuspendController(newFakeSuspendCtrl())
}

// TestATDD_44_2_021_ScriptExecutor_WaitsAtStatementBoundary
//
// Story 44.2 AC#2: when IsSuspendRequested() returns true at the
// statement-boundary check, ScriptExecutor must NOT execute the next
// statement until ResumedCh() closes.
func TestATDD_44_2_021_ScriptExecutor_WaitsAtStatementBoundary(t *testing.T) {
	// Two export statements (cheap, no spawner needed) — the executor must
	// pause between them when the controller flips to Suspended.
	src := "export A=before\nexport B=after\n"
	script, err := ParseScript(src)
	if err != nil {
		t.Fatalf("ParseScript: %v", err)
	}

	ctrl := newFakeSuspendCtrl()
	spawner := &mockSpawner{}
	env := NewEnvironment()
	executor := NewScriptExecutor(spawner, env)
	executor.SetSuspendController(ctrl)

	// Pause BEFORE we start so the executor will block trying to run B
	// (statement-boundary check fires before each statement). Even an
	// approach that checks only between statements must observe this.
	ctrl.Pause()

	done := make(chan error, 1)
	go func() {
		_, err := executor.Execute(context.Background(), script)
		done <- err
	}()

	// Within 200ms the executor should NOT have completed — it must be
	// blocked at the statement boundary waiting on ResumedCh().
	select {
	case got := <-done:
		t.Fatalf("Execute returned prematurely (err=%v) without waiting for resume", got)
	case <-time.After(200 * time.Millisecond):
	}

	// Resume — executor must complete promptly.
	ctrl.Resume()
	select {
	case got := <-done:
		if got != nil {
			t.Fatalf("Execute after resume: %v", got)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Execute did not finish within 1s after Resume")
	}

	// Both statements must have run.
	if v, _ := env.Get("A"); v != "before" {
		t.Errorf("A=%q, want \"before\"", v)
	}
	if v, _ := env.Get("B"); v != "after" {
		t.Errorf("B=%q, want \"after\"", v)
	}
}

// TestATDD_44_2_022_ScriptExecutor_EmitsStmtPausedAndResumed
//
// Story 44.2 AC#2: when the executor enters the wait at a statement
// boundary, it MUST emit ScriptStmtPaused (Meta carries "reason" =
// "user_paused" and "stmt_kind"). After Resume, it emits ScriptStmtResumed
// before running the statement.
func TestATDD_44_2_022_ScriptExecutor_EmitsStmtPausedAndResumed(t *testing.T) {
	src := "export A=1\nexport B=2\n"
	script, err := ParseScript(src)
	if err != nil {
		t.Fatalf("ParseScript: %v", err)
	}

	var (
		evMu   sync.Mutex
		events []ScriptEvent
	)
	captureEvent := func(ev ScriptEvent) {
		evMu.Lock()
		events = append(events, ev)
		evMu.Unlock()
	}

	ctrl := newFakeSuspendCtrl()
	spawner := &mockSpawner{}
	env := NewEnvironment()
	executor := NewScriptExecutor(spawner, env)
	executor.SetSuspendController(ctrl)
	executor.OnEvent = captureEvent

	ctrl.Pause()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = executor.Execute(context.Background(), script)
	}()

	// Wait for ScriptStmtPaused to appear, then resume.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		evMu.Lock()
		hasPaused := false
		for _, ev := range events {
			if ev.Kind == ScriptStmtPaused {
				hasPaused = true
				break
			}
		}
		evMu.Unlock()
		if hasPaused {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	ctrl.Resume()
	<-done

	evMu.Lock()
	defer evMu.Unlock()

	var pausedIdx, resumedIdx = -1, -1
	for i, ev := range events {
		if ev.Kind == ScriptStmtPaused && pausedIdx < 0 {
			pausedIdx = i
		}
		if ev.Kind == ScriptStmtResumed && resumedIdx < 0 {
			resumedIdx = i
		}
	}
	if pausedIdx < 0 {
		t.Fatal("ScriptStmtPaused never emitted")
	}
	if resumedIdx < 0 {
		t.Fatal("ScriptStmtResumed never emitted")
	}
	if pausedIdx > resumedIdx {
		t.Errorf("event ordering violated: Paused at %d, Resumed at %d", pausedIdx, resumedIdx)
	}

	pausedEv := events[pausedIdx]
	if got, ok := pausedEv.Meta["reason"].(string); !ok || got != "user_paused" {
		t.Errorf("ScriptStmtPaused.Meta[reason] = %v, want \"user_paused\"", pausedEv.Meta["reason"])
	}
	if _, ok := pausedEv.Meta["stmt_kind"]; !ok {
		t.Errorf("ScriptStmtPaused.Meta is missing stmt_kind: %+v", pausedEv.Meta)
	}
}

// TestATDD_44_2_023_ScriptExecutor_ContextCancelDuringWait
//
// Story 44.2 AC#2 wait body:
//
//	select {
//	case <-ctx.Done(): return ctx.Err()
//	case <-suspendController.ResumedCh(): /* continue */
//	}
//
// Verify ctx cancellation during the wait propagates as error (so daemon
// shutdown / SIGKILL still tears down a paused executor).
func TestATDD_44_2_023_ScriptExecutor_ContextCancelDuringWait(t *testing.T) {
	src := "export A=1\nexport B=2\n"
	script, err := ParseScript(src)
	if err != nil {
		t.Fatalf("ParseScript: %v", err)
	}

	ctrl := newFakeSuspendCtrl()
	spawner := &mockSpawner{}
	env := NewEnvironment()
	executor := NewScriptExecutor(spawner, env)
	executor.SetSuspendController(ctrl)
	ctrl.Pause()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := executor.Execute(ctx, script)
		done <- err
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case got := <-done:
		if got == nil {
			t.Error("Execute returned nil, want non-nil error after ctx cancel during suspend wait")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Execute did not return within 1s after ctx cancel during suspend wait")
	}
}

// TestATDD_44_2_024_ScriptExecutor_NilControllerFastPath
//
// Story 44.2 AC#2 last bullet: "如果 ScriptExecutor 未注入 SuspendController
// （spawner 是 mock spawner、单元测试场景），跳过检查直接执行；现有 174 个
// shell/ 包测试不受影响". Pin that nil-controller path stays free of any
// blocking behaviour.
func TestATDD_44_2_024_ScriptExecutor_NilControllerFastPath(t *testing.T) {
	src := "export A=1\nexport B=2\n"
	script, err := ParseScript(src)
	if err != nil {
		t.Fatalf("ParseScript: %v", err)
	}

	spawner := &mockSpawner{}
	env := NewEnvironment()
	executor := NewScriptExecutor(spawner, env)
	// Intentionally do NOT call SetSuspendController.

	start := time.Now()
	if _, err := executor.Execute(context.Background(), script); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Errorf("nil-controller fast-path took %v, want <100ms", elapsed)
	}
}

// TestATDD_44_2_025_ScriptExecutor_WaitsBeforeWhileIteration
//
// Story 44.2 §Tasks 2.4: the suspend check must also fire at the head of
// each `while` iteration (statement-boundary equivalence inside the loop
// body) — `dev-auto.ash` is a while-true loop, so without this the user's
// 6-step reproduction does not return to the next iteration after resume.
func TestATDD_44_2_025_ScriptExecutor_WaitsBeforeWhileIteration(t *testing.T) {
	// A while loop that increments and exits when $i == 2.
	src := `export i=0
while $i != 2
  export i=2
end
`
	script, err := ParseScript(src)
	if err != nil {
		t.Fatalf("ParseScript: %v", err)
	}

	ctrl := newFakeSuspendCtrl()
	spawner := &mockSpawner{}
	env := NewEnvironment()
	executor := NewScriptExecutor(spawner, env)
	executor.SetSuspendController(ctrl)

	// Pause right before the loop body runs — the executor should not
	// reach the `export i=2` until we resume.
	ctrl.Pause()
	done := make(chan error, 1)
	go func() {
		_, err := executor.Execute(context.Background(), script)
		done <- err
	}()

	// Verify the executor is blocked: i should still be "0" after a brief wait.
	time.Sleep(150 * time.Millisecond)
	if v, _ := env.Get("i"); v == "2" {
		t.Fatal("executor advanced the while body while controller reported Suspended")
	}

	ctrl.Resume()
	select {
	case got := <-done:
		if got != nil {
			t.Fatalf("Execute: %v", got)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Execute did not finish after resume within 1s")
	}
	if v, _ := env.Get("i"); v != "2" {
		t.Errorf("i = %q, want \"2\"", v)
	}
}
