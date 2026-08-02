package ipc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"time"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/debug"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/kernel"
	"github.com/rnixai/rnix/vfs"
)

func (s *Server) handleCtxProfile(conn net.Conn, rawPayload json.RawMessage) {
	var req CtxProfileRequest
	if err := json.Unmarshal(rawPayload, &req); err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID", Message: "invalid ctx_profile request"}})
		return
	}

	pid, pidOK := s.resolvePID(req.PID, req.UUID)
	if !pidOK {
		// Fallback: try loading ctx-profile.json from disk for reaped processes
		s.handleCtxProfileFromDisk(conn, req.PID, req.UUID)
		return
	}

	info, err := s.kern.GetProcInfo(pid)
	if err != nil {
		s.handleCtxProfileFromDisk(conn, req.PID, req.UUID)
		return
	}

	if info.State != types.StateRunning && info.State != types.StateZombie {
		if info.State == types.StateDead {
			// Dead process: try loading snapshot from disk
			s.handleCtxProfileFromDisk(conn, req.PID, req.UUID)
		} else {
			writeResponse(conn, Response{OK: false, Error: &ErrorPayload{
				Code:    "INVALID",
				Message: fmt.Sprintf("process %d is in %s state; ctx-profile requires running or zombie", pid, info.State),
			}})
		}
		return
	}

	if s.ctxMgr == nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INTERNAL", Message: "context manager not available"}})
		return
	}

	// NFR34: analysis must complete within 1s
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	type ctxProfileResult struct {
		payload []byte
		err     error
	}
	done := make(chan ctxProfileResult, 1)
	go func() {
		rawCtx, err := s.ctxMgr.CtxRead(info.CtxID, 0, 0)
		if err != nil {
			done <- ctxProfileResult{err: fmt.Errorf("failed to read context: %w", err)}
			return
		}

		var ctxData debug.ContextData
		if err := json.Unmarshal(rawCtx, &ctxData); err != nil {
			done <- ctxProfileResult{err: fmt.Errorf("failed to parse context: %w", err)}
			return
		}

		result := debug.AnalyzeContext(&ctxData, info.PID, info.CtxID, info.TokensUsed, info.ContextBudget)
		payload, err := json.Marshal(result)
		if err != nil {
			done <- ctxProfileResult{err: fmt.Errorf("failed to marshal result: %w", err)}
			return
		}
		done <- ctxProfileResult{payload: payload}
	}()

	select {
	case r := <-done:
		if r.err != nil {
			writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INTERNAL", Message: r.err.Error()}})
			return
		}
		writeResponse(conn, Response{OK: true, Payload: r.payload})
	case <-ctx.Done():
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID", Message: "ctx-profile analysis timed out (NFR34: 1s limit)"}})
	}
}

// handleCtxProfileFromDisk loads a saved ctx-profile.json from disk for dead processes.
func (s *Server) handleCtxProfileFromDisk(conn net.Conn, pid types.PID, uuid string) {
	if uuid == "" && pid != 0 {
		if hist := s.kern.FindHistoryByPID(pid); hist != nil {
			uuid = hist.UUID
		}
	}
	if uuid == "" || !isValidUUID(uuid) {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "NOT_FOUND", Message: "process not found"}})
		return
	}
	baseDir := kernel.FindBaseDirByUUID(s.kern.GetDataDir(), uuid)
	if baseDir == "" {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "NOT_FOUND", Message: "no data found for UUID"}})
		return
	}
	path := filepath.Join(baseDir, "steps", uuid, "ctx-profile.json")
	data, err := os.ReadFile(path)
	if err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "NOT_FOUND", Message: "context profile not available"}})
		return
	}
	writeResponse(conn, Response{OK: true, Payload: json.RawMessage(data)})
}

