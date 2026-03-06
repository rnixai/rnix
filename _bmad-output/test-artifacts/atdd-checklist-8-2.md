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
  - _bmad-output/implementation-artifacts/8-2-skill-search.md
  - _bmad/tea/config.yaml
  - skillpkg/types.go
  - skillpkg/client.go
  - skillpkg/client_test.go
  - cmd/crux/skill.go
  - cmd/crux/skill_test.go
---

# ATDD 检查清单 - Epic 8, Story 8.2: skill search 搜索

**日期:** 2026-03-01
**作者:** Decker
**主要测试级别:** 单元测试 (Go Backend)

---

## Story 概要

Story 8.2 实现 `crux skill search <keyword>` 命令，允许用户搜索社区仓库中可用的 Skill。搜索基于客户端过滤策略，调用已有的 `FetchIndex()` 获取完整索引，然后在本地按 keyword 做大小写不敏感的子串匹配。

**作为** 用户
**我想要** 通过 `skill search <keyword>` 搜索社区仓库中可用的 Skill
**以便** 我可以发现适合我需求的能力模块

---

## 验收标准

1. **AC #1: search 子命令注册** — Given `cmd/crux/skill.go` 中 search 子命令已注册，When 执行 `skill search code`，Then 返回匹配的 Skill 列表，And 每条结果包含：名称、描述、版本、下载量
2. **AC #2: 搜索无结果** — Given 搜索结果为空，When 无匹配关键词，Then 输出 `No skills found for "keyword".` + 建议
3. **AC #3: JSON 输出** — Given 使用 `--json` flag，When 搜索，Then 输出 JSON 数组，字段 snake_case

---

## 失败测试（RED Phase）

### 单元测试 — skillpkg/client_test.go (6 个测试)

**文件:** `skillpkg/client_test.go`

- **测试:** TestRegistryClient_Search_MatchByName
  - **状态:** RED - Search() 方法未实现（panic stub）
  - **验证:** AC #1 — keyword 匹配 Skill 名称
  - **优先级:** P0

- **测试:** TestRegistryClient_Search_MatchByDescription
  - **状态:** RED - Search() 方法未实现（panic stub）
  - **验证:** AC #1 — keyword 匹配 Skill 描述
  - **优先级:** P0

- **测试:** TestRegistryClient_Search_CaseInsensitive
  - **状态:** RED - Search() 方法未实现（panic stub）
  - **验证:** AC #1 — 大小写不敏感匹配
  - **优先级:** P1

- **测试:** TestRegistryClient_Search_NoMatch
  - **状态:** RED - Search() 方法未实现（panic stub）
  - **验证:** AC #2 — 无匹配时返回空切片
  - **优先级:** P1

- **测试:** TestRegistryClient_Search_EmptyKeyword
  - **状态:** RED - Search() 方法未实现（panic stub）
  - **验证:** AC #1 — 空 keyword 返回全部 Skill（浏览全部）
  - **优先级:** P1

- **测试:** TestRegistryClient_Search_ResultFields
  - **状态:** RED - Search() 方法未实现（panic stub）
  - **验证:** AC #1 — SearchResult 字段完整（名称、描述、版本、下载量）
  - **优先级:** P0

### CLI 测试 — cmd/crux/skill_test.go (4 个测试)

**文件:** `cmd/crux/skill_test.go`

- **测试:** TestSkillSearchCmd_Registered
  - **状态:** RED - skill search 子命令未注册
  - **验证:** AC #1 — search 子命令已注册到 skill 命令
  - **优先级:** P0

- **测试:** TestSkillSearch_JSONOutput
  - **状态:** RED - renderSkillSearchJSON 未实现（panic stub）
  - **验证:** AC #3 — JSON 输出格式和 snake_case 字段
  - **优先级:** P0

- **测试:** TestSkillSearch_EmptyResult_JSONOutput
  - **状态:** RED - renderSkillSearchJSON 未实现（panic stub）
  - **验证:** AC #2, #3 — 空结果 JSON 返回空数组
  - **优先级:** P1

- **测试:** TestSkillSearch_NoArgs_BrowseAll
  - **状态:** RED - skill search 子命令未注册
  - **验证:** AC #1 — 无参数时接受（MaximumNArgs(1)）= 浏览全部
  - **优先级:** P1

---

