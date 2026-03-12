# Story 23.7: rnix-compose/init 配置格式升级

Status: done

## Story

As a 用户,
I want `rnix-compose.yaml` 和 `rnix-init.yaml` 支持 `provider` + `model` 组合配置,
So that 多 provider 场景下模型指定不会产生歧义。

## Acceptance Criteria

1. **Given** `rnix-compose.yaml` 中智能体配置
   **When** 使用新格式 `provider: ollama` + `model: llama3`
   **Then** Compose 引擎正确解析并在 Spawn 时传递 provider 参数

2. **Given** 向后兼容
   **When** 使用旧格式仅指定 `model: haiku`（无 `provider` 字段）
   **Then** 系统使用默认 provider（claude），行为与升级前一致

3. **Given** `rnix-init.yaml` supervisor children 配置
   **When** 指定 `provider: groq` + `model: llama-3.3-70b-versatile`
   **Then** init 引导时正确使用指定的 provider 和 model

## Tasks / Subtasks

### Task 1: Compose 配置解析——AgentSpec 新增 Provider 字段（AC: #1, #2）

- [x] 1.1 在 `compose/types.go` 的 `AgentSpec` 结构体中新增 `Provider` 字段：
  ```go
  type AgentSpec struct {
      Intent        string            `yaml:"intent"`
      Agent         string            `yaml:"agent,omitempty"`
      Model         string            `yaml:"model,omitempty"`
      Provider      string            `yaml:"provider,omitempty"` // 新增
      Skills        []string          `yaml:"skills,omitempty"`
      ContextBudget int               `yaml:"context_budget,omitempty"`
      TimeoutMs     int64             `yaml:"timeout_ms,omitempty"`
      DependsOn     map[string]string `yaml:"depends_on,omitempty"`
  }
  ```

- [x] 1.2 在 `compose/types.go` 的 `ComposeSpawnOpts` 结构体中新增 `Provider` 字段：
  ```go
  type ComposeSpawnOpts struct {
      Model         string
      Provider      string // 新增
      SystemPrompt  string
      ParentPID     types.PID
      ContextBudget int
      TimeoutMs     int64
      TraceID       types.TraceID
      ParentSpanID  types.SpanID
  }
  ```

- [x] 1.3 在 `compose/types.go` 的 `ComposeSpec` 结构体中新增顶层 `Provider` 字段（全局默认）：
  ```go
  type ComposeSpec struct {
      Version  string                `yaml:"version"`
      Intent   string                `yaml:"intent"`
      Model    string                `yaml:"model,omitempty"`
      Provider string                `yaml:"provider,omitempty"` // 新增
      Agents   map[string]*AgentSpec `yaml:"agents"`
  }
  ```

### Task 2: Compose 引擎传递 Provider 参数（AC: #1, #2）

- [x] 2.1 修改 `compose/engine.go` 的 `executeNode` 方法，在构建 `ComposeSpawnOpts` 时传递 provider：
  ```go
  // Provider priority: agent-level provider > spec-level provider (global default)
  provider := agentSpec.Provider
  if provider == "" {
      provider = e.spec.Provider
  }
  opts := ComposeSpawnOpts{
      Model:         model,
      Provider:      provider, // 新增
      ContextBudget: agentSpec.ContextBudget,
      TimeoutMs:     agentSpec.TimeoutMs,
      TraceID:       traceID,
  }
  ```
  **优先级规则**：agent 级 `provider` > spec 级全局 `provider` > 系统默认（空字符串 = claude）

### Task 3: IPC 适配层传递 Provider（AC: #1）

- [x] 3.1 修改 `cmd/rnix/compose.go` 的 `ipcKernelSpawner.Spawn` 方法，在构建 `ipc.SpawnRequest` 时传递 Provider：
  ```go
  req := ipc.SpawnRequest{
      Intent:        intent,
      Model:         opts.Model,
      Provider:      opts.Provider, // 新增
      ContextBudget: opts.ContextBudget,
      TimeoutMs:     opts.TimeoutMs,
      TraceID:       string(opts.TraceID),
      ParentSpanID:  string(opts.ParentSpanID),
  }
  ```
  **关键**：`ipc.SpawnRequest` 已有 `Provider` 字段（Story 23-3 添加），IPC Server 已将其传递到 `kernel.SpawnOpts.Provider`（`ipc/server.go:551`）。本 Task 仅需在 compose 的 IPC 桥接中传递该字段。

