package kernel

import (
	"fmt"
	"maps"
	"time"

	"github.com/rnixai/rnix/internal/types"
)

// SignalHandler is a custom signal handler function.
type SignalHandler func(types.Signal)

// SignalManager manages signal delivery, blocking, and handling.
type SignalManager interface {
	Signal(pid types.PID, sig types.Signal) error
	SigBlock(pid types.PID, sig types.Signal) error
	SigUnblock(pid types.PID, sig types.Signal) error
}

// Compile-time interface compliance check.
var _ SignalManager = (*KernelImpl)(nil)

// Signal delivers a signal to the target process.
func (k *KernelImpl) Signal(pid types.PID, sig types.Signal) error {
	start := time.Now()

	if !sig.Valid() {
		return NewSyscallError("Signal", pid, "",
			fmt.Errorf("invalid signal: %d", sig), types.ErrInvalid)
	}

	proc, ok := k.GetProcess(pid)
	if !ok {
		return NewSyscallError("Signal", pid, "",
			fmt.Errorf("process not found"), types.ErrNotFound)
	}

	state := proc.GetState()
	if state == types.StateZombie || state == types.StateDead {
		return NewSyscallError("Signal", pid, "",
			fmt.Errorf("process %d is %s", pid, state), types.ErrNotFound)
	}

	// Suspended process: no running goroutine to deliver signals through context
	if state == types.StateSuspended {
		if sig.IsTermination() {
			return k.killSuspendedProcess(proc, sig, "Signal", start)
		}
		// Story 44.1 — Suspended-state SIGPAUSE/SIGRESUME branch:
		//   SIGPAUSE: true no-op (already suspended). Event renamed from
		//             "noop_suspended" to "noop_already_suspended" so the
		//             observable trace distinguishes "redundant pause on a
		//             suspended target" from the legacy bag-of-no-ops.
		//   SIGRESUME: delegates to ResumeSubtree so resuming a Suspended
		//             root cascades to every Suspended descendant. This is
		//             the dashboard-`r` path that pre-44.1 silently did
		//             nothing because the branch returned without state
		//             change.
		switch sig {
		case types.SIGPAUSE:
			k.emitEvent(proc, "Signal", map[string]any{
				"pid":    pid,
				"signal": sig.String(),
				"action": "noop_already_suspended",
			}, nil, nil, time.Since(start))
			return nil
		case types.SIGRESUME:
			affected, skipped, err := k.ResumeSubtree(pid)
			if err != nil {
				// Repackage as a Signal SyscallError so callers/audit tooling
				// keying on Syscall=="Signal" do not miss this error class
				// (Story 44.1 code review F10).
				return NewSyscallError("Signal", pid, "", err, types.ErrInternal)
			}
			k.emitEvent(proc, "Signal", map[string]any{
				"pid":      pid,
				"signal":   sig.String(),
				"action":   "resumed_subtree",
				"affected": affected,
				"skipped":  skipped,
			}, nil, nil, time.Since(start))
			return nil
		}
		// Other non-termination signals on Suspended: silently ignored,
		// preserving the legacy event name for backward-compat with any
		// trace tooling that still keys on "noop_suspended".
		k.emitEvent(proc, "Signal", map[string]any{
			"pid":    pid,
			"signal": sig.String(),
			"action": "noop_suspended",
		}, nil, nil, time.Since(start))
		return nil
	}

	action, extraArgs, deliverErr := k.deliverSignal(proc, sig)

	args := map[string]any{
		"pid":    pid,
		"signal": sig.String(),
		"action": action,
	}
	maps.Copy(args, extraArgs)
	k.emitEvent(proc, "Signal", args, nil, deliverErr, time.Since(start))

	if deliverErr != nil {
		return NewSyscallError("Signal", pid, "", deliverErr, types.ErrInternal)
	}
	return nil
}

// deliverSignal performs the actual signal dispatch logic.
// Uses resolveSignalDisposition for atomic check of blocked/handler/default
// under a single lock hold, preventing TOCTOU races.
// SIGKILL always uses default behavior (cannot be blocked or handled).
// Returns the action label, any structured args to merge into the Signal
// event, and an error (non-nil only for kernel-level failures — e.g.
// SuspendSubtree / ResumeSubtree returning an error).
func (k *KernelImpl) deliverSignal(proc *Process, sig types.Signal) (string, map[string]any, error) {
	disp, handler := proc.resolveSignalDisposition(sig)
	switch disp {
	case dispBlocked:
		return "blocked_pending", nil, nil
	case dispHandler:
		handler(sig)
		return "handler", nil, nil
	default:
		return k.defaultSignalAction(proc, sig)
	}
}

