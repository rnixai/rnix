package kernel

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/rnixai/rnix/internal/types"
)

// =============================================================================
// Disk garbage collector for .rnix/data/steps/<uuid>/ (Story 42.5).
//
// Scans terminated process directories under <stepDataDir>/data/steps/,
// matches them against the active GcConfig retention policy, and removes
// the on-disk data + the in-memory procHistory entry. Running/Suspended
// processes are always exempt (AC#3). Both retention rules are unioned; a
// candidate hit by either rule is reaped (AC#1, #2).
// =============================================================================

// GcCandidate describes one terminated process eligible for removal.
//
//   - UUID: process UUID (corresponds to .rnix/data/steps/<uuid>/).
//   - DeadAt: RFC3339Nano timestamp from proc-info.json (empty if never set).
//   - SizeBytes: total byte size of the candidate directory (best-effort sum).
//   - Reason: "age" | "count" | "age,count" — which retention rule(s) matched.
type GcCandidate struct {
	UUID      string
	DeadAt    string
	SizeBytes int64
	Reason    string
}

// GcResult is the outcome of one RunGc invocation.
//
// RemovedCount + FreedBytes describe what was actually cleaned in non-dry-run
// mode; Candidates is always populated for dry-run + as the planned list for
// regular runs (callers should not assume RemovedUUIDs == Candidates because
// individual deletion errors are tolerated).
type GcResult struct {
	RemovedCount int
	FreedBytes   int64
	RemovedUUIDs []string
	Candidates   []GcCandidate
}

// RunGc scans .rnix/data/steps/ for terminated processes and removes those
// matching the active GcCfg retention policy (Story 42.5 AC#1, #2, #3, #14).
//
// dryRun=true populates result.Candidates without touching the filesystem
// (drives `rnix gc --dry-run`).
// force=true is currently a CLI-side concern (bypass confirmation prompt);
// daemon callers always pass true.
//
// Returns a zero-value result + nil error when:
//   - gc is disabled (both retention dials zero) — equivalent no-op
//   - .rnix/data/steps/ does not exist — empty data dir
//   - the scan yields zero candidates — nothing to reap today
func (k *KernelImpl) RunGc(dryRun bool, force bool) (GcResult, error) {
	_ = force // reserved for daemon vs CLI semantics; daemon always force=true

	cfg := k.GcCfg()
	if cfg.RetentionDays == 0 && cfg.MaxEntries == 0 {
		// AC#8:全零 = 关闭 → treat as success no-op
		return GcResult{}, nil
	}

	// Serialize concurrent RunGc invocations (CLI `rnix gc` + daemon ticker)
	// to prevent double-counting removed UUIDs.
	k.gcMu.Lock()
	defer k.gcMu.Unlock()

	// Collect live UUIDs from procTable to protect running processes.
	live := make(map[string]bool)
	if k.procTable != nil {
		k.procTable.Range(func(_ types.PID, p *Process) bool {
			if p != nil && p.UUID != "" {
				live[p.UUID] = true
			}
			return true
		})
	}

	var result GcResult

	// GC each per-project data directory independently.
	for _, baseDir := range AllProjectBaseDirs(k.dataDir) {
		stepsDir := filepath.Join(baseDir, "steps")
		candidates, err := scanGcCandidates(stepsDir, cfg)
		if err != nil {
			log.Printf("[gc] warn: scan %s: %v", stepsDir, err)
			continue
		}

		// Filter live processes.
		if len(live) > 0 && len(candidates) > 0 {
			filtered := candidates[:0]
			for _, c := range candidates {
				if live[c.UUID] {
					log.Printf("[gc] skip live process UUID %s", c.UUID)
					continue
				}
				filtered = append(filtered, c)
			}
			candidates = filtered
		}

		result.Candidates = append(result.Candidates, candidates...)
		if dryRun {
			continue
		}

		for _, c := range candidates {
			freed, removeErr := removeGcCandidate(stepsDir, c.UUID)
			if removeErr != nil {
				log.Printf("[gc] warn: remove %s: %v", c.UUID, removeErr)
				continue
			}
			if k.procHistory != nil {
				k.procHistory.RemoveByUUID(c.UUID)
			}
			if appendErr := appendGcRemovedUUID(baseDir, c.UUID); appendErr != nil {
				log.Printf("[gc] warn: persist removed uuid %s: %v", c.UUID, appendErr)
			}
			result.RemovedCount++
			result.FreedBytes += freed
			result.RemovedUUIDs = append(result.RemovedUUIDs, c.UUID)
		}
	}
	return result, nil
}

