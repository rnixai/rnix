package memory

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// MemoryProvider is the storage backend interface for memory files.
//
// Target values: "memory" → MEMORY.md, "user" → USER.md.
//
// Phase 1-3 (current): FileMemoryProvider implements this interface using local
// filesystem files with per-target capacity limits.
//
// Phase 4 evolution (Architecture Decision 38): MemoryProvider may be backed by
// an external service via MCP stdio transport, reusing the existing /dev/mcp/*
// driver infrastructure. The interface contract remains unchanged; only the
// underlying storage mechanism changes from local files to remote RPC.
type MemoryProvider interface {
	Load() error
	Add(target string, content string) error
	Replace(target string, old string, new string) error
	Remove(target string, old string) error
	Snapshot(target string) string
	Capacity(target string) (used int, limit int)
}

// entrySeparator is the § character used to delimit entries in MEMORY.md.
const entrySeparator = "§"

// entrySepRuneLen is the rune count of entrySeparator (always 1).
const entrySepRuneLen = 1

// FileMemoryProvider implements MemoryProvider using local filesystem files.
// Each target maps to a file in baseDir (e.g. baseDir/MEMORY.md).
type FileMemoryProvider struct {
	mu           sync.Mutex
	baseDir      string
	targetLimits map[string]int   // per-target char limits: {"memory": 4096, "user": 2048}
	entries      map[string][]string // target → list of entries
}

// NewFileMemoryProvider creates a provider backed by files in baseDir.
// targetLimits maps target names to their char limits (e.g. {"memory": 4096, "user": 2048}).
func NewFileMemoryProvider(baseDir string, targetLimits map[string]int) *FileMemoryProvider {
	return &FileMemoryProvider{
		baseDir:      baseDir,
		targetLimits: targetLimits,
		entries:      make(map[string][]string),
	}
}

// limitFor returns the char limit for a target. Falls back to "memory" limit,
// then to a hardcoded default of 4096.
func (p *FileMemoryProvider) limitFor(target string) int {
	if limit, ok := p.targetLimits[target]; ok {
		return limit
	}
	if limit, ok := p.targetLimits["memory"]; ok {
		return limit
	}
	return 4096
}

func (p *FileMemoryProvider) targetFile(target string) string {
	switch target {
	case "user":
		return filepath.Join(p.baseDir, "USER.md")
	default: // "memory"
		return filepath.Join(p.baseDir, "MEMORY.md")
	}
}

// Load reads existing memory files from disk.
func (p *FileMemoryProvider) Load() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	for _, target := range []string{"memory", "user"} {
		path := p.targetFile(target)
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				p.entries[target] = nil
				continue
			}
			return fmt.Errorf("load %s: %w", target, err)
		}
		p.entries[target] = parseEntries(string(data))
	}
	return nil
}

// Add appends a new entry to the target.
func (p *FileMemoryProvider) Add(target string, content string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Capacity check
	limit := p.limitFor(target)
	current := p.usedLocked(target)
	addition := len([]rune(content)) + entrySepRuneLen + 1 // §content\n
	if current+addition > limit {
		return fmt.Errorf("memory capacity limit exceeded: used=%d, limit=%d, needed=%d additional chars", current, limit, addition)
	}

	p.entries[target] = append(p.entries[target], content)
	return p.persistLocked(target)
}

// Replace swaps old entry for new entry.
func (p *FileMemoryProvider) Replace(target string, old string, new string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	idx := p.findEntry(target, old)
	if idx < 0 {
		return fmt.Errorf("entry not found for Replace: %q", truncateForError(old))
	}

	// Capacity check: new entry may be larger than old
	oldLen := len([]rune(old)) + entrySepRuneLen + 1
	newLen := len([]rune(new)) + entrySepRuneLen + 1
	if delta := newLen - oldLen; delta > 0 {
		limit := p.limitFor(target)
		current := p.usedLocked(target)
		if current+delta > limit {
			return fmt.Errorf("memory capacity limit exceeded: used=%d, limit=%d, needed=%d additional chars", current, limit, delta)
		}
	}

	p.entries[target][idx] = new
	return p.persistLocked(target)
}

// Remove deletes the matching entry.
func (p *FileMemoryProvider) Remove(target string, old string) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	idx := p.findEntry(target, old)
	if idx < 0 {
		return fmt.Errorf("entry not found for Remove: %q", truncateForError(old))
	}
	p.entries[target] = append(p.entries[target][:idx], p.entries[target][idx+1:]...)
	return p.persistLocked(target)
}

// Snapshot returns all entries joined by newlines (no § delimiters).
func (p *FileMemoryProvider) Snapshot(target string) string {
	p.mu.Lock()
	defer p.mu.Unlock()

	entries := p.entries[target]
	if len(entries) == 0 {
		return ""
	}
	return strings.Join(entries, "\n")
}

// Capacity returns current used chars and the limit.
func (p *FileMemoryProvider) Capacity(target string) (used int, limit int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.usedLocked(target), p.limitFor(target)
}

// usedLocked computes total chars (including § delimiters and newlines). Must hold mu.
func (p *FileMemoryProvider) usedLocked(target string) int {
	total := 0
	for _, e := range p.entries[target] {
		total += len([]rune(e)) + entrySepRuneLen + 1 // §entry\n
	}
	return total
}

func (p *FileMemoryProvider) findEntry(target string, content string) int {
	for i, e := range p.entries[target] {
		if e == content {
			return i
		}
	}
	return -1
}

// persistLocked writes the current entries to disk with flock. Must hold mu.
func (p *FileMemoryProvider) persistLocked(target string) error {
	path := p.targetFile(target)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir for %s: %w", target, err)
	}

	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return fmt.Errorf("open %s: %w", target, err)
	}
	defer f.Close()

	if err := flockExclusive(f.Fd()); err != nil {
		return fmt.Errorf("flock %s: %w", target, err)
	}
	defer flockUnlock(f.Fd()) //nolint:errcheck

	var sb strings.Builder
	for _, e := range p.entries[target] {
		sb.WriteString(entrySeparator)
		sb.WriteString(e)
		sb.WriteByte('\n')
	}
	_, err = f.WriteString(sb.String())
	return err
}

// parseEntries splits a §-delimited file into entries.
func parseEntries(data string) []string {
	if data == "" {
		return nil
	}
	var entries []string
	for line := range strings.SplitSeq(data, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if content, ok := strings.CutPrefix(line, entrySeparator); ok {
			entries = append(entries, content)
		}
	}
	return entries
}

func truncateForError(s string) string {
	runes := []rune(s)
	if len(runes) > 40 {
		return string(runes[:40]) + "..."
	}
	return s
}
