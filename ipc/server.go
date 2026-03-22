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
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/rnixai/rnix/agents"
	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/debug"
	"github.com/rnixai/rnix/drivers/llm"
	"github.com/rnixai/rnix/internal/config"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/xsync"
	"github.com/rnixai/rnix/kernel"
	"github.com/rnixai/rnix/shell"
	"github.com/rnixai/rnix/skills"
	"github.com/rnixai/rnix/vfs"

	"github.com/goccy/go-yaml"
)

const (
	DefaultIdleTimeout = 60 * time.Second
	idleCheckEvery     = 5 * time.Second
)

// AgentLoaderFunc loads an agent by name. Matches agents.AgentLoader.Load signature.
type AgentLoaderFunc func(name string) (*agents.AgentInfo, error)

// intentManager abstracts intent.Manager to avoid direct import cycle.
type intentManager interface {
	ApplyIntent(ctx context.Context, intentStr, model, provider string, autoStart bool) (intentID string, treeJSON []byte, err error)
	ConfirmIntent(intentID string) error
	ExecuteIntent(ctx context.Context, intentID string,
		onNodeStart func(nodeID string, pid uint64),
		onNodeComplete func(nodeID, result string),
		onNodeFailed func(nodeID, errMsg string),
		onProgress func(completed, total int),
		onNodeRetry func(nodeID string, attempt, maxRetries int),
		onNodeTimeout func(nodeID string),
		onDriftDetected func(nodeID, driftType, message string),
		onDriftResolved func(nodeID string),
	) error
	IntentStatus(intentID string) ([]byte, error)
	ListActiveIntents() ([]byte, error)
	ApplyIncrementalIntent(ctx context.Context, intentID, intentStr, model, provider string) (string, []byte, error)
	ListAllIntents() ([]byte, error)
}

// Server is the IPC server that bridges client requests to the kernel.
type Server struct {
	kern        *kernel.KernelImpl
	agentLoader AgentLoaderFunc
	callbackMux *callbackMux
	version     string
	IdleTimeout time.Duration
	ctxMgr      *rnixctx.Manager
	skillLoader *skills.SkillLoader

	listener    net.Listener
	activeConns atomic.Int32
	done        chan struct{}
	closeOnce   sync.Once
	stopOnce    sync.Once
	wg          sync.WaitGroup

	mu          sync.Mutex
	idleTimer   *time.Timer
	idleStopped bool

	// gdb session management: per-PID detach signals
	gdbMu       sync.Mutex
	gdbDetachCh map[types.PID]chan struct{}

	// intent management (set via SetIntentManager)
	intentMgr intentManager

	// provider health status (set via SetProviderStatusFunc)
	providerStatuses func() []ProviderStatusWire

	// reputation store (set via SetReputationStore, Story 21.3)
	reputationStore *kernel.ReputationStore

	// synergy matrix (set via SetSynergyMatrix, Story 21.5)
	synergyMatrix *kernel.SynergyMatrix

	// immune daemon (set via SetImmuneDaemon, Story 22.1)
	immuneDaemon *kernel.ImmuneDaemon

	// global config (set via SetGlobalConfig, Story 25.3)
	globalConfig *config.GlobalConfig

	// globalProvidersRaw caches raw YAML bytes from global providers.yaml
	// for DeepMergeYAML with project-level providers.yaml.
	globalProvidersRaw []byte

	// projectProviders tracks active project-level providers for daemon status display.
	// Key: projectDir, Value: list of project-only providers (not in global config).
	projectProviders *xsync.SyncMap[string, []ProviderStatusWire]
}

// NewServer creates an IPC server backed by the given kernel.
// The callbackMux is registered as the kernel's callback handler for per-PID routing.
func NewServer(kern *kernel.KernelImpl, agentLoader AgentLoaderFunc, version string) *Server {
	s := &Server{
		kern:             kern,
		agentLoader:      agentLoader,
		callbackMux:      newCallbackMux(),
		version:          version,
		IdleTimeout:      DefaultIdleTimeout,
		done:             make(chan struct{}),
		projectProviders: xsync.NewSyncMap[string, []ProviderStatusWire](),
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
		case MethodAttachGdb:
			s.handleAttachGdb(conn, req.Payload)
			return // streaming method — handler manages connection lifetime
		case MethodDetachGdb:
			s.handleDetachGdb(conn, req.Payload)
		case MethodGdbCommand:
			s.handleGdbCommand(conn, req.Payload)
		case MethodRecordStart:
			s.handleRecordStart(conn, req.Payload)
		case MethodRecordStop:
			s.handleRecordStop(conn, req.Payload)
		case MethodRecordList:
			s.handleRecordList(conn)
		case MethodReplayLoad:
			s.handleReplayLoad(conn, req.Payload)
		case MethodForkContinue:
			s.handleForkContinue(conn, req.Payload)
		case MethodSpawnPipeline:
			s.handleSpawnPipeline(conn, req.Payload)
			return // streaming method
		case MethodExecScript:
			s.handleExecScript(conn, req.Payload)
			return // streaming method
		case MethodCtxProfile:
			s.handleCtxProfile(conn, req.Payload)
		case MethodCtxGrowth:
			s.handleCtxGrowth(conn, req.Payload)
		case MethodApplyIntent:
			s.handleApplyIntent(conn, req.Payload, scanner)
			return // streaming method
		case MethodIntentStatus:
			s.handleIntentStatus(conn, req.Payload)
		case MethodIntentConfirm:
			s.handleIntentConfirm(conn, req.Payload)
		case MethodApplyIncrementalIntent:
			s.handleApplyIncrementalIntent(conn, req.Payload)
		case MethodIntentList:
			s.handleIntentList(conn)
		case MethodLineage:
			s.handleLineage(conn, req.Payload)
		case MethodProviderStatus:
			s.handleProviderStatus(conn)
		case MethodBudgetStatus:
			s.handleBudgetStatus(conn, req.Payload)
		case MethodSLAStatus:
			s.handleSLAStatus(conn, req.Payload)
		case MethodReputationStatus:
			s.handleReputationStatus(conn, req.Payload)
		case MethodSynergyList:
			s.handleSynergyList(conn)
		case MethodImmuneStatus:
			s.handleImmuneStatus(conn)
		case MethodImmuneResume:
			s.handleImmuneResume(conn, req.Payload)
		case MethodSimilarityQuery:
			s.handleSimilarityQuery(conn, req.Payload)
		case MethodTopologyQuery:
			s.handleTopologyQuery(conn)
		case MethodGetStepDetail:
			s.handleGetStepDetail(conn, req.Payload)
		case MethodListSteps:
			s.handleListSteps(conn, req.Payload)
		case MethodGetProcDetail:
			s.handleGetProcDetail(conn, req.Payload)
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

func (s *Server) handleCtxProfile(conn net.Conn, rawPayload json.RawMessage) {
	var req CtxProfileRequest
	if err := json.Unmarshal(rawPayload, &req); err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID", Message: "invalid ctx_profile request"}})
		return
	}

	info, err := s.kern.GetProcInfo(req.PID)
	if err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "NOT_FOUND", Message: fmt.Sprintf("process %d not found", req.PID)}})
		return
	}

	if info.State != types.StateRunning && info.State != types.StateZombie {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{
			Code:    "INVALID",
			Message: fmt.Sprintf("process %d is in %s state; ctx-profile requires running or zombie", req.PID, info.State),
		}})
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

func (s *Server) handleCtxGrowth(conn net.Conn, rawPayload json.RawMessage) {
	var req CtxGrowthRequest
	if err := json.Unmarshal(rawPayload, &req); err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID", Message: "invalid ctx_growth request"}})
		return
	}

	info, err := s.kern.GetProcInfo(req.PID)
	if err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "NOT_FOUND", Message: fmt.Sprintf("process %d not found", req.PID)}})
		return
	}

	if info.State != types.StateRunning {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{
			Code:    "INVALID",
			Message: fmt.Sprintf("process %d is in %s state; ctx-growth requires running", req.PID, info.State),
		}})
		return
	}

	history, err := s.kern.GetTokenHistory(req.PID)
	if err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INTERNAL", Message: err.Error()}})
		return
	}

	currentStep := 0
	if len(history) > 0 {
		currentStep = history[len(history)-1].Step
	}

	maxSteps := info.MaxSteps
	if maxSteps <= 0 {
		maxSteps = 10 // fallback to kernel.DefaultMaxSteps
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

	events, err := s.kern.GetLineage(req.PID)
	if err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "NOT_FOUND", Message: fmt.Sprintf("process %d not found", req.PID)}})
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
		PID:    req.PID,
		Events: ipcEvents,
	}
	payload, _ := json.Marshal(resp)
	writeResponse(conn, Response{OK: true, Payload: payload})
}

