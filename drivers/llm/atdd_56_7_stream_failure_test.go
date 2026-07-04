package llm

// ============================================================================
// ATDD Story 56.7 — Stream 失败路径 sink 归集 + stderr 透传（drivers/llm 侧）
//
//   - AC4: writeStream 的 error 事件路径与 ErrStreamIncomplete 路径都归集
//     sink → LLMFile.LastRawCapture()；drain-to-close 拿到的是 driver 在
//     「error 事件之后、close(ch) 之前」set 的终态 capture（56.3 时序铁律）。
//   - AC5: codex / cursor Stream 无 result 退出时 error 消息含 stderr tail 与
//     exit code（exit 非零与零两种）；stderr 超 2KB 只带尾部 ≤2KB（全量在
//     raw capture）；ErrStreamIncomplete 包装错误在 capture 有 stderr 时追加。
//
// helper-process 模式复用 56.3（cmdBuilder 注入 re-exec 自身），并按
// commit a7826b6 先例注入 GOCOVERDIR 防 -cover 下 runtime warning 污染 stderr。
// ============================================================================

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/rnixai/rnix/vfs"
)

// --- AC4: writeStream drain-to-close 归集（fake driver 直击 vfsfile） ---

// drainOrderDriver 模拟 CLI driver 的失败时序：先 set 一个「仅 Request」的
// 过早态 capture → 发 error 事件 → set 终态 capture（含 Response）→ close(ch)。
// 旧实现（error 事件立即 return）拿不到任何 capture；drain-to-close 必须拿到
// 终态（Response 完整）。
type drainOrderDriver struct {
	extraAfterError []StreamEvent // error 事件后追加发送的事件（验证 drain 忽略语义）
}

func (d *drainOrderDriver) Call(_ context.Context, _ LLMRequest) (*LLMResponse, error) {
	return nil, errors.New("not used")
}

func (d *drainOrderDriver) Stream(ctx context.Context, _ LLMRequest) (<-chan StreamEvent, error) {
	sink := rawSinkFromContext(ctx)
	ch := make(chan StreamEvent, 8)
	go func() {
		defer close(ch)
		early := newCLIRawCapture()
		fillCLIRequest(early, "fake-cli", []string{"--flag"}, "the prompt", nil)
		if sink != nil {
			sink.set(early)
		}
		ch <- StreamEvent{Type: "error", Err: NewLLMError("fake", 0, errors.New("stream exploded"))}
		for _, evt := range d.extraAfterError {
			ch <- evt
		}
		// 56.3 时序铁律：sink 终态 set 在 close(ch) 之前 —— error 事件已发出，
		// 提前 return 的消费方只能看到 early（甚至 nil）。
		terminal := newCLIRawCapture()
		fillCLIRequest(terminal, "fake-cli", []string{"--flag"}, "the prompt", nil)
		fillCLIResponse(terminal, "partial stdout", "fatal: device on fire", 2)
		if sink != nil {
			sink.set(terminal)
		}
	}()
	return ch, nil
}

func (d *drainOrderDriver) Info() DriverInfo {
	return DriverInfo{Name: "fake", Provider: "fake", DefaultModel: "fake-1"}
}

func TestATDD_56_7_AC4_StreamErrorEvent_SinkCollectedAtTerminalState(t *testing.T) {
	t.Parallel()
	d := &drainOrderDriver{
		// 错误已定局后 driver 还可能吐 content/done——drain 必须忽略而非回填
		extraAfterError: []StreamEvent{
			{Type: "content", Content: "late content"},
			{Type: "done", Content: "late done"},
		},
	}
	f := openLLMFile(t, d, ModeStream)
	defer f.Close()

	err := f.Write(context.Background(), []byte(`{"intent":"test"}`))
	if err == nil {
		t.Fatal("expected error from stream error event")
	}
	if !strings.Contains(err.Error(), "stream exploded") {
		t.Errorf("returned error must be the FIRST error event's Err, got: %v", err)
	}

	cap := f.LastRawCapture()
	if cap == nil {
		t.Fatal("LastRawCapture() == nil — error path did not collect the sink (G1)")
	}
	// 必须是终态 capture（Response 已填），不是 early（仅 Request）
	if cap.Response == nil {
		t.Fatalf("collected capture has nil Response — got the premature state, want terminal (drain-to-close): %+v", cap)
	}
	if stderr, _ := cap.Response["stderr"].(string); stderr != "fatal: device on fire" {
		t.Errorf("Response[stderr] = %v, want terminal capture's stderr", cap.Response["stderr"])
	}
	// Request 形态保留（对齐 writeCall 失败路径语义）
	if cap.Request == nil {
		t.Error("Request missing from collected capture")
	}
}

