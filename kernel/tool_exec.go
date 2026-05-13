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
	"github.com/rnixai/rnix/vfs"
)

// errFingerprintCounter tracks consecutive tool errors by fingerprint (errorCode|toolPath).
// When a new fingerprint appears, count resets to 1. Same fingerprint increments count.
type errFingerprintCounter struct {
	fp    string // "<errCode>|<toolPath>"
	count int
}

func (c *errFingerprintCounter) bump(errCode, toolPath string) int {
	fp := errCode + "|" + toolPath
	if c.fp != fp {
		c.fp = fp
		c.count = 1
	} else {
		c.count++
	}
	return c.count
}

func (c *errFingerprintCounter) reset() {
	c.fp = ""
	c.count = 0
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

// circuitBreakerReason formats the exit reason string with fingerprint detail for traceability.
func circuitBreakerReason(fp string, count int) string {
	return fmt.Sprintf("circuit_breaker: %d consecutive errors with fingerprint %s", count, fp)
}

// bumpToolError applies per-step deduplication then bumps the cross-step counter.
// Returns (count, tripped) where tripped=true when count >= 3.
// Cancellation errors are filtered upstream and never reach here.
func bumpToolError(counter *errFingerprintCounter, seen map[string]bool, errCode, toolPath string) (int, bool) {
	fp := errCode + "|" + toolPath
	if seen[fp] {
		return counter.count, false
	}
	seen[fp] = true
	n := counter.bump(errCode, toolPath)
	return n, n >= 3
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
				k.finishProcess(proc, ExitStatus{Code: 1, Reason: circuitBreakerReason(counter.fp, counter.count)})
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
				k.finishProcess(proc, ExitStatus{Code: 1, Reason: circuitBreakerReason(counter.fp, counter.count)})
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
				proc.mu.Lock()
				proc.HasToolError = true
				proc.mu.Unlock()
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
				// Cancellation/timeout errors are recorded but do not contribute to the circuit
				// breaker — they reflect user intent or upstream deadline, not tool misuse.
				if !cancelled {
					if _, tripped := bumpToolError(counter, seen, errCode, mapping.VFSPath); tripped {
						k.finishProcess(proc, ExitStatus{Code: 1, Reason: circuitBreakerReason(counter.fp, counter.count)})
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
			counter.reset()
			proc.mu.Lock()
			proc.HasToolError = false
			proc.mu.Unlock()

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
		isEmpty := len(tc.Input) == 0
		openFlags := vfs.O_RDWR
		if isEmpty {
			openFlags = vfs.O_RDONLY
		}
		openStart := time.Now()
		fd, err := k.vfs.Open(proc.PID, mapping.VFSPath, openFlags)
		k.emitEvent(proc, "Open", map[string]any{"path": mapping.VFSPath, "flags": int(openFlags)}, fd, err, time.Since(openStart))
		if err != nil {
			return "", fmt.Errorf("open failed: %w", err)
		}
		if !isEmpty {
			writeStart := time.Now()
			if err := k.vfs.Write(proc.ctx, proc.PID, fd, inputData); err != nil {
				k.emitEvent(proc, "Write", map[string]any{"fd": fd, "size": len(inputData)}, nil, err, time.Since(writeStart))
				_ = k.vfs.Close(proc.PID, fd)
				return "", fmt.Errorf("write failed: %w", err)
			}
			k.emitEvent(proc, "Write", map[string]any{"fd": fd, "size": len(inputData)}, nil, nil, time.Since(writeStart))
		}
		readStart := time.Now()
		data, err := k.vfs.Read(proc.PID, fd, 1<<20)
		k.emitEvent(proc, "Read", map[string]any{"fd": fd, "length": 1 << 20}, len(data), err, time.Since(readStart))
		closeStart := time.Now()
		closeErr := k.vfs.Close(proc.PID, fd)
		k.emitEvent(proc, "Close", map[string]any{"fd": fd}, nil, closeErr, time.Since(closeStart))
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

func (k *KernelImpl) executeMetaAction(proc *Process, tc llmToolCall, mapping toolMapping, step int, stepStart time.Time, counter *errFingerprintCounter, seen map[string]bool, promptResult *rnixctx.PromptResult, rawResponseStr string, resp *llmResponse) bool {
	switch mapping.Action {
	case ActionComplete:
		resultStr, _ := tc.Input["result"].(string)
		proc.mu.Lock()
		proc.Result = resultStr
		hadError := proc.HasToolError
		proc.mu.Unlock()
		k.emitLog(proc, step, types.LogOutput, resultStr, "")
		k.writeStepRecord(proc, step, promptResult, rawResponseStr, resp,
			"complete", briefTextSummary(resultStr), "", "", "", "", time.Since(stepStart), nil)
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
		childOpts := SpawnOpts{
			Model:         modelStr,
			ParentPID:     proc.PID,
			TraceID:       proc.TraceID,
			ProjectConfig: proc.ProjectConfig,
		}
		if proc.TraceID != "" {
			childOpts.ParentSpanID = proc.SpanID
		}

		var agentInfo *agents.AgentInfo
		if agentStr != "" {
			if k.agentLoader == nil {
				errMsg := fmt.Sprintf("spawn error: agent %q requested but no agent loader configured", agentStr)
				_ = k.appendToolResult(proc, step, tc.ID, tc.Name, errMsg)
				k.emitLog(proc, step, types.LogTool, errMsg, "spawn")
				k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "spawn_error"}, nil, fmt.Errorf("%s", errMsg), time.Since(stepStart))
				if _, tripped := bumpToolError(counter, seen, string(types.ErrInternal), "spawn"); tripped {
					k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "circuit_breaker", "consecutive_errors": counter.count}, nil, nil, time.Since(stepStart))
					k.finishProcess(proc, ExitStatus{Code: 1, Reason: circuitBreakerReason(counter.fp, counter.count)})
					return false
				}
				return true
			}
			ai, loadErr := k.agentLoader(agentStr)
			if loadErr != nil {
				errMsg := fmt.Sprintf("spawn error: agent %q load failed: %v", agentStr, loadErr)
				_ = k.appendToolResult(proc, step, tc.ID, tc.Name, errMsg)
				k.emitLog(proc, step, types.LogTool, errMsg, "spawn")
				k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "spawn_error"}, nil, loadErr, time.Since(stepStart))
				if !isCancellation(loadErr) {
					// Use ErrInternal so all spawn-error sub-types share fingerprint "INTERNAL|spawn".
					if _, tripped := bumpToolError(counter, seen, string(types.ErrInternal), "spawn"); tripped {
						k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "circuit_breaker", "consecutive_errors": counter.count}, nil, nil, time.Since(stepStart))
						k.finishProcess(proc, ExitStatus{Code: 1, Reason: circuitBreakerReason(counter.fp, counter.count)})
						return false
					}
				}
				return true
			}
			agentInfo = ai
		}

		childPID, err := k.Spawn(intentStr, agentInfo, childOpts)
		if err != nil {
			errMsg := fmt.Sprintf("spawn failed: %v", err)
			_ = k.appendToolResult(proc, step, tc.ID, tc.Name, errMsg)
			k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "spawn_error"}, nil, err, time.Since(stepStart))
			if !isCancellation(err) {
				// All spawn-error branches share fingerprint "INTERNAL|spawn" so that mixed
				// failure modes (no-loader / load-fail / spawn-fail) accumulate together —
				// otherwise alternating sub-types would silently bypass the breaker.
				if _, tripped := bumpToolError(counter, seen, string(types.ErrInternal), "spawn"); tripped {
					k.finishProcess(proc, ExitStatus{Code: 1, Reason: circuitBreakerReason(counter.fp, counter.count)})
					return false
				}
			}
			return true
		}
		// Successful spawn resets the circuit breaker — matches the vfs success path
		// and the spec's "any tool success resets counter" rule.
		counter.reset()
		childExit, _ := k.Wait(childPID)
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
		return true

	case ActionReplan:
		reasonStr, _ := tc.Input["reason"].(string)
		msg := fmt.Sprintf("Replanning: %s", reasonStr)
		_ = k.appendToolResult(proc, step, tc.ID, tc.Name, msg)
		k.emitLog(proc, step, types.LogOutput, msg, "")
		k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "replan"}, msg, nil, time.Since(stepStart))
		return true

	case ActionSpecialize:
		skillName, _ := tc.Input["skill_name"].(string)
		if skillName == "" {
			errMsg := "specialize error: empty skill name"
			_ = k.appendToolResult(proc, step, tc.ID, tc.Name, errMsg)
			k.emitLog(proc, step, types.LogTool, errMsg, "specialize")
			k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "specialize_error"}, nil, nil, time.Since(stepStart))
			return true
		}
		if k.skillLoader == nil {
			errMsg := "specialize error: no skill loader configured"
			_ = k.appendToolResult(proc, step, tc.ID, tc.Name, errMsg)
			k.emitLog(proc, step, types.LogTool, errMsg, "specialize")
			k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "specialize_error"}, nil, nil, time.Since(stepStart))
			return true
		}

		proc.mu.Lock()
		alreadyLoaded := slices.Contains(proc.Skills, skillName)
		proc.mu.Unlock()
		if alreadyLoaded {
			resultMsg := fmt.Sprintf("skill %q is already loaded — its instructions are in your system prompt. Follow them using available VFS devices (/dev/fs, /dev/shell, etc.). Do NOT try to call this skill as a tool.", skillName)
			_ = k.appendToolResult(proc, step, tc.ID, tc.Name, resultMsg)
			k.emitLog(proc, step, types.LogTool, resultMsg, "specialize")
			k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "specialize_already_loaded", "skill": skillName}, nil, nil, time.Since(stepStart))
			return true
		}

		skillInfo, loadErr := k.skillLoader(skillName)
		if loadErr != nil {
			errMsg := fmt.Sprintf("specialize error: skill %q load failed: %v", skillName, loadErr)
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
		proc.AllowedDevices = append(proc.AllowedDevices, skillInfo.Manifest.AllowedTools()...)
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

		k.emitEvent(proc, "StemSpecialize", map[string]any{"skill": skillName, "total_skills": totalSkills}, nil, nil, 0)

		if k.diffMemory != nil {
			k.diffMemory.Record(proc.Intent, allSkills)
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

		if !proc.HasSections && skillInfo.Body != "" {
			body := skillInfo.Body
			if skillInfo.Dir != "" {
				body = "Base directory for this skill: " + skillInfo.Dir + "\n\n" + body
			}
			header := fmt.Sprintf("[Dynamic Skill Loaded: %s]", skillName)
			if err := k.ctxMgr.AppendMessage(proc.CtxID, rnixctx.RoleUser, header+"\n\n"+body); err != nil {
				proc.mu.Lock()
				proc.Skills = slices.DeleteFunc(proc.Skills, func(s string) bool { return s == skillName })
				delete(proc.SkillBodies, skillName)
				delete(proc.SkillDirs, skillName)
				removedTools := skillInfo.Manifest.AllowedTools()
				if len(removedTools) > 0 {
					removeSet := make(map[string]struct{}, len(removedTools))
					for _, t := range removedTools {
						removeSet[t] = struct{}{}
					}
					proc.AllowedDevices = slices.DeleteFunc(proc.AllowedDevices, func(d string) bool {
						_, rm := removeSet[d]
						return rm
					})
				}
				proc.mu.Unlock()
				k.emitEvent(proc, "SpecializeRollback", map[string]any{"skill": skillName, "reason": "context_full"}, nil, err, 0)
				errMsg := fmt.Sprintf("skill %q load failed: context full. The skill was NOT loaded. Try again after context is compacted.", skillName)
				_ = k.appendToolResult(proc, step, tc.ID, tc.Name, errMsg)
				k.emitLog(proc, step, types.LogTool, errMsg, "specialize")
				k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "specialize_rollback", "skill": skillName}, nil, err, time.Since(stepStart))
				return true
			}
		}

		resultMsg := fmt.Sprintf("skill %q loaded successfully", skillName)
		_ = k.appendToolResult(proc, step, tc.ID, tc.Name, resultMsg)
		k.emitLog(proc, step, types.LogTool, resultMsg, "specialize")
		k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "specialize", "skill": skillName}, nil, nil, time.Since(stepStart))
		stepDur := time.Since(stepStart)
		if k.callbacks != nil {
			k.callbacks.OnStepComplete(proc.PID, step, "specialize", skillName, false, float64(stepDur.Microseconds())/1000.0)
		}
		return true

	case ActionDiscoverSkill:
		query, _ := tc.Input["query"].(string)
		matches, err := discoverSkills(proc, query)
		if err != nil {
			_ = k.appendToolResult(proc, step, tc.ID, tc.Name, "discover_skill error: "+err.Error())
			return true
		}
		resultStr := discoverResultJSON(query, matches)
		_ = k.appendToolResult(proc, step, tc.ID, tc.Name, resultStr)
		k.emitEvent(proc, "ReasonStep", map[string]any{"step": step, "action": "discover_skill", "query": query, "matches": len(matches)}, nil, nil, time.Since(stepStart))
		return true

	case ActionDeferredSkillPlaceholder:
		// LLM tried to call a deferred skill placeholder — guide it to use discover_skill + specialize
		skillName := strings.TrimPrefix(tc.Name, "skill_")
		msg := fmt.Sprintf("This skill (%s) is deferred and not yet loaded. Use discover_skill to search for it, then specialize to load it.", skillName)
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

		if !proc.PlanningEnabled {
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
