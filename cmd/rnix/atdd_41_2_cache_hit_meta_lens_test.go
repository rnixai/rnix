package main

// =============================================================================
// Story 41.2 AC#10: buildMetaLens Cache Hit Rate + Cached 分母修正集成测试
// =============================================================================
//
// 测试矩阵：
//   - Cache Hit 行位置（在 Cached 之后、Output 之前）
//   - Cache Hit 行公式（OpenAI / Anthropic driver 分支）
//   - 首步 dim + label（prefix 共享 / 冷启动）
//   - 无数据时不输出 Cache Hit 行（避免空行）
//   - Cached 行分母改为 InputTokens（文字尾标 "(/ 200k context)"）
//   - Tokens 区原 4 行顺序保留（27-4 fixture 兼容）

import (
	"strings"
	"testing"

	"github.com/rnixai/rnix/ipc"
)

func newMetaLensDetailFixture(step int, driverType string, in, cached, out int) *ipc.GetStepDetailResponse {
	return &ipc.GetStepDetailResponse{
		Step:              step,
		Action:            "tool_call",
		InputTokens:       in,
		CachedInputTokens: cached,
		OutputTokens:      out,
		TokenCount:        in + out,
		DriverType:        driverType,
		MessageCount:      3,
	}
}

func TestBuildMetaLens_CacheHitRow_OpenAICompat(t *testing.T) {
	m := newTestInspectorModelWithDetail()
	m.inspector.Detail = newMetaLensDetailFixture(2, "openai-compat", 14118, 3456, 239)
	content := m.buildMetaLens(m.inspector.Detail)
	stripped := stripANSIApprox(content)

	if !strings.Contains(stripped, "Cache Hit:") {
		t.Fatalf("missing 'Cache Hit:' line in:\n%s", stripped)
	}
	// 24.5% = 3456 / 14118
	if !strings.Contains(stripped, "24.5%") {
		t.Errorf("expected hit rate '24.5%%' in:\n%s", stripped)
	}
	if !strings.Contains(stripped, "of input") {
		t.Errorf("expected 'of input' suffix in:\n%s", stripped)
	}
}

func TestBuildMetaLens_CacheHitRow_Anthropic(t *testing.T) {
	m := newTestInspectorModelWithDetail()
	m.inspector.Detail = newMetaLensDetailFixture(2, "anthropic", 14118, 3456, 239)
	content := m.buildMetaLens(m.inspector.Detail)
	stripped := stripANSIApprox(content)

	if !strings.Contains(stripped, "Cache Hit:") {
		t.Fatalf("missing 'Cache Hit:' line:\n%s", stripped)
	}
	// Anthropic denom = input + cached = 17574 → 3456/17574 ≈ 19.7%
	if !strings.Contains(stripped, "19.7%") {
		t.Errorf("anthropic公式 expect '19.7%%' in:\n%s", stripped)
	}
	if !strings.Contains(stripped, "of (input + cached)") {
		t.Errorf("anthropic suffix mismatch in:\n%s", stripped)
	}
}

func TestBuildMetaLens_CacheHitRow_FirstStep_PrefixShared(t *testing.T) {
	m := newTestInspectorModelWithDetail()
	m.inspector.Detail = newMetaLensDetailFixture(1, "openai-compat", 14118, 3456, 239)
	content := m.buildMetaLens(m.inspector.Detail)
	stripped := stripANSIApprox(content)

	if !strings.Contains(stripped, "[first step · prefix shared]") {
		t.Errorf("first step warm hit rate should label '[first step · prefix shared]'; got:\n%s", stripped)
	}
}

func TestBuildMetaLens_CacheHitRow_FirstStep_ColdStart(t *testing.T) {
	m := newTestInspectorModelWithDetail()
	// hit rate = 0 / 14118 = 0% ≤ 5% threshold → cold start
	m.inspector.Detail = newMetaLensDetailFixture(1, "openai-compat", 14118, 0, 239)
	content := m.buildMetaLens(m.inspector.Detail)
	stripped := stripANSIApprox(content)

	if !strings.Contains(stripped, "[first step · cold start]") {
		t.Errorf("first step cold hit rate should label '[first step · cold start]'; got:\n%s", stripped)
	}
}

func TestBuildMetaLens_CacheHitRow_NoInput(t *testing.T) {
	m := newTestInspectorModelWithDetail()
	// 全空：Cache Hit 不该输出且不留空行
	m.inspector.Detail = &ipc.GetStepDetailResponse{
		Step:         2,
		Action:       "noop",
		MessageCount: 1,
	}
	content := m.buildMetaLens(m.inspector.Detail)
	stripped := stripANSIApprox(content)

	if strings.Contains(stripped, "Cache Hit:") {
		t.Errorf("no-input meta lens should not render Cache Hit line; got:\n%s", stripped)
	}
}

func TestBuildMetaLens_CachedRow_DenominatorIsInput(t *testing.T) {
	m := newTestInspectorModelWithDetail()
	m.inspector.Detail = newMetaLensDetailFixture(2, "openai-compat", 14118, 3456, 239)
	content := m.buildMetaLens(m.inspector.Detail)
	stripped := stripANSIApprox(content)

	if !strings.Contains(stripped, "(/ 200,000 context)") {
		t.Errorf("Cached 行 should carry capacity-view suffix '(/ 200,000 context)'; got:\n%s", stripped)
	}
	// Cached 行用 RenderRateLine: "Cached: 24.5%  (3.5k / 14.1k of input  (/ 200,000 context))"
	// 这里 Cached 用 RenderRateLine 公式（cached / input）= 24.5%
	if !strings.Contains(stripped, "Cached:") {
		t.Fatalf("Cached 行 missing in:\n%s", stripped)
	}
}

func TestBuildMetaLens_TokensRowSequence_Preserved(t *testing.T) {
	m := newTestInspectorModelWithDetail()
	m.inspector.Detail = newMetaLensDetailFixture(2, "openai-compat", 14118, 3456, 239)
	content := m.buildMetaLens(m.inspector.Detail)
	stripped := stripANSIApprox(content)

	// 期望顺序：Input → Cached → Cache Hit → Output → Total
	inputIdx := strings.Index(stripped, "Input:")
	cachedIdx := strings.Index(stripped, "Cached:")
	hitIdx := strings.Index(stripped, "Cache Hit:")
	outputIdx := strings.Index(stripped, "Output:")
	totalIdx := strings.Index(stripped, "Total:")

	if inputIdx < 0 || cachedIdx < 0 || hitIdx < 0 || outputIdx < 0 || totalIdx < 0 {
		t.Fatalf("missing one of Input/Cached/Cache Hit/Output/Total in:\n%s", stripped)
	}
	if inputIdx >= cachedIdx || cachedIdx >= hitIdx || hitIdx >= outputIdx || outputIdx >= totalIdx {
		t.Errorf("Tokens row sequence broken: Input=%d Cached=%d CacheHit=%d Output=%d Total=%d\n%s",
			inputIdx, cachedIdx, hitIdx, outputIdx, totalIdx, stripped)
	}
}
