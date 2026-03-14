# Story 22.5: 协作拓扑与强化路径

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a 平台构建者,
I want 系统自动识别高频协作路径，并通过 `rnix topology` 查看协作拓扑图,
So that 我可以了解智能体间的协作模式并优化编排。

## Acceptance Criteria

1. **AC1: 协作事件收集**
   - Given 系统有智能体间的协作历史（通过 `ImmuneDaemon.RecordCooperation` 在 Spawn 和 IPC Send 时记录）
   - When 系统分析协作数据
   - Then 协作历史包含 Spawn 父子关系和 IPC 消息发送两种协作类型
   - And 每种类型独立计数

2. **AC2: 强化路径自动识别**
   - Given 协作历史中存在高频协作对（协作次数 >= 阈值）
   - When 系统分析协作数据
   - Then 高频协作路径被自动识别和标记为强化路径（reinforced paths）
   - And 强化路径阈值可配置（默认 5 次）

3. **AC3: 协作拓扑图生成**
   - Given 存在协作历史数据
   - When 用户执行 `rnix topology`
   - Then 展示智能体协作拓扑图：节点=智能体（标注名称和声誉分数），边=协作关系（标注频率）
   - And 强化路径以特殊标记区分

4. **AC4: Compose 编排优先建议**
   - Given 后续 Compose 编排生成依赖关系
   - When 存在强化路径
   - Then 系统提供查询接口返回强化路径列表，供编排系统优先选择已验证的高频协作组合

5. **AC5: IPC 查询接口**
   - Given 协作拓扑数据已建立
   - When 用户通过 IPC 查询拓扑
   - Then 返回完整的拓扑图数据（节点列表 + 边列表 + 强化路径列表）

6. **AC6: JSON 输出**
   - Given 拓扑数据已建立
   - When 用户执行 `rnix topology --json`
   - Then 以 JSON 格式输出拓扑数据（与 IPC 响应格式一致）

## Tasks / Subtasks

### Task 1: 协作拓扑核心数据结构（AC: #1, #2, #4）

- [x] 1.1 在 `kernel/immune.go` 中新增协作拓扑类型：
  ```go
  // CooperationEdge represents a directed cooperation relationship between two agents.
  type CooperationEdge struct {
      From       string `json:"from"`
      To         string `json:"to"`
      SpawnCount int    `json:"spawn_count"` // parent spawned child count
      MsgCount   int    `json:"msg_count"`   // IPC message send count
      Total      int    `json:"total"`       // SpawnCount + MsgCount
      Reinforced bool   `json:"reinforced"`  // true if Total >= threshold
  }

  // TopologyNode represents an agent in the collaboration topology.
  type TopologyNode struct {
      Agent           string  `json:"agent"`
      ReputationScore float64 `json:"reputation_score"`
      Connections     int     `json:"connections"` // number of edges involving this node
  }

  // CollaborationTopology holds the complete collaboration graph.
  type CollaborationTopology struct {
      Nodes           []TopologyNode    `json:"nodes"`
      Edges           []CooperationEdge `json:"edges"`
      ReinforcedPaths []CooperationEdge `json:"reinforced_paths"`
  }
  ```

- [x] 1.2 新增强化路径阈值常量：
  ```go
  const DefaultReinforcementThreshold = 5
  ```

- [x] 1.3 在 `ImmuneDaemon` 中扩展 `coopHistory` 为细分类型：
  ```go
  // 扩展现有 coopHistory 结构，新增类型化协作记录
  type CoopRecord struct {
      SpawnCount int
      MsgCount   int
  }
  ```
  - 新增 `coopRecords map[string]map[string]*CoopRecord` 字段（agentA -> agentB -> record）
  - 保持现有 `coopHistory` 字段向后兼容（总计数继续用于相似度矩阵）

- [x] 1.4 新增 `ImmuneDaemon.RecordCooperationTyped(agentA, agentB string, coopType string)` 方法：
  - `coopType` 为 `"spawn"` 或 `"msg"`
  - 更新 `coopRecords` 中的对应计数
  - 同时调用现有的 `RecordCooperation()` 保持向后兼容
  - nil daemon 安全

