package kernel

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"time"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/internal/config"
	"github.com/rnixai/rnix/kernel/memory"
	skillpkg "github.com/rnixai/rnix/skills"
	"github.com/rnixai/rnix/vfs"
)

// nowFn returns the current time. Package-level indirection so tests can inject
// a fixed "now" when validating that env_info's Date is frozen at registration
// time (Story 69.1). Mirrors the internal/ui nowFunc precedent.
var nowFn = time.Now

// Backpressure tiers. Content within a tier is byte-identical across every
// Build() of the same session — that stability is the whole point (Story 69.1).
const (
	backpressureTierElevated = "elevated"
	backpressureTierCritical = "critical"
)

// backpressureTier classifies context pressure into a discrete tier.
// Returns "" (no warning), "elevated", or "critical".
//
// The argument is a PERCENTAGE, deliberately axis-agnostic: it carried slot
// usage% up to Story 69.1 and carries token usage% from Story 71.1 on. Signature
// and boundary arithmetic are unchanged across that move — only the caller's data
// source changed.
//
// Boundaries use strict `>`: exactly at the threshold does NOT trigger, matching
// the pre-existing behaviour (TestBackpressureSection_AtExactThreshold).
// The critical boundary is threshold + (100-threshold)/2 rather than a hardcoded
// 85 so a custom BackpressureThreshold splits the remaining headroom evenly
// (default 70 → 85; custom 50 → 75; custom 90 → 95). For any threshold in
// (0, 100) the critical boundary never inverts against the threshold.
// Threshold ≥ 100 silently disables every warning for pct ≤ 100, exactly as
// the pre-69.1 `slotPct > threshold` test behaved — a misconfiguration degrades
// as before, not in some new way. On the token axis pct CAN exceed 100 (the
// denominator is ContextBudget = window×9/10, so real usage near the window
// crosses 100%); pct > 100 lands in critical, which is correct — the
// "Threshold ≥ 100 disables" claim is only about the pct ≤ 100 range.
// Spawn-time validation of the threshold is tracked in deferred-work.md.
func backpressureTier(usagePct, threshold float64) string {
	if usagePct <= threshold {
		return ""
	}
	if usagePct > threshold+(100-threshold)/2 {
		return backpressureTierCritical
	}
	return backpressureTierElevated
}

