package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"maps"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"github.com/rnixai/rnix/internal/jsonl"
)

// RecallResult is a search result returned to callers of Search and SummarizeRecallResults.
type RecallResult struct {
	UUID      string    `json:"uuid"`
	Summary   string    `json:"summary"`
	Timestamp time.Time `json:"timestamp"`
	Source    string    `json:"source,omitempty"`
}

// recallToolDefResult holds VFS device ToolDef metadata for /dev/memory/recall.
type recallToolDefResult struct {
	IsReadOnly        bool
	IsConcurrencySafe bool
	IsDestructive     bool
	ShouldDefer       bool
	SearchHint        string
}

// posting is an entry in the inverted index pointing to a document.
type posting struct {
	UUID string
	Step int
	Time time.Time // for temporal sorting
}

// docEntry stores enrichment data for a single step within a process.
type docEntry struct {
	Step    int       `json:"step"`
	Time    time.Time `json:"time"`
	Summary string    `json:"summary"`
	Action  string    `json:"action"`
}

// SearchResult is a single search hit used internally with detailed step info.
type SearchResult struct {
	UUID    string    `json:"uuid"`
	Step    int       `json:"step"`
	Time    time.Time `json:"time"`
	Summary string    `json:"summary"`
	Action  string    `json:"action"`
	Score   int       `json:"score"`
}

// persistedIndex is the JSON representation for SaveIndex/LoadIndex.
type persistedIndex struct {
	Index   map[string][]posting  `json:"index"`
	Docs    map[string][]docEntry `json:"docs"`
	Indexed map[string]bool       `json:"indexed"`
}

// MemorySourceKeyGlobal is the recall-index source key for the global
// MEMORY.md backing the global_memory target ({globalDir}/memory/MEMORY.md).
const MemorySourceKeyGlobal = "memory:global"

// MemorySourceKeyForBaseDir derives the recall-index source key for a project
// data directory's MEMORY.md ({baseDir}/memory/MEMORY.md). Startup wiring
// (cmd/rnix/main.go) and runtime notification (MemoryStore.recallSourceKey)
// must both derive keys through this function so the same file never appears
// under two different sources.
func MemorySourceKeyForBaseDir(baseDir string) string {
	return "memory:project:" + baseDir
}

// memoryEntry holds a single § entry and its pre-computed token set.
type memoryEntry struct {
	text   string
	tokens map[string]bool
}

// memorySource holds all entries from one MEMORY.md file.
type memorySource struct {
	entries []memoryEntry
	ts      time.Time
}

// RecallIndex is an in-memory inverted index over process step records.
// It supports keyword search across all indexed processes.
//
// Thread-safe: all mutations and queries are protected by mu.
type RecallIndex struct {
	mu        sync.RWMutex
	index     map[string][]posting  // term → postings
	docs      map[string][]docEntry // uuid → doc entries
	indexed   map[string]bool       // uuid → already indexed (idempotency)
	ready     atomic.Bool
	readyCh   chan struct{} // closed when initial build completes
	readyOnce sync.Once     // protects readyCh from double-close

	memSources map[string]*memorySource // key → MEMORY.md source (parallel to session index)
}

// NewRecallIndex creates a new empty RecallIndex.
func NewRecallIndex() *RecallIndex {
	return &RecallIndex{
		index:      make(map[string][]posting),
		docs:       make(map[string][]docEntry),
		indexed:    make(map[string]bool),
		readyCh:    make(chan struct{}),
		memSources: make(map[string]*memorySource),
	}
}

// Ready returns true if the initial index build is complete.
func (ri *RecallIndex) Ready() bool {
	return ri.ready.Load()
}

// WaitReady blocks until the index build is complete or timeout elapses.
func (ri *RecallIndex) WaitReady(timeout time.Duration) bool {
	select {
	case <-ri.readyCh:
		return true
	case <-time.After(timeout):
		return false
	}
}

// ProcessCount returns the number of indexed processes.
func (ri *RecallIndex) ProcessCount() int {
	ri.mu.RLock()
	defer ri.mu.RUnlock()
	return len(ri.indexed)
}

// markReady marks the index as ready for queries. Safe to call multiple times.
func (ri *RecallIndex) markReady() {
	ri.ready.Store(true)
	ri.readyOnce.Do(func() {
		close(ri.readyCh)
	})
}

// IndexMemorySource replaces all entries for a given source key with the
// provided entries. This is whole-source replacement semantics: the previous
// entries (if any) are discarded and rebuilt from the new slice.
// MEMORY.md files are small (≤4096 chars) so rebuild cost is negligible.
func (ri *RecallIndex) IndexMemorySource(key string, entries []string, ts time.Time) {
	built := make([]memoryEntry, 0, len(entries))
	for _, text := range entries {
		tokens := tokenize(text)
		tokSet := make(map[string]bool, len(tokens))
		for _, tok := range tokens {
			tokSet[tok] = true
		}
		built = append(built, memoryEntry{text: text, tokens: tokSet})
	}

	ri.mu.Lock()
	defer ri.mu.Unlock()
	ri.memSources[key] = &memorySource{entries: built, ts: ts}
}

