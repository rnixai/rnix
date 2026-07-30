package ipc

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rnixai/rnix/agents"
	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/internal/config"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/xsync"
	"github.com/rnixai/rnix/kernel"
	"github.com/rnixai/rnix/skills"
	"github.com/rnixai/rnix/vfs"
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
	// Story 45.1 — daemon build provenance, surfaced via MethodDaemonStatus.
	// commit and buildDate are populated at NewServer construction and treated
	// as immutable metadata thereafter; startedAt records construction wall
	// time for downstream uptime calculations.
	commit      string
	buildDate   string
	startedAt   time.Time
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

	// providerDriverCache lazily caches provider name → driver type from
	// globalConfig.Providers (Story 41.2 AC#3, used by handleGetStepDetail to
	// populate response.DriverType so dashboard can compute cache hit rate
	// with correct driver-specific formula).
	providerDriverCache     map[string]string
	providerDriverCacheOnce sync.Once

	featureProfile string
	featureFlags   map[string]bool
}

// NewServer creates an IPC server backed by the given kernel.
// The callbackMux is registered as the kernel's callback handler for per-PID routing.
//
// Story 45.1: NewServer takes version + commit + buildDate. commit and
// buildDate are surfaced via MethodDaemonStatus so downstream consumers can
// verify which rnix commit the running daemon was built from. Tests may pass
// empty strings for the provenance fields when they only exercise non-status
// methods.
func NewServer(kern *kernel.KernelImpl, agentLoader AgentLoaderFunc, version, commit, buildDate string) *Server {
	s := &Server{
		kern:             kern,
		agentLoader:      agentLoader,
		callbackMux:      newCallbackMux(),
		version:          version,
		commit:           commit,
		buildDate:        buildDate,
		startedAt:        time.Now(),
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
		if p.State == types.StateRunning || p.State == types.StateCreated || p.State == types.StateSuspended {
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
		if p.State == types.StateRunning || p.State == types.StateCreated || p.State == types.StateSuspended {
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

		// Close callbackMux done channel to unblock pending OnAskUser calls
		close(s.callbackMux.done)

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
		case MethodDaemonStatus:
			s.handleDaemonStatus(conn)
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
		case MethodReputationStatus:
			s.handleReputationStatus(conn, req.Payload)
		case MethodSynergyList:
			s.handleSynergyList(conn)
		case MethodImmuneStatus:
			s.handleImmuneStatus(conn)
		case MethodImmuneResume:
			s.handleImmuneResume(conn, req.Payload)
		case MethodImmuneForget:
			s.handleImmuneForget(conn, req.Payload)
		case MethodSimilarityQuery:
			s.handleSimilarityQuery(conn, req.Payload)
		case MethodTopologyQuery:
			s.handleTopologyQuery(conn)
		case MethodGetStepDetail:
			s.handleGetStepDetail(conn, req.Payload)
		case MethodListSteps:
			s.handleListSteps(conn, req.Payload)
		case MethodListEvents:
			s.handleListEvents(conn, req.Payload)
		case MethodGetRawCapture:
			s.handleGetRawCapture(conn, req.Payload)
		case MethodGetProcDetail:
			s.handleGetProcDetail(conn, req.Payload)
		case MethodTraceList:
			s.handleTraceList(conn)
		case MethodTraceTree:
			s.handleTraceTree(conn, req.Payload)
		case MethodListAllProcs:
			s.handleListAllProcs(conn, req.Payload)
		case MethodSuspend:
			s.handleSuspend(conn, req.Payload)
		case MethodResume:
			s.handleResume(conn, req.Payload)
		case MethodResumeWatch:
			s.handleResumeWatch(conn, req.Payload)
			return // streaming method — handler manages connection lifetime
		case MethodHeartbeatStatus:
			s.handleHeartbeatStatus(conn)
		case MethodCompact:
			s.handleCompact(conn, req.Payload)
		case MethodAnswerUser:
			s.handleAnswerUser(conn, req.Payload)
		case MethodSignalTree:
			s.handleSignalTree(conn, req.Payload)
		case MethodPauseSubtree:
			s.handlePauseSubtree(conn, req.Payload)
		case MethodResumeSubtree:
			s.handleResumeSubtree(conn, req.Payload)
		case MethodListResumable:
			s.handleListResumable(conn)
		case MethodGetResumeLineage:
			s.handleGetResumeLineage(conn, req.Payload)
		case MethodGc:
			s.handleGc(conn, req.Payload)
		case MethodGcDryRun:
			s.handleGcDryRun(conn, req.Payload)
		case MethodMCPList:
			s.handleMCPList(conn)
		case MethodMCPTest:
			s.handleMCPTest(conn, req.Payload)
		case MethodMCPLogs:
			s.handleMCPLogs(conn, req.Payload)
		case MethodMCPReload:
			s.handleMCPReload(conn)
		case MethodWait:
			s.handleWait(conn, req.Payload)
			return // long-blocking method — handler manages connection lifetime (mirror MethodSpawn)
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

// handleDaemonStatus (Story 45.1 AC#3) surfaces daemon build provenance.
// Distinct from handlePing so the lightweight ping path stays free of
// extra fields (ipc/daemon.go:56 EnsureDaemon still polls Ping).
func (s *Server) handleDaemonStatus(conn net.Conn) {
	payload, _ := json.Marshal(DaemonStatusResponse{
		Version:         s.version,
		DaemonCommit:    s.commit,
		DaemonBuildDate: s.buildDate,
		DaemonPID:       os.Getpid(),
		StartedAt:       s.startedAt.UnixMilli(),
		FeatureProfile:  s.featureProfile,
		FeatureFlags:    s.featureFlags,
	})
	writeResponse(conn, Response{OK: true, Payload: payload})
}

// SetFeatureProfile injects the active feature profile name and per-flag map
// into the server so handleDaemonStatus can surface them. Called once at daemon
// startup after config.ResolveFeatures.
func (s *Server) SetFeatureProfile(profile string, flags map[string]bool) {
	s.featureProfile = profile
	s.featureFlags = flags
}

func (s *Server) handleShutdown(conn net.Conn) {
	writeResponse(conn, Response{OK: true})
	go s.Shutdown()
}

// --- Setter methods ---

// CallbackMux returns the server's callback multiplexer for use as KernelCallbacks.
func (s *Server) CallbackMux() kernel.KernelCallbacks {
	return s.callbackMux
}

// SetKernel sets the kernel instance after construction (for circular init).
func (s *Server) SetKernel(k *kernel.KernelImpl) {
	s.kern = k
}

// LoadProjectConfig is the kernel-facing wrapper around resolveProjectContext,
// exposing only the ProjectConfig (the agent-loader return value is consumed
// internally by the spawn IPC path and irrelevant to kernel callers). Used by
// kernel.LoadSuspendedFromDisk via the SetProjectConfigLoader injection so
// placeholders revived after a daemon restart can re-attach project context
// (LLMFileOpener / AgentLoader / SkillLoader / EnvSnapshot) and successfully
// reopen project-only LLM devices on resume.
//
// rnixEnv defaults to "" — resolveProjectContext interprets that as the
// canonical "development" env. Future work may persist RnixEnv to
// proc-info.json so post-restart resume honors the original env. The current
// caller (LoadSuspendedFromDisk) accepts that limitation in exchange for not
// stashing a stale env identifier on disk.
func (s *Server) LoadProjectConfig(projectDir string) (*config.ProjectConfig, error) {
	cfg, _, err := s.resolveProjectContext(projectDir, "")
	return cfg, err
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

// --- Utility functions ---

func mustMarshal(v any) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
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

// isValidUUID checks if a string looks like a valid UUID (8-4-4-4-12 hex format).
func isValidUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if c != '-' {
				return false
			}
		} else {
			if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
				return false
			}
		}
	}
	return true
}

// resolveProcess tries to find a process in memory. UUID takes priority over PID.
func (s *Server) resolveProcess(pid types.PID, uuid string) (*kernel.Process, bool) {
	if uuid != "" && isValidUUID(uuid) {
		if proc, ok := s.kern.GetProcessByUUID(uuid); ok {
			return proc, true
		}
	}
	if pid != 0 {
		return s.kern.GetProcess(pid)
	}
	return nil, false
}

// resolvePID resolves a PID from either PID or UUID. UUID takes priority.
// Returns (pid, true) on success, (0, false) on failure.
func (s *Server) resolvePID(pid types.PID, uuid string) (types.PID, bool) {
	if uuid != "" && isValidUUID(uuid) {
		if proc, ok := s.kern.GetProcessByUUID(uuid); ok {
			return proc.PID, true
		}
		// UUID provided but not found in memory → fail
		return 0, false
	}
	if pid != 0 {
		return pid, true
	}
	return 0, false
}

// resolveStepsPath resolves the steps.jsonl path. UUID takes priority.
// For reaped processes: UUID query reads from disk, PID query returns empty.
func (s *Server) resolveStepsPath(pid types.PID, uuid string) string {
	proc, found := s.resolveProcess(pid, uuid)
	if found {
		return s.resolveStepsPathFromProc(proc)
	}
	// Process not in memory — UUID can read from disk
	if uuid != "" && isValidUUID(uuid) {
		base := kernel.FindBaseDirByUUID(s.kern.GetDataDir(), uuid)
		if base != "" {
			p := filepath.Join(base, "steps", uuid, "steps.jsonl")
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
		return ""
	}
	// PID only, process not in memory → not found
	return ""
}

// resolveStepsPathFromProc resolves the steps.jsonl path from a living process using UUID.
func (s *Server) resolveStepsPathFromProc(proc *kernel.Process) string {
	base := s.kern.ResolveStepBaseDir(proc)
	if base == "" {
		return ""
	}
	return filepath.Join(base, "steps", proc.UUID, "steps.jsonl")
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

// toolCallRecordsToWire converts kernel-side StepRecord.ToolCalls 到 wire 表示。
// Spec step-inspector-data-fidelity：保留 nil 语义,旧 steps.jsonl 没有该数组时
// 返回 nil,让 dashboard 退化使用顶层 ToolPath/ToolInput/... 字段。
func toolCallRecordsToWire(calls []types.ToolCallRecord) []ToolCallDetailWire {
	if len(calls) == 0 {
		return nil
	}
	wires := make([]ToolCallDetailWire, len(calls))
	for i, c := range calls {
		wires[i] = ToolCallDetailWire{
			ID:         c.ID,
			Name:       c.Name,
			Path:       c.Path,
			Input:      c.Input,
			Result:     c.Result,
			Error:      c.Error,
			ErrorCode:  c.ErrorCode,
			DurationMs: c.DurationMs,
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
