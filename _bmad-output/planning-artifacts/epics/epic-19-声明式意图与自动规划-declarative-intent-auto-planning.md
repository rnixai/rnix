# Epic 19: 声明式意图与自动规划

用户只需声明期望状态，系统自动分解子任务、分配智能体，Reconciler 持续调和差异。

## Story 19.1: 意图声明与任务分解

As a 应用开发者,
I want 通过 `rnix apply "高层意图"` 声明期望状态，系统自动分解为子意图树,
So that 我可以用自然语言描述目标而不需要手动编排。

**Acceptance Criteria:**

**Given** 用户执行 `rnix apply "我要一个完整的博客系统"`
**When** 系统接收意图
**Then** 系统将高层意图递归分解为子意图树（Intent Tree），每个子意图对应一个或多个智能体进程

**Given** 意图分解完成
**When** 系统展示分解结果
**Then** 显示子任务列表、依赖关系和执行计划，等待用户确认后开始执行

**Given** `intent/types.go` 中 `IntentTree` 和 `IntentNode` 已实现
**When** 创建一个新的 IntentTree
**Then** IntentTree 包含 `ID`（IntentID）、`RootIntent`、`State`、`Nodes`（map[string]*IntentNode）、`CreatedAt`、`CompletedAt` 字段
**And** IntentNode 包含 `ID`、`Intent`、`Agent`、`Model`、`DependsOn`、`State`、`PID`、`Result`、`Error`、`Children` 字段
**And** IntentState 定义 `pending`、`decomposing`、`await_confirm`、`executing`、`completed`、`failed` 六种状态（FR106）

**Given** `intent/types.go` 中 IntentTree 辅助方法已实现
**When** 调用辅助方法
**Then** `Progress()` 返回 `(completed, total int)` 正确计算完成进度
**And** `RunnableNodes()` 返回所有依赖已满足且状态为 pending 的节点
**And** `MarkCompleted(nodeID, result)` 标记节点完成并检查下游依赖
**And** `MarkFailed(nodeID, errMsg)` 标记失败并通过 `cascadeFailure` 递归级联标记依赖下游为 failed
**And** `IsTerminal()` 在所有节点都为 completed 或 failed 时返回 true

**Given** `intent/dag.go` 中 DAG 构建和拓扑排序已实现
**When** 调用 `BuildIntentDAG(tree)` 构建 DAG
**Then** 正确构建节点间依赖关系图
**And** `DetectCycle(dag)` 使用三色 DFS 检测循环依赖，存在循环时返回错误
**And** `TopologicalSort(dag)` 使用分层 Kahn 算法返回分层排序结果，每层可并行执行
**And** 无依赖的节点归入同一层，有依赖的节点在其依赖层之后（FR110）

**Given** `intent/decomposer.go` 中 `Decomposer` 已实现
**When** 调用 `Decomposer.Decompose(ctx, intent, model)` 分解意图
**Then** 通过 `LLMCaller` 接口调用 LLM，传入 `decomposePromptTemplate` 格式的分解 prompt
**And** 解析 LLM 返回的 JSON 数组为 `[]*IntentNode`
**And** 调用 `BuildIntentDAG` + `DetectCycle` 验证无循环依赖
**And** 返回构建好的 `*IntentTree`，根意图记录在 `RootIntent` 字段

**Given** LLM 分解返回无效结果
**When** 返回内容不是有效 JSON、依赖存在循环、或结果为空
**Then** 分别返回 JSON 解析错误、循环依赖错误、或空结果错误
**And** 错误消息包含足够上下文信息（intent 内容、错误原因）

**Given** `intent/engine.go` 中事件驱动执行引擎已实现
**When** 调用 `Engine.Execute(ctx)` 执行意图树
**Then** 使用 `nodeEvent` channel 收集完成/失败事件，主循环消费事件并推进状态
**And** 无依赖的节点并行执行，有依赖的节点等待上游完成后立即调度（非分层阻塞模式）
**And** 节点失败时通过 `MarkFailed` 级联标记下游为 failed，但不终止已启动的独立分支
**And** ctx 取消时正确停止所有 goroutine（FR106、FR110）

