package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Compile-time interface checks.
var (
	_ LLMDriver        = (*OpenAICompatDriver)(nil)
	_ ToolCallingDriver = (*OpenAICompatDriver)(nil)
)

// newTestDriver creates a test server + driver. Returns driver, server (for manual close), and cleanup.
func newTestDriver(handler http.HandlerFunc) (*OpenAICompatDriver, *httptest.Server, func()) {
	ts := httptest.NewServer(handler)
	d := NewOpenAICompatDriver("test", ts.URL,
		WithCompatModel("test-model"),
		WithHTTPClient(ts.Client()),
	)
	return d, ts, ts.Close
}

func TestOpenAICompatDriver_Info(t *testing.T) {
	d := NewOpenAICompatDriver("ollama", "http://localhost:11434/v1",
		WithCompatModel("llama3"),
	)
	info := d.Info()
	if info.Name != "ollama" {
		t.Errorf("Name = %q, want %q", info.Name, "ollama")
	}
	if info.Provider != "ollama" {
		t.Errorf("Provider = %q, want %q", info.Provider, "ollama")
	}
	if info.DefaultModel != "llama3" {
		t.Errorf("DefaultModel = %q, want %q", info.DefaultModel, "llama3")
	}
}

func TestOpenAICompatDriver_Call_Success(t *testing.T) {
	d, _, cleanup := newTestDriver(func(w http.ResponseWriter, r *http.Request) {
		resp := `{"choices":[{"index":0,"message":{"role":"assistant","content":"Hello!"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, resp)
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

func TestOpenAICompatDriver_Call_WithMessages(t *testing.T) {
	var gotBody oaiRequest
	d, _, cleanup := newTestDriver(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &gotBody)
		fmt.Fprint(w, `{"choices":[{"message":{"content":"ok"}}],"usage":{}}`)
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
	if len(gotBody.Messages) != 3 {
		t.Fatalf("Messages len = %d, want 3", len(gotBody.Messages))
	}
	if gotBody.Messages[0].Role != "user" || gotBody.Messages[0].Content != "hello" {
		t.Errorf("Messages[0] = %+v, want role=user content=hello", gotBody.Messages[0])
	}
}

func TestOpenAICompatDriver_Call_IntentFallback(t *testing.T) {
	var gotBody oaiRequest
	d, _, cleanup := newTestDriver(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &gotBody)
		fmt.Fprint(w, `{"choices":[{"message":{"content":"ok"}}],"usage":{}}`)
	})
	defer cleanup()

	_, err := d.Call(context.Background(), LLMRequest{Intent: "what is Go?"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if len(gotBody.Messages) != 1 {
		t.Fatalf("Messages len = %d, want 1", len(gotBody.Messages))
	}
	if gotBody.Messages[0].Role != "user" || gotBody.Messages[0].Content != "what is Go?" {
		t.Errorf("Messages[0] = %+v, want role=user content='what is Go?'", gotBody.Messages[0])
	}
}

func TestOpenAICompatDriver_Call_SystemPrompt(t *testing.T) {
	var gotBody oaiRequest
	d, _, cleanup := newTestDriver(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &gotBody)
		fmt.Fprint(w, `{"choices":[{"message":{"content":"ok"}}],"usage":{}}`)
	})
	defer cleanup()

	_, err := d.Call(context.Background(), LLMRequest{
		Intent:       "hi",
		SystemPrompt: "You are helpful",
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if len(gotBody.Messages) != 2 {
		t.Fatalf("Messages len = %d, want 2", len(gotBody.Messages))
	}
	if gotBody.Messages[0].Role != "system" || gotBody.Messages[0].Content != "You are helpful" {
		t.Errorf("Messages[0] = %+v, want system message", gotBody.Messages[0])
	}
	if gotBody.Messages[1].Role != "user" {
		t.Errorf("Messages[1].Role = %q, want user", gotBody.Messages[1].Role)
	}
}

func TestOpenAICompatDriver_Call_SystemPromptNoDouble(t *testing.T) {
	var gotBody oaiRequest
	d, _, cleanup := newTestDriver(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &gotBody)
		fmt.Fprint(w, `{"choices":[{"message":{"content":"ok"}}],"usage":{}}`)
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
	if len(gotBody.Messages) != 2 {
		t.Fatalf("Messages len = %d, want 2 (no double system)", len(gotBody.Messages))
	}
	if gotBody.Messages[0].Role != "system" || gotBody.Messages[0].Content != "Existing system prompt" {
		t.Errorf("Messages[0] = %+v, want existing system message", gotBody.Messages[0])
	}
}

func TestOpenAICompatDriver_Call_Timeout(t *testing.T) {
	d, _, cleanup := newTestDriver(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(500 * time.Millisecond)
		fmt.Fprint(w, `{"choices":[{"message":{"content":"ok"}}],"usage":{}}`)
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

func TestOpenAICompatDriver_Call_AuthError(t *testing.T) {
	d, _, cleanup := newTestDriver(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		fmt.Fprint(w, `{"error":{"message":"invalid api key","type":"invalid_request_error"}}`)
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

func TestOpenAICompatDriver_Call_RateLimit(t *testing.T) {
	d, _, cleanup := newTestDriver(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		fmt.Fprint(w, `{"error":{"message":"rate limit exceeded"}}`)
	})
	defer cleanup()

	_, err := d.Call(context.Background(), LLMRequest{Intent: "hi"})
	if !errors.Is(err, ErrRateLimit) {
		t.Errorf("error = %v, want ErrRateLimit", err)
	}
}

func TestOpenAICompatDriver_Call_ModelNotFound(t *testing.T) {
	d, _, cleanup := newTestDriver(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		fmt.Fprint(w, `{"error":{"message":"model not found"}}`)
	})
	defer cleanup()

	_, err := d.Call(context.Background(), LLMRequest{Intent: "hi"})
	if !errors.Is(err, ErrModelNotFound) {
		t.Errorf("error = %v, want ErrModelNotFound", err)
	}
}

func TestOpenAICompatDriver_Call_ContextLength(t *testing.T) {
	d, _, cleanup := newTestDriver(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		fmt.Fprint(w, `{"error":{"message":"maximum context length exceeded","code":"context_length_exceeded"}}`)
	})
	defer cleanup()

	_, err := d.Call(context.Background(), LLMRequest{Intent: "hi"})
	if !errors.Is(err, ErrContextLength) {
		t.Errorf("error = %v, want ErrContextLength", err)
	}
}

func TestOpenAICompatDriver_Call_ContextLengthInMessage(t *testing.T) {
	d, _, cleanup := newTestDriver(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		fmt.Fprint(w, `{"error":{"message":"This model maximum context_length is 8192 tokens"}}`)
	})
	defer cleanup()

	_, err := d.Call(context.Background(), LLMRequest{Intent: "hi"})
	if !errors.Is(err, ErrContextLength) {
		t.Errorf("error = %v, want ErrContextLength", err)
	}
}

func TestOpenAICompatDriver_Call_NoAPIKey(t *testing.T) {
	var gotAuth string
	d, _, cleanup := newTestDriver(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"ok"}}],"usage":{}}`)
	})
	defer cleanup()

	_, err := d.Call(context.Background(), LLMRequest{Intent: "hi"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if gotAuth != "" {
		t.Errorf("Authorization = %q, want empty (no apiKey set)", gotAuth)
	}
}

func TestOpenAICompatDriver_Call_TrailingSlash(t *testing.T) {
	var gotPath string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		fmt.Fprint(w, `{"choices":[{"message":{"content":"ok"}}],"usage":{}}`)
	}))
	defer ts.Close()

	d := NewOpenAICompatDriver("test", ts.URL+"/",
		WithCompatModel("m"),
		WithHTTPClient(ts.Client()),
	)
	_, err := d.Call(context.Background(), LLMRequest{Intent: "hi"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if gotPath != "/chat/completions" {
		t.Errorf("path = %q, want /chat/completions (no double slash)", gotPath)
	}
}

func TestOpenAICompatDriver_Call_ToolResultMessage(t *testing.T) {
	var gotBody oaiRequest
	d, _, cleanup := newTestDriver(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &gotBody)
		fmt.Fprint(w, `{"choices":[{"message":{"content":"ok"}}],"usage":{}}`)
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
	toolMsg := gotBody.Messages[2]
	if toolMsg.Role != "tool" || toolMsg.ToolCallID != "call_1" {
		t.Errorf("tool message = %+v, want role=tool tool_call_id=call_1", toolMsg)
	}
}

func TestOpenAICompatDriver_Call_AssistantToolCallsMessage(t *testing.T) {
	var gotBody oaiRequest
	d, _, cleanup := newTestDriver(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &gotBody)
		fmt.Fprint(w, `{"choices":[{"message":{"content":"ok"}}],"usage":{}}`)
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
	assistantMsg := gotBody.Messages[1]
	if len(assistantMsg.ToolCalls) != 1 {
		t.Fatalf("ToolCalls len = %d, want 1", len(assistantMsg.ToolCalls))
	}
	tc := assistantMsg.ToolCalls[0]
	if tc.ID != "call_1" || tc.Function.Name != "get_weather" {
		t.Errorf("ToolCall = %+v, want id=call_1 name=get_weather", tc)
	}
	// Verify arguments is a JSON string
	var args map[string]any
	if err := json.Unmarshal([]byte(tc.Function.Arguments), &args); err != nil {
		t.Fatalf("arguments is not valid JSON: %v", err)
	}
	if args["city"] != "Tokyo" {
		t.Errorf("arguments.city = %v, want Tokyo", args["city"])
	}
}

// --- Stream tests ---

func writeSSE(w http.ResponseWriter, data string) {
	fmt.Fprintf(w, "data: %s\n\n", data)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

func TestOpenAICompatDriver_Stream_Success(t *testing.T) {
	d, _, cleanup := newTestDriver(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		writeSSE(w, `{"choices":[{"delta":{"content":"Hel"}}]}`)
		writeSSE(w, `{"choices":[{"delta":{"content":"lo!"}}]}`)
		writeSSE(w, `{"choices":[{"delta":{},"finish_reason":"stop"}],"usage":{"prompt_tokens":8,"completion_tokens":3,"total_tokens":11}}`)
		writeSSE(w, "[DONE]")
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

func TestOpenAICompatDriver_Stream_HTTPError(t *testing.T) {
	d, _, cleanup := newTestDriver(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		fmt.Fprint(w, `{"error":{"message":"unauthorized"}}`)
	})
	defer cleanup()

	_, err := d.Stream(context.Background(), LLMRequest{Intent: "hi"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrAuth) {
		t.Errorf("error = %v, want ErrAuth", err)
	}
	var llmErr *LLMError
	if !errors.As(err, &llmErr) {
		t.Errorf("error should be *LLMError, got %T", err)
	} else if llmErr.StatusCode != 401 {
		t.Errorf("StatusCode = %d, want 401", llmErr.StatusCode)
	}
}

func TestOpenAICompatDriver_Stream_SystemError(t *testing.T) {
	// Use a closed server to guarantee connection refused
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	closedURL := ts.URL
	ts.Close()

	d := NewOpenAICompatDriver("test", closedURL,
		WithCompatModel("m"),
		WithCompatTimeout(2*time.Second),
	)
	_, err := d.Stream(context.Background(), LLMRequest{Intent: "hi"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// System-level errors should NOT be *LLMError
	var llmErr *LLMError
	if errors.As(err, &llmErr) {
		t.Errorf("system error should not be *LLMError, got %v", err)
	}
}

func TestOpenAICompatDriver_Stream_InvalidSSE(t *testing.T) {
	d, _, cleanup := newTestDriver(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		writeSSE(w, `not-valid-json`)
		writeSSE(w, `{"choices":[{"delta":{"content":"ok"}}]}`)
		writeSSE(w, "[DONE]")
	})
	defer cleanup()

	ch, err := d.Stream(context.Background(), LLMRequest{Intent: "hi"})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	var hasError, hasContent, hasDone bool
	for evt := range ch {
		switch evt.Type {
		case "error":
			hasError = true
		case "content":
			hasContent = true
		case "done":
			hasDone = true
		}
	}
	if !hasError {
		t.Error("expected error event for invalid JSON")
	}
	if !hasContent {
		t.Error("expected content event after error recovery")
	}
	if !hasDone {
		t.Error("expected done event")
	}
}

func TestOpenAICompatDriver_Stream_UsageFromEarlierChunk(t *testing.T) {
	d, _, cleanup := newTestDriver(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		writeSSE(w, `{"choices":[{"delta":{"content":"hi"}}]}`)
		// Usage arrives in a separate chunk before finish
		writeSSE(w, `{"choices":[{"delta":{}}],"usage":{"prompt_tokens":12,"completion_tokens":4,"total_tokens":16}}`)
		writeSSE(w, `{"choices":[{"delta":{},"finish_reason":"stop"}]}`)
		writeSSE(w, "[DONE]")
	})
	defer cleanup()

	ch, err := d.Stream(context.Background(), LLMRequest{Intent: "hi"})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var doneEvt StreamEvent
	for evt := range ch {
		if evt.Type == "done" {
			doneEvt = evt
		}
	}
	if doneEvt.TokensUsed != 16 {
		t.Errorf("TokensUsed = %d, want 16", doneEvt.TokensUsed)
	}
	if doneEvt.InputTokens != 12 {
		t.Errorf("InputTokens = %d, want 12", doneEvt.InputTokens)
	}
}

func TestOpenAICompatDriver_Call_EmptyChoices(t *testing.T) {
	d, _, cleanup := newTestDriver(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"choices":[],"usage":{"prompt_tokens":5,"completion_tokens":0,"total_tokens":5}}`)
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

func TestOpenAICompatDriver_WithStreamUsage(t *testing.T) {
	var gotBody oaiRequest
	d, _, cleanup := newTestDriver(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &gotBody)
		w.Header().Set("Content-Type", "text/event-stream")
		writeSSE(w, `{"choices":[{"delta":{"content":"ok"}}]}`)
		writeSSE(w, "[DONE]")
	})
	defer cleanup()

	// Enable stream usage
	d.streamUsage = true
	ch, err := d.Stream(context.Background(), LLMRequest{Intent: "hi"})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	for range ch {
	}
	if gotBody.StreamOptions == nil || !gotBody.StreamOptions.IncludeUsage {
		t.Error("stream_options.include_usage should be true")
	}
}

func TestOpenAICompatDriver_Stream_LargeChunk(t *testing.T) {
	largeContent := strings.Repeat("x", 100*1024) // 100KB > 64KB default
	d, _, cleanup := newTestDriver(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		writeSSE(w, fmt.Sprintf(`{"choices":[{"delta":{"content":"%s"}}]}`, largeContent))
		writeSSE(w, "[DONE]")
	})
	defer cleanup()

	ch, err := d.Stream(context.Background(), LLMRequest{Intent: "hi"})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	var gotContent string
	for evt := range ch {
		if evt.Type == "content" {
			gotContent += evt.Content
		}
		if evt.Type == "error" {
			t.Fatalf("unexpected error: %v", evt.Err)
		}
	}
	if len(gotContent) != len(largeContent) {
		t.Errorf("content length = %d, want %d", len(gotContent), len(largeContent))
	}
}

// --- ToolCalling tests ---

func TestOpenAICompatDriver_CallWithTools_Success(t *testing.T) {
	d, _, cleanup := newTestDriver(func(w http.ResponseWriter, r *http.Request) {
		var reqBody oaiRequest
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &reqBody)

		// Verify tools were sent
		if len(reqBody.Tools) != 1 || reqBody.Tools[0].Function.Name != "get_weather" {
			t.Errorf("tools not sent correctly: %+v", reqBody.Tools)
		}

		resp := `{"choices":[{"message":{"role":"assistant","content":"","tool_calls":[{"id":"call_abc","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"Tokyo\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":20,"completion_tokens":10,"total_tokens":30}}`
		fmt.Fprint(w, resp)
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

func TestOpenAICompatDriver_CallWithTools_ArgumentsParsing(t *testing.T) {
	d, _, cleanup := newTestDriver(func(w http.ResponseWriter, r *http.Request) {
		resp := `{"choices":[{"message":{"tool_calls":[{"id":"c1","type":"function","function":{"name":"search","arguments":"{\"query\":\"test\",\"limit\":10}"}}]}}],"usage":{}}`
		fmt.Fprint(w, resp)
	})
	defer cleanup()

	resp, err := d.CallWithTools(context.Background(), LLMRequest{Intent: "search"}, []ToolDef{{Name: "search"}})
	if err != nil {
		t.Fatalf("CallWithTools: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("ToolCalls len = %d, want 1", len(resp.ToolCalls))
	}
	input := resp.ToolCalls[0].Input
	if input["query"] != "test" {
		t.Errorf("Input[query] = %v, want test", input["query"])
	}
	// JSON numbers deserialize as float64
	if limit, ok := input["limit"].(float64); !ok || limit != 10 {
		t.Errorf("Input[limit] = %v, want 10", input["limit"])
	}
}

func TestOpenAICompatDriver_StreamWithTools_SingleTool(t *testing.T) {
	d, _, cleanup := newTestDriver(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// First chunk: tool call header with id and name start
		writeSSE(w, `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","type":"function","function":{"name":"get_","arguments":""}}]}}]}`)
		// Second chunk: name continuation + arguments start
		writeSSE(w, `{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"name":"weather","arguments":"{\"ci"}}]}}]}`)
		// Third chunk: arguments continuation
		writeSSE(w, `{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"ty\":\"Tokyo\"}"}}]}}]}`)
		// finish_reason triggers flush
		fr := "tool_calls"
		_ = fr
		writeSSE(w, `{"choices":[{"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":5,"completion_tokens":3,"total_tokens":8}}`)
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
}

func TestOpenAICompatDriver_StreamWithTools_MultiTool(t *testing.T) {
	d, _, cleanup := newTestDriver(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// Interleaved tool calls with different indices
		writeSSE(w, `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"search","arguments":""}}]}}]}`)
		writeSSE(w, `{"choices":[{"delta":{"tool_calls":[{"index":1,"id":"c2","type":"function","function":{"name":"read","arguments":""}}]}}]}`)
		writeSSE(w, `{"choices":[{"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"q\":\"test\"}"}}]}}]}`)
		writeSSE(w, `{"choices":[{"delta":{"tool_calls":[{"index":1,"function":{"arguments":"{\"path\":\"/tmp\"}"}}]}}]}`)
		writeSSE(w, "[DONE]")
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

func TestOpenAICompatDriver_StreamWithTools_FinishReason(t *testing.T) {
	d, _, cleanup := newTestDriver(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		writeSSE(w, `{"choices":[{"delta":{"tool_calls":[{"index":0,"id":"c1","type":"function","function":{"name":"run","arguments":"{}"}}]}}]}`)
		// finish_reason triggers tool calls flush before [DONE]
		writeSSE(w, `{"choices":[{"delta":{},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`)
		// [DONE] comes after but should not generate another event since we already returned
		writeSSE(w, "[DONE]")
	})
	defer cleanup()

	ch, err := d.StreamWithTools(context.Background(), LLMRequest{Intent: "run"}, []ToolDef{{Name: "run"}})
	if err != nil {
		t.Fatalf("StreamWithTools: %v", err)
	}

	var events []StreamEvent
	for evt := range ch {
		events = append(events, evt)
	}
	// Should have exactly one done event (from finish_reason, not from [DONE])
	doneCount := 0
	for _, e := range events {
		if e.Type == "done" {
			doneCount++
			if len(e.ToolCalls) != 1 || e.ToolCalls[0].Name != "run" {
				t.Errorf("done event ToolCalls = %+v, want [{Name:run}]", e.ToolCalls)
			}
			if e.TokensUsed != 15 {
				t.Errorf("TokensUsed = %d, want 15", e.TokensUsed)
			}
		}
		if e.Type == "error" {
			t.Errorf("unexpected error: %v", e.Err)
		}
	}
	if doneCount != 1 {
		t.Errorf("done events = %d, want 1", doneCount)
	}
}
