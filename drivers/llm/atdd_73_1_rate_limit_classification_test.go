package llm

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"google.golang.org/genai"
)

// Story 73.1 — rate-limit trichotomy and classification-layer split.
//
// Fixture provenance (see [[observability-data-provenance-principle]]):
//   - quotaBodyFixture / throttleBodyFixture / cliThrottleFixture are verbatim
//     captures from this repo's production data (1836 proc-info scan).
//   - ccUsageLimitFixture is NOT captured here — it comes from cc-src-side
//     enumeration knowledge. A grep across all 1836 samples finds no such
//     string; all six files containing "usage limit" carry cliThrottleFixture.
//     It is exercised as a forward-looking case, not as evidence.
const (
	// Captured: qwen provider, status 429 — terminal window exhaustion.
	quotaBodyFixture = "Your token-plan 1-week quota has been exhausted. The quota will reset at 08-05 22:23:00 UTC."
	// Captured: infron provider, status 429 — retryable throttle. Note the
	// epic text said "after 4 seconds"; the actual capture says 6. The
	// judgement matches the template, never the number.
	throttleBodyFixture = "Rate limit exceeded (5 requests per minute). Please try again after 6 seconds. Please visit https://infron.ai/dashboard/index (request id: 20260802071039277881832TijeWlme)"
	// Captured: reclaude provider via CLI (wicket-f5947827, pid 2155). This
	// exact line produced zero transient_retry events and killed the process
	// with exit_reason "llm write failed".
	cliThrottleFixture = "API Error: Server is temporarily limiting requests (not your usage limit) · carpool 5h quota exhausted"
	// NOT captured in this repo — cc-src enumeration knowledge only.
	ccUsageLimitFixture = "Claude usage limit reached. Your limit will reset at 3pm."
)

// --- AC4: response-body evidence split (HTTP side) ---

func TestATDD_73_1_AC4_BodyEvidenceSplit(t *testing.T) {
	cases := []struct {
		name    string
		msg     string
		errType string
		want    RateLimitKind
	}{
		{"captured qwen quota body → quota", quotaBodyFixture, "", KindQuota},
		{"captured infron throttle body → throttle", throttleBodyFixture, "", KindThrottle},
		{"structured insufficient_quota error.type → quota", "", "insufficient_quota", KindQuota},
		{"cc-src usage-limit-reached phrasing → quota", ccUsageLimitFixture, "", KindQuota},
		// Fail-open: an unrecognised 429 body is retryable, never terminal.
		{"unknown body fails open to throttle", "something went sideways", "", KindThrottle},
		{"empty body fails open to throttle", "", "", KindThrottle},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyRateLimitBody(tc.msg, tc.errType); got != tc.want {
				t.Fatalf("classifyRateLimitBody(%q, %q) = %v, want %v", tc.msg, tc.errType, got, tc.want)
			}
		})
	}
}

// TestATDD_73_1_AC4_BannedSubstrings is the counter-proof for the banned
// substring list. Ordering alone does not protect us: if the terminal marker
// set carried bare "usage limit" or bare "quota exhausted", the captured CLI
// throttle line would be classified terminal and Story 73.3 would suspend the
// process for hours — against the provider's explicit "(not your usage
// limit)". Feeding the CLI line through the HTTP-side classifier proves the
// marker set itself is precise, independent of branch order.
func TestATDD_73_1_AC4_BannedSubstrings(t *testing.T) {
	if got := classifyRateLimitBody(cliThrottleFixture, ""); got != KindThrottle {
		t.Fatalf("captured CLI throttle line classified %v, want %v — the terminal marker set has regressed to a bare substring", got, KindThrottle)
	}

	// A body containing ONLY "usage limit" (no retryable marker at all) also
	// must not reach the terminal branch: this is the case branch order cannot
	// catch, so it pins marker precision directly.
	if got := classifyRateLimitBody("hit an internal usage limit boundary", ""); got != KindThrottle {
		t.Fatalf("bare 'usage limit' body classified %v, want %v", got, KindThrottle)
	}

	// Guard the marker sets structurally so a future edit cannot reintroduce
	// the banned entries.
	for _, banned := range []string{"usage limit", "quota exhausted", "limit", "quota"} {
		for _, marker := range rateLimitQuotaMarkers {
			if marker == banned {
				t.Errorf("rateLimitQuotaMarkers must not contain banned substring %q", banned)
			}
		}
	}
}

