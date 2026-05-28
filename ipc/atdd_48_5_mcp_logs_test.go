//go:build atdd_48_5_red

package ipc

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/rnixai/rnix/kernel"
	"github.com/rnixai/rnix/vfs"
)

// =============================================================================
// ATDD 48.5 AC4 (IPC mcp_logs) — daemon → CLI stderr ring buffer projection
//
// Story: _bmad-output/implementation-artifacts/48-5-mcp-health-check-stderr-reconnect.md
// Phase: 🔴 RED — `//go:build atdd_48_5_red`. Run:
//            go test -tags=atdd_48_5_red -race ./ipc/
//        Pre-impl: compile fails on undefined MethodMCPLogs / MCPLogsResponse /
//        Client.MCPLogs — the red signal.
//
// Mirrors atdd_48_3_mcp_list_test.go's setupResumeIPCTest + MountManager wiring.
//
// Injection points (dev-story Task 7):
//   ipc.MethodMCPLogs Method = "mcp_logs"
//   ipc.MCPLogsRequest{Name string}
//   ipc.MCPLogsResponse{Lines []string}   // MarshalJSON: nil → []
//   (*Server).handleMCPLogs (NOT_FOUND + available server list hint)
//   (*Client).MCPLogs(name string) (*MCPLogsResponse, error)
//   kernel.MCPTransportByServer(name) (vfs.MCPTransport, bool)
// =============================================================================

// mcpLogsStubTransport implements the FULL post-48.5 vfs.MCPTransport surface
// (4 legacy + 7 new methods). Fields are configurable so the same stub serves
// both the mcp_logs (StderrTail) and mcp_list backfill (ToolCount/LastCheck)
// assertions.
type mcpLogsStubTransport struct {
	lines     []string
	tools     int
	resources int
	lastCheck time.Time
}

func (s *mcpLogsStubTransport) Connect(_ context.Context) error { return nil }
func (s *mcpLogsStubTransport) Call(_ context.Context, _ string, _ json.RawMessage) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}
func (s *mcpLogsStubTransport) Close() error                  { return nil }
func (s *mcpLogsStubTransport) Ping(_ context.Context) error { return nil }

// --- post-48.5 status/health surface (Story Task 5.3) ---
func (s *mcpLogsStubTransport) Status() vfs.MCPStatus { return vfs.MCPStatusConnected }
func (s *mcpLogsStubTransport) Alive() bool           { return true }
func (s *mcpLogsStubTransport) ToolCount() int        { return s.tools }
func (s *mcpLogsStubTransport) ResourceCount() int    { return s.resources }
func (s *mcpLogsStubTransport) LastCheck() time.Time  { return s.lastCheck }
func (s *mcpLogsStubTransport) ReconnectCount() int   { return 0 }
func (s *mcpLogsStubTransport) StderrTail() []string  { return s.lines }

func mountLogsStub(t *testing.T, kern *kernel.KernelImpl, path, server string, lines []string) {
	t.Helper()
	devReg := vfs.NewDeviceRegistry()
	factory := func(_ vfs.MCPConfig) (vfs.MCPTransport, error) {
		return &mcpLogsStubTransport{lines: lines}, nil
	}
	mgr := vfs.NewMountManager(devReg, factory)
	kern.SetMountManager(mgr)
	if err := kern.Mount(path, vfs.MCPConfig{ServerName: server, Command: "npx", TransportType: "stdio"}); err != nil {
		t.Fatalf("Mount %s: %v", server, err)
	}
}

// -----------------------------------------------------------------------------
// _050: mounted server with captured stderr → client.MCPLogs returns the lines.
// -----------------------------------------------------------------------------
func TestATDD_48_5_050_IPC_McpLogs_ReturnsLines(t *testing.T) {
	client, kern, _, _ := setupResumeIPCTest(t)
	mountLogsStub(t, kern, "/mnt/mcp/100-playwright", "playwright",
		[]string{"npx error: spawn failed", "retrying connection"})

	resp, err := client.MCPLogs("playwright")
	if err != nil {
		t.Fatalf("MCPLogs: %v", err)
	}
	if resp == nil {
		t.Fatal("response nil")
	}
	if len(resp.Lines) != 2 {
		t.Fatalf("Lines len = %d, want 2: %v", len(resp.Lines), resp.Lines)
	}
	joined := strings.Join(resp.Lines, "\n")
	if !strings.Contains(joined, "npx error") || !strings.Contains(joined, "retrying") {
		t.Errorf("Lines missing expected stderr content: %v", resp.Lines)
	}
}

