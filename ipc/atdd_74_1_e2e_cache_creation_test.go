package ipc

// E2E for Story 74.1 — 单步 cache creation 全链路闭合（FR1 落盘 + FR2 显示数据源）。
//
// 与既有单层 ATDD 的差异（本文件的独立价值）：
//   - drivers/llm atdd_74_1：driver 层取值 + vfsfile 桥（无 kernel，无 IPC）
//   - kernel atdd_74_1 KRN-003：kernel 落盘（sequenceLLMFile 直喂 JSON，无真实
//     driver stream；真实 stream 路径的 vfsfile 桥从未在 kernel 层被驱动过）
//   - ipc atdd_27_2：wire 序列化 + 手工写盘的 get_step_detail（testStepRecord 无
//     creation 字段 —— server_observe.go 的 CacheCreationInputTokens 映射从未被
//     真实数据驱动过）
//
// 本文件用真实 claude-cli driver（mock CommandBuilder 自包含 helper 进程，走真实
// stream 事件 → vfsfile 桥）驱动真实 kernel spawn → 真实 steps.jsonl → 真实 IPC
// 客户端 get_step_detail，一次钉住 FR1 与 FR2 的请求粒度闭合；并补上旧数据（无
// creation 字段）经 IPC 映射读回 0 的 NFR5 兼容断言。

import (
	gocontext "context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/drivers/llm"
	"github.com/rnixai/rnix/internal/config"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/kernel"
	"github.com/rnixai/rnix/vfs"
)

// 项目目录用真实 temp 目录（非假常量）：kernel 会把 ProjectConfig.ProjectDir
// 线程进 LLMRequest，claude-cli driver 的 configureCommandDir 会把它设为子进程
// cmd.Dir —— 不存在的目录会导致 helper 进程 chdir ENOENT。数据布局手工复刻
// kernel.TestSetupDataDir（dataDir + config.ProjectDataDir）。

// TestHelperProcessE2E741 emits a claude stream-json session whose assistant
// frame carries message.usage WITH cache_creation_input_tokens (mid-stream
// usage event source) and whose result frame carries usage with creation too
// (done event source — the authoritative value that must reach steps.jsonl).
// Self-contained helper, isolated env guard GO_TEST_PROCESS_E2E_741=1 — does
// not touch the shared TestHelperProcess switch (Story 66.6 convention).
func TestHelperProcessE2E741(t *testing.T) {
	if os.Getenv("GO_TEST_PROCESS_E2E_741") != "1" {
		return
	}
	lines := []string{
		// main-thread assistant WITH per-round-trip usage incl. creation=15.
		`{"type":"assistant","message":{"id":"msg_e2e","content":[{"type":"text","text":"working"}],"usage":{"input_tokens":100,"cache_read_input_tokens":40,"cache_creation_input_tokens":15,"output_tokens":30}}}`,
		// result → done event, session-total usage incl. creation=25 (authoritative).
		`{"type":"result","subtype":"success","result":"done","is_error":false,"usage":{"input_tokens":200,"cache_read_input_tokens":40,"cache_creation_input_tokens":25,"output_tokens":50}}`,
	}
	for _, l := range lines {
		_, _ = os.Stdout.WriteString(l + "\n")
	}
	os.Exit(0)
}

func e2e741CmdBuilder() llm.CommandBuilder {
	return func(ctx gocontext.Context, _ string, args ...string) *exec.Cmd {
		cs := append([]string{"-test.run=TestHelperProcessE2E741", "--"}, args...)
		cmd := exec.CommandContext(ctx, os.Args[0], cs...)
		cmd.Env = append(os.Environ(), "GO_TEST_PROCESS_E2E_741=1")
		return cmd
	}
}

