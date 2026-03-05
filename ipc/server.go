package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gonewx/crux/agents"
	"github.com/gonewx/crux/internal/types"
	"github.com/gonewx/crux/internal/xsync"
	"github.com/gonewx/crux/kernel"
	"github.com/gonewx/crux/shell"
)

const (
	DefaultIdleTimeout = 60 * time.Second
	idleCheckEvery     = 5 * time.Second
)

// AgentLoaderFunc loads an agent by name. Matches agents.AgentLoader.Load signature.
type AgentLoaderFunc func(name string) (*agents.AgentInfo, error)

// Server is the IPC server that bridges client requests to the kernel.
type Server struct {
	kern        *kernel.KernelImpl
	agentLoader AgentLoaderFunc
	callbackMux *callbackMux
	version     string
	IdleTimeout time.Duration

	listener    net.Listener
	activeConns atomic.Int32
	done        chan struct{}
	closeOnce   sync.Once
	stopOnce    sync.Once
	wg          sync.WaitGroup

	mu          sync.Mutex
	idleTimer   *time.Timer
	idleStopped bool
}

// NewServer creates an IPC server backed by the given kernel.
// The callbackMux is registered as the kernel's callback handler for per-PID routing.
func NewServer(kern *kernel.KernelImpl, agentLoader AgentLoaderFunc, version string) *Server {
	s := &Server{
		kern:        kern,
		agentLoader: agentLoader,
		callbackMux: newCallbackMux(),
		version:     version,
		IdleTimeout: DefaultIdleTimeout,
		done:        make(chan struct{}),
	}
	return s
}

func (s *Server) idleTimeoutDuration() time.Duration {
	if s.IdleTimeout > 0 {
		return s.IdleTimeout
	}
	return DefaultIdleTimeout
}

// ListenAndServe starts listening on the given socket path and serving requests.
// Blocks until Shutdown is called or an error occurs.
func (s *Server) ListenAndServe(socketPath string) error {
	if err := os.MkdirAll(filepath.Dir(socketPath), 0700); err != nil {
		return fmt.Errorf("ipc: create socket dir: %w", err)
	}

	ln, err := net.Listen("unix", socketPath)
	if err != nil {
		return fmt.Errorf("ipc: listen %s: %w", socketPath, err)
	}
	s.listener = ln

	s.mu.Lock()
	s.idleTimer = time.AfterFunc(s.idleTimeoutDuration(), func() {
		s.tryAutoShutdown()
	})
	s.mu.Unlock()

	s.wg.Add(1)
	go s.acceptLoop()

	s.wg.Add(1)
	go s.idleWatcher()

	return nil
}

func (s *Server) acceptLoop() {
	defer s.wg.Done()
	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.done:
				return
			default:
				log.Printf("[ipc-server] accept error: %v", err)
				return
			}
		}
		s.activeConns.Add(1)
		s.resetIdle()
		s.wg.Add(1)
		go s.handleConn(conn)
	}
}

func (s *Server) idleWatcher() {
	defer s.wg.Done()
	ticker := time.NewTicker(idleCheckEvery)
	defer ticker.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
			s.checkIdle()
		}
	}
}

func (s *Server) checkIdle() {
	procs := s.kern.ListProcs()
	activeProcs := 0
	for _, p := range procs {
		if p.State == types.StateRunning || p.State == types.StateCreated {
			activeProcs++
		}
	}
	if activeProcs > 0 || s.activeConns.Load() > 0 {
		s.mu.Lock()
		if !s.idleStopped {
			s.idleTimer.Stop()
		}
		s.mu.Unlock()
	}
	// When idle: let the timer count down naturally from the last resetIdle call.
}

func (s *Server) resetIdle() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.idleStopped {
		s.idleTimer.Reset(s.idleTimeoutDuration())
	}
}

func (s *Server) tryAutoShutdown() {
	procs := s.kern.ListProcs()
	activeProcs := 0
	for _, p := range procs {
		if p.State == types.StateRunning || p.State == types.StateCreated {
			activeProcs++
		}
	}
	if activeProcs == 0 && s.activeConns.Load() == 0 {
		s.Shutdown()
	} else {
		s.resetIdle()
	}
}

