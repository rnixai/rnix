package agents

import (
	"sort"

	"github.com/rnixai/rnix/skills"
	"github.com/rnixai/rnix/vfs"
)

// AgentModels defines the LLM provider and model preferences for an agent.
type AgentModels struct {
	Provider  string `yaml:"provider"`
	Preferred string `yaml:"preferred"`
	Fallback  string `yaml:"fallback"`
}

// AgentManifest represents the parsed contents of an agent's agent.yaml.
type AgentManifest struct {
	Name          string      `yaml:"name"`
	Description   string      `yaml:"description"`
	Models        AgentModels `yaml:"models"`
	ContextBudget int         `yaml:"context_budget"`
	Skills        []string    `yaml:"skills"`
	MCP           []string    `yaml:"mcp,omitempty"` // MCP server references
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

// SystemPrompt assembles the full system prompt: agent instructions + skill bodies.
func (a *AgentInfo) SystemPrompt() string {
	prompt := a.Instructions
	for _, skill := range a.Skills {
		if skill.Body != "" {
			prompt += "\n\n" + skill.Body
		}
	}
	return prompt
}
