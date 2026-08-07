package llm

// ATDD coverage for Story 75.4 — 其余端点验证 (Verify Remaining Endpoints).
// Mock 端点固件 (httptest) 覆盖四家 OpenAI 兼容端点的已确认 wire 差异，
// 无需真实 API key 可跑 (AC6)。真实端点实测结果见
// _bmad-output/implementation-artifacts/investigations/openai-driver-endpoint-verification.md
//
// 判据一律使用 driver 层输出 (LLMResponse / StreamEvent)，mock 只负责按
// 端点真实 wire 形状返回原始 JSON — 不在测试侧用 Valid() 判 ExtraFields
// (卷宗 §4: Valid() 对 ExtraFields 恒 false)。

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

// ============================================================================
// Ollama (AC1) — 本地实测: gemma4:31b-cloud / qwen3:1.7b @ http://localhost:11434
//   确认差异: usage omitempty 常缺; reasoning 拼法 (非 reasoning_content);
//   reasoning_effort 白名单 (high|medium|low|max|none, 非法值 400);
//   流式混合响应拆 reasoning/content 双 chunk; 不支持 tool_choice/logit_bias/user/n
// ============================================================================

// TestATDD_75_4_Ollama_UsageMissing_NonStream: usage 整体缺失时
// convertCompletion 必须零值安全 — TokensUsed=0、不 panic、Content 仍提取。
// 🔴 回归形状: 若未来把 Usage 改成 *Usage 指针并在缺失时解引用 → panic。
func TestATDD_75_4_Ollama_UsageMissing_NonStream(t *testing.T) {
	d, _, cleanup := newTestOpenAIDriver(func(w http.ResponseWriter, r *http.Request) {
		// Ollama 真实形状: usage 为 omitempty，经常整体缺失
		writeJSON(w, `{"id":"chatcmpl-1","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"你好"},"finish_reason":"stop"}]}`)
	})
	defer cleanup()

	resp, err := d.Call(context.Background(), LLMRequest{Intent: "hi"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if resp.Content != "你好" {
		t.Errorf("Content = %q, want %q", resp.Content, "你好")
	}
	if resp.TokensUsed != 0 {
		t.Errorf("TokensUsed = %d, want 0 (usage missing)", resp.TokensUsed)
	}
	if resp.InputTokens != 0 || resp.OutputTokens != 0 {
		t.Errorf("token fields = %d/%d, want 0/0", resp.InputTokens, resp.OutputTokens)
	}
}

// TestATDD_75_4_Ollama_UsageMissing_Stream: 流式 usage 缺失时
// done 事件不得带 token 字段 (streamInternal 的 acc.Usage.TotalTokens > 0 守卫)，
// 且不得发 error 事件。
func TestATDD_75_4_Ollama_UsageMissing_Stream(t *testing.T) {
	d, _, cleanup := newTestOpenAIDriver(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// Ollama 流式: 推理 delta (reasoning 拼法) + content，usage 指针缺失
		writeSSE(w, `{"id":"c","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"reasoning":"先列方程。"},"finish_reason":null}]}`)
		writeSSE(w, `{"id":"c","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"content":"鸡23兔12"},"finish_reason":null}]}`)
		writeSSE(w, `{"id":"c","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`)
		writeSSE(w, "[DONE]")
	})
	defer cleanup()

	ch, err := d.Stream(context.Background(), LLMRequest{Intent: "hi"})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	var reasoning, contents []string
	var doneEvt StreamEvent
	for evt := range ch {
		switch evt.Type {
		case "reasoning":
			reasoning = append(reasoning, evt.Content)
		case "content":
			contents = append(contents, evt.Content)
		case "done":
			doneEvt = evt
		case "error":
			t.Fatalf("unexpected error event: %v", evt.Err)
		}
	}
	if got := strings.Join(reasoning, ""); got != "先列方程。" {
		t.Errorf("reasoning = %q, want %q", got, "先列方程。")
	}
	if got := strings.Join(contents, ""); got != "鸡23兔12" {
		t.Errorf("content = %q, want %q", got, "鸡23兔12")
	}
	if doneEvt.TokensUsed != 0 {
		t.Errorf("done TokensUsed = %d, want 0 (usage missing)", doneEvt.TokensUsed)
	}
}

// TestATDD_75_4_Ollama_ReasoningSpellingPriority: 双拼法同时出现时
// 优先级 reasoning → reasoning_content (与 openai_compat.reasoningText() 一致)。
// 真实端点 qwen3:1.7b 实测只回 reasoning；本测试用双字段不同值锁定优先级。
func TestATDD_75_4_Ollama_ReasoningSpellingPriority(t *testing.T) {
	const want = "reasoning 拼法优先"
	d, _, cleanup := newTestOpenAIDriver(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"id":"c","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"答案","reasoning":"`+want+`","reasoning_content":"content 拼法"},"finish_reason":"stop"}],"usage":{}}`)
	})
	defer cleanup()

	resp, err := d.Call(context.Background(), LLMRequest{Intent: "hi"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if resp.Reasoning != want {
		t.Errorf("Reasoning = %q, want %q (reasoning 拼法应优先)", resp.Reasoning, want)
	}
}

// TestATDD_75_4_Ollama_MixedResponse_DualChunk: Ollama ToChunks 把
// 「thinking + content/tool_calls 并存」拆成 reasoning chunk + content chunk
// 两个 SSE 事件。流式事件序列与累积必须正确 (易错点 10: 按双事件断言)。
func TestATDD_75_4_Ollama_MixedResponse_DualChunk(t *testing.T) {
	d, _, cleanup := newTestOpenAIDriver(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		// 混合响应: Ollama 拆成两个独立 chunk — 第一个带 reasoning (无 content)，
		// 第二个带 content (无 reasoning)
		writeSSE(w, `{"id":"c","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","reasoning":"思考过程..."},"finish_reason":null}]}`)
		writeSSE(w, `{"id":"c","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"role":"assistant","content":"最终答案"},"finish_reason":"stop"}]}`)
		writeSSE(w, "[DONE]")
	})
	defer cleanup()

	ch, err := d.Stream(context.Background(), LLMRequest{Intent: "hi"})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}

	var reasoning, contents []string
	for evt := range ch {
		switch evt.Type {
		case "reasoning":
			reasoning = append(reasoning, evt.Content)
		case "content":
			contents = append(contents, evt.Content)
		case "error":
			t.Fatalf("unexpected error event: %v", evt.Err)
		}
	}
	if got := strings.Join(reasoning, ""); got != "思考过程..." {
		t.Errorf("reasoning = %q, want %q", got, "思考过程...")
	}
	if got := strings.Join(contents, ""); got != "最终答案" {
		t.Errorf("content = %q, want %q", got, "最终答案")
	}
}

