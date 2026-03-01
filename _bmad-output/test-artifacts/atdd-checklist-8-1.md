---
stepsCompleted:
  - step-01-preflight-and-context
  - step-02-generation-mode
  - step-03-test-strategy
  - step-04-generate-tests
  - step-04c-aggregate
  - step-05-validate-and-complete
lastStep: step-05-validate-and-complete
lastSaved: '2026-03-01'
workflowType: testarch-atdd
inputDocuments:
  - _bmad-output/implementation-artifacts/8-1-skill-install.md
  - _bmad/tea/config.yaml
  - _bmad/tea/testarch/knowledge/data-factories.md
  - _bmad/tea/testarch/knowledge/test-quality.md
  - _bmad/tea/testarch/knowledge/test-levels-framework.md
  - _bmad/tea/testarch/knowledge/test-healing-patterns.md
---

# ATDD 检查清单 - Epic 8, Story 8.1: skill install 安装

**日期:** 2026-03-01
**作者:** Decker
**主要测试级别:** 单元测试 + 集成测试 (Go Backend)

---

## Story 概要

Story 8.1 实现 `crux skill install` 命令，允许用户从社区仓库安装 Skill 包。包括社区仓库客户端、本地注册表管理、安装流程编排和 CLI 子命令注册。

**作为** 用户
**我想要** 通过 `skill install <name>` 从社区仓库安装 Skill
**以便** 我可以快速获取社区共享的能力模块

---

## 验收标准

1. **AC #1: 社区仓库客户端** — Given `skillpkg/client.go` 已实现，When 调用社区仓库 API，Then 支持 Skill 下载、版本解析、完整性验证
2. **AC #2: 单个 Skill 安装** — Given `cmd/crux/skill.go` 中 install 子命令已注册，When 执行 `skill install code-analysis`，Then 从社区仓库下载并安装到 `lib/skills/code-analysis/`，更新本地注册表
3. **AC #3: 批量安装** — Given 批量安装，When 执行 `skill install pr-reviewer code-analyst tech-writer`，Then 依次安装三个 Skill
4. **AC #4: 重复安装提示** — Given Skill 已安装，When 再次执行 `skill install code-analysis`，Then 提示已安装且询问是否覆盖
5. **AC #5: 安装后可用** — Given 安装的 Skill 包含有效的 SKILL.md，When Agent 引用该 Skill，Then 无需修改即可使用

---

## 失败测试（RED Phase）

### 单元测试 — skillpkg/client_test.go (6 个测试)

**文件:** `skillpkg/client_test.go` (166 行)

- **测试:** TestRegistryClient_Fetch_Success
  - **状态:** RED - RegistryClient 未实现
  - **验证:** AC #1 — 从社区仓库成功下载 Skill 包
  - **优先级:** P0

- **测试:** TestRegistryClient_Fetch_NotFound
  - **状态:** RED - RegistryClient 未实现
  - **验证:** AC #1 — 不存在的 Skill 返回错误
  - **优先级:** P1

- **测试:** TestRegistryClient_Fetch_NetworkError
  - **状态:** RED - RegistryClient 未实现
  - **验证:** AC #1 — 网络错误处理
  - **优先级:** P1

- **测试:** TestRegistryClient_Resolve_LatestVersion
  - **状态:** RED - RegistryClient 未实现
  - **验证:** AC #1 — 版本解析功能
  - **优先级:** P0

- **测试:** TestRegistryClient_Verify_ValidChecksum
  - **状态:** RED - RegistryClient 未实现
  - **验证:** AC #1 — SHA256 完整性验证通过
  - **优先级:** P0

- **测试:** TestRegistryClient_Verify_InvalidChecksum
  - **状态:** RED - RegistryClient 未实现
  - **验证:** AC #1 — SHA256 校验不匹配时返回错误
  - **优先级:** P0

### 单元测试 — skillpkg/registry_test.go (8 个测试)

