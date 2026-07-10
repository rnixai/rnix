package ipc

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"

	"github.com/rnixai/rnix/internal/config"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/kernel"
	"github.com/rnixai/rnix/vfs"
)

func (s *Server) handleListProcs(conn net.Conn) {
	procs := s.kern.ListProcs()
	wireProcs := make([]ProcInfoWire, len(procs))
	for i, p := range procs {
		wireProcs[i] = ProcInfoToWire(p)
	}
	payload, _ := json.Marshal(ListProcsResponse{Processes: wireProcs})
	writeResponse(conn, Response{OK: true, Payload: payload})
}

// handleListAllProcs serves MethodListAllProcs. Story 34.8: it now accepts an
// optional ListAllProcsRequest{Offset, Limit} payload to page through the
// (potentially unbounded) historical process set so a single wire response
// stays well under the IPC scanner buffer.
//
// Backward compatibility (AC1): a nil/empty/unparseable payload, or
// Offset==0 && Limit==0 (Limit<=0 in general), returns the FULL set with no
// Total/HasMore metadata — exactly the legacy behavior the 5 full-fetch callers
// (ps -a / resume / apply / compose_resume) rely on.
//
// Pagination direction is "most-recent-first" (AC2): kernel ListAllProcs()
// sorts ascending by CreatedAt (oldest first), but the dashboard cares about
// the newest processes. Offset=0 returns the newest Limit entries (counting
// back from the tail); increasing Offset reaches progressively older batches.
func (s *Server) handleListAllProcs(conn net.Conn, rawPayload json.RawMessage) {
	procs := s.kern.ListAllProcs()
	// Diagnostic dump for tree-structure debugging — gated by RNIX_DEBUG_TREE=1
	// to avoid spamming stderr on the ~1Hz dashboard tick. Each line: PID /
	// UUID[:8] / PPID / ParentUUID[:8] / State / IsPaused. Use this when the
	// user reports "pause+resume broke the tree" to pinpoint whether parent/
	// child relationship is preserved at the data layer.
	if os.Getenv("RNIX_DEBUG_TREE") == "1" {
		log.Printf("[list_all] dump (%d procs):", len(procs))
		for _, p := range procs {
			log.Printf("[list_all]   pid=%d uuid=%s ppid=%d parent_uuid=%s state=%s paused=%v",
				p.PID, shortUUID(p.UUID), p.PPID, shortUUID(p.ParentUUID), p.State, p.IsPaused)
		}
	}

	total := len(procs)

	// Parse the optional pagination payload. Lenient by design: any decode
	// failure or zero-value request falls back to the full set (no metadata),
	// mirroring the fail-open style of the other handlers.
	var req ListAllProcsRequest
	if len(rawPayload) > 0 {
		_ = json.Unmarshal(rawPayload, &req)
	}

	if req.Limit <= 0 {
		// Full set, no pagination metadata (backward compatible).
		writeResponse(conn, Response{OK: true, Payload: marshalProcList(procs, 0, false, false)})
		return
	}

	// Most-recent-first slicing over the CreatedAt-ascending result: treat the
	// tail as page 0. Offset counts newest entries to skip; Limit is the page
	// size. The returned page is itself ordered newest-first.
	page, hasMore := pageMostRecentFirst(procs, req.Offset, req.Limit)
	writeResponse(conn, Response{OK: true, Payload: marshalProcList(page, total, hasMore, true)})
}

// pageMostRecentFirst returns one "most-recent-first" page of a CreatedAt-ascending
// slice (oldest first). Offset is the number of newest entries to skip and limit
// is the page size; the returned page is ordered newest-first. An out-of-range
// offset yields an empty page with hasMore=false (never panics).
func pageMostRecentFirst(procs []vfs.ProcInfo, offset, limit int) (page []vfs.ProcInfo, hasMore bool) {
	n := len(procs)
	if offset < 0 {
		offset = 0
	}
	if offset >= n {
		return []vfs.ProcInfo{}, false
	}
	// Walk from the newest (tail) backwards: index n-1-offset down to
	// n-offset-limit (clamped at 0).
	end := n - offset          // exclusive upper bound in ascending slice
	start := max(end-limit, 0) // inclusive lower bound
	page = make([]vfs.ProcInfo, 0, end-start)
	for i := end - 1; i >= start; i-- {
		page = append(page, procs[i])
	}
	hasMore = start > 0
	return page, hasMore
}

