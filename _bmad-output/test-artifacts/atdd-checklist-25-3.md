---
stepsCompleted: ['step-01-preflight-and-context', 'step-02-generation-mode', 'step-03-test-strategy', 'step-04-generate-tests']
lastStep: 'step-04-generate-tests'
lastSaved: '2026-03-15'
workflowType: 'testarch-atdd'
inputDocuments:
  - '_bmad-output/planning-artifacts/epics/epic-25-配置系统重构-configuration-system-redesign.md'
  - '_bmad-output/implementation-artifacts/25-2-rnix-init-and-global-config-loading.md'
  - '_bmad-output/test-artifacts/atdd-checklist-25-2.md'
  - '_bmad/tea/testarch/knowledge/data-factories.md'
  - '_bmad/tea/testarch/knowledge/test-quality.md'
  - '_bmad/tea/testarch/knowledge/test-healing-patterns.md'
  - '_bmad/tea/testarch/knowledge/test-levels-framework.md'
  - '_bmad/tea/testarch/knowledge/test-priorities-matrix.md'
---

# ATDD Checklist - Epic 25, Story 3: 项目级配置合并与模块适配

**Date:** 2026-03-15
**Primary Test Level:** Unit + Integration (Go backend)

---

## Story Summary

Story 25.3 实现项目级配置合并与模块适配。CLI 通过 `config.ProjectDir()` 从 CWD 向上发现 `.rnix/` 项目目录，通过 IPC `SpawnRequest.ProjectDir` 字段传入 daemon。daemon 对项目 `.rnix/providers.yaml` 与全局配置进行 `DeepMergeYAML` 合并，`agents/loader.go` 和 `skills/loader.go` 改用 `ShadowResolve` 实现项目级优先加载，`kernel/process.go` 新增 `ProjectConfig` 字段实现每进程不可变配置快照，`drivers/llm/config.go` 迁移到使用 `config` 包路径解析。

**As a** Rnix 用户
**I want** 在项目中使用 `.rnix/` 自定义配置、agent 和 skill，系统自动与全局配置合并
**So that** 不同项目可以有不同的配置和工具集，同一 daemon 支持多项目

---

## Acceptance Criteria

1. **AC1 - IPC SpawnRequest ProjectDir 字段:** Given `ipc/protocol.go` 中 `SpawnRequest` 已新增 `ProjectDir` 字段, When CLI 端在包含 `.rnix/` 的项目中发送 spawn 请求, Then payload 包含 `"project_dir": "/path/to/project"`, And 字段标记 `json:"project_dir,omitempty"`，旧版 CLI 不发送时 daemon 按空处理
2. **AC2 - 项目 providers 合并:** Given 项目 `.rnix/providers.yaml` 新增了一个 provider, When daemon 处理该项目的 spawn 请求, Then `DeepMergeYAML(全局providers, 项目providers)` 生成合并后配置, And 新增的 provider 注册到当前请求的 DriverRegistry
3. **AC3 - Agent ShadowResolve 项目优先:** Given 项目 `.rnix/agents/coder/` 存在，全局 `~/.config/rnix/agents/coder/` 也存在, When spawn 请求指定 `--agent=coder`, Then `agents/loader.go` 通过 `ShadowResolve` 加载项目级 `coder/`, And 全局级 `coder/` 被完全遮蔽
4. **AC4 - Skill ShadowResolve 项目优先:** Given 项目 `.rnix/skills/review/` 存在, When spawn 请求引用 skill `review`, Then `skills/loader.go` 通过 `ShadowResolve` 优先加载项目级
5. **AC5 - Agent 全局回退:** Given 项目中无 `.rnix/agents/planner/` 但全局有, When spawn 请求指定 `--agent=planner`, Then 回退到全局级 `~/.config/rnix/agents/planner/`
6. **AC6 - 多项目进程隔离:** Given daemon 同时服务项目 A 和项目 B, When 项目 A spawn 请求使用 project_dir_A，项目 B 使用 project_dir_B, Then 各进程持有独立的 `ProjectConfig` 快照，互不影响, And `Process.ProjectConfig` 在进程生命周期内不可变
7. **AC7 - CLI ProjectDir 发现:** Given CLI 端从 CWD 向上查找, When 在 `/a/b/c/src/` 工作，`.rnix/` 在 `/a/b/c/`, Then CLI 发现 `project_dir="/a/b/c"` 并通过 IPC 传入
8. **AC8 - CLI 无 .rnix 时空 ProjectDir:** Given CLI 端未找到 `.rnix/`, When 在任意目录发送 spawn 请求, Then `project_dir` 为空，daemon 仅使用全局配置
9. **AC9 - 项目 providers.yaml 语法错误:** Given 项目 `.rnix/providers.yaml` YAML 语法错误, When daemon 处理该项目的 spawn 请求, Then spawn 失败，返回 IPC 错误（文件名 + 错误原因）, And 不影响其他项目的 spawn
10. **AC10 - 运行时数据目录隔离:** Given 运行时数据目录, When 系统写入 records/traces/reputation/immune 数据, Then 存放在 `.rnix/data/` 子目录下，与配置文件物理隔离
11. **AC11 - drivers/llm/config.go 适配:** Given `drivers/llm/config.go` 已适配, When 加载 providers 配置, Then 通过 `config.GlobalDir()` 和 `config.ResolvePath()` 获取路径, And 不再使用旧的 `FindProvidersConfigPath()` 硬编码逻辑
12. **AC12 - Process ProjectConfig 字段:** Given `kernel/process.go` 已适配, When 创建新进程, Then `Process` 结构体包含 `ProjectConfig *config.ProjectConfig` 字段, And 配置快照在 spawn 时生成，进程退出时自然释放

