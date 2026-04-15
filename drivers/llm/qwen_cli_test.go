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

// TestQwenHelperProcess is a mock process used by tests to simulate the Qwen CLI.
func TestQwenHelperProcess(t *testing.T) {
	if os.Getenv("GO_TEST_PROCESS") != "1" {
		return
	}
	switch os.Getenv("GO_TEST_CASE") {
	case "qwen_success":
		// Qwen JSON output is an array of messages with nested usage in result.
		fmt.Fprint(os.Stdout, `[{"type":"system","subtype":"init"},{"type":"assistant","uuid":"a1","session_id":"s1","message":{"id":"m1","type":"message","role":"assistant","model":"qwen3-coder","content":[{"type":"text","text":"test output"}]},"parent_tool_use_id":null},{"type":"result","subtype":"success","result":"test output","is_error":false,"duration_ms":100,"num_turns":1,"usage":{"input_tokens":80,"output_tokens":20,"total_tokens":100}}]`)
	case "qwen_is_error":
		fmt.Fprint(os.Stdout, `[{"type":"result","subtype":"error_during_execution","is_error":true,"result":"LLM error message","usage":{"input_tokens":0,"output_tokens":0}}]`)
	case "qwen_is_error_with_error_field":
		fmt.Fprint(os.Stdout, `[{"type":"result","subtype":"error_during_execution","is_error":true,"result":"","error":{"message":"auth failed key invalid"},"usage":{"input_tokens":0,"output_tokens":0}}]`)
	case "qwen_cli_error":
		fmt.Fprint(os.Stderr, "Error: invalid arguments")
		os.Exit(1)
	case "qwen_invalid_json":
		fmt.Fprint(os.Stdout, "not json at all")
	case "qwen_timeout":
		time.Sleep(5 * time.Second)
	case "qwen_empty_result":
		fmt.Fprint(os.Stdout, `[{"type":"result","subtype":"success","result":"","is_error":false,"usage":{"input_tokens":10,"output_tokens":0}}]`)
	case "qwen_args_echo":
		fmt.Fprint(os.Stderr, strings.Join(os.Args, " "))
		fmt.Fprint(os.Stdout, `[{"type":"result","subtype":"success","result":"ok","is_error":false,"usage":{"input_tokens":0,"output_tokens":0}}]`)
	case "qwen_stream_success":
		fmt.Fprintln(os.Stdout, `{"type":"system","subtype":"init","tools":["read","write"]}`)
		fmt.Fprintln(os.Stdout, `{"type":"assistant","uuid":"a1","session_id":"s1","message":{"content":[{"type":"text","text":"hello "}]},"parent_tool_use_id":null}`)
		fmt.Fprintln(os.Stdout, `{"type":"assistant","uuid":"a2","session_id":"s1","message":{"content":[{"type":"text","text":"world"}]},"parent_tool_use_id":null}`)
		fmt.Fprintln(os.Stdout, `{"type":"result","subtype":"success","result":"hello world","is_error":false,"num_turns":1,"usage":{"input_tokens":70,"output_tokens":30}}`)
	case "qwen_stream_error":
		fmt.Fprintln(os.Stdout, `{"type":"result","subtype":"error_during_execution","result":"stream error message","is_error":true,"usage":{"input_tokens":0,"output_tokens":0}}`)
	case "qwen_stream_empty_result":
		fmt.Fprintln(os.Stdout, `{"type":"result","subtype":"success","result":"","is_error":false,"usage":{"input_tokens":10,"output_tokens":0}}`)
	case "qwen_stream_with_events":
		fmt.Fprintln(os.Stdout, `{"type":"system","subtype":"init","model":"qwen3-coder","session_id":"s1"}`)
		fmt.Fprintln(os.Stdout, `{"type":"user","uuid":"u1","session_id":"s1","message":{"role":"user","content":[{"type":"text","text":"test"}]},"parent_tool_use_id":null}`)
		fmt.Fprintln(os.Stdout, `{"type":"stream_event","uuid":"se1","session_id":"s1","parent_tool_use_id":null,"event":{"type":"content_block_start","content_block":{"type":"tool_use","name":"Read","id":"toolu_1"}}}`)
		fmt.Fprintln(os.Stdout, `{"type":"stream_event","uuid":"se2","session_id":"s1","parent_tool_use_id":null,"event":{"type":"content_block_start","content_block":{"type":"thinking"}}}`)
		fmt.Fprintln(os.Stdout, `{"type":"stream_event","uuid":"se3","session_id":"s1","parent_tool_use_id":null,"event":{"type":"content_block_delta","delta":{"type":"thinking_delta","thinking":"let me think"}}}`)
		fmt.Fprintln(os.Stdout, `{"type":"assistant","uuid":"a1","session_id":"s1","message":{"content":[{"type":"text","text":"done"}]},"parent_tool_use_id":null}`)
		fmt.Fprintln(os.Stdout, `{"type":"result","subtype":"success","result":"done","is_error":false,"usage":{"input_tokens":50,"output_tokens":20}}`)
	case "qwen_exit1_rate_limit":
		fmt.Fprint(os.Stdout, `[{"type":"result","subtype":"error_during_execution","is_error":true,"result":"rate limit exceeded","usage":{"input_tokens":0,"output_tokens":0}}]`)
		os.Exit(1)
	case "qwen_stream_is_error_empty":
		fmt.Fprintln(os.Stdout, `{"type":"result","subtype":"error_during_execution","result":"","is_error":true,"usage":{"input_tokens":0,"output_tokens":0}}`)
	}
	os.Exit(0)
}

