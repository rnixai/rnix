package tasks

import (
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rnixai/rnix/internal/xsync"
)

// TaskStatus represents the lifecycle state of a task.
type TaskStatus string

const (
	TaskPending    TaskStatus = "pending"
	TaskInProgress TaskStatus = "in_progress"
	TaskCompleted  TaskStatus = "completed"
	TaskDeleted    TaskStatus = "deleted"
)

// ValidStatus reports whether s is a recognized task status.
func ValidStatus(s TaskStatus) bool {
	switch s {
	case TaskPending, TaskInProgress, TaskCompleted, TaskDeleted:
		return true
	}
	return false
}

// Task represents a tracked work item.
type Task struct {
	ID          string     `json:"id"`
	Subject     string     `json:"subject"`
	Description string     `json:"description,omitempty"`
	Status      TaskStatus `json:"status"`
	Owner       string     `json:"owner,omitempty"`
	Blocks      []string   `json:"blocks,omitempty"`
	BlockedBy   []string   `json:"blocked_by,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

// TaskStore is a thread-safe in-memory task storage.
// Tasks are session-level (daemon lifetime) and not persisted to disk.
type TaskStore struct {
	tasks   *xsync.SyncMap[string, *Task]
	counter atomic.Int64
	mu      sync.Mutex // protects Update and AddBlocks for field-level safety
}

// NewTaskStore creates an empty TaskStore.
func NewTaskStore() *TaskStore {
	return &TaskStore{
		tasks: xsync.NewSyncMap[string, *Task](),
	}
}

// nextID generates a monotonically increasing short task ID.
func (s *TaskStore) nextID() string {
	n := s.counter.Add(1)
	return fmt.Sprintf("task-%d", n)
}

// Create adds a new task and returns it.
func (s *TaskStore) Create(subject, description string, status TaskStatus, owner string) *Task {
	if status == "" {
		status = TaskPending
	}
	now := time.Now()
	t := &Task{
		ID:          s.nextID(),
		Subject:     subject,
		Description: description,
		Status:      status,
		Owner:       owner,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	s.tasks.Store(t.ID, t)
	return t
}

// Get retrieves a task by ID. Returns nil if not found.
func (s *TaskStore) Get(id string) *Task {
	t, ok := s.tasks.Load(id)
	if !ok {
		return nil
	}
	return t
}

// Update modifies an existing task. Returns the updated task or nil if not found.
func (s *TaskStore) Update(id string, fn func(t *Task)) *Task {
	s.mu.Lock()
	defer s.mu.Unlock()

	t, ok := s.tasks.Load(id)
	if !ok {
		return nil
	}
	// Clone to avoid data races with concurrent List/Get/Marshal
	clone := *t
	clone.Blocks = slices.Clone(t.Blocks)
	clone.BlockedBy = slices.Clone(t.BlockedBy)
	fn(&clone)
	clone.UpdatedAt = time.Now()
	s.tasks.Store(id, &clone)
	return &clone
}

// List returns all tasks, optionally filtered by status.
func (s *TaskStore) List(statusFilter TaskStatus) []*Task {
	var result []*Task
	s.tasks.Range(func(id string, t *Task) bool {
		if statusFilter == "" || t.Status == statusFilter {
			result = append(result, t)
		}
		return true
	})
	return result
}

// AddBlocks adds a "blocks" dependency: task `id` blocks task `blockedID`.
// Updates both sides of the relationship.
func (s *TaskStore) AddBlocks(id, blockedID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	blocker, ok := s.tasks.Load(id)
	if !ok {
		return fmt.Errorf("task %q not found", id)
	}
	blocked, ok := s.tasks.Load(blockedID)
	if !ok {
		return fmt.Errorf("task %q not found", blockedID)
	}

	// Clone both tasks to avoid data races
	blockerClone := *blocker
	blockerClone.Blocks = slices.Clone(blocker.Blocks)
	blockerClone.BlockedBy = slices.Clone(blocker.BlockedBy)

	blockedClone := *blocked
	blockedClone.Blocks = slices.Clone(blocked.Blocks)
	blockedClone.BlockedBy = slices.Clone(blocked.BlockedBy)

	if !containsStr(blockerClone.Blocks, blockedID) {
		blockerClone.Blocks = append(blockerClone.Blocks, blockedID)
		blockerClone.UpdatedAt = time.Now()
	}
	if !containsStr(blockedClone.BlockedBy, id) {
		blockedClone.BlockedBy = append(blockedClone.BlockedBy, id)
		blockedClone.UpdatedAt = time.Now()
	}

	s.tasks.Store(id, &blockerClone)
	s.tasks.Store(blockedID, &blockedClone)
	return nil
}

// AddBlockedBy adds a "blocked by" dependency: task `id` is blocked by task `blockerID`.
// Updates both sides of the relationship.
func (s *TaskStore) AddBlockedBy(id, blockerID string) error {
	return s.AddBlocks(blockerID, id)
}

func containsStr(slice []string, s string) bool {
	return slices.Contains(slice, s)
}