// TestATDD_73_1_AC4_HTTPOrderingIsTerminalFirst pins the deliberate asymmetry
// between the two classifiers. HTTP driver messages carry protocol
// boilerplate: the Anthropic SDK renders every 429 as
// `POST "…": 429 Too Many Requests {…}`, and "too many requests" is a
// retryable marker. If the HTTP classifier were reordered to retryable-first
// (matching classifyCliError), terminal quota would become undetectable on
// that driver no matter what the provider body says.
func TestATDD_73_1_AC4_HTTPOrderingIsTerminalFirst(t *testing.T) {
	// The exact shape the Anthropic SDK produces for the captured quota body.
	sdkShaped := `POST "https://api.anthropic.com/v1/messages": 429 Too Many Requests {"type":"error","error":{"message":"` + quotaBodyFixture + `"}}`
	if got := classifyRateLimitBody(sdkShaped, ""); got != KindQuota {
		t.Fatalf("SDK-shaped quota message classified %v, want %v — the HTTP classifier must test terminal markers before retryable ones, or status-line boilerplate masks every quota", got, KindQuota)
	}

	// The CLI classifier keeps the opposite order, and the mixed-evidence
	// captured line must still land retryable there.
	if _, err := classifyCliError(cliThrottleFixture); !errors.Is(err, ErrRateLimitThrottle) {
		t.Fatalf("CLI classifier must stay retryable-first, got %v", err)
	}
}

// --- AC2: sentinel layering and byte-fidelity of Error() ---

func TestATDD_73_1_AC2_SentinelChain(t *testing.T) {
	throttle := NewRateLimitError(KindThrottle, throttleBodyFixture)
	quota := NewRateLimitError(KindQuota, quotaBodyFixture)
	overload := NewRateLimitError(KindOverload, "")

	// Kind-specific sentinels are reachable.
	if !errors.Is(throttle, ErrRateLimitThrottle) {
		t.Error("throttle must match ErrRateLimitThrottle")
	}
	if !errors.Is(quota, ErrQuotaExhausted) {
		t.Error("quota must match ErrQuotaExhausted")
	}
	if !errors.Is(overload, ErrServerOverloaded) {
		t.Error("overload must match ErrServerOverloaded")
	}

	// Both rate-limit kinds keep the public ErrRateLimit sentinel on the chain
	// (eight existing driver tests and ipc/http_openai.go depend on it).
	if !errors.Is(throttle, ErrRateLimit) || !errors.Is(quota, ErrRateLimit) {
		t.Error("throttle and quota must both stay errors.Is(ErrRateLimit)")
	}
	// Overload is deliberately NOT a rate limit.
	if errors.Is(overload, ErrRateLimit) {
		t.Error("overload must NOT match ErrRateLimit — 529/503 is not a quota problem")
	}

	// The two kinds must remain mutually distinguishable.
	if errors.Is(throttle, ErrQuotaExhausted) || errors.Is(quota, ErrRateLimitThrottle) {
		t.Error("throttle and quota sentinels must not match each other")
	}

	// IsTransient no longer claims rate limits (AC1).
	if IsTransient(throttle) || IsTransient(quota) || IsTransient(overload) {
		t.Error("IsTransient must not claim the rate-limit family after AC1")
	}
	if !IsRateLimited(throttle) || !IsRateLimited(quota) || !IsRateLimited(overload) {
		t.Error("IsRateLimited must claim all three kinds")
	}
	if IsRateLimited(ErrTimeout) {
		t.Error("IsRateLimited must not claim unrelated sentinels")
	}
}

