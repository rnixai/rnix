package llm

// ATDD coverage for Story 56.2 — openai-compat (手写 HTTP) 通过包装
// d.httpClient 的 Transport 为 captureRoundTripper 实现统一捕获，所有
// /v1/chat/completions 调用自动产生 RawCapture（Call/Stream 共用 doHTTP）。
//
// 范式参考：openai_compat_test.go（httptest + writeSSE 累积 SSE）+
// atdd_55_1_reasoning_effort_test.go（reasoning_effort + thinking_budget
// 双路径透传断言）。

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// ============================================================================
// 56-2-INT-009 — openai-compat Call: CAP-1 (effort + budget 共存) +
//                CAP-2 (原始 JSON) + 主脱敏 (AC #1, #3, #4, #5, #7)
// ============================================================================

func TestATDD_56_2_INT009_OpenAICompatCall_RawCapture(t *testing.T) {
	const respBody = `{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"total_tokens":3}}`
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(respBody))
	}))
	defer ts.Close()

	d := NewOpenAICompatDriver("test", ts.URL,
		WithCompatModel("deepseek-v4"),
		WithHTTPClient(ts.Client()),
		WithAPIKey("sk-secret-test-key-1234567890abcdef"),
		WithCompatReasoningEffort("medium"),
		WithCompatThinkingBudget(2048),
	)

	f := openLLMFile(t, d, ModeCall)
	defer f.Close()
	writeStringReq(t, f, `{"intent":"hello"}`)

	cap := f.LastRawCapture()
	if cap == nil {
		t.Fatal("(a) LastRawCapture() == nil; want non-nil")
	}
	if cap.Kind != "api" {
		t.Errorf("(a) Kind = %q, want api", cap.Kind)
	}
	url, _ := cap.Request["url"].(string)
	if !strings.HasSuffix(url, "/chat/completions") {
		t.Errorf("(a) Request.url = %q, want suffix /chat/completions", url)
	}

	body := captureReqBody(t, cap)
	if !strings.Contains(body, `"reasoning_effort":"medium"`) {
		t.Errorf("(b) body missing reasoning_effort=medium: %s", body)
	}
	if !strings.Contains(body, `"budget_tokens":2048`) {
		t.Errorf("(b) body missing thinking.budget_tokens=2048: %s", body)
	}

	auth := captureHeader(t, cap, "Authorization")
	if strings.Contains(auth, "sk-secret-test-key") {
		t.Errorf("(c) Authorization plaintext leaked: %q", auth)
	}
	if !strings.HasPrefix(auth, "Bearer redacted(") {
		t.Errorf("(c) Authorization not redacted: %q (want \"Bearer redacted(...)\")", auth)
	}

	if got := captureRespStatus(t, cap); got != 200 {
		t.Errorf("(d) status = %d, want 200", got)
	}
	if got := captureRespBody(t, cap); got != respBody {
		t.Errorf("(d) response body mismatch:\n got=%q\nwant=%q", got, respBody)
	}

	// (e) 同一条 RawCapture：request 与 response 必须都在同一个对象里。
	if cap.Request == nil || cap.Response == nil {
		t.Errorf("(e) Request/Response not on the same RawCapture: req=%v resp=%v", cap.Request, cap.Response)
	}
}

// ============================================================================
// 56-2-INT-010 — openai-compat Stream: CAP-2 原样 SSE + 关闭时序
//                (AC #4, #6, #11)
// ============================================================================

func TestATDD_56_2_INT010_OpenAICompatStream_RawSSE(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		writeSSE(w, `{"choices":[{"delta":{"content":"Hel"},"index":0}]}`)
		writeSSE(w, `{"choices":[{"delta":{"content":"lo!"},"index":0}]}`)
		writeSSE(w, `{"choices":[{"delta":{},"index":0,"finish_reason":"stop"}],"usage":{"total_tokens":3}}`)
		writeSSE(w, "[DONE]")
	}))
	defer ts.Close()

	d := NewOpenAICompatDriver("test", ts.URL,
		WithCompatModel("deepseek-v4"),
		WithHTTPClient(ts.Client()),
		WithAPIKey("sk-stream-test"),
	)
	f := openLLMFile(t, d, "") // ModeStream default
	defer f.Close()
	writeStringReq(t, f, `{"intent":"hello"}`)

	cap := f.LastRawCapture()
	if cap == nil {
		t.Fatal("LastRawCapture() == nil; want non-nil")
	}

	respBody := captureRespBody(t, cap)
	// (a) 原样 SSE：至少含 2 段 "data: " 与 "[DONE]"。
	if c := strings.Count(respBody, "data: "); c < 3 {
		t.Errorf("(a) response body missing SSE chunks (count=%d, want >=3):\n%s", c, respBody)
	}
	if !strings.Contains(respBody, "[DONE]") {
		t.Errorf("(a) response body missing [DONE]:\n%s", respBody)
	}

	// (b) sink 填充早于 range ch 结束 → LastRawCapture 立即看到完整 body。
	if !strings.HasSuffix(strings.TrimRight(respBody, "\n"), "[DONE]") {
		t.Errorf("(b) response body did not end with [DONE] (truncated?):\n%q", respBody)
	}

	// (c) Request.body 含 stream:true。
	var reqBody map[string]any
	if err := json.Unmarshal([]byte(captureReqBody(t, cap)), &reqBody); err != nil {
		t.Fatalf("(c) failed to parse request body: %v", err)
	}
	if reqBody["stream"] != true {
		t.Errorf("(c) request body missing stream:true: %+v", reqBody["stream"])
	}
	_ = context.Background()
}
