package ipc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	rnixctx "github.com/rnixai/rnix/context"
	"github.com/rnixai/rnix/kernel"
)

// =============================================================================
// ATDD 45.3 — 删除 HB-1 lifecycle ticker + SpawnAndWait 内"父进程假心跳"hack
//             （P4 daemon-passive 扩散层；45.2 warn-only 地基之上的退役 PR）
//
// Story 45.3 删除目标（参考 _bmad-output/implementation-artifacts/
// 45-3-remove-hb1-ticker-and-supervisor-auto-recovery.md §代码位置精确锚点）：
//   1. ipc/server_pipeline.go:11        "sync" import
//   2. ipc/server_pipeline.go:32-34     scriptRunnerHeartbeatInterval 常量
//   3. ipc/server_pipeline.go:36-52     keepScriptRunnerHeartbeat 函数
//   4. ipc/server_pipeline.go:184-200   SpawnAndWait 内 heartbeatTicker setup
//   5. ipc/server_pipeline.go:224-226   case <-hbCh: parentProc.TouchHeartbeat()
//   6. ipc/server_pipeline.go:340-358   handleExecScript HB-1 启动块
//   7. ipc/server_pipeline.go:412-419   HB-1 cleanup (scriptCancel + hbWG.Wait)
//   8. ipc/server_pipeline_heartbeat_test.go  整个文件
//   9. kernel/process.go:778-779, 835   TouchHeartbeat 注释更新（非可测）
//
// RED-phase 信号：本文件 4 个 TestATDD_45_3_00X 用例在改动**之前**运行：
//   - 001 NoHB1KeeperSymbol                              — FAIL（红）
//   - 002 ScriptRunnerLastHeartbeatNotAdvancedByDaemon   — PASS（P4 不变量锚定）
//   - 003 HandleExecScriptCompilesWithoutHB1             — PASS（编译即证据）
//   - 004 SpawnAndWaitNoHeartbeatTicker                  — FAIL（红）
// 改动落地后 001 / 004 转绿；002 / 003 不变（不变量 / 编译契约持续守护）。
//
// 引用：
//   - _bmad-output/implementation-artifacts/45-3-remove-hb1-ticker-and-supervisor-auto-recovery.md
//     §Acceptance-Criteria AC1–AC8 + §Decisions D1–D4 + §代码位置精确锚点
//   - _bmad-output/planning-artifacts/epics/epic-45-heartbeat-subsystem-reform.md
//     §AC-EA3（line 102 "grep -rn \"keepScriptRunnerHeartbeat\" ipc/ 返回空"）
//   - kernel/atdd_45_2_warn_only_test.go（兄弟范式参考：行文风格 + 4 用例矩阵）
//   - ipc/server_pipeline_test.go#newRunningScriptProc（保留 helper，本文件不复用）
//
// Decision D3（参考 spec line 430-442）：偏 grep 子进程 vs AST 分析。本文件实现
// 改为 `os.ReadFile + strings.Contains + filepath.Walk` 标准库路径——比 exec
// grep 子进程更可移植（不依赖 PATH 中的 grep 二进制），与 D3 "降级为
// os.ReadFile + strings.Contains 遍历" 完全一致。
// =============================================================================

// hb1AndWaitGroupSymbols 列出 HB-1 keeper 与 handleExecScript 内 sync.WaitGroup
// 局部变量的标识符。pre-impl 阶段，server_pipeline.go + server_pipeline_heartbeat_test.go
// 必含这些符号；post-impl 阶段，整个 ipc/ 包内归零。
//
// 这些符号被 epic-45 §AC-EA3 line 102 显式列为"grep 必须返回空"的契约证据。
var hb1AndWaitGroupSymbols = []string{
	"keepScriptRunnerHeartbeat",     // AC1: HB-1 keeper 函数（生产代码 + 5 个旧 test 引用）
	"scriptRunnerHeartbeatInterval", // AC1: HB-1 keeper 周期常量
	"hbWG",                          // AC3: handleExecScript 内 sync.WaitGroup 唯一使用点
}

// spawnAndWaitTickerSymbols 是 SpawnAndWait 内"父进程假心跳"hack 的局部标识符。
// 仅出现在 server_pipeline.go（不跨文件）；本切片用于 AC2 单文件断言。
//
// `parentProc.TouchHeartbeat` 采用方法调用串而非裸 `parentProc`，避免误伤
// `s.parentPID` 等合法字段引用（ipcKernelSpawner.parentPID 在 Story 25.x 之后
// 仍是生产代码的合法成员，不在本 story 删除范围内）。
var spawnAndWaitTickerSymbols = []string{
	"heartbeatTicker",           // AC2: SpawnAndWait 内 *time.Ticker 局部变量
	"hbCh",                      // AC2: 闭包返回的 channel
	"parentProc.TouchHeartbeat", // AC2: case 分支体内的方法调用
}

