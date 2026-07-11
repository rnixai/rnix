package llm

// ATDD for Story 66.6 — driver-layer mid-stream usage events.
//
// AC1/AC7 (driver 侧): claude-cli assistant frames and codex-cli turn.completed
// events must surface a StreamEvent{Type:"usage"} carrying that round-trip's
// DELTA, so the kernel can preview token growth during a long CLI session.
// 拍板 8: claude subagent frames (parent_tool_use_id != "") must NOT emit usage
// so the tally stays consistent with the final `done`.
//
// Injection mirrors the Story 56.6 pattern: self-contained helper processes
// re-exec'd via isolated env guards (GO_TEST_PROCESS_666_*), so the shared
// TestHelperProcess switches are untouched.
//
// Red mechanism (记忆 atdd-code-story-red-mechanism-preference): these are the
// dev-story green assertions; the RED proof is described inline (comment out the
// driver emit → the usage-count assertion turns FAIL). Verified during dev.

import (
	"context"
	"os"
	"os/exec"
	"testing"
)

// TestHelperProcessClaudeUsage666 emits a claude stream-json session with a
// main-thread assistant frame carrying message.usage, a SUBAGENT assistant
// frame also carrying usage (must be excluded), and a final result. Only runs
// under GO_TEST_PROCESS_666_CLAUDE=1.
func TestHelperProcessClaudeUsage666(t *testing.T) {
	if os.Getenv("GO_TEST_PROCESS_666_CLAUDE") != "1" {
		return
	}
	lines := []string{
		// main-thread assistant WITH per-round-trip usage → emits a usage event.
		`{"type":"assistant","message":{"id":"msg_main","content":[{"type":"text","text":"working"}],"usage":{"input_tokens":100,"cache_read_input_tokens":20,"output_tokens":30}}}`,
		// subagent assistant WITH usage (parent_tool_use_id set) → must NOT emit.
		`{"type":"assistant","parent_tool_use_id":"toolu_task1","session_id":"sess_1","message":{"id":"msg_sub","content":[{"type":"text","text":"sub"}],"usage":{"input_tokens":50,"output_tokens":10}}}`,
		// result → done event, session-total usage (authoritative).
		`{"type":"result","subtype":"success","result":"done","is_error":false,"usage":{"input_tokens":200,"cache_read_input_tokens":40,"output_tokens":50}}`,
	}
	for _, l := range lines {
		_, _ = os.Stdout.WriteString(l + "\n")
	}
	os.Exit(0)
}

func claudeUsage666CmdBuilder() CommandBuilder {
	return func(ctx context.Context, _ string, args ...string) *exec.Cmd {
		cs := append([]string{"-test.run=TestHelperProcessClaudeUsage666", "--"}, args...)
		cmd := exec.CommandContext(ctx, os.Args[0], cs...)
		cmd.Env = append(os.Environ(), "GO_TEST_PROCESS_666_CLAUDE=1")
		return cmd
	}
}

// TestHelperProcessCodexUsage666 emits a codex --json session with TWO
// turn.completed events (per-turn deltas) so the accumulation + delta emit can
// both be asserted. Only runs under GO_TEST_PROCESS_666_CODEX=1.
func TestHelperProcessCodexUsage666(t *testing.T) {
	if os.Getenv("GO_TEST_PROCESS_666_CODEX") != "1" {
		return
	}
	lines := []string{
		`{"type":"thread.started","thread_id":"tid-666"}`,
		`{"type":"turn.started"}`,
		`{"type":"turn.completed","usage":{"input_tokens":100,"cached_input_tokens":10,"output_tokens":30}}`,
		`{"type":"turn.started"}`,
		`{"type":"turn.completed","usage":{"input_tokens":50,"cached_input_tokens":5,"output_tokens":20}}`,
		`{"type":"item.completed","item":{"id":"item_1","type":"agent_message","text":"done"}}`,
	}
	for _, l := range lines {
		_, _ = os.Stdout.WriteString(l + "\n")
	}
	os.Exit(0)
}

func codexUsage666CmdBuilder() CommandBuilder {
	return func(ctx context.Context, _ string, args ...string) *exec.Cmd {
		cs := append([]string{"-test.run=TestHelperProcessCodexUsage666", "--"}, args...)
		cmd := exec.CommandContext(ctx, os.Args[0], cs...)
		cmd.Env = append(os.Environ(), "GO_TEST_PROCESS_666_CODEX=1")
		return cmd
	}
}

func collectStream666(t *testing.T, d LLMDriver) []StreamEvent {
	t.Helper()
	ch, err := d.Stream(context.Background(), LLMRequest{Intent: "long task"})
	if err != nil {
		t.Fatalf("Stream() error: %v", err)
	}
	var events []StreamEvent
	for evt := range ch {
		events = append(events, evt)
	}
	return events
}

