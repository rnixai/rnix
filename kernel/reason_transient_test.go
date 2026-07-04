package kernel

import (
	gocontext "context"
	"errors"
	"fmt"
	"testing"

	"github.com/rnixai/rnix/drivers/llm"
	"github.com/rnixai/rnix/internal/types"
)

// TestIsTransientLLMError_TimeoutAndCancel locks the F2 hotfix semantics
// (spec-apc-noop-f1-f2): timeout-flavoured transport failures — including the
// "[DRIVER] …: … (… TLS handshake timeout)" string shape produced when a
// driver error crosses the VFS boundary — must be classified transient so the
// reasonStep retry path applies, while context cancellation (user-driven
// kill/suspend) must NOT be misclassified as transient.
func TestIsTransientLLMError_TimeoutAndCancel(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "driver-wrapped TLS handshake timeout is transient",
			err: types.NewDriverError("write", "/dev/llm/openai",
				fmt.Errorf("http request failed: %w", errors.New("net/http: TLS handshake timeout")),
				types.ErrDriver),
			want: true,
		},
		{
			name: "bare TLS handshake timeout string is transient (outer fallback)",
			err:  errors.New("net/http: TLS handshake timeout"),
			want: true,
		},
		{
			name: "timed out phrasing is transient",
			err:  errors.New("llm [claude]: request timed out waiting for response"),
			want: true,
		},
		{
			name: "ErrTimeout sentinel from driver layer is transient",
			err:  llm.NewLLMError("openai-compat", 0, llm.ErrTimeout),
			want: true,
		},
		{
			name: "context canceled is not transient",
			err:  gocontext.Canceled,
			want: false,
		},
		{
			name: "driver-wrapped context canceled is not transient",
			err: types.NewDriverError("write", "/dev/llm/openai",
				gocontext.Canceled, types.ErrDriver),
			want: false,
		},
		{
			name: "certificate error is not transient",
			err:  errors.New("http request failed: x509: certificate signed by unknown authority"),
			want: false,
		},
		{
			// Permanent-error veto: a 401 auth failure whose provider body
			// mentions "session timeout" must NOT be misclassified as
			// transient by the substring fallback.
			name: "auth error with timeout-flavoured text is not transient",
			err:  llm.NewLLMError("x", 401, fmt.Errorf("session timeout: %w", llm.ErrAuth)),
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isTransientLLMError(tc.err); got != tc.want {
				t.Fatalf("isTransientLLMError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}
