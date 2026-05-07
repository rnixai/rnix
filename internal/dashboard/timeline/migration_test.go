// Package timeline — migration_test.go (Story 38-5 PR11 Step 4(c))
package timeline

import (
	"errors"
	"testing"

	"github.com/rnixai/rnix/internal/ui"
)

func TestMigrationCheck_AlreadyChecked_Noop(t *testing.T) {
	state := TimelineState{MigrationChecked: true, SortAsc: true}
	got, show := MigrationCheck(state, func(*ui.UIState) error { return nil })
	if show {
		t.Error("MigrationChecked=true 应当 noop 返回 false")
	}
	if got.UIState != nil {
		t.Error("noop 不应分配 UIState")
	}
}

func TestMigrationCheck_FirstCheck_AscMode_TriggersNotice(t *testing.T) {
	saved := false
	saver := func(*ui.UIState) error { saved = true; return nil }
	state := TimelineState{SortAsc: true}

	got, show := MigrationCheck(state, saver)

	if !show {
		t.Error("升序首次检查应返回 show=true")
	}
	if !got.MigrationChecked {
		t.Error("MigrationChecked 应被标记为 true")
	}
	if got.UIState == nil {
		t.Fatal("UIState 应被懒分配")
	}
	if !got.UIState.TimelineSortMigrationShown {
		t.Error("TimelineSortMigrationShown 应被标记为 true")
	}
	if !saved {
		t.Error("saver 应被调用")
	}
}

func TestMigrationCheck_FirstCheck_DescMode_NoNotice(t *testing.T) {
	state := TimelineState{SortAsc: false}
	got, show := MigrationCheck(state, func(*ui.UIState) error {
		t.Error("descending mode 不应触发 saver")
		return nil
	})
	if show {
		t.Error("非升序模式不应触发提示")
	}
	if !got.MigrationChecked {
		t.Error("MigrationChecked 仍应标记（防止反复检查）")
	}
}

func TestMigrationCheck_AlreadyShown_NoNotice(t *testing.T) {
	state := TimelineState{
		SortAsc: true,
		UIState: &ui.UIState{TimelineSortMigrationShown: true},
	}
	got, show := MigrationCheck(state, func(*ui.UIState) error {
		t.Error("已展示过不应再次 saver")
		return nil
	})
	if show {
		t.Error("已展示过不应再次触发提示")
	}
	if !got.MigrationChecked {
		t.Error("MigrationChecked 仍应标记")
	}
}

func TestMigrationCheck_SaverError_DoesNotBlock(t *testing.T) {
	state := TimelineState{SortAsc: true}
	saver := func(*ui.UIState) error { return errors.New("disk full") }

	got, show := MigrationCheck(state, saver)

	if !show {
		t.Error("saver 失败仍应 show=true（不阻塞 UI）")
	}
	if !got.UIState.TimelineSortMigrationShown {
		t.Error("flag 仍应标记（即使 saver 失败）")
	}
}

func TestMigrationCheck_NilSaver_UsesDefault(t *testing.T) {
	// nil saver fallback to ui.SaveUIState（生产路径 · 不应 panic）
	state := TimelineState{SortAsc: true}
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("nil saver fallback panic: %v", r)
		}
	}()
	_, _ = MigrationCheck(state, nil)
}

func TestMigrationCheck_LazyAllocatesUIState(t *testing.T) {
	state := TimelineState{SortAsc: false} // 不触发 saver 但仍应分配
	got, _ := MigrationCheck(state, func(*ui.UIState) error { return nil })
	if got.UIState == nil {
		t.Error("UIState 应被懒分配（即使非升序）")
	}
}