// newE2E741Harness builds a kernel + IPC server whose /dev/llm/claude is a
// REAL claude-cli driver (mock command builder) — the same registration shape
// production uses (llm.FileFactory). Returns the server, socket path, the real
// project dir and the per-project data base dir.
func newE2E741Harness(t *testing.T) (*Server, string, string, string) {
	t.Helper()

	drv := llm.NewClaudeCliDriver(llm.WithCommandBuilder(e2e741CmdBuilder()))
	devReg := vfs.NewDeviceRegistry()
	if err := devReg.Register("/dev/llm/claude", llm.FileFactory(drv, "/dev/llm/claude", "")); err != nil {
		t.Fatalf("register /dev/llm/claude: %v", err)
	}
	vfsInst := vfs.NewVFS(devReg)
	ctxMgr := rnixctx.NewManager()

	srv := NewServer(nil, nil, "0.1.0-test", "", "")
	kern := kernel.NewKernel(vfsInst, ctxMgr, srv.CallbackMux())
	srv.kern = kern
	srv.SetContextManager(ctxMgr)

	dataDir := t.TempDir()
	kern.SetDataDir(dataDir)
	projDir := t.TempDir()
	projBase := config.ProjectDataDir(dataDir, projDir)
	if err := os.MkdirAll(projBase, 0o755); err != nil {
		t.Fatalf("mkdir proj base: %v", err)
	}

	sockDir := t.TempDir()
	sockPath := filepath.Join(sockDir, "test.sock")
	if err := srv.ListenAndServe(sockPath); err != nil {
		t.Fatalf("ListenAndServe: %v", err)
	}
	t.Cleanup(func() {
		srv.Shutdown()
		srv.Wait()
		kern.Shutdown()
	})
	return srv, sockPath, projDir, projBase
}

