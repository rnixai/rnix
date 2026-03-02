package compose

import (
	"time"

	"github.com/gonewx/crux/agents"
	"github.com/gonewx/crux/internal/types"
)

// ComposeSpec is the top-level structure of crux-compose.yaml.
type ComposeSpec struct {
	Version string                `yaml:"version"`
	Intent  string                `yaml:"intent"`
	Model   string                `yaml:"model,omitempty"`
	Agents  map[string]*AgentSpec `yaml:"agents"`
}

// AgentSpec defines a single agent in the compose workflow.
type AgentSpec struct {
	Intent        string            `yaml:"intent"`
	Agent         string            `yaml:"agent,omitempty"`
	Model         string            `yaml:"model,omitempty"`
	Skills        []string          `yaml:"skills,omitempty"`
	ContextBudget int               `yaml:"context_budget,omitempty"`
	DependsOn     map[string]string `yaml:"depends_on,omitempty"`
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
	Model         string
	SystemPrompt  string
	ParentPID     types.PID
	ContextBudget int
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
}

// AgentLoaderFunc loads an agent definition by name.
type AgentLoaderFunc func(name string) (*agents.AgentInfo, error)

// ScheduleResult records the execution result of a single agent.
type ScheduleResult struct {
	Name     string
	PID      types.PID
	ExitCode int
	Output   string
	Err      error
	Duration time.Duration
}
