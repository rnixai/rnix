package context

import (
	"strconv"
	"strings"
	"testing"

	"github.com/rnixai/rnix/internal/types"
)

// Story 69.2 — token 统计口径补全（AC2）与回归红线（AC7）。
//
// 基线缺陷：TokenUsage 只累加 EstimateTokens(msg.Content)，ToolCalls[].Input /
// ReasoningBlocks[] / Message.Reasoning 三类载荷完全不计。dev worker 负载正是
// 大量工具调用，故小上下文偏差达 41.7%（卷宗三点基准）。
//
// 本文件的用例分两组：
//   - RED 组（骨架下必红）：EstimateMessageTokens 逐字段 + TokenUsage 漏算护栏。
//   - green-guard 组（全程须绿）：口径一致护栏 / AC1 降级语义。

// --- RED: EstimateMessageTokens 逐字段 ---

func TestEstimateMessageTokens_ContentOnly(t *testing.T) {
	msg := Message{Role: RoleUser, Content: "Hello, World!"}
	got := EstimateMessageTokens(msg)
	want := EstimateTokens("Hello, World!")
	if got != want {
		t.Errorf("EstimateMessageTokens(content-only) = %d, want %d", got, want)
	}
}

func TestEstimateMessageTokens_EmptyMessage(t *testing.T) {
	if got := EstimateMessageTokens(Message{}); got != 0 {
		t.Errorf("EstimateMessageTokens(empty) = %d, want 0", got)
	}
}

func TestEstimateMessageTokens_ToolCallsCounted(t *testing.T) {
	base := Message{Role: RoleAssistant, Content: "calling a tool"}
	withTool := base
	withTool.ToolCalls = []ToolCall{{
		ID:   "call_1",
		Name: "Read",
		Input: map[string]any{
			"file_path": strings.Repeat("/very/long/path/segment", 40),
			"limit":     2000,
		},
	}}

	baseTokens := EstimateMessageTokens(base)
	toolTokens := EstimateMessageTokens(withTool)
	if toolTokens <= baseTokens {
		t.Errorf("ToolCalls not counted: with=%d, base=%d (want with > base)", toolTokens, baseTokens)
	}
}

func TestEstimateMessageTokens_NestedToolInputCounted(t *testing.T) {
	// 嵌套 map Input —— json.Marshal 递归序列化，体积必须计入。
	shallow := Message{Role: RoleAssistant, ToolCalls: []ToolCall{{
		ID: "c", Name: "Bash", Input: map[string]any{"command": "ls"},
	}}}
	nested := Message{Role: RoleAssistant, ToolCalls: []ToolCall{{
		ID: "c", Name: "Bash", Input: map[string]any{
			"command": "ls",
			"env": map[string]any{
				"PATH":    strings.Repeat("/usr/local/bin:", 30),
				"nested2": map[string]any{"deep": strings.Repeat("x", 200)},
			},
		},
	}}}

	if got, want := EstimateMessageTokens(nested), EstimateMessageTokens(shallow); got <= want {
		t.Errorf("nested tool input not counted: nested=%d, shallow=%d (want nested > shallow)", got, want)
	}
}

func TestEstimateMessageTokens_ToolNameCounted(t *testing.T) {
	// 工具名进 wire，须计入。Input 相同、仅 Name 长度不同。
	short := Message{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "c", Name: "Ls"}}}
	long := Message{Role: RoleAssistant, ToolCalls: []ToolCall{{ID: "c", Name: strings.Repeat("VeryLongToolName", 20)}}}

	if got, want := EstimateMessageTokens(long), EstimateMessageTokens(short); got <= want {
		t.Errorf("ToolCalls[].Name not counted: long=%d, short=%d (want long > short)", got, want)
	}
}

func TestEstimateMessageTokens_ToolInputStable(t *testing.T) {
	// 新发现 3：fmt.Sprintf("%v", map) 的迭代顺序随机会让 token 数抖动。
	// json.Marshal 的 key 有序 → 同一 Message 多次估算必须恒等。
	msg := Message{Role: RoleAssistant, ToolCalls: []ToolCall{{
		ID:   "c",
		Name: "Write",
		Input: map[string]any{
			"alpha": "a", "beta": "b", "gamma": "c", "delta": "d",
			"epsilon": "e", "zeta": "f", "eta": "g", "theta": "h",
		},
	}}}

	first := EstimateMessageTokens(msg)
	for i := range 50 {
		if got := EstimateMessageTokens(msg); got != first {
			t.Fatalf("iteration %d: EstimateMessageTokens unstable: %d != %d (map iteration order leaked into the estimate)", i, got, first)
		}
	}
}