// backpressureText returns the byte-identical warning body for a tier.
// Unknown/empty tiers return "" so the section is skipped by Build() phase 4.
//
// Deliberately qualitative, never quantitative (Story 69.1 / Decision 33): the
// LLM only needs a behavioural direction, and the exact numbers are for the
// operator — who still gets them through the untouched IPC ContextStats fields
// rendered by the dashboard. Putting them in the prompt bought nothing and cost
// the whole prompt cache prefix on every single step.
//
// Bodies are byte-frozen across Story 71.1's axis move (slot% → token%) on
// purpose: the guidance ("prefer sequential tool calls", "write down what you
// need") is about how the model should behave under pressure, which does not
// depend on how the kernel measures that pressure. Rewording would churn the
// cache prefix for zero behavioural gain.
func backpressureText(tier string) string {
	switch tier {
	case backpressureTierElevated:
		return `# Context Resource Warning

Context message slots are running low. Prefer sequential tool calls over parallel batches to conserve context space. Avoid requesting more than 2 tool calls per turn until context is compacted.`
	case backpressureTierCritical:
		return `# Context Resource Warning

Context message slots are nearly exhausted. Limit yourself to a single tool call per turn until context is compacted. Write down any important information from device results in your response, as older results may be cleared to free up space.`
	default:
		return ""
	}
}

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

	// projectDir resolves per-project memory targets (Fix H). Empty when the
	// process has no project config → memory store falls back to global data.
	projectDir := ""
	if proc.ProjectConfig != nil {
		projectDir = proc.ProjectConfig.ProjectDir
	}

	// --- Project doc section (cached — eager frozen snapshot, Story 35.7) ---
	// Reads the nearest project-root AGENTS.md once at spawn time and captures it
	// in the closure (strongest freeze, matching agent_instructions) so a mid-run
	// edit to AGENTS.md never shifts this process's prompt and breaks the LLM
	// prompt cache hit rate (Story 35.2 lesson). Only AGENTS.md is recognized —
	// never CLAUDE.md/RNIX.md (AC3 / SPEC-agents-md-injection Non-goal). Empty when
	// injection is disabled (agent.yaml project_doc:false), there's no project, or
	// no AGENTS.md exists. startDir==boundary==projectDir because the process work
	// dir equals the project root (spawn.go SetWorkDir); nearest-wins still applies
	// for deeper startDirs, validated by the FindNearestAgentsMD unit tests.
	projectDoc := ""
	if proc.ProjectDocInjection && projectDir != "" {
		if body := config.FindNearestAgentsMD(projectDir, projectDir); body != "" {
			projectDoc = "# Project Instructions (AGENTS.md)\n\n" + body
		}
	}
	reg.Register("project_doc", func() string { return projectDoc }, true)

	// --- Memory section (cached — frozen snapshot per-process, Story 35.2) ---
	reg.Register("memory", func() string {
		if k.memoryStore == nil {
			return ""
		}
		return memory.BuildMemoryBlock(k.memoryStore, projectDir)
	}, true)

	// --- User profile section (cached — frozen snapshot per-process, Story 35.6) ---
	reg.Register("user_profile", func() string {
		if k.memoryStore == nil {
			return ""
		}
		return memory.BuildUserProfileBlock(k.memoryStore, projectDir)
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
	//
	// DO NOT change this to cached=true (Story 69.1 ruling 4, correcting the
	// Epic 69 audit table which claimed it was a free win). spawn.go Build()s
	// eagerly to capture FinalSystemPrompt *before* reason.go assigns
	// proc.scratchDir, so at that moment GetScratchDir() returns "". Caching would
	// freeze computed=true with an empty value and the section would disappear for
	// the rest of the process (until a compact's Invalidate()) — a behaviour
	// regression, not a no-op. It is not a cache-prefix breaker either: scratchDir
	// is assigned once and stays constant from step 1 onward.
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

	// spawnDate is captured eagerly at registration time (same pattern as
	// projectDoc / agentInstructions) so a long-running process that crosses
	// midnight keeps a byte-stable Date line instead of silently invalidating the
	// provider's prompt cache prefix mid-run (Story 69.1).
	spawnDate := nowFn().Format("2006-01-02")

	// env_info: working directory, git status, platform, model, date (CC-aligned)
	//
	// Cached stays false on purpose (Story 69.1 ruling 3): gdb env vars are written
	// at runtime via IPC SetGdbEnv, an explicit operator action that must take
	// effect without waiting for the next Invalidate() (i.e. the next compact).
	// With the date frozen above and the gdb map key-sorted below, this section is
	// byte-stable step to step unless the operator actually changes something.
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

		items = append(items, "Date: "+spawnDate)

		var b strings.Builder
		b.WriteString("# Environment\n\nYou have been invoked in the following environment:\n")
		for _, item := range items {
			b.WriteString(" - ")
			b.WriteString(item)
			b.WriteString("\n")
		}

		// GDB environment variables (debug mode only).
		// Key-sorted: GetGdbEnvVars returns a map, and Go randomizes map iteration
		// order, so an unsorted range emitted different bytes for the same variables
		// on adjacent steps — another silent prompt-cache prefix breaker (69.1).
		if envVars := proc.GetGdbEnvVars(); len(envVars) > 0 {
			b.WriteString("\n## Debug Environment Variables\n")
			for _, k := range slices.Sorted(maps.Keys(envVars)) {
				fmt.Fprintf(&b, "%s=%s\n", k, envVars[k])
			}
		}

		return b.String()
	}, false)

	// loaded_skills: currently loaded skill names + synergies.
	// Skill bodies are delivered out-of-band:
	//   • Claude CLI gets them via a content-addressed bundle + --add-dir;
	//   • Other drivers receive them merged into SystemPrompt by the VFS layer.
	//
	// cached=false is intentional (Story 69.1 ruling 5): the only mutation source
	// is specialize (tool_exec.go appends / rolls back proc.Skills), and a change
	// there IS a semantic change that must reach the prompt immediately — the same
	// contract Invalidate() expresses. No hidden byte churn: DetectSynergies sorts
	// its inputs, so identical skill sets always render identically.
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

	// backpressure: qualitative context-pressure guidance (Story 69.1),
	// measured on the TOKEN axis since Story 71.1 (AC2).
	//
	// The data source used to be slot usage (len(Messages)/MaxSize). Story 71.1
	// removes the slot ceiling entirely, which would have made slotMax==0 and
	// silently deleted this section forever — the exact "mechanism removed with
	// nobody noticing" failure Story 70.2 had just walked into. Moving the axis
	// keeps the mechanism alive AND fixes a long-standing false alarm: the slot
	// axis crossed 70% at roughly 36k real tokens, so this section had been
	// warning the model about exhaustion while it sat at a fraction of capacity.
	//
	// 分子 = proc.LastInputTokens: the provider's own reported prompt tokens from
	// the PREVIOUS step (observability provenance — a native number beats an
	// estimate). NOT the same numerator the compaction axis uses: TokenUsage()
	// estimates tokens in-process (including the just-built system prompt), while
	// LastInputTokens is the provider's report, which excludes the current system
	// prompt and carries per-driver accounting semantics. The two can diverge by
	// the system-prompt size. Deliberate: backpressure is advisory guidance, not a
	// gate, and the provenance principle favours the provider's number.
	// 分母 = proc.effectiveContextTokenLimit(): the SAME scale applyCtxTokenLimit
	// pushes into ctx.TokenLimit, so the prompt-side and compaction-side
	// denominators cannot drift.
	//
	// 🔴 This ComputeFn must NEVER call k.ctxMgr.TokenUsage(): TokenUsage calls
	// Sections.Build(), Build() calls this ComputeFn — unbounded recursion into a
	// stack overflow. Every value read here comes from kernel-side proc fields,
	// which is precisely why this shape was chosen: no ctx lock, no re-entry.
	//
	// LastInputTokens == 0 (first step, no response yet) → pct 0 → tier "" → no
	// injection. Correct degradation: the context is at its smallest then.
	//
	// The body is a per-tier constant so the system prompt stays byte-identical
	// across every step within a tier — a single changed byte invalidates the
	// provider's prompt cache prefix (both Anthropic's explicit CacheControl
	// breakpoint on the system prompt and implicit auto-caching), which is what
	// drove the measured 99.5% → 8.9% cache-hit collapse and the compact-timeout
	// death spiral. Tier transitions (≤2 per session) are the only churn left.
	//
	// Do NOT "fix" this by flipping Cached to true: Cached only holds until the
	// next Invalidate(), and compact *does* Invalidate() — worse, caching a stale
	// usage figure feeds the LLM wrong capacity information, which is strictly
	// worse than not injecting at all. The content itself has to be
	// step-invariant.
	reg.Register("backpressure", func() string {
		limit := proc.effectiveContextTokenLimit()
		if limit <= 0 {
			return ""
		}
		pct := float64(proc.GetLastInputTokens()) / float64(limit) * 100
		return backpressureText(backpressureTier(pct, proc.effectiveBackpressureThreshold()))
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
