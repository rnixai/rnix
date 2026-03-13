# Story 21.2: 合约 SLA 与评估

Status: done

## Story

As a 应用开发者,
I want 通过合约 SLA 约束智能体协作的输入格式、输出质量、token 消耗和超时,
So that 智能体之间的协作有明确的质量保证。

## Acceptance Criteria

1. **AC1: SLA 定义与解析**
   - Given agent.yaml 或 compose.yaml 中定义了合约 SLA（包含 max_tokens、max_duration_ms、output_format 等约束）
   - When 系统加载配置时
   - Then SLA 定义被正确解析为结构化类型，缺省字段使用合理默认值

2. **AC2: SLA 自动评估**
   - Given 智能体完成执行
   - When 系统自动评估 SLA
   - Then 对每项约束（token 消耗、响应时间、输出格式）逐一检查通过/失败
   - And 生成 SLA 评估结果（SLAResult）

3. **AC3: 评估结果记录到声誉分数**
   - Given SLA 评估完成
   - When 评估结果生成
   - Then 结果记录到该 Agent 模板的声誉分数文件（`$PROJECT/.rnix/reputation/{agent_name}.json`）
   - And 包含各项通过/失败状态、时间戳、执行元数据

4. **AC4: Compose 引擎集成 SLA 评估**
   - Given Compose 编排完成每个智能体的执行
   - When 智能体退出后
   - Then Engine 自动触发 SLA 评估并将结果附加到 ScheduleResult
   - And SLA 评估结果可通过 IPC 查询

5. **AC5: 无 SLA 定义时向后兼容**
   - Given agent.yaml 或 compose.yaml 未定义 SLA
   - When 智能体执行完成
   - Then 行为与现有完全一致——不进行 SLA 评估，无声誉记录

## Tasks / Subtasks

### Task 1: SLA 数据模型（AC: #1, #5）

- [x] 1.1 新建 `kernel/sla.go`，定义 SLA 核心类型：

  ```go
  // SLASpec 定义合约 SLA 约束
  type SLASpec struct {
      MaxTokens    int    `yaml:"max_tokens,omitempty" json:"max_tokens,omitempty"`
      MaxDurationMs int64 `yaml:"max_duration_ms,omitempty" json:"max_duration_ms,omitempty"`
      OutputFormat  string `yaml:"output_format,omitempty" json:"output_format,omitempty"` // "json" | "markdown" | "" (any)
  }

  // SLACheckResult 记录单项 SLA 检查结果
  type SLACheckResult struct {
      Name    string `json:"name"`     // 检查项名称 ("max_tokens", "max_duration_ms", "output_format")
      Passed  bool   `json:"passed"`
      Actual  string `json:"actual"`   // 实际值的字符串表示
      Limit   string `json:"limit"`    // SLA 限制值的字符串表示
  }

  // SLAResult 记录完整 SLA 评估结果
  type SLAResult struct {
      AgentName  string           `json:"agent_name"`
      Passed     bool             `json:"passed"`      // 所有检查项均通过
      Checks     []SLACheckResult `json:"checks"`
      EvaluatedAt time.Time       `json:"evaluated_at"`
      TokensUsed  int             `json:"tokens_used"`
      DurationMs  int64           `json:"duration_ms"`
  }

  // IsEmpty returns true if no SLA constraints are defined.
  func (s *SLASpec) IsEmpty() bool
  // Evaluate checks the SLA against actual execution metrics.
  func (s *SLASpec) Evaluate(agentName string, tokensUsed int, durationMs int64, output string) *SLAResult
  ```

- [x] 1.2 实现 `SLASpec.IsEmpty()` -- 所有字段为零值时返回 true
- [x] 1.3 实现 `SLASpec.Evaluate()` -- 逐项检查：
  - `max_tokens`：实际 tokensUsed <= MaxTokens（MaxTokens=0 跳过）
  - `max_duration_ms`：实际 durationMs <= MaxDurationMs（MaxDurationMs=0 跳过）
  - `output_format`：检查输出是否为有效 JSON（"json"）或包含 Markdown 标题（"markdown"），空字符串跳过
  - `Passed` = 所有检查项均为 true

