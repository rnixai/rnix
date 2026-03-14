---
stepsCompleted: ['step-01-preflight-and-context', 'step-02-generation-mode', 'step-03-test-strategy', 'step-04-generate-tests']
lastStep: 'step-04-generate-tests'
lastSaved: '2026-03-15'
workflowType: 'testarch-atdd'
inputDocuments:
  - '_bmad-output/implementation-artifacts/25-1-internal-config-package-and-embed-fs.md'
---

# ATDD Checklist - Epic 25, Story 1: internal/config 包与 embed.FS 基础设施

**Date:** 2026-03-15
**Primary Test Level:** Unit (Go backend)

---

## Story Summary

Story 25.1 实现统一的 `internal/config/` 包，提供双层配置路径解析（全局/项目级）、YAML 深度合并、Agent/Skill shadow 查找、嵌入资源提取能力。所有函数仅依赖标准库，无外部依赖。

**As a** 平台构建者
**I want** 一个统一的 `internal/config/` 包提供双层配置路径解析、YAML 合并、Agent/Skill shadow 查找和 embed.FS 嵌入提取能力
**So that** 所有模块通过统一 API 获取配置路径，消除路径硬编码和逻辑不一致

---

## Acceptance Criteria

1. **AC1 - GlobalDir 默认路径:** Given `internal/config/paths.go` 已实现, When 调用 `config.GlobalDir()`, Then 返回 `$XDG_CONFIG_HOME/rnix/`（未设置时回退 `~/.config/rnix/`）And 仅依赖标准库 + `internal/types`
2. **AC2 - GlobalDir 尊重 XDG:** Given 环境变量 `XDG_CONFIG_HOME=/custom/path`, When 调用 `config.GlobalDir()`, Then 返回 `/custom/path/rnix/`
3. **AC3 - ProjectDir 查找:** Given 目录结构 `/a/b/c/.rnix/` 存在, When 调用 `config.ProjectDir("/a/b/c/sub/deep")`, Then 返回 `"/a/b/c"` And 延迟 <= 10ms
4. **AC4 - ProjectDir 未找到:** Given 当前目录到 `$HOME` 路径上无 `.rnix/`, When 调用 `config.ProjectDir(cwd)`, Then 返回 `("", nil)` 不报错
5. **AC5 - DeepMergeYAML:** Given `internal/config/merge.go` 已实现, When 调用 `DeepMergeYAML(base, override)`, Then 递归 map 合并，slice 替换不追加，scalar 覆盖
6. **AC6 - ShadowResolve 项目级优先:** Given `ShadowResolve` 已实现, When 项目级 `coder/` 存在, Then 返回项目级路径
7. **AC7 - ShadowResolve 全局级回退:** Given 仅全局级 `coder/` 存在, When 调用 `ShadowResolve`, Then 返回全局级路径
8. **AC8 - ListMerged 去重:** Given `ListMerged` 已实现, When 项目有 `coder/`、全局有 `coder/` 和 `planner/`, Then 返回 `["coder", "planner"]`（不重复）
9. **AC9 - ExtractEmbedded 幂等:** Given `internal/config/embed.go` 已实现, When 目标目录中 `coder/` 已存在, Then 跳过不覆盖
10. **AC10 - embed.FS 嵌入:** Given 项目根目录 `embedded.go` 已创建, When 编译 `go build ./cmd/rnix/`, Then `lib/agents/` 和 `lib/skills/` 通过 embed.FS 嵌入
11. **AC11 - 类型定义:** Given `internal/config/types.go` 已实现, When 查看类型, Then Scope、GlobalConfig、ProjectConfig 完整定义
12. **AC12 - 全量测试:** Given 所有 config 包函数, When 运行 `go test -race ./internal/config/...`, Then 全部通过

---

## Failing Tests Created (RED Phase)

### Unit Tests (38 tests)

---

#### paths_test.go — AC1/AC2: GlobalDir (5 tests)

**File:** `internal/config/paths_test.go`

