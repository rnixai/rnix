package llm

// ATDD coverage for Story 56.2 — anthropic (官方 SDK) 经
// option.WithMiddleware 捕获原始 HTTP 请求与底层原始响应。
//
// 范式参考：anthropic_test.go + WithAnthropicBaseURL（指向 httptest.Server）
// + atdd_55_1_reasoning_effort_test.go（output_config.effort 与 thinking
// budget 降级双路径）。注意 anthropic-sdk-go 的 option 包与 openai-go 同名
// 但是不同类型，captureMiddleware 工厂需各 driver 独立或经泛型适配。

import (
	"testing"
)

// ============================================================================
// 56-2-INT-011 — anthropic Call: CAP-1 (effort 优先) + CAP-2 (原始 JSON)
//                + 主脱敏 (AC #1, #3, #4, #5, #6, #7)
// ============================================================================

func TestATDD_56_2_INT011_AnthropicCall_EffortPath_RED(t *testing.T) {
	t.Skip("56.2 dev 阶段移除：anthropic NewAnthropicDriver 追加 captureMiddleware（anthropic-sdk-go option 包独立）后断言 " +
		"(a) Kind == \"api\"；" +
		"(b) Request.body 含 output_config.effort=\"high\" 且 max_tokens 真实值（55.1 透传路径）；" +
		"(c) Request.headers[\"x-api-key\"] = redacted(...)，Authorization/api-key/x-api-key 三键全部主脱敏；" +
		"(d) Response.status==200 且 Response.body 等于 httptest 返回的完整 Anthropic-style JSON 原文；" +
		"(e) effort 模式下不应同时 send thinking budget（55.1 行为）— 通过 raw body 检查再次 verify。")
}

// ============================================================================
// 56-2-INT-012 — anthropic Call: budget 降级路径捕获 (AC #3 budget 降级)
//
// 当 effort 未配置 / DeepSeek V4 Anthropic-compat 端点要求 budget 时，rnix
// 走 thinking budget 路径——本测试要求 raw capture 反映该真实 body。
// ============================================================================

func TestATDD_56_2_INT012_AnthropicCall_BudgetFallback_RED(t *testing.T) {
	t.Skip("56.2 dev 阶段移除：仅 WithAnthropicThinkingBudget(2048) 不带 effort 配置时，断言 " +
		"(a) Request.body 含 thinking.type=\"enabled\" 与 thinking.budget_tokens=2048（55.1 budget 降级保留）；" +
		"(b) Request.body 不含 output_config.effort（effort 未设置时不应出现）；" +
		"(c) Response.body 仍是原样 JSON（CAP-2 不被 effort 路径影响）。")
}

// ============================================================================
// 56-2-INT-013 — anthropic Stream: CAP-2 原样 SSE (AC #4, #6, #11)
// ============================================================================

func TestATDD_56_2_INT013_AnthropicStream_RawSSE_RED(t *testing.T) {
	t.Skip("56.2 dev 阶段移除：anthropic Stream 路径 captureMiddleware 经 TeeReader 包 resp.Body 后断言 " +
		"(a) LLMFile.LastRawCapture().Response.body 是原样 Anthropic SSE 文本（含 message_start / content_block_delta / message_stop 至少 3 段事件）；" +
		"(b) Request.body 含 stream:true；" +
		"(c) headers 全部主脱敏。")
}
