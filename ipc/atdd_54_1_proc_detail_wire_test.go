package ipc

import (
	"encoding/json"
	"slices"
	"testing"

	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/kernel"
)

// ATDD Story 54.1 / AC5 — proc.AllowedTools 贯通 IPC ProcDetail wire。
//
// RED 形态：骨架 + t.Skip（与 kernel/atdd_54_1_*.go 一致，用户 Decker 2026-06-09 拍板）。
// 骨架已在 ipc/protocol.go 的 GetProcDetailResponse 加 AllowedTools 字段，故 JSON tag
// green-guard 立即可绿；填充端（server_process.go 把 proc.AllowedTools 投影到 wire）是
// dev-story 的活 → 活进程填充用例 t.Skip 标记，移除后 RED，dev 落地填充后转绿。
//
// 复用 atdd_42_3 同包 helper：setupResumeIPCTest / callGetProcDetail / containsSubstring。

// 54.1-INT-013 [RED] AC5：活进程 GetProcDetail 填充 AllowedTools（server_process.go:318 区）。
//
// RED：当前 handleGetProcDetail 不投影 proc.AllowedTools → resp.AllowedTools == nil。
func TestATDD_54_1_IPC_001_GetProcDetail_LiveProcess_AllowedTools(t *testing.T) {
	client, kern, _, _ := setupResumeIPCTest(t)

	proc := kernel.NewProcess(0, "live tool-level process", nil)
	proc.AllowedTools = []string{"Read", "Write"}
	if err := proc.Start(); err != nil {
		t.Fatalf("proc.Start: %v", err)
	}
	kern.AddProcess(proc)
	t.Cleanup(func() { _ = kern.Kill(proc.PID, types.SIGKILL) })

	resp, err := callGetProcDetail(t, client, proc.PID)
	if err != nil {
		t.Fatalf("GetProcDetail: %v", err)
	}
	if !slices.Contains(resp.AllowedTools, "Read") {
		t.Errorf("AC5: resp.AllowedTools = %v, 应投影 proc.AllowedTools（含 Read）", resp.AllowedTools)
	}
}

// 54.1-INT-013b GREEN 护栏 AC5：进程级 allowed_tools 的 wire 契约 + 与 SkillInfoWire.AllowedTools
// 区分（命名陷阱）。骨架已加字段+tag → 即应通过；守护「顶层进程级」与「skills[] 声明级」并存不混淆。
func TestATDD_54_1_IPC_002_GetProcDetailResponse_AllowedToolsJSONTag_GreenGuard(t *testing.T) {
	resp := GetProcDetailResponse{
		PID:          types.PID(42),
		UUID:         "allowed-tools-aaaaaaaa-bbbb-cccc-dddd-000000000001",
		AllowedTools: []string{"Read", "Write"},                                         // 进程级权威工具集
		Skills:       []SkillInfoWire{{Name: "demo", AllowedTools: []string{"/dev/fs"}}}, // skill 声明级（不同概念）
	}
	data, err := json.Marshal(resp)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	jsonStr := string(data)
	if !containsSubstring(jsonStr, `"allowed_tools"`) || !containsSubstring(jsonStr, `"Read"`) {
		t.Errorf("AC5: 进程级 allowed_tools 缺失: %s", jsonStr)
	}
	// 与 skill 声明级区分：skills 嵌套 wire 仍在（两个 allowed_tools 概念并存）。
	if !containsSubstring(jsonStr, `"skills":[`) {
		t.Errorf("AC5: skills wire 应保留（顶层进程级 allowed_tools 不取代 skill 声明级）: %s", jsonStr)
	}

	// 空 AllowedTools → omitempty 省略（避免 legacy / 无约束进程的 wire 噪声）。
	resp2 := GetProcDetailResponse{PID: types.PID(43), UUID: "no-tools-aaaaaaaa-bbbb-cccc-dddd-000000000001"}
	data2, _ := json.Marshal(resp2)
	if containsSubstring(string(data2), `"allowed_tools"`) {
		t.Errorf("AC5: 空 AllowedTools 应 omitempty 省略，got: %s", string(data2))
	}
}
