package ipc

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	gocontext "context"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/drivers/llm"
	"github.com/rnixai/rnix/internal/types"
	"github.com/rnixai/rnix/kernel"
	"github.com/rnixai/rnix/vfs"
)

// =============================================================================
// E2E — Story 66.2: SIGTERM 收割不得伪装 completed
//
// 为什么需要这一层（相对 story 已有的 12 个测试的增量）:
//
//	kernel/atdd_66_2_*  用 interruptLLMFile 直接返回 *llm.StreamInterruptedError,
//	                    **绕过 vfsfile.writeStream** —— 测的是 kernel 分流。
//	drivers/llm/vfsfile_test.go 用 gatedStreamDriver + 手工构造的 &LLMFile{},
//	                    **从不进 kernel** —— 测的是 driver 判定。
//
// 两层之间的接缝（driver.Stream → 真 writeStream → typed error → 真 kernel 分流
// → finishProcess → proc-info.json → IPC wire → 消费面）此前无任何测试。
// 66.2 code-review 的 [Patch] P1 正是在这条接缝上发现的 bug（streamErr 先于
// interrupted 判定 → partial 掷硬币式丢失），且当时明确记录 "测试掩盖
// (gatedStreamDriver 静默 close 不发 error；interruptLLMFile 绕过 writeStream)"。
// 本文件把那条接缝焊死。
//
// 搭建取舍:
//   - LLM 设备经 **llm.FileFactory 注册真实 LLMFile** —— writeStream 是生产代码,
//     只有底层 driver 是 mock。这是本文件存在的全部理由。
//   - Spawn 走 kern.Spawn(ProjectConfig) 而非 client.SpawnAndWatch: 前者保证
//     proc-info.json / events.jsonl 落到测试已知的 baseDir 且不阻塞等待完成。
//   - **Kill 与所有读回一律走真 Unix socket 上的 Client**（MethodKill / MethodWait
//     / MethodListAllProcs）—— 这是 66.2 完全未覆盖的 IPC 面, 也是编排器真实
//     消费 exit_reason 的路径。
//
// t.Cleanup LIFO: TestSetupDataDir 内的 t.TempDir() 必须先于 kern.Shutdown 的
// cleanup 注册（否则 "TempDir RemoveAll: directory not empty"）。setupInterruptE2E
// 沿用 setupResumeIPCTest 的顺序。
// =============================================================================

// partialChunks 是 driver 在被杀之前吐出的流式文本分片。刻意不含 "error"/
// "fail"/"timeout" 等词, 以免与 ui.isFailedResult 的内容启发式纠缠——本文件
// 断言的是 exit_reason 契约, 不是 badge 着色。
var partialChunks = []string{"收到编排任务，", "正在分析目标仓库"}

const partialJoined = "收到编排任务，正在分析目标仓库"

// e2eStreamDriver 是一个真实实现 llm.LLMDriver 的 mock。它精确复刻 claude-cli
// 在 proc.Cancel() 下的行为: 先吐若干 content 事件, 然后阻塞在 ctx.Done() 上,
// 醒来后 close(ch) —— 静默断流, 从不发 done 事件。
//
// emitErrorOnCancel 复刻另一半竞态: 真实 driver（claude_cli.go:695-698 /
// anthropic.go:503-506）在 cancel 时 select 于 "发 error 事件" 与 "<-ctx.Done()"
// 之间, 约半数场景 error 事件胜出。这条分支验证 P1 修复（interrupted 判定必须
// 压过 streamErr）在端到端链路上真的成立。
type e2eStreamDriver struct {
	contentChunks     []string
	sendDone          bool   // true → 发 done 事件后正常返回（不被 cancel 影响）
	doneContent       string // done 事件的最终内容（覆盖累积的 content）
	emitErrorOnCancel bool   // true → ctx.Done() 后抢先发一个 error 事件

	ready     chan struct{} // 所有 content 已送出、driver 就位待杀
	readyOnce sync.Once
}

func (d *e2eStreamDriver) Call(_ gocontext.Context, _ llm.LLMRequest) (*llm.LLMResponse, error) {
	return nil, fmt.Errorf("e2eStreamDriver: Call not used (stream mode)")
}

