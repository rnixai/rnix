package ipc

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"maps"
	"net"
	"path/filepath"

	"github.com/rnixai/rnix/agents"
	"github.com/rnixai/rnix/internal/config"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/kernel"
	"github.com/rnixai/rnix/shell"
)

// Story 44.2 ADR-section: Ctrl+C semantics redefined. CLI disconnect no longer
// cancels scriptCtx; the daemon emits SignalTree(scriptPID, SIGPAUSE) and the
// ScriptExecutor parks at the next statement boundary via SuspendController,
// keeping the whole subtree resumable. The three sentinel errors that the
// pre-44.2 finalizeScriptRunner used to distinguish CLI-disconnect from
// kill/shutdown (errCLIDisconnected / errScriptKilled / errDaemonShutdown) are
// deleted — finalize now branches purely on the process state (Suspended) and
// execErr. See finalizeScriptRunner below for the case analysis. The canonical
// SuspendReason for this path lives in kernel.SuspendReasonCLIDisconnected and
// is set by the kernel SignalTree → SuspendSubtree path, not by ipc.

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
			Type:             "spawn",
			Intent:           c.Intent,
			Agent:            c.Agent,
			Model:            c.Model,
			Provider:         c.Provider,
			FallbackProvider: c.FallbackProvider,
			FallbackModel:    c.FallbackModel,
			ReasoningEffort:  c.ReasoningEffort,
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

	// Record cooperation edges between consecutive pipeline stages (Story 51.2).
	if s.immuneDaemon != nil && len(spawner.agentTemplates) > 1 {
		for i := 1; i < len(spawner.agentTemplates); i++ {
			prev, curr := spawner.agentTemplates[i-1], spawner.agentTemplates[i]
			if prev != "" && curr != "" {
				s.immuneDaemon.RecordCooperationTyped(prev, curr, "spawn")
			}
		}
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
	kernel         *kernel.KernelImpl
	agentLoader    AgentLoaderFunc
	projectConfig  *config.ProjectConfig // per-spawn project config snapshot (nil = global only)
	pids           []types.PID
	agentTemplates []string  // resolved agent template per stage (for cooperation recording)
	pipelineTotal  int       // total stages in pipeline (set before Execute)
	parentPID      types.PID // script runner PID; 0 if not in a script context
}

