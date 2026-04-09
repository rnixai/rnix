// Package tasks implements the /dev/tasks VFS device for dynamic task management.
package tasks

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// TasksDriver implements the /dev/tasks VFS device.
type TasksDriver struct {
	store *TaskStore
}

// compile-time interface check
var _ vfs.ToolDescriptor = (*TasksDriver)(nil)

// NewDriver creates a TasksDriver with a shared TaskStore.
func NewDriver() *TasksDriver {
	return &TasksDriver{store: NewTaskStore()}
}

// NewDriverWithStore creates a TasksDriver with an explicit TaskStore (for testing).
func NewDriverWithStore(store *TaskStore) *TasksDriver {
	return &TasksDriver{store: store}
}

// ToolDefs returns the tool definitions for the tasks device.
func (d *TasksDriver) ToolDefs() []vfs.ToolDef {
	return []vfs.ToolDef{
		{
			Name:              "task_create",
			Description:       loadPrompt("task_create"),
			IsReadOnly:        false,
			IsConcurrencySafe: true,
			IsDestructive:     false,
			ShouldDefer:       false,
			SearchHint:        "task create manage todo",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"subject": map[string]any{
						"type":        "string",
						"description": "Brief title for the task",
					},
					"description": map[string]any{
						"type":        "string",
						"description": "Detailed task description",
					},
					"status": map[string]any{
						"type":        "string",
						"enum":        []string{"pending", "in_progress"},
						"description": "Initial status (default: pending)",
					},
					"owner": map[string]any{
						"type":        "string",
						"description": "Who is responsible for this task",
					},
					"addBlocks": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Task IDs that this task blocks",
					},
					"addBlockedBy": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Task IDs that block this task",
					},
				},
				"required": []string{"subject"},
			},
		},
		{
			Name:              "task_update",
			Description:       loadPrompt("task_update"),
			IsReadOnly:        false,
			IsConcurrencySafe: true,
			IsDestructive:     false,
			ShouldDefer:       false,
			SearchHint:        "task update status progress",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id": map[string]any{
						"type":        "string",
						"description": "Task ID to update",
					},
					"subject": map[string]any{
						"type":        "string",
						"description": "New task title",
					},
					"description": map[string]any{
						"type":        "string",
						"description": "New task description",
					},
					"status": map[string]any{
						"type":        "string",
						"enum":        []string{"pending", "in_progress", "completed", "deleted"},
						"description": "New task status",
					},
					"owner": map[string]any{
						"type":        "string",
						"description": "New task owner",
					},
					"addBlocks": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Task IDs to add as blocked by this task",
					},
					"addBlockedBy": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Task IDs to add as blocking this task",
					},
				},
				"required": []string{"id"},
			},
		},
		{
			Name:              "task_list",
			Description:       loadPrompt("task_list"),
			IsReadOnly:        true,
			IsConcurrencySafe: true,
			IsDestructive:     false,
			ShouldDefer:       false,
			SearchHint:        "task list query filter",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"status": map[string]any{
						"type":        "string",
						"enum":        []string{"pending", "in_progress", "completed", "deleted"},
						"description": "Filter tasks by status",
					},
				},
			},
		},
		{
			Name:              "task_get",
			Description:       loadPrompt("task_get"),
			IsReadOnly:        true,
			IsConcurrencySafe: true,
			IsDestructive:     false,
			ShouldDefer:       false,
			SearchHint:        "task get detail info",
			Parameters: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id": map[string]any{
						"type":        "string",
						"description": "Task ID to retrieve",
					},
				},
				"required": []string{"id"},
			},
		},
	}
}

// TasksFile implements vfs.VFSFile for /dev/tasks via write-then-read semantics.
type TasksFile struct {
	driver     *TasksDriver
	devicePath string
	response   []byte
	offset     int
	closed     bool
}

