package compose

import (
	"time"

	"github.com/rnixai/rnix/agents"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/kernel"
)

// ComposeSpec is the top-level structure of .rnix/compose.yaml.
type ComposeSpec struct {
	Version     string                `yaml:"version"`
	Intent      string                `yaml:"intent"`
	Model       string                `yaml:"model,omitempty"`
	Provider    string                `yaml:"provider,omitempty"`
	TokenBudget int                   `yaml:"token_budget,omitempty"`
	Agents      map[string]*AgentSpec `yaml:"agents"`
}

// AgentSpec defines a single agent in the compose workflow.
type AgentSpec struct {
	Intent        string            `yaml:"intent"`
	Agent         string            `yaml:"agent,omitempty"`
	Model         string            `yaml:"model,omitempty"`
	Provider      string            `yaml:"provider,omitempty"`
	Skills        []string          `yaml:"skills,omitempty"`
	Priority      string            `yaml:"priority,omitempty"`
	MaxTokens     int64             `yaml:"max_tokens,omitempty"`
	TimeoutMs     int64             `yaml:"timeout_ms,omitempty"`
	DependsOn     map[string]string `yaml:"depends_on,omitempty"`
	SLA           *kernel.SLASpec   `yaml:"sla,omitempty"`
	Candidates    []string          `yaml:"candidates,omitempty"` // candidate agents for auto-selection (Story 21.3)
}

// DAG represents the directed acyclic graph of agent dependencies.
type DAG struct {
	Nodes map[string]*DAGNode
}

// DAGNode represents a single node in the DAG.
type DAGNode struct {
	Name       string
	Spec       *AgentSpec
	DependsOn  []string // upstream dependency agent names
	DependedBy []string // downstream agents that depend on this node
}

// ComposeSpawnOpts contains spawn options for the compose engine.
type ComposeSpawnOpts struct {
	Model        string
	Provider     string
	SystemPrompt string
	ParentPID    types.PID
	MaxTokens    int64
	TimeoutMs    int64
	TraceID       types.TraceID
	ParentSpanID  types.SpanID
	ComposeNode   string   // compose node name
	ComposeDeps   []string // upstream dependency node names
}

// ComposeExitStatus records a process exit status for compose.
type ComposeExitStatus struct {
	Code   int
	Reason string
	Err    error
}

// KernelSpawner defines the kernel operations needed by the compose engine.
type KernelSpawner interface {
	Spawn(intent string, agent *agents.AgentInfo, opts ComposeSpawnOpts) (types.PID, error)
	Wait(pid types.PID) (ComposeExitStatus, error)
	GetProcessResult(pid types.PID) (string, bool)
	// GetSpanID returns the SpanID for a completed process (for trace parent-child, Story 15.1).
	// Returns false if the process has no SpanID or is unknown.
	GetSpanID(pid types.PID) (types.SpanID, bool)
	// GetTokensUsed returns the token consumption for a completed process (Story 21.1).
	// Returns false if the process is unknown or has no token data.
	GetTokensUsed(pid types.PID) (int, bool)
}

// AgentLoaderFunc loads an agent definition by name.
type AgentLoaderFunc func(name string) (*agents.AgentInfo, error)

// ScheduleResult records the execution result of a single agent.
type ScheduleResult struct {
	Name       string
	PID        types.PID
	ExitCode   int
	Output     string
	Err        error
	Duration   time.Duration
	TokensUsed int
	SLAResult  *kernel.SLAResult
}

// HistoricalNodeResult captures the persisted result of a previously executed
// compose node, used by Engine.ExecuteFromNode to seed upstream context when
// resuming a compose DAG from a failed node (Story 42.4).
type HistoricalNodeResult struct {
	PID      types.PID
	Output   string
	Tokens   int
	SpanID   types.SpanID
	ExitCode int
}

// HistoricalSeeder is an optional capability of a KernelSpawner implementation:
// engines may type-assert to inject historical (persisted) node results into the
// spawner's internal result/token/spanID caches before scheduling downstream
// nodes (Story 42.4). The compose package's primary KernelSpawner interface
// stays unchanged; concrete spawners (e.g. ipcKernelSpawner) implement this
// extension so engine_test.go's mockKernelSpawner keeps working without
// modification.
type HistoricalSeeder interface {
	SeedHistorical(pid types.PID, result string, tokens int, spanID types.SpanID)
}

// Priority type aliases for compose-level access to kernel.Priority.
type Priority = kernel.Priority

const (
	PriorityHigh   = kernel.PriorityHigh
	PriorityNormal = kernel.PriorityNormal
	PriorityLow    = kernel.PriorityLow
)

// ParsePriority converts a priority string to a kernel.Priority value.
func ParsePriority(s string) kernel.Priority {
	return kernel.ParsePriority(s)
}
