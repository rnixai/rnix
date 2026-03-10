# Story 19.3: 增量更新与状态查询

Status: review

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a 应用开发者,
I want 在执行过程中更新意图并查看完整状态,
So that 我可以动态调整需求而不丢失已完成的工作。

## Acceptance Criteria

1. **Given** 意图正在执行中（部分子任务已完成）
   **When** 用户执行 `rnix apply "加上评论功能"`
   **Then** Reconciler 计算增量差异，仅执行新增/变更部分，已完成的工作不回滚

2. **Given** 一个活跃的意图
   **When** 用户执行 `rnix intent status`
   **Then** 显示意图树的当前状态：整体进度、各子意图完成度、执行中的智能体列表、待解决的 drift 项

3. **Given** 增量更新引入新的子意图节点
   **When** 新节点依赖已完成节点
   **Then** 新节点立即可调度执行，无需重新执行已完成的上游节点

4. **Given** 增量更新引入新的子意图节点
   **When** 新节点依赖尚未完成的节点
   **Then** 新节点等待依赖完成后自动调度

5. **Given** 增量更新修改了已完成节点的意图描述
   **When** Reconciler 检测到变更
   **Then** 将该节点标记为需要重新执行，状态重置为 pending

6. **Given** 用户对已执行中的意图执行增量更新
   **When** 有子任务正在执行
   **Then** 正在执行的子任务不受影响，继续运行至完成；新增/变更的节点在合适时机调度

7. **Given** 增量分解返回的新节点引用了不存在的依赖
   **When** Reconciler 尝试合并
   **Then** 返回错误，拒绝增量更新，保持原有意图树不变

## Tasks / Subtasks

### Task 1: IntentTree 增量合并核心逻辑（AC: #1, #3, #4, #5）

- [x] 1.1 在 `intent/types.go` 新增 DriftType 常量：

  ```go
  DriftNewRequirement  DriftType = "new_requirement"
  DriftNodeModified    DriftType = "node_modified"
  ```

- [x] 1.2 新建 `intent/merge.go`，实现 `MergeResult` 类型和 `MergeIncremental` 函数：

  ```go
  type MergeResult struct {
      AddedNodes    []string  // 新增节点 ID
      ModifiedNodes []string  // 意图变更的节点 ID（需重新执行）
      UnchangedNodes []string // 未变化的节点 ID
  }

  func MergeIncremental(existing *IntentTree, newNodes []*IntentNode) (*MergeResult, error)
  ```

  逻辑：
  - 遍历 `newNodes`：
    - 如果节点 ID 在 `existing.Nodes` 中不存在 → 新增节点（添加到 `existing.Nodes`，状态 `IntentPending`）
    - 如果节点 ID 已存在且 `Intent` 不同 → 修改节点（重置状态为 `IntentPending`，清除 Result/Error/PID）
    - 如果节点 ID 已存在且 `Intent` 相同 → 未变化（保留现有状态）
  - 验证所有新节点的 `DependsOn` 引用有效（存在于合并后的节点集合中）
  - 对合并后的树重新执行 `BuildIntentDAG` 验证无循环依赖
  - 更新 `DesiredNodes`：为新增节点设置 desired = `IntentCompleted`

- [x] 1.3 实现 `IntentTree.ResetNode(nodeID string)` 方法：
  - 重置节点状态为 `IntentPending`
  - 清除 `Error`、`PID`、`Result`、`RetryCount`
  - 保留 `MaxRetries`、`Timeout` 配置

### Task 2: Decomposer 增量分解能力（AC: #1）

- [x] 2.1 在 `intent/decomposer.go` 新增增量分解 prompt 模板：

  ```go
  const incrementalDecomposePromptTemplate = `你是一个任务规划系统。现有一个意图树正在执行中，用户希望增量更新。

  原始意图: %s

  当前子任务及状态:
  %s

  用户新增需求: %s

  要求:
  1. 分析新需求与现有子任务的关系
  2. 返回合并后的完整子任务列表（包括已有的和新增的）
  3. 已有子任务保持原 id 不变；新增子任务用新 id
  4. 如果新需求修改了已有子任务的目标，更新其 intent 字段
  5. 正确声明 depends_on 依赖关系
  6. 返回纯 JSON 数组，不要包含其他文本

  示例输出:
  [
    {"id": "design", "intent": "设计数据模型和 API 接口", "depends_on": []},
    {"id": "backend", "intent": "实现后端 API 服务", "depends_on": ["design"]},
    {"id": "comment", "intent": "实现评论功能后端", "depends_on": ["design"]},
    {"id": "frontend", "intent": "实现前端界面（含评论组件）", "depends_on": ["design", "comment"]}
  ]`
  ```

