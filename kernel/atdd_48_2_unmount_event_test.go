//go:build unix

package kernel

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// =============================================================================
// ATDD 48.2 AC7 — kernel.finishProcess emitEvent("Unmount") forced/duration 元数据
//
// Story: _bmad-output/implementation-artifacts/48-2-mcp-transport-graceful-shutdown.md
// Phase: TDD RED — t.Skip until Task 3.1 lands. The current finishProcess
//        implementation (kernel/reason.go:104-113) sends ONLY {"path",
//        "auto"} in the Unmount args map. This story adds:
//          • duration_ms (always)
//          • forced=true  (when Close returned ErrForceKilled OR took ≥ 4.9s)
//          • reason="sigterm_ignored" (forced path only)
//
// The graceful path also gains duration_ms (the existing test surface in
// 48.1 silently treats unknown args as "extra noise" — see
// drainMountEvents in atdd_48_1_helpers_test.go — so this is backward
// compatible for 48.1's regression tests).
//
// Compile note: use the exported `types.ErrForceKilled` constant directly.
// (Earlier RED-phase drafts used `types.ErrCode("force_killed")` literal to
// compile before Task 2.5 added the constant; code review F5 then renamed the
// underlying string to "FORCE_KILLED" so any remaining literal would diverge.)
// =============================================================================

// -----------------------------------------------------------------------------
// AC7: forced path (Close returned ErrForceKilled + slow elapsed)
// -----------------------------------------------------------------------------
func TestATDD_48_2_007_UnmountEvent_ForcedKill_AnnotatesArgs(t *testing.T) {
	// Single mount with a transport that simulates the SIGTERM-timeout path:
	// Close blocks 4.95s and returns *types.DriverError{Code: ErrForceKilled}.
	transports := map[string]*trackingMCPTransport{
		"playwright": {
			closeDelay: 4950 * time.Millisecond,
			closeErr: types.NewDriverError("Close", "/mnt/mcp/100-playwright",
				errors.New("SIGTERM ignored, force-killed"), types.ErrForceKilled),
		},
	}
	factory := newTrackingTransportFactory(transports)

	llmFile := &mockLLMFile{readData: makeLLMResponse("ac7", 1)}
	k, _, _ := newTestKernel(t, llmFile)

	baseDir := t.TempDir()
	k.SetStepDataDir(baseDir)

	devReg := k.vfs.DeviceRegistry()
	mgr := vfs.NewMountManager(devReg, factory)
	k.SetMountManager(mgr)

	const path = "/mnt/mcp/100-playwright"
	cfg := vfs.MCPConfig{
		ServerName:    "playwright",
		Command:       "/bin/echo",
		TransportType: "stdio",
	}
	if err := mgr.Mount(path, cfg); err != nil {
		t.Fatalf("Mount: %v", err)
	}

	// Build a proc the test fully controls. We DO NOT register it in
	// procTable — finishProcess does not require it. We DO need:
	//   - Running state (so Terminate transitions to Zombie cleanly)
	//   - MCPMounts populated with our mount path
	//   - EventWriter attached so emitEvent persists to events.jsonl
	proc := NewProcess(0, "AC7 forced unmount", nil)
	if err := proc.Start(); err != nil {
		t.Fatalf("proc.Start: %v", err)
	}
	proc.MCPMounts = []string{path}

	ew, err := NewEventWriter(baseDir, proc.UUID)
	if err != nil {
		t.Fatalf("NewEventWriter: %v", err)
	}
	t.Cleanup(func() { _ = ew.Close() })
	proc.AttachEventWriter(ew)

	// Trigger the production code path under test.
	start := time.Now()
	k.finishProcess(proc, ExitStatus{Code: 0, Reason: "complete"})
	elapsed := time.Since(start)

	// finishProcess synchronously walks mcpMounts (line 104-113), so the
	// 4.95s Close delay must have been waited for.
	if elapsed < 4900*time.Millisecond {
		t.Fatalf("finishProcess returned in %v — Unmount path skipped or transport mock not honoured",
			elapsed)
	}

	if err := ew.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	eventsPath := filepath.Join(baseDir, "data", "steps", proc.UUID, "events.jsonl")
	if _, err := os.Stat(eventsPath); err != nil {
		t.Fatalf("events.jsonl not created at %s: %v", eventsPath, err)
	}

	rows, err := ReadAllEvents(eventsPath)
	if err != nil {
		t.Fatalf("ReadAllEvents: %v", err)
	}

	var unmountEvt *SyscallEventDisk
	for i := range rows {
		if rows[i].Syscall == "Unmount" {
			evt := rows[i]
			unmountEvt = &evt
			break
		}
	}
	if unmountEvt == nil {
		t.Fatalf("no Unmount event in events.jsonl; got %d events", len(rows))
	}

	// path + auto (existing fields preserved)
	if got, _ := unmountEvt.Args["path"].(string); got != path {
		t.Errorf("Unmount args.path = %q, want %q", got, path)
	}
	if got, _ := unmountEvt.Args["auto"].(bool); !got {
		t.Errorf("Unmount args.auto = %v, want true", unmountEvt.Args["auto"])
	}

	// duration_ms — graceful path test (below) also asserts this; here we
	// pin the lower bound to ≥ 4900 because we delayed Close 4950ms.
	durRaw, ok := unmountEvt.Args["duration_ms"]
	if !ok {
		t.Fatal("Unmount args missing duration_ms (Task 3.1 not landed)")
	}
	dur, ok := durRaw.(float64) // JSON numeric → float64
	if !ok {
		t.Fatalf("Unmount args.duration_ms type = %T, want float64", durRaw)
	}
	if int64(dur) < 4900 {
		t.Errorf("Unmount args.duration_ms = %v, want ≥ 4900", dur)
	}

	// forced + reason — the AC7 distinguishing markers.
	forced, _ := unmountEvt.Args["forced"].(bool)
	if !forced {
		t.Errorf("Unmount args.forced = %v, want true (sentinel not propagated)", unmountEvt.Args["forced"])
	}
	if got, _ := unmountEvt.Args["reason"].(string); got != "sigterm_ignored" {
		t.Errorf("Unmount args.reason = %q, want %q", got, "sigterm_ignored")
	}
}

