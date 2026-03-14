# Story 22.2: 异常检测与威胁记忆

Status: done

## Story

As a 平台构建者,
I want 系统在智能体行为偏离基线时自动告警和挂起，并记忆已知威胁模式,
So that 异常行为被及时拦截，已知威胁不需要重新检测。

## Acceptance Criteria

1. **AC1: 异常检测与自动挂起**
   - Given 智能体行为偏离基线超过阈值（如文件写入频率 5 倍于正常值）
   - When Immune Daemon 检测到异常
   - Then 触发告警并自动挂起（suspend）该进程，显示异常类型和偏离程度

2. **AC2: 威胁记忆库（Antibody Memory）**
   - Given 一个已识别的异常行为模式
   - When 系统记录到威胁记忆库
   - Then 后续相同模式出现时立即拦截，无需重新检测

3. **AC3: 进程恢复/终止**
   - Given 进程被挂起
   - When 用户审查后
   - Then 可通过 `rnix immune resume <pid>` 恢复执行或 `rnix kill <pid>` 终止

4. **AC4: 异常类型和偏离程度展示**
   - Given 异常检测触发
   - When 告警信息生成
   - Then 包含：异常类型（syscall 频率异常/token 速率异常/设备访问异常）、具体指标值、偏离倍数、被挂起的 PID

5. **AC5: 威胁记忆持久化**
   - Given 威胁记忆库有记录
   - When daemon 重启后
   - Then 自动加载已有的威胁记忆数据

6. **AC6: 向后兼容**
   - Given 现有 ImmuneDaemon 功能（Story 22.1）
   - When 新增异常检测和威胁记忆功能
   - Then 不影响现有行为监控和 Profile 建立功能

## Tasks / Subtasks

### Task 1: AnomalyType 和 AnomalyAlert 数据类型（AC: #1, #4）

- [x] 1.1 在 `kernel/immune.go` 中新增数据结构：

  ```go
  // AnomalyType 异常检测类型枚举。
  type AnomalyType string

  const (
      AnomalySyscallFreq  AnomalyType = "syscall_freq"    // syscall 调用频率异常
      AnomalyTokenRate    AnomalyType = "token_rate"      // token 消耗速率异常
      AnomalyDeviceAccess AnomalyType = "device_access"   // 未预期的设备访问
  )

  // AnomalyAlert 异常告警记录。
  type AnomalyAlert struct {
      PID           types.PID   `json:"pid"`
      AgentTemplate string      `json:"agent_template"`
      Type          AnomalyType `json:"type"`
      Detail        string      `json:"detail"`      // 人类可读描述（如 "Open 频率 12.0 是基线 2.4 的 5.0 倍"）
      Deviation     float64     `json:"deviation"`   // 偏离倍数（实际值/基线均值）
      Timestamp     time.Time   `json:"timestamp"`
  }
  ```

- [x] 1.2 单元测试：
  - `TestAnomalyAlert_JSONRoundTrip` -- 序列化/反序列化完整性
  - `TestAnomalyType_Constants` -- 常量值正确

### Task 2: ThreatSignature 和 AntibodyMemory 数据类型（AC: #2, #5）

- [x] 2.1 在 `kernel/immune.go` 中新增：

  ```go
  // ThreatSignature 威胁签名——描述一种已知的异常行为模式。
  type ThreatSignature struct {
      ID            string      `json:"id"`              // 唯一标识，格式 "threat-<timestamp>"
      Type          AnomalyType `json:"type"`            // 异常类型
      AgentTemplate string      `json:"agent_template"`  // 触发的 Agent 模板
      Metric        string      `json:"metric"`          // 具体指标名（如 syscall 名 "Open"，或 "token_rate"）
      Threshold     float64     `json:"threshold"`       // 触发阈值（偏离倍数）
      CreatedAt     time.Time   `json:"created_at"`
  }
  ```

  **AntibodyMemory 持久化** 扩展 `ImmuneStore`：

  ```go
  // SaveThreat 追加一条威胁签名到威胁记忆库。
  // 文件路径：{baseDir}/threats.jsonl
  func (s *ImmuneStore) SaveThreat(sig ThreatSignature) error

  // LoadThreats 加载全部威胁签名。
  func (s *ImmuneStore) LoadThreats() ([]ThreatSignature, error)
  ```

