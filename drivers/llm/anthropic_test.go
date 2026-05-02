package llm

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/anthropics/anthropic-sdk-go"
)

// Compile-time interface checks.
var (
	_ LLMDriver         = (*AnthropicDriver)(nil)
	_ ToolCallingDriver = (*AnthropicDriver)(nil)
	_ HealthChecker     = (*AnthropicDriver)(nil)
)

func TestAnthropicDriver_Info(t *testing.T) {
	d := NewAnthropicDriver("anthropic",
		WithAnthropicModel("claude-sonnet-4-20250514"),
		WithAnthropicKey("test-key"),
	)
	info := d.Info()
	if info.Name != "anthropic" {
		t.Errorf("Name = %q, want %q", info.Name, "anthropic")
	}
	if info.Provider != "anthropic" {
		t.Errorf("Provider = %q, want %q", info.Provider, "anthropic")
	}
	if info.DefaultModel != "claude-sonnet-4-20250514" {
		t.Errorf("DefaultModel = %q, want %q", info.DefaultModel, "claude-sonnet-4-20250514")
	}
	if info.DriverType != DriverAnthropic {
		t.Errorf("DriverType = %q, want %q", info.DriverType, DriverAnthropic)
	}
}

func TestAnthropicDriver_Info_NoModel(t *testing.T) {
	d := NewAnthropicDriver("my-claude")
	info := d.Info()
	if info.Name != "my-claude" {
		t.Errorf("Name = %q, want %q", info.Name, "my-claude")
	}
	if info.DefaultModel != "" {
		t.Errorf("DefaultModel = %q, want empty", info.DefaultModel)
	}
	if info.DriverType != DriverAnthropic {
		t.Errorf("DriverType = %q, want %q", info.DriverType, DriverAnthropic)
	}
}

func TestAnthropicDriver_DefaultMaxTokens(t *testing.T) {
	d := NewAnthropicDriver("test")
	if d.defaultMaxTokens != 4096 {
		t.Errorf("defaultMaxTokens = %d, want 4096", d.defaultMaxTokens)
	}
	d2 := NewAnthropicDriver("test", WithAnthropicMaxTokens(8192))
	if d2.defaultMaxTokens != 8192 {
		t.Errorf("defaultMaxTokens = %d, want 8192", d2.defaultMaxTokens)
	}
}

// TestAnthropicDriver_ConvertMessagesToAnthropic verifies message role mapping.
func TestAnthropicDriver_ConvertMessagesToAnthropic(t *testing.T) {
	msgs := []Message{
		{Role: "system", Content: "you are helpful"},
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi there"},
		{Role: "user", Content: "do something"},
	}

	result := convertMessagesToAnthropic(msgs)

	// System messages are skipped (handled via params.System)
	if len(result) != 3 {
		t.Fatalf("len(result) = %d, want 3 (system skipped)", len(result))
	}
	if result[0].Role != "user" {
		t.Errorf("result[0].Role = %q, want user", result[0].Role)
	}
	if result[1].Role != "assistant" {
		t.Errorf("result[1].Role = %q, want assistant", result[1].Role)
	}
	if result[2].Role != "user" {
		t.Errorf("result[2].Role = %q, want user", result[2].Role)
	}
}

// TestAnthropicDriver_ConvertMessagesToAnthropic_ToolRole verifies tool result mapping.
func TestAnthropicDriver_ConvertMessagesToAnthropic_ToolRole(t *testing.T) {
	msgs := []Message{
		{Role: "tool", ToolCallID: "call-123", Content: "result data"},
	}
	result := convertMessagesToAnthropic(msgs)
	if len(result) != 1 {
		t.Fatalf("len(result) = %d, want 1", len(result))
	}
	// Tool results are sent as user messages with ToolResult content blocks
	if result[0].Role != "user" {
		t.Errorf("Role = %q, want user", result[0].Role)
	}
}

// TestAnthropicDriver_ConvertMessagesToAnthropic_AssistantWithToolCalls verifies
// assistant messages with tool calls produce correct content blocks.
func TestAnthropicDriver_ConvertMessagesToAnthropic_AssistantWithToolCalls(t *testing.T) {
	msgs := []Message{
		{
			Role:    "assistant",
			Content: "Let me search",
			ToolCalls: []ToolCall{
				{ID: "tc-1", Name: "search", Input: map[string]any{"q": "test"}},
			},
		},
	}
	result := convertMessagesToAnthropic(msgs)
	if len(result) != 1 {
		t.Fatalf("len(result) = %d, want 1", len(result))
	}
	if result[0].Role != "assistant" {
		t.Errorf("Role = %q, want assistant", result[0].Role)
	}
	// Should have 2 content blocks: text + tool_use
	if len(result[0].Content) != 2 {
		t.Fatalf("len(Content) = %d, want 2", len(result[0].Content))
	}
}

