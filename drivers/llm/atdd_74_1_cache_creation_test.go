package llm

// ATDD for Story 74.1 — driver 层 cache creation 取值（AC1）与零值透传（AC1-5/NFR2）。
//
//   - anthropic Call 路径断言已加在既有 TestAnthropicDriver_ConvertMessage_CachedTokens
//     （fixture 已含 cache_creation_input_tokens: 10，不新建重复 fixture）。
//   - 本文件覆盖：anthropic stream done 事件、claude-cli Call + Stream 两路径、
//     5 个无源 driver（openai-official/gemini/codex-cli/cursor-cli/qwen-cli）零值断言、
//     vfsfile.go stream 桥搬运（修正 2）。
//
// claude-cli 注入沿用 Story 66.6 模式：自包含 helper 进程 + 隔离 env guard
// （GO_TEST_PROCESS_741_*），不触碰共享 TestHelperProcess switch。

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	"google.golang.org/genai"
)

// TestHelperProcessClaudeCacheCreation741 emits a claude stream-json session
// whose assistant frame carries message.usage WITH cache_creation_input_tokens
// (mid-stream usage event source) and whose result frame carries usage with
// creation too (done event source). Only runs under GO_TEST_PROCESS_741_CLAUDE=1.
func TestHelperProcessClaudeCacheCreation741(t *testing.T) {
	if os.Getenv("GO_TEST_PROCESS_741_CLAUDE") != "1" {
		return
	}
	lines := []string{
		// main-thread assistant WITH per-round-trip usage incl. creation.
		`{"type":"assistant","message":{"id":"msg_main","content":[{"type":"text","text":"working"}],"usage":{"input_tokens":100,"cache_read_input_tokens":20,"cache_creation_input_tokens":15,"output_tokens":30}}}`,
		// result → done event, session-total usage incl. creation (authoritative).
		`{"type":"result","subtype":"success","result":"done","is_error":false,"usage":{"input_tokens":200,"cache_read_input_tokens":40,"cache_creation_input_tokens":25,"output_tokens":50}}`,
	}
	for _, l := range lines {
		_, _ = os.Stdout.WriteString(l + "\n")
	}
	os.Exit(0)
}

func claudeCacheCreation741CmdBuilder() CommandBuilder {
	return func(ctx context.Context, _ string, args ...string) *exec.Cmd {
		cs := append([]string{"-test.run=TestHelperProcessClaudeCacheCreation741", "--"}, args...)
		cmd := exec.CommandContext(ctx, os.Args[0], cs...)
		cmd.Env = append(os.Environ(), "GO_TEST_PROCESS_741_CLAUDE=1")
		return cmd
	}
}

// TestHelperProcessClaudeCallCacheCreation741 emits a claude JSON-mode result
// whose structured Usage block carries cache_creation_input_tokens (the only
// place creation exists — no flat legacy field). Runs under
// GO_TEST_PROCESS_741_CLAUDE_CALL=1.
func TestHelperProcessClaudeCallCacheCreation741(t *testing.T) {
	if os.Getenv("GO_TEST_PROCESS_741_CLAUDE_CALL") != "1" {
		return
	}
	_, _ = os.Stdout.WriteString(`{"type":"result","subtype":"success","result":"ok","is_error":false,"usage":{"input_tokens":100,"cache_read_input_tokens":50,"cache_creation_input_tokens":30,"output_tokens":20}}`)
	os.Exit(0)
}

func claudeCallCacheCreation741CmdBuilder() CommandBuilder {
	return func(ctx context.Context, _ string, args ...string) *exec.Cmd {
		cs := append([]string{"-test.run=TestHelperProcessClaudeCallCacheCreation741", "--"}, args...)
		cmd := exec.CommandContext(ctx, os.Args[0], cs...)
		cmd.Env = append(os.Environ(), "GO_TEST_PROCESS_741_CLAUDE_CALL=1")
		return cmd
	}
}

