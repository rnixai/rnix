package llm

import (
	"net/http"
	"strings"
	"testing"
	"time"

	anthropic "github.com/anthropics/anthropic-sdk-go"
	openai "github.com/openai/openai-go/v3"
)

// Story 73.2 — AC1 (Retry-After three-form parsing) and AC3 (body wait
// parsing). All fixtures use injected clocks (AC9: now is a function
// parameter, no package-level time seam needed on the parse side).
//
// Fixture provenance ([[observability-data-provenance-principle]]):
//   - the 343831-second Retry-After value, the "reset at 08-05 22:23:00 UTC"
//     body and the "try again after 6 seconds" body are VERBATIM production
//     captures (see 73.1's provenance note); the sample instant
//     2026-08-01T22:52:29Z is the capture time recorded in the dossier.
//   - the HTTP-date header fixtures are CONSTRUCTED — this repo has no
//     captured date-form Retry-After.

var captureInstant = time.Date(2026, 8, 1, 22, 52, 29, 0, time.UTC)

func hdr(pairs ...string) http.Header {
	h := http.Header{}
	for i := 0; i+1 < len(pairs); i += 2 {
		h.Set(pairs[i], pairs[i+1])
	}
	return h
}

// TestATDD_73_2_AC1_ParseRetryAfterThreeForms covers the three wire shapes in
// order (opencode's form set — do not reinvent).
func TestATDD_73_2_AC1_ParseRetryAfterThreeForms(t *testing.T) {
	cases := []struct {
		name string
		h    http.Header
		want time.Duration
	}{
		{"retry-after-ms milliseconds", hdr("retry-after-ms", "1500"), 1500 * time.Millisecond},
		{"retry-after integer seconds", hdr("retry-after", "6"), 6 * time.Second},
		// CONSTRUCTED fixture (no capture): HTTP date relative to the
		// injected now. The date is the captured reset instant, so the delta
		// reproduces the captured 343831 seconds exactly.
		{"retry-after HTTP date relative", hdr("retry-after", "Wed, 05 Aug 2026 22:23:00 GMT"), 343831 * time.Second},
		// 🔴 Production-captured value. This case pins the SECONDS reading:
		// interpreting it as milliseconds would yield 5.7 minutes instead of
		// the 95.5 hours the provider actually meant (cross-verified against
		// the body's self-declared reset instant).
		{"captured 343831 stays seconds", hdr("retry-after", "343831"), 343831 * time.Second},
		// retry-after-ms takes precedence when both headers are present.
		{"ms header wins over seconds header", hdr("retry-after-ms", "1500", "retry-after", "6"), 1500 * time.Millisecond},
		// Header name matching is case-insensitive (http.Header.Get
		// canonicalises).
		{"case-insensitive header name", hdr("Retry-After", "6"), 6 * time.Second},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseRetryAfter(tc.h, captureInstant)
			if !ok {
				t.Fatalf("parseRetryAfter(%v) = (_, false), want (%v, true)", tc.h, tc.want)
			}
			if got != tc.want {
				t.Fatalf("parseRetryAfter(%v) = %v, want %v", tc.h, got, tc.want)
			}
		})
	}
}

// TestATDD_73_2_AC1_NegativeAndGarbageValues: malformed input must fail-safe
// to (0, false) — NEVER a negative duration (time.After fires immediately on
// negatives, silently converting backoff into zero-delay hammering).
func TestATDD_73_2_AC1_NegativeAndGarbageValues(t *testing.T) {
	cases := []struct {
		name string
		h    http.Header
	}{
		{"negative seconds", hdr("retry-after", "-5")},
		{"zero seconds", hdr("retry-after", "0")},
		{"garbage text", hdr("retry-after", "abc")},
		{"negative ms", hdr("retry-after-ms", "-100")},
		{"zero ms", hdr("retry-after-ms", "0")},
		{"garbage ms", hdr("retry-after-ms", "soon")},
		{"empty values", hdr("retry-after", "", "retry-after-ms", "")},
		{"no headers at all", http.Header{}},
		{"nil header map", nil},
		{"date in the past", hdr("retry-after", "Fri, 01 Aug 2026 00:00:00 GMT")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseRetryAfter(tc.h, captureInstant)
			if ok {
				t.Fatalf("parseRetryAfter(%v) = (%v, true), want (0, false)", tc.h, got)
			}
			if got != 0 {
				t.Fatalf("parseRetryAfter(%v) returned non-zero duration %v on failure", tc.h, got)
			}
			if got < 0 {
				t.Fatalf("parseRetryAfter(%v) returned NEGATIVE duration %v — would fire time.After immediately", tc.h, got)
			}
		})
	}
}

