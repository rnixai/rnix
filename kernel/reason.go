package kernel

import (
	gocontext "context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/debug"
	"github.com/rnixai/rnix/drivers/llm"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// attachStepObservation initialises the per-process step/event writers and
// resolves stepsDir / scratchDir + checkpointErrCh on disk. Safe to call
// repeatedly: a subsequent invocation is a no-op when stepWriter is already
// set, so resume / load_suspended (which need writers attached BEFORE they
// emit Resurrect / ResumeFromHistory / Mount) and reasonStep (the legacy
// attachment site for fresh-spawn paths) can both invoke it without racing.
//
// Story 48.1 code-review P1 — before this helper existed, only reasonStep
// attached the writer, so the resume hot path emitted its events into a nil
// writer and the [event-drop] warning fired. LoadSuspendedFromDisk was
// even worse: Suspended placeholders never run reasonStep, so Resurrect
// + Mount events were permanently dropped from events.jsonl.
func (k *KernelImpl) attachStepObservation(proc *Process) {
	if proc == nil {
		return
	}
	proc.mu.Lock()
	already := proc.stepWriter != nil && proc.eventWriter != nil && proc.stepsDir != ""
	proc.mu.Unlock()
	if already {
		return
	}

	stepBaseDir := k.ResolveStepBaseDir(proc)
	if stepBaseDir == "" {
		log.Printf("[observation] no step base dir for pid=%d uuid=%s — step/event data will not be recorded", proc.PID, proc.UUID)
		return
	}

	proc.mu.Lock()
	if proc.stepWriter == nil {
		if sw, err := NewStepWriter(stepBaseDir, proc.UUID); err == nil {
			proc.stepWriter = sw
		} else {
			log.Printf("[observation] step writer creation failed pid=%d: %v", proc.PID, err)
		}
	}
	if proc.eventWriter == nil {
		if ew, err := NewEventWriter(stepBaseDir, proc.UUID); err == nil {
			proc.eventWriter = ew
		} else {
			log.Printf("[observation] event writer creation failed pid=%d: %v", proc.PID, err)
		}
	}
	// Review patch P7: attach RawWriter on resume / LoadSuspendedFromDisk
	// paths so CAP-4 default-on holds for revived processes (spawn.go has
	// the equivalent auto-attach for fresh spawns). Lazy file creation
	// (review patch P1) means no empty raw.jsonl is created if no driver
	// ever opts in.
	if proc.rawWriter == nil && k.RawCaptureCfg().Enabled {
		if rw, err := NewRawWriter(stepBaseDir, proc.UUID); err == nil {
			proc.rawWriter = rw
		} else {
			log.Printf("[observation] raw writer creation failed pid=%d: %v", proc.PID, err)
		}
	}
	if proc.stepsDir == "" {
		proc.stepsDir = filepath.Join(stepBaseDir, "steps", proc.UUID)
	}
	if proc.scratchDir == "" {
		proc.scratchDir = filepath.Join(stepBaseDir, "scratch", proc.UUID)
	}
	if proc.checkpointErrCh == nil {
		proc.checkpointErrCh = make(chan error, 1)
	}
	scratch := proc.scratchDir
	proc.mu.Unlock()

	_ = os.MkdirAll(scratch, 0o755)
}

// finishProcess terminates the process and writes the exit status to the Done channel.
func (k *KernelImpl) finishProcess(proc *Process, exit ExitStatus) {
	proc.mu.Lock()
	// Story 66.6: a step killed / interrupted / write-failed before its normal
	// boundary never reached reason.go's authoritative accounting, so its
	// mid-stream usage still sits in StreamTokensUsed. Fold it into TokensUsed as
	// the last-known value — there is no authoritative resp at this point, so
	// there is no double-count window. Everything below that reads TokensUsed
	// (reputation, synergy, GetProcInfo → proc-info.json, immune Finalize via
	// OnProcessExit) benefits with zero changes. Normal completion already
	// cleared StreamTokensUsed at the last step boundary, so this is a no-op there.
	if proc.StreamTokensUsed > 0 {
		proc.TokensUsed += proc.StreamTokensUsed
		proc.StreamTokensUsed = 0
		proc.StreamInputTokens = 0
	}
	// Story 48.1 code-review P2/P4 — mount lifetime is now ref-counted in
	// the MountManager (see vfs/mount.go). finishProcess always releases
	// ONE reference per path the process held (whether obtained via the
	// original Mount or via Acquire on the resume / fork path); the real
	// teardown happens only when the last owner releases. The old
	// mcpReusedMounts skip-list became unnecessary and is no longer
	// consulted — the field is still populated by reattachMCPMounts for
	// the Mount event's reused=true marker.
	mcpMounts := append([]string(nil), proc.MCPMounts...)
	// Backfill Result from exit reason when empty — ensures Dashboard always has
	// diagnostic info for failed processes (e.g., empty LLM response, context cancelled).
	if proc.Result == "" && exit.Code != 0 && exit.Reason != "" {
		proc.Result = exit.Reason
	}
	// F4: surface the process's actual output on the ExitStatus so the intent
	// reconciler can inject a child's real result into its dependents' context
	// (rnix-eval mcp/hello-mcp), instead of only the exit reason. proc.Result
	// holds the `complete` action's output (or the backfilled reason above).
	exit.Result = proc.Result
	// Story 44.5 AC3 — failed exits clear stale SuspendReason. The
	// pause-during-LLM-Write race (Story 44.5 AC1) is the primary case where
	// suspendProcess has already stamped SuspendReason before reasonStep
	// races it to a kill path; without this clear the persisted proc-info.json
	// shows the contradiction (state=dead + suspend_reason=user_paused +
	// exit_reason="llm write failed"). AC1 is the primary defence; this is the
	// tuple-invariant safety net for any future code path that hits the same
	// race (Story 44.5 AC6 ValidateProcInfoInvariant enforces it).
	if exit.Code != 0 {
		proc.SuspendReason = ""
	}
	proc.mu.Unlock()

	// Story 51.4 AR-1: all template-named processes feed reputation store.
	if k.reputationStoreForStem != nil && proc.AgentTemplate != "" {
		slaResult := &SLAResult{
			AgentName:   proc.AgentTemplate,
			Passed:      exit.Code == 0,
			TokensUsed:  proc.TokensUsed,
			DurationMs:  time.Since(proc.CreatedAt).Milliseconds(),
			EvaluatedAt: time.Now(),
		}
		if err := k.reputationStoreForStem.RecordResult(proc.AgentTemplate, slaResult); err != nil {
			log.Printf("[warn] finishProcess pid=%d: RecordResult(%s) failed: %v", proc.PID, proc.AgentTemplate, err)
		}
	}

	// Story 51.4 EM-1: all processes with skills feed synergy matrix.
	if k.synergyMatrixForStem != nil && len(proc.Skills) > 0 {
		if err := k.synergyMatrixForStem.RecordCombo(SynergyRecord{
			ComboKey:   NewComboKey(proc.Skills),
			Skills:     proc.Skills,
			Passed:     exit.Code == 0,
			TokensUsed: proc.TokensUsed,
			DurationMs: time.Since(proc.CreatedAt).Milliseconds(),
			Timestamp:  time.Now(),
		}); err != nil {
			log.Printf("[warn] finishProcess pid=%d: RecordCombo failed: %v", proc.PID, err)
		}
	}

	if k.mountMgr != nil {
		for _, mountPath := range mcpMounts {
			unmountStart := time.Now()
			err := k.mountMgr.Unmount(mountPath)
			duration := time.Since(unmountStart)
			args := map[string]any{
				"path":        mountPath,
				"auto":        true,
				"duration_ms": duration.Milliseconds(),
			}
			// Story 48.2 AC7: annotate forced-kill Unmount events so the
			// dashboard timeline can surface "SIGTERM was ignored, escalated
			// to SIGKILL after 5s". Two detection paths:
			//   (a) primary  — DriverError.Code == ErrForceKilled, set by
			//       drivers/mcp/transport.Close when escalation occurred.
			//       Code review F1 (2026-05-28) restored this as the real
			//       primary by making vfs/mount.go::Unmount propagate the
			//       Close return value.
			//   (b) fallback — duration ≥ gracefulShutdownTimeout - 100ms AND
			//       err != nil. The err != nil guard (code review F3)
			//       prevents marking a graceful Unmount as forced on slow
			//       machines / GC pauses where the wall clock approaches 5s
			//       even though SIGTERM was honoured.
			forced := false
			var dErr *types.DriverError
			if errors.As(err, &dErr) && dErr.Code == types.ErrForceKilled {
				forced = true
			}
			if !forced && err != nil && duration >= 4900*time.Millisecond {
				forced = true
			}
			if forced {
				args["forced"] = true
				args["reason"] = "sigterm_ignored"
			}
			k.emitEvent(proc, "Unmount", args, nil, err, duration)
		}
	}

	_ = proc.Terminate(exit)
	if info, err := k.GetProcInfo(proc.PID); err == nil {
		if err := SaveProcInfo(k.ResolveStepBaseDir(proc), *info); err != nil {
			log.Printf("[finish] proc-info.json write error pid=%d uuid=%s: %v", proc.PID, proc.UUID, err)
		}
	}

	if k.recordMgr != nil && k.recordMgr.IsRecording(proc.PID) {
		stateEvent := debug.RecordEvent{
			Timestamp: time.Since(proc.CreatedAt),
			PID:       proc.PID,
			Type:      debug.RecordStateChange,
			State: &debug.StateChangeData{
				FromState: types.StateRunning.String(),
				ToState:   types.StateZombie.String(),
				Reason:    exit.Reason,
			},
		}
		if err := k.recordMgr.RecordEvent(proc.PID, stateEvent); err != nil {
			log.Printf("[record] state change error pid=%d: %v", proc.PID, err)
		}
	}

	if k.callbacks != nil {
		if exit.Err != nil {
			k.callbacks.OnError(proc.PID, exit.Err)
		}
		k.callbacks.OnComplete(proc.PID, proc.Result, exit)
	}

	if k.immuneDaemon != nil {
		k.immuneDaemon.OnProcessExit(proc.PID, proc.TokensUsed, exit.Code == 0)
	}

	select {
	case proc.Done <- exit:
	default:
	}

	proc.mu.Lock()
	ppid := proc.PPID
	proc.mu.Unlock()
	if ppid > 0 {
		if _, parentExists := k.GetProcess(ppid); !parentExists {
			select {
			case k.reapCh <- proc.PID:
			default:
				// Fallback when reapCh is full or the reaper has already exited.
				// Track via reaperWg so Shutdown waits for this goroutine before
				// returning — otherwise reapProcess may still be writing
				// process-meta.json after t.TempDir() cleanup runs.
				k.reaperWg.Go(func() {
					k.reapProcess(proc)
				})
			}
		}
	}
}

// isTransientLLMError checks if an error from the LLM driver is transient
// and worth retrying (socket disconnect, overloaded, connection reset, etc.).
func isTransientLLMError(err error) bool {
	// Permanent-error veto: auth/login/context-length/model-not-found are 4xx-class
	// failures that never succeed on retry. Check them first so a provider error
	// body containing words like "timeout" cannot be misclassified as transient
	// by the substring fallbacks below.
	if errors.Is(err, llm.ErrAuth) ||
		errors.Is(err, llm.ErrLoginRequired) ||
		errors.Is(err, llm.ErrContextLength) ||
		errors.Is(err, llm.ErrModelNotFound) {
		return false
	}
	// Check via the llm package's IsTransient (uses sentinel errors including ErrStreamIncomplete)
	if llm.IsTransient(err) {
		return true
	}
	// Story 73.1 / AC1-D3: rate limits left llm.IsTransient when the trichotomy
	// landed, so they must be claimed here explicitly — otherwise a 429 skips
	// the retry path entirely and goes straight to attemptFallback →
	// finishProcess, which is worse than the zero-delay retries it replaced.
	// All three kinds return true here; kind-specific backoff is Story 73.2's
	// work, this only holds the line against regression.
	if llm.IsRateLimited(err) {
		return true
	}
	var llmErr interface{ Unwrap() error }
	if errors.As(err, &llmErr) {
		inner := llmErr.Unwrap()
		if inner != nil {
			if llm.IsTransient(inner) {
				return true
			}
			if llm.IsRateLimited(inner) {
				return true
			}
			lower := strings.ToLower(inner.Error())
			if strings.Contains(lower, "socket") ||
				strings.Contains(lower, "connection") ||
				strings.Contains(lower, "overloaded") ||
				strings.Contains(lower, "eof") ||
				strings.Contains(lower, "reset by peer") ||
				strings.Contains(lower, "timeout") ||
				strings.Contains(lower, "timed out") ||
				strings.Contains(lower, "stream") {
				return true
			}
		}
	}
	// Also match on the outer error string as a fallback. Timeout-flavoured
	// transport failures (e.g. "net/http: TLS handshake timeout" from a flaky
	// relay) are transient by nature; without this they skip the retry path
	// and kill the process outright (7/4 apex case).
	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "socket") ||
		strings.Contains(lower, "connection") ||
		strings.Contains(lower, "overloaded") ||
		strings.Contains(lower, "timeout") ||
		strings.Contains(lower, "timed out") ||
		strings.Contains(lower, "stream ended without result")
}

