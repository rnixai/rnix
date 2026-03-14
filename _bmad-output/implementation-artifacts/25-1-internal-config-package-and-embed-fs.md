# Story 25.1: internal/config 包与 embed.FS 基础设施

Status: done

## Story

As a 平台构建者,
I want 一个统一的 `internal/config/` 包提供双层配置路径解析、YAML 合并、Agent/Skill shadow 查找和 embed.FS 嵌入提取能力,
So that 所有模块通过统一 API 获取配置路径，消除路径硬编码和逻辑不一致。

## Acceptance Criteria

1. **Given** `internal/config/paths.go` 已实现
   **When** 调用 `config.GlobalDir()`
   **Then** 返回 `$XDG_CONFIG_HOME/rnix/`（未设置时回退 `~/.config/rnix/`）
   **And** 仅依赖标准库 + `internal/types`

2. **Given** 环境变量 `XDG_CONFIG_HOME=/custom/path`
   **When** 调用 `config.GlobalDir()`
   **Then** 返回 `/custom/path/rnix/`

3. **Given** 目录结构 `/a/b/c/.rnix/` 存在
   **When** 调用 `config.ProjectDir("/a/b/c/sub/deep")`
   **Then** 返回 `"/a/b/c"`
   **And** 延迟 ≤ 10ms（NFR54，≤ 20 层目录深度）

4. **Given** 当前目录到 `$HOME` 路径上无 `.rnix/`
   **When** 调用 `config.ProjectDir(cwd)`
   **Then** 返回 `("", nil)` 不报错
   **And** 到 `$HOME` 或文件系统根停止，防止无限遍历

5. **Given** `internal/config/merge.go` 已实现
   **When** 调用 `DeepMergeYAML(base, override)` 且 base=`{a: {x: 1}}`, override=`{a: {y: 2}}`
   **Then** 返回 `{a: {x: 1, y: 2}}`（递归 map 合并）
   **And** slice 替换不追加，scalar 覆盖，处理时间 ≤ 50ms（NFR55）

6. **Given** `internal/config/merge.go` 中 `ShadowResolve` 已实现
   **When** 调用 `ShadowResolve("coder", projectAgentsDir, globalAgentsDir)` 且项目级 `coder/` 存在
   **Then** 返回项目级路径，全局级被完全遮蔽

7. **Given** `ShadowResolve` 仅全局级 `coder/` 存在
   **When** 调用 `ShadowResolve("coder", projectAgentsDir, globalAgentsDir)`
   **Then** 返回全局级路径

8. **Given** `ListMerged` 已实现
   **When** 调用 `ListMerged(projectAgentsDir, globalAgentsDir)` 且项目有 `coder/`、全局有 `coder/` 和 `planner/`
   **Then** 返回 `["coder", "planner"]`（不重复）

9. **Given** `internal/config/embed.go` 已实现
   **When** 调用 `ExtractEmbedded(fsys, "lib/agents", targetDir)` 且目标目录中 `coder/` 已存在
   **Then** 跳过 `coder/` 不覆盖
   **And** 不存在的目录正常创建和复制

10. **Given** 项目根目录 `embedded.go` 已创建
    **When** 编译 `go build ./cmd/rnix/`
    **Then** `lib/agents/` 和 `lib/skills/` 通过 `embed.FS` 嵌入到二进制中

11. **Given** `internal/config/types.go` 已实现
    **When** 查看 `Scope`、`GlobalConfig`、`ProjectConfig` 类型
    **Then** 类型定义完整，ProjectConfig 字段为值类型或新分配副本（不可变）

12. **Given** 所有 config 包函数
    **When** 运行 `go test -race ./internal/config/...`
    **Then** 全部通过，测试使用 `t.TempDir()` + `t.Setenv()` 隔离，不依赖真实 `$HOME`

## Tasks / Subtasks

### Task 1: 创建 `internal/config/doc.go` 和 `types.go`（AC: #11）