func (s *Server) handleCtxGrowth(conn net.Conn, rawPayload json.RawMessage) {
	var req CtxGrowthRequest
	if err := json.Unmarshal(rawPayload, &req); err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID", Message: "invalid ctx_growth request"}})
		return
	}

	pid, pidOK := s.resolvePID(req.PID, req.UUID)
	if !pidOK {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "NOT_FOUND", Message: "process not found"}})
		return
	}

	info, err := s.kern.GetProcInfo(pid)
	if err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "NOT_FOUND", Message: "process not found"}})
		return
	}

	if info.State != types.StateRunning {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{
			Code:    "INVALID",
			Message: fmt.Sprintf("process %d is in %s state; ctx-growth requires running", pid, info.State),
		}})
		return
	}

	history, err := s.kern.GetTokenHistory(pid)
	if err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INTERNAL", Message: err.Error()}})
		return
	}

	currentStep := 0
	if len(history) > 0 {
		currentStep = history[len(history)-1].Step
	}

	maxSteps := info.MaxSteps
	if maxSteps == 0 {
		// Infinite steps: cannot predict step exhaustion, return empty prediction
		result := debug.PredictGrowth(info.PID, info.TokensUsed, info.ContextBudget, currentStep, 0, history)
		payload, err := json.Marshal(result)
		if err != nil {
			writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INTERNAL", Message: err.Error()}})
			return
		}
		writeResponse(conn, Response{OK: true, Payload: payload})
		return
	}
	result := debug.PredictGrowth(info.PID, info.TokensUsed, info.ContextBudget, currentStep, maxSteps, history)

	payload, err := json.Marshal(result)
	if err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INTERNAL", Message: err.Error()}})
		return
	}
	writeResponse(conn, Response{OK: true, Payload: payload})
}

func (s *Server) handleLineage(conn net.Conn, rawPayload json.RawMessage) {
	var req LineageRequest
	if err := json.Unmarshal(rawPayload, &req); err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID", Message: "invalid lineage request"}})
		return
	}

	pid, pidOK := s.resolvePID(req.PID, req.UUID)
	if !pidOK {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "NOT_FOUND", Message: "process not found"}})
		return
	}

	events, err := s.kern.GetLineage(pid)
	if err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "NOT_FOUND", Message: "process not found"}})
		return
	}

	ipcEvents := make([]LineageEvent, len(events))
	for i, e := range events {
		skills := e.Skills
		if skills == nil {
			skills = []string{}
		}
		ipcEvents[i] = LineageEvent{
			TimestampMs: e.Timestamp.UnixMilli(),
			Phase:       e.Phase,
			Skills:      skills,
			Trigger:     e.Trigger,
			FromMemory:  e.FromMemory,
		}
	}

	resp := LineageResponse{
		PID:    pid,
		Events: ipcEvents,
	}
	payload, _ := json.Marshal(resp)
	writeResponse(conn, Response{OK: true, Payload: payload})
}

