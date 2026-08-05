package kernel

import (
	gocontext "context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rnixai/rnix/drivers/llm"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// Story 73.1 — kernel-side share: the classification split must not regress
// retry behaviour (AC1 / D3) and the retry event must carry the class (AC6).
//
// Fixture provenance: the two 429 bodies below are verbatim captures from this
// repo's production data (see drivers/llm/atdd_73_1_rate_limit_classification_test.go
// for the full provenance note).
const (
	kernelQuotaBody    = "Your token-plan 1-week quota has been exhausted. The quota will reset at 08-05 22:23:00 UTC."
	kernelThrottleBody = "Rate limit exceeded (5 requests per minute). Please try again after 6 seconds."
)

// wrapLikeProduction reproduces the wrapping depth the kernel actually sees.
// A driver's *llm.LLMError crosses the VFS boundary and gets wrapped again;
// *types.DriverError has the same Unwrap shape as vfs.VFSError (whose
// constructor is unexported) and is the sanctioned stand-in.
func wrapLikeProduction(inner error) error {
	return types.NewDriverError("write", "/dev/llm/qwen", inner, types.ErrDriver)
}

// --- AC1 / D3: removing ErrRateLimit from IsTransient must not make 429 fatal ---

// TestATDD_73_1_AC1_RateLimitStillEntersRetryPath is the anti-regression lock
// for the red line in D3. Before this story a 429 was retried three times with
// zero delay and then killed the process; AC1 takes rate limits out of
// llm.IsTransient, and if the kernel does not claim them back explicitly the
// failure mode gets strictly worse — straight to attemptFallback and death
// with no retry at all. Backoff itself is Story 73.2; this only holds the line.
func TestATDD_73_1_AC1_RateLimitStillEntersRetryPath(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{
			name: "structured quota error through production wrapping",
			err: wrapLikeProduction(llm.NewLLMError("qwen", 429,
				llm.NewRateLimitError(llm.KindQuota, kernelQuotaBody))),
		},
		{
			name: "structured throttle error through production wrapping",
			err: wrapLikeProduction(llm.NewLLMError("infron", 429,
				llm.NewRateLimitError(llm.KindThrottle, kernelThrottleBody))),
		},
		{
			name: "server overload through production wrapping",
			err: wrapLikeProduction(llm.NewLLMError("anthropic", 529,
				llm.NewRateLimitError(llm.KindOverload, ""))),
		},
		{
			// CLI drivers carry a bare sentinel — no provider body to structure.
			name: "bare CLI throttle sentinel",
			err:  llm.NewLLMError("claude", 429, llm.ErrRateLimitThrottle),
		},
		{
			name: "bare CLI quota sentinel",
			err:  llm.NewLLMError("claude", 429, llm.ErrQuotaExhausted),
		},
		{
			// The public sentinel must keep working on its own: any provider or
			// test still producing the pre-73.1 shape must not become fatal.
			name: "legacy bare ErrRateLimit shape",
			err:  llm.NewLLMError("claude", 429, fmt.Errorf("%s: %w", kernelThrottleBody, llm.ErrRateLimit)),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if !isTransientLLMError(tc.err) {
				t.Fatalf("isTransientLLMError(%v) = false, want true — 429 has stopped entering the retry path, which is worse than the bug this story fixes", tc.err)
			}
		})
	}
}

// TestATDD_73_1_AC1_PermanentVetoStillWins guards the ordering in §5's
// combination matrix: the permanent-error veto runs before the rate-limit
// claim, so a context-length or auth failure whose body happens to mention a
// quota stays non-retryable.
func TestATDD_73_1_AC1_PermanentVetoStillWins(t *testing.T) {
	cases := []struct {
		name string
		err  error
	}{
		{
			name: "context length with quota-flavoured text",
			err: llm.NewLLMError("openai", 400,
				fmt.Errorf("token quota has been exhausted for this context: %w", llm.ErrContextLength)),
		},
		{
			name: "auth failure with rate-limit-flavoured text",
			err: llm.NewLLMError("openai", 401,
				fmt.Errorf("rate limit exceeded: %w", llm.ErrAuth)),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if isTransientLLMError(tc.err) {
				t.Fatalf("isTransientLLMError(%v) = true, want false — the permanent-error veto must keep running before the rate-limit claim", tc.err)
			}
		})
	}
}

