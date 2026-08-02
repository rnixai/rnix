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

// indexFingerprint captures the steps.jsonl state observed *at index time* for
// one process (Story 72.4). It is the sole judge of whether a persisted index
// entry may still be trusted after a daemon restart.
//
// The values must come from the Stat performed before the file is parsed — never
// from a fresh Stat at SaveIndex time. A file that grew during the scan would
// otherwise be recorded as "indexed up to its current size" and the appended
// tail would be skipped forever (same failure shape as the Story 72.2
// watermark-ahead-of-data defect).
type indexFingerprint struct {
	Size    int64 `json:"size"`
	MTimeMs int64 `json:"mtime_ms"`
}

// persistedIndex is the JSON representation for SaveIndex/LoadIndex.
//
// Fprints is omitempty so a pre-72.4 index.json unmarshals cleanly; every UUID
// missing a fingerprint is treated as stale by InvalidateStale.
type persistedIndex struct {
	Index   map[string][]posting        `json:"index"`
	Docs    map[string][]docEntry       `json:"docs"`
	Indexed map[string]bool             `json:"indexed"`
	Fprints map[string]indexFingerprint `json:"fprints,omitempty"`
}

// DefaultIndexBuildConcurrency caps how many step directories are scanned in
// parallel by BuildAllFromDisksAsync (Story 72.4 AC2).
//
// Measured on a 11 GB / 120 base-dir / 1773-process corpus: cold cache 8.14 s at
// 8 vs 8.97 s at 120 (unbounded) and 8.67 s at 4; warm cache 4/8/16/32/119 all
// within noise (6.1-6.3 s). Serial is 2.3x slower, so concurrency itself is a
// net win — the cap exists to bound goroutine and fd peaks, not to relieve lock
// contention (indexProcessDir already parses outside the lock).
//
// Deliberately not runtime.NumCPU(): this is I/O bound, and 32 (this machine's
// core count) measured marginally slower than 8.
const DefaultIndexBuildConcurrency = 8

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

	// Story 72.4 — uuid → steps.jsonl size+mtime as observed at index time.
	// Persisted alongside the postings; InvalidateStale compares it against the
	// current on-disk state to decide whether a restored entry can be trusted.
	fprints map[string]indexFingerprint

	memSources map[string]*memorySource // key → MEMORY.md source (parallel to session index)
}

// NewRecallIndex creates a new empty RecallIndex.
func NewRecallIndex() *RecallIndex {
	return &RecallIndex{
		index:      make(map[string][]posting),
		docs:       make(map[string][]docEntry),
		indexed:    make(map[string]bool),
		fprints:    make(map[string]indexFingerprint),
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

// BuildAllFromDisksAsync scans every step root in stepsDirs under a
// concurrency cap, then marks the index ready exactly once (Story 72.4 AC2/AC6).
//
// The single markReady is the point of this entry point: when the startup path
// fans out per-base-dir BuildFromDiskAsync goroutines (120 on the reference
// corpus), the first one to finish flips Ready() while 119/120 of the index is
// still missing, and /dev/memory/recall starts answering searches from a
// 1/120-complete index. A coordinated goroutine flips Ready only when the last
// directory is done.
//
// The existing BuildFromDiskAsync / BuildFromDisk are intentionally unchanged —
// per-directory callers (drivers/memory atdd_35_4) depend on their current
// behavior. This is a separate startup-only entry point.
//
// A nil or empty stepsDirs is not an error (nothing to scan) — the goroutine
// marks ready and exits. Per-directory errors are logged, not returned: a
// missing project dir must not block the rest of the build.
func (ri *RecallIndex) BuildAllFromDisksAsync(stepsDirs []string, concurrency int) {
	if concurrency <= 0 {
		concurrency = DefaultIndexBuildConcurrency
	}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[recall] panic during index build: %v", r)
			}
			ri.markReady()
		}()

		sem := make(chan struct{}, concurrency)
		var wg sync.WaitGroup
		for _, dir := range stepsDirs {
			wg.Add(1)
			sem <- struct{}{}
			go func() {
				defer wg.Done()
				defer func() { <-sem }()
				// The recover lives in the worker, not just the coordinator:
				// a recover in the outer goroutine cannot catch a panic in a
				// child goroutine. BuildFromDiskAsync keeps its work and its
				// recover in the same goroutine; this entry point must do the
				// same per worker or a single bad dir would kill the daemon.
				defer func() {
					if r := recover(); r != nil {
						log.Printf("[recall] panic during index build of %s: %v", dir, r)
					}
				}()
				if err := ri.scanStepsDir(dir); err != nil {
					log.Printf("[recall] index build failed: %v", err)
				}
			}()
		}
		wg.Wait()
	}()
}