func (s *Server) handleProviderStatus(conn net.Conn) {
	var wires []ProviderStatusWire
	if s.providerStatuses != nil {
		wires = s.providerStatuses()
		// Tag global providers
		for i := range wires {
			wires[i].Source = "global"
		}
	}
	if wires == nil {
		wires = []ProviderStatusWire{}
	}

	// Append project-level providers
	seen := make(map[string]bool, len(wires))
	for _, w := range wires {
		seen[w.Name] = true
	}
	s.projectProviders.Range(func(_ string, providers []ProviderStatusWire) bool {
		for _, p := range providers {
			if !seen[p.Name] {
				wires = append(wires, p)
				seen[p.Name] = true
			}
		}
		return true
	})

	payload, _ := json.Marshal(ProviderStatusResponse{Providers: wires})
	writeResponse(conn, Response{OK: true, Payload: payload})
}

func (s *Server) handleBudgetStatus(conn net.Conn, rawPayload json.RawMessage) {
	var req BudgetStatusRequest
	if err := json.Unmarshal(rawPayload, &req); err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID", Message: "invalid budget_status request"}})
		return
	}

	status, err := s.kern.GetBudgetStatus(req.GroupID)
	if err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "NOT_FOUND", Message: err.Error()}})
		return
	}

	quotaWires := make([]AgentQuotaWire, len(status.Quotas))
	for i, q := range status.Quotas {
		var priStr string
		switch q.Priority {
		case kernel.PriorityHigh:
			priStr = "high"
		case kernel.PriorityNormal:
			priStr = "normal"
		case kernel.PriorityLow:
			priStr = "low"
		default:
			priStr = "normal"
		}
		quotaWires[i] = AgentQuotaWire{
			PID:      q.PID,
			Name:     q.Name,
			Priority: priStr,
			Quota:    q.Quota,
			Consumed: q.Consumed,
		}
	}

	resp := BudgetStatusResponse{
		TotalBudget: status.TotalBudget,
		Allocated:   status.Allocated,
		Consumed:    status.Consumed,
		Remaining:   status.Remaining,
		Quotas:      quotaWires,
	}
	respPayload, _ := json.Marshal(resp)
	writeResponse(conn, Response{OK: true, Payload: respPayload})
}

func (s *Server) handleSLAStatus(conn net.Conn, rawPayload json.RawMessage) {
	var req SLAStatusRequest
	if err := json.Unmarshal(rawPayload, &req); err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID", Message: "invalid sla_status request"}})
		return
	}

	results, err := s.kern.GetSLAResults(req.GroupID)
	if err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "NOT_FOUND", Message: err.Error()}})
		return
	}

	wires := make([]SLAResultWire, len(results))
	for i, r := range results {
		wires[i] = SLAResultWire{
			AgentName:  r.AgentName,
			Passed:     r.Passed,
			Checks:     r.Checks,
			TokensUsed: r.TokensUsed,
			DurationMs: r.DurationMs,
		}
	}

	resp := SLAStatusResponse{Results: wires}
	respPayload, _ := json.Marshal(resp)
	writeResponse(conn, Response{OK: true, Payload: respPayload})
}

func (s *Server) handleReputationStatus(conn net.Conn, rawPayload json.RawMessage) {
	var req ReputationStatusRequest
	if err := json.Unmarshal(rawPayload, &req); err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID", Message: "invalid reputation_status request"}})
		return
	}

	if s.reputationStore == nil {
		resp := ReputationStatusResponse{Summaries: []kernel.ReputationSummary{}}
		respPayload, _ := json.Marshal(resp)
		writeResponse(conn, Response{OK: true, Payload: respPayload})
		return
	}

	var summaries []kernel.ReputationSummary

	if req.AgentName != "" {
		summary, err := s.reputationStore.GetSummary(req.AgentName)
		if err != nil {
			writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INTERNAL", Message: err.Error()}})
			return
		}
		summaries = []kernel.ReputationSummary{*summary}
	} else {
		all, err := s.reputationStore.GetAllSummaries()
		if err != nil {
			writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INTERNAL", Message: err.Error()}})
			return
		}
		summaries = all
	}

	resp := ReputationStatusResponse{Summaries: summaries}
	respPayload, _ := json.Marshal(resp)
	writeResponse(conn, Response{OK: true, Payload: respPayload})
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

	// Build project config and project-aware loaders (Story 25.3)
	projectCfg, agentLoaderFn, err := s.resolveProjectContext(req.ProjectDir, req.RnixEnv)
	if err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "CONFIG_ERROR", Message: err.Error()}})
		return
	}

	var agentInfo *agents.AgentInfo
	if req.Agent != "" && agentLoaderFn != nil {
		var err error
		agentInfo, err = agentLoaderFn(req.Agent)
		if err != nil {
			writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "NOT_FOUND", Message: fmt.Sprintf("agent %q not found: %v", req.Agent, err)}})
			return
		}
	}

	eventCh := make(chan StreamEvent, 64)

	pid, err := s.kern.Spawn(req.Intent, agentInfo, kernel.SpawnOpts{
		Model:         req.Model,
		Provider:      req.Provider,
		MaxTurns:      req.MaxSteps,
		ContextBudget: req.ContextBudget,
		TimeoutMs:     req.TimeoutMs,
		TraceID:       types.TraceID(req.TraceID),
		ParentSpanID:  types.SpanID(req.ParentSpanID),
		ProjectConfig: projectCfg,
	})
	if err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INTERNAL", Message: err.Error()}})
		return
	}

	s.callbackMux.register(pid, eventCh)
	defer s.callbackMux.unregister(pid)
	defer s.kern.Reap(pid) // Reap top-level process after stream ends (no CLI Wait in daemon mode)

	// Compensate for OnSpawn event lost during kern.Spawn (fires before register)
	// Retrieve process to access resolved provider/model
	proc, ok := s.kern.GetProcess(pid)
	if !ok {
		writeStreamEvent(conn, StreamEvent{Type: StreamError, Payload: marshalJSON(ErrorPayload{Code: "INTERNAL", Message: "process vanished after spawn"})})
		return
	}
	spawnPP := ProgressPayload{Event: "spawn", PID: pid, Intent: req.Intent, Provider: proc.Provider, Model: proc.Model, UUID: proc.UUID}
	spawnPayload, _ := json.Marshal(spawnPP)
	select {
	case eventCh <- StreamEvent{Type: StreamProgress, Payload: spawnPayload}:
	default:
	}

	payload, _ := json.Marshal(SpawnResponse{PID: pid, UUID: proc.UUID})
	writeResponse(conn, Response{OK: true, Payload: payload})

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
		// Include SpanID for compose parent-child trace (Story 15.1)
		if spanID, ok := s.kern.GetSpanID(pid); ok {
			pp.SpanID = string(spanID)
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
			if lew.TimestampMs <= lastReplayedTs {
				continue // skip entries already replayed from history
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

func (s *Server) handleAttachGdb(conn net.Conn, rawPayload json.RawMessage) {
	var req AttachGdbRequest
	if err := json.Unmarshal(rawPayload, &req); err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID", Message: "invalid attach_gdb request"}})
		return
	}

	// Validate process exists and is Running
	info, infoErr := s.kern.GetProcInfo(req.PID)
	if infoErr != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "NOT_FOUND", Message: "process not found"}})
		return
	}
	if info.State != types.StateRunning {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID_STATE", Message: fmt.Sprintf("process %d is %s, not running", req.PID, info.State)}})
		return
	}

	debugCh, debugOK := s.kern.GetDebugChan(req.PID)
	logCh, logOK := s.kern.GetLogChan(req.PID)

	if (!debugOK || debugCh == nil) && (!logOK || logCh == nil) {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "NOT_FOUND", Message: "process not found or no channels"}})
		return
	}

	// Get process for Done channel monitoring
	proc, procOK := s.kern.GetProcess(req.PID)
	if !procOK {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "NOT_FOUND", Message: "process not found"}})
		return
	}

	// Check if process was cancelled (Kill called but state not yet transitioned)
	if proc.IsCancelled() {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID_STATE", Message: fmt.Sprintf("process %d has been terminated", req.PID)}})
		return
	}

	// Register per-PID detach channel (single-attach enforcement)
	// Must happen BEFORE sending OK response to prevent race conditions
	detachCh := make(chan struct{})
	s.gdbMu.Lock()
	if s.gdbDetachCh == nil {
		s.gdbDetachCh = make(map[types.PID]chan struct{})
	}
	if _, exists := s.gdbDetachCh[req.PID]; exists {
		s.gdbMu.Unlock()
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "ALREADY_ATTACHED", Message: fmt.Sprintf("process %d already has an active gdb session", req.PID)}})
		return
	}
	s.gdbDetachCh[req.PID] = detachCh
	s.gdbMu.Unlock()
	defer func() {
		s.gdbMu.Lock()
		delete(s.gdbDetachCh, req.PID)
		s.gdbMu.Unlock()
	}()

	// Build initial response with process metadata
	skills := info.Skills
	if skills == nil {
		skills = []string{}
	}
	gdbResp := AttachGdbResponse{
		PID:        info.PID,
		State:      info.State,
		Intent:     info.Intent,
		Skills:     skills,
		TokensUsed: info.TokensUsed,
	}
	payload, _ := json.Marshal(gdbResp)
	writeResponse(conn, Response{OK: true, Payload: payload})

	enc := json.NewEncoder(conn)

	// Get process cancellation channel for Kill detection
	cancelledCh := proc.CancelledCh()

	// Forward events from both channels using select
	for {
		select {
		case event, ok := <-debugCh:
			if !ok {
				// Debug channel closed -- send state change and EOF
				statePayload, _ := json.Marshal(map[string]any{"pid": req.PID, "state": "exited"})
				_ = enc.Encode(GdbEvent{Type: StreamGdbStateChange, Payload: statePayload})
				_ = enc.Encode(GdbEvent{Type: StreamEOF})
				return
			}
			sew := SyscallEventToWire(event)
			evPayload, _ := json.Marshal(sew)
			// Route GdbPause events as gdb_prompt for breakpoint notifications
			evType := StreamGdbSyscall
			if event.Syscall == "GdbPause" {
				promptPayload, _ := json.Marshal(event.Args)
				evType = StreamGdbPrompt
				evPayload = promptPayload
			}
			if err := enc.Encode(GdbEvent{Type: evType, Payload: evPayload}); err != nil {
				return
			}
		case entry, ok := <-logCh:
			if !ok {
				// Log channel closed -- send state change and EOF
				statePayload, _ := json.Marshal(map[string]any{"pid": req.PID, "state": "exited"})
				_ = enc.Encode(GdbEvent{Type: StreamGdbStateChange, Payload: statePayload})
				_ = enc.Encode(GdbEvent{Type: StreamEOF})
				return
			}
			lew := LogEntryToWire(entry)
			evPayload, _ := json.Marshal(lew)
			if err := enc.Encode(GdbEvent{Type: StreamGdbLog, Payload: evPayload}); err != nil {
				return
			}
		case <-detachCh:
			// Client sent detach via separate connection -- send EOF and return
			_ = enc.Encode(GdbEvent{Type: StreamEOF})
			return
		case <-proc.Done:
			// Process exited -- send state change and EOF
			statePayload, _ := json.Marshal(map[string]any{"pid": req.PID, "state": "exited"})
			_ = enc.Encode(GdbEvent{Type: StreamGdbStateChange, Payload: statePayload})
			_ = enc.Encode(GdbEvent{Type: StreamEOF})
			return
		case <-cancelledCh:
			// Process was killed (context cancelled) -- send state change and EOF
			statePayload, _ := json.Marshal(map[string]any{"pid": req.PID, "state": "killed"})
			_ = enc.Encode(GdbEvent{Type: StreamGdbStateChange, Payload: statePayload})
			_ = enc.Encode(GdbEvent{Type: StreamEOF})
			return
		case <-s.done:
			return
		}
	}
}

