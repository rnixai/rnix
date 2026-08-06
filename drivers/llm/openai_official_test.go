package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

var (
	_ LLMDriver        = (*OpenAIDriver)(nil)
	_ ToolCallingDriver = (*OpenAIDriver)(nil)
	_ HealthChecker     = (*OpenAIDriver)(nil)
)

const okCompletion = `{"id":"c","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{}}`

func writeJSON(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/json")
	fmt.Fprint(w, body)
}

func writeJSONStatus(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	fmt.Fprint(w, body)
}

func newTestOpenAIDriver(handler http.HandlerFunc) (*OpenAIDriver, *httptest.Server, func()) {
	ts := httptest.NewServer(handler)
	d := NewOpenAIDriver("test-openai",
		WithOpenAIModel("gpt-4o"),
		WithOpenAIBaseURL(ts.URL),
		WithOpenAIKey("sk-test-key"),
		WithOpenAIHTTPClient(ts.Client()),
	)
	return d, ts, ts.Close
}

func TestOpenAIDriver_Info(t *testing.T) {
	d := NewOpenAIDriver("openai",
		WithOpenAIModel("gpt-4o"),
		WithOpenAIKey("sk-test"),
	)
	info := d.Info()
	if info.Name != "openai" {
		t.Errorf("Name = %q, want %q", info.Name, "openai")
	}
	if info.Provider != "openai" {
		t.Errorf("Provider = %q, want %q", info.Provider, "openai")
	}
	if info.DefaultModel != "gpt-4o" {
		t.Errorf("DefaultModel = %q, want %q", info.DefaultModel, "gpt-4o")
	}
	if info.DriverType != DriverOpenAI {
		t.Errorf("DriverType = %q, want %q", info.DriverType, DriverOpenAI)
	}
}

func TestOpenAIDriver_Call_Success(t *testing.T) {
	d, _, cleanup := newTestOpenAIDriver(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/chat/completions") {
			t.Errorf("path = %q, want /chat/completions suffix", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer sk-test-key" {
			t.Errorf("Authorization = %q, want Bearer sk-test-key", got)
		}
		writeJSON(w, `{"id":"chatcmpl-1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"Hello!"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`)
	})
	defer cleanup()

	resp, err := d.Call(context.Background(), LLMRequest{Intent: "hi"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if resp.Content != "Hello!" {
		t.Errorf("Content = %q, want %q", resp.Content, "Hello!")
	}
	if resp.TokensUsed != 15 {
		t.Errorf("TokensUsed = %d, want 15", resp.TokensUsed)
	}
	if resp.InputTokens != 10 {
		t.Errorf("InputTokens = %d, want 10", resp.InputTokens)
	}
	if resp.OutputTokens != 5 {
		t.Errorf("OutputTokens = %d, want 5", resp.OutputTokens)
	}
}

func TestOpenAIDriver_Call_WithMessages(t *testing.T) {
	var gotBody map[string]any
	d, _, cleanup := newTestOpenAIDriver(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &gotBody)
		writeJSON(w, okCompletion)
	})
	defer cleanup()

	_, err := d.Call(context.Background(), LLMRequest{
		Messages: []Message{
			{Role: "user", Content: "hello"},
			{Role: "assistant", Content: "hi"},
			{Role: "user", Content: "how are you"},
		},
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	msgs, ok := gotBody["messages"].([]any)
	if !ok {
		t.Fatal("messages not in request body")
	}
	if len(msgs) != 3 {
		t.Fatalf("Messages len = %d, want 3", len(msgs))
	}
}

func TestOpenAIDriver_Call_SystemPrompt(t *testing.T) {
	var gotBody map[string]any
	d, _, cleanup := newTestOpenAIDriver(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &gotBody)
		writeJSON(w, okCompletion)
	})
	defer cleanup()

	_, err := d.Call(context.Background(), LLMRequest{
		Intent:       "hi",
		SystemPrompt: "You are helpful",
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	msgs := gotBody["messages"].([]any)
	if len(msgs) != 2 {
		t.Fatalf("Messages len = %d, want 2", len(msgs))
	}
	first := msgs[0].(map[string]any)
	if first["role"] != "system" {
		t.Errorf("first message role = %v, want system", first["role"])
	}
}

func TestOpenAIDriver_Call_SystemPromptNoDouble(t *testing.T) {
	var gotBody map[string]any
	d, _, cleanup := newTestOpenAIDriver(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &gotBody)
		writeJSON(w, okCompletion)
	})
	defer cleanup()

	_, err := d.Call(context.Background(), LLMRequest{
		SystemPrompt: "You are helpful",
		Messages: []Message{
			{Role: "system", Content: "Existing system prompt"},
			{Role: "user", Content: "hi"},
		},
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	msgs := gotBody["messages"].([]any)
	if len(msgs) != 2 {
		t.Fatalf("Messages len = %d, want 2 (no double system)", len(msgs))
	}
}

func TestOpenAIDriver_Call_Timeout(t *testing.T) {
	d, _, cleanup := newTestOpenAIDriver(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		writeJSON(w, okCompletion)
	})
	defer cleanup()

	_, err := d.Call(context.Background(), LLMRequest{
		Intent:    "hi",
		TimeoutMs: 50,
	})
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !errors.Is(err, ErrTimeout) {
		t.Errorf("error = %v, want ErrTimeout", err)
	}
}