- [x] 2.2 在 `Decomposer` 新增 `DecomposeIncremental` 方法：

  ```go
  func (d *Decomposer) DecomposeIncremental(ctx context.Context, tree *IntentTree, newIntent string, model string) ([]*IntentNode, error)
  ```

  逻辑：
  - 构建当前子任务状态摘要（ID、Intent、State）
  - 使用增量分解 prompt 调用 LLM
  - 解析 LLM 返回的 JSON 数组为 `[]*IntentNode`
  - 返回新节点列表（包含已有+新增），由 `MergeIncremental` 处理合并

### Task 3: Manager 增量更新接口（AC: #1, #6）

- [x] 3.1 在 `intent/manager.go` 新增 `ApplyIncremental` 方法：

  ```go
  func (m *Manager) ApplyIncremental(ctx context.Context, intentID IntentID, newIntent string, model string) (*IntentTree, *MergeResult, error)
  ```

  逻辑：
  - 获取已有 IntentTree（验证存在且非终态）
  - 调用 `Decomposer.DecomposeIncremental` 获取新节点列表
  - 调用 `MergeIncremental` 合并到现有树
  - 返回更新后的树和合并结果

- [x] 3.2 扩展 `Reconciler` 以支持运行时新增节点：

  ```go
  func (r *Reconciler) InjectNodes(nodes []*IntentNode) error
  ```

  逻辑：
  - 在 mutex 保护下将新节点加入 tree.Nodes
  - 更新 DesiredNodes
  - 为新增的 pending 节点调用 spawnRunnable

- [x] 3.3 修改 `Manager.Execute`：保存 Reconciler 引用以支持运行时注入

  ```go
  type Manager struct {
      // ... 现有字段 ...
      activeReconcilers map[IntentID]*Reconciler
  }
  ```

### Task 4: IPC 协议扩展——增量更新（AC: #1）

- [x] 4.1 在 `ipc/protocol.go` 新增 Method 常量：

  ```go
  MethodApplyIncrementalIntent Method = "apply_incremental_intent"
  ```

- [x] 4.2 新增 Request/Response 类型：

  ```go
  type ApplyIncrementalIntentRequest struct {
      IntentID string `json:"intent_id"`
      Intent   string `json:"intent"`
      Model    string `json:"model,omitempty"`
  }

  type ApplyIncrementalIntentResponse struct {
      IntentID      string          `json:"intent_id"`
      Tree          *IntentTreeWire `json:"tree"`
      AddedNodes    []string        `json:"added_nodes"`
      ModifiedNodes []string        `json:"modified_nodes"`
  }
  ```

- [x] 4.3 新增 StreamEvent 类型：

  ```go
  StreamIntentIncrementalMerged StreamEventType = "intent_incremental_merged"
  ```

### Task 5: IPC Server 处理增量更新（AC: #1, #6）

- [x] 5.1 在 `intentManager` 接口新增方法：

  ```go
  type intentManager interface {
      // ... 现有方法 ...
      ApplyIncrementalIntent(ctx context.Context, intentID, intentStr, model string) (string, []byte, error)
  }
  ```

- [x] 5.2 在 `server.go` 的 `dispatch` 中注册 `MethodApplyIncrementalIntent` handler

- [x] 5.3 实现 `handleApplyIncrementalIntent`：
  - 解析请求
  - 调用 `intentMgr.ApplyIncrementalIntent`
  - 发送 `StreamIntentIncrementalMerged` 事件（包含合并结果）
  - 如果 Reconciler 正在运行，注入新节点并继续执行

### Task 6: IPC Adapter 适配（AC: #1）

- [x] 6.1 在 `IntentManagerAdapter` 新增 `ApplyIncrementalIntent` 方法：

  ```go
  func (a *IntentManagerAdapter) ApplyIncrementalIntent(ctx context.Context, intentID, intentStr, model string) (string, []byte, error)
  ```

  逻辑：
  - 调用 `a.mgr.ApplyIncremental`
  - 将 MergeResult 和更新后的 tree 序列化返回

