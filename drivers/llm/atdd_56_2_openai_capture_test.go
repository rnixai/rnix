package llm

// ATDD coverage for Story 56.2 — openai (官方 SDK) 经 option.WithMiddleware
// 捕获原始 HTTP 请求与底层原始响应。
//
// 红灯机制：业务断言（CAP-1 真实 effort/temperature/max_tokens 落 body、
// CAP-2 原始 JSON / 原样 SSE 字节流、headers 主脱敏 only fingerprint）一律
// t.Skip 占位 RED。56.2 dev 在 NewOpenAIDriver 追加 captureMiddleware()
// 接线后移除 skip 验 RED → GREEN。
//
// 范式参考：drivers/llm/atdd_55_1_reasoning_effort_test.go（httptest 注入
// + WithOpenAIBaseURL + WithOpenAIHTTPClient）+ openai_official_test.go。

import (
	"testing"
)

// ============================================================================
// 56-2-INT-006 — openai Call: CAP-1 (effort 透传) + CAP-2 (原始 JSON)
//                + 同次关联 + 主脱敏 (AC #1, #3, #4, #5, #6, #7)
// ============================================================================

func TestATDD_56_2_INT006_OpenAICall_RawCapture_RED(t *testing.T) {
	t.Skip("56.2 dev 阶段移除：openai NewOpenAIDriver 追加 captureMiddleware（取 ctx sink + 读 req.Body 后 GetBody 重置 + tee resp.Body）后断言 " +
		"LLMFile.LastRawCapture() 满足 " +
		"(a) Kind == \"api\"；" +
		"(b) Request.body 含 reasoning_effort=\"high\"、max_tokens、temperature 真实值；" +
		"(c) Request.headers[\"Authorization\"] 形如 \"Bearer redacted(len=...,sha256=...)\"，明文 sk-test 零出现；" +
		"(d) Response.status == 200 且 Response.body 等同 httptest 返回的完整 JSON 原文（非 LLMResponse）；" +
		"(e) Request 与 Response 同一条 RawCapture（CAP-2 success 同次关联）。")
}

// ============================================================================
// 56-2-INT-007 — openai Stream: CAP-2 SSE 原样字节 (AC #4, #6, #11)
// ============================================================================

func TestATDD_56_2_INT007_OpenAIStream_RawSSE_RED(t *testing.T) {
	t.Skip("56.2 dev 阶段移除：openai Stream 路径下 captureMiddleware 必须用 io.TeeReader 包 resp.Body（非 io.ReadAll，否则吞流），断言 " +
		"(a) range stream channel 结束（close(ch)）后 LLMFile.LastRawCapture().Response.body 是原样 SSE 文本，含至少两段 \"data: {...}\" 与 \"[DONE]\"；" +
		"(b) body 类型为 string（落盘 truncateRawCapture 才能切到，非 string 会逃过截断 — kernel/raw_writer.go:339 largestStringKey）；" +
		"(c) sink 在 channel drain 完毕（happens-before close）之后才被 LLMFile 归集，避免 stream goroutine 与 LLMFile goroutine 之间的可见性裂缝。")
}

// ============================================================================
// 56-2-INT-008 — openai 默认 HTTP middleware 不破坏现有测试 (AC #11 零回归)
//
// GREEN-guard 兜底：56.2 dev 接 middleware 后，55.1 的 ReasoningEffort 测试
// 与 openai_official_test 既有用例必须仍然通过——它们用 WithOpenAIHTTPClient
// 注入 httptest.Client，middleware 必须兼容（不能 hard-code 自己的
// http.Client）。本测试在源文件层面提前提示：56.2 接线时 captureMiddleware
// 应通过 option.WithMiddleware（修饰已有 client）而非 option.WithHTTPClient
// （会覆盖测试注入）。
// ============================================================================

func TestATDD_56_2_INT008_OpenAIMiddleware_HTTPClientCompat_RED(t *testing.T) {
	t.Skip("56.2 dev 阶段移除：在 NewOpenAIDriver 接线后跑 TestOpenAIDriver_ReasoningEffort_Passthrough / _Empty_NoRegression / _ForwardCompat 必须保持绿；" +
		"做法 = sdkOpts append option.WithMiddleware(captureMiddleware())，禁止用 option.WithHTTPClient 覆盖 WithOpenAIHTTPClient 的测试注入。")
}