func TestOpenAIDriver_Call_AuthError(t *testing.T) {
	d, _, cleanup := newTestOpenAIDriver(func(w http.ResponseWriter, r *http.Request) {
		writeJSONStatus(w, 401, `{"error":{"message":"invalid api key","type":"invalid_request_error","param":null,"code":"invalid_api_key"}}`)
	})
	defer cleanup()

	_, err := d.Call(context.Background(), LLMRequest{Intent: "hi"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrAuth) {
		t.Errorf("error = %v, want ErrAuth", err)
	}
	var llmErr *LLMError
	if errors.As(err, &llmErr) && llmErr.StatusCode != 401 {
		t.Errorf("StatusCode = %d, want 401", llmErr.StatusCode)
	}
}

func TestOpenAIDriver_Call_RateLimit(t *testing.T) {
	d, _, cleanup := newTestOpenAIDriver(func(w http.ResponseWriter, r *http.Request) {
		writeJSONStatus(w, 429, `{"error":{"message":"rate limit exceeded","type":"requests","param":null,"code":null}}`)
	})
	defer cleanup()

	_, err := d.Call(context.Background(), LLMRequest{Intent: "hi"})
	if !errors.Is(err, ErrRateLimit) {
		t.Errorf("error = %v, want ErrRateLimit", err)
	}
}

func TestOpenAIDriver_Call_ModelNotFound(t *testing.T) {
	d, _, cleanup := newTestOpenAIDriver(func(w http.ResponseWriter, r *http.Request) {
		writeJSONStatus(w, 404, `{"error":{"message":"model not found","type":"invalid_request_error","param":null,"code":null}}`)
	})
	defer cleanup()

	_, err := d.Call(context.Background(), LLMRequest{Intent: "hi"})
	if !errors.Is(err, ErrModelNotFound) {
		t.Errorf("error = %v, want ErrModelNotFound", err)
	}
}

func TestOpenAIDriver_Call_EmptyChoices(t *testing.T) {
	d, _, cleanup := newTestOpenAIDriver(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"id":"c","object":"chat.completion","choices":[],"usage":{"prompt_tokens":5,"completion_tokens":0,"total_tokens":5}}`)
	})
	defer cleanup()

	resp, err := d.Call(context.Background(), LLMRequest{Intent: "hi"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if resp.Content != "" {
		t.Errorf("Content = %q, want empty", resp.Content)
	}
	if resp.TokensUsed != 5 {
		t.Errorf("TokensUsed = %d, want 5", resp.TokensUsed)
	}
}

func TestOpenAIDriver_Call_ToolResultMessage(t *testing.T) {
	var gotBody map[string]any
	d, _, cleanup := newTestOpenAIDriver(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &gotBody)
		writeJSON(w, okCompletion)
	})
	defer cleanup()

	_, err := d.Call(context.Background(), LLMRequest{
		Messages: []Message{
			{Role: "user", Content: "what's the weather?"},
			{Role: "assistant", Content: ""},
			{Role: "tool", Content: `{"temp": 25}`, ToolCallID: "call_1"},
		},
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	msgs := gotBody["messages"].([]any)
	toolMsg := msgs[2].(map[string]any)
	if toolMsg["role"] != "tool" {
		t.Errorf("tool message role = %v, want tool", toolMsg["role"])
	}
	if toolMsg["tool_call_id"] != "call_1" {
		t.Errorf("tool_call_id = %v, want call_1", toolMsg["tool_call_id"])
	}
}

func TestOpenAIDriver_Call_AssistantToolCallsMessage(t *testing.T) {
	var gotBody map[string]any
	d, _, cleanup := newTestOpenAIDriver(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &gotBody)
		writeJSON(w, okCompletion)
	})
	defer cleanup()

	_, err := d.Call(context.Background(), LLMRequest{
		Messages: []Message{
			{Role: "user", Content: "weather?"},
			{
				Role:    "assistant",
				Content: "",
				ToolCalls: []ToolCall{
					{ID: "call_1", Name: "get_weather", Input: map[string]any{"city": "Tokyo"}},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	msgs := gotBody["messages"].([]any)
	assistantMsg := msgs[1].(map[string]any)
	toolCalls, ok := assistantMsg["tool_calls"].([]any)
	if !ok || len(toolCalls) != 1 {
		t.Fatalf("ToolCalls = %v, want 1 tool call", assistantMsg["tool_calls"])
	}
	tc := toolCalls[0].(map[string]any)
	if tc["id"] != "call_1" {
		t.Errorf("ToolCall id = %v, want call_1", tc["id"])
	}
	fn := tc["function"].(map[string]any)
	if fn["name"] != "get_weather" {
		t.Errorf("ToolCall name = %v, want get_weather", fn["name"])
	}
}

// --- CallWithTools tests ---

func TestOpenAIDriver_CallWithTools_Success(t *testing.T) {
	d, _, cleanup := newTestOpenAIDriver(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]any
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &reqBody)

		tools, ok := reqBody["tools"].([]any)
		if !ok || len(tools) != 1 {
			t.Errorf("tools not sent correctly: %v", reqBody["tools"])
		}

		writeJSON(w, `{"id":"c","object":"chat.completion","choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"call_abc","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Tokyo\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":20,"completion_tokens":10,"total_tokens":30}}`)
	})
	defer cleanup()

	tools := []ToolDef{
		{Name: "get_weather", Description: "Get weather", Parameters: map[string]any{"type": "object"}},
	}
	resp, err := d.CallWithTools(context.Background(), LLMRequest{Intent: "weather in Tokyo"}, tools)
	if err != nil {
		t.Fatalf("CallWithTools: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("ToolCalls len = %d, want 1", len(resp.ToolCalls))
	}
	tc := resp.ToolCalls[0]
	if tc.ID != "call_abc" {
		t.Errorf("ID = %q, want call_abc", tc.ID)
	}
	if tc.Name != "get_weather" {
		t.Errorf("Name = %q, want get_weather", tc.Name)
	}
	if tc.Input["city"] != "Tokyo" {
		t.Errorf("Input[city] = %v, want Tokyo", tc.Input["city"])
	}
}

// --- Stream tests ---

func writeOAISSE(w http.ResponseWriter, data string) {
	fmt.Fprintf(w, "data: %s\n\n", data)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

func TestOpenAIDriver_Stream_Success(t *testing.T) {
	d, _, cleanup := newTestOpenAIDriver(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		writeOAISSE(w, `{"id":"c","object":"chat.completion.chunk","choices":[{"delta":{"content":"Hel"},"index":0}]}`)
		writeOAISSE(w, `{"id":"c","object":"chat.completion.chunk","choices":[{"delta":{"content":"lo!"},"index":0}]}`)
		writeOAISSE(w, `{"id":"c","object":"chat.completion.chunk","choices":[{"delta":{},"index":0,"finish_reason":"stop"}],"usage":{"prompt_tokens":8,"completion_tokens":3,"total_tokens":11}}`)
		writeOAISSE(w, "[DONE]")
	})
	defer cleanup()

	ch, err := d.Stream(context.Background(), LLMRequest{Intent: "hi"})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	var contents []string
	var doneEvent StreamEvent
	for evt := range ch {
		switch evt.Type {
		case "content":
			contents = append(contents, evt.Content)
		case "done":
			doneEvent = evt
		case "error":
			t.Fatalf("unexpected error event: %v", evt.Err)
		}
	}
	if got := strings.Join(contents, ""); got != "Hello!" {
		t.Errorf("content = %q, want %q", got, "Hello!")
	}
	if doneEvent.TokensUsed != 11 {
		t.Errorf("TokensUsed = %d, want 11", doneEvent.TokensUsed)
	}
}

func TestOpenAIDriver_Stream_WithUsage(t *testing.T) {
	var gotBody map[string]any
	d, _, cleanup := newTestOpenAIDriver(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &gotBody)
		w.Header().Set("Content-Type", "text/event-stream")
		writeOAISSE(w, `{"id":"c","object":"chat.completion.chunk","choices":[{"delta":{"content":"ok"},"index":0}]}`)
		writeOAISSE(w, "[DONE]")
	})
	defer cleanup()

	ch, err := d.Stream(context.Background(), LLMRequest{Intent: "hi"})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	for range ch {
	}

	streamOpts, ok := gotBody["stream_options"].(map[string]any)
	if !ok {
		t.Fatal("stream_options not in request body")
	}
	if streamOpts["include_usage"] != true {
		t.Error("stream_options.include_usage should be true")
	}
}