// attemptFallback tries the fallback provider when primary LLM call fails.
// maxExitReasonDetailBytes caps the driver-error detail merged into an exit
// reason (Story 66.1), mirroring the 200-byte precedent in cli_subagent.go.
const maxExitReasonDetailBytes = 200

// driverErrorDetail extracts the root cause of an LLM write/read failure for
// inclusion in the process exit reason (Story 66.1 / P1b). It unwraps to the
// device error's undecorated cause — *types.DriverError.Err for non-LLM
// devices, *llm.LLMError.Err for LLM drivers (whose Error() prepends a
// redundant "llm [provider] (status N)" that duplicates proc-info's Provider
// field), or *vfs.VFSError.Err as a fallback — then keeps only the first line,
// flattens residual control characters, and truncates to
// maxExitReasonDetailBytes on a UTF-8 boundary.
func driverErrorDetail(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	var de *types.DriverError
	var le *llm.LLMError
	var ve *vfs.VFSError
	switch {
	case errors.As(err, &de) && de.Err != nil:
		msg = de.Err.Error()
	case errors.As(err, &le) && le.Err != nil:
		// Production LLM failures are *llm.LLMError (no LLM driver emits
		// *types.DriverError); its Error() prepends "llm [provider] (status
		// N)" decoration, so take the underlying cause (Story 66.1 D1). Must
		// precede the VFSError case: errors.As(&ve) matches the outer VFSError
		// wrapper and would re-surface the decorated LLMError.Error().
		msg = le.Err.Error()
	case errors.As(err, &ve) && ve.Err != nil:
		// VFS wraps device errors that are neither DriverError nor LLMError
		// (e.g. a raw error surfaced by a mock or a non-driver device); its
		// .Err is the undecorated cause.
		msg = ve.Err.Error()
	}
	// First line only: a \n or a bare \r both terminate it.
	if i := strings.IndexAny(msg, "\n\r"); i >= 0 {
		msg = msg[:i]
	}
	// Flatten residual control chars (tabs, ANSI escapes, ...) so the detail
	// cannot corrupt single-line proc-info.json / daemon-log output — mirrors
	// internal/ui.aggregatePreview (commit a8d42c6).
	msg = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, msg)
	return truncateUTF8Bytes(strings.TrimSpace(msg), maxExitReasonDetailBytes)
}