- RED **Test:** TestGlobalDir_Default
  - **Status:** RED -- `GlobalDir` 函数未定义
  - **Verifies:** AC#1 -- 未设置 `$XDG_CONFIG_HOME` 时返回 `~/.config/rnix/`
  - **Priority:** P0

- RED **Test:** TestGlobalDir_XDGConfigHome
  - **Status:** RED -- `GlobalDir` 函数未定义
  - **Verifies:** AC#2 -- `$XDG_CONFIG_HOME=/custom/path` 时返回 `/custom/path/rnix/`
  - **Priority:** P0

- RED **Test:** TestGlobalDir_XDGConfigHome_TrailingSlash
  - **Status:** RED -- `GlobalDir` 函数未定义
  - **Verifies:** AC#2 -- `$XDG_CONFIG_HOME=/custom/path/` 带尾部斜杠时路径拼接正确
  - **Priority:** P1

- RED **Test:** TestGlobalDir_EmptyXDG
  - **Status:** RED -- `GlobalDir` 函数未定义
  - **Verifies:** AC#1 -- `$XDG_CONFIG_HOME=""` 空字符串时回退到默认路径
  - **Priority:** P1

- RED **Test:** TestGlobalDir_NoHome
  - **Status:** RED -- `GlobalDir` 函数未定义
  - **Verifies:** AC#1 -- `$HOME` 也获取失败时返回错误
  - **Priority:** P1

---

#### paths_test.go — AC3/AC4: ProjectDir (7 tests)

- RED **Test:** TestProjectDir_Found
  - **Status:** RED -- `ProjectDir` 函数未定义
  - **Verifies:** AC#3 -- `.rnix/` 存在时返回父目录
  - **Priority:** P0

- RED **Test:** TestProjectDir_NestedDeep
  - **Status:** RED -- `ProjectDir` 函数未定义
  - **Verifies:** AC#3 -- 多层嵌套向上查找 `.rnix/`
  - **Priority:** P0

- RED **Test:** TestProjectDir_NotFound
  - **Status:** RED -- `ProjectDir` 函数未定义
  - **Verifies:** AC#4 -- 未找到 `.rnix/` 返回 `("", nil)`
  - **Priority:** P0

- RED **Test:** TestProjectDir_StopsAtHome
  - **Status:** RED -- `ProjectDir` 函数未定义
  - **Verifies:** AC#4 -- 到 `$HOME` 停止遍历
  - **Priority:** P1

- RED **Test:** TestProjectDir_StopsAtRoot
  - **Status:** RED -- `ProjectDir` 函数未定义
  - **Verifies:** AC#4 -- 到文件系统根停止，防止无限遍历
  - **Priority:** P1

- RED **Test:** TestProjectDir_RelativePath
  - **Status:** RED -- `ProjectDir` 函数未定义
  - **Verifies:** AC#3 -- 传入相对路径时自动转绝对路径
  - **Priority:** P2

- RED **Test:** TestProjectDir_DotRnixIsFile
  - **Status:** RED -- `ProjectDir` 函数未定义
  - **Verifies:** AC#4 -- `.rnix` 是文件而非目录时跳过继续向上查找
  - **Priority:** P2

---

#### paths_test.go — AC1: ResolvePath/ResolveDir (4 tests)

- RED **Test:** TestResolvePath_GlobalScope
  - **Status:** RED -- `ResolvePath` 函数未定义
  - **Verifies:** AC#1 -- `ScopeGlobal` 时返回全局路径拼接
  - **Priority:** P1

- RED **Test:** TestResolvePath_ProjectScope
  - **Status:** RED -- `ResolvePath` 函数未定义
  - **Verifies:** AC#1 -- `ScopeProject` 时返回项目 `.rnix/` 路径拼接
  - **Priority:** P1

- RED **Test:** TestResolveDir_GlobalScope
  - **Status:** RED -- `ResolveDir` 函数未定义
  - **Verifies:** AC#1 -- `ScopeGlobal` 目录路径拼接
  - **Priority:** P1

