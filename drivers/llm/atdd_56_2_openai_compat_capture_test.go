package llm

// ATDD coverage for Story 56.2 — openai-compat (手写 HTTP) 在 doHTTP /
// streamInternal 内联捕获原始 HTTP 请求与响应。
//
// 范式参考：openai_compat_test.go（httptest + jsonBody 解析 + scanner SSE）
// + atdd_55_1_reasoning_effort_test.go（reasoning_effort + thinking_budget
// 双路径透传断言）。

import (
	"testing"
)

// ============================================================================
// 56-2-INT-009 — openai-compat Call: CAP-1 (effort + budget 共存) +
//                CAP-2 (原始 JSON) + 主脱敏 (AC #1, #3, #4, #5, #7)
//
// openai-compat 的 thinking_budget 与 reasoning_effort 是正交可共存的
// （DeepSeek V4 多轮工具调用要求），本测试要求 body 同时含两者真实值。
// ============================================================================

func TestATDD_56_2_INT009_OpenAICompatCall_RawCapture_RED(t *testing.T) {
	t.Skip("56.2 dev 阶段移除：openai-compat doHTTP（drivers/llm/openai_compat.go:423）内联接线后断言 " +
		"(a) Kind == \"api\"、url == ts.URL+\"/chat/completions\"；" +
		"(b) Request.body 含 reasoning_effort=\"medium\" 与 thinking.budget_tokens=2048（两者共存，正交透传）；" +
		"(c) Request.headers[\"Authorization\"] = redacted(...)（主脱敏走 vfs.RedactHeaders，driver 在写 sink 前调用）；" +
		"(d) Response.status==200 且 Response.body 等于 httptest 返回的完整 JSON 字符串（非归一化 LLMResponse）；" +
		"(e) Request 与 Response 同一条 RawCapture。")
}

// ============================================================================
// 56-2-INT-010 — openai-compat Stream: CAP-2 原样 SSE + 关闭时序
//                (AC #4, #6, #11)
//
// streamInternal:605 内的 scanner 循环必须用 tee/累积 buffer 收原样 SSE 行，
// sink.Response.body 在 ch close 之前完成填充，否则 LLMFile 的 range ch
// 结束断言时会读到部分内容（happens-before 必须在 close(ch) 时建立）。
// ============================================================================

func TestATDD_56_2_INT010_OpenAICompatStream_RawSSE_RED(t *testing.T) {
	t.Skip("56.2 dev 阶段移除：openai-compat streamInternal scanner 累积接线后断言 " +
		"(a) LLMFile.LastRawCapture().Response.body 是原样 SSE 字符串，至少含 2 段 \"data: {...}\" 与 \"data: [DONE]\\n\\n\"；" +
		"(b) sink 填充发生在 close(ch) 之前 — LLMFile range ch 结束后读到的就是完整 body，不能出现 \"[DONE]\" 缺失；" +
		"(c) Request.body 含 stream:true。")
}