// silentIncompleteDriver: 不发 done、不发 content、不发 error——直接 set 带
// stderr 的终态 capture 后 close(ch)，触发 ErrStreamIncomplete 路径。
type silentIncompleteDriver struct {
	stderr   string
	exitCode int
	noSink   bool // true 时不 set 任何 capture（验证无 capture 时消息无诊断后缀）
}

func (d *silentIncompleteDriver) Call(_ context.Context, _ LLMRequest) (*LLMResponse, error) {
	return nil, errors.New("not used")
}

func (d *silentIncompleteDriver) Stream(ctx context.Context, _ LLMRequest) (<-chan StreamEvent, error) {
	sink := rawSinkFromContext(ctx)
	ch := make(chan StreamEvent, 2)
	go func() {
		defer close(ch)
		ch <- StreamEvent{Type: "system", Content: "init"}
		if sink != nil && !d.noSink {
			cap := newCLIRawCapture()
			fillCLIRequest(cap, "fake-cli", []string{"--flag"}, "prompt", nil)
			fillCLIResponse(cap, "", d.stderr, d.exitCode)
			sink.set(cap)
		}
	}()
	return ch, nil
}

func (d *silentIncompleteDriver) Info() DriverInfo {
	return DriverInfo{Name: "fake", Provider: "fake", DefaultModel: "fake-1"}
}

func TestATDD_56_7_AC4_StreamIncomplete_SinkCollectedAndDiagAppended(t *testing.T) {
	t.Parallel()
	d := &silentIncompleteDriver{stderr: "connection dropped mid-flight", exitCode: 1}
	f := openLLMFile(t, d, ModeStream)
	defer f.Close()

	err := f.Write(context.Background(), []byte(`{"intent":"test"}`))
	if err == nil {
		t.Fatal("expected ErrStreamIncomplete")
	}
	if !errors.Is(err, ErrStreamIncomplete) {
		t.Fatalf("errors.Is(err, ErrStreamIncomplete) = false: %v", err)
	}
	// AC4: sink 已归集（G2 — 归集在 ErrStreamIncomplete guard 之前）
	cap := f.LastRawCapture()
	if cap == nil {
		t.Fatal("LastRawCapture() == nil on ErrStreamIncomplete path (G2)")
	}
	// AC5 后半 / G7: 错误消息追加 capture 中的 stderr tail + exit code
	if !strings.Contains(err.Error(), "connection dropped mid-flight") {
		t.Errorf("ErrStreamIncomplete message must carry stderr tail, got: %v", err)
	}
	if !strings.Contains(err.Error(), "exit 1") {
		t.Errorf("ErrStreamIncomplete message must carry exit code, got: %v", err)
	}
}

func TestATDD_56_7_AC4_StreamIncomplete_NoCaptureNoDiag(t *testing.T) {
	t.Parallel()
	d := &silentIncompleteDriver{noSink: true}
	f := openLLMFile(t, d, ModeStream)
	defer f.Close()

	err := f.Write(context.Background(), []byte(`{"intent":"test"}`))
	if !errors.Is(err, ErrStreamIncomplete) {
		t.Fatalf("want ErrStreamIncomplete, got: %v", err)
	}
	if strings.Contains(err.Error(), "stderr") {
		t.Errorf("no capture → no diag suffix expected, got: %v", err)
	}
}

