package llm

// Per-request reasoning_effort override coverage (spec-reasoning-effort-per-request).
//
// Story 55.1 proved the driver-instance default (WithXxxEffort) lands in the
// request. This file proves the SECOND tier: a per-request LLMRequest.ReasoningEffort
// overrides that instance default, and an empty request field falls back to it
// (zero-regression). The two tiers mirror Model's resolveModel(req) pattern.
//
// Each supporting driver gets:
//   - override: req.ReasoningEffort != "" wins over the instance default.
//   - fallback: req.ReasoningEffort == "" uses the instance default verbatim.
//   - case/forward-compat passthrough: arbitrary values pass untouched.
//   - budget interplay preserved under the override path (anthropic/gemini mutual
//     exclusion; openai-compat orthogonal coexistence).
// no-op drivers (cursor/qwen) ignore a non-empty req.ReasoningEffort without error.

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
)

// --- openai (official SDK) ---------------------------------------------------

func TestOpenAIDriver_ReasoningEffort_RequestOverride(t *testing.T) {
	var gotBody map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		writeJSON(w, okCompletion)
	}))
	defer ts.Close()

	// Instance default "low"; the request asks for "high" → request wins.
	d := NewOpenAIDriver("test-openai",
		WithOpenAIModel("gpt-5.1"),
		WithOpenAIBaseURL(ts.URL),
		WithOpenAIKey("sk-test"),
		WithOpenAIHTTPClient(ts.Client()),
		WithOpenAIReasoningEffort("low"),
	)
	if _, err := d.Call(context.Background(), LLMRequest{Intent: "hi", ReasoningEffort: "high"}); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if gotBody["reasoning_effort"] != "high" {
		t.Errorf("reasoning_effort = %v, want high (request overrides instance low)", gotBody["reasoning_effort"])
	}
}

func TestOpenAIDriver_ReasoningEffort_RequestEmpty_FallsBackToInstance(t *testing.T) {
	var gotBody map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		writeJSON(w, okCompletion)
	}))
	defer ts.Close()

	d := NewOpenAIDriver("test-openai",
		WithOpenAIModel("gpt-5.1"),
		WithOpenAIBaseURL(ts.URL),
		WithOpenAIKey("sk-test"),
		WithOpenAIHTTPClient(ts.Client()),
		WithOpenAIReasoningEffort("medium"),
	)
	// Empty request field → instance default "medium" (zero-regression).
	if _, err := d.Call(context.Background(), LLMRequest{Intent: "hi"}); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if gotBody["reasoning_effort"] != "medium" {
		t.Errorf("reasoning_effort = %v, want medium (fallback to instance default)", gotBody["reasoning_effort"])
	}
}

// --- openai-compat -----------------------------------------------------------

func TestOpenAICompatDriver_ReasoningEffort_RequestOverride(t *testing.T) {
	var gotBody oaiRequest
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		writeJSON(w, `{"choices":[{"message":{"content":"ok"}}],"usage":{}}`)
	}))
	defer ts.Close()

	d := NewOpenAICompatDriver("test", ts.URL,
		WithCompatModel("test-model"),
		WithHTTPClient(ts.Client()),
		WithCompatReasoningEffort("low"),
	)
	if _, err := d.Call(context.Background(), LLMRequest{Intent: "hi", ReasoningEffort: "high"}); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if gotBody.ReasoningEffort != "high" {
		t.Errorf("reasoning_effort = %q, want high (request override)", gotBody.ReasoningEffort)
	}
}

// TestOpenAICompatDriver_ReasoningEffort_RequestOverride_CoexistsWithBudget
// proves the per-request override does NOT disturb the orthogonal thinking_budget
// path (DeepSeek multi-turn tool calls): both ship together.
func TestOpenAICompatDriver_ReasoningEffort_RequestOverride_CoexistsWithBudget(t *testing.T) {
	var gotBody oaiRequest
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		writeJSON(w, `{"choices":[{"message":{"content":"ok"}}],"usage":{}}`)
	}))
	defer ts.Close()

	d := NewOpenAICompatDriver("test", ts.URL,
		WithCompatModel("deepseek-v4"),
		WithHTTPClient(ts.Client()),
		WithCompatThinkingBudget(8192),
		WithCompatReasoningEffort("low"),
	)
	if _, err := d.Call(context.Background(), LLMRequest{Intent: "hi", ReasoningEffort: "high"}); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if gotBody.ReasoningEffort != "high" {
		t.Errorf("reasoning_effort = %q, want high (request override)", gotBody.ReasoningEffort)
	}
	if gotBody.Thinking == nil || gotBody.Thinking.BudgetTokens != 8192 {
		t.Errorf("thinking budget not retained alongside per-request effort: %+v", gotBody.Thinking)
	}
}

// --- anthropic ---------------------------------------------------------------

