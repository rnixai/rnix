package main

import (
	"testing"
)

// =============================================================================
// Story 48.3 AC3 — `rnix check mcp` 宿主环境预检查
//
// **RED PHASE — all tests t.Skip until Story 48.3 Task 5 lands.**
//
// Task 5 creates `cmd/rnix/check.go`:
//   checkCmd      — top-level "check" group (separate from `rnix doctor`)
//   checkMcpCmd   — "mcp" subcommand, RunE = runCheckMcp
//
// Probes (in order):
//   1. node      — exec.LookPath("node") + `node --version`
//   2. npx       — exec.LookPath("npx") + `npx --version`
//   3. chromium  — IFF mcp.yaml contains a server named "playwright" or
//                  whose command contains "playwright", scan platform paths
//
// Critical test-injection seam:
//   var lookPath = exec.LookPath       // pkg-level var so tests can override
//   var runCommand = func(name, args ...string) (string, error) { ... }
//
// Output format reuses doctor.go's three-level renderer (INFO/WARN/ERROR)
// with same JSON shape (`status, checks[]`).
//
// AC3 five scenarios:
//   _011 — all pass: node/npx/chromium present
//   _012 — npx missing → ERROR + exit 1
//   _013 — playwright in mcp.yaml + no chromium → WARN + exit 0
//   _014 — mcp.yaml missing → still runs node/npx checks
//   _015 — --json mode shape
//
// Compile-safety: tests reference `rootCmd.Find` only; no direct symbol
// reference to checkMcpCmd / runCheckMcp / lookPath until GREEN phase.
// =============================================================================

// -----------------------------------------------------------------------------
// _011: all pass — node, npx, chromium present → 3 INFO + exit 0
// -----------------------------------------------------------------------------
func TestATDD_48_3_011_CheckMcp_AllPass(t *testing.T) {
	t.Skip("RED: 等待 Story 48.3 Task 5 — cmd/rnix/check.go + checkMcpCmd")

	// GREEN-phase plan:
	//
	//   // Monkey-patch lookPath to fake "all present" state.
	//   oldLookPath := lookPath
	//   lookPath = func(name string) (string, error) {
	//       switch name {
	//       case "node": return "/usr/bin/node", nil
	//       case "npx":  return "/usr/bin/npx", nil
	//       }
	//       return "", exec.ErrNotFound
	//   }
	//   defer func() { lookPath = oldLookPath }()
	//
	//   oldRun := runCommand
	//   runCommand = func(name string, args ...string) (string, error) {
	//       switch name {
	//       case "node": return "v22.0.0", nil
	//       case "npx":  return "10.5.0", nil
	//       }
	//       return "", nil
	//   }
	//   defer func() { runCommand = oldRun }()
	//
	//   // Stage a mcp.yaml referencing playwright so Chromium probe runs.
	//   // Patch chromium detector to "found":
	//   oldChromeDetect := detectChromium
	//   detectChromium = func() (string, error) { return "/usr/bin/chromium", nil }
	//   defer func() { detectChromium = oldChromeDetect }()
	//
	//   exitCode = 0
	//   var buf bytes.Buffer
	//   cmd, _, err := rootCmd.Find([]string{"check", "mcp"})
	//   if err != nil { t.Fatalf("check mcp not registered: %v", err) }
	//   cmd.SetOut(&buf); cmd.SetErr(&buf)
	//   cmd.RunE(cmd, nil)
	//
	//   if exitCode != 0 { t.Errorf("exitCode=%d, want 0", exitCode) }
	//   out := buf.String()
	//   for _, want := range []string{"node", "npx", "chromium"} {
	//       if !strings.Contains(out, want) {
	//           t.Errorf("missing %q in output: %s", want, out)
	//       }
	//   }
	//   if !strings.Contains(out, "All checks passed") {
	//       t.Errorf("missing summary line; got: %s", out)
	//   }
}

// -----------------------------------------------------------------------------
// _012: npx missing → ERROR + exit 1
// -----------------------------------------------------------------------------
func TestATDD_48_3_012_CheckMcp_NoNpx_Fail(t *testing.T) {
	t.Skip("RED: 等待 Story 48.3 Task 5.2 — lookPath ERROR mapping")

	// GREEN-phase plan:
	//
	//   lookPath = func(name string) (string, error) {
	//       if name == "node" { return "/usr/bin/node", nil }
	//       return "", exec.ErrNotFound  // npx + others missing
	//   }
	//
	//   exitCode = 0
	//   ...
	//   cmd.RunE(cmd, nil)
	//
	//   if exitCode != 1 { t.Errorf("exitCode=%d, want 1 (ERROR found)", exitCode) }
	//   out := buf.String()
	//   if !strings.Contains(out, "ERROR") || !strings.Contains(out, "npx") {
	//       t.Errorf("expect ERROR + npx hint; got: %s", out)
	//   }
	//   // Three-segment error: what / why / how
	//   if !strings.Contains(out, "not found in $PATH") {
	//       t.Errorf("missing 'why' (not found in PATH); got: %s", out)
	//   }
	//   if !strings.Contains(out, "npm") || !strings.Contains(out, "install") {
	//       t.Errorf("missing 'how' (install hint); got: %s", out)
	//   }
}

