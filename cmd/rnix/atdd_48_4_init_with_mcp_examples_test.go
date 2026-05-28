// =============================================================================
// Story 48.4 — `rnix init --with-mcp-examples` ATDD (GREEN)
//
// Asserts Story 48.4 AC1 + AC6.
//
//	go test -run TestATDD_48_4_00 ./cmd/rnix/
// =============================================================================

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// -----------------------------------------------------------------------------
// helper: 在临时目录中替换 GlobalDir 解析,让 init 把 mcp.yaml 写到隔离位置
// 复用 check.go::mcpConfigPathForCheck 同款 monkey-patch 思路
// -----------------------------------------------------------------------------

// swapGlobalDirForInit redirects config.GlobalDir() to a tempdir for the duration
// of the test. dev-story 需要在 init.go 暴露一个 var globalDirForInit (默认调
// config.GlobalDir),与 check.go 的 mcpConfigPathForCheck 注入风格一致.
func swapGlobalDirForInit(t *testing.T, dir string) func() {
	t.Helper()
	old := globalDirForInit
	globalDirForInit = dir
	return func() { globalDirForInit = old }
}

// runInitCmd executes `rnix init [flags]` against the cobra rootCmd and returns
// the captured stdout/stderr buffer.
func runInitCmd(t *testing.T, args ...string) string {
	t.Helper()
	// Other ATDD tests in this package call rootCmd.SetArgs([...]) without
	// resetting it. cobra v1.10.2 bubbles initCmd.Execute() up to rootCmd
	// and reads root.args, so leftover values from earlier tests would
	// reroute this dispatch to whatever sub-command they last invoked.
	// Reset BOTH rootCmd and initCmd args (the dispatch hack in runRoot also
	// reads initCmd.args via reflect) and re-clear in cleanup so this helper
	// is hermetic regardless of who runs after.
	rootCmd.SetArgs(nil)
	initCmd.SetArgs(nil)
	t.Cleanup(func() {
		rootCmd.SetArgs(nil)
		initCmd.SetArgs(nil)
	})
	var buf bytes.Buffer
	cmd, _, err := rootCmd.Find(append([]string{"init"}, args...))
	if err != nil {
		t.Fatalf("init command not registered: %v", err)
	}
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("init Execute: %v\noutput=%s", err, buf.String())
	}
	return buf.String()
}

// -----------------------------------------------------------------------------
// _001 (AC1): --with-mcp-examples 在 mcp.yaml 不存在时生成模板
//
// Given globalDir 不含 mcp.yaml
// When  rnix init --with-mcp-examples
// Then  mcp.yaml 写入 + 文件内容字节相等于 mcpExampleYAML const
// -----------------------------------------------------------------------------
func TestATDD_48_4_001_InitWithMcpExamples_GeneratesYaml(t *testing.T) {
	dir := t.TempDir()
	defer swapGlobalDirForInit(t, dir)()
	defer resetExitCode(t)()

	_ = runInitCmd(t, "--with-mcp-examples")

	mcpPath := filepath.Join(dir, "mcp.yaml")
	got, err := os.ReadFile(mcpPath)
	if err != nil {
		t.Fatalf("mcp.yaml not created at %s: %v", mcpPath, err)
	}
	if string(got) != mcpExampleYAML {
		t.Errorf("mcp.yaml content drift\n--- got ---\n%s\n--- want (mcpExampleYAML) ---\n%s",
			string(got), mcpExampleYAML)
	}

	// 模板必须含 playwright server + 引用真实命令 (AC7 防 hint 失效)
	for _, want := range []string{
		"servers:",
		"playwright:",
		"npx",
		"@playwright/mcp",
		"transport_type: stdio",
		"rnix check mcp",
		"rnix mcp test playwright",
	} {
		if !strings.Contains(string(got), want) {
			t.Errorf("mcpExampleYAML missing %q\n%s", want, string(got))
		}
	}

	// 模板**禁止**嵌入 timestamp / username (idempotency + byte-stable 断言)
	banned := []string{"generated at 20", "timestamp:"}
	if user := os.Getenv("USER"); user != "" {
		// Guard: when USER is unset (some CI containers / Docker / `su -`),
		// "@"+USER collapses to "@" and would falsely match every `@scope/pkg`
		// reference in the template.
		banned = append(banned, "@"+user)
	}
	for _, b := range banned {
		if strings.Contains(string(got), b) {
			t.Errorf("mcpExampleYAML must not embed per-machine token %q", b)
		}
	}
}

