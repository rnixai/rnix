package ipc

import (
	gocontext "context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rnixai/rnix/agents"
	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/drivers/llm"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/kernel"
	"github.com/rnixai/rnix/vfs"
)

// =============================================================================
// E2E — Story 73.4: 限流在进程账面与 wire 上可见（FR8 + NFR3 通道②收口）
//
// 为什么需要这一层（相对 story 已有的 9 个 ATDD 测试的增量）:
//
//	kernel/atdd_73_4_visibility_test.go  用 flakyRawLLMFile / readFailRateLimitLLM
//	                                   （raw VFSFile 直接返回 typed error）——
//	                                   **绕过 vfsfile.writeStream**, 且断言读
//	                                   **磁盘 events.jsonl**（readDiskEvents）。
//	ipc/atdd_73_4_resume_at_wire_test.go  直接调 ProcInfoToWire / WireToProcInfo
//	                                   —— **手工构造 vfs.ProcInfo**, 不经 kernel。
//
// 两层之间的接缝（driver error → 真 writeStream → 真 kernel 分类 → 终态事件 /
// proc-info → IPC wire → 编排器消费面）此前无任何测试。73.4 的 user story 主体
// 正是"As a 排查进程死因的运维者"——编排器 / `rnix ps` 消费的是 MethodWait 的
// exit_reason 与 list_all_procs 的 wire 字段。本文件把这条接缝焊死, 与 66.2
// （writeStream partial） / 66.6（writeStream usage）同构。
//
// 搭建取舍（对齐 atdd_66_2_interrupted_e2e_test.go）:
//   - LLM 设备经 **llm.FileFactory 注册真实 LLMFile** —— writeStream 是生产代码,
//     只有底层 driver 是 mock。
//   - Spawn 走 kern.Spawn(ProjectConfig) —— proc-info.json / events.jsonl 落盘
//     到测试已知的 baseDir。
//   - 所有读回（MethodWait / MethodListAllProcs / MethodListEvents）一律走真
//     Unix socket 上的 Client —— 编排器真实消费 exit_reason / suspend_reason /
//     resume_at_ms 的路径。
// =============================================================================

// 73.4 固件 provenance 纪律（§5）: 限流文案逐字复用 73.1 实捕的 qwen 样本
// （kernelThrottleBody / kernelQuotaBody 在 kernel 包同名同文）, 不自造。
const (
	e2eThrottleBody = "Rate limit exceeded (5 requests per minute). Please try again after 6 seconds."
	e2eQuotaBody    = "Your token-plan 1-week quota has been exhausted. The quota will reset at 08-05 22:23:00 UTC."
)

// e2eFailStreamDriver 是一个真实实现 llm.LLMDriver 的 mock: Stream 立即 emit
// 一个 error 事件（携带 failErr）后关闭 channel。真 writeStream 的
// `case "error"`（vfsfile.go:329-333）把它变成 Write 的返回错误 —— 与真实
// CLI/API driver 的失败形状同构, 且不绕过 writeStream。
type e2eFailStreamDriver struct {
	failErr error
}

func (d *e2eFailStreamDriver) Call(_ gocontext.Context, _ llm.LLMRequest) (*llm.LLMResponse, error) {
	return nil, fmt.Errorf("e2eFailStreamDriver: Call not used (stream mode)")
}

func (d *e2eFailStreamDriver) Info() llm.DriverInfo {
	return llm.DriverInfo{
		Name:         "e2e-rl",
		Provider:     "test",
		DefaultModel: "mock-model",
		DriverType:   "mock",
	}
}

func (d *e2eFailStreamDriver) Stream(_ gocontext.Context, _ llm.LLMRequest) (<-chan llm.StreamEvent, error) {
	ch := make(chan llm.StreamEvent, 1)
	ch <- llm.StreamEvent{Type: "error", Err: d.failErr}
	close(ch)
	return ch, nil
}

// e2eDoneStreamDriver 是正常完成对照: Stream emit 一个 done 事件后关闭 ——
// 66.2 的 NormalCompletion 形状。
type e2eDoneStreamDriver struct {
	doneContent string
}

func (d *e2eDoneStreamDriver) Call(_ gocontext.Context, _ llm.LLMRequest) (*llm.LLMResponse, error) {
	return nil, fmt.Errorf("e2eDoneStreamDriver: Call not used (stream mode)")
}

