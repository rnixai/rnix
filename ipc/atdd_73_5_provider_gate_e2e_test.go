package ipc

import (
	gocontext "context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rnixai/rnix/agents"
	"github.com/rnixai/rnix/drivers/llm"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/kernel"
)

// =============================================================================
// E2E — Story 73.5: per-provider 并发闸门在编排器消费面上可见（AC1/AC6/AC7/AC8）
//
// 为什么需要这一层（相对 story 已有的 11 单测 + 7 ATDD 的增量）:
//
//	kernel/provider_gate_test.go       用 gateKernel + parkWriteLLM ——
//	                                 **绕过 vfsfile.writeStream**（mock Write 直接
//	                                 返回）, 且断言读**磁盘 events.jsonl**。
//	kernel/atdd_73_5_provider_gate_test.go  同样不经过真实 LLMFile / 真实 IPC
//	                                 wire —— k.Spawn + waitDone + ReadAllEvents
//	                                 全部在 kernel 包内直读。
//
// 两层之间的接缝（真 writeStream 持 slot → 真 kernel gate 排队 → 排队事件经
// MethodListEvents wire 可见 → 排队中 SIGTERM 经 MethodKill wire 生效 →
// MethodWait exit_reason 到编排器）此前无任何测试。73.5 的 user story 主体是
// "As a 并发跑多个 agent 的编排者"——编排器消费的正是 gate 排队是否可观测、
// 等待是否可取消。本文件把这条接缝焊死, 与 73.4（限流归因经 wire）/
// 66.2（SIGTERM 经 wire）同构。
//
// 搭建取舍（对齐 atdd_73_4_visibility_e2e_test.go）:
//   - LLM 设备经 **llm.FileFactory 注册真实 LLMFile** —— writeStream 是生产代码,
//     slot 由真实的 kernel gate 发放, 只有底层 driver 是 mock。
//   - 三处调用点里的"主路径 Write"是唯一可经 wire 端到端压测的调用点（compact /
//     fallback 的 gate 超时路径依赖 60s 真实 gateAcquireTimeout, 无 seam 可缩,
//     已由 kernel ATDD 经 sleepFunc seam 覆盖——本层不做）。
//   - 所有读回（MethodWait / MethodListAllProcs / MethodListEvents）与 Kill 一律
//     走真 Unix socket 上的 Client —— 编排器真实消费路径。
// =============================================================================

// gateDoneJSON 是进程正常完成的响应形状（73.4 E2E 同款 JSON complete）。
const gateDoneJSON = `{"action":"complete","summary":"73.5 e2e ok","content":"73.5 e2e ok"}`

// parkingStreamDriver 是真实实现 llm.LLMDriver 的 mock: Stream 在 releaseCh
// 关闭前一直停车（期间不吐任何事件）——真 writeStream 的 range ch 因此停在
// 事件循环里, 进程的 gate slot 被真实持有。reachedCh 在首次 Stream 调用时
// 关闭（证明持有者已进 Write）；streamCalls 计数每次 Stream 调用（证明排队者
// 在 slot 释放前从未到达 Write——gate 在 Write 之前拦截, AC8）。
//
// Release 经 sync.Once 幂等: 测试正常路径关一次, t.Cleanup 兜底再关一次也不
// 会 double-close——cleanup 时不会有进程仍 parked 在 Stream 里。
type parkingStreamDriver struct {
	reachedOnce sync.Once
	reachedCh   chan struct{}
	releaseOnce sync.Once
	releaseCh   chan struct{}
	streamCalls atomic.Int32
	doneContent string
}

func newParkingStreamDriver(doneContent string) *parkingStreamDriver {
	return &parkingStreamDriver{
		reachedCh:   make(chan struct{}),
		releaseCh:   make(chan struct{}),
		doneContent: doneContent,
	}
}

func (d *parkingStreamDriver) Release() { d.releaseOnce.Do(func() { close(d.releaseCh) }) }

func (d *parkingStreamDriver) Call(_ gocontext.Context, _ llm.LLMRequest) (*llm.LLMResponse, error) {
	return nil, fmt.Errorf("parkingStreamDriver: Call not used (stream mode)")
}