func TestAnthropicDriver_Effort_RequestOverride(t *testing.T) {
	// Instance "low"; request "high" → request wins, budget path stays disengaged.
	d := NewAnthropicDriver("test", WithAnthropicEffort("low"))
	params := d.buildParams(LLMRequest{Intent: "hi", ReasoningEffort: "high"}, nil)
	if string(params.OutputConfig.Effort) != "high" {
		t.Errorf("OutputConfig.Effort = %q, want high (request override)", params.OutputConfig.Effort)
	}
	if params.Thinking.GetBudgetTokens() != nil {
		t.Errorf("Thinking budget set under effort path, want nil")
	}
}

func TestAnthropicDriver_Effort_RequestEmpty_FallsBackToInstance(t *testing.T) {
	d := NewAnthropicDriver("test", WithAnthropicEffort("max"))
	params := d.buildParams(LLMRequest{Intent: "hi"}, nil)
	if string(params.OutputConfig.Effort) != "max" {
		t.Errorf("OutputConfig.Effort = %q, want max (fallback to instance)", params.OutputConfig.Effort)
	}
}

// TestAnthropicDriver_Effort_RequestOverride_PriorityOverBudget verifies the
// mutual-exclusion switch still picks effort (now from the request) over budget.
func TestAnthropicDriver_Effort_RequestOverride_PriorityOverBudget(t *testing.T) {
	// No instance effort, only a budget — the REQUEST supplies the effort and must
	// still win the switch, leaving the budget path disengaged.
	d := NewAnthropicDriver("test", WithAnthropicThinkingBudget(8192))
	params := d.buildParams(LLMRequest{Intent: "hi", ReasoningEffort: "high"}, nil)
	if string(params.OutputConfig.Effort) != "high" {
		t.Errorf("OutputConfig.Effort = %q, want high (request effort beats instance budget)", params.OutputConfig.Effort)
	}
	if params.Thinking.GetBudgetTokens() != nil {
		t.Errorf("Thinking budget set despite request effort priority, want nil")
	}
}

// --- gemini ------------------------------------------------------------------

func TestGeminiDriver_ThinkingLevel_RequestOverride(t *testing.T) {
	// Instance "LOW"; request "HIGH" (uppercase passthrough) → request wins.
	d := NewGeminiDriver("test", WithGeminiThinkingLevel("LOW"))
	cfg := d.buildConfig(LLMRequest{Intent: "hi", ReasoningEffort: "HIGH"}, nil)
	if cfg.ThinkingConfig == nil {
		t.Fatal("ThinkingConfig nil, want set")
	}
	if string(cfg.ThinkingConfig.ThinkingLevel) != "HIGH" {
		t.Errorf("ThinkingLevel = %q, want HIGH (request override, uppercase passthrough)", cfg.ThinkingConfig.ThinkingLevel)
	}
	if cfg.ThinkingConfig.ThinkingBudget != nil {
		t.Errorf("ThinkingBudget set alongside level, want nil (mutually exclusive)")
	}
}

func TestGeminiDriver_ThinkingLevel_RequestEmpty_FallsBackToInstance(t *testing.T) {
	d := NewGeminiDriver("test", WithGeminiThinkingLevel("MEDIUM"))
	cfg := d.buildConfig(LLMRequest{Intent: "hi"}, nil)
	if cfg.ThinkingConfig == nil {
		t.Fatal("ThinkingConfig nil, want set")
	}
	if string(cfg.ThinkingConfig.ThinkingLevel) != "MEDIUM" {
		t.Errorf("ThinkingLevel = %q, want MEDIUM (fallback to instance)", cfg.ThinkingConfig.ThinkingLevel)
	}
}

// TestGeminiDriver_ThinkingLevel_RequestOverride_MutualExclusion verifies the
// per-request level still wins over an instance budget (Gemini 3 rejects both).
func TestGeminiDriver_ThinkingLevel_RequestOverride_MutualExclusion(t *testing.T) {
	d := NewGeminiDriver("test", WithGeminiThinkingBudget(4096))
	cfg := d.buildConfig(LLMRequest{Intent: "hi", ReasoningEffort: "LOW"}, nil)
	if cfg.ThinkingConfig == nil {
		t.Fatal("ThinkingConfig nil, want set")
	}
	if string(cfg.ThinkingConfig.ThinkingLevel) != "LOW" {
		t.Errorf("ThinkingLevel = %q, want LOW (request level beats instance budget)", cfg.ThinkingConfig.ThinkingLevel)
	}
	if cfg.ThinkingConfig.ThinkingBudget != nil {
		t.Errorf("ThinkingBudget set despite request level, want nil (mutually exclusive)")
	}
}

// --- claude-cli --------------------------------------------------------------

