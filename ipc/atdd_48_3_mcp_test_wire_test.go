package ipc

import (
	"testing"
)

// =============================================================================
// Story 48.3 AC5 (IPC mcp_test wire) — daemon-side probe IPC contract
//
// **RED PHASE — all tests t.Skip until Story 48.3 Task 3 lands.**
//
// Wire contract (Story §AC5 Task 3.2):
//   MCPTestRequest  { Name string }
//   MCPTestResponse { Server string, OK bool, Stages []MCPTestStageWire,
//                     Tools int, Resources int, Prompts int,
//                     ServerInfo string }
//   MCPTestStageWire { Name, OK, DurationMs, Error }
//
// Server-side routing (Story §AC5 Task 3.3 + 3.4):
//   - MethodMCPTest → s.handleMCPTest(conn, payload)
//   - handleMCPTest looks up Name in kernel.MCPRegistry; not-found returns
//     ErrorPayload{Code: "NOT_FOUND"}
//   - Empty Name returns ErrorPayload{Code: "INVALID"} (Story §AC5 "empty name")
//   - Successful path: calls kern.RunMCPProbe → marshals MCPProbeResult →
//     wraps in MCPTestResponse → writeResponse OK=true (even if probe failed —
//     business failure stays in payload, not IPC layer; Story §易错点 4)
//
// Compile-safety: this file uses local wire types and the literal
// Method("mcp_test"); deletes them after Task 3.
// =============================================================================

// localMCPTestReq / localMCPTestResp / localMCPTestStage mirror Story §3.2;
// keep until ipc.MCPTestRequest etc. lands.
type localMCPTestReq struct {
	Name string `json:"name"`
}

type localMCPTestStage struct {
	Name       string `json:"name"`
	OK         bool   `json:"ok"`
	DurationMs int64  `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
}

type localMCPTestResp struct {
	Server     string              `json:"server"`
	OK         bool                `json:"ok"`
	Stages     []localMCPTestStage `json:"stages"`
	Tools      int                 `json:"tools"`
	Resources  int                 `json:"resources"`
	Prompts    int                 `json:"prompts"`
	ServerInfo string              `json:"server_info,omitempty"`
}

// -----------------------------------------------------------------------------
// _021: happy path — registered server, healthy mock transport → all stages OK
// -----------------------------------------------------------------------------
func TestATDD_48_3_021_IPC_McpTest_HappyPath(t *testing.T) {
	t.Skip("RED: 等待 Story 48.3 Task 3.4 — handleMCPTest + client.MCPTest")

	// GREEN-phase plan:
	//
	//   client, kern, _, _ := setupResumeIPCTest(t)
	//   kern.SetMCPRegistry(map[string]vfs.MCPConfig{
	//       "playwright": {ServerName: "playwright", Command: "ok"},
	//   })
	//   kern.SetTransportFactory(<healthy fake>)
	//
	//   resp, err := client.MCPTest("playwright")
	//   if err != nil { t.Fatalf("MCPTest: %v", err) }
	//   if !resp.OK { t.Fatalf("resp.OK=false, want true; stages=%+v", resp.Stages) }
	//   if resp.Server != "playwright" { t.Errorf("Server=%q, want playwright", resp.Server) }
	//   if len(resp.Stages) < 4 {
	//       t.Errorf("stages=%d, want ≥ 4 (connect/tools/resources/prompts)", len(resp.Stages))
	//   }
	//   for _, s := range resp.Stages {
	//       if !s.OK { t.Errorf("stage %q OK=false; healthy mock should not fail any", s.Name) }
	//       if s.DurationMs <= 0 { t.Errorf("stage %q DurationMs=%d, want > 0", s.Name, s.DurationMs) }
	//   }
	_ = localMCPTestResp{}
}

// -----------------------------------------------------------------------------
// _021b: name not in registry → NOT_FOUND error payload, OK=false
// -----------------------------------------------------------------------------
func TestATDD_48_3_021b_IPC_McpTest_NotFound(t *testing.T) {
	t.Skip("RED: 等待 Story 48.3 Task 3.4 — handleMCPTest lookup + NOT_FOUND")

	// GREEN-phase plan:
	//
	//   client, kern, _, _ := setupResumeIPCTest(t)
	//   kern.SetMCPRegistry(map[string]vfs.MCPConfig{
	//       "playwright": {ServerName: "playwright", Command: "ok"},
	//   })
	//
	//   resp, err := client.MCPTest("playwriht")  // typo
	//   if err == nil {
	//       t.Errorf("MCPTest with missing name should return error; got resp=%+v", resp)
	//   }
	//   // The error must carry code NOT_FOUND so cmd-side can map to
	//   // exitCode=1 + Available list. Story §AC2 §"server \"playwriht\" not found".
	//   // Concrete shape: ipc.Client.call wraps ErrorPayload in error; assert
	//   // err.Error() contains "NOT_FOUND" or "not found".
	_ = localMCPTestReq{}
}

// -----------------------------------------------------------------------------
// _021c: empty name → INVALID error (Story §AC5 "name 字段空字符串")
// -----------------------------------------------------------------------------
func TestATDD_48_3_021c_IPC_McpTest_InvalidName(t *testing.T) {
	t.Skip("RED: 等待 Story 48.3 Task 3.4 — handleMCPTest input validation")

	// GREEN-phase plan:
	//
	//   client, _, _, _ := setupResumeIPCTest(t)
	//   _, err := client.MCPTest("")
	//   if err == nil { t.Fatal("empty name should error; got nil") }
	//   if !strings.Contains(err.Error(), "INVALID") &&
	//      !strings.Contains(err.Error(), "name required") {
	//       t.Errorf("err=%q; want INVALID/name required message", err)
	//   }
}