// handleGetStepDetail returns the full prompt and step data for a specific process step (Story 27.2).
func (s *Server) handleGetStepDetail(conn net.Conn, rawPayload json.RawMessage) {
	var req GetStepDetailRequest
	if err := json.Unmarshal(rawPayload, &req); err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID", Message: "invalid get_step_detail request"}})
		return
	}

	// Phase A: resolve SystemPrompt + ToolDefs + Provider + DriverType (AC#3)
	var systemPrompt string
	var toolDefs []vfs.ToolDef
	var stepsPath string
	var providerName string
	var contextWindow int

	proc, procFound := s.resolveProcess(req.PID, req.UUID)
	if procFound {
		systemPrompt = proc.GetFinalSystemPrompt()
		toolDefs = proc.GetNativeToolDefs()
		stepsPath = s.resolveStepsPathFromProc(proc)
		providerName = proc.Provider
		contextWindow = proc.ContextWindow
	} else {
		// Process not in memory — try UUID from request, then fall back to process history
		uuid := req.UUID
		if uuid == "" && req.PID != 0 {
			if hist := s.kern.FindHistoryByPID(req.PID); hist != nil {
				uuid = hist.UUID
				providerName = hist.Provider
				contextWindow = hist.ContextWindow
			}
		} else if uuid != "" {
			if hist := s.kern.FindHistoryByUUID(uuid); hist != nil {
				providerName = hist.Provider
				contextWindow = hist.ContextWindow
			}
		}
		stepsPath = s.resolveStepsPath(req.PID, uuid)
		if stepsPath == "" {
			writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "NOT_FOUND", Message: "process not found"}})
			return
		}
		metaPath := filepath.Join(filepath.Dir(stepsPath), "process-meta.json")
		if meta, err := readProcessMeta(metaPath); err == nil {
			systemPrompt = meta.SystemPrompt
			toolDefs = meta.ToolDefs
		} else if !os.IsNotExist(err) {
			// Absence is expected (Story 56.6 synthetic subagent dirs never
			// have a process-meta.json); anything else is a real host
			// process whose meta got corrupted — leave a breadcrumb.
			log.Printf("get_step_detail: process-meta.json unreadable, degrading to empty prompt (uuid=%s): %v", uuid, err)
		}
		// Either way degrade to an empty systemPrompt / nil toolDefs instead
		// of NOT_FOUND — the step data in steps.jsonl is still fully served,
		// and toolDefs falls back to the StepRecord below (only when the
		// step carries a non-empty Action).
	}

	if stepsPath == "" {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "NOT_FOUND", Message: "process not found"}})
		return
	}

	// Phase B: read StepRecord
	rec, err := kernel.ReadStep(stepsPath, req.Step)
	if err != nil {
		if errors.Is(err, kernel.ErrStepNotFound) {
			// The step genuinely does not exist — preserve the original wording.
			writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "not_found", Message: fmt.Sprintf("step %d not yet recorded", req.Step)}})
			return
		}
		// Story 72.1 AC3: a real read failure must not be reported as
		// "step not yet recorded" — surface it as an internal error.
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: string(types.ErrInternal), Message: fmt.Sprintf("read step %d: %v", req.Step, err)}})
		return
	}

	// Fallback: if toolDefs is empty, build from StepRecord
	if len(toolDefs) == 0 && rec.Action != "" {
		toolDefs = []vfs.ToolDef{{Name: rec.Action, Description: rec.Summary}}
	}

	// Phase C: assemble response
	resp := GetStepDetailResponse{
		SystemPrompt:      systemPrompt,
		Tools:             toolDefsToWire(toolDefs),
		Step:              rec.Step,
		Messages:          messagesToWire(rec.Messages),
		MessageCount:      rec.MessageCount,
		TokenCount:        rec.TokenCount,
		RawResponse:       rec.RawResponse,
		Action:            rec.Action,
		Summary:           rec.Summary,
		ToolPath:          rec.ToolPath,
		ToolInput:         rec.ToolInput,
		ToolResult:        rec.ToolResult,
		ToolError:         rec.ToolError,
		ToolDurationMs:    float64(rec.ToolDuration.Microseconds()) / 1000.0,
		RequestTokens:     rec.RequestTokens,
		ResponseTokens:    rec.ResponseTokens,
		InputTokens:       rec.InputTokens,
		OutputTokens:      rec.OutputTokens,
		CachedInputTokens: rec.CachedInputTokens,
		ToolCalls:         toolCallRecordsToWire(rec.ToolCalls),
		Provider:          providerName,
		DriverType:        s.driverTypeForProvider(providerName),
		ContextWindow:     contextWindow,
	}

	payload, _ := json.Marshal(resp)
	writeResponse(conn, Response{OK: true, Payload: payload})
}

// hasToolCallError reports whether any tool call in a step record carries an error.
// Used to compute StepSummaryWire.HasError so a step whose last tool call succeeded
// but earlier calls failed is still surfaced in the Timeline. (Without this, a step
// that parallel-invokes Agent(failed) + Read(ok) would render as a clean step.)
func hasToolCallError(calls []types.ToolCallRecord) bool {
	for _, c := range calls {
		if c.Error != "" {
			return true
		}
	}
	return false
}

