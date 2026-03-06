//go:build manual

// Manual verification tests for OpenAICompatDriver against a real endpoint.
//
// Usage:
//   # Ollama (no key needed):
//   COMPAT_BASE_URL=http://localhost:11434/v1 COMPAT_MODEL=qwen2.5:0.5b go test ./drivers/llm/ -run TestManual -tags manual -v
//
//   # Groq:
//   COMPAT_BASE_URL=https://api.groq.com/openai/v1 COMPAT_API_KEY=gsk_xxx COMPAT_MODEL=llama-3.3-70b-versatile go test ./drivers/llm/ -run TestManual -tags manual -v
//
//   # DeepSeek:
//   COMPAT_BASE_URL=https://api.deepseek.com/v1 COMPAT_API_KEY=sk-xxx COMPAT_MODEL=deepseek-chat go test ./drivers/llm/ -run TestManual -tags manual -v
//
//   # OpenAI:
//   COMPAT_BASE_URL=https://api.openai.com/v1 COMPAT_API_KEY=sk-xxx COMPAT_MODEL=gpt-4o-mini go test ./drivers/llm/ -run TestManual -tags manual -v

package llm

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func manualDriver(t *testing.T) *OpenAICompatDriver {
	t.Helper()
	baseURL := os.Getenv("COMPAT_BASE_URL")
	if baseURL == "" {
		t.Skip("COMPAT_BASE_URL not set, skipping manual test")
	}
	model := os.Getenv("COMPAT_MODEL")
	if model == "" {
		t.Skip("COMPAT_MODEL not set, skipping manual test")
	}

	opts := []CompatOption{
		WithCompatModel(model),
		WithCompatTimeout(30 * time.Second),
	}
	if key := os.Getenv("COMPAT_API_KEY"); key != "" {
		opts = append(opts, WithAPIKey(key))
	}
	return NewOpenAICompatDriver("manual-test", baseURL, opts...)
}

func TestManual_Call(t *testing.T) {
	d := manualDriver(t)
	resp, err := d.Call(context.Background(), LLMRequest{
		Intent: "Reply with exactly: HELLO RNIX",
	})
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}
	fmt.Printf("Content:      %s\n", resp.Content)
	fmt.Printf("TokensUsed:   %d\n", resp.TokensUsed)
	fmt.Printf("InputTokens:  %d\n", resp.InputTokens)
	fmt.Printf("OutputTokens: %d\n", resp.OutputTokens)

	if !strings.Contains(strings.ToUpper(resp.Content), "HELLO") {
		t.Errorf("unexpected content: %s", resp.Content)
	}
}

func TestManual_Call_WithMessages(t *testing.T) {
	d := manualDriver(t)
	resp, err := d.Call(context.Background(), LLMRequest{
		SystemPrompt: "You are a calculator. Only reply with numbers.",
		Messages: []Message{
			{Role: "user", Content: "What is 2+3?"},
		},
	})
	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}
	fmt.Printf("Content: %s\n", resp.Content)
	if !strings.Contains(resp.Content, "5") {
		t.Errorf("expected answer containing '5', got: %s", resp.Content)
	}
}

func TestManual_Stream(t *testing.T) {
	d := manualDriver(t)
	ch, err := d.Stream(context.Background(), LLMRequest{
		Intent: "Count from 1 to 5, one number per line.",
	})
	if err != nil {
		t.Fatalf("Stream failed: %v", err)
	}

	var full strings.Builder
	for evt := range ch {
		switch evt.Type {
		case "content":
			fmt.Print(evt.Content)
			full.WriteString(evt.Content)
		case "done":
			fmt.Printf("\n--- done (tokens: %d, in: %d, out: %d) ---\n",
				evt.TokensUsed, evt.InputTokens, evt.OutputTokens)
		case "error":
			t.Fatalf("stream error: %v", evt.Err)
		}
	}
	if full.Len() == 0 {
		t.Error("received no content from stream")
	}
}

func TestManual_CallWithTools(t *testing.T) {
	d := manualDriver(t)
	tools := []ToolDef{
		{
			Name:        "get_weather",
			Description: "Get the current weather for a city",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"city": map[string]any{
						"type":        "string",
						"description": "City name",
					},
				},
				"required": []any{"city"},
			},
		},
	}

	resp, err := d.CallWithTools(context.Background(), LLMRequest{
		Intent: "What's the weather in Tokyo?",
	}, tools)
	if err != nil {
		t.Fatalf("CallWithTools failed: %v", err)
	}

	fmt.Printf("Content:   %q\n", resp.Content)
	fmt.Printf("ToolCalls: %d\n", len(resp.ToolCalls))
	for i, tc := range resp.ToolCalls {
		fmt.Printf("  [%d] ID=%s Name=%s Input=%v\n", i, tc.ID, tc.Name, tc.Input)
	}

	if len(resp.ToolCalls) == 0 {
		t.Error("expected at least one tool call")
	} else if resp.ToolCalls[0].Name != "get_weather" {
		t.Errorf("expected tool call name 'get_weather', got %q", resp.ToolCalls[0].Name)
	}
}

func TestManual_StreamWithTools(t *testing.T) {
	d := manualDriver(t)
	tools := []ToolDef{
		{
			Name:        "search",
			Description: "Search the web",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{"type": "string"},
				},
				"required": []any{"query"},
			},
		},
	}

	ch, err := d.StreamWithTools(context.Background(), LLMRequest{
		Intent: "Search for 'Rnix agent OS'",
	}, tools)
	if err != nil {
		t.Fatalf("StreamWithTools failed: %v", err)
	}

	for evt := range ch {
		switch evt.Type {
		case "content":
			fmt.Print(evt.Content)
		case "done":
			fmt.Printf("\n--- done (tokens: %d) ---\n", evt.TokensUsed)
			if len(evt.ToolCalls) > 0 {
				for i, tc := range evt.ToolCalls {
					fmt.Printf("  ToolCall[%d]: ID=%s Name=%s Input=%v\n", i, tc.ID, tc.Name, tc.Input)
				}
			}
		case "error":
			t.Fatalf("stream error: %v", evt.Err)
		}
	}
}

func TestManual_Info(t *testing.T) {
	d := manualDriver(t)
	info := d.Info()
	fmt.Printf("Name:         %s\n", info.Name)
	fmt.Printf("Provider:     %s\n", info.Provider)
	fmt.Printf("DefaultModel: %s\n", info.DefaultModel)
}