// -----------------------------------------------------------------------------
// _013: mcp.yaml has playwright + no Chromium → WARN, exit 0
// -----------------------------------------------------------------------------
func TestATDD_48_3_013_CheckMcp_PlaywrightInYaml_NoChrome_Warn(t *testing.T) {
	t.Skip("RED: 等待 Story 48.3 Task 5.4 — mcp.yaml playwright scan + Chromium probe")

	// GREEN-phase plan:
	//
	//   // 1. Write a temp mcp.yaml referencing a playwright command.
	//   dir := t.TempDir()
	//   yamlPath := filepath.Join(dir, "mcp.yaml")
	//   os.WriteFile(yamlPath, []byte(`servers:
	// playwright:
	//   command: npx
	//   args: ["@playwright/mcp"]
	// `), 0o600)
	//
	//   // 2. Point check.go at this path (via env var or pkg var override).
	//   oldYamlPath := mcpConfigPathForCheck
	//   mcpConfigPathForCheck = yamlPath
	//   defer func() { mcpConfigPathForCheck = oldYamlPath }()
	//
	//   // 3. lookPath: node + npx OK; chromium NOT found.
	//   lookPath = func(name string) (string, error) {
	//       if name == "node" || name == "npx" { return "/usr/bin/" + name, nil }
	//       return "", exec.ErrNotFound
	//   }
	//   detectChromium = func() (string, error) { return "", fmt.Errorf("chromium not found") }
	//
	//   exitCode = 0
	//   ...
	//   cmd.RunE(cmd, nil)
	//
	//   if exitCode != 0 {
	//       t.Errorf("exitCode=%d, want 0 (WARN does not fail; Story §AC3 \"WARN 不阻塞\")", exitCode)
	//   }
	//   out := buf.String()
	//   if !strings.Contains(out, "WARN") || !strings.Contains(out, "chromium") {
	//       t.Errorf("expect WARN for chromium; got: %s", out)
	//   }
	//   if !strings.Contains(out, "playwright install chromium") {
	//       t.Errorf("missing install hint; got: %s", out)
	//   }
}

// -----------------------------------------------------------------------------
// _014: mcp.yaml absent → still runs node/npx checks; chromium skipped
// -----------------------------------------------------------------------------
func TestATDD_48_3_014_CheckMcp_NoYaml_StillRunsNodeNpxChecks(t *testing.T) {
	t.Skip("RED: 等待 Story 48.3 Task 5.4 — mcp.yaml not-exist branch")

	// GREEN-phase plan:
	//
	//   // mcp.yaml not present (point at nonexistent path).
	//   mcpConfigPathForCheck = "/nonexistent/mcp.yaml"
	//
	//   lookPath = func(name string) (string, error) {
	//       return "/usr/bin/" + name, nil
	//   }
	//
	//   exitCode = 0
	//   ...
	//   cmd.RunE(cmd, nil)
	//
	//   if exitCode != 0 { t.Errorf("exitCode=%d, want 0", exitCode) }
	//   out := buf.String()
	//   if !strings.Contains(out, "node") || !strings.Contains(out, "npx") {
	//       t.Errorf("node/npx checks should still run; got: %s", out)
	//   }
	//   if !strings.Contains(out, "mcp.yaml not found") {
	//       t.Errorf("expect 'mcp.yaml not found' INFO hint; got: %s", out)
	//   }
	//   if strings.Contains(out, "chromium") {
	//       t.Errorf("chromium check should be skipped when no playwright in mcp.yaml; got: %s", out)
	//   }
}

// -----------------------------------------------------------------------------
// _015: --json mode shape (status + checks[] like doctor.go)
// -----------------------------------------------------------------------------
func TestATDD_48_3_015_CheckMcp_JSONMode(t *testing.T) {
	t.Skip("RED: 等待 Story 48.3 Task 5.5 — JSON mode same shape as doctor")

	// GREEN-phase plan:
	//
	//   oldJSON := flagJSON; flagJSON = true; defer func() { flagJSON = oldJSON }()
	//
	//   lookPath = func(name string) (string, error) {
	//       return "/usr/bin/" + name, nil
	//   }
	//
	//   var buf bytes.Buffer
	//   cmd, _, _ := rootCmd.Find([]string{"check", "mcp"})
	//   cmd.SetOut(&buf); cmd.SetErr(&buf)
	//   cmd.RunE(cmd, nil)
	//
	//   var resp JSONResponse
	//   if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
	//       t.Fatalf("JSON parse: %v\n%s", err, buf.String())
	//   }
	//   data, ok := resp.Data.(map[string]any)
	//   if !ok { t.Fatalf("Data shape: %T", resp.Data) }
	//   if _, ok := data["status"]; !ok { t.Error("missing status field") }
	//   if _, ok := data["checks"]; !ok { t.Error("missing checks array") }
	//   // checks must be a list, even if empty (no null)
	//   checks, ok := data["checks"].([]any)
	//   if !ok || checks == nil {
	//       t.Errorf("checks must be array, got %T", data["checks"])
	//   }
}