- [x] 2.2 单元测试：
  - `TestImmuneStore_SaveAndLoadThreats` -- 写入 3 条，读回全部
  - `TestImmuneStore_LoadThreats_Empty` -- 文件不存在返回空切片
  - `TestThreatSignature_JSONRoundTrip` -- 序列化/反序列化完整性

### Task 3: AnomalyDetector 异常检测引擎（AC: #1, #2, #4）

- [x] 3.1 在 `kernel/immune.go` 中实现 AnomalyDetector：

  ```go
  // DefaultDeviationThreshold 默认偏离阈值倍数。
  // 当实际值 > 均值 + threshold * stddev 时视为异常。
  // 使用 3.0 表示 3 倍标准差（99.7% 置信区间外）。
  const DefaultDeviationThreshold = 3.0

  // AnomalyDetector 基于 NormalProfile 检测行为异常。
  type AnomalyDetector struct {
      threshold float64 // 偏离阈值（标准差倍数）
  }

  func NewAnomalyDetector(threshold float64) *AnomalyDetector

  // CheckSyscallAnomaly 检查单次 SyscallEvent 是否异常。
  // 基于 BehaviorCollector 的累积计数与 NormalProfile 比较。
  // 返回 nil 表示正常，返回 *AnomalyAlert 表示检测到异常。
  func (d *AnomalyDetector) CheckSyscallAnomaly(
      pid types.PID,
      agentTemplate string,
      syscallName string,
      currentCount int,
      profile *NormalProfile,
  ) *AnomalyAlert

  // CheckTokenRateAnomaly 检查 token 消耗速率是否异常。
  func (d *AnomalyDetector) CheckTokenRateAnomaly(
      pid types.PID,
      agentTemplate string,
      currentRate float64,
      profile *NormalProfile,
  ) *AnomalyAlert

  // MatchThreat 检查当前行为是否匹配已知的威胁签名。
  // 匹配条件：相同 agent template + 相同 anomaly type + 相同 metric。
  func (d *AnomalyDetector) MatchThreat(
      agentTemplate string,
      anomalyType AnomalyType,
      metric string,
      threats []ThreatSignature,
  ) *ThreatSignature
  ```

  **检测算法：**
  - syscall 频率异常：当前累计次数 > mean + threshold * stddev（Profile 已有的均值和标准差）
  - token 速率异常：当前速率 > mean + threshold * stddev
  - 偏离倍数 = currentValue / mean（当 mean > 0 时）

- [x] 3.2 单元测试：
  - `TestAnomalyDetector_SyscallNormal` -- 正常范围不报异常
  - `TestAnomalyDetector_SyscallAnomaly` -- 超出阈值报异常
  - `TestAnomalyDetector_TokenRateNormal` -- 正常 token 速率不报异常
  - `TestAnomalyDetector_TokenRateAnomaly` -- 超出阈值报异常
  - `TestAnomalyDetector_NoProfile` -- Profile 为 nil 时返回 nil（无法检测）
  - `TestAnomalyDetector_ZeroMean` -- 均值为 0 时不误报
  - `TestAnomalyDetector_MatchThreat` -- 匹配已知威胁签名
  - `TestAnomalyDetector_NoMatchThreat` -- 不匹配的签名返回 nil

### Task 4: ImmuneDaemon 异常检测集成（AC: #1, #2, #3, #6）

