---
stepsCompleted: ['step-01-preflight-and-context', 'step-02-generation-mode', 'step-03-test-strategy', 'step-04-generate-tests']
lastStep: 'step-04-generate-tests'
lastSaved: '2026-03-15'
workflowType: 'testarch-atdd'
inputDocuments:
  - '_bmad-output/implementation-artifacts/25-2-rnix-init-and-global-config-loading.md'
  - '_bmad-output/implementation-artifacts/25-1-internal-config-package-and-embed-fs.md'
  - '_bmad/tea/testarch/knowledge/data-factories.md'
  - '_bmad/tea/testarch/knowledge/test-quality.md'
  - '_bmad/tea/testarch/knowledge/test-healing-patterns.md'
  - '_bmad/tea/testarch/knowledge/test-levels-framework.md'
  - '_bmad/tea/testarch/knowledge/test-priorities-matrix.md'
---

# ATDD Checklist - Epic 25, Story 2: rnix init 与全局配置加载

**Date:** 2026-03-15
**Primary Test Level:** Unit + Integration (Go backend)

---

## Story Summary

Story 25.2 实现 `rnix init` 命令和 daemon 全局配置加载。`rnix init` 自动初始化全局目录（`~/.config/rnix/`）和项目目录（`.rnix/`），从 embed.FS 提取内置 agent/skill，生成默认配置文件。daemon 启动时从全局配置目录加载 `providers.yaml`、`config.yaml`、`mcp.yaml`，替代原有的硬编码路径。

**As a** Rnix 用户
**I want** 运行 `rnix init` 获得完整的全局和项目配置环境，daemon 从新目录加载配置
**So that** 安装后即可使用，内置 agent/skill 可自由定制，配置集中管理

---

## Acceptance Criteria

1. **AC1 - 全局目录初始化:** Given 全局目录 `~/.config/rnix/` 不存在, When 运行 `rnix init`, Then 创建 `~/.config/rnix/` 目录及子目录（agents/、skills/），从 embed.FS 提取内置 agent/skill，生成默认 `providers.yaml` 和 `config.yaml`，执行时间 <= 3 秒
2. **AC2 - 全局目录幂等:** Given 全局目录 `~/.config/rnix/` 已存在且包含用户修改的 agent, When 运行 `rnix init`, Then 跳过已存在的文件和目录，不覆盖用户修改，输出 "skipped" 提示
3. **AC3 - 项目目录初始化:** Given 当前目录下无 `.rnix/` 目录, When 运行 `rnix init`, Then 在当前目录创建 `.rnix/` 及子目录（agents/、skills/、data/），生成空 `config.yaml`
4. **AC4 - 项目目录幂等:** Given 当前目录已有 `.rnix/`, When 运行 `rnix init`, Then 跳过项目初始化，输出提示信息，所有操作幂等
5. **AC5 - daemon 全局配置加载:** Given daemon 启动, When 加载全局配置, Then 从 `~/.config/rnix/` 读取 `providers.yaml`、`config.yaml`、`mcp.yaml`，解析并缓存为 `GlobalConfig` 结构体，注册全局 LLM 驱动到 `DriverRegistry`
6. **AC6 - providers.yaml 不存在容错:** Given 全局 `providers.yaml` 不存在, When daemon 启动, Then 使用内置默认配置 + 输出 info 日志，不崩溃
7. **AC7 - providers.yaml 语法错误:** Given 全局 `providers.yaml` YAML 语法错误, When daemon 启动, Then 启动失败，输出详细错误信息（文件名、行号、错误原因）

---

## Failing Tests Created (RED Phase)

### Unit Tests (31 tests)

---

#### init_test.go -- AC1: 全局目录初始化 (6 tests)

**File:** `cmd/rnix/init_test.go`

- RED **Test:** TestInitGlobal_CreatesDirectories
  - **Status:** RED -- `initGlobal` 函数未定义
  - **Verifies:** AC#1 -- 创建 `agents/`、`skills/` 子目录
  - **Priority:** P0

- RED **Test:** TestInitGlobal_ExtractsEmbeddedAgents
  - **Status:** RED -- `initGlobal` 函数未定义
  - **Verifies:** AC#1 -- 从 embed.FS 提取内置 agent 文件到 `agents/` 目录
  - **Priority:** P0

- RED **Test:** TestInitGlobal_ExtractsEmbeddedSkills
  - **Status:** RED -- `initGlobal` 函数未定义
  - **Verifies:** AC#1 -- 从 embed.FS 提取内置 skill 文件到 `skills/` 目录
  - **Priority:** P0

