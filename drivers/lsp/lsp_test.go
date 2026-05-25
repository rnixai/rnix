package lsp

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// --- ToolDef metadata tests (Task 4.2) ---

func TestLspDriver_ToolDefs_Metadata(t *testing.T) {
	d := NewDriverWithCommand("gopls")
	defs := d.ToolDefs()

	if len(defs) != 1 {
		t.Fatalf("expected 1 ToolDef, got %d", len(defs))
	}

	def := defs[0]
	if def.Name != "LSP" {
		t.Errorf("expected name 'LSP', got %q", def.Name)
	}
	if !def.IsReadOnly {
		t.Error("LSP should be ReadOnly")
	}
	if !def.IsConcurrencySafe {
		t.Error("lsp should be ConcurrencySafe")
	}
	if !def.ShouldDefer {
		t.Error("lsp should have ShouldDefer=true")
	}
	if def.SearchHint != "lsp code definition references symbol" {
		t.Errorf("unexpected SearchHint: %q", def.SearchHint)
	}
	if def.MaxResultTokens != 100000 {
		t.Errorf("expected MaxResultTokens=100000, got %d", def.MaxResultTokens)
	}
	if def.IsDestructive {
		t.Error("lsp should NOT be destructive")
	}

	// Verify parameters include operation enum with all 9 operations
	params, ok := def.Parameters["properties"].(map[string]any)
	if !ok {
		t.Fatal("expected properties in parameters")
	}
	opProp, ok := params["operation"].(map[string]any)
	if !ok {
		t.Fatal("expected 'operation' property")
	}
	enumVals, ok := opProp["enum"].([]string)
	if !ok {
		t.Fatal("expected enum in operation")
	}
	if len(enumVals) != 9 {
		t.Errorf("expected 9 operation enum values, got %d", len(enumVals))
	}

	expectedOps := []string{
		"goToDefinition", "findReferences", "hover",
		"documentSymbol", "workspaceSymbol",
		"goToImplementation", "prepareCallHierarchy",
		"incomingCalls", "outgoingCalls",
	}
	for i, expected := range expectedOps {
		if i < len(enumVals) && enumVals[i] != expected {
			t.Errorf("enum[%d]: expected %q, got %q", i, expected, enumVals[i])
		}
	}
}

func TestLspDriver_ToolDescriptorInterface(t *testing.T) {
	var _ vfs.ToolDescriptor = (*LspDriver)(nil)
}

// --- LspFile lifecycle tests ---

func TestLspFile_Lifecycle(t *testing.T) {
	d := NewDriverWithCommand("gopls")
	f := &LspFile{driver: d, devicePath: "/dev/lsp", workDir: "/tmp"}

	// Read before Write
	_, err := f.Read(100)
	if err == nil {
		t.Error("expected error reading before write")
	}

	// Close
	if err := f.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Double close
	if err := f.Close(); err == nil {
		t.Error("expected error on double close")
	}

	// Write after close
	if err := f.Write(context.Background(), []byte(`{"operation":"hover"}`)); err == nil {
		t.Error("expected error writing to closed file")
	}

	// Read after close
	if _, err := f.Read(100); err == nil {
		t.Error("expected error reading from closed file")
	}
}

func TestLspFile_Stat(t *testing.T) {
	d := NewDriverWithCommand("gopls")
	f := &LspFile{driver: d, devicePath: "/dev/lsp", workDir: "/tmp"}

	stat, err := f.Stat()
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if !stat.IsDevice {
		t.Error("expected IsDevice=true")
	}
	if stat.DevicePath != "/dev/lsp" {
		t.Errorf("expected DevicePath='/dev/lsp', got %q", stat.DevicePath)
	}
}

