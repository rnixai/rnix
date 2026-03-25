package kernel

import (
	gocontext "context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"maps"
	"path"
	"path/filepath"
	"slices"
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
	"github.com/rnixai/rnix/skills"
	"github.com/rnixai/rnix/vfs"
)

// MountManager defines the interface for mounting/unmounting MCP servers.
type MountManager interface {
	Mount(path string, config vfs.MCPConfig) error
	Unmount(path string) error
	UnmountAll() error
}

// DefaultMaxSteps is the maximum number of reasoning steps before forced completion.
const DefaultMaxSteps = 100

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
	TraceID       types.TraceID // inherited trace ID; empty = no tracing
	ParentSpanID  types.SpanID  // parent process span ID
	Provider      string        // LLM provider override (from CLI --provider); "" = use agent manifest or default "claude"

	PreallocatedCtxID types.CtxID           // non-zero = skip CtxAlloc, use this pre-setup context
	SkipReasonLoop    bool                  // true = don't open LLM device or start reasonStep goroutine
	ProjectConfig     *config.ProjectConfig // project-level config snapshot; nil = global only
}

// toolProtocol is the legacy text-based action protocol, preserved as a
// fallback reference and test baseline. New processes use generateToolProtocol()
// from kernel/toolgen.go which auto-generates the protocol from ToolDefs.
var _ = toolProtocol //nolint:unused
var _ = planProtocol //nolint:unused

const toolProtocol = `

[Action Protocol]
Respond with a JSON object to perform an action, or plain text for your final answer.

Tool call — execute a VFS device:
{"action": "tool_call", "tool": "<vfs-device-path>", "data": {<tool-specific-payload>}}

Available VFS device paths:
  - Read file: tool="/dev/fs/src/main.go", data={}
  - Write file: tool="/dev/fs/docs/output.md", data={"content": "file content here"}
  - List directory: tool="/dev/fs/src", data={"op": "list"}
  - Run command: tool="/dev/shell", data={"command": "ls -la"}
  - LLM call: tool="/dev/llm/<provider>", data={"intent": "..."}
  - MCP tool: tool="/dev/mcp/<server>/<tool>", data={...}

IMPORTANT path rules:
  - /dev/fs paths MUST include the file/dir path after /dev/fs (e.g. /dev/fs/src/main.go). Never use /dev/fs alone.
  - /dev/fs paths are relative to the project working directory. Do NOT include the project name.
  - /dev/shell has no subpath — always use exactly "/dev/shell".

Spawn child process:
{"action": "spawn", "tool": "<child intent>", "data": {"agent": "<name>", "model": "<model>"}}

Complete — finish with a result:
{"action": "complete", "tool": "", "data": {"result": "<final output>"}}

Replan — revise your approach:
{"action": "replan", "tool": "", "data": {"reason": "<why replanning>"}}

Specialize — dynamically load a skill:
{"action": "specialize", "tool": "<skill-name>", "data": {}}

[Skills vs Tools]
Skills are instruction sets, NOT callable VFS devices. They teach you new capabilities.
- To load a skill: use the specialize action above.
- Once loaded, the skill's instructions appear in your conversation. Follow them using available VFS devices.
- Do NOT call skills via /dev/mcp/ or any other device path — skills have no device path.
- If a skill is already loaded, its instructions are already in your system prompt. Act on them directly.

If no action is needed, respond with plain text (your final answer).`

// planProtocol is the legacy plan action protocol text, preserved alongside toolProtocol.
const planProtocol = `

Plan — create an execution plan before acting:
{"action": "plan", "tool": "", "data": {"steps": ["step1", "step2", ...], "reason": "why planning"}}

Use planning when the task requires multiple coordinated steps. For simple tasks, use tool_call directly.`

// ActionType classifies LLM response actions.
type ActionType string

const (
	ActionText       ActionType = "text"
	ActionToolCall   ActionType = "tool_call"
	ActionPlan       ActionType = "plan"
	ActionSpawn      ActionType = "spawn"
	ActionComplete   ActionType = "complete"
	ActionReplan     ActionType = "replan"
	ActionSpecialize ActionType = "specialize"
)

// ReasonAction represents a parsed action from an LLM response.
type ReasonAction struct {
	Type     ActionType
	Content  string
	ToolPath string
	ToolData []byte
}

// llmRequest is the JSON payload written to the LLM VFS device.
// Field names and json tags are compatible with drivers/llm.LLMRequest.
type llmRequest struct {
	Intent       string            `json:"intent"`
	SystemPrompt string            `json:"system_prompt,omitempty"`
	Model        string            `json:"model,omitempty"`
	MaxTurns     int               `json:"max_turns,omitempty"`
	TimeoutMs    int64             `json:"timeout_ms,omitempty"`
	Messages     []rnixctx.Message `json:"messages,omitempty"`
	Tools        []vfs.ToolDef     `json:"tools,omitempty"`
}

// llmToolCall represents a tool invocation in an LLM response.
// Fields and JSON tags are compatible with llm.ToolCall and context.ToolCall.
type llmToolCall struct {
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	Input      map[string]any `json:"input,omitempty"`
	ParseError string         `json:"parse_error,omitempty"`
}

// llmResponse is the JSON payload read from the LLM VFS device.
// Field names and json tags are compatible with drivers/llm.LLMResponse.
type llmResponse struct {
	Content    string        `json:"content"`
	TokensUsed int           `json:"tokens_used"`
	ToolCalls  []llmToolCall `json:"tool_calls,omitempty"`
}

// KernelCallbacks allows the CLI layer to receive progress notifications
// from the kernel without introducing a reverse dependency on internal/ui.
type KernelCallbacks interface {
	OnSpawn(pid types.PID, intent, provider, model, uuid string)
	OnStep(pid types.PID, step int, total int)
	OnStepComplete(pid types.PID, step int, action string, summary string, hasError bool, durationMs float64)
	OnComplete(pid types.PID, result string, exit ExitStatus)
	OnError(pid types.PID, err error)
}

// ProcessManager defines the kernel's process management interface.
// Kill and Wait are added in Story 4.1; GetPID is implemented as a Process method (not interface method)
// since PID is an immutable property of the process itself, not a kernel-level query.
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
	// When non-empty, StepWriter writes to this dir instead of <projectDir>/.rnix/
	stepDataDir string
}

// NewKernel creates a new KernelImpl with the given VFS, context manager, and optional callbacks.
// Pass nil for cb to run in silent mode (no progress notifications).
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
// Used in tests to redirect step data to a temp directory.
func (k *KernelImpl) SetStepDataDir(dir string) {
	k.stepDataDir = dir
}

// GetStepDataDir returns the base directory for step data output.
func (k *KernelImpl) GetStepDataDir() string {
	return k.stepDataDir
}

// resolveLLMDevice returns the VFS device path for the LLM provider and its source.
// providerOverride takes precedence over the agent manifest's provider field.
// Returns "/dev/llm/claude" by default when both are empty.
// When hasProvider is non-nil, validates against the DriverRegistry; when nil,
// allows any provider (backward compatibility for tests without resolver injection).
// The source return value indicates where the provider was resolved from:
// "cli", "agent", "project", or "default".
func (k *KernelImpl) resolveLLMDevice(agent *agents.AgentInfo, providerOverride string) (string, string, error) {
	provider := providerOverride
	source := "cli"
	if provider == "" {
		if agent != nil && agent.Manifest.Models.Provider != "" {
			provider = agent.Manifest.Models.Provider
			source = "agent"
		}
	}
	if provider == "" {
		if k.defaultProvider != "" {
			provider = k.defaultProvider
			source = "project"
		} else {
			provider = "claude"
			source = "default"
		}
	}

	if k.hasProvider != nil && !k.hasProvider(provider) {
		available := "none"
		if k.providerNames != nil {
			names := k.providerNames()
			if len(names) > 0 {
				available = strings.Join(names, ", ")
			}
		}
		return "", "", fmt.Errorf("unsupported LLM provider: %q (available: %s)", provider, available)
	}

	return "/dev/llm/" + provider, source, nil
}

