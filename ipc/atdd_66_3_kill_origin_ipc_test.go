package ipc

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/kernel"
)

// =============================================================================
// ATDD — Story 66.3: Kill 事件来源审计（IPC 面）
//
// kernel/atdd_66_3_* 覆盖 KillWithOrigin 的内核语义（cascade / escalation /
// killed_suspended / unknown 兜底）。本文件焊死另一条链: CLI 侧 origin →
// 真 Unix socket → handleKill → kernel → events.jsonl，以及 daemon 入口日志。
// 复用 66.2 E2E 的 setupInterruptE2E / dialClient / spawnAndAwaitStream。
// =============================================================================

// killEventArgsFromDisk reads events.jsonl for the process and returns the args
// of every Kill event. origin 落在嵌套 args 内，不在事件顶层（66.2 教训）。
func killEventArgsFromDisk(t *testing.T, projBase, uuid string) []map[string]any {
	t.Helper()
	path := filepath.Join(projBase, "steps", uuid, "events.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var out []map[string]any
	for line := range bytes.SplitSeq(data, []byte("\n")) {
		if len(line) == 0 {
			continue
		}
		var evt map[string]any
		if json.Unmarshal(line, &evt) != nil {
			continue
		}
		if evt["syscall"] != "Kill" {
			continue
		}
		if args, ok := evt["args"].(map[string]any); ok {
			out = append(out, args)
		}
	}
	return out
}

// captureDaemonLog redirects the standard logger into a buffer for the duration
// of the test. Not parallel-safe — callers must not t.Parallel().
// syncBuffer is a mutex-guarded bytes.Buffer. The daemon's background
// goroutines write through the global logger while the test reads String();
// without the lock that races the buffer's backing slice under -race
// (Story 66.3 review F8).
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func captureDaemonLog(t *testing.T) *syncBuffer {
	t.Helper()
	buf := &syncBuffer{}
	log.SetOutput(buf)
	t.Cleanup(func() { log.SetOutput(os.Stderr) })
	return buf
}

// -----------------------------------------------------------------------------
// E2E-1 (AC1) — CLI origin 经 wire 落到 events.jsonl 的 Kill 记录
// -----------------------------------------------------------------------------

func TestATDD_66_3_IPC_KillOrigin_AcrossWire(t *testing.T) {
	cases := []struct {
		name   string
		origin types.KillOrigin
	}{
		{"cli", types.KillOriginCLI},
		{"dashboard", types.KillOriginDashboard},
		{"os_signal", types.KillOriginOSSignal},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			driver := newKillableDriver(false)
			sockPath, kern, _, projBase := setupInterruptE2E(t, driver)
			pid := spawnAndAwaitStream(t, kern, driver, "66.3 "+tc.name)

			proc, ok := kern.GetProcess(pid)
			if !ok {
				t.Fatalf("process %d not found", pid)
			}
			uuid := proc.UUID

			if err := dialClient(t, sockPath).Kill(pid, types.SIGTERM, tc.origin); err != nil {
				t.Fatalf("client.Kill: %v", err)
			}
			if _, err := dialClient(t, sockPath).Wait(pid, 5000); err != nil {
				t.Fatalf("client.Wait: %v", err)
			}

			killArgs := killEventArgsFromDisk(t, projBase, uuid)
			if len(killArgs) == 0 {
				t.Fatal("no Kill events in events.jsonl")
			}
			wantRequesterPrefix := filepath.Base(os.Args[0]) + "["
			for i, args := range killArgs {
				if args["origin"] != string(tc.origin) {
					t.Errorf("Kill event #%d: origin = %v, want %q", i, args["origin"], tc.origin)
				}
				requester, _ := args["requester"].(string)
				if !strings.HasPrefix(requester, wantRequesterPrefix) {
					t.Errorf("Kill event #%d: requester = %q, want prefix %q (client auto-fills argv0[pid])",
						i, requester, wantRequesterPrefix)
				}
			}
		})
	}
}

// -----------------------------------------------------------------------------
// E2E-2 (AC2) — daemon 入口日志一行带 pid/uuid/signal/origin/requester
// -----------------------------------------------------------------------------

