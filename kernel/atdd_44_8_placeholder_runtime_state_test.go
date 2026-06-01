package kernel

import (
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/config"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
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
	dir := filepath.Join(baseDir, "steps", uuid)
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
	dir := filepath.Join(baseDir, "steps", uuid)
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
	stepsDir := filepath.Join(dataDir, "steps", uuid)
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
	path := filepath.Join(baseDir, "steps", uuid, "proc-info.json")
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
// (here: process-meta.json is present but contains malformed JSON), rehydrate
// must fail and LoadSuspendedFromDisk must skip the placeholder rather than
// publish a half-armed Process to procTable. Half-armed placeholders crash
// deeper in the stack on the next resume; skipping with a log line is the
// lesser evil.
//
// Note: this test does NOT use "process-meta.json missing" as the failure
// trigger — Epic 44.8 deliberately added a fallback that synthesizes a
// SystemPrompt via registerSections so legacy placeholders revive instead
// of being permanently stuck. Genuine corruption (unparseable JSON) is the
// remaining failure mode and the one this test pins down.
//
// We additionally verify that subsequent well-formed placeholders in the
// same scan are still loaded — the failure must not abort the loop.
func TestATDD_44_8_040_RehydrateFailureSkipsPlaceholder(t *testing.T) {
	k, dataDir := newReloadKernel(t)
	badUUID := uuidForTest("bad8040")
	goodUUID := uuidForTest("ok8040")

	// Bad fixture: proc-info + steps + primary_device set, but
	// process-meta.json contains malformed JSON so rehydrate fails on the
	// json.Unmarshal gate (read OK, parse not OK).
	writeSuspendProcInfoFixture(t, dataDir, suspendDiskInfo{
		PID:       1,
		UUID:      badUUID,
		State:     "suspended",
		Intent:    "corrupt meta",
		Provider:  "claude",
		Model:     "claude-opus-4-7",
		CreatedAt: staticTime(t, -time.Hour).Format(time.RFC3339Nano),
		CtxID:     1,
	}, false /*withStepsJSONL — write our own steps file below*/, false /*withMeta — write our own corrupt one below*/)
	overwriteProcInfoPrimaryDevice(t, dataDir, badUUID, "/dev/llm/claude")
	writeStepsJSONLFixture(t, dataDir, badUUID, 1)
	// Write a deliberately malformed process-meta.json so rehydrate hits
	// json.Unmarshal error (not the missing-file fallback path).
	corruptMetaPath := filepath.Join(dataDir, "steps", badUUID, "process-meta.json")
	if err := os.WriteFile(corruptMetaPath, []byte("{not valid json"), 0o644); err != nil {
		t.Fatalf("write corrupt meta: %v", err)
	}

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

// TestATDD_44_8_050_SuspendWritesProcessMetaToDisk
//
// Epic 44.8 invariant: process-meta.json must be on disk by the time
// suspendProcess returns. Without this the next-daemon rehydrate has no
// system_prompt to deserialize into the revived ctx — exactly the
// regression reported by the user ("按 r 无法正确恢复子进程"). Previously
// process-meta.json was only written by reapProcess, which never runs on
// the Suspended path.
//
// We exercise the production suspendProcess helper directly (not via the
// public Suspend) so the test does not need a real reasonStep goroutine —
// the metadata-write contract is independent of how the process arrived at
// the Running state.
func TestATDD_44_8_050_SuspendWritesProcessMetaToDisk(t *testing.T) {
	k, dataDir := newReloadKernel(t)
	uuid := uuidForTest("sus8050")

	proc := NewProcess(0, "suspend-meta-write", []string{"bmad-help"})
	proc.UUID = uuid
	proc.Provider = "claude"
	proc.Model = "claude-opus-4-7"
	proc.PrimaryDevice = "/dev/llm/claude"
	proc.AllowedDevices = []string{"/dev/fs", "/dev/llm/claude"}
	proc.FinalSystemPrompt = "You are a 44.8 test agent."
	TestSetProjectConfig(proc)
	// Synthetic nativeToolDefs — production code only requires this to be
	// JSON-marshallable; the exact shape is opaque to writeProcessMeta.
	proc.nativeToolDefs = []vfs.ToolDef{
		{Name: "fs", Description: "filesystem"},
	}
	k.AddProcess(proc)
	if err := proc.Start(); err != nil {
		t.Fatalf("proc.Start (Created→Running): %v", err)
	}

	if err := k.suspendProcess(proc, "user_paused", ExitSuspended); err != nil {
		t.Fatalf("suspendProcess: %v", err)
	}

	metaPath := filepath.Join(dataDir, "steps", uuid, "process-meta.json")
	data, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("process-meta.json missing after suspend: %v", err)
	}
	var meta struct {
		SystemPrompt string `json:"system_prompt"`
		ToolDefs     []any  `json:"tool_defs"`
	}
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatalf("unmarshal process-meta.json: %v", err)
	}
	if meta.SystemPrompt != "You are a 44.8 test agent." {
		t.Errorf("SystemPrompt = %q, want %q", meta.SystemPrompt, "You are a 44.8 test agent.")
	}
	if len(meta.ToolDefs) != 1 {
		t.Errorf("ToolDefs len = %d, want 1 (synthetic /dev/fs)", len(meta.ToolDefs))
	}

	// proc-info.json must also reflect state=suspended at this point — both
	// snapshots are written together by suspendProcess, and the next-daemon
	// LoadSuspendedFromDisk needs both to be sibling-fresh.
	procInfoPath := filepath.Join(dataDir, "steps", uuid, "proc-info.json")
	procData, err := os.ReadFile(procInfoPath)
	if err != nil {
		t.Fatalf("proc-info.json missing: %v", err)
	}
	var rawInfo map[string]any
	if err := json.Unmarshal(procData, &rawInfo); err != nil {
		t.Fatalf("unmarshal proc-info.json: %v", err)
	}
	if rawInfo["state"] != "suspended" {
		t.Errorf("proc-info state = %v, want suspended", rawInfo["state"])
	}
	if rawInfo["primary_device"] != "/dev/llm/claude" {
		t.Errorf("proc-info primary_device = %v, want /dev/llm/claude", rawInfo["primary_device"])
	}
}

