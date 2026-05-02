package intentdriver

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/rnixai/rnix/intent"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// mockDecomposeCaller implements intent.LLMCaller for testing.
type mockDecomposeCaller struct {
	response string
	err      error
}

func (m *mockDecomposeCaller) Call(_ context.Context, _ string, _ string) (string, error) {
	if m.err != nil {
		return "", m.err
	}
	return m.response, nil
}

// mockSpawner implements intent.KernelSpawner for testing.
type mockSpawner struct {
	pidAlloc uint64
}

func (m *mockSpawner) SpawnIntent(_ context.Context, _ *intent.IntentNode) (types.PID, error) {
	m.pidAlloc++
	return types.PID(m.pidAlloc), nil
}

func (m *mockSpawner) Wait(_ types.PID) (intent.ExitStatus, error) {
	return intent.ExitStatus{Code: 0, Reason: "done"}, nil
}

func newTestManager(caller intent.LLMCaller) *intent.Manager {
	return intent.NewManager(
		intent.NewDecomposer(caller),
		&mockSpawner{},
		intent.DefaultReconcilerConfig(),
	)
}

func TestIntentDriver_ToolDefs_Metadata(t *testing.T) {
	driver := NewDriver(newTestManager(&mockDecomposeCaller{}))
	defs := driver.ToolDefs()

	if len(defs) != 4 {
		t.Fatalf("expected 4 tool defs, got %d", len(defs))
	}

	expected := map[string]struct {
		readOnly bool
		deferred bool
		subpath  string
	}{
		"intent_decompose": {readOnly: false, deferred: true, subpath: "/decompose"},
		"intent_status":    {readOnly: true, deferred: false, subpath: "/status"},
		"intent_confirm":   {readOnly: false, deferred: false, subpath: "/confirm"},
		"intent_execute":   {readOnly: false, deferred: true, subpath: "/execute"},
	}

	for _, def := range defs {
		exp, ok := expected[def.Name]
		if !ok {
			t.Errorf("unexpected tool def name: %s", def.Name)
			continue
		}
		if def.Description == "" {
			t.Errorf("%s: description should not be empty", def.Name)
		}
		if def.IsReadOnly != exp.readOnly {
			t.Errorf("%s: IsReadOnly=%v, want %v", def.Name, def.IsReadOnly, exp.readOnly)
		}
		if def.ShouldDefer != exp.deferred {
			t.Errorf("%s: ShouldDefer=%v, want %v", def.Name, def.ShouldDefer, exp.deferred)
		}
		if def.Parameters == nil {
			t.Errorf("%s: Parameters should not be nil", def.Name)
		}
		if def.Subpath != exp.subpath {
			t.Errorf("%s: Subpath=%q, want %q", def.Name, def.Subpath, exp.subpath)
		}
	}
}

func TestIntentFile_Decompose_Success(t *testing.T) {
	nodesJSON := `[{"id":"a","intent":"task a","depends_on":[]},{"id":"b","intent":"task b","depends_on":["a"]}]`
	mgr := newTestManager(&mockDecomposeCaller{response: nodesJSON})
	driver := NewDriver(mgr)

	factory := FileFactory(driver)
	file, err := factory("/decompose", vfs.O_RDWR, "")
	if err != nil {
		t.Fatalf("factory error: %v", err)
	}

	input := `{"intent":"build a blog system"}`
	if err := file.Write(context.Background(), []byte(input)); err != nil {
		t.Fatalf("write error: %v", err)
	}

	data, err := file.Read(1 << 20)
	if err != nil {
		t.Fatalf("read error: %v", err)
	}

	var tree intent.IntentTree
	if err := json.Unmarshal(data, &tree); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if tree.ID == "" {
		t.Error("expected non-empty intent ID")
	}
	if len(tree.Nodes) != 2 {
		t.Errorf("expected 2 nodes, got %d", len(tree.Nodes))
	}
	if tree.State != intent.IntentAwaitConfirm {
		t.Errorf("expected state %q, got %q", intent.IntentAwaitConfirm, tree.State)
	}

	if err := file.Close(); err != nil {
		t.Fatalf("close error: %v", err)
	}
}

