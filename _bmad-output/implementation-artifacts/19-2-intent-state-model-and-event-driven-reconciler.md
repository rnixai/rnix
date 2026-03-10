# Story 19.2: 意图状态模型与事件驱动 Reconciler

Status: ready-for-dev

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a 应用开发者,
I want 系统维护意图状态模型并通过事件驱动的 Reconciler 自动调和差异,
So that 子任务失败或超时时系统自动处理，无需我手动干预。

## Acceptance Criteria

1. **Given** 意图正在执行中
   **When** 系统持续监测
   **Then** 维护 Desired（期望状态）、Current（当前状态）、Drift（差异）三个状态

2. **Given** 某个子任务失败或超时
   **When** 事件触发 Reconciler
   **Then** 系统自动重新规划和重试该子任务，无需用户手动干预
   **And** 从检测到 drift 到启动调和行动延迟 ≤ 5s（NFR40）

3. **Given** 子任务成功完成
   **When** 事件触发 Reconciler
   **Then** 更新 Current 状态，检查下游依赖并启动可执行的后续任务

4. **Given** 子任务重试次数达到上限
   **When** Reconciler 检测到
   **Then** 标记该节点最终失败，级联标记依赖下游为 failed，向用户报告

5. **Given** 子任务执行超时
   **When** Reconciler 检测到
   **Then** 先终止超时进程，再按重试策略处理（重试或标记失败）

6. **Given** 用户查询 `rnix intent status`
   **When** Reconciler 正在运行
   **Then** 状态信息包含当前 drift 项列表、重试计数、每个节点的 Desired/Current 对比

## Tasks / Subtasks

### Task 1: IntentNode 扩展——重试策略与超时配置（AC: #2, #4, #5）

- [ ] 1.1 在 `intent/types.go` 的 `IntentNode` 结构体中新增字段：

  ```go
  RetryCount   int           `json:"retry_count" yaml:"retry_count"`
  MaxRetries   int           `json:"max_retries" yaml:"max_retries"`
  Timeout      time.Duration `json:"timeout,omitempty" yaml:"timeout,omitempty"`
  LastFailedAt *time.Time    `json:"last_failed_at,omitempty" yaml:"last_failed_at,omitempty"`
  ```

- [ ] 1.2 新增 IntentState 常量：

  ```go
  IntentRetrying IntentState = "retrying"
  ```

- [ ] 1.3 新增 `IntentNode` 方法：
  - `CanRetry() bool` — `RetryCount < MaxRetries`
  - `IncrRetry()` — `RetryCount++`，记录 `LastFailedAt`

### Task 2: IntentTree 三态模型——Desired/Current/Drift（AC: #1, #6）

- [ ] 2.1 在 `intent/types.go` 中新增 Drift 类型：

  ```go
  type DriftType string
  const (
      DriftNodeFailed  DriftType = "node_failed"
      DriftNodeTimeout DriftType = "node_timeout"
  )

  type DriftItem struct {
      NodeID    string    `json:"node_id"`
      Type      DriftType `json:"type"`
      Message   string    `json:"message"`
      DetectedAt time.Time `json:"detected_at"`
  }
  ```

- [ ] 2.2 在 `IntentTree` 中新增三态字段：

  ```go
  DesiredNodes map[string]IntentState `json:"desired_nodes" yaml:"desired_nodes"`
  Drifts       []DriftItem            `json:"drifts,omitempty" yaml:"drifts,omitempty"`
  ```

  **设计说明**：
  - `Desired` = 所有节点目标状态均为 `IntentCompleted`（由初始分解确定，静态不变）
  - `Current` = 各 `IntentNode.State`（已有字段，不重复存储）
  - `Drift` = `DesiredNodes[nodeID]` 与 `node.State` 不一致的记录

- [ ] 2.3 新增 `IntentTree` 方法：
  - `InitDesired()` — 初始化 `DesiredNodes`，为每个 node 设置目标 `IntentCompleted`
  - `ComputeDrifts() []DriftItem` — 扫描所有节点，对比 Desired 与 Current，返回差异列表
  - `AddDrift(item DriftItem)` — 添加 drift 记录
  - `ClearDrift(nodeID string)` — 节点成功后清除其 drift
  - `ActiveDrifts() []DriftItem` — 返回未解决的 drift 列表