func (k *KernelImpl) attemptFallback(proc *Process, req llmRequest, primaryDevice string, primaryErr error, step int) ([]byte, error) {
	if proc.FallbackDevice == "" {
		return nil, primaryErr
	}

	fallbackStart := time.Now()

	var fbFD types.FD
	var err error
	if proc.ProjectConfig != nil && proc.ProjectConfig.LLMFileOpener != nil {
		fbProvider := strings.TrimPrefix(proc.FallbackDevice, "/dev/llm/")
		fileAny, openErr := proc.ProjectConfig.LLMFileOpener(fbProvider, int(vfs.O_RDWR))
		if openErr == nil {
			if vfsFile, ok := fileAny.(vfs.VFSFile); ok {
				fbFD = k.vfs.RegisterFD(proc.PID, vfsFile)
			} else {
				log.Printf("[kernel] warning: LLMFileOpener returned non-VFSFile type %T for fallback provider %q", fileAny, fbProvider)
				fbFD, err = k.vfs.Open(proc.PID, proc.FallbackDevice, vfs.O_RDWR)
			}
		} else {
			fbFD, err = k.vfs.Open(proc.PID, proc.FallbackDevice, vfs.O_RDWR)
		}
	} else {
		fbFD, err = k.vfs.Open(proc.PID, proc.FallbackDevice, vfs.O_RDWR)
	}
	if err != nil {
		return nil, primaryErr
	}
	defer func() { _ = k.vfs.Close(proc.PID, fbFD) }()

	// Attach a heartbeat-only stream handler to the fallback FD. The primary
	// llmFD gets the full setupDriverStreamHandler at spawn/subtree/resume time;
	// that handler refreshes proc.LastHeartbeat (via TouchHeartbeat) on every
	// driver stream event. Without an equivalent on the fallback FD, a long but
	// active fallback call (one streaming events that will eventually succeed)
	// leaves LastHeartbeat frozen at the primary-failure instant and triggers
	// spurious HeartbeatMonitor stall warnings.
	//
	// We deliberately do NOT reuse setupDriverStreamHandler here: it also writes
	// driver step records (its closure-local driverStep counter restarts at 0,
	// colliding with the primary handler's monotonic numbering in the same
	// steps.jsonl) and merges the fallback provider's tools into
	// proc.nativeToolDefs. For the transient fallback FD we want heartbeat
	// liveness only — nothing else.
	if obs, ok := k.vfs.GetFile(proc.PID, fbFD).(vfs.StreamObserver); ok {
		obs.SetStreamHandler(func(map[string]any) { proc.TouchHeartbeat() })
	}

	// Only Model is rebound for the fallback provider; req.ReasoningEffort is
	// reused as-is. When fallback crosses providers (e.g. gemini→openai), the
	// effort value is NOT case-converted (gemini wants HIGH, openai wants high) —
	// any mismatch is reported/degraded by the downstream API, consistent with
	// the verbatim-passthrough rule and the Model-only fallback behavior.
	req.Model = proc.FallbackModel
	reqJSON, err := json.Marshal(req)
	if err != nil {
		return nil, primaryErr
	}

	if err := k.vfs.Write(proc.ctx, proc.PID, fbFD, reqJSON); err != nil {
		k.emitEvent(proc, "ReasonStep", map[string]any{
			"step":            step,
			"action":          "fallback_exhausted",
			"primary_device":  primaryDevice,
			"primary_error":   primaryErr.Error(),
			"fallback_device": proc.FallbackDevice,
			"fallback_error":  err.Error(),
		}, nil, err, time.Since(fallbackStart))
		// Story 56.7 (G4): the fallback call is persisted to raw.jsonl whether
		// it succeeds or fails. Explicit calls before each return — the
		// `defer Close(fbFD)` above runs at function return, so the FD is
		// still valid at capture time.
		k.captureRawLLMCall(proc, fbFD, step, err)
		return nil, fmt.Errorf("primary %s: %v; fallback %s: %w", primaryDevice, primaryErr, proc.FallbackDevice, err)
	}

	respData, err := k.vfs.Read(proc.PID, fbFD, 1<<20)
	if err != nil {
		k.emitEvent(proc, "ReasonStep", map[string]any{
			"step":            step,
			"action":          "fallback_exhausted",
			"primary_device":  primaryDevice,
			"primary_error":   primaryErr.Error(),
			"fallback_device": proc.FallbackDevice,
			"fallback_error":  err.Error(),
		}, nil, err, time.Since(fallbackStart))
		k.captureRawLLMCall(proc, fbFD, step, err)
		return nil, fmt.Errorf("primary %s: %v; fallback %s: %w", primaryDevice, primaryErr, proc.FallbackDevice, err)
	}

	k.emitEvent(proc, "ReasonStep", map[string]any{
		"step":            step,
		"action":          "fallback",
		"primary_device":  primaryDevice,
		"primary_error":   primaryErr.Error(),
		"fallback_device": proc.FallbackDevice,
		"fallback_model":  proc.FallbackModel,
	}, nil, nil, time.Since(fallbackStart))

	// Story 56.7 (G4): successful fallback record (no outcome marker). Same
	// step as the primary-failure record — ReadRawForStep resolves via
	// last-match (terminal outcome wins).
	k.captureRawLLMCall(proc, fbFD, step, nil)

	return respData, nil
}

