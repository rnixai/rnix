package llm

// ATDD coverage for Story 55.1: LLM Driver reasoning_effort 透传配置与 budget 迁移.
//
// Each driver gets two guardrails:
//   - passthrough: reasoning_effort != "" lands verbatim in the API params /
//     request body / CLI args (NO validation/mapping — open string passthrough).
//   - zero-regression: reasoning_effort == "" leaves the request untouched.
//
// anthropic & gemini additionally assert the budget path is RETAINED (防回归)
// and that effort takes priority without clobbering it for the no-effort path.

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os/exec"
	"strings"
	"testing"
)

// --- openai (official SDK) ---------------------------------------------------

func TestOpenAIDriver_ReasoningEffort_Passthrough(t *testing.T) {
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
		WithOpenAIReasoningEffort("high"),
	)
	if _, err := d.Call(context.Background(), LLMRequest{Intent: "hi"}); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if gotBody["reasoning_effort"] != "high" {
		t.Errorf("reasoning_effort = %v, want high", gotBody["reasoning_effort"])
	}
}

// TestOpenAIDriver_ReasoningEffort_ForwardCompat verifies a non-SDK-constant
// value (xhigh, gpt-5.1-codex-max+) passes through untouched — no whitelist.
func TestOpenAIDriver_ReasoningEffort_ForwardCompat(t *testing.T) {
	var gotBody map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		writeJSON(w, okCompletion)
	}))
	defer ts.Close()

	d := NewOpenAIDriver("test-openai",
		WithOpenAIModel("gpt-5.1-codex-max"),
		WithOpenAIBaseURL(ts.URL),
		WithOpenAIKey("sk-test"),
		WithOpenAIHTTPClient(ts.Client()),
		WithOpenAIReasoningEffort("xhigh"),
	)
	if _, err := d.Call(context.Background(), LLMRequest{Intent: "hi"}); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if gotBody["reasoning_effort"] != "xhigh" {
		t.Errorf("reasoning_effort = %v, want xhigh (forward-compat, no whitelist)", gotBody["reasoning_effort"])
	}
}

func TestOpenAIDriver_ReasoningEffort_Empty_NoRegression(t *testing.T) {
	var gotBody map[string]any
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &gotBody)
		writeJSON(w, okCompletion)
	}))
	defer ts.Close()

	d := NewOpenAIDriver("test-openai",
		WithOpenAIModel("gpt-4o"),
		WithOpenAIBaseURL(ts.URL),
		WithOpenAIKey("sk-test"),
		WithOpenAIHTTPClient(ts.Client()),
	)
	if _, err := d.Call(context.Background(), LLMRequest{Intent: "hi"}); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if _, present := gotBody["reasoning_effort"]; present {
		t.Errorf("reasoning_effort present in body, want omitted when unset")
	}
}

// --- anthropic (migration) ---------------------------------------------------

func TestAnthropicDriver_Effort_Passthrough(t *testing.T) {
	d := NewAnthropicDriver("test", WithAnthropicEffort("high"))
	params := d.buildParams(LLMRequest{Intent: "hi"}, nil)
	if string(params.OutputConfig.Effort) != "high" {
		t.Errorf("OutputConfig.Effort = %q, want high", params.OutputConfig.Effort)
	}
	// Effort path must NOT engage the thinking-budget path.
	if params.Thinking.GetBudgetTokens() != nil {
		t.Errorf("Thinking budget set under effort path, want nil")
	}
}

// TestAnthropicDriver_Effort_ForwardCompat: SDK constants cap at low/medium/high/
// max, but the open string type must still pass arbitrary future levels.
func TestAnthropicDriver_Effort_ForwardCompat(t *testing.T) {
	d := NewAnthropicDriver("test", WithAnthropicEffort("max"))
	params := d.buildParams(LLMRequest{Intent: "hi"}, nil)
	if string(params.OutputConfig.Effort) != "max" {
		t.Errorf("OutputConfig.Effort = %q, want max", params.OutputConfig.Effort)
	}
}

func TestAnthropicDriver_Effort_Empty_NoRegression(t *testing.T) {
	d := NewAnthropicDriver("test")
	params := d.buildParams(LLMRequest{Intent: "hi"}, nil)
	if params.OutputConfig.Effort != "" {
		t.Errorf("OutputConfig.Effort = %q, want empty when unset", params.OutputConfig.Effort)
	}
}