---

## Failing Tests Created (RED Phase)

### Unit Tests (38 tests)

---

#### ipc/protocol_test.go -- AC1: SpawnRequest ProjectDir 字段 (3 tests)

**File:** `ipc/protocol_test.go`

- RED **Test:** TestSpawnRequest_ProjectDirField
  - **Status:** RED -- `SpawnRequest` 无 `ProjectDir` 字段
  - **Verifies:** AC#1 -- `SpawnRequest` 包含 `ProjectDir string` 字段，json tag 为 `"project_dir,omitempty"`
  - **Priority:** P0

- RED **Test:** TestSpawnRequest_ProjectDirSerialization
  - **Status:** RED -- `SpawnRequest` 无 `ProjectDir` 字段
  - **Verifies:** AC#1 -- 序列化时 `project_dir` 字段正确出现在 JSON 中；空值时 omit
  - **Priority:** P0

- RED **Test:** TestSpawnRequest_ProjectDirBackwardCompat
  - **Status:** RED -- `SpawnRequest` 无 `ProjectDir` 字段
  - **Verifies:** AC#1 -- 旧版 JSON（不含 `project_dir`）反序列化后 `ProjectDir` 为空字符串
  - **Priority:** P1

---

#### cmd/rnix/spawn_project_test.go -- AC7, AC8: CLI ProjectDir 发现 (4 tests)

**File:** `cmd/rnix/spawn_project_test.go`

- RED **Test:** TestCLI_ProjectDirDiscovery
  - **Status:** RED -- CLI 未实现 ProjectDir 发现逻辑
  - **Verifies:** AC#7 -- 从子目录向上查找 `.rnix/`，找到后设置 `project_dir`
  - **Priority:** P0

- RED **Test:** TestCLI_ProjectDirDiscovery_DeepNested
  - **Status:** RED -- CLI 未实现 ProjectDir 发现逻辑
  - **Verifies:** AC#7 -- 从深层子目录（如 `/a/b/c/src/pkg/`）向上查找 `.rnix/` 在 `/a/b/c/`
  - **Priority:** P1

- RED **Test:** TestCLI_ProjectDirDiscovery_NoRnixDir
  - **Status:** RED -- CLI 未实现 ProjectDir 发现逻辑
  - **Verifies:** AC#8 -- 无 `.rnix/` 时 `project_dir` 为空字符串
  - **Priority:** P0

- RED **Test:** TestCLI_ProjectDirDiscovery_RootBoundary
  - **Status:** RED -- CLI 未实现 ProjectDir 发现逻辑
  - **Verifies:** AC#8 -- 到达 `$HOME` 或文件系统根时停止搜索，返回空
  - **Priority:** P1

---

#### agents/loader_shadow_test.go -- AC3, AC5: Agent ShadowResolve (5 tests)

**File:** `agents/loader_shadow_test.go`

