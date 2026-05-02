package kernel

import (
	"testing"

	"github.com/rnixai/rnix/vfs"
)

// mockToolDescriptor implements vfs.ToolDescriptor for testing.
type mockToolDescriptor struct {
	defs []vfs.ToolDef
}

func (m *mockToolDescriptor) ToolDefs() []vfs.ToolDef {
	return m.defs
}

func TestBuildToolDefs_ShellAndFS(t *testing.T) {
	reg := vfs.NewDeviceRegistry()
	factory := func(subpath string, flags vfs.OpenFlag, workDir string) (vfs.VFSFile, error) {
		return nil, nil
	}

	shellDriver := &mockToolDescriptor{defs: []vfs.ToolDef{
		{Name: "shell", Description: "Run command"},
	}}
	fsDriver := &mockToolDescriptor{defs: []vfs.ToolDef{
		{Name: "read_file", Description: "Read"},
		{Name: "write_file", Description: "Write"},
		{Name: "list_dir", Description: "List"},
	}}

	_ = reg.RegisterWithDriver("/dev/shell", factory, shellDriver)
	_ = reg.RegisterWithDriver("/dev/fs", factory, fsDriver)

	defs, toolMap := buildToolDefs(reg, nil, true)

	if len(defs) != 4 {
		t.Fatalf("expected 4 tool defs, got %d", len(defs))
	}

	if _, ok := toolMap["shell"]; !ok {
		t.Fatal("expected 'shell' in toolMap")
	}
	if toolMap["shell"].VFSPath != "/dev/shell" {
		t.Fatalf("expected VFSPath '/dev/shell', got %q", toolMap["shell"].VFSPath)
	}
	if toolMap["read_file"].FSOperation != "read_file" {
		t.Fatalf("expected FSOperation 'read_file', got %q", toolMap["read_file"].FSOperation)
	}
}

func TestBuildToolDefs_AllowedDevicesFilter(t *testing.T) {
	reg := vfs.NewDeviceRegistry()
	factory := func(subpath string, flags vfs.OpenFlag, workDir string) (vfs.VFSFile, error) {
		return nil, nil
	}

	shellDriver := &mockToolDescriptor{defs: []vfs.ToolDef{
		{Name: "shell", Description: "Run command"},
	}}
	fsDriver := &mockToolDescriptor{defs: []vfs.ToolDef{
		{Name: "read_file", Description: "Read"},
	}}

	_ = reg.RegisterWithDriver("/dev/shell", factory, shellDriver)
	_ = reg.RegisterWithDriver("/dev/fs", factory, fsDriver)

	// Only allow /dev/shell
	defs, toolMap := buildToolDefs(reg, []string{"/dev/shell"}, true)

	if len(defs) != 1 {
		t.Fatalf("expected 1 tool def (shell only), got %d", len(defs))
	}
	if defs[0].Name != "shell" {
		t.Fatalf("expected tool name 'shell', got %q", defs[0].Name)
	}
	if _, ok := toolMap["read_file"]; ok {
		t.Fatal("read_file should not be in toolMap when only /dev/shell is allowed")
	}
}

func TestBuildToolDefs_NilAllowedDevices_SkipsLLMDevices(t *testing.T) {
	reg := vfs.NewDeviceRegistry()
	factory := func(subpath string, flags vfs.OpenFlag, workDir string) (vfs.VFSFile, error) {
		return nil, nil
	}

	shellDriver := &mockToolDescriptor{defs: []vfs.ToolDef{
		{Name: "shell", Description: "Run command"},
	}}
	llmDriver := &mockToolDescriptor{defs: []vfs.ToolDef{
		{Name: "llm_call", Description: "Call LLM"},
	}}

	_ = reg.RegisterWithDriver("/dev/shell", factory, shellDriver)
	_ = reg.RegisterWithDriver("/dev/llm/claude", factory, llmDriver)

	defs, _ := buildToolDefs(reg, nil, true)

	for _, d := range defs {
		if d.Name == "llm_call" {
			t.Fatal("LLM device tools should be skipped")
		}
	}
	if len(defs) != 1 {
		t.Fatalf("expected 1 tool def (shell only), got %d", len(defs))
	}
}

func TestBuildToolDefs_UnknownPath_SkipsSilently(t *testing.T) {
	reg := vfs.NewDeviceRegistry()

	// No drivers registered but allowedDevices references unknown paths
	defs, toolMap := buildToolDefs(reg, []string{"/dev/unknown", "/dev/mcp/server"}, true)

	if len(defs) != 0 {
		t.Fatalf("expected 0 tool defs, got %d", len(defs))
	}
	if len(toolMap) != 0 {
		t.Fatalf("expected empty toolMap, got %d entries", len(toolMap))
	}
}