func (d *e2eDoneStreamDriver) Info() llm.DriverInfo {
	return llm.DriverInfo{
		Name:         "e2e-done",
		Provider:     "test",
		DefaultModel: "mock-model",
		DriverType:   "mock",
	}
}

func (d *e2eDoneStreamDriver) Stream(_ gocontext.Context, _ llm.LLMRequest) (<-chan llm.StreamEvent, error) {
	ch := make(chan llm.StreamEvent, 1)
	ch <- llm.StreamEvent{Type: "done", Content: d.doneContent, TokensUsed: 1}
	close(ch)
	return ch, nil
}

// setupRateLimitE2E 起真实 socket server + kernel, 注册两个 LLM 设备
// （/dev/llm/primary + /dev/llm/fb, provider resolver 对齐
// newFailureRawKernel 的双设备形态 —— D5 归因链需要 fallback）。LLM 设备经
// llm.FileFactory 包成**真实 LLMFile**, 只有底层 driver 是 mock。
func setupRateLimitE2E(t *testing.T, primary, fallback llm.LLMDriver) (sockPath string, kern *kernel.KernelImpl, dataDir, projBase string) {
	t.Helper()

	devReg := vfs.NewDeviceRegistry()
	if err := devReg.Register("/dev/llm/claude", llm.FileFactory(primary, "/dev/llm/claude", "")); err != nil {
		t.Fatalf("register /dev/llm/claude: %v", err)
	}
	if err := devReg.Register("/dev/llm/fb", llm.FileFactory(fallback, "/dev/llm/fb", "")); err != nil {
		t.Fatalf("register /dev/llm/fb: %v", err)
	}
	vfsInst := vfs.NewVFS(devReg)
	ctxMgr := rnixctx.NewManager()

	srv := NewServer(nil, nil, "0.1.0-test", "", "")
	kern = kernel.NewKernel(vfsInst, ctxMgr, srv.CallbackMux())
	srv.kern = kern
	kern.SetProviderResolver(
		func() []string { return []string{"claude", "fb"} },
		func(name string) bool { return name == "claude" || name == "fb" },
	)

	// 必须先于下面的 kern.Shutdown cleanup 注册（LIFO）—— 对齐
	// setupInterruptE2E 的顺序。
	dataDir, projBase = kernel.TestSetupDataDir(t, kern)

	sockPath = filepath.Join(t.TempDir(), "test.sock")
	if err := srv.ListenAndServe(sockPath); err != nil {
		t.Fatalf("ListenAndServe: %v", err)
	}
	t.Cleanup(func() {
		srv.Shutdown()
		srv.Wait()
		kern.Shutdown()
	})

	return sockPath, kern, dataDir, projBase
}

// e2eFallbackAgentInfo 构造带 fallback 的 AgentInfo（对齐 kernel 包测试的
// fallbackAgentInfo 形状）: provider=claude → /dev/llm/claude,
// fallbackProvider=fb → /dev/llm/fb。
func e2eFallbackAgentInfo() *agents.AgentInfo {
	return &agents.AgentInfo{
		Manifest: agents.AgentManifest{
			Name: "e2e-rl-agent",
			Models: agents.AgentModels{
				Provider:         "claude",
				Preferred:        "sonnet",
				Fallback:         "haiku",
				FallbackProvider: "fb",
			},
			ContextBudget: 4096,
		},
		Instructions: "E2E rate-limit visibility agent.",
	}
}

// rateLimitThrottleErr 构造生产形状的限流错误: LLMError(429) 包
// RateLimitErrorWithWait。wait=50ms（构造参数, 文案仍是 73.1 实捕文本;
// 73.3 §7 已确立"duration 可构造、文案必须实捕"的 provenance 纪律）——
// 让三次 transient_retry 各自只睡 ~50-62ms, E2E 总耗时可控。
func rateLimitThrottleErr() error {
	return llm.NewLLMError("qwen", 429,
		llm.NewRateLimitErrorWithWait(llm.KindThrottle, e2eThrottleBody, 50*time.Millisecond, time.Time{}, "header"))
}