// handleDetachGdb handles detach requests sent on a separate connection.
func (s *Server) handleDetachGdb(conn net.Conn, rawPayload json.RawMessage) {
	var req DetachGdbRequest
	if err := json.Unmarshal(rawPayload, &req); err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID", Message: "invalid detach_gdb request"}})
		return
	}

	s.gdbMu.Lock()
	ch, ok := s.gdbDetachCh[req.PID]
	if ok {
		close(ch)
		delete(s.gdbDetachCh, req.PID)
	}
	s.gdbMu.Unlock()

	writeResponse(conn, Response{OK: true})
}

// handleGdbCommand dispatches gdb commands (break, delete, continue, info) to the kernel.
func (s *Server) handleGdbCommand(conn net.Conn, rawPayload json.RawMessage) {
	var req GdbCommandRequest
	if err := json.Unmarshal(rawPayload, &req); err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID", Message: "invalid gdb_command request"}})
		return
	}

	proc, ok := s.kern.GetProcess(req.PID)
	if !ok {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "NOT_FOUND", Message: fmt.Sprintf("process %d not found", req.PID)}})
		return
	}

	var gcr GdbCommandResponse
	switch req.Command {
	case "break":
		gcr = s.handleGdbBreak(proc, req.Args)
	case "delete":
		gcr = s.handleGdbDelete(proc, req.Args)
	case "continue":
		proc.GdbResume()
		gcr = GdbCommandResponse{OK: true, Message: "resumed"}
	case "info":
		gcr = s.handleGdbInfo(proc, req.Args)
	case "step":
		gcr = s.handleGdbStep(proc, req.Args)
	case "inspect":
		gcr = s.handleGdbInspect(proc, req.Args)
	case "set":
		gcr = s.handleGdbSet(proc, req.Args)
	default:
		gcr = GdbCommandResponse{OK: false, Message: fmt.Sprintf("unknown gdb command: %s", req.Command)}
	}

	payload, _ := json.Marshal(gcr)
	writeResponse(conn, Response{OK: true, Payload: payload})
}

// handleGdbBreak creates a breakpoint from parsed args.
func (s *Server) handleGdbBreak(proc *kernel.Process, args []string) GdbCommandResponse {
	if len(args) == 0 {
		return GdbCommandResponse{OK: false, Message: "usage: break <type> [args...]"}
	}

	bp, err := parseBreakpointArgs(args)
	if err != nil {
		return GdbCommandResponse{OK: false, Message: err.Error()}
	}

	id := proc.AddBreakpoint(bp)
	return GdbCommandResponse{OK: true, Message: fmt.Sprintf("breakpoint %d set", id), Data: map[string]any{"bp_id": id}}
}

// handleGdbDelete removes a breakpoint by ID.
func (s *Server) handleGdbDelete(proc *kernel.Process, args []string) GdbCommandResponse {
	if len(args) == 0 {
		return GdbCommandResponse{OK: false, Message: "usage: delete <bp_id>"}
	}
	id, err := strconv.Atoi(args[0])
	if err != nil {
		return GdbCommandResponse{OK: false, Message: fmt.Sprintf("invalid breakpoint ID: %s", args[0])}
	}
	if proc.RemoveBreakpoint(id) {
		return GdbCommandResponse{OK: true, Message: fmt.Sprintf("breakpoint %d deleted", id)}
	}
	return GdbCommandResponse{OK: false, Message: fmt.Sprintf("breakpoint %d not found", id)}
}

// handleGdbInfo returns breakpoint information.
func (s *Server) handleGdbInfo(proc *kernel.Process, args []string) GdbCommandResponse {
	// Default to "breakpoints" if no args or explicit "breakpoints"/"bp"
	if len(args) > 0 && args[0] != "breakpoints" && args[0] != "bp" {
		return GdbCommandResponse{OK: false, Message: "usage: info breakpoints|bp"}
	}
	bps := proc.ListBreakpoints()
	bpList := make([]map[string]any, len(bps))
	for i, bp := range bps {
		bpList[i] = map[string]any{
			"id":        bp.ID,
			"type":      bpTypeString(bp.Type),
			"enabled":   bp.Enabled,
			"hit_count": bp.HitCount,
			"condition": bpConditionString(bp),
		}
	}
	return GdbCommandResponse{OK: true, Data: bpList}
}

// handleGdbStep sets step mode and resumes the process.
func (s *Server) handleGdbStep(proc *kernel.Process, args []string) GdbCommandResponse {
	if len(args) == 0 {
		return GdbCommandResponse{OK: false, Message: "usage: step <syscall|reasoning>"}
	}
	mode := args[0]

	switch mode {
	case "syscall":
		proc.SetStepMode(kernel.StepSyscall)
	case "reasoning":
		proc.SetStepMode(kernel.StepReasoning)
	default:
		return GdbCommandResponse{OK: false, Message: fmt.Sprintf("unknown step mode: %s (valid: syscall, reasoning)", mode)}
	}

	proc.GdbResume()
	return GdbCommandResponse{OK: true, Message: fmt.Sprintf("stepping %s", mode)}
}

// handleGdbInspect handles the inspect command to examine process state.
func (s *Server) handleGdbInspect(proc *kernel.Process, args []string) GdbCommandResponse {
	if len(args) == 0 {
		return GdbCommandResponse{OK: false, Message: "usage: inspect <context|ctx>"}
	}

	subCmd := args[0]
	switch subCmd {
	case "context", "ctx":
		if s.ctxMgr == nil {
			return GdbCommandResponse{OK: false, Message: "context manager not available"}
		}
		info, err := s.ctxMgr.GetContextInfo(proc.CtxID)
		if err != nil {
			return GdbCommandResponse{OK: false, Message: fmt.Sprintf("inspect context failed: %v", err)}
		}
		info["pid"] = proc.PID
		info["ctx_id"] = proc.CtxID
		totalMsgs, _ := info["total_messages"].(int)
		totalTokens, _ := info["total_tokens"].(int)
		return GdbCommandResponse{
			OK:      true,
			Message: fmt.Sprintf("context: %d messages, ~%d tokens", totalMsgs, totalTokens),
			Data:    info,
		}
	default:
		return GdbCommandResponse{OK: false, Message: fmt.Sprintf("unknown inspect target: %s (valid: context, ctx)", subCmd)}
	}
}

