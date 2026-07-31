package kernel

import (
	"context"
	"fmt"
	"log"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/internal/config"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// pidCounter is the global PID allocator. PIDs start at 1 and monotonically
// increase. They are never recycled within a daemon's lifetime; daemon restart
// re-seeds the counter from on-disk proc-info via SeedPIDCounterFromDisk so
// fresh allocations don't collide with persisted PIDs.
var pidCounter atomic.Uint64

// nextPID returns the next unique PID.
func nextPID() types.PID {
	return types.PID(pidCounter.Add(1))
}

// ProcessBudget tracks per-process resource budget for cost/token limiting.
// When a budget limit is reached, the process is suspended (not terminated).
type ProcessBudget struct {
	MaxTokens int64   // 0 = unlimited
	MaxCost   float64 // 0 = unlimited (USD)
	UsedCost  float64 // accumulated cost
}

// ExitStatus records how a process terminated.
type ExitStatus struct {
	Code   int    // 0 = normal, non-zero = abnormal
	Reason string // human-readable reason
	Result string // F4: the process's actual output (proc.Result), distinct from Reason; used by intent reconciler to inject a child's real result into dependents' context
	Err    error  // underlying error, if any
}

const (
	ExitOK          = 0 // normal completion
	ExitError       = 1 // abnormal termination (circuit_breaker, append failure, unexpected exit)
	ExitSuspended   = 2 // suspended (budget_exhausted, loop_detected, user_suspended, max_turns)
	ExitContextFull = 3 // context full after precompact (recoverable via resume + compact)
)

// DeferredSkillMeta holds lightweight metadata for a deferred skill,
// used by discover_skill to score relevance without loading the full skill body.
type DeferredSkillMeta struct {
	Name        string
	Description string
	SearchHint  string
}

// Process represents an agent process.
type Process struct {
	PID             types.PID
	UUID            string // UUID v7 — immutable after creation, globally unique across daemon restarts
	OriginUUID      string // non-empty when this process was forked from another UUID (resume --fork)
	ResumedFromStep int    // step number from which this process was resumed (0 = fresh spawn)
	PPID            types.PID
	Depth           int                // spawn-chain depth for the recursion guard; 0 = top-level. Set from SpawnOpts.Depth; only ActionSpawn accumulates (parent.Depth+1).
	ParentUUID      string             // UUID of parent process — immutable after creation
	State           types.ProcessState // guarded by mu
	Intent          string             // immutable after creation
	Skills          []string
	DeferredSkills  []DeferredSkillMeta // metadata-only skills for discover_skill scoring
	SkillBodies     map[string]string   // skill name → body content (mu protected); updated by spawn + specialize
	SkillDirs       map[string]string   // skill name → absolute source directory (mu protected); used for claude-cli bundle symlinks
	Children        []types.PID
	FDTable         map[types.FD]vfs.VFSFile // per architecture doc; VFS manages actual FD state internally
	DebugChan       chan types.SyscallEvent
	LogChan         chan types.LogEntry
	Done            chan ExitStatus
	CreatedAt       time.Time
	DeadAt          time.Time     // set by reapProcess, used for TTL cleanup
	Exit            *ExitStatus   // non-nil in Zombie/Dead
	CtxID           types.CtxID   // context allocated by Spawn
	Result          string        // final output from reasoning
	ResultPartial   bool          // true when Result contains incomplete stream content (signal kill)
	TokensUsed      int           // cumulative token consumption
	LastInputTokens int           // most recent LLM call's prompt input tokens (for context_budget check)
	// Story 66.6 — mid-stream usage ledger (all mu protected). A single CLI
	// ReasonStep is an entire CLI session (hundreds of API round-trips inside
	// the CLI process), so TokensUsed stays 0 until `done`. StreamTokensUsed
	// accumulates the driver's mid-stream usage deltas for the CURRENT step so
	// ps / proc-info can preview live growth; it is cleared at the step boundary
	// (reason.go) where resp.TokensUsed is authoritative, and folded into
	// TokensUsed only when the step is killed before that boundary
	// (finishProcess). StreamInputTokens keeps the most recent mid-stream input
	// value (observation only). UsageProvenance labels the fidelity source
	// (see vfs.UsageProvenance*). DriverType is the resolved LLM driver type
	// (e.g. "claude-cli") captured by setupDriverStreamHandler, read at the step
	// boundary to decide cli_stream vs api_response provenance.
	StreamTokensUsed  int
	StreamInputTokens int
	UsageProvenance   string
	DriverType        string
	// ToolCallCount is the process-level cumulative tool-call count (Story 66.6):
	// CLI drivers bump it per DriverToolCall flush (observe.go), the kernel bumps
	// it per native VFS tool dispatch (tool_exec.go). A ps ACTIVE-column liveness
	// signal that keeps moving during a long step regardless of usage availability.
	ToolCallCount int
	ContextBudget   int           // 0 = no limit; >0 = suspend when single-step InputTokens >= ContextBudget
	CtxSize         int           // context message slot limit used at allocation (for checkpoint)
	Budget          ProcessBudget // per-process resource budget (mu protected); suspend when exhausted
	MaxSteps        int           // max reasoning steps for this process (from SpawnOpts.MaxTurns or DefaultMaxSteps)

	// Loop detection (Story 70.1). STEP COUNTS, not durations.
	// 0 = use the kernel default (30 fine / 60 coarse); NEGATIVE = disable that
	// track. Read via effectiveLoopThreshold() / effectiveCoarseLoopThreshold(),
	// which pass a negative value through UNCHANGED so NewLoopDetector can honour
	// the disable request.
	LoopThreshold       int
	CoarseLoopThreshold int
	AllowedDevices  []string      // nil/empty = all devices allowed; non-empty = whitelist only
	DeniedDevices   []string      // device blacklist; checked before AllowedDevices; blocks access regardless of whitelist
	// AllowedTools is the process-level AUTHORITATIVE tool-name whitelist (Story 54.1).
	// nil/empty = no tool-level constraint; non-empty = only these base-device tools
	// (by tc.Name, e.g. "Read") may run — so allowed-tools:Read permits Read but denies
	// Write even though both live under /dev/fs. DISTINCT from the three same-named
	// concepts: SkillManifest.AllowedTools() / AgentInfo.AllowedTools() return DECLARED
	// raw values (device paths today); SkillInfoWire.AllowedTools is a dashboard wire
	// field. This is the runtime-enforced tool set. MCP tools are NOT gated here (their
	// names are dynamic) — they stay path-gated via AllowedDevices. (mu protected)
	AllowedTools []string
	MCPMounts    []string // MCP mount paths auto-mounted by Spawn
	// mcpReusedMounts contains MCP paths this process did NOT mount itself
	// (Story 48.1 AC7 — fork-resume collision: an existing mount under the
	// shared `/mnt/mcp/<original-pid>-<server>` path is reused rather than
	// duplicated). finishProcess SKIPS these on exit-time unmount so a sibling
	// fork or the original parent still sees the mount. Empty slice (the
	// normal case) means MCPMounts and the unmount set are identical.
	// Protected by proc.mu in the same critical section as MCPMounts.
	mcpReusedMounts []string
	// mcpFDPaths maps an open FD to its MCP tool path (e.g.
	// /mnt/mcp/100-playwright/tools/screenshot), populated by vfsOpenWithEvent
	// only for /mnt/mcp/ opens. This gates Story 48.5 health observation to the
	// MCP fast path (non-MCP fds never enter the map → zero overhead, AC6) and
	// lets Write/Read resolve the owning server without an fd→path syscall.
	// Protected by proc.mu.
	mcpFDPaths map[types.FD]string
	// mcpReconnectSeen records the last-observed transport ReconnectCount per
	// server so the kernel emits mcp.reconnect exactly once per delta (Story
	// 48.5 AC3 / §易错点 13). Protected by proc.mu.
	mcpReconnectSeen map[string]int
	TraceID          types.TraceID
	SpanID           types.SpanID
	ParentSpanID     types.SpanID
	// failedChildren counts spawned children (ActionSpawn) and orchestration
	// sub-tasks (intent, via DriverError.FailsParent → MarkFailedChild) that
	// exited non-zero. It is the sole Layer-1 program-level driver of a non-zero
	// completion exit code (spec-exit-code-tool-error-fidelity). VFS tool error
	// codes are content-layer results and do NOT affect the exit code — the old
	// sticky HasToolError flag (removed by this spec) is intentionally gone.
	failedChildren int // F2: count of children/sub-tasks that exited non-zero (mu protected)

	// Log history ring buffer (mu protected)
	logHistory []types.LogEntry
	logHistIdx int // ring buffer write position
	logHistLen int // current valid entry count

	// Token history ring buffer (mu protected)
	tokenHistory []types.TokenSnapshot
	tokenHistIdx int // ring buffer write position
	tokenHistLen int // current valid entry count

	// Signal system (mu protected)
	sigHandlers    map[types.Signal]SignalHandler
	blockedSignals map[types.Signal]struct{}
	pendingSignals map[types.Signal]struct{}
	resumeCh       chan struct{} // nil=not paused; non-nil=paused, close to resume
	pausedAt       time.Time     // when Pause() was called; zero if not paused
	pausedTotal    time.Duration // accumulated paused duration across all pause/resume cycles

	// Thread system (mu protected for threads map, atomic for counter)
	threads    map[types.TID]*Thread
	tidCounter atomic.Uint64

	// Coroutine system (mu protected for coroutines map, atomic for counter)
	coroutines  map[types.CoID]*Coroutine
	coIDCounter atomic.Uint64

	// Differentiation lineage (has its own mutex, not protected by proc.mu)
	lineage *Lineage

	// Stem differentiation results (set once during Spawn, read-only after)
	StemMatches    []StemMatchResult // all candidates with scores
	StemSelected   []string          // skills actually loaded
	StemFromMemory bool              // true if skills came from DiffMemory lookup

	// GDB breakpoint system (mu protected)
	breakpoints      []*Breakpoint
	gdbPauseCh       chan struct{} // nil=not paused; non-nil=paused, close to resume
	gdbStepMode      StepMode      // current single-step mode (StepNone by default)
	gdbModelOverride string        // empty = no override; non-empty = use this model for LLM requests
	gdbEnvVars       map[string]string
	gdbExtraSkills   []string

	// Fallback configuration (Story 23.5)
	FallbackModel       string            // fallback model name
	FallbackProvider    string            // fallback provider name; "" = same as primary
	FallbackDevice      string            // resolved fallback VFS device path; "" = no fallback
	// FallbackResolveError carries the reason fallback device resolution failed
	// at spawn (Story 66.4). NOT persisted — a pure spawn-time carrier consumed
	// by the IPC spawn payload (→ ProgressPayload.Warning) and a delayed
	// events.jsonl event. Empty when resolution succeeded or no fallback config.
	FallbackResolveError string           // spawn-time fallback resolve failure reason; "" = ok
	PrimaryDevice       string            // primary VFS device path (e.g. "/dev/llm/claude")
	Provider            string            // resolved provider name (immutable after spawn)
	Model               string            // resolved model name (immutable after spawn)
	ReasoningEffort     string            // resolved reasoning-effort/level snapshotted from driver (immutable after spawn; Story 55.2); "" = unset
	DriverMeta          map[string]string // runtime metadata from DriverMetaProvider (immutable after spawn)
	AgentTemplate       string            // agent manifest name (immutable after spawn; Story 51.4)
	ContextWindow       int               // per-model context window size (immutable after spawn); 0 = use fallback
	PlanningEnabled     bool              // deprecated: use FeatureFlags.Planning; kept for agent manifest override compat
	FeatureFlags        FeatureFlags      // per-process feature flags; copied from kernel at spawn time
	CompactionDisabled  bool              // true = skip autoCompactIfNeeded (derived from FeatureFlags.Compaction==false)
	Language            string            // preferred response language (from agent manifest); empty = no preference
	ProjectDocInjection bool              // inject project-root AGENTS.md into system prompt (Story 35.7); default true, agent.yaml project_doc:false disables

	// Observation system (Story 27.1)
	FinalSystemPrompt string       // Full system prompt captured on first reasonStep (mu protected)
	stepWriter        *StepWriter  // NDJSON step recorder (mu protected)
	eventWriter       *EventWriter // NDJSON syscall event recorder (mu protected)
	rawWriter         *RawWriter   // NDJSON raw LLM request/response recorder (mu protected; Story 56.1)
	streamCleanups    []func()     // stream observer cleanup hooks (mu protected)
	// eventDropWarned ensures emitEvent logs at most one warning per process
	// when it discovers the EventWriter is nil and a disk drop happens. This
	// converts the previously-silent skip (observe.go:55) into a per-process
	// audit line so daemon-restart placeholders and other niche misroutes
	// (e.g. PrimaryDevice=="" path in subtree.go:resumeOneForSubtree) are
	// visible in daemon stderr. Once-style guard so a chatty script-runner
	// does not flood the log with one line per syscall.
	eventDropWarned atomic.Bool

	// Heartbeat liveness (Story 30.5) — mu protected
	LastHeartbeat time.Time     // updated at each step start
	StepTimeout   time.Duration // 0 = disabled; >0 = stale threshold

	// Checkpoint system (Story 30.2) — fire-and-forget async write per step
	checkpointErrCh chan error // buffered cap=1; carries last async write error
	stepsDir        string     // resolved directory for checkpoint writes; "" = no checkpoint
	scratchDir      string     // per-process temp directory for intermediate files; "" = not created

	// Periodic checkpoint throttling (Story 42.2) — mu protected
	lastCheckpointStep int       // last step at which checkpoint was written (0 = never)
	lastCheckpointTime time.Time // last successful checkpoint dispatch timestamp

	// Two-phase shutdown (Story 31.3)
	GracePeriod     time.Duration // 0 = use DefaultGracePeriod; >0 = custom grace period for SIGTERM
	shutdownStarted atomic.Bool   // CAS guard: prevents duplicate twoPhaseShutdown goroutines
	IPCPersist      bool          // true = persist IPC messages to disk (set for permanent supervised processes)

	// Orchestration metadata (Story 34.7) — immutable after spawn, no locking needed
	ComposeNode   string   // compose node name (e.g. "summarizer"), empty = not compose
	ComposeDeps   []string // depends_on node names (e.g. ["researcher", "analyst"])
	PipelineIndex int      // 0-based stage index in pipeline (only meaningful when PipelineTotal > 0)
	PipelineTotal int      // total stages in pipeline, 0 = not pipeline

	// Suspend system (Story 30.3) — atomic flag for graceful suspend
	suspendRequested atomic.Bool // set by Suspend(), checked by reasonStep
	SuspendReason    string      // reason for suspension (mu protected)

	// Resume notification (Story 44.2) — mu protected. resumedCh is closed by
	// Unsuspend() so a parked script-runner ScriptExecutor (waiting at a
	// statement boundary) wakes up. Each Suspend cycle hands out a fresh chan:
	// ResumedCh() returns the CURRENT chan, callers MUST re-fetch it after each
	// Resume. resumedClose guards the one-shot close so a duplicate Unsuspend
	// (illegal-transition path) never double-closes the same chan.
	resumedCh    chan struct{}
	resumedClose *sync.Once

	// Last completed reasoning step (Story 44.1 code review F5) — atomic so
	// reasonStep can write without holding proc.mu, and so suspend/resume
	// helpers can snapshot the value without coordinating locks. Reads 0 when
	// the process has never completed a step (fresh spawn). Used by
	// resumeOneForSubtree to decide where to restart the loop after an
	// in-memory SIGPAUSE/SIGRESUME cycle, instead of starting at step 1 and
	// overwriting steps.jsonl entries.
	LastCompletedStep atomic.Int64

	// Context compact (Story 31.2) — mu protected
	CompactThreshold      float64                          // 0 = use default (80.0); >0 = trigger compact when TokenUsage > threshold
	SlotCompactThreshold  float64                          // 0 = use default (80.0); >0 = trigger compact when slot usage% > threshold
	BackpressureThreshold float64                          // 0 = use default (70.0); >0 = inject backpressure section when slot usage% > threshold
	CompactTimeout        time.Duration                    // 0 = use default (30s); >0 = timeout for compact LLM call
	ReadFileState         map[string]rnixctx.ReadFileEntry // tracks recently read files for post-compact restore
	compactMu             sync.Mutex                       // prevents concurrent compact operations (auto + manual IPC)

	// Step-level cancel (Story 30.6) — cancel current LLM call without killing process
	stepCancel context.CancelFunc // mu protected; cancel for current step's LLM call

	// Tool calling support (immutable after Spawn)
	HasSections bool                     // true when SectionRegistry is attached to context
	sections    *rnixctx.SectionRegistry // prompt section registry (nil for bare spawns)
	// synthesizedPrompt records whether this process's system prompt was
	// produced by rehydrate.go's fallback path (no process-meta.json on
	// disk, sections re-registered from skill registry + mcpConfigs).
	// reattachMCPMounts uses this to decide whether to rebuild + SetSystemPrompt
	// after mount pruning so a legacy snapshot whose synthesis ran BEFORE
	// mount pruning does not leave the LLM with phantom tool advertisements.
	// Story 48.1 code-review P5. Protected by proc.mu.
	synthesizedPrompt bool
	toolMap           map[string]toolMapping // tool name → VFS path mapping; immutable after Spawn
	nativeToolDefs    []vfs.ToolDef          // collected ToolDefs for req.Tools; immutable after Spawn
	mcpDevicePaths    []string               // MCP device paths for mixed mode text injection; immutable after Spawn
	mcpConfigs        []vfs.MCPConfig        // MCP server configs for Instructions injection; immutable after Spawn

	// Project configuration (Story 25.3) — immutable after spawn, no locking needed
	ProjectConfig *config.ProjectConfig

	mu            sync.Mutex
	cancel        context.CancelFunc
	ctx           context.Context
	wg            sync.WaitGroup
	reapOnce      sync.Once     // ensures reap executes at most once
	writebackOnce sync.Once     // ensures writeback submit runs at most once (Story 35.3)
	terminateOnce sync.Once     // ensures terminated channel is closed exactly once
	terminated    chan struct{} // closed when process transitions to Zombie; broadcast to all waiters
}

// NewProcess creates a new process in the Created state with a unique PID.
func NewProcess(ppid types.PID, intent string, skills []string) *Process {
	ctx, cancel := context.WithCancel(context.Background())
	return &Process{
		PID:                 nextPID(),
		UUID:                uuid.Must(uuid.NewV7()).String(),
		PPID:                ppid,
		State:               types.StateCreated,
		Intent:              intent,
		Skills:              skills,
		PlanningEnabled:     true,
		ProjectDocInjection: true,
		FeatureFlags:        FullFeatureFlags(),
		Children:            []types.PID{},
		FDTable:             make(map[types.FD]vfs.VFSFile),
		DebugChan:           make(chan types.SyscallEvent, 256),
		LogChan:             make(chan types.LogEntry, 256),
		Done:                make(chan ExitStatus, 1),
		terminated:          make(chan struct{}),
		resumedCh:           make(chan struct{}),
		resumedClose:        &sync.Once{},
		CreatedAt:           time.Now(),
		ctx:                 ctx,
		cancel:              cancel,
	}
}

// validTransitions defines the legal state transitions.
var validTransitions = map[types.ProcessState][]types.ProcessState{
	types.StateCreated:   {types.StateRunning},
	types.StateRunning:   {types.StateZombie, types.StateSuspended},
	types.StateZombie:    {types.StateDead},
	types.StateSuspended: {types.StateRunning, types.StateDead},
	// StateDead has no valid transitions
}

// GetPID returns the process's own PID.
// This is the Rnix equivalent of Unix getpid(2). Since PID is immutable after
// creation, no locking is required.
func (p *Process) GetPID() types.PID {
	return p.PID
}

// GetState returns the current process state in a thread-safe manner.
func (p *Process) GetState() types.ProcessState {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.State
}

// GetFinalSystemPrompt returns the captured system prompt (thread-safe).
func (p *Process) GetFinalSystemPrompt() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.FinalSystemPrompt
}