- [x] 4.1 扩展 `ImmuneDaemon` 结构体和方法：

  ```go
  // 新增字段：
  type ImmuneDaemon struct {
      // ... 现有字段 ...
      detector *AnomalyDetector
      threats  []ThreatSignature              // 已加载的威胁签名缓存
      alerts   map[types.PID]*AnomalyAlert    // PID → 最近一次告警（仅挂起进程）
      suspendFn func(pid types.PID) error     // 挂起进程的回调（注入 Kernel.Kill(pid, SIGPAUSE)）
  }

  // 修改 NewImmuneDaemon 签名（保持向后兼容，suspendFn 可为 nil）：
  // 或新增 setter：
  func (d *ImmuneDaemon) SetSuspendFunc(fn func(pid types.PID) error)

  // 修改 Start：加载威胁签名
  // 修改 OnSyscallEvent：在 Observe 后执行异常检测

  // 新方法：
  // GetAlerts 返回所有当前活跃的告警。
  func (d *ImmuneDaemon) GetAlerts() map[types.PID]*AnomalyAlert

  // ResumeProcess 从挂起状态恢复进程（清除告警记录）。
  // 实际的 SIGRESUME 发送由调用方（CLI/IPC handler）负责。
  func (d *ImmuneDaemon) ClearAlert(pid types.PID)

  // GetThreats 返回所有已知的威胁签名。
  func (d *ImmuneDaemon) GetThreats() []ThreatSignature
  ```

  **OnSyscallEvent 异常检测流程：**
  1. 调用 `collector.Observe(event)` 更新行为统计
  2. 检查 `profile` 是否存在（无 Profile 跳过检测）
  3. 检查威胁签名是否匹配（已知威胁立即挂起，无需重新检测）
  4. 调用 `detector.CheckSyscallAnomaly` 检测 syscall 频率异常
  5. 如果检测到异常：
     a. 创建 `AnomalyAlert` 并记录到 `d.alerts[pid]`
     b. 创建 `ThreatSignature` 并持久化到威胁记忆库
     c. 调用 `d.suspendFn(pid)` 挂起进程（发送 SIGPAUSE）

- [x] 4.2 单元测试：
  - `TestImmuneDaemon_AnomalyDetection` -- 行为超出基线时自动挂起
  - `TestImmuneDaemon_ThreatMemoryMatch` -- 已知威胁签名立即拦截
  - `TestImmuneDaemon_NoProfileNoDetection` -- 无 Profile 时不检测
  - `TestImmuneDaemon_ClearAlert` -- 清除告警后不再持有该 PID 告警
  - `TestImmuneDaemon_GetAlerts` -- 返回当前所有活跃告警
  - `TestImmuneDaemon_GetThreats` -- 返回所有已知威胁签名
  - `TestImmuneDaemon_ThreatPersistence` -- daemon 重启后加载已有威胁签名
  - `TestImmuneDaemon_SuspendFnNil` -- suspendFn 为 nil 时不 panic

### Task 5: IPC 协议扩展（AC: #3, #4）

- [x] 5.1 在 `ipc/protocol.go` 新增：

  ```go
  MethodImmuneResume Method = "immune_resume"

  type ImmuneResumeRequest struct {
      PID uint64 `json:"pid"`
  }

  type ImmuneResumeResponse struct {
      OK      bool   `json:"ok"`
      Message string `json:"message"`
  }
  ```

- [x] 5.2 扩展 `ImmuneStatusResponse`：

  ```go
  // 在现有 ImmuneStatusResponse 中新增字段：
  type ImmuneStatusResponse struct {
      Running      bool                             `json:"running"`
      ProfileCount int                              `json:"profile_count"`
      Profiles     map[string]*kernel.NormalProfile  `json:"profiles"`
      ActivePIDs   []uint64                          `json:"active_pids"`
      Alerts       []AlertWire                       `json:"alerts"`        // 新增
      ThreatCount  int                              `json:"threat_count"`   // 新增
  }

  // AlertWire 是 AnomalyAlert 的 IPC 线格式。
  type AlertWire struct {
      PID           uint64  `json:"pid"`
      AgentTemplate string  `json:"agent_template"`
      Type          string  `json:"type"`
      Detail        string  `json:"detail"`
      Deviation     float64 `json:"deviation"`
      TimestampMs   int64   `json:"timestamp_ms"`
  }
  ```

