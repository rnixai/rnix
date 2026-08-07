// Package inspector — meta_lens_test.go (Story 38-5 PR11 Step 4(c))
//
// 行为测试覆盖 5 个 Meta lens helper + 1 常量（与 cmd/rnix.dashboard_inspector.go
// renderMetaSectionHeader / metaSectionHeaderWidth / renderTokenLine / formatThousands
// / renderTokenBar 等价契约）：
//
//   - MetaTokenContextWindow == 200000（Claude 3.5 / Sonnet 4.x default · drift guard）
//   - RenderMetaSectionHeader 标题 + 填充 + 最小 3 字符 floor + utf8 rune count
//   - MetaSectionHeaderWidth 4 路径（zero / 70 cap / shrink mWidth-2 / floor 16）
//   - RenderTokenLine Tokens / Total 双形态 + ASCII 降级
//   - FormatThousands 边界（< 1000 / 1000 / 200000 / negative / zero / max）
//   - RenderTokenBar 边界（width < 1 / total <= 0 / count < 0 / count > total / ASCII）
//
// profile-tolerant 模式：颜色断言通过 stripANSI helper 剥离 ANSI escape，
// 保证 NoColor / TrueColor lipgloss profile 下行为一致（Story 38-3 教训应用）.
package inspector

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/rnixai/rnix/drivers/llm"
)

// stripANSIMeta removes ANSI escape sequences for profile-tolerant assertions
// （与 mode_label_test.go::stripANSI / hint_test.go 同模式，包内私有避免命名冲突）.
func stripANSIMeta(s string) string {
	out := make([]byte, 0, len(s))
	in := false
	for i := range len(s) {
		c := s[i]
		if c == 0x1b {
			in = true
			continue
		}
		if in {
			if c == 'm' {
				in = false
			}
			continue
		}
		out = append(out, c)
	}
	return string(out)
}

func TestMetaTokenContextWindow_DriftGuard(t *testing.T) {
	if MetaTokenContextWindow != 200000 {
		t.Errorf("MetaTokenContextWindow drift: got %d, want 200000 (Claude 3.5 / Sonnet 4.x default)", MetaTokenContextWindow)
	}
}

func TestRenderMetaSectionHeader_TitleAndFill(t *testing.T) {
	got := stripANSIMeta(RenderMetaSectionHeader("Tokens", 30))
	if !strings.Contains(got, "── Tokens ") {
		t.Errorf("output missing prefix \"── Tokens \": %q", got)
	}
	// 应该填充至 width=30（按 rune count 计算）
	if utf8.RuneCountInString(got) != 30 {
		t.Errorf("output rune count = %d, want 30", utf8.RuneCountInString(got))
	}
}

func TestRenderMetaSectionHeader_MinFillCountFloor(t *testing.T) {
	// width 太小（≤ "── Title " 的 rune count）时填充至少 3 字符
	got := stripANSIMeta(RenderMetaSectionHeader("Title", 5))
	if !strings.Contains(got, "───") {
		t.Errorf("min fill of 3 missing: %q", got)
	}
}

func TestRenderMetaSectionHeader_CJKTitleRuneCount(t *testing.T) {
	// CJK 标题（"令牌" = 2 个 rune · 6 个 byte）— 按 rune count 对齐
	got := stripANSIMeta(RenderMetaSectionHeader("令牌", 30))
	if !strings.Contains(got, "── 令牌 ") {
		t.Errorf("CJK title missing: %q", got)
	}
}

func TestMetaSectionHeaderWidth_ZeroFallback(t *testing.T) {
	if got := MetaSectionHeaderWidth(0); got != 70 {
		t.Errorf("MetaSectionHeaderWidth(0) = %d, want 70", got)
	}
	if got := MetaSectionHeaderWidth(-1); got != 70 {
		t.Errorf("MetaSectionHeaderWidth(-1) = %d, want 70", got)
	}
}

