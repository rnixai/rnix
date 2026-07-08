package memory

import (
	"fmt"
	"path/filepath"
	"sync"
	"time"

	"github.com/rnixai/rnix/internal/config"
)

// MemoryStore wraps a global MemoryProvider plus a set of lazily-created
// per-project providers, with security scanning and capacity management. It is
// the primary API surface for the memory subsystem.
//
// "global_memory" is user-scoped and lives under the daemon's global memory
// dir. Project-scoped targets ("memory", "user") are resolved per access by
// the caller's project dir, landing under {dataDir}/projects/{id}/memory —
// the same per-project root used for steps/events (see ResolveStepBaseDir).
// projectDir is threaded per call (not stored on the struct) because the store
// is shared across concurrent processes belonging to different projects.
type MemoryStore struct {
	global  *FileMemoryProvider
	cfg     MemoryConfig
	dataDir string         // per-project data root; project memory dirs derive from it
	limits  map[string]int // captured for lazy per-project provider creation

	mu       sync.RWMutex
	projects map[string]*FileMemoryProvider // keyed by resolved memory baseDir

	// recallMu guards recallIndex and serializes the snapshot→index sequence
	// in notifyRecallIndex so concurrent writes to the same target cannot
	// commit a stale snapshot last (lost-update).
	recallMu    sync.Mutex
	recallIndex *RecallIndex // nil when recall is disabled
}

// NewMemoryStore creates a new MemoryStore. globalDir backs global memory;
// dataDir is the per-project data root under which each project's memory dir
// is resolved (mirrors the steps/events layout). The project provider is no
// longer fixed at construction — it is resolved per access by projectDir.
func NewMemoryStore(globalDir, dataDir string, cfg MemoryConfig) *MemoryStore {
	limits := map[string]int{"memory": cfg.Store.MemoryCharLimit, "user": cfg.Store.UserCharLimit}
	return &MemoryStore{
		global:   NewFileMemoryProvider(globalDir, limits),
		cfg:      cfg,
		dataDir:  dataDir,
		limits:   limits,
		projects: make(map[string]*FileMemoryProvider),
	}
}

// Load initializes the global provider from disk. Per-project providers are
// loaded lazily on first access (see providerForProject).
func (s *MemoryStore) Load() error {
	if err := s.global.Load(); err != nil {
		return fmt.Errorf("load global memory: %w", err)
	}
	return nil
}

