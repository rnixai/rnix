package kernel

import (
	"fmt"
	"strings"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/vfs"
)

// toolMapping describes how a tool name maps back to a VFS device or meta action.
type toolMapping struct {
	Type        string     // "vfs" or "meta"
	VFSPath     string     // VFS device path (for Type="vfs")
	Action      ActionType // meta action type (for Type="meta")
	FSOperation string     // "Read", "Write", "list_dir", "Edit", "Glob", "Grep" for /dev/fs tools (PascalCase canonical names; list_dir kept as-is — CC has no equivalent)
}

// buildToolDefs collects ToolDefs from registered VFS device drivers.
// Skips /dev/llm/ prefixed devices (LLM is not a user-invocable tool).
// Returns the collected ToolDefs and a toolMap for name→mapping resolution.
func buildToolDefs(devReg *vfs.DeviceRegistry, allowedDevices []string) ([]vfs.ToolDef, map[string]toolMapping) {
	var defs []vfs.ToolDef
	toolMap := make(map[string]toolMapping)

	collectFromDriver := func(devPath string, driver any) {
		// Skip LLM devices — they are not user-invocable tools
		if strings.HasPrefix(devPath, "/dev/llm/") || strings.HasPrefix(devPath, "/dev/llm") {
			return
		}
		td, ok := driver.(vfs.ToolDescriptor)
		if !ok {
			return
		}
		for _, def := range td.ToolDefs() {
			defs = append(defs, def)
			// Drivers that multiplex multiple tools onto distinct subpaths
			// (e.g. /dev/intent → /decompose, /confirm, /status, /execute)
			// set def.Subpath so the kernel routes Open to the correct
			// VFSFile.subpath. Empty Subpath = open device root (existing
			// behavior for /dev/shell, /dev/fs, etc.).
			vfsPath := devPath + def.Subpath
			m := toolMapping{Type: "vfs", VFSPath: vfsPath}
			// Tag FS operations for special handling in executeVFSTool
			switch def.Name {
			case "Read", "Write", "list_dir", "Edit", "Glob", "Grep":
				m.FSOperation = def.Name
			}
			toolMap[def.Name] = m
		}
	}

	if baseDeviceWhitelistActive(allowedDevices) {
		for _, devPath := range allowedDevices {
			driver, ok := devReg.GetDriver(devPath)
			if !ok {
				continue // silently skip unknown paths (e.g., MCP devices)
			}
			collectFromDriver(devPath, driver)
		}
	} else {
		// No skill-declared base-device whitelist (empty, or only MCP mounts):
		// expose every base device. MCP mounts are additive permits and must not
		// suppress base-device tools — an agent with `mcp:` but no `skills:` (e.g.
		// stem) still needs /dev/fs, /dev/shell, etc.
		devReg.RangeDrivers(func(devPath string, driver any) bool {
			collectFromDriver(devPath, driver)
			return true
		})
	}

	return defs, toolMap
}

// baseDeviceWhitelistActive reports whether allowedDevices imposes a whitelist on
// base /dev devices. MCP mount paths (mcpPathPrefix, e.g. /mnt/mcp/<pid>-server)
// are additive permits — they grant access to that mount without restricting base
// devices. The base-device whitelist therefore activates only when at least one
// NON-MCP device path is present (i.e. a skill declared allowed_tools). Without
// this, appending an MCP mount to an otherwise-empty AllowedDevices would flip the
// process from "unrestricted" to "MCP-only" and revoke /dev/fs, /dev/shell, etc.
func baseDeviceWhitelistActive(allowedDevices []string) bool {
	for _, dev := range allowedDevices {
		if !strings.HasPrefix(dev, mcpPathPrefix) {
			return true
		}
	}
	return false
}

