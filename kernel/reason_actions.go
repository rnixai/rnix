package kernel

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/rnixai/rnix/agents"
	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// handleAction dispatches a parsed action to the appropriate handler.
// Returns true if the reasonStep loop should continue, false if the process should exit.
func (k *KernelImpl) handleAction(proc *Process, action ReasonAction, resp llmResponse,
	rawResponseStr string, promptResult *rnixctx.PromptResult, step int, stepStart time.Time,
	consecutiveToolErrors *int, opts SpawnOpts) bool {

	switch action.Type {
	case ActionText:
		return k.handleActionText(proc, action, resp, rawResponseStr, promptResult, step, stepStart)
	case ActionToolCall:
		return k.handleActionToolCall(proc, action, resp, rawResponseStr, promptResult, step, stepStart, consecutiveToolErrors)
	case ActionPlan:
		return k.handleActionPlan(proc, action, resp, rawResponseStr, promptResult, step, stepStart)
	case ActionSpawn:
		return k.handleActionSpawn(proc, action, resp, rawResponseStr, promptResult, step, stepStart, consecutiveToolErrors)
	case ActionComplete:
		return k.handleActionComplete(proc, action, resp, rawResponseStr, promptResult, step, stepStart)
	case ActionReplan:
		return k.handleActionReplan(proc, action, resp, rawResponseStr, promptResult, step, stepStart)
	case ActionSpecialize:
		return k.handleActionSpecialize(proc, action, resp, rawResponseStr, promptResult, step, stepStart, consecutiveToolErrors)
	case ActionDiscoverSkill:
		return k.handleActionDiscoverSkill(proc, action, resp, rawResponseStr, promptResult, step, stepStart, consecutiveToolErrors)
	}
	return true
}

func (k *KernelImpl) handleActionText(proc *Process, action ReasonAction, resp llmResponse,
	rawResponseStr string, promptResult *rnixctx.PromptResult, step int, stepStart time.Time) bool {

	k.emitLog(proc, step, types.LogOutput, action.Content, "")
	k.writeStepRecord(proc, step, promptResult, rawResponseStr, &resp,
		"text", briefTextSummary(action.Content), "", "", "", "", 0)

	proc.mu.Lock()
	proc.Result = action.Content
	hadError := proc.HasToolError
	proc.mu.Unlock()
	k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "text"}, action.Content, nil, time.Since(stepStart))
	stepDur := time.Since(stepStart)
	if k.callbacks != nil {
		k.callbacks.OnStepComplete(proc.PID, step, "text", briefTextSummary(action.Content), false, float64(stepDur.Microseconds())/1000.0)
	}
	exitCode := 0
	reason := "completed"
	if hadError {
		exitCode = 1
		reason = "completed_with_tool_errors"
	}
	k.finishProcess(proc, ExitStatus{Code: exitCode, Reason: reason})
	return false
}

