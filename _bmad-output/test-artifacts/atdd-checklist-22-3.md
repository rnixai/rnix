---
stepsCompleted: ['step-01-preflight-and-context', 'step-02-generation-mode', 'step-03-test-strategy', 'step-04-generate-tests', 'step-05-validate-and-complete']
lastStep: 'step-05-validate-and-complete'
lastSaved: '2026-03-14'
workflowType: 'testarch-atdd'
inputDocuments:
  - '_bmad-output/implementation-artifacts/22-3-security-status-management.md'
  - 'kernel/immune.go'
  - 'kernel/immune_test.go'
  - 'kernel/atdd_22_1_immune_daemon_test.go'
  - 'kernel/atdd_22_2_anomaly_detection_test.go'
  - 'ipc/protocol.go'
  - 'ipc/server.go'
  - 'ipc/atdd_22_2_anomaly_ipc_test.go'
  - 'cmd/rnix/immune.go'
  - 'cmd/rnix/immune_test.go'
  - 'cmd/rnix/atdd_22_2_anomaly_cmd_test.go'
---

# ATDD Checklist - Epic 22, Story 3: 安全状态管理

**Date:** 2026-03-14
**Author:** Decker
**Primary Test Level:** Unit + Integration (Go backend)

---

## Story Summary

通过 `rnix immune status` 查看完整的安全监控状态，包括 daemon 运行时间、安全态势摘要、已挂起进程列表、威胁记忆统计，以及 JSON 输出模式。

**As a** 平台构建者
**I want** 通过 `rnix immune status` 查看完整的安全监控状态
**So that** 我可以全面了解系统的安全态势

---

## Acceptance Criteria

1. **AC1: Daemon 状态和运行时间** - 显示 daemon 状态（running/stopped）和运行时间（Uptime）
2. **AC2: 当前告警列表** - 每项显示 PID、Agent 模板、异常类型、偏离程度和触发时间
3. **AC3: 已挂起进程及可用操作** - 每项显示 PID、异常原因和可用操作（resume / kill）
4. **AC4: 威胁记忆库条目数** - 显示威胁签名总数
5. **AC5: 综合安全态势总结** - 在顶部提供安全态势摘要行（如 "Security: OK" / "Security: 2 alerts, 1 suspended"）
6. **AC6: JSON 输出模式** - JSON 包含所有字段：daemon 状态、运行时间、告警列表、挂起进程、威胁计数、安全态势

---

## Failing Tests Created (RED Phase)

### Unit Tests - kernel/atdd_22_3_security_status_test.go (10 tests)

**File:** `kernel/atdd_22_3_security_status_test.go`

- **Test:** `TestImmuneDaemon_Uptime_Running` (22.3-UNIT-001)
  - **Status:** RED - ImmuneDaemon.Uptime 方法不存在
  - **Verifies:** AC1 - Start 后 Uptime > 0
  - **Priority:** P0

- **Test:** `TestImmuneDaemon_Uptime_NotRunning` (22.3-UNIT-002)
  - **Status:** RED - ImmuneDaemon.Uptime 方法不存在
  - **Verifies:** AC1 - 未 Start 时 Uptime == 0
  - **Priority:** P0

- **Test:** `TestImmuneDaemon_Uptime_Nil` (22.3-UNIT-003)
  - **Status:** RED - ImmuneDaemon.Uptime 方法不存在
  - **Verifies:** AC1 - nil daemon 返回 0
  - **Priority:** P0

- **Test:** `TestImmuneDaemon_Uptime_AfterStop` (22.3-UNIT-004)
  - **Status:** RED - ImmuneDaemon.Uptime 方法不存在
  - **Verifies:** AC1 - Stop 后 Uptime 归零
  - **Priority:** P0

- **Test:** `TestImmuneDaemon_SuspendedPIDs_Empty` (22.3-UNIT-005)
  - **Status:** RED - ImmuneDaemon.SuspendedPIDs 方法不存在
  - **Verifies:** AC3 - 无告警时返回空切片
  - **Priority:** P0