// StartGcDaemon launches the background gc loop that periodically reaps
// terminated process directories per the active GcCfg (Story 42.5 AC#7, #8).
//
// Disabled config (both RetentionDays and MaxEntries == 0) returns immediately
// with a "[gc] disabled" log; otherwise it ticks every IntervalSeconds and
// invokes RunGc(false, true). Ctx cancellation cleanly exits the goroutine.
//
// The first run fires immediately (no warm-up wait) so daemons crashing into a
// large backlog don't keep accumulating disk for IntervalSeconds before the
// first reap.
func (k *KernelImpl) StartGcDaemon(ctx context.Context) {
	cfg := k.GcCfg()
	if cfg.RetentionDays == 0 && cfg.MaxEntries == 0 {
		log.Printf("[gc] disabled (no retention policy configured)")
		return
	}

	interval := time.Duration(cfg.IntervalSeconds) * time.Second
	log.Printf("[gc] enabled: retention_days=%d max_entries=%d interval=%s",
		cfg.RetentionDays, cfg.MaxEntries, interval)

	// First run immediately (skip the warm-up wait for AC#7's "first-immediate-run").
	if _, err := k.RunGc(false, true); err != nil {
		log.Printf("[gc] warn: initial run: %v", err)
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			log.Printf("[gc] daemon stopped")
			return
		case <-ticker.C:
			if _, err := k.RunGc(false, true); err != nil {
				log.Printf("[gc] warn: periodic run: %v", err)
			}
		}
	}
}

// scanGcCandidates walks <stepsDir>/<uuid>/proc-info.json and returns the
// entries hit by the retention rules.
//
// Why not reuse LoadProcHistory: gc cares about (UUID, state, dead_at,
// directory size) at scan time, not full ProcInfo + sort + maxSize truncation.
// gc also runs periodically and must stream-scan without preloading a million
// records into RAM. LoadProcHistory's structure has a different memory profile.
func scanGcCandidates(stepsDir string, cfg GcConfig) ([]GcCandidate, error) {
	entries, err := os.ReadDir(stepsDir)
	if err != nil {
		if os.IsNotExist(err) {
			// Empty / never-created data dir is a valid empty result.
			return nil, nil
		}
		return nil, fmt.Errorf("gc: read steps dir %s: %w", stepsDir, err)
	}

	type rawEntry struct {
		uuid      string
		state     string
		deadAt    string
		deadTime  time.Time
		sizeBytes int64
	}

	now := time.Now().UTC()
	var raws []rawEntry
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		uuid := entry.Name()
		piPath := filepath.Join(stepsDir, uuid, procInfoFilename)
		data, readErr := os.ReadFile(piPath)
		if readErr != nil {
			// Missing proc-info.json → skip (best-effort, mirrors LoadProcHistory).
			continue
		}
		var d procInfoDisk
		if jerr := json.Unmarshal(data, &d); jerr != nil {
			log.Printf("[gc] warn: corrupt %s: %v", piPath, jerr)
			continue
		}
		if d.UUID == "" {
			continue
		}
		// AC#3: Running / Suspended exempt. Defense-in-depth: filter by the raw
		// disk string first so any unrecognized state (e.g., future "init",
		// "paused") is exempt by default instead of falling through to Dead.
		// parseProcessState retains a Dead fallback to keep procInfoFromDisk
		// permissive, but gc must be conservative.
		if d.State != "dead" && d.State != "zombie" {
			continue
		}
		state := parseProcessState(d.State)
		if state != types.StateDead && state != types.StateZombie {
			continue
		}
		// dead_at empty → cannot evaluate retention; conservative skip.
		if d.DeadAt == "" {
			continue
		}
		t, perr := time.Parse(time.RFC3339Nano, d.DeadAt)
		if perr != nil {
			log.Printf("[gc] warn: parse dead_at %s: %v", piPath, perr)
			continue
		}
		raws = append(raws, rawEntry{
			uuid:      d.UUID,
			state:     d.State,
			deadAt:    d.DeadAt,
			deadTime:  t,
			sizeBytes: dirSizeBytes(filepath.Join(stepsDir, uuid)),
		})
	}

	// Apply retention_days rule (AC#1).
	ageHit := make(map[string]bool)
	if cfg.RetentionDays > 0 {
		threshold := time.Duration(cfg.RetentionDays) * 24 * time.Hour
		for _, r := range raws {
			if now.Sub(r.deadTime) >= threshold {
				ageHit[r.uuid] = true
			}
		}
	}

	// Apply max_entries rule (AC#2): keep newest cfg.MaxEntries by dead_at,
	// candidates are the oldest (total - max_entries) entries.
	countHit := make(map[string]bool)
	if cfg.MaxEntries > 0 && len(raws) > cfg.MaxEntries {
		sortedByAge := make([]rawEntry, len(raws))
		copy(sortedByAge, raws)
		sort.SliceStable(sortedByAge, func(i, j int) bool {
			return sortedByAge[i].deadTime.Before(sortedByAge[j].deadTime)
		})
		excess := len(raws) - cfg.MaxEntries
		for i := range excess {
			countHit[sortedByAge[i].uuid] = true
		}
	}

	candidates := make([]GcCandidate, 0, len(raws))
	for _, r := range raws {
		ageMatch := ageHit[r.uuid]
		countMatch := countHit[r.uuid]
		if !ageMatch && !countMatch {
			continue
		}
		reason := ""
		switch {
		case ageMatch && countMatch:
			reason = "age,count"
		case ageMatch:
			reason = "age"
		default:
			reason = "count"
		}
		candidates = append(candidates, GcCandidate{
			UUID:      r.uuid,
			DeadAt:    r.deadAt,
			SizeBytes: r.sizeBytes,
			Reason:    reason,
		})
	}
	return candidates, nil
}

