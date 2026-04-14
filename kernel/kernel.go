package kernel

import (
	gocontext "context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rnixai/rnix/agents"
	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/debug"
	"github.com/rnixai/rnix/internal/config"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/internal/xsync"
	"github.com/rnixai/rnix/kernel/memory"
	"github.com/rnixai/rnix/skills"
	"github.com/rnixai/rnix/vfs"
)

// MountManager defines the interface for mounting/unmounting MCP servers.
type MountManager interface {
	Mount(path string, config vfs.MCPConfig) error
	Unmount(path string) error
	UnmountAll() error
}

// pidContextKey is used to store PID in context for VFS device callbacks.
type pidContextKey struct{}

// ContextWithPID returns a context carrying the given PID.
func ContextWithPID(ctx gocontext.Context, pid types.PID) gocontext.Context {
	return gocontext.WithValue(ctx, pidContextKey{}, pid)
}

// PIDFromContext extracts the PID from a context, returning 0 if not present.
func PIDFromContext(ctx gocontext.Context) types.PID {
	if pid, ok := ctx.Value(pidContextKey{}).(types.PID); ok {
		return pid
	}
	return 0
}

// DefaultMaxSteps is the default maximum number of reasoning steps.
// 0 means infinite (no step limit).
const DefaultMaxSteps = 0

// DefaultCtxSize is the default context size (message count) for new contexts.
const DefaultCtxSize = 64

// SpawnOpts configures optional parameters for Spawn.
type SpawnOpts struct {
	Model         string
	SystemPrompt  string
	MaxTurns      int
	TimeoutMs     int64
	ParentPID     types.PID     // parent process PID; 0 = top-level/CLI spawn
	ContextBudget int           // 0 = no limit; >0 = terminate when TokensUsed >= ContextBudget
	MaxTokens     int64         // per-process token budget; 0 = unlimited; >0 = suspend when exhausted
	MaxCost       float64       // per-process cost budget (USD); 0 = unlimited; >0 = suspend when exhausted
	TraceID       types.TraceID // inherited trace ID; empty = no tracing
	ParentSpanID  types.SpanID  // parent process span ID
	Provider      string        // LLM provider override (from CLI --provider); "" = use agent manifest or default "claude"
	StepTimeout   time.Duration // per-step heartbeat timeout; 0 = use agent manifest or default 5m
	StartStep     int           // Resume: start reasoning loop from this step (0 = normal start from 1)

	PreallocatedCtxID types.CtxID           // non-zero = skip CtxAlloc, use this pre-setup context
	SkipReasonLoop    bool                  // true = don't open LLM device or start reasonStep goroutine
	ProjectConfig     *config.ProjectConfig // project-level config snapshot; nil = global only
	CompactThreshold  float64               // 0 = use default (80%); >0 = trigger compact when TokenUsage > threshold
	GracePeriod       time.Duration          // 0 = use DefaultGracePeriod; >0 = custom SIGTERM grace period

	// Orchestration metadata (Story 34.7)
	ComposeNode   string   // compose node name (e.g. "summarizer")
	ComposeDeps   []string // upstream dependency node names
	PipelineIndex int      // 0-based stage index; -1 = not pipeline
	PipelineTotal int      // total stages; 0 = not pipeline
}

// KernelCallbacks allows the CLI layer to receive progress notifications
// from the kernel without introducing a reverse dependency on internal/ui.
type KernelCallbacks interface {
	OnSpawn(pid types.PID, intent, provider, model, uuid string)
	OnStep(pid types.PID, step int, total int)
	OnStepComplete(pid types.PID, step int, action string, summary string, hasError bool, durationMs float64)
	OnComplete(pid types.PID, result string, exit ExitStatus)
	OnError(pid types.PID, err error)
	OnAskUser(pid types.PID, requestID string, questions []byte) ([]byte, error)
}