// reasonStep executes the reasoning loop for a process.
func (k *KernelImpl) reasonStep(proc *Process, llmFD types.FD, opts SpawnOpts) {
	maxSteps := proc.MaxSteps

	// Idempotent: when resume.go / load_suspended.go has already attached
	// the writers via attachStepObservation, this call is a fast no-op
	// (Story 48.1 code-review P1 — resume-path Mount/Resurrect events were
	// dropped because reasonStep was the only attachment site).
	k.attachStepObservation(proc)

	defer func() {
		// Check atomic flag first (no lock needed) to avoid deadlock if
		// a panic unwind occurs while proc.mu is held by suspendProcess.
		if proc.IsSuspendRequested() {
			notifySuspendDone(proc)
			return
		}
		state := proc.GetState()
		if state == types.StateRunning {
			k.finishProcess(proc, ExitStatus{Code: 1, Reason: "unexpected exit"})
		}
	}()

	var lastResultSummary string
	var consecutiveToolErrors errFingerprintCounter
	var consecutiveTransientRetries int
	// Story 73.2 / D5: the rate-limit family retries on its OWN counter —
	// socket/EOF/timeout keeps the legacy `< 2` budget above untouched, and
	// the two counters never consume each other. Declared beside
	// consecutiveTransientRetries on purpose; both reset together on success.
	var consecutiveRateLimitRetries int
	// lastToolResultHash carries the PREVIOUS tool_call step's result hash into the
	// loop check, which happens before this step's tools run (Story 70.1 AC2).
	var lastToolResultHash uint64
	loopDetector := NewLoopDetector(proc.effectiveLoopThreshold(), proc.effectiveCoarseLoopThreshold())

	startStep := 1
	if opts.StartStep > 0 {
		startStep = opts.StartStep
	}
	for step := startStep; maxSteps == 0 || step <= maxSteps; step++ {
		stepStart := time.Now()

		// Check previous checkpoint write error (Story 30.2 AC#10)
		if proc.checkpointErrCh != nil {
			select {
			case err := <-proc.checkpointErrCh:
				k.emitEvent(proc, "CheckpointWriteError", map[string]any{
					"step": step - 1,
					"err":  err.Error(),
				}, nil, err, 0)
			default:
			}
		}

		// Update heartbeat (Story 30.5)
		proc.mu.Lock()
		proc.LastHeartbeat = time.Now()
		proc.mu.Unlock()

		if k.callbacks != nil {
			k.callbacks.OnStep(proc.PID, step, maxSteps)
		}

		// Check pause state
		if ch := proc.WaitIfPaused(); ch != nil {
			k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "paused"}, nil, nil, 0)
			select {
			case <-ch:
				k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "resumed"}, nil, nil, time.Since(stepStart))
			case <-proc.ctx.Done():
				if proc.IsSuspendRequested() {
					return
				}
				k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "cancelled_while_paused"}, nil, proc.ctx.Err(), time.Since(stepStart))
				k.finishProcess(proc, ExitStatus{Code: 1, Reason: "context cancelled while paused"})
				return
			}
		}

		// Check context cancellation
		select {
		case <-proc.ctx.Done():
			if proc.IsSuspendRequested() {
				// Suspend exit: Kernel.Suspend() handles state transition and cleanup
				return
			}
			k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "cancelled"}, nil, proc.ctx.Err(), time.Since(stepStart))
			k.finishProcess(proc, ExitStatus{Code: 1, Reason: "context cancelled"})
			return
		default:
		}

		// Check gdb reasoning breakpoint
		if hit := proc.CheckBreakpoint(BreakpointContext{BPType: BPReasoning, StepNumber: step}); hit != nil {
			proc.GdbPause(fmt.Sprintf("reasoning breakpoint hit at step %d", step), hit)
			select {
			case <-proc.ctx.Done():
				k.finishProcess(proc, ExitStatus{Code: 1, Reason: "context cancelled while gdb-paused"})
				return
			default:
			}
		}

		// Check gdb step reasoning mode
		if proc.GetStepMode() == StepReasoning {
			proc.ClearStepMode()
			proc.GdbPause("step_reasoning", nil, map[string]any{"step_number": step, "last_result_summary": lastResultSummary})
			select {
			case <-proc.ctx.Done():
				k.finishProcess(proc, ExitStatus{Code: 1, Reason: "context cancelled while gdb-paused"})
				return
			default:
			}
		}

		// Build prompt from context
		buildPromptStart := time.Now()
		promptResult, err := k.ctxMgr.BuildPrompt(proc.CtxID)
		k.emitEvent(proc, "CtxRead", map[string]any{"cid": proc.CtxID, "op": "BuildPrompt"}, nil, err, time.Since(buildPromptStart))
		if err != nil {
			k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "error"}, nil, err, time.Since(stepStart))
			k.finishProcess(proc, ExitStatus{Code: 1, Reason: "build prompt failed", Err: err})
			return
		}

		// Construct LLM request
		model := opts.Model
		if override := proc.GetGdbModelOverride(); override != "" {
			model = override
		}
		// eventModel resolves the model for observability events only: opts.Model
		// is empty when no explicit model was requested (the driver applies its
		// own default), but that resolved default is captured on proc.Model at
		// spawn — fall back to it so strace Write events report the actual model
		// instead of "". The request model (Model: model below) is intentionally
		// left untouched to preserve the "empty → driver default" contract.
		eventModel := model
		if eventModel == "" {
			eventModel = proc.Model
		}
		sysPrompt := promptResult.SystemPrompt

		// When sections are NOT used, manually append dynamic prompt parts (legacy path)
		if !proc.HasSections {
			if envVars := proc.GetGdbEnvVars(); len(envVars) > 0 {
				var envSection strings.Builder
				envSection.WriteString("\n\n[GDB Environment Variables]\n")
				for k, v := range envVars {
					fmt.Fprintf(&envSection, "%s=%s\n", k, v)
				}
				sysPrompt += envSection.String()
			}
			proc.mu.Lock()
			loadedSkills := make([]string, len(proc.Skills))
			copy(loadedSkills, proc.Skills)
			proc.mu.Unlock()
			if len(loadedSkills) > 0 {
				// R5: skill bodies are delivered out-of-band; here we only advertise
				// which skills are loaded so the agent knows about them.
				sysPrompt += "\n\n# Loaded Skills\nCurrently loaded: " +
					strings.Join(loadedSkills, ", ") +
					".\nSkill instructions are delivered out-of-band."
			}
		}
		proc.mu.Lock()
		if proc.FinalSystemPrompt == "" {
			proc.FinalSystemPrompt = sysPrompt
		}
		proc.mu.Unlock()

		// R5: collect skills with their on-disk dirs for bundle-capable drivers.
		var projectDir string
		if proc.ProjectConfig != nil {
			projectDir = proc.ProjectConfig.ProjectDir
		}
		proc.mu.Lock()
		skillList := make([]llm.Skill, 0, len(proc.Skills))
		for _, name := range proc.Skills {
			body := proc.SkillBodies[name]
			if body == "" {
				continue
			}
			skillList = append(skillList, llm.Skill{
				Name: name,
				Body: body,
				Dir:  proc.SkillDirs[name],
			})
		}
		proc.mu.Unlock()

		req := llmRequest{
			Intent:          proc.Intent,
			SystemPrompt:    sysPrompt,
			Model:           model,
			ReasoningEffort: proc.ReasoningEffort,
			MaxTurns:        0,
			TimeoutMs:       opts.TimeoutMs,
			Messages:        promptResult.Messages,
			Skills:          skillList,
			ProjectDir:      projectDir,
			CallerPID:       uint64(proc.PID),
			CallerDepth:     proc.Depth,
			CallerUUID:      proc.UUID,
		}
		req.Tools = proc.nativeToolDefs
		reqJSON, err := json.Marshal(req)
		if err != nil {
			k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "error"}, nil, err, time.Since(stepStart))
			k.finishProcess(proc, ExitStatus{Code: 1, Reason: "marshal request failed", Err: err})
			return
		}

		// Write request to LLM device — use step-level context (Story 30.6)
		stepCtx, stepCancel := gocontext.WithCancel(proc.ctx)
		proc.SetStepCancel(stepCancel)

		writeStart := time.Now()
		var respData []byte
		if err := k.vfs.Write(stepCtx, proc.PID, llmFD, reqJSON); err != nil {
			proc.RunStreamCleanups()
			proc.SetStepCancel(nil)

			// Check if this was a step-level cancel (heartbeat retry), not process cancel
			// Must check BEFORE calling stepCancel() to distinguish real cancellation from cleanup
			if stepCtx.Err() != nil && proc.ctx.Err() == nil {
				stepCancel()
				k.emitEvent(proc, "ReasonStep", map[string]any{
					"step":   step,
					"action": "step_retry",
					"reason": "heartbeat_monitor_retry",
				}, nil, nil, time.Since(stepStart))
				continue // retry current step
			}
			stepCancel()

			// Story 44.5 AC1 — when SIGPAUSE arrives while the LLM Write is in
			// flight, suspendOneForSubtree (subtree.go) flips suspendRequested
			// and cancels proc.ctx. The cancel surfaces here as a Write error.
			// Returning directly hands control to the defer at reason.go:237,
			// which observes IsSuspendRequested and notifies the waiting
			// suspendProcess. Without this guard reasonStep proceeds into
			// attemptFallback → finishProcess(failed), killing the process and
			// leaving proc-info.json with state=dead +
			// suspend_reason=user_paused + exit_reason="llm write failed".
			if proc.IsSuspendRequested() && errors.Is(proc.ctx.Err(), gocontext.Canceled) {
				return
			}

			// Story 56.7 (G3): persist the failed primary request to raw.jsonl
			// with outcome=error. Placed after the suspend early-return (a
			// pause is not a failure — the process will resume) and before the
			// transient-retry check so both transient and terminal failures
			// are captured (AC1/AC3).
			k.captureRawLLMCall(proc, llmFD, step, err)

			// Story 66.2: process-level cancel (SIGTERM/SIGKILL/SignalTree)
			// during LLM write → interrupted, not fake completed or pointless
			// transient retry. Placed after suspend early-return (suspend wins)
			// and captureRaw, before transient retry (killed → no retry).
			if proc.ctx.Err() != nil {
				k.handleInterruptedWrite(proc, step, promptResult, err, stepStart)
				return
			}

			// Transient error retry. Story 73.2 / D5: the rate-limit family
			// branches off BEFORE the legacy transient counter — it keeps its
			// own counter (consecutiveRateLimitRetries, capped at
			// maxRateLimitRetries) and its own disposition (backoff wait),
			// while socket/EOF/timeout failures keep the `< 2` budget and the
			// zero-delay retry VERBATIM. The two counters never consume each
			// other's budget; both reset on success below.
			if isTransientLLMError(err) {
				kind, isRateLimit := llm.RateLimitKindOf(err)
				if isRateLimit {
					// AC4 three-level precedence: server-declared wait
					// (header or body, parsed driver-side — D8) bypasses the
					// local maxDelay cap; only the hard cap below bounds it.
					// attempt is 1-based for the retry about to happen.
					baseDelay, waitSource, serverStated := resolveRateLimitDelay(err, consecutiveRateLimitRetries+1)

					if baseDelay > maxInProcessWait {
						// AC5 give-up: a required wait beyond the in-process
						// limit is neither waited out nor retried — fall
						// through to the existing failure path
						// (attemptFallback → finishProcess). That is no
						// worse than pre-73.2 (long quota windows died there
						// too) and stops burning retries on a window that
						// cannot recover in-process.
						//
						// TODO(Story 73.3): replace this give-up with
						// selfSuspend + ResumeAt so long quota windows
						// suspend the process and resume at the reset
						// instant instead of killing it. 73.2 deliberately
						// stops at the early-exit seam (D4).
						k.emitEvent(proc, "ReasonStep", map[string]any{
							"step":            step,
							"action":          "rate_limit_giveup",
							"wait_ms":         baseDelay.Milliseconds(), // the REQUIRED wait, not a waited duration
							"limit_ms":        maxInProcessWait.Milliseconds(),
							"rate_limit_kind": kind.String(),
						}, nil, nil, time.Since(stepStart))
						log.Printf("[kernel] pid=%d step=%d rate limit giveup: kind=%s required wait %s exceeds in-process limit %s — no retry (device=%s)",
							proc.PID, step, kind, baseDelay, maxInProcessWait, proc.PrimaryDevice)
						// No continue: control falls through to attemptFallback.
					} else if consecutiveRateLimitRetries < maxRateLimitRetries {
						consecutiveRateLimitRetries++
						// AC4 one-sided jitter applies to server-declared
						// values too (de-correlating simultaneous retries);
						// the result is clamped to the hard cap so jitter can
						// never promote an in-cap wait into give-up
						// territory (test 9-②'s 59s provider relies on this).
						delay := min(applyRateLimitJitter(baseDelay), maxInProcessWait)
						retryFields := map[string]any{
							"step":    step,
							"action":  "transient_retry",
							"attempt": consecutiveRateLimitRetries,
							"reason":  err.Error(),
							// Story 73.1 / AC6 field, preserved verbatim:
							"rate_limit_kind": kind.String(),
						}
						// Story 73.2 / D9: four new fields ride the existing
						// transient_retry event. Non-rate-limit retries carry
						// none of them (anti-semantic-placeholder discipline,
						// same as 73.1's rate_limit_kind).
						retryFields["wait_ms"] = delay.Milliseconds()
						retryFields["wait_source"] = waitSource
						if serverStated {
							if ra, resetAt, _, ok := llm.RateLimitWaitOf(err); ok {
								if ra > 0 {
									retryFields["retry_after_ms"] = ra.Milliseconds()
								}
								if !resetAt.IsZero() {
									retryFields["reset_at"] = resetAt.Format(time.RFC3339)
								}
							}
						}
						k.emitEvent(proc, "ReasonStep", retryFields, nil, nil, time.Since(stepStart))
						log.Printf("[kernel] pid=%d step=%d rate limit classified: kind=%s attempt=%d wait=%s source=%s device=%s",
							proc.PID, step, kind, consecutiveRateLimitRetries, delay, waitSource, proc.PrimaryDevice)

						// AC6: interruptible chunked wait. A cancel arriving
						// mid-wait never passes the pre-retry cancel check
						// above, so this select is the only defence; the true
						// case routes through handleInterruptedWrite — the
						// SAME exit path as a cancel during the write — so
						// one SIGTERM yields one exit_reason regardless of
						// timing.
						if k.backoffWaitInterruptible(proc, delay) {
							k.handleInterruptedWrite(proc, step, promptResult, err, stepStart)
							return
						}
						continue // retry current step
					}
					// Rate-limit budget exhausted with the wait still inside
					// the cap: fall through to attemptFallback — fallback
					// only AFTER backoff is exhausted (§6 combination matrix).
				} else if consecutiveTransientRetries < 2 {
					consecutiveTransientRetries++
					retryFields := map[string]any{
						"step":    step,
						"action":  "transient_retry",
						"attempt": consecutiveTransientRetries,
						"reason":  err.Error(),
					}
					k.emitEvent(proc, "ReasonStep", retryFields, nil, nil, time.Since(stepStart))
					continue // retry current step (zero-delay, unchanged)
				}
			}

			k.emitEvent(proc, "Write", map[string]any{"fd": llmFD, "size": len(reqJSON), "model": eventModel, "reasoning_effort": proc.ReasoningEffort}, nil, err, time.Since(writeStart))
			fbData, fbErr := k.attemptFallback(proc, req, proc.PrimaryDevice, err, step)
			if fbErr != nil {
				reason := "llm write failed"
				if proc.FallbackDevice != "" {
					reason = "all providers exhausted"
				}
				// Story 66.1 (P1b): surface the driver's first error line in the
				// exit reason so quota/auth/link failures are distinguishable in
				// proc-info.json without digging through events.jsonl. The step
				// record below keeps the base reason — it already carries the
				// full error text.
				exitReason := reason
				if detail := driverErrorDetail(fbErr); detail != "" {
					exitReason = reason + ": " + detail
				}
				logDevice := proc.PrimaryDevice
				if proc.FallbackDevice != "" {
					// "all providers exhausted" spans primary+fallback; label
					// both so the daemon log is not misattributed to primary
					// alone (Story 66.1 P1).
					logDevice += "+" + proc.FallbackDevice
				}
				log.Printf("[kernel] pid=%d %s (device=%s): %v", proc.PID, reason, logDevice, fbErr)
				// Record the failed step with prompt data so it's visible in LLM viewer
				k.writeStepRecord(proc, step, promptResult, "",
					nil, "error", fmt.Sprintf("%s: %v", reason, fbErr), "", "", "", "", 0, nil)
				k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "error"}, nil, fbErr, time.Since(stepStart))
				k.finishProcess(proc, ExitStatus{Code: 1, Reason: exitReason, Err: fbErr})
				return
			}
			respData = fbData
		} else {
			proc.SetStepCancel(nil)
			stepCancel()
			consecutiveTransientRetries = 0 // reset on success
			// Story 73.2 / D5: both counters reset together — a success after
			// a rate-limit wait proves the window recovered, clearing the
			// family budget for the next throttle.
			consecutiveRateLimitRetries = 0

			k.emitEvent(proc, "Write", map[string]any{"fd": llmFD, "size": len(reqJSON), "model": eventModel, "reasoning_effort": proc.ReasoningEffort}, nil, nil, time.Since(writeStart))
			readStart := time.Now()
			var readErr error
			respData, readErr = k.vfs.Read(proc.PID, llmFD, 1<<20)
			readArgs := map[string]any{"fd": llmFD, "length": 1 << 20}
			if len(respData) > 0 {
				readArgs["content"] = string(respData)
			}
			k.emitEvent(proc, "Read", readArgs, len(respData), readErr, time.Since(readStart))
			// Story 56.1 AC#6/#7: pull raw LLM request/response capture from
			// the LLM file (if the driver opted into vfs.RawCaptureProvider)
			// and append a NDJSON line to <baseDir>/steps/<uuid>/raw.jsonl.
			// Review patch P6: moved post-Read so future Stream-style drivers
			// can populate Last with both request + accumulated response in
			// one go. Best-effort — failures are logged via captureRawLLMCall
			// and must NOT abort reasonStep. Story 56.7: readErr (nil on the
			// happy path) marks the record outcome=error when the Read failed.
			k.captureRawLLMCall(proc, llmFD, step, readErr)
			if readErr != nil {
				proc.RunStreamCleanups()
				// Story 44.5 AC1 — same suspend-vs-kill race as the Write path
				// above. ctx cancel triggered by SIGPAUSE surfaces as a Read
				// error; return so the defer at reason.go:237 hands off to
				// suspendProcess instead of finishProcess.
				if proc.IsSuspendRequested() && errors.Is(proc.ctx.Err(), gocontext.Canceled) {
					return
				}
				// Story 66.2: same interrupted guard as the write-err path.
				if proc.ctx.Err() != nil {
					k.handleInterruptedWrite(proc, step, promptResult, readErr, stepStart)
					return
				}
				// Story 66.1 (P1b): same driver-detail merge as the write-failed
				// branch above.
				exitReason := "llm read failed"
				if detail := driverErrorDetail(readErr); detail != "" {
					exitReason = "llm read failed: " + detail
				}
				log.Printf("[kernel] pid=%d llm read failed (device=%s): %v", proc.PID, proc.PrimaryDevice, readErr)
				// Record the failed step with prompt data so it's visible in LLM viewer
				k.writeStepRecord(proc, step, promptResult, string(respData),
					nil, "error", fmt.Sprintf("llm read failed: %v", readErr), "", "", "", "", 0, nil)
				k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "error"}, nil, readErr, time.Since(stepStart))
				k.finishProcess(proc, ExitStatus{Code: 1, Reason: exitReason, Err: readErr})
				return
			}
		}

		// Parse LLM response
		var resp llmResponse
		rawResponseStr := string(respData)
		if err := json.Unmarshal(respData, &resp); err != nil {
			proc.RunStreamCleanups()
			// Story 44.5 AC1 — completing the pause-protocol coverage: if the
			// driver was mid-stream when SIGPAUSE arrived, the Read can return
			// a truncated/empty buffer that fails unmarshal. Treat as suspend
			// instead of error so reasonStep yields to suspendProcess.
			if proc.IsSuspendRequested() && errors.Is(proc.ctx.Err(), gocontext.Canceled) {
				return
			}
			// Record the failed step with raw response so it's visible in LLM viewer
			k.writeStepRecord(proc, step, promptResult, rawResponseStr, nil,
				"error", fmt.Sprintf("unmarshal response failed: %v", err), "", "", "", "", 0, nil)
			k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "error"}, nil, err, time.Since(stepStart))
			k.finishProcess(proc, ExitStatus{Code: 1, Reason: "unmarshal response failed", Err: err})
			return
		}

		// max_turns: agentic loop (CLI side) hit its turn limit. Record and exit
		// with a distinct reason so the Dashboard / parent process can react
		// differently from a true "empty response" configuration error. (R2)
		if resp.StopReason == "max_turns" && len(resp.ToolCalls) == 0 {
			k.writeStepRecord(proc, step, promptResult, rawResponseStr, &resp,
				"error", "max_turns_reached", "", "", "", "", 0, nil)
			k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "max_turns_reached"}, nil, nil, time.Since(stepStart))
			k.finishProcess(proc, ExitStatus{Code: ExitSuspended, Reason: "max_turns_reached"})
			return
		}

		// Guard: detect empty LLM response (common symptom of misconfigured provider)
		if resp.Content == "" && resp.TokensUsed == 0 && len(resp.ToolCalls) == 0 {
			emptyErr := fmt.Errorf("LLM returned empty response (content=\"\", tokens=0, no tool_calls) — check provider/model configuration")
			k.writeStepRecord(proc, step, promptResult, rawResponseStr, &resp,
				"error", "empty_llm_response", "", "", "", "", 0, nil)
			k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "empty_response"}, nil, emptyErr, time.Since(stepStart))
			k.finishProcess(proc, ExitStatus{Code: 1, Reason: "empty_llm_response", Err: emptyErr})
			return
		}

		lastResultSummary = resp.Content
		if len(lastResultSummary) > 80 {
			lastResultSummary = lastResultSummary[:80] + "..."
		}

		// Recording hook: LLM response
		if k.recordMgr != nil && k.recordMgr.IsRecording(proc.PID) {
			llmEvent := debug.RecordEvent{
				Timestamp: time.Since(proc.CreatedAt),
				PID:       proc.PID,
				Type:      debug.RecordLLMResponse,
				LLM: &debug.LLMResponseData{
					Model:          model,
					ResponseTokens: resp.TokensUsed,
				},
			}
			if err := k.recordMgr.RecordEvent(proc.PID, llmEvent); err != nil {
				log.Printf("[record] llm event error pid=%d: %v", proc.PID, err)
			}
		}

		proc.mu.Lock()
		// Story 66.6: the final LLMResponse is authoritative. Discard the current
		// step's mid-stream accumulation (StreamTokensUsed = session total ⊇ Σ
		// deltas for CLI drivers) BEFORE folding resp.TokensUsed in, so the live
		// preview never double-counts. Provenance for the authoritative value is
		// cli_stream when the driver parsed it out of a CLI stream, api_response
		// when it came straight from an API response (mergeProvenance only lowers).
		proc.StreamTokensUsed = 0
		proc.StreamInputTokens = 0
		stepProvenance := vfs.UsageProvenanceAPIResponse
		if llm.IsCLIDriver(proc.DriverType) {
			stepProvenance = vfs.UsageProvenanceCLIStream
		}
		proc.UsageProvenance = mergeProvenance(proc.UsageProvenance, stepProvenance)
		proc.TokensUsed += resp.TokensUsed
		proc.LastInputTokens = resp.InputTokens
		budget := proc.ContextBudget
		tokens := proc.TokensUsed
		inputTokens := resp.InputTokens
		hasTrace := proc.TraceID != ""
		proc.mu.Unlock()

		proc.AppendTokenSnapshot(step, tokens)

		if hasTrace && k.spanRecorder != nil {
			k.spanRecorder.RecordTokens(proc.PID, resp.TokensUsed)
		}

		k.checkBudgetWarning(proc, step, inputTokens, budget)

		if budget > 0 && inputTokens >= budget {
			k.emitLog(proc, step, types.LogOutput, fmt.Sprintf("Context budget exceeded: %d/%d input tokens", inputTokens, budget), "")
			k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "context_full", "input_tokens": inputTokens, "budget": budget}, nil, nil, time.Since(stepStart))
			k.writeStepRecord(proc, step, promptResult, rawResponseStr, &resp,
				"error", fmt.Sprintf("context_full: %d/%d input tokens", inputTokens, budget), "", "", "", "", 0, nil)
			if err := k.selfSuspend(proc, "context_full", ExitContextFull); err != nil {
				log.Printf("[kernel] pid=%d context_full suspend failed: %v, falling back to terminate", proc.PID, err)
				k.finishProcess(proc, ExitStatus{Code: ExitContextFull, Reason: "context_full", Err: fmt.Errorf("context budget exceeded: %d/%d input tokens", inputTokens, budget)})
			}
			return
		}

		// Cost accumulation + budget check (Story 30.7) — suspend instead of terminate
		proc.mu.Lock()
		if proc.Budget.MaxCost > 0 {
			if resp.CostUSD > 0 {
				// Prefer the cost reported by the CLI (paperclip-style R4).
				// More accurate than token × rate estimation, especially for
				// Bedrock / subscription accounts where cost_per_token is unknown.
				proc.Budget.UsedCost += resp.CostUSD
			} else {
				cpt := k.getCostPerToken(proc.Provider)
				if cpt < 0 {
					cpt = 0
				}
				if cpt == 0 {
					log.Printf("[kernel] pid=%d warning: MaxCost=$%.2f set but no CostUSD reported and costPerToken=0 for provider %q — cost budget will not trigger", proc.PID, proc.Budget.MaxCost, proc.Provider)
				}
				proc.Budget.UsedCost += float64(resp.TokensUsed) * cpt
			}
		}
		budgetCheck := proc.Budget
		proc.mu.Unlock()
		if (budgetCheck.MaxTokens > 0 && int64(tokens) >= budgetCheck.MaxTokens) ||
			(budgetCheck.MaxCost > 0 && budgetCheck.UsedCost >= budgetCheck.MaxCost) {
			k.emitEvent(proc, "ReasonStep", map[string]any{
				"step": step, "action": "budget_exhausted",
				"used_tokens": tokens, "max_tokens": budgetCheck.MaxTokens,
				"used_cost": budgetCheck.UsedCost, "max_cost": budgetCheck.MaxCost,
			}, nil, nil, time.Since(stepStart))
			// Record the step that triggered budget exhaustion
			k.writeStepRecord(proc, step, promptResult, rawResponseStr, &resp,
				"error", fmt.Sprintf("budget_exhausted: tokens=%d/%d cost=%.4f/%.4f",
					tokens, budgetCheck.MaxTokens, budgetCheck.UsedCost, budgetCheck.MaxCost),
				"", "", "", "", 0, nil)
			if err := k.selfSuspend(proc, "budget_exhausted", ExitSuspended); err != nil {
				log.Printf("[kernel] pid=%d budget suspend failed: %v, falling back to terminate", proc.PID, err)
				k.finishProcess(proc, ExitStatus{Code: ExitError, Reason: "budget_exhausted + suspend failed"})
			}
			return
		}

		if hit := proc.CheckBreakpoint(BreakpointContext{BPType: BPBudget, TokensUsed: tokens, StepNumber: step}); hit != nil {
			proc.GdbPause(fmt.Sprintf("budget breakpoint hit: %d tokens used", tokens), hit)
			select {
			case <-proc.ctx.Done():
				k.finishProcess(proc, ExitStatus{Code: 1, Reason: "context cancelled while gdb-paused"})
				return
			default:
			}
		}

		// Tool calls path: execute tools and continue reasoning loop
		if len(resp.ToolCalls) > 0 {
			// Loop detection: hash first tool call
			tc := resp.ToolCalls[0]
			inputStr := ""
			if tc.Input != nil {
				if raw, err := json.Marshal(tc.Input); err == nil {
					inputStr = string(raw)
				}
			}
			loopHash := ActionHash("tool_call", tc.Name, inputStr)
			coarseHash := CoarseActionHash("tool_call", tc.Name)
			if loopResult := loopDetector.CheckDual(loopHash, coarseHash, lastToolResultHash); loopResult != LoopNone {
				if stopped := k.handleLoopDetection(proc, loopResult, loopDetector.LastTriggeredThreshold, loopDetector.LastTriggeredTrack, step, stepStart); stopped {
					return
				}
			}

			toolCallsAcc, shouldContinue := k.executeToolCalls(proc, resp, step, stepStart, &consecutiveToolErrors, promptResult, rawResponseStr)
			// Spec step-inspector-data-fidelity：tool_calls 路径在 step 完成时
			// 一次性写入 StepRecord（含 ToolCalls 数组）,避免循环内多次写入导致
			// ReadStep 去重时只剩末尾一个 call。旧字段 toolPath/toolInput/toolResult
			// 从数组末元素回填以兼容旧 reader。
			//
			// 缺陷 2 修复：Messages 用 step 结束后的完整快照（含本步 assistant
			// + N 个 tool_result),而非 promptResult（调用前快照）。BuildPrompt 失败
			// 时降级使用 promptResult。
			if len(toolCallsAcc) > 0 {
				freshPrompt := promptResult
				if fp, fpErr := k.ctxMgr.BuildPrompt(proc.CtxID); fpErr == nil {
					freshPrompt = fp
				}
				last := toolCallsAcc[len(toolCallsAcc)-1]
				summary := buildBatchToolSummary(toolCallsAcc)
				k.writeStepRecord(proc, step, freshPrompt, rawResponseStr, &resp,
					"tool_call", summary,
					last.Path, last.Input, last.Result, last.Error,
					time.Duration(last.DurationMs*float64(time.Millisecond)), toolCallsAcc)
			}
			if !shouldContinue {
				return
			}
			// Feed this step's batch result into the NEXT step's loop check
			// (Story 70.1 AC2/AC3). The check above runs before executeToolCalls,
			// so it can only ever see the previous step's result — hence the
			// one-step lag.
			//
			// Why the lag is harmless: in a genuine loop of identical actions the
			// result sequence is identical too, so shifting the results by one
			// position leaves every comparison window uniform. The only effect is
			// that the trigger moves from step 2N to step 2N+1. Keeping the check
			// BEFORE execution is what matters — it stops a real spin before one
			// more side-effecting tool call (git commit / spawn / rm) runs.
			lastToolResultHash = ToolResultHash(toolCallsAcc)
			// Proactive reclamation of leaked tool results (Story 69.4). This is
			// the ONLY call site on purpose: tool_exec.go's autoCompactIfNeeded is
			// a fault-handling path already covered by Story 69.3's mechanical
			// fallback, and preCompactForToolCalls is a pure slot judgement that
			// a token-axis prune cannot move.
			//
			// Ordering is the mechanism, not a coincidence: the reclamation runs
			// FIRST, then autoCompactIfNeeded re-reads TokenUsage and sees the
			// lowered figure — so a compaction that would have fired may now not
			// fire at all. That deferral is what "推迟触阈" means here.
			k.reclaimLeakedIfNeeded(proc, step)
			// Auto-compact check (Story 31.2): after tool calls processed, before checkpoint
			k.autoCompactIfNeeded(proc, step)
			k.asyncWriteCheckpoint(proc, step, consecutiveToolErrors.count)
			// Record completed step so an in-memory SIGPAUSE/SIGRESUME cycle
			// restarts at step+1 instead of step 1 (Story 44.1 code review F5).
			proc.LastCompletedStep.Store(int64(step))
			continue
		}

		// No tool calls: final result (CLI Agent completed or SDK model finished)
		if hit := proc.CheckBreakpoint(BreakpointContext{BPType: BPQuality, LLMResponse: resp.Content, StepNumber: step}); hit != nil {
			proc.GdbPause(fmt.Sprintf("quality breakpoint hit at step %d", step), hit)
			select {
			case <-proc.ctx.Done():
				k.finishProcess(proc, ExitStatus{Code: 1, Reason: "context cancelled while gdb-paused"})
				return
			default:
			}
		}

		k.emitLog(proc, step, types.LogOutput, resp.Content, "")
		k.writeStepRecord(proc, step, promptResult, rawResponseStr, &resp,
			"text", briefTextSummary(resp.Content), "", "", "", "", 0, nil)

		proc.mu.Lock()
		proc.Result = resp.Content
		failedChildren := proc.failedChildren
		proc.mu.Unlock()
		k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "text"}, resp.Content, nil, time.Since(stepStart))
		stepDur := time.Since(stepStart)
		if k.callbacks != nil {
			k.callbacks.OnStepComplete(proc.PID, step, "text", briefTextSummary(resp.Content), false, float64(stepDur.Microseconds())/1000.0)
		}
		// Record completed step so an in-memory SIGPAUSE/SIGRESUME cycle
		// restarts at step+1 instead of step 1 (Story 44.1 code review F5).
		proc.LastCompletedStep.Store(int64(step))
		// Layer-1 exit verdict — identical to the ActionComplete path (CAP-3,
		// spec-exit-code-tool-error-fidelity). VFS tool error codes never drive
		// exit; only failedChildren (here) and the circuit breaker (an earlier
		// early-exit) do. This path previously LACKED the failedChildren guard
		// (asymmetry vs ActionComplete), letting a process exiting via final text
		// mask failed children with exit 0 — adding it closes that fake-success hole.
		exitCode := 0
		reason := "completed"
		if failedChildren > 0 {
			exitCode = 1
			reason = fmt.Sprintf("completed_with_%d_failed_children", failedChildren)
		}
		// Story 66.2 backstop (shared with ActionComplete / plan-as-text via
		// completionVerdict): the response was fully received (receivedDone=true
		// in writeStream) but the process was cancelled between Write-return and
		// here → interrupted. Result keeps the complete text (no [partial]
		// prefix) — the content is intact, only the process is dying. A
		// signal-kill dominates a completed_with_N_failed_children verdict.
		reason, exitCode = k.completionVerdict(proc, reason, exitCode)
		k.finishProcess(proc, ExitStatus{Code: exitCode, Reason: reason})
		return
	}

	k.finishProcess(proc, ExitStatus{Code: 1, Reason: "max steps exceeded"})
}

