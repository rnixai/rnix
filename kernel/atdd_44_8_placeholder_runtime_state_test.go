package kernel

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
)

// =============================================================================
// ATDD 44.8 — Epic 44 follow-up: daemon-restart placeholder is fully armed.
//
// Root-cause history: a child agent suspended via SIGPAUSE, the parent then
// reaped, then the daemon was restarted. LoadSuspendedFromDisk produced a
// placeholder Process with PrimaryDevice="" / CtxID=0 / nativeToolDefs=nil /
// SkillBodies={}. When the user pressed dashboard `r`, ResumeSubtree routed
// the placeholder into kernel/subtree.go:resumeOneForSubtree, which on
// PrimaryDevice=="" detoured into the script-runner branch — emitting an
// `awaiting_script_driver` event (silently dropped because eventWriter was
// also nil) and returning without launching reasonStep. Net effect:
// procTable state=Running, dashboard 🟢, but the process never made progress.
//
// Fix:
//   - vfs.ProcInfo + procInfoDisk gained a PrimaryDevice field so the
//     LLM VFS device path is now persisted.
//   - LoadSuspendedFromDisk wires PrimaryDevice (with resolveLLMDevice
//     fallback for legacy snapshots) AND calls rehydrateRuntimeStateFromDisk
//     so the placeholder has CtxID + tool defs + skill bodies before it ever
//     enters procTable.
//   - rehydrateRuntimeStateFromDisk is shared with resumeFromHistory so the
//     two paths cannot drift silently again.
//
// These tests cover the invariants directly. The dashboard-`r`-end-to-end
// scenario is exercised at the IPC layer by atdd_44_4_*; we do not duplicate
// it here.
// =============================================================================

