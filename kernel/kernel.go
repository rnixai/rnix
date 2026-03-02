package kernel

import (
	"encoding/json"
	"errors"
	"fmt"
	gocontext "context"
	"path"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gonewx/crux/agents"
	cruxctx "github.com/gonewx/crux/context"
	"github.com/gonewx/crux/debug"
	"github.com/gonewx/crux/internal/types"
	"github.com/gonewx/crux/internal/xsync"
	"github.com/gonewx/crux/vfs"
)

// MountManager defines the interface for mounting/unmounting MCP servers.
type MountManager interface {
	Mount(path string, config vfs.MCPConfig) error
	Unmount(path string) error
	UnmountAll() error
}

// DefaultMaxSteps is the maximum number of reasoning steps before forced completion.
const DefaultMaxSteps = 10

// DefaultCtxSize is the default context size (message count) for new contexts.
const DefaultCtxSize = 64

// SpawnOpts configures optional parameters for Spawn.
type SpawnOpts struct {
	Model        string
	SystemPrompt string
	MaxTurns     int
	TimeoutMs    int64
	ParentPID    types.PID // parent process PID; 0 = top-level/CLI spawn
}

// ActionType classifies LLM response actions.
type ActionType string

const (
	ActionText     ActionType = "text"
	ActionToolCall ActionType = "tool_call"
	ActionSpawn    ActionType = "spawn"
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
	Messages     []cruxctx.Message `json:"messages,omitempty"`
}

// llmResponse is the JSON payload read from the LLM VFS device.
// Field names and json tags are compatible with drivers/llm.LLMResponse.
type llmResponse struct {
	Content    string `json:"content"`
	TokensUsed int    `json:"tokens_used"`
}

// KernelCallbacks allows the CLI layer to receive progress notifications
// from the kernel without introducing a reverse dependency on internal/ui.
type KernelCallbacks interface {
	OnSpawn(pid types.PID, intent string)
	OnStep(pid types.PID, step int, total int)
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
	ctxMgr    *cruxctx.Manager
	callbacks KernelCallbacks

	// Reaper infrastructure (Story 4.2)
	reapCh       chan types.PID  // PIDs pending auto-reap
	stopCh       chan struct{}   // signals reaper goroutine to stop
	reaperWg     sync.WaitGroup // waits for reaper goroutine exit
	shutdownOnce sync.Once      // ensures Shutdown executes at most once

	// IPC messaging (Story 6.1)
	msgQueues *xsync.SyncMap[types.PID, *MessageQueue]
	msgSeq    atomic.Uint64

	// Process groups (Story 6.3)
	procGroups *xsync.SyncMap[types.PGID, *ProcGroup]

	// MCP mount manager (Story 9.1)
	mountMgr MountManager
}