// Write dispatches task operations based on the JSON input.
func (f *TasksFile) Write(_ context.Context, data []byte) error {
	if f.closed {
		return &types.DriverError{Op: "Write", Device: f.devicePath, Err: fmt.Errorf("tasks file closed"), Code: types.ErrDriver}
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return &types.DriverError{Op: "Write", Device: f.devicePath, Err: fmt.Errorf("invalid JSON input: %w", err), Code: types.ErrInvalid}
	}

	// Determine operation from the tool name in subpath or from field presence
	var result any
	var opErr error

	// Dispatch based on which fields are present.
	// Check "id" first — update/get always requires an id, while create requires subject.
	// This prevents {id, subject} from being routed to create instead of update.
	if _, hasID := raw["id"]; hasID {
		if f.hasUpdateFields(raw) {
			result, opErr = f.handleUpdate(data)
		} else {
			result, opErr = f.handleGet(data)
		}
	} else if _, hasSubject := raw["subject"]; hasSubject {
		result, opErr = f.handleCreate(data)
	} else if _, hasStatus := raw["status"]; hasStatus {
		result, opErr = f.handleList(data)
	} else {
		// No specific fields → list all
		result, opErr = f.handleList(data)
	}

	if opErr != nil {
		return opErr
	}

	respJSON, err := json.Marshal(result)
	if err != nil {
		return &types.DriverError{Op: "Write", Device: f.devicePath, Err: fmt.Errorf("marshal response: %w", err), Code: types.ErrInternal}
	}
	f.response = respJSON
	f.offset = 0
	return nil
}

func (f *TasksFile) hasUpdateFields(raw map[string]json.RawMessage) bool {
	for _, key := range []string{"subject", "description", "status", "owner", "addBlocks", "addBlockedBy"} {
		if _, ok := raw[key]; ok && key != "id" {
			return true
		}
	}
	return false
}

func (f *TasksFile) handleCreate(data []byte) (*Task, error) {
	var req struct {
		Subject     string   `json:"subject"`
		Description string   `json:"description"`
		Status      string   `json:"status"`
		Owner       string   `json:"owner"`
		AddBlocks   []string `json:"addBlocks"`
		AddBlockedBy []string `json:"addBlockedBy"`
	}
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, &types.DriverError{Op: "Write", Device: f.devicePath, Err: fmt.Errorf("invalid create request: %w", err), Code: types.ErrInvalid}
	}
	if req.Subject == "" {
		return nil, &types.DriverError{Op: "Write", Device: f.devicePath, Err: fmt.Errorf("subject is required"), Code: types.ErrInvalid}
	}

	status := TaskStatus(req.Status)
	if req.Status != "" && !ValidStatus(status) {
		return nil, &types.DriverError{Op: "Write", Device: f.devicePath, Err: fmt.Errorf("invalid status %q", req.Status), Code: types.ErrInvalid}
	}

	task := f.driver.store.Create(req.Subject, req.Description, status, req.Owner)

	for _, blockedID := range req.AddBlocks {
		_ = f.driver.store.AddBlocks(task.ID, blockedID) // non-fatal: target may not exist yet
	}
	for _, blockerID := range req.AddBlockedBy {
		_ = f.driver.store.AddBlockedBy(task.ID, blockerID) // non-fatal
	}

	return task, nil
}

func (f *TasksFile) handleUpdate(data []byte) (*Task, error) {
	var req struct {
		ID          string   `json:"id"`
		Subject     string   `json:"subject"`
		Description string   `json:"description"`
		Status      string   `json:"status"`
		Owner       string   `json:"owner"`
		AddBlocks   []string `json:"addBlocks"`
		AddBlockedBy []string `json:"addBlockedBy"`
	}
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, &types.DriverError{Op: "Write", Device: f.devicePath, Err: fmt.Errorf("invalid update request: %w", err), Code: types.ErrInvalid}
	}
	if req.ID == "" {
		return nil, &types.DriverError{Op: "Write", Device: f.devicePath, Err: fmt.Errorf("id is required"), Code: types.ErrInvalid}
	}
	if req.Status != "" && !ValidStatus(TaskStatus(req.Status)) {
		return nil, &types.DriverError{Op: "Write", Device: f.devicePath, Err: fmt.Errorf("invalid status %q", req.Status), Code: types.ErrInvalid}
	}

	task := f.driver.store.Update(req.ID, func(t *Task) {
		if req.Subject != "" {
			t.Subject = req.Subject
		}
		if req.Description != "" {
			t.Description = req.Description
		}
		if req.Status != "" {
			t.Status = TaskStatus(req.Status)
		}
		if req.Owner != "" {
			t.Owner = req.Owner
		}
	})
	if task == nil {
		return nil, &types.DriverError{Op: "Write", Device: f.devicePath, Err: fmt.Errorf("task %q not found", req.ID), Code: types.ErrNotFound}
	}

	for _, blockedID := range req.AddBlocks {
		_ = f.driver.store.AddBlocks(task.ID, blockedID)
	}
	for _, blockerID := range req.AddBlockedBy {
		_ = f.driver.store.AddBlockedBy(task.ID, blockerID)
	}

	return task, nil
}

