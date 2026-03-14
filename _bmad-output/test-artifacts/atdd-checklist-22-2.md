---
stepsCompleted: ['step-01-preflight-and-context', 'step-02-generation-mode', 'step-03-test-strategy', 'step-04-generate-tests', 'step-05-validate-and-complete']
lastStep: 'step-05-validate-and-complete'
lastSaved: '2026-03-14'
workflowType: 'testarch-atdd'
inputDocuments:
  - '_bmad-output/implementation-artifacts/22-2-anomaly-detection-and-threat-memory.md'
  - 'kernel/immune.go'
  - 'kernel/immune_test.go'
  - 'kernel/atdd_22_1_immune_daemon_test.go'
  - 'ipc/protocol.go'
  - 'ipc/server.go'
  - 'ipc/client.go'
  - 'cmd/rnix/immune.go'
  - 'cmd/rnix/immune_test.go'
  - 'internal/types/types.go'
---

# ATDD Checklist - Epic 22, Story 2: 异常检测与威胁记忆

**Date:** 2026-03-14
**Author:** Decker
**Primary Test Level:** Unit + Integration (Go backend)

---

## Story Summary

系统在智能体行为偏离基线时自动告警和挂起，并记忆已知威胁模式。包括异常检测与自动挂起、威胁记忆库（Antibody Memory）、进程恢复/终止、异常类型和偏离程度展示、威胁记忆持久化和向后兼容。

**As a** 平台构建者
**I want** 系统在智能体行为偏离基线时自动告警和挂起，并记忆已知威胁模式
**So that** 异常行为被及时拦截，已知威胁不需要重新检测

---

## Acceptance Criteria

1. **AC1: 异常检测与自动挂起** - 行为偏离基线超过阈值时触发告警并自动挂起（suspend）进程
2. **AC2: 威胁记忆库（Antibody Memory）** - 已识别的异常行为模式记录到威胁记忆库，后续相同模式立即拦截
3. **AC3: 进程恢复/终止** - 通过 `rnix immune resume <pid>` 恢复或 `rnix kill <pid>` 终止挂起进程
4. **AC4: 异常类型和偏离程度展示** - 告警包含异常类型、具体指标值、偏离倍数、被挂起的 PID
5. **AC5: 威胁记忆持久化** - daemon 重启后自动加载已有的威胁记忆数据
6. **AC6: 向后兼容** - 不影响现有 ImmuneDaemon 功能（Story 22.1）

---

## Failing Tests Created (RED Phase)

### Unit Tests - kernel/atdd_22_2_anomaly_detection_test.go (25 tests)

**File:** `kernel/atdd_22_2_anomaly_detection_test.go`

- **Test:** `TestAnomalyAlert_JSONRoundTrip` (22.2-UNIT-001)
  - **Status:** RED - AnomalyAlert 类型不存在
  - **Verifies:** AC1/AC4 - JSON 序列化/反序列化完整性，snake_case 字段名
  - **Priority:** P0

- **Test:** `TestAnomalyType_Constants` (22.2-UNIT-002)
  - **Status:** RED - AnomalyType 常量不存在
  - **Verifies:** AC4 - AnomalySyscallFreq/AnomalyTokenRate/AnomalyDeviceAccess 常量值正确
  - **Priority:** P0

- **Test:** `TestThreatSignature_JSONRoundTrip` (22.2-UNIT-003)
  - **Status:** RED - ThreatSignature 类型不存在
  - **Verifies:** AC2/AC5 - JSON 序列化/反序列化完整性
  - **Priority:** P0

- **Test:** `TestImmuneStore_SaveAndLoadThreats` (22.2-UNIT-004)
  - **Status:** RED - ImmuneStore.SaveThreat/LoadThreats 不存在
  - **Verifies:** AC2/AC5 - 写入 3 条威胁签名，读回全部
  - **Priority:** P0

- **Test:** `TestImmuneStore_LoadThreats_Empty` (22.2-UNIT-005)
  - **Status:** RED - ImmuneStore.LoadThreats 不存在
  - **Verifies:** AC5 - 文件不存在返回空切片
  - **Priority:** P0

- **Test:** `TestImmuneStore_ThreatsJSONLinesFormat` (22.2-UNIT-006)
  - **Status:** RED - ImmuneStore.SaveThreat 不存在
  - **Verifies:** AC5 - 威胁文件使用 JSON Lines 格式
  - **Priority:** P0

