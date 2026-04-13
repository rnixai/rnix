package kernel

import (
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/rnixai/rnix/agents"
	rnixctx "github.com/rnixai/rnix/context"
	drivershell "github.com/rnixai/rnix/drivers/shell"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// executeNativeToolCalls processes native tool calls from the LLM response.
func (k *KernelImpl) executeNativeToolCalls(proc *Process, resp llmResponse, step int, stepStart time.Time, consecutiveToolErrors *int) bool {
	if err := k.ctxMgr.AppendAssistantWithToolCalls(proc.CtxID, resp.Content, convertToolCalls(resp.ToolCalls)); err != nil {
		k.finishProcess(proc, ExitStatus{Code: 1, Reason: "append assistant with tool calls failed", Err: err})
		return false
	}

	k.emitLog(proc, step, types.LogThink, resp.Content, "")

	for _, tc := range resp.ToolCalls {
		if tc.ParseError != "" {
			errMsg := fmt.Sprintf("Tool error (%s): arguments parse failed: %s", tc.Name, tc.ParseError)
			_ = k.ctxMgr.AppendToolResult(proc.CtxID, tc.ID, errMsg)
			k.emitLog(proc, step, types.LogTool, errMsg, tc.Name)
			// Record the failed step so it's visible in LLM viewer and step timeline
			k.writeDriverStepRecordFull(proc, step, tc.Name,
				fmt.Sprintf("✗ parse_error: %s", tc.Name),
				tc.Name, tc.ParseError, errMsg, time.Since(stepStart),
				nil, 0)
			*consecutiveToolErrors++
			if *consecutiveToolErrors >= 3 {
				k.finishProcess(proc, ExitStatus{Code: 1, Reason: "circuit_breaker: 3 consecutive tool errors"})
				return false
			}
			continue
		}

		mapping, ok := proc.toolMap[tc.Name]
		if !ok {
			errMsg := "error: unknown tool " + tc.Name
			_ = k.ctxMgr.AppendToolResult(proc.CtxID, tc.ID, errMsg)
			k.emitLog(proc, step, types.LogTool, errMsg, tc.Name)
			// Record the failed step so it's visible in LLM viewer
			k.writeDriverStepRecordFull(proc, step, tc.Name,
				fmt.Sprintf("✗ unknown tool: %s", tc.Name),
				tc.Name, "", errMsg, time.Since(stepStart),
				nil, 0)
			*consecutiveToolErrors++
			if *consecutiveToolErrors >= 3 {
				k.finishProcess(proc, ExitStatus{Code: 1, Reason: "circuit_breaker: 3 consecutive tool errors"})
				return false
			}
			continue
		}

		switch mapping.Type {
		case "vfs":
			result, err := k.executeNativeVFSTool(proc, tc, mapping)
			if err != nil {
				errMsg := fmt.Sprintf("Tool error (%s): %v", tc.Name, err)
				_ = k.ctxMgr.AppendToolResult(proc.CtxID, tc.ID, errMsg)
				proc.mu.Lock()
				proc.HasToolError = true
				proc.mu.Unlock()
				k.emitLog(proc, step, types.LogTool, errMsg, mapping.VFSPath)
				// Record the failed tool call so it appears in step timeline
				inputJSON, _ := json.Marshal(tc.Input)
				k.writeDriverStepRecordFull(proc, step, tc.Name,
					fmt.Sprintf("✗ %s: %v", tc.Name, err),
					mapping.VFSPath, string(inputJSON), errMsg, time.Since(stepStart),
					nil, 0)
				*consecutiveToolErrors++
				if *consecutiveToolErrors >= 3 {
					k.finishProcess(proc, ExitStatus{Code: 1, Reason: "circuit_breaker: 3 consecutive tool errors"})
					return false
				}
				continue
			}
			_ = k.ctxMgr.AppendToolResult(proc.CtxID, tc.ID, result)
			toolContent := result
			if len(toolContent) > 500 {
				toolContent = toolContent[:500] + fmt.Sprintf("... (truncated, %d bytes total)", len(result))
			}
			k.emitLog(proc, step, types.LogTool, toolContent, mapping.VFSPath)
			*consecutiveToolErrors = 0

			inputJSON, _ := json.Marshal(tc.Input)
			k.writeDriverStepRecordFull(proc, step, tc.Name,
				driverToolSummary(tc.Name, mapping.VFSPath, string(inputJSON)),
				mapping.VFSPath, string(inputJSON), result, time.Since(stepStart),
				nil, 0)

			k.emitEvent(proc, "ReasonStep", func() map[string]any {
				meta := map[string]any{
					"step":   step,
					"action": "native_tool_call",
					"tool":   tc.Name,
				}
				if tc.Name == "shell" {
					if cmd, _ := tc.Input["command"].(string); cmd != "" {
						meta["is_read_only"] = drivershell.IsReadOnlyCommand(cmd)
					}
				}
				return meta
			}(), result, nil, time.Since(stepStart))
			stepDur := time.Since(stepStart)
			if k.callbacks != nil {
				k.callbacks.OnStepComplete(proc.PID, step, "tool_call", briefToolCallSummary(mapping.VFSPath, result), false, float64(stepDur.Microseconds())/1000.0)
			}

		case "meta":
			shouldContinue := k.executeNativeMetaAction(proc, tc, mapping, step, stepStart)
			if !shouldContinue {
				return false
			}
		}
	}

	return true
}

// executeNativeVFSTool executes a VFS tool call from native function calling.
func (k *KernelImpl) executeNativeVFSTool(proc *Process, tc llmToolCall, mapping toolMapping) (string, error) {
	if len(proc.AllowedDevices) > 0 {
		cleanPath := path.Clean(mapping.VFSPath)
		allowed := false
		for _, dev := range proc.AllowedDevices {
			if cleanPath == dev || strings.HasPrefix(cleanPath, dev+"/") {
				allowed = true
				break
			}
		}
		if !allowed {
			return "", fmt.Errorf("permission denied: device %s not in allowed list %v", mapping.VFSPath, proc.AllowedDevices)
		}
	}

	switch mapping.FSOperation {
	case "read_file":
		pathStr, _ := tc.Input["path"].(string)
		if pathStr == "" {
			received, _ := json.Marshal(tc.Input)
			return "", fmt.Errorf("read_file: missing required 'path' parameter. Received: %s. Expected: {\"path\": \"<relative_path>\"}", string(received))
		}

		// Get file mtime for dedup and tracking
		var fileMtime time.Time
		absPath := filepath.Join(k.vfs.GetWorkDir(proc.PID), pathStr)
		if info, err := os.Stat(absPath); err == nil {
			fileMtime = info.ModTime()
		}

		// Dedup: if same path with same mtime was already read, return stub
		proc.mu.Lock()
		if prev, ok := proc.ReadFileState[pathStr]; ok && !fileMtime.IsZero() && prev.Mtime.Equal(fileMtime) {
			tokens := rnixctx.EstimateTokens(prev.Content)
			proc.mu.Unlock()
			return fmt.Sprintf("<file_unchanged path=%q mtime=%d tokens=%d/>", pathStr, fileMtime.UnixMilli(), tokens), nil
		}
		proc.mu.Unlock()

		vfsPath := mapping.VFSPath + "/" + pathStr
		fd, err := k.vfs.Open(proc.PID, vfsPath, vfs.O_RDONLY)
		if err != nil {
			return "", fmt.Errorf("open failed: %w", err)
		}
		data, err := k.vfs.Read(proc.PID, fd, 1<<20)
		_ = k.vfs.Close(proc.PID, fd)
		if err != nil {
			return "", fmt.Errorf("read failed: %w", err)
		}
		// Track ReadFileState for post-compact restore (Story 31.2)
		k.trackReadFile(proc, pathStr, string(data), fileMtime)
		return string(data), nil

	case "write_file":
		pathStr, _ := tc.Input["path"].(string)
		if pathStr == "" {
			received, _ := json.Marshal(tc.Input)
			return "", fmt.Errorf("write_file: missing required 'path' parameter. Received: %s. Expected: {\"path\": \"<relative_path>\", \"content\": \"<text>\"}", string(received))
		}

		// Read-before-write safety check (Story 32.1 AC#3)
		warning, rbwErr := k.checkReadBeforeWrite(proc, pathStr)
		if rbwErr != nil {
			return "", rbwErr
		}

		contentStr, _ := tc.Input["content"].(string)
		vfsPath := mapping.VFSPath + "/" + pathStr
		fd, err := k.vfs.Open(proc.PID, vfsPath, vfs.O_WRONLY)
		if err != nil {
			return "", fmt.Errorf("open failed: %w", err)
		}
		writeData, _ := json.Marshal(map[string]string{"content": contentStr})
		if err := k.vfs.Write(proc.ctx, proc.PID, fd, writeData); err != nil {
			_ = k.vfs.Close(proc.PID, fd)
			return "", fmt.Errorf("write failed: %w", err)
		}
		data, err := k.vfs.Read(proc.PID, fd, 1<<20)
		_ = k.vfs.Close(proc.PID, fd)
		if err != nil {
			return "", fmt.Errorf("read result failed: %w", err)
		}
		result := string(data)
		if warning != "" {
			result = warning + "\n" + result
		}
		k.updateReadFileMtime(proc, pathStr)
		return result, nil

	case "list_dir":
		pathStr, _ := tc.Input["path"].(string)
		if pathStr == "" {
			received, _ := json.Marshal(tc.Input)
			return "", fmt.Errorf("list_dir: missing required 'path' parameter. Received: %s. Expected: {\"path\": \"<relative_path>\"}", string(received))
		}
		vfsPath := mapping.VFSPath + "/" + pathStr
		fd, err := k.vfs.Open(proc.PID, vfsPath, vfs.O_WRONLY)
		if err != nil {
			return "", fmt.Errorf("open failed: %w", err)
		}
		writeData, _ := json.Marshal(map[string]string{"op": "list"})
		if err := k.vfs.Write(proc.ctx, proc.PID, fd, writeData); err != nil {
			_ = k.vfs.Close(proc.PID, fd)
			return "", fmt.Errorf("write failed: %w", err)
		}
		data, err := k.vfs.Read(proc.PID, fd, 1<<20)
		_ = k.vfs.Close(proc.PID, fd)
		if err != nil {
			return "", fmt.Errorf("read result failed: %w", err)
		}
		return string(data), nil

	case "edit_file":
		pathStr, _ := tc.Input["path"].(string)
		if pathStr == "" {
			received, _ := json.Marshal(tc.Input)
			return "", fmt.Errorf("edit_file: missing required 'path' parameter. Received: %s", string(received))
		}

		// Read-before-write safety check (Story 32.1 AC#3)
		warning, rbwErr := k.checkReadBeforeWrite(proc, pathStr)
		if rbwErr != nil {
			return "", rbwErr
		}

		// Build the writeRequest for the driver
		editReq := map[string]any{
			"op":         "edit",
			"old_string": tc.Input["old_string"],
			"new_string": tc.Input["new_string"],
		}
		if v, ok := tc.Input["replace_all"]; ok {
			editReq["replace_all"] = v
		}

		vfsPath := mapping.VFSPath + "/" + pathStr
		fd, err := k.vfs.Open(proc.PID, vfsPath, vfs.O_WRONLY)
		if err != nil {
			return "", fmt.Errorf("open failed: %w", err)
		}
		writeData, _ := json.Marshal(editReq)
		if err := k.vfs.Write(proc.ctx, proc.PID, fd, writeData); err != nil {
			_ = k.vfs.Close(proc.PID, fd)
			return "", fmt.Errorf("write failed: %w", err)
		}
		data, err := k.vfs.Read(proc.PID, fd, 1<<20)
		_ = k.vfs.Close(proc.PID, fd)
		if err != nil {
			return "", fmt.Errorf("read result failed: %w", err)
		}
		result := string(data)
		if warning != "" {
			result = warning + "\n" + result
		}
		k.updateReadFileMtime(proc, pathStr)
		return result, nil

	case "glob":
		patternStr, _ := tc.Input["pattern"].(string)
		if patternStr == "" {
			received, _ := json.Marshal(tc.Input)
			return "", fmt.Errorf("glob: missing required 'pattern' parameter. Received: %s", string(received))
		}
		globReq := map[string]any{"op": "glob", "pattern": patternStr}
		if v, ok := tc.Input["path"]; ok {
			globReq["path"] = v
		}
		if v, ok := tc.Input["head_limit"]; ok {
			globReq["head_limit"] = v
		}

		// Open at the workDir root (no subpath)
		fd, err := k.vfs.Open(proc.PID, mapping.VFSPath+"/.", vfs.O_WRONLY)
		if err != nil {
			return "", fmt.Errorf("open failed: %w", err)
		}
		writeData, _ := json.Marshal(globReq)
		if err := k.vfs.Write(proc.ctx, proc.PID, fd, writeData); err != nil {
			_ = k.vfs.Close(proc.PID, fd)
			return "", fmt.Errorf("write failed: %w", err)
		}
		data, err := k.vfs.Read(proc.PID, fd, 1<<20)
		_ = k.vfs.Close(proc.PID, fd)
		if err != nil {
			return "", fmt.Errorf("read result failed: %w", err)
		}
		return string(data), nil

	case "grep":
		patternStr, _ := tc.Input["pattern"].(string)
		if patternStr == "" {
			received, _ := json.Marshal(tc.Input)
			return "", fmt.Errorf("grep: missing required 'pattern' parameter. Received: %s", string(received))
		}
		grepReq := map[string]any{"op": "grep", "pattern": patternStr}
		for _, key := range []string{"path", "output_mode", "case_insensitive", "glob", "context", "head_limit"} {
			if v, ok := tc.Input[key]; ok {
				grepReq[key] = v
			}
		}

		fd, err := k.vfs.Open(proc.PID, mapping.VFSPath+"/.", vfs.O_WRONLY)
		if err != nil {
			return "", fmt.Errorf("open failed: %w", err)
		}
		writeData, _ := json.Marshal(grepReq)
		if err := k.vfs.Write(proc.ctx, proc.PID, fd, writeData); err != nil {
			_ = k.vfs.Close(proc.PID, fd)
			return "", fmt.Errorf("write failed: %w", err)
		}
		data, err := k.vfs.Read(proc.PID, fd, 1<<20)
		_ = k.vfs.Close(proc.PID, fd)
		if err != nil {
			return "", fmt.Errorf("read result failed: %w", err)
		}
		return string(data), nil

	default:
		inputData, _ := json.Marshal(tc.Input)
		fd, err := k.vfs.Open(proc.PID, mapping.VFSPath, vfs.O_RDWR)
		if err != nil {
			return "", fmt.Errorf("open failed: %w", err)
		}
		if err := k.vfs.Write(proc.ctx, proc.PID, fd, inputData); err != nil {
			_ = k.vfs.Close(proc.PID, fd)
			return "", fmt.Errorf("write failed: %w", err)
		}
		data, err := k.vfs.Read(proc.PID, fd, 1<<20)
		_ = k.vfs.Close(proc.PID, fd)
		if err != nil {
			return "", fmt.Errorf("read failed: %w", err)
		}
		return string(data), nil
	}
}

// checkReadBeforeWrite validates that the file was read before writing (Story 32.1 AC#3).
// Returns (warning, nil) for mtime drift, ("", error) to block writes to unread files,
// and ("", nil) when all clear. Allows writes to new files (file doesn't exist on disk).
func (k *KernelImpl) checkReadBeforeWrite(proc *Process, pathStr string) (string, error) {
	absPath := filepath.Join(k.vfs.GetWorkDir(proc.PID), pathStr)

	// Allow writing new files without prior read
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		return "", nil
	}

	proc.mu.Lock()
	entry, hasRead := proc.ReadFileState[pathStr]
	proc.mu.Unlock()

	if !hasRead {
		return "", fmt.Errorf("file %q must be read before writing — read the file first to avoid data loss", pathStr)
	}

	// Check mtime drift: file changed on disk since last read
	if !entry.Mtime.IsZero() {
		if info, err := os.Stat(absPath); err == nil {
			currentMtime := info.ModTime()
			if !currentMtime.Equal(entry.Mtime) {
				return fmt.Sprintf("⚠️  WARNING: %q has been modified on disk since last read (read mtime: %s, current mtime: %s). Re-read the file to see latest changes.",
					pathStr, entry.Mtime.Format(time.RFC3339Nano), currentMtime.Format(time.RFC3339Nano)), nil
			}
		}
	}

	return "", nil
}

