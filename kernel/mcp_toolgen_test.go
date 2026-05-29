//go:build unix

package kernel

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// 路线 B — 将 MCP 工具暴露为 native ToolDef 的测试。
//
// 覆盖 spec-mcp-native-tooldefs 的 I/O & Edge-Case Matrix：
//   - 正常发现 / 带参与无参调用 / tools/list 失败降级 / 名称冲突/超长 / 原始名 VFSPath。
// 复用 atdd_48_5_mcp_events_test.go 的 setupMCPEventKernel（挂载 transport + 设 proc.MCPMounts）。

// toolgenMockTransport implements the full vfs.MCPTransport surface with a
// configurable tools/list response, so buildMCPToolDefs can be exercised against
// a known tool inventory. callErr (when set) fails every Call (tools/list error
//降级路径).
type toolgenMockTransport struct {
	toolsList json.RawMessage // returned for "tools/list" when callErr == nil
	callErr   error
}

func (m *toolgenMockTransport) Connect(_ context.Context) error { return nil }
func (m *toolgenMockTransport) Call(_ context.Context, method string, _ json.RawMessage) (json.RawMessage, error) {
	if m.callErr != nil {
		return nil, m.callErr
	}
	switch method {
	case "tools/list":
		if m.toolsList != nil {
			return m.toolsList, nil
		}
		return json.RawMessage(`{"tools":[]}`), nil
	case "tools/call":
		return json.RawMessage(`{"content":[{"type":"text","text":"ok"}]}`), nil
	default:
		return json.RawMessage(`{}`), nil
	}
}
func (m *toolgenMockTransport) Close() error                 { return nil }
func (m *toolgenMockTransport) Ping(_ context.Context) error { return nil }
func (m *toolgenMockTransport) Status() vfs.MCPStatus        { return vfs.MCPStatusConnected }
func (m *toolgenMockTransport) Alive() bool                  { return m.callErr == nil }
func (m *toolgenMockTransport) ToolCount() int               { return 0 }
func (m *toolgenMockTransport) ResourceCount() int           { return 0 }
func (m *toolgenMockTransport) LastCheck() time.Time         { return time.Time{} }
func (m *toolgenMockTransport) ReconnectCount() int          { return 0 }
func (m *toolgenMockTransport) StderrTail() []string         { return nil }

const twoToolsList = `{"tools":[
  {"name":"browser_navigate","description":"Navigate to a URL",
   "inputSchema":{"type":"object","properties":{"url":{"type":"string"}},"required":["url"]}},
  {"name":"browser_snapshot","description":"Capture a snapshot"}
]}`

// --- sanitizeMCPToolName (pure) ----------------------------------------------

func TestSanitizeMCPToolName(t *testing.T) {
	t.Run("legal name passes through unchanged", func(t *testing.T) {
		seen := map[string]bool{}
		got := sanitizeMCPToolName("mcp__playwright__browser_navigate", seen)
		if got != "mcp__playwright__browser_navigate" {
			t.Fatalf("got %q, want unchanged", got)
		}
	})

	t.Run("illegal chars replaced with underscore", func(t *testing.T) {
		seen := map[string]bool{}
		got := sanitizeMCPToolName("mcp__srv__foo/bar baz.qux", seen)
		if strings.ContainsAny(got, "/ .") {
			t.Fatalf("got %q still contains illegal chars", got)
		}
		if got != "mcp__srv__foo_bar_baz_qux" {
			t.Fatalf("got %q, want mcp__srv__foo_bar_baz_qux", got)
		}
	})

	t.Run("over-length truncated within cap with hash suffix", func(t *testing.T) {
		seen := map[string]bool{}
		raw := "mcp__playwright__" + strings.Repeat("x", 100)
		got := sanitizeMCPToolName(raw, seen)
		if len(got) > mcpToolNameMaxLen {
			t.Fatalf("got len %d (%q), want <= %d", len(got), got, mcpToolNameMaxLen)
		}
	})

	t.Run("collisions are disambiguated to unique names", func(t *testing.T) {
		seen := map[string]bool{}
		// Two distinct raws that sanitize to the same string.
		a := sanitizeMCPToolName("mcp__srv__tool/x", seen)
		b := sanitizeMCPToolName("mcp__srv__tool x", seen)
		if a == b {
			t.Fatalf("collision not disambiguated: both %q", a)
		}
	})

	t.Run("all-illegal raw does not yield empty name", func(t *testing.T) {
		seen := map[string]bool{}
		got := sanitizeMCPToolName("///", seen)
		if got == "" {
			t.Fatal("got empty name")
		}
	})
}