- **Test:** `TestDefaultDeviationThreshold_Value` (22.2-UNIT-007)
  - **Status:** RED - DefaultDeviationThreshold 常量不存在
  - **Verifies:** AC1 - 默认偏离阈值为 3.0
  - **Priority:** P0

- **Test:** `TestAnomalyDetector_SyscallNormal` (22.2-UNIT-008)
  - **Status:** RED - NewAnomalyDetector 函数不存在
  - **Verifies:** AC1 - 正常范围内的 syscall 不报异常
  - **Priority:** P0

- **Test:** `TestAnomalyDetector_SyscallAnomaly` (22.2-UNIT-009)
  - **Status:** RED - NewAnomalyDetector 函数不存在
  - **Verifies:** AC1/AC4 - 超出阈值的 syscall 触发告警，包含类型和偏离倍数
  - **Priority:** P0

- **Test:** `TestAnomalyDetector_TokenRateNormal` (22.2-UNIT-010)
  - **Status:** RED - NewAnomalyDetector 函数不存在
  - **Verifies:** AC1 - 正常 token 速率不报异常
  - **Priority:** P0

- **Test:** `TestAnomalyDetector_TokenRateAnomaly` (22.2-UNIT-011)
  - **Status:** RED - NewAnomalyDetector 函数不存在
  - **Verifies:** AC1/AC4 - 超出阈值的 token 速率触发告警
  - **Priority:** P0

- **Test:** `TestAnomalyDetector_NoProfile` (22.2-UNIT-012)
  - **Status:** RED - NewAnomalyDetector 函数不存在
  - **Verifies:** AC1 - Profile 为 nil 时返回 nil（无法检测）
  - **Priority:** P1

- **Test:** `TestAnomalyDetector_ZeroMean` (22.2-UNIT-013)
  - **Status:** RED - NewAnomalyDetector 函数不存在
  - **Verifies:** AC1 - 均值为 0 时不误报、不 panic
  - **Priority:** P1

- **Test:** `TestAnomalyDetector_MatchThreat` (22.2-UNIT-014)
  - **Status:** RED - NewAnomalyDetector 函数不存在
  - **Verifies:** AC2 - 匹配已知威胁签名（相同 template + type + metric）
  - **Priority:** P0

- **Test:** `TestAnomalyDetector_NoMatchThreat` (22.2-UNIT-015)
  - **Status:** RED - NewAnomalyDetector 函数不存在
  - **Verifies:** AC2 - 不匹配的签名返回 nil
  - **Priority:** P1

- **Test:** `TestImmuneDaemon_AnomalyDetection` (22.2-UNIT-016)
  - **Status:** RED - ImmuneDaemon.SetSuspendFunc/GetAlerts 不存在
  - **Verifies:** AC1/AC3 - 行为超出基线时自动挂起进程
  - **Priority:** P0

- **Test:** `TestImmuneDaemon_ThreatMemoryMatch` (22.2-UNIT-017)
  - **Status:** RED - ImmuneDaemon.SetSuspendFunc 不存在
  - **Verifies:** AC2 - 已知威胁签名立即拦截（无需重新检测）
  - **Priority:** P0

- **Test:** `TestImmuneDaemon_NoProfileNoDetection` (22.2-UNIT-018)
  - **Status:** RED - ImmuneDaemon.SetSuspendFunc 不存在
  - **Verifies:** AC6 - 无 Profile 时不检测、不挂起
  - **Priority:** P1

- **Test:** `TestImmuneDaemon_ClearAlert` (22.2-UNIT-019)
  - **Status:** RED - ImmuneDaemon.ClearAlert/GetAlerts 不存在
  - **Verifies:** AC3 - 清除告警后不再持有该 PID 告警
  - **Priority:** P0

- **Test:** `TestImmuneDaemon_GetAlerts` (22.2-UNIT-020)
  - **Status:** RED - ImmuneDaemon.GetAlerts 不存在
  - **Verifies:** AC4 - 返回当前所有活跃告警
  - **Priority:** P0

- **Test:** `TestImmuneDaemon_GetThreats` (22.2-UNIT-021)
  - **Status:** RED - ImmuneDaemon.GetThreats 不存在
  - **Verifies:** AC2 - 返回所有已知威胁签名
  - **Priority:** P0

- **Test:** `TestImmuneDaemon_ThreatPersistence` (22.2-UNIT-022)
  - **Status:** RED - ImmuneDaemon.GetThreats 不存在
  - **Verifies:** AC5 - daemon 重启后加载已有威胁签名
  - **Priority:** P0

