package agents

import (
	"sort"
	"strings"

	"github.com/rnixai/rnix/skills"
	"github.com/rnixai/rnix/vfs"
)

// AgentModels defines the LLM provider and model preferences for an agent.
type AgentModels struct {
	Provider         string `yaml:"provider"`
	Preferred        string `yaml:"preferred"`
	Fallback         string `yaml:"fallback"`
	FallbackProvider string `yaml:"fallback_provider"` // cross-provider fallback; empty = same provider (Story 23.5)
}

// AgentManifest represents the parsed contents of an agent's agent.yaml.
type AgentManifest struct {
	Name          string      `yaml:"name"`
	Description   string      `yaml:"description"`
	Models        AgentModels `yaml:"models"`
	ContextBudget int         `yaml:"context_budget"`
	Skills        []string    `yaml:"skills"`
	MCP           []string    `yaml:"mcp,omitempty"`       // MCP server references
	MaxSteps      int         `yaml:"max_steps,omitempty"` // max reasoning steps; 0 = use default
	Planning      *bool       `yaml:"planning,omitempty"`  // nil = not set (true), *true = enabled, *false = disabled
	MaxTokens     int64       `yaml:"max_tokens,omitempty"` // per-process token budget; 0 = unlimited
	MaxCost       float64     `yaml:"max_cost,omitempty"`   // per-process cost budget (USD); 0 = unlimited
	StepTimeout   string      `yaml:"step_timeout,omitempty"` // duration string e.g. "10m"; default "5m"; "0" = disabled
	SLA           *AgentSLA   `yaml:"sla,omitempty"`       // SLA constraints (Story 21.2)
	Alternatives  []string    `yaml:"alternatives,omitempty"` // alternative agent names for auto-selection (Story 21.3)
}

// AgentSLA defines SLA constraints in agent.yaml.
// Mirrors kernel.SLASpec but lives in agents package to avoid circular dependency.
type AgentSLA struct {
	MaxTokens     int    `yaml:"max_tokens,omitempty" json:"max_tokens,omitempty"`
	MaxDurationMs int64  `yaml:"max_duration_ms,omitempty" json:"max_duration_ms,omitempty"`
	OutputFormat  string `yaml:"output_format,omitempty" json:"output_format,omitempty"`
}

// AgentInfo contains the fully loaded agent definition.
type AgentInfo struct {
	Manifest     AgentManifest
	Instructions string
	Skills       []*skills.SkillInfo
	MCPConfigs   []vfs.MCPConfig // resolved MCP configurations from global mcp.yaml
}

// AllowedTools aggregates AllowedTools from all referenced skills, deduplicated and sorted.
func (a *AgentInfo) AllowedTools() []string {
	toolSet := make(map[string]struct{})
	for _, skill := range a.Skills {
		for _, tool := range skill.Manifest.AllowedTools() {
			toolSet[tool] = struct{}{}
		}
	}
	tools := make([]string, 0, len(toolSet))
	for tool := range toolSet {
		tools = append(tools, tool)
	}
	sort.Strings(tools)
	return tools
}

// SystemPrompt assembles the full system prompt: agent instructions + skill bodies + synergy instructions.
func (a *AgentInfo) SystemPrompt() string {
	var prompt strings.Builder
	prompt.WriteString(a.Instructions)
	for _, skill := range a.Skills {
		if skill.Body != "" {
			if skill.Dir != "" {
				prompt.WriteString("\n\nBase directory for this skill: " + skill.Dir + "\n\n")
			} else {
				prompt.WriteString("\n\n")
			}
			prompt.WriteString(skill.Body)
		}
	}
	// Synergy detection: append emergent instructions
	synergyInstructions := skills.DetectSynergies(a.Skills)
	if len(synergyInstructions) > 0 {
		prompt.WriteString("\n\n[Skill Synergy]\n\n")
		prompt.WriteString(strings.Join(synergyInstructions, "\n"))
	}
	return prompt.String()
}
