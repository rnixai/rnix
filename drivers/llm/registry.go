package llm

import (
	"sort"

	"github.com/rnixai/rnix/internal/xsync"
)

// DriverRegistry is a thread-safe registry for LLM drivers.
type DriverRegistry struct {
	registry *xsync.Registry[LLMDriver]
}

// NewDriverRegistry creates a new empty DriverRegistry.
func NewDriverRegistry() *DriverRegistry {
	return &DriverRegistry{
		registry: xsync.NewRegistry[LLMDriver](),
	}
}

// Register adds a driver under the given path. Returns an error if the path is already registered.
func (r *DriverRegistry) Register(path string, driver LLMDriver) error {
	return r.registry.Register(path, driver)
}

// Get retrieves a driver by path. The boolean indicates whether the path was found.
func (r *DriverRegistry) Get(path string) (LLMDriver, bool) {
	return r.registry.Get(path)
}

// Names returns a sorted list of all registered driver names.
func (r *DriverRegistry) Names() []string {
	var names []string
	r.registry.Range(func(name string, _ LLMDriver) bool {
		names = append(names, name)
		return true
	})
	sort.Strings(names)
	return names
}

// Len returns the number of registered drivers.
func (r *DriverRegistry) Len() int {
	count := 0
	r.registry.Range(func(_ string, _ LLMDriver) bool {
		count++
		return true
	})
	return count
}