// Shutdown gracefully stops the server: closes listener, waits for connections, cleans up.
func (s *Server) Shutdown() {
	s.stopOnce.Do(func() {
		s.mu.Lock()
		s.idleStopped = true
		s.idleTimer.Stop()
		s.mu.Unlock()

		s.closeOnce.Do(func() { close(s.done) })
		if s.listener != nil {
			s.listener.Close()
		}
	})
}

// Done returns a channel that is closed when the server has stopped.
func (s *Server) Done() <-chan struct{} {
	return s.done
}

// Wait blocks until all server goroutines have exited.
func (s *Server) Wait() {
	s.wg.Wait()
}

func (s *Server) handleConn(conn net.Conn) {
	defer s.wg.Done()
	defer func() {
		conn.Close()
		s.activeConns.Add(-1)
		s.resetIdle()
	}()

	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 0, 1<<20), 1<<20)

	for scanner.Scan() {
		var req Request
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID", Message: "malformed request"}})
			return
		}

		switch req.Method {
		case MethodPing:
			s.handlePing(conn)
		case MethodListProcs:
			s.handleListProcs(conn)
		case MethodKill:
			s.handleKill(conn, req.Payload)
		case MethodSpawn:
			s.handleSpawn(conn, req.Payload)
			return // streaming method — handler manages connection lifetime
		case MethodAttachDebug:
			s.handleAttachDebug(conn, req.Payload)
			return // streaming method — handler manages connection lifetime
		case MethodAttachLog:
			s.handleAttachLog(conn, req.Payload)
			return // streaming method — handler manages connection lifetime
		case MethodSpawnPipeline:
			s.handleSpawnPipeline(conn, req.Payload)
			return // streaming method
		case MethodExecScript:
			s.handleExecScript(conn, req.Payload)
			return // streaming method
		case MethodShutdown:
			s.handleShutdown(conn)
			return
		default:
			writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID", Message: fmt.Sprintf("unknown method: %s", req.Method)}})
			return
		}
	}
}

func (s *Server) handlePing(conn net.Conn) {
	payload, _ := json.Marshal(PingResponse{Version: s.version})
	writeResponse(conn, Response{OK: true, Payload: payload})
}

