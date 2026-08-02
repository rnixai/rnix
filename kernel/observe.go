package kernel

import (
	"encoding/json"
	"fmt"
	"log"
	"maps"
	"strings"
	"sync"
	"time"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/debug"
	"github.com/rnixai/rnix/drivers/llm"
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
	} else if k.dataDir != "" && shouldWarnEventDrop(proc) && proc.eventDropWarned.CompareAndSwap(false, true) {
		// First-time warning per process that we are dropping disk events.
		// This is the breadcrumb that would have saved hours during Epic 44
		// follow-up debugging — before this line, a placeholder revived by
		// LoadSuspendedFromDisk silently swallowed every Resume / Resurrect /
		// Suspend audit event because reasonStep had not yet built an
		// EventWriter. The shouldWarnEventDrop filter scopes the noise to
		// processes that SHOULD have an EventWriter: reasonStep-driven (i.e.
		// PrimaryDevice non-empty) and live (Running / Suspended). Tests
		// using bare NewProcess fixtures, script-runners that detach their
		// writer on purpose, and post-reap finalize emits stay silent.
		log.Printf("[event-drop] pid=%d uuid=%s syscall=%s state=%s — reasonStep-driven proc has no EventWriter; further drops on this proc will be silent",
			proc.PID, proc.UUID, syscall, proc.GetState())
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

// EmitScriptEvent is the public entry for non-reasoning processes (currently
// the script-runner with SpawnOpts.SkipReasonLoop=true, Story 43.2 OBS-1) to
// inject control-flow trace events into the same observation pipeline used
// by reasonStep. Internally it forwards to emitEvent so events.jsonl,
// DebugChan, recordMgr, and immuneDaemon all see the event consistently —
// callers MUST NOT bypass this and write to EventWriter directly, otherwise
// the rest of the observation stack (Dashboard subscribers, trace recorder,
// immune monitor) silently misses the event.
//
// proc == nil is a no-op (defensive: the script-runner Reap path races with
// late emit callbacks during shutdown). When the process has no EventWriter
// attached (e.g. NewEventWriter init failed in handleExecScript), the inner
// emitEvent gates on `ew != nil` and silently skips the disk write — the
// other side effects (DebugChan, recordMgr, immune) still fire on the
// best-effort principle.
func (k *KernelImpl) EmitScriptEvent(proc *Process, syscall string, args map[string]any) {
	if proc == nil {
		return
	}
	k.emitEvent(proc, syscall, args, nil, nil, 0)
}

// shouldWarnEventDrop reports whether the calling emitEvent should surface a
// "no EventWriter" warning for this proc. We scope the warning to processes
// that are SUPPOSED to have an EventWriter:
//   - PrimaryDevice non-empty → reasonStep-driven LLM agent (script-runners
//     and bare test fixtures stay quiet).
//   - State Running or Suspended → live processes only. Post-reap finalize
//     emits, NewProcess test fixtures sitting in StateCreated, and Zombie/Dead
//     are excluded.
func shouldWarnEventDrop(proc *Process) bool {
	if proc.PrimaryDevice == "" {
		return false
	}
	switch proc.GetState() {
	case types.StateRunning, types.StateSuspended:
		return true
	default:
		return false
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
//
// 设计契约（spec-step-inspector-data-fidelity）：一次调用 = 一个 reasonStep 完整快照
// = 一行 steps.jsonl。caller 应在 step 完成时（含所有 parallel tool calls 执行完）
// 调用本函数,不要在 toolLoop 内反复调用。
//
//   - toolCalls 数组承载该 step 内所有 parallel tool calls；为空表示非 tool 路径
//     （text / complete / error 等）。当数组非空时,旧字段 toolPath/toolInput/toolResult/
//     toolError/toolDuration 仍从 caller 传入（通常取数组末元素以兼容旧 reader）。
//   - resp 中 InputTokens/OutputTokens/CachedInputTokens 透传至 StepRecord 同名字段。
func (k *KernelImpl) writeStepRecord(proc *Process, step int, promptResult *rnixctx.PromptResult,
	rawResponse string, resp *llmResponse, actionType string, summary string,
	toolPath string, toolInput string, toolResult string, toolError string, toolDuration time.Duration,
	toolCalls []types.ToolCallRecord) {

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
		ToolCalls:    toolCalls,
	}
	if resp != nil {
		rec.TokenCount = resp.TokensUsed
		rec.ResponseTokens = resp.TokensUsed
		rec.InputTokens = resp.InputTokens
		rec.OutputTokens = resp.OutputTokens
		rec.CachedInputTokens = resp.CachedInputTokens
	}

	if err := sw.WriteStep(rec); err != nil {
		log.Printf("[step_writer] write error pid=%d step=%d: %v", proc.PID, step, err)
	}
}

const maxDriverToolResultBytes = 64 * 1024

// truncateToBytes caps s at maxBytes, appending a UTF-8-safe truncation marker
// when it overflows. The marker text is the single canonical truncation marker
// in the codebase — do not introduce a second wording.
//
// Idempotent: a string already at or below maxBytes is returned verbatim, so
// truncating an already-truncated value never produces a double marker.
func truncateToBytes(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	marker := fmt.Sprintf("\n[truncated: %d bytes total]", len(s))
	limit := max(0, maxBytes-len(marker))
	return truncateUTF8Bytes(s, limit) + marker
}

func truncateDriverToolResult(s string) string {
	return truncateToBytes(s, maxDriverToolResultBytes)
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
		ToolResult:   truncateDriverToolResult(toolResult),
		ToolDuration: toolDuration,
		Messages:     messages,
		MessageCount: messageCount,
	}
	if err := sw.WriteStep(rec); err != nil {
		log.Printf("[step_writer] driver step write error pid=%d step=%d: %v", proc.PID, step, err)
	}
}

// buildBatchToolSummary 为一个 reasonStep 内 N 个 parallel tool calls 生成 summary 文案。
//
//   - N==1：沿用 driverToolSummary 单 call 格式（向后兼容)。
//   - N>1：返回 "tool_call ×N: <name1>, <name2>, ..."(超过 3 个名字截断)。
func buildBatchToolSummary(calls []types.ToolCallRecord) string {
	if len(calls) == 0 {
		return ""
	}
	if len(calls) == 1 {
		return driverToolSummary(calls[0].Name, calls[0].Path, calls[0].Input)
	}
	names := make([]string, 0, len(calls))
	for _, c := range calls {
		name := c.Path
		if name == "" {
			name = c.Name
		}
		names = append(names, name)
	}
	if len(names) > 3 {
		names = append(names[:3], "...")
	}
	return fmt.Sprintf("tool_call ×%d: %s", len(calls), strings.Join(names, ", "))
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
	case "Glob", "Grep":
		key = "pattern"
	case "Read", "Write", "Edit":
		key = "path"
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
	case "content":
		// 消息级 agent_message（token 级 delta 在 handler 入口已被过滤）。
		return "DriverMessage"
	case "item":
		// driver 透传的未知 item 类型（如 codex error item）。
		return "DriverItem"
	default:
		return "DriverEvent"
	}
}

