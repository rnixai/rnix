// Package detail — keys_test.go (Story 38-5 PR5 Step 2)
//
// KeyLayer / SelectedPIDProvider 行为测试。验证：
//
//  1. KeyLayer 返回的 Bindings/Docs 集合与 38-1 落地完全一致（detail pane 注册体为空）
//  2. ActiveModesFn 在 selectedPID > 0 / == 0 / 大值边界下返回正确 Mode
//  3. nil 安全：ctx 不实现 SelectedPIDProvider 时返回 nil（不 panic）
//  4. fallback nil 安全
package detail

import (
	"strings"
	"testing"

	"github.com/rnixai/rnix/internal/types"
)

// fakeSelectedPIDProvider 实现 SelectedPIDProvider interface，用于 ActiveModesFn 测试。
type fakeSelectedPIDProvider struct {
	pid types.PID
}

func (f fakeSelectedPIDProvider) SelectedPID() types.PID { return f.pid }

// 编译期断言：fakeSelectedPIDProvider 满足 SelectedPIDProvider interface。
var _ SelectedPIDProvider = (*fakeSelectedPIDProvider)(nil)

func TestKeyLayer_NameAndEmptyContract(t *testing.T) {
	t.Parallel()
	l := KeyLayer(nil)
	if l == nil {
		t.Fatal("KeyLayer returned nil")
	}
	if l.Name != "Detail Pane" {
		t.Errorf("Name = %q, want %q", l.Name, "Detail Pane")
	}
	if l.Bindings == nil || len(l.Bindings) != 0 {
		t.Errorf("expected empty Bindings map (38-1 detail pane has no pane-specific keys), got %d bindings", len(l.Bindings))
	}
	if l.Docs == nil || len(l.Docs) != 0 {
		t.Errorf("expected empty Docs map (v/y placeholders removed in 38-1 M4), got %d docs", len(l.Docs))
	}
}

func TestKeyLayer_FallbackNilSafe(t *testing.T) {
	t.Parallel()
	l := KeyLayer(nil)
	if l.Fallback != nil {
		t.Errorf("KeyLayer(nil).Fallback should be nil, got non-nil")
	}
}

func TestActiveModes_PIDPositive(t *testing.T) {
	t.Parallel()
	l := KeyLayer(nil)
	ctx := fakeSelectedPIDProvider{pid: 42}
	modes := l.ActiveModesFn(ctx)
	if len(modes) != 1 {
		t.Fatalf("expected 1 mode, got %d", len(modes))
	}
	if modes[0].Name != "pid" {
		t.Errorf("expected mode Name=pid, got %q", modes[0].Name)
	}
	if modes[0].Value != "42" {
		t.Errorf("expected mode Value=42, got %q", modes[0].Value)
	}
}

func TestActiveModes_PIDZero(t *testing.T) {
	t.Parallel()
	l := KeyLayer(nil)
	ctx := fakeSelectedPIDProvider{pid: 0}
	modes := l.ActiveModesFn(ctx)
	if len(modes) != 1 {
		t.Fatalf("expected 1 mode, got %d", len(modes))
	}
	if modes[0].Name != "view" {
		t.Errorf("expected mode Name=view, got %q", modes[0].Name)
	}
	if !strings.Contains(modes[0].Value, "no selection") {
		t.Errorf("expected mode Value to contain 'no selection', got %q", modes[0].Value)
	}
}

// nonProvider 故意不实现 SelectedPIDProvider 来测试 type assertion 失败路径。
type nonProvider struct{}

func TestActiveModes_NonSelectedPIDProviderContext(t *testing.T) {
	t.Parallel()
	l := KeyLayer(nil)
	modes := l.ActiveModesFn(nonProvider{})
	if modes != nil {
		t.Errorf("expected nil modes when ctx doesn't implement SelectedPIDProvider, got %v", modes)
	}
}

func TestActiveModes_PIDLargeValue(t *testing.T) {
	t.Parallel()
	// 边界 case：types.PID 是 uint64，验证大值 PID 正确格式化
	l := KeyLayer(nil)
	ctx := fakeSelectedPIDProvider{pid: types.PID(1234567890)}
	modes := l.ActiveModesFn(ctx)
	if len(modes) != 1 || modes[0].Value != "1234567890" {
		t.Errorf("expected mode Value=1234567890, got %v", modes)
	}
}

func TestStateProvider_InterfaceShape(t *testing.T) {
	t.Parallel()
	// 验证 StateProvider interface 可被引用（PR5 Step 3 / PR11 扩展用）
	// 编译能通过即表明 interface 定义存在（type assertion 类型在 PR5 Step 3 落地后可换为
	// `var _ StateProvider = (*DetailModel)(nil)` 在 model.go 中）。
	sp := (StateProvider)(nil)
	_ = sp // 编译期引用即可
}

