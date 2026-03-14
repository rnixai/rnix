---
stepsCompleted: ['step-01-preflight-and-context', 'step-02-generation-mode', 'step-03-test-strategy', 'step-04-generate-tests', 'step-05-validate-and-complete']
lastStep: 'step-05-validate-and-complete'
lastSaved: '2026-03-14'
workflowType: 'testarch-atdd'
inputDocuments:
  - '_bmad-output/implementation-artifacts/22-5-collaboration-topology-and-reinforcement-paths.md'
  - 'kernel/immune.go'
  - 'kernel/atdd_22_4_capability_migration_test.go'
  - 'kernel/reputation.go'
  - 'ipc/protocol.go'
  - 'ipc/server.go'
  - 'ipc/client.go'
  - 'ipc/atdd_22_4_similarity_ipc_test.go'
  - 'cmd/rnix/immune.go'
  - 'cmd/rnix/atdd_22_4_similarity_cmd_test.go'
---

# ATDD Checklist - Epic 22, Story 5: 协作拓扑与强化路径

**Date:** 2026-03-14
**Author:** Decker
**Primary Test Level:** Unit + Integration (Go backend)

---

## Story Summary

系统自动识别高频协作路径，并通过 `rnix topology` 查看协作拓扑图，了解智能体间的协作模式并优化编排。

**As a** 平台构建者
**I want** 系统自动识别高频协作路径，并通过 `rnix topology` 查看协作拓扑图
**So that** 我可以了解智能体间的协作模式并优化编排

---

## Acceptance Criteria

1. **AC1: 协作事件收集** - 协作历史包含 Spawn 父子关系和 IPC 消息发送两种协作类型，每种类型独立计数
2. **AC2: 强化路径自动识别** - 高频协作路径被自动识别和标记为强化路径（阈值可配置，默认 5 次）
3. **AC3: 协作拓扑图生成** - `rnix topology` 展示节点（智能体+声誉）和边（协作关系+频率），强化路径有特殊标记
4. **AC4: Compose 编排优先建议** - 查询接口返回强化路径列表，供编排系统优先选择
5. **AC5: IPC 查询接口** - 通过 IPC 查询拓扑，返回完整的拓扑图数据
6. **AC6: JSON 输出** - `rnix topology --json` 以 JSON 格式输出拓扑数据

---

## Failing Tests Created (RED Phase)

### Unit Tests - kernel/atdd_22_5_collaboration_topology_test.go (22 tests)

**File:** `kernel/atdd_22_5_collaboration_topology_test.go`

- **Test:** `TestDefaultReinforcementThreshold_Value` (22.5-UNIT-001)
  - **Status:** RED - DefaultReinforcementThreshold 常量不存在
  - **Verifies:** AC2 - 强化路径阈值常量为 5
  - **Priority:** P0

- **Test:** `TestCooperationEdge_Fields` (22.5-UNIT-002)
  - **Status:** RED - CooperationEdge 类型不存在
  - **Verifies:** AC1 - 协作边结构体字段完整
  - **Priority:** P0

- **Test:** `TestTopologyNode_Fields` (22.5-UNIT-003)
  - **Status:** RED - TopologyNode 类型不存在
  - **Verifies:** AC3 - 拓扑节点结构体字段完整
  - **Priority:** P0

- **Test:** `TestCollaborationTopology_Fields` (22.5-UNIT-004)
  - **Status:** RED - CollaborationTopology 类型不存在
  - **Verifies:** AC3 - 协作拓扑结构体字段完整
  - **Priority:** P0

- **Test:** `TestImmuneDaemon_RecordCooperationTyped_Spawn` (22.5-UNIT-005)
  - **Status:** RED - ImmuneDaemon.RecordCooperationTyped 方法不存在
  - **Verifies:** AC1 - Spawn 类型协作记录正确
  - **Priority:** P0

- **Test:** `TestImmuneDaemon_RecordCooperationTyped_Msg` (22.5-UNIT-006)
  - **Status:** RED - ImmuneDaemon.RecordCooperationTyped 方法不存在
  - **Verifies:** AC1 - IPC 消息类型协作记录正确
  - **Priority:** P0

