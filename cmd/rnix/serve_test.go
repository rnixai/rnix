package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------------------
// Story 24.5: rnix serve CLI 命令与端到端集成 — ATDD 验收测试
// ---------------------------------------------------------------------------
// 这些测试在 serve.go 实现前处于 RED（失败）状态。
// RED 原因：serve 子命令尚未注册到 rootCmd。
//
// 测试覆盖的 AC：
//   AC#1 — serve 命令存在且输出启动信息
//   AC#2 — --port 参数（默认 8080，支持自定义）
// ---------------------------------------------------------------------------

// findServeCmd 在 rootCmd 中查找 serve 子命令。返回 nil 表示未注册。
func findServeCmd(t *testing.T) *cobra.Command {
	t.Helper()
	for _, cmd := range rootCmd.Commands() {
		if cmd.Name() == "serve" {
			return cmd
		}
	}
	return nil
}

// --- AC #1: serveCmd 注册到 rootCmd ----------------------------------------

func TestServeCmd_Exists(t *testing.T) {
	// Given: rootCmd 已注册所有子命令
	// When: 查找 "serve" 子命令
	// Then: serve 子命令应存在且 Use == "serve"
	cmd := findServeCmd(t)
	if cmd == nil {
		t.Fatal("expected 'serve' subcommand registered on rootCmd")
	}
	if cmd.Use != "serve" {
		t.Errorf("serve cmd Use = %q, want %q", cmd.Use, "serve")
	}
}

// --- AC #2: --port 参数默认值 -----------------------------------------------

func TestServeCmd_DefaultPort(t *testing.T) {
	// Given: serve 命令已注册
	// When: 查看 --port flag 的默认值
	// Then: 默认端口应为 8080
	cmd := findServeCmd(t)
	if cmd == nil {
		t.Fatal("serve command not found")
	}

	portFlag := cmd.Flags().Lookup("port")
	if portFlag == nil {
		t.Fatal("--port flag not found on serve command")
	}
	if portFlag.DefValue != "8080" {
		t.Errorf("--port default = %q, want %q", portFlag.DefValue, "8080")
	}
}

// --- AC #2: --port 自定义值 -------------------------------------------------

func TestServeCmd_CustomPort(t *testing.T) {
	// Given: serve 命令已注册
	// When: 设置 --port 9090
	// Then: 端口值应解析为 9090
	cmd := findServeCmd(t)
	if cmd == nil {
		t.Fatal("serve command not found")
	}

	if err := cmd.Flags().Set("port", "9090"); err != nil {
		t.Fatalf("setting --port: %v", err)
	}
	val, err := cmd.Flags().GetInt("port")
	if err != nil {
		t.Fatalf("getting --port value: %v", err)
	}
	if val != 9090 {
		t.Errorf("--port = %d, want 9090", val)
	}
}

// --- AC #1: Help 输出包含关键信息 -------------------------------------------

func TestServeCmd_HelpOutput(t *testing.T) {
	// Given: serve 命令已注册
	// When: 执行 serve --help
	// Then: 输出包含 "OpenAI" 和 "--port"
	cmd := findServeCmd(t)
	if cmd == nil {
		t.Fatal("serve command not found")
	}

	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--help"})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute serve --help: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "OpenAI") {
		t.Errorf("help output missing 'OpenAI', got:\n%s", output)
	}
	if !strings.Contains(output, "--port") {
		t.Errorf("help output missing '--port', got:\n%s", output)
	}
}

// --- AC #1: 命令具有可执行的 RunE -------------------------------------------

func TestServeCmd_HasRunE(t *testing.T) {
	// Given: serve 命令已注册
	// When: 检查 RunE 函数
	// Then: RunE 不为 nil（命令可执行）
	cmd := findServeCmd(t)
	if cmd == nil {
		t.Fatal("serve command not found")
	}

	if cmd.RunE == nil {
		t.Error("serve command RunE is nil, expected executable function")
	}
}