// providerForProject returns the project-scoped provider for projectDir,
// lazily creating and Load()-ing it on first use. An empty projectDir falls
// back to the global data dir (mirrors ResolveStepBaseDir's nil-ProjectConfig
// branch). If dataDir is unset (degenerate/test), it falls back to the global
// provider so writes never target an empty/relative path. Providers are cached
// by resolved baseDir so concurrent processes for the same project share one
// instance (its own mutex + flock serialize writes).
func (s *MemoryStore) providerForProject(projectDir string) *FileMemoryProvider {
	var baseRoot string
	if projectDir == "" {
		baseRoot = config.GlobalDataDir(s.dataDir)
	} else {
		baseRoot = config.ProjectDataDir(s.dataDir, projectDir)
	}
	if baseRoot == "" {
		return s.global
	}
	baseDir := filepath.Join(baseRoot, "memory")

	s.mu.RLock()
	p, ok := s.projects[baseDir]
	s.mu.RUnlock()
	if ok {
		return p
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if p, ok = s.projects[baseDir]; ok { // re-check under write lock
		return p
	}
	p = NewFileMemoryProvider(baseDir, s.limits)
	_ = p.Load() // best-effort; missing files yield empty entries
	s.projects[baseDir] = p
	return p
}

// resolveProvider returns the provider and concrete file target for a logical
// target. "global_memory" maps to the global provider's "memory" file;
// everything else ("memory", "user", ...) is project-scoped and resolved by
// projectDir.
func (s *MemoryStore) resolveProvider(target, projectDir string) (*FileMemoryProvider, string) {
	if target == "global_memory" {
		return s.global, "memory"
	}
	// "memory", "user", etc. → project scope
	return s.providerForProject(projectDir), target
}

// Add appends content after security scanning and capacity check.
func (s *MemoryStore) Add(target, content, projectDir string) error {
	if result := ScanContent(content); result.Rejected {
		return fmt.Errorf("%s", result.Reason)
	}
	provider, provTarget := s.resolveProvider(target, projectDir)
	if err := provider.Add(provTarget, content); err != nil {
		return err
	}
	if indexableTargets[target] {
		s.notifyRecallIndex(target, projectDir)
	}
	return nil
}

// Replace swaps an existing entry after scanning the new content.
func (s *MemoryStore) Replace(target, old, new, projectDir string) error {
	if result := ScanContent(new); result.Rejected {
		return fmt.Errorf("%s", result.Reason)
	}
	provider, provTarget := s.resolveProvider(target, projectDir)
	if err := provider.Replace(provTarget, old, new); err != nil {
		return err
	}
	if indexableTargets[target] {
		s.notifyRecallIndex(target, projectDir)
	}
	return nil
}

// Remove deletes an existing entry (no scan needed for deletion).
func (s *MemoryStore) Remove(target, old, projectDir string) error {
	provider, provTarget := s.resolveProvider(target, projectDir)
	if err := provider.Remove(provTarget, old); err != nil {
		return err
	}
	if indexableTargets[target] {
		s.notifyRecallIndex(target, projectDir)
	}
	return nil
}

// Snapshot returns the full content of a target as a single string.
func (s *MemoryStore) Snapshot(target, projectDir string) string {
	provider, provTarget := s.resolveProvider(target, projectDir)
	return provider.Snapshot(provTarget)
}

// SetRecallIndex injects the recall index for live notification on writes.
// Nil disables notifications (recall disabled path).
func (s *MemoryStore) SetRecallIndex(ri *RecallIndex) {
	s.recallMu.Lock()
	defer s.recallMu.Unlock()
	s.recallIndex = ri
}

// memoryTargets that should trigger recall index updates (fail-closed whitelist).
var indexableTargets = map[string]bool{
	"memory":        true,
	"global_memory": true,
}

// notifyRecallIndex re-indexes the provider's entries into the recall index.
// Called after successful Add/Replace/Remove on indexable targets. The whole
// snapshot→index sequence runs under recallMu: snapshots always reflect the
// provider state at notify time, so a delayed notifier cannot overwrite a
// newer snapshot with a stale one (IndexMemorySource is whole-source replace).
func (s *MemoryStore) notifyRecallIndex(target, projectDir string) {
	s.recallMu.Lock()
	defer s.recallMu.Unlock()
	if s.recallIndex == nil {
		return
	}
	provider, provTarget := s.resolveProvider(target, projectDir)
	entries := provider.entriesSnapshot(provTarget)
	key := s.recallSourceKey(target, projectDir)
	s.recallIndex.IndexMemorySource(key, entries, time.Now())
}

// recallSourceKey derives the recall index key for a given target+projectDir
// via the shared MemorySourceKey* helpers also used by the startup wiring
// (cmd/rnix/main.go), keeping both derivation paths identical.
func (s *MemoryStore) recallSourceKey(target, projectDir string) string {
	if target == "global_memory" {
		return MemorySourceKeyGlobal
	}
	var baseRoot string
	if projectDir == "" {
		baseRoot = config.GlobalDataDir(s.dataDir)
	} else {
		baseRoot = config.ProjectDataDir(s.dataDir, projectDir)
	}
	if baseRoot == "" {
		return MemorySourceKeyGlobal
	}
	return MemorySourceKeyForBaseDir(baseRoot)
}

// Capacity returns used chars and limit for a target.
func (s *MemoryStore) Capacity(target, projectDir string) (used int, limit int) {
	provider, provTarget := s.resolveProvider(target, projectDir)
	return provider.Capacity(provTarget)
}