const partialResultPrefix = "[partial] "

// handleInterruptedWrite handles the case where a process-level cancel
// (SIGTERM/SIGKILL/SignalTree) occurred during an LLM write or read.
// If the driver returned a StreamInterruptedError with partial content,
// that content is preserved in steps.jsonl and proc.Result is tagged
// with the [partial] prefix. Otherwise Result is left empty for
// finishProcess to backfill as "interrupted".
func (k *KernelImpl) handleInterruptedWrite(proc *Process, step int, promptResult *rnixctx.PromptResult, err error, stepStart time.Time) {
	var partial string
	var sie *llm.StreamInterruptedError
	if errors.As(err, &sie) {
		partial = sie.Partial
	}

	if partial != "" {
		k.writeStepRecord(proc, step, promptResult, "",
			nil, "text", briefTextSummary(partial), "", "", "", "", 0, nil)
		proc.mu.Lock()
		proc.Result = partialResultPrefix + partial
		proc.ResultPartial = true
		proc.mu.Unlock()
	}

	k.emitEvent(proc, "ReasonStep", map[string]any{
		"step":    step,
		"action":  "interrupted",
		"partial": partial != "",
	}, nil, nil, time.Since(stepStart))
	log.Printf("[kernel] pid=%d interrupted (signal) during llm write, partial=%v", proc.PID, partial != "")
	k.finishProcess(proc, ExitStatus{Code: 1, Reason: "interrupted", Err: err})
}

