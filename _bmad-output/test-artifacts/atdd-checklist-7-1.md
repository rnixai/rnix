---
stepsCompleted:
  - 'step-01-preflight-and-context'
  - 'step-02-generation-mode'
  - 'step-03-test-strategy'
  - 'step-04-generate-tests'
  - 'step-05-checklist'
lastStep: 'step-05-checklist'
lastSaved: '2026-02-28'
workflowType: 'testarch-atdd'
inputDocuments:
  - '_bmad-output/implementation-artifacts/7-1-crux-compose-yaml-parsing-and-dag-scheduling-engine.md'
  - 'kernel/kernel.go'
  - 'kernel/process.go'
  - 'kernel/kernel_test.go'
  - 'kernel/process_test.go'
  - 'agents/types.go'
  - 'internal/types/types.go'
  - 'go.mod'
---

# ATDD Checklist - Epic 7, Story 7.1: crux-compose.yaml 解析与 DAG 调度引擎

**Date:** 2026-02-28
**Author:** Decker
**Primary Test Level:** Unit

---

## Story Summary

Story 7.1 为 Crux 操作系统实现声明式多智能体编排能力。用户通过 `crux-compose.yaml` 文件定义多个智能体及其依赖关系，系统自动构建 DAG（有向无环图），检测循环依赖，并按拓扑排序执行调度。

**As a** 用户
**I want** 通过 YAML 文件声明式定义多智能体工作流及其依赖关系
**So that** 系统自动按正确顺序调度执行

---

## Acceptance Criteria

1. **AC #1 — YAML 解析**: 解析 `crux-compose.yaml`，正确提取每个智能体的 `intent`、`agent` 引用、`skills` 列表和 `depends_on` 依赖，构建 DAG
2. **AC #2 — 循环依赖检测**: YAML 中存在循环依赖时返回清晰错误信息，标注循环路径
3. **AC #3 — 拓扑排序调度**: 按拓扑顺序启动智能体，无依赖分支自动并行化，≤ 10 个智能体启动延迟 ≤ 2s（NFR21）
4. **AC #4 — 依赖触发**: 智能体 A 完成后自动启动依赖 B，A 的输出可注入 B 的上下文
5. **AC #5 — YAML 格式支持**: 支持 version、intent、agents（含 agent/skills/depends_on）完整格式

---

## 技术栈检测

- **detected_stack**: `backend`（Go 项目，`go.mod` 存在，无前端指标）
- **test_framework**: Go 标准 `testing` 包 + `-race` 检测
- **test_dir**: `compose/` 包内测试（`*_test.go`）
- **generation_mode**: AI Generation（后端项目，无浏览器录制需求）

---

## 测试策略

### 测试级别选择

| AC | 测试级别 | 理由 |
|----|---------|------|
| AC #1 | Unit | YAML 解析是纯函数，无外部依赖 |
| AC #2 | Unit | DAG 循环检测是纯算法逻辑 |
| AC #3 | Unit | 拓扑排序是纯算法；调度引擎通过 mock KernelSpawner 测试 |
| AC #4 | Unit | 依赖触发和输出传递通过 mock 验证 |
| AC #5 | Unit | YAML 格式验证是纯解析逻辑 |

### 优先级

| 优先级 | 测试 | AC |
|--------|------|-----|
| P0 | ParseBytes 合法 YAML 解析 | AC #1, #5 |
| P0 | ParseBytes 字段验证（version/agents/intent） | AC #1, #5 |
| P0 | BuildDAG 基本构建 | AC #1 |
| P0 | DetectCycle 循环检测 | AC #2 |
| P0 | TopologicalSort 分层排序 | AC #3 |
| P0 | Engine Execute 基本调度 | AC #3, #4 |
| P1 | 依赖引用验证 | AC #1 |
| P1 | Engine 失败传播 | AC #3 |
| P1 | Engine 输出传递 | AC #4 |
| P1 | Engine context 取消 | AC #3 |
| P2 | 10 智能体性能（NFR21） | AC #3 |
| P2 | 边界情况（单节点、空依赖） | AC #1, #3 |

---

## Failing Tests Created (RED Phase)

### Parser Tests (15 tests)

