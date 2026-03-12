package llm

import (
	"fmt"
	"log"
	"strings"

	"github.com/rnixai/rnix/vfs"
)

// DeviceRegisterer abstracts VFS device registration for testability.
// vfs.DeviceRegistry naturally satisfies this interface.
type DeviceRegisterer interface {
	Register(path string, factory vfs.VFSFileFactory) error
}

// CreateDriver creates an LLMDriver instance from a ProviderConfig.
// It dispatches to the appropriate constructor based on cfg.Driver.
// API key handling is deferred to Story 23-4.
func CreateDriver(cfg ProviderConfig) (LLMDriver, error) {
	switch cfg.Driver {
	case DriverClaudeCLI:
		var opts []ClaudeCliOption
		if cfg.DefaultModel != "" {
			opts = append(opts, WithModel(cfg.DefaultModel))
		}
		return NewClaudeCliDriver(opts...), nil

	case DriverCursorCLI:
		var opts []CursorCliOption
		if cfg.DefaultModel != "" {
			opts = append(opts, CursorWithModel(cfg.DefaultModel))
		}
		return NewCursorCliDriver(opts...), nil

	case DriverOpenAICompat:
		var opts []CompatOption
		if cfg.DefaultModel != "" {
			opts = append(opts, WithCompatModel(cfg.DefaultModel))
		}
		return NewOpenAICompatDriver(cfg.Name, cfg.BaseURL, opts...), nil

	default:
		return nil, fmt.Errorf("unsupported driver type: %q", cfg.Driver)
	}
}

// RegisterProviders creates driver instances from config and registers them
// in both the DriverRegistry and VFS DeviceRegistry.
// It uses fail-fast: any error stops registration immediately.
func RegisterProviders(cfg *ProvidersConfig, driverReg *DriverRegistry, devReg DeviceRegisterer) error {
	for _, pc := range cfg.Providers {
		driver, err := CreateDriver(pc)
		if err != nil {
			return fmt.Errorf("provider %q: %w", pc.Name, err)
		}

		if err := driverReg.Register(pc.Name, driver); err != nil {
			return fmt.Errorf("provider %q: driver registry: %w", pc.Name, err)
		}

		vfsPath := "/dev/llm/" + pc.Name
		if err := devReg.Register(vfsPath, FileFactory(driver, vfsPath)); err != nil {
			return fmt.Errorf("provider %q: device registry: %w", pc.Name, err)
		}
	}

	names := driverReg.Names()
	var parts []string
	for _, n := range names {
		parts = append(parts, n+" → /dev/llm/"+n)
	}
	log.Printf("[llm] registered %d providers: %s", len(names), strings.Join(parts, ", "))

	return nil
}
