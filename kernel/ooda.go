package kernel

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/rnixai/rnix/agents"
	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// OODAPhase represents a phase in the OODA reasoning loop.
type OODAPhase string

const (
	PhaseObserve OODAPhase = "observe"
	PhaseOrient  OODAPhase = "orient"
	PhaseDecide  OODAPhase = "decide"
	PhaseAct     OODAPhase = "act"
)

// OODAActionType classifies the action chosen by the Decide phase.
type OODAActionType string

const (
	OODAToolCall OODAActionType = "tool_call"
	OODASpawn    OODAActionType = "spawn"
	OODAComplete OODAActionType = "complete"
	OODAReplan   OODAActionType = "replan"
)

// OODADecision is the structured output of the Decide phase.
type OODADecision struct {
	Action OODAActionType `json:"action"`
	Target string         `json:"target"`
	Data   json.RawMessage `json:"data,omitempty"`
	Reason string         `json:"reason"`
}

// OODAState tracks the state of an OODA reasoning loop.
type OODAState struct {
	Phase        OODAPhase
	Cycle        int
	Observations string
	Orientation  string
	Decision     *OODADecision
}

// OODA prompt templates injected into context for each phase.
const oodaOrientPromptTemplate = `[OODA Orient] Based on the following observations, evaluate:
1. Current state vs. desired state
2. Key deviations and their significance
3. Available resources and constraints

Observations:
%s

Mission: %s`

const oodaDecidePromptTemplate = `[OODA Decide] Based on orientation analysis, output a JSON decision.
You MUST respond with ONLY a valid JSON object in this exact format:
{"action": "tool_call|spawn|complete|replan", "target": "...", "data": {}, "reason": "..."}

For "spawn" action: target is the child intent, data may include {"agent": "agent-name", "model": "model-name"}.

Orientation:
%s`