// TestATDD_73_1_AC2_ExitReasonKeepsProviderClue pins the fidelity contract at
// the consumer end. driverErrorDetail's output is what lands in
// proc-info.json's exit_reason; Story 73.4 recorded that only 2 of 24
// rate-limit deaths carried any clue at all, and that clue came from exactly
// this string. A RateLimitError.Error() returning only the short sentinel
// would erase it.
func TestATDD_73_1_AC2_ExitReasonKeepsProviderClue(t *testing.T) {
	// Unwrapped *llm.LLMError — the shape driverErrorDetail's LLMError branch
	// handles, and the one production LLM failures actually take (no LLM driver
	// emits *types.DriverError; see the branch comment at reason.go:341).
	structured := llm.NewLLMError("qwen", 429,
		llm.NewRateLimitError(llm.KindQuota, kernelQuotaBody))
	legacy := llm.NewLLMError("qwen", 429,
		fmt.Errorf("%s: %w", kernelQuotaBody, llm.ErrRateLimit))

	got := driverErrorDetail(structured)
	// Byte-for-byte identical to the pre-73.1 output for the same failure.
	if want := driverErrorDetail(legacy); got != want {
		t.Fatalf("driverErrorDetail(structured) = %q, want %q (byte-identical to the pre-73.1 shape)", got, want)
	}
	if got != kernelQuotaBody+": "+llm.ErrRateLimit.Error() {
		t.Fatalf("driverErrorDetail = %q, want the provider body plus the public sentinel", got)
	}
	// 118 bytes in the recorded case — comfortably inside the 200-byte cap, so
	// the clue is not truncated away.
	if len(got) > maxExitReasonDetailBytes {
		t.Fatalf("detail is %d bytes, exceeding maxExitReasonDetailBytes(%d)", len(got), maxExitReasonDetailBytes)
	}

	// The clue must also survive the extra VFS-boundary wrap the kernel sees.
	if wrapped := driverErrorDetail(wrapLikeProduction(structured)); !strings.Contains(wrapped, "quota will reset") {
		t.Errorf("wrapped detail = %q, want the provider quota clue preserved", wrapped)
	}
}

// --- AC6: transient_retry carries rate_limit_kind ---

// rateLimitRetryLLM fails the first Write with writeErr, then succeeds — the
// shape that drives exactly one transient_retry event followed by a normal
// completion.
type rateLimitRetryLLM struct {
	writeErr error
	failures int
}

func (f *rateLimitRetryLLM) Write(_ gocontext.Context, _ []byte) error {
	if f.failures > 0 {
		f.failures--
		return f.writeErr
	}
	return nil
}

func (f *rateLimitRetryLLM) Read(_ int) ([]byte, error) { return makeLLMResponse("done", 5), nil }
func (f *rateLimitRetryLLM) Close() error               { return nil }
func (f *rateLimitRetryLLM) Stat() (vfs.FileStat, error) {
	return vfs.FileStat{IsDevice: true, Name: "/dev/llm/qwen"}, nil
}
func (f *rateLimitRetryLLM) SupportsToolCalling() bool { return true }

// retryEventArgs runs one process whose first LLM Write fails with writeErr and
// returns the args of the transient_retry event it produced.
func retryEventArgs(t *testing.T, writeErr error) map[string]any {
	t.Helper()
	llmFile := &rateLimitRetryLLM{writeErr: writeErr, failures: 1}
	// "claude" is the default provider Spawn resolves when no provider is
	// given (same convention as the 56.7 harness).
	k, baseDir := newFailureRawKernel(t, llmFile, nil, "claude", "")

	pid, err := k.Spawn("73.1 AC6 retry event", nil, failureRawSpawnOpts(baseDir))
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	proc, _ := k.GetProcess(pid)
	waitDone(t, proc)

	evs, err := ReadAllEvents(filepath.Join(baseDir, "steps", proc.UUID, "events.jsonl"))
	if err != nil {
		t.Fatalf("ReadAllEvents: %v", err)
	}
	for _, ev := range evs {
		if ev.Syscall == "ReasonStep" && ev.Args["action"] == "transient_retry" {
			return ev.Args
		}
	}
	t.Fatalf("no transient_retry event found among %d events", len(evs))
	return nil
}