- [x] 5.3 在 `ipc/server.go` 新增 handler：
  - `handleImmuneResume` -- 调用 `kernel.Kill(pid, SIGRESUME)` + `immuneDaemon.ClearAlert(pid)`
  - 在 `dispatch` map 中注册 `MethodImmuneResume`
  - 修改 `handleImmuneStatus` 以包含 Alerts 和 ThreatCount

- [x] 5.4 在 `ipc/client.go` 新增：

  ```go
  func (c *Client) ImmuneResume(pid uint64) (*ImmuneResumeResponse, error)
  ```

- [x] 5.5 单元测试：
  - `TestMethodImmuneResume_Constant` -- 验证常量值 "immune_resume"

### Task 6: CLI 命令扩展（AC: #3, #4）

- [x] 6.1 在 `cmd/rnix/immune.go` 新增 `immune resume` 子命令：

  ```go
  var immuneResumeCmd = &cobra.Command{
      Use:   "resume <pid>",
      Short: "Resume a suspended process",
      Args:  cobra.ExactArgs(1),
      RunE:  runImmuneResume,
  }
  ```

  **输出格式：**
  ```
  Process 42 resumed successfully.
  ```

- [x] 6.2 修改 `immune status` 输出以显示告警和威胁信息：

  **终端输出新增段落：**
  ```
  Immune Daemon: running
  Profiles: 3
  Active Monitors: 2
  Threat Memory: 5 signatures

  AGENT TEMPLATE    SAMPLES  TOKEN RATE (avg)  DURATION (avg)  LAST UPDATED
  code-analyst           12         2.5 tok/s          15.2s  2026-03-14
  ...

  ALERTS (1):
    PID 42: syscall_freq - Open 频率 12.0 是基线 2.4 的 5.0 倍 (2026-03-14 10:30:00)
    Actions: rnix immune resume 42 | rnix kill 42
  ```

- [x] 6.3 单元测试：
  - `TestRunImmuneResume_Success` -- 成功恢复输出正确
  - `TestRunImmuneResume_NoDaemon` -- daemon 未运行时输出错误
  - `TestRunImmuneStatus_WithAlerts` -- 有告警时表格输出包含 ALERTS 段落
  - `TestRunImmuneStatus_WithThreats` -- 显示威胁记忆数量

### Task 7: Kernel 集成（AC: #1, #6）

- [x] 7.1 修改 `cmd/rnix/main.go` 的 `runDaemon`：
  - 创建 `AnomalyDetector` 实例（使用 `DefaultDeviationThreshold`）
  - 将 detector 注入到 `ImmuneDaemon`
  - 设置 `suspendFn` 回调为 `func(pid types.PID) error { return kernel.Kill(pid, types.SIGPAUSE) }`
  - 确保 Kernel 引用对 ImmuneDaemon 可用（用于 SIGPAUSE/SIGRESUME 信号发送）

- [x] 7.2 确认现有功能不受影响（回归验证）

## Dev Notes

### 核心设计决策

**基于统计阈值的异常检测。** 算法选择：
1. 使用 NormalProfile 的均值和标准差定义正常范围
2. 默认阈值 3.0 标准差（即超过 mean + 3*stddev 视为异常）
3. 3 倍标准差对应正态分布 99.7% 置信区间——仅 0.3% 的正常行为会被误判
4. 阈值可通过 `DefaultDeviationThreshold` 常量调整

**实时逐事件检测，非周期性批处理。** 检测时机：
1. 在 `OnSyscallEvent` 中，每次事件到达时即时检测
2. 比较 BehaviorCollector 的累积计数与 NormalProfile
3. 只在有对应 Profile 的情况下检测（无 Profile 的新模板不检测）
4. 检测逻辑在调用方 goroutine 中执行——`CheckSyscallAnomaly` 必须快速返回

