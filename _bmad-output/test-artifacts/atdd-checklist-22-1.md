---
stepsCompleted: ['step-01-preflight-and-context', 'step-02-generation-mode', 'step-03-test-strategy', 'step-04-generate-tests', 'step-05-validate-and-complete']
lastStep: 'step-05-validate-and-complete'
lastSaved: '2026-03-14'
workflowType: 'testarch-atdd'
inputDocuments:
  - '_bmad-output/implementation-artifacts/22-1-immune-daemon-and-behavior-baseline.md'
  - 'kernel/reputation.go'
  - 'kernel/synergy_matrix.go'
  - 'kernel/process.go'
  - 'kernel/kernel.go'
  - 'ipc/protocol.go'
  - 'ipc/server.go'
  - 'ipc/client.go'
  - 'cmd/rnix/synergy.go'
  - 'internal/types/types.go'
---

# ATDD Checklist - Epic 22, Story 1: Immune Daemon 与行为基线

**Date:** 2026-03-14
**Author:** Decker
**Primary Test Level:** Unit + Integration (Go backend)

---

## Story Summary

系统运行 Immune Daemon 持续监控智能体行为并建立正常行为基线，为异常检测提供依据。包括行为数据采集、NormalProfile 建立、持久化与加载、行为样本记录、空闲无开销和向后兼容。

**As a** 平台构建者
**I want** 系统运行 Immune Daemon 持续监控智能体行为并建立正常行为基线
**So that** 系统能识别什么是"正常"行为，为异常检测提供依据

---

## Acceptance Criteria

1. **AC1: Immune Daemon 启动与监控** - 持续监控所有智能体行为模式（syscall 频率、资源访问模式、token 消耗速率），CPU 开销 <= 3%
2. **AC2: 行为数据采集** - 记录 syscall 调用频率、资源访问路径、token 消耗速率、执行时长，采集间隔可配置
3. **AC3: Normal Profile 建立** - 至少 5 条历史记录后建立行为基线（均值 +/- 标准差），持久化到 `$PROJECT/.rnix/immune/`
4. **AC4: Normal Profile 持久化与加载** - daemon 重启后自动加载已有 Profile，新数据持续更新
5. **AC5: 行为样本记录** - JSON Lines 格式追加到 `{agent-template}.jsonl`，含完整行为摘要
6. **AC6: Immune Daemon 空闲无开销** - 无进程时 CPU 接近 0%，无轮询、无 busy-wait
7. **AC7: 向后兼容** - 不影响现有功能，ImmuneDaemon 为可选模块，nil 时不影响系统

---

## Failing Tests Created (RED Phase)

### Unit Tests - kernel/atdd_22_1_immune_daemon_test.go (28 tests)

**File:** `kernel/atdd_22_1_immune_daemon_test.go`

- **Test:** `TestBehaviorSample_JSONRoundTrip` (22.1-UNIT-001)
  - **Status:** RED - BehaviorSample 类型不存在
  - **Verifies:** AC2/AC5 - JSON 序列化/反序列化完整性，snake_case 字段名
  - **Priority:** P0

- **Test:** `TestBehaviorSample_DefaultValues` (22.1-UNIT-002)
  - **Status:** RED - BehaviorSample 类型不存在
  - **Verifies:** AC2 - 零值安全，不 panic
  - **Priority:** P1

- **Test:** `TestComputeProfile_SufficientSamples` (22.1-UNIT-003)
  - **Status:** RED - ComputeProfile 函数不存在
  - **Verifies:** AC3 - 6 样本正确计算均值和标准差
  - **Priority:** P0

- **Test:** `TestComputeProfile_InsufficientSamples` (22.1-UNIT-004)
  - **Status:** RED - ComputeProfile 函数不存在
  - **Verifies:** AC3 - 不足 5 样本返回 nil
  - **Priority:** P0

- **Test:** `TestComputeProfile_EmptySamples` (22.1-UNIT-005)
  - **Status:** RED - ComputeProfile 函数不存在
  - **Verifies:** AC3 - 空样本列表返回 nil
  - **Priority:** P0

- **Test:** `TestComputeProfile_SingleSyscall` (22.1-UNIT-006)
  - **Status:** RED - ComputeProfile 函数不存在
  - **Verifies:** AC3 - 单一 syscall 类型计算正确
  - **Priority:** P1