func TestATDD_66_3_IPC_DaemonKillRequestLog(t *testing.T) {
	buf := captureDaemonLog(t)

	driver := newKillableDriver(false)
	sockPath, kern, _, _ := setupInterruptE2E(t, driver)
	pid := spawnAndAwaitStream(t, kern, driver, "66.3 daemon log")

	// The CLI kills by PID, so the request carries no UUID (uuid= is empty in
	// the log line — the audit trail records the request verbatim, before
	// resolvePID, per the story's log-placement decision).
	if err := dialClient(t, sockPath).Kill(pid, types.SIGTERM, types.KillOriginDashboard); err != nil {
		t.Fatalf("client.Kill: %v", err)
	}
	if _, err := dialClient(t, sockPath).Wait(pid, 5000); err != nil {
		t.Fatalf("client.Wait: %v", err)
	}

	logs := buf.String()
	line := findLogLine(logs, "[ipc] kill request:")
	if line == "" {
		t.Fatalf("daemon log missing '[ipc] kill request:' line:\n%s", logs)
	}
	// Wire-controlled fields are logged via %q (Story 66.3 F2a), so the
	// values appear quoted: origin="dashboard", requester="<argv0>[pid]".
	for _, want := range []string{
		fmt.Sprintf("pid=%d", pid),
		"uuid=",
		"signal=SIGTERM",
		`origin="dashboard"`,
		`requester="` + filepath.Base(os.Args[0]),
	} {
		if !strings.Contains(line, want) {
			t.Errorf("kill request log line missing %q\nline: %s", want, line)
		}
	}
}

// findLogLine returns the first line of logs containing marker, or "".
func findLogLine(logs, marker string) string {
	for line := range strings.SplitSeq(logs, "\n") {
		if strings.Contains(line, marker) {
			return line
		}
	}
	return ""
}

// -----------------------------------------------------------------------------
// E2E-3 (AC2) — 目标不存在的 kill 请求同样留痕
// -----------------------------------------------------------------------------

func TestATDD_66_3_IPC_KillMissingPID_StillLogged(t *testing.T) {
	buf := captureDaemonLog(t)

	driver := newKillableDriver(false)
	sockPath, _, _, _ := setupInterruptE2E(t, driver)

	err := dialClient(t, sockPath).Kill(types.PID(999999), types.SIGKILL, types.KillOriginCLI)
	if err == nil {
		t.Fatal("Kill on a missing PID should fail")
	}

	line := findLogLine(buf.String(), "[ipc] kill request:")
	if line == "" {
		t.Fatalf("a kill request for a nonexistent PID must still leave a daemon log trace:\n%s", buf.String())
	}
	for _, want := range []string{"pid=999999", "signal=SIGKILL", `origin="cli"`} {
		if !strings.Contains(line, want) {
			t.Errorf("kill request log line missing %q\nline: %s", want, line)
		}
	}
}

// -----------------------------------------------------------------------------
// E2E-4 (AC5) — 老 client 不传 origin → server 落 unknown（wire additive）
// -----------------------------------------------------------------------------

func TestATDD_66_3_IPC_LegacyClient_NoOrigin_Unknown(t *testing.T) {
	driver := newKillableDriver(false)
	sockPath, kern, _, projBase := setupInterruptE2E(t, driver)
	pid := spawnAndAwaitStream(t, kern, driver, "66.3 legacy wire")

	proc, _ := kern.GetProcess(pid)
	uuid := proc.UUID

	// A pre-66.3 client sends {"pid":N,"signal":1} with no origin/requester.
	conn := dial(t, sockPath)
	payload, _ := json.Marshal(map[string]any{"pid": pid, "signal": types.SIGTERM})
	req, _ := json.Marshal(Request{Method: MethodKill, Payload: payload})
	if _, err := conn.Write(append(req, '\n')); err != nil {
		t.Fatalf("write legacy kill request: %v", err)
	}
	reader := bufio.NewReader(conn)
	respLine, err := reader.ReadBytes('\n')
	if err != nil {
		t.Fatalf("read kill response: %v", err)
	}
	var resp Response
	if err := json.Unmarshal(respLine, &resp); err != nil {
		t.Fatalf("unmarshal kill response: %v", err)
	}
	if !resp.OK {
		t.Fatalf("legacy kill request rejected: %+v", resp.Error)
	}

	if _, err := dialClient(t, sockPath).Wait(pid, 5000); err != nil {
		t.Fatalf("client.Wait: %v", err)
	}

	killArgs := killEventArgsFromDisk(t, projBase, uuid)
	if len(killArgs) == 0 {
		t.Fatal("no Kill events in events.jsonl")
	}
	for i, args := range killArgs {
		if args["origin"] != string(types.KillOriginUnknown) {
			t.Errorf("Kill event #%d: origin = %v, want %q for a legacy client",
				i, args["origin"], types.KillOriginUnknown)
		}
	}
}

