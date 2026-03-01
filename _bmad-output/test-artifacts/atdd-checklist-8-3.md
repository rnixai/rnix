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
  - _bmad-output/implementation-artifacts/8-3-skill-update.md
  - skillpkg/types.go
  - skillpkg/installer.go
  - skillpkg/installer_test.go
  - skillpkg/registry.go
  - skillpkg/client_test.go
  - cmd/crux/skill.go
  - cmd/crux/skill_test.go
---

# ATDD 清单 - Epic 8, Story 8.3: skill update 更新

**日期:** 2026-03-01
**作者:** Decker
**主要测试级别:** 单元测试 (Unit)

---

## Story 概要

**As a** 用户,
**I want** 通过 `skill update [name]` 更新已安装的 Skill,
**So that** 我始终使用最新兼容版本的能力模块。

本 Story 为 Crux Agent OS 的 skill 包管理系统添加更新功能，支持单个和批量更新已安装的社区 Skill。

---

## 验收标准

1. **update 子命令注册与单个更新** — 在 `cmd/crux/skill.go` 中注册 update 子命令，执行 `skill update code-analysis` 时检查最新版本，有更新则下载替换并更新本地注册表
2. **全量更新** — 不指定名称时检查所有已安装 Skill，显示可更新列表，批量更新
3. **已是最新版本** — 已是最新版本时输出提示信息

---

## 已创建的失败测试 (RED Phase)

### 单元测试 (5 tests)

**文件:** `skillpkg/update_test.go` (215 行)

- **Test:** `TestInstaller_Update_HasUpdate`
  - **状态:** RED - `Installer` 没有 `Update` 方法
  - **验证:** AC #1 - 有新版本时下载更新并返回 Updated=true，验证版本号和注册表更新

- **Test:** `TestInstaller_Update_AlreadyUpToDate`
  - **状态:** RED - `Installer` 没有 `Update` 方法
  - **验证:** AC #3 - 版本相同时返回 Updated=false

- **Test:** `TestInstaller_Update_NotInstalled`
  - **状态:** RED - `Installer` 没有 `Update` 方法
  - **验证:** AC #1 错误处理 - 未安装的 Skill 返回错误

- **Test:** `TestInstaller_UpdateAll_MultipleSkills`
  - **状态:** RED - `Installer` 没有 `UpdateAll` 方法
  - **验证:** AC #2 - 批量检查多个已安装 Skill，部分更新、部分保持不变

- **Test:** `TestInstaller_UpdateAll_SkipBuiltin`
  - **状态:** RED - `Installer` 没有 `UpdateAll` 方法
  - **验证:** AC #2 - 系统自带 Skill (Source="builtin") 不被更新

### CLI 测试 (6 tests)

**文件:** `cmd/crux/skill_test.go` (新增约 180 行)

- **Test:** `TestSkillUpdateCmd_Registered`
  - **状态:** RED - `update` 子命令未注册
  - **验证:** AC #1 - update 子命令在 skill 下注册

- **Test:** `TestSkillUpdate_JSONOutput`
  - **状态:** RED - `skillpkg.UpdateResult` 类型不存在
  - **验证:** AC #1, #3 - JSON 输出格式正确，字段使用 snake_case

- **Test:** `TestSkillUpdate_EmptyResult_JSONOutput`
  - **状态:** RED - `skillpkg.UpdateResult` 类型不存在
  - **验证:** AC #2, #3 - 全量更新无结果时返回空数组而非 null

- **Test:** `TestSkillUpdate_NoArgs_Accepted`
  - **状态:** RED - `update` 子命令未注册
  - **验证:** AC #2 - 零参数被接受（ArbitraryArgs）

- **Test:** `TestSkillUpdate_WithArgs_Accepted`
  - **状态:** RED - `update` 子命令未注册
  - **验证:** AC #1 - 一个或多个参数被接受

- **Test:** `TestSkillUpdate_MixedResults_JSONOutput`
  - **状态:** RED - `skillpkg.UpdateResult` 类型不存在
  - **验证:** AC #1, #2 - 更新成功和错误混合结果的 JSON 输出

---

## 测试辅助设施

### Mock 工具函数

**文件:** `skillpkg/update_test.go`

**新增:**

- `setupMockRegistryWithVersion(t, name, version)` - 创建返回指定版本的 mock HTTP 服务器
  - **用途:** 测试版本比较逻辑，模拟从 v1.0.0 升级到 v2.0.0
  - **复用:** `createTestTarGz` 和 `checksumOf` 来自 `client_test.go`

### 复用的现有测试基础设施

- `setupMockRegistry(t, name)` - 从 `client_test.go`，创建 v1.0.0 的 mock 服务器
- `createTestTarGz(t, name)` - 从 `client_test.go`，创建有效的 .tar.gz skill 包
- `checksumOf(data)` - 从 `client_test.go`，计算 SHA256 校验和
- `setupMockRegistryMultiSkill(t)` - 从 `client_test.go`，多 skill 索引 mock

