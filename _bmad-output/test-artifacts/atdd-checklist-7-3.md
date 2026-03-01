---
stepsCompleted:
  - 'step-01-preflight-and-context'
  - 'step-02-generation-mode'
  - 'step-03-test-strategy'
  - 'step-04-generate-tests'
  - 'step-05-checklist'
lastStep: 'step-05-checklist'
lastSaved: '2026-03-01'
workflowType: 'testarch-atdd'
inputDocuments:
  - '_bmad-output/implementation-artifacts/7-3-crux-compose-down-command.md'
  - 'cmd/crux/compose.go'
  - 'cmd/crux/compose_test.go'
  - 'cmd/crux/main_test.go'
  - 'internal/ui/compose.go'
  - 'internal/ui/compose_test.go'
  - 'compose/types.go'
  - 'compose/parser.go'
  - 'ipc/client.go'
  - 'ipc/protocol.go'
  - 'internal/types/types.go'
  - 'vfs/proc.go'
  - 'go.mod'
---

# ATDD Checklist - Epic 7, Story 7.3: crux compose down 命令

**Date:** 2026-03-01
**Author:** Decker
**Primary Test Level:** Unit + Integration

---

## Story Summary

Story 7.3 为 Crux 操作系统实现 `crux compose down` CLI 命令，停止编排中所有运行中的智能体并释放资源。通过解析 compose YAML 获取 agent intent 列表，查询 daemon 进程表，匹配并终止运行中的进程，输出释放汇总。

**As a** 用户
**I want** 通过 `crux compose down` 停止编排中所有智能体并释放资源
**So that** 我可以清理中断的工作流

---

## Acceptance Criteria

1. **AC #1 — compose down 子命令注册**: Given compose down 子命令已注册，When 执行 `crux compose down`，Then 向编排中所有运行中的智能体发送 Kill 信号，And 等待所有进程转为 Dead，And 释放所有资源
2. **AC #2 — 部分完成场景**: Given 部分智能体已完成，部分仍在运行，When 执行 `crux compose down`，Then 仅终止仍在运行的智能体，And 输出释放汇总（终止了 N 个进程，释放了 M 个上下文）

---

## 技术栈检测

- **detected_stack**: `backend`（Go 项目，`go.mod` 存在，无前端指标）
- **test_framework**: Go 标准 `testing` 包 + `-race` 检测
- **test_dir**: `cmd/crux/` (CLI 测试) + `internal/ui/` (UI 组件测试)
- **generation_mode**: AI Generation（后端项目，无浏览器录制需求）

---

## 测试策略

### 测试级别选择

| AC | 测试级别 | 测试文件 | 理由 |
|----|---------|---------|------|
| AC #1 | Unit + Integration | `cmd/crux/compose_test.go` | CLI 子命令注册是单元验证；runComposeDown 通过 IPC 集成测试 |
| AC #1 | Unit | `cmd/crux/compose_test.go` | matchComposeProcesses 辅助函数的纯逻辑单元测试 |
| AC #2 | Unit | `internal/ui/compose_test.go` | 释放汇总 UI 是纯渲染逻辑 |

### 优先级

| 优先级 | 测试 | AC |
|--------|------|-----|
| P0 | compose down 子命令注册 | AC #1 |
| P0 | compose down help 输出 | AC #1 |
| P0 | compose down 仅终止运行中进程 | AC #1, #2 |
| P0 | 进程匹配辅助函数 | AC #1, #2 |
| P0 | 释放汇总渲染 | AC #2 |
| P1 | compose down daemon 未运行 | AC #1 |
| P1 | compose down 文件不存在 | AC #1 |
| P1 | compose down 无匹配进程 | AC #2 |
| P1 | JSON 输出模式 | AC #2 |
| P2 | 释放汇总 quiet 模式 | AC #2 |
| P2 | JSON 空结果 | AC #2 |

---

## Failing Tests Created (RED Phase)

### CLI Tests (10 tests)

**File:** `cmd/crux/compose_test.go` (新增约 280 行)

- **Test:** `TestComposeDownCmd_Registered`
  - **Status:** RED — `composeDownCmd` 未注册
  - **Verifies:** AC #1 — compose down 子命令注册

