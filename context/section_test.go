package context

import (
	"sync"
	"sync/atomic"
	"testing"
)

func TestSectionRegistry_Build_Order(t *testing.T) {
	r := NewSectionRegistry()
	r.Register("a", func() string { return "AAA" }, true)
	r.Register("b", func() string { return "BBB" }, false)
	r.Register("c", func() string { return "CCC" }, true)

	got := r.Build()
	want := "AAA\n\nBBB\n\nCCC"
	if got != want {
		t.Errorf("Build() = %q, want %q", got, want)
	}
}

func TestSectionRegistry_Build_SkipsEmpty(t *testing.T) {
	r := NewSectionRegistry()
	r.Register("a", func() string { return "AAA" }, true)
	r.Register("empty", func() string { return "" }, false)
	r.Register("c", func() string { return "CCC" }, true)

	got := r.Build()
	want := "AAA\n\nCCC"
	if got != want {
		t.Errorf("Build() = %q, want %q", got, want)
	}
}

func TestSectionRegistry_Build_AllEmpty(t *testing.T) {
	r := NewSectionRegistry()
	r.Register("a", func() string { return "" }, true)
	r.Register("b", func() string { return "" }, false)

	got := r.Build()
	if got != "" {
		t.Errorf("Build() = %q, want empty", got)
	}
}

func TestSectionRegistry_CachedSection_ComputesOnce(t *testing.T) {
	var callCount atomic.Int32
	r := NewSectionRegistry()
	r.Register("cached", func() string {
		callCount.Add(1)
		return "cached-value"
	}, true)

	// Build multiple times
	r.Build()
	r.Build()
	r.Build()

	if got := callCount.Load(); got != 1 {
		t.Errorf("cached computeFn called %d times, want 1", got)
	}
}

func TestSectionRegistry_DynamicSection_ComputesEveryTime(t *testing.T) {
	var callCount atomic.Int32
	r := NewSectionRegistry()
	r.Register("dynamic", func() string {
		callCount.Add(1)
		return "dynamic-value"
	}, false)

	r.Build()
	r.Build()
	r.Build()

	if got := callCount.Load(); got != 3 {
		t.Errorf("dynamic computeFn called %d times, want 3", got)
	}
}

func TestSectionRegistry_Invalidate_ResetsCachedSections(t *testing.T) {
	var callCount atomic.Int32
	r := NewSectionRegistry()
	r.Register("cached", func() string {
		callCount.Add(1)
		return "value"
	}, true)

	r.Build()
	if got := callCount.Load(); got != 1 {
		t.Fatalf("expected 1 call before invalidate, got %d", got)
	}

	r.Invalidate()
	r.Build()
	if got := callCount.Load(); got != 2 {
		t.Errorf("expected 2 calls after invalidate+build, got %d", got)
	}
}

func TestSectionRegistry_Invalidate_DoesNotAffectDynamic(t *testing.T) {
	var dynamicCount atomic.Int32
	r := NewSectionRegistry()
	r.Register("dynamic", func() string {
		dynamicCount.Add(1)
		return "dynamic"
	}, false)

	r.Build()
	r.Invalidate()
	r.Build()

	if got := dynamicCount.Load(); got != 2 {
		t.Errorf("dynamic computeFn called %d times, want 2", got)
	}
}

func TestSectionRegistry_Len(t *testing.T) {
	r := NewSectionRegistry()
	if r.Len() != 0 {
		t.Errorf("Len() = %d, want 0", r.Len())
	}
	r.Register("a", func() string { return "" }, true)
	r.Register("b", func() string { return "" }, false)
	if r.Len() != 2 {
		t.Errorf("Len() = %d, want 2", r.Len())
	}
}

func TestSectionRegistry_ConcurrentBuild(t *testing.T) {
	r := NewSectionRegistry()
	r.Register("s1", func() string { return "A" }, true)
	r.Register("s2", func() string { return "B" }, false)

	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			result := r.Build()
			if result != "A\n\nB" {
				t.Errorf("concurrent Build() = %q", result)
			}
		})
	}
	wg.Wait()
}

func TestSectionRegistry_Empty(t *testing.T) {
	r := NewSectionRegistry()
	got := r.Build()
	if got != "" {
		t.Errorf("Build() on empty registry = %q, want empty", got)
	}
}

