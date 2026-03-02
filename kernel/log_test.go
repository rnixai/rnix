package kernel

import (
	"testing"
	"time"

	"github.com/gonewx/crux/internal/types"
)

// ============================================================
// ATDD RED PHASE — Story 10.2: crux log 分类推理日志
// Tests assert EXPECTED behavior. They will NOT COMPILE until
// LogChan, emitLog, and GetLogChan are implemented.
// ============================================================

// --- 10.2-UNIT-006: NewProcess creates LogChan with buffer 256 ---

func TestNewProcess_LogChan(t *testing.T) {
	proc := NewProcess(0, "test", nil)
	if proc.LogChan == nil {
		t.Fatal("LogChan should not be nil after NewProcess")
	}
	if cap(proc.LogChan) != 256 {
		t.Errorf("LogChan cap = %d, want 256", cap(proc.LogChan))
	}
}

// --- 10.2-UNIT-007: emitLog sends correct LogEntry to LogChan ---

func TestEmitLog_SendsEntry(t *testing.T) {
	k := newSimpleKernel(t)
	proc := NewProcess(0, "test", nil)
	_ = proc.Start()
	k.AddProcess(proc)

	k.emitLog(proc, 1, types.LogThink, "reasoning about the problem", "")

	select {
	case entry := <-proc.LogChan:
		if entry.Category != types.LogThink {
			t.Errorf("Category = %q, want %q", entry.Category, types.LogThink)
		}
		if entry.Content != "reasoning about the problem" {
			t.Errorf("Content = %q, want %q", entry.Content, "reasoning about the problem")
		}
		if entry.Step != 1 {
			t.Errorf("Step = %d, want 1", entry.Step)
		}
		if entry.PID != proc.PID {
			t.Errorf("PID = %d, want %d", entry.PID, proc.PID)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for log entry")
	}
}

// --- 10.2-UNIT-008: emitLog tool category includes ToolPath ---

func TestEmitLog_ToolCategory(t *testing.T) {
	k := newSimpleKernel(t)
	proc := NewProcess(0, "test", nil)
	_ = proc.Start()
	k.AddProcess(proc)

	k.emitLog(proc, 2, types.LogTool, "Read src/main.go (2847 bytes)", "/dev/fs")

	select {
	case entry := <-proc.LogChan:
		if entry.Category != types.LogTool {
			t.Errorf("Category = %q, want %q", entry.Category, types.LogTool)
		}
		if entry.ToolPath != "/dev/fs" {
			t.Errorf("ToolPath = %q, want %q", entry.ToolPath, "/dev/fs")
		}
		if entry.Content != "Read src/main.go (2847 bytes)" {
			t.Errorf("Content mismatch")
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for log entry")
	}
}

// --- 10.2-UNIT-009: emitLog output category ---

func TestEmitLog_OutputCategory(t *testing.T) {
	k := newSimpleKernel(t)
	proc := NewProcess(0, "test", nil)
	_ = proc.Start()
	k.AddProcess(proc)

	k.emitLog(proc, 3, types.LogOutput, "fixed the race condition", "")

	select {
	case entry := <-proc.LogChan:
		if entry.Category != types.LogOutput {
			t.Errorf("Category = %q, want %q", entry.Category, types.LogOutput)
		}
		if entry.ToolPath != "" {
			t.Errorf("ToolPath should be empty for output category, got %q", entry.ToolPath)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for log entry")
	}
}

// --- 10.2-UNIT-010: emitLog sets Timestamp relative to process creation ---

func TestEmitLog_TimestampRelative(t *testing.T) {
	k := newSimpleKernel(t)
	proc := NewProcess(0, "test", nil)
	_ = proc.Start()
	k.AddProcess(proc)

	time.Sleep(10 * time.Millisecond)
	k.emitLog(proc, 1, types.LogThink, "content", "")

	select {
	case entry := <-proc.LogChan:
		if entry.Timestamp <= 0 {
			t.Errorf("Timestamp should be positive (relative to process creation), got %v", entry.Timestamp)
		}
		if entry.Timestamp > 5*time.Second {
			t.Errorf("Timestamp unreasonably large: %v", entry.Timestamp)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for log entry")
	}
}

// --- 10.2-UNIT-011: emitLog non-blocking when LogChan buffer full ---

func TestEmitLog_NonBlocking_BufferFull(t *testing.T) {
	k := newSimpleKernel(t)
	proc := NewProcess(0, "test", nil)
	_ = proc.Start()
	k.AddProcess(proc)

	for i := 0; i < 256; i++ {
		proc.LogChan <- types.LogEntry{Category: types.LogThink, Content: "fill"}
	}

	done := make(chan struct{})
	go func() {
		k.emitLog(proc, 1, types.LogThink, "this will be dropped", "")
		close(done)
	}()

	select {
	case <-done:
		// emitLog returned without blocking
	case <-time.After(time.Second):
		t.Fatal("emitLog blocked when LogChan buffer was full")
	}
}

// --- 10.2-UNIT-012: emitLog safe when LogChan is nil ---

func TestEmitLog_NilLogChan(t *testing.T) {
	k := newSimpleKernel(t)
	proc := NewProcess(0, "test", nil)
	_ = proc.Start()
	k.AddProcess(proc)

	proc.mu.Lock()
	proc.LogChan = nil
	proc.mu.Unlock()

	// Must not panic
	k.emitLog(proc, 1, types.LogThink, "content", "")
}

// --- 10.2-UNIT-013: GetLogChan returns channel for valid PID ---

func TestGetLogChan_ValidPID(t *testing.T) {
	k := newSimpleKernel(t)
	proc := NewProcess(0, "test", nil)
	_ = proc.Start()
	k.AddProcess(proc)

	ch, ok := k.GetLogChan(proc.PID)
	if !ok {
		t.Fatal("GetLogChan should return true for valid PID")
	}
	if ch == nil {
		t.Fatal("GetLogChan should return non-nil channel")
	}
}

// --- 10.2-UNIT-014: GetLogChan returns false for non-existent PID ---

func TestGetLogChan_InvalidPID(t *testing.T) {
	k := newSimpleKernel(t)

	_, ok := k.GetLogChan(999)
	if ok {
		t.Error("GetLogChan should return false for non-existent PID")
	}
}

// --- 10.2-UNIT-015: GetLogChan returns false after LogChan nil-out ---

func TestGetLogChan_NilAfterClose(t *testing.T) {
	k := newSimpleKernel(t)
	proc := NewProcess(0, "test", nil)
	_ = proc.Start()
	k.AddProcess(proc)

	proc.mu.Lock()
	ch := proc.LogChan
	proc.LogChan = nil
	proc.mu.Unlock()
	if ch != nil {
		close(ch)
	}

	_, ok := k.GetLogChan(proc.PID)
	if ok {
		t.Error("GetLogChan should return false after LogChan nil-out")
	}
}

// --- 10.2-UNIT-016: reapProcess closes LogChan ---

func TestReapProcess_ClosesLogChan(t *testing.T) {
	k := newSimpleKernel(t)
	proc := NewProcess(0, "test", nil)
	_ = proc.Start()
	k.AddProcess(proc)

	logChan := proc.LogChan

	_ = proc.Terminate(ExitStatus{Code: 0})
	k.reapProcess(proc)

	_, open := <-logChan
	if open {
		t.Error("LogChan should be closed after reap")
	}

	proc.mu.Lock()
	isNil := proc.LogChan == nil
	proc.mu.Unlock()
	if !isNil {
		t.Error("proc.LogChan should be nil after reap")
	}
}

// --- 10.2-UNIT-017: LogChan independent of DebugChan ---

func TestLogChan_IndependentOfDebugChan(t *testing.T) {
	k := newSimpleKernel(t)
	proc := NewProcess(0, "test", nil)
	_ = proc.Start()
	k.AddProcess(proc)

	k.emitLog(proc, 1, types.LogThink, "log entry", "")

	select {
	case entry := <-proc.LogChan:
		if entry.Category != types.LogThink {
			t.Errorf("unexpected LogChan category: %q", entry.Category)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout on LogChan")
	}

	select {
	case <-proc.DebugChan:
		t.Error("DebugChan should be empty — emitLog should not write to DebugChan")
	default:
		// expected: DebugChan is empty
	}
}