// TestATDD_75_4_Ollama_EffortInvalid400_GenericError: reasoning_effort 白名单
// 校验 — 非法值 (xhigh) 返回 400 "invalid reasoning value"。
// driver verbatim 透传 (resolveEffort 无校验)，classifyError 必须分类为
// 通用 LLMError (400，非 context_length)，不 panic。
func TestATDD_75_4_Ollama_EffortInvalid400_GenericError(t *testing.T) {
	var gotBody map[string]any
	d, _, cleanup := newTestOpenAIDriver(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &gotBody)
		// Ollama 真实错误形状 (实测 2026-08-07)
		writeJSONStatus(w, 400, `{"error":{"message":"invalid reasoning value: 'xhigh' (must be \"high\", \"medium\", \"low\", \"max\", or \"none\")","type":"invalid_request_error","param":null,"code":null}}`)
	})
	defer cleanup()

	_, err := d.Call(context.Background(), LLMRequest{
		Intent:          "hi",
		ReasoningEffort: "xhigh",
	})
	if err == nil {
		t.Fatal("expected 400 error, got nil")
	}
	// verbatim 透传确认
	if got := gotBody["reasoning_effort"]; got != "xhigh" {
		t.Errorf("reasoning_effort = %v, want xhigh (verbatim passthrough)", got)
	}
	// 通用 LLMError 400，不得误分类为 context_length
	var llmErr *LLMError
	if !errors.As(err, &llmErr) {
		t.Fatalf("error = %v, want *LLMError", err)
	}
	if llmErr.StatusCode != 400 {
		t.Errorf("StatusCode = %d, want 400", llmErr.StatusCode)
	}
	if errors.Is(err, ErrContextLength) {
		t.Errorf("error %v misclassified as ErrContextLength (400 must stay generic)", err)
	}
}