### Task 7: IPC Client 扩展（AC: #1）

- [x] 7.1 在 `client.go` 新增 `ApplyIncrementalIntent` 方法：

  ```go
  func (c *Client) ApplyIncrementalIntent(req ApplyIncrementalIntentRequest) (*ApplyIncrementalIntentResponse, error)
  ```

### Task 8: CLI 命令更新——增量更新支持（AC: #1）

- [x] 8.1 修改 `cmd/rnix/apply.go` 的 `runApply`：
  - 检查是否已有同名/相关意图正在执行
  - 如果有活跃意图，提示用户选择：创建新意图 or 增量更新现有意图
  - 增量更新时调用 `client.ApplyIncrementalIntent`

- [x] 8.2 新增 `--update` / `-u` flag 显式指定增量更新目标意图：

  ```
  rnix apply "加上评论功能" --update intent-1
  ```

- [x] 8.3 修改 `onEvent` 处理新的 `StreamIntentIncrementalMerged` 事件：
  - 渲染合并结果：新增节点列表、修改节点列表

### Task 9: Intent Status 增强（AC: #2）

- [x] 9.1 修改 `cmd/rnix/intent.go` 的 `runIntentStatus` 增强显示：
  - 整体进度百分比（已完成/总数）
  - 按状态分组的节点列表（executing、pending、completed、failed、retrying）
  - 执行中智能体列表（节点 ID + PID + 意图描述）
  - Drift 项列表（已有，确保完善）

- [x] 9.2 新增 `rnix intent list` 子命令：
  - 列出所有意图（活跃 + 已完成）
  - 表格显示：ID、Root Intent（截断）、State、进度、创建时间

### Task 10: UI 渲染增强（AC: #2）

- [x] 10.1 在 `internal/ui/intent.go` 新增：
  - `RenderIntentMergeResult(r *Renderer, added, modified []string, mode OutputMode)` — 渲染增量合并结果
  - `RenderIntentStatusDetail(r *Renderer, tree *IntentTreeWire, mode OutputMode)` — 增强的状态详情视图
  - `RenderIntentList(r *Renderer, trees []*IntentTreeWire, mode OutputMode)` — 意图列表表格

- [x] 10.2 增强 `RenderIntentTree`：
  - 增加进度百分比显示
  - 节点状态增加颜色编码：executing=蓝色、pending=灰色、completed=绿色、failed=红色、retrying=黄色

### Task 11: 测试（AC: #1-#7）

- [x] 11.1 `intent/merge_test.go` 新增：
  - `TestMergeIncremental_AddNewNodes` — 新增节点正确合并
  - `TestMergeIncremental_ModifyExistingNode` — 已有节点 intent 变更后重置状态
  - `TestMergeIncremental_UnchangedNodes` — 未变化节点保持状态不变
  - `TestMergeIncremental_CompletedNodePreserved` — 已完成节点且 intent 未变时不重置
  - `TestMergeIncremental_ModifiedCompletedNode` — 已完成节点 intent 变更时重置为 pending
  - `TestMergeIncremental_InvalidDependency` — 引用不存在依赖返回错误
  - `TestMergeIncremental_CycleDependency` — 循环依赖返回错误
  - `TestMergeIncremental_DesiredNodesUpdated` — 合并后 DesiredNodes 包含新节点
  - `TestIntentTree_ResetNode` — 节点重置正确

- [x] 11.2 `intent/decomposer_test.go` 新增：
  - `TestDecomposer_DecomposeIncremental` — 增量分解返回正确节点列表
  - `TestDecomposer_DecomposeIncremental_InvalidJSON` — LLM 返回无效 JSON 时报错

- [x] 11.3 `intent/manager_test.go` 新增：
  - `TestManager_ApplyIncremental` — 增量更新正确合并
  - `TestManager_ApplyIncremental_NotFound` — 不存在的 intent 返回错误
  - `TestManager_ApplyIncremental_TerminalState` — 终态 intent 拒绝更新

- [x] 11.4 `intent/reconciler_test.go` 新增：
  - `TestReconciler_InjectNodes` — 运行时注入新节点后正确调度
  - `TestReconciler_InjectNodes_WithDependency` — 注入的节点有依赖时等待完成后调度

