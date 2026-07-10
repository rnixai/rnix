package kernel

// Synthetic 子代理 dashboard 误报标红修复 — 写侧单测
// (spec: spec-synthetic-subagent-dashboard-fix.md, I/O 矩阵前三行)。
//
// 根因: cliSubagentTracker.finalize 从不写 ExitCode/ExitCodeSet，
// ui.IsProcessFailed 落到文本启发式 → 成功报告含 "fail"/"error" 字样即误标红。
// 修复: tool_result 路径写权威 exit code（is_error→1 否则 0，ExitCodeSet=true），
// 空报告成功收尾写非失败哨兵 "done"；stream-end 兜底（未报告即中断）写
// Result="interrupted" + ExitReason="interrupted"（历史统计分桶键），不伪造 exit code。

import (
	"strings"
	"testing"

	"github.com/rnixai/rnix/internal/types"
)

// isProcessFailedForTest mirrors internal/ui.IsProcessFailed (importing
// internal/ui here is impossible: ui → compose → kernel import cycle in
// tests). Authoritative path: ExitCodeSet → ExitCode != 0. Fallback: the
// result-text heuristic incl. the "interrupted" non-failure exception.
func isProcessFailedForTest(exitCode int, exitCodeSet bool, result string) bool {
	if exitCodeSet {
		return exitCode != 0
	}
	if result == "" {
		return true
	}
	lower := strings.ToLower(result)
	if lower == "interrupted" || lower == "cli_disconnected" {
		return false
	}
	return strings.Contains(lower, "error") ||
		strings.Contains(lower, "fail") ||
		strings.Contains(lower, "timeout")
}

// newExitCodeTestTracker builds a kernel + host + tracker whose synthetic
// children persist under a temp data dir, and returns the tracker plus its
// resolved step base dir (for proc-info.json round-trip assertions).
func newExitCodeTestTracker(t *testing.T) (*cliSubagentTracker, string) {
	t.Helper()
	// TempDir cleanup must run AFTER kernel Shutdown (LIFO): register it first.
	dataDir := t.TempDir()
	k := newSimpleKernel(t)
	k.SetDataDir(dataDir)

	host := NewProcess(0, "cli subagent exit code host", nil)
	tracker := k.newCLISubagentTracker(host)
	if tracker.baseDir == "" {
		t.Fatal("tracker baseDir empty — synthetic children would not persist")
	}
	return tracker, tracker.baseDir
}

func spawnExitCodeTestChild(t *testing.T, tracker *cliSubagentTracker, toolUseID string) *syntheticChild {
	t.Helper()
	c := tracker.getOrCreate(toolUseBlock{
		id:   toolUseID,
		name: "Task",
		input: map[string]any{
			"subagent_type": "explore",
			"description":   "run the test suite",
		},
	})
	if c == nil {
		t.Fatal("getOrCreate returned nil child")
	}
	return c
}

// I/O 矩阵行 1: 成功 tool_result（is_error 缺省=false），报告文本含 "fail" 字样
// → exit_code=0, exit_code_set=true；IsProcessFailed 走权威路径不标红。
func TestCLISubagent_FinalizeByToolResult_SuccessReportWithFailWord(t *testing.T) {
	tracker, baseDir := newExitCodeTestTracker(t)
	c := spawnExitCodeTestChild(t, tracker, "toolu_success")

	report := "All 42 tests passed. Note: TestRetryOnFailure now covers the timeout path."
	// is_error 缺省: extractToolResultBlocks 对缺失键默认 false。
	evt := map[string]any{
		"type": "user",
		"content": []any{
			map[string]any{
				"type":        "tool_result",
				"tool_use_id": "toolu_success",
				"content":     report,
			},
		},
	}
	results := extractToolResultBlocks(evt)
	if len(results) != 1 {
		t.Fatalf("extractToolResultBlocks: got %d blocks, want 1", len(results))
	}
	if results[0].isError {
		t.Error("is_error 缺省应解析为 false")
	}

	if !tracker.finalizeByToolResult(results[0].toolUseID, results[0].content, results[0].isError) {
		t.Fatal("finalizeByToolResult: child not found")
	}

	if c.info.State != types.StateDead {
		t.Errorf("State = %v, want Dead", c.info.State)
	}
	if c.info.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", c.info.ExitCode)
	}
	if !c.info.ExitCodeSet {
		t.Error("ExitCodeSet = false, want true（tool_result 路径必须写权威 exit code）")
	}
	if !strings.Contains(c.info.Result, "TestRetryOnFailure") {
		t.Errorf("Result 应保留报告文本, got %q", c.info.Result)
	}
	if isProcessFailedForTest(c.info.ExitCode, c.info.ExitCodeSet, c.info.Result) {
		t.Error("含 \"fail\"/\"timeout\" 字样的成功报告不应标红（权威 exit_code=0 应生效）")
	}

	// proc-info.json 落盘 round-trip。
	hist, err := LoadProcHistory(baseDir, 100)
	if err != nil {
		t.Fatalf("LoadProcHistory: %v", err)
	}
	loaded := hist.FindByUUID(c.uuid)
	if loaded == nil {
		t.Fatal("synthetic child 未落盘 proc-info.json")
	}
	if loaded.ExitCode != 0 || !loaded.ExitCodeSet {
		t.Errorf("proc-info.json round-trip: ExitCode=%d ExitCodeSet=%v, want 0/true",
			loaded.ExitCode, loaded.ExitCodeSet)
	}
}

