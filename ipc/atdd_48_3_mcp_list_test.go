package ipc

import (
	"testing"
)

// =============================================================================
// Story 48.3 AC5 (IPC mcp_list wire) — daemon → CLI registry projection
//
// **RED PHASE — all tests t.Skip until Story 48.3 Task 3 lands.**
//
// Once Task 3 ships:
//   - ipc/protocol.go gains MethodMCPList constant + MCPMountWire +
//     MCPListResponse
//   - ipc/server.go switch routes MethodMCPList → s.handleMCPList
//   - ipc/server_mcp.go implements handleMCPList(conn) that reads
//     s.kern.ListMounts() + s.kern.MCPRegistry() and emits wires
//   - ipc/client.go gains (c *Client).MCPList() with nil-normalize
//     mirror of ListResumable (client.go:266-279)
//
// Compile-safety contract for this RED file:
//   - We use the literal Method("mcp_list") (no constant reference)
//   - We unmarshal payload bytes into a local wireRow/wireResp type
//     defined below (no reference to ipc.MCPMountWire which doesn't exist
//     yet)
//   - We bypass client.MCPList() until it lands; instead the Skip body
//     describes what to assert when the suite goes GREEN
//
// dev-story GREEN edits: in each test, remove t.Skip and replace local
// wireResp with ipc.MCPListResponse (or call client.MCPList()).
// =============================================================================

// localMCPMountWire mirrors the wire shape from Story §AC5 Task 3.2; defined
// locally so this RED file compiles before the symbol exists in protocol.go.
// dev-story Task 3.2 introduces ipc.MCPMountWire — at that point delete this
// type and switch tests to ipc.MCPMountWire / MCPListResponse.
type localMCPMountWire struct {
	Name        string `json:"name"`
	Path        string `json:"path"`
	Transport   string `json:"transport"`
	Status      string `json:"status"`
	Tools       int    `json:"tools"`
	Resources   int    `json:"resources"`
	LastCheckMs int64  `json:"last_check_ms,omitempty"`
}

type localMCPListResp struct {
	Mounts []localMCPMountWire `json:"mounts"`
}

// -----------------------------------------------------------------------------
// _020: empty mount registry → wires=[]  (NOT null) — AC5 nil-normalize
// -----------------------------------------------------------------------------
func TestATDD_48_3_020_IPC_McpList_Empty(t *testing.T) {
	t.Skip("RED: 等待 Story 48.3 Task 3 — MethodMCPList + handleMCPList + client.MCPList")

	// GREEN-phase plan (executable after Task 3 lands):
	//
	//   client, _, _, _ := setupResumeIPCTest(t)
	//   resp, err := client.MCPList()
	//   if err != nil { t.Fatalf("MCPList: %v", err) }
	//   if resp == nil { t.Fatal("response nil") }
	//   if resp.Mounts == nil {
	//       t.Error("Mounts must be non-nil slice ([]), not nil (Story §wire nil-normalize)")
	//   }
	//   if len(resp.Mounts) != 0 {
	//       t.Errorf("Mounts len=%d, want 0 on empty kernel", len(resp.Mounts))
	//   }
	//
	// Until then, this Skip documents the AC5 expectation so the test
	// suite annotates the gap for dev-story handoff.
	_ = localMCPListResp{}
}

// -----------------------------------------------------------------------------
// _020b: two mounts → wires reflect Path / Name / Status round-trip
// -----------------------------------------------------------------------------
func TestATDD_48_3_020b_IPC_McpList_TwoMounts(t *testing.T) {
	t.Skip("RED: 等待 Story 48.3 Task 3 + Task 1 — registry + mounts wired")

	// GREEN-phase plan:
	//
	// 1. setupResumeIPCTest gives us a *KernelImpl with MountManager (Story
	//    48.1 helper). We need to:
	//    a. Inject mcpRegistry via k.SetMCPRegistry(...)  (Task 1.4)
	//    b. Mount 2 paths via k.Mount("/mnt/mcp/1-playwright", cfg)  (Task 9.1)
	// 2. client.MCPList() returns 2 wires.
	// 3. Assert each wire has:
	//    - Name parsed from path (`<pid>-<server>` → `<server>`)
	//    - Path = mount path
	//    - Transport ∈ {"stdio"}
	//    - Status = "connected" (initial mount state)
	//    - Tools = 0 (Story keeps placeholder until 48.5)
	//    - Resources = 0
	//    - LastCheckMs = 0 (no probe yet)
	//
	// Sketch (do not execute until Task 3):
	//
	//   client, kern, _, _ := setupResumeIPCTest(t)
	//   // ... wire MountManager + Mount 2 paths ...
	//   resp, err := client.MCPList()
	//   if err != nil { t.Fatalf("MCPList: %v", err) }
	//   if len(resp.Mounts) != 2 {
	//       t.Fatalf("Mounts len=%d, want 2", len(resp.Mounts))
	//   }
	//   names := map[string]bool{}
	//   for _, m := range resp.Mounts {
	//       names[m.Name] = true
	//       if m.Status != "connected" {
	//           t.Errorf("mount %q Status=%q, want \"connected\"", m.Name, m.Status)
	//       }
	//       if m.Transport != "stdio" {
	//           t.Errorf("mount %q Transport=%q, want \"stdio\"", m.Name, m.Transport)
	//       }
	//       if m.Tools != 0 || m.Resources != 0 {
	//           t.Errorf("mount %q Tools/Resources non-zero pre-48.5", m.Name)
	//       }
	//   }
	//   if !names["playwright"] || !names["deepwiki"] {
	//       t.Errorf("expected mounts {playwright, deepwiki}, got %v", names)
	//   }
	_ = kern_unused()
}

// kern_unused keeps the file in-package importing nothing exotic. dev-story
// can delete once setupResumeIPCTest is exercised in the GREEN body.
func kern_unused() bool { return true }