func TestOpenAIDriver_StreamWithTools_SingleTool(t *testing.T) {
	d, _, cleanup := newTestOpenAIDriver(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		writeOAISSE(w, `{"id":"c","object":"chat.completion.chunk","choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"get_","arguments":""}}]},"index":0}]}`)
		writeOAISSE(w, `{"id":"c","object":"chat.completion.chunk","choices":[{"delta":{"tool_calls":[{"index":0,"function":{"name":"weather","arguments":"{\"ci"}}]},"index":0}]}`)
		writeOAISSE(w, `{"id":"c","object":"chat.completion.chunk","choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"ty\":\"Tokyo\"}"}}]},"index":0}]}`)
		writeOAISSE(w, `{"id":"c","object":"chat.completion.chunk","choices":[{"delta":{},"index":0,"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8}}`)
		writeOAISSE(w, "[DONE]")
	})
	defer cleanup()

	ch, err := d.StreamWithTools(context.Background(), LLMRequest{Intent: "weather"}, []ToolDef{{Name: "get_weather"}})
	if err != nil {
		t.Fatalf("StreamWithTools: %v", err)
	}

	var doneEvt StreamEvent
	for evt := range ch {
		if evt.Type == "error" {
			t.Fatalf("error event: %v", evt.Err)
		}
		if evt.Type == "done" {
			doneEvt = evt
		}
	}
	if len(doneEvt.ToolCalls) != 1 {
		t.Fatalf("ToolCalls len = %d, want 1", len(doneEvt.ToolCalls))
	}
	tc := doneEvt.ToolCalls[0]
	if tc.ID != "call_1" {
		t.Errorf("ID = %q, want call_1", tc.ID)
	}
	if tc.Name != "get_weather" {
		t.Errorf("Name = %q, want get_weather", tc.Name)
	}
	if tc.Input["city"] != "Tokyo" {
		t.Errorf("Input[city] = %v, want Tokyo", tc.Input["city"])
	}
	if doneEvt.TokensUsed != 8 {
		t.Errorf("TokensUsed = %d, want 8", doneEvt.TokensUsed)
	}
}