// removeGcCandidate deletes <stepsDir>/<uuid>/ and returns the bytes freed.
// Pre-statting the size lets us report freed bytes even though os.RemoveAll
// doesn't return that info. Errors deleting individual files inside the
// directory tree surface as a non-nil error; the caller logs and continues.
func removeGcCandidate(stepsDir, uuid string) (int64, error) {
	dir := filepath.Join(stepsDir, uuid)
	size := dirSizeBytes(dir)
	if err := os.RemoveAll(dir); err != nil {
		return 0, fmt.Errorf("remove %s: %w", dir, err)
	}
	return size, nil
}

// dirSizeBytes returns the total byte size of regular files under dir.
// Best-effort: missing dir or scan errors return 0. Symlinks are skipped via
// the default Walk semantics (size of link entry, not target).
func dirSizeBytes(dir string) int64 {
	var total int64
	_ = filepath.Walk(dir, func(_ string, info os.FileInfo, err error) error {
		if err != nil || info == nil {
			return nil
		}
		if !info.IsDir() {
			total += info.Size()
		}
		return nil
	})
	return total
}

// gcRemovedFilename is the JSON Lines file under <stepDataDir>/data/ that
// records UUIDs gc has reaped. Each line is {"uuid": "...", "removed_at": "..."}.
// LoadGcRemovedUUIDs reads it at daemon startup so HasEverSeen distinguishes
// "gc'd" from "never spawned" across restarts (Story 42.5 AC#6, review Decision #2).
const gcRemovedFilename = ".gc-removed.json"

type gcRemovedRecord struct {
	UUID      string `json:"uuid"`
	RemovedAt string `json:"removed_at"`
}

// appendGcRemovedUUID appends one UUID to <baseDir>/data/.gc-removed.json as a
// JSON Lines record. Creates the file (and parent dir) on first write.
//
// Empty baseDir or uuid returns nil (no-op for tests with no real data dir).
func appendGcRemovedUUID(baseDir, uuid string) error {
	if baseDir == "" || uuid == "" {
		return nil
	}
	if err := os.MkdirAll(baseDir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", baseDir, err)
	}
	path := filepath.Join(baseDir, gcRemovedFilename)
	rec := gcRemovedRecord{UUID: uuid, RemovedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	line, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}

// LoadGcRemovedUUIDs reads <baseDir>/data/.gc-removed.json and returns the set
// of UUIDs that gc has previously reaped. Missing file → empty set + nil err.
// Corrupt lines are logged and skipped (best-effort).
func LoadGcRemovedUUIDs(baseDir string) (map[string]struct{}, error) {
	if baseDir == "" {
		return nil, nil
	}
	path := filepath.Join(baseDir, gcRemovedFilename)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	out := make(map[string]struct{})
	for line := range splitGcRemovedLines(data) {
		if len(line) == 0 {
			continue
		}
		var rec gcRemovedRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			log.Printf("[gc] warn: corrupt %s line: %v", path, err)
			continue
		}
		if rec.UUID != "" {
			out[rec.UUID] = struct{}{}
		}
	}
	return out, nil
}

// splitGcRemovedLines yields each newline-delimited byte slice of data (no
// trailing newline in each yielded slice). Empty input yields no values.
// Local to gc.go to avoid clashing with the test helper splitLines in
// atdd_21_2_reputation_test.go.
func splitGcRemovedLines(data []byte) func(yield func([]byte) bool) {
	return func(yield func([]byte) bool) {
		start := 0
		for i, b := range data {
			if b == '\n' {
				if !yield(data[start:i]) {
					return
				}
				start = i + 1
			}
		}
		if start < len(data) {
			yield(data[start:])
		}
	}
}