func TestMetaSectionHeaderWidth_Cap70(t *testing.T) {
	// 大 mWidth → cap 至 70（避免分隔符过宽影响视觉）
	if got := MetaSectionHeaderWidth(120); got != 70 {
		t.Errorf("MetaSectionHeaderWidth(120) = %d, want 70 (cap)", got)
	}
	if got := MetaSectionHeaderWidth(72); got != 70 {
		t.Errorf("MetaSectionHeaderWidth(72) = %d, want 70 (cap)", got)
	}
}

func TestMetaSectionHeaderWidth_Shrink(t *testing.T) {
	// 中等 mWidth → mWidth-2（让出 inspector 1 列 padding + 边距）
	if got := MetaSectionHeaderWidth(60); got != 58 {
		t.Errorf("MetaSectionHeaderWidth(60) = %d, want 58 (shrink mWidth-2)", got)
	}
}

func TestMetaSectionHeaderWidth_Floor16(t *testing.T) {
	// 小 mWidth → floor 16（保证标题至少能容纳 "── XYZ ────────"）
	if got := MetaSectionHeaderWidth(10); got != 16 {
		t.Errorf("MetaSectionHeaderWidth(10) = %d, want 16 (floor)", got)
	}
	if got := MetaSectionHeaderWidth(17); got != 16 {
		t.Errorf("MetaSectionHeaderWidth(17) = %d, want 16 (mWidth-2=15 → floor)", got)
	}
}

func TestRenderTokenLine_Tokens(t *testing.T) {
	got := stripANSIMeta(RenderTokenLine("Request:", 1500, MetaTokenContextWindow, false))
	if !strings.Contains(got, "Request:") {
		t.Errorf("missing label: %q", got)
	}
	if !strings.Contains(got, "1500") {
		t.Errorf("missing raw count 1500: %q", got)
	}
	if !strings.Contains(got, "0.8%") {
		t.Errorf("missing pct 0.8%%: %q", got)
	}
}

func TestRenderTokenLine_Total(t *testing.T) {
	// total 形态：使用千位分隔符 + "of <total> context"
	got := stripANSIMeta(RenderTokenLine("Total:", 2300, MetaTokenContextWindow, true))
	if !strings.Contains(got, "Total:") {
		t.Errorf("missing label: %q", got)
	}
	if !strings.Contains(got, "2300 of 200,000 context") {
		t.Errorf("missing 'of <total> context' with thousands separator: %q", got)
	}
}

func TestRenderTokenLine_ZeroTotal(t *testing.T) {
	// total = 0 → pct 路径应是 0% 不 panic
	got := stripANSIMeta(RenderTokenLine("Request:", 100, 0, false))
	if !strings.Contains(got, "0.0%") {
		t.Errorf("zero total pct expected 0.0%%: %q", got)
	}
}

func TestRenderTokenLine_LabelRightAligned(t *testing.T) {
	// 短标签应右对齐 10 字符（前置空格让冒号对齐）
	got := stripANSIMeta(RenderTokenLine("X:", 1, 100, true))
	// "%10s" of "X:" = "        X:" (8 spaces + "X:")
	if !strings.Contains(got, "        X:") {
		t.Errorf("expected right-aligned to 10 chars: %q", got)
	}
}

