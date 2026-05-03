// Package plugin — search_test.go (Story 38-5 PR1 SearchPlugin contract tests)
package plugin

import (
	"testing"
)

// fakeSearchable 用于测试 SearchPlugin.Apply 在不同 Searchable 实现上的行为。
type fakeSearchable struct {
	lines []string
}

func (f *fakeSearchable) SearchableLines() []string {
	if f == nil {
		return nil
	}
	return f.lines
}

func TestSearchPlugin_Apply_BasicMatch(t *testing.T) {
	target := &fakeSearchable{lines: []string{"alpha", "beta gamma", "delta"}}
	p := &SearchPlugin{}

	matches := p.Apply(target, "gamma")
	if len(matches) != 1 || matches[0] != 1 {
		t.Errorf("matches = %v, want [1]", matches)
	}
}

func TestSearchPlugin_Apply_CaseInsensitive(t *testing.T) {
	target := &fakeSearchable{lines: []string{"Hello World", "GoLang"}}
	p := &SearchPlugin{}

	matches := p.Apply(target, "world")
	if len(matches) != 1 || matches[0] != 0 {
		t.Errorf("case-insensitive matches = %v, want [0]", matches)
	}

	matches = p.Apply(target, "GOLANG")
	if len(matches) != 1 || matches[0] != 1 {
		t.Errorf("uppercase query matches = %v, want [1]", matches)
	}
}

func TestSearchPlugin_Apply_NilTarget(t *testing.T) {
	p := &SearchPlugin{}
	if got := p.Apply(nil, "anything"); got != nil {
		t.Errorf("Apply(nil, _) = %v, want nil", got)
	}
}

func TestSearchPlugin_Apply_EmptyQuery(t *testing.T) {
	target := &fakeSearchable{lines: []string{"a", "b"}}
	p := &SearchPlugin{}
	if got := p.Apply(target, ""); got != nil {
		t.Errorf("Apply(_, \"\") = %v, want nil", got)
	}
}

func TestSearchPlugin_Apply_NilPlugin(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("nil SearchPlugin.Apply panicked: %v", r)
		}
	}()
	target := &fakeSearchable{lines: []string{"a"}}
	var p *SearchPlugin // nil
	if got := p.Apply(target, "a"); got != nil {
		t.Errorf("nil plugin Apply = %v, want nil", got)
	}
}

func TestSearchPlugin_Apply_NoMatch(t *testing.T) {
	target := &fakeSearchable{lines: []string{"alpha", "beta"}}
	p := &SearchPlugin{}
	if got := p.Apply(target, "zeta"); got != nil {
		t.Errorf("no-match Apply = %v, want nil", got)
	}
}

func TestSearchPlugin_Apply_MultipleMatches(t *testing.T) {
	target := &fakeSearchable{lines: []string{"foo bar", "bar foo", "baz", "foo"}}
	p := &SearchPlugin{}
	matches := p.Apply(target, "foo")
	wantMatches := []int{0, 1, 3}
	if len(matches) != len(wantMatches) {
		t.Fatalf("multi-match count = %d, want %d", len(matches), len(wantMatches))
	}
	for i := range wantMatches {
		if matches[i] != wantMatches[i] {
			t.Errorf("matches[%d] = %d, want %d", i, matches[i], wantMatches[i])
		}
	}
}
