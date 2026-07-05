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

// swapGlobalDirForInit redirects init's global config directory to a tempdir for
// the duration of the test and flips skipProjectInitForTest so runInit never
// touches the real CWD's .rnix. Both are package-level seams (globalDirForInit is
// also read by spawn_preflight.go); restoring them on cleanup keeps the suite
// hermetic. Because these vars are shared package state — as is the cobra rootCmd
// singleton driven by runInitCmd — this test group runs SERIALLY (no t.Parallel):
// distinct tempdirs across concurrent tests would race on globalDirForInit.
func swapGlobalDirForInit(t *testing.T, dir string) func() {
	t.Helper()
	oldDir := globalDirForInit
	oldSkip := skipProjectInitForTest
	globalDirForInit = dir
	skipProjectInitForTest = true
	return func() {
		globalDirForInit = oldDir
		skipProjectInitForTest = oldSkip
	}
}

// runInitCmd executes `rnix init [flags]` and returns the captured output.
//
// It uses the cobra-idiomatic test pattern: SetArgs on the ROOT command with the
// sub-command name prepended, then rootCmd.Execute(). cobra's Execute() always
// dispatches from Root().ExecuteC() using the root's args, so setting args on a
// sub-command (the old helper's rootCmd.Find + cmd.SetArgs) is silently ignored
// and Execute falls back to os.Args[1:] (the test binary's -test.* flags). Using
// root.SetArgs makes cmd.Flags().GetBool resolve correctly with no production-side
// test heuristics. flagQuiet is a persistent flag bound to a package var, so we
// reset it on cleanup to prevent leakage into later serial tests.
func runInitCmd(t *testing.T, args ...string) string {
	t.Helper()
	// cobra retains a flag's parsed value AND its Changed bit on the singleton
	// command between Execute() calls within one test process — real `rnix`
	// invocations are separate processes and never see this. Reset both flags
	// (value + Changed) to their defaults before parsing so a prior test's
	// `--with-mcp-examples` or `--quiet` cannot leak into a later run. --quiet is
	// a persistent flag on rootCmd bound to flagQuiet; resetting Value keeps its
	// Changed bit consistent with GetBool for any future Changed("quiet") reader.
	resetInitFlags := func() {
		if f := initCmd.Flags().Lookup("with-mcp-examples"); f != nil {
			_ = f.Value.Set("false")
			f.Changed = false
		}
		if f := rootCmd.PersistentFlags().Lookup("quiet"); f != nil {
			_ = f.Value.Set("false")
			f.Changed = false
		}
		flagQuiet = false
	}
	resetInitFlags()
	var buf bytes.Buffer
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs(append([]string{"init"}, args...))
	t.Cleanup(func() {
		rootCmd.SetArgs(nil)
		rootCmd.SetOut(nil)
		rootCmd.SetErr(nil)
		resetInitFlags()
	})
	if err := rootCmd.Execute(); err != nil {
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
		"MCP examples enabled",
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

	if strings.Contains(out, "MCP examples enabled") {
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
	if strings.Contains(out, "Quick check") {
		t.Errorf("quiet mode leaked guidance section, got:\n%s", out)
	}
	if strings.Contains(out, "Prerequisites") {
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