// -----------------------------------------------------------------------------
// _051: unknown server → NOT_FOUND error + available server list in message.
// -----------------------------------------------------------------------------
func TestATDD_48_5_051_IPC_McpLogs_UnknownServer_NotFound(t *testing.T) {
	client, kern, _, _ := setupResumeIPCTest(t)
	mountLogsStub(t, kern, "/mnt/mcp/100-playwright", "playwright", []string{"x"})

	_, err := client.MCPLogs("does-not-exist")
	if err == nil {
		t.Fatal("MCPLogs(unknown) succeeded; want NOT_FOUND error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "NOT_FOUND") {
		t.Errorf("error = %q; want NOT_FOUND code", msg)
	}
	// Story Task 7.3: surface the available server list as a hint.
	if !strings.Contains(msg, "playwright") {
		t.Errorf("error = %q; want available-server hint containing 'playwright'", msg)
	}
}

// -----------------------------------------------------------------------------
// _052: empty stderr buffer → Lines is [] (not null) on the wire. (nil-normalize)
// -----------------------------------------------------------------------------
func TestATDD_48_5_052_IPC_McpLogs_NilNormalize(t *testing.T) {
	client, kern, _, _ := setupResumeIPCTest(t)
	mountLogsStub(t, kern, "/mnt/mcp/100-quiet", "quiet", nil)

	resp, err := client.MCPLogs("quiet")
	if err != nil {
		t.Fatalf("MCPLogs: %v", err)
	}
	if resp.Lines == nil {
		t.Error("Lines must be a non-nil empty slice ([]) on the wire, not nil")
	}
	if len(resp.Lines) != 0 {
		t.Errorf("Lines len = %d, want 0 for an empty buffer", len(resp.Lines))
	}
}

// -----------------------------------------------------------------------------
// _053: AC5 — handleMCPList backfills Tools / Resources / LastCheckMs from the
//        transport's live counters (the 48.3 placeholders are now filled).
// -----------------------------------------------------------------------------
func TestATDD_48_5_053_IPC_McpList_BackfillFromTransport(t *testing.T) {
	client, kern, _, _ := setupResumeIPCTest(t)

	devReg := vfs.NewDeviceRegistry()
	stub := &mcpLogsStubTransport{tools: 4, resources: 2, lastCheck: time.Unix(1_700_000_000, 0)}
	factory := func(_ vfs.MCPConfig) (vfs.MCPTransport, error) { return stub, nil }
	mgr := vfs.NewMountManager(devReg, factory)
	kern.SetMountManager(mgr)
	if err := kern.Mount("/mnt/mcp/100-playwright", vfs.MCPConfig{ServerName: "playwright", Command: "npx", TransportType: "stdio"}); err != nil {
		t.Fatalf("Mount: %v", err)
	}

	resp, err := client.MCPList()
	if err != nil {
		t.Fatalf("MCPList: %v", err)
	}
	if len(resp.Mounts) != 1 {
		t.Fatalf("Mounts len = %d, want 1", len(resp.Mounts))
	}
	m := resp.Mounts[0]
	if m.Tools != 4 {
		t.Errorf("wire.Tools = %d, want 4 (backfilled from transport.ToolCount)", m.Tools)
	}
	if m.Resources != 2 {
		t.Errorf("wire.Resources = %d, want 2 (backfilled from transport.ResourceCount)", m.Resources)
	}
	if m.LastCheckMs == 0 {
		t.Errorf("wire.LastCheckMs = 0, want non-zero (backfilled from transport.LastCheck)")
	}
}