**File:** `compose/parser_test.go` (286 lines)

- **Test:** `TestParseBytes_Valid`
  - **Status:** RED — `ParseBytes` undefined (type not declared)
  - **Verifies:** AC #1, #5 — 合法 YAML 解析，提取 version/intent/agents/depends_on

- **Test:** `TestParseBytes_FullFormat`
  - **Status:** RED — `ParseBytes` undefined
  - **Verifies:** AC #5 — 完整格式支持（含 agent 引用字段）

- **Test:** `TestParseBytes_NoDependencies`
  - **Status:** RED — `ParseBytes` undefined
  - **Verifies:** AC #1 — 无依赖智能体解析

- **Test:** `TestParseFile_Valid`
  - **Status:** RED — `ParseFile` undefined
  - **Verifies:** AC #1 — 从文件路径解析 YAML

- **Test:** `TestParseFile_NotFound`
  - **Status:** RED — `ParseFile` undefined
  - **Verifies:** AC #1 — 文件不存在返回错误

- **Test:** `TestParseBytes_InvalidYAML`
  - **Status:** RED — `ParseBytes` undefined
  - **Verifies:** AC #1 — 无效 YAML 语法返回错误

- **Test:** `TestParseBytes_InvalidVersion`
  - **Status:** RED — `ParseBytes` undefined
  - **Verifies:** AC #1 — version 非 "1.0" 返回错误

- **Test:** `TestParseBytes_MissingVersion`
  - **Status:** RED — `ParseBytes` undefined
  - **Verifies:** AC #1 — 缺少 version 返回错误

- **Test:** `TestParseBytes_EmptyAgents`
  - **Status:** RED — `ParseBytes` undefined
  - **Verifies:** AC #1 — agents 为空返回错误

- **Test:** `TestParseBytes_MissingAgents`
  - **Status:** RED — `ParseBytes` undefined
  - **Verifies:** AC #1 — 缺少 agents 返回错误

- **Test:** `TestParseBytes_AgentMissingIntent`
  - **Status:** RED — `ParseBytes` undefined
  - **Verifies:** AC #1 — agent 缺少 intent 返回错误

- **Test:** `TestParseBytes_DependsOnInvalidRef`
  - **Status:** RED — `ParseBytes` undefined
  - **Verifies:** AC #1 — depends_on 引用不存在的 agent 返回错误

- **Test:** `TestParseBytes_DependsOnInvalidCondition`
  - **Status:** RED — `ParseBytes` undefined
  - **Verifies:** AC #1 — depends_on 使用不支持的条件返回错误

- **Test:** `TestParseBytes_MissingTopLevelIntent`
  - **Status:** RED — `ParseBytes` undefined
  - **Verifies:** AC #1 — 缺少顶层 intent 返回错误

- **Test:** `TestParseBytes_SingleAgent`
  - **Status:** RED — `ParseBytes` undefined
  - **Verifies:** AC #1, #5 — 单个智能体合法解析

### DAG Tests (13 tests)

**File:** `compose/dag_test.go` (322 lines)

- **Test:** `TestBuildDAG_NoDeps`
  - **Status:** RED — `ComposeSpec`/`BuildDAG` undefined
  - **Verifies:** AC #1 — 无依赖 DAG 构建

- **Test:** `TestBuildDAG_LinearDeps`
  - **Status:** RED — `ComposeSpec`/`BuildDAG` undefined
  - **Verifies:** AC #1 — 线性依赖链 A -> B -> C

- **Test:** `TestBuildDAG_DiamondDeps`
  - **Status:** RED — `ComposeSpec`/`BuildDAG` undefined
  - **Verifies:** AC #1 — 菱形依赖 A -> B, A -> C, B -> D, C -> D

- **Test:** `TestDetectCycle_NoCycle`
  - **Status:** RED — `ComposeSpec`/`BuildDAG`/`DetectCycle` undefined
  - **Verifies:** AC #2 — 无环图检测通过

- **Test:** `TestDetectCycle_SimpleCycle`
  - **Status:** RED — `ComposeSpec`/`BuildDAG` undefined
  - **Verifies:** AC #2 — A -> B -> A 循环检测

