// =============================================================================
// Story 48.4 — `rnix --agent playwright-demo` spawn preflight ATDD (GREEN)
//
// Asserts Story 48.4 AC3 / AC4 / AC6 / AC7:
//   - playwright-demo 触发预检 (mcp.yaml / npx / chromium)
//   - 非 playwright-demo agent 完全 bypass
//   - ERROR 阻断 spawn + 引导式三段式输出 + exit 1
//   - WARN 不阻断 + 继续 spawn (ok=true)
//   - JSON / ASCII / quiet 三模式渲染正确
// =============================================================================

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

// -----------------------------------------------------------------------------
// helpers (复用 check.go monkey-patch 风格 + preflight 自己的注入点)
// -----------------------------------------------------------------------------

func swapPreflightLookPath(replacement func(string) (string, error)) func() {
	old := preflightLookPath
	preflightLookPath = replacement
	return func() { preflightLookPath = old }
}

func swapPreflightDetectChromium(replacement func() (string, error)) func() {
	old := preflightDetectChromium
	preflightDetectChromium = replacement
	return func() { preflightDetectChromium = old }
}

func swapPreflightMcpYamlPath(path string) func() {
	old := preflightMcpYamlPath
	preflightMcpYamlPath = path
	return func() { preflightMcpYamlPath = old }
}