// GetSuspendReason returns the suspend reason (thread-safe).
func (p *Process) GetSuspendReason() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.SuspendReason
}

// SetSuspendReason updates the suspend reason (thread-safe).
func (p *Process) SetSuspendReason(reason string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.SuspendReason = reason
}

// GetNativeToolDefs returns the native tool definitions (immutable after Spawn).
func (p *Process) GetNativeToolDefs() []vfs.ToolDef {
	return p.nativeToolDefs
}

// GetProjectConfig returns the project config (immutable after Spawn).
func (p *Process) GetProjectConfig() *config.ProjectConfig {
	return p.ProjectConfig
}

// FDSnapshot represents a point-in-time copy of a file descriptor entry.
type FDSnapshot struct {
	FD         types.FD
	DevicePath string
}

// GetFDSnapshot returns a thread-safe snapshot of the current FD table.
// Collects FD-to-file references under lock, then calls Stat() outside the lock
// to avoid potential deadlock if a VFS driver acquires another lock in Stat().
func (p *Process) GetFDSnapshot() []FDSnapshot {
	p.mu.Lock()
	type fdRef struct {
		fd   types.FD
		file vfs.VFSFile
	}
	refs := make([]fdRef, 0, len(p.FDTable))
	for fd, file := range p.FDTable {
		refs = append(refs, fdRef{fd: fd, file: file})
	}
	p.mu.Unlock()

	entries := make([]FDSnapshot, 0, len(refs))
	for _, r := range refs {
		devPath := ""
		if r.file != nil {
			if stat, err := r.file.Stat(); err == nil {
				devPath = stat.DevicePath
			}
		}
		entries = append(entries, FDSnapshot{FD: r.fd, DevicePath: devPath})
	}
	slices.SortFunc(entries, func(a, b FDSnapshot) int { return int(a.FD) - int(b.FD) })
	return entries
}

