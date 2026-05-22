package ipc

import (
	"encoding/json"
	"testing"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/kernel"
	"github.com/rnixai/rnix/vfs"
)

// =============================================================================
// ATDD 42.3: 可观测层 — GetProcDetailResponse OriginUUID/ResumedFromStep（AC#6, AC#8）
//
// Covers 4 scenarios:
//   - IPC-005: 活进程 detail OriginUUID 填充
//   - IPC-006: 活进程 detail ResumedFromStep 填充
//   - IPC-007: 历史进程 detail 从 procInfoDisk 派生
//   - IPC-008: JSON tag snake_case 序列化校验
//
// RED PHASE:
//   - handleGetProcDetail 尚未填充 OriginUUID/ResumedFromStep（即使字段已存在）
//   - 活/历史路径的填充逻辑都在 dev-story 阶段补齐
// =============================================================================

// --- 42.3-IPC-005: 活进程 detail OriginUUID 填充 (AC#6, AC#8) ---

func TestATDD_42_3_IPC_005_GetProcDetail_LiveProcess_OriginUUID(t *testing.T) {
	client, kern, _, _ := setupResumeIPCTest(t)

	// 注入一个活进程，proc.OriginUUID 非空模拟 fork 结果
	proc := kernel.NewProcess(0, "live forked process", nil)
	originUUID := "live-origin-aaaaaaaa-bbbb-cccc-dddd-000000000001"
	proc.OriginUUID = originUUID
	proc.ResumedFromStep = 25
	if err := proc.Start(); err != nil {
		t.Fatalf("proc.Start: %v", err)
	}
	kern.AddProcess(proc)
	t.Cleanup(func() { _ = kern.Kill(proc.PID, types.SIGKILL) })

	resp, err := callGetProcDetail(t, client, proc.PID)
	if err != nil {
		t.Fatalf("GetProcDetail: %v", err)
	}
	if resp.OriginUUID != originUUID {
		t.Errorf("OriginUUID = %q, want %q", resp.OriginUUID, originUUID)
	}
}

// --- 42.3-IPC-006: 活进程 detail ResumedFromStep 填充 (AC#8) ---

func TestATDD_42_3_IPC_006_GetProcDetail_LiveProcess_ResumedFromStep(t *testing.T) {
	client, kern, _, _ := setupResumeIPCTest(t)

	proc := kernel.NewProcess(0, "live step process", nil)
	proc.OriginUUID = "step-origin-aaaaaaaa-bbbb-cccc-dddd-000000000001"
	proc.ResumedFromStep = 31
	if err := proc.Start(); err != nil {
		t.Fatalf("proc.Start: %v", err)
	}
	kern.AddProcess(proc)
	t.Cleanup(func() { _ = kern.Kill(proc.PID, types.SIGKILL) })

	resp, err := callGetProcDetail(t, client, proc.PID)
	if err != nil {
		t.Fatalf("GetProcDetail: %v", err)
	}
	if resp.ResumedFromStep != 31 {
		t.Errorf("ResumedFromStep = %d, want 31", resp.ResumedFromStep)
	}
}

// --- 42.3-IPC-007: 历史进程 detail 从 procInfoDisk 派生 (AC#6, AC#8) ---

func TestATDD_42_3_IPC_007_GetProcDetail_HistoricalProcess_LineageFromDisk(t *testing.T) {
	client, kern, _, _ := setupResumeIPCTest(t)
	// Use a standards-compliant UUID format (8-4-4-4-12 hex) so the server's
	// isValidUUID gate accepts it on the history path.
	uuid := "01ec9999-1234-7890-abcd-000000000001"
	origin := "01ec9999-1234-7890-abcd-000000000002"

	// procHistory 注入已 reaped 进程
	procHist := kern.ProcHistory()
	procHist.Add(vfs.ProcInfo{
		PID:             types.PID(7), UUID: uuid,
		State:           types.StateDead, Intent: "history fork",
		OriginUUID:      origin,
		ResumedFromStep: 12,
	})

	resp, err := callGetProcDetailByUUID(t, client, uuid)
	if err != nil {
		t.Fatalf("GetProcDetail (history): %v", err)
	}
	if resp.OriginUUID != origin {
		t.Errorf("OriginUUID = %q, want %q", resp.OriginUUID, origin)
	}
	if resp.ResumedFromStep != 12 {
		t.Errorf("ResumedFromStep = %d, want 12", resp.ResumedFromStep)
	}
}

// --- 42.3-IPC-008: JSON tag snake_case 序列化校验 (AC#8) ---

func TestATDD_42_3_IPC_008_GetProcDetailResponse_JSONTags(t *testing.T) {
	resp := GetProcDetailResponse{
		PID:             types.PID(42),
		UUID:            "json-tag-aaaaaaaa-bbbb-cccc-dddd-000000000001",
		OriginUUID:      "json-origin-aaaaaaaa-bbbb-cccc-dddd-000000000002",
		ResumedFromStep: 15,
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	jsonStr := string(data)
	for _, want := range []string{
		`"origin_uuid":"json-origin-`,
		`"resumed_from_step":15`,
	} {
		if !containsSubstring(jsonStr, want) {
			t.Errorf("JSON missing %q\noutput: %s", want, jsonStr)
		}
	}

	// Empty OriginUUID + 0 ResumedFromStep should omit via omitempty
	resp2 := GetProcDetailResponse{
		PID:  types.PID(43),
		UUID: "no-lineage-aaaaaaaa-bbbb-cccc-dddd-000000000001",
	}
	data2, _ := json.Marshal(resp2)
	if containsSubstring(string(data2), "origin_uuid") {
		t.Errorf("empty OriginUUID should omit, got: %s", string(data2))
	}
	if containsSubstring(string(data2), "resumed_from_step") {
		t.Errorf("zero ResumedFromStep should omit, got: %s", string(data2))
	}
}

// =============================================================================
// Helpers — replace bodies when GetProcDetail accepts richer queries.
// =============================================================================

func callGetProcDetail(t *testing.T, c *Client, pid types.PID) (*GetProcDetailResponse, error) {
	t.Helper()
	return c.GetProcDetail(pid)
}

func callGetProcDetailByUUID(t *testing.T, c *Client, uuid string) (*GetProcDetailResponse, error) {
	t.Helper()
	// Client.GetProcDetail accepts (pid, uuid...); pass uuid only so the server
	// resolves via procHistory or disk. PID 0 alone would 404 — UUID is required.
	return c.GetProcDetail(0, uuid)
}

func containsSubstring(s, substr string) bool {
	if len(substr) == 0 {
		return true
	}
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