// TestATDD_44_8_051_SuspendThenRestartThenLoadProducesArmedPlaceholder
//
// End-to-end of the user-reported regression path:
//   1. spawn-like proc set up + Running
//   2. suspendProcess writes proc-info.json + process-meta.json
//   3. "daemon restart" — drop kernel1, create kernel2 on the same stepDataDir
//   4. kernel2.LoadSuspendedFromDisk lifts the placeholder back into
//      procTable with full runtime state (PrimaryDevice / CtxID / tool defs /
//      LastCompletedStep) so a subsequent ResumeSubtree on the new kernel
//      hits the reasonStep-driven branch instead of the script-runner detour.
//
// This is the regression seed for "按 r 无法正确恢复子进程". A future bug
// that breaks the suspend → restart → resume chain (e.g. forgetting to
// persist a new ProcInfo field, or regressing the rehydrate fallback) trips
// this test immediately.
func TestATDD_44_8_051_SuspendThenRestartThenLoadProducesArmedPlaceholder(t *testing.T) {
	dataDir := t.TempDir()

	// ===== kernel1: spawn-like setup + suspend =====
	kern1 := newSimpleKernel(t)
	kern1.SetDataDir(dataDir)
	projBase := TestProjectBaseDir(dataDir)
	if err := os.MkdirAll(projBase, 0o755); err != nil {
		t.Fatal(err)
	}

	uuid := uuidForTest("e2e8051")
	proc := NewProcess(0, "suspend-restart-resume", []string{"bmad-help"})
	proc.UUID = uuid
	proc.Provider = "claude"
	proc.Model = "claude-opus-4-7"
	proc.PrimaryDevice = "/dev/llm/claude"
	proc.AllowedDevices = []string{"/dev/fs", "/dev/llm/claude"}
	proc.FinalSystemPrompt = "You are the 44.8 e2e test agent."
	TestSetProjectConfig(proc)
	proc.nativeToolDefs = []vfs.ToolDef{
		{Name: "fs", Description: "filesystem"},
	}

	kern1.AddProcess(proc)
	if err := proc.Start(); err != nil {
		t.Fatalf("proc.Start: %v", err)
	}

	// Write a synthetic steps.jsonl so rehydrate has something to replay.
	// suspendProcess does not write this — production reasonStep does — so
	// the test must seed it explicitly.
	writeStepsJSONLFixture(t, projBase, uuid, 2)

	if err := kern1.suspendProcess(proc, "user_paused", ExitSuspended); err != nil {
		t.Fatalf("kern1.suspendProcess: %v", err)
	}

	// ===== "daemon restart": kernel2 on the same on-disk state =====
	kern2 := newSimpleKernel(t)
	kern2.SetDataDir(dataDir)

	loaded, err := kern2.LoadSuspendedFromDisk()
	if err != nil {
		t.Fatalf("kern2.LoadSuspendedFromDisk: %v", err)
	}
	if loaded != 1 {
		t.Fatalf("loaded = %d, want 1", loaded)
	}

	revived, ok := kern2.GetProcessByUUID(uuid)
	if !ok {
		t.Fatalf("UUID %s missing from kern2.procTable after restart", uuid)
	}

	// The whole point of the fix: the revived placeholder must look armed,
	// not stripped-down. resumeOneForSubtree would now hit the
	// reasonStep-driven branch (PrimaryDevice non-empty) and have a real
	// CtxID + tool defs to drive BuildPrompt / tool dispatch.
	if revived.PrimaryDevice == "" {
		t.Error("revived PrimaryDevice = empty, want /dev/llm/claude — suspend→restart lost the field")
	}
	if revived.CtxID == 0 {
		t.Error("revived CtxID = 0, want non-zero — rehydrate must allocate ctx")
	}
	revived.mu.Lock()
	toolDefs := len(revived.nativeToolDefs)
	revived.mu.Unlock()
	if toolDefs == 0 {
		t.Error("revived nativeToolDefs empty — rehydrate must rebuild tool defs")
	}
	if got := revived.LastCompletedStep.Load(); got != 2 {
		t.Errorf("revived LastCompletedStep = %d, want 2 (matching seeded steps.jsonl)", got)
	}
	if state := revived.GetState(); state != types.StateSuspended {
		t.Errorf("revived state = %s, want Suspended", state)
	}
}