- **Test:** `TestDetectCycle_ComplexCycle`
  - **Status:** RED — `ComposeSpec`/`BuildDAG` undefined
  - **Verifies:** AC #2 — A -> B -> C -> A 三节点循环

- **Test:** `TestDetectCycle_SelfCycle`
  - **Status:** RED — `ComposeSpec`/`BuildDAG` undefined
  - **Verifies:** AC #2 — 自依赖循环检测

- **Test:** `TestDetectCycle_PartialCycle`
  - **Status:** RED — `ComposeSpec`/`BuildDAG` undefined
  - **Verifies:** AC #2 — 部分节点有环的混合图检测

- **Test:** `TestTopologicalSort_AllParallel`
  - **Status:** RED — `ComposeSpec`/`BuildDAG`/`TopologicalSort` undefined
  - **Verifies:** AC #3 — 全并行拓扑排序（单层）

- **Test:** `TestTopologicalSort_Sequential`
  - **Status:** RED — `ComposeSpec`/`BuildDAG`/`TopologicalSort` undefined
  - **Verifies:** AC #3 — 纯串行拓扑排序

- **Test:** `TestTopologicalSort_Diamond`
  - **Status:** RED — `ComposeSpec`/`BuildDAG`/`TopologicalSort` undefined
  - **Verifies:** AC #3 — 菱形分层排序 [A], [B,C], [D]

- **Test:** `TestTopologicalSort_ComplexGraph`
  - **Status:** RED — `ComposeSpec`/`BuildDAG`/`TopologicalSort` undefined
  - **Verifies:** AC #3 — 复杂图拓扑约束验证

- **Test:** `TestTopologicalSort_SingleNode`
  - **Status:** RED — `ComposeSpec`/`BuildDAG`/`TopologicalSort` undefined
  - **Verifies:** AC #3 — 单节点拓扑排序

### Engine Tests (13 tests)

**File:** `compose/engine_test.go` (420 lines)

- **Test:** `TestNewEngine_Valid`
  - **Status:** RED — `ComposeSpec`/`NewEngine`/`ComposeSpawnOpts` undefined
  - **Verifies:** AC #3 — 引擎构造成功

- **Test:** `TestNewEngine_CyclicSpec`
  - **Status:** RED — `NewEngine` undefined
  - **Verifies:** AC #2 — 循环依赖拒绝

- **Test:** `TestEngine_Execute_NoDeps`
  - **Status:** RED — `NewEngine`/`Execute` undefined
  - **Verifies:** AC #3 — 全并行调度

- **Test:** `TestEngine_Execute_LinearDeps`
  - **Status:** RED — `NewEngine`/`Execute` undefined
  - **Verifies:** AC #3, #4 — 串行依赖调度（验证执行顺序）

- **Test:** `TestEngine_Execute_DiamondDeps`
  - **Status:** RED — `NewEngine`/`Execute` undefined
  - **Verifies:** AC #3, #4 — 菱形依赖正确调度

- **Test:** `TestEngine_Execute_FailurePropagation`
  - **Status:** RED — `NewEngine`/`Execute`/`ScheduleResult` undefined
  - **Verifies:** AC #3 — 上游失败，下游不启动

- **Test:** `TestEngine_Execute_ContextCancel`
  - **Status:** RED — `NewEngine`/`Execute` undefined
  - **Verifies:** AC #3 — context 取消中止调度

- **Test:** `TestEngine_Execute_OutputPassthrough`
  - **Status:** RED — `NewEngine`/`Execute` undefined
  - **Verifies:** AC #4 — 上游输出注入下游上下文

- **Test:** `TestEngine_Execute_Performance`
  - **Status:** RED — `NewEngine`/`Execute` undefined
  - **Verifies:** AC #3/NFR21 — 10 智能体启动延迟 ≤ 2s

- **Test:** `TestEngine_Execute_AgentWithSkills`
  - **Status:** RED — `NewEngine`/`Execute` undefined
  - **Verifies:** AC #5 — 带 skills 列表的智能体执行

- **Test:** `TestEngine_Execute_AgentWithRef`
  - **Status:** RED — `NewEngine`/`Execute` undefined
  - **Verifies:** AC #5 — 带 agent 引用的智能体执行