// IndexMemoryFile reads a MEMORY.md file from disk and indexes its § entries.
// A missing or empty file clears any previously indexed state for the key
// (mirrors IndexMemorySource's clear-on-empty semantics); read errors are
// logged and leave the existing indexed state untouched.
func (ri *RecallIndex) IndexMemoryFile(key, path string) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			ri.IndexMemorySource(key, nil, time.Now())
			return
		}
		log.Printf("[recall] warn: read memory file %s: %v", path, err)
		return
	}
	entries := parseEntries(string(data))
	info, statErr := os.Stat(path)
	ts := time.Now()
	if statErr == nil {
		ts = info.ModTime()
	}
	ri.IndexMemorySource(key, entries, ts)
}

// BuildFromDiskAsync launches background index construction from disk.
// Non-blocking: returns immediately, index builds in a goroutine.
// stepsDir should be the directory containing UUID subdirectories (e.g. .rnix/data/steps/).
func (ri *RecallIndex) BuildFromDiskAsync(stepsDir string) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[recall] panic during index build: %v", r)
			}
			ri.markReady()
		}()
		if err := ri.BuildFromDisk(stepsDir); err != nil {
			log.Printf("[recall] index build failed: %v", err)
		}
	}()
}

// BuildFromDisk scans stepsDir/*/steps.jsonl and builds the full index.
// stepsDir is the directory containing UUID subdirectories directly.
// Marks the index as ready upon completion.
func (ri *RecallIndex) BuildFromDisk(stepsDir string) error {
	entries, err := os.ReadDir(stepsDir)
	if err != nil {
		ri.markReady()
		return fmt.Errorf("read steps dir %s: %w", stepsDir, err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		uuid := entry.Name()
		processDir := filepath.Join(stepsDir, uuid)
		ri.indexProcessDir(uuid, processDir)
	}

	ri.mu.RLock()
	docCount := len(ri.indexed)
	termCount := len(ri.index)
	ri.mu.RUnlock()
	log.Printf("[recall] index built: %d processes, %d terms", docCount, termCount)

	ri.markReady()
	return nil
}

// IndexProcess indexes a single process by UUID. Idempotent: no-op if already indexed.
// stepsDir is the directory containing the steps.jsonl file for this process.
func (ri *RecallIndex) IndexProcess(uuid string, stepsDir string) error {
	ri.mu.RLock()
	if ri.indexed[uuid] {
		ri.mu.RUnlock()
		return nil
	}
	ri.mu.RUnlock()

	// Verify the directory exists
	if _, err := os.Stat(stepsDir); err != nil {
		return fmt.Errorf("steps dir for %s: %w", uuid, err)
	}

	ri.indexProcessDir(uuid, stepsDir)
	return nil
}

// indexProcessDir reads steps.jsonl from processDir and indexes all steps.
func (ri *RecallIndex) indexProcessDir(uuid string, processDir string) {
	stepsPath := filepath.Join(processDir, "steps.jsonl")
	f, err := os.Open(stepsPath)
	if err != nil {
		return // no steps.jsonl — skip silently
	}
	defer f.Close()

	// Quick check under read lock — avoid parsing file if already indexed
	ri.mu.RLock()
	if ri.indexed[uuid] {
		ri.mu.RUnlock()
		return
	}
	ri.mu.RUnlock()

	// Use file mod time as the base timestamp for this process
	info, err := f.Stat()
	var baseTime time.Time
	if err == nil {
		baseTime = info.ModTime()
	} else {
		baseTime = time.Now()
	}

	// Collect all entries and postings without holding the lock
	var entries []docEntry
	localPostings := make(map[string][]posting)

	// Story 72.1: unbounded line reads. The former 1 MB limit meant a single
	// oversized step truncated the index at that point — this is the source of
	// the user-visible "[recall] scanner error ... token too long" logs. The
	// error branch below is retained for genuine I/O failures (a parse problem
	// still deserves a breadcrumb); it just no longer fires on line length.
	scanErr := jsonl.Scan(f, stepsPath, func(line []byte) error {
		var partial struct {
			Step    int    `json:"step"`
			Summary string `json:"summary"`
			Action  string `json:"action"`
		}
		if json.Unmarshal(line, &partial) != nil {
			return nil
		}
		if partial.Summary == "" && partial.Action == "" {
			return nil
		}

		entry := docEntry{
			Step:    partial.Step,
			Time:    baseTime,
			Summary: partial.Summary,
			Action:  partial.Action,
		}
		entries = append(entries, entry)

		// Build index text from summary + action
		text := partial.Summary + " " + partial.Action
		tokens := tokenize(text)
		p := posting{UUID: uuid, Step: partial.Step, Time: baseTime}

		for _, tok := range tokens {
			localPostings[tok] = append(localPostings[tok], p)
		}
		return nil
	})
	if scanErr != nil {
		log.Printf("[recall] scanner error for %s: %v", stepsPath, scanErr)
	}

	if len(entries) == 0 {
		return
	}

	// Atomically commit all postings and docs under a single write lock
	ri.mu.Lock()
	defer ri.mu.Unlock()

	// Double-check under write lock (another goroutine may have indexed this UUID)
	if ri.indexed[uuid] {
		return
	}
	ri.indexed[uuid] = true

	for tok, posts := range localPostings {
		ri.index[tok] = append(ri.index[tok], posts...)
	}
	ri.docs[uuid] = entries
}

// Search performs keyword search across the inverted index.
// Returns up to maxResults RecallResult entries, sorted by timestamp (newest first).
// Works regardless of the ready state (sync BuildFromDisk callers can search immediately).
func (ri *RecallIndex) Search(query string, maxResults int) []RecallResult {
	if maxResults <= 0 {
		maxResults = 20
	}

	queryTokens := tokenize(query)
	if len(queryTokens) == 0 {
		return nil
	}

	// Score each (uuid) by counting matching terms across all its steps
	type uuidScore struct {
		UUID      string
		Score     int
		Timestamp time.Time
		Summary   string
	}
	uuidScores := make(map[string]*uuidScore)

	ri.mu.RLock()
	for _, tok := range queryTokens {
		postings, ok := ri.index[tok]
		if !ok {
			continue
		}
		// Track which UUIDs were hit by this token (for intersection counting)
		seen := make(map[string]bool)
		for _, p := range postings {
			if seen[p.UUID] {
				continue
			}
			seen[p.UUID] = true
			us, ok := uuidScores[p.UUID]
			if !ok {
				us = &uuidScore{UUID: p.UUID, Timestamp: p.Time}
				uuidScores[p.UUID] = us
			}
			us.Score++
			if p.Time.After(us.Timestamp) {
				us.Timestamp = p.Time
			}
		}
	}

	// Build results with best summary per UUID
	var results []RecallResult
	for _, us := range uuidScores {
		// Only include results that matched ALL query tokens (intersection)
		if us.Score < len(queryTokens) {
			continue
		}
		summary := ""
		if docs, ok := ri.docs[us.UUID]; ok && len(docs) > 0 {
			summary = docs[0].Summary
		}
		results = append(results, RecallResult{
			UUID:      us.UUID,
			Summary:   summary,
			Timestamp: us.Timestamp,
			Source:    "session",
		})
	}

	// Collect memory source hits within the same RLock window. Iterate keys
	// in sorted order so ties below resolve deterministically.
	memKeys := make([]string, 0, len(ri.memSources))
	for key := range ri.memSources {
		memKeys = append(memKeys, key)
	}
	sort.Strings(memKeys)
	for _, key := range memKeys {
		for _, entry := range ri.memSources[key].entries {
			hit := true
			for _, tok := range queryTokens {
				if !entry.tokens[tok] {
					hit = false
					break
				}
			}
			if hit {
				results = append(results, RecallResult{
					UUID:      key,
					Summary:   entry.text,
					Timestamp: ri.memSources[key].ts,
					Source:    "memory",
				})
			}
		}
	}
	ri.mu.RUnlock()

	// Sort by timestamp desc (newest first). Entries of one memory source
	// share a single ts, so break ties by UUID then Summary to keep result
	// order — and the maxResults cut — deterministic across calls.
	sort.Slice(results, func(i, j int) bool {
		if !results[i].Timestamp.Equal(results[j].Timestamp) {
			return results[i].Timestamp.After(results[j].Timestamp)
		}
		if results[i].UUID != results[j].UUID {
			return results[i].UUID < results[j].UUID
		}
		return results[i].Summary < results[j].Summary
	})

	if len(results) > maxResults {
		results = results[:maxResults]
	}

	return results
}

// SaveIndex persists the index to disk as a JSON file.
func (ri *RecallIndex) SaveIndex(path string) error {
	ri.mu.RLock()
	// Deep-copy maps to avoid race between RUnlock and json.Marshal
	indexCopy := make(map[string][]posting, len(ri.index))
	for k, v := range ri.index {
		cp := make([]posting, len(v))
		copy(cp, v)
		indexCopy[k] = cp
	}
	docsCopy := make(map[string][]docEntry, len(ri.docs))
	for k, v := range ri.docs {
		cp := make([]docEntry, len(v))
		copy(cp, v)
		docsCopy[k] = cp
	}
	indexedCopy := make(map[string]bool, len(ri.indexed))
	maps.Copy(indexedCopy, ri.indexed)
	ri.mu.RUnlock()

	data := persistedIndex{
		Index:   indexCopy,
		Docs:    docsCopy,
		Indexed: indexedCopy,
	}

	b, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal index: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir for index: %w", err)
	}

	return os.WriteFile(path, b, 0o644)
}