func TestOpenAIDriver_StreamWithTools_MultiTool(t *testing.T) {
	d, _, cleanup := newTestOpenAIDriver(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		writeOAISSE(w, `{"id":"c","object":"chat.completion.chunk","choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"search","arguments":""}}]},"index":0}]}`)
		writeOAISSE(w, `{"id":"c","object":"chat.completion.chunk","choices":[{"delta":{"tool_calls":[{"index":1,"id":"c2","type":"function","function":{"name":"read","arguments":""}}]},"index":0}]}`)
		writeOAISSE(w, `{"id":"c","object":"chat.completion.chunk","choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"q\":\"test\"}"}}]},"index":0}]}`)
		writeOAISSE(w, `{"id":"c","object":"chat.completion.chunk","choices":[{"delta":{"tool_calls":[{"index":1,"function":{"arguments":"{\"path\":\"/tmp\"}"}}]},"index":0}]}`)
		writeOAISSE(w, `{"id":"c","object":"chat.completion.chunk","choices":[{"delta":{},"index":0,"finish_reason":"tool_calls"}]}`)
		writeOAISSE(w, "[DONE]")
	})
	defer cleanup()

	ch, err := d.StreamWithTools(context.Background(), LLMRequest{Intent: "do stuff"}, []ToolDef{{Name: "search"}, {Name: "read"}})
	if err != nil {
		t.Fatalf("StreamWithTools: %v", err)
	}

	var doneEvt StreamEvent
	for evt := range ch {
		if evt.Type == "error" {
			t.Fatalf("error event: %v", evt.Err)
		}
		if evt.Type == "done" {
			doneEvt = evt
		}
	}
	if len(doneEvt.ToolCalls) != 2 {
		t.Fatalf("ToolCalls len = %d, want 2", len(doneEvt.ToolCalls))
	}
	if doneEvt.ToolCalls[0].ID != "c1" || doneEvt.ToolCalls[0].Name != "search" {
		t.Errorf("ToolCalls[0] = %+v, want id=c1 name=search", doneEvt.ToolCalls[0])
	}
	if doneEvt.ToolCalls[0].Input["q"] != "test" {
		t.Errorf("ToolCalls[0].Input[q] = %v, want test", doneEvt.ToolCalls[0].Input["q"])
	}
	if doneEvt.ToolCalls[1].ID != "c2" || doneEvt.ToolCalls[1].Name != "read" {
		t.Errorf("ToolCalls[1] = %+v, want id=c2 name=read", doneEvt.ToolCalls[1])
	}
	if doneEvt.ToolCalls[1].Input["path"] != "/tmp" {
		t.Errorf("ToolCalls[1].Input[path] = %v, want /tmp", doneEvt.ToolCalls[1].Input["path"])
	}
}