**Given** `rnix apply` CLI 命令及 `--yes`/`-y` flag 已实现（`cmd/rnix/apply.go`）
**When** 用户执行 `rnix apply "意图" --yes`
**Then** 跳过用户确认步骤，系统直接开始执行
**And** 不带 `--yes` 时，显示分解结果并等待用户确认后执行

**Given** IPC 协议已扩展（`ipc/protocol.go`）
**When** 客户端调用 `apply_intent` 方法
**Then** 流式返回 7 种 StreamEvent：`intent_decomposed`、`intent_confirm_required`、`intent_node_start`、`intent_node_done`、`intent_node_failed`、`intent_progress`、`intent_complete`
**And** `intent_status` 方法非流式返回 `IntentStatusResponse`，包含活跃 IntentTree 列表（FR111）

## Story 19.2: 意图状态模型与事件驱动 Reconciler

As a 应用开发者,
I want 系统维护意图状态模型并通过事件驱动的 Reconciler 自动调和差异,
So that 子任务失败或超时时系统自动处理，无需我手动干预。

**Acceptance Criteria:**

**Given** 意图正在执行中
**When** 系统持续监测
**Then** 维护 Desired（期望状态）、Current（当前状态）、Drift（差异）三个状态

**Given** 某个子任务失败或超时
**When** 事件触发 Reconciler
**Then** 系统自动重新规划和重试该子任务，无需用户手动干预
**And** 从检测到 drift 到启动调和行动延迟 <= 5s（NFR43）

**Given** 子任务成功完成
**When** 事件触发 Reconciler
**Then** 更新 Current 状态，检查下游依赖并启动可执行的后续任务

**Given** `intent/types.go` 中 IntentNode 已扩展重试和超时字段
**When** 查看 IntentNode 结构体
**Then** 包含 `RetryCount`、`MaxRetries`、`Timeout`（time.Duration）、`LastFailedAt` 字段
**And** `CanRetry()` 方法在 `RetryCount < MaxRetries` 时返回 true
**And** `IncrRetry()` 方法递增 `RetryCount` 并记录 `LastFailedAt` 时间戳
**And** 新增 `IntentRetrying` 状态常量，`RunnableNodes()` 同时接受 `pending` 和 `retrying` 状态（FR108）

**Given** `intent/types.go` 中三态模型已实现
**When** 调用 `IntentTree.InitDesired()`
**Then** 为每个节点在 `DesiredNodes` 中设置目标状态为 `IntentCompleted`
**And** `ComputeDrifts()` 扫描所有节点，仅对 `IntentFailed` 状态的节点生成 `DriftItem`（不将 pending/executing/retrying 误报为 drift）
**And** `AddDrift(item)` 添加 drift 记录，`ClearDrift(nodeID)` 清除指定节点的 drift
**And** `ActiveDrifts()` 返回当前未解决的 drift 列表（FR107）

**Given** `intent/reconciler.go` 中 `Reconciler` 已实现
**When** 调用 `Reconciler.Execute(ctx)` 执行意图树
**Then** 首先调用 `tree.InitDesired()` 初始化目标状态
**And** 使用 `reconcileEvent` channel 收集完成/失败/超时事件，事件驱动主循环消费事件并推进状态
**And** 回调函数（`ReconcilerCallbacks`）在 mutex 外调用，避免死锁（FR108）

**Given** 某个子任务在 Reconciler 执行中失败
**When** `evNodeFailed` 事件被消费
**Then** 检查 `CanRetry()`——如果可重试，调用 `IncrRetry()`，先设置 `IntentRetrying` 状态，然后重置为 `IntentPending`
**And** 清除 `node.Error`、`node.PID`、`node.Result`，添加 drift 记录（`DriftNodeFailed`）
**And** 立即通过 `spawnRunnable()` 重新调度该节点
**And** 触发 `OnNodeRetry` 和 `OnDriftDetected` 回调

