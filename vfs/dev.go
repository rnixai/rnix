package vfs

import (
	"errors"
	"fmt"
	"strings"

	"github.com/gonewx/crux/internal/xsync"
)

// errDeviceNotFound is a sentinel error indicating no device matched the path.
var errDeviceNotFound = errors.New("device not found")

// DeviceRegistry manages device path to factory mappings.
type DeviceRegistry struct {
	registry *xsync.Registry[VFSFileFactory]
}

// NewDeviceRegistry creates a new empty DeviceRegistry.
func NewDeviceRegistry() *DeviceRegistry {
	return &DeviceRegistry{
		registry: xsync.NewRegistry[VFSFileFactory](),
	}
}

// Register registers a device factory at the given path.
// Returns an error if the path is already registered.
func (d *DeviceRegistry) Register(path string, factory VFSFileFactory) error {
	return d.registry.Register(path, factory)
}

// Unregister removes a device factory at the given path.
// Returns an error if the path is not registered.
func (d *DeviceRegistry) Unregister(path string) error {
	return d.registry.Unregister(path)
}

// Open looks up the factory for the given path and creates a VFSFile.
// Supports exact match and longest-prefix match.
func (d *DeviceRegistry) Open(path string, flags OpenFlag) (VFSFile, error) {
	// Try exact match first.
	factory, ok := d.registry.Get(path)
	if ok {
		return factory("", flags)
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
		return factory(subpath, flags)
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