// TestBuildToolDefs_SubpathRouting verifies that drivers multiplexing multiple
// tools onto distinct subpaths (e.g. /dev/intent dispatching /decompose,
// /confirm, /status, /execute) get their tool name → full-path mapping
// preserved in toolMap. Without subpath routing, all four intent tools were
// mapped to /dev/intent root and dispatched into handleDecompose, breaking
// confirm/execute/status calls with "intent is required".
func TestBuildToolDefs_SubpathRouting(t *testing.T) {
	reg := vfs.NewDeviceRegistry()
	factory := func(subpath string, flags vfs.OpenFlag, workDir string) (vfs.VFSFile, error) {
		return nil, nil
	}

	intentDriver := &mockToolDescriptor{defs: []vfs.ToolDef{
		{Name: "intent_decompose", Subpath: "/decompose"},
		{Name: "intent_status", Subpath: "/status"},
		{Name: "intent_confirm", Subpath: "/confirm"},
		{Name: "intent_execute", Subpath: "/execute"},
	}}
	shellDriver := &mockToolDescriptor{defs: []vfs.ToolDef{
		{Name: "shell"}, // empty Subpath → opens device root
	}}

	_ = reg.RegisterWithDriver("/dev/intent", factory, intentDriver)
	_ = reg.RegisterWithDriver("/dev/shell", factory, shellDriver)

	_, toolMap := buildToolDefs(reg, nil, true)

	wantPaths := map[string]string{
		"intent_decompose": "/dev/intent/decompose",
		"intent_status":    "/dev/intent/status",
		"intent_confirm":   "/dev/intent/confirm",
		"intent_execute":   "/dev/intent/execute",
		"shell":            "/dev/shell",
	}
	for name, want := range wantPaths {
		got, ok := toolMap[name]
		if !ok {
			t.Errorf("toolMap missing %q", name)
			continue
		}
		if got.VFSPath != want {
			t.Errorf("toolMap[%q].VFSPath = %q, want %q", name, got.VFSPath, want)
		}
	}
}

func TestMetaToolDefs(t *testing.T) {
	defs, metaMap := metaToolDefs(true, nil)

	expectedNames := map[string]bool{
		"complete": false, "spawn": false, "replan": false, "specialize": false, "plan": false,
	}
	for _, d := range defs {
		if _, ok := expectedNames[d.Name]; ok {
			expectedNames[d.Name] = true
		}
	}
	for name, found := range expectedNames {
		if !found {
			t.Fatalf("missing meta tool def: %q", name)
		}
	}

	if _, ok := metaMap["complete"]; !ok {
		t.Fatal("expected 'complete' in metaMap")
	}
	if metaMap["complete"].Action != ActionComplete {
		t.Fatalf("expected ActionComplete, got %v", metaMap["complete"].Action)
	}
}

func TestMetaToolDefs_PlanningDisabled(t *testing.T) {
	defs, metaMap := metaToolDefs(false, nil)

	for _, d := range defs {
		if d.Name == "plan" {
			t.Fatal("plan should not be included when planning is disabled")
		}
	}
	if _, ok := metaMap["plan"]; ok {
		t.Fatal("plan should not be in metaMap when planning is disabled")
	}
}

func TestMetaToolDefs_JSONSchemaComplete(t *testing.T) {
	defs, _ := metaToolDefs(true, nil)

	for _, d := range defs {
		if d.Parameters == nil {
			t.Fatalf("tool %q has nil Parameters", d.Name)
		}
		if d.Parameters["type"] != "object" {
			t.Fatalf("tool %q: expected parameters type 'object', got %v", d.Name, d.Parameters["type"])
		}
		if _, ok := d.Parameters["properties"]; !ok {
			t.Fatalf("tool %q: missing 'properties' in Parameters", d.Name)
		}
		if _, ok := d.Parameters["required"]; !ok {
			t.Fatalf("tool %q: missing 'required' in Parameters", d.Name)
		}
	}
}

func TestSystemEventMergesToolDefs(t *testing.T) {
	// Simulate kernel-initialized nativeToolDefs (VFS + meta tools)
	proc := &Process{}
	proc.nativeToolDefs = []vfs.ToolDef{
		{Name: "read_file", Description: "Read a file"},
		{Name: "write_file", Description: "Write a file"},
		{Name: "shell", Description: "Run command"},
		{Name: "complete", Description: "Finish task"},
		{Name: "spawn", Description: "Spawn child"},
	}

	// Simulate driver system event with tools: [LSP, mcp_auth, read_file]
	// (read_file is a duplicate that should be skipped)
	driverTools := []string{"LSP", "mcp__gateway__authenticate", "read_file"}

	// Apply the merge logic (mirrors observe.go system event handler)
	proc.mu.Lock()
	existing := make(map[string]bool, len(proc.nativeToolDefs))
	for _, td := range proc.nativeToolDefs {
		existing[td.Name] = true
	}
	for _, name := range driverTools {
		if !existing[name] {
			proc.nativeToolDefs = append(proc.nativeToolDefs, vfs.ToolDef{Name: name})
		}
	}
	proc.mu.Unlock()

	// Verify: all original tools preserved + new driver tools added (no duplicates)
	if len(proc.nativeToolDefs) != 7 {
		t.Fatalf("expected 7 tool defs (5 kernel + 2 new driver), got %d", len(proc.nativeToolDefs))
	}

	names := make(map[string]bool)
	for _, td := range proc.nativeToolDefs {
		if names[td.Name] {
			t.Fatalf("duplicate tool def: %q", td.Name)
		}
		names[td.Name] = true
	}

	// Original kernel tools must be preserved
	for _, required := range []string{"read_file", "write_file", "shell", "complete", "spawn"} {
		if !names[required] {
			t.Fatalf("kernel tool %q was lost after driver event merge", required)
		}
	}

	// New driver tools must be added
	for _, required := range []string{"LSP", "mcp__gateway__authenticate"} {
		if !names[required] {
			t.Fatalf("driver tool %q was not merged", required)
		}
	}
}

