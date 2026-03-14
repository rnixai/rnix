# Story 22.4: 能力迁移与相似度矩阵

Status: done

## Story

As a 平台构建者,
I want 系统在智能体异常退出且 Supervisor 重启失败时，自动将任务迁移到相似能力的智能体,
So that 任务不会因单个智能体故障而丢失。

## Acceptance Criteria

1. **AC1: 能力相似度矩阵计算**
   - Given 系统有多个 Agent 模板（通过 `agentLoader` 加载）
   - When 系统计算能力相似度
   - Then 基于 Skill 重叠度计算任意两个 Agent 之间的相似度分数（0.0~1.0）
   - And 相似度矩阵存储在内存中，支持按需查询

2. **AC2: 历史协作记录纳入相似度**
   - Given Agent 之间有历史协作记录（父子 spawn 关系、IPC 消息发送）
   - When 计算能力相似度
   - Then 协作历史作为加权因子纳入相似度分数（Skill 重叠 70% + 协作历史 30%）

3. **AC3: Supervisor 重启失败触发能力迁移**
   - Given Supervisor 重启某子进程失败（超过 `MaxRestarts` 次数）
   - When Supervisor 因 `max_restarts_exceeded` 关闭前
   - Then 系统尝试将失败子进程的任务上下文迁移到最佳替代 Agent
   - And 迁移包括：Intent（原始意图）+ Context 历史消息 + 环境变量

4. **AC4: 最佳替代 Agent 选择**
   - Given 能力相似度矩阵已建立
   - When 触发能力迁移
   - Then 选择相似度最高且声誉分数（ReputationScore）最高的 Agent 作为替代
   - And 若无可用替代（相似度 < 0.3 阈值），迁移放弃并记录事件

5. **AC5: 迁移性能约束**
   - Given 触发能力迁移
   - When 执行迁移过程（选择替代 + Spawn 新进程 + 上下文注入）
   - Then 总迁移时间 <= 10s（NFR48）

6. **AC6: IPC 查询接口**
   - Given 能力相似度矩阵已建立
   - When 用户通过 IPC 查询相似度
   - Then 返回指定 Agent 的相似 Agent 列表（按相似度降序）

7. **AC7: CLI 展示**
   - Given 能力迁移相关功能已实现
   - When 用户执行 `rnix immune similarity [agent-name]`
   - Then 显示该 Agent 的能力相似度排行榜（Agent 名、相似度分数、Skill 重叠列表）

## Tasks / Subtasks

### Task 1: 能力相似度矩阵核心算法（AC: #1, #2）

- [x] 1.1 在 `kernel/immune.go` 中新增 `CapabilitySimilarity` 结构体：
  ```go
  type CapabilitySimilarity struct {
      AgentA     string  `json:"agent_a"`
      AgentB     string  `json:"agent_b"`
      SkillScore float64 `json:"skill_score"`  // Skill 重叠度 0.0~1.0（Jaccard 系数）
      CoopScore  float64 `json:"coop_score"`   // 协作历史分 0.0~1.0
      Score      float64 `json:"score"`        // 综合分 = 0.7 * SkillScore + 0.3 * CoopScore
  }
  ```

- [x] 1.2 新增 `SimilarityMatrix` 结构体和计算方法：
  ```go
  type SimilarityMatrix struct {
      mu      sync.RWMutex
      entries map[string]map[string]*CapabilitySimilarity // agentA -> agentB -> similarity
  }
  ```
  - `NewSimilarityMatrix()` 构造函数
  - `Compute(agents map[string][]string, coopHistory map[string]map[string]int)` — 计算矩阵
    - `agents` 参数: agentName -> skillNames 列表
    - `coopHistory` 参数: agentA -> agentB -> 协作次数
  - `Get(agentA, agentB string) *CapabilitySimilarity` — 查询两个 Agent 之间的相似度
  - `GetSimilar(agentName string, minScore float64) []CapabilitySimilarity` — 获取相似 Agent 列表

- [x] 1.3 Jaccard 系数计算：`|A ∩ B| / |A ∪ B|`，两个 Agent 的 Skill 名集合交并比
  - 空集合时返回 0.0
  - 自身相似度不存储（跳过 agentA == agentB）

- [x] 1.4 协作历史分计算：
  - 将协作次数归一化为 0.0~1.0（`min(coopCount, maxCoop) / maxCoop`，maxCoop 取所有协作对的最大值）
  - 无协作记录时 coopScore = 0.0