**文件:** `skillpkg/registry_test.go` (220 行)

- **测试:** TestLocalRegistry_Add_NewEntry
  - **状态:** RED - LocalRegistry 未实现
  - **验证:** AC #1 — 添加新注册表条目
  - **优先级:** P0

- **测试:** TestLocalRegistry_Add_UpdateExisting
  - **状态:** RED - LocalRegistry 未实现
  - **验证:** AC #1 — 更新已存在的注册表条目
  - **优先级:** P1

- **测试:** TestLocalRegistry_Get_NotFound
  - **状态:** RED - LocalRegistry 未实现
  - **验证:** AC #1 — 查询不存在的 Skill
  - **优先级:** P1

- **测试:** TestLocalRegistry_List_Empty
  - **状态:** RED - LocalRegistry 未实现
  - **验证:** AC #1 — 列出空注册表
  - **优先级:** P2

- **测试:** TestLocalRegistry_List_MultipleEntries
  - **状态:** RED - LocalRegistry 未实现
  - **验证:** AC #1 — 列出多个已安装 Skill
  - **优先级:** P1

- **测试:** TestLocalRegistry_Remove_Existing
  - **状态:** RED - LocalRegistry 未实现
  - **验证:** AC #1 — 移除已安装 Skill
  - **优先级:** P1

- **测试:** TestLocalRegistry_Remove_NotFound
  - **状态:** RED - LocalRegistry 未实现
  - **验证:** AC #1 — 移除不存在的 Skill
  - **优先级:** P2

- **测试:** TestLocalRegistry_Persistence_SurvivesReload
  - **状态:** RED - LocalRegistry 未实现
  - **验证:** AC #1 — 注册表持久化
  - **优先级:** P0

### 集成测试 — skillpkg/installer_test.go (8 个测试)

**文件:** `skillpkg/installer_test.go` (306 行)

- **测试:** TestInstaller_Install_SingleSkill
  - **状态:** RED - Installer 未实现
  - **验证:** AC #2 — 单个 Skill 完整安装流程
  - **优先级:** P0

- **测试:** TestInstaller_Install_VerifySKILLMD
  - **状态:** RED - Installer 未实现
  - **验证:** AC #5 — 安装后 SKILL.md 可被 SkillLoader 加载
  - **优先级:** P0

- **测试:** TestInstaller_Install_ChecksumVerification
  - **状态:** RED - Installer 未实现
  - **验证:** AC #1 — 安装流程中的完整性验证
  - **优先级:** P0

- **测试:** TestInstaller_Install_InvalidChecksum
  - **状态:** RED - Installer 未实现
  - **验证:** AC #1 — 篡改包被拒绝并清理
  - **优先级:** P0

- **测试:** TestInstaller_Install_AlreadyInstalled_WithForce
  - **状态:** RED - Installer 未实现
  - **验证:** AC #4 — 强制覆盖安装
  - **优先级:** P1

- **测试:** TestInstaller_Install_AlreadyInstalled_WithoutForce
  - **状态:** RED - Installer 未实现
  - **验证:** AC #4 — 重复安装返回错误
  - **优先级:** P1

- **测试:** TestInstaller_Install_NetworkError
  - **状态:** RED - Installer 未实现
  - **验证:** AC #1 — 网络不可达时的错误处理
  - **优先级:** P1

- **测试:** TestInstaller_Install_CreatesDirectories
  - **状态:** RED - Installer 未实现
  - **验证:** AC #2 — 自动创建安装目录
  - **优先级:** P1

### CLI 测试 — cmd/crux/skill_test.go (11 个测试)

**文件:** `cmd/crux/skill_test.go` (180 行)

- **测试:** TestSkillCmd_Registered
  - **状态:** RED - skill 命令未注册
  - **验证:** AC #2 — skill 子命令已注册到 rootCmd
  - **优先级:** P0

