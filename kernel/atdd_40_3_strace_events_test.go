package kernel

import (
	"os"
	"path/filepath"
	"testing"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// ============================================================================
// ATDD Tests for Story 40.3: Strace 事件 — binary 解析 + capability 探测
//
// AC1: ProcInfo.DriverMeta 字段存在且可赋值
// AC2: Strace 事件 — claude_cli.resolve (binary 解析结果)
// AC3: Strace 事件 — claude_cli.capabilities (capability 探测结果)
// ============================================================================

// mockLLMFileWithMeta extends mockLLMFile with DriverMeta() support so that
// spawn.go's driverMetaProvider type assertion succeeds and populates
// proc.DriverMeta + emits strace events during the spawn path.
type mockLLMFileWithMeta struct {
	mockLLMFile
	driverMeta map[string]string
}

func (f *mockLLMFileWithMeta) DriverMeta() map[string]string {
	return f.driverMeta
}

// newDriverMetaTestKernel creates a kernel with a mock LLM device, backed by
// an EventWriter for disk event capture. Returns the kernel, spawned process,
// and the events.jsonl directory for assertion.
func newDriverMetaTestKernel(t *testing.T) (*KernelImpl, *Process, string) {
	t.Helper()

	llmFile := &mockLLMFile{
		readData: makeLLMResponse("done", 10),
	}
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return llmFile, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()

	baseDir := t.TempDir()
	k := NewKernel(v, ctxMgr, nil)
	k.stepDataDir = baseDir
	t.Cleanup(k.Shutdown)

	pid, err := k.Spawn("test driver meta events", nil, SpawnOpts{
		SkipReasonLoop: true,
		EventWriterFactory: func(proc *Process) (*EventWriter, error) {
			return NewEventWriter(baseDir, proc.UUID)
		},
	})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatalf("process %d not found after spawn", pid)
	}

	return k, proc, baseDir
}

// newDriverMetaTestKernelWithMeta creates a kernel where the mock LLM file
// implements DriverMeta(), causing spawn to populate proc.DriverMeta and emit
// claude_cli.resolve / claude_cli.capabilities strace events.
// Uses SkipReasonLoop=false so the LLM device open path runs (where DriverMeta
// is populated), then waits for the process to complete via the mock response.
func newDriverMetaTestKernelWithMeta(t *testing.T, meta map[string]string) (*KernelImpl, *Process, string) {
	t.Helper()

	llmFile := &mockLLMFileWithMeta{
		mockLLMFile: mockLLMFile{readData: makeLLMResponse("done", 10)},
		driverMeta:  meta,
	}
	reg := vfs.NewDeviceRegistry()
	_ = reg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return llmFile, nil
	})
	v := vfs.NewVFS(reg)
	ctxMgr := rnixctx.NewManager()

	baseDir := t.TempDir()
	k := NewKernel(v, ctxMgr, nil)
	k.stepDataDir = baseDir
	t.Cleanup(k.Shutdown)

	pid, err := k.Spawn("test driver meta events", nil, SpawnOpts{
		EventWriterFactory: func(proc *Process) (*EventWriter, error) {
			return NewEventWriter(baseDir, proc.UUID)
		},
	})
	if err != nil {
		t.Fatalf("Spawn failed: %v", err)
	}

	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatalf("process %d not found after spawn", pid)
	}

	// Wait for process to complete (reason loop exits on "done" response)
	<-proc.Done

	return k, proc, baseDir
}

// ---------------------------------------------------------------------------
// AC #1: ProcInfo.DriverMeta 字段存在且可赋值
// ---------------------------------------------------------------------------

func TestATDD_40_3_AC1_ProcInfo_HasDriverMetaField(t *testing.T) {
	t.Parallel()

	info := vfs.ProcInfo{
		PID:    types.PID(1),
		Intent: "test",
		DriverMeta: map[string]string{
			"resolved_bin":    "/usr/local/bin/claude",
			"permission_mode": "bypassPermissions",
		},
	}

	if info.DriverMeta["resolved_bin"] != "/usr/local/bin/claude" {
		t.Error("AC1 FAIL: ProcInfo.DriverMeta field not readable")
	}
}

// ---------------------------------------------------------------------------
// AC #2: Strace 事件 — claude_cli.resolve
// ---------------------------------------------------------------------------

