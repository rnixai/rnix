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

func TestDriverRegistry_Names_Empty(t *testing.T) {
	t.Parallel()
	r := NewDriverRegistry()
	names := r.Names()
	if len(names) != 0 {
		t.Errorf("expected empty names, got %v", names)
	}
}

func TestDriverRegistry_Names_Sorted(t *testing.T) {
	t.Parallel()
	r := NewDriverRegistry()
	_ = r.Register("cursor", NewCursorCliDriver())
	_ = r.Register("claude", NewClaudeCliDriver())
	_ = r.Register("ollama", NewOpenAICompatDriver("ollama", "http://localhost:11434/v1"))

	names := r.Names()
	expected := []string{"claude", "cursor", "ollama"}
	if len(names) != len(expected) {
		t.Fatalf("expected %d names, got %d: %v", len(expected), len(names), names)
	}
	for i, name := range names {
		if name != expected[i] {
			t.Errorf("names[%d] = %q, expected %q", i, name, expected[i])
		}
	}
}

func TestDriverRegistry_Len(t *testing.T) {
	t.Parallel()
	r := NewDriverRegistry()
	if r.Len() != 0 {
		t.Errorf("expected 0, got %d", r.Len())
	}
	_ = r.Register("claude", NewClaudeCliDriver())
	if r.Len() != 1 {
		t.Errorf("expected 1, got %d", r.Len())
	}
	_ = r.Register("cursor", NewCursorCliDriver())
	if r.Len() != 2 {
		t.Errorf("expected 2, got %d", r.Len())
	}
}
