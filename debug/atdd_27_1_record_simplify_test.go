package debug

import (
	"encoding/json"
	"testing"
)

// ============================================================
// Story 27.1 AC-9: Record 系统简化
//
// 验证 record 系统中的摘要字段已被删除。
// ============================================================

func TestRecordSimplify_ContextSnapshotData_NoSystemPromptHash(t *testing.T) {
	snap := ContextSnapshotData{
		Messages: []string{"hello"},
	}
	data, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if _, exists := raw["system_prompt_hash"]; exists {
		t.Fatal("ContextSnapshotData should not have system_prompt_hash field (AC-9: simplify)")
	}
}

func TestRecordSimplify_ContextSnapshotData_NoMessageCount(t *testing.T) {
	snap := ContextSnapshotData{
		Messages: []string{"hello", "world"},
	}
	data, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if _, exists := raw["message_count"]; exists {
		t.Fatal("ContextSnapshotData should not have message_count field (AC-9: simplify)")
	}
}

func TestRecordSimplify_ContextSnapshotData_NoTokenEstimate(t *testing.T) {
	snap := ContextSnapshotData{
		Messages: []string{"test"},
	}
	data, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if _, exists := raw["token_estimate"]; exists {
		t.Fatal("ContextSnapshotData should not have token_estimate field (AC-9: simplify)")
	}
}

func TestRecordSimplify_LLMResponseData_NoResponseSummary(t *testing.T) {
	resp := LLMResponseData{
		Model:          "claude",
		RequestTokens:  100,
		ResponseTokens: 50,
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if _, exists := raw["response_summary"]; exists {
		t.Fatal("LLMResponseData should not have response_summary field (AC-9: simplify)")
	}
}
