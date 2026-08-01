package kernel

import (
	"regexp"
	"strings"
	"testing"
	"time"

	rnixctx "github.com/rnixai/rnix/context"
)

// Story 69.1: backpressure warnings must not carry per-step varying numbers,
// because every byte change in the system prompt invalidates the provider's
// prompt cache prefix (measured: 99.5% → 8.9% hit rate, ~150k tokens recomputed
// per step, main call latency 7.4s → 48.7s median, compact 30s timeout always
// blown → slots never reclaimed → ErrContextFull → hang).

// --- AC1/AC2: pure tier classification ---

func TestATDD_69_1_AC1_BackpressureTier_Boundaries(t *testing.T) {
	tests := []struct {
		name      string
		slotPct   float64
		threshold float64
		want      string
	}{
		// Default threshold 70 → critical boundary 85.
		{"below default threshold", 50, 70, ""},
		{"exactly at default threshold (> not >=)", 70, 70, ""},
		{"just above default threshold", 70.1, 70, backpressureTierElevated},
		{"mid elevated", 80, 70, backpressureTierElevated},
		{"exactly at critical boundary (> not >=)", 85, 70, backpressureTierElevated},
		{"just above critical boundary", 85.1, 70, backpressureTierCritical},
		{"full", 100, 70, backpressureTierCritical},
		// Custom threshold 50 → critical boundary evenly splits headroom at 75.
		{"custom below", 50, 50, ""},
		{"custom elevated", 60, 50, backpressureTierElevated},
		{"custom at critical boundary", 75, 50, backpressureTierElevated},
		{"custom critical", 75.1, 50, backpressureTierCritical},
		// High custom threshold must not invert against a hardcoded 85.
		{"high threshold elevated", 92, 90, backpressureTierElevated},
		{"high threshold critical", 95.1, 90, backpressureTierCritical},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := backpressureTier(tt.slotPct, tt.threshold); got != tt.want {
				t.Errorf("backpressureTier(%v, %v) = %q, want %q", tt.slotPct, tt.threshold, got, tt.want)
			}
		})
	}
}

func TestATDD_69_1_AC2_BackpressureText_Tiers(t *testing.T) {
	elevated := backpressureText(backpressureTierElevated)
	critical := backpressureText(backpressureTierCritical)

	if elevated == "" {
		t.Fatal("elevated tier text must not be empty")
	}
	if critical == "" {
		t.Fatal("critical tier text must not be empty")
	}
	if elevated == critical {
		t.Error("elevated and critical text must differ (tiers must not degenerate to one constant)")
	}
	for _, tc := range []struct {
		tier string
		text string
	}{{"elevated", elevated}, {"critical", critical}} {
		if !strings.Contains(tc.text, "Context Resource Warning") {
			t.Errorf("%s text lost the section heading anchor", tc.tier)
		}
	}
	// AC2: elevated keeps the original behavioural guidance semantics.
	if !strings.Contains(elevated, "sequential") || !strings.Contains(elevated, "parallel") {
		t.Errorf("elevated text must preserve the sequential-over-parallel guidance, got:\n%s", elevated)
	}
	if !strings.Contains(elevated, "2 tool calls") {
		t.Errorf("elevated text must preserve the max-2-tool-calls-per-turn guidance, got:\n%s", elevated)
	}
	// AC2: critical tightens to a single tool call and echoes the frc promise.
	if !strings.Contains(critical, "single tool call") {
		t.Errorf("critical text must converge to a single tool call, got:\n%s", critical)
	}
	if !strings.Contains(strings.ToLower(critical), "write down") {
		t.Errorf("critical text must tell the model to write important information into its response, got:\n%s", critical)
	}

	// Unknown / empty tiers produce no section.
	for _, tier := range []string{"", "unknown", "ELEVATED"} {
		if got := backpressureText(tier); got != "" {
			t.Errorf("backpressureText(%q) = %q, want empty", tier, got)
		}
	}
}

// --- AC6-①/②: byte-identical within a tier, different across tiers ---

// fillTo tops the context up to exactly total messages.
func fillTo(t *testing.T, ctxMgr *rnixctx.Manager, proc *Process, total int) {
	t.Helper()
	used, _, err := ctxMgr.SlotUsage(proc.CtxID)
	if err != nil {
		t.Fatalf("SlotUsage: %v", err)
	}
	// Code review P3: fail loud instead of silently no-op'ing when the context
	// already holds more messages than requested — a no-op here would let the
	// caller assert on a slot percentage that never materialized.
	if used > total {
		t.Fatalf("fillTo: context already has %d messages, more than requested total %d", used, total)
	}
	for i := used; i < total; i++ {
		role := rnixctx.RoleUser
		if i%2 == 1 {
			role = rnixctx.RoleAssistant
		}
		if err := ctxMgr.AppendMessage(proc.CtxID, role, "x"); err != nil {
			t.Fatalf("AppendMessage #%d: %v", i, err)
		}
	}
}

