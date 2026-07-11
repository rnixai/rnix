package ipc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/drivers/llm"
	drivershell "github.com/rnixai/rnix/drivers/shell"
	"github.com/rnixai/rnix/internal/config"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/kernel"
	"github.com/rnixai/rnix/vfs"
)

// =============================================================================
// E2E — Story 68.1: replay/scripted LLM driver, real daemon lifecycle (AC4, AC5)
//
// Setup mirrors atdd_66_2_interrupted_e2e_test.go's setupInterruptE2E: a real
// socket server + real kernel + a real LLMFile (llm.FileFactory), only here the
// underlying driver is the real *llm.ReplayDriver registered at /dev/llm/replay
// instead of a hand-rolled mock. Spawn goes through the actual MethodSpawn wire
// (client.SpawnDetached) rather than kern.Spawn directly, because only the real
// IPC handleSpawn path reaps the top-level process to Dead once its progress
// stream completes (kern.Spawn alone leaves a killed/completed top-level
// process parked at Zombie — see the atdd_66_2 file's own commentary). Reaching
// Dead is what AC4 asks for and is also what makes "wait, then read the files"
// unambiguous: everything the process could still write is flushed by then.
//
// req.ProjectDir is left empty on every SpawnRequest, so resolveProjectContext
// returns nil ProjectConfig (pure global mode) and step data lands under
// config.GlobalDataDir(dataDir)/steps/<uuid>/ — the same "global" bucket
// reloadHistory's AllBaseDirs comment documents in atdd_66_2.
// =============================================================================

// setupReplayE2E starts a real socket server backed by a real kernel with a
// real *llm.ReplayDriver mounted at /dev/llm/replay. Returns the socket path
// and the data root (files land under config.GlobalDataDir(dataDir)/steps/<uuid>/).
func setupReplayE2E(t *testing.T) (sockPath, dataDir string) {
	t.Helper()

	driver := llm.NewReplayDriver("replay")
	devReg := vfs.NewDeviceRegistry()
	if err := devReg.Register("/dev/llm/replay", llm.FileFactory(driver, "/dev/llm/replay", "")); err != nil {
		t.Fatalf("register /dev/llm/replay: %v", err)
	}
	// A no-agent top-level spawn has no AllowedDevices whitelist, which makes
	// buildToolDefs (kernel/toolgen.go) expose EVERY registered base device via
	// devReg.RangeDrivers — so the script's Bash tool_call only resolves if
	// /dev/shell is actually registered here (it is not implied by /dev/llm/*).
	shellDriver := drivershell.NewDriver()
	if err := devReg.RegisterWithDriver("/dev/shell", drivershell.FileFactory(shellDriver, "/dev/shell"), shellDriver); err != nil {
		t.Fatalf("register /dev/shell: %v", err)
	}
	vfsInst := vfs.NewVFS(devReg)
	ctxMgr := rnixctx.NewManager()

	srv := NewServer(nil, nil, "0.1.0-test", "", "")
	kern := kernel.NewKernel(vfsInst, ctxMgr, srv.CallbackMux())
	srv.kern = kern

	// t.TempDir() must be registered before kern.Shutdown's cleanup (LIFO —
	// see [[test-cleanup-lifo-tempdir-order]]); TestSetupDataDir does this.
	dataDir, _ = kernel.TestSetupDataDir(t, kern)

	sockPath = filepath.Join(t.TempDir(), "test.sock")
	if err := srv.ListenAndServe(sockPath); err != nil {
		t.Fatalf("ListenAndServe: %v", err)
	}
	t.Cleanup(func() {
		srv.Shutdown()
		srv.Wait()
		kern.Shutdown()
	})

	return sockPath, dataDir
}

