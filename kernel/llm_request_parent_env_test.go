package kernel

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/config"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

func TestReasonStep_LLMRequestIncludesCallerProcessInfo(t *testing.T) {
	k := newSimpleKernel(t)
	dataDir, _ := TestSetupDataDir(t, k)

	var capturedReq llmRequest
	captureLLM := &capturingLLMFile{
		inner:       &mockLLMFile{readData: []byte(`{"action":"complete","summary":"done","content":"done"}`)},
		capturedReq: &capturedReq,
	}
	if err := k.vfs.DeviceRegistry().Register("/dev/llm/claude", func(_ string, _ vfs.OpenFlag, _ string) (vfs.VFSFile, error) {
		return captureLLM, nil
	}); err != nil {
		t.Fatalf("register llm: %v", err)
	}

	pid, err := k.Spawn("parent env bridge", nil, SpawnOpts{Depth: 2})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	proc, ok := k.GetProcess(pid)
	if !ok {
		t.Fatalf("spawned process %d not found", pid)
	}

	select {
	case <-proc.Done:
	case <-time.After(2 * time.Second):
		t.Fatal("process did not complete")
	}

	if capturedReq.CallerPID != uint64(pid) {
		t.Fatalf("CallerPID = %d, want %d", capturedReq.CallerPID, pid)
	}
	if capturedReq.CallerDepth != 2 {
		t.Fatalf("CallerDepth = %d, want 2", capturedReq.CallerDepth)
	}
	if proc.GetState() != types.StateZombie {
		t.Fatalf("state = %s, want zombie", proc.GetState())
	}

	procInfoPath := filepath.Join(config.GlobalDataDir(dataDir), "steps", proc.UUID, "proc-info.json")
	raw, err := os.ReadFile(procInfoPath)
	if err != nil {
		t.Fatalf("read terminal proc-info.json: %v", err)
	}
	var disk struct {
		State  string `json:"state"`
		Result string `json:"result"`
	}
	if err := json.Unmarshal(raw, &disk); err != nil {
		t.Fatalf("unmarshal proc-info.json: %v", err)
	}
	if disk.State != "zombie" {
		t.Fatalf("disk state = %q, want zombie", disk.State)
	}
	if disk.Result != "done" {
		t.Fatalf("disk result = %q, want done", disk.Result)
	}
}