// --- buildMCPToolDefs ---------------------------------------------------------

func TestBuildMCPToolDefs_Discovery(t *testing.T) {
	tr := &toolgenMockTransport{toolsList: json.RawMessage(twoToolsList)}
	k, proc, _, path := setupMCPEventKernel(t, "playwright", tr)

	defs, toolMap := k.buildMCPToolDefs(proc)
	if len(defs) != 2 {
		t.Fatalf("got %d defs, want 2: %+v", len(defs), defs)
	}

	byName := map[string]vfs.ToolDef{}
	for _, d := range defs {
		byName[d.Name] = d
	}

	nav, ok := byName["mcp__playwright__browser_navigate"]
	if !ok {
		t.Fatalf("missing mcp__playwright__browser_navigate; got %v", byName)
	}
	if nav.MaxResultTokens != mcpToolMaxResultTokens {
		t.Errorf("MaxResultTokens = %d, want %d", nav.MaxResultTokens, mcpToolMaxResultTokens)
	}
	// inputSchema flows straight into Parameters.
	props, _ := nav.Parameters["properties"].(map[string]any)
	if _, has := props["url"]; !has {
		t.Errorf("browser_navigate Parameters missing url property: %v", nav.Parameters)
	}
	// toolMap VFSPath uses the ORIGINAL tool name, not the prefixed visible name.
	if m := toolMap["mcp__playwright__browser_navigate"]; m.VFSPath != path+"/tools/browser_navigate" || m.Type != "vfs" {
		t.Errorf("toolMap entry = %+v, want VFSPath %s type vfs", m, path+"/tools/browser_navigate")
	}

	// A tool with no inputSchema still gets a valid object schema.
	snap := byName["mcp__playwright__browser_snapshot"]
	if snap.Parameters["type"] != "object" {
		t.Errorf("browser_snapshot Parameters = %v, want a default object schema", snap.Parameters)
	}
	if m := toolMap["mcp__playwright__browser_snapshot"]; m.VFSPath != path+"/tools/browser_snapshot" {
		t.Errorf("snapshot VFSPath = %s, want original name", m.VFSPath)
	}
}

func TestBuildMCPToolDefs_NoTransport_Skips(t *testing.T) {
	tr := &toolgenMockTransport{toolsList: json.RawMessage(twoToolsList)}
	k, proc, _, _ := setupMCPEventKernel(t, "playwright", tr)
	// Point at a mount that has no live transport — must skip without blocking.
	proc.MCPMounts = []string{"/mnt/mcp/999-ghost"}

	defs, toolMap := k.buildMCPToolDefs(proc)
	if defs != nil || toolMap != nil {
		t.Fatalf("expected nil defs/map for missing transport, got %d defs", len(defs))
	}
}

func TestBuildMCPToolDefs_ToolsListError_Skips(t *testing.T) {
	tr := &toolgenMockTransport{callErr: context.DeadlineExceeded}
	k, proc, _, _ := setupMCPEventKernel(t, "playwright", tr)

	defs, _ := k.buildMCPToolDefs(proc)
	if defs != nil {
		t.Fatalf("expected nil defs when tools/list fails, got %d", len(defs))
	}
}

// --- attachMCPToolDefs --------------------------------------------------------

func TestAttachMCPToolDefs_AppendsPreservingBase(t *testing.T) {
	tr := &toolgenMockTransport{toolsList: json.RawMessage(twoToolsList)}
	k, proc, _, _ := setupMCPEventKernel(t, "playwright", tr)

	// Simulate the base/meta tool set already assembled before mounting.
	proc.nativeToolDefs = []vfs.ToolDef{{Name: "Bash"}, {Name: "complete"}}
	proc.toolMap = map[string]toolMapping{
		"Bash":     {Type: "vfs", VFSPath: "/dev/shell"},
		"complete": {Type: "meta"},
	}

	k.attachMCPToolDefs(proc)

	names := map[string]bool{}
	for _, d := range proc.nativeToolDefs {
		names[d.Name] = true
	}
	for _, want := range []string{"Bash", "complete", "mcp__playwright__browser_navigate", "mcp__playwright__browser_snapshot"} {
		if !names[want] {
			t.Errorf("nativeToolDefs missing %q after attach; got %v", want, names)
		}
	}
	if _, ok := proc.toolMap["mcp__playwright__browser_navigate"]; !ok {
		t.Error("toolMap missing MCP entry after attach")
	}
	if proc.toolMap["Bash"].VFSPath != "/dev/shell" {
		t.Error("attach clobbered base toolMap entry")
	}
}