---

## Mock 需求

### Community Registry Mock

本 Story 复用已有的 `httptest.Server` mock 模式，新增 `setupMockRegistryWithVersion` 支持：

**端点:** `GET /packages/{name}/latest.yaml`
- 可配置返回不同版本号

**成功响应:**
```yaml
version: "2.0.0"
checksum: "sha256:abc..."
```

**端点:** `GET /packages/{name}/{version}.tar.gz`
- 返回有效的 tar.gz skill 包

---

## 实现清单

### Test: TestInstaller_Update_HasUpdate

**文件:** `skillpkg/update_test.go`

**使此测试通过的任务:**

- [ ] 在 `skillpkg/types.go` 中添加 `UpdateResult` 结构体（Name, OldVersion, NewVersion, Updated 字段，json tag 使用 snake_case）
- [ ] 在 `skillpkg/types.go` 中添加 `UpdateOpts` 结构体（Force bool 字段）
- [ ] 在 `skillpkg/installer.go` 中实现 `Installer.Update(name string, opts UpdateOpts) (*UpdateResult, error)` 方法
- [ ] Update 逻辑：从 registry.Get 获取已安装版本 → client.Resolve 获取最新版本 → 比较版本 → 调用 Install(name, InstallOpts{Force: true}) 执行更新
- [ ] 运行测试: `go test ./skillpkg/ -run TestInstaller_Update_HasUpdate -v`
- [ ] 测试通过 (green phase)

### Test: TestInstaller_Update_AlreadyUpToDate

**文件:** `skillpkg/update_test.go`

**使此测试通过的任务:**

- [ ] 确保 Update 方法在 `existing.Version == ver.Version` 时返回 `UpdateResult{Updated: false}`
- [ ] 运行测试: `go test ./skillpkg/ -run TestInstaller_Update_AlreadyUpToDate -v`
- [ ] 测试通过 (green phase)

### Test: TestInstaller_Update_NotInstalled

**文件:** `skillpkg/update_test.go`

**使此测试通过的任务:**

- [ ] 确保 Update 方法在 `registry.Get` 返回 nil 时返回错误 `skill %q is not installed`
- [ ] 运行测试: `go test ./skillpkg/ -run TestInstaller_Update_NotInstalled -v`
- [ ] 测试通过 (green phase)

### Test: TestInstaller_UpdateAll_MultipleSkills

**文件:** `skillpkg/update_test.go`

**使此测试通过的任务:**

- [ ] 在 `skillpkg/installer.go` 中实现 `Installer.UpdateAll(opts UpdateOpts) ([]UpdateResult, error)` 方法
- [ ] UpdateAll 逻辑：从 registry.List 获取所有已安装 Skill → 过滤 Source=="community" → 对每个调用 Update → 收集结果
- [ ] 运行测试: `go test ./skillpkg/ -run TestInstaller_UpdateAll_MultipleSkills -v`
- [ ] 测试通过 (green phase)

### Test: TestInstaller_UpdateAll_SkipBuiltin

**文件:** `skillpkg/update_test.go`

**使此测试通过的任务:**

- [ ] 确保 UpdateAll 只处理 Source=="community" 的条目
- [ ] 运行测试: `go test ./skillpkg/ -run TestInstaller_UpdateAll_SkipBuiltin -v`
- [ ] 测试通过 (green phase)

### Test: TestSkillUpdateCmd_Registered

**文件:** `cmd/crux/skill_test.go`

**使此测试通过的任务:**

- [ ] 在 `cmd/crux/skill.go` 中定义 `skillUpdateCmd` cobra.Command（Use: "update [name...]", Args: cobra.ArbitraryArgs）
- [ ] 在 `init()` 中 `skillCmd.AddCommand(skillUpdateCmd)`
- [ ] 运行测试: `go test ./cmd/crux/ -run TestSkillUpdateCmd_Registered -v`
- [ ] 测试通过 (green phase)

### Test: TestSkillUpdate_JSONOutput

**文件:** `cmd/crux/skill_test.go`

**使此测试通过的任务:**

- [ ] 在 `cmd/crux/skill.go` 中定义 `updateErrorEntry` 结构体（Name, Code, Message 字段）
- [ ] 在 `cmd/crux/skill.go` 中定义 `skillUpdateJSONData` 结构体（Results 和 Errors 字段）
- [ ] 实现 `renderSkillUpdateJSON(r, results, errs)` 函数
- [ ] 确保 JSON 输出使用 snake_case 字段名
- [ ] 运行测试: `go test ./cmd/crux/ -run TestSkillUpdate_JSONOutput -v`
- [ ] 测试通过 (green phase)

### Test: TestSkillUpdate_EmptyResult_JSONOutput

**文件:** `cmd/crux/skill_test.go`

**使此测试通过的任务:**

- [ ] 确保 renderSkillUpdateJSON 将 nil results 转换为空切片 `[]`
- [ ] 运行测试: `go test ./cmd/crux/ -run TestSkillUpdate_EmptyResult_JSONOutput -v`
- [ ] 测试通过 (green phase)