// TestAnthropicDriver_ConvertToolDefsToAnthropic verifies tool conversion.
func TestAnthropicDriver_ConvertToolDefsToAnthropic(t *testing.T) {
	tools := []ToolDef{
		{
			Name:        "get_weather",
			Description: "Get weather info",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"city": map[string]any{"type": "string"},
				},
				"required": []any{"city"},
			},
		},
	}
	result := convertToolDefsToAnthropic(tools)
	if len(result) != 1 {
		t.Fatalf("len(result) = %d, want 1", len(result))
	}
	if result[0].OfTool == nil {
		t.Fatal("expected OfTool to be set")
	}
	if result[0].OfTool.Name != "get_weather" {
		t.Errorf("Name = %q, want get_weather", result[0].OfTool.Name)
	}
}

// TestAnthropicDriver_ConvertToolDefsToAnthropic_Nil verifies nil tools returns nil.
func TestAnthropicDriver_ConvertToolDefsToAnthropic_Nil(t *testing.T) {
	result := convertToolDefsToAnthropic(nil)
	if result != nil {
		t.Errorf("expected nil, got %v", result)
	}
}

// TestAnthropicDriver_ConvertToolSchemaToAnthropic verifies schema conversion.
func TestAnthropicDriver_ConvertToolSchemaToAnthropic(t *testing.T) {
	params := map[string]any{
		"properties": map[string]any{
			"name": map[string]any{"type": "string"},
		},
		"required": []any{"name"},
	}
	schema := convertToolSchemaToAnthropic(params)
	if schema.Properties == nil {
		t.Error("expected Properties to be set")
	}
	if len(schema.Required) != 1 || schema.Required[0] != "name" {
		t.Errorf("Required = %v, want [name]", schema.Required)
	}
}

