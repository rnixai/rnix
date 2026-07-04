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

// TestCodexHelperProcess is a mock process used by tests to simulate the Codex CLI.
func TestCodexHelperProcess(t *testing.T) {
	if os.Getenv("GO_TEST_PROCESS") != "1" {
		return
	}
	switch os.Getenv("GO_TEST_CASE") {
	case "codex_call_success":
		// Non-JSON mode: progress to stderr, final message to stdout.
		fmt.Fprint(os.Stderr, "Running task...\n")
		fmt.Fprint(os.Stdout, "The repository structure looks good.")
	case "codex_call_error":
		fmt.Fprint(os.Stderr, "Error: authentication failed")
		os.Exit(1)
	case "codex_call_empty":
		// No output at all.
	case "codex_timeout":
		time.Sleep(5 * time.Second)
	case "codex_args_echo":
		fmt.Fprint(os.Stderr, strings.Join(os.Args, "\x00"))
		fmt.Fprint(os.Stdout, "ok")
	case "codex_stream_success":
		// Mock lines mirror the REAL codex 0.14x --json schema (verified live,
		// investigation codex-cli-observability-parity): command_execution
		// carries aggregated_output + exit_code, NOT "output".
		fmt.Fprintln(os.Stdout, `{"type":"thread.started","thread_id":"tid-123"}`)
		fmt.Fprintln(os.Stdout, `{"type":"turn.started"}`)
		fmt.Fprintln(os.Stdout, `{"type":"item.started","item":{"id":"item_1","type":"command_execution","command":"bash -lc ls","status":"in_progress","aggregated_output":"","exit_code":null}}`)
		fmt.Fprintln(os.Stdout, `{"type":"item.completed","item":{"id":"item_1","type":"command_execution","command":"bash -lc ls","status":"completed","aggregated_output":"file1.go\nfile2.go","exit_code":0}}`)
		fmt.Fprintln(os.Stdout, `{"type":"item.completed","item":{"id":"item_2","type":"agent_message","text":"Found 2 Go files in the repository."}}`)
		fmt.Fprintln(os.Stdout, `{"type":"turn.completed","usage":{"input_tokens":500,"cached_input_tokens":400,"output_tokens":50}}`)
	case "codex_stream_collab":
		// multi_agent_v2 collab session: spawn_agent + wait items, plus an
		// unknown "error" item that must pass through (not be dropped).
		fmt.Fprintln(os.Stdout, `{"type":"thread.started","thread_id":"tid-collab"}`)
		fmt.Fprintln(os.Stdout, `{"type":"item.completed","item":{"id":"item_0","type":"error","message":"Under-development features enabled: multi_agent_v2."}}`)
		fmt.Fprintln(os.Stdout, `{"type":"turn.started"}`)
		fmt.Fprintln(os.Stdout, `{"type":"item.started","item":{"id":"item_1","type":"collab_tool_call","tool":"spawn_agent","sender_thread_id":"tid-collab","status":"in_progress"}}`)
		fmt.Fprintln(os.Stdout, `{"type":"item.completed","item":{"id":"item_1","type":"collab_tool_call","tool":"spawn_agent","sender_thread_id":"tid-collab","status":"completed"}}`)
		fmt.Fprintln(os.Stdout, `{"type":"item.started","item":{"id":"item_2","type":"collab_tool_call","tool":"wait","sender_thread_id":"tid-collab","status":"in_progress"}}`)
		fmt.Fprintln(os.Stdout, `{"type":"item.completed","item":{"id":"item_2","type":"collab_tool_call","tool":"wait","sender_thread_id":"tid-collab","status":"completed"}}`)
		fmt.Fprintln(os.Stdout, `{"type":"item.completed","item":{"id":"item_3","type":"agent_message","text":"Worker finished."}}`)
		fmt.Fprintln(os.Stdout, `{"type":"turn.completed","usage":{"input_tokens":10,"output_tokens":5}}`)
	case "codex_stream_error":
		fmt.Fprintln(os.Stdout, `{"type":"error","message":"rate limit exceeded"}`)
	case "codex_stream_turn_failed":
		fmt.Fprintln(os.Stdout, `{"type":"turn.started"}`)
		fmt.Fprintln(os.Stdout, `{"type":"turn.failed","message":"context length exceeded"}`)
	case "codex_stream_reasoning":
		fmt.Fprintln(os.Stdout, `{"type":"thread.started","thread_id":"tid-456"}`)
		fmt.Fprintln(os.Stdout, `{"type":"item.completed","item":{"id":"item_1","type":"reasoning","text":"Let me think about this..."}}`)
		fmt.Fprintln(os.Stdout, `{"type":"item.completed","item":{"id":"item_2","type":"agent_message","text":"Here is my answer."}}`)
		fmt.Fprintln(os.Stdout, `{"type":"turn.completed","usage":{"input_tokens":100,"output_tokens":30}}`)
	case "codex_stream_no_agent_message":
		fmt.Fprintln(os.Stdout, `{"type":"thread.started","thread_id":"tid-789"}`)
		fmt.Fprintln(os.Stdout, `{"type":"turn.started"}`)
		fmt.Fprintln(os.Stdout, `{"type":"turn.completed","usage":{"input_tokens":100,"output_tokens":0}}`)
	case "codex_stream_truncated":
		fmt.Fprintln(os.Stdout, `{"type":"thread.started","thread_id":"tid-truncated"}`)
		fmt.Fprintln(os.Stdout, `{"type":"turn.started"}`)
		fmt.Fprintln(os.Stdout, `{"type":"item.completed","item":{"id":"item_1","type":"agent_message","text":"Waiting for worker to finish."}}`)
	case "codex_stream_idle_timeout":
		fmt.Fprintln(os.Stdout, `{"type":"thread.started","thread_id":"tid-timeout"}`)
		fmt.Fprintln(os.Stdout, `{"type":"turn.started"}`)
		fmt.Fprintln(os.Stdout, `{"type":"item.completed","item":{"id":"item_1","type":"agent_message","text":"Waiting for worker to finish."}}`)
		time.Sleep(5 * time.Second)
	case "codex_call_rate_limit":
		fmt.Fprint(os.Stderr, "rate limit exceeded")
		os.Exit(1)
	}
	os.Exit(0)
}