// --- end-to-end execution through executeVFSTool ------------------------------

func TestExecuteVFSTool_MCP_NoArgAndArgs(t *testing.T) {
	tr := &toolgenMockTransport{toolsList: json.RawMessage(twoToolsList)}
	k, proc, _, path := setupMCPEventKernel(t, "playwright", tr)
	k.attachMCPToolDefs(proc)

	// No-argument MCP tool: empty Input must still trigger the tools/call.
	// Without the route-B fix the default branch opens O_RDONLY, skips Write,
	// and mcpFile.Read errors with "write a request first".
	t.Run("no-arg tool succeeds", func(t *testing.T) {
		tc := llmToolCall{Name: "mcp__playwright__browser_snapshot"}
		mapping := proc.toolMap["mcp__playwright__browser_snapshot"]
		if mapping.VFSPath != path+"/tools/browser_snapshot" {
			t.Fatalf("mapping VFSPath = %s", mapping.VFSPath)
		}
		out, err := k.executeVFSTool(proc, tc, mapping)
		if err != nil {
			t.Fatalf("no-arg MCP tool errored: %v", err)
		}
		if !strings.Contains(out, "ok") {
			t.Errorf("result = %q, want tools/call payload", out)
		}
	})

	t.Run("tool with args succeeds", func(t *testing.T) {
		tc := llmToolCall{
			Name:  "mcp__playwright__browser_navigate",
			Input: map[string]any{"url": "https://example.com"},
		}
		mapping := proc.toolMap["mcp__playwright__browser_navigate"]
		out, err := k.executeVFSTool(proc, tc, mapping)
		if err != nil {
			t.Fatalf("arg MCP tool errored: %v", err)
		}
		if !strings.Contains(out, "ok") {
			t.Errorf("result = %q, want tools/call payload", out)
		}
	})
}

// --- resume path (AC4): reattachMCPMounts must attach MCP native ToolDefs -----

// TestReattachMCPMounts_AttachesNativeToolDefs locks AC4: after a resume
// reattaches MCP transports, the resumed process's tool set must regain its
// mcp__<server>__<tool> defs — same as a fresh spawn. Exercises the real
// reattachMCPMounts → attachMCPToolDefs hook (covers resume.go + load_suspended.go,
// both of which call reattachMCPMounts after the base tool set is rebuilt).
func TestReattachMCPMounts_AttachesNativeToolDefs(t *testing.T) {
	transports := map[string]*mockMCPTransport{"playwright": {}}
	k, _, _ := setupResumeKernelWithMockMCP(t, transports)

	proc := NewProcess(0, "resume mcp toolgen", nil)
	if err := proc.Start(); err != nil {
		t.Fatalf("proc.Start: %v", err)
	}
	// Simulate the base tool set already rebuilt by rehydrateRuntimeStateFromDisk
	// (which runs BEFORE reattachMCPMounts on both resume paths).
	proc.nativeToolDefs = []vfs.ToolDef{{Name: "Bash"}}
	proc.toolMap = map[string]toolMapping{"Bash": {Type: "vfs", VFSPath: "/dev/shell"}}

	mountPath := fmt.Sprintf("/mnt/mcp/%d-playwright", proc.PID)
	info := vfs.ProcInfo{
		State: types.StateSuspended,
		MCPMounts: []vfs.MCPMountSnapshot{
			{Path: mountPath, Config: vfs.MCPConfig{ServerName: "playwright", Command: "/bin/echo", TransportType: "stdio"}},
		},
	}

	if n := k.reattachMCPMounts(proc, info); n != 1 {
		t.Fatalf("reattachMCPMounts mounted %d, want 1", n)
	}

	names := map[string]bool{}
	for _, d := range proc.nativeToolDefs {
		names[d.Name] = true
	}
	// The shared mockMCPTransport's tools/list returns a single "echo" tool.
	if !names["mcp__playwright__echo"] {
		t.Errorf("after resume reattach, nativeToolDefs missing mcp__playwright__echo; got %v", names)
	}
	if !names["Bash"] {
		t.Error("base tool lost across reattach attach")
	}
	if m, ok := proc.toolMap["mcp__playwright__echo"]; !ok || m.VFSPath != mountPath+"/tools/echo" {
		t.Errorf("toolMap[mcp__playwright__echo] = %+v ok=%v, want VFSPath %s", m, ok, mountPath+"/tools/echo")
	}
}
