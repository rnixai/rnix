package llm

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Regression tests for the `command` config override (providers.yaml `command:`
// → factory WithCommand). Root cause: resolveClaudeBinary previously iterated
// only fallbackBins, ignoring an explicit command override, so a configured
// binary (e.g. "agimqtt") was silently replaced by claude/openclaude — which
// then failed auth with a confusing 401. See
// investigations/claude-cli-command-override-ignored-investigation.md.

// 覆盖-命中：command 显式设置且二进制在 PATH → 解析到 override bin，不回退 claude。
func TestClaudeCli_CommandOverride_ResolvesOverrideBinary(t *testing.T) {
	// no t.Parallel: t.Setenv modifies shared process environment

	tmpDir := t.TempDir()
	// 同时放一个真实 claude，证明候选独占 [agimqtt]、从未尝试 claude。
	for _, name := range []string{"claude", "agimqtt"} {
		if err := os.WriteFile(filepath.Join(tmpDir, name), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", tmpDir)

	d := NewClaudeCliDriver(
		WithCommand("agimqtt"),
		// 注入空扩展目录，避免宿主 ~/.local/bin/claude 干扰（与 atdd_40_2 同源）。
		WithExtendedBinDirs(func() []string { return nil }),
	)
	resolved, err := d.resolveClaudeBinary()
	if err != nil {
		t.Fatalf("resolveClaudeBinary returned error: %v", err)
	}
	if !strings.HasSuffix(resolved, "agimqtt") {
		t.Errorf("expected resolved path to end with override binary 'agimqtt', got %q", resolved)
	}
}

// 覆盖-未命中：command 设置但二进制缺失（claude 存在）→ 报错且绝不回退 claude。
func TestClaudeCli_CommandOverride_MissingBinaryErrorsNoFallback(t *testing.T) {
	// no t.Parallel: t.Setenv modifies shared process environment

	tmpDir := t.TempDir()
	// 只放 claude，不放 agimqtt：复现用户场景，验证不再静默回退到 claude。
	if err := os.WriteFile(filepath.Join(tmpDir, "claude"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", tmpDir)

	d := NewClaudeCliDriver(
		WithCommand("agimqtt"),
		WithExtendedBinDirs(func() []string { return nil }),
	)
	resolved, err := d.resolveClaudeBinary()
	if err == nil {
		t.Fatalf("expected error when override binary missing, got resolved=%q", resolved)
	}
	if !strings.Contains(err.Error(), "agimqtt") {
		t.Errorf("error %q should name the missing override binary 'agimqtt'", err.Error())
	}
	if resolved != "" {
		t.Errorf("override miss must NOT fall back (got resolved=%q); claude in PATH must stay untouched", resolved)
	}
}

// DriverMeta 的 fallback_candidates 须反映真实候选：覆盖时 = override bin，
// 未设 command 时 = 默认 claude,openclaude（守护可观测性，防面板误导）。
func TestClaudeCli_CommandOverride_DriverMetaReflectsCandidates(t *testing.T) {
	t.Parallel()

	overridden := NewClaudeCliDriver(
		WithCommand("agimqtt"),
		WithCommandBuilder(mockCmdBuilder("success")),
	)
	if got := overridden.DriverMeta()["fallback_candidates"]; got != "agimqtt" {
		t.Errorf("overridden fallback_candidates = %q, want %q", got, "agimqtt")
	}

	def := NewClaudeCliDriver(WithCommandBuilder(mockCmdBuilder("success")))
	if got := def.DriverMeta()["fallback_candidates"]; got != "claude,openclaude" {
		t.Errorf("default fallback_candidates = %q, want %q", got, "claude,openclaude")
	}
}
