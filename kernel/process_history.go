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
	for i := len(h.entries) - 1; i >= 0; i-- {
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

	for i := len(h.entries) - 1; i >= 0; i-- {
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

// ---------------------------------------------------------------------------
// Disk persistence for proc-info.json
// ---------------------------------------------------------------------------

// procInfoDisk is the JSON-serializable representation of ProcInfo on disk.
type procInfoDisk struct {
	PID            uint64   `json:"pid"`
	UUID           string   `json:"uuid"`
	OriginUUID     string   `json:"origin_uuid,omitempty"`
	ResumedFromStep int     `json:"resumed_from_step,omitempty"`
	ExitReason     string   `json:"exit_reason,omitempty"`
	CtxSize        int      `json:"ctx_size,omitempty"`
	PPID           uint64   `json:"ppid"`
	ParentUUID     string   `json:"parent_uuid,omitempty"`
	State          string   `json:"state"`
	Intent         string   `json:"intent"`
	Skills         []string `json:"skills,omitempty"`
	TokensUsed     int      `json:"tokens_used"`
	ContextBudget  int      `json:"context_budget,omitempty"`
	MaxSteps       int      `json:"max_steps,omitempty"`
	CreatedAt      string   `json:"created_at"`
	DeadAt         string   `json:"dead_at,omitempty"`
	CtxID          uint64   `json:"ctx_id"`
	Result         string   `json:"result,omitempty"`
	AllowedDevices []string `json:"allowed_devices,omitempty"`
	Provider       string   `json:"provider,omitempty"`
	Model          string   `json:"model,omitempty"`
	ContextWindow  int      `json:"context_window,omitempty"`
	ComposeNode    string   `json:"compose_node,omitempty"`
	ComposeDeps    []string `json:"compose_deps,omitempty"`
	PipelineIndex  int      `json:"pipeline_index"`
	PipelineTotal  int      `json:"pipeline_total"`
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
		State:          info.State.String(),
		Intent:         info.Intent,
		Skills:         info.Skills,
		TokensUsed:     info.TokensUsed,
		ContextBudget:  info.ContextBudget,
		MaxSteps:       info.MaxSteps,
		CreatedAt:      info.CreatedAt.Format(time.RFC3339Nano),
		CtxID:          uint64(info.CtxID),
		Result:         info.Result,
		AllowedDevices: info.AllowedDevices,
		Provider:       info.Provider,
		Model:          info.Model,
		ContextWindow:  info.ContextWindow,
		ComposeNode:    info.ComposeNode,
		ComposeDeps:    append([]string(nil), info.ComposeDeps...),
		PipelineIndex:  info.PipelineIndex,
		PipelineTotal:  info.PipelineTotal,
	}
	if !info.DeadAt.IsZero() {
		d.DeadAt = info.DeadAt.Format(time.RFC3339Nano)
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
		State:          parseProcessState(d.State),
		Intent:         d.Intent,
		Skills:         d.Skills,
		TokensUsed:     d.TokensUsed,
		ContextBudget:  d.ContextBudget,
		MaxSteps:       d.MaxSteps,
		CtxID:          types.CtxID(d.CtxID),
		Result:         d.Result,
		AllowedDevices: d.AllowedDevices,
		Provider:       d.Provider,
		Model:          d.Model,
		ContextWindow:  d.ContextWindow,
		ComposeNode:    d.ComposeNode,
		ComposeDeps:    d.ComposeDeps,
		PipelineIndex:  d.PipelineIndex,
		PipelineTotal:  d.PipelineTotal,
	}
	if d.CreatedAt != "" {
		info.CreatedAt, _ = time.Parse(time.RFC3339Nano, d.CreatedAt)
	}
	if d.DeadAt != "" {
		info.DeadAt, _ = time.Parse(time.RFC3339Nano, d.DeadAt)
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
	default:
		return types.StateDead
	}
}

// procInfoFilename is the name of the ProcInfo snapshot file.
const procInfoFilename = "proc-info.json"

// SaveProcInfo writes a ProcInfo snapshot to <baseDir>/data/steps/<uuid>/proc-info.json.
// Best-effort: returns nil silently if baseDir or UUID is empty.
func SaveProcInfo(baseDir string, info vfs.ProcInfo) error {
	if baseDir == "" || info.UUID == "" {
		return nil
	}
	dir := filepath.Join(baseDir, "data", "steps", info.UUID)
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
	if baseDir == "" {
		return NewProcessHistory(maxSize), nil
	}

	stepsDir := filepath.Join(baseDir, "data", "steps")
	entries, err := os.ReadDir(stepsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return NewProcessHistory(maxSize), nil
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

	if len(infos) > maxSize {
		infos = infos[len(infos)-maxSize:]
	}

	return &ProcessHistory{
		entries: infos,
		maxSize: maxSize,
	}, nil
}

// ListResumable scans <baseDir>/data/steps/*/proc-info.json and returns snapshots
// whose state is "running" (i.e., processes left over from daemon crashes that
// did not transition to Zombie/Dead before the daemon died).
//
// Corrupt JSON files are logged and skipped. Missing baseDir or steps dir returns
// nil, nil (consistent with LoadProcHistory's behavior).
func ListResumable(baseDir string) ([]vfs.ProcInfo, error) {
	if baseDir == "" {
		return nil, nil
	}
	stepsDir := filepath.Join(baseDir, "data", "steps")
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
		if d.UUID == "" || d.State != "running" {
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
