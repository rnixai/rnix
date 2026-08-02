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
	"github.com/rnixai/rnix/internal/jsonl"
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
	// Story 72.2 F10: try idx offset + direct read first (O(line)), fall back
	// to sequential scan (O(file)) when idx is unavailable.
	var rec *types.StepRecord
	var readErr error
	idxPath := kernel.IdxPathForJSONL(stepsPath)
	if offset, idxErr := kernel.ReadStepOffsetFromIdx(idxPath, stepsPath, req.Step); idxErr == nil && offset >= 0 {
		rec, readErr = kernel.ReadStepAtOffset(stepsPath, offset)
		// P2: the idx offset may be stale (jsonl truncated, concurrent rebuild,
		// or blank-line drift — P6) and land on a DIFFERENT valid record. A
		// successful read of the wrong step is worse than a read error: it would
		// silently return another step's details under this step's number.
		// Verify the record is the one requested; if not, fall back to the
		// authoritative sequential scan.
		if readErr != nil || rec == nil || rec.Step != req.Step {
			rec, readErr = kernel.ReadStep(stepsPath, req.Step)
		}
	} else {
		rec, readErr = kernel.ReadStep(stepsPath, req.Step)
	}
	if readErr != nil {
		if errors.Is(readErr, kernel.ErrStepNotFound) {
			// The step genuinely does not exist — preserve the original wording.
			writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "not_found", Message: fmt.Sprintf("step %d not yet recorded", req.Step)}})
			return
		}
		// Story 72.1 AC3: a real read failure must not be reported as
		// "step not yet recorded" — surface it as an internal error.
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: string(types.ErrInternal), Message: fmt.Sprintf("read step %d: %v", req.Step, readErr)}})
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
//
// Story 72.2 AC2/AC6: serves from the idx cache when available (O(1) hit /
// O(delta) incremental), falling back to full jsonl scan with cache backfill.
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

	// P1/AC5 + P4: whether the owning process can still append to the jsonl. A
	// process present in the kernel process table is alive — Dead/reaped
	// processes are removed from the table and only reachable via FindHistory*.
	// This drives both the idx staleness check (a dead process's lagging idx is
	// stale, AC5 row 3) and the rebuild gate (never rename under a live writer).
	procAlive := false
	if req.PID != 0 {
		_, procAlive = s.kern.GetProcess(req.PID)
	}
	if !procAlive && uuid != "" {
		_, procAlive = s.kern.GetProcessByUUID(uuid)
	}

	// Try idx cache path (AC6).
	entries, total, parseErrors, cached := s.listStepsViaCache(stepsPath, procAlive)
	if !cached {
		// Fallback: full scan (existing path, verbatim preserved).
		// Scan with afterStep=0 so the backfilled cache holds the full view.
		records, t, pe, err := kernel.ReadAllStepsWithErrors(stepsPath, 0)
		if err != nil {
			// Story 72.1 AC3: the path was already resolved above, so a failure here
			// is a genuine read/parse problem — not "process not found".
			writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: string(types.ErrInternal), Message: fmt.Sprintf("read steps: %v", err)}})
			return
		}
		total = t
		parseErrors = pe
		entries = make([]kernel.StepIdxEntry, len(records))
		for i, r := range records {
			entries[i] = kernel.StepIdxEntry{
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
		// F3: backfill memory cache so the next tick is O(1).
		s.backfillIdxCache(stepsPath, entries, total, parseErrors)
		// Background disk idx rebuild (AC4). Skipped for live processes (P4):
		// rename would orphan the StepWriter's append fd.
		go kernel.RebuildIdx(stepsPath, procAlive)
	}

	// Apply afterStep filter on the dedup view.
	if req.AfterStep > 0 {
		filtered := entries[:0]
		for _, e := range entries {
			if e.Step > req.AfterStep {
				filtered = append(filtered, e)
			}
		}
		entries = filtered
	}

	// Apply Offset/Limit pagination (AC1).
	if req.Limit > 0 {
		if req.Offset > 0 {
			if req.Offset < len(entries) {
				entries = entries[req.Offset:]
			} else {
				entries = nil
			}
		}
		if len(entries) > req.Limit {
			entries = entries[:req.Limit]
		}
	}

	wires := make([]StepSummaryWire, len(entries))
	for i, e := range entries {
		wires[i] = StepSummaryWire{
			Step:        e.Step,
			Action:      e.Action,
			Summary:     e.Summary,
			ToolPath:    e.ToolPath,
			HasError:    e.HasError,
			DurationMs:  e.DurationMs,
			TokenCount:  e.TokenCount,
			TimestampMs: e.TimestampMs,
		}
	}

	respPayload, _ := json.Marshal(ListStepsResponse{Steps: wires, Total: total, ParseErrors: parseErrors})
	writeResponse(conn, Response{OK: true, Payload: respPayload})
}