- **Test:** `TestEngine_Execute_PartialFailure`
  - **Status:** RED — `NewEngine`/`Execute` undefined
  - **Verifies:** AC #3 — 菱形中部分失败正确传播

- **Test:** `TestEngine_Execute_EmptyAfterCancel`
  - **Status:** RED — `NewEngine`/`Execute` undefined
  - **Verifies:** AC #3 — 已取消 context 立即返回错误

---

## 类型依赖（需新增）

### `compose/types.go`

```go
// ComposeSpec 是 crux-compose.yaml 的顶层结构
type ComposeSpec struct {
    Version string                `yaml:"version"`
    Intent  string                `yaml:"intent"`
    Agents  map[string]*AgentSpec `yaml:"agents"`
}

// AgentSpec 定义编排中的单个智能体
type AgentSpec struct {
    Intent    string            `yaml:"intent"`
    Agent     string            `yaml:"agent,omitempty"`
    Skills    []string          `yaml:"skills,omitempty"`
    DependsOn map[string]string `yaml:"depends_on,omitempty"`
}

// DAG 表示智能体依赖关系的有向无环图
type DAG struct {
    Nodes map[string]*DAGNode
}

// DAGNode 表示 DAG 中的一个节点
type DAGNode struct {
    Name       string
    Spec       *AgentSpec
    DependsOn  []string
    DependedBy []string
}

// ComposeSpawnOpts 是 compose 引擎的 Spawn 选项
type ComposeSpawnOpts struct {
    Model        string
    SystemPrompt string
    ParentPID    types.PID
}

// ComposeExitStatus 记录进程退出状态
type ComposeExitStatus struct {
    Code   int
    Reason string
    Err    error
}

// ScheduleResult 记录单个智能体的执行结果
type ScheduleResult struct {
    Name     string
    PID      types.PID
    ExitCode int
    Output   string
    Err      error
    Duration time.Duration
}

// KernelSpawner 定义 compose 引擎需要的内核操作子集
type KernelSpawner interface {
    Spawn(intent string, agent *agents.AgentInfo, opts ComposeSpawnOpts) (types.PID, error)
    Wait(pid types.PID) (ComposeExitStatus, error)
    GetProcessResult(pid types.PID) (string, bool)
}

// AgentLoaderFunc 按名称加载 agent 定义
type AgentLoaderFunc func(name string) (*agents.AgentInfo, error)
```

---

## Implementation Checklist

### Test: TestParseBytes_Valid / TestParseBytes_FullFormat / TestParseBytes_NoDependencies / TestParseBytes_SingleAgent

**File:** `compose/parser_test.go`

**Tasks to make these tests pass:**

- [ ] 创建 `compose/types.go` 定义 ComposeSpec、AgentSpec 等核心类型
- [ ] 创建 `compose/parser.go` 实现 `ParseBytes(data []byte) (*ComposeSpec, error)`
- [ ] 使用 `github.com/goccy/go-yaml` 反序列化
- [ ] Run test: `go test ./compose/ -run 'TestParseBytes_Valid|TestParseBytes_Full|TestParseBytes_NoDep|TestParseBytes_Single' -race`
- [ ] Tests pass (green phase)

---

### Test: TestParseFile_Valid / TestParseFile_NotFound

**File:** `compose/parser_test.go`

**Tasks to make these tests pass:**

- [ ] 实现 `ParseFile(path string) (*ComposeSpec, error)` — 读文件 + 调用 ParseBytes
- [ ] Run test: `go test ./compose/ -run TestParseFile -race`
- [ ] Tests pass (green phase)

---

### Test: TestParseBytes_InvalidVersion / TestParseBytes_MissingVersion / TestParseBytes_EmptyAgents / TestParseBytes_MissingAgents / TestParseBytes_AgentMissingIntent / TestParseBytes_MissingTopLevelIntent

**File:** `compose/parser_test.go`

**Tasks to make these tests pass:**