- **Test:** `TestComposeDown_HelpOutput`
  - **Status:** RED — compose down 命令不存在
  - **Verifies:** AC #1 — help 输出包含 -f flag 说明

- **Test:** `TestComposeDown_FileNotFound`
  - **Status:** RED — `flagComposeDownFile`/`runComposeDown` undefined
  - **Verifies:** AC #1 — 文件不存在返回错误

- **Test:** `TestComposeDown_NoDaemon`
  - **Status:** RED — `flagComposeDownFile`/`runComposeDown` undefined
  - **Verifies:** AC #1 — daemon 未运行时正常退出（exit code 0）

- **Test:** `TestComposeDown_NoMatchingProcesses`
  - **Status:** RED — `flagComposeDownFile`/`runComposeDown` undefined
  - **Verifies:** AC #2 — 无匹配进程时正常退出

- **Test:** `TestComposeDown_KillRunningOnly`
  - **Status:** RED — `flagComposeDownFile`/`runComposeDown` undefined
  - **Verifies:** AC #1, #2 — 仅终止 Running/Created 进程，跳过 Zombie/Dead

- **Test:** `TestComposeDown_JSONOutput`
  - **Status:** RED — `ComposeDownResult`/`ComposeDownEntry`/`RenderComposeDownSummaryJSON` undefined
  - **Verifies:** AC #2 — JSON 格式输出

- **Test:** `TestMatchComposeProcesses_AllRunning`
  - **Status:** RED — `matchComposeProcesses` undefined
  - **Verifies:** AC #1 — 全部运行中进程匹配

- **Test:** `TestMatchComposeProcesses_MixedStates`
  - **Status:** RED — `matchComposeProcesses` undefined
  - **Verifies:** AC #2 — 混合状态进程分类

- **Test:** `TestMatchComposeProcesses_NoMatch`
  - **Status:** RED — `matchComposeProcesses` undefined
  - **Verifies:** AC #2 — 无匹配 intent 进程

### UI Tests (5 tests)

**File:** `internal/ui/compose_test.go` (新增约 170 行)

- **Test:** `TestRenderComposeDownSummary`
  - **Status:** RED — `ComposeDownEntry`/`RenderComposeDownSummary` undefined
  - **Verifies:** AC #2 — 终端模式释放汇总

- **Test:** `TestRenderComposeDownSummary_NoKills`
  - **Status:** RED — `ComposeDownEntry`/`RenderComposeDownSummary` undefined
  - **Verifies:** AC #2 — 全部已完成时汇总

- **Test:** `TestRenderComposeDownSummary_QuietMode`
  - **Status:** RED — `ComposeDownEntry`/`RenderComposeDownSummary` undefined
  - **Verifies:** AC #2 — quiet 模式不输出

- **Test:** `TestRenderComposeDownSummaryJSON`
  - **Status:** RED — `ComposeDownEntry`/`RenderComposeDownSummaryJSON` undefined
  - **Verifies:** AC #2 — JSON 格式释放汇总

- **Test:** `TestRenderComposeDownSummaryJSON_Empty`
  - **Status:** RED — `ComposeDownEntry`/`RenderComposeDownSummaryJSON` undefined
  - **Verifies:** AC #2 — 空结果 JSON

---

## Implementation Checklist

### Test: TestComposeDownCmd_Registered / TestComposeDown_HelpOutput

**File:** `cmd/crux/compose_test.go`

**Tasks to make these tests pass:**

- [ ] 在 `cmd/crux/compose.go` 中添加 `composeDownCmd` 声明
- [ ] 添加 `-f/--file` flag（`flagComposeDownFile`，默认 `crux-compose.yaml`）
- [ ] 在现有 `init()` 中添加 `composeCmd.AddCommand(composeDownCmd)`
- [ ] Run test: `go test ./cmd/crux/ -run 'TestComposeDownCmd_Registered|TestComposeDown_Help' -race`
- [ ] Tests pass (green phase)

---

### Test: TestComposeDown_FileNotFound

**File:** `cmd/crux/compose_test.go`

**Tasks to make these tests pass:**