// listStepsViaCache tries to serve the full dedup step view from the idx cache.
// Returns entries (unfiltered by afterStep), total, parseErrors, and whether
// the cache path succeeded.
//
// procAlive (P1/AC5, P3, P4) reports whether the owning process can still append
// to the jsonl; it gates the staleness check, the partial-line watermark
// rollback, and is passed through to ReadStepsFromIdx.
func (s *Server) listStepsViaCache(stepsPath string, procAlive bool) ([]kernel.StepIdxEntry, int, int, bool) {
	idxPath := kernel.IdxPathForJSONL(stepsPath)

	s.idxMu.Lock()
	defer s.idxMu.Unlock()

	cs, hit := s.idxCache[stepsPath]
	if hit {
		cs.lastAccess = s.nextIdxClockLocked() // P10: refresh LRU stamp
		if cs.idxSize >= 0 {
			// Disk idx mode — check for growth.
			fi, err := os.Stat(idxPath)
			if err != nil {
				delete(s.idxCache, stepsPath) // idx deleted — invalidate
				return nil, 0, 0, false
			}
			if fi.Size() == cs.idxSize {
				// O(1) hit.
				return s.cacheToEntries(cs), cs.total, cs.parseErrors, true
			}
			if fi.Size() > cs.idxSize {
				// Incremental: read only appended idx lines.
				consumed, mErr := s.mergeIdxAppend(cs, idxPath, cs.idxSize, procAlive)
				if mErr != nil {
					// P3: torn/IO read — the merge is incomplete; do not advance
					// the watermark past unread bytes. Invalidate and fall back
					// to a full scan.
					delete(s.idxCache, stepsPath)
					return nil, 0, 0, false
				}
				cs.idxSize += consumed // advance ONLY by bytes actually consumed
				return s.cacheToEntries(cs), cs.total, cs.parseErrors, true
			}
			// idx shrank (rebuilt) — invalidate and re-read.
			delete(s.idxCache, stepsPath)
		} else {
			// Fallback backfill mode (no disk idx when this entry was cached).
			// P11: a background RebuildIdx may have since written the disk idx
			// (dead processes only — live processes skip rebuild, P4). If so,
			// adopt it: drop this fallback entry and re-read the authoritative
			// disk idx below instead of staying in the slower jsonl-merge mode.
			if fi, statErr := os.Stat(idxPath); statErr == nil && fi.Size() > 0 {
				delete(s.idxCache, stepsPath)
				// fall through to the disk-idx read below
			} else {
				// No disk idx yet — track jsonl growth.
				fi, err := os.Stat(stepsPath)
				if err != nil {
					delete(s.idxCache, stepsPath)
					return nil, 0, 0, false
				}
				if fi.Size() == cs.jsonlSize {
					return s.cacheToEntries(cs), cs.total, cs.parseErrors, true
				}
				if fi.Size() > cs.jsonlSize {
					consumed, mErr := s.mergeJSONLAppend(cs, stepsPath, cs.jsonlSize, procAlive)
					if mErr != nil {
						delete(s.idxCache, stepsPath) // P3: torn read — full scan
						return nil, 0, 0, false
					}
					cs.jsonlSize += consumed
					return s.cacheToEntries(cs), cs.total, cs.parseErrors, true
				}
				delete(s.idxCache, stepsPath) // jsonl shrank — re-read
			}
		}
	}

	// Cache miss (or invalidated above) — try reading the disk idx.
	idxEntries, total, parseErrors, err := kernel.ReadStepsFromIdx(idxPath, stepsPath, 0, procAlive)
	if err == nil {
		fi, _ := os.Stat(idxPath)
		idxSize := int64(0)
		if fi != nil {
			idxSize = fi.Size()
		}
		cs = &idxCacheState{
			last:        make(map[int]kernel.StepIdxEntry, len(idxEntries)),
			order:       make([]int, 0, len(idxEntries)),
			total:       total,
			parseErrors: parseErrors,
			idxSize:     idxSize,
			lastAccess:  s.nextIdxClockLocked(),
		}
		for _, e := range idxEntries {
			if _, seen := cs.last[e.Step]; !seen {
				cs.order = append(cs.order, e.Step)
			}
			cs.last[e.Step] = e
		}
		s.evictIdxCacheLocked()
		s.idxCache[stepsPath] = cs
		return s.cacheToEntries(cs), cs.total, cs.parseErrors, true
	}

	return nil, 0, 0, false
}