### Task 3: Reconciler 核心实现（AC: #2, #3, #4, #5）

- [ ] 3.1 新建 `intent/reconciler.go`：

  ```go
  type ReconcilerConfig struct {
      DefaultMaxRetries int           // 默认最大重试次数（3）
      DefaultTimeout    time.Duration // 默认节点超时（5min）
      ReconcileInterval time.Duration // 调和检查间隔（1s）
      MaxReconcileDelay time.Duration // NFR40: ≤ 5s
  }

  type Reconciler struct {
      tree      *IntentTree
      spawner   KernelSpawner
      config    ReconcilerConfig
      mu        sync.Mutex
      callbacks ReconcilerCallbacks
      eventCh   chan reconcileEvent
  }

  type ReconcilerCallbacks struct {
      OnNodeRetry    func(nodeID string, attempt int, maxRetries int)
      OnNodeTimeout  func(nodeID string)
      OnDriftDetected func(drift DriftItem)
      OnDriftResolved func(nodeID string)
      OnNodeStart    func(nodeID string, pid types.PID)
      OnNodeComplete func(nodeID string, result string)
      OnNodeFailed   func(nodeID string, err string)
      OnProgress     func(completed, total int)
  }

  func DefaultReconcilerConfig() ReconcilerConfig
  func NewReconciler(tree *IntentTree, spawner KernelSpawner, config ReconcilerConfig, callbacks ReconcilerCallbacks) (*Reconciler, error)
  ```

- [ ] 3.2 定义 `reconcileEvent` 类型：

  ```go
  type reconcileEvent struct {
      nodeID  string
      evType  reconcileEventType
      result  string
      errMsg  string
  }

  type reconcileEventType int
  const (
      evNodeCompleted reconcileEventType = iota
      evNodeFailed
      evNodeTimeout
  )
  ```

- [ ] 3.3 实现 `Reconciler.Execute(ctx context.Context) error`：
  - 调用 `tree.InitDesired()` 初始化目标状态
  - 启动事件驱动主循环（与 `Engine.Execute` 类似但增加调和逻辑）：
    1. 启动所有 runnable 节点（每个节点在 goroutine 中执行）
    2. 每个节点执行时包装 `context.WithTimeout` 实现超时控制
    3. 主循环从 `eventCh` 消费事件：
       - `evNodeCompleted`：更新 Current → `IntentCompleted`，清除 drift，启动新 runnable 节点
       - `evNodeFailed`：检查 `CanRetry()`——
         - 可重试：`IncrRetry()`，状态重置为 `IntentRetrying`→`IntentPending`，添加 drift，立即重新调度
         - 不可重试：`MarkFailed` + 级联失败
       - `evNodeTimeout`：终止进程，同 `evNodeFailed` 处理（超时视为一种失败）
    4. 所有节点终止后返回

- [ ] 3.4 实现 `Reconciler.executeNodeWithTimeout`：
  - 使用 `context.WithTimeout(ctx, node.Timeout)` 包装
  - 如果 `node.Timeout == 0`，使用 `config.DefaultTimeout`
  - Spawn → Wait，超时时发送 `evNodeTimeout` 事件
  - 正常完成/失败发送对应事件

- [ ] 3.5 实现重试逻辑：
  - 节点失败后，如果 `CanRetry()`，重置状态为 `IntentPending`
  - 清除 `node.Error`、`node.PID`、`node.Result`
  - 立即通过 `spawnRunnable()` 重新调度
  - 回调 `OnNodeRetry`，通知 UI 显示重试信息

### Task 4: Engine 替换为 Reconciler（AC: #2, #3）

- [ ] 4.1 在 `Manager` 中新增 `ReconcilerConfig` 字段：

  ```go
  type Manager struct {
      // ... 现有字段 ...
      reconcilerConfig ReconcilerConfig
  }

  func NewManager(decomposer *Decomposer, spawner KernelSpawner, config ReconcilerConfig) *Manager
  ```

