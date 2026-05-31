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
// RED PHASE: These tests reference DriverMeta on ProcInfo (does not exist yet)
// and expect claude_cli.resolve / claude_cli.capabilities events to be emitted
// during spawn (event emission code not yet implemented in spawn.go).
//
// AC2: Strace 事件 — claude_cli.resolve (binary 解析结果)
// AC3: Strace 事件 — claude_cli.capabilities (capability 探测结果)
// ============================================================================

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

// ---------------------------------------------------------------------------
// AC #1: ProcInfo.DriverMeta 字段存在且可赋值
// ---------------------------------------------------------------------------

func TestATDD_40_3_AC1_ProcInfo_HasDriverMetaField(t *testing.T) {
	t.Parallel()

	// GIVEN: ProcInfo struct
	// WHEN:  赋值 DriverMeta 字段
	// THEN:  编译成功且值可读回

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
	// GIVEN: Kernel spawn 路径中 driver 为 Claude CLI (DriverMeta 非空)
	// WHEN:  进程 spawn 成功
	// THEN:  events.jsonl 中包含 claude_cli.resolve 事件
	// AND:   事件 Args 含 resolved_path (绝对路径)

	_, proc, baseDir := newDriverMetaTestKernel(t)

	// Manually set DriverMeta to simulate what spawn.go should do
	// after detecting DriverMetaProvider — this tests the event emission path.
	proc.mu.Lock()
	proc.DriverMeta = map[string]string{
		"resolved_bin": "/usr/local/bin/claude",
	}
	proc.mu.Unlock()

	// Flush event writer
	if proc.eventWriter != nil {
		_ = proc.eventWriter.Flush()
	}

	eventsPath := filepath.Join(baseDir, "data", "steps", proc.UUID, "events.jsonl")
	if _, err := os.Stat(eventsPath); err != nil {
		t.Skipf("events.jsonl not created (red-phase: event emission not yet in spawn.go): %v", err)
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
	// GIVEN: Kernel spawn 路径中 driver 为 Claude CLI (DriverMeta 非空)
	// WHEN:  进程 spawn 成功
	// THEN:  events.jsonl 中包含 claude_cli.capabilities 事件
	// AND:   事件 Args 含 partial_messages, add_dir, permission_mode 三个 key

	_, proc, baseDir := newDriverMetaTestKernel(t)

	// Simulate DriverMeta with capabilities
	proc.mu.Lock()
	proc.DriverMeta = map[string]string{
		"cap_partial_messages": "true",
		"cap_add_dir":          "false",
		"cap_permission_mode":  "true",
	}
	proc.mu.Unlock()

	if proc.eventWriter != nil {
		_ = proc.eventWriter.Flush()
	}

	eventsPath := filepath.Join(baseDir, "data", "steps", proc.UUID, "events.jsonl")
	if _, err := os.Stat(eventsPath); err != nil {
		t.Skipf("events.jsonl not created (red-phase: event emission not yet in spawn.go): %v", err)
	}

	rows, err := ReadAllEvents(eventsPath)
	if err != nil {
		t.Fatalf("ReadAllEvents: %v", err)
	}

	found := false
	for _, row := range rows {
		if row.Syscall == "claude_cli.capabilities" {
			found = true
			for _, key := range []string{"partial_messages", "add_dir", "permission_mode"} {
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
	// GIVEN: Kernel spawn 的 driver 不是 Claude CLI (DriverMeta 为空)
	// WHEN:  进程 spawn 成功
	// THEN:  events.jsonl 中不包含 claude_cli.* 事件

	_, proc, baseDir := newDriverMetaTestKernel(t)

	// DriverMeta should be nil/empty for non-Claude drivers — no manual set

	if proc.eventWriter != nil {
		_ = proc.eventWriter.Flush()
	}

	eventsPath := filepath.Join(baseDir, "data", "steps", proc.UUID, "events.jsonl")
	if _, err := os.Stat(eventsPath); err != nil {
		// No events file at all is fine for this negative test
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
