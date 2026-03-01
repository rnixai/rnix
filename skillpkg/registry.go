package skillpkg

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/goccy/go-yaml"
)

const registryFileName = ".registry.yaml"

// registryData is the on-disk YAML structure for the local skill registry.
type registryData struct {
	Skills map[string]RegistryEntry `yaml:"skills"`
}

// LocalRegistry manages the local skill registry stored in the skills directory.
type LocalRegistry struct {
	basePath string // path to lib/skills/
}

// NewLocalRegistry creates a new LocalRegistry rooted at basePath.
func NewLocalRegistry(basePath string) *LocalRegistry {
	return &LocalRegistry{basePath: basePath}
}

func (r *LocalRegistry) registryPath() string {
	return filepath.Join(r.basePath, registryFileName)
}

func (r *LocalRegistry) load() (*registryData, error) {
	data, err := os.ReadFile(r.registryPath())
	if err != nil {
		if os.IsNotExist(err) {
			return &registryData{Skills: make(map[string]RegistryEntry)}, nil
		}
		return nil, fmt.Errorf("read registry: %w", err)
	}

	var reg registryData
	if err := yaml.Unmarshal(data, &reg); err != nil {
		return nil, fmt.Errorf("parse registry: %w", err)
	}
	if reg.Skills == nil {
		reg.Skills = make(map[string]RegistryEntry)
	}
	return &reg, nil
}

func (r *LocalRegistry) save(reg *registryData) error {
	data, err := yaml.Marshal(reg)
	if err != nil {
		return fmt.Errorf("marshal registry: %w", err)
	}

	if err := os.MkdirAll(r.basePath, 0o755); err != nil {
		return fmt.Errorf("create registry directory: %w", err)
	}

	if err := os.WriteFile(r.registryPath(), data, 0o644); err != nil {
		return fmt.Errorf("write registry: %w", err)
	}
	return nil
}

// Add adds or updates a registry entry for the named skill.
func (r *LocalRegistry) Add(entry RegistryEntry) error {
	reg, err := r.load()
	if err != nil {
		return err
	}

	reg.Skills[entry.Name] = RegistryEntry{
		Version:     entry.Version,
		InstalledAt: entry.InstalledAt,
		Source:      entry.Source,
		Checksum:    entry.Checksum,
	}

	return r.save(reg)
}

// Get retrieves a registry entry by skill name.
func (r *LocalRegistry) Get(name string) (*RegistryEntry, error) {
	reg, err := r.load()
	if err != nil {
		return nil, err
	}

	entry, ok := reg.Skills[name]
	if !ok {
		return nil, nil
	}
	entry.Name = name
	return &entry, nil
}

// List returns all registered skills.
func (r *LocalRegistry) List() ([]RegistryEntry, error) {
	reg, err := r.load()
	if err != nil {
		return nil, err
	}

	var entries []RegistryEntry
	for name, entry := range reg.Skills {
		entry.Name = name
		entries = append(entries, entry)
	}
	return entries, nil
}

// Remove deletes a registry entry by skill name.
func (r *LocalRegistry) Remove(name string) error {
	reg, err := r.load()
	if err != nil {
		return err
	}

	if _, ok := reg.Skills[name]; !ok {
		return fmt.Errorf("skill %q not found in registry", name)
	}

	delete(reg.Skills, name)
	return r.save(reg)
}
