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

	gocontext "context"

	"github.com/rnixai/rnix/drivers/llm"
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

// =============================================================================
// QA E2E 补充（Story 66.3, bmad-qa-generate-e2e-tests）
//
// 现有 66.3 覆盖里，级联归因（AC3）由 kernel/atdd_66_3 的
// TestATDD_66_3_AC3_SignalTree_ParentCascade 验证，但它**直调
// k.SignalTreeWithOrigin，绕过真实 wire**；本文件的 E2E-5
// (TestATDD_66_3_IPC_SignalTreeOrigin_AcrossWire) 虽经 wire，却只有单进程
// （affected=1），不触发任何后代归因。下面两个用例焊死此前无端到端覆盖的两条
// SignalTree wire 链路：①级联的后代 origin 经真 socket 落 events.jsonl；
// ②SignalTree 的 legacy-client（无 origin）additive 兜底。这正是 dashboard /
// 编排器经 IPC 真实观测的路径。
// =============================================================================

// -----------------------------------------------------------------------------
// E2E-6 (AC1×AC3) — SignalTree 级联经真 wire: 后代 events.jsonl 带
//                   origin=parent-cascade + root_pid/root_origin 锚定根请求。
// -----------------------------------------------------------------------------

func TestATDD_66_3_IPC_SignalTreeCascade_DescendantOrigin_AcrossWire(t *testing.T) {
	driver := newCascadeDriver(2) // 放行前等父+子都停在 ctx.Done() 上
	sockPath, kern, _, projBase := setupInterruptE2E(t, driver)

	parentPID, err := kern.Spawn("66.3 cascade parent", nil,
		kernel.SpawnOpts{ProjectConfig: kernel.TestProjectConfig()})
	if err != nil {
		t.Fatalf("Spawn parent: %v", err)
	}
	// child 显式传 ProjectConfig：spawn.go 用各进程自带的 config 落盘（不从
	// parent 继承覆盖），同一 testProjectDir 保证父子落到同一 projBase。
	childPID, err := kern.Spawn("66.3 cascade child", nil,
		kernel.SpawnOpts{ParentPID: parentPID, ProjectConfig: kernel.TestProjectConfig()})
	if err != nil {
		t.Fatalf("Spawn child: %v", err)
	}

	select {
	case <-driver.gate:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for parent+child to park on ctx.Done()")
	}

	parent, ok := kern.GetProcess(parentPID)
	if !ok {
		t.Fatalf("parent %d not found", parentPID)
	}
	child, ok := kern.GetProcess(childPID)
	if !ok {
		t.Fatalf("child %d not found", childPID)
	}
	parentUUID, childUUID := parent.UUID, child.UUID

	// SignalTree over the real Unix socket, attributed to the dashboard.
	resp, err := dialClient(t, sockPath).SignalTree(parentPID, types.SIGTERM, types.KillOriginDashboard)
	if err != nil {
		t.Fatalf("client.SignalTree: %v", err)
	}
	if resp.Affected != 2 {
		t.Errorf("affected = %d, want 2 (parent + child)", resp.Affected)
	}
	if _, err := dialClient(t, sockPath).Wait(parentPID, 5000); err != nil {
		t.Fatalf("Wait parent: %v", err)
	}
	if _, err := dialClient(t, sockPath).Wait(childPID, 5000); err != nil {
		t.Fatalf("Wait child: %v", err)
	}

	// Tree root: keeps the original origin, gains no cascade args.
	rootArgs := killEventArgsFromDisk(t, projBase, parentUUID)
	if len(rootArgs) == 0 {
		t.Fatal("no Kill events for the tree root")
	}
	for i, args := range rootArgs {
		if args["origin"] != string(types.KillOriginDashboard) {
			t.Errorf("root Kill #%d: origin = %v, want %q", i, args["origin"], types.KillOriginDashboard)
		}
		if _, ok := args["root_pid"]; ok {
			t.Errorf("root Kill #%d: tree root must not carry root_pid", i)
		}
		if _, ok := args["root_origin"]; ok {
			t.Errorf("root Kill #%d: tree root must not carry root_origin", i)
		}
	}

	// Descendant: attributed to the cascade, pinned to the root request. The
	// requester is the client's argv0[pid] (auto-filled on the wire) and is
	// inherited by every descendant of the cascade.
	childArgs := killEventArgsFromDisk(t, projBase, childUUID)
	if len(childArgs) == 0 {
		t.Fatal("no Kill events for the descendant")
	}
	wantRequesterPrefix := filepath.Base(os.Args[0]) + "["
	for i, args := range childArgs {
		if args["origin"] != string(types.KillOriginParentCascade) {
			t.Errorf("child Kill #%d: origin = %v, want %q", i, args["origin"], types.KillOriginParentCascade)
		}
		if args["root_origin"] != string(types.KillOriginDashboard) {
			t.Errorf("child Kill #%d: root_origin = %v, want %q", i, args["root_origin"], types.KillOriginDashboard)
		}
		// JSON numbers decode to float64.
		if got, want := args["root_pid"], float64(parentPID); got != want {
			t.Errorf("child Kill #%d: root_pid = %v, want %v", i, got, want)
		}
		requester, _ := args["requester"].(string)
		if !strings.HasPrefix(requester, wantRequesterPrefix) {
			t.Errorf("child Kill #%d: requester = %q, want inherited wire prefix %q",
				i, requester, wantRequesterPrefix)
		}
	}
}

