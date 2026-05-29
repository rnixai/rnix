package kernel

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/rnixai/rnix/vfs"
)

// =============================================================================
// Story 48.3 AC4 — mcp_manager 实化 (Init → LoadMCPConfig → SetMCPRegistry)
//
// Source: _bmad-output/implementation-artifacts/48-3-cli-mcp-list-test-mcp-manager.md
//
// **RED PHASE — all tests `t.Skip` until Story 48.3 Task 1 lands.**
//
// The 48.2-baseline `mcpManagerService` is a 28-line struct{} that only
// validates YAML can parse. Task 1 rewrites it to:
//   (a) call drivers/mcp.LoadMCPConfig (strong-typed MCPGlobalConfig parse)
//   (b) expose Servers() map[string]vfs.MCPConfig
//   (c) Bootstrap type-switches to *mcpManagerService and calls
//       k.SetMCPRegistry(svc.Servers())
//
// The 4 tests below cover the Story's §AC4 four scenarios:
//   _016 — valid mcp.yaml → Servers() returns N entries with correct shape
//   _017 — malformed YAML + required=true → Bootstrap returns error
//   _018 — mcp.yaml absent → Init returns nil, Servers() returns nil/empty
//   _019 — Bootstrap chains svc.Servers() → k.MCPRegistry() via SetMCPRegistry
//
// Compile-safety: this file references the new `Servers()` method and
// SetMCPRegistry / MCPRegistry getter. Story 48.3's contract tests
// (kernel/kernel_mcp_test.go) already wire those symbols, so this file
// reuses them — both files transition GREEN together once Task 6 (kernel
// surface) + Task 1 (mcp_manager rewrite) land.
//
// Skip → Green checklist:
//   - Task 1.1 (mcpManagerService rewrite + Servers()) → unblocks _016, _017, _018
//   - Task 1.4 (kernel SetMCPRegistry / MCPRegistry) → unblocks _019
//   - Task 1.5 (Bootstrap type-switch injection) → unblocks _019 fully
// =============================================================================

// writeMCPYaml writes a minimal global mcp.yaml at the given path.
func writeMCPYaml(t *testing.T, path string, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// -----------------------------------------------------------------------------
// _016: happy path — mcp.yaml with 2 servers loads into Servers() map
// -----------------------------------------------------------------------------
func TestATDD_48_3_016_McpManager_LoadsServers(t *testing.T) {

	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "mcp.yaml")
	writeMCPYaml(t, yamlPath, `
servers:
  playwright:
    command: npx
    args: ["@playwright/mcp"]
    transport_type: stdio
  deepwiki:
    command: deepwiki-mcp
    transport_type: stdio
`)

	// Task 1: mcpManagerService struct gains `servers map[string]vfs.MCPConfig`
	// field plus Init(cfg) that reads cfg["config_path"] and calls
	// drivers/mcp.LoadMCPConfig.
	svc := &mcpManagerService{}
	cfg := map[string]any{"config_path": yamlPath}
	if err := svc.Init(cfg); err != nil {
		t.Fatalf("Init: %v", err)
	}

	// New Servers() accessor (Task 1.2).
	servers := svc.Servers()
	if servers == nil {
		t.Fatal("Servers() returned nil after Init on valid yaml")
	}
	if len(servers) != 2 {
		t.Fatalf("Servers() len=%d, want 2", len(servers))
	}
	if got, want := servers["playwright"].Command, "npx"; got != want {
		t.Errorf("playwright.Command = %q, want %q", got, want)
	}
	if got := servers["playwright"]; got.TransportType != "stdio" || got.ServerName != "playwright" {
		t.Errorf("playwright shape = %+v, want ServerName=\"playwright\" TransportType=\"stdio\"", got)
	}
	if got := servers["deepwiki"].Command; got != "deepwiki-mcp" {
		t.Errorf("deepwiki.Command = %q, want %q", got, "deepwiki-mcp")
	}
}

// -----------------------------------------------------------------------------
// _017: malformed YAML + required=true Service → Bootstrap propagates error
// -----------------------------------------------------------------------------
func TestATDD_48_3_017_McpManager_InvalidYaml_RequiredFails(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "mcp.yaml")
	// Malformed: `command` value is a list where scalar expected; LoadMCPConfig
	// returns an error here.
	writeMCPYaml(t, yamlPath, `
servers:
  broken:
    command: [not, a, string]
`)

	svc := &mcpManagerService{}
	err := svc.Init(map[string]any{"config_path": yamlPath})
	if err == nil {
		t.Fatal("Init returned nil on malformed yaml; want error")
	}

	// With required=true in rnix-init.yaml, Bootstrap surfaces this as a
	// ServiceError with Recovery hint (init.go:127-134 既有逻辑). The
	// assertion here is structural: the service must NOT swallow the error.
	if svc.Servers() != nil {
		t.Errorf("Servers() = non-nil after Init error; want nil to flag bad state")
	}
}

// -----------------------------------------------------------------------------
// _018: mcp.yaml absent → Init returns nil, Servers() nil/empty (graceful)
// -----------------------------------------------------------------------------
func TestATDD_48_3_018_McpManager_NoYaml_GracefulEmpty(t *testing.T) {
	dir := t.TempDir()
	// Intentionally do NOT create mcp.yaml; just point at the path.
	yamlPath := filepath.Join(dir, "mcp.yaml")

	svc := &mcpManagerService{}
	if err := svc.Init(map[string]any{"config_path": yamlPath}); err != nil {
		t.Fatalf("Init on missing file returned error: %v (want nil — graceful skip)", err)
	}

	servers := svc.Servers()
	if len(servers) != 0 {
		t.Errorf("Servers() = %d entries on missing file; want 0/nil", len(servers))
	}
}