// GetDetailSnapshot returns a thread-safe snapshot of process fields needed for detail view.
type DetailSnapshot struct {
	PID             types.PID
	UUID            string
	PPID            types.PID
	State           types.ProcessState
	Intent          string
	Provider        string
	Model           string
	ReasoningEffort string // Story 55.2: snapshotted reasoning-effort/level for detail view
	CreatedAt       time.Time
	DeadAt          time.Time
	PausedTotal     time.Duration
	Skills          []string
	AllowedDevices  []string
	// DeniedDevices mirrors AllowedDevices in this observability snapshot so the
	// detail view reflects a resumed process's device blocklist (Story 37.6). It
	// is captured at the kernel layer for symmetry; IPC/dashboard surfacing stays
	// out of this story's kernel/+vfs/ persistence scope.
	DeniedDevices []string
	// AllowedTools mirrors the process authoritative tool-name whitelist
	// (Story 54.1) into the observability snapshot so the detail view / IPC
	// wire can surface a process's runtime tool set alongside AllowedDevices.
	AllowedTools    []string
	CtxID           types.CtxID
	TokensUsed      int
	LastInputTokens int
	ContextBudget   int
	MaxTokens       int64
	MaxCost         float64
	UsedCost        float64
	ComposeNode     string
	ComposeDeps     []string
	PipelineIndex   int
	PipelineTotal   int

	// Story 42.3 — resume lineage fields (populated when process is the result
	// of a fork/resume operation; empty otherwise).
	OriginUUID      string
	ResumedFromStep int
	DriverMeta      map[string]string
	FeatureProfile  string
}

