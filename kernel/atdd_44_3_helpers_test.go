package kernel

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
)

// =============================================================================
// ATDD 44.3 — Shared helpers for the "persist suspended state + daemon
// restart no-auto-resume" story.
//
// Conventions:
//   - File-isolated: helpers only used by atdd_44_3_*_test.go.
//   - Helpers call t.Helper() so failures point at the call site.
//   - Disk fixtures are written by raw json.Marshal of a synthetic on-disk
//     proc-info structure — NOT by calling procInfoToDisk(). This makes the
//     fixtures independent of dev-story Task 1 landing first; tests assert
//     the post-Task-1 invariants by reading the on-disk JSON back through
//     the real procInfoFromDisk() / LoadSuspendedFromDisk().
//   - SuspendReason example values follow Story 44.3 §"Epic 45 解耦红线":
//     use "user_paused" / "cli_disconnected" / "" / "reason_for_test_only".
//     The string "heartbeat_timeout" is intentionally NEVER used as an
//     example — it will be enforced by a grep audit in Step 5 / Task 8.3.
// =============================================================================

// newReloadKernel returns a kernel with stepDataDir set to a per-test TempDir,
// suitable for LoadSuspendedFromDisk / SaveProcInfo round-trip tests.
//
// Why a dedicated helper instead of newSimpleKernel:
//   - LoadSuspendedFromDisk relies on a non-empty stepDataDir; newSimpleKernel
//     leaves it empty.
//   - Several 44.3 ATDD tests rebuild a fresh KernelImpl on the same
//     stepDataDir to emulate a "daemon restart"; this helper standardises the
//     directory wiring so those tests stay readable.
func newReloadKernel(t *testing.T) (*KernelImpl, string) {
	t.Helper()
	k := newSimpleKernel(t)
	dataDir := t.TempDir()
	k.SetStepDataDir(dataDir)
	return k, dataDir
}

// suspendDiskInfo describes the JSON we want a synthetic .rnix/data/steps/
// proc-info.json fixture to contain. Mirrors the post-Task-1 procInfoDisk
// shape but is defined here so the fixture file can be written without
// depending on procInfoDisk being exported / having the new fields yet.
type suspendDiskInfo struct {
	PID            uint64   `json:"pid"`
	UUID           string   `json:"uuid"`
	OriginUUID     string   `json:"origin_uuid,omitempty"`
	PPID           uint64   `json:"ppid"`
	ParentUUID     string   `json:"parent_uuid,omitempty"`
	State          string   `json:"state"`
	Intent         string   `json:"intent"`
	Skills         []string `json:"skills,omitempty"`
	TokensUsed     int      `json:"tokens_used"`
	MaxSteps       int      `json:"max_steps,omitempty"`
	CreatedAt      string   `json:"created_at"`
	DeadAt         string   `json:"dead_at,omitempty"`
	CtxID          uint64   `json:"ctx_id"`
	AllowedDevices []string `json:"allowed_devices,omitempty"`
	Provider       string   `json:"provider,omitempty"`
	Model          string   `json:"model,omitempty"`
	ContextWindow  int      `json:"context_window,omitempty"`
	// Story 44.3 AC#1 new fields — the very symbols whose absence makes
	// kernel/atdd_44_3_proc_info_disk_test.go fail to compile pre-Task-1.
	SuspendReason string `json:"suspend_reason,omitempty"`
	PausedAt      string `json:"paused_at,omitempty"`
	IsPaused      bool   `json:"is_paused,omitempty"`
	PausedTotalMs int64  `json:"paused_total_ms,omitempty"`
}