// handleListSteps returns step summaries for a process (Story 27.3).
func (s *Server) handleListSteps(conn net.Conn, rawPayload json.RawMessage) {
	var req ListStepsRequest
	if err := json.Unmarshal(rawPayload, &req); err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID", Message: "invalid list_steps request"}})
		return
	}

	uuid := req.UUID
	if uuid == "" && req.PID != 0 {
		if _, ok := s.kern.GetProcess(req.PID); !ok {
			if hist := s.kern.FindHistoryByPID(req.PID); hist != nil {
				uuid = hist.UUID
			}
		}
	}
	stepsPath := s.resolveStepsPath(req.PID, uuid)

	if stepsPath == "" {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "NOT_FOUND", Message: "process not found"}})
		return
	}

	records, total, parseErrors, err := kernel.ReadAllStepsWithErrors(stepsPath, req.AfterStep)
	if err != nil {
		// Story 72.1 AC3: the path was already resolved above, so a failure here
		// is a genuine read/parse problem — not "process not found".
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: string(types.ErrInternal), Message: fmt.Sprintf("read steps: %v", err)}})
		return
	}

	wires := make([]StepSummaryWire, len(records))
	for i, r := range records {
		wires[i] = StepSummaryWire{
			Step:        r.Step,
			Action:      r.Action,
			Summary:     r.Summary,
			ToolPath:    r.ToolPath,
			HasError:    r.ToolError != "" || hasToolCallError(r.ToolCalls),
			DurationMs:  float64(r.ToolDuration.Microseconds()) / 1000.0,
			TokenCount:  r.TokenCount,
			TimestampMs: r.Timestamp.Milliseconds(),
		}
	}

	respPayload, _ := json.Marshal(ListStepsResponse{Steps: wires, Total: total, ParseErrors: parseErrors})
	writeResponse(conn, Response{OK: true, Payload: respPayload})
}

// handleListEvents returns persisted syscall events from events.jsonl for a process.
func (s *Server) handleListEvents(conn net.Conn, rawPayload json.RawMessage) {
	var req ListEventsRequest
	if err := json.Unmarshal(rawPayload, &req); err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID", Message: "invalid list_events request"}})
		return
	}

	uuid := req.UUID
	if uuid == "" && req.PID != 0 {
		if proc, ok := s.kern.GetProcess(req.PID); ok {
			uuid = proc.UUID
		} else if hist := s.kern.FindHistoryByPID(req.PID); hist != nil {
			uuid = hist.UUID
		}
	}

	eventsPath := s.resolveEventsPath(uuid)
	if eventsPath == "" {
		writeResponse(conn, Response{OK: true, Payload: mustMarshal(ListEventsResponse{Events: []SyscallEventWire{}})})
		return
	}

	diskEvents, parseErrors, err := kernel.ReadAllEventsWithErrors(eventsPath)
	if err != nil {
		// Story 72.1 AC3/F3: the former behavior swallowed the error into an
		// OK=true empty list, so a read failure looked like "no syscall events".
		// The empty-path branch above (events file may legitimately not exist)
		// is untouched — only a real read failure is reported here.
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: string(types.ErrInternal), Message: fmt.Sprintf("read events: %v", err)}})
		return
	}

	wires := make([]SyscallEventWire, len(diskEvents))
	for i, d := range diskEvents {
		wires[i] = SyscallEventWire{
			TimestampMs: int64(d.TimestampMs),
			PID:         types.PID(d.PID),
			Syscall:     d.Syscall,
			Args:        d.Args,
			Result:      d.Result,
			Error:       d.Error,
			DurationMs:  d.DurationMs,
			TraceID:     d.TraceID,
			SpanID:      d.SpanID,
		}
	}

	respPayload, _ := json.Marshal(ListEventsResponse{Events: wires, ParseErrors: parseErrors})
	writeResponse(conn, Response{OK: true, Payload: respPayload})
}

// resolveEventsPath returns the path to events.jsonl for a UUID.
func (s *Server) resolveEventsPath(uuid string) string {
	if uuid == "" || !isValidUUID(uuid) {
		return ""
	}
	baseDir := kernel.FindBaseDirByUUID(s.kern.GetDataDir(), uuid)
	if baseDir == "" {
		return ""
	}
	path := filepath.Join(baseDir, "steps", uuid, "events.jsonl")
	if _, err := os.Stat(path); err != nil {
		return ""
	}
	return path
}