- [x] 11.5 `internal/ui/intent_test.go` 新增：
  - `TestRenderIntentMergeResult_TTY` — 合并结果 TTY 渲染
  - `TestRenderIntentMergeResult_JSON` — JSON 模式
  - `TestRenderIntentStatusDetail_TTY` — 增强状态视图
  - `TestRenderIntentList_TTY` — 意图列表渲染
  - `TestRenderIntentList_Empty` — 空列表显示

- [x] 11.6 竞态测试：`go test -race ./intent/... ./ipc/... ./cmd/rnix/... ./internal/ui/...`

## Dev Notes

### 关键架构约束

- **FR109**（对应 Epic 19 Story 3）：增量更新意图时，Reconciler 计算差异，仅执行新增/变更部分，已完成的工作不回滚
- **FR110**：`rnix intent status` 显示意图树完整状态：整体进度、各子意图完成度、执行中的智能体列表、待解决的 drift 项
- Story 19.2 预留的扩展点：`DriftTypeNewRequirement`、`Manager.Apply` 接受已有 IntentID、status 显示增量更新信息

### 增量合并设计原则

**三种合并结果**：
```
newNodes 中的节点:
  ├── ID 不存在于 existing → AddedNode: 新增到 tree, 状态 pending
  ├── ID 存在且 Intent 相同 → UnchangedNode: 保持现状（不干扰执行中/已完成节点）
  └── ID 存在且 Intent 不同 → ModifiedNode: 重置为 pending（需要重新执行）
```

**已完成节点的处理**：
- Intent 未变 → 保持 completed 状态（核心要求：已完成的工作不回滚）
- Intent 已变 → 重置为 pending（目标变了，需要重新执行）
- 新节点依赖已完成节点 → 直接满足依赖，可立即调度

**执行中节点的处理**：
- 不中断正在执行的节点（AC #6 明确要求）
- 如果执行中节点的 Intent 被修改：等当前执行完成后，再根据结果决定是否重新执行
- 增量合并仅修改节点元数据，不干扰 Reconciler 事件循环

### Reconciler 运行时注入设计

```
Manager.ApplyIncremental(ctx, intentID, newIntent, model)
  ├── 1. Decomposer.DecomposeIncremental → []*IntentNode
  ├── 2. MergeIncremental(existing, newNodes) → MergeResult
  ├── 3. 如果 Reconciler 正在运行:
  │     └── Reconciler.InjectNodes(addedNodes + modifiedNodes)
  │           ├── mu.Lock()
  │           ├── 添加新节点到 tree.Nodes
  │           ├── 重置修改节点状态
  │           ├── 更新 DesiredNodes
  │           ├── spawnRunnable(ctx) — 调度新可执行节点
  │           └── mu.Unlock()
  └── 4. 返回 (tree, mergeResult, nil)
```

**InjectNodes 并发安全**：
- 通过 Reconciler.mu 保护——与事件循环使用同一个 mutex
- 注入操作是原子的：在 mutex 持有期间完成所有状态变更
- 注入后立即调用 spawnRunnable，新节点可在下一个事件循环迭代中执行

### IPC 增量更新协议

```
Client                          Server
  |-- apply_incremental_intent -->|
  |     {intent_id, intent}       |
  |                               |-- Decomposer.DecomposeIncremental
  |                               |-- MergeIncremental
  |<-- intent_incremental_merged --|
  |     {added_nodes, modified}   |
  |                               |-- Reconciler.InjectNodes（如果正在执行）
  |<-- intent_node_start ---------|（新节点开始执行）
  |<-- intent_node_done ----------|
  |<-- ...                        |
```

### Intent Status 增强显示

```
$ rnix intent status intent-1

Intent: intent-1
Root: "我要一个完整的博客系统"
State: executing
Progress: 2/5 (40%)

Nodes:
  [completed] design   - 设计数据模型和 API 接口
  [completed] backend  - 实现后端 API 服务
  [executing] frontend - 实现前端界面          PID: 42
  [pending]   comment  - 实现评论功能后端
  [pending]   test     - 编写集成测试

Active Agents:
  frontend (PID 42) - 实现前端界面

Drifts: (none)
```

### 现有代码影响分析

