package llm

// ATDD coverage for Story 56.2 — openai (官方 SDK) 经 option.WithMiddleware
// 捕获原始 HTTP 请求与底层原始响应。NewOpenAIDriver 自动追加
// captureMiddlewareFunc，无需测试侧额外配置。

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ============================================================================
// 56-2-INT-006 — openai Call: CAP-1 (effort 透传) + CAP-2 (原始 JSON)
//                + 同次关联 + 主脱敏 (AC #1, #3, #4, #5, #6, #7)
// ============================================================================

func TestATDD_56_2_INT006_OpenAICall_RawCapture(t *testing.T) {
	const respBody = `{"id":"c","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"hello"},"finish_reason":"stop"}],"usage":{"total_tokens":5}}`
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(respBody))
	}))
	defer ts.Close()

	temp := 0.7
	d := NewOpenAIDriver("test-openai",
		WithOpenAIModel("gpt-5.1"),
		WithOpenAIBaseURL(ts.URL),
		WithOpenAIKey("sk-test-key-1234567890abcdef"),
		WithOpenAIHTTPClient(ts.Client()),
		WithOpenAIReasoningEffort("high"),
	)

	f := openLLMFile(t, d, ModeCall)
	defer f.Close()
	writeStringReq(t, f, `{"intent":"hi","temperature":0.7,"max_tokens":256}`)
	_ = temp

	cap := f.LastRawCapture()
	if cap == nil {
		t.Fatal("(a) LastRawCapture() == nil; want non-nil")
	}
	if cap.Kind != "api" {
		t.Errorf("(a) Kind = %q, want api", cap.Kind)
	}

	body := captureReqBody(t, cap)
	if !strings.Contains(body, `"reasoning_effort":"high"`) {
		t.Errorf("(b) body missing reasoning_effort=high:\n%s", body)
	}
	if !strings.Contains(body, `"max_tokens":256`) {
		t.Errorf("(b) body missing max_tokens=256:\n%s", body)
	}
	if !strings.Contains(body, `"temperature":0.7`) {
		t.Errorf("(b) body missing temperature=0.7:\n%s", body)
	}

	auth := captureHeader(t, cap, "Authorization")
	if strings.Contains(auth, "sk-test-key") {
		t.Errorf("(c) Authorization plaintext leaked: %q", auth)
	}
	if !strings.HasPrefix(auth, "Bearer redacted(") {
		t.Errorf("(c) Authorization not redacted: %q", auth)
	}

	if got := captureRespStatus(t, cap); got != 200 {
		t.Errorf("(d) status = %d, want 200", got)
	}
	if got := captureRespBody(t, cap); got != respBody {
		t.Errorf("(d) response body mismatch:\n got=%q\nwant=%q", got, respBody)
	}
	if cap.Request == nil || cap.Response == nil {
		t.Errorf("(e) Request/Response not on the same RawCapture: req=%v resp=%v", cap.Request, cap.Response)
	}
}

// ============================================================================
// 56-2-INT-007 — openai Stream: CAP-2 SSE 原样字节 (AC #4, #6, #11)
// ============================================================================

func TestATDD_56_2_INT007_OpenAIStream_RawSSE(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		writeSSE(w, `{"id":"c","object":"chat.completion.chunk","choices":[{"delta":{"content":"Hel"},"index":0}]}`)
		writeSSE(w, `{"id":"c","object":"chat.completion.chunk","choices":[{"delta":{"content":"lo!"},"index":0}]}`)
		writeSSE(w, `{"id":"c","object":"chat.completion.chunk","choices":[{"delta":{},"index":0,"finish_reason":"stop"}],"usage":{"total_tokens":11}}`)
		writeSSE(w, "[DONE]")
	}))
	defer ts.Close()

	d := NewOpenAIDriver("test-openai",
		WithOpenAIModel("gpt-5.1"),
		WithOpenAIBaseURL(ts.URL),
		WithOpenAIKey("sk-stream-test"),
		WithOpenAIHTTPClient(ts.Client()),
	)
	f := openLLMFile(t, d, "") // stream
	defer f.Close()
	writeStringReq(t, f, `{"intent":"hi"}`)

	cap := f.LastRawCapture()
	if cap == nil {
		t.Fatal("LastRawCapture() == nil; want non-nil")
	}

	respBody := captureRespBody(t, cap)
	if c := strings.Count(respBody, "data: "); c < 3 {
		t.Errorf("(a) response body missing SSE chunks (count=%d, want >=3):\n%s", c, respBody)
	}
	if !strings.Contains(respBody, "[DONE]") {
		t.Errorf("(a) response body missing [DONE]:\n%s", respBody)
	}

	// (b) body 类型为 string（裁决 3 字段约定 → 非 string 不能被 kernel
	// truncateRawCapture 切到，largestStringKey 路径要 string 才生效）。
	if _, ok := cap.Response["body"].(string); !ok {
		t.Errorf("(b) Response[body] is not string: %T", cap.Response["body"])
	}

	// (c) range ch 结束后 sink 已归集，body 末尾必含 [DONE]。
	if !strings.HasSuffix(strings.TrimRight(respBody, "\n"), "[DONE]") {
		t.Errorf("(c) body truncated before [DONE]:\n%q", respBody)
	}
}

// ============================================================================
// 56-2-INT-008 — middleware 不破坏 WithOpenAIHTTPClient 测试注入 (AC #11)
// ============================================================================

func TestATDD_56_2_INT008_OpenAIMiddleware_HTTPClientCompat(t *testing.T) {
	// 覆盖 55.1 的 ReasoningEffort 测试形态——middleware 与 HTTPClient 注入
	// 共存（middleware tee resp.Body，httptest client 提供网络层）。
	var gotBody string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		buf := make([]byte, 1024)
		n, _ := r.Body.Read(buf)
		gotBody = string(buf[:n])
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"c","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{}}`))
	}))
	defer ts.Close()

	d := NewOpenAIDriver("test",
		WithOpenAIModel("gpt-5.1"),
		WithOpenAIBaseURL(ts.URL),
		WithOpenAIKey("sk-test"),
		WithOpenAIHTTPClient(ts.Client()),
		WithOpenAIReasoningEffort("high"),
	)

	f := openLLMFile(t, d, ModeCall)
	defer f.Close()
	writeStringReq(t, f, `{"intent":"hi"}`)

	if !strings.Contains(gotBody, `"reasoning_effort":"high"`) {
		t.Errorf("server received body missing effort: %s", gotBody)
	}
	cap := f.LastRawCapture()
	if cap == nil {
		t.Fatal("LastRawCapture() == nil — middleware should populate cap with effort path")
	}
	if !strings.Contains(captureReqBody(t, cap), `"reasoning_effort":"high"`) {
		t.Errorf("captured req body missing effort: %s", captureReqBody(t, cap))
	}
}
