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
  - '_bmad-output/implementation-artifacts/7-2-rnix-compose-up-command.md'
  - '_bmad-output/planning-artifacts/epics/epic-7-compose-多智能体编排agent-compose.md'
  - 'compose/types.go'
  - 'compose/engine.go'
  - 'compose/parser.go'
  - 'compose/dag.go'
  - 'compose/engine_test.go'
  - 'cmd/rnix/main.go'
  - 'cmd/rnix/main_test.go'
  - 'cmd/rnix/integration_test.go'
  - 'ipc/client.go'
  - 'ipc/protocol.go'
  - 'internal/ui/renderer.go'
  - 'internal/ui/summary.go'
  - 'go.mod'
---

# ATDD Checklist - Epic 7, Story 7.2: rnix compose up 命令

**Date:** 2026-03-01
**Author:** Decker
**Primary Test Level:** Unit + Integration

---

## Story Summary

Story 7.2 为 Rnix 操作系统实现 `rnix compose up` CLI 命令，一键启动编排定义的所有智能体。通过 IPC 适配器模式将 CLI 侧的 compose 引擎（Story 7.1）与 daemon 通信层连接，实现实时进度输出、失败传播和编排汇总。

**As a** 用户
**I want** 通过 `rnix compose up` 一键启动编排定义的所有智能体
**So that** 完整的多智能体工作流一条命令即可运行

---

## Acceptance Criteria

1. **AC #1 — compose up 子命令注册**: `rnix compose up` 读取当前目录的 `rnix-compose.yaml`，按 DAG 顺序 Spawn 所有智能体，实时输出每个智能体的启动和完成状态
2. **AC #2 — 自定义文件**: `rnix compose up -f my-workflow.yaml` 使用指定文件而非默认文件
3. **AC #3 — 失败传播**: 上游智能体退出非零码时，依赖它的下游智能体不启动，输出明确的错误信息
4. **AC #4 — 编排汇总**: 所有智能体完成后显示编排汇总：每个智能体的退出码、token 消耗、耗时

---

## 技术栈检测

- **detected_stack**: `backend`（Go 项目，`go.mod` 存在，无前端指标）
- **test_framework**: Go 标准 `testing` 包 + `-race` 检测
- **test_dir**: `cmd/rnix/` (CLI 测试) + `internal/ui/` (UI 组件测试)
- **generation_mode**: AI Generation（后端项目，无浏览器录制需求）

---

## 测试策略

### 测试级别选择

| AC | 测试级别 | 测试文件 | 理由 |
|----|---------|---------|------|
| AC #1 | Unit + Integration | `cmd/rnix/compose_test.go` | CLI 子命令注册是单元验证；runComposeUp 通过 IPC 集成测试 |
| AC #2 | Unit | `cmd/rnix/compose_test.go` | -f flag 是 CLI 参数解析 |
| AC #3 | Unit | `cmd/rnix/compose_test.go` | 失败传播通过 mock KernelSpawner + compose Engine 验证 |
| AC #4 | Unit | `internal/ui/compose_test.go` | 汇总 UI 是纯渲染逻辑 |

### 优先级

| 优先级 | 测试 | AC |
|--------|------|-----|
| P0 | compose/compose up 子命令注册 | AC #1 |
| P0 | compose up 默认文件读取 | AC #1 |
| P0 | compose up 自定义文件 | AC #2 |
| P0 | 编排汇总渲染 | AC #4 |
| P1 | 失败传播（上游失败，下游不启动） | AC #3 |
| P1 | JSON 输出模式 | AC #4 |
| P1 | IPC 适配器接口合规 | AC #1 |
| P1 | 文件不存在错误处理 | AC #1 |
| P2 | 信号处理（context 取消） | AC #1 |
| P2 | NoDaemon 错误处理 | AC #1 |
| P2 | IPC 适配器边界情况 | AC #1 |
| P2 | 实时进度输出 | AC #1 |

---

## Failing Tests Created (RED Phase)

### CLI Tests (13 tests)

**File:** `cmd/rnix/compose_test.go` (339 lines)

- **Test:** `TestComposeCmd_Registered`
  - **Status:** RED — `composeCmd` 未注册到 `rootCmd`
  - **Verifies:** AC #1 — compose 子命令注册