- RED **Test:** TestResolveDir_ProjectScope
  - **Status:** RED -- `ResolveDir` 函数未定义
  - **Verifies:** AC#1 -- `ScopeProject` 目录路径拼接
  - **Priority:** P1

---

#### merge_test.go — AC5: DeepMergeYAML (8 tests)

**File:** `internal/config/merge_test.go`

- RED **Test:** TestDeepMergeYAML_BothEmpty
  - **Status:** RED -- `DeepMergeYAML` 函数未定义
  - **Verifies:** AC#5 -- 两个空 map 合并返回空 map
  - **Priority:** P1

- RED **Test:** TestDeepMergeYAML_BaseOnly
  - **Status:** RED -- `DeepMergeYAML` 函数未定义
  - **Verifies:** AC#5 -- override 为空时保持 base
  - **Priority:** P1

- RED **Test:** TestDeepMergeYAML_OverrideOnly
  - **Status:** RED -- `DeepMergeYAML` 函数未定义
  - **Verifies:** AC#5 -- base 为空时使用 override
  - **Priority:** P1

- RED **Test:** TestDeepMergeYAML_RecursiveNested
  - **Status:** RED -- `DeepMergeYAML` 函数未定义
  - **Verifies:** AC#5 -- `{a: {x: 1}}` + `{a: {y: 2}}` = `{a: {x: 1, y: 2}}`
  - **Priority:** P0

- RED **Test:** TestDeepMergeYAML_ThreeLevelDeep
  - **Status:** RED -- `DeepMergeYAML` 函数未定义
  - **Verifies:** AC#5 -- 三层深度递归合并
  - **Priority:** P1

- RED **Test:** TestDeepMergeYAML_TypeConflict
  - **Status:** RED -- `DeepMergeYAML` 函数未定义
  - **Verifies:** AC#5 -- map vs scalar 类型冲突时 override 覆盖
  - **Priority:** P1

- RED **Test:** TestDeepMergeYAML_SliceReplace
  - **Status:** RED -- `DeepMergeYAML` 函数未定义
  - **Verifies:** AC#5 -- slice 替换不追加
  - **Priority:** P0

- RED **Test:** TestDeepMergeYAML_NilValues
  - **Status:** RED -- `DeepMergeYAML` 函数未定义
  - **Verifies:** AC#5 -- nil 值处理：保持 base、用 override
  - **Priority:** P2

---

#### merge_test.go — AC6/AC7: ShadowResolve (3 tests)

- RED **Test:** TestShadowResolve_ProjectLevel
  - **Status:** RED -- `ShadowResolve` 函数未定义
  - **Verifies:** AC#6 -- 项目级存在时返回项目级路径
  - **Priority:** P0

- RED **Test:** TestShadowResolve_GlobalFallback
  - **Status:** RED -- `ShadowResolve` 函数未定义
  - **Verifies:** AC#7 -- 仅全局级存在时返回全局级路径
  - **Priority:** P0

- RED **Test:** TestShadowResolve_NotFound
  - **Status:** RED -- `ShadowResolve` 函数未定义
  - **Verifies:** AC#6/AC#7 -- 都不存在时返回空字符串
  - **Priority:** P1

---

#### merge_test.go — AC8: ListMerged (3 tests)

- RED **Test:** TestListMerged_Dedup
  - **Status:** RED -- `ListMerged` 函数未定义
  - **Verifies:** AC#8 -- 去重 + 排序验证
  - **Priority:** P0

- RED **Test:** TestListMerged_EmptyDirs
  - **Status:** RED -- `ListMerged` 函数未定义
  - **Verifies:** AC#8 -- 空目录处理
  - **Priority:** P1

- RED **Test:** TestListMerged_NonExistentDir
  - **Status:** RED -- `ListMerged` 函数未定义
  - **Verifies:** AC#8 -- 不存在的目录处理不报错
  - **Priority:** P2

---

#### embed_test.go — AC9: ExtractEmbedded (5 tests)

**File:** `internal/config/embed_test.go`

- RED **Test:** TestExtractEmbedded_EmptyTarget
  - **Status:** RED -- `ExtractEmbedded` 函数未定义
  - **Verifies:** AC#9 -- 正常提取到空目录
  - **Priority:** P0

