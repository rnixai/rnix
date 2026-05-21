package kernel

import (
	gocontext "context"
	"fmt"
	"log"
	"time"

	"github.com/rnixai/rnix/internal/types"
)

// Compile-time interface conformance check.
// SubtreeManager is declared in kernel/kernel.go alongside other manager
// interfaces; the assertion here makes the implementation contract local
// to the file that defines the methods.
var _ SubtreeManager = (*KernelImpl)(nil)

// SuspendSubtree is the public entry point for AC#1: SIGPAUSE semantics
// expressed as a single state-machine transition over the target subtree.
// It exists so callers that already know they want subtree semantics can
// avoid the Signal layer entirely.
func (k *KernelImpl) SuspendSubtree(rootPID types.PID) (int, error) {
	root, ok := k.GetProcess(rootPID)
	if !ok {
		return 0, NewSyscallError("SuspendSubtree", rootPID, "",
			fmt.Errorf("process not found"), types.ErrNotFound)
	}
	return k.suspendSubtree(root)
}

// ResumeSubtree is the public entry point for AC#3.
//
// Concurrency: holds resumeMu for the duration of the walk so that
// ResumeSubtree and ResumeWithOpts cannot interleave their state transitions
// (review red-line — see Epic 44.1 dev-notes "resumeMu & concurrency").
func (k *KernelImpl) ResumeSubtree(rootPID types.PID) (int, int, error) {
	if rootPID == 0 {
		return 0, 0, NewSyscallError("ResumeSubtree", rootPID, "",
			fmt.Errorf("invalid PID 0"), types.ErrInvalid)
	}

	k.resumeMu.Lock()
	defer k.resumeMu.Unlock()

	root, ok := k.GetProcess(rootPID)
	if !ok {
		return 0, 0, NewSyscallError("ResumeSubtree", rootPID, "",
			fmt.Errorf("process not found"), types.ErrNotFound)
	}

	pids := k.collectSubtreePIDs(root)
	var affected, skipped int

	for _, pid := range pids {
		proc, ok := k.GetProcess(pid)
		if !ok {
			// Process was reaped between collection and processing — counts
			// as skipped per AC#7's "BreaksOnReapedDescendant" semantics.
			skipped++
			continue
		}
		state := proc.GetState()
		switch state {
		case types.StateSuspended:
			if err := k.resumeOneForSubtree(proc); err != nil {
				log.Printf("[subtree] resume pid=%d failed: %v", pid, err)
				skipped++
				continue
			}
			affected++
		case types.StateDead:
			// AC#2: emit skipped_dead / skipped_failed depending on exit code.
			isFailed := false
			proc.mu.Lock()
			if proc.Exit != nil && proc.Exit.Code != 0 {
				isFailed = true
			}
			proc.mu.Unlock()
			k.emitResumeSkipped(proc, isFailed, state)
			skipped++
		case types.StateZombie:
			k.emitResumeSkipped(proc, false, state)
			skipped++
		case types.StateRunning:
			// Already running — no-op for state but still counted as skipped.
			k.emitResumeSkipped(proc, false, state)
			skipped++
		default:
			skipped++
		}
	}
	return affected, skipped, nil
}

// suspendSubtree is the internal worker shared by SuspendSubtree (public API)
// and defaultSignalAction (Signal SIGPAUSE entry). It walks the subtree
// rooted at root and transitions every Running node to Suspended with
// reason="user_paused".
//
// Returning a non-nil error is reserved for kernel-level failures — per-node
// errors (e.g. racy state transitions) are logged and skipped to keep the
// fan-out best-effort, matching the "subtree resume tolerates a reaped
// descendant" contract on the reverse side.
func (k *KernelImpl) suspendSubtree(root *Process) (int, error) {
	pids := k.collectSubtreePIDs(root)
	affected := 0
	for _, pid := range pids {
		proc, ok := k.GetProcess(pid)
		if !ok {
			continue
		}
		if proc.GetState() != types.StateRunning {
			continue
		}
		if err := k.suspendOneForSubtree(proc, "user_paused"); err != nil {
			log.Printf("[subtree] suspend pid=%d failed: %v", pid, err)
			continue
		}
		affected++
	}
	return affected, nil
}

