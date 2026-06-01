package kernel

import (
	gocontext "context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
)

// TestIPCPersist_WriteAndLoad verifies messages are persisted to disk and can be reloaded.
func TestIPCPersist_WriteAndLoad(t *testing.T) {
	k := newSimpleKernel(t)
	tmpDir := t.TempDir()
	k.SetStepDataDir(tmpDir)

	proc := newIPCTestProcess(t, k)
	proc.UUID = "test-persist-uuid"
	proc.IPCPersist = true

	queue, ok := k.msgQueues.Load(proc.PID)
	if !ok {
		t.Fatal("message queue not found")
	}

	if err := enablePersistence(queue, tmpDir, proc.UUID); err != nil {
		t.Fatalf("enablePersistence failed: %v", err)
	}

	// Send messages to trigger persist via enqueue
	sender := newIPCTestProcess(t, k)
	for i := range 3 {
		if err := k.Send(sender.PID, proc.PID, []byte("msg-"+string(rune('A'+i)))); err != nil {
			t.Fatalf("Send %d failed: %v", i, err)
		}
	}

	// Verify persist file exists
	persistPath := filepath.Join(tmpDir, "ipc", proc.UUID, "inbox.jsonl")
	if _, err := os.Stat(persistPath); err != nil {
		t.Fatalf("persist file not created: %v", err)
	}

	// Load persisted messages
	msgs, err := loadPersistedMessages(tmpDir, proc.UUID)
	if err != nil {
		t.Fatalf("loadPersistedMessages failed: %v", err)
	}
	if len(msgs) != 3 {
		t.Fatalf("loaded %d messages, want 3", len(msgs))
	}
	if string(msgs[0].Data) != "msg-A" {
		t.Errorf("msg[0].Data = %q, want %q", msgs[0].Data, "msg-A")
	}
	if msgs[0].FromPID != sender.PID {
		t.Errorf("msg[0].FromPID = %d, want %d", msgs[0].FromPID, sender.PID)
	}
}

// TestIPCPersist_Cleanup verifies removePersistedMessages deletes the directory.
func TestIPCPersist_Cleanup(t *testing.T) {
	tmpDir := t.TempDir()
	uuid := "cleanup-test-uuid"

	// Create a persist dir with a file
	dir := filepath.Join(tmpDir, "ipc", uuid)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "inbox.jsonl"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := removePersistedMessages(tmpDir, uuid); err != nil {
		t.Fatalf("removePersistedMessages failed: %v", err)
	}

	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Error("persist directory should be removed")
	}
}

// TestIPCPersist_LoadNonExistent verifies loading from missing dir returns nil.
func TestIPCPersist_LoadNonExistent(t *testing.T) {
	msgs, err := loadPersistedMessages(t.TempDir(), "no-such-uuid")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if msgs != nil {
		t.Errorf("expected nil, got %d messages", len(msgs))
	}
}

// TestIPCPersist_TransientNoFile verifies non-persistent queues don't write files.
func TestIPCPersist_TransientNoFile(t *testing.T) {
	k := newSimpleKernel(t)
	tmpDir := t.TempDir()
	k.SetStepDataDir(tmpDir)

	proc := newIPCTestProcess(t, k)
	// IPCPersist defaults to false — no enablePersistence call

	sender := newIPCTestProcess(t, k)
	if err := k.Send(sender.PID, proc.PID, []byte("transient")); err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	// No IPC persist directory should exist
	ipcDir := filepath.Join(tmpDir, "ipc")
	if _, err := os.Stat(ipcDir); err == nil {
		t.Error("IPC persist directory should not exist for transient process")
	}
}