func (d *e2eStreamDriver) Info() llm.DriverInfo {
	return llm.DriverInfo{
		Name:         "e2e-interrupt",
		Provider:     "test",
		DefaultModel: "mock-model",
		DriverType:   "mock",
	}
}

func (d *e2eStreamDriver) Stream(ctx gocontext.Context, _ llm.LLMRequest) (<-chan llm.StreamEvent, error) {
	// 缓冲足够容纳所有 content + done/error, 使 driver goroutine 在 cancel
	// 之后发 error 事件时不会阻塞（消费者可能还没排到）。
	ch := make(chan llm.StreamEvent, len(d.contentChunks)+2)
	go func() {
		defer close(ch)
		for _, c := range d.contentChunks {
			select {
			case ch <- llm.StreamEvent{Type: "content", Content: c}:
			case <-ctx.Done():
				return
			}
		}
		if d.sendDone {
			ch <- llm.StreamEvent{Type: "done", Content: d.doneContent, TokensUsed: 42}
			return
		}
		// 所有 content 已进 channel buffer —— 通知测试可以下刀了。
		if d.ready != nil {
			d.readyOnce.Do(func() { close(d.ready) })
		}
		<-ctx.Done()
		if d.emitErrorOnCancel {
			ch <- llm.StreamEvent{Type: "error", Content: "context canceled", Err: ctx.Err()}
		}
	}()
	return ch, nil
}

// newKillableDriver 造一个"吐 partial 后等死"的 driver。
func newKillableDriver(emitErrorOnCancel bool) *e2eStreamDriver {
	return &e2eStreamDriver{
		contentChunks:     partialChunks,
		emitErrorOnCancel: emitErrorOnCancel,
		ready:             make(chan struct{}),
	}
}