## 数据工厂

### Go 测试 — 内置 mock 模式

本项目使用 Go 标准库的 `net/http/httptest` 进行测试数据管理。

**Mock 模式:**

- `setupMockRegistryMultiSkill(t)` — 模拟含 4 个 Skill 的社区仓库 HTTP API（支持 downloads 字段）
- 复用已有的 `setupMockRegistry(t, name)` — 单 Skill mock（Story 8.1 遗产）

**文件:** `skillpkg/client_test.go` (测试辅助函数部分)

---

## Fixtures

### Go Backend Fixtures

- **`httptest.Server`** — Mock HTTP 服务器模拟社区仓库 API（含 4 个 Skill 索引）
- **`t.Helper()`** — 标记辅助函数以改善错误报告
- **`t.Skip()`** — TDD Red Phase 标记
- **`t.Cleanup()`** — 自动清理 mock 服务器

---

## Mock 需求

### 社区仓库 API Mock（搜索场景）

**端点:** `GET /index.yaml` — Skill 索引（含 downloads 字段）

**成功响应 (index.yaml):**
```yaml
skills:
  - name: code-analysis
    description: "Analyze code quality and patterns"
    latest: "1.0.0"
    downloads: 1234
  - name: pr-reviewer
    description: "Review pull requests with AI"
    latest: "2.1.0"
    downloads: 5678
  - name: tech-writer
    description: "Generate technical documentation"
    latest: "1.2.0"
    downloads: 890
  - name: bug-finder
    description: "Find and analyze bugs in code"
    latest: "3.0.0"
    downloads: 2345
```

**实现:** 使用 `net/http/httptest` 创建 mock 服务器，无需外部依赖

---

## 实现检查清单

### 测试: TestRegistryClient_Search_MatchByName / MatchByDescription / CaseInsensitive / NoMatch / EmptyKeyword / ResultFields

**文件:** `skillpkg/client_test.go`

**使这些测试通过的任务:**

- [ ] 在 `skillpkg/types.go` 中为 `SkillIndexEntry` 添加 `Downloads int` 字段和 JSON tag（已在 RED Phase 完成）
- [ ] 在 `skillpkg/types.go` 中创建 `SearchResult` 结构体（已在 RED Phase 完成）
- [ ] 在 `skillpkg/client.go` 中实现 `Search(keyword string) ([]SearchResult, error)` 方法（替换 panic stub）
- [ ] 实现逻辑：调用 `FetchIndex()` 获取索引，使用 `strings.Contains` + `strings.ToLower` 做大小写不敏感子串匹配
- [ ] 空 keyword 返回全部 Skill
- [ ] 无匹配返回空切片（非 nil）
- [ ] 将 `SkillIndexEntry` 转换为 `SearchResult`（`Latest` → `Version`）
- [ ] 移除 `t.Skip()` 标记
- [ ] 运行测试: `go test ./skillpkg/ -run TestRegistryClient_Search -v`
- [ ] 全部通过（green phase）

### 测试: TestSkillSearchCmd_Registered

**文件:** `cmd/crux/skill_test.go`

**使此测试通过的任务:**

- [ ] 在 `cmd/crux/skill.go` 中定义 `skillSearchCmd` cobra.Command（`Use: "search <keyword>"`, `Args: cobra.MaximumNArgs(1)`）
- [ ] 在 `init()` 中 `skillCmd.AddCommand(skillSearchCmd)`
- [ ] 移除 `t.Skip()` 标记
- [ ] 运行测试: `go test ./cmd/crux/ -run TestSkillSearchCmd_Registered -v`
- [ ] 测试通过（green phase）

### 测试: TestSkillSearch_JSONOutput / EmptyResult_JSONOutput

**文件:** `cmd/crux/skill_test.go`

**使这些测试通过的任务:**

- [ ] 实现 `renderSkillSearchJSON(r *ui.Renderer, results []skillpkg.SearchResult)` 函数（替换 panic stub）
- [ ] 使用 `JSONResponse{OK: true, Data: searchJSONData{Results: results}}` 格式
- [ ] 空结果时 Results 为空数组（非 null）
- [ ] 确保 JSON 字段为 snake_case（已通过 json tag 保证）
- [ ] 移除 `t.Skip()` 标记
- [ ] 运行测试: `go test ./cmd/crux/ -run "TestSkillSearch_JSONOutput|TestSkillSearch_EmptyResult" -v`
- [ ] 全部通过（green phase）