// GetDetailSnapshot returns a thread-safe copy of process detail fields.
func (p *Process) GetDetailSnapshot() DetailSnapshot {
	p.mu.Lock()
	defer p.mu.Unlock()
	return DetailSnapshot{
		PID:             p.PID,
		UUID:            p.UUID,
		PPID:            p.PPID,
		State:           p.State,
		Intent:          p.Intent,
		Provider:        p.Provider,
		Model:           p.Model,
		ReasoningEffort: p.ReasoningEffort,
		CreatedAt:       p.CreatedAt,
		DeadAt:          p.DeadAt,
		PausedTotal:     p.pausedTotal,
		Skills:          append([]string(nil), p.Skills...),
		AllowedDevices:  append([]string(nil), p.AllowedDevices...),
		DeniedDevices:   append([]string(nil), p.DeniedDevices...),
		AllowedTools:    append([]string(nil), p.AllowedTools...),
		CtxID:           p.CtxID,
		TokensUsed:      p.TokensUsed,
		LastInputTokens: p.LastInputTokens,
		ContextBudget:   p.ContextBudget,
		MaxTokens:       p.Budget.MaxTokens,
		MaxCost:         p.Budget.MaxCost,
		UsedCost:        p.Budget.UsedCost,
		ComposeNode:     p.ComposeNode,
		ComposeDeps:     append([]string(nil), p.ComposeDeps...),
		PipelineIndex:   p.PipelineIndex,
		PipelineTotal:   p.PipelineTotal,
		OriginUUID:      p.OriginUUID,
		ResumedFromStep: p.ResumedFromStep,
		DriverMeta:      p.DriverMeta,
		FeatureProfile:  p.FeatureFlags.ProfileName,
	}
}

// SetFinalSystemPrompt sets the captured system prompt (thread-safe).
func (p *Process) SetFinalSystemPrompt(s string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.FinalSystemPrompt = s
}

// SetNativeToolDefs sets the native tool definitions.
func (p *Process) SetNativeToolDefs(defs []vfs.ToolDef) {
	p.nativeToolDefs = defs
}

// Finish is a test convenience that records a result, writes to Done, and transitions to Zombie.
func (p *Process) Finish(result string, code int, err error) {
	p.mu.Lock()
	p.Result = result
	p.mu.Unlock()
	exit := ExitStatus{Code: code, Reason: result, Err: err}
	_ = p.Terminate(exit)
	select {
	case p.Done <- exit:
	default:
	}
}

// transitionLocked attempts to move the process to the target state.
// Caller must hold p.mu.
func (p *Process) transitionLocked(target types.ProcessState) error {
	if slices.Contains(validTransitions[p.State], target) {
		p.State = target
		return nil
	}

	return NewSyscallError(
		"transition",
		p.PID,
		"",
		fmt.Errorf("illegal transition: %d → %d", p.State, target),
		types.ErrInternal,
	)
}

// Transition attempts to move the process to the target state.
// Returns *SyscallError if the transition is illegal.
func (p *Process) Transition(target types.ProcessState) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.transitionLocked(target)
}

// Start transitions the process from Created to Running.
func (p *Process) Start() error {
	return p.Transition(types.StateRunning)
}

// Terminate transitions the process from Running to Zombie and records the exit status.
// It also closes the terminated channel (broadcast) so twoPhaseShutdown and other
// zero-or-more waiters can detect process exit without racing on the one-shot Done channel.
func (p *Process) Terminate(exit ExitStatus) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if err := p.transitionLocked(types.StateZombie); err != nil {
		return err
	}
	p.Exit = &exit
	p.terminateOnce.Do(func() { close(p.terminated) })
	return nil
}

// Reap transitions the process from Zombie to Dead.
func (p *Process) Reap() error {
	return p.Transition(types.StateDead)
}

// closeTerminated closes the terminated broadcast channel exactly once.
// Called by Terminate() for normal exit paths. Also safe to call from
// alternative exit paths (e.g., killSuspendedProcess) via Once.
func (p *Process) closeTerminated() {
	p.terminateOnce.Do(func() { close(p.terminated) })
}

// Cancel cancels the process context, signaling the reasoning goroutine to stop.
func (p *Process) Cancel() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cancel != nil {
		p.cancel()
	}
}

// IsCancelled returns true if the process context has been cancelled.
func (p *Process) IsCancelled() bool {
	p.mu.Lock()
	ctx := p.ctx
	p.mu.Unlock()
	if ctx == nil {
		return false
	}
	return ctx.Err() != nil
}

// CancelledCh returns a channel that is closed when the process context is cancelled.
// Returns nil if the process has no context.
func (p *Process) CancelledCh() <-chan struct{} {
	p.mu.Lock()
	ctx := p.ctx
	p.mu.Unlock()
	if ctx == nil {
		return nil
	}
	return ctx.Done()
}

