package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// --- ToolDef metadata tests ---

func TestTasksDriver_ToolDefs_Metadata(t *testing.T) {
	d := NewDriver()
	defs := d.ToolDefs()

	if len(defs) != 4 {
		t.Fatalf("expected 4 ToolDefs, got %d", len(defs))
	}

	tests := []struct {
		name       string
		readOnly   bool
		concurrent bool
		destr      bool
		deferred   bool
		hint       string
	}{
		{"TaskCreate", false, true, false, false, "task create manage todo"},
		{"TaskUpdate", false, true, false, false, "task update status progress"},
		{"TaskList", true, true, false, false, "task list query filter"},
		{"TaskGet", true, true, false, false, "task get detail info"},
	}

	for i, tt := range tests {
		def := defs[i]
		if def.Name != tt.name {
			t.Errorf("[%d] expected name %q, got %q", i, tt.name, def.Name)
		}
		if def.IsReadOnly != tt.readOnly {
			t.Errorf("[%s] IsReadOnly: want %v, got %v", tt.name, tt.readOnly, def.IsReadOnly)
		}
		if def.IsConcurrencySafe != tt.concurrent {
			t.Errorf("[%s] IsConcurrencySafe: want %v, got %v", tt.name, tt.concurrent, def.IsConcurrencySafe)
		}
		if def.IsDestructive != tt.destr {
			t.Errorf("[%s] IsDestructive: want %v, got %v", tt.name, tt.destr, def.IsDestructive)
		}
		if def.ShouldDefer != tt.deferred {
			t.Errorf("[%s] ShouldDefer: want %v, got %v", tt.name, tt.deferred, def.ShouldDefer)
		}
		if def.SearchHint != tt.hint {
			t.Errorf("[%s] SearchHint: want %q, got %q", tt.name, tt.hint, def.SearchHint)
		}
		if def.Description == "" {
			t.Errorf("[%s] Description should not be empty", tt.name)
		}
	}
}

func TestTasksDriver_ToolDescriptorInterface(t *testing.T) {
	var _ vfs.ToolDescriptor = (*TasksDriver)(nil)
}

// --- TaskStore CRUD tests ---

func TestTaskStore_Create(t *testing.T) {
	s := NewTaskStore()
	task := s.Create("Fix bug", "A critical bug", "", "alice")

	if task.ID == "" {
		t.Fatal("task ID should not be empty")
	}
	if task.Subject != "Fix bug" {
		t.Errorf("unexpected subject: %q", task.Subject)
	}
	if task.Status != TaskPending {
		t.Errorf("expected default status pending, got %q", task.Status)
	}
	if task.Owner != "alice" {
		t.Errorf("expected owner alice, got %q", task.Owner)
	}
}

func TestTaskStore_Create_WithStatus(t *testing.T) {
	s := NewTaskStore()
	task := s.Create("Active task", "", TaskInProgress, "")

	if task.Status != TaskInProgress {
		t.Errorf("expected in_progress, got %q", task.Status)
	}
}

func TestTaskStore_Get(t *testing.T) {
	s := NewTaskStore()
	created := s.Create("Test task", "", "", "")

	got := s.Get(created.ID)
	if got == nil {
		t.Fatal("expected to find task")
	}
	if got.ID != created.ID {
		t.Errorf("ID mismatch: %q vs %q", got.ID, created.ID)
	}

	missing := s.Get("nonexistent")
	if missing != nil {
		t.Error("expected nil for missing task")
	}
}

func TestTaskStore_Update(t *testing.T) {
	s := NewTaskStore()
	created := s.Create("Original", "", "", "")

	updated := s.Update(created.ID, func(t *Task) {
		t.Subject = "Updated"
		t.Status = TaskCompleted
	})
	if updated == nil {
		t.Fatal("expected updated task")
	}
	if updated.Subject != "Updated" {
		t.Errorf("expected subject 'Updated', got %q", updated.Subject)
	}
	if updated.Status != TaskCompleted {
		t.Errorf("expected status completed, got %q", updated.Status)
	}

	missing := s.Update("nonexistent", func(t *Task) {})
	if missing != nil {
		t.Error("expected nil for missing task update")
	}
}