func (k *KernelImpl) handleActionToolCall(proc *Process, action ReasonAction, resp llmResponse,
	rawResponseStr string, promptResult *rnixctx.PromptResult, step int, stepStart time.Time,
	consecutiveToolErrors *int) bool {

	appendAssistantStart := time.Now()
	if err := k.ctxMgr.AppendMessage(proc.CtxID, rnixctx.RoleAssistant, resp.Content); err != nil {
		k.emitEvent(proc, "CtxWrite", map[string]any{"cid": proc.CtxID, "op": "AppendMessage", "role": string(rnixctx.RoleAssistant)}, nil, err, time.Since(appendAssistantStart))
		k.finishProcess(proc, ExitStatus{Code: 1, Reason: "append assistant message failed", Err: err})
		return false
	}
	k.emitEvent(proc, "CtxWrite", map[string]any{"cid": proc.CtxID, "op": "AppendMessage", "role": string(rnixctx.RoleAssistant)}, nil, nil, time.Since(appendAssistantStart))

	if k.recordMgr != nil && k.recordMgr.IsRecording(proc.PID) {
		k.recordContextSnapshot(proc)
	}

	// Device permission whitelist check
	if len(proc.AllowedDevices) > 0 {
		cleanPath := path.Clean(action.ToolPath)
		allowed := false
		for _, dev := range proc.AllowedDevices {
			if cleanPath == dev || strings.HasPrefix(cleanPath, dev+"/") {
				allowed = true
				break
			}
		}
		if !allowed {
			permErr := fmt.Sprintf("permission denied: device %s not in allowed list %v", action.ToolPath, proc.AllowedDevices)
			appendPermStart := time.Now()
			if err := k.ctxMgr.AppendToolResult(proc.CtxID, action.ToolPath, permErr); err != nil {
				k.emitEvent(proc, "CtxWrite", map[string]any{"cid": proc.CtxID, "op": "AppendToolResult", "tool": action.ToolPath}, nil, err, time.Since(appendPermStart))
				k.finishProcess(proc, ExitStatus{Code: 1, Reason: "append permission error failed", Err: err})
				return false
			}
			k.emitEvent(proc, "CtxWrite", map[string]any{"cid": proc.CtxID, "op": "AppendToolResult", "tool": action.ToolPath}, nil, nil, time.Since(appendPermStart))
			k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "permission_denied", "tool": action.ToolPath}, nil, errors.New(permErr), time.Since(stepStart))
			return true
		}
	}

	// Open tool device
	toolOpenStart := time.Now()
	toolDataStr := string(action.ToolData)
	isEmpty := len(action.ToolData) == 0 || toolDataStr == "{}" || toolDataStr == "null"
	openFlags := vfs.O_RDWR
	if isEmpty {
		openFlags = vfs.O_RDONLY
	}
	toolFD, err := k.vfs.Open(proc.PID, action.ToolPath, openFlags)
	k.emitEvent(proc, "Open", map[string]any{"path": action.ToolPath, "flags": openFlags}, toolFD, err, time.Since(toolOpenStart))
	if err != nil {
		errMsg := fmt.Sprintf("Tool error (%s): open failed: %v", action.ToolPath, err)
		if appendErr := k.ctxMgr.AppendToolResult(proc.CtxID, action.ToolPath, errMsg); appendErr != nil {
			k.finishProcess(proc, ExitStatus{Code: 1, Reason: "append tool error failed", Err: appendErr})
			return false
		}
		proc.mu.Lock()
		proc.HasToolError = true
		proc.mu.Unlock()
		k.emitLog(proc, step, types.LogTool, errMsg, action.ToolPath)
		*consecutiveToolErrors++
		if *consecutiveToolErrors >= 3 {
			k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "circuit_breaker", "consecutive_errors": *consecutiveToolErrors}, nil, nil, time.Since(stepStart))
			k.finishProcess(proc, ExitStatus{Code: 1, Reason: "circuit_breaker: 3 consecutive tool errors"})
			return false
		}
		return true
	}

	// Write tool data
	if !isEmpty {
		toolWriteStart := time.Now()
		if err := k.vfs.Write(proc.ctx, proc.PID, toolFD, action.ToolData); err != nil {
			k.emitEvent(proc, "Write", map[string]any{"fd": toolFD, "size": len(action.ToolData)}, nil, err, time.Since(toolWriteStart))
			_ = k.vfs.Close(proc.PID, toolFD)
			errMsg := fmt.Sprintf("Tool error (%s): write failed: %v", action.ToolPath, err)
			if appendErr := k.ctxMgr.AppendToolResult(proc.CtxID, action.ToolPath, errMsg); appendErr != nil {
				k.finishProcess(proc, ExitStatus{Code: 1, Reason: "append tool error failed", Err: appendErr})
				return false
			}
			proc.mu.Lock()
			proc.HasToolError = true
			proc.mu.Unlock()
			k.emitLog(proc, step, types.LogTool, errMsg, action.ToolPath)
			*consecutiveToolErrors++
			if *consecutiveToolErrors >= 3 {
				k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "circuit_breaker", "consecutive_errors": *consecutiveToolErrors}, nil, nil, time.Since(stepStart))
				k.finishProcess(proc, ExitStatus{Code: 1, Reason: "circuit_breaker: 3 consecutive tool errors"})
				return false
			}
			return true
		}
		k.emitEvent(proc, "Write", map[string]any{"fd": toolFD, "size": len(action.ToolData)}, nil, nil, time.Since(toolWriteStart))
	}

	// Read tool result
	toolReadStart := time.Now()
	toolResult, err := k.vfs.Read(proc.PID, toolFD, 1<<20)
	k.emitEvent(proc, "Read", map[string]any{"fd": toolFD, "length": 1 << 20}, len(toolResult), err, time.Since(toolReadStart))
	if err != nil {
		_ = k.vfs.Close(proc.PID, toolFD)
		errMsg := fmt.Sprintf("Tool error (%s): read failed: %v", action.ToolPath, err)
		if appendErr := k.ctxMgr.AppendToolResult(proc.CtxID, action.ToolPath, errMsg); appendErr != nil {
			k.finishProcess(proc, ExitStatus{Code: 1, Reason: "append tool error failed", Err: appendErr})
			return false
		}
		proc.mu.Lock()
		proc.HasToolError = true
		proc.mu.Unlock()
		k.emitLog(proc, step, types.LogTool, errMsg, action.ToolPath)
		*consecutiveToolErrors++
		if *consecutiveToolErrors >= 3 {
			k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "circuit_breaker", "consecutive_errors": *consecutiveToolErrors}, nil, nil, time.Since(stepStart))
			k.finishProcess(proc, ExitStatus{Code: 1, Reason: "circuit_breaker: 3 consecutive tool errors"})
			return false
		}
		return true
	}

	toolContent := string(toolResult)
	if len(toolContent) > 500 {
		toolContent = toolContent[:500] + fmt.Sprintf("... (truncated, %d bytes total)", len(toolResult))
	}
	k.emitLog(proc, step, types.LogTool, toolContent, action.ToolPath)

	closeStart := time.Now()
	closeErr := k.vfs.Close(proc.PID, toolFD)
	k.emitEvent(proc, "Close", map[string]any{"fd": toolFD}, nil, closeErr, time.Since(closeStart))

	k.writeStepRecord(proc, step, promptResult, rawResponseStr, &resp,
		"tool_call", briefToolCallSummary(action.ToolPath, string(toolResult)),
		action.ToolPath, string(action.ToolData), string(toolResult), "", time.Since(toolOpenStart))

	appendToolStart := time.Now()
	if err := k.ctxMgr.AppendToolResult(proc.CtxID, action.ToolPath, string(toolResult)); err != nil {
		k.emitEvent(proc, "CtxWrite", map[string]any{"cid": proc.CtxID, "op": "AppendToolResult", "tool": action.ToolPath}, nil, err, time.Since(appendToolStart))
		k.finishProcess(proc, ExitStatus{Code: 1, Reason: "append tool result failed", Err: err})
		return false
	}
	k.emitEvent(proc, "CtxWrite", map[string]any{"cid": proc.CtxID, "op": "AppendToolResult", "tool": action.ToolPath}, nil, nil, time.Since(appendToolStart))

	// Track file reads for post-compact restore (Story 31.2)
	if strings.HasPrefix(action.ToolPath, "/dev/fs/") && isEmpty {
		filePath := strings.TrimPrefix(action.ToolPath, "/dev/fs/")
		var fileMtime time.Time
		absPath := filepath.Join(k.vfs.GetWorkDir(proc.PID), filePath)
		if info, err := os.Stat(absPath); err == nil {
			fileMtime = info.ModTime()
		}
		k.trackReadFile(proc, filePath, string(toolResult), fileMtime)
	}

	*consecutiveToolErrors = 0
	stepDur := time.Since(stepStart)
	k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "tool_call", "tool": action.ToolPath}, string(toolResult), nil, stepDur)
	if k.callbacks != nil {
		k.callbacks.OnStepComplete(proc.PID, step, "tool_call", briefToolCallSummary(action.ToolPath, string(toolResult)), false, float64(stepDur.Microseconds())/1000.0)
	}
	return true
}