- **Test:** `TestComposeUpCmd_Registered`
  - **Status:** RED — `composeUpCmd` 未注册
  - **Verifies:** AC #1 — compose up 子命令注册

- **Test:** `TestComposeUp_HelpOutput`
  - **Status:** RED — compose up 命令不存在
  - **Verifies:** AC #1, #2 — help 输出包含 -f flag 说明

- **Test:** `TestComposeUp_DefaultFile`
  - **Status:** RED — `flagComposeFile`/`runComposeUp` undefined
  - **Verifies:** AC #1 — 默认读取 rnix-compose.yaml

- **Test:** `TestComposeUp_CustomFile`
  - **Status:** RED — `flagComposeFile`/`runComposeUp` undefined
  - **Verifies:** AC #2 — -f flag 指定自定义文件

- **Test:** `TestComposeUp_FileNotFound`
  - **Status:** RED — `flagComposeFile`/`runComposeUp` undefined
  - **Verifies:** AC #1 — 文件不存在返回错误

- **Test:** `TestComposeUp_FailurePropagation`
  - **Status:** RED — `RenderComposeSummary` undefined
  - **Verifies:** AC #3 — 上游失败，下游不启动

- **Test:** `TestComposeUp_Summary`
  - **Status:** RED — `RenderComposeSummary` undefined
  - **Verifies:** AC #4 — 编排汇总渲染

- **Test:** `TestComposeUp_JSONOutput`
  - **Status:** RED — `RenderComposeSummaryJSON` undefined
  - **Verifies:** AC #4 — JSON 格式输出

- **Test:** `TestComposeUp_SignalHandling`
  - **Status:** RED — 通过 compose.Engine + context 取消验证
  - **Verifies:** AC #1 — Ctrl+C 信号处理

- **Test:** `TestComposeUp_NoDaemon`
  - **Status:** RED — `flagComposeFile`/`runComposeUp` undefined
  - **Verifies:** AC #1 — daemon 未运行时错误处理

- **Test:** `TestIpcKernelSpawner_ImplementsInterface`
  - **Status:** RED — `ipcKernelSpawner` 类型 undefined
  - **Verifies:** AC #1 — IPC 适配器实现 KernelSpawner 接口

- **Test:** `TestIpcKernelSpawner_Wait_NoChannel`
  - **Status:** RED — `ipcKernelSpawner`/`waitResult` undefined
  - **Verifies:** AC #1 — Wait 未知 PID 返回错误

- **Test:** `TestIpcKernelSpawner_GetProcessResult_NotFound`
  - **Status:** RED — `ipcKernelSpawner` undefined
  - **Verifies:** AC #1 — 结果缓存未命中

- **Test:** `TestIpcKernelSpawner_GetProcessResult_Found`
  - **Status:** RED — `ipcKernelSpawner` undefined
  - **Verifies:** AC #1 — 结果缓存命中

### UI Tests (14 tests)

**File:** `internal/ui/compose_test.go` (301 lines)

- **Test:** `TestRenderComposeSummary_AllSuccess`
  - **Status:** RED — `RenderComposeSummary` undefined
  - **Verifies:** AC #4 — 全部成功的汇总渲染

- **Test:** `TestRenderComposeSummary_WithFailures`
  - **Status:** RED — `RenderComposeSummary` undefined
  - **Verifies:** AC #4 — 包含失败的汇总渲染

- **Test:** `TestRenderComposeSummary_WithSkipped`
  - **Status:** RED — `RenderComposeSummary` undefined
  - **Verifies:** AC #3, #4 — 跳过的智能体汇总

- **Test:** `TestRenderComposeSummary_EmptyResults`
  - **Status:** RED — `RenderComposeSummary` undefined
  - **Verifies:** AC #4 — 空结果边界情况

- **Test:** `TestRenderComposeSummary_QuietMode`
  - **Status:** RED — `RenderComposeSummary` undefined
  - **Verifies:** AC #4 — quiet 模式不输出

- **Test:** `TestRenderComposeSummaryJSON_Valid`
  - **Status:** RED — `RenderComposeSummaryJSON` undefined
  - **Verifies:** AC #4 — JSON 格式合法

- **Test:** `TestRenderComposeSummaryJSON_AgentFields`
  - **Status:** RED — `RenderComposeSummaryJSON` undefined
  - **Verifies:** AC #4 — JSON agent 字段完整