// NewKernel creates a new KernelImpl with the given VFS, context manager, and optional callbacks.
// Pass nil for cb to run in silent mode (no progress notifications).
func NewKernel(v *vfs.VFS, ctxMgr *cruxctx.Manager, cb KernelCallbacks) *KernelImpl {
	k := &KernelImpl{
		procTable: xsync.NewSyncMap[types.PID, *Process](),
		vfs:       v,
		ctxMgr:    ctxMgr,
		callbacks: cb,
		reapCh:    make(chan types.PID, 64),
		stopCh:    make(chan struct{}),
		msgQueues:  xsync.NewSyncMap[types.PID, *MessageQueue](),
		procGroups: xsync.NewSyncMap[types.PGID, *ProcGroup](),
	}
	k.startReaper()
	return k
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

	// Maintain parent-child tracking
	if opts.ParentPID > 0 {
		parent, ok := k.GetProcess(opts.ParentPID)
		if !ok {
			return 0, NewSyscallError("Spawn", opts.ParentPID, "", fmt.Errorf("parent process %d not found", opts.ParentPID), types.ErrNotFound)
		}
		parent.AddChild(proc.PID)
	}

	// Load Agent information if specified
	if agent != nil {
		// System prompt = Agent instructions + Skill bodies
		agentPrompt := agent.SystemPrompt()
		if opts.SystemPrompt == "" {
			opts.SystemPrompt = agentPrompt
		} else {
			opts.SystemPrompt = opts.SystemPrompt + "\n\n" + agentPrompt
		}

		// Aggregate AllowedTools from all Skills
		proc.AllowedDevices = agent.AllowedTools()

		// Model selection priority: CLI --model > Agent manifest > driver default
		if opts.Model == "" && agent.Manifest.Models.Preferred != "" {
			opts.Model = agent.Manifest.Models.Preferred
		}
	}

	// Allocate context
	ctxAllocStart := time.Now()
	cid, err := k.ctxMgr.CtxAlloc(DefaultCtxSize)
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
	if err := k.ctxMgr.AppendMessage(cid, cruxctx.RoleUser, intent); err != nil {
		k.emitEvent(proc, "CtxWrite", map[string]any{
			"cid":  cid,
			"op":   "AppendMessage",
			"role": string(cruxctx.RoleUser),
		}, nil, err, time.Since(appendStart))
		_ = k.ctxMgr.CtxFree(cid)
		return 0, NewSyscallError("Spawn", proc.PID, "", err, types.ErrInternal)
	}
	k.emitEvent(proc, "CtxWrite", map[string]any{
		"cid":  cid,
		"op":   "AppendMessage",
		"role": string(cruxctx.RoleUser),
	}, nil, nil, time.Since(appendStart))

	// Open LLM device via VFS
	openStart := time.Now()
	llmFD, err := k.vfs.Open(proc.PID, "/dev/llm/claude", vfs.O_RDWR)
	k.emitEvent(proc, "Open", map[string]any{
		"path":  "/dev/llm/claude",
		"flags": vfs.O_RDWR,
	}, llmFD, err, time.Since(openStart))
	if err != nil {
		_ = k.ctxMgr.CtxFree(cid)
		return 0, NewSyscallError("Spawn", proc.PID, "/dev/llm/claude", err, types.ErrDriver)
	}
	proc.FDTable[llmFD] = nil // VFS manages actual file internally; tracks FD existence for process inspection

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
	}
	k.emitEvent(proc, "Spawn", spawnArgs, proc.PID, nil, time.Since(start))

	// Launch reasoning goroutine
	// Note: CtxFree deferred to Wait/Reap (Story 4.1) per resource release order
	proc.wg.Add(1)
	go func() {
		defer proc.wg.Done()
		defer func() { _ = k.vfs.CloseAll(proc.PID) }()
		_ = proc.Start() // Created → Running
		k.reasonStep(proc, llmFD, opts)
	}()

	// Notify callback after process is registered and goroutine launched
	if k.callbacks != nil {
		k.callbacks.OnSpawn(proc.PID, intent)
	}

	return proc.PID, nil
}

// emitEvent sends a SyscallEvent to the process DebugChan (non-blocking).
// This is a convenience wrapper that auto-fills process info (PID, Timestamp)
// and delegates to debug.EmitEvent for the actual non-blocking write.
// Holds proc.mu only during channel access to prevent races with Wait's close(DebugChan).
func (k *KernelImpl) emitEvent(proc *Process, syscall string, args map[string]any, result any, err error, duration time.Duration) {
	event := debug.NewEvent(proc.PID, proc.CreatedAt, syscall, args)
	debug.CompleteEvent(&event, result, err, duration)

	proc.mu.Lock()
	ch := proc.DebugChan
	if ch != nil {
		debug.EmitEvent(ch, event)
	}
	proc.mu.Unlock()
}