// TestAnthropicDriver_Effort_PriorityOverBudget verifies effort takes priority:
// when both are set, OutputConfig.Effort is used and the budget path is skipped.
func TestAnthropicDriver_Effort_PriorityOverBudget(t *testing.T) {
	d := NewAnthropicDriver("test",
		WithAnthropicThinkingBudget(8192),
		WithAnthropicEffort("high"),
	)
	params := d.buildParams(LLMRequest{Intent: "hi"}, nil)
	if string(params.OutputConfig.Effort) != "high" {
		t.Errorf("OutputConfig.Effort = %q, want high (priority)", params.OutputConfig.Effort)
	}
	if params.Thinking.GetBudgetTokens() != nil {
		t.Errorf("Thinking budget set despite effort priority, want nil")
	}
}

// TestAnthropicDriver_Budget_RetainedWithoutEffort locks 防回归: with no effort,
// the thinking-budget path (DeepSeek V4 Anthropic-compat, HTTP 400 防回归) is intact.
func TestAnthropicDriver_Budget_RetainedWithoutEffort(t *testing.T) {
	d := NewAnthropicDriver("test", WithAnthropicThinkingBudget(8192))
	params := d.buildParams(LLMRequest{Intent: "hi"}, nil)
	if params.Thinking.GetBudgetTokens() == nil || *params.Thinking.GetBudgetTokens() != 8192 {
		t.Errorf("Thinking budget not retained: %v", params.Thinking.GetBudgetTokens())
	}
	if params.OutputConfig.Effort != "" {
		t.Errorf("OutputConfig.Effort = %q, want empty", params.OutputConfig.Effort)
	}
}

// --- gemini (migration) ------------------------------------------------------

func TestGeminiDriver_ThinkingLevel_Passthrough(t *testing.T) {
	d := NewGeminiDriver("test", WithGeminiThinkingLevel("HIGH"))
	cfg := d.buildConfig(LLMRequest{Intent: "hi"}, nil)
	if cfg.ThinkingConfig == nil {
		t.Fatal("ThinkingConfig nil, want set")
	}
	if string(cfg.ThinkingConfig.ThinkingLevel) != "HIGH" {
		t.Errorf("ThinkingLevel = %q, want HIGH (uppercase passthrough)", cfg.ThinkingConfig.ThinkingLevel)
	}
	// Mutual exclusion: budget must NOT be sent alongside level (Gemini 3 rejects both).
	if cfg.ThinkingConfig.ThinkingBudget != nil {
		t.Errorf("ThinkingBudget set alongside level, want nil (mutually exclusive)")
	}
}

func TestGeminiDriver_ThinkingLevel_MutualExclusion_LevelWins(t *testing.T) {
	d := NewGeminiDriver("test",
		WithGeminiThinkingBudget(4096),
		WithGeminiThinkingLevel("LOW"),
	)
	cfg := d.buildConfig(LLMRequest{Intent: "hi"}, nil)
	if cfg.ThinkingConfig == nil {
		t.Fatal("ThinkingConfig nil, want set")
	}
	if string(cfg.ThinkingConfig.ThinkingLevel) != "LOW" {
		t.Errorf("ThinkingLevel = %q, want LOW (priority)", cfg.ThinkingConfig.ThinkingLevel)
	}
	if cfg.ThinkingConfig.ThinkingBudget != nil {
		t.Errorf("ThinkingBudget set despite level priority, want nil (mutually exclusive)")
	}
}

// TestGeminiDriver_Budget_RetainedWithoutLevel locks 防回归 for Gemini ≤2.5.
func TestGeminiDriver_Budget_RetainedWithoutLevel(t *testing.T) {
	d := NewGeminiDriver("test", WithGeminiThinkingBudget(4096))
	cfg := d.buildConfig(LLMRequest{Intent: "hi"}, nil)
	if cfg.ThinkingConfig == nil {
		t.Fatal("ThinkingConfig nil, want budget path set")
	}
	if cfg.ThinkingConfig.ThinkingBudget == nil || *cfg.ThinkingConfig.ThinkingBudget != 4096 {
		t.Errorf("ThinkingBudget not retained: %v", cfg.ThinkingConfig.ThinkingBudget)
	}
	if cfg.ThinkingConfig.ThinkingLevel != "" {
		t.Errorf("ThinkingLevel = %q, want empty", cfg.ThinkingConfig.ThinkingLevel)
	}
}

