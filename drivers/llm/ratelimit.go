package llm

import (
	"errors"
	"strings"
	"time"
)

// RateLimitKind is the trichotomy of rate-limit failures established by
// Story 73.1 / AC2. Values are PascalCase per project convention (no
// ALL_CAPS constants).
type RateLimitKind int

const (
	// KindThrottle — retryable throttle (ErrRateLimitThrottle).
	KindThrottle RateLimitKind = iota
	// KindQuota — terminal quota exhaustion (ErrQuotaExhausted).
	KindQuota
	// KindOverload — server overload 529/503 (ErrServerOverloaded).
	KindOverload
)

func (k RateLimitKind) String() string {
	switch k {
	case KindQuota:
		return "quota"
	case KindOverload:
		return "overload"
	default:
		return "throttle"
	}
}

// RateLimitError is the structured payload that carries a classified
// rate-limit failure across the driver→kernel boundary. Drivers must not
// import kernel/, so the wait durations travel on the error value itself.
//
// Story 73.1 only builds the type and the judgement surface; RetryAfter /
// ResetAt / Source are filled by Story 73.2 and stay zero-valued here.
type RateLimitError struct {
	Kind    RateLimitKind // throttle / quota / overload
	Message string        // provider's original body — must be preserved verbatim
	// The following fields are populated by Story 73.2. This story leaves them zero.
	RetryAfter time.Duration // server-declared wait duration (0 = unknown)
	ResetAt    time.Time     // quota-window reset instant (zero value = unknown)
	Source     string        // "header" / "body" / "" (provenance, filled by 73.2)
}

// Error implements error. It MUST keep the "<message>: <public sentinel>"
// shape (Story 73.1 / AC2-② byte-fidelity contract): driverErrorDetail
// (kernel/reason.go) takes the first line of this string as exit_reason, and
// Story 73.4's data shows the provider text after "status 429):" is exactly
// what carries the quota clue. Returning only the short sentinel text would
// regress those two recorded deaths into bare strings.
//
// The trailing text is the PUBLIC sentinel (ErrRateLimit / ErrServerOverloaded),
// not the kind-specific one, so the rendered string stays byte-identical to
// the pre-73.1 shape. The kind is not carried in the text at all — it travels
// on the Unwrap chain (for code) and in the AC6 event field + log line (for
// operators), which is where classification belongs.
func (e *RateLimitError) Error() string {
	if e.Message == "" {
		// Production 503 bodies are empty (recorded shape "status 503): )").
		// Emitting a leading ": " there would be noise, not fidelity.
		return e.displaySentinel().Error()
	}
	return e.Message + ": " + e.displaySentinel().Error()
}

// Unwrap returns the sentinel for the classified kind, keeping the whole
// error chain errors.Is/errors.As-traversable (Story 73.1 / D2). Both
// throttle and quota unwind to ErrRateLimit via their sentinels.
func (e *RateLimitError) Unwrap() error {
	return e.sentinel()
}

// sentinel is the kind-specific sentinel — the errors.Is judgement surface.
func (e *RateLimitError) sentinel() error {
	switch e.Kind {
	case KindQuota:
		return ErrQuotaExhausted
	case KindOverload:
		return ErrServerOverloaded
	default:
		return ErrRateLimitThrottle
	}
}

// displaySentinel is the public sentinel used for rendering only, keeping
// Error() byte-identical to the pre-73.1 output (see Error).
func (e *RateLimitError) displaySentinel() error {
	if e.Kind == KindOverload {
		return ErrServerOverloaded
	}
	return ErrRateLimit
}

// IsRateLimited reports whether err belongs to the rate-limit family.
// Kernel-side entry point for the retry decision; callers should not
// errors.As into *RateLimitError directly.
//
// The ErrRateLimit check is deliberately inclusive rather than enumerating the
// two kind sentinels: it subsumes both (each wraps it) AND still claims a bare
// ErrRateLimit. That matters because AC1 removed rate limits from IsTransient
// — any error shape carrying only the public sentinel would otherwise become
// instantly fatal, which is the same "delete without reconnecting" regression
// D3 forbids, just reached by a different shape.
//
// Note the deliberate asymmetry with RateLimitKindOf: this answers "is it in
// the family" (inclusive, fail-open — it drives the retry decision), while
// RateLimitKindOf answers "what did the classifier determine" (honest about
// provenance — it drives the reported kind). A bare ErrRateLimit is therefore
// retryable but carries no classified kind, degrading to exactly the
// pre-73.1 behaviour rather than fabricating a classification.
func IsRateLimited(err error) bool {
	return errors.Is(err, ErrRateLimit) || errors.Is(err, ErrServerOverloaded)
}