func codexMockCmdBuilder(testCase string) CommandBuilder {
	return func(ctx context.Context, name string, args ...string) *exec.Cmd {
		cs := []string{"-test.run=TestCodexHelperProcess", "--"}
		cs = append(cs, args...)
		cmd := exec.CommandContext(ctx, os.Args[0], cs...)
		cmd.Env = append(os.Environ(), "GO_TEST_PROCESS=1", "GO_TEST_CASE="+testCase)
		return cmd
	}
}

func TestCodexCliDriver_Call_Success(t *testing.T) {
	t.Parallel()
	d := NewCodexCliDriver(CodexWithCommandBuilder(codexMockCmdBuilder("codex_call_success")))
	resp, err := d.Call(context.Background(), LLMRequest{Intent: "summarize the repo"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Content != "The repository structure looks good." {
		t.Errorf("expected content, got %q", resp.Content)
	}
	// Non-JSON mode: no token stats available.
	if resp.TokensUsed != 0 {
		t.Errorf("expected tokens_used 0 in non-JSON mode, got %d", resp.TokensUsed)
	}
}

func TestCodexCliDriver_Call_Error(t *testing.T) {
	t.Parallel()
	d := NewCodexCliDriver(CodexWithCommandBuilder(codexMockCmdBuilder("codex_call_error")))
	_, err := d.Call(context.Background(), LLMRequest{Intent: "test"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	var llmErr *LLMError
	if !errors.As(err, &llmErr) {
		t.Fatal("expected errors.As to extract *LLMError")
	}
	if llmErr.Provider != "codex" {
		t.Errorf("Provider = %q, want %q", llmErr.Provider, "codex")
	}
	// "auth" keyword should classify as ErrAuth.
	if !errors.Is(err, ErrAuth) {
		t.Errorf("expected errors.Is(err, ErrAuth), got: %v", err)
	}
}

func TestCodexCliDriver_Call_Empty(t *testing.T) {
	t.Parallel()
	d := NewCodexCliDriver(CodexWithCommandBuilder(codexMockCmdBuilder("codex_call_empty")))
	_, err := d.Call(context.Background(), LLMRequest{Intent: "test"})
	if err == nil {
		t.Fatal("expected error for empty output, got nil")
	}
	var llmErr *LLMError
	if !errors.As(err, &llmErr) {
		t.Fatal("expected errors.As to extract *LLMError")
	}
	if !strings.Contains(llmErr.Error(), "truncated") {
		t.Errorf("expected truncation error, got: %v", llmErr)
	}
}

func TestCodexCliDriver_Call_Timeout(t *testing.T) {
	t.Parallel()
	d := NewCodexCliDriver(
		CodexWithCommandBuilder(codexMockCmdBuilder("codex_timeout")),
		CodexWithTimeout(200*time.Millisecond),
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
	if llmErr.Provider != "codex" {
		t.Errorf("Provider = %q, want %q", llmErr.Provider, "codex")
	}
}

func TestCodexCliDriver_Call_RateLimit(t *testing.T) {
	t.Parallel()
	d := NewCodexCliDriver(CodexWithCommandBuilder(codexMockCmdBuilder("codex_call_rate_limit")))
	_, err := d.Call(context.Background(), LLMRequest{Intent: "test"})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.Is(err, ErrRateLimit) {
		t.Errorf("expected errors.Is(err, ErrRateLimit), got: %v", err)
	}
}

func TestCodexCliDriver_Call_Args(t *testing.T) {
	t.Parallel()
	var capturedArgs []string
	d := NewCodexCliDriver(CodexWithCommandBuilder(func(ctx context.Context, name string, args ...string) *exec.Cmd {
		capturedArgs = args
		return codexMockCmdBuilder("codex_call_success")(ctx, name, args...)
	}))

	_, err := d.Call(context.Background(), LLMRequest{
		Intent: "analyze code",
		Model:  "o3",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	argsStr := strings.Join(capturedArgs, " ")
	if !strings.Contains(argsStr, "exec") {
		t.Errorf("expected 'exec' subcommand, got: %s", argsStr)
	}
	if !argsContainPair(capturedArgs, "--sandbox", "workspace-write") {
		t.Errorf("expected --sandbox workspace-write flag, got: %s", argsStr)
	}
	if strings.Contains(argsStr, "--full-auto") {
		t.Errorf("unexpected --full-auto flag, got: %s", argsStr)
	}
	if !strings.Contains(argsStr, "-m o3") {
		t.Errorf("expected -m o3, got: %s", argsStr)
	}
	// Call mode should not have --json.
	if strings.Contains(argsStr, "--json") {
		t.Errorf("unexpected --json in call mode, got: %s", argsStr)
	}
}

func TestCodexCliDriver_Call_DefaultArgs(t *testing.T) {
	t.Parallel()
	var capturedArgs []string
	d := NewCodexCliDriver(CodexWithCommandBuilder(func(ctx context.Context, name string, args ...string) *exec.Cmd {
		capturedArgs = args
		return codexMockCmdBuilder("codex_call_success")(ctx, name, args...)
	}))

	_, err := d.Call(context.Background(), LLMRequest{Intent: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	argsStr := strings.Join(capturedArgs, " ")
	if !strings.Contains(argsStr, "-m "+CodexDefaultModel) {
		t.Errorf("expected default model %q, got: %s", CodexDefaultModel, argsStr)
	}
	if !argsContainPair(capturedArgs, "--sandbox", "workspace-write") {
		t.Errorf("expected --sandbox workspace-write, got: %s", argsStr)
	}
	if strings.Contains(argsStr, "--full-auto") {
		t.Errorf("unexpected --full-auto, got: %s", argsStr)
	}
	// Should NOT contain Claude/Qwen-specific flags.
	if strings.Contains(argsStr, "--bare") {
		t.Errorf("unexpected --bare flag, got: %s", argsStr)
	}
	if strings.Contains(argsStr, "--tools") {
		t.Errorf("unexpected --tools flag, got: %s", argsStr)
	}
	if strings.Contains(argsStr, "--output-format") {
		t.Errorf("unexpected --output-format flag, got: %s", argsStr)
	}
}

// TestCodexCliDriver_Stream_CollabToolCalls verifies multi_agent_v2 collab
// items (spawn_agent/wait) surface as tool_call events, agent_message content
// carries the subtype marker, and unknown item types (error) pass through as
// generic "item" events instead of being silently dropped.
// Investigation: codex-cli-observability-parity (R2).
func TestCodexCliDriver_Stream_CollabToolCalls(t *testing.T) {
	t.Parallel()
	d := NewCodexCliDriver(CodexWithCommandBuilder(codexMockCmdBuilder("codex_stream_collab")))
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

	// spawn_agent + wait: started/completed pairs as tool_call events with
	// the collab tool name and call_id.
	for _, want := range []struct{ tool, callID string }{
		{"spawn_agent", "item_1"},
		{"wait", "item_2"},
	} {
		var started, completed bool
		for _, e := range events {
			if e.Type != "tool_call" || e.Data["tool"] != want.tool || e.Data["call_id"] != want.callID {
				continue
			}
			switch e.Content {
			case "started":
				started = true
			case "completed":
				completed = true
				if e.Data["result"] != "completed" {
					t.Errorf("collab %s completed: expected result=completed, got %v", want.tool, e.Data["result"])
				}
			}
		}
		if !started || !completed {
			t.Errorf("collab %s: started=%v completed=%v, types: %v", want.tool, started, completed, typeSeq)
		}
	}

	// agent_message content carries the message-level subtype marker.
	found := false
	for _, e := range events {
		if e.Type == "content" && e.Content == "Worker finished." {
			if e.Data["subtype"] == "agent_message" {
				found = true
			}
			break
		}
	}
	if !found {
		t.Errorf("expected content event with subtype=agent_message, got types: %v", typeSeq)
	}

	// Unknown "error" item passes through as a generic item event.
	found = false
	for _, e := range events {
		if e.Type == "item" && e.Data["item_type"] == "error" {
			if msg, _ := e.Data["message"].(string); strings.Contains(msg, "multi_agent_v2") {
				found = true
			}
			break
		}
	}
	if !found {
		t.Errorf("expected pass-through item event for error item, got types: %v", typeSeq)
	}
}

func TestCodexCliDriver_Stream_Success(t *testing.T) {
	t.Parallel()
	d := NewCodexCliDriver(CodexWithCommandBuilder(codexMockCmdBuilder("codex_stream_success")))
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

	// Verify system event (thread.started).
	found := false
	for _, e := range events {
		if e.Type == "system" && e.Content == "init" {
			if e.Data["thread_id"] == "tid-123" {
				found = true
			}
			break
		}
	}
	if !found {
		t.Errorf("expected system:init event with thread_id, got types: %v", typeSeq)
	}

	// Verify tool_call started event.
	found = false
	for _, e := range events {
		if e.Type == "tool_call" && e.Content == "started" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected tool_call:started event, got types: %v", typeSeq)
	}

	// Verify tool_call completed event carries the aggregated output under
	// the "result" key (kernel backfill whitelist) plus the exit code.
	found = false
	for _, e := range events {
		if e.Type == "tool_call" && e.Content == "completed" {
			if e.Data["result"] == "file1.go\nfile2.go" && e.Data["exit_code"] == 0 {
				found = true
			}
			break
		}
	}
	if !found {
		t.Errorf("expected tool_call:completed event with result+exit_code, got types: %v", typeSeq)
	}

	// Verify content event (from agent_message).
	found = false
	for _, e := range events {
		if e.Type == "content" && strings.Contains(e.Content, "Found 2 Go files") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected content event with agent message, got types: %v", typeSeq)
	}

	// Verify done event at end.
	last := events[len(events)-1]
	if last.Type != "done" {
		t.Errorf("expected last event to be done, got %q", last.Type)
	}
	if last.InputTokens != 500 {
		t.Errorf("expected input_tokens 500, got %d", last.InputTokens)
	}
	if last.OutputTokens != 50 {
		t.Errorf("expected output_tokens 50, got %d", last.OutputTokens)
	}
	if last.CachedInputTokens != 400 {
		t.Errorf("expected cached_input_tokens 400, got %d", last.CachedInputTokens)
	}
	if last.TokensUsed != 550 {
		t.Errorf("expected tokens_used 550, got %d", last.TokensUsed)
	}
}

func TestCodexCliDriver_Stream_Error(t *testing.T) {
	t.Parallel()
	d := NewCodexCliDriver(CodexWithCommandBuilder(codexMockCmdBuilder("codex_stream_error")))
	ch, err := d.Stream(context.Background(), LLMRequest{Intent: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var events []StreamEvent
	for evt := range ch {
		events = append(events, evt)
	}

	// Should have error event + done/error for no agent message.
	hasError := false
	for _, e := range events {
		if e.Type == "error" && e.Err != nil {
			hasError = true
			if !errors.Is(e.Err, ErrRateLimit) {
				t.Errorf("expected rate limit error classification, got: %v", e.Err)
			}
			break
		}
	}
	if !hasError {
		t.Error("expected at least one error event")
	}
}

func TestCodexCliDriver_Stream_TurnFailed(t *testing.T) {
	t.Parallel()
	d := NewCodexCliDriver(CodexWithCommandBuilder(codexMockCmdBuilder("codex_stream_turn_failed")))
	ch, err := d.Stream(context.Background(), LLMRequest{Intent: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var events []StreamEvent
	for evt := range ch {
		events = append(events, evt)
	}

	hasError := false
	for _, e := range events {
		if e.Type == "error" && e.Err != nil {
			hasError = true
			var llmErr *LLMError
			if errors.As(e.Err, &llmErr) {
				if !strings.Contains(llmErr.Error(), "context length") {
					t.Errorf("expected context length in error, got: %v", llmErr)
				}
			}
			break
		}
	}
	if !hasError {
		t.Error("expected error event for turn.failed")
	}
}

func TestCodexCliDriver_Stream_Reasoning(t *testing.T) {
	t.Parallel()
	d := NewCodexCliDriver(CodexWithCommandBuilder(codexMockCmdBuilder("codex_stream_reasoning")))
	ch, err := d.Stream(context.Background(), LLMRequest{Intent: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var events []StreamEvent
	for evt := range ch {
		events = append(events, evt)
	}

	// Verify thinking event.
	found := false
	for _, e := range events {
		if e.Type == "thinking" && strings.Contains(e.Content, "think about this") {
			found = true
			break
		}
	}
	if !found {
		typeSeq := make([]string, len(events))
		for i, e := range events {
			typeSeq[i] = e.Type
		}
		t.Errorf("expected thinking event, got types: %v", typeSeq)
	}

	// Verify done event.
	last := events[len(events)-1]
	if last.Type != "done" {
		t.Errorf("expected done, got %q", last.Type)
	}
	if last.Content != "Here is my answer." {
		t.Errorf("expected agent message in done, got %q", last.Content)
	}
}

func TestCodexCliDriver_Stream_NoAgentMessage(t *testing.T) {
	t.Parallel()
	d := NewCodexCliDriver(CodexWithCommandBuilder(codexMockCmdBuilder("codex_stream_no_agent_message")))
	ch, err := d.Stream(context.Background(), LLMRequest{Intent: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var events []StreamEvent
	for evt := range ch {
		events = append(events, evt)
	}

	// Last event should be error (no agent message).
	hasErr := false
	for _, e := range events {
		if e.Type == "error" && e.Err != nil {
			if strings.Contains(e.Err.Error(), "truncated") {
				hasErr = true
			}
		}
	}
	if !hasErr {
		typeSeq := make([]string, len(events))
		for i, e := range events {
			typeSeq[i] = e.Type
		}
		t.Errorf("expected truncation error event, got types: %v", typeSeq)
	}
}

func TestCodexCliDriver_Stream_TruncatedAfterAgentMessage(t *testing.T) {
	t.Parallel()
	d := NewCodexCliDriver(CodexWithCommandBuilder(codexMockCmdBuilder("codex_stream_truncated")))
	ch, err := d.Stream(context.Background(), LLMRequest{Intent: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	streamErr := collectStreamError(t, ch)
	if streamErr == nil {
		t.Fatal("expected truncated stream error, got nil")
	}
	if !strings.Contains(streamErr.Error(), "stream ended before turn.completed") {
		t.Fatalf("expected turn.completed truncation detail, got: %v", streamErr)
	}
}

func TestCodexCliDriver_Stream_IdleTimeoutAfterAgentMessage(t *testing.T) {
	t.Parallel()
	d := NewCodexCliDriver(
		CodexWithCommandBuilder(codexMockCmdBuilder("codex_stream_idle_timeout")),
		CodexWithTimeout(100*time.Millisecond),
	)
	ch, err := d.Stream(context.Background(), LLMRequest{Intent: "test"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	streamErr := collectStreamError(t, ch)
	if streamErr == nil {
		t.Fatal("expected timeout stream error, got nil")
	}
	if !errors.Is(streamErr, ErrTimeout) {
		t.Fatalf("expected ErrTimeout sentinel, got: %v", streamErr)
	}
}

func TestCodexCliDriver_Stream_Args(t *testing.T) {
	t.Parallel()
	var capturedArgs []string
	d := NewCodexCliDriver(CodexWithCommandBuilder(func(ctx context.Context, name string, args ...string) *exec.Cmd {
		capturedArgs = args
		return codexMockCmdBuilder("codex_stream_success")(ctx, name, args...)
	}))

	ch, err := d.Stream(context.Background(), LLMRequest{Intent: "test", Model: "o4-mini"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for range ch {
		// drain
	}

	argsStr := strings.Join(capturedArgs, " ")
	if !strings.Contains(argsStr, "--json") {
		t.Errorf("expected --json in stream mode, got: %s", argsStr)
	}
	if !argsContainPair(capturedArgs, "--sandbox", "workspace-write") {
		t.Errorf("expected --sandbox workspace-write, got: %s", argsStr)
	}
	if strings.Contains(argsStr, "--full-auto") {
		t.Errorf("unexpected --full-auto, got: %s", argsStr)
	}
	if !strings.Contains(argsStr, "-m o4-mini") {
		t.Errorf("expected -m o4-mini, got: %s", argsStr)
	}
}

func TestCodexCliDriver_Info(t *testing.T) {
	d := NewCodexCliDriver(CodexWithModel("o3"))
	info := d.Info()
	if info.Name != "codex-cli" {
		t.Errorf("expected name 'codex-cli', got %q", info.Name)
	}
	if info.Provider != "codex" {
		t.Errorf("expected provider 'codex', got %q", info.Provider)
	}
	if info.DefaultModel != "o3" {
		t.Errorf("expected default model 'o3', got %q", info.DefaultModel)
	}
	if info.DriverType != DriverCodexCLI {
		t.Errorf("expected driver type %q, got %q", DriverCodexCLI, info.DriverType)
	}
}

func TestCodexCliDriver_Options(t *testing.T) {
	t.Parallel()
	t.Run("CodexWithModel", func(t *testing.T) {
		d := NewCodexCliDriver(CodexWithModel("o3"))
		if d.defaultModel != "o3" {
			t.Errorf("expected model 'o3', got %q", d.defaultModel)
		}
	})

	t.Run("CodexWithTimeout", func(t *testing.T) {
		d := NewCodexCliDriver(CodexWithTimeout(60 * time.Second))
		if d.defaultTimeout != 60*time.Second {
			t.Errorf("expected timeout 60s, got %v", d.defaultTimeout)
		}
	})

	t.Run("CodexWithCommand", func(t *testing.T) {
		d := NewCodexCliDriver(CodexWithCommand("my-codex"))
		if d.cliCommand != "my-codex" {
			t.Errorf("expected command 'my-codex', got %q", d.cliCommand)
		}
	})

	t.Run("CodexWithCommandBuilder", func(t *testing.T) {
		called := false
		cb := func(ctx context.Context, name string, args ...string) *exec.Cmd {
			called = true
			return codexMockCmdBuilder("codex_call_success")(ctx, name, args...)
		}
		d := NewCodexCliDriver(CodexWithCommandBuilder(cb))
		_, _ = d.Call(context.Background(), LLMRequest{Intent: "test"})
		if !called {
			t.Error("custom command builder was not called")
		}
	})

	t.Run("CodexWithExtraArgs", func(t *testing.T) {
		var capturedArgs []string
		d := NewCodexCliDriver(
			CodexWithExtraArgs([]string{"--ephemeral", "--skip-git-repo-check"}),
			CodexWithCommandBuilder(func(ctx context.Context, name string, args ...string) *exec.Cmd {
				capturedArgs = args
				return codexMockCmdBuilder("codex_call_success")(ctx, name, args...)
			}),
		)
		_, _ = d.Call(context.Background(), LLMRequest{Intent: "test"})
		argsStr := strings.Join(capturedArgs, " ")
		if !strings.Contains(argsStr, "--ephemeral") {
			t.Errorf("expected --ephemeral in args, got: %s", argsStr)
		}
		if !strings.Contains(argsStr, "--skip-git-repo-check") {
			t.Errorf("expected --skip-git-repo-check in args, got: %s", argsStr)
		}
	})
}

func TestCodexCliDriver_BuildPrompt_SingleTurn(t *testing.T) {
	d := NewCodexCliDriver()
	req := LLMRequest{Intent: "analyze code"}
	prompt := d.buildPrompt(req)
	if prompt != "analyze code" {
		t.Errorf("expected 'analyze code', got %q", prompt)
	}
}

func TestCodexCliDriver_BuildPrompt_WithSystemPrompt(t *testing.T) {
	d := NewCodexCliDriver()
	req := LLMRequest{
		Intent:       "analyze code",
		SystemPrompt: "You are a code reviewer.",
	}
	prompt := d.buildPrompt(req)
	if !strings.Contains(prompt, "[System Instructions]") {
		t.Errorf("expected system instructions prefix, got: %s", prompt)
	}
	if !strings.Contains(prompt, "You are a code reviewer.") {
		t.Errorf("expected system prompt content, got: %s", prompt)
	}
	if !strings.Contains(prompt, "[Task]") {
		t.Errorf("expected [Task] label, got: %s", prompt)
	}
	if !strings.Contains(prompt, "analyze code") {
		t.Errorf("expected intent, got: %s", prompt)
	}
}

func TestCodexCliDriver_BuildPrompt_MultiTurn(t *testing.T) {
	// CLI Agents manage their own agent loop internally, so each Call is an
	// independent task — buildPrompt only returns the intent, ignoring Messages.
	d := NewCodexCliDriver()
	req := LLMRequest{
		Intent: "fix the bug",
		Messages: []Message{
			{Role: "user", Content: "hello"},
			{Role: "assistant", Content: "hi there"},
			{Role: "tool", ToolCallID: "tc1", Content: "result data"},
			{Role: "user", Content: "fix this"},
		},
	}
	prompt := d.buildPrompt(req)
	if prompt != "fix the bug" {
		t.Errorf("expected intent-only prompt, got: %s", prompt)
	}
}