- **测试:** TestSkillInstallCmd_Registered
  - **状态:** RED - skill install 命令未注册
  - **验证:** AC #2 — install 子命令已注册到 skill 命令
  - **优先级:** P0

- **测试:** TestSkillInstall_SingleSkill_JSONOutput
  - **状态:** RED - skill install 命令未实现
  - **验证:** AC #2 — 单个安装的 JSON 输出
  - **优先级:** P1

- **测试:** TestSkillInstall_BatchInstall
  - **状态:** RED - skill install 命令未实现
  - **验证:** AC #3 — 批量安装多个 Skill
  - **优先级:** P0

- **测试:** TestSkillInstall_BatchInstall_JSONOutput
  - **状态:** RED - skill install 命令未实现
  - **验证:** AC #3 — 批量安装的 JSON 输出
  - **优先级:** P1

- **测试:** TestSkillInstall_AlreadyInstalled_ShowsPrompt
  - **状态:** RED - skill install 命令未实现
  - **验证:** AC #4 — 重复安装显示提示
  - **优先级:** P1

- **测试:** TestSkillInstall_AlreadyInstalled_ForceFlag
  - **状态:** RED - skill install 命令未实现
  - **验证:** AC #4 — --force 跳过重复确认
  - **优先级:** P1

- **测试:** TestSkillInstall_NoArgs
  - **状态:** RED - skill install 命令未实现
  - **验证:** 错误处理 — 无参数时的提示
  - **优先级:** P2

- **测试:** TestSkillInstall_InvalidSkillName
  - **状态:** RED - skill install 命令未实现
  - **验证:** 安全性 — 路径穿越防护
  - **优先级:** P1

- **测试:** TestSkillInstall_Flags_Force
  - **状态:** RED - skill install 命令未实现
  - **验证:** AC #4 — --force flag 注册
  - **优先级:** P2

- **测试:** TestSkillInstall_Flags_JSON
  - **状态:** RED - skill install 命令未实现
  - **验证:** --json flag 支持
  - **优先级:** P2

---

## 数据工厂

### Go 测试 — 内置 mock 模式

本项目使用 Go 标准库的 `net/http/httptest` 和 `t.TempDir()` 进行测试数据管理，无需外部工厂库。

**Mock 模式:**

- `newMockRegistryServer(t)` — 模拟社区仓库 HTTP API
- `newMockRegistryServerWithTamperedData(t)` — 模拟篡改数据的仓库
- `t.TempDir()` — 自动清理的临时目录（注册表、安装目录）

**文件:** `skillpkg/installer_test.go` (测试辅助函数部分)

---

## Fixtures

### Go Backend Fixtures

本项目遵循 Go 测试惯例，使用以下模式：

- **`httptest.Server`** — Mock HTTP 服务器模拟社区仓库 API
- **`t.TempDir()`** — 临时目录，测试结束后自动清理
- **`t.Helper()`** — 标记辅助函数以改善错误报告
- **`t.Skip()`** — TDD Red Phase 标记

---

## Mock 需求

### 社区仓库 API Mock

**端点:** `GET /index.yaml` — Skill 索引
**端点:** `GET /packages/<name>/latest.yaml` — 最新版本元数据
**端点:** `GET /packages/<name>/<version>.tar.gz` — Skill 包下载

**成功响应 (latest.yaml):**
```yaml
name: code-analysis
version: "1.0.0"
checksum: "sha256:abc123"
```

**失败响应:** HTTP 404 Not Found

**实现:** 使用 `net/http/httptest` 创建 mock 服务器，无需外部依赖

---

## 实现检查清单

### 测试: TestRegistryClient_Fetch_Success / Fetch_NotFound / Fetch_NetworkError

**文件:** `skillpkg/client_test.go`

**使此测试通过的任务:**