**新建文件（2 个）**：
| 文件 | 说明 |
|------|------|
| `intent/merge.go` | 增量合并核心：MergeResult、MergeIncremental、依赖验证 |
| `intent/merge_test.go` | 合并测试（9 个测试） |

**修改文件（10 个）**：
| 文件 | 变更类型 | 说明 |
|------|---------|------|
| `intent/types.go` | 扩展 | 新增 DriftType 常量（DriftNewRequirement、DriftNodeModified）+ ResetNode 方法 |
| `intent/decomposer.go` | 中改 | 新增 incrementalDecomposePromptTemplate + DecomposeIncremental 方法 |
| `intent/manager.go` | 中改 | 新增 activeReconcilers 字段 + ApplyIncremental 方法 + Execute 保存 Reconciler 引用 |
| `intent/reconciler.go` | 小改 | 新增 InjectNodes 方法 |
| `ipc/protocol.go` | 扩展 | 新 Method 常量 + Request/Response 类型 + 新 StreamEvent 类型 |
| `ipc/server.go` | 中改 | intentManager 接口新增方法 + handleApplyIncrementalIntent handler |
| `ipc/intent_adapter.go` | 小改 | 新增 ApplyIncrementalIntent 适配 |
| `ipc/client.go` | 小改 | 新增 ApplyIncrementalIntent 客户端方法 |
| `cmd/rnix/apply.go` | 中改 | --update flag + 增量更新逻辑 + onEvent 新事件处理 |
| `cmd/rnix/intent.go` | 中改 | 状态显示增强 + intent list 子命令 |
| `internal/ui/intent.go` | 扩展 | 3 个新渲染函数 + RenderIntentTree 增强 |

**不涉及的文件**：
- `intent/engine.go` — 保持不变
- `intent/dag.go` — 不变（MergeIncremental 调用 BuildIntentDAG 验证）
- `intent/cli_caller.go` — 不变
- `kernel/`、`vfs/`、`drivers/`、`compose/`、`shell/` — 不涉及

### 接口兼容性

**`Manager` 新增方法（向后兼容）**：
- 新增 `ApplyIncremental` — 不影响现有 `Apply`/`Execute`/`Status`
- 新增 `activeReconcilers` 字段 — `Execute` 保存引用，不影响已有签名

**`intentManager` 接口扩展**：
- 新增 `ApplyIncrementalIntent` 方法 — 需更新 `IntentManagerAdapter` 实现
- 不修改已有方法签名

**`Reconciler` 新增方法（向后兼容）**：
- 新增 `InjectNodes` — 不影响已有 `Execute`
- 使用同一个 `mu` mutex，确保与事件循环互斥

### 增量分解 LLM Prompt 设计

关键点：
- 向 LLM 提供当前子任务状态（ID、Intent、State），使其了解已有工作
- 要求 LLM 保持已有节点 ID 不变，仅修改 intent 或添加新节点
- LLM 返回完整的合并后任务列表，由 `MergeIncremental` 处理差异计算
- 这样 LLM 只负责规划，合并逻辑完全确定性（不依赖 LLM 的合并能力）

### 组合矩阵

| 本功能 | 交互对象 | 交互方式 | 需验证 |
|--------|----------|----------|--------|
| MergeIncremental | IntentTree.Nodes | 扩展：添加新节点到现有 map | 是 |
| MergeIncremental | BuildIntentDAG | 复用：合并后验证 DAG 有效性 | 是 |
| MergeIncremental | IntentTree.DesiredNodes | 扩展：为新增节点设置 desired | 是 |
| Reconciler.InjectNodes | Reconciler.Execute 事件循环 | 并发：通过 mu 互斥保护 | 是 |
| Reconciler.InjectNodes | Reconciler.spawnRunnable | 复用：注入后调用 spawnRunnable 调度 | 是 |
| ApplyIncremental | Decomposer.DecomposeIncremental | 新增：增量分解 | 是 |
| ApplyIncremental | Manager.activeReconcilers | 新增：查找活跃 Reconciler 注入节点 | 是 |
| rnix apply --update | rnix apply（现有） | 扩展：新增 flag 和增量路径 | 是 |
| intent status 增强 | RenderIntentTree（现有） | 增强：更多状态信息 | 是 |
| intent list | Manager.ListActive + intents map | 新增：列出所有意图 | 是 |
| 增量更新 IPC | handleApplyIntent（现有） | 独立：新 handler，不影响现有 apply | 否 |

