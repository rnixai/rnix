package kernel

import (
	"encoding/json"
	"fmt"
	"log"
	"maps"
	"strings"
	"time"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/debug"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// emitEvent sends a SyscallEvent to the process DebugChan (non-blocking).
func (k *KernelImpl) emitEvent(proc *Process, syscall string, args map[string]any, result any, err error, duration time.Duration) {
	// Check gdb syscall breakpoint at entry (Story 13.2)
	if syscall != "GdbPause" {
		if hit := proc.CheckBreakpoint(BreakpointContext{
			BPType:      BPSyscall,
			SyscallName: syscall,
			SyscallArgs: args,
		}); hit != nil {
			proc.GdbPause(fmt.Sprintf("syscall breakpoint hit: %s", syscall), hit)
		}
	}

	// Check gdb step syscall mode (Story 13.3)
	if syscall != "GdbPause" && syscall != "ReasonStep" {
		if proc.GetStepMode() == StepSyscall {
			proc.ClearStepMode()
			proc.GdbPause("step_syscall", nil, map[string]any{
				"syscall_name": syscall,
				"syscall_args": args,
			})
		}
	}

	event := debug.NewEvent(proc.PID, proc.CreatedAt, syscall, args)
	debug.CompleteEvent(&event, result, err, duration)
	proc.mu.Lock()
	event.TraceID = proc.TraceID
	event.SpanID = proc.SpanID
	hasTrace := proc.TraceID != ""
	ch := proc.DebugChan
	ew := proc.eventWriter
	if ch != nil {
		debug.EmitEvent(ch, event)
	}
	proc.mu.Unlock()

	// Persist syscall event to disk (always-on)
	if ew != nil {
		_ = ew.WriteEvent(event)
	}

	// Recording hook (Story 14.1)
	if k.recordMgr != nil && k.recordMgr.IsRecording(proc.PID) {
		recEvent := debug.RecordEventFromSyscall(event)
		if err := k.recordMgr.RecordEvent(proc.PID, recEvent); err != nil {
			log.Printf("[record] write error pid=%d: %v", proc.PID, err)
		}
	}

	if hasTrace && k.spanRecorder != nil {
		k.spanRecorder.RecordSyscall(proc.PID)
	}

	// Immune daemon behavior monitoring (Story 22.1)
	if k.immuneDaemon != nil {
		k.immuneDaemon.OnSyscallEvent(proc.PID, event)
	}
}

// emitLog sends a LogEntry to the process LogChan (non-blocking).
func (k *KernelImpl) emitLog(proc *Process, step int, cat types.LogCategory, content, toolPath string) {
	entry := types.LogEntry{
		Timestamp: time.Since(proc.CreatedAt),
		PID:       proc.PID,
		Step:      step,
		Category:  cat,
		Content:   content,
		ToolPath:  toolPath,
	}
	proc.mu.Lock()
	proc.AppendLogHistory(entry)
	ch := proc.LogChan
	if ch != nil {
		select {
		case ch <- entry:
		default:
		}
	}
	proc.mu.Unlock()
}

// writeStepRecord assembles and writes a StepRecord to the process's StepWriter.
func (k *KernelImpl) writeStepRecord(proc *Process, step int, promptResult *rnixctx.PromptResult,
	rawResponse string, resp *llmResponse, actionType string, summary string,
	toolPath string, toolInput string, toolResult string, toolError string, toolDuration time.Duration) {

	proc.mu.Lock()
	sw := proc.stepWriter
	proc.mu.Unlock()
	if sw == nil {
		return
	}

	var msgsRaw json.RawMessage
	var msgCount int
	if promptResult != nil {
		msgCount = len(promptResult.Messages)
		var marshalErr error
		msgsRaw, marshalErr = json.Marshal(promptResult.Messages)
		if marshalErr != nil {
			log.Printf("[step_writer] messages marshal error pid=%d step=%d: %v", proc.PID, step, marshalErr)
			msgsRaw = nil
			msgCount = 0
		}
	}

	rec := types.StepRecord{
		Step:         step,
		Timestamp:    time.Since(proc.CreatedAt),
		Messages:     msgsRaw,
		MessageCount: msgCount,
		RawResponse:  rawResponse,
		Action:       actionType,
		Summary:      summary,
		ToolPath:     toolPath,
		ToolInput:    toolInput,
		ToolResult:   toolResult,
		ToolError:    toolError,
		ToolDuration: toolDuration,
	}
	if resp != nil {
		rec.TokenCount = resp.TokensUsed
		rec.ResponseTokens = resp.TokensUsed
	}

	if err := sw.WriteStep(rec); err != nil {
		log.Printf("[step_writer] write error pid=%d step=%d: %v", proc.PID, step, err)
	}
}

// writeDriverStepRecordFull writes a StepRecord with accumulated tool data for CLI driver events.
func (k *KernelImpl) writeDriverStepRecordFull(proc *Process, step int, action, summary, toolPath, toolInput, toolResult string, toolDuration time.Duration, messages json.RawMessage, messageCount int) {
	proc.mu.Lock()
	sw := proc.stepWriter
	proc.mu.Unlock()
	if sw == nil {
		log.Printf("[step_writer] WARNING: stepWriter is nil for pid=%d, step=%d will not be recorded", proc.PID, step)
		return
	}

	rec := types.StepRecord{
		Step:         step,
		Timestamp:    time.Since(proc.CreatedAt),
		Action:       action,
		Summary:      summary,
		ToolPath:     toolPath,
		ToolInput:    toolInput,
		ToolResult:   toolResult,
		ToolDuration: toolDuration,
		Messages:     messages,
		MessageCount: messageCount,
	}
	if err := sw.WriteStep(rec); err != nil {
		log.Printf("[step_writer] driver step write error pid=%d step=%d: %v", proc.PID, step, err)
	}
}

// briefToolCallSummary generates "{toolPath} -> {briefResult}" for OnStepComplete.
func briefToolCallSummary(toolPath, toolResult string) string {
	brief := strings.ReplaceAll(toolResult, "\n", " ")
	r := []rune(brief)
	if len(r) > 60 {
		brief = string(r[:60]) + "..."
	}
	if brief == "" {
		brief = "ok"
	}
	return toolPath + " → " + brief
}

// driverToolSummary generates a human-readable summary for CLI driver tool calls.
func driverToolSummary(tool, toolPath, toolInput string) string {
	if toolInput == "" {
		if toolPath != "" {
			return toolPath
		}
		return tool
	}

	var input map[string]any
	if err := json.Unmarshal([]byte(toolInput), &input); err != nil {
		if toolPath != "" {
			return toolPath
		}
		return tool
	}

	var key string
	switch tool {
	case "Bash":
		key = "command"
	case "glob", "Grep":
		key = "pattern"
	case "Read", "Write", "Edit":
		key = "file_path"
	case "WebFetch":
		key = "url"
	default:
		for _, k := range []string{"command", "pattern", "file_path", "url", "query", "text"} {
			if v, ok := input[k]; ok {
				key = k
				_ = v
				break
			}
		}
	}

	if key != "" {
		if val, ok := input[key]; ok {
			valStr := fmt.Sprintf("%v", val)
			r := []rune(valStr)
			if len(r) > 60 {
				valStr = string(r[:60]) + "..."
			}
			return tool + ": " + valStr
		}
	}

	if toolPath != "" {
		return toolPath
	}
	return tool
}

// briefTextSummary extracts the first non-empty line from text content, truncated to 60 runes.
func briefTextSummary(content string) string {
	for line := range strings.SplitSeq(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		r := []rune(line)
		if len(r) > 60 {
			return string(r[:60]) + "..."
		}
		return line
	}
	return ""
}

// extractContentText extracts text from a content field that may be a string,
// []map[string]any, or []any.
func extractContentText(content any) string {
	switch v := content.(type) {
	case string:
		return v
	case map[string]any:
		if v["type"] == "text" {
			if text, ok := v["text"].(string); ok {
				return text
			}
		}
		return ""
	case []map[string]any:
		var parts []string
		for _, block := range v {
			if block == nil {
				continue
			}
			if block["type"] == "text" {
				if text, ok := block["text"].(string); ok && text != "" {
					parts = append(parts, text)
				}
			}
		}
		return strings.Join(parts, "\n")
	case []any:
		var parts []string
		for _, item := range v {
			if block, ok := item.(map[string]any); ok {
				if block["type"] == "text" {
					if text, ok := block["text"].(string); ok && text != "" {
						parts = append(parts, text)
					}
				}
			}
		}
		return strings.Join(parts, "\n")
	default:
		return ""
	}
}

// driverEventToSyscall maps a driver event type to a precise syscall name.
func driverEventToSyscall(evtType string) string {
	switch evtType {
	case "tool_call":
		return "DriverToolCall"
	case "thinking":
		return "DriverThinking"
	case "system":
		return "DriverInit"
	case "user":
		return "DriverUser"
	default:
		return "DriverEvent"
	}
}

// driverEventToLog maps a driver event to a LogEntry category and content.
func driverEventToLog(evt map[string]any) (types.LogCategory, string, string, bool) {
	evtType, _ := evt["type"].(string)
	subtype, _ := evt["subtype"].(string)
	contentField, _ := evt["content"].(string)

	switch evtType {
	case "tool_call":
		tool, _ := evt["tool"].(string)
		desc, _ := evt["description"].(string)
		path, _ := evt["path"].(string)
		content := contentField
		if subtype != "" && content == "" {
			content = subtype
		}
		if desc != "" {
			content = desc
		}
		toolPath := tool
		if path != "" {
			toolPath = tool + ":" + path
		}
		return types.LogTool, content, toolPath, true
	case "thinking":
		text := strings.TrimSpace(contentField)
		if text == "" || text == subtype {
			return "", "", "", false
		}
		r := []rune(text)
		if len(r) > 80 {
			text = string(r[:80]) + "..."
		}
		return types.LogThink, text, "", true
	default:
		return "", "", "", false
	}
}

// GetLogChan safely retrieves the log channel for a process under lock.
func (k *KernelImpl) GetLogChan(pid types.PID) (chan types.LogEntry, bool) {
	proc, ok := k.GetProcess(pid)
	if !ok {
		return nil, false
	}
	proc.mu.Lock()
	ch := proc.LogChan
	proc.mu.Unlock()
	return ch, ch != nil
}

// GetLogHistory returns a copy of the log history for a process.
func (k *KernelImpl) GetLogHistory(pid types.PID) ([]types.LogEntry, bool) {
	proc, ok := k.GetProcess(pid)
	if !ok {
		return nil, false
	}
	proc.mu.Lock()
	history := proc.GetLogHistory()
	proc.mu.Unlock()
	return history, true
}

// GetDebugChan safely retrieves the debug channel for a process under lock.
func (k *KernelImpl) GetDebugChan(pid types.PID) (chan types.SyscallEvent, bool) {
	proc, ok := k.GetProcess(pid)
	if !ok {
		return nil, false
	}
	proc.mu.Lock()
	ch := proc.DebugChan
	proc.mu.Unlock()
	return ch, ch != nil
}

// setupDriverStreamHandler configures the stream handler for driver events and native tool detection.
func (k *KernelImpl) setupDriverStreamHandler(proc *Process, llmFD types.FD) {
	file := k.vfs.GetFile(proc.PID, llmFD)
	if file == nil {
		return
	}

	if obs, ok := file.(vfs.StreamObserver); ok {
		driverStep := 0
		var pendingTool struct {
			step     int
			tool     string
			summary  string
			toolPath string
			start    time.Time
			inputBuf strings.Builder
			result   string
		}
		var msgHistory []rnixctx.Message
		if proc.Intent != "" {
			msgHistory = append(msgHistory, rnixctx.Message{
				Role:    rnixctx.RoleUser,
				Content: proc.Intent,
			})
		}

		flushPendingTool := func() {
			if pendingTool.step == 0 {
				return
			}
			dur := time.Since(pendingTool.start)
			toolInput := pendingTool.inputBuf.String()
			summary := driverToolSummary(pendingTool.tool, pendingTool.toolPath, toolInput)
			var msgsRaw json.RawMessage
			msgCount := len(msgHistory)
			if msgCount > 0 {
				if data, err := json.Marshal(msgHistory); err == nil {
					msgsRaw = data
				}
			}
			k.writeDriverStepRecordFull(proc, pendingTool.step, pendingTool.tool,
				summary, pendingTool.toolPath, toolInput, pendingTool.result, dur,
				msgsRaw, msgCount)
			pendingTool.step = 0
			pendingTool.inputBuf.Reset()
			pendingTool.result = ""
		}
		obs.SetStreamHandler(func(evt map[string]any) {
			// Driver activity refreshes heartbeat — prevents false stall
			// detection during long-running streaming LLM calls.
			proc.TouchHeartbeat()

			evtType, _ := evt["type"].(string)
			syscallName := driverEventToSyscall(evtType)
			k.emitEvent(proc, syscallName, evt, nil, nil, 0)

			if cat, content, toolPath, ok := driverEventToLog(evt); ok {
				k.emitLog(proc, 0, cat, content, toolPath)
			}

			if evtType == "system" {
				// Merge driver-reported tools into existing nativeToolDefs
				// (which already contains VFS + meta tools from buildToolDefs).
				// Never overwrite — only append new entries to avoid losing
				// kernel-injected tools (Story 35-bug: observe.go overwrite).
				var driverTools []string
				if tools, ok := evt["tools"].([]string); ok {
					driverTools = tools
				} else if toolsAny, ok := evt["tools"].([]any); ok {
					for _, t := range toolsAny {
						if name, ok := t.(string); ok {
							driverTools = append(driverTools, name)
						}
					}
				}
				if len(driverTools) > 0 {
					proc.mu.Lock()
					existing := make(map[string]bool, len(proc.nativeToolDefs))
					for _, td := range proc.nativeToolDefs {
						existing[td.Name] = true
					}
					for _, name := range driverTools {
						if !existing[name] {
							proc.nativeToolDefs = append(proc.nativeToolDefs, vfs.ToolDef{Name: name})
						}
					}
					proc.mu.Unlock()
				}
			}

			if evtType == "assistant" || evtType == "user" {
				if evtType == "user" && evt["content"] == nil {
					// do nothing
				} else {
					role, _ := evt["role"].(string)
					if role == "" {
						role = evtType
					}
					msg := rnixctx.Message{Role: rnixctx.Role(role)}
					contentVal := evt["content"]
					switch c := contentVal.(type) {
					case []map[string]any:
						for _, block := range c {
							blockType, _ := block["type"].(string)
							switch blockType {
							case "text":
								if text, ok := block["text"].(string); ok {
									if msg.Content != "" {
										msg.Content += "\n"
									}
									msg.Content += text
								}
							case "tool_use":
								name, _ := block["name"].(string)
								id, _ := block["id"].(string)
								msg.ToolCalls = append(msg.ToolCalls, rnixctx.ToolCall{ID: id, Name: name})
							case "tool_result":
								toolUseID, _ := block["tool_use_id"].(string)
								msg.ToolCallID = toolUseID
								if text := extractContentText(block["content"]); text != "" {
									msg.Content = text
								}
							}
						}
					case []any:
						for _, item := range c {
							if block, ok := item.(map[string]any); ok {
								blockType, _ := block["type"].(string)
								switch blockType {
								case "text":
									if text, ok := block["text"].(string); ok {
										if msg.Content != "" {
											msg.Content += "\n"
										}
										msg.Content += text
									}
								case "tool_use":
									name, _ := block["name"].(string)
									id, _ := block["id"].(string)
									msg.ToolCalls = append(msg.ToolCalls, rnixctx.ToolCall{ID: id, Name: name})
								case "tool_result":
									toolUseID, _ := block["tool_use_id"].(string)
									msg.ToolCallID = toolUseID
									if text := extractContentText(block["content"]); text != "" {
										msg.Content = text
									}
								}
							}
						}
					case map[string]any:
						blockType, _ := c["type"].(string)
						switch blockType {
						case "text":
							if text, ok := c["text"].(string); ok {
								msg.Content = text
							}
						case "tool_use":
							name, _ := c["name"].(string)
							id, _ := c["id"].(string)
							msg.ToolCalls = append(msg.ToolCalls, rnixctx.ToolCall{ID: id, Name: name})
						case "tool_result":
							toolUseID, _ := c["tool_use_id"].(string)
							msg.ToolCallID = toolUseID
							if text := extractContentText(c["content"]); text != "" {
								msg.Content = text
							}
						}
					case string:
						msg.Content = c
					}
					msgHistory = append(msgHistory, msg)
				}
			}

			switch evtType {
			case "tool_call":
				contentVal, _ := evt["content"].(string)
				switch contentVal {
				case "started":
					flushPendingTool()
					driverStep++
					tool, _ := evt["tool"].(string)
					desc, _ := evt["description"].(string)
					pathStr, _ := evt["path"].(string)
					cmd, _ := evt["command"].(string)
					summary := desc
					if summary == "" {
						summary = tool
					}
					toolPath := tool
					if pathStr != "" {
						toolPath = tool + ":" + pathStr
					}
					pendingTool.step = driverStep
					pendingTool.tool = tool
					pendingTool.summary = summary
					pendingTool.toolPath = toolPath
					pendingTool.start = time.Now()
					pendingTool.inputBuf.Reset()
					if cmd != "" {
						pendingTool.inputBuf.WriteString(cmd)
					} else if pathStr != "" {
						pendingTool.inputBuf.WriteString(pathStr)
					}
					if argsStr, ok := evt["args"].(string); ok && argsStr != "" {
						if pendingTool.inputBuf.Len() == 0 {
							pendingTool.inputBuf.WriteString(argsStr)
						}
					}
					if tool != "" {
						proc.mu.Lock()
						if len(proc.nativeToolDefs) == 0 {
							proc.nativeToolDefs = []vfs.ToolDef{{Name: tool, Description: desc}}
						} else {
							found := false
							for i, td := range proc.nativeToolDefs {
								if td.Name == tool {
									if td.Description == "" && desc != "" {
										proc.nativeToolDefs[i].Description = desc
									}
									found = true
									break
								}
							}
							if !found {
								proc.nativeToolDefs = append(proc.nativeToolDefs, vfs.ToolDef{Name: tool, Description: desc})
							}
						}
						proc.mu.Unlock()
					}
				case "input_delta":
					if partial, ok := evt["partial_json"].(string); ok {
						pendingTool.inputBuf.WriteString(partial)
					}
				case "completed":
					if result, ok := evt["result"].(string); ok && result != "" {
						pendingTool.result = result
					}
					if pendingTool.result == "" {
						if result, ok := evt["tool_result"].(string); ok {
							pendingTool.result = result
						}
					}
					flushPendingTool()
				}
			case "user":
				flushPendingTool()
			}
		})
	}

	if tc, ok := file.(vfs.ToolCapable); ok && tc.SupportsToolCalling() {
		vfsDefs, vfsMap := buildToolDefs(k.vfs.DeviceRegistry(), proc.AllowedDevices, proc.PlanningEnabled)
		metaDefs, metaMap := metaToolDefs(proc.PlanningEnabled, proc.DeferredSkills)
		allDefs := make([]vfs.ToolDef, 0, len(vfsDefs)+len(metaDefs))
		allDefs = append(allDefs, vfsDefs...)
		allDefs = append(allDefs, metaDefs...)
		proc.nativeToolDefs = allDefs
		proc.toolMap = make(map[string]toolMapping, len(vfsMap)+len(metaMap))
		maps.Copy(proc.toolMap, vfsMap)
		maps.Copy(proc.toolMap, metaMap)
	}

	for _, dev := range proc.AllowedDevices {
		if strings.HasPrefix(dev, "/dev/mcp/") || strings.HasPrefix(dev, "/mnt/mcp/") {
			proc.mcpDevicePaths = append(proc.mcpDevicePaths, dev)
		}
	}
}