// oodaReasonStep is the OODA-mode reasoning loop, replacing the linear reasonStep.
func (k *KernelImpl) oodaReasonStep(proc *Process, llmFD types.FD, opts SpawnOpts) {
	maxCycles := proc.MaxSteps

	defer func() {
		if proc.GetState() == types.StateRunning {
			k.finishProcess(proc, ExitStatus{Code: 1, Reason: "unexpected ooda exit"})
		}
	}()

	for cycle := 1; cycle <= maxCycles; cycle++ {
		cycleStart := time.Now()

		// Check context cancellation
		select {
		case <-proc.ctx.Done():
			k.emitEvent(proc, "OODACycle", map[string]any{
				"cycle":  cycle,
				"action": "cancelled",
			}, nil, proc.ctx.Err(), time.Since(cycleStart))
			k.finishProcess(proc, ExitStatus{Code: 1, Reason: "context cancelled"})
			return
		default:
		}

		proc.mu.Lock()
		proc.oodaState.Phase = PhaseObserve
		proc.oodaState.Cycle = cycle
		proc.mu.Unlock()

		// --- Observe ---
		observeStart := time.Now()
		observations := k.oodaObserve(proc)
		k.emitEvent(proc, "OODAObserve", map[string]any{
			"cycle":        cycle,
			"observations": len(observations),
		}, observations, nil, time.Since(observeStart))
		k.emitLog(proc, cycle, types.LogOODA, fmt.Sprintf("[ooda:observe] cycle=%d len=%d", cycle, len(observations)), "")

		proc.mu.Lock()
		proc.oodaState.Observations = observations
		proc.oodaState.Phase = PhaseOrient
		proc.mu.Unlock()

		// Check context cancellation after Observe
		select {
		case <-proc.ctx.Done():
			k.finishProcess(proc, ExitStatus{Code: 1, Reason: "context cancelled"})
			return
		default:
		}

		// --- Orient ---
		orientStart := time.Now()
		orientation, err := k.oodaOrient(proc, llmFD, observations, opts)
		if err != nil {
			k.emitEvent(proc, "OODAOrient", map[string]any{
				"cycle": cycle,
			}, nil, err, time.Since(orientStart))
			k.finishProcess(proc, ExitStatus{Code: 1, Reason: "orient failed", Err: err})
			return
		}
		k.emitEvent(proc, "OODAOrient", map[string]any{
			"cycle": cycle,
		}, orientation, nil, time.Since(orientStart))
		k.emitLog(proc, cycle, types.LogOODA, fmt.Sprintf("[ooda:orient] cycle=%d", cycle), "")

		proc.mu.Lock()
		proc.oodaState.Orientation = orientation
		proc.oodaState.Phase = PhaseDecide
		proc.mu.Unlock()

		// Check context cancellation after Orient
		select {
		case <-proc.ctx.Done():
			k.finishProcess(proc, ExitStatus{Code: 1, Reason: "context cancelled"})
			return
		default:
		}

		// --- Decide ---
		decideStart := time.Now()
		decision, err := k.oodaDecide(proc, llmFD, orientation, opts)
		if err != nil {
			k.emitEvent(proc, "OODADecide", map[string]any{
				"cycle": cycle,
				"error": err.Error(),
			}, nil, err, time.Since(decideStart))
			// Parse failure → replan
			decision = &OODADecision{
				Action: OODAReplan,
				Reason: fmt.Sprintf("decide parse error: %v", err),
			}
		} else {
			k.emitEvent(proc, "OODADecide", map[string]any{
				"cycle":  cycle,
				"action": string(decision.Action),
				"target": decision.Target,
			}, decision.Reason, nil, time.Since(decideStart))
		}
		k.emitLog(proc, cycle, types.LogOODA, fmt.Sprintf("[ooda:decide] cycle=%d action=%s", cycle, decision.Action), "")

		proc.mu.Lock()
		proc.oodaState.Decision = decision
		proc.oodaState.Phase = PhaseAct
		proc.mu.Unlock()

		// --- Act ---
		actStart := time.Now()
		actResult := k.oodaAct(proc, llmFD, decision, opts)
		k.emitEvent(proc, "OODAAct", map[string]any{
			"cycle":  cycle,
			"action": string(decision.Action),
		}, actResult, nil, time.Since(actStart))
		k.emitLog(proc, cycle, types.LogOODA, fmt.Sprintf("[ooda:act] cycle=%d action=%s", cycle, decision.Action), "")

		// Check completion
		if decision.Action == OODAComplete {
			proc.mu.Lock()
			proc.Result = actResult
			proc.mu.Unlock()
			k.finishProcess(proc, ExitStatus{Code: 0, Reason: "ooda_completed"})

			frameworkOverhead := time.Since(cycleStart)
			k.emitEvent(proc, "OODACycle", map[string]any{
				"cycle":              cycle,
				"framework_overhead": frameworkOverhead.Milliseconds(),
				"completed":          true,
			}, nil, nil, frameworkOverhead)
			return
		}

		frameworkOverhead := time.Since(cycleStart)
		k.emitEvent(proc, "OODACycle", map[string]any{
			"cycle":              cycle,
			"framework_overhead": frameworkOverhead.Milliseconds(),
		}, nil, nil, frameworkOverhead)
	}

	k.finishProcess(proc, ExitStatus{Code: 1, Reason: "max ooda cycles exceeded"})
}

// oodaObserve collects environment data via framework code (no LLM call).
func (k *KernelImpl) oodaObserve(proc *Process) string {
	snapshot := map[string]any{}

	// Collect process list
	procs := k.ListProcesses()
	procList := make([]map[string]any, 0, len(procs))
	for _, p := range procs {
		procList = append(procList, map[string]any{
			"pid":    p.PID,
			"state":  p.GetState().String(),
			"intent": p.Intent,
		})
	}
	snapshot["processes"] = procList

	// Collect context info
	ctxInfo, err := k.ctxMgr.GetContextInfo(proc.CtxID)
	if err == nil {
		snapshot["context"] = ctxInfo
	}

	data, _ := json.Marshal(snapshot)
	return string(data)
}