// I/O 矩阵行 2: tool_result 带 is_error=true → exit_code=1, exit_code_set=true；标红。
func TestCLISubagent_FinalizeByToolResult_IsErrorTrue(t *testing.T) {
	tracker, baseDir := newExitCodeTestTracker(t)
	c := spawnExitCodeTestChild(t, tracker, "toolu_error")

	evt := map[string]any{
		"type": "user",
		"content": []any{
			map[string]any{
				"type":        "tool_result",
				"tool_use_id": "toolu_error",
				"content":     "subagent crashed",
				"is_error":    true,
			},
		},
	}
	results := extractToolResultBlocks(evt)
	if len(results) != 1 {
		t.Fatalf("extractToolResultBlocks: got %d blocks, want 1", len(results))
	}
	if !results[0].isError {
		t.Fatal("is_error=true 未被 extractToolResultBlocks 提取")
	}

	if !tracker.finalizeByToolResult(results[0].toolUseID, results[0].content, results[0].isError) {
		t.Fatal("finalizeByToolResult: child not found")
	}

	if c.info.ExitCode != 1 {
		t.Errorf("ExitCode = %d, want 1", c.info.ExitCode)
	}
	if !c.info.ExitCodeSet {
		t.Error("ExitCodeSet = false, want true")
	}
	if !isProcessFailedForTest(c.info.ExitCode, c.info.ExitCodeSet, c.info.Result) {
		t.Error("is_error=true 的子代理应判定为失败")
	}

	hist, err := LoadProcHistory(baseDir, 100)
	if err != nil {
		t.Fatalf("LoadProcHistory: %v", err)
	}
	loaded := hist.FindByUUID(c.uuid)
	if loaded == nil {
		t.Fatal("synthetic child 未落盘 proc-info.json")
	}
	if loaded.ExitCode != 1 || !loaded.ExitCodeSet {
		t.Errorf("proc-info.json round-trip: ExitCode=%d ExitCodeSet=%v, want 1/true",
			loaded.ExitCode, loaded.ExitCodeSet)
	}
}

// I/O 矩阵行 3: stream end 无 tool_result → finalizeAll 兜底：
// Result="interrupted"（复用既有非失败例外），ExitCodeSet 保持 false。
func TestCLISubagent_FinalizeAll_Backstop_Interrupted(t *testing.T) {
	tracker, _ := newExitCodeTestTracker(t)
	c := spawnExitCodeTestChild(t, tracker, "toolu_interrupted")

	tracker.finalizeAll()

	if c.info.State != types.StateDead {
		t.Errorf("State = %v, want Dead", c.info.State)
	}
	if c.info.Result != "interrupted" {
		t.Errorf("Result = %q, want \"interrupted\"（兜底路径未报告即中断）", c.info.Result)
	}
	if c.info.ExitReason != "interrupted" {
		t.Errorf("ExitReason = %q, want \"interrupted\"（历史统计按 ExitReason 分桶，缺失会把中断条目计入 Done 虚增成功率）", c.info.ExitReason)
	}
	if c.info.ExitCodeSet {
		t.Error("ExitCodeSet = true, want false（兜底路径不伪造 exit code）")
	}
	if isProcessFailedForTest(c.info.ExitCode, c.info.ExitCodeSet, c.info.Result) {
		t.Error("interrupted 子代理应渲染为非失败（symbols.go 既有例外）")
	}
}

// 审查补充: 成功 tool_result 但报告文本为空 → Result 写非失败哨兵 "done"。
// 若留空，ui.StateBadge 的文本通道（isFailedResult("")==true）会渲染 [E]/失败徽章，
// 与同一行的权威 exit_code=0（IsProcessFailed 不红）自相矛盾。
func TestCLISubagent_FinalizeByToolResult_EmptyReport_NonFailureSentinel(t *testing.T) {
	tracker, _ := newExitCodeTestTracker(t)
	c := spawnExitCodeTestChild(t, tracker, "toolu_empty")

	if !tracker.finalizeByToolResult("toolu_empty", "", false) {
		t.Fatal("finalizeByToolResult: child not found")
	}

	if c.info.ExitCode != 0 || !c.info.ExitCodeSet {
		t.Errorf("ExitCode=%d ExitCodeSet=%v, want 0/true", c.info.ExitCode, c.info.ExitCodeSet)
	}
	if c.info.Result != "done" {
		t.Errorf("Result = %q, want \"done\"（空报告成功收尾需非失败哨兵，避免文本启发式消费方与权威 exit code 矛盾）", c.info.Result)
	}
	if c.info.ExitReason != "" {
		t.Errorf("ExitReason = %q, want \"\"（正常收尾不写 interrupted）", c.info.ExitReason)
	}
}
