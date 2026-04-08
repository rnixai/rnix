package kernel

import (
	"fmt"
	"strings"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/vfs"
)

// toolMapping describes how a native tool name maps back to a VFS device or meta action.
type toolMapping struct {
	Type        string     // "vfs" or "meta"
	VFSPath     string     // VFS device path (for Type="vfs")
	Action      ActionType // meta action type (for Type="meta")
	FSOperation string     // "read_file", "write_file", "list_dir" for /dev/fs tools
}

// buildToolDefs collects ToolDefs from registered VFS device drivers.
// Skips /dev/llm/ prefixed devices (LLM is not a user-invocable tool).
// Returns the collected ToolDefs and a toolMap for name→mapping resolution.
func buildToolDefs(devReg *vfs.DeviceRegistry, allowedDevices []string, planningEnabled bool) ([]vfs.ToolDef, map[string]toolMapping) {
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
			m := toolMapping{Type: "vfs", VFSPath: devPath}
			// Tag FS operations for special handling in executeNativeVFSTool
			switch def.Name {
			case "read_file", "write_file", "list_dir", "edit_file", "glob", "grep":
				m.FSOperation = def.Name
			}
			toolMap[def.Name] = m
		}
	}

	if len(allowedDevices) > 0 {
		for _, devPath := range allowedDevices {
			driver, ok := devReg.GetDriver(devPath)
			if !ok {
				continue // silently skip unknown paths (e.g., MCP devices)
			}
			collectFromDriver(devPath, driver)
		}
	} else {
		devReg.RangeDrivers(func(devPath string, driver any) bool {
			collectFromDriver(devPath, driver)
			return true
		})
	}

	return defs, toolMap
}

