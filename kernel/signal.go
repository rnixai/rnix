package kernel

import (
	"fmt"
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
			// Kill the suspended process directly
			if err := proc.Transition(types.StateDead); err != nil {
				return NewSyscallError("Signal", pid, "", err, types.ErrInternal)
			}
			proc.mu.Lock()
			proc.Exit = &ExitStatus{Code: 1, Reason: "signal while suspended"}
			proc.DeadAt = time.Now()
			proc.mu.Unlock()
			k.emitEvent(proc, "Signal", map[string]any{
				"pid":    pid,
				"signal": sig.String(),
				"action": "killed_suspended",
			}, nil, nil, time.Since(start))
			k.reapSuspendedProcess(proc)
			return nil
		}
		// Non-termination signals on suspended: ignored (SIGPAUSE redundant, SIGRESUME for 30.4)
		k.emitEvent(proc, "Signal", map[string]any{
			"pid":    pid,
			"signal": sig.String(),
			"action": "noop_suspended",
		}, nil, nil, time.Since(start))
		return nil
	}

	action := k.deliverSignal(proc, sig)

	k.emitEvent(proc, "Signal", map[string]any{
		"pid":    pid,
		"signal": sig.String(),
		"action": action,
	}, nil, nil, time.Since(start))

	return nil
}

// deliverSignal performs the actual signal dispatch logic.
// Uses resolveSignalDisposition for atomic check of blocked/handler/default
// under a single lock hold, preventing TOCTOU races.
// SIGKILL always uses default behavior (cannot be blocked or handled).
// Returns the action string describing what happened.
func (k *KernelImpl) deliverSignal(proc *Process, sig types.Signal) string {
	disp, handler := proc.resolveSignalDisposition(sig)
	switch disp {
	case dispBlocked:
		return "blocked_pending"
	case dispHandler:
		handler(sig)
		return "handler"
	default:
		return k.defaultSignalAction(proc, sig)
	}
}

// defaultSignalAction executes the default behavior for a signal.
func (k *KernelImpl) defaultSignalAction(proc *Process, sig types.Signal) string {
	switch {
	case sig.IsTermination():
		proc.Cancel()
		return "terminated"
	case sig == types.SIGPAUSE:
		proc.Pause()
		return "paused"
	case sig == types.SIGRESUME:
		proc.Resume()
		return "resumed"
	default:
		return "ignored"
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