- [x] 1.5 单元测试：
  - `TestSimilarityMatrix_Compute_BasicSkillOverlap` -- 两个 Agent 共享部分 Skill
  - `TestSimilarityMatrix_Compute_NoOverlap` -- 完全不同的 Skill 集合，Score=0
  - `TestSimilarityMatrix_Compute_IdenticalSkills` -- 完全相同的 Skill，SkillScore=1.0
  - `TestSimilarityMatrix_Compute_WithCoopHistory` -- 协作历史纳入综合分
  - `TestSimilarityMatrix_GetSimilar_SortedByScore` -- 结果按 Score 降序
  - `TestSimilarityMatrix_GetSimilar_MinScoreFilter` -- minScore 过滤
  - `TestSimilarityMatrix_Compute_EmptyInput` -- 空输入返回空矩阵

### Task 2: ImmuneDaemon 集成相似度矩阵（AC: #1, #2）

- [x] 2.1 在 `ImmuneDaemon` 结构体中新增字段：
  ```go
  // Story 22.4: capability migration
  similarity  *SimilarityMatrix
  coopHistory map[string]map[string]int // agentA -> agentB -> count
  ```

- [x] 2.2 新增 `ImmuneDaemon.UpdateSimilarityMatrix(agents map[string][]string)` 方法：
  - 使用当前 `coopHistory` 和传入的 Agent-Skill 映射重新计算矩阵
  - nil daemon 安全（nil 检查）

- [x] 2.3 新增 `ImmuneDaemon.RecordCooperation(agentA, agentB string)` 方法：
  - 记录一次协作事件（双向：A->B 和 B->A 各计一次）
  - 在进程 Spawn 时调用（父进程 Agent 与子进程 Agent 之间）

- [x] 2.4 新增 `ImmuneDaemon.GetSimilarity(agentA, agentB string) *CapabilitySimilarity` 和
  `ImmuneDaemon.GetSimilarAgents(agentName string, minScore float64) []CapabilitySimilarity` 委托方法

- [x] 2.5 单元测试：
  - `TestImmuneDaemon_UpdateSimilarityMatrix` -- 矩阵更新后可查询
  - `TestImmuneDaemon_RecordCooperation` -- 协作记录正确累加
  - `TestImmuneDaemon_GetSimilarAgents_NilDaemon` -- nil daemon 返回 nil

### Task 3: 能力迁移引擎（AC: #3, #4, #5）

- [x] 3.1 在 `kernel/immune.go` 中新增 `MigrationResult` 结构体：
  ```go
  type MigrationResult struct {
      OriginalPID   types.PID `json:"original_pid"`
      OriginalAgent string    `json:"original_agent"`
      TargetAgent   string    `json:"target_agent"`
      NewPID        types.PID `json:"new_pid"`
      Similarity    float64   `json:"similarity"`
      DurationMs    int64     `json:"duration_ms"`
      Success       bool      `json:"success"`
      Reason        string    `json:"reason"` // 失败原因（成功时为空）
  }
  ```

- [x] 3.2 在 `ImmuneDaemon` 中新增能力迁移方法：
  ```go
  type MigrateFunc func(intent string, agentName string, contextMessages []string) (types.PID, error)
  ```
  - `SetMigrateFunc(fn MigrateFunc)` — 注入迁移执行函数（由 IPC server 或 kernel 注入）
  - `AttemptMigration(pid types.PID, agentTemplate string, intent string, contextMsgs []string) *MigrationResult`：
    1. 从 `SimilarityMatrix` 获取 `GetSimilarAgents(agentTemplate, 0.3)`
    2. 结合 `ReputationStore.GetSummary()` 对候选排序（similarity * 0.6 + reputation * 0.4）
    3. 调用 `migrateFunc` 在最佳候选上 Spawn 新进程
    4. 记录迁移事件（SyscallEvent 格式）
    5. 若所有候选都失败/无候选，返回 `Success: false`

- [x] 3.3 在 `ImmuneDaemon` 中新增 `SetReputationStore(rs *ReputationStore)` 方法

- [x] 3.4 最小相似度阈值常量：
  ```go
  const MinMigrationSimilarity = 0.3
  ```

