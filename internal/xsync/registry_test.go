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
	for i := 0; i < 100; i++ {
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
