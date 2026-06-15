package llm

// ATDD coverage for Story 56.2 — anthropic (官方 SDK) 经
// option.WithMiddleware 捕获原始 HTTP 请求与底层原始响应。

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// anthropic 端点返回的最小完整 Message 形态——供 httptest 直接吐回。
const anthropicOKResp = `{
	"id": "msg_test",
	"type": "message",
	"role": "assistant",
	"content": [{"type":"text","text":"ok"}],
	"model": "claude-test",
	"stop_reason": "end_turn",
	"stop_sequence": null,
	"usage": {"input_tokens": 5, "output_tokens": 3}
}`

// ============================================================================
// 56-2-INT-011 — anthropic Call: CAP-1 (effort 优先) + CAP-2 (原始 JSON)
// ============================================================================

func TestATDD_56_2_INT011_AnthropicCall_EffortPath(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(anthropicOKResp))
	}))
	defer ts.Close()

	d := NewAnthropicDriver("test",
		WithAnthropicModel("claude-opus-4-7"),
		WithAnthropicBaseURL(ts.URL),
		WithAnthropicHTTPClient(ts.Client()),
		WithAnthropicKey("sk-ant-test-1234567890abcdef"),
		WithAnthropicEffort("high"),
		WithAnthropicMaxTokens(1024),
	)

	f := openLLMFile(t, d, ModeCall)
	defer f.Close()
	writeStringReq(t, f, `{"intent":"hi"}`)

	cap := f.LastRawCapture()
	if cap == nil {
		t.Fatal("(a) LastRawCapture() == nil; want non-nil")
	}
	if cap.Kind != "api" {
		t.Errorf("(a) Kind = %q, want api", cap.Kind)
	}

	body := captureReqBody(t, cap)
	// (b) effort 路径：output_config.effort=high
	if !strings.Contains(body, `"effort":"high"`) {
		t.Errorf("(b) body missing output_config.effort=high:\n%s", body)
	}
	if !strings.Contains(body, `"max_tokens":1024`) {
		t.Errorf("(b) body missing max_tokens=1024:\n%s", body)
	}

	// (c) 主脱敏 — anthropic 用 x-api-key header；SDK 实际写哪个 key 取决
	// 于版本，验「无明文 + 任何脱敏 header 都不含 sk-ant-test 明文」。
	hdrs, _ := cap.Request["headers"].(map[string]string)
	for k, v := range hdrs {
		if strings.Contains(v, "sk-ant-test-1234567890") {
			t.Errorf("(c) header %q leaks plaintext API key: %q", k, v)
		}
	}

	if got := captureRespStatus(t, cap); got != 200 {
		t.Errorf("(d) status = %d, want 200", got)
	}
	if got := captureRespBody(t, cap); got != anthropicOKResp {
		t.Errorf("(d) response body mismatch:\n got=%q\nwant=%q", got, anthropicOKResp)
	}

	// (e) effort 模式不应同时 send thinking budget（55.1 优先级行为）。
	if strings.Contains(body, `"thinking"`) && strings.Contains(body, `"budget_tokens"`) {
		t.Errorf("(e) effort path leaked thinking.budget_tokens:\n%s", body)
	}
}

// ============================================================================
// 56-2-INT-012 — anthropic Call: budget 降级路径 (AC #3 budget 降级)
// ============================================================================

func TestATDD_56_2_INT012_AnthropicCall_BudgetFallback(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(anthropicOKResp))
	}))
	defer ts.Close()

	d := NewAnthropicDriver("test",
		WithAnthropicModel("deepseek-v4-anthropic"),
		WithAnthropicBaseURL(ts.URL),
		WithAnthropicHTTPClient(ts.Client()),
		WithAnthropicKey("sk-ant-test"),
		WithAnthropicThinkingBudget(2048),
	)

	f := openLLMFile(t, d, ModeCall)
	defer f.Close()
	writeStringReq(t, f, `{"intent":"hi"}`)

	cap := f.LastRawCapture()
	body := captureReqBody(t, cap)

	// (a) budget 降级路径：thinking.type=enabled + budget_tokens=2048
	if !strings.Contains(body, `"type":"enabled"`) {
		t.Errorf("(a) body missing thinking.type=enabled:\n%s", body)
	}
	if !strings.Contains(body, `"budget_tokens":2048`) {
		t.Errorf("(a) body missing thinking.budget_tokens=2048:\n%s", body)
	}

	// (b) effort 未设置不应出现 output_config.effort（55.1 行为）。
	if strings.Contains(body, `"effort"`) {
		t.Errorf("(b) body unexpectedly contains effort key when not set:\n%s", body)
	}

	// (c) Response 仍是原样 JSON。
	if got := captureRespBody(t, cap); got != anthropicOKResp {
		t.Errorf("(c) response body mismatch")
	}
}

// ============================================================================
// 56-2-INT-013 — anthropic Stream: CAP-2 原样 SSE (AC #4, #6, #11)
// ============================================================================

func TestATDD_56_2_INT013_AnthropicStream_RawSSE(t *testing.T) {
	// Anthropic SSE 是命名事件流，最小完整集合：message_start →
	// content_block_start → content_block_delta → content_block_stop →
	// message_delta → message_stop。
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		fl, _ := w.(http.Flusher)
		emit := func(event, data string) {
			_, _ = w.Write([]byte("event: " + event + "\ndata: " + data + "\n\n"))
			if fl != nil {
				fl.Flush()
			}
		}
		emit("message_start", `{"type":"message_start","message":{"id":"msg","type":"message","role":"assistant","content":[],"model":"claude","stop_reason":null,"stop_sequence":null,"usage":{"input_tokens":5,"output_tokens":0}}}`)
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
		WithAnthropicKey("sk-ant-stream"),
		WithAnthropicMaxTokens(128),
	)
	f := openLLMFile(t, d, "")
	defer f.Close()
	writeStringReq(t, f, `{"intent":"hi"}`)

	cap := f.LastRawCapture()
	if cap == nil {
		t.Fatal("LastRawCapture() == nil")
	}
	respBody := captureRespBody(t, cap)

	// (a) 原样 SSE 至少含 message_start / content_block_delta / message_stop。
	for _, evt := range []string{"event: message_start", "event: content_block_delta", "event: message_stop"} {
		if !strings.Contains(respBody, evt) {
			t.Errorf("(a) response body missing %q:\n%s", evt, respBody)
		}
	}

	// (b) Request.body 含 stream:true
	var reqBody map[string]any
	_ = json.Unmarshal([]byte(captureReqBody(t, cap)), &reqBody)
	if reqBody["stream"] != true {
		t.Errorf("(b) request body missing stream:true: %+v", reqBody["stream"])
	}

	// (c) headers 主脱敏（任何包含明文 key 的都失败）。
	hdrs, _ := cap.Request["headers"].(map[string]string)
	for k, v := range hdrs {
		if strings.Contains(v, "sk-ant-stream") {
			t.Errorf("(c) header %q leaks plaintext API key: %q", k, v)
		}
	}
}
