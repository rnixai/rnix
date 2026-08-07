package kernel

import (
	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/drivers/llm"
	"github.com/rnixai/rnix/vfs"
)

// ActionType classifies LLM response actions.
type ActionType string

const (
	ActionText                     ActionType = "text"
	ActionToolCall                 ActionType = "tool_call"
	ActionPlan                     ActionType = "plan"
	ActionSpawn                    ActionType = "spawn"
	ActionComplete                 ActionType = "complete"
	ActionReplan                   ActionType = "replan"
	ActionSpecialize               ActionType = "specialize"
	ActionDiscoverSkill            ActionType = "discover_skill"
	ActionDeferredSkillPlaceholder ActionType = "deferred_skill_placeholder"
)

// llmRequest is the JSON payload written to the LLM VFS device.
// Field names and json tags are compatible with drivers/llm.LLMRequest.
type llmRequest struct {
	Intent       string `json:"intent"`
	SystemPrompt string `json:"system_prompt,omitempty"`
	Model        string `json:"model,omitempty"`
	// ReasoningEffort overrides the driver instance's configured reasoning
	// effort/level for this request. JSON tag MUST match drivers/llm.LLMRequest
	// (reasoning_effort) or the field is silently dropped across the VFS boundary.
	ReasoningEffort string            `json:"reasoning_effort,omitempty"`
	MaxTurns        int               `json:"max_turns,omitempty"`
	TimeoutMs       int64             `json:"timeout_ms,omitempty"`
	Messages        []rnixctx.Message `json:"messages,omitempty"`
	Tools           []vfs.ToolDef     `json:"tools,omitempty"`
	Skills          []llm.Skill       `json:"skills,omitempty"`
	ProjectDir      string            `json:"project_dir,omitempty"`
	CallerPID       uint64            `json:"caller_pid,omitempty"`
	CallerDepth     int               `json:"caller_depth,omitempty"`
	// CallerUUID mirrors drivers/llm.LLMRequest.CallerUUID — the spawning
	// process's UUIDv7, injected into the CLI child env as RNIX_PROC_UUID for
	// os-reconcile attribution (Story 66.5). JSON tag MUST match (caller_uuid)
	// or the field is silently dropped across the VFS boundary.
	CallerUUID string `json:"caller_uuid,omitempty"`
}

// llmToolCall represents a tool invocation in an LLM response.
// Fields and JSON tags are compatible with llm.ToolCall and context.ToolCall.
type llmToolCall struct {
	ID         string         `json:"id"`
	Name       string         `json:"name"`
	Input      map[string]any `json:"input,omitempty"`
	ParseError string         `json:"parse_error,omitempty"`
}

// reasoningBlock mirrors llm.ReasoningBlock for the JSON wire format
// flowing through the LLM VFS device into the kernel's response decoder.
type reasoningBlock struct {
	Type             string `json:"type"`
	Thinking         string `json:"thinking,omitempty"`
	Signature        string `json:"signature,omitempty"`
	Data             string `json:"data,omitempty"`
	ThoughtSignature []byte `json:"thought_signature,omitempty"`
}

// llmResponse is the JSON payload read from the LLM VFS device.
// Field names and json tags are compatible with drivers/llm.LLMResponse.
type llmResponse struct {
	Content           string `json:"content"`
	Reasoning         string `json:"reasoning,omitempty"`
	TokensUsed        int    `json:"tokens_used"`
	InputTokens       int    `json:"input_tokens,omitempty"`
	OutputTokens      int    `json:"output_tokens,omitempty"`
	CachedInputTokens int    `json:"cached_input_tokens,omitempty"`
	// CacheCreationInputTokens mirrors drivers/llm.LLMResponse (Story 74.1).
	// JSON tag MUST match (cache_creation_input_tokens) or the field is
	// silently dropped across the VFS boundary.
	CacheCreationInputTokens int              `json:"cache_creation_input_tokens,omitempty"`
	ToolCalls                []llmToolCall    `json:"tool_calls,omitempty"`
	ReasoningBlocks          []reasoningBlock `json:"reasoning_blocks,omitempty"`
	CostUSD                  float64          `json:"cost_usd,omitempty"`
	StopReason               string           `json:"stop_reason,omitempty"`
}
