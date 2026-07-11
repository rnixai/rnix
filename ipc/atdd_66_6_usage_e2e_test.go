package ipc

import (
	gocontext "context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rnixai/rnix/drivers/llm"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/kernel"
	"github.com/rnixai/rnix/vfs"
)

// =============================================================================
// E2E — Story 66.6: 流式 usage 增量传导 · 端到端接缝焊死
//
// 为什么需要这一层（相对 story 已有的 13 个测试的增量）:
//
//	drivers/llm/atdd_66_6_*  验证 driver 把 message.usage / turn.completed 变成
//	                         StreamEvent{Type:"usage"} —— **止于 driver 的 channel**,
//	                         从不进 writeStream。
//	kernel/atdd_66_6_UNIT-002/003  用 usageLLMFile 直接调 `f.handler(evt)` 喂事件 ——
//	                         **绕过 vfsfile.writeStream 的 case "usage"**, 测的是
//	                         observe.go 的账本分支。
//	ipc/atdd_66_6_wire_test  只 round-trip 手工构造的 ProcInfoWire ——**不经 kernel**。
//
// 三层都不覆盖真正的病灶通路:
//
//	driver.Stream(usage 事件) → 真 writeStream case "usage"（字段搬运）
//	  → 真 onEvent → 真 observe.go handler（AddStreamUsage）
//	  → 真 GetProcInfo 合成（TokensUsed+StreamTokensUsed）→ IPC wire → 消费面。
//
// `vfsfile.go:289 case "usage"` 是整条链上唯一没有任何测试触及的一环。若有人删
// 掉这个 case, 或漏搬一个字段（如忘了 tokens_used）, driver 单测与 kernel 单测
// 全绿, 而案卷的"TOKENS 恒 0"盲区会原样复现。本文件把这一环焊死 —— 与
// atdd_66_2_interrupted_e2e 焊死 writeStream partial 接缝同构。
//
// 搭建复用 atdd_66_2 的 setupInterruptE2E / dialClient / procFromWire /
// readProcInfoDisk / killAndWait / reloadHistory —— LLM 设备经 llm.FileFactory
// 注册**真实 LLMFile**, 只有底层 driver 是 mock。
//
// 时序取舍: usage 事件经 driver goroutine → 真 writeStream → handler 异步落到
// kernel 的 write goroutine（不同于 kernel 单测的同步 f.handler(evt)）, 故中途
// 采样一律用 waitForWireTokens 轮询到最终一致, 而非 <-reached 后立即断言。
// =============================================================================

// usageE2EDriver 是一个真实实现 llm.LLMDriver 的 mock, 复刻 claude-cli 长 step
// 的行为: 先 emit 若干 usage 增量事件（每轮 API 往返的 delta）与一次 tool_call
// 生命周期, 然后要么 park 在 ctx.Done（kill 场景）, 要么 emit done 正常收束。
//
// DriverType() 报 claude-cli, 使 observe.go 的 IsCLIDriver 判定成立（与真实
// CLI driver 同路), usage 事件标 provenance cli_stream。
type usageE2EDriver struct {
	usageDeltas  []int  // 每个 = 一轮往返的 tokens_used delta
	emitToolCall bool   // true → 追加一次 tool_call started+completed 生命周期
	sendDone     bool   // true → emit done 正常收束; false → park 待杀
	doneContent  string // done 的最终内容（complete action JSON）
	doneTokens   int    // done 的权威 session 总量

	ready     chan struct{} // 所有中途事件已送出、driver 就位
	readyOnce sync.Once
}

func (d *usageE2EDriver) Call(_ gocontext.Context, _ llm.LLMRequest) (*llm.LLMResponse, error) {
	return nil, fmt.Errorf("usageE2EDriver: Call not used (stream mode)")
}

func (d *usageE2EDriver) Info() llm.DriverInfo {
	return llm.DriverInfo{
		Name:         "e2e-usage",
		Provider:     "test",
		DefaultModel: "mock-model",
		DriverType:   llm.DriverClaudeCLI, // → IsCLIDriver == true
	}
}