- RED **Test:** TestExtractEmbedded_SkipExisting
  - **Status:** RED -- `ExtractEmbedded` 函数未定义
  - **Verifies:** AC#9 -- 已存在文件不覆盖
  - **Priority:** P0

- RED **Test:** TestExtractEmbedded_NestedDirs
  - **Status:** RED -- `ExtractEmbedded` 函数未定义
  - **Verifies:** AC#9 -- 嵌套目录结构保持
  - **Priority:** P1

- RED **Test:** TestExtractEmbeddedForce_Overwrite
  - **Status:** RED -- `ExtractEmbeddedForce` 函数未定义
  - **Verifies:** AC#9 -- Force 版本覆盖已存在文件
  - **Priority:** P1

- RED **Test:** TestExtractEmbedded_PathPrefix
  - **Status:** RED -- `ExtractEmbedded` 函数未定义
  - **Verifies:** AC#9 -- embed.FS 路径前缀 `lib/agents/` 正确剥离
  - **Priority:** P0

---

#### types_test.go — AC11: 类型定义 (3 tests)

**File:** `internal/config/types_test.go`

- RED **Test:** TestScope_Constants
  - **Status:** RED -- `Scope`、`ScopeGlobal`、`ScopeProject` 未定义
  - **Verifies:** AC#11 -- Scope 类型及常量定义完整
  - **Priority:** P1

- RED **Test:** TestGlobalConfig_Fields
  - **Status:** RED -- `GlobalConfig` 类型未定义
  - **Verifies:** AC#11 -- GlobalConfig 结构体字段完整
  - **Priority:** P1

- RED **Test:** TestProjectConfig_Fields
  - **Status:** RED -- `ProjectConfig` 类型未定义
  - **Verifies:** AC#11 -- ProjectConfig 结构体字段完整
  - **Priority:** P1

---

## AC <-> Test 覆盖矩阵

| AC | Test(s) | 覆盖方式 |
|----|---------|----------|
| AC1 GlobalDir 默认 | TestGlobalDir_Default, TestGlobalDir_EmptyXDG, TestGlobalDir_NoHome | 默认路径回退 + 空 XDG + HOME 缺失 |
| AC2 GlobalDir XDG | TestGlobalDir_XDGConfigHome, TestGlobalDir_XDGConfigHome_TrailingSlash | XDG 路径 + 尾部斜杠边界 |
| AC3 ProjectDir 查找 | TestProjectDir_Found, TestProjectDir_NestedDeep, TestProjectDir_RelativePath, TestProjectDir_DotRnixIsFile | 正常查找 + 多层嵌套 + 相对路径 + 文件而非目录 |
| AC4 ProjectDir 未找到 | TestProjectDir_NotFound, TestProjectDir_StopsAtHome, TestProjectDir_StopsAtRoot | 未找到返回空 + HOME 边界 + 根边界 |
| AC5 DeepMergeYAML | TestDeepMergeYAML_BothEmpty, TestDeepMergeYAML_BaseOnly, TestDeepMergeYAML_OverrideOnly, TestDeepMergeYAML_RecursiveNested, TestDeepMergeYAML_ThreeLevelDeep, TestDeepMergeYAML_TypeConflict, TestDeepMergeYAML_SliceReplace, TestDeepMergeYAML_NilValues | 空 + 单侧 + 递归 + 三层 + 类型冲突 + slice 替换 + nil |
| AC6 ShadowResolve 项目级 | TestShadowResolve_ProjectLevel | 项目级存在返回项目级 |
| AC7 ShadowResolve 全局级 | TestShadowResolve_GlobalFallback | 仅全局级存在返回全局级 |
| AC8 ListMerged | TestListMerged_Dedup, TestListMerged_EmptyDirs, TestListMerged_NonExistentDir | 去重排序 + 空目录 + 不存在目录 |
| AC9 ExtractEmbedded | TestExtractEmbedded_EmptyTarget, TestExtractEmbedded_SkipExisting, TestExtractEmbedded_NestedDirs, TestExtractEmbeddedForce_Overwrite, TestExtractEmbedded_PathPrefix | 正常提取 + 跳过已存在 + 嵌套 + 强制覆盖 + 前缀剥离 |
| AC10 embed.FS | (编译验证，非单元测试) | `go build ./cmd/rnix/` 编译成功 |
| AC11 类型定义 | TestScope_Constants, TestGlobalConfig_Fields, TestProjectConfig_Fields | 类型和字段完整性 |
| AC12 全量测试 | 全部 38 个测试 | `go test -race ./internal/config/...` 全部通过 |

