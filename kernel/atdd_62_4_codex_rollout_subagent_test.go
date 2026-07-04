package kernel

import (
	gocontext "context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rnixai/rnix/agents"
	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/drivers/llm"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

func writeCodexRolloutFixture624(t *testing.T, dir, name string, lines ...string) string {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir rollout dir: %v", err)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatalf("write rollout fixture: %v", err)
	}
	return path
}

func codexRolloutLines624(parentThreadID string) []string {
	return []string{
		`{"timestamp":"2026-07-04T03:35:46.425Z","type":"session_meta","payload":{"id":"019f2b32-5379-7572-b015-bcda06c37e9c","source":{"subagent":{"thread_spawn":{"parent_thread_id":"` + parentThreadID + `","depth":1,"agent_path":"/root/sa_dev_3_3_attempt1","agent_nickname":"Huygens"}}}}}`,
		`{"timestamp":"2026-07-04T03:35:46.425Z","type":"event_msg","payload":{"type":"task_started"}}`,
		`{"timestamp":"2026-07-04T03:35:56.425Z","type":"response_item","payload":{"type":"function_call","name":"exec_command","arguments":"{\"cmd\":\"sed -n '1,80p' kernel/cli_subagent.go\",\"workdir\":\"/repo\"}","call_id":"call_1"}}`,
		`{"timestamp":"2026-07-04T03:35:59.381Z","type":"response_item","payload":{"type":"function_call_output","call_id":"call_1","output":"Chunk ID: abc\nWall time: 2.9560 seconds\nProcess exited with code 0\nOutput:\npackage kernel"}}`,
		`{"timestamp":"2026-07-04T03:36:12.306Z","type":"event_msg","payload":{"type":"agent_message","message":"loaded context"}}`,
		`{"timestamp":"2026-07-04T03:36:20.000Z","type":"event_msg","payload":{"type":"task_complete","message":"done"}}`,
	}
}

func TestATDD_62_4_INT_001_RolloutParentThreadSynthesizesCodexChild(t *testing.T) {
	tk := newObserveTestKernel(t)
	tk.proc.Provider = "codex"
	tk.proc.Model = "gpt-5.5"
	parentThreadID := "019f2b1f-4c78-7ee0-b3af-3557972a7145"
	rolloutDir := t.TempDir()
	writeCodexRolloutFixture624(t, rolloutDir, "rollout-subagent.jsonl", codexRolloutLines624(parentThreadID)...)

	tr := newCodexRolloutTracker(tk.k, tk.proc, tk.baseDir, parentThreadID, rolloutDir)
	if err := tr.scanOnce(); err != nil {
		t.Fatalf("scanOnce: %v", err)
	}

	children := syntheticChildren566(tk.k, tk.proc.UUID)
	if len(children) != 1 {
		t.Fatalf("AC1 FAIL: codex rollout 合成子节点数=%d, want 1", len(children))
	}
	if children[0].ParentUUID != tk.proc.UUID {
		t.Fatalf("AC1 FAIL: ParentUUID=%q, want %q", children[0].ParentUUID, tk.proc.UUID)
	}
	if children[0].Intent != "Huygens" {
		t.Fatalf("AC1 FAIL: Intent=%q, want nickname Huygens", children[0].Intent)
	}
}

