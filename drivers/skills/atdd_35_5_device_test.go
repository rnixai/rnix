package skills

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/rnixai/rnix/vfs"
)

// =============================================================================
// ATDD Tests for Story 35.5: /dev/skills/manage VFS Device
// TDD RED PHASE — Tests for drivers/skills/driver.go + file.go
// =============================================================================

// --- Mock SkillManager for device tests ---

type mockSkillManager struct {
	createErr error
	patchErr  error
	deleteErr error

	lastCreateName         string
	lastCreateDescription  string
	lastCreateAllowedTools string
	lastCreateBody         string
	lastCreateCallerDevs   []string

	lastPatchName       string
	lastPatchBody       string
	lastPatchCallerDevs []string

	lastDeleteName string
}

func (m *mockSkillManager) Create(name, description, allowedTools, body string, callerDevices []string) error {
	m.lastCreateName = name
	m.lastCreateDescription = description
	m.lastCreateAllowedTools = allowedTools
	m.lastCreateBody = body
	m.lastCreateCallerDevs = callerDevices
	return m.createErr
}

func (m *mockSkillManager) Patch(name, newBody string, callerDevices []string) error {
	m.lastPatchName = name
	m.lastPatchBody = newBody
	m.lastPatchCallerDevs = callerDevices
	return m.patchErr
}

func (m *mockSkillManager) Delete(name string) error {
	m.lastDeleteName = name
	return m.deleteErr
}

// =============================================================================
// ToolDef Tests (AC8)
// =============================================================================

// 35.5-VFS-001: SkillManageDriver implements ToolDescriptor
func TestSkillManageDriver_ToolDefs(t *testing.T) {
	driver := NewSkillManageDriver(&mockSkillManager{})
	defs := driver.ToolDefs()

	if len(defs) != 1 {
		t.Fatalf("expected 1 ToolDef, got %d", len(defs))
	}

	def := defs[0]
	if def.Name != "SkillManage" {
		t.Errorf("expected Name='SkillManage', got %q", def.Name)
	}
	if def.IsReadOnly {
		t.Error("expected IsReadOnly=false")
	}
	if !def.IsConcurrencySafe {
		t.Error("expected IsConcurrencySafe=true")
	}
	if !def.IsDestructive {
		t.Error("expected IsDestructive=true (delete operation)")
	}
	if !def.ShouldDefer {
		t.Error("expected ShouldDefer=true (non-core device)")
	}
	if def.SearchHint == "" {
		t.Error("expected non-empty SearchHint")
	}
	if def.Description == "" {
		t.Error("expected non-empty Description from embedded prompt")
	}
}

// 35.5-VFS-002: SkillManageDriver compile-time interface check
func TestSkillManageDriver_ImplementsToolDescriptor(t *testing.T) {
	var _ vfs.ToolDescriptor = (*SkillManageDriver)(nil)
}

// =============================================================================
// Write: Create Action Tests (AC1)
// =============================================================================

// 35.5-VFS-003: Write with action=create routes to SkillManager.Create
func TestSkillManageFile_Write_Create(t *testing.T) {
	mgr := &mockSkillManager{}
	driver := NewSkillManageDriver(mgr)
	file := &SkillManageFile{driver: driver}

	req, _ := json.Marshal(map[string]string{
		"action":        "create",
		"name":          "test-skill",
		"description":   "A test skill",
		"allowed_tools": "/dev/fs /dev/shell",
		"body":          "# Instructions\nDo things.",
	})

	err := file.Write(context.Background(), req)
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	if mgr.lastCreateName != "test-skill" {
		t.Errorf("expected Create called with name='test-skill', got %q", mgr.lastCreateName)
	}
	if mgr.lastCreateDescription != "A test skill" {
		t.Errorf("expected description='A test skill', got %q", mgr.lastCreateDescription)
	}
	if mgr.lastCreateAllowedTools != "/dev/fs /dev/shell" {
		t.Errorf("expected allowed_tools='/dev/fs /dev/shell', got %q", mgr.lastCreateAllowedTools)
	}
	if mgr.lastCreateBody != "# Instructions\nDo things." {
		t.Errorf("expected body passed correctly, got %q", mgr.lastCreateBody)
	}
}

// 35.5-VFS-004: Write with action=create and empty body returns error
func TestSkillManageFile_Write_Create_EmptyBody(t *testing.T) {
	mgr := &mockSkillManager{}
	driver := NewSkillManageDriver(mgr)
	file := &SkillManageFile{driver: driver}

	req, _ := json.Marshal(map[string]string{
		"action":      "create",
		"name":        "test-skill",
		"description": "A test skill",
	})

	err := file.Write(context.Background(), req)
	if err == nil {
		t.Error("expected error when body is empty for create action")
	}
}

// =============================================================================
// Write: Patch Action Tests (AC2)
// =============================================================================

// 35.5-VFS-005: Write with action=patch routes to SkillManager.Patch
func TestSkillManageFile_Write_Patch(t *testing.T) {
	mgr := &mockSkillManager{}
	driver := NewSkillManageDriver(mgr)
	file := &SkillManageFile{driver: driver}

	req, _ := json.Marshal(map[string]string{
		"action": "patch",
		"name":   "existing-skill",
		"body":   "updated body",
	})

	err := file.Write(context.Background(), req)
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	if mgr.lastPatchName != "existing-skill" {
		t.Errorf("expected Patch called with name='existing-skill', got %q", mgr.lastPatchName)
	}
	if mgr.lastPatchBody != "updated body" {
		t.Errorf("expected body='updated body', got %q", mgr.lastPatchBody)
	}
}