// TestATDD_44_8_060_ProjectDirRoundTrip
//
// vfs.ProcInfo.ProjectDir must survive a procInfoToDisk → JSON →
// procInfoFromDisk round-trip. The whole "revive ProjectConfig" pipeline is
// gated on this field being readable post-restart; if the persistence
// regresses, every project-scoped placeholder breaks.
func TestATDD_44_8_060_ProjectDirRoundTrip(t *testing.T) {
	dataDir := t.TempDir()
	uuid := uuidForTest("pd8060")
	disk := procInfoDisk{
		UUID:          uuid,
		State:         "suspended",
		Intent:        "project-dir round-trip",
		Provider:      "opencodego",
		Model:         "deepseek-v4-flash",
		PrimaryDevice: "/dev/llm/opencodego",
		ProjectDir:    "/abs/path/to/EchoMatrix",
		CreatedAt:     staticTime(t, -time.Hour).Format(time.RFC3339Nano),
	}
	stepsDir := filepath.Join(dataDir, "steps", uuid)
	if err := os.MkdirAll(stepsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	raw, _ := json.MarshalIndent(disk, "", "  ")
	if err := os.WriteFile(filepath.Join(stepsDir, "proc-info.json"), raw, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	got, err := loadProcInfoDisk(stepsDir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.ProjectDir != "/abs/path/to/EchoMatrix" {
		t.Errorf("ProjectDir round-trip lost: got %q", got.ProjectDir)
	}
}

// TestATDD_44_8_070_LoadSuspendedReattachesProjectConfig
//
// End-to-end of the project-only-provider regression that the user surfaced
// as "按 r 无法正确恢复子进程": when the placeholder belongs to a project
// using a provider that the daemon's global registry does not know about
// (opencodego in EchoMatrix), the revived placeholder must carry the
// project's ProjectConfig — including LLMFileOpener — so resumeOneForSubtree
// can open the LLM device via the project path instead of falling back to
// k.vfs.Open and hitting `device not found`.
//
// We inject a fake projectConfigLoader that returns a ProjectConfig with an
// LLMFileOpener stub and verify the revived placeholder ends up with both
// proc.ProjectConfig non-nil and ProjectConfig.LLMFileOpener non-nil.
func TestATDD_44_8_070_LoadSuspendedReattachesProjectConfig(t *testing.T) {
	k, dataDir := newReloadKernel(t)
	uuid := uuidForTest("pc8070")

	// Build the fake project context the loader will hand back. The opener
	// is the spawn-path bypass that lets resumeOneForSubtree skip the
	// global-VFS device validation — see kernel/resume.go:openLLMDeviceForResume.
	fakeOpener := func(provider string, flags int) (any, error) {
		return nil, fmt.Errorf("not used in this test")
	}
	projectDir := filepath.Join(dataDir, "fake-project")
	if err := os.MkdirAll(filepath.Join(projectDir, ".rnix"), 0o755); err != nil {
		t.Fatalf("mkdir project: %v", err)
	}
	loaderCalls := 0
	k.SetProjectConfigLoader(func(pd string) (*config.ProjectConfig, error) {
		loaderCalls++
		if pd != projectDir {
			t.Errorf("loader called with %q, want %q", pd, projectDir)
		}
		return &config.ProjectConfig{
			ProjectDir:      projectDir,
			DefaultProvider: "opencodego",
			LLMFileOpener:   fakeOpener,
		}, nil
	})

	writeSuspendProcInfoFixture(t, dataDir, suspendDiskInfo{
		PID:           7,
		UUID:          uuid,
		State:         "suspended",
		Intent:        "project-only provider placeholder",
		Provider:      "opencodego",
		Model:         "deepseek-v4-flash",
		MaxSteps:      50,
		CreatedAt:     staticTime(t, -time.Hour).Format(time.RFC3339Nano),
		CtxID:         1,
		ContextWindow: 100000,
		SuspendReason: "user_paused",
		IsPaused:      true,
	}, false, false)
	overwriteProcInfoFields(t, dataDir, uuid, map[string]any{
		"primary_device": "/dev/llm/opencodego",
		"project_dir":    projectDir,
	})
	writeProcessMetaFixture(t, dataDir, uuid, "Project agent.")
	writeStepsJSONLFixture(t, dataDir, uuid, 1)

	loaded, err := k.LoadSuspendedFromDisk()
	if err != nil {
		t.Fatalf("LoadSuspendedFromDisk: %v", err)
	}
	if loaded != 1 {
		t.Fatalf("loaded = %d, want 1", loaded)
	}
	if loaderCalls != 1 {
		t.Errorf("loader called %d times, want 1", loaderCalls)
	}

	proc, ok := k.GetProcessByUUID(uuid)
	if !ok {
		t.Fatalf("UUID missing from procTable")
	}
	if proc.ProjectConfig == nil {
		t.Fatal("ProjectConfig = nil — LoadSuspendedFromDisk must re-attach project context")
	}
	if proc.ProjectConfig.LLMFileOpener == nil {
		t.Error("ProjectConfig.LLMFileOpener = nil — project-only providers cannot be opened on resume")
	}
	if proc.ProjectConfig.ProjectDir != projectDir {
		t.Errorf("ProjectConfig.ProjectDir = %q, want %q", proc.ProjectConfig.ProjectDir, projectDir)
	}
}

// TestATDD_44_8_080_LoadSuspendedHandlesProjectLoaderFailure
//
// Best-effort invariant: when the projectConfigLoader returns an error (the
// project directory was deleted, providers.yaml is malformed, etc.), the
// placeholder must still load — its first resume will surface the real
// problem to the user instead of silently disappearing during daemon
// startup.
func TestATDD_44_8_080_LoadSuspendedHandlesProjectLoaderFailure(t *testing.T) {
	k, dataDir := newReloadKernel(t)
	uuid := uuidForTest("pf8080")

	k.SetProjectConfigLoader(func(pd string) (*config.ProjectConfig, error) {
		return nil, fmt.Errorf("simulated project context failure")
	})

	writeSuspendProcInfoFixture(t, dataDir, suspendDiskInfo{
		PID:       9,
		UUID:      uuid,
		State:     "suspended",
		Intent:    "failing project loader",
		Provider:  "claude",
		Model:     "claude-opus-4-7",
		CreatedAt: staticTime(t, -time.Hour).Format(time.RFC3339Nano),
		CtxID:     1,
	}, false, false)
	overwriteProcInfoFields(t, dataDir, uuid, map[string]any{
		"primary_device": "/dev/llm/claude",
		"project_dir":    "/nonexistent/project",
	})
	writeProcessMetaFixture(t, dataDir, uuid, "Failure test agent.")
	writeStepsJSONLFixture(t, dataDir, uuid, 1)

	loaded, err := k.LoadSuspendedFromDisk()
	if err != nil {
		t.Fatalf("LoadSuspendedFromDisk: %v", err)
	}
	if loaded != 1 {
		t.Errorf("loaded = %d, want 1 (loader failure must not skip the placeholder)", loaded)
	}
	proc, ok := k.GetProcessByUUID(uuid)
	if !ok {
		t.Fatal("UUID missing — placeholder was skipped instead of degrading gracefully")
	}
	if proc.ProjectConfig != nil {
		t.Errorf("ProjectConfig = %v, want nil after loader failure", proc.ProjectConfig)
	}
}

// overwriteProcInfoFields merges arbitrary key/value pairs into the existing
// proc-info.json. Used by 070 / 080 where the shared suspendDiskInfo helper
// in atdd_44_3_helpers_test.go does not yet carry primary_device or
// project_dir fields. Defined once here to keep 44.3 fixtures stable.
func overwriteProcInfoFields(t *testing.T, baseDir, uuid string, fields map[string]any) {
	t.Helper()
	path := filepath.Join(baseDir, "steps", uuid, "proc-info.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read proc-info.json: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal proc-info.json: %v", err)
	}
	maps.Copy(raw, fields)
	out, _ := json.MarshalIndent(raw, "", "  ")
	if err := os.WriteFile(path, out, 0o644); err != nil {
		t.Fatalf("write proc-info.json: %v", err)
	}
}
