# Story 22.3: 安全状态管理

Status: done

## Story

As a 平台构建者,
I want 通过 `rnix immune status` 查看完整的安全监控状态,
So that 我可以全面了解系统的安全态势。

## Acceptance Criteria

1. **AC1: Daemon 状态和运行时间**
   - Given Immune Daemon 运行中
   - When 用户执行 `rnix immune status`
   - Then 显示 daemon 状态（running/stopped）和运行时间（Uptime）

2. **AC2: 当前告警列表**
   - Given 存在活跃的异常告警
   - When 状态输出中显示告警
   - Then 每项显示 PID、Agent 模板、异常类型、偏离程度和触发时间

3. **AC3: 已挂起进程及可用操作**
   - Given 存在已挂起的进程
   - When 状态输出中显示挂起项
   - Then 每项显示 PID、异常原因和可用操作（resume / kill）

4. **AC4: 威胁记忆库条目数**
   - Given 威胁记忆库有记录
   - When 状态输出中显示威胁统计
   - Then 显示威胁签名总数

5. **AC5: 综合安全态势总结**
   - Given 状态输出完成
   - When 输出汇总信息
   - Then 在顶部提供安全态势摘要行（如 "Security: OK" / "Security: 2 alerts, 1 suspended"）

6. **AC6: JSON 输出模式**
   - Given 用户执行 `rnix immune status --json`
   - When 输出 JSON 格式
   - Then 包含所有字段：daemon 状态、运行时间、告警列表、挂起进程、威胁计数、安全态势

## Tasks / Subtasks

### Task 1: ImmuneDaemon 新增 Uptime 追踪（AC: #1）

- [x] 1.1 在 `kernel/immune.go` 的 `ImmuneDaemon` 结构体中新增 `startedAt time.Time` 字段
- [x] 1.2 在 `Start()` 中记录 `d.startedAt = time.Now()`
- [x] 1.3 新增 `Uptime() time.Duration` 方法：若 running 返回 `time.Since(d.startedAt)`，否则返回 0
- [x] 1.4 单元测试：
  - `TestImmuneDaemon_Uptime_Running` -- Start 后 Uptime > 0
  - `TestImmuneDaemon_Uptime_NotRunning` -- 未 Start 时 Uptime == 0
  - `TestImmuneDaemon_Uptime_Nil` -- nil daemon 返回 0

### Task 2: ImmuneDaemon 新增 SuspendedPIDs 方法（AC: #3）

- [x] 2.1 新增 `SuspendedPIDs() []types.PID` 方法：返回所有 alerts map 中的 PID（即被挂起的进程）
- [x] 2.2 单元测试：
  - `TestImmuneDaemon_SuspendedPIDs_Empty` -- 无告警时返回空
  - `TestImmuneDaemon_SuspendedPIDs_WithAlerts` -- 有告警时返回正确 PID 列表

### Task 3: IPC 协议扩展（AC: #1, #5, #6）

- [x] 3.1 扩展 `ipc/protocol.go` 的 `ImmuneStatusResponse`：

  ```go
  type ImmuneStatusResponse struct {
      Running      bool                             `json:"running"`
      UptimeMs     int64                            `json:"uptime_ms"`     // 新增：运行时间（毫秒）
      ProfileCount int                              `json:"profile_count"`
      Profiles     map[string]*kernel.NormalProfile  `json:"profiles"`
      ActivePIDs   []uint64                          `json:"active_pids"`
      SuspendedPIDs []uint64                         `json:"suspended_pids"` // 新增：已挂起的 PID
      Alerts       []AlertWire                       `json:"alerts"`
      ThreatCount  int                              `json:"threat_count"`
      SecurityStatus string                          `json:"security_status"` // 新增："ok" / "warning" / "critical"
  }
  ```

- [x] 3.2 修改 `ipc/server.go` 的 `handleImmuneStatus`：
  - 填充 `UptimeMs`：`d.Uptime().Milliseconds()`
  - 填充 `SuspendedPIDs`：从 `d.SuspendedPIDs()` 转换
  - 填充 `SecurityStatus`：根据告警数和挂起数计算
    - 无告警无挂起 = "ok"
    - 有告警或有挂起 = "warning"