// marshalProcList builds the ListProcsResponse wire payload. When withMeta is
// true the Total/HasMore pagination fields are populated (Story 34.8); when
// false they are omitted entirely (omitempty) so legacy full-fetch responses
// are byte-for-byte unchanged.
func marshalProcList(procs []vfs.ProcInfo, total int, hasMore, withMeta bool) json.RawMessage {
	wireProcs := make([]ProcInfoWire, len(procs))
	for i, p := range procs {
		wireProcs[i] = ProcInfoToWire(p)
	}
	resp := ListProcsResponse{Processes: wireProcs}
	if withMeta {
		resp.Total = total
		resp.HasMore = hasMore
	}
	payload, _ := json.Marshal(resp)
	return payload
}

// shortUUID 返回 UUID 的诊断用短串：前 8 hex（时间戳高位）+ ".." + 后 4 hex
// （随机熵尾，区分 UUIDv7 同分钟生成的不同 UUID）。空串显示为 "-"。
func shortUUID(u string) string {
	if u == "" {
		return "-"
	}
	if len(u) <= 13 {
		return u
	}
	return u[:8] + ".." + u[len(u)-4:]
}

func (s *Server) handleKill(conn net.Conn, rawPayload json.RawMessage) {
	var req KillRequest
	if err := json.Unmarshal(rawPayload, &req); err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID", Message: "invalid kill request"}})
		return
	}
	if req.Signal == 0 {
		req.Signal = types.SIGTERM
	}

	// Story 66.3: log every well-formed kill request, before resolvePID —
	// "who tried to kill a nonexistent PID" is audit information too. Origin
	// falls back to unknown for pre-66.3 clients that omit it.
	origin := killOriginFromWire(req.Origin)
	// %q on the wire-controlled string fields (uuid/origin/requester): quote and
	// escape so a crafted client cannot inject a forged "[ipc] kill request:"
	// line via an embedded newline (Story 66.3 review F2a).
	log.Printf("[ipc] kill request: pid=%d uuid=%q signal=%s origin=%q requester=%q",
		req.PID, req.UUID, req.Signal, origin.String(), req.Requester)

	pid, pidOK := s.resolvePID(req.PID, req.UUID)
	if !pidOK {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "NOT_FOUND", Message: "process not found"}})
		return
	}

	attr := kernel.KillAttribution{Origin: origin, Requester: req.Requester}
	if err := s.kern.KillWithOrigin(pid, req.Signal, attr); err != nil {
		code := "INTERNAL"
		var sysErr *kernel.SyscallError
		if errors.As(err, &sysErr) {
			code = string(sysErr.Code)
		}
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: code, Message: err.Error()}})
		return
	}
	writeResponse(conn, Response{OK: true})
}

// killOriginFromWire maps an optional wire origin string to a KillOrigin,
// defaulting to unknown for empty (legacy client / omitted field). The value
// is passed through verbatim — no whitelist (Story 66.3 open-string design).
func killOriginFromWire(s string) types.KillOrigin {
	if s == "" {
		return types.KillOriginUnknown
	}
	return types.KillOrigin(s)
}

func (s *Server) handleSignalTree(conn net.Conn, rawPayload json.RawMessage) {
	var req SignalTreeRequest
	if err := json.Unmarshal(rawPayload, &req); err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID", Message: "invalid signal_tree request"}})
		return
	}
	if req.Signal == 0 {
		req.Signal = types.SIGPAUSE
	}

	origin := killOriginFromWire(req.Origin)
	// %q on wire-controlled fields to prevent forged log lines (Story 66.3 F2a).
	log.Printf("[ipc] signal_tree request: pid=%d uuid=%q signal=%s origin=%q requester=%q",
		req.PID, req.UUID, req.Signal, origin.String(), req.Requester)

	pid, pidOK := s.resolvePID(req.PID, req.UUID)
	if !pidOK {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "NOT_FOUND", Message: "process not found"}})
		return
	}

	attr := kernel.KillAttribution{Origin: origin, Requester: req.Requester}
	affected, err := s.kern.SignalTreeWithOrigin(pid, req.Signal, attr)
	if err != nil {
		code := "INTERNAL"
		var sysErr *kernel.SyscallError
		if errors.As(err, &sysErr) {
			code = string(sysErr.Code)
		}
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: code, Message: err.Error()}})
		return
	}
	payload, _ := json.Marshal(SignalTreeResponse{Affected: affected})
	writeResponse(conn, Response{OK: true, Payload: payload})
}