- RED **Test:** TestInitGlobal_GeneratesProvidersYAML
  - **Status:** RED -- `writeDefaultProviders` 函数未定义
  - **Verifies:** AC#1 -- 生成 `providers.yaml` 且内容为有效 YAML
  - **Priority:** P0

- RED **Test:** TestInitGlobal_GeneratesConfigYAML
  - **Status:** RED -- `writeDefaultConfig` 函数未定义
  - **Verifies:** AC#1 -- 生成 `config.yaml`
  - **Priority:** P1

- RED **Test:** TestInitGlobal_ProvidersContent
  - **Status:** RED -- `writeDefaultProviders` 函数未定义
  - **Verifies:** AC#1 -- `providers.yaml` 内容包含 `default_provider` 字段
  - **Priority:** P1

---

#### init_test.go -- AC2: 全局目录幂等 (3 tests)

- RED **Test:** TestInitGlobal_SkipExistingFiles
  - **Status:** RED -- `initGlobal` 函数未定义
  - **Verifies:** AC#2 -- 已存在的文件不被覆盖
  - **Priority:** P0

- RED **Test:** TestInitGlobal_SkipExistingProvidersYAML
  - **Status:** RED -- `writeDefaultProviders` 函数未定义
  - **Verifies:** AC#2 -- 已存在的 `providers.yaml` 不被覆盖
  - **Priority:** P0

- RED **Test:** TestInitGlobal_SkipExistingConfigYAML
  - **Status:** RED -- `writeDefaultConfig` 函数未定义
  - **Verifies:** AC#2 -- 已存在的 `config.yaml` 不被覆盖
  - **Priority:** P1

---

#### init_test.go -- AC3: 项目目录初始化 (4 tests)

- RED **Test:** TestInitProject_CreatesDirectories
  - **Status:** RED -- `initProject` 函数未定义
  - **Verifies:** AC#3 -- 创建 `.rnix/agents/`、`.rnix/skills/`、`.rnix/data/` 子目录
  - **Priority:** P0

- RED **Test:** TestInitProject_GeneratesConfigYAML
  - **Status:** RED -- `initProject` 函数未定义
  - **Verifies:** AC#3 -- 生成 `.rnix/config.yaml` 且包含注释说明
  - **Priority:** P1

- RED **Test:** TestInitProject_DataSubdirectories
  - **Status:** RED -- `initProject` 函数未定义
  - **Verifies:** AC#3 -- `.rnix/data/` 目录结构正确
  - **Priority:** P1

- RED **Test:** TestInitProject_DirectoryPermissions
  - **Status:** RED -- `initProject` 函数未定义
  - **Verifies:** AC#3 -- 目录权限为 0o755
  - **Priority:** P2

---

#### init_test.go -- AC4: 项目目录幂等 (2 tests)

- RED **Test:** TestInitProject_SkipExisting
  - **Status:** RED -- `initProject` 函数未定义
  - **Verifies:** AC#4 -- `.rnix/` 已存在时跳过初始化
  - **Priority:** P0

- RED **Test:** TestInitProject_IdempotentRun
  - **Status:** RED -- `initProject` 函数未定义
  - **Verifies:** AC#4 -- 连续运行两次结果一致
  - **Priority:** P1

---

#### init_test.go -- AC1/AC3: runInit 集成 (3 tests)

- RED **Test:** TestRunInit_BothGlobalAndProject
  - **Status:** RED -- `runInit` 函数未定义
  - **Verifies:** AC#1, AC#3 -- 先初始化全局再初始化项目
  - **Priority:** P0

- RED **Test:** TestRunInit_GlobalExistsProjectNew
  - **Status:** RED -- `runInit` 函数未定义
  - **Verifies:** AC#2, AC#3 -- 全局已存在时跳过全局、初始化项目
  - **Priority:** P1

- RED **Test:** TestRunInit_BothExist
  - **Status:** RED -- `runInit` 函数未定义
  - **Verifies:** AC#2, AC#4 -- 全局和项目都存在时全部跳过
  - **Priority:** P1

---

#### init_test.go -- AC2: initCmd 命令注册 (1 test)

- RED **Test:** TestInitCmd_Registered
  - **Status:** RED -- `initCmd` 变量未定义
  - **Verifies:** AC#1 -- initCmd 已注册到 rootCmd
  - **Priority:** P1

---

#### daemon 配置加载测试 -- AC5: daemon 全局配置加载 (4 tests)

**File:** `cmd/rnix/init_test.go`（或新文件 `cmd/rnix/daemon_config_test.go`）

- RED **Test:** TestDaemonConfig_LoadProvidersFromGlobal
  - **Status:** RED -- daemon 配置加载逻辑未修改
  - **Verifies:** AC#5 -- 从全局目录加载 `providers.yaml`
  - **Priority:** P0

