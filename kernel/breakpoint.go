package kernel

import (
	"maps"
	"regexp"
	"slices"
	"strings"
	"sync/atomic"

	"github.com/rnixai/rnix/debug"
)

// BreakpointType enumerates the four kinds of gdb breakpoints.
type BreakpointType int

const (
	BPSyscall   BreakpointType = 1
	BPReasoning BreakpointType = 2
	BPQuality   BreakpointType = 3
	BPBudget    BreakpointType = 4
)

// QualityMode distinguishes pattern-match vs eval-based quality breakpoints.
type QualityMode int

const (
	QualityModePattern QualityMode = 1
	QualityModeEval    QualityMode = 2
)

// Breakpoint represents a single gdb breakpoint registered on a process.
type Breakpoint struct {
	ID        int
	Type      BreakpointType
	Condition BreakpointCondition
	Enabled   bool
	HitCount  int
}

// BreakpointCondition is the interface for type-specific matching logic.
type BreakpointCondition interface {
	Match(ctx BreakpointContext) bool
}

// BreakpointContext carries the runtime data needed to evaluate breakpoint conditions.
type BreakpointContext struct {
	BPType      BreakpointType
	SyscallName string
	SyscallArgs map[string]any
	LLMResponse string
	TokensUsed  int
	StepNumber  int
}

// --- Concrete condition types ---

// SyscallCondition matches when the syscall name equals Name.
type SyscallCondition struct {
	Name string
}

func (c *SyscallCondition) Match(ctx BreakpointContext) bool {
	return ctx.SyscallName == c.Name
}

// ReasoningCondition always matches (triggers on every reasoning step).
type ReasoningCondition struct{}

func (c *ReasoningCondition) Match(_ BreakpointContext) bool {
	return true
}

// QualityCondition matches based on LLM response content.
type QualityCondition struct {
	Mode     QualityMode
	Pattern  *regexp.Regexp // used when Mode == QualityModePattern
	EvalExpr string         // used when Mode == QualityModeEval
}

func (c *QualityCondition) Match(ctx BreakpointContext) bool {
	switch c.Mode {
	case QualityModePattern:
		if c.Pattern == nil {
			return false
		}
		return c.Pattern.MatchString(ctx.LLMResponse)
	case QualityModeEval:
		// MVP: triggers when content does NOT contain the eval expression.
		// This detects "quality not met" — if the expression describes what
		// should be present, absence triggers the breakpoint.
		return !strings.Contains(ctx.LLMResponse, c.EvalExpr)
	}
	return false
}

// BudgetCondition matches when token usage reaches or exceeds Threshold.
// Tracks whether it has already fired to prevent infinite re-triggering
// (tokens only increase, so >= would match on every subsequent step).
type BudgetCondition struct {
	Threshold int
	fired     bool
}

func (c *BudgetCondition) Match(ctx BreakpointContext) bool {
	if c.fired {
		return false
	}
	if ctx.TokensUsed >= c.Threshold {
		c.fired = true
		return true
	}
	return false
}

// --- Step mode types ---

// StepMode controls single-step execution for gdb debugging.
type StepMode int

const (
	StepNone      StepMode = 0 // no step mode active (zero value)
	StepSyscall   StepMode = 1 // step to next syscall
	StepReasoning StepMode = 2 // step to next reasoning iteration
)

// SetStepMode sets the single-step execution mode. Thread-safe.
func (p *Process) SetStepMode(mode StepMode) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.gdbStepMode = mode
}

// GetStepMode returns the current single-step mode. Thread-safe.
func (p *Process) GetStepMode() StepMode {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.gdbStepMode
}

// ClearStepMode resets the step mode to StepNone. Thread-safe.
func (p *Process) ClearStepMode() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.gdbStepMode = StepNone
}

// --- Process breakpoint methods ---

// bpIDCounter is the global breakpoint ID allocator (IDs are unique across all processes).
var bpIDCounter atomic.Int64

// AddBreakpoint registers a breakpoint on the process and returns its assigned ID.
func (p *Process) AddBreakpoint(bp *Breakpoint) int {
	id := int(bpIDCounter.Add(1))
	bp.ID = id

	p.mu.Lock()
	p.breakpoints = append(p.breakpoints, bp)
	p.mu.Unlock()

	return id
}

