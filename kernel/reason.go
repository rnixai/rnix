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

// finishProcess terminates the process and writes the exit status to the Done channel.
func (k *KernelImpl) finishProcess(proc *Process, exit ExitStatus) {
	proc.mu.Lock()
	mcpMounts := append([]string(nil), proc.MCPMounts...)
	// Backfill Result from exit reason when empty — ensures Dashboard always has
	// diagnostic info for failed processes (e.g., empty LLM response, context cancelled).
	if proc.Result == "" && exit.Code != 0 && exit.Reason != "" {
		proc.Result = exit.Reason
	}
	proc.mu.Unlock()
	if k.mountMgr != nil {
		for _, mountPath := range mcpMounts {
			unmountStart := time.Now()
			err := k.mountMgr.Unmount(mountPath)
			k.emitEvent(proc, "Unmount", map[string]any{
				"path": mountPath,
				"auto": true,
			}, nil, err, time.Since(unmountStart))
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
				go k.reapProcess(proc)
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

	stepBaseDir := k.stepDataDir
	if stepBaseDir == "" {
		if proc.ProjectConfig != nil && proc.ProjectConfig.ProjectDir != "" {
			stepBaseDir = filepath.Join(proc.ProjectConfig.ProjectDir, ".rnix")
		}
	}
	if stepBaseDir != "" {
		sw, err := NewStepWriter(stepBaseDir, proc.UUID)
		if err == nil {
			proc.mu.Lock()
			proc.stepWriter = sw
			proc.mu.Unlock()
		}
		ew, err := NewEventWriter(stepBaseDir, proc.UUID)
		if err == nil {
			proc.mu.Lock()
			proc.eventWriter = ew
			proc.mu.Unlock()
		}
		// Initialize checkpoint channel and resolve steps directory (Story 30.2)
		stepsDir := filepath.Join(stepBaseDir, "data", "steps", proc.UUID)
		scratchDir := filepath.Join(stepBaseDir, "data", "scratch", proc.UUID)
		_ = os.MkdirAll(scratchDir, 0o755)
		proc.mu.Lock()
		proc.checkpointErrCh = make(chan error, 1)
		proc.stepsDir = stepsDir
		proc.scratchDir = scratchDir
		proc.mu.Unlock()
	}

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
	var consecutiveToolErrors int
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
					nil, "error", fmt.Sprintf("%s: %v", reason, fbErr), "", "", "", "", 0)
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
				// Record the failed step with prompt data so it's visible in LLM viewer
				k.writeStepRecord(proc, step, promptResult, string(respData),
					nil, "error", fmt.Sprintf("llm read failed: %v", readErr), "", "", "", "", 0)
				k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "error"}, nil, readErr, time.Since(stepStart))
				k.finishProcess(proc, ExitStatus{Code: 1, Reason: "llm read failed", Err: readErr})
				return
			}
		}

		// Parse LLM response
		var resp llmResponse
		rawResponseStr := string(respData)
		if err := json.Unmarshal(respData, &resp); err != nil {
			// Record the failed step with raw response so it's visible in LLM viewer
			k.writeStepRecord(proc, step, promptResult, rawResponseStr, nil,
				"error", fmt.Sprintf("unmarshal response failed: %v", err), "", "", "", "", 0)
			k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "error"}, nil, err, time.Since(stepStart))
			k.finishProcess(proc, ExitStatus{Code: 1, Reason: "unmarshal response failed", Err: err})
			return
		}

		// max_turns: agentic loop (CLI side) hit its turn limit. Record and exit
		// with a distinct reason so the Dashboard / parent process can react
		// differently from a true "empty response" configuration error. (R2)
		if resp.StopReason == "max_turns" && len(resp.ToolCalls) == 0 {
			k.writeStepRecord(proc, step, promptResult, rawResponseStr, &resp,
				"error", "max_turns_reached", "", "", "", "", 0)
			k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "max_turns_reached"}, nil, nil, time.Since(stepStart))
			k.finishProcess(proc, ExitStatus{Code: 2, Reason: "max_turns_reached"})
			return
		}

		// Guard: detect empty LLM response (common symptom of misconfigured provider)
		if resp.Content == "" && resp.TokensUsed == 0 && len(resp.ToolCalls) == 0 {
			emptyErr := fmt.Errorf("LLM returned empty response (content=\"\", tokens=0, no tool_calls) — check provider/model configuration")
			k.writeStepRecord(proc, step, promptResult, rawResponseStr, &resp,
				"error", "empty_llm_response", "", "", "", "", 0)
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
				"error", fmt.Sprintf("budget_exceeded: %d/%d tokens", tokens, budget), "", "", "", "", 0)
			k.finishProcess(proc, ExitStatus{Code: 2, Reason: "budget_exceeded", Err: fmt.Errorf("token budget exceeded: %d/%d", tokens, budget)})
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
				"", "", "", "", 0)
			if err := k.selfSuspend(proc, "budget_exhausted"); err != nil {
				log.Printf("[kernel] pid=%d budget suspend failed: %v, falling back to terminate", proc.PID, err)
				k.finishProcess(proc, ExitStatus{Code: 1, Reason: "budget_exhausted + suspend failed"})
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

			shouldContinue := k.executeToolCalls(proc, resp, step, stepStart, &consecutiveToolErrors, promptResult, rawResponseStr)
			if !shouldContinue {
				return
			}
			// Auto-compact check (Story 31.2): after tool calls processed, before checkpoint
			k.autoCompactIfNeeded(proc, step)
			k.asyncWriteCheckpoint(proc, step, consecutiveToolErrors)
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
			"text", briefTextSummary(resp.Content), "", "", "", "", 0)

		proc.mu.Lock()
		proc.Result = resp.Content
		hadError := proc.HasToolError
		proc.mu.Unlock()
		k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "text"}, resp.Content, nil, time.Since(stepStart))
		stepDur := time.Since(stepStart)
		if k.callbacks != nil {
			k.callbacks.OnStepComplete(proc.PID, step, "text", briefTextSummary(resp.Content), false, float64(stepDur.Microseconds())/1000.0)
		}
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
func (k *KernelImpl) asyncWriteCheckpoint(proc *Process, step int, consecutiveToolErrors int) {
	proc.mu.Lock()
	dir := proc.stepsDir
	errCh := proc.checkpointErrCh
	proc.mu.Unlock()
	if dir == "" || errCh == nil {
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
		if err := k.selfSuspend(proc, "loop_detected"); err != nil {
			log.Printf("[kernel] pid=%d suspend failed: %v, falling back to terminate", proc.PID, err)
			k.finishProcess(proc, ExitStatus{Code: 1, Reason: "loop detected + suspend failed"})
		}
		return true
	}
	return false
}
