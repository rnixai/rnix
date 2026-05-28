package ipc

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/rnixai/rnix/vfs"
)

// =============================================================================
// Story 48.3 AC5 (IPC mcp_test wire) — daemon-side probe IPC contract
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
//   - Empty Name returns ErrorPayload{Code: "INVALID"}
//   - Successful path: calls kern.RunMCPProbe → marshals MCPProbeResult →
//     wraps in MCPTestResponse → writeResponse OK=true (even if probe failed —
//     business failure stays in payload, not IPC layer; Story §易错点 4)
// =============================================================================

// -----------------------------------------------------------------------------
// _021: happy path — registered server, healthy mock transport → all stages OK
// -----------------------------------------------------------------------------
func TestATDD_48_3_021_IPC_McpTest_HappyPath(t *testing.T) {
	client, kern, _, _ := setupResumeIPCTest(t)

	kern.SetMCPRegistry(map[string]vfs.MCPConfig{
		"playwright": {ServerName: "playwright", Command: "ok"},
	})
	kern.SetTransportFactory(func(_ vfs.MCPConfig) (vfs.MCPTransport, error) {
		return &mcpTestStubTransport{
			toolsResp:     json.RawMessage(`{"tools":[{"name":"echo"},{"name":"goto"}]}`),
			resourcesResp: json.RawMessage(`{"resources":[{"uri":"page://current"}]}`),
			promptsResp:   json.RawMessage(`{"prompts":[]}`),
		}, nil
	})

	resp, err := client.MCPTest("playwright")
	if err != nil {
		t.Fatalf("MCPTest: %v", err)
	}
	if !resp.OK {
		t.Fatalf("resp.OK=false, want true; stages=%+v", resp.Stages)
	}
	if resp.Server != "playwright" {
		t.Errorf("Server=%q, want playwright", resp.Server)
	}
	if len(resp.Stages) < 4 {
		t.Errorf("stages=%d, want ≥ 4 (connect/tools/resources/prompts)", len(resp.Stages))
	}
	if resp.Tools != 2 {
		t.Errorf("Tools=%d, want 2", resp.Tools)
	}
	if resp.Resources != 1 {
		t.Errorf("Resources=%d, want 1", resp.Resources)
	}
}

// -----------------------------------------------------------------------------
// _021b: name not in registry → NOT_FOUND error payload
// -----------------------------------------------------------------------------
func TestATDD_48_3_021b_IPC_McpTest_NotFound(t *testing.T) {
	client, kern, _, _ := setupResumeIPCTest(t)
	kern.SetMCPRegistry(map[string]vfs.MCPConfig{
		"playwright": {ServerName: "playwright", Command: "ok"},
	})
	kern.SetTransportFactory(func(_ vfs.MCPConfig) (vfs.MCPTransport, error) {
		return &mcpTestStubTransport{}, nil
	})

	_, err := client.MCPTest("playwriht")
	if err == nil {
		t.Fatal("MCPTest with missing name should return error; got nil")
	}
	if !strings.Contains(err.Error(), "NOT_FOUND") && !strings.Contains(err.Error(), "not found") {
		t.Errorf("err=%q; want NOT_FOUND/not found message", err)
	}
}

// -----------------------------------------------------------------------------
// _021c: empty name → INVALID error
// -----------------------------------------------------------------------------
func TestATDD_48_3_021c_IPC_McpTest_InvalidName(t *testing.T) {
	client, _, _, _ := setupResumeIPCTest(t)

	_, err := client.MCPTest("")
	if err == nil {
		t.Fatal("empty name should error; got nil")
	}
	if !strings.Contains(err.Error(), "INVALID") && !strings.Contains(err.Error(), "name required") {
		t.Errorf("err=%q; want INVALID/name required message", err)
	}
}

// -----------------------------------------------------------------------------
// _021d: connect failure → IPC OK=true, payload OK=false, stage[0] failed
// -----------------------------------------------------------------------------
func TestATDD_48_3_021d_IPC_McpTest_ConnectFail_ReturnsPayload(t *testing.T) {
	client, kern, _, _ := setupResumeIPCTest(t)
	kern.SetMCPRegistry(map[string]vfs.MCPConfig{
		"broken": {ServerName: "broken", Command: "nonexistent"},
	})
	kern.SetTransportFactory(func(_ vfs.MCPConfig) (vfs.MCPTransport, error) {
		return &mcpTestStubTransport{connectErr: errors.New("exec: not found")}, nil
	})

	// Story §易错点 4: probe business failure does NOT escalate to IPC error.
	resp, err := client.MCPTest("broken")
	if err != nil {
		t.Fatalf("MCPTest returned IPC error for probe business failure: %v", err)
	}
	if resp.OK {
		t.Error("resp.OK=true after connect failure; want false")
	}
	if len(resp.Stages) == 0 || resp.Stages[0].OK {
		t.Errorf("stage[0] should record connect failure; stages=%+v", resp.Stages)
	}
	if resp.Stages[0].Error == "" {
		t.Error("stage[0].Error empty after connect failure")
	}
}

// mcpTestStubTransport satisfies vfs.MCPTransport with configurable Connect
// error + per-method Call payloads. Independent of probeMockTransport in the
// kernel package to keep the ipc test package self-contained.
type mcpTestStubTransport struct {
	connectErr    error
	toolsResp     json.RawMessage
	resourcesResp json.RawMessage
	promptsResp   json.RawMessage
}

func (s *mcpTestStubTransport) Connect(_ context.Context) error { return s.connectErr }

func (s *mcpTestStubTransport) Call(_ context.Context, method string, _ json.RawMessage) (json.RawMessage, error) {
	switch method {
	case "tools/list":
		if s.toolsResp == nil {
			return json.RawMessage(`{"tools":[]}`), nil
		}
		return s.toolsResp, nil
	case "resources/list":
		if s.resourcesResp == nil {
			return json.RawMessage(`{"resources":[]}`), nil
		}
		return s.resourcesResp, nil
	case "prompts/list":
		if s.promptsResp == nil {
			return json.RawMessage(`{"prompts":[]}`), nil
		}
		return s.promptsResp, nil
	}
	return json.RawMessage(`{}`), nil
}

func (s *mcpTestStubTransport) Close() error                 { return nil }
func (s *mcpTestStubTransport) Ping(_ context.Context) error { return nil }

// --- Story 48.5 health/status surface (no-op defaults for legacy stubs) ---
func (s *mcpTestStubTransport) Status() vfs.MCPStatus { return vfs.MCPStatusConnected }
func (s *mcpTestStubTransport) Alive() bool           { return true }
func (s *mcpTestStubTransport) ToolCount() int        { return 0 }
func (s *mcpTestStubTransport) ResourceCount() int    { return 0 }
func (s *mcpTestStubTransport) LastCheck() time.Time  { return time.Time{} }
func (s *mcpTestStubTransport) ReconnectCount() int   { return 0 }
func (s *mcpTestStubTransport) StderrTail() []string  { return nil }
