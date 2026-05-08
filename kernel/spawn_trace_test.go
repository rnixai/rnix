package kernel

import (
	"testing"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/debug"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

func newTraceTestKernel(t testing.TB) *KernelImpl {
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return &mockLLMFile{}, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()
	k := NewKernel(v, ctxMgr, nil)
	t.Cleanup(k.Shutdown)
	return k
}

func TestSpawn_TopLevel_AutoGeneratesTraceID(t *testing.T) {
	k := newTraceTestKernel(t)

	pid, err := k.Spawn("test intent", nil, SpawnOpts{
		SkipReasonLoop: true,
	})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatalf("process %d not found", pid)
	}

	if proc.TraceID == "" {
		t.Error("top-level spawn should auto-generate TraceID, got empty")
	}
	if proc.SpanID == "" {
		t.Error("top-level spawn should auto-generate SpanID, got empty")
	}
	if proc.ParentSpanID != "" {
		t.Errorf("top-level spawn should have empty ParentSpanID, got %q", proc.ParentSpanID)
	}
}

func TestSpawn_ExplicitTraceID_NotOverridden(t *testing.T) {
	k := newTraceTestKernel(t)
	explicit := types.TraceID("compose-trace-abc123")
	parentSpan := types.SpanID("parent-span-xyz")

	pid, err := k.Spawn("compose task", nil, SpawnOpts{
		SkipReasonLoop: true,
		TraceID:        explicit,
		ParentSpanID:   parentSpan,
	})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatalf("process %d not found", pid)
	}

	if proc.TraceID != explicit {
		t.Errorf("TraceID = %q, want %q (explicit should not be overridden)", proc.TraceID, explicit)
	}
	if proc.ParentSpanID != parentSpan {
		t.Errorf("ParentSpanID = %q, want %q", proc.ParentSpanID, parentSpan)
	}
	if proc.SpanID == "" {
		t.Error("SpanID should be generated even with explicit TraceID")
	}
}

func TestSpawn_ChildInheritsParentTraceID(t *testing.T) {
	k := newTraceTestKernel(t)

	parentPID, err := k.Spawn("parent intent", nil, SpawnOpts{
		SkipReasonLoop: true,
	})
	if err != nil {
		t.Fatalf("parent Spawn failed: %v", err)
	}

	parentProc, ok := k.GetProcess(parentPID)
	if !ok {
		t.Fatalf("parent process %d not found", parentPID)
	}
	parentTraceID := parentProc.TraceID
	parentSpanID := parentProc.SpanID

	childPID, err := k.Spawn("child intent", nil, SpawnOpts{
		SkipReasonLoop: true,
		ParentPID:      parentPID,
		TraceID:        parentTraceID,
		ParentSpanID:   parentSpanID,
	})
	if err != nil {
		t.Fatalf("child Spawn failed: %v", err)
	}

	childProc, ok := k.GetProcess(childPID)
	if !ok {
		t.Fatalf("child process %d not found", childPID)
	}

	if childProc.TraceID != parentTraceID {
		t.Errorf("child TraceID = %q, want %q (should inherit parent)", childProc.TraceID, parentTraceID)
	}
	if childProc.ParentSpanID != parentSpanID {
		t.Errorf("child ParentSpanID = %q, want %q", childProc.ParentSpanID, parentSpanID)
	}
	if childProc.SpanID == "" {
		t.Error("child SpanID should be generated")
	}
	if childProc.SpanID == parentSpanID {
		t.Error("child SpanID should differ from parent SpanID")
	}
}

func TestSpawn_SpanRecorderReceivesSpan(t *testing.T) {
	k := newTraceTestKernel(t)

	pid, err := k.Spawn("traced intent", nil, SpawnOpts{
		SkipReasonLoop: true,
	})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatalf("process %d not found", pid)
	}

	span := k.spanRecorder.GetSpan(pid)
	if span == nil {
		t.Fatal("spanRecorder should have a span for the auto-traced process")
	}
	if span.TraceID != proc.TraceID {
		t.Errorf("span TraceID = %q, want %q", span.TraceID, proc.TraceID)
	}
	if span.SpanID != proc.SpanID {
		t.Errorf("span SpanID = %q, want %q", span.SpanID, proc.SpanID)
	}
	if span.Name != "traced intent" {
		t.Errorf("span Name = %q, want %q", span.Name, "traced intent")
	}

	// End span and verify status
	k.spanRecorder.EndSpan(pid, debug.SpanOK)
	ended := k.spanRecorder.GetSpan(pid)
	if ended == nil {
		t.Fatal("span should still be accessible after EndSpan")
	}
	if ended.Status != debug.SpanOK {
		t.Errorf("span Status = %v, want SpanOK", ended.Status)
	}
}
