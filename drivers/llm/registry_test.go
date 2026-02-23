package llm

import (
	"testing"
)

func TestDriverRegistry_RegisterGet(t *testing.T) {
	r := NewDriverRegistry()
	d := NewClaudeCliDriver()

	if err := r.Register("/dev/llm/claude", d); err != nil {
		t.Fatalf("Register failed: %v", err)
	}

	got, ok := r.Get("/dev/llm/claude")
	if !ok {
		t.Fatal("expected driver to be found")
	}
	if got.Info().Name != d.Info().Name {
		t.Errorf("expected driver name %q, got %q", d.Info().Name, got.Info().Name)
	}
}

func TestDriverRegistry_DuplicateRegister(t *testing.T) {
	r := NewDriverRegistry()
	d := NewClaudeCliDriver()

	if err := r.Register("/dev/llm/claude", d); err != nil {
		t.Fatalf("first Register failed: %v", err)
	}

	err := r.Register("/dev/llm/claude", d)
	if err == nil {
		t.Fatal("expected error on duplicate register, got nil")
	}
}

func TestDriverRegistry_GetNotFound(t *testing.T) {
	r := NewDriverRegistry()

	_, ok := r.Get("/dev/llm/nonexistent")
	if ok {
		t.Error("expected driver not found")
	}
}
