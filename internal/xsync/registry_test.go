package xsync

import (
	"fmt"
	"sync"
	"testing"
)

func TestRegistry_RegisterAndGet(t *testing.T) {
	r := NewRegistry[string]()
	if err := r.Register("a", "alpha"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	v, ok := r.Get("a")
	if !ok || v != "alpha" {
		t.Fatalf("got (%q, %v), want (\"alpha\", true)", v, ok)
	}
}

func TestRegistry_DuplicateRegister(t *testing.T) {
	r := NewRegistry[int]()
	_ = r.Register("x", 1)
	if err := r.Register("x", 2); err == nil {
		t.Fatal("expected error on duplicate register")
	}
}

func TestRegistry_GetMissing(t *testing.T) {
	r := NewRegistry[int]()
	_, ok := r.Get("missing")
	if ok {
		t.Fatal("expected ok=false for missing key")
	}
}

func TestRegistry_List(t *testing.T) {
	r := NewRegistry[int]()
	_ = r.Register("a", 1)
	_ = r.Register("b", 2)
	_ = r.Register("c", 3)
	items := r.List()
	if len(items) != 3 {
		t.Fatalf("got %d items, want 3", len(items))
	}
}

func TestRegistry_ConcurrentAccess(t *testing.T) {
	r := NewRegistry[int]()
	var wg sync.WaitGroup
	for i := range 100 {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_ = r.Register(fmt.Sprintf("key-%d", i), i)
			r.Get(fmt.Sprintf("key-%d", i))
			r.List()
		}(i)
	}
	wg.Wait()
	if len(r.List()) != 100 {
		t.Fatalf("expected 100 items, got %d", len(r.List()))
	}
}

// --- Story 9.1: Unregister Tests ---

func TestRegistry_Unregister(t *testing.T) {
	t.Run("unregister existing key succeeds", func(t *testing.T) {
		// Given: a Registry with a registered key
		r := NewRegistry[string]()
		_ = r.Register("mcp-github", "github-transport")

		// When: Unregister is called
		err := r.Unregister("mcp-github")

		// Then: no error, key is removed
		if err != nil {
			t.Fatalf("Unregister failed: %v", err)
		}
		_, ok := r.Get("mcp-github")
		if ok {
			t.Fatal("expected key to be removed after Unregister")
		}
	})

	t.Run("unregister missing key returns error", func(t *testing.T) {
		// Given: an empty Registry
		r := NewRegistry[string]()

		// When: Unregister is called for a non-existent key
		err := r.Unregister("nonexistent")

		// Then: error is returned
		if err == nil {
			t.Fatal("expected error for unregistering missing key, got nil")
		}
	})

	t.Run("unregister then register same key succeeds", func(t *testing.T) {
		// Given: a Registry with a registered then unregistered key
		r := NewRegistry[string]()
		_ = r.Register("mcp-github", "v1")
		_ = r.Unregister("mcp-github")

		// When: Register is called again with the same key
		err := r.Register("mcp-github", "v2")

		// Then: registration succeeds
		if err != nil {
			t.Fatalf("re-Register after Unregister failed: %v", err)
		}
		v, ok := r.Get("mcp-github")
		if !ok || v != "v2" {
			t.Fatalf("expected value 'v2', got (%q, %v)", v, ok)
		}
	})

	t.Run("unregister updates list count", func(t *testing.T) {
		// Given: a Registry with 3 items
		r := NewRegistry[int]()
		_ = r.Register("a", 1)
		_ = r.Register("b", 2)
		_ = r.Register("c", 3)

		// When: one item is unregistered
		_ = r.Unregister("b")

		// Then: list has 2 items
		items := r.List()
		if len(items) != 2 {
			t.Fatalf("expected 2 items after Unregister, got %d", len(items))
		}
	})
}

func TestRegistry_ConcurrentUnregister(t *testing.T) {
	t.Run("concurrent register and unregister is safe", func(t *testing.T) {
		// Given: a Registry
		r := NewRegistry[int]()

		// Pre-register keys
		for i := range 50 {
			_ = r.Register(fmt.Sprintf("key-%d", i), i)
		}

		// When: concurrent Unregister operations
		var wg sync.WaitGroup
		for i := range 50 {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				_ = r.Unregister(fmt.Sprintf("key-%d", i))
			}(i)
		}
		wg.Wait()

		// Then: no panic, all items removed (verified by -race flag)
		if len(r.List()) != 0 {
			t.Fatalf("expected 0 items after concurrent Unregister, got %d", len(r.List()))
		}
	})
}
