# Epic 15: 分布式追踪与上下文分析

用户可以追踪跨多智能体系统的完整因果链，定位性能瓶颈和错误根因，分析每个智能体的上下文使用效率。

## Story 15.1: Trace ID 生成与 Span 记录

As a 平台构建者,
I want 系统自动为 Compose 编排生成 Trace ID 并在智能体间传播，每个智能体记录 Span 数据,
So that 我可以获得跨进程的完整因果链数据。

**Acceptance Criteria:**

**Given** 一个 Compose 编排启动
**When** `compose up` 执行
**Then** 系统生成唯一 Trace ID，Compose 内所有 Spawn 的进程自动继承该 Trace ID 并生成独立 SpanID

**Given** 智能体 A 通过 IPC 向智能体 B 发送消息
**When** Send/Recv 执行
**Then** Trace ID 作为消息元数据自动携带，B 的 Span 记录 parent 指向 A 的 Span

**Given** 智能体执行过程中
**When** 每个 syscall 被调用
**Then** 系统在 Span 中记录起止时间、syscall 序列和 token 消耗
**And** Trace/Span 传播不增加 IPC 延迟超过 10ms（NFR33）

## Story 15.2: 分布式追踪视图

As a 平台构建者,
I want 通过 `rnix trace <trace-id>` 查看完整的分布式追踪视图,
So that 我可以一目了然地看到所有智能体的时序关系和依赖链路。

**Acceptance Criteria:**

**Given** 一个已完成的 Compose 编排的 Trace ID
**When** 用户执行 `rnix trace <trace-id>`
**Then** 系统展示所有参与智能体的 Span 树状视图，包含时序关系、持续时间和 token 消耗

**Given** 追踪视图中包含多个 Span
**When** 用户查看视图
**Then** Span 之间的 parent-child 关系清晰展示，可展开/折叠

## Story 15.3: Trace Blame 根因定位

As a 平台构建者,
I want 通过 `rnix trace blame <trace-id>` 自动分析追踪数据定位关键路径节点,
So that 我可以快速找到耗时最长、消耗最大或产生错误的瓶颈。

**Acceptance Criteria:**

**Given** 一个有效的 Trace ID
**When** 用户执行 `rnix trace blame <trace-id>`
**Then** 系统自动分析并高亮标记：耗时最长的节点、token 消耗最大的节点、产生错误的关键路径

**Given** blame 分析结果
**When** 结果中包含错误节点
**Then** 显示错误传播链路（从根因到最终失败的完整路径）

## Story 15.4: 上下文使用分析

As a 平台构建者,
I want 通过 `rnix ctx-profile <pid>` 查看智能体上下文的使用分析，识别最大消费者,
So that 我可以优化 token 使用效率，减少不必要的上下文消耗。

**Acceptance Criteria:**

**Given** 一个 Running 或 Zombie 状态的智能体
**When** 用户执行 `rnix ctx-profile <pid>`
**Then** 系统将上下文分为活跃（当前推理引用）、温（近期使用）、冷（未引用）、泄漏（已无用未释放）四类展示
**And** 分析结果延迟 <= 1s（NFR34）

**Given** 上下文分析结果
**When** 结果中存在大消费者
**Then** 系统识别哪个 Skill 或工具结果占用最多 token，并给出具体优化建议

## Story 15.5: 上下文增长预测与告警

As a 平台构建者,
I want 系统预测当前上下文的增长趋势并在预计耗尽前告警,
So that 我可以提前采取措施避免 token 预算耗尽导致推理中断。

**Acceptance Criteria:**

**Given** 智能体正在执行且有 token 预算
**When** 系统检测到上下文增长趋势
**Then** 基于历史增长速率预测何时耗尽预算

**Given** 预测结果显示即将耗尽（剩余 < 20%）
**When** 告警条件触发
**Then** 系统发出告警，显示当前消耗/总额、百分比和预估剩余步数