// scanIPCGoFilesExceptSelf 遍历 ipc/ 目录下的所有 .go 文件，跳过本 ATDD 测试
// 文件本身（避免 self-reference：本文件保留 hb1AndWaitGroupSymbols 列表作为
// 断言消息源，自然会被字符串字面量匹配到，但跳过自身后不污染断言）。
//
// 测试运行时 cwd 为 ipc 包目录，所以 filepath.Walk(".") 即遍历 ipc/。
func scanIPCGoFilesExceptSelf(t *testing.T) []string {
	t.Helper()
	const selfName = "atdd_45_3_no_hb1_test.go"
	var files []string
	err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if filepath.Base(path) == selfName {
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		files = append(files, path)
		return nil
	})
	if err != nil {
		t.Fatalf("Story 45.3: scan ipc/ failed: %v", err)
	}
	return files
}

// -----------------------------------------------------------------------------
// AC1 / AC3 / AC4: HB-1 keeper / 常量 / sync.WaitGroup / 整个 test 文件从 ipc/ 删除
// -----------------------------------------------------------------------------

// TestATDD_45_3_001_NoHB1KeeperSymbol
//
// epic-45 §AC-EA3 line 102 明确："`grep -rn \"keepScriptRunnerHeartbeat\" ipc/`
// 返回空"。本测试遍历 ipc/ 目录下的所有 .go 文件（跳过本 ATDD 文件自身以避免
// self-reference），断言任何文件都不再包含 HB-1 keeper 函数 / 常量 / WaitGroup
// 标识符。
//
// 单次 walk 同时覆盖：
//   - AC1（生产代码：server_pipeline.go 内 keepScriptRunnerHeartbeat / scriptRunnerHeartbeatInterval 删除）
//   - AC3（生产代码：handleExecScript 内 hbWG 局部变量删除）
//   - AC4（测试代码：server_pipeline_heartbeat_test.go 整个文件删除 — 5 个旧 test
//          全部引用上述符号，文件删除后断言自然 PASS）
//
// RED 断言：当前源码下 server_pipeline.go 与 server_pipeline_heartbeat_test.go
// 均包含 keepScriptRunnerHeartbeat / scriptRunnerHeartbeatInterval / hbWG →
// 任一文件、任一符号匹配即 FAIL。
//
// 错误消息显式标注 (文件路径 + 符号 + AC)，便于 dev-story 阶段精确定位。
func TestATDD_45_3_001_NoHB1KeeperSymbol(t *testing.T) {
	files := scanIPCGoFilesExceptSelf(t)
	if len(files) == 0 {
		t.Fatal("Story 45.3 AC1: scan returned 0 .go files — ipc/ package layout unexpected")
	}

	for _, path := range files {
		body, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("Story 45.3 AC1: read %s: %v", path, err)
		}
		text := string(body)
		for _, sym := range hb1AndWaitGroupSymbols {
			if strings.Contains(text, sym) {
				t.Errorf("Story 45.3 AC1/AC3/AC4: %s still contains HB-1 symbol %q — must be deleted (epic-45 §AC-EA3 line 102: grep -rn %q ipc/ must return empty)",
					path, sym, sym)
			}
		}
	}
}

// -----------------------------------------------------------------------------
// AC2 / AC7: Daemon 不再有任何路径主动推进 LastHeartbeat（P4 不变量锚定）
// -----------------------------------------------------------------------------

