//go:build atdd_48_6_red && unix

package mcp

import (
	"context"
	"strings"
	"testing"
	"time"
)

// =============================================================================
// ATDD 48.6 AC3 — per-server max_output_bytes truncation
//
// Story: _bmad-output/implementation-artifacts/48-6-mount-concurrency-per-server-timeout.md
// Phase: 🔴 RED — gated `//go:build atdd_48_6_red && unix`. The unix tag scopes
//        out Windows (the bash mock servers below are unix-only). Run:
//            go test -tags=atdd_48_6_red -race -run TestATDD_48_6_02 ./drivers/mcp/
//
// Pre-impl RED signals:
//   _020/_021/_022 — COMPILE red: TransportConfig.MaxOutputBytes does not exist
//                    yet (Task 1.5). The compile failure under the tag IS the red
//                    signal (48.5 convention).
//   _023 — BEHAVIORAL red (§易错点 4): the scanner line cap is hardcoded at 4MB
//          (`sc.Buffer(..., 4*1024*1024)`); a ~5MB response hits bufio.ErrTooLong
//          and Call fails "read response" before truncation can run. The cap must
//          rise to max(4MB, MaxOutputBytes).
//
// Injection points (dev-story Task 3.1–3.3):
//   TransportConfig.MaxOutputBytes int64
//   requestLocked truncates resp.Result when len > MaxOutputBytes
//   truncation pushes a warning into t.stderrBuf (surfaced via StderrTail())
//   startProcessLocked scanner cap = max(4MB, MaxOutputBytes)
//
// Reuses mockMCPServer from transport_test.go (same package) for the tiny-result
// under-limit case.
// =============================================================================

// mockMCPBigResult completes the handshake then, for each request, emits a
// tools/call result whose `text` field is ~16KB of 'x' — far larger than the
// small MaxOutputBytes the truncation tests configure.
const mockMCPBigResult = `
read req
echo '{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05","capabilities":{},"serverInfo":{"name":"big","version":"1.0.0"}}}'
read notif
big=$(head -c 16000 < /dev/zero | tr '\0' x)
while IFS= read -r line; do
  id="${line#*\"id\":}"
  id="${id%%,*}"
  id="${id%%\}*}"
  printf '{"jsonrpc":"2.0","id":%s,"result":{"content":[{"type":"text","text":"%s"}]}}\n' "$id" "$big"
done
`

// mockMCPHugeResult emits a ~5MB single-line result — larger than the legacy 4MB
// scanner cap — to prove the scanner buffer is raised to max(4MB, MaxOutputBytes).
const mockMCPHugeResult = `
read req
echo '{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05","capabilities":{},"serverInfo":{"name":"huge","version":"1.0.0"}}}'
read notif
big=$(head -c 5000000 < /dev/zero | tr '\0' x)
while IFS= read -r line; do
  id="${line#*\"id\":}"
  id="${id%%,*}"
  id="${id%%\}*}"
  printf '{"jsonrpc":"2.0","id":%s,"result":{"content":[{"type":"text","text":"%s"}]}}\n' "$id" "$big"
done
`

// connectBig connects a transport to `script` with an explicit max_output_bytes.
func connectBig(t *testing.T, script string, maxOut int64) *StdioTransport {
	t.Helper()
	tr := NewStdioTransport(TransportConfig{
		Command:        "bash",
		Args:           []string{"-c", script},
		TimeoutMillis:  3000,
		MaxOutputBytes: maxOut, // Task 1.5 new field
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := tr.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	return tr
}

// -----------------------------------------------------------------------------
// _020: tools/call result over the limit → truncated to MaxOutputBytes, Call
//        still returns a (truncated) non-empty result with no error. (AC3)
// -----------------------------------------------------------------------------
func TestATDD_48_6_020_ToolsCall_TruncatedToMaxOutputBytes(t *testing.T) {
	const limit = int64(4096)
	tr := connectBig(t, mockMCPBigResult, limit)
	defer tr.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	result, err := tr.Call(ctx, "tools/call", nil)
	if err != nil {
		t.Fatalf("Call returned error on an oversized result; AC3 requires no crash + a returned result: %v", err)
	}
	if int64(len(result)) > limit {
		t.Errorf("result len = %d, want <= %d (truncated to max_output_bytes)", len(result), limit)
	}
	if len(result) == 0 {
		t.Error("result empty after truncation; want a truncated-but-present payload")
	}
}

// -----------------------------------------------------------------------------
// _021: truncation records a warning into the transport's stderr ring buffer,
//        visible via StderrTail() (and thus `rnix mcp logs <name>`). (AC3)
// -----------------------------------------------------------------------------
func TestATDD_48_6_021_Truncation_WarnsInStderrRing(t *testing.T) {
	const limit = int64(4096)
	tr := connectBig(t, mockMCPBigResult, limit)
	defer tr.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := tr.Call(ctx, "tools/call", nil); err != nil {
		t.Fatalf("Call: %v", err)
	}

	tail := strings.ToLower(strings.Join(tr.StderrTail(), "\n"))
	if !strings.Contains(tail, "truncat") {
		t.Errorf("StderrTail has no truncation warning after an oversized result; got:\n%s", tail)
	}
}

// -----------------------------------------------------------------------------
// _022: a result UNDER the limit is returned intact with NO truncation warning. (AC3)
//        Reuses mockMCPServer, whose tools/call result is the tiny `{}`.
// -----------------------------------------------------------------------------
func TestATDD_48_6_022_UnderLimit_NotTruncated_NoWarning(t *testing.T) {
	tr := connectBig(t, mockMCPServer, 1<<20) // 1MB limit; server returns tiny {}
	defer tr.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result, err := tr.Call(ctx, "tools/call", nil)
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if len(result) == 0 {
		t.Fatal("empty result for an under-limit call")
	}
	if tail := strings.ToLower(strings.Join(tr.StderrTail(), "\n")); strings.Contains(tail, "truncat") {
		t.Errorf("unexpected truncation warning for an under-limit result:\n%s", tail)
	}
}

// -----------------------------------------------------------------------------
// _023: with MaxOutputBytes > 4MB, the scanner cap must rise to fit the line —
//        a ~5MB result reads through (and, being under the 6MB limit, is NOT
//        truncated) instead of dying on bufio.ErrTooLong. (AC3 §易错点 4)
// -----------------------------------------------------------------------------
func TestATDD_48_6_023_ScannerGrowsBeyondDefault4MB(t *testing.T) {
	const limit = int64(6 << 20) // 6MB > legacy 4MB scanner cap
	tr := connectBig(t, mockMCPHugeResult, limit)
	defer tr.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := tr.Call(ctx, "tools/call", nil)
	if err != nil {
		t.Fatalf("Call failed on a ~5MB result with a 6MB max_output_bytes; scanner cap not raised to max(4MB, MaxOutputBytes) (§易错点 4): %v", err)
	}
	if int64(len(result)) > limit {
		t.Errorf("result len = %d exceeds limit %d", len(result), limit)
	}
	if len(result) < 4*1024*1024 {
		t.Errorf("result len = %d — looks clipped at the old 4MB scanner cap rather than read whole", len(result))
	}
}