- [ ] 在 ParseBytes 中实现字段验证逻辑
- [ ] version 必须为 "1.0"
- [ ] agents 不能为空或缺失
- [ ] 每个 agent 必须有 intent
- [ ] 顶层 intent 必须存在
- [ ] Run test: `go test ./compose/ -run 'TestParseBytes_Invalid|TestParseBytes_Missing|TestParseBytes_Empty|TestParseBytes_Agent' -race`
- [ ] Tests pass (green phase)

---

### Test: TestParseBytes_DependsOnInvalidRef / TestParseBytes_DependsOnInvalidCondition

**File:** `compose/parser_test.go`

**Tasks to make these tests pass:**

- [ ] depends_on 引用的 agent name 必须在 agents map 中存在
- [ ] depends_on 的值仅支持 "completed"
- [ ] Run test: `go test ./compose/ -run TestParseBytes_DependsOn -race`
- [ ] Tests pass (green phase)

---

### Test: TestBuildDAG_NoDeps / TestBuildDAG_LinearDeps / TestBuildDAG_DiamondDeps

**File:** `compose/dag_test.go`

**Tasks to make these tests pass:**

- [ ] 创建 `compose/dag.go` 实现 `BuildDAG(spec *ComposeSpec) (*DAG, error)`
- [ ] 从 ComposeSpec.Agents 构建 DAGNode 及 DependsOn/DependedBy 边
- [ ] Run test: `go test ./compose/ -run TestBuildDAG -race`
- [ ] Tests pass (green phase)

---

### Test: TestDetectCycle_NoCycle / TestDetectCycle_SimpleCycle / TestDetectCycle_ComplexCycle / TestDetectCycle_SelfCycle / TestDetectCycle_PartialCycle

**File:** `compose/dag_test.go`

**Tasks to make these tests pass:**

- [ ] 在 dag.go 中实现 `(d *DAG) DetectCycle() ([]string, error)` — DFS 三色标记法
- [ ] BuildDAG 内部调用 DetectCycle，有环则返回详细错误
- [ ] 错误信息包含循环路径
- [ ] Run test: `go test ./compose/ -run TestDetectCycle -race`
- [ ] Tests pass (green phase)

---

### Test: TestTopologicalSort_AllParallel / TestTopologicalSort_Sequential / TestTopologicalSort_Diamond / TestTopologicalSort_ComplexGraph / TestTopologicalSort_SingleNode

**File:** `compose/dag_test.go`

**Tasks to make these tests pass:**

- [ ] 在 dag.go 中实现 `(d *DAG) TopologicalSort() ([][]string, error)` — Kahn 算法
- [ ] 返回分层结果，每层包含可并行执行的节点
- [ ] Run test: `go test ./compose/ -run TestTopologicalSort -race`
- [ ] Tests pass (green phase)

---

### Test: TestNewEngine_Valid / TestNewEngine_CyclicSpec

**File:** `compose/engine_test.go`

**Tasks to make these tests pass:**

- [ ] 创建 `compose/engine.go` 定义 Engine 结构体和 KernelSpawner 接口
- [ ] 实现 `NewEngine(spec *ComposeSpec, ks KernelSpawner, al AgentLoaderFunc) (*Engine, error)`
- [ ] 构造函数内调用 BuildDAG，有环则返回错误
- [ ] Run test: `go test ./compose/ -run TestNewEngine -race`
- [ ] Tests pass (green phase)

---

### Test: TestEngine_Execute_NoDeps / TestEngine_Execute_LinearDeps / TestEngine_Execute_DiamondDeps

**File:** `compose/engine_test.go`

**Tasks to make these tests pass:**

- [ ] 实现 `(e *Engine) Execute(ctx context.Context) ([]ScheduleResult, error)`
- [ ] 获取 TopologicalSort 分层结果
- [ ] 逐层执行，同层 goroutine 并行 Spawn
- [ ] Wait 等待当前层所有节点完成后进入下一层
- [ ] Run test: `go test ./compose/ -run 'TestEngine_Execute_NoDeps|TestEngine_Execute_Linear|TestEngine_Execute_Diamond' -race`
- [ ] Tests pass (green phase)

---

### Test: TestEngine_Execute_FailurePropagation / TestEngine_Execute_PartialFailure

**File:** `compose/engine_test.go`

**Tasks to make these tests pass:**