- **Test:** `TestComputeProfile_ZeroVariance` (22.1-UNIT-007)
  - **Status:** RED - ComputeProfile 函数不存在
  - **Verifies:** AC3 - 所有样本相同时标准差为 0
  - **Priority:** P1

- **Test:** `TestMinSamplesForProfile_Value` (22.1-UNIT-008)
  - **Status:** RED - MinSamplesForProfile 常量不存在
  - **Verifies:** AC3 - 常量值为 5
  - **Priority:** P0

- **Test:** `TestImmuneStore_RecordAndGetSamples` (22.1-UNIT-009)
  - **Status:** RED - NewImmuneStore 函数不存在
  - **Verifies:** AC4/AC5 - 写入 3 条样本，读回全部
  - **Priority:** P0

- **Test:** `TestImmuneStore_EmptyFile` (22.1-UNIT-010)
  - **Status:** RED - NewImmuneStore 函数不存在
  - **Verifies:** AC5 - 文件不存在时返回空切片
  - **Priority:** P0

- **Test:** `TestImmuneStore_SaveAndLoadProfile` (22.1-UNIT-011)
  - **Status:** RED - ImmuneStore.SaveProfile/LoadProfile 不存在
  - **Verifies:** AC4 - Profile 写入后读回一致
  - **Priority:** P0

- **Test:** `TestImmuneStore_LoadProfileNotExist` (22.1-UNIT-012)
  - **Status:** RED - ImmuneStore.LoadProfile 不存在
  - **Verifies:** AC4 - 文件不存在返回 nil, nil
  - **Priority:** P0

- **Test:** `TestImmuneStore_LoadAllProfiles` (22.1-UNIT-013)
  - **Status:** RED - ImmuneStore.LoadAllProfiles 不存在
  - **Verifies:** AC4 - 多模板 Profile 全部加载
  - **Priority:** P0

- **Test:** `TestImmuneStore_ConcurrentWrites` (22.1-UNIT-014)
  - **Status:** RED - NewImmuneStore 函数不存在
  - **Verifies:** AC5 - 多 goroutine 并发写入不丢数据
  - **Priority:** P1

- **Test:** `TestBehaviorCollector_ObserveAccumulates` (22.1-UNIT-015)
  - **Status:** RED - NewBehaviorCollector 函数不存在
  - **Verifies:** AC1/AC2 - 多次 Observe 正确累积统计
  - **Priority:** P0

- **Test:** `TestBehaviorCollector_Finalize` (22.1-UNIT-016)
  - **Status:** RED - NewBehaviorCollector 函数不存在
  - **Verifies:** AC2 - Finalize 生成完整 BehaviorSample（含 token rate 计算）
  - **Priority:** P0

- **Test:** `TestBehaviorCollector_DeviceAccessDedup` (22.1-UNIT-017)
  - **Status:** RED - NewBehaviorCollector 函数不存在
  - **Verifies:** AC2 - 设备路径去重
  - **Priority:** P1

- **Test:** `TestBehaviorCollector_ZeroEvents` (22.1-UNIT-018)
  - **Status:** RED - NewBehaviorCollector 函数不存在
  - **Verifies:** AC2 - 无事件时 Finalize 返回零值样本
  - **Priority:** P1

- **Test:** `TestImmuneDaemon_StartStop` (22.1-UNIT-019)
  - **Status:** RED - NewImmuneDaemon 函数不存在
  - **Verifies:** AC1 - 启动和停止生命周期
  - **Priority:** P0

- **Test:** `TestImmuneDaemon_OnProcessLifecycle` (22.1-UNIT-020)
  - **Status:** RED - ImmuneDaemon.OnProcessStart/OnSyscallEvent/OnProcessExit 不存在
  - **Verifies:** AC1/AC2 - Start → SyscallEvents → Exit 完整流程
  - **Priority:** P0

- **Test:** `TestImmuneDaemon_ProfileBuilding` (22.1-UNIT-021)
  - **Status:** RED - NewImmuneDaemon 函数不存在
  - **Verifies:** AC3 - 累积足够样本后自动建立 Profile
  - **Priority:** P0

- **Test:** `TestImmuneDaemon_ProfilePersistence` (22.1-UNIT-022)
  - **Status:** RED - NewImmuneDaemon 函数不存在
  - **Verifies:** AC4 - 重启后加载已有 Profile
  - **Priority:** P0

