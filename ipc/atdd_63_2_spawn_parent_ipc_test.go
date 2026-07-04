package ipc

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/internal/config"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/kernel"
	"github.com/rnixai/rnix/vfs"
)

func setupSpawnParentIPCTest(t *testing.T) (string, *kernel.KernelImpl, string, *mockLLMFile) {
	t.Helper()

	completeResp := `{"action":"complete","summary":"done","content":"done"}`
	llmFile := &mockLLMFile{readData: []byte(completeResp)}
	devReg := vfs.NewDeviceRegistry()
	_ = devReg.Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return llmFile, nil
	})
	vfsInst := vfs.NewVFS(devReg)
	ctxMgr := rnixctx.NewManager()

	srv := NewServer(nil, nil, "0.1.0-test", "", "")
	kern := kernel.NewKernel(vfsInst, ctxMgr, srv.CallbackMux())
	srv.kern = kern

	dataDir, _ := kernel.TestSetupDataDir(t, kern)

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

	return sockPath, kern, dataDir, llmFile
}

func addRunningParentProcess(t *testing.T, kern *kernel.KernelImpl, depth int) *kernel.Process {
	t.Helper()

	parent := kernel.NewProcess(0, "atdd 63.2 parent", []string{"test"})
	parent.Depth = depth
	if err := parent.Start(); err != nil {
		t.Fatalf("parent Start: %v", err)
	}
	kern.AddProcess(parent)
	return parent
}

func spawnDetachedForParentTest(t *testing.T, sockPath string, req SpawnRequest) (types.PID, func()) {
	t.Helper()

	client, err := Dial(sockPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	pid, err := client.SpawnDetached(req)
	if err != nil {
		client.Close()
		t.Fatalf("SpawnDetached: %v", err)
	}
	return pid, func() { client.Close() }
}

// --- 63.2-INT-001: parent_pid 挂树数据正确 (AC1) ---

func TestATDD_63_2_INT_001_SpawnWithParentPID_LinksChildUnderParent(t *testing.T) {
	sockPath, kern, _, llmFile := setupSpawnParentIPCTest(t)
	parent := addRunningParentProcess(t, kern, 2)
	reached, release := llmFile.parkOnRead()
	defer close(release)

	childPID, closeClient := spawnDetachedForParentTest(t, sockPath, SpawnRequest{
		Intent:    "atdd 63.2 child",
		ParentPID: parent.PID,
	})
	defer closeClient()
	waitForReached(t, reached, 2*time.Second, "child to enter LLM Read")

	child, ok := kern.GetProcess(childPID)
	if !ok {
		t.Fatalf("child PID %d not found", childPID)
	}
	if child.PPID != parent.PID {
		t.Fatalf("child PPID = %d, want parent PID %d", child.PPID, parent.PID)
	}
	if child.ParentUUID != parent.UUID {
		t.Fatalf("child ParentUUID = %q, want %q", child.ParentUUID, parent.UUID)
	}
	if !slices.Contains(parent.GetChildren(), childPID) {
		t.Fatalf("parent children = %v, want child PID %d", parent.GetChildren(), childPID)
	}
}

// --- 63.2-INT-002: handler 层 depth 推导触发 MaxSpawnDepth (AC4) ---

func TestATDD_63_2_INT_002_ParentDepthExceededRejected(t *testing.T) {
	sockPath, kern, _, _ := setupSpawnParentIPCTest(t)
	parent := addRunningParentProcess(t, kern, kernel.MaxSpawnDepth)

	client, err := Dial(sockPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()

	_, err = client.SpawnDetached(SpawnRequest{Intent: "too deep", ParentPID: parent.PID})
	if err == nil {
		t.Fatal("expected spawn to be rejected when parent depth reaches MaxSpawnDepth")
	}
	if !strings.Contains(err.Error(), "spawn rejected: depth") {
		t.Fatalf("error = %v, want spawn rejected depth message", err)
	}
}

// --- 63.2-INT-003: stale parent 降级 root，不阻断 spawn (AC5) ---

func TestATDD_63_2_INT_003_StaleParentDegradesToRoot(t *testing.T) {
	sockPath, kern, _, llmFile := setupSpawnParentIPCTest(t)
	reached, release := llmFile.parkOnRead()
	defer close(release)

	childPID, closeClient := spawnDetachedForParentTest(t, sockPath, SpawnRequest{
		Intent:    "stale parent",
		ParentPID: 999999,
	})
	defer closeClient()
	waitForReached(t, reached, 2*time.Second, "child to enter LLM Read")

	child, ok := kern.GetProcess(childPID)
	if !ok {
		t.Fatalf("child PID %d not found", childPID)
	}
	if child.PPID != 0 {
		t.Fatalf("child PPID = %d, want root PPID 0 for stale parent", child.PPID)
	}
	if child.ParentUUID != "" {
		t.Fatalf("child ParentUUID = %q, want empty for stale parent", child.ParentUUID)
	}
}

// --- 63.2-INT-004: proc-info.json 持久化 ppid / parent_uuid (AC6) ---

func TestATDD_63_2_INT_004_ProcInfoPersistsParentLinkage(t *testing.T) {
	sockPath, kern, dataDir, _ := setupSpawnParentIPCTest(t)
	parent := addRunningParentProcess(t, kern, 1)

	client, err := Dial(sockPath)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer client.Close()

	var childUUID string
	pid, _, err := client.SpawnAndWatch(SpawnRequest{Intent: "persist parent", ParentPID: parent.PID}, func(ev StreamEvent) {
		if ev.Type != StreamProgress {
			return
		}
		var pp ProgressPayload
		if err := json.Unmarshal(ev.Payload, &pp); err == nil && pp.Event == "spawn" {
			childUUID = pp.UUID
		}
	})
	if err != nil {
		t.Fatalf("SpawnAndWatch: %v", err)
	}
	if childUUID == "" {
		t.Fatal("spawn progress did not include child UUID")
	}

	procInfoPath := filepath.Join(config.GlobalDataDir(dataDir), "steps", childUUID, "proc-info.json")
	var raw []byte
	deadline := time.Now().Add(2 * time.Second)
	for {
		raw, err = os.ReadFile(procInfoPath)
		if err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("proc-info.json not written for child PID %d at %s: %v", pid, procInfoPath, err)
		}
		time.Sleep(10 * time.Millisecond)
	}

	var disk struct {
		PPID       uint64 `json:"ppid"`
		ParentUUID string `json:"parent_uuid"`
	}
	if err := json.Unmarshal(raw, &disk); err != nil {
		t.Fatalf("unmarshal proc-info.json: %v", err)
	}
	if disk.PPID != uint64(parent.PID) {
		t.Fatalf("disk PPID = %d, want %d", disk.PPID, parent.PID)
	}
	if disk.ParentUUID != parent.UUID {
		t.Fatalf("disk ParentUUID = %q, want %q", disk.ParentUUID, parent.UUID)
	}
}
