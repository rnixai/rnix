# Epic 21: Token 经济、声誉与 Skill 协同

系统智能管理 token 预算，通过合约 SLA 约束质量，基于声誉择优选择，自动检测 Skill 协同效应。

## Story 21.1: Token 预算池与分配调度

As a 应用开发者,
I want 为 Compose 编排分配总 token 预算池，系统按优先级智能分配配额,
So that 关键任务获得更多资源，低优先级任务不会浪费预算。

**Acceptance Criteria:**

**Given** rnix-compose.yaml 定义了总 token 预算和各智能体优先级
**When** `compose up` 执行
**Then** 系统创建预算池并按优先级分配初始配额

**Given** 多个智能体竞争有限 token 预算
**When** 高优先级智能体需要更多配额
**Then** 系统通过价格信号机制调度——高优先级获得更多配额，低优先级被降级或排队
**And** 预算分配决策延迟 <= 100ms（NFR43）

## Story 21.2: 合约 SLA 与评估

As a 应用开发者,
I want 通过合约 SLA 约束智能体协作的输入格式、输出质量、token 消耗和超时,
So that 智能体之间的协作有明确的质量保证。

**Acceptance Criteria:**

**Given** agent.yaml 或 compose.yaml 中定义了合约 SLA
**When** 智能体完成执行
**Then** 系统自动评估是否满足 SLA（输出质量、token 消耗、响应时间）

**Given** SLA 评估完成
**When** 结果生成
**Then** 评估结果记录到该 Agent 模板的声誉分数，显示各项通过/失败状态

## Story 21.3: 声誉系统与自动选择

As a 应用开发者,
I want 系统跟踪 Agent 模板的历史表现并在自动分配时择优选择,
So that 表现好的 Agent 模板被更多使用，系统质量持续优化。

**Acceptance Criteria:**

**Given** Agent 模板有历史执行记录
**When** 用户执行 `rnix reputation [agent]`
**Then** 显示声誉分数、成功率、平均 token 效率、SLA 达标率和近期趋势

**Given** Reconciler 或系统需要自动选择 Agent 模板
**When** 多个候选模板可用
**Then** 系统优先选择声誉高的模板

## Story 21.4: Skill Synergy 声明与自动检测

As a 平台构建者,
I want SKILL.md 可以声明 synergy 字段，系统在加载多个 Skill 时自动检测并激活协同效应,
So that Skill 组合能产生 1+1>2 的涌现能力。

**Acceptance Criteria:**

**Given** SKILL.md 中声明了 `synergy` 字段，指定与特定 Skill 的协同指令
**When** 智能体同时加载这两个 Skill
**Then** 系统自动检测 synergy 组合，将涌现指令追加到 system prompt

**Given** 多个 synergy 组合同时命中
**When** 智能体加载 N 个有交叉 synergy 的 Skill
**Then** 所有命中的 synergy 指令都被追加
**And** 组合检测开销 <= 100ms（NFR46）

## Story 21.5: Skill 组合矩阵

As a 平台构建者,
I want 系统维护 Skill 组合矩阵记录历史表现，可通过命令查看有效组合,
So that 我可以了解哪些 Skill 组合效果最好。

**Acceptance Criteria:**

**Given** 系统已积累 Skill 组合的历史执行数据
**When** 用户执行 `rnix synergy list`
**Then** 展示已知的有效 Skill 组合，包含成功率对比（组合 vs 单独）、token 效率提升和使用频次

**Given** 组合矩阵数据
**When** 存在显著优于单 Skill 的组合
**Then** 标记为推荐组合