- [ ] 4.2 修改 `Manager.Execute`：使用 `Reconciler` 替代 `Engine`

  ```go
  func (m *Manager) Execute(ctx context.Context, intentID IntentID, callbacks ReconcilerCallbacks) error {
      // ...
      reconciler, err := NewReconciler(tree, m.spawner, m.reconcilerConfig, callbacks)
      // ...
      return reconciler.Execute(ctx)
  }
  ```

- [ ] 4.3 保留 `Engine` 不删除——`Engine` 作为无调和的简单执行器仍可独立使用；`Reconciler` 是 `Engine` 的增强版本

### Task 5: IPC 协议扩展——Reconciler 事件（AC: #2, #6）

- [ ] 5.1 在 `ipc/protocol.go` 新增 StreamEvent 类型：

  ```go
  StreamIntentNodeRetry     StreamEventType = "intent_node_retry"
  StreamIntentNodeTimeout   StreamEventType = "intent_node_timeout"
  StreamIntentDriftDetected StreamEventType = "intent_drift_detected"
  StreamIntentDriftResolved StreamEventType = "intent_drift_resolved"
  ```

- [ ] 5.2 扩展 `IntentNodeEventPayload`：

  ```go
  type IntentNodeEventPayload struct {
      // ... 现有字段 ...
      RetryAttempt int    `json:"retry_attempt,omitempty"`
      MaxRetries   int    `json:"max_retries,omitempty"`
      DriftType    string `json:"drift_type,omitempty"`
  }
  ```

- [ ] 5.3 扩展 `IntentNodeWire`：

  ```go
  type IntentNodeWire struct {
      // ... 现有字段 ...
      RetryCount   int   `json:"retry_count,omitempty"`
      MaxRetries   int   `json:"max_retries,omitempty"`
      TimeoutMs    int64 `json:"timeout_ms,omitempty"`
  }
  ```

- [ ] 5.4 扩展 `IntentTreeWire`：

  ```go
  type IntentTreeWire struct {
      // ... 现有字段 ...
      Drifts []DriftItemWire `json:"drifts,omitempty"`
  }

  type DriftItemWire struct {
      NodeID     string `json:"node_id"`
      Type       string `json:"type"`
      Message    string `json:"message"`
      DetectedAtMs int64 `json:"detected_at_ms"`
  }
  ```

### Task 6: IPC Server 适配（AC: #2, #3）

- [ ] 6.1 扩展 `intentManager` 接口——`ExecuteIntent` 方法签名增加 Reconciler 回调：

  ```go
  type intentManager interface {
      // ... 现有方法 ...
      ExecuteIntent(ctx context.Context, intentID string,
          onNodeStart func(nodeID string, pid uint64),
          onNodeComplete func(nodeID, result string),
          onNodeFailed func(nodeID, errMsg string),
          onProgress func(completed, total int),
          onNodeRetry func(nodeID string, attempt, maxRetries int),
          onNodeTimeout func(nodeID string),
          onDriftDetected func(nodeID, driftType, message string),
          onDriftResolved func(nodeID string),
      ) error
  }
  ```

- [ ] 6.2 修改 `handleApplyIntent`：发送新增的 Reconciler 事件（retry/timeout/drift）

- [ ] 6.3 修改 `handleIntentStatus`：返回 drift 信息

### Task 7: IPC Adapter 更新（AC: #2）

- [ ] 7.1 更新 `IntentManagerAdapter.ExecuteIntent` 转换 `ReconcilerCallbacks`
- [ ] 7.2 更新 `intentTreeToWire` 和 `intentNodeToWire` 包含新字段
- [ ] 7.3 新增 `driftItemToWire` 转换函数

### Task 8: IPC Client 扩展（AC: #6）

- [ ] 8.1 `ApplyIntentAndWatch` 的 `onEvent` 需处理新增的 StreamEvent 类型（`intent_node_retry`、`intent_node_timeout`、`intent_drift_detected`、`intent_drift_resolved`）——无接口变更，仅 event 类型扩展

### Task 9: CLI 更新——状态显示增强（AC: #6）

- [ ] 9.1 修改 `cmd/rnix/apply.go` 的 `onEvent` 回调：处理 retry/timeout/drift 事件，渲染对应 UI
- [ ] 9.2 修改 `cmd/rnix/intent.go` 的 `runIntentStatus`：显示 drift 列表

