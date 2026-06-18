package llm

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// ATDD Story 60.1 — AC1 + AC4 (driver 层): reasoning/thinking 事件入流统一。
//
// 痛点根源 (investigations/llm-thinking-phase-invisible): writeStream 的
// `case "reasoning"` 只做 reasoning.WriteString,从不调 f.onEvent → API driver
// (anthropic/gemini/openai-compat) 的思考增量永不成为实时可观测事件。
//
// 本文件复用 vfsfile_test.go 既有的 streamMockDriver（events []StreamEvent）+
// LLMFile.SetStreamHandler,无需任何生产骨架——AC1 是真 RED(skip),AC4 是
// green-guard(不 skip,实时钉住"转发是旁路、不改 LLMResponse 构造")。
//
// 红灯机制 [[atdd-code-story-red-mechanism-preference]]: RED 用 t.Skip 保
// 提交期 make all 绿; dev 移 skip 填 vfsfile.go 转发逻辑验 RED→GREEN。

// ---------------------------------------------------------------------------
// 60.1-UNIT-001 (AC1, RED): reasoning 增量必须转发到 onEvent(归一为 thinking)
// ---------------------------------------------------------------------------

func TestATDD_60_1_AC1_ReasoningForwardedToOnEvent(t *testing.T) {
	t.Skip("RED 60.1-UNIT-001: vfsfile.go `case \"reasoning\"` 尚未转发 onEvent" +
		"(归一为 type:\"thinking\")。dev 移除本 skip 验 RED→GREEN")

	driver := &streamMockDriver{events: []StreamEvent{
		{Type: "reasoning", Content: "let me think step by step"},
		{Type: "reasoning", Content: " ... weighing options"},
		{Type: "done", Content: "the answer is 42", TokensUsed: 10},
	}}
	f := &LLMFile{driver: driver, devicePath: "/dev/llm/anthropic"} // 默认 mode = stream

	// streamMockDriver.Stream 同步填 channel,writeStream 在 Write goroutine 内
	// 同步 range → onEvent 同步触发,无需 mutex。
	var forwarded []map[string]any
	f.SetStreamHandler(func(evt map[string]any) {
		forwarded = append(forwarded, evt)
	})

	reqJSON, _ := json.Marshal(LLMRequest{Intent: "test"})
	if err := f.Write(context.Background(), reqJSON); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	// AC1: reasoning 必须成为可观测事件,且归一为 "thinking" 命中下游既有映射。
	var sawThinking bool
	var combined strings.Builder
	for _, e := range forwarded {
		if e["type"] == "thinking" {
			sawThinking = true
			if c, ok := e["content"].(string); ok {
				combined.WriteString(c)
			}
		}
	}
	if !sawThinking {
		t.Fatalf("AC1: 期望至少一个归一为 type=\"thinking\" 的转发事件,实得 %d 个事件: %+v",
			len(forwarded), forwarded)
	}
	if !strings.Contains(combined.String(), "weighing options") {
		t.Errorf("AC1: 转发的思考内容应携带 reasoning 增量,实得 %q", combined.String())
	}
}

// ---------------------------------------------------------------------------
// 60.1-UNIT-002 (AC4, green-guard): 转发是旁路,Reasoning 累积不变
// ---------------------------------------------------------------------------

func TestATDD_60_1_AC4_ReasoningStillAccumulated(t *testing.T) {
	// green-guard(不 skip): 当前即 PASS; dev 在 case "reasoning" 加 onEvent
	// 转发后,reasoning.WriteString 必须保留 → LLMResponse.Reasoning 不回归。
	driver := &streamMockDriver{events: []StreamEvent{
		{Type: "reasoning", Content: "alpha"},
		{Type: "reasoning", Content: "beta"},
		{Type: "done", Content: "final", TokensUsed: 7},
	}}
	f := &LLMFile{driver: driver, devicePath: "/dev/llm/anthropic"}
	f.SetStreamHandler(func(_ map[string]any) {}) // 设 handler 验证转发与否都不影响累积

	reqJSON, _ := json.Marshal(LLMRequest{Intent: "test"})
	if err := f.Write(context.Background(), reqJSON); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	data, err := f.Read(0)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	var resp LLMResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if resp.Reasoning != "alphabeta" {
		t.Errorf("AC4: Reasoning 累积应为 \"alphabeta\",实得 %q", resp.Reasoning)
	}
	if resp.Content != "final" {
		t.Errorf("AC4: Content 应为 \"final\",实得 %q", resp.Content)
	}
}

// ---------------------------------------------------------------------------
// 60.1-UNIT-003 (AC4, green-guard): ReasoningBlocks signature round-trip 不丢
// ---------------------------------------------------------------------------

func TestATDD_60_1_AC4_ReasoningBlocksRoundTripIntact(t *testing.T) {
	// green-guard(不 skip): thinking-mode 下一轮请求必需 signature round-trip,
	// 缺失致 Anthropic HTTP 400(见 anthropic.go 块顺序约定)。AC1 转发不得破坏。
	driver := &streamMockDriver{events: []StreamEvent{
		{Type: "reasoning", Content: "thinking aloud"},
		{Type: "done", Content: "answer", TokensUsed: 12, ReasoningBlocks: []ReasoningBlock{
			{Type: "thinking", Thinking: "weighing", Signature: "sig_60_1"},
			{Type: "redacted_thinking", Data: "enc-blob"},
		}},
	}}
	f := &LLMFile{driver: driver, devicePath: "/dev/llm/anthropic"}
	f.SetStreamHandler(func(_ map[string]any) {})

	reqJSON, _ := json.Marshal(LLMRequest{Intent: "test"})
	if err := f.Write(context.Background(), reqJSON); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	data, err := f.Read(0)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	var resp LLMResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(resp.ReasoningBlocks) != 2 {
		t.Fatalf("AC4: ReasoningBlocks 应保留 2 块,实得 %d", len(resp.ReasoningBlocks))
	}
	if resp.ReasoningBlocks[0].Signature != "sig_60_1" {
		t.Errorf("AC4: 思考 signature 丢失: %+v", resp.ReasoningBlocks[0])
	}
	if resp.ReasoningBlocks[1].Type != "redacted_thinking" || resp.ReasoningBlocks[1].Data != "enc-blob" {
		t.Errorf("AC4: redacted_thinking 丢失: %+v", resp.ReasoningBlocks[1])
	}
}
