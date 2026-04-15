package llm

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
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
