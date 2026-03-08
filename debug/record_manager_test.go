package debug

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/rnixai/rnix/internal/types"
)

// 14.1-MGR-001: StartRecording 创建 Recorder 并返回 recordID
func TestRecordManager_StartRecording(t *testing.T) {
	baseDir := t.TempDir()
	mgr := NewRecordManager(baseDir)

	recordID, err := mgr.StartRecording(types.PID(42), "test intent")
	if err != nil {
		t.Fatalf("StartRecording failed: %v", err)
	}

	if recordID == "" {
		t.Fatal("expected non-empty recordID")
	}

	// Verify recording directory was created
	entries, err := os.ReadDir(baseDir)
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected 1 directory, got %d", len(entries))
	}

	mgr.CloseAll()
}

// 14.1-MGR-002: StartRecording 同一 PID 重复录制返回错误
func TestRecordManager_StartRecording_DuplicatePID(t *testing.T) {
	baseDir := t.TempDir()
	mgr := NewRecordManager(baseDir)
	defer mgr.CloseAll()

	pid := types.PID(42)

	_, err := mgr.StartRecording(pid, "first")
	if err != nil {
		t.Fatalf("first StartRecording failed: %v", err)
	}

	_, err = mgr.StartRecording(pid, "second")
	if err == nil {
		t.Fatal("expected error for duplicate PID recording, got nil")
	}
}

// 14.1-MGR-003: StopRecording 停止并移除活跃录制
func TestRecordManager_StopRecording(t *testing.T) {
	baseDir := t.TempDir()
	mgr := NewRecordManager(baseDir)

	pid := types.PID(10)
	_, err := mgr.StartRecording(pid, "stop test")
	if err != nil {
		t.Fatalf("StartRecording failed: %v", err)
	}

	if !mgr.IsRecording(pid) {
		t.Fatal("expected IsRecording=true before stop")
	}

	if err := mgr.StopRecording(pid); err != nil {
		t.Fatalf("StopRecording failed: %v", err)
	}

	if mgr.IsRecording(pid) {
		t.Fatal("expected IsRecording=false after stop")
	}
}

// 14.1-MGR-004: StopRecording 不存在的 PID 返回错误
func TestRecordManager_StopRecording_NotFound(t *testing.T) {
	baseDir := t.TempDir()
	mgr := NewRecordManager(baseDir)

	err := mgr.StopRecording(types.PID(999))
	if err == nil {
		t.Fatal("expected error for non-existent PID, got nil")
	}
}

// 14.1-MGR-005: RecordEvent 写入活跃录制
func TestRecordManager_RecordEvent(t *testing.T) {
	baseDir := t.TempDir()
	mgr := NewRecordManager(baseDir)

	pid := types.PID(42)
	_, err := mgr.StartRecording(pid, "event test")
	if err != nil {
		t.Fatalf("StartRecording failed: %v", err)
	}

	ev := RecordEvent{
		PID:  pid,
		Type: RecordSyscall,
		Syscall: &SyscallEventData{
			Syscall: "Open",
			Args: map[string]any{"path": "/dev/null"},
		},
	}

	if err := mgr.RecordEvent(pid, ev); err != nil {
		t.Fatalf("RecordEvent failed: %v", err)
	}

	mgr.CloseAll()

	// Verify event was written by checking events.jsonl
	entries, _ := os.ReadDir(baseDir)
	if len(entries) == 0 {
		t.Fatal("no recording directories found")
	}
	eventsPath := filepath.Join(baseDir, entries[0].Name(), "events.jsonl")
	data, err := os.ReadFile(eventsPath)
	if err != nil {
		t.Fatalf("read events.jsonl failed: %v", err)
	}
	if len(data) == 0 {
		t.Fatal("events.jsonl is empty, expected at least one event")
	}
}