// seedMcpYamlWithPlaywright writes a minimal mcp.yaml that references playwright.
func seedMcpYamlWithPlaywright(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp.yaml")
	body := `servers:
  playwright:
    command: npx
    args: ["-y", "@playwright/mcp@latest"]
    transport_type: stdio
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("seed mcp.yaml: %v", err)
	}
	return path
}

// runPreflightForAgent invokes runSpawnPreflight against the rootCmd-derived
// container and returns (ok, output). rootCmd.Find(nil) returns root itself
// with err==nil per cobra contract, so the only failure mode worth defending
// is the panic-on-deref path, not an explicit error branch.
func runPreflightForAgent(t *testing.T, agentName string) (bool, string) {
	t.Helper()
	cmd, _, _ := rootCmd.Find(nil)
	if cmd == nil {
		t.Fatalf("rootCmd.Find(nil) returned nil cobra.Command — rootCmd init regression")
	}
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	ok := runSpawnPreflight(cmd, agentName)
	return ok, buf.String()
}

// -----------------------------------------------------------------------------
// _007 (AC3): playwright-demo + npx 缺失 → ERROR 阻断 + 三段式引导 + exit 1
// -----------------------------------------------------------------------------
func TestATDD_48_4_007_Preflight_NoNpx_BlocksAndGuides(t *testing.T) {
	yamlPath := seedMcpYamlWithPlaywright(t)
	defer swapPreflightMcpYamlPath(yamlPath)()
	defer swapPreflightLookPath(func(name string) (string, error) {
		if name == "node" {
			return "/usr/bin/node", nil
		}
		return "", exec.ErrNotFound // npx 缺
	})()
	defer swapPreflightDetectChromium(func() (string, error) {
		return "/usr/bin/chromium", nil
	})()
	defer resetExitCode(t)()

	ok, out := runPreflightForAgent(t, "playwright-demo")

	if ok {
		t.Fatalf("preflight should return ok=false when npx missing, got ok=true\noutput=%s", out)
	}
	if exitCode != 1 {
		t.Errorf("exitCode=%d, want 1 after preflight ERROR", exitCode)
	}

	// 三段式: what / why / how
	// what
	if !strings.Contains(out, "preflight") && !strings.Contains(out, "Preflight") {
		t.Errorf("missing 'preflight' label in error, got:\n%s", out)
	}
	if !strings.Contains(out, "npx") {
		t.Errorf("missing 'npx' (what) in error, got:\n%s", out)
	}
	// why: $PATH 提示
	if !strings.Contains(out, "PATH") {
		t.Errorf("missing 'PATH' (why) in error, got:\n%s", out)
	}
	// how: 真实可跑的修复命令 (AC7)
	hasHow := false
	for _, hint := range []string{"npm install", "brew install node", "nodejs.org"} {
		if strings.Contains(out, hint) {
			hasHow = true
			break
		}
	}
	if !hasHow {
		t.Errorf("missing 'how' install hint in error, got:\n%s", out)
	}
	// 验证 + 重试 hint
	if !strings.Contains(out, "rnix check mcp") {
		t.Errorf("missing 'rnix check mcp' retry hint, got:\n%s", out)
	}

	// AC7: 引导命令不得虚构未实现命令
	for _, banned := range []string{"rnix mcp install", "rnix mcp setup"} {
		if strings.Contains(out, banned) {
			t.Errorf("output references nonexistent command %q:\n%s", banned, out)
		}
	}
}

// -----------------------------------------------------------------------------
// _008 (AC3): playwright-demo + mcp.yaml 不存在 → ERROR + 引导跑 init flag
// -----------------------------------------------------------------------------
func TestATDD_48_4_008_Preflight_NoMcpYaml_BlocksAndGuides(t *testing.T) {
	// 指向一个肯定不存在的路径
	defer swapPreflightMcpYamlPath("/tmp/nonexistent-48-4-mcp.yaml")()
	defer swapPreflightLookPath(func(name string) (string, error) {
		return "/usr/bin/" + name, nil // npx/node 都装
	})()
	defer swapPreflightDetectChromium(func() (string, error) {
		return "/usr/bin/chromium", nil
	})()
	defer resetExitCode(t)()

	ok, out := runPreflightForAgent(t, "playwright-demo")

	if ok {
		t.Fatalf("preflight should return ok=false when mcp.yaml missing, got ok=true\noutput=%s", out)
	}
	if exitCode != 1 {
		t.Errorf("exitCode=%d, want 1", exitCode)
	}

	if !strings.Contains(out, "mcp.yaml") {
		t.Errorf("missing 'mcp.yaml' in error, got:\n%s", out)
	}
	// 引导用户跑 init flag (AC7 verified command)
	if !strings.Contains(out, "rnix init --with-mcp-examples") {
		t.Errorf("missing 'rnix init --with-mcp-examples' hint, got:\n%s", out)
	}
}

// -----------------------------------------------------------------------------
// _009 (AC3): 非 playwright-demo agent (如 code-analyst) → preflight bypass
// -----------------------------------------------------------------------------
func TestATDD_48_4_009_Preflight_NonTriggerAgent_Bypasses(t *testing.T) {
	// 故意把所有外部依赖都设成失败状态:如果 preflight 真的跑了肯定 ok=false
	defer swapPreflightMcpYamlPath("/tmp/should-not-be-checked.yaml")()
	defer swapPreflightLookPath(func(string) (string, error) {
		return "", exec.ErrNotFound
	})()
	defer swapPreflightDetectChromium(func() (string, error) {
		return "", errors.New("intentional")
	})()
	defer resetExitCode(t)()

	for _, agent := range []string{"code-analyst", "stem", "orchestrator", ""} {
		t.Run("agent="+agent, func(t *testing.T) {
			ok, out := runPreflightForAgent(t, agent)
			if !ok {
				t.Errorf("preflight should bypass for agent=%q, but got ok=false\noutput=%s",
					agent, out)
			}
			if exitCode != 0 {
				t.Errorf("exitCode=%d, want 0 (bypass path)", exitCode)
			}
			if strings.Contains(out, "preflight") || strings.Contains(out, "Preflight") {
				t.Errorf("preflight should produce no output for non-trigger agent, got:\n%s", out)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// _010 (AC6): --json + playwright-demo + npx 缺失 → JSON 错误负载
// -----------------------------------------------------------------------------
func TestATDD_48_4_010_Preflight_JSONMode(t *testing.T) {
	yamlPath := seedMcpYamlWithPlaywright(t)
	defer swapPreflightMcpYamlPath(yamlPath)()
	defer swapPreflightLookPath(func(name string) (string, error) {
		return "", exec.ErrNotFound // 全缺
	})()
	defer resetExitCode(t)()

	oldJSON := flagJSON
	flagJSON = true
	defer func() { flagJSON = oldJSON }()

	ok, out := runPreflightForAgent(t, "playwright-demo")

	if ok {
		t.Fatalf("preflight should return ok=false in JSON mode, got ok=true\noutput=%s", out)
	}

	var resp JSONResponse
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("JSON parse failed: %v\nraw=%s", err, out)
	}
	if resp.OK {
		t.Error("JSONResponse.OK should be false on preflight failure")
	}
	errPayload, ok := resp.Error.(map[string]any)
	if !ok {
		t.Fatalf("Error payload shape: %T", resp.Error)
	}
	for _, want := range []string{"code", "message", "hint"} {
		if _, exists := errPayload[want]; !exists {
			t.Errorf("JSON error payload missing %q field; got=%v", want, errPayload)
		}
	}
	// code 必须是 sentinel 形式 (如 PREFLIGHT_NPX_MISSING)
	codeStr, _ := errPayload["code"].(string)
	if !strings.HasPrefix(codeStr, "PREFLIGHT_") {
		t.Errorf("error code should start with PREFLIGHT_, got %q", codeStr)
	}
}

// -----------------------------------------------------------------------------
// _011 (AC4): playwright-demo + 全 ok 但 Chromium 缺 → WARN 不阻断 + 继续 spawn
// -----------------------------------------------------------------------------
func TestATDD_48_4_011_Preflight_NoChromium_WarnsButContinues(t *testing.T) {
	yamlPath := seedMcpYamlWithPlaywright(t)
	defer swapPreflightMcpYamlPath(yamlPath)()
	defer swapPreflightLookPath(func(name string) (string, error) {
		return "/usr/bin/" + name, nil // npx + node 都有
	})()
	defer swapPreflightDetectChromium(func() (string, error) {
		return "", errors.New("chromium not found")
	})()
	defer resetExitCode(t)()

	ok, out := runPreflightForAgent(t, "playwright-demo")

	if !ok {
		t.Fatalf("WARN (no chromium) must NOT block spawn (AC4), got ok=false\noutput=%s", out)
	}
	if exitCode != 0 {
		t.Errorf("exitCode=%d, want 0 (WARN doesn't fail)", exitCode)
	}

	// 必须有 warning 字样
	if !strings.Contains(out, "WARN") && !strings.Contains(out, "Chromium") && !strings.Contains(out, "chromium") {
		t.Errorf("expected chromium WARN in output, got:\n%s", out)
	}
	// 必须含 install hint
	if !strings.Contains(out, "playwright install chromium") {
		t.Errorf("expected 'playwright install chromium' hint, got:\n%s", out)
	}
	// 应继续提示 spawn 继续 — exact-match "Continuing anyway" so a future
	// negation like "Operation discontinued" can't sneak past the assertion.
	if !strings.Contains(out, "Continuing anyway") {
		t.Errorf("expected exact 'Continuing anyway' phrase, got:\n%s", out)
	}
}

// -----------------------------------------------------------------------------
// _012 (AC4): playwright-demo + 全过 → preflight 静默继续 (无 stdout 污染)
// -----------------------------------------------------------------------------
func TestATDD_48_4_012_Preflight_AllPass_SilentContinue(t *testing.T) {
	yamlPath := seedMcpYamlWithPlaywright(t)
	defer swapPreflightMcpYamlPath(yamlPath)()
	defer swapPreflightLookPath(func(name string) (string, error) {
		return "/usr/bin/" + name, nil
	})()
	defer swapPreflightDetectChromium(func() (string, error) {
		return "/usr/bin/chromium", nil
	})()
	defer resetExitCode(t)()

	ok, out := runPreflightForAgent(t, "playwright-demo")

	if !ok {
		t.Fatalf("all-pass should return ok=true, got ok=false\noutput=%s", out)
	}
	if exitCode != 0 {
		t.Errorf("exitCode=%d, want 0", exitCode)
	}
	// AC4 显式要求"passing checks 静默" (不像 check mcp 会打印 All checks passed)
	// 允许极少的 debug 输出 (≤ 60 字符) 但不应有人类可读引导
	trimmed := strings.TrimSpace(out)
	if len(trimmed) > 60 {
		t.Errorf("all-pass preflight should be silent, got %d chars:\n%s", len(trimmed), out)
	}
	if strings.Contains(out, "All checks passed") {
		t.Errorf("preflight should NOT mirror check-mcp summary line, got:\n%s", out)
	}
}

// -----------------------------------------------------------------------------
// _015 (AC6): RNIX_ASCII=1 + preflight 失败 → 输出全 ASCII (无 Unicode 箭头)
// -----------------------------------------------------------------------------
func TestATDD_48_4_015_Preflight_AsciiMode_FallbackChars(t *testing.T) {
	yamlPath := seedMcpYamlWithPlaywright(t)
	defer swapPreflightMcpYamlPath(yamlPath)()
	defer swapPreflightLookPath(func(name string) (string, error) {
		return "", exec.ErrNotFound
	})()
	defer resetExitCode(t)()

	t.Setenv("RNIX_ASCII", "1")

	_, out := runPreflightForAgent(t, "playwright-demo")

	// 禁止 Unicode 符号
	for _, banned := range []string{"→", "✓", "✗", "•", "─", "│"} {
		if strings.Contains(out, banned) {
			t.Errorf("RNIX_ASCII=1 leaked unicode glyph %q\n%s", banned, out)
		}
	}
	// 但仍要保留语义信息 (修复 hint 等)
	if !strings.Contains(out, "npx") {
		t.Errorf("ASCII mode dropped 'npx' label\n%s", out)
	}
}