- **Test:** `TestImmuneDaemon_NilSafe` (22.1-UNIT-023)
  - **Status:** RED - ImmuneDaemon 类型不存在
  - **Verifies:** AC7 - daemon 为 nil 时所有方法不 panic
  - **Priority:** P0

- **Test:** `TestImmuneDaemon_ConcurrentAccess` (22.1-UNIT-024)
  - **Status:** RED - NewImmuneDaemon 函数不存在
  - **Verifies:** AC1 - 多进程并发事件不竞态
  - **Priority:** P1

- **Test:** `TestImmuneStore_JSONLinesFormat` (22.1-UNIT-025)
  - **Status:** RED - NewImmuneStore 函数不存在
  - **Verifies:** AC5 - 样本文件使用 JSON Lines 格式（每行一条 JSON）
  - **Priority:** P0

- **Test:** `TestImmuneStore_ProfilePath` (22.1-UNIT-026)
  - **Status:** RED - NewImmuneStore 函数不存在
  - **Verifies:** AC4 - Profile 文件路径在 immune/profiles/ 目录
  - **Priority:** P0

- **Test:** `TestImmuneDaemon_GetAllProfiles` (22.1-UNIT-027)
  - **Status:** RED - NewImmuneDaemon 函数不存在
  - **Verifies:** AC3 - 获取所有已建立的缓存 Profile
  - **Priority:** P0

- **Test:** `TestNormalProfile_JSONSerialization` (22.1-UNIT-028)
  - **Status:** RED - NormalProfile 类型不存在
  - **Verifies:** AC3/AC4 - JSON 字段 snake_case 格式
  - **Priority:** P1

### IPC Protocol Tests - ipc/atdd_22_1_immune_ipc_test.go (5 tests)

**File:** `ipc/atdd_22_1_immune_ipc_test.go`

- **Test:** `TestMethodImmuneStatus_Constant` (22.1-IPC-001)
  - **Status:** RED - MethodImmuneStatus 常量不存在
  - **Verifies:** AC3 - 常量值为 "immune_status"
  - **Priority:** P0

- **Test:** `TestImmuneStatusRequest_TypeExists` (22.1-IPC-002)
  - **Status:** RED - ImmuneStatusRequest 类型不存在
  - **Verifies:** AC3 - 请求类型编译通过
  - **Priority:** P0

- **Test:** `TestImmuneStatusResponse_Fields` (22.1-IPC-003)
  - **Status:** RED - ImmuneStatusResponse 类型不存在
  - **Verifies:** AC3 - 响应包含 running、profile_count、active_pids 字段
  - **Priority:** P0

- **Test:** `TestImmuneStatusResponse_EmptyActivePIDs` (22.1-IPC-004)
  - **Status:** RED - ImmuneStatusResponse 类型不存在
  - **Verifies:** AC3 - 空 active_pids 序列化为 [] 而非 null
  - **Priority:** P0

- **Test:** `TestClient_ImmuneStatus_MethodExists` (22.1-IPC-005)
  - **Status:** RED - Client.ImmuneStatus 方法不存在
  - **Verifies:** AC3 - 客户端方法签名正确
  - **Priority:** P1

### CLI Tests - cmd/rnix/atdd_22_1_immune_cmd_test.go (4 tests)

**File:** `cmd/rnix/atdd_22_1_immune_cmd_test.go`

- **Test:** `TestRunImmuneStatus_NoProfiles` (22.1-CLI-001)
  - **Status:** RED - immuneCmd 不存在
  - **Verifies:** AC3 - 无 Profile 时显示 "No behavior profiles established."
  - **Priority:** P0

- **Test:** `TestRunImmuneStatus_JSON` (22.1-CLI-002)
  - **Status:** RED - immuneCmd 不存在
  - **Verifies:** AC3 - JSON 模式输出正确格式
  - **Priority:** P0

- **Test:** `TestImmuneCmd_Registered` (22.1-CLI-003)
  - **Status:** RED - immuneCmd 不存在
  - **Verifies:** AC3 - immune 命令注册 + status 子命令
  - **Priority:** P1