func (k *KernelImpl) handleActionPlan(proc *Process, action ReasonAction, resp llmResponse,
	rawResponseStr string, promptResult *rnixctx.PromptResult, step int, stepStart time.Time) bool {

	if !proc.PlanningEnabled {
		k.emitLog(proc, step, types.LogOutput, action.Content, "")
		k.writeStepRecord(proc, step, promptResult, rawResponseStr, &resp, "plan_as_text", briefTextSummary(action.Content), "", "", "", "", 0)
		proc.mu.Lock()
		proc.Result = action.Content
		proc.mu.Unlock()
		k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "plan_as_text"}, action.Content, nil, time.Since(stepStart))
		stepDur := time.Since(stepStart)
		if k.callbacks != nil {
			k.callbacks.OnStepComplete(proc.PID, step, "text", briefTextSummary(action.Content), false, float64(stepDur.Microseconds())/1000.0)
		}
		k.finishProcess(proc, ExitStatus{Code: 0, Reason: "completed"})
		return false
	}

	planContent := fmt.Sprintf("[Plan]\n%s", string(action.ToolData))
	k.writeStepRecord(proc, step, promptResult, rawResponseStr, &resp, "plan", briefPlanSummary(action.ToolData), "", "", "", "", 0)
	appendStart := time.Now()
	if err := k.ctxMgr.AppendMessage(proc.CtxID, rnixctx.RoleAssistant, planContent); err != nil {
		k.emitEvent(proc, "CtxWrite", map[string]any{"cid": proc.CtxID, "op": "AppendMessage", "role": string(rnixctx.RoleAssistant)}, nil, err, time.Since(appendStart))
		k.finishProcess(proc, ExitStatus{Code: 1, Reason: "append plan failed", Err: err})
		return false
	}
	k.emitEvent(proc, "CtxWrite", map[string]any{"cid": proc.CtxID, "op": "AppendMessage", "role": string(rnixctx.RoleAssistant)}, nil, nil, time.Since(appendStart))

	k.emitLog(proc, step, types.LogOutput, planContent, "")
	k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "plan"}, nil, nil, time.Since(stepStart))
	stepDur := time.Since(stepStart)
	if k.callbacks != nil {
		k.callbacks.OnStepComplete(proc.PID, step, "plan", briefPlanSummary(action.ToolData), false, float64(stepDur.Microseconds())/1000.0)
	}
	return true
}