// -----------------------------------------------------------------------------
// _019: Bootstrap injects mcpManagerService.Servers() into kernel.MCPRegistry
// -----------------------------------------------------------------------------
func TestATDD_48_3_019_McpManager_InjectsToKernel(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "mcp.yaml")
	writeMCPYaml(t, yamlPath, `
servers:
  playwright:
    command: npx
    args: ["@playwright/mcp"]
    transport_type: stdio
`)

	cfg := &InitConfig{
		Services: []ServiceConfig{{
			Name:     "mcp-validator",
			Type:     "mcp_manager",
			Required: false,
			Config:   map[string]any{"config_path": yamlPath},
		}},
	}

	k := newKernelOnly(t)

	// Bootstrap runs the service loop (init.go:108-142) and AFTER each
	// service Init succeeds, type-switches *mcpManagerService and calls
	// k.SetMCPRegistry(svc.Servers()).
	if _, err := Bootstrap(k, cfg, nil); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}

	reg := k.MCPRegistry()
	if reg == nil {
		t.Fatal("kernel.MCPRegistry() = nil after Bootstrap; injection missed")
	}
	if _, ok := reg["playwright"]; !ok {
		t.Errorf("kernel.MCPRegistry()[playwright] missing; got keys: %v", mapKeys(reg))
	}
	if got := reg["playwright"].Command; got != "npx" {
		t.Errorf("kernel.MCPRegistry()[playwright].Command = %q, want \"npx\"", got)
	}

	// Verify wire-level shape matches vfs.MCPConfig (the registry value type
	// is what IPC handleMCPList / handleMCPTest will consume).
	_ = reg["playwright"]
}

// mapKeys returns the keys of m sorted-ish for stable error messages.
func mapKeys(m map[string]vfs.MCPConfig) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// -----------------------------------------------------------------------------
// Regression — implicit mcp_manager when init config declares no service.
//
// Root cause of the `rnix mcp test playwright` → "not found in mcp.yaml"
// inconsistency: a default install has no init.yaml, so DefaultInitConfig()
// returns an empty service list, mcp_manager is never instantiated, and
// kernel.MCPRegistry() stays nil — even though mcp.yaml correctly declares the
// server. `rnix check mcp` reads the file directly so it still reported the
// server, producing the contradictory diagnostics. The _019 test above only
// exercised the EXPLICITLY-declared path, so it stayed green.
// See investigations/mcp-registry-not-bootstrapped-investigation.md.
// -----------------------------------------------------------------------------
func TestRegression_McpManager_ImplicitWhenInitConfigEmpty(t *testing.T) {
	dir := t.TempDir()
	// GlobalDir() resolves to $XDG_CONFIG_HOME/rnix; place mcp.yaml there so
	// the implicit mcp_manager fallback (Init(nil) → defaultMCPConfigPath())
	// finds it without an explicit config_path.
	t.Setenv("XDG_CONFIG_HOME", dir)
	rnixDir := filepath.Join(dir, "rnix")
	if err := os.MkdirAll(rnixDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeMCPYaml(t, filepath.Join(rnixDir, "mcp.yaml"), `
servers:
  playwright:
    command: npx
    args: ["@playwright/mcp"]
    transport_type: stdio
`)

	k := newKernelOnly(t)

	// DefaultInitConfig() declares no services — the default-install path.
	if _, err := Bootstrap(k, DefaultInitConfig(), nil); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}

	reg := k.MCPRegistry()
	if reg == nil {
		t.Fatal("kernel.MCPRegistry() = nil after Bootstrap with empty init config; implicit mcp_manager fallback did not run")
	}
	if _, ok := reg["playwright"]; !ok {
		t.Errorf("kernel.MCPRegistry()[playwright] missing; got keys: %v", mapKeys(reg))
	}
}

// A user-declared mcp_manager must suppress the implicit fallback (no override
// of the explicitly-configured registry). Here the declared service points at
// a project-local mcp.yaml that has a DIFFERENT server than the global one;
// the registry must reflect the declared config, not the global fallback.
func TestRegression_McpManager_DeclaredTakesPrecedence(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	rnixDir := filepath.Join(dir, "rnix")
	if err := os.MkdirAll(rnixDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Global mcp.yaml (would be picked by the implicit fallback).
	writeMCPYaml(t, filepath.Join(rnixDir, "mcp.yaml"), `
servers:
  global-only:
    command: npx
    transport_type: stdio
`)
	// Explicit project config with a different server.
	declaredPath := filepath.Join(dir, "declared.yaml")
	writeMCPYaml(t, declaredPath, `
servers:
  declared-only:
    command: npx
    transport_type: stdio
`)

	cfg := &InitConfig{Services: []ServiceConfig{{
		Name:   "mcp-validator",
		Type:   "mcp_manager",
		Config: map[string]any{"config_path": declaredPath},
	}}}

	k := newKernelOnly(t)
	if _, err := Bootstrap(k, cfg, nil); err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}

	reg := k.MCPRegistry()
	if _, ok := reg["declared-only"]; !ok {
		t.Errorf("declared config not honored; got keys: %v", mapKeys(reg))
	}
	if _, ok := reg["global-only"]; ok {
		t.Error("implicit fallback overrode the declared mcp_manager config")
	}
}
