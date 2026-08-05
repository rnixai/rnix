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

// subtreeMaxDepth bounds the recursion depth in collectSubtreePIDs. The old
// reactivateCliDisconnectedAncestors used the same value as a defensive cap;
// keeping it here protects against pathological trees and stack overflow on
// hosts with small Go stacks.
const subtreeMaxDepth = 32

// SuspendSubtree is the public entry point for AC#1: SIGPAUSE semantics
// expressed as a single state-machine transition over the target subtree.
//
// Concurrency: holds resumeMu for the duration of the walk, symmetric with
// ResumeSubtree. Without this, a concurrent ResumeSubtree on the same tree
// can interleave per-node transitions and leave the subtree in a mixed
// Running/Suspended state.
func (k *KernelImpl) SuspendSubtree(rootPID types.PID) (int, error) {
	root, ok := k.GetProcess(rootPID)
	if !ok {
		return 0, NewSyscallError("SuspendSubtree", rootPID, "",
			fmt.Errorf("process not found"), types.ErrNotFound)
	}

	k.resumeMu.Lock()
	defer k.resumeMu.Unlock()

	return k.suspendSubtree(root)
}

// ResumeSubtree is the public entry point for AC#3.
//
// Concurrency: holds resumeMu for the duration of the walk so that
// ResumeSubtree, SuspendSubtree and ResumeWithOpts cannot interleave their
// state transitions (review red-line — see Epic 44.1 dev-notes "resumeMu &
// concurrency").
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

	return k.resumeSubtreeLocked(root)
}

// resumeSubtreeLocked is the inner worker that performs the ResumeSubtree
// walk. Callers MUST already hold k.resumeMu. This split lets ResumeWithOpts
// (Story 44.3 AC#4) trigger subtree wakeup from inside its own
// resumeMu-locked critical section without re-acquiring the lock and
// deadlocking.
func (k *KernelImpl) resumeSubtreeLocked(root *Process) (int, int, error) {
	pids := k.collectSubtreePIDs(root)
	var affected, skipped int
	allAlreadyRunning := true

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
			allAlreadyRunning = false
			if err := k.resumeOneForSubtree(proc); err != nil {
				log.Printf("[subtree] resume pid=%d failed: %v", pid, err)
				skipped++
				continue
			}
			affected++
		case types.StateDead:
			allAlreadyRunning = false
			// AC#2: emit skipped_dead / skipped_failed depending on exit code.
			isFailed := false
			exitUnknown := false
			proc.mu.Lock()
			switch {
			case proc.Exit == nil:
				exitUnknown = true
			case proc.Exit.Code != 0:
				isFailed = true
			}
			proc.mu.Unlock()
			k.emitResumeSkipped(proc, isFailed, exitUnknown, state)
			skipped++
		case types.StateZombie:
			allAlreadyRunning = false
			k.emitResumeSkipped(proc, false, false, state)
			skipped++
		case types.StateRunning:
			// Already running. Suppress per-node Resume(skipped) events when
			// the ENTIRE subtree is already Running (review F15: avoid event
			// spam from nested ResumeSubtree(parent) → ResumeSubtree(child)).
			// We emit a single rollup event below if that turns out to be the
			// case; for now just count.
			skipped++
		default:
			allAlreadyRunning = false
			skipped++
		}
	}

	if affected == 0 && allAlreadyRunning && len(pids) > 0 {
		// Whole-subtree-already-Running: emit one rollup noop instead of
		// noise from nested ResumeSubtree(parent) → ResumeSubtree(child)
		// chains where the inner call sees every node Running.
		k.emitEvent(root, "Resume", map[string]any{
			"pid":     root.PID,
			"action":  "noop_all_running",
			"skipped": skipped,
		}, nil, nil, 0)
	}
	// Dead / Zombie / Failed already emitted distinguished events via
	// emitResumeSkipped; already-Running nodes intentionally emit nothing so
	// they do not flood the timeline pane.

	return affected, skipped, nil
}

