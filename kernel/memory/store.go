package memory

import (
	"fmt"
)

// MemoryStore wraps a pair of MemoryProviders (global + project) with security
// scanning and capacity management. It is the primary API surface for the memory
// subsystem.
type MemoryStore struct {
	global  *FileMemoryProvider
	project *FileMemoryProvider
	cfg     MemoryConfig
}

// NewMemoryStore creates a new MemoryStore with dual-scope providers.
func NewMemoryStore(globalDir, projectDir string, cfg MemoryConfig) *MemoryStore {
	limits := map[string]int{"memory": cfg.Store.MemoryCharLimit, "user": cfg.Store.UserCharLimit}
	return &MemoryStore{
		global:  NewFileMemoryProvider(globalDir, limits),
		project: NewFileMemoryProvider(projectDir, limits),
		cfg:     cfg,
	}
}

// Load initializes both providers from disk.
func (s *MemoryStore) Load() error {
	if err := s.global.Load(); err != nil {
		return fmt.Errorf("load global memory: %w", err)
	}
	if err := s.project.Load(); err != nil {
		return fmt.Errorf("load project memory: %w", err)
	}
	return nil
}

// resolveProvider returns the provider for the given target.
// "global_memory" → global provider, everything else → project provider.
func (s *MemoryStore) resolveProvider(target string) (*FileMemoryProvider, string) {
	if target == "global_memory" {
		return s.global, "memory"
	}
	// "memory", "user", etc. → project scope
	return s.project, target
}

// Add appends content after security scanning and capacity check.
func (s *MemoryStore) Add(target string, content string) error {
	if result := ScanContent(content); result.Rejected {
		return fmt.Errorf("%s", result.Reason)
	}
	provider, provTarget := s.resolveProvider(target)
	return provider.Add(provTarget, content)
}

// Replace swaps an existing entry after scanning the new content.
func (s *MemoryStore) Replace(target string, old string, new string) error {
	if result := ScanContent(new); result.Rejected {
		return fmt.Errorf("%s", result.Reason)
	}
	provider, provTarget := s.resolveProvider(target)
	return provider.Replace(provTarget, old, new)
}

// Remove deletes an existing entry (no scan needed for deletion).
func (s *MemoryStore) Remove(target string, old string) error {
	provider, provTarget := s.resolveProvider(target)
	return provider.Remove(provTarget, old)
}

// Snapshot returns the full content of a target as a single string.
func (s *MemoryStore) Snapshot(target string) string {
	provider, provTarget := s.resolveProvider(target)
	return provider.Snapshot(provTarget)
}

// Capacity returns used chars and limit for a target.
func (s *MemoryStore) Capacity(target string) (used int, limit int) {
	provider, provTarget := s.resolveProvider(target)
	return provider.Capacity(provTarget)
}
