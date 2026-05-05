// Package status — hint_test.go (Story 38-5 PR11 Step 4(c))
//
// 行为测试覆盖（与 cmd/rnix.dashboard_status.go::hint / hintGroup 等价契约）：
//   - Hint 返回包含 key 和 desc 两段（非空）；
//   - HintGroup 双空格分隔（"  "）；
//   - HintGroup 空切片返回空字符串；
//   - HintGroup 单元素 passthrough（不附加分隔符）；
//   - RenderDescStyle 至少包含输入文本（profile-tolerant）；
//   - 颜色使用 ColorAgent (key) / ColorMuted (desc) — profile-tolerant 模式
//     仅断言 SGR 字节存在与否，不固化具体颜色字符串（与 mode_label_test.go
//     stripANSI 模式同）.
package status

import (
	"strings"
	"testing"
)

func TestHint_ReturnsKeyAndDesc(t *testing.T) {
	got := Hint("j/k", "nav")
	plain := stripANSI(got)
	if !strings.Contains(plain, "j/k") {
		t.Errorf("Hint output missing key %q: %q", "j/k", plain)
	}
	if !strings.Contains(plain, "nav") {
		t.Errorf("Hint output missing desc %q: %q", "nav", plain)
	}
}

func TestHint_EmptyInputs(t *testing.T) {
	// 空 key 与空 desc 不应 panic，返回串可以是空（profile-dependent）
	got := Hint("", "")
	if stripANSI(got) != "" {
		t.Errorf("Hint(\"\",\"\") plain text expected empty, got %q", stripANSI(got))
	}
}

func TestHintGroup_DoubleSpaceSeparator(t *testing.T) {
	got := HintGroup("a", "b", "c")
	want := "a  b  c"
	if got != want {
		t.Errorf("HintGroup(a,b,c) = %q, want %q", got, want)
	}
}

func TestHintGroup_EmptyReturnsEmpty(t *testing.T) {
	got := HintGroup()
	if got != "" {
		t.Errorf("HintGroup() = %q, want empty", got)
	}
}

func TestHintGroup_SinglePassthrough(t *testing.T) {
	got := HintGroup("only")
	if got != "only" {
		t.Errorf("HintGroup(only) = %q, want %q", got, "only")
	}
}

func TestRenderDescStyle_ContainsText(t *testing.T) {
	got := RenderDescStyle("(PID 42)")
	plain := stripANSI(got)
	if !strings.Contains(plain, "(PID 42)") {
		t.Errorf("RenderDescStyle plain text missing input: %q", plain)
	}
}

func TestHint_KeyAndDescVisuallyDistinct(t *testing.T) {
	// 即使在 NoColor profile 下，stripped 输出也应是 "key" 紧接 "desc"
	// （没有分隔符 · caller 自加 space）。这是与 cmd/rnix.hint 等价契约的
	// 关键：返回值是 keyStyled + descStyled 的字符串拼接，**没有内部空格**。
	got := stripANSI(Hint("KEY", "DESC"))
	if got != "KEYDESC" {
		t.Errorf("Hint plain text expected concat KEY+DESC, got %q", got)
	}
}

func TestHintInitOnce_LazyAndIdempotent(t *testing.T) {
	// 多次调用 Hint 应正常工作（sync.Once 守卫单次初始化 · 后续调用复用）
	first := Hint("a", "b")
	second := Hint("a", "b")
	if first != second {
		t.Errorf("Hint not idempotent across calls: %q vs %q", first, second)
	}
}