func TestEstimateMessageTokens_ReasoningBlocksCounted(t *testing.T) {
	base := Message{Role: RoleAssistant, Content: "answer"}
	withThinking := base
	withThinking.ReasoningBlocks = []ReasoningBlock{{
		Type:     "thinking",
		Thinking: strings.Repeat("let me think about this carefully. ", 30),
	}}

	if got, want := EstimateMessageTokens(withThinking), EstimateMessageTokens(base); got <= want {
		t.Errorf("ReasoningBlocks[].Thinking not counted: with=%d, base=%d", got, want)
	}
}

func TestEstimateMessageTokens_RedactedThinkingDataCounted(t *testing.T) {
	base := Message{Role: RoleAssistant, Content: "answer"}
	withData := base
	withData.ReasoningBlocks = []ReasoningBlock{{
		Type: "redacted_thinking",
		Data: strings.Repeat("EncRypTeDb64Payload", 40),
	}}

	if got, want := EstimateMessageTokens(withData), EstimateMessageTokens(base); got <= want {
		t.Errorf("ReasoningBlocks[].Data not counted: with=%d, base=%d", got, want)
	}
}

func TestEstimateMessageTokens_FlatReasoningCounted(t *testing.T) {
	// 新发现 2：Message.Reasoning 平铺字段（openai-compat 类 driver 的
	// reasoning_content）同样漏算。
	base := Message{Role: RoleAssistant, Content: "answer"}
	withReasoning := base
	withReasoning.Reasoning = strings.Repeat("chain of thought text. ", 30)

	if got, want := EstimateMessageTokens(withReasoning), EstimateMessageTokens(base); got <= want {
		t.Errorf("Message.Reasoning not counted: with=%d, base=%d", got, want)
	}
}

func TestEstimateMessageTokens_MultipleToolCallsAccumulate(t *testing.T) {
	one := Message{Role: RoleAssistant, ToolCalls: []ToolCall{
		{ID: "a", Name: "Read", Input: map[string]any{"path": strings.Repeat("x", 100)}},
	}}
	three := Message{Role: RoleAssistant, ToolCalls: []ToolCall{
		{ID: "a", Name: "Read", Input: map[string]any{"path": strings.Repeat("x", 100)}},
		{ID: "b", Name: "Read", Input: map[string]any{"path": strings.Repeat("y", 100)}},
		{ID: "c", Name: "Read", Input: map[string]any{"path": strings.Repeat("z", 100)}},
	}}

	if got, want := EstimateMessageTokens(three), 2*EstimateMessageTokens(one); got <= want {
		t.Errorf("multiple ToolCalls do not accumulate: three=%d, 2*one=%d", got, want)
	}
}

// --- RED: TokenUsage 漏算护栏（本 story 主证）---

func TestTokenUsage_CountsToolCallInput(t *testing.T) {
	// A: 只有 Content。B: 同 Content + 大 ToolCalls.Input。
	// 基线实现下两者 Used 相等 → 真红。
	mkCtx := func(t *testing.T) (*Manager, types.CtxID) {
		t.Helper()
		mgr := NewManager()
		cid, err := mgr.CtxAlloc(100)
		if err != nil {
			t.Fatalf("CtxAlloc: %v", err)
		}
		return mgr, cid
	}

	mgrA, cidA := mkCtx(t)
	if err := mgrA.AppendMessage(cidA, RoleAssistant, "same content"); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}
	statsA, err := mgrA.TokenUsage(cidA)
	if err != nil {
		t.Fatalf("TokenUsage(A): %v", err)
	}

	mgrB, cidB := mkCtx(t)
	bigInput := map[string]any{"content": strings.Repeat("payload ", 500)}
	if err := mgrB.AppendAssistantWithToolCalls(cidB, "same content", "", nil,
		[]ToolCall{{ID: "call_1", Name: "Write", Input: bigInput}}); err != nil {
		t.Fatalf("AppendAssistantWithToolCalls: %v", err)
	}
	statsB, err := mgrB.TokenUsage(cidB)
	if err != nil {
		t.Fatalf("TokenUsage(B): %v", err)
	}

	if statsB.Used <= statsA.Used {
		t.Errorf("ToolCalls.Input not counted in TokenUsage: B.Used=%d, A.Used=%d (want B > A)",
			statsB.Used, statsA.Used)
	}
}

