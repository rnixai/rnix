package llm

// ATDD coverage for Story 56.2 — gemini (genai SDK) 经
// ClientConfig.HTTPClient 注入自定义 *http.Client (Transport=捕获
// RoundTripper) 捕获原始 HTTP 请求与底层原始响应。
//
// AC #10 要求新增 WithGeminiHTTPClient option 同时支持「测试注入 httptest」
// 与「生产 RoundTripper 捕获」。注意 ThinkingLevel 是大写
// (MINIMAL/LOW/MEDIUM/HIGH)，rnix 透传不转换大小写。

import (
	"net/http"
	"testing"
)

// ============================================================================
// 56-2-INT-014 — WithGeminiHTTPClient option 存在 (AC #10) — GREEN-guard
//
// 编译期断言 + 运行期 nil-safe 检查：option 必须接受 *http.Client 且不
// 因传入 nil 报错（gemini.go newClient 需在 nil 时回退 SDK 默认行为）。
// 这条 GREEN 让 dev 不能轻易删掉这个注入口（删了 56.2 测试无法 httptest
// 触达）。
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

func TestATDD_56_2_INT015_GeminiCall_RawCapture_RED(t *testing.T) {
	t.Skip("56.2 dev 阶段移除：gemini newClient 把捕获 RoundTripper 装进 ClientConfig.HTTPClient（per-call newClient 须每次装上，参考 gemini.go:84-89）后断言 " +
		"(a) Kind == \"api\"；" +
		"(b) Request.body 含 thinking_config.thinking_level=\"HIGH\"（大写！rnix 透传不转换 — gemini.go:144-148）；" +
		"(c) Request.headers 主脱敏，明文 API key 零出现；" +
		"(d) Response.status==200 且 Response.body 等于 httptest 返回的完整 GenerateContent JSON 原文（非 LLMResponse）；" +
		"(e) Request 与 Response 同一条 RawCapture。")
}

// ============================================================================
// 56-2-INT-016 — gemini Call: thinking_budget 降级路径 (AC #3)
//
// 仅 WithGeminiThinkingBudget 不带 level 时走 budget 透传，verify capture
// 同步反映该 body。
// ============================================================================

func TestATDD_56_2_INT016_GeminiCall_BudgetFallback_RED(t *testing.T) {
	t.Skip("56.2 dev 阶段移除：仅 WithGeminiThinkingBudget(8192) 不带 level 时，断言 " +
		"(a) Request.body 含 thinking_config.thinking_budget=8192（gemini.go:149-154 互斥路径）；" +
		"(b) Request.body 不含 thinking_config.thinking_level；" +
		"(c) Response.body 仍是原样 JSON。")
}

// ============================================================================
// 56-2-INT-017 — gemini Stream: CAP-2 原样 SSE + per-call newClient
//                场景下捕获 RoundTripper 有效 (AC #4, #6, #10, #11)
// ============================================================================

func TestATDD_56_2_INT017_GeminiStream_RawSSE_RED(t *testing.T) {
	t.Skip("56.2 dev 阶段移除：gemini Stream 路径下 RoundTripper TeeReader 包 resp.Body，断言 " +
		"(a) LLMFile.LastRawCapture().Response.body 是原样 GenerateContentStream SSE/chunked-JSON 文本（含至少 2 段 candidates 数组与 finish reason）；" +
		"(b) Request.body 含 stream 开关（gemini SDK 通过 streamInternal 触发不同 endpoint，body 形态由 SDK 决定 — 测试不假设字段名而是断言「响应非空 + 多 chunk」）；" +
		"(c) headers 全部主脱敏；" +
		"(d) per-call newClient 场景下捕获 RoundTripper 仍生效（gemini.go:84 是 per-Call/Stream 重建 client，捕获装载在 cfg.httpClient 注入路径上）。")
}