// RemoveBreakpoint removes the breakpoint with the given ID. Returns true if found.
func (p *Process) RemoveBreakpoint(id int) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	for i, bp := range p.breakpoints {
		if bp.ID == id {
			p.breakpoints = append(p.breakpoints[:i], p.breakpoints[i+1:]...)
			return true
		}
	}
	return false
}

// ListBreakpoints returns a copy of all registered breakpoints.
func (p *Process) ListBreakpoints() []*Breakpoint {
	p.mu.Lock()
	defer p.mu.Unlock()

	result := make([]*Breakpoint, len(p.breakpoints))
	copy(result, p.breakpoints)
	return result
}

// CheckBreakpoint evaluates all enabled breakpoints of the matching type.
// Returns the first matching breakpoint (with HitCount incremented), or nil.
func (p *Process) CheckBreakpoint(ctx BreakpointContext) *Breakpoint {
	p.mu.Lock()
	defer p.mu.Unlock()

	if len(p.breakpoints) == 0 {
		return nil
	}

	for _, bp := range p.breakpoints {
		if !bp.Enabled {
			continue
		}
		if bp.Type != ctx.BPType {
			continue
		}
		if bp.Condition != nil && bp.Condition.Match(ctx) {
			bp.HitCount++
			return bp
		}
	}
	return nil
}

// --- GDB pause/resume methods ---

// GdbPause blocks the calling goroutine until GdbResume is called.
// Sends a GdbPause event to DebugChan before blocking.
// extraArgs are merged into the event args (e.g., syscall_name, step_number).
func (p *Process) GdbPause(reason string, hitBP *Breakpoint, extraArgs ...map[string]any) {
	p.mu.Lock()
	ch := make(chan struct{})
	p.gdbPauseCh = ch

	// Emit GdbPause event to DebugChan
	args := map[string]any{
		"reason": reason,
	}
	if hitBP != nil {
		args["bp_id"] = hitBP.ID
		args["bp_type"] = hitBP.Type
	}
	for _, ea := range extraArgs {
		maps.Copy(args, ea)
	}
	event := debug.NewEvent(p.PID, p.CreatedAt, "GdbPause", args)
	if p.DebugChan != nil {
		debug.EmitEvent(p.DebugChan, event)
	}
	p.mu.Unlock()

	// Block until resumed
	<-ch
}

// GdbResume unblocks a GdbPause. Idempotent (no-op if not paused).
func (p *Process) GdbResume() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.gdbPauseCh != nil {
		close(p.gdbPauseCh)
		p.gdbPauseCh = nil
	}
}

// IsGdbPaused reports whether the process is currently paused by gdb.
func (p *Process) IsGdbPaused() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.gdbPauseCh != nil
}

// GdbPauseCh returns the gdb pause channel, or nil if not paused.
func (p *Process) GdbPauseCh() <-chan struct{} {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.gdbPauseCh
}

// --- GDB runtime parameter methods (all thread-safe via mu) ---

// SetGdbModelOverride sets the model override for LLM requests. Thread-safe.
// An empty string clears the override.
func (p *Process) SetGdbModelOverride(model string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.gdbModelOverride = model
}

// GetGdbModelOverride returns the current model override. Thread-safe.
// Returns empty string if no override is set.
func (p *Process) GetGdbModelOverride() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.gdbModelOverride
}

// SetGdbEnv sets an environment variable in the gdb env vars map. Thread-safe.
func (p *Process) SetGdbEnv(key, value string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.gdbEnvVars == nil {
		p.gdbEnvVars = make(map[string]string)
	}
	p.gdbEnvVars[key] = value
}

// GetGdbEnvVars returns a copy of the gdb environment variables. Thread-safe.
func (p *Process) GetGdbEnvVars() map[string]string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.gdbEnvVars == nil {
		return map[string]string{}
	}
	result := make(map[string]string, len(p.gdbEnvVars))
	maps.Copy(result, p.gdbEnvVars)
	return result
}

// AddGdbSkill adds a skill name to the gdb extra skills list. Idempotent. Thread-safe.
func (p *Process) AddGdbSkill(name string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if slices.Contains(p.gdbExtraSkills, name) {
		return
	}
	p.gdbExtraSkills = append(p.gdbExtraSkills, name)
}

// GetGdbExtraSkills returns a copy of the gdb extra skills list. Thread-safe.
func (p *Process) GetGdbExtraSkills() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.gdbExtraSkills == nil {
		return []string{}
	}
	result := make([]string, len(p.gdbExtraSkills))
	copy(result, p.gdbExtraSkills)
	return result
}