func TestIntentFile_Decompose_MissingIntent(t *testing.T) {
	mgr := newTestManager(&mockDecomposeCaller{})
	driver := NewDriver(mgr)
	factory := FileFactory(driver)
	file, _ := factory("/decompose", vfs.O_RDWR, "")

	err := file.Write(context.Background(), []byte(`{"model":"test"}`))
	if err == nil {
		t.Fatal("expected error for missing intent")
	}
	var driverErr *types.DriverError
	if ok := isDriverError(err, &driverErr); !ok {
		t.Fatalf("expected DriverError, got %T", err)
	}
	if driverErr.Code != types.ErrInvalid {
		t.Errorf("expected code %q, got %q", types.ErrInvalid, driverErr.Code)
	}
}

// TestIntentFile_Decompose_AutoStart verifies that auto_start=true causes
// decompose to chain confirm + execute and return a tree in terminal state
// (so the LLM can skip the follow-up intent_confirm/intent_execute calls
// that are unreliable on weak models).
func TestIntentFile_Decompose_AutoStart(t *testing.T) {
	nodesJSON := `[{"id":"a","intent":"task a","depends_on":[]}]`
	mgr := newTestManager(&mockDecomposeCaller{response: nodesJSON})
	driver := NewDriver(mgr)

	factory := FileFactory(driver)
	file, _ := factory("/decompose", vfs.O_RDWR, "")

	input := `{"intent":"hello world","auto_start":true}`
	if err := file.Write(context.Background(), []byte(input)); err != nil {
		t.Fatalf("write error: %v", err)
	}

	data, err := file.Read(1 << 20)
	if err != nil {
		t.Fatalf("read error: %v", err)
	}

	var tree intent.IntentTree
	if err := json.Unmarshal(data, &tree); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	// After auto_start, the tree should not be in await_confirm —
	// confirm + execute have run synchronously.
	if tree.State == intent.IntentAwaitConfirm {
		t.Errorf("expected post-execute state, got await_confirm — auto_start did not chain")
	}
}

func TestIntentFile_Status_Success(t *testing.T) {
	nodesJSON := `[{"id":"a","intent":"task a","depends_on":[]}]`
	mgr := newTestManager(&mockDecomposeCaller{response: nodesJSON})
	driver := NewDriver(mgr)

	// Create an intent first via the manager
	tree, err := mgr.Apply(context.Background(), intent.ApplyRequest{Intent: "test"})
	if err != nil {
		t.Fatalf("setup: apply failed: %v", err)
	}

	factory := FileFactory(driver)
	file, _ := factory("/status", vfs.O_RDWR, "")

	input, _ := json.Marshal(map[string]string{"intent_id": string(tree.ID)})
	if err := file.Write(context.Background(), input); err != nil {
		t.Fatalf("write error: %v", err)
	}

	data, err := file.Read(1 << 20)
	if err != nil {
		t.Fatalf("read error: %v", err)
	}

	var result intent.IntentTree
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if result.ID != tree.ID {
		t.Errorf("expected ID %q, got %q", tree.ID, result.ID)
	}
	file.Close()
}

func TestIntentFile_Status_NotFound(t *testing.T) {
	mgr := newTestManager(&mockDecomposeCaller{})
	driver := NewDriver(mgr)
	factory := FileFactory(driver)
	file, _ := factory("/status", vfs.O_RDWR, "")

	input := `{"intent_id":"intent-999"}`
	err := file.Write(context.Background(), []byte(input))
	if err == nil {
		t.Fatal("expected error for non-existent intent")
	}
	var driverErr *types.DriverError
	if ok := isDriverError(err, &driverErr); !ok {
		t.Fatalf("expected DriverError, got %T", err)
	}
	if driverErr.Code != types.ErrNotFound {
		t.Errorf("expected code %q, got %q", types.ErrNotFound, driverErr.Code)
	}
}

func TestIntentFile_Confirm_Success(t *testing.T) {
	nodesJSON := `[{"id":"a","intent":"task a","depends_on":[]}]`
	mgr := newTestManager(&mockDecomposeCaller{response: nodesJSON})
	driver := NewDriver(mgr)

	tree, _ := mgr.Apply(context.Background(), intent.ApplyRequest{Intent: "test"})

	factory := FileFactory(driver)
	file, _ := factory("/confirm", vfs.O_RDWR, "")

	input, _ := json.Marshal(map[string]string{"intent_id": string(tree.ID)})
	if err := file.Write(context.Background(), input); err != nil {
		t.Fatalf("write error: %v", err)
	}

	data, err := file.Read(1 << 20)
	if err != nil {
		t.Fatalf("read error: %v", err)
	}

	var result map[string]bool
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if !result["ok"] {
		t.Error("expected ok=true")
	}

	// Verify state changed
	updated, _ := mgr.Status(tree.ID)
	if updated.State != intent.IntentExecuting {
		t.Errorf("expected state %q after confirm, got %q", intent.IntentExecuting, updated.State)
	}
	file.Close()
}

