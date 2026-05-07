// Tests for StraceToUnifiedEvent (Story 38-5 PR11 Step 4(c)).
//
// 行为契约覆盖（与 cmd/rnix/dashboard_debug_test.go::Test_straceToUnifiedEvent_*
// 等价 · 0-行为变更验证）：
//   1. 字段映射：sew → UnifiedEvent 7 字段（Type/Severity/Timestamp/PID/Summary/RawEvent/...）
//   2. 严重度分桶：sew.Error == "" → SevInfo · 非空 → SevError
//   3. 时间戳：time.UnixMilli(TimestampMs) 单位换算
//   4. RawEvent 副本：返回值不共享底层 sew 内存（避免后续修改污染）
//   5. Type 固定为 EventSyscall
//   6. Summary 非空（debug.FormatEvent 集成 · 不深入断言具体格式）

package event

import (
	"testing"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/ipc"
)

func TestStraceToUnifiedEvent_BasicMapping(t *testing.T) {
	sew := ipc.SyscallEventWire{
		TimestampMs: 1700_000_000_000, // 2023-11-14T22:13:20Z (任意 ms 值)
		PID:         types.PID(42),
		Syscall:     "CtxAlloc",
		Args:        map[string]any{"size": 64},
		Result:      "17",
		DurationMs:  0.001,
		TraceID:     "trace-1",
		SpanID:      "span-1",
	}

	got := StraceToUnifiedEvent(sew)

	if got.Type != EventSyscall {
		t.Errorf("Type: want %q, got %q", EventSyscall, got.Type)
	}
	if got.Severity != SevInfo {
		t.Errorf("Severity (no error): want %d, got %d", SevInfo, got.Severity)
	}
	if got.PID != types.PID(42) {
		t.Errorf("PID: want 42, got %d", got.PID)
	}
	if got.Timestamp.UnixMilli() != 1700_000_000_000 {
		t.Errorf("Timestamp: want 1700e9 ms, got %d", got.Timestamp.UnixMilli())
	}
	if got.Summary == "" {
		t.Error("Summary should be non-empty (debug.FormatEvent integration)")
	}
	if got.RawEvent == nil {
		t.Fatal("RawEvent should not be nil")
	}
	if got.RawEvent.PID != types.PID(42) {
		t.Errorf("RawEvent.PID: want 42, got %d", got.RawEvent.PID)
	}
}

func TestStraceToUnifiedEvent_ErrorSeverity(t *testing.T) {
	sew := ipc.SyscallEventWire{
		TimestampMs: 1700_000_000_000,
		PID:         types.PID(7),
		Syscall:     "CtxFree",
		Error:       "context not found",
	}

	got := StraceToUnifiedEvent(sew)

	if got.Severity != SevError {
		t.Errorf("Severity (with error): want %d (SevError), got %d", SevError, got.Severity)
	}
}

func TestStraceToUnifiedEvent_RawEventIsCopy(t *testing.T) {
	sew := ipc.SyscallEventWire{
		TimestampMs: 1700_000_000_000,
		PID:         types.PID(99),
		Syscall:     "Open",
		Args:        map[string]any{"path": "/dev/llm/claude"},
	}

	got := StraceToUnifiedEvent(sew)
	if got.RawEvent == nil {
		t.Fatal("RawEvent should not be nil")
	}

	// 修改原 sew 不应影响 got.RawEvent (拷贝语义)
	sew.PID = types.PID(0)
	sew.Syscall = "Close"
	if got.RawEvent.PID != types.PID(99) {
		t.Errorf("RawEvent.PID should be 99 (copy not shared), got %d", got.RawEvent.PID)
	}
	if got.RawEvent.Syscall != "Open" {
		t.Errorf("RawEvent.Syscall should be %q (copy not shared), got %q", "Open", got.RawEvent.Syscall)
	}
}

func TestStraceToUnifiedEvent_ZeroTimestamp(t *testing.T) {
	// 时间戳 0 不应 panic · UnixMilli(0) = epoch
	sew := ipc.SyscallEventWire{
		TimestampMs: 0,
		PID:         types.PID(1),
	}

	got := StraceToUnifiedEvent(sew)
	if got.Timestamp.UnixMilli() != 0 {
		t.Errorf("zero timestamp: want UnixMilli=0, got %d", got.Timestamp.UnixMilli())
	}
}

func TestStraceToUnifiedEvent_SyscallTypeFixed(t *testing.T) {
	// 多个不同 syscall 名应该都得到相同 Type=EventSyscall
	cases := []string{"CtxAlloc", "CtxFree", "Open", "Read", "Write", "Close", "Spawn"}
	for _, name := range cases {
		t.Run(name, func(t *testing.T) {
			sew := ipc.SyscallEventWire{Syscall: name, PID: types.PID(1)}
			got := StraceToUnifiedEvent(sew)
			if got.Type != EventSyscall {
				t.Errorf("syscall %q: Type want %q, got %q", name, EventSyscall, got.Type)
			}
		})
	}
}
