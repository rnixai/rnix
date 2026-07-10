package ipc

// step detail NOT_FOUND 修复 — 读侧降级单测
// (spec: spec-synthetic-subagent-dashboard-fix.md)。
//
// 根因: synthetic 子代理目录（Story 56.6）只有 steps.jsonl + proc-info.json，
// 没有 process-meta.json；handleGetStepDetail 的磁盘 fallback 强制要求该文件，
// 读失败直接 NOT_FOUND → TUI Timeline 展开永久 "Loading…"。
// 修复: readProcessMeta 失败降级为空 system_prompt / nil toolDefs 继续返回
// step 数据（toolDefs 走既有 StepRecord fallback）。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/kernel"
	"github.com/rnixai/rnix/vfs"
)

// I/O 矩阵行 4: steps.jsonl + proc-info.json 存在、process-meta.json 缺失
// → get_step_detail 返回 OK，system_prompt=""，step 字段完整。
func TestGetStepDetail_MissingProcessMeta_DegradesToEmptyPrompt(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)
	_, projBase := kernel.TestSetupDataDir(t, srv.kern)

	procUUID := "019576f4-9abc-7def-8123-456789abcdef"

	// synthetic 目录形态：proc-info.json + steps.jsonl，无 process-meta.json。
	if err := kernel.SaveProcInfo(projBase, vfs.ProcInfo{
		UUID: procUUID, Synthetic: true, ParentUUID: "host-uuid",
		PID: 0, State: types.StateDead, CreatedAt: time.Now(),
	}); err != nil {
		t.Fatalf("SaveProcInfo: %v", err)
	}
	writeTestStepsUUID(t, projBase, procUUID, []types.StepRecord{
		testStepRecord(1),
		testStepRecord(2),
	})
	if _, err := os.Stat(filepath.Join(projBase, "steps", procUUID, "process-meta.json")); !os.IsNotExist(err) {
		t.Fatal("test precondition: process-meta.json 不应存在")
	}

	conn := dial(t, sockPath)
	resp := sendRequest(t, conn, MethodGetStepDetail, GetStepDetailRequest{UUID: procUUID, Step: 1})

	if !resp.OK {
		t.Fatalf("缺 process-meta.json 应降级返回 OK，got error: %+v", resp.Error)
	}

	var detail GetStepDetailResponse
	if err := json.Unmarshal(resp.Payload, &detail); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if detail.SystemPrompt != "" {
		t.Errorf("SystemPrompt = %q, want \"\"（synthetic 节点无独立 system prompt）", detail.SystemPrompt)
	}
	// step 字段完整（来自 steps.jsonl 的 StepRecord）。
	if detail.Step != 1 {
		t.Errorf("Step = %d, want 1", detail.Step)
	}
	if detail.Summary != "step 1 summary" {
		t.Errorf("Summary = %q, want %q", detail.Summary, "step 1 summary")
	}
	if detail.ToolInput != `{"step":1}` {
		t.Errorf("ToolInput = %q, want %q", detail.ToolInput, `{"step":1}`)
	}
	if detail.ToolResult != "result_1" {
		t.Errorf("ToolResult = %q, want %q", detail.ToolResult, "result_1")
	}
	// toolDefs 走 StepRecord fallback（Action 非空 → 单条合成 ToolDef）。
	if len(detail.Tools) != 1 || detail.Tools[0].Name != "tool_call" {
		t.Errorf("Tools 应从 StepRecord fallback 合成, got %+v", detail.Tools)
	}
}

// I/O 矩阵行 6: process-meta.json 存在但 JSON 损坏 → 同缺失处理，降级不 crash。
func TestGetStepDetail_CorruptProcessMeta_DegradesToEmptyPrompt(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)
	_, projBase := kernel.TestSetupDataDir(t, srv.kern)

	procUUID := "019576f5-1111-7abc-9222-333344445555"
	writeTestStepsUUID(t, projBase, procUUID, []types.StepRecord{
		testStepRecord(1),
	})
	metaPath := filepath.Join(projBase, "steps", procUUID, "process-meta.json")
	if err := os.WriteFile(metaPath, []byte("{corrupt json"), 0o644); err != nil {
		t.Fatalf("write corrupt meta: %v", err)
	}

	conn := dial(t, sockPath)
	resp := sendRequest(t, conn, MethodGetStepDetail, GetStepDetailRequest{UUID: procUUID, Step: 1})

	if !resp.OK {
		t.Fatalf("损坏的 process-meta.json 应降级返回 OK，got error: %+v", resp.Error)
	}
	var detail GetStepDetailResponse
	if err := json.Unmarshal(resp.Payload, &detail); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if detail.SystemPrompt != "" {
		t.Errorf("SystemPrompt = %q, want \"\"", detail.SystemPrompt)
	}
	if detail.Step != 1 {
		t.Errorf("Step = %d, want 1", detail.Step)
	}
}

// I/O 矩阵行 5 回归护栏: 目录连 steps.jsonl 都没有 → 仍 NOT_FOUND（原行为不变）。
func TestGetStepDetail_NoStepsFile_StillNotFound(t *testing.T) {
	srv, sockPath, _ := setupTestServer(t)
	_, _ = kernel.TestSetupDataDir(t, srv.kern)

	conn := dial(t, sockPath)
	resp := sendRequest(t, conn, MethodGetStepDetail, GetStepDetailRequest{
		UUID: "019576f6-dead-7bee-8f00-000000000000", Step: 1,
	})
	if resp.OK {
		t.Fatal("无 steps.jsonl 的 UUID 应保持 NOT_FOUND")
	}
	if resp.Error == nil || resp.Error.Code != "NOT_FOUND" {
		t.Errorf("expected NOT_FOUND, got %+v", resp.Error)
	}
}
