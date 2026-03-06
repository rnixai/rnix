package debug

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/usecrux/crux/internal/types"
)

func TestEmitEvent_NilChannel(t *testing.T) {
	// EmitEvent with nil channel must not panic.
	event := types.SyscallEvent{Syscall: "Test"}
	EmitEvent(nil, event) // should be a no-op
}

func TestEmitEvent_Success(t *testing.T) {
	ch := make(chan types.SyscallEvent, 1)
	event := types.SyscallEvent{
		PID:     1,
		Syscall: "Open",
		Args:    map[string]any{"path": "/dev/null"},
	}

	EmitEvent(ch, event)

	select {
	case got := <-ch:
		if got.PID != 1 {
			t.Fatalf("expected PID 1, got %d", got.PID)
		}
		if got.Syscall != "Open" {
			t.Fatalf("expected Syscall 'Open', got %q", got.Syscall)
		}
		if got.Args["path"] != "/dev/null" {
			t.Fatalf("expected Args[path] '/dev/null', got %v", got.Args["path"])
		}
	default:
		t.Fatal("expected event in channel, got nothing")
	}
}

func TestEmitEvent_BufferFull(t *testing.T) {
	ch := make(chan types.SyscallEvent, 1)

	// Fill the buffer.
	ch <- types.SyscallEvent{Syscall: "First"}

	// This must not block.
	done := make(chan struct{})
	go func() {
		EmitEvent(ch, types.SyscallEvent{Syscall: "Second"})
		close(done)
	}()

	select {
	case <-done:
		// OK — non-blocking.
	case <-time.After(1 * time.Second):
		t.Fatal("EmitEvent blocked on full channel")
	}

	// Only the first event should be in the channel.
	got := <-ch
	if got.Syscall != "First" {
		t.Fatalf("expected 'First', got %q", got.Syscall)
	}
}

func TestNewEvent_Fields(t *testing.T) {
	pid := types.PID(42)
	createdAt := time.Now().Add(-100 * time.Millisecond) // simulate process created 100ms ago
	args := map[string]any{"fd": 3, "length": 1024}

	event := NewEvent(pid, createdAt, "Read", args)

	if event.PID != pid {
		t.Fatalf("expected PID %d, got %d", pid, event.PID)
	}
	if event.Syscall != "Read" {
		t.Fatalf("expected Syscall 'Read', got %q", event.Syscall)
	}
	if event.Args["fd"] != 3 {
		t.Fatalf("expected Args[fd] = 3, got %v", event.Args["fd"])
	}
	if event.Args["length"] != 1024 {
		t.Fatalf("expected Args[length] = 1024, got %v", event.Args["length"])
	}
	if event.Timestamp <= 0 {
		t.Fatalf("expected positive Timestamp, got %v", event.Timestamp)
	}
	// Timestamp should be approximately >= 100ms (since createdAt was 100ms ago).
	if event.Timestamp < 90*time.Millisecond {
		t.Fatalf("expected Timestamp >= 90ms, got %v", event.Timestamp)
	}
	// Result, Err, Duration should be zero values.
	if event.Result != nil {
		t.Fatalf("expected nil Result, got %v", event.Result)
	}
	if event.Err != nil {
		t.Fatalf("expected nil Err, got %v", event.Err)
	}
	if event.Duration != 0 {
		t.Fatalf("expected zero Duration, got %v", event.Duration)
	}
}

func TestCompleteEvent_FillsFields(t *testing.T) {
	event := types.SyscallEvent{
		PID:     1,
		Syscall: "Write",
	}

	duration := 5 * time.Millisecond
	testErr := fmt.Errorf("write failed")
	CompleteEvent(&event, 128, testErr, duration)

	if event.Result != 128 {
		t.Fatalf("expected Result 128, got %v", event.Result)
	}
	if event.Err != testErr {
		t.Fatalf("expected Err 'write failed', got %v", event.Err)
	}
	if event.Duration != duration {
		t.Fatalf("expected Duration %v, got %v", duration, event.Duration)
	}

	// Test with nil error (success case).
	event2 := types.SyscallEvent{Syscall: "Read"}
	CompleteEvent(&event2, 256, nil, 10*time.Millisecond)
	if event2.Err != nil {
		t.Fatalf("expected nil Err, got %v", event2.Err)
	}
	if event2.Result != 256 {
		t.Fatalf("expected Result 256, got %v", event2.Result)
	}
}

func TestEmitEvent_ConcurrentSafety(t *testing.T) {
	ch := make(chan types.SyscallEvent, 256)
	const goroutines = 100
	const eventsPerGoroutine = 10

	var wg sync.WaitGroup
	for g := range goroutines {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for i := range eventsPerGoroutine {
				EmitEvent(ch, types.SyscallEvent{
					PID:     types.PID(id),
					Syscall: fmt.Sprintf("Op%d", i),
				})
			}
		}(g)
	}
	wg.Wait()

	// Drain and count events.
	close(ch)
	count := 0
	for range ch {
		count++
	}

	// With 256 buffer and 1000 total events, some may be dropped.
	// But we should have received at least the buffer size worth.
	if count == 0 {
		t.Fatal("expected some events, got 0")
	}
	if count > goroutines*eventsPerGoroutine {
		t.Fatalf("impossible event count: %d", count)
	}
	t.Logf("received %d/%d events (buffer=256)", count, goroutines*eventsPerGoroutine)
}
