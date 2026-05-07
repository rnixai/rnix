// Package tree — keys_test.go (Story 38-5 PR2 Step 2)
//
// KeyLayer / SortLabels / StateProvider 行为测试。验证：
//
//	1. KeyLayer 返回的 Docs 键集合（5 个键）与 38-1 落地完全一致
//	2. ActiveModesFn 在不同 SortMode/SortAsc/SearchMode 组合下返回正确 Mode
//	3. nil 安全：ctx 不实现 StateProvider 时返回 nil（不 panic）
//	4. SortLabels 长度与 SortMode 取值范围对齐（防越界）
package tree

import (
	"testing"

	"github.com/rnixai/rnix/internal/ui"
)

// fakeStateProvider 是单元测试用的 StateProvider，让我们能在不依赖 cmd/rnix.dashboardModel
// 的前提下验证 ActiveModesFn 行为。
type fakeStateProvider struct {
	state TreeState
}

func (f fakeStateProvider) TreeState() TreeState { return f.state }

func TestKeyLayer_DocsRegistered(t *testing.T) {
	t.Parallel()
	l := KeyLayer(nil)
	if l == nil {
		t.Fatal("KeyLayer returned nil")
	}
	expectedKeys := []string{"K", "s", "o", "enter", "/"}
	for _, k := range expectedKeys {
		if _, ok := l.Docs[k]; !ok {
			t.Errorf("Docs missing key %q", k)
		}
	}
	if len(l.Docs) != len(expectedKeys) {
		t.Errorf("Docs has %d entries, want %d (38-1 五键契约)", len(l.Docs), len(expectedKeys))
	}
	if l.Name != "Tree Pane" {
		t.Errorf("Name = %q, want %q", l.Name, "Tree Pane")
	}
}

func TestKeyLayer_FallbackNilSafe(t *testing.T) {
	t.Parallel()
	l := KeyLayer(nil)
	if l.Fallback != nil {
		t.Errorf("KeyLayer(nil).Fallback should be nil, got non-nil")
	}
	// 注：实际 fallback 调用由 ui.Dispatcher 路由，本包不直接测试 fallback 执行（cmd/rnix 集成测试覆盖）。
	// fallback 非 nil 时的行为由 cmd/rnix/dashboard_test.go 的 keybind contract test 覆盖。
}

func TestActiveModes_SortTime(t *testing.T) {
	t.Parallel()
	l := KeyLayer(nil)
	sp := fakeStateProvider{state: TreeState{SortMode: 0, SortAsc: false}}
	modes := l.ActiveModes(sp)
	wantSort := ui.Mode{Name: "sort", Value: "time"}
	wantDir := ui.Mode{Name: "dir", Value: "desc"}
	if len(modes) != 2 {
		t.Fatalf("modes len = %d, want 2 (sort + dir, no search)", len(modes))
	}
	if modes[0] != wantSort {
		t.Errorf("modes[0] = %v, want %v", modes[0], wantSort)
	}
	if modes[1] != wantDir {
		t.Errorf("modes[1] = %v, want %v", modes[1], wantDir)
	}
}

func TestActiveModes_SortPIDAscending(t *testing.T) {
	t.Parallel()
	l := KeyLayer(nil)
	sp := fakeStateProvider{state: TreeState{SortMode: 1, SortAsc: true}}
	modes := l.ActiveModes(sp)
	if len(modes) != 2 {
		t.Fatalf("modes len = %d, want 2", len(modes))
	}
	if modes[0].Value != "pid" {
		t.Errorf("sort.Value = %q, want %q", modes[0].Value, "pid")
	}
	if modes[1].Value != "asc" {
		t.Errorf("dir.Value = %q, want %q", modes[1].Value, "asc")
	}
}

func TestActiveModes_SortStateWithSearch(t *testing.T) {
	t.Parallel()
	l := KeyLayer(nil)
	sp := fakeStateProvider{state: TreeState{SortMode: 2, SortAsc: false, SearchMode: true}}
	modes := l.ActiveModes(sp)
	if len(modes) != 3 {
		t.Fatalf("modes len = %d, want 3 (sort + dir + search)", len(modes))
	}
	if modes[0].Value != "state" {
		t.Errorf("sort.Value = %q, want %q", modes[0].Value, "state")
	}
	if modes[2] != (ui.Mode{Name: "search", Value: "on"}) {
		t.Errorf("modes[2] = %v, want search:on", modes[2])
	}
}

func TestActiveModes_SearchQueryWithoutMode(t *testing.T) {
	t.Parallel()
	// SearchMode=false 但 SearchQuery 非空时仍应显示 search:on（用户已退出输入但保留 query）。
	l := KeyLayer(nil)
	sp := fakeStateProvider{state: TreeState{SortMode: 0, SortAsc: false, SearchQuery: "init"}}
	modes := l.ActiveModes(sp)
	if len(modes) != 3 {
		t.Fatalf("modes len = %d, want 3 (sort + dir + search via residual query)", len(modes))
	}
	if modes[2].Name != "search" || modes[2].Value != "on" {
		t.Errorf("modes[2] = %v, want search:on (SearchQuery!=\"\" 触发)", modes[2])
	}
}

func TestActiveModes_SortModeOutOfRange(t *testing.T) {
	t.Parallel()
	// SortMode 越界（如 99）时应回退到默认 "time" 而不是 panic / out-of-bounds。
	l := KeyLayer(nil)
	sp := fakeStateProvider{state: TreeState{SortMode: 99, SortAsc: false}}
	modes := l.ActiveModes(sp)
	if len(modes) != 2 {
		t.Fatalf("modes len = %d, want 2", len(modes))
	}
	if modes[0].Value != "time" {
		t.Errorf("sort.Value = %q, want %q (越界回退)", modes[0].Value, "time")
	}
}

func TestActiveModes_NonStateProviderContext(t *testing.T) {
	t.Parallel()
	// ctx 不实现 StateProvider 时返回 nil（典型场景：cmd/rnix 端 cast 失败 / 测试传入 string）。
	l := KeyLayer(nil)
	modes := l.ActiveModes("not-a-state-provider")
	if modes != nil {
		t.Errorf("ActiveModes with non-StateProvider ctx = %v, want nil", modes)
	}
}

func TestSortLabels_AlignedWithTreeStateSortMode(t *testing.T) {
	t.Parallel()
	want := []string{"Time", "PID", "State"}
	if len(SortLabels) != len(want) {
		t.Fatalf("len(SortLabels) = %d, want %d", len(SortLabels), len(want))
	}
	for i, w := range want {
		if SortLabels[i] != w {
			t.Errorf("SortLabels[%d] = %q, want %q", i, SortLabels[i], w)
		}
	}
}