// -----------------------------------------------------------------------------
// E2E-1 (AC4 + AC2 wire) — D5 归因 + code 字段穿过真 socket
//
// 链路: 真 writeStream 收到 error 事件 → 真 kernel 分类（限流 → 3 次 in-cap
// 重试）→ attemptFallback → fallback 裸连接错误 → 聚合死亡 → exit_reason 带
// primary 限流归因 → MethodWait / list_all_procs wire 可见; 事件 code 字段
// 经 MethodListEvents wire 可见（fallback_exhausted=RATE_LIMIT（primary 的
// code）, 终态 error=DRIVER（聚合 %w 只包 fallback））。
// -----------------------------------------------------------------------------
func TestE2E_73_4_D5RateLimitAttributionAcrossIPC(t *testing.T) {
	primary := &e2eFailStreamDriver{failErr: rateLimitThrottleErr()}
	fallback := &e2eFailStreamDriver{
		failErr: fmt.Errorf("connection refused: dial tcp 10.0.0.1:443"),
	}
	sockPath, kern, _, _ := setupRateLimitE2E(t, primary, fallback)

	pid, err := kern.Spawn("e2e 73.4 — D5 attribution over wire", e2eFallbackAgentInfo(),
		kernel.SpawnOpts{ProjectConfig: kernel.TestProjectConfig()})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	// 编排器真实消费路径: MethodWait 的 wire 上 exit_reason 必须带 primary 的
	// 限流归因（D5: primary 限流优先于 fallback 的裸连接明细）。
	resp, err := dialClient(t, sockPath).Wait(pid, 5000)
	if err != nil {
		t.Fatalf("client.Wait: %v", err)
	}
	if resp.TimedOut {
		t.Fatal("client.Wait timed out — rate-limit death should finish promptly")
	}
	if !strings.HasPrefix(resp.ExitReason, "all providers exhausted: ") {
		t.Fatalf("wire exit_reason = %q, want prefix %q", resp.ExitReason, "all providers exhausted: ")
	}
	if !strings.Contains(resp.ExitReason, "rate limit") {
		t.Errorf("wire exit_reason = %q, want the PRIMARY's rate-limit clue (D5 attribution must win over the bare fallback error)", resp.ExitReason)
	}
	if resp.ExitCode != 1 {
		t.Errorf("wire exit_code = %d, want 1", resp.ExitCode)
	}

	// list_all_procs wire 与 MethodWait 一致（dashboard / ps 消费面）。
	got := procFromWire(t, sockPath, pid)
	if got.ExitReason != resp.ExitReason {
		t.Errorf("list_all_procs exit_reason = %q, want %q (MethodWait)", got.ExitReason, resp.ExitReason)
	}
	if !got.ExitCodeSet || got.ExitCode != 1 {
		t.Errorf("list_all_procs exit_code=%d exit_code_set=%v, want 1/true", got.ExitCode, got.ExitCodeSet)
	}

	// AC2 wire: fallback_exhausted 事件携带 primary 的 code=RATE_LIMIT（73.4 的
	// headline —— 事件流能区分"死于限流"）, 经 MethodListEvents 真 socket 可见。
	uuid := got.UUID
	if uuid == "" {
		t.Fatalf("empty UUID for pid=%d", pid)
	}
	evs, err := dialClient(t, sockPath).ListEvents(pid, uuid)
	if err != nil {
		t.Fatalf("client.ListEvents: %v", err)
	}
	var sawFallbackExhaustedCode, sawTerminalErrorCode bool
	for _, ev := range evs {
		if ev.Syscall != "ReasonStep" {
			continue
		}
		action, _ := ev.Args["action"].(string)
		code, _ := ev.Args["code"].(string)
		switch action {
		case "fallback_exhausted":
			if code != string(types.ErrRateLimit) {
				t.Errorf("fallback_exhausted wire code = %q, want %q (primary's code)", code, types.ErrRateLimit)
			}
			sawFallbackExhaustedCode = true
		case "error":
			// 终态 write-fail 事件的 code = aggregate 的 code（%w 只包 fallback
			// 连接错误 → DRIVER）—— 与 kernel ATDD
			// TestATDD_73_4_AC2_EventCode_WriteFailWithFallback 同判, 但经 wire。
			if code != string(types.ErrDriver) {
				t.Errorf("terminal error wire code = %q, want %q (aggregate wraps only the fallback connection error)", code, types.ErrDriver)
			}
			sawTerminalErrorCode = true
		}
	}
	if !sawFallbackExhaustedCode {
		t.Error("no fallback_exhausted event found over list_events wire")
	}
	if !sawTerminalErrorCode {
		t.Error("no terminal ReasonStep action=error event found over list_events wire")
	}
}

