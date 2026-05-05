// Package plugin — input_key_test.go (Story 38-5 PR11 Step 4(c))
//
// HandleInputKey 契约测试（SearchPlugin 的非-enter 输入态按键处理）。
package plugin

import (
	"testing"
	"time"
)

func TestHandleInputKey_Esc_ExitsAndClears(t *testing.T) {
	p := &SearchPlugin{Mode: true, Query: "foo"}
	handled := p.HandleInputKey("esc")
	if !handled {
		t.Error("esc 应返回 handled=true")
	}
	if p.Mode {
		t.Error("esc 应退出搜索模式")
	}
	if p.Query != "" {
		t.Errorf("esc 应清空 Query, got %q", p.Query)
	}
}

func TestHandleInputKey_Backspace_DeletesLastRune(t *testing.T) {
	p := &SearchPlugin{Mode: true, Query: "hello"}
	if !p.HandleInputKey("backspace") {
		t.Error("backspace 应返回 handled=true")
	}
	if p.Query != "hell" {
		t.Errorf("backspace: Query = %q, want %q", p.Query, "hell")
	}
}

func TestHandleInputKey_Backspace_EmptyQuery_Noop(t *testing.T) {
	p := &SearchPlugin{Mode: true, Query: ""}
	if !p.HandleInputKey("backspace") {
		t.Error("backspace 空 Query 仍应 handled=true")
	}
	if p.Query != "" {
		t.Errorf("Query 应保持空, got %q", p.Query)
	}
}

func TestHandleInputKey_Backspace_UnicodeRune(t *testing.T) {
	p := &SearchPlugin{Mode: true, Query: "中文a"}
	p.HandleInputKey("backspace")
	if p.Query != "中文" {
		t.Errorf("backspace 删除 ASCII rune: got %q, want %q", p.Query, "中文")
	}
	p.HandleInputKey("backspace")
	if p.Query != "中" {
		t.Errorf("backspace 删除中文 rune: got %q, want %q", p.Query, "中")
	}
}

func TestHandleInputKey_Space_AppendsSpace(t *testing.T) {
	p := &SearchPlugin{Mode: true, Query: "foo"}
	if !p.HandleInputKey(" ") {
		t.Error("\" \" 应 handled=true")
	}
	if p.Query != "foo " {
		t.Errorf("space: Query = %q, want %q", p.Query, "foo ")
	}

	// "space" 别名同等效果
	p2 := &SearchPlugin{Mode: true, Query: "bar"}
	p2.HandleInputKey("space")
	if p2.Query != "bar " {
		t.Errorf("\"space\" 别名: Query = %q, want %q", p2.Query, "bar ")
	}
}

func TestHandleInputKey_SingleCharAppends(t *testing.T) {
	p := &SearchPlugin{Mode: true, Query: ""}
	cases := []struct{ key, want string }{
		{"a", "a"},
		{"B", "aB"},
		{"1", "aB1"},
		{"-", "aB1-"},
		{"中", "aB1-中"}, // 单个 rune（即使 UTF-8 多 byte）
	}
	for _, tc := range cases {
		t.Run(tc.key, func(t *testing.T) {
			if !p.HandleInputKey(tc.key) {
				t.Errorf("single char %q 应 handled=true", tc.key)
			}
			if p.Query != tc.want {
				t.Errorf("Query = %q, want %q", p.Query, tc.want)
			}
		})
	}
}

func TestHandleInputKey_MultiCharKey_NotHandled(t *testing.T) {
	p := &SearchPlugin{Mode: true, Query: "foo"}
	cases := []string{"enter", "up", "down", "left", "right", "f1", "ctrl+c", "shift+tab"}
	for _, key := range cases {
		t.Run(key, func(t *testing.T) {
			if p.HandleInputKey(key) {
				t.Errorf("%q 应返回 handled=false（caller 自行处理）", key)
			}
			if p.Query != "foo" {
				t.Errorf("未处理键不应改变 Query, got %q", p.Query)
			}
			if !p.Mode {
				t.Error("未处理键不应改变 Mode")
			}
		})
	}
}

func TestHandleInputKey_NilReceiver_Safe(t *testing.T) {
	var p *SearchPlugin
	if handled := p.HandleInputKey("a"); handled {
		t.Error("nil receiver 应返回 false")
	}
}

func TestEnterSearchMode_Forward(t *testing.T) {
	p := &SearchPlugin{Query: "stale", Reverse: true}
	p.EnterSearchMode(false)
	if !p.Mode {
		t.Error("EnterSearchMode 应设 Mode=true")
	}
	if p.Query != "" {
		t.Errorf("Query 应清空, got %q", p.Query)
	}
	if p.Reverse {
		t.Error("forward 应清 Reverse=false")
	}
}

func TestEnterSearchMode_Backward(t *testing.T) {
	p := &SearchPlugin{}
	p.EnterSearchMode(true)
	if !p.Reverse {
		t.Error("backward 应设 Reverse=true")
	}
	if !p.Mode {
		t.Error("应设 Mode=true")
	}
}

func TestEnterSearchMode_PreservesMatches(t *testing.T) {
	// Matches/MatchIdx 不在 Enter 阶段清空（caller 在 enter 命令时显式重算）
	p := &SearchPlugin{Matches: []int{1, 2, 3}, MatchIdx: 1}
	p.EnterSearchMode(false)
	if len(p.Matches) != 3 || p.MatchIdx != 1 {
		t.Error("EnterSearchMode 不应清空 Matches/MatchIdx")
	}
}

func TestEnterSearchMode_NilReceiver_Safe(t *testing.T) {
	var p *SearchPlugin
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("nil receiver panic: %v", r)
		}
	}()
	p.EnterSearchMode(false)
}