- RED **Test:** TestAgentLoader_ShadowResolve_ProjectOverridesGlobal
  - **Status:** RED -- `AgentLoader` 未适配多目录 ShadowResolve
  - **Verifies:** AC#3 -- 项目级 agent 目录优先于全局级
  - **Priority:** P0

- RED **Test:** TestAgentLoader_ShadowResolve_FallbackToGlobal
  - **Status:** RED -- `AgentLoader` 未适配多目录 ShadowResolve
  - **Verifies:** AC#5 -- 项目级不存在时回退到全局级
  - **Priority:** P0

- RED **Test:** TestAgentLoader_ShadowResolve_ProjectOnlyAgent
  - **Status:** RED -- `AgentLoader` 未适配多目录 ShadowResolve
  - **Verifies:** AC#3 -- 仅在项目级存在的 agent 正常加载
  - **Priority:** P1

- RED **Test:** TestAgentLoader_ShadowResolve_GlobalOnlyAgent
  - **Status:** RED -- `AgentLoader` 未适配多目录 ShadowResolve
  - **Verifies:** AC#5 -- 仅在全局级存在的 agent 正常加载
  - **Priority:** P1

- RED **Test:** TestAgentLoader_ShadowResolve_NeitherExists
  - **Status:** RED -- `AgentLoader` 未适配多目录 ShadowResolve
  - **Verifies:** AC#3 -- 两级都不存在时返回 "agent not found" 错误
  - **Priority:** P1

---

#### skills/loader_shadow_test.go -- AC4: Skill ShadowResolve (4 tests)

**File:** `skills/loader_shadow_test.go`

- RED **Test:** TestSkillLoader_ShadowResolve_ProjectOverridesGlobal
  - **Status:** RED -- `SkillLoader` 未适配多目录 ShadowResolve
  - **Verifies:** AC#4 -- 项目级 skill 目录优先于全局级
  - **Priority:** P0

- RED **Test:** TestSkillLoader_ShadowResolve_FallbackToGlobal
  - **Status:** RED -- `SkillLoader` 未适配多目录 ShadowResolve
  - **Verifies:** AC#4 -- 项目级不存在时回退到全局级
  - **Priority:** P0

- RED **Test:** TestSkillLoader_ShadowResolve_ProjectOnlySkill
  - **Status:** RED -- `SkillLoader` 未适配多目录 ShadowResolve
  - **Verifies:** AC#4 -- 仅在项目级存在的 skill 正常加载
  - **Priority:** P1

- RED **Test:** TestSkillLoader_ShadowResolve_NeitherExists
  - **Status:** RED -- `SkillLoader` 未适配多目录 ShadowResolve
  - **Verifies:** AC#4 -- 两级都不存在时返回 "skill not found" 错误
  - **Priority:** P1

---

#### cmd/rnix/daemon_merge_test.go -- AC2: 项目 Providers 合并 (5 tests)

**File:** `cmd/rnix/daemon_merge_test.go`

- RED **Test:** TestDaemonMerge_ProjectProvidersAddsProvider
  - **Status:** RED -- daemon 未实现项目配置合并
  - **Verifies:** AC#2 -- 项目 providers.yaml 新增 provider 后合并结果包含该 provider
  - **Priority:** P0

- RED **Test:** TestDaemonMerge_ProjectProvidersOverridesDefault
  - **Status:** RED -- daemon 未实现项目配置合并
  - **Verifies:** AC#2 -- 项目 providers.yaml 中同名 provider 覆盖全局同名 provider 的字段
  - **Priority:** P0

- RED **Test:** TestDaemonMerge_EmptyProjectProviders
  - **Status:** RED -- daemon 未实现项目配置合并
  - **Verifies:** AC#2 -- 项目无 providers.yaml 时使用全局配置，不报错
  - **Priority:** P1

- RED **Test:** TestDaemonMerge_ProjectConfigMerge
  - **Status:** RED -- daemon 未实现项目配置合并
  - **Verifies:** AC#2 -- 项目 config.yaml 与全局 config.yaml 深度合并
  - **Priority:** P1

- RED **Test:** TestDaemonMerge_ProjectMCPMerge
  - **Status:** RED -- daemon 未实现项目 MCP 配置合并
  - **Verifies:** AC#2 -- 项目 mcp.yaml 与全局 mcp.yaml 合并
  - **Priority:** P2

---