- [x] 1.4 单元测试 `kernel/sla_test.go`：
  - `TestSLASpec_IsEmpty` -- 空 SLASpec 返回 true
  - `TestSLASpec_IsEmpty_WithConstraints` -- 有约束返回 false
  - `TestSLASpec_Evaluate_AllPassed` -- 所有项在限制内
  - `TestSLASpec_Evaluate_TokenExceeded` -- token 超出
  - `TestSLASpec_Evaluate_DurationExceeded` -- 时间超出
  - `TestSLASpec_Evaluate_OutputFormatJSON_Valid` -- 有效 JSON 输出通过
  - `TestSLASpec_Evaluate_OutputFormatJSON_Invalid` -- 非 JSON 输出失败
  - `TestSLASpec_Evaluate_NoConstraints` -- 无约束全部通过
  - `TestSLASpec_Evaluate_MultipleFailures` -- 多项同时失败

### Task 2: 配置扩展（AC: #1, #5）

- [x] 2.1 `compose/types.go`：`AgentSpec` 添加 `SLA *kernel.SLASpec \`yaml:"sla,omitempty"\``
- [x] 2.2 `agents/types.go`：`AgentManifest` 添加 `SLA *kernel.SLASpec \`yaml:"sla,omitempty"\``
- [x] 2.3 单元测试：
  - `TestParseComposeSpec_WithSLA` -- 解析 compose.yaml 中的 SLA 字段
  - `TestParseAgentManifest_WithSLA` -- 解析 agent.yaml 中的 SLA 字段
  - `TestParseComposeSpec_WithoutSLA` -- 无 SLA 字段时 nil

### Task 3: 声誉分数持久化（AC: #3）

- [x] 3.1 新建 `kernel/reputation.go`，定义声誉存储：

  ```go
  // ReputationRecord 记录单次 SLA 评估
  type ReputationRecord struct {
      SLAResult   *SLAResult `json:"sla_result"`
      Timestamp   time.Time  `json:"timestamp"`
  }

  // ReputationStore 管理 Agent 模板的声誉数据
  type ReputationStore struct {
      mu      sync.Mutex
      baseDir string  // $PROJECT/.rnix/reputation/
  }

  func NewReputationStore(baseDir string) *ReputationStore
  // RecordResult 追加 SLA 评估结果到 Agent 的声誉文件
  func (rs *ReputationStore) RecordResult(agentName string, result *SLAResult) error
  // GetHistory 读取 Agent 的声誉历史
  func (rs *ReputationStore) GetHistory(agentName string) ([]ReputationRecord, error)
  ```

- [x] 3.2 实现 `RecordResult` -- 追加 JSON 记录到 `{baseDir}/{agent_name}.json`：
  - 文件不存在时自动创建（含父目录）
  - 文件格式：JSON Lines（每行一条记录），便于追加写入
  - 使用 `os.OpenFile` 以 `O_APPEND|O_CREATE|O_WRONLY` 模式写入

- [x] 3.3 实现 `GetHistory` -- 读取 JSON Lines 文件并解析为 `[]ReputationRecord`
  - 文件不存在时返回空切片（无错误）

- [x] 3.4 单元测试 `kernel/reputation_test.go`：
  - `TestReputationStore_RecordResult` -- 写入后读取一致
  - `TestReputationStore_RecordResult_Multiple` -- 多次追加正确
  - `TestReputationStore_GetHistory_NoFile` -- 文件不存在返回空切片
  - `TestReputationStore_RecordResult_CreateDir` -- 自动创建目录
  - `TestReputationStore_ConcurrentWrite` -- 多 goroutine 并发写入安全

### Task 4: Compose 引擎集成 SLA 评估（AC: #2, #4）

- [x] 4.1 `compose/types.go`：`ScheduleResult` 添加 `SLAResult *kernel.SLAResult` 字段
- [x] 4.2 `compose/engine.go`：Engine 结构体添加 `reputationStore *kernel.ReputationStore` 字段
- [x] 4.3 `compose/engine.go`：`NewEngine` 接受可选 `ReputationStore`（可为 nil）
- [x] 4.4 `compose/engine.go`：`executeNode` 中智能体完成后，如果 `AgentSpec.SLA` 非空：
  - 调用 `SLASpec.Evaluate()` 生成 SLAResult
  - 附加到 `ScheduleResult.SLAResult`
  - 如果 `reputationStore` 非 nil，调用 `RecordResult` 持久化