func TestGeminiDriver_NoThinking_Empty_NoRegression(t *testing.T) {
	d := NewGeminiDriver("test")
	cfg := d.buildConfig(LLMRequest{Intent: "hi"}, nil)
	if cfg.ThinkingConfig != nil {
		t.Errorf("ThinkingConfig = %+v, want nil when neither level nor budget set", cfg.ThinkingConfig)
	}
}

// --- claude-cli --------------------------------------------------------------

func TestClaudeCliDriver_Effort_Passthrough(t *testing.T) {
	var capturedArgs []string
	d := NewClaudeCliDriver(
		WithClaudeEffort("high"),
		WithCommandBuilder(func(ctx context.Context, name string, args ...string) *exec.Cmd {
			capturedArgs = args
			return mockCmdBuilder("success")(ctx, name, args...)
		}),
	)
	if _, err := d.Call(context.Background(), LLMRequest{Intent: "hi"}); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !argsContainPair(capturedArgs, "--effort", "high") {
		t.Errorf("expected --effort high, got: %s", strings.Join(capturedArgs, " "))
	}
}

func TestClaudeCliDriver_Effort_Empty_NoRegression(t *testing.T) {
	var capturedArgs []string
	d := NewClaudeCliDriver(
		WithCommandBuilder(func(ctx context.Context, name string, args ...string) *exec.Cmd {
			capturedArgs = args
			return mockCmdBuilder("success")(ctx, name, args...)
		}),
	)
	if _, err := d.Call(context.Background(), LLMRequest{Intent: "hi"}); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if strings.Contains(strings.Join(capturedArgs, " "), "--effort") {
		t.Errorf("unexpected --effort when unset, got: %s", strings.Join(capturedArgs, " "))
	}
}

// TestClaudeCliDriver_Effort_OrderBeforeExtraArgs verifies the AC #7 ordering
// guarantee: built-in args → --effort → extraArgs.
func TestClaudeCliDriver_Effort_OrderBeforeExtraArgs(t *testing.T) {
	var capturedArgs []string
	d := NewClaudeCliDriver(
		WithClaudeEffort("medium"),
		WithExtraArgs([]string{"--sentinel-extra"}),
		WithCommandBuilder(func(ctx context.Context, name string, args ...string) *exec.Cmd {
			capturedArgs = args
			return mockCmdBuilder("success")(ctx, name, args...)
		}),
	)
	if _, err := d.Call(context.Background(), LLMRequest{Intent: "hi"}); err != nil {
		t.Fatalf("Call: %v", err)
	}
	effortIdx := indexOf(capturedArgs, "--effort")
	extraIdx := indexOf(capturedArgs, "--sentinel-extra")
	modelIdx := indexOf(capturedArgs, "--model")
	if effortIdx < 0 || extraIdx < 0 || modelIdx < 0 {
		t.Fatalf("missing flags: %s", strings.Join(capturedArgs, " "))
	}
	// AC #7 ordering: built-in args (e.g. --model) → --effort → extraArgs.
	// Lock BOTH halves: effort must follow the built-ins and precede extraArgs.
	if modelIdx > effortIdx {
		t.Errorf("--effort (%d) must follow built-in args like --model (%d): %s", effortIdx, modelIdx, strings.Join(capturedArgs, " "))
	}
	if effortIdx > extraIdx {
		t.Errorf("--effort (%d) must precede extraArgs (%d): %s", effortIdx, extraIdx, strings.Join(capturedArgs, " "))
	}
}

// --- codex-cli ---------------------------------------------------------------

