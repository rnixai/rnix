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
	_ = r.Register("ollama", NewOpenAIDriver("ollama", WithOpenAIBaseURL("http://localhost:11434/v1")))

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

// --- Health Status Tests ---

func TestDriverRegistry_HealthStatus_DefaultUnchecked(t *testing.T) {
	t.Parallel()
	r := NewDriverRegistry()
	_ = r.Register("claude", NewClaudeCliDriver())

	if got := r.GetHealth("claude"); got != HealthStatusUnchecked {
		t.Errorf("GetHealth = %q, want %q", got, HealthStatusUnchecked)
	}
}

func TestDriverRegistry_SetHealth_Healthy(t *testing.T) {
	t.Parallel()
	r := NewDriverRegistry()
	_ = r.Register("ollama", NewOpenAIDriver("ollama", WithOpenAIBaseURL("http://localhost:11434/v1")))

	r.SetHealth("ollama", HealthStatusHealthy)
	if got := r.GetHealth("ollama"); got != HealthStatusHealthy {
		t.Errorf("GetHealth = %q, want %q", got, HealthStatusHealthy)
	}
}

func TestDriverRegistry_SetHealth_Unhealthy(t *testing.T) {
	t.Parallel()
	r := NewDriverRegistry()
	_ = r.Register("groq", NewOpenAIDriver("groq", WithOpenAIBaseURL("https://api.groq.com/openai/v1")))

	r.SetHealth("groq", HealthStatusUnhealthy)
	if got := r.GetHealth("groq"); got != HealthStatusUnhealthy {
		t.Errorf("GetHealth = %q, want %q", got, HealthStatusUnhealthy)
	}
}

func TestDriverRegistry_GetHealth_NotRegistered(t *testing.T) {
	t.Parallel()
	r := NewDriverRegistry()

	if got := r.GetHealth("nonexistent"); got != HealthStatusUnchecked {
		t.Errorf("GetHealth = %q, want %q", got, HealthStatusUnchecked)
	}
}

func TestDriverRegistry_HealthStatuses_Sorted(t *testing.T) {
	t.Parallel()
	r := NewDriverRegistry()
	_ = r.Register("cursor", NewCursorCliDriver())
	_ = r.Register("claude", NewClaudeCliDriver())
	_ = r.Register("ollama", NewOpenAIDriver("ollama", WithOpenAIBaseURL("http://localhost:11434/v1")))

	r.SetHealth("ollama", HealthStatusHealthy)

	statuses := r.HealthStatuses()
	if len(statuses) != 3 {
		t.Fatalf("expected 3 statuses, got %d", len(statuses))
	}

	// Verify sorted by name
	expectedNames := []string{"claude", "cursor", "ollama"}
	for i, s := range statuses {
		if s.Name != expectedNames[i] {
			t.Errorf("statuses[%d].Name = %q, want %q", i, s.Name, expectedNames[i])
		}
	}

	// Verify driver types
	if statuses[0].Driver != DriverClaudeCLI {
		t.Errorf("claude driver = %q, want %q", statuses[0].Driver, DriverClaudeCLI)
	}
	if statuses[1].Driver != DriverCursorCLI {
		t.Errorf("cursor driver = %q, want %q", statuses[1].Driver, DriverCursorCLI)
	}
	if statuses[2].Driver != DriverOpenAI {
		t.Errorf("ollama driver = %q, want %q", statuses[2].Driver, DriverOpenAI)
	}

	// Verify health statuses
	if statuses[0].Health != HealthStatusUnchecked {
		t.Errorf("claude health = %q, want %q", statuses[0].Health, HealthStatusUnchecked)
	}
	if statuses[2].Health != HealthStatusHealthy {
		t.Errorf("ollama health = %q, want %q", statuses[2].Health, HealthStatusHealthy)
	}
}