func TestTokenUsage_CountsReasoning(t *testing.T) {
	mgrA := NewManager()
	cidA, err := mgrA.CtxAlloc(100)
	if err != nil {
		t.Fatalf("CtxAlloc: %v", err)
	}
	if err := mgrA.AppendAssistantWithToolCalls(cidA, "answer", "", nil, nil); err != nil {
		t.Fatalf("AppendAssistantWithToolCalls(A): %v", err)
	}
	statsA, err := mgrA.TokenUsage(cidA)
	if err != nil {
		t.Fatalf("TokenUsage(A): %v", err)
	}

	mgrB := NewManager()
	cidB, err := mgrB.CtxAlloc(100)
	if err != nil {
		t.Fatalf("CtxAlloc: %v", err)
	}
	blocks := []ReasoningBlock{{Type: "thinking", Thinking: strings.Repeat("thought ", 300)}}
	if err := mgrB.AppendAssistantWithToolCalls(cidB, "answer",
		strings.Repeat("flat reasoning ", 200), blocks, nil); err != nil {
		t.Fatalf("AppendAssistantWithToolCalls(B): %v", err)
	}
	statsB, err := mgrB.TokenUsage(cidB)
	if err != nil {
		t.Fatalf("TokenUsage(B): %v", err)
	}

	if statsB.Used <= statsA.Used {
		t.Errorf("Reasoning/ReasoningBlocks not counted in TokenUsage: B.Used=%d, A.Used=%d",
			statsB.Used, statsA.Used)
	}
}

// --- green-guard: 口径一致护栏（防三处分叉回归）---

func TestEstimateMessagesTokens_MatchesPerMessageSum(t *testing.T) {
	mgr := NewManager()
	cid, err := mgr.CtxAlloc(100)
	if err != nil {
		t.Fatalf("CtxAlloc: %v", err)
	}
	if err := mgr.AppendMessage(cid, RoleUser, "please read a file"); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}
	if err := mgr.AppendAssistantWithToolCalls(cid, "sure", "flat reasoning",
		[]ReasoningBlock{{Type: "thinking", Thinking: "thinking text"}},
		[]ToolCall{{ID: "c1", Name: "Read", Input: map[string]any{"path": "/tmp/x", "limit": 10}}}); err != nil {
		t.Fatalf("AppendAssistantWithToolCalls: %v", err)
	}
	if err := mgr.AppendToolResult(cid, "c1", "file contents here"); err != nil {
		t.Fatalf("AppendToolResult: %v", err)
	}

	ctx, err := mgr.GetContext(cid)
	if err != nil {
		t.Fatalf("GetContext: %v", err)
	}

	ctx.mu.RLock()
	viaHelper := mgr.estimateMessagesTokens(ctx)
	sum := 0
	for _, msg := range ctx.Messages {
		sum += EstimateMessageTokens(msg)
	}
	ctx.mu.RUnlock()

	if viaHelper != sum {
		t.Errorf("estimateMessagesTokens = %d, Σ EstimateMessageTokens = %d (the two call sites diverged)",
			viaHelper, sum)
	}
}

// --- green-guard: AC1 降级语义（未设 TokenLimit → DefaultTokenLimit）---

func TestTokenUsage_LimitFallsBackToDefault(t *testing.T) {
	mgr := NewManager()
	cid, err := mgr.CtxAlloc(100)
	if err != nil {
		t.Fatalf("CtxAlloc: %v", err)
	}
	if err := mgr.AppendMessage(cid, RoleUser, "hi"); err != nil {
		t.Fatalf("AppendMessage: %v", err)
	}

	stats, err := mgr.TokenUsage(cid)
	if err != nil {
		t.Fatalf("TokenUsage: %v", err)
	}
	if stats.Limit != DefaultTokenLimit {
		t.Errorf("Limit = %d, want DefaultTokenLimit %d (AC1 degradation semantics)", stats.Limit, DefaultTokenLimit)
	}
}

// --- AC3：偏差方向与量级实测（记录用，不设"凑数字"断言）---

// estimateContentOnly reproduces the pre-Story-69.2 accounting so the same
// context can be measured under both口径 in one run. It is a test-local replica
// on purpose: production code must have exactly one accounting path
// (EstimateMessageTokens), so this baseline lives here rather than being kept
// alive as a second production function.
func estimateContentOnly(msgs []Message) int {
	total := 0
	for _, msg := range msgs {
		total += EstimateTokens(msg.Content)
	}
	return total
}