- **Test:** `TestRenderComposeSummaryJSON_EmptyResults`
  - **Status:** RED — `RenderComposeSummaryJSON` undefined
  - **Verifies:** AC #4 — 空结果 JSON

- **Test:** `TestRenderComposeProgress_Spawning`
  - **Status:** RED — `RenderComposeProgress` undefined
  - **Verifies:** AC #1 — 智能体启动实时进度

- **Test:** `TestRenderComposeProgress_Done`
  - **Status:** RED — `RenderComposeProgress` undefined
  - **Verifies:** AC #1 — 智能体完成实时进度

- **Test:** `TestRenderComposeProgress_Failed`
  - **Status:** RED — `RenderComposeProgress` undefined
  - **Verifies:** AC #3 — 失败智能体实时进度

- **Test:** `TestRenderComposeProgress_Skipped`
  - **Status:** RED — `RenderComposeProgress` undefined
  - **Verifies:** AC #3 — 跳过智能体实时进度

- **Test:** `TestRenderComposeProgress_QuietMode`
  - **Status:** RED — `RenderComposeProgress` undefined
  - **Verifies:** AC #1 — quiet 模式不输出进度

---

## Implementation Checklist

### Test: TestComposeCmd_Registered / TestComposeUpCmd_Registered / TestComposeUp_HelpOutput

**File:** `cmd/rnix/compose_test.go`

**Tasks to make these tests pass:**

- [ ] 创建 `cmd/rnix/compose.go`，声明 `composeCmd` 和 `composeUpCmd`
- [ ] 添加 `-f/--file` flag（`flagComposeFile`，默认 `rnix-compose.yaml`）
- [ ] 在 `cmd/rnix/main.go` 的 `init()` 中添加 `rootCmd.AddCommand(composeCmd)`
- [ ] Run test: `go test ./cmd/rnix/ -run 'TestComposeCmd_Registered|TestComposeUpCmd|TestComposeUp_Help' -race`
- [ ] Tests pass (green phase)

---

### Test: TestComposeUp_DefaultFile / TestComposeUp_CustomFile / TestComposeUp_FileNotFound

**File:** `cmd/rnix/compose_test.go`

**Tasks to make these tests pass:**

- [ ] 实现 `runComposeUp(cmd *cobra.Command, args []string) error`
- [ ] 读取 `flagComposeFile` 指定的 YAML 文件（默认 `rnix-compose.yaml`）
- [ ] 调用 `compose.ParseFile()` 解析 YAML
- [ ] 文件不存在时返回错误或设置 exitCode
- [ ] Run test: `go test ./cmd/rnix/ -run 'TestComposeUp_DefaultFile|TestComposeUp_CustomFile|TestComposeUp_FileNotFound' -race`
- [ ] Tests pass (green phase)

---

### Test: TestIpcKernelSpawner_ImplementsInterface / TestIpcKernelSpawner_Wait_NoChannel / TestIpcKernelSpawner_GetProcessResult_*

**File:** `cmd/rnix/compose_test.go`

**Tasks to make these tests pass:**

- [ ] 在 `cmd/rnix/compose.go` 中定义 `ipcKernelSpawner` 结构体
- [ ] 定义 `waitResult` 类型
- [ ] 实现 `compose.KernelSpawner` 接口的三个方法：Spawn/Wait/GetProcessResult
- [ ] Spawn 通过 IPC Client 调用 SpawnAndWatch（每个 agent 独立连接）
- [ ] Wait 从 completion channel 读取结果
- [ ] GetProcessResult 从 xsync.SyncMap 缓存读取
- [ ] Run test: `go test ./cmd/rnix/ -run TestIpcKernelSpawner -race`
- [ ] Tests pass (green phase)

---

### Test: TestComposeUp_FailurePropagation

**File:** `cmd/rnix/compose_test.go`

**Tasks to make these tests pass:**

- [ ] runComposeUp 中正确调用 engine.Execute()
- [ ] 收集 ScheduleResult，上游失败时下游标记为 skipped
- [ ] 输出失败智能体名称和受影响的下游
- [ ] Run test: `go test ./cmd/rnix/ -run TestComposeUp_FailurePropagation -race`
- [ ] Test passes (green phase)

---

### Test: TestComposeUp_NoDaemon

**File:** `cmd/rnix/compose_test.go`

**Tasks to make these tests pass:**