// Spawn creates a new agent process that automatically executes the reasonStep loop.
func (k *KernelImpl) Spawn(intent string, agent *agents.AgentInfo, opts SpawnOpts) (types.PID, error) {
	start := time.Now()

	var skillNames []string
	if agent != nil {
		for _, s := range agent.Skills {
			skillNames = append(skillNames, s.Manifest.Name)
		}
	}
	proc := NewProcess(opts.ParentPID, intent, skillNames)
	// Note: proc.Skills may be updated below after stem differentiation (Story 20.3)

	// Set project config snapshot (Story 25.3) — immutable after spawn
	proc.ProjectConfig = opts.ProjectConfig

	// Register per-process WorkDir for VFS path resolution
	if opts.ProjectConfig != nil && opts.ProjectConfig.ProjectDir != "" {
		k.vfs.SetWorkDir(proc.PID, opts.ProjectConfig.ProjectDir)
	}

	// Maintain parent-child tracking
	if opts.ParentPID > 0 {
		parent, ok := k.GetProcess(opts.ParentPID)
		if !ok {
			return 0, NewSyscallError("Spawn", opts.ParentPID, "", fmt.Errorf("parent process %d not found", opts.ParentPID), types.ErrNotFound)
		}
		parent.AddChild(proc.PID)
	}

	// Normalize negative budget to 0 (no limit) before priority resolution
	if opts.ContextBudget < 0 {
		opts.ContextBudget = 0
	}

	// Save original CLI model before agent manifest may override it (P1: model_source tracking)
	cliModel := opts.Model

	// Load Agent information if specified
	if agent != nil {
		// Stem agent differentiation: auto-match skills based on intent (Story 20.3)
		if agent.Manifest.Name == "stem" && len(agent.Manifest.Skills) == 0 && k.stemMatcher != nil {
			diffStart := time.Now()
			k.emitLog(proc, 0, types.LogOutput, fmt.Sprintf("differentiating: matching skills for intent %q", intent), "")

			// Check differentiation memory first (Story 20.4)
			var matchedSkills []string
			var matchErr error
			var fromMemory bool
			if k.diffMemory != nil {
				if remembered, ok := k.diffMemory.Lookup(intent); ok {
					matchedSkills = remembered
					fromMemory = true
					k.emitLog(proc, 0, types.LogOutput, fmt.Sprintf(
						"differentiating: reusing remembered path for intent %q: %v", intent, remembered), "")
				}
			}
			// Fallback to keyword matching if no memory hit
			if !fromMemory {
				matchedSkills, matchErr = k.stemMatcher.Match(intent)
				if matchErr != nil {
					k.emitLog(proc, 0, types.LogOutput, fmt.Sprintf("differentiating: match error: %v", matchErr), "")
				}
			}

			if matchErr == nil && len(matchedSkills) > 0 && k.skillLoader != nil {
				var loadedNames []string
				for _, skillName := range matchedSkills {
					skillInfo, loadErr := k.skillLoader(skillName)
					if loadErr == nil {
						agent.Skills = append(agent.Skills, skillInfo)
						loadedNames = append(loadedNames, skillName)
					}
				}
				if len(loadedNames) > 0 {
					k.emitLog(proc, 0, types.LogOutput, fmt.Sprintf("differentiating: loading skills %v", loadedNames), "")
					// Update proc.Skills so ps/ProcInfo reflects differentiated skills
					proc.Skills = loadedNames

					// Record initial differentiation lineage (Story 20.5)
					if proc.lineage == nil {
						proc.lineage = NewLineage()
					}
					proc.lineage.Record(LineageEvent{
						Timestamp:  time.Now(),
						Phase:      "initial",
						Skills:     loadedNames,
						Trigger:    intent,
						FromMemory: fromMemory,
					})
				}

				// Record differentiation path to memory (Story 20.4)
				if k.diffMemory != nil && len(loadedNames) > 0 {
					k.diffMemory.Record(intent, loadedNames)
				}
			}
			diffDuration := time.Since(diffStart)
			eventArgs := map[string]any{
				"matched_skills": matchedSkills,
				"duration_ms":    diffDuration.Milliseconds(),
				"from_memory":    fromMemory,
			}
			var eventErr error
			if matchErr != nil {
				eventErr = matchErr
			}
			k.emitEvent(proc, "StemDifferentiate", eventArgs, nil, eventErr, diffDuration)
		}

		// System prompt = Agent instructions + Skill bodies
		agentPrompt := agent.SystemPrompt()
		if opts.SystemPrompt == "" {
			opts.SystemPrompt = agentPrompt
		} else {
			opts.SystemPrompt = opts.SystemPrompt + "\n\n" + agentPrompt
		}

		// Aggregate AllowedTools from all Skills
		proc.AllowedDevices = agent.AllowedTools()

		// Planning configuration: nil (default) = true, explicit false = disabled
		if agent.Manifest.Planning != nil && !*agent.Manifest.Planning {
			proc.PlanningEnabled = false
		}

		// Model selection priority: CLI --model > Agent manifest > driver default
		if opts.Model == "" && agent.Manifest.Models.Preferred != "" {
			opts.Model = agent.Manifest.Models.Preferred
		}

		// Budget priority: opts (CLI/Compose) > agent manifest > 0 (no limit)
		if opts.ContextBudget == 0 && agent.Manifest.ContextBudget > 0 {
			opts.ContextBudget = agent.Manifest.ContextBudget
		}

		// Fallback configuration (Story 23.5)
		if agent.Manifest.Models.Fallback != "" {
			proc.FallbackModel = agent.Manifest.Models.Fallback
			fbProvider := agent.Manifest.Models.FallbackProvider
			if fbProvider == "" {
				// Same-provider fallback: resolve using main provider
				p := opts.Provider
				if p == "" {
					p = agent.Manifest.Models.Provider
				}
				if p == "" {
					p = "claude"
				}
				fbProvider = p
			}
			proc.FallbackProvider = fbProvider
			fbDevice, _, fbErr := k.resolveLLMDevice(nil, fbProvider)
			if fbErr == nil {
				proc.FallbackDevice = fbDevice
			}
			// fallback resolution failure is non-blocking; means fallback unavailable
		}
	}

	proc.ContextBudget = opts.ContextBudget

	// MaxSteps priority: CLI --max-steps > agent manifest max_steps > DefaultMaxSteps
	maxStepsForProc := DefaultMaxSteps
	if agent != nil && agent.Manifest.MaxSteps > 0 {
		maxStepsForProc = agent.Manifest.MaxSteps
	}
	if opts.MaxTurns > 0 {
		maxStepsForProc = opts.MaxTurns
	}
	proc.MaxSteps = maxStepsForProc

	if opts.TraceID != "" {
		proc.TraceID = opts.TraceID
		proc.ParentSpanID = opts.ParentSpanID
		proc.SpanID = debug.GenerateSpanID()
		if k.spanRecorder != nil {
			k.spanRecorder.StartSpan(proc.PID, proc.TraceID, proc.SpanID, proc.ParentSpanID, intent)
		}
	}

	var cid types.CtxID
	var llmFD types.FD

	if opts.PreallocatedCtxID != 0 {
		// Use pre-allocated context (fork-continue path)
		cid = opts.PreallocatedCtxID
		proc.CtxID = cid
	} else {
		// Allocate context
		ctxAllocStart := time.Now()
		var err error
		cid, err = k.ctxMgr.CtxAlloc(DefaultCtxSize)
		k.emitEvent(proc, "CtxAlloc", map[string]any{
			"size": DefaultCtxSize,
		}, cid, err, time.Since(ctxAllocStart))
		if err != nil {
			return 0, NewSyscallError("Spawn", proc.PID, "", err, types.ErrInternal)
		}
		proc.CtxID = cid

		// Set system prompt if provided
		if opts.SystemPrompt != "" {
			setPromptStart := time.Now()
			if err := k.ctxMgr.SetSystemPrompt(cid, opts.SystemPrompt); err != nil {
				k.emitEvent(proc, "CtxWrite", map[string]any{
					"cid": cid,
					"op":  "SetSystemPrompt",
				}, nil, err, time.Since(setPromptStart))
				_ = k.ctxMgr.CtxFree(cid)
				return 0, NewSyscallError("Spawn", proc.PID, "", err, types.ErrInternal)
			}
			k.emitEvent(proc, "CtxWrite", map[string]any{
				"cid": cid,
				"op":  "SetSystemPrompt",
			}, nil, nil, time.Since(setPromptStart))
		}

		// Append initial intent as user message
		appendStart := time.Now()
		if err := k.ctxMgr.AppendMessage(cid, rnixctx.RoleUser, intent); err != nil {
			k.emitEvent(proc, "CtxWrite", map[string]any{
				"cid":  cid,
				"op":   "AppendMessage",
				"role": string(rnixctx.RoleUser),
			}, nil, err, time.Since(appendStart))
			_ = k.ctxMgr.CtxFree(cid)
			return 0, NewSyscallError("Spawn", proc.PID, "", err, types.ErrInternal)
		}
		k.emitEvent(proc, "CtxWrite", map[string]any{
			"cid":  cid,
			"op":   "AppendMessage",
			"role": string(rnixctx.RoleUser),
		}, nil, nil, time.Since(appendStart))
	}

	if !opts.SkipReasonLoop {
		resolveStart := time.Now()

		// Use project-level default provider if available and no explicit override
		providerOverride := opts.Provider
		providerOverrideFromProject := false
		if providerOverride == "" && agent != nil && agent.Manifest.Models.Provider == "" &&
			opts.ProjectConfig != nil && opts.ProjectConfig.DefaultProvider != "" {
			providerOverride = opts.ProjectConfig.DefaultProvider
			providerOverrideFromProject = true
		}

		// Resolve LLM device path based on agent provider
		// When project config provides an LLMFileOpener, skip global provider validation
		// since the provider may only exist at project level
		var llmDevice string
		var providerSource string
		var resolveErr error
		if opts.ProjectConfig != nil && opts.ProjectConfig.LLMFileOpener != nil {
			// Build device path without global validation
			// Use opts.Provider (actual CLI value), not providerOverride which may
			// have been pre-resolved from ProjectConfig.DefaultProvider
			provider := opts.Provider
			providerSource = "cli"
			if provider == "" {
				if agent != nil && agent.Manifest.Models.Provider != "" {
					provider = agent.Manifest.Models.Provider
					providerSource = "agent"
				}
			}
			if provider == "" {
				if opts.ProjectConfig.DefaultProvider != "" {
					provider = opts.ProjectConfig.DefaultProvider
					providerSource = "project"
				} else if k.defaultProvider != "" {
					provider = k.defaultProvider
					providerSource = "project"
				} else {
					provider = "claude"
					providerSource = "default"
				}
			}
			llmDevice = "/dev/llm/" + provider
		} else {
			llmDevice, providerSource, resolveErr = k.resolveLLMDevice(agent, providerOverride)
			if providerOverrideFromProject && providerSource == "cli" {
				providerSource = "project"
			}
		}
		if resolveErr != nil {
			if opts.PreallocatedCtxID == 0 {
				_ = k.ctxMgr.CtxFree(cid)
			}
			return 0, NewSyscallError("Spawn", proc.PID, "", resolveErr, types.ErrDriver)
		}
		proc.PrimaryDevice = llmDevice // Store for fallback reference (Story 23.5)
		proc.Provider = strings.TrimPrefix(llmDevice, "/dev/llm/")
		proc.Model = opts.Model

		// Determine model source: CLI --model > agent manifest preferred > driver default
		modelSource := "driver"
		if opts.Model != "" {
			if cliModel != "" {
				modelSource = "cli"
			} else {
				modelSource = "agent"
			}
		}

		// Emit ConfigResolve strace event (Story 3.5)
		configArgs := map[string]any{
			"provider":        proc.Provider,
			"provider_source": providerSource,
			"model":           opts.Model,
			"model_source":    modelSource,
		}
		// When agent overrides project default, show the override relationship
		projectDefault := k.defaultProvider
		if opts.ProjectConfig != nil && opts.ProjectConfig.DefaultProvider != "" {
			projectDefault = opts.ProjectConfig.DefaultProvider
		}
		if projectDefault != "" && projectDefault != proc.Provider {
			configArgs["project_default"] = projectDefault
		}
		k.emitEvent(proc, "ConfigResolve", configArgs, nil, nil, time.Since(resolveStart))

		// Open LLM device via VFS (or project-level override)
		openStart := time.Now()
		var err error
		if proc.ProjectConfig != nil && proc.ProjectConfig.LLMFileOpener != nil {
			// Try project-level LLM device first
			providerName := strings.TrimPrefix(llmDevice, "/dev/llm/")
			file, openErr := proc.ProjectConfig.LLMFileOpener(providerName, int(vfs.O_RDWR))
			if openErr == nil {
				if vfsFile, ok := file.(vfs.VFSFile); ok {
					llmFD = k.vfs.RegisterFD(proc.PID, vfsFile)
				} else {
					log.Printf("[kernel] warning: LLMFileOpener returned non-VFSFile type %T for provider %q, falling back to global VFS", file, providerName)
					llmFD, err = k.vfs.Open(proc.PID, llmDevice, vfs.O_RDWR)
				}
			} else {
				// Project opener failed — fallback to global VFS
				llmFD, err = k.vfs.Open(proc.PID, llmDevice, vfs.O_RDWR)
			}
		} else {
			llmFD, err = k.vfs.Open(proc.PID, llmDevice, vfs.O_RDWR)
		}
		k.emitEvent(proc, "Open", map[string]any{
			"path":  llmDevice,
			"flags": vfs.O_RDWR,
		}, llmFD, err, time.Since(openStart))
		if err != nil {
			if opts.PreallocatedCtxID == 0 {
				_ = k.ctxMgr.CtxFree(cid)
			}
			return 0, NewSyscallError("Spawn", proc.PID, llmDevice, err, types.ErrDriver)
		}
		proc.FDTable[llmFD] = nil // VFS manages actual file internally; tracks FD existence for process inspection

		// Set up stream handler for driver events (tool_call, thinking, system, etc.)
		if file := k.vfs.GetFile(proc.PID, llmFD); file != nil {
			if obs, ok := file.(vfs.StreamObserver); ok {
				driverStep := 0 // sub-step counter for driver events within a single reasoning step
				obs.SetStreamHandler(func(evt map[string]any) {
					evtType, _ := evt["type"].(string)
					syscallName := driverEventToSyscall(evtType)
					k.emitEvent(proc, syscallName, evt, nil, nil, 0)

					// Also emit to LogChan for rnix log visibility
					if cat, content, toolPath, ok := driverEventToLog(evt); ok {
						k.emitLog(proc, 0, cat, content, toolPath)
					}

					// Write StepRecord for tool_call events so Dashboard Step Timeline shows them
					if evtType == "tool_call" {
						contentVal, _ := evt["content"].(string)
						if contentVal == "started" {
							driverStep++
							tool, _ := evt["tool"].(string)
							desc, _ := evt["description"].(string)
							path, _ := evt["path"].(string)
							summary := desc
							if summary == "" {
								summary = tool
							}
							toolPath := tool
							if path != "" {
								toolPath = tool + ":" + path
							}
							k.writeDriverStepRecord(proc, driverStep, "tool_call", summary, toolPath)
						}
					}
				})
			}
			// Detect native tool calling capability
			if tc, ok := file.(vfs.ToolCapable); ok && tc.SupportsToolCalling() {
				proc.UseNativeTools = true
				vfsDefs, vfsMap := buildToolDefs(k.vfs.DeviceRegistry(), proc.AllowedDevices, proc.PlanningEnabled)
				metaDefs, metaMap := metaToolDefs(proc.PlanningEnabled)
				allDefs := make([]vfs.ToolDef, 0, len(vfsDefs)+len(metaDefs))
				allDefs = append(allDefs, vfsDefs...)
				allDefs = append(allDefs, metaDefs...)
				proc.nativeToolDefs = allDefs
				proc.toolMap = make(map[string]toolMapping, len(vfsMap)+len(metaMap))
				maps.Copy(proc.toolMap, vfsMap)
				maps.Copy(proc.toolMap, metaMap)
			}
			// Extract MCP device paths for mixed mode text injection
			for _, dev := range proc.AllowedDevices {
				if strings.HasPrefix(dev, "/dev/mcp/") || strings.HasPrefix(dev, "/mnt/mcp/") {
					proc.mcpDevicePaths = append(proc.mcpDevicePaths, dev)
				}
			}
		}

		// Auto-mount MCP servers referenced in agent.yaml (Story 9.2)
		if agent != nil && len(agent.MCPConfigs) > 0 {
			if k.mountMgr == nil {
				_ = k.vfs.CloseAll(proc.PID)
				if opts.PreallocatedCtxID == 0 {
					_ = k.ctxMgr.CtxFree(cid)
				}
				return 0, NewSyscallError("Spawn", proc.PID, "",
					fmt.Errorf("mount manager not initialized but agent requires MCP servers"), types.ErrInternal)
			}
			var mountedPaths []string
			for _, mcpCfg := range agent.MCPConfigs {
				mountPath := fmt.Sprintf("/mnt/mcp/%d-%s", proc.PID, mcpCfg.ServerName)
				// mcpCfg is a value copy from the slice — safe to mutate without affecting agent.MCPConfigs
				if opts.ProjectConfig != nil && opts.ProjectConfig.ProjectDir != "" {
					mcpCfg.WorkDir = opts.ProjectConfig.ProjectDir
				}
				mountStart := time.Now()
				if err := k.mountMgr.Mount(mountPath, mcpCfg); err != nil {
					k.emitEvent(proc, "Mount", map[string]any{
						"path": mountPath,
						"auto": true,
					}, nil, err, time.Since(mountStart))
					for _, p := range mountedPaths {
						_ = k.mountMgr.Unmount(p)
					}
					_ = k.vfs.CloseAll(proc.PID)
					if opts.PreallocatedCtxID == 0 {
						_ = k.ctxMgr.CtxFree(cid)
					}
					return 0, NewSyscallError("Spawn", proc.PID, mountPath,
						fmt.Errorf("auto-mount mcp %q failed: %w", mcpCfg.ServerName, err), types.ErrDriver)
				}
				k.emitEvent(proc, "Mount", map[string]any{
					"path": mountPath,
					"auto": true,
				}, nil, nil, time.Since(mountStart))
				mountedPaths = append(mountedPaths, mountPath)
			}
			proc.mu.Lock()
			proc.MCPMounts = mountedPaths
			proc.AllowedDevices = append(proc.AllowedDevices, mountedPaths...)
			proc.mu.Unlock()
		}
	}

	// Set up goroutine context for cancellation
	ctx, cancel := gocontext.WithCancel(gocontext.Background())
	proc.cancel = cancel
	proc.ctx = ctx

	// Register process in table
	k.AddProcess(proc)

	// Initialize IPC message queue for the new process (Story 6.1)
	k.msgQueues.Store(proc.PID, newMessageQueue())

	// Emit Spawn syscall event
	spawnArgs := map[string]any{
		"intent": intent,
	}
	if agent != nil {
		spawnArgs["agent"] = agent.Manifest.Name
		spawnArgs["skills"] = skillNames
		spawnArgs["allowed_devices"] = proc.AllowedDevices
		if len(proc.MCPMounts) > 0 {
			spawnArgs["mcp_mounts"] = proc.MCPMounts
		}
	}
	if opts.SkipReasonLoop {
		spawnArgs["skip_reason_loop"] = true
	}
	k.emitEvent(proc, "Spawn", spawnArgs, proc.PID, nil, time.Since(start))

	if opts.SkipReasonLoop {
		// No reasoning goroutine — process starts in Running state immediately
		_ = proc.Start() // Created → Running
	} else {
		// Launch reasoning goroutine
		// Note: CtxFree deferred to Wait/Reap (Story 4.1) per resource release order
		proc.wg.Go(func() {
			defer func() { _ = k.vfs.CloseAll(proc.PID) }()
			_ = proc.Start() // Created → Running
			k.reasonStep(proc, llmFD, opts)
		})
	}

	// Notify callback after process is registered and goroutine launched
	if k.callbacks != nil {
		k.callbacks.OnSpawn(proc.PID, intent, proc.Provider, proc.Model, proc.UUID)
	}

	// Notify immune daemon about new process (Story 22.1)
	if k.immuneDaemon != nil {
		agentName := ""
		if agent != nil {
			agentName = agent.Manifest.Name
		}
		k.immuneDaemon.OnProcessStart(proc.PID, agentName)
	}

	return proc.PID, nil
}