// cacheToEntries builds the ordered dedup entry slice from cache state.
func (s *Server) cacheToEntries(cs *idxCacheState) []kernel.StepIdxEntry {
	entries := make([]kernel.StepIdxEntry, 0, len(cs.order))
	for _, step := range cs.order {
		entries = append(entries, cs.last[step])
	}
	return entries
}

// mergeIdxAppend reads idx lines appended after fromSize and merges into cache.
//
// Story 72.2 P3: returns (consumed, err). consumed is the number of bytes that
// formed COMPLETE lines and were actually merged; the caller advances the
// watermark by exactly this much — never by the file's current size. If the
// writer is mid-flush, the trailing partial line is left unconsumed and is
// re-read on the next tick once it is complete. A non-EOF I/O error (torn read)
// returns err so the caller can invalidate rather than silently skip a window.
func (s *Server) mergeIdxAppend(cs *idxCacheState, idxPath string, fromSize int64, procAlive bool) (int64, error) {
	f, err := os.Open(idxPath)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	if _, err := f.Seek(fromSize, 0); err != nil {
		return 0, err
	}
	consumed := int64(0)
	scanErr := jsonl.Scan(f, idxPath, func(line []byte) error {
		// jsonl.Scan only delivers COMPLETE lines (terminated by '\n', or the
		// final EOF fragment). A complete line's bytes are consumed regardless
		// of whether it parses — a malformed complete line is skipped (counted
		// as a parse error) but its bytes must not be re-read forever.
		consumed += int64(len(line))
		var raw struct {
			O  int64   `json:"o"`
			S  int     `json:"s"`
			A  string  `json:"a"`
			M  string  `json:"m"`
			T  string  `json:"t"`
			E  bool    `json:"e"`
			D  float64 `json:"d"`
			K  int     `json:"k"`
			TS int64   `json:"ts"`
		}
		if err := json.Unmarshal(line, &raw); err != nil {
			cs.parseErrors++ // P8: surface malformed idx lines
			return nil
		}
		entry := kernel.StepIdxEntry{
			Offset: raw.O, Step: raw.S, Action: raw.A, Summary: raw.M,
			ToolPath: raw.T, HasError: raw.E, DurationMs: raw.D,
			TokenCount: raw.K, TimestampMs: raw.TS,
		}
		cs.total++
		if _, seen := cs.last[entry.Step]; !seen {
			cs.order = append(cs.order, entry.Step)
		}
		cs.last[entry.Step] = entry
		return nil
	})
	if scanErr != nil {
		return 0, scanErr // torn read — caller invalidates
	}
	_ = procAlive // reserved for future liveness-aware merge policy
	return consumed, nil
}

// mergeJSONLAppend reads jsonl lines appended after fromSize and merges into
// cache (for fallback-backfilled caches with no disk idx). See mergeIdxAppend
// for the P3 consumed-bytes / error contract.
func (s *Server) mergeJSONLAppend(cs *idxCacheState, jsonlPath string, fromSize int64, procAlive bool) (int64, error) {
	f, err := os.Open(jsonlPath)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	if _, err := f.Seek(fromSize, 0); err != nil {
		return 0, err
	}
	// offset tracks the absolute jsonl byte position of the current line so each
	// parsed entry carries a correct StepIdxEntry.Offset (for get_step_detail).
	// consumed tracks only complete-line bytes for the watermark (P3).
	offset := fromSize
	consumed := int64(0)
	scanErr := jsonl.Scan(f, jsonlPath, func(line []byte) error {
		consumed += int64(len(line))
		entry, err := kernel.ParseIdxEntryFromJSONL(line, offset)
		offset += int64(len(line)) // line includes trailing '\n'
		if err != nil {
			cs.parseErrors++ // P8
			return nil
		}
		cs.total++
		if _, seen := cs.last[entry.Step]; !seen {
			cs.order = append(cs.order, entry.Step)
		}
		cs.last[entry.Step] = entry
		return nil
	})
	if scanErr != nil {
		return 0, scanErr
	}
	_ = procAlive
	return consumed, nil
}