// handlePauseSubtree (Story 44.4 AC#1) suspends the target process and its
// living descendants via kernel.SuspendSubtree. dashboard `p` routes here.
func (s *Server) handlePauseSubtree(conn net.Conn, rawPayload json.RawMessage) {
	var req PauseSubtreeRequest
	if err := json.Unmarshal(rawPayload, &req); err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID", Message: "invalid pause_subtree request"}})
		return
	}

	affected, err := s.kern.SuspendSubtree(req.PID)
	if err != nil {
		code := "INTERNAL"
		var sysErr *kernel.SyscallError
		if errors.As(err, &sysErr) {
			code = string(sysErr.Code)
		}
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: code, Message: err.Error()}})
		return
	}
	payload, _ := json.Marshal(PauseSubtreeResponse{Affected: affected})
	writeResponse(conn, Response{OK: true, Payload: payload})
}

// handleResumeSubtree (Story 44.4 AC#1) resumes every Suspended node in the
// target subtree via kernel.ResumeSubtree, skipping Dead/Failed/Zombie nodes.
// dashboard `r` on a Suspended process routes here (Decker bug core fix).
func (s *Server) handleResumeSubtree(conn net.Conn, rawPayload json.RawMessage) {
	var req ResumeSubtreeRequest
	if err := json.Unmarshal(rawPayload, &req); err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID", Message: "invalid resume_subtree request"}})
		return
	}

	affected, skipped, err := s.kern.ResumeSubtree(req.PID)
	if err != nil {
		code := "INTERNAL"
		var sysErr *kernel.SyscallError
		if errors.As(err, &sysErr) {
			code = string(sysErr.Code)
		}
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: code, Message: err.Error()}})
		return
	}
	payload, _ := json.Marshal(ResumeSubtreeResponse{Affected: affected, Skipped: skipped})
	writeResponse(conn, Response{OK: true, Payload: payload})
}

func (s *Server) handleSuspend(conn net.Conn, rawPayload json.RawMessage) {
	var req SuspendRequest
	if err := json.Unmarshal(rawPayload, &req); err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID", Message: "invalid suspend request"}})
		return
	}

	pid, pidOK := s.resolvePID(req.PID, req.UUID)
	if !pidOK {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "NOT_FOUND", Message: "process not found"}})
		return
	}

	if err := s.kern.Suspend(pid); err != nil {
		code := "INTERNAL"
		var sysErr *kernel.SyscallError
		if errors.As(err, &sysErr) {
			code = string(sysErr.Code)
		}
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: code, Message: err.Error()}})
		return
	}

	proc, ok := s.kern.GetProcess(pid)
	if !ok {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INTERNAL", Message: "process disappeared after suspend"}})
		return
	}

	// Read last checkpoint step (best-effort)
	checkpointStep := 0
	stepsDir := proc.GetStepsDir()
	if stepsDir != "" {
		if cp, err := kernel.ReadCheckpointPublic(stepsDir); err == nil {
			checkpointStep = cp.LastStep
		}
	}

	resp := SuspendResponse{
		PID:            pid,
		UUID:           proc.UUID,
		State:          "suspended",
		CheckpointStep: checkpointStep,
	}
	payload, _ := json.Marshal(resp)
	writeResponse(conn, Response{OK: true, Payload: payload})
}