- [x] 1.1 新建 `internal/config/doc.go`，包注释说明 config 包职责（双层配置路径解析、YAML 合并、Shadow 查找、嵌入资源提取）
- [x] 1.2 新建 `internal/config/types.go`，定义以下类型：
  - `Scope` 类型（`int` 底层）及常量 `ScopeGlobal`、`ScopeProject`
  - `GlobalConfig` 结构体（Dir, Providers, Config, MCP, AgentsDir, SkillsDir 字段）
  - `ProjectConfig` 结构体（ProjectDir, Providers, Config, MCP, AgentDirs, SkillDirs, InitConfig, ComposeSpec 字段）
  - **注意**: `Providers` 等指针字段必须指向新分配的副本，禁止共享 GlobalConfig 的底层数据

### Task 2: 实现 `internal/config/paths.go`（AC: #1, #2, #3, #4）

- [x] 2.1 新建 `internal/config/paths.go`，实现 `GlobalDir() (string, error)`：
  - 读取 `$XDG_CONFIG_HOME`，设置则返回 `$XDG_CONFIG_HOME/rnix/`
  - 未设置则用 `os.UserHomeDir()` 回退到 `~/.config/rnix/`
  - `$HOME` 也获取失败则返回错误
- [x] 2.2 实现 `ProjectDir(startDir string) (string, error)`：
  - `filepath.Abs(startDir)` 转绝对路径
  - 从 `startDir` 向上逐级遍历，检查 `.rnix/` 子目录是否存在（`os.Stat` + `IsDir`）
  - 找到则返回 `.rnix/` 的**父目录**路径
  - 到 `$HOME` 或文件系统根（`parent == dir`）停止
  - 未找到返回 `("", nil)` 不报错
- [x] 2.3 实现 `ResolvePath(scope Scope, projectDir, filename string) string`：
  - `ScopeGlobal` → `filepath.Join(globalDir, filename)`
  - `ScopeProject` → `filepath.Join(projectDir, ".rnix", filename)`
- [x] 2.4 实现 `ResolveDir(scope Scope, projectDir, dirname string) string`
- [x] 2.5 新建 `internal/config/paths_test.go`，测试覆盖：
  - `GlobalDir` 默认 `~/.config/rnix/`（mock `$HOME`）
  - `GlobalDir` 尊重 `$XDG_CONFIG_HOME`
  - `ProjectDir` 找到 `.rnix/` 返回父目录
  - `ProjectDir` 多层嵌套向上查找
  - `ProjectDir` 未找到返回空字符串 + nil 错误
  - `ProjectDir` 到 `$HOME` 停止
  - `ProjectDir` 到文件系统根停止
  - `ResolvePath` 两种 scope 路径拼接正确
  - 所有测试用 `t.TempDir()` + `t.Setenv()`

### Task 3: 实现 `internal/config/merge.go`（AC: #5, #6, #7, #8）

- [x] 3.1 新建 `internal/config/merge.go`，实现 `DeepMergeYAML(base, override map[string]any) map[string]any`：
  - 创建新 result map（base 浅拷贝）
  - 遍历 override：key 不存在于 base 则直接赋值
  - 双方都是 `map[string]any` 则递归合并
  - 其他情况（scalar、slice、类型冲突）override 覆盖
  - **slice 策略：替换不追加**
- [x] 3.2 实现 `ShadowResolve(name string, dirs ...string) string`：
  - 按 dirs 顺序遍历，`os.Stat(filepath.Join(dir, name))` 找到目录则返回完整路径
  - 全部未找到返回空字符串
- [x] 3.3 实现 `ListMerged(dirs ...string) ([]string, error)`：
  - 按 dirs 顺序遍历，`os.ReadDir` 列出子目录
  - 用 `map[string]bool` 去重
  - 返回排序后的去重名称列表