// TerminatedCh returns a channel that is closed when the process reaches a
// terminal state (Zombie via Terminate, or Dead via killSuspendedProcess).
// Broadcast semantics: safe for any number of concurrent waiters; never
// consumes proc.Done. By the time this channel is closed, proc.Exit is
// guaranteed non-nil (both close sites assign Exit under p.mu before closing).
// The channel is unconditionally allocated in NewProcess, so no nil guard or
// lock is needed (unlike CancelledCh, whose ctx can be nil).
func (p *Process) TerminatedCh() <-chan struct{} {
	return p.terminated
}

// AddChild appends a child PID to the Children slice (thread-safe).
func (p *Process) AddChild(pid types.PID) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.Children = append(p.Children, pid)
}

// MarkFailedChild records that a spawned child exited abnormally (non-zero exit
// code). ActionComplete consults this so a parent that orchestrated failing
// children does not report success with exit 0 (F2: assessment integrity —
// child failures must not be masked behind the parent's own `complete`).
func (p *Process) MarkFailedChild() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.failedChildren++
}

// RemoveChild removes a child PID from the Children slice (thread-safe).
func (p *Process) RemoveChild(pid types.PID) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for i, c := range p.Children {
		if c == pid {
			p.Children = append(p.Children[:i], p.Children[i+1:]...)
			return
		}
	}
}

// GetChildren returns a copy of the Children slice (thread-safe).
func (p *Process) GetChildren() []types.PID {
	p.mu.Lock()
	defer p.mu.Unlock()
	result := make([]types.PID, len(p.Children))
	copy(result, p.Children)
	return result
}

// --- Lineage methods (lineage has its own mutex) ---

// GetLineage returns the process lineage, or nil if not a differentiated process.
func (p *Process) GetLineage() *Lineage {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.lineage
}

// SetLineage sets the process lineage.
func (p *Process) SetLineage(l *Lineage) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lineage = l
}

// --- Signal disposition (atomic check under single lock) ---

// signalDisposition describes what should happen when a signal is delivered.
type signalDisposition int

const (
	dispBlocked signalDisposition = iota // signal is blocked, added to pending
	dispHandler                          // custom handler registered
	dispDefault                          // no handler, use default behavior
)

// resolveSignalDisposition atomically determines the disposition for a signal
// under a single lock hold. This prevents TOCTOU races between IsBlocked/AddPending
// and between GetHandler checks. SIGKILL always returns dispDefault (cannot be
// blocked or handled).
func (p *Process) resolveSignalDisposition(sig types.Signal) (signalDisposition, SignalHandler) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Check if blocked (SIGKILL can never be blocked due to SigBlock validation)
	if p.blockedSignals != nil {
		if _, ok := p.blockedSignals[sig]; ok {
			if p.pendingSignals == nil {
				p.pendingSignals = make(map[types.Signal]struct{})
			}
			p.pendingSignals[sig] = struct{}{}
			return dispBlocked, nil
		}
	}

	// SIGKILL cannot have custom handler — force-kill semantics
	if !sig.Blockable() {
		return dispDefault, nil
	}

	// Check custom handler
	if p.sigHandlers != nil {
		if h, ok := p.sigHandlers[sig]; ok {
			return dispHandler, h
		}
	}

	return dispDefault, nil
}

// --- Signal state methods (all thread-safe via mu) ---

// SetHandler registers a custom signal handler for the given signal.
func (p *Process) SetHandler(sig types.Signal, handler SignalHandler) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.sigHandlers == nil {
		p.sigHandlers = make(map[types.Signal]SignalHandler)
	}
	p.sigHandlers[sig] = handler
}

// GetHandler returns the custom handler for the given signal, if any.
func (p *Process) GetHandler(sig types.Signal) (SignalHandler, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.sigHandlers == nil {
		return nil, false
	}
	h, ok := p.sigHandlers[sig]
	return h, ok
}

// BlockSignal adds the signal to the blocked set.
func (p *Process) BlockSignal(sig types.Signal) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.blockedSignals == nil {
		p.blockedSignals = make(map[types.Signal]struct{})
	}
	p.blockedSignals[sig] = struct{}{}
}

// UnblockSignal removes the signal from the blocked set and returns whether
// there was a pending signal of this type.
func (p *Process) UnblockSignal(sig types.Signal) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.blockedSignals != nil {
		delete(p.blockedSignals, sig)
	}
	_, hasPending := p.pendingSignals[sig]
	return hasPending
}

// IsBlocked reports whether the signal is in the blocked set.
func (p *Process) IsBlocked(sig types.Signal) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.blockedSignals == nil {
		return false
	}
	_, ok := p.blockedSignals[sig]
	return ok
}

// AddPending adds the signal to the pending set.
func (p *Process) AddPending(sig types.Signal) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.pendingSignals == nil {
		p.pendingSignals = make(map[types.Signal]struct{})
	}
	p.pendingSignals[sig] = struct{}{}
}

// HasPending reports whether the signal is in the pending set.
func (p *Process) HasPending(sig types.Signal) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.pendingSignals == nil {
		return false
	}
	_, ok := p.pendingSignals[sig]
	return ok
}

// ClearPending removes the signal from the pending set.
func (p *Process) ClearPending(sig types.Signal) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.pendingSignals != nil {
		delete(p.pendingSignals, sig)
	}
}

// Pause creates a resumeCh channel, putting the process into paused state.
// Idempotent — if already paused, this is a no-op.
//
// Note: as of Epic 44.1, business code must NOT call this method directly.
// Use k.Suspend(pid) / k.SuspendSubtree(pid) / k.ResumeSubtree(pid) or the
// SIGPAUSE / SIGRESUME signals instead — those route through the canonical
// Suspended state machine, emit standard Suspend/Resume events, propagate
// to descendants, and survive daemon restart (44.3). The resumeCh-based
// SoftPause path here is retained only as an internal sync primitive for
// kernel/reason.go:WaitIfPaused defensive checks and for kernel/coroutine.go
// (which uses a separately-typed resumeCh and is unaffected by this note).
func (p *Process) Pause() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.resumeCh == nil {
		p.resumeCh = make(chan struct{})
		p.pausedAt = time.Now()
	}
}

// Resume closes the resumeCh channel, unblocking any goroutine waiting on WaitIfPaused.
// Idempotent — if not paused, this is a no-op.
//
// Note: as of Epic 44.1, business code must NOT call this method directly.
// See the Pause godoc above — k.ResumeSubtree(pid) or SIGRESUME is the
// supported wake path.
func (p *Process) Resume() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.resumeCh != nil {
		if !p.pausedAt.IsZero() {
			p.pausedTotal += time.Since(p.pausedAt)
		}
		close(p.resumeCh)
		p.resumeCh = nil
		p.pausedAt = time.Time{}
	}
}

// WaitIfPaused returns the resume channel if the process is paused.
// Returns nil if not paused (caller should skip select).
func (p *Process) WaitIfPaused() <-chan struct{} {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.resumeCh
}

// IsPaused reports whether the process is currently paused.
func (p *Process) IsPaused() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.resumeCh != nil
}