// backfillIdxCache populates the cache from a full-scan result (F3).
// idxSize = -1 marks this as a fallback backfill with no disk idx.
//
// Story 72.2 P5: jsonlSize is set from a stat taken AFTER the full scan, so it
// can be ahead of the scanned data if the writer appended mid-scan. This is a
// deliberate, bounded trade-off:
//   - DEAD process (the case this story optimizes): the jsonl is immutable, so
//     the stat and the scan agree exactly — no window exists.
//   - LIVE process: a mid-scan append lands in the (scanEnd, statSize] gap and
//     is skipped by the incremental merge. The gap is bounded by one scan's
//     duration and self-heals — the next RebuildIdx (after the process dies)
//     regenerates the idx, and P11 adopts it. Chasing the alternative (stat
//     before scan) instead double-counts the same window into `total`, breaking
//     the F6 invariant that Total equals the file's record count.
func (s *Server) backfillIdxCache(stepsPath string, entries []kernel.StepIdxEntry, total, parseErrors int) {
	s.idxMu.Lock()
	defer s.idxMu.Unlock()

	if _, exists := s.idxCache[stepsPath]; exists {
		return // already cached (race between concurrent requests)
	}

	fi, _ := os.Stat(stepsPath)
	jsonlSize := int64(0)
	if fi != nil {
		jsonlSize = fi.Size()
	}

	cs := &idxCacheState{
		last:        make(map[int]kernel.StepIdxEntry, len(entries)),
		order:       make([]int, 0, len(entries)),
		total:       total,
		parseErrors: parseErrors,
		idxSize:     -1,
		jsonlSize:   jsonlSize,
		lastAccess:  s.nextIdxClockLocked(),
	}
	for _, e := range entries {
		if _, seen := cs.last[e.Step]; !seen {
			cs.order = append(cs.order, e.Step)
		}
		cs.last[e.Step] = e
	}
	s.evictIdxCacheLocked()
	s.idxCache[stepsPath] = cs
}

// nextIdxClockLocked returns a monotonically increasing access stamp (P10).
// Must be called with idxMu held. int64 will not wrap in any daemon lifetime.
func (s *Server) nextIdxClockLocked() int64 {
	s.idxClock++
	return s.idxClock
}

// evictIdxCacheLocked evicts the least-recently-accessed entry when over
// capacity (cap 8). Must be called with idxMu held.
//
// Story 72.2 P10: the old implementation deleted an ARBITRARY entry (Go map
// iteration order is randomized), so a hot process could be evicted by a cold
// one and re-trigger a full scan + rebuild on its very next tick — the exact
// steady-state thrash this cache exists to prevent. Evict the oldest lastAccess
// instead, so hot entries survive.
func (s *Server) evictIdxCacheLocked() {
	for len(s.idxCache) >= 8 {
		var oldestKey string
		var oldestStamp int64 = -1
		for k, v := range s.idxCache {
			if oldestStamp < 0 || v.lastAccess < oldestStamp {
				oldestStamp = v.lastAccess
				oldestKey = k
			}
		}
		if oldestKey == "" {
			break
		}
		delete(s.idxCache, oldestKey)
	}
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

	// Story 72.2 F5: events pagination = full-scan + slice (not idx).
	total := len(wires)
	if req.Limit > 0 {
		if req.Offset > 0 {
			if req.Offset < len(wires) {
				wires = wires[req.Offset:]
			} else {
				wires = nil
			}
		}
		if len(wires) > req.Limit {
			wires = wires[:req.Limit]
		}
	}

	respPayload, _ := json.Marshal(ListEventsResponse{Events: wires, ParseErrors: parseErrors, Total: total})
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

	// Story 72.2 F5: raw pagination = full-scan + slice (not idx).
	// Step>0 path above takes priority and never reaches here.
	total := len(records)
	if req.Limit > 0 {
		if req.Offset > 0 {
			if req.Offset < len(records) {
				records = records[req.Offset:]
			} else {
				records = nil
			}
		}
		if len(records) > req.Limit {
			records = records[:req.Limit]
		}
	}

	writeResponse(conn, Response{OK: true, Payload: mustMarshal(GetRawCaptureResponse{Records: records, ParseErrors: parseErrors, Total: total})})
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