func (s *Server) handleResume(conn net.Conn, rawPayload json.RawMessage) {
	var req ResumeRequest
	if err := json.Unmarshal(rawPayload, &req); err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID", Message: "invalid resume request"}})
		return
	}

	if req.UUID == "" {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID", Message: "uuid is required"}})
		return
	}

	// Epic 42 fix: rebuild ProjectConfig for the resumed process so it inherits
	// project-level .env / providers.yaml / LLMFileOpener (mirrors handleSpawn).
	// Without this, resumeFromHistory falls back to the global VFS driver and the
	// resumed process loses project-level API keys — root cause of the Dashboard
	// `r` 401 bug. Best-effort: errors degrade to global-only mode rather than
	// failing the resume (back-compat with older clients that don't send ProjectDir).
	var projectCfg *config.ProjectConfig
	if req.ProjectDir != "" {
		cfg, _, cfgErr := s.resolveProjectContext(req.ProjectDir, req.RnixEnv)
		if cfgErr != nil {
			writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "CONFIG_ERROR", Message: cfgErr.Error()}})
			return
		}
		projectCfg = cfg
	}

	result, err := s.kern.ResumeWithOpts(req.UUID, kernel.ResumeOpts{
		Fork:          req.Fork,
		FromStep:      req.FromStep,
		ProjectConfig: projectCfg,
		NewInput:      req.NewInput,
	})
	if err != nil {
		code := "INTERNAL"
		var sysErr *kernel.SyscallError
		if errors.As(err, &sysErr) {
			code = string(sysErr.Code)
		}
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: code, Message: err.Error()}})
		return
	}

	// Epic 42 follow-up: handleResume returns immediately (no event stream),
	// so unlike handleSpawn's `defer s.kern.Reap(pid)`, nothing would transition
	// the resumed Zombie → Dead. Without a reaper, `proc.DeadAt` stays zero
	// forever and Dashboard's Agent Tree elapsed time grows past kill/error
	// (user-visible bug).
	//
	// Note: resumed processes are NOT necessarily top-level. kernel.resume
	// restoreParentLinkage re-attaches PPID + parent.AddChild when the parent
	// (UUID-matched) is still in procTable; otherwise the resumed process
	// becomes a root only because its parent is gone.
	//
	// Launch a background goroutine to wait for proc.Done (closed by
	// finishProcess) and trigger Reap, mirroring handleSpawn's lifecycle.
	// Reap is idempotent via reapOnce, so this is safe even if the user
	// also calls kernel.Wait(pid).
	if proc, ok := s.kern.GetProcess(result.PID); ok {
		s.wg.Go(func() {
			<-proc.Done
			s.kern.Reap(result.PID)
		})
	}

	resp := ResumeResponse{
		PID:             result.PID,
		UUID:            result.UUID,
		ResumedFromStep: result.ResumedFromStep,
	}
	payload, _ := json.Marshal(resp)
	writeResponse(conn, Response{OK: true, Payload: payload})
}