// emitEvent sends a SyscallEvent to the process DebugChan (non-blocking).
// This is a convenience wrapper that auto-fills process info (PID, Timestamp)
// and delegates to debug.EmitEvent for the actual non-blocking write.
// Holds proc.mu only during channel access to prevent races with Wait's close(DebugChan).
func (k *KernelImpl) emitEvent(proc *Process, syscall string, args map[string]any, result any, err error, duration time.Duration) {
	// Check gdb syscall breakpoint at entry (Story 13.2)
	// Skip checking for internal gdb events to avoid infinite recursion.
	if syscall != "GdbPause" {
		if hit := proc.CheckBreakpoint(BreakpointContext{
			BPType:      BPSyscall,
			SyscallName: syscall,
			SyscallArgs: args,
		}); hit != nil {
			proc.GdbPause(fmt.Sprintf("syscall breakpoint hit: %s", syscall), hit)
		}
	}

	// Check gdb step syscall mode (Story 13.3)
	// Skip internal events: GdbPause (would trigger recursion) and ReasonStep (control event).
	if syscall != "GdbPause" && syscall != "ReasonStep" {
		if proc.GetStepMode() == StepSyscall {
			proc.ClearStepMode()
			proc.GdbPause("step_syscall", nil, map[string]any{
				"syscall_name": syscall,
				"syscall_args": args,
			})
		}
	}

	event := debug.NewEvent(proc.PID, proc.CreatedAt, syscall, args)
	debug.CompleteEvent(&event, result, err, duration)
	proc.mu.Lock()
	event.TraceID = proc.TraceID
	event.SpanID = proc.SpanID
	hasTrace := proc.TraceID != ""
	ch := proc.DebugChan
	if ch != nil {
		debug.EmitEvent(ch, event)
	}
	proc.mu.Unlock()

	// Recording hook (Story 14.1): write event to disk if recording is active
	if k.recordMgr != nil && k.recordMgr.IsRecording(proc.PID) {
		recEvent := debug.RecordEventFromSyscall(event)
		if err := k.recordMgr.RecordEvent(proc.PID, recEvent); err != nil {
			log.Printf("[record] write error pid=%d: %v", proc.PID, err)
		}
	}

	if hasTrace && k.spanRecorder != nil {
		k.spanRecorder.RecordSyscall(proc.PID)
	}

	// Immune daemon behavior monitoring (Story 22.1)
	if k.immuneDaemon != nil {
		k.immuneDaemon.OnSyscallEvent(proc.PID, event)
	}
}

// finishProcess terminates the process and writes the exit status to the Done channel.
func (k *KernelImpl) finishProcess(proc *Process, exit ExitStatus) {
	// Auto-Unmount MCP servers before terminating (Story 9.2)
	proc.mu.Lock()
	mcpMounts := append([]string(nil), proc.MCPMounts...)
	proc.mu.Unlock()
	if k.mountMgr != nil {
		for _, mountPath := range mcpMounts {
			unmountStart := time.Now()
			err := k.mountMgr.Unmount(mountPath)
			k.emitEvent(proc, "Unmount", map[string]any{
				"path": mountPath,
				"auto": true,
			}, nil, err, time.Since(unmountStart))
			// Unmount failure does not block process exit
		}
	}

	_ = proc.Terminate(exit)

	// Recording hook: state change (Story 14.1)
	if k.recordMgr != nil && k.recordMgr.IsRecording(proc.PID) {
		stateEvent := debug.RecordEvent{
			Timestamp: time.Since(proc.CreatedAt),
			PID:       proc.PID,
			Type:      debug.RecordStateChange,
			State: &debug.StateChangeData{
				FromState: types.StateRunning.String(),
				ToState:   types.StateZombie.String(),
				Reason:    exit.Reason,
			},
		}
		if err := k.recordMgr.RecordEvent(proc.PID, stateEvent); err != nil {
			log.Printf("[record] state change error pid=%d: %v", proc.PID, err)
		}
	}

	// Notify callbacks
	if k.callbacks != nil {
		if exit.Err != nil {
			k.callbacks.OnError(proc.PID, exit.Err)
		}
		k.callbacks.OnComplete(proc.PID, proc.Result, exit)
	}

	// Notify immune daemon about process exit (Story 22.1)
	if k.immuneDaemon != nil {
		k.immuneDaemon.OnProcessExit(proc.PID, proc.TokensUsed, exit.Code == 0)
	}

	select {
	case proc.Done <- exit:
	default:
	}

	// Story 4.2: Orphan detection
	// If parent process no longer exists in table, push to reapCh for auto-reap.
	// PPID=0 processes (top-level spawn) are NOT auto-reaped here —
	// in daemon mode, IPC Server calls kernel.Reap(pid) after spawn stream ends.
	// Read PPID under lock to prevent race with handleOrphanChildren's reparent write.
	proc.mu.Lock()
	ppid := proc.PPID
	proc.mu.Unlock()
	if ppid > 0 {
		if _, parentExists := k.GetProcess(ppid); !parentExists {
			select {
			case k.reapCh <- proc.PID:
			default:
				go k.reapProcess(proc)
			}
		}
	}
}

// attemptFallback tries the fallback provider when primary LLM call fails.
// Returns the response data and nil error on success, or nil and error if fallback also fails.
// Emits strace events for the fallback attempt. (Story 23.5)
func (k *KernelImpl) attemptFallback(proc *Process, req llmRequest, primaryDevice string, primaryErr error, step int) ([]byte, error) {
	if proc.FallbackDevice == "" {
		return nil, primaryErr // no fallback configured
	}

	fallbackStart := time.Now()

	// Open fallback device — try project-level driver first, then global VFS
	var fbFD types.FD
	var err error
	if proc.ProjectConfig != nil && proc.ProjectConfig.LLMFileOpener != nil {
		fbProvider := strings.TrimPrefix(proc.FallbackDevice, "/dev/llm/")
		fileAny, openErr := proc.ProjectConfig.LLMFileOpener(fbProvider, int(vfs.O_RDWR))
		if openErr == nil {
			if vfsFile, ok := fileAny.(vfs.VFSFile); ok {
				fbFD = k.vfs.RegisterFD(proc.PID, vfsFile)
			} else {
				log.Printf("[kernel] warning: LLMFileOpener returned non-VFSFile type %T for fallback provider %q", fileAny, fbProvider)
				fbFD, err = k.vfs.Open(proc.PID, proc.FallbackDevice, vfs.O_RDWR)
			}
		} else {
			// Project opener doesn't have this provider — fallback to global VFS
			fbFD, err = k.vfs.Open(proc.PID, proc.FallbackDevice, vfs.O_RDWR)
		}
	} else {
		fbFD, err = k.vfs.Open(proc.PID, proc.FallbackDevice, vfs.O_RDWR)
	}
	if err != nil {
		return nil, primaryErr // can't open fallback, return original error
	}
	defer func() { _ = k.vfs.Close(proc.PID, fbFD) }()

	// Modify request to use fallback model
	req.Model = proc.FallbackModel
	reqJSON, err := json.Marshal(req)
	if err != nil {
		return nil, primaryErr
	}

	// Write to fallback device
	if err := k.vfs.Write(proc.ctx, proc.PID, fbFD, reqJSON); err != nil {
		// Fallback also failed — emit exhausted event
		k.emitEvent(proc, "ReasonStep", map[string]any{
			"step":            step,
			"action":          "fallback_exhausted",
			"primary_device":  primaryDevice,
			"primary_error":   primaryErr.Error(),
			"fallback_device": proc.FallbackDevice,
			"fallback_error":  err.Error(),
		}, nil, err, time.Since(fallbackStart))
		return nil, fmt.Errorf("primary %s: %v; fallback %s: %v", primaryDevice, primaryErr, proc.FallbackDevice, err)
	}

	// Read response from fallback device
	respData, err := k.vfs.Read(proc.PID, fbFD, 1<<20)
	if err != nil {
		k.emitEvent(proc, "ReasonStep", map[string]any{
			"step":            step,
			"action":          "fallback_exhausted",
			"primary_device":  primaryDevice,
			"primary_error":   primaryErr.Error(),
			"fallback_device": proc.FallbackDevice,
			"fallback_error":  err.Error(),
		}, nil, err, time.Since(fallbackStart))
		return nil, fmt.Errorf("primary %s: %v; fallback %s: %v", primaryDevice, primaryErr, proc.FallbackDevice, err)
	}

	// Fallback succeeded — emit success event
	k.emitEvent(proc, "ReasonStep", map[string]any{
		"step":            step,
		"action":          "fallback",
		"primary_device":  primaryDevice,
		"primary_error":   primaryErr.Error(),
		"fallback_device": proc.FallbackDevice,
		"fallback_model":  proc.FallbackModel,
	}, nil, nil, time.Since(fallbackStart))

	return respData, nil
}