func (s *Server) handleListProcs(conn net.Conn) {
	procs := s.kern.ListProcs()
	wireProcs := make([]ProcInfoWire, len(procs))
	for i, p := range procs {
		wireProcs[i] = ProcInfoToWire(p)
	}
	payload, _ := json.Marshal(ListProcsResponse{Processes: wireProcs})
	writeResponse(conn, Response{OK: true, Payload: payload})
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

	if err := s.kern.Kill(req.PID, req.Signal); err != nil {
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

func (s *Server) handleShutdown(conn net.Conn) {
	writeResponse(conn, Response{OK: true})
	go s.Shutdown()
}

func (s *Server) handleSpawn(conn net.Conn, rawPayload json.RawMessage) {
	var req SpawnRequest
	if err := json.Unmarshal(rawPayload, &req); err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID", Message: "invalid spawn request"}})
		return
	}

	var agentInfo *agents.AgentInfo
	if req.Agent != "" && s.agentLoader != nil {
		var err error
		agentInfo, err = s.agentLoader(req.Agent)
		if err != nil {
			writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "NOT_FOUND", Message: fmt.Sprintf("agent %q not found: %v", req.Agent, err)}})
			return
		}
	}

	eventCh := make(chan StreamEvent, 64)

	pid, err := s.kern.Spawn(req.Intent, agentInfo, kernel.SpawnOpts{
		Model:         req.Model,
		MaxTurns:      req.MaxSteps,
		ContextBudget: req.ContextBudget,
		TimeoutMs:     req.TimeoutMs,
	})
	if err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INTERNAL", Message: err.Error()}})
		return
	}

	s.callbackMux.register(pid, eventCh)
	defer s.callbackMux.unregister(pid)
	defer s.kern.Reap(pid) // Reap top-level process after stream ends (no CLI Wait in daemon mode)

	// Compensate for OnSpawn event lost during kern.Spawn (fires before register)
	spawnPP := ProgressPayload{Event: "spawn", PID: pid, Intent: req.Intent}
	spawnPayload, _ := json.Marshal(spawnPP)
	select {
	case eventCh <- StreamEvent{Type: StreamProgress, Payload: spawnPayload}:
	default:
	}

	payload, _ := json.Marshal(SpawnResponse{PID: pid})
	writeResponse(conn, Response{OK: true, Payload: payload})

	proc, ok := s.kern.GetProcess(pid)
	if !ok {
		writeStreamEvent(conn, StreamEvent{Type: StreamError, Payload: marshalJSON(ErrorPayload{Code: "INTERNAL", Message: "process vanished after spawn"})})
		return
	}

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
		if info, err := s.kern.GetProcInfo(pid); err == nil {
			pp.Result = info.Result
			pp.TokensUsed = info.TokensUsed
		}
		if exit.Err != nil {
			pp.ErrorMessage = exit.Err.Error()
		}

		payload, _ := json.Marshal(pp)
		select {
		case eventCh <- StreamEvent{Type: StreamComplete, Payload: payload}:
		default:
		}
	}()

	enc := json.NewEncoder(conn)
	for {
		select {
		case ev, ok := <-eventCh:
			if !ok {
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

func (s *Server) handleAttachDebug(conn net.Conn, rawPayload json.RawMessage) {
	var req AttachDebugRequest
	if err := json.Unmarshal(rawPayload, &req); err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID", Message: "invalid attach_debug request"}})
		return
	}

	debugCh, ok := s.kern.GetDebugChan(req.PID)
	if !ok || debugCh == nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "NOT_FOUND", Message: "process not found or no debug channel"}})
		return
	}

	writeResponse(conn, Response{OK: true})

	enc := json.NewEncoder(conn)
	for event := range debugCh {
		sew := SyscallEventToWire(event)
		payload, _ := json.Marshal(sew)
		se := StreamEvent{Type: StreamSyscallEvent, Payload: payload}
		if err := enc.Encode(se); err != nil {
			return
		}
	}
	_ = enc.Encode(StreamEvent{Type: StreamEOF})
}

func (s *Server) handleAttachLog(conn net.Conn, rawPayload json.RawMessage) {
	var req AttachLogRequest
	if err := json.Unmarshal(rawPayload, &req); err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID", Message: "invalid attach_log request"}})
		return
	}

	// Check process existence via both history and live channel
	history, histOK := s.kern.GetLogHistory(req.PID)
	logCh, logOK := s.kern.GetLogChan(req.PID)

	if !histOK && (!logOK || logCh == nil) {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "NOT_FOUND", Message: "process not found"}})
		return
	}

	writeResponse(conn, Response{OK: true})
	enc := json.NewEncoder(conn)

	// Replay history logs with timestamp tracking for dedup
	var lastReplayedTs int64
	if histOK && len(history) > 0 {
		for _, entry := range history {
			lew := LogEntryToWire(entry)
			payload, _ := json.Marshal(lew)
			se := StreamEvent{Type: StreamLogEntry, Payload: payload}
			if err := enc.Encode(se); err != nil {
				return
			}
			lastReplayedTs = lew.TimestampMs
		}
	}

	// Stream live logs (if process is still alive)
	if logOK && logCh != nil {
		for entry := range logCh {
			lew := LogEntryToWire(entry)
			if lew.TimestampMs < lastReplayedTs {
				continue // skip entries older than last replayed
			}
			payload, _ := json.Marshal(lew)
			se := StreamEvent{Type: StreamLogEntry, Payload: payload}
			if err := enc.Encode(se); err != nil {
				return
			}
		}
	}

	_ = enc.Encode(StreamEvent{Type: StreamEOF})
}

// callbackMux routes KernelCallbacks events to per-PID channels for streaming to clients.
type callbackMux struct {
	handlers *xsync.SyncMap[types.PID, chan<- StreamEvent]
}