func (k *KernelImpl) handleActionSpawn(proc *Process, action ReasonAction, resp llmResponse,
	rawResponseStr string, promptResult *rnixctx.PromptResult, step int, stepStart time.Time,
	consecutiveToolErrors *int) bool {

	k.writeStepRecord(proc, step, promptResult, rawResponseStr, &resp, "spawn", action.ToolPath, "", "", "", "", 0)
	appendAssistantStart := time.Now()
	if err := k.ctxMgr.AppendMessage(proc.CtxID, rnixctx.RoleAssistant, resp.Content); err != nil {
		k.emitEvent(proc, "CtxWrite", map[string]any{"cid": proc.CtxID, "op": "AppendMessage", "role": string(rnixctx.RoleAssistant)}, nil, err, time.Since(appendAssistantStart))
		k.finishProcess(proc, ExitStatus{Code: 1, Reason: "append assistant message failed", Err: err})
		return false
	}
	k.emitEvent(proc, "CtxWrite", map[string]any{"cid": proc.CtxID, "op": "AppendMessage", "role": string(rnixctx.RoleAssistant)}, nil, nil, time.Since(appendAssistantStart))

	childOpts := SpawnOpts{
		ParentPID:     proc.PID,
		TraceID:       proc.TraceID,
		ProjectConfig: proc.ProjectConfig,
	}
	if proc.TraceID != "" {
		childOpts.ParentSpanID = proc.SpanID
	}

	var sd spawnActionData
	if len(action.ToolData) > 0 {
		_ = json.Unmarshal(action.ToolData, &sd)
	}
	if sd.Model != "" {
		childOpts.Model = sd.Model
	}

	var agentInfo *agents.AgentInfo
	spawnIntent := action.ToolPath
	if sd.Agent != "" {
		if k.agentLoader == nil {
			errMsg := fmt.Sprintf("spawn error: agent %q requested but no agent loader configured", sd.Agent)
			_ = k.ctxMgr.AppendToolResult(proc.CtxID, "spawn", errMsg)
			k.emitLog(proc, step, types.LogTool, errMsg, "spawn")
			k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "spawn_error"}, nil, fmt.Errorf("%s", errMsg), time.Since(stepStart))
			*consecutiveToolErrors++
			if *consecutiveToolErrors >= 3 {
				k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "circuit_breaker", "consecutive_errors": *consecutiveToolErrors}, nil, nil, time.Since(stepStart))
				k.finishProcess(proc, ExitStatus{Code: 1, Reason: "circuit_breaker: 3 consecutive tool errors"})
				return false
			}
			return true
		}
		var loadErr error
		agentInfo, loadErr = k.agentLoader(sd.Agent)
		if loadErr != nil {
			errMsg := fmt.Sprintf("spawn error: agent %q load failed: %v", sd.Agent, loadErr)
			_ = k.ctxMgr.AppendToolResult(proc.CtxID, "spawn", errMsg)
			k.emitLog(proc, step, types.LogTool, errMsg, "spawn")
			k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "spawn_error"}, nil, loadErr, time.Since(stepStart))
			*consecutiveToolErrors++
			if *consecutiveToolErrors >= 3 {
				k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "circuit_breaker", "consecutive_errors": *consecutiveToolErrors}, nil, nil, time.Since(stepStart))
				k.finishProcess(proc, ExitStatus{Code: 1, Reason: "circuit_breaker: 3 consecutive tool errors"})
				return false
			}
			return true
		}
	}

	childPID, spawnErr := k.Spawn(spawnIntent, agentInfo, childOpts)
	if spawnErr != nil {
		errMsg := fmt.Sprintf("spawn error: %v", spawnErr)
		_ = k.ctxMgr.AppendToolResult(proc.CtxID, "spawn", errMsg)
		k.emitLog(proc, step, types.LogTool, errMsg, "spawn")
		k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "spawn_error"}, nil, spawnErr, time.Since(stepStart))
		*consecutiveToolErrors++
		if *consecutiveToolErrors >= 3 {
			k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "circuit_breaker", "consecutive_errors": *consecutiveToolErrors}, nil, nil, time.Since(stepStart))
			k.finishProcess(proc, ExitStatus{Code: 1, Reason: "circuit_breaker: 3 consecutive tool errors"})
			return false
		}
		return true
	}

	childProc, childOk := k.GetProcess(childPID)
	if !childOk {
		errMsg := "spawn error: child process not found after spawn"
		_ = k.ctxMgr.AppendToolResult(proc.CtxID, "spawn", errMsg)
		k.emitLog(proc, step, types.LogTool, errMsg, "spawn")
		return true
	}

	var spawnResult string
	select {
	case exit := <-childProc.Done:
		childProc.mu.Lock()
		childResult := childProc.Result
		childProc.mu.Unlock()
		if exit.Code != 0 {
			spawnResult = fmt.Sprintf("child exited with code %d: %s", exit.Code, exit.Reason)
		} else {
			spawnResult = childResult
		}
	case <-proc.ctx.Done():
		k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "cancelled_waiting_child"}, nil, proc.ctx.Err(), time.Since(stepStart))
		k.finishProcess(proc, ExitStatus{Code: 1, Reason: "context cancelled while waiting for child"})
		return false
	}

	appendResultStart := time.Now()
	if err := k.ctxMgr.AppendToolResult(proc.CtxID, "spawn", spawnResult); err != nil {
		k.emitEvent(proc, "CtxWrite", map[string]any{"cid": proc.CtxID, "op": "AppendToolResult", "tool": "spawn"}, nil, err, time.Since(appendResultStart))
		k.finishProcess(proc, ExitStatus{Code: 1, Reason: "append spawn result failed", Err: err})
		return false
	}
	k.emitEvent(proc, "CtxWrite", map[string]any{"cid": proc.CtxID, "op": "AppendToolResult", "tool": "spawn"}, nil, nil, time.Since(appendResultStart))

	*consecutiveToolErrors = 0
	k.emitLog(proc, step, types.LogTool, spawnResult, "spawn")
	k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "spawn", "child_pid": childPID}, spawnResult, nil, time.Since(stepStart))
	stepDur := time.Since(stepStart)
	if k.callbacks != nil {
		k.callbacks.OnStepComplete(proc.PID, step, "spawn", fmt.Sprintf("spawn PID %d %q", childPID, spawnIntent), false, float64(stepDur.Microseconds())/1000.0)
	}
	return true
}

