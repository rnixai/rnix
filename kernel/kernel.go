package kernel

import (
	"encoding/json"
	"errors"
	"fmt"
	gocontext "context"
	"path"
	"strings"
	"time"

	"github.com/gonewx/crux/agents"
	cruxctx "github.com/gonewx/crux/context"
	"github.com/gonewx/crux/internal/types"
	"github.com/gonewx/crux/internal/xsync"
	"github.com/gonewx/crux/vfs"
)

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

// KernelImpl is the core microkernel implementation.
type KernelImpl struct {
	procTable *xsync.SyncMap[types.PID, *Process]
	vfs       *vfs.VFS
	ctxMgr    *cruxctx.Manager
	callbacks KernelCallbacks
}

// NewKernel creates a new KernelImpl with the given VFS, context manager, and optional callbacks.
// Pass nil for cb to run in silent mode (no progress notifications).
func NewKernel(v *vfs.VFS, ctxMgr *cruxctx.Manager, cb KernelCallbacks) *KernelImpl {
	return &KernelImpl{
		procTable: xsync.NewSyncMap[types.PID, *Process](),
		vfs:       v,
		ctxMgr:    ctxMgr,
		callbacks: cb,
	}
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
	proc := NewProcess(0, intent, skillNames)

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
	cid, err := k.ctxMgr.CtxAlloc(DefaultCtxSize)
	if err != nil {
		return 0, NewSyscallError("Spawn", proc.PID, "", err, types.ErrInternal)
	}
	proc.CtxID = cid

	// Set system prompt if provided
	if opts.SystemPrompt != "" {
		if err := k.ctxMgr.SetSystemPrompt(cid, opts.SystemPrompt); err != nil {
			_ = k.ctxMgr.CtxFree(cid)
			return 0, NewSyscallError("Spawn", proc.PID, "", err, types.ErrInternal)
		}
	}

	// Append initial intent as user message
	if err := k.ctxMgr.AppendMessage(cid, cruxctx.RoleUser, intent); err != nil {
		_ = k.ctxMgr.CtxFree(cid)
		return 0, NewSyscallError("Spawn", proc.PID, "", err, types.ErrInternal)
	}

	// Open LLM device via VFS
	llmFD, err := k.vfs.Open(proc.PID, "/dev/llm/claude", vfs.O_RDWR)
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
		defer k.vfs.CloseAll(proc.PID)
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
func (k *KernelImpl) emitEvent(proc *Process, syscall string, args map[string]any, result any, err error, duration time.Duration) {
	if proc.DebugChan == nil {
		return
	}
	event := types.SyscallEvent{
		Timestamp: time.Since(proc.CreatedAt),
		PID:       proc.PID,
		Syscall:   syscall,
		Args:      args,
		Result:    result,
		Err:       err,
		Duration:  duration,
	}
	select {
	case proc.DebugChan <- event:
	default: // buffer full, drop event
	}
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
		promptResult, err := k.ctxMgr.BuildPrompt(proc.CtxID)
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
		if err := k.vfs.Write(proc.ctx, proc.PID, llmFD, reqJSON); err != nil {
			k.emitEvent(proc, "ReasonStep", map[string]any{
				"step":   step,
				"action": "error",
			}, nil, err, time.Since(stepStart))
			k.finishProcess(proc, ExitStatus{Code: 1, Reason: "llm write failed", Err: err})
			return
		}

		// Read response from LLM device
		respData, err := k.vfs.Read(proc.PID, llmFD, 1<<20) // 1MB max
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

		proc.TokensUsed += resp.TokensUsed

		// Parse action
		action := parseAction(&resp)

		switch action.Type {
		case ActionText:
			proc.Result = action.Content
			k.emitEvent(proc, "ReasonStep", map[string]any{
				"step":   step,
				"action": "text",
			}, action.Content, nil, time.Since(stepStart))
			k.finishProcess(proc, ExitStatus{Code: 0, Reason: "completed"})
			return

		case ActionToolCall:
			// Append LLM response as assistant message to maintain conversation history
			if err := k.ctxMgr.AppendMessage(proc.CtxID, cruxctx.RoleAssistant, resp.Content); err != nil {
				k.finishProcess(proc, ExitStatus{Code: 1, Reason: "append assistant message failed", Err: err})
				return
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
					if err := k.ctxMgr.AppendToolResult(proc.CtxID, action.ToolPath, permErr); err != nil {
						k.finishProcess(proc, ExitStatus{Code: 1, Reason: "append permission error failed", Err: err})
						return
					}
					k.emitEvent(proc, "ReasonStep", map[string]any{
						"step":   step,
						"action": "permission_denied",
						"tool":   action.ToolPath,
					}, nil, errors.New(permErr), time.Since(stepStart))
					continue
				}
			}

			// Open tool device
			toolFD, err := k.vfs.Open(proc.PID, action.ToolPath, vfs.O_RDWR)
			if err != nil {
				k.finishProcess(proc, ExitStatus{Code: 1, Reason: "tool open failed: " + action.ToolPath, Err: err})
				return
			}

			// Write tool data
			if err := k.vfs.Write(proc.ctx, proc.PID, toolFD, action.ToolData); err != nil {
				_ = k.vfs.Close(proc.PID, toolFD)
				k.finishProcess(proc, ExitStatus{Code: 1, Reason: "tool write failed", Err: err})
				return
			}

			// Read tool result
			toolResult, err := k.vfs.Read(proc.PID, toolFD, 1<<20)
			if err != nil {
				_ = k.vfs.Close(proc.PID, toolFD)
				k.finishProcess(proc, ExitStatus{Code: 1, Reason: "tool read failed", Err: err})
				return
			}

			_ = k.vfs.Close(proc.PID, toolFD)

			// Append tool result to context
			if err := k.ctxMgr.AppendToolResult(proc.CtxID, action.ToolPath, string(toolResult)); err != nil {
				k.finishProcess(proc, ExitStatus{Code: 1, Reason: "append tool result failed", Err: err})
				return
			}

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

// ListProcesses returns all processes currently in the process table.
func (k *KernelImpl) ListProcesses() []*Process {
	var procs []*Process
	k.procTable.Range(func(_ types.PID, p *Process) bool {
		procs = append(procs, p)
		return true
	})
	return procs
}