- **Test:** `TestImmuneDaemon_SuspendedPIDs_WithAlerts` (22.3-UNIT-006)
  - **Status:** RED - ImmuneDaemon.SuspendedPIDs 方法不存在
  - **Verifies:** AC3 - 有告警时返回正确 PID 列表
  - **Priority:** P0

- **Test:** `TestImmuneDaemon_SuspendedPIDs_Nil` (22.3-UNIT-007)
  - **Status:** RED - ImmuneDaemon.SuspendedPIDs 方法不存在
  - **Verifies:** AC3 - nil daemon 返回 nil
  - **Priority:** P0

- **Test:** `TestImmuneDaemon_SuspendedPIDs_AfterClear` (22.3-UNIT-008)
  - **Status:** RED - ImmuneDaemon.SuspendedPIDs 方法不存在
  - **Verifies:** AC3 - ClearAlert 后 PID 从列表中移除
  - **Priority:** P0

- **Test:** `TestImmuneDaemon_Uptime_Concurrent` (22.3-UNIT-009)
  - **Status:** RED - ImmuneDaemon.Uptime 方法不存在
  - **Verifies:** AC1 - 并发读取 Uptime 不竞态
  - **Priority:** P1

- **Test:** `TestImmuneDaemon_SuspendedPIDs_Concurrent` (22.3-UNIT-010)
  - **Status:** RED - ImmuneDaemon.SuspendedPIDs 方法不存在
  - **Verifies:** AC3 - 并发读取 SuspendedPIDs 不竞态
  - **Priority:** P1

### IPC Protocol Tests - ipc/atdd_22_3_security_status_ipc_test.go (5 tests)

**File:** `ipc/atdd_22_3_security_status_ipc_test.go`

- **Test:** `TestImmuneStatusResponse_UptimeMs` (22.3-IPC-001)
  - **Status:** RED - ImmuneStatusResponse 缺少 UptimeMs 字段
  - **Verifies:** AC1 - uptime_ms 字段序列化
  - **Priority:** P0

- **Test:** `TestImmuneStatusResponse_SuspendedPIDs` (22.3-IPC-002)
  - **Status:** RED - ImmuneStatusResponse 缺少 SuspendedPIDs 字段
  - **Verifies:** AC3 - suspended_pids 字段序列化
  - **Priority:** P0

- **Test:** `TestImmuneStatusResponse_SecurityStatus` (22.3-IPC-003)
  - **Status:** RED - ImmuneStatusResponse 缺少 SecurityStatus 字段
  - **Verifies:** AC5 - security_status 字段序列化
  - **Priority:** P0

- **Test:** `TestImmuneStatusResponse_AllNewFields` (22.3-IPC-004)
  - **Status:** RED - ImmuneStatusResponse 缺少新字段
  - **Verifies:** AC6 - JSON 包含所有 22.3 新增字段
  - **Priority:** P0

- **Test:** `TestImmuneStatusResponse_BackwardCompatible_22_3` (22.3-IPC-005)
  - **Status:** RED - ImmuneStatusResponse 缺少新字段
  - **Verifies:** AC6 - 旧格式 JSON 反序列化后新字段为零值
  - **Priority:** P1

### CLI Tests - cmd/rnix/atdd_22_3_security_status_cmd_test.go (10 tests)

**File:** `cmd/rnix/atdd_22_3_security_status_cmd_test.go`

- **Test:** `TestFormatUptime_Seconds` (22.3-CLI-001)
  - **Status:** RED - formatUptime 函数不存在
  - **Verifies:** AC1 - 秒级别格式化 "42s"
  - **Priority:** P0

- **Test:** `TestFormatUptime_Minutes` (22.3-CLI-002)
  - **Status:** RED - formatUptime 函数不存在
  - **Verifies:** AC1 - 分钟级别格式化 "5m30s"
  - **Priority:** P0

- **Test:** `TestFormatUptime_Hours` (22.3-CLI-003)
  - **Status:** RED - formatUptime 函数不存在
  - **Verifies:** AC1 - 小时级别格式化 "2h15m"
  - **Priority:** P0