// TestATDD_75_4_Ollama_UnsupportedFields_ZeroSend: Ollama 不支持
// tool_choice / logit_bias / user / n — 请求体必须零发送这些字段。
func TestATDD_75_4_Ollama_UnsupportedFields_ZeroSend(t *testing.T) {
	var gotBody map[string]any
	d, _, cleanup := newTestOpenAIDriver(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &gotBody)
		writeJSON(w, `{"id":"c","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{}}`)
	})
	defer cleanup()

	_, err := d.Call(context.Background(), LLMRequest{Intent: "hi", MaxTokens: 100})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	for _, field := range []string{"tool_choice", "logit_bias", "user", "n"} {
		if _, present := gotBody[field]; present {
			t.Errorf("request body unexpectedly carries unsupported field %q", field)
		}
	}
}

// ============================================================================
// Groq (AC2) — 无 API key (环境变量未注入)，按文档化行为 mock + 记录。
//   风险点: assistant 消息 reasoning 字段曾 400 (gptel #774) — 本测试为
//   回归护栏: 若 Groq 恢复拒绝，driver 必须分类为通用 LLMError 而非 panic。
// ============================================================================

// TestATDD_75_4_Groq_AssistantReasoning_400_NoPanic: 模拟 Groq 对携带
// reasoning 字段的 assistant 消息返回 400 "property 'reasoning' is
// unsupported" (gptel #774 形状)。driver 必须: 不 panic、返回通用 LLMError 400。
// mock 先断言请求体确实带了双拼法写侧注入 (75.1 AC3 仍在)。
func TestATDD_75_4_Groq_AssistantReasoning_400_NoPanic(t *testing.T) {
	var gotBody map[string]any
	d, _, cleanup := newTestOpenAIDriver(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &gotBody)
		writeJSONStatus(w, 400, `{"error":{"message":"property 'reasoning' is unsupported","type":"invalid_request_error","param":null,"code":null}}`)
	})
	defer cleanup()

	_, err := d.Call(context.Background(), LLMRequest{
		Messages: []Message{
			{Role: "user", Content: "用工具"},
			{Role: "assistant", Content: "", Reasoning: "思考", ToolCalls: []ToolCall{{ID: "call_q1ef", Name: "get_weather", Input: map[string]any{"city": "北京"}}}},
			{Role: "tool", Content: `{"temp":25}`, ToolCallID: "call_q1ef"},
		},
	})
	if err == nil {
		t.Fatal("expected 400 error, got nil")
	}
	var llmErr *LLMError
	if !errors.As(err, &llmErr) {
		t.Fatalf("error = %v, want *LLMError (no panic)", err)
	}
	if llmErr.StatusCode != 400 {
		t.Errorf("StatusCode = %d, want 400", llmErr.StatusCode)
	}
	if errors.Is(err, ErrContextLength) {
		t.Errorf("error misclassified as ErrContextLength")
	}
	// 写侧双拼法注入仍在 (75.1 AC3) — Groq 拒绝的是注入后的请求
	msgs := gotBody["messages"].([]any)
	asst := msgs[1].(map[string]any)
	if asst["reasoning"] != "思考" {
		t.Errorf("assistant reasoning = %v, want 思考 (write-side injection active)", asst["reasoning"])
	}
	if asst["reasoning_content"] != "思考" {
		t.Errorf("assistant reasoning_content = %v, want 思考", asst["reasoning_content"])
	}
}

