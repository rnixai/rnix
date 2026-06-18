package ipc

import (
	"os"
	"path/filepath"
	"testing"
)

// =============================================================================
// ATDD 59.2 (IPC mcp_reload) — daemon re-parses mcp.yaml and swaps the registry
//
// Spec: _bmad-output/implementation-artifacts/spec-mcp-http-version-negotiation-reload.md
//
// End-to-end round trip over the test socket (mirrors atdd_48_5_mcp_logs_test.go):
// exercises MethodMCPReload dispatch + MCPReloadResponse wire marshalling +
// Client.MCPReload, on top of kernel.ReloadMCPRegistry. The on-disk mcp.yaml is
// controlled via XDG_CONFIG_HOME (config.GlobalDir → defaultMCPConfigPath).
//
// Run: go test -race -run TestATDD_59_2_IPC ./ipc/
// =============================================================================

// setGlobalMCPConfig points config.GlobalDir() at a fresh temp dir and, when
// content != "", writes mcp.yaml there. Returns nothing — reload reads the path
// itself. content == "" leaves the dir without an mcp.yaml (IsNotExist path).
func setGlobalMCPConfig(t *testing.T, content string) {
	t.Helper()
	dir := t.TempDir()
	rnixDir := filepath.Join(dir, "rnix")
	if err := os.MkdirAll(rnixDir, 0o755); err != nil {
		t.Fatalf("mkdir rnix config dir: %v", err)
	}
	if content != "" {
		if err := os.WriteFile(filepath.Join(rnixDir, "mcp.yaml"), []byte(content), 0o644); err != nil {
			t.Fatalf("write mcp.yaml: %v", err)
		}
	}
	t.Setenv("XDG_CONFIG_HOME", dir)
}

// _IPC_010 — happy path: a 2-server mcp.yaml reloads to ServerCount=2 with a
// sorted name list, over the real IPC round trip.
func TestATDD_59_2_IPC_McpReload_RefreshesAndReturnsSortedServers(t *testing.T) {
	setGlobalMCPConfig(t, "servers:\n  beta:\n    command: echo\n  alpha:\n    command: echo\n")

	client, _, _, _ := setupResumeIPCTest(t)

	resp, err := client.MCPReload()
	if err != nil {
		t.Fatalf("MCPReload: %v", err)
	}
	if resp == nil {
		t.Fatal("response nil")
	}
	if resp.ServerCount != 2 {
		t.Errorf("ServerCount = %d, want 2", resp.ServerCount)
	}
	if len(resp.Servers) != 2 || resp.Servers[0] != "alpha" || resp.Servers[1] != "beta" {
		t.Errorf("Servers = %v, want sorted [alpha beta]", resp.Servers)
	}
}

// _IPC_011 — no mcp.yaml on disk is not an error: ServerCount=0 and Servers is a
// non-nil empty slice on the wire (nil-normalize).
func TestATDD_59_2_IPC_McpReload_NoConfig_ZeroServers(t *testing.T) {
	setGlobalMCPConfig(t, "") // dir exists, mcp.yaml absent

	client, _, _, _ := setupResumeIPCTest(t)

	resp, err := client.MCPReload()
	if err != nil {
		t.Fatalf("MCPReload (no config) should not error: %v", err)
	}
	if resp.ServerCount != 0 {
		t.Errorf("ServerCount = %d, want 0 when mcp.yaml absent", resp.ServerCount)
	}
	if resp.Servers == nil {
		t.Error("Servers must be a non-nil empty slice ([]) on the wire, not nil")
	}
}
