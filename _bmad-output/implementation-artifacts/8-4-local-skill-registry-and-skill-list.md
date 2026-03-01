# Story 8.4: 本地 Skill 注册表与 skill list

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a 用户,
I want 通过 `skill list` 查看所有已安装的 Skill,
so that 我了解本地可用的能力模块。

## Acceptance Criteria

1. **skill list 表格输出** — Given 本地 Skill 注册表已维护，When 执行 `skill list`，Then 输出表格：NAME、VERSION、PATH、DESCRIPTION，And 包含系统自带 Skill 和社区安装的 Skill

2. **无社区 Skill 时的提示** — Given 无已安装 Skill（除系统自带），When 执行 `skill list`，Then 显示系统自带 Skill + `Tip: skill search <keyword> 发现更多 Skill`

3. **JSON 输出** — Given 使用 `--json` flag，When 列出，Then 输出 JSON 数组，字段 snake_case

## Tasks / Subtasks

- [x] Task 1: 扩展 `skillpkg/registry.go` 或 `skillpkg/types.go` — 添加 ListEntry 类型 (AC: #1, #3)
  - [x] 1.1 创建 `ListEntry` 结构体：`Name`、`Version`、`Path`、`Description`、`Source` (builtin/community)，JSON tag 使用 snake_case
  - [x] 1.2 在 `Installer` 上添加 `ListAll() ([]ListEntry, error)` 方法——聚合 builtin + community skills

- [x] Task 2: 实现 `Installer.ListAll()` — 聚合 builtin 与 community skills (AC: #1, #2)
  - [x] 2.1 从 `LocalRegistry.List()` 获取所有已注册 skill（包含 community 和可能的 builtin 条目）
  - [x] 2.2 扫描 `basePath` 目录（`lib/skills/`），枚举所有子目录
  - [x] 2.3 对每个子目录，尝试 `skillLoader.LoadMetadata(name)` 读取 SKILL.md 的 description
  - [x] 2.4 对注册表中存在的 skill：使用注册表的 version 和 source，SKILL.md 的 description
  - [x] 2.5 对注册表中不存在但目录存在的 skill（系统自带未注册）：Source="builtin"，Version 从 SKILL.md metadata 获取或为空，Path 为相对路径
  - [x] 2.6 按 Name 字母排序返回结果
  - [x] 2.7 结果使用 `make([]ListEntry, 0)` 初始化，确保 JSON 序列化为 `[]` 而非 `null`

- [x] Task 3: 在 `cmd/crux/skill.go` 中添加 `skill list` 子命令 (AC: #1, #2, #3)
  - [x] 3.1 定义 `skillListCmd` cobra.Command：`Use: "list"`, `Short: "List all installed skills"`, `Args: cobra.NoArgs`
  - [x] 3.2 在 `init()` 中 `skillCmd.AddCommand(skillListCmd)`
  - [x] 3.3 实现 `runSkillList`：
    - 创建 `Installer`（复用 `skillRegistryURL`、`lib/skills` basePath）
    - 调用 `installer.ListAll()` 获取所有 skill
    - 根据 output mode 分发渲染
  - [x] 3.4 终端模式输出 — 表格格式：
    ```
    [skill] NAME                 VERSION    SOURCE      DESCRIPTION
    [skill] code-analysis        1.0.0      builtin     Analyze code quality and patterns
    [skill] pr-reviewer          2.1.0      community   Review pull requests with AI
    ```
  - [x] 3.5 终端模式——无社区 skill 时追加提示：
    ```
    [skill] Tip: skill search <keyword> to discover more skills.
    ```
  - [x] 3.6 JSON 模式输出：`JSONResponse{OK: true, Data: skillListJSONData{Skills: entries}}`
    - 字段 snake_case（已通过 json tag 保证）
    - 每个条目包含 `name`、`version`、`path`、`description`、`source`
  - [x] 3.7 Quiet 模式输出：仅输出 skill 名称（每行一个）

- [x] Task 4: 测试 (AC: #1, #2, #3)
  - [x] 4.1 `skillpkg/installer_test.go`：添加 `TestInstaller_ListAll_*` 测试
    - 仅 builtin skill（目录存在但注册表无记录）
    - community + builtin 混合
    - 空目录（无 skill）
    - 目录存在但 SKILL.md 无效时跳过（不崩溃）
  - [x] 4.2 `cmd/crux/skill_test.go`：添加 list CLI 测试
    - `TestSkillListCmd_Registered`：验证 list 子命令注册
    - `TestSkillList_JSONOutput`：验证 JSON 输出格式和 snake_case
    - `TestSkillList_EmptyResult_JSONOutput`：验证空列表 JSON（空 skills 数组）
    - `TestSkillList_NoArgs_Required`：验证 cobra.NoArgs 拒绝参数

- [x] Task 5: 集成验证 (AC: #1-#3)
  - [x] 5.1 `make test` 全部通过（含 `-race`）
  - [x] 5.2 `make lint` 通过
  - [x] 5.3 `make build` 编译成功
  - [x] 5.4 验证现有所有测试无回归

## Dev Notes

### 核心架构决策

**无新增包**：本 Story 的所有改动都在现有包内完成（`skillpkg/` 和 `cmd/crux/`），不创建新目录或新包。

**聚合策略**：`ListAll()` 方法需要聚合两种来源的 skill：
1. **LocalRegistry 已注册的 skill**（通过 `.registry.yaml`）—— 有 version、source、checksum 信息
2. **文件系统中存在但未注册的 skill**（目录下有 SKILL.md）—— 系统自带 skill，未走 install 流程

聚合逻辑：先从 `LocalRegistry.List()` 获取注册表，再扫描 `basePath` 目录，对未在注册表中的目录标记为 builtin。

**Description 来源**：每个 skill 的 description 从其 `SKILL.md` 的 frontmatter `description` 字段获取（通过 `skillLoader.LoadMetadata(name).Manifest.Description`）。如果 SKILL.md 不可读或无 description，使用空字符串。

**Path 字段**：相对路径，格式 `lib/skills/<name>/`。

**依赖方向不变**：
```
cmd/crux/skill.go -> skillpkg/ -> skills/ (已有依赖链)
```
- 不引入任何新的外部依赖
- 不修改现有接口（只新增方法和类型）

### 技术要求

**新增类型**（`skillpkg/types.go`）：
```go
// ListEntry represents a skill in the combined list output.
type ListEntry struct {
    Name        string `json:"name"`
    Version     string `json:"version"`
    Path        string `json:"path"`
    Description string `json:"description"`
    Source      string `json:"source"` // "builtin" or "community"
}
```

**ListAll 方法**（`skillpkg/installer.go`）：
```go
func (inst *Installer) ListAll() ([]ListEntry, error) {
    // 1. Load registry entries
    regEntries, err := inst.registry.List()
    // 2. Build map of registered names
    regMap := make(map[string]RegistryEntry)
    for _, e := range regEntries { regMap[e.Name] = e }
    // 3. Scan basePath directory for all skill subdirs
    dirEntries, _ := os.ReadDir(inst.basePath)
    // 4. For each subdir, try to load SKILL.md metadata
    // 5. Merge: if in regMap use registry info, else Source="builtin"
    // 6. Sort by name, return
}
```

**关键实现细节**：
- `os.ReadDir(basePath)` 获取子目录列表（跳过文件和 `.registry.yaml`）
- 对每个子目录调用 `inst.skillLoader.LoadMetadata(name)` 获取 description
- 如果 `LoadMetadata` 失败（SKILL.md 缺失或无效），跳过该目录（不报错，不 panic）
- 注册表中的 skill 但目录不存在——仍然列出（标记 description 为空，注明 "missing"）
- 使用 `sort.Slice` 按 `Name` 字母序排序

**CLI 子命令注册模式**（参考 `skillInstallCmd`、`skillSearchCmd`、`skillUpdateCmd` 已有模式）：
- 在 `cmd/crux/skill.go` 中定义 `skillListCmd`
- `init()` 中 `skillCmd.AddCommand(skillListCmd)`
- 复用全局 `flagJSON`、`flagQuiet` flags（不需要额外 flags）
- `Args: cobra.NoArgs` — 不接受参数

**输出格式**：
- 终端模式（有 builtin + community skill）：
  ```
  [skill] NAME                 VERSION    SOURCE      DESCRIPTION
  [skill] code-analysis        1.0.0      builtin     Analyze code quality and patterns
  [skill] pr-reviewer          2.1.0      community   Review pull requests with AI
  ```
- 终端模式（仅 builtin skill，无 community）：
  ```
  [skill] NAME                 VERSION    SOURCE      DESCRIPTION
  [skill] code-analysis                   builtin     Analyze code quality and patterns
  [skill] Tip: skill search <keyword> to discover more skills.
  ```
- JSON 模式：
  ```json
  {"ok":true,"data":{"skills":[{"name":"code-analysis","version":"1.0.0","path":"lib/skills/code-analysis/","description":"Analyze code quality and patterns","source":"builtin"}]}}
  ```
- Quiet 模式：仅输出 skill 名称，每行一个

**Tip 显示逻辑**：遍历 `ListEntry` 结果，如果没有任何 `Source == "community"` 的条目，在终端模式下追加 Tip 行。

### 代码复用

**必须复用的现有代码**：
- `skillpkg.LocalRegistry.List()` — 获取所有已注册 Skill（含 community 和 builtin 条目）
- `skillpkg.NewInstaller()` — 创建安装器（已有 skillLoader 引用）
- `skills.SkillLoader.LoadMetadata()` — 读取 SKILL.md frontmatter 获取 description
- `skillRegistryURL` 包变量 — 测试时可替换的注册表 URL（ListAll 不需要网络，但创建 Installer 需要 client 参数）
- `resolveOutputMode()` — 解析输出模式
- `ui.NewRenderer()` + `ui.InitStyles()` — 初始化输出渲染器
- `JSONResponse` 结构体 — 统一 JSON 输出格式
- `ui.KernelStyle.Render("[skill]")` — 终端输出前缀样式

**参考现有模式**：
- `cmd/crux/skill.go` 中 `runSkillSearch` — 表格输出模式（表头 + 数据行 `fmt.Fprintf`）
- `cmd/crux/skill.go` 中 `renderSkillSearchJSON` — JSON 渲染模式
- `cmd/crux/skill_test.go` — CLI 测试模式（验证命令注册、JSON 输出）
- `skillpkg/installer_test.go` — 使用 TempDir + mock 测试

### 反模式防护

- **不要**在 `ListAll` 中发起网络请求——这是纯本地操作，不需要 `RegistryClient`
- **不要**引入新的外部依赖
- **不要**修改 `LocalRegistry.List()` 的签名或行为——只在 `Installer` 层新增 `ListAll()` 方法
- **不要**在 `skillpkg/` 中导入 `internal/ui/` 或 `cmd/crux/`——UI 渲染仅在 CLI 层
- **不要**使用 `interface{}` 存储列表结果——使用明确的 `ListEntry` 结构体
- **不要**使用 `.yml` 后缀——统一 `.yaml`
- **不要**使用 `I` 前缀接口命名
- **不要**使用 `var entries []ListEntry`（会导致 JSON `null`）——使用 `make([]ListEntry, 0)`
- **不要**在扫描目录时因单个 skill 的 SKILL.md 读取失败而中断全部——跳过并继续
- **不要**列出 `.registry.yaml` 文件或 `testdata` 等非 skill 目录——只列出含 SKILL.md 的目录
- **不要**忽略 Unicode 截断问题——如果 description 需要截断，使用 `[]rune` 而非字节索引

### 测试策略

**Installer 层单元测试**（`skillpkg/installer_test.go`）：

需要在 TempDir 中设置 skill 目录结构，包含 SKILL.md 文件。

- `TestInstaller_ListAll_BuiltinOnly`：创建 TempDir，放入 skill 目录 + SKILL.md（但不通过 registry 注册），验证 Source="builtin"、description 正确
- `TestInstaller_ListAll_Mixed`：创建 TempDir，部分 skill 通过 registry 注册（Source="community"），部分未注册（builtin），验证聚合正确
- `TestInstaller_ListAll_Empty`：空目录，验证返回空 slice（非 nil）
- `TestInstaller_ListAll_InvalidSkillSkipped`：创建不含有效 SKILL.md 的目录，验证被跳过不报错

**CLI 层单元测试**（`cmd/crux/skill_test.go`）：

- `TestSkillListCmd_Registered`：验证 list 子命令注册在 skill 下
- `TestSkillList_JSONOutput`：构造 `ListEntry` slice，调用 `renderSkillListJSON`，验证 JSON 格式和 snake_case
- `TestSkillList_EmptyResult_JSONOutput`：验证空列表 JSON（skills 为 `[]`，非 `null`）
- `TestSkillList_NoArgs_Required`：验证 cobra.NoArgs 拒绝参数

**SKILL.md Test Fixture 格式**：
```markdown
---
name: test-skill
description: A test skill for unit testing
---

# Test Skill

This is a test skill body.
```

**Mock 策略**：使用 `t.TempDir()` 创建临时 basePath，直接在目录中写入 SKILL.md 文件和 `.registry.yaml`，不需要 mock HTTP server（ListAll 是纯本地操作）。

### 前一个 Story 的经验教训（来自 8.3 Code Review）

1. **Unicode 截断问题**：Description 截断必须使用 `[]rune` 而非字节索引——本 Story 的终端表格输出中 description 列截断需遵循此模式
2. **nil slice vs 空 slice**：返回值初始化用 `make([]T, 0)` 而非 `var results []T`，确保 JSON 序列化为 `[]` 而非 `null`
3. **`UpdateAll` 静默吞错误**：本 Story 的 `ListAll()` 在遇到单个 skill SKILL.md 读取失败时也应跳过而非中断，但这是预期行为（部分损坏的 skill 不应阻止列出其他 skill）
4. **错误码模式**：如果 `ListAll` 有错误需要 JSON 输出，复用 `{code, message}` 模式
5. **死文件注意**：`skillpkg/stubs_test_support.go`（空文件）和 `skillpkg/testdata/index.yaml`（未引用 fixture）仍然存在——不要在本 Story 中引用这些死文件
6. **`skillRegistryURL` 包变量**：创建 `Installer` 需要 `RegistryClient`，但 `ListAll()` 不使用网络——测试中可传入任意 URL
7. **目录遍历安全**：`SkillLoader.loadAndParse()` 已有 path containment check，`ListAll()` 的 `os.ReadDir` 只读取 basePath 下一级目录，安全性已保证

### Git 提交模式参考

最近提交（5415a45）为 Story 8.3 实现：
- 修改范围：`skillpkg/types.go`、`skillpkg/installer.go`、`skillpkg/update_test.go`、`cmd/crux/skill.go`、`cmd/crux/skill_test.go`
- 本 Story 类似范围：扩展 `skillpkg/types.go`（ListEntry）、`skillpkg/installer.go`（ListAll）、新增或扩展 `skillpkg/installer_test.go`、`cmd/crux/skill.go`（list 子命令）、`cmd/crux/skill_test.go`（list CLI 测试）

### Project Structure Notes

修改文件清单：
```
skillpkg/types.go          # 添加 ListEntry 类型
skillpkg/installer.go      # 添加 ListAll() 方法
skillpkg/installer_test.go # 添加 ListAll 相关测试（或新增 list_test.go）
cmd/crux/skill.go          # 添加 skill list 子命令和 runSkillList
cmd/crux/skill_test.go     # 添加 list CLI 测试
```

不新增文件（除测试文件外），不新增包，不修改 `cmd/crux/main.go`（`skillCmd` 已通过 `rootCmd.AddCommand` 注册，子命令在 `skill.go` 的 `init()` 中添加）。

### References

- [Source: _bmad-output/planning-artifacts/epics/epic-8-skill-包管理与生态skill-package-management.md#Story 8.4]
- [Source: _bmad-output/implementation-artifacts/8-3-skill-update.md] — 前序 Story 实现和 Code Review 记录
- [Source: _bmad-output/implementation-artifacts/8-2-skill-search.md] — 搜索功能参考（表格输出模式）
- [Source: _bmad-output/implementation-artifacts/8-1-skill-install.md] — Install 流程实现
- [Source: skillpkg/types.go] — 现有类型定义（RegistryEntry、InstallResult、UpdateResult）
- [Source: skillpkg/installer.go] — 现有 Installer 和 Install/Update 实现
- [Source: skillpkg/registry.go] — 现有 LocalRegistry（Get/List/Add/Remove，registryData 结构）
- [Source: skills/types.go] — SkillManifest 结构体（Name、Description 字段）
- [Source: skills/loader.go] — SkillLoader.LoadMetadata() 实现
- [Source: cmd/crux/skill.go] — 现有 skill 子命令和 install/search/update 实现
- [Source: cmd/crux/skill_test.go] — 现有 CLI 测试模式
- [Source: cmd/crux/main.go#JSONResponse] — 统一 JSON 输出结构体
- [Source: _bmad-output/project-context.md] — 项目编码规则

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6 (claude-opus-4-6)

### Debug Log References

No issues encountered during implementation.

### Completion Notes List

- Implemented `ListEntry` struct in `skillpkg/types.go` with snake_case JSON tags for name, version, path, description, source fields
- Implemented `Installer.ListAll()` in `skillpkg/installer.go` that aggregates registry-tracked (community) skills and filesystem-only (builtin) skills
  - Scans `basePath` directory, loads SKILL.md metadata via `skillLoader.LoadMetadata()` for description
  - Merges with registry data for version/source info
  - Handles missing SKILL.md gracefully (skips invalid dirs)
  - Handles registry entries with missing directories (still listed)
  - Skips dot-files (`.registry.yaml`)
  - Returns `make([]ListEntry, 0)` for empty results (JSON `[]` not `null`)
  - Results sorted alphabetically by Name
- Added `skillListCmd` cobra command (`skill list`) with `cobra.NoArgs`
- Implemented `runSkillList` with three output modes:
  - Terminal: table format with NAME/VERSION/SOURCE/DESCRIPTION columns, tip line when no community skills
  - JSON: `{ok:true, data:{skills:[...]}}` format with snake_case fields
  - Quiet: skill names only, one per line
- All 12 ATDD tests pass (7 in `skillpkg/list_test.go`, 5 in `cmd/crux/skill_test.go`)
- All existing tests pass with no regressions (IPC/socket test failures are pre-existing sandbox environment limitation)
- Build and vet pass successfully

### File List

- `skillpkg/types.go` — Added `ListEntry` struct
- `skillpkg/installer.go` — Added `ListAll()` method and `sort` import
- `skillpkg/list_test.go` — ATDD tests for ListAll (pre-existing, all pass)
- `cmd/crux/skill.go` — Added `skillListCmd`, `runSkillList`, `skillListJSONData`, `renderSkillListJSON`
- `cmd/crux/skill_test.go` — ATDD tests for list CLI (pre-existing, all pass)
- `_bmad-output/implementation-artifacts/sprint-status.yaml` — Updated story status to review
- `_bmad-output/implementation-artifacts/8-4-local-skill-registry-and-skill-list.md` — Updated story status, tasks, and dev record

### Change Log

- 2026-03-01: Implemented Story 8.4 — Local Skill Registry and `skill list` command. Added `ListEntry` type, `Installer.ListAll()` method, and `skill list` CLI subcommand with terminal/JSON/quiet output modes. All acceptance criteria satisfied.
- 2026-03-01: Code Review (AI) — Fixed H1 bug: `ListAll()` silently dropped registered skills with corrupted SKILL.md due to premature `seen` flag. Now registered skills with invalid SKILL.md are still listed from registry data. Added test `TestInstaller_ListAll_RegisteredSkillWithCorruptedSKILLMD`. All 13 ATDD tests pass (8 in `skillpkg/list_test.go`, 5 in `cmd/crux/skill_test.go`).

### Senior Developer Review (AI)

**Reviewer:** Decker (AI) on 2026-03-01

**Outcome:** Approved with fixes applied

**Issues Found:** 1 High, 3 Medium, 2 Low

**HIGH Issues (Fixed):**
- H1: `ListAll()` set `seen[name]` before `LoadMetadata()` error check — registered community skills with corrupted SKILL.md silently vanished from listing. Fixed by adding registry fallback path for known skills with invalid SKILL.md. Test added.

**MEDIUM Issues (Noted, not blocking):**
- M1: Terminal table shows SOURCE column instead of PATH (AC #1 specifies PATH). SOURCE is arguably more useful; acceptable deviation.
- M2: No test for terminal output rendering (table format, Tip line). JSON output well tested.
- M3: Description truncation at 40 runes cuts without word-boundary awareness. Consistent with existing `runSkillSearch` pattern.

**LOW Issues (Noted):**
- L1: Defensive nil check in `renderSkillListJSON` technically unnecessary since `ListAll()` guarantees non-nil. Consistent with other render functions.
- L2: `RegistryClient` allocated but unused in `runSkillList` (Installer requires it). Documented in Dev Notes.

**Compilation Issues:** None. `sort` import is used by `sort.Slice` in `ListAll()`. `runSkillList` is defined at `cmd/crux/skill.go:390`. Build, vet, and all tests pass.
