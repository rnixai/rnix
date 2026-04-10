package kernel

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// ============================================================
// ATDD — Story 34-7: Orchestration metadata persistence
// ============================================================

func TestProcInfoDisk_ComposeRoundTrip(t *testing.T) {
	info := vfs.ProcInfo{
		PID:         1,
		UUID:        "test-compose-uuid",
		State:       types.StateDead,
		Intent:      "compose task",
		CreatedAt:   time.Now(),
		DeadAt:      time.Now().Add(5 * time.Second),
		ComposeNode: "summarizer",
		ComposeDeps: []string{"researcher", "analyst"},
	}

	disk := procInfoToDisk(info)
	if disk.ComposeNode != "summarizer" {
		t.Errorf("disk ComposeNode = %q, want 'summarizer'", disk.ComposeNode)
	}
	if len(disk.ComposeDeps) != 2 {
		t.Errorf("disk ComposeDeps len = %d, want 2", len(disk.ComposeDeps))
	}

	roundTripped := procInfoFromDisk(disk)
	if roundTripped.ComposeNode != "summarizer" {
		t.Errorf("roundtrip ComposeNode = %q, want 'summarizer'", roundTripped.ComposeNode)
	}
	if len(roundTripped.ComposeDeps) != 2 || roundTripped.ComposeDeps[1] != "analyst" {
		t.Errorf("roundtrip ComposeDeps = %v, want [researcher, analyst]", roundTripped.ComposeDeps)
	}
}

func TestProcInfoDisk_PipelineRoundTrip(t *testing.T) {
	info := vfs.ProcInfo{
		PID:           2,
		UUID:          "test-pipeline-uuid",
		State:         types.StateDead,
		Intent:        "pipeline stage",
		CreatedAt:     time.Now(),
		DeadAt:        time.Now().Add(3 * time.Second),
		PipelineIndex: 2,
		PipelineTotal: 5,
	}

	disk := procInfoToDisk(info)
	if disk.PipelineIndex != 2 {
		t.Errorf("disk PipelineIndex = %d, want 2", disk.PipelineIndex)
	}
	if disk.PipelineTotal != 5 {
		t.Errorf("disk PipelineTotal = %d, want 5", disk.PipelineTotal)
	}

	roundTripped := procInfoFromDisk(disk)
	if roundTripped.PipelineIndex != 2 || roundTripped.PipelineTotal != 5 {
		t.Errorf("roundtrip Pipeline = %d/%d, want 2/5", roundTripped.PipelineIndex, roundTripped.PipelineTotal)
	}
}

func TestProcInfoDisk_NoOrchestration(t *testing.T) {
	info := vfs.ProcInfo{
		PID:       3,
		UUID:      "test-plain-uuid",
		State:     types.StateDead,
		Intent:    "plain task",
		CreatedAt: time.Now(),
	}

	disk := procInfoToDisk(info)
	data, err := json.Marshal(disk)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	jsonStr := string(data)
	// With omitempty, compose_node/pipeline_total should not appear
	if containsStr(jsonStr, `"compose_node"`) {
		t.Error("empty compose_node should be omitted from disk JSON")
	}
	if containsStr(jsonStr, `"pipeline_total"`) {
		t.Error("zero pipeline_total should be omitted from disk JSON")
	}
}

func TestSaveProcInfo_WithOrchestration(t *testing.T) {
	dir := t.TempDir()

	info := vfs.ProcInfo{
		PID:           1,
		UUID:          "save-orch-test",
		State:         types.StateDead,
		Intent:        "compose save test",
		CreatedAt:     time.Now(),
		ComposeNode:   "worker",
		ComposeDeps:   []string{"input"},
		PipelineIndex: 0,
		PipelineTotal: 0,
	}

	if err := SaveProcInfo(dir, info); err != nil {
		t.Fatalf("SaveProcInfo: %v", err)
	}

	path := filepath.Join(dir, "data", "steps", info.UUID, procInfoFilename)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	var disk procInfoDisk
	if err := json.Unmarshal(data, &disk); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if disk.ComposeNode != "worker" {
		t.Errorf("saved ComposeNode = %q, want 'worker'", disk.ComposeNode)
	}
	if len(disk.ComposeDeps) != 1 || disk.ComposeDeps[0] != "input" {
		t.Errorf("saved ComposeDeps = %v, want [input]", disk.ComposeDeps)
	}
}

func containsStr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
