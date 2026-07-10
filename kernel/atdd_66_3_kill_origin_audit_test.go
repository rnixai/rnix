package kernel

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	gocontext "context"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// ============================================================================
// ATDD Story 66.3 — Kill 事件来源审计
// ============================================================================

// readEventArgs returns the args map of every events.jsonl record whose
// syscall matches the given name, in file order.
//
// Story 66.2 Debug Log 教训：origin/action 落在嵌套的 `args` 字段内，
// 不在事件顶层——读取必须经 evt["args"].(map[string]any)。
func readEventArgs(t *testing.T, stepDir, syscall string) []map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(stepDir, "events.jsonl"))
	if err != nil {
		t.Fatalf("read events.jsonl: %v", err)
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
		if evt["syscall"] != syscall {
			continue
		}
		args, ok := evt["args"].(map[string]any)
		if !ok {
			args = map[string]any{}
		}
		out = append(out, args)
	}
	return out
}

// killEventsWithSignal filters Kill event args down to those for a signal.
func killEventsWithSignal(t *testing.T, stepDir string, sig types.Signal) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, args := range readEventArgs(t, stepDir, "Kill") {
		if args["signal"] == sig.String() {
			out = append(out, args)
		}
	}
	return out
}

// killParkFile blocks in Write until the process context is cancelled, then
// surfaces ctx.Err(). Safe for several processes to share concurrently.
type killParkFile struct {
	mu      sync.Mutex
	entered int
	gate    chan struct{} // closed once `want` processes have entered Write
	want    int
}

func newKillParkFile(want int) *killParkFile {
	return &killParkFile{gate: make(chan struct{}), want: want}
}

func (f *killParkFile) Write(ctx gocontext.Context, _ []byte) error {
	f.mu.Lock()
	f.entered++
	if f.entered == f.want {
		close(f.gate)
	}
	f.mu.Unlock()
	<-ctx.Done()
	return ctx.Err()
}

func (f *killParkFile) Read(_ int) ([]byte, error) { return makeLLMResponse("parked", 1), nil }
func (f *killParkFile) Close() error               { return nil }
func (f *killParkFile) Stat() (vfs.FileStat, error) {
	return vfs.FileStat{IsDevice: true, Name: "/dev/llm/claude"}, nil
}
func (f *killParkFile) SupportsToolCalling() bool { return true }

// stubbornLLMFile ignores context cancellation entirely — Write returns only
// after release is closed. This is the "SIGTERM lands on a hung LLM write"
// shape from the platform case file (PID 2137), and the only way to force
// twoPhaseShutdown past its grace period into a SIGKILL escalation.
type stubbornLLMFile struct {
	reached chan struct{}
	release chan struct{}
	once    sync.Once
}

func (f *stubbornLLMFile) Write(ctx gocontext.Context, _ []byte) error {
	f.once.Do(func() { close(f.reached) })
	<-f.release
	if err := ctx.Err(); err != nil {
		return err
	}
	return nil
}

func (f *stubbornLLMFile) Read(_ int) ([]byte, error) { return makeLLMResponse("stubborn", 1), nil }
func (f *stubbornLLMFile) Close() error               { return nil }
func (f *stubbornLLMFile) Stat() (vfs.FileStat, error) {
	return vfs.FileStat{IsDevice: true, Name: "/dev/llm/claude"}, nil
}
func (f *stubbornLLMFile) SupportsToolCalling() bool { return true }

// waitForKillEvent polls events.jsonl (EventWriter flushes per line) until a
// Kill event for sig appears, or the deadline expires.
func waitForKillEvent(t *testing.T, stepDir string, sig types.Signal, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if len(killEventsWithSignal(t, stepDir, sig)) > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s Kill event in %s", sig, stepDir)
}

// --- 用例 1（AC5）：未指定 origin 的遗留路径落 unknown 而非缺字段 ---

func TestATDD_66_3_AC5_LegacyKill_UnknownOrigin(t *testing.T) {
	reached := make(chan struct{})
	llmFile := &interruptLLMFile{reached: reached}
	k, baseDir := newInterruptKernel(t, llmFile)

	pid, err := k.Spawn("66.3 AC5 legacy kill", nil, interruptSpawnOpts(baseDir))
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	proc, _ := k.GetProcess(pid)

	<-reached
	if err := k.Kill(pid, types.SIGKILL); err != nil {
		t.Fatalf("Kill: %v", err)
	}
	waitDone(t, proc)

	killArgs := readEventArgs(t, resolveStepDir(k, proc), "Kill")
	if len(killArgs) == 0 {
		t.Fatal("no Kill events in events.jsonl")
	}
	for i, args := range killArgs {
		if args["origin"] != string(types.KillOriginUnknown) {
			t.Errorf("Kill event #%d: origin = %v, want %q", i, args["origin"], types.KillOriginUnknown)
		}
		if _, ok := args["requester"]; !ok {
			t.Errorf("Kill event #%d: missing requester field", i)
		}
	}
}

