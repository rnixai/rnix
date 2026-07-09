package kernel

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"sync"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// ProcessHistory stores snapshots of processes that have been removed from the
// process table by the reaper. It acts as a bounded FIFO ring buffer protected
// by a RWMutex so the reaper can write while the Dashboard reads concurrently.
//
// removedUUIDs tracks UUIDs that were Add'd then RemoveByUUID'd (Story 42.5
// AC#6) so resumeFromHistory can distinguish "garbage collected" from "never
// spawned" errors. The map grows monotonically; in practice the daemon process
// lifetime caps it.
type ProcessHistory struct {
	mu           sync.RWMutex
	entries      []vfs.ProcInfo
	removedUUIDs map[string]struct{}
	maxSize      int
}

// NewProcessHistory creates a ProcessHistory with the given capacity.
func NewProcessHistory(maxSize int) *ProcessHistory {
	return &ProcessHistory{
		entries:      make([]vfs.ProcInfo, 0, maxSize),
		removedUUIDs: make(map[string]struct{}),
		maxSize:      maxSize,
	}
}

// Add appends a process snapshot. If the buffer is full, the oldest entry is
// evicted (FIFO).
func (h *ProcessHistory) Add(info vfs.ProcInfo) {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.entries = append(h.entries, info)
	if len(h.entries) > h.maxSize {
		h.entries = h.entries[len(h.entries)-h.maxSize:]
	}
}

// Upsert replaces the most recent entry matching info.UUID in place, or appends
// info when no match exists (Story 56.6). Synthesized CLI-subagent nodes use
// this so the determinism/idempotency contract (same tool_use_id → one node)
// holds and finalize (Running → Dead) updates the live snapshot rather than
// adding a duplicate. Appending honors the same FIFO cap as Add.
//
// Story 64.2 extends this to the two real-process history sink/load paths:
// reap.go cleanupExpiredDead and proc_query.go LoadHistory. Replacing in place
// keeps a single snapshot per UUID (no startup-loaded + reap double entry) AND
// preserves FIFO insertion position — the precondition ListAllProcs's stable
// sort depends on ("procHistory.List() preserves FIFO insertion order").
//
// Terminal guard: a stored Dead snapshot is never replaced by a non-terminal
// one — mirrors ListAllProcs's shouldReplaceHistoryEntry rule ③ at the storage
// layer, so a stale non-terminal copy (e.g. a suspended snapshot in a
// later-scanned baseDir during LoadHistory) cannot erase real exit facts.
func (h *ProcessHistory) Upsert(info vfs.ProcInfo) {
	if info.UUID == "" {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()

	for i := range slices.Backward(h.entries) {
		if h.entries[i].UUID == info.UUID {
			if isTerminalHistoryState(h.entries[i].State) && !isTerminalHistoryState(info.State) {
				return
			}
			h.entries[i] = info
			return
		}
	}
	h.entries = append(h.entries, info)
	if len(h.entries) > h.maxSize {
		h.entries = h.entries[len(h.entries)-h.maxSize:]
	}
}

// List returns a deep copy of all stored snapshots.
func (h *ProcessHistory) List() []vfs.ProcInfo {
	h.mu.RLock()
	defer h.mu.RUnlock()

	out := make([]vfs.ProcInfo, len(h.entries))
	copy(out, h.entries)
	return out
}

// FindByPID returns the most recent snapshot for the given PID, or nil if not found.
func (h *ProcessHistory) FindByPID(pid types.PID) *vfs.ProcInfo {
	h.mu.RLock()
	defer h.mu.RUnlock()

	// Search backwards for the most recent entry with this PID.
	for i := range slices.Backward(h.entries) {
		if h.entries[i].PID == pid {
			info := h.entries[i]
			return &info
		}
	}
	return nil
}

// FindByUUID returns the most recent snapshot for the given UUID, or nil if not found.
func (h *ProcessHistory) FindByUUID(uuid string) *vfs.ProcInfo {
	if uuid == "" {
		return nil
	}
	h.mu.RLock()
	defer h.mu.RUnlock()

	for i := range slices.Backward(h.entries) {
		if h.entries[i].UUID == uuid {
			info := h.entries[i]
			return &info
		}
	}
	return nil
}

// Len returns the current number of stored entries.
func (h *ProcessHistory) Len() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.entries)
}