func collectStream741(t *testing.T, d LLMDriver) []StreamEvent {
	t.Helper()
	// 30s 超时（review 修复：原版 context.Background() 无时限，driver 回归
	// 永不关 channel 时挂死整个测试二进制，10 分钟默认超时才炸）。
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ch, err := d.Stream(ctx, LLMRequest{Intent: "long task"})
	if err != nil {
		t.Fatalf("Stream() error: %v", err)
	}
	var events []StreamEvent
	for evt := range ch {
		events = append(events, evt)
	}
	return events
}

// readBufferedResponse decodes the JSON buffered by LLMFile after Write.
func readBufferedResponse(t *testing.T, f *LLMFile) LLMResponse {
	t.Helper()
	buf, err := f.Read(1 << 20)
	if err != nil {
		t.Fatalf("LLMFile.Read: %v", err)
	}
	var resp LLMResponse
	if err := json.Unmarshal(buf, &resp); err != nil {
		t.Fatalf("unmarshal buffered response: %v", err)
	}
	return resp
}

// -----------------------------------------------------------------------------
// 74-1-ANTH-001 (AC1-2): anthropic stream 路径 — accumulated acc.Usage 带
// cache_creation_input_tokens → done 事件字段正确。
// RED: done 事件构造未加 CacheCreationInputTokens 时断言 FAIL。
// -----------------------------------------------------------------------------
func TestATDD_74_1_ANTH_001_StreamDoneCarriesCacheCreation(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl, _ := w.(http.Flusher)
		emit := func(event, data string) {
			_, _ = w.Write([]byte("event: " + event + "\ndata: " + data + "\n\n"))
			if fl != nil {
				fl.Flush()
			}
		}
		emit("message_start", `{"type":"message_start","message":{"id":"msg","type":"message","role":"assistant","content":[],"model":"claude","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":5,"output_tokens":0,"cache_read_input_tokens":3,"cache_creation_input_tokens":10}}}`)
		emit("content_block_start", `{"type":"content_block_start","index":0,"content_block":{"type":"text","text":""}}`)
		emit("content_block_delta", `{"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"ok"}}`)
		emit("content_block_stop", `{"type":"content_block_stop","index":0}`)
		emit("message_delta", `{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":1}}`)
		emit("message_stop", `{"type":"message_stop"}`)
	}))
	defer ts.Close()

	d := NewAnthropicDriver("test",
		WithAnthropicModel("claude-test"),
		WithAnthropicBaseURL(ts.URL),
		WithAnthropicHTTPClient(ts.Client()),
		WithAnthropicKey("sk-ant-741"),
		WithAnthropicMaxTokens(128),
	)

	events := collectStream741(t, d)
	var done *StreamEvent
	for i := range events {
		if events[i].Type == "done" {
			done = &events[i]
		}
	}
	if done == nil {
		t.Fatal("no done event received")
	}
	if done.CacheCreationInputTokens != 10 {
		t.Errorf("done.CacheCreationInputTokens = %d, want 10", done.CacheCreationInputTokens)
	}
	if done.CachedInputTokens != 3 {
		t.Errorf("done.CachedInputTokens = %d, want 3", done.CachedInputTokens)
	}
	// TokensUsed 语义不变：Input + Output（AC1-5：creation 不并入合计）。
	if done.TokensUsed != 6 {
		t.Errorf("done.TokensUsed = %d, want 6 (input 5 + output 1)", done.TokensUsed)
	}
}

