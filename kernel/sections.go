package kernel

import (
	"fmt"
	"strings"

	rnixctx "github.com/rnixai/rnix/context"
)

// registerSections creates a SectionRegistry with all standard sections for an agent-based spawn.
// Static sections use embedded template content; dynamic sections capture proc and kernel pointers.
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

	// action_protocol: tool protocol or MCP snippet (cached — VFS defs immutable after spawn)
	reg.Register("action_protocol", func() string {
		if proc.UseNativeTools {
			if len(proc.mcpDevicePaths) > 0 {
				return mcpToolProtocolSnippet(proc.mcpDevicePaths)
			}
			return ""
		}
		vfsDefs, vfsMap := buildToolDefs(k.vfs.DeviceRegistry(), proc.AllowedDevices, proc.PlanningEnabled)
		metaDefs, metaMap := metaToolDefs(proc.PlanningEnabled)
		return generateToolProtocol(vfsDefs, vfsMap, metaDefs, metaMap, proc.PlanningEnabled)
	}, true)

	// loaded_skills: current list of loaded skills (dynamic — skills can change via specialize)
	reg.Register("loaded_skills", func() string {
		proc.mu.Lock()
		skills := make([]string, len(proc.Skills))
		copy(skills, proc.Skills)
		proc.mu.Unlock()
		if len(skills) == 0 {
			return ""
		}
		return "[Loaded Skills]\nThe following skills are loaded: " +
			strings.Join(skills, ", ") +
			".\nTheir instructions are already in your system prompt. Follow them using available VFS devices." +
			"\nDo NOT try to call these skills via /dev/mcp/ or any device path."
	}, false)

	return reg
}