func (k *KernelImpl) handleActionComplete(proc *Process, action ReasonAction, resp llmResponse,
	rawResponseStr string, promptResult *rnixctx.PromptResult, step int, stepStart time.Time) bool {

	var completeData struct {
		Result string `json:"result"`
	}
	if len(action.ToolData) > 0 {
		_ = json.Unmarshal(action.ToolData, &completeData)
	}
	result := completeData.Result
	if result == "" {
		result = action.Content
	}

	k.emitLog(proc, step, types.LogOutput, result, "")
	k.writeStepRecord(proc, step, promptResult, rawResponseStr, &resp, "complete", briefTextSummary(result), "", "", "", "", 0)
	proc.mu.Lock()
	proc.Result = result
	proc.mu.Unlock()

	k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "complete"}, result, nil, time.Since(stepStart))
	stepDur := time.Since(stepStart)
	if k.callbacks != nil {
		k.callbacks.OnStepComplete(proc.PID, step, "complete", briefTextSummary(result), false, float64(stepDur.Microseconds())/1000.0)
	}
	k.finishProcess(proc, ExitStatus{Code: 0, Reason: "completed"})
	return false
}

func (k *KernelImpl) handleActionReplan(proc *Process, action ReasonAction, resp llmResponse,
	rawResponseStr string, promptResult *rnixctx.PromptResult, step int, stepStart time.Time) bool {

	var replanData struct {
		Reason string `json:"reason"`
	}
	if len(action.ToolData) > 0 {
		_ = json.Unmarshal(action.ToolData, &replanData)
	}
	reason := replanData.Reason
	if reason == "" {
		reason = action.Content
	}

	replanContent := fmt.Sprintf("[Replan] %s", reason)
	k.writeStepRecord(proc, step, promptResult, rawResponseStr, &resp, "replan", reason, "", "", "", "", 0)
	appendStart := time.Now()
	if err := k.ctxMgr.AppendMessage(proc.CtxID, rnixctx.RoleAssistant, replanContent); err != nil {
		k.emitEvent(proc, "CtxWrite", map[string]any{"cid": proc.CtxID, "op": "AppendMessage", "role": string(rnixctx.RoleAssistant)}, nil, err, time.Since(appendStart))
		k.finishProcess(proc, ExitStatus{Code: 1, Reason: "append replan failed", Err: err})
		return false
	}
	k.emitEvent(proc, "CtxWrite", map[string]any{"cid": proc.CtxID, "op": "AppendMessage", "role": string(rnixctx.RoleAssistant)}, nil, nil, time.Since(appendStart))

	k.emitLog(proc, step, types.LogOutput, replanContent, "")
	k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "replan"}, nil, nil, time.Since(stepStart))
	stepDur := time.Since(stepStart)
	if k.callbacks != nil {
		k.callbacks.OnStepComplete(proc.PID, step, "replan", briefReplanSummary(reason), false, float64(stepDur.Microseconds())/1000.0)
	}
	return true
}