// absReplayScript resolves a testdata-relative replay script to an absolute
// path — the driver's model→script-path channel (裁决 2) treats a relative
// req.Model as relative to req.ProjectDir, which we deliberately leave empty
// (global mode), so the test must hand the driver an absolute path itself.
func absReplayScript(t *testing.T, relPath string) string {
	t.Helper()
	abs, err := filepath.Abs(relPath)
	if err != nil {
		t.Fatalf("filepath.Abs(%s): %v", relPath, err)
	}
	if _, err := os.Stat(abs); err != nil {
		t.Fatalf("replay fixture %s: %v", abs, err)
	}
	return abs
}

// waitForDeadState polls MethodListAllProcs until pid reaches types.StateDead
// (the real handleSpawn's deferred Reap runs asynchronously right after the
// spawning connection observes the stream's terminal "complete" event — a
// microseconds-scale race, not an instant guarantee — so this test recovers
// with a short poll rather than a single racy read).
func waitForDeadState(t *testing.T, sockPath string, pid types.PID, timeout time.Duration) vfs.ProcInfo {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last vfs.ProcInfo
	for {
		last = procFromWire(t, sockPath, pid)
		if last.State == types.StateDead {
			return last
		}
		if time.Now().After(deadline) {
			t.Fatalf("pid=%d did not reach Dead within %s (last state=%s, exit_reason=%q)",
				pid, timeout, last.State, last.ExitReason)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func readFileMustExistNonEmpty(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected %s to exist and be readable: %v", path, err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		t.Fatalf("expected %s to be non-empty", path)
	}
	return string(data)
}

// volatileEventFields are stripped before comparing two runs' events.jsonl —
// kept narrow ([[dev note 关键防错 #9]]: over-broad exclusion turns the
// determinism assertion into a tautology). Everything else (syscall type,
// args, result, error) must compare byte-for-byte equal across runs.
var volatileEventFields = []string{"ts_ms", "pid", "dur_ms", "trace_id", "span_id"}

// normalizeVolatileEventFields strips identifiers that are process-instance
// counters rather than script behavior:
//   - the top-level fields above (timestamps, this test's own PID, durations);
//   - a "Spawn" event's "result", which carries the newly-assigned child PID
//     (kernel's global atomic PID counter — monotonically different on every
//     spawn across the whole test binary, never part of what the script
//     determines);
//   - "args.cid", the context ID assigned to this process (also a global
//     monotonic counter, one per spawn on the shared kernel instance used by
//     both runs).
func normalizeVolatileEventFields(m map[string]any) {
	for _, f := range volatileEventFields {
		delete(m, f)
	}
	if m["syscall"] == "Spawn" {
		delete(m, "result")
	}
	if args, ok := m["args"].(map[string]any); ok {
		delete(args, "cid")
	}
}

// readNormalizedEvents parses events.jsonl into one map per line with the
// volatile fields above removed, preserving line order.
func readNormalizedEvents(t *testing.T, path string) []map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var out []map[string]any
	for line := range strings.SplitSeq(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("unmarshal event line in %s: %v\nline=%s", path, err, line)
		}
		normalizeVolatileEventFields(m)
		out = append(out, m)
	}
	return out
}

// -----------------------------------------------------------------------------
// AC4 — full real lifecycle: spawn → tool_call → complete → Dead, all four
// observability files land on disk with the expected content.
// -----------------------------------------------------------------------------

