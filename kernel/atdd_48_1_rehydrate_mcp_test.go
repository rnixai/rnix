package kernel

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// =============================================================================
// ATDD 48.1 — Resume 路径 MCP mount 恢复
//
// Story: _bmad-output/implementation-artifacts/48-1-resume-mcp-mount-restore.md
// Status: ready-for-dev
// Phase:  TDD RED — every test starts with t.Skip until the corresponding
//         Task lands. Activate by removing the t.Skip line; the assertions
//         below then drive the implementation through GREEN.
//
// Coverage:
//   AC1 → TestATDD_48_1_001_RehydrateRestoresMCPMount_Reap            (Task 1, 2, 3.1, 4.1)
//   AC2 → TestATDD_48_1_002_RehydrateRestoresMCPMount_LoadSuspended   (Task 1, 2, 3.2, 4.2)
//   AC3 → TestATDD_48_1_003_PartialMountFailure_PreservesSuccessful   (Task 2.2 三组对齐)
//   AC4 → TestATDD_48_1_004_CwdFallback_OriginalMissing               (Task 2.2 fallback 序)
//   AC5 → TestATDD_48_1_005_DaemonRestart_MountSucceedsWithRecoveryFlag (Task 2.2 recovery flag)
//   AC6 → TestATDD_48_1_006_NoMCP_RehydrateUnchanged                  (regression / zero-overhead)
//   AC7 → TestATDD_48_1_007_ForkResume_AlreadyMountedReused           (Task 2.2 ErrAlreadyMounted 复用)
//
// Per Story §Dev Notes §易错点 #8 the new mount loop MUST run AFTER
// k.AddProcess(proc) so events.jsonl + DebugChan see the Mount events.
// Tests below subscribe to proc.DebugChan; if the dev wires the loop into
// the wrong slot the Mount-event assertions will catch it.
// =============================================================================

// makeMountedProcWithMCP simulates "spawn → tool-call → reap" by hand-writing
// the steps.jsonl + proc-info.json (with mcp_mounts) the way reap.go would
// have if Story 48.1 had landed. We keep it inline rather than driving a
// real Spawn so the test depends ONLY on disk shape — that isolates rehydrate
// from any unrelated regression in spawn.go's MCP mount path.
//
// Returns the on-disk PID (the one a real reap would persist), which the
// resume path MUST use when rebuilding mount paths.
func makeMountedProcWithMCP(t *testing.T, baseDir, uuid string,
	mounts []procMountFixture) (originalPID uint64, allowedDevices []string) {
	t.Helper()
	originalPID = 4242
	allowedDevices = []string{"/dev/fs", "/dev/shell"}
	for _, m := range mounts {
		allowedDevices = append(allowedDevices, m.Path)
	}
	writeProcInfoWithMCPMounts(t, baseDir, uuid, originalPID, "dead", "complete",
		allowedDevices, mounts)
	return originalPID, allowedDevices
}

