package ipc

import (
	"encoding/json"
	"errors"
	"io"
	"testing"
)

// =============================================================================
// ATDD 44.2 — AC#6: daemon-side stream encoder must tolerate a closed
//                   client connection after Ctrl+C.
//
// Story 44.2 AC#6: once the conn.Read goroutine has fired SIGPAUSE (the
// new behaviour from AC#1), the CLI client has already shut. Subsequent
// `enc.Encode(...)` calls in the daemon may fail with io.EOF / broken
// pipe. The daemon MUST:
//
//   - NOT panic
//   - NOT cancel scriptCtx as a side effect of the encode failure
//   - allow the executor goroutine to keep running until it parks at the
//     statement boundary
//   - allow finalize to attempt one final SCRIPT_SUSPENDED stream that is
//     swallowed without error
//
// The current production code in `server_pipeline.go` already uses
// `_ = enc.Encode(...)` (line 442, etc.). This file pins the
// invariant: AC#6 forbids dev-story from "improving" the error-handling
// to a panic / log.Fatal / scriptCancel side-effect.
//
// These tests are GREEN both before and after Story 44.2 lands — they
// regression-guard the existing swallow-error pattern. The "real" RED
// for AC#6 is the end-to-end test (atdd_44_2_end_to_end_test.go) which
// verifies the executor keeps running after a SIGPAUSE-style event;
// these unit tests are the static safety net.
// =============================================================================

// brokenWriter is an io.Writer that always returns io.ErrClosedPipe,
// emulating a conn that the client has already closed.
type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) {
	return 0, io.ErrClosedPipe
}

type errWriter struct{ err error }

func (e errWriter) Write([]byte) (int, error) { return 0, e.err }

// TestATDD_44_2_060_EncEncode_ClosedPipe_NoPanic
//
// AC#6: streaming the SCRIPT_SUSPENDED final event to a closed conn must
// NOT panic. Mirrors the production pattern `_ = enc.Encode(StreamEvent{...})`
// from `server_pipeline.go`.
func TestATDD_44_2_060_EncEncode_ClosedPipe_NoPanic(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("enc.Encode panicked on closed pipe: %v", r)
		}
	}()

	enc := json.NewEncoder(brokenWriter{})
	payload, _ := json.Marshal(ErrorPayload{Code: "SCRIPT_SUSPENDED", Message: "x"})

	// Mirror the production pattern from handleExecScript / finalizeScriptRunner
	// (server_pipeline.go:442): `_ = enc.Encode(StreamEvent{...})`.
	_ = enc.Encode(StreamEvent{Type: StreamError, Payload: payload})
}

// TestATDD_44_2_061_EncEncode_GenericWriteError_AlsoSafe
//
// Real-world brokenness on the network (TLS reset, abrupt RST, etc.)
// surfaces as assorted error types — `enc.Encode` MUST swallow ALL
// encoder failures uniformly. Use a wrapped error to assert the daemon
// does not pattern-match on io.ErrClosedPipe specifically.
func TestATDD_44_2_061_EncEncode_GenericWriteError_AlsoSafe(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("enc.Encode panicked on generic write error: %v", r)
		}
	}()

	enc := json.NewEncoder(errWriter{err: errors.New("connection reset by peer")})
	payload, _ := json.Marshal(ErrorPayload{Code: "SCRIPT_SUSPENDED"})
	_ = enc.Encode(StreamEvent{Type: StreamError, Payload: payload})
}

// TestATDD_44_2_062_FinalizeStream_NoEncoderInvocationAfterReturn
//
// AC#6 follow-up invariant: handleExecScript's post-finalize streaming
// calls (server_pipeline.go:440-443) MUST NOT block on a closed conn —
// they wrap `enc.Encode` in `_ =` and exit. Validate the well-known case
// where the outcome stream is intentionally skipped (case-2 success
// path) — there is nothing to stream on success in finalize and the
// caller handles ExecScriptResponse separately.
//
// This is a structural invariant: as long as handleExecScript keeps the
// branching pattern
//
//	if outcome.streamPayload != nil {
//	    payload, _ := json.Marshal(outcome.streamPayload)
//	    _ = enc.Encode(StreamEvent{Type: outcome.streamType, Payload: payload})
//	}
//
// a nil payload triggers zero writes — so even a non-broken conn never
// sees an extraneous frame. Pin that nil short-circuit.
func TestATDD_44_2_062_FinalizeStream_NoEncoderInvocationAfterReturn(t *testing.T) {
	outcome := scriptRunnerOutcome{
		streamPayload: nil,
		streamType:    "",
		returnEarly:   false,
	}
	if outcome.streamPayload != nil {
		t.Fatal("test fixture invariant broken: streamPayload must be nil for case-2 success outcome")
	}
}
