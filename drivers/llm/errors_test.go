package llm

import (
	"errors"
	"fmt"
	"testing"
)

func TestLLMError_Error(t *testing.T) {
	err := NewLLMError("claude", 429, ErrRateLimit)
	got := err.Error()
	want := "llm [claude] (status 429): llm: rate limit exceeded"
	if got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestLLMError_Error_ZeroStatus(t *testing.T) {
	err := NewLLMError("claude", 0, fmt.Errorf("something broke"))
	got := err.Error()
	want := "llm [claude]: something broke"
	if got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestLLMError_Unwrap(t *testing.T) {
	inner := fmt.Errorf("inner error")
	err := NewLLMError("openai", 0, inner)
	if unwrapped := errors.Unwrap(err); unwrapped != inner {
		t.Errorf("Unwrap() = %v, want %v", unwrapped, inner)
	}
}

func TestLLMError_Is_SentinelErrors(t *testing.T) {
	err := NewLLMError("claude", 429, ErrRateLimit)
	if !errors.Is(err, ErrRateLimit) {
		t.Error("errors.Is(err, ErrRateLimit) = false, want true")
	}
}

func TestLLMError_As(t *testing.T) {
	err := NewLLMError("claude", 429, ErrRateLimit)
	var llmErr *LLMError
	if !errors.As(err, &llmErr) {
		t.Fatal("errors.As failed to extract *LLMError")
	}
	if llmErr.Provider != "claude" {
		t.Errorf("Provider = %q, want %q", llmErr.Provider, "claude")
	}
	if llmErr.StatusCode != 429 {
		t.Errorf("StatusCode = %d, want %d", llmErr.StatusCode, 429)
	}
}

func TestSentinelErrors_Distinct(t *testing.T) {
	sentinels := []error{ErrRateLimit, ErrAuth, ErrContextLength, ErrModelNotFound, ErrTimeout}
	for i := range sentinels {
		for j := i + 1; j < len(sentinels); j++ {
			if errors.Is(sentinels[i], sentinels[j]) {
				t.Errorf("sentinel[%d] (%v) should not match sentinel[%d] (%v)", i, sentinels[i], j, sentinels[j])
			}
		}
	}
}