// TestATDD_73_1_AC2_ErrorTextFidelity pins the exit_reason fidelity contract:
// driverErrorDetail (kernel/reason.go) takes this string's first line, and it
// is the only place the provider's quota clue survives into proc-info.json.
func TestATDD_73_1_AC2_ErrorTextFidelity(t *testing.T) {
	quota := NewRateLimitError(KindQuota, quotaBodyFixture)
	// Byte-identical to the pre-73.1 shape: "<body>: llm: rate limit exceeded".
	want := quotaBodyFixture + ": " + ErrRateLimit.Error()
	if got := quota.Error(); got != want {
		t.Fatalf("Error() = %q, want %q", got, want)
	}
	if len(want) > 200 {
		t.Fatalf("fidelity string is %d bytes, exceeding maxExitReasonDetailBytes(200); the clue would be truncated", len(want))
	}

	// The full production wrap must also stay byte-identical.
	wrapped := NewLLMError("qwen", 429, quota)
	wantWrapped := "llm [qwen] (status 429): " + want
	if got := wrapped.Error(); got != wantWrapped {
		t.Fatalf("wrapped Error() = %q, want %q", got, wantWrapped)
	}

	// Empty-body 503 (captured shape "status 503): )") must not render a
	// dangling ": " prefix.
	if got := NewRateLimitError(KindOverload, "").Error(); got != ErrServerOverloaded.Error() {
		t.Fatalf("empty-message Error() = %q, want %q", got, ErrServerOverloaded.Error())
	}
}

// TestATDD_73_1_AC2_CliErrorTextFidelity extends the byte-fidelity contract to
// the CLI path (review decision 2026-08-03). CLI drivers carry no provider
// body, so classifyCliError returns RateLimitError{Kind, Message:""} — the
// rendering must stay byte-identical to the pre-73.1 bare-sentinel text
// ("llm: rate limit exceeded"), with the kind travelling only on the Unwrap
// chain. The bare kind sentinels carry parenthesised suffixes
// ("… (retryable throttle)") that would otherwise leak into exit_reason and
// the transient_retry event's reason field.
func TestATDD_73_1_AC2_CliErrorTextFidelity(t *testing.T) {
	code, classified := classifyCliError(cliThrottleFixture)
	wrapped := NewLLMError("claude", code, classified)
	want := "llm [claude] (status 429): " + ErrRateLimit.Error()
	if got := wrapped.Error(); got != want {
		t.Fatalf("CLI throttle rendering = %q, want %q (byte-identical to pre-73.1)", got, want)
	}

	// The quota side renders the same public sentinel text: pre-73.1 had a
	// single "rate limit" branch, so both kinds shared this exact string.
	_, quotaErr := classifyCliError(quotaBodyFixture)
	if got := NewLLMError("claude", 429, quotaErr).Error(); got != want {
		t.Fatalf("CLI quota rendering = %q, want %q", got, want)
	}

	// Overload is a new classification (pre-73.1 mapped it to ErrTransient);
	// its text is the overload sentinel's own — no suffix leakage either.
	_, overloadErr := classifyCliError("upstream returned 503")
	wantOverload := "llm [claude] (status 529): " + ErrServerOverloaded.Error()
	if got := NewLLMError("claude", 529, overloadErr).Error(); got != wantOverload {
		t.Fatalf("CLI overload rendering = %q, want %q", got, wantOverload)
	}

	// The kind must still be extractable through the LLMError wrap.
	if kind, ok := RateLimitKindOf(wrapped); !ok || kind != KindThrottle {
		t.Fatalf("RateLimitKindOf(CLI throttle) = (%v, %v), want (throttle, true)", kind, ok)
	}
	if kind, ok := RateLimitKindOf(NewLLMError("claude", 429, quotaErr)); !ok || kind != KindQuota {
		t.Fatalf("RateLimitKindOf(CLI quota) = (%v, %v), want (quota, true)", kind, ok)
	}
}

