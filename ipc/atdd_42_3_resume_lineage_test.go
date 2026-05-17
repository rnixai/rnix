package ipc

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/vfs"
)

// =============================================================================
// ATDD 42.3: 可观测层 — IPC get_resume_lineage 集成测试（AC#7）
//
// Covers 3 scenarios:
//   - IPC-002: 单 fork 链端到端（procHistory 注入 + Client.GetResumeLineage）
//   - IPC-003: 空 UUID → ErrInvalid
//   - IPC-004: MethodGetResumeLineage 与 MethodLineage 不冲突（dispatch 路由独立）
//
// RED PHASE:
//   - server.handleGetResumeLineage 已 stub，但 kernel.GetResumeLineage 返回
//     ErrNotFound（BuildResumeLineage 未实现）
//   - 测试用 t.Skip 跳过；dev-story 替换 BuildResumeLineage 实现后移除 skip
// =============================================================================

// --- 42.3-IPC-002: get_resume_lineage 端到端（单 fork 链）(AC#7) ---

func TestATDD_42_3_IPC_002_GetResumeLineage_SingleForkChain(t *testing.T) {
	t.Skip("RED PHASE: kernel.BuildResumeLineage not yet implemented")

	client, kern, _ := setupResumeIPCTest(t)
	parent := "lineage-parent-aaaaaaaa-bbbb-cccc-dddd-000000000001"
	child := "lineage-child-aaaaaaaa-bbbb-cccc-dddd-000000000002"

	// 注入 procHistory（无需磁盘文件，BuildResumeLineage 优先用 procHistory）
	procHist := kern.ProcHistory()
	procHist.Add(vfs.ProcInfo{
		PID: types.PID(1), UUID: parent, State: types.StateDead, Intent: "parent",
	})
	procHist.Add(vfs.ProcInfo{
		PID: types.PID(2), UUID: child, State: types.StateDead, Intent: "child",
		OriginUUID: parent, ResumedFromStep: 10,
	})

	resp, err := client.GetResumeLineage(child)
	if err != nil {
		t.Fatalf("GetResumeLineage: %v", err)
	}
	if resp.Current.UUID != child {
		t.Errorf("Current.UUID = %q, want %q", resp.Current.UUID, child)
	}
	if len(resp.Ancestors) != 1 {
		t.Fatalf("Ancestors len = %d, want 1", len(resp.Ancestors))
	}
	if resp.Ancestors[0].UUID != parent {
		t.Errorf("Ancestors[0].UUID = %q, want %q", resp.Ancestors[0].UUID, parent)
	}
	if resp.Truncated {
		t.Error("Truncated = true, want false for 1-level chain")
	}
}

// --- 42.3-IPC-003: 空 UUID → ErrInvalid (AC#7) ---

func TestATDD_42_3_IPC_003_GetResumeLineage_EmptyUUID(t *testing.T) {
	client, _, _ := setupResumeIPCTest(t)

	_, err := client.GetResumeLineage("")
	if err == nil {
		t.Fatal("GetResumeLineage(\"\") should return error")
	}
	// handleGetResumeLineage stub 已实现空 UUID 拒绝，所以此用例可以在 RED PHASE 通过
	if !strings.Contains(err.Error(), "uuid") {
		t.Errorf("err = %v, want substring 'uuid'", err)
	}
}

// --- 42.3-IPC-004: MethodGetResumeLineage 路由独立于 MethodLineage (AC#7) ---

func TestATDD_42_3_IPC_004_GetResumeLineage_RoutingIndependent(t *testing.T) {
	// 该用例验证 dispatch 已注册 MethodGetResumeLineage 常量并路由到独立 handler
	// （而非 fallthrough 到 handleLineage 的 stem-cell 分化语义）。
	// RED PHASE：handleGetResumeLineage stub 已注册，所以这条断言可立即生效。

	client, kern, _ := setupResumeIPCTest(t)
	uuid := "route-aaaaaaaa-bbbb-cccc-dddd-000000000001"
	procHist := kern.ProcHistory()
	procHist.Add(vfs.ProcInfo{
		PID: types.PID(50), UUID: uuid, State: types.StateDead, Intent: "route test",
	})

	// 调用 GetResumeLineage 应得到独立响应类型（而非 LineageResponse）。
	resp, err := client.GetResumeLineage(uuid)
	if err != nil {
		// RED PHASE 期间 kernel.BuildResumeLineage 返回 ErrNotFound 是预期。
		// 关键是 err 不应该是 "unknown method" 或 LineageRequest 风格的错误。
		if strings.Contains(err.Error(), "unknown method") {
			t.Fatalf("dispatch missing for MethodGetResumeLineage: %v", err)
		}
		t.Logf("RED PHASE expected lookup error: %v", err)
		return
	}
	// 一旦 GREEN PHASE 实现，结构应是 GetResumeLineageResponse（非 LineageResponse）
	if _, marshalErr := json.Marshal(resp); marshalErr != nil {
		t.Errorf("response not serializable as GetResumeLineageResponse: %v", marshalErr)
	}
}
