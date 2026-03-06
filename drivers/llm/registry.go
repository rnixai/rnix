package llm

import (
	"github.com/usecrux/crux/internal/xsync"
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
