package kernel

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/kernel/memory"
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

	// --- Memory section (cached — frozen snapshot per-process, Story 35.2) ---
	reg.Register("memory", func() string {
		if k.memoryStore == nil {
			return ""
		}
		return memory.BuildMemoryBlock(k.memoryStore)
	}, true)

	// --- User profile section (cached — frozen snapshot per-process, Story 35.6) ---
	reg.Register("user_profile", func() string {
		if k.memoryStore == nil {
			return ""
		}
		return memory.BuildUserProfileBlock(k.memoryStore)
	}, true)

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
		if proc.CompactionDisabled {
			return ""
		}
		return `# Function Result Clearing

Old device results may be automatically cleared from context to free up space. When working with device results, write down any important information you might need later in your response, as the original result may be cleared later.`
	}, true)

	// spawn_guidance: guidance on when/how to spawn child processes
	reg.Register("spawn_guidance", func() string {
		if !proc.FeatureFlags.Planning {
			return ""
		}
		return `# Spawning Sub-Processes

You can spawn child processes for parallel or delegated work via the spawn meta-action. Guidelines:

When to spawn:
- Independent sub-tasks that can run in parallel (e.g., researching multiple modules simultaneously)
- Large delegated tasks that benefit from a separate context window
- Work requiring a different agent specialization or skill set

When NOT to spawn:
- NEVER spawn a child to read a file, execute a single command, or perform any one-tool operation — do it directly with the tools you have
- NEVER spawn a child with the same task you already have — that creates an infinite loop. Do the work yourself
- If your task can be completed with 1-2 tool calls, do NOT spawn — just execute directly
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

	// env_info: working directory, git status, platform, model, date (CC-aligned)
	reg.Register("env_info", func() string {
		var items []string

		workDir := ""
		if proc.ProjectConfig != nil {
			workDir = proc.ProjectConfig.ProjectDir
		}
		if workDir != "" {
			items = append(items, "Working directory: "+workDir)
			if _, err := os.Stat(filepath.Join(workDir, ".git")); err == nil {
				items = append(items, "Is a git repository: Yes")
			} else {
				items = append(items, "Is a git repository: No")
			}
		}

		items = append(items, "Platform: "+runtime.GOOS+"/"+runtime.GOARCH)

		if proc.Model != "" {
			modelDesc := proc.Model
			if proc.Provider != "" {
				modelDesc = proc.Provider + "/" + proc.Model
			}
			items = append(items, "Model: "+modelDesc)
		}

		items = append(items, "Date: "+time.Now().Format("2006-01-02"))

		var b strings.Builder
		b.WriteString("# Environment\n\nYou have been invoked in the following environment:\n")
		for _, item := range items {
			b.WriteString(" - ")
			b.WriteString(item)
			b.WriteString("\n")
		}

		// GDB environment variables (debug mode only)
		if envVars := proc.GetGdbEnvVars(); len(envVars) > 0 {
			b.WriteString("\n## Debug Environment Variables\n")
			for k, v := range envVars {
				fmt.Fprintf(&b, "%s=%s\n", k, v)
			}
		}

		return b.String()
	}, false)

	// loaded_skills: currently loaded skill names + synergies.
	// Skill bodies are delivered out-of-band:
	//   • Claude CLI gets them via a content-addressed bundle + --add-dir;
	//   • Other drivers receive them merged into SystemPrompt by the VFS layer.
	reg.Register("loaded_skills", func() string {
		proc.mu.Lock()
		skills := make([]string, len(proc.Skills))
		copy(skills, proc.Skills)
		proc.mu.Unlock()
		if len(skills) == 0 {
			return ""
		}
		var b strings.Builder
		b.WriteString("# Loaded Skills\nCurrently loaded: ")
		b.WriteString(strings.Join(skills, ", "))
		b.WriteString(".\nSkill instructions are delivered out-of-band.\n")
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
				b.WriteString("\n## Skill Synergies\n\n")
				b.WriteString(strings.Join(synergyInstructions, "\n"))
			}
		}
		return b.String()
	}, false)

	reg.Register("backpressure", func() string {
		slotUsed, slotMax, err := k.ctxMgr.SlotUsage(proc.CtxID)
		if err != nil || slotMax == 0 {
			return ""
		}
		slotPct := float64(slotUsed) / float64(slotMax) * 100
		if slotPct <= proc.effectiveBackpressureThreshold() {
			return ""
		}
		return fmt.Sprintf("# Context Resource Warning\n\n"+
			"Context message slots are %.0f%% full (%d/%d). "+
			"Prefer sequential tool calls over parallel batches to conserve context space. "+
			"Avoid requesting more than 2 tool calls per turn until context is compacted.",
			slotPct, slotUsed, slotMax)
	}, false)

	// action_protocol: no longer used (text protocol removed); section returns empty.
	// Tool definitions are passed via req.Tools for SDK providers;
	// CLI Agent providers use their own built-in tools.
	reg.Register("action_protocol", func() string {
		return ""
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