func newCallbackMux() *callbackMux {
	return &callbackMux{
		handlers: xsync.NewSyncMap[types.PID, chan<- StreamEvent](),
	}
}

func (m *callbackMux) register(pid types.PID, ch chan<- StreamEvent) {
	m.handlers.Store(pid, ch)
}

func (m *callbackMux) unregister(pid types.PID) {
	m.handlers.Delete(pid)
}

func (m *callbackMux) send(pid types.PID, ev StreamEvent) {
	ch, ok := m.handlers.Load(pid)
	if ok {
		select {
		case ch <- ev:
		default:
		}
	}
}

// KernelCallbacks interface implementation for the server's callbackMux.

func (m *callbackMux) OnSpawn(pid types.PID, intent string) {
	pp := ProgressPayload{Event: "spawn", PID: pid, Intent: intent}
	payload, _ := json.Marshal(pp)
	m.send(pid, StreamEvent{Type: StreamProgress, Payload: payload})
}

func (m *callbackMux) OnStep(pid types.PID, step int, total int) {
	pp := ProgressPayload{Event: "step", PID: pid, Step: step, Total: total}
	payload, _ := json.Marshal(pp)
	m.send(pid, StreamEvent{Type: StreamProgress, Payload: payload})
}

func (m *callbackMux) OnComplete(pid types.PID, result string, exit kernel.ExitStatus) {
	// Completion is handled by the spawn handler goroutine reading from proc.Done.
}

func (m *callbackMux) OnError(pid types.PID, err error) {
	pp := ProgressPayload{Event: "error", PID: pid, ErrorMessage: err.Error()}
	payload, _ := json.Marshal(pp)
	m.send(pid, StreamEvent{Type: StreamProgress, Payload: payload})
}

// Compile-time check that callbackMux implements KernelCallbacks.
var _ kernel.KernelCallbacks = (*callbackMux)(nil)

// CallbackMux returns the server's callback multiplexer for use as KernelCallbacks.
func (s *Server) CallbackMux() kernel.KernelCallbacks {
	return s.callbackMux
}

// SetKernel sets the kernel instance after construction (for circular init).
func (s *Server) SetKernel(k *kernel.KernelImpl) {
	s.kern = k
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

	writeResponse(conn, Response{OK: true})

	enc := json.NewEncoder(conn)
	spawner := &ipcKernelSpawner{
		kernel:      s.kern,
		agentLoader: s.agentLoader,
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
	kernel      *kernel.KernelImpl
	agentLoader AgentLoaderFunc
	pids        []types.PID
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

	pid, err := s.kernel.Spawn(intent, agentInfo, kernel.SpawnOpts{Model: model})
	if err != nil {
		return "", 1, 0, err
	}
	s.pids = append(s.pids, pid)

	proc, ok := s.kernel.GetProcess(pid)
	if !ok {
		return "", 1, 0, fmt.Errorf("process vanished after spawn")
	}

	select {
	case exit := <-proc.Done:
		info, infoErr := s.kernel.GetProcInfo(pid)
		s.kernel.Reap(pid)
		if infoErr != nil {
			return "", exit.Code, 0, nil
		}
		return info.Result, exit.Code, info.TokensUsed, nil
	case <-ctx.Done():
		_ = s.kernel.Kill(pid, types.SIGTERM)
		s.kernel.Reap(pid)
		return "", 1, 0, ctx.Err()
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

	writeResponse(conn, Response{OK: true})

	enc := json.NewEncoder(conn)
	spawner := &ipcKernelSpawner{
		kernel:      s.kern,
		agentLoader: s.agentLoader,
	}

	executor := shell.NewScriptExecutor(spawner, env)
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		select {
		case <-s.done:
			cancel()
		case <-ctx.Done():
		}
	}()

	result, execErr := executor.Execute(ctx, script)
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

// --- helpers ---

func writeResponse(conn net.Conn, resp Response) {
	enc := json.NewEncoder(conn)
	_ = enc.Encode(resp)
}

func writeStreamEvent(conn net.Conn, ev StreamEvent) {
	enc := json.NewEncoder(conn)
	_ = enc.Encode(ev)
}

func marshalJSON(v any) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}