func (k *KernelImpl) handleActionSpecialize(proc *Process, action ReasonAction, resp llmResponse,
	rawResponseStr string, promptResult *rnixctx.PromptResult, step int, stepStart time.Time,
	consecutiveToolErrors *int) bool {

	k.writeStepRecord(proc, step, promptResult, rawResponseStr, &resp, "specialize", action.ToolPath, "", "", "", "", 0)
	skillName := action.ToolPath
	if skillName == "" {
		errMsg := "specialize error: empty skill name"
		_ = k.ctxMgr.AppendToolResult(proc.CtxID, "specialize", errMsg)
		k.emitLog(proc, step, types.LogTool, errMsg, "specialize")
		k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "specialize_error"}, nil, nil, time.Since(stepStart))
		return true
	}
	if k.skillLoader == nil {
		errMsg := "specialize error: no skill loader configured"
		_ = k.ctxMgr.AppendToolResult(proc.CtxID, "specialize", errMsg)
		k.emitLog(proc, step, types.LogTool, errMsg, "specialize")
		k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "specialize_error"}, nil, nil, time.Since(stepStart))
		return true
	}

	proc.mu.Lock()
	alreadyLoaded := slices.Contains(proc.Skills, skillName)
	proc.mu.Unlock()
	if alreadyLoaded {
		resultMsg := fmt.Sprintf("skill %q is already loaded — its instructions are in your system prompt. Follow them using available VFS devices (/dev/fs, /dev/shell, etc.). Do NOT try to call this skill as a tool.", skillName)
		_ = k.ctxMgr.AppendToolResult(proc.CtxID, "specialize", resultMsg)
		k.emitLog(proc, step, types.LogTool, resultMsg, "specialize")
		k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "specialize_already_loaded", "skill": skillName}, nil, nil, time.Since(stepStart))
		return true
	}

	skillInfo, loadErr := k.skillLoader(skillName)
	if loadErr != nil {
		errMsg := fmt.Sprintf("specialize error: skill %q load failed: %v", skillName, loadErr)
		_ = k.ctxMgr.AppendToolResult(proc.CtxID, "specialize", errMsg)
		k.emitLog(proc, step, types.LogTool, errMsg, "specialize")
		k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "specialize_error", "skill": skillName}, nil, loadErr, time.Since(stepStart))
		return true
	}

	proc.mu.Lock()
	if slices.Contains(proc.Skills, skillName) {
		proc.mu.Unlock()
		resultMsg := fmt.Sprintf("skill %q is already loaded — its instructions are in your system prompt. Follow them using available VFS devices (/dev/fs, /dev/shell, etc.). Do NOT try to call this skill as a tool.", skillName)
		_ = k.ctxMgr.AppendToolResult(proc.CtxID, "specialize", resultMsg)
		k.emitLog(proc, step, types.LogTool, resultMsg, "specialize")
		k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "specialize_already_loaded", "skill": skillName}, nil, nil, time.Since(stepStart))
		return true
	}
	proc.Skills = append(proc.Skills, skillName)
	proc.AllowedDevices = append(proc.AllowedDevices, skillInfo.Manifest.AllowedTools()...)
	totalSkills := len(proc.Skills)
	allSkills := make([]string, totalSkills)
	copy(allSkills, proc.Skills)
	proc.mu.Unlock()

	if skillInfo.Body != "" {
		appendStart := time.Now()
		dirHint := ""
		if skillInfo.Dir != "" {
			dirHint = fmt.Sprintf("Base directory for this skill: %s\n\n", skillInfo.Dir)
		}
		if appendErr := k.ctxMgr.AppendMessage(proc.CtxID, rnixctx.RoleUser,
			fmt.Sprintf("[Dynamic Skill Loaded: %s]\nThe following are instructions from skill %q. Follow these instructions using VFS devices. Do NOT try to call this skill as a tool.\n\n%s%s", skillName, skillName, dirHint, skillInfo.Body)); appendErr != nil {
			k.emitLog(proc, step, types.LogTool, fmt.Sprintf("specialize warning: failed to inject skill body for %q: %v", skillName, appendErr), "specialize")
			k.emitEvent(proc, "CtxWrite", map[string]any{"cid": proc.CtxID, "op": "AppendMessage", "role": string(rnixctx.RoleUser)}, nil, appendErr, time.Since(appendStart))
		}
	}

	k.emitEvent(proc, "StemSpecialize", map[string]any{"skill": skillName, "total_skills": totalSkills}, nil, nil, 0)

	if k.diffMemory != nil {
		k.diffMemory.Record(proc.Intent, allSkills)
	}

	if proc.lineage != nil {
		trigger := action.Content
		if trigger == "" {
			trigger = "specialize"
		}
		proc.lineage.Record(LineageEvent{
			Timestamp: time.Now(),
			Phase:     "progressive",
			Skills:    []string{skillName},
			Trigger:   trigger,
		})
	}

	resultMsg := fmt.Sprintf("skill %q loaded successfully", skillName)
	_ = k.ctxMgr.AppendToolResult(proc.CtxID, "specialize", resultMsg)
	k.emitLog(proc, step, types.LogTool, resultMsg, "specialize")
	k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "specialize", "skill": skillName}, nil, nil, time.Since(stepStart))
	stepDur := time.Since(stepStart)
	if k.callbacks != nil {
		k.callbacks.OnStepComplete(proc.PID, step, "specialize", skillName, false, float64(stepDur.Microseconds())/1000.0)
	}
	return true
}

