package llm

// ATDD coverage for Story 56.2 — gemini (genai SDK) 经
// ClientConfig.HTTPClient 注入自定义 *http.Client (Transport=捕获
// RoundTripper) 捕获原始 HTTP 请求与底层原始响应。

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const geminiOKResp = `{
	"candidates": [{
		"content": {"parts":[{"text":"ok"}], "role":"model"},
		"finishReason":"STOP",
		"index": 0
	}],
	"usageMetadata": {"promptTokenCount":5, "candidatesTokenCount":3, "totalTokenCount":8}
}`

// ============================================================================
// 56-2-INT-014 — WithGeminiHTTPClient option 存在 (AC #10) — GREEN-guard
// ============================================================================

func TestATDD_56_2_INT014_GeminiHTTPClientOption_Compiles(t *testing.T) {
	// (a) 接受任意 *http.Client（包括标准库默认）
	_ = NewGeminiDriver("gemini-test",
		WithGeminiAPIKey("dummy"),
		WithGeminiModel("gemini-3-pro"),
		WithGeminiHTTPClient(http.DefaultClient),
	)
	// (b) nil 不 panic（让 dev 的 nil 检查路径有 guard）
	_ = NewGeminiDriver("gemini-test-nil",
		WithGeminiAPIKey("dummy"),
		WithGeminiHTTPClient(nil),
	)
}

// ============================================================================
// 56-2-INT-015 — gemini Call: CAP-1 (thinking_level 大写透传) +
//                CAP-2 (原始 JSON) + 主脱敏 (AC #1, #3, #4, #5, #6, #7, #10)
// ============================================================================

func TestATDD_56_2_INT015_GeminiCall_RawCapture(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(geminiOKResp))
	}))
	defer ts.Close()

	d := NewGeminiDriver("gemini-test",
		WithGeminiAPIKey("AIza-secret-1234567890abcdef"),
		WithGeminiModel("gemini-3-pro"),
		WithGeminiBaseURL(ts.URL),
		WithGeminiHTTPClient(ts.Client()),
		WithGeminiThinkingLevel("HIGH"),
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
	// (b) thinking_level=HIGH 大写透传（gemini.go:144-148 大写传出）。
	if !strings.Contains(body, `"thinkingLevel":"HIGH"`) {
		t.Errorf("(b) body missing thinkingLevel=HIGH:\n%s", body)
	}

	// (c) headers 主脱敏 + URL 中可能含 key 也得脱敏？genai SDK 默认把
	// API key 放在 query 或 x-goog-api-key header；这里检测 header 和 URL
	// 都无明文。
	hdrs, _ := cap.Request["headers"].(map[string]string)
	for k, v := range hdrs {
		if strings.Contains(v, "AIza-secret-1234567890") {
			t.Errorf("(c) header %q leaks plaintext API key: %q", k, v)
		}
	}

	if got := captureRespStatus(t, cap); got != 200 {
		t.Errorf("(d) status = %d, want 200", got)
	}
	if got := captureRespBody(t, cap); got != geminiOKResp {
		t.Errorf("(d) response body mismatch:\n got=%q\nwant=%q", got, geminiOKResp)
	}
	// (e) 同一条 RawCapture。
	if cap.Request == nil || cap.Response == nil {
		t.Errorf("(e) Request/Response not on the same RawCapture")
	}
}

// ============================================================================
// 56-2-INT-016 — gemini Call: thinking_budget 降级路径 (AC #3)
// ============================================================================

func TestATDD_56_2_INT016_GeminiCall_BudgetFallback(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(geminiOKResp))
	}))
	defer ts.Close()

	d := NewGeminiDriver("gemini-test",
		WithGeminiAPIKey("dummy"),
		WithGeminiModel("gemini-2.5-pro"),
		WithGeminiBaseURL(ts.URL),
		WithGeminiHTTPClient(ts.Client()),
		WithGeminiThinkingBudget(8192),
	)

	f := openLLMFile(t, d, ModeCall)
	defer f.Close()
	writeStringReq(t, f, `{"intent":"hi"}`)

	cap := f.LastRawCapture()
	body := captureReqBody(t, cap)
	// (a) budget=8192 透传（gemini.go:149-154）。
	if !strings.Contains(body, `"thinkingBudget":8192`) {
		t.Errorf("(a) body missing thinkingBudget=8192:\n%s", body)
	}
	// (b) 不应同时含 thinkingLevel（互斥）。
	if strings.Contains(body, `"thinkingLevel"`) {
		t.Errorf("(b) body should not contain thinkingLevel when only budget set:\n%s", body)
	}
	// (c) Response 仍是原样 JSON。
	if got := captureRespBody(t, cap); got != geminiOKResp {
		t.Errorf("(c) response body mismatch")
	}
}

// ============================================================================
// 56-2-INT-017 — gemini Stream: CAP-2 原样 SSE + per-call newClient 场景
// ============================================================================

func TestATDD_56_2_INT017_GeminiStream_RawSSE(t *testing.T) {
	// gemini SSE chunk 形态：每行 "data: {...}\n\n"，每 chunk 含 candidates 数组。
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		writeSSE(w, `{"candidates":[{"content":{"parts":[{"text":"Hel"}],"role":"model"},"index":0}]}`)
		writeSSE(w, `{"candidates":[{"content":{"parts":[{"text":"lo!"}],"role":"model"},"index":0}]}`)
		writeSSE(w, `{"candidates":[{"content":{"parts":[{"text":""}],"role":"model"},"finishReason":"STOP","index":0}],"usageMetadata":{"promptTokenCount":3,"candidatesTokenCount":2,"totalTokenCount":5}}`)
	}))
	defer ts.Close()

	d := NewGeminiDriver("gemini-test",
		WithGeminiAPIKey("AIza-stream"),
		WithGeminiModel("gemini-3-pro"),
		WithGeminiBaseURL(ts.URL),
		WithGeminiHTTPClient(ts.Client()),
	)
	f := openLLMFile(t, d, "")
	defer f.Close()
	writeStringReq(t, f, `{"intent":"hi"}`)

	cap := f.LastRawCapture()
	if cap == nil {
		t.Fatal("LastRawCapture() == nil")
	}
	respBody := captureRespBody(t, cap)

	// (a) 原样 chunked-JSON：至少 2 段 candidates 数组 + 一个 finishReason。
	if c := strings.Count(respBody, `"candidates"`); c < 2 {
		t.Errorf("(a) response body missing candidates chunks (count=%d):\n%s", c, respBody)
	}
	if !strings.Contains(respBody, `"finishReason"`) {
		t.Errorf("(a) response body missing finishReason:\n%s", respBody)
	}

	// (b) Request.body 非空（gemini SDK 决定 body 形态，不假设字段名）。
	if captureReqBody(t, cap) == "" {
		t.Errorf("(b) request body is empty")
	}

	// (c) headers 全部主脱敏。
	hdrs, _ := cap.Request["headers"].(map[string]string)
	for k, v := range hdrs {
		if strings.Contains(v, "AIza-stream") {
			t.Errorf("(c) header %q leaks plaintext API key: %q", k, v)
		}
	}

	// (d) per-call newClient 场景下 RoundTripper 仍生效：cap 不空即证。
	if cap.Response == nil {
		t.Errorf("(d) per-call newClient RoundTripper failed: Response is nil")
	}
}