func TestATDD_62_4_INT_002_RolloutFunctionCallsWriteChildStepsAndEvents(t *testing.T) {
	tk := newObserveTestKernel(t)
	parentThreadID := "019f2b1f-4c78-7ee0-b3af-3557972a7145"
	rolloutDir := t.TempDir()
	writeCodexRolloutFixture624(t, rolloutDir, "rollout-subagent.jsonl", codexRolloutLines624(parentThreadID)...)

	tr := newCodexRolloutTracker(tk.k, tk.proc, tk.baseDir, parentThreadID, rolloutDir)
	if err := tr.scanOnce(); err != nil {
		t.Fatalf("scanOnce: %v", err)
	}

	children := syntheticChildren566(tk.k, tk.proc.UUID)
	if len(children) != 1 {
		t.Fatalf("setup: children=%d, want 1", len(children))
	}
	childSteps := readStepsForUUID566(t, tk.baseDir, children[0].UUID)
	if len(childSteps) != 1 {
		t.Fatalf("AC2 FAIL: child steps=%d, want 1", len(childSteps))
	}
	if childSteps[0].Action != "exec_command" {
		t.Fatalf("AC2 FAIL: action=%q, want exec_command", childSteps[0].Action)
	}
	if !strings.Contains(childSteps[0].ToolInput, "kernel/cli_subagent.go") {
		t.Fatalf("AC2 FAIL: missing function_call arguments in tool_input: %q", childSteps[0].ToolInput)
	}
	if !strings.Contains(childSteps[0].ToolResult, "package kernel") {
		t.Fatalf("AC2 FAIL: missing function_call_output in tool_result: %q", childSteps[0].ToolResult)
	}
	if childSteps[0].ToolDuration != 2956*time.Millisecond {
		t.Fatalf("AC2 FAIL: ToolDuration=%s, want 2.956s from rollout Wall time", childSteps[0].ToolDuration)
	}

	childEvents := readEventsForUUID566(t, tk.baseDir, children[0].UUID)
	if len(childEvents) == 0 {
		t.Fatal("AC2 FAIL: child events.jsonl empty")
	}
}

func TestATDD_62_4_INT_003_HostFinalizeClosesRunningCodexChildren(t *testing.T) {
	tk := newObserveTestKernel(t)
	parentThreadID := "019f2b1f-4c78-7ee0-b3af-3557972a7145"
	rolloutDir := t.TempDir()
	lines := codexRolloutLines624(parentThreadID)
	lines = lines[:len(lines)-1] // no task_complete: host done/error must finalize.
	writeCodexRolloutFixture624(t, rolloutDir, "rollout-running-subagent.jsonl", lines...)

	tr := newCodexRolloutTracker(tk.k, tk.proc, tk.baseDir, parentThreadID, rolloutDir)
	if err := tr.scanOnce(); err != nil {
		t.Fatalf("scanOnce: %v", err)
	}
	children := syntheticChildren566(tk.k, tk.proc.UUID)
	if len(children) != 1 || children[0].State != types.StateRunning {
		t.Fatalf("setup: child should be running before host finalize, got %+v", children)
	}

	tr.finalizeAll()
	children = syntheticChildren566(tk.k, tk.proc.UUID)
	if len(children) != 1 {
		t.Fatalf("AC3 FAIL: child count after finalize=%d, want 1", len(children))
	}
	if children[0].State != types.StateDead {
		t.Fatalf("AC3 FAIL: child state after host finalize=%v, want Dead", children[0].State)
	}
}

func TestATDD_62_4_INT_004_MissingRolloutDirDegradesSilently(t *testing.T) {
	tk := newObserveTestKernel(t)
	tr := newCodexRolloutTracker(tk.k, tk.proc, tk.baseDir, "parent-thread", filepath.Join(t.TempDir(), "missing"))
	if err := tr.scanOnce(); err != nil {
		t.Fatalf("AC4 FAIL: missing rollout dir should not fail host observation: %v", err)
	}
	if n := len(syntheticChildren566(tk.k, tk.proc.UUID)); n != 0 {
		t.Fatalf("AC4 FAIL: missing rollout dir synthesized %d children, want 0", n)
	}
}