func TestATDD_56_7_AC4_WriteClearsStaleRawCaptureOnNoCaptureFailure(t *testing.T) {
	t.Parallel()
	d := &silentIncompleteDriver{noSink: true}
	f := openLLMFile(t, d, ModeStream)
	defer f.Close()
	f.lastRawCapture = &vfs.RawCapture{
		TsMs:    1,
		Step:    41,
		Kind:    "cli",
		Request: map[string]any{"argv": []string{"old-call"}},
	}

	err := f.Write(context.Background(), []byte(`{"intent":"test"}`))
	if !errors.Is(err, ErrStreamIncomplete) {
		t.Fatalf("want ErrStreamIncomplete, got: %v", err)
	}
	if got := f.LastRawCapture(); got != nil {
		t.Fatalf("stale LastRawCapture survived a no-capture failure: %+v", got)
	}
}

// --- AC5: codex / cursor stderr 透传（helper-process 模拟 CLI） ---

// TestHelperProcess567 按 56.6 先例独立开关（不污染既有 helper switch）。
// GO_TEST_STDERR_REPEAT 控制 stderr 重复次数（>2KB 场景）。
func TestHelperProcess567(t *testing.T) {
	if os.Getenv("GO_TEST_PROCESS_567") != "1" {
		return
	}
	stderrMsg := os.Getenv("GO_TEST_STDERR_567")
	switch os.Getenv("GO_TEST_CASE_567") {
	case "codex_no_agent_message_exit2":
		fmt.Fprintln(os.Stdout, `{"type":"thread.started","thread_id":"tid-567"}`)
		fmt.Fprintln(os.Stdout, `{"type":"turn.completed","usage":{"input_tokens":10,"output_tokens":0}}`)
		fmt.Fprint(os.Stderr, stderrMsg)
		os.Exit(2)
	case "codex_no_agent_message_exit0":
		fmt.Fprintln(os.Stdout, `{"type":"thread.started","thread_id":"tid-567"}`)
		fmt.Fprintln(os.Stdout, `{"type":"turn.completed","usage":{"input_tokens":10,"output_tokens":0}}`)
		fmt.Fprint(os.Stderr, stderrMsg)
		os.Exit(0)
	case "codex_scanner_err_exit3":
		fmt.Fprint(os.Stdout, strings.Repeat("X", scannerMaxSize+1))
		fmt.Fprint(os.Stderr, stderrMsg)
		os.Exit(3)
	case "cursor_no_result_exit1":
		fmt.Fprintln(os.Stdout, `{"type":"result","subtype":"success","result":"","is_error":false}`)
		fmt.Fprint(os.Stderr, stderrMsg)
		os.Exit(1)
	case "cursor_no_result_exit0":
		fmt.Fprintln(os.Stdout, `{"type":"result","subtype":"success","result":"","is_error":false}`)
		fmt.Fprint(os.Stderr, stderrMsg)
		os.Exit(0)
	}
	os.Exit(0)
}

// mock567CmdBuilder re-exec 自身跑 TestHelperProcess567。GOCOVERDIR 注入防
// -cover 下 "warning: GOCOVERDIR not set" 污染 stderr 断言（a7826b6 先例）。
func mock567CmdBuilder(t *testing.T, testCase, stderrMsg string) CommandBuilder {
	t.Helper()
	coverDir := t.TempDir()
	return func(ctx context.Context, _ string, args ...string) *exec.Cmd {
		cs := append([]string{"-test.run=TestHelperProcess567", "--"}, args...)
		cmd := exec.CommandContext(ctx, os.Args[0], cs...)
		cmd.Env = append(os.Environ(),
			"GO_TEST_PROCESS_567=1",
			"GO_TEST_CASE_567="+testCase,
			"GO_TEST_STDERR_567="+stderrMsg,
			"GOCOVERDIR="+coverDir,
		)
		return cmd
	}
}