### Test: TestSkillUpdate_NoArgs_Accepted / TestSkillUpdate_WithArgs_Accepted

**文件:** `cmd/crux/skill_test.go`

**使此测试通过的任务:**

- [ ] skillUpdateCmd 使用 `cobra.ArbitraryArgs`（接受任意数量参数）
- [ ] 运行测试: `go test ./cmd/crux/ -run "TestSkillUpdate_.*Args" -v`
- [ ] 测试通过 (green phase)

### Test: TestSkillUpdate_MixedResults_JSONOutput

**文件:** `cmd/crux/skill_test.go`

**使此测试通过的任务:**

- [ ] renderSkillUpdateJSON 在有 errors 时设置 ok=false
- [ ] 运行测试: `go test ./cmd/crux/ -run TestSkillUpdate_MixedResults_JSONOutput -v`
- [ ] 测试通过 (green phase)

---

## 运行测试

```bash
# 运行所有 Story 8.3 相关测试（当前会编译失败 - RED phase）
go test ./skillpkg/ -run "TestInstaller_Update" -v
go test ./cmd/crux/ -run "TestSkillUpdate" -v

# 运行单个测试文件
go test ./skillpkg/ -run "TestInstaller_Update_HasUpdate" -v

# 运行全部测试（含 race 检测）
make test

# 运行 lint
make lint

# 完整构建验证
make all
```

---

## Red-Green-Refactor 工作流

### RED Phase (完成)

**TEA Agent 职责:**

- 所有测试已编写并确认无法编译（方法/类型不存在）
- Mock 辅助函数已创建（setupMockRegistryWithVersion）
- 测试覆盖所有 3 个验收标准
- 实现清单已创建

**验证:**

- `go vet ./skillpkg/` 输出: `Update undefined (type *Installer has no field or method Update)`
- `go vet ./cmd/crux/` 输出: `undefined: skillpkg.UpdateResult`
- 所有测试因缺少实现而无法编译，而非测试本身有 bug

---

### GREEN Phase (DEV Team - 下一步)

**DEV Agent 职责:**

1. **先实现类型** — `skillpkg/types.go` 添加 `UpdateResult` 和 `UpdateOpts`
2. **实现核心方法** — `skillpkg/installer.go` 添加 `Update()` 和 `UpdateAll()`
3. **实现 CLI 命令** — `cmd/crux/skill.go` 添加 `skillUpdateCmd` 和 `runSkillUpdate`
4. **逐个验证测试** — 每实现一个组件后运行相关测试
5. **运行完整测试套件** — `make test` 确认无回归

**关键原则:**

- 一次一个测试（不要同时修复所有）
- 最小实现（不过度设计）
- 复用 `Install(name, InstallOpts{Force: true})` 执行实际更新
- 版本比较使用简单字符串相等

---

### REFACTOR Phase (DEV Team - 所有测试通过后)

1. 验证所有测试通过
2. 检查代码质量
3. 运行 `make lint`
4. 确认无重复代码
5. 运行 `make all` 最终验证

---

## 测试执行证据

### 初始测试运行 (RED Phase 验证)

**命令:** `go vet ./skillpkg/ && go vet ./cmd/crux/`

**结果:**

```
# github.com/gonewx/crux/skillpkg
vet: skillpkg/update_test.go:71:29: installerV2.Update undefined (type *Installer has no field or method Update)

# github.com/gonewx/crux/cmd/crux
vet: cmd/crux/skill_test.go:368:24: undefined: skillpkg.UpdateResult
```

**总结:**

- 总测试数: 11 (5 单元 + 6 CLI)
- 通过: 0 (预期)
- 失败: 11 (预期 - 编译错误)
- 状态: RED phase 已验证

---

## 知识库引用

本 ATDD 工作流参考了以下内容:

- **test-quality.md** - 测试设计原则（Given-When-Then、每测试一断言、确定性、隔离性）
- **test-levels-framework.md** - 测试级别选择框架（后端项目使用 Unit + Integration）
- **data-factories.md** - 数据工厂模式（mock HTTP server 用于随机/可控测试数据）
- **Story 8.2 代码和测试** - 复用现有 mock 模式和 CLI 测试模式
- **Story 8.1 代码和测试** - Install 流程实现和测试模式

---

## 注意事项

- Go TDD Red Phase 的体现方式是编译失败（引用不存在的方法/类型），而非 JavaScript 的 `test.skip()` 模式
- 所有测试复用已有的 mock 基础设施（setupMockRegistry、createTestTarGz），仅新增 `setupMockRegistryWithVersion` 用于版本比较测试
- 测试文件分离策略：installer 层测试放在 `skillpkg/update_test.go`，CLI 层测试追加到 `cmd/crux/skill_test.go`
- UpdateAll 测试需要构建多 skill 的 mock 服务器，直接内联而非创建新辅助函数，保持简洁

---

**Generated by BMad TEA Agent** - 2026-03-01
