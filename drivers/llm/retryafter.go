package llm

import (
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Story 73.2 — server-declared wait parsing. This file is the PARSE layer of
// the rate-limit handling stack: headers and provider bodies → RetryAfter /
// ResetAt / Source. The decision-and-wait layer (priority arbitration against
// the kernel's hard caps, jitter, the actual sleep) lives in kernel/reason.go
// — drivers/ must never import kernel/ (project-context iron rule + AC7).

// parseRetryAfter extracts a server-declared wait duration from response
// headers, trying the three wire shapes in order (opencode's retry.ts form
// set, Story 73.2 / AC1 — do not reinvent):
//
//  1. retry-after-ms — integer milliseconds
//  2. retry-after    — integer SECONDS
//  3. retry-after    — absolute HTTP date (RFC 7231 via http.ParseTime)
//
// now is a parameter (not a time.Now() call) because the date form is a
// relative computation and tests must inject fixed instants (AC9).
//
// 🔴 The seconds reading is pinned by production evidence: a captured
// `Retry-After: 343831` matched the body's self-declared "reset at 08-05
// 22:23:00 UTC" exactly when read as seconds (sample instant 08-01 22:52:29
// UTC + 343831s). Reading it as milliseconds would shrink a 95.5-hour quota
// window to 5.7 minutes and the process would retry long before recovery.
// Values that "look too large for seconds" therefore stay seconds — the AC5
// hard cap (kernel side) is what bounds the wait, never a unit guess here.
//
// Negative values and parse failures return (0, false): a negative duration
// passed to time.After fires immediately, silently converting backoff into
// the zero-delay hammering this epic exists to eliminate.
func parseRetryAfter(h http.Header, now time.Time) (time.Duration, bool) {
	if h == nil {
		return 0, false
	}
	// http.Header.Get canonicalises the key, so lookup is case-insensitive.
	if ms := strings.TrimSpace(h.Get("retry-after-ms")); ms != "" {
		if v, err := strconv.ParseInt(ms, 10, 64); err == nil && v > 0 {
			return scaleDuration(v, time.Millisecond), true
		}
		// A malformed/zero/negative ms header FALLS THROUGH to retry-after:
		// the three wire shapes are tried IN ORDER, and a proxy-injected
		// garbage ms header must not shadow the origin's valid seconds header
		// (symmetric with the garbage retry-after fall-through to the body
		// channel). Review 2026-08-03.
	}
	if ra := strings.TrimSpace(h.Get("retry-after")); ra != "" {
		if v, err := strconv.ParseInt(ra, 10, 64); err == nil {
			if v <= 0 {
				return 0, false
			}
			return scaleDuration(v, time.Second), true
		}
		if t, err := http.ParseTime(ra); err == nil {
			if d := t.Sub(now); d > 0 {
				return d, true
			}
			return 0, false
		}
		return 0, false
	}
	return 0, false
}

// scaleDuration converts a parsed count into a Duration without int64
// overflow. Declared waits beyond ~292 years (MaxInt64 nanoseconds) cannot be
// represented; they SATURATE to the maximum Duration instead of wrapping to a
// small positive value — a wrapped 584-year declaration would otherwise be
// credited as a sub-second wait and retry almost immediately, bypassing the
// kernel's give-up decision. Bounding a wait is the kernel's AC5 hard cap's
// job (this file never guesses units or caps — see the 343831 note above),
// so a saturated value correctly lands in the give-up branch.
func scaleDuration(v int64, unit time.Duration) time.Duration {
	if v > math.MaxInt64/int64(unit) {
		return time.Duration(math.MaxInt64)
	}
	return time.Duration(v) * unit
}

// resetAtBodyRe matches the captured qwen shape "The quota will reset at 08-05
// 22:23:00 UTC." — month-day without a year (fixture provenance: verbatim
// production capture, see atdd_73_1_rate_limit_classification_test.go).
//
// The UTC-equivalent suffix is REQUIRED (review 2026-08-03): the instant is
// always built in time.UTC, so a missing or unrecognised zone suffix (+08:00,
// PST, …) would be silently read as UTC — off by up to ±14h, which a
// past-shifted instant then compounds into a next-year rollover and a
// give-up-and-die. The captured shape carries an explicit "UTC"; anything
// else fails safe to local backoff instead of guessing.
var resetAtBodyRe = regexp.MustCompile(`(?i)reset at (\d{1,2})-(\d{1,2}) (\d{1,2}):(\d{2}):(\d{2})\s*(UTC|GMT|Z)\b`)

// retryAfterBodyRe matches the captured infron shape "Please try again after
// 6 seconds." The judgement matches the TEMPLATE `try again after<N> <unit>`,
// never a specific number; units cover seconds/second/s and minutes/minute/m
// (provider wording is not uniform).
var retryAfterBodyRe = regexp.MustCompile(`(?i)try again after (\d+)\s*(seconds?|minutes?|[sm])\b`)

// parseWaitFromBody extracts wait information from a provider error message
// (Story 73.2 / AC3). It is a REQUIRED path, not an optional degradation: 6
// of the 13 captured 429s carried no headers at all (the infron batch gives
// the wait only in the body).
//
// Two captured shapes:
//   - absolute reset instant (qwen, terminal quota):
//     "…quota has been exhausted. The quota will reset at 08-05 22:23:00 UTC."
//     → resetAt. The timestamp carries no year, so now supplies it; a reset
//     date already past in the current year rolls into the next year (Dec 31
//     seeing "01-02" means January 2nd NEXT year).
//   - relative seconds (infron, retryable throttle):
//     "…Please try again after 6 seconds…" → retryAfter.
//
// now is a parameter for the same clock-injection reason as parseRetryAfter.
// Parse failures return ok=false (fail-safe) so the caller falls back to the
// local exponential backoff — never (0, true), which a caller could read as
// "the server said no wait is needed".
func parseWaitFromBody(msg string, now time.Time) (retryAfter time.Duration, resetAt time.Time, ok bool) {
	if m := resetAtBodyRe.FindStringSubmatch(msg); m != nil {
		month, _ := strconv.Atoi(m[1])
		day, _ := strconv.Atoi(m[2])
		hour, _ := strconv.Atoi(m[3])
		minute, _ := strconv.Atoi(m[4])
		second, _ := strconv.Atoi(m[5])
		if month < 1 || month > 12 || day < 1 || day > 31 || hour > 23 || minute > 59 || second > 59 {
			return 0, time.Time{}, false
		}
		candidate := time.Date(now.Year(), time.Month(month), day, hour, minute, second, 0, time.UTC)
		// time.Date NORMALIZES impossible dates (02-31 → 03-03); a round-trip
		// check rejects them fail-safe instead of waiting for an instant the
		// provider never named (review 2026-08-03).
		if candidate.Month() != time.Month(month) || candidate.Day() != day {
			return 0, time.Time{}, false
		}
		if candidate.Before(now) {
			// Strictly-past instants roll into next year (Dec 31 seeing
			// "01-02"). An instant exactly equal to now is returned as-is:
			// the kernel's time.Until ≤ 0 handling degrades it to local
			// backoff (retry promptly — the window is resetting now), whereas
			// a next-year roll would balloon into a give-up-and-die.
			candidate = candidate.AddDate(1, 0, 0)
		}
		return 0, candidate, true
	}
	if m := retryAfterBodyRe.FindStringSubmatch(msg); m != nil {
		n, err := strconv.Atoi(m[1])
		if err != nil || n <= 0 {
			return 0, time.Time{}, false
		}
		unit := strings.ToLower(m[2])
		if strings.HasPrefix(unit, "m") {
			return scaleDuration(int64(n), time.Minute), time.Time{}, true
		}
		return scaleDuration(int64(n), time.Second), time.Time{}, true
	}
	return 0, time.Time{}, false
}

// resolveRateLimitWait arbitrates the wait a kernel should honour for a
// classified rate-limit failure (Story 73.2 / AC2 + AC3 + D2). Precedence:
// HEADERS win over the body, but only when they parse to a positive duration
// (D2 — headers are structured fields; the body is natural language whose
// parsing depends on provider wording). Missing headers are the norm, not the
// exception (6 of 13 captured 429s), so body parsing is a required path.
//
// The returned source records the channel actually credited — provenance, not
// the policy name: a body-only parse reports "body" even though headers had
// priority ([[observability-data-provenance-principle]]). Both parse failures
// yield ("", false) and the caller degrades to local exponential backoff.
func resolveRateLimitWait(h http.Header, msg string, now time.Time) (retryAfter time.Duration, resetAt time.Time, source string, ok bool) {
	if d, hOK := parseRetryAfter(h, now); hOK && d > 0 {
		return d, time.Time{}, "header", true
	}
	// Symmetric positivity gate with the header branch: ok=true must carry a
	// usable instruction (a positive relative wait OR a reset instant).
	if ra, at, bOK := parseWaitFromBody(msg, now); bOK && (ra > 0 || !at.IsZero()) {
		return ra, at, "body", true
	}
	return 0, time.Time{}, "", false
}