// -----------------------------------------------------------------------------
// 74-1-ANTH-002 (AC1-2 补充): acc.Usage 累积源可读——creation 在 SDK 反序列化
// 后仍可从 acc 读到（streamInternal 构造 done 时取自同源）；populateDoneEventFromAcc
// 只填 content/reasoning/tool 字段，不触碰 usage（两处不重叠）。
// -----------------------------------------------------------------------------
func TestATDD_74_1_ANTH_002_AccumulatedUsageCarriesCreation(t *testing.T) {
	accJSON := `{
		"id": "msg_usage",
		"type": "message",
		"role": "assistant",
		"content": [{"type": "text", "text": "hello"}],
		"model": "claude-sonnet-4",
		"stop_reason": "end_turn",
		"stop_sequence": null,
		"usage": {"input_tokens": 7, "output_tokens": 2, "cache_read_input_tokens": 4, "cache_creation_input_tokens": 11}
	}`
	var acc anthropic.Message
	if err := json.Unmarshal([]byte(accJSON), &acc); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got := int(acc.Usage.CacheCreationInputTokens); got != 11 {
		t.Errorf("acc.Usage.CacheCreationInputTokens = %d, want 11", got)
	}
	evt := StreamEvent{Type: "done"}
	// 预置哨兵值：populateDoneEventFromAcc 只填 content/reasoning/tool 字段，
	// 不触碰 usage——预置后断言存活，把「两处不重叠」从注释声明变成真实断言
	// （review 修复：原版不预置即无法验证不触碰性）。
	evt.CacheCreationInputTokens = 999
	evt.CachedInputTokens = 888
	evt.TokensUsed = 777
	populateDoneEventFromAcc(&acc, &evt)
	// 无 leak 的纯 TextBlock 不设 evt.Content（既有契约，见
	// TestAnthropicDriver_StreamDone_DeepSeekTextLeak 的 no-leak 断言）——
	// 本测试只钉 acc.Usage 源可读 + 函数不触碰 usage 字段。
	if evt.Content != "" {
		t.Errorf("evt.Content = %q, want empty (no-leak path)", evt.Content)
	}
	if evt.CacheCreationInputTokens != 999 {
		t.Errorf("populateDoneEventFromAcc touched CacheCreationInputTokens: got %d, want sentinel 999 (usage 字段不重叠)", evt.CacheCreationInputTokens)
	}
	if evt.CachedInputTokens != 888 {
		t.Errorf("populateDoneEventFromAcc touched CachedInputTokens: got %d, want sentinel 888", evt.CachedInputTokens)
	}
	if evt.TokensUsed != 777 {
		t.Errorf("populateDoneEventFromAcc touched TokensUsed: got %d, want sentinel 777", evt.TokensUsed)
	}
}

// -----------------------------------------------------------------------------
// 74-1-CLI-001 (AC1-3): claude-cli Call 路径 — claudeCliResponse JSON（usage 含
// cache_creation_input_tokens）→ mergeClaudeUsage 返回 creation →
// LLMResponse.CacheCreationInputTokens 正确。
// -----------------------------------------------------------------------------
func TestATDD_74_1_CLI_001_ClaudeCallCarriesCacheCreation(t *testing.T) {
	d := NewClaudeCliDriver(WithCommandBuilder(claudeCallCacheCreation741CmdBuilder()))
	resp, err := d.Call(context.Background(), LLMRequest{Intent: "test"})
	if err != nil {
		t.Fatalf("Call() error: %v", err)
	}
	if resp.CacheCreationInputTokens != 30 {
		t.Errorf("CacheCreationInputTokens = %d, want 30", resp.CacheCreationInputTokens)
	}
	if resp.CachedInputTokens != 50 {
		t.Errorf("CachedInputTokens = %d, want 50", resp.CachedInputTokens)
	}
	if resp.TokensUsed != 120 {
		t.Errorf("TokensUsed = %d, want 120 (input 100 + output 20, creation 不并入)", resp.TokensUsed)
	}
}

// -----------------------------------------------------------------------------
// 74-1-CLI-002 (AC1-4): claude-cli Stream 路径 — assistant 帧（Message.Usage 带
// creation）→ "usage" 事件携带；result 帧（Usage 带 creation）→ "done" 事件携带。
// -----------------------------------------------------------------------------
func TestATDD_74_1_CLI_002_ClaudeStreamCarriesCacheCreation(t *testing.T) {
	d := NewClaudeCliDriver(WithCommandBuilder(claudeCacheCreation741CmdBuilder()))
	events := collectStream741(t, d)

	var usage *StreamEvent
	var done *StreamEvent
	for i := range events {
		switch events[i].Type {
		case "usage":
			u := events[i]
			usage = &u
		case "done":
			d2 := events[i]
			done = &d2
		}
	}

	if usage == nil {
		t.Fatal("no mid-stream usage event")
	}
	if usage.CacheCreationInputTokens != 15 {
		t.Errorf("usage.CacheCreationInputTokens = %d, want 15 (Message.Usage 携带)", usage.CacheCreationInputTokens)
	}
	if usage.CachedInputTokens != 20 {
		t.Errorf("usage.CachedInputTokens = %d, want 20", usage.CachedInputTokens)
	}

	if done == nil {
		t.Fatal("no done event")
	}
	if done.CacheCreationInputTokens != 25 {
		t.Errorf("done.CacheCreationInputTokens = %d, want 25 (result Usage 携带)", done.CacheCreationInputTokens)
	}
	if done.CachedInputTokens != 40 {
		t.Errorf("done.CachedInputTokens = %d, want 40", done.CachedInputTokens)
	}
}