func TestIntentFile_Confirm_WrongState(t *testing.T) {
	nodesJSON := `[{"id":"a","intent":"task a","depends_on":[]}]`
	mgr := newTestManager(&mockDecomposeCaller{response: nodesJSON})
	driver := NewDriver(mgr)

	tree, _ := mgr.Apply(context.Background(), intent.ApplyRequest{Intent: "test"})
	mgr.Confirm(tree.ID) // transition to executing

	factory := FileFactory(driver)
	file, _ := factory("/confirm", vfs.O_RDWR, "")

	input, _ := json.Marshal(map[string]string{"intent_id": string(tree.ID)})
	err := file.Write(context.Background(), input)
	if err == nil {
		t.Fatal("expected error for double confirm")
	}
}

func TestIntentFile_InvalidJSON(t *testing.T) {
	mgr := newTestManager(&mockDecomposeCaller{})
	driver := NewDriver(mgr)
	factory := FileFactory(driver)
	file, _ := factory("/decompose", vfs.O_RDWR, "")

	err := file.Write(context.Background(), []byte("not json"))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestIntentFile_ReadBeforeWrite(t *testing.T) {
	mgr := newTestManager(&mockDecomposeCaller{})
	driver := NewDriver(mgr)
	factory := FileFactory(driver)
	file, _ := factory("/status", vfs.O_RDWR, "")

	_, err := file.Read(1024)
	if err == nil {
		t.Fatal("expected error on read before write")
	}
}

func TestIntentFile_WriteAfterClose(t *testing.T) {
	mgr := newTestManager(&mockDecomposeCaller{})
	driver := NewDriver(mgr)
	factory := FileFactory(driver)
	file, _ := factory("/status", vfs.O_RDWR, "")

	file.Close()
	err := file.Write(context.Background(), []byte(`{"intent_id":"x"}`))
	if err == nil {
		t.Fatal("expected error on write after close")
	}
}

func TestIntentFile_UnknownSubpath(t *testing.T) {
	mgr := newTestManager(&mockDecomposeCaller{})
	driver := NewDriver(mgr)
	factory := FileFactory(driver)
	_, err := factory("/unknown", vfs.O_RDWR, "")
	if err == nil {
		t.Fatal("expected error for unknown subpath")
	}
	if !strings.Contains(err.Error(), "unknown subpath") {
		t.Errorf("error should mention unknown subpath, got: %v", err)
	}
}

func TestIntentFile_FileFactory_DevicePath(t *testing.T) {
	mgr := newTestManager(&mockDecomposeCaller{})
	driver := NewDriver(mgr)
	factory := FileFactory(driver)

	tests := []struct {
		subpath    string
		wantDevice string
	}{
		{"/decompose", "/dev/intent/decompose"},
		{"/status", "/dev/intent/status"},
		{"/confirm", "/dev/intent/confirm"},
		{"/execute", "/dev/intent/execute"},
		{"", "/dev/intent"},
	}

	for _, tt := range tests {
		file, err := factory(tt.subpath, vfs.O_RDWR, "")
		if err != nil {
			t.Fatalf("factory(%q): %v", tt.subpath, err)
		}
		stat, _ := file.Stat()
		if stat.Name != tt.wantDevice {
			t.Errorf("factory(%q): stat.Name=%q, want %q", tt.subpath, stat.Name, tt.wantDevice)
		}
	}
}

func TestIntentFile_Decompose_EmptySubpath(t *testing.T) {
	nodesJSON := `[{"id":"x","intent":"do something","depends_on":[]}]`
	mgr := newTestManager(&mockDecomposeCaller{response: nodesJSON})
	driver := NewDriver(mgr)
	factory := FileFactory(driver)
	file, _ := factory("", vfs.O_RDWR, "")

	input := `{"intent":"test intent"}`
	if err := file.Write(context.Background(), []byte(input)); err != nil {
		t.Fatalf("write error: %v", err)
	}
	data, err := file.Read(1 << 20)
	if err != nil {
		t.Fatalf("read error: %v", err)
	}
	var tree intent.IntentTree
	if err := json.Unmarshal(data, &tree); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if tree.ID == "" {
		t.Error("expected non-empty intent ID")
	}
	file.Close()
}

func isDriverError(err error, target **types.DriverError) bool {
	return errors.As(err, target)
}