// TestATDD_75_4_Groq_ShortToolCallID_RoundTrip: Groq 产出 call_q1ef 式短 id —
// convertSDKToolCalls 不得对 id 长度做假设，往返一致。
func TestATDD_75_4_Groq_ShortToolCallID_RoundTrip(t *testing.T) {
	d, _, cleanup := newTestOpenAIDriver(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"id":"c","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"","tool_calls":[{"id":"call_q1ef","type":"function","function":{"name":"get_weather","arguments":"{\"city\":\"北京\"}"}}]},"finish_reason":"tool_calls"}],"usage":{"prompt_tokens":10,"completion_tokens":5,"total_tokens":15}}`)
	})
	defer cleanup()

	resp, err := d.CallWithTools(context.Background(), LLMRequest{Intent: "weather"}, []ToolDef{{Name: "get_weather"}})
	if err != nil {
		t.Fatalf("CallWithTools: %v", err)
	}
	if len(resp.ToolCalls) != 1 {
		t.Fatalf("ToolCalls len = %d, want 1", len(resp.ToolCalls))
	}
	tc := resp.ToolCalls[0]
	if tc.ID != "call_q1ef" {
		t.Errorf("ID = %q, want call_q1ef", tc.ID)
	}
	if tc.Name != "get_weather" {
		t.Errorf("Name = %q, want get_weather", tc.Name)
	}
	if tc.Input["city"] != "北京" {
		t.Errorf("Input[city] = %v, want 北京", tc.Input["city"])
	}
	if tc.ParseError != "" {
		t.Errorf("ParseError = %q, want empty", tc.ParseError)
	}
}

// TestATDD_75_4_Groq_UnsupportedFields_ZeroSend: Groq 拒绝 logprobs /
// logit_bias / top_logprobs / messages[].name；n 必须为 1 — 请求体零发送。
func TestATDD_75_4_Groq_UnsupportedFields_ZeroSend(t *testing.T) {
	var gotBody map[string]any
	d, _, cleanup := newTestOpenAIDriver(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &gotBody)
		writeJSON(w, `{"id":"c","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{}}`)
	})
	defer cleanup()

	_, err := d.Call(context.Background(), LLMRequest{
		Messages: []Message{{Role: "user", Content: "hi", ToolCallID: ""}},
	})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	for _, field := range []string{"logprobs", "logit_bias", "top_logprobs", "n"} {
		if _, present := gotBody[field]; present {
			t.Errorf("request body unexpectedly carries unsupported field %q", field)
		}
	}
	if msgs, ok := gotBody["messages"].([]any); ok && len(msgs) > 0 {
		if _, present := msgs[0].(map[string]any)["name"]; present {
			t.Errorf("messages[0] carries unsupported 'name' field")
		}
	}
}

// TestATDD_75_4_Groq_TemperatureZero_Accepted: Groq 将 temperature:0 转换为
// 1e-8 不报错 — driver 透传 0 时不得 400。
func TestATDD_75_4_Groq_TemperatureZero_Accepted(t *testing.T) {
	var gotBody map[string]any
	d, _, cleanup := newTestOpenAIDriver(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &gotBody)
		writeJSON(w, `{"id":"c","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{}}`)
	})
	defer cleanup()

	zero := 0.0
	_, err := d.Call(context.Background(), LLMRequest{Intent: "hi", Temperature: &zero})
	if err != nil {
		t.Fatalf("Call with temperature 0: %v", err)
	}
	if got := gotBody["temperature"]; got != 0.0 {
		t.Errorf("temperature = %v, want 0 (verbatim)", got)
	}
}

// ============================================================================
// OpenRouter (AC3) — 无 API key，mock + 记录。
//   reasoning 拼法读取已被 75.1 覆盖 (TestOpenAIDriver_Call_ReasoningOpenRouterSpelling
//   + Stream 版)；本文件补 reasoning_details 并存行为。
// ============================================================================