// TestATDD_73_1_AC2_RateLimit73_2FieldsStayZero pinned the 73.1 scope line
// ("this story builds the fields, 73.2 fills them"). Story 73.2 has now
// landed, so the assertion is updated in place rather than deleted — the
// contract it guards has TWO halves, and both still matter:
//
//	① the bare NewRateLimitError constructor still leaves the fields zero, so
//	   every caller that has no wait evidence (the four CLI drivers, whose
//	   classifyCliError has no headers and no provider body) keeps producing
//	   zero-valued fields rather than fabricated ones;
//	② the wait-carrying constructor is the ONLY way values arrive, which is
//	   what keeps parsing on the driver side of the boundary (73.2 / D8 —
//	   the kernel reads these fields, it never writes them).
func TestATDD_73_1_AC2_RateLimit73_2FieldsStayZero(t *testing.T) {
	// ① The infron fixture literally contains "after 6 seconds" — the single
	// most tempting string to parse implicitly. The plain constructor must
	// still not do it: evidence is parsed by the caller, not the constructor.
	e := NewRateLimitError(classifyRateLimitBody(throttleBodyFixture, ""), throttleBodyFixture)
	if e.RetryAfter != 0 {
		t.Errorf("RetryAfter = %v, want 0 (the plain constructor never parses)", e.RetryAfter)
	}
	if !e.ResetAt.IsZero() {
		t.Errorf("ResetAt = %v, want zero value (the plain constructor never parses)", e.ResetAt)
	}
	if e.Source != "" {
		t.Errorf("Source = %q, want empty (the plain constructor never parses)", e.Source)
	}
	// The CLI path proves half ① is load-bearing, not hypothetical.
	_, cliErr := classifyCliError(cliThrottleFixture)
	if _, _, _, ok := RateLimitWaitOf(cliErr); ok {
		t.Error("CLI classification must carry no wait fields — it has neither headers nor a provider body")
	}

	// ② Story 73.2's constructor is where values enter, and they must survive
	// extraction through the production wrapping intact.
	resetAt := time.Date(2026, 8, 5, 22, 23, 0, 0, time.UTC)
	filled := NewRateLimitErrorWithWait(KindThrottle, throttleBodyFixture, 6*time.Second, resetAt, "header")
	wrapped := types.NewDriverError("write", "/dev/llm/infron",
		NewLLMError("infron", 429, filled), types.ErrDriver)

	ra, at, source, ok := RateLimitWaitOf(wrapped)
	if !ok {
		t.Fatal("RateLimitWaitOf must extract the wait fields through LLMError+DriverError wrapping")
	}
	if ra != 6*time.Second {
		t.Errorf("RetryAfter = %v, want 6s", ra)
	}
	if !at.Equal(resetAt) {
		t.Errorf("ResetAt = %v, want %v", at, resetAt)
	}
	if source != "header" {
		t.Errorf("Source = %q, want %q", source, "header")
	}

	// The 73.1 byte-fidelity contract is unaffected by the new fields: they
	// travel on the struct, never in Error() text (exit_reason is derived
	// from that string, capped at 200 bytes).
	if got, want := filled.Error(), throttleBodyFixture+": "+ErrRateLimit.Error(); got != want {
		t.Errorf("Error() = %q, want %q — wait fields must not leak into the rendered text", got, want)
	}
}

// --- AC2/D2: kind extraction through both carrier shapes ---