// TouchHeartbeat updates LastHeartbeat to now (thread-safe).
//
// Production callers: driver streaming events (kernel/observe.go) — these are
// the honest activity signals from the reasoning loop. Note that the field
// LastHeartbeat itself is also written directly (without going through this
// method) at spawn-time seeding (kernel/spawn.go), reasonStep boundaries
// (kernel/reason.go), Resume / Resume-subtree (kernel/resume.go,
// kernel/subtree.go), and suspended-state load (kernel/load_suspended.go);
// see those sites for their respective invariants. Fake-heartbeat ticker paths
// in ipc/server_pipeline.go that previously called this on behalf of parent
// processes were removed by Story 45.3 (P4 daemon-passive).
func (p *Process) TouchHeartbeat() {
	p.mu.Lock()
	p.LastHeartbeat = time.Now()
	p.mu.Unlock()
}

// AddStreamUsage folds one mid-stream usage delta into the live ledger for the
// current step (Story 66.6). tokensDelta is added to StreamTokensUsed;
// inputTokens overwrites StreamInputTokens (most-recent semantics — the driver
// reports the round-trip's prompt size, not a delta). prov is merged into
// UsageProvenance via mergeProvenance (only ever lowers, never raises). Called
// from the driver stream handler; heartbeat is refreshed separately by the
// handler's TouchHeartbeat.
func (p *Process) AddStreamUsage(tokensDelta, inputTokens int, prov string) {
	p.mu.Lock()
	p.StreamTokensUsed += tokensDelta
	if inputTokens > 0 {
		p.StreamInputTokens = inputTokens
	}
	p.UsageProvenance = mergeProvenance(p.UsageProvenance, prov)
	p.mu.Unlock()
}

// IncToolCallCount bumps the process-level tool-call liveness counter by one
// (Story 66.6). Called once per CLI DriverToolCall flush and once per kernel
// native VFS tool dispatch.
func (p *Process) IncToolCallCount() {
	p.mu.Lock()
	p.ToolCallCount++
	p.mu.Unlock()
}

// mergeProvenance combines the current and incoming usage-provenance labels,
// keeping the LOWEST fidelity of the two (Story 66.6 拍板 6). Empty current →
// take the incoming value. Unknown labels rank 0 (below every known level) so a
// future / mistyped value is held conservatively and never used to lower a
// known rank silently — but if current is known and incoming is unknown, the
// known value is preserved (we do not downgrade to an unrecognised label).
func mergeProvenance(cur, incoming string) string {
	if incoming == "" {
		return cur
	}
	if cur == "" {
		return incoming
	}
	if cur == incoming {
		return cur
	}
	cr, ck := provenanceRank(cur)
	ir, ik := provenanceRank(incoming)
	// If either side is unrecognised, keep the recognised one rather than
	// blindly lowering to an unknown label.
	if !ck {
		return incoming
	}
	if !ik {
		return cur
	}
	if ir < cr {
		return incoming
	}
	return cur
}

// provenanceRank maps a known provenance label to its fidelity rank (higher =
// more authoritative) and reports whether the label was recognised.
func provenanceRank(p string) (int, bool) {
	switch p {
	case vfs.UsageProvenanceEstimated:
		return 1, true
	case vfs.UsageProvenanceCLIStream:
		return 2, true
	case vfs.UsageProvenanceAPIResponse:
		return 3, true
	default:
		return 0, false
	}
}

// AttachEventWriter installs an EventWriter on a process whose driver is not
// driven by reasonStep — currently the script-runner spawned with
// SpawnOpts{SkipReasonLoop: true} (Story 43.2 OBS-1). Concurrent emitEvent
// readers acquire p.mu, so this setter is race-safe against the publish path.
//
// If a writer is already attached, it is Flush+Close'd before being replaced
// so a double-attach (intentional or racy with reasonStep self-init) does not
// leak the prior FD. Close errors are logged but otherwise non-fatal.
//
// Reasoning processes that spawn WITHOUT a pre-supplied EventWriterFactory now
// auto-attach their EventWriter through this setter during the early-attach
// window (Story 56.5, kernel/spawn.go), so this is the normal attach path for
// them. Mutual exclusion with reasonStep's own late init is preserved by
// attachStepObservation's `if proc.eventWriter == nil` guard (kernel/reason.go),
// which no-ops once an EventWriter is already attached — preventing a second
// writer / double FD. Reap (kernel/reap.go) closes whichever EventWriter
// happens to be attached, so no extra cleanup is required at the call site.
func (p *Process) AttachEventWriter(ew *EventWriter) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.eventWriter != nil && p.eventWriter != ew {
		if err := p.eventWriter.Close(); err != nil {
			log.Printf("[event_writer] close prior writer pid=%d: %v", p.PID, err)
		}
	}
	p.eventWriter = ew
}

// AttachRawWriter installs a RawWriter on the process. Mirrors
// AttachEventWriter; called by spawn paths (Story 56.1) so the kernel
// reasonStep capture hook can write raw LLM request/response records to
// <baseDir>/steps/<uuid>/raw.jsonl as soon as the first LLM Write succeeds.
//
// A double-attach closes the prior writer to avoid leaking the file FD; close
// errors are logged but otherwise non-fatal. Safe to call before reap; reap
// (kernel/reap.go) closes whichever writer happens to be attached.
func (p *Process) AttachRawWriter(rw *RawWriter) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.rawWriter != nil && p.rawWriter != rw {
		if err := p.rawWriter.Close(); err != nil {
			log.Printf("[raw_writer] close prior writer pid=%d: %v", p.PID, err)
		}
	}
	p.rawWriter = rw
}

// DetachAndCloseEventWriter flushes and closes the currently attached
// EventWriter (if any) and clears the field. Used by the script-runner
// Suspend path (Story 43.2 D3) where the process transitions to Suspended
// outside of Reap and the FD would otherwise leak for the full Suspended
// lifetime. After detach the process's emit path silently no-ops on disk
// writes (DebugChan / recordMgr / immune still fire), which matches the
// "no writer means best-effort" semantics in emitEvent.
//
// Safe to call multiple times; safe to call when no writer is attached.
// EventWriter is opened with O_APPEND, so a later AttachEventWriter using
// the same UUID preserves history.
func (p *Process) DetachAndCloseEventWriter() {
	p.mu.Lock()
	ew := p.eventWriter
	p.eventWriter = nil
	p.mu.Unlock()
	if ew != nil {
		if err := ew.Close(); err != nil {
			log.Printf("[event_writer] detach-close pid=%d: %v", p.PID, err)
		}
	}
}

// LastHeartbeatSnapshot returns the current LastHeartbeat value under
// proc.mu — use this instead of reading the field directly when racing
// with any of LastHeartbeat's writers. Post-Story 45.3 the writer set is:
// driver streaming (via TouchHeartbeat at kernel/observe.go), reasonStep
// boundary updates (kernel/reason.go), spawn-time seeding (kernel/spawn.go),
// Resume / Resume-subtree (kernel/resume.go, kernel/subtree.go), and
// suspended-state load (kernel/load_suspended.go).
func (p *Process) LastHeartbeatSnapshot() time.Time {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.LastHeartbeat
}

// IsSuspendRequested reports whether suspend has been requested for this process.
func (p *Process) IsSuspendRequested() bool {
	return p.suspendRequested.Load()
}