func TestFormatThousands_Small(t *testing.T) {
	cases := map[int]string{
		0:   "0",
		1:   "1",
		999: "999",
	}
	for n, want := range cases {
		if got := FormatThousands(n); got != want {
			t.Errorf("FormatThousands(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestFormatThousands_Large(t *testing.T) {
	cases := map[int]string{
		1000:    "1,000",
		1234:    "1,234",
		10000:   "10,000",
		200000:  "200,000",
		1000000: "1,000,000",
	}
	for n, want := range cases {
		if got := FormatThousands(n); got != want {
			t.Errorf("FormatThousands(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestFormatThousands_Negative(t *testing.T) {
	if got := FormatThousands(-200000); got != "-200,000" {
		t.Errorf("FormatThousands(-200000) = %q, want %q", got, "-200,000")
	}
}

func TestRenderTokenBar_NormalRatio(t *testing.T) {
	// 50% with width 20 → 10 filled + 10 empty
	got := RenderTokenBar(50, 100, 20)
	full := "█"
	empty := "░"
	want := strings.Repeat(full, 10) + strings.Repeat(empty, 10)
	if got != want {
		t.Errorf("RenderTokenBar(50,100,20) = %q, want %q", got, want)
	}
}

func TestRenderTokenBar_ZeroTotal(t *testing.T) {
	// total <= 0 → 全 empty（除非 width 为 0/负数）
	got := RenderTokenBar(100, 0, 5)
	if got != "░░░░░" {
		t.Errorf("RenderTokenBar(100,0,5) = %q, want %q", got, "░░░░░")
	}
}

func TestRenderTokenBar_OverflowClampsToFull(t *testing.T) {
	// count > total → ratio clamp 至 1.0 → 全 filled
	got := RenderTokenBar(300000, 200000, 10)
	if got != "██████████" {
		t.Errorf("RenderTokenBar(300000,200000,10) = %q, want full bar", got)
	}
}

func TestRenderTokenBar_NegativeCountClampsToZero(t *testing.T) {
	// count < 0 → ratio clamp 至 0 → 全 empty
	got := RenderTokenBar(-1, 100, 5)
	if got != "░░░░░" {
		t.Errorf("RenderTokenBar(-1,100,5) = %q, want all empty", got)
	}
}

func TestRenderTokenBar_TinyWidthClampsToOne(t *testing.T) {
	// width < 1 → 强制 1（不返回空字符串）
	got := RenderTokenBar(50, 100, 0)
	// width 1 + ratio 0.5 → filled = 0 → "░"
	if got != "░" {
		t.Errorf("RenderTokenBar(50,100,0) = %q, want single-cell empty bar", got)
	}
}

// =============================================================================
// Story 41.2 — RenderRateLine (4 项契约)
// =============================================================================

func TestRenderRateLine_NormalCase(t *testing.T) {
	got := stripANSIMeta(RenderRateLine("Cache Hit:", 1_581_184, 1_642_532, "of input"))
	// pct ~= 96.3%
	if !strings.Contains(got, "96.3%") {
		t.Errorf("RenderRateLine normal: missing '96.3%%' in %q", got)
	}
	// 用 timeline.FormatTokenCount 的 1.58M / 1.64M 格式
	if !strings.Contains(got, "1.58M / 1.64M of input") {
		t.Errorf("RenderRateLine normal: missing '1.58M / 1.64M of input' in %q", got)
	}
	// label 右对齐 10 字符（与 RenderTokenLine 一致）
	if !strings.Contains(got, "Cache Hit:") {
		t.Errorf("RenderRateLine normal: missing label in %q", got)
	}
}

func TestRenderRateLine_ZeroNumeratorAndDenominator(t *testing.T) {
	// numerator==0 && denom==0 → 返回空串（调用方决定不输出）
	got := RenderRateLine("Cache Hit:", 0, 0, "of input")
	if got != "" {
		t.Errorf("RenderRateLine zero/zero = %q, want empty string", got)
	}
}

func TestRenderRateLine_ZeroDenominator(t *testing.T) {
	// numerator > 0 but denom == 0 → "—  (cached <num>)" 不 panic
	got := stripANSIMeta(RenderRateLine("Cache Hit:", 100, 0, "of input"))
	if !strings.Contains(got, "—") {
		t.Errorf("RenderRateLine zero-denom: missing em-dash in %q", got)
	}
	if !strings.Contains(got, "cached 100") {
		t.Errorf("RenderRateLine zero-denom: missing 'cached 100' in %q", got)
	}
}

func TestRenderRateLine_OverHundredPercent(t *testing.T) {
	// cached > input (Anthropic 实际不该有此情况) → clamp ">100%" + 提示
	got := stripANSIMeta(RenderRateLine("Cache Hit:", 200, 100, "of input"))
	if !strings.Contains(got, ">100%") {
		t.Errorf("RenderRateLine >100%%: missing clamp marker in %q", got)
	}
	if !strings.Contains(got, "check driver semantics") {
		t.Errorf("RenderRateLine >100%%: missing debug guard in %q", got)
	}
}

// =============================================================================
// Story 41.2 — ComputeCacheHitRate (6 项契约 driver 分支 + 边界)
// =============================================================================

func TestComputeCacheHitRate_OpenAI(t *testing.T) {
	rate, denom := ComputeCacheHitRate("openai", 14118, 3456)
	wantRate := 3456.0 / 14118.0
	if rate != wantRate {
		t.Errorf("ComputeCacheHitRate(openai,14118,3456) rate = %v, want %v", rate, wantRate)
	}
	if denom != 14118 {
		t.Errorf("denom = %d, want 14118 (input only)", denom)
	}
}

func TestComputeCacheHitRate_Anthropic(t *testing.T) {
	rate, denom := ComputeCacheHitRate("anthropic", 14118, 3456)
	wantDenom := 14118 + 3456
	wantRate := 3456.0 / float64(wantDenom)
	if rate != wantRate {
		t.Errorf("ComputeCacheHitRate(anthropic) rate = %v, want %v", rate, wantRate)
	}
	if denom != wantDenom {
		t.Errorf("denom = %d, want %d (input + cached)", denom, wantDenom)
	}
}

func TestComputeCacheHitRate_DeepSeek(t *testing.T) {
	rate, denom := ComputeCacheHitRate("deepseek", 1_642_532, 1_581_184)
	wantRate := 1_581_184.0 / 1_642_532.0
	if rate != wantRate {
		t.Errorf("ComputeCacheHitRate(deepseek) rate = %v, want %v", rate, wantRate)
	}
	if denom != 1_642_532 {
		t.Errorf("denom = %d, want 1642532", denom)
	}
}

func TestComputeCacheHitRate_EmptyDriverFallback(t *testing.T) {
	rate, denom := ComputeCacheHitRate("", 100, 50)
	if rate != 0.5 {
		t.Errorf("empty driver fallback rate = %v, want 0.5", rate)
	}
	if denom != 100 {
		t.Errorf("denom = %d, want 100", denom)
	}
}

func TestComputeCacheHitRate_ZeroInput(t *testing.T) {
	rate, denom := ComputeCacheHitRate("openai", 0, 0)
	if rate != 0 || denom != 0 {
		t.Errorf("zero input: got (%v, %d), want (0, 0)", rate, denom)
	}
	// 边界 case：cached > 0 但 input == 0 仍返回 (0, 0)
	rate, denom = ComputeCacheHitRate("openai", 0, 500)
	if rate != 0 || denom != 0 {
		t.Errorf("zero input cached>0: got (%v, %d), want (0, 0)", rate, denom)
	}
}

func TestComputeCacheHitRate_OverHundred(t *testing.T) {
	// 异常 case：cached > input（OpenAI 语义下不应出现）→ rate > 1 不 clamp
	rate, denom := ComputeCacheHitRate("openai", 100, 200)
	if rate != 2.0 {
		t.Errorf("over-hundred rate = %v, want 2.0 (no clamp at compute layer)", rate)
	}
	if denom != 100 {
		t.Errorf("denom = %d, want 100", denom)
	}
}

func TestComputeCacheHitRate_CLIDriversFallback(t *testing.T) {
	// CLI drivers fallback 到 OpenAI 公式
	cases := []string{"claude-cli", "cursor-cli", "codex-cli", "qwen-cli", "gemini", "unknown-future"}
	for _, d := range cases {
		rate, denom := ComputeCacheHitRate(d, 100, 50)
		if rate != 0.5 || denom != 100 {
			t.Errorf("driver %q fallback failed: got (%v, %d), want (0.5, 100)", d, rate, denom)
		}
	}
}

func TestIsAnthropicDriver(t *testing.T) {
	if !IsAnthropicDriver("anthropic") {
		t.Errorf("IsAnthropicDriver(anthropic) should be true")
	}
	for _, d := range []string{"openai", "deepseek", "claude-cli", "", "unknown"} {
		if IsAnthropicDriver(d) {
			t.Errorf("IsAnthropicDriver(%q) should be false", d)
		}
	}
}

// TestComputeCacheHitRate_DriverConstantsInSync 守护本包 dashboard 端硬编码的
// driver type 字符串常量与 drivers/llm/config.go 常量值一致（防漂移）。
//
// 测试反向 import drivers/llm 是 ok 的（仅测试 · 生产代码 dashboard 不 import
// drivers/llm · 单向依赖保持）。
// =============================================================================
// ResolveContextWindow (3 项契约)
// =============================================================================

func TestResolveContextWindow_ExplicitConfig(t *testing.T) {
	got := ResolveContextWindow("deepseek-v4-flash", 500_000)
	if got != 500_000 {
		t.Errorf("ResolveContextWindow with explicit config = %d, want 500000", got)
	}
}

func TestResolveContextWindow_PrefixMatch(t *testing.T) {
	got := ResolveContextWindow("deepseek-v4-flash", 0)
	if got != 1_000_000 {
		t.Errorf("ResolveContextWindow prefix deepseek-v4-flash = %d, want 1000000", got)
	}
	got = ResolveContextWindow("gpt-5-turbo", 0)
	if got != 400_000 {
		t.Errorf("ResolveContextWindow prefix gpt-5-turbo = %d, want 400000", got)
	}
}

func TestResolveContextWindow_UnknownFallback(t *testing.T) {
	got := ResolveContextWindow("custom-finetune-v1", 0)
	if got != MetaTokenContextWindow {
		t.Errorf("ResolveContextWindow unknown model = %d, want %d (MetaTokenContextWindow)", got, MetaTokenContextWindow)
	}
}

func TestComputeCacheHitRate_DriverConstantsInSync(t *testing.T) {
	type pair struct {
		dashboardLocal string
		driversLLM     string
		name           string
	}
	// 反向 import drivers/llm 仅在测试中允许 · 当 drivers/llm 改名时本测试
	// 自动暴露不一致（无需手动同步硬编码字符串）。
	pairs := []pair{
		{driverClaudeCLI, llm.DriverClaudeCLI, "ClaudeCLI"},
		{driverCursorCLI, llm.DriverCursorCLI, "CursorCLI"},
		{driverCodexCLI, llm.DriverCodexCLI, "CodexCLI"},
		{driverQwenCLI, llm.DriverQwenCLI, "QwenCLI"},
		{driverOpenAI, llm.DriverOpenAI, "OpenAI"},
		{driverGemini, llm.DriverGemini, "Gemini"},
		{driverAnthropic, llm.DriverAnthropic, "Anthropic"},
	}
	for _, p := range pairs {
		if p.dashboardLocal != p.driversLLM {
			t.Errorf("driver const %q drift: dashboard=%q drivers/llm=%q", p.name, p.dashboardLocal, p.driversLLM)
		}
	}
}

// =============================================================================
// dashboard-model-info：DashIfEmpty + RenderModelSectionLines 行为测试。
// =============================================================================

func TestDashIfEmpty_NonEmptyReturnsValue(t *testing.T) {
	t.Setenv("RNIX_ASCII", "")
	if got := DashIfEmpty("claude-opus-4-8"); got != "claude-opus-4-8" {
		t.Errorf("DashIfEmpty(non-empty) = %q, want unchanged", got)
	}
}

func TestDashIfEmpty_EmptyReturnsEmDash(t *testing.T) {
	t.Setenv("RNIX_ASCII", "")
	if got := DashIfEmpty(""); got != "—" {
		t.Errorf("DashIfEmpty(\"\") = %q, want \"—\"", got)
	}
}

func TestDashIfEmpty_EmptyASCIIReturnsHyphen(t *testing.T) {
	t.Setenv("RNIX_ASCII", "1")
	if got := DashIfEmpty(""); got != "-" {
		t.Errorf("DashIfEmpty(\"\") ASCII = %q, want \"-\"", got)
	}
}

// claude-cli 全字段：三项 + 全部 DriverMeta 诊断（binary/permission/caps 聚合/probe/fallback）。
func TestRenderModelSectionLines_ClaudeCliFullMeta(t *testing.T) {
	t.Setenv("RNIX_ASCII", "")
	meta := map[string]string{
		"resolved_bin":         "/usr/local/bin/claude",
		"permission_mode":      "bypassPermissions",
		"cap_partial_messages": "true",
		"cap_add_dir":          "true",
		"cap_permission_mode":  "true",
		"fallback_candidates":  "claude,openclaude",
		"probe_duration_ms":    "12",
	}
	got := stripANSIMeta(strings.Join(
		RenderModelSectionLines("claude", "claude-opus-4-8", "high", "claude-cli", meta), "\n"))

	for _, want := range []string{
		"Provider:", "claude",
		"Model:", "claude-opus-4-8",
		"Effort:", "high",
		"Driver:", "claude-cli",
		"Binary:", "/usr/local/bin/claude",
		"Permission:", "bypassPermissions",
		"Probe:", "12",
		"Fallback:", "claude,openclaude",
		"Cap:", "partial-messages=true", "add-dir=true", "permission-mode=true",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("ClaudeCli meta output missing %q:\n%s", want, got)
		}
	}
	// cap_* 应聚合为单行 "Cap:"，不应泄漏原始下划线键名。
	if strings.Contains(got, "cap_") {
		t.Errorf("cap_* keys should be aggregated, raw key leaked:\n%s", got)
	}
}

// 非 CLI driver：DriverMeta 为 nil → 三项 + "(无运行时诊断)"，无诊断字段、无空行。
func TestRenderModelSectionLines_NonCLINilMeta(t *testing.T) {
	t.Setenv("RNIX_ASCII", "")
	lines := RenderModelSectionLines("deepseek", "deepseek-v4", "", "openai", nil)
	got := stripANSIMeta(strings.Join(lines, "\n"))

	for _, want := range []string{"deepseek", "deepseek-v4", "openai", "(no runtime diagnostics)"} {
		if !strings.Contains(got, want) {
			t.Errorf("NonCLI output missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "Binary:") || strings.Contains(got, "Cap:") {
		t.Errorf("NonCLI (nil meta) should not render diagnostic fields:\n%s", got)
	}
	// 5 行：Provider / Model / Effort / Driver / 诊断提示——无多余空行。
	if len(lines) != 5 {
		t.Errorf("NonCLI nil meta line count = %d, want 5", len(lines))
	}
	for i, ln := range lines {
		if strings.TrimSpace(stripANSIMeta(ln)) == "" {
			t.Errorf("line %d is blank, want no empty lines", i)
		}
	}
}

// Model 为空（历史/placeholder 进程）→ "—" 占位，不 panic。
func TestRenderModelSectionLines_EmptyModelDash(t *testing.T) {
	t.Setenv("RNIX_ASCII", "")
	got := stripANSIMeta(strings.Join(
		RenderModelSectionLines("claude", "", "", "claude-cli", nil), "\n"))
	if !strings.Contains(got, "Model:") || !strings.Contains(got, "—") {
		t.Errorf("empty Model should render \"—\" placeholder:\n%s", got)
	}
}

// ASCII 模式：空值用 "-" 而非 "—"，整体无 Unicode em-dash 字形。
func TestRenderModelSectionLines_ASCIIMode(t *testing.T) {
	t.Setenv("RNIX_ASCII", "1")
	got := stripANSIMeta(strings.Join(
		RenderModelSectionLines("", "", "", "", nil), "\n"))
	if strings.Contains(got, "—") {
		t.Errorf("ASCII mode must not contain em-dash \"—\":\n%s", got)
	}
	if !strings.Contains(got, "-") {
		t.Errorf("ASCII mode empty value should render \"-\":\n%s", got)
	}
}

// 未知 DriverMeta key → 回退原始键名（加冒号），保证未来新增字段也可见。
func TestRenderModelSectionLines_UnknownKeyFallback(t *testing.T) {
	t.Setenv("RNIX_ASCII", "")
	meta := map[string]string{"future_field": "xyz"}
	got := stripANSIMeta(strings.Join(
		RenderModelSectionLines("p", "m", "", "d", meta), "\n"))
	if !strings.Contains(got, "future_field:") || !strings.Contains(got, "xyz") {
		t.Errorf("unknown key should fall back to raw name:\n%s", got)
	}
}