func TestTaskStore_List(t *testing.T) {
	s := NewTaskStore()
	s.Create("Task A", "", TaskPending, "")
	s.Create("Task B", "", TaskInProgress, "")
	s.Create("Task C", "", TaskPending, "")

	all := s.List("")
	if len(all) != 3 {
		t.Errorf("expected 3 tasks, got %d", len(all))
	}

	pending := s.List(TaskPending)
	if len(pending) != 2 {
		t.Errorf("expected 2 pending tasks, got %d", len(pending))
	}

	inProgress := s.List(TaskInProgress)
	if len(inProgress) != 1 {
		t.Errorf("expected 1 in-progress task, got %d", len(inProgress))
	}
}

func TestTaskStore_Dependencies(t *testing.T) {
	s := NewTaskStore()
	a := s.Create("Task A", "", "", "")
	b := s.Create("Task B", "", "", "")

	if err := s.AddBlocks(a.ID, b.ID); err != nil {
		t.Fatalf("AddBlocks failed: %v", err)
	}

	aAfter := s.Get(a.ID)
	bAfter := s.Get(b.ID)

	if len(aAfter.Blocks) != 1 || aAfter.Blocks[0] != b.ID {
		t.Errorf("A should block B: %v", aAfter.Blocks)
	}
	if len(bAfter.BlockedBy) != 1 || bAfter.BlockedBy[0] != a.ID {
		t.Errorf("B should be blocked by A: %v", bAfter.BlockedBy)
	}

	// Duplicate add should be idempotent
	if err := s.AddBlocks(a.ID, b.ID); err != nil {
		t.Fatalf("second AddBlocks failed: %v", err)
	}
	aFinal := s.Get(a.ID)
	if len(aFinal.Blocks) != 1 {
		t.Errorf("duplicate should be idempotent, got %d blocks", len(aFinal.Blocks))
	}
}

func TestTaskStore_AddBlocks_NotFound(t *testing.T) {
	s := NewTaskStore()
	a := s.Create("Task A", "", "", "")

	if err := s.AddBlocks(a.ID, "nonexistent"); err == nil {
		t.Error("expected error for missing blocked task")
	}
	if err := s.AddBlocks("nonexistent", a.ID); err == nil {
		t.Error("expected error for missing blocker task")
	}
}

// --- VFSFile operation tests ---

func writeAndRead(t *testing.T, f *TasksFile, data []byte) json.RawMessage {
	t.Helper()
	if err := f.Write(context.Background(), data); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	resp, err := f.Read(0)
	if err != nil {
		t.Fatalf("Read failed: %v", err)
	}
	return resp
}

func TestTasksFile_Create(t *testing.T) {
	d := NewDriver()
	f := &TasksFile{driver: d, devicePath: "/dev/tasks"}

	data, _ := json.Marshal(map[string]any{
		"subject":     "New task",
		"description": "Some details",
		"owner":       "bob",
	})

	resp := writeAndRead(t, f, data)

	var task Task
	if err := json.Unmarshal(resp, &task); err != nil {
		t.Fatalf("unmarshal task: %v", err)
	}
	if task.Subject != "New task" {
		t.Errorf("unexpected subject: %q", task.Subject)
	}
	if task.ID == "" {
		t.Error("task should have an ID")
	}
}

func TestTasksFile_Update(t *testing.T) {
	d := NewDriver()
	f := &TasksFile{driver: d, devicePath: "/dev/tasks"}

	// Create first
	createData, _ := json.Marshal(map[string]any{"subject": "Original"})
	resp := writeAndRead(t, f, createData)
	var created Task
	json.Unmarshal(resp, &created)

	// Update
	updateData, _ := json.Marshal(map[string]any{
		"id":     created.ID,
		"status": "completed",
	})
	resp = writeAndRead(t, f, updateData)
	var updated Task
	json.Unmarshal(resp, &updated)

	if updated.Status != TaskCompleted {
		t.Errorf("expected completed, got %q", updated.Status)
	}
}

func TestTasksFile_Get(t *testing.T) {
	d := NewDriver()
	f := &TasksFile{driver: d, devicePath: "/dev/tasks"}

	// Create
	createData, _ := json.Marshal(map[string]any{"subject": "My task"})
	resp := writeAndRead(t, f, createData)
	var created Task
	json.Unmarshal(resp, &created)

	// Get
	getData, _ := json.Marshal(map[string]any{"id": created.ID})
	resp = writeAndRead(t, f, getData)
	var got Task
	json.Unmarshal(resp, &got)

	if got.Subject != "My task" {
		t.Errorf("unexpected subject: %q", got.Subject)
	}
}