- **Test:** `TestImmuneDaemon_SuspendFnNil` (22.2-UNIT-023)
  - **Status:** RED - ImmuneDaemon.SetSuspendFunc 不存在
  - **Verifies:** AC6 - suspendFn 为 nil 时不 panic
  - **Priority:** P1

- **Test:** `TestImmuneDaemon_NilSafe_NewMethods` (22.2-UNIT-024)
  - **Status:** RED - ImmuneDaemon.GetAlerts/GetThreats/ClearAlert/SetSuspendFunc 不存在
  - **Verifies:** AC6 - nil daemon 时新方法不 panic
  - **Priority:** P0

- **Test:** `TestImmuneDaemon_ConcurrentAnomalyDetection` (22.2-UNIT-025)
  - **Status:** RED - ImmuneDaemon.SetSuspendFunc 不存在
  - **Verifies:** AC1 - 多进程并发异常检测不竞态
  - **Priority:** P1

### IPC Protocol Tests - ipc/atdd_22_2_anomaly_ipc_test.go (7 tests)

**File:** `ipc/atdd_22_2_anomaly_ipc_test.go`

- **Test:** `TestMethodImmuneResume_Constant` (22.2-IPC-001)
  - **Status:** RED - MethodImmuneResume 常量不存在
  - **Verifies:** AC3 - 常量值为 "immune_resume"
  - **Priority:** P0

- **Test:** `TestImmuneResumeRequest_JSON` (22.2-IPC-002)
  - **Status:** RED - ImmuneResumeRequest 类型不存在
  - **Verifies:** AC3 - 请求类型编译通过，JSON 包含 pid 字段
  - **Priority:** P0

- **Test:** `TestImmuneResumeResponse_JSON` (22.2-IPC-003)
  - **Status:** RED - ImmuneResumeResponse 类型不存在
  - **Verifies:** AC3 - 响应包含 ok 和 message 字段
  - **Priority:** P0

- **Test:** `TestAlertWire_JSON` (22.2-IPC-004)
  - **Status:** RED - AlertWire 类型不存在
  - **Verifies:** AC4 - AlertWire 包含 pid、agent_template、type、detail、deviation、timestamp_ms 字段
  - **Priority:** P0

- **Test:** `TestImmuneStatusResponse_ExtendedFields` (22.2-IPC-005)
  - **Status:** RED - ImmuneStatusResponse 缺少 Alerts 和 ThreatCount 字段
  - **Verifies:** AC4 - ImmuneStatusResponse 扩展包含 alerts 和 threat_count 字段
  - **Priority:** P0

- **Test:** `TestImmuneStatusResponse_BackwardCompatible` (22.2-IPC-006)
  - **Status:** RED - ImmuneStatusResponse 缺少 Alerts 字段
  - **Verifies:** AC6 - 旧格式 JSON 反序列化后新字段为零值
  - **Priority:** P1

- **Test:** `TestClient_ImmuneResume_MethodExists` (22.2-IPC-007)
  - **Status:** RED - Client.ImmuneResume 方法不存在
  - **Verifies:** AC3 - 客户端方法签名正确
  - **Priority:** P1

### CLI Tests - cmd/rnix/atdd_22_2_anomaly_cmd_test.go (5 tests)

**File:** `cmd/rnix/atdd_22_2_anomaly_cmd_test.go`

- **Test:** `TestRunImmuneResume_Success` (22.2-CLI-001)
  - **Status:** RED - immuneResumeCmd 不存在
  - **Verifies:** AC3 - resume 命令 Use 和 Short 正确、要求 ExactArgs(1)
  - **Priority:** P0

- **Test:** `TestRunImmuneResume_NoDaemon` (22.2-CLI-002)
  - **Status:** RED - immuneResumeCmd 不存在
  - **Verifies:** AC3 - daemon 未运行时输出错误
  - **Priority:** P0

- **Test:** `TestRunImmuneStatus_WithAlerts` (22.2-CLI-003)
  - **Status:** RED - immuneResumeCmd 不存在（无法验证注册）
  - **Verifies:** AC4 - immune 命令包含 resume 子命令
  - **Priority:** P1

- **Test:** `TestRunImmuneStatus_JSONWithAlerts` (22.2-CLI-004)
  - **Status:** RED - immuneResumeCmd 不存在
  - **Verifies:** AC4 - JSON 模式输出正确格式
  - **Priority:** P1

