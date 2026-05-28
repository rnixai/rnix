package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// =============================================================================
// Story 48.3 AC3 — `rnix check mcp` 宿主环境预检查 (GREEN tests)
//
// All checks use the test-injection seams declared in check.go:
//   - lookPath   (default exec.LookPath)
//   - runCommand (default 10s-timeout exec wrapper)
//   - detectChromium
//   - mcpConfigPathForCheck
// =============================================================================

func swapLookPath(replacement func(string) (string, error)) func() {
	old := lookPath
	lookPath = replacement
	return func() { lookPath = old }
}

func swapRunCommand(replacement func(string, ...string) (string, error)) func() {
	old := runCommand
	runCommand = replacement
	return func() { runCommand = old }
}

func swapDetectChromium(replacement func() (string, error)) func() {
	old := detectChromium
	detectChromium = replacement
	return func() { detectChromium = old }
}

func swapMCPConfigPath(path string) func() {
	old := mcpConfigPathForCheck
	mcpConfigPathForCheck = path
	return func() { mcpConfigPathForCheck = old }
}

// -----------------------------------------------------------------------------
// _011: all pass — node, npx, chromium present → 3+ INFO + exit 0
// -----------------------------------------------------------------------------
func TestATDD_48_3_011_CheckMcp_AllPass(t *testing.T) {
	defer swapLookPath(func(name string) (string, error) {
		switch name {
		case "node":
			return "/usr/bin/node", nil
		case "npx":
			return "/usr/bin/npx", nil
		}
		return "", exec.ErrNotFound
	})()
	defer swapRunCommand(func(name string, _ ...string) (string, error) {
		switch name {
		case "node":
			return "v22.0.0", nil
		case "npx":
			return "10.5.0", nil
		}
		return "", nil
	})()
	defer swapDetectChromium(func() (string, error) { return "/usr/bin/chromium", nil })()

	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "mcp.yaml")
	mustWriteYaml(t, yamlPath, `servers:
  playwright:
    command: npx
    args: ["@playwright/mcp"]
`)
	defer swapMCPConfigPath(yamlPath)()
	defer resetExitCode(t)()

	var buf bytes.Buffer
	cmd, _, err := rootCmd.Find([]string{"check", "mcp"})
	if err != nil {
		t.Fatalf("check mcp not registered: %v", err)
	}
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}

	if exitCode != 0 {
		t.Errorf("exitCode=%d, want 0", exitCode)
	}
	out := buf.String()
	for _, want := range []string{"node", "npx", "chromium", "All checks passed"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in output: %s", want, out)
		}
	}
}

// -----------------------------------------------------------------------------
// _012: npx missing → ERROR + exit 1
// -----------------------------------------------------------------------------
func TestATDD_48_3_012_CheckMcp_NoNpx_Fail(t *testing.T) {
	defer swapLookPath(func(name string) (string, error) {
		if name == "node" {
			return "/usr/bin/node", nil
		}
		return "", exec.ErrNotFound
	})()
	defer swapRunCommand(func(_ string, _ ...string) (string, error) { return "v22", nil })()
	defer swapMCPConfigPath("/nonexistent/mcp.yaml")()
	defer resetExitCode(t)()

	var buf bytes.Buffer
	cmd, _, _ := rootCmd.Find([]string{"check", "mcp"})
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}

	if exitCode != 1 {
		t.Errorf("exitCode=%d, want 1 (ERROR found)", exitCode)
	}
	out := buf.String()
	if !strings.Contains(out, "ERROR") || !strings.Contains(out, "npx") {
		t.Errorf("expect ERROR + npx hint; got: %s", out)
	}
	if !strings.Contains(out, "not found in $PATH") {
		t.Errorf("missing 'why' (not found in PATH); got: %s", out)
	}
	if !strings.Contains(out, "npm") {
		t.Errorf("missing 'how' (install hint); got: %s", out)
	}
}