func (d *parkingStreamDriver) Info() llm.DriverInfo {
	return llm.DriverInfo{
		Name:         "e2e-gate-park",
		Provider:     "test",
		DefaultModel: "mock-model",
		DriverType:   "mock",
	}
}

func (d *parkingStreamDriver) Stream(ctx gocontext.Context, _ llm.LLMRequest) (<-chan llm.StreamEvent, error) {
	d.reachedOnce.Do(func() { close(d.reachedCh) })
	d.streamCalls.Add(1)
	select {
	case <-d.releaseCh:
	case <-ctx.Done():
	}
	ch := make(chan llm.StreamEvent, 1)
	if ctx.Err() == nil {
		ch <- llm.StreamEvent{Type: "done", Content: d.doneContent, TokensUsed: 1}
	}
	close(ch)
	return ch, nil
}

// setupGateE2E 起真实 socket server + kernel（复用 73.4 的双设备搭建）, 并把
// per-provider 并发上限注入为 `limit`（SetProviderConcurrencyLimitFunc 是 73.5
// 的公开 setter, 与生产注入点 main.go 同形）。
func setupGateE2E(t *testing.T, primary, fallback llm.LLMDriver, limit int) (sockPath string, kern *kernel.KernelImpl) {
	t.Helper()
	sockPath, kern, _, _ = setupRateLimitE2E(t, primary, fallback)
	kern.SetProviderConcurrencyLimitFunc(func(string) int { return limit })
	if park, ok := primary.(*parkingStreamDriver); ok {
		t.Cleanup(park.Release)
	}
	return sockPath, kern
}

// fbProviderAgent 构造主 provider 为 fb 的 agent —— 与 /dev/llm/fb（73.4 搭建
// 里的第二设备）对齐, 用于证明不同 provider 各走各的桶（D8: per-provider 语义）。
func fbProviderAgent() *agents.AgentInfo {
	return &agents.AgentInfo{
		Manifest: agents.AgentManifest{
			Name: "e2e-fb-agent",
			Models: agents.AgentModels{
				Provider:  "fb",
				Preferred: "mock",
			},
			ContextBudget: 4096,
		},
		Instructions: "E2E gate isolation agent on provider fb.",
	}
}

// listEventsWire 经真 MethodListEvents 拉回进程的 wire 事件。
func listEventsWire(t *testing.T, sockPath string, pid types.PID) []SyscallEventWire {
	t.Helper()
	uuid := procFromWire(t, sockPath, pid).UUID
	if uuid == "" {
		t.Fatalf("empty UUID for pid=%d", pid)
	}
	evs, err := dialClient(t, sockPath).ListEvents(pid, uuid)
	if err != nil {
		t.Fatalf("client.ListEvents(pid=%d): %v", pid, err)
	}
	return evs
}

// waitForGateEvent 轮询 MethodListEvents wire 直到目标 gate 事件出现（events.jsonl
// 逐条即时 flush, EventWriter 每次 WriteEvent 都 Flush —— 运行中进程的事件同样
// 可经 wire 读到）。返回事件 args。
func waitForGateEvent(t *testing.T, sockPath string, pid types.PID, syscall, provider string, timeout time.Duration) map[string]any {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, ev := range listEventsWire(t, sockPath, pid) {
			if ev.Syscall != syscall {
				continue
			}
			if provider == "" || ev.Args["provider"] == provider {
				return ev.Args
			}
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("event %q (provider=%q) never appeared on the wire for pid=%d within %v", syscall, provider, pid, timeout)
	return nil
}

// argF 宽容读取 wire 事件 args 里的数值字段（events.jsonl → json.Unmarshal 走
// float64, 与 kernel 包 test 里 argMillis 的容错同理）。
func argF(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int64:
		return float64(n)
	case int:
		return float64(n)
	}
	return 0
}

