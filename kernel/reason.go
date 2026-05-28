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
		return
	}

	proc.mu.Lock()
	if proc.stepWriter == nil {
		if sw, err := NewStepWriter(stepBaseDir, proc.UUID); err == nil {
			proc.stepWriter = sw
		}
	}
	if proc.eventWriter == nil {
		if ew, err := NewEventWriter(stepBaseDir, proc.UUID); err == nil {
			proc.eventWriter = ew
		}
	}
	if proc.stepsDir == "" {
		proc.stepsDir = filepath.Join(stepBaseDir, "data", "steps", proc.UUID)
	}
	if proc.scratchDir == "" {
		proc.scratchDir = filepath.Join(stepBaseDir, "data", "scratch", proc.UUID)
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
	// Check via the llm package's IsTransient (uses sentinel errors including ErrStreamIncomplete)
	if llm.IsTransient(err) {
		return true
	}
	var llmErr interface{ Unwrap() error }
	if errors.As(err, &llmErr) {
		inner := llmErr.Unwrap()
		if inner != nil {
			if llm.IsTransient(inner) {
				return true
			}
			lower := strings.ToLower(inner.Error())
			if strings.Contains(lower, "socket") ||
				strings.Contains(lower, "connection") ||
				strings.Contains(lower, "overloaded") ||
				strings.Contains(lower, "eof") ||
				strings.Contains(lower, "reset by peer") ||
				strings.Contains(lower, "stream") {
				return true
			}
		}
	}
	// Also match on the outer error string as a fallback
	lower := strings.ToLower(err.Error())
	return strings.Contains(lower, "socket") ||
		strings.Contains(lower, "connection") ||
		strings.Contains(lower, "overloaded") ||
		strings.Contains(lower, "stream ended without result")
}

