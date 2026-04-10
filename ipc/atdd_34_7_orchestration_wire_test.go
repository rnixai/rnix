package ipc

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// ============================================================
// ATDD — Story 34-7: Orchestration metadata wire roundtrip
// ============================================================

func TestProcInfoWire_ComposeRoundTrip(t *testing.T) {
	info := vfs.ProcInfo{
		PID:         1,
		UUID:        "test-uuid-001",
		State:       types.StateRunning,
		Intent:      "test",
		CreatedAt:   time.Now(),
		ComposeNode: "summarizer",
		ComposeDeps: []string{"researcher", "analyst"},
	}

	wire := ProcInfoToWire(info)
	if wire.ComposeNode != "summarizer" {
		t.Errorf("ComposeNode = %q, want 'summarizer'", wire.ComposeNode)
	}
	if len(wire.ComposeDeps) != 2 || wire.ComposeDeps[0] != "researcher" {
		t.Errorf("ComposeDeps = %v, want [researcher, analyst]", wire.ComposeDeps)
	}

	roundTripped := WireToProcInfo(wire)
	if roundTripped.ComposeNode != "summarizer" {
		t.Errorf("roundtrip ComposeNode = %q, want 'summarizer'", roundTripped.ComposeNode)
	}
	if len(roundTripped.ComposeDeps) != 2 {
		t.Errorf("roundtrip ComposeDeps len = %d, want 2", len(roundTripped.ComposeDeps))
	}
}

func TestProcInfoWire_PipelineRoundTrip(t *testing.T) {
	info := vfs.ProcInfo{
		PID:           2,
		UUID:          "test-uuid-002",
		State:         types.StateRunning,
		Intent:        "pipeline stage",
		CreatedAt:     time.Now(),
		PipelineIndex: 1,
		PipelineTotal: 3,
	}

	wire := ProcInfoToWire(info)
	if wire.PipelineIndex != 1 {
		t.Errorf("PipelineIndex = %d, want 1", wire.PipelineIndex)
	}
	if wire.PipelineTotal != 3 {
		t.Errorf("PipelineTotal = %d, want 3", wire.PipelineTotal)
	}

	roundTripped := WireToProcInfo(wire)
	if roundTripped.PipelineIndex != 1 || roundTripped.PipelineTotal != 3 {
		t.Errorf("roundtrip Pipeline = %d/%d, want 1/3", roundTripped.PipelineIndex, roundTripped.PipelineTotal)
	}
}

func TestProcInfoWire_OmitemptyBackwardCompat(t *testing.T) {
	info := vfs.ProcInfo{
		PID:       3,
		UUID:      "test-uuid-003",
		State:     types.StateRunning,
		Intent:    "plain",
		CreatedAt: time.Now(),
	}

	wire := ProcInfoToWire(info)
	data, err := json.Marshal(wire)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	jsonStr := string(data)
	if containsField(jsonStr, "compose_node") {
		t.Error("empty compose_node should be omitted from JSON (omitempty)")
	}
	if containsField(jsonStr, "pipeline_total") {
		t.Error("zero pipeline_total should be omitted from JSON (omitempty)")
	}
}

func TestGetProcDetailResponse_ComposeFields(t *testing.T) {
	resp := GetProcDetailResponse{
		PID:           1,
		UUID:          "test-uuid-004",
		State:         "running",
		Intent:        "test",
		ComposeNode:   "analyzer",
		ComposeDeps:   []string{"input-fetcher"},
		PipelineIndex: 0,
		PipelineTotal: 0,
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded GetProcDetailResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.ComposeNode != "analyzer" {
		t.Errorf("ComposeNode = %q, want 'analyzer'", decoded.ComposeNode)
	}
	if len(decoded.ComposeDeps) != 1 || decoded.ComposeDeps[0] != "input-fetcher" {
		t.Errorf("ComposeDeps = %v, want [input-fetcher]", decoded.ComposeDeps)
	}
}

func TestGetProcDetailResponse_PipelineFields(t *testing.T) {
	resp := GetProcDetailResponse{
		PID:           2,
		UUID:          "test-uuid-005",
		State:         "running",
		Intent:        "pipeline",
		PipelineIndex: 2,
		PipelineTotal: 4,
	}

	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded GetProcDetailResponse
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.PipelineIndex != 2 || decoded.PipelineTotal != 4 {
		t.Errorf("Pipeline = %d/%d, want 2/4", decoded.PipelineIndex, decoded.PipelineTotal)
	}
}

func TestSpawnRequest_OrchestrationFields(t *testing.T) {
	req := SpawnRequest{
		Intent:        "test",
		ComposeNode:   "worker",
		ComposeDeps:   []string{"fetcher"},
		PipelineIndex: 0,
		PipelineTotal: 2,
	}

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded SpawnRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.ComposeNode != "worker" {
		t.Errorf("ComposeNode = %q, want 'worker'", decoded.ComposeNode)
	}
	if decoded.PipelineTotal != 2 {
		t.Errorf("PipelineTotal = %d, want 2", decoded.PipelineTotal)
	}
}

// containsField checks if a JSON key appears in the raw JSON string.
func containsField(jsonStr, field string) bool {
	return json.Valid([]byte(jsonStr)) && len(jsonStr) > 0 &&
		(len(field) > 0 && searchForString(jsonStr, `"`+field+`"`))
}

func searchForString(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