### Task 10: UI 渲染增强（AC: #6）

- [ ] 10.1 在 `internal/ui/intent.go` 新增：
  - `RenderIntentNodeRetry(nodeID string, attempt, maxRetries int, mode OutputMode)` — 黄色/橙色重试提示
  - `RenderIntentNodeTimeout(nodeID string, mode OutputMode)` — 红色超时提示
  - `RenderDriftList(drifts []DriftItemWire, mode OutputMode)` — drift 表格/列表
  - 扩展 `RenderIntentTree`：节点状态增加 `retrying`=黄色↻
- [ ] 10.2 扩展 `RenderIntentNodeEvent`：处理新事件类型

### Task 11: Daemon 初始化更新（AC: #2）

- [ ] 11.1 在 `cmd/rnix/main.go` 中传递 `ReconcilerConfig` 给 `NewManager`：

  ```go
  reconcilerConfig := intent.DefaultReconcilerConfig()
  intentMgr := intent.NewManager(decomposer, kernelSpawner, reconcilerConfig)
  ```

### Task 12: Decomposer 初始化三态（AC: #1）

- [ ] 12.1 修改 `Decomposer.Decompose`：分解完成后调用 `tree.InitDesired()` 初始化目标状态

### Task 13: 测试（AC: #1-#6）

- [ ] 13.1 `intent/types_test.go` 新增：
  - `TestIntentNode_CanRetry` — 重试次数未超限返回 true
  - `TestIntentNode_CanRetry_Exhausted` — 重试次数达上限返回 false
  - `TestIntentNode_IncrRetry` — 重试计数递增 + LastFailedAt 更新
  - `TestIntentTree_InitDesired` — 所有节点 desired 为 completed
  - `TestIntentTree_ComputeDrifts` — 正确计算差异
  - `TestIntentTree_AddDrift_ClearDrift` — drift 增删
  - `TestIntentTree_ActiveDrifts` — 返回未解决 drift

- [ ] 13.2 `intent/reconciler_test.go` 新增：
  - `TestReconciler_Execute_AllSuccess` — 全部成功无重试
  - `TestReconciler_Execute_RetrySuccess` — 节点失败后重试成功
  - `TestReconciler_Execute_RetryExhausted` — 重试耗尽后最终失败
  - `TestReconciler_Execute_Timeout` — 节点超时触发重试
  - `TestReconciler_Execute_TimeoutExhausted` — 超时多次后最终失败
  - `TestReconciler_Execute_CascadeAfterExhausted` — 重试耗尽后级联失败
  - `TestReconciler_Execute_ParallelWithRetry` — 并行节点中一个重试不影响另一个
  - `TestReconciler_Execute_ContextCancel` — ctx 取消时正确停止
  - `TestReconciler_Execute_DriftDetectedCallback` — drift 事件回调正确触发
  - `TestReconciler_Execute_DriftResolvedCallback` — 重试成功后 drift 清除
  - `TestReconciler_Execute_NFR40_Latency` — 从检测到 drift 到启动重试 ≤ 5s（用 mock spawner 验证时间差）
  - `TestReconciler_Callbacks` — 所有回调类型均被调用

- [ ] 13.3 `intent/decomposer_test.go` 新增：
  - `TestDecomposer_InitDesired` — 分解后 DesiredNodes 已初始化

- [ ] 13.4 `intent/manager_test.go` 更新：
  - 更新 `NewManager` 调用签名（新增 `ReconcilerConfig` 参数）
  - `TestManager_Execute_WithReconciler` — 验证 Execute 使用 Reconciler 而非 Engine

- [ ] 13.5 `internal/ui/intent_test.go` 新增：
  - `TestRenderIntentNodeRetry_TTY` — 重试事件渲染
  - `TestRenderIntentNodeRetry_JSON` — JSON 模式
  - `TestRenderIntentNodeTimeout_TTY` — 超时事件渲染
  - `TestRenderDriftList_TTY` — drift 表格渲染
  - `TestRenderDriftList_JSON` — JSON 模式
  - `TestRenderDriftList_Empty` — 空 drift 显示"无 drift"

