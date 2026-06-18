package kernel

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// =============================================================================
// ATDD 59.2 — kernel MCP registry hot-reload (ReloadMCPRegistry)
//
// Spec: _bmad-output/implementation-artifacts/spec-mcp-http-version-negotiation-reload.md
//
// Covers the reload I/O matrix + the concurrency guard added with the runtime
// swap: SetMCPRegistry/MCPRegistry were lock-free (set-once at Bootstrap) until
// `mcp reload` made the field swappable while IPC handler goroutines read it.
//
// Run: go test -race -run TestKernel_ReloadMCPRegistry ./kernel/
// =============================================================================

// setupGlobalMCPDir points config.GlobalDir() at a fresh temp dir (via
// XDG_CONFIG_HOME) and returns the path where mcp.yaml should be written. The
// file is NOT created — tests write/rewrite/remove it to drive reload.
func setupGlobalMCPDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	rnixDir := filepath.Join(dir, "rnix")
	if err := os.MkdirAll(rnixDir, 0o755); err != nil {
		t.Fatalf("mkdir rnix config dir: %v", err)
	}
	t.Setenv("XDG_CONFIG_HOME", dir)
	return filepath.Join(rnixDir, "mcp.yaml")
}

func writeMCPYAML(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write mcp.yaml: %v", err)
	}
}

// TestKernel_ReloadMCPRegistry_RefreshesRegistry — a mcp.yaml edit (add a
// server) is reflected by ReloadMCPRegistry without a restart; the returned
// names are sorted (AC: reload happy path).
func TestKernel_ReloadMCPRegistry_RefreshesRegistry(t *testing.T) {
	mcpPath := setupGlobalMCPDir(t)
	k := newKernelOnly(t)

	writeMCPYAML(t, mcpPath, "servers:\n  alpha:\n    command: echo\n")
	count, names, err := k.ReloadMCPRegistry()
	if err != nil {
		t.Fatalf("reload (1 server): %v", err)
	}
	if count != 1 || len(names) != 1 || names[0] != "alpha" {
		t.Fatalf("reload (1 server) = count %d names %v, want 1 [alpha]", count, names)
	}
	if _, ok := k.MCPRegistry()["alpha"]; !ok {
		t.Error("registry missing alpha after reload")
	}

	// Edit mcp.yaml: add beta. Reload must see it; names sorted (alpha, beta).
	writeMCPYAML(t, mcpPath, "servers:\n  beta:\n    command: echo\n  alpha:\n    command: echo\n")
	count, names, err = k.ReloadMCPRegistry()
	if err != nil {
		t.Fatalf("reload (2 servers): %v", err)
	}
	if count != 2 {
		t.Fatalf("reload (2 servers) count = %d, want 2", count)
	}
	if len(names) != 2 || names[0] != "alpha" || names[1] != "beta" {
		t.Errorf("reload names = %v, want sorted [alpha beta]", names)
	}
}

// TestKernel_ReloadMCPRegistry_MissingFile_ClearsRegistry — a reload when
// mcp.yaml is absent is NOT an error; it clears the registry to zero servers
// (IsNotExist semantics, same shape as a daemon started without the file).
func TestKernel_ReloadMCPRegistry_MissingFile_ClearsRegistry(t *testing.T) {
	mcpPath := setupGlobalMCPDir(t)
	k := newKernelOnly(t)

	writeMCPYAML(t, mcpPath, "servers:\n  alpha:\n    command: echo\n")
	if _, _, err := k.ReloadMCPRegistry(); err != nil {
		t.Fatalf("seed reload: %v", err)
	}

	if err := os.Remove(mcpPath); err != nil {
		t.Fatalf("remove mcp.yaml: %v", err)
	}
	count, names, err := k.ReloadMCPRegistry()
	if err != nil {
		t.Fatalf("reload after remove should be nil error (IsNotExist), got %v", err)
	}
	if count != 0 || len(names) != 0 {
		t.Errorf("after remove reload = count %d names %v, want 0 / empty", count, names)
	}
	if got := len(k.MCPRegistry()); got != 0 {
		t.Errorf("registry not cleared after mcp.yaml removed: %d entries", got)
	}
}

// TestKernel_ReloadMCPRegistry_BadConfig_PreservesOldRegistry — a reload of a
// broken mcp.yaml returns an error and leaves the previous good registry intact
// (reload must never wipe a working config on a bad edit).
func TestKernel_ReloadMCPRegistry_BadConfig_PreservesOldRegistry(t *testing.T) {
	mcpPath := setupGlobalMCPDir(t)
	k := newKernelOnly(t)

	// Good registry first.
	writeMCPYAML(t, mcpPath, "servers:\n  alpha:\n    command: echo\n")
	if _, _, err := k.ReloadMCPRegistry(); err != nil {
		t.Fatalf("seed reload: %v", err)
	}

	// Broken mcp.yaml: unterminated flow sequence → YAML parse error.
	writeMCPYAML(t, mcpPath, "servers: [unclosed\n")
	count, names, err := k.ReloadMCPRegistry()
	if err == nil {
		t.Fatal("reload of broken mcp.yaml should return an error")
	}
	if count != 0 || names != nil {
		t.Errorf("on error reload returned count=%d names=%v, want 0 / nil", count, names)
	}
	// The previous good registry must survive the failed reload.
	if _, ok := k.MCPRegistry()["alpha"]; !ok {
		t.Error("old registry destroyed by a failed reload — it must be preserved")
	}
}

// TestKernel_ReloadMCPRegistry_ConcurrentReadWrite_NoRace — concurrent reloads
// (field writes via SetMCPRegistry) racing concurrent MCPRegistry reads (mirrors
// the mcp_list / mcp_test IPC handler goroutines). Fails the -race detector if
// the mcpRegistryMu guard is missing.
func TestKernel_ReloadMCPRegistry_ConcurrentReadWrite_NoRace(t *testing.T) {
	mcpPath := setupGlobalMCPDir(t)
	k := newKernelOnly(t)
	writeMCPYAML(t, mcpPath, "servers:\n  alpha:\n    command: echo\n")
	if _, _, err := k.ReloadMCPRegistry(); err != nil {
		t.Fatalf("seed reload: %v", err)
	}

	var wg sync.WaitGroup
	// Writers: repeatedly reload (each swaps the registry field).
	for range 4 {
		wg.Go(func() {
			for range 50 {
				_, _, _ = k.ReloadMCPRegistry()
			}
		})
	}
	// Readers: repeatedly read + iterate the registry.
	for range 4 {
		wg.Go(func() {
			for range 50 {
				for name := range k.MCPRegistry() {
					_ = name
				}
			}
		})
	}
	wg.Wait()

	if _, ok := k.MCPRegistry()["alpha"]; !ok {
		t.Error("registry lost alpha after concurrent reloads (every reload reads the same file)")
	}
}