// TestATDD_73_2_AC3_BodyFixtures parses the two captured body shapes.
func TestATDD_73_2_AC3_BodyFixtures(t *testing.T) {
	// Captured qwen shape (terminal quota): absolute reset instant.
	ra, resetAt, ok := parseWaitFromBody(quotaBodyFixture, captureInstant)
	if !ok {
		t.Fatalf("parseWaitFromBody(quota fixture) = (_, _, false), want a reset instant")
	}
	if ra != 0 {
		t.Errorf("quota fixture retryAfter = %v, want 0 (absolute form carries no relative wait)", ra)
	}
	want := time.Date(2026, 8, 5, 22, 23, 0, 0, time.UTC)
	if !resetAt.Equal(want) {
		t.Errorf("quota fixture resetAt = %v, want %v", resetAt, want)
	}

	// Captured infron shape (retryable throttle): relative seconds. The epic
	// said "after 4 seconds"; the capture says 6 — the parser matches the
	// template, never the number.
	ra, resetAt, ok = parseWaitFromBody(throttleBodyFixture, captureInstant)
	if !ok {
		t.Fatalf("parseWaitFromBody(throttle fixture) = (_, _, false), want a relative wait")
	}
	if ra != 6*time.Second {
		t.Errorf("throttle fixture retryAfter = %v, want 6s", ra)
	}
	if !resetAt.IsZero() {
		t.Errorf("throttle fixture resetAt = %v, want zero", resetAt)
	}

	// Unit variants the provider wording produces.
	for body, want := range map[string]time.Duration{
		"Please try again after 1 second.":1 * time.Second,
		"Please try again after 90 s.":      90 * time.Second,
		"Please try again after 2 minutes.": 2 * time.Minute,
		"Please try again after 1 minute.":  1 * time.Minute,
		"Please try again after 5 m.":       5 * time.Minute,
	} {
		ra, _, ok := parseWaitFromBody(body, captureInstant)
		if !ok || ra != want {
			t.Errorf("parseWaitFromBody(%q) = (%v, _, %v), want (%v, _, true)", body, ra, ok, want)
		}
	}

	// Fail-safe: unrecognised bodies must NOT return ok with a zero wait
	// (a caller could read that as "the server said no wait is needed").
	if _, _, ok := parseWaitFromBody("something went sideways", captureInstant); ok {
		t.Error("unrecognised body must return ok=false")
	}
	if _, _, ok := parseWaitFromBody("", captureInstant); ok {
		t.Error("empty body must return ok=false")
	}
	if ra, _, ok := parseWaitFromBody("try again after 0 seconds", captureInstant); ok || ra != 0 {
		t.Errorf("zero-count body = (%v, _, %v), want (0, _, false)", ra, ok)
	}
}

// TestATDD_73_2_AC3_YearRollover 🔴: the captured reset shape carries no
// year. Seeing "01-02" on Dec 31 must resolve to NEXT year, not a past
// instant in the current one.
func TestATDD_73_2_AC3_YearRollover(t *testing.T) {
	now := time.Date(2026, 12, 31, 12, 0, 0, 0, time.UTC)
	_, resetAt, ok := parseWaitFromBody("The quota will reset at 01-02 10:00:00 UTC.", now)
	if !ok {
		t.Fatal("parseWaitFromBody = (_, _, false), want a reset instant")
	}
	want := time.Date(2027, 1, 2, 10, 0, 0, 0, time.UTC)
	if !resetAt.Equal(want) {
		t.Fatalf("resetAt = %v, want %v (year must roll over)", resetAt, want)
	}

	// Sanity within the same year: August seen from January stays this year.
	now = time.Date(2026, 1, 15, 0, 0, 0, 0, time.UTC)
	_, resetAt, ok = parseWaitFromBody("The quota will reset at 08-05 22:23:00 UTC.", now)
	if !ok {
		t.Fatal("same-year case failed to parse")
	}
	if want := time.Date(2026, 8, 5, 22, 23, 0, 0, time.UTC); !resetAt.Equal(want) {
		t.Fatalf("resetAt = %v, want %v", resetAt, want)
	}
}