// 14.1-MGR-006: RecordEvent 无活跃录制时静默跳过
func TestRecordManager_RecordEvent_NoActiveRecording(t *testing.T) {
	baseDir := t.TempDir()
	mgr := NewRecordManager(baseDir)

	ev := RecordEvent{
		PID:  types.PID(999),
		Type: RecordSyscall,
		Syscall: &SyscallEventData{
			Syscall: "Ghost",
		},
	}

	// Should NOT return error for non-recording PID
	err := mgr.RecordEvent(types.PID(999), ev)
	if err != nil {
		t.Fatalf("expected nil error for non-recording PID, got %v", err)
	}
}

// 14.1-MGR-007: IsRecording 返回正确状态
func TestRecordManager_IsRecording(t *testing.T) {
	baseDir := t.TempDir()
	mgr := NewRecordManager(baseDir)
	defer mgr.CloseAll()

	pid := types.PID(42)

	// Before recording
	if mgr.IsRecording(pid) {
		t.Fatal("expected IsRecording=false before StartRecording")
	}

	// Start recording
	mgr.StartRecording(pid, "test")
	if !mgr.IsRecording(pid) {
		t.Fatal("expected IsRecording=true after StartRecording")
	}

	// Stop recording
	mgr.StopRecording(pid)
	if mgr.IsRecording(pid) {
		t.Fatal("expected IsRecording=false after StopRecording")
	}
}

// 14.1-MGR-008: CloseAll 关闭所有活跃录制
func TestRecordManager_CloseAll(t *testing.T) {
	baseDir := t.TempDir()
	mgr := NewRecordManager(baseDir)

	// Start multiple recordings
	for i := 1; i <= 3; i++ {
		_, err := mgr.StartRecording(types.PID(i), "multi test")
		if err != nil {
			t.Fatalf("StartRecording PID %d failed: %v", i, err)
		}
	}

	// All should be recording
	for i := 1; i <= 3; i++ {
		if !mgr.IsRecording(types.PID(i)) {
			t.Fatalf("expected PID %d to be recording", i)
		}
	}

	mgr.CloseAll()

	// All should be stopped
	for i := 1; i <= 3; i++ {
		if mgr.IsRecording(types.PID(i)) {
			t.Fatalf("expected PID %d to NOT be recording after CloseAll", i)
		}
	}

	// Verify metadata.json for each shows completed status
	entries, _ := os.ReadDir(baseDir)
	for _, entry := range entries {
		metaPath := filepath.Join(baseDir, entry.Name(), "metadata.json")
		data, err := os.ReadFile(metaPath)
		if err != nil {
			t.Fatalf("read metadata.json for %s failed: %v", entry.Name(), err)
		}
		var meta RecordMetadata
		json.Unmarshal(data, &meta)
		if meta.Status != RecordStatusCompleted {
			t.Fatalf("expected status=%q for %s, got %q", RecordStatusCompleted, entry.Name(), meta.Status)
		}
	}
}

// 14.1-MGR-009: ListRecords 扫描 baseDir 返回 metadata 列表
func TestRecordManager_ListRecords(t *testing.T) {
	baseDir := t.TempDir()
	mgr := NewRecordManager(baseDir)

	// Create 2 recordings, close them
	for i := 1; i <= 2; i++ {
		_, err := mgr.StartRecording(types.PID(i), "list test")
		if err != nil {
			t.Fatalf("StartRecording PID %d failed: %v", i, err)
		}
	}
	mgr.CloseAll()

	// List records
	records, err := mgr.ListRecords()
	if err != nil {
		t.Fatalf("ListRecords failed: %v", err)
	}

	if len(records) != 2 {
		t.Fatalf("expected 2 records, got %d", len(records))
	}

	// Each record should have valid metadata
	for _, rec := range records {
		if rec.RecordID == "" {
			t.Fatal("expected non-empty RecordID")
		}
		if rec.Status != RecordStatusCompleted {
			t.Fatalf("expected Status=%q, got %q", RecordStatusCompleted, rec.Status)
		}
	}
}
