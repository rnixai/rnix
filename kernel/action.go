package kernel

import (
	"encoding/json"
	"strings"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/vfs"
)

// toolProtocol is the legacy text-based action protocol, preserved as a
// fallback reference and test baseline. New processes use generateToolProtocol()
// from kernel/toolgen.go which auto-generates the protocol from ToolDefs.
var _ = toolProtocol //nolint:unused
var _ = planProtocol //nolint:unused

const toolProtocol = `

[Action Protocol]
Respond with a JSON object to perform an action, or plain text for your final answer.

Tool call — execute a VFS device:
{"action": "tool_call", "tool": "<vfs-device-path>", "data": {<tool-specific-payload>}}

Available VFS device paths:
  - Read file: tool="/dev/fs/src/main.go", data={}
  - Write file: tool="/dev/fs/docs/output.md", data={"content": "file content here"}
  - List directory: tool="/dev/fs/src", data={"op": "list"}
  - Run command: tool="/dev/shell", data={"command": "ls -la"}
  - LLM call: tool="/dev/llm/<provider>", data={"intent": "..."}
  - MCP tool: tool="/dev/mcp/<server>/<tool>", data={...}

IMPORTANT path rules:
  - /dev/fs paths MUST include the file/dir path after /dev/fs (e.g. /dev/fs/src/main.go). Never use /dev/fs alone.
  - /dev/fs paths are relative to the project working directory. Do NOT include the project name.
  - /dev/shell has no subpath — always use exactly "/dev/shell".

Spawn child process:
{"action": "spawn", "tool": "<child intent>", "data": {"agent": "<name>", "model": "<model>"}}

Complete — finish with a result:
{"action": "complete", "tool": "", "data": {"result": "<final output>"}}

Replan — revise your approach:
{"action": "replan", "tool": "", "data": {"reason": "<why replanning>"}}

Specialize — dynamically load a skill:
{"action": "specialize", "tool": "<skill-name>", "data": {}}

[Skills vs Tools]
Skills are instruction sets, NOT callable VFS devices. They teach you new capabilities.
- To load a skill: use the specialize action above.
- Once loaded, the skill's instructions appear in your conversation. Follow them using available VFS devices.
- Do NOT call skills via /dev/mcp/ or any other device path — skills have no device path.
- If a skill is already loaded, its instructions are already in your system prompt. Act on them directly.

If no action is needed, respond with plain text (your final answer).`

// planProtocol is the legacy plan action protocol text, preserved alongside toolProtocol.
const planProtocol = `

Plan — create an execution plan before acting:
{"action": "plan", "tool": "", "data": {"steps": ["step1", "step2", ...], "reason": "why planning"}}

Use planning when the task requires multiple coordinated steps. For simple tasks, use tool_call directly.`

// ActionType classifies LLM response actions.
type ActionType string

const (
	ActionText          ActionType = "text"
	ActionToolCall      ActionType = "tool_call"
	ActionPlan                     ActionType = "plan"
	ActionSpawn                    ActionType = "spawn"
	ActionComplete                 ActionType = "complete"
	ActionReplan                   ActionType = "replan"
	ActionSpecialize               ActionType = "specialize"
	ActionDiscoverSkill            ActionType = "discover_skill"
	ActionDeferredSkillPlaceholder ActionType = "deferred_skill_placeholder"
)

// ReasonAction represents a parsed action from an LLM response.
type ReasonAction struct {
	Type     ActionType
	Content  string
	ToolPath string
	ToolData []byte
}

// llmRequest is the JSON payload written to the LLM VFS device.
// Field names and json tags are compatible with drivers/llm.LLMRequest.
type llmRequest struct {
	Intent       string            `json:"intent"`
	SystemPrompt string            `json:"system_prompt,omitempty"`
	Model        string            `json:"model,omitempty"`
	MaxTurns     int               `json:"max_turns,omitempty"`
	TimeoutMs    int64             `json:"timeout_ms,omitempty"`
	Messages     []rnixctx.Message `json:"messages,omitempty"`
	Tools        []vfs.ToolDef     `json:"tools,omitempty"`
}

// llmToolCall represents a tool invocation in an LLM response.
// Fields and JSON tags are compatible with llm.ToolCall and context.ToolCall.
type llmToolCall struct {
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	Input      map[string]any `json:"input,omitempty"`
	ParseError string         `json:"parse_error,omitempty"`
}

