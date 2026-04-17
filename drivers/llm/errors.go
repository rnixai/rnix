package llm

import (
	"errors"
	"fmt"
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

// IsTransient returns true if the error is a transient LLM error that may
// succeed on retry (socket disconnect, connection reset, overloaded, etc.).
func IsTransient(err error) bool {
	return errors.Is(err, ErrTransient) ||
		errors.Is(err, ErrRateLimit) ||
		errors.Is(err, ErrTimeout) ||
		errors.Is(err, ErrStreamIncomplete)
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