// reasonStep executes the reasoning loop for a process.
func (k *KernelImpl) reasonStep(proc *Process, llmFD types.FD, opts SpawnOpts) {
	maxSteps := proc.MaxSteps
	if maxSteps <= 0 {
		maxSteps = DefaultMaxSteps
	}

	// Initialize StepWriter for observation system (Story 27.1)
	stepBaseDir := k.stepDataDir
	if stepBaseDir == "" {
		if proc.ProjectConfig != nil && proc.ProjectConfig.ProjectDir != "" {
			stepBaseDir = filepath.Join(proc.ProjectConfig.ProjectDir, ".rnix")
		}
	}
	if stepBaseDir != "" {
		sw, err := NewStepWriter(stepBaseDir, proc.UUID)
		if err == nil {
			proc.mu.Lock()
			proc.stepWriter = sw
			proc.mu.Unlock()
		}
		// Non-fatal: step recording is best-effort
	}

	defer func() {
		// Ensure process always transitions to Zombie
		if proc.GetState() == types.StateRunning {
			k.finishProcess(proc, ExitStatus{Code: 1, Reason: "unexpected exit"})
		}
	}()

	var lastResultSummary string
	var consecutiveToolErrors int

	for step := 1; step <= maxSteps; step++ {
		stepStart := time.Now()

		// Notify step progress callback
		if k.callbacks != nil {
			k.callbacks.OnStep(proc.PID, step, maxSteps)
		}

		// Check pause state (Story 6.4: Signal System)
		if ch := proc.WaitIfPaused(); ch != nil {
			k.emitEvent(proc, "ReasonStep", map[string]any{
				"step":   step,
				"action": "paused",
			}, nil, nil, 0)

			select {
			case <-ch:
				// Resumed, continue execution
				k.emitEvent(proc, "ReasonStep", map[string]any{
					"step":   step,
					"action": "resumed",
				}, nil, nil, time.Since(stepStart))
			case <-proc.ctx.Done():
				// Cancelled while paused (Kill can terminate a paused process)
				k.emitEvent(proc, "ReasonStep", map[string]any{
					"step":   step,
					"action": "cancelled_while_paused",
				}, nil, proc.ctx.Err(), time.Since(stepStart))
				k.finishProcess(proc, ExitStatus{Code: 1, Reason: "context cancelled while paused"})
				return
			}
		}

		// Check context cancellation
		select {
		case <-proc.ctx.Done():
			k.emitEvent(proc, "ReasonStep", map[string]any{
				"step":   step,
				"action": "cancelled",
			}, nil, proc.ctx.Err(), time.Since(stepStart))
			k.finishProcess(proc, ExitStatus{Code: 1, Reason: "context cancelled"})
			return
		default:
		}

		// Check gdb reasoning breakpoint (Story 13.2)
		if hit := proc.CheckBreakpoint(BreakpointContext{
			BPType:     BPReasoning,
			StepNumber: step,
		}); hit != nil {
			proc.GdbPause(fmt.Sprintf("reasoning breakpoint hit at step %d", step), hit)
			// After resume, re-check cancellation
			select {
			case <-proc.ctx.Done():
				k.finishProcess(proc, ExitStatus{Code: 1, Reason: "context cancelled while gdb-paused"})
				return
			default:
			}
		}

		// Check gdb step reasoning mode (Story 13.3)
		if proc.GetStepMode() == StepReasoning {
			proc.ClearStepMode()
			proc.GdbPause("step_reasoning", nil, map[string]any{
				"step_number":         step,
				"last_result_summary": lastResultSummary,
			})
			// After resume, re-check cancellation
			select {
			case <-proc.ctx.Done():
				k.finishProcess(proc, ExitStatus{Code: 1, Reason: "context cancelled while gdb-paused"})
				return
			default:
			}
		}

		// Build prompt from context
		buildPromptStart := time.Now()
		promptResult, err := k.ctxMgr.BuildPrompt(proc.CtxID)
		k.emitEvent(proc, "CtxRead", map[string]any{
			"cid": proc.CtxID,
			"op":  "BuildPrompt",
		}, nil, err, time.Since(buildPromptStart))
		if err != nil {
			k.emitEvent(proc, "ReasonStep", map[string]any{
				"step":   step,
				"action": "error",
			}, nil, err, time.Since(stepStart))
			k.finishProcess(proc, ExitStatus{Code: 1, Reason: "build prompt failed", Err: err})
			return
		}

		// Construct LLM request with full conversation history
		// Apply gdb model override if set (Story 13.4)
		model := opts.Model
		if override := proc.GetGdbModelOverride(); override != "" {
			model = override
		}
		// Apply gdb environment variables injection (tech debt fix)
		sysPrompt := promptResult.SystemPrompt
		if envVars := proc.GetGdbEnvVars(); len(envVars) > 0 {
			var envSection strings.Builder
			envSection.WriteString("\n\n[GDB Environment Variables]\n")
			for k, v := range envVars {
				fmt.Fprintf(&envSection, "%s=%s\n", k, v)
			}
			sysPrompt += envSection.String()
		}
		// Inject action protocol so the LLM knows how to produce structured
		// JSON actions via VFS device paths and other action types.
		if proc.UseNativeTools {
			// Native tool calling: inject MCP text descriptions if needed (mixed mode)
			if len(proc.mcpDevicePaths) > 0 {
				sysPrompt += mcpToolProtocolSnippet(proc.mcpDevicePaths)
			}
		} else {
			// Text protocol: auto-generate from ToolDefs (or use cached per-process protocol)
			if proc.generatedProtocol == "" {
				vfsDefs, vfsMap := buildToolDefs(k.vfs.DeviceRegistry(), proc.AllowedDevices, proc.PlanningEnabled)
				metaDefs, metaMap := metaToolDefs(proc.PlanningEnabled)
				proc.generatedProtocol = generateToolProtocol(vfsDefs, vfsMap, metaDefs, metaMap, proc.PlanningEnabled)
			}
			sysPrompt += proc.generatedProtocol
		}
		// Inject loaded skills list so the LLM knows which skills are available
		// and understands they are instructions, not callable tools.
		proc.mu.Lock()
		loadedSkills := make([]string, len(proc.Skills))
		copy(loadedSkills, proc.Skills)
		proc.mu.Unlock()
		if len(loadedSkills) > 0 {
			sysPrompt += "\n\n[Loaded Skills]\nThe following skills are loaded: " +
				strings.Join(loadedSkills, ", ") +
				".\nTheir instructions are already in your system prompt. Follow them using available VFS devices." +
				"\nDo NOT try to call these skills via /dev/mcp/ or any device path."
		}
		// Capture FinalSystemPrompt on first reasonStep (Story 27.1 AC-5)
		proc.mu.Lock()
		if proc.FinalSystemPrompt == "" {
			proc.FinalSystemPrompt = sysPrompt
		}
		proc.mu.Unlock()

		req := llmRequest{
			Intent:       proc.Intent,
			SystemPrompt: sysPrompt,
			Model:        model,
			MaxTurns:     0,
			TimeoutMs:    opts.TimeoutMs,
			Messages:     promptResult.Messages,
		}
		if proc.UseNativeTools {
			req.Tools = proc.nativeToolDefs
		}
		reqJSON, err := json.Marshal(req)
		if err != nil {
			k.emitEvent(proc, "ReasonStep", map[string]any{
				"step":   step,
				"action": "error",
			}, nil, err, time.Since(stepStart))
			k.finishProcess(proc, ExitStatus{Code: 1, Reason: "marshal request failed", Err: err})
			return
		}

		// Write request to LLM device
		writeStart := time.Now()
		var respData []byte
		if err := k.vfs.Write(proc.ctx, proc.PID, llmFD, reqJSON); err != nil {
			k.emitEvent(proc, "Write", map[string]any{
				"fd":    llmFD,
				"size":  len(reqJSON),
				"model": model,
			}, nil, err, time.Since(writeStart))

			// Attempt fallback (Story 23.5)
			fbData, fbErr := k.attemptFallback(proc, req, proc.PrimaryDevice, err, step)
			if fbErr != nil {
				reason := "llm write failed"
				if proc.FallbackDevice != "" {
					reason = "all providers exhausted"
				}
				k.emitEvent(proc, "ReasonStep", map[string]any{
					"step":   step,
					"action": "error",
				}, nil, fbErr, time.Since(stepStart))
				k.finishProcess(proc, ExitStatus{Code: 1, Reason: reason, Err: fbErr})
				return
			}
			// Fallback succeeded
			respData = fbData
		} else {
			k.emitEvent(proc, "Write", map[string]any{
				"fd":    llmFD,
				"size":  len(reqJSON),
				"model": model,
			}, nil, nil, time.Since(writeStart))

			// Read response from LLM device
			readStart := time.Now()
			var readErr error
			respData, readErr = k.vfs.Read(proc.PID, llmFD, 1<<20) // 1MB max
			k.emitEvent(proc, "Read", map[string]any{
				"fd":     llmFD,
				"length": 1 << 20,
			}, len(respData), readErr, time.Since(readStart))
			if readErr != nil {
				k.emitEvent(proc, "ReasonStep", map[string]any{
					"step":   step,
					"action": "error",
				}, nil, readErr, time.Since(stepStart))
				k.finishProcess(proc, ExitStatus{Code: 1, Reason: "llm read failed", Err: readErr})
				return
			}
		}

		// Parse LLM response
		var resp llmResponse
		rawResponseStr := string(respData)
		if err := json.Unmarshal(respData, &resp); err != nil {
			k.emitEvent(proc, "ReasonStep", map[string]any{
				"step":   step,
				"action": "error",
			}, nil, err, time.Since(stepStart))
			k.finishProcess(proc, ExitStatus{Code: 1, Reason: "unmarshal response failed", Err: err})
			return
		}

		// Update last result summary for step reasoning display
		lastResultSummary = resp.Content
		if len(lastResultSummary) > 80 {
			lastResultSummary = lastResultSummary[:80] + "..."
		}

		// Recording hook: LLM response (Story 14.1)
		if k.recordMgr != nil && k.recordMgr.IsRecording(proc.PID) {
			llmEvent := debug.RecordEvent{
				Timestamp: time.Since(proc.CreatedAt),
				PID:       proc.PID,
				Type:      debug.RecordLLMResponse,
				LLM: &debug.LLMResponseData{
					Model:          model,
					ResponseTokens: resp.TokensUsed,
				},
			}
			if err := k.recordMgr.RecordEvent(proc.PID, llmEvent); err != nil {
				log.Printf("[record] llm event error pid=%d: %v", proc.PID, err)
			}
		}

		proc.mu.Lock()
		proc.TokensUsed += resp.TokensUsed
		budget := proc.ContextBudget
		tokens := proc.TokensUsed
		hasTrace := proc.TraceID != ""
		procGroups := make([]types.PGID, len(proc.groups))
		copy(procGroups, proc.groups)
		proc.mu.Unlock()

		proc.AppendTokenSnapshot(step, tokens)

		if hasTrace && k.spanRecorder != nil {
			k.spanRecorder.RecordTokens(proc.PID, resp.TokensUsed)
		}

		// Update BudgetPool consumption (Story 21.1)
		for _, pgid := range procGroups {
			if pool, ok := k.budgetPools.Load(pgid); ok {
				_ = pool.RecordUsage(proc.PID, resp.TokensUsed)
				break
			}
		}

		k.checkBudgetWarning(proc, step, tokens, budget)

		if budget > 0 && tokens >= budget {
			k.emitLog(proc, step, types.LogOutput,
				fmt.Sprintf("Token budget exceeded: %d/%d", tokens, budget), "")
			k.emitEvent(proc, "ReasonStep", map[string]any{
				"step":   step,
				"action": "budget_exceeded",
				"tokens": tokens,
				"budget": budget,
			}, nil, nil, time.Since(stepStart))
			k.finishProcess(proc, ExitStatus{
				Code:   2,
				Reason: "budget_exceeded",
				Err:    fmt.Errorf("token budget exceeded: %d/%d", tokens, budget),
			})
			return
		}

		// Check gdb budget breakpoint (Story 13.2)
		if hit := proc.CheckBreakpoint(BreakpointContext{
			BPType:     BPBudget,
			TokensUsed: tokens,
			StepNumber: step,
		}); hit != nil {
			proc.GdbPause(fmt.Sprintf("budget breakpoint hit: %d tokens used", tokens), hit)
			select {
			case <-proc.ctx.Done():
				k.finishProcess(proc, ExitStatus{Code: 1, Reason: "context cancelled while gdb-paused"})
				return
			default:
			}
		}

		// Native tool calls path: handle ToolCalls from LLM response directly
		if proc.UseNativeTools && len(resp.ToolCalls) > 0 {
			// Write StepRecord before context modification (Story 27.1 AC-6)
			toolNames := make([]string, len(resp.ToolCalls))
			for i, tc := range resp.ToolCalls {
				toolNames[i] = tc.Name
			}
			k.writeStepRecord(proc, step, promptResult, rawResponseStr, &resp,
				"native_tool_call", strings.Join(toolNames, ","), "", "", "", "", 0)
			shouldContinue := k.executeNativeToolCalls(proc, resp, step, stepStart, &consecutiveToolErrors)
			if !shouldContinue {
				return
			}
			continue
		}

		// Parse action (text protocol path, or native mode fallback when no ToolCalls)
		action := parseAction(&resp)

		// Check gdb quality breakpoint (Story 13.2)
		if hit := proc.CheckBreakpoint(BreakpointContext{
			BPType:      BPQuality,
			LLMResponse: resp.Content,
			StepNumber:  step,
		}); hit != nil {
			proc.GdbPause(fmt.Sprintf("quality breakpoint hit at step %d", step), hit)
			select {
			case <-proc.ctx.Done():
				k.finishProcess(proc, ExitStatus{Code: 1, Reason: "context cancelled while gdb-paused"})
				return
			default:
			}
		}

		// Emit [think] log entry with the LLM's full reasoning text
		k.emitLog(proc, step, types.LogThink, resp.Content, "")

		switch action.Type {
		case ActionText:
			// Emit [output] log entry with the final text
			k.emitLog(proc, step, types.LogOutput, action.Content, "")

			// Write StepRecord before modifying context (Story 27.1 AC-6)
			k.writeStepRecord(proc, step, promptResult, rawResponseStr, &resp,
				"text", briefTextSummary(action.Content), "", "", "", "", 0)

			proc.mu.Lock()
			proc.Result = action.Content
			hadError := proc.HasToolError
			proc.mu.Unlock()
			k.emitEvent(proc, "ReasonStep", map[string]any{
				"step":   step,
				"action": "text",
			}, action.Content, nil, time.Since(stepStart))
			stepDur := time.Since(stepStart)
			if k.callbacks != nil {
				k.callbacks.OnStepComplete(proc.PID, step, "text", briefTextSummary(action.Content), false, float64(stepDur.Microseconds())/1000.0)
			}
			exitCode := 0
			reason := "completed"
			if hadError {
				exitCode = 1
				reason = "completed_with_tool_errors"
			}
			k.finishProcess(proc, ExitStatus{Code: exitCode, Reason: reason})
			return

		case ActionToolCall:
			// Append LLM response as assistant message to maintain conversation history
			appendAssistantStart := time.Now()
			if err := k.ctxMgr.AppendMessage(proc.CtxID, rnixctx.RoleAssistant, resp.Content); err != nil {
				k.emitEvent(proc, "CtxWrite", map[string]any{
					"cid":  proc.CtxID,
					"op":   "AppendMessage",
					"role": string(rnixctx.RoleAssistant),
				}, nil, err, time.Since(appendAssistantStart))
				k.finishProcess(proc, ExitStatus{Code: 1, Reason: "append assistant message failed", Err: err})
				return
			}
			k.emitEvent(proc, "CtxWrite", map[string]any{
				"cid":  proc.CtxID,
				"op":   "AppendMessage",
				"role": string(rnixctx.RoleAssistant),
			}, nil, nil, time.Since(appendAssistantStart))

			// Recording hook: context snapshot after AppendMessage (Story 14.1)
			if k.recordMgr != nil && k.recordMgr.IsRecording(proc.PID) {
				k.recordContextSnapshot(proc)
			}

			// Device permission whitelist check (AC #2, #4)
			if len(proc.AllowedDevices) > 0 {
				cleanPath := path.Clean(action.ToolPath)
				allowed := false
				for _, dev := range proc.AllowedDevices {
					if cleanPath == dev || strings.HasPrefix(cleanPath, dev+"/") {
						allowed = true
						break
					}
				}
				if !allowed {
					permErr := fmt.Sprintf("permission denied: device %s not in allowed list %v", action.ToolPath, proc.AllowedDevices)
					appendPermStart := time.Now()
					if err := k.ctxMgr.AppendToolResult(proc.CtxID, action.ToolPath, permErr); err != nil {
						k.emitEvent(proc, "CtxWrite", map[string]any{
							"cid":  proc.CtxID,
							"op":   "AppendToolResult",
							"tool": action.ToolPath,
						}, nil, err, time.Since(appendPermStart))
						k.finishProcess(proc, ExitStatus{Code: 1, Reason: "append permission error failed", Err: err})
						return
					}
					k.emitEvent(proc, "CtxWrite", map[string]any{
						"cid":  proc.CtxID,
						"op":   "AppendToolResult",
						"tool": action.ToolPath,
					}, nil, nil, time.Since(appendPermStart))
					k.emitEvent(proc, "ReasonStep", map[string]any{
						"step":   step,
						"action": "permission_denied",
						"tool":   action.ToolPath,
					}, nil, errors.New(permErr), time.Since(stepStart))
					continue
				}
			}

			// Open tool device with auto-downgraded flags
			toolOpenStart := time.Now()
			toolDataStr := string(action.ToolData)
			isEmpty := len(action.ToolData) == 0 || toolDataStr == "{}" || toolDataStr == "null"
			openFlags := vfs.O_RDWR
			if isEmpty {
				openFlags = vfs.O_RDONLY
			}
			toolFD, err := k.vfs.Open(proc.PID, action.ToolPath, openFlags)
			k.emitEvent(proc, "Open", map[string]any{
				"path":  action.ToolPath,
				"flags": openFlags,
			}, toolFD, err, time.Since(toolOpenStart))
			if err != nil {
				errMsg := fmt.Sprintf("Tool error (%s): open failed: %v", action.ToolPath, err)
				if appendErr := k.ctxMgr.AppendToolResult(proc.CtxID, action.ToolPath, errMsg); appendErr != nil {
					k.finishProcess(proc, ExitStatus{Code: 1, Reason: "append tool error failed", Err: appendErr})
					return
				}
				proc.mu.Lock()
				proc.HasToolError = true
				proc.mu.Unlock()
				k.emitLog(proc, step, types.LogTool, errMsg, action.ToolPath)
				consecutiveToolErrors++
				if consecutiveToolErrors >= 3 {
					k.emitEvent(proc, "ReasonStep", map[string]any{
						"step":               step,
						"action":             "circuit_breaker",
						"consecutive_errors": consecutiveToolErrors,
					}, nil, nil, time.Since(stepStart))
					k.finishProcess(proc, ExitStatus{Code: 1, Reason: "circuit_breaker: 3 consecutive tool errors"})
					return
				}
				continue
			}

			// Write tool data (skip for read-only opens where data is empty)
			if !isEmpty {
				toolWriteStart := time.Now()
				if err := k.vfs.Write(proc.ctx, proc.PID, toolFD, action.ToolData); err != nil {
					k.emitEvent(proc, "Write", map[string]any{
						"fd":   toolFD,
						"size": len(action.ToolData),
					}, nil, err, time.Since(toolWriteStart))
					_ = k.vfs.Close(proc.PID, toolFD)
					errMsg := fmt.Sprintf("Tool error (%s): write failed: %v", action.ToolPath, err)
					if appendErr := k.ctxMgr.AppendToolResult(proc.CtxID, action.ToolPath, errMsg); appendErr != nil {
						k.finishProcess(proc, ExitStatus{Code: 1, Reason: "append tool error failed", Err: appendErr})
						return
					}
					proc.mu.Lock()
					proc.HasToolError = true
					proc.mu.Unlock()
					k.emitLog(proc, step, types.LogTool, errMsg, action.ToolPath)
					consecutiveToolErrors++
					if consecutiveToolErrors >= 3 {
						k.emitEvent(proc, "ReasonStep", map[string]any{
							"step":               step,
							"action":             "circuit_breaker",
							"consecutive_errors": consecutiveToolErrors,
						}, nil, nil, time.Since(stepStart))
						k.finishProcess(proc, ExitStatus{Code: 1, Reason: "circuit_breaker: 3 consecutive tool errors"})
						return
					}
					continue
				}
				k.emitEvent(proc, "Write", map[string]any{
					"fd":   toolFD,
					"size": len(action.ToolData),
				}, nil, nil, time.Since(toolWriteStart))
			}

			// Read tool result
			toolReadStart := time.Now()
			toolResult, err := k.vfs.Read(proc.PID, toolFD, 1<<20)
			k.emitEvent(proc, "Read", map[string]any{
				"fd":     toolFD,
				"length": 1 << 20,
			}, len(toolResult), err, time.Since(toolReadStart))
			if err != nil {
				_ = k.vfs.Close(proc.PID, toolFD)
				errMsg := fmt.Sprintf("Tool error (%s): read failed: %v", action.ToolPath, err)
				if appendErr := k.ctxMgr.AppendToolResult(proc.CtxID, action.ToolPath, errMsg); appendErr != nil {
					k.finishProcess(proc, ExitStatus{Code: 1, Reason: "append tool error failed", Err: appendErr})
					return
				}
				proc.mu.Lock()
				proc.HasToolError = true
				proc.mu.Unlock()
				k.emitLog(proc, step, types.LogTool, errMsg, action.ToolPath)
				consecutiveToolErrors++
				if consecutiveToolErrors >= 3 {
					k.emitEvent(proc, "ReasonStep", map[string]any{
						"step":               step,
						"action":             "circuit_breaker",
						"consecutive_errors": consecutiveToolErrors,
					}, nil, nil, time.Since(stepStart))
					k.finishProcess(proc, ExitStatus{Code: 1, Reason: "circuit_breaker: 3 consecutive tool errors"})
					return
				}
				continue
			}

			// Emit [tool] log entry with tool path and result summary
			toolContent := string(toolResult)
			if len(toolContent) > 500 {
				toolContent = toolContent[:500] + fmt.Sprintf("... (truncated, %d bytes total)", len(toolResult))
			}
			k.emitLog(proc, step, types.LogTool, toolContent, action.ToolPath)

			// Close tool device
			closeStart := time.Now()
			closeErr := k.vfs.Close(proc.PID, toolFD)
			k.emitEvent(proc, "Close", map[string]any{
				"fd": toolFD,
			}, nil, closeErr, time.Since(closeStart))

			// Write StepRecord before AppendToolResult (Story 27.1 AC-6).
			// Placed after tool execution to capture ToolResult/ToolDuration, but before
			// AppendToolResult. Messages snapshot consistency is guaranteed because
			// rec.Messages uses promptResult (immutable from BuildPrompt), not live context.
			k.writeStepRecord(proc, step, promptResult, rawResponseStr, &resp,
				"tool_call", briefToolCallSummary(action.ToolPath, string(toolResult)),
				action.ToolPath, string(action.ToolData), string(toolResult), "", time.Since(toolOpenStart))

			// Append tool result to context
			appendToolStart := time.Now()
			if err := k.ctxMgr.AppendToolResult(proc.CtxID, action.ToolPath, string(toolResult)); err != nil {
				k.emitEvent(proc, "CtxWrite", map[string]any{
					"cid":  proc.CtxID,
					"op":   "AppendToolResult",
					"tool": action.ToolPath,
				}, nil, err, time.Since(appendToolStart))
				k.finishProcess(proc, ExitStatus{Code: 1, Reason: "append tool result failed", Err: err})
				return
			}
			k.emitEvent(proc, "CtxWrite", map[string]any{
				"cid":  proc.CtxID,
				"op":   "AppendToolResult",
				"tool": action.ToolPath,
			}, nil, nil, time.Since(appendToolStart))

			consecutiveToolErrors = 0
			stepDur := time.Since(stepStart)
			k.emitEvent(proc, "ReasonStep", map[string]any{
				"step":   step,
				"action": "tool_call",
				"tool":   action.ToolPath,
			}, string(toolResult), nil, stepDur)
			if k.callbacks != nil {
				k.callbacks.OnStepComplete(proc.PID, step, "tool_call", briefToolCallSummary(action.ToolPath, string(toolResult)), false, float64(stepDur.Microseconds())/1000.0)
			}
			continue

		case ActionPlan:
			if !proc.PlanningEnabled {
				k.emitLog(proc, step, types.LogOutput, action.Content, "")
				// Write StepRecord (Story 27.1)
				k.writeStepRecord(proc, step, promptResult, rawResponseStr, &resp,
					"plan_as_text", briefTextSummary(action.Content), "", "", "", "", 0)
				proc.mu.Lock()
				proc.Result = action.Content
				proc.mu.Unlock()
				k.emitEvent(proc, "ReasonStep", map[string]any{
					"step":   step,
					"action": "plan_as_text",
				}, action.Content, nil, time.Since(stepStart))
				stepDur := time.Since(stepStart)
				if k.callbacks != nil {
					k.callbacks.OnStepComplete(proc.PID, step, "text", briefTextSummary(action.Content), false, float64(stepDur.Microseconds())/1000.0)
				}
				k.finishProcess(proc, ExitStatus{Code: 0, Reason: "completed"})
				return
			}

			planContent := fmt.Sprintf("[Plan]\n%s", string(action.ToolData))
			// Write StepRecord before AppendMessage (Story 27.1 AC-6)
			k.writeStepRecord(proc, step, promptResult, rawResponseStr, &resp,
				"plan", briefPlanSummary(action.ToolData), "", "", "", "", 0)
			appendStart := time.Now()
			if err := k.ctxMgr.AppendMessage(proc.CtxID, rnixctx.RoleAssistant, planContent); err != nil {
				k.emitEvent(proc, "CtxWrite", map[string]any{
					"cid":  proc.CtxID,
					"op":   "AppendMessage",
					"role": string(rnixctx.RoleAssistant),
				}, nil, err, time.Since(appendStart))
				k.finishProcess(proc, ExitStatus{Code: 1, Reason: "append plan failed", Err: err})
				return
			}
			k.emitEvent(proc, "CtxWrite", map[string]any{
				"cid":  proc.CtxID,
				"op":   "AppendMessage",
				"role": string(rnixctx.RoleAssistant),
			}, nil, nil, time.Since(appendStart))

			k.emitLog(proc, step, types.LogOutput, planContent, "")
			k.emitEvent(proc, "ReasonStep", map[string]any{
				"step":   step,
				"action": "plan",
			}, nil, nil, time.Since(stepStart))
			stepDur := time.Since(stepStart)
			if k.callbacks != nil {
				k.callbacks.OnStepComplete(proc.PID, step, "plan", briefPlanSummary(action.ToolData), false, float64(stepDur.Microseconds())/1000.0)
			}
			continue

		case ActionSpawn:
			// Write StepRecord before AppendMessage (Story 27.1 AC-6)
			k.writeStepRecord(proc, step, promptResult, rawResponseStr, &resp,
				"spawn", action.ToolPath, "", "", "", "", 0)
			appendAssistantStart2 := time.Now()
			if err := k.ctxMgr.AppendMessage(proc.CtxID, rnixctx.RoleAssistant, resp.Content); err != nil {
				k.emitEvent(proc, "CtxWrite", map[string]any{
					"cid":  proc.CtxID,
					"op":   "AppendMessage",
					"role": string(rnixctx.RoleAssistant),
				}, nil, err, time.Since(appendAssistantStart2))
				k.finishProcess(proc, ExitStatus{Code: 1, Reason: "append assistant message failed", Err: err})
				return
			}
			k.emitEvent(proc, "CtxWrite", map[string]any{
				"cid":  proc.CtxID,
				"op":   "AppendMessage",
				"role": string(rnixctx.RoleAssistant),
			}, nil, nil, time.Since(appendAssistantStart2))

			childOpts := SpawnOpts{
				ParentPID:     proc.PID,
				TraceID:       proc.TraceID,
				ProjectConfig: proc.ProjectConfig,
			}
			if proc.TraceID != "" {
				childOpts.ParentSpanID = proc.SpanID
			}

			var sd spawnActionData
			if len(action.ToolData) > 0 {
				_ = json.Unmarshal(action.ToolData, &sd)
			}
			if sd.Model != "" {
				childOpts.Model = sd.Model
			}

			var agentInfo *agents.AgentInfo
			spawnIntent := action.ToolPath
			if sd.Agent != "" {
				if k.agentLoader == nil {
					errMsg := fmt.Sprintf("spawn error: agent %q requested but no agent loader configured", sd.Agent)
					_ = k.ctxMgr.AppendToolResult(proc.CtxID, "spawn", errMsg)
					k.emitLog(proc, step, types.LogTool, errMsg, "spawn")
					k.emitEvent(proc, "ReasonStep", map[string]any{
						"step":   step,
						"action": "spawn_error",
					}, nil, fmt.Errorf("%s", errMsg), time.Since(stepStart))
					consecutiveToolErrors++
					if consecutiveToolErrors >= 3 {
						k.emitEvent(proc, "ReasonStep", map[string]any{
							"step":               step,
							"action":             "circuit_breaker",
							"consecutive_errors": consecutiveToolErrors,
						}, nil, nil, time.Since(stepStart))
						k.finishProcess(proc, ExitStatus{Code: 1, Reason: "circuit_breaker: 3 consecutive tool errors"})
						return
					}
					continue
				}
				var loadErr error
				agentInfo, loadErr = k.agentLoader(sd.Agent)
				if loadErr != nil {
					errMsg := fmt.Sprintf("spawn error: agent %q load failed: %v", sd.Agent, loadErr)
					_ = k.ctxMgr.AppendToolResult(proc.CtxID, "spawn", errMsg)
					k.emitLog(proc, step, types.LogTool, errMsg, "spawn")
					k.emitEvent(proc, "ReasonStep", map[string]any{
						"step":   step,
						"action": "spawn_error",
					}, nil, loadErr, time.Since(stepStart))
					consecutiveToolErrors++
					if consecutiveToolErrors >= 3 {
						k.emitEvent(proc, "ReasonStep", map[string]any{
							"step":               step,
							"action":             "circuit_breaker",
							"consecutive_errors": consecutiveToolErrors,
						}, nil, nil, time.Since(stepStart))
						k.finishProcess(proc, ExitStatus{Code: 1, Reason: "circuit_breaker: 3 consecutive tool errors"})
						return
					}
					continue
				}
			}

			childPID, spawnErr := k.Spawn(spawnIntent, agentInfo, childOpts)
			if spawnErr != nil {
				errMsg := fmt.Sprintf("spawn error: %v", spawnErr)
				_ = k.ctxMgr.AppendToolResult(proc.CtxID, "spawn", errMsg)
				k.emitLog(proc, step, types.LogTool, errMsg, "spawn")
				k.emitEvent(proc, "ReasonStep", map[string]any{
					"step":   step,
					"action": "spawn_error",
				}, nil, spawnErr, time.Since(stepStart))
				consecutiveToolErrors++
				if consecutiveToolErrors >= 3 {
					k.emitEvent(proc, "ReasonStep", map[string]any{
						"step":               step,
						"action":             "circuit_breaker",
						"consecutive_errors": consecutiveToolErrors,
					}, nil, nil, time.Since(stepStart))
					k.finishProcess(proc, ExitStatus{Code: 1, Reason: "circuit_breaker: 3 consecutive tool errors"})
					return
				}
				continue
			}

			childProc, childOk := k.GetProcess(childPID)
			if !childOk {
				errMsg := "spawn error: child process not found after spawn"
				_ = k.ctxMgr.AppendToolResult(proc.CtxID, "spawn", errMsg)
				k.emitLog(proc, step, types.LogTool, errMsg, "spawn")
				continue
			}

			var spawnResult string
			select {
			case exit := <-childProc.Done:
				childProc.mu.Lock()
				childResult := childProc.Result
				childProc.mu.Unlock()
				if exit.Code != 0 {
					spawnResult = fmt.Sprintf("child exited with code %d: %s", exit.Code, exit.Reason)
				} else {
					spawnResult = childResult
				}
			case <-proc.ctx.Done():
				k.emitEvent(proc, "ReasonStep", map[string]any{
					"step":   step,
					"action": "cancelled_waiting_child",
				}, nil, proc.ctx.Err(), time.Since(stepStart))
				k.finishProcess(proc, ExitStatus{Code: 1, Reason: "context cancelled while waiting for child"})
				return
			}

			appendResultStart := time.Now()
			if err := k.ctxMgr.AppendToolResult(proc.CtxID, "spawn", spawnResult); err != nil {
				k.emitEvent(proc, "CtxWrite", map[string]any{
					"cid":  proc.CtxID,
					"op":   "AppendToolResult",
					"tool": "spawn",
				}, nil, err, time.Since(appendResultStart))
				k.finishProcess(proc, ExitStatus{Code: 1, Reason: "append spawn result failed", Err: err})
				return
			}
			k.emitEvent(proc, "CtxWrite", map[string]any{
				"cid":  proc.CtxID,
				"op":   "AppendToolResult",
				"tool": "spawn",
			}, nil, nil, time.Since(appendResultStart))

			consecutiveToolErrors = 0
			k.emitLog(proc, step, types.LogTool, spawnResult, "spawn")
			k.emitEvent(proc, "ReasonStep", map[string]any{
				"step":      step,
				"action":    "spawn",
				"child_pid": childPID,
			}, spawnResult, nil, time.Since(stepStart))
			stepDur := time.Since(stepStart)
			if k.callbacks != nil {
				k.callbacks.OnStepComplete(proc.PID, step, "spawn", fmt.Sprintf("spawn PID %d %q", childPID, spawnIntent), false, float64(stepDur.Microseconds())/1000.0)
			}
			continue

		case ActionComplete:
			var completeData struct {
				Result string `json:"result"`
			}
			if len(action.ToolData) > 0 {
				_ = json.Unmarshal(action.ToolData, &completeData)
			}
			result := completeData.Result
			if result == "" {
				result = action.Content
			}

			k.emitLog(proc, step, types.LogOutput, result, "")
			// Write StepRecord (Story 27.1)
			k.writeStepRecord(proc, step, promptResult, rawResponseStr, &resp,
				"complete", briefTextSummary(result), "", "", "", "", 0)
			proc.mu.Lock()
			proc.Result = result
			proc.mu.Unlock()

			k.emitEvent(proc, "ReasonStep", map[string]any{
				"step":   step,
				"action": "complete",
			}, result, nil, time.Since(stepStart))
			stepDur := time.Since(stepStart)
			if k.callbacks != nil {
				k.callbacks.OnStepComplete(proc.PID, step, "complete", briefTextSummary(result), false, float64(stepDur.Microseconds())/1000.0)
			}
			k.finishProcess(proc, ExitStatus{Code: 0, Reason: "completed"})
			return

		case ActionReplan:
			var replanData struct {
				Reason string `json:"reason"`
			}
			if len(action.ToolData) > 0 {
				_ = json.Unmarshal(action.ToolData, &replanData)
			}
			reason := replanData.Reason
			if reason == "" {
				reason = action.Content
			}

			replanContent := fmt.Sprintf("[Replan] %s", reason)
			// Write StepRecord before AppendMessage (Story 27.1 AC-6)
			k.writeStepRecord(proc, step, promptResult, rawResponseStr, &resp,
				"replan", reason, "", "", "", "", 0)
			appendStart := time.Now()
			if err := k.ctxMgr.AppendMessage(proc.CtxID, rnixctx.RoleAssistant, replanContent); err != nil {
				k.emitEvent(proc, "CtxWrite", map[string]any{
					"cid":  proc.CtxID,
					"op":   "AppendMessage",
					"role": string(rnixctx.RoleAssistant),
				}, nil, err, time.Since(appendStart))
				k.finishProcess(proc, ExitStatus{Code: 1, Reason: "append replan failed", Err: err})
				return
			}
			k.emitEvent(proc, "CtxWrite", map[string]any{
				"cid":  proc.CtxID,
				"op":   "AppendMessage",
				"role": string(rnixctx.RoleAssistant),
			}, nil, nil, time.Since(appendStart))

			k.emitLog(proc, step, types.LogOutput, replanContent, "")
			k.emitEvent(proc, "ReasonStep", map[string]any{
				"step":   step,
				"action": "replan",
			}, nil, nil, time.Since(stepStart))
			stepDur := time.Since(stepStart)
			if k.callbacks != nil {
				k.callbacks.OnStepComplete(proc.PID, step, "replan", briefReplanSummary(reason), false, float64(stepDur.Microseconds())/1000.0)
			}
			continue

		case ActionSpecialize:
			// Write StepRecord (Story 27.1)
			k.writeStepRecord(proc, step, promptResult, rawResponseStr, &resp,
				"specialize", action.ToolPath, "", "", "", "", 0)
			skillName := action.ToolPath
			if skillName == "" {
				errMsg := "specialize error: empty skill name"
				_ = k.ctxMgr.AppendToolResult(proc.CtxID, "specialize", errMsg)
				k.emitLog(proc, step, types.LogTool, errMsg, "specialize")
				k.emitEvent(proc, "ReasonStep", map[string]any{
					"step":   step,
					"action": "specialize_error",
				}, nil, nil, time.Since(stepStart))
				continue
			}
			if k.skillLoader == nil {
				errMsg := "specialize error: no skill loader configured"
				_ = k.ctxMgr.AppendToolResult(proc.CtxID, "specialize", errMsg)
				k.emitLog(proc, step, types.LogTool, errMsg, "specialize")
				k.emitEvent(proc, "ReasonStep", map[string]any{
					"step":   step,
					"action": "specialize_error",
				}, nil, nil, time.Since(stepStart))
				continue
			}

			// TOCTOU first check: is this skill already loaded?
			proc.mu.Lock()
			alreadyLoaded := slices.Contains(proc.Skills, skillName)
			proc.mu.Unlock()
			if alreadyLoaded {
				resultMsg := fmt.Sprintf("skill %q is already loaded — its instructions are in your system prompt. Follow them using available VFS devices (/dev/fs, /dev/shell, etc.). Do NOT try to call this skill as a tool.", skillName)
				_ = k.ctxMgr.AppendToolResult(proc.CtxID, "specialize", resultMsg)
				k.emitLog(proc, step, types.LogTool, resultMsg, "specialize")
				k.emitEvent(proc, "ReasonStep", map[string]any{
					"step":   step,
					"action": "specialize_already_loaded",
					"skill":  skillName,
				}, nil, nil, time.Since(stepStart))
				continue
			}

			// Load skill outside lock (I/O may be slow)
			skillInfo, loadErr := k.skillLoader(skillName)
			if loadErr != nil {
				errMsg := fmt.Sprintf("specialize error: skill %q load failed: %v", skillName, loadErr)
				_ = k.ctxMgr.AppendToolResult(proc.CtxID, "specialize", errMsg)
				k.emitLog(proc, step, types.LogTool, errMsg, "specialize")
				k.emitEvent(proc, "ReasonStep", map[string]any{
					"step":   step,
					"action": "specialize_error",
					"skill":  skillName,
				}, nil, loadErr, time.Since(stepStart))
				continue
			}

			// TOCTOU second check under lock to prevent concurrent duplicate
			proc.mu.Lock()
			if slices.Contains(proc.Skills, skillName) {
				proc.mu.Unlock()
				resultMsg := fmt.Sprintf("skill %q is already loaded — its instructions are in your system prompt. Follow them using available VFS devices (/dev/fs, /dev/shell, etc.). Do NOT try to call this skill as a tool.", skillName)
				_ = k.ctxMgr.AppendToolResult(proc.CtxID, "specialize", resultMsg)
				k.emitLog(proc, step, types.LogTool, resultMsg, "specialize")
				k.emitEvent(proc, "ReasonStep", map[string]any{
					"step":   step,
					"action": "specialize_already_loaded",
					"skill":  skillName,
				}, nil, nil, time.Since(stepStart))
				continue
			}
			proc.Skills = append(proc.Skills, skillName)
			proc.AllowedDevices = append(proc.AllowedDevices, skillInfo.Manifest.AllowedTools()...)
			totalSkills := len(proc.Skills)
			allSkills := make([]string, totalSkills)
			copy(allSkills, proc.Skills)
			proc.mu.Unlock()

			// Inject skill body into context as RoleUser
			if skillInfo.Body != "" {
				appendStart := time.Now()
				dirHint := ""
				if skillInfo.Dir != "" {
					dirHint = fmt.Sprintf("Base directory for this skill: %s\n\n", skillInfo.Dir)
				}
				if appendErr := k.ctxMgr.AppendMessage(proc.CtxID, rnixctx.RoleUser,
					fmt.Sprintf("[Dynamic Skill Loaded: %s]\nThe following are instructions from skill %q. Follow these instructions using VFS devices. Do NOT try to call this skill as a tool.\n\n%s%s", skillName, skillName, dirHint, skillInfo.Body)); appendErr != nil {
					k.emitLog(proc, step, types.LogTool, fmt.Sprintf(
						"specialize warning: failed to inject skill body for %q: %v", skillName, appendErr), "specialize")
					k.emitEvent(proc, "CtxWrite", map[string]any{
						"cid":  proc.CtxID,
						"op":   "AppendMessage",
						"role": string(rnixctx.RoleUser),
					}, nil, appendErr, time.Since(appendStart))
				}
			}

			// Emit specialize event
			k.emitEvent(proc, "StemSpecialize", map[string]any{
				"skill":        skillName,
				"total_skills": totalSkills,
			}, nil, nil, 0)

			// Update differentiation memory
			if k.diffMemory != nil {
				k.diffMemory.Record(proc.Intent, allSkills)
			}

			// Record progressive specialization lineage
			if proc.lineage != nil {
				trigger := action.Content
				if trigger == "" {
					trigger = "specialize"
				}
				proc.lineage.Record(LineageEvent{
					Timestamp: time.Now(),
					Phase:     "progressive",
					Skills:    []string{skillName},
					Trigger:   trigger,
				})
			}

			// Return success as tool message
			resultMsg := fmt.Sprintf("skill %q loaded successfully", skillName)
			_ = k.ctxMgr.AppendToolResult(proc.CtxID, "specialize", resultMsg)
			k.emitLog(proc, step, types.LogTool, resultMsg, "specialize")
			k.emitEvent(proc, "ReasonStep", map[string]any{
				"step":   step,
				"action": "specialize",
				"skill":  skillName,
			}, nil, nil, time.Since(stepStart))
			stepDur := time.Since(stepStart)
			if k.callbacks != nil {
				k.callbacks.OnStepComplete(proc.PID, step, "specialize", skillName, false, float64(stepDur.Microseconds())/1000.0)
			}
			continue
		}
	}

	// Max steps exceeded — incomplete reasoning
	k.finishProcess(proc, ExitStatus{Code: 1, Reason: "max steps exceeded"})
}