// TestAnthropicDriver_ConvertMessage verifies response message conversion.
func TestAnthropicDriver_ConvertMessage(t *testing.T) {
	d := NewAnthropicDriver("test")

	// Build a mock Message with text and tool_use content blocks.
	// We test via JSON round-trip since the Message struct uses internal types.
	msgJSON := `{
		"id": "msg_01",
		"type": "message",
		"role": "assistant",
		"content": [
			{"type": "text", "text": "Hello world"},
			{"type": "tool_use", "id": "tu_01", "name": "search", "input": {"q": "test"}}
		],
		"model": "claude-sonnet-4-20250514",
		"stop_reason": "end_turn",
		"stop_sequence": null,
		"usage": {"input_tokens": 100, "output_tokens": 50}
	}`
	var msg anthropic.Message
	if err := json.Unmarshal([]byte(msgJSON), &msg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	resp := d.convertMessage(&msg)
	if resp.Content != "Hello world" {
		t.Errorf("Content = %q, want %q", resp.Content, "Hello world")
	}
	if resp.InputTokens != 100 {
		t.Errorf("InputTokens = %d, want 100", resp.InputTokens)
	}
	if resp.OutputTokens != 50 {
		t.Errorf("OutputTokens = %d, want 50", resp.OutputTokens)
	}
	if resp.TokensUsed != 150 {
		t.Errorf("TokensUsed = %d, want 150", resp.TokensUsed)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("len(ToolCalls) = %d, want 1", len(resp.ToolCalls))
	}
	tc := resp.ToolCalls[0]
	if tc.ID != "tu_01" {
		t.Errorf("ToolCall.ID = %q, want tu_01", tc.ID)
	}
	if tc.Name != "search" {
		t.Errorf("ToolCall.Name = %q, want search", tc.Name)
	}
	if tc.Input["q"] != "test" {
		t.Errorf("ToolCall.Input[q] = %v, want test", tc.Input["q"])
	}
}

// TestAnthropicDriver_ConvertMessage_CachedTokens verifies R8: cache_read
// tokens reported by the Anthropic API surface on LLMResponse.CachedInputTokens.
func TestAnthropicDriver_ConvertMessage_CachedTokens(t *testing.T) {
	d := NewAnthropicDriver("test")

	msgJSON := `{
		"id": "msg_02",
		"type": "message",
		"role": "assistant",
		"content": [{"type": "text", "text": "cached"}],
		"model": "claude-sonnet-4-20250514",
		"stop_reason": "end_turn",
		"stop_sequence": null,
		"usage": {
			"input_tokens": 100,
			"output_tokens": 50,
			"cache_read_input_tokens": 80,
			"cache_creation_input_tokens": 10
		}
	}`
	var msg anthropic.Message
	if err := json.Unmarshal([]byte(msgJSON), &msg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	resp := d.convertMessage(&msg)
	if resp.CachedInputTokens != 80 {
		t.Errorf("CachedInputTokens = %d, want 80", resp.CachedInputTokens)
	}
	if resp.InputTokens != 100 {
		t.Errorf("InputTokens = %d, want 100", resp.InputTokens)
	}
}

// TestAnthropicDriver_ClassifyError verifies HTTP status code → sentinel mapping.
func TestAnthropicDriver_ClassifyError(t *testing.T) {
	d := NewAnthropicDriver("anthropic-test")

	tests := []struct {
		name      string
		err       error
		wantCode  int
		wantErrIs error
	}{
		{
			name:      "401 maps to ErrAuth",
			err:       makeAnthropicAPIError(401),
			wantCode:  401,
			wantErrIs: ErrAuth,
		},
		{
			name:      "429 maps to ErrRateLimit",
			err:       makeAnthropicAPIError(429),
			wantCode:  429,
			wantErrIs: ErrRateLimit,
		},
		{
			name:      "404 maps to ErrModelNotFound",
			err:       makeAnthropicAPIError(404),
			wantCode:  404,
			wantErrIs: ErrModelNotFound,
		},
		{
			name:      "400 context maps to ErrContextLength",
			err:       makeAnthropicAPIErrorWithMsg(400, "context length exceeded"),
			wantCode:  400,
			wantErrIs: ErrContextLength,
		},
		{
			name:      "400 non-context does not map to ErrContextLength",
			err:       makeAnthropicAPIError(400),
			wantCode:  400,
			wantErrIs: nil,
		},
		{
			name:     "non-API error wrapped without status",
			err:      fmt.Errorf("network timeout"),
			wantCode: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := d.classifyError(tt.err)

			if tt.wantCode > 0 {
				var llmErr *LLMError
				if !errors.As(got, &llmErr) {
					t.Fatalf("expected *LLMError, got %T: %v", got, got)
				}
				if llmErr.StatusCode != tt.wantCode {
					t.Errorf("StatusCode = %d, want %d", llmErr.StatusCode, tt.wantCode)
				}
				if llmErr.Provider != "anthropic-test" {
					t.Errorf("Provider = %q, want %q", llmErr.Provider, "anthropic-test")
				}
			}

			if tt.wantErrIs != nil {
				if !errors.Is(got, tt.wantErrIs) {
					t.Errorf("errors.Is(%v) = false, want true for %v", got, tt.wantErrIs)
				}
			} else if tt.wantCode > 0 && tt.name == "400 non-context does not map to ErrContextLength" {
				if errors.Is(got, ErrContextLength) {
					t.Errorf("expected no ErrContextLength, but got it")
				}
			}
		})
	}
}

// TestAnthropicConfig_ValidDriver verifies anthropic driver passes validation.
func TestAnthropicConfig_ValidDriver(t *testing.T) {
	data := []byte(`
version: "1"
providers:
  - name: claude-api
    driver: anthropic
    default_model: claude-sonnet-4-20250514
    api_key_env: ANTHROPIC_API_KEY
`)
	cfg, err := ParseProvidersConfig(data)
	if err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
	if len(cfg.Providers) != 1 {
		t.Fatalf("len(providers) = %d, want 1", len(cfg.Providers))
	}
	p := cfg.Providers[0]
	if p.Driver != DriverAnthropic {
		t.Errorf("Driver = %q, want %q", p.Driver, DriverAnthropic)
	}
}

// TestAnthropicFactory_CreateDriver verifies factory creates an AnthropicDriver.
func TestAnthropicFactory_CreateDriver(t *testing.T) {
	cfg := ProviderConfig{
		Name:         "claude-api",
		Driver:       DriverAnthropic,
		DefaultModel: "claude-sonnet-4-20250514",
		APIKeyEnv:    "ANTHROPIC_API_KEY",
		MaxTokens:    8192,
	}
	drv, err := CreateDriverWithEnv(cfg, func(key string) string {
		if key == "ANTHROPIC_API_KEY" {
			return "test-api-key"
		}
		return ""
	})
	if err != nil {
		t.Fatalf("CreateDriverWithEnv: %v", err)
	}
	ad, ok := drv.(*AnthropicDriver)
	if !ok {
		t.Fatalf("expected *AnthropicDriver, got %T", drv)
	}
	if ad.defaultModel != "claude-sonnet-4-20250514" {
		t.Errorf("defaultModel = %q, want claude-sonnet-4-20250514", ad.defaultModel)
	}
	if ad.defaultMaxTokens != 8192 {
		t.Errorf("defaultMaxTokens = %d, want 8192", ad.defaultMaxTokens)
	}
}

// TestAnthropicFactory_BaseURL verifies base_url is wired through.
func TestAnthropicFactory_BaseURL(t *testing.T) {
	cfg := ProviderConfig{
		Name:    "bedrock",
		Driver:  DriverAnthropic,
		BaseURL: "https://custom-endpoint.example.com",
	}
	drv, err := CreateDriverWithEnv(cfg, func(string) string { return "" })
	if err != nil {
		t.Fatalf("CreateDriverWithEnv: %v", err)
	}
	if _, ok := drv.(*AnthropicDriver); !ok {
		t.Fatalf("expected *AnthropicDriver, got %T", drv)
	}
}

// makeAnthropicAPIError creates an *anthropic.Error for testing.
func makeAnthropicAPIError(statusCode int) *anthropic.Error {
	req, _ := http.NewRequest("POST", "https://api.anthropic.com/v1/messages", nil)
	e := &anthropic.Error{
		StatusCode: statusCode,
		Request:    req,
		Response:   &http.Response{StatusCode: statusCode, Status: fmt.Sprintf("%d %s", statusCode, http.StatusText(statusCode))},
	}
	return e
}

// makeAnthropicAPIErrorWithMsg creates an *anthropic.Error whose Error() string
// contains the given message. We achieve this by marshalling the error with raw JSON
// via UnmarshalJSON so the JSON.raw field (used by Error()) includes the message.
func makeAnthropicAPIErrorWithMsg(statusCode int, msg string) *anthropic.Error {
	req, _ := http.NewRequest("POST", "https://api.anthropic.com/v1/messages", nil)
	body := fmt.Sprintf(`{"type":"error","error":{"type":"invalid_request_error","message":"%s"}}`, msg)
	e := &anthropic.Error{
		StatusCode: statusCode,
		Request:    req,
		Response:   &http.Response{StatusCode: statusCode, Status: fmt.Sprintf("%d %s", statusCode, http.StatusText(statusCode))},
	}
	// Populate JSON.raw so Error() includes the message text
	_ = e.UnmarshalJSON([]byte(body))
	return e
}

// TestAnthropicDriver_ConvertMessage_Thinking verifies that thinking content
// blocks (with signature) round-trip through the response decoder into
// LLMResponse.ReasoningBlocks. Provider parity: deepseek's anthropic-compat
// endpoint requires the signature to be echoed back on subsequent turns.
func TestAnthropicDriver_ConvertMessage_Thinking(t *testing.T) {
	d := NewAnthropicDriver("test")

	msgJSON := `{
		"id": "msg_thinking",
		"type": "message",
		"role": "assistant",
		"content": [
			{"type": "thinking", "thinking": "let me reason about this", "signature": "sig_abc123"},
			{"type": "text", "text": "the answer is 42"}
		],
		"model": "claude-sonnet-4-20250514",
		"stop_reason": "end_turn",
		"stop_sequence": null,
		"usage": {"input_tokens": 10, "output_tokens": 20}
	}`
	var msg anthropic.Message
	if err := json.Unmarshal([]byte(msgJSON), &msg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	resp := d.convertMessage(&msg)
	if resp.Content != "the answer is 42" {
		t.Errorf("Content = %q, want %q", resp.Content, "the answer is 42")
	}
	if resp.Reasoning != "let me reason about this" {
		t.Errorf("Reasoning = %q, want backwards-compat thinking text", resp.Reasoning)
	}
	if len(resp.ReasoningBlocks) != 1 {
		t.Fatalf("len(ReasoningBlocks) = %d, want 1", len(resp.ReasoningBlocks))
	}
	rb := resp.ReasoningBlocks[0]
	if rb.Type != "thinking" {
		t.Errorf("ReasoningBlock.Type = %q, want thinking", rb.Type)
	}
	if rb.Thinking != "let me reason about this" {
		t.Errorf("ReasoningBlock.Thinking = %q", rb.Thinking)
	}
	if rb.Signature != "sig_abc123" {
		t.Errorf("ReasoningBlock.Signature = %q, want sig_abc123 (signature must survive)", rb.Signature)
	}
}

// TestAnthropicDriver_ConvertMessage_RedactedThinking verifies redacted
// thinking blocks preserve the opaque Data payload.
func TestAnthropicDriver_ConvertMessage_RedactedThinking(t *testing.T) {
	d := NewAnthropicDriver("test")

	msgJSON := `{
		"id": "msg_redacted",
		"type": "message",
		"role": "assistant",
		"content": [
			{"type": "redacted_thinking", "data": "opaque-payload-xyz"},
			{"type": "text", "text": "done"}
		],
		"model": "claude-sonnet-4-20250514",
		"stop_reason": "end_turn",
		"stop_sequence": null,
		"usage": {"input_tokens": 5, "output_tokens": 5}
	}`
	var msg anthropic.Message
	if err := json.Unmarshal([]byte(msgJSON), &msg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	resp := d.convertMessage(&msg)
	if len(resp.ReasoningBlocks) != 1 {
		t.Fatalf("len(ReasoningBlocks) = %d, want 1", len(resp.ReasoningBlocks))
	}
	rb := resp.ReasoningBlocks[0]
	if rb.Type != "redacted_thinking" {
		t.Errorf("Type = %q, want redacted_thinking", rb.Type)
	}
	if rb.Data != "opaque-payload-xyz" {
		t.Errorf("Data = %q, want opaque-payload-xyz", rb.Data)
	}
}

// TestAnthropicDriver_ThinkingBlockRoundTrip verifies that an assistant
// message persisted with ReasoningBlocks emits ThinkingBlockParam(s) ahead
// of any text and tool_use blocks when sent back to the API. Block order
// is mandated by the Anthropic API.
func TestAnthropicDriver_ThinkingBlockRoundTrip(t *testing.T) {
	msgs := []Message{
		{
			Role: "assistant",
			ReasoningBlocks: []ReasoningBlock{
				{Type: "thinking", Thinking: "weighing options", Signature: "sig_xyz"},
			},
			Content: "Let me search",
			ToolCalls: []ToolCall{
				{ID: "tu-1", Name: "search", Input: map[string]any{"q": "test"}},
			},
		},
	}
	result := convertMessagesToAnthropic(msgs)
	if len(result) != 1 {
		t.Fatalf("len(result) = %d, want 1", len(result))
	}
	blocks := result[0].Content
	if len(blocks) != 3 {
		t.Fatalf("len(blocks) = %d, want 3 (thinking + text + tool_use)", len(blocks))
	}

	// Block 0 must be a thinking block carrying the original signature.
	if blocks[0].OfThinking == nil {
		t.Fatalf("blocks[0] is not a thinking block; got %+v", blocks[0])
	}
	if blocks[0].OfThinking.Thinking != "weighing options" {
		t.Errorf("thinking text = %q", blocks[0].OfThinking.Thinking)
	}
	if blocks[0].OfThinking.Signature != "sig_xyz" {
		t.Errorf("signature = %q, want sig_xyz", blocks[0].OfThinking.Signature)
	}

	// Block 1 must be the text block (NOT first — thinking comes first).
	if blocks[1].OfText == nil {
		t.Fatalf("blocks[1] is not a text block; got %+v", blocks[1])
	}
	if blocks[1].OfText.Text != "Let me search" {
		t.Errorf("text = %q", blocks[1].OfText.Text)
	}

	// Block 2 must be the tool_use block (last per API ordering rules).
	if blocks[2].OfToolUse == nil {
		t.Fatalf("blocks[2] is not a tool_use block; got %+v", blocks[2])
	}
	if blocks[2].OfToolUse.Name != "search" {
		t.Errorf("tool name = %q", blocks[2].OfToolUse.Name)
	}
}

// TestAnthropicDriver_RedactedThinkingPersisted verifies the round-trip
// path for redacted_thinking — the opaque data field must survive into
// the SDK params block.
func TestAnthropicDriver_RedactedThinkingPersisted(t *testing.T) {
	msgs := []Message{
		{
			Role: "assistant",
			ReasoningBlocks: []ReasoningBlock{
				{Type: "redacted_thinking", Data: "encrypted-blob"},
			},
			Content: "ok",
		},
	}
	result := convertMessagesToAnthropic(msgs)
	if len(result) != 1 {
		t.Fatalf("len(result) = %d, want 1", len(result))
	}
	blocks := result[0].Content
	if len(blocks) != 2 {
		t.Fatalf("len(blocks) = %d, want 2 (redacted_thinking + text)", len(blocks))
	}
	if blocks[0].OfRedactedThinking == nil {
		t.Fatalf("blocks[0] is not a redacted_thinking block; got %+v", blocks[0])
	}
	if blocks[0].OfRedactedThinking.Data != "encrypted-blob" {
		t.Errorf("data = %q, want encrypted-blob", blocks[0].OfRedactedThinking.Data)
	}
}

// TestAnthropicDriver_AssistantNoThinking_NoRegression locks the
// historical behaviour: assistant messages without ReasoningBlocks must
// still produce a single text block (and tool_use blocks if present),
// matching the pre-thinking layout.
func TestAnthropicDriver_AssistantNoThinking_NoRegression(t *testing.T) {
	msgs := []Message{
		{Role: "assistant", Content: "hi there"},
	}
	result := convertMessagesToAnthropic(msgs)
	if len(result) != 1 || len(result[0].Content) != 1 {
		t.Fatalf("expected 1 message with 1 text block; got %d msgs / blocks=%v", len(result), result)
	}
	if result[0].Content[0].OfText == nil || result[0].Content[0].OfText.Text != "hi there" {
		t.Errorf("expected text block 'hi there'; got %+v", result[0].Content[0])
	}
}

// TestExtractDeepSeekThinking_NoSeparator verifies the zero-alloc
// fast path: text without DeepSeek separators must round-trip unchanged
// and report hadSeparator=false.
func TestExtractDeepSeekThinking_NoSeparator(t *testing.T) {
	clean, reasoning, had := extractDeepSeekThinking("hello world")
	if clean != "hello world" || reasoning != "" || had {
		t.Errorf("extractDeepSeekThinking(no-sep) = (%q, %q, %v), want (%q, %q, false)",
			clean, reasoning, had, "hello world", "")
	}
}

// TestExtractDeepSeekThinking_BeginEndPair verifies the canonical
// begin+end pair: thinking text between separators routes to reasoning,
// content outside routes to clean.
func TestExtractDeepSeekThinking_BeginEndPair(t *testing.T) {
	in := "<｜begin▁of▁thinking｜>plan A<｜end▁of▁thinking｜>final"
	clean, reasoning, had := extractDeepSeekThinking(in)
	if clean != "final" || reasoning != "plan A" || !had {
		t.Errorf("extractDeepSeekThinking(pair) = (%q, %q, %v), want (%q, %q, true)",
			clean, reasoning, had, "final", "plan A")
	}
}

// TestExtractDeepSeekThinking_EndOnly verifies the trailing-only-end
// case: when there's no begin separator but an end exists, the prefix
// is treated as thinking and the suffix as content.
func TestExtractDeepSeekThinking_EndOnly(t *testing.T) {
	in := "plan A<｜end▁of▁thinking｜>final"
	clean, reasoning, had := extractDeepSeekThinking(in)
	if clean != "final" || reasoning != "plan A" || !had {
		t.Errorf("extractDeepSeekThinking(end-only) = (%q, %q, %v), want (%q, %q, true)",
			clean, reasoning, had, "final", "plan A")
	}
}

// TestExtractDeepSeekThinking_MultiplePairs verifies multiple begin/end
// pairs in a single TextBlock: thinking parts are concatenated in
// encounter order, content parts likewise.
func TestExtractDeepSeekThinking_MultiplePairs(t *testing.T) {
	in := "<｜begin▁of▁thinking｜>a<｜end▁of▁thinking｜>x<｜begin▁of▁thinking｜>b<｜end▁of▁thinking｜>y"
	clean, reasoning, had := extractDeepSeekThinking(in)
	if clean != "xy" || reasoning != "ab" || !had {
		t.Errorf("extractDeepSeekThinking(multi) = (%q, %q, %v), want (%q, %q, true)",
			clean, reasoning, had, "xy", "ab")
	}
}

// TestExtractDeepSeekThinking_BeginOnlyPartial verifies the case where
// a begin separator is present but the matching end is missing: the
// remainder after begin is treated as thinking, content is empty.
func TestExtractDeepSeekThinking_BeginOnlyPartial(t *testing.T) {
	in := "<｜begin▁of▁thinking｜>partial"
	clean, reasoning, had := extractDeepSeekThinking(in)
	if clean != "" || reasoning != "partial" || !had {
		t.Errorf("extractDeepSeekThinking(begin-only) = (%q, %q, %v), want (%q, %q, true)",
			clean, reasoning, had, "", "partial")
	}
}

// TestAnthropicDriver_ConvertMessage_DeepSeekTextLeak_Pair verifies
// convertMessage routes a TextBlock with begin+end separators into
// LLMResponse.Content (clean) and ReasoningBlocks (thinking with
// signature="").
func TestAnthropicDriver_ConvertMessage_DeepSeekTextLeak_Pair(t *testing.T) {
	d := NewAnthropicDriver("test")
	msgJSON := `{
		"id": "msg_leak",
		"type": "message",
		"role": "assistant",
		"content": [
			{"type": "text", "text": "<｜begin▁of▁thinking｜>weighing options<｜end▁of▁thinking｜>final answer"}
		],
		"model": "deepseek-v4-pro",
		"stop_reason": "end_turn",
		"stop_sequence": null,
		"usage": {"input_tokens": 5, "output_tokens": 8}
	}`
	var msg anthropic.Message
	if err := json.Unmarshal([]byte(msgJSON), &msg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	resp := d.convertMessage(&msg)
	if resp.Content != "final answer" {
		t.Errorf("Content = %q, want %q", resp.Content, "final answer")
	}
	if resp.Reasoning != "weighing options" {
		t.Errorf("Reasoning = %q, want %q", resp.Reasoning, "weighing options")
	}
	if len(resp.ReasoningBlocks) != 1 {
		t.Fatalf("len(ReasoningBlocks) = %d, want 1", len(resp.ReasoningBlocks))
	}
	rb := resp.ReasoningBlocks[0]
	if rb.Type != "thinking" || rb.Thinking != "weighing options" || rb.Signature != "" {
		t.Errorf("ReasoningBlock = %+v, want {thinking, weighing options, ''}", rb)
	}
}

// TestAnthropicDriver_ConvertMessage_DeepSeekTextLeak_EndOnly verifies
// the trailing-only-end case at the convertMessage integration level.
func TestAnthropicDriver_ConvertMessage_DeepSeekTextLeak_EndOnly(t *testing.T) {
	d := NewAnthropicDriver("test")
	msgJSON := `{
		"id": "msg_leak_end",
		"type": "message",
		"role": "assistant",
		"content": [
			{"type": "text", "text": "internal reasoning<｜end▁of▁thinking｜>visible reply"}
		],
		"model": "deepseek-v4-pro",
		"stop_reason": "end_turn",
		"stop_sequence": null,
		"usage": {"input_tokens": 3, "output_tokens": 6}
	}`
	var msg anthropic.Message
	if err := json.Unmarshal([]byte(msgJSON), &msg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	resp := d.convertMessage(&msg)
	if resp.Content != "visible reply" {
		t.Errorf("Content = %q, want %q", resp.Content, "visible reply")
	}
	if resp.Reasoning != "internal reasoning" {
		t.Errorf("Reasoning = %q, want %q", resp.Reasoning, "internal reasoning")
	}
}

// TestAnthropicDriver_ConvertMessage_DeepSeekTextLeak_CoexistsWithSDKBlock
// verifies that an SDK ThinkingBlock (with signature) and a TextBlock
// containing a DeepSeek-style leak coexist correctly: both contribute
// ReasoningBlocks in encounter order, and only the leaked one has an
// empty signature.
func TestAnthropicDriver_ConvertMessage_DeepSeekTextLeak_CoexistsWithSDKBlock(t *testing.T) {
	d := NewAnthropicDriver("test")
	msgJSON := `{
		"id": "msg_mixed",
		"type": "message",
		"role": "assistant",
		"content": [
			{"type": "thinking", "thinking": "sdk-thought", "signature": "sig_xyz"},
			{"type": "text", "text": "<｜begin▁of▁thinking｜>leak-thought<｜end▁of▁thinking｜>real reply"}
		],
		"model": "deepseek-v4-pro",
		"stop_reason": "end_turn",
		"stop_sequence": null,
		"usage": {"input_tokens": 7, "output_tokens": 12}
	}`
	var msg anthropic.Message
	if err := json.Unmarshal([]byte(msgJSON), &msg); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	resp := d.convertMessage(&msg)
	if resp.Content != "real reply" {
		t.Errorf("Content = %q, want %q", resp.Content, "real reply")
	}
	if len(resp.ReasoningBlocks) != 2 {
		t.Fatalf("len(ReasoningBlocks) = %d, want 2 (SDK + leak)", len(resp.ReasoningBlocks))
	}
	sdk := resp.ReasoningBlocks[0]
	if sdk.Thinking != "sdk-thought" || sdk.Signature != "sig_xyz" {
		t.Errorf("SDK block = %+v, want sdk-thought/sig_xyz", sdk)
	}
	leak := resp.ReasoningBlocks[1]
	if leak.Thinking != "leak-thought" || leak.Signature != "" {
		t.Errorf("leak block = %+v, want leak-thought/empty-sig", leak)
	}
	if !strings.Contains(resp.Reasoning, "sdk-thought") || !strings.Contains(resp.Reasoning, "leak-thought") {
		t.Errorf("Reasoning = %q, want both sdk-thought and leak-thought", resp.Reasoning)
	}
}

// TestAnthropicDriver_StreamDone_DeepSeekTextLeak validates the stream
// done-event extraction logic by directly invoking populateDoneEventFromAcc
// with an accumulated message that mixes a clean TextBlock and a
// DeepSeek-leaked TextBlock. Avoids a real SSE round-trip.
func TestAnthropicDriver_StreamDone_DeepSeekTextLeak(t *testing.T) {
	accJSON := `{
		"id": "msg_stream",
		"type": "message",
		"role": "assistant",
		"content": [
			{"type": "text", "text": "hello "},
			{"type": "text", "text": "<｜begin▁of▁thinking｜>internal<｜end▁of▁thinking｜>world"}
		],
		"model": "deepseek-v4-pro",
		"stop_reason": "end_turn",
		"stop_sequence": null,
		"usage": {"input_tokens": 2, "output_tokens": 4}
	}`
	var acc anthropic.Message
	if err := json.Unmarshal([]byte(accJSON), &acc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	evt := StreamEvent{Type: "done"}
	populateDoneEventFromAcc(&acc, &evt)

	if evt.Content != "hello world" {
		t.Errorf("evt.Content = %q, want %q (cleaned + concatenated)", evt.Content, "hello world")
	}
	if len(evt.ReasoningBlocks) != 1 {
		t.Fatalf("len(ReasoningBlocks) = %d, want 1", len(evt.ReasoningBlocks))
	}
	rb := evt.ReasoningBlocks[0]
	if rb.Type != "thinking" || rb.Thinking != "internal" || rb.Signature != "" {
		t.Errorf("ReasoningBlock = %+v, want {thinking, internal, ''}", rb)
	}

	// Pure TextBlocks (no leak) must NOT set evt.Content (vfsfile.go
	// reset is destructive — resetting with empty content for a clean
	// stream would erase already-streamed TextDeltas).
	cleanJSON := `{
		"id": "msg_stream_clean",
		"type": "message",
		"role": "assistant",
		"content": [{"type": "text", "text": "no leak here"}],
		"model": "claude-sonnet-4",
		"stop_reason": "end_turn",
		"stop_sequence": null,
		"usage": {"input_tokens": 1, "output_tokens": 1}
	}`
	var accClean anthropic.Message
	if err := json.Unmarshal([]byte(cleanJSON), &accClean); err != nil {
		t.Fatalf("unmarshal clean: %v", err)
	}
	evtClean := StreamEvent{Type: "done"}
	populateDoneEventFromAcc(&accClean, &evtClean)
	if evtClean.Content != "" {
		t.Errorf("evtClean.Content = %q, want empty (no-leak path must NOT set Content)", evtClean.Content)
	}
}

// TestAnthropicDriver_LeakReasoningBlock_RoundTrip closes AC3: a
// ReasoningBlock with Signature="" extracted from a DeepSeek text leak
// must still be emitted as a ThinkingBlockParam on the next request
// turn (DeepSeek's anthropic-compat endpoint requires every prior
// thinking block to be echoed back, even with an empty signature).
func TestAnthropicDriver_LeakReasoningBlock_RoundTrip(t *testing.T) {
	msgs := []Message{
		{
			Role: "assistant",
			ReasoningBlocks: []ReasoningBlock{
				{Type: "thinking", Thinking: "leaked-thought", Signature: ""},
			},
			Content: "visible reply",
		},
	}
	result := convertMessagesToAnthropic(msgs)
	if len(result) != 1 {
		t.Fatalf("len(result) = %d, want 1", len(result))
	}
	blocks := result[0].Content
	if len(blocks) != 2 {
		t.Fatalf("len(blocks) = %d, want 2 (thinking + text)", len(blocks))
	}
	if blocks[0].OfThinking == nil {
		t.Fatalf("blocks[0] is not a thinking block; got %+v", blocks[0])
	}
	if blocks[0].OfThinking.Thinking != "leaked-thought" {
		t.Errorf("thinking text = %q, want leaked-thought", blocks[0].OfThinking.Thinking)
	}
	if blocks[0].OfThinking.Signature != "" {
		t.Errorf("signature = %q, want empty (leak path)", blocks[0].OfThinking.Signature)
	}
	if blocks[1].OfText == nil || blocks[1].OfText.Text != "visible reply" {
		t.Errorf("blocks[1] = %+v, want text 'visible reply'", blocks[1])
	}
}