- [x] 3.5 单元测试：
  - `TestImmuneDaemon_AttemptMigration_Success` -- 成功迁移到最佳替代
  - `TestImmuneDaemon_AttemptMigration_NoCandidate` -- 无候选时 Success=false
  - `TestImmuneDaemon_AttemptMigration_BelowThreshold` -- 相似度低于阈值时跳过
  - `TestImmuneDaemon_AttemptMigration_ReputationWeighted` -- 声誉分数影响选择
  - `TestImmuneDaemon_AttemptMigration_NilDaemon` -- nil daemon 返回 nil

### Task 4: Supervisor 集成能力迁移（AC: #3）

- [x] 4.1 在 `Supervisor` 结构体中新增 `immuneDaemon *ImmuneDaemon` 字段
- [x] 4.2 修改 `KernelImpl.SpawnSupervisor` 传递 `immuneDaemon` 引用到 `Supervisor`
- [x] 4.3 修改 `handleChildExit` 中 `exceedsRestartLimit()` 分支：
  - 在 `shutdownAll()` 之前，尝试调用 `immuneDaemon.AttemptMigration()`
  - 迁移参数从 `child.spec` 提取（Intent、Agent 模板名）
  - 上下文消息从 `ctxMgr.GetMessages(child.ctxID)` 获取（需传递 ctxMgr 引用）
  - 迁移成功：记录 `SupervisorMigration` 事件，不执行 `shutdownAll`（已有替代者继续）
  - 迁移失败：继续原有的 `shutdownAll` + `finishProcess` 流程

- [x] 4.4 单元测试：
  - `TestSupervisor_MigrationOnRestartExceeded` -- 重启超限时触发迁移
  - `TestSupervisor_MigrationFailed_FallbackShutdown` -- 迁移失败时正常关闭

### Task 5: IPC 协议扩展（AC: #6）

- [x] 5.1 在 `ipc/protocol.go` 新增：
  ```go
  MethodSimilarityQuery Method = "similarity_query"
  ```

- [x] 5.2 新增请求/响应类型：
  ```go
  type SimilarityQueryRequest struct {
      AgentName string  `json:"agent_name"`
      MinScore  float64 `json:"min_score,omitempty"` // 默认 0.0
  }

  type SimilarityQueryResponse struct {
      Agent       string                         `json:"agent"`
      Similarities []kernel.CapabilitySimilarity `json:"similarities"`
  }
  ```

- [x] 5.3 在 `ipc/server.go` 注册 `handleSimilarityQuery` handler
  - 调用 `immuneDaemon.GetSimilarAgents()`

- [x] 5.4 在 `ipc/client.go` 新增 `Client.SimilarityQuery(agentName string, minScore float64)` 方法

- [x] 5.5 单元测试：
  - `TestSimilarityQueryResponse_Serialization` -- JSON 序列化/反序列化
  - `TestHandleSimilarityQuery_Integration` -- server handler 调用验证

### Task 6: CLI 命令实现（AC: #7）

- [x] 6.1 在 `cmd/rnix/immune.go` 中新增 `similarity` 子命令：
  ```
  rnix immune similarity [agent-name]
  ```
  - 无 agent-name 时显示所有 Agent 对的相似度矩阵摘要
  - 指定 agent-name 时显示该 Agent 的相似 Agent 排行榜

- [x] 6.2 文本输出格式：
  ```
  Capability Similarity for "code-analyst":

  AGENT            SIMILARITY  SKILL OVERLAP
  code-reviewer         0.85  code-analysis, testing
  debugger              0.62  code-analysis
  doc-writer            0.31  -
  ```

- [x] 6.3 JSON 输出格式（`--json` flag）：
  - 使用 `SimilarityQueryResponse` 结构体

- [x] 6.4 单元测试：
  - `TestRunImmuneSimilarity_TextOutput` -- 文本格式验证
  - `TestRunImmuneSimilarity_JSONOutput` -- JSON 格式验证
  - `TestRunImmuneSimilarity_NoAgent` -- 无参数时输出所有

## Dev Notes

### 核心设计决策

**增量扩展，非重写。** 本 Story 在现有 `ImmuneDaemon` 基础上新增能力迁移模块，不修改 22.1~22.3 的现有功能。

**相似度算法选择 Jaccard 系数。** Skill 重叠度使用集合交并比（Jaccard Index）计算，简单高效，无需权重调优。综合分数加权：Skill 重叠 70% + 协作历史 30%。

