package web

import (
	"fmt"
	"slices"
	"sync"

	"github.com/rnixai/rnix/internal/types"
)

// SearchBackendRegistry holds named search backends with read/write isolation.
// A daemon constructs one registry at startup; WebDriver references it for
// dispatching web_search calls to the chosen backend.
//
// The zero value is not usable — always construct via NewSearchBackendRegistry.
type SearchBackendRegistry struct {
	mu          sync.RWMutex
	backends    map[string]SearchBackend
	defaultName string
}

// NewSearchBackendRegistry returns an empty registry ready for Register calls.
func NewSearchBackendRegistry() *SearchBackendRegistry {
	return &SearchBackendRegistry{backends: make(map[string]SearchBackend)}
}

// Register adds (or replaces) a backend under the given name.
// Nil backends are silently ignored to prevent nil-pointer panics in Default().
func (r *SearchBackendRegistry) Register(name string, b SearchBackend) {
	if b == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.backends[name] = b
}

// Get fetches a backend by name.
func (r *SearchBackendRegistry) Get(name string) (SearchBackend, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	b, ok := r.backends[name]
	return b, ok
}

// SetDefault marks one backend as the default. The named backend must already
// have been registered.
func (r *SearchBackendRegistry) SetDefault(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.backends[name]; !ok {
		return fmt.Errorf("registry: cannot set default to %q: not registered", name)
	}
	r.defaultName = name
	return nil
}

// DefaultName returns the configured default backend name and whether one is set.
func (r *SearchBackendRegistry) DefaultName() (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.defaultName == "" {
		return "", false
	}
	return r.defaultName, true
}

// Default returns the configured default backend or an "unavailable" DriverError
// when no backend is registered yet.
func (r *SearchBackendRegistry) Default() (SearchBackend, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.defaultName == "" {
		return nil, ErrSearchNotConfigured()
	}
	b, ok := r.backends[r.defaultName]
	if !ok {
		return nil, &types.DriverError{
			Op:     "Search",
			Device: "/dev/web",
			Err:    fmt.Errorf("registry: default backend %q registered but missing", r.defaultName),
			Code:   types.ErrInternal,
		}
	}
	return b, nil
}

// Names returns the sorted list of registered backend names.
func (r *SearchBackendRegistry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]string, 0, len(r.backends))
	for n := range r.backends {
		out = append(out, n)
	}
	// stable ordering for log/test determinism
	sortStrings(out)
	return out
}

// Len reports the number of registered backends.
func (r *SearchBackendRegistry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.backends)
}

func sortStrings(s []string) {
	slices.Sort(s)
}