// completionVerdict overrides a would-be "completed" verdict to "interrupted"
// when the process context was cancelled by a signal (SIGTERM/SIGKILL/
// SignalTree) while NOT suspended. Story 66.2 (code-review D1): shared by every
// completion site — the final-text branch, ActionComplete, and the
// planning-disabled plan-as-text branch — so a signal-killed process never
// reports `completed` regardless of which completion path it exits through. The
// window guarded is "full response received (receivedDone=true), then cancelled
// before the verdict is committed"; the in-flight-write case is handled earlier
// by handleInterruptedWrite. A cancel dominates any base reason (incl.
// completed_with_N_failed_children).
func (k *KernelImpl) completionVerdict(proc *Process, base string, code int) (string, int) {
	if proc.ctx.Err() != nil && !proc.IsSuspendRequested() {
		return "interrupted", 1
	}
	return base, code
}

// asyncWriteCheckpoint serializes context synchronously, then dispatches
// checkpoint write to a fire-and-forget goroutine. Errors are sent to
// proc.checkpointErrCh (buffered, cap=1); the next step checks this channel.
//
// Story 42.2: applies ShouldCheckpoint throttling — only writes when step-count
// or time-window threshold is reached, and updates lastCheckpointStep/Time in the
// main goroutine before launching the async writer (optimistic update to prevent
// double-dispatch). Also triggers SaveProcInfo (state=running) so daemon crash
// recovery can discover the process.
func (k *KernelImpl) asyncWriteCheckpoint(proc *Process, step int, consecutiveToolErrors int) {
	proc.mu.Lock()
	dir := proc.stepsDir
	errCh := proc.checkpointErrCh
	proc.mu.Unlock()
	if dir == "" || errCh == nil {
		return
	}

	if !k.ShouldCheckpoint(proc, step) {
		return
	}

	// Serialize context synchronously (main goroutine) before launching async write
	ctx, err := k.ctxMgr.GetContext(proc.CtxID)
	if err != nil {
		log.Printf("[checkpoint] pid=%d step=%d GetContext failed: %v", proc.PID, step, err)
		return
	}
	ctxSnap, err := ctx.Serialize()
	if err != nil {
		log.Printf("[checkpoint] pid=%d step=%d Serialize failed: %v", proc.PID, step, err)
		return
	}

	cpData := buildCheckpointData(proc, step, json.RawMessage(ctxSnap), consecutiveToolErrors)

	// Optimistic update: stamp the throttle fields in the main goroutine so
	// the next ShouldCheckpoint call sees the new baseline immediately.
	proc.mu.Lock()
	proc.lastCheckpointStep = step
	proc.lastCheckpointTime = time.Now()
	proc.mu.Unlock()

	// Snapshot ProcInfo for SaveProcInfo (state=running, best-effort)
	var procInfo *vfs.ProcInfo
	if pi, piErr := k.GetProcInfo(proc.PID); piErr == nil {
		procInfo = pi
	} else {
		log.Printf("[checkpoint] pid=%d GetProcInfo failed: %v", proc.PID, piErr)
	}

	// Track the write goroutine in proc.wg so reapProcess and Shutdown wait for it to
	// finish before TempDir cleanup. Safe to call Go here: this function runs inside the
	// reasoning goroutine, which is itself tracked by proc.wg, so the counter is > 0.
	proc.wg.Go(func() {
		if err := writeCheckpoint(dir, cpData); err != nil {
			select {
			case errCh <- err:
			default: // channel full, discard old error
			}
		}
		// Avoid persisting a phantom state=running snapshot if the process
		// transitioned to Zombie/Dead between the main-goroutine snapshot and
		// the async write — otherwise the next daemon startup would resurrect
		// it as resumable. We can read state without the lock because GetState
		// is atomic.
		if procInfo != nil && proc.GetState() == types.StateRunning {
			if err := SaveProcInfo(k.ResolveStepBaseDir(proc), *procInfo); err != nil {
				log.Printf("[checkpoint] pid=%d SaveProcInfo failed: %v", proc.PID, err)
			}
		}
	})
}

