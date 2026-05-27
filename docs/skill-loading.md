# Skill 加载与使用机制

本文档梳理 Rnix 中 skill 系统从「设计层 → 应用层 → 运行时」的完整工作流，重点解释：

- 5 层架构与目录布局
- 3 种代码层加载方式（LoadMetadata / LoadFull / WithDiag）
- 3 种应用层加载路径（CLI 包管理 / Spawn 静态注入 / 运行时按需加载）
- `discover_skill` + `specialize` 的触发链与配置要求
- CLI 通路（4 scope）与 Spawn 通路（2 scope）的范围差异

姐妹文档：
- [skills.md](skills.md) — Epic 47 多 scope 包管理操作手册（`rnix skill` CLI 行为）

## 目录

- [整体架构](#整体架构)
- [路径解析（agentskills.io 4 scope 规范）](#路径解析agentskillsio-4-scope-规范)
- [代码层：3 种加载方式](#代码层3-种加载方式)
- [应用层入口 A：CLI 静态包管理](#应用层入口-acli-静态包管理-rnix-skill)
- [应用层入口 B：Spawn 静态注入](#应用层入口-bspawn-静态注入-rnix-apply--rnix-compose)
- [System Prompt 拼装格式](#system-prompt-拼装格式)
- [应用层入口 C：运行时按需加载](#应用层入口-c运行时按需加载-discover_skill--specialize)
- [配置要求清单](#配置要求清单)
- [安全与防呆机制](#安全与防呆机制)
- [应用层关键裂缝](#应用层关键裂缝cli-4-scope-vs-spawn-2-scope)
- [最小可行配置示例](#最小可行配置示例)

## 整体架构

Skill 系统分为 5 个独立层次，职责清晰：

| 层 | 包 | 职责 |
|---|---|---|
| 类型层 | `skills/types.go` | `SkillManifest`（YAML frontmatter）+ `SkillInfo`（含 body） |
| 解析层 | `skills/loader.go` | `SkillLoader` — 路径解析 + SKILL.md 解析 |
| 发现层 | `skills/discovery.go` | `SkillDiscovery` — 扫描目录列出可用 skill |
| 管理层 | `skills/manager.go` + `skillpkg/` | 运行时 CRUD + 从 registry 安装/更新 |
| VFS 暴露层 | `drivers/skills/` | `/dev/skills/manage` 让 agent 自己增删改 skill |

应用层加载入口分布：

```
                         ┌──────────────────────────────┐
                         │  user terminal / shell       │
                         └──────────────┬───────────────┘
                                        │
        ┌───────────────────────────────┼───────────────────────────────┐
        │                               │                               │
        ▼                               ▼                               ▼
  rnix skill ...                  rnix apply / spawn              gdb / hot-patch
  (CLI 静态包管理)                  (静态注入 system prompt)        (运行时插桩)
        │                               │                               │
        ▼                               ▼                               │
  4-scope ResolveSkillScopes      2-scope (project + global)            │
  .rnix/skills/                   project: .rnix/skills/                │
  .agents/skills/                 global:  ~/.config/rnix/skills/       │
  ~/.config/rnix/skills/                  │                              │
  ~/.agents/skills/                       ▼                              │
        │                          AgentLoader.Load                     │
        ▼                                 │                              │
  skillpkg.Installer            (skills + deferred_skills)              │
  (HTTP registry + .tar.gz)              │                              │
                                          ▼                              │
                                  Process.Skills / DeferredSkills        │
                                          │                              │
                ┌─────────────────────────┼──────────────────────────┐   │
                ▼                         ▼                          ▼   ▼
        SystemPrompt 拼装         discover_skill (ToolSearch)    specialize
        instructions +            (deferred 元数据排序)            (运行时挂载 body)
        skill.Body * N            ────────────►  specialize  ◄────────────
        synergies                          (LLM 主动按需调用)
```

## 路径解析（agentskills.io 4 scope 规范）

由 `internal/config/skillscope.go::ResolveSkillScopes` 实现，**4 个根目录按优先级排列**：

```
1. <projectDir>/.rnix/skills      (project / native)   ← 优先级最高
2. <projectDir>/.agents/skills    (project / agents)
3. $XDG_CONFIG_HOME/rnix/skills   (user / native)
4. $HOME/.agents/skills           (user / agents)      ← 优先级最低
```

排序规则：
- **跨 scope**：project > user
- **同 scope**：native > agents（rnix 自有命名空间优先）

可选 `WithAncestorTraversal(true)` 启用祖先目录遍历——从 cwd 向上爬到 `.git/` 边界，每层检查 `.rnix/skills` 与 `.agents/skills`，受 `maxAncestorDepth=6` 与 `maxDirs=2000` 双重保护。

## 代码层：3 种加载方式

`SkillLoader`（`skills/loader.go`）基于「first dir wins」的 shadow 解析，对外暴露三种调用：

### 1. LoadMetadata — 仅读 frontmatter

```go
func (l *SkillLoader) LoadMetadata(skillName string) (*SkillInfo, error)
```

跳过 body 字符串构造，用于 `discover_skill` 列表场景，性能敏感。

### 2. LoadFull — 完整加载（frontmatter + body）

```go
func (l *SkillLoader) LoadFull(skillName string) (*SkillInfo, error)
```

返回完整 `SkillInfo{Manifest, Body, Dir}`。Process spawn 时拼装 system prompt 用这个。

### 3. WithDiag 变体 — 带 lenient 诊断

```go
func (l *SkillLoader) LoadMetadataWithDiag(name) (*SkillInfo, []LenientWarning, error)
func (l *SkillLoader) LoadFullWithDiag(name) (*SkillInfo, []LenientWarning, error)
```

Story 47.2 引入的宽松验证：
- **LenientSkipError**（致命跳过）：missing name / missing description / YAML 解析失败
- **LenientWarning**（警告但加载）：name 与父目录不匹配、name 超过 64 字符

旧调用者用 backward-compat shim 保持二值返回。

## 应用层入口 A：CLI 静态包管理 `rnix skill`

`cmd/rnix/skill.go` 注册了 4 个子命令，**仅用于安装/列出/搜索**，不触发任何 agent 运行：

| 子命令 | 作用 | 关键 flag |
|---|---|---|
| `rnix skill install <name>` | 从 registry 下载 .tar.gz、校验 sha256、解压到 writeScope | `-g`（user scope）、`--shared`（agents 命名空间）、`--force` |
| `rnix skill search [keyword]` | HTTP 查询 registry index | — |
| `rnix skill update [name...]` | 拉新版本，**保留 origin scope** 不跨 scope 漂移 | 无参时更新全部 |
| `rnix skill list` | 跨 4 scope 列出，含 shadow 诊断 | `-g`/`-p` scope 过滤 |

### 写位置决策矩阵

`resolveWriteScope`（`cmd/rnix/skill.go:125`）：

| cwd 在项目里？ | `-g` | `--shared` | 写到 |
|---|---|---|---|
| 是 | ✗ | ✗ | `<proj>/.rnix/skills/` |
| 是 | ✗ | ✓ | `<proj>/.agents/skills/` |
| 是 | ✓ | ✗ | `~/.config/rnix/skills/` |
| 是 | ✓ | ✓ | `~/.agents/skills/` |
| 否 | — | ✗ | `~/.config/rnix/skills/` |
| 否 | — | ✓ | `~/.agents/skills/` |

### 诊断管道

4 类警告写到 stderr 一行一条（JSON 模式则塞进 `diagnostics` 节点）：

- `Warnings` — shadow（同名 skill 被高优先级覆盖）
- `Skipped` — 致命 lenient 跳过（缺 name/description、yaml 报错）
- `Lenient` — 警告但加载（name 与父目录不匹配等）
- `Trust` — 项目未信任的 advisory（Story 47.4）

## 应用层入口 B：Spawn 静态注入 `rnix apply` / `rnix compose`

这是 agent **真正用到 skill** 的入口。

### 第 1 步：CLI 决定项目根

`apply.go:63` 调 `config.ProjectDir(cwd)`，把绝对路径塞到 `SpawnRequest.ProjectDir`，发给 daemon。

### 第 2 步：Daemon `resolveProjectContext` 构造 2 层搜索路径

`ipc/server_spawn.go:154` —— **注意只用 2 层目录**，不是 CLI 那 4 层：

```go
projectAgentsDir := filepath.Join(projectDir, ".rnix", "agents")
projectSkillsDir := filepath.Join(projectDir, ".rnix", "skills")
agentDirs := []string{projectAgentsDir, gc.AgentsDir}   // 项目优先
skillDirs := []string{projectSkillsDir, gc.SkillsDir}   // 项目优先
```

「项目优先」由 `config.ShadowResolve` 在加载器内部实现：第一个命中的目录获胜。

### 第 3 步：AgentLoader 拉 agent.yaml + skill bodies

`agents/loader.go::Load(agentName)` 一次性把所有依赖灌进 `AgentInfo`：

```yaml
# agent.yaml
name: orchestrator
skills:                      # 立即全文加载（LoadFull → body 进 prompt）
  - planning
  - file-edit
deferred_skills:             # 只加载元数据（LoadMetadata），body 等用时再拿
  - code-analysis
  - pr-review
mcp:
  - playwright
```

两类 skill 的核心区别在于 **何时进 system prompt**：

- `skills:` → 立即 `LoadFull` → `SystemPrompt()` 拼接其 body
- `deferred_skills:` → 仅 `LoadMetadata` → 名称/描述/SearchHint 注册为 `DeferredSkillMeta`，body 不进 prompt

### 第 4 步：捕获到 process，持久化

`kernel/spawn.go:189` 把 `skill.Body` 存到 `proc.SkillBodies`（map[name]body）。reaper 回收时写到 `.rnix/data/steps/<uuid>/process-meta.json`，resume 时按名复原。

## System Prompt 拼装格式

`AgentInfo.SystemPrompt()`（`agents/types.go:73`）按固定模板拼：

```
<agent.Instructions>

Base directory for this skill: /abs/path/to/skill1
<skill1.Body>

Base directory for this skill: /abs/path/to/skill2
<skill2.Body>

[Skill Synergy]
<DetectSynergies() 产物>
```

**3 件值得记住的事**：

1. `Base directory:` 行让 skill body 里的相对路径变成绝对路径——LLM 可以引用 skill 包内的脚本/资源
2. `[Skill Synergy]` 是 Story 21.4 引入的 emergent 指令注入——当多个 skill 出现已声明的协同关系时拼接补充提示
3. `AllowedTools()` 取所有 skill `allowed-tools` 的**并集**，决定 VFS 设备白名单

## 应用层入口 C：运行时按需加载 `discover_skill` + `specialize`

这是 skill 的「动态扩展」路径，避免 prompt 一开始就塞爆。

### 触发方式：完全由 LLM 自主决定

这不是用户主动调用的命令，**没有 CLI 入口**。它是 agent 进程运行中由 LLM 在 `reasonStep` 循环里**自己选择**的工具调用动作，触发条件是 LLM 觉得「我需要更多能力」。

### 完整触发链路

```
agent spawn 启动
  ├─ 加载 deferred_skills → 取 metadata（name/description/search_hint）
  ├─ proc.DeferredSkills 注册 N 个 DeferredSkillMeta
  └─ metaToolDefs 给 LLM 看的工具清单里默认追加：
       ├─ ToolSearch  (默认存在,无需开关)
       ├─ Skill       (默认存在,无需开关)
       └─ skill_<name> × N    占位 ToolDef，描述写明 "deferred"
              ↓
       LLM 第 K 步：根据用户意图 + 工具清单决定要「找能力」
              ↓
       生成 tool_call: {"name":"ToolSearch","input":{"query":"code review"}}
              ↓
       kernel/tool_exec.go:930 ActionDiscoverSkill
       → discoverSkills() 给 proc.DeferredSkills 打分
       → 返回 JSON: {query, matches:[{name, description, score}]}
              ↓
       LLM 第 K+1 步：根据匹配结果决定要加载哪个
              ↓
       生成 tool_call: {"name":"Skill","input":{"skill":"code-review"}}
              ↓
       kernel/tool_exec.go:780 ActionSpecialize
       → ProjectConfig.SkillLoader(name) 拉 LoadFull
       → 把 body 以 [Dynamic Skill Loaded: <name>] 形式 AppendMessage(user)
       → 把 skill 的 allowed_tools 追加到 proc.AllowedDevices（设备白名单扩张）
       → 后续步骤就能用这个 skill 的能力了
```

### 工具命名

LLM 侧看到的是 **`ToolSearch`** 和 **`Skill`** 这两个名字（`kernel/toolgen.go:148,129`），**不是** `discover_skill` / `specialize`——后者是 kernel 内部的 ActionType。命名故意对齐 Claude Code 的训练分布锚点。

### discover_skill 评分规则

`kernel/discover.go:96`：

| 匹配位置 | 分数 |
|---|---|
| 名字完全相等 | +12 |
| 名字部分包含 | +6 |
| `search_hint` 命中 | +4 |
| description 命中 | +2 |

已加载的 skill 自动从结果中过滤（`discover.go:51`）；`max_results` 默认 5，硬上限 50。

## 配置要求清单

### 1. agent.yaml 必须声明 `deferred_skills`

这是最关键的开关。**没有这行配置，`ToolSearch` 即便存在也没东西可找**（`proc.DeferredSkills` 为空，永远返回空匹配）。

```yaml
# .rnix/agents/my-agent/agent.yaml
name: my-agent
description: ...
models:
  preferred: sonnet
skills:                        # 立即加载、body 进 system prompt
  - planning
deferred_skills:               # ← 这才是 discover_skill 能找到的池子
  - code-review
  - pr-analysis
  - refactor-helper
```

**目前 rnix 内置的三个 agent（orchestrator / code-analyst / stem）都没用 deferred_skills**——这是个相对新的、待普及的能力。需要在项目自己的 agent.yaml 里显式启用。

### 2. SKILL.md 可选加 `search_hint`

```markdown
---
name: code-review
description: Review code for bugs and style issues
allowed-tools: /dev/fs /dev/shell
search_hint: review audit lint quality bugs PR diff   ← 关键词扩展
---
# Code Review Skill
...
```

`search_hint` 不进 system prompt、不影响 body，**纯粹是 ToolSearch 的关键词扩展词表**。当 description 写得很正式、但用户/LLM 用的关键词风格不同时特别有用。

### 3. Skill 必须在 spawn 的搜索路径中

deferred skill 的 `LoadMetadata` 走的是 spawn 链路的 2 层 skillDirs：

- `<proj>/.rnix/skills/`
- `~/.config/rnix/skills/`

```yaml
deferred_skills:
  - code-review     # 必须存在于这两个目录之一,否则 spawn 直接报错
```

`.agents/skills/`（agentskills.io 兼容命名空间）**不会被 spawn 加载**——详见 [应用层关键裂缝](#应用层关键裂缝cli-4-scope-vs-spawn-2-scope)。

### 完全不需要额外配置的部分

- `ToolSearch` 和 `Skill` 这两个元工具**默认始终注册**给所有 agent（`metaToolDefs` 头部数组）——不像 `EnterPlanMode` 受 `planning: false` 控制
- 不需要在 `allowed-tools` 里声明它们
- 不需要任何 daemon 配置或 feature flag

## 安全与防呆机制

### specialize 的 3 重防护

1. **占位 ToolDef**（`toolgen.go:201`）：每个 deferred skill 会注册一个名为 `skill_<name>` 的占位工具，描述里写明「deferred, use ToolSearch + Skill」。如果 LLM 不调 ToolSearch 直接尝试调 `skill_code_review`，会落到 `ActionDeferredSkillPlaceholder` 分支返回纠错信息——不会直接执行也不会被 ToolSearch 当成已加载

2. **重复加载防护**（`tool_exec.go:798,819`）：specialize 前后两次检查 `proc.Skills` 是否已含此名，已加载则返回「Don't try to call this skill as a tool」消息——避免 LLM 把 skill 当工具反复调

3. **slot 不足回滚**（`tool_exec.go:880`）：specialize 加载一个 skill 至少需要 2 个 ctx slot（tool result + user message），不够则**完整回滚**：从 `proc.Skills` / `SkillBodies` / `SkillDirs` 删除，并从 `AllowedDevices` 移除该 skill 引入的设备——保持事务一致性

### 路径与 trust 防护

- **path traversal 拦截**（`loader.go:88`）：skill name 含 `/`、`\`、`..` 直接拒绝
- **path containment 检查**（`loader.go:103`）：解析后的绝对路径必须落在 searchDirs 之一下方
- **Trust check**（Story 47.4）：install/update 前对未信任项目目录发 advisory

### Compact 时的保留

`kernel/compact.go:299` —— 当 context 触发压缩时，`compactPrompt` 会用 `k.skillLoader(name)` 重新拉一遍 `proc.SkillBodies` 里登记的 skill，连同 system prompt 一起恢复，**避免压缩把 skill 全部冲掉**。

## 应用层关键裂缝（CLI 4 scope vs Spawn 2 scope）

| 维度 | CLI 包管理 (`rnix skill`) | Spawn 运行时 |
|---|---|---|
| 搜索目录数 | 4（project + user × native + agents） | 2（project + user，只 native） |
| 谁来加载 | `skillpkg.Installer` | `agents.AgentLoader` |
| 读取深度 | 仅 metadata + .tar.gz 落盘 | `LoadFull`（默认）+ `LoadMetadata`（deferred） |
| 是否进 prompt | 否 | `skills:` 是，`deferred_skills:` 否 |
| 跨 scope 行为 | install 看 writeScope，update 锁 origin | 项目永远覆盖 global |
| Trust 检查 | install/update 前置 advisory | 暂未接入 |

**核心张力**：CLI 通路支持 agentskills.io 生态的 4 scope；spawn 通路只读 rnix-native 的 2 scope。在 `.agents/skills/` 装了 skill 想让 agent 用，目前**必须**也放到 `.rnix/skills/` 或 `~/.config/rnix/skills/`，否则 agent.yaml `skills:` / `deferred_skills:` 引用不到。

这是当前应用层最容易踩的坑。

## 最小可行配置示例

让 deferred skill + ToolSearch 在你的项目里跑起来：

```bash
# 1. 在项目里建一个 deferred skill 仓
mkdir -p .rnix/skills/code-review
cat > .rnix/skills/code-review/SKILL.md <<'EOF'
---
name: code-review
description: Audit code changes for bugs and style issues
allowed-tools: /dev/fs /dev/shell
search_hint: review audit lint diff PR quality
---
# Code Review
When reviewing code, ...
EOF

# 2. 让某个 agent 的 manifest 用它
mkdir -p .rnix/agents/dev/
cat > .rnix/agents/dev/agent.yaml <<'EOF'
name: dev
description: dev agent
models:
  preferred: sonnet
skills:
  - planning           # 假设你已有这个
deferred_skills:
  - code-review        # 进 metadata 池
EOF
cat > .rnix/agents/dev/instructions.md <<'EOF'
You are a development agent. When you need a specific capability, use ToolSearch first to find it, then Skill to load it.
EOF

# 3. spawn 起来看 LLM 是否会自己触发
rnix apply "review the diff in this branch" --agent dev
```

### 调试提示

- 如果 LLM 不主动调 `ToolSearch`，在 `instructions.md` 里**显式提示**——Claude 类模型相对自然，弱模型可能需要在 system prompt 加一行：「When unsure what tool to use, call ToolSearch with keywords first」
- 用 `rnix dashboard` Timeline 面板观察工具调用序列，看 LLM 实际做了什么
- `events.jsonl` 里 `ReasonStep` 事件的 `action` 字段可看到 `discover_skill` / `specialize` 是否发生

## 相关源码锚点

- `internal/config/skillscope.go::ResolveSkillScopes` — 4 scope 路径解析
- `skills/loader.go::SkillLoader` — 解析 SKILL.md（LoadMetadata / LoadFull）
- `skills/discovery.go::SkillDiscovery.DiscoverAll` — 批量扫描去重
- `skillpkg/installer.go::Installer` — 多 scope 感知的包管理器
- `skillpkg/client.go::RegistryClient` — registry HTTP 客户端
- `cmd/rnix/skill.go` — CLI 子命令实现 + writeScope 决策
- `cmd/rnix/apply.go` — spawn 入口
- `ipc/server_spawn.go::resolveProjectContext` — 2 scope spawn 搜索路径构造
- `agents/loader.go::AgentLoader.Load` — 把 agent.yaml + skills + deferred_skills 灌进 AgentInfo
- `agents/types.go::AgentInfo.SystemPrompt` — system prompt 拼装格式
- `kernel/spawn.go` — Process 启动时 skill 注入
- `kernel/toolgen.go::metaToolDefs` — `ToolSearch` / `Skill` / `skill_<name>` 占位工具注册
- `kernel/discover.go::discoverSkills` — deferred skill 评分匹配
- `kernel/tool_exec.go::ActionDiscoverSkill` / `ActionSpecialize` — 运行时动作执行
- `kernel/compact.go` — 压缩时 skill 恢复
- `drivers/skills/driver.go` — `/dev/skills/manage` VFS 设备（agent 自反射 CRUD）

## 设计要点速记

- **解析与发现解耦**：`SkillLoader` 不知道目录是 "项目级" 还是 "用户级"，`SkillDiscovery` 也不关心 scope，scope 信息全部沉到 `internal/config/skillscope.go`
- **Shadow 解析**：项目 skill 永远覆盖用户 skill；install/update 都走显式 `writeScope` 不会跨 scope
- **Lenient 兼容**：对接 agentskills.io 生态时容忍非致命瑕疵，但通过诊断通道回传给 UI
- **运行时可塑**：skill 既能预装，也能由 agent 自己即时创建（`/dev/skills/manage` VFS 自反射）
- **元工具默认注册**：`ToolSearch` / `Skill` 不受 manifest 控制，所有 agent 都能用——前提是 `deferred_skills` 池非空