// ProcessManager defines the kernel's process management interface.
type ProcessManager interface {
	Spawn(intent string, agent *agents.AgentInfo, opts SpawnOpts) (types.PID, error)
	Kill(pid types.PID, signal types.Signal) error
	Wait(pid types.PID) (ExitStatus, error)
}

// Compile-time interface compliance check.
var _ ProcessManager = (*KernelImpl)(nil)

// KernelImpl is the core microkernel implementation.
type KernelImpl struct {
	procTable *xsync.SyncMap[types.PID, *Process]
	vfs       *vfs.VFS
	ctxMgr    *rnixctx.Manager
	callbacks KernelCallbacks

	// Reaper infrastructure (Story 4.2)
	reapCh       chan types.PID // PIDs pending auto-reap
	stopCh       chan struct{}  // signals reaper goroutine to stop
	reaperWg     sync.WaitGroup // waits for reaper goroutine exit
	shutdownOnce sync.Once      // ensures Shutdown executes at most once
	deadTicker   *time.Ticker   // periodic cleanup of expired Dead processes

	// IPC messaging (Story 6.1)
	msgQueues *xsync.SyncMap[types.PID, *MessageQueue]
	msgSeq    atomic.Uint64

	// Process groups (Story 6.3)
	procGroups *xsync.SyncMap[types.PGID, *ProcGroup]

	// MCP mount manager (Story 9.1)
	mountMgr MountManager

	// Execution recording (Story 14.1)
	recordMgr *debug.RecordManager

	// Span recording (Story 15.1)
	spanRecorder *debug.SpanRecorder

	// Agent loader for autonomous spawn (Story 20.2)
	agentLoader func(name string) (*agents.AgentInfo, error)

	// Stem agent differentiation (Story 20.3)
	stemMatcher *StemMatcher
	skillLoader func(string) (*skills.SkillInfo, error)

	// Differentiation memory (Story 20.4)
	diffMemory *DiffMemory

	// Provider resolution callbacks (Story 23.3)
	providerNames   func() []string
	hasProvider     func(name string) bool
	costPerToken    func(provider string) float64 // returns cost per token for a provider; 0 = unknown
	defaultProvider string // injected default provider name; "" = fall back to "claude"

	// Budget pools (Story 21.1)
	budgetPools *xsync.SyncMap[types.PGID, *BudgetPool]

	// SLA results (Story 21.2)
	slaResults   *xsync.SyncMap[types.PGID, []*SLAResult]
	slaResultsMu sync.Mutex // guards Load+Modify+Store on slaResults

	// Process history (Story 29.4)
	procHistory *ProcessHistory

	// Immune daemon (Story 22.1)
	immuneDaemon *ImmuneDaemon

	// Step data directory override for testing (Story 27.1)
	stepDataDir string

	// Heartbeat monitor (Story 30.6)
	heartbeatMonitor *HeartbeatMonitor

	// Resume serialization — prevents concurrent Resume for the same UUID (Story 30.4 review)
	resumeMu sync.Mutex

	// Memory store (Story 35.2)
	memoryStore *memory.MemoryStore

	// Writeback worker (Story 35.3)
	writebackWorker *memory.WritebackWorker

	// Recall index (Story 35.4)
	recallIndex *memory.RecallIndex
}

// NewKernel creates a new KernelImpl with the given VFS, context manager, and optional callbacks.
func NewKernel(v *vfs.VFS, ctxMgr *rnixctx.Manager, cb KernelCallbacks) *KernelImpl {
	k := &KernelImpl{
		procTable:    xsync.NewSyncMap[types.PID, *Process](),
		vfs:          v,
		ctxMgr:       ctxMgr,
		callbacks:    cb,
		reapCh:       make(chan types.PID, 64),
		stopCh:       make(chan struct{}),
		msgQueues:    xsync.NewSyncMap[types.PID, *MessageQueue](),
		procGroups:   xsync.NewSyncMap[types.PGID, *ProcGroup](),
		spanRecorder: debug.NewSpanRecorder(),
		procHistory:  NewProcessHistory(1000),
		budgetPools:  xsync.NewSyncMap[types.PGID, *BudgetPool](),
		slaResults:   xsync.NewSyncMap[types.PGID, []*SLAResult](),
	}
	k.startReaper()
	return k
}