// --- 用例 2（AC1/AC6）：watchdog 直调 KillWithOrigin ---

func TestATDD_66_3_AC1_WatchdogDirectKill_Origin(t *testing.T) {
	llmFile := newKillParkFile(1)
	k, baseDir := newInterruptKernel(t, llmFile)

	pid, err := k.Spawn("66.3 AC1 watchdog", nil, interruptSpawnOpts(baseDir))
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	proc, _ := k.GetProcess(pid)

	<-llmFile.gate
	attr := KillAttribution{Origin: types.KillOriginWatchdog, Requester: "intent-reconciler"}
	if err := k.KillWithOrigin(pid, types.SIGKILL, attr); err != nil {
		t.Fatalf("KillWithOrigin: %v", err)
	}
	waitDone(t, proc)

	killArgs := killEventsWithSignal(t, resolveStepDir(k, proc), types.SIGKILL)
	if len(killArgs) == 0 {
		t.Fatal("no SIGKILL Kill events")
	}
	for i, args := range killArgs {
		if args["origin"] != string(types.KillOriginWatchdog) {
			t.Errorf("Kill event #%d: origin = %v, want %q", i, args["origin"], types.KillOriginWatchdog)
		}
		if args["requester"] != "intent-reconciler" {
			t.Errorf("Kill event #%d: requester = %v, want %q", i, args["requester"], "intent-reconciler")
		}
		// A non-cascade kill must not fabricate root_pid / root_origin.
		if _, ok := args["root_pid"]; ok {
			t.Errorf("Kill event #%d: unexpected root_pid on a non-cascade kill", i)
		}
		if _, ok := args["escalation"]; ok {
			t.Errorf("Kill event #%d: unexpected escalation on a direct kill", i)
		}
	}
}

// --- 用例 3（AC3）：级联终止 — 树根保留原 origin，后代 parent-cascade + root args ---

func TestATDD_66_3_AC3_SignalTree_ParentCascade(t *testing.T) {
	llmFile := newKillParkFile(2)
	k, baseDir := newInterruptKernel(t, llmFile)

	parentPID, err := k.Spawn("66.3 AC3 parent", nil, interruptSpawnOpts(baseDir))
	if err != nil {
		t.Fatalf("Spawn parent: %v", err)
	}
	childPID, err := k.Spawn("66.3 AC3 child", nil, SpawnOpts{ParentPID: parentPID})
	if err != nil {
		t.Fatalf("Spawn child: %v", err)
	}
	parent, _ := k.GetProcess(parentPID)
	child, _ := k.GetProcess(childPID)

	<-llmFile.gate // both processes parked in Write

	attr := KillAttribution{Origin: types.KillOriginDashboard, Requester: "rnix[4242]"}
	affected, err := k.SignalTreeWithOrigin(parentPID, types.SIGTERM, attr)
	if err != nil {
		t.Fatalf("SignalTreeWithOrigin: %v", err)
	}
	if affected != 2 {
		t.Errorf("affected = %d, want 2 (parent + child)", affected)
	}

	waitDone(t, parent)
	waitDone(t, child)

	// Tree root keeps the original origin and gains no cascade args.
	rootArgs := killEventsWithSignal(t, resolveStepDir(k, parent), types.SIGTERM)
	if len(rootArgs) == 0 {
		t.Fatal("no SIGTERM Kill events for the tree root")
	}
	for i, args := range rootArgs {
		if args["origin"] != string(types.KillOriginDashboard) {
			t.Errorf("root Kill event #%d: origin = %v, want %q", i, args["origin"], types.KillOriginDashboard)
		}
		if _, ok := args["root_pid"]; ok {
			t.Errorf("root Kill event #%d: tree root must not carry root_pid", i)
		}
		if _, ok := args["root_origin"]; ok {
			t.Errorf("root Kill event #%d: tree root must not carry root_origin", i)
		}
	}

	// Descendant is attributed to the cascade, pinned to the root request.
	childArgs := killEventsWithSignal(t, resolveStepDir(k, child), types.SIGTERM)
	if len(childArgs) == 0 {
		t.Fatal("no SIGTERM Kill events for the descendant")
	}
	for i, args := range childArgs {
		if args["origin"] != string(types.KillOriginParentCascade) {
			t.Errorf("child Kill event #%d: origin = %v, want %q", i, args["origin"], types.KillOriginParentCascade)
		}
		if args["root_origin"] != string(types.KillOriginDashboard) {
			t.Errorf("child Kill event #%d: root_origin = %v, want %q", i, args["root_origin"], types.KillOriginDashboard)
		}
		// JSON numbers decode to float64.
		if got, want := args["root_pid"], float64(parentPID); got != want {
			t.Errorf("child Kill event #%d: root_pid = %v, want %v", i, got, want)
		}
		if args["requester"] != "rnix[4242]" {
			t.Errorf("child Kill event #%d: requester = %v, want inherited %q", i, args["requester"], "rnix[4242]")
		}
	}
}