// -----------------------------------------------------------------------------
// _013: mcp.yaml has playwright + no Chromium → WARN, exit 0
// -----------------------------------------------------------------------------
func TestATDD_48_3_013_CheckMcp_PlaywrightInYaml_NoChrome_Warn(t *testing.T) {
	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "mcp.yaml")
	mustWriteYaml(t, yamlPath, `servers:
  playwright:
    command: npx
    args: ["@playwright/mcp"]
`)
	defer swapMCPConfigPath(yamlPath)()

	defer swapLookPath(func(name string) (string, error) {
		switch name {
		case "node", "npx":
			return "/usr/bin/" + name, nil
		}
		return "", exec.ErrNotFound
	})()
	defer swapRunCommand(func(_ string, _ ...string) (string, error) { return "v22", nil })()
	defer swapDetectChromium(func() (string, error) { return "", errors.New("chromium not found") })()
	defer resetExitCode(t)()

	var buf bytes.Buffer
	cmd, _, _ := rootCmd.Find([]string{"check", "mcp"})
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}

	if exitCode != 0 {
		t.Errorf("exitCode=%d, want 0 (WARN does not fail)", exitCode)
	}
	out := buf.String()
	if !strings.Contains(out, "WARN") || !strings.Contains(out, "chromium") {
		t.Errorf("expect WARN for chromium; got: %s", out)
	}
	if !strings.Contains(out, "playwright install chromium") {
		t.Errorf("missing install hint; got: %s", out)
	}
}

// -----------------------------------------------------------------------------
// _014: mcp.yaml absent → still runs node/npx; chromium skipped
// -----------------------------------------------------------------------------
func TestATDD_48_3_014_CheckMcp_NoYaml_StillRunsNodeNpxChecks(t *testing.T) {
	defer swapMCPConfigPath("/nonexistent/mcp.yaml")()
	defer swapLookPath(func(name string) (string, error) { return "/usr/bin/" + name, nil })()
	defer swapRunCommand(func(_ string, _ ...string) (string, error) { return "v22", nil })()
	defer resetExitCode(t)()

	var buf bytes.Buffer
	cmd, _, _ := rootCmd.Find([]string{"check", "mcp"})
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}

	if exitCode != 0 {
		t.Errorf("exitCode=%d, want 0", exitCode)
	}
	out := buf.String()
	if !strings.Contains(out, "node") || !strings.Contains(out, "npx") {
		t.Errorf("node/npx checks should still run; got: %s", out)
	}
	if !strings.Contains(out, "mcp.yaml not found") {
		t.Errorf("expect 'mcp.yaml not found' INFO hint; got: %s", out)
	}
	if strings.Contains(out, "chromium") {
		t.Errorf("chromium check should be skipped when no playwright in mcp.yaml; got: %s", out)
	}
}

// -----------------------------------------------------------------------------
// _015: --json mode shape (status + checks[] like doctor.go)
// -----------------------------------------------------------------------------
func TestATDD_48_3_015_CheckMcp_JSONMode(t *testing.T) {
	defer swapLookPath(func(name string) (string, error) { return "/usr/bin/" + name, nil })()
	defer swapRunCommand(func(_ string, _ ...string) (string, error) { return "v22", nil })()
	defer swapMCPConfigPath("/nonexistent/mcp.yaml")()
	defer resetExitCode(t)()

	oldJSON := flagJSON
	flagJSON = true
	defer func() { flagJSON = oldJSON }()

	var buf bytes.Buffer
	cmd, _, _ := rootCmd.Find([]string{"check", "mcp"})
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	if err := cmd.RunE(cmd, nil); err != nil {
		t.Fatalf("RunE: %v", err)
	}

	var resp JSONResponse
	if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
		t.Fatalf("JSON parse: %v\n%s", err, buf.String())
	}
	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("Data shape: %T", resp.Data)
	}
	if _, ok := data["status"]; !ok {
		t.Error("missing status field")
	}
	checks, ok := data["checks"].([]any)
	if !ok || checks == nil {
		t.Errorf("checks must be array, got %T", data["checks"])
	}
}

func mustWriteYaml(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