// RequestSuspend sets the suspend-requested flag without touching state.
// suspendOneForSubtree (kernel/subtree.go) is the production writer; this
// exported setter lets the ipc package drive the same atomic in unit tests
// that exercise the CancelledCh-watcher race window (Story 44.2 AC#1.b).
func (p *Process) RequestSuspend() {
	p.suspendRequested.Store(true)
}

// ResumedCh returns the channel that Unsuspend() closes when this process
// transitions back to Running. ScriptExecutor parks on it at a statement
// boundary while the subtree is Suspended (Story 44.2 AC#3 / AC#4).
//
// Each Suspend cycle hands out a DIFFERENT chan instance — callers MUST
// re-fetch via ResumedCh() after every Resume, never cache it across cycles.
// Safe for concurrent readers (parallel-block workers may all observe the
// same close).
func (p *Process) ResumedCh() <-chan struct{} {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.resumedCh
}

// SetStepCancel sets the cancel function for the current step's LLM call.
func (p *Process) SetStepCancel(cancel context.CancelFunc) {
	p.mu.Lock()
	p.stepCancel = cancel
	p.mu.Unlock()
}

// CancelStep cancels the current step's LLM call without affecting the process context.
func (p *Process) CancelStep() {
	p.mu.Lock()
	if p.stepCancel != nil {
		p.stepCancel()
	}
	p.mu.Unlock()
}

// AddStreamCleanup registers best-effort cleanup for stream observers attached
// to the process's LLM FD. Cleanup functions must be idempotent; they may be
// called by driver terminal events and by reasonStep failure paths.
func (p *Process) AddStreamCleanup(fn func()) {
	if fn == nil {
		return
	}
	p.mu.Lock()
	p.streamCleanups = append(p.streamCleanups, fn)
	p.mu.Unlock()
}

// RunStreamCleanups runs registered stream cleanup hooks outside proc.mu.
func (p *Process) RunStreamCleanups() {
	p.mu.Lock()
	cleanups := append([]func(){}, p.streamCleanups...)
	p.mu.Unlock()
	for _, fn := range cleanups {
		fn()
	}
}

// Suspend transitions the process from Running to Suspended.
func (p *Process) Suspend() error {
	return p.Transition(types.StateSuspended)
}

// Unsuspend transitions the process from Suspended to Running.
//
// Order matters: the suspendRequested flag is cleared only AFTER a successful
// state transition. If Transition fails (e.g. the process raced to Dead
// concurrently) we leave suspendRequested as-is so the reasonStep defer path
// observes a consistent (suspendRequested=true, state=Dead) tuple instead of
// a phantom "cleared but never resumed" state.
func (p *Process) Unsuspend() error {
	if err := p.Transition(types.StateRunning); err != nil {
		return err
	}
	p.suspendRequested.Store(false)

	// Story 44.2: wake any ScriptExecutor parked on ResumedCh(). Ordering is
	// load-bearing — suspendRequested.Store(false) above MUST happen before
	// the close so a waiter that wakes and re-checks IsSuspendRequested() sees
	// false (otherwise it would re-park immediately, an infinite loop). Under
	// proc.mu we first install a FRESH chan + once for the next Suspend cycle,
	// then close the OLD chan outside the critical-section snapshot. The
	// sync.Once guards against a double Unsuspend double-closing the same chan.
	p.mu.Lock()
	oldCh := p.resumedCh
	oldClose := p.resumedClose
	p.resumedCh = make(chan struct{})
	p.resumedClose = &sync.Once{}
	p.mu.Unlock()
	oldClose.Do(func() { close(oldCh) })
	return nil
}

// GetStepsDir returns the checkpoint steps directory (thread-safe).
func (p *Process) GetStepsDir() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.stepsDir
}

// GetScratchDir returns the per-process scratchpad directory path.
func (p *Process) GetScratchDir() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.scratchDir
}

// DefaultCompactThreshold is the default token usage percentage that triggers auto-compact.
const DefaultCompactThreshold = 80.0

// DefaultGracePeriod is the default grace period for two-phase SIGTERM shutdown.
const DefaultGracePeriod = 10 * time.Second

// effectiveGracePeriod returns the configured grace period, defaulting to 10s.
func (p *Process) effectiveGracePeriod() time.Duration {
	if p.GracePeriod > 0 {
		return p.GracePeriod
	}
	return DefaultGracePeriod
}

// effectiveCompactThreshold returns the configured compact threshold, defaulting to 80%.
func (p *Process) effectiveCompactThreshold() float64 {
	if p.CompactThreshold > 0 {
		return p.CompactThreshold
	}
	return DefaultCompactThreshold
}

// --- Story 69.4: proactive leaked-tool-result reclamation gates ---
//
// proactiveReclaimWatermarkRatio derives the reclamation watermark from the
// compaction threshold rather than declaring a second absolute number: 0.75 of
// the compact threshold, i.e. 60% under the 80% defaults. It is a RATIO on
// purpose. A subtraction (threshold-20) goes negative once an operator
// configures a threshold of 20 or less, and a negative watermark fires on every
// single step — the same倒挂 trap Story 69.1's tier boundary formula avoids.
//
// The point of firing below the compact threshold is to reclaim while there is
// still slack, so the (expensive) compaction is deferred rather than merely
// preceded.
const proactiveReclaimWatermarkRatio = 0.75

// minReclaimTokens is the absolute floor on a reclamation batch. Below roughly
// one leaked tool result's worth of payload, rewriting cold-zone bytes costs
// more than it saves: every rewrite invalidates the provider's cached prompt
// prefix (drivers/llm/anthropic.go applyCacheControlToMessageHistory), so the
// next request is billed as a full recompute.
const minReclaimTokens = 1000

// minReclaimRatioPct scales that floor with the context: on a large context a
// 1000-token batch is noise, so require the candidates to be worth at least 20%
// of the RECLAIMABLE pool (usage minus the system prompt, which can never be
// reclaimed — counting it would raise the gate above what the message history
// can supply on a system-prompt-heavy agent). 20% is the point where one
// reclamation plausibly defers a compaction by several steps, which is what pays
// for the single cache invalidation it causes.
const minReclaimRatioPct = 20

const DefaultSlotCompactThreshold = 80.0

// effectiveSlotCompactThreshold returns the configured slot compact threshold, defaulting to 80%.
func (p *Process) effectiveSlotCompactThreshold() float64 {
	if p.SlotCompactThreshold > 0 {
		return p.SlotCompactThreshold
	}
	return DefaultSlotCompactThreshold
}

const DefaultCompactTimeout = 30 * time.Second

func (p *Process) effectiveCompactTimeout() time.Duration {
	if p.CompactTimeout > 0 {
		return p.CompactTimeout
	}
	return DefaultCompactTimeout
}

const DefaultBackpressureThreshold = 70.0

func (p *Process) effectiveBackpressureThreshold() float64 {
	if p.BackpressureThreshold > 0 {
		return p.BackpressureThreshold
	}
	return DefaultBackpressureThreshold
}

