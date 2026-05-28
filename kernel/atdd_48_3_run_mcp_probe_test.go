package kernel

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/rnixai/rnix/vfs"
)

// =============================================================================
// Story 48.3 AC2 (kernel side) — RunMCPProbe 一次性探针实现
//
// **RED PHASE — Skip until Story 48.3 Task 2 lands.**
//
// kernel_mcp_test.go already drives the contract surface (SetTransportFactory
// + RunMCPProbe basic shape). This file complements it with three deeper
// scenarios that the IPC handleMCPTest (Task 3.4) and CLI runMCPTest
// (Task 4.3) rely on:
//
//   _022 — partial failure: tools/list OK, resources/list & prompts/list
//          fail; res.OK stays true (resources/prompts unsupported is OK)
//   _023 — initialize timeout: connect blocks past 10s deadline → stage
//          marked timeout, downstream stages skipped, transport Close still
//          invoked via defer
//   _024 — server_info passthrough: when Connect populates the transport's
//          internal serverInfo (proxied through a sentinel hook), the
//          MCPProbeResult.ServerInfo field carries it. AC2 Story Dev Notes
//          §决策 7 — current Story keeps this as best-effort empty (will fill
//          when transport.go grows a getter); test pins the contract so any
//          future plumbing is verified.
//
// Note: kernel_mcp_test.go is true-RED (compile fail) which means the kernel
// test binary won't build until Task 6 lands. Once it does, removing
// `t.Skip` here turns these tests on.
// =============================================================================

// -----------------------------------------------------------------------------
// _022: resources/list and prompts/list errors don't fail the overall probe
// -----------------------------------------------------------------------------
func TestATDD_48_3_022_RunMCPProbe_OptionalCallsFail_OverallOK(t *testing.T) {
	tp := &probeMockTransport{
		toolsResp: json.RawMessage(`{"tools":[{"name":"echo"}]}`),
		callErr: map[string]error{
			"resources/list": errors.New("method not supported"),
			"prompts/list":   errors.New("method not supported"),
		},
	}
	k := newKernelOnly(t)
	k.SetTransportFactory(func(_ vfs.MCPConfig) (vfs.MCPTransport, error) { return tp, nil })

	cfg := vfs.MCPConfig{ServerName: "minimal", Command: "minimal-mcp"}
	ctx, cancel := context.WithTimeout(context.Background(), 11*time.Second)
	defer cancel()
	res := k.RunMCPProbe(ctx, cfg)

	if !res.OK {
		t.Errorf("res.OK = false; resources/list + prompts/list failures should NOT fail overall (Story §决策 6)")
	}
	if res.Tools != 1 {
		t.Errorf("res.Tools = %d, want 1", res.Tools)
	}
	if res.Resources != 0 {
		t.Errorf("res.Resources = %d, want 0 (unsupported → 0)", res.Resources)
	}
	if res.Prompts != 0 {
		t.Errorf("res.Prompts = %d, want 0", res.Prompts)
	}

	// Verify per-stage status records the failure even though overall is OK.
	var resStage, prmStage *MCPProbeStage
	for i := range res.Stages {
		switch res.Stages[i].Name {
		case "resources_list":
			resStage = &res.Stages[i]
		case "prompts_list":
			prmStage = &res.Stages[i]
		}
	}
	if resStage == nil || resStage.OK {
		t.Errorf("resources_list stage OK = true or missing; want OK=false to surface real error")
	}
	if prmStage == nil || prmStage.OK {
		t.Errorf("prompts_list stage OK = true or missing")
	}
	if !tp.closed {
		t.Error("transport.Close() not called by RunMCPProbe defer")
	}
}

