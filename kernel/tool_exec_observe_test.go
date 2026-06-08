package kernel

// Observation-stack regression tests for the four observability gaps surfaced
// while debugging the EchoMatrix bmad-autopilot Timeline silence:
//
//   - Fix 2: ActionSpawn failure must persist a step record so Agent() failures
//     appear in the Timeline pane (without this, a step that only contained a
//     failing Agent call had no steps.jsonl row at all).
//   - Fix 3: vfs fast-path tools (Read/Write/Edit/Glob/Grep) must emit
//     Open/Write/Read/Close syscall events the same way the default branch
//     already did — events.jsonl/heatmap/strace previously missed all of them.
//   - Fix 4: failed vfs tool calls must emit a ReasonStep `native_tool_call`
//     event, mirroring the success path so failures aren't silent at the
//     syscall layer.

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// observeTestKernel sets up a minimal kernel + process with stepWriter and
// eventWriter attached, so test code can invoke kernel helpers directly and
// then read back the on-disk artifacts.
type observeTestKernel struct {
	k       *KernelImpl
	proc    *Process
	baseDir string
}

func newObserveTestKernel(t *testing.T) *observeTestKernel {
	t.Helper()

	baseDir := t.TempDir()

	reg := vfs.NewDeviceRegistry()
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	t.Cleanup(k.Shutdown)
	k.SetStepDataDir(baseDir)

	proc := NewProcess(0, "observe test", nil)

	sw, err := NewStepWriter(baseDir, proc.UUID)
	if err != nil {
		t.Fatalf("NewStepWriter: %v", err)
	}
	ew, err := NewEventWriter(baseDir, proc.UUID)
	if err != nil {
		t.Fatalf("NewEventWriter: %v", err)
	}
	t.Cleanup(func() { _ = sw.Close() })
	t.Cleanup(func() { _ = ew.Close() })

	proc.stepWriter = sw
	proc.eventWriter = ew

	return &observeTestKernel{k: k, proc: proc, baseDir: baseDir}
}

func (tk *observeTestKernel) readSteps(t *testing.T) []types.StepRecord {
	t.Helper()
	path := filepath.Join(tk.baseDir, "steps", tk.proc.UUID, "steps.jsonl")
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open steps.jsonl: %v", err)
	}
	defer f.Close()

	var out []types.StepRecord
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1<<20), 1<<20)
	for scanner.Scan() {
		var rec types.StepRecord
		if err := json.Unmarshal(scanner.Bytes(), &rec); err != nil {
			t.Fatalf("unmarshal step: %v", err)
		}
		out = append(out, rec)
	}
	return out
}

func (tk *observeTestKernel) readEvents(t *testing.T) []SyscallEventDisk {
	t.Helper()
	path := filepath.Join(tk.baseDir, "steps", tk.proc.UUID, "events.jsonl")
	events, err := ReadAllEvents(path)
	if err != nil {
		t.Fatalf("ReadAllEvents: %v", err)
	}
	return events
}

// ---------------------------------------------------------------------------
// Fix 2: ActionSpawn failure persists step record with non-empty ToolError
// and a synthetic ToolCallRecord so HasError aggregation lights up.
// ---------------------------------------------------------------------------

func TestWriteSpawnFailureStepRecord_PersistsErrorRow(t *testing.T) {
	tk := newObserveTestKernel(t)

	tc := llmToolCall{
		ID:   "tc_spawn_1",
		Name: "Agent",
		Input: map[string]any{
			"agent":  "general-purpose",
			"intent": "do thing",
		},
	}
	errMsg := "Agent error: agent \"general-purpose\" load failed: agent directory not found"

	tk.k.writeSpawnFailureStepRecord(tk.proc, 7, tc, errMsg, nil, "", nil, time.Now())

	// Close stepWriter so the bufio.Writer flushes before we read.
	if tk.proc.stepWriter != nil {
		_ = tk.proc.stepWriter.Close()
		tk.proc.stepWriter = nil
	}

	recs := tk.readSteps(t)
	if len(recs) != 1 {
		t.Fatalf("expected 1 step record, got %d", len(recs))
	}
	rec := recs[0]
	if rec.Step != 7 {
		t.Errorf("Step = %d, want 7", rec.Step)
	}
	if rec.Action != "spawn" {
		t.Errorf("Action = %q, want %q", rec.Action, "spawn")
	}
	if rec.ToolError == "" || !strings.Contains(rec.ToolError, "general-purpose") {
		t.Errorf("ToolError = %q, want non-empty mentioning agent name", rec.ToolError)
	}
	if len(rec.ToolCalls) != 1 {
		t.Fatalf("len(ToolCalls) = %d, want 1", len(rec.ToolCalls))
	}
	if rec.ToolCalls[0].Name != "Agent" {
		t.Errorf("ToolCalls[0].Name = %q, want %q", rec.ToolCalls[0].Name, "Agent")
	}
	if rec.ToolCalls[0].Error != errMsg {
		t.Errorf("ToolCalls[0].Error mismatch")
	}
	if !strings.Contains(rec.ToolCalls[0].Input, "general-purpose") {
		t.Errorf("ToolCalls[0].Input missing agent field: %q", rec.ToolCalls[0].Input)
	}
}

// ---------------------------------------------------------------------------
// Fix 3: vfs fast-path emit helpers produce Open/Read/Write/Close events.
//
// Tests bypass the full executeVFSTool/reasonStep stack and exercise the
// helpers directly with a stub VFS device. Symmetry is verified against
// the default-branch behavior previously documented in events.jsonl.
// ---------------------------------------------------------------------------

type stubVFSFile struct {
	readBuf  []byte
	writeBuf []byte
	closed   bool
}