// suspendSubtree is the internal worker shared by SuspendSubtree (public API)
// and defaultSignalAction (Signal SIGPAUSE entry). It walks the subtree
// rooted at root and transitions every Running node to Suspended with
// reason="user_paused".
//
// A second-pass scan catches descendants that were spawned during the first
// pass — without this, a fork between collectSubtreePIDs and the per-node
// Suspend leaves the new child Running while its parent is Suspended,
// violating the "subtree paused" invariant the user expects.
func (k *KernelImpl) suspendSubtree(root *Process) (int, error) {
	totalAffected := 0
	const maxPasses = 3 // bounded to avoid pathological fork loops
	for range maxPasses {
		pids := k.collectSubtreePIDs(root)
		passAffected := 0
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
			passAffected++
		}
		totalAffected += passAffected
		if passAffected == 0 {
			break // converged: no new Running descendants
		}
	}
	return totalAffected, nil
}

// collectSubtreePIDs returns root.PID followed by every descendant PID
// reachable through GetChildren, in pre-order. When a child PID is no longer
// in procTable (already reaped) we fall back to procHistory to look up its
// UUID, then scan the live procTable for grandchildren whose ParentUUID
// matches — this prevents a reaped intermediate from stranding deeper
// descendants outside the subtree walk.
func (k *KernelImpl) collectSubtreePIDs(root *Process) []types.PID {
	if root == nil {
		return nil
	}
	pids := make([]types.PID, 0, 8)
	visited := make(map[types.PID]struct{})

	var visit func(p *Process, depth int)
	visit = func(p *Process, depth int) {
		if _, seen := visited[p.PID]; seen {
			return
		}
		if depth > subtreeMaxDepth {
			log.Printf("[subtree] depth bound %d hit at pid=%d; truncating walk",
				subtreeMaxDepth, p.PID)
			return
		}
		visited[p.PID] = struct{}{}
		pids = append(pids, p.PID)
		for _, cpid := range p.GetChildren() {
			if cp, ok := k.GetProcess(cpid); ok {
				visit(cp, depth+1)
				continue
			}
			// Dangling child reference (reaped descendant). Try to recover
			// living grandchildren via ParentUUID lookup in procHistory →
			// procTable.
			if k.procHistory == nil {
				continue
			}
			pi := k.procHistory.FindByPID(cpid)
			if pi == nil || pi.UUID == "" {
				continue
			}
			k.procTable.Range(func(_ types.PID, gc *Process) bool {
				if gc.ParentUUID == pi.UUID {
					visit(gc, depth+1)
				}
				return true
			})
		}
	}
	visit(root, 0)
	return pids
}