- [ ] 实现 `runComposeDown(cmd *cobra.Command, args []string) error`
- [ ] 调用 `compose.ParseFile(flagComposeDownFile)` 解析 YAML
- [ ] 文件不存在时设置 exitCode 并返回
- [ ] Run test: `go test ./cmd/crux/ -run TestComposeDown_FileNotFound -race`
- [ ] Test passes (green phase)

---

### Test: TestComposeDown_NoDaemon

**File:** `cmd/crux/compose_test.go`

**Tasks to make these tests pass:**

- [ ] runComposeDown 中使用 `ipc.Dial(ipc.SocketPath())` 连接 daemon（不用 EnsureDaemon）
- [ ] 连接失败时输出 "No daemon running, nothing to stop"，exitCode = 0
- [ ] Run test: `go test ./cmd/crux/ -run TestComposeDown_NoDaemon -race`
- [ ] Test passes (green phase)

---

### Test: TestComposeDown_NoMatchingProcesses

**File:** `cmd/crux/compose_test.go`

**Tasks to make these tests pass:**

- [ ] runComposeDown 中调用 `client.ListProcs()` 获取进程列表
- [ ] 调用 `matchComposeProcesses()` 匹配 compose spec 中的 agent
- [ ] 无匹配进程时输出 "No matching processes found"，exitCode = 0
- [ ] Run test: `go test ./cmd/crux/ -run TestComposeDown_NoMatchingProcesses -race`
- [ ] Test passes (green phase)

---

### Test: TestMatchComposeProcesses_AllRunning / TestMatchComposeProcesses_MixedStates / TestMatchComposeProcesses_NoMatch

**File:** `cmd/crux/compose_test.go`

**Tasks to make these tests pass:**

- [ ] 在 `cmd/crux/compose.go` 中实现 `matchComposeProcesses(procs []vfs.ProcInfo, spec *compose.ComposeSpec) (running []vfs.ProcInfo, completed []vfs.ProcInfo)`
- [ ] 遍历 spec.Agents 收集所有 intent
- [ ] 遍历 procs，匹配 intent，根据 State 分类到 running 或 completed
- [ ] Running/Created 状态归入 running；Zombie/Dead 状态归入 completed
- [ ] Run test: `go test ./cmd/crux/ -run TestMatchComposeProcesses -race`
- [ ] Tests pass (green phase)

---

### Test: TestComposeDown_KillRunningOnly

**File:** `cmd/crux/compose_test.go`

**Tasks to make these tests pass:**

- [ ] runComposeDown 中对 matchComposeProcesses 返回的 running 进程逐一调用 `client.Kill(pid, types.SIGTERM)`
- [ ] 收集 Kill 错误但继续终止其他进程（best-effort）
- [ ] Run test: `go test ./cmd/crux/ -run TestComposeDown_KillRunningOnly -race`
- [ ] Test passes (green phase)

---

### Test: TestRenderComposeDownSummary / TestRenderComposeDownSummary_NoKills / TestRenderComposeDownSummary_QuietMode

**File:** `internal/ui/compose_test.go`

**Tasks to make these tests pass:**

- [ ] 在 `internal/ui/compose.go` 中定义 `ComposeDownEntry` 结构体（PID, Intent, State 字段）
- [ ] 实现 `RenderComposeDownSummary(r *Renderer, killed []ComposeDownEntry, skipped []ComposeDownEntry)`
- [ ] 每行输出格式：`[compose] PID N: killed (SIGTERM) — "intent"` 或 `[compose] PID N: skipped (already completed) — "intent"`
- [ ] 汇总行：`[compose] Teardown complete: N killed, M skipped`
- [ ] quiet 模式不输出
- [ ] Run test: `go test ./internal/ui/ -run TestRenderComposeDownSummary -race`
- [ ] Tests pass (green phase)

---

### Test: TestRenderComposeDownSummaryJSON / TestRenderComposeDownSummaryJSON_Empty

**File:** `internal/ui/compose_test.go`

**Tasks to make these tests pass:**