// TestIPCPersist_CloseFlushes verifies that closing the queue closes the persist file.
func TestIPCPersist_CloseFlushes(t *testing.T) {
	k := newSimpleKernel(t)
	tmpDir := t.TempDir()

	proc := newIPCTestProcess(t, k)
	proc.UUID = "close-test-uuid"

	queue, _ := k.msgQueues.Load(proc.PID)
	if err := enablePersistence(queue, tmpDir, proc.UUID); err != nil {
		t.Fatalf("enablePersistence failed: %v", err)
	}

	// Enqueue a message then close
	_ = k.Send(newIPCTestProcess(t, k).PID, proc.PID, []byte("before-close"))
	queue.close()

	// persistFile should be nil after close
	queue.mu.Lock()
	if queue.persistFile != nil {
		t.Error("persistFile should be nil after close")
	}
	queue.mu.Unlock()

	// Persisted data should still be on disk
	msgs, err := loadPersistedMessages(tmpDir, proc.UUID)
	if err != nil {
		t.Fatalf("loadPersistedMessages after close: %v", err)
	}
	if len(msgs) != 1 {
		t.Errorf("expected 1 persisted message, got %d", len(msgs))
	}
}

// TestIPCPersist_Recovery verifies the supervisor recovery flow: load old messages,
// inject into new queue, remove old persist files.
func TestIPCPersist_Recovery(t *testing.T) {
	tmpDir := t.TempDir()
	oldUUID := "old-process-uuid"

	// Simulate persisted messages from a previous incarnation
	dir := filepath.Join(tmpDir, "ipc", oldUUID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	ndjson := `{"from_pid":1,"to_pid":2,"seq":1,"data":"aGVsbG8=","created_at":"2024-01-01T00:00:00Z"}` + "\n" +
		`{"from_pid":1,"to_pid":2,"seq":2,"data":"d29ybGQ=","created_at":"2024-01-01T00:00:01Z"}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, "inbox.jsonl"), []byte(ndjson), 0o644); err != nil {
		t.Fatal(err)
	}

	// Load messages
	msgs, err := loadPersistedMessages(tmpDir, oldUUID)
	if err != nil {
		t.Fatalf("loadPersistedMessages failed: %v", err)
	}
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages, got %d", len(msgs))
	}
	if string(msgs[0].Data) != "hello" {
		t.Errorf("msg[0].Data = %q, want %q", msgs[0].Data, "hello")
	}
	if string(msgs[1].Data) != "world" {
		t.Errorf("msg[1].Data = %q, want %q", msgs[1].Data, "world")
	}

	// Inject into a new queue
	queue := newMessageQueue()
	for _, m := range msgs {
		_ = queue.enqueue(m)
	}

	// Verify queue has the recovered messages
	ctx, cancel := gocontext.WithTimeout(gocontext.Background(), time.Second)
	defer cancel()
	m1, err := queue.dequeue(ctx)
	if err != nil {
		t.Fatalf("dequeue failed: %v", err)
	}
	if string(m1.Data) != "hello" {
		t.Errorf("recovered msg[0] = %q, want %q", m1.Data, "hello")
	}

	// Clean up old files
	if err := removePersistedMessages(tmpDir, oldUUID); err != nil {
		t.Fatalf("removePersistedMessages failed: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Error("old persist dir should be removed after recovery")
	}
}

// TestIPCPersist_ReapCleanup verifies that reapProcess removes persist files on normal exit.
func TestIPCPersist_ReapCleanup(t *testing.T) {
	k := newSimpleKernel(t)
	_, projBase := TestSetupDataDir(t, k)

	proc := newIPCTestProcess(t, k)
	proc.UUID = "reap-cleanup-uuid"
	proc.IPCPersist = true
	TestSetProjectConfig(proc)

	queue, _ := k.msgQueues.Load(proc.PID)
	if err := enablePersistence(queue, projBase, proc.UUID); err != nil {
		t.Fatalf("enablePersistence failed: %v", err)
	}

	// Enqueue a message
	sender := newIPCTestProcess(t, k)
	_ = k.Send(sender.PID, proc.PID, []byte("persist-me"))

	// Simulate normal exit
	proc.mu.Lock()
	proc.Exit = &ExitStatus{Code: 0, Reason: "normal"}
	proc.State = types.StateZombie
	proc.mu.Unlock()

	// Close done channel to satisfy proc.Done
	proc.Cancel()
	close(proc.Done)

	// Reap the process
	k.reapProcess(proc)

	// Persist directory should be removed (normal exit)
	persistDir := filepath.Join(projBase, "ipc", proc.UUID)
	if _, err := os.Stat(persistDir); !os.IsNotExist(err) {
		t.Error("persist dir should be removed on normal exit")
	}
}
