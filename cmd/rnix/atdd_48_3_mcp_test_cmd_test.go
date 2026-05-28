package main

import (
	"testing"
)

// =============================================================================
// Story 48.3 AC2 — `rnix mcp test <name>` 一次性探针 CLI command
//
// **RED PHASE — all tests t.Skip until Story 48.3 Task 2 + Task 4 land.**
//
// Task 4 creates `mcpTestCmd` with Args=cobra.ExactArgs(1), RunE=runMCPTest:
//   - Resolves daemon via ipc.Dial(ipc.SocketPath())
//   - Daemon down → exit 1 + hint (different from `mcp list` graceful-empty
//     behavior — Story §决策 1)
//   - client.MCPTest(name) → 阶段输出 + summary
//   - --json mode → MCPTestResponse 序列化
//
// AC2 six scenarios:
//   _005 — happy 3 stages + tools=N
//   _006 — server not in mcp.yaml → "Available: ..." hint, exit 1
//   _007 — connect fails (bad binary) → stage[0]=FAILED, exit 1
//   _008 — initialize hangs → TIMEOUT, exit 1
//   _009 — --json mode shape
//   _010 — daemon down → hard fail exit 1 (NOT graceful)
//
// Compile-safety: tests use `rootCmd.Find` + `cmd.RunE` so the file
// compiles even when mcp/mcpTestCmd don't exist (cobra returns nil cmd +
// error). All concrete assertions are in the GREEN-phase sketch.
// =============================================================================

// -----------------------------------------------------------------------------
// _005: happy path — registered server, healthy mock → 3-stage success output
// -----------------------------------------------------------------------------
func TestATDD_48_3_005_McpTest_HappyPath(t *testing.T) {
	t.Skip("RED: 等待 Story 48.3 Task 2 + Task 4 — RunMCPProbe + mcpTestCmd")

	// GREEN-phase plan:
	//
	//   client, kern, _, _ := setupResumeIPCTest(t)  // (in main_test sibling helper)
	//   kern.SetMCPRegistry(map[string]vfs.MCPConfig{
	//       "playwright": {ServerName: "playwright", Command: "ok"},
	//   })
	//   kern.SetTransportFactory(<healthy fake — see kernel_mcp_test.go>)
	//   ipc.SocketPathOverride = <test socket>
	//
	//   var buf bytes.Buffer
	//   cmd, _, _ := rootCmd.Find([]string{"mcp", "test"})
	//   cmd.SetOut(&buf); cmd.SetErr(&buf)
	//   cmd.SetArgs([]string{"playwright"})
	//   if err := cmd.RunE(cmd, []string{"playwright"}); err != nil {
	//       t.Fatalf("RunE: %v", err)
	//   }
	//
	//   if exitCode != 0 { t.Errorf("exitCode=%d, want 0", exitCode) }
	//   out := buf.String()
	//   if !strings.Contains(out, "[1/3]") || !strings.Contains(out, "connect") {
	//       t.Errorf("missing stage 1/3 connect; got: %s", out)
	//   }
	//   if !strings.Contains(out, "OK") {
	//       t.Errorf("missing OK marker; got: %s", out)
	//   }
}

// -----------------------------------------------------------------------------
// _006: server name not in registry → exit 1 + available-list hint
// -----------------------------------------------------------------------------
func TestATDD_48_3_006_McpTest_ServerNotFound(t *testing.T) {
	t.Skip("RED: 等待 Story 48.3 Task 4.3 + Task 3 NOT_FOUND mapping")

	// GREEN-phase plan:
	//
	//   kern.SetMCPRegistry(map[string]vfs.MCPConfig{
	//       "playwright": {ServerName: "playwright", Command: "ok"},
	//       "deepwiki":   {ServerName: "deepwiki", Command: "ok"},
	//   })
	//
	//   exitCode = 0
	//   var buf bytes.Buffer
	//   cmd, _, _ := rootCmd.Find([]string{"mcp", "test"})
	//   cmd.SetOut(&buf); cmd.SetErr(&buf)
	//   cmd.RunE(cmd, []string{"playwriht"})  // typo
	//
	//   if exitCode != 1 { t.Errorf("exitCode=%d, want 1", exitCode) }
	//   out := buf.String()
	//   if !strings.Contains(out, "playwriht") {
	//       t.Errorf("output should echo bad name; got: %s", out)
	//   }
	//   if !strings.Contains(out, "Available") || !strings.Contains(out, "playwright") {
	//       t.Errorf("output should list available servers; got: %s", out)
	//   }
}