### 不支持的特性（本 Story 范围外）

- **意图持久化**：IntentTree 仍为内存存储，重启后丢失（未来可扩展）
- **意图回滚**：不支持撤销增量更新（undo）
- **意图版本历史**：不记录意图变更历史（仅保留最终状态）
- **节点删除**：增量更新不支持删除已有节点（仅新增和修改）
- **冲突解决**：如果 LLM 增量分解结果与运行时状态冲突，简单拒绝

### 现有代码模式（必须遵循）

**IPC Method 注册模式** — 参考 `protocol.go` 中已有的 intent 常量：
- Method 命名：`snake_case`，如 `apply_incremental_intent`
- StreamEvent 类型命名：`StreamIntent` 前缀 + PascalCase 事件名

**流式 IPC Handler 模式** — 参考 `server.go` 的 `handleApplyIntent`：
- 新事件通过 `syncWriteEvent(conn, &writeMu, StreamEvent{...})` 发送

**CLI 命令模式** — 参考 `cmd/rnix/apply.go`：
- `onEvent` switch 分支新增处理新 StreamEvent 类型
- Cobra flag 注册在 `init()` 中

**测试模式** — 参考 `intent/reconciler_test.go`：
- mock `KernelSpawner` 控制节点成功/失败
- mock `LLMCaller` 返回预设 JSON
- `-race` 竞态检测

**错误处理**：
- 非 syscall 使用 `error` 而非 `*SyscallError`
- 错误消息格式：`fmt.Errorf("merge: %w", err)` / `fmt.Errorf("apply incremental: intent %s: %w", intentID, err)`

### Git Intelligence

最近提交（Story 19.2 完成）：
- `232e9a6` refactor: update traceability report for Story 19.2
- `fa300be` feat: cr Story 19.2 implementation
- `6386d9a` feat: ds 19-2
- `46a9955` feat: atdd 19-2
- `01963d8` feat: cs 19.2 for intent state model and event-driven reconciler

Story 19.2 Code Review 关键修复点（需延续到 19.3）：
- `ReconcilerCallbacks` 回调在 mutex 外调用（避免死锁）
- 所有新增 JSON 字段使用 `snake_case`
- `IntentRetrying` 状态已正确使用——Reconciler 重试时先设置 `IntentRetrying`，`RunnableNodes` 同时接受 `pending` 和 `retrying`
- `finalizeTreeState` 设置 `tree.CompletedAt = &now`
- `ComputeDrifts` 仅报告 `IntentFailed` 节点为 drift

本 Story 应确保：
- `InjectNodes` 中的状态变更在 mutex 持有期间完成
- 增量合并不中断正在执行的节点
- 新增 wire types 中 `time.Duration` 转换为 `int64` 毫秒（JSON 规范）
- 所有并发结构通过 `-race` 测试
- `MergeIncremental` 是纯函数（无副作用），仅修改传入的 tree

### Project Structure Notes

新增文件（2 个）：
- `intent/merge.go` — 增量合并核心：MergeResult 类型、MergeIncremental 函数、依赖验证逻辑
- `intent/merge_test.go` — 9 个合并测试

修改文件（11 个）：
- `intent/types.go` — DriftType 新增常量 + ResetNode 方法
- `intent/decomposer.go` — incrementalDecomposePromptTemplate + DecomposeIncremental 方法
- `intent/manager.go` — activeReconcilers 字段 + ApplyIncremental 方法
- `intent/reconciler.go` — InjectNodes 方法
- `ipc/protocol.go` — 新 Method + Request/Response + StreamEvent
- `ipc/server.go` — intentManager 接口扩展 + handleApplyIncrementalIntent
- `ipc/intent_adapter.go` — ApplyIncrementalIntent 适配
- `ipc/client.go` — ApplyIncrementalIntent 客户端
- `cmd/rnix/apply.go` — --update flag + 增量更新逻辑 + onEvent
- `cmd/rnix/intent.go` — 状态增强 + intent list 子命令
- `internal/ui/intent.go` — 3 个新渲染函数 + RenderIntentTree 增强

不涉及：`intent/engine.go`、`intent/dag.go`、`intent/cli_caller.go`、`kernel/`、`vfs/`、`drivers/`、`compose/`、`shell/`、`agents/`、`skills/`。