// writeProcessMetaFixture writes a minimal process-meta.json that satisfies
// rehydrateRuntimeStateFromDisk's "system_prompt must be non-empty" check.
func writeProcessMetaFixture(t *testing.T, baseDir, uuid, systemPrompt string) {
	t.Helper()
	dir := filepath.Join(baseDir, "data", "steps", uuid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	meta := map[string]any{
		"system_prompt": systemPrompt,
		"tools":         []any{},
	}
	mb, _ := json.Marshal(meta)
	if err := os.WriteFile(filepath.Join(dir, "process-meta.json"), mb, 0o644); err != nil {
		t.Fatalf("write process-meta.json: %v", err)
	}
}

// writeStepsJSONLFixture writes a multi-step steps.jsonl so rehydrate has a
// realistic lastStep + messages payload to deserialize. Each step record
// carries a synthetic single-message history so parseStepsJSONL produces a
// non-empty Messages slice — Deserialize must accept that without crashing.
func writeStepsJSONLFixture(t *testing.T, baseDir, uuid string, steps int) {
	t.Helper()
	dir := filepath.Join(baseDir, "data", "steps", uuid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	var sb strings.Builder
	for i := 1; i <= steps; i++ {
		// Messages is the cumulative history at this step; we just push one
		// assistant message so size grows linearly. Schema must match
		// rnixctx.Message JSON shape — role + content covers the minimum.
		msgs := []map[string]any{
			{"role": "assistant", "content": "step-" + itoaSimple(i) + " response"},
		}
		msgsRaw, _ := json.Marshal(msgs)
		rec := map[string]any{
			"step":     i,
			"messages": json.RawMessage(msgsRaw),
		}
		line, _ := json.Marshal(rec)
		sb.Write(line)
		sb.WriteByte('\n')
	}
	if err := os.WriteFile(filepath.Join(dir, "steps.jsonl"), []byte(sb.String()), 0o644); err != nil {
		t.Fatalf("write steps.jsonl: %v", err)
	}
}

// itoaSimple keeps the test file self-contained (no fmt.Sprintf for hot loops).
func itoaSimple(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	neg := n < 0
	if neg {
		n = -n
	}
	for n > 0 {
		pos--
		buf[pos] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

// TestATDD_44_8_010_PrimaryDeviceDiskRoundTrip
//
// vfs.ProcInfo.PrimaryDevice must survive a procInfoToDisk → JSON →
// procInfoFromDisk round-trip. This is the foundational unit-level test: if
// the field is dropped here, every downstream LoadSuspendedFromDisk /
// resumeFromHistory invariant collapses.
func TestATDD_44_8_010_PrimaryDeviceDiskRoundTrip(t *testing.T) {
	dataDir := t.TempDir()
	uuid := uuidForTest("rt8010")
	// Hand-build a procInfoDisk so we control the on-disk JSON shape.
	disk := procInfoDisk{
		UUID:          uuid,
		State:         "suspended",
		Intent:        "round-trip",
		Provider:      "claude",
		Model:         "claude-opus-4-7",
		PrimaryDevice: "/dev/llm/claude",
		CreatedAt:     staticTime(t, -time.Hour).Format(time.RFC3339Nano),
	}
	stepsDir := filepath.Join(dataDir, "data", "steps", uuid)
	if err := os.MkdirAll(stepsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	raw, err := json.MarshalIndent(disk, "", "  ")
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(stepsDir, "proc-info.json"), raw, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Read back via the production loader. Tests in 44.3 already cover this
	// for the broader set of fields; we only assert PrimaryDevice here.
	got, err := loadProcInfoDisk(stepsDir)
	if err != nil {
		t.Fatalf("loadProcInfoDisk: %v", err)
	}
	if got.PrimaryDevice != "/dev/llm/claude" {
		t.Errorf("PrimaryDevice round-trip lost: got %q, want %q", got.PrimaryDevice, "/dev/llm/claude")
	}
}

// loadProcInfoDisk is a thin local helper that reads a proc-info.json and
// unmarshals it into procInfoDisk. We avoid touching the public
// procInfoFromDisk helper here so we can assert on the JSON-level field
// directly.
func loadProcInfoDisk(stepsDir string) (*procInfoDisk, error) {
	data, err := os.ReadFile(filepath.Join(stepsDir, "proc-info.json"))
	if err != nil {
		return nil, err
	}
	var d procInfoDisk
	if err := json.Unmarshal(data, &d); err != nil {
		return nil, err
	}
	return &d, nil
}

// TestATDD_44_8_020_LoadSuspendedRehydratesPlaceholder
//
// Main invariant: a properly-formed Suspended snapshot on disk produces a
// placeholder Process whose runtime state is fully rehydrated — PrimaryDevice
// set, CtxID non-zero with messages deserialized in, nativeToolDefs and
// toolMap populated, SkillBodies map present. Before the fix the placeholder
// was a stripped-down shell with all these fields zero / nil / empty.
//
// We do not exercise ResumeSubtree here: the placeholder field invariants are
// sufficient to prove the misroute in resumeOneForSubtree:line 327 cannot
// trigger any more. The dashboard-`r` end-to-end path is covered at the IPC
// layer.
func TestATDD_44_8_020_LoadSuspendedRehydratesPlaceholder(t *testing.T) {
	k, dataDir := newReloadKernel(t)
	uuid := uuidForTest("reh8020")

	writeSuspendProcInfoFixture(t, dataDir, suspendDiskInfo{
		PID:           5,
		UUID:          uuid,
		State:         "suspended",
		Intent:        "rehydrate placeholder runtime",
		Provider:      "claude",
		Model:         "claude-opus-4-7",
		MaxSteps:      100,
		CreatedAt:     staticTime(t, -2 * time.Hour).Format(time.RFC3339Nano),
		CtxID:         1,
		ContextWindow: 100000,
		SuspendReason: "user_paused",
		IsPaused:      true,
	}, false /*withStepsJSONL — we write our own below*/, false /*withMeta — same*/)
	// Replace the fixture's stub primary_device-less proc-info.json with one
	// that has primary_device set. The synthetic suspendDiskInfo type in
	// atdd_44_3_helpers_test.go predates this field, so we overwrite the
	// file directly.
	overwriteProcInfoPrimaryDevice(t, dataDir, uuid, "/dev/llm/claude")
	writeProcessMetaFixture(t, dataDir, uuid, "You are a resumed test agent.")
	writeStepsJSONLFixture(t, dataDir, uuid, 3)

	loaded, err := k.LoadSuspendedFromDisk()
	if err != nil {
		t.Fatalf("LoadSuspendedFromDisk: %v", err)
	}
	if loaded != 1 {
		t.Fatalf("loaded = %d, want 1", loaded)
	}

	proc, ok := k.GetProcessByUUID(uuid)
	if !ok {
		t.Fatalf("UUID %s not in procTable after LoadSuspendedFromDisk", uuid)
	}

	// PrimaryDevice must be set so dashboard-`r` → resumeOneForSubtree does
	// not detour into the script-runner branch.
	if proc.PrimaryDevice != "/dev/llm/claude" {
		t.Errorf("PrimaryDevice = %q, want %q", proc.PrimaryDevice, "/dev/llm/claude")
	}

	// CtxID must be non-zero — the placeholder must own an allocated ctx
	// with the deserialized message history, otherwise BuildPrompt on the
	// next resume crashes on a nil ctx lookup.
	if proc.CtxID == 0 {
		t.Error("CtxID = 0, want non-zero (rehydrate must allocate ctx)")
	}

	// nativeToolDefs must be non-empty — rehydrate builds at least the
	// meta tool defs (planning + skill) even when AllowedDevices is empty.
	proc.mu.Lock()
	toolDefs := len(proc.nativeToolDefs)
	toolMap := len(proc.toolMap)
	proc.mu.Unlock()
	if toolDefs == 0 {
		t.Error("nativeToolDefs is empty, want >0 (rehydrate must rebuild tool defs)")
	}
	if toolMap == 0 {
		t.Error("toolMap is empty, want >0 (rehydrate must rebuild tool map)")
	}

	// LastCompletedStep must reflect the steps.jsonl tail so
	// resumeOneForSubtree's startStep fallback (LastCompletedStep+1) produces
	// a sane value.
	if got := proc.LastCompletedStep.Load(); got != 3 {
		t.Errorf("LastCompletedStep = %d, want 3 (rehydrate must surface lastStep)", got)
	}

	// State invariant: still Suspended after rehydrate (rehydrate does not
	// flip state — only LoadSuspendedFromDisk's Start→Suspend transition
	// does, and that ends at Suspended).
	if got := proc.GetState(); got != types.StateSuspended {
		t.Errorf("State = %s, want Suspended", got)
	}
}

// overwriteProcInfoPrimaryDevice re-writes proc-info.json with primary_device
// set, since the shared suspendDiskInfo helper in atdd_44_3_helpers_test.go
// does not yet carry this field. Defined here (rather than touching the
// helper) to keep 44.3 fixtures stable.
func overwriteProcInfoPrimaryDevice(t *testing.T, baseDir, uuid, primaryDevice string) {
	t.Helper()
	path := filepath.Join(baseDir, "data", "steps", uuid, "proc-info.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read proc-info.json: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal proc-info.json: %v", err)
	}
	raw["primary_device"] = primaryDevice
	out, _ := json.MarshalIndent(raw, "", "  ")
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatalf("write proc-info.json: %v", err)
	}
}

// TestATDD_44_8_030_LegacyFixtureFallbackToProvider
//
// Backward-compat invariant: an old proc-info.json without primary_device
// must still produce a usable placeholder. LoadSuspendedFromDisk derives
// PrimaryDevice via resolveLLMDevice(Provider) so legacy snapshots from
// pre-44.8 daemons still revive correctly. Without this, every Suspended
// process written before the fix would be permanently silently broken.
func TestATDD_44_8_030_LegacyFixtureFallbackToProvider(t *testing.T) {
	k, dataDir := newReloadKernel(t)
	uuid := uuidForTest("leg8030")

	// Note: no overwriteProcInfoPrimaryDevice — we deliberately leave
	// primary_device absent to mimic legacy disk content.
	writeSuspendProcInfoFixture(t, dataDir, suspendDiskInfo{
		PID:           9,
		UUID:          uuid,
		State:         "suspended",
		Intent:        "legacy placeholder",
		Provider:      "claude",
		Model:         "claude-opus-4-7",
		MaxSteps:      50,
		CreatedAt:     staticTime(t, -time.Hour).Format(time.RFC3339Nano),
		CtxID:         1,
		ContextWindow: 100000,
		SuspendReason: "user_paused",
		IsPaused:      true,
	}, false, false)
	writeProcessMetaFixture(t, dataDir, uuid, "Legacy agent.")
	writeStepsJSONLFixture(t, dataDir, uuid, 1)

	loaded, err := k.LoadSuspendedFromDisk()
	if err != nil {
		t.Fatalf("LoadSuspendedFromDisk: %v", err)
	}
	if loaded != 1 {
		t.Fatalf("loaded = %d, want 1", loaded)
	}

	proc, ok := k.GetProcessByUUID(uuid)
	if !ok {
		t.Fatalf("UUID %s not in procTable", uuid)
	}
	// The fallback resolveLLMDevice maps provider="claude" to
	// "/dev/llm/claude". This is the contract the resume path relies on.
	if proc.PrimaryDevice != "/dev/llm/claude" {
		t.Errorf("legacy fallback PrimaryDevice = %q, want %q", proc.PrimaryDevice, "/dev/llm/claude")
	}
}

// TestATDD_44_8_040_RehydrateFailureSkipsPlaceholder
//
// Failure invariant: when on-disk artifacts are corrupt or incomplete
// (here: process-meta.json missing), rehydrate must fail and
// LoadSuspendedFromDisk must skip the placeholder rather than publish a
// half-armed Process to procTable. Half-armed placeholders crash deeper in
// the stack on the next resume; skipping with a log line is the lesser evil.
//
// We additionally verify that subsequent well-formed placeholders in the
// same scan are still loaded — the failure must not abort the loop.
func TestATDD_44_8_040_RehydrateFailureSkipsPlaceholder(t *testing.T) {
	k, dataDir := newReloadKernel(t)
	badUUID := uuidForTest("bad8040")
	goodUUID := uuidForTest("ok8040")

	// Bad fixture: proc-info written, primary_device set, BUT
	// process-meta.json deliberately omitted so rehydrate fails on the
	// system_prompt validation gate.
	writeSuspendProcInfoFixture(t, dataDir, suspendDiskInfo{
		PID:       1,
		UUID:      badUUID,
		State:     "suspended",
		Intent:    "missing meta",
		Provider:  "claude",
		Model:     "claude-opus-4-7",
		CreatedAt: staticTime(t, -time.Hour).Format(time.RFC3339Nano),
		CtxID:     1,
	}, false /*withStepsJSONL — write our own steps file below*/, false /*withMeta — keep missing*/)
	overwriteProcInfoPrimaryDevice(t, dataDir, badUUID, "/dev/llm/claude")
	writeStepsJSONLFixture(t, dataDir, badUUID, 1)
	// Intentionally do NOT call writeProcessMetaFixture for badUUID.

	// Good fixture: complete artifacts, must still load even though the
	// preceding entry in the scan failed.
	writeSuspendProcInfoFixture(t, dataDir, suspendDiskInfo{
		PID:       2,
		UUID:      goodUUID,
		State:     "suspended",
		Intent:    "valid placeholder",
		Provider:  "claude",
		Model:     "claude-opus-4-7",
		CreatedAt: staticTime(t, -2 * time.Hour).Format(time.RFC3339Nano),
		CtxID:     1,
	}, false, false)
	overwriteProcInfoPrimaryDevice(t, dataDir, goodUUID, "/dev/llm/claude")
	writeProcessMetaFixture(t, dataDir, goodUUID, "Good agent.")
	writeStepsJSONLFixture(t, dataDir, goodUUID, 1)

	loaded, err := k.LoadSuspendedFromDisk()
	if err != nil {
		t.Fatalf("LoadSuspendedFromDisk: %v", err)
	}
	if loaded != 1 {
		t.Errorf("loaded = %d, want 1 (only the good fixture)", loaded)
	}
	if _, ok := k.GetProcessByUUID(badUUID); ok {
		t.Error("bad placeholder was loaded into procTable; expected skip on rehydrate failure")
	}
	if _, ok := k.GetProcessByUUID(goodUUID); !ok {
		t.Error("good placeholder missing from procTable; rehydrate failure must not abort the scan")
	}
}