// setupInterruptE2E 起真实 socket server, 并把 driver 包进**真实的 LLMFile**
// （llm.FileFactory）注册到 /dev/llm/claude。返回 sockPath 与 projBase。
//
// 返回 sockPath 而非一个长命 Client, 因为 MethodWait 是 long-blocking method:
// dispatch 处理完即 `return`, 触发 handleConn 的 defer conn.Close()（与
// MethodSpawn 同一约定, ipc/server.go:431-433）。Wait 之后那条连接就废了。
// 每个操作各自 dial 也更贴近真实形态 —— `rnix kill` 与 `rnix ps` 本就是两个
// 独立进程, 各建各的连接。
// dataDir 是全局数据根（LoadHistory 遍历 AllBaseDirs(dataDir) = dataDir/projects/*
// + dataDir/global）；projBase 是本项目的 per-project 子目录（proc-info.json 落在
// projBase/steps/<uuid>/）。两者别混用: SetStepDataDir 只是 SetDataDir 的 deprecated
// 别名, 喂它 projBase 会让 LoadHistory 扫空目录。
func setupInterruptE2E(t *testing.T, driver llm.LLMDriver) (sockPath string, kern *kernel.KernelImpl, dataDir, projBase string) {
	t.Helper()

	devReg := vfs.NewDeviceRegistry()
	if err := devReg.Register("/dev/llm/claude", llm.FileFactory(driver, "/dev/llm/claude", "")); err != nil {
		t.Fatalf("register /dev/llm/claude: %v", err)
	}
	vfsInst := vfs.NewVFS(devReg)
	ctxMgr := rnixctx.NewManager()

	srv := NewServer(nil, nil, "0.1.0-test", "", "")
	kern = kernel.NewKernel(vfsInst, ctxMgr, srv.CallbackMux())
	srv.kern = kern

	// 必须先于下面的 kern.Shutdown cleanup 注册（LIFO）。
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

// dialClient opens a fresh client connection to the daemon socket, closed on cleanup.
// (Named dialClient because server_test.go already owns a `dial` returning net.Conn.)
func dialClient(t *testing.T, sockPath string) *Client {
	t.Helper()
	client, err := Dial(sockPath)
	if err != nil {
		t.Fatalf("Dial(%s): %v", sockPath, err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

// spawnAndAwaitStream spawns a process and blocks until the driver has pushed
// all of its partial content and parked on ctx.Done() — i.e. the process is
// Running with an in-flight LLM Write. Mirrors the parkOnWrite handshake used
// by atdd_44_5, but the parking happens inside the real driver.
func spawnAndAwaitStream(t *testing.T, kern *kernel.KernelImpl, d *e2eStreamDriver, intent string) types.PID {
	t.Helper()
	pid, err := kern.Spawn(intent, nil, kernel.SpawnOpts{ProjectConfig: kernel.TestProjectConfig()})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}
	select {
	case <-d.ready:
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for driver to stream partial content and park on ctx.Done()")
	}
	return pid
}

// procFromWire pulls one process back through the real IPC wire
// (MethodListAllProcs → ProcInfoWire → WireToProcInfo). This is exactly what
// the dashboard and any orchestrator observe.
func procFromWire(t *testing.T, sockPath string, pid types.PID) vfs.ProcInfo {
	t.Helper()
	procs, err := dialClient(t, sockPath).ListAllProcs()
	if err != nil {
		t.Fatalf("client.ListAllProcs: %v", err)
	}
	for _, p := range procs {
		if p.PID == pid {
			return p
		}
	}
	t.Fatalf("pid=%d not found in ListAllProcs (%d entries)", pid, len(procs))
	return vfs.ProcInfo{}
}

// readProcInfoDisk decodes proc-info.json as a raw map so assertions bind to
// the JSON tag names (the stable on-disk surface), not the Go struct layout.
func readProcInfoDisk(t *testing.T, projBase, uuid string) map[string]any {
	t.Helper()
	path := filepath.Join(projBase, "steps", uuid, "proc-info.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var snap map[string]any
	if err := json.Unmarshal(data, &snap); err != nil {
		t.Fatalf("unmarshal proc-info.json: %v", err)
	}
	return snap
}

// killAndWait sends SIGTERM across the real socket and waits for the reaped
// exit status — also across the real socket, on a second connection (MethodWait
// consumes its connection).
func killAndWait(t *testing.T, sockPath string, pid types.PID) *WaitResponse {
	t.Helper()
	if err := dialClient(t, sockPath).Kill(pid, types.SIGTERM, types.KillOriginCLI); err != nil {
		t.Fatalf("client.Kill(pid=%d, SIGTERM): %v", pid, err)
	}
	resp, err := dialClient(t, sockPath).Wait(pid, 5000)
	if err != nil {
		t.Fatalf("client.Wait(pid=%d): %v", pid, err)
	}
	if resp.TimedOut {
		t.Fatalf("client.Wait(pid=%d) timed out — process never reached a terminal state", pid)
	}
	return resp
}

// -----------------------------------------------------------------------------
// E2E-1 (AC1, AC2) — 全链: 真 writeStream → 真 kernel 分流 → 真 socket 读回
// -----------------------------------------------------------------------------

// TestE2E_66_2_KillDuringStream_InterruptedAcrossIPC
//
// 案卷 PID 2160 的端到端还原: 长任务的流式 write 挂起中被 `rnix kill` (SIGTERM)。
// 修复前 writeStream 会把已累积的部分文本组装成正常 LLMResponse 返回 nil error,
// kernel 走 text 分支落 exit_reason=completed —— 被杀伪装成完成。
//
// 断言 MethodWait 的 wire 上 exit_reason 是**精确字面量** "interrupted"（无前后
// 缀）—— dashboard D1 四段路由、ui.isFailedResult 豁免、64.1 归一化常量三处消费
// 面全是等值匹配, 带信号后缀会全部脱靶。
func TestE2E_66_2_KillDuringStream_InterruptedAcrossIPC(t *testing.T) {
	driver := newKillableDriver(false)
	sockPath, kern, _, _ := setupInterruptE2E(t, driver)

	pid := spawnAndAwaitStream(t, kern, driver, "e2e 66.2 — kill during stream")
	resp := killAndWait(t, sockPath, pid)

	if resp.ExitReason != "interrupted" {
		t.Errorf("wire exit_reason = %q, want exactly %q "+
			"(伪装成 completed 正是 Story 66.2 要消灭的缺陷)", resp.ExitReason, "interrupted")
	}
	if resp.ExitCode != 1 {
		t.Errorf("wire exit_code = %d, want 1", resp.ExitCode)
	}
}

// -----------------------------------------------------------------------------
// E2E-2 (AC1, 拍板 4) — partial 标注必须活着穿过 IPC wire
// -----------------------------------------------------------------------------

// TestE2E_66_2_WirePreservesPartialResultPrefix
//
// 拍板 4 的前缀腿: `[partial] ` 进下游 prompt 是正确信号, 防 automator 盲用
// result 字符串插值。
func TestE2E_66_2_WirePreservesPartialResultPrefix(t *testing.T) {
	driver := newKillableDriver(false)
	sockPath, kern, _, _ := setupInterruptE2E(t, driver)

	pid := spawnAndAwaitStream(t, kern, driver, "e2e 66.2 — partial prefix over wire")
	killAndWait(t, sockPath, pid)

	got := procFromWire(t, sockPath, pid)

	if got.ExitReason != "interrupted" {
		t.Fatalf("wire exit_reason = %q, want %q", got.ExitReason, "interrupted")
	}
	if !strings.HasPrefix(got.Result, "[partial] ") {
		t.Errorf("wire result = %q, want %q prefix "+
			"(真 writeStream 必须把已收到的流式文本作为 StreamInterruptedError.Partial 交给 kernel)",
			got.Result, "[partial] ")
	}
	if !strings.Contains(got.Result, partialJoined) {
		t.Errorf("wire result = %q, want to contain the streamed partial text %q",
			got.Result, partialJoined)
	}
}

// TestE2E_66_2_WirePreservesResultPartialFlag
//
// 拍板 4 的 bool 腿: "bool 字段供编排器程序化路由（**不该靠字符串嗅探判断
// partial**）"。story 的 user story 主体正是 "As a 编排器"。
//
// 编排器读 proc 信息的路径是 IPC（MethodListAllProcs / MethodListProcs）,
// 返回类型 vfs.ProcInfo —— 同类型进、同类型出, 中间过 ProcInfoWire。若 wire
// 少这个字段, 客户端拿到的 ResultPartial 会静默恒为 false, 编排器被迫退回
// 字符串嗅探。
func TestE2E_66_2_WirePreservesResultPartialFlag(t *testing.T) {
	driver := newKillableDriver(false)
	sockPath, kern, _, projBase := setupInterruptE2E(t, driver)

	pid := spawnAndAwaitStream(t, kern, driver, "e2e 66.2 — result_partial over wire")
	killAndWait(t, sockPath, pid)

	got := procFromWire(t, sockPath, pid)

	// 先证明 daemon 侧确实置了 true（落盘是权威）——把 wire 层的丢失与
	// kernel 层的未置位区分开, 免得失败信息指错方向。
	uuid := got.UUID
	if uuid == "" {
		t.Fatalf("wire UUID empty for pid=%d — cannot locate proc-info.json", pid)
	}
	snap := readProcInfoDisk(t, projBase, uuid)
	if diskPartial, _ := snap["result_partial"].(bool); !diskPartial {
		t.Fatalf("proc-info.json result_partial = false — kernel 侧就没置位, "+
			"wire 断言无从谈起 (snapshot=%v)", snap["exit_reason"])
	}

	if !got.ResultPartial {
		t.Errorf("ProcInfo.ResultPartial = false after IPC round-trip, want true "+
			"(daemon 侧 proc-info.json 已是 true) — ProcInfoWire 缺 result_partial 字段, "+
			"ProcInfoToWire/WireToProcInfo 未映射, 编排器只能靠 %q 前缀字符串嗅探, "+
			"违背 Story 66.2 拍板 4", "[partial] ")
	}
}

// -----------------------------------------------------------------------------
// E2E-3 (code-review P1) — driver 在 cancel 时抢发 error 事件, partial 仍须存活
// -----------------------------------------------------------------------------

// TestE2E_66_2_ErrorEventRace_PartialSurvives
//
// P1 场景: cancel 时流式 driver 在 "发 error 事件" 与 "<-ctx.Done()" 间 select
// 二选一, error 事件约半数胜出 → streamErr 置位。若 writeStream 的
// `if streamErr != nil { return streamErr }` 跑在 interrupted 判定之前,
// StreamInterruptedError 就不产生, partial 内容丢弃, kernel errors.As 失败,
// 于是无 `[partial] ` 前缀、ResultPartial=false —— 掷硬币式的数据丢失。
//
// exit_reason 在两种竞态下都正确（kernel guard 基于 proc.ctx, 与 error 类型无关）,
// 所以只断言 exit_reason 是抓不住这个 bug 的。partial 标注才是探针。
//
// 这是 P1 的**首个端到端回归网**: 修复时升级的 TestLLMFile_Write_ContextCancelled
// 只覆盖 driver 层。
func TestE2E_66_2_ErrorEventRace_PartialSurvives(t *testing.T) {
	driver := newKillableDriver(true) // ← cancel 后抢发 error 事件
	sockPath, kern, _, _ := setupInterruptE2E(t, driver)

	pid := spawnAndAwaitStream(t, kern, driver, "e2e 66.2 — error-event race")
	resp := killAndWait(t, sockPath, pid)

	if resp.ExitReason != "interrupted" {
		t.Errorf("wire exit_reason = %q, want %q (error 事件不得改变退出语义)",
			resp.ExitReason, "interrupted")
	}

	got := procFromWire(t, sockPath, pid)
	if !strings.HasPrefix(got.Result, "[partial] ") {
		t.Errorf("wire result = %q, want %q prefix — driver 发 error 事件时 partial 被丢弃了; "+
			"interrupted 判定必须前置于 `if streamErr != nil { return streamErr }` "+
			"(drivers/llm/vfsfile.go, code-review P1)", got.Result, "[partial] ")
	}
	if !strings.Contains(got.Result, partialJoined) {
		t.Errorf("wire result = %q, want to contain %q", got.Result, partialJoined)
	}
}

// -----------------------------------------------------------------------------
// E2E-4 (AC6) — 正常完成路径零回归
// -----------------------------------------------------------------------------

// TestE2E_66_2_NormalCompletion_NoRegression
//
// AC6 对照组: 未被杀的进程走 done 事件 → 完整 LLMResponse → complete action →
// exit_reason 仍是 "completed", 且 result 不带 partial 污染。
func TestE2E_66_2_NormalCompletion_NoRegression(t *testing.T) {
	driver := &e2eStreamDriver{
		contentChunks: []string{"thinking"},
		sendDone:      true,
		doneContent:   `{"action":"complete","summary":"任务完成","content":"任务完成"}`,
	}
	sockPath, kern, _, _ := setupInterruptE2E(t, driver)

	pid, err := kern.Spawn("e2e 66.2 — normal completion", nil,
		kernel.SpawnOpts{ProjectConfig: kernel.TestProjectConfig()})
	if err != nil {
		t.Fatalf("Spawn: %v", err)
	}

	resp, err := dialClient(t, sockPath).Wait(pid, 5000)
	if err != nil {
		t.Fatalf("client.Wait: %v", err)
	}
	if resp.TimedOut {
		t.Fatalf("client.Wait timed out — normal completion should finish promptly")
	}

	if resp.ExitReason != "completed" {
		t.Errorf("wire exit_reason = %q, want %q (未被杀的进程不得被 66.2 的守卫误伤)",
			resp.ExitReason, "completed")
	}
	if resp.ExitCode != 0 {
		t.Errorf("wire exit_code = %d, want 0", resp.ExitCode)
	}

	got := procFromWire(t, sockPath, pid)
	if strings.Contains(got.Result, "[partial]") {
		t.Errorf("wire result = %q must not carry a [partial] marker on a clean completion", got.Result)
	}
	if got.ResultPartial {
		t.Errorf("ProcInfo.ResultPartial = true on a clean completion, want false")
	}
}

// -----------------------------------------------------------------------------
// E2E-5 (AC1, AC6) — 落盘快照 + daemon 重启 roundtrip
// -----------------------------------------------------------------------------

// TestE2E_66_2_DiskSnapshot_And_DaemonRestartRoundtrip
//
// 两件事:
//  1. proc-info.json 的 on-disk 形态（exit_reason / exit_code / exit_code_set /
//     result_partial）—— 编排器与 `rnix resume` 的持久权威。
//  2. daemon 重启后 LoadHistory 装载: 64.1 归一化对**非空** ExitReason 是 verbatim
//     保留, 而 66.2 产出的正是非空 "interrupted"。story 回归面审计断言"零冲突",
//     此前无跨 kernel 实例的测试证明它。
func TestE2E_66_2_DiskSnapshot_And_DaemonRestartRoundtrip(t *testing.T) {
	driver := newKillableDriver(false)
	sockPath, kern, dataDir, projBase := setupInterruptE2E(t, driver)

	pid := spawnAndAwaitStream(t, kern, driver, "e2e 66.2 — disk + restart")
	killAndWait(t, sockPath, pid)

	uuid := procFromWire(t, sockPath, pid).UUID
	if uuid == "" {
		t.Fatalf("empty UUID for pid=%d", pid)
	}

	snap := readProcInfoDisk(t, projBase, uuid)
	// 落盘时进程仍是 zombie（顶层进程无人 reap, 见
	// TestE2E_66_2_InterruptedRoutesToDashboardD1_BothPhases 的注释）。这条断言
	// 把它文档化: 这个非终态快照正是下面 64.1 归一化的输入。
	if got, _ := snap["state"].(string); got != "zombie" {
		t.Logf("note: proc-info.json state = %q (expected \"zombie\" for an un-reaped "+
			"top-level process); 若 reap 语义变了, 下面的归一化断言含义随之改变", got)
	}
	if got, _ := snap["exit_reason"].(string); got != "interrupted" {
		t.Errorf("proc-info.json exit_reason = %q, want %q", got, "interrupted")
	}
	if got, _ := snap["exit_code"].(float64); got != 1 {
		t.Errorf("proc-info.json exit_code = %v, want 1", snap["exit_code"])
	}
	if got, _ := snap["exit_code_set"].(bool); !got {
		t.Errorf("proc-info.json exit_code_set = %v, want true "+
			"(真实退出, 区别于 64.1 归一化条目的 false)", snap["exit_code_set"])
	}
	if got, _ := snap["result_partial"].(bool); !got {
		t.Errorf("proc-info.json result_partial = %v, want true", snap["result_partial"])
	}
	if got, _ := snap["result"].(string); !strings.HasPrefix(got, "[partial] ") {
		t.Errorf("proc-info.json result = %q, want %q prefix", got, "[partial] ")
	}

	// --- daemon 重启: 全新 kernel 从同一 dataDir 装载历史 ---
	reloaded := reloadHistory(t, dataDir, uuid)

	if reloaded.ExitReason != "interrupted" {
		t.Errorf("after daemon restart: ExitReason = %q, want %q verbatim "+
			"(64.1 归一化只改写空/非终态 reason)", reloaded.ExitReason, "interrupted")
	}
	if !reloaded.ExitCodeSet || reloaded.ExitCode != 1 {
		t.Errorf("after daemon restart: exit_code=%d exit_code_set=%v, want 1/true",
			reloaded.ExitCode, reloaded.ExitCodeSet)
	}
	if !reloaded.ResultPartial {
		t.Errorf("after daemon restart: ResultPartial = false, want true "+
			"(procInfoDisk roundtrip 必须保住 partial 标注)")
	}
}

// reloadHistory 起一个全新的 KernelImpl（模拟 daemon 重启）, 从 dataDir 装载
// 进程历史, 返回目标 UUID 的条目。
//
// 必须传 dataDir（全局根）而非 projBase: LoadHistory 遍历 AllBaseDirs(dataDir)
// = dataDir/projects/* + dataDir/global, 喂它 projBase 会扫到空目录并静默返回。
func reloadHistory(t *testing.T, dataDir, uuid string) vfs.ProcInfo {
	t.Helper()

	devReg := vfs.NewDeviceRegistry()
	vfsInst := vfs.NewVFS(devReg)
	fresh := kernel.NewKernel(vfsInst, rnixctx.NewManager(), nil)
	t.Cleanup(fresh.Shutdown)

	fresh.SetDataDir(dataDir)
	if err := fresh.LoadHistory(); err != nil {
		t.Fatalf("LoadHistory(%s): %v", dataDir, err)
	}

	for _, p := range fresh.ListAllProcs() {
		if p.UUID == uuid {
			return p
		}
	}
	t.Fatalf("uuid=%s not found after daemon-restart LoadHistory(%s)", uuid, dataDir)
	return vfs.ProcInfo{}
}

// -----------------------------------------------------------------------------
// E2E-6 (拍板 1) — 契约桥: 两阶段命中 dashboard D1 四段路由
// -----------------------------------------------------------------------------

// TestE2E_66_2_InterruptedRoutesToDashboardD1_BothPhases
//
// 拍板 1 的落地证明。exit_reason 选精确字面量 "interrupted"（而非
// "killed (SIGTERM)"）的**唯一理由**是消费面已把它培育成一等公民, 且全是**等值**
// 匹配 —— 带信号后缀会全部脱靶, 被杀的进程掉进 Failed 红段。
//
// D1 路由行为本身已由 tree/atdd_64_3_history_stats_test.go 覆盖（手工构造
// `dead + ExitReason:"interrupted"` 断言归 Interrupted 段）。这里补的是另一半:
// **66.2 真实产出的形状是否正是 64.3 断言的那个输入形状**。不直接调
// tree.RenderHistoryStats 是因为 internal/dashboard/tree → timeline → ipc,
// 反向依赖会造成 import cycle;故断言输入契约, 路由行为交给 tree 包。
//
// 一个非显然的事实（E2E 发现, 与 story 拍板 1 的字面描述有出入）:
//
//	经 IPC kill 的**顶层**进程（PPID=0）不会自动进入 Dead —— 它停在 Zombie。
//	finishProcess 只在 `ppid > 0 且父进程已不存在`（孤儿）时才投 reapCh
//	(kernel/reason.go:236-240);真正 reap 的 kernel.Wait 不被 IPC 调用 ——
//	MethodWait 只等 proc.TerminatedCh()。这是 Unix zombie 语义, 非缺陷。
//
// 于是 D1 的 `StateDead + ExitReason=="interrupted"` 等值分支在 **live kill 场景
// 根本不经过**;此刻走的是 `case types.StateZombie: interrupted++`（该分支不查
// ExitReason）。等值分支只在 **daemon 重启后** 生效: 64.1 归一化把 zombie 快照转
// dead, 而非空 ExitReason 被 verbatim 保留。
//
// 两个阶段都必须落 Interrupted 段, 故两段都断言。
func TestE2E_66_2_InterruptedRoutesToDashboardD1_BothPhases(t *testing.T) {
	driver := newKillableDriver(false)
	sockPath, kern, dataDir, _ := setupInterruptE2E(t, driver)

	pid := spawnAndAwaitStream(t, kern, driver, "e2e 66.2 — D1 contract")
	killAndWait(t, sockPath, pid)

	// --- 阶段 1: live。dashboard 经 D1 的 zombie 分支归 Interrupted。---
	live := procFromWire(t, sockPath, pid)
	if live.State != types.StateZombie {
		t.Errorf("live wire State = %s, want Zombie — 顶层进程被 kill 后无人 reap, 应停在 "+
			"Zombie。若此处变成 Dead, 说明 reap 语义已改, D1 的 zombie 分支与本注释需同步复核",
			live.State)
	}
	// ExitReason 在 Zombie 阶段就已 stamp —— 它不依赖 reap。
	if live.ExitReason != "interrupted" {
		t.Errorf("live wire ExitReason = %q, want exactly %q", live.ExitReason, "interrupted")
	}
	if !live.ExitCodeSet || live.ExitCode != 1 {
		t.Errorf("live wire exit_code=%d exit_code_set=%v, want 1/true",
			live.ExitCode, live.ExitCodeSet)
	}

	// --- 阶段 2: daemon 重启。64.1 归一化 zombie→dead, reason verbatim 保留,
	//     此后 dashboard 走 D1 的 StateDead + ExitReason 等值分支。---
	reloaded := reloadHistory(t, dataDir, live.UUID)
	if reloaded.State != types.StateDead {
		t.Errorf("after daemon restart: State = %s, want Dead (64.1 归一化非终态快照)",
			reloaded.State)
	}
	// 等值, 不是 Contains。这一行就是拍板 1 的全部内容。
	if reloaded.ExitReason != "interrupted" {
		t.Errorf("after daemon restart: ExitReason = %q, want exactly %q — dashboard D1 / "+
			"ui.isFailedResult 豁免 / 64.1 归一化常量三处消费面全是等值匹配, 任何前后缀 "+
			"都会让被杀的进程掉进 Failed 红段", reloaded.ExitReason, "interrupted")
	}
}