- [ ] 节点失败时记录错误到 ScheduleResult
- [ ] 下游节点检查上游状态，如有失败则标记为 Failed 不启动
- [ ] Run test: `go test ./compose/ -run 'TestEngine_Execute_Failure|TestEngine_Execute_Partial' -race`
- [ ] Tests pass (green phase)

---

### Test: TestEngine_Execute_ContextCancel / TestEngine_Execute_EmptyAfterCancel

**File:** `compose/engine_test.go`

**Tasks to make these tests pass:**

- [ ] Execute 方法检查 ctx.Done()
- [ ] 在每层执行前和 Wait 期间检查 context 取消
- [ ] Run test: `go test ./compose/ -run 'TestEngine_Execute_Context|TestEngine_Execute_Empty' -race`
- [ ] Tests pass (green phase)

---

### Test: TestEngine_Execute_OutputPassthrough

**File:** `compose/engine_test.go`

**Tasks to make these tests pass:**

- [ ] Wait 完成后调用 GetProcessResult 获取上游输出
- [ ] 将输出拼接到下游 Spawn 的 SystemPrompt 中
- [ ] 格式：`## 上游智能体输出\n### {name} 输出:\n{result}`
- [ ] Run test: `go test ./compose/ -run TestEngine_Execute_Output -race`
- [ ] Test passes (green phase)

---

### Test: TestEngine_Execute_Performance

**File:** `compose/engine_test.go`

**Tasks to make these tests pass:**

- [ ] 确保 10 个并行 agent 的 Engine 开销（不含 LLM）≤ 2s
- [ ] 并行调度使用 goroutine + sync.WaitGroup
- [ ] Run test: `go test ./compose/ -run TestEngine_Execute_Performance -race -count=1`
- [ ] Test passes (green phase)

---

### Test: TestEngine_Execute_AgentWithSkills / TestEngine_Execute_AgentWithRef

**File:** `compose/engine_test.go`

**Tasks to make these tests pass:**

- [ ] 当 AgentSpec 指定 `agent` 字段时，通过 AgentLoaderFunc 加载 AgentInfo
- [ ] 当仅指定 `skills` 时，构建轻量 AgentInfo
- [ ] Run test: `go test ./compose/ -run 'TestEngine_Execute_Agent' -race`
- [ ] Tests pass (green phase)

---

## Running Tests

```bash
# Run all failing tests for this story (will fail to compile until implementation exists)
go test ./compose/ -race -v

# Run specific test group
go test ./compose/ -run TestParseBytes -race -v
go test ./compose/ -run TestBuildDAG -race -v
go test ./compose/ -run TestDetectCycle -race -v
go test ./compose/ -run TestTopologicalSort -race -v
go test ./compose/ -run TestEngine -race -v

# Run all project tests (including regression)
make test

# Run with coverage
go test ./compose/ -race -coverprofile=coverage.out && go tool cover -html=coverage.out

# Run with verbose timing
go test ./compose/ -run TestEngine_Execute_Performance -race -v -count=1
```

---

## Red-Green-Refactor Workflow

### RED Phase (Complete)

**TEA Agent Responsibilities:**

- All 41 tests written and failing (compilation errors due to missing implementation)
- Tests follow existing project conventions (standard `testing` package, no testify)
- Tests cover all 5 acceptance criteria + NFR21 性能要求
- Mock KernelSpawner 模式与 Story 6.5 的 mock 模式一致

**Verification:**

- All tests fail to compile (`undefined` errors for unimplemented types and functions)
- Failure is due to missing implementation, not test bugs
- Existing tests are unaffected (compose 是独立新包)

---

### GREEN Phase (DEV Team - Next Steps)

**DEV Agent Responsibilities:**

1. **Start with types**: 创建 `compose/types.go` 定义所有数据结构
2. **Implement parser**: 创建 `compose/parser.go` 实现 ParseBytes/ParseFile
3. **Implement DAG**: 创建 `compose/dag.go` 实现 BuildDAG/DetectCycle/TopologicalSort
4. **Implement engine**: 创建 `compose/engine.go` 实现 NewEngine/Execute
5. **Run tests incrementally**: `go test ./compose/ -run TestParseBytes_Valid -race` first

