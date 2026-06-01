package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// RegistryEntry maps a project data ID to its absolute filesystem path.
type RegistryEntry struct {
	ID          string `json:"id"`
	ProjectPath string `json:"project_path"`
	LastSeen    string `json:"last_seen"`
}

// ProjectRegistry maintains a mapping from ProjectDataID to absolute project
// paths in a projects.json file under the global data directory.
type ProjectRegistry struct {
	mu      sync.RWMutex
	path    string
	entries map[string]RegistryEntry
}

// NewProjectRegistry creates a registry backed by the given file path.
func NewProjectRegistry(path string) *ProjectRegistry {
	return &ProjectRegistry{
		path:    path,
		entries: make(map[string]RegistryEntry),
	}
}

// Load reads the registry from disk. Missing file is not an error.
func (r *ProjectRegistry) Load() error {
	r.mu.Lock()
	defer r.mu.Unlock()

	data, err := os.ReadFile(r.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var entries []RegistryEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return err
	}

	r.entries = make(map[string]RegistryEntry, len(entries))
	for _, e := range entries {
		r.entries[e.ID] = e
	}
	return nil
}

// Register adds or updates a project in the registry. Returns the project
// data ID. Idempotent — repeated calls for the same path only update LastSeen.
func (r *ProjectRegistry) Register(projectPath string) (string, error) {
	if projectPath == "" {
		return "", nil
	}

	id := ProjectDataID(projectPath)

	r.mu.Lock()
	defer r.mu.Unlock()

	r.entries[id] = RegistryEntry{
		ID:          id,
		ProjectPath: projectPath,
		LastSeen:    time.Now().UTC().Format(time.RFC3339),
	}

	return id, r.persistLocked()
}

// Lookup returns the project path for a given data ID.
func (r *ProjectRegistry) Lookup(projectDataID string) (string, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	e, ok := r.entries[projectDataID]
	if !ok {
		return "", false
	}
	return e.ProjectPath, true
}

// List returns all registered entries.
func (r *ProjectRegistry) List() []RegistryEntry {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]RegistryEntry, 0, len(r.entries))
	for _, e := range r.entries {
		out = append(out, e)
	}
	return out
}

func (r *ProjectRegistry) persistLocked() error {
	entries := make([]RegistryEntry, 0, len(r.entries))
	for _, e := range r.entries {
		entries = append(entries, e)
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}

	dir := filepath.Dir(r.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	tmp := r.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, r.path)
}