// -----------------------------------------------------------------------------
// E2E-1 (AC7 + AC6 + D8) — 排队事件经 wire 可见; 排队中 SIGTERM → interrupted;
//
//	不同 provider 互不阻塞 + 零噪声
//
// 链路: 真 writeStream 持有 claude slot（parking driver 不吐事件）→ proc2 在
// kernel gate 排队（真实 10s 心跳分块, 首块后 emit provider_gate_wait）→
// MethodListEvents wire 读到 {provider, limit, queued, wait_ms} → MethodKill
// (SIGTERM) 打断排队 → MethodWait 的 wire exit_reason="interrupted"。
// 同时: proc3 走 fb provider 的独立桶, 在 claude 拥堵期间正常完成, 且其事件流
// 零 gate 噪声（healthy 流零噪声原则, D7）。
// -----------------------------------------------------------------------------
func TestE2E_73_5_GateWaitEventAndCancelAcrossIPC(t *testing.T) {
	park := newParkingStreamDriver(gateDoneJSON)
	done := &e2eDoneStreamDriver{doneContent: gateDoneJSON}
	sockPath, kern := setupGateE2E(t, park, done, 1)

	// proc1 持有 claude 的 gate slot: 它的 Write 在真实 writeStream 内停车。
	pid1, err := kern.Spawn("e2e 73.5 — gate holder", e2eFallbackAgentInfo(),
		kernel.SpawnOpts{ProjectConfig: kernel.TestProjectConfig()})
	if err != nil {
		t.Fatalf("Spawn proc1: %v", err)
	}
	select {
	case <-park.reachedCh:
	case <-time.After(5 * time.Second):
		t.Fatal("proc1 never reached the LLM Write (slot not held)")
	}

	// proc2 同 provider claude → 在 gate 排队（Write 之前, AC8）。
	pid2, err := kern.Spawn("e2e 73.5 — gate queued", e2eFallbackAgentInfo(),
		kernel.SpawnOpts{ProjectConfig: kernel.TestProjectConfig()})
	if err != nil {
		t.Fatalf("Spawn proc2: %v", err)
	}

	// proc3 走 fb 的独立桶: claude 拥堵期间必须正常完成（D8 per-provider 分桶）。
	pid3, err := kern.Spawn("e2e 73.5 — fb isolation", fbProviderAgent(),
		kernel.SpawnOpts{ProjectConfig: kernel.TestProjectConfig()})
	if err != nil {
		t.Fatalf("Spawn proc3: %v", err)
	}
	resp3, err := dialClient(t, sockPath).Wait(pid3, 5000)
	if err != nil {
		t.Fatalf("client.Wait(proc3): %v", err)
	}
	if resp3.TimedOut {
		t.Fatal("proc3 timed out — fb bucket must not be blocked by claude congestion")
	}
	if resp3.ExitCode != 0 {
		t.Errorf("proc3 wire exit_code = %d, want 0 (different provider must proceed)", resp3.ExitCode)
	}

	// 零噪声（D7）: proc3 快速获取, 其事件流不得出现任何 gate 事件。
	for _, ev := range listEventsWire(t, sockPath, pid3) {
		if ev.Syscall == "provider_gate_wait" || ev.Syscall == "provider_gate_timeout" {
			t.Errorf("proc3 (fast acquire) carries %q event — healthy flow must stay silent", ev.Syscall)
		}
	}

	// AC7 通道②: 排队 > gateWaitEmitThreshold 的 provider_gate_wait 事件必须经
	// 真 socket 的 MethodListEvents 可见。真实时间下首块心跳 10s 后 emit。
	args := waitForGateEvent(t, sockPath, pid2, "provider_gate_wait", "claude", 20*time.Second)
	if p, _ := args["provider"].(string); p != "claude" {
		t.Errorf("event provider = %q, want claude", p)
	}
	if l := argF(args["limit"]); l != 1 {
		t.Errorf("event limit = %v, want 1", args["limit"])
	}
	if q := argF(args["queued"]); q != 1 {
		t.Errorf("event queued = %v, want 1 (single waiter, including the emitter — D7)", args["queued"])
	}
	if w := argF(args["wait_ms"]); w < 1000 {
		t.Errorf("event wait_ms = %v, want >= 1000 (emitted only above gateWaitEmitThreshold)", args["wait_ms"])
	}

	// AC6/D4: 排队等待中 SIGTERM（真 socket MethodKill）→ 编排器经 MethodWait
	// 读到 interrupted —— 与 66.2 的 write 中断同一条 exit 路径, 一个 SIGTERM
	// 一个 exit_reason。
	resp2 := killAndWait(t, sockPath, pid2)
	if resp2.ExitReason != "interrupted" {
		t.Errorf("wire exit_reason = %q, want %q (cancel during gate wait)", resp2.ExitReason, "interrupted")
	}
	if resp2.ExitCode != 1 {
		t.Errorf("wire exit_code = %d, want 1", resp2.ExitCode)
	}

	// 收尾: 释放持有者 → proc1 正常完成。
	park.Release()
	resp1, err := dialClient(t, sockPath).Wait(pid1, 5000)
	if err != nil {
		t.Fatalf("client.Wait(proc1): %v", err)
	}
	if resp1.TimedOut {
		t.Fatal("proc1 timed out after release")
	}
	if resp1.ExitCode != 0 || resp1.ExitReason != "completed" {
		t.Errorf("proc1 wire exit = code=%d reason=%q, want 0/completed", resp1.ExitCode, resp1.ExitReason)
	}
}

