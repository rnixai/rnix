package llm

import (
	"net/http"
	"strings"
	"testing"
	"time"
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
// openai_compat is the only driver feeding BOTH channels of
// classifyRateLimitBody (message + structured Code/Type); 73.1's tests only
// covered agreeing evidence. When they CONTRADICT — a body that reads like a
// retryable throttle carried by an error.type that says the quota is spent —
// the HTTP-side terminal-first ordering must let quota win. That precedence
// was a deliberate decision with no test pinning it.
func TestATDD_73_2_ContradictoryEvidenceTerminalFirst(t *testing.T) {
	// Body evidence says retryable; structured type says terminal.
	const throttleBody = "Rate limit exceeded (5 requests per minute). Please try again after 6 seconds."
	if got := classifyRateLimitBody(throttleBody, "insufficient_quota"); got != KindQuota {
		t.Fatalf("contradictory evidence classified %v, want %v — structured terminal type must beat body throttle evidence (HTTP terminal-first)", got, KindQuota)
	}

	// Same contradiction end-to-end through the driver, since that is where
	// the two channels actually meet.
	d, _, cleanup := newTestDriver(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(429)
		// Header carries a wait; the type still decides the KIND. The two
		// dimensions are orthogonal — 73.2 fills wait fields on quota errors
		// too (the disposition difference is the kernel's, not the
		// classifier's).
		w.Header().Set("retry-after", "6")
		_, _ = w.Write([]byte(`{"error":{"message":"Rate limit exceeded (5 requests per minute). Please try again after 6 seconds.","type":"insufficient_quota"}}`))
	})
	defer cleanup()
	_, err := d.Call(t.Context(), LLMRequest{Intent: "hi"})
	assertKind(t, err, KindQuota)
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
