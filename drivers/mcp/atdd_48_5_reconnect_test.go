//go:build unix

package mcp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// =============================================================================
// ATDD 48.5 AC3 — reconnecter backoff (Task 4)
//
// Story: _bmad-output/implementation-artifacts/48-5-mcp-health-check-stderr-reconnect.md
// Phase: 🔴 RED — `//go:build atdd_48_5_red && unix`. Run:
//            go test -tags=atdd_48_5_red -race -run TestATDD_48_5_02 ./drivers/mcp/
//
// Design: these tests drive the (unexported, same-package) reconnect(ctx)
// method DIRECTLY rather than going through the L1/L2 trigger path. Per the
// Story §健康检查时序 diagram, L1 failure returns ErrDeviceDisconnected without
// inline reconnect, and only L2-ping-failure triggers reconnect — so driving
// the reconnecter directly isolates AC3's backoff semantics from the trigger
// ambiguity. The trigger wiring is covered behaviorally by health_test.go _013.
//
// Injection points (dev-story Task 4):
//   type ReconnectPolicy struct { MaxRetries int; Backoff []time.Duration }
//   TransportConfig.ReconnectPolicy ReconnectPolicy   // zero → defaults
//   (*StdioTransport).reconnect(ctx context.Context) error
//   (*StdioTransport).ReconnectCount() int
//   (*StdioTransport).ToolCount() int
//   (*StdioTransport).Status() vfs.MCPStatus  (+ MCPStatusBackoffExhausted)
// =============================================================================

// mockMCPWithTools answers every post-handshake request with a fixed tools +
// resources payload so the reconnecter's tools/list refresh yields ToolCount=2.
const mockMCPWithTools = `
read req
echo '{"jsonrpc":"2.0","id":1,"result":{"protocolVersion":"2024-11-05","capabilities":{},"serverInfo":{"name":"mock","version":"1.0.0"}}}'
read notif
while IFS= read -r line; do
  id="${line#*\"id\":}"
  id="${id%%,*}"
  id="${id%%\}*}"
  echo "{\"jsonrpc\":\"2.0\",\"id\":${id},\"result\":{\"tools\":[{\"name\":\"a\"},{\"name\":\"b\"}],\"resources\":[{\"uri\":\"r1\"}]}}"
done
`

// fastBackoff is a 3-attempt policy with millisecond delays so exhaustion
// resolves in ~7ms instead of 7s (Story §测试策略 "reconnect 退避加速").
func fastBackoff(maxRetries int) ReconnectPolicy {
	return ReconnectPolicy{
		MaxRetries: maxRetries,
		Backoff:    []time.Duration{1 * time.Millisecond, 2 * time.Millisecond, 4 * time.Millisecond},
	}
}

func connectWithTools(t *testing.T, policy ReconnectPolicy) *StdioTransport {
	t.Helper()
	tr := NewStdioTransport(TransportConfig{
		Command:         "bash",
		Args:            []string{"-c", mockMCPWithTools},
		TimeoutMillis:   3000,
		ReconnectPolicy: policy,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := tr.Connect(ctx); err != nil {
		t.Fatalf("Connect: %v", err)
	}
	return tr
}

// -----------------------------------------------------------------------------
// _020: reconnect success → re-exec child + initialize + tools/list refresh,
//        Status=Connected, ToolCount updated, ReconnectCount incremented. (AC3)
// -----------------------------------------------------------------------------
func TestATDD_48_5_020_Reconnect_Success_RefreshesToolsList(t *testing.T) {
	tr := connectWithTools(t, fastBackoff(3))
	defer tr.Close()

	before := tr.ReconnectCount()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := tr.reconnect(ctx); err != nil {
		t.Fatalf("reconnect: %v", err)
	}

	if tr.Status() != vfs.MCPStatusConnected {
		t.Errorf("Status = %v, want Connected after successful reconnect", tr.Status())
	}
	if got := tr.ToolCount(); got != 2 {
		t.Errorf("ToolCount = %d, want 2 (tools/list refreshed on reconnect)", got)
	}
	if tr.ReconnectCount() != before+1 {
		t.Errorf("ReconnectCount = %d, want %d (must increment on success)", tr.ReconnectCount(), before+1)
	}
}

// -----------------------------------------------------------------------------
// _021: backoff exhausted — re-exec keeps failing → 3 attempts → Status =
//        BackoffExhausted. (AC3)
// -----------------------------------------------------------------------------
func TestATDD_48_5_021_Reconnect_BackoffExhausted_Status(t *testing.T) {
	tr := connectWithTools(t, fastBackoff(3))
	defer tr.Close()

	// Sabotage the relaunch command so every reconnect attempt fails fast.
	tr.config.Command = "/nonexistent/mcp-binary-48-5"
	tr.config.Args = nil

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start := time.Now()
	err := tr.reconnect(ctx)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("reconnect against a broken command succeeded; want failure")
	}
	if tr.Status() != vfs.MCPStatusBackoffExhausted {
		t.Errorf("Status = %v, want BackoffExhausted after 3 failed attempts", tr.Status())
	}
	// With ms backoff, 3 attempts must complete well under 1s (NOT the 1+2+4=7s
	// production schedule) — proves the injected fast policy is honoured.
	if elapsed > 1*time.Second {
		t.Errorf("exhaustion took %v — injected fast backoff not applied", elapsed)
	}
}

// -----------------------------------------------------------------------------
// _022: after BackoffExhausted, subsequent Call returns an error IMMEDIATELY
//        (no retry, no 30s block). (AC3 "后续 tool call 立即返回错误")
// -----------------------------------------------------------------------------
func TestATDD_48_5_022_BackoffExhausted_CallFastFails(t *testing.T) {
	tr := connectWithTools(t, fastBackoff(3))
	defer tr.Close()

	tr.config.Command = "/nonexistent/mcp-binary-48-5"
	tr.config.Args = nil

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = tr.reconnect(ctx) // drive into BackoffExhausted
	if tr.Status() != vfs.MCPStatusBackoffExhausted {
		t.Fatalf("precondition failed: Status = %v, want BackoffExhausted", tr.Status())
	}

	callCtx, callCancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer callCancel()
	start := time.Now()
	_, err := tr.Call(callCtx, "tools/call", nil)
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Call in BackoffExhausted state succeeded; want immediate error")
	}
	var de *types.DriverError
	if !errors.As(err, &de) {
		t.Errorf("error = %v; want *types.DriverError", err)
	}
	if elapsed > 1*time.Second {
		t.Errorf("Call took %v — BackoffExhausted state must fast-fail, not block on stdio scan", elapsed)
	}
}

// -----------------------------------------------------------------------------
// _023: ReconnectCount is monotonic across multiple successful reconnects.
//        (AC3 — kernel relies on the delta to emit mcp.reconnect exactly once.)
// -----------------------------------------------------------------------------
func TestATDD_48_5_023_ReconnectCount_Monotonic(t *testing.T) {
	tr := connectWithTools(t, fastBackoff(3))
	defer tr.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	start := tr.ReconnectCount()
	for i := range 2 {
		if err := tr.reconnect(ctx); err != nil {
			t.Fatalf("reconnect #%d: %v", i, err)
		}
	}
	if got := tr.ReconnectCount(); got != start+2 {
		t.Errorf("ReconnectCount = %d, want %d after 2 successful reconnects", got, start+2)
	}
}