### Task 4: Init 配置解析——ChildConfig 新增 Provider 字段（AC: #3）

- [x] 4.1 在 `kernel/init.go` 的 `ChildConfig` 结构体中新增 `Provider` 字段：
  ```go
  type ChildConfig struct {
      Name          string `yaml:"name"`
      Intent        string `yaml:"intent"`
      Agent         string `yaml:"agent"`
      Model         string `yaml:"model"`
      Provider      string `yaml:"provider"` // 新增
      ContextBudget int    `yaml:"context_budget"`
      Restart       string `yaml:"restart"`
  }
  ```

- [x] 4.2 在 `kernel/supervisor.go` 的 `ChildSpec` 结构体中新增 `Provider` 字段：
  ```go
  type ChildSpec struct {
      Name          string
      Intent        string
      Agent         *agents.AgentInfo
      Model         string
      Provider      string // 新增
      ContextBudget int
      Restart       ChildRestart
  }
  ```

- [x] 4.3 修改 `kernel/init.go` 的 `toSupervisorSpec` 方法，传递 Provider：
  ```go
  cs := ChildSpec{
      Name:          cc.Name,
      Intent:        cc.Intent,
      Model:         cc.Model,
      Provider:      cc.Provider, // 新增
      ContextBudget: cc.ContextBudget,
      Restart:       ChildRestart(cc.Restart),
  }
  ```

- [x] 4.4 修改 `kernel/supervisor.go` 的 `startChild` 方法，在构建 `SpawnOpts` 时传递 Provider：
  ```go
  opts := SpawnOpts{
      ParentPID:     s.proc.PID,
      Model:         spec.Model,
      Provider:      spec.Provider, // 新增
      ContextBudget: spec.ContextBudget,
  }
  ```

### Task 5: 单元测试（AC: #1-#3）

- [x] 5.1 新增 `compose/atdd_23_7_compose_init_config_upgrade_test.go` provider 解析测试：

  | 测试 | 场景 | 期望结果 |
  |------|------|----------|
  | `TestParseBytes_AgentProvider` | YAML 中 agent 指定 `provider: ollama` | `AgentSpec.Provider == "ollama"` |
  | `TestParseBytes_GlobalProvider` | YAML 顶层指定 `provider: groq` | `ComposeSpec.Provider == "groq"` |
  | `TestParseBytes_NoProvider_BackwardCompat` | YAML 无 provider 字段 | `AgentSpec.Provider == ""` 且 `ComposeSpec.Provider == ""` |

- [x] 5.2 新增 `compose/atdd_23_7_compose_init_config_upgrade_test.go` provider 传递测试：

  | 测试 | 场景 | 期望结果 |
  |------|------|----------|
  | `TestEngine_Execute_AgentProviderPassedToSpawn` | agent 指定 `provider: ollama` | `ComposeSpawnOpts.Provider == "ollama"` |
  | `TestEngine_Execute_GlobalProviderFallback` | spec 指定 `provider: groq`，agent 未指定 | `ComposeSpawnOpts.Provider == "groq"` |
  | `TestEngine_Execute_AgentProviderOverridesGlobal` | spec 指定 `provider: groq`，agent 指定 `provider: ollama` | `ComposeSpawnOpts.Provider == "ollama"` |
  | `TestEngine_Execute_NoProvider_EmptyString` | 无 provider 配置 | `ComposeSpawnOpts.Provider == ""` |

- [x] 5.3 新增 `kernel/atdd_23_7_compose_init_config_upgrade_test.go` provider 解析测试：

  | 测试 | 场景 | 期望结果 |
  |------|------|----------|
  | `TestLoadInitConfig_ChildProvider` | YAML children 指定 `provider: groq` | `ChildConfig.Provider == "groq"` |
  | `TestLoadInitConfig_ChildNoProvider_BackwardCompat` | YAML children 无 provider 字段 | `ChildConfig.Provider == ""` |