// handleGdbSet handles the set command for runtime parameter hot modification.
func (s *Server) handleGdbSet(proc *kernel.Process, args []string) GdbCommandResponse {
	if len(args) < 2 {
		return GdbCommandResponse{OK: false, Message: "usage: set <model|context|skills|env> <args...>"}
	}
	subCmd := args[0]
	switch subCmd {
	case "model":
		proc.SetGdbModelOverride(args[1])
		return GdbCommandResponse{OK: true, Message: fmt.Sprintf("model set to %s", args[1])}
	case "context":
		if len(args) < 3 {
			return GdbCommandResponse{OK: false, Message: "usage: set context append <text>"}
		}
		if args[1] != "append" {
			return GdbCommandResponse{OK: false, Message: "usage: set context append <text>"}
		}
		content := strings.Join(args[2:], " ")
		if s.ctxMgr == nil {
			return GdbCommandResponse{OK: false, Message: "context manager not available"}
		}
		if err := s.ctxMgr.AppendMessage(proc.CtxID, rnixctx.RoleUser, content); err != nil {
			return GdbCommandResponse{OK: false, Message: fmt.Sprintf("context append failed: %v", err)}
		}
		return GdbCommandResponse{OK: true, Message: "context updated"}
	case "skills":
		if len(args) < 3 || args[1] != "add" {
			return GdbCommandResponse{OK: false, Message: "usage: set skills add <name>"}
		}
		skillName := args[2]
		proc.AddGdbSkill(skillName)
		// Hot-load skill body into context if skill loader and context manager are available
		if s.skillLoader != nil && s.ctxMgr != nil {
			info, err := s.skillLoader.LoadFull(skillName)
			if err != nil {
				return GdbCommandResponse{OK: true, Message: fmt.Sprintf("skill %s recorded (load failed: %v)", skillName, err)}
			}
			if info.Body != "" {
				if err := s.ctxMgr.AppendMessage(proc.CtxID, rnixctx.RoleUser, fmt.Sprintf("[Skill: %s]\n%s", skillName, info.Body)); err != nil {
					return GdbCommandResponse{OK: true, Message: fmt.Sprintf("skill %s recorded (context append failed: %v)", skillName, err)}
				}
				return GdbCommandResponse{OK: true, Message: fmt.Sprintf("skill %s loaded and injected into context", skillName)}
			}
		}
		return GdbCommandResponse{OK: true, Message: fmt.Sprintf("skill %s added", skillName)}
	case "env":
		kv := args[1]
		idx := strings.Index(kv, "=")
		if idx <= 0 {
			return GdbCommandResponse{OK: false, Message: "usage: set env KEY=VALUE"}
		}
		proc.SetGdbEnv(kv[:idx], kv[idx+1:])
		return GdbCommandResponse{OK: true, Message: fmt.Sprintf("env %s set", kv[:idx])}
	default:
		return GdbCommandResponse{OK: false, Message: fmt.Sprintf("unknown set target: %s", subCmd)}
	}
}

// parseBreakpointArgs parses "break" command arguments into a Breakpoint.
func parseBreakpointArgs(args []string) (*kernel.Breakpoint, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("missing breakpoint type")
	}
	bpType := args[0]
	switch bpType {
	case "syscall":
		if len(args) < 2 {
			return nil, fmt.Errorf("usage: break syscall <name>")
		}
		return &kernel.Breakpoint{
			Type:      kernel.BPSyscall,
			Enabled:   true,
			Condition: &kernel.SyscallCondition{Name: args[1]},
		}, nil
	case "reasoning":
		return &kernel.Breakpoint{
			Type:      kernel.BPReasoning,
			Enabled:   true,
			Condition: &kernel.ReasoningCondition{},
		}, nil
	case "quality":
		return parseQualityBreakpoint(args[1:])
	case "budget":
		if len(args) < 2 {
			return nil, fmt.Errorf("usage: break budget <tokens>")
		}
		threshold, err := strconv.Atoi(args[1])
		if err != nil {
			return nil, fmt.Errorf("invalid budget threshold: %s", args[1])
		}
		return &kernel.Breakpoint{
			Type:      kernel.BPBudget,
			Enabled:   true,
			Condition: &kernel.BudgetCondition{Threshold: threshold},
		}, nil
	default:
		return nil, fmt.Errorf("unknown breakpoint type: %s (valid: syscall, reasoning, quality, budget)", bpType)
	}
}

// parseQualityBreakpoint parses quality breakpoint flags.
func parseQualityBreakpoint(args []string) (*kernel.Breakpoint, error) {
	if len(args) < 2 {
		return nil, fmt.Errorf("usage: break quality --pattern <pattern> | --eval <criteria>")
	}
	switch args[0] {
	case "--pattern":
		re, err := regexp.Compile(args[1])
		if err != nil {
			return nil, fmt.Errorf("invalid pattern: %v", err)
		}
		return &kernel.Breakpoint{
			Type:    kernel.BPQuality,
			Enabled: true,
			Condition: &kernel.QualityCondition{
				Mode:    kernel.QualityModePattern,
				Pattern: re,
			},
		}, nil
	case "--eval":
		return &kernel.Breakpoint{
			Type:    kernel.BPQuality,
			Enabled: true,
			Condition: &kernel.QualityCondition{
				Mode:     kernel.QualityModeEval,
				EvalExpr: args[1],
			},
		}, nil
	default:
		return nil, fmt.Errorf("unknown quality flag: %s (valid: --pattern, --eval)", args[0])
	}
}

// bpTypeString returns a human-readable name for a breakpoint type.
func bpTypeString(t kernel.BreakpointType) string {
	switch t {
	case kernel.BPSyscall:
		return "syscall"
	case kernel.BPReasoning:
		return "reasoning"
	case kernel.BPQuality:
		return "quality"
	case kernel.BPBudget:
		return "budget"
	default:
		return fmt.Sprintf("unknown(%d)", t)
	}
}