// -----------------------------------------------------------------------------
// AC1: 正常 reap → resume 路径 MCP mount 重建（核心场景，P0）
// -----------------------------------------------------------------------------
//
// Given a process P1 that referenced 1 MCP server and was reaped normally,
// when the user runs `rnix resume <uuid>`, then:
//   1. The original mount path is re-registered in mountMgr (using P1's
//      ORIGINAL PID, not the new PID assigned to P2).
//   2. DeviceRegistry has the path.
//   3. proc.nativeToolDefs includes the MCP server's tool defs.
//   4. Open + Write + Read on the MCP path succeeds (no ErrNotFound).
//   5. events.jsonl logs: ResumeFromHistory → Mount(source="resume") → ...
//
// This is the central correctness test — failing it means resumed processes
// silently lose MCP capability (the bug Investigation Finding 9 documented).
//
func TestATDD_48_1_001_RehydrateRestoresMCPMount_Reap(t *testing.T) {
	t.Skip("RED PHASE: story 48.1 Task 1.1-1.5 + Task 2 + Task 3.1 not implemented yet — " +
		"remove this Skip after dev wires reattachMCPMounts() into resumeFromHistory")

	uuid := "48-1-ac1-reap-resume-0000-000000000001"
	serverName := "deepwiki"
	mock := &mockMCPTransport{}
	transports := map[string]*mockMCPTransport{serverName: mock}
	k, baseDir, mgr := setupResumeKernelWithMockMCP(t, transports)

	originalPID, _ := makeMountedProcWithMCP(t, baseDir, uuid, []procMountFixture{
		{
			Path:   mcpMountPath(4242, serverName),
			Config: makeMCPConfig(serverName, "/bin/echo", baseDir),
		},
	})

	// procHistory.Add lets resumeFromHistory locate the disk snapshot when
	// procTable does not yet contain this UUID (typical reap → resume flow).
	k.procHistory.Add(vfs.ProcInfo{
		PID: types.PID(originalPID), UUID: uuid,
		State: types.StateDead, Intent: "AC1 resume with MCP",
	})

	result, err := k.ResumeWithOpts(uuid, ResumeOpts{Fork: false})
	if err != nil {
		t.Fatalf("Resume failed: %v", err)
	}
	t.Cleanup(func() { cleanupResumedProc(t, k, result.PID) })

	proc, ok := k.GetProcess(result.PID)
	if !ok {
		t.Fatalf("resumed proc not in procTable (PID=%d)", result.PID)
	}

	// --- Assertion 1 + 2: mountMgr + DeviceRegistry contain the ORIGINAL path
	expectedPath := mcpMountPath(originalPID, serverName)
	mounts := mgr.ListMounts()
	found := false
	for _, m := range mounts {
		if m.Path == expectedPath {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("mountMgr.ListMounts missing %q; got %d mounts: %+v",
			expectedPath, len(mounts), mounts)
	}
	if _, lookupOk := k.vfs.DeviceRegistry().GetDriver(expectedPath); !lookupOk {
		t.Errorf("DeviceRegistry.GetDriver(%q) returned ok=false (mount loop did not register device)",
			expectedPath)
	}

	// --- Assertion 3: AllowedDevices retains the MCP path (so buildToolDefs
	//                  + LLM-driven tools/list can later discover MCP tools).
	// Note: MCP per-tool ToolDefs enter proc.nativeToolDefs lazily (the LLM
	// driver pushes them through a "system" event after calling tools/list —
	// see observe.go:545). Static post-resume snapshot will NOT show them,
	// so we instead verify the prerequisite: the MCP path survived rehydrate's
	// AllowedDevices filtering.
	proc.mu.Lock()
	gotAllowed := append([]string(nil), proc.AllowedDevices...)
	proc.mu.Unlock()
	if !slices.Contains(gotAllowed, expectedPath) {
		t.Errorf("proc.AllowedDevices missing %q after rehydrate; got %v", expectedPath, gotAllowed)
	}

	// --- Assertion 4: Open + Write + Read flow returns no ErrNotFound.
	// VFS API: Open(pid, path, flags) / Write(ctx, pid, fd, data) / Read(pid, fd, n) / Close(pid, fd).
	fd, openErr := k.vfs.Open(proc.PID, expectedPath+"/tools/echo", vfs.O_RDWR)
	if openErr != nil {
		t.Fatalf("VFS Open(%q) failed: %v (mount path not routable)",
			expectedPath+"/tools/echo", openErr)
	}
	defer func() { _ = k.vfs.Close(proc.PID, fd) }()
	writeCtx, writeCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer writeCancel()
	if writeErr := k.vfs.Write(writeCtx, proc.PID, fd, []byte(`{"text":"hi"}`)); writeErr != nil {
		t.Errorf("VFS Write failed: %v", writeErr)
	}
	out, readErr := k.vfs.Read(proc.PID, fd, 4096)
	if readErr != nil {
		t.Errorf("VFS Read failed: %v", readErr)
	}
	if len(out) == 0 {
		t.Errorf("VFS Read returned empty payload — mock transport not invoked")
	}

	// --- Assertion 5: events.jsonl / DebugChan ordering
	// Story 48.1 §易错点 #8: emits must arrive AFTER AddProcess. Pull events
	// for up to 1s; we expect at least one Mount event with source="resume".
	mountEvts, all := drainMountEvents(t, proc, 1*time.Second)
	if len(mountEvts) == 0 {
		t.Errorf("no Mount syscall events observed (rehydrate emitted before AddProcess?); allEvents=%d", len(all))
	}
	for _, evt := range mountEvts {
		if src, ok := mountEventArg(evt, "source"); !ok || src != "resume" {
			t.Errorf("Mount event missing source=\"resume\"; got args=%v", evt.Args)
		}
		if p, ok := mountEventArg(evt, "path"); !ok || p != expectedPath {
			t.Errorf("Mount event path=%v, want %q", p, expectedPath)
		}
	}

	// --- Mock invocation accounting: transport.Connect must have been called.
	connectCount, _, _ := mock.snapshot()
	if connectCount == 0 {
		t.Errorf("mock MCPTransport.Connect never invoked — mount loop never reached transport factory")
	}
}

// -----------------------------------------------------------------------------
// AC2: Suspended placeholder 复活路径 MCP mount 重建（daemon restart 场景，P0）
// -----------------------------------------------------------------------------
//
// Given a daemon restart that loads a Suspended placeholder from disk, the
// MCP transport must be re-mounted BEFORE the user runs `rnix resume` so the
// placeholder in procTable already advertises MCP toolMap entries.
//
// We assert by inspecting mountMgr immediately after LoadSuspendedFromDisk
// returns — no user-issued resume needed.
//
func TestATDD_48_1_002_RehydrateRestoresMCPMount_LoadSuspended(t *testing.T) {
	t.Skip("RED PHASE: story 48.1 Task 1 + Task 2 + Task 3.2 not implemented yet — " +
		"remove this Skip after dev wires reattachMCPMounts() into LoadSuspendedFromDisk")

	uuid := "48-1-ac2-suspended-revive-0000-00000000002"
	serverName := "playwright"
	mock := &mockMCPTransport{}
	transports := map[string]*mockMCPTransport{serverName: mock}
	k, baseDir, mgr := setupResumeKernelWithMockMCP(t, transports)

	originalPID := uint64(7777)
	mounts := []procMountFixture{
		{
			Path:   mcpMountPath(originalPID, serverName),
			Config: makeMCPConfig(serverName, "/bin/echo", baseDir),
		},
	}
	writeProcInfoWithMCPMounts(t, baseDir, uuid, originalPID, "suspended", "budget_exhausted",
		[]string{"/dev/fs", mcpMountPath(originalPID, serverName)}, mounts)

	// LoadSuspendedFromDisk is the daemon-restart entry. It reads every
	// proc-info.json with state=="suspended", recreates the placeholder in
	// procTable, and (post-48.1) re-mounts each MCP transport.
	if _, err := k.LoadSuspendedFromDisk(); err != nil {
		t.Fatalf("LoadSuspendedFromDisk failed: %v", err)
	}

	// --- Assertion: mountMgr already has the path BEFORE any user resume
	expectedPath := mcpMountPath(originalPID, serverName)
	mountList := mgr.ListMounts()
	found := false
	for _, m := range mountList {
		if m.Path == expectedPath {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("after LoadSuspendedFromDisk, mountMgr missing %q (still %d mounts)",
			expectedPath, len(mountList))
	}

	// --- Assertion: the placeholder in procTable carries the path in its
	//                in-memory MCPMounts slice (mirrors disk snapshot).
	var revived *Process
	k.procTable.Range(func(_ types.PID, p *Process) bool {
		if p.UUID == uuid {
			revived = p
			return false
		}
		return true
	})
	if revived == nil {
		t.Fatalf("LoadSuspendedFromDisk did not revive placeholder for UUID=%q", uuid)
	}
	revived.mu.Lock()
	gotPaths := append([]string(nil), revived.MCPMounts...)
	revived.mu.Unlock()
	if len(gotPaths) != 1 || gotPaths[0] != expectedPath {
		t.Errorf("revived.MCPMounts = %v, want [%q]", gotPaths, expectedPath)
	}

	// --- Assertion: stepDataDir-empty edge case is non-fatal (AC2 §Then 4)
	// Verify Connect was called at least once for the AC2 server.
	if mock.connectCount == 0 {
		t.Errorf("mock transport Connect never invoked during LoadSuspendedFromDisk")
	}
}

// -----------------------------------------------------------------------------
// AC3: 部分失败保留成功项（多 MCP server 单点故障，P0）
// -----------------------------------------------------------------------------
//
// Given 2 MCP servers persisted on disk and 1 (server-B) will fail Connect
// during rehydrate, the resume must:
//   - keep server-A's mount + tool defs
//   - drop server-B from proc.MCPMounts, proc.AllowedDevices, proc.mcpConfigs
//     (all three must stay in sync — Dev Notes §易错点 #3)
//   - emit Mount events for both (one ok, one err)
//   - NOT abort the resume
//
func TestATDD_48_1_003_PartialMountFailure_PreservesSuccessful(t *testing.T) {
	t.Skip("RED PHASE: story 48.1 Task 2.2 partial-failure handling not implemented yet — " +
		"remove this Skip after dev implements three-way slice alignment")

	uuid := "48-1-ac3-partial-fail-0000-000000000003"
	serverA := "deepwiki"
	serverB := "broken-server"
	mockA := &mockMCPTransport{}
	mockB := &mockMCPTransport{connectErr: errors.New("connect refused: no such binary")}
	transports := map[string]*mockMCPTransport{
		serverA: mockA,
		serverB: mockB,
	}
	k, baseDir, mgr := setupResumeKernelWithMockMCP(t, transports)

	originalPID := uint64(8888)
	pathA := mcpMountPath(originalPID, serverA)
	pathB := mcpMountPath(originalPID, serverB)
	mounts := []procMountFixture{
		{Path: pathA, Config: makeMCPConfig(serverA, "/bin/echo", baseDir)},
		{Path: pathB, Config: makeMCPConfig(serverB, "/nonexistent/binary", baseDir)},
	}
	writeProcInfoWithMCPMounts(t, baseDir, uuid, originalPID, "dead", "complete",
		[]string{"/dev/fs", pathA, pathB}, mounts)
	k.procHistory.Add(vfs.ProcInfo{
		PID: types.PID(originalPID), UUID: uuid,
		State: types.StateDead, Intent: "AC3 partial failure",
	})

	result, err := k.ResumeWithOpts(uuid, ResumeOpts{Fork: false})
	if err != nil {
		t.Fatalf("Resume aborted on partial failure (must continue): %v", err)
	}
	t.Cleanup(func() { cleanupResumedProc(t, k, result.PID) })

	proc, ok := k.GetProcess(result.PID)
	if !ok {
		t.Fatalf("resumed proc missing (PID=%d)", result.PID)
	}

	// --- Assertion: only server-A in mountMgr
	mountList := mgr.ListMounts()
	if len(mountList) != 1 || mountList[0].Path != pathA {
		t.Errorf("mountMgr.ListMounts = %+v, want exactly [%q]", mountList, pathA)
	}

	// --- Assertion: proc.MCPMounts, AllowedDevices, mcpConfigs all dropped server-B
	proc.mu.Lock()
	gotMounts := append([]string(nil), proc.MCPMounts...)
	gotAllowed := append([]string(nil), proc.AllowedDevices...)
	gotConfigCount := len(proc.mcpConfigs)
	proc.mu.Unlock()

	if len(gotMounts) != 1 || gotMounts[0] != pathA {
		t.Errorf("proc.MCPMounts = %v, want [%q]", gotMounts, pathA)
	}
	for _, d := range gotAllowed {
		if d == pathB {
			t.Errorf("proc.AllowedDevices still contains failed path %q (three-way alignment broken)", pathB)
		}
	}
	if gotConfigCount != 1 {
		t.Errorf("proc.mcpConfigs len = %d, want 1 (server-B not pruned — prompt will lie about tools)",
			gotConfigCount)
	}

	// --- Assertion: events show both attempts (one ok, one error)
	mountEvts, _ := drainMountEvents(t, proc, 1*time.Second)
	if len(mountEvts) != 2 {
		t.Errorf("expected 2 Mount events (A=ok, B=err); got %d", len(mountEvts))
	}
	var sawErr, sawOK bool
	for _, evt := range mountEvts {
		if evt.Err != nil {
			sawErr = true
		} else {
			sawOK = true
		}
	}
	if !sawErr || !sawOK {
		t.Errorf("Mount events lack the err/ok mix (sawErr=%v sawOK=%v) — failure was not recorded",
			sawErr, sawOK)
	}
}

// -----------------------------------------------------------------------------
// AC4: cwd fallback —— ProjectDir 已不存在时优雅降级 (P1)
// -----------------------------------------------------------------------------
//
// fallback order: original WorkDir → os.Getwd() → /tmp
//
func TestATDD_48_1_004_CwdFallback_OriginalMissing(t *testing.T) {
	t.Skip("RED PHASE: story 48.1 Task 2.2 cwd fallback not implemented yet — " +
		"remove this Skip after dev implements WorkDir os.Stat fallback chain")

	uuid := "48-1-ac4-cwd-fallback-0000-000000000004"
	serverName := "deepwiki"

	// Create + remove a temp dir to simulate an environment drift: the
	// original project dir is gone but the proc-info.json still references it.
	gonePath := filepath.Join(t.TempDir(), "deleted-project")
	if err := os.MkdirAll(gonePath, 0o755); err != nil {
		t.Fatalf("mkdir gonePath: %v", err)
	}
	if err := os.RemoveAll(gonePath); err != nil {
		t.Fatalf("remove gonePath: %v", err)
	}

	var capturedCfg vfs.MCPConfig
	mockT := &mockMCPTransport{}
	// Capture the WorkDir the factory actually receives — this is the only
	// way to verify the fallback ran (the disk snapshot still says gonePath).
	transports := map[string]*mockMCPTransport{}
	k, baseDir := setupResumeKernel(t)
	devReg := k.vfs.DeviceRegistry()
	capturingFactory := func(cfg vfs.MCPConfig) (vfs.MCPTransport, error) {
		if cfg.ServerName == serverName {
			capturedCfg = cfg
			return mockT, nil
		}
		fallback := &mockMCPTransport{serverName: cfg.ServerName}
		transports[cfg.ServerName] = fallback
		return fallback, nil
	}
	k.SetMountManager(vfs.NewMountManager(devReg, capturingFactory))

	originalPID := uint64(5555)
	pathA := mcpMountPath(originalPID, serverName)
	mounts := []procMountFixture{
		{Path: pathA, Config: makeMCPConfig(serverName, "/bin/echo", gonePath)},
	}
	writeProcInfoWithMCPMounts(t, baseDir, uuid, originalPID, "dead", "complete",
		[]string{"/dev/fs", pathA}, mounts)
	k.procHistory.Add(vfs.ProcInfo{
		PID: types.PID(originalPID), UUID: uuid,
		State: types.StateDead, Intent: "AC4 cwd fallback",
	})

	result, err := k.ResumeWithOpts(uuid, ResumeOpts{Fork: false})
	if err != nil {
		t.Fatalf("Resume failed: %v", err)
	}
	t.Cleanup(func() { cleanupResumedProc(t, k, result.PID) })

	// --- Assertion: factory received a non-gone WorkDir
	if capturedCfg.ServerName != serverName {
		t.Fatalf("transport factory never invoked for %q (mount loop skipped or factory mis-routed)",
			serverName)
	}
	if capturedCfg.WorkDir == gonePath {
		t.Errorf("WorkDir = %q (still the deleted path) — fallback did not run", capturedCfg.WorkDir)
	}
	// Reachable WorkDir: cwd or /tmp.
	if _, statErr := os.Stat(capturedCfg.WorkDir); statErr != nil {
		t.Errorf("WorkDir %q after fallback is not reachable: %v", capturedCfg.WorkDir, statErr)
	}

	// --- Assertion: Mount event flags the fallback in args
	proc, ok := k.GetProcess(result.PID)
	if !ok {
		t.Fatalf("resumed proc missing")
	}
	mountEvts, _ := drainMountEvents(t, proc, 1*time.Second)
	if len(mountEvts) == 0 {
		t.Fatalf("no Mount events observed")
	}
	matched := mountEvts[0]
	if v, ok := mountEventArg(matched, "cwd_fallback"); !ok || v != true {
		t.Errorf("Mount event missing cwd_fallback=true; args=%v", matched.Args)
	}
	if v, ok := mountEventArg(matched, "cwd_original"); !ok || v != gonePath {
		t.Errorf("Mount event cwd_original = %v, want %q", v, gonePath)
	}
	if v, ok := mountEventArg(matched, "cwd_resolved"); !ok || v == "" || v == gonePath {
		t.Errorf("Mount event cwd_resolved = %v (must be set and != original)", v)
	}
}

// -----------------------------------------------------------------------------
// AC5: 旧 MCP 子进程残留检测 (SIGKILL 后 resume) — P2
// -----------------------------------------------------------------------------
//
// The "best-effort" part — real SIGKILL of the daemon — is OUT OF SCOPE for
// this Story (deferred to 48.2). We only assert that when mountMgr is empty
// AND info.State==suspended, the Mount event carries the
// daemon_restart_recovery=true flag.
//
func TestATDD_48_1_005_DaemonRestart_MountSucceedsWithRecoveryFlag(t *testing.T) {
	t.Skip("RED PHASE: story 48.1 Task 2.2 daemon_restart_recovery flag not implemented yet — " +
		"remove this Skip after dev adds the recovery-flag heuristic")

	uuid := "48-1-ac5-daemon-restart-0000-000000000005"
	serverName := "deepwiki"
	mock := &mockMCPTransport{}
	transports := map[string]*mockMCPTransport{serverName: mock}
	k, baseDir, _ := setupResumeKernelWithMockMCP(t, transports)

	originalPID := uint64(6666)
	pathA := mcpMountPath(originalPID, serverName)
	mounts := []procMountFixture{
		{Path: pathA, Config: makeMCPConfig(serverName, "/bin/echo", baseDir)},
	}
	writeProcInfoWithMCPMounts(t, baseDir, uuid, originalPID, "suspended", "daemon_restart",
		[]string{"/dev/fs", pathA}, mounts)

	if _, err := k.LoadSuspendedFromDisk(); err != nil {
		t.Fatalf("LoadSuspendedFromDisk failed: %v", err)
	}

	// Locate the revived placeholder.
	var revived *Process
	k.procTable.Range(func(_ types.PID, p *Process) bool {
		if p.UUID == uuid {
			revived = p
			return false
		}
		return true
	})
	if revived == nil {
		t.Fatalf("placeholder not revived")
	}

	mountEvts, _ := drainMountEvents(t, revived, 1*time.Second)
	if len(mountEvts) == 0 {
		t.Fatalf("no Mount events observed during LoadSuspendedFromDisk")
	}
	matched := mountEvts[0]
	if v, ok := mountEventArg(matched, "daemon_restart_recovery"); !ok || v != true {
		t.Errorf("Mount event missing daemon_restart_recovery=true; args=%v", matched.Args)
	}
}

// -----------------------------------------------------------------------------
// AC6: 没引用 MCP 的进程 resume —— 零开销，行为完全不变 (P0 regression)
// -----------------------------------------------------------------------------
//
// This MUST stay green even when Task 2.2 lands. If it flips red, the new
// mount loop has overhead on the no-MCP path and Epic 42/44 tests will all
// regress next.
//
func TestATDD_48_1_006_NoMCP_RehydrateUnchanged(t *testing.T) {
	t.Skip("RED PHASE: story 48.1 Task 2.2 zero-overhead skip not implemented yet — " +
		"remove this Skip alongside AC1 (one PR should activate AC1 and AC6 together)")

	uuid := "48-1-ac6-no-mcp-0000-0000-0000-000000000006"
	k, baseDir, mgr := setupResumeKernelWithMockMCP(t, nil)

	// Use the legacy helper — it does NOT write the mcp_mounts field.
	writeTestStepsAndMeta(t, baseDir, uuid, 5, "complete")
	k.procHistory.Add(vfs.ProcInfo{
		PID: types.PID(999), UUID: uuid,
		State: types.StateDead, Intent: "AC6 no-MCP regression",
	})

	result, err := k.ResumeWithOpts(uuid, ResumeOpts{Fork: false})
	if err != nil {
		t.Fatalf("Resume of no-MCP process failed: %v", err)
	}
	t.Cleanup(func() { cleanupResumedProc(t, k, result.PID) })

	if len(mgr.ListMounts()) != 0 {
		t.Errorf("mountMgr non-empty after no-MCP resume: %+v", mgr.ListMounts())
	}

	proc, ok := k.GetProcess(result.PID)
	if !ok {
		t.Fatalf("resumed proc missing")
	}
	mountEvts, all := drainMountEvents(t, proc, 200*time.Millisecond)
	if len(mountEvts) != 0 {
		t.Errorf("expected zero Mount events on no-MCP resume; got %d (all events: %d)",
			len(mountEvts), len(all))
	}

	// Belt-and-suspenders: verify nil mountMgr also non-fatal.
	k2, baseDir2 := setupResumeKernel(t)
	// Intentionally NOT calling SetMountManager.
	writeTestStepsAndMeta(t, baseDir2, uuid, 5, "complete")
	k2.procHistory.Add(vfs.ProcInfo{
		PID: types.PID(998), UUID: uuid,
		State: types.StateDead, Intent: "AC6 nil mountMgr",
	})
	if _, err := k2.ResumeWithOpts(uuid, ResumeOpts{Fork: false}); err != nil {
		t.Errorf("Resume with nil mountMgr returned error (must be optional): %v", err)
	}
}

// -----------------------------------------------------------------------------
// AC7: fork resume —— ErrAlreadyMounted 降级为复用 (P1)
// -----------------------------------------------------------------------------
//
// When fork resume's mount path collides with an existing mount (because a
// sibling resume already claimed it), the rehydrate loop must NOT error out —
// it must record `reused=true` in the Mount event and add the path to
// proc.MCPMounts so buildToolDefs picks up the existing DeviceRegistry entry.
//
func TestATDD_48_1_007_ForkResume_AlreadyMountedReused(t *testing.T) {
	t.Skip("RED PHASE: story 48.1 Task 2.2 ErrAlreadyMounted reuse path not implemented yet — " +
		"remove this Skip after dev adds the ErrAlreadyMounted branch")

	uuid := "48-1-ac7-fork-resume-0000-000000000007"
	serverName := "deepwiki"
	mock := &mockMCPTransport{}
	transports := map[string]*mockMCPTransport{serverName: mock}
	k, baseDir, mgr := setupResumeKernelWithMockMCP(t, transports)

	originalPID := uint64(3333)
	pathA := mcpMountPath(originalPID, serverName)

	// Pre-occupy the mount path (as if a sibling fork already resumed).
	if err := mgr.Mount(pathA, makeMCPConfig(serverName, "/bin/echo", baseDir)); err != nil {
		t.Fatalf("pre-occupy mount failed: %v", err)
	}
	mounts := []procMountFixture{
		{Path: pathA, Config: makeMCPConfig(serverName, "/bin/echo", baseDir)},
	}
	writeProcInfoWithMCPMounts(t, baseDir, uuid, originalPID, "dead", "complete",
		[]string{"/dev/fs", pathA}, mounts)
	k.procHistory.Add(vfs.ProcInfo{
		PID: types.PID(originalPID), UUID: uuid,
		State: types.StateDead, Intent: "AC7 fork resume",
	})

	result, err := k.ResumeWithOpts(uuid, ResumeOpts{Fork: true})
	if err != nil {
		t.Fatalf("Fork resume failed on ErrAlreadyMounted (must reuse): %v", err)
	}
	if result.UUID == uuid {
		// fork must produce a NEW UUID per ADR 40.
		t.Errorf("fork resume returned same UUID=%q, expected new UUID", result.UUID)
	}
	t.Cleanup(func() { cleanupResumedProc(t, k, result.PID) })

	proc, ok := k.GetProcess(result.PID)
	if !ok {
		t.Fatalf("forked proc missing (PID=%d)", result.PID)
	}

	proc.mu.Lock()
	gotMounts := append([]string(nil), proc.MCPMounts...)
	proc.mu.Unlock()
	if len(gotMounts) != 1 || gotMounts[0] != pathA {
		t.Errorf("forked proc.MCPMounts = %v, want [%q] (reuse failed to record path)",
			gotMounts, pathA)
	}

	mountEvts, _ := drainMountEvents(t, proc, 1*time.Second)
	foundReuse := false
	for _, evt := range mountEvts {
		if v, ok := mountEventArg(evt, "reused"); ok && v == true {
			foundReuse = true
			break
		}
	}
	if !foundReuse {
		t.Errorf("no Mount event with reused=true; saw %d events", len(mountEvts))
	}

	// mountMgr still has exactly one entry (no duplicate).
	if list := mgr.ListMounts(); len(list) != 1 {
		t.Errorf("mountMgr.ListMounts len = %d, want 1 (duplicate mount created)", len(list))
	}
}