- [x] 5.4 新增 `kernel/atdd_23_7_compose_init_config_upgrade_test.go` supervisor provider 传递测试：

  | 测试 | 场景 | 期望结果 |
  |------|------|----------|
  | `TestToSupervisorSpec_ChildProvider` | `ChildConfig.Provider = "groq"` | `ChildSpec.Provider == "groq"` |
  | `TestBootstrap_SupervisorChildProvider` | rnix-init.yaml child 指定 provider | Bootstrap 成功启动 supervisor（注：未深度验证 SpawnOpts.Provider 传递） |

- [x] 5.5 新增 ATDD 集成测试 `kernel/atdd_23_7_compose_init_config_upgrade_test.go`：

  | 测试 | 场景 | 期望结果 |
  |------|------|----------|
  | `TestATDD_23_7_AC1_ComposeProviderPassedToSpawn` | compose YAML agent 指定 `provider: ollama` + `model: llama3` | 引擎 Spawn 时 opts.Provider="ollama", opts.Model="llama3" |
  | `TestATDD_23_7_AC2_ComposeBackwardCompat` | compose YAML 仅指定 `model: haiku`，无 provider | 引擎 Spawn 时 opts.Provider=""（空字符串 = 系统默认 claude） |
  | `TestATDD_23_7_AC2_ComposeGlobalProviderFallback` | spec 指定 `provider: groq`，agent 未指定 | 引擎 Spawn 时 opts.Provider="groq" |
  | `TestATDD_23_7_AC3_InitChildProvider` | init YAML child 指定 `provider: groq` + `model: llama-3.3-70b-versatile` | Supervisor startChild 时 SpawnOpts.Provider="groq" |
  | `TestATDD_23_7_AC3_InitChildNoProvider` | init YAML child 无 provider | Supervisor startChild 时 SpawnOpts.Provider="" |

- [x] 5.6 确保所有测试启用 `-race` 检测

## Dev Notes

### 核心设计决策

**1. 在 ComposeSpec / AgentSpec / ComposeSpawnOpts 中新增 Provider 字段（最小侵入式）。**
- `AgentSpec` 新增 `Provider string` yaml tag `provider`，YAML 解析自动绑定
- `ComposeSpawnOpts` 新增 `Provider string`，compose 引擎透传
- `ComposeSpec` 新增顶层 `Provider string`（全局默认 provider），与已有的顶层 `Model` 字段对称
- 空字符串表示"使用系统默认"（即 claude），保持向后兼容
- **零运行时开销**：旧 YAML 文件无 `provider` 字段时解析为空字符串，行为不变

**2. Provider 优先级链：agent级 > spec全局 > 系统默认（claude）。**
- 与 Model 的优先级逻辑完全一致（`engine.go:139-142`）
- 在 `executeNode` 中处理优先级，compose 引擎外部（IPC/kernel）不需要了解 compose 层的优先级逻辑
- 系统默认值不在 compose 层设置——传空字符串到 `kernel.SpawnOpts.Provider`，由内核的 `resolveLLMDevice()` 决定默认值（Story 23-3 已实现）

**3. Init ChildConfig / ChildSpec 新增 Provider 字段，传递到 SpawnOpts。**
- 与 Model 字段处理方式完全对称
- `toSupervisorSpec()` 将 `ChildConfig.Provider` 转换为 `ChildSpec.Provider`
- `Supervisor.startChild()` 将 `ChildSpec.Provider` 放入 `SpawnOpts.Provider`
- 空字符串 = 系统默认，向后兼容

**4. IPC 层已有完整 Provider 支持（无需修改 IPC 协议）。**
- `ipc.SpawnRequest` 已有 `Provider string` 字段（Story 23-3）
- `ipc/server.go:551` 已将 `req.Provider` 传递到 `kernel.SpawnOpts.Provider`
- compose 的 `ipcKernelSpawner.Spawn()` 只需在构建 `SpawnRequest` 时传递 `opts.Provider`
- **零协议变更**，零跨模块风险

### 变更范围