// TestATDD_73_2_ParseRobustness (review 2026-08-03 patches): overflow
// saturation, ms-header fall-through, timezone requirement, and calendar
// validation. Each case pins a guard that a silent wrap/normalization would
// otherwise defeat.
func TestATDD_73_2_ParseRobustness(t *testing.T) {
	// Overflow saturates instead of wrapping: a ~584-year declaration fits
	// int64 but NOT time.Duration once multiplied. A wrap would credit it as
	// a sub-second wait and retry almost immediately; saturation lands it in
	// the kernel's give-up branch (wait > maxInProcessWait).
	huge := hdr("retry-after", "18446744074") // ≈584 years in seconds
	if d, ok := parseRetryAfter(huge, captureInstant); !ok || d < 365*24*time.Hour {
		t.Errorf("oversized seconds header = (%v, %v), want a saturated multi-year duration", d, ok)
	}
	hugeMs := hdr("retry-after-ms", "18446744073710") // ≈584 years in ms
	if d, ok := parseRetryAfter(hugeMs, captureInstant); !ok || d < 365*24*time.Hour {
		t.Errorf("oversized ms header = (%v, %v), want a saturated duration", d, ok)
	}
	if ra, _, ok := parseWaitFromBody("Please try again after 9999999999999 seconds.", captureInstant); !ok || ra < 365*24*time.Hour {
		t.Errorf("oversized body count = (%v, %v), want a saturated duration", ra, ok)
	}

	// A garbage retry-after-ms header falls through to retry-after instead of
	// shadowing the origin's valid seconds header.
	if d, ok := parseRetryAfter(hdr("retry-after-ms", "soon", "retry-after", "30"), captureInstant); !ok || d != 30*time.Second {
		t.Errorf("garbage ms + valid seconds = (%v, %v), want (30s, true)", d, ok)
	}

	// The reset instant requires a UTC-equivalent suffix: the parser builds
	// time.UTC, so missing or foreign zones fail safe instead of guessing.
	for _, body := range []string{
		"The quota will reset at 08-05 22:23:00.",        // no suffix
		"The quota will reset at 08-05 22:23:00 +08:00.", // foreign offset
	} {
		if _, _, ok := parseWaitFromBody(body, captureInstant); ok {
			t.Errorf("body %q parsed, want ok=false (no UTC-equivalent suffix)", body)
		}
	}

	// Impossible calendar dates fail safe instead of normalizing (02-31 would
	// silently become 03-03 and wait for an instant never declared).
	if _, _, ok := parseWaitFromBody("The quota will reset at 02-31 10:00:00 UTC.", captureInstant); ok {
		t.Error("impossible date 02-31 parsed, want ok=false")
	}

	// An instant exactly equal to now is returned as-is, not rolled a year
	// forward (the kernel's time.Until ≤ 0 handling degrades it to local
	// backoff — the window is resetting NOW; a year roll would give up).
	now := time.Date(2026, 8, 5, 22, 23, 0, 0, time.UTC)
	_, resetAt, ok := parseWaitFromBody("The quota will reset at 08-05 22:23:00 UTC.", now)
	if !ok || !resetAt.Equal(now) {
		t.Errorf("exact-equality instant = (%v, %v), want (%v, true)", resetAt, ok, now)
	}
}

// TestATDD_73_2_D2_HeaderPrecedence: when BOTH channels carry a wait and the
// values differ, the header wins — but Source must record the channel
// actually credited ([[observability-data-provenance-principle]]), and a
// body-only parse reports "body" even though headers had priority.
func TestATDD_73_2_D2_HeaderPrecedence(t *testing.T) {
	body := throttleBodyFixture // carries "after 6 seconds"
	h := hdr("retry-after", "30")

	ra, resetAt, source, ok := resolveRateLimitWait(h, body, captureInstant)
	if !ok {
		t.Fatal("resolveRateLimitWait = (_, _, _, false), want a wait")
	}
	if ra != 30*time.Second {
		t.Errorf("retryAfter = %v, want 30s (header value, not the body's 6s)", ra)
	}
	if !resetAt.IsZero() {
		t.Errorf("resetAt = %v, want zero for the header path", resetAt)
	}
	if source != "header" {
		t.Errorf("source = %q, want %q", source, "header")
	}

	// Header present but unparseable → body is credited.
	ra, _, source, ok = resolveRateLimitWait(hdr("retry-after", "abc"), body, captureInstant)
	if !ok || ra != 6*time.Second || source != "body" {
		t.Errorf("garbage header: got (%v, %q, %v), want (6s, body, true)", ra, source, ok)
	}

	// No header at all (the gemini shape — headers unreachable) → body.
	ra, _, source, ok = resolveRateLimitWait(nil, body, captureInstant)
	if !ok || ra != 6*time.Second || source != "body" {
		t.Errorf("nil header: got (%v, %q, %v), want (6s, body, true)", ra, source, ok)
	}

	// Neither channel → fail through to local backoff at the caller.
	if _, _, source, ok = resolveRateLimitWait(nil, "nothing useful", captureInstant); ok || source != "" {
		t.Errorf("no evidence: got (%q, %v), want (_, \"\", false)", source, ok)
	}
}

// --- Story 73.1 code-review deferrals, closed here (story §9) ---
//
// Both entries were filed as "harden when 73.2 extends this function family";
// 73.2 extends exactly that family, so they land with it.