- [ ] 创建 `skillpkg/types.go`：定义 SkillPackage, SkillVersion, RegistryEntry 等类型
- [ ] 创建 `skillpkg/client.go`：实现 RegistryClient 结构体
- [ ] 实现 `Fetch(name string) (*SkillPackage, error)` 方法
- [ ] 使用 `net/http` 发送 GET 请求下载元数据和包
- [ ] 错误处理：404 返回 "skill not found"，网络错误包装为用户友好消息
- [ ] 运行测试: `go test ./skillpkg/ -run TestRegistryClient_Fetch -v`
- [ ] 测试通过（green phase）

### 测试: TestRegistryClient_Resolve_LatestVersion

**文件:** `skillpkg/client_test.go`

**使此测试通过的任务:**

- [ ] 实现 `Resolve(name, versionConstraint string) (*SkillVersion, error)` 方法
- [ ] 空版本约束时获取 latest.yaml
- [ ] 运行测试: `go test ./skillpkg/ -run TestRegistryClient_Resolve -v`
- [ ] 测试通过（green phase）

### 测试: TestRegistryClient_Verify_ValidChecksum / Verify_InvalidChecksum

**文件:** `skillpkg/client_test.go`

**使此测试通过的任务:**

- [ ] 实现 `Verify(pkg *SkillPackage) error` 方法
- [ ] 使用 `crypto/sha256` 计算包数据的哈希值
- [ ] 与 pkg.Checksum 比对
- [ ] 运行测试: `go test ./skillpkg/ -run TestRegistryClient_Verify -v`
- [ ] 测试通过（green phase）

### 测试: TestLocalRegistry_* (8 个测试)

**文件:** `skillpkg/registry_test.go`

**使这些测试通过的任务:**

- [ ] 创建 `skillpkg/registry.go`：实现 LocalRegistry 结构体
- [ ] 实现 `NewLocalRegistry(path string) *LocalRegistry`
- [ ] 实现 `Add(entry RegistryEntry) error` — YAML 序列化写入
- [ ] 实现 `Get(name string) (*RegistryEntry, error)` — YAML 反序列化读取
- [ ] 实现 `List() ([]RegistryEntry, error)` — 返回全部条目
- [ ] 实现 `Remove(name string) error` — 从 YAML 中移除
- [ ] 使用 `github.com/goccy/go-yaml` 解析 YAML
- [ ] 运行测试: `go test ./skillpkg/ -run TestLocalRegistry -v`
- [ ] 全部通过（green phase）

### 测试: TestInstaller_Install_* (8 个测试)

**文件:** `skillpkg/installer_test.go`

**使这些测试通过的任务:**

- [ ] 创建 `skillpkg/installer.go`：实现 Installer 结构体
- [ ] 实现 `NewInstaller(config InstallerConfig) *Installer`
- [ ] 实现 `Install(name string, opts InstallOpts) (*InstallResult, error)`
- [ ] 安装流程：Fetch → Verify → 解压到 `lib/skills/<name>/` → 验证 SKILL.md → 更新注册表
- [ ] 使用 `archive/tar` + `compress/gzip` 解压 tar.gz
- [ ] 已安装检查：无 Force 时返回 "already installed" 错误
- [ ] 失败回滚：校验不通过时清理已下载文件
- [ ] 自动创建安装目录
- [ ] 运行测试: `go test ./skillpkg/ -run TestInstaller -v`
- [ ] 全部通过（green phase）

### 测试: TestSkillCmd_Registered / TestSkillInstallCmd_Registered

**文件:** `cmd/crux/skill_test.go`

**使这些测试通过的任务:**

- [ ] 创建 `cmd/crux/skill.go`
- [ ] 定义 `skillCmd` 父命令 (`cobra.Command`)
- [ ] 定义 `skillInstallCmd` 子命令
- [ ] 在 `init()` 中注册: `skillCmd.AddCommand(skillInstallCmd)` 和 `rootCmd.AddCommand(skillCmd)`
- [ ] 注册 `--force` flag
- [ ] 运行测试: `go test ./cmd/crux/ -run TestSkillCmd -v`
- [ ] 全部通过（green phase）