- [ ] 13.6 竞态测试：`go test -race ./intent/... ./ipc/... ./cmd/rnix/... ./internal/ui/...`

## Dev Notes

### 关键架构约束

- **FR107**：系统维护一个意图状态模型（Intent State），包含期望状态（Desired）、当前状态（Current）和差异（Drift），Reconciler 持续监测并消除差异
- **FR108**：Reconciler 采用事件驱动模式——当子任务完成、失败或超时时触发调和循环，自动重新规划和重试
- **NFR40**：Reconciler 从检测到 drift 到启动调和行动延迟 ≤ 5s
- **架构延迟决策**："声明式意图 Reconciler 的具体事件驱动框架选型"——本 Story 选型为：Go channel + goroutine 的事件驱动模式（与 Engine 一致），不引入外部框架

### Reconciler 设计原则

**Reconciler 是 Engine 的增强版**，不是替代品：
- `Engine` 保持不变——简单的一次性执行器，节点失败即级联失败，不重试
- `Reconciler` 基于 Engine 的事件驱动模式，增加：
  1. 重试逻辑（失败/超时后自动重调度）
  2. 超时监控（每节点独立 `context.WithTimeout`）
  3. 三态管理（Desired/Current/Drift 跟踪）
  4. 丰富的事件回调（retry、timeout、drift）

**事件驱动模式**（不是轮询）：
```
reconcileEvent channel
  ├── evNodeCompleted → 更新 Current → 清除 drift → 启动 runnable
  ├── evNodeFailed → 检查 CanRetry → 重试 or 标记失败
  └── evNodeTimeout → 终止进程 → 检查 CanRetry → 重试 or 标记失败
```

**与 Kubernetes Reconciler 的对比**：
- K8s Reconciler：轮询式（`Reconcile(Request) Result`），每次调用比较整体 Desired/Actual
- Rnix Reconciler：事件驱动式（channel 事件触发），每个节点完成/失败立即处理
- 原因：Rnix 的 intent 节点是有限数量的离散任务，不像 K8s 需管理大规模声明式资源

### 三态模型设计

```
Desired = { node_a: completed, node_b: completed, node_c: completed }
                                                        ↕ drift detection
Current = { node_a: completed, node_b: failed,    node_c: pending }
                                     ↓
Drift = [ { node_id: "node_b", type: "node_failed", message: "spawn failed: ..." } ]
                                     ↓
Reconciler → retry node_b → success → clear drift → schedule node_c
```

- **Desired** 在分解时确定，所有节点目标为 `IntentCompleted`，静态不变
- **Current** 就是各 `IntentNode.State`，运行时动态更新
- **Drift** 是 Desired 与 Current 不一致的记录列表，存储在 `IntentTree.Drifts`
- Drift 生命周期：检测（节点失败/超时）→ 记录 → 尝试调和（重试）→ 解决（重试成功）或升级（重试耗尽）

### 超时控制设计

每个节点的超时通过 `context.WithTimeout` 实现：
1. `IntentNode.Timeout` 字段（可选，默认 0 表示使用全局默认）
2. `ReconcilerConfig.DefaultTimeout`（全局默认超时，建议 5min）
3. 超时后：
   - Spawn 的子进程由 `context.WithTimeout` 自动取消（`exec.CommandContext` 的 ctx 传播）
   - 发送 `evNodeTimeout` 事件
   - 与 `evNodeFailed` 走相同重试路径

**注意**：超时不是由 Reconciler 主动 Kill——而是通过 `context.WithTimeout` 的 ctx 传播机制，SpawnIntent 的 ctx 超时后底层的 `exec.CommandContext` 自动终止进程。

### 重试策略设计

简单固定次数重试，不实现指数退避：
1. `IntentNode.MaxRetries` 默认从 `ReconcilerConfig.DefaultMaxRetries`（3）
2. 每次失败/超时：`RetryCount++`
3. `RetryCount < MaxRetries` → 重置节点为 `IntentPending` → 立即重新调度
4. `RetryCount >= MaxRetries` → 最终失败 → 级联下游
5. 重试之间不添加延迟——失败后立即重试（LLM 调用本身有足够的时间间隔）

