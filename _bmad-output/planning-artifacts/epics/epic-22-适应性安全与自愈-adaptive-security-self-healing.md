# Epic 22: 适应性安全与自愈

系统通过 Immune Daemon 监控行为、检测异常、拦截威胁，并在故障时自动迁移任务到相似能力的智能体。

## Story 22.1: Immune Daemon 与行为基线

As a 平台构建者,
I want 系统运行 Immune Daemon 持续监控智能体行为并建立正常行为基线,
So that 系统能识别什么是"正常"行为，为异常检测提供依据。

**Acceptance Criteria:**

**Given** Rnix daemon 启动
**When** Immune Daemon 开始运行
**Then** 持续监控所有智能体的行为模式（syscall 频率、资源访问模式、token 消耗速率）
**And** CPU 开销 <= 3%（10 并发进程场景，NFR44）

**Given** Agent 模板有足够的历史执行数据
**When** 系统分析历史数据
**Then** 建立该模板的 Normal Profile（行为基线），包含各指标的正常范围

## Story 22.2: 异常检测与威胁记忆

As a 平台构建者,
I want 系统在智能体行为偏离基线时自动告警和挂起，并记忆已知威胁模式,
So that 异常行为被及时拦截，已知威胁不需要重新检测。

**Acceptance Criteria:**

**Given** 智能体行为偏离基线超过阈值（如文件写入频率 5 倍于正常值）
**When** Immune Daemon 检测到异常
**Then** 触发告警并自动挂起（suspend）该进程，显示异常类型和偏离程度

**Given** 一个已识别的异常行为模式
**When** 系统记录到威胁记忆库（Antibody Memory）
**Then** 后续相同模式出现时立即拦截，无需重新检测

**Given** 进程被挂起
**When** 用户审查后
**Then** 可通过 `rnix immune resume <pid>` 恢复执行或 kill 终止

## Story 22.3: 安全状态管理

As a 平台构建者,
I want 通过 `rnix immune status` 查看完整的安全监控状态,
So that 我可以全面了解系统的安全态势。

**Acceptance Criteria:**

**Given** Immune Daemon 运行中
**When** 用户执行 `rnix immune status`
**Then** 显示：daemon 状态和运行时间、当前告警列表、已挂起进程、威胁记忆库条目数

**Given** 存在已挂起的进程
**When** 状态输出中显示挂起项
**Then** 每项显示 PID、异常原因和可用操作（resume / kill）

## Story 22.4: 能力迁移与相似度矩阵

As a 平台构建者,
I want 系统在智能体异常退出且 Supervisor 重启失败时，自动将任务迁移到相似能力的智能体,
So that 任务不会因单个智能体故障而丢失。

**Acceptance Criteria:**

**Given** Supervisor 重启某智能体失败（超过最大重试次数）
**When** 系统触发能力迁移
**Then** 基于能力相似度矩阵选择最佳替代智能体，将未完成的任务上下文迁移过去继续执行
**And** 能力迁移 <= 10s（NFR45）

**Given** 系统有多个 Agent 模板
**When** 系统计算能力相似度
**Then** 基于 Skill 重叠度和历史协作记录维护能力相似度矩阵

## Story 22.5: 协作拓扑与强化路径

As a 平台构建者,
I want 系统自动识别高频协作路径，并通过 `rnix topology` 查看协作拓扑图,
So that 我可以了解智能体间的协作模式并优化编排。

**Acceptance Criteria:**

**Given** 系统有智能体间的协作历史
**When** 系统分析协作数据
**Then** 高频协作路径（A 频繁 spawn B、B 频繁向 C 发送消息）被自动识别和记录为强化路径

**Given** 存在协作历史数据
**When** 用户执行 `rnix topology`
**Then** 展示智能体协作拓扑图：节点=智能体（标注名称和声誉分数），边=协作关系（宽度=频率）

**Given** 后续 Compose 编排生成依赖关系
**When** 存在强化路径
**Then** 系统优先建议已验证的高频协作组合