### 测试: TestSkillSearch_NoArgs_BrowseAll

**文件:** `cmd/crux/skill_test.go`

**使此测试通过的任务:**

- [ ] `skillSearchCmd` 的 `Args` 设为 `cobra.MaximumNArgs(1)`（允许 0 个参数）
- [ ] 实现 `runSkillSearch`：无参数时 keyword 为空字符串 = 浏览全部
- [ ] 移除 `t.Skip()` 标记
- [ ] 运行测试: `go test ./cmd/crux/ -run TestSkillSearch_NoArgs_BrowseAll -v`
- [ ] 测试通过（green phase）

### 额外实现任务（无独立测试但由 Story 要求）

- [ ] 终端模式输出：表格格式（NAME、DESCRIPTION、VERSION、DOWNLOADS）
- [ ] DESCRIPTION 超过 40 字符时截断并加 `...`
- [ ] Quiet 模式输出：每行仅输出 skill 名称
- [ ] 无结果终端模式输出 `No skills found for "keyword".` + 提示信息

---

## 运行测试

```bash
# 运行所有 Story 8.2 失败测试
go test ./skillpkg/ ./cmd/crux/ -run "TestRegistryClient_Search|TestSkillSearch" -v

# 运行 skillpkg 搜索测试
go test ./skillpkg/ -run TestRegistryClient_Search -v

# 运行 CLI 搜索测试
go test ./cmd/crux/ -run TestSkillSearch -v

# 运行全部测试（含竞态检测）
go test -race ./...

# 调试特定测试
go test ./skillpkg/ -run TestRegistryClient_Search_MatchByName -v -count=1
```

---

## Red-Green-Refactor 工作流

### RED Phase (完成)

**TEA Agent 职责:**

- 所有测试已编写并标记为 `t.Skip()` (跳过)
- Mock 服务器辅助函数 `setupMockRegistryMultiSkill` 已创建
- 类型和方法 panic stub 已创建（`SearchResult`、`Search()`、`renderSkillSearchJSON()`）
- 实现检查清单已创建

**验证:**

- 所有 10 个新测试运行并跳过 (t.Skip)
- 无编译错误
- 现有测试套件无回归（`go test -race ./...` 全部通过）

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

1. `skillpkg/client.go` — 实现 `Search()` 方法（替换 panic stub）
2. `cmd/crux/skill.go` — 注册 `skillSearchCmd` 子命令 + 实现 `runSkillSearch`
3. `cmd/crux/skill.go` — 实现 `renderSkillSearchJSON`（替换 panic stub）

**关键原则:**

- 一次一个测试 (不要试图一次修所有)
- 最小实现 (不要过度设计)
- 频繁运行测试 (即时反馈)
- 实现完成后删除 panic stub

---

### REFACTOR Phase (DEV 团队 — 所有测试通过后)

1. 验证所有测试通过
2. 检查代码质量 (可读性、可维护性)
3. 确保测试仍然通过
4. `make lint` 通过
5. `make build` 编译成功

---

## 下一步

1. **将此检查清单分享给 DEV 工作流**
2. **运行失败测试确认 RED Phase:** `go test ./skillpkg/ ./cmd/crux/ -run "TestRegistryClient_Search|TestSkillSearch" -v`
3. **开始实现** — 按实现检查清单顺序
4. **一次一个测试** (red → green)
5. **所有测试通过后** 重构代码
6. **最终验证:** `make all`

---

## 知识库引用

本 ATDD 工作流参考了以下知识片段：

- **data-factories.md** — 工厂模式与测试数据管理 (适配为 Go httptest 模式)
- **test-quality.md** — 测试质量标准 (确定性、隔离性、显式断言)
- **test-levels-framework.md** — 测试级别选择 (单元/集成)
- **test-healing-patterns.md** — 常见失败模式和修复策略

---

## 测试执行证据

### 初始测试运行 (RED Phase 验证)

**命令:** `go test ./skillpkg/ ./cmd/crux/ -run "TestRegistryClient_Search|TestSkillSearch" -v`

**结果:**