- RED **Test:** TestDaemonConfig_LoadSkillsFromGlobal
  - **Status:** RED -- `skills.NewSkillLoader` 路径未修改
  - **Verifies:** AC#5 -- `skillLoader` 使用全局 skills 目录
  - **Priority:** P0

- RED **Test:** TestDaemonConfig_LoadAgentsFromGlobal
  - **Status:** RED -- `agents.NewAgentLoader` 路径未修改
  - **Verifies:** AC#5 -- `agentLoader` 使用全局 agents 目录
  - **Priority:** P0

- RED **Test:** TestDaemonConfig_LoadMCPFromGlobal
  - **Status:** RED -- `os.Stat("mcp.yaml")` 路径未修改
  - **Verifies:** AC#5 -- MCP 配置从全局目录加载
  - **Priority:** P1

---

#### daemon 配置加载测试 -- AC6: providers.yaml 不存在 (2 tests)

- RED **Test:** TestDaemonConfig_MissingProviders_UsesDefault
  - **Status:** RED -- daemon 配置加载逻辑未修改
  - **Verifies:** AC#6 -- 全局 `providers.yaml` 不存在时使用内置默认配置
  - **Priority:** P0

- RED **Test:** TestDaemonConfig_MissingProviders_NoCrash
  - **Status:** RED -- daemon 配置加载逻辑未修改
  - **Verifies:** AC#6 -- 全局 `providers.yaml` 不存在时不崩溃
  - **Priority:** P0

---

#### daemon 配置加载测试 -- AC7: providers.yaml 语法错误 (2 tests)

- RED **Test:** TestDaemonConfig_InvalidProvidersYAML_ReturnsError
  - **Status:** RED -- daemon 配置加载逻辑未修改
  - **Verifies:** AC#7 -- YAML 语法错误时返回详细错误
  - **Priority:** P0

- RED **Test:** TestDaemonConfig_InvalidProvidersYAML_ContainsFilename
  - **Status:** RED -- daemon 配置加载逻辑未修改
  - **Verifies:** AC#7 -- 错误信息包含文件名
  - **Priority:** P1

---

#### daemon 路径迁移测试 -- AC5: 运行时数据路径变更 (4 tests)

- RED **Test:** TestDaemonConfig_RecordsDirMigrated
  - **Status:** RED -- `recordBaseDir` 路径未修改
  - **Verifies:** AC#5 -- records 目录从 `.rnix/records` 迁移到 `.rnix/data/records`
  - **Priority:** P1

- RED **Test:** TestDaemonConfig_ReputationDirMigrated
  - **Status:** RED -- `reputationDir` 路径未修改
  - **Verifies:** AC#5 -- reputation 目录迁移到 `.rnix/data/reputation`
  - **Priority:** P1

- RED **Test:** TestDaemonConfig_ImmuneDirMigrated
  - **Status:** RED -- `immuneDir` 路径未修改
  - **Verifies:** AC#5 -- immune 目录迁移到 `.rnix/data/immune`
  - **Priority:** P1

- RED **Test:** TestDaemonConfig_TracesDirMigrated
  - **Status:** RED -- `traceBaseDir` 路径未修改
  - **Verifies:** AC#5 -- traces 目录迁移到 `.rnix/data/traces`
  - **Priority:** P1

---

## AC <-> Test 覆盖矩阵

| AC | Test(s) | 覆盖方式 |
|----|---------|----------|
| AC1 全局目录初始化 | TestInitGlobal_CreatesDirectories, TestInitGlobal_ExtractsEmbeddedAgents, TestInitGlobal_ExtractsEmbeddedSkills, TestInitGlobal_GeneratesProvidersYAML, TestInitGlobal_GeneratesConfigYAML, TestInitGlobal_ProvidersContent, TestRunInit_BothGlobalAndProject | 目录创建 + embed 提取 + 配置生成 + 完整流程 |
| AC2 全局目录幂等 | TestInitGlobal_SkipExistingFiles, TestInitGlobal_SkipExistingProvidersYAML, TestInitGlobal_SkipExistingConfigYAML, TestRunInit_GlobalExistsProjectNew, TestRunInit_BothExist | 文件跳过 + 集成场景 |
| AC3 项目目录初始化 | TestInitProject_CreatesDirectories, TestInitProject_GeneratesConfigYAML, TestInitProject_DataSubdirectories, TestInitProject_DirectoryPermissions, TestRunInit_BothGlobalAndProject | 目录创建 + 配置生成 + 权限 |
| AC4 项目目录幂等 | TestInitProject_SkipExisting, TestInitProject_IdempotentRun, TestRunInit_BothExist | 跳过 + 幂等验证 |
| AC5 daemon 全局配置加载 | TestDaemonConfig_LoadProvidersFromGlobal, TestDaemonConfig_LoadSkillsFromGlobal, TestDaemonConfig_LoadAgentsFromGlobal, TestDaemonConfig_LoadMCPFromGlobal, TestDaemonConfig_RecordsDirMigrated, TestDaemonConfig_ReputationDirMigrated, TestDaemonConfig_ImmuneDirMigrated, TestDaemonConfig_TracesDirMigrated | 全局加载 + 路径迁移 |
| AC6 providers.yaml 不存在 | TestDaemonConfig_MissingProviders_UsesDefault, TestDaemonConfig_MissingProviders_NoCrash | 默认配置回退 + 不崩溃 |
| AC7 providers.yaml 语法错误 | TestDaemonConfig_InvalidProvidersYAML_ReturnsError, TestDaemonConfig_InvalidProvidersYAML_ContainsFilename | 错误返回 + 错误详情 |