// -----------------------------------------------------------------------------
// E2E-2 (AC5) — 真实 73.3 配额挂起经 list_all_procs wire 可见
//
// 链路: 真 writeStream 收到 quota error（far-future resetAt）→ 真 kernel
// quotaSuspendProcess（D6: 超上限挂起, 跳过 fallback）→ Process 带
// SuspendReason=quota_exhausted + ResumeAt → GetProcInfo → IPC wire:
// suspend_reason + resume_at_ms 同时可见 —— "因配额挂起 + 等到何时"。
// -----------------------------------------------------------------------------
func TestE2E_73_4_QuotaSuspendedVisibleAcrossIPC(t *testing.T) {
	resetAt := time.Now().Add(2 * time.Hour).UTC().Truncate(time.Second)
	quotaErr := llm.NewLLMError("qwen", 429,
		llm.NewRateLimitErrorWithWait(llm.KindQuota, e2eQuotaBody, 0, resetAt, "body"))
	primary := &e2eFailStreamDriver{failErr: quotaErr}
	fallback := &e2eFailStreamDriver{failErr: fmt.Errorf("unused: quota suspension skips fallback (D6)")}
	sockPath, kern, _, _ := setupRateLimitE2E(t, primary, fallback)

	pid, err := kern.Spawn("e2e 73.4 — quota suspend over wire", nil,
		kernel.SpawnOpts{ProjectConfig: kernel.TestProjectConfig()})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	// 挂起不是终态 —— MethodWait 会等到超时。正确探针是轮询 list_all_procs
	// 直到 wire 呈现挂起形状（66.6 的 waitForWireProc 模式）。
	got := waitForWireProc(t, sockPath, pid, "SuspendReason=quota_exhausted && ResumeAt==resetAt", 5*time.Second,
		func(p vfs.ProcInfo) bool {
			return p.State == types.StateSuspended && p.SuspendReason == "quota_exhausted" && !p.ResumeAt.IsZero()
		})

	// AC5: "等到何时" —— wire 上的 resume_at_ms 必须是 resetAt 的毫秒投影
	// （73.3 服务端绝对时刻, 逐字保真）。
	if got.SuspendReason != "quota_exhausted" {
		t.Errorf("wire SuspendReason = %q, want quota_exhausted", got.SuspendReason)
	}
	if !got.ResumeAt.Equal(resetAt) {
		t.Errorf("wire ResumeAt = %v, want the server reset instant %v verbatim", got.ResumeAt, resetAt)
	}
	if got.ResumeAt.UnixMilli() != resetAt.UnixMilli() {
		t.Errorf("wire ResumeAtMs = %d, want %d", got.ResumeAt.UnixMilli(), resetAt.UnixMilli())
	}
}

// -----------------------------------------------------------------------------
// E2E-3 (AC6) — 零值安全穿过真 wire
//
// 正常完成的进程在 list_all_procs wire 上既不带 suspend_reason 也不带
// resume_at_ms —— 既有渲染（dashboard / rnix ps）不被新字段破坏。
// -----------------------------------------------------------------------------
func TestE2E_73_4_RunningProcessOmitsFieldsAcrossIPC(t *testing.T) {
	primary := &e2eDoneStreamDriver{doneContent: `{"action":"complete","summary":"e2e ok","content":"e2e ok"}`}
	fallback := &e2eFailStreamDriver{failErr: fmt.Errorf("unused")}
	sockPath, kern, _, _ := setupRateLimitE2E(t, primary, fallback)

	pid, err := kern.Spawn("e2e 73.4 — zero-value safety over wire", nil,
		kernel.SpawnOpts{ProjectConfig: kernel.TestProjectConfig()})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	resp, err := dialClient(t, sockPath).Wait(pid, 5000)
	if err != nil {
		t.Fatalf("client.Wait: %v", err)
	}
	if resp.TimedOut {
		t.Fatal("client.Wait timed out — normal completion should finish promptly")
	}
	if resp.ExitCode != 0 {
		t.Fatalf("wire exit_code = %d, want 0 (normal completion)", resp.ExitCode)
	}

	got := procFromWire(t, sockPath, pid)
	if got.SuspendReason != "" {
		t.Errorf("wire SuspendReason = %q, want empty for a normal process", got.SuspendReason)
	}
	if !got.ResumeAt.IsZero() {
		t.Errorf("wire ResumeAt = %v, want zero for a normal process (omitempty keeps legacy rendering)", got.ResumeAt)
	}
}