func TestLspFile_InvalidJSON(t *testing.T) {
	d := NewDriverWithCommand("gopls")
	f := &LspFile{driver: d, devicePath: "/dev/lsp", workDir: "/tmp"}

	err := f.Write(context.Background(), []byte(`not json`))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestLspFile_MissingOperation(t *testing.T) {
	d := NewDriverWithCommand("gopls")
	f := &LspFile{driver: d, devicePath: "/dev/lsp", workDir: "/tmp"}

	err := f.Write(context.Background(), []byte(`{}`))
	if err == nil {
		t.Error("expected error when operation is missing")
	}
}

func TestLspFile_UnknownOperation(t *testing.T) {
	d := NewDriverWithCommand("gopls")
	f := &LspFile{driver: d, devicePath: "/dev/lsp", workDir: "/tmp"}

	err := f.Write(context.Background(), []byte(`{"operation":"nonexistent"}`))
	if err == nil {
		t.Error("expected error for unknown operation")
	}
	de, ok := err.(*types.DriverError)
	if !ok {
		t.Fatalf("expected *types.DriverError, got %T", err)
	}
	if de.Code != types.ErrInvalid {
		t.Errorf("expected ErrInvalid, got %v", de.Code)
	}
}

// --- buildParams tests ---

func TestBuildParams_PositionBased(t *testing.T) {
	d := NewDriverWithCommand("gopls")
	req := lspRequest{
		Operation: "goToDefinition",
		FilePath:  "main.go",
		Line:      10,
		Character: 5,
	}

	params, err := d.buildParams(req, "textDocument/definition", "/work")
	if err != nil {
		t.Fatalf("buildParams failed: %v", err)
	}

	m, ok := params.(map[string]any)
	if !ok {
		t.Fatal("expected map[string]any")
	}

	// Check URI
	td, ok := m["textDocument"].(map[string]any)
	if !ok {
		t.Fatal("expected textDocument in params")
	}
	uri, ok := td["uri"].(string)
	if !ok || uri != "file:///work/main.go" {
		t.Errorf("expected 'file:///work/main.go', got %q", uri)
	}

	// Check position (1-based → 0-based conversion)
	pos, ok := m["position"].(map[string]any)
	if !ok {
		t.Fatal("expected position in params")
	}
	if pos["line"] != 9 {
		t.Errorf("expected line=9 (0-based), got %v", pos["line"])
	}
	if pos["character"] != 4 {
		t.Errorf("expected character=4 (0-based), got %v", pos["character"])
	}
}

func TestBuildParams_DocumentSymbol_NoPosition(t *testing.T) {
	d := NewDriverWithCommand("gopls")
	req := lspRequest{
		Operation: "documentSymbol",
		FilePath:  "main.go",
	}

	params, err := d.buildParams(req, "textDocument/documentSymbol", "/work")
	if err != nil {
		t.Fatalf("buildParams failed: %v", err)
	}

	m := params.(map[string]any)
	if _, ok := m["position"]; ok {
		t.Error("documentSymbol should NOT have position")
	}
}

func TestBuildParams_WorkspaceSymbol(t *testing.T) {
	d := NewDriverWithCommand("gopls")
	req := lspRequest{
		Operation: "workspaceSymbol",
		Query:     "MyFunc",
	}

	params, err := d.buildParams(req, "workspace/symbol", "/work")
	if err != nil {
		t.Fatalf("buildParams failed: %v", err)
	}

	m := params.(map[string]any)
	if m["query"] != "MyFunc" {
		t.Errorf("expected query 'MyFunc', got %v", m["query"])
	}
}

func TestBuildParams_WorkspaceSymbol_MissingQuery(t *testing.T) {
	d := NewDriverWithCommand("gopls")
	req := lspRequest{
		Operation: "workspaceSymbol",
	}

	_, err := d.buildParams(req, "workspace/symbol", "/work")
	if err == nil {
		t.Error("expected error when query is missing for workspaceSymbol")
	}
}

func TestBuildParams_IncomingCalls_MissingItem(t *testing.T) {
	d := NewDriverWithCommand("gopls")
	req := lspRequest{
		Operation: "incomingCalls",
	}

	_, err := d.buildParams(req, "callHierarchy/incomingCalls", "/work")
	if err == nil {
		t.Error("expected error when item is missing for incomingCalls")
	}
}

func TestBuildParams_IncomingCalls_WithItem(t *testing.T) {
	d := NewDriverWithCommand("gopls")
	item := json.RawMessage(`{"name":"func1","uri":"file:///x.go"}`)
	req := lspRequest{
		Operation: "incomingCalls",
		Item:      item,
	}

	params, err := d.buildParams(req, "callHierarchy/incomingCalls", "/work")
	if err != nil {
		t.Fatalf("buildParams failed: %v", err)
	}

	m := params.(map[string]any)
	if m["item"] == nil {
		t.Error("expected item in params")
	}
}

func TestBuildParams_MissingFilePath(t *testing.T) {
	d := NewDriverWithCommand("gopls")
	req := lspRequest{
		Operation: "goToDefinition",
		Line:      1,
		Character: 1,
	}

	_, err := d.buildParams(req, "textDocument/definition", "/work")
	if err == nil {
		t.Error("expected error when filePath is missing")
	}
}

func TestBuildParams_MissingPosition(t *testing.T) {
	d := NewDriverWithCommand("gopls")
	req := lspRequest{
		Operation: "hover",
		FilePath:  "main.go",
	}

	_, err := d.buildParams(req, "textDocument/hover", "/work")
	if err == nil {
		t.Error("expected error when line/character are missing for hover")
	}
}

func TestBuildParams_FindReferences_IncludesContext(t *testing.T) {
	d := NewDriverWithCommand("gopls")
	req := lspRequest{
		Operation: "findReferences",
		FilePath:  "main.go",
		Line:      5,
		Character: 3,
	}

	params, err := d.buildParams(req, "textDocument/references", "/work")
	if err != nil {
		t.Fatalf("buildParams failed: %v", err)
	}

	m := params.(map[string]any)
	ctx, ok := m["context"].(map[string]any)
	if !ok {
		t.Fatal("expected 'context' in references params")
	}
	if ctx["includeDeclaration"] != true {
		t.Error("expected includeDeclaration=true")
	}
}

func TestBuildParams_Hover(t *testing.T) {
	d := NewDriverWithCommand("gopls")
	req := lspRequest{
		Operation: "hover",
		FilePath:  "main.go",
		Line:      5,
		Character: 10,
	}

	params, err := d.buildParams(req, "textDocument/hover", "/work")
	if err != nil {
		t.Fatalf("buildParams failed: %v", err)
	}

	m := params.(map[string]any)
	td := m["textDocument"].(map[string]any)
	if td["uri"] != "file:///work/main.go" {
		t.Errorf("unexpected URI: %v", td["uri"])
	}
	pos := m["position"].(map[string]any)
	if pos["line"] != 4 || pos["character"] != 9 {
		t.Errorf("unexpected position: line=%v char=%v", pos["line"], pos["character"])
	}
}

func TestBuildParams_GoToImplementation(t *testing.T) {
	d := NewDriverWithCommand("gopls")
	req := lspRequest{
		Operation: "goToImplementation",
		FilePath:  "iface.go",
		Line:      3,
		Character: 7,
	}

	params, err := d.buildParams(req, "textDocument/implementation", "/work")
	if err != nil {
		t.Fatalf("buildParams failed: %v", err)
	}

	m := params.(map[string]any)
	td := m["textDocument"].(map[string]any)
	if td["uri"] != "file:///work/iface.go" {
		t.Errorf("unexpected URI: %v", td["uri"])
	}
	pos := m["position"].(map[string]any)
	if pos["line"] != 2 || pos["character"] != 6 {
		t.Errorf("unexpected position: line=%v char=%v", pos["line"], pos["character"])
	}
}

func TestBuildParams_PrepareCallHierarchy(t *testing.T) {
	d := NewDriverWithCommand("gopls")
	req := lspRequest{
		Operation: "prepareCallHierarchy",
		FilePath:  "main.go",
		Line:      1,
		Character: 1,
	}

	params, err := d.buildParams(req, "textDocument/prepareCallHierarchy", "/work")
	if err != nil {
		t.Fatalf("buildParams failed: %v", err)
	}

	m := params.(map[string]any)
	if m["position"] == nil {
		t.Error("expected position in params")
	}
}

func TestBuildParams_OutgoingCalls_WithItem(t *testing.T) {
	d := NewDriverWithCommand("gopls")
	item := json.RawMessage(`{"name":"func2","uri":"file:///y.go"}`)
	req := lspRequest{
		Operation: "outgoingCalls",
		Item:      item,
	}

	params, err := d.buildParams(req, "callHierarchy/outgoingCalls", "/work")
	if err != nil {
		t.Fatalf("buildParams failed: %v", err)
	}

	m := params.(map[string]any)
	if m["item"] == nil {
		t.Error("expected item in params")
	}
}

func TestBuildParams_OutgoingCalls_MissingItem(t *testing.T) {
	d := NewDriverWithCommand("gopls")
	req := lspRequest{
		Operation: "outgoingCalls",
	}

	_, err := d.buildParams(req, "callHierarchy/outgoingCalls", "/work")
	if err == nil {
		t.Error("expected error when item is missing for outgoingCalls")
	}
}

func TestBuildParams_PathTraversal(t *testing.T) {
	d := NewDriverWithCommand("gopls")
	req := lspRequest{
		Operation: "goToDefinition",
		FilePath:  "../../etc/passwd",
		Line:      1,
		Character: 1,
	}

	_, err := d.buildParams(req, "textDocument/definition", "/work")
	if err == nil {
		t.Error("expected error for path traversal attempt")
	}
	de, ok := err.(*types.DriverError)
	if !ok {
		t.Fatalf("expected *types.DriverError, got %T", err)
	}
	if de.Code != types.ErrPermission {
		t.Errorf("expected ErrPermission, got %v", de.Code)
	}
}

// --- lspOperations mapping test ---

func TestLspOperations_AllMapped(t *testing.T) {
	expectedOps := []string{
		"goToDefinition", "findReferences", "hover",
		"documentSymbol", "workspaceSymbol",
		"goToImplementation", "prepareCallHierarchy",
		"incomingCalls", "outgoingCalls",
	}

	for _, op := range expectedOps {
		if _, ok := lspOperations[op]; !ok {
			t.Errorf("operation %q not mapped in lspOperations", op)
		}
	}

	if len(lspOperations) != 9 {
		t.Errorf("expected exactly 9 operations, got %d", len(lspOperations))
	}
}

// --- FileFactory test ---

func TestFileFactory(t *testing.T) {
	d := NewDriverWithCommand("gopls")
	factory := FileFactory(d)

	file, err := factory("", vfs.O_RDWR, "/tmp")
	if err != nil {
		t.Fatalf("FileFactory failed: %v", err)
	}

	lf, ok := file.(*LspFile)
	if !ok {
		t.Fatal("expected *LspFile")
	}
	if lf.devicePath != "/dev/lsp" {
		t.Errorf("unexpected devicePath: %q", lf.devicePath)
	}
	if lf.workDir != "/tmp" {
		t.Errorf("unexpected workDir: %q", lf.workDir)
	}
}

// --- Device registration integration test (Task 4.3) ---

func TestDeviceRegistration_Integration(t *testing.T) {
	devReg := vfs.NewDeviceRegistry()

	webDriver := NewDriverWithCommand("gopls")
	err := devReg.RegisterWithDriver("/dev/lsp", FileFactory(webDriver), webDriver)
	if err != nil {
		t.Fatalf("RegisterWithDriver failed: %v", err)
	}

	// Verify driver is discoverable
	driver, ok := devReg.GetDriver("/dev/lsp")
	if !ok {
		t.Fatal("driver not found after registration")
	}

	td, ok := driver.(vfs.ToolDescriptor)
	if !ok {
		t.Fatal("driver does not implement ToolDescriptor")
	}

	defs := td.ToolDefs()
	if len(defs) != 1 || defs[0].Name != "LSP" {
		t.Errorf("unexpected ToolDefs: %+v", defs)
	}

	// Verify RangeDrivers discovers it
	found := false
	devReg.RangeDrivers(func(path string, d any) bool {
		if path == "/dev/lsp" {
			found = true
			return false
		}
		return true
	})
	if !found {
		t.Error("driver not found via RangeDrivers")
	}
}