func TestE2E_68_1_Replay_FullLifecycle(t *testing.T) {
	sockPath, dataDir := setupReplayE2E(t)
	scriptPath := absReplayScript(t, "testdata/replay/two-step.responses.yaml")

	pid, err := dialClient(t, sockPath).SpawnDetached(SpawnRequest{
		Intent:   "e2e 68.1 — replay full lifecycle",
		Provider: "replay",
		Model:    scriptPath,
	})
	if err != nil {
		t.Fatalf("SpawnDetached: %v", err)
	}

	final := waitForDeadState(t, sockPath, pid, 5*time.Second)
	if final.ExitReason != "completed" {
		t.Errorf("ExitReason = %q, want %q", final.ExitReason, "completed")
	}
	if final.UUID == "" {
		t.Fatalf("empty UUID for pid=%d", pid)
	}

	stepsDir := filepath.Join(config.GlobalDataDir(dataDir), "steps", final.UUID)

	stepsContent := readFileMustExistNonEmpty(t, filepath.Join(stepsDir, "steps.jsonl"))
	if !strings.Contains(stepsContent, `"action":"tool_call"`) || !strings.Contains(stepsContent, `"Bash"`) {
		t.Errorf("steps.jsonl missing a tool_call step for Bash:\n%s", stepsContent)
	}
	if !strings.Contains(stepsContent, `"action":"complete"`) || !strings.Contains(stepsContent, "replay e2e done") {
		t.Errorf("steps.jsonl missing the complete step / scripted result:\n%s", stepsContent)
	}

	eventsContent := readFileMustExistNonEmpty(t, filepath.Join(stepsDir, "events.jsonl"))
	if !strings.Contains(eventsContent, `"syscall":"ReasonStep"`) {
		t.Errorf("events.jsonl missing ReasonStep events:\n%s", eventsContent)
	}

	procInfoContent := readFileMustExistNonEmpty(t, filepath.Join(stepsDir, "proc-info.json"))
	if !strings.Contains(procInfoContent, final.UUID) {
		t.Errorf("proc-info.json does not mention its own uuid %q:\n%s", final.UUID, procInfoContent)
	}

	rawContent := readFileMustExistNonEmpty(t, filepath.Join(stepsDir, "raw.jsonl"))
	if !strings.Contains(rawContent, `"kind":"replay"`) {
		t.Errorf(`raw.jsonl missing "kind":"replay" (Epic 56 raw capture 8/8→9/9):`+"\n%s", rawContent)
	}
}

// -----------------------------------------------------------------------------
// AC5 — determinism red line: same script, two independent runs, identical
// syscall event sequence (type + payload, volatile fields excluded) and
// identical final result.
// -----------------------------------------------------------------------------

func TestE2E_68_1_Replay_DeterministicAcrossTwoRuns(t *testing.T) {
	sockPath, dataDir := setupReplayE2E(t)
	scriptPath := absReplayScript(t, "testdata/replay/two-step.responses.yaml")

	runOnce := func(n int) (uuid, result string, events []map[string]any) {
		pid, err := dialClient(t, sockPath).SpawnDetached(SpawnRequest{
			Intent:   "e2e 68.1 — determinism",
			Provider: "replay",
			Model:    scriptPath,
		})
		if err != nil {
			t.Fatalf("run #%d: SpawnDetached: %v", n, err)
		}
		final := waitForDeadState(t, sockPath, pid, 5*time.Second)
		if final.ExitReason != "completed" {
			t.Fatalf("run #%d: ExitReason = %q, want %q", n, final.ExitReason, "completed")
		}
		stepsDir := filepath.Join(config.GlobalDataDir(dataDir), "steps", final.UUID)
		return final.UUID, final.Result, readNormalizedEvents(t, filepath.Join(stepsDir, "events.jsonl"))
	}

	uuid1, result1, events1 := runOnce(1)
	uuid2, result2, events2 := runOnce(2)

	if uuid1 == "" || uuid2 == "" || uuid1 == uuid2 {
		t.Fatalf("expected two distinct non-empty UUIDs, got %q and %q", uuid1, uuid2)
	}
	if result1 != result2 {
		t.Errorf("final result differs across two runs of the same script:\nrun1=%q\nrun2=%q", result1, result2)
	}
	if len(events1) != len(events2) {
		t.Fatalf("event count differs across two runs: run1=%d run2=%d\nrun1=%+v\nrun2=%+v",
			len(events1), len(events2), events1, events2)
	}
	for i := range events1 {
		if !reflect.DeepEqual(events1[i], events2[i]) {
			t.Errorf("event[%d] differs across two runs (ts_ms/pid/dur_ms/trace_id/span_id excluded):\nrun1=%+v\nrun2=%+v",
				i, events1[i], events2[i])
		}
	}
}