// -----------------------------------------------------------------------------
// E2E-5 (AC1/AC3) — SignalTree wire origin
// -----------------------------------------------------------------------------

func TestATDD_66_3_IPC_SignalTreeOrigin_AcrossWire(t *testing.T) {
	driver := newKillableDriver(false)
	sockPath, kern, _, projBase := setupInterruptE2E(t, driver)
	pid := spawnAndAwaitStream(t, kern, driver, "66.3 signal tree")

	proc, _ := kern.GetProcess(pid)
	uuid := proc.UUID

	resp, err := dialClient(t, sockPath).SignalTree(pid, types.SIGTERM, types.KillOriginDashboard)
	if err != nil {
		t.Fatalf("client.SignalTree: %v", err)
	}
	if resp.Affected != 1 {
		t.Errorf("affected = %d, want 1", resp.Affected)
	}
	if _, err := dialClient(t, sockPath).Wait(pid, 5000); err != nil {
		t.Fatalf("client.Wait: %v", err)
	}

	killArgs := killEventArgsFromDisk(t, projBase, uuid)
	if len(killArgs) == 0 {
		t.Fatal("no Kill events in events.jsonl")
	}
	for i, args := range killArgs {
		if args["origin"] != string(types.KillOriginDashboard) {
			t.Errorf("Kill event #%d: origin = %v, want %q", i, args["origin"], types.KillOriginDashboard)
		}
	}
}

// -----------------------------------------------------------------------------
// UNIT (AC1) — immune resume 走 KillOriginResume（非终止信号也标来源）
// -----------------------------------------------------------------------------

// Story 66.3 review (decision A): immune resume must ACTUALLY resume a
// Suspended process. The dev-story path (KillWithOrigin(SIGRESUME)) noop'd a
// Suspended target — SIGRESUME is non-terminating, so the Kill path fell to
// noop_suspended and never called ResumeSubtree, leaving the process stuck
// while stamping a misleading origin=resume audit event. handleImmuneResume now
// routes through Signal(), whose Suspended branch delegates to ResumeSubtree.
// Signal events are origin-free, so the contract asserted here is functional
// (the process returns to Running), not attribution.
func TestATDD_66_3_IPC_ImmuneResume_ResumesProcess(t *testing.T) {
	driver := newKillableDriver(false)
	sockPath, kern, _, _ := setupInterruptE2E(t, driver)
	pid := spawnAndAwaitStream(t, kern, driver, "66.3 immune resume")

	proc, _ := kern.GetProcess(pid)

	// Suspend first so SIGRESUME has something to act on.
	if err := kern.KillWithOrigin(pid, types.SIGPAUSE,
		kernel.KillAttribution{Origin: types.KillOriginWatchdog, Requester: "immune"}); err != nil {
		t.Fatalf("SIGPAUSE: %v", err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for proc.GetState() != types.StateSuspended && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if state := proc.GetState(); state != types.StateSuspended {
		t.Fatalf("state = %v, want Suspended", state)
	}

	if _, err := dialClient(t, sockPath).ImmuneResume(uint64(pid)); err != nil {
		t.Fatalf("client.ImmuneResume: %v", err)
	}

	// The process must leave Suspended and be Running again — the whole point of
	// resume. A stuck-Suspended process (the pre-fix bug) fails here.
	deadline = time.Now().Add(3 * time.Second)
	for proc.GetState() == types.StateSuspended && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if state := proc.GetState(); state != types.StateRunning {
		t.Fatalf("after immune resume: state = %v, want Running (process must actually resume)", state)
	}

	// Cleanup: the resumed process parks in Write again.
	_ = kern.Kill(pid, types.SIGKILL)
}