// -----------------------------------------------------------------------------
// 74-1-VFS-001 (AC1 修正 3 / T2): vfsfile.go stream 桥 — claude stream 的 done
// 事件经 writeStream 归集进 LLMResponse（bufferResponse 后 Read 解码）。
// 零改动清单纪律：这是 stream 路径唯一搬运处，漏掉则 steps.jsonl 永远无 creation。
// -----------------------------------------------------------------------------
func TestATDD_74_1_VFS_001_StreamBridgeCarriesCacheCreation(t *testing.T) {
	d := NewClaudeCliDriver(WithCommandBuilder(claudeCacheCreation741CmdBuilder()))
	f := openLLMFile(t, d, "")
	defer f.Close()

	writeStringReq(t, f, `{"intent":"hi"}`)

	resp := readBufferedResponse(t, f)
	if resp.CacheCreationInputTokens != 25 {
		t.Errorf("resp.CacheCreationInputTokens = %d, want 25 (vfsfile done case 搬运)", resp.CacheCreationInputTokens)
	}
	if resp.CachedInputTokens != 40 {
		t.Errorf("resp.CachedInputTokens = %d, want 40", resp.CachedInputTokens)
	}
}

// -----------------------------------------------------------------------------
// 74-1-ZERO-001 (AC1-5): openai-official Call 路径 — 喂非零 usage（含
// prompt_tokens_details.cached_tokens）后断言 CacheCreationInputTokens 仍 0。
// -----------------------------------------------------------------------------
func TestATDD_74_1_ZERO_001_OpenAICallZeroCacheCreation(t *testing.T) {
	d, _, cleanup := newTestOpenAIDriver(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"id":"c","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":100,"completion_tokens":20,"total_tokens":120,"prompt_tokens_details":{"cached_tokens":80}}}`)
	})
	defer cleanup()

	resp, err := d.Call(context.Background(), LLMRequest{Intent: "hi"})
	if err != nil {
		t.Fatalf("Call() error: %v", err)
	}
	if resp.CachedInputTokens != 80 {
		t.Errorf("CachedInputTokens = %d, want 80 (cached 有数据源)", resp.CachedInputTokens)
	}
	if resp.CacheCreationInputTokens != 0 {
		t.Errorf("CacheCreationInputTokens = %d, want 0 (openai 无 creation 数据源，恒 0 透传)", resp.CacheCreationInputTokens)
	}
}

// -----------------------------------------------------------------------------
// 74-1-ZERO-002 (AC1-5): openai-official Stream 路径 — 同零值断言。
// -----------------------------------------------------------------------------
func TestATDD_74_1_ZERO_002_OpenAIStreamZeroCacheCreation(t *testing.T) {
	d, _, cleanup := newTestOpenAIDriver(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		writeOAISSE(w, `{"id":"c","object":"chat.completion.chunk","choices":[{"delta":{"content":"ok"},"index":0}]}`)
		writeOAISSE(w, `{"id":"c","object":"chat.completion.chunk","choices":[{"delta":{},"index":0,"finish_reason":"stop"}],"usage":{"prompt_tokens":100,"completion_tokens":20,"total_tokens":120,"prompt_tokens_details":{"cached_tokens":80}}}`)
		writeOAISSE(w, "[DONE]")
	})
	defer cleanup()

	events := collectStream741(t, d)
	var done *StreamEvent
	for i := range events {
		if events[i].Type == "done" {
			done = &events[i]
		}
	}
	if done == nil {
		t.Fatal("no done event")
	}
	if done.CachedInputTokens != 80 {
		t.Errorf("done.CachedInputTokens = %d, want 80", done.CachedInputTokens)
	}
	if done.CacheCreationInputTokens != 0 {
		t.Errorf("done.CacheCreationInputTokens = %d, want 0", done.CacheCreationInputTokens)
	}
}

// -----------------------------------------------------------------------------
// 74-1-ZERO-003 (AC1-5): gemini — extractResponse 喂非零 CachedContentTokenCount
// 后断言 CacheCreationInputTokens 仍 0（纯函数直测，无 SDK 网络往返）。
// -----------------------------------------------------------------------------
func TestATDD_74_1_ZERO_003_GeminiZeroCacheCreation(t *testing.T) {
	resp := &genai.GenerateContentResponse{
		UsageMetadata: &genai.GenerateContentResponseUsageMetadata{
			PromptTokenCount:        100,
			CandidatesTokenCount:    20,
			TotalTokenCount:         120,
			CachedContentTokenCount: 90,
		},
		Candidates: []*genai.Candidate{{
			Content: &genai.Content{Role: "model", Parts: []*genai.Part{{Text: "ok"}}},
		}},
	}
	out := extractResponse(resp)
	if out.CachedInputTokens != 90 {
		t.Errorf("CachedInputTokens = %d, want 90", out.CachedInputTokens)
	}
	if out.CacheCreationInputTokens != 0 {
		t.Errorf("CacheCreationInputTokens = %d, want 0 (gemini 无 creation 数据源)", out.CacheCreationInputTokens)
	}
}

// -----------------------------------------------------------------------------
// 74-1-ZERO-004 (AC1-5): codex-cli — 既有 codex_stream_success mock 带
// cached_input_tokens=400，断言 done 事件 creation 仍 0。
// -----------------------------------------------------------------------------
func TestATDD_74_1_ZERO_004_CodexZeroCacheCreation(t *testing.T) {
	d := NewCodexCliDriver(CodexWithCommandBuilder(codexMockCmdBuilder("codex_stream_success")))
	events := collectStream741(t, d)
	var done *StreamEvent
	for i := range events {
		if events[i].Type == "done" {
			done = &events[i]
		}
	}
	if done == nil {
		t.Fatal("no done event")
	}
	if done.CachedInputTokens != 400 {
		t.Errorf("done.CachedInputTokens = %d, want 400", done.CachedInputTokens)
	}
	if done.CacheCreationInputTokens != 0 {
		t.Errorf("done.CacheCreationInputTokens = %d, want 0 (codex-cli 无 creation 数据源)", done.CacheCreationInputTokens)
	}
}

// -----------------------------------------------------------------------------
// 74-1-ZERO-005 (AC1-5): cursor-cli — Call 路径喂非零 usage（input 90/output 30）
// 断言 creation 仍 0。
// -----------------------------------------------------------------------------
func TestATDD_74_1_ZERO_005_CursorZeroCacheCreation(t *testing.T) {
	d := NewCursorCliDriver(CursorWithCommandBuilder(cursorMockCmdBuilder("cursor_success")))
	resp, err := d.Call(context.Background(), LLMRequest{Intent: "test"})
	if err != nil {
		t.Fatalf("Call() error: %v", err)
	}
	if resp.InputTokens != 90 || resp.OutputTokens != 30 {
		t.Fatalf("unexpected usage input=%d output=%d", resp.InputTokens, resp.OutputTokens)
	}
	if resp.CacheCreationInputTokens != 0 {
		t.Errorf("CacheCreationInputTokens = %d, want 0 (cursor-cli 无 creation 数据源)", resp.CacheCreationInputTokens)
	}
}

// -----------------------------------------------------------------------------
// 74-1-ZERO-006 (AC1-5): qwen-cli — Stream 路径喂非零 usage（input 70/output 30）
// 断言 done 事件 creation 仍 0。
// -----------------------------------------------------------------------------
func TestATDD_74_1_ZERO_006_QwenZeroCacheCreation(t *testing.T) {
	d := NewQwenCliDriver(QwenWithCommandBuilder(qwenMockCmdBuilder("qwen_stream_success")))
	events := collectStream741(t, d)
	var done *StreamEvent
	for i := range events {
		if events[i].Type == "done" {
			done = &events[i]
		}
	}
	if done == nil {
		t.Fatal("no done event")
	}
	if done.InputTokens != 70 || done.OutputTokens != 30 {
		t.Fatalf("unexpected usage input=%d output=%d", done.InputTokens, done.OutputTokens)
	}
	if done.CacheCreationInputTokens != 0 {
		t.Errorf("done.CacheCreationInputTokens = %d, want 0 (qwen-cli 无 creation 数据源)", done.CacheCreationInputTokens)
	}
}

// -----------------------------------------------------------------------------
// 74-1-REPLAY-001/002 (review decision): replay driver Call + Stream 路径 —
// 脚本 usage 带 cache_creation_input_tokens → LLMResponse / done 事件透传。
// replay 是 Tier1/agtest 法定驱动（ValidateTier1 规则 3），FR1 的确定性回归
// 覆盖唯一通道；零改动清单外经 review 拍板扩展。
// -----------------------------------------------------------------------------
func writeReplayScript741(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "case.responses.yaml")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatalf("write replay script: %v", err)
	}
	return p
}

func TestATDD_74_1_REPLAY_001_CallCarriesCacheCreation(t *testing.T) {
	d := NewReplayDriver("replay-test")
	script := writeReplayScript741(t, `version: "1"