// handleResumeWatch (apex 10-11 / spike 10-10 S-1 fix) resumes a process and
// keeps the connection open, streaming the same event shapes as handleSpawn:
// initial Response carries ResumeResponse, then StreamProgress events flow
// until StreamComplete/StreamError. The kernel event surface already fires for
// resumed processes (resume.go OnSpawn + reason.go shared loop OnStep /
// OnThinking); the pre-existing one-shot handleResume simply never registered
// a callbackMux handler, so every event was dropped at the IPC delivery layer.
// This handler mirrors handleSpawn's "eventCh → callbackMux.register(pid) →
// stream loop → reap" lifecycle; handleResume's one-shot semantics stay
// untouched (zero behavior change for existing clients).
func (s *Server) handleResumeWatch(conn net.Conn, rawPayload json.RawMessage) {
	var req ResumeRequest
	if err := json.Unmarshal(rawPayload, &req); err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID", Message: "invalid resume_watch request"}})
		return
	}
	if req.UUID == "" {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID", Message: "uuid is required"}})
		return
	}

	// Rebuild ProjectConfig exactly like handleResume (project .env / providers /
	// LLMFileOpener); errors surface as CONFIG_ERROR, mirroring the one-shot path.
	var projectCfg *config.ProjectConfig
	if req.ProjectDir != "" {
		cfg, _, cfgErr := s.resolveProjectContext(req.ProjectDir, req.RnixEnv)
		if cfgErr != nil {
			writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "CONFIG_ERROR", Message: cfgErr.Error()}})
			return
		}
		projectCfg = cfg
	}

	// apex 10-11 started-semantics: gate the resumed process's LLM round until we
	// have written the initial OK response, so apex's "started" reflects a real
	// launch and a pre-OK read failure can't leave a running fork behind (double
	// run). Closed right after writeResponse(OK) below; the defer is a safety net
	// so the reasoning goroutine can never leak if we bail before that point.
	launchReady := make(chan struct{})
	launchClosed := false
	closeLaunch := func() {
		if !launchClosed {
			launchClosed = true
			close(launchReady)
		}
	}
	defer closeLaunch()

	result, err := s.kern.ResumeWithOpts(req.UUID, kernel.ResumeOpts{
		Fork:          req.Fork,
		FromStep:      req.FromStep,
		ProjectConfig: projectCfg,
		NewInput:      req.NewInput,
		LaunchReady:   launchReady,
	})
	if err != nil {
		code := "INTERNAL"
		var sysErr *kernel.SyscallError
		if errors.As(err, &sysErr) {
			code = string(sysErr.Code)
		}
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: code, Message: err.Error()}})
		return
	}

	pid := result.PID
	eventCh := make(chan StreamEvent, 64)
	s.callbackMux.register(pid, eventCh)
	defer s.callbackMux.unregister(pid)
	defer s.kern.Reap(pid) // mirror handleSpawn: reap after stream ends (idempotent via reapOnce)

	proc, ok := s.kern.GetProcess(pid)
	if !ok {
		writeStreamEvent(conn, StreamEvent{Type: StreamError, Payload: marshalJSON(ErrorPayload{Code: "INTERNAL", Message: "process vanished after resume"})})
		return
	}

	// Compensate for the OnSpawn event lost during kern.ResumeWithOpts (fires
	// before register). Shape matches handleSpawn's compensation event so apex
	// consumes resume streams with the same decoding path as spawn streams.
	spawnPP := ProgressPayload{Event: "spawn", PID: pid, Intent: proc.Intent, Provider: proc.Provider, Model: proc.Model, ReasoningEffort: proc.ReasoningEffort, UUID: proc.UUID}
	spawnPayload, _ := json.Marshal(spawnPP)
	select {
	case eventCh <- StreamEvent{Type: StreamProgress, Payload: spawnPayload}:
	default:
	}

	respPayload, _ := json.Marshal(ResumeResponse{
		PID:             result.PID,
		UUID:            result.UUID,
		ResumedFromStep: result.ResumedFromStep,
	})
	writeResponse(conn, Response{OK: true, Payload: respPayload})
	// OK is on the wire — release the resumed process's reasoning round. From here
	// apex's "started" reflects a launched process, so any later read failure
	// surfaces as-is instead of triggering a double-run fallback spawn.
	closeLaunch()

	doneCh := make(chan struct{})
	go func() {
		defer close(doneCh)
		exit := <-proc.Done
		pp := ProgressPayload{
			Event:      "complete",
			PID:        pid,
			ExitCode:   exit.Code,
			ExitReason: exit.Reason,
		}
		if info, infoErr := s.kern.GetProcInfo(pid); infoErr == nil {
			pp.Result = info.Result
			pp.TokensUsed = info.TokensUsed
		}
		if exit.Err != nil {
			pp.ErrorMessage = exit.Err.Error()
		}
		if spanID, spanOK := s.kern.GetSpanID(pid); spanOK {
			pp.SpanID = string(spanID)
		}
		completePayload, _ := json.Marshal(pp)
		select {
		case eventCh <- StreamEvent{Type: StreamComplete, Payload: completePayload}:
		default:
		}
	}()

	enc := json.NewEncoder(conn)
	for {
		select {
		case ev, evOK := <-eventCh:
			if !evOK {
				return
			}
			if err := enc.Encode(ev); err != nil {
				return
			}
			if ev.Type == StreamComplete || ev.Type == StreamError {
				return
			}
		case <-doneCh:
			for {
				select {
				case ev := <-eventCh:
					_ = enc.Encode(ev)
					if ev.Type == StreamComplete || ev.Type == StreamError {
						return
					}
				default:
					return
				}
			}
		case <-s.done:
			return
		}
	}
}

