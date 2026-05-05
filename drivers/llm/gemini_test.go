package llm

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	"google.golang.org/genai"
)

var (
	_ LLMDriver         = (*GeminiDriver)(nil)
	_ ToolCallingDriver = (*GeminiDriver)(nil)
	_ HealthChecker     = (*GeminiDriver)(nil)
)

func TestGeminiDriver_Info(t *testing.T) {
	d := NewGeminiDriver("gemini",
		WithGeminiModel("gemini-2.0-flash"),
		WithGeminiAPIKey("test-key"),
	)
	info := d.Info()
	if info.Name != "gemini" {
		t.Errorf("Name = %q, want %q", info.Name, "gemini")
	}
	if info.Provider != "gemini" {
		t.Errorf("Provider = %q, want %q", info.Provider, "gemini")
	}
	if info.DefaultModel != "gemini-2.0-flash" {
		t.Errorf("DefaultModel = %q, want %q", info.DefaultModel, "gemini-2.0-flash")
	}
	if info.DriverType != DriverGemini {
		t.Errorf("DriverType = %q, want %q", info.DriverType, DriverGemini)
	}
}

func TestGeminiDriver_Info_NoModel(t *testing.T) {
	d := NewGeminiDriver("my-gemini")
	info := d.Info()
	if info.Name != "my-gemini" {
		t.Errorf("Name = %q, want %q", info.Name, "my-gemini")
	}
	if info.DefaultModel != "" {
		t.Errorf("DefaultModel = %q, want empty", info.DefaultModel)
	}
	if info.DriverType != DriverGemini {
		t.Errorf("DriverType = %q, want %q", info.DriverType, DriverGemini)
	}
}

// TestGeminiConfig_NoBaseURLRequired verifies that a gemini provider config
// does not require base_url, unlike openai-compat.
func TestGeminiConfig_NoBaseURLRequired(t *testing.T) {
	data := []byte(`
version: "1"
providers:
  - name: gemini
    driver: gemini
    default_model: gemini-2.0-flash
    api_key_env: GEMINI_API_KEY
`)
	cfg, err := ParseProvidersConfig(data)
	if err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
	if len(cfg.Providers) != 1 {
		t.Fatalf("len(providers) = %d, want 1", len(cfg.Providers))
	}
	p := cfg.Providers[0]
	if p.Driver != DriverGemini {
		t.Errorf("Driver = %q, want %q", p.Driver, DriverGemini)
	}
}

// TestGeminiConfig_ThinkingBudget verifies that thinking_budget is parsed correctly.
func TestGeminiConfig_ThinkingBudget(t *testing.T) {
	data := []byte(`
version: "1"
providers:
  - name: gemini-think
    driver: gemini
    default_model: gemini-2.5-pro
    api_key_env: GEMINI_API_KEY
    thinking_budget: 8192
`)
	cfg, err := ParseProvidersConfig(data)
	if err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
	p := cfg.Providers[0]
	if p.ThinkingBudget != 8192 {
		t.Errorf("ThinkingBudget = %d, want 8192", p.ThinkingBudget)
	}
}

// TestGeminiConfig_InvalidDriver ensures unknown drivers still fail validation.
func TestGeminiConfig_InvalidDriver(t *testing.T) {
	data := []byte(`
version: "1"
providers:
  - name: bad
    driver: not-a-driver
`)
	_, err := ParseProvidersConfig(data)
	if err == nil {
		t.Fatal("expected validation error for unknown driver, got nil")
	}
	if !contains(err.Error(), "gemini") {
		t.Errorf("validation error should list gemini as valid driver, got: %v", err)
	}
}