// bpConditionString returns a human-readable description of a breakpoint's condition.
func bpConditionString(bp *kernel.Breakpoint) string {
	if bp.Condition == nil {
		return ""
	}
	switch c := bp.Condition.(type) {
	case *kernel.SyscallCondition:
		return c.Name
	case *kernel.ReasoningCondition:
		return "every step"
	case *kernel.QualityCondition:
		if c.Mode == kernel.QualityModePattern && c.Pattern != nil {
			return fmt.Sprintf("pattern: %s", c.Pattern.String())
		}
		return fmt.Sprintf("eval: %s", c.EvalExpr)
	case *kernel.BudgetCondition:
		return fmt.Sprintf(">= %d tokens", c.Threshold)
	default:
		return "custom"
	}
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

func (m *callbackMux) OnSpawn(pid types.PID, intent, provider, model, procUUID string) {
	pp := ProgressPayload{Event: "spawn", PID: pid, Intent: intent, Provider: provider, Model: model, UUID: procUUID}
	payload, _ := json.Marshal(pp)
	m.send(pid, StreamEvent{Type: StreamProgress, Payload: payload})
}

func (m *callbackMux) OnStep(pid types.PID, step int, total int) {
	pp := ProgressPayload{Event: "step", PID: pid, Step: step, Total: total}
	payload, _ := json.Marshal(pp)
	m.send(pid, StreamEvent{Type: StreamProgress, Payload: payload})
}

func (m *callbackMux) OnStepComplete(pid types.PID, step int, action string, summary string, hasError bool, durationMs float64) {
	pp := ProgressPayload{Event: "step_complete", PID: pid, Step: step, Action: action, Summary: summary, HasError: hasError, DurationMs: durationMs}
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

// SetContextManager sets the context manager for inspect context support.
func (s *Server) SetContextManager(mgr *rnixctx.Manager) {
	s.ctxMgr = mgr
}

// SetSkillLoader sets the skill loader for gdb set skills hot-loading.
func (s *Server) SetSkillLoader(loader *skills.SkillLoader) {
	s.skillLoader = loader
}

// SetIntentManager injects the intent manager into the server.
func (s *Server) SetIntentManager(mgr intentManager) {
	s.intentMgr = mgr
}

// SetProviderStatusFunc injects the provider status query function.
func (s *Server) SetProviderStatusFunc(fn func() []ProviderStatusWire) {
	s.providerStatuses = fn
}

// globalProviderNames returns the set of provider names from the global providers config.
func (s *Server) globalProviderNames() map[string]struct{} {
	names := make(map[string]struct{})
	if s.providerStatuses != nil {
		for _, w := range s.providerStatuses() {
			names[w.Name] = struct{}{}
		}
	}
	return names
}

// SetReputationStore sets the reputation store for reputation queries (Story 21.3).
func (s *Server) SetReputationStore(rs *kernel.ReputationStore) {
	s.reputationStore = rs
}

// SetSynergyMatrix sets the synergy matrix for synergy list queries (Story 21.5).
func (s *Server) SetSynergyMatrix(m *kernel.SynergyMatrix) {
	s.synergyMatrix = m
}

// SetImmuneDaemon sets the immune daemon for status queries (Story 22.1).
func (s *Server) SetImmuneDaemon(d *kernel.ImmuneDaemon) {
	s.immuneDaemon = d
}

// SetGlobalConfig sets the global configuration used for project-level config merging.
func (s *Server) SetGlobalConfig(gc *config.GlobalConfig) {
	s.globalConfig = gc
}

// SetGlobalProvidersRaw caches the raw YAML bytes from the global providers.yaml file.
// Used for DeepMergeYAML with project-level providers.yaml.
func (s *Server) SetGlobalProvidersRaw(raw []byte) {
	s.globalProvidersRaw = raw
}

// resolveProjectContext builds a ProjectConfig and project-aware agent loader for a spawn request.
// If projectDir is empty, returns (nil, s.agentLoader, nil) — pure global mode.
// If projectDir is non-empty, loads .env files, merges project providers with global,
// creates project-level LLM drivers, and builds project-aware loaders.
func (s *Server) resolveProjectContext(projectDir, rnixEnv string) (*config.ProjectConfig, AgentLoaderFunc, error) {
	if projectDir == "" {
		return nil, s.agentLoader, nil
	}

	gc := s.globalConfig
	if gc == nil {
		return nil, s.agentLoader, nil
	}

	// F-01: Sanitize projectDir — clean path and require absolute
	projectDir = filepath.Clean(projectDir)
	if !filepath.IsAbs(projectDir) {
		return nil, s.agentLoader, nil
	}

	// Safety: verify projectDir contains a .rnix/ subdirectory
	if _, err := os.Stat(filepath.Join(projectDir, ".rnix")); err != nil {
		// No .rnix/ directory — fall back to global mode
		return nil, s.agentLoader, nil
	}

	// F-02: Validate rnixEnv early (also validated in LoadDotenvDir, but defense-in-depth)
	if rnixEnv != "" && !config.ValidateEnvName(rnixEnv) {
		return nil, nil, fmt.Errorf("invalid RNIX_ENV value: %q", rnixEnv)
	}

	// Build search directories (project first, global second)
	projectAgentsDir := filepath.Join(projectDir, ".rnix", "agents")
	projectSkillsDir := filepath.Join(projectDir, ".rnix", "skills")
	agentDirs := []string{projectAgentsDir, gc.AgentsDir}
	skillDirs := []string{projectSkillsDir, gc.SkillsDir}

	// Load .env files from project directory
	dotenvVars, dotenvErr := config.LoadDotenvDir(projectDir, rnixEnv)
	if dotenvErr != nil {
		return nil, nil, fmt.Errorf("loading .env files from %s: %w", projectDir, dotenvErr)
	}
	envLookup := config.NewEnvLookup(dotenvVars)

	// Merge project providers.yaml with global (raw YAML bytes → DeepMergeYAML)
	projectProvidersPath := filepath.Join(projectDir, ".rnix", "providers.yaml")
	var mergedProvidersCfg *llm.ProvidersConfig

	if _, statErr := os.Stat(projectProvidersPath); statErr == nil {
		projectRaw, readErr := os.ReadFile(projectProvidersPath)
		if readErr != nil {
			return nil, nil, fmt.Errorf("reading project providers %s: %w", projectProvidersPath, readErr)
		}

		if len(s.globalProvidersRaw) > 0 && len(projectRaw) > 0 {
			var globalMap, projectMap map[string]any
			if unmarshalErr := yaml.Unmarshal(s.globalProvidersRaw, &globalMap); unmarshalErr != nil {
				return nil, nil, fmt.Errorf("parsing global providers YAML: %w", unmarshalErr)
			}
			if unmarshalErr := yaml.Unmarshal(projectRaw, &projectMap); unmarshalErr != nil {
				return nil, nil, fmt.Errorf("parsing project providers YAML: %w", unmarshalErr)
			}

			merged := config.DeepMergeYAML(globalMap, projectMap)

			if baseSlice, ok := globalMap["providers"].([]any); ok {
				if overSlice, ok := projectMap["providers"].([]any); ok {
					merged["providers"] = config.MergeNamedSlice(baseSlice, overSlice, "name")
				}
			}

			mergedBytes, marshalErr := yaml.Marshal(merged)
			if marshalErr != nil {
				return nil, nil, fmt.Errorf("marshaling merged providers: %w", marshalErr)
			}

			var parseErr error
			mergedProvidersCfg, parseErr = llm.ParseProvidersConfig(mergedBytes)
			if parseErr != nil {
				return nil, nil, fmt.Errorf("validating merged providers config: %w", parseErr)
			}
		} else {
			// No global raw bytes — just load project config directly
			var loadErr error
			mergedProvidersCfg, loadErr = llm.LoadProvidersConfig(projectProvidersPath)
			if loadErr != nil {
				return nil, nil, fmt.Errorf("project providers config %s: %w", projectProvidersPath, loadErr)
			}
		}
	}

	// F-12: If no project providers.yaml, shallow-copy global config to preserve immutability
	if mergedProvidersCfg == nil {
		if gcProviders, ok := gc.Providers.(*llm.ProvidersConfig); ok && gcProviders != nil {
			copied := *gcProviders
			mergedProvidersCfg = &copied
		}
	}

	// Build ProjectConfig snapshot
	projCfg := &config.ProjectConfig{
		ProjectDir:  projectDir,
		Providers:   mergedProvidersCfg,
		AgentDirs:   agentDirs,
		SkillDirs:   skillDirs,
		EnvSnapshot: dotenvVars,
	}

	// Create project-level LLM drivers and LLMFileOpener if we have merged providers
	if mergedProvidersCfg != nil {
		factories := make(map[string]vfs.VFSFileFactory)
		var projectOnlyProviders []ProviderStatusWire
		for _, pc := range mergedProvidersCfg.Providers {
			driver, driverErr := llm.CreateDriverWithEnv(pc, envLookup)
			if driverErr != nil {
				log.Printf("[ipc] warning: project provider %q: %v", pc.Name, driverErr)
				continue
			}
			factories[pc.Name] = llm.FileFactory(driver, "/dev/llm/"+pc.Name, pc.Mode)
		}

		// Track project-only providers (defined in project providers.yaml but not in global)
		if projectRawProviders := mergedProvidersCfg.Providers; len(projectRawProviders) > 0 {
			globalNames := s.globalProviderNames()
			for _, pc := range projectRawProviders {
				if _, isGlobal := globalNames[pc.Name]; !isGlobal {
					projectOnlyProviders = append(projectOnlyProviders, ProviderStatusWire{
						Name:   pc.Name,
						Driver: pc.Driver,
						Health: "unchecked",
						Source: "project",
					})
				}
			}
			if len(projectOnlyProviders) > 0 {
				s.projectProviders.Store(projectDir, projectOnlyProviders)
			}
		}

		if len(factories) > 0 {
			projCfg.LLMFileOpener = func(provider string, flags int) (any, error) {
				factory, ok := factories[provider]
				if !ok {
					return nil, fmt.Errorf("project provider %q not found", provider)
				}
				return factory("", vfs.OpenFlag(flags), projectDir)
			}
		}

		projCfg.DefaultProvider = mergedProvidersCfg.ResolveDefaultProvider()
	}

	// Create project-aware skill and agent loaders
	projectSkillLoader := skills.NewSkillLoader(skillDirs)
	projectAgentLoader := agents.NewAgentLoader(agentDirs, projectSkillLoader, nil)

	return projCfg, projectAgentLoader.Load, nil
}

func (s *Server) handleSynergyList(conn net.Conn) {
	if s.synergyMatrix == nil {
		resp := SynergyListResponse{Combos: []kernel.ComboSummary{}}
		respPayload, _ := json.Marshal(resp)
		writeResponse(conn, Response{OK: true, Payload: respPayload})
		return
	}

	summaries, err := s.synergyMatrix.GetComboSummaries()
	if err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INTERNAL", Message: err.Error()}})
		return
	}

	if summaries == nil {
		summaries = []kernel.ComboSummary{}
	}
	resp := SynergyListResponse{Combos: summaries}
	respPayload, _ := json.Marshal(resp)
	writeResponse(conn, Response{OK: true, Payload: respPayload})
}

func (s *Server) handleImmuneStatus(conn net.Conn) {
	if s.immuneDaemon == nil {
		resp := ImmuneStatusResponse{
			Running:        false,
			Profiles:       map[string]*kernel.NormalProfile{},
			ActivePIDs:     []uint64{},
			SuspendedPIDs:  []uint64{},
			Alerts:         []AlertWire{},
			SecurityStatus: "ok",
		}
		respPayload, _ := json.Marshal(resp)
		writeResponse(conn, Response{OK: true, Payload: respPayload})
		return
	}

	profiles := s.immuneDaemon.GetAllProfiles()
	if profiles == nil {
		profiles = map[string]*kernel.NormalProfile{}
	}
	rawPIDs := s.immuneDaemon.ActivePIDs()
	activePIDs := make([]uint64, len(rawPIDs))
	for i, p := range rawPIDs {
		activePIDs[i] = uint64(p)
	}

	// Story 22.2: include alerts and threat count
	rawAlerts := s.immuneDaemon.GetAlerts()
	alertWires := make([]AlertWire, 0, len(rawAlerts))
	for _, a := range rawAlerts {
		alertWires = append(alertWires, AlertWire{
			PID:           uint64(a.PID),
			AgentTemplate: a.AgentTemplate,
			Type:          string(a.Type),
			Detail:        a.Detail,
			Deviation:     a.Deviation,
			TimestampMs:   a.Timestamp.UnixMilli(),
		})
	}
	threats := s.immuneDaemon.GetThreats()

	// Story 22.3: uptime, suspended PIDs, security status
	uptimeMs := s.immuneDaemon.Uptime().Milliseconds()
	rawSuspended := s.immuneDaemon.SuspendedPIDs()
	suspendedPIDs := make([]uint64, len(rawSuspended))
	for i, p := range rawSuspended {
		suspendedPIDs[i] = uint64(p)
	}

	securityStatus := "ok"
	if len(alertWires) > 0 || len(suspendedPIDs) > 0 {
		securityStatus = "warning"
	}

	resp := ImmuneStatusResponse{
		Running:        s.immuneDaemon.IsRunning(),
		UptimeMs:       uptimeMs,
		ProfileCount:   len(profiles),
		Profiles:       profiles,
		ActivePIDs:     activePIDs,
		SuspendedPIDs:  suspendedPIDs,
		Alerts:         alertWires,
		ThreatCount:    len(threats),
		SecurityStatus: securityStatus,
	}
	respPayload, _ := json.Marshal(resp)
	writeResponse(conn, Response{OK: true, Payload: respPayload})
}

func (s *Server) handleImmuneResume(conn net.Conn, rawPayload json.RawMessage) {
	var req ImmuneResumeRequest
	if err := json.Unmarshal(rawPayload, &req); err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID", Message: "invalid immune_resume request"}})
		return
	}

	pid := types.PID(req.PID)

	// Send SIGRESUME to the process
	if s.kern != nil {
		if err := s.kern.Kill(pid, types.SIGRESUME); err != nil {
			writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "NOT_FOUND", Message: fmt.Sprintf("failed to resume PID %d: %v", req.PID, err)}})
			return
		}
	}

	// Clear the alert in the immune daemon
	if s.immuneDaemon != nil {
		s.immuneDaemon.ClearAlert(pid)
	}

	result := ImmuneResumeResponse{
		OK:      true,
		Message: fmt.Sprintf("Process %d resumed successfully.", req.PID),
	}
	respPayload, _ := json.Marshal(result)
	writeResponse(conn, Response{OK: true, Payload: respPayload})
}