- [x] 4.5 单元测试 `compose/engine_test.go`：
  - `TestEngine_Execute_WithSLA_AllPassed` -- SLA 全部通过
  - `TestEngine_Execute_WithSLA_TokenExceeded` -- token 超出 SLA
  - `TestEngine_Execute_WithoutSLA` -- 无 SLA 时行为不变
  - `TestEngine_Execute_WithSLA_RecordReputation` -- 声誉记录被持久化

### Task 5: IPC SLA 查询（AC: #4）

- [x] 5.1 `ipc/protocol.go`：新增 IPC 方法：

  ```go
  const MethodSLAStatus = "sla_status"

  type SLAStatusRequest struct {
      GroupID types.ProcGroupID `json:"group_id"`
  }

  type SLAStatusResponse struct {
      Results []SLAResultWire `json:"results"`
  }

  type SLAResultWire struct {
      AgentName  string                `json:"agent_name"`
      PID        types.PID             `json:"pid"`
      Passed     bool                  `json:"passed"`
      Checks     []kernel.SLACheckResult `json:"checks"`
      TokensUsed int                   `json:"tokens_used"`
      DurationMs int64                 `json:"duration_ms"`
  }
  ```

- [x] 5.2 `kernel/kernel.go`：新增 `slaResults *xsync.SyncMap[types.ProcGroupID, []*SLAResult]` 字段
- [x] 5.3 `kernel/kernel.go`：新增 `RecordSLAResult(groupID types.ProcGroupID, result *SLAResult)` 方法
- [x] 5.4 `kernel/kernel.go`：新增 `GetSLAResults(groupID types.ProcGroupID) ([]*SLAResult, error)` 方法
- [x] 5.5 `ipc/server.go`：dispatch 注册 `MethodSLAStatus` handler
- [x] 5.6 `ipc/client.go`：新增 `SLAStatus(groupID types.ProcGroupID) (*SLAStatusResponse, error)` 方法
- [x] 5.7 单元测试：
  - `TestServer_SLAStatus_Success` -- 查询已有 SLA 结果
  - `TestServer_SLAStatus_NoResults` -- 无结果返回空列表
  - `TestClient_SLAStatus` -- IPC 客户端往返测试

### Task 6: 端到端集成验证（AC: all）

- [x] 6.1 集成测试 `compose/sla_integration_test.go`：
  - `TestE2E_SLA_EvaluateAndRecord` -- Compose 执行后 SLA 评估并记录声誉
  - `TestE2E_SLA_TokenExceeded` -- token 超出 SLA 的结果正确记录
  - `TestE2E_SLA_NoSLA_BackwardCompat` -- 无 SLA 时现有行为不变
  - `TestE2E_SLA_MultipleAgents` -- 多智能体各有不同 SLA

## Dev Notes

### 核心设计决策

**SLASpec 在 kernel 包内。** 设计理由：
1. SLA 评估需要 token 消耗和执行时长等 kernel 级指标
2. 与 BudgetPool 同层——都是 kernel 级资源管理概念
3. ReputationStore 也在 kernel 包内——声誉是内核级数据

**SLA 评估是后置验证，不是运行时约束。** 设计选择：
1. SLA 评估在智能体完成后执行——不会中断正在运行的智能体
2. 运行时预算控制已由 BudgetPool（Story 21.1）和 ContextBudget（Story 10.3）处理
3. SLA 评估是质量度量，不是执行控制——评估失败不导致进程终止
4. 这种设计避免了 SLA 检查与 reasonStep 的耦合

**声誉数据使用 JSON Lines 文件存储。** 理由：
1. 追加写入性能好——无需读取整个文件来添加新记录
2. 格式简单——每行一个 JSON 对象，便于解析和调试
3. 与 Rnix 的"一切皆文件"哲学一致
4. 存储路径 `$PROJECT/.rnix/reputation/` 遵循已有的文件持久化路径模式