// RemoveByUUID removes all entries matching the given UUID from history.
// Returns true if any entry was removed. Also marks the UUID as "ever seen"
// (Story 42.5 AC#6) so post-gc HasEverSeen lookups still report true and
// resumeFromHistory can emit the "garbage collected" error variant.
func (h *ProcessHistory) RemoveByUUID(uuid string) bool {
	if uuid == "" {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()

	n := 0
	for _, e := range h.entries {
		if e.UUID != uuid {
			h.entries[n] = e
			n++
		}
	}
	removed := n != len(h.entries)
	if removed {
		h.entries = h.entries[:n]
		// AC#6: record the UUID as ever-seen so future HasEverSeen calls hit
		// even though the entry is no longer in the ring buffer.
		if h.removedUUIDs == nil {
			h.removedUUIDs = make(map[string]struct{})
		}
		h.removedUUIDs[uuid] = struct{}{}
	}
	return removed
}

// HasEverSeen reports whether the given UUID is currently in history OR was
// previously Add'd and later RemoveByUUID'd (Story 42.5 AC#6 — distinguish
// "garbage collected" from "never spawned" in resume errors).
func (h *ProcessHistory) HasEverSeen(uuid string) bool {
	if uuid == "" {
		return false
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for i := range h.entries {
		if h.entries[i].UUID == uuid {
			return true
		}
	}
	if h.removedUUIDs != nil {
		if _, ok := h.removedUUIDs[uuid]; ok {
			return true
		}
	}
	return false
}

// SeedRemovedUUIDs merges the given UUID set into the in-memory removedUUIDs
// map. Used by LoadHistory to restore the post-restart "ever seen" view from
// the persisted .gc-removed.json (Story 42.5 AC#6 cross-restart correctness).
func (h *ProcessHistory) SeedRemovedUUIDs(uuids map[string]struct{}) {
	if len(uuids) == 0 {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.removedUUIDs == nil {
		h.removedUUIDs = make(map[string]struct{}, len(uuids))
	}
	for u := range uuids {
		h.removedUUIDs[u] = struct{}{}
	}
}

// ---------------------------------------------------------------------------
// Disk persistence for proc-info.json
// ---------------------------------------------------------------------------

// procInfoDisk is the JSON-serializable representation of ProcInfo on disk.
type procInfoDisk struct {
	PID             uint64   `json:"pid"`
	UUID            string   `json:"uuid"`
	OriginUUID      string   `json:"origin_uuid,omitempty"`
	ResumedFromStep int      `json:"resumed_from_step,omitempty"`
	ExitReason      string   `json:"exit_reason,omitempty"`
	CtxSize         int      `json:"ctx_size,omitempty"`
	PPID            uint64   `json:"ppid"`
	ParentUUID      string   `json:"parent_uuid,omitempty"`
	State           string   `json:"state"`
	Intent          string   `json:"intent"`
	Skills          []string `json:"skills,omitempty"`
	TokensUsed      int      `json:"tokens_used"`
	LastInputTokens int      `json:"last_input_tokens,omitempty"`
	ContextBudget   int      `json:"context_budget,omitempty"`
	MaxSteps        int      `json:"max_steps,omitempty"`
	CreatedAt       string   `json:"created_at"`
	DeadAt          string   `json:"dead_at,omitempty"`
	PausedTotalMs   int64    `json:"paused_total_ms,omitempty"`
	// Story 44.3 AC#1 — suspend metadata persisted across daemon restart so
	// LoadSuspendedFromDisk can recreate accurate Suspended placeholders.
	// SuspendReason intentionally typed as string (not enum) to stay
	// decoupled from Epic 45's planned HeartbeatMonitor removal.
	SuspendReason   string   `json:"suspend_reason,omitempty"`
	PausedAt        string   `json:"paused_at,omitempty"`
	IsPaused        bool     `json:"is_paused,omitempty"`
	CtxID           uint64   `json:"ctx_id"`
	Result          string   `json:"result,omitempty"`
	AllowedDevices  []string `json:"allowed_devices,omitempty"`
	DeniedDevices   []string `json:"denied_devices,omitempty"`
	AllowedTools    []string `json:"allowed_tools,omitempty"` // Story 54.1: authoritative tool whitelist; omitempty keeps legacy proc-info.json clean
	Provider        string   `json:"provider,omitempty"`
	Model           string   `json:"model,omitempty"`
	ReasoningEffort string   `json:"reasoning_effort,omitempty"` // Story 55.2: snapshotted reasoning-effort/level
	// PrimaryDevice — Epic 44 follow-up: persist the LLM VFS path so daemon
	// restart can rebuild reasonStep-driven placeholders. Without this,
	// LoadSuspendedFromDisk leaves PrimaryDevice="" and resumeOneForSubtree
	// silently routes the placeholder into the script-runner branch.
	PrimaryDevice string `json:"primary_device,omitempty"`
	// ProjectDir — Epic 44 follow-up: persist the project root so
	// LoadSuspendedFromDisk can call the injected projectConfigLoader to
	// rebuild a full ProjectConfig (LLMFileOpener / AgentLoader / SkillLoader)
	// on placeholder revival. Without this, processes using a project-only
	// provider (e.g. opencodego) cannot reopen their LLM device after a
	// daemon restart — see EchoMatrix `device not found: /dev/llm/opencodego`
	// regression.
	ProjectDir    string   `json:"project_dir,omitempty"`
	ContextWindow int      `json:"context_window,omitempty"`
	ComposeNode   string   `json:"compose_node,omitempty"`
	ComposeDeps   []string `json:"compose_deps,omitempty"`
	PipelineIndex int      `json:"pipeline_index"`
	PipelineTotal int      `json:"pipeline_total"`
	// Authoritative exit signal: 0 = success, non-zero = failure.
	// ExitCodeSet=false (zero value, e.g. legacy snapshots without these fields)
	// means dashboard must fall back to result-text heuristic.
	ExitCode    int  `json:"exit_code,omitempty"`
	ExitCodeSet bool `json:"exit_code_set,omitempty"`

	// Story 48.1 — MCP mount snapshots persisted alongside AllowedDevices so
	// the resume / load_suspended path can re-mount transports without
	// re-reading agent.yaml (which may have changed or be missing post-reap).
	// `omitempty` ensures legacy snapshots without this field re-marshal
	// cleanly (TestProcInfoDisk_MCPMounts_BackwardCompat) and the AC6
	// zero-overhead path stays branch-free in JSON.
	MCPMounts      []mcpMountDisk    `json:"mcp_mounts,omitempty"`
	DriverMeta     map[string]string `json:"driver_meta,omitempty"`
	FeatureProfile string            `json:"feature_profile,omitempty"`

	// Synthetic marks a CLI-subagent observation node (Story 56.6) — see
	// vfs.ProcInfo.Synthetic. omitempty keeps legacy proc-info.json clean and
	// makes absent → false on load (backward compatible).
	Synthetic bool `json:"synthetic,omitempty"`
}

// mcpMountDisk mirrors vfs.MCPMountSnapshot on disk. Defined here (instead of
// reusing vfs.MCPMountSnapshot directly in procInfoDisk) so the kernel-side
// disk schema can evolve independently of the vfs in-memory snapshot. Today
// the two are structurally identical; an explicit conversion layer keeps the
// boundary obvious if the disk format ever needs to add transport-specific
// fields (e.g. mcp_subprocess_pids, deferred to Story 48.2).
type mcpMountDisk struct {
	Path   string        `json:"path"`
	Config vfs.MCPConfig `json:"config"`
}

func procInfoToDisk(info vfs.ProcInfo) procInfoDisk {
	d := procInfoDisk{
		PID:             uint64(info.PID),
		UUID:            info.UUID,
		OriginUUID:      info.OriginUUID,
		ResumedFromStep: info.ResumedFromStep,
		ExitReason:      info.ExitReason,
		CtxSize:         info.CtxSize,
		PPID:            uint64(info.PPID),
		ParentUUID:      info.ParentUUID,
		State:           info.State.String(),
		Intent:          info.Intent,
		Skills:          info.Skills,
		TokensUsed:      info.TokensUsed,
		LastInputTokens: info.LastInputTokens,
		ContextBudget:   info.ContextBudget,
		MaxSteps:        info.MaxSteps,
		CreatedAt:       info.CreatedAt.Format(time.RFC3339Nano),
		CtxID:           uint64(info.CtxID),
		Result:          info.Result,
		AllowedDevices:  info.AllowedDevices,
		DeniedDevices:   info.DeniedDevices,
		AllowedTools:    info.AllowedTools,
		Provider:        info.Provider,
		Model:           info.Model,
		ReasoningEffort: info.ReasoningEffort,
		PrimaryDevice:   info.PrimaryDevice,
		ProjectDir:      info.ProjectDir,
		ContextWindow:   info.ContextWindow,
		ComposeNode:     info.ComposeNode,
		ComposeDeps:     append([]string(nil), info.ComposeDeps...),
		PipelineIndex:   info.PipelineIndex,
		PipelineTotal:   info.PipelineTotal,
		// Story 44.3 AC#1 — persist suspend metadata so daemon restart can
		// reload Suspended placeholders into procTable.
		SuspendReason:  info.SuspendReason,
		IsPaused:       info.IsPaused,
		ExitCode:       info.ExitCode,
		ExitCodeSet:    info.ExitCodeSet,
		DriverMeta:     info.DriverMeta,
		FeatureProfile: info.FeatureProfile,
		Synthetic:      info.Synthetic,
	}
	if !info.DeadAt.IsZero() {
		d.DeadAt = info.DeadAt.Format(time.RFC3339Nano)
	}
	if !info.PausedAt.IsZero() {
		d.PausedAt = info.PausedAt.Format(time.RFC3339Nano)
	}
	if info.PausedTotal > 0 {
		d.PausedTotalMs = info.PausedTotal.Milliseconds()
	}
	// Story 48.1 — translate vfs.MCPMountSnapshot ↔ mcpMountDisk. nil / empty
	// slices stay nil so `omitempty` keeps the JSON field absent on legacy or
	// non-MCP processes.
	if len(info.MCPMounts) > 0 {
		d.MCPMounts = make([]mcpMountDisk, 0, len(info.MCPMounts))
		for _, m := range info.MCPMounts {
			d.MCPMounts = append(d.MCPMounts, mcpMountDisk{Path: m.Path, Config: m.Config})
		}
	}
	return d
}

func procInfoFromDisk(d procInfoDisk) vfs.ProcInfo {
	info := vfs.ProcInfo{
		PID:             types.PID(d.PID),
		UUID:            d.UUID,
		OriginUUID:      d.OriginUUID,
		ResumedFromStep: d.ResumedFromStep,
		ExitReason:      d.ExitReason,
		CtxSize:         d.CtxSize,
		PPID:            types.PID(d.PPID),
		ParentUUID:      d.ParentUUID,
		State:           parseProcessState(d.State),
		Intent:          d.Intent,
		Skills:          d.Skills,
		TokensUsed:      d.TokensUsed,
		LastInputTokens: d.LastInputTokens,
		ContextBudget:   d.ContextBudget,
		MaxSteps:        d.MaxSteps,
		CtxID:           types.CtxID(d.CtxID),
		Result:          d.Result,
		AllowedDevices:  d.AllowedDevices,
		DeniedDevices:   d.DeniedDevices,
		AllowedTools:    d.AllowedTools,
		Provider:        d.Provider,
		Model:           d.Model,
		ReasoningEffort: d.ReasoningEffort,
		PrimaryDevice:   d.PrimaryDevice,
		ProjectDir:      d.ProjectDir,
		ContextWindow:   d.ContextWindow,
		ComposeNode:     d.ComposeNode,
		ComposeDeps:     d.ComposeDeps,
		PipelineIndex:   d.PipelineIndex,
		PipelineTotal:   d.PipelineTotal,
		// Story 44.3 AC#1 — restore suspend metadata. SuspendReason is a
		// transparent string passthrough; PausedAt parses best-effort and
		// stays zero on parse failure.
		SuspendReason:  d.SuspendReason,
		IsPaused:       d.IsPaused,
		ExitCode:       d.ExitCode,
		ExitCodeSet:    d.ExitCodeSet,
		DriverMeta:     d.DriverMeta,
		FeatureProfile: d.FeatureProfile,
		Synthetic:      d.Synthetic,
	}
	if d.CreatedAt != "" {
		info.CreatedAt, _ = time.Parse(time.RFC3339Nano, d.CreatedAt)
	}
	if d.DeadAt != "" {
		info.DeadAt, _ = time.Parse(time.RFC3339Nano, d.DeadAt)
	}
	if d.PausedAt != "" {
		info.PausedAt, _ = time.Parse(time.RFC3339Nano, d.PausedAt)
	}
	if d.PausedTotalMs > 0 {
		info.PausedTotal = time.Duration(d.PausedTotalMs) * time.Millisecond
	}
	// Story 48.1 — translate mcpMountDisk back into vfs.MCPMountSnapshot.
	// Legacy snapshots without the field arrive as nil here; the loop is
	// skipped and info.MCPMounts stays nil — exactly what the AC6 zero-overhead
	// path and the BackwardCompat test expect.
	if len(d.MCPMounts) > 0 {
		info.MCPMounts = make([]vfs.MCPMountSnapshot, 0, len(d.MCPMounts))
		for _, m := range d.MCPMounts {
			info.MCPMounts = append(info.MCPMounts, vfs.MCPMountSnapshot{Path: m.Path, Config: m.Config})
		}
	}
	return info
}

func parseProcessState(s string) types.ProcessState {
	switch s {
	case "created":
		return types.StateCreated
	case "running":
		return types.StateRunning
	case "zombie":
		return types.StateZombie
	case "dead":
		return types.StateDead
	case "suspended":
		return types.StateSuspended
	default:
		return types.StateDead
	}
}

// procInfoFilename is the name of the ProcInfo snapshot file.
const procInfoFilename = "proc-info.json"

// SeedPIDCounterFromDisk scans every proc-info.json under
// <baseDir>/data/steps/ and lifts pidCounter to max(disk PIDs) so the next
// nextPID() returns a value strictly larger than every persisted PID. This
// closes the daemon-restart PID-reuse hole that surfaced in EchoMatrix on
// 2026-05-26: pidCounter (a process-local atomic.Uint64) reset to 0 on
// daemon start, then LoadSuspendedFromDisk reloaded a parent placeholder
// that the user later Resumed — Resume's downstream Spawn calls allocated
// PID=1 for the new child while the on-disk parent snapshot still carried
// PID=2, producing the dashboard layout "child PID=1 under parent PID=2"
// and the visual impression of swapped lineage.
//
// Best-effort: a corrupt or unreadable entry is skipped without failing
// the seed. Daemon startup MUST call this before any code path that
// invokes NewProcess (which calls nextPID).
func SeedPIDCounterFromDisk(baseDir string) error {
	if baseDir == "" {
		return nil
	}
	stepsDir := filepath.Join(baseDir, "steps")
	entries, err := os.ReadDir(stepsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("readdir %s: %w", stepsDir, err)
	}
	var maxPID uint64
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(stepsDir, entry.Name(), procInfoFilename)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var d procInfoDisk
		if err := json.Unmarshal(data, &d); err != nil {
			continue
		}
		if d.PID > maxPID {
			maxPID = d.PID
		}
	}
	for {
		current := pidCounter.Load()
		if current >= maxPID {
			return nil
		}
		if pidCounter.CompareAndSwap(current, maxPID) {
			return nil
		}
	}
}

// SaveProcInfo writes a ProcInfo snapshot to <baseDir>/data/steps/<uuid>/proc-info.json.
// Best-effort: returns nil silently if baseDir or UUID is empty.
func SaveProcInfo(baseDir string, info vfs.ProcInfo) error {
	if baseDir == "" || info.UUID == "" {
		return nil
	}
	dir := filepath.Join(baseDir, "steps", info.UUID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}

	data, err := json.MarshalIndent(procInfoToDisk(info), "", "  ")
	if err != nil {
		return fmt.Errorf("marshal proc-info: %w", err)
	}

	target := filepath.Join(dir, procInfoFilename)
	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, target); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename %s: %w", target, err)
	}
	return nil
}