// -----------------------------------------------------------------------------
// _007: connect fails (bad command) → stage[0]=FAILED + exit 1
// -----------------------------------------------------------------------------
func TestATDD_48_3_007_McpTest_ConnectFail(t *testing.T) {
	t.Skip("RED: 等待 Story 48.3 Task 2.3 — connect failure short-circuit")

	// GREEN-phase plan:
	//
	//   kern.SetMCPRegistry(map[string]vfs.MCPConfig{
	//       "broken": {ServerName: "broken", Command: "nonexistent-binary"},
	//   })
	//   kern.SetTransportFactory(<fake returning connect error>)
	//
	//   exitCode = 0
	//   var buf bytes.Buffer
	//   cmd, _, _ := rootCmd.Find([]string{"mcp", "test"})
	//   cmd.SetOut(&buf); cmd.SetErr(&buf)
	//   cmd.RunE(cmd, []string{"broken"})
	//
	//   if exitCode != 1 { t.Errorf("exitCode=%d, want 1", exitCode) }
	//   out := buf.String()
	//   if !strings.Contains(out, "[1/3]") || !strings.Contains(out, "FAILED") {
	//       t.Errorf("expect stage[0]=FAILED; got: %s", out)
	//   }
	//   // stages 2/3 must NOT have run.
	//   if strings.Contains(out, "[2/3]") {
	//       t.Errorf("stage[1] should not run after connect failure; got: %s", out)
	//   }
}

// -----------------------------------------------------------------------------
// _008: initialize hangs past 10s → TIMEOUT line + exit 1
// -----------------------------------------------------------------------------
func TestATDD_48_3_008_McpTest_InitializeTimeout(t *testing.T) {
	t.Skip("RED: 等待 Story 48.3 Task 2.3 — ctx 10s budget + TIMEOUT label")

	// GREEN-phase plan:
	//   kern.SetTransportFactory(<hanging fake — see hangingMockTransport in
	//                              kernel/atdd_48_3_run_mcp_probe_test.go>)
	//   // 12s budget; daemon probe should timeout at ~10s.
	//   ...
	//   if !strings.Contains(out, "TIMEOUT") {
	//       t.Errorf("expect TIMEOUT label; got: %s", out)
	//   }
	//   if exitCode != 1 { t.Errorf("exitCode=%d, want 1", exitCode) }
	//   // Total wall-time ≤ 20s (Story §AC2 §"整个 test 命令总耗时 ≤ 20s").
	//   if elapsed > 20*time.Second { t.Errorf("took %v > 20s upper bound", elapsed) }
}

// -----------------------------------------------------------------------------
// _009: --json mode shape — MCPTestResponse-compatible JSON
// -----------------------------------------------------------------------------
func TestATDD_48_3_009_McpTest_JSONMode(t *testing.T) {
	t.Skip("RED: 等待 Story 48.3 Task 4.4 — renderMCPTestJSON")

	// GREEN-phase plan:
	//   oldJSON := flagJSON; flagJSON = true; defer func() { flagJSON = oldJSON }()
	//   ...
	//   var resp JSONResponse
	//   if err := json.Unmarshal(buf.Bytes(), &resp); err != nil {
	//       t.Fatalf("JSON parse: %v\n%s", err, buf.String())
	//   }
	//   // Data is daemon's MCPTestResponse; shape pinned by ipc test
	//   // (ipc/atdd_48_3_mcp_test_wire_test.go::_021). Here we just verify
	//   // CLI faithfully forwards it.
	//   m, ok := resp.Data.(map[string]any)
	//   if !ok { t.Fatalf("Data shape: %T", resp.Data) }
	//   if _, ok := m["server"]; !ok { t.Errorf("missing 'server' field") }
	//   if _, ok := m["stages"]; !ok { t.Errorf("missing 'stages' field") }
}

// -----------------------------------------------------------------------------
// _010: daemon down → hard fail exit 1 (NOT graceful empty)
// -----------------------------------------------------------------------------
func TestATDD_48_3_010_McpTest_DaemonDown(t *testing.T) {
	t.Skip("RED: 等待 Story 48.3 Task 4.3 — daemon-down hard fail (differs from list)")

	// GREEN-phase plan:
	//
	//   old := ipc.SocketPathOverride
	//   ipc.SocketPathOverride = "/tmp/rnix-test-nonexistent.sock"
	//   defer func() { ipc.SocketPathOverride = old }()
	//
	//   exitCode = 0
	//   var buf bytes.Buffer
	//   cmd, _, _ := rootCmd.Find([]string{"mcp", "test"})
	//   cmd.SetOut(&buf); cmd.SetErr(&buf)
	//   cmd.RunE(cmd, []string{"playwright"})
	//
	//   if exitCode != 1 {
	//       t.Errorf("exitCode=%d, want 1 (test requires daemon; differs from list)", exitCode)
	//   }
	//   out := buf.String()
	//   if !strings.Contains(out, "Daemon not running") {
	//       t.Errorf("output should mention daemon-down; got: %s", out)
	//   }
	//   // CRITICAL: test command must NOT EnsureDaemon. Verified by code-review
	//   // (no EnsureDaemon import in mcp.go) + post-test process check.
}
