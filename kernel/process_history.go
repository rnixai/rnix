package kernel

import (
	"sync"

	"github.com/rnixai/rnix/vfs"
)

// ProcessHistory stores snapshots of processes that have been removed from the
// process table by the reaper. It acts as a bounded FIFO ring buffer protected
// by a RWMutex so the reaper can write while the Dashboard reads concurrently.
type ProcessHistory struct {
	mu      sync.RWMutex
	entries []vfs.ProcInfo
	maxSize int
}

// NewProcessHistory creates a ProcessHistory with the given capacity.
func NewProcessHistory(maxSize int) *ProcessHistory {
	return &ProcessHistory{
		entries: make([]vfs.ProcInfo, 0, maxSize),
		maxSize: maxSize,
	}
}

// Add appends a process snapshot. If the buffer is full, the oldest entry is
// evicted (FIFO).
func (h *ProcessHistory) Add(info vfs.ProcInfo) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.entries = append(h.entries, info)
	if len(h.entries) > h.maxSize {
		h.entries = h.entries[len(h.entries)-h.maxSize:]
	}
}

// List returns a deep copy of all stored snapshots.
func (h *ProcessHistory) List() []vfs.ProcInfo {
	h.mu.RLock()
	defer h.mu.RUnlock()

	out := make([]vfs.ProcInfo, len(h.entries))
	copy(out, h.entries)
	return out
}

// Len returns the current number of stored entries.
func (h *ProcessHistory) Len() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.entries)
}
