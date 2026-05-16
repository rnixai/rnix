package ipc

import (
	gocontext "context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/kernel"
	"github.com/rnixai/rnix/vfs"
)

// mockLLMFile is a minimal VFS file mock for resume IPC tests.
type mockLLMFile struct {
	mu       sync.Mutex
	readData []byte
}

func (f *mockLLMFile) Write(_ gocontext.Context, _ []byte) error { return nil }
func (f *mockLLMFile) Read(_ int) ([]byte, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.readData, nil
}
func (f *mockLLMFile) Close() error                    { return nil }
func (f *mockLLMFile) Stat() (vfs.FileStat, error)     { return vfs.FileStat{IsDevice: true, Name: "/dev/llm/claude"}, nil }
func (f *mockLLMFile) SupportsToolCalling() bool       { return true }

// =============================================================================
// ATDD 42.1: Resume 机制层 — IPC 集成测试
// =============================================================================

func setupResumeIPCTest(t *testing.T) (*Client, *kernel.KernelImpl, string) {
	t.Helper()

	completeResp := `{"action":"complete","summary":"done","content":"done"}`
	llmFile := &mockLLMFile{readData: []byte(completeResp)}
	devReg := vfs.NewDeviceRegistry()
	_ = devReg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return llmFile, nil
	})
	vfsInst := vfs.NewVFS(devReg)
	ctxMgr := rnixctx.NewManager()

	srv := NewServer(nil, nil, "0.1.0-test")
	kern := kernel.NewKernel(vfsInst, ctxMgr, srv.CallbackMux())
	srv.kern = kern

	baseDir := t.TempDir()
	kern.SetStepDataDir(baseDir)

	sockDir := t.TempDir()
	sockPath := filepath.Join(sockDir, "test.sock")
	if err := srv.ListenAndServe(sockPath); err != nil {
		t.Fatalf("ListenAndServe: %v", err)
	}
	t.Cleanup(func() {
		srv.Shutdown()
		srv.Wait()
		kern.Shutdown()
	})

	client, err := Dial(sockPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { client.Close() })

	return client, kern, baseDir
}

func writeIPCTestData(t *testing.T, baseDir, uuid string, lastStep int) {
	t.Helper()
	stepsDir := filepath.Join(baseDir, "data", "steps", uuid)
	if err := os.MkdirAll(stepsDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	var stepsContent []byte
	for i := 1; i <= lastStep; i++ {
		record := map[string]any{
			"step":     i,
			"action":   "tool_call",
			"messages": []map[string]string{{"role": "user", "content": fmt.Sprintf("step %d", i)}},
		}
		line, _ := json.Marshal(record)
		stepsContent = append(stepsContent, line...)
		stepsContent = append(stepsContent, '\n')
	}
	_ = os.WriteFile(filepath.Join(stepsDir, "steps.jsonl"), stepsContent, 0o600)
	meta, _ := json.Marshal(map[string]any{"system_prompt": "test", "tool_defs": []any{}})
	_ = os.WriteFile(filepath.Join(stepsDir, "process-meta.json"), meta, 0o600)
	info, _ := json.Marshal(map[string]any{
		"pid": 99, "uuid": uuid, "state": "dead", "intent": "ipc test",
		"provider": "claude", "model": "claude-4", "tokens_used": 1000,
	})
	_ = os.WriteFile(filepath.Join(stepsDir, "proc-info.json"), info, 0o600)
}

// --- 42.1-INT-001: IPC resume fork=false 走续跑路径 (AC#7) ---

func TestATDD_42_1_INT_001_IPC_Resume_NoFork(t *testing.T) {
	client, _, baseDir := setupResumeIPCTest(t)
	uuid := "ipc-nofork-0000-0000-000000000001"
	writeIPCTestData(t, baseDir, uuid, 5)

	resp, err := client.ResumeWithOpts(uuid, false)
	if err != nil {
		t.Fatalf("IPC Resume failed: %v", err)
	}
	if resp.UUID != uuid {
		t.Errorf("UUID = %q, want %q", resp.UUID, uuid)
	}
	if resp.PID == 0 {
		t.Error("PID should be non-zero")
	}
	if resp.ResumedFromStep != 6 {
		t.Errorf("ResumedFromStep = %d, want 6", resp.ResumedFromStep)
	}
}

// --- 42.1-INT-002: IPC resume fork=true 走分叉路径 (AC#7) ---

func TestATDD_42_1_INT_002_IPC_Resume_Fork(t *testing.T) {
	client, _, baseDir := setupResumeIPCTest(t)
	uuid := "ipc-fork-0000-0000-000000000001"
	writeIPCTestData(t, baseDir, uuid, 10)

	resp, err := client.ResumeWithOpts(uuid, true)
	if err != nil {
		t.Fatalf("IPC Resume (fork) failed: %v", err)
	}
	if resp.UUID == uuid {
		t.Errorf("Fork should produce new UUID, got original %q", uuid)
	}
	if resp.UUID == "" {
		t.Error("Fork UUID should not be empty")
	}
}

// --- 42.1-INT-003: 向后兼容（Client.Resume 无 Fork = fork=false）(AC#7) ---

func TestATDD_42_1_INT_003_IPC_Resume_BackwardCompat(t *testing.T) {
	client, _, baseDir := setupResumeIPCTest(t)
	uuid := "ipc-compat-0000-0000-000000000001"
	writeIPCTestData(t, baseDir, uuid, 3)

	resp, err := client.Resume(uuid)
	if err != nil {
		t.Fatalf("IPC Resume (compat) failed: %v", err)
	}
	if resp.UUID != uuid {
		t.Errorf("Default should keep UUID, got %q want %q", resp.UUID, uuid)
	}
}