// collectStreamError drain 整个 channel 并返回首个 error 事件的 Err。
func collectStreamError(t *testing.T, ch <-chan StreamEvent) error {
	t.Helper()
	var first error
	for evt := range ch {
		if evt.Type == "error" && evt.Err != nil && first == nil {
			first = evt.Err
		}
	}
	return first
}

func TestATDD_56_7_AC5_Codex_NoAgentMessage_StderrAndExitInError(t *testing.T) {
	t.Parallel()
	const stderrMsg = "codex quota exhausted for org"
	d := NewCodexCliDriver(CodexWithCommandBuilder(mock567CmdBuilder(t, "codex_no_agent_message_exit2", stderrMsg)))
	ch, err := d.Stream(context.Background(), LLMRequest{Intent: "test"})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	streamErr := collectStreamError(t, ch)
	if streamErr == nil {
		t.Fatal("expected error event for no-agent-message")
	}
	msg := streamErr.Error()
	if !strings.Contains(msg, stderrMsg) {
		t.Errorf("error message missing stderr tail: %q", msg)
	}
	if !strings.Contains(msg, "exit 2") {
		t.Errorf("error message missing exit code: %q", msg)
	}
}

func TestATDD_56_7_AC5_Codex_NoAgentMessage_ExitZero_StillCarriesExitAndStderr(t *testing.T) {
	t.Parallel()
	const stderrMsg = "warning: model deprecated"
	d := NewCodexCliDriver(CodexWithCommandBuilder(mock567CmdBuilder(t, "codex_no_agent_message_exit0", stderrMsg)))
	ch, err := d.Stream(context.Background(), LLMRequest{Intent: "test"})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	streamErr := collectStreamError(t, ch)
	if streamErr == nil {
		t.Fatal("expected error event")
	}
	msg := streamErr.Error()
	if !strings.Contains(msg, "exit 0") {
		t.Errorf("exit-zero failure must still carry exit code: %q", msg)
	}
	if !strings.Contains(msg, stderrMsg) {
		t.Errorf("error message missing stderr: %q", msg)
	}
}

func TestATDD_56_7_AC5_Codex_OversizeStderr_OnlyTailInMessage_FullInCapture(t *testing.T) {
	t.Parallel()
	// >2KB stderr：头部 3000 个 'A' + 独特尾部标记
	const tailMarker = "FINAL-CAUSE: token refresh failed"
	bigStderr := strings.Repeat("A", 3000) + tailMarker

	d := NewCodexCliDriver(CodexWithCommandBuilder(mock567CmdBuilder(t, "codex_no_agent_message_exit2", bigStderr)))
	f := openLLMFile(t, d, ModeStream)
	defer f.Close()

	err := f.Write(context.Background(), []byte(`{"intent":"test"}`))
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, tailMarker) {
		t.Errorf("tail marker missing from error message: %q", msg)
	}
	// 消息内联的 stderr ≤ 2KB：3000 个 'A' 的头部必然被截掉
	if strings.Contains(msg, strings.Repeat("A", 2049)) {
		t.Errorf("error message carries >2KB stderr — tail cap violated (len=%d)", len(msg))
	}
	// 全量 stderr 仍在 raw capture（经 writeStream 失败路径归集，G1）
	cap := f.LastRawCapture()
	if cap == nil {
		t.Fatal("LastRawCapture() == nil — codex Stream failure must still collect sink")
	}
	fullStderr, _ := cap.Response["stderr"].(string)
	if !strings.Contains(fullStderr, strings.Repeat("A", 3000)) {
		t.Errorf("raw capture must keep FULL stderr (len=%d)", len(fullStderr))
	}
}