func TestATDD_73_1_AC2_RateLimitKindOf(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		wantKind RateLimitKind
		wantOK   bool
	}{
		{"structured quota", NewRateLimitError(KindQuota, quotaBodyFixture), KindQuota, true},
		{"structured throttle", NewRateLimitError(KindThrottle, throttleBodyFixture), KindThrottle, true},
		{"structured overload", NewRateLimitError(KindOverload, ""), KindOverload, true},
		// CLI drivers carry a bare sentinel (no provider body to structure).
		{"bare quota sentinel", ErrQuotaExhausted, KindQuota, true},
		{"bare throttle sentinel", ErrRateLimitThrottle, KindThrottle, true},
		{"bare overload sentinel", ErrServerOverloaded, KindOverload, true},
		{"non-rate-limit error", ErrTimeout, KindThrottle, false},
		{"nil", nil, KindThrottle, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			kind, ok := RateLimitKindOf(tc.err)
			if ok != tc.wantOK || kind != tc.wantKind {
				t.Fatalf("RateLimitKindOf() = (%v, %v), want (%v, %v)", kind, ok, tc.wantKind, tc.wantOK)
			}
		})
	}

	// String() feeds the AC6 event field; pin the wire values.
	if got := [3]string{KindThrottle.String(), KindQuota.String(), KindOverload.String()}; got != [3]string{"throttle", "quota", "overload"} {
		t.Fatalf("kind strings = %v, want [throttle quota overload]", got)
	}
}

// TestATDD_73_1_D2_WrapPenetration reproduces the production wrapping depth.
// The kernel never sees the driver's raw error: it is wrapped by *LLMError and
// again at the VFS boundary. If errors.Is/As stop penetrating those layers,
// every downstream story in this epic silently no-ops.
func TestATDD_73_1_D2_WrapPenetration(t *testing.T) {
	inner := NewLLMError("qwen", 429, NewRateLimitError(KindQuota, quotaBodyFixture))
	// vfs.newVFSError is unexported and vfs imports would cycle; *DriverError
	// has the identical Unwrap shape and is the sanctioned stand-in (the story
	// names it explicitly).
	wrapped := types.NewDriverError("write", "/dev/llm/qwen", inner, types.ErrDriver)

	if !errors.Is(wrapped, ErrQuotaExhausted) {
		t.Fatal("errors.Is must penetrate DriverError→LLMError→RateLimitError to the quota sentinel")
	}
	if !errors.Is(wrapped, ErrRateLimit) {
		t.Fatal("the public ErrRateLimit sentinel must stay reachable through the wraps")
	}
	if !IsRateLimited(wrapped) {
		t.Fatal("IsRateLimited must see through the production wrapping")
	}
	kind, ok := RateLimitKindOf(wrapped)
	if !ok || kind != KindQuota {
		t.Fatalf("RateLimitKindOf(wrapped) = (%v, %v), want (quota, true)", kind, ok)
	}
	// The structured payload must remain extractable for Story 73.2.
	var rle *RateLimitError
	if !errors.As(wrapped, &rle) {
		t.Fatal("errors.As must extract *RateLimitError through the wraps")
	}
	if rle.Message != quotaBodyFixture {
		t.Errorf("Message = %q, want the provider body verbatim", rle.Message)
	}
	// And the provider clue must survive into the rendered text.
	if !strings.Contains(wrapped.Error(), "quota will reset") {
		t.Errorf("wrapped.Error() = %q, want the provider quota clue preserved", wrapped.Error())
	}
}

// --- AC3: 529 / 503 as their own status cases, across all four HTTP drivers ---

func TestATDD_73_1_AC3_AnthropicOverloadStatuses(t *testing.T) {
	d := NewAnthropicDriver("anthropic-test", WithAnthropicKey("k"))
	for _, status := range []int{529, 503} {
		t.Run(fmt.Sprintf("status_%d", status), func(t *testing.T) {
			// Empty body: the captured 503 shape carries no text at all, so
			// only the status case can classify it.
			err := d.classifyError(makeAnthropicAPIError(status))
			assertOverload(t, err, status)
		})
	}
}

func TestATDD_73_1_AC3_GeminiOverloadStatuses(t *testing.T) {
	d := NewGeminiDriver("gemini-test")
	for _, status := range []int{529, 503} {
		t.Run(fmt.Sprintf("status_%d", status), func(t *testing.T) {
			err := d.classifyError(genai.APIError{Code: status, Message: ""})
			assertOverload(t, err, status)
		})
	}
}

