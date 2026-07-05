package kernel

import (
	gocontext "context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/rnixai/rnix/agents"
	rnixctx "github.com/rnixai/rnix/context"
	drivershell "github.com/rnixai/rnix/drivers/shell"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/skills"
	"github.com/rnixai/rnix/vfs"
)

// TotalToolErrorThreshold is the cumulative error count (across all fingerprints)
// that triggers the circuit breaker. This prevents agents from bypassing the
// per-fingerprint breaker by alternating between different failing tools.
const TotalToolErrorThreshold = 6

// errFingerprintCounter tracks consecutive tool errors by fingerprint (errorCode|toolPath).
// When a new fingerprint appears, count resets to 1. Same fingerprint increments count.
// The total field accumulates across all fingerprints and decays by 1 on each
// recordSuccess() (it is NOT zeroed), so a single success cannot erase the
// cross-fingerprint history the cumulative breaker relies on.
type errFingerprintCounter struct {
	fp    string // "<errCode>|<toolPath>"
	count int
	total int
}

func (c *errFingerprintCounter) bump(errCode, toolPath string) int {
	fp := errCode + "|" + toolPath
	c.total++
	if c.fp != fp {
		c.fp = fp
		c.count = 1
	} else {
		c.count++
	}
	return c.count
}

// recordSuccess records a unit of progress (a successful tool call or a clean
// child exit). The consecutive streak (count/fp) is fully cleared, but the
// cumulative pressure (total) only decays by 1 so a single success cannot erase
// the cross-fingerprint history — closing the "error→success→error" bypass while
// letting healthy long-running processes drain total back to 0 over time.
func (c *errFingerprintCounter) recordSuccess() {
	c.fp = ""
	c.count = 0
	if c.total > 0 {
		c.total--
	}
}

// appendToolResult appends a tool result to the context. Errors here used
// to be silently dropped with `_ =` at every call site, which masked
// context-overflow bugs and produced protocol-illegal LLM requests
// downstream (DeepSeek HTTP 400 "insufficient tool messages following
// tool_calls message"). With ErrContextFull surfaced atomically by
// AppendAssistantWithToolCalls, this should rarely fail; the emitted
// ToolResultDropped event is a visibility safety net.
func (k *KernelImpl) appendToolResult(proc *Process, step int, toolCallID, toolName, content string) error {
	if err := k.ctxMgr.AppendToolResult(proc.CtxID, toolCallID, content); err != nil {
		k.emitEvent(proc, "ToolResultDropped", map[string]any{
			"cid":          int(proc.CtxID),
			"step":         step,
			"tool_call_id": toolCallID,
			"tool":         toolName,
		}, nil, err, 0)
		return err
	}
	return nil
}

// executeToolCalls processes native tool calls from the LLM response.
//
// 返回值（spec-step-inspector-data-fidelity）：
//   - toolCalls：vfs 路径累积的 ToolCallRecord 列表（含成功/失败）;由 caller 在
//     step 完成时合并写入一行 StepRecord,避免循环内多次 writeStepRecord 导致
//     ReadStep 去重时丢前 N-1 个 call。
//   - shouldContinue：与原返回值同语义。
//
// 注意：permission_denied / meta action / think 等边缘路径仍保留独立 writeStepRecord
// 调用（向后兼容旧 reader,且这些路径单 step 内最多触发一次）。
// isCancellation returns true for errors that represent user/process cancellation or timeouts.
// Such errors must not contribute to the circuit_breaker counter — they are flow control, not
// tool failures.
func isCancellation(err error) bool {
	return errors.Is(err, gocontext.Canceled) || errors.Is(err, gocontext.DeadlineExceeded)
}

// extractErrCode pulls the categorized ErrCode out of a DriverError, falling back to
// ErrInternal when the error is not a DriverError. The fallback keeps internal errors
// clustered per device path rather than per arbitrary error chain.
func extractErrCode(err error) string {
	var de *types.DriverError
	if errors.As(err, &de) {
		return string(de.Code)
	}
	return string(types.ErrInternal)
}

// circuitBreakerReason formats the exit reason string for traceability.
// It distinguishes between per-fingerprint trips and cumulative total trips.
func circuitBreakerReason(counter *errFingerprintCounter) string {
	if counter.count >= 3 {
		return fmt.Sprintf("circuit_breaker: %d consecutive errors with fingerprint %s", counter.count, counter.fp)
	}
	return fmt.Sprintf("circuit_breaker: %d cumulative tool errors across fingerprints", counter.total)
}

// bumpToolError applies per-step deduplication then bumps the cross-step counter.
// Returns (count, tripped) where tripped=true when same-fingerprint count >= 3
// OR cumulative total errors >= TotalToolErrorThreshold.
// Cancellation errors are filtered upstream and never reach here.
func bumpToolError(counter *errFingerprintCounter, seen map[string]bool, errCode, toolPath string) (int, bool) {
	fp := errCode + "|" + toolPath
	if seen[fp] {
		return counter.count, false
	}
	seen[fp] = true
	n := counter.bump(errCode, toolPath)
	return n, n >= 3 || counter.total >= TotalToolErrorThreshold
}

// spawnBreakerOutcome applies the circuit-breaker policy for a child spawn that
// finished, based on its exit code (spec spawn-recursion-no-permission, scheme
// 3). A child that finished cleanly (ExitOK) is progress and decays the breaker
// pressure (recordSuccess). A child that exited abnormally (ExitError) is a
// no-progress "spawn-to-escape" and bumps the shared "INTERNAL|spawn"
// fingerprint — returns tripped=true when the breaker should fire. Because a
// circuit-broken child itself exits with code 1, the trip cascades up the
// recursion chain. Suspended / context-full children are neutral (neither decay
// nor bump). The shared fingerprint means a failing child and a failing spawn()
// accumulate together, so alternating failure modes cannot silently bypass the breaker.
func spawnBreakerOutcome(counter *errFingerprintCounter, seen map[string]bool, code int) (tripped bool) {
	switch code {
	case ExitOK:
		counter.recordSuccess()
	case ExitError:
		_, tripped = bumpToolError(counter, seen, string(types.ErrInternal), "spawn")
	}
	return tripped
}

