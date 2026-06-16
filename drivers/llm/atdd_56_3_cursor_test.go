package llm

// ATDD coverage for Story 56.3 — cursor-cli driver raw capture
// (CAP-1 argv + stdin + CAP-2 stdout/stderr/exit_code)。
//
// cursor 形态特征：prompt 走 argv 末尾（buildArgs 末尾 append）；effort
// 不支持（Cursor thinking 绑 model 名后缀），故 ATDD 不验 effort，仅验
// argv / stdin / stdout / stderr / exit_code 全链路捕获。

import (
	"strings"
	"testing"
)

// 56-3-CURSOR-001 — Call 路径 raw capture
func TestATDD_56_3_CURSOR001_Call_RawCaptured(t *testing.T) {
	t.Parallel()
	d := NewCursorCliDriver(CursorWithCommandBuilder(cursorMockCmdBuilder("cursor_success")))
	f := openLLMFile(t, d, ModeCall)
	defer f.Close()
	writeStringReq(t, f, `{"intent":"hi-cursor"}`)

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
	// argv[0] 应该是 cliCommand "cursor-agent"（或 fallback "cursor"），不空
	if argv[0] == "" {
		t.Errorf("argv[0] empty: %v", argv)
	}
	// cursor prompt 在 argv 里 → stdin=""
	stdin, _ := cap.Request["stdin"].(string)
	if stdin != "" {
		t.Errorf("cursor stdin should be empty (prompt in argv), got %q", stdin)
	}

	stdout, _ := cap.Response["stdout"].(string)
	if !strings.Contains(stdout, `"result":"cursor output"`) {
		t.Errorf("stdout missing fixture payload: %q", stdout)
	}
	if _, ok := cap.Response["stderr"].(string); !ok {
		t.Fatalf("stderr type wrong: %T", cap.Response["stderr"])
	}
	exit, _ := cap.Response["exit_code"].(int)
	if exit != 0 {
		t.Errorf("exit_code=%d want 0", exit)
	}
}

// 56-3-CURSOR-002 — Stream 路径 raw capture
func TestATDD_56_3_CURSOR002_Stream_RawCaptured(t *testing.T) {
	t.Parallel()
	d := NewCursorCliDriver(CursorWithCommandBuilder(cursorMockCmdBuilder("cursor_stream_success")))
	f := openLLMFile(t, d, ModeStream)
	defer f.Close()
	writeStringReq(t, f, `{"intent":"hi"}`)

	cap := f.LastRawCapture()
	if cap == nil {
		t.Fatal("LastRawCapture() == nil for Stream path")
	}
	stdout, _ := cap.Response["stdout"].(string)
	if !strings.Contains(stdout, `"result":"hello world"`) {
		t.Errorf("stream stdout missing raw result: %q", stdout)
	}
	if !strings.Contains(stdout, `"type":"assistant"`) {
		t.Errorf("stream stdout missing assistant raw event: %q", stdout)
	}
}
