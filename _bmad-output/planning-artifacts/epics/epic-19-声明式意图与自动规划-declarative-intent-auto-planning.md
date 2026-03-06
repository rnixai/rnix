# Epic 19: 声明式意图与自动规划

用户只需声明期望状态，系统自动分解子任务、分配智能体，Reconciler 持续调和差异。

## Story 19.1: 意图声明与任务分解

As a 应用开发者,
I want 通过 `crux apply "高层意图"` 声明期望状态，系统自动分解为子意图树,
So that 我可以用自然语言描述目标而不需要手动编排。

**Acceptance Criteria:**

**Given** 用户执行 `crux apply "我要一个完整的博客系统"`
**When** 系统接收意图
**Then** 系统将高层意图递归分解为子意图树（Intent Tree），每个子意图对应一个或多个智能体进程

**Given** 意图分解完成
**When** 系统展示分解结果
**Then** 显示子任务列表、依赖关系和执行计划，等待用户确认后开始执行

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
**And** 从检测到 drift 到启动调和行动延迟 <= 5s（NFR40）

**Given** 子任务成功完成
**When** 事件触发 Reconciler
**Then** 更新 Current 状态，检查下游依赖并启动可执行的后续任务

## Story 19.3: 增量更新与状态查询

As a 应用开发者,
I want 在执行过程中更新意图并查看完整状态,
So that 我可以动态调整需求而不丢失已完成的工作。

**Acceptance Criteria:**

**Given** 意图正在执行中（部分子任务已完成）
**When** 用户执行 `crux apply "加上评论功能"`
**Then** Reconciler 计算增量差异，仅执行新增/变更部分，已完成的工作不回滚

**Given** 一个活跃的意图
**When** 用户执行 `crux intent status`
**Then** 显示意图树的当前状态：整体进度、各子意图完成度、执行中的智能体列表、待解决的 drift 项