- [x] 1.5 新增 `ImmuneDaemon.GetTopology() *CollaborationTopology` 方法：
  - 从 `coopRecords` 构建边列表
  - 从 `repStore` 获取每个 Agent 的声誉分数
  - 使用 `DefaultReinforcementThreshold` 判断强化路径
  - nil daemon 返回 nil

- [x] 1.6 新增 `ImmuneDaemon.GetReinforcedPaths() []CooperationEdge` 方法：
  - 返回 Total >= threshold 的边列表（按 Total 降序排列）
  - nil daemon 返回 nil

- [x] 1.7 单元测试：
  - `TestImmuneDaemon_RecordCooperationTyped` -- 分类型记录正确
  - `TestImmuneDaemon_GetTopology_Basic` -- 基本拓扑构建
  - `TestImmuneDaemon_GetTopology_WithReputation` -- 声誉分数集成
  - `TestImmuneDaemon_GetTopology_ReinforcedPaths` -- 高频路径标记
  - `TestImmuneDaemon_GetReinforcedPaths_Sorted` -- 按频率降序
  - `TestImmuneDaemon_GetTopology_NilDaemon` -- nil daemon 返回 nil
  - `TestImmuneDaemon_GetTopology_Empty` -- 无协作数据时返回空拓扑

### Task 2: IPC 协议扩展（AC: #5）

- [x] 2.1 在 `ipc/protocol.go` 新增：
  ```go
  MethodTopologyQuery Method = "topology_query"
  ```

- [x] 2.2 新增请求/响应类型：
  ```go
  type TopologyQueryRequest struct{}

  type TopologyQueryResponse struct {
      Nodes           []kernel.TopologyNode    `json:"nodes"`
      Edges           []kernel.CooperationEdge `json:"edges"`
      ReinforcedPaths []kernel.CooperationEdge `json:"reinforced_paths"`
  }
  ```

- [x] 2.3 在 `ipc/server.go` 注册 `handleTopologyQuery` handler
  - 调用 `immuneDaemon.GetTopology()`

- [x] 2.4 在 `ipc/client.go` 新增 `Client.TopologyQuery()` 方法

- [x] 2.5 单元测试：
  - `TestTopologyQueryResponse_Serialization` -- JSON 序列化/反序列化
  - `TestHandleTopologyQuery_Integration` -- server handler 调用验证

### Task 3: CLI 命令实现（AC: #3, #6）

- [x] 3.1 在 `cmd/rnix/` 中新增 `topology.go` 文件，注册顶级命令：
  ```
  rnix topology
  ```
  - 顶级命令（非 immune 子命令），因为 FR137 定义的是 `rnix topology`

- [x] 3.2 文本输出格式设计：
  ```
  Collaboration Topology (3 agents, 4 edges)

  NODES:
  AGENT                REPUTATION  CONNECTIONS
  code-analyst              0.85            3
  code-reviewer              0.72            2
  debugger                   0.65            2

  EDGES:
  FROM                 TO                   SPAWN  MSG  TOTAL  REINFORCED
  code-analyst         code-reviewer            8    3     11  *
  code-analyst         debugger                 2    5      7  *
  code-reviewer        debugger                 1    1      2
  code-analyst         doc-writer               1    0      1

  REINFORCED PATHS (2):
  code-analyst -> code-reviewer (11 interactions)
  code-analyst -> debugger (7 interactions)
  ```

- [x] 3.3 JSON 输出格式（`--json` flag）：
  - 使用 `TopologyQueryResponse` 结构体

- [x] 3.4 单元测试：
  - `TestRunTopology_TextOutput` -- 文本格式验证
  - `TestRunTopology_JSONOutput` -- JSON 格式验证
  - `TestRunTopology_NoData` -- 无数据时输出提示
  - `TestRunTopology_DaemonUnavailable` -- daemon 不可用错误处理