- **Test:** `TestImmuneDaemon_RecordCooperationTyped_Mixed` (22.5-UNIT-007)
  - **Status:** RED - ImmuneDaemon.RecordCooperationTyped 方法不存在
  - **Verifies:** AC1 - 混合类型协作的 SpawnCount 和 MsgCount 独立计数
  - **Priority:** P0

- **Test:** `TestImmuneDaemon_RecordCooperationTyped_BackwardCompat` (22.5-UNIT-008)
  - **Status:** RED - ImmuneDaemon.RecordCooperationTyped 方法不存在
  - **Verifies:** AC1 - RecordCooperationTyped 同时更新 coopHistory（22.4 向后兼容）
  - **Priority:** P0

- **Test:** `TestImmuneDaemon_RecordCooperationTyped_NilDaemon` (22.5-UNIT-009)
  - **Status:** RED - ImmuneDaemon.RecordCooperationTyped 方法不存在
  - **Verifies:** AC1 - nil daemon 安全
  - **Priority:** P0

- **Test:** `TestImmuneDaemon_GetTopology_Basic` (22.5-UNIT-010)
  - **Status:** RED - ImmuneDaemon.GetTopology 方法不存在
  - **Verifies:** AC3 - 基本拓扑构建（节点和边）
  - **Priority:** P0

- **Test:** `TestImmuneDaemon_GetTopology_WithReputation` (22.5-UNIT-011)
  - **Status:** RED - ImmuneDaemon.GetTopology 方法不存在
  - **Verifies:** AC3 - 声誉分数集成到节点
  - **Priority:** P0

- **Test:** `TestImmuneDaemon_GetTopology_ReinforcedPaths` (22.5-UNIT-012)
  - **Status:** RED - ImmuneDaemon.GetTopology 方法不存在
  - **Verifies:** AC2 - 高频路径标记为强化路径，低频路径不标记
  - **Priority:** P0

- **Test:** `TestImmuneDaemon_GetTopology_NilDaemon` (22.5-UNIT-013)
  - **Status:** RED - ImmuneDaemon.GetTopology 方法不存在
  - **Verifies:** AC3 - nil daemon 返回 nil
  - **Priority:** P0

- **Test:** `TestImmuneDaemon_GetTopology_Empty` (22.5-UNIT-014)
  - **Status:** RED - ImmuneDaemon.GetTopology 方法不存在
  - **Verifies:** AC3 - 无协作数据时返回空拓扑
  - **Priority:** P0

- **Test:** `TestImmuneDaemon_GetReinforcedPaths_Sorted` (22.5-UNIT-015)
  - **Status:** RED - ImmuneDaemon.GetReinforcedPaths 方法不存在
  - **Verifies:** AC4 - 强化路径按 Total 降序排列
  - **Priority:** P0

- **Test:** `TestImmuneDaemon_GetReinforcedPaths_NilDaemon` (22.5-UNIT-016)
  - **Status:** RED - ImmuneDaemon.GetReinforcedPaths 方法不存在
  - **Verifies:** AC4 - nil daemon 返回 nil
  - **Priority:** P0

- **Test:** `TestImmuneDaemon_GetReinforcedPaths_NoneAboveThreshold` (22.5-UNIT-017)
  - **Status:** RED - ImmuneDaemon.GetReinforcedPaths 方法不存在
  - **Verifies:** AC4 - 无高于阈值的路径时返回空
  - **Priority:** P1

- **Test:** `TestImmuneDaemon_GetTopology_NodeConnections` (22.5-UNIT-018)
  - **Status:** RED - ImmuneDaemon.GetTopology 方法不存在
  - **Verifies:** AC3 - 节点连接数正确计算
  - **Priority:** P0

- **Test:** `TestImmuneDaemon_GetTopology_ReinforcedAtExactThreshold` (22.5-UNIT-019)
  - **Status:** RED - ImmuneDaemon.GetTopology 方法不存在
  - **Verifies:** AC2 - 恰好等于阈值时标记为强化路径（>= 而非 >）
  - **Priority:** P1