// briefToolCallSummary generates "{toolPath} → {briefResult}" for OnStepComplete.
func briefToolCallSummary(toolPath, toolResult string) string {
	brief := strings.ReplaceAll(toolResult, "\n", " ")
	r := []rune(brief)
	if len(r) > 60 {
		brief = string(r[:60]) + "..."
	}
	if brief == "" {
		brief = "ok"
	}
	return toolPath + " → " + brief
}

// briefPlanSummary generates "plan (N steps)" from plan ToolData JSON.
func briefPlanSummary(toolData json.RawMessage) string {
	var planData struct {
		Steps []json.RawMessage `json:"steps"`
	}
	if err := json.Unmarshal(toolData, &planData); err != nil || len(planData.Steps) == 0 {
		return "plan"
	}
	return fmt.Sprintf("plan (%d steps)", len(planData.Steps))
}

// briefReplanSummary generates a truncated replan reason.
func briefReplanSummary(reason string) string {
	r := []rune(reason)
	if len(r) > 40 {
		return string(r[:40]) + "..."
	}
	return reason
}

// briefTextSummary extracts the first non-empty line from text content, truncated to 60 runes.
func briefTextSummary(content string) string {
	// Find first non-empty line
	for line := range strings.SplitSeq(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		r := []rune(line)
		if len(r) > 60 {
			return string(r[:60]) + "..."
		}
		return line
	}
	return ""
}

