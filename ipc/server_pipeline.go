package ipc

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"path/filepath"
	"time"

	"github.com/rnixai/rnix/agents"
	"github.com/rnixai/rnix/internal/config"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/kernel"
	"github.com/rnixai/rnix/shell"
)

// --- spawn pipeline ---

func (s *Server) handleSpawnPipeline(conn net.Conn, rawPayload json.RawMessage) {
	var req SpawnPipelineRequest
	if err := json.Unmarshal(rawPayload, &req); err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID", Message: "invalid spawn_pipeline request"}})
		return
	}
	if len(req.Commands) == 0 {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID", Message: "pipeline must have at least one command"}})
		return
	}

	pipeline := &shell.Pipeline{
		Commands: make([]shell.Command, len(req.Commands)),
	}
	for i, c := range req.Commands {
		pipeline.Commands[i] = shell.Command{
			Type:   "spawn",
			Intent: c.Intent,
			Agent:  c.Agent,
			Model:  c.Model,
		}
	}

	// Build project config and project-aware loaders (Story 25.3)
	projectCfg, agentLoaderFn, err := s.resolveProjectContext(req.ProjectDir, req.RnixEnv)
	if err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "CONFIG_ERROR", Message: err.Error()}})
		return
	}

	writeResponse(conn, Response{OK: true})

	enc := json.NewEncoder(conn)
	spawner := &ipcKernelSpawner{
		kernel:        s.kern,
		agentLoader:   agentLoaderFn,
		projectConfig: projectCfg,
		pipelineTotal: len(req.Commands),
	}

	executor := shell.NewPipelineExecutor(spawner)
	executor.OnStageStart = func(stage, total int, intent string) {
		pp := ProgressPayload{
			Event: "pipeline_stage",
			Step:  stage,
			Total: total,
		}
		payload, _ := json.Marshal(pp)
		_ = enc.Encode(StreamEvent{Type: StreamProgress, Payload: payload})
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-s.done:
			cancel()
		case <-ctx.Done():
		}
	}()

	result, err := executor.Execute(ctx, pipeline)
	if err != nil {
		ep := ErrorPayload{Code: "PIPELINE_ERROR", Message: err.Error()}
		payload, _ := json.Marshal(ep)
		_ = enc.Encode(StreamEvent{Type: StreamError, Payload: payload})
		return
	}

	resp := SpawnPipelineResponse{
		Stages: make([]PipelineStageWire, len(result.Stages)),
	}
	for i, stage := range result.Stages {
		var pid types.PID
		if i < len(spawner.pids) {
			pid = spawner.pids[i]
		}
		resp.Stages[i] = PipelineStageWire{
			PID:        pid,
			Intent:     stage.Intent,
			Result:     stage.Result,
			ExitCode:   stage.ExitCode,
			TokensUsed: stage.TokensUsed,
			ElapsedMs:  stage.Elapsed.Milliseconds(),
		}
	}

	payload, _ := json.Marshal(resp)
	_ = enc.Encode(StreamEvent{Type: StreamComplete, Payload: payload})
}

// ipcKernelSpawner adapts the real kernel to the shell.KernelSpawner interface.
type ipcKernelSpawner struct {
	kernel        *kernel.KernelImpl
	agentLoader   AgentLoaderFunc
	projectConfig *config.ProjectConfig // per-spawn project config snapshot (nil = global only)
	pids          []types.PID
	pipelineTotal int       // total stages in pipeline (set before Execute)
	parentPID     types.PID // script runner PID; 0 if not in a script context
}

func (s *ipcKernelSpawner) SpawnAndWait(ctx context.Context, intent, agentName, model string) (string, int, int, error) {
	var agentInfo *agents.AgentInfo
	if agentName != "" && s.agentLoader != nil {
		var err error
		agentInfo, err = s.agentLoader(agentName)
		if err != nil {
			return "", 1, 0, fmt.Errorf("agent %q not found: %v", agentName, err)
		}
	}

	pid, err := s.kernel.Spawn(intent, agentInfo, kernel.SpawnOpts{
		Model:         model,
		ProjectConfig: s.projectConfig,
		PipelineIndex: len(s.pids),
		PipelineTotal: s.pipelineTotal,
		ParentPID:     s.parentPID,
	})
	if err != nil {
		return "", 1, 0, err
	}
	s.pids = append(s.pids, pid)

	proc, ok := s.kernel.GetProcess(pid)
	if !ok {
		return "", 1, 0, fmt.Errorf("process vanished after spawn")
	}

	// Keep parent's heartbeat alive while waiting for child, so
	// HeartbeatMonitor doesn't mistake the idle parent for stalled.
	var heartbeatTicker *time.Ticker
	var parentProc *kernel.Process
	if s.parentPID > 0 {
		if pp, ppOK := s.kernel.GetProcess(s.parentPID); ppOK {
			parentProc = pp
			heartbeatTicker = time.NewTicker(10 * time.Second)
			defer heartbeatTicker.Stop()
		}
	}
	hbCh := func() <-chan time.Time {
		if heartbeatTicker != nil {
			return heartbeatTicker.C
		}
		return nil
	}()

	for {
		select {
		case exit := <-proc.Done:
			info, infoErr := s.kernel.GetProcInfo(pid)
			s.kernel.Reap(pid)
			if infoErr != nil {
				return "", exit.Code, 0, nil
			}
			return info.Result, exit.Code, info.TokensUsed, nil
		case <-ctx.Done():
			// Don't kill paused child processes — they are intentionally suspended
			// and should survive parent context cancellation.
			if info, err := s.kernel.GetProcInfo(pid); err == nil && info.IsPaused {
				return "", 1, 0, ctx.Err()
			}
			_ = s.kernel.Kill(pid, types.SIGTERM)
			s.kernel.Reap(pid)
			return "", 1, 0, ctx.Err()
		case <-hbCh:
			parentProc.TouchHeartbeat()
		}
	}
}

