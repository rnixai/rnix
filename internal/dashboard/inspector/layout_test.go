// Tests for ContentHeight (Story 38-5 PR11 Step 4(c)).
//
// 行为契约覆盖（与 cmd/rnix.inspectorContentHeight + ATDD 27-4 等价）：
//   1. termHeight >= 20 → termHeight - 6 (含 thumbnail bar 2 行)
//   2. termHeight < 20 → termHeight - 4 (不含 thumbnail bar)
//   3. termHeight == 20 边界（恰满足 ≥20 路径）
//   4. 极小 termHeight → max clamp 至 1（不返回 0 或负数）
//   5. 高 terminal 大值 → 大约线性减 6

package inspector

import "testing"

func TestContentHeight_TallTerminal(t *testing.T) {
	got := ContentHeight(40)
	want := 34 // 40 - 6 (含 thumbnail bar)
	if got != want {
		t.Errorf("ContentHeight(40): want %d, got %d", want, got)
	}
}

func TestContentHeight_ShortTerminal(t *testing.T) {
	got := ContentHeight(18)
	want := 14 // 18 - 4 (不含 thumbnail bar)
	if got != want {
		t.Errorf("ContentHeight(18): want %d, got %d", want, got)
	}
}

func TestContentHeight_BoundaryAt20(t *testing.T) {
	// 20 应进入 ≥20 路径 → 14
	got := ContentHeight(20)
	want := 14
	if got != want {
		t.Errorf("ContentHeight(20) boundary: want %d, got %d", want, got)
	}
}

func TestContentHeight_Boundary19(t *testing.T) {
	// 19 应进入 <20 路径 → 15
	got := ContentHeight(19)
	want := 15
	if got != want {
		t.Errorf("ContentHeight(19) boundary: want %d, got %d", want, got)
	}
}

func TestContentHeight_TooSmallClampedToOne(t *testing.T) {
	// termHeight=0 → max(0-4, 1) = 1
	if got := ContentHeight(0); got != 1 {
		t.Errorf("ContentHeight(0): want 1 (max clamp), got %d", got)
	}
	// termHeight=1 → max(1-4, 1) = 1
	if got := ContentHeight(1); got != 1 {
		t.Errorf("ContentHeight(1): want 1 (max clamp), got %d", got)
	}
	// termHeight=4 → max(4-4, 1) = 1
	if got := ContentHeight(4); got != 1 {
		t.Errorf("ContentHeight(4): want 1 (max clamp), got %d", got)
	}
}

func TestContentHeight_VeryTallTerminal(t *testing.T) {
	got := ContentHeight(120)
	want := 114
	if got != want {
		t.Errorf("ContentHeight(120): want %d, got %d", want, got)
	}
}

func TestContentHeight_NegativeClampedToOne(t *testing.T) {
	// 防御：负值不应 panic 且至少返回 1
	if got := ContentHeight(-5); got != 1 {
		t.Errorf("ContentHeight(-5): want 1 (max clamp), got %d", got)
	}
}
