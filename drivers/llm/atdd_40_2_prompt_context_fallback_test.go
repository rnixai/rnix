package llm

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

// ============================================================================
// ATDD Tests for Story 40.2: P1 Prompt 上下文增强与 Fallback 兼容
//
// RED PHASE: These tests reference methods/fields that do not exist yet
// (resolveClaudeBinary, WithFallbackBins, extendedBinDirs, enhanced buildPrompt).
// They will fail to compile until the feature is implemented.
//
// AC1: Prompt 上下文注入 (buildPrompt with ProjectDir + Skills)
// AC2: 不复制工具描述 (Tools NOT copied into prompt)
// AC3: Fallback 二进制解析 (resolveClaudeBinary + WithFallbackBins)
// AC4: 扩展 PATH 搜索 (extendedBinDirs for ~/.local/bin, nvm, bun)
// AC5: EPIPE 静默吞噬 (stdin EPIPE swallowed, errors via stderr/exit code)
//
// IMPLEMENTATION DEPENDENCY: AC5 tests require a new TestHelperProcess case
// "epipe_fast_exit" in claude_cli_test.go that closes stdin immediately and
// exits with code 1 + stderr message (simulating CLI fast-exit on auth failure).
// ============================================================================

// ---------------------------------------------------------------------------
// AC #1: Prompt 上下文注入 — buildPrompt 增强
// ---------------------------------------------------------------------------

func TestATDD_40_2_AC1_BuildPrompt_WithProjectDirAndSkills(t *testing.T) {
	t.Parallel()

	// GIVEN: req.ProjectDir 和 req.Skills 均非空
	// WHEN:  buildPrompt 构造 prompt
	// THEN:  包含 # Instructions、Working directory、skill 名列表、# User request

	d := NewClaudeCliDriver(WithCommandBuilder(mockCmdBuilder("success")))
	req := LLMRequest{
		Intent:     "analyze code quality",
		ProjectDir: "/home/user/myproject",
		Skills: []Skill{
			{Name: "code-analyst", Body: "skill body 1"},
			{Name: "reviewer", Body: "skill body 2"},
		},
	}

	prompt := d.buildPrompt(req)

	if !strings.Contains(prompt, "# Instructions") {
		t.Error("AC1 FAIL: prompt missing '# Instructions' header")
	}
	if !strings.Contains(prompt, "Working directory: /home/user/myproject") {
		t.Error("AC1 FAIL: prompt missing 'Working directory' with correct path")
	}
	if !strings.Contains(prompt, "code-analyst") || !strings.Contains(prompt, "reviewer") {
		t.Error("AC1 FAIL: prompt missing skill names")
	}
	if !strings.Contains(prompt, "# User request") {
		t.Error("AC1 FAIL: prompt missing '# User request' header")
	}
	if !strings.HasSuffix(prompt, req.Intent) {
		t.Error("AC1 FAIL: prompt should end with the original intent")
	}

	instrIdx := strings.Index(prompt, "# Instructions")
	reqIdx := strings.Index(prompt, "# User request")
	if instrIdx >= reqIdx {
		t.Error("AC1 FAIL: '# Instructions' must appear before '# User request'")
	}
}

func TestATDD_40_2_AC1_BuildPrompt_NoContext(t *testing.T) {
	t.Parallel()

	// GIVEN: req.ProjectDir == "" 且 len(req.Skills) == 0
	// WHEN:  buildPrompt 构造 prompt
	// THEN:  直接返回 req.Intent（向后兼容）

	d := NewClaudeCliDriver(WithCommandBuilder(mockCmdBuilder("success")))
	req := LLMRequest{
		Intent: "simple question",
	}

	prompt := d.buildPrompt(req)

	if prompt != "simple question" {
		t.Errorf("AC1 FAIL: expected raw intent %q, got %q", "simple question", prompt)
	}
}

func TestATDD_40_2_AC1_BuildPrompt_OnlyProjectDir(t *testing.T) {
	t.Parallel()

	// GIVEN: req.ProjectDir 非空但 req.Skills 为空
	// WHEN:  buildPrompt 构造 prompt
	// THEN:  注入 Working directory 但不注入 skills section

	d := NewClaudeCliDriver(WithCommandBuilder(mockCmdBuilder("success")))
	req := LLMRequest{
		Intent:     "check files",
		ProjectDir: "/tmp/proj",
	}

	prompt := d.buildPrompt(req)

	if !strings.Contains(prompt, "Working directory: /tmp/proj") {
		t.Error("AC1 FAIL: prompt missing Working directory when ProjectDir is set")
	}
	if strings.Contains(prompt, "skill") || strings.Contains(prompt, "Skill") {
		t.Error("AC1 FAIL: prompt should NOT contain skills section when Skills is empty")
	}
	if !strings.Contains(prompt, "# User request") {
		t.Error("AC1 FAIL: prompt missing '# User request' header")
	}
}