// --- JumpMatch ---

func TestJumpMatch_EmptyMatches_ReturnsFalse(t *testing.T) {
	p := &SearchPlugin{Matches: nil, MatchIdx: 0}
	if p.JumpMatch(1) {
		t.Error("空 Matches 应返回 false")
	}
}

func TestJumpMatch_ForwardCycles(t *testing.T) {
	p := &SearchPlugin{Matches: []int{1, 2, 3}, MatchIdx: 0}
	if !p.JumpMatch(1) {
		t.Error("应返回 true")
	}
	if p.MatchIdx != 1 {
		t.Errorf("MatchIdx = %d, want 1", p.MatchIdx)
	}
	p.JumpMatch(1)
	p.JumpMatch(1)
	if p.MatchIdx != 0 {
		t.Errorf("循环到首位: MatchIdx = %d, want 0", p.MatchIdx)
	}
}

func TestJumpMatch_BackwardWraps(t *testing.T) {
	p := &SearchPlugin{Matches: []int{1, 2, 3}, MatchIdx: 0}
	p.JumpMatch(-1)
	if p.MatchIdx != 2 {
		t.Errorf("从 0 后退应环绕到末位: MatchIdx = %d, want 2", p.MatchIdx)
	}
}

func TestJumpMatch_ReverseFlipsDirection(t *testing.T) {
	// Reverse=true 时 dir=+1 实际效果是 -1（Story 36-6 AC-6）
	p := &SearchPlugin{Matches: []int{1, 2, 3}, MatchIdx: 0, Reverse: true}
	p.JumpMatch(1)
	if p.MatchIdx != 2 {
		t.Errorf("Reverse + dir=+1 应表现为 -1: MatchIdx = %d, want 2", p.MatchIdx)
	}
}

func TestJumpMatch_ReverseDoubleFlip(t *testing.T) {
	// Reverse=true + dir=-1 → 实际效果是 +1
	p := &SearchPlugin{Matches: []int{1, 2, 3}, MatchIdx: 0, Reverse: true}
	p.JumpMatch(-1)
	if p.MatchIdx != 1 {
		t.Errorf("Reverse + dir=-1 应表现为 +1: MatchIdx = %d, want 1", p.MatchIdx)
	}
}

func TestJumpMatch_NilReceiver_ReturnsFalse(t *testing.T) {
	var p *SearchPlugin
	if p.JumpMatch(1) {
		t.Error("nil receiver 应返回 false")
	}
}

// --- Reset ---

func TestReset_ClearsAllFields(t *testing.T) {
	p := &SearchPlugin{
		Mode:            true,
		Query:           "foo",
		Matches:         []int{1, 2},
		MatchIdx:        1,
		Reverse:         true,
		CrossLens:       true,
		NoMatchExpireAt: time.Now().Add(time.Hour),
	}
	p.Reset()
	if p.Mode {
		t.Error("Reset: Mode 应为 false")
	}
	if p.Query != "" {
		t.Errorf("Reset: Query = %q, want \"\"", p.Query)
	}
	if p.Matches != nil {
		t.Errorf("Reset: Matches = %v, want nil", p.Matches)
	}
	if p.MatchIdx != 0 {
		t.Errorf("Reset: MatchIdx = %d, want 0", p.MatchIdx)
	}
	if p.Reverse {
		t.Error("Reset: Reverse 应为 false")
	}
	if p.CrossLens {
		t.Error("Reset: CrossLens 应为 false")
	}
	if !p.NoMatchExpireAt.IsZero() {
		t.Errorf("Reset: NoMatchExpireAt 应为零值, got %v", p.NoMatchExpireAt)
	}
}

func TestReset_NilReceiver_Safe(t *testing.T) {
	var p *SearchPlugin
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("nil receiver panic: %v", r)
		}
	}()
	p.Reset()
}

// --- ExitSearchMode ---

func TestExitSearchMode_ClearsFourFields(t *testing.T) {
	p := &SearchPlugin{
		Mode:     true,
		Query:    "foo",
		Matches:  []int{1, 2},
		MatchIdx: 1,
	}
	p.ExitSearchMode()
	if p.Mode {
		t.Error("ExitSearchMode: Mode 应为 false")
	}
	if p.Query != "" {
		t.Errorf("ExitSearchMode: Query = %q, want \"\"", p.Query)
	}
	if p.Matches != nil {
		t.Errorf("ExitSearchMode: Matches 应为 nil, got %v", p.Matches)
	}
	if p.MatchIdx != 0 {
		t.Errorf("ExitSearchMode: MatchIdx = %d, want 0", p.MatchIdx)
	}
}

func TestExitSearchMode_PreservesCrossPaneFields(t *testing.T) {
	// 与 Reset 区别：保留 Reverse / CrossLens / NoMatchExpireAt（cross-pane 持久）
	expireTime := time.Now().Add(time.Hour)
	p := &SearchPlugin{
		Mode:            true,
		Query:           "stale",
		Reverse:         true,
		CrossLens:       true,
		NoMatchExpireAt: expireTime,
	}
	p.ExitSearchMode()
	if !p.Reverse {
		t.Error("ExitSearchMode: Reverse 应保留")
	}
	if !p.CrossLens {
		t.Error("ExitSearchMode: CrossLens 应保留")
	}
	if !p.NoMatchExpireAt.Equal(expireTime) {
		t.Error("ExitSearchMode: NoMatchExpireAt 应保留")
	}
}

func TestExitSearchMode_NilReceiver_Safe(t *testing.T) {
	var p *SearchPlugin
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("nil receiver panic: %v", r)
		}
	}()
	p.ExitSearchMode()
}