### 测试: TestSkillInstall_BatchInstall / JSON / Force

**文件:** `cmd/crux/skill_test.go`

**使这些测试通过的任务:**

- [ ] 实现 `runSkillInstall` RunE 函数
- [ ] 解析 args 为 Skill 名称列表
- [ ] 循环调用 `skillpkg.Installer.Install()`
- [ ] 支持 `--json` 输出 JSON 格式结果
- [ ] 支持 `--force` 跳过重复安装确认
- [ ] 无参数时返回错误
- [ ] 路径穿越防护（拒绝含 `..` 的名称）
- [ ] 运行测试: `go test ./cmd/crux/ -run TestSkillInstall -v`
- [ ] 全部通过（green phase）

---

## 运行测试

```bash
# 运行所有 Story 8.1 失败测试
go test ./skillpkg/ ./cmd/crux/ -run "TestRegistryClient|TestLocalRegistry|TestInstaller|TestSkill" -v

# 运行 skillpkg 包测试
go test ./skillpkg/ -v

# 运行 CLI 层测试
go test ./cmd/crux/ -run "TestSkill" -v

# 运行全部测试（含竞态检测）
go test -race ./...

# 调试特定测试
go test ./skillpkg/ -run TestInstaller_Install_SingleSkill -v -count=1
```

---

## Red-Green-Refactor 工作流

### RED Phase (完成)

**TEA Agent 职责:**

- 所有测试已编写并标记为 `t.Skip()` (失败)
- Mock 服务器和辅助函数已创建
- 类型桩文件已创建 (`stubs_test_support.go`)
- 实现检查清单已创建

**验证:**

- 所有测试运行并跳过 (t.Skip)
- 无编译错误
- 现有测试套件无回归

---

### GREEN Phase (DEV 团队 — 下一步)

**DEV Agent 职责:**

1. **从一个失败测试开始** — 按优先级选择 (P0 优先)
2. **阅读测试** — 理解预期行为
3. **实现最小代码** — 使该测试通过
4. **移除 t.Skip()** — 验证测试通过
5. **勾选检查清单** — 标记任务完成
6. **继续下一个测试**

**建议实现顺序:**

