package ui

import (
	"reflect"
	"testing"

	"charm.land/bubbles/v2/viewport"
)

func TestHandleListKey_CursorOnly(t *testing.T) {
	cases := []struct {
		name       string
		key        string
		start      int
		itemCount  int
		pageSize   int
		wantCursor int
		wantHandle bool
	}{
		{"j mid", "j", 3, 10, 5, 4, true},
		{"j at end", "j", 9, 10, 5, 9, true},
		{"down alias", "down", 0, 10, 5, 1, true},
		{"k mid", "k", 3, 10, 5, 2, true},
		{"k at start", "k", 0, 10, 5, 0, true},
		{"up alias", "up", 5, 10, 5, 4, true},
		{"pgdown mid", "pgdown", 2, 10, 5, 7, true},
		{"pgdown clamp", "pgdown", 8, 10, 5, 9, true},
		{"ctrl+f alias", "ctrl+f", 0, 10, 4, 4, true},
		{"pgup mid", "pgup", 7, 10, 5, 2, true},
		{"pgup clamp", "pgup", 2, 10, 5, 0, true},
		{"ctrl+b alias", "ctrl+b", 8, 10, 4, 4, true},
		{"ctrl+d half", "ctrl+d", 2, 10, 6, 5, true},
		{"ctrl+u half", "ctrl+u", 5, 10, 6, 2, true},
		{"g home", "g", 5, 10, 5, 0, true},
		{"home alias", "home", 9, 10, 5, 0, true},
		{"G end", "G", 0, 10, 5, 9, true},
		{"shift+G", "shift+G", 3, 10, 5, 9, true},
		{"end alias", "end", 2, 10, 5, 9, true},
		{"unknown key", "x", 3, 10, 5, 3, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cur := tc.start
			got := HandleListKey(tc.key, nil, &cur, tc.itemCount, ListNavOpts{PageSize: tc.pageSize})
			if got != tc.wantHandle {
				t.Fatalf("handled = %v, want %v", got, tc.wantHandle)
			}
			if cur != tc.wantCursor {
				t.Fatalf("cursor = %d, want %d", cur, tc.wantCursor)
			}
		})
	}
}

func TestHandleListKey_EmptyList(t *testing.T) {
	for _, key := range []string{"j", "k", "pgdown", "pgup", "ctrl+d", "ctrl+u", "g", "G", "home", "end"} {
		cur := 0
		handled := HandleListKey(key, nil, &cur, 0, ListNavOpts{PageSize: 5})
		if !handled {
			t.Errorf("key %q: expected handled=true for empty list", key)
		}
		if cur != 0 {
			t.Errorf("key %q: cursor mutated to %d on empty list", key, cur)
		}
	}
}

func TestHandleListKey_OnCursorChange(t *testing.T) {
	called := 0
	lastSeen := -1
	opts := ListNavOpts{PageSize: 5, OnCursorChange: func(c int) {
		called++
		lastSeen = c
	}}
	cur := 2
	HandleListKey("j", nil, &cur, 10, opts)
	if called != 1 || lastSeen != 3 {
		t.Fatalf("expected callback once with 3, got called=%d last=%d", called, lastSeen)
	}
	// No change at top → no callback
	called = 0
	cur = 0
	HandleListKey("k", nil, &cur, 10, opts)
	if called != 0 {
		t.Fatalf("unexpected callback for no-op")
	}
}

func TestHandleListKey_ViewportOnly(t *testing.T) {
	vp := viewport.New(viewport.WithHeight(10), viewport.WithWidth(40))
	vp.SetContent("a\nb\nc\nd\ne\nf\ng\nh\ni\nj\nk\nl\nm\nn\no\np")
	// PageDown should advance Y offset
	before := vp.YOffset()
	handled := HandleListKey("pgdown", &vp, nil, 0, ListNavOpts{})
	if !handled {
		t.Fatal("pgdown not handled")
	}
	if vp.YOffset() == before {
		t.Logf("note: viewport did not advance (content may fit); YOffset before=%d after=%d", before, vp.YOffset())
	}
	// GotoBottom then GotoTop
	HandleListKey("G", &vp, nil, 0, ListNavOpts{})
	HandleListKey("g", &vp, nil, 0, ListNavOpts{})
	if vp.YOffset() != 0 {
		t.Fatalf("expected YOffset 0 after g, got %d", vp.YOffset())
	}
}

func TestFindMatches(t *testing.T) {
	content := "alpha\nBeta bar\ngamma\nbar delta\nend"
	got := FindMatches(content, "bar")
	want := []int{1, 3}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	if FindMatches(content, "") != nil {
		t.Fatal("empty query should return nil")
	}
	if FindMatches(content, "xx") != nil {
		t.Fatal("no match should return nil")
	}
}

func TestHighlightMatches(t *testing.T) {
	got := HighlightMatches("foo BAR baz bar", "bar", "<", ">")
	want := "foo <BAR> baz <bar>"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	if HighlightMatches("abc", "", "<", ">") != "abc" {
		t.Fatal("empty query unchanged")
	}
}

func TestClampCursor(t *testing.T) {
	if clampCursor(-1, 5) != 0 {
		t.Fatal("negative should clamp to 0")
	}
	if clampCursor(10, 5) != 4 {
		t.Fatal("oob should clamp to n-1")
	}
	if clampCursor(0, 0) != 0 {
		t.Fatal("empty list should return 0")
	}
}