// LoadProcHistory scans <baseDir>/data/steps/*/proc-info.json and returns a
// ProcessHistory populated with the most recent maxSize entries (sorted by CreatedAt).
// Missing or corrupt files are skipped with a warning log.
func LoadProcHistory(baseDir string, maxSize int) (*ProcessHistory, error) {
	infos, err := loadAllProcInfos(baseDir)
	if err != nil {
		return nil, err
	}

	if len(infos) > maxSize {
		infos = infos[len(infos)-maxSize:]
	}

	return &ProcessHistory{
		entries: infos,
		maxSize: maxSize,
	}, nil
}

// loadAllProcInfos scans <baseDir>/steps/*/proc-info.json and returns every
// snapshot sorted by CreatedAt, with no size cap. LoadHistory reconciles over
// this full set (Story 64.1 review D2): stale non-terminal entries beyond the
// in-memory ring window must still be normalized on disk, because ListResumable
// reads the disk directly and is not bounded by the window.
func loadAllProcInfos(baseDir string) ([]vfs.ProcInfo, error) {
	if baseDir == "" {
		return nil, nil
	}

	stepsDir := filepath.Join(baseDir, "steps")
	entries, err := os.ReadDir(stepsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("readdir %s: %w", stepsDir, err)
	}

	var infos []vfs.ProcInfo
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		path := filepath.Join(stepsDir, entry.Name(), procInfoFilename)
		data, err := os.ReadFile(path)
		if err != nil {
			continue // no proc-info.json, skip silently
		}
		var d procInfoDisk
		if err := json.Unmarshal(data, &d); err != nil {
			log.Printf("[history] corrupt %s: %v", path, err)
			continue
		}
		if d.UUID == "" {
			continue
		}
		infos = append(infos, procInfoFromDisk(d))
	}

	slices.SortFunc(infos, func(a, b vfs.ProcInfo) int {
		return a.CreatedAt.Compare(b.CreatedAt)
	})

	return infos, nil
}