**威胁记忆实现为签名匹配。** 设计选择：
1. ThreatSignature 由三元组标识：`(agent_template, anomaly_type, metric)`
2. 新事件到达时先检查威胁签名（O(n) 遍历，n 为签名数量，通常很小）
3. 匹配到已知签名立即挂起，跳过统计检测（快速路径）
4. 威胁签名持久化为 JSON Lines 文件，daemon 重启后自动加载

**挂起机制复用 SIGPAUSE 信号。** 实现策略：
1. 异常检测触发后调用 `suspendFn(pid)`，实际发送 `SIGPAUSE` 信号
2. 恢复通过 `rnix immune resume <pid>` 命令，实际发送 `SIGRESUME` 信号
3. 复用 Story 6.4 已实现的 SIGPAUSE/SIGRESUME 信号系统和进程暂停/恢复逻辑
4. `suspendFn` 通过依赖注入传入，避免 ImmuneDaemon 直接依赖 Kernel

**suspendFn 回调注入，避免循环依赖。** 架构合规：
1. `kernel/immune.go` 不能导入 `kernel.go` 中的 `KernelImpl`（同一个包，但逻辑上 ImmuneDaemon 不应直接持有 KernelImpl 引用）
2. 通过 `SetSuspendFunc` setter 注入回调函数
3. 回调在 `cmd/rnix/main.go` 中设置为 `kernel.Kill(pid, SIGPAUSE)` 的闭包
4. `suspendFn` 为 nil 时检测到异常仅记录告警，不执行挂起（降级模式）

### 架构合规

- **依赖方向**：`kernel/immune.go` 仅使用标准库 + `internal/types`（与 22.1 一致）
- **包边界**：在现有 `kernel/immune.go` 文件内扩展，不新增文件
- **IPC 扩展 4 步标准**：protocol.go → server.go → client.go → cmd/rnix/immune.go
- **JSON 输出**：`snake_case` 字段名，包装在 `JSONResponse` 中
- **持久化路径**：威胁签名存储在 `$PROJECT/.rnix/immune/threats.jsonl`（符合 Decision 8 规划）
- **向后兼容**：新增字段不影响现有 ImmuneStatusResponse 的 JSON 解析（新字段使用 `omitempty` 或有零值默认）
- **nil 保护**：所有新增公开方法检查 receiver 是否为 nil
- **SIGPAUSE/SIGRESUME**：复用 `internal/types` 已定义的信号常量（SIGPAUSE=4, SIGRESUME=5）

### 关键文件变更

| 文件 | 变更类型 | 说明 |
|------|---------|------|
| `kernel/immune.go` | 修改 | 新增 AnomalyType/AnomalyAlert/ThreatSignature 类型、AnomalyDetector、ImmuneStore 威胁持久化、ImmuneDaemon 异常检测逻辑 |
| `kernel/immune_test.go` | 修改 | 新增异常检测和威胁记忆相关单元测试 |
| `ipc/protocol.go` | 修改 | 新增 MethodImmuneResume、ImmuneResumeRequest/Response、AlertWire；扩展 ImmuneStatusResponse |
| `ipc/server.go` | 修改 | 新增 handleImmuneResume handler、修改 handleImmuneStatus 包含告警和威胁信息 |
| `ipc/client.go` | 修改 | 新增 ImmuneResume() 客户端方法 |
| `cmd/rnix/immune.go` | 修改 | 新增 immune resume 子命令、修改 immune status 输出格式 |
| `cmd/rnix/immune_test.go` | 修改 | 新增 resume 和扩展 status 测试 |
| `cmd/rnix/main.go` | 修改 | runDaemon 中创建 AnomalyDetector 并注入、设置 suspendFn 回调 |

### 复用模式

- **信号系统**：复用 Story 6.4 的 SIGPAUSE/SIGRESUME 信号——`kernel.Kill(pid, types.SIGPAUSE)` 挂起、`kernel.Kill(pid, types.SIGRESUME)` 恢复
- **JSON Lines 持久化**：复用 `ImmuneStore` 的 `RecordSample` 模式（`os.OpenFile` 追加写 + `bufio.Scanner` 逐行读）
- **IPC handler 模式**：复用 `handleImmuneStatus` 的 nil 检查 + 序列化模式
- **CLI 命令模式**：复用 `cmd/rnix/immune.go` 现有的 Cobra 命令结构
- **NormalProfile 统计方法**：复用 22.1 的 `ComputeProfile` 计算的均值和标准差