**Given** 子任务重试次数达到上限
**When** Reconciler 检测到 `CanRetry()` 返回 false
**Then** 调用 `MarkFailed` 标记该节点最终失败，级联标记依赖下游为 failed
**And** 触发 `OnNodeFailed` 回调，向用户报告最终失败原因

**Given** 子任务执行超时
**When** `context.WithTimeout` 的 ctx 超时触发
**Then** 底层 `exec.CommandContext` 自动终止进程，发送 `evNodeTimeout` 事件
**And** 超时后与 `evNodeFailed` 走相同重试路径——先检查 `CanRetry()`，可重试则重调度，否则标记失败
**And** 节点超时配置：`node.Timeout > 0` 时使用节点自身超时，否则使用 `config.DefaultTimeout`（默认 5min）

**Given** 用户查询 `rnix intent status`（`cmd/rnix/intent.go`）
**When** Reconciler 正在运行
**Then** 状态信息包含当前 drift 项列表（通过 `DriftItemWire` 传递）、每个节点的 `RetryCount`/`MaxRetries`（通过 `IntentNodeWire` 传递）
**And** UI 渲染 drift 表格（`RenderDriftList`）、重试事件（`RenderIntentNodeRetry`，黄色/橙色）、超时事件（`RenderIntentNodeTimeout`，红色）

**Given** Reconciler 从检测到 drift 到启动调和行动
**When** 测量 `evNodeFailed` 事件到 `spawnRunnable()` 重新调度的时间差
**Then** 延迟 <= 5s（NFR43）
**And** 实际延迟为微秒级（Go channel 操作为纳秒级，中间仅 mutex lock + 状态重置 + spawn goroutine）
**And** 通过 `TestReconciler_Execute_NFR40_Latency` 使用 mock spawner 时间戳验证

**Given** IPC 协议已扩展 Reconciler 事件（`ipc/protocol.go`）
**When** Reconciler 运行中触发重试/超时/drift 事件
**Then** 流式发送 4 种新增 StreamEvent：`intent_node_retry`、`intent_node_timeout`、`intent_drift_detected`、`intent_drift_resolved`
**And** `IntentNodeEventPayload` 包含 `RetryAttempt`、`MaxRetries`、`DriftType` 扩展字段

## Story 19.3: 增量更新与状态查询

As a 应用开发者,
I want 在执行过程中更新意图并查看完整状态,
So that 我可以动态调整需求而不丢失已完成的工作。

**Acceptance Criteria:**

**Given** 意图正在执行中（部分子任务已完成）
**When** 用户执行 `rnix apply "加上评论功能"`
**Then** Reconciler 计算增量差异，仅执行新增/变更部分，已完成的工作不回滚

**Given** 一个活跃的意图
**When** 用户执行 `rnix intent status`
**Then** 显示意图树的当前状态：整体进度、各子意图完成度、执行中的智能体列表、待解决的 drift 项

**Given** `intent/merge.go` 中 `MergeIncremental` 函数已实现
**When** 调用 `MergeIncremental(existing, newNodes)` 进行增量合并
**Then** 返回 `MergeResult`，包含 `AddedNodes`（新增节点 ID 列表）、`ModifiedNodes`（意图变更的节点 ID 列表）、`UnchangedNodes`（未变化的节点 ID 列表）
**And** 新节点（ID 不存在于 existing.Nodes）被添加到 tree，状态设为 `IntentPending`
**And** 已有节点且 Intent 相同的保持现有状态不变（已完成的工作不回滚）
**And** 已有节点且 Intent 不同的通过 `ResetNode` 重置为 `IntentPending`（需要重新执行）
**And** 合并后更新 `DesiredNodes`，为新增节点设置目标 `IntentCompleted`（FR109）