// evtInt reads an integer field from a driver event map (Story 66.6). The
// usage event is constructed in-process by vfsfile.writeStream with Go int
// values, but float64 is also handled defensively in case the map ever arrives
// via a JSON round-trip. Missing / non-numeric keys yield 0.
func evtInt(evt map[string]any, key string) int {
	switch v := evt[key].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	default:
		return 0
	}
}

// driverEventToLog maps a driver event to a LogEntry category and content.
func driverEventToLog(evt map[string]any) (types.LogCategory, string, string, bool) {
	evtType, _ := evt["type"].(string)
	subtype, _ := evt["subtype"].(string)
	contentField, _ := evt["content"].(string)

	switch evtType {
	case "tool_call":
		// Story 65.1 (AC #4): input fragments are not log entries — the
		// block-level aggregate carries the complete input instead.
		if contentField == "input_delta" {
			return "", "", "", false
		}
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
	case "content":
		if subtype != "agent_message" {
			return "", "", "", false
		}
		text := strings.TrimSpace(contentField)
		if text == "" {
			return "", "", "", false
		}
		r := []rune(text)
		if len(r) > 80 {
			text = string(r[:80]) + "..."
		}
		return types.LogOutput, text, "", true
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
		type pendingToolState struct {
			step     int
			tool     string
			summary  string
			toolPath string
			start    time.Time
			inputBuf strings.Builder
			result   string
			callID   string
		}
		// pendings holds every tool call that has been `started` but not yet
		// flushed, keyed by call_id. Story 40.4 (multi-slot, review F1): a single
		// claude assistant message can in principle carry multiple tool_use
		// blocks (parallel calls), so more than one tool may be mid-flight at
		// once. `current` points at the tool currently accumulating input_delta
		// fragments (the most recently `started` one, since claude streams one
		// content block's deltas at a time). Tools without a call_id (cursor) are
		// tracked via `current` + pendingOrder and flushed by their `completed`
		// event or the stream-end backstop.
		pendings := make(map[string]*pendingToolState)
		// pendingOrder preserves started order across all in-flight tools (both
		// call_id-keyed and id-less), so flushAll persists step records
		// deterministically rather than in random map-iteration order.
		var pendingOrder []*pendingToolState
		var current *pendingToolState
		// authInputs maps a tool call_id → its marshaled authoritative input
		// JSON, captured from assistant tool_use blocks. Story 40.4: claude CLI
		// interleaves the previous round's user(tool_result) with the next
		// tool's partial input deltas, so the incremental inputBuf can be
		// truncated; the assistant block carries the complete authoritative
		// input and overrides inputBuf at flush time.
		authInputs := make(map[string]string)
		var msgHistory []rnixctx.Message
		if proc.Intent != "" {
			msgHistory = append(msgHistory, rnixctx.Message{
				Role:    rnixctx.RoleUser,
				Content: proc.Intent,
			})
		}

		// flushTool persists one pending tool's step record. It prefers the
		// authoritative input captured from the assistant tool_use block (keyed
		// by call_id) over the incrementally accumulated inputBuf, which can be
		// truncated when claude CLI interleaves the previous round's
		// user(tool_result) with this tool's partial deltas. Falls back to
		// inputBuf when no authoritative input is available (AC #3 / INT-006).
		// After persisting it removes the tool from the pending tracking
		// structures so it is never flushed twice.
		flushTool := func(p *pendingToolState) {
			if p == nil || p.step == 0 {
				return
			}
			dur := time.Since(p.start)
			toolInput := p.inputBuf.String()
			if p.callID != "" {
				if auth, ok := authInputs[p.callID]; ok {
					toolInput = auth
				}
			}
			summary := driverToolSummary(p.tool, p.toolPath, toolInput)
			var msgsRaw json.RawMessage
			msgCount := len(msgHistory)
			if msgCount > 0 {
				if data, err := json.Marshal(msgHistory); err == nil {
					msgsRaw = data
				}
			}
			k.writeDriverStepRecordFull(proc, p.step, p.tool,
				summary, p.toolPath, toolInput, p.result, dur,
				msgsRaw, msgCount)
			// Story 65.1 (AC #2, 裁决 1/2/4): every tool call gets exactly one
			// terminal aggregate event at flush time, carrying the complete
			// (authoritative-first) input and result. Emitted for all drivers —
			// codex/cursor downstream consumers see a redundant-but-uniform
			// signal alongside the driver-native `completed` event. Content is
			// bounded by the same 64KiB UTF-8-safe truncation as tool results;
			// steps.jsonl remains the fidelity layer for the full input.
			//
			// Story 72.1 code-review (2026-08-02): that last sentence is a real
			// invariant, not a remark — StepWriter deliberately bounds the input
			// fields at 4 MB rather than 64 KB so this file can truncate while
			// steps.jsonl keeps the whole document (kernel/step_writer.go
			// truncateStepRecordForWrite). Do NOT "unify" the two quanta.
			aggEvt := map[string]any{
				"type":        "tool_call",
				"subtype":     "aggregate",
				"tool":        p.tool,
				"input":       truncateDriverToolResult(toolInput),
				"result":      truncateDriverToolResult(p.result),
				"duration_ms": dur.Milliseconds(),
				"step":        p.step,
			}
			if p.callID != "" {
				aggEvt["call_id"] = p.callID
			}
			if path, ok := strings.CutPrefix(p.toolPath, p.tool+":"); ok {
				aggEvt["path"] = path
			}
			k.emitEvent(proc, "DriverToolCall", aggEvt, nil, nil, 0)
			// Story 66.6: bump the process-level tool-call liveness counter once
			// per CLI DriverToolCall flush (the ACTIVE ps column keeps moving even
			// when usage data is unavailable).
			proc.IncToolCallCount()
			// Remove from tracking so it cannot be flushed again. The
			// authoritative input entry is deleted to bound memory and prevent
			// crosstalk between calls reusing identifiers.
			if p.callID != "" {
				delete(authInputs, p.callID)
				delete(pendings, p.callID)
			}
			for i, np := range pendingOrder {
				if np == p {
					pendingOrder = append(pendingOrder[:i], pendingOrder[i+1:]...)
					break
				}
			}
			if current == p {
				current = nil
			}
			p.step = 0
		}

		// flushAll drains every still-pending tool in started order. Used at
		// stream end so the final tool in a session is never lost (review F1 /
		// defer #36).
		flushAll := func() {
			for len(pendingOrder) > 0 {
				flushTool(pendingOrder[0])
			}
			current = nil
		}

		// Story 65.1 (AC #1, 裁决 2): thinking block accumulator. Fragments are
		// concatenated verbatim (claude/cursor/API deltas are intra-word splits;
		// a separator would corrupt the text). Block boundaries: any non-thinking
		// event, a new `started` marker, or the done/error backstop.
		var thinkingBuf strings.Builder
		var thinkingStart, thinkingLast time.Time
		thinkingFragments := 0

		// flushThinking emits one DriverThinking aggregate event (subtype=
		// "aggregate", 裁决 1) carrying the full block text (64KiB UTF-8-safe
		// truncation, 裁决 4) and produces the single block-level LogThink via
		// the unchanged driverEventToLog pure function (裁决 3 — the 80-rune
		// truncation precedent applies naturally). A block with zero valid
		// fragments (e.g. started-only) emits nothing.
		flushThinking := func() {
			if thinkingFragments == 0 {
				return
			}
			aggEvt := map[string]any{
				"type":        "thinking",
				"subtype":     "aggregate",
				"content":     truncateDriverToolResult(thinkingBuf.String()),
				"fragments":   thinkingFragments,
				"duration_ms": thinkingLast.Sub(thinkingStart).Milliseconds(),
			}
			k.emitEvent(proc, "DriverThinking", aggEvt, nil, nil, 0)
			if cat, content, toolPath, ok := driverEventToLog(aggEvt); ok {
				k.emitLog(proc, 0, cat, content, toolPath)
			}
			thinkingBuf.Reset()
			thinkingFragments = 0
		}

		// recordAuthInput marshals an assistant tool_use block's authoritative
		// input and stores it keyed by call_id (Story 40.4 AC #1/#5). A nil input
		// (no-arg tool) is recorded as "{}" so a present-but-empty input is
		// distinguishable from "no authoritative input captured" at flush time.
		// Marshal failures are silently skipped (the call falls back to inputBuf).
		recordAuthInput := func(id string, input any, present bool) {
			if id == "" || !present {
				return
			}
			if input == nil {
				authInputs[id] = "{}"
				return
			}
			if data, err := json.Marshal(input); err == nil {
				authInputs[id] = string(data)
			}
		}
		// Story 56.6: subagentTracker reconstructs CLI subagents (Claude Code
		// Task/Agent) into synthetic child process-tree nodes. Created only for
		// CLI-driver hosts (AC5 gate — API drivers take the real ActionSpawn
		// path); nil otherwise so the synthesis branches are skipped entirely.
		// Gated on the driver TYPE (via vfs.DriverTypeProvider) rather than the
		// provider-named device path — a claude-cli provider named anything other
		// than "claude" still reconstructs its subagents (Story 56.6 review).
		var subagentTracker *cliSubagentTracker
		var codexRolloutTracker *codexRolloutTracker
		var driverType string
		if dtp, ok := file.(vfs.DriverTypeProvider); ok {
			driverType = dtp.DriverType()
		}
		// Story 66.6: snapshot the resolved driver type so the step boundary
		// (reason.go) can label usage provenance cli_stream vs api_response.
		// Set under proc.mu for -race safety against the reasonStep reader.
		proc.mu.Lock()
		proc.DriverType = driverType
		proc.mu.Unlock()
		if llm.IsCLIDriver(driverType) {
			subagentTracker = k.newCLISubagentTracker(proc)
		}
		var streamTrackerMu sync.Mutex
		cleanupStreamTrackers := func() {
			streamTrackerMu.Lock()
			defer streamTrackerMu.Unlock()
			if subagentTracker != nil {
				subagentTracker.finalizeAll()
			}
			if codexRolloutTracker != nil {
				codexRolloutTracker.stop()
				codexRolloutTracker = nil
			}
		}
		proc.AddStreamCleanup(cleanupStreamTrackers)
		obs.SetStreamHandler(func(evt map[string]any) {
			// Driver activity refreshes heartbeat — prevents false stall
			// detection during long-running streaming LLM calls.
			proc.TouchHeartbeat()

			evtType, _ := evt["type"].(string)

			// Story 56.6: route subagent-internal frames to their synthetic child
			// BEFORE any host-side emit. A frame carrying parent_tool_use_id that
			// matches a tracked Task dispatch is the subagent's own activity — its
			// tool steps go to the child's steps.jsonl and its syscall events to
			// the child's events.jsonl, and the host handler returns so the host's
			// step stream, events.jsonl, and live LogChan are never polluted (AC3
			// + Timeline isolation, Story 56.6 review). Frames whose
			// parent_tool_use_id matches no known child fall through to normal
			// host processing below.
			if subagentTracker != nil && subagentTracker.route(evt) {
				return
			}

			// Story 66.6: mid-stream usage delta from a CLI driver (claude/codex).
			// Accumulate into the live per-step counter for real-time ps/proc-info
			// preview and mark provenance cli_stream. Placed BEFORE the content
			// early-return and any emit — usage events only update in-memory
			// counters and are NOT written to events.jsonl (high-frequency, same
			// rationale as the content fan-out; heartbeat already refreshed above).
			if evtType == "usage" {
				// code-review: pick provenance by driver family instead of
				// hardcoding cli_stream. Only CLI drivers emit usage today (拍板 3),
				// but if a future API driver ever emits a mid-stream usage event, a
				// hardcoded cli_stream(rank 2) would sink the step boundary's correct
				// api_response(rank 3) permanently — mergeProvenance keeps the LOWEST
				// rank. Aligned with the step-boundary decision in reason.go.
				usageProv := vfs.UsageProvenanceCLIStream
				if !llm.IsCLIDriver(driverType) {
					usageProv = vfs.UsageProvenanceAPIResponse
				}
				proc.AddStreamUsage(evtInt(evt, "tokens_used"), evtInt(evt, "input_tokens"), usageProv)
				return
			}

			// Story 65.1 (裁决 2): thinking block boundary detection + fragment
			// accumulation. This sits BEFORE the content early-return below so a
			// pure-text stream phase flushes the preceding thinking block
			// immediately instead of leaving it suspended until `done`. Fragment
			// predicate (content non-blank && content != subtype) matches the
			// driverEventToLog thinking filter and covers all four sources:
			// claude/qwen (subtype=delta), cursor (passthrough subtype), API
			// drivers and codex (no subtype key). The `started` marker only
			// delimits blocks and never enters the text (60.2 semantics).
			if evtType == "thinking" {
				sub, _ := evt["subtype"].(string)
				if sub == "started" {
					flushThinking()
				}
				// A block opens only on a non-blank fragment, but once open,
				// whitespace-only fragments (paragraph separators like "\n\n")
				// are appended too — verbatim concatenation must match the
				// per-delta reconstruction (60.2) or paragraphs would fuse.
				if content, _ := evt["content"].(string); content != "" && content != sub {
					if strings.TrimSpace(content) != "" || thinkingFragments > 0 {
						if thinkingFragments == 0 {
							thinkingStart = time.Now()
						}
						thinkingBuf.WriteString(content)
						thinkingFragments++
						thinkingLast = time.Now()
					}
				}
			} else {
				flushThinking()
			}

			// content 事件分流（调查 codex-cli-observability-parity R3）：
			// API driver 的 token 级 content delta 高频到达，仅用于刷新
			// heartbeat（上面的 TouchHeartbeat 已完成），直接返回避免
			// events.jsonl 洪泛；CLI driver 的消息级 agent_message
			// （subtype 标记）继续向下落盘留痕。
			if evtType == "content" {
				if sub, _ := evt["subtype"].(string); sub != "agent_message" {
					return
				}
			}

			syscallName := driverEventToSyscall(evtType)
			k.emitEvent(proc, syscallName, evt, nil, nil, 0)

			// Story 65.1 (AC #4, 裁决 3): thinking fragments no longer produce
			// per-fragment LogThink entries — the accumulator emits one
			// block-level entry at flush time via the same pure function.
			if evtType != "thinking" {
				if cat, content, toolPath, ok := driverEventToLog(evt); ok {
					k.emitLog(proc, 0, cat, content, toolPath)
				}
			}

			// Story 60.1 AC2: 思考事件(CLI driver 的 "thinking" + AC1 归一后的
			// API driver reasoning)除写 LogChan 外,还驱动 OnThinking 回调,使默认
			// 前台 spawn 在长思考阶段有实时反馈。传完整(未截断)思考文本——节流/
			// 呈现由前台渲染层(internal/ui)负责。step 取当前进行中的 step(已完成
			// step + 1),atomic 读保证 streaming goroutine 内线程安全。回调实现非
			// 阻塞,不阻塞 reasonStep(与 emitLog 非阻塞语义一致)。
			if evtType == "thinking" && k.callbacks != nil {
				if text, _ := evt["content"].(string); strings.TrimSpace(text) != "" {
					step := int(proc.LastCompletedStep.Load()) + 1
					k.callbacks.OnThinking(proc.PID, step, text)
				}
			}

			if evtType == "system" {
				if driverType == llm.DriverCodexCLI {
					if sub, _ := evt["subtype"].(string); sub == "init" {
						if threadID, _ := evt["thread_id"].(string); threadID != "" {
							streamTrackerMu.Lock()
							if codexRolloutTracker == nil {
								codexRolloutTracker = newCodexRolloutTracker(k, proc,
									k.syntheticChildBaseDir(proc), threadID, codexRolloutDirForNow(time.Now()))
								codexRolloutTracker.start()
							}
							streamTrackerMu.Unlock()
						}
					}
				}
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

			// Story 56.6: detect subagent lifecycle on host (main-thread) frames.
			// An assistant Task/Agent tool_use whose input carries subagent_type
			// synthesizes a child node (AC2); the host's matching tool_result
			// finalizes it (AC4). The host still records the Task as its own
			// aggregate step (Dev Notes decision #1) — these run alongside, not
			// instead of, the normal host processing below.
			if subagentTracker != nil {
				switch evtType {
				case "assistant":
					for _, b := range extractToolUseBlocks(evt) {
						if isSubagentDispatch(b) {
							subagentTracker.getOrCreate(b)
						}
					}
				case "user":
					for _, r := range extractToolResultBlocks(evt) {
						if r.toolUseID != "" {
							subagentTracker.finalizeByToolResult(r.toolUseID, r.content, r.isError)
						}
					}
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
								if evtType == "assistant" {
									input, present := block["input"]
									recordAuthInput(id, input, present)
								}
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
									if evtType == "assistant" {
										input, present := block["input"]
										recordAuthInput(id, input, present)
									}
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
							if evtType == "assistant" {
								input, present := c["input"]
								recordAuthInput(id, input, present)
							}
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

					// Story 62.5: assistant only records authoritative input.
					// claude/qwen deliver tool output later via user(tool_result);
					// flush when that result arrives so steps.jsonl carries both
					// ToolInput and ToolResult. Matching is by tool_use_id, which
					// avoids the 40.4 regression where user frames flushed the
					// currently accumulating next tool.
					if evtType == "user" {
						for _, r := range extractToolResultBlocks(evt) {
							if r.toolUseID == "" {
								continue
							}
							if p, ok := pendings[r.toolUseID]; ok {
								p.result = r.content
								flushTool(p)
							}
						}
					}
				}
			}

			switch evtType {
			case "tool_call":
				contentVal, _ := evt["content"].(string)
				switch contentVal {
				case "started":
					// Multi-slot (review F1): a new tool starting does NOT flush
					// prior pending tools. Parallel claude tool_use blocks arrive
					// as started(A), started(B), ... before the combined assistant
					// event; flushing on started(B) would truncate A before its
					// authoritative input arrives. Each tool now owns its own slot
					// and is flushed by its matching user(tool_result) event
					// (claude/qwen), its completed event (codex/cursor), or the
					// stream-end `done` backstop.
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
					p := &pendingToolState{
						step:     driverStep,
						tool:     tool,
						summary:  summary,
						toolPath: toolPath,
						start:    time.Now(),
					}
					p.callID, _ = evt["call_id"].(string)
					if cmd != "" {
						p.inputBuf.WriteString(cmd)
					} else if pathStr != "" {
						p.inputBuf.WriteString(pathStr)
					}
					if argsStr, ok := evt["args"].(string); ok && argsStr != "" {
						if p.inputBuf.Len() == 0 {
							p.inputBuf.WriteString(argsStr)
						}
					}
					if p.callID != "" {
						pendings[p.callID] = p
					}
					pendingOrder = append(pendingOrder, p)
					current = p
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
					// Deltas belong to the most recently started tool (claude
					// streams one content block's input fragments at a time).
					if partial, ok := evt["partial_json"].(string); ok && current != nil {
						current.inputBuf.WriteString(partial)
					}
				case "completed":
					// codex/cursor flush each tool on its own `completed` event.
					// Resolve the target by call_id when present (codex), else
					// fall back to `current` (cursor carries no call_id).
					p := current
					if cid, _ := evt["call_id"].(string); cid != "" {
						if tracked, ok := pendings[cid]; ok {
							p = tracked
						}
					}
					if p != nil {
						if result, ok := evt["result"].(string); ok && result != "" {
							p.result = result
						}
						if p.result == "" {
							if result, ok := evt["tool_result"].(string); ok {
								p.result = result
							}
						}
						flushTool(p)
					}
				}
			case "done":
				// Stream-end backstop (review F1 / defer #36): flush any tool
				// still pending. claude emits no `completed` for tool calls, so a
				// terminal tool whose authoritative input never arrived (or whose
				// assistant event was the stream's last meaningful message) would
				// otherwise be dropped. flushAll drains both id-keyed and
				// no-id pending tools.
				flushAll()
				// Story 65.1 backstop: flush a still-suspended thinking block at
				// stream end (idempotent — the handler-top boundary check usually
				// already flushed it).
				flushThinking()
				// Story 56.6 (AC4): finalize any subagent child still Running at
				// stream end (no explicit Task tool_result) so synthetic nodes
				// never leak in a perpetual Running state.
				cleanupStreamTrackers()
			// NOTE (Stories 40.4/62.5): user events only flush tools when their
			// tool_result block matches the pending call_id. This preserves the
			// 40.4 input fix (no unconditional user flush of the next tool) while
			// letting claude/qwen populate ToolResult before the step is written.
			// Remaining tools are drained by each tool's completed event
			// (codex/cursor) or by the stream-end `done` backstop.
			case "error":
				// Stream-error backstop (65.1 review): an error terminates the
				// stream just like `done` — drain pending tools so every tool
				// call still gets its steps.jsonl record and aggregate event,
				// then flush the suspended thinking block (both idempotent).
				flushAll()
				flushThinking()
				// Story 56.6 (AC4): a terminating error also ends the stream —
				// finalize dangling subagent children so they do not leak.
				cleanupStreamTrackers()
			}
		})
	}

	if tc, ok := file.(vfs.ToolCapable); ok && tc.SupportsToolCalling() {
		vfsDefs, vfsMap := buildToolDefs(k.vfs.DeviceRegistry(), proc.AllowedDevices)
		metaDefs, metaMap := metaToolDefs(proc.FeatureFlags, proc.DeferredSkills)
		allDefs := make([]vfs.ToolDef, 0, len(vfsDefs)+len(metaDefs))
		allDefs = append(allDefs, vfsDefs...)
		allDefs = append(allDefs, metaDefs...)
		proc.nativeToolDefs = allDefs
		proc.toolMap = make(map[string]toolMapping, len(vfsMap)+len(metaMap))
		maps.Copy(proc.toolMap, vfsMap)
		maps.Copy(proc.toolMap, metaMap)
		// 路线 B 的 MCP native ToolDef 注入不在此处——MCP 挂载发生在工具集组装之后
		// (spawn.go 的 auto-mount、resume 的 reattachMCPMounts)，故 MCP 工具由
		// k.attachMCPToolDefs 在挂载完成后追加。见 kernel/mcp_toolgen.go。
	}

	for _, dev := range proc.AllowedDevices {
		if strings.HasPrefix(dev, "/dev/mcp/") || strings.HasPrefix(dev, "/mnt/mcp/") {
			proc.mcpDevicePaths = append(proc.mcpDevicePaths, dev)
		}
	}
}
