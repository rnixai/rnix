package kernel

import (
	"strings"
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

func TestGenerateToolProtocol(t *testing.T) {
	reg := vfs.NewDeviceRegistry()
	factory := func(subpath string, flags vfs.OpenFlag, workDir string) (vfs.VFSFile, error) {
		return nil, nil
	}
	shellDriver := &mockToolDescriptor{defs: []vfs.ToolDef{
		{Name: "shell", Description: "Run command", Parameters: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"command": map[string]any{"type": "string"},
			},
		}},
	}}
	_ = reg.RegisterWithDriver("/dev/shell", factory, shellDriver)

	defs, toolMap := buildToolDefs(reg, nil, true)
	metaDefs, metaMap := metaToolDefs(true, nil)

	protocol := generateToolProtocol(defs, toolMap, metaDefs, metaMap, true)

	if !strings.Contains(protocol, "Action Protocol") {
		t.Fatal("expected protocol to contain 'Action Protocol'")
	}
	if !strings.Contains(protocol, "/dev/shell") {
		t.Fatal("expected protocol to contain '/dev/shell'")
	}
	if !strings.Contains(protocol, "complete") {
		t.Fatal("expected protocol to contain 'complete' action")
	}
}

func TestGenerateToolProtocol_PlanningConditional(t *testing.T) {
	metaDefs, metaMap := metaToolDefs(false, nil)
	protocol := generateToolProtocol(nil, nil, metaDefs, metaMap, false)

	if strings.Contains(protocol, "Plan —") {
		t.Fatal("expected no plan section when planning disabled")
	}

	metaDefsEnabled, metaMapEnabled := metaToolDefs(true, nil)
	protocolEnabled := generateToolProtocol(nil, nil, metaDefsEnabled, metaMapEnabled, true)

	if !strings.Contains(protocolEnabled, "Plan —") {
		t.Fatal("expected plan section when planning enabled")
	}
}