// defaultSignalAction executes the default behavior for a signal.
// Returns (action label, extra event args, error). The error is non-nil only
// when a downstream kernel call fails (currently SuspendSubtree /
// ResumeSubtree); in that case Signal() bubbles it up to the caller instead
// of silently swallowing it as in pre-44.1 (Story 44.1 code review F6).
func (k *KernelImpl) defaultSignalAction(proc *Process, sig types.Signal) (string, map[string]any, error) {
	switch {
	case sig == types.SIGTERM:
		// Two-phase shutdown: SIGTERM triggers graceful shutdown with grace period.
		// CAS prevents duplicate shutdown goroutines from concurrent Kill/Signal calls.
		if !proc.shutdownStarted.CompareAndSwap(false, true) {
			return "shutdown_already_started", nil, nil
		}
		// Phase 1: cancel context synchronously so callers see immediate effect
		proc.Cancel()
		// Phase 2: goroutine waits for exit or escalates to force-kill on timeout
		go k.twoPhaseShutdown(proc, proc.effectiveGracePeriod())
		return "shutdown_initiated", nil, nil
	case sig == types.SIGKILL:
		// SIGKILL: immediate termination, no grace period
		proc.Cancel()
		return "terminated", nil, nil
	case sig.IsTermination():
		// Other termination signals (future): immediate cancel
		proc.Cancel()
		return "terminated", nil, nil
	case sig == types.SIGPAUSE:
		// Story 44.1 — route through the state machine instead of the
		// legacy SoftPause path. suspendSubtree puts proc and every Running
		// descendant into Suspended; the dashboard-p / SIGPAUSE / Ctrl+C
		// (44.2) callers all funnel here so semantics stay unified.
		affected, err := k.suspendSubtree(proc)
		if err != nil {
			return "suspend_failed", map[string]any{"err": err.Error()}, err
		}
		return "suspended_subtree", map[string]any{"affected": affected}, nil
	case sig == types.SIGRESUME:
		// Story 44.1 — mirror of SIGPAUSE: subtree-scoped resume via the
		// state machine. ResumeSubtree acquires resumeMu, so the caller MUST
		// NOT already hold it (Signal's call chain does not — neither Signal
		// nor deliverSignal touches resumeMu).
		affected, skipped, err := k.ResumeSubtree(proc.PID)
		if err != nil {
			return "resume_failed", map[string]any{"err": err.Error()}, err
		}
		return "resumed_subtree", map[string]any{
			"affected": affected,
			"skipped":  skipped,
		}, nil
	default:
		return "ignored", nil, nil
	}
}

// SignalTree delivers a signal to the target process and all its living descendants.
// It traverses the process tree recursively via Children, skipping zombie/dead processes.
// Returns the total number of processes affected (including the root).
//
// Story 44.1 — SIGPAUSE / SIGRESUME short-circuit: those signals already
// fan out across the subtree via SuspendSubtree / ResumeSubtree inside
// defaultSignalAction. Letting SignalTree also recurse would yield O(N²)
// transitions and duplicate events, so we delegate to the subtree APIs and
// skip the per-node recursion entirely. We also emit a single SignalTree
// event so consumers that key on Syscall=="SignalTree" or scan for fan-out
// signal events still see the operation (Story 44.1 code review F11).
//
// For backward compatibility with the pre-44.1 contract ("affected = total
// nodes touched, including already-Suspended roots"), SIGRESUME returns
// affected+skipped as the legacy count, with the structured per-node detail
// available in the emitted SignalTree event (Story 44.1 code review F7).
func (k *KernelImpl) SignalTree(pid types.PID, sig types.Signal) (int, error) {
	if !sig.Valid() {
		return 0, NewSyscallError("SignalTree", pid, "",
			fmt.Errorf("invalid signal: %d", sig), types.ErrInvalid)
	}

	if sig == types.SIGPAUSE {
		affected, err := k.SuspendSubtree(pid)
		k.emitSignalTreeEventByPID(pid, sig, affected, 0, err)
		return affected, err
	}
	if sig == types.SIGRESUME {
		affected, skipped, err := k.ResumeSubtree(pid)
		k.emitSignalTreeEventByPID(pid, sig, affected, skipped, err)
		// Legacy contract: total nodes touched.
		return affected + skipped, err
	}

	proc, ok := k.GetProcess(pid)
	if !ok {
		return 0, NewSyscallError("SignalTree", pid, "",
			fmt.Errorf("process not found"), types.ErrNotFound)
	}

	affected := 0
	k.signalTreeRecursive(proc, sig, &affected)
	return affected, nil
}

