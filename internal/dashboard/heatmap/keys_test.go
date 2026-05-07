// Package heatmap — keys_test.go (Story 38-5 PR3 Step 2)
//
// KeyLayer / StateProvider 行为测试。验证：
//
//  1. KeyLayer 返回的 Docs 键集合（4 个键）与 38-1 落地完全一致
//  2. ActiveModesFn 在 Expanded true/false 下返回正确 Mode
//  3. nil 安全：ctx 不实现 StateProvider 时返回 nil（不 panic）
//  4. fallback nil 安全
package heatmap

import (
	"testing"

	"github.com/rnixai/rnix/internal/ui"
)

// fakeStateProvider 是单元测试用的 StateProvider，让我们能在不依赖 cmd/rnix.dashboardModel
// 的前提下验证 ActiveModesFn 行为。
type fakeStateProvider struct {
	state HeatmapState
}

func (f fakeStateProvider) HeatmapState() HeatmapState { return f.state }

func TestKeyLayer_DocsRegistered(t *testing.T) {
	t.Parallel()
	l := KeyLayer(nil)
	if l == nil {
		t.Fatal("KeyLayer returned nil")
	}
	expectedKeys := []string{"=", "%", "t", "f"}
	for _, k := range expectedKeys {
		if _, ok := l.Docs[k]; !ok {
			t.Errorf("Docs missing key %q", k)
		}
	}
	if len(l.Docs) != len(expectedKeys) {
		t.Errorf("Docs has %d entries, want %d (38-1 four-key contract)", len(l.Docs), len(expectedKeys))
	}
	if l.Name != "Heatmap Pane" {
		t.Errorf("Name = %q, want %q", l.Name, "Heatmap Pane")
	}
}

func TestKeyLayer_FallbackNilSafe(t *testing.T) {
	t.Parallel()
	l := KeyLayer(nil)
	if l.Fallback != nil {
		t.Errorf("KeyLayer(nil).Fallback should be nil, got non-nil")
	}
}

func TestKeyLayer_DocKeyDescriptions(t *testing.T) {
	t.Parallel()
	l := KeyLayer(nil)
	expected := map[string]string{
		"=": "Absolute scale",
		"%": "Relative scale",
		"t": "Toggle totals",
		"f": "Filter by segment kind",
	}
	for k, want := range expected {
		got := l.Docs[k]
		if got.Description != want {
			t.Errorf("Docs[%q].Description = %q, want %q", k, got.Description, want)
		}
		if got.Key != k {
			t.Errorf("Docs[%q].Key = %q, want %q", k, got.Key, k)
		}
	}
}

func TestActiveModes_Expanded(t *testing.T) {
	t.Parallel()
	l := KeyLayer(nil)
	ctx := fakeStateProvider{state: HeatmapState{Expanded: true}}
	modes := l.ActiveModesFn(ctx)
	if len(modes) != 1 {
		t.Fatalf("got %d modes, want 1", len(modes))
	}
	if modes[0].Name != "view" || modes[0].Value != "expanded" {
		t.Errorf("modes[0] = %+v, want {view, expanded}", modes[0])
	}
}

func TestActiveModes_Summary(t *testing.T) {
	t.Parallel()
	l := KeyLayer(nil)
	ctx := fakeStateProvider{state: HeatmapState{Expanded: false}}
	modes := l.ActiveModesFn(ctx)
	if len(modes) != 1 {
		t.Fatalf("got %d modes, want 1", len(modes))
	}
	if modes[0].Name != "view" || modes[0].Value != "summary" {
		t.Errorf("modes[0] = %+v, want {view, summary}", modes[0])
	}
}

func TestActiveModes_NonStateProviderContext(t *testing.T) {
	t.Parallel()
	l := KeyLayer(nil)
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("ActiveModesFn panicked on non-StateProvider ctx: %v", r)
		}
	}()
	// nil ctx 不实现 StateProvider，应返回 nil 而不 panic
	var nilCtx ui.KeyContext // = nil interface value
	modes := l.ActiveModesFn(nilCtx)
	if modes != nil {
		t.Errorf("expected nil modes for nil ctx, got %v", modes)
	}
}

func TestStateProvider_Implementation(t *testing.T) {
	t.Parallel()
	// 编译期断言：fakeStateProvider 必须满足 StateProvider interface
	var _ StateProvider = fakeStateProvider{}
}