func qwenMockCmdBuilder(testCase string) CommandBuilder {
	return func(ctx context.Context, name string, args ...string) *exec.Cmd {
		cs := []string{"-test.run=TestQwenHelperProcess", "--"}
		cs = append(cs, args...)
		cmd := exec.CommandContext(ctx, os.Args[0], cs...)
		cmd.Env = append(os.Environ(), "GO_TEST_PROCESS=1", "GO_TEST_CASE="+testCase)
		return cmd
	}
}

func TestQwenCliDriver_Call_Success(t *testing.T) {
	t.Parallel()
	d := NewQwenCliDriver(QwenWithCommandBuilder(qwenMockCmdBuilder("qwen_success")))
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
	if resp.InputTokens != 80 {
		t.Errorf("expected input_tokens 80, got %d", resp.InputTokens)
	}
	if resp.OutputTokens != 20 {
		t.Errorf("expected output_tokens 20, got %d", resp.OutputTokens)
	}
}

func TestQwenCliDriver_Call_Timeout(t *testing.T) {
	t.Parallel()
	d := NewQwenCliDriver(
		QwenWithCommandBuilder(qwenMockCmdBuilder("qwen_timeout")),
		QwenWithTimeout(200*time.Millisecond),
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
	if llmErr.Provider != "qwen" {
		t.Errorf("Provider = %q, want %q", llmErr.Provider, "qwen")
	}
}

func TestQwenCliDriver_Call_CLIError(t *testing.T) {
	t.Parallel()
	d := NewQwenCliDriver(QwenWithCommandBuilder(qwenMockCmdBuilder("qwen_cli_error")))
	_, err := d.Call(context.Background(), LLMRequest{Intent: "test"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var llmErr *LLMError
	if !errors.As(err, &llmErr) {
		t.Fatal("expected errors.As to extract *LLMError")
	}
	if llmErr.Provider != "qwen" {
		t.Errorf("Provider = %q, want %q", llmErr.Provider, "qwen")
	}
	if !strings.Contains(llmErr.Error(), "cli failed") {
		t.Errorf("expected 'cli failed' in error, got: %v", llmErr)
	}
}

func TestQwenCliDriver_Call_InvalidJSON(t *testing.T) {
	t.Parallel()
	d := NewQwenCliDriver(QwenWithCommandBuilder(qwenMockCmdBuilder("qwen_invalid_json")))
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

func TestQwenCliDriver_Call_IsError(t *testing.T) {
	t.Parallel()
	d := NewQwenCliDriver(QwenWithCommandBuilder(qwenMockCmdBuilder("qwen_is_error")))
	_, err := d.Call(context.Background(), LLMRequest{Intent: "test"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var llmErr *LLMError
	if !errors.As(err, &llmErr) {
		t.Fatal("expected errors.As to extract *LLMError")
	}
	if llmErr.Provider != "qwen" {
		t.Errorf("Provider = %q, want %q", llmErr.Provider, "qwen")
	}
	if !strings.Contains(llmErr.Error(), "LLM error message") {
		t.Errorf("expected error content, got: %v", llmErr)
	}
}

func TestQwenCliDriver_Call_IsErrorWithErrorField(t *testing.T) {
	t.Parallel()
	d := NewQwenCliDriver(QwenWithCommandBuilder(qwenMockCmdBuilder("qwen_is_error_with_error_field")))
	_, err := d.Call(context.Background(), LLMRequest{Intent: "test"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var llmErr *LLMError
	if !errors.As(err, &llmErr) {
		t.Fatal("expected errors.As to extract *LLMError")
	}
	// Should classify "auth" keyword as auth error.
	if !errors.Is(err, ErrAuth) {
		t.Errorf("expected errors.Is(err, ErrAuth), got: %v", err)
	}
}

func TestQwenCliDriver_Call_EmptyResult(t *testing.T) {
	t.Parallel()
	d := NewQwenCliDriver(QwenWithCommandBuilder(qwenMockCmdBuilder("qwen_empty_result")))
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

func TestQwenCliDriver_Call_Args(t *testing.T) {
	t.Parallel()
	var capturedArgs []string
	d := NewQwenCliDriver(QwenWithCommandBuilder(func(ctx context.Context, name string, args ...string) *exec.Cmd {
		capturedArgs = args
		return qwenMockCmdBuilder("qwen_success")(ctx, name, args...)
	}))

	_, err := d.Call(context.Background(), LLMRequest{
		Intent:       "analyze code",
		SystemPrompt: "you are an expert",
		Model:        "qwen3-coder",
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
	if !strings.Contains(argsStr, "--model qwen3-coder") {
		t.Errorf("expected --model qwen3-coder, got: %s", argsStr)
	}
	if !strings.Contains(argsStr, "--max-session-turns 3") {
		t.Errorf("expected --max-session-turns 3, got: %s", argsStr)
	}
	if !strings.Contains(argsStr, "--yolo") {
		t.Errorf("expected --yolo flag, got: %s", argsStr)
	}
}

func TestQwenCliDriver_Call_DefaultArgs(t *testing.T) {
	t.Parallel()
	var capturedArgs []string
	d := NewQwenCliDriver(QwenWithCommandBuilder(func(ctx context.Context, name string, args ...string) *exec.Cmd {
		capturedArgs = args
		return qwenMockCmdBuilder("qwen_success")(ctx, name, args...)
	}))

	_, err := d.Call(context.Background(), LLMRequest{Intent: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	argsStr := strings.Join(capturedArgs, " ")
	if !strings.Contains(argsStr, "--model "+QwenDefaultModel) {
		t.Errorf("expected default model %q, got: %s", QwenDefaultModel, argsStr)
	}
	if !strings.Contains(argsStr, "--output-format json") {
		t.Errorf("expected --output-format json, got: %s", argsStr)
	}
	if !strings.Contains(argsStr, "--yolo") {
		t.Errorf("expected --yolo, got: %s", argsStr)
	}
	if strings.Contains(argsStr, "--max-session-turns") {
		t.Errorf("expected no --max-session-turns in default args, got: %s", argsStr)
	}
	if strings.Contains(argsStr, "--system-prompt") {
		t.Errorf("unexpected --system-prompt for empty system prompt, got: %s", argsStr)
	}
	// Should NOT contain Claude-specific flags.
	if strings.Contains(argsStr, "--bare") {
		t.Errorf("unexpected --bare flag, got: %s", argsStr)
	}
	if strings.Contains(argsStr, "--tools") {
		t.Errorf("unexpected --tools flag, got: %s", argsStr)
	}
}

func TestQwenCliDriver_Info(t *testing.T) {
	d := NewQwenCliDriver(QwenWithModel("qwen3-coder"))
	info := d.Info()
	if info.Name != "qwen-cli" {
		t.Errorf("expected name 'qwen-cli', got %q", info.Name)
	}
	if info.Provider != "qwen" {
		t.Errorf("expected provider 'qwen', got %q", info.Provider)
	}
	if info.DefaultModel != "qwen3-coder" {
		t.Errorf("expected default model 'qwen3-coder', got %q", info.DefaultModel)
	}
	if info.DriverType != DriverQwenCLI {
		t.Errorf("expected driver type %q, got %q", DriverQwenCLI, info.DriverType)
	}
}

func TestQwenCliDriver_Options(t *testing.T) {
	t.Parallel()
	t.Run("QwenWithModel", func(t *testing.T) {
		d := NewQwenCliDriver(QwenWithModel("qwen3-coder"))
		if d.defaultModel != "qwen3-coder" {
			t.Errorf("expected model 'qwen3-coder', got %q", d.defaultModel)
		}
	})

	t.Run("QwenWithTimeout", func(t *testing.T) {
		d := NewQwenCliDriver(QwenWithTimeout(60 * time.Second))
		if d.defaultTimeout != 60*time.Second {
			t.Errorf("expected timeout 60s, got %v", d.defaultTimeout)
		}
	})

	t.Run("QwenWithCommand", func(t *testing.T) {
		d := NewQwenCliDriver(QwenWithCommand("my-qwen"))
		if d.cliCommand != "my-qwen" {
			t.Errorf("expected command 'my-qwen', got %q", d.cliCommand)
		}
	})

	t.Run("QwenWithCommandBuilder", func(t *testing.T) {
		called := false
		cb := func(ctx context.Context, name string, args ...string) *exec.Cmd {
			called = true
			return qwenMockCmdBuilder("qwen_success")(ctx, name, args...)
		}
		d := NewQwenCliDriver(QwenWithCommandBuilder(cb))
		_, _ = d.Call(context.Background(), LLMRequest{Intent: "test"})
		if !called {
			t.Error("custom command builder was not called")
		}
	})

	t.Run("QwenWithExtraArgs", func(t *testing.T) {
		var capturedArgs []string
		d := NewQwenCliDriver(
			QwenWithExtraArgs([]string{"--exclude-tools", "shell", "--custom-flag"}),
			QwenWithCommandBuilder(func(ctx context.Context, name string, args ...string) *exec.Cmd {
				capturedArgs = args
				return qwenMockCmdBuilder("qwen_success")(ctx, name, args...)
			}),
		)
		_, _ = d.Call(context.Background(), LLMRequest{Intent: "test"})
		argsStr := strings.Join(capturedArgs, " ")
		if !strings.Contains(argsStr, "--exclude-tools") {
			t.Errorf("expected --exclude-tools in args, got: %s", argsStr)
		}
		if !strings.Contains(argsStr, "--custom-flag") {
			t.Errorf("expected --custom-flag in args, got: %s", argsStr)
		}
	})
}

func TestQwenCliDriver_Stream_Success(t *testing.T) {
	t.Parallel()
	d := NewQwenCliDriver(QwenWithCommandBuilder(qwenMockCmdBuilder("qwen_stream_success")))
	ch, err := d.Stream(context.Background(), LLMRequest{Intent: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var events []StreamEvent
	for evt := range ch {
		events = append(events, evt)
	}

	// Expect: system + 2*(assistant + content) + done = 6 events
	if len(events) != 6 {
		types := make([]string, len(events))
		for i, e := range events {
			types[i] = e.Type
		}
		t.Fatalf("expected 6 events, got %d: %v", len(events), types)
	}

	if events[0].Type != "system" || events[0].Content != "init" {
		t.Errorf("event[0]: expected system:init, got type=%q content=%q", events[0].Type, events[0].Content)
	}
	if events[1].Type != "assistant" {
		t.Errorf("event[1]: expected assistant, got type=%q", events[1].Type)
	}
	if events[2].Type != "content" || events[2].Content != "hello " {
		t.Errorf("event[2]: expected content 'hello ', got type=%q content=%q", events[2].Type, events[2].Content)
	}
	if events[4].Type != "content" || events[4].Content != "world" {
		t.Errorf("event[4]: expected content 'world', got type=%q content=%q", events[4].Type, events[4].Content)
	}

	last := events[len(events)-1]
	if last.Type != "done" || last.Content != "hello world" {
		t.Errorf("last event: expected done 'hello world', got type=%q content=%q", last.Type, last.Content)
	}
	if last.TokensUsed != 100 {
		t.Errorf("expected tokens_used 100, got %d", last.TokensUsed)
	}
}

func TestQwenCliDriver_Stream_Error(t *testing.T) {
	t.Parallel()
	d := NewQwenCliDriver(QwenWithCommandBuilder(qwenMockCmdBuilder("qwen_stream_error")))
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
		t.Fatal("expected errors.As to extract *LLMError")
	}
	if llmErr.Provider != "qwen" {
		t.Errorf("Provider = %q, want %q", llmErr.Provider, "qwen")
	}
}

func TestQwenCliDriver_Stream_EmptyResult(t *testing.T) {
	t.Parallel()
	d := NewQwenCliDriver(QwenWithCommandBuilder(qwenMockCmdBuilder("qwen_stream_empty_result")))
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
	if !strings.Contains(llmErr.Error(), "truncated") {
		t.Errorf("expected truncation error, got: %v", llmErr)
	}
}

func TestQwenCliDriver_Stream_AllEventTypes(t *testing.T) {
	t.Parallel()
	d := NewQwenCliDriver(QwenWithCommandBuilder(qwenMockCmdBuilder("qwen_stream_with_events")))
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
	if last.InputTokens != 50 {
		t.Errorf("expected input_tokens 50, got %d", last.InputTokens)
	}
	if last.OutputTokens != 20 {
		t.Errorf("expected output_tokens 20, got %d", last.OutputTokens)
	}
}

func TestQwenCliDriver_Stream_IsErrorEmpty(t *testing.T) {
	t.Parallel()
	d := NewQwenCliDriver(QwenWithCommandBuilder(qwenMockCmdBuilder("qwen_stream_is_error_empty")))
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

func TestQwenCliDriver_Call_RateLimitClassification(t *testing.T) {
	t.Parallel()
	d := NewQwenCliDriver(QwenWithCommandBuilder(qwenMockCmdBuilder("qwen_exit1_rate_limit")))
	_, err := d.Call(context.Background(), LLMRequest{Intent: "test"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrRateLimit) {
		t.Errorf("expected errors.Is(err, ErrRateLimit), got: %v", err)
	}
}

func TestQwenCliDriver_Stream_Args(t *testing.T) {
	t.Parallel()
	var capturedArgs []string
	d := NewQwenCliDriver(QwenWithCommandBuilder(func(ctx context.Context, name string, args ...string) *exec.Cmd {
		capturedArgs = args
		return qwenMockCmdBuilder("qwen_stream_success")(ctx, name, args...)
	}))

	ch, err := d.Stream(context.Background(), LLMRequest{Intent: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for range ch {
		// drain
	}

	argsStr := strings.Join(capturedArgs, " ")
	if !strings.Contains(argsStr, "--output-format stream-json") {
		t.Errorf("expected --output-format stream-json, got: %s", argsStr)
	}
	if !strings.Contains(argsStr, "--include-partial-messages") {
		t.Errorf("expected --include-partial-messages, got: %s", argsStr)
	}
	if !strings.Contains(argsStr, "--yolo") {
		t.Errorf("expected --yolo, got: %s", argsStr)
	}
}

func TestParseQwenJsonResult(t *testing.T) {
	t.Parallel()

	t.Run("valid array", func(t *testing.T) {
		data := []byte(`[{"type":"system"},{"type":"result","subtype":"success","result":"hello","is_error":false,"usage":{"input_tokens":10,"output_tokens":5}}]`)
		result, err := parseQwenJsonResult(data)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if result.Result != "hello" {
			t.Errorf("expected result 'hello', got %q", result.Result)
		}
		if result.Usage.InputTokens != 10 {
			t.Errorf("expected input_tokens 10, got %d", result.Usage.InputTokens)
		}
	})

	t.Run("no result in array", func(t *testing.T) {
		data := []byte(`[{"type":"system"},{"type":"assistant"}]`)
		_, err := parseQwenJsonResult(data)
		if err == nil {
			t.Fatal("expected error for array without result")
		}
	})

	t.Run("invalid json", func(t *testing.T) {
		data := []byte(`not json`)
		_, err := parseQwenJsonResult(data)
		if err == nil {
			t.Fatal("expected error for invalid json")
		}
	})

	t.Run("empty array", func(t *testing.T) {
		data := []byte(`[]`)
		_, err := parseQwenJsonResult(data)
		if err == nil {
			t.Fatal("expected error for empty array")
		}
	})
}

func TestQwenCliDriver_BuildPrompt_MultiTurn(t *testing.T) {
	d := NewQwenCliDriver()
	req := LLMRequest{
		Intent: "fix the bug",
		Messages: []Message{
			{Role: "user", Content: "hello"},
			{Role: "assistant", Content: "hi there"},
			{Role: "user", Content: "fix this"},
		},
	}
	prompt := d.buildPrompt(req)
	if !strings.Contains(prompt, "Human: hello") {
		t.Errorf("expected 'Human: hello' in prompt, got: %s", prompt)
	}
	if !strings.Contains(prompt, "Assistant: hi there") {
		t.Errorf("expected 'Assistant: hi there' in prompt, got: %s", prompt)
	}
	if !strings.Contains(prompt, "Continue. Your original task: fix the bug") {
		t.Errorf("expected task reminder in prompt, got: %s", prompt)
	}
}