```
=== RUN   TestRegistryClient_Search_MatchByName
    client_test.go:252: ATDD RED Phase: Search() method not implemented yet — Story 8.2
--- SKIP: TestRegistryClient_Search_MatchByName (0.00s)
=== RUN   TestRegistryClient_Search_MatchByDescription
    client_test.go:290: ATDD RED Phase: Search() method not implemented yet — Story 8.2
--- SKIP: TestRegistryClient_Search_MatchByDescription (0.00s)
=== RUN   TestRegistryClient_Search_CaseInsensitive
    client_test.go:314: ATDD RED Phase: Search() method not implemented yet — Story 8.2
--- SKIP: TestRegistryClient_Search_CaseInsensitive (0.00s)
=== RUN   TestRegistryClient_Search_NoMatch
    client_test.go:344: ATDD RED Phase: Search() method not implemented yet — Story 8.2
--- SKIP: TestRegistryClient_Search_NoMatch (0.00s)
=== RUN   TestRegistryClient_Search_EmptyKeyword
    client_test.go:365: ATDD RED Phase: Search() method not implemented yet — Story 8.2
--- SKIP: TestRegistryClient_Search_EmptyKeyword (0.00s)
=== RUN   TestRegistryClient_Search_ResultFields
    client_test.go:386: ATDD RED Phase: Search() method not implemented yet — Story 8.2
--- SKIP: TestRegistryClient_Search_ResultFields (0.00s)
PASS
ok      github.com/usecrux/crux/skillpkg
=== RUN   TestSkillSearchCmd_Registered
    skill_test.go:241: ATDD RED Phase: skill search subcommand not implemented yet — Story 8.2
--- SKIP: TestSkillSearchCmd_Registered (0.00s)
=== RUN   TestSkillSearch_JSONOutput
    skill_test.go:269: ATDD RED Phase: skill search subcommand not implemented yet — Story 8.2
--- SKIP: TestSkillSearch_JSONOutput (0.00s)
=== RUN   TestSkillSearch_EmptyResult_JSONOutput
    skill_test.go:313: ATDD RED Phase: skill search subcommand not implemented yet — Story 8.2
--- SKIP: TestSkillSearch_EmptyResult_JSONOutput (0.00s)
=== RUN   TestSkillSearch_NoArgs_BrowseAll
    skill_test.go:341: ATDD RED Phase: skill search subcommand not implemented yet — Story 8.2
--- SKIP: TestSkillSearch_NoArgs_BrowseAll (0.00s)
PASS
ok      github.com/usecrux/crux/cmd/crux
```

**总结:**

- 总测试数: 10
- 通过: 0 (预期)
- 跳过: 10 (RED Phase — t.Skip)
- 状态: RED Phase 已验证
- 回归: 无 (`go test -race ./...` 全部通过)

---

## 备注

- 本项目是 Go 后端项目 (detected_stack = backend)，使用标准 Go 测试框架
- 所有测试使用 `t.Skip()` 标记 Red Phase
- 生产代码中添加了 panic stub（`Search()` 和 `renderSkillSearchJSON()`），实现时应替换为真实实现
- `SkillIndexEntry` 已添加 `Downloads` 字段和 JSON tag，对现有 YAML 反序列化无影响（int 零值兼容）
- `SearchResult` 类型已创建，字段使用 snake_case JSON tag
- Mock 仓库服务器使用 `net/http/httptest` 标准库，含 4 个不同 Skill 的索引数据
- 测试遵循项目现有模式：Given-When-Then 注释、`t.Helper()`、`t.Cleanup()` 自动清理
- 本 Story 仅修改现有文件（不新增文件、不新增包）

---

## 修改文件清单

| 文件 | 变更类型 | 说明 |
|------|---------|------|
| `skillpkg/types.go` | 修改 | 添加 `Downloads` 字段、JSON tag、`SearchResult` 类型 |
| `skillpkg/client.go` | 修改 | 添加 `Search()` panic stub 方法 |
| `skillpkg/client_test.go` | 修改 | 添加 6 个搜索测试 + `setupMockRegistryMultiSkill` 辅助函数 |
| `cmd/crux/skill.go` | 修改 | 添加 `searchJSONData` 类型 + `renderSkillSearchJSON` panic stub |
| `cmd/crux/skill_test.go` | 修改 | 添加 4 个搜索 CLI 测试 |

---

**Generated by BMad TEA Agent** - 2026-03-01
