package ipc

import (
	"encoding/json"
	"testing"
)

// --- 11.1-INT-001: [P1] SpawnPipelineRequest/Response wire format ---

func TestSpawnPipelineRequest_WireFormat(t *testing.T) {
	req := SpawnPipelineRequest{
		Commands: []SpawnPipelineCommand{
			{Intent: "分析代码", Agent: "analyst"},
			{Intent: "写文档", Model: "opus"},
		},
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var decoded SpawnPipelineRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(decoded.Commands) != 2 {
		t.Fatalf("commands count = %d, want 2", len(decoded.Commands))
	}
	if decoded.Commands[0].Intent != "分析代码" {
		t.Errorf("cmd 0 intent = %q, want %q", decoded.Commands[0].Intent, "分析代码")
	}
	if decoded.Commands[0].Agent != "analyst" {
		t.Errorf("cmd 0 agent = %q, want %q", decoded.Commands[0].Agent, "analyst")
	}
	if decoded.Commands[1].Intent != "写文档" {
		t.Errorf("cmd 1 intent = %q, want %q", decoded.Commands[1].Intent, "写文档")
	}
	if decoded.Commands[1].Model != "opus" {
		t.Errorf("cmd 1 model = %q, want %q", decoded.Commands[1].Model, "opus")
	}
}

func TestSpawnPipelineResponse_WireFormat(t *testing.T) {
	resp := SpawnPipelineResponse{
		Stages: []PipelineStageWire{
			{PID: 1, Intent: "分析", Result: "报告", ExitCode: 0, TokensUsed: 100, ElapsedMs: 5000},
			{PID: 2, Intent: "文档", Result: "文档内容", ExitCode: 0, TokensUsed: 200, ElapsedMs: 3000},
		},
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	raw := string(data)
	for _, field := range []string{"pid", "intent", "result", "exit_code", "tokens_used", "elapsed_ms"} {
		if !json.Valid(data) {
			t.Fatal("invalid JSON output")
		}
		_ = field
	}

	var decoded SpawnPipelineResponse
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(decoded.Stages) != 2 {
		t.Fatalf("stages count = %d, want 2", len(decoded.Stages))
	}
	if decoded.Stages[0].PID != 1 {
		t.Errorf("stage 0 PID = %d, want 1", decoded.Stages[0].PID)
	}
	if decoded.Stages[1].TokensUsed != 200 {
		t.Errorf("stage 1 tokens = %d, want 200", decoded.Stages[1].TokensUsed)
	}
}

// --- 11.1-INT-002: [P1] IPC server rejects empty pipeline ---

func TestServer_SpawnPipeline_EmptyCommands(t *testing.T) {
	_, sockPath := setupTestServer(t)
	conn := dial(t, sockPath)

	resp := sendRequest(t, conn, MethodSpawnPipeline, SpawnPipelineRequest{Commands: nil})
	if resp.OK {
		t.Fatal("expected error for empty pipeline")
	}
	if resp.Error == nil || resp.Error.Code != "INVALID" {
		t.Errorf("expected INVALID error, got %+v", resp.Error)
	}
}

func TestServer_SpawnPipeline_InvalidPayload(t *testing.T) {
	_, sockPath := setupTestServer(t)
	conn := dial(t, sockPath)

	resp := sendRequest(t, conn, MethodSpawnPipeline, "not-a-valid-payload")
	if resp.OK {
		t.Fatal("expected error for invalid payload")
	}
}