- [ ] runComposeUp 中连接 daemon（通过 ipc.EnsureDaemon 或 ipc.Dial）
- [ ] 连接失败时输出错误信息并设置 exitCode = 1
- [ ] Run test: `go test ./cmd/rnix/ -run TestComposeUp_NoDaemon -race`
- [ ] Test passes (green phase)

---

### Test: TestComposeUp_SignalHandling

**File:** `cmd/rnix/compose_test.go`

**Tasks to make these tests pass:**

- [ ] runComposeUp 中设置 SIGINT/SIGTERM 处理
- [ ] 收到信号时取消 context，触发 engine.Execute 中止
- [ ] Run test: `go test ./cmd/rnix/ -run TestComposeUp_SignalHandling -race`
- [ ] Test passes (green phase)

---

### Test: TestRenderComposeSummary_* (5 tests)

**File:** `internal/ui/compose_test.go`

**Tasks to make these tests pass:**

- [ ] 创建 `internal/ui/compose.go`
- [ ] 实现 `RenderComposeSummary(r *Renderer, results []compose.ScheduleResult)`
- [ ] 表格格式：Agent / Status / Exit / Duration
- [ ] 汇总行：Total / Succeeded / Failed / Skipped
- [ ] quiet 模式不输出
- [ ] Run test: `go test ./internal/ui/ -run TestRenderComposeSummary -race`
- [ ] Tests pass (green phase)

---

### Test: TestRenderComposeSummaryJSON_* (3 tests)

**File:** `internal/ui/compose_test.go`

**Tasks to make these tests pass:**

- [ ] 实现 `RenderComposeSummaryJSON(r *Renderer, results []compose.ScheduleResult)`
- [ ] JSON 结构：`{ok: true, data: {agents: [...], summary: {...}}}`
- [ ] agent 字段：name, status, exit_code, elapsed_ms
- [ ] summary 字段：total, succeeded, failed, skipped
- [ ] Run test: `go test ./internal/ui/ -run TestRenderComposeSummaryJSON -race`
- [ ] Tests pass (green phase)

---

### Test: TestRenderComposeProgress_* (5 tests)

**File:** `internal/ui/compose_test.go`

**Tasks to make these tests pass:**

- [ ] 实现 `RenderComposeProgress(r *Renderer, name string, status string, index int, total int, exitCode int)`
- [ ] 格式：`[compose] [N/M] name: status (exitCode) duration`
- [ ] 支持 spawning/done/failed/skipped 状态
- [ ] quiet 模式不输出
- [ ] Run test: `go test ./internal/ui/ -run TestRenderComposeProgress -race`
- [ ] Tests pass (green phase)

---

### Test: TestComposeUp_Summary / TestComposeUp_JSONOutput

**File:** `cmd/rnix/compose_test.go`

**Tasks to make these tests pass:**

- [ ] runComposeUp 中调用 RenderComposeSummary 或 RenderComposeSummaryJSON
- [ ] 根据 flagJSON 选择渲染模式
- [ ] Run test: `go test ./cmd/rnix/ -run 'TestComposeUp_Summary|TestComposeUp_JSON' -race`
- [ ] Tests pass (green phase)

---

## Running Tests

```bash
# Run all failing tests for this story (will fail to compile until implementation exists)
go test ./cmd/rnix/ -run TestCompose -race -v
go test ./internal/ui/ -run TestRenderCompose -race -v

# Run specific test groups
go test ./cmd/rnix/ -run TestComposeCmd -race -v          # 子命令注册
go test ./cmd/rnix/ -run TestComposeUp_Default -race -v   # 默认文件
go test ./cmd/rnix/ -run TestComposeUp_Custom -race -v    # 自定义文件
go test ./cmd/rnix/ -run TestComposeUp_Failure -race -v   # 失败传播
go test ./cmd/rnix/ -run TestIpcKernelSpawner -race -v    # IPC 适配器
go test ./internal/ui/ -run TestRenderComposeSummary -race -v  # 汇总渲染
go test ./internal/ui/ -run TestRenderComposeProgress -race -v # 进度渲染

# Run all project tests (including regression)
make test

# Run with coverage
go test ./cmd/rnix/ -run TestCompose -race -coverprofile=compose-cmd.out
go test ./internal/ui/ -run TestRenderCompose -race -coverprofile=compose-ui.out

# Run with verbose timing
go test ./cmd/rnix/ -run TestCompose -race -v -count=1
```