// llmResponse is the JSON payload read from the LLM VFS device.
// Field names and json tags are compatible with drivers/llm.LLMResponse.
type llmResponse struct {
	Content    string        `json:"content"`
	TokensUsed int           `json:"tokens_used"`
	ToolCalls  []llmToolCall `json:"tool_calls,omitempty"`
}

// spawnActionData contains optional parameters parsed from spawn action data.
type spawnActionData struct {
	Agent string `json:"agent,omitempty"`
	Model string `json:"model,omitempty"`
}

// parseAction determines the action type from an LLM response.
func parseAction(resp *llmResponse) ReasonAction {
	if action, ok := tryParseStructuredAction(resp.Content); ok {
		return action
	}

	// Fallback: extract JSON from markdown code blocks or embedded objects.
	// Models often wrap JSON actions in ```json ... ``` or mix text with JSON.
	if extracted := extractEmbeddedAction(resp.Content); extracted != "" {
		if action, ok := tryParseStructuredAction(extracted); ok {
			return action
		}
	}

	return ReasonAction{Type: ActionText, Content: resp.Content}
}

// tryParseStructuredAction attempts to parse raw JSON text as a structured action.
func tryParseStructuredAction(raw string) (ReasonAction, bool) {
	var structured struct {
		Action string          `json:"action"`
		Tool   string          `json:"tool,omitempty"`
		Data   json.RawMessage `json:"data,omitempty"`
	}
	if err := json.Unmarshal([]byte(raw), &structured); err != nil {
		return ReasonAction{}, false
	}
	toolData := structured.Data
	if toolData == nil {
		toolData = []byte("{}")
	}
	switch ActionType(structured.Action) {
	case ActionToolCall:
		if structured.Tool != "" {
			return ReasonAction{
				Type:     ActionToolCall,
				ToolPath: structured.Tool,
				ToolData: toolData,
			}, true
		}
	case ActionPlan:
		return ReasonAction{
			Type:     ActionPlan,
			Content:  raw,
			ToolData: toolData,
		}, true
	case ActionSpawn:
		return ReasonAction{
			Type:     ActionSpawn,
			Content:  raw,
			ToolPath: structured.Tool,
			ToolData: toolData,
		}, true
	case ActionComplete:
		return ReasonAction{
			Type:     ActionComplete,
			Content:  raw,
			ToolData: toolData,
		}, true
	case ActionReplan:
		return ReasonAction{
			Type:     ActionReplan,
			Content:  raw,
			ToolData: toolData,
		}, true
	case ActionSpecialize:
		return ReasonAction{
			Type:     ActionSpecialize,
			ToolPath: structured.Tool,
			Content:  raw,
			ToolData: toolData,
		}, true
	case ActionDiscoverSkill:
		return ReasonAction{
			Type:     ActionDiscoverSkill,
			ToolPath: structured.Tool,
			Content:  raw,
			ToolData: toolData,
		}, true
	}
	return ReasonAction{}, false
}

// extractEmbeddedAction extracts a JSON action from text that may contain
// markdown code blocks (```json ... ```) or inline JSON objects ({"action": ...}).
func extractEmbeddedAction(content string) string {
	// Strategy 1: markdown code block — ```json\n{...}\n``` or ```\n{...}\n```
	for _, fence := range []string{"```json\n", "```json\r\n", "```\n", "```\r\n"} {
		start := strings.Index(content, fence)
		if start < 0 {
			continue
		}
		jsonStart := start + len(fence)
		end := strings.Index(content[jsonStart:], "\n```")
		if end < 0 {
			end = strings.Index(content[jsonStart:], "\r\n```")
		}
		if end < 0 {
			continue
		}
		candidate := strings.TrimSpace(content[jsonStart : jsonStart+end])
		if len(candidate) > 0 && candidate[0] == '{' {
			return candidate
		}
	}

	// Strategy 2: find the last top-level JSON object containing "action"
	// This handles models that output text followed by a bare JSON block.
	if idx := strings.LastIndex(content, `{"action"`); idx >= 0 {
		candidate := content[idx:]
		depth := 0
		for i, c := range candidate {
			switch c {
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					return candidate[:i+1]
				}
			}
		}
	}

	return ""
}
