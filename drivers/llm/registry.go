package llm

import (
	"sort"

	"github.com/rnixai/rnix/internal/xsync"
)

// HealthStatus represents the health state of a provider.
type HealthStatus string

const (
	HealthStatusHealthy   HealthStatus = "healthy"
	HealthStatusUnhealthy HealthStatus = "unhealthy"
	HealthStatusUnchecked HealthStatus = "unchecked"
)

// ProviderStatus represents a provider's name, driver type, and health status.
type ProviderStatus struct {
	Name   string       `json:"name"`
	Driver string       `json:"driver"`
	Health HealthStatus `json:"health"`
}

// DriverRegistry is a thread-safe registry for LLM drivers.
type DriverRegistry struct {
	registry *xsync.Registry[LLMDriver]
	health   *xsync.SyncMap[string, HealthStatus]
}

// NewDriverRegistry creates a new empty DriverRegistry.
func NewDriverRegistry() *DriverRegistry {
	return &DriverRegistry{
		registry: xsync.NewRegistry[LLMDriver](),
		health:   xsync.NewSyncMap[string, HealthStatus](),
	}
}

// Register adds a driver under the given path. Returns an error if the path is already registered.
func (r *DriverRegistry) Register(path string, driver LLMDriver) error {
	if err := r.registry.Register(path, driver); err != nil {
		return err
	}
	r.health.Store(path, HealthStatusUnchecked)
	return nil
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

// SetHealth updates the health status of a named provider.
func (r *DriverRegistry) SetHealth(name string, status HealthStatus) {
	r.health.Store(name, status)
}

// GetHealth returns the health status of a named provider.
// Returns HealthStatusUnchecked if the name is not found.
func (r *DriverRegistry) GetHealth(name string) HealthStatus {
	if status, ok := r.health.Load(name); ok {
		return status
	}
	return HealthStatusUnchecked
}

// HealthStatuses returns all providers with their driver type and health status, sorted by name.
func (r *DriverRegistry) HealthStatuses() []ProviderStatus {
	var statuses []ProviderStatus
	r.registry.Range(func(name string, drv LLMDriver) bool {
		health := r.GetHealth(name)
		driverType := drv.Info().DriverType
		statuses = append(statuses, ProviderStatus{
			Name:   name,
			Driver: driverType,
			Health: health,
		})
		return true
	})
	sort.Slice(statuses, func(i, j int) bool {
		return statuses[i].Name < statuses[j].Name
	})
	return statuses
}
