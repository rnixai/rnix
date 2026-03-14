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
**And** CPU 开销 <= 3%（10 并发进程场景，NFR47）

**Given** Agent 模板有足够的历史执行数据
**When** 系统分析历史数据
**Then** 建立该模板的 Normal Profile（行为基线），包含各指标的正常范围

**Given** `kernel/immune.go` 中的 `ImmuneDaemon` 已实现
**When** 智能体进程通过 Spawn 创建并运行
**Then** `OnProcessStart` 创建该进程的 `BehaviorCollector` 实例
**And** 每个 `SyscallEvent` 通过 `OnSyscallEvent` 传递给对应的 BehaviorCollector 进行行为聚合
**And** 进程退出时 `OnProcessExit` 生成 `BehaviorSample` 并持久化到 `$PROJECT/.rnix/immune/{agentTemplate}.jsonl`

**Given** Agent 模板历史执行数据不足 5 条（`MinSamplesForProfile = 5`）
**When** 系统尝试建立 NormalProfile
**Then** `ComputeProfile` 返回 nil，不生成行为基线
**And** 后续该模板的异常检测被跳过（无 Profile 不检测）

**Given** NormalProfile 已保存到 `$PROJECT/.rnix/immune/profiles/{agentTemplate}-profile.json`
**When** daemon 重启后 `ImmuneDaemon.Start()` 调用 `ImmuneStore.LoadAllProfiles()`
**Then** 自动加载已有的 NormalProfile 数据到内存缓存
**And** 新的执行数据持续增量更新 Profile

**Given** 系统无运行中的智能体进程
**When** ImmuneDaemon 处于空闲状态（事件驱动模式，无主动轮询 goroutine、无 ticker）
**Then** CPU 开销接近 0%（满足 NFR47 <= 3% 要求）
**And** 内存占用仅为 NormalProfile 缓存

**Given** 现有 `kernel/kernel.go` 的 `KernelImpl` 结构体
**When** 新增 `immuneDaemon *ImmuneDaemon` 字段及 `SetImmuneDaemon` setter
**Then** `immuneDaemon` 为 nil 时所有钩子调用跳过，不影响现有进程管理、Compose、IPC 等功能
**And** 不修改已有方法签名，通过 `go test -race` 无数据竞争

**Given** `ImmuneDaemon` 功能通过 IPC 暴露
**When** IPC 协议新增 `MethodImmuneStatus`（`ipc/protocol.go`）
**Then** `ipc/client.go` 提供 `ImmuneStatus()` 方法
**And** `cmd/rnix/immune.go` 提供 `rnix immune status` CLI 命令，表格输出 Profile 列表（Agent 模板名、样本数、平均 token 速率、平均时长、最后更新）
**And** `--json` flag 输出 `JSONResponse[ImmuneStatusResponse]` 格式

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
**Then** 可通过 `rnix immune resume <pid>` 恢复执行或 `rnix kill <pid>` 终止

**Given** `kernel/immune.go` 中的 `AnomalyDetector` 已实现，阈值 `DefaultDeviationThreshold = 3.0`（3 倍标准差，99.7% 置信区间外）
**When** `OnSyscallEvent` 中 BehaviorCollector 累积计数超过 NormalProfile 的 `mean + threshold * stddev`
**Then** 创建 `AnomalyAlert`（含 PID、AgentTemplate、AnomalyType、Detail、Deviation 偏离倍数）
**And** 创建 `ThreatSignature`（三元组标识：`agent_template + anomaly_type + metric`）并持久化到 `$PROJECT/.rnix/immune/threats.jsonl`
**And** 通过注入的 `suspendFn` 回调发送 `SIGPAUSE` 信号挂起进程（复用 Story 6.4 的信号系统）

**Given** `AnomalyDetector` 检测三种异常类型
**When** syscall 调用频率异常（`AnomalySyscallFreq`）、token 消耗速率异常（`AnomalyTokenRate`）或未预期的设备访问（`AnomalyDeviceAccess`）
**Then** 告警 `Detail` 字段包含人类可读描述（如 "Open 频率 12.0 是基线 2.4 的 5.0 倍"）
**And** `Deviation` 字段记录偏离倍数（`currentValue / mean`）

**Given** NormalProfile 不存在（新模板或样本不足）
**When** 智能体行为事件到达
**Then** `AnomalyDetector` 返回 nil（无法检测），不触发误报
**And** `suspendFn` 为 nil 时检测到异常仅记录告警，不执行挂起（降级模式）