- [x] 3.3 单元测试：
  - `TestImmuneStatusResponse_UptimeMs` -- 验证 uptime_ms 字段序列化
  - `TestImmuneStatusResponse_SecurityStatus` -- 验证 security_status 字段

### Task 4: CLI 输出增强（AC: #1, #2, #3, #4, #5）

- [x] 4.1 修改 `cmd/rnix/immune.go` 的 `runImmuneStatus` 文本输出格式：

  **增强后的终端输出格式：**
  ```
  Immune Daemon: running (uptime: 2h15m)
  Security: OK
  Profiles: 3
  Active Monitors: 2
  Threat Memory: 5 signatures

  AGENT TEMPLATE       SAMPLES  TOKEN RATE (avg)  DURATION (avg)  LAST UPDATED
  code-analyst              12         2.5 tok/s          15.2s  2026-03-14
  ...

  ALERTS (1):
    PID 42: syscall_freq - Open 频率 12.0 是基线 2.4 的 5.0 倍 (2026-03-14 10:30:00)
      Actions: rnix immune resume 42 | rnix kill 42

  SUSPENDED PROCESSES (1):
    PID 42 — syscall_freq anomaly (use: rnix immune resume 42 | rnix kill 42)
  ```

  **无告警时的输出：**
  ```
  Immune Daemon: running (uptime: 2h15m)
  Security: OK
  Profiles: 3
  Active Monitors: 2
  Threat Memory: 0 signatures

  AGENT TEMPLATE       SAMPLES  TOKEN RATE (avg)  DURATION (avg)  LAST UPDATED
  code-analyst              12         2.5 tok/s          15.2s  2026-03-14
  ```

- [x] 4.2 新增 `formatUptime(ms int64) string` 辅助函数：
  - < 60s: "42s"
  - < 60m: "5m30s"
  - >= 60m: "2h15m"

- [x] 4.3 新增安全态势总结行：
  - "Security: OK" -- 无告警无挂起
  - "Security: 2 alerts, 1 suspended" -- 有告警/挂起时显示具体数量

- [x] 4.4 单元测试：
  - `TestRunImmuneStatus_Uptime` -- 输出包含 uptime 信息
  - `TestRunImmuneStatus_SecurityOK` -- 无告警时显示 "Security: OK"
  - `TestRunImmuneStatus_SecurityWarning` -- 有告警时显示具体数量
  - `TestRunImmuneStatus_SuspendedProcesses` -- 挂起进程段落显示正确
  - `TestFormatUptime` -- 格式化函数覆盖各时间范围
  - `TestRunImmuneStatus_JSON_FullFields` -- JSON 输出包含所有新增字段

## Dev Notes

### 核心设计决策

**增量扩展，非重写。** 本 Story 基于 22.1 和 22.2 已实现的 `immune status` 功能，仅做增量增强：
1. 新增 Uptime 追踪（daemon 运行时间）
2. 新增 SecurityStatus 摘要行（安全态势一目了然）
3. 新增 SuspendedPIDs 显式列表（明确区分"有告警"和"被挂起"）
4. 增强文本输出格式（更完整的信息展示）

**告警与挂起进程的关系。** 当前实现中 alerts map 中的 PID 即为被挂起的进程（检测到异常 → 创建 alert → 调用 suspendFn 挂起）。SuspendedPIDs 方法从 alerts map 中提取 PID 列表。

**SecurityStatus 计算规则：**
- `"ok"` -- alerts 为空且 suspendedPIDs 为空
- `"warning"` -- 有 alerts 或有 suspended PIDs

**Uptime 实现为纯内存值，不持久化。** daemon 重启后 Uptime 重新计算，不从磁盘恢复。

### 架构合规

- **依赖方向**：`kernel/immune.go` 仅使用标准库 + `internal/types`（不引入新依赖）
- **包边界**：在现有 `kernel/immune.go` 文件内扩展，不新增文件
- **IPC 扩展**：仅扩展现有 `ImmuneStatusResponse` 结构体字段（向后兼容，新字段有零值默认）
- **CLI 修改**：仅修改 `cmd/rnix/immune.go` 中的 `runImmuneStatus` 函数
- **JSON 输出**：新增字段使用 `snake_case` 命名（`uptime_ms`、`suspended_pids`、`security_status`）
- **nil 保护**：新增方法检查 `d == nil`，ImmuneDaemon 为 nil 时返回安全默认值

