package ipc

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/rnixai/rnix/vfs"
)

// =============================================================================
// Story 48.3 AC5 (IPC mcp_list wire) — daemon → CLI registry projection
//
// GREEN tests for:
//   - ipc.MethodMCPList + MCPListResponse + MCPMountWire (protocol.go)
//   - Server.handleMCPList (server_mcp.go)
//   - Client.MCPList (client.go)
//
// setupResumeIPCTest builds the Server / Client over a unix socket. We wire
// an independent MountManager + DeviceRegistry onto the kernel so Mount calls
// succeed without spawning real subprocesses; mcp_list only reads the mount
// table so a parallel devReg is fine here.
// =============================================================================

// -----------------------------------------------------------------------------
// _020: empty mount registry → wires=[] (NOT null) — AC5 nil-normalize
// -----------------------------------------------------------------------------
func TestATDD_48_3_020_IPC_McpList_Empty(t *testing.T) {
	client, _, _, _ := setupResumeIPCTest(t)

	resp, err := client.MCPList()
	if err != nil {
		t.Fatalf("MCPList: %v", err)
	}
	if resp == nil {
		t.Fatal("response nil")
	}
	if resp.Mounts == nil {
		t.Error("Mounts must be non-nil slice ([]), not nil (wire nil-normalize)")
	}
	if len(resp.Mounts) != 0 {
		t.Errorf("Mounts len=%d, want 0 on empty kernel", len(resp.Mounts))
	}
}

// -----------------------------------------------------------------------------
// _020b: two mounts → wires reflect Path / Name / Status round-trip
// -----------------------------------------------------------------------------
func TestATDD_48_3_020b_IPC_McpList_TwoMounts(t *testing.T) {
	client, kern, _, _ := setupResumeIPCTest(t)

	devReg := vfs.NewDeviceRegistry()
	factory := func(_ vfs.MCPConfig) (vfs.MCPTransport, error) {
		return &mcpListStubTransport{}, nil
	}
	mgr := vfs.NewMountManager(devReg, factory)
	kern.SetMountManager(mgr)

	cfgA := vfs.MCPConfig{ServerName: "playwright", Command: "npx", TransportType: "stdio"}
	cfgB := vfs.MCPConfig{ServerName: "deepwiki", Command: "deepwiki-mcp", TransportType: ""}
	pathA := "/mnt/mcp/100-playwright"
	pathB := "/mnt/mcp/101-deepwiki"
	if err := kern.Mount(pathA, cfgA); err != nil {
		t.Fatalf("Mount A: %v", err)
	}
	if err := kern.Mount(pathB, cfgB); err != nil {
		t.Fatalf("Mount B: %v", err)
	}

	resp, err := client.MCPList()
	if err != nil {
		t.Fatalf("MCPList: %v", err)
	}
	if len(resp.Mounts) != 2 {
		t.Fatalf("Mounts len=%d, want 2; got=%+v", len(resp.Mounts), resp.Mounts)
	}

	got := map[string]MCPMountWire{}
	for _, m := range resp.Mounts {
		got[m.Name] = m
	}
	playwright, ok := got["playwright"]
	if !ok {
		t.Fatalf("missing playwright in wire mounts: %+v", got)
	}
	if playwright.Path != pathA {
		t.Errorf("playwright.Path = %q, want %q", playwright.Path, pathA)
	}
	if playwright.Status != "connected" {
		t.Errorf("playwright.Status = %q, want \"connected\"", playwright.Status)
	}
	if playwright.Transport != "stdio" {
		t.Errorf("playwright.Transport = %q, want \"stdio\"", playwright.Transport)
	}
	if playwright.Tools != 0 || playwright.Resources != 0 {
		t.Errorf("playwright tools/resources non-zero pre-48.5: %+v", playwright)
	}

	deepwiki, ok := got["deepwiki"]
	if !ok {
		t.Fatalf("missing deepwiki in wire mounts: %+v", got)
	}
	if deepwiki.Transport != "stdio" {
		t.Errorf("deepwiki.Transport = %q, want \"stdio\" (empty defaults)", deepwiki.Transport)
	}
}

// mcpListStubTransport stands in for a real MCP server during AC1 mount tests.
// Connect always succeeds; Call returns empty payloads (mcp_list does not RPC,
// but Mount->transport.Connect calls Connect, hence we satisfy the interface).
type mcpListStubTransport struct{}

func (s *mcpListStubTransport) Connect(_ context.Context) error { return nil }
func (s *mcpListStubTransport) Call(_ context.Context, _ string, _ json.RawMessage) (json.RawMessage, error) {
	return json.RawMessage(`{}`), nil
}
func (s *mcpListStubTransport) Close() error                  { return nil }
func (s *mcpListStubTransport) Ping(_ context.Context) error { return nil }