// metaToolDefs returns ToolDefs for kernel meta actions, conditionally based on FeatureFlags.
// complete is always generated; other meta tools are gated by their respective flag.
// Tool names follow Claude Code canonical form to match LLM training distribution anchors.
func metaToolDefs(flags FeatureFlags, deferredSkills []DeferredSkillMeta) ([]vfs.ToolDef, map[string]toolMapping) {
	defs := []vfs.ToolDef{
		{
			Name:            "complete",
			Description:     "Finish the task with a final result.",
			MaxResultTokens: 0,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"result": map[string]any{
						"type":        "string",
						"description": "The final output or answer",
					},
				},
				"required": []string{"result"},
			},
		},
	}
	metaMap := map[string]toolMapping{
		"complete": {Type: "meta", Action: ActionComplete},
	}

	if flags.Spawn {
		defs = append(defs, vfs.ToolDef{
			Name:            "Agent",
			Description:     "Spawn a child process (sub-agent) to handle a sub-task. Returns the agent's report when it completes.",
			MaxResultTokens: 0,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"intent": map[string]any{
						"type":        "string",
						"description": "What the child process should accomplish",
					},
					"agent": map[string]any{
						"type":        "string",
						"description": "Optional agent name to use",
					},
					"model": map[string]any{
						"type":        "string",
						"description": "Optional model override",
					},
				},
				"required": []string{"intent"},
			},
		})
		metaMap["Agent"] = toolMapping{Type: "meta", Action: ActionSpawn}
	}

	if flags.Replan {
		defs = append(defs, vfs.ToolDef{
			Name:            "replan",
			Description:     "Revise the current approach with a new plan.",
			MaxResultTokens: 0,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"reason": map[string]any{
						"type":        "string",
						"description": "Why the current approach needs revision",
					},
				},
				"required": []string{"reason"},
			},
		})
		metaMap["replan"] = toolMapping{Type: "meta", Action: ActionReplan}
	}

	if flags.Specialize {
		defs = append(defs, vfs.ToolDef{
			Name:            "Skill",
			Description:     "Dynamically load a skill to gain new capabilities.",
			MaxResultTokens: 0,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"skill": map[string]any{
						"type":        "string",
						"description": "Name of the skill to load",
					},
					"args": map[string]any{
						"type":        "string",
						"description": "Optional arguments for the skill (reserved; currently ignored)",
					},
				},
				"required": []string{"skill"},
			},
		})
		metaMap["Skill"] = toolMapping{Type: "meta", Action: ActionSpecialize}
	}

	if flags.DiscoverSkill {
		defs = append(defs, vfs.ToolDef{
			Name:            "ToolSearch",
			Description:     "Search deferred tools by keyword to find relevant capabilities without loading them. Returns matching tool names with descriptions and relevance scores.",
			MaxResultTokens: 0,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "Keywords describing the capability you need",
					},
					"max_results": map[string]any{
						"type":        "number",
						"description": "Maximum number of results to return (default 5)",
					},
				},
				"required": []string{"query"},
			},
		})
		metaMap["ToolSearch"] = toolMapping{Type: "meta", Action: ActionDiscoverSkill}

		// Deferred skill placeholders depend on ToolSearch being available
		for _, ds := range deferredSkills {
			toolName := "skill_" + ds.Name
			defs = append(defs, vfs.ToolDef{
				Name:        toolName,
				Description: fmt.Sprintf("[Deferred Skill] %s — %s. Use ToolSearch to search, then Skill to load.", ds.Name, ds.Description),
				ShouldDefer: true,
				Parameters: map[string]any{
					"type":       "object",
					"properties": map[string]any{},
				},
			})
			metaMap[toolName] = toolMapping{Type: "meta", Action: ActionDeferredSkillPlaceholder}
		}
	}

	if flags.Planning {
		defs = append(defs, vfs.ToolDef{
			Name:            "EnterPlanMode",
			Description:     "Create an execution plan before acting. Use when the task requires multiple coordinated steps.",
			MaxResultTokens: 0,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"steps": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Ordered list of steps to execute",
					},
					"reason": map[string]any{
						"type":        "string",
						"description": "Why planning is needed",
					},
				},
				"required": []string{"steps", "reason"},
			},
		})
		metaMap["EnterPlanMode"] = toolMapping{Type: "meta", Action: ActionPlan}
	}

	return defs, metaMap
}

// convertToolCalls converts kernel llmToolCall slice to context.ToolCall slice.
func convertToolCalls(calls []llmToolCall) []rnixctx.ToolCall {
	result := make([]rnixctx.ToolCall, len(calls))
	for i, tc := range calls {
		result[i] = rnixctx.ToolCall{
			ID:    tc.ID,
			Name:  tc.Name,
			Input: tc.Input,
		}
	}
	return result
}

// convertReasoningBlocks converts kernel reasoningBlock slice to context.ReasoningBlock slice.
func convertReasoningBlocks(blocks []reasoningBlock) []rnixctx.ReasoningBlock {
	if len(blocks) == 0 {
		return nil
	}
	result := make([]rnixctx.ReasoningBlock, len(blocks))
	for i, b := range blocks {
		result[i] = rnixctx.ReasoningBlock{
			Type:             b.Type,
			Thinking:         b.Thinking,
			Signature:        b.Signature,
			Data:             b.Data,
			ThoughtSignature: b.ThoughtSignature,
		}
	}
	return result
}