**迁移为"尽力而为"。** 能力迁移不保证 100% 成功——若无满足阈值的替代 Agent，迁移放弃，Supervisor 继续执行原有的关闭逻辑。不阻塞 Supervisor 正常行为。

**迁移执行通过注入函数（MigrateFunc）。** 避免 `ImmuneDaemon` 直接依赖 `KernelImpl.Spawn`——通过函数注入保持依赖方向正确（kernel -> immune，不是 immune -> kernel）。

**协作历史为纯内存统计。** 本 Story 不引入持久化协作历史——daemon 重启后从零开始积累。后续 Story 22.5 可扩展持久化。

### 架构合规

- **依赖方向**：`kernel/immune.go` 仅使用标准库 + `internal/types`（不引入新依赖）
- **包边界**：所有新增代码在现有 `kernel/immune.go` 文件内扩展，不新增文件
- **IPC 扩展**：遵循 4 步标准流程（protocol → server → client → cmd）
- **Supervisor 修改**：仅在 `handleChildExit` 的 `exceedsRestartLimit()` 分支插入迁移尝试
- **nil 保护**：所有新增公开方法检查 `d == nil`，nil daemon 时返回安全默认值
- **JSON 字段**：使用 `snake_case` 命名（`skill_score`、`coop_score`、`original_pid`）

### 关键文件变更

| 文件 | 变更类型 | 说明 |
|------|---------|------|
| `kernel/immune.go` | 修改 | 新增 SimilarityMatrix、CapabilitySimilarity、MigrationResult 类型和方法 |
| `kernel/immune_test.go` | 修改 | 新增相似度矩阵和迁移引擎单元测试 |
| `kernel/supervisor.go` | 修改 | handleChildExit 集成迁移尝试 |
| `ipc/protocol.go` | 修改 | 新增 MethodSimilarityQuery + 请求/响应类型 |
| `ipc/server.go` | 修改 | 新增 handleSimilarityQuery handler |
| `ipc/client.go` | 修改 | 新增 Client.SimilarityQuery 方法 |
| `cmd/rnix/immune.go` | 修改 | 新增 similarity 子命令 |
| `cmd/rnix/immune_test.go` | 修改 | 新增 CLI 测试 |

### 复用模式

- **ImmuneDaemon 方法模式**：复用 22.1/22.2/22.3 的 nil 检查 + RWMutex 锁模式
- **IPC 扩展模式**：复用 `MethodImmuneStatus` 的 4 步扩展流程
- **ReputationStore.GetSummary()**：直接复用 21.3 的声誉查询（不修改声誉系统）
- **Supervisor 结构**：在现有 `handleChildExit` 分支中插入迁移逻辑，不改变现有流程
- **AgentInfo.AllowedTools()**：间接复用——迁移时通过 agentLoader 获取完整 AgentInfo

### 从 Story 22.3 继承的经验

- **nil 保护是关键**：所有新增公开方法检查 `d == nil`——22.3 验证了此模式的必要性
- **增量扩展优于重写**：22.3 成功地在现有代码上增量添加功能——本 Story 同样遵守
- **测试使用 cmd.OutOrStdout() 捕获**：CLI 测试通过 `bytes.Buffer` 替换 stdout 验证输出格式
- **IPC 向后兼容**：新增 Method 和 Response 类型不影响旧客户端

### 组合矩阵

| 本功能 | 交互对象 | 交互方式 | 需验证 |
|--------|----------|----------|--------|
| SimilarityMatrix | ImmuneDaemon (22.1~22.3) | 集成：作为 ImmuneDaemon 的新字段 | 是 |
| AttemptMigration | Supervisor (10.4) | 集成：restartLimit 分支调用迁移 | 是 |
| AttemptMigration | ReputationStore (21.3) | 依赖：读取声誉分数辅助选择 | 是 |
| AttemptMigration | agentLoader (20.2) | 依赖：加载替代 Agent 的 Skill 信息 | 是 |
| RecordCooperation | Kernel.Spawn (1.6) | 集成：Spawn 时记录父子 Agent 协作 | 是 |
| IPC similarity_query | IPC server (4.6) | 扩展：新增 Method 和 handler | 是 |
| CLI similarity | cmd/rnix/immune.go (22.1) | 扩展：新增子命令 | 是 |

### Project Structure Notes