// SetStepDataDir overrides the base directory for StepWriter output.
func (k *KernelImpl) SetStepDataDir(dir string) {
	k.stepDataDir = dir
}

// GetStepDataDir returns the base directory for step data output.
func (k *KernelImpl) GetStepDataDir() string {
	return k.stepDataDir
}

// resolveBaseDir returns the base .rnix directory for persistence data.
func (k *KernelImpl) resolveBaseDir(proc *Process) string {
	if k.stepDataDir != "" {
		return k.stepDataDir
	}
	if proc.ProjectConfig != nil && proc.ProjectConfig.ProjectDir != "" {
		return filepath.Join(proc.ProjectConfig.ProjectDir, ".rnix")
	}
	return ""
}

// SetMountManager sets the MCP mount manager on the kernel.
func (k *KernelImpl) SetMountManager(mgr MountManager) {
	k.mountMgr = mgr
}

// SetMemoryStore injects the memory store for BuildPrompt injection and VFS device access.
func (k *KernelImpl) SetMemoryStore(store *memory.MemoryStore) {
	k.memoryStore = store
}

// MemoryStore returns the kernel's memory store, or nil if memory is disabled.
func (k *KernelImpl) MemoryStore() *memory.MemoryStore {
	return k.memoryStore
}

// SetWritebackWorker injects the writeback worker for async knowledge extraction.
func (k *KernelImpl) SetWritebackWorker(w *memory.WritebackWorker) {
	k.writebackWorker = w
}

// WritebackWorker returns the kernel's writeback worker, or nil if writeback is disabled.
func (k *KernelImpl) WritebackWorker() *memory.WritebackWorker {
	return k.writebackWorker
}

// SetRecallIndex injects the recall index for cross-process search.
func (k *KernelImpl) SetRecallIndex(ri *memory.RecallIndex) {
	k.recallIndex = ri
}

// RecallIndex returns the kernel's recall index, or nil if recall is disabled.
func (k *KernelImpl) RecallIndex() *memory.RecallIndex {
	return k.recallIndex
}

// SetRecordManager sets the execution recording manager on the kernel.
func (k *KernelImpl) SetRecordManager(mgr *debug.RecordManager) {
	k.recordMgr = mgr
}

// SetSpanWriter sets the optional SpanWriter for persisting completed spans.
func (k *KernelImpl) SetSpanWriter(w *debug.SpanWriter) {
	if k.spanRecorder != nil {
		k.spanRecorder.SetWriter(w)
	}
}

// SetAgentLoader injects the agent loading function for autonomous spawn.
func (k *KernelImpl) SetAgentLoader(loader func(name string) (*agents.AgentInfo, error)) {
	k.agentLoader = loader
}

// SetStemMatcher injects the stem agent matcher for auto-differentiation.
func (k *KernelImpl) SetStemMatcher(m *StemMatcher) {
	k.stemMatcher = m
}

// SetSkillLoader injects the skill loading function for stem agent differentiation.
func (k *KernelImpl) SetSkillLoader(fn func(string) (*skills.SkillInfo, error)) {
	k.skillLoader = fn
}

// SetDiffMemory injects the differentiation memory for stem agent path reuse.
func (k *KernelImpl) SetDiffMemory(m *DiffMemory) {
	k.diffMemory = m
}

// SetImmuneDaemon injects the immune daemon for behavioral monitoring (Story 22.1).
func (k *KernelImpl) SetImmuneDaemon(d *ImmuneDaemon) {
	k.immuneDaemon = d
}