func (k *KernelImpl) executeToolCalls(proc *Process, resp llmResponse, step int, stepStart time.Time, counter *errFingerprintCounter, promptResult *rnixctx.PromptResult, rawResponseStr string) ([]types.ToolCallRecord, bool) {
	// Spec step-inspector-data-fidelity：vfs 主路径累积器,toolLoop 内每个 call append 一项;
	// caller 在 step 完成时合并写入一行 StepRecord。声明在函数顶部以便所有 return 点引用。
	var toolCalls []types.ToolCallRecord

	// Per-step deduplication: a fingerprint that already bumped within this step
	// is recorded but not counted again. Single step with N identical errors counts as 1.
	seen := map[string]bool{}

	if err := k.preCompactForToolCalls(proc, len(resp.ToolCalls), step); err != nil {
		log.Printf("[kernel] pid=%d precompact warning: %v", proc.PID, err)
	}

	appendStart := time.Now()
	if err := k.ctxMgr.AppendAssistantWithToolCalls(proc.CtxID, resp.Content, resp.Reasoning, convertReasoningBlocks(resp.ReasoningBlocks), convertToolCalls(resp.ToolCalls)); err != nil {
		k.emitEvent(proc, "CtxWrite", map[string]any{"cid": proc.CtxID, "op": "AppendAssistantWithToolCalls"}, nil, err, time.Since(appendStart))
		if errors.Is(err, rnixctx.ErrContextFull) {
			if suspErr := k.selfSuspend(proc, "context_full", ExitContextFull); suspErr != nil {
				log.Printf("[kernel] pid=%d context_full suspend failed: %v, falling back to terminate", proc.PID, suspErr)
				k.finishProcess(proc, ExitStatus{Code: ExitError, Reason: "context_full + suspend failed", Err: err})
			}
			return toolCalls, false
		}
		k.finishProcess(proc, ExitStatus{Code: ExitError, Reason: "append assistant with tool calls failed", Err: err})
		return toolCalls, false
	}
	k.emitEvent(proc, "CtxWrite", map[string]any{"cid": proc.CtxID, "op": "AppendAssistantWithToolCalls"}, nil, nil, time.Since(appendStart))

	k.emitLog(proc, step, types.LogThink, resp.Content, "")

	type droppedResult struct {
		toolCallID string
		toolName   string
		content    string
	}
	var dropped *droppedResult

toolLoop:
	for _, tc := range resp.ToolCalls {
		if tc.ParseError != "" {
			errMsg := fmt.Sprintf("Tool error (%s): arguments parse failed: %s", tc.Name, tc.ParseError)
			if err := k.appendToolResult(proc, step, tc.ID, tc.Name, errMsg); err != nil {
				dropped = &droppedResult{tc.ID, tc.Name, errMsg}
				break
			}
			k.emitLog(proc, step, types.LogTool, errMsg, tc.Name)
			k.writeDriverStepRecordFull(proc, step, tc.Name,
				fmt.Sprintf("✗ parse_error: %s", tc.Name),
				tc.Name, tc.ParseError, errMsg, time.Since(stepStart),
				nil, 0)
			if _, tripped := bumpToolError(counter, seen, string(types.ErrInvalid), tc.Name); tripped {
				k.finishProcess(proc, ExitStatus{Code: 1, Reason: circuitBreakerReason(counter)})
				return toolCalls, false
			}
			continue
		}

		// Handle LLM thinking output — not a tool error, just record and continue
		if tc.Name == "think" || tc.Name == "thinking" {
			thinkContent, _ := tc.Input["content"].(string)
			if thinkContent == "" {
				thinkContent = fmt.Sprintf("%v", tc.Input)
			}
			_ = k.appendToolResult(proc, step, tc.ID, tc.Name, "ok")
			k.writeDriverStepRecordFull(proc, step, tc.Name,
				"think", tc.Name, "", thinkContent, time.Since(stepStart),
				nil, 0)
			continue
		}

		mapping, ok := proc.toolMap[tc.Name]
		if !ok {
			if strings.HasPrefix(tc.Name, "/mnt/") {
				mapping = toolMapping{Type: "vfs", VFSPath: tc.Name}
				ok = true
			}
		}
		if !ok {
			errMsg := "error: unknown tool " + tc.Name
			appendToolStart := time.Now()
			if err := k.appendToolResult(proc, step, tc.ID, tc.Name, errMsg); err != nil {
				dropped = &droppedResult{tc.ID, tc.Name, errMsg}
				break
			}
			k.emitEvent(proc, "CtxWrite", map[string]any{"cid": proc.CtxID, "op": "AppendToolResult", "tool": tc.Name}, nil, nil, time.Since(appendToolStart))
			k.emitLog(proc, step, types.LogTool, errMsg, tc.Name)
			k.writeStepRecord(proc, step, promptResult, rawResponseStr, &resp,
				"permission_denied", fmt.Sprintf("✗ unknown tool: %s", tc.Name),
				tc.Name, "", "", errMsg, time.Since(stepStart), nil)
			k.emitEvent(proc, "ReasonStep", map[string]any{
				"step":   step,
				"action": "permission_denied",
				"tool":   tc.Name,
			}, nil, fmt.Errorf("%s", errMsg), time.Since(stepStart))
			if _, tripped := bumpToolError(counter, seen, string(types.ErrPermission), tc.Name); tripped {
				k.finishProcess(proc, ExitStatus{Code: 1, Reason: circuitBreakerReason(counter)})
				return toolCalls, false
			}
			continue
		}

		switch mapping.Type {
		case "vfs":
			callStart := time.Now()
			result, err := k.executeVFSTool(proc, tc, mapping)
			callDurMs := float64(time.Since(callStart).Microseconds()) / 1000.0
			if err != nil {
				// Cancellation/timeout errors get an empty ErrorCode so dashboards do not
				// mislabel user-cancelled or upstream-deadline errors as INTERNAL.
				var errCode string
				cancelled := isCancellation(err)
				if !cancelled {
					errCode = extractErrCode(err)
				}
				errMsg := fmt.Sprintf("Tool error (%s): %v", tc.Name, err)
				appendToolStart := time.Now()
				if appendErr := k.appendToolResult(proc, step, tc.ID, tc.Name, errMsg); appendErr != nil {
					dropped = &droppedResult{tc.ID, tc.Name, errMsg}
					break toolLoop
				}
				k.emitEvent(proc, "CtxWrite", map[string]any{"cid": proc.CtxID, "op": "AppendToolResult", "tool": tc.Name}, nil, nil, time.Since(appendToolStart))
				// C8 (spec-exit-code-tool-error-fidelity): an orchestration sub-task
				// failure (e.g. /dev/intent decompose whose sub-task tree failed) is a
				// Layer-1 child failure — route it through failedChildren so the
				// parent's completion exit code is non-zero, mirroring a non-zero
				// ActionSpawn child. Plain VFS tool errors leave FailsParent false and
				// stay content-layer (observability only) per the 2-layer model; the
				// old sticky HasToolError flag is gone (exit no longer reads it).
				var de *types.DriverError
				if errors.As(err, &de) && de.FailsParent {
					proc.MarkFailedChild()
				}
				k.emitLog(proc, step, types.LogTool, errMsg, mapping.VFSPath)
				inputJSON, _ := json.Marshal(tc.Input)
				toolCalls = append(toolCalls, types.ToolCallRecord{
					ID:         tc.ID,
					Name:       tc.Name,
					Path:       mapping.VFSPath,
					Input:      string(inputJSON),
					Error:      errMsg,
					ErrorCode:  errCode,
					DurationMs: callDurMs,
				})
				// Symmetric with the success path (line ~284): emit a ReasonStep event so
				// events.jsonl / Debug timeline / immune monitor see the failed tool call.
				// Without this, failed vfs tool calls were silent at the syscall layer —
				// only the CtxWrite (AppendToolResult) trace remained.
				k.emitEvent(proc, "ReasonStep", map[string]any{
					"step":   step,
					"action": "native_tool_call",
					"tool":   tc.Name,
					"error":  errMsg,
				}, nil, err, time.Since(stepStart))
				// Cancellation/timeout errors are recorded but do not contribute to the circuit
				// breaker — they reflect user intent or upstream deadline, not tool misuse.
				if !cancelled {
					if _, tripped := bumpToolError(counter, seen, errCode, mapping.VFSPath); tripped {
						k.finishProcess(proc, ExitStatus{Code: 1, Reason: circuitBreakerReason(counter)})
						return toolCalls, false
					}
				}
				continue
			}
			appendToolStart := time.Now()
			if appendErr := k.appendToolResult(proc, step, tc.ID, tc.Name, result); appendErr != nil {
				dropped = &droppedResult{tc.ID, tc.Name, result}
				break toolLoop
			}
			k.emitEvent(proc, "CtxWrite", map[string]any{"cid": proc.CtxID, "op": "AppendToolResult", "tool": tc.Name}, nil, nil, time.Since(appendToolStart))
			toolContent := result
			if len(toolContent) > 500 {
				toolContent = toolContent[:500] + fmt.Sprintf("... (truncated, %d bytes total)", len(result))
			}
			k.emitLog(proc, step, types.LogTool, toolContent, mapping.VFSPath)
			counter.recordSuccess()

			inputJSON, _ := json.Marshal(tc.Input)
			toolCalls = append(toolCalls, types.ToolCallRecord{
				ID:         tc.ID,
				Name:       tc.Name,
				Path:       mapping.VFSPath,
				Input:      string(inputJSON),
				Result:     result,
				DurationMs: callDurMs,
			})

			k.emitEvent(proc, "ReasonStep", func() map[string]any {
				meta := map[string]any{
					"step":   step,
					"action": "native_tool_call",
					"tool":   tc.Name,
				}
				if tc.Name == "Bash" {
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
			shouldContinue := k.executeMetaAction(proc, tc, mapping, step, stepStart, counter, seen, promptResult, rawResponseStr, &resp)
			if !shouldContinue {
				return toolCalls, false
			}
		}

	}

	if dropped != nil {
		log.Printf("[kernel] pid=%d step=%d tool result dropped for %s, attempting compact+retry", proc.PID, step, dropped.toolName)
		k.autoCompactIfNeeded(proc, step)
		if err := k.appendToolResult(proc, step, dropped.toolCallID, dropped.toolName, dropped.content); err != nil {
			log.Printf("[kernel] pid=%d step=%d tool result retry after compact still failed: %v", proc.PID, step, err)
		}
		return toolCalls, true
	}

	return toolCalls, true
}

// executeVFSTool executes a VFS tool call from native function calling.
func (k *KernelImpl) executeVFSTool(proc *Process, tc llmToolCall, mapping toolMapping) (string, error) {
	// Permission model (Story 54.1 — tool-level enforcement):
	//   ① DeniedDevices blacklist — checked first, by device path (below).
	//   ② MCP target — additive permit, path-gated against AllowedDevices (MCP
	//      tool names are dynamic, so they cannot be gated by AllowedTools).
	//   ③ Base /dev target — the AUTHORITATIVE unit is the tool NAME: when
	//      proc.AllowedTools is non-empty, only those tool names may run
	//      (allowed-tools:Read permits Read but denies Write though both live
	//      under /dev/fs). When AllowedTools is empty (legacy process whose tool
	//      set was never derived), fall back to the legacy device-level whitelist
	//      so existing behavior and backward compatibility are preserved.
	cleanPath := path.Clean(mapping.VFSPath)

	// DeniedDevices blacklist: checked before AllowedDevices. Intent-spawned
	// children use this to block recursive /dev/intent/decompose access.
	for _, denied := range proc.DeniedDevices {
		if cleanPath == denied || strings.HasPrefix(cleanPath, denied+"/") {
			return "", fmt.Errorf("permission denied: device %s is blocked for this process. "+
				"Intent subtasks cannot re-decompose — complete your work with the devices you have",
				mapping.VFSPath)
		}
	}

	targetIsMCP := strings.HasPrefix(cleanPath, mcpPathPrefix)
	switch {
	case targetIsMCP:
		// MCP mounts stay path-gated (additive permit). Enforce the AllowedDevices
		// whitelist so a process reaches only its own mounts.
		if len(proc.AllowedDevices) > 0 && !deviceAllowed(cleanPath, proc.AllowedDevices) {
			return "", fmt.Errorf("permission denied: device %s not in allowed list %v. "+
				"Child processes inherit the same device restrictions, so spawning a child will NOT grant access to this device. "+
				"If you cannot finish the task with the devices you have, use complete to report what you accomplished and which capability was missing",
				mapping.VFSPath, proc.AllowedDevices)
		}
	case toolWhitelistActive(proc.AllowedTools):
		// Base device, tool-level (authoritative): gate by the tool name.
		if !slices.Contains(proc.AllowedTools, tc.Name) {
			return "", fmt.Errorf("permission denied: tool %q is not in this process's allowed tools %v. "+
				"This is a tool-level restriction — other tools on the same device are not granted by it. "+
				"Child processes inherit the same tool restrictions, so spawning a child will NOT grant access to this tool. "+
				"If you cannot finish the task with the tools you have, use complete to report what you accomplished and which capability was missing",
				tc.Name, proc.AllowedTools)
		}
	case baseDeviceWhitelistActive(proc.AllowedDevices):
		// Base device, backward-compat fallback: a legacy process (AllowedTools
		// not derived) still carries a device-level whitelist. Gate by path.
		if !deviceAllowed(cleanPath, proc.AllowedDevices) {
			return "", fmt.Errorf("permission denied: device %s not in allowed list %v. "+
				"Child processes inherit the same device restrictions, so spawning a child will NOT grant access to this device. "+
				"If you cannot finish the task with the devices you have, use complete to report what you accomplished and which capability was missing",
				mapping.VFSPath, proc.AllowedDevices)
		}
	}

	switch mapping.FSOperation {
	case "Read":
		pathStr, _ := tc.Input["path"].(string)
		if pathStr == "" {
			received, _ := json.Marshal(tc.Input)
			return "", fmt.Errorf("tool %q: missing required 'path' parameter. Received: %s. Expected: {\"path\": \"<relative_path>\"}", "Read", string(received))
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
		fd, err := k.vfsOpenWithEvent(proc, vfsPath, vfs.O_RDONLY)
		if err != nil {
			return "", fmt.Errorf("open failed: %w", err)
		}
		data, err := k.vfsReadWithEvent(proc, fd, 1<<20)
		_ = k.vfsCloseWithEvent(proc, fd)
		if err != nil {
			return "", fmt.Errorf("read failed: %w", err)
		}
		// Track ReadFileState for post-compact restore (Story 31.2)
		k.trackReadFile(proc, pathStr, string(data), fileMtime)
		return string(data), nil

	case "Write":
		pathStr, _ := tc.Input["path"].(string)
		if pathStr == "" {
			received, _ := json.Marshal(tc.Input)
			return "", fmt.Errorf("tool %q: missing required 'path' parameter. Received: %s. Expected: {\"path\": \"<relative_path>\", \"content\": \"<text>\"}", "Write", string(received))
		}

		// Read-before-write safety check (Story 32.1 AC#3)
		warning, rbwErr := k.checkReadBeforeWrite(proc, pathStr)
		if rbwErr != nil {
			return "", rbwErr
		}

		contentStr, _ := tc.Input["content"].(string)
		vfsPath := mapping.VFSPath + "/" + pathStr
		fd, err := k.vfsOpenWithEvent(proc, vfsPath, vfs.O_WRONLY)
		if err != nil {
			return "", fmt.Errorf("open failed: %w", err)
		}
		writeData, _ := json.Marshal(map[string]string{"content": contentStr})
		if err := k.vfsWriteWithEvent(proc, fd, writeData); err != nil {
			_ = k.vfsCloseWithEvent(proc, fd)
			return "", fmt.Errorf("write failed: %w", err)
		}
		data, err := k.vfsReadWithEvent(proc, fd, 1<<20)
		_ = k.vfsCloseWithEvent(proc, fd)
		if err != nil {
			return "", fmt.Errorf("read result failed: %w", err)
		}
		result := string(data)
		if warning != "" {
			result = warning + "\n" + result
		}
		k.updateReadFileMtime(proc, pathStr)
		return result, nil

	case "Edit":
		pathStr, _ := tc.Input["path"].(string)
		if pathStr == "" {
			received, _ := json.Marshal(tc.Input)
			return "", fmt.Errorf("tool %q: missing required 'path' parameter. Received: %s", "Edit", string(received))
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
		fd, err := k.vfsOpenWithEvent(proc, vfsPath, vfs.O_WRONLY)
		if err != nil {
			return "", fmt.Errorf("open failed: %w", err)
		}
		writeData, _ := json.Marshal(editReq)
		if err := k.vfsWriteWithEvent(proc, fd, writeData); err != nil {
			_ = k.vfsCloseWithEvent(proc, fd)
			return "", fmt.Errorf("write failed: %w", err)
		}
		data, err := k.vfsReadWithEvent(proc, fd, 1<<20)
		_ = k.vfsCloseWithEvent(proc, fd)
		if err != nil {
			return "", fmt.Errorf("read result failed: %w", err)
		}
		result := string(data)
		if warning != "" {
			result = warning + "\n" + result
		}
		k.updateReadFileMtime(proc, pathStr)
		return result, nil

	case "Glob":
		patternStr, _ := tc.Input["pattern"].(string)
		if patternStr == "" {
			received, _ := json.Marshal(tc.Input)
			return "", fmt.Errorf("tool %q: missing required 'pattern' parameter. Received: %s", "Glob", string(received))
		}
		globReq := map[string]any{"op": "glob", "pattern": patternStr}
		if v, ok := tc.Input["path"]; ok {
			globReq["path"] = v
		}
		if v, ok := tc.Input["head_limit"]; ok {
			globReq["head_limit"] = v
		}

		// Open at the workDir root (no subpath)
		fd, err := k.vfsOpenWithEvent(proc, mapping.VFSPath+"/.", vfs.O_WRONLY)
		if err != nil {
			return "", fmt.Errorf("open failed: %w", err)
		}
		writeData, _ := json.Marshal(globReq)
		if err := k.vfsWriteWithEvent(proc, fd, writeData); err != nil {
			_ = k.vfsCloseWithEvent(proc, fd)
			return "", fmt.Errorf("write failed: %w", err)
		}
		data, err := k.vfsReadWithEvent(proc, fd, 1<<20)
		_ = k.vfsCloseWithEvent(proc, fd)
		if err != nil {
			return "", fmt.Errorf("read result failed: %w", err)
		}
		return string(data), nil

	case "Grep":
		patternStr, _ := tc.Input["pattern"].(string)
		if patternStr == "" {
			received, _ := json.Marshal(tc.Input)
			return "", fmt.Errorf("tool %q: missing required 'pattern' parameter. Received: %s", "Grep", string(received))
		}
		grepReq := map[string]any{"op": "grep", "pattern": patternStr}
		for _, key := range []string{"path", "output_mode", "case_insensitive", "glob", "context", "head_limit"} {
			if v, ok := tc.Input[key]; ok {
				grepReq[key] = v
			}
		}

		fd, err := k.vfsOpenWithEvent(proc, mapping.VFSPath+"/.", vfs.O_WRONLY)
		if err != nil {
			return "", fmt.Errorf("open failed: %w", err)
		}
		writeData, _ := json.Marshal(grepReq)
		if err := k.vfsWriteWithEvent(proc, fd, writeData); err != nil {
			_ = k.vfsCloseWithEvent(proc, fd)
			return "", fmt.Errorf("write failed: %w", err)
		}
		data, err := k.vfsReadWithEvent(proc, fd, 1<<20)
		_ = k.vfsCloseWithEvent(proc, fd)
		if err != nil {
			return "", fmt.Errorf("read result failed: %w", err)
		}
		return string(data), nil

	default:
		inputData, _ := json.Marshal(tc.Input)
		isEmpty := len(tc.Input) == 0
		// MCP tools require a Write (the tools/call) before Read returns a
		// result — mcpFile.Read errors ("write a request first") without a
		// prior Write. So an MCP target ALWAYS opens O_RDWR and writes, even
		// with no arguments (empty input → "{}"), unlike read-only base-device
		// opens which skip the write when input is empty. 路线 B 无参数工具边界。
		targetIsMCP := strings.HasPrefix(mapping.VFSPath, mcpPathPrefix)
		openFlags := vfs.O_RDWR
		if isEmpty && !targetIsMCP {
			openFlags = vfs.O_RDONLY
		}
		fd, err := k.vfsOpenWithEvent(proc, mapping.VFSPath, openFlags)
		if err != nil {
			return "", fmt.Errorf("open failed: %w", err)
		}
		if file := k.vfs.GetFile(proc.PID, fd); file != nil {
			if cpa, ok := file.(vfs.CallerProviderAware); ok {
				cpa.SetCallerProvider(proc.Provider)
			}
			if loa, ok := file.(vfs.LLMOpenerAware); ok {
				loa.SetLLMOpener(k.buildLLMOpener(proc))
			}
			if pca, ok := file.(vfs.ProjectConfigAware); ok && proc.ProjectConfig != nil {
				pca.SetProjectConfig(proc.ProjectConfig)
			}
			if cpia, ok := file.(vfs.CallerProcessInfoAware); ok {
				cpia.SetCallerProcessInfo(proc.PID, proc.Depth)
			}
		}
		if !isEmpty || targetIsMCP {
			writeData := inputData
			if isEmpty {
				writeData = []byte("{}")
			}
			if err := k.vfsWriteWithEvent(proc, fd, writeData); err != nil {
				_ = k.vfsCloseWithEvent(proc, fd)
				return "", fmt.Errorf("write failed: %w", err)
			}
		}
		data, err := k.vfsReadWithEvent(proc, fd, 1<<20)
		_ = k.vfsCloseWithEvent(proc, fd)
		if err != nil {
			return "", fmt.Errorf("read failed: %w", err)
		}
		return string(data), nil
	}
}

// buildLLMOpener returns an LLM file opener that tries the process's project-level
// LLMFileOpener first, then falls back to the global VFS device registry.
func (k *KernelImpl) buildLLMOpener(proc *Process) func(provider string) (vfs.VFSFile, error) {
	return func(provider string) (vfs.VFSFile, error) {
		if proc.ProjectConfig != nil && proc.ProjectConfig.LLMFileOpener != nil {
			f, err := proc.ProjectConfig.LLMFileOpener(provider, int(vfs.O_RDWR))
			if err == nil {
				if vf, ok := f.(vfs.VFSFile); ok {
					return vf, nil
				}
			}
		}
		return k.vfs.DeviceRegistry().Open("/dev/llm/"+provider, vfs.O_RDWR, "")
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

func (k *KernelImpl) executeMetaAction(proc *Process, tc llmToolCall, mapping toolMapping, step int, stepStart time.Time, counter *errFingerprintCounter, seen map[string]bool, promptResult *rnixctx.PromptResult, rawResponseStr string, resp *llmResponse) bool {
	switch mapping.Action {
	case ActionComplete:
		resultStr, _ := tc.Input["result"].(string)
		proc.mu.Lock()
		proc.Result = resultStr
		failedChildren := proc.failedChildren
		proc.mu.Unlock()
		k.emitLog(proc, step, types.LogOutput, resultStr, "")
		k.writeStepRecord(proc, step, promptResult, rawResponseStr, resp,
			"complete", briefTextSummary(resultStr), "", "", "", "", time.Since(stepStart), nil)
		k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "complete"}, resultStr, nil, time.Since(stepStart))
		stepDur := time.Since(stepStart)
		if k.callbacks != nil {
			k.callbacks.OnStepComplete(proc.PID, step, "complete", briefTextSummary(resultStr), false, float64(stepDur.Microseconds())/1000.0)
		}
		// Exit code is a Layer-1 program-level signal
		// (spec-exit-code-tool-error-fidelity, C1): reaching `complete` proves the
		// agent routed every VFS tool result through its content layer, so the 13
		// VFS tool error codes never drive the exit code. Only program-level machine
		// failures do — failedChildren here, and the circuit breaker which exits via
		// finishProcess at its trip point (bumpToolError) and never reaches this
		// completion point. Mirror this exact verdict in the final-text path
		// (reason.go) per CAP-3.
		exitCode := 0
		reason := "completed"
		if failedChildren > 0 {
			// F2 (assessment integrity): a parent that spawned children which
			// exited non-zero must not report success — otherwise child
			// failures (e.g. 401s, no output) are masked behind the parent's
			// own `complete` and `rnix apply` reports exit 0 (the rnix-eval
			// mcp/hello-mcp "fake success"). intent/orchestration sub-task
			// failures also flow here via MarkFailedChild (C8 — see the
			// DriverError.FailsParent handling in the vfs tool-error branch).
			exitCode = 1
			reason = fmt.Sprintf("completed_with_%d_failed_children", failedChildren)
		}
		k.finishProcess(proc, ExitStatus{Code: exitCode, Reason: reason})
		return false

	case ActionSpawn:
		intentStr, _ := tc.Input["intent"].(string)
		agentStr, _ := tc.Input["agent"].(string)
		modelStr, _ := tc.Input["model"].(string)

		// Spawn-recursion guard (spec spawn-recursion-no-permission): reject a
		// spawn that would exceed MaxSpawnDepth. Deterministic backstop for the
		// case where the LLM keeps spawning children to "acquire" a device
		// permission it lacks — child AllowedDevices are always ≤ parent, so
		// the chain can never succeed. The rejection feeds the circuit breaker
		// (fingerprint "INTERNAL|spawn") so repeated attempts at the depth
		// limit trip the breaker and unwind the chain.
		if proc.Depth+1 > MaxSpawnDepth {
			errMsg := fmt.Sprintf("spawn rejected: maximum spawn depth %d reached (current depth %d). "+
				"Child processes inherit your device restrictions and cannot gain new permissions by spawning deeper. "+
				"Use complete to report your results with the devices you have", MaxSpawnDepth, proc.Depth)
			_ = k.appendToolResult(proc, step, tc.ID, tc.Name, errMsg)
			k.emitLog(proc, step, types.LogTool, errMsg, "spawn")
			k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "spawn_depth_exceeded", "depth": proc.Depth, "max_depth": MaxSpawnDepth}, nil, fmt.Errorf("%s", errMsg), time.Since(stepStart))
			k.writeSpawnFailureStepRecord(proc, step, tc, errMsg, promptResult, rawResponseStr, resp, stepStart)
			if _, tripped := bumpToolError(counter, seen, string(types.ErrInternal), "spawn"); tripped {
				k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "circuit_breaker", "consecutive_errors": counter.count}, nil, nil, time.Since(stepStart))
				k.finishProcess(proc, ExitStatus{Code: 1, Reason: circuitBreakerReason(counter)})
				return false
			}
			return true
		}

		// Story 37.5: align ActionSpawn child devices with intent-decompose
		// children. stripOrchestrationDevices removes purely-orchestration
		// devices so a parent with AllowedDevices=['/dev/intent'] yields a
		// fail-open child able to reach /dev/shell etc.; unionDevices adds
		// /dev/intent to the deny-list to block recursive orchestration —
		// the same deny as cmd/rnix/main.go SpawnFunc, while also preserving
		// the parent's existing denies.
		// F3 (model routing): inherit the parent's resolved provider when the
		// spawn action doesn't name one, so a bare child model doesn't fall
		// back to the project default provider and produce a remote 401
		// (rnix-eval mcp/hello-mcp). Explicit `provider` still takes precedence.
		childProvider, _ := tc.Input["provider"].(string)
		if childProvider == "" {
			childProvider = proc.Provider
		}
		childOpts := SpawnOpts{
			Model:          modelStr,
			Provider:       childProvider,
			ParentPID:      proc.PID,
			Depth:          proc.Depth + 1,
			TraceID:        proc.TraceID,
			ProjectConfig:  proc.ProjectConfig,
			AllowedDevices: stripOrchestrationDevices(proc.AllowedDevices),
			DeniedDevices:  unionDevices(proc.DeniedDevices, orchestrationOnlyDevices),
			// Story 54.1 (review patch A): derive the child's tool whitelist from the
			// PARENT's authoritative proc.AllowedTools — intersected with the
			// orchestration-stripped device expansion — so a parent already narrowed
			// to a tool subset (e.g. specialize to [Read]) keeps child ⊆ parent as a
			// LOGICAL guarantee, not a coincidence that only holds while AllowedTools
			// ≡ device expansion (54.5 subset skills would break that). The intersect
			// still drops orchestration-only tools (intent_*) via the stripped set.
			// k.deviceRegistry() is nil-safe (SkipReasonLoop script-runner nil VFS).
			AllowedTools: intersectDevices(proc.AllowedTools, expandDevicesToTools(k.deviceRegistry(), stripOrchestrationDevices(proc.AllowedDevices))),
		}
		if proc.TraceID != "" {
			childOpts.ParentSpanID = proc.SpanID
		}

		var agentInfo *agents.AgentInfo
		if agentStr != "" {
			loader, hasLoader := k.resolveSpawnAgentLoader(proc)
			if !hasLoader {
				errMsg := fmt.Sprintf("Agent error: agent %q requested but no agent loader configured", agentStr)
				_ = k.appendToolResult(proc, step, tc.ID, tc.Name, errMsg)
				k.emitLog(proc, step, types.LogTool, errMsg, "spawn")
				k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "spawn_error"}, nil, fmt.Errorf("%s", errMsg), time.Since(stepStart))
				k.writeSpawnFailureStepRecord(proc, step, tc, errMsg, promptResult, rawResponseStr, resp, stepStart)
				if _, tripped := bumpToolError(counter, seen, string(types.ErrInternal), "spawn"); tripped {
					k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "circuit_breaker", "consecutive_errors": counter.count}, nil, nil, time.Since(stepStart))
					k.finishProcess(proc, ExitStatus{Code: 1, Reason: circuitBreakerReason(counter)})
					return false
				}
				return true
			}
			ai, loadErr := loader(agentStr)
			if loadErr != nil {
				errMsg := fmt.Sprintf("Agent error: agent %q load failed: %v", agentStr, loadErr)
				_ = k.appendToolResult(proc, step, tc.ID, tc.Name, errMsg)
				k.emitLog(proc, step, types.LogTool, errMsg, "spawn")
				k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "spawn_error"}, nil, loadErr, time.Since(stepStart))
				k.writeSpawnFailureStepRecord(proc, step, tc, errMsg, promptResult, rawResponseStr, resp, stepStart)
				if !isCancellation(loadErr) {
					// Use ErrInternal so all spawn-error sub-types share fingerprint "INTERNAL|spawn".
					if _, tripped := bumpToolError(counter, seen, string(types.ErrInternal), "spawn"); tripped {
						k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "circuit_breaker", "consecutive_errors": counter.count}, nil, nil, time.Since(stepStart))
						k.finishProcess(proc, ExitStatus{Code: 1, Reason: circuitBreakerReason(counter)})
						return false
					}
				}
				return true
			}
			agentInfo = ai
		}

		childPID, err := k.Spawn(intentStr, agentInfo, childOpts)
		if err != nil {
			errMsg := fmt.Sprintf("Agent failed: %v", err)
			_ = k.appendToolResult(proc, step, tc.ID, tc.Name, errMsg)
			k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "spawn_error"}, nil, err, time.Since(stepStart))
			k.writeSpawnFailureStepRecord(proc, step, tc, errMsg, promptResult, rawResponseStr, resp, stepStart)
			if !isCancellation(err) {
				// All spawn-error branches share fingerprint "INTERNAL|spawn" so that mixed
				// failure modes (no-loader / load-fail / spawn-fail) accumulate together —
				// otherwise alternating sub-types would silently bypass the breaker.
				if _, tripped := bumpToolError(counter, seen, string(types.ErrInternal), "spawn"); tripped {
					k.finishProcess(proc, ExitStatus{Code: 1, Reason: circuitBreakerReason(counter)})
					return false
				}
			}
			return true
		}
		// WaitChildInReason is cancellable on parent.ctx (so SuspendSubtree on
		// the parent can unwind reasonStep instead of deadlocking on a child
		// that may run for many minutes) and refreshes parent.LastHeartbeat
		// periodically (so heartbeat_monitor does not escalate stall recovery
		// on a parent legitimately blocked on its child).
		childExit, cancelled := k.WaitChildInReason(proc, childPID)
		if cancelled {
			// Parent ctx cancelled while waiting on child. Don't append a tool
			// result — the conversation flow is broken (assistant.tool_calls
			// without a paired tool result). Returning false here also lets
			// proc.wg.Wait() in suspendOneForSubtree complete.
			//
			// Distinguish two cancel sources:
			//
			//   (a) SuspendSubtree(parent) — suspendOneForSubtree did
			//       suspendRequested.Store(true) BEFORE Cancel(), so the
			//       reasonStep defer observes the flag and routes to
			//       notifySuspendDone. State transitions to Suspended via
			//       suspendOneForSubtree -> suspendProcess. No action needed.
			//
			//   (b) Any other cancel — Kill (SIGTERM/SIGKILL), external
			//       proc.Cancel(), defensive ctx mishandling, etc. — leaves
			//       suspendRequested=false. The reasonStep defer then sees
			//       state=Running and mislabels the exit as "unexpected exit"
			//       (Code=1), giving operators a false "kernel bug" signal
			//       for what is actually an explicit cancel. Surface the
			//       cancel cleanly here by transitioning to Zombie with a
			//       descriptive reason; the defer's state==Running guard
			//       then skips the unexpected-exit branch.
			k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "spawn_cancelled", "child_pid": childPID}, nil, nil, time.Since(stepStart))
			if !proc.IsSuspendRequested() {
				k.finishProcess(proc, ExitStatus{Code: 1, Reason: "cancelled while waiting on child", Err: proc.ctx.Err()})
			}
			return false
		}
		childProc, _ := k.GetProcess(childPID)
		result := ""
		if childProc != nil {
			result = childProc.Result
		}
		if result == "" {
			result = fmt.Sprintf("child PID %d exited: %s (code %d)", childPID, childExit.Reason, childExit.Code)
		}
		_ = k.appendToolResult(proc, step, tc.ID, tc.Name, result)
		k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "spawn", "child_pid": childPID}, result, nil, time.Since(stepStart))
		stepDur := time.Since(stepStart)
		if k.callbacks != nil {
			k.callbacks.OnStepComplete(proc.PID, step, "spawn", fmt.Sprintf("child pid=%d", childPID), false, float64(stepDur.Microseconds())/1000.0)
		}

		// F2 (assessment integrity): record non-zero child exits so the parent's
		// eventual `complete` reflects child failures in its own exit code,
		// rather than reporting success while spawned sub-tasks failed.
		if childExit.Code != ExitOK {
			proc.MarkFailedChild()
		}

		// Circuit breaker (spec spawn-recursion-no-permission, scheme 3): only
		// a clean child resets; an abnormal child is a no-progress spawn that
		// feeds the breaker so runaway "spawn-to-escape" loops trip and cascade
		// up the chain. See spawnBreakerOutcome.
		if spawnBreakerOutcome(counter, seen, childExit.Code) {
			k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "circuit_breaker", "consecutive_errors": counter.count}, nil, nil, time.Since(stepStart))
			k.finishProcess(proc, ExitStatus{Code: 1, Reason: circuitBreakerReason(counter)})
			return false
		}
		return true

	case ActionReplan:
		reasonStr, _ := tc.Input["reason"].(string)
		msg := fmt.Sprintf("Replanning: %s", reasonStr)
		_ = k.appendToolResult(proc, step, tc.ID, tc.Name, msg)
		k.emitLog(proc, step, types.LogOutput, msg, "")
		k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "replan"}, msg, nil, time.Since(stepStart))
		return true

	case ActionSpecialize:
		skillName, _ := tc.Input["skill"].(string)
		if skillName == "" {
			errMsg := "Skill error: empty skill name"
			_ = k.appendToolResult(proc, step, tc.ID, tc.Name, errMsg)
			k.emitLog(proc, step, types.LogTool, errMsg, "specialize")
			k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "specialize_error"}, nil, nil, time.Since(stepStart))
			return true
		}
		loader, hasLoader := k.resolveSpecializeSkillLoader(proc)
		if !hasLoader {
			errMsg := "Skill error: no skill loader configured"
			_ = k.appendToolResult(proc, step, tc.ID, tc.Name, errMsg)
			k.emitLog(proc, step, types.LogTool, errMsg, "specialize")
			k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "specialize_error"}, nil, nil, time.Since(stepStart))
			return true
		}

		proc.mu.Lock()
		alreadyLoaded := slices.Contains(proc.Skills, skillName)
		proc.mu.Unlock()
		if alreadyLoaded {
			resultMsg := fmt.Sprintf("skill %q is already loaded — its instructions are in your system prompt. Follow them using your available tools. Do NOT try to call this skill as a tool.", skillName)
			_ = k.appendToolResult(proc, step, tc.ID, tc.Name, resultMsg)
			k.emitLog(proc, step, types.LogTool, resultMsg, "specialize")
			k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "specialize_already_loaded", "skill": skillName}, nil, nil, time.Since(stepStart))
			return true
		}

		skillInfo, loadErr := loader(skillName)
		if loadErr != nil {
			errMsg := fmt.Sprintf("Skill error: skill %q load failed: %v", skillName, loadErr)
			_ = k.appendToolResult(proc, step, tc.ID, tc.Name, errMsg)
			k.emitLog(proc, step, types.LogTool, errMsg, "specialize")
			k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "specialize_error", "skill": skillName}, nil, loadErr, time.Since(stepStart))
			return true
		}

		proc.mu.Lock()
		if slices.Contains(proc.Skills, skillName) {
			proc.mu.Unlock()
			resultMsg := fmt.Sprintf("skill %q is already loaded — its instructions are in your system prompt.", skillName)
			_ = k.appendToolResult(proc, step, tc.ID, tc.Name, resultMsg)
			return true
		}
		proc.Skills = append(proc.Skills, skillName)
		// Story 54.5: normalize the skill's declared allowed-tools (semantic tool
		// names or legacy device paths) into device ROOTS (→ AllowedDevices, for
		// routing / presentation) and authoritative tool names (→ AllowedTools),
		// appending each de-duplicated. Replaces the Story 54.1 "append declared
		// values verbatim to AllowedDevices + expandDevicesToTools(declared) to
		// AllowedTools", which only handled device-path declarations and would
		// pollute AllowedDevices with raw tool names. Keeps the two whitelists in
		// lockstep so enforcement stays tool-granular after specialize (an
		// unrestricted process becomes restricted to the skill's tools).
		declTools, declDevices := k.normalizeDeclaredAllowedTools(skillInfo.Manifest.AllowedTools())
		// Privilege-escalation guard: a permission-constrained process must not
		// widen its own whitelists by loading a skill — the child ⊆ parent
		// invariant established at spawn (intersectDevices) has to survive
		// specialize. proc.AllowedDevices/AllowedTools ARE the spawn-time
		// constraint result, so intersecting the skill's declarations against
		// them gives only-narrow semantics: the skill body still loads
		// (knowledge), but declared tools/devices outside the whitelist are
		// withheld and enforcement keeps denying them. Unconstrained processes
		// (both lists empty = allow-all) keep the historical behavior of
		// adopting the skill's toolset verbatim. The lists are intersected as a
		// pair — narrowing only one would let a tool-name grant bypass an
		// active device-level whitelist (executeVFSTool is first-match).
		declaredToolCount := len(declTools)
		if len(proc.AllowedDevices) > 0 || len(proc.AllowedTools) > 0 {
			declDevices = intersectDevices(proc.AllowedDevices, declDevices)
			declTools = intersectDevices(proc.AllowedTools, declTools)
		}
		toolsWithheld := declaredToolCount - len(declTools)
		// Track what this specialize actually appended so the context-full
		// rollback below removes exactly that — never pre-existing entries the
		// process already held (which a recompute-from-manifest removal would
		// strip, silently revoking permissions on rollback).
		var addedDevices, addedTools []string
		for _, dev := range declDevices {
			if !slices.Contains(proc.AllowedDevices, dev) {
				proc.AllowedDevices = append(proc.AllowedDevices, dev)
				addedDevices = append(addedDevices, dev)
			}
		}
		for _, tool := range declTools {
			if !slices.Contains(proc.AllowedTools, tool) {
				proc.AllowedTools = append(proc.AllowedTools, tool)
				addedTools = append(addedTools, tool)
			}
		}
		if skillInfo.Body != "" {
			if proc.SkillBodies == nil {
				proc.SkillBodies = make(map[string]string)
			}
			body := skillInfo.Body
			if skillInfo.Dir != "" {
				body = "Base directory for this skill: " + skillInfo.Dir + "\n\n" + body
			}
			proc.SkillBodies[skillName] = body
		}
		if skillInfo.Dir != "" {
			if proc.SkillDirs == nil {
				proc.SkillDirs = make(map[string]string)
			}
			proc.SkillDirs[skillName] = skillInfo.Dir
		}
		totalSkills := len(proc.Skills)
		allSkills := make([]string, totalSkills)
		copy(allSkills, proc.Skills)
		proc.mu.Unlock()

		specializePayload := map[string]any{"skill": skillName, "total_skills": totalSkills}
		if toolsWithheld > 0 {
			specializePayload["tools_withheld"] = toolsWithheld
		}
		k.emitEvent(proc, "StemSpecialize", specializePayload, nil, nil, 0)

		if k.diffMemory != nil {
			var ac int
			if k.stemMatcher != nil {
				ac, _ = k.stemMatcher.AvailableCount()
			}
			k.diffMemory.Record(proc.Intent, allSkills, ac)
		}

		if proc.lineage != nil {
			trigger := "specialize"
			if tc.Input != nil {
				if t, ok := tc.Input["trigger"].(string); ok && t != "" {
					trigger = t
				}
			}
			proc.lineage.Record(LineageEvent{
				Timestamp: time.Now(),
				Phase:     "progressive",
				Skills:    []string{skillName},
				Trigger:   trigger,
			})
		}

		// Specialize 必须先 appendToolResult,再 AppendMessage(user, skill body)。
		// 否则消息序列变成:
		//   assistant + tool_calls [Skill] → user(skill body) → tool(skill loaded)
		// 这违反 OpenAI/DeepSeek 协议「tool 必须紧跟 assistant.tool_calls」,
		// 触发 HTTP 400 "Messages with role 'tool' must be a response to a
		// preceding message with 'tool_calls'"。改为先 tool result 后 user 消息,
		// 序列就是 assistant+tc → tool → user — 合法且语义不变。
		needsUserMessage := !proc.HasSections && skillInfo.Body != ""

		// reasonStep 是单线程,这里的 slot 预检相对于后续两次 append 是可靠的。
		// 需要 1 个 tool 槽位 + (可选) 1 个 user 槽位。
		if needsUserMessage {
			slots, _ := k.ctxMgr.AvailableSlots(proc.CtxID)
			if slots < 2 {
				proc.mu.Lock()
				proc.Skills = slices.DeleteFunc(proc.Skills, func(s string) bool { return s == skillName })
				delete(proc.SkillBodies, skillName)
				delete(proc.SkillDirs, skillName)
				// Roll back exactly the entries the append above contributed
				// (addedDevices/addedTools), NOT a recompute from the manifest —
				// a manifest recompute would also strip pre-existing entries the
				// process held before specialize when the skill's declarations
				// overlap them (silent permission revocation on rollback).
				if len(addedDevices) > 0 {
					devRemoveSet := make(map[string]struct{}, len(addedDevices))
					for _, d := range addedDevices {
						devRemoveSet[d] = struct{}{}
					}
					proc.AllowedDevices = slices.DeleteFunc(proc.AllowedDevices, func(d string) bool {
						_, rm := devRemoveSet[d]
						return rm
					})
				}
				// Symmetric tool-level rollback — drop the tool names this
				// specialize actually added, mirroring the device removal above.
				if len(addedTools) > 0 {
					toolRemoveSet := make(map[string]struct{}, len(addedTools))
					for _, t := range addedTools {
						toolRemoveSet[t] = struct{}{}
					}
					proc.AllowedTools = slices.DeleteFunc(proc.AllowedTools, func(t string) bool {
						_, rm := toolRemoveSet[t]
						return rm
					})
				}
				proc.mu.Unlock()
				k.emitEvent(proc, "SpecializeRollback", map[string]any{"skill": skillName, "reason": "context_full"}, nil, nil, 0)
				errMsg := fmt.Sprintf("skill %q load failed: context full. The skill was NOT loaded. Try again after context is compacted.", skillName)
				_ = k.appendToolResult(proc, step, tc.ID, tc.Name, errMsg)
				k.emitLog(proc, step, types.LogTool, errMsg, "specialize")
				k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "specialize_rollback", "skill": skillName}, nil, nil, time.Since(stepStart))
				return true
			}
		}

		resultMsg := fmt.Sprintf("skill %q loaded successfully", skillName)
		if toolsWithheld > 0 {
			resultMsg += fmt.Sprintf(" (%d declared tool(s) outside this process's permission whitelist were not granted)", toolsWithheld)
		}
		_ = k.appendToolResult(proc, step, tc.ID, tc.Name, resultMsg)

		if needsUserMessage {
			body := skillInfo.Body
			if skillInfo.Dir != "" {
				body = "Base directory for this skill: " + skillInfo.Dir + "\n\n" + body
			}
			header := fmt.Sprintf("[Dynamic Skill Loaded: %s]", skillName)
			if err := k.ctxMgr.AppendMessage(proc.CtxID, rnixctx.RoleUser, header+"\n\n"+body); err != nil {
				log.Printf("[kernel] pid=%d specialize: AppendMessage failed after slot precheck (skill=%q): %v", proc.PID, skillName, err)
			}
		}

		k.emitLog(proc, step, types.LogTool, resultMsg, "specialize")
		k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "specialize", "skill": skillName}, nil, nil, time.Since(stepStart))
		stepDur := time.Since(stepStart)
		if k.callbacks != nil {
			k.callbacks.OnStepComplete(proc.PID, step, "specialize", skillName, false, float64(stepDur.Microseconds())/1000.0)
		}
		return true

	case ActionDiscoverSkill:
		query, _ := tc.Input["query"].(string)
		maxResults := 0
		if v, ok := tc.Input["max_results"]; ok {
			switch n := v.(type) {
			case float64:
				maxResults = int(n)
			case int:
				maxResults = n
			}
		}
		matches, err := discoverSkills(proc, query, maxResults)
		if err != nil {
			_ = k.appendToolResult(proc, step, tc.ID, tc.Name, "ToolSearch error: "+err.Error())
			return true
		}
		resultStr := discoverResultJSON(query, matches)
		_ = k.appendToolResult(proc, step, tc.ID, tc.Name, resultStr)
		k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "discover_skill", "query": query, "matches": len(matches)}, nil, nil, time.Since(stepStart))
		return true

	case ActionDeferredSkillPlaceholder:
		// LLM tried to call a deferred skill placeholder — guide it to use ToolSearch + Skill
		skillName := strings.TrimPrefix(tc.Name, "skill_")
		msg := fmt.Sprintf("This skill (%s) is deferred and not yet loaded. Use ToolSearch to search for it, then Skill to load it.", skillName)
		_ = k.appendToolResult(proc, step, tc.ID, tc.Name, msg)
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

		if !proc.FeatureFlags.Planning {
			// Planning disabled: treat as text output and finish
			content := fmt.Sprintf("Plan (%s): %v", reasonStr, steps)
			k.emitLog(proc, step, types.LogOutput, content, "")
			proc.mu.Lock()
			proc.Result = content
			proc.mu.Unlock()
			k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "plan_as_text"}, content, nil, time.Since(stepStart))
			stepDur := time.Since(stepStart)
			if k.callbacks != nil {
				k.callbacks.OnStepComplete(proc.PID, step, "text", briefTextSummary(content), false, float64(stepDur.Microseconds())/1000.0)
			}
			k.finishProcess(proc, ExitStatus{Code: 0, Reason: "completed"})
			return false
		}

		var planContent strings.Builder
		fmt.Fprintf(&planContent, "[Plan]\n")
		for i, s := range steps {
			fmt.Fprintf(&planContent, "  %d. %s\n", i+1, s)
		}
		planStr := planContent.String()
		appendStart := time.Now()
		if err := k.ctxMgr.AppendMessage(proc.CtxID, rnixctx.RoleAssistant, planStr); err != nil {
			k.emitEvent(proc, "CtxWrite", map[string]any{"cid": proc.CtxID, "op": "AppendMessage", "role": string(rnixctx.RoleAssistant)}, nil, err, time.Since(appendStart))
			k.finishProcess(proc, ExitStatus{Code: 1, Reason: "append plan failed", Err: err})
			return false
		}
		k.emitEvent(proc, "CtxWrite", map[string]any{"cid": proc.CtxID, "op": "AppendMessage", "role": string(rnixctx.RoleAssistant)}, nil, nil, time.Since(appendStart))
		_ = k.appendToolResult(proc, step, tc.ID, tc.Name, planStr)
		k.emitLog(proc, step, types.LogOutput, planStr, "")
		k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "plan"}, planStr, nil, time.Since(stepStart))
		stepDur := time.Since(stepStart)
		if k.callbacks != nil {
			k.callbacks.OnStepComplete(proc.PID, step, "plan", fmt.Sprintf("plan (%d steps)", len(steps)), false, float64(stepDur.Microseconds())/1000.0)
		}
		return true
	}

	return true
}