func (s *ipcKernelSpawner) SpawnAndWait(ctx context.Context, req shell.SpawnRequest) (string, int, int, error) {
	intent := req.Intent
	agentName := req.Agent
	model := req.Model
	resolvedAgent := agentName
	if resolvedAgent == "" {
		resolvedAgent = "stem"
	}
	var agentInfo *agents.AgentInfo
	if s.agentLoader != nil {
		info, loadErr := s.agentLoader(resolvedAgent)
		if loadErr != nil {
			if agentName != "" {
				return "", 1, 0, fmt.Errorf("agent %q not found: %v", agentName, loadErr)
			}
		} else {
			agentInfo = info
		}
	}

	pid, err := s.kernel.Spawn(intent, agentInfo, kernel.SpawnOpts{
		Model:            model,
		Provider:         req.Provider,
		FallbackProvider: req.FallbackProvider,
		FallbackModel:    req.FallbackModel,
		ReasoningEffort:  req.ReasoningEffort,
		ProjectConfig:    s.projectConfig,
		PipelineIndex:    len(s.pids),
		PipelineTotal:    s.pipelineTotal,
		ParentPID:        s.parentPID,
	})
	if err != nil {
		return "", 1, 0, err
	}
	s.pids = append(s.pids, pid)
	s.agentTemplates = append(s.agentTemplates, resolvedAgent)

	proc, ok := s.kernel.GetProcess(pid)
	if !ok {
		return "", 1, 0, fmt.Errorf("process vanished after spawn")
	}

	for {
		select {
		case exit := <-proc.Done:
			// Story 44.5 AC7 — proc.Done double-loads "terminal exit" and
			// "mid-state suspend" notifications (kernel/suspend.go
			// notifySuspendDone writes ExitSuspended on every Pause cycle).
			// Without checking state here, a SIGPAUSE on the child surfaces
			// to ScriptExecutor as exitCode=ExitSuspended=2 and triggers
			// on-error (the dev-auto.ash "上一轮执行异常" regression).
			// Unix wait(2) semantics: wait blocks across SIGSTOP, returning
			// only on terminal exit. Mid-state Done events are discarded.
			switch proc.GetState() {
			case types.StateZombie, types.StateDead:
				info, infoErr := s.kernel.GetProcInfo(pid)
				s.kernel.Reap(pid)
				if infoErr != nil {
					return "", exit.Code, 0, nil
				}
				return info.Result, exit.Code, info.TokensUsed, nil
			default:
				// StateRunning (Suspend transition window — notifySuspendDone
				// writes Done before suspendProcess flips state) or
				// StateSuspended: discard the exit and re-arm select. The
				// next Done write will come from finishProcess when the
				// resumed reasonStep actually completes.
				continue
			}
		case <-ctx.Done():
			// Story 44.2: scriptCtx is only cancelled on real termination
			// (SIGKILL/SIGTERM/daemon shutdown) — CLI disconnect now suspends
			// the subtree instead, so a suspended child never reaches here via
			// disconnect. Preserve intentionally-suspended/paused children;
			// otherwise tear the child down to match the parent termination.
			if info, err := s.kernel.GetProcInfo(pid); err == nil &&
				(info.IsPaused || info.State == types.StateSuspended) {
				return "", 1, 0, ctx.Err()
			}
			_ = s.kernel.Kill(pid, types.SIGTERM)
			s.kernel.Reap(pid)
			return "", 1, 0, ctx.Err()
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

	// OBS-1 (Epic 43, Story 43.2 / D4): pre-attach EventWriter via
	// SpawnOpts.EventWriterFactory so the Spawn event itself lands in
	// script-runner's events.jsonl. Factory is invoked inside Spawn after
	// UUID assignment but before the first emitEvent. Failure is logged
	// and non-fatal — EmitScriptEvent gates on `ew != nil` internally.
	scriptPID, err := s.kern.Spawn(scriptIntent, nil, kernel.SpawnOpts{
		SkipReasonLoop: true,
		ProjectConfig:  projectCfg,
		EventWriterFactory: func(proc *kernel.Process) (*kernel.EventWriter, error) {
			stepBaseDir := s.kern.ResolveStepBaseDir(proc)
			if stepBaseDir == "" {
				return nil, nil
			}
			return kernel.NewEventWriter(stepBaseDir, proc.UUID)
		},
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

	// Build a context that is cancelled when the script-runner is truly
	// terminating:
	//   1. The script-runner process receives SIGTERM/SIGKILL (K in rnix top)
	//   2. The daemon is shutting down
	//
	// Story 44.2: client disconnect (Ctrl+C) is NO LONGER a cancellation — see
	// the conn.Read goroutine below, which now emits SignalTree(SIGPAUSE)
	// instead, so the subtree suspends (and stays resumable) rather than
	// tearing down. The CancelledCh watcher distinguishes a suspend-driven
	// proc.Cancel() (suspendRequested==true) from a real kill: see
	// runCancelWatcher (AC#1.b).
	scriptCtx, scriptCancel := context.WithCancelCause(context.Background())
	defer scriptCancel(nil)

	go runCancelWatcher(s.done, scriptCtx, scriptProc, scriptCancel)

	// Detect client disconnect: the client only writes the initial request, so
	// any subsequent read returns EOF/error when the client closes the
	// connection. Story 44.2 (AC#1): on disconnect we emit SIGPAUSE on the
	// whole subtree — equivalent to pressing `p` on the root script-runner —
	// rather than cancelling scriptCtx. The executor parks at the next
	// statement boundary; `rnix resume <uuid>` / dashboard `R` continues it.
	go func() {
		buf := make([]byte, 1)
		conn.Read(buf) //nolint:errcheck // intentional: only care about close, not the value
		if _, err := s.kern.SignalTree(scriptPID, types.SIGPAUSE); err != nil {
			log.Printf("[exec_script] SignalTree(SIGPAUSE) on CLI disconnect: pid=%d err=%v", scriptPID, err)
		}
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
	// Story 44.2 (AC#3): wire the SuspendController so the executor parks at a
	// statement boundary when SignalTree(SIGPAUSE) suspends the subtree.
	executor.SetSuspendController(processSuspendController{proc: scriptProc})
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

	// OBS-1 (Epic 43, Story 43.2): forward shell-side ScriptEvents into
	// the kernel's unified emitEvent path. ScriptEvent stays a shell-only
	// type (kernel never imports shell), so this closure does the small
	// shape translation here at the IPC boundary: line + intent get
	// promoted into the syscall args alongside whatever Meta the shell
	// already filled in. The kernel side gates on `ew != nil` and
	// `proc != nil`, so this is safe even if EventWriter init failed
	// above.
	executor.OnEvent = func(ev shell.ScriptEvent) {
		args := make(map[string]any, len(ev.Meta)+2)
		maps.Copy(args, ev.Meta)
		// P9: don't clobber Meta["line"] / Meta["intent"] if the shell side
		// ever starts populating them — future emit sites that set these
		// explicitly should win over the generic ev.Line / ev.Intent fields.
		if _, present := args["line"]; !present {
			args["line"] = ev.Line
		}
		if ev.Intent != "" {
			if _, present := args["intent"]; !present {
				args["intent"] = ev.Intent
			}
		}
		s.kern.EmitScriptEvent(scriptProc, string(ev.Kind), args)
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
	outcome := finalizeScriptRunner(scriptProc, execErr, lastResult, lastExitCode)
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
// process. Story 44.2 simplified the decision to two inputs — the process
// state and execErr — after deleting the three sentinel causes. Streaming +
// Reap are returned in scriptRunnerOutcome so the caller stays in control of
// network I/O.
//
// Cases:
//
//	case-0: scriptProc is Suspended
//	        The SignalTree(SIGPAUSE) path (CLI disconnect / dashboard `p`)
//	        suspended the subtree before the executor returned. Under normal
//	        Ctrl+C the executor parks at the statement boundary and never
//	        reaches finalize while Suspended; this branch only fires in the
//	        rare race where ctx was also cancelled at the same instant. Keep
//	        the process in procTable (NO reap), preserve its SuspendReason,
//	        and stream SCRIPT_SUSPENDED so a future `rnix resume` / dashboard
//	        `R` can continue the whole subtree.
//
//	case-1: execErr != nil (and state != Suspended)
//	        Real failure / kill / daemon shutdown → Finish(code=1, execErr),
//	        Reap, stream SCRIPT_ERROR.
//
//	case-2: execErr == nil
//	        Clean run → Finish(lastResult, lastExitCode, nil), Reap. Caller
//	        streams ExecScriptResponse on the success path (returnEarly=false).
func finalizeScriptRunner(scriptProc *kernel.Process, execErr error, lastResult string, lastExitCode int) scriptRunnerOutcome {
	// case-0: subtree was SIGPAUSE-suspended. Suspended takes precedence over
	// execErr — surface SCRIPT_SUSPENDED, never a misleading success/error.
	// Do NOT reap (keep it resumable) and do NOT overwrite SuspendReason.
	if scriptProc.GetState() == types.StateSuspended {
		return scriptRunnerOutcome{
			reapAfter: false,
			streamPayload: ErrorPayload{
				Code:    "SCRIPT_SUSPENDED",
				Message: "script subtree suspended; resume to continue",
			},
			streamType:  StreamError,
			returnEarly: true,
		}
	}
	// case-1: real error path (kill / shutdown / non-ctx error).
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
	// case-2: clean run.
	scriptProc.Finish(lastResult, lastExitCode, nil)
	return scriptRunnerOutcome{
		reapAfter:   true,
		returnEarly: false,
	}
}