**Given** 威胁记忆库（`threats.jsonl`）有已知的 `ThreatSignature` 记录
**When** 新事件到达且 `MatchThreat` 匹配成功（相同 agent_template + anomaly_type + metric）
**Then** 立即拦截并挂起进程，跳过统计检测（快速路径）
**And** daemon 重启后 `Start()` 自动从磁盘加载已有威胁签名

**Given** 进程被挂起后用户执行 `rnix immune resume <pid>`
**When** IPC 发送 `MethodImmuneResume` 请求（`ipc/protocol.go`）
**Then** server handler 调用 `kernel.Kill(pid, SIGRESUME)` 恢复进程
**And** `ImmuneDaemon.ClearAlert(pid)` 清除该 PID 的告警记录

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

**Given** `kernel/immune.go` 中 `ImmuneDaemon` 新增 `startedAt time.Time` 字段
**When** `Start()` 记录启动时间，用户执行 `rnix immune status`
**Then** 输出包含 "Immune Daemon: running (uptime: 2h15m)" 格式的运行时间
**And** `Uptime()` 方法返回 `time.Since(d.startedAt)`，未 Start 或 nil daemon 返回 0

**Given** `ImmuneStatusResponse`（`ipc/protocol.go`）扩展三个新字段
**When** IPC server 填充响应
**Then** `UptimeMs` 包含 daemon 运行时长（毫秒）
**And** `SuspendedPIDs` 包含所有被挂起进程的 PID 列表（从 alerts map 提取）
**And** `SecurityStatus` 为 `"ok"`（无告警无挂起）或 `"warning"`（有告警或有挂起）

**Given** 无活跃告警且无挂起进程
**When** CLI 输出安全态势摘要行
**Then** 显示 "Security: OK"
**And** 不显示 ALERTS 和 SUSPENDED PROCESSES 段落

**Given** 存在 2 条活跃告警和 1 个挂起进程
**When** CLI 输出安全态势摘要行
**Then** 显示 "Security: 2 alerts, 1 suspended"
**And** ALERTS 段落每项显示 PID、Agent 模板、异常类型、偏离程度、触发时间和可用操作（`rnix immune resume <pid> | rnix kill <pid>`）
**And** SUSPENDED PROCESSES 段落显示挂起的 PID 列表及对应异常类型

**Given** 用户执行 `rnix immune status --json`
**When** CLI 以 JSON 模式输出
**Then** 输出 `JSONResponse[ImmuneStatusResponse]` 格式，包含所有字段：`running`、`uptime_ms`、`profile_count`、`profiles`、`active_pids`、`suspended_pids`、`alerts`（`AlertWire` 数组）、`threat_count`、`security_status`

**Given** `formatUptime(ms int64)` 辅助函数（`cmd/rnix/immune.go`）
**When** 格式化不同范围的时间
**Then** < 60s 显示 "42s"、< 60m 显示 "5m30s"、>= 60m 显示 "2h15m"

## Story 22.4: 能力迁移与相似度矩阵

As a 平台构建者,
I want 系统在智能体异常退出且 Supervisor 重启失败时，自动将任务迁移到相似能力的智能体,
So that 任务不会因单个智能体故障而丢失。

**Acceptance Criteria:**

**Given** Supervisor 重启某智能体失败（超过最大重试次数）
**When** 系统触发能力迁移
**Then** 基于能力相似度矩阵选择最佳替代智能体，将未完成的任务上下文迁移过去继续执行
**And** 能力迁移 <= 10s（NFR48）

**Given** 系统有多个 Agent 模板
**When** 系统计算能力相似度
**Then** 基于 Skill 重叠度和历史协作记录维护能力相似度矩阵

**Given** `kernel/immune.go` 中 `SimilarityMatrix` 和 `CapabilitySimilarity` 已实现
**When** 调用 `Compute(agents map[string][]string, coopHistory map[string]map[string]int)`
**Then** 使用 Jaccard 系数（`|A ∩ B| / |A ∪ B|`）计算 Skill 重叠度（`SkillScore` 0.0~1.0）
**And** 协作历史归一化为 `CoopScore`（`min(coopCount, maxCoop) / maxCoop`，0.0~1.0）
**And** 综合分 `Score = 0.7 * SkillScore + 0.3 * CoopScore`
**And** 自身相似度不存储（跳过 agentA == agentB），空 Skill 集合返回 0.0