### 从 Story 22.1 继承的经验

- **同步持久化优于异步**：22.1 中 `OnProcessExit` 最初使用异步 goroutine 导致测试 flakiness，改为同步后稳定。本 Story 的威胁签名持久化也使用同步写入
- **nil 保护是关键**：22.1 所有公开方法检查 `d == nil`——本 Story 新增方法同样遵守
- **ActivePIDs 类型需对齐**：22.1 的 ATDD 测试期望 `[]uint64`，IPC 层必须转换 `types.PID` → `uint64`
- **测试时使用 `t.TempDir()` 替代手动清理**：22.1 的 ImmuneStore 测试使用临时目录，本 Story 继续此模式

### 组合矩阵

| 本功能 | 交互对象 | 交互方式 | 需验证 |
|--------|----------|----------|--------|
| AnomalyDetector.CheckSyscallAnomaly | NormalProfile (22.1) | 依赖：使用 Profile 的均值和标准差 | 是 |
| ImmuneDaemon.OnSyscallEvent 异常检测 | BehaviorCollector (22.1) | 集成：Observe 后获取累积计数进行检测 | 是 |
| suspendFn(SIGPAUSE) | kernel.Kill + Signal System (6.4) | 集成：通过信号系统挂起进程 | 是 |
| immune resume → SIGRESUME | kernel.Kill + Signal System (6.4) | 集成：通过信号系统恢复进程 | 是 |
| ThreatSignature 持久化 | ImmuneStore (22.1) | 扩展：在同一目录下新增 threats.jsonl 文件 | 是 |
| ImmuneStatusResponse 扩展 | IPC immune_status (22.1) | 扩展：新增 Alerts 和 ThreatCount 字段 | 是 |
| 异常告警 | Supervisor (10.4) | 独立：异常检测挂起进程不干预 Supervisor 重启策略 | 否 |
| 异常告警 | BudgetPool (21.1) | 独立：异常检测不影响 token 预算分配 | 否 |
| 异常告警 | Reputation (21.3) | 独立：异常行为不直接影响声誉评分（未来可考虑联动） | 否 |

### Project Structure Notes

- `kernel/immune.go` 在现有文件内扩展——不新建文件，保持 22.1 的单文件结构
- `cmd/rnix/immune.go` 在现有文件内新增 `immune resume` 子命令
- 威胁签名文件 `$PROJECT/.rnix/immune/threats.jsonl` 与行为样本文件同级
- 测试文件与源文件同目录——遵循 Go 惯例

### References