func (s *ipcKernelSpawner) Wait(ctx context.Context, pid int) (int, error) {
	proc, ok := s.kernel.GetProcess(types.PID(pid))
	if !ok {
		return 1, fmt.Errorf("process %d not found", pid)
	}
	select {
	case exit := <-proc.Done:
		s.kernel.Reap(types.PID(pid))
		return exit.Code, nil
	case <-ctx.Done():
		return 1, ctx.Err()
	}
}

// Compile-time check that ipcKernelSpawner implements shell.KernelSpawner.
var _ shell.KernelSpawner = (*ipcKernelSpawner)(nil)

// --- exec script ---

func (s *Server) handleExecScript(conn net.Conn, rawPayload json.RawMessage) {
	var req ExecScriptRequest
	if err := json.Unmarshal(rawPayload, &req); err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID", Message: "invalid exec_script request"}})
		return
	}

	script, err := shell.ParseScript(req.Script)
	if err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "PARSE_ERROR", Message: err.Error()}})
		return
	}

	env := shell.NewEnvironment()
	for k, v := range req.Env {
		env.Set(k, v)
	}

	// Build project config and project-aware loaders (Story 25.3)
	projectCfg, agentLoaderFn, err := s.resolveProjectContext(req.ProjectDir, "")
	if err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "CONFIG_ERROR", Message: err.Error()}})
		return
	}

	// Derive a display name for the script runner process.
	scriptIntent := "run: script"
	if f, ok := req.Env["RNIX_SCRIPT_FILE"]; ok && f != "" {
		scriptIntent = "run: " + filepath.Base(f)
	}

	// Register the script itself as a kernel process (visible in rnix top, killable via K).
	scriptPID, err := s.kern.Spawn(scriptIntent, nil, kernel.SpawnOpts{
		SkipReasonLoop: true,
		ProjectConfig:  projectCfg,
	})
	if err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "SPAWN_ERROR", Message: err.Error()}})
		return
	}
	scriptProc, ok := s.kern.GetProcess(scriptPID)
	if !ok {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INTERNAL", Message: "script runner process vanished after spawn"}})
		return
	}

	writeResponse(conn, Response{OK: true})

	enc := json.NewEncoder(conn)

	// Build a context that is cancelled when:
	//   1. The script runner process receives SIGTERM/SIGKILL (K in rnix top)
	//   2. The daemon is shutting down
	//   3. The client disconnects (Ctrl+C or terminal close)
	scriptCtx, scriptCancel := context.WithCancel(context.Background())
	defer scriptCancel()

	go func() {
		select {
		case <-s.done:
			scriptCancel()
		case <-scriptProc.CancelledCh():
			scriptCancel()
		case <-scriptCtx.Done():
		}
	}()

	// Detect client disconnect: the client only writes the initial request, so any
	// subsequent read returns EOF/error when the client closes the connection.
	go func() {
		buf := make([]byte, 1)
		conn.Read(buf) //nolint:errcheck // intentional: only care about close, not the value
		scriptCancel()
	}()

	spawner := &ipcKernelSpawner{
		kernel:        s.kern,
		agentLoader:   agentLoaderFn,
		projectConfig: projectCfg,
		parentPID:     scriptPID,
	}

	executor := shell.NewScriptExecutor(spawner, env)
	if req.ScriptDir != "" {
		executor.SetScriptDir(req.ScriptDir)
	}
	executor.OnStageStart = func(stage, total int, intent string) {
		pp := ProgressPayload{
			Event:  "script_step",
			Step:   stage,
			Total:  total,
			Intent: intent,
		}
		payload, _ := json.Marshal(pp)
		_ = enc.Encode(StreamEvent{Type: StreamProgress, Payload: payload})
	}

	result, execErr := executor.Execute(scriptCtx, script)

	// Finalise the script runner process so it transitions Running→Zombie and is reaped.
	// twoPhaseShutdown (if running because of a K kill) will unblock on proc.terminated.
	if execErr != nil {
		scriptProc.Finish("", 1, execErr)
	} else {
		scriptProc.Finish(result.LastResult, result.LastExitCode, nil)
	}
	s.kern.Reap(scriptPID)

	if execErr != nil {
		ep := ErrorPayload{Code: "SCRIPT_ERROR", Message: execErr.Error()}
		payload, _ := json.Marshal(ep)
		_ = enc.Encode(StreamEvent{Type: StreamError, Payload: payload})
		return
	}

	resp := ExecScriptResponse{
		LastResult:   result.LastResult,
		LastExitCode: result.LastExitCode,
		TotalTokens:  result.TotalTokens,
		ElapsedMs:    result.Elapsed.Milliseconds(),
	}
	payload, _ := json.Marshal(resp)
	_ = enc.Encode(StreamEvent{Type: StreamComplete, Payload: payload})
}