- **Test:** `TestImmuneResumeCmd_Registered` (22.2-CLI-005)
  - **Status:** RED - immuneResumeCmd 不存在
  - **Verifies:** AC3 - immune 命令注册 resume 子命令
  - **Priority:** P0

---

## Implementation Checklist

### Task 1: AnomalyType 和 AnomalyAlert 数据类型 (kernel/immune.go)

**Tests to make pass:** 22.2-UNIT-001, 22.2-UNIT-002

- [ ] 定义 `AnomalyType` 类型和 `AnomalySyscallFreq/AnomalyTokenRate/AnomalyDeviceAccess` 常量
- [ ] 定义 `AnomalyAlert` 结构体（含 JSON tags snake_case）
- [ ] Run: `go test -race -run "TestAnomalyAlert|TestAnomalyType" ./kernel/...`

### Task 2: ThreatSignature 和 AntibodyMemory 持久化 (kernel/immune.go)

**Tests to make pass:** 22.2-UNIT-003 ~ 22.2-UNIT-006

- [ ] 定义 `ThreatSignature` 结构体（含 JSON tags snake_case）
- [ ] 实现 `ImmuneStore.SaveThreat(sig ThreatSignature) error`（JSON Lines 追加写入 threats.jsonl）
- [ ] 实现 `ImmuneStore.LoadThreats() ([]ThreatSignature, error)`
- [ ] Run: `go test -race -run "TestThreatSignature|TestImmuneStore_SaveAndLoadThreats|TestImmuneStore_LoadThreats_Empty|TestImmuneStore_ThreatsJSONLines" ./kernel/...`

### Task 3: AnomalyDetector 异常检测引擎 (kernel/immune.go)

**Tests to make pass:** 22.2-UNIT-007 ~ 22.2-UNIT-015

- [ ] 定义 `DefaultDeviationThreshold = 3.0` 常量
- [ ] 实现 `NewAnomalyDetector(threshold float64) *AnomalyDetector`
- [ ] 实现 `CheckSyscallAnomaly(pid, agentTemplate, syscallName, currentCount, profile) *AnomalyAlert`
- [ ] 实现 `CheckTokenRateAnomaly(pid, agentTemplate, currentRate, profile) *AnomalyAlert`
- [ ] 实现 `MatchThreat(agentTemplate, anomalyType, metric, threats) *ThreatSignature`
- [ ] Run: `go test -race -run "TestDefaultDeviationThreshold|TestAnomalyDetector" ./kernel/...`

### Task 4: ImmuneDaemon 异常检测集成 (kernel/immune.go)

**Tests to make pass:** 22.2-UNIT-016 ~ 22.2-UNIT-025

- [ ] 扩展 `ImmuneDaemon` 结构体：新增 detector、threats、alerts、suspendFn 字段
- [ ] 实现 `SetSuspendFunc(fn func(pid types.PID) error)`
- [ ] 修改 `Start()`：加载威胁签名
- [ ] 修改 `OnSyscallEvent()`：在 Observe 后执行异常检测和威胁匹配
- [ ] 实现 `GetAlerts() map[types.PID]*AnomalyAlert`
- [ ] 实现 `ClearAlert(pid types.PID)`
- [ ] 实现 `GetThreats() []ThreatSignature`
- [ ] 所有新方法 nil receiver 保护
- [ ] Run: `go test -race -run "TestImmuneDaemon_AnomalyDetection|TestImmuneDaemon_ThreatMemoryMatch|TestImmuneDaemon_NoProfileNoDetection|TestImmuneDaemon_ClearAlert|TestImmuneDaemon_GetAlerts|TestImmuneDaemon_GetThreats|TestImmuneDaemon_ThreatPersistence|TestImmuneDaemon_SuspendFnNil|TestImmuneDaemon_NilSafe_NewMethods|TestImmuneDaemon_ConcurrentAnomalyDetection" ./kernel/...`

### Task 5: IPC 协议扩展 (ipc/)

**Tests to make pass:** 22.2-IPC-001 ~ 22.2-IPC-007

