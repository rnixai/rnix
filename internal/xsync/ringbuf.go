package xsync

import "sync"

// RingBuffer is a fixed-capacity, thread-safe ring buffer with overwrite-oldest
// semantics. Once full, each Push evicts the oldest element. It is the
// substrate for Story 48.5's MCP stderr capture (drivers/mcp keeps the most
// recent N stderr lines so `rnix mcp logs` can surface crash diagnostics).
//
// The generic parameter T follows the project's semantic-naming convention for
// generic containers (cf. SyncMap[K,V], Registry[Item]); every access is
// guarded by sync.Mutex (project-context.md §线程安全模式).
type RingBuffer[T any] struct {
	mu    sync.Mutex
	buf   []T
	cap   int
	start int // index of the oldest element
	size  int // number of live elements (≤ cap)
}

// NewRingBuffer creates a RingBuffer holding at most capacity elements. A
// capacity < 1 is clamped to 1 so the buffer is always usable.
func NewRingBuffer[T any](capacity int) *RingBuffer[T] {
	if capacity < 1 {
		capacity = 1
	}
	return &RingBuffer[T]{
		buf: make([]T, capacity),
		cap: capacity,
	}
}

// Push appends v. When the buffer is full the oldest element is evicted.
func (r *RingBuffer[T]) Push(v T) {
	r.mu.Lock()
	defer r.mu.Unlock()

	idx := (r.start + r.size) % r.cap
	r.buf[idx] = v
	if r.size < r.cap {
		r.size++
		return
	}
	// Full: overwrite oldest and advance the window.
	r.start = (r.start + 1) % r.cap
}

// Snapshot returns the live elements oldest→newest as a freshly allocated
// slice. The caller may freely mutate the returned slice without affecting the
// buffer's internal storage (deep copy at the slice level).
func (r *RingBuffer[T]) Snapshot() []T {
	r.mu.Lock()
	defer r.mu.Unlock()

	out := make([]T, r.size)
	for i := 0; i < r.size; i++ {
		out[i] = r.buf[(r.start+i)%r.cap]
	}
	return out
}

// Len returns the current number of live elements (0..cap).
func (r *RingBuffer[T]) Len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.size
}
