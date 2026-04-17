package llm

import (
	"context"
	"errors"
	"fmt"
	"io"
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
	case "claude_stream_with_events":
		fmt.Fprintln(os.Stdout, `{"type":"system","subtype":"init","model":"haiku","session_id":"s1"}`)
		fmt.Fprintln(os.Stdout, `{"type":"user","message":{"content":[{"type":"text","text":"test"}]}}`)
		fmt.Fprintln(os.Stdout, `{"type":"stream_event","event":{"type":"content_block_start","content_block":{"type":"tool_use","name":"Read","id":"toolu_1"}}}`)
		fmt.Fprintln(os.Stdout, `{"type":"stream_event","event":{"type":"content_block_start","content_block":{"type":"thinking"}}}`)
		fmt.Fprintln(os.Stdout, `{"type":"stream_event","event":{"type":"content_block_delta","delta":{"type":"thinking_delta","thinking":"let me think"}}}`)
		fmt.Fprintln(os.Stdout, `{"type":"assistant","message":{"content":[{"type":"text","text":"done"}]}}`)
		fmt.Fprintln(os.Stdout, `{"type":"result","subtype":"success","result":"done","is_error":false,"input_tokens":50,"output_tokens":20}`)
	case "cost_usd_full":
		fmt.Fprint(os.Stdout, `{"type":"result","subtype":"success","result":"ok","is_error":false,"total_cost_usd":0.042,"stop_reason":"end_turn","usage":{"input_tokens":100,"cache_read_input_tokens":50,"output_tokens":20}}`)
	case "cost_usd_stream":
		fmt.Fprintln(os.Stdout, `{"type":"assistant","message":{"content":[{"type":"text","text":"hi"}]}}`)
		fmt.Fprintln(os.Stdout, `{"type":"result","subtype":"success","result":"hi","is_error":false,"total_cost_usd":0.042,"stop_reason":"end_turn","usage":{"input_tokens":100,"cache_read_input_tokens":50,"output_tokens":20}}`)
	case "max_turns_result":
		fmt.Fprint(os.Stdout, `{"type":"result","subtype":"error_max_turns","result":"partial","is_error":true,"stop_reason":"max_turns","usage":{"input_tokens":200,"output_tokens":50}}`)
	case "max_turns_stream":
		fmt.Fprintln(os.Stdout, `{"type":"assistant","message":{"content":[{"type":"text","text":"still thinking"}]}}`)
		fmt.Fprintln(os.Stdout, `{"type":"result","subtype":"error_max_turns","result":"partial","is_error":true,"stop_reason":"max_turns","usage":{"input_tokens":200,"output_tokens":50}}`)
	case "auth_required":
		fmt.Fprint(os.Stdout, `{"type":"result","subtype":"error","result":"Please run 'claude login' and visit https://claude.ai/oauth/authorize?redirect=abc to continue.","is_error":true}`)
	case "auth_required_stream":
		fmt.Fprintln(os.Stdout, `{"type":"result","subtype":"error","result":"Not logged in. Please log in at https://claude.ai/auth to continue.","is_error":true}`)
	case "stdin_echo":
		// R1: validate prompt is delivered via stdin (not argv). Echo back as result.
		data, _ := io.ReadAll(os.Stdin)
		fmt.Fprintf(os.Stdout, `{"type":"result","subtype":"success","result":%q,"is_error":false,"input_tokens":1,"output_tokens":1}`, string(data))
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	// R1: prompt now goes to stdin (--print -), NOT argv
	if !strings.Contains(argsStr, "--print -") {
		t.Errorf("expected --print - for stdin mode, got: %s", argsStr)
	}
	if strings.Contains(argsStr, "analyze code") {
		t.Errorf("prompt should not be in argv (stdin mode), got: %s", argsStr)
	}
	// R1: system prompt goes via --append-system-prompt-file, NOT --system-prompt
	if !strings.Contains(argsStr, "--append-system-prompt-file") {
		t.Errorf("expected --append-system-prompt-file, got: %s", argsStr)
	}
	if strings.Contains(argsStr, "you are an expert") {
		t.Errorf("system prompt should not be in argv (file mode), got: %s", argsStr)
	}
	if !strings.Contains(argsStr, "--model opus") {
		t.Errorf("expected --model opus, got: %s", argsStr)
	}
	if !strings.Contains(argsStr, "--max-turns 3") {
		t.Errorf("expected --max-turns 3, got: %s", argsStr)
	}
}