// -----------------------------------------------------------------------------
// _002 (AC1): --with-mcp-examples 在 terminal 输出三段引导
// -----------------------------------------------------------------------------
func TestATDD_48_4_002_InitWithMcpExamples_PrintsGuidance(t *testing.T) {
	dir := t.TempDir()
	defer swapGlobalDirForInit(t, dir)()
	defer resetExitCode(t)()

	out := runInitCmd(t, "--with-mcp-examples")

	// 第一段: MCP 示例已启用 + mcp.yaml 路径
	for _, want := range []string{
		"MCP 示例已启用",
		filepath.Join(dir, "mcp.yaml"),
	} {
		if !strings.Contains(out, want) {
			t.Errorf("guidance section 1 missing %q\n%s", want, out)
		}
	}

	// 第二段: 快速验证三条命令 (顺序与 AC1 文案一致)
	for _, want := range []string{
		"rnix check mcp",
		"rnix mcp test playwright",
		"rnix --agent playwright-demo",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("guidance section 2 missing %q\n%s", want, out)
		}
	}

	// 第三段: 前置依赖 (node + chromium 提示)
	for _, want := range []string{
		"node",
		"npx",
		"chromium",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("guidance section 3 missing %q\n%s", want, out)
		}
	}
}

// -----------------------------------------------------------------------------
// _003 (AC1): --with-mcp-examples 在 mcp.yaml 已存在时 idempotent skip
// -----------------------------------------------------------------------------
func TestATDD_48_4_003_InitWithMcpExamples_Idempotent(t *testing.T) {
	dir := t.TempDir()
	defer swapGlobalDirForInit(t, dir)()
	defer resetExitCode(t)()

	// 先写一份用户自定义的 mcp.yaml,跑 init 应当不覆盖
	mcpPath := filepath.Join(dir, "mcp.yaml")
	custom := "# custom mcp.yaml — DO NOT OVERWRITE\nservers: {}\n"
	if err := os.WriteFile(mcpPath, []byte(custom), 0o644); err != nil {
		t.Fatalf("seed custom mcp.yaml: %v", err)
	}

	out := runInitCmd(t, "--with-mcp-examples")

	got, err := os.ReadFile(mcpPath)
	if err != nil {
		t.Fatalf("re-read mcp.yaml: %v", err)
	}
	if string(got) != custom {
		t.Errorf("idempotent: mcp.yaml content changed\n--- got ---\n%s\n--- want (custom) ---\n%s",
			string(got), custom)
	}

	// 输出应该提示 skip + 仍打印引导段 (用户可能要看 hint)
	if !strings.Contains(out, "skipping") && !strings.Contains(out, "already exists") {
		t.Errorf("expected 'already exists' / 'skipping' hint in stdout, got:\n%s", out)
	}
	if !strings.Contains(out, "rnix check mcp") {
		t.Errorf("expected guidance section to still print on skip path, got:\n%s", out)
	}
}

// -----------------------------------------------------------------------------
// _004 (AC1): 不带 --with-mcp-examples → 不写 mcp.yaml + 无引导段 (零回归)
// -----------------------------------------------------------------------------
func TestATDD_48_4_004_Init_WithoutFlag_NoMcpYaml(t *testing.T) {
	dir := t.TempDir()
	defer swapGlobalDirForInit(t, dir)()
	defer resetExitCode(t)()

	out := runInitCmd(t /* no flag */)

	mcpPath := filepath.Join(dir, "mcp.yaml")
	if _, err := os.Stat(mcpPath); err == nil {
		t.Errorf("mcp.yaml created without --with-mcp-examples (path=%s)", mcpPath)
	}

	if strings.Contains(out, "MCP 示例已启用") {
		t.Errorf("guidance section emitted without flag, got:\n%s", out)
	}
	if strings.Contains(out, "rnix check mcp") {
		t.Errorf("guidance hint emitted without flag, got:\n%s", out)
	}
}

// -----------------------------------------------------------------------------
// _014 (AC6): --quiet + --with-mcp-examples → 仅输出 wrote 一行
// -----------------------------------------------------------------------------
func TestATDD_48_4_014_InitWithMcp_QuietMode(t *testing.T) {
	dir := t.TempDir()
	defer swapGlobalDirForInit(t, dir)()
	defer resetExitCode(t)()

	out := runInitCmd(t, "--with-mcp-examples", "--quiet")

	// quiet 模式应抑制"快速验证 / 前置依赖"等引导段
	if strings.Contains(out, "快速验证") {
		t.Errorf("quiet mode leaked guidance section, got:\n%s", out)
	}
	if strings.Contains(out, "前置依赖") {
		t.Errorf("quiet mode leaked prerequisite section, got:\n%s", out)
	}

	// 但 "wrote mcp.yaml" 行必须保留 (用户需要知道文件位置)
	if !strings.Contains(out, "mcp.yaml") {
		t.Errorf("quiet mode should still report wrote path, got:\n%s", out)
	}

	// 不超过 3 行 (allow some forgiveness for trailing newline / wrote line)
	lines := strings.Count(strings.TrimSpace(out), "\n") + 1
	if lines > 3 {
		t.Errorf("quiet mode output too verbose (%d lines):\n%s", lines, out)
	}
}