#### cmd/rnix/daemon_merge_test.go -- AC9: 项目 providers.yaml 错误处理 (3 tests)

**File:** `cmd/rnix/daemon_merge_test.go`

- RED **Test:** TestDaemonMerge_InvalidProjectProvidersYAML
  - **Status:** RED -- daemon 未实现项目配置合并
  - **Verifies:** AC#9 -- 项目 providers.yaml 语法错误时 spawn 失败，返回 IPC 错误
  - **Priority:** P0

- RED **Test:** TestDaemonMerge_InvalidProjectProvidersContainsFilename
  - **Status:** RED -- daemon 未实现项目配置合并
  - **Verifies:** AC#9 -- 错误信息包含项目 providers.yaml 的文件名
  - **Priority:** P1

- RED **Test:** TestDaemonMerge_InvalidProjectDoesNotAffectOthers
  - **Status:** RED -- daemon 未实现项目配置合并
  - **Verifies:** AC#9 -- 项目 A 配置错误不影响项目 B 的 spawn
  - **Priority:** P1

---

#### kernel/process_project_test.go -- AC6, AC12: Process ProjectConfig (5 tests)

**File:** `kernel/process_project_test.go`

- RED **Test:** TestProcess_HasProjectConfigField
  - **Status:** RED -- `Process` 无 `ProjectConfig` 字段
  - **Verifies:** AC#12 -- `Process` 结构体包含 `ProjectConfig *config.ProjectConfig` 字段
  - **Priority:** P0

- RED **Test:** TestProcess_ProjectConfigSetAtSpawn
  - **Status:** RED -- `Process` 无 `ProjectConfig` 字段
  - **Verifies:** AC#12 -- ProjectConfig 在 spawn 时设置，非 nil（有项目时）或 nil（无项目时）
  - **Priority:** P0

- RED **Test:** TestProcess_ProjectConfigImmutable
  - **Status:** RED -- `Process` 无 `ProjectConfig` 字段
  - **Verifies:** AC#6 -- ProjectConfig 指针在进程生命周期内不可变（即不会被重新赋值）
  - **Priority:** P0

- RED **Test:** TestProcess_MultiProjectIsolation
  - **Status:** RED -- `Process` 无 `ProjectConfig` 字段
  - **Verifies:** AC#6 -- 不同 ProjectDir 的进程持有独立的 ProjectConfig 快照
  - **Priority:** P0

- RED **Test:** TestProcess_NilProjectConfigForGlobalOnly
  - **Status:** RED -- `Process` 无 `ProjectConfig` 字段
  - **Verifies:** AC#8 -- 无 project_dir 时 ProjectConfig 为 nil，进程正常运行
  - **Priority:** P1

---

#### drivers/llm/config_migration_test.go -- AC11: LLM Config 路径迁移 (4 tests)

**File:** `drivers/llm/config_migration_test.go`

- RED **Test:** TestLLMConfig_UsesConfigGlobalDir
  - **Status:** RED -- `drivers/llm/config.go` 仍使用硬编码路径
  - **Verifies:** AC#11 -- 加载 providers 配置时通过 `config.GlobalDir()` 获取路径
  - **Priority:** P0

- RED **Test:** TestLLMConfig_UsesResolvePath
  - **Status:** RED -- `drivers/llm/config.go` 仍使用 `FindProvidersConfigPath()`
  - **Verifies:** AC#11 -- 通过 `config.ResolvePath()` 解析配置文件路径
  - **Priority:** P0

- RED **Test:** TestLLMConfig_FindProvidersConfigPathDeprecated
  - **Status:** RED -- `FindProvidersConfigPath()` 仍存在
  - **Verifies:** AC#11 -- `FindProvidersConfigPath()` 不再被调用或已移除
  - **Priority:** P1

- RED **Test:** TestLLMConfig_ProjectScopeResolvePath
  - **Status:** RED -- 未实现项目级 providers 路径解析
  - **Verifies:** AC#11 -- 项目级 providers 路径通过 `config.ResolvePath(ScopeProject, ...)` 获取
  - **Priority:** P1

---

#### cmd/rnix/daemon_merge_test.go -- AC10: 运行时数据目录 (2 tests)

**File:** `cmd/rnix/daemon_merge_test.go`