// oodaOrient evaluates observations against the mission via LLM.
func (k *KernelImpl) oodaOrient(proc *Process, llmFD types.FD, observations string, opts SpawnOpts) (string, error) {
	prompt := fmt.Sprintf(oodaOrientPromptTemplate, observations, proc.Intent)

	if err := k.ctxMgr.AppendMessage(proc.CtxID, rnixctx.RoleUser, prompt); err != nil {
		return "", fmt.Errorf("orient append prompt: %w", err)
	}

	resp, err := k.oodaCallLLM(proc, llmFD, opts)
	if err != nil {
		return "", fmt.Errorf("orient llm call: %w", err)
	}

	if err := k.ctxMgr.AppendMessage(proc.CtxID, rnixctx.RoleAssistant, resp.Content); err != nil {
		return "", fmt.Errorf("orient append response: %w", err)
	}

	proc.mu.Lock()
	proc.TokensUsed += resp.TokensUsed
	proc.mu.Unlock()

	return resp.Content, nil
}

// oodaDecide produces a structured action decision via LLM.
func (k *KernelImpl) oodaDecide(proc *Process, llmFD types.FD, orientation string, opts SpawnOpts) (*OODADecision, error) {
	prompt := fmt.Sprintf(oodaDecidePromptTemplate, orientation)

	if err := k.ctxMgr.AppendMessage(proc.CtxID, rnixctx.RoleUser, prompt); err != nil {
		return nil, fmt.Errorf("decide append prompt: %w", err)
	}

	resp, err := k.oodaCallLLM(proc, llmFD, opts)
	if err != nil {
		return nil, fmt.Errorf("decide llm call: %w", err)
	}

	if err := k.ctxMgr.AppendMessage(proc.CtxID, rnixctx.RoleAssistant, resp.Content); err != nil {
		return nil, fmt.Errorf("decide append response: %w", err)
	}

	proc.mu.Lock()
	proc.TokensUsed += resp.TokensUsed
	proc.mu.Unlock()

	var decision OODADecision
	if err := json.Unmarshal([]byte(resp.Content), &decision); err != nil {
		return nil, fmt.Errorf("decide parse json: %w", err)
	}

	return &decision, nil
}

// oodaAct executes the decision from the Decide phase.
func (k *KernelImpl) oodaAct(proc *Process, llmFD types.FD, decision *OODADecision, opts SpawnOpts) string {
	switch decision.Action {
	case OODAToolCall:
		return k.oodaActToolCall(proc, decision)

	case OODASpawn:
		return k.oodaActSpawn(proc, decision, opts)

	case OODAComplete:
		result := decision.Reason
		if result == "" {
			result = "completed"
		}
		return result

	case OODAReplan:
		// Write replan reason into context for next cycle's Observe
		_ = k.ctxMgr.AppendMessage(proc.CtxID, rnixctx.RoleUser,
			fmt.Sprintf("[OODA Replan] %s", decision.Reason))
		return decision.Reason

	default:
		return fmt.Sprintf("unknown action: %s", decision.Action)
	}
}

// oodaActToolCall executes a VFS tool call (Open/Write/Read/Close).
func (k *KernelImpl) oodaActToolCall(proc *Process, decision *OODADecision) string {
	toolFD, err := k.vfs.Open(proc.PID, decision.Target, vfs.O_RDWR)
	if err != nil {
		return fmt.Sprintf("tool open error: %v", err)
	}

	if err := k.vfs.Write(proc.ctx, proc.PID, toolFD, decision.Data); err != nil {
		_ = k.vfs.Close(proc.PID, toolFD)
		return fmt.Sprintf("tool write error: %v", err)
	}

	result, err := k.vfs.Read(proc.PID, toolFD, 1<<20)
	_ = k.vfs.Close(proc.PID, toolFD)
	if err != nil {
		return fmt.Sprintf("tool read error: %v", err)
	}

	// Append tool result to context for next cycle
	if err := k.ctxMgr.AppendToolResult(proc.CtxID, decision.Target, string(result)); err != nil {
		return fmt.Sprintf("tool append error: %v", err)
	}

	return string(result)
}