**节点重置操作**：
```go
node.State = IntentPending
node.Error = ""
node.PID = 0
node.Result = ""
```

### NFR40 合规方案

"从检测到 drift 到启动调和行动延迟 ≤ 5s"：
- Reconciler 事件驱动——channel 事件消费是即时的（Go channel 操作 ~nanosecond 级）
- 从 `evNodeFailed` 到 `spawnRunnable()` 之间只有：mutex lock → CanRetry 判断 → 状态重置 → spawn goroutine
- 实际延迟远小于 5s（微秒级），NFR40 自然满足
- 测试验证：`TestReconciler_Execute_NFR40_Latency` 使用 mock spawner 记录时间戳验证

### 现有代码影响分析

**修改文件**：
| 文件 | 变更类型 | 说明 |
|------|---------|------|
| `intent/types.go` | 扩展 | IntentNode 新增 4 字段 + IntentState 新增 retrying + IntentTree 新增 3 字段 + 新类型 DriftItem/DriftType + 新方法 7 个 |
| `intent/decomposer.go` | 小改 | `Decompose` 末尾调用 `tree.InitDesired()` |
| `intent/manager.go` | 中改 | `NewManager` 新增 `ReconcilerConfig` 参数，`Execute` 改用 `Reconciler`，`EngineCallbacks` → `ReconcilerCallbacks` |
| `ipc/protocol.go` | 扩展 | 4 个新 StreamEvent 类型 + IntentNodeWire/IntentTreeWire/IntentNodeEventPayload 扩展 + 新类型 DriftItemWire |
| `ipc/server.go` | 中改 | `intentManager` 接口签名变更（ExecuteIntent 增加回调参数），handleApplyIntent 发送新事件 |
| `ipc/intent_adapter.go` | 中改 | ExecuteIntent/intentTreeToWire/intentNodeToWire 适配新字段，新增 driftItemToWire |
| `cmd/rnix/apply.go` | 小改 | onEvent 处理新 StreamEvent 类型 |
| `cmd/rnix/intent.go` | 小改 | 状态显示增加 drift 信息 |
| `cmd/rnix/main.go` | 小改 | NewManager 传递 ReconcilerConfig |
| `internal/ui/intent.go` | 扩展 | 3 个新渲染函数 + 扩展现有函数 |

**新建文件**：
| 文件 | 说明 |
|------|------|
| `intent/reconciler.go` | Reconciler 核心实现 |
| `intent/reconciler_test.go` | Reconciler 测试（12+ 个测试） |

**不涉及的文件**：
- `intent/engine.go` — 保持不变，Engine 仍可独立使用
- `intent/dag.go` — 不变
- `intent/cli_caller.go` — 不变
- `kernel/`、`vfs/`、`drivers/`、`compose/`、`shell/` — 不涉及

### 接口兼容性

**`EngineCallbacks` vs `ReconcilerCallbacks`**：
- `ReconcilerCallbacks` 是 `EngineCallbacks` 的超集
- `EngineCallbacks` 保持不变（Engine 不受影响）
- `ReconcilerCallbacks` 新增 `OnNodeRetry`、`OnNodeTimeout`、`OnDriftDetected`、`OnDriftResolved`
- IPC 层的 `intentManager` 接口 `ExecuteIntent` 签名变更——新增回调参数

**`Manager.Execute` 签名变更**：
- 旧：`Execute(ctx, intentID, EngineCallbacks) error`
- 新：`Execute(ctx, intentID, ReconcilerCallbacks) error`
- 影响：`IntentManagerAdapter.ExecuteIntent` 需适配

**`NewManager` 签名变更**：
- 旧：`NewManager(decomposer, spawner) *Manager`
- 新：`NewManager(decomposer, spawner, ReconcilerConfig) *Manager`
- 影响：`cmd/rnix/main.go` 初始化代码

### Reconciler 内部状态机

```
节点状态转移（Reconciler 增强）：
  pending ──spawn──→ executing ──success──→ completed
                         │                      ↑
                         │ fail/timeout          │ retry succeed
                         ↓                      │
                      retrying ──reset──→ pending
                         │
                         │ exhausted
                         ↓
                      failed ──cascade──→ downstream failed
```