- **Test:** `TestRunImmuneStatus_Table` (22.1-CLI-004)
  - **Status:** RED - immuneStatusCmd 不存在
  - **Verifies:** AC3 - 表格模式输出包含正确列
  - **Priority:** P1

---

## Implementation Checklist

### Task 1: BehaviorSample 数据类型 (kernel/immune.go)

**Tests to make pass:** 22.1-UNIT-001, 22.1-UNIT-002

- [ ] 定义 `BehaviorSample` 结构体（含 JSON tags snake_case）
- [ ] Run: `go test -race -run "TestBehaviorSample" ./kernel/...`

### Task 2: NormalProfile 与 ComputeProfile (kernel/immune.go)

**Tests to make pass:** 22.1-UNIT-003 ~ 22.1-UNIT-008, 22.1-UNIT-028

- [ ] 定义 `NormalProfile` 结构体（含 JSON tags snake_case）
- [ ] 定义 `MinSamplesForProfile = 5` 常量
- [ ] 实现 `ComputeProfile(agentTemplate string, samples []BehaviorSample) *NormalProfile`
  - 计算各 syscall 均值和标准差
  - 计算 token rate 均值和标准差
  - 计算 duration 均值和标准差
  - 样本不足 5 条返回 nil
- [ ] Run: `go test -race -run "TestComputeProfile|TestMinSamplesForProfile|TestNormalProfile" ./kernel/...`

### Task 3: ImmuneStore 持久化引擎 (kernel/immune.go)

**Tests to make pass:** 22.1-UNIT-009 ~ 22.1-UNIT-014, 22.1-UNIT-025, 22.1-UNIT-026

- [ ] 实现 `NewImmuneStore(baseDir string) *ImmuneStore`
- [ ] 实现 `RecordSample(sample BehaviorSample) error`（JSON Lines 追加写入）
- [ ] 实现 `GetSamples(agentTemplate string) ([]BehaviorSample, error)`
- [ ] 实现 `SaveProfile(profile *NormalProfile) error`
- [ ] 实现 `LoadProfile(agentTemplate string) (*NormalProfile, error)`
- [ ] 实现 `LoadAllProfiles() (map[string]*NormalProfile, error)`
- [ ] Run: `go test -race -run "TestImmuneStore" ./kernel/...`

### Task 4: BehaviorCollector 运行时采集 (kernel/immune.go)

**Tests to make pass:** 22.1-UNIT-015 ~ 22.1-UNIT-018

- [ ] 实现 `NewBehaviorCollector(pid types.PID, agentTemplate string) *BehaviorCollector`
- [ ] 实现 `Observe(event types.SyscallEvent)` — 累积 syscall 计数和设备访问
- [ ] 实现 `Finalize(tokensUsed int, exitNormal bool) BehaviorSample` — 生成最终样本
- [ ] Run: `go test -race -run "TestBehaviorCollector" ./kernel/...`

### Task 5: ImmuneDaemon 核心引擎 (kernel/immune.go)

**Tests to make pass:** 22.1-UNIT-019 ~ 22.1-UNIT-024, 22.1-UNIT-027

- [ ] 实现 `NewImmuneDaemon(store *ImmuneStore) *ImmuneDaemon`
- [ ] 实现 `Start() error` — 加载已有 Profile
- [ ] 实现 `Stop()`
- [ ] 实现 `OnProcessStart(pid, agentTemplate)` — 创建 BehaviorCollector
- [ ] 实现 `OnSyscallEvent(pid, event)` — 传递事件到 collector
- [ ] 实现 `OnProcessExit(pid, tokensUsed, exitNormal)` — 生成样本、持久化、更新 Profile
- [ ] 实现 `GetProfile(agentTemplate) *NormalProfile`
- [ ] 实现 `GetAllProfiles() map[string]*NormalProfile`
- [ ] 所有方法 nil receiver 保护
- [ ] Run: `go test -race -run "TestImmuneDaemon" ./kernel/...`

### Task 6: Kernel 集成 (kernel/kernel.go)

**Tests to verify:** Spawn/reapProcess 中调用 ImmuneDaemon 钩子

