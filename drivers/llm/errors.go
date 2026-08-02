package llm

import (
	"errors"
	"fmt"
	"net"
)

// Sentinel errors for LLM-specific failure categories.
var (
	ErrRateLimit        = errors.New("llm: rate limit exceeded")
	ErrAuth             = errors.New("llm: authentication failed")
	ErrLoginRequired    = errors.New("llm: login required")
	ErrContextLength    = errors.New("llm: context length exceeded")
	ErrModelNotFound    = errors.New("llm: model not found")
	ErrTimeout          = errors.New("llm: request timed out")
	ErrTransient        = errors.New("llm: transient error")
	ErrStreamIncomplete = errors.New("llm: stream ended without result")
	ErrMaxTurns         = errors.New("llm: agent loop exhausted max turns")
)

// Rate-limit trichotomy sentinels (Story 73.1 / AC2). They form the stable
// errors.Is judgement surface; the structured payload that carries wait
// durations lives in *RateLimitError (ratelimit.go).
//
// ErrRateLimitThrottle and ErrQuotaExhausted both wrap ErrRateLimit so the
// public sentinel established by Architecture Decision 4 stays on the Unwrap
// chain — eight existing driver tests assert errors.Is(err, ErrRateLimit) and
// downstream consumers (ipc/http_openai.go formatUpstreamError) key off it.
// ErrServerOverloaded deliberately does NOT wrap ErrRateLimit: 529/503 mean
// "the server is momentarily unavailable", not "your allowance is spent", and
// merging the two would let Story 73.3's terminal-quota suspension swallow a
// transient service blip.
var (
	// ErrRateLimitThrottle — retryable throttle: the provider states a short
	// wait is enough to recover.
	ErrRateLimitThrottle = fmt.Errorf("%w (retryable throttle)", ErrRateLimit)
	// ErrQuotaExhausted — terminal quota exhaustion: a whole window is spent,
	// recovery requires waiting for the reset instant.
	ErrQuotaExhausted = fmt.Errorf("%w (quota window exhausted)", ErrRateLimit)
	// ErrServerOverloaded — 529/503: the server is temporarily unavailable.
	ErrServerOverloaded = errors.New("llm: server overloaded")
)

// IsTransient returns true if the error is a transient LLM error that may
// succeed on retry (socket disconnect, connection reset, overloaded, etc.).
//
// Story 73.1 / AC1: rate limits are deliberately NOT part of this set. They
// need a wait before the retry, whereas everything here is safe to retry with
// zero delay. The kernel claims the rate-limit family separately via
// IsRateLimited so the two can carry different backoff policies (Story 73.2).
func IsTransient(err error) bool {
	return errors.Is(err, ErrTransient) ||
		errors.Is(err, ErrTimeout) ||
		errors.Is(err, ErrStreamIncomplete)
}

// classifyTransportError maps low-level HTTP transport failures to typed LLM
// errors. Timeout-flavoured net.Error values (TLS handshake timeout, dial
// timeout, response header timeout) are transient by nature and map to
// ErrTimeout so the kernel retry path applies; anything else keeps the
// generic wrap.
func classifyTransportError(provider string, err error) error {
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		// Preserve the original error text ("TLS handshake timeout", "dial
		// timeout", …) for logs while keeping errors.Is(…, ErrTimeout) true.
		return NewLLMError(provider, 0, fmt.Errorf("%w: %v", ErrTimeout, err))
	}
	return fmt.Errorf("http request failed: %w", err)
}

// StreamInterruptedError indicates the LLM stream was interrupted by context
// cancellation before a "done" event was received. Partial carries any text
// content accumulated before the interruption. This error is NOT transient
// (retry is pointless when the process is being killed).
type StreamInterruptedError struct {
	Partial string
}

func (e *StreamInterruptedError) Error() string {
	n := len(e.Partial)
	return fmt.Sprintf("llm: stream interrupted by cancellation (%d bytes partial content)", n)
}

// LLMError represents a typed error from an LLM driver.
type LLMError struct {
	Provider   string            // "claude", "openai", "gemini", "ollama"
	StatusCode int               // HTTP status code (0 if not applicable)
	Err        error             // underlying sentinel or raw error
	Meta       map[string]string // optional structured context (e.g., login_url)
}

// Error formats the error as "llm [provider] (status N): message".
// When StatusCode is 0, the "(status N)" part is omitted.
func (e *LLMError) Error() string {
	if e.StatusCode != 0 {
		return fmt.Sprintf("llm [%s] (status %d): %s", e.Provider, e.StatusCode, e.Err)
	}
	return fmt.Sprintf("llm [%s]: %s", e.Provider, e.Err)
}

// Unwrap returns the underlying error for errors.Is/As chain traversal.
func (e *LLMError) Unwrap() error {
	return e.Err
}

// NewLLMError constructs a new LLMError.
func NewLLMError(provider string, statusCode int, err error) *LLMError {
	return &LLMError{Provider: provider, StatusCode: statusCode, Err: err}
}
