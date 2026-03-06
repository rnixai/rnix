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

// TestHelperProcess is a mock process used by tests to simulate the Claude CLI.
// It checks for GO_TEST_PROCESS=1 and dispatches based on GO_TEST_CASE.
func TestHelperProcess(t *testing.T) {
	if os.Getenv("GO_TEST_PROCESS") != "1" {
		return
	}
	switch os.Getenv("GO_TEST_CASE") {
	case "success":
		fmt.Fprint(os.Stdout, `{"type":"result","subtype":"success","result":"test output","cost_usd":0.001,"is_error":false,"duration_ms":100,"num_turns":1,"input_tokens":80,"output_tokens":20}`)
	case "is_error":
		fmt.Fprint(os.Stdout, `{"type":"result","subtype":"error","result":"LLM error message","is_error":true}`)
	case "cli_error":
		fmt.Fprint(os.Stderr, "Error: invalid arguments")
		os.Exit(1)
	case "invalid_json":
		fmt.Fprint(os.Stdout, "not json at all")
	case "timeout":
		time.Sleep(5 * time.Second)
	case "args_echo":
		fmt.Fprint(os.Stderr, strings.Join(os.Args, " "))
		fmt.Fprint(os.Stdout, `{"type":"result","subtype":"success","result":"ok","is_error":false}`)
	case "stream_success":
		fmt.Fprintln(os.Stdout, `{"type":"assistant","message":{"content":[{"type":"text","text":"hello "}]}}`)
		fmt.Fprintln(os.Stdout, `{"type":"assistant","message":{"content":[{"type":"text","text":"world"}]}}`)
		fmt.Fprintln(os.Stdout, `{"type":"result","subtype":"success","result":"hello world","is_error":false,"num_turns":1,"input_tokens":70,"output_tokens":30}`)
	case "stream_error":
		fmt.Fprintln(os.Stdout, `{"type":"result","subtype":"error","result":"stream error message","is_error":true}`)
	case "exit1_with_json":
		fmt.Fprint(os.Stdout, `{"type":"result","subtype":"error","result":"API rate limited","is_error":true}`)
		os.Exit(1)
	case "exit1_no_json":
		fmt.Fprint(os.Stderr, "Error: network failure")
		os.Exit(1)
	case "empty_result":
		fmt.Fprint(os.Stdout, `{"type":"result","subtype":"success","result":"","is_error":false}`)
	case "stream_empty_result":
		fmt.Fprintln(os.Stdout, `{"type":"result","subtype":"success","result":"","is_error":false}`)
	case "is_error_empty_result":
		fmt.Fprint(os.Stdout, `{"type":"result","subtype":"error","result":"","is_error":true}`)
	case "exit1_valid_result":
		fmt.Fprint(os.Stdout, `{"type":"result","subtype":"success","result":"partial output","is_error":false,"num_turns":1,"input_tokens":60,"output_tokens":40}`)
		os.Exit(1)
	case "stream_is_error_empty":
		fmt.Fprintln(os.Stdout, `{"type":"result","subtype":"error","result":"","is_error":true}`)
	}
	os.Exit(0)
}

func mockCmdBuilder(testCase string) CommandBuilder {
	return func(ctx context.Context, name string, args ...string) *exec.Cmd {
		cs := []string{"-test.run=TestHelperProcess", "--"}
		cs = append(cs, args...)
		cmd := exec.CommandContext(ctx, os.Args[0], cs...)
		cmd.Env = append(os.Environ(), "GO_TEST_PROCESS=1", "GO_TEST_CASE="+testCase)
		return cmd
	}
}