// -----------------------------------------------------------------------------
// 66-6-DRV-001 (AC1/AC7): claude main-thread assistant frame emits exactly one
// usage event with the round-trip DELTA. RED: without the assistant-case emit
// (or the Message.Usage field), zero usage events are produced.
// -----------------------------------------------------------------------------
func TestATDD_66_6_DRV_001_ClaudeMainThreadEmitsUsageDelta(t *testing.T) {
	d := NewClaudeCliDriver(WithCommandBuilder(claudeUsage666CmdBuilder()))
	events := collectStream666(t, d)

	var usageEvents []StreamEvent
	for _, e := range events {
		if e.Type == "usage" {
			usageEvents = append(usageEvents, e)
		}
	}
	if len(usageEvents) != 1 {
		t.Fatalf("AC1 FAIL: 期望恰好 1 个 usage 事件（仅主线程），got %d", len(usageEvents))
	}
	u := usageEvents[0]
	if u.TokensUsed != 130 || u.InputTokens != 100 || u.OutputTokens != 30 || u.CachedInputTokens != 20 {
		t.Errorf("AC1 FAIL: usage delta 错误 tokens=%d in=%d out=%d cached=%d, want 130/100/30/20",
			u.TokensUsed, u.InputTokens, u.OutputTokens, u.CachedInputTokens)
	}
}

// -----------------------------------------------------------------------------
// 66-6-DRV-002 (AC7/拍板 8): claude subagent frame (parent_tool_use_id set) does
// NOT emit a usage event even though message.usage is non-zero. This keeps the
// live tally consistent with the final `done` (subagent billing口径不确定，取安全侧).
// The single usage event proven by DRV-001 is the main-thread one; if the
// subagent gate were removed, this test would see 2.
// -----------------------------------------------------------------------------
func TestATDD_66_6_DRV_002_ClaudeSubagentFrameOmitsUsage(t *testing.T) {
	d := NewClaudeCliDriver(WithCommandBuilder(claudeUsage666CmdBuilder()))
	events := collectStream666(t, d)

	usageCount := 0
	for _, e := range events {
		if e.Type == "usage" {
			usageCount++
			// The only legitimate usage event is the main-thread 130-token one.
			if e.TokensUsed == 60 {
				t.Error("AC7 FAIL: subagent 帧（50+10）不应产生 usage 事件")
			}
		}
	}
	if usageCount != 1 {
		t.Fatalf("AC7 FAIL: 期望 1 个 usage 事件，got %d（subagent 帧疑似泄漏 usage）", usageCount)
	}
}

// -----------------------------------------------------------------------------
// 66-6-DRV-003 (AC4): claude `done` still carries the authoritative session
// total, independent of the mid-stream usage events.
// -----------------------------------------------------------------------------
func TestATDD_66_6_DRV_003_ClaudeDoneAuthoritativeUnchanged(t *testing.T) {
	d := NewClaudeCliDriver(WithCommandBuilder(claudeUsage666CmdBuilder()))
	events := collectStream666(t, d)

	var done *StreamEvent
	for i := range events {
		if events[i].Type == "done" {
			done = &events[i]
		}
	}
	if done == nil {
		t.Fatal("setup: 无 done 事件")
	}
	if done.TokensUsed != 250 || done.InputTokens != 200 || done.OutputTokens != 50 {
		t.Errorf("AC4 FAIL: done 权威值错误 tokens=%d in=%d out=%d, want 250/200/50",
			done.TokensUsed, done.InputTokens, done.OutputTokens)
	}
}

// -----------------------------------------------------------------------------
// 66-6-DRV-004 (AC1): codex turn.completed emits a per-turn usage DELTA event
// for EACH turn (two here), and the final `done` carries the accumulated total.
// RED: without the turn.completed emit, zero usage events are produced.
// -----------------------------------------------------------------------------
func TestATDD_66_6_DRV_004_CodexTurnCompletedEmitsUsageDeltas(t *testing.T) {
	d := NewCodexCliDriver(CodexWithCommandBuilder(codexUsage666CmdBuilder()))
	events := collectStream666(t, d)

	var deltas []int
	var done *StreamEvent
	for i := range events {
		switch events[i].Type {
		case "usage":
			deltas = append(deltas, events[i].TokensUsed)
		case "done":
			done = &events[i]
		}
	}
	if len(deltas) != 2 {
		t.Fatalf("AC1 FAIL: 期望 2 个 codex usage 事件，got %d", len(deltas))
	}
	if deltas[0] != 130 || deltas[1] != 70 {
		t.Errorf("AC1 FAIL: codex usage delta 错误 got %v, want [130 70]", deltas)
	}
	if done == nil || done.TokensUsed != 200 {
		t.Errorf("AC4 FAIL: codex done 累计总量错误 got %v, want 200", done)
	}
}