// updateReadFileMtime refreshes the mtime in ReadFileState after a successful write/edit
// so subsequent dedup checks use the post-write mtime.
func (k *KernelImpl) updateReadFileMtime(proc *Process, pathStr string) {
	absPath := filepath.Join(k.vfs.GetWorkDir(proc.PID), pathStr)
	info, err := os.Stat(absPath)
	if err != nil {
		return
	}
	proc.mu.Lock()
	defer proc.mu.Unlock()
	if entry, ok := proc.ReadFileState[pathStr]; ok {
		entry.Mtime = info.ModTime()
		proc.ReadFileState[pathStr] = entry
	}
}

func (k *KernelImpl) executeNativeMetaAction(proc *Process, tc llmToolCall, mapping toolMapping, step int, stepStart time.Time) bool {
	switch mapping.Action {
	case ActionComplete:
		resultStr, _ := tc.Input["result"].(string)
		proc.mu.Lock()
		proc.Result = resultStr
		hadError := proc.HasToolError
		proc.mu.Unlock()
		k.emitLog(proc, step, types.LogOutput, resultStr, "")
		k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "complete"}, resultStr, nil, time.Since(stepStart))
		stepDur := time.Since(stepStart)
		if k.callbacks != nil {
			k.callbacks.OnStepComplete(proc.PID, step, "complete", briefTextSummary(resultStr), false, float64(stepDur.Microseconds())/1000.0)
		}
		exitCode := 0
		reason := "completed"
		if hadError {
			exitCode = 1
			reason = "completed_with_tool_errors"
		}
		k.finishProcess(proc, ExitStatus{Code: exitCode, Reason: reason})
		return false

	case ActionSpawn:
		intentStr, _ := tc.Input["intent"].(string)
		agentStr, _ := tc.Input["agent"].(string)
		modelStr, _ := tc.Input["model"].(string)
		childOpts := SpawnOpts{Model: modelStr}
		var agentInfo *agents.AgentInfo
		if agentStr != "" && k.agentLoader != nil {
			if ai, err := k.agentLoader(agentStr); err == nil {
				agentInfo = ai
			}
		}
		childPID, err := k.Spawn(intentStr, agentInfo, childOpts)
		if err != nil {
			errMsg := fmt.Sprintf("spawn failed: %v", err)
			_ = k.ctxMgr.AppendToolResult(proc.CtxID, tc.ID, errMsg)
			return true
		}
		childExit, _ := k.Wait(childPID)
		childProc, _ := k.GetProcess(childPID)
		result := ""
		if childProc != nil {
			result = childProc.Result
		}
		if result == "" {
			result = fmt.Sprintf("child PID %d exited: %s (code %d)", childPID, childExit.Reason, childExit.Code)
		}
		_ = k.ctxMgr.AppendToolResult(proc.CtxID, tc.ID, result)
		k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "spawn", "child_pid": childPID}, result, nil, time.Since(stepStart))
		return true

	case ActionReplan:
		reasonStr, _ := tc.Input["reason"].(string)
		msg := fmt.Sprintf("Replanning: %s", reasonStr)
		_ = k.ctxMgr.AppendToolResult(proc.CtxID, tc.ID, msg)
		k.emitLog(proc, step, types.LogOutput, msg, "")
		k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "replan"}, msg, nil, time.Since(stepStart))
		return true

	case ActionSpecialize:
		skillName, _ := tc.Input["skill_name"].(string)
		var resultMsg string
		if k.skillLoader != nil {
			if skill, err := k.skillLoader(skillName); err == nil {
				proc.mu.Lock()
				proc.Skills = append(proc.Skills, skillName)
				proc.AllowedDevices = append(proc.AllowedDevices, skill.Manifest.AllowedTools()...)
				if skill.Body != "" {
					if proc.SkillBodies == nil {
						proc.SkillBodies = make(map[string]string)
					}
					body := skill.Body
					if skill.Dir != "" {
						body = "Base directory for this skill: " + skill.Dir + "\n\n" + body
					}
					proc.SkillBodies[skillName] = body
				}
				proc.mu.Unlock()
				// Legacy path: inject skill body into context for non-section processes
				if !proc.HasSections && skill.Body != "" {
					body := skill.Body
					if skill.Dir != "" {
						body = "Base directory for this skill: " + skill.Dir + "\n\n" + body
					}
					header := fmt.Sprintf("[Dynamic Skill Loaded: %s]", skillName)
					_ = k.ctxMgr.AppendMessage(proc.CtxID, rnixctx.RoleUser, header+"\n\n"+body)
				}
				resultMsg = fmt.Sprintf("Skill '%s' loaded successfully", skillName)
			} else {
				resultMsg = fmt.Sprintf("Failed to load skill '%s': %v", skillName, err)
			}
		} else {
			resultMsg = "skill loader not available"
		}
		_ = k.ctxMgr.AppendToolResult(proc.CtxID, tc.ID, resultMsg)
		k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "specialize", "skill": skillName}, resultMsg, nil, time.Since(stepStart))
		return true

	case ActionDiscoverSkill:
		query, _ := tc.Input["query"].(string)
		matches, err := discoverSkills(proc, query)
		if err != nil {
			_ = k.ctxMgr.AppendToolResult(proc.CtxID, tc.ID, "discover_skill error: "+err.Error())
			return true
		}
		resultStr := discoverResultJSON(query, matches)
		_ = k.ctxMgr.AppendToolResult(proc.CtxID, tc.ID, resultStr)
		k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "discover_skill", "query": query, "matches": len(matches)}, nil, nil, time.Since(stepStart))
		return true

	case ActionDeferredSkillPlaceholder:
		// LLM tried to call a deferred skill placeholder — guide it to use discover_skill + specialize
		skillName := strings.TrimPrefix(tc.Name, "skill_")
		msg := fmt.Sprintf("This skill (%s) is deferred and not yet loaded. Use discover_skill to search for it, then specialize to load it.", skillName)
		_ = k.ctxMgr.AppendToolResult(proc.CtxID, tc.ID, msg)
		return true

	case ActionPlan:
		stepsAny, _ := tc.Input["steps"].([]any)
		reasonStr, _ := tc.Input["reason"].(string)
		var steps []string
		for _, s := range stepsAny {
			if str, ok := s.(string); ok {
				steps = append(steps, str)
			}
		}
		var planContent strings.Builder
		fmt.Fprintf(&planContent, "Plan (%s):\n", reasonStr)
		for i, s := range steps {
			fmt.Fprintf(&planContent, "  %d. %s\n", i+1, s)
		}
		_ = k.ctxMgr.AppendToolResult(proc.CtxID, tc.ID, planContent.String())
		k.emitLog(proc, step, types.LogOutput, planContent.String(), "")
		k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "plan"}, planContent.String(), nil, time.Since(stepStart))
		stepDur := time.Since(stepStart)
		if k.callbacks != nil {
			k.callbacks.OnStepComplete(proc.PID, step, "plan", "Created plan with "+fmt.Sprintf("%d steps", len(steps)), false, float64(stepDur.Microseconds())/1000.0)
		}
		return true
	}

	return true
}