- [x] 3.4 新建 `internal/config/merge_test.go`，测试覆盖矩阵：
  - DeepMerge: 空 map + 空 map
  - DeepMerge: 单侧有值
  - DeepMerge: 嵌套递归（两层以上）
  - DeepMerge: 类型冲突（map vs scalar）override 优先
  - DeepMerge: slice 替换不追加
  - DeepMerge: 三层深度递归
  - DeepMerge: nil 值处理
  - ShadowResolve: 项目级存在 → 返回项目级
  - ShadowResolve: 仅全局级存在 → 返回全局级
  - ShadowResolve: 都不存在 → 返回空字符串
  - ListMerged: 去重 + 排序验证
  - ListMerged: 空目录处理

### Task 4: 实现 `internal/config/embed.go`（AC: #9）

- [x] 4.1 新建 `internal/config/embed.go`，实现 `ExtractEmbedded(fsys fs.FS, srcRoot, targetDir string) error`：
  - `fs.WalkDir(fsys, srcRoot, ...)` 遍历嵌入资源
  - 用 `filepath.Rel(srcRoot, path)` 剥离前缀得到相对路径（**关键：必须剥离，否则路径多出 `lib/agents/` 前缀**）
  - 目标路径 = `filepath.Join(targetDir, relPath)`
  - 目录：`os.MkdirAll` 创建
  - 文件：检查目标是否已存在，**已存在则跳过不覆盖**
  - 不存在则从 embed.FS 读取内容写入
- [x] 4.2 实现 `ExtractEmbeddedForce(fsys fs.FS, srcRoot, targetDir string) error`（强制覆盖版本，用于 upgrade 场景）
- [x] 4.3 新建 `internal/config/embed_test.go`，测试覆盖：
  - 正常提取到空目录
  - 已存在文件不覆盖
  - 已存在目录跳过
  - 嵌套目录结构保持
  - Force 版本覆盖已存在文件
  - 使用 `testing/fstest.MapFS` 模拟 embed.FS

### Task 5: 创建项目根 `embedded.go`（AC: #10）

- [x] 5.1 新建项目根 `embedded.go`（package `rnix`），声明 embed.FS：
  ```go
  package rnix

  import "embed"

  //go:embed lib/agents
  var EmbeddedAgents embed.FS

  //go:embed lib/skills
  var EmbeddedSkills embed.FS
  ```
- [x] 5.2 验证 `go build ./cmd/rnix/` 编译成功，`lib/agents/` 和 `lib/skills/` 被正确嵌入
- [x] 5.3 在 `cmd/rnix/` 中可通过 `import rnix "github.com/rnixai/rnix"` 引用 `rnix.EmbeddedAgents` 和 `rnix.EmbeddedSkills`

### Task 6: 全量测试验证（AC: #12）

- [x] 6.1 运行 `go test -race ./internal/config/...` 确保全部通过
- [x] 6.2 运行 `make all` 确保 lint + vet + test + build 全部通过
- [x] 6.3 确认 `internal/config` 包的依赖仅为标准库（os, path/filepath, io/fs, embed, fmt, sort），无外部依赖

## Dev Notes

### 架构约束

- **依赖方向**: `internal/config` 仅依赖标准库（os, path/filepath, io/fs, embed, fmt, sort）。禁止导入项目内任何包（包括 `internal/types`）。所有上层模块（agents/, skills/, drivers/llm/, kernel/, ipc/, cmd/）可导入 `internal/config`
- **types.go 仅含类型定义**: 不含任何函数实现。`GlobalConfig` 和 `ProjectConfig` 中引用其他包类型（如 `*llm.ProvidersConfig`）时使用 `any` 或接口类型避免引入循环依赖
- **每文件职责边界严格隔离**:
  - `paths.go` — 纯路径计算，不读 YAML、不解析内容
  - `merge.go` — 纯数据结构合并，不做 I/O、不读文件
  - `embed.go` — 嵌入资源提取到磁盘，不做路径解析
  - `types.go` — 类型定义 only

### 关键实现细节