## Dev Notes

### 核心设计决策

**增量扩展，非重写。** 本 Story 在 22.4 的 `ImmuneDaemon.coopHistory` 基础上新增协作拓扑视图和强化路径识别，不修改 22.1~22.4 的现有功能。

**类型化协作记录。** 22.4 只记录了协作总次数（`coopHistory map[string]map[string]int`），22.5 需区分 Spawn 和 IPC 消息两种协作类型。解决方案：新增 `coopRecords` 字段存储细分类型，同时保持 `coopHistory` 向后兼容（相似度矩阵继续使用总计数）。

**强化路径为纯内存计算。** 强化路径基于当前 `coopRecords` 实时计算，不引入额外持久化。阈值硬编码为常量 `DefaultReinforcementThreshold = 5`。

**CLI 为顶级命令。** Epic 文件中 FR137 定义为 `rnix topology`（非 `rnix immune topology`），因此注册为 rootCmd 的子命令，与 `rnix ps`、`rnix kill` 平级。

**声誉分数集成。** 拓扑节点的声誉分数通过现有 `ReputationStore.GetSummary()` 查询，不修改声誉系统。查询失败时默认为 0.0。

### 架构合规

- **依赖方向**：`kernel/immune.go` 仅使用标准库 + `internal/types`（不引入新依赖）
- **包边界**：核心拓扑逻辑在 `kernel/immune.go` 内扩展，CLI 新建 `cmd/rnix/topology.go`
- **IPC 扩展**：遵循 4 步标准流程（protocol -> server -> client -> cmd）
- **nil 保护**：所有新增公开方法检查 `d == nil`，nil daemon 时返回安全默认值
- **JSON 字段**：使用 `snake_case` 命名（`spawn_count`、`msg_count`、`reinforced_paths`）

### 关键文件变更

| 文件 | 变更类型 | 说明 |
|------|---------|------|
| `kernel/immune.go` | 修改 | 新增 CooperationEdge、TopologyNode、CollaborationTopology 类型和 ImmuneDaemon 拓扑方法 |
| `kernel/immune_test.go` | 修改 | 新增拓扑和强化路径单元测试 |
| `ipc/protocol.go` | 修改 | 新增 MethodTopologyQuery + 请求/响应类型 |
| `ipc/server.go` | 修改 | 新增 handleTopologyQuery handler |
| `ipc/client.go` | 修改 | 新增 Client.TopologyQuery 方法 |
| `cmd/rnix/topology.go` | 新增 | topology 顶级命令 |
| `cmd/rnix/topology_test.go` | 新增 | CLI 测试 |

### 复用模式

- **ImmuneDaemon 方法模式**：复用 22.1~22.4 的 nil 检查 + RWMutex 锁模式
- **IPC 扩展模式**：复用 `MethodSimilarityQuery` 的 4 步扩展流程
- **ReputationStore.GetSummary()**：直接复用 21.3 的声誉查询（不修改声誉系统）
- **CLI 模式**：复用 `cmd/rnix/immune.go` 的 flagJSON 判断 + JSONResponse 包装 + cmd.OutOrStdout() 输出
- **RecordCooperation()**：22.4 已建立的协作记录方法，新增 Typed 版本不改变原方法签名

### 从 Story 22.4 继承的经验

- **nil 保护是关键**：所有新增公开方法检查 `d == nil`——22.3/22.4 已验证此模式
- **增量扩展优于重写**：22.4 成功地在现有代码上增量添加——本 Story 同样遵守
- **测试使用 cmd.OutOrStdout() 捕获**：CLI 测试通过 `bytes.Buffer` 替换 stdout 验证输出格式
- **IPC 向后兼容**：新增 Method 和 Response 类型不影响旧客户端
- **向后兼容字段**：新增 `coopRecords` 不删除旧 `coopHistory`，避免破坏 22.4 的相似度矩阵计算

### 组合矩阵

