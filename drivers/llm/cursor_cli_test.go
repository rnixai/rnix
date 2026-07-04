package llm

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// TestCursorHelperProcess is a mock process used by tests to simulate the Cursor CLI.
// It checks for GO_TEST_PROCESS=1 and dispatches based on GO_TEST_CASE.
func TestCursorHelperProcess(t *testing.T) {
	if os.Getenv("GO_TEST_PROCESS") != "1" {
		return
	}
	switch os.Getenv("GO_TEST_CASE") {
	case "cursor_success":
		fmt.Fprint(os.Stdout, `{"type":"result","subtype":"success","result":"cursor output","cost_usd":0.002,"is_error":false,"duration_ms":150,"num_turns":1,"input_tokens":90,"output_tokens":30}`)
	case "cursor_is_error":
		fmt.Fprint(os.Stdout, `{"type":"result","subtype":"error","result":"cursor error message","is_error":true}`)
	case "cursor_cli_error":
		fmt.Fprint(os.Stderr, "Error: invalid arguments")
		os.Exit(1)
	case "cursor_invalid_json":
		fmt.Fprint(os.Stdout, "not json at all")
	case "cursor_timeout":
		time.Sleep(5 * time.Second)
	case "cursor_args_echo":
		fmt.Fprint(os.Stderr, strings.Join(os.Args, " "))
		fmt.Fprint(os.Stdout, `{"type":"result","subtype":"success","result":"ok","is_error":false}`)
	case "cursor_stream_success":
		fmt.Fprintln(os.Stdout, `{"type":"system","subtype":"init","model":"gpt-4"}`)
		fmt.Fprintln(os.Stdout, `{"type":"user","message":{"content":[{"type":"text","text":"inspect go.mod"}]}}`)
		fmt.Fprintln(os.Stdout, `{"type":"assistant","message":{"content":[{"type":"text","text":"hello "}]}}`)
		fmt.Fprintln(os.Stdout, `{"type":"tool_call","subtype":"started"}`)
		fmt.Fprintln(os.Stdout, `{"type":"tool_call","subtype":"completed"}`)
		fmt.Fprintln(os.Stdout, `{"type":"assistant","message":{"content":[{"type":"text","text":"world"},{"type":"tool_use","id":"toolu_1","name":"read_file","input":{"path":"go.mod"}}]}}`)
		fmt.Fprintln(os.Stdout, `{"type":"result","subtype":"success","result":"hello world","is_error":false,"num_turns":1,"input_tokens":80,"output_tokens":40}`)
	case "cursor_stream_error":
		fmt.Fprintln(os.Stdout, `{"type":"result","subtype":"error","result":"stream error message","is_error":true}`)
	case "cursor_empty_result":
		fmt.Fprint(os.Stdout, `{"type":"result","subtype":"success","result":"","is_error":false}`)
	case "cursor_not_authenticated":
		fmt.Fprint(os.Stdout, `{"type":"result","subtype":"error","result":"authentication key invalid","is_error":true}`)
	case "cursor_stream_timeout":
		time.Sleep(5 * time.Second)
	case "cursor_stream_is_error_empty":
		fmt.Fprintln(os.Stdout, `{"type":"result","subtype":"error","result":"","is_error":true}`)
	case "cursor_exit1_with_json":
		fmt.Fprint(os.Stdout, `{"type":"result","subtype":"error","result":"API rate limited","is_error":true}`)
		os.Exit(1)
	case "cursor_exit1_no_json":
		fmt.Fprint(os.Stderr, "Error: network failure")
		os.Exit(1)
	case "cursor_exit1_valid_result":
		fmt.Fprint(os.Stdout, `{"type":"result","subtype":"success","result":"partial output","is_error":false,"num_turns":1,"input_tokens":60,"output_tokens":40}`)
		os.Exit(1)
	case "cursor_stream_with_thinking":
		fmt.Fprintln(os.Stdout, `{"type":"system","subtype":"init","model":"gpt-4"}`)
		fmt.Fprintln(os.Stdout, `{"type":"user","message":{"content":[{"type":"text","text":"test"}]}}`)
		fmt.Fprintln(os.Stdout, `{"type":"thinking","subtype":"delta","text":"analyzing..."}`)
		fmt.Fprintln(os.Stdout, `{"type":"thinking","subtype":"completed"}`)
		fmt.Fprintln(os.Stdout, `{"type":"assistant","message":{"content":[{"type":"text","text":"result"}]}}`)
		fmt.Fprintln(os.Stdout, `{"type":"result","subtype":"success","result":"result","is_error":false,"input_tokens":50,"output_tokens":20}`)
	}
	os.Exit(0)
}