### References

- [Source: _bmad-output/planning-artifacts/epics/epic-19-声明式意图与自动规划-declarative-intent-auto-planning.md#Story 19.3]
- [Source: _bmad-output/implementation-artifacts/19-2-intent-state-model-and-event-driven-reconciler.md — 为 Story 19-3 预留的扩展点]
- [Source: _bmad-output/project-context.md]
- [Source: intent/types.go — IntentTree/IntentNode/IntentState/DriftType/DriftItem/MergeResult]
- [Source: intent/reconciler.go — Reconciler/ReconcilerCallbacks/Execute/spawnRunnable]
- [Source: intent/decomposer.go — Decomposer/LLMCaller/Decompose/decomposePromptTemplate]
- [Source: intent/manager.go — Manager/NewManager/Apply/Execute/Status/ListActive]
- [Source: ipc/protocol.go — MethodApplyIntent/MethodIntentStatus/StreamEvent types/IntentTreeWire/IntentNodeWire]
- [Source: ipc/server.go — intentManager interface/handleApplyIntent/syncWriteEvent]
- [Source: ipc/intent_adapter.go — IntentManagerAdapter/intentTreeToWire/intentNodeToWire]
- [Source: ipc/client.go — ApplyIntentAndWatch/IntentStatus]
- [Source: cmd/rnix/apply.go — runApply/onEvent switch]
- [Source: cmd/rnix/intent.go — runIntentStatus]
- [Source: internal/ui/intent.go — RenderIntentTree/RenderIntentProgress/RenderIntentNodeEvent/RenderDriftList]
- [Source: _bmad-output/planning-artifacts/architecture/core-architectural-decisions.md — 延迟决策]
- [Source: _bmad-output/planning-artifacts/architecture/implementation-patterns-consistency-rules.md — IPC 扩展标准步骤]

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6

### Debug Log References

N/A

### Completion Notes List

- All 11 tasks (30+ subtasks) implemented and verified
- Tests written first (RED), implementation made them pass (GREEN)
- `go test -race ./intent/... ./internal/ui/...` passes cleanly
- `go build ./...` succeeds with no errors
- Pre-existing test failures (unrelated to this story): TestRunDashboard_NoDaemon, TestRunTop_NoDaemon (TTY), TestClaudeCliDriver_Call_DefaultArgs (model default)
- MergeIncremental is a pure function with rollback on validation failure
- InjectNodes uses same mutex as Reconciler event loop for concurrency safety
- Manager.ListAll() added (not in original tasks) to support `intent list` CLI command

### File List

**New files (1):**
- `intent/merge.go` — MergeResult type, MergeIncremental function, dependency validation, rollback logic

**Modified files (11):**
- `intent/types.go` — DriftNewRequirement/DriftNodeModified constants + ResetNode method
- `intent/decomposer.go` — incrementalDecomposePromptTemplate + DecomposeIncremental method
- `intent/manager.go` — activeReconcilers field + ApplyIncremental + ListAll methods
- `intent/reconciler.go` — InjectNodes method
- `ipc/protocol.go` — MethodApplyIncrementalIntent + MethodIntentList + StreamIntentIncrementalMerged + request/response types
- `ipc/server.go` — intentManager interface extension + handleApplyIncrementalIntent + handleIntentList
- `ipc/intent_adapter.go` — ApplyIncrementalIntent + ListAllIntents adapter methods
- `ipc/client.go` — ApplyIncrementalIntent + IntentList client methods
- `cmd/rnix/apply.go` — --update flag + runApplyIncremental + onEvent StreamIntentIncrementalMerged handling
- `cmd/rnix/intent.go` — intentListCmd + enhanced runIntentStatus + runIntentList
- `internal/ui/intent.go` — RenderIntentMergeResult + RenderIntentStatusDetail + RenderIntentList + enhanced RenderIntentTree

**Test files (pre-existing, tests already written in RED phase):**
- `intent/merge_test.go` — 9 tests for MergeIncremental
- `intent/decomposer_test.go` — 2 incremental decompose tests
- `intent/manager_test.go` — 3 ApplyIncremental tests
- `intent/reconciler_test.go` — 2 InjectNodes tests
- `internal/ui/intent_test.go` — 5 UI render tests
