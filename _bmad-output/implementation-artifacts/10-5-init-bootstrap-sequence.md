# Story 10.5: init 引导序列

Status: done

## Story

As a 系统,
I want daemon 启动时按配置初始化系统级服务和 Supervisor 树,
So that 系统启动后所有基础设施就位。

## Acceptance Criteria

1. **AC1: 配置驱动的 init 引导序列**
   - Given `kernel/init.go` 已实现
   - When daemon 启动
   - Then 按配置文件初始化系统级服务（FR65）：
   - And 日志聚合服务启动
   - And Skill 注册表初始化（扫描 `lib/skills/`）
   - And MCP 服务管理器初始化
   - And Supervisor 树按配置构建

2. **AC2: 必须服务启动失败**
   - Given 初始化过程中某服务启动失败
   - When 为必须服务（required: true）
   - Then daemon 启动失败，输出具体错误信息和恢复建议

3. **AC3: 可选服务启动失败**
   - Given 初始化过程中某服务启动失败
   - When 为可选服务（required: false）
   - Then 记录警告，继续启动其余服务

## Tasks / Subtasks

- [x] Task 1: Init 配置类型定义 (AC: #1)
  - [x] 1.1 `kernel/init.go`：定义 `InitConfig` 结构体（Services []ServiceConfig, Supervisors []SupervisorConfig）
  - [x] 1.2 `kernel/init.go`：定义 `ServiceConfig` 结构体（Name, Type, Required, Config map[string]any）
  - [x] 1.3 `kernel/init.go`：定义 `SupervisorConfig` 结构体（Name, Strategy, MaxRestarts, MaxWindow, Children []ChildConfig, Required）
  - [x] 1.4 `kernel/init.go`：定义 `ChildConfig` 结构体（Name, Intent, Agent, Model, ContextBudget, Restart）
  - [x] 1.5 `kernel/init.go`：`LoadInitConfig(path string) (*InitConfig, error)` 解析 `crux-init.yaml`
  - [x] 1.6 `kernel/init.go`：`DefaultInitConfig() *InitConfig` 返回无配置文件时的默认配置

- [x] Task 2: ServiceInitializer 接口与内置服务 (AC: #1, #2, #3)
  - [x] 2.1 `kernel/init.go`：定义 `ServiceInitializer` 接口 `Init(cfg map[string]any) error` + `Name() string`
  - [x] 2.2 `kernel/init.go`：定义 `InitResult` 结构体（Started []string, Warnings []string, Failed []ServiceError）
  - [x] 2.3 `kernel/init.go`：定义 `ServiceError` 结构体（Service string, Err error, Recovery string）
  - [x] 2.4 `kernel/init.go`：`skillRegistryService` 实现——扫描 `lib/skills/` 目录，预加载所有 Skill 元数据
  - [x] 2.5 `kernel/init.go`：`mcpManagerService` 实现——验证 `mcp.yaml` 中所有 MCP 服务器可达性
  - [x] 2.6 `kernel/init.go`：`logAggregatorService` 实现——初始化日志聚合通道

- [x] Task 3: Init 引擎核心实现 (AC: #1, #2, #3)
  - [x] 3.1 `kernel/init.go`：`Bootstrap(k *KernelImpl, cfg *InitConfig, agentLoader AgentLoaderFunc) (*InitResult, error)` 主入口
  - [x] 3.2 Phase 1：遍历 `cfg.Services`，按序调用 `ServiceInitializer.Init()`
  - [x] 3.3 Phase 1 错误处理：required 服务失败 → 返回 error（含恢复建议）；optional 服务失败 → 记录 warning 继续
  - [x] 3.4 Phase 2：遍历 `cfg.Supervisors`，调用 `k.SpawnSupervisor()` 构建 Supervisor 树
  - [x] 3.5 Phase 2 错误处理：required Supervisor 失败 → 回滚已启动的 Supervisor → 返回 error；optional → warning 继续
  - [x] 3.6 返回 `InitResult`（已启动服务列表、警告列表）

- [x] Task 4: runDaemon 集成 (AC: #1, #2, #3)
  - [x] 4.1 `cmd/crux/main.go`：在 `runDaemon` 中 `srv.ListenAndServe()` 之后，调用 `kernel.Bootstrap()`
  - [x] 4.2 `cmd/crux/main.go`：Bootstrap 失败（required 服务）→ 输出错误 + 恢复建议 → srv.Shutdown() → 返回 error
  - [x] 4.3 `cmd/crux/main.go`：Bootstrap 成功 → 打印 InitResult 摘要（已启动服务 + 警告）到 stderr

- [x] Task 5: AgentLoaderFunc 类型桥接 (AC: #1)
  - [x] 5.1 `kernel/init.go`：定义 `AgentLoaderFunc` 类型 `func(name string) (*agents.AgentInfo, error)` 用于 Bootstrap 参数
  - [x] 5.2 `cmd/crux/main.go`：传递 `agentLoader.Load` 作为 `AgentLoaderFunc`

- [x] Task 6: 测试 (AC: all)
  - [x] 6.1 `kernel/init_test.go`：默认配置（无 crux-init.yaml）——Bootstrap 成功，InitResult.Started 为空
  - [x] 6.2 `kernel/init_test.go`：required 服务失败 → Bootstrap 返回 error，error 信息含服务名和恢复建议
  - [x] 6.3 `kernel/init_test.go`：optional 服务失败 → Bootstrap 成功，InitResult.Warnings 含警告信息
  - [x] 6.4 `kernel/init_test.go`：Supervisor 树构建——从 config 创建 SupervisorSpec → SpawnSupervisor 成功
  - [x] 6.5 `kernel/init_test.go`：required Supervisor 失败 → Bootstrap 返回 error
  - [x] 6.6 `kernel/init_test.go`：混合场景——2 个 required 服务 + 1 个 optional 服务 + 1 个 Supervisor → 全部成功
  - [x] 6.7 `kernel/init_test.go`：Supervisor 构建使用 AgentLoaderFunc 加载 agent 定义
  - [x] 6.8 `cmd/crux/main_test.go`：确认无命令注册回归

## Dev Notes

### 关键架构决策

#### 配置驱动的 Init 引导

init 引导序列通过 `crux-init.yaml` 配置文件驱动。文件不存在时使用默认配置（空服务列表），daemon 正常启动——保持向后兼容。

**crux-init.yaml 示例：**

```yaml
# crux-init.yaml
services:
  - name: skill-registry
    type: skill_registry
    required: false
    config:
      scan_path: lib/skills

  - name: mcp-manager
    type: mcp_manager
    required: false
    config:
      config_path: mcp.yaml

  - name: log-aggregator
    type: log_aggregator
    required: false

supervisors:
  - name: system-supervisor
    strategy: one_for_one
    max_restarts: 5
    max_window: 60s
    required: true
    children:
      - name: monitor-agent
        intent: "系统监控"
        agent: system-monitor
        model: haiku
        context_budget: 1000
        restart: permanent
```

#### Init 引导序列的两个 Phase

```
Phase 1: 服务初始化（串行，有序）
  → skill_registry → mcp_manager → log_aggregator

Phase 2: Supervisor 树构建（串行，有序）
  → 按 supervisors 列表顺序逐个 SpawnSupervisor
```

Phase 1 和 Phase 2 均严格串行执行，保证依赖服务先于依赖者启动。

#### Bootstrap 函数签名

```go
// kernel/init.go

// AgentLoaderFunc loads an agent definition by name.
type AgentLoaderFunc func(name string) (*agents.AgentInfo, error)

// Bootstrap executes the init sequence: services first, then supervisors.
// Returns InitResult on success (may contain warnings for optional failures).
// Returns error if any required service or supervisor fails.
func Bootstrap(k *KernelImpl, cfg *InitConfig, agentLoader AgentLoaderFunc) (*InitResult, error)
```

Bootstrap 是一个包级函数（不是 KernelImpl 的方法），接收 `*KernelImpl` 作为参数。原因：
- Init 逻辑是启动时一次性的，不属于内核核心 API
- 避免膨胀 KernelImpl 接口
- 测试时可以独立构造参数

#### 服务类型注册表

使用 `map[string]ServiceInitializer` 内部注册表匹配 `ServiceConfig.Type` 到具体实现：

```go
var serviceRegistry = map[string]func() ServiceInitializer{
    "skill_registry": func() ServiceInitializer { return &skillRegistryService{} },
    "mcp_manager":    func() ServiceInitializer { return &mcpManagerService{} },
    "log_aggregator": func() ServiceInitializer { return &logAggregatorService{} },
}
```

#### ServiceInitializer 接口

```go
type ServiceInitializer interface {
    Name() string
    Init(cfg map[string]any) error
}
```

每个内置服务实现 `ServiceInitializer`：

**skillRegistryService**：
- 扫描 `config["scan_path"]`（默认 `lib/skills/`）目录
- 对每个子目录调用 `skills.SkillLoader.LoadMetadata()` 预加载元数据
- 目的：在 daemon 启动时验证所有 Skill 定义完整有效
- 如果 `scan_path` 目录不存在，视为空注册表（非错误）

**mcpManagerService**：
- 加载 `config["config_path"]`（默认 `mcp.yaml`）
- 调用 `mcp.LoadMCPConfig()` 验证配置可解析
- 不启动 MCP 服务器进程（按需启动），仅验证配置
- 如果配置文件不存在，视为空 MCP 配置（非错误）

**logAggregatorService**：
- 初始化日志聚合通道（当前为 no-op 占位符）
- 为未来日志集中收集做准备
- MVP 阶段始终成功

#### SupervisorConfig → SupervisorSpec 转换

```go
func (sc *SupervisorConfig) toSupervisorSpec(agentLoader AgentLoaderFunc) (SupervisorSpec, error) {
    spec := SupervisorSpec{
        Strategy:    RestartStrategy(sc.Strategy),
        MaxRestarts: sc.MaxRestarts,
        MaxWindow:   sc.MaxWindow,
    }
    for _, cc := range sc.Children {
        cs := ChildSpec{
            Name:          cc.Name,
            Intent:        cc.Intent,
            Model:         cc.Model,
            ContextBudget: cc.ContextBudget,
            Restart:       ChildRestart(cc.Restart),
        }
        // 加载 agent 定义（可选）
        if cc.Agent != "" {
            agent, err := agentLoader(cc.Agent)
            if err != nil {
                return SupervisorSpec{}, fmt.Errorf("load agent %q for child %q: %w", cc.Agent, cc.Name, err)
            }
            cs.Agent = agent
        }
        spec.Children = append(spec.Children, cs)
    }
    return spec, nil
}
```

#### runDaemon 集成点

在现有 `runDaemon` 中，Bootstrap 调用位于 `srv.ListenAndServe()` 之后：

```go
// cmd/crux/main.go — runDaemon 中
if err := srv.ListenAndServe(socketPath); err != nil {
    return fmt.Errorf("daemon: listen failed: %w", err)
}

// NEW: Init bootstrap sequence
initCfg, err := kernel.LoadInitConfig("crux-init.yaml")
if err != nil {
    // Config parse error → fatal
    srv.Shutdown()
    return fmt.Errorf("daemon: init config error: %w", err)
}
result, err := kernel.Bootstrap(k, initCfg, agentLoader.Load)
if err != nil {
    // Required service/supervisor failed → fatal
    srv.Shutdown()
    os.Remove(socketPath)
    return fmt.Errorf("daemon: bootstrap failed: %w", err)
}
// Print init result summary to stderr
for _, svc := range result.Started {
    fmt.Fprintf(os.Stderr, "[init] ✓ %s\n", svc)
}
for _, warn := range result.Warnings {
    fmt.Fprintf(os.Stderr, "[init] ⚠ %s\n", warn)
}
```

**关键顺序**：`ListenAndServe` → `Bootstrap` → 等待信号。先监听后引导，确保 init 过程中的 SpawnSupervisor 能通过内核操作。如果 Bootstrap 失败，立即清理并返回错误。

### 复用现有代码

**必须复用（不要重新实现）：**
- `kernel/supervisor.go`：`SpawnSupervisor(spec SupervisorSpec)` — Supervisor 树创建
- `skills/loader.go`：`SkillLoader.LoadMetadata()` — Skill 元数据加载
- `drivers/mcp/config.go`：`LoadMCPConfig()` — MCP 配置解析
- `kernel/kernel.go`：`KernelImpl` — 内核实例（传递给 Bootstrap）
- `agents/loader.go`：`AgentLoader.Load()` — Agent 定义加载

**不要修改的现有代码：**
- `kernel/supervisor.go` — Supervisor 已完整实现，直接使用
- `kernel/kernel.go` — 不需要新方法或接口
- `kernel/process.go` — 不涉及
- `kernel/reap.go` — 不涉及
- `ipc/` — IPC 层不需要暴露 Bootstrap 操作
- `compose/` — compose 独立于 init bootstrap

### 修改文件清单

**新文件：**
- `kernel/init.go` — Init 引导核心实现（~200 行）：配置类型 + Bootstrap 函数 + 内置服务
- `kernel/init_test.go` — 8 个测试用例

**修改文件：**
- `cmd/crux/main.go` — runDaemon 中添加 Bootstrap 调用（~20 行新增）

### 测试策略

#### 测试方法

使用 `newTestKernel(t, llmFile)` 创建测试内核（复用现有 pattern），Bootstrap 函数接受注入的 `AgentLoaderFunc`。

**mock 策略：**
- 自定义 `ServiceInitializer` mock：控制 Init 成功/失败
- `AgentLoaderFunc` mock：返回预构建的 `AgentInfo` 或 error
- 直接构建 `InitConfig` 结构体，不依赖文件系统

#### 测试用例分组

| 测试 ID | 类别 | 验证内容 |
|---------|------|---------|
| 10.5-UNIT-001 | P0 | 默认配置（无服务无 Supervisor）→ Bootstrap 成功 |
| 10.5-UNIT-002 | P0 | required 服务失败 → Bootstrap 返回 error |
| 10.5-UNIT-003 | P0 | optional 服务失败 → Bootstrap 成功 + warnings |
| 10.5-UNIT-004 | P0 | Supervisor 树构建成功 |
| 10.5-UNIT-005 | P0 | required Supervisor 构建失败 → Bootstrap 返回 error |
| 10.5-INT-001 | P1 | 混合场景：多服务 + Supervisor → 全部成功 |
| 10.5-INT-002 | P1 | AgentLoaderFunc 加载 agent → ChildSpec.Agent 正确设置 |
| 10.5-REG-001 | P2 | 现有测试全部通过（回归检查） |

### 边界情况

- **crux-init.yaml 不存在**：`LoadInitConfig` 返回空的 `DefaultInitConfig()`，Bootstrap 成功，InitResult 为空。完全向后兼容。
- **空 services 和 supervisors 列表**：Bootstrap 成功，InitResult 为空。
- **scan_path 不存在**：`skillRegistryService` 视为空注册表，不返回 error。
- **mcp.yaml 不存在**：`mcpManagerService` 视为空 MCP 配置，不返回 error。
- **agent 定义不存在**：`agentLoader` 返回 error → 如果该 Supervisor 是 required → Bootstrap 失败。
- **MaxRestarts/MaxWindow 为 0**：`SpawnSupervisor` 内部设默认值（MaxRestarts=3, MaxWindow=60s），不需要在 init 层处理。
- **Strategy 值无效**：`SupervisorSpec` 验证在 `SpawnSupervisor` 中处理，init 层直接转发。
- **多个 Supervisor 部分失败**：required Supervisor 失败时，回滚所有已启动的 Supervisor（Kill + Reap），然后返回 error。

### Story 10.4 关键教训

根据 Story 10.4 的实现经验：
1. **Supervisor 是 kernel Process**：SpawnSupervisor 返回 PID，可通过 `k.Kill(pid, SIGKILL)` 停止。回滚已启动的 Supervisor 时使用此方法。
2. **monitor goroutine 泄漏**：Supervisor 退出前 shutdownAll 确保子进程已退出。Bootstrap 回滚时同样需要等待 Supervisor 完全退出。
3. **intent-router mock 测试模式**：如果 init 测试需要启动真实 Supervisor，复用 Story 10.4 的 intent-router mock 方案。
4. **SpawnSupervisor 不需要 LLM FD**：Supervisor 不调用 LLM，但其子进程需要。测试时需注册 mock LLM 设备。

### Project Structure Notes

- **新文件**：
  - `kernel/init.go` — Init 引导核心（配置类型 + Bootstrap + 内置服务 + 服务注册表）
  - `kernel/init_test.go` — 8 个测试用例
- **修改文件**：
  - `cmd/crux/main.go` — runDaemon 中添加 Bootstrap 调用
- **不修改**：kernel/kernel.go、kernel/supervisor.go、kernel/process.go、kernel/reap.go、ipc/、compose/、drivers/、vfs/、context/、agents/、skills/
- **不需要新依赖**
- **配置文件**：`crux-init.yaml`（可选，不存在时使用默认空配置）

### References

- [Source: _bmad-output/planning-artifacts/epics/epic-10-监控supervisor-与运维monitoring-supervisor-operations.md#Story 10.5]
- [Source: _bmad-output/planning-artifacts/archive/prd.md#FR65 init 引导序列]
- [Source: _bmad-output/planning-artifacts/archive/architecture.md#init (PID=1) 微内核组件]
- [Source: _bmad-output/planning-artifacts/archive/architecture.md#/etc/init.d/ VFS 目录结构]
- [Source: _bmad-output/project-context.md#进程状态机]
- [Source: _bmad-output/project-context.md#依赖方向]
- [Source: _bmad-output/implementation-artifacts/10-4-supervisor-tree-and-restart-strategy.md#SpawnSupervisor 实现]
- [Source: _bmad-output/implementation-artifacts/10-4-supervisor-tree-and-restart-strategy.md#Supervisor 核心类型]
- [Source: _bmad-output/implementation-artifacts/10-4-supervisor-tree-and-restart-strategy.md#CR-1 stale event 修复]
- [Source: cmd/crux/main.go#runDaemon 函数 L704-770]
- [Source: kernel/kernel.go#NewKernel + KernelImpl struct]
- [Source: kernel/supervisor.go#SpawnSupervisor + SupervisorSpec + ChildSpec]
- [Source: kernel/reap.go#Shutdown 方法]
- [Source: skills/loader.go#SkillLoader + LoadMetadata]
- [Source: agents/loader.go#AgentLoader + Load]
- [Source: drivers/mcp/config.go#LoadMCPConfig + MCPGlobalConfig]

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6

### Debug Log References

- SpawnSupervisor 是异步的：子进程启动失败发生在 goroutine 中，Bootstrap 需要通过 `checkSupervisorStartup` 等待并检测启动失败
- 使用 `proc.wg.Wait()` + 200ms 超时来区分启动失败（goroutine 立即退出，exit code != 0）和正常运行（goroutine 在 monitor loop 中阻塞）

### Completion Notes List

- Task 1-5：在 `kernel/init.go` 中实现了所有配置类型（InitConfig, ServiceConfig, SupervisorConfig, ChildConfig）、ServiceInitializer 接口、Bootstrap 核心引擎、3 个内置服务（skillRegistryService, mcpManagerService, logAggregatorService）、服务类型注册表、AgentLoaderFunc 类型
- Task 4：在 `cmd/crux/main.go` runDaemon 中集成 Bootstrap 调用，位于 ListenAndServe 之后、信号等待之前
- Task 6：8 个测试全部通过（5 个 P0 单元测试 + 2 个 P1 集成测试 + 1 个 P2 回归测试）
- 关键设计决策：`checkSupervisorStartup` 函数通过等待 goroutine 完成来检测异步 supervisor 启动失败，使用 200ms 超时避免阻塞正常启动的 supervisor

### Code Review Fixes (CR-2026-03-03)

- **H1 修复**: 为 `LoadInitConfig` 添加 3 个测试（文件不存在/有效YAML解析/无效YAML错误），覆盖 YAML → InitConfig 的完整解析路径，包括 `time.Duration` 反序列化验证
- **M1 修复**: `runDaemon` init config 错误路径补充 `k.Shutdown()` 调用，与 bootstrap 失败路径对齐
- **M2 修复**: `rollbackSupervisors` 中 `k.Kill` 错误不再静默丢弃，改为 stderr 输出 warning
- **M4 修复**: 日志输出格式从 `[init] started:`/`[init] warning:` 改为 `[init] ✓`/`[init] ⚠`，与 Story spec 保持一致

### Deferred Issues (not fixed, documented for future)

- **H2**: `checkSupervisorStartup` 200ms 硬编码超时 — 真实环境可能需要可配置值，但当前 MVP 阶段可接受
- **H3**: `serviceRegistry` 全局可变状态 — 测试注入模式可工作但脆弱，未来重构为参数注入
- **M3**: `TestBootstrap_AgentLoaderFunc_SetsChildAgent` 不直接断言 Agent 字段 — 因 ChildSpec 在 supervisor goroutine 内部使用，外部无法直接验证
- **M5**: 内置服务测试仅验证容错路径 — 需要 fixture 目录才能测试正常扫描逻辑

### File List

- `kernel/init.go` — 新文件：Init 引导核心实现（配置类型 + Bootstrap + 内置服务 + 服务注册表 + checkSupervisorStartup）。CR: rollbackSupervisors Kill 错误日志
- `kernel/init_test.go` — 新文件：11 个测试用例（5 P0 + 2 P1 + 1 P2 + 3 CR-LoadInitConfig）
- `cmd/crux/main.go` — 修改：runDaemon 中添加 Bootstrap 调用。CR: config 错误路径补充 k.Shutdown()；日志格式对齐 spec