// handleGetRawCapture returns raw LLM request/response records from raw.jsonl
// for a process (Story 56.4 · CAP-3 单一数据后端). Mirrors handleListEvents:
// PID→UUID 解析（live GetProcess → FindHistoryByPID 回退）+ resolveRawPath +
// kernel 读 API。无文件 / 空 uuid → {Records: []}（OK=true 空列表，不报错）。
//
// Step==0 → ReadAllRawWithErrors（全部记录 + malformed 计数, AC#8）；
// Step>0 → ReadRawForStepWithErrors（仅该 step 一条 + 全文件 malformed 计数，
// 让单步查询路径也暴露 ParseErrors · 56.4 review decision 1→a）。读路径零反脱敏——
// 落盘已脱敏的凭据指纹原样返回（AC#5）。三路（strace / dashboard / 直接 IPC）共用
// 此唯一后端，天然一致（AC#4）。
func (s *Server) handleGetRawCapture(conn net.Conn, rawPayload json.RawMessage) {
	var req GetRawCaptureRequest
	if err := json.Unmarshal(rawPayload, &req); err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID", Message: "invalid get_raw_capture request"}})
		return
	}

	uuid := req.UUID
	if uuid == "" && req.PID != 0 {
		if proc, ok := s.kern.GetProcess(req.PID); ok {
			uuid = proc.UUID
		} else if hist := s.kern.FindHistoryByPID(req.PID); hist != nil {
			uuid = hist.UUID
		}
	}

	rawPath := s.resolveRawPath(uuid)
	if rawPath == "" {
		writeResponse(conn, Response{OK: true, Payload: mustMarshal(GetRawCaptureResponse{Records: []vfs.RawCapture{}})})
		return
	}

	if req.Step > 0 {
		rec, parseErrors, err := kernel.ReadRawForStepWithErrors(rawPath, req.Step)
		if err != nil || rec == nil {
			writeResponse(conn, Response{OK: true, Payload: mustMarshal(GetRawCaptureResponse{Records: []vfs.RawCapture{}, ParseErrors: parseErrors})})
			return
		}
		writeResponse(conn, Response{OK: true, Payload: mustMarshal(GetRawCaptureResponse{Records: []vfs.RawCapture{*rec}, ParseErrors: parseErrors})})
		return
	}

	records, parseErrors, err := kernel.ReadAllRawWithErrors(rawPath)
	if err != nil {
		writeResponse(conn, Response{OK: true, Payload: mustMarshal(GetRawCaptureResponse{Records: []vfs.RawCapture{}})})
		return
	}
	if records == nil {
		records = []vfs.RawCapture{}
	}
	writeResponse(conn, Response{OK: true, Payload: mustMarshal(GetRawCaptureResponse{Records: records, ParseErrors: parseErrors})})
}

// resolveRawPath returns the path to raw.jsonl for a UUID (Story 56.4), mirroring
// resolveEventsPath. Empty uuid / invalid uuid / missing baseDir / missing file
// all return "" so handleGetRawCapture yields an OK empty list.
func (s *Server) resolveRawPath(uuid string) string {
	if uuid == "" || !isValidUUID(uuid) {
		return ""
	}
	baseDir := kernel.FindBaseDirByUUID(s.kern.GetDataDir(), uuid)
	if baseDir == "" {
		return ""
	}
	path := filepath.Join(baseDir, "steps", uuid, "raw.jsonl")
	if _, err := os.Stat(path); err != nil {
		return ""
	}
	return path
}

