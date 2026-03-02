package ipc

import (
	"bufio"
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
		if p.State != types.StateDead {
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
		if p.State != types.StateDead {
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
		Model:    req.Model,
		MaxTurns: req.MaxSteps,
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

	logCh, ok := s.kern.GetLogChan(req.PID)
	if !ok || logCh == nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "NOT_FOUND", Message: "process not found or no log channel"}})
		return
	}

	writeResponse(conn, Response{OK: true})

	enc := json.NewEncoder(conn)
	for entry := range logCh {
		lew := LogEntryToWire(entry)
		payload, _ := json.Marshal(lew)
		se := StreamEvent{Type: StreamLogEntry, Payload: payload}
		if err := enc.Encode(se); err != nil {
			return
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