---

## 测试隔离策略

- **临时目录:** 所有文件系统操作通过 `t.TempDir()` 创建临时目录，测试完自动清理
- **环境变量:** `t.Setenv("XDG_CONFIG_HOME", ...)` 和 `t.Setenv("HOME", ...)` 隔离全局路径
- **embed.FS 模拟:** 使用 `testing/fstest.MapFS` 替代真实 embed.FS
- **工作目录隔离:** 使用 `t.Chdir()` (Go 1.24+) 或 `os.Chdir()` + cleanup 隔离项目目录操作
- **并行安全:** 所有测试标记 `t.Parallel()`（除需要 `os.Chdir()` 的测试外）
- **Race 检测:** 通过 `go test -race` 运行
- **daemon 测试:** 提取 daemon 配置加载逻辑为可测试的函数，避免启动完整 daemon

---

## 实现目标文件

| 文件 | 状态 | 说明 |
|------|------|------|
| `cmd/rnix/init.go` | 待创建 | rnix init 命令实现（initCmd, runInit, initGlobal, initProject, writeDefaultProviders, writeDefaultConfig） |
| `cmd/rnix/init_test.go` | 待创建 (RED) | 31 个测试覆盖 init 命令和 daemon 配置加载 |
| `cmd/rnix/main.go` | 待修改 | init() 注册 initCmd + runDaemon() 全局配置加载改造 |

---

## 测试优先级分布

| 优先级 | 数量 | 测试 |
|--------|------|------|
| P0 | 14 | InitGlobal_CreatesDirectories, InitGlobal_ExtractsEmbeddedAgents, InitGlobal_ExtractsEmbeddedSkills, InitGlobal_GeneratesProvidersYAML, InitGlobal_SkipExistingFiles, InitGlobal_SkipExistingProvidersYAML, InitProject_CreatesDirectories, InitProject_SkipExisting, RunInit_BothGlobalAndProject, DaemonConfig_LoadProvidersFromGlobal, DaemonConfig_LoadSkillsFromGlobal, DaemonConfig_LoadAgentsFromGlobal, DaemonConfig_MissingProviders_UsesDefault, DaemonConfig_MissingProviders_NoCrash, DaemonConfig_InvalidProvidersYAML_ReturnsError |
| P1 | 14 | InitGlobal_GeneratesConfigYAML, InitGlobal_ProvidersContent, InitGlobal_SkipExistingConfigYAML, InitProject_GeneratesConfigYAML, InitProject_DataSubdirectories, InitProject_IdempotentRun, RunInit_GlobalExistsProjectNew, RunInit_BothExist, InitCmd_Registered, DaemonConfig_LoadMCPFromGlobal, DaemonConfig_InvalidProvidersYAML_ContainsFilename, DaemonConfig_RecordsDirMigrated, DaemonConfig_ReputationDirMigrated, DaemonConfig_ImmuneDirMigrated, DaemonConfig_TracesDirMigrated |
| P2 | 1 | InitProject_DirectoryPermissions |

---

## 下一步

1. 创建 `cmd/rnix/init.go` 定义 `initCmd`、`runInit`、`initGlobal`、`initProject`、`writeDefaultProviders`、`writeDefaultConfig`
2. 在 `cmd/rnix/main.go` 注册 `initCmd` 并改造 `runDaemon()` 配置加载流程
3. 创建 `cmd/rnix/init_test.go` 实现所有 31 个测试
4. 运行 `go test -race ./cmd/rnix/...` 验证全部 GREEN
5. 运行 `make all` 确保 lint + vet + test + build 全部通过