**OutputFormat 检查是轻量级的。** 设计选择：
1. "json" 检查：`json.Valid([]byte(output))` -- 仅验证语法
2. "markdown" 检查：检查输出是否包含 `#` 开头的行 -- 基本格式验证
3. 不做深层语义验证——SLA 关注的是格式合规性，不是内容质量
4. 未来 Story 21.3 可以扩展更复杂的质量评估

### 架构合规

- **依赖方向**：`kernel/sla.go` 和 `kernel/reputation.go` 仅依赖 `sync`、`os`、`encoding/json`、`time`，无新外部依赖
- **IPC 标准 4 步**：protocol.go（类型） → server.go（handler） → client.go（方法） → 未来 CLI 命令
- **并发安全**：ReputationStore 使用 sync.Mutex 保护文件写入，SLASpec.Evaluate 是无状态纯函数
- **kernel 不导入新包**：SLA 和 Reputation 都在 kernel 包内
- **向后兼容**：SLA 为 nil 时不执行评估，所有现有行为不变
- **复用 Story 21.1 模式**：SLA 结果通过 ScheduleResult 传递，复用已有的 Compose → 结果收集链

### 关键文件变更

| 文件 | 变更类型 | 说明 |
|------|---------|------|
| `kernel/sla.go` | **新建** | SLASpec/SLACheckResult/SLAResult 类型，Evaluate 方法 |
| `kernel/sla_test.go` | **新建** | SLA 评估逻辑测试 |
| `kernel/reputation.go` | **新建** | ReputationStore，RecordResult/GetHistory 方法 |
| `kernel/reputation_test.go` | **新建** | 声誉存储测试 |
| `compose/types.go` | 修改 | AgentSpec.SLA, ScheduleResult.SLAResult |
| `compose/engine.go` | 修改 | Engine.reputationStore, executeNode 中 SLA 评估 |
| `compose/engine_test.go` | 修改 | SLA 集成测试 |
| `compose/sla_integration_test.go` | **新建** | 端到端集成测试 |
| `agents/types.go` | 修改 | AgentManifest.SLA 字段 |
| `ipc/protocol.go` | 修改 | MethodSLAStatus, SLAStatusRequest/Response |
| `ipc/server.go` | 修改 | dispatch 注册 handleSLAStatus |
| `ipc/client.go` | 修改 | SLAStatus 客户端方法 |
| `kernel/kernel.go` | 修改 | slaResults SyncMap, RecordSLAResult/GetSLAResults 方法 |

### 复用模式

- **BudgetPool 注册模式**：SLA 结果的注册/查询复用 SyncMap 管理模式
- **IPC 4 步模式**：完全复用 project-context.md 中定义的 IPC 扩展标准步骤
- **ScheduleResult 扩展模式**：复用 Story 21.1 在 ScheduleResult 添加 TokensUsed 的模式
- **JSON Lines 持久化**：与 debug 包的 trace 数据存储模式一致
- **文件持久化路径**：`$PROJECT/.rnix/reputation/` 遵循架构文档中定义的路径模式

### 从 Story 21.1 继承的经验

- **配额传递链**：Story 21.1 通过 ScheduleResult.TokensUsed 传递 token 消耗。SLA 评估在相同位置（executeNode 完成后）获取 token 数据。
- **Engine 扩展模式**：Story 21.1 在 Engine 添加 budgetPool 字段。SLA 添加 reputationStore 字段采用相同模式。
- **向后兼容测试**：Story 21.1 有 `TestEngine_Execute_NoBudgetPool`。SLA 需有对应的 `TestEngine_Execute_WithoutSLA`。
- **Code Review 修复**：Story 21.1 中 `ipcKernelSpawner.GetTokensUsed` 是 stub 导致 bug。SLA 查询方法必须有实际实现。

### 组合矩阵