### 现有代码模式（必须遵循）

**IPC Method 注册模式** — 参考 `protocol.go` 中已有的 intent 常量：
- StreamEvent 类型命名：`StreamIntent` 前缀 + PascalCase 事件名

**流式 IPC Handler 模式** — 参考 `server.go` 的 `handleApplyIntent`：
- 新事件通过 `syncWriteEvent(conn, &writeMu, StreamEvent{...})` 发送（已有的并发写保护）

**CLI 命令模式** — 参考 `cmd/rnix/apply.go`：
- `onEvent` switch 分支新增处理新 StreamEvent 类型

**测试模式** — 参考 `intent/engine_test.go`：
- mock `KernelSpawner` 控制节点成功/失败/超时
- 测试回调是否正确触发
- `-race` 竞态检测

**错误处理**：
- Reconciler 不是 syscall，使用 `error` 而非 `*SyscallError`
- 错误消息格式：`fmt.Errorf("reconciler: intent %s: node %s: %w", intentID, nodeID, err)`

### 组合矩阵

| 本功能 | 交互对象 | 交互方式 | 需验证 |
|--------|----------|----------|--------|
| Reconciler retry | Engine execute | 替代：Reconciler 替代 Engine 作为 Manager 的执行器 | 是 |
| Reconciler retry | IntentTree.MarkFailed | 增强：重试时不调用 MarkFailed（先检查 CanRetry），耗尽后才调用 | 是 |
| Reconciler retry | IntentTree.cascadeFailure | 增强：仅在重试耗尽后才级联，重试中不级联 | 是 |
| Reconciler timeout | context.WithTimeout | 组合：每节点独立 timeout ctx | 是 |
| Reconciler timeout | KernelSpawner.SpawnIntent | 传递：timeout ctx 传给 SpawnIntent | 是 |
| Reconciler drift | IntentTree.Drifts | 新增：drift 生命周期管理 | 是 |
| Reconciler events | IPC StreamEvent | 扩展：4 个新事件类型 | 是 |
| Reconciler events | syncWriteEvent | 复用：并发写保护 | 是 |
| RetryCount | IntentNodeWire | 映射：新字段需通过 wire 类型传递到 CLI | 是 |
| Drift list | IntentTreeWire | 映射：新字段需通过 wire 类型传递到 CLI | 是 |
| `rnix intent status` | DriftItemWire | 新增：状态显示包含 drift 信息 | 是 |
| Engine（保留） | Reconciler | 共存：Engine 仍可独立使用，Reconciler 是增强版 | 否（独立路径） |

### 不支持的特性（本 Story 范围外）

- **增量更新**：`rnix apply "加上评论功能"` 修改现有意图（Story 19-3）
- **LLM 重新规划**：失败后通过 LLM 重新分解子任务（本 Story 仅简单重试同一任务，不做重新规划）
- **指数退避**：重试间不添加延迟（LLM 调用本身有时间成本，无需退避）
- **意图持久化**：IntentTree 仅内存存储（未来可扩展）
- **自定义重试策略**：仅支持固定次数重试，不支持条件重试、退避策略等

### 为 Story 19-3 预留的扩展点

- `IntentTree.Drifts` 可被增量更新扩展——新增 `DriftTypeNewRequirement` 表示用户更新意图
- `Manager.Apply` 可扩展为接受已有 `IntentID` 参数，实现增量分解
- `rnix intent status` 的 drift 显示可扩展为显示"待处理的增量更新"

### Git Intelligence

最近提交（Story 19.1 完成）：
- `f8bfffb` fix: update default model in Claude CLI
- `af5e151` feat: update traceability report for Story 19.1
- `83eda10` chore: fix story content
- `f951d7f` feat: cr Story 19.1
- `ff86c4a` feat: ds 19.1

Story 19.1 Code Review 修复关键点（需延续到 19.2）：
- `handleApplyIntent` 使用原始 `connScanner` 参数（不新建 scanner）
- `syncWriteEvent` 包装并发写保护（conn 写入需 `sync.Mutex`）
- `intentID` 从 `decomposed` 事件提取（不依赖 `ApplyIntentAndWatch` 返回值）