1. `skillpkg/types.go` — 核心类型定义 (从 stubs_test_support.go 迁移)
2. `skillpkg/client.go` — RegistryClient (AC #1)
3. `skillpkg/registry.go` — LocalRegistry (AC #1)
4. `skillpkg/installer.go` — Installer (AC #2, #4, #5)
5. `cmd/crux/skill.go` — CLI 命令 (AC #2, #3, #4)

**关键原则:**

- 一次一个测试 (不要试图一次修所有)
- 最小实现 (不要过度设计)
- 频繁运行测试 (即时反馈)
- 实现完成后删除 `skillpkg/stubs_test_support.go`

---

### REFACTOR Phase (DEV 团队 — 所有测试通过后)

1. 验证所有测试通过
2. 检查代码质量 (可读性、可维护性)
3. 消除重复代码
4. 确保测试仍然通过
5. `make lint` 通过
6. `make build` 编译成功

---

## 下一步

1. **将此检查清单分享给 DEV 工作流**
2. **运行失败测试确认 RED Phase:** `go test ./skillpkg/ ./cmd/crux/ -run "TestRegistryClient|TestLocalRegistry|TestInstaller|TestSkill" -v`
3. **开始实现** — 按实现检查清单顺序
4. **一次一个测试** (red → green)
5. **所有测试通过后** 重构代码
6. **最终验证:** `make all`

---

## 知识库引用

本 ATDD 工作流参考了以下知识片段：

- **data-factories.md** — 工厂模式与测试数据管理 (适配为 Go httptest 模式)
- **test-quality.md** — 测试质量标准 (确定性、隔离性、显式断言)
- **test-levels-framework.md** — 测试级别选择 (单元/集成/E2E)
- **test-healing-patterns.md** — 常见失败模式和修复策略

---

## 测试执行证据

### 初始测试运行 (RED Phase 验证)

**命令:** `go test ./skillpkg/ ./cmd/crux/ -run "TestRegistryClient|TestLocalRegistry|TestInstaller|TestSkill" -v`

**结果:**

```
--- SKIP: TestRegistryClient_Fetch_Success (0.00s)
--- SKIP: TestRegistryClient_Fetch_NotFound (0.00s)
--- SKIP: TestRegistryClient_Fetch_NetworkError (0.00s)
--- SKIP: TestRegistryClient_Resolve_LatestVersion (0.00s)
--- SKIP: TestRegistryClient_Verify_ValidChecksum (0.00s)
--- SKIP: TestRegistryClient_Verify_InvalidChecksum (0.00s)
--- SKIP: TestInstaller_Install_SingleSkill (0.00s)
--- SKIP: TestInstaller_Install_VerifySKILLMD (0.00s)
--- SKIP: TestInstaller_Install_ChecksumVerification (0.00s)
--- SKIP: TestInstaller_Install_InvalidChecksum (0.00s)
--- SKIP: TestInstaller_Install_AlreadyInstalled_WithForce (0.00s)
--- SKIP: TestInstaller_Install_AlreadyInstalled_WithoutForce (0.00s)
--- SKIP: TestInstaller_Install_NetworkError (0.00s)
--- SKIP: TestInstaller_Install_CreatesDirectories (0.00s)
--- SKIP: TestLocalRegistry_Add_NewEntry (0.00s)
--- SKIP: TestLocalRegistry_Add_UpdateExisting (0.00s)
--- SKIP: TestLocalRegistry_Get_NotFound (0.00s)
--- SKIP: TestLocalRegistry_List_Empty (0.00s)
--- SKIP: TestLocalRegistry_List_MultipleEntries (0.00s)
--- SKIP: TestLocalRegistry_Remove_Existing (0.00s)
--- SKIP: TestLocalRegistry_Remove_NotFound (0.00s)
--- SKIP: TestLocalRegistry_Persistence_SurvivesReload (0.00s)
--- SKIP: TestSkillCmd_Registered (0.00s)
--- SKIP: TestSkillInstallCmd_Registered (0.00s)
--- SKIP: TestSkillInstall_SingleSkill_JSONOutput (0.00s)
--- SKIP: TestSkillInstall_BatchInstall (0.00s)
--- SKIP: TestSkillInstall_BatchInstall_JSONOutput (0.00s)
--- SKIP: TestSkillInstall_AlreadyInstalled_ShowsPrompt (0.00s)
--- SKIP: TestSkillInstall_AlreadyInstalled_ForceFlag (0.00s)
--- SKIP: TestSkillInstall_NoArgs (0.00s)
--- SKIP: TestSkillInstall_InvalidSkillName (0.00s)
--- SKIP: TestSkillInstall_Flags_Force (0.00s)
--- SKIP: TestSkillInstall_Flags_JSON (0.00s)
PASS
```

**总结:**

- 总测试数: 33
- 通过: 0 (预期)
- 跳过: 33 (RED Phase — t.Skip)
- 状态: RED Phase 已验证
- 回归: 无 (`go test ./...` 全部通过)

---

## 备注

- 本项目是 Go 后端项目 (detected_stack = backend)，使用标准 Go 测试框架
- 所有测试使用 `t.Skip()` 而非 TypeScript 的 `test.skip()` 标记 Red Phase
- 类型桩文件 `skillpkg/stubs_test_support.go` 包含编译所需的最小类型和构造函数（均 panic），实现时应替换为真实实现
- Mock 仓库服务器使用 `net/http/httptest` 标准库，无需额外依赖
- 测试遵循项目现有模式：Given-When-Then 注释、`t.Helper()`、`t.TempDir()` 自动清理

---

**Generated by BMad TEA Agent** - 2026-03-01