- **Test:** `TestFormatUptime_Zero` (22.3-CLI-004)
  - **Status:** RED - formatUptime 函数不存在
  - **Verifies:** AC1 - 零值格式化 "0s"
  - **Priority:** P0

- **Test:** `TestFormatUptime_ExactMinute` (22.3-CLI-005)
  - **Status:** RED - formatUptime 函数不存在
  - **Verifies:** AC1 - 整分钟边界格式化
  - **Priority:** P1

- **Test:** `TestRunImmuneStatus_Uptime` (22.3-CLI-006)
  - **Status:** RED - formatUptime 函数不存在（编译失败）
  - **Verifies:** AC1 - immune status 命令路径包含 uptime 信息
  - **Priority:** P0

- **Test:** `TestRunImmuneStatus_SecurityOK` (22.3-CLI-007)
  - **Status:** RED - formatUptime 函数不存在（编译失败）
  - **Verifies:** AC5 - 无告警时显示 "Security: OK"
  - **Priority:** P0

- **Test:** `TestRunImmuneStatus_SecurityWarning` (22.3-CLI-008)
  - **Status:** RED - formatUptime 函数不存在（编译失败）
  - **Verifies:** AC5 - 有告警时显示具体数量
  - **Priority:** P0

- **Test:** `TestRunImmuneStatus_SuspendedProcesses` (22.3-CLI-009)
  - **Status:** RED - formatUptime 函数不存在（编译失败）
  - **Verifies:** AC3 - SUSPENDED PROCESSES 段落显示正确
  - **Priority:** P0

- **Test:** `TestRunImmuneStatus_JSON_FullFields` (22.3-CLI-010)
  - **Status:** RED - formatUptime 函数不存在（编译失败）
  - **Verifies:** AC6 - JSON 输出包含所有新增字段
  - **Priority:** P0

---

## Implementation Checklist

### Task 1: ImmuneDaemon 新增 Uptime 追踪 (kernel/immune.go)

**Tests to make pass:** 22.3-UNIT-001, 22.3-UNIT-002, 22.3-UNIT-003, 22.3-UNIT-004, 22.3-UNIT-009

- [ ] 在 `ImmuneDaemon` 结构体中新增 `startedAt time.Time` 字段
- [ ] 在 `Start()` 中记录 `d.startedAt = time.Now()`
- [ ] 在 `Stop()` 中重置 `d.startedAt = time.Time{}`（零值）
- [ ] 新增 `Uptime() time.Duration` 方法：若 running 返回 `time.Since(d.startedAt)`，否则返回 0
- [ ] nil receiver 检查：`d == nil` 时返回 0
- [ ] Run: `go test -race -run "TestImmuneDaemon_Uptime" ./kernel/...`

### Task 2: ImmuneDaemon 新增 SuspendedPIDs 方法 (kernel/immune.go)

**Tests to make pass:** 22.3-UNIT-005, 22.3-UNIT-006, 22.3-UNIT-007, 22.3-UNIT-008, 22.3-UNIT-010

- [ ] 新增 `SuspendedPIDs() []types.PID` 方法：返回 alerts map 中所有 PID
- [ ] nil receiver 检查：`d == nil` 时返回 nil
- [ ] 使用 RLock 保护并发读取
- [ ] Run: `go test -race -run "TestImmuneDaemon_SuspendedPIDs" ./kernel/...`

### Task 3: IPC 协议扩展 (ipc/protocol.go)

**Tests to make pass:** 22.3-IPC-001, 22.3-IPC-002, 22.3-IPC-003, 22.3-IPC-004, 22.3-IPC-005

- [ ] `ImmuneStatusResponse` 新增 `UptimeMs int64` 字段 (json:"uptime_ms")
- [ ] `ImmuneStatusResponse` 新增 `SuspendedPIDs []uint64` 字段 (json:"suspended_pids")
- [ ] `ImmuneStatusResponse` 新增 `SecurityStatus string` 字段 (json:"security_status")
- [ ] Run: `go test -race -run "TestImmuneStatusResponse_UptimeMs|TestImmuneStatusResponse_SuspendedPIDs|TestImmuneStatusResponse_SecurityStatus|TestImmuneStatusResponse_AllNewFields|TestImmuneStatusResponse_BackwardCompatible_22_3" ./ipc/...`