// executeNativeToolCalls processes native tool calls from the LLM response.
// Returns true if the reasonStep loop should continue, false if the process should exit.
func (k *KernelImpl) executeNativeToolCalls(proc *Process, resp llmResponse, step int, stepStart time.Time, consecutiveToolErrors *int) bool {
	// Append assistant message with tool calls to context
	if err := k.ctxMgr.AppendAssistantWithToolCalls(proc.CtxID, resp.Content, convertToolCalls(resp.ToolCalls)); err != nil {
		k.finishProcess(proc, ExitStatus{Code: 1, Reason: "append assistant with tool calls failed", Err: err})
		return false
	}

	// Emit [think] log entry
	k.emitLog(proc, step, types.LogThink, resp.Content, "")

	for _, tc := range resp.ToolCalls {
		// Check for argument parse errors before dispatch
		if tc.ParseError != "" {
			errMsg := fmt.Sprintf("Tool error (%s): arguments parse failed: %s", tc.Name, tc.ParseError)
			_ = k.ctxMgr.AppendToolResult(proc.CtxID, tc.ID, errMsg)
			k.emitLog(proc, step, types.LogTool, errMsg, tc.Name)
			*consecutiveToolErrors++
			if *consecutiveToolErrors >= 3 {
				k.finishProcess(proc, ExitStatus{Code: 1, Reason: "circuit_breaker: 3 consecutive tool errors"})
				return false
			}
			continue
		}

		mapping, ok := proc.toolMap[tc.Name]
		if !ok {
			// Unknown tool
			errMsg := "error: unknown tool " + tc.Name
			_ = k.ctxMgr.AppendToolResult(proc.CtxID, tc.ID, errMsg)
			k.emitLog(proc, step, types.LogTool, errMsg, tc.Name)
			*consecutiveToolErrors++
			if *consecutiveToolErrors >= 3 {
				k.finishProcess(proc, ExitStatus{Code: 1, Reason: "circuit_breaker: 3 consecutive tool errors"})
				return false
			}
			continue
		}

		switch mapping.Type {
		case "vfs":
			result, err := k.executeNativeVFSTool(proc, tc, mapping)
			if err != nil {
				errMsg := fmt.Sprintf("Tool error (%s): %v", tc.Name, err)
				_ = k.ctxMgr.AppendToolResult(proc.CtxID, tc.ID, errMsg)
				proc.mu.Lock()
				proc.HasToolError = true
				proc.mu.Unlock()
				k.emitLog(proc, step, types.LogTool, errMsg, mapping.VFSPath)
				*consecutiveToolErrors++
				if *consecutiveToolErrors >= 3 {
					k.finishProcess(proc, ExitStatus{Code: 1, Reason: "circuit_breaker: 3 consecutive tool errors"})
					return false
				}
				continue
			}
			_ = k.ctxMgr.AppendToolResult(proc.CtxID, tc.ID, result)
			toolContent := result
			if len(toolContent) > 500 {
				toolContent = toolContent[:500] + fmt.Sprintf("... (truncated, %d bytes total)", len(result))
			}
			k.emitLog(proc, step, types.LogTool, toolContent, mapping.VFSPath)
			*consecutiveToolErrors = 0

			k.emitEvent(proc, "ReasonStep", map[string]any{
				"step":   step,
				"action": "native_tool_call",
				"tool":   tc.Name,
			}, result, nil, time.Since(stepStart))
			stepDur := time.Since(stepStart)
			if k.callbacks != nil {
				k.callbacks.OnStepComplete(proc.PID, step, "tool_call", briefToolCallSummary(mapping.VFSPath, result), false, float64(stepDur.Microseconds())/1000.0)
			}

		case "meta":
			shouldContinue := k.executeNativeMetaAction(proc, tc, mapping, step, stepStart)
			if !shouldContinue {
				return false
			}
		}
	}

	return true
}

// executeNativeVFSTool executes a VFS tool call from native function calling.
func (k *KernelImpl) executeNativeVFSTool(proc *Process, tc llmToolCall, mapping toolMapping) (string, error) {
	// Permission check using VFS path
	if len(proc.AllowedDevices) > 0 {
		cleanPath := path.Clean(mapping.VFSPath)
		allowed := false
		for _, dev := range proc.AllowedDevices {
			if cleanPath == dev || strings.HasPrefix(cleanPath, dev+"/") {
				allowed = true
				break
			}
		}
		if !allowed {
			return "", fmt.Errorf("permission denied: device %s not in allowed list %v", mapping.VFSPath, proc.AllowedDevices)
		}
	}

	switch mapping.FSOperation {
	case "read_file":
		pathStr, _ := tc.Input["path"].(string)
		if pathStr == "" {
			received, _ := json.Marshal(tc.Input)
			return "", fmt.Errorf("read_file: missing required 'path' parameter. Received: %s. Expected: {\"path\": \"<relative_path>\"}", string(received))
		}
		vfsPath := mapping.VFSPath + "/" + pathStr
		fd, err := k.vfs.Open(proc.PID, vfsPath, vfs.O_RDONLY)
		if err != nil {
			return "", fmt.Errorf("open failed: %w", err)
		}
		data, err := k.vfs.Read(proc.PID, fd, 1<<20)
		_ = k.vfs.Close(proc.PID, fd)
		if err != nil {
			return "", fmt.Errorf("read failed: %w", err)
		}
		return string(data), nil

	case "write_file":
		pathStr, _ := tc.Input["path"].(string)
		if pathStr == "" {
			received, _ := json.Marshal(tc.Input)
			return "", fmt.Errorf("write_file: missing required 'path' parameter. Received: %s. Expected: {\"path\": \"<relative_path>\", \"content\": \"<text>\"}", string(received))
		}
		contentStr, _ := tc.Input["content"].(string)
		vfsPath := mapping.VFSPath + "/" + pathStr
		fd, err := k.vfs.Open(proc.PID, vfsPath, vfs.O_WRONLY)
		if err != nil {
			return "", fmt.Errorf("open failed: %w", err)
		}
		writeData, _ := json.Marshal(map[string]string{"content": contentStr})
		if err := k.vfs.Write(proc.ctx, proc.PID, fd, writeData); err != nil {
			_ = k.vfs.Close(proc.PID, fd)
			return "", fmt.Errorf("write failed: %w", err)
		}
		data, err := k.vfs.Read(proc.PID, fd, 1<<20)
		_ = k.vfs.Close(proc.PID, fd)
		if err != nil {
			return "", fmt.Errorf("read result failed: %w", err)
		}
		return string(data), nil

	case "list_dir":
		pathStr, _ := tc.Input["path"].(string)
		if pathStr == "" {
			received, _ := json.Marshal(tc.Input)
			return "", fmt.Errorf("list_dir: missing required 'path' parameter. Received: %s. Expected: {\"path\": \"<relative_path>\"}", string(received))
		}
		vfsPath := mapping.VFSPath + "/" + pathStr
		fd, err := k.vfs.Open(proc.PID, vfsPath, vfs.O_WRONLY)
		if err != nil {
			return "", fmt.Errorf("open failed: %w", err)
		}
		writeData, _ := json.Marshal(map[string]string{"op": "list"})
		if err := k.vfs.Write(proc.ctx, proc.PID, fd, writeData); err != nil {
			_ = k.vfs.Close(proc.PID, fd)
			return "", fmt.Errorf("write failed: %w", err)
		}
		data, err := k.vfs.Read(proc.PID, fd, 1<<20)
		_ = k.vfs.Close(proc.PID, fd)
		if err != nil {
			return "", fmt.Errorf("read result failed: %w", err)
		}
		return string(data), nil

	default:
		// Generic VFS tool (e.g., shell)
		inputData, _ := json.Marshal(tc.Input)
		fd, err := k.vfs.Open(proc.PID, mapping.VFSPath, vfs.O_RDWR)
		if err != nil {
			return "", fmt.Errorf("open failed: %w", err)
		}
		if err := k.vfs.Write(proc.ctx, proc.PID, fd, inputData); err != nil {
			_ = k.vfs.Close(proc.PID, fd)
			return "", fmt.Errorf("write failed: %w", err)
		}
		data, err := k.vfs.Read(proc.PID, fd, 1<<20)
		_ = k.vfs.Close(proc.PID, fd)
		if err != nil {
			return "", fmt.Errorf("read failed: %w", err)
		}
		return string(data), nil
	}
}

