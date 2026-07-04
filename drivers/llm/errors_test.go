package llm

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
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

// fakeTimeoutErr implements net.Error with Timeout()==true, mimicking
// net/http transport timeouts (TLS handshake timeout, dial timeout).
type fakeTimeoutErr struct{ msg string }

func (e *fakeTimeoutErr) Error() string   { return e.msg }
func (e *fakeTimeoutErr) Timeout() bool   { return true }
func (e *fakeTimeoutErr) Temporary() bool { return true }

func TestClassifyTransportError_TimeoutMapsToErrTimeout(t *testing.T) {
	// net/http wraps transport errors in *url.Error which delegates Timeout()
	// to the inner error; a plain net.Error is equivalent for errors.As.
	err := classifyTransportError("opencodezen", &fakeTimeoutErr{msg: "net/http: TLS handshake timeout"})
	if !IsTransient(err) {
		t.Fatalf("timeout transport error must be transient, got %v", err)
	}
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("timeout transport error must map to ErrTimeout, got %v", err)
	}
	if !strings.Contains(err.Error(), "TLS handshake timeout") {
		t.Fatalf("classified error must preserve original text, got %q", err.Error())
	}
}

func TestClassifyTransportError_URLErrorWrappedTimeout(t *testing.T) {
	// Production shape: net/http wraps transport failures in *url.Error, which
	// delegates Timeout() to the inner error. Classification must still map
	// to ErrTimeout through the wrap.
	wrapped := &url.Error{
		Op:  "Post",
		URL: "https://x",
		Err: &fakeTimeoutErr{msg: "net/http: TLS handshake timeout"},
	}
	err := classifyTransportError("opencodezen", wrapped)
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("url.Error-wrapped timeout must map to ErrTimeout, got %v", err)
	}
	if !strings.Contains(err.Error(), "TLS handshake timeout") {
		t.Fatalf("classified error must preserve original text, got %q", err.Error())
	}
}

func TestClassifyTransportError_NonTimeoutKeepsGenericWrap(t *testing.T) {
	inner := errors.New("certificate signed by unknown authority")
	err := classifyTransportError("opencodezen", inner)
	if errors.Is(err, ErrTimeout) {
		t.Fatalf("non-timeout error must not map to ErrTimeout, got %v", err)
	}
	if !errors.Is(err, inner) {
		t.Fatalf("generic wrap must preserve the original error chain, got %v", err)
	}
}