// RateLimitKindOf extracts the classified kind from err. It works for both
// carrier shapes: a *RateLimitError (HTTP drivers) and a bare sentinel (CLI
// drivers, which carry no provider body). Returns (kind, false) when err
// carries no classification — including a bare ErrRateLimit, which is
// rate-limited but unclassified (see IsRateLimited).
func RateLimitKindOf(err error) (RateLimitKind, bool) {
	switch {
	case errors.Is(err, ErrQuotaExhausted):
		return KindQuota, true
	case errors.Is(err, ErrServerOverloaded):
		return KindOverload, true
	case errors.Is(err, ErrRateLimitThrottle):
		return KindThrottle, true
	default:
		return KindThrottle, false
	}
}

// rateLimitQuotaMarkers are terminal-quota evidence: hitting any of them
// means the whole window is spent. The set form (not scattered ifs) lets new
// provider shapes be added as entries (Story 73.1 / AC4).
//
// Precision constraints — bare "usage limit" and bare "quota exhausted" are
// deliberately absent:
//   - "usage limit" would match the only CLI text in this repo's 1836 samples,
//     "(not your usage limit)", misclassifying a retryable throttle as a
//     terminal quota (a 5-hour suspend for something the provider explicitly
//     says is not quota).
//   - "quota exhausted" would match "carpool 5h quota exhausted", which rides
//     on the same CLI throttle line. Bare "quota exhausted" may ONLY appear on
//     the retryable side (fail-open direction).
var rateLimitQuotaMarkers = []string{
	"insufficient_quota",
	"quota has been exhausted",
	"quota will reset",
	"usage limit reached",
}

// rateLimitThrottleMarkers are retryable-throttle evidence. Shared by the
// HTTP body split and the CLI classifier.
//
// "rate limit" (rather than AC4's longer "rate limit exceeded") is the entry
// used because it subsumes it and matches AC5's CLI row verbatim — CLI
// providers say "API rate limited", which the longer form would miss.
var rateLimitThrottleMarkers = []string{
	"rate limit",
	"requests per minute",
	"try again after",
	"too many requests",
	"temporarily limiting",
	"quota exhausted",
}

// classifyRateLimitBody splits a 429 response body into the rate-limit
// trichotomy using server self-declared evidence. errType is the provider's
// structured error.type where one is parsed (OpenAI-compat only).
//
// Ordering here is terminal-first (Story 73.1 / AC4: "hit any terminal marker
// → terminal"), which is the OPPOSITE of classifyCliError's retryable-first
// order (AC5). The two are not interchangeable, and the divergence is load-
// bearing:
//
//   - HTTP driver messages carry protocol boilerplate. The Anthropic SDK's
//     Error() renders as `POST "…": 429 Too Many Requests {…}` — the status
//     text alone hits the retryable marker "too many requests" on EVERY 429.
//     Retryable-first would therefore make terminal quota undetectable on that
//     driver, whatever the body says.
//   - CLI messages have no boilerplate but do carry mixed evidence (the
//     recorded "(not your usage limit) · carpool 5h quota exhausted" line), so
//     there retryable-first is what keeps the fail-open direction.
//
// Fail-open is preserved on both paths, because ordering is only half the
// guard: the terminal marker set is precise enough that the mixed-evidence CLI
// line hits none of it (see rateLimitQuotaMarkers). An unknown body classifies
// as throttle, never quota — misjudging a throttle as terminal suspends or
// kills the process, while the reverse costs one recoverable backoff.
func classifyRateLimitBody(msg, errType string) RateLimitKind {
	lower := strings.ToLower(msg + " " + errType)

	if containsAnyMarker(lower, rateLimitQuotaMarkers) {
		return KindQuota
	}
	if containsAnyMarker(lower, rateLimitThrottleMarkers) {
		return KindThrottle
	}
	return KindThrottle // fail-open: unknown → retryable
}

// containsAnyMarker reports whether lower (already lower-cased) contains any
// of the markers. Marker matching is set-based on purpose: adding a new
// provider shape means adding a set entry, never a new branch.
func containsAnyMarker(lower string, markers []string) bool {
	for _, marker := range markers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// NewRateLimitError builds a *RateLimitError for the given kind, keeping the
// provider's original message as Message. errMsg should be the raw provider
// body (not yet wrapped in fmt.Errorf chains) so Error() stays byte-faithful
// to the recorded production shape.
func NewRateLimitError(kind RateLimitKind, errMsg string) *RateLimitError {
	return &RateLimitError{Kind: kind, Message: errMsg}
}
