package llm

import (
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