// TestATDD_75_4_OpenRouter_ReasoningDetails_Coexist: 新版 OpenRouter 返回
// choices[].message.reasoning_details 结构化数组，但文档称默认仍填充纯
// `reasoning` 字符串 — 两者并存时 driver 读纯 `reasoning`，details 不影响。
func TestATDD_75_4_OpenRouter_ReasoningDetails_Coexist(t *testing.T) {
	const want = "纯 reasoning 字符串"
	d, _, cleanup := newTestOpenAIDriver(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, `{"id":"c","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"答案","reasoning":"`+want+`","reasoning_details":[{"type":"reasoning","text":"结构化 reasoning 对象","signature":null}]},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":20,"total_tokens":30}}`)
	})
	defer cleanup()

	resp, err := d.Call(context.Background(), LLMRequest{Intent: "hi"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if resp.Reasoning != want {
		t.Errorf("Reasoning = %q, want %q (pure string wins over reasoning_details)", resp.Reasoning, want)
	}
	if resp.Content != "答案" {
		t.Errorf("Content = %q, want 答案", resp.Content)
	}
	if resp.TokensUsed != 30 {
		t.Errorf("TokensUsed = %d, want 30", resp.TokensUsed)
	}
}

// ============================================================================
// GLM / Zhipu (AC4) — 无 API key，mock + 记录。
//   reasoning_content 拼法读取已被 75.1 覆盖；本文件补流式 tool_call 分片累积
//   与流式 usage 结尾 chunk。
// ============================================================================

// TestATDD_75_4_GLM_Stream_ToolCallShards: GLM 流式 tool_call.arguments 按
// index 分片增量返回 — SDK accumulator 必须累积出完整 arguments。
// 同时带 reasoning_content delta (GLM thinking 默认开启)。
func TestATDD_75_4_GLM_Stream_ToolCallShards(t *testing.T) {
	d, _, cleanup := newTestOpenAIDriver(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		writeSSE(w, `{"id":"c","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"reasoning_content":"思考中"},"finish_reason":null}]}`)
		writeSSE(w, `{"id":"c","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"id":"call_glm1","type":"function","function":{"name":"get_weather","arguments":""}}]},"finish_reason":null}]}`)
		writeSSE(w, `{"id":"c","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"city\":"}}]},"finish_reason":null}]}`)
		writeSSE(w, `{"id":"c","object":"chat.completion.chunk","choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"\"北京\"}"}}]},"finish_reason":null}]}`)
		writeSSE(w, `{"id":"c","object":"chat.completion.chunk","choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`)
		writeSSE(w, `{"id":"c","object":"chat.completion.chunk","choices":[],"usage":{"prompt_tokens":15,"completion_tokens":10,"total_tokens":25}}`)
		writeSSE(w, "[DONE]")
	})
	defer cleanup()

	ch, err := d.StreamWithTools(context.Background(), LLMRequest{Intent: "weather"}, []ToolDef{{Name: "get_weather"}})
	if err != nil {
		t.Fatalf("StreamWithTools: %v", err)
	}

	var reasoning []string
	var doneEvt StreamEvent
	for evt := range ch {
		switch evt.Type {
		case "reasoning":
			reasoning = append(reasoning, evt.Content)
		case "done":
			doneEvt = evt
		case "error":
			t.Fatalf("unexpected error event: %v", evt.Err)
		}
	}
	if got := strings.Join(reasoning, ""); got != "思考中" {
		t.Errorf("reasoning = %q, want %q", got, "思考中")
	}
	if len(doneEvt.ToolCalls) != 1 {
		t.Fatalf("ToolCalls len = %d, want 1", len(doneEvt.ToolCalls))
	}
	tc := doneEvt.ToolCalls[0]
	if tc.ID != "call_glm1" {
		t.Errorf("ID = %q, want call_glm1", tc.ID)
	}
	if tc.Input["city"] != "北京" {
		t.Errorf("Input[city] = %v, want 北京 (shard accumulation)", tc.Input["city"])
	}
	// 流式 usage: 结尾 chunk (include_usage) → done 事件 token 字段正确
	if doneEvt.TokensUsed != 25 {
		t.Errorf("TokensUsed = %d, want 25", doneEvt.TokensUsed)
	}
	if doneEvt.OutputTokens != 10 {
		t.Errorf("OutputTokens = %d, want 10", doneEvt.OutputTokens)
	}
}