- [ ] protocol.go: 新增 `MethodImmuneResume` 常量
- [ ] protocol.go: 新增 `ImmuneResumeRequest/ImmuneResumeResponse` 类型
- [ ] protocol.go: 新增 `AlertWire` 类型
- [ ] protocol.go: 扩展 `ImmuneStatusResponse`（新增 Alerts 和 ThreatCount 字段）
- [ ] server.go: 新增 `handleImmuneResume` handler + dispatch 注册
- [ ] server.go: 修改 `handleImmuneStatus` 以包含 Alerts 和 ThreatCount
- [ ] client.go: 新增 `ImmuneResume(pid uint64) (*ImmuneResumeResponse, error)`
- [ ] Run: `go test -race -run "TestMethodImmuneResume|TestImmuneResumeRequest|TestImmuneResumeResponse|TestAlertWire|TestImmuneStatusResponse_ExtendedFields|TestImmuneStatusResponse_BackwardCompatible|TestClient_ImmuneResume" ./ipc/...`

### Task 6: CLI 命令扩展 (cmd/rnix/immune.go)

**Tests to make pass:** 22.2-CLI-001 ~ 22.2-CLI-005

- [ ] 新增 `immuneResumeCmd` 变量和 `runImmuneResume` 函数
- [ ] 在 `init()` 中注册 `immuneResumeCmd` 到 `immuneCmd`
- [ ] 修改 `runImmuneStatus` 输出显示 ALERTS 段落和 Threat Memory 计数
- [ ] Run: `go test -race -run "TestRunImmuneResume|TestRunImmuneStatus_WithAlerts|TestRunImmuneStatus_JSONWithAlerts|TestImmuneResumeCmd_Registered" ./cmd/rnix/...`

### Task 7: Kernel 集成 (cmd/rnix/main.go)

- [ ] 在 runDaemon 中创建 `AnomalyDetector` 实例（使用 `DefaultDeviationThreshold`）
- [ ] 将 detector 注入到 `ImmuneDaemon`
- [ ] 设置 `suspendFn` 回调为 `kernel.Kill(pid, types.SIGPAUSE)` 闭包
- [ ] 确认现有功能不受影响（回归验证）

---

## Test Summary

| Category | Test File | Count | Priority |
|----------|----------|-------|----------|
| Unit (kernel) | `kernel/atdd_22_2_anomaly_detection_test.go` | 25 | 16 P0, 9 P1 |
| IPC | `ipc/atdd_22_2_anomaly_ipc_test.go` | 7 | 5 P0, 2 P1 |
| CLI | `cmd/rnix/atdd_22_2_anomaly_cmd_test.go` | 5 | 3 P0, 2 P1 |
| **Total** | | **37** | **24 P0, 13 P1** |

---

## AC Coverage Matrix

| AC | Description | Tests |
|----|------------|-------|
| AC1 | 异常检测与自动挂起 | 22.2-UNIT-001, 007, 008, 009, 010, 011, 012, 013, 016, 025 |
| AC2 | 威胁记忆库 | 22.2-UNIT-003, 004, 005, 014, 015, 017, 021 |
| AC3 | 进程恢复/终止 | 22.2-UNIT-016, 019; IPC-001, 002, 003; CLI-001, 002, 005 |
| AC4 | 异常类型和偏离程度展示 | 22.2-UNIT-001, 002, 009, 011, 020; IPC-004, 005; CLI-003, 004 |
| AC5 | 威胁记忆持久化 | 22.2-UNIT-003, 004, 005, 006, 022 |
| AC6 | 向后兼容 | 22.2-UNIT-018, 023, 024; IPC-006 |

---

## Key Risks & Assumptions

1. **检测时机在事件到达时**：异常检测在 `OnSyscallEvent` 中执行，需确保检测逻辑快速返回，不阻塞调用方 goroutine。测试通过并发测试（UNIT-025）验证线程安全。
2. **BehaviorCollector 需要暴露当前计数**：当前 `BehaviorCollector.syscallCounts` 是私有字段。异常检测需要在 `OnSyscallEvent` 中获取当前累积计数进行比较。可能需要新增 `GetSyscallCount(name string) int` 方法。
3. **威胁签名匹配为 O(n)**：当前设计遍历所有威胁签名进行匹配。签名数量通常很小（<100），线性扫描足够。若未来签名数量增长，可考虑使用 map 索引。
4. **suspendFn 通过依赖注入**：避免 ImmuneDaemon 直接依赖 Kernel，通过 `SetSuspendFunc` 注入回调。测试通过 mock suspendFn 验证调用。
5. **向后兼容**：ImmuneStatusResponse 新增字段使用 Go 零值默认，旧版本 JSON 反序列化不受影响（IPC-006 验证）。

## Next Step

推荐执行 `dev-story` 工作流实现 Story 22.2，按 Implementation Checklist 中的 Task 顺序依次将测试从 RED 变为 GREEN。