// handleSimilarityQuery returns similar agents for the given agent (Story 22.4).
func (s *Server) handleSimilarityQuery(conn net.Conn, rawPayload json.RawMessage) {
	var req SimilarityQueryRequest
	if err := json.Unmarshal(rawPayload, &req); err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID", Message: "invalid similarity_query request"}})
		return
	}

	var similarities []kernel.CapabilitySimilarity
	if s.immuneDaemon != nil {
		similarities = s.immuneDaemon.GetSimilarAgents(req.AgentName, req.MinScore)
	}
	if similarities == nil {
		similarities = []kernel.CapabilitySimilarity{}
	}

	resp := SimilarityQueryResponse{
		Agent:        req.AgentName,
		Similarities: similarities,
	}
	respPayload, _ := json.Marshal(resp)
	writeResponse(conn, Response{OK: true, Payload: respPayload})
}

// handleTopologyQuery returns the collaboration topology (Story 22.5).
func (s *Server) handleTopologyQuery(conn net.Conn) {
	var topo *kernel.CollaborationTopology
	if s.immuneDaemon != nil {
		topo = s.immuneDaemon.GetTopology()
	}

	resp := TopologyQueryResponse{
		Nodes:           []kernel.TopologyNode{},
		Edges:           []kernel.CooperationEdge{},
		ReinforcedPaths: []kernel.CooperationEdge{},
	}
	if topo != nil {
		if topo.Nodes != nil {
			resp.Nodes = topo.Nodes
		}
		if topo.Edges != nil {
			resp.Edges = topo.Edges
		}
		if topo.ReinforcedPaths != nil {
			resp.ReinforcedPaths = topo.ReinforcedPaths
		}
	}

	respPayload, _ := json.Marshal(resp)
	writeResponse(conn, Response{OK: true, Payload: respPayload})
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

	proc, procFound := s.kern.GetProcess(req.PID)
	if procFound {
		systemPrompt = proc.GetFinalSystemPrompt()
		toolDefs = proc.GetNativeToolDefs()
		stepsPath = s.resolveStepsPathFromProc(proc, req.PID)
	} else {
		// Process not in memory — try disk
		stepsPath = s.resolveStepsPathFallback(req.PID)
		if stepsPath == "" {
			writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "not_found", Message: fmt.Sprintf("process %d not found", req.PID)}})
			return
		}
		metaPath := filepath.Join(filepath.Dir(stepsPath), "process-meta.json")
		meta, err := readProcessMeta(metaPath)
		if err != nil {
			writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "not_found", Message: fmt.Sprintf("process %d not found", req.PID)}})
			return
		}
		systemPrompt = meta.SystemPrompt
		toolDefs = meta.ToolDefs
	}

	if stepsPath == "" {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "not_found", Message: fmt.Sprintf("process %d not found", req.PID)}})
		return
	}

	// Phase B: read StepRecord
	rec, err := kernel.ReadStep(stepsPath, req.Step)
	if err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "not_found", Message: fmt.Sprintf("step %d not yet recorded", req.Step)}})
		return
	}

	// Phase C: assemble response
	resp := GetStepDetailResponse{
		SystemPrompt:   systemPrompt,
		Tools:          toolDefsToWire(toolDefs),
		Step:           rec.Step,
		Messages:       messagesToWire(rec.Messages),
		MessageCount:   rec.MessageCount,
		TokenCount:     rec.TokenCount,
		RawResponse:    rec.RawResponse,
		Action:         rec.Action,
		Summary:        rec.Summary,
		ToolPath:       rec.ToolPath,
		ToolInput:      rec.ToolInput,
		ToolResult:     rec.ToolResult,
		ToolError:      rec.ToolError,
		ToolDurationMs: float64(rec.ToolDuration.Microseconds()) / 1000.0,
		RequestTokens:  rec.RequestTokens,
		ResponseTokens: rec.ResponseTokens,
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

	var stepsPath string
	proc, procFound := s.kern.GetProcess(req.PID)
	if procFound {
		stepsPath = s.resolveStepsPathFromProc(proc, req.PID)
	} else {
		stepsPath = s.resolveStepsPathFallback(req.PID)
	}

	if stepsPath == "" {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "not_found", Message: fmt.Sprintf("process %d not found", req.PID)}})
		return
	}

	records, total, err := kernel.ReadAllSteps(stepsPath, req.AfterStep)
	if err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "not_found", Message: fmt.Sprintf("process %d not found", req.PID)}})
		return
	}

	wires := make([]StepSummaryWire, len(records))
	for i, r := range records {
		wires[i] = StepSummaryWire{
			Step:       r.Step,
			Action:     r.Action,
			Summary:    r.Summary,
			HasError:   r.ToolError != "",
			DurationMs: float64(r.ToolDuration.Microseconds()) / 1000.0,
		}
	}

	respPayload, _ := json.Marshal(ListStepsResponse{Steps: wires, Total: total})
	writeResponse(conn, Response{OK: true, Payload: respPayload})
}

// handleGetProcDetail returns full process detail including env, skills, FD table, context stats (Story 27.6).
func (s *Server) handleGetProcDetail(conn net.Conn, rawPayload json.RawMessage) {
	var req GetProcDetailRequest
	if err := json.Unmarshal(rawPayload, &req); err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "invalid_request", Message: "invalid get_proc_detail request"}})
		return
	}

	proc, ok := s.kern.GetProcess(req.PID)
	if !ok {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "not_found", Message: fmt.Sprintf("process %d not found", req.PID)}})
		return
	}

	// Thread-safe snapshots
	snap := proc.GetDetailSnapshot()
	fdSnap := proc.GetFDSnapshot()

	resp := GetProcDetailResponse{
		PID:            snap.PID,
		UUID:           snap.UUID,
		PPID:           snap.PPID,
		State:          snap.State.String(),
		Intent:         snap.Intent,
		Provider:       snap.Provider,
		Model:          snap.Model,
		CreatedAtMs:    snap.CreatedAt.UnixMilli(),
		AllowedDevices: snap.AllowedDevices,
	}
	if !snap.DeadAt.IsZero() {
		resp.DeadAtMs = snap.DeadAt.UnixMilli()
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
	if snap.ContextBudget > 0 {
		resp.ContextStats.UsagePct = float64(snap.TokensUsed) * 100.0 / float64(snap.ContextBudget)
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
	}

	payload, err := json.Marshal(resp)
	if err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "internal", Message: fmt.Sprintf("marshal get_proc_detail: %v", err)}})
		return
	}
	writeResponse(conn, Response{OK: true, Payload: payload})
}