### 关键文件变更

| 文件 | 变更类型 | 说明 |
|------|---------|------|
| `kernel/immune.go` | 修改 | 新增 `startedAt` 字段、`Uptime()` 方法、`SuspendedPIDs()` 方法 |
| `kernel/immune_test.go` | 修改 | 新增 Uptime 和 SuspendedPIDs 单元测试 |
| `ipc/protocol.go` | 修改 | ImmuneStatusResponse 新增 `UptimeMs`、`SuspendedPIDs`、`SecurityStatus` 字段 |
| `ipc/server.go` | 修改 | handleImmuneStatus 填充新字段 |
| `cmd/rnix/immune.go` | 修改 | 输出格式增强：uptime、security 摘要、suspended 段落、formatUptime 函数 |
| `cmd/rnix/immune_test.go` | 修改 | 新增增强输出相关测试 |

### 复用模式

- **ImmuneDaemon 方法模式**：复用 22.1/22.2 的 nil 检查 + mutex 锁模式
- **ImmuneStatusResponse 扩展**：新增字段有零值默认——不影响旧客户端解析
- **CLI 输出模式**：复用 `runImmuneStatus` 现有的文本/JSON 双模式输出结构
- **formatDurationMs**：现有函数可复用于 Profile 输出，新增 `formatUptime` 用于 daemon 运行时间

### 从 Story 22.2 继承的经验

- **nil 保护是关键**：22.2 所有公开方法检查 `d == nil`——本 Story 新增方法同样遵守
- **ActivePIDs 类型需对齐**：22.2 中 `types.PID` → `uint64` 转换模式，SuspendedPIDs 同样适用
- **ImmuneStatusResponse 向后兼容**：22.2 新增 Alerts/ThreatCount 不影响 22.1 客户端——本 Story 新增字段同样保持兼容
- **测试使用 cmd.OutOrStdout() 捕获**：22.2 CLI 测试通过 `bytes.Buffer` 替换 stdout 验证输出格式

### 组合矩阵

| 本功能 | 交互对象 | 交互方式 | 需验证 |
|--------|----------|----------|--------|
| Uptime() | ImmuneDaemon.Start/Stop (22.1) | 集成：Start 时记录 startedAt，Stop 后 Uptime 返回 0 | 是 |
| SuspendedPIDs() | ImmuneDaemon.alerts map (22.2) | 依赖：从 alerts map 提取 PID 列表 | 是 |
| SecurityStatus | Alerts + SuspendedPIDs | 派生：根据两者是否为空计算状态 | 是 |
| ImmuneStatusResponse 扩展 | IPC immune_status (22.1/22.2) | 扩展：新增 3 个字段，不影响现有字段 | 是 |
| CLI 输出格式 | cmd/rnix/immune.go (22.1/22.2) | 增强：在现有输出基础上添加 uptime/security/suspended 段落 | 是 |
| formatUptime | formatDurationMs (22.1) | 独立：新增函数，不修改现有函数 | 否 |

### Project Structure Notes

- `kernel/immune.go` 在现有文件内扩展——不新建文件，保持 22.1/22.2 的单文件结构
- `cmd/rnix/immune.go` 在现有文件内修改输出格式
- 无新增持久化文件——Uptime 是纯内存值
- 测试文件与源文件同目录——遵循 Go 惯例

### References