本 Story 应确保：
- 所有新增 JSON 字段使用 `snake_case`
- `Reconciler` 在 `intent/` 包内，不导入 `kernel/`、`cmd/`、`ipc/`
- `ReconcilerCallbacks` 回调在 mutex 外调用（避免死锁——参考 Engine 的模式）
- 重试逻辑在 mutex 持有期间完成状态重置（原子操作）
- 新增 wire types 中 `time.Duration` 转换为 `int64` 毫秒（JSON 规范）
- drift 的 `detected_at` 使用 `time.Now()` 记录
- 所有并发结构通过 `-race` 测试

### Project Structure Notes

新增文件（2 个）：
- `intent/reconciler.go` — Reconciler 核心：ReconcilerConfig、ReconcilerCallbacks、Reconciler 结构体、Execute/executeNodeWithTimeout/重试逻辑
- `intent/reconciler_test.go` — 12+ 个测试

修改文件（10 个）：
- `intent/types.go` — IntentNode 新增字段、IntentState 新增 retrying、IntentTree 新增字段和方法、DriftItem/DriftType 类型
- `intent/decomposer.go` — Decompose 末尾调用 InitDesired
- `intent/manager.go` — NewManager 签名变更、Execute 使用 Reconciler
- `ipc/protocol.go` — 新 StreamEvent 类型 + wire types 扩展 + DriftItemWire
- `ipc/server.go` — intentManager 接口扩展 + handleApplyIntent 发送新事件
- `ipc/intent_adapter.go` — ExecuteIntent 适配 + wire 转换更新
- `cmd/rnix/apply.go` — onEvent 新分支
- `cmd/rnix/intent.go` — 状态显示增加 drift
- `cmd/rnix/main.go` — NewManager 调用更新
- `internal/ui/intent.go` — 新渲染函数 + 扩展现有函数

不涉及：`intent/engine.go`、`intent/dag.go`、`intent/cli_caller.go`、`kernel/`、`vfs/`、`drivers/`、`compose/`、`shell/`、`agents/`、`skills/`。

### References

- [Source: _bmad-output/planning-artifacts/epics/epic-19-声明式意图与自动规划-declarative-intent-auto-planning.md#Story 19.2]
- [Source: _bmad-output/planning-artifacts/prd/functional-requirements.md#FR107, FR108]
- [Source: _bmad-output/planning-artifacts/prd/non-functional-requirements.md#NFR40]
- [Source: _bmad-output/planning-artifacts/architecture/core-architectural-decisions.md#延迟决策 - 声明式意图 Reconciler]
- [Source: _bmad-output/project-context.md]
- [Source: intent/types.go — IntentTree/IntentNode/IntentState]
- [Source: intent/engine.go — Engine.Execute/executeNode/spawnRunnable/nodeEvent]
- [Source: intent/manager.go — Manager/NewManager/Apply/Confirm/Execute/Status/ListActive]
- [Source: intent/decomposer.go — Decomposer/LLMCaller/Decompose/decomposePromptTemplate]
- [Source: ipc/protocol.go — MethodApplyIntent/MethodIntentStatus/StreamEvent types/IntentTreeWire/IntentNodeWire/IntentNodeEventPayload]
- [Source: ipc/server.go — intentManager interface/handleApplyIntent/syncWriteEvent]
- [Source: ipc/intent_adapter.go — IntentManagerAdapter/intentTreeToWire/intentNodeToWire/IntentKernelSpawner]
- [Source: ipc/client.go — ApplyIntentAndWatch/ConfirmIntent/IntentStatus]
- [Source: cmd/rnix/apply.go — runApply/onEvent switch]
- [Source: cmd/rnix/intent.go — runIntentStatus]
- [Source: cmd/rnix/main.go — daemon 初始化/IntentManager 注入]
- [Source: internal/ui/intent.go — RenderIntentTree/RenderIntentProgress/RenderIntentNodeEvent]
- [Source: _bmad-output/implementation-artifacts/19-1-intent-declaration-and-task-decomposition.md]

## Dev Agent Record

### Agent Model Used

{{agent_model_name_version}}

### Debug Log References

### Completion Notes List

### File List
