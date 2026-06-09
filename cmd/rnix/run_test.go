package main

import (
	"strings"
	"testing"

	agentshell "github.com/rnixai/rnix/shell"
	"github.com/spf13/cobra"
)

// ============================================================
// ATDD RED PHASE — Story 18.5: 模块化与脚本执行 — rnix run CLI
//
// Tests reference runCmd (cobra.Command) and runRunCmd function
// — which do NOT exist yet → compile failure = RED phase.
// ============================================================

// --- 18.5-CLI-001: [P0] run 子命令已注册 ---

func TestRunCmd_Registered(t *testing.T) {
	var buf strings.Builder
	rootCmd.SetOut(&buf)
	rootCmd.SetArgs([]string{"--help"})
	t.Cleanup(func() {
		rootCmd.SetOut(nil)
		rootCmd.SetArgs(nil)
	})
	_ = rootCmd.Execute()

	output := buf.String()
	if !strings.Contains(output, "run") {
		t.Errorf("expected 'run' subcommand in help, got %q", output)
	}
}

// --- 18.5-CLI-002: [P0] run 无参数报错 ---

func TestRunCmd_NoArgs(t *testing.T) {
	if runCmd.Args == nil {
		t.Fatal("runCmd.Args should be set (expected MinimumNArgs(1))")
	}
	if err := runCmd.Args(runCmd, []string{}); err == nil {
		t.Error("expected error with 0 args")
	}
	if err := runCmd.Args(runCmd, []string{"script.ash"}); err != nil {
		t.Errorf("expected success with 1 arg, got %v", err)
	}
}

// --- 18.5-CLI-003: [P0] run 文件不存在报错含文件名 ---

func TestRunCmd_FileNotFound(t *testing.T) {
	saved := exitCode
	defer func() { exitCode = saved }()
	exitCode = 0

	err := runRunCmd(&cobra.Command{}, []string{"/nonexistent/script.ash"})
	if err == nil {
		if exitCode == 0 {
			t.Fatal("expected error or non-zero exitCode for non-existent file")
		}
	}
}

// --- 18.5-CLI-004: [P0] runCmd Use 和 Short 描述正确 ---

func TestRunCmd_UsageAndDescription(t *testing.T) {
	if !strings.Contains(runCmd.Use, "run") {
		t.Errorf("runCmd.Use should contain 'run', got %q", runCmd.Use)
	}
	if !strings.Contains(runCmd.Use, "script") {
		t.Errorf("runCmd.Use should mention script, got %q", runCmd.Use)
	}
	if runCmd.Short == "" {
		t.Error("runCmd.Short should not be empty")
	}
}

// --- 18.5-CLI-005: [P1] runCmd 支持 --json flag（继承自 root）---

func TestRunCmd_SupportsJSONFlag(t *testing.T) {
	f := runCmd.Flags().Lookup("json")
	if f == nil {
		f = runCmd.InheritedFlags().Lookup("json")
	}
	if f == nil {
		t.Error("run command should support --json flag (either local or inherited)")
	}
}

// --- 18.5-CLI-006: [P0] StripShebang 在 run 路径正确去除 shebang (AC3) ---

func TestRunCmd_ShebangStripped(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect string
	}{
		{"with shebang", "#!/usr/bin/env -S rnix run\nexport A=1", "export A=1"},
		{"no shebang", "export A=1", "export A=1"},
		{"shebang only", "#!/usr/bin/env -S rnix run", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := agentshell.StripShebang(tt.input)
			if got != tt.expect {
				t.Errorf("StripShebang(%q) = %q, want %q", tt.input, got, tt.expect)
			}
		})
	}
}