// emitSignalTreeEventByPID emits a single rollup SignalTree event for the
// short-circuit SIGPAUSE/SIGRESUME paths. Best-effort: if the root process
// has been reaped between SignalTree entry and emit, we silently skip — the
// per-node events from suspendSubtree/ResumeSubtree still carry the trace.
func (k *KernelImpl) emitSignalTreeEventByPID(pid types.PID, sig types.Signal, affected, skipped int, err error) {
	proc, ok := k.GetProcess(pid)
	if !ok {
		return
	}
	args := map[string]any{
		"pid":      pid,
		"signal":   sig.String(),
		"affected": affected,
		"skipped":  skipped,
	}
	if err != nil {
		args["err"] = err.Error()
	}
	k.emitEvent(proc, "SignalTree", args, nil, err, 0)
}

// signalTreeRecursive sends the signal to proc and recurses into its children.
func (k *KernelImpl) signalTreeRecursive(proc *Process, sig types.Signal, affected *int) {
	state := proc.GetState()
	if state == types.StateZombie || state == types.StateDead {
		return
	}

	// Signal this process (reuse Kill which handles all dispatch logic)
	if err := k.Kill(proc.PID, sig); err == nil {
		*affected++
	}

	// Recurse into children
	children := proc.GetChildren()
	for _, childPID := range children {
		childProc, ok := k.GetProcess(childPID)
		if !ok {
			continue
		}
		k.signalTreeRecursive(childProc, sig, affected)
	}
}

// SigBlock blocks a signal for the target process.
func (k *KernelImpl) SigBlock(pid types.PID, sig types.Signal) error {
	start := time.Now()

	if !sig.Valid() {
		return NewSyscallError("SigBlock", pid, "",
			fmt.Errorf("invalid signal: %d", sig), types.ErrInvalid)
	}

	if !sig.Blockable() {
		return NewSyscallError("SigBlock", pid, "",
			fmt.Errorf("signal %s cannot be blocked", sig), types.ErrInvalid)
	}

	proc, ok := k.GetProcess(pid)
	if !ok {
		return NewSyscallError("SigBlock", pid, "",
			fmt.Errorf("process not found"), types.ErrNotFound)
	}

	state := proc.GetState()
	if state == types.StateZombie || state == types.StateDead {
		return NewSyscallError("SigBlock", pid, "",
			fmt.Errorf("process %d is %s", pid, state), types.ErrNotFound)
	}

	proc.BlockSignal(sig)

	k.emitEvent(proc, "SigBlock", map[string]any{
		"pid":    pid,
		"signal": sig.String(),
	}, nil, nil, time.Since(start))

	return nil
}

// SigUnblock unblocks a signal for the target process.
// If there was a pending signal of this type, it is immediately delivered.
func (k *KernelImpl) SigUnblock(pid types.PID, sig types.Signal) error {
	start := time.Now()

	if !sig.Valid() {
		return NewSyscallError("SigUnblock", pid, "",
			fmt.Errorf("invalid signal: %d", sig), types.ErrInvalid)
	}

	proc, ok := k.GetProcess(pid)
	if !ok {
		return NewSyscallError("SigUnblock", pid, "",
			fmt.Errorf("process not found"), types.ErrNotFound)
	}

	state := proc.GetState()
	if state == types.StateZombie || state == types.StateDead {
		return NewSyscallError("SigUnblock", pid, "",
			fmt.Errorf("process %d is %s", pid, state), types.ErrNotFound)
	}

	hasPending := proc.UnblockSignal(sig)

	k.emitEvent(proc, "SigUnblock", map[string]any{
		"pid":         pid,
		"signal":      sig.String(),
		"had_pending": hasPending,
	}, nil, nil, time.Since(start))

	// If there was a pending signal, deliver it now
	if hasPending {
		proc.ClearPending(sig)
		return k.Signal(pid, sig)
	}

	return nil
}