- RED **Test:** TestDaemonMerge_RuntimeDataInProjectDataDir
  - **Status:** RED -- 运行时数据路径变更已在 25-2 完成，此处验证项目级数据隔离
  - **Verifies:** AC#10 -- 项目运行时数据存放在 `.rnix/data/` 子目录下
  - **Priority:** P1

- RED **Test:** TestDaemonMerge_RuntimeDataSeparatedFromConfig
  - **Status:** RED -- 未验证配置与数据的物理隔离
  - **Verifies:** AC#10 -- `.rnix/data/` 与 `.rnix/config.yaml` 等配置文件物理隔离
  - **Priority:** P2

---

#### cmd/rnix/daemon_merge_test.go -- AC6: 多项目 spawn 隔离 (3 tests)

**File:** `cmd/rnix/daemon_merge_test.go`

- RED **Test:** TestDaemonMerge_ConcurrentSpawnDifferentProjects
  - **Status:** RED -- daemon 未实现项目配置合并
  - **Verifies:** AC#6 -- 并发 spawn 不同项目的请求互不干扰
  - **Priority:** P0

- RED **Test:** TestDaemonMerge_ProjectADoesNotSeeProjectBConfig
  - **Status:** RED -- daemon 未实现项目配置合并
  - **Verifies:** AC#6 -- 项目 A 的 providers 不泄漏到项目 B 的进程
  - **Priority:** P0

- RED **Test:** TestDaemonMerge_EmptyProjectDirUsesGlobalOnly
  - **Status:** RED -- daemon 未实现项目配置合并
  - **Verifies:** AC#8 -- project_dir 为空时仅使用全局配置
  - **Priority:** P1

---

## AC <-> Test 覆盖矩阵

| AC | Test(s) | 覆盖方式 |
|----|---------|----------|
| AC1 IPC ProjectDir 字段 | TestSpawnRequest_ProjectDirField, TestSpawnRequest_ProjectDirSerialization, TestSpawnRequest_ProjectDirBackwardCompat | 字段存在 + JSON 序列化 + 向后兼容 |
| AC2 项目 Providers 合并 | TestDaemonMerge_ProjectProvidersAddsProvider, TestDaemonMerge_ProjectProvidersOverridesDefault, TestDaemonMerge_EmptyProjectProviders, TestDaemonMerge_ProjectConfigMerge, TestDaemonMerge_ProjectMCPMerge | 新增/覆盖/空值/config 合并/MCP 合并 |
| AC3 Agent ShadowResolve | TestAgentLoader_ShadowResolve_ProjectOverridesGlobal, TestAgentLoader_ShadowResolve_ProjectOnlyAgent, TestAgentLoader_ShadowResolve_NeitherExists | 项目优先/仅项目/均不存在 |
| AC4 Skill ShadowResolve | TestSkillLoader_ShadowResolve_ProjectOverridesGlobal, TestSkillLoader_ShadowResolve_FallbackToGlobal, TestSkillLoader_ShadowResolve_ProjectOnlySkill, TestSkillLoader_ShadowResolve_NeitherExists | 项目优先/全局回退/仅项目/均不存在 |
| AC5 Agent 全局回退 | TestAgentLoader_ShadowResolve_FallbackToGlobal, TestAgentLoader_ShadowResolve_GlobalOnlyAgent | 全局回退/仅全局 |
| AC6 多项目隔离 | TestProcess_ProjectConfigImmutable, TestProcess_MultiProjectIsolation, TestDaemonMerge_ConcurrentSpawnDifferentProjects, TestDaemonMerge_ProjectADoesNotSeeProjectBConfig | 不可变/独立快照/并发隔离/配置不泄漏 |
| AC7 CLI ProjectDir 发现 | TestCLI_ProjectDirDiscovery, TestCLI_ProjectDirDiscovery_DeepNested | 基本发现 + 深层嵌套 |
| AC8 CLI 无 .rnix | TestCLI_ProjectDirDiscovery_NoRnixDir, TestCLI_ProjectDirDiscovery_RootBoundary, TestProcess_NilProjectConfigForGlobalOnly, TestDaemonMerge_EmptyProjectDirUsesGlobalOnly | 空 project_dir + 根边界 + nil ProjectConfig + 仅全局 |
| AC9 项目 providers 错误 | TestDaemonMerge_InvalidProjectProvidersYAML, TestDaemonMerge_InvalidProjectProvidersContainsFilename, TestDaemonMerge_InvalidProjectDoesNotAffectOthers | 错误返回 + 文件名 + 隔离 |
| AC10 运行时数据隔离 | TestDaemonMerge_RuntimeDataInProjectDataDir, TestDaemonMerge_RuntimeDataSeparatedFromConfig | 数据目录 + 物理隔离 |
| AC11 LLM config 迁移 | TestLLMConfig_UsesConfigGlobalDir, TestLLMConfig_UsesResolvePath, TestLLMConfig_FindProvidersConfigPathDeprecated, TestLLMConfig_ProjectScopeResolvePath | GlobalDir + ResolvePath + 旧函数废弃 + 项目级路径 |
| AC12 Process ProjectConfig | TestProcess_HasProjectConfigField, TestProcess_ProjectConfigSetAtSpawn, TestProcess_ProjectConfigImmutable, TestProcess_MultiProjectIsolation, TestProcess_NilProjectConfigForGlobalOnly | 字段存在 + spawn 设置 + 不可变 + 多项目 + nil |