// writeSuspendProcInfoFixture writes a synthetic proc-info.json + an empty
// steps.jsonl + a minimal process-meta.json into
// <baseDir>/data/steps/<uuid>/.
// The minimal companion files satisfy ListResumable's filter
// (steps.jsonl OR checkpoint.json must exist) and resumeFromHistory's read
// of process-meta.json. Tests that only exercise the disk → procInfoFromDisk
// path can skip writing them by setting the booleans to false.
func writeSuspendProcInfoFixture(t *testing.T, baseDir string, info suspendDiskInfo, withStepsJSONL, withMeta bool) {
	t.Helper()
	if info.UUID == "" {
		t.Fatal("writeSuspendProcInfoFixture: empty UUID")
	}
	dir := filepath.Join(baseDir, "data", "steps", info.UUID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	data, err := json.MarshalIndent(info, "", "  ")
	if err != nil {
		t.Fatalf("marshal suspendDiskInfo: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "proc-info.json"), data, 0o644); err != nil {
		t.Fatalf("write proc-info.json: %v", err)
	}
	if withStepsJSONL {
		// Single placeholder step record — enough for ListResumable's filter
		// and parseStepsJSONL's lastStep computation. Schema-permissive: the
		// Messages field can be empty.
		stepLine := []byte(`{"step":1,"messages":[]}` + "\n")
		if err := os.WriteFile(filepath.Join(dir, "steps.jsonl"), stepLine, 0o644); err != nil {
			t.Fatalf("write steps.jsonl: %v", err)
		}
	}
	if withMeta {
		meta := map[string]any{
			"system_prompt": "You are a resumed test agent.",
			"tools":         []any{},
		}
		mb, _ := json.Marshal(meta)
		if err := os.WriteFile(filepath.Join(dir, "process-meta.json"), mb, 0o644); err != nil {
			t.Fatalf("write process-meta.json: %v", err)
		}
	}
}

// readProcInfoFromDisk44_3 reads <baseDir>/data/steps/<uuid>/proc-info.json
// into a raw map so tests can assert on JSON fields without depending on Go
// type definitions. Returns nil + t.Fatal on read/parse error so test
// callers can dereference unchecked.
//
// Suffix "_44_3" disambiguates from kernel/resume_lineage.go's
// readProcInfoFromDisk(stepDataDir, uuid) helper which returns *vfs.ProcInfo
// — different return type, different purpose.
func readProcInfoFromDisk44_3(t *testing.T, baseDir, uuid string) map[string]any {
	t.Helper()
	path := filepath.Join(baseDir, "data", "steps", uuid, "proc-info.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal %s: %v", path, err)
	}
	return raw
}

// staticTime returns a frozen time at the given offset relative to a
// deterministic anchor. Used so PausedAt / CreatedAt fixtures stay stable
// across test runs.
func staticTime(t *testing.T, offset time.Duration) time.Time {
	t.Helper()
	anchor, err := time.Parse(time.RFC3339Nano, "2026-05-22T12:00:00.000000000Z")
	if err != nil {
		t.Fatalf("anchor parse: %v", err)
	}
	return anchor.Add(offset)
}

// uuidForTest returns a deterministic 36-char UUID-shaped string built from
// the supplied tag. Avoids needing to import a UUID library. The returned
// value satisfies kernel/resume.go:isValidUUIDFormat (only alphanumeric + '-').
func uuidForTest(tag string) string {
	// Pad / truncate tag to a fixed shape: 8-4-4-4-12.
	// Hashing avoided to keep fixture files diff-friendly for debugging.
	pad := func(s string, n int) string {
		if len(s) >= n {
			return s[:n]
		}
		out := make([]byte, n)
		copy(out, s)
		for i := len(s); i < n; i++ {
			out[i] = '0'
		}
		return string(out)
	}
	return pad(tag, 8) + "-" + pad("4433", 4) + "-" + pad("44a3", 4) + "-" + pad("44a3", 4) + "-" + pad(tag+"44a3", 12)
}

// drainAllEvents pulls every event currently queued on proc.DebugChan,
// returning quickly when the channel drains. Unlike drainDebugChan44_1 it
// does not block on a deadline once the channel is empty — useful for
// "assert no event of kind X was emitted within window" tests where we want
// the post-window snapshot.
func drainAllEvents(t *testing.T, proc *Process, window time.Duration) []types.SyscallEvent {
	t.Helper()
	deadline := time.After(window)
	out := make([]types.SyscallEvent, 0, 8)
	for {
		select {
		case ev := <-proc.DebugChan:
			out = append(out, ev)
		case <-deadline:
			return out
		}
	}
}