// LoadIndex restores the index from a JSON file written by SaveIndex.
func (ri *RecallIndex) LoadIndex(path string) error {
	b, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read index file: %w", err)
	}

	var data persistedIndex
	if err := json.Unmarshal(b, &data); err != nil {
		return fmt.Errorf("unmarshal index: %w", err)
	}

	ri.mu.Lock()
	defer ri.mu.Unlock()

	if data.Index != nil {
		ri.index = data.Index
	}
	if data.Docs != nil {
		ri.docs = data.Docs
	}
	if data.Indexed != nil {
		ri.indexed = data.Indexed
	}

	return nil
}

// SummarizeRecallResults calls an auxiliary LLM to produce a concise summary
// of the search results for the given query. If results are empty, returns
// empty string without calling the LLM.
func SummarizeRecallResults(ctx context.Context, caller LLMCaller, query string, results []RecallResult) (string, error) {
	if len(results) == 0 {
		return "", nil
	}

	systemPrompt := loadPromptTemplate("recall_summarize.txt")

	// Build user prompt from results
	var b strings.Builder
	fmt.Fprintf(&b, "Query: %s\n\nResults:\n", query)
	for i, r := range results {
		fmt.Fprintf(&b, "%d. [%s] %s\n", i+1, r.UUID, r.Summary)
	}

	summary, err := caller.Call(ctx, systemPrompt, b.String(), 1000)
	if err != nil {
		return "", fmt.Errorf("recall summarize LLM call: %w", err)
	}

	return summary, nil
}