func cursorMockCmdBuilder(testCase string) CommandBuilder {
	return func(ctx context.Context, name string, args ...string) *exec.Cmd {
		cs := []string{"-test.run=TestCursorHelperProcess", "--"}
		cs = append(cs, args...)
		cmd := exec.CommandContext(ctx, os.Args[0], cs...)
		cmd.Env = append(os.Environ(), "GO_TEST_PROCESS=1", "GO_TEST_CASE="+testCase)
		return cmd
	}
}

func TestCursorCliDriver_Call_Success(t *testing.T) {
	t.Parallel()
	d := NewCursorCliDriver(CursorWithCommandBuilder(cursorMockCmdBuilder("cursor_success")))
	resp, err := d.Call(context.Background(), LLMRequest{Intent: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "cursor output" {
		t.Errorf("expected content 'cursor output', got %q", resp.Content)
	}
	if resp.TokensUsed != 120 {
		t.Errorf("expected tokens_used 120, got %d", resp.TokensUsed)
	}
	if resp.InputTokens != 90 {
		t.Errorf("expected input_tokens 90, got %d", resp.InputTokens)
	}
	if resp.OutputTokens != 30 {
		t.Errorf("expected output_tokens 30, got %d", resp.OutputTokens)
	}
}

func TestCursorCliDriver_Call_Timeout(t *testing.T) {
	t.Parallel()
	d := NewCursorCliDriver(
		CursorWithCommandBuilder(cursorMockCmdBuilder("cursor_timeout")),
		CursorWithTimeout(200*time.Millisecond),
	)
	_, err := d.Call(context.Background(), LLMRequest{Intent: "test"})
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
	if !errors.Is(err, ErrTimeout) {
		t.Errorf("expected errors.Is(err, ErrTimeout), got: %v", err)
	}
	var llmErr *LLMError
	if !errors.As(err, &llmErr) {
		t.Fatal("expected errors.As to extract *LLMError")
	}
	if llmErr.Provider != "cursor" {
		t.Errorf("Provider = %q, want %q", llmErr.Provider, "cursor")
	}
}

func TestCursorCliDriver_Call_CLIError(t *testing.T) {
	t.Parallel()
	d := NewCursorCliDriver(CursorWithCommandBuilder(cursorMockCmdBuilder("cursor_cli_error")))
	_, err := d.Call(context.Background(), LLMRequest{Intent: "test"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var llmErr *LLMError
	if !errors.As(err, &llmErr) {
		t.Fatal("expected errors.As to extract *LLMError")
	}
	if llmErr.Provider != "cursor" {
		t.Errorf("Provider = %q, want %q", llmErr.Provider, "cursor")
	}
	if !strings.Contains(llmErr.Error(), "cli failed") {
		t.Errorf("expected 'cli failed' in error, got: %v", llmErr)
	}
	if !strings.Contains(llmErr.Error(), "Error: invalid arguments") {
		t.Errorf("expected stderr content in error, got: %v", llmErr)
	}
}

func TestCursorCliDriver_Call_IsError(t *testing.T) {
	t.Parallel()
	d := NewCursorCliDriver(CursorWithCommandBuilder(cursorMockCmdBuilder("cursor_is_error")))
	_, err := d.Call(context.Background(), LLMRequest{Intent: "test"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var llmErr *LLMError
	if !errors.As(err, &llmErr) {
		t.Fatal("expected errors.As to extract *LLMError")
	}
	if llmErr.Provider != "cursor" {
		t.Errorf("Provider = %q, want %q", llmErr.Provider, "cursor")
	}
	if !strings.Contains(llmErr.Error(), "cursor error message") {
		t.Errorf("expected error content, got: %v", llmErr)
	}
}

func TestCursorCliDriver_Call_InvalidJSON(t *testing.T) {
	t.Parallel()
	d := NewCursorCliDriver(CursorWithCommandBuilder(cursorMockCmdBuilder("cursor_invalid_json")))
	_, err := d.Call(context.Background(), LLMRequest{Intent: "test"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var llmErr *LLMError
	if !errors.As(err, &llmErr) {
		t.Fatal("expected errors.As to extract *LLMError")
	}
	if !strings.Contains(llmErr.Error(), "invalid json") {
		t.Errorf("expected 'invalid json' in error, got: %v", llmErr)
	}
}

func TestCursorCliDriver_Call_Args(t *testing.T) {
	t.Parallel()
	var capturedArgs []string
	d := NewCursorCliDriver(CursorWithCommandBuilder(func(ctx context.Context, name string, args ...string) *exec.Cmd {
		capturedArgs = args
		return cursorMockCmdBuilder("cursor_success")(ctx, name, args...)
	}))

	_, err := d.Call(context.Background(), LLMRequest{
		Intent:       "analyze code",
		SystemPrompt: "you are an expert",
		Model:        "gpt-4",
		MaxTurns:     3,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	argsStr := strings.Join(capturedArgs, " ")

	// --print must be present
	if !strings.Contains(argsStr, "--print") {
		t.Errorf("expected --print flag, got: %s", argsStr)
	}

	// --force --trust --approve-mcps must be present
	if !strings.Contains(argsStr, "--force") {
		t.Errorf("expected --force flag, got: %s", argsStr)
	}
	if !strings.Contains(argsStr, "--trust") {
		t.Errorf("expected --trust flag, got: %s", argsStr)
	}
	if !strings.Contains(argsStr, "--approve-mcps") {
		t.Errorf("expected --approve-mcps flag, got: %s", argsStr)
	}

	// --model gpt-4
	if !strings.Contains(argsStr, "--model gpt-4") {
		t.Errorf("expected --model gpt-4, got: %s", argsStr)
	}

	// --max-turns must NOT be present (Cursor CLI doesn't support it)
	if strings.Contains(argsStr, "--max-turns") {
		t.Errorf("expected no --max-turns in args, got: %s", argsStr)
	}

	// System prompt must be prefixed to prompt
	lastArg := capturedArgs[len(capturedArgs)-1]
	if !strings.Contains(lastArg, "[System Instructions]") {
		t.Errorf("expected system prompt prefix in last arg, got: %s", lastArg)
	}
	if !strings.Contains(lastArg, "you are an expert") {
		t.Errorf("expected system prompt content in last arg, got: %s", lastArg)
	}
	if !strings.Contains(lastArg, "[End System Instructions]") {
		t.Errorf("expected system prompt suffix in last arg, got: %s", lastArg)
	}
	if !strings.Contains(lastArg, "analyze code") {
		t.Errorf("expected intent in last arg, got: %s", lastArg)
	}

	// Prompt must be the last positional argument
	if lastArg == "--model" || lastArg == "--approve-mcps" {
		t.Errorf("prompt should be last arg, got: %s", lastArg)
	}
}

func TestCursorCliDriver_Call_DefaultArgs(t *testing.T) {
	t.Parallel()
	var capturedArgs []string
	d := NewCursorCliDriver(CursorWithCommandBuilder(func(ctx context.Context, name string, args ...string) *exec.Cmd {
		capturedArgs = args
		return cursorMockCmdBuilder("cursor_success")(ctx, name, args...)
	}))

	_, err := d.Call(context.Background(), LLMRequest{Intent: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	argsStr := strings.Join(capturedArgs, " ")
	// Default model is empty, so --model should NOT be present
	if strings.Contains(argsStr, "--model") {
		t.Errorf("expected no --model in default args, got: %s", argsStr)
	}
	// --max-turns should NOT be present
	if strings.Contains(argsStr, "--max-turns") {
		t.Errorf("expected no --max-turns in default args, got: %s", argsStr)
	}
	// --system-prompt flag should NOT be present (Cursor CLI doesn't have it)
	if strings.Contains(argsStr, "--system-prompt") {
		t.Errorf("unexpected --system-prompt flag, got: %s", argsStr)
	}
	// Prompt should be the last argument
	lastArg := capturedArgs[len(capturedArgs)-1]
	if lastArg != "test" {
		t.Errorf("expected prompt 'test' as last arg, got: %s", lastArg)
	}
}

func TestCursorCliDriver_Call_EmptyResult(t *testing.T) {
	t.Parallel()
	d := NewCursorCliDriver(CursorWithCommandBuilder(cursorMockCmdBuilder("cursor_empty_result")))
	_, err := d.Call(context.Background(), LLMRequest{Intent: "test"})
	if err == nil {
		t.Fatal("expected error for empty result, got nil")
	}
	var llmErr *LLMError
	if !errors.As(err, &llmErr) {
		t.Fatal("expected errors.As to extract *LLMError")
	}
	if !strings.Contains(llmErr.Error(), "truncated") {
		t.Errorf("expected truncation error, got: %v", llmErr)
	}
}

func TestCursorCliDriver_Call_NotAuthenticated(t *testing.T) {
	t.Parallel()
	d := NewCursorCliDriver(CursorWithCommandBuilder(cursorMockCmdBuilder("cursor_not_authenticated")))
	_, err := d.Call(context.Background(), LLMRequest{Intent: "test"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrAuth) {
		t.Errorf("expected errors.Is(err, ErrAuth), got: %v", err)
	}
	var llmErr *LLMError
	if !errors.As(err, &llmErr) {
		t.Fatal("expected errors.As to extract *LLMError")
	}
	if llmErr.Provider != "cursor" {
		t.Errorf("Provider = %q, want %q", llmErr.Provider, "cursor")
	}
	if llmErr.StatusCode != 401 {
		t.Errorf("StatusCode = %d, want 401", llmErr.StatusCode)
	}
}

func TestCursorCliDriver_Stream_Success(t *testing.T) {
	t.Parallel()
	d := NewCursorCliDriver(CursorWithCommandBuilder(cursorMockCmdBuilder("cursor_stream_success")))
	ch, err := d.Stream(context.Background(), LLMRequest{Intent: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var events []StreamEvent
	for evt := range ch {
		events = append(events, evt)
	}

	// Should have 9 events: 1 system + 1 user + 2 assistant + 2 content
	// (from assistant) + 2 tool_call (started/completed) + 1 done (from result).
	// system events are now forwarded; tool_call events are forwarded
	if len(events) != 9 {
		t.Fatalf("expected 9 events (system/user/assistant/content/tool/done), got %d: %+v", len(events), events)
	}

	if events[0].Type != "system" || events[0].Content != "init" {
		t.Errorf("event[0]: expected system 'init', got type=%q content=%q", events[0].Type, events[0].Content)
	}
	if events[1].Type != "user" {
		t.Fatalf("event[1]: expected user, got type=%q content=%q data=%v", events[1].Type, events[1].Content, events[1].Data)
	}
	if events[1].Data["role"] != "user" {
		t.Errorf("event[1]: expected role=user, got %v", events[1].Data["role"])
	}
	userContent, ok := events[1].Data["content"].([]map[string]any)
	if !ok || len(userContent) != 1 || userContent[0]["text"] != "inspect go.mod" {
		t.Errorf("event[1]: expected user message content, got %#v", events[1].Data["content"])
	}
	if events[2].Type != "assistant" {
		t.Fatalf("event[2]: expected assistant, got type=%q content=%q data=%v", events[2].Type, events[2].Content, events[2].Data)
	}
	if events[2].Data["role"] != "assistant" {
		t.Errorf("event[2]: expected role=assistant, got %v", events[2].Data["role"])
	}
	if events[3].Type != "content" || events[3].Content != "hello " {
		t.Errorf("event[3]: expected content 'hello ', got type=%q content=%q", events[3].Type, events[3].Content)
	}
	if events[4].Type != "tool_call" || events[4].Content != "started" {
		t.Errorf("event[4]: expected tool_call 'started', got type=%q content=%q", events[4].Type, events[4].Content)
	}
	if events[5].Type != "tool_call" || events[5].Content != "completed" {
		t.Errorf("event[5]: expected tool_call 'completed', got type=%q content=%q", events[5].Type, events[5].Content)
	}
	if events[6].Type != "assistant" {
		t.Fatalf("event[6]: expected assistant, got type=%q content=%q data=%v", events[6].Type, events[6].Content, events[6].Data)
	}
	assistantContent, ok := events[6].Data["content"].([]map[string]any)
	if !ok || len(assistantContent) != 2 {
		t.Fatalf("event[6]: expected two assistant content blocks, got %#v", events[6].Data["content"])
	}
	if assistantContent[0]["text"] != "world" {
		t.Errorf("event[6]: expected text block 'world', got %#v", assistantContent[0])
	}
	if assistantContent[1]["type"] != "tool_use" || assistantContent[1]["id"] != "toolu_1" || assistantContent[1]["name"] != "read_file" {
		t.Errorf("event[6]: expected tool_use block, got %#v", assistantContent[1])
	}
	if events[7].Type != "content" || events[7].Content != "world" {
		t.Errorf("event[7]: expected content 'world', got type=%q content=%q", events[7].Type, events[7].Content)
	}
	if events[8].Type != "done" || events[8].Content != "hello world" {
		t.Errorf("event[8]: expected done 'hello world', got type=%q content=%q", events[8].Type, events[8].Content)
	}
	if events[8].TokensUsed != 120 {
		t.Errorf("expected tokens_used 120, got %d", events[8].TokensUsed)
	}
}

func TestCursorCliDriver_Stream_Error(t *testing.T) {
	t.Parallel()
	d := NewCursorCliDriver(CursorWithCommandBuilder(cursorMockCmdBuilder("cursor_stream_error")))
	ch, err := d.Stream(context.Background(), LLMRequest{Intent: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var events []StreamEvent
	for evt := range ch {
		events = append(events, evt)
	}

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != "error" {
		t.Errorf("expected error event, got type=%q", events[0].Type)
	}
	if events[0].Err == nil {
		t.Error("expected non-nil error")
	}
	var llmErr *LLMError
	if !errors.As(events[0].Err, &llmErr) {
		t.Fatal("expected errors.As to extract *LLMError from StreamEvent.Err")
	}
	if llmErr.Provider != "cursor" {
		t.Errorf("Provider = %q, want %q", llmErr.Provider, "cursor")
	}
	if !strings.Contains(llmErr.Error(), "stream error message") {
		t.Errorf("expected error content, got: %v", llmErr)
	}
}

func TestCursorCliDriver_Stream_Timeout(t *testing.T) {
	t.Parallel()
	d := NewCursorCliDriver(
		CursorWithCommandBuilder(cursorMockCmdBuilder("cursor_stream_timeout")),
		CursorWithTimeout(200*time.Millisecond),
	)
	ch, err := d.Stream(context.Background(), LLMRequest{Intent: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var events []StreamEvent
	for evt := range ch {
		events = append(events, evt)
	}

	// Channel should close cleanly after timeout; no goroutine leak
	// The stream goroutine exits when context is cancelled
	if len(events) > 0 {
		// If any events, they should be error events (from scanner error on killed process)
		for _, evt := range events {
			if evt.Type != "error" {
				t.Errorf("expected only error events after timeout, got type=%q", evt.Type)
			}
		}
	}
}

func TestCursorCliDriver_Info(t *testing.T) {
	t.Parallel()
	d := NewCursorCliDriver(CursorWithModel("gpt-4"))
	info := d.Info()
	if info.Name != "cursor-cli" {
		t.Errorf("expected name 'cursor-cli', got %q", info.Name)
	}
	if info.Provider != "cursor" {
		t.Errorf("expected provider 'cursor', got %q", info.Provider)
	}
	if info.DefaultModel != "gpt-4" {
		t.Errorf("expected default model 'gpt-4', got %q", info.DefaultModel)
	}
}

func TestCursorCliDriver_Options(t *testing.T) {
	t.Parallel()
	t.Run("CursorWithModel", func(t *testing.T) {
		t.Parallel()
		d := NewCursorCliDriver(CursorWithModel("claude-3.5-sonnet"))
		if d.defaultModel != "claude-3.5-sonnet" {
			t.Errorf("expected model 'claude-3.5-sonnet', got %q", d.defaultModel)
		}
	})

	t.Run("CursorWithTimeout", func(t *testing.T) {
		t.Parallel()
		d := NewCursorCliDriver(CursorWithTimeout(60 * time.Second))
		if d.defaultTimeout != 60*time.Second {
			t.Errorf("expected timeout 60s, got %v", d.defaultTimeout)
		}
	})

	t.Run("CursorWithCommandBuilder", func(t *testing.T) {
		t.Parallel()
		called := false
		cb := func(ctx context.Context, name string, args ...string) *exec.Cmd {
			called = true
			return cursorMockCmdBuilder("cursor_success")(ctx, name, args...)
		}
		d := NewCursorCliDriver(CursorWithCommandBuilder(cb))
		_, _ = d.Call(context.Background(), LLMRequest{Intent: "test"})
		if !called {
			t.Error("custom command builder was not called")
		}
	})
}

func TestCursorCliDriver_Call_ExitCodeWithJSON(t *testing.T) {
	t.Parallel()
	d := NewCursorCliDriver(CursorWithCommandBuilder(cursorMockCmdBuilder("cursor_exit1_with_json")))
	_, err := d.Call(context.Background(), LLMRequest{Intent: "test"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrRateLimit) {
		t.Errorf("expected errors.Is(err, ErrRateLimit), got: %v", err)
	}
	var llmErr *LLMError
	if !errors.As(err, &llmErr) {
		t.Fatal("expected errors.As to extract *LLMError")
	}
	if llmErr.StatusCode != 429 {
		t.Errorf("StatusCode = %d, want 429", llmErr.StatusCode)
	}
	if llmErr.Provider != "cursor" {
		t.Errorf("Provider = %q, want %q", llmErr.Provider, "cursor")
	}
}

func TestCursorCliDriver_Call_ExitCodeNoJSON(t *testing.T) {
	t.Parallel()
	d := NewCursorCliDriver(CursorWithCommandBuilder(cursorMockCmdBuilder("cursor_exit1_no_json")))
	_, err := d.Call(context.Background(), LLMRequest{Intent: "test"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var llmErr *LLMError
	if !errors.As(err, &llmErr) {
		t.Fatal("expected errors.As to extract *LLMError")
	}
	if !strings.Contains(llmErr.Error(), "cli failed") {
		t.Errorf("expected 'cli failed', got: %v", llmErr)
	}
	if !strings.Contains(llmErr.Error(), "Error: network failure") {
		t.Errorf("expected stderr content in error, got: %v", llmErr)
	}
}

func TestCursorCliDriver_Call_ExitCodeWithValidResult(t *testing.T) {
	t.Parallel()
	d := NewCursorCliDriver(CursorWithCommandBuilder(cursorMockCmdBuilder("cursor_exit1_valid_result")))
	resp, err := d.Call(context.Background(), LLMRequest{Intent: "test"})
	if err != nil {
		t.Fatalf("expected success despite exit code 1, got error: %v", err)
	}
	if resp.Content != "partial output" {
		t.Errorf("expected 'partial output', got %q", resp.Content)
	}
	if resp.TokensUsed != 100 {
		t.Errorf("expected tokens_used 100, got %d", resp.TokensUsed)
	}
}

func TestCursorCliDriver_Stream_IsErrorEmptyResult(t *testing.T) {
	t.Parallel()
	d := NewCursorCliDriver(CursorWithCommandBuilder(cursorMockCmdBuilder("cursor_stream_is_error_empty")))
	ch, err := d.Stream(context.Background(), LLMRequest{Intent: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var events []StreamEvent
	for evt := range ch {
		events = append(events, evt)
	}

	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	if events[0].Type != "error" {
		t.Fatalf("expected error event, got type=%q", events[0].Type)
	}
	var llmErr *LLMError
	if !errors.As(events[0].Err, &llmErr) {
		t.Fatal("expected errors.As to extract *LLMError")
	}
	if !strings.Contains(llmErr.Error(), "unknown error") {
		t.Errorf("expected 'unknown error' fallback, got: %v", llmErr)
	}
}

func TestCursorCliDriver_Stream_ThinkingEvents(t *testing.T) {
	t.Parallel()
	d := NewCursorCliDriver(CursorWithCommandBuilder(cursorMockCmdBuilder("cursor_stream_with_thinking")))
	ch, err := d.Stream(context.Background(), LLMRequest{Intent: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var events []StreamEvent
	for evt := range ch {
		events = append(events, evt)
	}

	typeSeq := make([]string, len(events))
	for i, e := range events {
		typeSeq[i] = e.Type
	}

	// Verify thinking events
	thinkingCount := 0
	for _, e := range events {
		if e.Type == "thinking" {
			thinkingCount++
			if e.Content == "analyzing..." {
				if e.Data["subtype"] != "delta" {
					t.Errorf("expected thinking delta subtype, got %v", e.Data["subtype"])
				}
			}
		}
	}
	if thinkingCount != 2 {
		t.Errorf("expected 2 thinking events (delta+completed), got %d, types: %v", thinkingCount, typeSeq)
	}

	// Verify user event
	found := false
	for _, e := range events {
		if e.Type == "user" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected user event, got types: %v", typeSeq)
	}

	// Verify done event at end
	last := events[len(events)-1]
	if last.Type != "done" {
		t.Errorf("expected last event to be done, got %q", last.Type)
	}
}