- [Source: _bmad-output/planning-artifacts/epics/epic-22-适应性安全与自愈-adaptive-security-self-healing.md#Story 22.3]
- [Source: _bmad-output/planning-artifacts/prd/functional-requirements.md#FR133] -- immune status 查看安全监控状态
- [Source: _bmad-output/implementation-artifacts/22-2-anomaly-detection-and-threat-memory.md] -- Story 22.2 实现和经验
- [Source: kernel/immune.go] -- ImmuneDaemon 现有实现（含 GetAlerts、GetThreats、IsRunning 等方法）
- [Source: ipc/protocol.go#ImmuneStatusResponse] -- 现有 IPC 响应类型（含 AlertWire、ThreatCount）
- [Source: ipc/server.go#handleImmuneStatus] -- 现有 server handler
- [Source: cmd/rnix/immune.go] -- 现有 CLI 命令结构（含 runImmuneStatus、formatDurationMs）
- [Source: _bmad-output/project-context.md#IPC 扩展标准步骤] -- IPC 扩展规范

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6

### Debug Log References

### Completion Notes List

- Task 1: 在 `ImmuneDaemon` 结构体中新增 `startedAt time.Time` 字段，在 `Start()` 中记录启动时间，新增 `Uptime()` 方法（含 nil 保护和 running 检查）。ATDD 测试覆盖：Running/NotRunning/Nil/AfterStop/Concurrent 共 5 个测试。
- Task 2: 新增 `SuspendedPIDs()` 方法，从 alerts map 中提取被挂起进程的 PID 列表。ATDD 测试覆盖：Empty/WithAlerts/Nil/AfterClear/Concurrent 共 5 个测试。
- Task 3: 扩展 `ImmuneStatusResponse` 新增 `UptimeMs`、`SuspendedPIDs`、`SecurityStatus` 三个字段。修改 `handleImmuneStatus` 填充新字段，SecurityStatus 根据告警和挂起数计算（无告警="ok"，有告警="warning"）。nil daemon 场景返回安全默认值。ATDD 测试覆盖序列化、向后兼容共 5 个测试。
- Task 4: 增强 CLI 文本输出：daemon 行增加 uptime 显示、新增 Security 摘要行、新增 SUSPENDED PROCESSES 段落。新增 `formatUptime()` 和 `securitySummary()` 辅助函数。ATDD 测试 + 单元测试覆盖 formatUptime 各时间范围、securitySummary 各组合。
- 修复 ATDD 测试文件中的 lint 问题：`sync.WaitGroup` 使用 Go 1.26 的 `wg.Go()` 模式替代 `wg.Add(1)+go func()+defer wg.Done()` 模式。
- 修复 ATDD 测试中 `runImmuneStatus == nil` 比较导致的编译错误。

### File List

- `kernel/immune.go` — 修改：新增 `startedAt` 字段、`Uptime()` 方法、`SuspendedPIDs()` 方法
- `kernel/immune_test.go` — 修改：保留包声明（具体测试由 ATDD 文件覆盖）
- `kernel/atdd_22_3_security_status_test.go` — 修改：修复 lint（wg.Go 替代 wg.Add+go）
- `ipc/protocol.go` — 修改：ImmuneStatusResponse 新增 UptimeMs、SuspendedPIDs、SecurityStatus 字段
- `ipc/server.go` — 修改：handleImmuneStatus 填充新字段（uptime、suspended PIDs、security status）
- `cmd/rnix/immune.go` — 修改：输出格式增强（uptime、security 摘要、suspended 段落、formatUptime、securitySummary 函数）
- `cmd/rnix/immune_test.go` — 修改：新增 TestFormatUptime、TestSecuritySummary 单元测试
- `cmd/rnix/atdd_22_3_security_status_cmd_test.go` — 修改：修复函数比较编译错误
- `ipc/atdd_22_3_security_status_ipc_test.go` — 新增：IPC 层 ATDD 测试（UptimeMs、SuspendedPIDs、SecurityStatus 序列化和向后兼容）

## Change Log

- 2026-03-14: Story 22.3 实现完成 — 安全状态管理增量增强（Uptime 追踪、SuspendedPIDs 方法、SecurityStatus 摘要、CLI 输出增强）。全部 4 个 Task 完成，20 个包全部通过测试（含 race 检测），lint 0 issue，构建成功。
- 2026-03-14: Code Review (AI) — 通过。1 个 MEDIUM 问题（File List 缺少 ipc/atdd_22_3_security_status_ipc_test.go）已修复。所有 AC 验证通过，代码质量良好，nil 安全、线程安全、向后兼容。状态更新为 done。