// e2e741Spawn spawns a process against the harness kernel and waits for it to
// reach a terminal state. Returns (pid, proc).
func e2e741Spawn(t *testing.T, srv *Server, projDir, intent string) (types.PID, *kernel.Process) {
	t.Helper()
	pid, err := srv.kern.Spawn(intent, nil, kernel.SpawnOpts{
		ProjectConfig: &config.ProjectConfig{ProjectDir: projDir},
	})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	proc, ok := srv.kern.GetProcess(pid)
	if !ok {
		t.Fatalf("process %d not found after spawn", pid)
	}
	select {
	case exit := <-proc.Done:
		if exit.Code != 0 {
			t.Fatalf("exit code %d: %s", exit.Code, exit.Reason)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("timed out waiting for process completion")
	}
	return pid, proc
}

// -----------------------------------------------------------------------------
// 74-1-E2E-001 (FR1+FR2): 全链路 — 真实 claude-cli stream（含 creation）经
// vfsfile 桥 → kernel observe 落 steps.jsonl → IPC get_step_detail 带出
// CacheCreationInputTokens。两条断言互相印证同一份数据（磁盘字节 vs wire）。
// -----------------------------------------------------------------------------
func TestATDD_74_1_E2E_001_FullChainStreamClosure(t *testing.T) {
	srv, sockPath, projDir, projBase := newE2E741Harness(t)

	pid, proc := e2e741Spawn(t, srv, projDir, "74-1 e2e cache creation")

	// FR1: steps.jsonl 逐步落盘 — 真实 stream 路径经 vfsfile 桥的 done 事件
	// （creation=25）必须到达 StepRecord（KRN-003 只覆盖直喂 JSON 的路径）。
	stepsFile := filepath.Join(projBase, "steps", proc.UUID, "steps.jsonl")
	data, err := os.ReadFile(stepsFile)
	if err != nil {
		t.Fatalf("ReadFile steps.jsonl: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) < 1 {
		t.Fatal("expected at least 1 StepRecord")
	}
	var rec types.StepRecord
	if err := json.Unmarshal([]byte(lines[0]), &rec); err != nil {
		t.Fatalf("Unmarshal StepRecord: %v", err)
	}
	if rec.CacheCreationInputTokens != 25 {
		t.Errorf("steps.jsonl CacheCreationInputTokens = %d, want 25 (vfsfile 桥 done 事件)", rec.CacheCreationInputTokens)
	}
	if rec.CachedInputTokens != 40 {
		t.Errorf("steps.jsonl CachedInputTokens = %d, want 40", rec.CachedInputTokens)
	}
	if rec.InputTokens != 200 || rec.OutputTokens != 50 {
		t.Errorf("steps.jsonl input/output = %d/%d, want 200/50", rec.InputTokens, rec.OutputTokens)
	}

	// FR2: IPC get_step_detail 映射 — server_observe.go 的
	// CacheCreationInputTokens 映射被真实数据驱动（atdd_27_2 的 testStepRecord
	// 从未带 creation，此映射此前无真实数据覆盖）。
	client, err := DialTimeout(sockPath, 3*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	detail, err := client.GetStepDetail(pid, 1)
	if err != nil {
		t.Fatalf("GetStepDetail: %v", err)
	}
	if detail.CacheCreationInputTokens != 25 {
		t.Errorf("get_step_detail CacheCreationInputTokens = %d, want 25 (server 映射)", detail.CacheCreationInputTokens)
	}
	if detail.CachedInputTokens != 40 {
		t.Errorf("get_step_detail CachedInputTokens = %d, want 40", detail.CachedInputTokens)
	}
	if detail.InputTokens != 200 || detail.OutputTokens != 50 {
		t.Errorf("get_step_detail input/output = %d/%d, want 200/50", detail.InputTokens, detail.OutputTokens)
	}
	// TokensUsed 语义不变（AC1-5）：creation 不并入合计。StepRecord 的
	// TokenCount = Input + Output（writeStepRecord 落盘形态）。
	if rec.TokenCount != 250 {
		t.Errorf("StepRecord.TokenCount = %d, want 250 (input 200 + output 50, creation 不并入)", rec.TokenCount)
	}
}

// -----------------------------------------------------------------------------
// 74-1-E2E-002 (NFR5): 旧数据兼容 — 无 cache_creation_input_tokens 字段的
// steps.jsonl 经 IPC get_step_detail 读回 CacheCreationInputTokens == 0（omitempty
// 天然保证，钉住 IPC 映射层的旧数据路径；KRN-002 只钉 JSON 解码层）。
// -----------------------------------------------------------------------------
func TestATDD_74_1_E2E_002_LegacyRecordZeroThroughIPC(t *testing.T) {
	srv, sockPath, projDir, projBase := newE2E741Harness(t)

	proc := kernel.NewProcess(0, "legacy 74-1 record", nil)
	// ProjectConfig 在 Start() 之前就位——与生产 spawn 形态一致（review
	// 修复：原版 Start 后才赋值，绕过真实解析路径的初始化顺序）。
	proc.ProjectConfig = &config.ProjectConfig{ProjectDir: projDir}
	_ = proc.Start()
	legacy := types.StepRecord{
		Step:              1,
		Timestamp:         time.Second,
		Action:            "text",
		Summary:           "legacy step without creation",
		MessageCount:      1,
		TokenCount:        250,
		RequestTokens:     200,
		ResponseTokens:    50,
		InputTokens:       200,
		OutputTokens:      50,
		CachedInputTokens: 40,
		// 无 CacheCreationInputTokens —— 旧格式
	}
	writeTestStepsUUID(t, projBase, proc.UUID, []types.StepRecord{legacy})
	srv.kern.AddProcess(proc)

	client, err := DialTimeout(sockPath, 3*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer client.Close()

	detail, err := client.GetStepDetail(proc.PID, 1)
	if err != nil {
		t.Fatalf("GetStepDetail: %v", err)
	}
	if detail.CacheCreationInputTokens != 0 {
		t.Errorf("legacy get_step_detail CacheCreationInputTokens = %d, want 0 (旧数据无字段)", detail.CacheCreationInputTokens)
	}
	if detail.CachedInputTokens != 40 {
		t.Errorf("legacy get_step_detail CachedInputTokens = %d, want 40 (既有字段不受影响)", detail.CachedInputTokens)
	}
	if detail.InputTokens != 200 || detail.OutputTokens != 50 {
		t.Errorf("legacy get_step_detail input/output = %d/%d, want 200/50", detail.InputTokens, detail.OutputTokens)
	}
}