// metaToolDefs returns ToolDefs for kernel meta actions (complete, spawn, replan, specialize, plan).
func metaToolDefs(planningEnabled bool) ([]vfs.ToolDef, map[string]toolMapping) {
	defs := []vfs.ToolDef{
		{
			Name:            "complete",
			Description:     "Finish the task with a final result.",
			MaxResultTokens: 0, // unlimited — meta action, no VFS result
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
		{
			Name:            "spawn",
			Description:     "Spawn a child process to handle a sub-task.",
			MaxResultTokens: 0, // unlimited — meta action
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
		},
		{
			Name:            "replan",
			Description:     "Revise the current approach with a new plan.",
			MaxResultTokens: 0, // unlimited — meta action
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
		},
		{
			Name:            "specialize",
			Description:     "Dynamically load a skill to gain new capabilities.",
			MaxResultTokens: 0, // unlimited — meta action
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"skill_name": map[string]any{
						"type":        "string",
						"description": "Name of the skill to load",
					},
				},
				"required": []string{"skill_name"},
			},
		},
		{
			Name:            "discover_skill",
			Description:     "Search deferred skills by keyword to find relevant capabilities without loading them. Returns matching skill names with descriptions and relevance scores.",
			MaxResultTokens: 0,
			ShouldDefer:     true,
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{
						"type":        "string",
						"description": "Keywords describing the capability you need",
					},
				},
				"required": []string{"query"},
			},
		},
	}

	metaMap := map[string]toolMapping{
		"complete":       {Type: "meta", Action: ActionComplete},
		"spawn":          {Type: "meta", Action: ActionSpawn},
		"replan":         {Type: "meta", Action: ActionReplan},
		"specialize":     {Type: "meta", Action: ActionSpecialize},
		"discover_skill": {Type: "meta", Action: ActionDiscoverSkill},
	}

	if planningEnabled {
		defs = append(defs, vfs.ToolDef{
			Name:            "plan",
			Description:     "Create an execution plan before acting. Use when the task requires multiple coordinated steps.",
			MaxResultTokens: 0, // unlimited — meta action
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
		metaMap["plan"] = toolMapping{Type: "meta", Action: ActionPlan}
	}

	return defs, metaMap
}

// generateToolProtocol generates a text-based action protocol from ToolDefs.
// Used for CLI drivers that don't support native function calling.
func generateToolProtocol(toolDefs []vfs.ToolDef, toolMap map[string]toolMapping, metaDefs []vfs.ToolDef, metaMap map[string]toolMapping, planningEnabled bool) string {
	var sb strings.Builder

	sb.WriteString("\n\n[Action Protocol]\n")
	sb.WriteString("Respond with a JSON object to perform an action, or plain text for your final answer.\n\n")

	// VFS tool calls
	sb.WriteString("Tool call — execute a VFS device:\n")
	sb.WriteString(`{"action": "tool_call", "tool": "<vfs-device-path>", "data": {<tool-specific-payload>}}`)
	sb.WriteString("\n\nAvailable VFS device paths:\n")

	for _, def := range toolDefs {
		m, ok := toolMap[def.Name]
		if !ok {
			continue
		}
		switch m.FSOperation {
		case "read_file":
			sb.WriteString("  - Read file: tool=\"")
			sb.WriteString(m.VFSPath)
			sb.WriteString("/src/main.go\", data={}\n")
		case "write_file":
			sb.WriteString("  - Write file: tool=\"")
			sb.WriteString(m.VFSPath)
			sb.WriteString("/docs/output.md\", data={\"content\": \"file content here\"}\n")
		case "list_dir":
			sb.WriteString("  - List directory: tool=\"")
			sb.WriteString(m.VFSPath)
			sb.WriteString("/src\", data={\"op\": \"list\"}\n")
		case "edit_file":
			sb.WriteString("  - Edit file (string replace): tool=\"")
			sb.WriteString(m.VFSPath)
			sb.WriteString("/src/main.go\", data={\"op\": \"edit\", \"old_string\": \"old text\", \"new_string\": \"new text\"}\n")
		case "glob":
			sb.WriteString("  - Glob (find files): tool=\"")
			sb.WriteString(m.VFSPath)
			sb.WriteString("/.\", data={\"op\": \"glob\", \"pattern\": \"**/*.go\"}\n")
		case "grep":
			sb.WriteString("  - Grep (search content): tool=\"")
			sb.WriteString(m.VFSPath)
			sb.WriteString("/.\", data={\"op\": \"grep\", \"pattern\": \"func main\"}\n")
		default:
			// Generic VFS tool (e.g., shell)
			sb.WriteString("  - ")
			sb.WriteString(def.Description)
			sb.WriteString(": tool=\"")
			sb.WriteString(m.VFSPath)
			sb.WriteString("\", data=")
			writeExampleData(&sb, def)
			sb.WriteString("\n")
		}
	}

	sb.WriteString("\nIMPORTANT path rules:\n")
	sb.WriteString("  - /dev/fs paths MUST include the file/dir path after /dev/fs (e.g. /dev/fs/src/main.go). Never use /dev/fs alone.\n")
	sb.WriteString("  - /dev/fs paths are relative to the project working directory. Do NOT include the project name.\n")
	sb.WriteString("  - /dev/shell has no subpath — always use exactly \"/dev/shell\".\n\n")

	// Meta actions
	for _, def := range metaDefs {
		m, ok := metaMap[def.Name]
		if !ok {
			continue
		}
		switch m.Action {
		case ActionSpawn:
			sb.WriteString("Spawn child process:\n")
			sb.WriteString(`{"action": "spawn", "tool": "<child intent>", "data": {"agent": "<name>", "model": "<model>"}}`)
			sb.WriteString("\n\n")
		case ActionComplete:
			sb.WriteString("Complete — finish with a result:\n")
			sb.WriteString(`{"action": "complete", "tool": "", "data": {"result": "<final output>"}}`)
			sb.WriteString("\n\n")
		case ActionReplan:
			sb.WriteString("Replan — revise your approach:\n")
			sb.WriteString(`{"action": "replan", "tool": "", "data": {"reason": "<why replanning>"}}`)
			sb.WriteString("\n\n")
		case ActionSpecialize:
			sb.WriteString("Specialize — dynamically load a skill:\n")
			sb.WriteString(`{"action": "specialize", "tool": "<skill-name>", "data": {}}`)
			sb.WriteString("\n\n")
		case ActionDiscoverSkill:
			sb.WriteString("Discover Skill — search deferred skills by keyword:\n")
			sb.WriteString(`{"action": "discover_skill", "tool": "<query keywords>", "data": {}}`)
			sb.WriteString("\n\n")
		case ActionPlan:
			sb.WriteString("Plan — create an execution plan before acting:\n")
			sb.WriteString(`{"action": "plan", "tool": "", "data": {"steps": ["step1", "step2", ...], "reason": "why planning"}}`)
			sb.WriteString("\n\nUse planning when the task requires multiple coordinated steps. For simple tasks, use tool_call directly.\n")
		}
	}

	sb.WriteString("\n[Skills vs Tools]\n")
	sb.WriteString("Skills are instruction sets, NOT callable VFS devices. They teach you new capabilities.\n")
	sb.WriteString("- To load a skill: use the specialize action above.\n")
	sb.WriteString("- Once loaded, the skill's instructions appear in your conversation. Follow them using available VFS devices.\n")
	sb.WriteString("- Do NOT call skills via /dev/mcp/ or any other device path — skills have no device path.\n")
	sb.WriteString("- If a skill is already loaded, its instructions are already in your system prompt. Act on them directly.\n")
	sb.WriteString("\nIf no action is needed, respond with plain text (your final answer).")

	return sb.String()
}

// writeExampleData writes an example JSON data object based on tool parameters.
func writeExampleData(sb *strings.Builder, def vfs.ToolDef) {
	props, _ := def.Parameters["properties"].(map[string]any)
	if len(props) == 0 {
		sb.WriteString("{}")
		return
	}
	sb.WriteString("{")
	first := true
	for k := range props {
		if !first {
			sb.WriteString(", ")
		}
		fmt.Fprintf(sb, `"%s": "..."`, k)
		first = false
	}
	sb.WriteString("}")
}

// mcpToolProtocolSnippet generates a text description for MCP devices in mixed mode.
func mcpToolProtocolSnippet(mcpDevices []string) string {
	if len(mcpDevices) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("\n\n[MCP Tools (text protocol)]\n")
	sb.WriteString("The following MCP tools are available via text protocol:\n")
	for _, dev := range mcpDevices {
		fmt.Fprintf(&sb, "  - MCP tool: tool=\"%s\", data={...tool-specific-params...}\n", dev)
	}
	sb.WriteString("Call MCP tools using the tool_call action with the device path above.\n")
	return sb.String()
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