func TestTasksFile_Get_NotFound(t *testing.T) {
	d := NewDriver()
	f := &TasksFile{driver: d, devicePath: "/dev/tasks"}

	data, _ := json.Marshal(map[string]any{"id": "nonexistent"})
	err := f.Write(context.Background(), data)
	if err == nil {
		t.Fatal("expected error for missing task")
	}
	var de *types.DriverError
	if !errors.As(err, &de) || de.Code != types.ErrNotFound {
		t.Errorf("expected ErrNotFound, got %v", err)
	}
}

func TestTasksFile_List(t *testing.T) {
	d := NewDriver()
	f := &TasksFile{driver: d, devicePath: "/dev/tasks"}

	// Create tasks
	for _, subj := range []string{"A", "B", "C"} {
		data, _ := json.Marshal(map[string]any{"subject": subj})
		writeAndRead(t, f, data)
	}

	// List all
	listData, _ := json.Marshal(map[string]any{})
	resp := writeAndRead(t, f, listData)
	var tasks []*Task
	json.Unmarshal(resp, &tasks)
	if len(tasks) != 3 {
		t.Errorf("expected 3 tasks, got %d", len(tasks))
	}
}

func TestTasksFile_List_FilteredByStatus(t *testing.T) {
	d := NewDriver()
	f := &TasksFile{driver: d, devicePath: "/dev/tasks"}

	// Create mixed status tasks
	for _, subj := range []string{"A", "B"} {
		data, _ := json.Marshal(map[string]any{"subject": subj})
		writeAndRead(t, f, data)
	}
	data, _ := json.Marshal(map[string]any{"subject": "C", "status": "in_progress"})
	writeAndRead(t, f, data)

	// Filter by pending
	listData, _ := json.Marshal(map[string]any{"status": "pending"})
	resp := writeAndRead(t, f, listData)
	var tasks []*Task
	json.Unmarshal(resp, &tasks)
	if len(tasks) != 2 {
		t.Errorf("expected 2 pending tasks, got %d", len(tasks))
	}
}

func TestTasksFile_InvalidJSON(t *testing.T) {
	d := NewDriver()
	f := &TasksFile{driver: d, devicePath: "/dev/tasks"}

	err := f.Write(context.Background(), []byte("not json"))
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	var de *types.DriverError
	if !errors.As(err, &de) || de.Code != types.ErrInvalid {
		t.Errorf("expected ErrInvalid, got %v", err)
	}
}

func TestTasksFile_Close(t *testing.T) {
	d := NewDriver()
	f := &TasksFile{driver: d, devicePath: "/dev/tasks"}

	if err := f.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}
	if err := f.Close(); err == nil {
		t.Fatal("expected error on double close")
	}
	if err := f.Write(context.Background(), []byte("{}")); err == nil {
		t.Fatal("expected error on write after close")
	}
}

func TestTasksFile_Stat(t *testing.T) {
	d := NewDriver()
	f := &TasksFile{driver: d, devicePath: "/dev/tasks"}

	stat, err := f.Stat()
	if err != nil {
		t.Fatalf("Stat failed: %v", err)
	}
	if !stat.IsDevice {
		t.Error("expected IsDevice=true")
	}
	if stat.DevicePath != "/dev/tasks" {
		t.Errorf("expected DevicePath /dev/tasks, got %q", stat.DevicePath)
	}
}

// --- Device registration integration test ---

func TestDeviceRegistration_Integration(t *testing.T) {
	devReg := vfs.NewDeviceRegistry()

	d := NewDriver()
	err := devReg.RegisterWithDriver("/dev/tasks", FileFactory(d), d)
	if err != nil {
		t.Fatalf("RegisterWithDriver failed: %v", err)
	}

	driver, ok := devReg.GetDriver("/dev/tasks")
	if !ok {
		t.Fatal("driver not found for /dev/tasks")
	}

	td, ok := driver.(vfs.ToolDescriptor)
	if !ok {
		t.Fatal("driver does not implement ToolDescriptor")
	}

	defs := td.ToolDefs()
	if len(defs) != 4 {
		t.Fatalf("expected 4 ToolDefs, got %d", len(defs))
	}

	found := false
	devReg.RangeDrivers(func(path string, d any) bool {
		if path == "/dev/tasks" {
			found = true
			return false
		}
		return true
	})
	if !found {
		t.Error("/dev/tasks not found via RangeDrivers")
	}
}