// executeNativeMetaAction executes a kernel meta action from native function calling.
// Returns true if the reasonStep loop should continue.
func (k *KernelImpl) executeNativeMetaAction(proc *Process, tc llmToolCall, mapping toolMapping, step int, stepStart time.Time) bool {
	switch mapping.Action {
	case ActionComplete:
		resultStr, _ := tc.Input["result"].(string)
		proc.mu.Lock()
		proc.Result = resultStr
		hadError := proc.HasToolError
		proc.mu.Unlock()
		k.emitLog(proc, step, types.LogOutput, resultStr, "")
		k.emitEvent(proc, "ReasonStep", map[string]any{
			"step":   step,
			"action": "complete",
		}, resultStr, nil, time.Since(stepStart))
		stepDur := time.Since(stepStart)
		if k.callbacks != nil {
			k.callbacks.OnStepComplete(proc.PID, step, "complete", briefTextSummary(resultStr), false, float64(stepDur.Microseconds())/1000.0)
		}
		exitCode := 0
		reason := "completed"
		if hadError {
			exitCode = 1
			reason = "completed_with_tool_errors"
		}
		k.finishProcess(proc, ExitStatus{Code: exitCode, Reason: reason})
		return false

	case ActionSpawn:
		intentStr, _ := tc.Input["intent"].(string)
		agentStr, _ := tc.Input["agent"].(string)
		modelStr, _ := tc.Input["model"].(string)
		childOpts := SpawnOpts{Model: modelStr}
		var agentInfo *agents.AgentInfo
		if agentStr != "" && k.agentLoader != nil {
			if ai, err := k.agentLoader(agentStr); err == nil {
				agentInfo = ai
			}
		}
		childPID, err := k.Spawn(intentStr, agentInfo, childOpts)
		if err != nil {
			errMsg := fmt.Sprintf("spawn failed: %v", err)
			_ = k.ctxMgr.AppendToolResult(proc.CtxID, tc.ID, errMsg)
			return true
		}
		// Wait for child
		childExit, _ := k.Wait(childPID)
		childProc, _ := k.GetProcess(childPID)
		result := ""
		if childProc != nil {
			result = childProc.Result
		}
		if result == "" {
			result = fmt.Sprintf("child PID %d exited: %s (code %d)", childPID, childExit.Reason, childExit.Code)
		}
		_ = k.ctxMgr.AppendToolResult(proc.CtxID, tc.ID, result)
		k.emitEvent(proc, "ReasonStep", map[string]any{
			"step":      step,
			"action":    "spawn",
			"child_pid": childPID,
		}, result, nil, time.Since(stepStart))
		return true

	case ActionReplan:
		reasonStr, _ := tc.Input["reason"].(string)
		msg := fmt.Sprintf("Replanning: %s", reasonStr)
		_ = k.ctxMgr.AppendToolResult(proc.CtxID, tc.ID, msg)
		k.emitLog(proc, step, types.LogOutput, msg, "")
		k.emitEvent(proc, "ReasonStep", map[string]any{
			"step":   step,
			"action": "replan",
		}, msg, nil, time.Since(stepStart))
		return true

	case ActionSpecialize:
		skillName, _ := tc.Input["skill_name"].(string)
		var resultMsg string
		if k.skillLoader != nil {
			if skill, err := k.skillLoader(skillName); err == nil {
				_ = k.ctxMgr.AppendMessage(proc.CtxID, rnixctx.RoleSystem, skill.Body)
				proc.mu.Lock()
				proc.Skills = append(proc.Skills, skillName)
				proc.mu.Unlock()
				resultMsg = fmt.Sprintf("Skill '%s' loaded successfully", skillName)
			} else {
				resultMsg = fmt.Sprintf("Failed to load skill '%s': %v", skillName, err)
			}
		} else {
			resultMsg = "skill loader not available"
		}
		_ = k.ctxMgr.AppendToolResult(proc.CtxID, tc.ID, resultMsg)
		k.emitEvent(proc, "ReasonStep", map[string]any{
			"step":   step,
			"action": "specialize",
			"skill":  skillName,
		}, resultMsg, nil, time.Since(stepStart))
		return true

	case ActionPlan:
		stepsAny, _ := tc.Input["steps"].([]any)
		reasonStr, _ := tc.Input["reason"].(string)
		var steps []string
		for _, s := range stepsAny {
			if str, ok := s.(string); ok {
				steps = append(steps, str)
			}
		}
		var planContent strings.Builder
		fmt.Fprintf(&planContent, "Plan (%s):\n", reasonStr)
		for i, s := range steps {
			fmt.Fprintf(&planContent, "  %d. %s\n", i+1, s)
		}
		_ = k.ctxMgr.AppendToolResult(proc.CtxID, tc.ID, planContent.String())
		k.emitLog(proc, step, types.LogOutput, planContent.String(), "")
		k.emitEvent(proc, "ReasonStep", map[string]any{
			"step":   step,
			"action": "plan",
		}, planContent.String(), nil, time.Since(stepStart))
		stepDur := time.Since(stepStart)
		if k.callbacks != nil {
			k.callbacks.OnStepComplete(proc.PID, step, "plan", "Created plan with "+fmt.Sprintf("%d steps", len(steps)), false, float64(stepDur.Microseconds())/1000.0)
		}
		return true
	}

	return true
}

// spawnActionData contains optional parameters parsed from spawn action data.
type spawnActionData struct {
	Agent string `json:"agent,omitempty"`
	Model string `json:"model,omitempty"`
}

// parseAction determines the action type from an LLM response.
func parseAction(resp *llmResponse) ReasonAction {
	if action, ok := tryParseStructuredAction(resp.Content); ok {
		return action
	}

	// Fallback: extract JSON from markdown code blocks or embedded objects.
	// Models often wrap JSON actions in ```json ... ``` or mix text with JSON.
	if extracted := extractEmbeddedAction(resp.Content); extracted != "" {
		if action, ok := tryParseStructuredAction(extracted); ok {
			return action
		}
	}

	return ReasonAction{Type: ActionText, Content: resp.Content}
}

// tryParseStructuredAction attempts to parse raw JSON text as a structured action.
func tryParseStructuredAction(raw string) (ReasonAction, bool) {
	var structured struct {
		Action string          `json:"action"`
		Tool   string          `json:"tool,omitempty"`
		Data   json.RawMessage `json:"data,omitempty"`
	}
	if err := json.Unmarshal([]byte(raw), &structured); err != nil {
		return ReasonAction{}, false
	}
	toolData := structured.Data
	if toolData == nil {
		toolData = []byte("{}")
	}
	switch ActionType(structured.Action) {
	case ActionToolCall:
		if structured.Tool != "" {
			return ReasonAction{
				Type:     ActionToolCall,
				ToolPath: structured.Tool,
				ToolData: toolData,
			}, true
		}
	case ActionPlan:
		return ReasonAction{
			Type:     ActionPlan,
			Content:  raw,
			ToolData: toolData,
		}, true
	case ActionSpawn:
		return ReasonAction{
			Type:     ActionSpawn,
			Content:  raw,
			ToolPath: structured.Tool,
			ToolData: toolData,
		}, true
	case ActionComplete:
		return ReasonAction{
			Type:     ActionComplete,
			Content:  raw,
			ToolData: toolData,
		}, true
	case ActionReplan:
		return ReasonAction{
			Type:     ActionReplan,
			Content:  raw,
			ToolData: toolData,
		}, true
	case ActionSpecialize:
		return ReasonAction{
			Type:     ActionSpecialize,
			ToolPath: structured.Tool,
			Content:  raw,
			ToolData: toolData,
		}, true
	}
	return ReasonAction{}, false
}

// extractEmbeddedAction extracts a JSON action from text that may contain
// markdown code blocks (```json ... ```) or inline JSON objects ({"action": ...}).
func extractEmbeddedAction(content string) string {
	// Strategy 1: markdown code block — ```json\n{...}\n``` or ```\n{...}\n```
	for _, fence := range []string{"```json\n", "```json\r\n", "```\n", "```\r\n"} {
		start := strings.Index(content, fence)
		if start < 0 {
			continue
		}
		jsonStart := start + len(fence)
		end := strings.Index(content[jsonStart:], "\n```")
		if end < 0 {
			end = strings.Index(content[jsonStart:], "\r\n```")
		}
		if end < 0 {
			continue
		}
		candidate := strings.TrimSpace(content[jsonStart : jsonStart+end])
		if len(candidate) > 0 && candidate[0] == '{' {
			return candidate
		}
	}

	// Strategy 2: find the last top-level JSON object containing "action"
	// This handles models that output text followed by a bare JSON block.
	if idx := strings.LastIndex(content, `{"action"`); idx >= 0 {
		candidate := content[idx:]
		depth := 0
		for i, c := range candidate {
			switch c {
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					return candidate[:i+1]
				}
			}
		}
	}

	return ""
}

// AddProcess registers a process in the kernel's process table.
func (k *KernelImpl) AddProcess(p *Process) {
	k.procTable.Store(p.PID, p)
}

// GetProcess retrieves a process by PID.
func (k *KernelImpl) GetProcess(pid types.PID) (*Process, bool) {
	return k.procTable.Load(pid)
}

// GetProcessByUUID finds a process by UUID in the process table.
// Returns (nil, false) if no matching process is found or uuid is empty.
func (k *KernelImpl) GetProcessByUUID(uuid string) (*Process, bool) {
	if uuid == "" {
		return nil, false
	}
	var found *Process
	k.procTable.Range(func(pid types.PID, proc *Process) bool {
		if proc.UUID == uuid {
			found = proc
			return false // stop iteration
		}
		return true
	})
	return found, found != nil
}

// RemoveProcess removes a process from the process table.
func (k *KernelImpl) RemoveProcess(pid types.PID) {
	k.procTable.Delete(pid)
}

// FindHistoryByPID returns the most recent history snapshot for a reaped process, or nil.
func (k *KernelImpl) FindHistoryByPID(pid types.PID) *vfs.ProcInfo {
	return k.procHistory.FindByPID(pid)
}

// RegisterBudgetPool associates a BudgetPool with a process group.
func (k *KernelImpl) RegisterBudgetPool(groupID types.PGID, pool *BudgetPool) {
	k.budgetPools.Store(groupID, pool)
}

// UnregisterBudgetPool removes a BudgetPool association (called on compose down).
func (k *KernelImpl) UnregisterBudgetPool(groupID types.PGID) {
	k.budgetPools.Delete(groupID)
}

// GetBudgetStatus returns a snapshot of the budget pool for the given group.
func (k *KernelImpl) GetBudgetStatus(groupID types.PGID) (*BudgetPoolStatus, error) {
	pool, ok := k.budgetPools.Load(groupID)
	if !ok {
		return nil, fmt.Errorf("no budget pool for group %d", groupID)
	}
	status := pool.GetStatus()
	return &status, nil
}

// RecordSLAResult appends an SLA evaluation result for a compose group (Story 21.2).
func (k *KernelImpl) RecordSLAResult(groupID types.PGID, result *SLAResult) {
	k.slaResultsMu.Lock()
	defer k.slaResultsMu.Unlock()
	existing, ok := k.slaResults.Load(groupID)
	if !ok {
		existing = []*SLAResult{}
	}
	existing = append(existing, result)
	k.slaResults.Store(groupID, existing)
}

// GetSLAResults returns the SLA evaluation results for a compose group (Story 21.2).
func (k *KernelImpl) GetSLAResults(groupID types.PGID) ([]*SLAResult, error) {
	results, ok := k.slaResults.Load(groupID)
	if !ok {
		return nil, fmt.Errorf("no SLA results for group %d", groupID)
	}
	return results, nil
}

// GetLineage returns the lineage events for the given PID.
// Returns nil events (no error) if the process has no lineage (not a differentiated process).
// Returns an error if the process is not found.
func (k *KernelImpl) GetLineage(pid types.PID) ([]LineageEvent, error) {
	proc, ok := k.GetProcess(pid)
	if !ok {
		return nil, NewSyscallError("GetLineage", pid, "", fmt.Errorf("process not found"), types.ErrNotFound)
	}
	lineage := proc.GetLineage()
	if lineage == nil {
		return nil, nil
	}
	return lineage.Events(), nil
}