1. **`ProjectDir()` 返回值约定**: 空字符串 ≠ 错误。调用方用 `if projectDir != ""` 判断是否在项目中，**不要用 `err != nil` 判断**
2. **`DeepMergeYAML` 合并语义**:
   - map + map → 递归合并
   - map + 非 map → override 覆盖
   - slice + slice → override **替换**（不追加）
   - scalar + scalar → override 覆盖
   - 任意 + nil/缺失 → 保持 base
   - nil/缺失 + 任意 → 用 override
3. **embed.FS 路径前缀剥离**: `//go:embed lib/agents` 嵌入后，WalkDir 的 path 带 `lib/agents/` 前缀。必须用 `filepath.Rel(srcRoot, path)` 剥离，否则提取时路径错误
4. **`ShadowResolve` 检查目录而非文件**: `os.Stat(candidate)` 后需验证 `info.IsDir()`
5. **`ListMerged` 需排序**: 返回的名称列表必须排序，确保结果稳定可预测
6. **`ExtractEmbedded` 的 `fsys` 参数类型为 `fs.FS` 而非 `embed.FS`**: 便于测试时使用 `testing/fstest.MapFS` 替代真实 embed.FS

### 现有代码中需关注的模式

- **导入别名**: `internal/config` 无 stdlib 冲突，直接用 `config.GlobalDir()` 不需别名
- **嵌入包引用**: `cmd/rnix/` 中用 `import rnix "github.com/rnixai/rnix"` 引用根包
- **测试标准**: 项目使用 Go 标准 `testing` 包，`-race` 竞态检测。测试函数命名 `Test<Type>_<Method>`
- **YAML 库**: 使用 `github.com/goccy/go-yaml`（不用 `gopkg.in/yaml.v3`）。本 Story 的 `DeepMergeYAML` 操作 `map[string]any` 不直接依赖 YAML 库
- **YAML 后缀**: 统一 `.yaml`（不用 `.yml`）

### 文件创建清单

| 文件 | 类型 | 说明 |
|------|------|------|
| `internal/config/doc.go` | 新建 | 包文档 |
| `internal/config/types.go` | 新建 | Scope, GlobalConfig, ProjectConfig |
| `internal/config/paths.go` | 新建 | GlobalDir, ProjectDir, ResolvePath, ResolveDir |
| `internal/config/paths_test.go` | 新建 | 路径解析测试 |
| `internal/config/merge.go` | 新建 | DeepMergeYAML, ShadowResolve, ListMerged |
| `internal/config/merge_test.go` | 新建 | 合并逻辑测试 |
| `internal/config/embed.go` | 新建 | ExtractEmbedded, ExtractEmbeddedForce |
| `internal/config/embed_test.go` | 新建 | 嵌入提取测试 |
| `embedded.go` | 新建 | 项目根 embed.FS 声明 |

### 不在本 Story 范围内

- `cmd/rnix/init.go`（rnix init 命令）→ Story 25.2
- daemon 全局配置加载流程 → Story 25.2
- IPC protocol.go 扩展 project_dir 字段 → Story 25.3
- agents/loader.go、skills/loader.go 适配 → Story 25.3
- drivers/llm/config.go 路径迁移 → Story 25.3
- 向后兼容 + rnix migrate → 排除在 Epic 25 范围外

### 组合矩阵

| 本 Story 功能 | 交互点 | 影响 | 需验证 |
|--------------|--------|------|--------|
| `embedded.go` embed.FS | `go build` 编译 | 二进制体积增大（嵌入 lib/agents/ + lib/skills/） | 是 |
| `config.GlobalDir()` | `$XDG_CONFIG_HOME` 环境变量 | 与 `drivers/llm/config.go` 中现有 XDG 逻辑一致性 | 否（Story 25.2/25.3 适配） |
| `config.ProjectDir()` | 文件系统 stat 调用 | 性能：≤ 20 层 stat < 10ms | 是 |
| `DeepMergeYAML` | 不依赖外部 | 纯函数，无副作用 | 否 |
| `ShadowResolve` | 文件系统 stat 调用 | 查找目录存在性 | 是 |
| `ListMerged` | `os.ReadDir` | 目录列表，排序稳定 | 是 |
| `ExtractEmbedded` | 文件系统写入 | 创建目录和文件，幂等跳过 | 是 |