responses:
  - content: "done"
    usage:
      input_tokens: 12
      output_tokens: 8
      cached_input_tokens: 5
      cache_creation_input_tokens: 7
`)
	resp, err := d.Call(t.Context(), LLMRequest{Model: script, CallerUUID: "proc-replay-001"})
	if err != nil {
		t.Fatalf("Call() error: %v", err)
	}
	if resp.InputTokens != 12 || resp.OutputTokens != 8 || resp.CachedInputTokens != 5 {
		t.Fatalf("unexpected usage input=%d output=%d cached=%d", resp.InputTokens, resp.OutputTokens, resp.CachedInputTokens)
	}
	if resp.CacheCreationInputTokens != 7 {
		t.Errorf("CacheCreationInputTokens = %d, want 7 (replay script usage 透传)", resp.CacheCreationInputTokens)
	}
	if resp.TokensUsed != 20 {
		t.Errorf("TokensUsed = %d, want 20 (Input+Output 语义不变，creation 不并入)", resp.TokensUsed)
	}
}

func TestATDD_74_1_REPLAY_002_StreamDoneCarriesCacheCreation(t *testing.T) {
	d := NewReplayDriver("replay-test")
	script := writeReplayScript741(t, `version: "1"
responses:
  - content: "done"
    usage:
      input_tokens: 12
      output_tokens: 8
      cached_input_tokens: 5
      cache_creation_input_tokens: 7
`)
	ch, err := d.Stream(t.Context(), LLMRequest{Model: script, CallerUUID: "proc-replay-002"})
	if err != nil {
		t.Fatalf("Stream() error: %v", err)
	}
	var done *StreamEvent
	for evt := range ch {
		if evt.Type == "done" {
			done = &evt
		}
	}
	if done == nil {
		t.Fatal("no done event")
	}
	if done.CachedInputTokens != 5 {
		t.Errorf("done.CachedInputTokens = %d, want 5", done.CachedInputTokens)
	}
	if done.CacheCreationInputTokens != 7 {
		t.Errorf("done.CacheCreationInputTokens = %d, want 7 (replay stream done 透传)", done.CacheCreationInputTokens)
	}
	if done.TokensUsed != 20 {
		t.Errorf("done.TokensUsed = %d, want 20 (creation 不并入)", done.TokensUsed)
	}
}
