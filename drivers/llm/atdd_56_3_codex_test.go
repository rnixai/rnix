package llm

// ATDD coverage for Story 56.3 — codex-cli driver raw capture
// (CAP-1 argv 含 effort 真实值 + CAP-2 stdout/stderr/exit_code)。
//
// codex 形态特征：prompt 走 argv 末尾（buildArgs append），stdin=""。
// 透传参数：`-c model_reasoning_effort=<v>` (KV form)。
//
// 走真实 LLMFile 路径（openLLMFile + writeStringReq）→ 验证 sink 注入、
// driver 出口填充、LLMFile 字段归集全链路。

import (
	"strings"
	"testing"
)

// 56-3-CODEX-001 — Call 路径 raw capture（argv + stdin="" + stdout/stderr/exit）
func TestATDD_56_3_CODEX001_Call_RawCaptured(t *testing.T) {
	t.Parallel()
	d := NewCodexCliDriver(
		CodexWithCommandBuilder(codexMockCmdBuilder("codex_call_success")),
		CodexWithReasoningEffort("high"),
	)
	f := openLLMFile(t, d, ModeCall)
	defer f.Close()
	writeStringReq(t, f, `{"intent":"summarize the repo"}`)

	cap := f.LastRawCapture()
	if cap == nil {
		t.Fatal("LastRawCapture() == nil — sink 未注入或 driver 出口未填充")
	}
	if cap.Kind != "cli" {
		t.Errorf("Kind=%q want cli", cap.Kind)
	}
	if cap.TsMs <= 0 {
		t.Errorf("TsMs not set: %d", cap.TsMs)
	}

	// argv: []string，CLI 透传 effort 必须可见
	argv, ok := cap.Request["argv"].([]string)
	if !ok {
		t.Fatalf("Request[argv] not []string: %T", cap.Request["argv"])
	}
	// 验证 effort 真实值出现在 argv（CAP-1）：codex 是 -c model_reasoning_effort=high
	joined := strings.Join(argv, " ")
	if !strings.Contains(joined, "model_reasoning_effort=high") {
		t.Errorf("effort flag not in argv: %v", argv)
	}
	// codex 的 prompt 在 argv 里（最后一个元素），故 stdin=""
	stdin, ok := cap.Request["stdin"].(string)
	if !ok {
		t.Fatalf("Request[stdin] not string: %T", cap.Request["stdin"])
	}
	if stdin != "" {
		t.Errorf("codex stdin should be empty (prompt in argv), got %q", stdin)
	}

	// Response: stdout/stderr/exit_code（裁决 3 字段）
	stdout, ok := cap.Response["stdout"].(string)
	if !ok {
		t.Fatalf("Response[stdout] not string: %T", cap.Response["stdout"])
	}
	if !strings.Contains(stdout, "The repository structure looks good.") {
		t.Errorf("stdout missing helper-process payload: %q", stdout)
	}
	if _, ok := cap.Response["stderr"].(string); !ok {
		t.Fatalf("Response[stderr] not string: %T", cap.Response["stderr"])
	}
	if _, ok := cap.Response["exit_code"].(int); !ok {
		t.Fatalf("Response[exit_code] not int: %T", cap.Response["exit_code"])
	}
}

// 56-3-CODEX-002 — Stream 路径 raw capture（tee stdout 累积原样 stream-json）
func TestATDD_56_3_CODEX002_Stream_RawCaptured(t *testing.T) {
	t.Parallel()
	d := NewCodexCliDriver(
		CodexWithCommandBuilder(codexMockCmdBuilder("codex_stream_success")),
		CodexWithReasoningEffort("medium"),
	)
	f := openLLMFile(t, d, ModeStream)
	defer f.Close()
	writeStringReq(t, f, `{"intent":"list go files"}`)

	cap := f.LastRawCapture()
	if cap == nil {
		t.Fatal("LastRawCapture() == nil for Stream path")
	}
	if cap.Kind != "cli" {
		t.Errorf("Kind=%q want cli", cap.Kind)
	}

	argv, _ := cap.Request["argv"].([]string)
	joined := strings.Join(argv, " ")
	if !strings.Contains(joined, "model_reasoning_effort=medium") {
		t.Errorf("effort=medium not in argv: %v", argv)
	}
	if !strings.Contains(joined, "--json") {
		t.Errorf("--json flag absent in stream argv: %v", argv)
	}

	// stream-json 原样字节（裁决 7 tee）必须含两个事件类型
	stdout, _ := cap.Response["stdout"].(string)
	if !strings.Contains(stdout, `"type":"thread.started"`) {
		t.Errorf("stdout missing thread.started raw event: %q", stdout)
	}
	if !strings.Contains(stdout, `"type":"item.completed"`) {
		t.Errorf("stdout missing item.completed raw event: %q", stdout)
	}
	exit, _ := cap.Response["exit_code"].(int)
	if exit != 0 {
		t.Errorf("exit_code=%d want 0 for stream_success", exit)
	}
}
