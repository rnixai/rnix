package ipc

import (
	"context"
	"encoding/json"
	"errors"
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

// Sentinel errors carried by context.WithCancelCause so the post-run cleanup
// in handleExecScript can distinguish CLI client disconnect (suspend parent,
// preserve children) from explicit kill / daemon shutdown (finish parent as
// failed and reap, matching legacy behaviour). Each scriptCancel goroutine
// passes one of these as its cause.
var (
	errCLIDisconnected = errors.New("cli disconnected")
	errScriptKilled    = errors.New("script process killed")
	errDaemonShutdown  = errors.New("daemon shutdown")
)

// suspendReasonCLIDisconnected re-binds the exported kernel constant so the
// rest of this file reads naturally. Using the kernel value (rather than a
// duplicated literal) makes mis-spellings a compile error in either package.
const suspendReasonCLIDisconnected = kernel.SuspendReasonCLIDisconnected

// scriptRunnerHeartbeatInterval is how often handleExecScript pings the
// script-runner's LastHeartbeat. See HB-1 (Epic 43, Story 43.1).
const scriptRunnerHeartbeatInterval = 10 * time.Second

// keepScriptRunnerHeartbeat ticks proc.LastHeartbeat at the given interval
// until ctx is done. It exists so handleExecScript can run it as a goroutine
// for the script-runner's full lifetime, complementing SpawnAndWait's
// narrower wait-for-child ticker. Unit-tested with a small interval; the
// real call site uses scriptRunnerHeartbeatInterval.
func keepScriptRunnerHeartbeat(ctx context.Context, proc *kernel.Process, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			proc.TouchHeartbeat()
		}
	}
}

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
			cause := context.Cause(ctx)
			// CLI-disconnect: preserve the child for later resume — do NOT kill
			// or reap. Matches the script-runner parent's Suspend semantics in
			// handleExecScript. Any child state (Running / Paused) survives.
			if errors.Is(cause, errCLIDisconnected) {
				return "", 1, 0, ctx.Err()
			}
			// Pre-existing carve-out: paused children are intentionally suspended
			// and should survive parent context cancellation regardless of cause.
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
	//
	// Each goroutine passes a sentinel cause via context.WithCancelCause so the
	// post-run block below can tell which reason fired and decide whether the
	// script-runner parent process should be Suspended (cli disconnect, allow
	// resume) or Finished as failed (kill / shutdown — legacy behaviour).
	scriptCtx, scriptCancel := context.WithCancelCause(context.Background())
	defer scriptCancel(nil)

	go func() {
		select {
		case <-s.done:
			scriptCancel(errDaemonShutdown)
		case <-scriptProc.CancelledCh():
			scriptCancel(errScriptKilled)
		case <-scriptCtx.Done():
		}
	}()

	// Detect client disconnect: the client only writes the initial request, so any
	// subsequent read returns EOF/error when the client closes the connection.
	go func() {
		buf := make([]byte, 1)
		conn.Read(buf) //nolint:errcheck // intentional: only care about close, not the value
		scriptCancel(errCLIDisconnected)
	}()

	// HB-1 (Epic 43, Story 43.1): keep the script-runner's heartbeat alive for
	// its entire lifetime. SkipReasonLoop processes don't get per-step heartbeat
	// updates that reasoning processes get; SpawnAndWait's own ticker only covers
	// the wait-for-child window. Without this, time spent between spawns
	// (statement gaps, condition evaluation, idle while-loop with paused
	// children) accumulates with no heartbeat and trips HeartbeatMonitor's STALL
	// detection. Coexists with SpawnAndWait's ticker as defence-in-depth; both
	// call TouchHeartbeat under proc.mu and are idempotent at 10s cadence.
	go keepScriptRunnerHeartbeat(scriptCtx, scriptProc, scriptRunnerHeartbeatInterval)

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

	// Decide how to finalise the script-runner process. See finalizeScriptRunner
	// for the full case analysis. We pass result.LastResult/LastExitCode only
	// when execErr is nil (clean run path) so the helper has all it needs.
	var lastResult string
	var lastExitCode int
	if execErr == nil && result != nil {
		lastResult = result.LastResult
		lastExitCode = result.LastExitCode
	}
	outcome := finalizeScriptRunner(scriptProc, context.Cause(scriptCtx), execErr, lastResult, lastExitCode)
	if outcome.reapAfter {
		s.kern.Reap(scriptPID)
	}
	if outcome.streamPayload != nil {
		payload, _ := json.Marshal(outcome.streamPayload)
		_ = enc.Encode(StreamEvent{Type: outcome.streamType, Payload: payload})
	}
	if outcome.returnEarly {
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

// scriptRunnerOutcome describes what handleExecScript should do after the
// script executor returns, decoupling the decision (testable) from the
// side effects (streaming, reaping) that require live IPC state.
type scriptRunnerOutcome struct {
	// reapAfter is true when the caller should call kernel.Reap(scriptPID).
	// false for the CLI-disconnect path where the parent stays in procTable
	// as Suspended so a Dashboard resume can re-activate it.
	reapAfter bool
	// streamPayload + streamType are the StreamEvent to encode back to the
	// client before returning, or nil/"" if nothing should be streamed by
	// the helper (the success path encodes ExecScriptResponse itself).
	streamPayload any
	streamType    StreamEventType
	// returnEarly is true when handleExecScript should return immediately
	// (interrupted or errored paths); false when the success path should
	// fall through to encoding the ExecScriptResponse.
	returnEarly bool
}

// finalizeScriptRunner picks the post-execute action for the script-runner
// process based on (cause, execErr) and applies the side effects on the
// process itself (Suspend / Finish). Streaming + Reap are returned in
// scriptRunnerOutcome so the caller stays in control of network I/O.
//
// Cases:
//  1. execErr != nil && cause is errCLIDisconnected && parent still Running
//     → Suspend(parent, reason=cli_disconnected), NOT reap. Stream
//     SCRIPT_INTERRUPTED. Dashboard `R` on any descendant can re-activate
//     via kernel.reactivateCliDisconnectedAncestors.
//  2. execErr != nil (kill / shutdown / non-ctx error)
//     → Finish(code=1, execErr), Reap. Stream SCRIPT_ERROR.
//  3. execErr == nil
//     → Finish(lastResult, lastExitCode, nil), Reap. Caller streams
//     ExecScriptResponse on the success path (returnEarly=false).
//
// On Suspend transition failure (should not happen from Running) we fall
// back to the case-2 path so a half-transitioned process is never left in
// procTable.
func finalizeScriptRunner(scriptProc *kernel.Process, cause, execErr error, lastResult string, lastExitCode int) scriptRunnerOutcome {
	if execErr != nil && errors.Is(cause, errCLIDisconnected) &&
		scriptProc.GetState() == types.StateRunning {
		scriptProc.SetSuspendReason(suspendReasonCLIDisconnected)
		if suspendErr := scriptProc.Suspend(); suspendErr == nil {
			return scriptRunnerOutcome{
				reapAfter: false,
				streamPayload: ErrorPayload{
					Code:    "SCRIPT_INTERRUPTED",
					Message: "cli disconnected; script suspended",
				},
				streamType:  StreamError,
				returnEarly: true,
			}
		}
		// Fallback: state transition failed → revert to legacy Finish+Reap.
		// Clear SuspendReason first so the persisted proc-info.json snapshot
		// doesn't carry a stale "cli_disconnected" tag — that would mislead
		// reactivateCliDisconnectedAncestors during any later descendant
		// resume into believing this (now Dead) process is wakeable.
		scriptProc.SetSuspendReason("")
		scriptProc.Finish("interrupted", 1, execErr)
		return scriptRunnerOutcome{
			reapAfter: true,
			streamPayload: ErrorPayload{
				Code:    "SCRIPT_ERROR",
				Message: execErr.Error(),
			},
			streamType:  StreamError,
			returnEarly: true,
		}
	}
	if execErr != nil {
		scriptProc.Finish("", 1, execErr)
		return scriptRunnerOutcome{
			reapAfter: true,
			streamPayload: ErrorPayload{
				Code:    "SCRIPT_ERROR",
				Message: execErr.Error(),
			},
			streamType:  StreamError,
			returnEarly: true,
		}
	}
	scriptProc.Finish(lastResult, lastExitCode, nil)
	return scriptRunnerOutcome{
		reapAfter:   true,
		returnEarly: false,
	}
}