- [Source: _bmad-output/planning-artifacts/epics/epic-22-适应性安全与自愈-adaptive-security-self-healing.md#Story 22.2]
- [Source: _bmad-output/planning-artifacts/prd/functional-requirements.md#FR131] -- 异常检测与自动挂起
- [Source: _bmad-output/planning-artifacts/prd/functional-requirements.md#FR132] -- 威胁记忆库（Antibody Memory）
- [Source: _bmad-output/planning-artifacts/prd/functional-requirements.md#FR133] -- immune status 查看安全监控状态
- [Source: _bmad-output/planning-artifacts/prd/non-functional-requirements.md#NFR47] -- CPU 开销 <= 3%
- [Source: _bmad-output/planning-artifacts/architecture/core-architectural-decisions.md#Decision 8] -- 持久化路径 $PROJECT/.rnix/immune/
- [Source: _bmad-output/planning-artifacts/architecture/implementation-patterns-consistency-rules.md#文件持久化路径模式]
- [Source: _bmad-output/project-context.md#IPC 扩展标准步骤]
- [Source: _bmad-output/implementation-artifacts/22-1-immune-daemon-and-behavior-baseline.md] -- Story 22.1 实现和经验
- [Source: kernel/immune.go] -- ImmuneDaemon 现有实现
- [Source: internal/types/types.go#SIGPAUSE/SIGRESUME] -- 信号定义
- [Source: kernel/kernel.go#emitEvent] -- SyscallEvent 集成点
- [Source: ipc/protocol.go#ImmuneStatusResponse] -- 现有 IPC 响应类型
- [Source: cmd/rnix/immune.go] -- 现有 CLI 命令结构

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6

### Debug Log References

N/A

### Completion Notes List

- Tasks 1-4（AnomalyType、ThreatSignature、AnomalyDetector、ImmuneDaemon 集成）在 Story 22.1 实现阶段已预先完成，本次开发聚焦 Tasks 5-7（IPC 协议扩展、CLI 命令、Kernel 集成）。
- ATDD 测试期望 daemon Start() 时自动初始化默认 AnomalyDetector（DefaultDeviationThreshold=3.0），已在 Start() 中添加 nil 检查与自动创建逻辑。
- suspendFn 回调通过 SetSuspendFunc 注入，在 cmd/rnix/main.go runDaemon 中设置为 kernel.Kill(pid, SIGPAUSE) 闭包，避免循环依赖。
- ImmuneStatusResponse 扩展 Alerts 和 ThreatCount 字段，保持向后兼容（零值默认）。
- AlertWire 作为 AnomalyAlert 的 IPC 线格式，使用 snake_case JSON 字段名。
- 修复 lint 问题：GetAlerts() 中使用 maps.Copy 替代手动 map 遍历；ATDD 测试中移除冗余 nil 检查。
- 全部 20 个包测试通过（含 race 检测），lint 0 issues，build 成功。

### File List

| 文件 | 变更类型 | 说明 |
|------|---------|------|
| `kernel/immune.go` | 修改 | AnomalyType/AnomalyAlert/ThreatSignature 类型、AnomalyDetector 引擎、ImmuneStore 威胁持久化、ImmuneDaemon 异常检测集成、Start() 自动初始化 detector |
| `kernel/atdd_22_2_anomaly_detection_test.go` | 新增 | 异常检测和威胁记忆 ATDD 测试（25 个测试用例） |
| `ipc/protocol.go` | 修改 | MethodImmuneResume 常量、ImmuneResumeRequest/Response、AlertWire 类型、ImmuneStatusResponse 扩展 Alerts/ThreatCount |
| `ipc/server.go` | 修改 | handleImmuneResume handler、dispatch 注册、handleImmuneStatus 包含告警和威胁计数 |
| `ipc/client.go` | 修改 | ImmuneResume(pid) 客户端方法 |
| `ipc/atdd_22_2_anomaly_ipc_test.go` | 新增 | IPC 协议 ATDD 测试（7 个测试用例） |
| `cmd/rnix/immune.go` | 修改 | immune resume 子命令、immune status 输出扩展（Threat Memory 行、ALERTS 段落） |
| `cmd/rnix/immune_test.go` | 未修改 | 22.1 已有测试保持不变 |
| `cmd/rnix/atdd_22_2_anomaly_cmd_test.go` | 新增 | CLI 命令 ATDD 测试（5 个测试用例） |
| `cmd/rnix/main.go` | 修改 | runDaemon 中创建 AnomalyDetector、注入 detector 到 ImmuneDaemon、设置 suspendFn 回调 |

### Change Log

- 2026-03-14: 实现 Tasks 5-7（IPC 协议扩展、CLI 命令、Kernel 集成），所有 ATDD 测试通过，`make all` 验证通过，Story 状态更新为 review。
- 2026-03-14: Code Review (AI) PASSED. 0 HIGH, 0 MEDIUM (fixed), 2 LOW issues. File List 已修正（测试文件位置不准确 -> 已更新）。所有 6 个 AC 验证通过，所有 7 个 Task 验证完成。37 个测试用例全部通过（含 race 检测）。状态更新为 done。