// vfsOpenWithEvent wraps VFS.Open with an Open syscall emit so events.jsonl
// reflects every tool's underlying file activity — including the 6 fast-path
// tools (Read/Write/Edit/Glob/Grep) that previously bypassed emit and
// left strace/heatmap/Debug timeline blind to vfs interactions.
func (k *KernelImpl) vfsOpenWithEvent(proc *Process, path string, flags vfs.OpenFlag) (types.FD, error) {
	start := time.Now()
	fd, err := k.vfs.Open(proc.PID, path, flags)
	k.emitEvent(proc, "Open", map[string]any{"path": path, "flags": int(flags)}, fd, err, time.Since(start))
	// Story 48.5: remember MCP tool paths so Write/Read can attribute health
	// events to a server. No-op for non-MCP paths (AC6 zero-overhead).
	if err == nil {
		proc.recordMCPOpenPath(fd, path)
	}
	return fd, err
}

// vfsWriteWithEvent wraps VFS.Write with a Write syscall emit. Mirrors the
// default-branch emit pattern so fast-path tools that internally Write request
// payloads (Edit/Glob/Grep/Write) participate in observation.
func (k *KernelImpl) vfsWriteWithEvent(proc *Process, fd types.FD, data []byte) error {
	start := time.Now()
	err := k.vfs.Write(proc.ctx, proc.PID, fd, data)
	k.emitEvent(proc, "Write", map[string]any{"fd": fd, "size": len(data)}, nil, err, time.Since(start))
	// Story 48.5: observe MCP health at the tool-call boundary (mcp.error /
	// mcp.reconnect). Gated to MCP fds — non-MCP writes skip this entirely.
	if mcpPath := proc.mcpPathForFD(fd); mcpPath != "" {
		k.observeMCPHealth(proc, mcpPath, err)
	}
	return err
}

