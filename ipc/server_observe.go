package ipc

import (
	"context"
	"encoding/json"
	"fmt"
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
	baseDir := s.kern.GetStepDataDir()
	if baseDir == "" {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "NOT_FOUND", Message: "no step data dir"}})
		return
	}
	path := filepath.Join(baseDir, "data", "steps", uuid, "ctx-profile.json")
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

	// Phase A: resolve SystemPrompt + ToolDefs
	var systemPrompt string
	var toolDefs []vfs.ToolDef
	var stepsPath string

	proc, procFound := s.resolveProcess(req.PID, req.UUID)
	if procFound {
		systemPrompt = proc.GetFinalSystemPrompt()
		toolDefs = proc.GetNativeToolDefs()
		stepsPath = s.resolveStepsPathFromProc(proc)
	} else {
		// Process not in memory — try UUID from request, then fall back to process history
		uuid := req.UUID
		if uuid == "" && req.PID != 0 {
			if hist := s.kern.FindHistoryByPID(req.PID); hist != nil {
				uuid = hist.UUID
			}
		}
		stepsPath = s.resolveStepsPath(req.PID, uuid)
		if stepsPath == "" {
			writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "NOT_FOUND", Message: "process not found"}})
			return
		}
		metaPath := filepath.Join(filepath.Dir(stepsPath), "process-meta.json")
		meta, err := readProcessMeta(metaPath)
		if err != nil {
			writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "NOT_FOUND", Message: "process not found"}})
			return
		}
		systemPrompt = meta.SystemPrompt
		toolDefs = meta.ToolDefs
	}

	if stepsPath == "" {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "NOT_FOUND", Message: "process not found"}})
		return
	}

	// Phase B: read StepRecord
	rec, err := kernel.ReadStep(stepsPath, req.Step)
	if err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "not_found", Message: fmt.Sprintf("step %d not yet recorded", req.Step)}})
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
	}

	payload, _ := json.Marshal(resp)
	writeResponse(conn, Response{OK: true, Payload: payload})
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

	records, total, err := kernel.ReadAllSteps(stepsPath, req.AfterStep)
	if err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "NOT_FOUND", Message: "process not found"}})
		return
	}

	wires := make([]StepSummaryWire, len(records))
	for i, r := range records {
		wires[i] = StepSummaryWire{
			Step:        r.Step,
			Action:      r.Action,
			Summary:     r.Summary,
			ToolPath:    r.ToolPath,
			HasError:    r.ToolError != "",
			DurationMs:  float64(r.ToolDuration.Microseconds()) / 1000.0,
			TokenCount:  r.TokenCount,
			TimestampMs: r.Timestamp.Milliseconds(),
		}
	}

	respPayload, _ := json.Marshal(ListStepsResponse{Steps: wires, Total: total})
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

	diskEvents, err := kernel.ReadAllEvents(eventsPath)
	if err != nil {
		writeResponse(conn, Response{OK: true, Payload: mustMarshal(ListEventsResponse{Events: []SyscallEventWire{}})})
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

	respPayload, _ := json.Marshal(ListEventsResponse{Events: wires})
	writeResponse(conn, Response{OK: true, Payload: respPayload})
}

// resolveEventsPath returns the path to events.jsonl for a UUID.
func (s *Server) resolveEventsPath(uuid string) string {
	if uuid == "" || !isValidUUID(uuid) {
		return ""
	}
	baseDir := s.kern.GetStepDataDir()
	if baseDir == "" {
		return ""
	}
	path := filepath.Join(baseDir, "data", "steps", uuid, "events.jsonl")
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

	compactResp := CompactResponse{
		PreTokens:  result.PreTokens,
		PostTokens: result.PostTokens,
		Restored:   result.ItemsRestored,
	}
	compactPayload, _ := json.Marshal(compactResp)
	writeResponse(conn, Response{OK: true, Payload: compactPayload})
}
