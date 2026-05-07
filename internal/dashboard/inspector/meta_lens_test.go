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