func TestATDD_56_7_AC5_Codex_ScannerErr_StderrAndExitInError(t *testing.T) {
	t.Parallel()
	const stderrMsg = "codex stdout frame exceeded scanner limit"
	d := NewCodexCliDriver(CodexWithCommandBuilder(mock567CmdBuilder(t, "codex_scanner_err_exit3", stderrMsg)))
	ch, err := d.Stream(context.Background(), LLMRequest{Intent: "test"})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	streamErr := collectStreamError(t, ch)
	if streamErr == nil {
		t.Fatal("expected scanner error event")
	}
	msg := streamErr.Error()
	if !strings.Contains(msg, "stream read error") {
		t.Errorf("base scanner error missing: %q", msg)
	}
	if !strings.Contains(msg, stderrMsg) {
		t.Errorf("error message missing stderr tail: %q", msg)
	}
	if !strings.Contains(msg, "exit 3") {
		t.Errorf("error message missing exit code: %q", msg)
	}
}

func TestATDD_56_7_AC5_Cursor_NoResult_StderrAndExitInError(t *testing.T) {
	t.Parallel()
	const stderrMsg = "cursor session expired"
	d := NewCursorCliDriver(CursorWithCommandBuilder(mock567CmdBuilder(t, "cursor_no_result_exit1", stderrMsg)))
	ch, err := d.Stream(context.Background(), LLMRequest{Intent: "test"})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	streamErr := collectStreamError(t, ch)
	if streamErr == nil {
		t.Fatal("expected error event for no-result")
	}
	msg := streamErr.Error()
	if !strings.Contains(msg, "response truncated: no result") {
		t.Errorf("base message drifted: %q", msg)
	}
	if !strings.Contains(msg, stderrMsg) {
		t.Errorf("error message missing stderr tail: %q", msg)
	}
	if !strings.Contains(msg, "exit 1") {
		t.Errorf("error message missing exit code: %q", msg)
	}
}

func TestATDD_56_7_AC5_Cursor_NoResult_ExitZero_CarriesExitCode(t *testing.T) {
	t.Parallel()
	const stderrMsg = "hint: empty completion returned"
	d := NewCursorCliDriver(CursorWithCommandBuilder(mock567CmdBuilder(t, "cursor_no_result_exit0", stderrMsg)))
	ch, err := d.Stream(context.Background(), LLMRequest{Intent: "test"})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	streamErr := collectStreamError(t, ch)
	if streamErr == nil {
		t.Fatal("expected error event")
	}
	msg := streamErr.Error()
	if !strings.Contains(msg, "exit 0") || !strings.Contains(msg, stderrMsg) {
		t.Errorf("exit-zero no-result must carry exit code + stderr: %q", msg)
	}
}

// --- stderrTail 纯函数边界（UTF-8 安全） ---

func TestATDD_56_7_StderrTail_UTF8BoundarySafe(t *testing.T) {
	t.Parallel()
	// 全 CJK（每字 3 字节）：截断点若落在 rune 中间必须前进到边界
	s := strings.Repeat("错", 1000) // 3000 bytes
	tail := stderrTail(s, 2048)
	if len(tail) > 2048 {
		t.Errorf("tail length %d > max 2048", len(tail))
	}
	for _, r := range tail {
		if r != '错' {
			t.Errorf("mangled rune %q in tail — UTF-8 boundary violated", r)
			break
		}
	}
	// 短输入原样返回（trim 后）
	if got := stderrTail("  short  ", 2048); got != "short" {
		t.Errorf("stderrTail short = %q", got)
	}
	// 空输入
	if got := stderrTail("", 2048); got != "" {
		t.Errorf("stderrTail empty = %q", got)
	}
}

// vfs.RawCapture 新字段 JSON 合约（snake_case + omitempty）。
func TestATDD_56_7_RawCapture_OutcomeFieldsContract(t *testing.T) {
	t.Parallel()
	rec := vfs.RawCapture{TsMs: 1, Step: 1, Kind: "cli", Outcome: "error", Error: "boom"}
	data, err := json.Marshal(rec)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, `"outcome":"error"`) || !strings.Contains(s, `"error":"boom"`) {
		t.Errorf("snake_case outcome/error tags missing: %s", s)
	}
}