---

## Red-Green-Refactor Workflow

### RED Phase (Complete)

**TEA Agent Responsibilities:**

- All 27 tests written and failing (compilation errors due to missing implementation)
- Tests follow existing project conventions (standard `testing` package, bytes.Buffer for output capture)
- Tests cover all 4 acceptance criteria
- IPC 适配器 mock 模式与 Story 7.1 的 mockKernelSpawner 模式一致
- UI 测试与现有 `summary_test.go`/`renderer_test.go` 风格一致

**Verification:**

- All tests fail to compile (`undefined` errors for unimplemented types and functions)
- Failure is due to missing implementation, not test bugs
- Existing tests are unaffected（compose package 测试通过，cmd/rnix 现有测试通过）

---

### GREEN Phase (DEV Team - Next Steps)

**DEV Agent Responsibilities:**

1. **Start with CLI scaffolding**: 创建 `cmd/rnix/compose.go` 声明命令结构
2. **Register commands**: 在 `cmd/rnix/main.go` init() 中注册 composeCmd
3. **Implement UI**: 创建 `internal/ui/compose.go` 实现渲染函数
4. **Implement IPC adapter**: 实现 `ipcKernelSpawner` 适配器
5. **Implement runComposeUp**: 组装完整执行流程
6. **Run tests incrementally**: `go test ./cmd/rnix/ -run TestComposeCmd_Registered -race` first

**Key Principles:**