// finishProcess terminates the process and writes the exit status to the Done channel.
func (k *KernelImpl) finishProcess(proc *Process, exit ExitStatus) {
	_ = proc.Terminate(exit)

	// Notify callbacks
	if k.callbacks != nil {
		if exit.Err != nil {
			k.callbacks.OnError(proc.PID, exit.Err)
		}
		k.callbacks.OnComplete(proc.PID, proc.Result, exit)
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

// reasonStep executes the reasoning loop for a process.
func (k *KernelImpl) reasonStep(proc *Process, llmFD types.FD, opts SpawnOpts) {
	maxSteps := DefaultMaxSteps
	if opts.MaxTurns > 0 {
		maxSteps = opts.MaxTurns
	}

	defer func() {
		// Ensure process always transitions to Zombie
		if proc.GetState() == types.StateRunning {
			k.finishProcess(proc, ExitStatus{Code: 1, Reason: "unexpected exit"})
		}
	}()

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
		req := llmRequest{
			Intent:       proc.Intent,
			SystemPrompt: promptResult.SystemPrompt,
			Model:        opts.Model,
			MaxTurns:     0,
			TimeoutMs:    opts.TimeoutMs,
			Messages:     promptResult.Messages,
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
		if err := k.vfs.Write(proc.ctx, proc.PID, llmFD, reqJSON); err != nil {
			k.emitEvent(proc, "Write", map[string]any{
				"fd":    llmFD,
				"size":  len(reqJSON),
				"model": opts.Model,
			}, nil, err, time.Since(writeStart))
			k.emitEvent(proc, "ReasonStep", map[string]any{
				"step":   step,
				"action": "error",
			}, nil, err, time.Since(stepStart))
			k.finishProcess(proc, ExitStatus{Code: 1, Reason: "llm write failed", Err: err})
			return
		}
		k.emitEvent(proc, "Write", map[string]any{
			"fd":    llmFD,
			"size":  len(reqJSON),
			"model": opts.Model,
		}, nil, nil, time.Since(writeStart))

		// Read response from LLM device
		readStart := time.Now()
		respData, err := k.vfs.Read(proc.PID, llmFD, 1<<20) // 1MB max
		k.emitEvent(proc, "Read", map[string]any{
			"fd":     llmFD,
			"length": 1 << 20,
		}, len(respData), err, time.Since(readStart))
		if err != nil {
			k.emitEvent(proc, "ReasonStep", map[string]any{
				"step":   step,
				"action": "error",
			}, nil, err, time.Since(stepStart))
			k.finishProcess(proc, ExitStatus{Code: 1, Reason: "llm read failed", Err: err})
			return
		}

		// Parse LLM response
		var resp llmResponse
		if err := json.Unmarshal(respData, &resp); err != nil {
			k.emitEvent(proc, "ReasonStep", map[string]any{
				"step":   step,
				"action": "error",
			}, nil, err, time.Since(stepStart))
			k.finishProcess(proc, ExitStatus{Code: 1, Reason: "unmarshal response failed", Err: err})
			return
		}

		proc.mu.Lock()
		proc.TokensUsed += resp.TokensUsed
		proc.mu.Unlock()

		// Parse action
		action := parseAction(&resp)

		switch action.Type {
		case ActionText:
			proc.mu.Lock()
			proc.Result = action.Content
			proc.mu.Unlock()
			k.emitEvent(proc, "ReasonStep", map[string]any{
				"step":   step,
				"action": "text",
			}, action.Content, nil, time.Since(stepStart))
			k.finishProcess(proc, ExitStatus{Code: 0, Reason: "completed"})
			return

		case ActionToolCall:
			// Append LLM response as assistant message to maintain conversation history
			appendAssistantStart := time.Now()
			if err := k.ctxMgr.AppendMessage(proc.CtxID, cruxctx.RoleAssistant, resp.Content); err != nil {
				k.emitEvent(proc, "CtxWrite", map[string]any{
					"cid":  proc.CtxID,
					"op":   "AppendMessage",
					"role": string(cruxctx.RoleAssistant),
				}, nil, err, time.Since(appendAssistantStart))
				k.finishProcess(proc, ExitStatus{Code: 1, Reason: "append assistant message failed", Err: err})
				return
			}
			k.emitEvent(proc, "CtxWrite", map[string]any{
				"cid":  proc.CtxID,
				"op":   "AppendMessage",
				"role": string(cruxctx.RoleAssistant),
			}, nil, nil, time.Since(appendAssistantStart))

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

			// Open tool device
			toolOpenStart := time.Now()
			toolFD, err := k.vfs.Open(proc.PID, action.ToolPath, vfs.O_RDWR)
			k.emitEvent(proc, "Open", map[string]any{
				"path":  action.ToolPath,
				"flags": vfs.O_RDWR,
			}, toolFD, err, time.Since(toolOpenStart))
			if err != nil {
				k.finishProcess(proc, ExitStatus{Code: 1, Reason: "tool open failed: " + action.ToolPath, Err: err})
				return
			}

			// Write tool data
			toolWriteStart := time.Now()
			if err := k.vfs.Write(proc.ctx, proc.PID, toolFD, action.ToolData); err != nil {
				k.emitEvent(proc, "Write", map[string]any{
					"fd":   toolFD,
					"size": len(action.ToolData),
				}, nil, err, time.Since(toolWriteStart))
				_ = k.vfs.Close(proc.PID, toolFD)
				k.finishProcess(proc, ExitStatus{Code: 1, Reason: "tool write failed", Err: err})
				return
			}
			k.emitEvent(proc, "Write", map[string]any{
				"fd":   toolFD,
				"size": len(action.ToolData),
			}, nil, nil, time.Since(toolWriteStart))

			// Read tool result
			toolReadStart := time.Now()
			toolResult, err := k.vfs.Read(proc.PID, toolFD, 1<<20)
			k.emitEvent(proc, "Read", map[string]any{
				"fd":     toolFD,
				"length": 1 << 20,
			}, len(toolResult), err, time.Since(toolReadStart))
			if err != nil {
				_ = k.vfs.Close(proc.PID, toolFD)
				k.finishProcess(proc, ExitStatus{Code: 1, Reason: "tool read failed", Err: err})
				return
			}

			// Close tool device
			closeStart := time.Now()
			closeErr := k.vfs.Close(proc.PID, toolFD)
			k.emitEvent(proc, "Close", map[string]any{
				"fd": toolFD,
			}, nil, closeErr, time.Since(closeStart))

			// Append tool result to context
			appendToolStart := time.Now()
			if err := k.ctxMgr.AppendToolResult(proc.CtxID, action.ToolPath, string(toolResult)); err != nil {
				k.emitEvent(proc, "CtxWrite", map[string]any{
					"cid": proc.CtxID,
					"op":  "AppendToolResult",
					"tool": action.ToolPath,
				}, nil, err, time.Since(appendToolStart))
				k.finishProcess(proc, ExitStatus{Code: 1, Reason: "append tool result failed", Err: err})
				return
			}
			k.emitEvent(proc, "CtxWrite", map[string]any{
				"cid": proc.CtxID,
				"op":  "AppendToolResult",
				"tool": action.ToolPath,
			}, nil, nil, time.Since(appendToolStart))

			k.emitEvent(proc, "ReasonStep", map[string]any{
				"step":   step,
				"action": "tool_call",
				"tool":   action.ToolPath,
			}, string(toolResult), nil, time.Since(stepStart))
			continue
		}
	}

	// Max steps exceeded — incomplete reasoning
	k.finishProcess(proc, ExitStatus{Code: 1, Reason: "max steps exceeded"})
}

// parseAction determines the action type from an LLM response.
func parseAction(resp *llmResponse) ReasonAction {
	// Try structured JSON action
	var structured struct {
		Action  string         `json:"action"`
		Content string         `json:"content,omitempty"`
		Tool    string         `json:"tool,omitempty"`
		Data    map[string]any `json:"data,omitempty"`
	}
	if err := json.Unmarshal([]byte(resp.Content), &structured); err == nil {
		if structured.Action == "tool_call" && structured.Tool != "" {
			toolData, _ := json.Marshal(structured.Data)
			return ReasonAction{
				Type:     ActionToolCall,
				ToolPath: structured.Tool,
				ToolData: toolData,
			}
		}
	}

	// Default: plain text output
	return ReasonAction{Type: ActionText, Content: resp.Content}
}

// AddProcess registers a process in the kernel's process table.
func (k *KernelImpl) AddProcess(p *Process) {
	k.procTable.Store(p.PID, p)
}

// GetProcess retrieves a process by PID.
func (k *KernelImpl) GetProcess(pid types.PID) (*Process, bool) {
	return k.procTable.Load(pid)
}

// RemoveProcess removes a process from the process table.
func (k *KernelImpl) RemoveProcess(pid types.PID) {
	k.procTable.Delete(pid)
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
		PPID:           proc.PPID,
		State:          proc.State,
		Intent:         proc.Intent,
		Skills:         append([]string(nil), proc.Skills...),
		TokensUsed:     proc.TokensUsed,
		CreatedAt:      proc.CreatedAt,
		CtxID:          proc.CtxID,
		Result:         proc.Result,
		AllowedDevices: append([]string(nil), proc.AllowedDevices...),
	}
	proc.mu.Unlock()
	return info, nil
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

// ListProcs returns snapshots of all processes in the process table.
// Satisfies vfs.ProcessInfoProvider interface via duck typing.
func (k *KernelImpl) ListProcs() []vfs.ProcInfo {
	var infos []vfs.ProcInfo
	k.procTable.Range(func(_ types.PID, proc *Process) bool {
		proc.mu.Lock()
		infos = append(infos, vfs.ProcInfo{
			PID:            proc.PID,
			PPID:           proc.PPID,
			State:          proc.State,
			Intent:         proc.Intent,
			Skills:         append([]string(nil), proc.Skills...),
			TokensUsed:     proc.TokensUsed,
			CreatedAt:      proc.CreatedAt,
			CtxID:          proc.CtxID,
			Result:         proc.Result,
			AllowedDevices: append([]string(nil), proc.AllowedDevices...),
		})
		proc.mu.Unlock()
		return true
	})
	return infos
}

// SetMountManager sets the MCP mount manager on the kernel.
// Pass nil to disable MCP support.
func (k *KernelImpl) SetMountManager(mgr MountManager) {
	k.mountMgr = mgr
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