- **Test:** `TestImmuneDaemon_RecordCooperationTyped_ConcurrentAccess` (22.5-UNIT-020)
  - **Status:** RED - ImmuneDaemon.RecordCooperationTyped 方法不存在
  - **Verifies:** AC1 - 并发记录不竞态
  - **Priority:** P1

- **Test:** `TestImmuneDaemon_GetTopology_NoReputationStore` (22.5-UNIT-021)
  - **Status:** RED - ImmuneDaemon.GetTopology 方法不存在
  - **Verifies:** AC3 - 无声誉存储时默认 0.0
  - **Priority:** P1

- **Test:** `TestImmuneDaemon_GetTopology_EdgeDirectionConsistent` (22.5-UNIT-022)
  - **Status:** RED - ImmuneDaemon.GetTopology 方法不存在
  - **Verifies:** AC3 - 双向协作聚合为单条边（不重复）
  - **Priority:** P1

### IPC Protocol Tests - ipc/atdd_22_5_topology_ipc_test.go (6 tests)

**File:** `ipc/atdd_22_5_topology_ipc_test.go`

- **Test:** `TestMethodTopologyQuery_Constant` (22.5-IPC-001)
  - **Status:** RED - MethodTopologyQuery 常量不存在
  - **Verifies:** AC5 - Method 常量定义
  - **Priority:** P0

- **Test:** `TestTopologyQueryResponse_Serialization` (22.5-IPC-002)
  - **Status:** RED - TopologyQueryResponse 类型不存在
  - **Verifies:** AC5 - 响应 JSON 序列化/反序列化
  - **Priority:** P0

- **Test:** `TestTopologyQueryResponse_JSONFieldNames` (22.5-IPC-003)
  - **Status:** RED - TopologyQueryResponse 类型不存在
  - **Verifies:** AC5 - JSON 字段使用 snake_case
  - **Priority:** P0

- **Test:** `TestTopologyQueryResponse_EmptyTopology` (22.5-IPC-004)
  - **Status:** RED - TopologyQueryResponse 类型不存在
  - **Verifies:** AC5 - 空数组序列化为 [] 而非 null
  - **Priority:** P1

- **Test:** `TestTopologyQueryResponse_BackwardCompatible` (22.5-IPC-005)
  - **Status:** RED - TopologyQueryResponse 类型不存在
  - **Verifies:** AC5 - 旧版 JSON 反序列化兼容
  - **Priority:** P0

- **Test:** `TestTopologyQueryRequest_Empty` (22.5-IPC-006)
  - **Status:** RED - TopologyQueryRequest 类型不存在
  - **Verifies:** AC5 - 空请求结构体序列化正确
  - **Priority:** P0

### CLI Tests - cmd/rnix/atdd_22_5_topology_cmd_test.go (7 tests)

**File:** `cmd/rnix/atdd_22_5_topology_cmd_test.go`

- **Test:** `TestRunTopology_TextOutput` (22.5-CLI-001)
  - **Status:** RED - formatTopologyText 函数不存在
  - **Verifies:** AC3 - 文本格式显示拓扑图（节点+边+强化路径）
  - **Priority:** P0

- **Test:** `TestRunTopology_JSONOutput` (22.5-CLI-002)
  - **Status:** RED - TopologyQueryResponse 类型不存在（编译失败）
  - **Verifies:** AC6 - JSON 格式输出
  - **Priority:** P0

- **Test:** `TestRunTopology_NoDaemon` (22.5-CLI-003)
  - **Status:** RED - topologyCmd 不存在（编译失败）
  - **Verifies:** AC3 - 无 daemon 时错误提示
  - **Priority:** P0

- **Test:** `TestTopologyCmd_Registered` (22.5-CLI-004)
  - **Status:** RED - topologyCmd 不存在（编译失败）
  - **Verifies:** AC3 - topology 命令注册为顶级命令
  - **Priority:** P0

- **Test:** `TestRunTopology_TextOutput_NoData` (22.5-CLI-005)
  - **Status:** RED - formatTopologyText 函数不存在
  - **Verifies:** AC3 - 无数据时输出提示
  - **Priority:** P0