func (f *stubVFSFile) Read(length int) ([]byte, error) {
	n := min(length, len(f.readBuf))
	out := append([]byte(nil), f.readBuf[:n]...)
	f.readBuf = f.readBuf[n:]
	return out, nil
}
func (f *stubVFSFile) Write(_ context.Context, p []byte) error {
	f.writeBuf = append(f.writeBuf, p...)
	return nil
}
func (f *stubVFSFile) Close() error { f.closed = true; return nil }
func (f *stubVFSFile) Stat() (vfs.FileStat, error) {
	return vfs.FileStat{Name: "stub", Size: int64(len(f.readBuf)), IsDevice: true}, nil
}

func TestVfsHelpers_EmitOpenReadWriteClose(t *testing.T) {
	tk := newObserveTestKernel(t)

	// Register a stub device so Open/Write/Read/Close succeed.
	stub := &stubVFSFile{readBuf: []byte(`{"ok":true}`)}
	if err := tk.k.vfs.DeviceRegistry().Register("/dev/stub", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return stub, nil
	}); err != nil {
		t.Fatalf("Register stub device: %v", err)
	}

	fd, err := tk.k.vfsOpenWithEvent(tk.proc, "/dev/stub", vfs.O_RDWR)
	if err != nil {
		t.Fatalf("vfsOpenWithEvent: %v", err)
	}
	if err := tk.k.vfsWriteWithEvent(tk.proc, fd, []byte("hello")); err != nil {
		t.Fatalf("vfsWriteWithEvent: %v", err)
	}
	if _, err := tk.k.vfsReadWithEvent(tk.proc, fd, 1<<20); err != nil {
		t.Fatalf("vfsReadWithEvent: %v", err)
	}
	if err := tk.k.vfsCloseWithEvent(tk.proc, fd); err != nil {
		t.Fatalf("vfsCloseWithEvent: %v", err)
	}

	if tk.proc.eventWriter != nil {
		_ = tk.proc.eventWriter.Close()
		tk.proc.eventWriter = nil
	}

	events := tk.readEvents(t)
	syscalls := map[string]int{}
	for _, ev := range events {
		syscalls[ev.Syscall]++
	}
	for _, want := range []string{"Open", "Write", "Read", "Close"} {
		if syscalls[want] != 1 {
			t.Errorf("expected exactly 1 %s syscall event, got %d (full: %v)", want, syscalls[want], syscalls)
		}
	}
}

// ---------------------------------------------------------------------------
// Fix 4: vfs failure branch emits ReasonStep `native_tool_call` event.
//
// The default-branch path triggers Write failure when the underlying device
// rejects a Write; the new emit ensures events.jsonl records the failed call
// rather than leaving Debug timeline blind to it.
// ---------------------------------------------------------------------------

type alwaysFailVFSFile struct{}

func (alwaysFailVFSFile) Read(_ int) ([]byte, error) {
	return nil, types.NewDriverError("read", "stub", errors.New("stub read fail"), types.ErrInternal)
}
func (alwaysFailVFSFile) Write(_ context.Context, _ []byte) error {
	return types.NewDriverError("write", "stub", errors.New("stub write fail"), types.ErrInternal)
}
func (alwaysFailVFSFile) Close() error                   { return nil }
func (alwaysFailVFSFile) Stat() (vfs.FileStat, error)    { return vfs.FileStat{Name: "fail", IsDevice: true}, nil }

func TestExecuteToolCalls_VFSFailureEmitsReasonStep(t *testing.T) {
	tk := newObserveTestKernel(t)

	if err := tk.k.vfs.DeviceRegistry().Register("/dev/failwrite", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return alwaysFailVFSFile{}, nil
	}); err != nil {
		t.Fatalf("Register fail device: %v", err)
	}

	// Wire up tool mapping so executeToolCalls treats "FailTool" as vfs targeting /dev/failwrite.
	tk.proc.toolMap = map[string]toolMapping{
		"FailTool": {Type: "vfs", VFSPath: "/dev/failwrite"},
	}

	resp := llmResponse{
		ToolCalls: []llmToolCall{
			{ID: "tc_1", Name: "FailTool", Input: map[string]any{"foo": "bar"}},
		},
	}
	// executeToolCalls needs a CtxID; allocate one.
	ctxID, err := tk.k.ctxMgr.CtxAlloc(16)
	if err != nil {
		t.Fatalf("CtxAlloc: %v", err)
	}
	tk.proc.CtxID = ctxID

	counter := &errFingerprintCounter{}
	toolCalls, shouldContinue := tk.k.executeToolCalls(tk.proc, resp, 3, time.Now(), counter, nil, "")
	if !shouldContinue {
		t.Fatal("expected shouldContinue=true after non-fatal tool error")
	}
	if len(toolCalls) != 1 {
		t.Fatalf("len(toolCalls) = %d, want 1", len(toolCalls))
	}
	if toolCalls[0].Error == "" {
		t.Error("expected non-empty Error on the recorded ToolCallRecord")
	}

	if tk.proc.eventWriter != nil {
		_ = tk.proc.eventWriter.Close()
		tk.proc.eventWriter = nil
	}

	events := tk.readEvents(t)
	var seenFailEvent bool
	for _, ev := range events {
		if ev.Syscall != "ReasonStep" {
			continue
		}
		if action, _ := ev.Args["action"].(string); action != "native_tool_call" {
			continue
		}
		if tool, _ := ev.Args["tool"].(string); tool != "FailTool" {
			continue
		}
		if errStr, _ := ev.Args["error"].(string); errStr != "" {
			seenFailEvent = true
			break
		}
	}
	if !seenFailEvent {
		t.Errorf("expected a ReasonStep native_tool_call event with non-empty error for FailTool — none found.\nevents: %+v", events)
	}
}
