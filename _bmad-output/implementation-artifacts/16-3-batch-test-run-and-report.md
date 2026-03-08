# Story 16.3: 批量测试运行与报告

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a 平台构建者,
I want 通过 `rnix agtest [test-file]` 批量运行测试并获得结构化结果报告,
So that 我可以快速了解整体回归状态。

## Acceptance Criteria

1. **Given** 一个或多个测试 YAML 文件
   **When** 用户执行 `rnix agtest tests/`
   **Then** 系统按顺序运行所有测试用例，输出结果报告（通过/失败/跳过 + 失败原因）
   **And** 单个测试用例框架开销 <= 500ms（NFR35）

2. **Given** 测试运行完成
   **When** 存在失败用例
   **Then** 报告中包含每个失败用例的断言类型、期望值、实际值和差异说明

## Tasks / Subtasks

- [x] Task 1: 测试运行器核心类型定义 (AC: #1, #2)
  - [x] 1.1 在 `agtest/runner.go` 中定义 `Runner` 结构体（使用 TestExecutor 接口代替直接 ipc.Client 依赖）
  - [x] 1.2 定义 `CaseResult` 结构体
  - [x] 1.3 定义 `CaseStatus` 类型和常量
  - [x] 1.4 定义 `SuiteResult` 结构体
  - [x] 1.5 定义 `ExecutionResult` 结构体和 `TestExecutor` 接口

- [x] Task 2: 测试运行器实现 (AC: #1)
  - [x] 2.1 实现 `Runner.RunSuite(ctx context.Context, suite *TestSuiteSpec) *SuiteResult`
  - [x] 2.2 按顺序遍历 `suite.Tests`，对每个 `TestCaseSpec` 调用 `Runner.runCase`
  - [x] 2.3 实现 `Runner.runCase(ctx context.Context, tc *TestCaseSpec) CaseResult`
  - [x] 2.4 超时处理：用 `tc.Timeout`（毫秒）或 `Runner.Timeout` 作为 context deadline
  - [x] 2.5 聚合所有 `CaseResult` 到 `SuiteResult`

- [x] Task 3: Syscall 事件收集 (AC: #1, #2)
  - [x] 3.1 在 `ipcExecutor.Execute` 中：spawn 流中不含 syscall 事件，改用 `AttachDebug` 二次连接收集
  - [x] 3.2 在 SpawnAndWatch 的 onEvent 回调中检测 spawn 事件，启动 goroutine 通过 AttachDebug 收集 syscall
  - [x] 3.3 提取 `SyscallEventWire.Syscall` 字符串追加到切片
  - [x] 3.4 确认 handleSpawn 不发送 syscall_event，syscall 通过独立 AttachDebug 连接收集

- [x] Task 4: LLM QualityJudge 实现 (AC: #2)
  - [x] 4.1 在 `agtest/judge.go` 中实现 `ParseQualityResponse` 纯函数
  - [x] 4.2 在 `cmd/rnix/agtest.go` 中实现 `ipcQualityJudge`，通过 IPC spawn 评估任务
  - [x] 4.3 评估 intent 包含 output 和 criteria，请求 JSON 返回
  - [x] 4.4 fallback 逻辑：无效 JSON 时检查 "passed": true，支持 markdown code fence 中的 JSON
  - [x] 4.5 默认使用 haiku 模型，超时 30 秒

- [x] Task 5: CLI 集成与报告输出 (AC: #1, #2)
  - [x] 5.1 修改 `cmd/rnix/agtest.go` 中的 `runAgtest` 函数：当非 `--dry-run` 时执行测试
  - [x] 5.2 连接 IPC daemon（通过 dialFunc），失败则报错
  - [x] 5.3 构造 `Runner{Executor, Judge, Timeout}` 并调用 `RunSuite`
  - [x] 5.4 实现纯文本报告输出函数 `agtestTextReport`（✓/✗ 符号、失败断言详情）
  - [x] 5.5 实现 JSON 报告输出 `agtestJSONReport`（JSONResponse 包装）
  - [x] 5.6 当有失败用例时设置 `exitCode = 1`
  - [x] 5.7 添加 `--timeout` flag 设置全局超时（默认 60000ms）

- [x] Task 6: 测试 (AC: #1, #2)
  - [x] 6.1 `agtest/runner_test.go`：Runner.RunSuite — 全部通过（MockExecutor）
  - [x] 6.2 `agtest/runner_test.go`：Runner.RunSuite — 混合通过/失败/错误
  - [x] 6.3 `agtest/runner_test.go`：Runner.runCase — spawn 错误返回 error 状态
  - [x] 6.4 `agtest/runner_test.go`：Runner.runCase — 超时场景
  - [x] 6.5 `agtest/runner_test.go`：Runner.runCase — 无断言按 exit code 判定（零/非零）
  - [x] 6.6 `agtest/runner_test.go`：Runner.runCase — syscall 事件正确收集
  - [x] 6.7 `agtest/runner_test.go`：SuiteResult 聚合计数正确
  - [x] 6.8 `agtest/judge_test.go`：ParseQualityResponse — 有效/无效/fallback/fence 4 个场景
  - [x] 6.9 `cmd/rnix/agtest_test.go`：纯文本报告格式验证（全通过、含失败）
  - [x] 6.10 `cmd/rnix/agtest_test.go`：JSON 报告格式验证
  - [x] 6.11 `cmd/rnix/agtest_test.go`：--timeout flag 解析、daemon 不可用友好报错

## Dev Notes

### 架构决策

本 story 是 Epic 16（agtest）的最终集成层，将 16-1 的解析和 16-2 的断言评估连接到真实的测试执行流程。核心设计原则：

1. **通过 IPC 运行测试** — agtest CLI 通过 IPC daemon 执行测试，复用已有的 `ipc.Client.SpawnAndWatch` 机制。不直接创建 kernel 实例（避免 CLI 包对 kernel 的直接依赖）。
2. **Runner 在 agtest 包中** — 测试运行逻辑放在 `agtest/runner.go`，CLI 只负责构造 Runner 和渲染报告，保持 cmd/rnix 薄壳模式。
3. **顺序执行** — 测试用例按定义顺序依次执行，不并行。理由：(a) LLM 调用成本高，并行可能超过 token 限制；(b) 顺序执行便于结果可复现。
4. **报告格式与已有 CLI 一致** — 纯文本用 `✓`/`✗` 符号，JSON 用 `JSONResponse{OK, Data}` 统一包装。

### 关键设计：测试执行流程

```
CLI: rnix agtest tests/
        |
        v
ParseDir("tests/") → TestSuiteSpec
        |
        v
Runner.RunSuite(ctx, suite) 
        |
        v
for each TestCaseSpec:
  +-- Runner.runCase(ctx, tc)
  |     +-- ipc.SpawnRequest{Intent, Agent, Model, TimeoutMs, ContextBudget}
  |     +-- client.SpawnAndWatch(req, collectSyscalls)
  |     +-- wait for complete/error
  |     +-- TestResult{Output: final.Result, Syscalls: collected, Duration}
  |     +-- EvalAssertions(ctx, result, tc.Assert, judge)
  |     +-- → CaseResult{Status, Assertions, ...}
  |
  +-- aggregate → SuiteResult
        |
        v
agtestReport(w, suiteResult) / agtestJSONReport(w, suiteResult)
```

### 关键设计：Syscall 事件收集

`SpawnAndWatch` 的 onEvent 回调接收所有 `StreamEvent`。查看 `ipc/server.go` 的 `handleSpawn` 实现，spawn 流中会自动发送 `StreamSyscallEvent` 和 `StreamLogEntry`。因此不需要额外的 `AttachDebug` 调用。

在 onEvent 中过滤 `ev.Type == StreamSyscallEvent`，解析 `SyscallEventWire`，提取 `.Syscall` 字段名到切片即可。

### 关键设计：QualityJudge 实现

`LLMQualityJudge` 通过 IPC spawn 一个短任务来评估质量。这意味着评估 LLM 调用也走 daemon，与正常 agent spawn 一致。评估 agent 不需要 skills 和 tools，只需要轻量模型做文本分析。

```go
func (j *LLMQualityJudge) Judge(ctx context.Context, output, criteria string) (*QualityResult, error) {
    intent := fmt.Sprintf("评估以下输出是否满足标准。\n\n输出:\n%s\n\n标准:\n%s\n\n请以JSON返回: {\"passed\": bool, \"score\": 0.0-1.0, \"reason\": \"...\"}", output, criteria)
    req := ipc.SpawnRequest{Intent: intent, Model: j.Model, TimeoutMs: 30000}
    _, final, err := j.Client.SpawnAndWatch(req, func(_ ipc.StreamEvent) {})
    // 解析 final.Result 为 QualityResult JSON
}
```

注意：`LLMQualityJudge` 不需要 mock daemon — 测试中直接使用 `MockQualityJudge`（16-2 已有）。`judge_test.go` 只测试 JSON 解析逻辑和 fallback 行为。

### 关键设计：CaseStatus 判定逻辑

```
if spawnErr != nil → StatusError
else if tc.Assert != nil:
    if all assertions passed → StatusPassed
    else → StatusFailed
else (no assertions):
    if final.ExitCode == 0 → StatusPassed
    else → StatusError (with ExitReason)
```

### 关键设计：超时传播

每个测试用例的超时优先级：
1. `tc.Timeout`（测试用例级别，毫秒）
2. `Runner.Timeout`（Runner 级别全局默认）
3. CLI `--timeout` flag（默认 60000ms）

通过 `context.WithTimeout` 传入 `runCase`，同时设置 `SpawnRequest.TimeoutMs`。

### 关键复用点

1. **IPC SpawnAndWatch** — 复用 `ipc.Client.SpawnAndWatch`，与 `cmd/rnix/main.go` 中 `runSpawn` 相同的模式
2. **SpawnRequest 字段映射** — Agent → tc.Agent.Name, Model → tc.Agent.Model, ContextBudget → tc.Agent.ContextBudget, TimeoutMs → tc.Timeout
3. **EvalAssertions** — 直接复用 16-2 的 `agtest.EvalAssertions`
4. **JSONResponse** — 复用 `cmd/rnix` 的 `JSONResponse{OK, Data, Error}` JSON 包装
5. **agtestError** — 复用已有的错误输出函数
6. **ProgressPayload 解析** — 复用 `ipc.ProgressPayload` 结构，与 main.go 中 onEvent 回调相同模式
7. **SyscallEventWire 解析** — 复用 `ipc.SyscallEventWire`，从 `StreamEvent.Payload` 反序列化

### 不要做的事情

- **不要**在 CLI 中直接创建 Kernel 实例 — 通过 IPC daemon 执行
- **不要**并行执行测试用例 — 顺序执行，保证可复现性
- **不要**修改 `agtest/types.go` 中已有类型 — Runner 类型在新文件 `runner.go`
- **不要**修改 `agtest/eval.go` — 直接使用已有的 `EvalAssertions`
- **不要**修改 `agtest/parser.go` — 直接使用已有的 `ParseFile`/`ParseDir`
- **不要**创建新的 IPC 方法 — 完全复用 `MethodSpawn` + `SpawnAndWatch`
- **不要**创建 `.rnix/tests/` 结果缓存目录 — 本 story 只做实时报告，缓存留给后续优化
- **不要**实现并行测试执行 — MVP 顺序执行足够
- **不要**实现 `--filter` 或 `--skip` 参数 — 未来增强
- **不要**修改 ipc/ 包的任何文件

### 组合矩阵

| 本功能 | 交互对象 | 交互方式 | 需验证 |
|--------|----------|----------|--------|
| Runner.runCase | ipc.Client.SpawnAndWatch | 通过 IPC spawn 测试用例中的 agent，收集结果 | 是 |
| Runner.runCase | agtest.EvalAssertions | 传入 TestResult 和 Assert 配置进行评估 | 是 |
| LLMQualityJudge | ipc.Client.SpawnAndWatch | 通过 IPC spawn 评估任务 | 是（mock） |
| CLI agtest | ipc.Dial | 连接 daemon，失败时报错 | 是 |
| CLI agtest | agtest.ParseFile/ParseDir | 解析 YAML 文件 | 已有测试 |
| CLI --json | JSONResponse | 复用已有 JSON 包装格式 | 是 |
| CLI --timeout | Runner.Timeout | 传递全局超时到 Runner | 是 |
| CaseResult.Syscalls | StreamSyscallEvent | 从 spawn 流中收集 syscall 名称 | 是 |

### Project Structure Notes

新建文件：
- `agtest/runner.go` — Runner、CaseResult、CaseStatus、SuiteResult、RunSuite、runCase
- `agtest/runner_test.go` — Runner 单元测试（mock IPC client）
- `agtest/judge.go` — LLMQualityJudge 实现
- `agtest/judge_test.go` — LLMQualityJudge JSON 解析测试

修改文件：
- `cmd/rnix/agtest.go` — 扩展 runAgtest 实现完整执行流程、报告输出、--timeout flag

### References

- [Source: agtest/eval.go] — EvalAssertions、TestResult、AssertionResult、QualityJudge 接口
- [Source: agtest/types.go] — TestSuiteSpec、TestCaseSpec、AgentConfig、AssertConfig
- [Source: agtest/parser.go] — ParseFile、ParseDir
- [Source: cmd/rnix/agtest.go] — 现有 CLI 命令、agtestDryRunOutput、agtestError
- [Source: cmd/rnix/main.go:405-481] — SpawnAndWatch 使用模式、ProgressPayload 解析模式
- [Source: ipc/protocol.go:60-70] — SpawnRequest 字段定义
- [Source: ipc/protocol.go:245-290] — StreamEvent、StreamEventType、ProgressPayload
- [Source: ipc/protocol.go:292-303] — SyscallEventWire 结构
- [Source: ipc/client.go:105-155] — SpawnAndWatch 客户端方法
- [Source: _bmad-output/planning-artifacts/epics/epic-16-推理回归测试-reasoning-regression-testing-agtest.md] — Epic 16 原始定义
- [Source: _bmad-output/planning-artifacts/prd/functional-requirements.md:162] — FR89: 批量运行测试
- [Source: _bmad-output/planning-artifacts/prd/non-functional-requirements.md:65] — NFR35: 框架开销 ≤ 500ms
- [Source: _bmad-output/planning-artifacts/architecture/implementation-patterns-consistency-rules.md:245] — .rnix/tests/ 路径约定

### 技术栈

- Go 1.26 标准库
- `context` — 超时控制
- `encoding/json` — JSON 报告和 SyscallEventWire 解析
- `time` — 超时和耗时计算
- 无新增外部依赖

### 前置 Story 学习总结

**来自 16-1（声明式测试用例定义）：**
1. ParseFile/ParseDir/ParseBytes 三层模式已稳定，直接复用
2. TestCaseSpec 的 Agent、Timeout 字段直接映射到 SpawnRequest
3. `goccy/go-yaml` 不引入新依赖

**来自 16-2（三种断言类型）：**
1. EvalAssertions 编排三种断言类型，返回 `[]AssertionResult` — Runner 直接调用
2. QualityJudge 接口 + MockQualityJudge 已就绪
3. AssertionResult{Type, Passed, Message, Expected, Actual} 结构完整，报告直接展示

**来自 Git 分析（最近 commit）：**
- commit 模式：`feat: complete story X-Y - description`
- 16-2 commit: `feat: complete story 16-2 - three assertion types`
- 16-1 commit: `feat: story 16-1 done`

**来自 Epic 15 回顾：**
1. IPC 扩展标准四步流程 — 本 story 不需要 IPC 扩展，完全复用 MethodSpawn
2. CLI 薄壳 + 包内逻辑分离模式 — Runner 在 agtest/，CLI 在 cmd/rnix/

### 性能考量

- **NFR35**：单个测试用例框架开销（不含 LLM 调用）≤ 500ms
- Runner 框架开销：IPC 连接建立（~10ms）+ SpawnRequest 序列化（~1ms）+ 断言评估（~1ms）+ 报告生成（~1ms）→ 远低于 500ms
- 真正的时间消耗在 LLM 调用，不计入框架开销
- 质量断言的 LLM 评估额外增加一次 spawn 开销（不计入 NFR35）

### Mock 测试策略

Runner 测试需要 mock IPC client。推荐方式：
1. 将 `Runner.Client` 改为接口类型 `SpawnClient`，包含 `SpawnAndWatch` 方法
2. 实现 `MockSpawnClient` 用于测试，可配置返回的 PID、ProgressPayload、error
3. 这样 runner_test.go 无需启动真实 daemon

```go
type SpawnClient interface {
    SpawnAndWatch(req ipc.SpawnRequest, onEvent func(ipc.StreamEvent)) (types.PID, *ipc.ProgressPayload, error)
    Close() error
}
```

实际 `ipc.Client` 天然满足此接口（duck typing），无需修改 ipc 包。

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6 (claude-opus-4-6)

### Debug Log References

- agtest 包: 72 个测试全部通过（runner 14 个 + judge 5 个 + eval 21 个 + parser 12 个 + validator 20 个），-race 检测通过
- cmd/rnix 包: agtest CLI 8 个测试全部通过
- 全项目 19 包测试通过，1 个预存 TTY 测试（TestRunTop_NoDaemon）不受影响

### Completion Notes List

- 新建 `agtest/runner.go`：Runner、CaseResult、SuiteResult、CaseStatus、ExecutionResult 类型，TestExecutor 接口
- Runner.RunSuite 按顺序执行所有测试用例，聚合结果到 SuiteResult
- Runner.runCase 通过 TestExecutor 执行 agent，调用 EvalAssertions 评估断言，支持超时传播
- CaseStatus 判定逻辑：spawn 错误 → error，无断言按 exit code 判定，有断言全部通过 → passed / 任一失败 → failed
- 新建 `agtest/judge.go`：ParseQualityResponse 纯函数，支持标准 JSON、markdown fence 内 JSON、fallback 启发式三种解析模式
- 修改 `cmd/rnix/agtest.go`：实现完整执行流程 — ipcExecutor（SpawnAndWatch + AttachDebug 二次连接收集 syscall）、ipcQualityJudge（IPC spawn 评估任务）、纯文本报告（✓/✗ + 失败断言详情）、JSON 报告（JSONResponse 包装）、--timeout flag
- TestExecutor 接口设计使 agtest 包不依赖 ipc 包，IPC 实现在 cmd/rnix 层
- Syscall 收集：spawn 流不含 syscall 事件，通过 SpawnAndWatch 回调检测 spawn 事件后启动 goroutine 用独立连接 AttachDebug 收集
- 无新增外部依赖

### File List

新建文件:
- `agtest/runner.go` — Runner、CaseResult、SuiteResult、CaseStatus、ExecutionResult、TestExecutor、RunSuite、runCase
- `agtest/runner_test.go` — 14 个 Runner 单元测试（MockExecutor）
- `agtest/judge.go` — ParseQualityResponse、qualityResponseJSON
- `agtest/judge_test.go` — 5 个 ParseQualityResponse 测试

修改文件:
- `cmd/rnix/agtest.go` — 完整执行流程、ipcExecutor、ipcQualityJudge、报告输出、--timeout flag
- `cmd/rnix/agtest_test.go` — 5 个新增 CLI 测试