func TestATDD_73_1_AC6_RetryEventCarriesRateLimitKind(t *testing.T) {
	cases := []struct {
		name     string
		writeErr error
		wantKind string
	}{
		{
			name: "quota",
			// Story 73.3 / D5 update: a WAITLESS KindQuota error now suspends
			// immediately — no transient_retry event exists there to carry the
			// kind (that disposition is covered by
			// TestATDD_73_3_D5_FastPath_NoWaitQuotaSuspendsImmediately). Give
			// the classification probe an in-cap server wait so the retry path
			// still exercises the kind plumbing.
			writeErr: llm.NewLLMError("qwen", 429,
				llm.NewRateLimitErrorWithWait(llm.KindQuota, kernelQuotaBody, 3*time.Second, time.Time{}, "header")),
			wantKind: "quota",
		},
		{
			name:     "throttle",
			writeErr: llm.NewLLMError("qwen", 429, llm.NewRateLimitError(llm.KindThrottle, kernelThrottleBody)),
			wantKind: "throttle",
		},
		{
			name:     "overload",
			writeErr: llm.NewLLMError("qwen", 529, llm.NewRateLimitError(llm.KindOverload, "")),
			wantKind: "overload",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			args := retryEventArgs(t, tc.writeErr)
			if got := args["rate_limit_kind"]; got != tc.wantKind {
				t.Fatalf("rate_limit_kind = %v, want %q", got, tc.wantKind)
			}
			// The four pre-existing fields must survive verbatim (same
			// discipline as Story 71.4 AC1: existing emits may not lose fields).
			for _, field := range []string{"step", "action", "attempt", "reason"} {
				if _, ok := args[field]; !ok {
					t.Errorf("pre-existing field %q missing from transient_retry event", field)
				}
			}
			if args["action"] != "transient_retry" {
				t.Errorf("action = %v, want transient_retry", args["action"])
			}
		})
	}
}

// TestATDD_73_1_AC6_NonRateLimitRetryOmitsField pins the other half of D6: a
// non-rate-limit transient failure carries NO rate_limit_kind at all. An
// anti-semantic "none" value would be worse than absence.
func TestATDD_73_1_AC6_NonRateLimitRetryOmitsField(t *testing.T) {
	socketErr := llm.NewLLMError("qwen", 0,
		fmt.Errorf("socket hang up: %w", llm.ErrTransient))
	args := retryEventArgs(t, socketErr)
	if _, present := args["rate_limit_kind"]; present {
		t.Fatalf("non-rate-limit transient retry must not carry rate_limit_kind, got %v", args["rate_limit_kind"])
	}
	if args["action"] != "transient_retry" {
		t.Errorf("action = %v, want transient_retry", args["action"])
	}
}

// --- §5 combination matrix: attemptFallback trigger conditions unchanged ---

// TestATDD_73_1_D5_FallbackStillTriggersOnRateLimit registers the D5 decision
// as executable fact: this story is the classification layer and leaves the
// disposition layer alone.
//
// Story 73.3 update (the call this test anticipated): terminal quota no longer
// reaches attemptFallback — the waitless fast path (D5) and the over-cap exit
// (D6) SUSPEND the process instead, and the suspend branch skips fallback by
// design (pinned by TestATDD_73_3_AC1_QuotaBeyondCap's zero-fallback-write
// assertion and the D5 fast-path test). This test now drives the rate-limit
// path that STILL exhausts its retry budget inside the cap and falls back: a
// retryable throttle with an in-cap server wait.
func TestATDD_73_1_D5_FallbackStillTriggersOnRateLimit(t *testing.T) {
	primary := &rateLimitRetryLLM{
		writeErr: llm.NewLLMError("qwen", 429,
			llm.NewRateLimitErrorWithWait(llm.KindThrottle, kernelThrottleBody, 3*time.Second, time.Time{}, "header")),
		failures: 999, // never recovers: exhaust retries, then fall back
	}
	fallback := &rateLimitRetryLLM{failures: 0}
	k, baseDir := newFailureRawKernel(t, primary, fallback, "qwen", "backup")

	agent := fallbackAgentInfo("qwen", "primary-model", "backup-model", "backup")
	pid, err := k.Spawn("73.1 D5 fallback unchanged", agent, failureRawSpawnOpts(baseDir))
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	proc, _ := k.GetProcess(pid)
	exit := waitDone(t, proc)

	// The fallback served the step, so the process completed rather than dying
	// on the primary's throttling.
	if exit.Code != 0 {
		t.Fatalf("exit = %+v, want code 0 — an in-cap rate limit that exhausts its retry budget must still reach the fallback provider", exit)
	}
	if errors.Is(exit.Err, llm.ErrRateLimitThrottle) {
		t.Errorf("exit.Err = %v, want the fallback to have recovered the step", exit.Err)
	}
}