// TestATDD_73_2_ContradictoryEvidenceTerminalFirst closes deferral 1.
// The HTTP-side terminal-first ordering was a deliberate decision with no test
// pinning it: a body that reads like a retryable throttle carried by a
// structured error.type saying the quota is spent must let quota win.
func TestATDD_73_2_ContradictoryEvidenceTerminalFirst(t *testing.T) {
	// Body evidence says retryable; structured type says terminal.
	const throttleBody = "Rate limit exceeded (5 requests per minute). Please try again after 6 seconds."
	if got := classifyRateLimitBody(throttleBody, "insufficient_quota"); got != KindQuota {
		t.Fatalf("contradictory evidence classified %v, want %v — structured terminal type must beat body throttle evidence (HTTP terminal-first)", got, KindQuota)
	}

	// Driver-level propagation: the unified openai driver (Story 75.3) renders
	// the SDK's typed error as text that carries BOTH the structured type and
	// the body evidence (JSON bodies are surfaced verbatim in err.Error()), so
	// the terminal-first ordering must let quota win — the same contradiction
	// the compat driver used to prove, now through the SDK path.
	d, _, cleanup := newTestOpenAIDriver(func(w http.ResponseWriter, _ *http.Request) {
		// 🔴 Header BEFORE WriteHeader: net/http snapshots the header map at
		// WriteHeader, so setting it afterwards never reaches the client
		// (review 2026-08-03: the original ordering made this E2E verify
		// nothing about header propagation).
		w.Header().Set("retry-after", "6")
		w.WriteHeader(429)
		_, _ = w.Write([]byte(`{"error":{"message":"Rate limit exceeded (5 requests per minute). Please try again after 6 seconds.","type":"insufficient_quota"}}`))
	})
	defer cleanup()
	_, err := d.Call(t.Context(), LLMRequest{Intent: "hi"})
	assertKind(t, err, KindQuota)

	// The header's wait actually propagated through a real HTTP round-trip —
	// the first E2E evidence of AC2's header wiring (review 2026-08-03):
	// kind comes from the structured type, the wait from the header, and
	// Source records the channel actually credited.
	ra, _, source, ok := RateLimitWaitOf(err)
	if !ok || ra != 6*time.Second || source != "header" {
		t.Errorf("RateLimitWaitOf = (%v, %q, %v), want (6s, %q, true)", ra, source, ok, "header")
	}
}

// TestATDD_73_2_AC2_NilResponseNoPanic 🔴 (AC9 item 6): the SDKs can yield
// errors with nil Response/Request on some construction paths, and the SDK's
// own Error() dereferences BOTH (internal/apierror). classifyError must
// classify without panicking and the wait fields must stay honestly empty.
// This is the test whose absence let the openai_official guard-order bug
// ship: the guard ran AFTER msg := err.Error(), making it dead code for the
// very shape it defends against (review 2026-08-03 patch P1/P2).
func TestATDD_73_2_AC2_NilResponseNoPanic(t *testing.T) {
	t.Run("anthropic", func(t *testing.T) {
		d := NewAnthropicDriver("anthropic-nil-test", WithAnthropicKey("k"))
		err := d.classifyError(&anthropic.Error{StatusCode: 429}) // Response & Request nil
		assertKind(t, err, KindThrottle) // fail-open classification completes
		if _, _, source, ok := RateLimitWaitOf(err); ok || source != "" {
			t.Errorf("RateLimitWaitOf = (_, %q, %v), want no wait information (nothing to parse)", source, ok)
		}
	})
	t.Run("openai_official", func(t *testing.T) {
		d := NewOpenAIDriver("openai-nil-test", WithOpenAIKey("k"))
		err := d.classifyError(&openai.Error{StatusCode: 429}) // Response & Request nil
		assertKind(t, err, KindThrottle)
		if _, _, source, ok := RateLimitWaitOf(err); ok || source != "" {
			t.Errorf("RateLimitWaitOf = (_, %q, %v), want no wait information (nothing to parse)", source, ok)
		}
	})
}

// TestATDD_73_2_MarkerSetsAreNotSubstrings closes deferral 2. The two marker
// sets are shared by two classifiers that scan them in OPPOSITE orders
// (HTTP terminal-first, CLI retryable-first). That only stays coherent while
// no entry of one set is a substring of an entry of the other: a throttle
// marker contained in a quota marker would make the CLI classifier claim
// quota bodies as throttle, and vice versa. 73.1 guarded only the four
// banned literals; this makes the implicit contract explicit and symmetric.
func TestATDD_73_2_MarkerSetsAreNotSubstrings(t *testing.T) {
	for _, q := range rateLimitQuotaMarkers {
		for _, th := range rateLimitThrottleMarkers {
			if strings.Contains(q, th) {
				t.Errorf("quota marker %q contains throttle marker %q — CLI's retryable-first scan would misclassify quota bodies as throttle", q, th)
			}
			if strings.Contains(th, q) {
				t.Errorf("throttle marker %q contains quota marker %q — HTTP's terminal-first scan would misclassify throttle bodies as quota", th, q)
			}
		}
	}
}
