// Package timeline — keys_test.go (Story 38-5 PR4 Step 2)
//
// KeyLayer / StateProvider 行为测试。验证：
//
//  1. KeyLayer 返回的 Docs 键集合（10 个键）与 38-1 落地完全一致
//  2. ActiveModesFn 在 StepFilterMode + ExpandMode 三态下返回正确 Mode
//  3. nil 安全：ctx 不实现 StateProvider 时返回 nil（不 panic）
//  4. fallback nil 安全
//  5. ExpandMode 越界回退（safety net · 与 cmd/rnix 端 pre-PR4 一致语义）
package timeline

import (
	"testing"

	"github.com/rnixai/rnix/internal/ui"
)

// fakeStateProvider 是单元测试用的 StateProvider，让我们能在不依赖 cmd/rnix.dashboardModel
// 的前提下验证 ActiveModesFn 行为。
type fakeStateProvider struct {
	state TimelineState
}

func (f fakeStateProvider) TimelineState() TimelineState { return f.state }

func TestKeyLayer_DocsRegistered(t *testing.T) {
	t.Parallel()
	l := KeyLayer(nil)
	if l == nil {
		t.Fatal("KeyLayer returned nil")
	}
	expectedKeys := []string{"f", "F", "v", "V", "e", "E", "C", "o", "P", "enter"}
	for _, k := range expectedKeys {
		if _, ok := l.Docs[k]; !ok {
			t.Errorf("Docs missing key %q", k)
		}
	}
	if len(l.Docs) != len(expectedKeys) {
		t.Errorf("Docs has %d entries, want %d (38-1 ten-key contract)", len(l.Docs), len(expectedKeys))
	}
	if l.Name != "Timeline Pane" {
		t.Errorf("Name = %q, want %q", l.Name, "Timeline Pane")
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
		"f":     "Filter mode (event types)",
		"F":     "Toggle follow live",
		"v":     "Expand step detail (L2)",
		"V":     "Debug detail (L3)",
		"e":     "Expand mode: all",
		"E":     "Expand mode: errors only",
		"C":     "Expand mode: collapsed",
		"o":     "Toggle sort direction",
		"P":     "Step Inspector (System lens)",
		"enter": "Expand / collapse",
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

func TestActiveModes_FilterOff_ExpandCollapsed(t *testing.T) {
	t.Parallel()
	l := KeyLayer(nil)
	ctx := fakeStateProvider{state: TimelineState{
		StepFilterMode: false,
		ExpandMode:     ExpandModeCollapsed,
	}}
	modes := l.ActiveModesFn(ctx)
	if len(modes) != 1 {
		t.Fatalf("got %d modes, want 1 (no filter, expand only)", len(modes))
	}
	if modes[0].Name != "expand" || modes[0].Value != "collapsed" {
		t.Errorf("modes[0] = %+v, want {expand, collapsed}", modes[0])
	}
}

func TestActiveModes_FilterOn_ExpandExpanded(t *testing.T) {
	t.Parallel()
	l := KeyLayer(nil)
	ctx := fakeStateProvider{state: TimelineState{
		StepFilterMode: true,
		ExpandMode:     ExpandModeExpanded,
	}}
	modes := l.ActiveModesFn(ctx)
	if len(modes) != 2 {
		t.Fatalf("got %d modes, want 2 (filter + expand)", len(modes))
	}
	if modes[0].Name != "filter" || modes[0].Value != "on" {
		t.Errorf("modes[0] = %+v, want {filter, on}", modes[0])
	}
	if modes[1].Name != "expand" || modes[1].Value != "all" {
		t.Errorf("modes[1] = %+v, want {expand, all}", modes[1])
	}
}

func TestActiveModes_FilterOn_ExpandErrorsOnly(t *testing.T) {
	t.Parallel()
	l := KeyLayer(nil)
	ctx := fakeStateProvider{state: TimelineState{
		StepFilterMode: true,
		ExpandMode:     ExpandModeErrorsOnly,
	}}
	modes := l.ActiveModesFn(ctx)
	if len(modes) != 2 {
		t.Fatalf("got %d modes, want 2 (filter + expand)", len(modes))
	}
	if modes[1].Name != "expand" || modes[1].Value != "errors" {
		t.Errorf("modes[1] = %+v, want {expand, errors}", modes[1])
	}
}

func TestActiveModes_ExpandOutOfRangeFallsBackToCollapsed(t *testing.T) {
	t.Parallel()
	l := KeyLayer(nil)
	ctx := fakeStateProvider{state: TimelineState{
		ExpandMode: TimelineExpandMode(99), // 越界值
	}}
	modes := l.ActiveModesFn(ctx)
	if len(modes) != 1 {
		t.Fatalf("got %d modes, want 1 (expand only)", len(modes))
	}
	if modes[0].Value != "collapsed" {
		t.Errorf("modes[0].Value = %q, want %q (out-of-range fallback)", modes[0].Value, "collapsed")
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

func TestExpandLabels_AlignedWithExpandMode(t *testing.T) {
	t.Parallel()
	// ExpandLabels 公开常量与 TimelineExpandMode 取值范围一一对应（按索引）
	if ExpandLabels[ExpandModeCollapsed] != "collapsed" {
		t.Errorf("ExpandLabels[Collapsed] = %q, want %q", ExpandLabels[ExpandModeCollapsed], "collapsed")
	}
	if ExpandLabels[ExpandModeExpanded] != "all" {
		t.Errorf("ExpandLabels[Expanded] = %q, want %q", ExpandLabels[ExpandModeExpanded], "all")
	}
	if ExpandLabels[ExpandModeErrorsOnly] != "errors" {
		t.Errorf("ExpandLabels[ErrorsOnly] = %q, want %q", ExpandLabels[ExpandModeErrorsOnly], "errors")
	}
}