---

## 测试隔离策略

- **临时目录:** 所有文件系统操作通过 `t.TempDir()` 创建临时目录，测试完自动清理
- **环境变量:** `t.Setenv("XDG_CONFIG_HOME", ...)` 和 `t.Setenv("HOME", ...)` 隔离全局路径
- **工作目录隔离:** 使用 `t.Chdir()` (Go 1.24+) 隔离 CWD 依赖操作
- **并行安全:** 所有测试标记 `t.Parallel()`（除需要 `os.Chdir()` 的测试外）
- **Race 检测:** 通过 `go test -race` 运行
- **Agent/Skill 文件模拟:** 在临时目录中创建最小化的 `agent.yaml`/`SKILL.md` 文件
- **IPC 测试:** 使用序列化/反序列化验证协议变更，不启动完整 daemon
- **Kernel 测试:** 直接构造 `Process` 结构体验证字段，提取配置合并逻辑为可测试函数

---

## 实现目标文件

| 文件 | 状态 | 说明 |
|------|------|------|
| `ipc/protocol.go` | 待修改 | SpawnRequest 新增 `ProjectDir` 字段 |
| `agents/loader.go` | 待修改 | 签名变更，改用 ShadowResolve + 多目录查找 |
| `skills/loader.go` | 待修改 | 签名变更，改用 ShadowResolve + 多目录查找 |
| `drivers/llm/config.go` | 待修改 | FindProvidersConfigPath 迁移到 config 包 |
| `kernel/process.go` | 待修改 | Process 新增 ProjectConfig 字段 |
| `kernel/init.go` | 待修改 | LoadInitConfig 使用 config.ResolvePath |
| `cmd/rnix/main.go` | 待修改 | CLI ProjectDir 发现 + daemon spawn handler 项目合并 |

---

## 测试优先级分布

| 优先级 | 数量 | 测试 |
|--------|------|------|
| P0 | 16 | SpawnRequest_ProjectDirField, SpawnRequest_ProjectDirSerialization, CLI_ProjectDirDiscovery, CLI_ProjectDirDiscovery_NoRnixDir, AgentLoader_ShadowResolve_ProjectOverridesGlobal, AgentLoader_ShadowResolve_FallbackToGlobal, SkillLoader_ShadowResolve_ProjectOverridesGlobal, SkillLoader_ShadowResolve_FallbackToGlobal, DaemonMerge_ProjectProvidersAddsProvider, DaemonMerge_ProjectProvidersOverridesDefault, DaemonMerge_InvalidProjectProvidersYAML, Process_HasProjectConfigField, Process_ProjectConfigSetAtSpawn, Process_ProjectConfigImmutable, Process_MultiProjectIsolation, DaemonMerge_ConcurrentSpawnDifferentProjects, DaemonMerge_ProjectADoesNotSeeProjectBConfig, LLMConfig_UsesConfigGlobalDir, LLMConfig_UsesResolvePath |
| P1 | 17 | SpawnRequest_ProjectDirBackwardCompat, CLI_ProjectDirDiscovery_DeepNested, CLI_ProjectDirDiscovery_RootBoundary, AgentLoader_ShadowResolve_ProjectOnlyAgent, AgentLoader_ShadowResolve_GlobalOnlyAgent, AgentLoader_ShadowResolve_NeitherExists, SkillLoader_ShadowResolve_ProjectOnlySkill, SkillLoader_ShadowResolve_NeitherExists, DaemonMerge_EmptyProjectProviders, DaemonMerge_ProjectConfigMerge, DaemonMerge_InvalidProjectProvidersContainsFilename, DaemonMerge_InvalidProjectDoesNotAffectOthers, Process_NilProjectConfigForGlobalOnly, DaemonMerge_RuntimeDataInProjectDataDir, DaemonMerge_EmptyProjectDirUsesGlobalOnly, LLMConfig_FindProvidersConfigPathDeprecated, LLMConfig_ProjectScopeResolvePath |
| P2 | 3 | DaemonMerge_ProjectMCPMerge, DaemonMerge_RuntimeDataSeparatedFromConfig |