| 本功能 | 交互对象 | 交互方式 | 需验证 |
|--------|----------|----------|--------|
| SLA 评估 | ScheduleResult.TokensUsed | 依赖：SLA 用 TokensUsed 检查 max_tokens | 是 |
| SLA 评估 | ScheduleResult.Duration | 依赖：SLA 用 Duration 检查 max_duration_ms | 是 |
| SLA 评估 | Process.Result | 依赖：SLA 用 output 检查 output_format | 是 |
| SLA 记录 | ReputationStore | 扩展：评估结果持久化到声誉文件 | 是 |
| SLA 与 BudgetPool | BudgetPool 配额 | 独立：SLA 是后置评估，BudgetPool 是运行时限制 | 否 |
| SLA 与 ContextBudget | Process.ContextBudget | 独立：SLA max_tokens 是 SLA 约束，ContextBudget 是硬限制 | 否 |
| IPC sla_status | IPC dispatch | 扩展：新增 sla_status case | 是 |
| SLA 与 failure_strategy | Compose failure 处理 | 独立：SLA 评估不影响 failure_strategy 决策 | 否 |
| ReputationStore 与 Compose down | Compose 生命周期 | 独立：声誉数据持久化在文件中，不随 Compose 释放 | 否 |

### Project Structure Notes

- `kernel/sla.go` 在 kernel 包内，与 `kernel/budget_pool.go` 平级
- `kernel/reputation.go` 在 kernel 包内，持久化到 `$PROJECT/.rnix/reputation/`
- SLASpec 是纯数据结构 + Evaluate 方法，无状态，线程安全
- ReputationStore 有自己的 Mutex，不依赖 kernel.mu
- IPC 类型定义在 `ipc/protocol.go`，与 kernel 包内的 SLAResult 是独立类型（IPC 边界转换）
- compose 包通过 `*kernel.SLASpec` 指针引用——compose 已依赖 kernel

### rnix-compose.yaml 扩展示例

```yaml
version: "1.0"
intent: "代码审查工作流"
token_budget: 50000

agents:
  reviewer:
    intent: "审查代码变更"
    agent: code-reviewer
    priority: high
    sla:
      max_tokens: 15000
      max_duration_ms: 60000
      output_format: "json"
  summarizer:
    intent: "生成审查摘要"
    agent: summarizer
    priority: normal
    sla:
      max_tokens: 8000
      max_duration_ms: 30000
      output_format: "markdown"
    depends_on:
      reviewer: completed
```

### agent.yaml 扩展示例

```yaml
name: code-reviewer
description: "审查代码变更"
models:
  provider: claude
  preferred: claude-sonnet-4-6
skills:
  - code-analysis
sla:
  max_tokens: 15000
  max_duration_ms: 60000
  output_format: "json"
```

### SLA 优先级说明

当 compose.yaml 的 AgentSpec 和 agent.yaml 的 AgentManifest 同时定义了 SLA 时：
- **compose.yaml 中的 SLA 优先** -- Compose 编排是上层调用者，应能覆盖 Agent 默认 SLA
- 如果 compose.yaml 未定义 SLA 但 agent.yaml 定义了 SLA，使用 agent.yaml 的 SLA
- 如果两者都未定义，不执行 SLA 评估

### References

