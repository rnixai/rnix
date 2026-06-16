package llm

// ATDD coverage for Story 56.3 — qwen-cli driver raw capture
// (CAP-1 argv + stdin = prompt + CAP-2 stdout/stderr/exit_code)。
//
// qwen 形态特征：prompt 走 stdin（cmd.Stdin = strings.NewReader(prompt)），
// 类似 claude；不支持 effort（Qwen3-Coder 无 effort 概念，按 CLAUDE.md
// reasoning_effort 表）。仅验 argv / stdin = prompt / stdout / exit_code。

import (
	"strings"
	"testing"
)

// 56-3-QWEN-001 — Call 路径 raw capture（stdin = prompt）
func TestATDD_56_3_QWEN001_Call_RawCaptured(t *testing.T) {
	t.Parallel()
	d := NewQwenCliDriver(QwenWithCommandBuilder(qwenMockCmdBuilder("qwen_success")))
	f := openLLMFile(t, d, ModeCall)
	defer f.Close()
	writeStringReq(t, f, `{"intent":"hi-qwen","system_prompt":"You are concise"}`)

	cap := f.LastRawCapture()
	if cap == nil {
		t.Fatal("LastRawCapture() == nil")
	}
	if cap.Kind != "cli" {
		t.Errorf("Kind=%q want cli", cap.Kind)
	}

	argv, ok := cap.Request["argv"].([]string)
	if !ok {
		t.Fatalf("Request[argv] not []string: %T", cap.Request["argv"])
	}
	if argv[0] == "" {
		t.Errorf("argv[0] empty: %v", argv)
	}

	// qwen 走 stdin → prompt 必须出现在 stdin
	stdin, _ := cap.Request["stdin"].(string)
	if !strings.Contains(stdin, "hi-qwen") {
		t.Errorf("stdin missing intent: %q", stdin)
	}

	stdout, _ := cap.Response["stdout"].(string)
	if !strings.Contains(stdout, `"result":"test output"`) {
		t.Errorf("stdout missing fixture payload: %q", stdout)
	}
	exit, _ := cap.Response["exit_code"].(int)
	if exit != 0 {
		t.Errorf("exit_code=%d want 0", exit)
	}
}

// 56-3-QWEN-002 — Stream 路径 raw capture
func TestATDD_56_3_QWEN002_Stream_RawCaptured(t *testing.T) {
	t.Parallel()
	d := NewQwenCliDriver(QwenWithCommandBuilder(qwenMockCmdBuilder("qwen_stream_success")))
	f := openLLMFile(t, d, ModeStream)
	defer f.Close()
	writeStringReq(t, f, `{"intent":"hi-qwen"}`)

	cap := f.LastRawCapture()
	if cap == nil {
		t.Fatal("LastRawCapture() == nil for Stream path")
	}
	stdin, _ := cap.Request["stdin"].(string)
	if !strings.Contains(stdin, "hi-qwen") {
		t.Errorf("stream stdin missing intent: %q", stdin)
	}

	stdout, _ := cap.Response["stdout"].(string)
	if !strings.Contains(stdout, `"result":"hello world"`) {
		t.Errorf("stream stdout missing raw result: %q", stdout)
	}
	if !strings.Contains(stdout, `"type":"assistant"`) {
		t.Errorf("stream stdout missing assistant event: %q", stdout)
	}
}