func TestClaudeCliDriver_Call_Success(t *testing.T) {
	d := NewClaudeCliDriver(WithCommandBuilder(mockCmdBuilder("success")))
	resp, err := d.Call(context.Background(), LLMRequest{Intent: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "test output" {
		t.Errorf("expected content 'test output', got %q", resp.Content)
	}
	if resp.TokensUsed != 100 {
		t.Errorf("expected tokens_used 100, got %d", resp.TokensUsed)
	}
}

func TestClaudeCliDriver_Call_Timeout(t *testing.T) {
	d := NewClaudeCliDriver(
		WithCommandBuilder(mockCmdBuilder("timeout")),
		WithTimeout(200*time.Millisecond),
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
	if llmErr.Provider != "claude" {
		t.Errorf("Provider = %q, want %q", llmErr.Provider, "claude")
	}
}

func TestClaudeCliDriver_Call_CLIError(t *testing.T) {
	d := NewClaudeCliDriver(WithCommandBuilder(mockCmdBuilder("cli_error")))
	_, err := d.Call(context.Background(), LLMRequest{Intent: "test"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var llmErr *LLMError
	if !errors.As(err, &llmErr) {
		t.Fatal("expected errors.As to extract *LLMError")
	}
	if llmErr.Provider != "claude" {
		t.Errorf("Provider = %q, want %q", llmErr.Provider, "claude")
	}
	if !strings.Contains(llmErr.Error(), "cli failed") {
		t.Errorf("expected 'cli failed' in error, got: %v", llmErr)
	}
	if !strings.Contains(llmErr.Error(), "Error: invalid arguments") {
		t.Errorf("expected stderr content in error, got: %v", llmErr)
	}
}

func TestClaudeCliDriver_Call_InvalidJSON(t *testing.T) {
	d := NewClaudeCliDriver(WithCommandBuilder(mockCmdBuilder("invalid_json")))
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

func TestClaudeCliDriver_Call_IsError(t *testing.T) {
	d := NewClaudeCliDriver(WithCommandBuilder(mockCmdBuilder("is_error")))
	_, err := d.Call(context.Background(), LLMRequest{Intent: "test"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var llmErr *LLMError
	if !errors.As(err, &llmErr) {
		t.Fatal("expected errors.As to extract *LLMError")
	}
	if llmErr.Provider != "claude" {
		t.Errorf("Provider = %q, want %q", llmErr.Provider, "claude")
	}
	if !strings.Contains(llmErr.Error(), "LLM error message") {
		t.Errorf("expected error content, got: %v", llmErr)
	}
}

func TestClaudeCliDriver_Call_Args(t *testing.T) {
	var capturedArgs []string
	d := NewClaudeCliDriver(WithCommandBuilder(func(ctx context.Context, name string, args ...string) *exec.Cmd {
		capturedArgs = args
		return mockCmdBuilder("success")(ctx, name, args...)
	}))

	_, err := d.Call(context.Background(), LLMRequest{
		Intent:       "analyze code",
		SystemPrompt: "you are an expert",
		Model:        "opus",
		MaxTurns:     3,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	argsStr := strings.Join(capturedArgs, " ")
	if !strings.Contains(argsStr, "-p analyze code") {
		t.Errorf("expected -p flag with intent, got: %s", argsStr)
	}
	if !strings.Contains(argsStr, "--system-prompt you are an expert") {
		t.Errorf("expected --system-prompt flag, got: %s", argsStr)
	}
	if !strings.Contains(argsStr, "--model opus") {
		t.Errorf("expected --model opus, got: %s", argsStr)
	}
	if !strings.Contains(argsStr, "--max-turns 3") {
		t.Errorf("expected --max-turns 3, got: %s", argsStr)
	}
}

func TestClaudeCliDriver_Call_DefaultArgs(t *testing.T) {
	var capturedArgs []string
	d := NewClaudeCliDriver(WithCommandBuilder(func(ctx context.Context, name string, args ...string) *exec.Cmd {
		capturedArgs = args
		return mockCmdBuilder("success")(ctx, name, args...)
	}))

	_, err := d.Call(context.Background(), LLMRequest{Intent: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	argsStr := strings.Join(capturedArgs, " ")
	if !strings.Contains(argsStr, "--model sonnet") {
		t.Errorf("expected default model 'sonnet', got: %s", argsStr)
	}
	if strings.Contains(argsStr, "--max-turns") {
		t.Errorf("expected no --max-turns in default args, got: %s", argsStr)
	}
	// system-prompt should NOT be present when empty
	if strings.Contains(argsStr, "--system-prompt") {
		t.Errorf("unexpected --system-prompt flag for empty system prompt, got: %s", argsStr)
	}
}

func TestClaudeCliDriver_Info(t *testing.T) {
	d := NewClaudeCliDriver(WithModel("opus"))
	info := d.Info()
	if info.Name != "claude-cli" {
		t.Errorf("expected name 'claude-cli', got %q", info.Name)
	}
	if info.Provider != "claude" {
		t.Errorf("expected provider 'claude', got %q", info.Provider)
	}
	if info.DefaultModel != "opus" {
		t.Errorf("expected default model 'opus', got %q", info.DefaultModel)
	}
}

func TestClaudeCliDriver_Options(t *testing.T) {
	t.Run("WithModel", func(t *testing.T) {
		d := NewClaudeCliDriver(WithModel("haiku"))
		if d.defaultModel != "haiku" {
			t.Errorf("expected model 'haiku', got %q", d.defaultModel)
		}
	})

	t.Run("WithTimeout", func(t *testing.T) {
		d := NewClaudeCliDriver(WithTimeout(60 * time.Second))
		if d.defaultTimeout != 60*time.Second {
			t.Errorf("expected timeout 60s, got %v", d.defaultTimeout)
		}
	})

	t.Run("WithCommandBuilder", func(t *testing.T) {
		called := false
		cb := func(ctx context.Context, name string, args ...string) *exec.Cmd {
			called = true
			return mockCmdBuilder("success")(ctx, name, args...)
		}
		d := NewClaudeCliDriver(WithCommandBuilder(cb))
		_, _ = d.Call(context.Background(), LLMRequest{Intent: "test"})
		if !called {
			t.Error("custom command builder was not called")
		}
	})
}

func TestClaudeCliDriver_Stream_Success(t *testing.T) {
	d := NewClaudeCliDriver(WithCommandBuilder(mockCmdBuilder("stream_success")))
	ch, err := d.Stream(context.Background(), LLMRequest{Intent: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var events []StreamEvent
	for evt := range ch {
		events = append(events, evt)
	}

	if len(events) != 3 {
		t.Fatalf("expected 3 events, got %d", len(events))
	}

	if events[0].Type != "content" || events[0].Content != "hello " {
		t.Errorf("event[0]: expected content 'hello ', got type=%q content=%q", events[0].Type, events[0].Content)
	}
	if events[1].Type != "content" || events[1].Content != "world" {
		t.Errorf("event[1]: expected content 'world', got type=%q content=%q", events[1].Type, events[1].Content)
	}
	if events[2].Type != "done" || events[2].Content != "hello world" {
		t.Errorf("event[2]: expected done 'hello world', got type=%q content=%q", events[2].Type, events[2].Content)
	}
	if events[2].TokensUsed != 100 {
		t.Errorf("expected tokens_used 100, got %d", events[2].TokensUsed)
	}
}

func TestClaudeCliDriver_Stream_Error(t *testing.T) {
	d := NewClaudeCliDriver(WithCommandBuilder(mockCmdBuilder("stream_error")))
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
	if llmErr.Provider != "claude" {
		t.Errorf("Provider = %q, want %q", llmErr.Provider, "claude")
	}
	if !strings.Contains(llmErr.Error(), "stream error message") {
		t.Errorf("expected error content, got: %v", llmErr)
	}
}

func TestClaudeCliDriver_Call_ExitCodeWithJSON(t *testing.T) {
	d := NewClaudeCliDriver(WithCommandBuilder(mockCmdBuilder("exit1_with_json")))
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
}

func TestClaudeCliDriver_Call_ExitCodeNoJSON(t *testing.T) {
	d := NewClaudeCliDriver(WithCommandBuilder(mockCmdBuilder("exit1_no_json")))
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

func TestClaudeCliDriver_Call_EmptyResult(t *testing.T) {
	d := NewClaudeCliDriver(WithCommandBuilder(mockCmdBuilder("empty_result")))
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
	if !strings.Contains(llmErr.Error(), "max_turns") {
		t.Errorf("expected max_turns hint in error, got: %v", llmErr)
	}
}

func TestClaudeCliDriver_Stream_EmptyResult(t *testing.T) {
	d := NewClaudeCliDriver(WithCommandBuilder(mockCmdBuilder("stream_empty_result")))
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
	if events[0].Err == nil {
		t.Fatal("expected non-nil error")
	}
	var llmErr *LLMError
	if !errors.As(events[0].Err, &llmErr) {
		t.Fatal("expected errors.As to extract *LLMError")
	}
	if !strings.Contains(llmErr.Error(), "truncated") {
		t.Errorf("expected truncation error, got: %v", llmErr)
	}
}

func TestClaudeCliDriver_Call_IsErrorEmptyResult(t *testing.T) {
	d := NewClaudeCliDriver(WithCommandBuilder(mockCmdBuilder("is_error_empty_result")))
	_, err := d.Call(context.Background(), LLMRequest{Intent: "test"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var llmErr *LLMError
	if !errors.As(err, &llmErr) {
		t.Fatal("expected errors.As to extract *LLMError")
	}
	if !strings.Contains(llmErr.Error(), "unknown error") {
		t.Errorf("expected 'unknown error' fallback, got: %v", llmErr)
	}
}

func TestClaudeCliDriver_Call_ExitCodeWithValidResult(t *testing.T) {
	d := NewClaudeCliDriver(WithCommandBuilder(mockCmdBuilder("exit1_valid_result")))
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

func TestClaudeCliDriver_Stream_IsErrorEmptyResult(t *testing.T) {
	d := NewClaudeCliDriver(WithCommandBuilder(mockCmdBuilder("stream_is_error_empty")))
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