// attemptFallback tries the fallback provider when primary LLM call fails.
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
		return nil, fmt.Errorf("primary %s: %v; fallback %s: %v", primaryDevice, primaryErr, proc.FallbackDevice, err)
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
		return nil, fmt.Errorf("primary %s: %v; fallback %s: %v", primaryDevice, primaryErr, proc.FallbackDevice, err)
	}

	k.emitEvent(proc, "ReasonStep", map[string]any{
		"step":            step,
		"action":          "fallback",
		"primary_device":  primaryDevice,
		"primary_error":   primaryErr.Error(),
		"fallback_device": proc.FallbackDevice,
		"fallback_model":  proc.FallbackModel,
	}, nil, nil, time.Since(fallbackStart))

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
	loopDetector := NewLoopDetector(DefaultLoopThreshold)

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
			Intent:       proc.Intent,
			SystemPrompt: sysPrompt,
			Model:        model,
			MaxTurns:     0,
			TimeoutMs:    opts.TimeoutMs,
			Messages:     promptResult.Messages,
			Skills:       skillList,
			ProjectDir:   projectDir,
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

			// Transient error retry (socket disconnect, overloaded, etc.)
			if isTransientLLMError(err) && consecutiveTransientRetries < 2 {
				consecutiveTransientRetries++
				k.emitEvent(proc, "ReasonStep", map[string]any{
					"step":    step,
					"action":  "transient_retry",
					"attempt": consecutiveTransientRetries,
					"reason":  err.Error(),
				}, nil, nil, time.Since(stepStart))
				continue // retry current step
			}

			k.emitEvent(proc, "Write", map[string]any{"fd": llmFD, "size": len(reqJSON), "model": model}, nil, err, time.Since(writeStart))
			fbData, fbErr := k.attemptFallback(proc, req, proc.PrimaryDevice, err, step)
			if fbErr != nil {
				reason := "llm write failed"
				if proc.FallbackDevice != "" {
					reason = "all providers exhausted"
				}
				// Record the failed step with prompt data so it's visible in LLM viewer
				k.writeStepRecord(proc, step, promptResult, "",
					nil, "error", fmt.Sprintf("%s: %v", reason, fbErr), "", "", "", "", 0, nil)
				k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "error"}, nil, fbErr, time.Since(stepStart))
				k.finishProcess(proc, ExitStatus{Code: 1, Reason: reason, Err: fbErr})
				return
			}
			respData = fbData
		} else {
			proc.SetStepCancel(nil)
			stepCancel()
			consecutiveTransientRetries = 0 // reset on success

			k.emitEvent(proc, "Write", map[string]any{"fd": llmFD, "size": len(reqJSON), "model": model}, nil, nil, time.Since(writeStart))
			readStart := time.Now()
			var readErr error
			respData, readErr = k.vfs.Read(proc.PID, llmFD, 1<<20)
			readArgs := map[string]any{"fd": llmFD, "length": 1 << 20}
			if len(respData) > 0 {
				readArgs["content"] = string(respData)
			}
			k.emitEvent(proc, "Read", readArgs, len(respData), readErr, time.Since(readStart))
			if readErr != nil {
				// Story 44.5 AC1 — same suspend-vs-kill race as the Write path
				// above. ctx cancel triggered by SIGPAUSE surfaces as a Read
				// error; return so the defer at reason.go:237 hands off to
				// suspendProcess instead of finishProcess.
				if proc.IsSuspendRequested() && errors.Is(proc.ctx.Err(), gocontext.Canceled) {
					return
				}
				// Record the failed step with prompt data so it's visible in LLM viewer
				k.writeStepRecord(proc, step, promptResult, string(respData),
					nil, "error", fmt.Sprintf("llm read failed: %v", readErr), "", "", "", "", 0, nil)
				k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "error"}, nil, readErr, time.Since(stepStart))
				k.finishProcess(proc, ExitStatus{Code: 1, Reason: "llm read failed", Err: readErr})
				return
			}
		}

		// Parse LLM response
		var resp llmResponse
		rawResponseStr := string(respData)
		if err := json.Unmarshal(respData, &resp); err != nil {
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
		proc.TokensUsed += resp.TokensUsed
		budget := proc.ContextBudget
		tokens := proc.TokensUsed
		hasTrace := proc.TraceID != ""
		procGroups := make([]types.PGID, len(proc.groups))
		copy(procGroups, proc.groups)
		proc.mu.Unlock()

		proc.AppendTokenSnapshot(step, tokens)

		if hasTrace && k.spanRecorder != nil {
			k.spanRecorder.RecordTokens(proc.PID, resp.TokensUsed)
		}

		for _, pgid := range procGroups {
			if pool, ok := k.budgetPools.Load(pgid); ok {
				_ = pool.RecordUsage(proc.PID, resp.TokensUsed)
				break
			}
		}

		k.checkBudgetWarning(proc, step, tokens, budget)

		if budget > 0 && tokens >= budget {
			k.emitLog(proc, step, types.LogOutput, fmt.Sprintf("Token budget exceeded: %d/%d", tokens, budget), "")
			k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "budget_exceeded", "tokens": tokens, "budget": budget}, nil, nil, time.Since(stepStart))
			// Record the step that triggered budget exceeded so LLM viewer shows the interaction
			k.writeStepRecord(proc, step, promptResult, rawResponseStr, &resp,
				"error", fmt.Sprintf("budget_exceeded: %d/%d tokens", tokens, budget), "", "", "", "", 0, nil)
			k.finishProcess(proc, ExitStatus{Code: ExitSuspended, Reason: "budget_exceeded", Err: fmt.Errorf("token budget exceeded: %d/%d", tokens, budget)})
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
			if loopResult := loopDetector.Check(loopHash); loopResult != LoopNone {
				if stopped := k.handleLoopDetection(proc, loopResult, step, stepStart); stopped {
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
		hadError := proc.HasToolError
		proc.mu.Unlock()
		k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "text"}, resp.Content, nil, time.Since(stepStart))
		stepDur := time.Since(stepStart)
		if k.callbacks != nil {
			k.callbacks.OnStepComplete(proc.PID, step, "text", briefTextSummary(resp.Content), false, float64(stepDur.Microseconds())/1000.0)
		}
		// Record completed step so an in-memory SIGPAUSE/SIGRESUME cycle
		// restarts at step+1 instead of step 1 (Story 44.1 code review F5).
		proc.LastCompletedStep.Store(int64(step))
		exitCode := 0
		reason := "completed"
		if hadError {
			exitCode = 1
			reason = "completed_with_tool_errors"
		}
		k.finishProcess(proc, ExitStatus{Code: exitCode, Reason: reason})
		return
	}

	k.finishProcess(proc, ExitStatus{Code: 1, Reason: "max steps exceeded"})
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
			if err := SaveProcInfo(k.stepDataDir, *procInfo); err != nil {
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
func (k *KernelImpl) handleLoopDetection(proc *Process, status LoopStatus, step int, stepStart time.Time) bool {
	switch status {
	case LoopWarning:
		k.emitEvent(proc, "LoopDetected", map[string]any{
			"step":      step,
			"threshold": DefaultLoopThreshold,
		}, nil, nil, time.Since(stepStart))
		log.Printf("[kernel] pid=%d loop detected at step %d: same action repeated %d times", proc.PID, step, DefaultLoopThreshold)
		// Inject warning into context as RoleUser message
		msg := LoopWarningMessage(DefaultLoopThreshold)
		if err := k.ctxMgr.AppendMessage(proc.CtxID, rnixctx.RoleUser, msg); err != nil {
			log.Printf("[kernel] pid=%d failed to inject loop warning: %v", proc.PID, err)
		}
	case LoopSuspend:
		k.emitEvent(proc, "LoopSuspend", map[string]any{
			"step":      step,
			"threshold": 2 * DefaultLoopThreshold,
		}, nil, nil, time.Since(stepStart))
		log.Printf("[kernel] pid=%d loop suspend at step %d: same action repeated %d times", proc.PID, step, 2*DefaultLoopThreshold)
		if err := k.selfSuspend(proc, "loop_detected", ExitSuspended); err != nil {
			log.Printf("[kernel] pid=%d suspend failed: %v, falling back to terminate", proc.PID, err)
			k.finishProcess(proc, ExitStatus{Code: ExitError, Reason: "loop detected + suspend failed"})
		}
		return true
	}
	return false
}