func TestLoadSection_ReturnsContent(t *testing.T) {
	got := LoadSection("intro")
	if got == "" {
		t.Fatal("LoadSection(intro) returned empty")
	}
	if !containsSubstring(got, "Rnix") {
		t.Errorf("LoadSection(intro) does not contain 'Rnix': %q", got[:min(80, len(got))])
	}
}

func TestLoadSection_UnknownReturnsEmpty(t *testing.T) {
	got := LoadSection("nonexistent_section_xyz")
	if got != "" {
		t.Errorf("LoadSection(nonexistent) = %q, want empty", got)
	}
}

func TestLoadSection_AllStandard(t *testing.T) {
	names := []string{"intro", "system_rules", "doing_tasks", "actions", "using_devices", "tone_style", "output_efficiency"}
	for _, name := range names {
		got := LoadSection(name)
		if got == "" {
			t.Errorf("LoadSection(%q) returned empty", name)
		}
	}
}

func TestBuildPrompt_WithSections(t *testing.T) {
	mgr := NewManager()
	cid, err := mgr.CtxAlloc(100)
	if err != nil {
		t.Fatal(err)
	}
	// Set legacy prompt (should be overridden by sections)
	_ = mgr.SetSystemPrompt(cid, "legacy prompt")

	sections := NewSectionRegistry()
	sections.Register("sec1", func() string { return "SECTION_1" }, true)
	sections.Register("sec2", func() string { return "SECTION_2" }, false)
	_ = mgr.SetSections(cid, sections)

	result, err := mgr.BuildPrompt(cid)
	if err != nil {
		t.Fatal(err)
	}
	want := "SECTION_1\n\nSECTION_2"
	if result.SystemPrompt != want {
		t.Errorf("BuildPrompt() SystemPrompt = %q, want %q", result.SystemPrompt, want)
	}
}

func TestBuildPrompt_WithoutSections_FallsBackToLegacy(t *testing.T) {
	mgr := NewManager()
	cid, err := mgr.CtxAlloc(100)
	if err != nil {
		t.Fatal(err)
	}
	_ = mgr.SetSystemPrompt(cid, "legacy prompt text")

	result, err := mgr.BuildPrompt(cid)
	if err != nil {
		t.Fatal(err)
	}
	if result.SystemPrompt != "legacy prompt text" {
		t.Errorf("BuildPrompt() SystemPrompt = %q, want %q", result.SystemPrompt, "legacy prompt text")
	}
}

func TestInvalidateSections_ViaManager(t *testing.T) {
	mgr := NewManager()
	cid, err := mgr.CtxAlloc(100)
	if err != nil {
		t.Fatal(err)
	}
	var callCount atomic.Int32
	sections := NewSectionRegistry()
	sections.Register("cached", func() string {
		callCount.Add(1)
		return "val"
	}, true)
	_ = mgr.SetSections(cid, sections)

	_, _ = mgr.BuildPrompt(cid)
	if callCount.Load() != 1 {
		t.Fatalf("expected 1 call, got %d", callCount.Load())
	}

	_ = mgr.InvalidateSections(cid)
	_, _ = mgr.BuildPrompt(cid)
	if callCount.Load() != 2 {
		t.Errorf("expected 2 calls after invalidate, got %d", callCount.Load())
	}
}

func TestInvalidateSections_NoopWithoutSections(t *testing.T) {
	mgr := NewManager()
	cid, err := mgr.CtxAlloc(100)
	if err != nil {
		t.Fatal(err)
	}
	// Should not panic or error
	if err := mgr.InvalidateSections(cid); err != nil {
		t.Errorf("InvalidateSections on context without sections: %v", err)
	}
}

func TestTokenUsage_WithSections(t *testing.T) {
	mgr := NewManager()
	cid, err := mgr.CtxAlloc(100)
	if err != nil {
		t.Fatal(err)
	}
	_ = mgr.SetSystemPrompt(cid, "short")
	sections := NewSectionRegistry()
	sections.Register("big", func() string { return "this is a much longer system prompt from sections" }, true)
	_ = mgr.SetSections(cid, sections)

	stats, err := mgr.TokenUsage(cid)
	if err != nil {
		t.Fatal(err)
	}
	// Sections-based token count should be higher than legacy "short" prompt
	if stats.Used < 5 {
		t.Errorf("TokenUsage with sections should reflect section content, got %d", stats.Used)
	}
}

func containsSubstring(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && findSubstring(s, sub))
}

func findSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