func TestCodexCliDriver_ReasoningEffort_Passthrough(t *testing.T) {
	var capturedArgs []string
	d := NewCodexCliDriver(
		CodexWithReasoningEffort("high"),
		CodexWithCommandBuilder(func(ctx context.Context, name string, args ...string) *exec.Cmd {
			capturedArgs = args
			return codexMockCmdBuilder("codex_call_success")(ctx, name, args...)
		}),
	)
	if _, err := d.Call(context.Background(), LLMRequest{Intent: "hi"}); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if !argsContainPair(capturedArgs, "-c", "model_reasoning_effort=high") {
		t.Errorf("expected -c model_reasoning_effort=high, got: %s", strings.Join(capturedArgs, " "))
	}
}

func TestCodexCliDriver_ReasoningEffort_Empty_NoRegression(t *testing.T) {
	var capturedArgs []string
	d := NewCodexCliDriver(
		CodexWithCommandBuilder(func(ctx context.Context, name string, args ...string) *exec.Cmd {
			capturedArgs = args
			return codexMockCmdBuilder("codex_call_success")(ctx, name, args...)
		}),
	)
	if _, err := d.Call(context.Background(), LLMRequest{Intent: "hi"}); err != nil {
		t.Fatalf("Call: %v", err)
	}
	if strings.Contains(strings.Join(capturedArgs, " "), "model_reasoning_effort") {
		t.Errorf("unexpected model_reasoning_effort when unset, got: %s", strings.Join(capturedArgs, " "))
	}
}

// TestCodexCliDriver_Effort_OrderBeforeExtraArgsAndPrompt verifies the effort
// flag precedes extraArgs AND that the prompt remains the trailing argument
// (`codex exec [OPTIONS] [PROMPT]`) — so the injected -c pair cannot displace
// the prompt from last position.
func TestCodexCliDriver_Effort_OrderBeforeExtraArgsAndPrompt(t *testing.T) {
	var capturedArgs []string
	d := NewCodexCliDriver(
		CodexWithReasoningEffort("high"),
		CodexWithExtraArgs([]string{"--sentinel-extra"}),
		CodexWithCommandBuilder(func(ctx context.Context, name string, args ...string) *exec.Cmd {
			capturedArgs = args
			return codexMockCmdBuilder("codex_call_success")(ctx, name, args...)
		}),
	)
	if _, err := d.Call(context.Background(), LLMRequest{Intent: "hi"}); err != nil {
		t.Fatalf("Call: %v", err)
	}
	effortIdx := indexOf(capturedArgs, "model_reasoning_effort=high")
	extraIdx := indexOf(capturedArgs, "--sentinel-extra")
	if effortIdx < 0 || extraIdx < 0 {
		t.Fatalf("missing flags: %s", strings.Join(capturedArgs, " "))
	}
	if effortIdx > extraIdx {
		t.Errorf("effort (%d) must precede extraArgs (%d): %s", effortIdx, extraIdx, strings.Join(capturedArgs, " "))
	}
	// Prompt must be the last argument; neither effort nor extraArgs may be last.
	last := len(capturedArgs) - 1
	if effortIdx == last || extraIdx == last {
		t.Errorf("prompt must be the trailing arg (effort/extraArgs must precede it): %s", strings.Join(capturedArgs, " "))
	}
}

// --- factory wiring ----------------------------------------------------------

func TestOpenAIFactory_ReasoningEffort(t *testing.T) {
	drv := mustCreateDriver(t, ProviderConfig{
		Name: "oai", Driver: DriverOpenAI, DefaultModel: "gpt-5.1", ReasoningEffort: "high",
	})
	d, ok := drv.(*OpenAIDriver)
	if !ok {
		t.Fatalf("got %T", drv)
	}
	if d.reasoningEffort != "high" {
		t.Errorf("reasoningEffort = %q, want high", d.reasoningEffort)
	}
}

// The compat factory-effort test has been removed: the openai driver
// (TestOpenAIFactory_ReasoningEffort above) now carries the same factory wiring,
// and the compat driver no longer exists (Story 75.3).

func TestAnthropicFactory_ReasoningEffort(t *testing.T) {
	drv := mustCreateDriver(t, ProviderConfig{
		Name: "anthropic", Driver: DriverAnthropic, ReasoningEffort: "max",
	})
	d, ok := drv.(*AnthropicDriver)
	if !ok {
		t.Fatalf("got %T", drv)
	}
	if d.effort != "max" {
		t.Errorf("effort = %q, want max", d.effort)
	}
}

