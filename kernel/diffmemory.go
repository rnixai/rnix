package kernel

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
	mu       sync.RWMutex
	entries  map[string]*DiffMemoryEntry
	maxSize  int
	filePath string // persistence target; empty = pure in-memory (no file writes)
}

// NewDiffMemory creates a new DiffMemory with the given maximum entry count.
// The returned store is pure in-memory: it never reads or writes any file.
func NewDiffMemory(maxSize int) *DiffMemory {
	if maxSize < 1 {
		maxSize = 1
	}
	return &DiffMemory{
		entries: make(map[string]*DiffMemoryEntry),
		maxSize: maxSize,
	}
}

// NewDiffMemoryWithPersistence creates a DiffMemory that persists differentiation
// paths to filePath as append-only JSON Lines (mirroring ReputationStore /
// SynergyMatrix), and replays that file into memory on construction so that
// stem-agent adaptation survives daemon restarts (Story 51.1).
//
// An empty filePath degrades to pure in-memory behaviour (no file writes),
// identical to NewDiffMemory. A missing file is not an error — the store starts
// empty. A corrupt line in the file is skipped (warned, not fatal) so a partially
// written file from a killed process can never block daemon startup (AC8).
func NewDiffMemoryWithPersistence(maxSize int, filePath string) (*DiffMemory, error) {
	if maxSize < 1 {
		maxSize = 1
	}
	dm := &DiffMemory{
		entries:  make(map[string]*DiffMemoryEntry),
		maxSize:  maxSize,
		filePath: filePath,
	}
	if filePath != "" {
		if err := dm.load(); err != nil {
			return nil, err
		}
	}
	return dm, nil
}

// load replays the persistence file into the in-memory map (Story 51.1).
// It is called once during construction (single-threaded, no lock needed).
// Semantics:
//   - Missing file → empty store, nil error (os.IsNotExist, like reputation).
//   - Empty / whitespace-only lines are skipped.
//   - A line that fails json.Unmarshal is skipped with a stderr warning and load
//     continues — deliberately diverging from reputation's "return on first error"
//     because load runs on the daemon startup critical path (AC8).
//   - Duplicate signatures are last-wins (later line overwrites earlier).
//   - After replay, the map is trimmed to maxSize via evictOne (AC5).
func (dm *DiffMemory) load() error {
	f, err := os.Open(dm.filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Bytes()
		if strings.TrimSpace(string(line)) == "" {
			continue
		}
		var e DiffMemoryEntry
		if err := json.Unmarshal(line, &e); err != nil {
			fmt.Fprintf(os.Stderr, "[diffmemory] warn: skip corrupt line: %v\n", err)
			continue
		}
		sig := normalizeIntent(e.Intent)
		copied := make([]string, len(e.Skills))
		copy(copied, e.Skills)
		// last-wins upsert; no eviction here — trim once after full replay so the
		// final retained set reflects every line's hit_count/timestamp.
		dm.entries[sig] = &DiffMemoryEntry{
			Intent:    e.Intent,
			Skills:    copied,
			Timestamp: e.Timestamp,
			HitCount:  e.HitCount,
		}
	}
	if err := scanner.Err(); err != nil {
		// Whatever parsed so far stays in the map; warn but never fail startup.
		fmt.Fprintf(os.Stderr, "[diffmemory] warn: scan persistence file: %v\n", err)
	}

	for len(dm.entries) > dm.maxSize {
		dm.evictOne()
	}
	return nil
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

	entry, ok := dm.entries[sig]
	if ok {
		if !skillsEqual(entry.Skills, skills) {
			copied := make([]string, len(skills))
			copy(copied, skills)
			entry.Skills = copied
		}
		entry.Timestamp = time.Now()
	} else {
		// Evict if at capacity
		if len(dm.entries) >= dm.maxSize {
			dm.evictOne()
		}
		copied := make([]string, len(skills))
		copy(copied, skills)
		entry = &DiffMemoryEntry{
			Intent:    intent,
			Skills:    copied,
			Timestamp: time.Now(),
			HitCount:  1,
		}
		dm.entries[sig] = entry
	}

	// Persist the current snapshot (which carries the HitCount already raised by a
	// preceding Lookup on the hot path) while still holding the write lock, so the
	// append is serialized with the in-memory mutation (AC1, AC9). Best-effort:
	// a write failure is warned, never propagated — Record stays side-effect-free
	// to its callers (spawn.go / tool_exec.go) and must not block the main flow.
	if dm.filePath != "" {
		if err := dm.appendEntry(entry); err != nil {
			fmt.Fprintf(os.Stderr, "[diffmemory] warn: persist record: %v\n", err)
		}
	}
}

// appendEntry serializes a single entry snapshot as one JSON line and appends it
// to the persistence file, creating parent directories and the file as needed.
// Mirrors ReputationStore.RecordResult / SynergyMatrix.RecordCombo exactly.
// Caller must hold dm.mu.
func (dm *DiffMemory) appendEntry(e *DiffMemoryEntry) error {
	if err := os.MkdirAll(filepath.Dir(dm.filePath), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(dm.filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = f.Write(data)
	return err
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
