package kernel

import (
	"fmt"
	"maps"
	"strings"

	rnixctx "github.com/rnixai/rnix/context"
	skillpkg "github.com/rnixai/rnix/skills"
	"github.com/rnixai/rnix/vfs"
)

// registerSections creates a SectionRegistry with all standard sections for an agent-based spawn.
// Static sections use embedded template content; dynamic sections capture proc and kernel pointers.
// Registration order matches AC1 spec: intro → system_rules → doing_tasks → actions →
// using_devices → tone_style → output_efficiency → agent_instructions → env_info →
// loaded_skills → action_protocol.
func registerSections(proc *Process, k *KernelImpl, agentInstructions string) *rnixctx.SectionRegistry {
	reg := rnixctx.NewSectionRegistry()

	// --- Static cached sections (from embedded templates) ---
	reg.Register("intro", func() string { return rnixctx.LoadSection("intro") }, true)
	reg.Register("system_rules", func() string { return rnixctx.LoadSection("system_rules") }, true)
	reg.Register("doing_tasks", func() string { return rnixctx.LoadSection("doing_tasks") }, true)
	reg.Register("actions", func() string { return rnixctx.LoadSection("actions") }, true)
	reg.Register("using_devices", func() string { return rnixctx.LoadSection("using_devices") }, true)
	reg.Register("tone_style", func() string { return rnixctx.LoadSection("tone_style") }, true)
	reg.Register("output_efficiency", func() string { return rnixctx.LoadSection("output_efficiency") }, true)

	// --- Agent instructions section (cached) ---
	reg.Register("agent_instructions", func() string { return agentInstructions }, true)

	// --- Dynamic sections (recomputed on each Build) ---

	// language: language preference (CC-aligned)
	reg.Register("language", func() string {
		lang := proc.Language
		if lang == "" {
			return ""
		}
		return fmt.Sprintf("# Language\n\nAlways respond in %s. Use %s for all explanations, comments, and communications with the user. Technical terms and code identifiers should remain in their original form.", lang, lang)
	}, true)

	// scratchpad: per-process scratchpad directory instructions (CC-aligned)
	reg.Register("scratchpad", func() string {
		dir := proc.GetScratchDir()
		if dir == "" {
			return ""
		}
		return fmt.Sprintf(`# Scratchpad Directory

IMPORTANT: Always use this scratchpad directory for temporary files instead of /tmp or other system temp directories:
%s

Use this directory for ALL temporary file needs:
- Storing intermediate results or data during multi-step tasks
- Writing temporary scripts or configuration files
- Saving outputs that don't belong in the user's project
- Creating working files during analysis or processing
- Any file that would otherwise go to /tmp

Only use /tmp if the user explicitly requests it.

The scratchpad directory is process-specific, isolated from the user's project, and can be used freely without permission prompts.`, dir)
	}, false)

	// frc: Function Result Clearing guidance (CC-aligned)
	reg.Register("frc", func() string {
		return `# Function Result Clearing

Old device results may be automatically cleared from context to free up space. When working with device results, write down any important information you might need later in your response, as the original result may be cleared later.`
	}, true)

	// spawn_guidance: guidance on when/how to spawn child processes
	reg.Register("spawn_guidance", func() string {
		if !proc.PlanningEnabled {
			return ""
		}
		return `# Spawning Sub-Processes

You can spawn child processes for parallel or delegated work via the spawn meta-action. Guidelines:

When to spawn:
- Independent sub-tasks that can run in parallel (e.g., researching multiple modules simultaneously)
- Large delegated tasks that benefit from a separate context window
- Work requiring a different agent specialization or skill set

When NOT to spawn:
- Simple lookups or single-step operations — do them directly
- Tasks requiring tight back-and-forth with the user — stay in the current process
- When the overhead of spawning outweighs the benefit (trivial tasks)

Best practices:
- Provide complete context to child processes — they do not inherit your conversation history
- Prefer fewer, well-scoped child processes over many tiny ones
- Monitor child process results and synthesize them into a coherent response`
	}, true)

	// mcp_instructions: per-MCP-server usage instructions (CC-aligned)
	reg.Register("mcp_instructions", func() string {
		return buildMCPInstructionsSection(proc.mcpConfigs)
	}, true)

	// env_info: GDB environment variables
	reg.Register("env_info", func() string {
		envVars := proc.GetGdbEnvVars()
		if len(envVars) == 0 {
			return ""
		}
		var b strings.Builder
		b.WriteString("[GDB Environment Variables]\n")
		for envKey, envVal := range envVars {
			fmt.Fprintf(&b, "%s=%s\n", envKey, envVal)
		}
		return b.String()
	}, false)

	// loaded_skills: current skill bodies + synergies (dynamic — skills can change via specialize)
	reg.Register("loaded_skills", func() string {
		proc.mu.Lock()
		skills := make([]string, len(proc.Skills))
		copy(skills, proc.Skills)
		bodies := make(map[string]string, len(proc.SkillBodies))
		maps.Copy(bodies, proc.SkillBodies)
		proc.mu.Unlock()
		if len(skills) == 0 {
			return ""
		}
		var b strings.Builder
		b.WriteString("[Loaded Skills]\nCurrently loaded: ")
		b.WriteString(strings.Join(skills, ", "))
		b.WriteString("\n")
		for _, name := range skills {
			if body, ok := bodies[name]; ok && body != "" {
				b.WriteString("\n--- Skill: ")
				b.WriteString(name)
				b.WriteString(" ---\n")
				b.WriteString(body)
				b.WriteString("\n")
			}
		}
		// Synergy detection via skillLoader (needs SkillInfo objects)
		if k.skillLoader != nil {
			var skillInfos []*skillpkg.SkillInfo
			for _, name := range skills {
				if si, err := k.skillLoader(name); err == nil {
					skillInfos = append(skillInfos, si)
				}
			}
			synergyInstructions := skillpkg.DetectSynergies(skillInfos)
			if len(synergyInstructions) > 0 {
				b.WriteString("\n[Skill Synergy]\n\n")
				b.WriteString(strings.Join(synergyInstructions, "\n"))
			}
		}
		return b.String()
	}, false)

	// action_protocol: tool protocol or MCP snippet (dynamic per AC3 — AllowedDevices may change via specialize)
	reg.Register("action_protocol", func() string {
		if proc.UseNativeTools {
			if len(proc.mcpDevicePaths) > 0 {
				return mcpToolProtocolSnippet(proc.mcpDevicePaths)
			}
			return ""
		}
		vfsDefs, vfsMap := buildToolDefs(k.vfs.DeviceRegistry(), proc.AllowedDevices, proc.PlanningEnabled)
		metaDefs, metaMap := metaToolDefs(proc.PlanningEnabled, proc.DeferredSkills)
		return generateToolProtocol(vfsDefs, vfsMap, metaDefs, metaMap, proc.PlanningEnabled)
	}, false)

	return reg
}

// buildMCPInstructionsSection formats MCP server instructions for the system prompt.
// Follows CC's getMcpInstructions pattern: only includes servers that have non-empty Instructions.
func buildMCPInstructionsSection(configs []vfs.MCPConfig) string {
	if len(configs) == 0 {
		return ""
	}
	var blocks []string
	for _, cfg := range configs {
		if cfg.Instructions == "" {
			continue
		}
		blocks = append(blocks, fmt.Sprintf("## %s\n%s", cfg.ServerName, cfg.Instructions))
	}
	if len(blocks) == 0 {
		return ""
	}
	return fmt.Sprintf("# MCP Server Instructions\n\nThe following MCP servers have provided instructions for how to use their tools and resources:\n\n%s", strings.Join(blocks, "\n\n"))
}