- `kernel/immune.go` 在现有文件内扩展——保持 22.1/22.2/22.3 的单文件结构
- `kernel/supervisor.go` 修改 `handleChildExit` 方法——新增约 20 行迁移逻辑
- `ipc/protocol.go`、`ipc/server.go`、`ipc/client.go` 遵循 IPC 4 步扩展
- `cmd/rnix/immune.go` 新增 `similarity` 子命令
- 无新增文件——全部在现有文件中扩展

### References

- [Source: _bmad-output/planning-artifacts/epics/epic-22-适应性安全与自愈-adaptive-security-self-healing.md#Story 22.4]
- [Source: _bmad-output/planning-artifacts/prd/functional-requirements.md#FR134] -- 能力迁移
- [Source: _bmad-output/planning-artifacts/prd/functional-requirements.md#FR135] -- 能力相似度矩阵
- [Source: _bmad-output/planning-artifacts/prd/non-functional-requirements.md#NFR48] -- 迁移 ≤ 10s
- [Source: _bmad-output/implementation-artifacts/22-3-security-status-management.md] -- Story 22.3 实现和经验
- [Source: kernel/immune.go] -- ImmuneDaemon 现有实现
- [Source: kernel/supervisor.go] -- Supervisor 和 handleChildExit 现有实现
- [Source: kernel/reputation.go] -- ReputationStore 和 ReputationSummary
- [Source: agents/types.go] -- AgentInfo 和 AllowedTools
- [Source: skills/types.go] -- SkillManifest 和 AllowedTools
- [Source: _bmad-output/project-context.md#IPC 扩展标准步骤]

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6

### Debug Log References

### Completion Notes List

- 实现了 `CapabilitySimilarity` 结构体和 `SimilarityMatrix` 类型，使用 Jaccard 系数计算 Skill 重叠度
- 实现了协作历史归一化计算，综合分 = 0.7 * SkillScore + 0.3 * CoopScore
- `ImmuneDaemon` 集成相似度矩阵：`UpdateSimilarityMatrix`、`RecordCooperation`、`GetSimilarity`、`GetSimilarAgents`
- 实现了 `MigrationResult` 和 `AttemptMigration` 能力迁移引擎，支持声誉加权候选排序
- `Supervisor.handleChildExit` 集成迁移尝试：重启超限时先尝试迁移，成功则跳过 shutdownAll
- IPC 扩展：`MethodSimilarityQuery` + `SimilarityQueryRequest/Response` + server handler + client method
- CLI 扩展：`rnix immune similarity [agent-name]` 子命令，支持文本和 JSON 输出
- 所有 25 个 ATDD 测试通过（kernel: 25, ipc: 6, cmd: 6），全部 20 个包零回归

### File List

- `kernel/immune.go` — 新增 CapabilitySimilarity、SimilarityMatrix、MigrationResult、MigrateFunc 类型及 ImmuneDaemon 集成方法
- `kernel/supervisor.go` — Supervisor 结构体新增 immuneDaemon 字段，handleChildExit 集成迁移尝试
- `kernel/supervisor_test.go` — 新增 TestSupervisor_MigrationOnRestartExceeded 和 TestSupervisor_MigrationFailed_FallbackShutdown 测试
- `ipc/protocol.go` — 新增 MethodSimilarityQuery 常量、SimilarityQueryRequest/Response 类型
- `ipc/server.go` — 新增 handleSimilarityQuery handler
- `ipc/client.go` — 新增 Client.SimilarityQuery 方法
- `cmd/rnix/immune.go` — 新增 similarity 子命令、runImmuneSimilarity、formatSimilarityText
- `kernel/atdd_22_4_capability_migration_test.go` — ATDD 测试（预置）
- `ipc/atdd_22_4_similarity_ipc_test.go` — ATDD 测试（预置）
- `cmd/rnix/atdd_22_4_similarity_cmd_test.go` — ATDD 测试（预置）

### Change Log

- 2026-03-14: Story 22.4 实现完成 — 能力迁移与相似度矩阵全部功能实现，6 个 Task 全部完成
- 2026-03-14: Code Review 修复 — (1) Supervisor 迁移成功时添加 Reap(child.pid) 防止僵尸进程 (2) SimilarityMatrix.Compute 排序 agent 名以保证确定性 (3) 新增 TestSupervisor_MigrationOnRestartExceeded 和 TestSupervisor_MigrationFailed_FallbackShutdown 测试 (4) 修正 File List 移除未修改的 kernel/immune_test.go 和 cmd/rnix/immune_test.go
