# Epic 20: 自主智能体（OODA + 干细胞分化）

智能体通过 OODA 循环自主执行任务，通用基底智能体根据意图自动匹配 Skill 完成分化。

## Story 20.1: OODA 循环核心实现

As a 平台构建者,
I want 智能体可以在推理循环中执行 OODA 四阶段（感知-判断-决策-行动）,
So that 智能体能够自主感知环境变化并做出适应性决策。

**Acceptance Criteria:**

**Given** 一个 OODA 模式的智能体
**When** 进入 Observe 阶段
**Then** 智能体通过 VFS 读取环境信息（/proc/ 状态、其他进程输出、文件变更）

**Given** Observe 阶段完成
**When** 进入 Orient 阶段
**Then** 智能体评估感知数据与目标的偏差

**Given** Orient 阶段完成
**When** 进入 Decide 阶段
**Then** 智能体自主选择下一步行动（调用工具、spawn 子进程、请求协作或调整计划）

**Given** Decide 阶段完成
**When** 进入 Act 阶段
**Then** 执行决策并将结果反馈到下一轮 Observe 形成闭环
**And** 单轮 OODA 循环框架开销 <= 200ms（NFR41）

## Story 20.2: OODA 配置与任务式指挥

As a 平台构建者,
I want 通过 agent.yaml 启用 OODA 模式，并让 OODA 智能体自主 spawn 子智能体,
So that 我可以构建自主决策的智能体层级。

**Acceptance Criteria:**

**Given** agent.yaml 中声明 `reasoning: ooda`
**When** spawn 该 Agent
**Then** 智能体使用 OODA 循环替代默认线性推理模式

**Given** OODA 模式的智能体在 Decide 阶段
**When** 智能体决定需要子任务
**Then** 智能体自主 spawn 子智能体，只下达意图不规定执行细节（任务式指挥）

**Given** OODA 智能体 spawn 的子智能体
**When** 子智能体的 agent.yaml 也声明 `reasoning: ooda`
**Then** 子智能体内部同样以 OODA 循环自主执行

## Story 20.3: Stem Agent 与自动分化

As a 平台构建者,
I want 系统提供通用基底智能体，根据接收到的意图自动匹配和加载最相关的 Skill 组合,
So that 我不需要预先指定 Agent 模板，系统能自动选择最佳能力组合。

**Acceptance Criteria:**

**Given** 用户执行 `crux "分析代码" --agent=stem`
**When** Stem Agent 接收意图
**Then** 系统分析意图，自动匹配最相关的 Skill 组合（如 code-analysis + git-tools），加载后完成分化

**Given** Stem Agent 分化过程
**When** 匹配到候选 Skill
**Then** 实时输出分化过程：`[agent/N] differentiating: loading skills [...]`
**And** Skill 匹配和加载 <= 3s（NFR42）

## Story 20.4: 渐进式特化与分化记忆

As a 平台构建者,
I want 分化后的智能体可以在执行过程中动态加载额外 Skill，并记忆分化路径,
So that 智能体能力可以按需扩展，且相似任务可快速复用上次分化结果。

**Acceptance Criteria:**

**Given** 一个已分化的智能体在执行任务
**When** 智能体检测到能力缺口（需要当前 Skill 未覆盖的工具）
**Then** 动态加载额外 Skill 进一步特化，不中断执行

**Given** 智能体完成分化和执行
**When** 分化路径被记录
**Then** 系统保存"表观遗传"记忆：哪些 Skill 被加载、加载顺序、触发意图

**Given** 下次接收到相似意图
**When** Stem Agent 开始分化
**Then** 系统优先复用上次记录的分化路径，加速分化过程

## Story 20.5: 分化谱系图

As a 平台构建者,
I want 通过 `crux lineage <pid>` 查看从基底到特化体的完整分化路径,
So that 我可以理解智能体是如何获得当前能力的。

**Acceptance Criteria:**

**Given** 一个经过分化的智能体
**When** 用户执行 `crux lineage <pid>`
**Then** 展示从 Stem Agent 到当前特化体的完整路径，包含每次分化加载的 Skill 和触发的意图

**Given** 谱系图中包含多次渐进式特化
**When** 展示谱系
**Then** 每次 Skill 加载标注时间点和触发原因