// --- HealthCheck tests ---

func TestOpenAIDriver_HealthCheck_Success(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if !strings.HasSuffix(r.URL.Path, "/models") {
			t.Errorf("expected path ending with /models, got %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		fmt.Fprint(w, `{"object":"list","data":[{"id":"gpt-4o","object":"model"}]}`)
	}))
	defer ts.Close()

	d := NewOpenAIDriver("test",
		WithOpenAIBaseURL(ts.URL),
		WithOpenAIKey("sk-test"),
		WithOpenAIHTTPClient(ts.Client()),
	)
	if err := d.HealthCheck(context.Background()); err != nil {
		t.Fatalf("HealthCheck: %v", err)
	}
}

func TestOpenAIDriver_ImplementsHealthChecker(t *testing.T) {
	d := NewOpenAIDriver("test", WithOpenAIKey("sk-test"))
	if _, ok := any(d).(HealthChecker); !ok {
		t.Error("OpenAIDriver should implement HealthChecker")
	}
}

// --- Factory integration tests ---

func TestCreateDriver_OpenAI(t *testing.T) {
	t.Parallel()
	d, err := CreateDriverWithEnv(ProviderConfig{
		Name:         "openai",
		Driver:       DriverOpenAI,
		DefaultModel: "gpt-4o",
		APIKeyEnv:    "OPENAI_API_KEY",
	}, func(key string) string {
		if key == "OPENAI_API_KEY" {
			return "sk-test"
		}
		return ""
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	info := d.Info()
	if info.DriverType != DriverOpenAI {
		t.Errorf("DriverType = %q, want %q", info.DriverType, DriverOpenAI)
	}
	if info.DefaultModel != "gpt-4o" {
		t.Errorf("DefaultModel = %q, want %q", info.DefaultModel, "gpt-4o")
	}
}

func TestCreateDriver_OpenAI_WithBaseURL(t *testing.T) {
	t.Parallel()
	d, err := CreateDriverWithEnv(ProviderConfig{
		Name:         "custom-openai",
		Driver:       DriverOpenAI,
		DefaultModel: "gpt-4o-mini",
		BaseURL:      "https://my-proxy.example.com/v1",
		APIKeyEnv:    "MY_KEY",
	}, func(key string) string {
		if key == "MY_KEY" {
			return "sk-custom"
		}
		return ""
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d == nil {
		t.Fatal("expected non-nil driver")
	}
}

func TestCreateDriver_OpenAI_NoBaseURL(t *testing.T) {
	t.Parallel()
	d, err := CreateDriverWithEnv(ProviderConfig{
		Name:   "openai",
		Driver: DriverOpenAI,
	}, func(string) string { return "" })
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d == nil {
		t.Fatal("expected non-nil driver (base_url optional for openai driver)")
	}
}

// --- Config validation tests ---

func TestConfig_OpenAI_Valid(t *testing.T) {
	cfg := &ProvidersConfig{
		Version: "1",
		Providers: []ProviderConfig{
			{Name: "openai", Driver: DriverOpenAI, DefaultModel: "gpt-4o"},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("unexpected validation error: %v", err)
	}
}

func TestConfig_OpenAI_NoBaseURL_Valid(t *testing.T) {
	cfg := &ProvidersConfig{
		Version: "1",
		Providers: []ProviderConfig{
			{Name: "openai", Driver: DriverOpenAI},
		},
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("expected no error for openai driver without base_url, got: %v", err)
	}
}

// --- extractSDKStatusCode tests ---

type fakeSDKErr struct {
	StatusCode int
	Message    string
}

func (e *fakeSDKErr) Error() string { return fmt.Sprintf("status %d: %s", e.StatusCode, e.Message) }

func TestExtractSDKStatusCode_StructWithField(t *testing.T) {
	err := &fakeSDKErr{StatusCode: 429, Message: "rate limited"}
	if got := extractSDKStatusCode(err); got != 429 {
		t.Errorf("extractSDKStatusCode = %d, want 429", got)
	}
}

func TestExtractSDKStatusCode_NoField(t *testing.T) {
	err := fmt.Errorf("plain error")
	if got := extractSDKStatusCode(err); got != 0 {
		t.Errorf("extractSDKStatusCode = %d, want 0", got)
	}
}

func TestExtractSDKStatusCode_Nil(t *testing.T) {
	if got := extractSDKStatusCode(nil); got != 0 {
		t.Errorf("extractSDKStatusCode = %d, want 0", got)
	}
}

// TestIsLikelyOpenAIOfficial verifies the heuristic used by the factory to
// decide whether to emit the openai_compat suggestion warning. False positives
// merely suppress an informational warning; false negatives only over-warn.
func TestIsLikelyOpenAIOfficial(t *testing.T) {
	cases := []struct {
		baseURL string
		want    bool
	}{
		{"https://api.openai.com/v1", true},
		{"https://API.OpenAI.com/v1", true}, // case-insensitive
		{"https://eastus.api.openai.azure.com/openai/deployments/gpt-4o", true},
		{"https://api.deepseek.com/v1", false},
		{"https://openrouter.ai/api/v1", false},
		{"https://api.x.ai/v1", false},
		{"https://my-proxy.example.com/v1", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := isLikelyOpenAIOfficial(tc.baseURL); got != tc.want {
			t.Errorf("isLikelyOpenAIOfficial(%q) = %v, want %v", tc.baseURL, got, tc.want)
		}
	}
}

// TestCreateDriver_OpenAI_WarnsOnNonOfficialBaseURL verifies the factory
// emits a warning when driver=openai is configured with a non-OpenAI BaseURL,
// pointing users at openai-compat for upstreams that return reasoning.
//
// What the warning is FOR changed on 2026-08-04: a live probe against
// deepseek-v4-flash showed such requests succeed (200, tool calls included)
// with reasoning_content omitted — the old "cryptic HTTP 400" rationale was
// wrong. The real cost is silent data loss: this driver drops the reasoning it
// receives. The assertion below checks "openai-compat" (hyphen) because that is
// the actual config value (see DriverOpenAICompat); the earlier spelling
// "openai_compat" pointed at a driver name no user could type.
func TestCreateDriver_OpenAI_WarnsOnNonOfficialBaseURL(t *testing.T) {
	var buf bytes.Buffer
	prevOut := log.Writer()
	prevFlags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(prevOut)
		log.SetFlags(prevFlags)
	})

	_, err := CreateDriverWithEnv(ProviderConfig{
		Name:    "deepseek-via-openai",
		Driver:  DriverOpenAI,
		BaseURL: "https://api.deepseek.com/v1",
	}, func(string) string { return "" })
	if err != nil {
		t.Fatalf("CreateDriverWithEnv: %v", err)
	}

	got := buf.String()
	if !strings.Contains(got, DriverOpenAICompat) {
		t.Errorf("expected warning to mention %s, got: %q", DriverOpenAICompat, got)
	}
	if !strings.Contains(got, "deepseek-via-openai") {
		t.Errorf("expected warning to mention provider name, got: %q", got)
	}
	if !strings.Contains(got, "api.deepseek.com") {
		t.Errorf("expected warning to mention BaseURL, got: %q", got)
	}
}

// TestCreateDriver_OpenAI_NoWarnOnOfficialBaseURL verifies the warning is
// suppressed for legitimate OpenAI / Azure OpenAI endpoints — the heuristic
// must not nag users running the driver as designed.
func TestCreateDriver_OpenAI_NoWarnOnOfficialBaseURL(t *testing.T) {
	for _, baseURL := range []string{
		"https://api.openai.com/v1",
		"https://eastus.api.openai.azure.com/openai/deployments/gpt-4o",
	} {
		t.Run(baseURL, func(t *testing.T) {
			var buf bytes.Buffer
			prevOut := log.Writer()
			prevFlags := log.Flags()
			log.SetOutput(&buf)
			log.SetFlags(0)
			t.Cleanup(func() {
				log.SetOutput(prevOut)
				log.SetFlags(prevFlags)
			})

			_, err := CreateDriverWithEnv(ProviderConfig{
				Name:    "openai",
				Driver:  DriverOpenAI,
				BaseURL: baseURL,
			}, func(string) string { return "" })
			if err != nil {
				t.Fatalf("CreateDriverWithEnv: %v", err)
			}
			if strings.Contains(buf.String(), DriverOpenAICompat) {
				t.Errorf("unexpected %s warning for %q: %q", DriverOpenAICompat, baseURL, buf.String())
			}
		})
	}
}