func TestATDD_40_3_AC2_Spawn_EmitsResolveEvent(t *testing.T) {
	meta := map[string]string{
		"resolved_bin":         "/usr/local/bin/claude",
		"permission_mode":      "bypassPermissions",
		"cap_partial_messages": "true",
		"cap_add_dir":          "false",
		"cap_permission_mode":  "true",
		"fallback_candidates":  "claude,openclaude",
	}

	_, proc, baseDir := newDriverMetaTestKernelWithMeta(t, meta)

	if proc.eventWriter != nil {
		_ = proc.eventWriter.Flush()
	}

	eventsPath := filepath.Join(baseDir, "data", "steps", proc.UUID, "events.jsonl")
	if _, err := os.Stat(eventsPath); err != nil {
		t.Fatalf("events.jsonl not created: %v", err)
	}

	rows, err := ReadAllEvents(eventsPath)
	if err != nil {
		t.Fatalf("ReadAllEvents: %v", err)
	}

	found := false
	for _, row := range rows {
		if row.Syscall == "claude_cli.resolve" {
			found = true
			resolvedPath, ok := row.Args["resolved_path"].(string)
			if !ok || resolvedPath == "" {
				t.Error("AC2 FAIL: claude_cli.resolve event missing resolved_path arg")
			}
			if !filepath.IsAbs(resolvedPath) {
				t.Errorf("AC2 FAIL: resolved_path should be absolute, got %q", resolvedPath)
			}
			// candidates carries the attempted fallback binary list (JSON array
			// decodes to []any after the events.jsonl round-trip).
			cands, ok := row.Args["candidates"].([]any)
			if !ok || len(cands) == 0 {
				t.Errorf("AC2 FAIL: claude_cli.resolve event missing candidates list, got %v", row.Args["candidates"])
			}
			break
		}
	}

	if !found {
		t.Error("AC2 FAIL: claude_cli.resolve event not found in events.jsonl after spawn")
	}
}

// ---------------------------------------------------------------------------
// AC #3: Strace 事件 — claude_cli.capabilities
// ---------------------------------------------------------------------------

func TestATDD_40_3_AC3_Spawn_EmitsCapabilitiesEvent(t *testing.T) {
	meta := map[string]string{
		"resolved_bin":         "/usr/local/bin/claude",
		"permission_mode":      "bypassPermissions",
		"cap_partial_messages": "true",
		"cap_add_dir":          "false",
		"cap_permission_mode":  "true",
		"probe_duration_ms":    "12",
	}

	_, proc, baseDir := newDriverMetaTestKernelWithMeta(t, meta)

	if proc.eventWriter != nil {
		_ = proc.eventWriter.Flush()
	}

	eventsPath := filepath.Join(baseDir, "data", "steps", proc.UUID, "events.jsonl")
	if _, err := os.Stat(eventsPath); err != nil {
		t.Fatalf("events.jsonl not created: %v", err)
	}

	rows, err := ReadAllEvents(eventsPath)
	if err != nil {
		t.Fatalf("ReadAllEvents: %v", err)
	}

	found := false
	for _, row := range rows {
		if row.Syscall == "claude_cli.capabilities" {
			found = true
			for _, key := range []string{"partial_messages", "add_dir", "permission_mode", "probe_duration_ms"} {
				if _, ok := row.Args[key]; !ok {
					t.Errorf("AC3 FAIL: claude_cli.capabilities event missing arg %q", key)
				}
			}
			break
		}
	}

	if !found {
		t.Error("AC3 FAIL: claude_cli.capabilities event not found in events.jsonl after spawn")
	}
}

// ---------------------------------------------------------------------------
// AC #1 (negative): 非 Claude CLI driver 不发射 driver 事件
// ---------------------------------------------------------------------------

func TestATDD_40_3_AC1_NonClaudeDriver_NoDriverEvents(t *testing.T) {
	_, proc, baseDir := newDriverMetaTestKernel(t)

	if proc.eventWriter != nil {
		_ = proc.eventWriter.Flush()
	}

	eventsPath := filepath.Join(baseDir, "data", "steps", proc.UUID, "events.jsonl")
	if _, err := os.Stat(eventsPath); err != nil {
		return
	}

	rows, err := ReadAllEvents(eventsPath)
	if err != nil {
		t.Fatalf("ReadAllEvents: %v", err)
	}

	for _, row := range rows {
		if row.Syscall == "claude_cli.resolve" || row.Syscall == "claude_cli.capabilities" {
			t.Errorf("AC1 FAIL: non-Claude driver should NOT emit %s event", row.Syscall)
		}
	}
}
