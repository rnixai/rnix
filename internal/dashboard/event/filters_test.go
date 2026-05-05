// Package event — filters_test.go (Story 38-5 PR11 Step 4(c))
package event

import (
	"strings"
	"testing"
)

func TestApplyStepFilterKey_NilFiltersLazyAlloc(t *testing.T) {
	got, _, _ := ApplyStepFilterKey(nil, true, "*")
	if got == nil {
		t.Fatal("nil 入参应被懒分配为 DefaultStepFilters")
	}
	expected := DefaultStepFilters()
	for k := range expected {
		if got[k] != expected[k] {
			t.Errorf("懒分配的 filter[%s] = %v, want %v", k, got[k], expected[k])
		}
	}
}

func TestApplyStepFilterKey_StepActionToggles(t *testing.T) {
	cases := []struct {
		key     string
		filterK string
	}{
		{"t", "tool_call"},
		{"p", "plan"},
		{"a", "text"},
		{"c", "complete"},
		{"s", "spawn"},
		{"r", "replan"},
		{"z", "specialize"},
	}
	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			f := DefaultStepFilters() // all true
			got, _, help := ApplyStepFilterKey(f, true, tc.key)
			if help {
				t.Errorf("key %q triggered help (expected toggle)", tc.key)
			}
			if got[tc.filterK] != false {
				t.Errorf("key %q: filter[%s] = true, want false (toggled)", tc.key, tc.filterK)
			}
			// Toggle back
			got, _, _ = ApplyStepFilterKey(got, true, tc.key)
			if got[tc.filterK] != true {
				t.Errorf("key %q (2nd toggle): filter[%s] = false, want true", tc.key, tc.filterK)
			}
		})
	}
}

func TestApplyStepFilterKey_SystemEventToggles(t *testing.T) {
	cases := []struct {
		key     string
		filterK string
	}{
		{"C", EventCompact},
		{"b", EventBudget},
		{"x", "sys_spawn"},
		{"X", EventExit},
		{"T", EventStall},
		{"i", EventImmune},
	}
	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			f := DefaultStepFilters()
			got, _, _ := ApplyStepFilterKey(f, true, tc.key)
			if got[tc.filterK] != false {
				t.Errorf("system key %q: filter[%s] = true, want false", tc.key, tc.filterK)
			}
		})
	}
}

func TestApplyStepFilterKey_StarResetsAll(t *testing.T) {
	f := DefaultStepFilters()
	f["tool_call"] = false
	f["plan"] = false

	got, _, _ := ApplyStepFilterKey(f, true, "*")
	expected := DefaultStepFilters()
	for k, v := range expected {
		if got[k] != v {
			t.Errorf("after reset: filter[%s] = %v, want %v", k, got[k], v)
		}
	}
}

func TestApplyStepFilterKey_FExitsFilterMode(t *testing.T) {
	_, mode, help := ApplyStepFilterKey(DefaultStepFilters(), true, "f")
	if mode {
		t.Error("key f 应退出 filter mode")
	}
	if help {
		t.Error("key f 不应触发 help")
	}
}

func TestApplyStepFilterKey_EscExitsFilterMode(t *testing.T) {
	_, mode, _ := ApplyStepFilterKey(DefaultStepFilters(), true, "esc")
	if mode {
		t.Error("key esc 应退出 filter mode")
	}
}

func TestApplyStepFilterKey_UnknownKeyShowsHelp(t *testing.T) {
	_, mode, help := ApplyStepFilterKey(DefaultStepFilters(), true, "Q")
	if !help {
		t.Error("未知按键应触发 showHelp=true")
	}
	if !mode {
		t.Error("未知按键不应改变 filter mode")
	}
}

func TestApplyStepFilterKey_HelpMsgConstant(t *testing.T) {
	if FilterHelpMsg == "" {
		t.Error("FilterHelpMsg 不应为空")
	}
	// Sanity check on key列表（防止文案漂移）
	mustContain := []string{"t/p/a/c/s/r/z", "C/b/x/X/T/i", "* all", "Esc"}
	for _, sub := range mustContain {
		if !strings.Contains(FilterHelpMsg, sub) {
			t.Errorf("FilterHelpMsg 缺少 %q", sub)
		}
	}
}
