package kernel

import (
	"fmt"
	"maps"
	"strings"

	rnixctx "github.com/rnixai/rnix/context"
	skillpkg "github.com/rnixai/rnix/skills"
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
		b.WriteString("[Loaded Skills]\nThe following skills are loaded: ")
		b.WriteString(strings.Join(skills, ", "))
		b.WriteString(".\nFollow their instructions using available VFS devices.")
		b.WriteString("\nDo NOT try to call these skills via /dev/mcp/ or any device path.\n")
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
