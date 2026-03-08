package debug

import (
	"testing"
	"time"
)

// 14.1-BENCH-001: WriteEvent 单次 < 100us
func BenchmarkRecorderWriteEvent(b *testing.B) {
	baseDir := b.TempDir()
	rec, err := NewRecorder(baseDir, 1, "benchmark")
	if err != nil {
		b.Fatalf("NewRecorder failed: %v", err)
	}
	defer rec.Close()

	ev := RecordEvent{
		Timestamp: 100 * time.Millisecond,
		PID:       1,
		Type:      RecordSyscall,
		Syscall: &SyscallEventData{
			Syscall:  "Read",
			Args:     map[string]any{"fd": 3, "path": "/dev/llm/claude"},
			Result:   1024,
			Duration: 5 * time.Millisecond,
		},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := rec.WriteEvent(ev); err != nil {
			b.Fatalf("WriteEvent failed: %v", err)
		}
	}
}