func TestATDD_69_1_AC6_SystemPromptByteIdenticalWithinTier(t *testing.T) {
	k, _, proc := setupBackpressureKernel(t, 256)
	sections := registerSections(proc, k, "")

	// Story 71.1 AC2 moved the pressure axis from slots to tokens. The property
	// under test is unchanged: within one tier the system prompt must not change
	// by a single byte, because that byte invalidates the provider's cache prefix.
	// Three points, all inside the elevated tier (70% < pct <= 85%).
	setBackpressurePct(proc, 70.7)
	first := sections.Build()
	setBackpressurePct(proc, 71.5)
	second := sections.Build()
	setBackpressurePct(proc, 78.1)
	third := sections.Build()

	if !strings.Contains(first, "Context Resource Warning") {
		t.Fatalf("expected backpressure section at 70.7%% token usage, got:\n%s", first)
	}
	if first != second {
		t.Errorf("system prompt changed between 70.7%% and 71.5%% within the same tier\n--- first ---\n%s\n--- second ---\n%s", first, second)
	}
	if second != third {
		t.Errorf("system prompt changed between 71.5%% and 78.1%% within the same tier\n--- second ---\n%s\n--- third ---\n%s", second, third)
	}

	// AC6-②: crossing into critical must change the body (tiers still carry meaning).
	setBackpressurePct(proc, 85.9)
	critical := sections.Build()
	if critical == third {
		t.Error("system prompt did not change when crossing from elevated into critical tier")
	}
	if !strings.Contains(critical, "Context Resource Warning") {
		t.Errorf("critical tier lost the backpressure section, got:\n%s", critical)
	}
}

// --- AC6-③: regression guard — no per-step slot numbers in the body ---

func TestATDD_69_1_AC6_BackpressureTextHasNoSlotNumbers(t *testing.T) {
	// Guard against the specific slot literals the old implementation emitted.
	// Note "2 tool calls" legitimately contains a digit, so the guard targets
	// concrete slot counts / limits / percentages rather than "any digit".
	forbidden := []string{"181", "183", "200", "220", "256", "181/256", "71%", "78%", "80%", "85%"}
	// Pattern guard (code review P2): the literal list above only blocks the old
	// implementation's known numbers. Regexps also catch future re-introductions
	// it cannot know about — any "N%" or "N/M" emission. Both tier bodies are free
	// of % and fraction patterns today; the legitimate constant "2 tool calls"
	// matches neither pattern.
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`\d+%`),
		regexp.MustCompile(`\d+\s*/\s*\d+`),
	}
	for _, tier := range []string{backpressureTierElevated, backpressureTierCritical} {
		text := backpressureText(tier)
		for _, f := range forbidden {
			if strings.Contains(text, f) {
				t.Errorf("tier %q body contains dynamic slot literal %q:\n%s", tier, f, text)
			}
		}
		for _, re := range patterns {
			if m := re.FindString(text); m != "" {
				t.Errorf("tier %q body matches numeric pattern /%s/ (%q):\n%s", tier, re, m, text)
			}
		}
	}
}

func TestATDD_69_1_AC6_BuiltPromptHasNoSlotNumbers(t *testing.T) {
	k, _, proc := setupBackpressureKernel(t, 256)
	sections := registerSections(proc, k, "")
	setBackpressurePct(proc, 70.7)

	result := sections.Build()
	// Story 71.1: the axis moved to tokens, so the literals to guard against are
	// the token figures (LastInputTokens 63630 of a 90000 budget) alongside the
	// legacy slot ones — a re-introduction on either axis must fail here.
	for _, f := range []string{"181", "256", "181/256", "71%", "70.7", "63630", "90000"} {
		if strings.Contains(result, f) {
			t.Errorf("built system prompt leaks dynamic usage literal %q:\n%s", f, result)
		}
	}
}

// --- AC6-④: env_info stability (gdb map ordering + frozen Date) ---

func TestATDD_69_1_AC6_EnvInfoGdbVarsStableAcrossBuilds(t *testing.T) {
	k, _, proc := setupBackpressureKernel(t, 256)
	proc.SetGdbEnv("ALPHA", "1")
	proc.SetGdbEnv("BRAVO", "2")
	proc.SetGdbEnv("CHARLIE", "3")

	sections := registerSections(proc, k, "")
	baseline := sections.Build()
	if !strings.Contains(baseline, "Debug Environment Variables") {
		t.Fatalf("expected gdb env vars in env_info, got:\n%s", baseline)
	}
	for i := range 30 {
		if got := sections.Build(); got != baseline {
			t.Fatalf("env_info output changed on build #%d (map iteration order leaked)\n--- baseline ---\n%s\n--- got ---\n%s", i, baseline, got)
		}
	}
}

func TestATDD_69_1_AC6_EnvInfoDateFrozenAtRegistration(t *testing.T) {
	orig := nowFn
	t.Cleanup(func() { nowFn = orig })

	fixed := time.Date(2026, 1, 15, 10, 0, 0, 0, time.UTC)
	nowFn = func() time.Time { return fixed }

	k, _, proc := setupBackpressureKernel(t, 256)
	sections := registerSections(proc, k, "")

	first := sections.Build()
	if !strings.Contains(first, "Date: 2026-01-15") {
		t.Fatalf("expected registration-time date in env_info, got:\n%s", first)
	}

	// Advance the injected clock across a day boundary: the frozen snapshot must hold.
	nowFn = func() time.Time { return fixed.Add(48 * time.Hour) }
	second := sections.Build()
	if second != first {
		t.Errorf("env_info changed after the clock advanced (Date must be frozen at registration)\n--- first ---\n%s\n--- second ---\n%s", first, second)
	}
	if strings.Contains(second, "2026-01-17") {
		t.Error("env_info re-read the clock instead of using the frozen registration-time date")
	}
}