// BuildFromDisk scans stepsDir/*/steps.jsonl and builds the full index.
// stepsDir is the directory containing UUID subdirectories directly.
// Marks the index as ready upon completion.
func (ri *RecallIndex) BuildFromDisk(stepsDir string) error {
	err := ri.scanStepsDir(stepsDir)
	ri.markReady()
	return err
}

// scanStepsDir is BuildFromDisk without the markReady side effect, so
// BuildAllFromDisksAsync can defer readiness until every directory is done
// (Story 72.4 AC6). BuildFromDisk keeps its original semantics by calling this
// and marking ready itself.
func (ri *RecallIndex) scanStepsDir(stepsDir string) error {
	entries, err := os.ReadDir(stepsDir)
	if err != nil {
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
	// Story 72.4: capture the fingerprint from this same pre-parse Stat. haveFp
	// stays false when Stat fails, so the UUID is treated as stale on the next
	// startup rather than being trusted on a fabricated fingerprint.
	var fp indexFingerprint
	haveFp := false
	if err == nil {
		baseTime = info.ModTime()
		fp = indexFingerprint{Size: info.Size(), MTimeMs: info.ModTime().UnixMilli()}
		haveFp = true
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
	if haveFp {
		ri.fprints[uuid] = fp
	}

	for tok, posts := range localPostings {
		ri.index[tok] = append(ri.index[tok], posts...)
	}
	ri.docs[uuid] = entries
}

// purgeUUIDLocked removes every trace of one UUID from the index: the indexed
// marker, its docs, its fingerprint, its dir record, and — critically — all of
// its postings.
//
// Clearing postings is not optional. indexProcessDir commits with
// `ri.index[tok] = append(ri.index[tok], posts...)`, so dropping only the
// `indexed` marker and letting the UUID be rescanned would leave two copies of
// every posting. Search's per-token `seen[p.UUID]` dedup happens to hide the
// score effect, which is exactly why the leak would go unnoticed while memory
// grows on every restart.
//
// Caller must hold ri.mu for writing.
func (ri *RecallIndex) purgeUUIDLocked(uuid string) {
	delete(ri.indexed, uuid)
	delete(ri.docs, uuid)
	delete(ri.fprints, uuid)

	for tok, posts := range ri.index {
		kept := posts[:0]
		for _, p := range posts {
			if p.UUID != uuid {
				kept = append(kept, p)
			}
		}
		if len(kept) == 0 {
			delete(ri.index, tok)
			continue
		}
		ri.index[tok] = kept
	}
}

// InvalidateStale validates every persisted index entry against the current
// on-disk state and purges the ones that can no longer be trusted (Story 72.4
// AC3). Returns the number of UUIDs purged.
//
// Call it after LoadIndex and before the background rescan: purged UUIDs are
// then re-indexed from scratch, while surviving ones are skipped in
// microseconds via the `indexed` map — which is where the 57x startup win comes
// from.
//
// Three outcomes per UUID:
//
//	steps.jsonl missing      → ghost (gc removed it, or a manual rm) → purge
//	size/mtime differ, or no
//	fingerprint at all       → stale (process kept writing; or a pre-72.4
//	                           index.json with no fprints) → purge
//	size/mtime match         → keep, rescan will skip it
//
// stepsDirs are the directories holding UUID subdirectories (one per project
// base dir). A UUID found under none of them is a ghost.
func (ri *RecallIndex) InvalidateStale(stepsDirs []string) int {
	ri.mu.RLock()
	uuids := make([]string, 0, len(ri.indexed))
	for uuid := range ri.indexed {
		uuids = append(uuids, uuid)
	}
	fpSnapshot := make(map[string]indexFingerprint, len(ri.fprints))
	maps.Copy(fpSnapshot, ri.fprints)
	ri.mu.RUnlock()

	if len(uuids) == 0 {
		return 0
	}

	// One ReadDir per step root instead of a stat per (uuid, root) pair: with
	// 1773 processes across 120 roots the latter is ~200k syscalls on a path
	// that runs before the daemon accepts its first request.
	homes := make(map[string]string, len(uuids))
	for _, root := range stepsDirs {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue // a project dir may legitimately not exist
		}
		for _, e := range entries {
			if !e.IsDir() {
				continue
			}
			if _, dup := homes[e.Name()]; !dup {
				homes[e.Name()] = root
			}
		}
	}

	// Stat outside the lock — one per indexed process — so Search is never
	// blocked behind the filesystem.
	var stale []string
	for _, uuid := range uuids {
		fp, haveFp := fpSnapshot[uuid]
		if !haveFp {
			// No fingerprint: either a pre-72.4 index.json, or the Stat failed
			// when the process was indexed. Both mean "cannot prove it is
			// current" — rescan rather than serve possibly-truncated results.
			stale = append(stale, uuid)
			continue
		}
		root, found := homes[uuid]
		if !found {
			stale = append(stale, uuid) // ghost: gc removed it, or a manual rm
			continue
		}
		info, err := os.Stat(filepath.Join(root, uuid, "steps.jsonl"))
		if err != nil {
			stale = append(stale, uuid)
			continue
		}
		cur := indexFingerprint{Size: info.Size(), MTimeMs: info.ModTime().UnixMilli()}
		if cur != fp {
			stale = append(stale, uuid) // the process kept writing after the snapshot
		}
	}

	if len(stale) == 0 {
		return 0
	}

	ri.mu.Lock()
	for _, uuid := range stale {
		ri.purgeUUIDLocked(uuid)
	}
	ri.mu.Unlock()
	return len(stale)
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
	// Story 72.4: fingerprints are recorded at scan time (indexProcessDir), never
	// re-stat'ed here — a file that grew after being indexed must be recorded at
	// its indexed size so the next startup sees the mismatch and rescans.
	// Entries restored by LoadIndex and skipped this run keep their old
	// fingerprint, which still describes the state their postings reflect.
	fprintsCopy := make(map[string]indexFingerprint, len(ri.fprints))
	maps.Copy(fprintsCopy, ri.fprints)
	ri.mu.RUnlock()

	data := persistedIndex{
		Index:   indexCopy,
		Docs:    docsCopy,
		Indexed: indexedCopy,
		Fprints: fprintsCopy,
	}

	b, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal index: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir for index: %w", err)
	}

	// Atomic write (tmp + rename, same shape as internal/config/registry.go
	// and internal/ui/state.go): a crash mid-save must not truncate the
	// previous good index. A corrupt index.json is safe — LoadIndex falls
	// back to a cold scan — but that fallback costs a full rescan on the
	// next startup. Rename leaves either the old or the new file, never a
	// partial one.
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return fmt.Errorf("write temp index: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename index: %w", err)
	}
	return nil
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
	// Story 72.4 — a pre-72.4 index.json has no "fprints"; leave the map empty
	// (never nil, writers assume it exists) so InvalidateStale judges every
	// restored UUID stale and rescans it.
	if data.Fprints != nil {
		ri.fprints = data.Fprints
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