- One test at a time (don't try to fix all at once)
- Minimal implementation (don't over-engineer)
- Run tests frequently (immediate feedback with `-race`)
- 不修改 `compose/` 包（Story 7.2 仅添加 CLI 层和 UI 层）
- IPC 适配器每个 agent 独立连接（SpawnAndWatch 独占连接）

---

### REFACTOR Phase (DEV Team - After All Tests Pass)

1. Verify all tests pass: `go test ./cmd/rnix/ -run TestCompose -race && go test ./internal/ui/ -run TestRenderCompose -race`
2. Run lint: `make lint`
3. Verify full suite: `make test`
4. Build: `make build`
5. Check no regression on Epic 1-7.1

---

## Test Execution Evidence

### Initial Test Run (RED Phase Verification)

**Command:** `go test ./cmd/rnix/ -run TestCompose -race`

**Results:**

```
# github.com/rnixai/rnix/cmd/rnix [github.com/rnixai/rnix/cmd/rnix.test]
cmd/rnix/compose_test.go:127:15: undefined: flagComposeFile
cmd/rnix/compose_test.go:130:3: undefined: flagComposeFile
cmd/rnix/compose_test.go:133:2: undefined: flagComposeFile
cmd/rnix/compose_test.go:136:9: undefined: runComposeUp
cmd/rnix/compose_test.go:170:15: undefined: flagComposeFile
cmd/rnix/compose_test.go:173:3: undefined: flagComposeFile
cmd/rnix/compose_test.go:176:2: undefined: flagComposeFile
cmd/rnix/compose_test.go:179:9: undefined: runComposeUp
cmd/rnix/compose_test.go:194:15: undefined: flagComposeFile
cmd/rnix/compose_test.go:197:3: undefined: flagComposeFile
cmd/rnix/compose_test.go:197:3: too many errors
FAIL    github.com/rnixai/rnix/cmd/rnix [build failed]
```

**Command:** `go test ./internal/ui/ -run TestRenderCompose -race`

**Results:**

```
# github.com/rnixai/rnix/internal/ui [github.com/rnixai/rnix/internal/ui.test]
internal/ui/compose_test.go:33:2: undefined: RenderComposeSummary
internal/ui/compose_test.go:62:2: undefined: RenderComposeSummary
internal/ui/compose_test.go:90:2: undefined: RenderComposeSummary
internal/ui/compose_test.go:107:2: undefined: RenderComposeSummary
internal/ui/compose_test.go:128:2: undefined: RenderComposeSummary
internal/ui/compose_test.go:149:2: undefined: RenderComposeSummaryJSON
internal/ui/compose_test.go:198:2: undefined: RenderComposeSummaryJSON
internal/ui/compose_test.go:218:2: undefined: RenderComposeSummaryJSON
internal/ui/compose_test.go:241:2: undefined: RenderComposeProgress
internal/ui/compose_test.go:264:2: undefined: RenderComposeProgress
internal/ui/compose_test.go:264:2: too many errors
FAIL    github.com/rnixai/rnix/internal/ui [build failed]
```

**Summary:**

- Total tests: 27 (13 CLI + 14 UI)
- Passing: 0 (expected — compilation fails)
- Failing: 27 (expected — implementation not yet written)
- Status: RED phase verified

**Existing tests unaffected:**

```
ok      github.com/rnixai/rnix/compose   1.127s (all 11 Story 7.1 tests pass)
```

**Expected Failure Messages:**
- `undefined: flagComposeFile` — compose.go 未创建
- `undefined: runComposeUp` — compose.go 未创建
- `undefined: ipcKernelSpawner` — compose.go 未创建
- `undefined: waitResult` — compose.go 未创建
- `undefined: RenderComposeSummary` — internal/ui/compose.go 未创建
- `undefined: RenderComposeSummaryJSON` — internal/ui/compose.go 未创建
- `undefined: RenderComposeProgress` — internal/ui/compose.go 未创建

---

## Notes

- Go ATDD 的 RED 阶段表现为编译失败（`undefined` 错误），而非运行时失败。这是因为 Go 编译整个包，新测试文件引用的未实现类型和函数会导致编译错误。
- Story 7.2 的核心是 CLI + IPC 适配器层，**不修改 `compose/` 包**。所有 compose 引擎逻辑来自 Story 7.1。
- `ipcKernelSpawner` 是关键适配器：每个 agent 使用独立 IPC 连接（因为 SpawnAndWatch 独占连接流）。
- 测试中使用 `mockComposeSpawner` 模拟 IPC 层，避免真实 daemon 依赖。
- UI 测试与现有 `summary_test.go` 风格一致：bytes.Buffer + InitStyles(no color)。
- 命令注册测试（TestComposeCmd_Registered）通过遍历 rootCmd.Commands() 验证，与项目现有模式一致。
- 失败传播测试直接使用 `compose.NewEngine` + mock spawner，验证 Engine 层面的行为正确性。

---

## New Files to Create

```
cmd/rnix/compose.go            # compose 命令注册 + compose up 实现 + IPC 适配器
internal/ui/compose.go          # 编排汇总 UI 组件（RenderComposeSummary/JSON/Progress）
```

## Files to Modify

```
cmd/rnix/main.go               # init() 中添加 rootCmd.AddCommand(composeCmd)（约 1 行）
```

## Dependencies

```
cmd/rnix/compose.go → compose/          （Engine、ParseFile、KernelSpawner）
cmd/rnix/compose.go → ipc/              （Client、EnsureDaemon、SpawnAndWatch）
cmd/rnix/compose.go → internal/ui/      （RenderComposeSummary/JSON/Progress）
cmd/rnix/compose.go → internal/xsync/   （SyncMap for result cache）
internal/ui/compose.go → compose/       （ScheduleResult 类型）
```

---

## Next Steps

1. **DEV agent 开始实现**：按 Implementation Checklist 顺序，从 CLI 脚手架 -> UI 组件 -> IPC 适配器 -> runComposeUp
2. **先注册命令**：创建 compose.go 骨架让命令注册测试通过
3. **再实现 UI**：internal/ui/compose.go 让渲染测试通过
4. **然后实现适配器**：ipcKernelSpawner 让接口和边界测试通过
5. **最后组装**：runComposeUp 把所有组件串联起来
6. **全量回归**：所有测试通过后 `make test` 验证无回归
7. **更新 Story 状态**：所有测试通过且 `make all` 成功后标记 Story 7.2 为 done

---

## Knowledge Base References Applied

- **test-quality.md** — Given-When-Then 结构、单一断言、确定性、隔离性
- **test-levels-framework.md** — Unit + Integration 级别选择（后端项目无 E2E）
- **existing test patterns** — `cmd/rnix/main_test.go` 的 CLI 测试模式（setupTestIPCServer、flag 保存恢复）
- **existing test patterns** — `internal/ui/summary_test.go` 的 UI 渲染测试模式
- **Story 7.1 ATDD checklist** — 后端 Go 项目 ATDD 模式参考
- **compose/engine_test.go** — mock KernelSpawner 模式参考

---

**Generated by BMad TEA Agent** - 2026-03-01
