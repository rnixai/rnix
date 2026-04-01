package kernel

import (
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/debug"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// finishProcess terminates the process and writes the exit status to the Done channel.
func (k *KernelImpl) finishProcess(proc *Process, exit ExitStatus) {
	proc.mu.Lock()
	mcpMounts := append([]string(nil), proc.MCPMounts...)
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
		proc.mu.Lock()
		proc.checkpointErrCh = make(chan error, 1)
		proc.stepsDir = stepsDir
		proc.mu.Unlock()
	}

	defer func() {
		if proc.GetState() == types.StateRunning {
			k.finishProcess(proc, ExitStatus{Code: 1, Reason: "unexpected exit"})
		}
	}()

	var lastResultSummary string
	var consecutiveToolErrors int
	loopDetector := NewLoopDetector(DefaultLoopThreshold)

	for step := 1; maxSteps == 0 || step <= maxSteps; step++ {
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
				k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "cancelled_while_paused"}, nil, proc.ctx.Err(), time.Since(stepStart))
				k.finishProcess(proc, ExitStatus{Code: 1, Reason: "context cancelled while paused"})
				return
			}
		}

		// Check context cancellation
		select {
		case <-proc.ctx.Done():
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
		if envVars := proc.GetGdbEnvVars(); len(envVars) > 0 {
			var envSection strings.Builder
			envSection.WriteString("\n\n[GDB Environment Variables]\n")
			for k, v := range envVars {
				fmt.Fprintf(&envSection, "%s=%s\n", k, v)
			}
			sysPrompt += envSection.String()
		}
		if proc.UseNativeTools {
			if len(proc.mcpDevicePaths) > 0 {
				sysPrompt += mcpToolProtocolSnippet(proc.mcpDevicePaths)
			}
		} else {
			if proc.generatedProtocol == "" {
				vfsDefs, vfsMap := buildToolDefs(k.vfs.DeviceRegistry(), proc.AllowedDevices, proc.PlanningEnabled)
				metaDefs, metaMap := metaToolDefs(proc.PlanningEnabled)
				proc.generatedProtocol = generateToolProtocol(vfsDefs, vfsMap, metaDefs, metaMap, proc.PlanningEnabled)
			}
			sysPrompt += proc.generatedProtocol
		}
		proc.mu.Lock()
		loadedSkills := make([]string, len(proc.Skills))
		copy(loadedSkills, proc.Skills)
		proc.mu.Unlock()
		if len(loadedSkills) > 0 {
			sysPrompt += "\n\n[Loaded Skills]\nThe following skills are loaded: " +
				strings.Join(loadedSkills, ", ") +
				".\nTheir instructions are already in your system prompt. Follow them using available VFS devices." +
				"\nDo NOT try to call these skills via /dev/mcp/ or any device path."
		}
		proc.mu.Lock()
		if proc.FinalSystemPrompt == "" {
			proc.FinalSystemPrompt = sysPrompt
		}
		proc.mu.Unlock()

		req := llmRequest{
			Intent:       proc.Intent,
			SystemPrompt: sysPrompt,
			Model:        model,
			MaxTurns:     0,
			TimeoutMs:    opts.TimeoutMs,
			Messages:     promptResult.Messages,
		}
		if proc.UseNativeTools {
			req.Tools = proc.nativeToolDefs
		}
		reqJSON, err := json.Marshal(req)
		if err != nil {
			k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "error"}, nil, err, time.Since(stepStart))
			k.finishProcess(proc, ExitStatus{Code: 1, Reason: "marshal request failed", Err: err})
			return
		}

		// Write request to LLM device
		writeStart := time.Now()
		var respData []byte
		if err := k.vfs.Write(proc.ctx, proc.PID, llmFD, reqJSON); err != nil {
			k.emitEvent(proc, "Write", map[string]any{"fd": llmFD, "size": len(reqJSON), "model": model}, nil, err, time.Since(writeStart))
			fbData, fbErr := k.attemptFallback(proc, req, proc.PrimaryDevice, err, step)
			if fbErr != nil {
				reason := "llm write failed"
				if proc.FallbackDevice != "" {
					reason = "all providers exhausted"
				}
				k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "error"}, nil, fbErr, time.Since(stepStart))
				k.finishProcess(proc, ExitStatus{Code: 1, Reason: reason, Err: fbErr})
				return
			}
			respData = fbData
		} else {
			k.emitEvent(proc, "Write", map[string]any{"fd": llmFD, "size": len(reqJSON), "model": model}, nil, nil, time.Since(writeStart))
			readStart := time.Now()
			var readErr error
			respData, readErr = k.vfs.Read(proc.PID, llmFD, 1<<20)
			k.emitEvent(proc, "Read", map[string]any{"fd": llmFD, "length": 1 << 20}, len(respData), readErr, time.Since(readStart))
			if readErr != nil {
				k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "error"}, nil, readErr, time.Since(stepStart))
				k.finishProcess(proc, ExitStatus{Code: 1, Reason: "llm read failed", Err: readErr})
				return
			}
		}

		// Parse LLM response
		var resp llmResponse
		rawResponseStr := string(respData)
		if err := json.Unmarshal(respData, &resp); err != nil {
			k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "error"}, nil, err, time.Since(stepStart))
			k.finishProcess(proc, ExitStatus{Code: 1, Reason: "unmarshal response failed", Err: err})
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
			k.finishProcess(proc, ExitStatus{Code: 2, Reason: "budget_exceeded", Err: fmt.Errorf("token budget exceeded: %d/%d", tokens, budget)})
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

		// Native tool calls path
		if proc.UseNativeTools && len(resp.ToolCalls) > 0 {
			// Loop detection for native tool calls: hash first tool call
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

			shouldContinue := k.executeNativeToolCalls(proc, resp, step, stepStart, &consecutiveToolErrors)
			if !shouldContinue {
				return
			}
			k.asyncWriteCheckpoint(proc, step, consecutiveToolErrors)
			continue
		}

		// Parse action (text protocol path)
		action := parseAction(&resp)

		// Loop detection: skip complete/text actions (normal termination signals)
		if action.Type != ActionComplete && action.Type != ActionText {
			loopHash := ActionHash(string(action.Type), action.ToolPath, string(action.ToolData))
			if loopResult := loopDetector.Check(loopHash); loopResult != LoopNone {
				if stopped := k.handleLoopDetection(proc, loopResult, step, stepStart); stopped {
					return
				}
			}
		}

		if hit := proc.CheckBreakpoint(BreakpointContext{BPType: BPQuality, LLMResponse: resp.Content, StepNumber: step}); hit != nil {
			proc.GdbPause(fmt.Sprintf("quality breakpoint hit at step %d", step), hit)
			select {
			case <-proc.ctx.Done():
				k.finishProcess(proc, ExitStatus{Code: 1, Reason: "context cancelled while gdb-paused"})
				return
			default:
			}
		}

		k.emitLog(proc, step, types.LogThink, resp.Content, "")

		shouldContinue := k.handleAction(proc, action, resp, rawResponseStr, promptResult, step, stepStart, &consecutiveToolErrors, opts)
		if !shouldContinue {
			return
		}
		k.asyncWriteCheckpoint(proc, step, consecutiveToolErrors)
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
		return
	}
	ctxSnap, err := ctx.Serialize()
	if err != nil {
		return
	}

	cpData := buildCheckpointData(proc, step, json.RawMessage(ctxSnap), consecutiveToolErrors)

	go func() {
		if err := writeCheckpoint(dir, cpData); err != nil {
			select {
			case errCh <- err:
			default: // channel full, discard old error
			}
		}
	}()
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
		// TODO(30.3): replace with Suspend(pid)
		k.finishProcess(proc, ExitStatus{Code: 1, Reason: fmt.Sprintf("loop detected: same action repeated %d times", 2*DefaultLoopThreshold)})
		return true
	}
	return false
}
