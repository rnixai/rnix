package debug

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
)

// 14.1-REC-001: NewRecorder 创建目录和 events.jsonl 文件
func TestNewRecorder_CreatesDirectoryAndFile(t *testing.T) {
	baseDir := t.TempDir()
	pid := types.PID(42)

	rec, err := NewRecorder(baseDir, pid, "test intent")
	if err != nil {
		t.Fatalf("NewRecorder failed: %v", err)
	}
	defer rec.Close()

	// Verify directory was created under baseDir
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 directory under baseDir, got %d", len(entries))
	}

	recDir := filepath.Join(baseDir, entries[0].Name())

	// Verify events.jsonl exists
	eventsPath := filepath.Join(recDir, "events.jsonl")
	if _, err := os.Stat(eventsPath); os.IsNotExist(err) {
		t.Fatalf("events.jsonl not created at %s", eventsPath)
	}

	// Verify metadata.json exists
	metaPath := filepath.Join(recDir, "metadata.json")
	if _, err := os.Stat(metaPath); os.IsNotExist(err) {
		t.Fatalf("metadata.json not created at %s", metaPath)
	}
}

// 14.1-REC-002: Recorder.WriteEvent 写入 JSONL 格式
func TestRecorder_WriteEvent_JSONL(t *testing.T) {
	baseDir := t.TempDir()
	rec, err := NewRecorder(baseDir, types.PID(1), "test")
	if err != nil {
		t.Fatalf("NewRecorder failed: %v", err)
	}

	// Write 3 events
	for i := range 3 {
		ev := RecordEvent{
			SeqNum:    uint64(i),
			Timestamp: time.Duration(i) * time.Second,
			PID:       1,
			Type:      RecordSyscall,
			Syscall: &SyscallEventData{
				Syscall:  "Read",
				Args:     map[string]any{"fd": 3},
				Result:   1024,
				Duration: time.Millisecond,
			},
		}
		if err := rec.WriteEvent(ev); err != nil {
			t.Fatalf("WriteEvent %d failed: %v", i, err)
		}
	}

	if err := rec.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Read events.jsonl and verify each line is valid JSON
	entries, _ := os.ReadDir(baseDir)
	eventsPath := filepath.Join(baseDir, entries[0].Name(), "events.jsonl")

	f, err := os.Open(eventsPath)
	if err != nil {
		t.Fatalf("open events.jsonl failed: %v", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	lineCount := 0
	for scanner.Scan() {
		var parsed RecordEvent
		if err := json.Unmarshal(scanner.Bytes(), &parsed); err != nil {
			t.Fatalf("line %d is not valid JSON: %v\nline: %s", lineCount, err, scanner.Text())
		}
		lineCount++
	}
	if lineCount != 3 {
		t.Fatalf("expected 3 lines in events.jsonl, got %d", lineCount)
	}
}

// 14.1-REC-003: Recorder.WriteEvent 递增 SeqNum
func TestRecorder_WriteEvent_IncrementsSeqNum(t *testing.T) {
	baseDir := t.TempDir()
	rec, err := NewRecorder(baseDir, types.PID(1), "test")
	if err != nil {
		t.Fatalf("NewRecorder failed: %v", err)
	}

	// Write events without setting SeqNum (recorder should assign)
	for i := range 5 {
		ev := RecordEvent{
			PID:  1,
			Type: RecordSyscall,
			Syscall: &SyscallEventData{
				Syscall: "Test",
			},
		}
		if err := rec.WriteEvent(ev); err != nil {
			t.Fatalf("WriteEvent %d failed: %v", i, err)
		}
	}

	if err := rec.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Read back and verify sequential SeqNums
	entries, _ := os.ReadDir(baseDir)
	eventsPath := filepath.Join(baseDir, entries[0].Name(), "events.jsonl")

	f, _ := os.Open(eventsPath)
	defer f.Close()

	scanner := bufio.NewScanner(f)
	expectedSeq := uint64(1)
	for scanner.Scan() {
		var parsed RecordEvent
		json.Unmarshal(scanner.Bytes(), &parsed)
		if parsed.SeqNum != expectedSeq {
			t.Fatalf("expected SeqNum=%d, got %d", expectedSeq, parsed.SeqNum)
		}
		expectedSeq++
	}
}

// 14.1-REC-004: Recorder.Close 更新 metadata.json status 为 "completed"
func TestRecorder_Close_UpdatesMetadata(t *testing.T) {
	baseDir := t.TempDir()
	rec, err := NewRecorder(baseDir, types.PID(10), "close test")
	if err != nil {
		t.Fatalf("NewRecorder failed: %v", err)
	}

	// Write one event
	ev := RecordEvent{
		PID:  10,
		Type: RecordSyscall,
		Syscall: &SyscallEventData{
			Syscall: "Open",
		},
	}
	rec.WriteEvent(ev)

	if err := rec.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Read metadata.json and verify
	entries, _ := os.ReadDir(baseDir)
	metaPath := filepath.Join(baseDir, entries[0].Name(), "metadata.json")

	data, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("read metadata.json failed: %v", err)
	}

	var meta RecordMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatalf("unmarshal metadata failed: %v", err)
	}

	if meta.Status != RecordStatusCompleted {
		t.Fatalf("expected status=%q, got %q", RecordStatusCompleted, meta.Status)
	}
	if meta.EventCount != 1 {
		t.Fatalf("expected EventCount=1, got %d", meta.EventCount)
	}
	if meta.EndTime.IsZero() {
		t.Fatal("expected EndTime to be set after Close")
	}
}