// -----------------------------------------------------------------------------
// _023: connect hangs past 10s ctx deadline → timeout stage + Close still runs
// -----------------------------------------------------------------------------
func TestATDD_48_3_023_RunMCPProbe_ConnectTimeout(t *testing.T) {
	tp := &hangingMockTransport{hangFor: 12 * time.Second}
	k := newKernelOnly(t)
	k.SetTransportFactory(func(_ vfs.MCPConfig) (vfs.MCPTransport, error) { return tp, nil })

	cfg := vfs.MCPConfig{ServerName: "slow", Command: "slow-mcp"}

	// Caller passes a parent ctx with a 12s budget; RunMCPProbe should derive
	// its OWN 10s deadline. After ~10s the connect stage records timeout and
	// the function returns — caller's ctx still has ~2s left.
	parent, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()

	start := time.Now()
	res := k.RunMCPProbe(parent, cfg)
	elapsed := time.Since(start)

	if elapsed > 11*time.Second {
		t.Errorf("RunMCPProbe took %v > 11s — internal 10s deadline broken", elapsed)
	}
	if elapsed < 9*time.Second {
		t.Errorf("RunMCPProbe took %v < 9s — timeout path skipped (transport returned too early)", elapsed)
	}

	if res.OK {
		t.Error("res.OK = true after timeout; want false")
	}
	if len(res.Stages) == 0 {
		t.Fatal("res.Stages empty after timeout")
	}
	if res.Stages[0].Name != "connect" {
		t.Errorf("stage[0].Name = %q, want \"connect\"", res.Stages[0].Name)
	}
	if res.Stages[0].OK {
		t.Error("connect stage OK = true after timeout")
	}
	if res.Stages[0].Error == "" {
		t.Error("connect stage Error empty; want timeout description")
	}

	// AC2 §"Close 仍 defer 触发" — transport must have been closed.
	if !tp.closed {
		t.Error("transport.Close() not called after timeout — leak risk")
	}
}

// -----------------------------------------------------------------------------
// _024: ServerInfo passthrough is best-effort (current Story 48.3 §决策 7)
// -----------------------------------------------------------------------------
func TestATDD_48_3_024_RunMCPProbe_ServerInfo_BestEffort(t *testing.T) {
	tp := &probeMockTransport{
		toolsResp: json.RawMessage(`{"tools":[]}`),
	}
	k := newKernelOnly(t)
	k.SetTransportFactory(func(_ vfs.MCPConfig) (vfs.MCPTransport, error) { return tp, nil })

	cfg := vfs.MCPConfig{ServerName: "anyserver", Command: "ok"}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	res := k.RunMCPProbe(ctx, cfg)

	if !res.OK {
		t.Fatalf("res.OK = false; want true for healthy mock")
	}
	// Story §决策 7: ServerInfo can be empty in this Story; this test only
	// pins the field's existence (compile-level) + best-effort contract:
	// if non-empty, must be human-readable (contains "@" / space / colon
	// for typical "<name> v<version>" formats). Empty is acceptable here.
	if res.ServerInfo != "" {
		// Cheap sanity: non-empty should at least not contain raw JSON braces.
		if containsCh(res.ServerInfo, '{') || containsCh(res.ServerInfo, '}') {
			t.Errorf("ServerInfo = %q looks like raw JSON; want human-readable", res.ServerInfo)
		}
	}
}

// -----------------------------------------------------------------------------
// Local helpers (RunMCPProbe-specific timing mock; differs from
// kernel_mcp_test.go's probeMockTransport which has no hangFor knob)
// -----------------------------------------------------------------------------

type hangingMockTransport struct {
	hangFor time.Duration
	closed  bool
}

func (m *hangingMockTransport) Connect(ctx context.Context) error {
	select {
	case <-time.After(m.hangFor):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (m *hangingMockTransport) Call(ctx context.Context, _ string, _ json.RawMessage) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}

func (m *hangingMockTransport) Close() error                 { m.closed = true; return nil }
func (m *hangingMockTransport) Ping(_ context.Context) error { return nil }

// --- Story 48.5 health/status surface (no-op defaults for legacy mocks) ---
func (m *hangingMockTransport) Status() vfs.MCPStatus { return vfs.MCPStatusConnected }
func (m *hangingMockTransport) Alive() bool           { return true }
func (m *hangingMockTransport) ToolCount() int        { return 0 }
func (m *hangingMockTransport) ResourceCount() int    { return 0 }
func (m *hangingMockTransport) LastCheck() time.Time  { return time.Time{} }
func (m *hangingMockTransport) ReconnectCount() int   { return 0 }
func (m *hangingMockTransport) StderrTail() []string  { return nil }

func containsCh(s string, ch byte) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == ch {
			return true
		}
	}
	return false
}