---

## 测试隔离策略

- **临时目录:** 所有文件系统操作通过 `t.TempDir()` 创建临时目录，测试完自动清理
- **环境变量:** `t.Setenv("XDG_CONFIG_HOME", ...)` 和 `t.Setenv("HOME", ...)` 隔离环境
- **embed.FS 模拟:** 使用 `testing/fstest.MapFS` 替代真实 embed.FS
- **并行安全:** 所有测试标记 `t.Parallel()`
- **Race 检测:** 通过 `go test -race` 运行
- **无外部依赖:** `internal/config` 仅依赖标准库（os, path/filepath, io/fs, fmt, sort）

---

## 实现目标文件

| 文件 | 状态 | 说明 |
|------|------|------|
| `internal/config/doc.go` | 待创建 | 包文档 |
| `internal/config/types.go` | 待创建 | Scope, GlobalConfig, ProjectConfig 类型定义 |
| `internal/config/paths.go` | 待创建 | GlobalDir, ProjectDir, ResolvePath, ResolveDir |
| `internal/config/merge.go` | 待创建 | DeepMergeYAML, ShadowResolve, ListMerged |
| `internal/config/embed.go` | 待创建 | ExtractEmbedded, ExtractEmbeddedForce |
| `embedded.go` | 待创建 | 项目根 embed.FS 声明 |
| `internal/config/paths_test.go` | 已规划 (RED) | 16 个测试 |
| `internal/config/merge_test.go` | 已规划 (RED) | 14 个测试 |
| `internal/config/embed_test.go` | 已规划 (RED) | 5 个测试 |
| `internal/config/types_test.go` | 已规划 (RED) | 3 个测试 |

---

## 测试优先级分布

| 优先级 | 数量 | 测试 |
|--------|------|------|
| P0 | 12 | GlobalDir_Default, GlobalDir_XDGConfigHome, ProjectDir_Found, ProjectDir_NestedDeep, ProjectDir_NotFound, DeepMergeYAML_RecursiveNested, DeepMergeYAML_SliceReplace, ShadowResolve_ProjectLevel, ShadowResolve_GlobalFallback, ListMerged_Dedup, ExtractEmbedded_EmptyTarget, ExtractEmbedded_SkipExisting, ExtractEmbedded_PathPrefix |
| P1 | 19 | GlobalDir_TrailingSlash, GlobalDir_EmptyXDG, GlobalDir_NoHome, ProjectDir_StopsAtHome, ProjectDir_StopsAtRoot, ResolvePath_Global/Project, ResolveDir_Global/Project, DeepMerge (BothEmpty, BaseOnly, OverrideOnly, ThreeLevelDeep, TypeConflict), ShadowResolve_NotFound, ListMerged_EmptyDirs, ExtractEmbedded_NestedDirs, ExtractEmbeddedForce_Overwrite, Scope_Constants, GlobalConfig_Fields, ProjectConfig_Fields |
| P2 | 4 | ProjectDir_RelativePath, ProjectDir_DotRnixIsFile, DeepMergeYAML_NilValues, ListMerged_NonExistentDir |

---

## 下一步

1. 实现 `internal/config/types.go`、`paths.go`、`merge.go`、`embed.go` 使所有测试编译通过
2. 运行 `go test -race ./internal/config/...` 验证全部 GREEN
3. 运行 `make all` 确保 lint + vet + test + build 全部通过
