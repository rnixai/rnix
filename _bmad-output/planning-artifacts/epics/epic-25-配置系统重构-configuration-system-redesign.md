# Epic 25: 配置系统重构（Configuration System Redesign）

用户安装 Rnix 后运行 `rnix init` 即可创建 `~/.config/rnix/` 全局配置环境，内置 agent/skill 从二进制模板自动提取并可自由定制。项目中创建 `.rnix/` 后，项目级配置自动与全局 deep merge，项目级 agent/skill shadow 全局同名定义。CLI 自动从 CWD 向上发现 `.rnix/`，通过 IPC 传入 daemon。同一 daemon 同时服务多项目，每进程持有不可变配置快照。

> **设计基础**
>
> 本 Epic 基于以下规划文档：
>
> | 文档 | 说明 |
> |------|------|
> | [配置系统重构简报](../config-system-redesign-brief-2026-03-14.md) | Party Mode 多智能体讨论产出 |
> | [PRD FR153-FR164](../prd/functional-requirements.md#configuration-system配置系统phase-2) | 配置系统功能需求 |
> | [PRD NFR53-NFR55](../prd/non-functional-requirements.md#configuration-system-quality配置系统质量phase-2) | 配置系统质量需求 |
> | [Architecture Decision 14-22](../architecture/core-architectural-decisions.md#配置系统重构架构决策epic-23) | 配置系统架构决策 |
> | [Implementation Patterns](../architecture/implementation-patterns-consistency-rules.md#配置系统实现模式epic-23-增量) | 配置系统实现模式 |
>
> **已排除：** FR161（deprecation warning）、FR162（rnix migrate）、NFR56（migrate 数据完整性）——全新项目无需向后兼容。

**架构决策：** Decision 14-22
**FRs covered:** FR153, FR154, FR155, FR156, FR157, FR158, FR159, FR160, FR163, FR164
**NFRs:** NFR53 (init ≤3s), NFR54 (ProjectDir ≤10ms), NFR55 (合并 ≤50ms)

## Story 25.1: internal/config 包与 embed.FS 基础设施

As a 平台构建者,
I want 一个统一的 `internal/config/` 包提供双层配置路径解析、YAML 合并、Agent/Skill shadow 查找和 embed.FS 嵌入提取能力,
So that 所有模块通过统一 API 获取配置路径，消除路径硬编码和逻辑不一致。

**Acceptance Criteria:**

**Given** `internal/config/paths.go` 已实现
**When** 调用 `config.GlobalDir()`
**Then** 返回 `$XDG_CONFIG_HOME/rnix/`（未设置时回退 `~/.config/rnix/`）
**And** 仅依赖标准库 + `internal/types`

**Given** 环境变量 `XDG_CONFIG_HOME=/custom/path`
**When** 调用 `config.GlobalDir()`
**Then** 返回 `/custom/path/rnix/`

**Given** 目录结构 `/a/b/c/.rnix/` 存在
**When** 调用 `config.ProjectDir("/a/b/c/sub/deep")`
**Then** 返回 `"/a/b/c"`
**And** 延迟 ≤ 10ms（NFR54，≤ 20 层目录深度）

**Given** 当前目录到 `$HOME` 路径上无 `.rnix/`
**When** 调用 `config.ProjectDir(cwd)`
**Then** 返回 `("", nil)` 不报错
**And** 到 `$HOME` 或文件系统根停止，防止无限遍历

**Given** `internal/config/merge.go` 已实现
**When** 调用 `DeepMergeYAML(base, override)` 且 base=`{a: {x: 1}}`, override=`{a: {y: 2}}`
**Then** 返回 `{a: {x: 1, y: 2}}`（递归 map 合并）
**And** slice 替换不追加，scalar 覆盖，处理时间 ≤ 50ms（NFR55）

**Given** `internal/config/merge.go` 中 `ShadowResolve` 已实现
**When** 调用 `ShadowResolve("coder", projectAgentsDir, globalAgentsDir)` 且项目级 `coder/` 存在
**Then** 返回项目级路径，全局级被完全遮蔽

**Given** `ShadowResolve` 仅全局级 `coder/` 存在
**When** 调用 `ShadowResolve("coder", projectAgentsDir, globalAgentsDir)`
**Then** 返回全局级路径

**Given** `ListMerged` 已实现
**When** 调用 `ListMerged(projectAgentsDir, globalAgentsDir)` 且项目有 `coder/`、全局有 `coder/` 和 `planner/`
**Then** 返回 `["coder", "planner"]`（不重复）

**Given** `internal/config/embed.go` 已实现
**When** 调用 `ExtractEmbedded(fsys, "lib/agents", targetDir)` 且目标目录中 `coder/` 已存在
**Then** 跳过 `coder/` 不覆盖
**And** 不存在的目录正常创建和复制

**Given** 项目根目录 `embedded.go` 已创建
**When** 编译 `go build ./cmd/rnix/`
**Then** `lib/agents/` 和 `lib/skills/` 通过 `embed.FS` 嵌入到二进制中

**Given** `internal/config/types.go` 已实现
**When** 查看 `Scope`、`GlobalConfig`、`ProjectConfig` 类型
**Then** 类型定义完整，ProjectConfig 字段为值类型或新分配副本（不可变）

**Given** 所有 config 包函数
**When** 运行 `go test -race ./internal/config/...`
**Then** 全部通过，测试使用 `t.TempDir()` + `t.Setenv()` 隔离，不依赖真实 `$HOME`

**Technical Notes:**
- 新建文件：`internal/config/paths.go`、`paths_test.go`、`merge.go`、`merge_test.go`、`embed.go`、`embed_test.go`、`types.go`、`doc.go`
- 新建文件：项目根 `embedded.go`（embed.FS 声明）
- 依赖方向：`internal/config` → 标准库 only（os, path/filepath, io/fs, embed, fmt）
- 每文件职责边界：paths.go 纯路径计算（不读 YAML）、merge.go 纯数据结构合并（不做 I/O）、embed.go 嵌入提取（不做路径解析）
- DeepMergeYAML 测试覆盖：空 map、单侧有值、嵌套递归、类型冲突、slice 替换、三层以上深度
- 配置文件命名：进入 `.rnix/` 或 `~/.config/rnix/` 后去掉 `rnix-` 前缀（FR159）

**FRs covered:** FR153, FR155, FR156, FR157, FR158（embed 声明）, FR159
**NFRs covered:** NFR54, NFR55

## Story 25.2: rnix init 与全局配置加载

As a Rnix 用户,
I want 运行 `rnix init` 获得完整的全局和项目配置环境，daemon 从新目录加载配置,
So that 安装后即可使用，内置 agent/skill 可自由定制，配置集中管理。

**Acceptance Criteria:**

**Given** 全局目录 `~/.config/rnix/` 不存在
**When** 运行 `rnix init`
**Then** 创建 `~/.config/rnix/` 目录及子目录（agents/、skills/）
**And** 从 embed.FS 提取内置 agent 到 `~/.config/rnix/agents/`
**And** 从 embed.FS 提取内置 skill 到 `~/.config/rnix/skills/`
**And** 生成默认 `providers.yaml` 和 `config.yaml`
**And** 执行时间 ≤ 3 秒（NFR53）

**Given** 全局目录 `~/.config/rnix/` 已存在且包含用户修改的 agent
**When** 运行 `rnix init`
**Then** 跳过已存在的文件和目录，不覆盖用户修改
**And** 输出 "skipped: file exists" 提示

**Given** 当前目录下无 `.rnix/` 目录
**When** 运行 `rnix init`
**Then** 在当前目录创建 `.rnix/` 及子目录（agents/、skills/、data/）
**And** 生成空的 `config.yaml`

**Given** 当前目录已有 `.rnix/`
**When** 运行 `rnix init`
**Then** 跳过项目初始化，输出提示信息
**And** 所有操作幂等

**Given** daemon 启动
**When** 加载全局配置
**Then** 从 `~/.config/rnix/` 读取 `providers.yaml`、`config.yaml`、`mcp.yaml`
**And** 解析并缓存为 `GlobalConfig` 结构体
**And** 注册全局 LLM 驱动到 `DriverRegistry`

**Given** 全局 `providers.yaml` 不存在
**When** daemon 启动
**Then** 使用内置默认配置 + 输出 info 日志，不崩溃

**Given** 全局 `providers.yaml` YAML 语法错误
**When** daemon 启动
**Then** 启动失败，输出详细错误信息（文件名、行号、错误原因）

**Technical Notes:**
- 新建文件：`cmd/rnix/init.go`
- 修改文件：`cmd/rnix/main.go`（daemon 启动 GlobalConfig 加载流程）
- `rnix init` 单一入口不区分全局/项目，自动判断
- 配置加载错误分级严格遵循架构规范

**FRs covered:** FR154, FR158（提取部分）, FR164（全局加载部分）
**NFRs covered:** NFR53

## Story 25.3: 项目级配置合并与模块适配

As a Rnix 用户,
I want 在项目中使用 `.rnix/` 自定义配置、agent 和 skill，系统自动与全局配置合并,
So that 不同项目可以有不同的配置和工具集，同一 daemon 支持多项目。

**Acceptance Criteria:**

**Given** `ipc/protocol.go` 中 `SpawnRequest` 已新增 `ProjectDir` 字段
**When** CLI 端在包含 `.rnix/` 的项目中发送 spawn 请求
**Then** payload 包含 `"project_dir": "/path/to/project"`
**And** 字段标记 `json:"project_dir,omitempty"`，旧版 CLI 不发送时 daemon 按空处理

**Given** 项目 `.rnix/providers.yaml` 新增了一个 provider
**When** daemon 处理该项目的 spawn 请求
**Then** `DeepMergeYAML(全局providers, 项目providers)` 生成合并后配置
**And** 新增的 provider 注册到当前请求的 DriverRegistry

**Given** 项目 `.rnix/agents/coder/` 存在，全局 `~/.config/rnix/agents/coder/` 也存在
**When** spawn 请求指定 `--agent=coder`
**Then** `agents/loader.go` 通过 `ShadowResolve` 加载项目级 `coder/`
**And** 全局级 `coder/` 被完全遮蔽

**Given** 项目 `.rnix/skills/review/` 存在
**When** spawn 请求引用 skill `review`
**Then** `skills/loader.go` 通过 `ShadowResolve` 优先加载项目级

**Given** 项目中无 `.rnix/agents/planner/` 但全局有
**When** spawn 请求指定 `--agent=planner`
**Then** 回退到全局级 `~/.config/rnix/agents/planner/`

**Given** daemon 同时服务项目 A 和项目 B
**When** 项目 A spawn 请求使用 project_dir_A，项目 B 使用 project_dir_B
**Then** 各进程持有独立的 `ProjectConfig` 快照，互不影响
**And** `Process.ProjectConfig` 在进程生命周期内不可变

**Given** CLI 端从 CWD 向上查找
**When** 在 `/a/b/c/src/` 工作，`.rnix/` 在 `/a/b/c/`
**Then** CLI 发现 `project_dir="/a/b/c"` 并通过 IPC 传入

**Given** CLI 端未找到 `.rnix/`
**When** 在任意目录发送 spawn 请求
**Then** `project_dir` 为空，daemon 仅使用全局配置

**Given** 项目 `.rnix/providers.yaml` YAML 语法错误
**When** daemon 处理该项目的 spawn 请求
**Then** spawn 失败，返回 IPC 错误（文件名 + 错误原因）
**And** 不影响其他项目的 spawn

**Given** 运行时数据目录
**When** 系统写入 records/traces/reputation/immune 数据
**Then** 存放在 `.rnix/data/` 子目录下，与配置文件物理隔离

**Given** `drivers/llm/config.go` 已适配
**When** 加载 providers 配置
**Then** 通过 `config.GlobalDir()` 和 `config.ResolvePath()` 获取路径
**And** 不再使用旧的 `FindProvidersConfigPath()` 硬编码逻辑

**Given** `kernel/process.go` 已适配
**When** 创建新进程
**Then** `Process` 结构体包含 `ProjectConfig *config.ProjectConfig` 字段
**And** 配置快照在 spawn 时生成，进程退出时自然释放

**Technical Notes:**
- 修改文件：`ipc/protocol.go`（SpawnRequest 新增字段）
- 修改文件：`agents/loader.go`（签名变更，改用 ShadowResolve + 多目录查找）
- 修改文件：`skills/loader.go`（签名变更，改用 ShadowResolve + 多目录查找）
- 修改文件：`drivers/llm/config.go`（FindProvidersConfigPath 迁移到 config 包）
- 修改文件：`kernel/process.go`（Process 新增 ProjectConfig 字段）
- 修改文件：`kernel/init.go`（LoadInitConfig 使用 config.ResolvePath）
- 修改文件：`cmd/rnix/main.go`（CLI ProjectDir 发现 + daemon spawn handler 项目合并）
- ProjectConfig 创建后禁止修改——如需变更，创建新快照
- 所有配置路径禁止直接拼接，必须调用 config 包函数

**FRs covered:** FR160, FR163, FR164（项目合并部分）
**NFRs covered:** NFR54, NFR55