func TestGeminiFactory_ReasoningEffort(t *testing.T) {
	drv := mustCreateDriver(t, ProviderConfig{
		Name: "gemini", Driver: DriverGemini, ReasoningEffort: "HIGH",
	})
	d, ok := drv.(*GeminiDriver)
	if !ok {
		t.Fatalf("got %T", drv)
	}
	if d.thinkingLevel != "HIGH" {
		t.Errorf("thinkingLevel = %q, want HIGH", d.thinkingLevel)
	}
}

func TestClaudeFactory_ReasoningEffort(t *testing.T) {
	drv := mustCreateDriver(t, ProviderConfig{
		Name: "claude", Driver: DriverClaudeCLI, ReasoningEffort: "high",
	})
	d, ok := drv.(*ClaudeCliDriver)
	if !ok {
		t.Fatalf("got %T", drv)
	}
	if d.effort != "high" {
		t.Errorf("effort = %q, want high", d.effort)
	}
}

func TestCodexFactory_ReasoningEffort(t *testing.T) {
	drv := mustCreateDriver(t, ProviderConfig{
		Name: "codex", Driver: DriverCodexCLI, ReasoningEffort: "high",
	})
	d, ok := drv.(*CodexCliDriver)
	if !ok {
		t.Fatalf("got %T", drv)
	}
	if d.effort != "high" {
		t.Errorf("effort = %q, want high", d.effort)
	}
}

// TestCursorFactory_ReasoningEffort_NoOpWithWarning: Cursor has no effort
// param — the factory must still create the driver and log a warning (no fake support).
func TestCursorFactory_ReasoningEffort_NoOpWithWarning(t *testing.T) {
	got := captureFactoryLog(t, ProviderConfig{
		Name: "cursor", Driver: DriverCursorCLI, ReasoningEffort: "high",
	})
	// Assert the driver-type token "cursor-cli" and the verb "ignored" — NOT the
	// bare provider name "cursor" (which the config sets, so it would echo even on
	// a wrong-driver/wrong-text log line and mask a broken warning).
	if !strings.Contains(got, "cursor-cli") || !strings.Contains(got, "ignored") || !strings.Contains(got, "reasoning_effort") {
		t.Errorf("expected cursor-cli reasoning_effort ignored warning, got: %q", got)
	}
}

// TestQwenFactory_ReasoningEffort_NoOpWithWarning: Qwen3-Coder has no effort
// concept — same no-op + warning contract.
func TestQwenFactory_ReasoningEffort_NoOpWithWarning(t *testing.T) {
	got := captureFactoryLog(t, ProviderConfig{
		Name: "qwen", Driver: DriverQwenCLI, ReasoningEffort: "high",
	})
	// Assert "qwen-cli" + "ignored" (not the bare provider name "qwen").
	if !strings.Contains(got, "qwen-cli") || !strings.Contains(got, "ignored") || !strings.Contains(got, "reasoning_effort") {
		t.Errorf("expected qwen-cli reasoning_effort ignored warning, got: %q", got)
	}
}

// --- helpers -----------------------------------------------------------------

func mustCreateDriver(t *testing.T, cfg ProviderConfig) LLMDriver {
	t.Helper()
	drv, err := CreateDriverWithEnv(cfg, func(string) string { return "" })
	if err != nil {
		t.Fatalf("CreateDriverWithEnv: %v", err)
	}
	return drv
}

// captureFactoryLog creates a driver and returns whatever the factory logged.
func captureFactoryLog(t *testing.T, cfg ProviderConfig) string {
	t.Helper()
	var buf bytes.Buffer
	prevOut := log.Writer()
	prevFlags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	t.Cleanup(func() {
		log.SetOutput(prevOut)
		log.SetFlags(prevFlags)
	})
	if _, err := CreateDriverWithEnv(cfg, func(string) string { return "" }); err != nil {
		t.Fatalf("CreateDriverWithEnv: %v", err)
	}
	return buf.String()
}

// argsContainPair reports whether args contains flag immediately followed by value.
func argsContainPair(args []string, flag, value string) bool {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == flag && args[i+1] == value {
			return true
		}
	}
	return false
}

func indexOf(args []string, target string) int {
	for i, a := range args {
		if a == target {
			return i
		}
	}
	return -1
}