// ListResumable scans <baseDir>/data/steps/*/proc-info.json and returns snapshots
// for ANY recoverable process — daemon crash leftovers (state=running), naturally
// exited processes (state=zombie/dead), suspended processes, and created-but-never-
// run processes. Per Epic 42 design (post-fix): resume is a session-level concept
// that does NOT depend on daemon crashing — any process with sufficient history
// on disk can be resumed.
//
// Filtering: requires both proc-info.json AND steps.jsonl to be present (otherwise
// the resume path lacks history to replay). UUID must be non-empty.
//
// Corrupt JSON files are logged and skipped. Missing baseDir or steps dir returns
// nil, nil (consistent with LoadProcHistory's behavior).
//
// Sorted by last activity (DeadAt > CreatedAt) most-recent-first.
func ListResumable(baseDir string) ([]vfs.ProcInfo, error) {
	if baseDir == "" {
		return nil, nil
	}
	stepsDir := filepath.Join(baseDir, "steps")
	entries, err := os.ReadDir(stepsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("readdir %s: %w", stepsDir, err)
	}

	var infos []vfs.ProcInfo
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		uuidDir := filepath.Join(stepsDir, entry.Name())
		path := filepath.Join(uuidDir, procInfoFilename)
		data, err := os.ReadFile(path)
		if err != nil {
			if !errors.Is(err, os.ErrNotExist) {
				log.Printf("[history] read %s: %v", path, err)
			}
			continue
		}
		var d procInfoDisk
		if err := json.Unmarshal(data, &d); err != nil {
			log.Printf("[history] corrupt %s: %v", path, err)
			continue
		}
		if d.UUID == "" {
			continue
		}
		// Story 56.6: synthetic CLI-subagent observation nodes are not real rnix
		// processes — exclude them from the resumable set so `rnix ps
		// --resumable` / `rnix resume <uuid>` never attempt to spawn a virtual
		// node (Epic 42 treats Dead as resumable, so this must be explicit).
		if d.Synthetic {
			continue
		}
		// Require either steps.jsonl OR checkpoint.json — both are valid resume
		// sources. The kernel.Resume path prefers checkpoint > history, so an
		// entry is resumable if at least one is present (Epic 42 fix).
		_, stepsErr := os.Stat(filepath.Join(uuidDir, "steps.jsonl"))
		_, cpErr := os.Stat(filepath.Join(uuidDir, "checkpoint.json"))
		if stepsErr != nil && cpErr != nil {
			continue
		}
		infos = append(infos, procInfoFromDisk(d))
	}
	// Most-recent-first ordering: prefer DeadAt, fall back to CreatedAt.
	sort.SliceStable(infos, func(i, j int) bool {
		ti := infos[i].DeadAt
		if ti.IsZero() {
			ti = infos[i].CreatedAt
		}
		tj := infos[j].DeadAt
		if tj.IsZero() {
			tj = infos[j].CreatedAt
		}
		return ti.After(tj)
	})
	return infos, nil
}