// ShouldBuildRecallIndex returns true if the recall index should be built
// based on the configuration.
func ShouldBuildRecallIndex(cfg RecallConfig) bool {
	return cfg.Enabled
}

// RecallDeviceToolDef returns VFS device ToolDef metadata for /dev/memory/recall.
// Follows Architecture Decision 35.
func RecallDeviceToolDef() recallToolDefResult {
	return recallToolDefResult{
		IsReadOnly:        true,
		IsConcurrencySafe: true,
		IsDestructive:     false,
		ShouldDefer:       true,
		SearchHint:        "search historical conversations and past agent knowledge",
	}
}

// tokenizeQuery splits a search query into normalized tokens.
// Exported for use by tests and external callers.
// Splits on whitespace/punctuation, lowercases, deduplicates, and filters
// tokens with length <= 1.
func tokenizeQuery(s string) []string {
	return tokenize(s)
}

// tokenize splits text into search tokens using Unicode-aware segmentation.
// Strategy:
//   - Split on whitespace and punctuation
//   - CJK characters are indexed as individual characters + bigrams
//   - Latin/digit sequences are indexed as whole words (lowercased)
//   - All tokens lowercased for case-insensitive matching
//   - Tokens with length <= 1 are filtered out
//   - Deduplication
func tokenize(text string) []string {
	text = strings.ToLower(text)
	seen := make(map[string]bool)
	var tokens []string

	addToken := func(tok string) {
		tok = strings.TrimSpace(tok)
		if tok == "" || len(tok) <= 1 {
			return
		}
		if !seen[tok] {
			seen[tok] = true
			tokens = append(tokens, tok)
		}
	}

	runes := []rune(text)
	i := 0
	for i < len(runes) {
		r := runes[i]

		if unicode.Is(unicode.Han, r) || unicode.Is(unicode.Katakana, r) || unicode.Is(unicode.Hiragana, r) {
			// CJK: collect consecutive CJK characters
			start := i
			for i < len(runes) && (unicode.Is(unicode.Han, runes[i]) || unicode.Is(unicode.Katakana, runes[i]) || unicode.Is(unicode.Hiragana, runes[i])) {
				i++
			}
			cjkRunes := runes[start:i]
			// Add bigrams for CJK sequences
			for j := range len(cjkRunes) - 1 {
				addToken(string(cjkRunes[j : j+2]))
			}
			// Add single CJK chars of 3+ byte characters
			for _, cr := range cjkRunes {
				s := string(cr)
				if len(s) >= 3 {
					addToken(s)
				}
			}
		} else if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' {
			// Latin/digit word
			start := i
			for i < len(runes) && (unicode.IsLetter(runes[i]) || unicode.IsDigit(runes[i]) || runes[i] == '_') {
				i++
			}
			addToken(string(runes[start:i]))
		} else {
			// Skip whitespace and punctuation
			i++
		}
	}

	return tokens
}
