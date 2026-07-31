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
	ReasoningEffort  string `yaml:"reasoning_effort"`  // agent-level effort default; passed through verbatim (no validation/case-mapping); empty = defer to driver snapshot
}

// AgentManifest represents the parsed contents of an agent's agent.yaml.
type AgentManifest struct {
	Name           string      `yaml:"name"`
	Description    string      `yaml:"description"`
	Models         AgentModels `yaml:"models"`
	ContextBudget  int         `yaml:"context_budget"`
	CtxSize        int         `yaml:"ctx_size,omitempty"`
	Skills         []string    `yaml:"skills"`
	Tools          []string    `yaml:"tools,omitempty"`           // agent-level tool declaration (Story 58.1); unioned with skill allowed-tools in AllowedTools()
	DeferredSkills []string    `yaml:"deferred_skills,omitempty"` // skill names loaded metadata-only (body loaded on discover_skill)
	MCP            []string    `yaml:"mcp,omitempty"`             // MCP server references
	MaxSteps       int         `yaml:"max_steps,omitempty"`       // max reasoning steps; 0 = use default
	Planning       *bool       `yaml:"planning,omitempty"`        // nil = not set (true), *true = enabled, *false = disabled
	MaxTokens      int64       `yaml:"max_tokens,omitempty"`      // per-process token budget; 0 = unlimited
	MaxCost        float64     `yaml:"max_cost,omitempty"`        // per-process cost budget (USD); 0 = unlimited
	StepTimeout    string      `yaml:"step_timeout,omitempty"`    // duration string e.g. "10m"; default "5m"; "0" = disabled
	// CompactTimeout bounds the compaction LLM call (Story 69.3 AC5). Duration
	// string e.g. "60s"; empty = default 30s.
	//
	// ⚠️ Semantics differ from StepTimeout on purpose: "0" here is NOT
	// "disabled". kernel's effectiveCompactTimeout() treats 0 as "fall back to
	// DefaultCompactTimeout (30s)". Making it symmetric with StepTimeout would
	// hand gocontext.WithTimeout(ctx, 0) to the compaction path, which expires
	// immediately — compaction would be permanently unavailable.
	//
	// This is an operator escape hatch for diagnosing a slow provider, not a
	// routine knob: raising it trades a fast failure for a slow one. The real
	// answer when the LLM is unavailable is the mechanical fallback.
	CompactTimeout string `yaml:"compact_timeout,omitempty"`
	// LoopThreshold / CoarseLoopThreshold configure loop detection (Story 70.1).
	//
	// ⚠️ These are STEP COUNTS (int), not duration strings — deliberately unlike
	// the StepTimeout / CompactTimeout fields above them. Do not "align the
	// shape" by making them strings.
	//
	// Semantics for both, and a THIRD convention distinct from either neighbour:
	//
	//	0 (omitted) → use the kernel default (30 fine / 60 coarse; 2N suspends)
	//	>0          → warn after N consecutive matching tool_call steps, suspend at 2N
	//	<0          → DISABLE that track entirely
	//
	// The negative-disables case is the operator escape hatch: it needs no
	// recompile, so a long-running orchestrator that trips a false positive in
	// production can be unblocked from its agent.yaml.
	//
	// Fine track matches on (actionType, toolPath, toolInput, result); coarse
	// ignores toolInput, catching "the LLM varies arguments but gets the same
	// result every time".
	LoopThreshold       int `yaml:"loop_threshold,omitempty"`
	CoarseLoopThreshold int `yaml:"coarse_loop_threshold,omitempty"`
	SLA            *AgentSLA   `yaml:"sla,omitempty"`             // SLA constraints (Story 21.2)
	Alternatives   []string    `yaml:"alternatives,omitempty"`    // alternative agent names for auto-selection (Story 21.3)
	Language       string      `yaml:"language,omitempty"`        // preferred response language (e.g. "Chinese", "English"); empty = no preference
	ProjectDoc     *bool       `yaml:"project_doc,omitempty"`     // nil = default (inject project-root AGENTS.md); *false = disable injection (Story 35.7)
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
	Manifest       AgentManifest
	Instructions   string
	Skills         []*skills.SkillInfo
	DeferredSkills []*skills.SkillInfo // metadata-only skills (body loaded on discover_skill)
	MCPConfigs     []vfs.MCPConfig     // resolved MCP configurations from global mcp.yaml
}

// AllowedTools aggregates AllowedTools from all referenced skills, unioned with
// the agent-level Manifest.Tools declaration (Story 58.1), deduplicated and sorted.
// Nil/empty Manifest.Tools contributes nothing (fail-closed: default ≠ all tools).
func (a *AgentInfo) AllowedTools() []string {
	toolSet := make(map[string]struct{})
	for _, skill := range a.Skills {
		for _, tool := range skill.Manifest.AllowedTools() {
			toolSet[tool] = struct{}{}
		}
	}
	for _, tool := range a.Manifest.Tools {
		toolSet[tool] = struct{}{}
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

// InstructionSections returns agent instructions and skill bodies as separate strings,
// suitable for registering as independent prompt sections.
func (a *AgentInfo) InstructionSections() (instructions string, skillBodies string) {
	instructions = a.Instructions

	var sb strings.Builder
	for _, skill := range a.Skills {
		if skill.Body != "" {
			if sb.Len() > 0 {
				sb.WriteString("\n\n")
			}
			if skill.Dir != "" {
				sb.WriteString("Base directory for this skill: " + skill.Dir + "\n\n")
			}
			sb.WriteString(skill.Body)
		}
	}
	// Synergy detection: append emergent instructions
	synergyInstructions := skills.DetectSynergies(a.Skills)
	if len(synergyInstructions) > 0 {
		if sb.Len() > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString("[Skill Synergy]\n\n")
		sb.WriteString(strings.Join(synergyInstructions, "\n"))
	}
	return instructions, sb.String()
}