// 35.5-VFS-006: Write with action=patch and empty body returns error
func TestSkillManageFile_Write_Patch_EmptyBody(t *testing.T) {
	mgr := &mockSkillManager{}
	driver := NewSkillManageDriver(mgr)
	file := &SkillManageFile{driver: driver}

	req, _ := json.Marshal(map[string]string{
		"action": "patch",
		"name":   "existing-skill",
	})

	err := file.Write(context.Background(), req)
	if err == nil {
		t.Error("expected error when body is empty for patch action")
	}
}

// =============================================================================
// Write: Delete Action Tests (AC3)
// =============================================================================

// 35.5-VFS-007: Write with action=delete routes to SkillManager.Delete
func TestSkillManageFile_Write_Delete(t *testing.T) {
	mgr := &mockSkillManager{}
	driver := NewSkillManageDriver(mgr)
	file := &SkillManageFile{driver: driver}

	req, _ := json.Marshal(map[string]string{
		"action": "delete",
		"name":   "unwanted-skill",
	})

	err := file.Write(context.Background(), req)
	if err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	if mgr.lastDeleteName != "unwanted-skill" {
		t.Errorf("expected Delete called with name='unwanted-skill', got %q", mgr.lastDeleteName)
	}
}

// =============================================================================
// Write: Error Handling Tests
// =============================================================================

// 35.5-VFS-008: Write with invalid JSON returns error
func TestSkillManageFile_Write_InvalidJSON(t *testing.T) {
	mgr := &mockSkillManager{}
	driver := NewSkillManageDriver(mgr)
	file := &SkillManageFile{driver: driver}

	err := file.Write(context.Background(), []byte("not json"))
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

// 35.5-VFS-009: Write with unknown action returns error
func TestSkillManageFile_Write_UnknownAction(t *testing.T) {
	mgr := &mockSkillManager{}
	driver := NewSkillManageDriver(mgr)
	file := &SkillManageFile{driver: driver}

	req, _ := json.Marshal(map[string]string{
		"action": "invalid",
		"name":   "test",
	})

	err := file.Write(context.Background(), req)
	if err == nil {
		t.Error("expected error for unknown action")
	}
}

// =============================================================================
// Read Tests
// =============================================================================

// 35.5-VFS-010: Read after successful Write returns success message
func TestSkillManageFile_Read_AfterWrite(t *testing.T) {
	mgr := &mockSkillManager{}
	driver := NewSkillManageDriver(mgr)
	file := &SkillManageFile{driver: driver}

	req, _ := json.Marshal(map[string]string{
		"action": "delete",
		"name":   "test",
	})
	_ = file.Write(context.Background(), req)

	data, err := file.Read(0)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	if len(data) == 0 {
		t.Error("expected non-empty Read result after successful Write")
	}
}

// 35.5-VFS-011: Read before Write returns empty or error
func TestSkillManageFile_Read_BeforeWrite(t *testing.T) {
	mgr := &mockSkillManager{}
	driver := NewSkillManageDriver(mgr)
	file := &SkillManageFile{driver: driver}

	data, err := file.Read(0)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	// Before any Write, result should be empty — this is acceptable
	_ = data
}

// =============================================================================
// Stat Tests
// =============================================================================

// 35.5-VFS-012: Stat returns correct device metadata
func TestSkillManageFile_Stat(t *testing.T) {
	mgr := &mockSkillManager{}
	driver := NewSkillManageDriver(mgr)
	file := &SkillManageFile{driver: driver}

	stat, err := file.Stat()
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if !stat.IsDevice {
		t.Error("expected IsDevice=true")
	}
	if stat.DevicePath != "/dev/skills/manage" {
		t.Errorf("expected DevicePath='/dev/skills/manage', got %q", stat.DevicePath)
	}
}

// =============================================================================
// FileFactory Tests
// =============================================================================

// 35.5-VFS-013: SkillManageFileFactory produces working files
func TestSkillManageFileFactory(t *testing.T) {
	mgr := &mockSkillManager{}
	driver := NewSkillManageDriver(mgr)
	factory := SkillManageFileFactory(driver)

	file, err := factory("", 0, "/tmp")
	if err != nil {
		t.Fatalf("factory failed: %v", err)
	}
	if file == nil {
		t.Fatal("expected non-nil file")
	}

	stat, err := file.Stat()
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if stat.DevicePath != "/dev/skills/manage" {
		t.Errorf("expected DevicePath='/dev/skills/manage', got %q", stat.DevicePath)
	}
}

// =============================================================================
// CallerDevices Injection Tests (AC4)
// =============================================================================

// 35.5-VFS-014: CallerDevices are passed through to SkillManager
func TestSkillManageFile_Write_CallerDevicesInjection(t *testing.T) {
	mgr := &mockSkillManager{}
	driver := NewSkillManageDriver(mgr)
	file := &SkillManageFile{
		driver:        driver,
		callerDevices: []string{"/dev/fs", "/dev/shell"},
	}

	req, _ := json.Marshal(map[string]string{
		"action":        "create",
		"name":          "test",
		"description":   "test",
		"allowed_tools": "/dev/fs",
		"body":          "body",
	})
	_ = file.Write(context.Background(), req)

	if len(mgr.lastCreateCallerDevs) != 2 {
		t.Errorf("expected 2 callerDevices, got %d", len(mgr.lastCreateCallerDevs))
	}
}