// handleGetProcDetail returns full process detail including env, skills, FD table, context stats (Story 27.6).
func (s *Server) handleGetProcDetail(conn net.Conn, rawPayload json.RawMessage) {
	var req GetProcDetailRequest
	if err := json.Unmarshal(rawPayload, &req); err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "invalid_request", Message: "invalid get_proc_detail request"}})
		return
	}

	proc, ok := s.resolveProcess(req.PID, req.UUID)
	if !ok {
		// Fallback: check procHistory for reaped processes
		s.handleGetProcDetailFromHistory(conn, req.PID, req.UUID)
		return
	}

	// Thread-safe snapshots
	snap := proc.GetDetailSnapshot()
	fdSnap := proc.GetFDSnapshot()

	resp := GetProcDetailResponse{
		PID:             snap.PID,
		UUID:            snap.UUID,
		PPID:            snap.PPID,
		State:           snap.State.String(),
		Intent:          snap.Intent,
		Provider:        snap.Provider,
		Model:           snap.Model,
		ReasoningEffort: snap.ReasoningEffort,
		CreatedAtMs:     snap.CreatedAt.UnixMilli(),
		AllowedDevices:  snap.AllowedDevices,
		AllowedTools:    snap.AllowedTools,
		ComposeNode:     snap.ComposeNode,
		ComposeDeps:     snap.ComposeDeps,
		PipelineIndex:   snap.PipelineIndex,
		PipelineTotal:   snap.PipelineTotal,
		OriginUUID:      snap.OriginUUID,
		ResumedFromStep: snap.ResumedFromStep,
		DriverMeta:      snap.DriverMeta,
		FeatureProfile:  snap.FeatureProfile,
	}
	if !snap.DeadAt.IsZero() {
		resp.DeadAtMs = snap.DeadAt.UnixMilli()
	}
	if snap.PausedTotal > 0 {
		resp.PausedTotalMs = snap.PausedTotal.Milliseconds()
	}

	// FD table
	fdEntries := make([]FDEntryWire, len(fdSnap))
	for i, f := range fdSnap {
		fdEntries[i] = FDEntryWire{FD: f.FD, DevicePath: f.DevicePath}
	}
	resp.FDTable = fdEntries

	// Env snapshot with masking
	envSnapshot := make(map[string]string)
	pc := proc.GetProjectConfig()
	if pc != nil && pc.EnvSnapshot != nil {
		for k, v := range pc.EnvSnapshot {
			if isSensitiveEnvKey(k) {
				envSnapshot[k] = "***"
			} else {
				envSnapshot[k] = v
			}
		}
	}
	resp.EnvSnapshot = envSnapshot

	// Build skill info with AllowedTools
	skillInfos := make([]SkillInfoWire, 0, len(snap.Skills))
	for _, name := range snap.Skills {
		si := SkillInfoWire{Name: name}
		if s.skillLoader != nil {
			info, err := s.skillLoader.LoadMetadata(name)
			if err == nil {
				si.AllowedTools = info.Manifest.AllowedTools()
			}
		}
		if si.AllowedTools == nil {
			si.AllowedTools = []string{}
		}
		skillInfos = append(skillInfos, si)
	}
	resp.Skills = skillInfos

	// Context stats from CtxManager
	resp.ContextStats = ContextStatsWire{
		TokensUsed:    snap.TokensUsed,
		ContextBudget: snap.ContextBudget,
	}
	if snap.ContextBudget > 0 && snap.LastInputTokens > 0 {
		resp.ContextStats.UsagePct = float64(snap.LastInputTokens) * 100.0 / float64(snap.ContextBudget)
	}
	if s.ctxMgr != nil && snap.CtxID > 0 {
		info, err := s.ctxMgr.GetContextInfo(snap.CtxID)
		if err == nil {
			switch mc := info["message_count"].(type) {
			case int:
				resp.ContextStats.MessageCount = mc
			case int64:
				resp.ContextStats.MessageCount = int(mc)
			case float64:
				resp.ContextStats.MessageCount = int(mc)
			}
		}
		slotUsed, slotMax, slotErr := s.ctxMgr.SlotUsage(snap.CtxID)
		if slotErr == nil {
			resp.ContextStats.SlotUsed = slotUsed
			resp.ContextStats.SlotMax = slotMax
			if slotMax > 0 {
				resp.ContextStats.SlotPercentage = float64(slotUsed) / float64(slotMax) * 100
			}
		}
	}

	payload, err := json.Marshal(resp)
	if err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "internal", Message: fmt.Sprintf("marshal get_proc_detail: %v", err)}})
		return
	}
	writeResponse(conn, Response{OK: true, Payload: payload})
}