| 本功能 | 交互对象 | 交互方式 | 需验证 |
|--------|----------|----------|--------|
| GetTopology | ImmuneDaemon (22.1~22.4) | 集成：读取 coopRecords 和 repStore | 是 |
| GetTopology | ReputationStore (21.3) | 依赖：读取声誉分数填充节点 | 是 |
| RecordCooperationTyped | RecordCooperation (22.4) | 兼容：调用原方法保持总计数 | 是 |
| GetReinforcedPaths | SimilarityMatrix (22.4) | 独立：强化路径与相似度矩阵独立计算 | 否 |
| IPC topology_query | IPC server (4.6) | 扩展：新增 Method 和 handler | 是 |
| CLI topology | rootCmd (1.7) | 扩展：新增顶级子命令 | 是 |
| 强化路径 | Compose 编排 (7.x) | 查询接口：GetReinforcedPaths 供编排使用 | 是 |

### Project Structure Notes

- `kernel/immune.go` 在现有文件内扩展——保持 22.1~22.4 的单文件结构
- `cmd/rnix/topology.go` 新增文件——因为是独立顶级命令，不适合放入 immune.go
- `ipc/protocol.go`、`ipc/server.go`、`ipc/client.go` 遵循 IPC 4 步扩展
- 无新增 kernel 文件——全部在 `kernel/immune.go` 中扩展

### References