func (d *usageE2EDriver) Stream(ctx gocontext.Context, _ llm.LLMRequest) (<-chan llm.StreamEvent, error) {
	// 缓冲足够容纳所有中途事件 + tool_call + done, 使 driver goroutine 不阻塞。
	ch := make(chan llm.StreamEvent, len(d.usageDeltas)+4)
	go func() {
		defer close(ch)
		for _, delta := range d.usageDeltas {
			// input/output 拆分刻意与 tokens_used 一致地非零, 以便 case "usage"
			// 漏搬 input_tokens 时不影响 tokens_used 断言（tokens_used 是主探针）。
			ev := llm.StreamEvent{
				Type:              "usage",
				TokensUsed:        delta,
				InputTokens:       delta - delta/4,
				OutputTokens:      delta / 4,
				CachedInputTokens: 0,
			}
			select {
			case ch <- ev:
			case <-ctx.Done():
				return
			}
		}
		if d.emitToolCall {
			ch <- llm.StreamEvent{Type: "tool_call", Content: "started",
				Data: map[string]any{"tool": "Bash", "call_id": "c1", "command": "ls"}}
			ch <- llm.StreamEvent{Type: "tool_call", Content: "completed",
				Data: map[string]any{"call_id": "c1", "result": "files"}}
		}
		if d.sendDone {
			ch <- llm.StreamEvent{Type: "done", Content: d.doneContent, TokensUsed: d.doneTokens}
			return
		}
		// 所有中途事件已进 channel buffer —— 通知测试 driver 已就位待杀。
		if d.ready != nil {
			d.readyOnce.Do(func() { close(d.ready) })
		}
		<-ctx.Done()
	}()
	return ch, nil
}

// newUsageKillableDriver 造一个"emit usage 增量后等死"的 driver。
func newUsageKillableDriver(deltas []int, emitToolCall bool) *usageE2EDriver {
	return &usageE2EDriver{
		usageDeltas:  deltas,
		emitToolCall: emitToolCall,
		ready:        make(chan struct{}),
	}
}

// testUsageSpawnOpts mirrors the 66-2 e2e spawn opts (project config wired so
// proc-info.json lands in the test's known baseDir).
func testUsageSpawnOpts() kernel.SpawnOpts {
	return kernel.SpawnOpts{ProjectConfig: kernel.TestProjectConfig()}
}