### Project Structure Notes

- `internal/config/` 与 `internal/types/`、`internal/xsync/`、`internal/ui/` 组织模式一致
- `embedded.go` 放在项目根目录（`package rnix`），与 `go.mod` 同级
- `lib/agents/` 和 `lib/skills/` 保持原位（开发时编辑，编译时嵌入，运行时不再作为查找路径）

### References

- [Source: _bmad-output/planning-artifacts/epics/epic-25-配置系统重构-configuration-system-redesign.md#Story 25.1]
- [Source: _bmad-output/planning-artifacts/architecture/core-architectural-decisions.md#Decision 14-22]
- [Source: _bmad-output/planning-artifacts/architecture/implementation-patterns-consistency-rules.md#配置系统实现模式]
- [Source: _bmad-output/planning-artifacts/config-system-redesign-brief-2026-03-14.md]
- [Source: _bmad-output/project-context.md]

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6

### Debug Log References

- golangci-lint 缓存损坏问题：通过 `golangci-lint cache clean` 解决
- modernize lint 规则：`maps.Copy` 替代手动 for-range 循环拷贝 map

### Completion Notes List

- Task 1: 创建 `doc.go`（包文档）和 `types.go`（Scope/GlobalConfig/ProjectConfig 类型定义，所有 pointer 字段使用 `any` 避免循环依赖）
- Task 2: 实现 `paths.go`（GlobalDir/ProjectDir/ResolvePath/ResolveDir），11 个测试覆盖 XDG_CONFIG_HOME、HOME 回退、.rnix/ 向上查找、边界停止、Scope 路径拼接
- Task 3: 实现 `merge.go`（DeepMergeYAML/ShadowResolve/ListMerged），16 个测试覆盖递归合并、类型冲突、slice 替换、nil 值、shadow 优先级、去重排序、非存在目录
- Task 4: 实现 `embed.go`（ExtractEmbedded/ExtractEmbeddedForce），5 个测试使用 `testing/fstest.MapFS` 覆盖空目标提取、跳过已存在文件/目录、嵌套结构保持、强制覆盖
- Task 5: 创建项目根 `embedded.go`，声明 `EmbeddedAgents` 和 `EmbeddedSkills` embed.FS，编译验证成功
- Task 6: 全量验证通过 — 40 个测试全部 PASS（race 检测），`make all`（lint 0 issues + vet + test + build）成功，依赖仅标准库（io/fs, maps, os, path/filepath, sort）

### Change Log

- 2026-03-15: 完成 Story 25-1 全部实现 — 新建 `internal/config/` 包（doc.go, types.go, paths.go, merge.go, embed.go）及完整测试（paths_test.go, merge_test.go, embed_test.go），新建项目根 `embedded.go`
- 2026-03-15: 代码审查修复 — 补充 ATDD 规划中缺失的 9 个测试：types_test.go（3 个 AC#11 测试）、paths_test.go（+5 个边界测试：XDG 尾部斜杠、空 XDG、HOME 缺失、相对路径、.rnix 是文件）、embed_test.go（+1 个路径前缀剥离验证）。测试总数从 31 增加到 40

### File List

- `internal/config/doc.go` (新建) — 包文档
- `internal/config/types.go` (新建) — Scope, GlobalConfig, ProjectConfig 类型定义
- `internal/config/types_test.go` (新建) — 类型定义测试 (3 tests)
- `internal/config/paths.go` (新建) — GlobalDir, ProjectDir, ResolvePath, ResolveDir
- `internal/config/paths_test.go` (新建) — 路径解析测试 (16 tests)
- `internal/config/merge.go` (新建) — DeepMergeYAML, ShadowResolve, ListMerged
- `internal/config/merge_test.go` (新建) — 合并逻辑测试 (16 tests)
- `internal/config/embed.go` (新建) — ExtractEmbedded, ExtractEmbeddedForce
- `internal/config/embed_test.go` (新建) — 嵌入提取测试 (6 tests)
- `embedded.go` (新建) — 项目根 embed.FS 声明 (EmbeddedAgents, EmbeddedSkills)

## Senior Developer Review (AI)

### Reviewer
Adversarial Code Review Agent (Claude Opus 4.6) — 2026-03-15

### Review Outcome: APPROVED

### Git vs Story 差异分析
- **Git 未跟踪文件与 Story File List 一致**: `embedded.go`, `internal/config/` 全部 8 个源文件均在 git 未跟踪列表中，完全匹配
- **sprint-status.yaml 修改**: 已修改但未暂存，合理（反映 Story 状态变更）
- **差异数量**: 0（无不一致）

### AC 验证结果

| AC | 状态 | 验证方式 |
|----|------|----------|
| AC1 GlobalDir 默认路径 | IMPLEMENTED | TestGlobalDir_Default 验证 `~/.config/rnix/` 回退 |
| AC2 GlobalDir XDG | IMPLEMENTED | TestGlobalDir_XDGConfigHome 验证自定义 XDG 路径 |
| AC3 ProjectDir 查找 | IMPLEMENTED | TestProjectDir_Found + TestProjectDir_NestedLookup 验证向上查找 |
| AC4 ProjectDir 未找到 | IMPLEMENTED | TestProjectDir_NotFound + StopsAtHome + StopsAtFilesystemRoot |
| AC5 DeepMergeYAML | IMPLEMENTED | 8 个测试覆盖递归/类型冲突/slice 替换/nil/三层深度 |
| AC6 ShadowResolve 项目级 | IMPLEMENTED | TestShadowResolve_ProjectExists 验证优先级 |
| AC7 ShadowResolve 全局级 | IMPLEMENTED | TestShadowResolve_OnlyGlobal 验证回退 |
| AC8 ListMerged | IMPLEMENTED | TestListMerged_Dedup_Sorted 验证去重 + 排序 |
| AC9 ExtractEmbedded | IMPLEMENTED | 6 个测试覆盖提取/跳过/嵌套/强制覆盖/前缀剥离 |
| AC10 embed.FS 嵌入 | IMPLEMENTED | `go build ./cmd/rnix/` 编译成功 |
| AC11 类型定义 | IMPLEMENTED | types_test.go 3 个测试验证 Scope/GlobalConfig/ProjectConfig |
| AC12 全量测试 | IMPLEMENTED | 40 个测试全部 PASS（-race） |

### Task 完成审计

所有 6 个 Task（含 22 个 Subtask）标记 [x]，全部验证通过。无虚假标记。

### 代码质量评估

**安全性**: 无注入风险，路径拼接使用 `filepath.Join`，目录遍历有 `$HOME` 和文件系统根的双重边界保护
**性能**: `ProjectDir` 使用简单 `os.Stat` 循环，`DeepMergeYAML` 使用 `maps.Copy` 高效拷贝
**错误处理**: 所有 I/O 操作正确检查错误，`ListMerged` 优雅处理不存在的目录
**代码质量**: 函数职责单一，命名清晰，无魔法数字，无复杂度过高的函数
**依赖**: 仅标准库（io/fs, maps, os, path/filepath, sort），无外部依赖 -- 符合架构约束

### 审查期间修复的问题

| 严重度 | 问题 | 修复方式 |
|--------|------|----------|
| HIGH | ATDD 规划了 38 个测试但仅实现 31 个，缺少 `types_test.go`（3 个 AC#11 测试）和 6 个边界测试 | 补充全部 9 个缺失测试，测试总数增至 40 |
| MEDIUM | File List 未包含 `types_test.go` | 更新 File List 添加该文件 |

### 总结

代码实现质量高，架构清晰，职责分离彻底。主要问题是测试覆盖与 ATDD 规划不完全对齐（缺少 `types_test.go` 和若干边界测试），已在审查中补充修复。修复后全部 40 个测试通过，Story 状态更新为 done。