// isSensitiveEnvKey returns true if the key name suggests it holds a secret.
func isSensitiveEnvKey(key string) bool {
	upper := strings.ToUpper(key)
	for _, s := range []string{"KEY", "SECRET", "TOKEN", "PASSWORD"} {
		if strings.Contains(upper, s) {
			return true
		}
	}
	return false
}

// resolveStepsPathFromProc resolves the steps.jsonl path from a living process using UUID.
func (s *Server) resolveStepsPathFromProc(proc *kernel.Process, pid types.PID) string {
	procUUID := proc.UUID
	pc := proc.GetProjectConfig()
	if pc != nil && pc.ProjectDir != "" {
		return filepath.Join(pc.ProjectDir, ".rnix", "data", "steps", procUUID, "steps.jsonl")
	}
	base := s.kern.GetStepDataDir()
	if base == "" {
		return ""
	}
	return filepath.Join(base, "data", "steps", procUUID, "steps.jsonl")
}

// resolveStepsPathFallback resolves steps.jsonl path when process is not in memory.
// Tries UUID directories first (scanning process-meta.json for PID match), then legacy PID path.
func (s *Server) resolveStepsPathFallback(pid types.PID) string {
	base := s.kern.GetStepDataDir()
	if base == "" {
		return ""
	}
	stepsBase := filepath.Join(base, "data", "steps")

	entries, err := os.ReadDir(stepsBase)
	if err == nil {
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			name := e.Name()
			if !isUUIDDir(name) {
				continue
			}
			metaPath := filepath.Join(stepsBase, name, "process-meta.json")
			metaData, err := os.ReadFile(metaPath)
			if err != nil {
				continue
			}
			var meta struct {
				PID types.PID `json:"pid"`
			}
			if json.Unmarshal(metaData, &meta) == nil && meta.PID == pid {
				return filepath.Join(stepsBase, name, "steps.jsonl")
			}
		}
	}

	// Legacy PID fallback
	pidStr := fmt.Sprintf("%d", pid)
	legacyPath := filepath.Join(stepsBase, pidStr, "steps.jsonl")
	if _, err := os.Stat(legacyPath); err == nil {
		return legacyPath
	}
	return ""
}

type processMetaFile struct {
	PID          types.PID     `json:"pid,omitempty"`
	SystemPrompt string        `json:"system_prompt"`
	ToolDefs     []vfs.ToolDef `json:"tool_defs"`
}

func readProcessMeta(path string) (*processMetaFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var meta processMetaFile
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, err
	}
	return &meta, nil
}

func isUUIDDir(name string) bool {
	_, err := uuid.Parse(name)
	return err == nil
}

func toolDefsToWire(defs []vfs.ToolDef) []ToolDefWire {
	if defs == nil {
		return []ToolDefWire{}
	}
	wires := make([]ToolDefWire, len(defs))
	for i, d := range defs {
		wires[i] = ToolDefWire{
			Name:        d.Name,
			Description: d.Description,
			Parameters:  d.Parameters,
		}
	}
	return wires
}

func messagesToWire(raw json.RawMessage) []MessageWire {
	if len(raw) == 0 {
		return []MessageWire{}
	}
	var msgs []rnixctx.Message
	if err := json.Unmarshal(raw, &msgs); err != nil {
		return []MessageWire{}
	}
	wires := make([]MessageWire, len(msgs))
	for i, m := range msgs {
		wm := MessageWire{
			Role:       string(m.Role),
			Content:    m.Content,
			ToolCallID: m.ToolCallID,
		}
		if len(m.ToolCalls) > 0 {
			wm.ToolCalls = make([]ToolCallWire, len(m.ToolCalls))
			for j, tc := range m.ToolCalls {
				wm.ToolCalls[j] = ToolCallWire{
					ID:    tc.ID,
					Name:  tc.Name,
					Input: tc.Input,
				}
			}
		}
		wires[i] = wm
	}
	return wires
}

// handleRecordStart starts execution recording for a process.
func (s *Server) handleRecordStart(conn net.Conn, rawPayload json.RawMessage) {
	var req RecordStartRequest
	if err := json.Unmarshal(rawPayload, &req); err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID", Message: "invalid record_start request"}})
		return
	}

	recordID, err := s.kern.StartRecording(req.PID)
	if err != nil {
		code := "INTERNAL"
		var sysErr *kernel.SyscallError
		if errors.As(err, &sysErr) {
			code = string(sysErr.Code)
		}
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: code, Message: err.Error()}})
		return
	}

	payload, _ := json.Marshal(RecordStartResponse{RecordID: recordID})
	writeResponse(conn, Response{OK: true, Payload: payload})
}

// handleRecordStop stops execution recording for a process.
func (s *Server) handleRecordStop(conn net.Conn, rawPayload json.RawMessage) {
	var req RecordStopRequest
	if err := json.Unmarshal(rawPayload, &req); err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID", Message: "invalid record_stop request"}})
		return
	}

	// Get event count before stopping
	var eventCount uint64
	if mgr := s.kern.GetRecordManager(); mgr != nil {
		eventCount = mgr.GetEventCount(req.PID)
	}

	if err := s.kern.StopRecording(req.PID); err != nil {
		code := "INTERNAL"
		var sysErr *kernel.SyscallError
		if errors.As(err, &sysErr) {
			code = string(sysErr.Code)
		}
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: code, Message: err.Error()}})
		return
	}

	payload, _ := json.Marshal(RecordStopResponse{EventCount: eventCount})
	writeResponse(conn, Response{OK: true, Payload: payload})
}

// handleRecordList lists all recorded sessions.
func (s *Server) handleRecordList(conn net.Conn) {
	mgr := s.kern.GetRecordManager()
	if mgr == nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INTERNAL", Message: "record manager not initialized"}})
		return
	}

	records, err := mgr.ListRecords()
	if err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INTERNAL", Message: err.Error()}})
		return
	}

	wireRecords := make([]RecordMetadataWire, len(records))
	for i, r := range records {
		wireRecords[i] = RecordMetadataWire{
			RecordID:   r.RecordID,
			PID:        r.PID,
			Intent:     r.Intent,
			StartTime:  r.StartTime.UnixMilli(),
			EventCount: r.EventCount,
			Status:     string(r.Status),
		}
		if !r.EndTime.IsZero() {
			wireRecords[i].EndTime = r.EndTime.UnixMilli()
		}
	}

	payload, _ := json.Marshal(RecordListResponse{Records: wireRecords})
	writeResponse(conn, Response{OK: true, Payload: payload})
}

// handleReplayLoad loads a recording and returns its metadata for replay.
func (s *Server) handleReplayLoad(conn net.Conn, rawPayload json.RawMessage) {
	var req ReplayLoadRequest
	if err := json.Unmarshal(rawPayload, &req); err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID", Message: "invalid replay_load request"}})
		return
	}

	mgr := s.kern.GetRecordManager()
	if mgr == nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INTERNAL", Message: "record manager not initialized"}})
		return
	}

	reader, err := mgr.LoadRecord(req.RecordID)
	if err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "NOT_FOUND", Message: err.Error()}})
		return
	}

	meta := reader.Metadata()
	resp := ReplayLoadResponse{
		RecordID:    meta.RecordID,
		PID:         meta.PID,
		Intent:      meta.Intent,
		EventCount:  reader.EventCount(),
		StartTimeMs: meta.StartTime.UnixMilli(),
		Status:      string(meta.Status),
	}
	if !meta.EndTime.IsZero() {
		resp.EndTimeMs = meta.EndTime.UnixMilli()
	}

	payload, _ := json.Marshal(resp)
	writeResponse(conn, Response{OK: true, Payload: payload})
}