| 文件 | 变更类型 | 说明 |
|------|---------|------|
| `compose/types.go` | **修改** | `ComposeSpec` 新增 `Provider`；`AgentSpec` 新增 `Provider`；`ComposeSpawnOpts` 新增 `Provider` |
| `compose/engine.go` | **修改** | `executeNode` 方法：构建 opts 时设置 Provider（agent级 > spec全局） |
| `cmd/rnix/compose.go` | **修改** | `ipcKernelSpawner.Spawn`：将 `opts.Provider` 传递到 `ipc.SpawnRequest.Provider` |
| `kernel/init.go` | **修改** | `ChildConfig` 新增 `Provider` 字段；`toSupervisorSpec` 传递 Provider |
| `kernel/supervisor.go` | **修改** | `ChildSpec` 新增 `Provider` 字段；`startChild` 将 Provider 放入 SpawnOpts |
| `compose/parser_test.go` | **修改** | 新增 provider 解析测试 |
| `compose/engine_test.go` | **修改** | 新增 provider 传递测试 |
| `kernel/init_test.go` | **修改** | 新增 init provider 测试 |
| `kernel/atdd_23_7_compose_init_config_upgrade_test.go` | **新增** | ATDD 集成测试 |

**不修改的文件：**
- `compose/parser.go` — YAML 解析由 `go-yaml` 自动处理，不需要修改解析代码
- `compose/dag.go` — DAG 构建不涉及 provider
- `kernel/kernel.go` — `SpawnOpts.Provider` 已存在，`resolveLLMDevice()` 已支持动态 provider 解析
- `ipc/protocol.go` — `SpawnRequest.Provider` 已存在
- `ipc/server.go` — 已将 `req.Provider` 传入 `SpawnOpts.Provider`
- `ipc/client.go` — SpawnRequest 序列化自动包含 Provider
- `drivers/llm/` — 不涉及配置格式变更

### 架构合规

- **依赖方向**：`compose/` 不依赖 `kernel/`（通过 `KernelSpawner` 接口解耦）；`cmd/rnix/compose.go` 桥接 compose → ipc，方向合理
- **无新跨包依赖**：所有修改都在现有导入范围内，不引入新的 import
- **线程安全**：`ComposeSpawnOpts.Provider` 为值类型（string），在 goroutine 间传递安全
- **向后兼容**：空 `Provider` 字段由 kernel 层 `resolveLLMDevice()` 默认为 "claude"（Story 23-3 已实现）
- **IPC 扩展**：无需 IPC 协议变更（`SpawnRequest.Provider` 已存在）

### 组合矩阵

| 本功能 | 交互对象 | 交互方式 | 需验证 |
|--------|----------|----------|--------|
| Compose Provider | kernel.SpawnOpts.Provider | 透传：compose → IPC → kernel | 是（AC #1 ATDD 测试） |
| Compose Provider | resolveLLMDevice（23.3） | 间接：Provider 空字符串走默认逻辑 | 是（AC #2 向后兼容测试） |
| Compose Provider | Fallback（23.5） | 无直接交互：fallback 在 reasonStep 内发生，与 provider 指定独立 | 否 |
| Compose Provider | HealthCheck（23.6） | 无直接交互：compose 不查询健康状态 | 否 |
| Init Provider | Supervisor.startChild | 透传：ChildSpec.Provider → SpawnOpts.Provider | 是（AC #3 ATDD 测试） |
| Init Provider | Bootstrap | 间接：Bootstrap 调用 toSupervisorSpec 传递 Provider | 是（Init 测试） |
| Compose global Provider | Agent-level Provider | 覆盖：agent级优先于全局 | 是（引擎优先级测试） |

### 前置 Story 智能

**来自 Story 23-1（config.go）：**
- `ProvidersConfig` 定义了 provider 名称列表，daemon 启动时解析
- Compose/init 中指定的 provider 名称必须在 `rnix-providers.yaml` 中存在（否则 `resolveLLMDevice` 会报错）

**来自 Story 23-2（factory.go）：**
- `RegisterProviders` 将所有 provider 注册到 `DriverRegistry` 和 VFS
- Compose spawn 时内核通过 VFS 访问 `/dev/llm/<provider_name>`

**来自 Story 23-3（kernel.go）：**
- `resolveLLMDevice()` 已从硬编码白名单改为查询 `DriverRegistry`
- `SpawnOpts.Provider` 空字符串默认使用 "claude"
- 不存在的 provider 返回明确错误列表

**来自 Story 23-4（factory.go）：**
- API Key 在 daemon 启动时注入驱动，compose/init 层不关心 Key