func TestATDD_62_4_INT_005_ObserveDriverInitStartsCodexRolloutTracker(t *testing.T) {
	parentThreadID := "019f2b1f-4c78-7ee0-b3af-3557972a7145"
	rolloutDir := t.TempDir()
	writeCodexRolloutFixture624(t, rolloutDir, "rollout-subagent.jsonl", codexRolloutLines624(parentThreadID)...)

	oldDir := codexRolloutDirForNow
	codexRolloutDirForNow = func(time.Time) string { return rolloutDir }
	t.Cleanup(func() { codexRolloutDirForNow = oldDir })

	h := newSubagent566Harness(t, "/dev/llm/codex")
	h.feed(map[string]any{
		"type":      "system",
		"subtype":   "init",
		"thread_id": parentThreadID,
	})

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if n := len(syntheticChildren566(h.tk.k, h.tk.proc.UUID)); n == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	children := syntheticChildren566(h.tk.k, h.tk.proc.UUID)
	if len(children) != 1 {
		t.Fatalf("AC1 FAIL: observe DriverInit did not start rollout tracker; children=%d", len(children))
	}

	h.feed(evtDone566("host done"))
	children = syntheticChildren566(h.tk.k, h.tk.proc.UUID)
	if children[0].State != types.StateDead {
		t.Fatalf("AC3 FAIL: host done did not finalize codex rollout child, state=%v", children[0].State)
	}
}

type atdd624FailingCodexStream struct {
	handler func(map[string]any)
}

func (f *atdd624FailingCodexStream) Write(_ gocontext.Context, _ []byte) error {
	if f.handler != nil {
		f.handler(map[string]any{
			"type":      "system",
			"subtype":   "init",
			"thread_id": "parent-abort-thread",
		})
	}
	return errors.New("codex stream aborted before terminal event")
}

func (f *atdd624FailingCodexStream) Read(_ int) ([]byte, error) { return nil, nil }
func (f *atdd624FailingCodexStream) Close() error               { return nil }
func (f *atdd624FailingCodexStream) Stat() (vfs.FileStat, error) {
	return vfs.FileStat{Name: "/dev/llm/claude", IsDevice: true}, nil
}
func (f *atdd624FailingCodexStream) SupportsToolCalling() bool { return true }
func (f *atdd624FailingCodexStream) SetStreamHandler(fn func(map[string]any)) {
	f.handler = fn
}
func (f *atdd624FailingCodexStream) DriverType() string { return llm.DriverCodexCLI }

func TestATDD_62_4_INT_006_WriteAbortFinalizesCodexRolloutChild(t *testing.T) {
	parentThreadID := "parent-abort-thread"
	rolloutDir := t.TempDir()
	lines := codexRolloutLines624(parentThreadID)
	lines = lines[:len(lines)-1] // no task_complete: Write abort must clean up.
	writeCodexRolloutFixture624(t, rolloutDir, "rollout-abort-subagent.jsonl", lines...)

	oldDir := codexRolloutDirForNow
	codexRolloutDirForNow = func(time.Time) string { return rolloutDir }
	t.Cleanup(func() { codexRolloutDirForNow = oldDir })

	llmFile := &atdd624FailingCodexStream{}
	reg := vfs.NewDeviceRegistry()
	if err := reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return llmFile, nil
	}); err != nil {
		t.Fatalf("register codex stream stub: %v", err)
	}
	k := NewKernel(vfs.NewVFS(reg), rnixctx.NewManager(), nil)
	t.Cleanup(k.Shutdown)
	k.SetStepDataDir(t.TempDir())

	pid, err := k.Spawn("trigger codex rollout abort", &agents.AgentInfo{
		Manifest: agents.AgentManifest{Name: "atdd62-codex-abort"},
	}, SpawnOpts{MaxTurns: 1})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	var children []vfs.ProcInfo
	for time.Now().Before(deadline) {
		if proc, ok := k.procTable.Load(pid); ok {
			children = syntheticChildren566(k, proc.UUID)
		}
		if len(children) == 1 && children[0].State == types.StateDead {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(children) != 1 {
		t.Fatalf("AC3 cleanup FAIL: synthetic codex children=%d, want 1", len(children))
	}
	if children[0].State != types.StateDead {
		t.Fatalf("AC3 cleanup FAIL: stream abort left codex child state=%v, want Dead", children[0].State)
	}
}