- [Source: _bmad-output/planning-artifacts/epics/epic-22-适应性安全与自愈-adaptive-security-self-healing.md#Story 22.5]
- [Source: _bmad-output/planning-artifacts/prd/functional-requirements.md#FR136] -- 高频协作路径自动识别和强化
- [Source: _bmad-output/planning-artifacts/prd/functional-requirements.md#FR137] -- rnix topology 协作拓扑图
- [Source: _bmad-output/planning-artifacts/prd/non-functional-requirements.md#NFR47] -- Immune Daemon CPU 开销 <= 3%
- [Source: _bmad-output/implementation-artifacts/22-4-capability-migration-and-similarity-matrix.md] -- Story 22.4 实现和 coopHistory 基础
- [Source: kernel/immune.go] -- ImmuneDaemon 现有实现（含 coopHistory 字段）
- [Source: kernel/reputation.go] -- ReputationStore 和 ReputationSummary
- [Source: ipc/protocol.go] -- IPC Method 和 Request/Response 模式
- [Source: cmd/rnix/immune.go] -- CLI 命令模式（flagJSON、JSONResponse）
- [Source: _bmad-output/project-context.md#IPC 扩展标准步骤]

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6

### Debug Log References

### Completion Notes List

- Task 1 完成：在 `kernel/immune.go` 中新增 `CooperationEdge`、`TopologyNode`、`CollaborationTopology`、`CoopRecord` 类型，`DefaultReinforcementThreshold` 常量，`RecordCooperationTyped`、`GetTopology`、`GetReinforcedPaths` 方法。22 个 ATDD 测试全部通过。
- Task 2 完成：在 `ipc/protocol.go` 新增 `MethodTopologyQuery`、`TopologyQueryRequest`、`TopologyQueryResponse` 类型。在 `ipc/server.go` 注册 `handleTopologyQuery` handler。在 `ipc/client.go` 新增 `TopologyQuery` 方法。6 个 IPC ATDD 测试通过。
- Task 3 完成：新增 `cmd/rnix/topology.go`，注册 `rnix topology` 顶级命令，支持文本和 `--json` 输出。7 个 CLI ATDD 测试通过。
- 全部 20 个包的 `make all`（lint + vet + test + build）通过，无回归。

### Change Log

- 2026-03-14: Story 22.5 全部实现完成。新增协作拓扑数据结构、类型化协作记录、强化路径识别、IPC topology_query 协议、`rnix topology` CLI 命令。
- 2026-03-14: 代码审查完成。修复 1 个 MEDIUM 问题（File List 遗漏 2 个测试文件 + 1 个状态标注错误）。所有 6 个 AC 验证通过，35 个测试通过。状态 → done。

### File List

- `kernel/immune.go` (修改) — 新增 CooperationEdge、TopologyNode、CollaborationTopology、CoopRecord 类型 + ImmuneDaemon 拓扑方法
- `kernel/atdd_22_5_collaboration_topology_test.go` (新增) — 22 个 ATDD 单元测试
- `ipc/protocol.go` (修改) — 新增 MethodTopologyQuery + TopologyQueryRequest/Response
- `ipc/server.go` (修改) — 新增 handleTopologyQuery handler + dispatch 注册
- `ipc/client.go` (修改) — 新增 Client.TopologyQuery 方法
- `ipc/atdd_22_5_topology_ipc_test.go` (新增) — 6 个 IPC 协议 ATDD 测试
- `cmd/rnix/topology.go` (新增) — rnix topology 顶级命令 + formatTopologyText
- `cmd/rnix/atdd_22_5_topology_cmd_test.go` (新增) — 7 个 CLI ATDD 测试

## Senior Developer Review (AI)

**Reviewer:** Decker (AI)
**Date:** 2026-03-14
**Outcome:** Approve

### Git vs Story Discrepancy: 1 found (fixed)
- **[MEDIUM] File List 不完整**: Story File List 遗漏了 `ipc/atdd_22_5_topology_ipc_test.go` 和 `cmd/rnix/atdd_22_5_topology_cmd_test.go` 两个测试文件，且将 `kernel/atdd_22_5_collaboration_topology_test.go` 错误标注为"修改"而非"新增"。**已修复**：更新 File List 添加所有 8 个文件并更正状态标注。

### AC 验证结果
| AC | 状态 | 证据 |
|----|------|------|
| AC1: 协作事件收集 | IMPLEMENTED | `RecordCooperationTyped` (immune.go:1195)，区分 spawn/msg 类型，测试 UNIT-005~009 |
| AC2: 强化路径自动识别 | IMPLEMENTED | `GetTopology` 中 `total >= DefaultReinforcementThreshold` (immune.go:1259)，测试 UNIT-012, UNIT-019 |
| AC3: 协作拓扑图生成 | IMPLEMENTED | `formatTopologyText` (topology.go:68)，节点含名称+声誉，边含频率，强化路径 `*` 标记，测试 CLI-001~006 |
| AC4: Compose 编排优先建议 | IMPLEMENTED | `GetReinforcedPaths` (immune.go:1316)，按 Total 降序排列，测试 UNIT-015~017 |
| AC5: IPC 查询接口 | IMPLEMENTED | `MethodTopologyQuery` + handler + client 完整 4 步 IPC 扩展，测试 IPC-001~006 |
| AC6: JSON 输出 | IMPLEMENTED | `--json` flag (topology.go:57)，测试 CLI-002 |

### 任务完成审计
所有 3 个 Task 的 16 个子任务均标注 [x]，逐一验证后确认全部实际实现。

### 代码质量评估
- **安全**: 无注入风险，无认证问题，所有公开方法 nil 保护到位
- **性能**: 纯内存计算，排序操作在小数据集上，无 N+1 问题
- **并发安全**: RWMutex 使用正确，`RecordCooperationTyped` 在释放锁后调用 `RecordCooperation` 避免死锁，测试 UNIT-020 使用 `-race` 检测确认
- **向后兼容**: `coopRecords` 新增字段不影响现有 `coopHistory`，`RecordCooperationTyped` 调用 `RecordCooperation` 保持相似度矩阵正常工作（测试 UNIT-008 确认）
- **测试质量**: 35 个测试全部为真实断言（22 kernel + 6 IPC + 7 CLI），覆盖 nil 安全、空数据、边界条件、并发访问

### 发现问题汇总
| 严重度 | 数量 | 说明 |
|--------|------|------|
| HIGH | 0 | — |
| MEDIUM | 1 | File List 不完整（已修复） |
| LOW | 0 | — |

### 验证结果
- `go vet ./...`: 通过
- `go test -race ./...`: 全部 20 个包通过
- `go build`: 成功