// effectiveLoopThreshold returns the fine-grain loop detection threshold.
//
// ⚠️ The test is `!= 0`, not `> 0`. A negative value is a deliberate "disable
// this track" request and must reach NewLoopDetector unchanged; treating it as
// unset would silently fall back to the default and re-enable the very detection
// the operator switched off.
func (p *Process) effectiveLoopThreshold() int {
	if p.LoopThreshold != 0 {
		return p.LoopThreshold
	}
	return DefaultLoopThreshold
}

// effectiveCoarseLoopThreshold returns the coarse-grain loop detection
// threshold. Same `!= 0` rule as effectiveLoopThreshold.
func (p *Process) effectiveCoarseLoopThreshold() int {
	if p.CoarseLoopThreshold != 0 {
		return p.CoarseLoopThreshold
	}
	return DefaultCoarseLoopThreshold
}

// TryLockCompact attempts to acquire the compact mutex without blocking.
// Returns true if acquired, false if another compact is in progress.
func (p *Process) TryLockCompact() bool {
	return p.compactMu.TryLock()
}

// UnlockCompact releases the compact mutex.
func (p *Process) UnlockCompact() {
	p.compactMu.Unlock()
}

const tokenHistoryCap = 50

// AppendTokenSnapshot records token usage at the current step. Thread-safe.
func (p *Process) AppendTokenSnapshot(step, tokens int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.tokenHistory == nil {
		p.tokenHistory = make([]types.TokenSnapshot, tokenHistoryCap)
	}
	p.tokenHistory[p.tokenHistIdx] = types.TokenSnapshot{
		Step:    step,
		Tokens:  tokens,
		DeltaMs: time.Since(p.CreatedAt).Milliseconds(),
	}
	p.tokenHistIdx = (p.tokenHistIdx + 1) % tokenHistoryCap
	if p.tokenHistLen < tokenHistoryCap {
		p.tokenHistLen++
	}
}

// GetTokenHistory returns a time-ordered copy of the token history. Thread-safe.
func (p *Process) GetTokenHistory() []types.TokenSnapshot {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.tokenHistLen == 0 {
		return nil
	}
	result := make([]types.TokenSnapshot, p.tokenHistLen)
	if p.tokenHistLen < tokenHistoryCap {
		copy(result, p.tokenHistory[:p.tokenHistLen])
	} else {
		start := p.tokenHistIdx
		n := copy(result, p.tokenHistory[start:])
		copy(result[n:], p.tokenHistory[:start])
	}
	return result
}

const logHistoryCap = 256

// AppendLogHistory adds a log entry to the ring buffer. Caller must hold p.mu.
func (p *Process) AppendLogHistory(entry types.LogEntry) {
	if p.logHistory == nil {
		p.logHistory = make([]types.LogEntry, logHistoryCap)
	}
	p.logHistory[p.logHistIdx] = entry
	p.logHistIdx = (p.logHistIdx + 1) % logHistoryCap
	if p.logHistLen < logHistoryCap {
		p.logHistLen++
	}
}

// GetLogHistory returns a time-ordered copy of the log history. Caller must hold p.mu.
func (p *Process) GetLogHistory() []types.LogEntry {
	if p.logHistLen == 0 {
		return nil
	}
	result := make([]types.LogEntry, p.logHistLen)
	if p.logHistLen < logHistoryCap {
		copy(result, p.logHistory[:p.logHistLen])
	} else {
		start := p.logHistIdx // oldest entry
		n := copy(result, p.logHistory[start:])
		copy(result[n:], p.logHistory[:start])
	}
	return result
}

// ClearSignalState cleans up all signal state (handlers, blocked, pending, resume channel).
// Used during process reap.
func (p *Process) ClearSignalState() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sigHandlers = nil
	p.blockedSignals = nil
	p.pendingSignals = nil
	if p.resumeCh != nil {
		close(p.resumeCh)
		p.resumeCh = nil
	}
}

// --- Thread management methods (all thread-safe via mu) ---

// AddThread registers a thread in the process's thread table.
func (p *Process) AddThread(t *Thread) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.threads == nil {
		p.threads = make(map[types.TID]*Thread)
	}
	p.threads[t.TID] = t
}

// GetThread retrieves a thread by TID from the process's thread table.
func (p *Process) GetThread(tid types.TID) (*Thread, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.threads == nil {
		return nil, false
	}
	t, ok := p.threads[tid]
	return t, ok
}

// RemoveThread removes a thread from the process's thread table.
func (p *Process) RemoveThread(tid types.TID) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.threads != nil {
		delete(p.threads, tid)
	}
}

// ClearThreads cancels all threads and waits for them to finish.
// Used during process reap.
func (p *Process) ClearThreads() {
	p.mu.Lock()
	threads := make([]*Thread, 0, len(p.threads))
	for _, t := range p.threads {
		threads = append(threads, t)
	}
	p.threads = nil
	p.mu.Unlock()

	for _, t := range threads {
		t.cancel()
		<-t.Done
	}
}

// --- Coroutine management methods (all thread-safe via mu) ---

// AddCoroutine registers a coroutine in the process's coroutine table.
func (p *Process) AddCoroutine(c *Coroutine) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.coroutines == nil {
		p.coroutines = make(map[types.CoID]*Coroutine)
	}
	p.coroutines[c.CoID] = c
}

// GetCoroutine retrieves a coroutine by CoID from the process's coroutine table.
func (p *Process) GetCoroutine(coID types.CoID) (*Coroutine, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.coroutines == nil {
		return nil, false
	}
	c, ok := p.coroutines[coID]
	return c, ok
}

// RemoveCoroutine removes a coroutine from the process's coroutine table.
func (p *Process) RemoveCoroutine(coID types.CoID) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.coroutines != nil {
		delete(p.coroutines, coID)
	}
}

// ClearCoroutines cleans up all coroutines by closing their resume channels.
// Used during process reap.
//
// Coroutine goroutines can be blocked at two points:
//  1. co.yieldCh <- value (inside yield closure, waiting for caller to read)
//  2. <-co.resumeCh (inside yield closure, waiting for caller to resume)
//
// Closing resumeCh unblocks case 2. For case 1, the goroutine will panic
// on send to closed channel if we close yieldCh, so we use a drain goroutine
// to consume from yieldCh, which unblocks the sender, then the coroutine
// proceeds to <-resumeCh which returns zero value (closed channel).
func (p *Process) ClearCoroutines() {
	p.mu.Lock()
	coroutines := make([]*Coroutine, 0, len(p.coroutines))
	for _, c := range p.coroutines {
		coroutines = append(coroutines, c)
	}
	p.coroutines = nil
	p.mu.Unlock()

	for _, c := range coroutines {
		c.mu.Lock()
		if c.State != coDone {
			// Close resumeCh to unblock coroutines waiting at <-co.resumeCh.
			// This also causes yield closure to return (resume receives zero value),
			// then the coroutine continues and eventually completes or blocks at
			// the next yieldCh send, which the drain goroutine handles.
			close(c.resumeCh)

			// Drain yieldCh to unblock coroutines blocked at co.yieldCh <- value.
			// The goroutine exits when yieldCh is closed (coroutine completes).
			go func(ch chan any) {
				for range ch {
				}
			}(c.yieldCh)
		}
		c.mu.Unlock()
	}
}