// waitForWireProc polls ListAllProcs over the real socket until the process
// satisfies `ready` (or times out). Mid-stream usage / tool-call signals flow
// through the kernel's async write goroutine, so the visible growth is
// eventually consistent — polling is the correct probe (vs the synchronous
// <-reached handshake the kernel unit test can rely on). `desc` names the
// awaited condition for the timeout message.
func waitForWireProc(t *testing.T, sockPath string, pid types.PID, desc string, timeout time.Duration, ready func(vfs.ProcInfo) bool) vfs.ProcInfo {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last vfs.ProcInfo
	for time.Now().Before(deadline) {
		last = procFromWire(t, sockPath, pid)
		if ready(last) {
			return last
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("wire never reached %s within %s "+
		"(usage/tool 增量未穿过 writeStream → handler → GetProcInfo 合成 → wire); "+
		"TokensUsed=%d UsageProvenance=%q ToolCallCount=%d",
		desc, timeout, last.TokensUsed, last.UsageProvenance, last.ToolCallCount)
	return last
}

// waitForWireTokens is the token-only specialization used where no tool call is
// in the scripted sequence.
func waitForWireTokens(t *testing.T, sockPath string, pid types.PID, want int, timeout time.Duration) vfs.ProcInfo {
	t.Helper()
	return waitForWireProc(t, sockPath, pid,
		fmt.Sprintf("TokensUsed>=%d", want), timeout,
		func(p vfs.ProcInfo) bool { return p.TokensUsed >= want })
}

// -----------------------------------------------------------------------------
// E2E-1 (AC1/AC2/AC5) — 全链: 真 writeStream case "usage" → 真 handler → 真 socket
//
// 案卷 PID 2160 的正向还原: 长 step 进行中, driver 已 emit 中途 usage, `rnix ps`
// (ListAllProcs) 必须看到 TOKENS 增长（案卷症状是恒 0）。这是本文件的核心理由 ——
// 唯一穿过 vfsfile.go:289 case "usage" 的测试。
// -----------------------------------------------------------------------------
func TestE2E_66_6_MidStreamUsageVisibleAcrossIPC(t *testing.T) {
	driver := newUsageKillableDriver([]int{100, 100}, true)
	sockPath, kern, _, _ := setupInterruptE2E(t, driver)

	pid, err := kern.Spawn("e2e 66.6 — mid-stream usage over wire", nil, testUsageSpawnOpts())
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	// driver 就位（所有 usage/tool_call 已进 channel buffer）。
	select {
	case <-driver.ready:
	case <-time.After(5 * time.Second):
		t.Fatal("driver 未就位 — usage 事件未送出")
	}

	// AC1/AC5: poll on the terminal tool-call flush (emitted AFTER both usage
	// deltas), so a satisfied tool count implies the two usages are already
	// counted — no racy assertion against the async write goroutine.
	got := waitForWireProc(t, sockPath, pid, "ToolCallCount>=1", 5*time.Second,
		func(p vfs.ProcInfo) bool { return p.ToolCallCount >= 1 })

	if got.TokensUsed != 200 {
		t.Errorf("AC1 FAIL: wire TokensUsed=%d, want 200 "+
			"(writeStream case \"usage\" 或字段搬运断裂)", got.TokensUsed)
	}
	// AC2: provenance 经 wire 标注 cli_stream。
	if got.UsageProvenance != vfs.UsageProvenanceCLIStream {
		t.Errorf("AC2 FAIL: wire UsageProvenance=%q, want %q",
			got.UsageProvenance, vfs.UsageProvenanceCLIStream)
	}
	// AC5: tool_call 生命周期经 writeStream case "tool_call" → flushTool 计数, 经 wire 可见。
	if got.ToolCallCount != 1 {
		t.Errorf("AC5 FAIL: wire ToolCallCount=%d, want 1 "+
			"(CLI DriverToolCall flush 未经 writeStream 计数到 wire)", got.ToolCallCount)
	}
	// 进程仍 Running（尚未发 done）—— 证明这是"进行中"预览, 不是落盘后的回读。
	if got.State != types.StateRunning {
		t.Errorf("expected process Running during mid-stream sampling, got %s", got.State)
	}

	// 收尾: kill 放行 parked driver, 避免 goroutine 泄漏（断言留给 E2E-2）。
	_ = dialClient(t, sockPath).Kill(pid, types.SIGTERM, types.KillOriginCLI)
	_, _ = dialClient(t, sockPath).Wait(pid, 5000)
}

// -----------------------------------------------------------------------------
// E2E-2 (AC4-kill/AC6) — 被杀 step 的中途累计并入 proc-info.json, 穿过 IPC + 落盘
//
// 长 step 被 `rnix kill` 时最终 LLMResponse 不存在, finishProcess 必须把
// StreamTokensUsed 并入 proc.TokensUsed（immune Finalize 的输入 = proc.TokensUsed）。
// 断言走**落盘的 proc-info.json**（权威）+ IPC wire, 不看内存 —— 与
// ipc-e2e-test-traps 记忆一致（被 kill 的顶层进程停 Zombie, 断言 StateDead 必超时）。
// -----------------------------------------------------------------------------
func TestE2E_66_6_KillFoldsMidStreamIntoProcInfo(t *testing.T) {
	driver := newUsageKillableDriver([]int{100, 100}, false)
	sockPath, kern, _, projBase := setupInterruptE2E(t, driver)

	pid, err := kern.Spawn("e2e 66.6 — kill folds usage", nil, testUsageSpawnOpts())
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	select {
	case <-driver.ready:
	case <-time.After(5 * time.Second):
		t.Fatal("driver 未就位")
	}
	// 先确认中途累计已穿到 wire（否则 kill 后 fold 的对象可能是 0, 假绿）。
	waitForWireTokens(t, sockPath, pid, 200, 5*time.Second)

	// kill → finishProcess 并入 → 落盘。
	killAndWait(t, sockPath, pid)

	got := procFromWire(t, sockPath, pid)
	uuid := got.UUID
	if uuid == "" {
		t.Fatalf("wire UUID empty for pid=%d — 无法定位 proc-info.json", pid)
	}

	// 权威断言走落盘快照（immune / audit / resume 的持久来源）。
	snap := readProcInfoDisk(t, projBase, uuid)
	if tokens, _ := snap["tokens_used"].(float64); int(tokens) != 200 {
		t.Errorf("AC4-kill FAIL: proc-info.json tokens_used=%v, want 200 "+
			"(finishProcess 被杀并入未生效; immune token_rate 会恒 0)", snap["tokens_used"])
	}
	if prov, _ := snap["usage_provenance"].(string); prov != vfs.UsageProvenanceCLIStream {
		t.Errorf("AC2 FAIL: proc-info.json usage_provenance=%q, want %q",
			prov, vfs.UsageProvenanceCLIStream)
	}
	// wire 侧一致（GetProcInfo 合成: 此时 StreamTokensUsed 已清零并入, == TokensUsed）。
	if got.TokensUsed != 200 {
		t.Errorf("AC4-kill FAIL: wire TokensUsed=%d, want 200", got.TokensUsed)
	}
	// 不得双计: 中途 200 并入后 == 200, 不是 400。
	if got.TokensUsed > 200 {
		t.Errorf("AC4 FAIL: wire TokensUsed=%d > 200 (疑似并入与合成双计)", got.TokensUsed)
	}
}

// -----------------------------------------------------------------------------
// E2E-3 (AC4 对账不双计) — 正常收束的 step 以最终 done 为权威, 中途累计被丢弃
//
// 中途 emit 2×100 后 emit done(TokensUsed=250) 正常完成。step 边界必须清零
// StreamTokensUsed 并以 resp 权威覆盖 → 经 wire 读回 250（非 450 双计）。
// 这是 UNIT-003 的端到端腿: UNIT-003 直接喂 handler, 这里走真 writeStream done 路径。
// -----------------------------------------------------------------------------
func TestE2E_66_6_NormalCompletionNoDoubleCountAcrossIPC(t *testing.T) {
	driver := &usageE2EDriver{
		usageDeltas: []int{100, 100},
		sendDone:    true,
		doneContent: `{"action":"complete","summary":"任务完成","content":"任务完成"}`,
		doneTokens:  250,
	}
	sockPath, kern, _, _ := setupInterruptE2E(t, driver)

	pid, err := kern.Spawn("e2e 66.6 — no double count", nil, testUsageSpawnOpts())
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	resp, err := dialClient(t, sockPath).Wait(pid, 5000)
	if err != nil {
		t.Fatalf("client.Wait: %v", err)
	}
	if resp.TimedOut {
		t.Fatal("client.Wait timed out — 正常完成应迅速收束")
	}
	if resp.ExitReason != "completed" {
		t.Errorf("wire exit_reason=%q, want completed", resp.ExitReason)
	}

	got := procFromWire(t, sockPath, pid)
	if got.TokensUsed != 250 {
		t.Errorf("AC4 FAIL: wire TokensUsed=%d, want 250 "+
			"(中途 2×100 应被 done 权威覆盖丢弃, 非 450 双计)", got.TokensUsed)
	}
}

// -----------------------------------------------------------------------------
// E2E-4 (AC2/AC5 持久化) — provenance + tool_call_count 穿过 daemon 重启
//
// 被杀 step 的 usage_provenance / tool_call_count 落 proc-info.json 后, 全新
// kernel 从同一 dataDir LoadHistory 必须原样装回（procInfoDisk round-trip 的跨
// 实例证明 —— story 回归面审计 #12 断言 immune 受益依赖此持久化）。
// -----------------------------------------------------------------------------
func TestE2E_66_6_ProvenanceAndToolCountSurviveDaemonRestart(t *testing.T) {
	driver := newUsageKillableDriver([]int{100, 100}, true)
	sockPath, kern, dataDir, _ := setupInterruptE2E(t, driver)

	pid, err := kern.Spawn("e2e 66.6 — restart persistence", nil, testUsageSpawnOpts())
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	select {
	case <-driver.ready:
	case <-time.After(5 * time.Second):
		t.Fatal("driver 未就位")
	}
	// 等中途累计 + tool 计数都已穿到 wire（tool flush 在两 usage 之后, 满足即
	// 蕴含 usage 已计）, 再 kill —— 否则 fold/计数可能捕捉到未处理完的中途态。
	waitForWireProc(t, sockPath, pid, "ToolCallCount>=1", 5*time.Second,
		func(p vfs.ProcInfo) bool { return p.ToolCallCount >= 1 && p.TokensUsed >= 200 })
	killAndWait(t, sockPath, pid)
	uuid := procFromWire(t, sockPath, pid).UUID
	if uuid == "" {
		t.Fatalf("empty UUID for pid=%d", pid)
	}

	// daemon 重启: 全新 kernel 从同一 dataDir 装载历史。
	reloaded := reloadHistory(t, dataDir, uuid)

	if reloaded.UsageProvenance != vfs.UsageProvenanceCLIStream {
		t.Errorf("after restart: UsageProvenance=%q, want %q "+
			"(procInfoDisk round-trip 丢失 provenance)",
			reloaded.UsageProvenance, vfs.UsageProvenanceCLIStream)
	}
	if reloaded.ToolCallCount != 1 {
		t.Errorf("after restart: ToolCallCount=%d, want 1 "+
			"(procInfoDisk round-trip 丢失 tool_call_count)", reloaded.ToolCallCount)
	}
	if reloaded.TokensUsed != 200 {
		t.Errorf("after restart: TokensUsed=%d, want 200 (被杀并入值未持久化)",
			reloaded.TokensUsed)
	}
}

// TestE2E_66_6_UsageEventsDoNotLeakToEventsLog is a guard for 拍板 7: usage events
// are high-frequency and must NOT be written to events.jsonl (only in-memory
// counters). A leak would flood the timeline/strace projection. We assert the
// on-wire result carries the usage signal (proving the event was processed)
// while the process's exit path stays clean — the events.jsonl absence itself is
// covered by the kernel-side handler `return` before emit; here we simply ensure
// the visible token growth does not depend on any logged event.
func TestE2E_66_6_UsageVisibleWithoutEventLogDependency(t *testing.T) {
	driver := newUsageKillableDriver([]int{50, 50, 50}, false)
	sockPath, kern, _, _ := setupInterruptE2E(t, driver)

	pid, err := kern.Spawn("e2e 66.6 — usage no event-log", nil, testUsageSpawnOpts())
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	select {
	case <-driver.ready:
	case <-time.After(5 * time.Second):
		t.Fatal("driver 未就位")
	}

	got := waitForWireTokens(t, sockPath, pid, 150, 5*time.Second)
	if got.TokensUsed != 150 {
		t.Errorf("AC1 FAIL: wire TokensUsed=%d, want 150 (3×50 delta)", got.TokensUsed)
	}
	if !strings.EqualFold(got.UsageProvenance, vfs.UsageProvenanceCLIStream) {
		t.Errorf("AC2 FAIL: UsageProvenance=%q, want cli_stream", got.UsageProvenance)
	}

	_ = dialClient(t, sockPath).Kill(pid, types.SIGTERM, types.KillOriginCLI)
	_, _ = dialClient(t, sockPath).Wait(pid, 5000)
}