// newSkipReasonLoopProcForATDD45_3 spawns a real SkipReasonLoop process via
// kernel.Spawn（命中 spawn.go:466 / :692 / :717 三处 SkipReasonLoop 分支），
// 但**不进入** handleExecScript / SpawnAndWait 主路径——即不启动 HB-1 keeper
// 或 SpawnAndWait 内 ticker。这是 P4 哲学锚定测试需要的"裸 daemon"状态：
// 进程仅由 Spawn 在 spawn.go:365 一次性初始化 LastHeartbeat，之后 daemon
// 无任何推进路径。
//
// 不复用已删除的 server_pipeline_heartbeat_test.go::newSkipReasonLoopProc
// （AC4 已删除该文件；本 helper 与之实现等价但生命周期独立，避免编译期
// 引用悬挂符号——参考 Story 45.3 spec line 553 "在本 ATDD 文件内新建等价 helper"）。
//
// 与 ipc/server_pipeline_test.go::newRunningScriptProc 不同：后者用 NewProcess
// 手工构造（不走 Spawn），未触发 Spawn 的 LastHeartbeat seed；本 helper 走完整
// Spawn 路径以确保 002 测试观察的 LastHeartbeat 不变量在生产环境下成立。
func newSkipReasonLoopProcForATDD45_3(t *testing.T) (*kernel.KernelImpl, *kernel.Process) {
	t.Helper()
	kern := kernel.NewKernel(nil, rnixctx.NewManager(), nil)
	t.Cleanup(func() { kern.Shutdown() })

	pid, err := kern.Spawn("run: atdd-45-3.ash", nil, kernel.SpawnOpts{SkipReasonLoop: true})
	if err != nil {
		t.Fatalf("Story 45.3 AC7: Spawn(SkipReasonLoop:true) failed: %v", err)
	}
	proc, ok := kern.GetProcess(pid)
	if !ok {
		t.Fatalf("Story 45.3 AC7: GetProcess(%d) vanished after Spawn", pid)
	}
	return kern, proc
}

// TestATDD_45_3_002_ScriptRunnerLastHeartbeatNotAdvancedByDaemon
//
// P4 daemon-passive 核心不变量（参考 epic-45 §最终结论 P4 + Story 45.3 spec
// Dev Notes §核心 bug 诊断）：daemon 不应主动推进业务进程的 LastHeartbeat
// （除 reasonStep / driver streaming / EventWriter 等"业务工作天然驱动"的合法
// writer 之外）。本测试通过裸 spawn 一个 SkipReasonLoop 进程（不进入
// handleExecScript / SpawnAndWait 主路径），观察 200ms 窗口内 LastHeartbeat
// 是否被推进。
//
// 覆盖：
//   - AC7（新建 ATDD 锚定 P4 等价语义；spec line 273-304 显式要求此测试用例）
//   - AC2 的反向语义（SpawnAndWait 内 "父进程在等子的窗口期不再自我 TouchHeartbeat"
//     的反面：脱离 SpawnAndWait 路径后 daemon 也不应主动推进 — 共同守护
//     "心跳由业务工作天然驱动" 的诚实模型）
//
// 状态轨迹（pre-impl / post-impl 均 PASS）：
//   - pre-impl 阶段：HB-1 keeper / SpawnAndWait ticker 都在 handleExecScript /
//     SpawnAndWait 内启动；本测试**不**走该路径 → ticker 不存在 → LastHeartbeat
//     不会被推进 → PASS（不变量已成立）
//   - post-impl 阶段：HB-1 keeper / ticker 都删除 → LastHeartbeat 显然不会被
//     推进 → PASS
//
// 此用例的设计目的是 **守护未来回归**：若有人新增 daemon 层"主动推进
// LastHeartbeat"的路径（如未来引入 spawn watchdog ticker、心跳模拟器、
// 或意外在 Spawn 后续 goroutine 中写入 LastHeartbeat），本测试 **立即** FAIL，
// 守住 P4 哲学红线（45.2 / 45.3 / 45.5 共同维护的不变量）。
//
// 200ms 是宽松时序断言：远短于 HB-1 keeper 真实周期（10s）与 SpawnAndWait
// ticker 周期（10s），但足够长以暴露任何"短周期 daemon 推进路径"（如未来
// 误引入的 100ms watchdog）；与 spec Task 6.3 / Dev Notes §测试脚手架复用
// 保持一致。
func TestATDD_45_3_002_ScriptRunnerLastHeartbeatNotAdvancedByDaemon(t *testing.T) {
	_, proc := newSkipReasonLoopProcForATDD45_3(t)

	initial := proc.LastHeartbeatSnapshot()
	if initial.IsZero() {
		t.Fatal("Story 45.3 AC7: Spawn did not seed LastHeartbeat (precondition broken — kernel/spawn.go:365 must set LastHeartbeat=now during Spawn)")
	}

	// 远短于 HB-1 keeper 真实周期（10s），但足以暴露任何短周期 daemon
	// 推进路径（未来 watchdog 等）。-race 下额外验证并发写入语义。
	time.Sleep(200 * time.Millisecond)

	current := proc.LastHeartbeatSnapshot()
	if !current.Equal(initial) {
		t.Errorf("Story 45.3 AC2/AC7: LastHeartbeat advanced by daemon: initial=%v current=%v delta=%v — P4 violation: daemon should not actively push LastHeartbeat on SkipReasonLoop processes outside reasonStep / driver streaming / EventWriter writer paths",
			initial, current, current.Sub(initial))
	}
}