// suspendOneForSubtree transitions a single Running process to Suspended
// using the caller-supplied reason. suspendProcess is the sole writer of
// SuspendReason/Exit — we set suspendRequested (atomic, observed by the
// reasonStep defer path) and then cancel + wait so the reasoning goroutine
// observes the flag and exits cleanly. The eventual resume reads
// LastCompletedStep on the resume leg (resumeOneForSubtree fallback at
// subtree.go:343-348) rather than pre-filling ResumedFromStep here —
// Story 44.5 AC2 removed that pre-fill because writing ResumedFromStep on
// the pause leg made the field meaningless on failed-exit snapshots
// (state=dead with a non-zero resumed_from_step that no resume ever ran).
func (k *KernelImpl) suspendOneForSubtree(proc *Process, reason string) error {
	// Defensive idempotency — a racy state change between collectSubtreePIDs
	// and this call must not surface as an illegal-transition error.
	if proc.GetState() != types.StateRunning {
		return nil
	}

	proc.suspendRequested.Store(true)

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
//
// Failure mode: if the FD re-open fails, we roll back the Unsuspend so the
// process does not end up Running-but-undriven (which would cause the
// heartbeat monitor to escalate stall recovery indefinitely).
func (k *KernelImpl) resumeOneForSubtree(proc *Process) error {
	preSnap := proc.GetDetailSnapshot()
	log.Printf("[resume-trace] resumeOneForSubtree enter pid=%d uuid=%s state=%s tokens=%d ctx_id=%d",
		preSnap.PID, preSnap.UUID, preSnap.State, preSnap.TokensUsed, preSnap.CtxID)
	defer func() {
		postSnap := proc.GetDetailSnapshot()
		log.Printf("[resume-trace] resumeOneForSubtree exit  pid=%d uuid=%s state=%s tokens=%d ctx_id=%d",
			postSnap.PID, postSnap.UUID, postSnap.State, postSnap.TokensUsed, postSnap.CtxID)
	}()
	if err := proc.Unsuspend(); err != nil {
		return fmt.Errorf("unsuspend: %w", err)
	}
	proc.SetSuspendReason("")
	// Story 73.3 / D7 — clear the quota wake instant alongside SuspendReason.
	// The quota wake scanner's re-check (Suspended + quota_exhausted +
	// ResumeAt due) then never matches an already-woken process, so there is
	// no double wake; the next SaveProcInfo erases the stale value from disk
	// via omitempty.
	proc.SetResumeAt(time.Time{})

	// Story 44.5 v2 — close the pausedAt → pausedTotal accounting cycle that
	// suspendProcess opened. Mirror of SoftPause Resume() (kernel/process.go:754).
	// Epic 44.1's HardSuspend rewrite did not migrate this accumulation, so
	// Dashboard tree render.go's `(PausedTotal subtract)` branch never had a
	// non-zero PausedTotal to subtract and elapsed-time kept advancing with
	// wall clock during the Suspended interval.
	proc.mu.Lock()
	if !proc.pausedAt.IsZero() {
		proc.pausedTotal += time.Since(proc.pausedAt)
		proc.pausedAt = time.Time{}
	}
	// Story 73.3 / AC8 — drop the stale suspend ExitStatus: suspendProcess set
	// it on the suspend leg, and leaving it on the now-Running process makes
	// GetProcInfo report a non-empty ExitReason, violating the invariant
	// matrix's Running row (the 73.3 guardrail test pins this; the leak
	// pre-existed for every in-place wake reason). Safe: the next suspend
	// cycle stamps a fresh Exit before any reader (notifySuspendDone) runs.
	proc.Exit = nil
	proc.mu.Unlock()

	// Story 44.5 AC8 — drain any stale ExitSuspended event left in proc.Done
	// by notifySuspendDone during the previous Suspend cycle. proc.Done is
	// cap=1 with non-blocking sends (see kernel/process.go:242 + select-default
	// pattern in notifySuspendDone); without this drain, the next reasonStep's
	// finishProcess write would be silently dropped (channel already full),
	// leaving SpawnAndWait blocked on a dry channel and the script-runner
	// hung forever on a successfully-resumed child.
	select {
	case <-proc.Done:
	default:
	}

	// Story 44.3 AC#2 — persist proc-info.json on the Unsuspend leg so disk
	// no longer carries the stale state=suspended + suspend_reason after a
	// dashboard R / SIGRESUME. Done in the kernel layer rather than inside
	// Process.Unsuspend() to keep the Process → Kernel one-way dependency
	// intact (策略 A from Story 44.3 §Tasks/2.2). Best-effort.
	if info, ierr := k.GetProcInfo(proc.PID); ierr == nil && info != nil {
		if perr := SaveProcInfo(k.ResolveStepBaseDir(proc), *info); perr != nil {
			log.Printf("[subtree] proc-info.json write error pid=%d uuid=%s: %v",
				proc.PID, proc.UUID, perr)
		}
	}

	// Script-runners and test fixtures have no PrimaryDevice; nothing to
	// restart. The caller (44.2 in the script-runner case) is responsible
	// for continuing execution. Emit Resume event so consumers see the
	// transition, but do NOT touch LastHeartbeat — without a driver writing
	// heartbeats the monitor would otherwise escalate to stall recovery.
	if proc.PrimaryDevice == "" {
		// Defensive ctx patch for the LoadSuspendedFromDisk → ResumeSubtree
		// path: the placeholder restored from disk has proc.ctx set to a
		// PID-less Background ctx, and this branch returns without
		// reconstructing ctx, so any later /dev/tty Write driven by the
		// script-runner would read PID=0 from ctx and trip extractAskUserPID's
		// guard. Layer ContextWithPID onto the existing ctx — cheaper than
		// rebuilding cancel/cancelFunc and safe even when ctx already has the
		// key (context.WithValue treats it as a shadow, not a mutation).
		if proc.ctx != nil {
			proc.mu.Lock()
			proc.ctx = ContextWithPID(proc.ctx, proc.PID)
			proc.mu.Unlock()
		}
		k.emitEvent(proc, "Resume", map[string]any{
			"pid":    proc.PID,
			"action": "awaiting_script_driver",
		}, nil, nil, 0)
		return nil
	}

	// Snapshot the old cancel under lock, replace with a fresh ctx, then
	// invoke the old cancel outside the lock. This avoids reading proc.cancel
	// without the mutex (the read+invoke pattern races any concurrent writer).
	gctx, cancel := gocontext.WithCancel(gocontext.Background())
	gctx = ContextWithPID(gctx, proc.PID)
	proc.mu.Lock()
	oldCancel := proc.cancel
	proc.ctx = gctx
	proc.cancel = cancel
	proc.LastHeartbeat = time.Now()
	proc.mu.Unlock()
	if oldCancel != nil {
		oldCancel()
	}

	// Re-open LLM device FD (the old one was closed by suspendProcess).
	llmFD, openErr := k.openLLMDeviceForResume(proc, proc.PrimaryDevice)
	if openErr != nil {
		// FD reopen failed — the process can no longer be driven. Finish it to a
		// terminal state (Running→Zombie, writes a terminal Done) instead of
		// re-Suspending. suspendProcess never writes proc.Done (only reasonStep's
		// defer → notifySuspendDone does, and no reasonStep goroutine is running
		// yet here), so a parent blocked in WaitChildInReason — which keys off
		// child.GetState() — would hang forever on a Suspended child that yields
		// no terminal Done. finishProcess is wg.Wait-free, safe to call from this
		// non-reasonStep goroutine (mirrors supervisor.go). child → Dead honours
		// the Resume philosophy: data is preserved, user can `rnix resume <uuid>`.
		k.emitEvent(proc, "Resume", map[string]any{
			"pid":    proc.PID,
			"action": "resume_failed",
			"error":  openErr.Error(),
		}, nil, openErr, 0)
		k.finishProcess(proc, ExitStatus{Code: 1, Reason: "resume_failed"})
		return fmt.Errorf("reopen llm device %q: %w", proc.PrimaryDevice, openErr)
	}
	proc.FDTable[llmFD] = nil
	k.setupDriverStreamHandler(proc, llmFD)

	// Use ResumedFromStep if set by suspendOneForSubtree (in-memory pause), or
	// LastCompletedStep+1 as a fallback for processes that were Suspended by
	// some other path (e.g. heartbeat stall → k.Suspend → SIGRESUME).
	startStep := proc.ResumedFromStep
	if startStep == 0 {
		if last := proc.LastCompletedStep.Load(); last > 0 {
			startStep = int(last) + 1
		}
	}
	spawnOpts := SpawnOpts{Model: proc.Model, StartStep: startStep}
	proc.wg.Go(func() {
		defer func() { _ = k.vfs.CloseAll(proc.PID) }()
		k.reasonStep(proc, llmFD, spawnOpts)
	})

	// Emit Resume event AFTER the goroutine is queued so observers never see
	// "resumed_subtree" before the reasoning loop is actually live.
	k.emitEvent(proc, "Resume", map[string]any{
		"pid":    proc.PID,
		"action": "resumed_subtree",
	}, nil, nil, 0)
	return nil
}

// emitResumeSkipped records why a descendant was passed over during
// ResumeSubtree. The shape of the args map is part of the contract observed
// by ATDD tests (skipped_dead / skipped_failed boolean flags) and by future
// Dashboard renderers. exitUnknown==true distinguishes "Dead with no Exit
// payload" (likely a race between Terminate and Transition) from a normal
// completion for forensic clarity.
func (k *KernelImpl) emitResumeSkipped(proc *Process, isFailed, exitUnknown bool, state types.ProcessState) {
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
	if exitUnknown {
		args["exit_unknown"] = true
	}
	k.emitEvent(proc, "Resume", args, nil, nil, 0)
}