// -----------------------------------------------------------------------------
// E2E-2 (AC1 + AC8) — 上限 1 时第二个进程在 Write 之前排队; 释放后排队者继续
//
// 链路: proc1 持 slot（真实 writeStream 停车）→ proc2 同 provider 排队 → 在
// 释放前的观察窗内 parking driver 的 Stream 调用数恒为 1（proc2 若未被 gate
// 拦截会立刻进 Write, streamCalls 变 2 —— 这是"排队在 Write 之前"（AC8）经
// 真 writeStream 的确定性探针）→ Release → proc1/proc2 都正常完成, 排队者经
// wire 读回 completed（AC1 的 release 对称性: defer release 归还 slot, 排队
// acquire 放行）。
// -----------------------------------------------------------------------------
func TestE2E_73_5_QueuedWaiterProceedsAfterRelease(t *testing.T) {
	park := newParkingStreamDriver(gateDoneJSON)
	done := &e2eDoneStreamDriver{doneContent: gateDoneJSON}
	sockPath, kern := setupGateE2E(t, park, done, 1)

	pid1, err := kern.Spawn("e2e 73.5 — release holder", e2eFallbackAgentInfo(),
		kernel.SpawnOpts{ProjectConfig: kernel.TestProjectConfig()})
	if err != nil {
		t.Fatalf("Spawn proc1: %v", err)
	}
	select {
	case <-park.reachedCh:
	case <-time.After(5 * time.Second):
		t.Fatal("proc1 never reached the LLM Write (slot not held)")
	}

	pid2, err := kern.Spawn("e2e 73.5 — release queued", e2eFallbackAgentInfo(),
		kernel.SpawnOpts{ProjectConfig: kernel.TestProjectConfig()})
	if err != nil {
		t.Fatalf("Spawn proc2: %v", err)
	}

	// 观察窗 2s: proc2 若未被 gate 拦截会在这期间进 Write（Stream 调用数 → 2）。
	// 恒为 1 才证明排队发生在真实 writeStream 之前。
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if n := park.streamCalls.Load(); n != 1 {
			t.Fatalf("streamCalls = %d, want 1 — proc2 reached the LLM Write while queued at the gate (AC8 broken)", n)
		}
		time.Sleep(100 * time.Millisecond)
	}

	// 释放 slot → 排队者 acquire 放行, 两者都正常完成。
	park.Release()
	for i, pid := range []types.PID{pid1, pid2} {
		resp, err := dialClient(t, sockPath).Wait(pid, 5000)
		if err != nil {
			t.Fatalf("client.Wait(proc%d): %v", i+1, err)
		}
		if resp.TimedOut {
			t.Fatalf("proc%d timed out after release", i+1)
		}
		if resp.ExitCode != 0 || resp.ExitReason != "completed" {
			t.Errorf("proc%d wire exit = code=%d reason=%q, want 0/completed", i+1, resp.ExitCode, resp.ExitReason)
		}
	}
	if n := park.streamCalls.Load(); n != 2 {
		t.Errorf("streamCalls = %d, want 2 (both writes reached the driver after the slot freed)", n)
	}

	got2 := procFromWire(t, sockPath, pid2)
	if !strings.Contains(got2.Result, "73.5 e2e ok") {
		t.Errorf("proc2 wire result = %q, want the completion content", got2.Result)
	}
}