// vfsReadWithEvent wraps VFS.Read with a Read syscall emit. Returns both data
// and error so fast-path callers retain their existing fallthrough semantics.
func (k *KernelImpl) vfsReadWithEvent(proc *Process, fd types.FD, length int) ([]byte, error) {
	start := time.Now()
	data, err := k.vfs.Read(proc.PID, fd, length)
	k.emitEvent(proc, "Read", map[string]any{"fd": fd, "length": length}, len(data), err, time.Since(start))
	// Story 48.5: MCP tool results are delivered via Read (tools/list,
	// resources/read); observe health here too. Gated to MCP fds.
	if mcpPath := proc.mcpPathForFD(fd); mcpPath != "" {
		k.observeMCPHealth(proc, mcpPath, err)
	}
	return data, err
}

// vfsCloseWithEvent wraps VFS.Close with a Close syscall emit so fd lifecycle
// is visible in events.jsonl. Matches the default-branch emit (line ~568).
func (k *KernelImpl) vfsCloseWithEvent(proc *Process, fd types.FD) error {
	start := time.Now()
	err := k.vfs.Close(proc.PID, fd)
	k.emitEvent(proc, "Close", map[string]any{"fd": fd}, nil, err, time.Since(start))
	proc.clearMCPOpenPath(fd) // Story 48.5: drop fd→MCP-path tracking
	return err
}