**Given** `ImmuneDaemon` 集成 `SimilarityMatrix`
**When** 调用 `GetSimilarAgents(agentName string, minScore float64)`
**Then** 返回相似度 >= minScore 的 Agent 列表，按 `Score` 降序排列
**And** `RecordCooperation(agentA, agentB string)` 在 Spawn 时记录双向协作计数

**Given** Supervisor 的 `handleChildExit` 中 `exceedsRestartLimit()` 为 true
**When** 系统尝试能力迁移（`ImmuneDaemon.AttemptMigration`）
**Then** 从 `SimilarityMatrix` 获取 `GetSimilarAgents(agentTemplate, MinMigrationSimilarity=0.3)` 候选列表
**And** 结合 `ReputationStore.GetSummary()` 对候选排序（similarity * 0.6 + reputation * 0.4）
**And** 通过注入的 `MigrateFunc` 在最佳候选上 Spawn 新进程，注入原始 Intent + Context 历史消息
**And** 迁移成功后 Reap 原进程防止僵尸进程，跳过 `shutdownAll`

**Given** 无满足阈值的替代 Agent（所有候选相似度 < `MinMigrationSimilarity = 0.3`）
**When** 能力迁移尝试
**Then** `MigrationResult.Success` 为 false，`Reason` 记录失败原因
**And** Supervisor 继续执行原有的 `shutdownAll + finishProcess` 关闭流程

**Given** 能力迁移通过 IPC 暴露查询接口（`MethodSimilarityQuery`，`ipc/protocol.go`）
**When** 用户执行 `rnix immune similarity [agent-name]`（`cmd/rnix/immune.go`）
**Then** 文本输出显示 Agent 名、相似度分数、Skill 重叠列表
**And** `--json` flag 输出 `SimilarityQueryResponse` 格式（`agent` + `similarities` 数组）

**Given** 能力迁移执行过程
**When** 测量迁移总时间（选择替代 + Spawn 新进程 + 上下文注入）
**Then** 总迁移时间 <= 10s（NFR48）

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
**Then** 展示智能体协作拓扑图：节点=智能体（标注名称和声誉分数），边=协作关系（标注频率）

**Given** 后续 Compose 编排生成依赖关系
**When** 存在强化路径
**Then** 系统优先建议已验证的高频协作组合

**Given** `kernel/immune.go` 中 `ImmuneDaemon` 扩展 `coopRecords map[string]map[string]*CoopRecord`
**When** 调用 `RecordCooperationTyped(agentA, agentB, coopType)` 记录协作事件
**Then** 按 `coopType`（`"spawn"` 或 `"msg"`）分别累加 `SpawnCount` 和 `MsgCount`
**And** 同时调用 `RecordCooperation()` 保持总计数向后兼容（相似度矩阵继续使用总计数）

**Given** 协作历史中某协作对的 `Total`（SpawnCount + MsgCount）>= `DefaultReinforcementThreshold`（默认 5 次）
**When** 系统通过 `GetTopology()` 构建拓扑图
**Then** 该协作对的 `CooperationEdge.Reinforced` 标记为 true
**And** `GetReinforcedPaths()` 返回所有强化路径，按 `Total` 降序排列

**Given** 协作拓扑数据通过 `GetTopology()` 生成 `CollaborationTopology`
**When** 构建 `TopologyNode` 列表
**Then** 每个节点包含 Agent 名称、声誉分数（通过 `ReputationStore.GetSummary()` 查询，失败时默认 0.0）、连接数
**And** `Edges` 列表包含所有 `CooperationEdge`（From、To、SpawnCount、MsgCount、Total、Reinforced）

**Given** 用户执行 `rnix topology`（`cmd/rnix/topology.go`，顶级命令，与 `rnix ps` 平级）
**When** CLI 以文本模式输出
**Then** 显示 NODES 表格（Agent 名、声誉分数、连接数）和 EDGES 表格（From、To、Spawn、Msg、Total、强化标记 `*`）
**And** 若有强化路径，额外显示 REINFORCED PATHS 段落（如 "code-analyst -> code-reviewer (11 interactions)"）

**Given** 用户执行 `rnix topology --json`
**When** CLI 以 JSON 模式输出
**Then** 输出 `JSONResponse[TopologyQueryResponse]` 格式，包含 `nodes`、`edges`、`reinforced_paths` 三个数组

**Given** 无协作历史数据
**When** 用户执行 `rnix topology`
**Then** 输出提示信息（如 "No collaboration data available."）
**And** 不显示空表格