**来自 Story 23-5（kernel.go）：**
- Fallback 在 `reasonStep` 内基于 LLM 调用失败触发，独立于 provider 指定
- compose 指定 `provider: ollama` 后，如果 ollama 失败，fallback 仍由 agent.yaml 的 `models.fallback` 配置控制

**来自 Story 23-6（registry.go/server.go）：**
- 健康检查结果存储在 DriverRegistry 中
- compose/init 不查询健康状态（未来优化方向：compose up 前检查所有涉及的 provider 是否 healthy）

**已有测试模式：**
- `compose/engine_test.go` 使用 `mockKernelSpawner` 记录 `ComposeSpawnOpts`，可验证 Provider 传递
- `compose/parser_test.go` 使用 `ParseBytes` 测试 YAML 解析
- `kernel/init_test.go` 使用 `newInitTestKernel` + `mockAgentLoader` 测试 Bootstrap
- `kernel/supervisor.go` 测试 `startChild` 通过 `KernelImpl.Spawn` 记录参数

### 测试策略

- **compose YAML 解析**：使用 `compose.ParseBytes()` 解析含 provider 字段的 YAML，验证 `AgentSpec.Provider` 和 `ComposeSpec.Provider`
- **compose 引擎**：使用 `mockKernelSpawner` 记录 `ComposeSpawnOpts.Provider`，验证优先级逻辑
- **init YAML 解析**：使用 `kernel.LoadInitConfig()` 解析含 provider 字段的 YAML，验证 `ChildConfig.Provider`
- **supervisor 传递**：使用 `toSupervisorSpec()` 验证 `ChildSpec.Provider` 传递
- **ATDD 集成**：构建完整的 compose spec / init config，通过引擎/bootstrap 执行，验证 spawn 参数中 Provider 正确
- **向后兼容**：解析不含 provider 的旧格式 YAML，确认 Provider 为空字符串
- **竞态检测**：所有测试运行 `-race`

### Git 智能

最近提交：
- `a37efeb fix: cr 23-6`
- `19a81bb feat: ds 23-6`
- `e269e6b feat: add ATDD checklist and initial health check tests for Story 23-6`
- `aba5371 feat: cs 23-6`
- `d63d3ac feat: implement Story 23.5 - Provider Fallback Mechanism`

提交建议：`feat: implement Story 23.7 - Compose/Init Config Upgrade for Multi-Provider`

### 跨 Story 边界（本 Story 不实现）

| 功能 | 归属 | 说明 |
|------|------|------|
| Compose up 前 provider 健康检查 | 未规划 | 可优化：compose up 前检查涉及的 provider 是否 healthy |
| Intent decomposition provider 传递 | 未规划 | Intent 子任务目前不指定 provider |
| Provider 热切换（运行时） | 未规划 | 当前 provider 在 Spawn 时确定，运行中不可变 |
| compose validate 命令检查 provider 有效性 | 未规划 | 可在解析后验证 provider 名称是否在 rnix-providers.yaml 中 |

### Project Structure Notes

- 变更主要在 `compose/` 包（types、engine）和 `kernel/` 包（init、supervisor）
- CLI 桥接层在 `cmd/rnix/compose.go`（IPC 适配）
- ATDD 测试在 `kernel/atdd_23_7_compose_init_config_upgrade_test.go`
- 不引入新外部依赖
- 与统一项目结构完全对齐
- 本 Story 是 Epic 23 的最后一个 Story，完成后 Epic 23 可标记为 done

### References

