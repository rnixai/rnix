//go:build unix

package mcp

import (
	"context"
	"strings"
	"testing"
	"time"
)

// =============================================================================
// ATDD 48.5 AC4 — stderr ring buffer capture (Task 6)
//
// Story: _bmad-output/implementation-artifacts/48-5-mcp-health-check-stderr-reconnect.md
// Phase: 🔴 RED — `//go:build atdd_48_5_red && unix`. Run:
//            go test -tags=atdd_48_5_red -race -run TestATDD_48_5_03 ./drivers/mcp/
//
// The transport must attach cmd.StderrPipe() on Connect (and each reconnect),
// pump lines into a transport-held RingBuffer[string] (cap 256) via a
// bufio.Scanner goroutine, and expose them through StderrTail().
//
// Injection points (dev-story Task 6):
//   (*StdioTransport).StderrTail() []string
//   transport-held *xsync.RingBuffer[string] (cap 256), survives reconnect
// =============================================================================

// mockMCPStderr completes the handshake, emits 3 diagnostic lines to stderr,
// then serves the echo loop. stdout stays valid JSON-RPC; stderr is the
// out-of-band diagnostic channel the ring buffer must capture.
const mockMCPStderr = `
read req
echo '{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05","capabilities":{},"serverInfo":{"name":"mock","version":"1.0.0"}}}'
echo "diag line 1" >&2
echo "diag line 2" >&2
echo "diag line 3" >&2
read notif
while IFS= read -r line; do
  id="${line#*\"id\":}"
  id="${id%%,*}"
  id="${id%%\}*}"
  echo "{\"jsonrpc\":\"2.0\",\"id\":${id},\"result\":{}}"
done
`

// mockMCPStderrFlood writes 300 stderr lines (> 256 cap) to exercise eviction.
const mockMCPStderrFlood = `
read req
echo '{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05","capabilities":{},"serverInfo":{"name":"mock","version":"1.0.0"}}}'
read notif
for i in $(seq 1 300); do echo "diag $i" >&2; done
while IFS= read -r line; do
  id="${line#*\"id\":}"
  id="${id%%,*}"
  id="${id%%\}*}"
  echo "{\"jsonrpc\":\"2.0\",\"id\":${id},\"result\":{}}"
done
`

// waitForStderr polls StderrTail until it has at least `min` lines or the
// deadline expires (stderr capture is async via the scanner goroutine).
func waitForStderr(t *testing.T, tr *StdioTransport, min int, timeout time.Duration) []string {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var tail []string
	for time.Now().Before(deadline) {
		tail = tr.StderrTail()
		if len(tail) >= min {
			return tail
		}
		time.Sleep(20 * time.Millisecond)
	}
	return tail
}

// -----------------------------------------------------------------------------
// _030: stderr lines written by the child are captured into the ring buffer
//        and surfaced via StderrTail(). (AC4)
// -----------------------------------------------------------------------------
func TestATDD_48_5_030_Stderr_CaptureLines(t *testing.T) {
	tr := connectMock(t, mockMCPStderr)
	defer tr.Close()

	tail := waitForStderr(t, tr, 3, 3*time.Second)
	if len(tail) < 3 {
		t.Fatalf("StderrTail captured %d lines, want ≥3: %v", len(tail), tail)
	}
	joined := strings.Join(tail, "\n")
	for _, want := range []string{"diag line 1", "diag line 2", "diag line 3"} {
		if !strings.Contains(joined, want) {
			t.Errorf("StderrTail missing %q; got:\n%s", want, joined)
		}
	}
}

// -----------------------------------------------------------------------------
// _031: > 256 stderr lines → ring buffer keeps only the most recent 256,
//        evicting the oldest. (AC4 + Task 1.2 capacity bound)
// -----------------------------------------------------------------------------
func TestATDD_48_5_031_Stderr_RingOverwrite256(t *testing.T) {
	tr := connectMock(t, mockMCPStderrFlood)
	defer tr.Close()

	tail := waitForStderr(t, tr, 256, 5*time.Second)
	if len(tail) != 256 {
		t.Fatalf("StderrTail len = %d, want exactly 256 (ring cap)", len(tail))
	}
	joined := strings.Join(tail, "\n")
	// Oldest 44 (diag 1..44) must have been evicted; newest (diag 300) retained.
	if strings.Contains(joined, "diag 1\n") || strings.HasPrefix(joined, "diag 1") {
		t.Errorf("oldest line 'diag 1' not evicted from 256-cap ring")
	}
	if !strings.Contains(joined, "diag 300") {
		t.Errorf("newest line 'diag 300' missing from ring tail")
	}
}

// -----------------------------------------------------------------------------
// _032: ring buffer survives reconnect — history retained + new lines appended
//        (NOT cleared). (AC4 "ring buffer 在 transport reconnect 后保留历史 + 续写")
// -----------------------------------------------------------------------------
func TestATDD_48_5_032_Stderr_PreservedAcrossReconnect(t *testing.T) {
	tr := NewStdioTransport(TransportConfig{
		Command:         "bash",
		Args:            []string{"-c", mockMCPStderr},
		TimeoutMillis:   3000,
		ReconnectPolicy: fastBackoff(3),
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := tr.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer tr.Close()

	pre := waitForStderr(t, tr, 3, 3*time.Second)
	if len(pre) < 3 {
		t.Fatalf("pre-reconnect StderrTail = %d lines, want ≥3", len(pre))
	}

	// Reconnect re-execs the SAME script → 3 more identical diag lines. The ring
	// buffer is a transport field (not a cmd field), so the original 3 must NOT
	// be cleared — total must grow.
	rctx, rcancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer rcancel()
	if err := tr.reconnect(rctx); err != nil {
		t.Fatalf("reconnect: %v", err)
	}

	post := waitForStderr(t, tr, 6, 3*time.Second)
	if len(post) <= len(pre) {
		t.Errorf("StderrTail did not grow across reconnect: pre=%d post=%d (ring cleared instead of preserved)", len(pre), len(post))
	}
	// Earliest captured line must still be present (crash-before/after diagnostics).
	if post[0] != "diag line 1" {
		t.Errorf("oldest pre-reconnect line lost: post[0]=%q, want %q", post[0], "diag line 1")
	}
}