// Kill sends a signal to the target process.
// If the process is already Zombie or Dead, Kill is a no-op (idempotent).
// Returns *SyscallError with ErrNotFound if the PID does not exist.
func (k *KernelImpl) Kill(pid types.PID, signal types.Signal) error {
	start := time.Now()

	if !signal.Valid() {
		return NewSyscallError("Kill", pid, "", fmt.Errorf("invalid signal %d", signal), types.ErrInvalid)
	}

	proc, ok := k.GetProcess(pid)
	if !ok {
		return NewSyscallError("Kill", pid, "", fmt.Errorf("process not found"), types.ErrNotFound)
	}

	// Emit entry event
	k.emitEvent(proc, "Kill", map[string]any{
		"pid":    pid,
		"signal": signal.String(),
	}, nil, nil, 0)

	state := proc.GetState()
	if state == types.StateZombie || state == types.StateDead {
		// Idempotent: already stopped
		k.emitEvent(proc, "Kill", map[string]any{
			"pid":    pid,
			"signal": signal.String(),
			"action": "noop",
		}, nil, nil, time.Since(start))
		return nil
	}

	// Delegate to deliverSignal for actual dispatch (handler/blocking/default logic)
	action := k.deliverSignal(proc, signal)

	k.emitEvent(proc, "Kill", map[string]any{
		"pid":    pid,
		"signal": signal.String(),
		"action": action,
	}, nil, nil, time.Since(start))

	return nil
}

// ListProcesses returns all processes currently in the process table.
func (k *KernelImpl) ListProcesses() []*Process {
	var procs []*Process
	k.procTable.Range(func(_ types.PID, p *Process) bool {
		procs = append(procs, p)
		return true
	})
	return procs
}

// GetSpanID returns the SpanID for the given process, if it has one.
// Used by IPC server for compose parent-child trace propagation (Story 15.1).
func (k *KernelImpl) GetSpanID(pid types.PID) (types.SpanID, bool) {
	proc, ok := k.GetProcess(pid)
	if !ok {
		return "", false
	}
	proc.mu.Lock()
	spanID := proc.SpanID
	proc.mu.Unlock()
	return spanID, spanID != ""
}

// GetProcInfo returns a snapshot of process information for the given PID.
// Satisfies vfs.ProcessInfoProvider interface via duck typing.
func (k *KernelImpl) GetProcInfo(pid types.PID) (*vfs.ProcInfo, error) {
	proc, ok := k.GetProcess(pid)
	if !ok {
		return nil, &vfs.VFSError{
			Op:     "GetProcInfo",
			Device: "/proc",
			Err:    fmt.Errorf("process %d not found", pid),
			Code:   types.ErrNotFound,
		}
	}

	proc.mu.Lock()
	info := &vfs.ProcInfo{
		PID:            proc.PID,
		UUID:           proc.UUID,
		PPID:           proc.PPID,
		State:          proc.State,
		Intent:         proc.Intent,
		Skills:         append([]string(nil), proc.Skills...),
		TokensUsed:     proc.TokensUsed,
		ContextBudget:  proc.ContextBudget,
		MaxSteps:       proc.MaxSteps,
		CreatedAt:      proc.CreatedAt,
		DeadAt:         proc.DeadAt,
		CtxID:          proc.CtxID,
		Result:         proc.Result,
		AllowedDevices: append([]string(nil), proc.AllowedDevices...),
		Provider:       proc.Provider,
		Model:          proc.Model,
	}
	proc.mu.Unlock()
	return info, nil
}

// GetTokenHistory returns a copy of the token usage history for a process.
func (k *KernelImpl) GetTokenHistory(pid types.PID) ([]types.TokenSnapshot, error) {
	proc, ok := k.GetProcess(pid)
	if !ok {
		return nil, NewSyscallError("GetTokenHistory", pid, "", fmt.Errorf("process %d not found", pid), types.ErrNotFound)
	}
	history := proc.GetTokenHistory()
	if history == nil {
		return []types.TokenSnapshot{}, nil
	}
	return history, nil
}

// driverEventToSyscall maps a driver event type to a precise syscall name.
func driverEventToSyscall(evtType string) string {
	switch evtType {
	case "tool_call":
		return "DriverToolCall"
	case "thinking":
		return "DriverThinking"
	case "system":
		return "DriverInit"
	case "user":
		return "DriverUser"
	default:
		return "DriverEvent"
	}
}

// driverEventToLog maps a driver event to a LogEntry category and content.
// Returns false if the event should not be logged (e.g., system init, user, empty thinking deltas).
func driverEventToLog(evt map[string]any) (types.LogCategory, string, string, bool) {
	evtType, _ := evt["type"].(string)
	subtype, _ := evt["subtype"].(string)
	contentField, _ := evt["content"].(string)

	switch evtType {
	case "tool_call":
		tool, _ := evt["tool"].(string)
		desc, _ := evt["description"].(string)
		path, _ := evt["path"].(string)
		// Prefer description, fall back to content field ("started"/"completed"), then subtype
		content := contentField
		if subtype != "" && content == "" {
			content = subtype
		}
		if desc != "" {
			content = desc
		}
		toolPath := tool
		if path != "" {
			toolPath = tool + ":" + path
		}
		return types.LogTool, content, toolPath, true
	case "thinking":
		text := strings.TrimSpace(contentField)
		if text == "" || text == subtype {
			// Skip empty thinking deltas and bare "delta"/"completed" labels
			return "", "", "", false
		}
		r := []rune(text)
		if len(r) > 80 {
			text = string(r[:80]) + "..."
		}
		return types.LogThink, text, "", true
	default:
		return "", "", "", false
	}
}

// emitLog sends a LogEntry to the process LogChan (non-blocking).
// Holds proc.mu only during channel access to prevent races with reapProcess close.
func (k *KernelImpl) emitLog(proc *Process, step int, cat types.LogCategory, content, toolPath string) {
	entry := types.LogEntry{
		Timestamp: time.Since(proc.CreatedAt),
		PID:       proc.PID,
		Step:      step,
		Category:  cat,
		Content:   content,
		ToolPath:  toolPath,
	}
	proc.mu.Lock()
	proc.AppendLogHistory(entry)
	ch := proc.LogChan
	if ch != nil {
		select {
		case ch <- entry:
		default:
		}
	}
	proc.mu.Unlock()
}

// GetLogChan safely retrieves the log channel for a process under lock.
// Returns nil, false if the process doesn't exist or the channel is nil.
func (k *KernelImpl) GetLogChan(pid types.PID) (chan types.LogEntry, bool) {
	proc, ok := k.GetProcess(pid)
	if !ok {
		return nil, false
	}
	proc.mu.Lock()
	ch := proc.LogChan
	proc.mu.Unlock()
	return ch, ch != nil
}

// GetLogHistory returns a copy of the log history for a process.
// Returns nil, false if the process doesn't exist.
func (k *KernelImpl) GetLogHistory(pid types.PID) ([]types.LogEntry, bool) {
	proc, ok := k.GetProcess(pid)
	if !ok {
		return nil, false
	}
	proc.mu.Lock()
	history := proc.GetLogHistory()
	proc.mu.Unlock()
	return history, true
}

// GetDebugChan safely retrieves the debug channel for a process under lock.
// Returns nil, false if the process doesn't exist or the channel is nil.
func (k *KernelImpl) GetDebugChan(pid types.PID) (chan types.SyscallEvent, bool) {
	proc, ok := k.GetProcess(pid)
	if !ok {
		return nil, false
	}
	proc.mu.Lock()
	ch := proc.DebugChan
	proc.mu.Unlock()
	return ch, ch != nil
}

// recordContextSnapshot captures a context snapshot for recording (Story 14.1).
// Simplified in Story 27.1: removed summary fields (StepRecord captures complete data).
func (k *KernelImpl) recordContextSnapshot(proc *Process) {
	ctxEvent := debug.RecordEvent{
		Timestamp: time.Since(proc.CreatedAt),
		PID:       proc.PID,
		Type:      debug.RecordContextSnapshot,
		Context:   &debug.ContextSnapshotData{},
	}
	if err := k.recordMgr.RecordEvent(proc.PID, ctxEvent); err != nil {
		log.Printf("[record] context snapshot write error pid=%d: %v", proc.PID, err)
	}
}

// writeStepRecord assembles and writes a StepRecord to the process's StepWriter.
// Best-effort: errors are logged but do not affect the reasoning loop.
func (k *KernelImpl) writeStepRecord(proc *Process, step int, promptResult *rnixctx.PromptResult,
	rawResponse string, resp *llmResponse, actionType string, summary string,
	toolPath string, toolInput string, toolResult string, toolError string, toolDuration time.Duration) {

	proc.mu.Lock()
	sw := proc.stepWriter
	proc.mu.Unlock()
	if sw == nil {
		return
	}

	var msgsRaw json.RawMessage
	var msgCount int
	if promptResult != nil {
		msgCount = len(promptResult.Messages)
		var marshalErr error
		msgsRaw, marshalErr = json.Marshal(promptResult.Messages)
		if marshalErr != nil {
			log.Printf("[step_writer] messages marshal error pid=%d step=%d: %v", proc.PID, step, marshalErr)
			msgsRaw = nil
			msgCount = 0
		}
	}

	rec := types.StepRecord{
		Step:         step,
		Timestamp:    time.Since(proc.CreatedAt),
		Messages:     msgsRaw,
		MessageCount: msgCount,
		RawResponse:  rawResponse,
		Action:       actionType,
		Summary:      summary,
		ToolPath:     toolPath,
		ToolInput:    toolInput,
		ToolResult:   toolResult,
		ToolError:    toolError,
		ToolDuration: toolDuration,
	}
	if resp != nil {
		rec.TokenCount = resp.TokensUsed
		rec.ResponseTokens = resp.TokensUsed
	}

	if err := sw.WriteStep(rec); err != nil {
		log.Printf("[step_writer] write error pid=%d step=%d: %v", proc.PID, step, err)
	}
}

// writeDriverStepRecord writes a lightweight StepRecord for CLI driver events (tool_call).
// This makes driver tool calls visible in Dashboard Step Timeline.
func (k *KernelImpl) writeDriverStepRecord(proc *Process, step int, action, summary, toolPath string) {
	proc.mu.Lock()
	sw := proc.stepWriter
	proc.mu.Unlock()
	if sw == nil {
		return
	}

	rec := types.StepRecord{
		Step:      step,
		Timestamp: time.Since(proc.CreatedAt),
		Action:    action,
		Summary:   summary,
		ToolPath:  toolPath,
	}
	if err := sw.WriteStep(rec); err != nil {
		log.Printf("[step_writer] driver step write error pid=%d step=%d: %v", proc.PID, step, err)
	}
}

// ListProcs returns snapshots of all processes in the process table.
// Satisfies vfs.ProcessInfoProvider interface via duck typing.
func (k *KernelImpl) ListProcs() []vfs.ProcInfo {
	var infos []vfs.ProcInfo
	k.procTable.Range(func(_ types.PID, proc *Process) bool {
		proc.mu.Lock()
		infos = append(infos, vfs.ProcInfo{
			PID:            proc.PID,
			UUID:           proc.UUID,
			PPID:           proc.PPID,
			State:          proc.State,
			Intent:         proc.Intent,
			Skills:         append([]string(nil), proc.Skills...),
			TokensUsed:     proc.TokensUsed,
			ContextBudget:  proc.ContextBudget,
			CreatedAt:      proc.CreatedAt,
			DeadAt:         proc.DeadAt,
			CtxID:          proc.CtxID,
			Result:         proc.Result,
			AllowedDevices: append([]string(nil), proc.AllowedDevices...),
			Provider:       proc.Provider,
			Model:          proc.Model,
		})
		proc.mu.Unlock()
		return true
	})
	return infos
}

// ListAllProcs returns the union of active processes (from procTable) and
// historical processes (reaped and removed). Results are deduplicated by PID
// (active entries take precedence) and sorted by CreatedAt ascending.
func (k *KernelImpl) ListAllProcs() []vfs.ProcInfo {
	active := k.ListProcs()
	historical := k.procHistory.List()

	seen := make(map[types.PID]bool, len(active))
	result := make([]vfs.ProcInfo, 0, len(active)+len(historical))
	for _, p := range active {
		seen[p.PID] = true
		result = append(result, p)
	}
	for _, p := range historical {
		if !seen[p.PID] {
			result = append(result, p)
		}
	}

	slices.SortFunc(result, func(a, b vfs.ProcInfo) int {
		return a.CreatedAt.Compare(b.CreatedAt)
	})
	return result
}

// SetMountManager sets the MCP mount manager on the kernel.
// Pass nil to disable MCP support.
func (k *KernelImpl) SetMountManager(mgr MountManager) {
	k.mountMgr = mgr
}

// SetRecordManager sets the execution recording manager on the kernel.
// Pass nil to disable recording support.
func (k *KernelImpl) SetRecordManager(mgr *debug.RecordManager) {
	k.recordMgr = mgr
}

// SetSpanWriter sets the optional SpanWriter for persisting completed spans.
// Pass nil to disable span persistence.
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

// SetProviderResolver injects callbacks for dynamic LLM provider validation.
func (k *KernelImpl) SetProviderResolver(names func() []string, has func(name string) bool) {
	k.providerNames = names
	k.hasProvider = has
}

// SetDefaultProvider injects the default LLM provider name used when neither
// the caller nor the agent manifest specifies a provider.
func (k *KernelImpl) SetDefaultProvider(name string) {
	k.defaultProvider = name
}

// StartRecording starts execution recording for the given PID.
// Returns the recording ID or an error if the PID doesn't exist or is already being recorded.
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

// GetRecordManager returns the record manager (used by IPC server for record_list).
func (k *KernelImpl) GetRecordManager() *debug.RecordManager {
	return k.recordMgr
}

// Mount mounts an MCP server at the given path via the MountManager.
// The path must start with /mnt/mcp/.
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
// The path must start with /mnt/mcp/.
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

// checkBudgetWarning emits warning/critical log and event when context budget
// is nearing exhaustion. Extracted from reasonStep to enable integration testing.
func (k *KernelImpl) checkBudgetWarning(proc *Process, step, tokens, budget int) {
	if budget <= 0 || tokens >= budget {
		return
	}
	remainPct := float64(budget-tokens) / float64(budget) * 100
	if remainPct >= 20 {
		return
	}
	avgRate := float64(tokens) / float64(step)
	estRemain := 0
	if avgRate > 0 {
		estRemain = int(float64(budget-tokens) / avgRate)
	}
	level := "warning"
	if remainPct < 10 {
		level = "critical"
	}
	k.emitLog(proc, step, types.LogWarning,
		fmt.Sprintf("Budget %s: %d/%d (%.0f%% remaining, ~%d steps left)",
			level, tokens, budget, remainPct, estRemain), "")
	k.emitEvent(proc, "ReasonStep", map[string]any{
		"step":          step,
		"action":        "budget_warning",
		"tokens":        tokens,
		"budget":        budget,
		"remaining_pct": remainPct,
		"est_remaining": estRemain,
		"alert_level":   level,
	}, nil, nil, 0)
}