func TestClaudeCliDriver_Effort_RequestOverride(t *testing.T) {
	var capturedArgs []string
	d := NewClaudeCliDriver(
		WithClaudeEffort("low"),
		WithCommandBuilder(func(ctx context.Context, name string, args ...string) *exec.Cmd {
			capturedArgs = args
			return mockCmdBuilder("success")(ctx, name, args...)
		}),
	)
	if _, err := d.Call(context.Background(), LLMRequest{Intent: "hi", ReasoningEffort: "xhigh"}); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !argsContainPair(capturedArgs, "--effort", "xhigh") {
		t.Errorf("expected --effort xhigh (request override), got: %s", strings.Join(capturedArgs, " "))
	}
}

func TestClaudeCliDriver_Effort_RequestEmpty_FallsBackToInstance(t *testing.T) {
	var capturedArgs []string
	d := NewClaudeCliDriver(
		WithClaudeEffort("medium"),
		WithCommandBuilder(func(ctx context.Context, name string, args ...string) *exec.Cmd {
			capturedArgs = args
			return mockCmdBuilder("success")(ctx, name, args...)
		}),
	)
	if _, err := d.Call(context.Background(), LLMRequest{Intent: "hi"}); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !argsContainPair(capturedArgs, "--effort", "medium") {
		t.Errorf("expected --effort medium (fallback to instance), got: %s", strings.Join(capturedArgs, " "))
	}
}

// --- codex-cli ---------------------------------------------------------------

func TestCodexCliDriver_ReasoningEffort_RequestOverride(t *testing.T) {
	var capturedArgs []string
	d := NewCodexCliDriver(
		CodexWithReasoningEffort("low"),
		CodexWithCommandBuilder(func(ctx context.Context, name string, args ...string) *exec.Cmd {
			capturedArgs = args
			return codexMockCmdBuilder("codex_call_success")(ctx, name, args...)
		}),
	)
	if _, err := d.Call(context.Background(), LLMRequest{Intent: "hi", ReasoningEffort: "high"}); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !argsContainPair(capturedArgs, "-c", "model_reasoning_effort=high") {
		t.Errorf("expected -c model_reasoning_effort=high (request override), got: %s", strings.Join(capturedArgs, " "))
	}
}

func TestCodexCliDriver_ReasoningEffort_RequestEmpty_FallsBackToInstance(t *testing.T) {
	var capturedArgs []string
	d := NewCodexCliDriver(
		CodexWithReasoningEffort("medium"),
		CodexWithCommandBuilder(func(ctx context.Context, name string, args ...string) *exec.Cmd {
			capturedArgs = args
			return codexMockCmdBuilder("codex_call_success")(ctx, name, args...)
		}),
	)
	if _, err := d.Call(context.Background(), LLMRequest{Intent: "hi"}); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !argsContainPair(capturedArgs, "-c", "model_reasoning_effort=medium") {
		t.Errorf("expected -c model_reasoning_effort=medium (fallback to instance), got: %s", strings.Join(capturedArgs, " "))
	}
}

// --- no-op drivers (cursor / qwen) -------------------------------------------

// TestCursorCliDriver_ReasoningEffort_RequestNoOp verifies a non-empty
// req.ReasoningEffort neither errors nor injects any effort flag — cursor-cli
// has no standalone effort parameter.
func TestCursorCliDriver_ReasoningEffort_RequestNoOp(t *testing.T) {
	var capturedArgs []string
	d := NewCursorCliDriver(
		CursorWithCommandBuilder(func(ctx context.Context, name string, args ...string) *exec.Cmd {
			capturedArgs = args
			return cursorMockCmdBuilder("cursor_success")(ctx, name, args...)
		}),
	)
	if _, err := d.Call(context.Background(), LLMRequest{Intent: "hi", ReasoningEffort: "high"}); err != nil {
		t.Fatalf("Call: %v (no-op driver must not error on request effort)", err)
	}
	joined := strings.Join(capturedArgs, " ")
	if strings.Contains(joined, "effort") || strings.Contains(joined, "high") {
		t.Errorf("cursor-cli must ignore request reasoning_effort, got: %s", joined)
	}
}

// TestQwenCliDriver_ReasoningEffort_RequestNoOp: same no-op contract for qwen.
func TestQwenCliDriver_ReasoningEffort_RequestNoOp(t *testing.T) {
	var capturedArgs []string
	d := NewQwenCliDriver(
		QwenWithCommandBuilder(func(ctx context.Context, name string, args ...string) *exec.Cmd {
			capturedArgs = args
			return qwenMockCmdBuilder("qwen_success")(ctx, name, args...)
		}),
	)
	if _, err := d.Call(context.Background(), LLMRequest{Intent: "hi", ReasoningEffort: "high"}); err != nil {
		t.Fatalf("Call: %v (no-op driver must not error on request effort)", err)
	}
	joined := strings.Join(capturedArgs, " ")
	if strings.Contains(joined, "effort") || strings.Contains(joined, "high") {
		t.Errorf("qwen-cli must ignore request reasoning_effort, got: %s", joined)
	}
}