// discoverResult is the JSON envelope returned to the LLM for discover_skill.
type discoverResult struct {
	Query   string        `json:"query"`
	Matches []discoverHit `json:"matches"`
}

type discoverHit struct {
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Score       float64 `json:"score"`
}

func (k *KernelImpl) handleActionDiscoverSkill(proc *Process, action ReasonAction, resp llmResponse,
	rawResponseStr string, promptResult *rnixctx.PromptResult, step int, stepStart time.Time,
	consecutiveToolErrors *int) bool {

	k.writeStepRecord(proc, step, promptResult, rawResponseStr, &resp, "discover_skill", action.ToolPath, "", "", "", "", 0)

	// Parse query from ToolData or ToolPath
	query := action.ToolPath
	if query == "" && len(action.ToolData) > 0 {
		var payload struct {
			Query string `json:"query"`
		}
		if err := json.Unmarshal(action.ToolData, &payload); err == nil && payload.Query != "" {
			query = payload.Query
		}
	}
	if query == "" {
		errMsg := "discover_skill error: empty query"
		_ = k.ctxMgr.AppendToolResult(proc.CtxID, "discover_skill", errMsg)
		k.emitLog(proc, step, types.LogTool, errMsg, "discover_skill")
		k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "discover_skill_error"}, nil, nil, time.Since(stepStart))
		return true
	}

	proc.mu.Lock()
	deferred := make([]DeferredSkillMeta, len(proc.DeferredSkills))
	copy(deferred, proc.DeferredSkills)
	loadedSkills := make([]string, len(proc.Skills))
	copy(loadedSkills, proc.Skills)
	proc.mu.Unlock()

	// Score each deferred skill against query keywords
	queryLower := strings.ToLower(query)
	keywords := strings.Fields(queryLower)
	var matches []discoverHit
	for _, ds := range deferred {
		// Skip already loaded skills
		if slices.Contains(loadedSkills, ds.Name) {
			continue
		}
		score := scoreSkillMatch(ds, keywords)
		if score > 0 {
			matches = append(matches, discoverHit{
				Name:        ds.Name,
				Description: ds.Description,
				Score:       score,
			})
		}
	}

	// Sort by score descending
	for i := 0; i < len(matches); i++ {
		for j := i + 1; j < len(matches); j++ {
			if matches[j].Score > matches[i].Score {
				matches[i], matches[j] = matches[j], matches[i]
			}
		}
	}

	result := discoverResult{Query: query, Matches: matches}
	resultJSON, _ := json.Marshal(result)
	resultStr := string(resultJSON)

	_ = k.ctxMgr.AppendToolResult(proc.CtxID, "discover_skill", resultStr)
	k.emitLog(proc, step, types.LogTool, fmt.Sprintf("discover_skill: query=%q matches=%d", query, len(matches)), "discover_skill")
	k.emitEvent(proc, "ReasonStep", map[string]any{
		"step":    step,
		"action":  "discover_skill",
		"query":   query,
		"matches": len(matches),
	}, nil, nil, time.Since(stepStart))
	stepDur := time.Since(stepStart)
	if k.callbacks != nil {
		k.callbacks.OnStepComplete(proc.PID, step, "discover_skill", query, false, float64(stepDur.Microseconds())/1000.0)
	}
	return true
}

// scoreSkillMatch scores a deferred skill against query keywords.
// Returns 0.0 if no match, up to 1.0 for best match.
func scoreSkillMatch(ds DeferredSkillMeta, keywords []string) float64 {
	nameLower := strings.ToLower(ds.Name)
	descLower := strings.ToLower(ds.Description)
	hintLower := strings.ToLower(ds.SearchHint)

	var score float64
	for _, kw := range keywords {
		if kw == "" {
			continue
		}
		// Name match: highest weight
		if strings.Contains(nameLower, kw) {
			score += 0.4
		}
		// SearchHint match: high weight
		if hintLower != "" && strings.Contains(hintLower, kw) {
			score += 0.35
		}
		// Description match: moderate weight
		if strings.Contains(descLower, kw) {
			score += 0.25
		}
	}
	// Normalize by keyword count
	if len(keywords) > 0 {
		score /= float64(len(keywords))
	}
	// Cap at 1.0
	if score > 1.0 {
		score = 1.0
	}
	return score
}