func TestClaudeCliDriver_Call_DefaultArgs(t *testing.T) {
	t.Parallel()
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
	if !strings.Contains(argsStr, "--model "+DefaultModel) {
		t.Errorf("expected default model %q, got: %s", DefaultModel, argsStr)
	}
	if strings.Contains(argsStr, "--max-turns") {
		t.Errorf("expected no --max-turns in default args, got: %s", argsStr)
	}
	// R1: --append-system-prompt-file should NOT be present when SystemPrompt empty
	if strings.Contains(argsStr, "--append-system-prompt-file") {
		t.Errorf("unexpected --append-system-prompt-file for empty system prompt, got: %s", argsStr)
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
	t.Parallel()
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

	t.Run("WithExtraArgs", func(t *testing.T) {
		var capturedArgs []string
		d := NewClaudeCliDriver(
			WithExtraArgs([]string{"--dangerously-skip-permissions", "--custom-flag"}),
			WithCommandBuilder(func(ctx context.Context, name string, args ...string) *exec.Cmd {
				capturedArgs = args
				return mockCmdBuilder("success")(ctx, name, args...)
			}),
		)
		_, _ = d.Call(context.Background(), LLMRequest{Intent: "test"})
		argsStr := strings.Join(capturedArgs, " ")
		if !strings.Contains(argsStr, "--dangerously-skip-permissions") {
			t.Errorf("expected --dangerously-skip-permissions in args, got: %s", argsStr)
		}
		if !strings.Contains(argsStr, "--custom-flag") {
			t.Errorf("expected --custom-flag in args, got: %s", argsStr)
		}
	})
}

func TestClaudeCliDriver_Stream_Success(t *testing.T) {
	t.Parallel()
	d := NewClaudeCliDriver(WithCommandBuilder(mockCmdBuilder("stream_success")))
	ch, err := d.Stream(context.Background(), LLMRequest{Intent: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var events []StreamEvent
	for evt := range ch {
		events = append(events, evt)
	}

	// 5 events: 2 assistant + 2 content (text extracted) + 1 done
	if len(events) != 5 {
		t.Fatalf("expected 5 events, got %d: %+v", len(events), events)
	}

	if events[0].Type != "assistant" {
		t.Errorf("event[0]: expected assistant, got type=%q", events[0].Type)
	}
	if events[1].Type != "content" || events[1].Content != "hello " {
		t.Errorf("event[1]: expected content 'hello ', got type=%q content=%q", events[1].Type, events[1].Content)
	}
	if events[2].Type != "assistant" {
		t.Errorf("event[2]: expected assistant, got type=%q", events[2].Type)
	}
	if events[3].Type != "content" || events[3].Content != "world" {
		t.Errorf("event[3]: expected content 'world', got type=%q content=%q", events[3].Type, events[3].Content)
	}
	if events[4].Type != "done" || events[4].Content != "hello world" {
		t.Errorf("event[4]: expected done 'hello world', got type=%q content=%q", events[4].Type, events[4].Content)
	}
	if events[4].TokensUsed != 100 {
		t.Errorf("expected tokens_used 100, got %d", events[4].TokensUsed)
	}
}

func TestClaudeCliDriver_Stream_Error(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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

func TestClaudeCliDriver_Stream_AllEventTypes(t *testing.T) {
	t.Parallel()
	d := NewClaudeCliDriver(WithCommandBuilder(mockCmdBuilder("claude_stream_with_events")))
	ch, err := d.Stream(context.Background(), LLMRequest{Intent: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var events []StreamEvent
	for evt := range ch {
		events = append(events, evt)
	}

	// Expect: system, user, tool_call(started), thinking(started), thinking(delta), content, done
	typeSeq := make([]string, len(events))
	for i, e := range events {
		typeSeq[i] = e.Type
	}

	// Verify system event
	found := false
	for _, e := range events {
		if e.Type == "system" && e.Content == "init" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected system:init event, got types: %v", typeSeq)
	}

	// Verify user event
	found = false
	for _, e := range events {
		if e.Type == "user" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected user event, got types: %v", typeSeq)
	}

	// Verify tool_call event from stream_event
	found = false
	for _, e := range events {
		if e.Type == "tool_call" && e.Content == "started" {
			if e.Data["tool"] == "Read" {
				found = true
				break
			}
		}
	}
	if !found {
		t.Errorf("expected tool_call:started event with tool=Read, got types: %v", typeSeq)
	}

	// Verify thinking event from stream_event
	found = false
	for _, e := range events {
		if e.Type == "thinking" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected thinking event, got types: %v", typeSeq)
	}

	// Verify done event at end
	last := events[len(events)-1]
	if last.Type != "done" {
		t.Errorf("expected last event to be done, got %q", last.Type)
	}
}

// TestClaudeCliDriver_Call_CostUSD verifies R4: total_cost_usd and Usage fields
// are parsed from the CLI response and surfaced on LLMResponse.
func TestClaudeCliDriver_Call_CostUSD(t *testing.T) {
	t.Parallel()
	d := NewClaudeCliDriver(WithCommandBuilder(mockCmdBuilder("cost_usd_full")))
	resp, err := d.Call(context.Background(), LLMRequest{Intent: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.CostUSD != 0.042 {
		t.Errorf("CostUSD = %v, want 0.042", resp.CostUSD)
	}
	if resp.InputTokens != 100 {
		t.Errorf("InputTokens = %d, want 100", resp.InputTokens)
	}
	if resp.OutputTokens != 20 {
		t.Errorf("OutputTokens = %d, want 20", resp.OutputTokens)
	}
	if resp.CachedInputTokens != 50 {
		t.Errorf("CachedInputTokens = %d, want 50", resp.CachedInputTokens)
	}
	if resp.TokensUsed != 120 {
		t.Errorf("TokensUsed = %d, want 120 (input+output)", resp.TokensUsed)
	}
	if resp.StopReason != "end_turn" {
		t.Errorf("StopReason = %q, want %q", resp.StopReason, "end_turn")
	}
}

// TestClaudeCliDriver_Stream_CostUSD verifies the stream path preserves the
// new usage/cost fields through the StreamEvent done event.
func TestClaudeCliDriver_Stream_CostUSD(t *testing.T) {
	t.Parallel()
	d := NewClaudeCliDriver(WithCommandBuilder(mockCmdBuilder("cost_usd_stream")))
	ch, err := d.Stream(context.Background(), LLMRequest{Intent: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var done StreamEvent
	for evt := range ch {
		if evt.Type == "done" {
			done = evt
		}
	}
	if done.Type != "done" {
		t.Fatal("never received done event")
	}
	if done.CostUSD != 0.042 {
		t.Errorf("CostUSD = %v, want 0.042", done.CostUSD)
	}
	if done.CachedInputTokens != 50 {
		t.Errorf("CachedInputTokens = %d, want 50", done.CachedInputTokens)
	}
	if done.StopReason != "end_turn" {
		t.Errorf("StopReason = %q, want %q", done.StopReason, "end_turn")
	}
}

// TestClaudeCliDriver_Call_MaxTurns verifies R2: a CLI result carrying
// subtype=error_max_turns (or stop_reason=max_turns) surfaces as ErrMaxTurns.
func TestClaudeCliDriver_Call_MaxTurns(t *testing.T) {
	t.Parallel()
	d := NewClaudeCliDriver(WithCommandBuilder(mockCmdBuilder("max_turns_result")))
	_, err := d.Call(context.Background(), LLMRequest{Intent: "test"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrMaxTurns) {
		t.Errorf("expected errors.Is(err, ErrMaxTurns), got: %v", err)
	}
	var llmErr *LLMError
	if !errors.As(err, &llmErr) {
		t.Fatal("expected *LLMError")
	}
	if llmErr.Provider != "claude" {
		t.Errorf("Provider = %q, want %q", llmErr.Provider, "claude")
	}
}

// TestClaudeCliDriver_Stream_MaxTurns verifies stream path emits an error event
// carrying ErrMaxTurns when the result carries max_turns markers.
func TestClaudeCliDriver_Stream_MaxTurns(t *testing.T) {
	t.Parallel()
	d := NewClaudeCliDriver(WithCommandBuilder(mockCmdBuilder("max_turns_stream")))
	ch, err := d.Stream(context.Background(), LLMRequest{Intent: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var finalEvt StreamEvent
	for evt := range ch {
		finalEvt = evt
	}
	if finalEvt.Type != "error" {
		t.Fatalf("expected final event type=error, got %q", finalEvt.Type)
	}
	if !errors.Is(finalEvt.Err, ErrMaxTurns) {
		t.Errorf("expected errors.Is(err, ErrMaxTurns), got: %v", finalEvt.Err)
	}
}

// TestClaudeCliDriver_Call_LoginRequired verifies R3: auth-required phrases
// surface as ErrLoginRequired with a Meta["login_url"] hint.
func TestClaudeCliDriver_Call_LoginRequired(t *testing.T) {
	t.Parallel()
	d := NewClaudeCliDriver(WithCommandBuilder(mockCmdBuilder("auth_required")))
	_, err := d.Call(context.Background(), LLMRequest{Intent: "test"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrLoginRequired) {
		t.Errorf("expected errors.Is(err, ErrLoginRequired), got: %v", err)
	}
	var llmErr *LLMError
	if !errors.As(err, &llmErr) {
		t.Fatal("expected *LLMError")
	}
	if llmErr.StatusCode != 401 {
		t.Errorf("StatusCode = %d, want 401", llmErr.StatusCode)
	}
	if got := llmErr.Meta["login_url"]; !strings.Contains(got, "claude.ai") {
		t.Errorf("Meta[login_url] = %q, want to contain claude.ai", got)
	}
}

// TestClaudeCliDriver_Stream_LoginRequired verifies stream path surfaces
// ErrLoginRequired in the final error event.
func TestClaudeCliDriver_Stream_LoginRequired(t *testing.T) {
	t.Parallel()
	d := NewClaudeCliDriver(WithCommandBuilder(mockCmdBuilder("auth_required_stream")))
	ch, err := d.Stream(context.Background(), LLMRequest{Intent: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var finalEvt StreamEvent
	for evt := range ch {
		finalEvt = evt
	}
	if !errors.Is(finalEvt.Err, ErrLoginRequired) {
		t.Errorf("expected errors.Is(err, ErrLoginRequired), got: %v", finalEvt.Err)
	}
	var llmErr *LLMError
	if !errors.As(finalEvt.Err, &llmErr) {
		t.Fatal("expected *LLMError")
	}
	if got := llmErr.Meta["login_url"]; !strings.Contains(got, "claude.ai") {
		t.Errorf("Meta[login_url] = %q, want to contain claude.ai", got)
	}
}

// TestClaudeCliDriver_Call_StdinPrompt verifies R1: the prompt is delivered
// via stdin (not argv). The helper echoes stdin back as the result.
func TestClaudeCliDriver_Call_StdinPrompt(t *testing.T) {
	t.Parallel()
	d := NewClaudeCliDriver(WithCommandBuilder(mockCmdBuilder("stdin_echo")))
	resp, err := d.Call(context.Background(), LLMRequest{Intent: "secret-prompt-xyz"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "secret-prompt-xyz" {
		t.Errorf("prompt did not reach stdin; content=%q", resp.Content)
	}
}

// TestClaudeCliDriver_BuildArgs_PromptNotInArgv verifies R1: the prompt string
// no longer appears in argv (avoids ps-ef leaks and ARG_MAX risk).
func TestClaudeCliDriver_BuildArgs_PromptNotInArgv(t *testing.T) {
	t.Parallel()
	d := NewClaudeCliDriver()
	args, sysFile, err := d.buildArgs(LLMRequest{Intent: "LEAKABLE_SECRET"}, "json")
	if err != nil {
		t.Fatalf("buildArgs error: %v", err)
	}
	if sysFile != "" {
		t.Errorf("expected no system prompt file when SystemPrompt empty, got %q", sysFile)
	}
	joined := strings.Join(args, " ")
	if strings.Contains(joined, "LEAKABLE_SECRET") {
		t.Errorf("prompt leaked into argv: %s", joined)
	}
	if !strings.Contains(joined, "--print -") {
		t.Errorf("expected --print - for stdin mode, got: %s", joined)
	}
}

// TestClaudeCliDriver_BuildArgs_SystemPromptFile verifies R1: a non-empty
// SystemPrompt is written to a temp file and referenced via
// --append-system-prompt-file (not injected as an argv string).
func TestClaudeCliDriver_BuildArgs_SystemPromptFile(t *testing.T) {
	t.Parallel()
	d := NewClaudeCliDriver()
	sysPrompt := "You are a helpful assistant with LONG CONTEXT instructions..."
	args, sysFile, err := d.buildArgs(LLMRequest{Intent: "ignored", SystemPrompt: sysPrompt}, "json")
	if err != nil {
		t.Fatalf("buildArgs error: %v", err)
	}
	if sysFile == "" {
		t.Fatal("expected system prompt tempfile, got empty")
	}
	defer os.Remove(sysFile)

	joined := strings.Join(args, " ")
	if strings.Contains(joined, sysPrompt) {
		t.Errorf("system prompt leaked into argv: %s", joined)
	}
	if !strings.Contains(joined, "--append-system-prompt-file") {
		t.Errorf("expected --append-system-prompt-file in args, got: %s", joined)
	}
	if !strings.Contains(joined, sysFile) {
		t.Errorf("expected %q in args, got: %s", sysFile, joined)
	}
	data, err := os.ReadFile(sysFile)
	if err != nil {
		t.Fatalf("read system prompt tempfile: %v", err)
	}
	if string(data) != sysPrompt {
		t.Errorf("tempfile content mismatch: got %q, want %q", string(data), sysPrompt)
	}
}