// handleForkContinue creates a new process from a fork context (fork-continue).
// Context is pre-allocated and populated with replayed messages, then passed to
// kernel.Spawn via PreallocatedCtxID + SkipReasonLoop to use the standard path.
func (s *Server) handleForkContinue(conn net.Conn, rawPayload json.RawMessage) {
	var req ForkContinueRequest
	if err := json.Unmarshal(rawPayload, &req); err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID", Message: "invalid fork_continue request"}})
		return
	}

	if req.Intent == "" {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INVALID", Message: "intent is required"}})
		return
	}

	// Determine parent PID and validity
	parentPID := types.PID(req.OriginalPID)
	ppidValid := false
	if parentPID > 0 {
		if _, ok := s.kern.GetProcess(parentPID); ok {
			ppidValid = true
		} else {
			parentPID = 0
		}
	}

	// Allocate context and replay messages BEFORE creating the process,
	// so failures here don't leave orphaned process resources.
	var cid types.CtxID
	if s.ctxMgr != nil {
		var err error
		cid, err = s.ctxMgr.CtxAlloc(kernel.DefaultCtxSize)
		if err != nil {
			writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INTERNAL", Message: fmt.Sprintf("CtxAlloc failed: %v", err)}})
			return
		}

		if req.SystemPrompt != "" {
			if err := s.ctxMgr.SetSystemPrompt(cid, req.SystemPrompt); err != nil {
				_ = s.ctxMgr.CtxFree(cid)
				writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INTERNAL", Message: fmt.Sprintf("SetSystemPrompt failed: %v", err)}})
				return
			}
		}

		for i, msg := range req.Messages {
			if msg.Role == "system" {
				continue
			}
			if msg.ToolCallID != "" {
				if err := s.ctxMgr.AppendToolResult(cid, msg.ToolCallID, msg.Content); err != nil {
					log.Printf("[fork_continue] warning: AppendToolResult failed at message %d: %v", i, err)
				}
			} else {
				if err := s.ctxMgr.AppendMessage(cid, rnixctx.Role(msg.Role), msg.Content); err != nil {
					log.Printf("[fork_continue] warning: AppendMessage failed at message %d: %v", i, err)
				}
			}
		}
	}

	// Use standard Spawn path with pre-allocated context and no reason loop
	pid, err := s.kern.Spawn(req.Intent, nil, kernel.SpawnOpts{
		ParentPID:         parentPID,
		PreallocatedCtxID: cid,
		SkipReasonLoop:    true,
	})
	if err != nil {
		if cid != 0 {
			_ = s.ctxMgr.CtxFree(cid)
		}
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "INTERNAL", Message: err.Error()}})
		return
	}

	actualPPID := parentPID
	if !ppidValid {
		actualPPID = 0
	}

	respPayload, _ := json.Marshal(ForkContinueResponse{
		PID:       pid,
		PPID:      actualPPID,
		PPIDValid: ppidValid,
	})
	writeResponse(conn, Response{OK: true, Payload: respPayload})
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

	pid, err := s.kernel.Spawn(intent, agentInfo, kernel.SpawnOpts{Model: model, ProjectConfig: s.projectConfig})
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

	writeResponse(conn, Response{OK: true})

	enc := json.NewEncoder(conn)
	spawner := &ipcKernelSpawner{
		kernel:        s.kern,
		agentLoader:   agentLoaderFn,
		projectConfig: projectCfg,
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

// --- Intent Handlers ---

func (s *Server) handleApplyIntent(conn net.Conn, rawPayload json.RawMessage, connScanner *bufio.Scanner) {
	if s.intentMgr == nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "not_available", Message: "intent manager not initialized"}})
		return
	}

	var req ApplyIntentRequest
	if err := json.Unmarshal(rawPayload, &req); err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "invalid_payload", Message: err.Error()}})
		return
	}

	intentID, treeJSON, err := s.intentMgr.ApplyIntent(context.Background(), req.Intent, req.Model, req.Provider, req.AutoStart)
	if err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "decompose_failed", Message: err.Error()}})
		return
	}

	var writeMu sync.Mutex
	syncWriteEvent := func(ev StreamEvent) {
		writeMu.Lock()
		defer writeMu.Unlock()
		writeStreamEvent(conn, ev)
	}

	writeResponse(conn, Response{OK: true, Payload: marshalJSON(ApplyIntentResponse{IntentID: intentID, Tree: nil})})

	syncWriteEvent(StreamEvent{Type: StreamIntentDecomposed, Payload: treeJSON})

	if !req.AutoStart {
		syncWriteEvent(StreamEvent{Type: StreamIntentConfirmReq, Payload: marshalJSON(IntentNodeEventPayload{NodeID: intentID})})

		if !connScanner.Scan() {
			return
		}
		var confirmReq Request
		if err := json.Unmarshal(connScanner.Bytes(), &confirmReq); err != nil {
			return
		}
		var confirm IntentConfirmRequest
		if err := json.Unmarshal(confirmReq.Payload, &confirm); err != nil {
			return
		}
		if !confirm.Confirm {
			syncWriteEvent(StreamEvent{Type: StreamIntentComplete, Payload: marshalJSON(IntentNodeEventPayload{Error: "user cancelled"})})
			return
		}
		if err := s.intentMgr.ConfirmIntent(intentID); err != nil {
			syncWriteEvent(StreamEvent{Type: StreamError, Payload: marshalJSON(IntentNodeEventPayload{Error: err.Error()})})
			return
		}
	}

	execErr := s.intentMgr.ExecuteIntent(context.Background(), intentID,
		func(nodeID string, pid uint64) {
			syncWriteEvent(StreamEvent{Type: StreamIntentNodeStart, Payload: marshalJSON(IntentNodeEventPayload{NodeID: nodeID, PID: pid})})
		},
		func(nodeID, result string) {
			syncWriteEvent(StreamEvent{Type: StreamIntentNodeDone, Payload: marshalJSON(IntentNodeEventPayload{NodeID: nodeID, Result: result})})
		},
		func(nodeID, errMsg string) {
			syncWriteEvent(StreamEvent{Type: StreamIntentNodeFailed, Payload: marshalJSON(IntentNodeEventPayload{NodeID: nodeID, Error: errMsg})})
		},
		func(completed, total int) {
			syncWriteEvent(StreamEvent{Type: StreamIntentProgress, Payload: marshalJSON(IntentNodeEventPayload{Completed: completed, Total: total})})
		},
		func(nodeID string, attempt, maxRetries int) {
			syncWriteEvent(StreamEvent{Type: StreamIntentNodeRetry, Payload: marshalJSON(IntentNodeEventPayload{NodeID: nodeID, RetryAttempt: attempt, MaxRetries: maxRetries})})
		},
		func(nodeID string) {
			syncWriteEvent(StreamEvent{Type: StreamIntentNodeTimeout, Payload: marshalJSON(IntentNodeEventPayload{NodeID: nodeID})})
		},
		func(nodeID, driftType, message string) {
			syncWriteEvent(StreamEvent{Type: StreamIntentDriftDetected, Payload: marshalJSON(IntentNodeEventPayload{NodeID: nodeID, DriftType: driftType, Error: message})})
		},
		func(nodeID string) {
			syncWriteEvent(StreamEvent{Type: StreamIntentDriftResolved, Payload: marshalJSON(IntentNodeEventPayload{NodeID: nodeID})})
		},
	)

	if execErr != nil {
		syncWriteEvent(StreamEvent{Type: StreamIntentComplete, Payload: marshalJSON(IntentNodeEventPayload{Error: execErr.Error()})})
	} else {
		syncWriteEvent(StreamEvent{Type: StreamIntentComplete, Payload: marshalJSON(IntentNodeEventPayload{})})
	}
}

func (s *Server) handleIntentStatus(conn net.Conn, rawPayload json.RawMessage) {
	if s.intentMgr == nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "not_available", Message: "intent manager not initialized"}})
		return
	}

	var req IntentStatusRequest
	if err := json.Unmarshal(rawPayload, &req); err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "invalid_payload", Message: err.Error()}})
		return
	}

	var data []byte
	var err error
	if req.IntentID != "" {
		data, err = s.intentMgr.IntentStatus(req.IntentID)
	} else {
		data, err = s.intentMgr.ListActiveIntents()
	}
	if err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "not_found", Message: err.Error()}})
		return
	}

	writeResponse(conn, Response{OK: true, Payload: data})
}

func (s *Server) handleIntentConfirm(conn net.Conn, rawPayload json.RawMessage) {
	if s.intentMgr == nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "not_available", Message: "intent manager not initialized"}})
		return
	}

	var req IntentConfirmRequest
	if err := json.Unmarshal(rawPayload, &req); err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "invalid_payload", Message: err.Error()}})
		return
	}

	if err := s.intentMgr.ConfirmIntent(req.IntentID); err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "confirm_failed", Message: err.Error()}})
		return
	}

	writeResponse(conn, Response{OK: true})
}

func (s *Server) handleApplyIncrementalIntent(conn net.Conn, rawPayload json.RawMessage) {
	if s.intentMgr == nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "not_available", Message: "intent manager not initialized"}})
		return
	}

	var req ApplyIncrementalIntentRequest
	if err := json.Unmarshal(rawPayload, &req); err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "invalid_payload", Message: err.Error()}})
		return
	}

	intentID, resultJSON, err := s.intentMgr.ApplyIncrementalIntent(context.Background(), req.IntentID, req.Intent, req.Model, req.Provider)
	if err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "incremental_failed", Message: err.Error()}})
		return
	}

	// Parse the result to build the response
	var mergeResp ApplyIncrementalIntentResponse
	if err := json.Unmarshal(resultJSON, &mergeResp); err != nil {
		// fallback: send raw
		mergeResp = ApplyIncrementalIntentResponse{IntentID: intentID}
	}

	writeResponse(conn, Response{OK: true, Payload: marshalJSON(mergeResp)})
}

func (s *Server) handleIntentList(conn net.Conn) {
	if s.intentMgr == nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "not_available", Message: "intent manager not initialized"}})
		return
	}

	data, err := s.intentMgr.ListAllIntents()
	if err != nil {
		writeResponse(conn, Response{OK: false, Error: &ErrorPayload{Code: "list_failed", Message: err.Error()}})
		return
	}

	writeResponse(conn, Response{OK: true, Payload: data})
}