// writeSpawnFailureStepRecord persists an ActionSpawn failure as a steps.jsonl row.
// Without this, a step whose only LLM tool call is a failing Agent() would never
// appear in the Timeline pane — executeMetaAction does not feed toolCallsAcc, and
// reason.go's "tool_call" writeStepRecord path only fires when toolCallsAcc is
// non-empty. The synthetic ToolCallRecord lets the IPC layer's HasError aggregation
// (server_observe.hasToolCallError) light up the err marker on the step row.
func (k *KernelImpl) writeSpawnFailureStepRecord(proc *Process, step int, tc llmToolCall,
	errMsg string, promptResult *rnixctx.PromptResult, rawResponseStr string,
	resp *llmResponse, stepStart time.Time) {

	inputJSON, _ := json.Marshal(tc.Input)
	rec := types.ToolCallRecord{
		ID:    tc.ID,
		Name:  tc.Name,
		Path:  "spawn",
		Input: string(inputJSON),
		Error: errMsg,
	}
	k.writeStepRecord(proc, step, promptResult, rawResponseStr, resp,
		"spawn", briefTextSummary(errMsg),
		"spawn", string(inputJSON), "", errMsg, time.Since(stepStart),
		[]types.ToolCallRecord{rec})
}

// resolveSpawnAgentLoader returns the agent loader to use for ActionSpawn.
// When the parent process carries a ProjectConfig with a project-aware
// AgentLoader (wired by ipc.resolveProjectContext), that loader wins so
// `.rnix/agents/` overrides take effect. Otherwise falls back to the
// daemon-wide k.agentLoader. The bool reports whether any usable loader was
// found — matching the pre-refactor "no agent loader configured" check.
//
// Project loader returns `any` (actually *agents.AgentInfo); the wrapper
// performs the type assertion so callers can treat it as a typed loader.
// Type-assertion failure and typed-nil returns BOTH fall through to the global
// loader rather than surfacing an internal mismatch to the LLM mid-step, but
// each fall-through is logged as a warning so the misbehavior is observable
// instead of silently swapping which agent gets spawned.
func (k *KernelImpl) resolveSpawnAgentLoader(proc *Process) (func(string) (*agents.AgentInfo, error), bool) {
	if proc != nil && proc.ProjectConfig != nil && proc.ProjectConfig.AgentLoader != nil {
		projectLoader := proc.ProjectConfig.AgentLoader
		return func(name string) (*agents.AgentInfo, error) {
			raw, err := projectLoader(name)
			if err != nil {
				return nil, err
			}
			ai, ok := raw.(*agents.AgentInfo)
			if !ok || ai == nil {
				// Project loader misbehaved (wrong concrete type, or typed-nil
				// AgentInfo). Without this guard a typed-nil sneaks through and
				// the caller would Spawn with agentInfo=nil — silently dropping
				// the requested agent. Fall back to global, but emit a log so
				// the user sees the misbehavior in strace / dashboard.
				if k.agentLoader != nil {
					k.emitLog(proc, 0, types.LogOutput, fmt.Sprintf(
						"warning: project AgentLoader returned %T (ok=%v) for agent %q — falling back to global loader",
						raw, ok, name), "")
					return k.agentLoader(name)
				}
				return nil, fmt.Errorf("project agent loader returned unexpected value (type=%T ok=%v) for agent %q", raw, ok, name)
			}
			return ai, nil
		}, true
	}
	if k.agentLoader != nil {
		return k.agentLoader, true
	}
	return nil, false
}

// resolveSpecializeSkillLoader mirrors resolveSpawnAgentLoader for the
// ActionSpecialize path. Project loader wins; falls back to global k.skillLoader.
func (k *KernelImpl) resolveSpecializeSkillLoader(proc *Process) (func(string) (*skills.SkillInfo, error), bool) {
	if proc != nil && proc.ProjectConfig != nil && proc.ProjectConfig.SkillLoader != nil {
		projectLoader := proc.ProjectConfig.SkillLoader
		return func(name string) (*skills.SkillInfo, error) {
			raw, err := projectLoader(name)
			if err != nil {
				return nil, err
			}
			si, ok := raw.(*skills.SkillInfo)
			if !ok || si == nil {
				if k.skillLoader != nil {
					k.emitLog(proc, 0, types.LogOutput, fmt.Sprintf(
						"warning: project SkillLoader returned %T (ok=%v) for skill %q — falling back to global loader",
						raw, ok, name), "")
					return k.skillLoader(name)
				}
				return nil, fmt.Errorf("project skill loader returned unexpected value (type=%T ok=%v) for skill %q", raw, ok, name)
			}
			return si, nil
		}, true
	}
	if k.skillLoader != nil {
		return k.skillLoader, true
	}
	return nil, false
}