**Key Principles:**

- One test at a time (don't try to fix all at once)
- Minimal implementation (don't over-engineer)
- Run tests frequently (immediate feedback with `-race`)
- compose 包不导入 kernel 包，通过 KernelSpawner 接口解耦

---

### REFACTOR Phase (DEV Team - After All Tests Pass)

1. Verify all tests pass: `go test ./compose/ -race`
2. Run lint: `make lint`
3. Verify full suite: `make test`
4. Build: `make build`
5. Check no regression on Epic 1-6

---

## Test Execution Evidence

### Initial Test Run (RED Phase Verification)

**Command:** `go test ./compose/ -race`

**Results:**

```
# github.com/gonewx/crux/compose [github.com/gonewx/crux/compose.test]
compose/engine_test.go:24:9: undefined: ComposeSpawnOpts
compose/engine_test.go:50:80: undefined: ComposeSpawnOpts
compose/engine_test.go:62:50: undefined: ComposeExitStatus
compose/dag_test.go:13:11: undefined: ComposeSpec
compose/dag_test.go:16:23: undefined: AgentSpec
compose/dag_test.go:24:14: undefined: BuildDAG
compose/dag_test.go:42:11: undefined: ComposeSpec
compose/dag_test.go:45:23: undefined: AgentSpec
compose/dag_test.go:53:14: undefined: BuildDAG
compose/dag_test.go:88:11: undefined: ComposeSpec
compose/dag_test.go:88:11: too many errors
FAIL    github.com/gonewx/crux/compose [build failed]
```

**Summary:**

- Total tests: 41
- Passing: 0 (expected — compilation fails)
- Failing: 41 (expected — implementation not yet written)
- Status: RED phase verified

**Expected Failure Messages:**
- `undefined: ComposeSpec` — types.go 未创建
- `undefined: AgentSpec` — types.go 未创建
- `undefined: BuildDAG` — dag.go 未创建
- `undefined: ParseBytes` — parser.go 未创建
- `undefined: ParseFile` — parser.go 未创建
- `undefined: ComposeSpawnOpts` — types.go 未创建
- `undefined: ComposeExitStatus` — types.go 未创建
- `undefined: NewEngine` — engine.go 未创建
- `undefined: ScheduleResult` — types.go 未创建

---

## Notes

- Go ATDD 的 RED 阶段表现为编译失败（`undefined` 错误），而非运行时失败。这是因为 Go 编译整个包，新测试文件引用的未实现类型和函数会导致编译错误。
- `compose/` 是全新独立包，不修改任何现有代码。测试文件使用标准 `testing` 包，与项目现有测试风格一致。
- Mock KernelSpawner 与 Story 6.5 的 mock 模式一致：通过接口解耦，测试中使用 mock 实现。
- Engine 测试使用 `mockKernelSpawner` 模拟 Spawn/Wait/GetProcessResult，避免真实内核依赖。
- `compose/` 包只依赖 `agents/` 和 `internal/types/`，不依赖 `kernel/` 包。
- 性能测试 `TestEngine_Execute_Performance` 验证 NFR21（10 智能体 ≤ 2s），mock 没有 delay 所以纯测量 Engine 开销。

---

## Next Steps

1. **DEV agent 开始实现**：按 Implementation Checklist 顺序，从 types -> parser -> dag -> engine
2. **逐步编译验证**：每实现一个文件后 `go build ./compose/` 检查编译
3. **逐步测试验证**：每完成一组相关实现后运行对应测试
4. **全量回归**：所有测试通过后 `make test` 验证无回归
5. **更新 Story 状态**：所有测试通过且 `make all` 成功后标记 Story 7.1 为 done

---

## Knowledge Base References Applied

- **test-quality.md** — Given-When-Then 结构、单一断言、确定性、隔离性
- **test-levels-framework.md** — Unit 级别选择（后端项目无 E2E）
- **existing test patterns** — `kernel_test.go`/`process_test.go` 的测试辅助函数和断言模式
- **Story 6.5 ATDD checklist** — 后端 Go 项目 ATDD 模式参考

---

**Generated by BMad TEA Agent** - 2026-02-28
