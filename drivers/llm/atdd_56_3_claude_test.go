package llm

// ATDD coverage for Story 56.3 — claude-cli driver raw capture
// (CAP-1 argv 含 effort 真实值 + stdin = prompt + CAP-2 stdout/stderr/exit_code)。
//
// claude 形态特征：prompt 走 stdin（cmd.StdinPipe + writeStdinSafe），不走
// argv；透传参数 `--effort <v>`（two-token form）。argv 用 effectiveBinary
// 解析的绝对路径而非 cliCommand 字符串。

import (
	"strings"
	"testing"
)

// 56-3-CLAUDE-001 — Call 路径 raw capture（argv + stdin = prompt）
func TestATDD_56_3_CLAUDE001_Call_RawCaptured(t *testing.T) {
	t.Parallel()
	d := NewClaudeCliDriver(
		WithCommandBuilder(mockCmdBuilder("success")),
		// effort 透传 verbose（claude 走 --effort <v>，two-token form）
		WithClaudeEffort("high"),
	)
	f := openLLMFile(t, d, ModeCall)
	defer f.Close()
	writeStringReq(t, f, `{"intent":"summarize","system_prompt":"You are helpful"}`)

	cap := f.LastRawCapture()
	if cap == nil {
		t.Fatal("LastRawCapture() == nil — sink 未注入或 driver 出口未填充")
	}
	if cap.Kind != "cli" {
		t.Errorf("Kind=%q want cli", cap.Kind)
	}

	argv, ok := cap.Request["argv"].([]string)
	if !ok {
		t.Fatalf("Request[argv] not []string: %T", cap.Request["argv"])
	}
	// 必含 --effort high（两 token form）
	foundEffort := false
	for i := 0; i < len(argv)-1; i++ {
		if argv[i] == "--effort" && argv[i+1] == "high" {
			foundEffort = true
			break
		}
	}
	if !foundEffort {
		t.Errorf("--effort high not in argv: %v", argv)
	}
	// 必含 --print -（claude convention）+ --output-format json
	joined := strings.Join(argv, " ")
	if !strings.Contains(joined, "--print -") {
		t.Errorf("--print - missing in argv: %v", argv)
	}
	if !strings.Contains(joined, "--output-format json") {
		t.Errorf("--output-format json missing: %v", argv)
	}

	// stdin 必须含 driver 构造的 prompt（system 指令 + intent）
	stdin, _ := cap.Request["stdin"].(string)
	if !strings.Contains(stdin, "summarize") {
		t.Errorf("stdin missing intent: %q", stdin)
	}

	// Response: stdout 含 success result（fixture）
	stdout, _ := cap.Response["stdout"].(string)
	if !strings.Contains(stdout, `"result":"test output"`) {
		t.Errorf("stdout missing result body: %q", stdout)
	}
	if _, ok := cap.Response["stderr"].(string); !ok {
		t.Fatalf("stderr type wrong: %T", cap.Response["stderr"])
	}
	exit, _ := cap.Response["exit_code"].(int)
	if exit != 0 {
		t.Errorf("exit_code=%d want 0", exit)
	}
}

// 56-3-CLAUDE-002 — Stream 路径（tee stdoutPipe 累积原样 stream-json）
func TestATDD_56_3_CLAUDE002_Stream_RawCaptured(t *testing.T) {
	t.Parallel()
	d := NewClaudeCliDriver(
		WithCommandBuilder(mockCmdBuilder("stream_success")),
	)
	f := openLLMFile(t, d, ModeStream)
	defer f.Close()
	writeStringReq(t, f, `{"intent":"hi"}`)

	cap := f.LastRawCapture()
	if cap == nil {
		t.Fatal("LastRawCapture() == nil for Stream path")
	}
	argv, _ := cap.Request["argv"].([]string)
	joined := strings.Join(argv, " ")
	if !strings.Contains(joined, "--output-format stream-json") {
		t.Errorf("stream output-format missing: %v", argv)
	}

	// 原样 stream-json 字节必须含 result line
	stdout, _ := cap.Response["stdout"].(string)
	if !strings.Contains(stdout, `"result":"hello world"`) {
		t.Errorf("stream stdout missing raw result line: %q", stdout)
	}
	if !strings.Contains(stdout, `"type":"assistant"`) {
		t.Errorf("stream stdout missing assistant raw event: %q", stdout)
	}
}