// The compat-driver overload-statuses test was deleted (Story 75.3): the
// deleted compat driver no longer exists, and the unified openai driver's
// overload classification is covered by TestATDD_73_1_AC3_OpenAIOfficialOverloadStatuses.

func TestATDD_73_1_AC3_OpenAIOfficialOverloadStatuses(t *testing.T) {
	for _, status := range []int{529, 503} {
		t.Run(fmt.Sprintf("status_%d", status), func(t *testing.T) {
			d, _, cleanup := newTestOpenAIDriver(func(w http.ResponseWriter, r *http.Request) {
				writeJSONStatus(w, status, `{"error":{"message":""}}`)
			})
			defer cleanup()
			_, err := d.Call(context.Background(), LLMRequest{Intent: "hi"})
			assertOverload(t, err, status)
		})
	}
}

func assertOverload(t *testing.T, err error, status int) {
	t.Helper()
	if err == nil {
		t.Fatalf("status %d: expected an error", status)
	}
	if !errors.Is(err, ErrServerOverloaded) {
		t.Fatalf("status %d: err = %v, want ErrServerOverloaded", status, err)
	}
	if errors.Is(err, ErrRateLimit) {
		t.Fatalf("status %d: overload must not be classified as a rate limit", status)
	}
	kind, ok := RateLimitKindOf(err)
	if !ok || kind != KindOverload {
		t.Fatalf("status %d: RateLimitKindOf = (%v, %v), want (overload, true)", status, kind, ok)
	}
}

// --- AC4 wiring: captured 429 bodies reach the right kind through the drivers ---

func TestATDD_73_1_AC4_DriverWiring429(t *testing.T) {
	cases := []struct {
		name string
		body string
		want RateLimitKind
	}{
		{"quota body", quotaBodyFixture, KindQuota},
		{"throttle body", throttleBodyFixture, KindThrottle},
	}
	for _, tc := range cases {
		t.Run("openai/"+tc.name, func(t *testing.T) {
			d, _, cleanup := newTestOpenAIDriver(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(429)
				fmt.Fprintf(w, `{"error":{"message":%q}}`, tc.body)
			})
			defer cleanup()
			_, err := d.Call(context.Background(), LLMRequest{Intent: "hi"})
			assertKind(t, err, tc.want)
			// Public sentinel preserved for existing consumers.
			if !errors.Is(err, ErrRateLimit) {
				t.Errorf("err = %v, want errors.Is(ErrRateLimit)", err)
			}
		})
		t.Run("anthropic/"+tc.name, func(t *testing.T) {
			d := NewAnthropicDriver("anthropic-test", WithAnthropicKey("k"))
			err := d.classifyError(makeAnthropicAPIErrorWithMsg(429, tc.body))
			assertKind(t, err, tc.want)
		})
		t.Run("gemini/"+tc.name, func(t *testing.T) {
			d := NewGeminiDriver("gemini-test")
			err := d.classifyError(genai.APIError{Code: 429, Message: tc.body})
			assertKind(t, err, tc.want)
		})
	}

	// openai can also read the provider's structured error.type, which
	// is authoritative even when the message is unhelpful.
	t.Run("openai/structured insufficient_quota", func(t *testing.T) {
		d, _, cleanup := newTestOpenAIDriver(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(429)
			fmt.Fprint(w, `{"error":{"message":"You exceeded your current plan.","type":"insufficient_quota"}}`)
		})
		defer cleanup()
		_, err := d.Call(context.Background(), LLMRequest{Intent: "hi"})
		assertKind(t, err, KindQuota)
	})
}

func assertKind(t *testing.T, err error, want RateLimitKind) {
	t.Helper()
	if err == nil {
		t.Fatal("expected an error")
	}
	kind, ok := RateLimitKindOf(err)
	if !ok {
		t.Fatalf("err = %v, want a classified rate-limit error", err)
	}
	if kind != want {
		t.Fatalf("kind = %v, want %v (err = %v)", kind, want, err)
	}
}