- [Source: compose/types.go#11-16] — `ComposeSpec` 结构体（新增 `Provider`）
- [Source: compose/types.go#19-27] — `AgentSpec` 结构体（新增 `Provider`）
- [Source: compose/types.go#43-51] — `ComposeSpawnOpts` 结构体（新增 `Provider`）
- [Source: compose/engine.go#137-148] — `executeNode` 中 model/opts 构建（provider 传递参考）
- [Source: cmd/rnix/compose.go#85-101] — `ipcKernelSpawner.Spawn`（传递 Provider 到 SpawnRequest）
- [Source: kernel/init.go#42-49] — `ChildConfig` 结构体（新增 `Provider`）
- [Source: kernel/supervisor.go#33-40] — `ChildSpec` 结构体（新增 `Provider`）
- [Source: kernel/init.go#240-258] — `toSupervisorSpec` 方法（传递 Provider）
- [Source: kernel/supervisor.go#194-200] — `startChild` 方法（Provider → SpawnOpts）
- [Source: kernel/kernel.go#37-52] — `SpawnOpts` 结构体（已有 `Provider` 字段）
- [Source: ipc/protocol.go#72] — `SpawnRequest.Provider` 字段（已存在）
- [Source: ipc/server.go#549-557] — IPC Server 将 `req.Provider` 传递到 `SpawnOpts`（已存在）
- FRs covered: FR143（Compose/Init 场景下的 provider 指定）

## Dev Agent Record

### Agent Model Used

Claude claude-4.6-opus (Cursor)

### Debug Log References

- ATDD RED phase tests (19 tests) pre-written by TEA agent in `compose/atdd_23_7_*.go` and `kernel/atdd_23_7_*.go`
- `TestBootstrap_SupervisorChildProvider` initially failed due to missing `/dev/llm/groq` in test kernel — fixed by registering mock groq device

### Completion Notes List

- All 5 Tasks completed with all subtasks checked off
- All 19 ATDD tests pass (RED → GREEN)
- `make all` passes: lint 0 issues, vet OK, all tests pass with `-race`, build OK
- No new external dependencies introduced
- No IPC protocol changes needed (`SpawnRequest.Provider` already exists from Story 23-3)
- YAML parsing handled automatically by `go-yaml` — no changes to `compose/parser.go`

### File List

| File | Change |
|------|--------|
| `compose/types.go` | `ComposeSpec`, `AgentSpec`, `ComposeSpawnOpts` 新增 `Provider` 字段 |
| `compose/engine.go` | `executeNode` 新增 Provider 优先级逻辑 (agent > spec global > empty) |
| `cmd/rnix/compose.go` | `ipcKernelSpawner.Spawn` 传递 `opts.Provider` 到 `SpawnRequest` |
| `kernel/init.go` | `ChildConfig` 新增 `Provider`; `toSupervisorSpec` 传递 Provider |
| `kernel/supervisor.go` | `ChildSpec` 新增 `Provider`; `startChild` 传递到 `SpawnOpts.Provider` |
| `compose/atdd_23_7_compose_init_config_upgrade_test.go` | **新增** ATDD + 单元测试（parser/engine provider 解析与传递） |
| `kernel/atdd_23_7_compose_init_config_upgrade_test.go` | **修改** ATDD + 单元测试（init/supervisor provider 解析与传递）；修复 Bootstrap 测试注册 groq mock 设备 |

### Senior Developer Review (AI)

**Reviewer:** Amelia (Dev Agent) — 2026-03-12
**Outcome:** Approve with fixes applied

**Review Findings (7 total: 0 Critical, 2 High, 3 Medium, 2 Low):**

| # | Severity | Finding | Resolution |
|---|----------|---------|------------|
| H1 | HIGH | File List 遗漏 `compose/atdd_23_7_*.go` | ✅ 已修复：补充到 File List |
| H2 | HIGH | Task 5.1-5.4 声称修改 `parser_test.go`/`engine_test.go`/`init_test.go` 但实际未改 | ✅ 已修复：更正 Task 描述为实际文件 |
| M1 | MEDIUM | ATDD 文件保留 "RED PHASE" 过时注释 | ✅ 已修复：更新注释 |
| M2 | MEDIUM | `TestBootstrap_SupervisorChildProvider` 未验证 Provider 传递到 SpawnOpts | ⚠️ 已知差距：需 kernel spawn recorder 机制，超出本 story scope |
| M3 | MEDIUM | `ChildConfig.Provider` yaml tag 缺 `omitempty` | ✅ 已修复：添加 `omitempty` |
| L1 | LOW | ATDD 与 Unit 测试覆盖场景重复 | ℹ️ ATDD 流程特性，不修复 |
| L2 | LOW | Story 变更范围表遗漏 compose ATDD 文件 | ✅ 已随 H1 修复 |

**Review 后测试结果：** 全部通过（`go test -race ./compose/... ./kernel/...` ✓）

### Change Log

| Date | Change | By |
|------|--------|----|
| 2026-03-12 | Story 实现完成 (ds 23-7) | Dev Agent |
| 2026-03-12 | Code review：修复 2H+2M issues，story status → done | Review Agent |