- [ ] 实现 `RenderComposeDownSummaryJSON(r *Renderer, killed []ComposeDownEntry, skipped []ComposeDownEntry)`
- [ ] JSON 结构：`{ok: true, data: {killed: [...], skipped: [...], summary: {killed_count, skipped_count, total_matched}}}`
- [ ] Run test: `go test ./internal/ui/ -run TestRenderComposeDownSummaryJSON -race`
- [ ] Tests pass (green phase)

---

### Test: TestComposeDown_JSONOutput

**File:** `cmd/crux/compose_test.go`

**Tasks to make these tests pass:**

- [ ] 在 `cmd/crux/compose.go` 中定义 `ComposeDownResult` 结构体（Killed, Skipped 字段）
- [ ] runComposeDown 中根据 outputMode 调用 RenderComposeDownSummary 或 RenderComposeDownSummaryJSON
- [ ] Run test: `go test ./cmd/crux/ -run TestComposeDown_JSONOutput -race`
- [ ] Test passes (green phase)

---

## Running Tests

```bash
# Run all failing tests for this story (will fail to compile until implementation exists)
go test ./cmd/crux/ -run TestComposeDown -race -v
go test ./cmd/crux/ -run TestMatchComposeProcesses -race -v
go test ./internal/ui/ -run TestRenderComposeDown -race -v

# Run specific test groups
go test ./cmd/crux/ -run TestComposeDownCmd_Registered -race -v   # 子命令注册
go test ./cmd/crux/ -run TestComposeDown_Help -race -v            # help 输出
go test ./cmd/crux/ -run TestComposeDown_FileNotFound -race -v    # 文件不存在
go test ./cmd/crux/ -run TestComposeDown_NoDaemon -race -v        # daemon 未运行
go test ./cmd/crux/ -run TestComposeDown_KillRunning -race -v     # 仅终止运行中进程
go test ./cmd/crux/ -run TestMatchComposeProcesses -race -v       # 进程匹配辅助函数
go test ./internal/ui/ -run TestRenderComposeDownSummary -race -v # 释放汇总渲染
go test ./internal/ui/ -run TestRenderComposeDownSummaryJSON -race -v # JSON 汇总

# Run all project tests (including regression)
make test

# Run with coverage
go test ./cmd/crux/ -run 'TestComposeDown|TestMatchCompose' -race -coverprofile=compose-down-cmd.out
go test ./internal/ui/ -run TestRenderComposeDown -race -coverprofile=compose-down-ui.out

# Run with verbose timing
go test ./cmd/crux/ -run 'TestComposeDown|TestMatchCompose' -race -v -count=1
```

---

## Red-Green-Refactor Workflow

### RED Phase (Complete)

**TEA Agent Responsibilities:**

- All 15 tests written and failing (compilation errors due to missing implementation)
- Tests follow existing project conventions (standard `testing` package, bytes.Buffer for output capture)
- Tests cover all 2 acceptance criteria
- 进程匹配逻辑抽取为独立可测试函数 matchComposeProcesses
- UI 测试与现有 `compose_test.go`（Story 7.2）风格一致

**Verification:**

- All tests fail to compile (`undefined` errors for unimplemented types and functions)
- Failure is due to missing implementation, not test bugs
- Existing tests are unaffected（compose package tests pass, Story 7.2 tests pass）

---

### GREEN Phase (DEV Team - Next Steps)

**DEV Agent Responsibilities:**

1. **Start with CLI scaffolding**: 在 `cmd/crux/compose.go` 中添加 composeDownCmd 声明和注册
2. **Implement matchComposeProcesses**: 进程匹配辅助函数
3. **Implement UI**: 在 `internal/ui/compose.go` 中添加 ComposeDownEntry + RenderComposeDownSummary + RenderComposeDownSummaryJSON
4. **Implement runComposeDown**: 组装完整执行流程
5. **Run tests incrementally**: `go test ./cmd/crux/ -run TestComposeDownCmd_Registered -race` first

**Key Principles:**

