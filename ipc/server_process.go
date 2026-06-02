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

func (s *Server) handleListAllProcs(conn net.Conn) {
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
	wireProcs := make([]ProcInfoWire, len(procs))
	for i, p := range procs {
		wireProcs[i] = ProcInfoToWire(p)
	}
	payload, _ := json.Marshal(ListProcsResponse{Processes: wireProcs})
	writeResponse(conn, Response{OK: true, Payload: payload})
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

	pid, pidOK := s.resolvePID(req.PID, req.UUID)
	if !pidOK {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "NOT_FOUND", Message: "process not found"}})
		return
	}

	if err := s.kern.Kill(pid, req.Signal); err != nil {
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

func (s *Server) handleSignalTree(conn net.Conn, rawPayload json.RawMessage) {
	var req SignalTreeRequest
	if err := json.Unmarshal(rawPayload, &req); err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID", Message: "invalid signal_tree request"}})
		return
	}
	if req.Signal == 0 {
		req.Signal = types.SIGPAUSE
	}

	pid, pidOK := s.resolvePID(req.PID, req.UUID)
	if !pidOK {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "NOT_FOUND", Message: "process not found"}})
		return
	}

	affected, err := s.kern.SignalTree(pid, req.Signal)
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
		CreatedAtMs:     snap.CreatedAt.UnixMilli(),
		AllowedDevices:  snap.AllowedDevices,
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
		CreatedAtMs:     info.CreatedAt.UnixMilli(),
		AllowedDevices:  info.AllowedDevices,
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
