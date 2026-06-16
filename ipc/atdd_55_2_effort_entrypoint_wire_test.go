package ipc

import (
	"encoding/json"
	"strings"
	"testing"
)

// ============================================================
// ATDD — Story 55.2: per-request reasoning_effort 外部入口（IPC wire 层）
// AC #3 (SpawnRequest wire 字段) + AC #8 (透传铁律) + AC #9 (omitempty 零回归)
//
// 区别于 atdd_55_2_effort_wire_test.go（展示面 story 的 ProcInfoWire roundtrip）：
// 本文件测的是「外部入口」的 SpawnRequest / SpawnPipelineCommand wire 字段。
//
// 这些是 GREEN-GUARD（非 t.Skip RED）：字段是纯 wire 合同——加字段+json tag
// 后 marshal/unmarshal 自然工作，无装配逻辑。它们锁定合同（reasoning_effort
// tag / omitempty / 原样透传不转大小写），随实现保持 GREEN，实时拦截字段命名
// 与序列化红线。server_spawn.go / server_pipeline.go 的 SpawnOpts 填值由编译期
// + 既有 handler 测试覆盖；wire roundtrip 是跨进程合同的咽喉。
// ============================================================

// --- 55-2-IPC-001 [P0]: SpawnRequest.ReasoningEffort marshal→unmarshal 保留 (AC #3) ---

func TestEffortEntrypointWire_SpawnRequest_RoundTrip(t *testing.T) {
	sr := SpawnRequest{Intent: "分析", ReasoningEffort: "high"}
	data, err := json.Marshal(sr)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(data), `"reasoning_effort":"high"`) {
		t.Errorf("AC#3: wire JSON should carry reasoning_effort tag, got: %s", data)
	}
	var decoded SpawnRequest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.ReasoningEffort != "high" {
		t.Errorf("AC#3: roundtrip effort = %q, want %q", decoded.ReasoningEffort, "high")
	}
}

// --- 55-2-IPC-002 [P1]: 空 effort omitempty 不出现在 JSON（零回归） (AC #9) ---

func TestEffortEntrypointWire_SpawnRequest_OmitEmpty(t *testing.T) {
	data, err := json.Marshal(SpawnRequest{Intent: "分析"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(data), "reasoning_effort") {
		t.Errorf("AC#9: empty effort must be omitted (omitempty), got: %s", data)
	}
}

// --- 55-2-IPC-003 [P1]: SpawnPipelineCommand.ReasoningEffort roundtrip + omitempty (AC #6) ---

func TestEffortEntrypointWire_SpawnPipelineCommand_RoundTrip(t *testing.T) {
	c := SpawnPipelineCommand{Intent: "stage", ReasoningEffort: "low"}
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(data), `"reasoning_effort":"low"`) {
		t.Errorf("AC#6: pipeline stage should carry reasoning_effort, got: %s", data)
	}
	var decoded SpawnPipelineCommand
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded.ReasoningEffort != "low" {
		t.Errorf("AC#6: roundtrip effort = %q, want %q", decoded.ReasoningEffort, "low")
	}

	empty, _ := json.Marshal(SpawnPipelineCommand{Intent: "x"})
	if strings.Contains(string(empty), "reasoning_effort") {
		t.Errorf("AC#9: empty pipeline effort must be omitted, got: %s", empty)
	}
}

// --- 55-2-IPC-004 [P0]: 透传铁律——HIGH/high/xhigh 原样保留，不转大小写 (AC #8) ---

func TestEffortEntrypointWire_Passthrough_VerbatimNoCaseFold(t *testing.T) {
	for _, v := range []string{"HIGH", "high", "xhigh", "MEDIUM", "max"} {
		sr := SpawnRequest{Intent: "x", ReasoningEffort: v}
		data, _ := json.Marshal(sr)
		var decoded SpawnRequest
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Fatalf("unmarshal %q: %v", v, err)
		}
		if decoded.ReasoningEffort != v {
			t.Errorf("AC#8: effort %q must pass through verbatim (no case-fold/mapping), got %q", v, decoded.ReasoningEffort)
		}
	}
}