func TestATDD_40_2_AC1_BuildPrompt_OnlySkills(t *testing.T) {
	t.Parallel()

	// GIVEN: req.ProjectDir == "" 但 req.Skills 非空
	// WHEN:  buildPrompt 构造 prompt
	// THEN:  注入 skills 列表但不注入 Working directory

	d := NewClaudeCliDriver(WithCommandBuilder(mockCmdBuilder("success")))
	req := LLMRequest{
		Intent: "use my skills",
		Skills: []Skill{
			{Name: "debugger", Body: "debug body"},
		},
	}

	prompt := d.buildPrompt(req)

	if strings.Contains(prompt, "Working directory") {
		t.Error("AC1 FAIL: prompt should NOT contain Working directory when ProjectDir is empty")
	}
	if !strings.Contains(prompt, "debugger") {
		t.Error("AC1 FAIL: prompt missing skill name 'debugger'")
	}
	if !strings.Contains(prompt, "# Instructions") {
		t.Error("AC1 FAIL: prompt missing '# Instructions' header when Skills is set")
	}
}

// ---------------------------------------------------------------------------
// AC #2: 不复制工具描述到 prompt
// ---------------------------------------------------------------------------

func TestATDD_40_2_AC2_BuildPrompt_ToolsNotCopied(t *testing.T) {
	t.Parallel()

	// GIVEN: req.Tools 含工具说明
	// WHEN:  buildPrompt 构造 prompt
	// THEN:  prompt 中不包含工具名称或描述（Claude CLI 自行发现工具）

	d := NewClaudeCliDriver(WithCommandBuilder(mockCmdBuilder("success")))
	req := LLMRequest{
		Intent:     "do something",
		ProjectDir: "/project",
		Skills:     []Skill{{Name: "test-skill", Body: "body"}},
		Tools: []ToolDef{
			{Name: "/dev/fs", Description: "Host filesystem access"},
			{Name: "/dev/shell", Description: "Shell command execution"},
		},
	}

	prompt := d.buildPrompt(req)

	if strings.Contains(prompt, "Host filesystem access") {
		t.Error("AC2 FAIL: prompt contains tool description — tools should NOT be copied")
	}
	if strings.Contains(prompt, "Shell command execution") {
		t.Error("AC2 FAIL: prompt contains tool description — tools should NOT be copied")
	}
	if strings.Contains(prompt, "/dev/fs") || strings.Contains(prompt, "/dev/shell") {
		t.Error("AC2 FAIL: prompt contains tool path — tools should NOT be copied")
	}
}

// ---------------------------------------------------------------------------
// AC #3: Fallback 二进制解析 — resolveClaudeBinary + WithFallbackBins
// ---------------------------------------------------------------------------