// --- AC5: CLI classifier ---

// TestATDD_73_1_AC5_CliThrottleFixture is the regression lock for the recorded
// death: this exact line matched none of the six old branches, fell to
// (0, nil), produced zero retries and killed the process. It must now land on
// the RETRYABLE side — the provider says "(not your usage limit)", so calling
// it terminal would be worse than the original bug.
func TestATDD_73_1_AC5_CliThrottleFixture(t *testing.T) {
	code, sentinel := classifyCliError(cliThrottleFixture)
	if code != 429 {
		t.Errorf("code = %d, want 429", code)
	}
	if !errors.Is(sentinel, ErrRateLimitThrottle) {
		t.Fatalf("sentinel = %v, want ErrRateLimitThrottle", sentinel)
	}
	if errors.Is(sentinel, ErrQuotaExhausted) {
		t.Fatal("captured CLI throttle line must NOT be classified as terminal quota")
	}
	if !errors.Is(sentinel, ErrRateLimit) {
		t.Error("public ErrRateLimit sentinel must stay reachable")
	}
}

func TestATDD_73_1_AC5_CliClassificationTable(t *testing.T) {
	cases := []struct {
		name     string
		msg      string
		wantCode int
		wantErr  error
	}{
		{"captured CLI throttle", cliThrottleFixture, 429, ErrRateLimitThrottle},
		{"CLI rate limited phrasing", "API rate limited", 429, ErrRateLimitThrottle},
		{"captured throttle body", throttleBodyFixture, 429, ErrRateLimitThrottle},
		{"captured quota body", quotaBodyFixture, 429, ErrQuotaExhausted},
		// cc-src enumeration knowledge, not a capture from this repo.
		{"cc-src usage limit reached", ccUsageLimitFixture, 429, ErrQuotaExhausted},
		{"overloaded", "Error: model overloaded", 529, ErrServerOverloaded},
		{"529 in text", "upstream returned 529", 529, ErrServerOverloaded},
		{"503 in text", "upstream returned 503", 529, ErrServerOverloaded},
		{"socket", "socket hang up", 0, ErrTransient},
		{"connection", "connection reset by peer", 0, ErrTransient},
		{"auth", "authentication failed", 401, ErrAuth},
		{"api key", "Invalid API key provided", 401, ErrAuth},
		{"api-key", "missing api-key header", 401, ErrAuth},
		{"apikey", "bad apikey", 401, ErrAuth},
		// Every provider env var in this repo is *_API_KEY; CLIs quote them
		// verbatim, so the underscore shape is a real auth message form.
		{"api_key underscore env form", "Error: ANTHROPIC_API_KEY environment variable not set", 401, ErrAuth},
		{"context length", "prompt is too long", 400, ErrContextLength},
		{"unclassified", "something else entirely", 0, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			code, err := classifyCliError(tc.msg)
			if code != tc.wantCode {
				t.Errorf("code = %d, want %d", code, tc.wantCode)
			}
			if tc.wantErr == nil {
				if err != nil {
					t.Errorf("err = %v, want nil", err)
				}
				return
			}
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("err = %v, want errors.Is(%v)", err, tc.wantErr)
			}
		})
	}
}

// TestATDD_73_1_AC5_KeyBranchNarrowed pins D7: the "key" branch is narrowed,
// not deleted. Deleting it would drop real auth failures into the retry path
// (ErrAuth is a permanent-error veto in isTransientLLMError); leaving it wide
// made any message mentioning a key look like an auth failure.
func TestATDD_73_1_AC5_KeyBranchNarrowed(t *testing.T) {
	if _, err := classifyCliError("Invalid API key"); !errors.Is(err, ErrAuth) {
		t.Errorf("genuine auth failure must still map to ErrAuth, got %v", err)
	}
	code, err := classifyCliError("missing key in response payload")
	if errors.Is(err, ErrAuth) {
		t.Errorf("bare 'key' must no longer imply auth, got (%d, %v)", code, err)
	}
}