- [Source: _bmad-output/planning-artifacts/epics/epic-21-token经济声誉与skill协同-token-economy-reputation-skill-synergy.md#Story 21.2]
- [Source: _bmad-output/implementation-artifacts/21-1-token-budget-pool-and-allocation.md] -- Story 21.1 BudgetPool 实现，本 Story 复用其 ScheduleResult 扩展模式
- [Source: kernel/budget_pool.go] -- BudgetPool 数据模型参考
- [Source: kernel/kernel.go#reasonStep] -- token 累加位置
- [Source: compose/types.go#ScheduleResult] -- 结果类型扩展点
- [Source: compose/engine.go#executeNode] -- SLA 评估注入点
- [Source: agents/types.go#AgentManifest] -- Agent 配置扩展点
- [Source: ipc/protocol.go] -- IPC Method 常量和请求/响应类型
- [Source: _bmad-output/project-context.md#IPC扩展标准步骤] -- IPC 4 步标准流程
- [Source: _bmad-output/project-context.md#文件持久化路径模式] -- 声誉数据存储路径
- [Source: _bmad-output/planning-artifacts/architecture/implementation-patterns-consistency-rules.md#Compose 引擎模式]

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6

### Debug Log References

- lint 修复: `kernel/sla.go:100` `strings.Split` → `strings.SplitSeq`（modernize/stringsseq）
- 测试修复: `compose/atdd_21_2_sla_test.go:375-377` mock tokenUsed 设置不依赖 PID 顺序，避免 map 迭代顺序导致的 flaky 测试

### Code Review Fixes

- **[H1] 竞态条件修复**: `kernel/kernel.go` — `RecordSLAResult` 的 Load+Modify+Store 操作添加 `slaResultsMu sync.Mutex` 保护，防止并发写入丢失数据
- **[M1] output_format Actual 语义修复**: `kernel/sla.go` — 检查失败时 Actual 改为 `"invalid_json"` / `"invalid_markdown"` 而非与 Limit 相同
- **[M2] SLAResultWire 补充 PID 字段**: `ipc/protocol.go` — 添加 `PID types.PID` 字段以符合 Story 规格

### Completion Notes List

- **Task 1**: `kernel/sla.go` 实现 SLASpec/SLACheckResult/SLAResult 类型，IsEmpty() 和 Evaluate() 方法。单元测试在 `kernel/atdd_21_2_sla_test.go` 覆盖 9 个测试用例。
- **Task 2**: `compose/types.go` AgentSpec.SLA 字段，`agents/types.go` AgentManifest.SLA 字段（使用 AgentSLA 类型避免循环依赖）。解析测试在 `compose/atdd_21_2_sla_test.go` 和 `agents/loader_test.go`。
- **Task 3**: `kernel/reputation.go` ReputationStore 实现 JSON Lines 文件持久化。单元测试在 `kernel/atdd_21_2_reputation_test.go` 覆盖 5 个测试用例含并发写入。
- **Task 4**: `compose/engine.go` 添加 reputationStore 字段和 NewEngineWithReputation 构造器，executeNode 中 SLA 评估和声誉记录。集成测试在 `compose/atdd_21_2_sla_test.go` 覆盖 8 个测试用例。
- **Task 5**: `ipc/protocol.go` SLAStatusRequest/Response/ResultWire 类型，`ipc/server.go` handleSLAStatus handler，`ipc/client.go` SLAStatus 方法，`kernel/kernel.go` slaResults SyncMap + RecordSLAResult/GetSLAResults。IPC 协议测试在 `ipc/protocol_test.go`，kernel 集成测试在 `kernel/atdd_21_2_kernel_sla_test.go`。
- **Task 6**: 端到端集成测试由 `compose/atdd_21_2_sla_test.go` 覆盖（TestEngine_Execute_WithSLA_RecordReputation, TestEngine_Execute_MultipleAgents_EachSLA 等）。

### File List

- `kernel/sla.go` — 新建：SLA 数据模型和评估逻辑
- `kernel/reputation.go` — 新建：声誉分数持久化存储
- `kernel/kernel.go` — 修改：添加 slaResults SyncMap、RecordSLAResult/GetSLAResults 方法
- `kernel/atdd_21_2_sla_test.go` — 新建：SLA 评估单元测试（ATDD）
- `kernel/atdd_21_2_reputation_test.go` — 新建：声誉存储单元测试（ATDD）
- `kernel/atdd_21_2_kernel_sla_test.go` — 新建：Kernel SLA 结果注册/查询测试（ATDD）
- `compose/types.go` — 修改：AgentSpec.SLA、ScheduleResult.SLAResult 字段
- `compose/engine.go` — 修改：reputationStore 字段、NewEngineWithReputation、executeNode SLA 评估
- `compose/atdd_21_2_sla_test.go` — 新建：Compose SLA 集成测试（ATDD）
- `agents/types.go` — 修改：AgentSLA 类型、AgentManifest.SLA 字段
- `agents/testdata/sla-agent/agent.yaml` — 新建：SLA agent 测试数据
- `agents/testdata/sla-agent/instructions.md` — 新建：SLA agent 测试指令
- `agents/loader_test.go` — 修改：添加 SLA 解析测试
- `ipc/protocol.go` — 修改：SLAStatusRequest/Response/ResultWire 类型、MethodSLAStatus 常量
- `ipc/server.go` — 修改：handleSLAStatus dispatch 注册和 handler
- `ipc/client.go` — 修改：SLAStatus 客户端方法
- `ipc/protocol_test.go` — 修改：SLA IPC 协议序列化测试