// handleGetProcDetailFromHistory constructs a GetProcDetailResponse from procHistory
// for processes that have been reaped from the active process table.
func (s *Server) handleGetProcDetailFromHistory(conn net.Conn, pid types.PID, uuid string) {
	var info *vfs.ProcInfo
	if uuid != "" && isValidUUID(uuid) {
		info = s.kern.FindHistoryByUUID(uuid)
	}
	if info == nil && pid != 0 {
		info = s.kern.FindHistoryByPID(pid)
	}
	if info == nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "NOT_FOUND", Message: "process not found"}})
		return
	}

	resp := GetProcDetailResponse{
		PID:             info.PID,
		UUID:            info.UUID,
		PPID:            info.PPID,
		State:           info.State.String(),
		Intent:          info.Intent,
		Provider:        info.Provider,
		Model:           info.Model,
		ReasoningEffort: info.ReasoningEffort,
		CreatedAtMs:     info.CreatedAt.UnixMilli(),
		AllowedDevices:  info.AllowedDevices,
		AllowedTools:    info.AllowedTools,
		FDTable:         []FDEntryWire{},
		EnvSnapshot:     map[string]string{},
		ComposeNode:     info.ComposeNode,
		ComposeDeps:     info.ComposeDeps,
		PipelineIndex:   info.PipelineIndex,
		PipelineTotal:   info.PipelineTotal,
		OriginUUID:      info.OriginUUID,
		ResumedFromStep: info.ResumedFromStep,
		DriverMeta:      info.DriverMeta,
		FeatureProfile:  info.FeatureProfile,
	}
	if !info.DeadAt.IsZero() {
		resp.DeadAtMs = info.DeadAt.UnixMilli()
	}
	if info.PausedTotal > 0 {
		resp.PausedTotalMs = info.PausedTotal.Milliseconds()
	}

	// Build skill info from history
	skillInfos := make([]SkillInfoWire, 0, len(info.Skills))
	for _, name := range info.Skills {
		si := SkillInfoWire{Name: name}
		if s.skillLoader != nil {
			meta, err := s.skillLoader.LoadMetadata(name)
			if err == nil {
				si.AllowedTools = meta.Manifest.AllowedTools()
			}
		}
		if si.AllowedTools == nil {
			si.AllowedTools = []string{}
		}
		skillInfos = append(skillInfos, si)
	}
	resp.Skills = skillInfos

	resp.ContextStats = ContextStatsWire{
		TokensUsed:    info.TokensUsed,
		ContextBudget: info.ContextBudget,
	}
	if info.ContextBudget > 0 && info.LastInputTokens > 0 {
		resp.ContextStats.UsagePct = float64(info.LastInputTokens) * 100.0 / float64(info.ContextBudget)
	}

	payload, err := json.Marshal(resp)
	if err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "internal", Message: fmt.Sprintf("marshal get_proc_detail: %v", err)}})
		return
	}
	writeResponse(conn, Response{OK: true, Payload: payload})
}

// handleListResumable returns proc-info snapshots that survive daemon crashes
// (state=running on disk, UUID not in current procTable). Story 42.2 AC#7.
func (s *Server) handleListResumable(conn net.Conn) {
	infos, err := s.kern.ListResumable()
	if err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "internal", Message: fmt.Sprintf("list_resumable: %v", err)}})
		return
	}
	globalDataDir := s.kern.GetDataDir()
	wires := make([]ResumableProcessWire, 0, len(infos))
	for _, info := range infos {
		wires = append(wires, resumableInfoToWire(info, globalDataDir))
	}
	payload, err := json.Marshal(ListResumableResponse{Processes: wires})
	if err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "internal", Message: fmt.Sprintf("marshal list_resumable: %v", err)}})
		return
	}
	writeResponse(conn, Response{OK: true, Payload: payload})
}