### Task 4: IPC Server 填充新字段 (ipc/server.go)

**Tests to make pass:** (集成测试 - 需 daemon 运行)

- [ ] `handleImmuneStatus` 填充 `UptimeMs`：`d.Uptime().Milliseconds()`
- [ ] `handleImmuneStatus` 填充 `SuspendedPIDs`：从 `d.SuspendedPIDs()` 转换 `types.PID → uint64`
- [ ] `handleImmuneStatus` 填充 `SecurityStatus`：
  - 无告警无挂起 = "ok"
  - 有告警或有挂起 = "warning"

### Task 5: CLI 输出增强 (cmd/rnix/immune.go)

**Tests to make pass:** 22.3-CLI-001 ~ 22.3-CLI-010

- [ ] 新增 `formatUptime(ms int64) string` 函数
  - < 60s: "42s"
  - < 60m: "5m30s"
  - >= 60m: "2h15m"
  - 0: "0s"
- [ ] 修改 `runImmuneStatus` 文本输出：
  - 第一行增加 uptime: `"Immune Daemon: running (uptime: 2h15m)"`
  - 新增安全态势摘要行: `"Security: OK"` / `"Security: 2 alerts, 1 suspended"`
  - 新增 SUSPENDED PROCESSES 段落
- [ ] Run: `go test -race -run "TestFormatUptime|TestRunImmuneStatus" ./cmd/rnix/...`

---

## Test Summary

| Category | Test File | Count | Priority |
|----------|----------|-------|----------|
| Unit (kernel) | `kernel/atdd_22_3_security_status_test.go` | 10 | 8 P0, 2 P1 |
| IPC | `ipc/atdd_22_3_security_status_ipc_test.go` | 5 | 4 P0, 1 P1 |
| CLI | `cmd/rnix/atdd_22_3_security_status_cmd_test.go` | 10 | 8 P0, 2 P1 |
| **Total** | | **25** | **20 P0, 5 P1** |

---

## AC Coverage Matrix

| AC | Description | Tests |
|----|------------|-------|
| AC1 | Daemon 状态和运行时间 | 22.3-UNIT-001, 002, 003, 004, 009; IPC-001; CLI-001, 002, 003, 004, 005, 006 |
| AC2 | 当前告警列表 | (已有 22.2 测试覆盖告警结构；22.3 CLI 输出测试复用) |
| AC3 | 已挂起进程及可用操作 | 22.3-UNIT-005, 006, 007, 008, 010; IPC-002; CLI-009 |
| AC4 | 威胁记忆库条目数 | (已有 22.2 测试覆盖 ThreatCount；22.3 不修改此逻辑) |
| AC5 | 综合安全态势总结 | IPC-003; CLI-007, 008 |
| AC6 | JSON 输出模式 | IPC-004, 005; CLI-010 |

---

## Key Risks & Assumptions

1. **Uptime 为纯内存值**：daemon 重启后 Uptime 重新计算，不从磁盘恢复。测试通过 Start/Stop 生命周期验证。
2. **SuspendedPIDs 从 alerts map 派生**：当前设计中 alerts map 中的 PID 即为被挂起的进程。无需额外持久化。
3. **SecurityStatus 计算规则简单明确**："ok" = 无告警无挂起，"warning" = 有告警或有挂起。测试通过 IPC 层验证字段存在性，CLI 层验证输出格式。
4. **向后兼容**：ImmuneStatusResponse 新增字段使用 Go 零值默认，旧版本 JSON 反序列化不受影响（IPC-005 验证）。
5. **formatUptime 独立于 formatDurationMs**：新增函数专门处理 daemon 运行时间格式化（"Xh Ym"），不修改现有 formatDurationMs 函数（用于 Profile duration 展示）。

## Next Step

推荐执行 `dev-story` 工作流实现 Story 22.3，按 Implementation Checklist 中的 Task 顺序依次将测试从 RED 变为 GREEN。