// SetHeartbeatMonitor injects the heartbeat monitor for stalled process detection (Story 30.6).
func (k *KernelImpl) SetHeartbeatMonitor(hm *HeartbeatMonitor) {
	k.heartbeatMonitor = hm
}

// GetHeartbeatMonitor returns the heartbeat monitor, or nil if not set.
func (k *KernelImpl) GetHeartbeatMonitor() *HeartbeatMonitor {
	return k.heartbeatMonitor
}

// SetProviderResolver injects callbacks for dynamic LLM provider validation.
func (k *KernelImpl) SetProviderResolver(names func() []string, has func(name string) bool) {
	k.providerNames = names
	k.hasProvider = has
}

// SetDefaultProvider injects the default LLM provider name.
func (k *KernelImpl) SetDefaultProvider(name string) {
	k.defaultProvider = name
}

// SetCostPerToken injects the cost-per-token lookup function for process budget tracking.
func (k *KernelImpl) SetCostPerToken(fn func(provider string) float64) {
	k.costPerToken = fn
}

// StartRecording starts execution recording for the given PID.
func (k *KernelImpl) StartRecording(pid types.PID) (string, error) {
	if k.recordMgr == nil {
		return "", NewSyscallError("StartRecording", pid, "", fmt.Errorf("record manager not initialized"), types.ErrInternal)
	}
	proc, ok := k.GetProcess(pid)
	if !ok {
		return "", NewSyscallError("StartRecording", pid, "", fmt.Errorf("process not found"), types.ErrNotFound)
	}
	if proc.GetState() != types.StateRunning {
		return "", NewSyscallError("StartRecording", pid, "", fmt.Errorf("process is not running (state: %s)", proc.GetState()), types.ErrInvalid)
	}
	recordID, err := k.recordMgr.StartRecording(pid, proc.Intent)
	if err != nil {
		return "", NewSyscallError("StartRecording", pid, "", err, types.ErrInternal)
	}
	return recordID, nil
}

// StopRecording stops execution recording for the given PID.
func (k *KernelImpl) StopRecording(pid types.PID) error {
	if k.recordMgr == nil {
		return NewSyscallError("StopRecording", pid, "", fmt.Errorf("record manager not initialized"), types.ErrInternal)
	}
	if err := k.recordMgr.StopRecording(pid); err != nil {
		return NewSyscallError("StopRecording", pid, "", err, types.ErrInternal)
	}
	return nil
}

// GetRecordManager returns the record manager.
func (k *KernelImpl) GetRecordManager() *debug.RecordManager {
	return k.recordMgr
}

// Mount mounts an MCP server at the given path via the MountManager.
func (k *KernelImpl) Mount(path string, config vfs.MCPConfig) error {
	if k.mountMgr == nil {
		return NewSyscallError("Mount", 0, path, fmt.Errorf("mount manager not initialized"), types.ErrInternal)
	}
	if !strings.HasPrefix(path, "/mnt/mcp/") {
		return NewSyscallError("Mount", 0, path, fmt.Errorf("invalid mount path: must start with /mnt/mcp/"), types.ErrInvalid)
	}
	err := k.mountMgr.Mount(path, config)
	if err != nil {
		return NewSyscallError("Mount", 0, path, err, types.ErrDriver)
	}
	return nil
}

// Unmount unmounts the MCP server at the given path.
func (k *KernelImpl) Unmount(path string) error {
	if k.mountMgr == nil {
		return NewSyscallError("Unmount", 0, path, fmt.Errorf("mount manager not initialized"), types.ErrInternal)
	}
	if !strings.HasPrefix(path, "/mnt/mcp/") {
		return NewSyscallError("Unmount", 0, path, fmt.Errorf("invalid unmount path: must start with /mnt/mcp/"), types.ErrInvalid)
	}
	err := k.mountMgr.Unmount(path)
	if err != nil {
		return NewSyscallError("Unmount", 0, path, err, types.ErrDriver)
	}
	return nil
}