// handleGc invokes kernel.RunGc(dryRun=false, force=true) and returns a
// summary. Story 42.5 AC#13. RED PHASE: kernel.RunGc returns a sentinel error
// which we map to ErrorPayload.
func (s *Server) handleGc(conn net.Conn, _ json.RawMessage) {
	result, err := s.kern.RunGc(false, true)
	if err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "internal", Message: fmt.Sprintf("gc: %v", err)}})
		return
	}
	resp := GcResponse{
		OK:           true,
		RemovedCount: result.RemovedCount,
		FreedBytes:   result.FreedBytes,
		RemovedUUIDs: result.RemovedUUIDs,
	}
	payload, err := json.Marshal(resp)
	if err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "internal", Message: fmt.Sprintf("marshal gc: %v", err)}})
		return
	}
	writeResponse(conn, Response{OK: true, Payload: payload})
}

// handleGcDryRun invokes kernel.RunGc(dryRun=true, _) and returns the
// candidates list. Story 42.5 AC#4 / AC#13.
func (s *Server) handleGcDryRun(conn net.Conn, _ json.RawMessage) {
	result, err := s.kern.RunGc(true, false)
	if err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "internal", Message: fmt.Sprintf("gc_dry_run: %v", err)}})
		return
	}
	cands := make([]GcCandidateWire, 0, len(result.Candidates))
	for _, c := range result.Candidates {
		cands = append(cands, GcCandidateWire{
			UUID:      c.UUID,
			DeadAt:    c.DeadAt,
			SizeBytes: c.SizeBytes,
			Reason:    c.Reason,
		})
	}
	resp := GcDryRunResponse{OK: true, DryRun: true, Candidates: cands}
	payload, err := json.Marshal(resp)
	if err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "internal", Message: fmt.Sprintf("marshal gc_dry_run: %v", err)}})
		return
	}
	writeResponse(conn, Response{OK: true, Payload: payload})
}

// resumableInfoToWire builds a ResumableProcessWire from a proc-info snapshot,
// preferring checkpoint.json for LastStep/LastActive when available.
func resumableInfoToWire(info vfs.ProcInfo, globalDataDir string) ResumableProcessWire {
	w := ResumableProcessWire{
		UUID:     info.UUID,
		Intent:   info.Intent,
		Agent:    deriveResumableAgent(info),
		Provider: info.Provider,
		Model:    info.Model,
	}

	// LastStep / LastActive: prefer checkpoint.json when present (most accurate
	// snapshot of in-flight progress); fall back to proc-info timestamps.
	if globalDataDir != "" && info.UUID != "" {
		if projBaseDir := kernel.FindBaseDirByUUID(globalDataDir, info.UUID); projBaseDir != "" {
			cpPath := filepath.Join(projBaseDir, "steps", info.UUID)
			if cp, err := kernel.ReadCheckpointPublic(cpPath); err == nil {
				w.LastStep = cp.LastStep
				if !cp.Timestamp.IsZero() {
					w.LastActive = cp.Timestamp.UnixMilli()
				}
			}
		}
	}
	if w.LastActive == 0 {
		switch {
		case !info.DeadAt.IsZero():
			w.LastActive = info.DeadAt.UnixMilli()
		case !info.CreatedAt.IsZero():
			w.LastActive = info.CreatedAt.UnixMilli()
		}
	}
	return w
}

// deriveResumableAgent picks a short agent label: first skill name, or the
// first word of the intent. Returns empty string when neither is available.
func deriveResumableAgent(info vfs.ProcInfo) string {
	if len(info.Skills) > 0 && info.Skills[0] != "" {
		return info.Skills[0]
	}
	if info.Intent == "" {
		return ""
	}
	fields := strings.Fields(info.Intent)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}