- **Test:** `TestRunTopology_TextOutput_ReinforcedMarker` (22.5-CLI-006)
  - **Status:** RED - formatTopologyText 函数不存在
  - **Verifies:** AC3 - 强化路径有特殊标记（*）
  - **Priority:** P1

- **Test:** `TestTopologyCmd_HasJSONFlag` (22.5-CLI-007)
  - **Status:** RED - topologyCmd 不存在（编译失败）
  - **Verifies:** AC6 - 命令支持 --json 标志
  - **Priority:** P1

---

## Implementation Checklist

### Task 1: 协作拓扑核心数据结构 (kernel/immune.go)

**Tests to make pass:** 22.5-UNIT-001, 22.5-UNIT-002, 22.5-UNIT-003, 22.5-UNIT-004

- [ ] 新增 `DefaultReinforcementThreshold = 5` 常量
- [ ] 新增 `CooperationEdge` 结构体（From, To, SpawnCount, MsgCount, Total, Reinforced）
- [ ] 新增 `TopologyNode` 结构体（Agent, ReputationScore, Connections）
- [ ] 新增 `CollaborationTopology` 结构体（Nodes, Edges, ReinforcedPaths）
- [ ] 新增 `CoopRecord` 结构体（SpawnCount, MsgCount）
- [ ] Run: `go test -race -run "TestDefaultReinforcementThreshold_Value|TestCooperationEdge_Fields|TestTopologyNode_Fields|TestCollaborationTopology_Fields" ./kernel/...`

### Task 2: 类型化协作记录 (kernel/immune.go)

**Tests to make pass:** 22.5-UNIT-005, 22.5-UNIT-006, 22.5-UNIT-007, 22.5-UNIT-008, 22.5-UNIT-009, 22.5-UNIT-020

- [ ] 在 `ImmuneDaemon` 中新增 `coopRecords map[string]map[string]*CoopRecord` 字段
- [ ] 新增 `RecordCooperationTyped(agentA, agentB string, coopType string)` 方法
  - 更新 `coopRecords` 中的对应计数（spawn 或 msg）
  - 同时调用 `RecordCooperation()` 保持向后兼容
  - nil daemon 安全
- [ ] Run: `go test -race -run "TestImmuneDaemon_RecordCooperationTyped" ./kernel/...`

### Task 3: 拓扑构建引擎 (kernel/immune.go)

**Tests to make pass:** 22.5-UNIT-010, 22.5-UNIT-011, 22.5-UNIT-012, 22.5-UNIT-013, 22.5-UNIT-014, 22.5-UNIT-018, 22.5-UNIT-019, 22.5-UNIT-021, 22.5-UNIT-022

- [ ] 新增 `GetTopology() *CollaborationTopology` 方法
  - 从 `coopRecords` 构建边列表（计算 SpawnCount, MsgCount, Total）
  - 判断 Reinforced（Total >= DefaultReinforcementThreshold）
  - 从 `repStore` 获取每个 Agent 的声誉分数（查询失败默认 0.0）
  - 计算每个节点的 Connections 数
  - nil daemon 返回 nil
  - 无协作数据时返回空拓扑（非 nil）
- [ ] Run: `go test -race -run "TestImmuneDaemon_GetTopology" ./kernel/...`

### Task 4: 强化路径查询 (kernel/immune.go)

**Tests to make pass:** 22.5-UNIT-015, 22.5-UNIT-016, 22.5-UNIT-017

- [ ] 新增 `GetReinforcedPaths() []CooperationEdge` 方法
  - 返回 Total >= threshold 的边列表（按 Total 降序排列）
  - nil daemon 返回 nil
- [ ] Run: `go test -race -run "TestImmuneDaemon_GetReinforcedPaths" ./kernel/...`

### Task 5: IPC 协议扩展 (ipc/protocol.go)

**Tests to make pass:** 22.5-IPC-001, 22.5-IPC-002, 22.5-IPC-003, 22.5-IPC-004, 22.5-IPC-005, 22.5-IPC-006

- [ ] 新增 `MethodTopologyQuery Method = "topology_query"` 常量
- [ ] 新增 `TopologyQueryRequest` 结构体（空）
- [ ] 新增 `TopologyQueryResponse` 结构体（Nodes, Edges, ReinforcedPaths）
- [ ] Run: `go test -race -run "TestMethodTopologyQuery|TestTopologyQueryResponse|TestTopologyQueryRequest" ./ipc/...`