// -----------------------------------------------------------------------------
// AC7 supplemental: graceful path (no forced flag, duration_ms present)
// -----------------------------------------------------------------------------
//
// Story §AC7 spec: "graceful 路径维持现状（path + auto）" — but Task 3.1's
// implementation ALSO adds `duration_ms` to graceful events (story Source Tree
// references the same args map for both paths). This test pins that:
//   - duration_ms is ALWAYS emitted (graceful + forced)
//   - forced / reason are ABSENT on graceful path
//
// Bench against the same finishProcess invocation but with a healthy
// transport that closes in <50ms.
func TestATDD_48_2_007b_UnmountEvent_GracefulPath_NoForcedFlag(t *testing.T) {
	transports := map[string]*trackingMCPTransport{
		"fast": {}, // no closeDelay, no closeErr
	}
	factory := newTrackingTransportFactory(transports)

	llmFile := &mockLLMFile{readData: makeLLMResponse("ac7b", 1)}
	k, _, _ := newTestKernel(t, llmFile)

	baseDir := t.TempDir()
	k.SetStepDataDir(baseDir)

	devReg := k.vfs.DeviceRegistry()
	mgr := vfs.NewMountManager(devReg, factory)
	k.SetMountManager(mgr)

	const path = "/mnt/mcp/200-fast"
	if err := mgr.Mount(path, vfs.MCPConfig{
		ServerName:    "fast",
		Command:       "/bin/echo",
		TransportType: "stdio",
	}); err != nil {
		t.Fatalf("Mount: %v", err)
	}

	proc := NewProcess(0, "AC7b graceful", nil)
	if err := proc.Start(); err != nil {
		t.Fatalf("proc.Start: %v", err)
	}
	proc.MCPMounts = []string{path}

	ew, err := NewEventWriter(baseDir, proc.UUID)
	if err != nil {
		t.Fatalf("NewEventWriter: %v", err)
	}
	t.Cleanup(func() { _ = ew.Close() })
	proc.AttachEventWriter(ew)

	k.finishProcess(proc, ExitStatus{Code: 0, Reason: "complete"})
	if err := ew.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	rows, err := ReadAllEvents(filepath.Join(baseDir, "data", "steps", proc.UUID, "events.jsonl"))
	if err != nil {
		t.Fatalf("ReadAllEvents: %v", err)
	}

	var unmountEvt *SyscallEventDisk
	for i := range rows {
		if rows[i].Syscall == "Unmount" {
			evt := rows[i]
			unmountEvt = &evt
			break
		}
	}
	if unmountEvt == nil {
		t.Fatalf("no Unmount event in graceful path; got %d events", len(rows))
	}

	if _, hasForced := unmountEvt.Args["forced"]; hasForced {
		// `forced=false` would also fail this test — Task 3.1 must OMIT the
		// key entirely on graceful paths to keep on-disk events lean.
		t.Errorf("graceful Unmount unexpectedly has args.forced=%v", unmountEvt.Args["forced"])
	}
	if _, hasReason := unmountEvt.Args["reason"]; hasReason {
		t.Errorf("graceful Unmount unexpectedly has args.reason=%v", unmountEvt.Args["reason"])
	}

	durRaw, ok := unmountEvt.Args["duration_ms"]
	if !ok {
		t.Fatal("graceful Unmount missing duration_ms")
	}
	dur, _ := durRaw.(float64)
	if int64(dur) >= 4900 {
		t.Errorf("graceful Unmount duration_ms = %v, want « 4.9s", dur)
	}
}