func (f *TasksFile) handleList(data []byte) ([]*Task, error) {
	var req struct {
		Status string `json:"status"`
	}
	_ = json.Unmarshal(data, &req)

	if req.Status != "" && !ValidStatus(TaskStatus(req.Status)) {
		return nil, &types.DriverError{Op: "Write", Device: f.devicePath, Err: fmt.Errorf("invalid status filter %q", req.Status), Code: types.ErrInvalid}
	}

	tasks := f.driver.store.List(TaskStatus(req.Status))
	if tasks == nil {
		tasks = []*Task{}
	}
	return tasks, nil
}

func (f *TasksFile) handleGet(data []byte) (*Task, error) {
	var req struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, &types.DriverError{Op: "Write", Device: f.devicePath, Err: fmt.Errorf("invalid get request: %w", err), Code: types.ErrInvalid}
	}
	if req.ID == "" {
		return nil, &types.DriverError{Op: "Write", Device: f.devicePath, Err: fmt.Errorf("id is required"), Code: types.ErrInvalid}
	}

	task := f.driver.store.Get(req.ID)
	if task == nil {
		return nil, &types.DriverError{Op: "Write", Device: f.devicePath, Err: fmt.Errorf("task %q not found", req.ID), Code: types.ErrNotFound}
	}
	return task, nil
}

// Read returns buffered response data.
func (f *TasksFile) Read(length int) ([]byte, error) {
	if f.closed {
		return nil, &types.DriverError{Op: "Read", Device: f.devicePath, Err: fmt.Errorf("tasks file closed"), Code: types.ErrDriver}
	}
	if f.response == nil {
		return nil, &types.DriverError{Op: "Read", Device: f.devicePath, Err: fmt.Errorf("no data available: write a request first"), Code: types.ErrDriver}
	}

	remaining := f.response[f.offset:]
	if len(remaining) == 0 {
		return nil, nil
	}
	if length <= 0 || length > len(remaining) {
		length = len(remaining)
	}
	out := make([]byte, length)
	copy(out, remaining[:length])
	f.offset += length
	return out, nil
}

// Close marks the file as closed.
func (f *TasksFile) Close() error {
	if f.closed {
		return &types.DriverError{Op: "Close", Device: f.devicePath, Err: fmt.Errorf("tasks file already closed"), Code: types.ErrDriver}
	}
	f.closed = true
	f.response = nil
	f.offset = 0
	return nil
}

// Stat returns metadata about this tasks device file.
func (f *TasksFile) Stat() (vfs.FileStat, error) {
	if f.closed {
		return vfs.FileStat{}, &types.DriverError{Op: "Stat", Device: f.devicePath, Err: fmt.Errorf("tasks file closed"), Code: types.ErrDriver}
	}
	return vfs.FileStat{
		Name:       f.devicePath,
		Size:       int64(len(f.response)),
		IsDevice:   true,
		DevicePath: "/dev/tasks",
	}, nil
}

// FileFactory returns a VFSFileFactory that creates TasksFile instances.
func FileFactory(driver *TasksDriver) vfs.VFSFileFactory {
	return func(subpath string, flags vfs.OpenFlag, workDir string) (vfs.VFSFile, error) {
		return &TasksFile{
			driver:     driver,
			devicePath: "/dev/tasks" + subpath,
		}, nil
	}
}