// --- 用例 4（AC4）：SIGKILL 升级继承原 origin + escalation 标注 ---

func TestATDD_66_3_AC4_GraceTimeoutEscalation_InheritsOrigin(t *testing.T) {
	llmFile := &stubbornLLMFile{
		reached: make(chan struct{}),
		release: make(chan struct{}),
	}
	k, baseDir := newInterruptKernel(t, llmFile)

	pid, err := k.Spawn("66.3 AC4 escalation", nil, SpawnOpts{GracePeriod: 50 * time.Millisecond})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	_ = baseDir
	proc, _ := k.GetProcess(pid)

	<-llmFile.reached
	attr := KillAttribution{Origin: types.KillOriginCLI, Requester: "rnix[1337]"}
	if err := k.KillWithOrigin(pid, types.SIGTERM, attr); err != nil {
		t.Fatalf("KillWithOrigin(SIGTERM): %v", err)
	}

	// The process ignores ctx cancellation, so the grace period expires and
	// twoPhaseShutdown escalates to SIGKILL.
	stepDir := resolveStepDir(k, proc)
	waitForKillEvent(t, stepDir, types.SIGKILL, 3*time.Second)

	close(llmFile.release)
	waitDone(t, proc)

	escalated := killEventsWithSignal(t, stepDir, types.SIGKILL)
	if len(escalated) == 0 {
		t.Fatal("no SIGKILL escalation events")
	}
	for i, args := range escalated {
		if args["origin"] != string(types.KillOriginCLI) {
			t.Errorf("escalation event #%d: origin = %v, want inherited %q — an escalation must not masquerade as a new source",
				i, args["origin"], types.KillOriginCLI)
		}
		if args["requester"] != "rnix[1337]" {
			t.Errorf("escalation event #%d: requester = %v, want inherited %q", i, args["requester"], "rnix[1337]")
		}
		if args["escalation"] != "grace_timeout" {
			t.Errorf("escalation event #%d: escalation = %v, want %q", i, args["escalation"], "grace_timeout")
		}
	}

	// The original SIGTERM events must NOT be tagged as escalations.
	for i, args := range killEventsWithSignal(t, stepDir, types.SIGTERM) {
		if _, ok := args["escalation"]; ok {
			t.Errorf("SIGTERM event #%d: unexpected escalation tag on the original request", i)
		}
	}
}

// --- 用例 5（AC1/AC6）：Suspended 进程被杀 → killed_suspended 事件带 origin ---

func TestATDD_66_3_AC1_KilledSuspended_Origin(t *testing.T) {
	reached := make(chan struct{})
	llmFile := &suspendParkingRawLLM{reachedCh: reached}
	k, baseDir := newInterruptKernel(t, llmFile)

	pid, err := k.Spawn("66.3 AC1 killed_suspended", nil, interruptSpawnOpts(baseDir))
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	proc, _ := k.GetProcess(pid)

	<-reached
	if err := k.Kill(pid, types.SIGPAUSE); err != nil {
		t.Fatalf("SIGPAUSE: %v", err)
	}
	// Wait for the suspend to land before the terminating kill.
	deadline := time.Now().Add(3 * time.Second)
	for proc.GetState() != types.StateSuspended && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if state := proc.GetState(); state != types.StateSuspended {
		t.Fatalf("process state = %v, want Suspended", state)
	}

	stepDir := resolveStepDir(k, proc)
	attr := KillAttribution{Origin: types.KillOriginWatchdog, Requester: "immune"}
	if err := k.KillWithOrigin(pid, types.SIGKILL, attr); err != nil {
		t.Fatalf("KillWithOrigin on suspended: %v", err)
	}

	var found bool
	for _, args := range readEventArgs(t, stepDir, "Kill") {
		if args["action"] != "killed_suspended" {
			continue
		}
		found = true
		if args["origin"] != string(types.KillOriginWatchdog) {
			t.Errorf("killed_suspended: origin = %v, want %q", args["origin"], types.KillOriginWatchdog)
		}
		if args["requester"] != "immune" {
			t.Errorf("killed_suspended: requester = %v, want %q", args["requester"], "immune")
		}
	}
	if !found {
		t.Error("no killed_suspended Kill event in events.jsonl")
	}
}
