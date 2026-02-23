package xsync

import (
	"fmt"
	"sync"
)

// Registry is a thread-safe named registry for items of type T.
type Registry[T any] struct {
	mu    sync.RWMutex
	items map[string]T
}

// NewRegistry creates a new empty Registry.
func NewRegistry[T any]() *Registry[T] {
	return &Registry[T]{items: make(map[string]T)}
}

// Register adds an item under the given name. Returns an error if the name is already registered.
func (r *Registry[T]) Register(name string, item T) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.items[name]; exists {
		return fmt.Errorf("already registered: %s", name)
	}
	r.items[name] = item
	return nil
}

// Get retrieves an item by name. The boolean indicates whether the name was found.
func (r *Registry[T]) Get(name string) (T, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	item, ok := r.items[name]
	return item, ok
}

// List returns a slice of all registered items.
func (r *Registry[T]) List() []T {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]T, 0, len(r.items))
	for _, item := range r.items {
		result = append(result, item)
	}
	return result
}