// TestAC3_ToolHeavyContextDeviation measures how much payload the old
// Content-only口径 could not see, on a context shaped like the dev-worker load
// from the investigation: every assistant turn carries its real payload inside
// ToolCalls[].Input (file writes, command arguments) while Content is a short
// sentence, and tool results come back as separate messages.
//
// The assertion is deliberately weak — a direction-and-magnitude check, not a
// target number. AC3 forbids tuning EstimateTokens' ratios or adding a
// compensation multiplier, so the residual against provider-reported
// prompt_tokens (tool schemas travel outside ctx.Messages, and provider
// tokenizers differ) is recorded rather than engineered away.
func TestAC3_ToolHeavyContextDeviation(t *testing.T) {
	mgr := NewManager()
	cid, err := mgr.CtxAlloc(256)
	if err != nil {
		t.Fatalf("CtxAlloc: %v", err)
	}
	if err := mgr.SetSystemPrompt(cid, strings.Repeat("system rules. ", 100)); err != nil {
		t.Fatalf("SetSystemPrompt: %v", err)
	}

	const rounds = 20
	for i := range rounds {
		id := "call_" + strconv.Itoa(i)
		calls := []ToolCall{{
			ID:   id,
			Name: "Write",
			Input: map[string]any{
				"file_path": "/mnt/disk0/project/src/module_" + strconv.Itoa(i) + ".go",
				"content":   strings.Repeat("func handler() error { return nil }\n", 40),
			},
		}}
		blocks := []ReasoningBlock{{
			Type:      "thinking",
			Thinking:  strings.Repeat("considering the next edit. ", 20),
			Signature: strings.Repeat("sig", 30), // 刻意不计（见 EstimateMessageTokens 注释）
		}}
		if err := mgr.AppendAssistantWithToolCalls(cid, "Writing the next module.",
			strings.Repeat("flat reasoning. ", 10), blocks, calls); err != nil {
			t.Fatalf("round %d AppendAssistantWithToolCalls: %v", i, err)
		}
		if err := mgr.AppendToolResult(cid, id, "wrote 40 lines"); err != nil {
			t.Fatalf("round %d AppendToolResult: %v", i, err)
		}
	}

	ctx, err := mgr.GetContext(cid)
	if err != nil {
		t.Fatalf("GetContext: %v", err)
	}
	ctx.mu.RLock()
	msgs := make([]Message, len(ctx.Messages))
	copy(msgs, ctx.Messages)
	ctx.mu.RUnlock()

	before := estimateContentOnly(msgs)
	stats, err := mgr.TokenUsage(cid)
	if err != nil {
		t.Fatalf("TokenUsage: %v", err)
	}
	// stats.Used includes the system prompt; compare message payload only.
	after := 0
	for _, msg := range msgs {
		after += EstimateMessageTokens(msg)
	}

	if after <= before {
		t.Fatalf("accounting did not grow: before=%d after=%d", before, after)
	}
	ratio := float64(before) / float64(after) * 100
	t.Logf("AC3 tool-heavy context (%d rounds, %d messages): Content-only=%d tokens, "+
		"full accounting=%d tokens (old口径 saw %.1f%% of the payload; %d tokens were invisible). "+
		"TokenUsage().Used=%d including the system prompt.",
		rounds, len(msgs), before, after, ratio, after-before, stats.Used)

	// 方向与量级：工具调用密集负载下旧口径漏掉的载荷必须是主要部分，而非零头。
	if ratio > 60.0 {
		t.Errorf("Content-only口径 still accounts for %.1f%% of a tool-heavy context — "+
			"expected the missing payload to dominate (投卷宗基准 41.7%%/75.4%%/90.2%%)", ratio)
	}
}

func TestSetTokenLimit_ReflectedInTokenUsage(t *testing.T) {
	mgr := NewManager()
	cid, err := mgr.CtxAlloc(100)
	if err != nil {
		t.Fatalf("CtxAlloc: %v", err)
	}
	const want = 885_254 // 983616 * 9 / 10
	if err := mgr.SetTokenLimit(cid, want); err != nil {
		t.Fatalf("SetTokenLimit: %v", err)
	}

	stats, err := mgr.TokenUsage(cid)
	if err != nil {
		t.Fatalf("TokenUsage: %v", err)
	}
	if stats.Limit != want {
		t.Errorf("Limit = %d, want %d", stats.Limit, want)
	}
}