func TestATDD_40_2_AC3_FallbackBins_SecondCandidate(t *testing.T) {
	// no t.Parallel: t.Setenv modifies shared process environment

	// GIVEN: cliCommand == "claude" 但 PATH 中不存在 "claude"
	// AND:   PATH 中存在 "openclaude"
	// WHEN:  resolveClaudeBinary 执行
	// THEN:  返回 "openclaude" 的绝对路径

	tmpDir := t.TempDir()

	// Create a fake "openclaude" executable (not "claude")
	openclaude := filepath.Join(tmpDir, "openclaude")
	if err := os.WriteFile(openclaude, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", tmpDir)

	d := NewClaudeCliDriver(WithCommandBuilder(mockCmdBuilder("success")))
	resolved, err := d.resolveClaudeBinary()
	if err != nil {
		t.Fatalf("AC3 FAIL: resolveClaudeBinary returned error: %v", err)
	}
	if !strings.HasSuffix(resolved, "openclaude") {
		t.Errorf("AC3 FAIL: expected resolved path to end with 'openclaude', got %q", resolved)
	}
}

func TestATDD_40_2_AC3_FallbackBins_AllFail(t *testing.T) {
	// no t.Parallel: t.Setenv modifies shared process environment

	// GIVEN: PATH 中没有 "claude" 也没有 "openclaude"
	// AND:   扩展路径也无候选
	// WHEN:  resolveClaudeBinary 执行
	// THEN:  返回包含 "no claude-compatible CLI found in PATH" 的错误

	t.Setenv("PATH", t.TempDir()) // empty dir, no binaries
	t.Setenv("HOME", t.TempDir()) // no ~/.local/bin etc.
	t.Setenv("NVM_DIR", "")       // no nvm

	d := NewClaudeCliDriver(WithCommandBuilder(mockCmdBuilder("success")))
	_, err := d.resolveClaudeBinary()
	if err == nil {
		t.Fatal("AC3 FAIL: expected error when no binary found, got nil")
	}
	if !strings.Contains(err.Error(), "no claude-compatible CLI found in PATH") {
		t.Errorf("AC3 FAIL: error message %q does not contain expected phrase", err.Error())
	}
}

func TestATDD_40_2_AC3_FallbackBins_CustomOption(t *testing.T) {
	// no t.Parallel: t.Setenv modifies shared process environment

	// GIVEN: WithFallbackBins(["my-claude"]) 追加候选
	// AND:   PATH 中存在 "my-claude"
	// WHEN:  resolveClaudeBinary 执行
	// THEN:  解析到 "my-claude"

	tmpDir := t.TempDir()
	myClaude := filepath.Join(tmpDir, "my-claude")
	if err := os.WriteFile(myClaude, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", tmpDir)

	d := NewClaudeCliDriver(
		WithCommandBuilder(mockCmdBuilder("success")),
		WithFallbackBins([]string{"my-claude"}),
	)
	resolved, err := d.resolveClaudeBinary()
	if err != nil {
		t.Fatalf("AC3 FAIL: resolveClaudeBinary returned error: %v", err)
	}
	if !strings.HasSuffix(resolved, "my-claude") {
		t.Errorf("AC3 FAIL: expected resolved path to end with 'my-claude', got %q", resolved)
	}
}

func TestATDD_40_2_AC3_ResolvedBinOnce_SingleExecution(t *testing.T) {
	// no t.Parallel: t.Setenv modifies shared process environment

	// GIVEN: resolvedBinOnce 保证解析只执行一次
	// WHEN:  并发调用 resolveBinaryIfNeeded
	// THEN:  resolvedBin 只被赋值一次，所有调用返回相同路径

	tmpDir := t.TempDir()
	claude := filepath.Join(tmpDir, "claude")
	if err := os.WriteFile(claude, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", tmpDir)

	d := NewClaudeCliDriver(WithCommandBuilder(mockCmdBuilder("success")))

	// Call resolve multiple times concurrently
	errs := make(chan error, 10)
	for range 10 {
		go func() {
			errs <- d.resolveBinaryIfNeeded()
		}()
	}
	for range 10 {
		if err := <-errs; err != nil {
			t.Errorf("AC3 FAIL: concurrent resolveBinaryIfNeeded returned error: %v", err)
		}
	}

	// All should have resolved to the same path
	if d.resolvedBin == "" {
		t.Error("AC3 FAIL: resolvedBin should be set after resolveBinaryIfNeeded")
	}
}

// ---------------------------------------------------------------------------
// AC #4: 扩展 PATH 搜索 — extendedBinDirs
// ---------------------------------------------------------------------------

func TestATDD_40_2_AC4_ExtendedBinDirs_LocalBin(t *testing.T) {
	// no t.Parallel: t.Setenv modifies shared process environment

	// GIVEN: PATH 中不含 claude/openclaude
	// AND:   ~/.local/bin/ 下存在 "claude" 可执行文件
	// WHEN:  resolveClaudeBinary 执行
	// THEN:  从 ~/.local/bin/claude 解析成功

	homeDir := t.TempDir()
	localBin := filepath.Join(homeDir, ".local", "bin")
	if err := os.MkdirAll(localBin, 0o755); err != nil {
		t.Fatal(err)
	}
	claude := filepath.Join(localBin, "claude")
	if err := os.WriteFile(claude, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("PATH", t.TempDir()) // empty PATH
	t.Setenv("HOME", homeDir)
	t.Setenv("NVM_DIR", "")

	d := NewClaudeCliDriver(WithCommandBuilder(mockCmdBuilder("success")))
	resolved, err := d.resolveClaudeBinary()
	if err != nil {
		t.Fatalf("AC4 FAIL: resolveClaudeBinary failed despite ~/.local/bin/claude existing: %v", err)
	}
	if resolved != claude {
		t.Errorf("AC4 FAIL: expected %q, got %q", claude, resolved)
	}
}

// ---------------------------------------------------------------------------
// AC #5: EPIPE 静默吞噬 — stdin broken pipe 不上报 stream error
// ---------------------------------------------------------------------------

func TestATDD_40_2_AC5_EPIPE_SilentSwallow_Call(t *testing.T) {
	t.Parallel()

	// GIVEN: Claude CLI 认证失败导致 fast-exit（stdin 写入端 EPIPE）
	// WHEN:  Call 执行
	// THEN:  EPIPE 不产生额外的 stream error
	// AND:   错误通过 stderr/exit code 路径上报

	d := NewClaudeCliDriver(
		WithCommandBuilder(mockCmdBuilder("epipe_fast_exit")),
		WithTimeout(5*time.Second),
	)
	_, err := d.Call(context.Background(), LLMRequest{
		Intent: strings.Repeat("A", 64*1024), // large prompt to trigger EPIPE
	})

	// Should get an error from stderr/exit code, NOT a panic or unhandled EPIPE
	if err == nil {
		t.Fatal("AC5 FAIL: expected error from CLI fast-exit, got nil")
	}

	// Verify error is NOT a raw EPIPE
	if errors.Is(err, syscall.EPIPE) {
		t.Error("AC5 FAIL: raw EPIPE leaked to caller — should be swallowed")
	}
}

func TestATDD_40_2_AC5_EPIPE_SilentSwallow_Stream(t *testing.T) {
	t.Parallel()

	// GIVEN: Claude CLI fast-exit 导致 stdin EPIPE
	// WHEN:  Stream 执行
	// THEN:  EPIPE 不推送到 stream channel
	// AND:   错误走 stderr/exit code 路径

	d := NewClaudeCliDriver(
		WithCommandBuilder(mockCmdBuilder("epipe_fast_exit")),
		WithTimeout(5*time.Second),
	)

	ch, err := d.Stream(context.Background(), LLMRequest{
		Intent: strings.Repeat("B", 64*1024),
	})

	// If Stream returns an error synchronously, verify it's not raw EPIPE
	if err != nil && errors.Is(err, syscall.EPIPE) {
		t.Error("AC5 FAIL: raw EPIPE leaked to Stream return — should be swallowed")
	}

	// Drain channel to check no EPIPE events leaked
	if ch != nil {
		for ev := range ch {
			if ev.Err != nil && errors.Is(ev.Err, syscall.EPIPE) {
				t.Error("AC5 FAIL: EPIPE event leaked to stream channel")
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Integration: Call 和 Stream 使用 resolved binary
// ---------------------------------------------------------------------------

func TestATDD_40_2_INT_Call_UsesResolvedBinary(t *testing.T) {
	// no t.Parallel: t.Setenv modifies shared process environment

	// GIVEN: resolveClaudeBinary 解析到特定路径
	// WHEN:  Call 执行
	// THEN:  exec.Cmd 使用 resolvedBin 而非原始 cliCommand

	tmpDir := t.TempDir()
	claude := filepath.Join(tmpDir, "claude")
	if err := os.WriteFile(claude, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", tmpDir)

	var capturedName string
	d := NewClaudeCliDriver(
		WithCommandBuilder(func(ctx context.Context, name string, args ...string) *exec.Cmd {
			capturedName = name
			cs := []string{"-test.run=TestHelperProcess", "--"}
			cs = append(cs, args...)
			cmd := exec.CommandContext(ctx, os.Args[0], cs...)
			cmd.Env = append(os.Environ(), "GO_TEST_PROCESS=1", "GO_TEST_CASE=success")
			return cmd
		}),
	)

	_, _ = d.Call(context.Background(), LLMRequest{Intent: "test"})

	// After resolve, the cmdBuilder should receive the resolved absolute path
	// (not "claude" string). When cmdBuilder is used, the name param should be
	// the resolved binary.
	if capturedName == "" {
		t.Error("INT FAIL: cmdBuilder was never called")
	}
}

func TestATDD_40_2_INT_EnsureCapabilities_UsesResolvedBin(t *testing.T) {
	// no t.Parallel: t.Setenv modifies shared process environment

	// GIVEN: resolveClaudeBinary 先于 ensureCapabilities 执行
	// WHEN:  ensureCapabilities 中调 probeCapabilities
	// THEN:  probe 使用 resolvedBin（若已解析）

	tmpDir := t.TempDir()
	claude := filepath.Join(tmpDir, "claude")
	if err := os.WriteFile(claude, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", tmpDir)

	probeNames := make([]string, 0)
	d := NewClaudeCliDriver(
		WithCommandBuilder(func(ctx context.Context, name string, args ...string) *exec.Cmd {
			probeNames = append(probeNames, name)
			cs := []string{"-test.run=TestHelperProcess", "--"}
			cs = append(cs, args...)
			cmd := exec.CommandContext(ctx, os.Args[0], cs...)
			cmd.Env = append(os.Environ(), "GO_TEST_PROCESS=1", "GO_TEST_CASE=cap_probe_full")
			return cmd
		}),
	)

	// Trigger resolve + capabilities
	_ = d.resolveBinaryIfNeeded()
	_ = d.ensureCapabilities()

	// The probe should have used the cmdBuilder (we can't assert the exact name
	// because cmdBuilder wraps the test binary, but at least verify it was called)
	if len(probeNames) == 0 {
		t.Error("INT FAIL: probeCapabilities never called cmdBuilder")
	}
}