**Given** 增量合并验证逻辑
**When** 新节点的 `DependsOn` 引用不存在的节点 ID
**Then** `MergeIncremental` 返回错误，回滚所有变更，保持原有意图树不变
**And** 对合并后的树调用 `BuildIntentDAG` + `DetectCycle` 验证无循环依赖，存在循环时同样返回错误并回滚

**Given** 增量更新引入新的子意图节点
**When** 新节点依赖已完成节点
**Then** 新节点的依赖已满足，`RunnableNodes()` 将其返回，立即可调度执行，无需重新执行已完成的上游节点

**Given** 增量更新引入新的子意图节点
**When** 新节点依赖尚未完成的节点
**Then** 新节点等待依赖完成后，由 Reconciler 事件循环自动检测并调度执行

**Given** 增量更新修改了已完成节点的意图描述
**When** Reconciler 检测到变更
**Then** `MergeIncremental` 将该节点标记为 `ModifiedNodes`，`ResetNode` 重置状态为 `IntentPending`
**And** `ResetNode` 清除 `Error`、`PID`、`Result`、`RetryCount`，但保留 `MaxRetries`、`Timeout` 配置

**Given** 用户对已执行中的意图执行增量更新
**When** 有子任务正在执行（状态为 `executing`）
**Then** 正在执行的子任务不受影响，继续运行至完成
**And** 新增/变更的节点通过 `Reconciler.InjectNodes` 在 mutex 保护下注入到意图树
**And** 注入后立即调用 `spawnRunnable(ctx)` 调度新的可执行节点，不中断现有事件循环

**Given** 增量分解返回的新节点引用了不存在的依赖
**When** Reconciler 尝试合并
**Then** 返回错误，拒绝增量更新，保持原有意图树不变

**Given** `intent/decomposer.go` 中 `DecomposeIncremental` 方法已实现
**When** 调用 `Decomposer.DecomposeIncremental(ctx, tree, newIntent, model)` 进行增量分解
**Then** 使用 `incrementalDecomposePromptTemplate` 构建 prompt，包含原始意图、当前子任务状态摘要、用户新需求
**And** LLM 返回合并后的完整子任务列表（包含已有和新增节点），由 `MergeIncremental` 处理差异计算
**And** LLM 仅负责规划，合并逻辑完全确定性（不依赖 LLM 的合并能力）

**Given** `intent/manager.go` 中 `ApplyIncremental` 方法已实现
**When** 调用 `Manager.ApplyIncremental(ctx, intentID, newIntent, model)`
**Then** 验证 intent 存在且非终态（completed/failed 拒绝更新）
**And** 调用 `DecomposeIncremental` 获取新节点列表，再调用 `MergeIncremental` 合并
**And** 如果 Reconciler 正在运行（`activeReconcilers[intentID]` 存在），调用 `Reconciler.InjectNodes` 注入新节点
**And** 返回更新后的 `IntentTree` 和 `MergeResult`

**Given** `rnix apply "新需求" --update intent-1` 命令已实现（`cmd/rnix/apply.go`）
**When** 用户执行带 `--update` / `-u` flag 的增量更新
**Then** 调用 `client.ApplyIncrementalIntent` 发送 `apply_incremental_intent` IPC 请求
**And** 服务端处理后返回 `StreamIntentIncrementalMerged` 事件，包含 `added_nodes` 和 `modified_nodes`
**And** UI 渲染合并结果（`RenderIntentMergeResult`），显示新增节点列表和修改节点列表

**Given** `rnix intent status [intent-id]` 已增强（`cmd/rnix/intent.go`）
**When** 用户查看意图状态
**Then** 显示整体进度百分比（已完成/总数）、按状态分组的节点列表（executing/pending/completed/failed/retrying）
**And** 显示执行中智能体列表（节点 ID + PID + 意图描述）和 drift 项列表（FR111）

**Given** `rnix intent list` 子命令已实现
**When** 用户执行 `rnix intent list`
**Then** 通过 `intent_list` IPC 方法获取所有意图（活跃 + 已完成）
**And** 表格显示 ID、Root Intent（截断）、State、进度、创建时间（`RenderIntentList`）
