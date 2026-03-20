package vfs

import (
	"errors"
	"fmt"
	"strings"

	"github.com/rnixai/rnix/internal/xsync"
)

// errDeviceNotFound is a sentinel error indicating no device matched the path.
var errDeviceNotFound = errors.New("device not found")

// DeviceRegistry manages device path to factory mappings.
type DeviceRegistry struct {
	registry  *xsync.Registry[VFSFileFactory]
	driverMap *xsync.SyncMap[string, any] // parallel map: path → driver object
}

// NewDeviceRegistry creates a new empty DeviceRegistry.
func NewDeviceRegistry() *DeviceRegistry {
	return &DeviceRegistry{
		registry:  xsync.NewRegistry[VFSFileFactory](),
		driverMap: xsync.NewSyncMap[string, any](),
	}
}

// Register registers a device factory at the given path.
// Returns an error if the path is already registered.
func (d *DeviceRegistry) Register(path string, factory VFSFileFactory) error {
	return d.registry.Register(path, factory)
}

// RegisterWithDriver registers a device factory and its associated driver object.
// The driver can later be retrieved via GetDriver for ToolDescriptor discovery.
func (d *DeviceRegistry) RegisterWithDriver(path string, factory VFSFileFactory, driver any) error {
	if err := d.registry.Register(path, factory); err != nil {
		return err
	}
	d.driverMap.Store(path, driver)
	return nil
}

// GetDriver returns the driver object registered at the given path.
func (d *DeviceRegistry) GetDriver(path string) (any, bool) {
	return d.driverMap.Load(path)
}

// RangeDrivers iterates over all registered drivers.
// The callback receives the device path and driver object.
// Return false from fn to stop iteration.
func (d *DeviceRegistry) RangeDrivers(fn func(path string, driver any) bool) {
	d.driverMap.Range(fn)
}

// Unregister removes a device factory at the given path.
// Returns an error if the path is not registered.
func (d *DeviceRegistry) Unregister(path string) error {
	d.driverMap.Delete(path)
	return d.registry.Unregister(path)
}

// Open looks up the factory for the given path and creates a VFSFile.
// Supports exact match and longest-prefix match.
func (d *DeviceRegistry) Open(path string, flags OpenFlag, workDir string) (VFSFile, error) {
	// Try exact match first.
	factory, ok := d.registry.Get(path)
	if ok {
		return factory("", flags, workDir)
	}

	// Try longest prefix match.
	bestPrefix := ""
	d.registry.Range(func(registered string, f VFSFileFactory) bool {
		if strings.HasPrefix(path, registered+"/") && len(registered) > len(bestPrefix) {
			bestPrefix = registered
			factory = f
		}
		return true
	})

	if bestPrefix != "" {
		subpath := strings.TrimPrefix(path, bestPrefix)
		return factory(subpath, flags, workDir)
	}

	return nil, fmt.Errorf("%w: %s", errDeviceNotFound, path)
}

// Stat returns file metadata for the given path.
func (d *DeviceRegistry) Stat(path string) (FileStat, error) {
	// Check exact match.
	_, ok := d.registry.Get(path)
	if ok {
		return FileStat{
			Name:       path,
			IsDevice:   true,
			DevicePath: path,
		}, nil
	}

	// Check prefix match.
	found := false
	bestPrefix := ""
	d.registry.Range(func(registered string, _ VFSFileFactory) bool {
		if strings.HasPrefix(path, registered+"/") && len(registered) > len(bestPrefix) {
			bestPrefix = registered
			found = true
		}
		return true
	})

	if found {
		return FileStat{
			Name:       path,
			IsDevice:   true,
			DevicePath: bestPrefix,
		}, nil
	}

	return FileStat{}, fmt.Errorf("%w: %s", errDeviceNotFound, path)
}