// TestGeminiDriver_ClassifyError verifies HTTP status code → sentinel mapping.
func TestGeminiDriver_ClassifyError(t *testing.T) {
	d := NewGeminiDriver("gemini-test")

	tests := []struct {
		name       string
		err        error
		wantCode   int
		wantErrIs  error
	}{
		{
			name:      "401 maps to ErrAuth",
			err:       genai.APIError{Code: 401, Message: "invalid api key"},
			wantCode:  401,
			wantErrIs: ErrAuth,
		},
		{
			name:      "429 maps to ErrRateLimit",
			err:       genai.APIError{Code: 429, Message: "quota exceeded"},
			wantCode:  429,
			wantErrIs: ErrRateLimit,
		},
		{
			name:      "404 maps to ErrModelNotFound",
			err:       genai.APIError{Code: 404, Message: "model not found"},
			wantCode:  404,
			wantErrIs: ErrModelNotFound,
		},
		{
			name:      "400 context length maps to ErrContextLength",
			err:       genai.APIError{Code: 400, Message: "context window exceeded"},
			wantCode:  400,
			wantErrIs: ErrContextLength,
		},
		{
			name:      "400 non-context does not map to ErrContextLength",
			err:       genai.APIError{Code: 400, Message: "bad request format"},
			wantCode:  400,
			wantErrIs: nil,
		},
		{
			name:  "non-API error wrapped without status",
			err:   fmt.Errorf("network timeout"),
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
				if llmErr.Provider != "gemini-test" {
					t.Errorf("Provider = %q, want %q", llmErr.Provider, "gemini-test")
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

// TestGeminiDriver_ConvertMsgToGenai_ToolRole verifies tool message conversion.
func TestGeminiDriver_ConvertMsgToGenai_ToolRole(t *testing.T) {
	nameByID := map[string]string{"call-abc": "search"}
	m := Message{
		Role:       "tool",
		ToolCallID: "call-abc",
		Content:    "the result",
	}
	c := convertMsgToGenai(m, nameByID)
	if c == nil {
		t.Fatal("expected non-nil content")
	}
	if c.Role != "user" {
		t.Errorf("Role = %q, want %q", c.Role, "user")
	}
	if len(c.Parts) != 1 {
		t.Fatalf("len(Parts) = %d, want 1", len(c.Parts))
	}
	fr := c.Parts[0].FunctionResponse
	if fr == nil {
		t.Fatal("expected FunctionResponse, got nil")
	}
	if fr.Name != "search" {
		t.Errorf("FunctionResponse.Name = %q, want %q", fr.Name, "search")
	}
	if fr.ID != "call-abc" {
		t.Errorf("FunctionResponse.ID = %q, want %q", fr.ID, "call-abc")
	}
	if fr.Response["result"] != "the result" {
		t.Errorf("FunctionResponse.Response[result] = %v, want %q", fr.Response["result"], "the result")
	}
}

// TestGeminiDriver_ConvertMsgToGenai_AssistantWithToolCalls verifies
// assistant+tool_calls message → model role with FunctionCall parts.
func TestGeminiDriver_ConvertMsgToGenai_AssistantWithToolCalls(t *testing.T) {
	m := Message{
		Role:    "assistant",
		Content: "",
		ToolCalls: []ToolCall{
			{ID: "tc-1", Name: "list_files", Input: map[string]any{"dir": "/tmp"}},
		},
	}
	c := convertMsgToGenai(m, nil)
	if c == nil {
		t.Fatal("expected non-nil content")
	}
	if c.Role != "model" {
		t.Errorf("Role = %q, want model", c.Role)
	}
	if len(c.Parts) != 1 {
		t.Fatalf("len(Parts) = %d, want 1", len(c.Parts))
	}
	fc := c.Parts[0].FunctionCall
	if fc == nil {
		t.Fatal("expected FunctionCall part")
	}
	if fc.Name != "list_files" {
		t.Errorf("FunctionCall.Name = %q, want list_files", fc.Name)
	}
}

// TestGeminiFactory_CreateDriver verifies factory creates a GeminiDriver.
func TestGeminiFactory_CreateDriver(t *testing.T) {
	cfg := ProviderConfig{
		Name:         "gemini",
		Driver:       DriverGemini,
		DefaultModel: "gemini-2.0-flash",
		APIKeyEnv:    "GEMINI_API_KEY",
	}
	drv, err := CreateDriverWithEnv(cfg, func(key string) string {
		if key == "GEMINI_API_KEY" {
			return "test-api-key"
		}
		return ""
	})
	if err != nil {
		t.Fatalf("CreateDriverWithEnv: %v", err)
	}
	gd, ok := drv.(*GeminiDriver)
	if !ok {
		t.Fatalf("expected *GeminiDriver, got %T", drv)
	}
	if gd.apiKey != "test-api-key" {
		t.Errorf("apiKey = %q, want test-api-key", gd.apiKey)
	}
	if gd.defaultModel != "gemini-2.0-flash" {
		t.Errorf("defaultModel = %q, want gemini-2.0-flash", gd.defaultModel)
	}
}

// TestGeminiFactory_ThinkingBudget verifies thinking_budget is wired through.
func TestGeminiFactory_ThinkingBudget(t *testing.T) {
	cfg := ProviderConfig{
		Name:           "gemini-think",
		Driver:         DriverGemini,
		DefaultModel:   "gemini-2.5-pro",
		ThinkingBudget: 4096,
	}
	drv, err := CreateDriverWithEnv(cfg, func(string) string { return "" })
	if err != nil {
		t.Fatalf("CreateDriverWithEnv: %v", err)
	}
	gd, ok := drv.(*GeminiDriver)
	if !ok {
		t.Fatalf("expected *GeminiDriver, got %T", drv)
	}
	if gd.thinkingBudget != 4096 {
		t.Errorf("thinkingBudget = %d, want 4096", gd.thinkingBudget)
	}
}

// contains is a helper for string containment checks.
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		func() bool {
			for i := 0; i <= len(s)-len(substr); i++ {
				if s[i:i+len(substr)] == substr {
					return true
				}
			}
			return false
		}())
}

func TestConvertToolDefsToGenai_NilParameters(t *testing.T) {
	tools := []ToolDef{
		{Name: "cron_create", Description: "create a cron job"},
		{Name: "with_params", Description: "tool with params", Parameters: map[string]any{
			"type":       "object",
			"properties": map[string]any{"x": map[string]any{"type": "string"}},
		}},
	}
	decls := convertToolDefsToGenai(tools)
	if len(decls) != 2 {
		t.Fatalf("expected 2 declarations, got %d", len(decls))
	}
	// Tool with nil Parameters must NOT set ParametersJsonSchema (would serialize as null).
	if decls[0].ParametersJsonSchema != nil {
		t.Errorf("tool with nil Parameters: expected ParametersJsonSchema == nil, got %v", decls[0].ParametersJsonSchema)
	}
	// Tool with non-nil Parameters must set ParametersJsonSchema.
	if decls[1].ParametersJsonSchema == nil {
		t.Error("tool with Parameters: expected ParametersJsonSchema != nil")
	}
}

// TestExtractResponse_PreservesThoughtSignature verifies that Gemini 2.5+
// thinking responses round-trip the opaque ThoughtSignature: extractResponse
// must collect every Thought part into LLMResponse.ReasoningBlocks with the
// signature bytes intact, not just concatenate the text.
func TestExtractResponse_PreservesThoughtSignature(t *testing.T) {
	sig := []byte{0xDE, 0xAD, 0xBE, 0xEF}
	resp := &genai.GenerateContentResponse{
		Candidates: []*genai.Candidate{{
			Content: &genai.Content{
				Role: "model",
				Parts: []*genai.Part{
					{Thought: true, Text: "plan: list files first", ThoughtSignature: sig},
					{Text: "running ls"},
					{FunctionCall: &genai.FunctionCall{ID: "tc_1", Name: "shell", Args: map[string]any{"cmd": "ls"}}},
				},
			},
		}},
	}

	out := extractResponse(resp)

	if out.Reasoning != "plan: list files first" {
		t.Errorf("Reasoning = %q, want %q", out.Reasoning, "plan: list files first")
	}
	if len(out.ReasoningBlocks) != 1 {
		t.Fatalf("len(ReasoningBlocks) = %d, want 1", len(out.ReasoningBlocks))
	}
	rb := out.ReasoningBlocks[0]
	if rb.Type != "thought" {
		t.Errorf("ReasoningBlocks[0].Type = %q, want thought", rb.Type)
	}
	if rb.Thinking != "plan: list files first" {
		t.Errorf("ReasoningBlocks[0].Thinking = %q", rb.Thinking)
	}
	if !bytes.Equal(rb.ThoughtSignature, sig) {
		t.Errorf("ThoughtSignature = %v, want %v (signature dropped on inbound)", rb.ThoughtSignature, sig)
	}
	if out.Content != "running ls" {
		t.Errorf("Content = %q, want %q", out.Content, "running ls")
	}
	if len(out.ToolCalls) != 1 || out.ToolCalls[0].Name != "shell" {
		t.Errorf("ToolCalls = %+v", out.ToolCalls)
	}
}

// TestConvertMsgToGenai_RebuildsThoughtPartFromReasoningBlock verifies the
// outbound path: an assistant Message carrying a ReasoningBlock{Type:"thought"}
// must be rebuilt as a genai.Part with Thought=true and the ThoughtSignature
// echoed verbatim. Gemini requires this on multi-turn function-calling round-trips.
func TestConvertMsgToGenai_RebuildsThoughtPartFromReasoningBlock(t *testing.T) {
	sig := []byte{0x01, 0x02, 0x03}
	m := Message{
		Role:    "assistant",
		Content: "running",
		ReasoningBlocks: []ReasoningBlock{
			{Type: "thought", Thinking: "plan: list dir", ThoughtSignature: sig},
			// Anthropic-style block must be ignored by Gemini.
			{Type: "thinking", Thinking: "ignored", Signature: "anthropic-sig"},
		},
		ToolCalls: []ToolCall{
			{ID: "tc-1", Name: "shell", Input: map[string]any{"cmd": "ls"}},
		},
	}

	c := convertMsgToGenai(m, nil)
	if c == nil || c.Role != "model" {
		t.Fatalf("c = %+v, want non-nil model", c)
	}
	// Part order MUST be: thought → text → function_call.
	if len(c.Parts) != 3 {
		t.Fatalf("len(Parts) = %d, want 3 (thought, text, function_call)", len(c.Parts))
	}
	if !c.Parts[0].Thought {
		t.Errorf("Parts[0].Thought = false, want true")
	}
	if c.Parts[0].Text != "plan: list dir" {
		t.Errorf("Parts[0].Text = %q, want %q", c.Parts[0].Text, "plan: list dir")
	}
	if !bytes.Equal(c.Parts[0].ThoughtSignature, sig) {
		t.Errorf("Parts[0].ThoughtSignature = %v, want %v", c.Parts[0].ThoughtSignature, sig)
	}
	if c.Parts[1].Text != "running" || c.Parts[1].Thought {
		t.Errorf("Parts[1] = %+v, want text 'running' (non-thought)", c.Parts[1])
	}
	if c.Parts[2].FunctionCall == nil || c.Parts[2].FunctionCall.Name != "shell" {
		t.Errorf("Parts[2] = %+v, want shell function_call", c.Parts[2])
	}
}

// TestConvertMsgToGenai_OmitsAnthropicBlocks verifies cross-driver safety:
// a Message persisted from an Anthropic turn must NOT leak its thinking
// blocks into a Gemini request — only Type="thought" entries are consumed.
func TestConvertMsgToGenai_OmitsAnthropicBlocks(t *testing.T) {
	m := Message{
		Role:    "assistant",
		Content: "ok",
		ReasoningBlocks: []ReasoningBlock{
			{Type: "thinking", Thinking: "anthropic-only", Signature: "sig"},
			{Type: "redacted_thinking", Data: "redacted"},
		},
	}
	c := convertMsgToGenai(m, nil)
	if c == nil {
		t.Fatal("c = nil")
	}
	for i, p := range c.Parts {
		if p.Thought {
			t.Errorf("Parts[%d].Thought = true; Anthropic block leaked into Gemini outbound", i)
		}
	}
}

// TestReasoningBlock_ThoughtSignatureJSONRoundTrip locks in wire-format
// stability for context persistence: ThoughtSignature ([]byte) must encode
// to base64 in JSON and decode back to identical bytes, so a daemon restart
// or VFS cross-process transport never corrupts the signature.
func TestReasoningBlock_ThoughtSignatureJSONRoundTrip(t *testing.T) {
	sig := []byte{0xDE, 0xAD, 0xBE, 0xEF, 0x00, 0xFF}
	original := ReasoningBlock{
		Type:             "thought",
		Thinking:         "plan",
		ThoughtSignature: sig,
	}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	var got ReasoningBlock
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !bytes.Equal(got.ThoughtSignature, sig) {
		t.Errorf("round-trip ThoughtSignature = %v, want %v", got.ThoughtSignature, sig)
	}
	if got.Type != "thought" || got.Thinking != "plan" {
		t.Errorf("round-trip lost fields: %+v", got)
	}
}