// oodaSpawnData contains optional parameters parsed from OODADecision.Data for spawn actions.
type oodaSpawnData struct {
	Agent string `json:"agent,omitempty"` // optional: child agent template name
	Model string `json:"model,omitempty"` // optional: override model
}

// oodaActSpawn creates a child process and waits for it to complete.
// Supports mission command: decision.Data may specify an agent name to load.
func (k *KernelImpl) oodaActSpawn(proc *Process, decision *OODADecision, opts SpawnOpts) string {
	childOpts := SpawnOpts{
		ParentPID: proc.PID,
		TraceID:   proc.TraceID,
	}
	if proc.TraceID != "" {
		childOpts.ParentSpanID = proc.SpanID
	}

	// Parse optional spawn data from decision
	var spawnData oodaSpawnData
	if len(decision.Data) > 0 {
		if err := json.Unmarshal(decision.Data, &spawnData); err != nil {
			return fmt.Sprintf("spawn error: invalid data payload: %v", err)
		}
	}
	if spawnData.Model != "" {
		childOpts.Model = spawnData.Model
	}

	// Load agent if specified (mission command pattern)
	var agentInfo *agents.AgentInfo
	if spawnData.Agent != "" {
		if k.agentLoader == nil {
			return fmt.Sprintf("spawn error: agent %q requested but no agent loader configured", spawnData.Agent)
		}
		var err error
		agentInfo, err = k.agentLoader(spawnData.Agent)
		if err != nil {
			return fmt.Sprintf("spawn error: agent %q load failed: %v", spawnData.Agent, err)
		}
	}

	pid, err := k.Spawn(decision.Target, agentInfo, childOpts)
	if err != nil {
		return fmt.Sprintf("spawn error: %v", err)
	}

	childProc, ok := k.GetProcess(pid)
	if !ok {
		return "spawn error: child process not found"
	}

	select {
	case exit := <-childProc.Done:
		childProc.mu.Lock()
		result := childProc.Result
		childProc.mu.Unlock()
		if exit.Code != 0 {
			return fmt.Sprintf("child exited with code %d: %s", exit.Code, exit.Reason)
		}
		return result
	case <-proc.ctx.Done():
		return "parent context cancelled"
	}
}

// oodaCallLLM performs a single LLM call via VFS (BuildPrompt → Write → Read → parse).
func (k *KernelImpl) oodaCallLLM(proc *Process, llmFD types.FD, opts SpawnOpts) (*llmResponse, error) {
	promptResult, err := k.ctxMgr.BuildPrompt(proc.CtxID)
	if err != nil {
		return nil, fmt.Errorf("build prompt: %w", err)
	}

	model := opts.Model
	req := llmRequest{
		Intent:       proc.Intent,
		SystemPrompt: promptResult.SystemPrompt,
		Model:        model,
		MaxTurns:     0,
		TimeoutMs:    opts.TimeoutMs,
		Messages:     promptResult.Messages,
	}
	reqJSON, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	if err := k.vfs.Write(proc.ctx, proc.PID, llmFD, reqJSON); err != nil {
		return nil, fmt.Errorf("llm write: %w", err)
	}

	respData, err := k.vfs.Read(proc.PID, llmFD, 1<<20)
	if err != nil {
		return nil, fmt.Errorf("llm read: %w", err)
	}

	var resp llmResponse
	if err := json.Unmarshal(respData, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response: %w", err)
	}

	return &resp, nil
}