- [ ] KernelImpl 新增 `immuneDaemon *ImmuneDaemon` 字段
- [ ] 新增 `SetImmuneDaemon(d *ImmuneDaemon)` setter
- [ ] Spawn 中调用 `d.OnProcessStart(pid, agentTemplate)`
- [ ] reapProcess 中调用 `d.OnProcessExit(pid, tokensUsed, exitNormal)`
- [ ] emitEvent 中调用 `d.OnSyscallEvent(pid, event)`
- [ ] 所有调用前检查 `d != nil`

### Task 7: IPC 协议扩展 (ipc/)

**Tests to make pass:** 22.1-IPC-001 ~ 22.1-IPC-005

- [ ] protocol.go: 新增 `MethodImmuneStatus` 常量和 `ImmuneStatusRequest`/`ImmuneStatusResponse` 类型
- [ ] server.go: 新增 `handleImmuneStatus` handler + dispatch 注册
- [ ] client.go: 新增 `ImmuneStatus() (*ImmuneStatusResponse, error)`
- [ ] Run: `go test -race -run "TestMethodImmuneStatus|TestImmuneStatusRequest|TestImmuneStatusResponse|TestClient_ImmuneStatus" ./ipc/...`

### Task 8: CLI 命令 (cmd/rnix/immune.go)

**Tests to make pass:** 22.1-CLI-001 ~ 22.1-CLI-004

- [ ] 新建 `cmd/rnix/immune.go`
- [ ] 实现 `immuneCmd`（immune 命令组）
- [ ] 实现 `immuneStatusCmd`（immune status 子命令）
- [ ] 实现 `runImmuneStatus` — 表格输出和 JSON 输出
- [ ] Run: `go test -race -run "TestRunImmuneStatus|TestImmuneCmd" ./cmd/rnix/...`

### Task 9: Daemon 初始化集成 (cmd/rnix/main.go)

- [ ] 在 runDaemon 中创建 ImmuneStore、ImmuneDaemon
- [ ] 调用 daemon.Start()，关闭时 daemon.Stop()
- [ ] 注入 kernel 和 IPC Server

---

## Test Summary

| Category | Test File | Count | Priority |
|----------|----------|-------|----------|
| Unit (kernel) | `kernel/atdd_22_1_immune_daemon_test.go` | 28 | 17 P0, 11 P1 |
| IPC | `ipc/atdd_22_1_immune_ipc_test.go` | 5 | 4 P0, 1 P1 |
| CLI | `cmd/rnix/atdd_22_1_immune_cmd_test.go` | 4 | 2 P0, 2 P1 |
| **Total** | | **37** | **23 P0, 14 P1** |

---

## AC Coverage Matrix

| AC | Description | Tests |
|----|------------|-------|
| AC1 | Immune Daemon 启动与监控 | 22.1-UNIT-015, 019, 020, 024 |
| AC2 | 行为数据采集 | 22.1-UNIT-001, 002, 015, 016, 017, 018, 020 |
| AC3 | Normal Profile 建立 | 22.1-UNIT-003~008, 021, 027, 028; IPC-001~004; CLI-001~004 |
| AC4 | Normal Profile 持久化与加载 | 22.1-UNIT-009, 011, 012, 013, 022, 026 |
| AC5 | 行为样本记录 | 22.1-UNIT-001, 009, 010, 014, 025 |
| AC6 | Immune Daemon 空闲无开销 | 事件驱动设计验证（架构保证，非轮询） |
| AC7 | 向后兼容 | 22.1-UNIT-023 |

---

## Key Risks & Assumptions

1. **事件驱动设计**：ImmuneDaemon 完全由外部事件驱动（OnProcessStart/OnSyscallEvent/OnProcessExit），无主动轮询，CPU 开销在空闲时为 0。AC6 通过架构设计保证而非运行时测量。
2. **SyscallEvent 集成点**：需在 kernel.go 的 emitEvent 方法中新增 ImmuneDaemon 调用，可能影响热路径性能。测试通过 nil 保护确保无 daemon 时不增加开销。
3. **并发安全**：ImmuneStore 使用 Mutex 保护文件写入，ImmuneDaemon 使用 RWMutex 保护 collectors 和 profiles map，需通过竞态检测验证。
4. **JSON Lines 持久化**：复用 ReputationStore 的 JSON Lines 模式，已在 21.x 系列中验证。

## Next Step

推荐执行 `dev-story` 工作流实现 Story 22.1，按 Implementation Checklist 中的 Task 顺序依次将测试从 RED 变为 GREEN。