// 14.1-REC-005: Recorder.Stop 更新 metadata.json status 为 "stopped"
func TestRecorder_Stop_UpdatesMetadata(t *testing.T) {
	baseDir := t.TempDir()
	rec, err := NewRecorder(baseDir, types.PID(20), "stop test")
	if err != nil {
		t.Fatalf("NewRecorder failed: %v", err)
	}

	// Write events
	for range 3 {
		rec.WriteEvent(RecordEvent{
			PID:  20,
			Type: RecordSyscall,
			Syscall: &SyscallEventData{
				Syscall: "Test",
			},
		})
	}

	if err := rec.Stop(); err != nil {
		t.Fatalf("Stop failed: %v", err)
	}

	// Read metadata
	entries, _ := os.ReadDir(baseDir)
	metaPath := filepath.Join(baseDir, entries[0].Name(), "metadata.json")
	data, _ := os.ReadFile(metaPath)

	var meta RecordMetadata
	json.Unmarshal(data, &meta)

	if meta.Status != RecordStatusStopped {
		t.Fatalf("expected status=%q, got %q", RecordStatusStopped, meta.Status)
	}
	if meta.EventCount != 3 {
		t.Fatalf("expected EventCount=3, got %d", meta.EventCount)
	}
}

// 14.1-REC-006: NewRecorder 写入 metadata.json status 为 "recording"
func TestNewRecorder_InitialMetadata(t *testing.T) {
	baseDir := t.TempDir()
	rec, err := NewRecorder(baseDir, types.PID(5), "init test")
	if err != nil {
		t.Fatalf("NewRecorder failed: %v", err)
	}
	defer rec.Close()

	// Read metadata.json immediately after creation
	entries, _ := os.ReadDir(baseDir)
	metaPath := filepath.Join(baseDir, entries[0].Name(), "metadata.json")
	data, err := os.ReadFile(metaPath)
	if err != nil {
		t.Fatalf("read metadata.json failed: %v", err)
	}

	var meta RecordMetadata
	if err := json.Unmarshal(data, &meta); err != nil {
		t.Fatalf("unmarshal metadata failed: %v", err)
	}

	if meta.Status != RecordStatusRecording {
		t.Fatalf("expected initial status=%q, got %q", RecordStatusRecording, meta.Status)
	}
	if meta.PID != 5 {
		t.Fatalf("expected PID=5, got %d", meta.PID)
	}
	if meta.Intent != "init test" {
		t.Fatalf("expected Intent='init test', got %q", meta.Intent)
	}
	if meta.EventCount != 0 {
		t.Fatalf("expected EventCount=0, got %d", meta.EventCount)
	}
	if meta.StartTime.IsZero() {
		t.Fatal("expected StartTime to be set")
	}
}

// 14.1-REC-007: Recorder 并发 WriteEvent 安全性
func TestRecorder_ConcurrentWriteEvent(t *testing.T) {
	baseDir := t.TempDir()
	rec, err := NewRecorder(baseDir, types.PID(99), "concurrency test")
	if err != nil {
		t.Fatalf("NewRecorder failed: %v", err)
	}

	const goroutines = 10
	const eventsPerGoroutine = 50

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for range goroutines {
		go func() {
			defer wg.Done()
			for range eventsPerGoroutine {
				ev := RecordEvent{
					PID:  99,
					Type: RecordSyscall,
					Syscall: &SyscallEventData{
						Syscall: "ConcurrentTest",
					},
				}
				if err := rec.WriteEvent(ev); err != nil {
					t.Errorf("WriteEvent failed: %v", err)
				}
			}
		}()
	}

	wg.Wait()

	if err := rec.Close(); err != nil {
		t.Fatalf("Close failed: %v", err)
	}

	// Verify all events were written (total = goroutines * eventsPerGoroutine)
	entries, _ := os.ReadDir(baseDir)
	eventsPath := filepath.Join(baseDir, entries[0].Name(), "events.jsonl")
	f, _ := os.Open(eventsPath)
	defer f.Close()

	scanner := bufio.NewScanner(f)
	lineCount := 0
	for scanner.Scan() {
		lineCount++
	}

	expected := goroutines * eventsPerGoroutine
	if lineCount != expected {
		t.Fatalf("expected %d lines, got %d", expected, lineCount)
	}
}