- One test at a time (don't try to fix all at once)
- Minimal implementation (don't over-engineer)
- Run tests frequently (immediate feedback with `-race`)
- 不修改 `compose/` 包（Story 7.3 仅在 `cmd/crux/` 和 `internal/ui/` 层添加代码）
- compose down 不使用 EnsureDaemon（daemon 未运行时正常退出）
- Kill 是 best-effort（某个 Kill 失败继续终止其他进程）

---

### REFACTOR Phase (DEV Team - After All Tests Pass)

1. Verify all tests pass: `go test ./cmd/crux/ -run 'TestComposeDown|TestMatchCompose' -race && go test ./internal/ui/ -run TestRenderComposeDown -race`
2. Run lint: `make lint`
3. Verify full suite: `make test`
4. Build: `make build`
5. Check no regression on Epic 1-7.2

---

## Test Execution Evidence

### Initial Test Run (RED Phase Verification)

**Command:** `go test ./cmd/crux/ -run 'TestComposeDown|TestMatchCompose' -race`

**Results:**

```
# github.com/gonewx/crux/cmd/crux [github.com/gonewx/crux/cmd/crux.test]
cmd/crux/compose_test.go:604:15: undefined: flagComposeDownFile
cmd/crux/compose_test.go:607:3: undefined: flagComposeDownFile
cmd/crux/compose_test.go:610:2: undefined: flagComposeDownFile
cmd/crux/compose_test.go:613:9: undefined: runComposeDown
cmd/crux/compose_test.go:638:15: undefined: flagComposeDownFile
cmd/crux/compose_test.go:642:3: undefined: flagComposeDownFile
cmd/crux/compose_test.go:647:2: undefined: flagComposeDownFile
cmd/crux/compose_test.go:652:9: undefined: runComposeDown
cmd/crux/compose_test.go:686:15: undefined: flagComposeDownFile
cmd/crux/compose_test.go:689:3: undefined: flagComposeDownFile
cmd/crux/compose_test.go:689:3: too many errors
FAIL    github.com/gonewx/crux/cmd/crux [build failed]
```

**Command:** `go test ./internal/ui/ -run TestRenderComposeDown -race`

**Results:**

```
# github.com/gonewx/crux/internal/ui [github.com/gonewx/crux/internal/ui.test]
internal/ui/compose_test.go:345:14: undefined: ComposeDownEntry
internal/ui/compose_test.go:349:15: undefined: ComposeDownEntry
internal/ui/compose_test.go:353:2: undefined: RenderComposeDownSummary
internal/ui/compose_test.go:379:15: undefined: ComposeDownEntry
internal/ui/compose_test.go:384:2: undefined: RenderComposeDownSummary
internal/ui/compose_test.go:404:14: undefined: ComposeDownEntry
internal/ui/compose_test.go:408:2: undefined: RenderComposeDownSummary
internal/ui/compose_test.go:424:14: undefined: ComposeDownEntry
internal/ui/compose_test.go:428:15: undefined: ComposeDownEntry
internal/ui/compose_test.go:432:2: undefined: RenderComposeDownSummaryJSON
internal/ui/compose_test.go:432:2: too many errors
FAIL    github.com/gonewx/crux/internal/ui [build failed]
```

**Summary:**

- Total tests: 15 (10 CLI + 5 UI)
- Passing: 0 (expected — compilation fails)
- Failing: 15 (expected — implementation not yet written)
- Status: RED phase verified

**Existing tests unaffected:**

```
ok      github.com/gonewx/crux/compose   (cached) (all Story 7.1 tests pass)
```

**Expected Failure Messages:**
- `undefined: flagComposeDownFile` — compose down 变量未声明
- `undefined: runComposeDown` — compose down 执行函数未实现
- `undefined: ComposeDownResult` — compose down 结果类型未定义
- `undefined: ComposeDownEntry` — compose down 条目类型未定义
- `undefined: matchComposeProcesses` — 进程匹配辅助函数未实现
- `undefined: RenderComposeDownSummary` — 终端汇总渲染函数未实现
- `undefined: RenderComposeDownSummaryJSON` — JSON 汇总渲染函数未实现

---

## Notes

- Go ATDD 的 RED 阶段表现为编译失败（`undefined` 错误），而非运行时失败。这是因为 Go 编译整个包，新测试文件引用的未实现类型和函数会导致编译错误。
- Story 7.3 的核心是 CLI + UI 层，**不修改 `compose/` 包**。所有 compose YAML 解析逻辑来自 Story 7.1 的 `compose.ParseFile()`。
- compose down **不使用 EnsureDaemon**：如果 daemon 没运行，说明没有进程需要终止，直接正常退出（exit code 0）。
- `matchComposeProcesses` 是关键辅助函数：通过 intent 匹配 compose spec 中的 agent 与 daemon 进程表。
- Kill 是 best-effort：某个进程 Kill 失败时继续终止其他进程，最终汇总所有错误。
- 与 Story 7.2 共享代码模式：复用 `compose.ParseFile()`、IPC `Client.Kill()`/`Client.ListProcs()`、`ui.KernelStyle`、`resolveOutputMode`。
- `-f/--file` flag 使用独立变量 `flagComposeDownFile`（避免与 compose up 的 `flagComposeFile` 冲突）。
- UI 测试风格与 Story 7.2 完全一致：bytes.Buffer + InitStyles(no color) + Renderer。

---

## New Types to Create

```go
// cmd/crux/compose.go
var composeDownCmd *cobra.Command   // compose down 子命令
var flagComposeDownFile string      // compose down 的 -f flag

type ComposeDownResult struct {
    Killed  []ComposeDownEntry
    Skipped []ComposeDownEntry
}

type ComposeDownEntry struct {
    PID    types.PID
    Intent string
    State  string
}

func runComposeDown(cmd *cobra.Command, args []string) error
func matchComposeProcesses(procs []vfs.ProcInfo, spec *compose.ComposeSpec) (running []vfs.ProcInfo, completed []vfs.ProcInfo)

// internal/ui/compose.go
type ComposeDownEntry struct {
    PID    uint64
    Intent string
    State  string
}

func RenderComposeDownSummary(r *Renderer, killed []ComposeDownEntry, skipped []ComposeDownEntry)
func RenderComposeDownSummaryJSON(r *Renderer, killed []ComposeDownEntry, skipped []ComposeDownEntry)
```

## Files to Modify

```
cmd/crux/compose.go            # 添加 composeDownCmd 注册 + runComposeDown + matchComposeProcesses
internal/ui/compose.go          # 添加 ComposeDownEntry + RenderComposeDownSummary/JSON
```

## Dependencies

```
cmd/crux/compose.go → compose/          （ParseFile，获取 agent intent 列表）
cmd/crux/compose.go → ipc/              （Client、Dial、Kill、ListProcs）
cmd/crux/compose.go → internal/types/   （PID、ProcessState、SIGTERM）
cmd/crux/compose.go → internal/ui/      （RenderComposeDownSummary/JSON）
cmd/crux/compose.go → vfs/              （ProcInfo 类型）
internal/ui/compose.go → (无新依赖)     （ComposeDownEntry 为 UI 包内部类型）
```

---

## Next Steps

1. **DEV agent 开始实现**：按 Implementation Checklist 顺序，从 CLI 脚手架 -> 进程匹配函数 -> UI 渲染 -> runComposeDown
2. **先注册命令**：在 compose.go 中添加 composeDownCmd 和 flag
3. **再实现辅助函数**：matchComposeProcesses 让匹配测试通过
4. **然后实现 UI**：ComposeDownEntry + RenderComposeDownSummary/JSON
5. **最后组装**：runComposeDown 把所有组件串联起来
6. **全量回归**：所有测试通过后 `make test` 验证无回归
7. **更新 Story 状态**：所有测试通过且 `make all` 成功后标记 Story 7.3 为 done

---

## Knowledge Base References Applied

- **test-quality.md** — Given-When-Then 结构、单一断言、确定性、隔离性
- **test-levels-framework.md** — Unit + Integration 级别选择（后端项目无 E2E）
- **existing test patterns** — `cmd/crux/compose_test.go` (Story 7.2) 的 CLI 测试模式
- **existing test patterns** — `internal/ui/compose_test.go` (Story 7.2) 的 UI 渲染测试模式
- **existing test patterns** — `cmd/crux/main_test.go` 的 setupTestIPCServer、flag 保存恢复模式
- **Story 7.2 ATDD checklist** — 后端 Go 项目 ATDD 模式参考

---

**Generated by BMad TEA Agent** - 2026-03-01