---

## Running Tests

```bash
# Run all tests for affected packages
go test -race ./ipc/... ./agents/... ./skills/... ./kernel/... ./drivers/llm/... ./cmd/rnix/...

# Run specific test file
go test -race -run TestSpawnRequest ./ipc/...
go test -race -run TestAgentLoader_ShadowResolve ./agents/...
go test -race -run TestSkillLoader_ShadowResolve ./skills/...
go test -race -run TestProcess_ProjectConfig ./kernel/...
go test -race -run TestDaemonMerge ./cmd/rnix/...
go test -race -run TestLLMConfig ./drivers/llm/...
go test -race -run TestCLI_ProjectDir ./cmd/rnix/...

# Run all project tests
make test

# Full validation
make all
```

---

## Red-Green-Refactor Workflow

### RED Phase (Current)

**TEA Agent Responsibilities:**

- All 38 tests designed and documented as failing
- Test isolation strategies defined
- AC coverage matrix complete (12 AC mapped)
- Implementation target files identified

**Verification:**

- All tests will fail due to missing implementation
- Failure reasons are clear: missing struct fields, missing function parameters, hardcoded paths
- Tests fail due to missing implementation, not test bugs

---

### GREEN Phase (DEV Team - Next Steps)

**DEV Agent Responsibilities:**

1. **Start with AC1** (IPC protocol change) - foundation for all other changes
2. **Then AC12** (Process.ProjectConfig field) - required by merge logic
3. **Then AC11** (LLM config migration) - cleanup hardcoded paths
4. **Then AC3, AC4, AC5** (Agent/Skill ShadowResolve) - loader adaptation
5. **Then AC2, AC9** (Daemon merge logic) - project config merge
6. **Then AC7, AC8** (CLI discovery) - wire up ProjectDir in CLI
7. **Finally AC6, AC10** (isolation/data) - integration verification

**Key Principles:**

- One AC at a time
- Run tests after each change
- Use `go test -race` to catch concurrency issues

---

### REFACTOR Phase (DEV Team - After All Tests Pass)

1. Verify all 38 tests pass
2. Remove any backward-compatible shims if no longer needed
3. Ensure `make all` passes (lint + vet + test + build)
4. Update story status

---

## Knowledge Base References Applied

- **test-levels-framework.md** - Unit for pure functions (ShadowResolve, DeepMergeYAML), Integration for IPC protocol and loader adaptation
- **test-priorities-matrix.md** - P0 for data integrity (config isolation), P1 for core user journeys (config merge)
- **data-factories.md** - Test data patterns (minimal agent.yaml, SKILL.md fixtures in temp dirs)
- **test-quality.md** - One assertion per test, determinism, isolation via t.TempDir()
- **test-healing-patterns.md** - Avoid flaky tests by using deterministic paths and env var isolation

---

## Notes

- Story 25-3 依赖 Story 25-1（config 包）和 25-2（rnix init + 全局配置加载），两者均已完成
- `AgentLoader` 和 `SkillLoader` 签名变更会影响所有调用方（`cmd/rnix/main.go` 中的 `runDaemon()`）
- 合并逻辑需要将 `ProvidersConfig` 转换为 `map[string]any` 再调用 `DeepMergeYAML`，或实现专用合并函数
- 多项目并发隔离测试需使用 goroutine + sync.WaitGroup 验证
- `FindProvidersConfigPath()` 迁移后需确认无其他调用方

---

**Generated by BMad TEA Agent** - 2026-03-15