// collectSubtreePIDs returns root.PID followed by every descendant PID
// reachable through GetChildren, in pre-order. It does NOT filter on state
// (the caller is expected to inspect each node).
func (k *KernelImpl) collectSubtreePIDs(root *Process) []types.PID {
	if root == nil {
		return nil
	}
	pids := make([]types.PID, 0, 8)
	visited := make(map[types.PID]struct{})
	var visit func(p *Process)
	visit = func(p *Process) {
		if _, seen := visited[p.PID]; seen {
			return
		}
		visited[p.PID] = struct{}{}
		pids = append(pids, p.PID)
		for _, cpid := range p.GetChildren() {
			cp, ok := k.GetProcess(cpid)
			if !ok {
				// Dangling child reference (reaped descendant) — tolerated.
				continue
			}
			visit(cp)
		}
	}
	visit(root)
	return pids
}

// suspendOneForSubtree transitions a single Running process to Suspended
// using the caller-supplied reason. It reuses the existing suspendProcess
// helper so the FD close + Suspend event + callback wiring stay in one place,
// while letting the subtree path use the canonical "user_paused" reason
// instead of k.Suspend's "user_suspended" string.
func (k *KernelImpl) suspendOneForSubtree(proc *Process, reason string) error {
	// Defensive idempotency — a racy state change between collectSubtreePIDs
	// and this call must not surface as an illegal-transition error.
	if proc.GetState() != types.StateRunning {
		return nil
	}

	proc.suspendRequested.Store(true)
	exit := ExitStatus{Code: ExitSuspended, Reason: "suspended: " + reason}
	proc.mu.Lock()
	proc.SuspendReason = reason
	proc.Exit = &exit
	proc.mu.Unlock()

	// Cancel ctx and wait for any reasonStep goroutine to exit. For test
	// fixtures and script-runners the wg is zero so Wait() returns immediately.
	proc.Cancel()
	proc.wg.Wait()

	return k.suspendProcess(proc, reason, ExitSuspended)
}

// resumeOneForSubtree transitions a single Suspended process back to Running
// and, when the process has a PrimaryDevice (i.e. is reasonStep-driven, not a
// script-runner or test fixture), re-opens the LLM device FD and restarts the
// reasoning loop. Script-runner resume is handed off to Story 44.2 — for
// those processes we only transition state and let the IPC layer drive the
// script forward.
func (k *KernelImpl) resumeOneForSubtree(proc *Process) error {
	if err := proc.Unsuspend(); err != nil {
		return fmt.Errorf("unsuspend: %w", err)
	}
	proc.SetSuspendReason("")
	k.emitEvent(proc, "Resume", map[string]any{
		"pid":    proc.PID,
		"action": "resumed_subtree",
	}, nil, nil, 0)

	// Script-runners and test fixtures have no PrimaryDevice; nothing to
	// restart. The caller (44.2 in the script-runner case) is responsible
	// for continuing execution.
	if proc.PrimaryDevice == "" {
		return nil
	}

	// Rebuild the cancelable ctx (Suspend cancelled the old one).
	if proc.cancel != nil {
		proc.cancel()
	}
	gctx, cancel := gocontext.WithCancel(gocontext.Background())
	proc.mu.Lock()
	proc.ctx = gctx
	proc.cancel = cancel
	proc.LastHeartbeat = time.Now()
	proc.mu.Unlock()

	// Re-open LLM device FD (the old one was closed by suspendProcess).
	llmFD, openErr := k.openLLMDeviceForResume(proc, proc.PrimaryDevice)
	if openErr != nil {
		return fmt.Errorf("reopen llm device %q: %w", proc.PrimaryDevice, openErr)
	}
	proc.FDTable[llmFD] = nil
	k.setupDriverStreamHandler(proc, llmFD)

	spawnOpts := SpawnOpts{Model: proc.Model, StartStep: proc.ResumedFromStep}
	proc.wg.Go(func() {
		defer func() { _ = k.vfs.CloseAll(proc.PID) }()
		k.reasonStep(proc, llmFD, spawnOpts)
	})
	return nil
}

// emitResumeSkipped records why a descendant was passed over during
// ResumeSubtree. The shape of the args map is part of the contract observed
// by ATDD tests (skipped_dead / skipped_failed boolean flags) and by future
// Dashboard renderers.
func (k *KernelImpl) emitResumeSkipped(proc *Process, isFailed bool, state types.ProcessState) {
	args := map[string]any{
		"pid":    proc.PID,
		"action": "skipped",
		"reason": state.String(),
	}
	switch {
	case isFailed:
		args["skipped_failed"] = true
	case state == types.StateDead:
		args["skipped_dead"] = true
	case state == types.StateZombie:
		args["skipped_zombie"] = true
	case state == types.StateRunning:
		args["skipped_running"] = true
	}
	k.emitEvent(proc, "Resume", args, nil, nil, 0)
}