### Task 6: IPC Server handler (ipc/server.go)

- [ ] 注册 `MethodTopologyQuery` → `handleTopologyQuery` handler
- [ ] `handleTopologyQuery` 调用 `immuneDaemon.GetTopology()`

### Task 7: IPC Client 方法 (ipc/client.go)

- [ ] 新增 `Client.TopologyQuery() (*TopologyQueryResponse, error)`

### Task 8: CLI 命令实现 (cmd/rnix/topology.go)

**Tests to make pass:** 22.5-CLI-001, 22.5-CLI-002, 22.5-CLI-003, 22.5-CLI-004, 22.5-CLI-005, 22.5-CLI-006, 22.5-CLI-007

- [ ] 新建 `cmd/rnix/topology.go` 文件
- [ ] 新增 `topologyCmd` — `rnix topology`（顶级命令）
- [ ] 新增 `formatTopologyText(w io.Writer, resp *TopologyQueryResponse)` 格式化函数
- [ ] 文本输出格式：Collaboration Topology 标题 + NODES 表格 + EDGES 表格 + REINFORCED PATHS 列表
- [ ] JSON 输出：`JSONResponse{OK: true, Data: resp}`
- [ ] 在 `init()` 中注册 `rootCmd.AddCommand(topologyCmd)`
- [ ] Run: `go test -race -run "TestRunTopology|TestTopologyCmd" ./cmd/rnix/...`

---

## Test Summary

| Category | Test File | Count | Priority |
|----------|----------|-------|----------|
| Unit (kernel) | `kernel/atdd_22_5_collaboration_topology_test.go` | 22 | 14 P0, 8 P1 |
| IPC | `ipc/atdd_22_5_topology_ipc_test.go` | 6 | 5 P0, 1 P1 |
| CLI | `cmd/rnix/atdd_22_5_topology_cmd_test.go` | 7 | 5 P0, 2 P1 |
| **Total** | | **35** | **24 P0, 11 P1** |

---

## AC Coverage Matrix

| AC | Description | Tests |
|----|------------|-------|
| AC1 | 协作事件收集 | 22.5-UNIT-002, 005, 006, 007, 008, 009, 020 |
| AC2 | 强化路径自动识别 | 22.5-UNIT-001, 012, 019 |
| AC3 | 协作拓扑图生成 | 22.5-UNIT-003, 004, 010, 011, 013, 014, 018, 021, 022; 22.5-CLI-001, 003, 004, 005, 006 |
| AC4 | Compose 编排优先建议 | 22.5-UNIT-015, 016, 017 |
| AC5 | IPC 查询接口 | 22.5-IPC-001, 002, 003, 004, 005, 006 |
| AC6 | JSON 输出 | 22.5-CLI-002, 007 |

---

## Key Risks & Assumptions

1. **增量扩展不重写**：新增 `coopRecords` 不删除旧 `coopHistory`，避免破坏 22.4 的相似度矩阵计算。RecordCooperationTyped 同时调用 RecordCooperation 保持向后兼容。
2. **强化路径为纯内存计算**：基于当前 `coopRecords` 实时计算，不引入额外持久化。Daemon 重启后从零积累。
3. **DefaultReinforcementThreshold = 5**：硬编码为常量。测试验证 >= 边界行为。
4. **声誉分数依赖 21.3 ReputationStore**：直接复用 `GetSummary()`，不修改声誉系统。查询失败时默认 0.0。
5. **CLI 为顶级命令**：`rnix topology`（非 `rnix immune topology`），注册在 rootCmd 下。
6. **边聚合不重复**：双向协作（A->B 和 B->A）聚合为单条边，不产生重复。
7. **IPC 向后兼容**：新增 Method 和 Response 类型不影响旧客户端（IPC-005 验证）。

## Next Step

推荐执行 `dev-story` 工作流实现 Story 22.5，按 Implementation Checklist 中的 Task 顺序依次将测试从 RED 变为 GREEN。
