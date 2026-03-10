package kernel

import (
	"sort"
	"strings"
	"sync"
	"time"
)

// DiffMemoryEntry records a single differentiation path for later reuse.
type DiffMemoryEntry struct {
	Intent    string    `json:"intent"`
	Skills    []string  `json:"skills"`
	Timestamp time.Time `json:"timestamp"`
	HitCount  int       `json:"hit_count"`
}

// DiffMemory stores and retrieves differentiation paths keyed by intent signatures.
type DiffMemory struct {
	mu      sync.RWMutex
	entries map[string]*DiffMemoryEntry
	maxSize int
}

// NewDiffMemory creates a new DiffMemory with the given maximum entry count.
func NewDiffMemory(maxSize int) *DiffMemory {
	if maxSize < 1 {
		maxSize = 1
	}
	return &DiffMemory{
		entries: make(map[string]*DiffMemoryEntry),
		maxSize: maxSize,
	}
}

// Record stores a differentiation path for the given intent.
// If the entry already exists with the same skills, only Timestamp is updated.
// If the entry exists with different skills, the skill list is replaced (latest wins).
// HitCount is only incremented by Lookup (actual reuse), not by Record.
// When maxSize is exceeded, the entry with the lowest HitCount (and oldest Timestamp as tiebreaker) is evicted.
func (dm *DiffMemory) Record(intent string, skills []string) {
	sig := normalizeIntent(intent)

	dm.mu.Lock()
	defer dm.mu.Unlock()

	if entry, ok := dm.entries[sig]; ok {
		if skillsEqual(entry.Skills, skills) {
			entry.Timestamp = time.Now()
		} else {
			copied := make([]string, len(skills))
			copy(copied, skills)
			entry.Skills = copied
			entry.Timestamp = time.Now()
		}
		return
	}

	// Evict if at capacity
	if len(dm.entries) >= dm.maxSize {
		dm.evictOne()
	}

	copied := make([]string, len(skills))
	copy(copied, skills)
	dm.entries[sig] = &DiffMemoryEntry{
		Intent:    intent,
		Skills:    copied,
		Timestamp: time.Now(),
		HitCount:  1,
	}
}

// Lookup retrieves the skill list for a given intent.
// Returns the skills and true if found, or nil and false if not found.
// A successful lookup increments the entry's HitCount.
func (dm *DiffMemory) Lookup(intent string) ([]string, bool) {
	sig := normalizeIntent(intent)

	dm.mu.Lock()
	defer dm.mu.Unlock()

	entry, ok := dm.entries[sig]
	if !ok {
		return nil, false
	}

	entry.HitCount++

	copied := make([]string, len(entry.Skills))
	copy(copied, entry.Skills)
	return copied, true
}

// normalizeIntent converts an intent string to a canonical signature
// by tokenizing (reusing stem.go's tokenize), sorting, and joining with spaces.
func normalizeIntent(intent string) string {
	tokens := tokenize(intent)
	sort.Strings(tokens)
	return strings.Join(tokens, " ")
}

// evictOne removes the entry with the lowest HitCount, using oldest Timestamp as tiebreaker.
// Caller must hold dm.mu write lock.
func (dm *DiffMemory) evictOne() {
	var evictKey string
	var evictEntry *DiffMemoryEntry

	for key, entry := range dm.entries {
		if evictEntry == nil {
			evictKey = key
			evictEntry = entry
			continue
		}
		if entry.HitCount < evictEntry.HitCount ||
			(entry.HitCount == evictEntry.HitCount && entry.Timestamp.Before(evictEntry.Timestamp)) {
			evictKey = key
			evictEntry = entry
		}
	}

	if evictKey != "" {
		delete(dm.entries, evictKey)
	}
}

// skillsEqual returns true if two skill slices contain the same elements in the same order.
func skillsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