// -----------------------------------------------------------------------------
// E2E-7 (AC5) — SignalTree 老 client 不传 origin → unknown。对称补 E2E-4(Kill)：
//               handleSignalTree 与 handleKill 共用 killOriginFromWire，此用例
//               焊死 SignalTree wire 的 additive 兜底路径。
// -----------------------------------------------------------------------------

func TestATDD_66_3_IPC_SignalTree_LegacyClient_NoOrigin_Unknown(t *testing.T) {
	driver := newKillableDriver(false)
	sockPath, kern, _, projBase := setupInterruptE2E(t, driver)
	pid := spawnAndAwaitStream(t, kern, driver, "66.3 signal_tree legacy wire")

	proc, _ := kern.GetProcess(pid)
	uuid := proc.UUID

	// A pre-66.3 client sends {"pid":N,"signal":1} with no origin/requester.
	conn := dial(t, sockPath)
	payload, _ := json.Marshal(map[string]any{"pid": pid, "signal": types.SIGTERM})
	req, _ := json.Marshal(Request{Method: MethodSignalTree, Payload: payload})
	if _, err := conn.Write(append(req, '\n')); err != nil {
		t.Fatalf("write legacy signal_tree request: %v", err)
	}
	reader := bufio.NewReader(conn)
	respLine, err := reader.ReadBytes('\n')
	if err != nil {
		t.Fatalf("read signal_tree response: %v", err)
	}
	var resp Response
	if err := json.Unmarshal(respLine, &resp); err != nil {
		t.Fatalf("unmarshal signal_tree response: %v", err)
	}
	if !resp.OK {
		t.Fatalf("legacy signal_tree request rejected: %+v", resp.Error)
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
			t.Errorf("Kill #%d: origin = %v, want %q for a legacy SignalTree client",
				i, args["origin"], types.KillOriginUnknown)
		}
	}
}

// -----------------------------------------------------------------------------
// cascadeStreamDriver — multi-process variant of e2eStreamDriver.
//
// The LLM device is a VFS singleton, so a parent and its child share one driver
// instance; e2eStreamDriver's readyOnce fires only for the first stream to park,
// leaving the second's readiness unobservable. cascadeStreamDriver instead
// counts arrivals and closes gate once `want` streams have pushed their content
// and parked on ctx.Done() — the handshake a SignalTree cascade needs (both
// processes Running with an in-flight Write before the tree signal lands).
// -----------------------------------------------------------------------------

type cascadeStreamDriver struct {
	contentChunks []string

	mu      sync.Mutex
	entered int
	want    int
	gate    chan struct{} // closed once `want` streams have parked
}

func newCascadeDriver(want int) *cascadeStreamDriver {
	return &cascadeStreamDriver{contentChunks: partialChunks, want: want, gate: make(chan struct{})}
}

func (d *cascadeStreamDriver) Call(_ gocontext.Context, _ llm.LLMRequest) (*llm.LLMResponse, error) {
	return nil, fmt.Errorf("cascadeStreamDriver: Call not used (stream mode)")
}

func (d *cascadeStreamDriver) Info() llm.DriverInfo {
	return llm.DriverInfo{Name: "e2e-cascade", Provider: "test", DefaultModel: "mock-model", DriverType: "mock"}
}

func (d *cascadeStreamDriver) Stream(ctx gocontext.Context, _ llm.LLMRequest) (<-chan llm.StreamEvent, error) {
	ch := make(chan llm.StreamEvent, len(d.contentChunks)+2)
	go func() {
		defer close(ch)
		for _, c := range d.contentChunks {
			select {
			case ch <- llm.StreamEvent{Type: "content", Content: c}:
			case <-ctx.Done():
				return
			}
		}
		d.mu.Lock()
		d.entered++
		if d.entered == d.want {
			close(d.gate)
		}
		d.mu.Unlock()
		<-ctx.Done()
	}()
	return ch, nil
}