// getCostPerToken returns the cost per token for a given provider.
// Returns 0 if no cost configuration is available (cost tracking disabled).
func (k *KernelImpl) getCostPerToken(provider string) float64 {
	if k.costPerToken == nil {
		return 0
	}
	return k.costPerToken(provider)
}

// handleLoopDetection processes a loop detection result. Returns true if the process was terminated.
func (k *KernelImpl) handleLoopDetection(proc *Process, status LoopStatus, threshold int, track string, step int, stepStart time.Time) bool {
	switch status {
	case LoopWarning:
		k.emitEvent(proc, "LoopDetected", map[string]any{
			"step":      step,
			"threshold": threshold,
		}, nil, nil, time.Since(stepStart))
		log.Printf("[kernel] pid=%d loop detected at step %d: same action repeated %d times (track=%s)", proc.PID, step, threshold, track)
		// Story 70.2: the warning is emitted as an event only — it is deliberately
		// NOT appended to the process context. The old implementation injected a
		// RoleUser message ("try a different approach"), which carried two proven
		// costs: it was replayed in every subsequent BuildPrompt for the rest of the
		// run (up to 61 times in the incident session), and its imperative is wrong
		// advice for an orchestrator that should continue its planned flow. Its
		// benefit was never observed: the only sample is a FALSE-POSITIVE detection
		// in which the orchestrator correctly ignored it, which says nothing about
		// whether a warning helps during a genuine loop.
		//
		// Do not "restore" the injection on the theory that it protected the prompt
		// cache: it did not. AppendMessage is a tail append, not a mid-sequence
		// insert, so the cached prefix survives it (measured: hit rate held at
		// 98-99% across the injection point). See the "循环检测警告机制" section in
		// CLAUDE.md for the measurements and for the accepted trade-off — with the
		// injection gone, the LLM has no visibility into loop detection at all,
		// including the eventual suspend.
	case LoopSuspend:
		k.emitEvent(proc, "LoopSuspend", map[string]any{
			"step":      step,
			"threshold": 2 * threshold,
		}, nil, nil, time.Since(stepStart))
		log.Printf("[kernel] pid=%d loop suspend at step %d: same action repeated %d times (track=%s)", proc.PID, step, 2*threshold, track)
		if err := k.selfSuspend(proc, "loop_detected", ExitSuspended); err != nil {
			log.Printf("[kernel] pid=%d suspend failed: %v, falling back to terminate", proc.PID, err)
			k.finishProcess(proc, ExitStatus{Code: ExitError, Reason: "loop detected + suspend failed"})
		}
		return true
	}
	return false
}