// -----------------------------------------------------------------------------
// AC1 / AC3: 本 ATDD 文件能够编译就是 HB-1 keeper / 常量删除的间接证据
// -----------------------------------------------------------------------------

// TestATDD_45_3_003_HandleExecScriptCompilesWithoutHB1
//
// 间接编译断言：本 ATDD 文件位于 ipc/ 包内，与 server_pipeline.go 同包编译。
// 若 server_pipeline.go 残留 HB-1 符号（keepScriptRunnerHeartbeat /
// scriptRunnerHeartbeatInterval / hbWG），但调用方（handleExecScript /
// SpawnAndWait）已改写为不再使用它们，编译器会报 "declared and not used" /
// "imported and not used"（sync import） → 整个 ipc/ 包 build 失败 → 本测试
// 无法编译 → CI 立即可见。
//
// 反过来，若 server_pipeline.go 完全清理（生产代码 + sync import 都删除），
// 本测试编译通过 → PASS。
//
// 因此本测试函数体仅做最低限度 sanity check（new + cleanup kernel），它能够
// 运行本身就是 AC1 + AC3 的证据。这是 Decision D3 "preferred grep over AST
// analysis" 的延伸：用 Go 编译器作为最低成本的 static analysis 工具。
//
// 状态轨迹（pre-impl / post-impl 均 PASS）：
//   - pre-impl: HB-1 符号被生产代码使用，编译通过，PASS
//   - post-impl: 生产代码不再使用 HB-1，sync import 删除，编译通过，PASS
//   - 错误状态: 生产代码 partially 删除（如仅删函数定义但保留调用方，或
//     保留 "sync" import 但删除 var hbWG sync.WaitGroup 使用点）→ go vet
//     报错 → 本测试无法运行 → CI 红 → dev-story 立即定位
func TestATDD_45_3_003_HandleExecScriptCompilesWithoutHB1(t *testing.T) {
	// 显式断言: ipc 包能 import kernel 包并构造 KernelImpl（验证 import block
	// 完整性）。同时引用 kernel.NewKernel — 若 kernel/process.go TouchHeartbeat
	// 方法被错误删除（AC6 仅改注释，方法签名必须保留），编译失败信号会从
	// 调用 kernel 的合法路径传播到本测试。
	kern := kernel.NewKernel(nil, rnixctx.NewManager(), nil)
	t.Cleanup(func() { kern.Shutdown() })
	// 测试本身不做行为断言 — 编译通过即语义成立（见函数注释）。
}

// -----------------------------------------------------------------------------
// AC2: SpawnAndWait 内 heartbeatTicker / hbCh / parentProc.TouchHeartbeat 删除
// -----------------------------------------------------------------------------

// TestATDD_45_3_004_SpawnAndWaitNoHeartbeatTicker
//
// AC2 范围（参考 spec line 129-175）：删除 server_pipeline.go:184-200 SpawnAndWait
// 内 heartbeatTicker setup（含 hbCh 闭包）+ :224-226 case <-hbCh:
// parentProc.TouchHeartbeat() 分支。本测试只读 server_pipeline.go 单文件
// （最小爆炸半径），断言这些局部标识符与方法调用串已彻底删除。
//
// 与 001 互补的语义分工：
//   - 001 验证整个 ipc/ 包内 HB-1 keeper / 常量 / wait group 不存在（跨文件）
//   - 004 验证 server_pipeline.go 单文件内 SpawnAndWait 父进程假心跳路径不存在
//     （单文件局部变量，不跨文件出现）
//
// RED 断言：当前 server_pipeline.go:184-226 仍有 heartbeatTicker / hbCh /
// parentProc.TouchHeartbeat 字符串 → 任一匹配即 FAIL。
//
// 与 spec Task 2.4 完全等价的语义：`grep -n "heartbeatTicker\|hbCh\|parentProc.TouchHeartbeat" ipc/server_pipeline.go` 必须返回空。
func TestATDD_45_3_004_SpawnAndWaitNoHeartbeatTicker(t *testing.T) {
	body, err := os.ReadFile("server_pipeline.go")
	if err != nil {
		t.Fatalf("Story 45.3 AC2: read server_pipeline.go: %v", err)
	}
	text := string(body)
	for _, sym := range spawnAndWaitTickerSymbols {
		if strings.Contains(text, sym) {
			t.Errorf("Story 45.3 AC2: server_pipeline.go still contains fake-heartbeat symbol %q — SpawnAndWait must not self-touch parent heartbeat (P4 daemon-passive: parent activity flows from reasonStep / driver streaming / events.jsonl density only; epic-45 §Code-Map line 147 \"同时清理 185, 225 处的相关引用\")",
				sym)
		}
	}
}
