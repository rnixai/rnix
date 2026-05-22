package ipc

import (
	"context"

	"github.com/rnixai/rnix/kernel"
	"github.com/rnixai/rnix/shell"
)

// processSuspendController adapts a *kernel.Process to the
// shell.SuspendController interface so ScriptExecutor can park at statement
// boundaries when the script-runner subtree is SIGPAUSE-suspended, without the
// shell package importing kernel (Story 44.2 AC#3).
type processSuspendController struct{ proc *kernel.Process }

func (c processSuspendController) IsSuspendRequested() bool { return c.proc.IsSuspendRequested() }
func (c processSuspendController) ResumedCh() <-chan struct{} { return c.proc.ResumedCh() }

// Compile-time interface satisfaction guard (Story 44.2 AC#3).
var _ shell.SuspendController = processSuspendController{}

// runCancelWatcher implements the CancelledCh watcher of handleExecScript
// (Story 44.2 AC#1.b), extracted so it is unit-testable without a full
// conn/handler pair. It blocks until one of {done, scriptProc.CancelledCh(),
// scriptCtx.Done()} fires and calls scriptCancel(nil) ONLY when:
//
//   - done fires (daemon shutdown), OR
//   - CancelledCh fires AND IsSuspendRequested() == false (real termination,
//     e.g. SIGKILL/SIGTERM, which never set the suspend flag).
//
// When CancelledCh fires while IsSuspendRequested() == true, the cancel
// originated from suspendOneForSubtree (which Store(true)s the flag BEFORE
// proc.Cancel()); the watcher MUST NOT cancel scriptCtx — the executor parks
// at the next statement boundary instead of erroring out.
func runCancelWatcher(done <-chan struct{}, scriptCtx context.Context,
	scriptProc *kernel.Process, scriptCancel context.CancelCauseFunc) {
	select {
	case <-done:
		scriptCancel(nil)
	case <-scriptProc.CancelledCh():
		if !scriptProc.IsSuspendRequested() {
			scriptCancel(nil)
		}
	case <-scriptCtx.Done():
	}
}