// handleCompact manually triggers context compaction for a running process (Story 31.2).
func (s *Server) handleCompact(conn net.Conn, rawPayload json.RawMessage) {
	var req CompactRequest
	if err := json.Unmarshal(rawPayload, &req); err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID", Message: "invalid compact request"}})
		return
	}

	proc, ok := s.kern.GetProcess(req.PID)
	if !ok {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: string(types.ErrNotFound), Message: "process not found"}})
		return
	}

	state := proc.GetState()
	if state != types.StateRunning {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: string(types.ErrInternal), Message: fmt.Sprintf("process not running (state=%d)", state)}})
		return
	}

	// Prevent concurrent compact (auto + manual IPC)
	if !proc.TryLockCompact() {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: string(types.ErrInternal), Message: "compact already in progress"}})
		return
	}
	defer proc.UnlockCompact()

	opts := rnixctx.CompactOpts{
		LLMCall:            s.kern.BuildCompactLLMCall(proc),
		Trigger:            "manual",
		CustomInstructions: req.CustomInstructions,
		ReadFileState:      s.kern.SnapshotReadFileState(proc),
		ActiveSkills:       s.kern.BuildActiveSkills(proc),
		ActivePlan:         s.kern.ExtractActivePlan(proc.CtxID),
	}

	result, err := s.ctxMgr.Compact(proc.CtxID, opts)
	if err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: string(types.ErrInternal), Message: fmt.Sprintf("compact failed: %v", err)}})
		return
	}

	// Clear ReadFileState after successful compact
	s.kern.ClearReadFileState(proc)

	// Story 71.4 AC3 — a successful manual compaction proves the operation works
	// again, so clear the latch. The manual path bypasses the latch (it never
	// goes through autoCompactIfNeeded), but leaving it set afterwards would keep
	// automatic compaction permanently disabled — welding shut the escape hatch
	// the operator just used. Mirrors qwen's startChat() reset, which the manual
	// /compress success path also travels through.
	s.kern.ClearCompactLatch(proc)

	compactResp := CompactResponse{
		PreTokens:  result.PreTokens,
		PostTokens: result.PostTokens,
		Restored:   result.ItemsRestored,
	}
	compactPayload, _ := json.Marshal(compactResp)
	writeResponse(conn, Response{OK: true, Payload: compactPayload})
}

// handleGetResumeLineage implements MethodGetResumeLineage (Story 42.3).
//
// Independent of handleLineage — the latter returns stem-cell skill
// differentiation events (Epic 20). This handler returns the cross-process fork
// graph anchored by OriginUUID.
func (s *Server) handleGetResumeLineage(conn net.Conn, rawPayload json.RawMessage) {
	var req GetResumeLineageRequest
	if err := json.Unmarshal(rawPayload, &req); err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID", Message: "invalid get_resume_lineage request"}})
		return
	}
	if req.UUID == "" {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID", Message: "uuid is required"}})
		return
	}
	if s.kern == nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INTERNAL", Message: "kernel not attached"}})
		return
	}
	data, err := s.kern.GetResumeLineage(req.UUID)
	if err != nil {
		code := "NOT_FOUND"
		var sysErr *kernel.SyscallError
		if errors.As(err, &sysErr) {
			code = string(sysErr.Code)
		}
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: code, Message: err.Error()}})
		return
	}

	resp := GetResumeLineageResponse{
		Current:         procInfoToLineageNode(data.Current),
		OriginUUID:      data.Current.OriginUUID,
		ResumedFromStep: data.Current.ResumedFromStep,
		Ancestors:       procInfosToLineageNodes(data.Ancestors),
		Descendants:     procInfosToLineageNodes(data.Descendants),
		Truncated:       data.Truncated,
	}
	payload, _ := json.Marshal(resp)
	writeResponse(conn, Response{OK: true, Payload: payload})
}

// procInfoToLineageNode maps a kernel/vfs ProcInfo snapshot to the wire-format
// ResumeLineageNode used by the Dashboard Detail Lineage section.
func procInfoToLineageNode(p vfs.ProcInfo) ResumeLineageNode {
	node := ResumeLineageNode{
		UUID:            p.UUID,
		OriginUUID:      p.OriginUUID,
		ResumedFromStep: p.ResumedFromStep,
		State:           p.State.String(),
		Intent:          p.Intent,
	}
	if !p.CreatedAt.IsZero() {
		node.CreatedAtMs = p.CreatedAt.UnixMilli()
	}
	return node
}

func procInfosToLineageNodes(items []vfs.ProcInfo) []ResumeLineageNode {
	if len(items) == 0 {
		return []ResumeLineageNode{}
	}
	out := make([]ResumeLineageNode, len(items))
	for i, p := range items {
		out[i] = procInfoToLineageNode(p)
	}
	return out
}
