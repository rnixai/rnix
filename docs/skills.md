# Skill 管理（多 scope + agentskills.io 互通）

本文档说明 Rnix 自 Epic 47（v0.10+）落地后的 skill 管理模型：四路径目录布局、CLI 子命令、shadow 冲突解决、lenient validation、trust check，以及与 [agentskills.io](https://agentskills.io/) 生态（vercel-labs/skills、Cursor、OpenCode 等）的互通。目标读者是 Rnix 用户（含从 monorepo 子目录或非 `.rnix/` 项目调用 `rnix skill` 的人）与希望做跨工具 skill 集成的开发者。本文档**只描述当前实现已有的行为**——若发现描述与命令实际输出不一致，请按"实现是事实"原则提 doc 修订 PR。

## 目录

- [目录布局与优先级](#目录布局与优先级)
- [CLI 命令](#cli-命令)
- [skill list 输出格式](#skill-list-输出格式)
- [冲突解决与 shadow warning](#冲突解决与-shadow-warning)
- [Lenient validation（SKILL.md 异常容忍）](#lenient-validationskillmd-异常容忍)
- [Trust check（项目级 skill 加载告警）](#trust-check项目级-skill-加载告警)
- [Ancestor traversal（monorepo 子目录支持，可选）](#ancestor-traversalmonorepo-子目录支持可选)
- [与 agentskills.io 生态互通](#与-agentskillsio-生态互通)
- [相关 ADR / 规范 / Investigation](#相关-adr--规范--investigation)
- [更新历史](#更新历史)

## 目录布局与优先级

Rnix 遵循 [agentskills.io 客户端实现指南](https://agentskills.io/client-implementation/adding-skills-support.md) 的"双 scope × 双 namespace 四路径"模型——project 与 user 两层 scope，每层内 rnix-native 与 cross-tool agents 两个 namespace。优先级与 [Architecture Decision 17](../_bmad-output/planning-artifacts/architecture/core-architectural-decisions.md#decision-17-agentskill-shadow-策略)（Agent/Skill Shadow 策略）一致；运行时不再扫描 `lib/skills/`（[Architecture Decision 18](../_bmad-output/planning-artifacts/architecture/core-architectural-decisions.md#decision-18-embedfs-嵌入策略)：embed.FS 内置 skill 由 `rnix init` 一次性解压到 `~/.config/rnix/skills/`）。

| Path | Scope | Namespace | 用途 | 优先级 | git 跟踪建议 | 触发 install 的 flag 组合 |
|------|-------|-----------|------|--------|--------------|--------------------------|
| `<projectDir>/.rnix/skills/` | project | native | 项目本地的 rnix 专属 skill | 1（最高） | 视项目策略 commit 或 .gitignore | `skill install <name>`（cwd 在 `.rnix/` 项目内，无 flag） |
| `<projectDir>/.agents/skills/` | project | agents | 项目本地的 agentskills.io 共享 skill | 2 | 推荐 commit（与其他 client 互通） | `skill install <name> --shared`（cwd 在 `.rnix/` 项目内） |
| `~/.config/rnix/skills/`（或 `$XDG_CONFIG_HOME/rnix/skills/`） | user | native | 用户全局 rnix 专属 skill | 3 | （用户家目录，不涉及 git） | `skill install <name> -g`；或无项目时无 flag |
| `~/.agents/skills/` | user | agents | 用户全局 agentskills.io 共享 skill | 4（最低） | （用户家目录，不涉及 git） | `skill install <name> -g --shared` |

**优先级规则**：

- **跨 scope**：`project > user`——agentskills.io 规范钉死，与 Decision 17 一致；project 内的同名 skill **完全替代** user 的同名 skill，不做字段合并。
- **同 scope 内**：`native > agents`——rnix-native 路径表达 "rnix-specific behavior"，从 Epic 8 起即为历史路径；agents namespace 由 47.1 引入作为 cross-tool 补充。
- **冲突替代**：同名 skill 冲突时 winning 一份**完全替代** shadowed 一份；不存在"两份字段合并"的语义。
- **dedupe 后只见 winning**：实际扫描的 `os.ReadDir` 顺序按上表 1→4，但返回结果按 name dedupe，每个 name 在 `skill list` 表格里只出现一次（winning 副本）；被 shadow 的副本走 [shadow warning 通道](#冲突解决与-shadow-warning)。

源码锚点：`internal/config/skillscope.go::SkillScope` / `SkillNamespace` / `ScopePath`（[Source: internal/config/skillscope.go:31-95](../internal/config/skillscope.go)）。

## CLI 命令

skill 子命令族包含四个：`install` / `update` / `list` / `search`。前三个走本地四路径，`search` 仅访问远程 registry。

### skill install

`rnix skill install <name> [name...]` 从社区 registry（默认 `https://registry.rnix.ai`）拉取 skill 并解压到 writeScope。writeScope 由 `(global, shared)` flag 组合决定：

| 触发条件 | 目标路径 | scope / namespace |
|---------|---------|-------------------|
| cwd 在 `.rnix/` 项目内 + 无 flag | `<projectDir>/.rnix/skills/<name>/` | project · native |
| cwd 不在 `.rnix/` 项目内 + 无 flag | `~/.config/rnix/skills/<name>/` | user · native |
| `-g` / `--global`（任意 cwd） | `~/.config/rnix/skills/<name>/` | user · native |
| cwd 在 `.rnix/` 项目内 + `--shared` | `<projectDir>/.agents/skills/<name>/` | project · agents |
| cwd 不在 `.rnix/` 项目内 + `--shared` | `~/.agents/skills/<name>/` | user · agents |
| `-g --shared`（任意 cwd） | `~/.agents/skills/<name>/` | user · agents |

- 两个 flag 正交：`-g` 改 scope（project↔user），`--shared` 改 namespace（native↔agents）。
- 多 args 场景：`rnix skill install foo bar baz` 三个 skill 写入**同一** writeScope（按上表规则统一选定一次）。
- Install 后用 strict `LoadMetadata` 验证（[skillpkg/installer.go:169](../skillpkg/installer.go)）——若 SKILL.md 触发了 lenient warning 类的条件（如 name 与目录不匹配），仍能 list 出来但 install 时会 rollback；详见 [deferred-work.md §Deferred from: code review of story-47-3](../_bmad-output/implementation-artifacts/deferred-work.md)。

示例：

```bash
rnix skill install code-analysis            # project/native（若在 .rnix/ 项目内）/ 否则 user/native
rnix skill install code-analysis -g         # 强制装到 user/native（~/.config/rnix/skills/）
rnix skill install code-analysis --shared   # 装到 agents namespace（.agents/skills/）
rnix skill install code-analysis --force    # 已存在时覆盖
rnix skill install code-analysis --json     # JSON 输出
```

### skill update

`rnix skill update [name...]` 检查并升级已安装的 community skill。47.2 起 update 不再静默跨 scope 迁移——

- `rnix skill update foo` 先在四路径中按 shadow 胜出原则找到 foo 当前所在 scope，update 后**写回同一 scope**。
- `rnix skill update`（无 args）遍历**所有** scope 的所有 community skill，各自原地 update。
- builtin 来源的 skill 不在 UpdateAll 范围内（社区 registry 没有它的 latest version）。

示例：

```bash
rnix skill update code-analysis              # 单个 skill（写回 origin scope）
rnix skill update code-analysis pr-reviewer  # 多个 skill
rnix skill update                            # 升级所有已安装的 community skill
rnix skill update code-analysis --json       # JSON 输出
```

### skill list

`rnix skill list` 列出四路径下所有 skill 的 dedupe 后视图。输出格式见 [skill list 输出格式](#skill-list-输出格式)；本节仅列 flag。

| Flag | 含义 |
|------|------|
| `-g` / `--global` | 仅显示 user scope（`~/.config/rnix/skills/` + `~/.agents/skills/`） |
| `-p` / `--project` | 仅显示 project scope（`<projectDir>/.rnix/skills/` + `<projectDir>/.agents/skills/`） |
| `--json` | JSON 输出（含 `diagnostics` 节点） |
| `--quiet` | 仅输出 skill 名（一行一个，便于 shell pipe） |

`-g` 与 `-p` 通过 Cobra 的 `MarkFlagsMutuallyExclusive` 互斥（[cmd/rnix/skill.go:92](../cmd/rnix/skill.go)），同时指定会得到 Cobra 标准错误。

示例：

```bash
rnix skill list           # 全部 scope
rnix skill list -g        # 仅 user scope
rnix skill list -p        # 仅 project scope
rnix skill list --json    # JSON（含 diagnostics）
rnix skill list --quiet   # 仅输出 skill 名
```

### skill search

`rnix skill search [keyword]` 走远程 community registry（默认 `https://registry.rnix.ai`）按 keyword 搜索。**不**扫描本地四路径，因此不受 Epic 47 改造影响。JSON 形状中**没有** `diagnostics` 节点（local 四路径未参与）。

示例：

```bash
rnix skill search code           # 搜索 keyword "code"
rnix skill search                # 浏览全部可用 skill
rnix skill search code --json    # JSON 输出
```

## skill list 输出格式

`rnix skill list` 表格输出包含 6 列（按出现顺序）：`NAME` / `VERSION` / `SOURCE` / `SCOPE` / `NAMESPACE` / `DESCRIPTION`。每行前缀 `[skill]` 与项目其他模块（`[ipc]` / `[kernel]` 等）保持一致，便于 grep 过滤。

| 列 | 取值 | 含义 |
|----|------|------|
| `NAME` | skill 目录名（= SKILL.md frontmatter `name`） | 引用 skill 时用的标识 |
| `VERSION` | semver 或空 | 来自 winning scope 的 `.registry.yaml`；未注册的 builtin/手动放置 skill 可为空 |
| `SOURCE` | `builtin` / `community` / 空 | `builtin` = rnix 内置；`community` = `skill install` 装入；空 = `.registry.yaml` 未记录 |
| `SCOPE` | `project` / `user` | 来自 winning `ScopePath.Scope` |
| `NAMESPACE` | `native` / `agents` | 来自 winning `ScopePath.Namespace` |
| `DESCRIPTION` | SKILL.md frontmatter `description` 字段 | 超长按 rune 截断到 40 字符（项目惯例，参考 [CLAUDE.md §CJK 字符处理检查](../CLAUDE.md)） |

### Shadowed skill 不出现在表格

被 shadow 的同名副本**不**出现在 list 表格里——每个 name 只显示 winning 副本。Shadow 事件不会被吞掉：每个 shadowed 副本对应一条 stderr warning（非 JSON 模式）或一个 `diagnostics.warnings[]` 数组条目（JSON 模式）。完整规则见 [冲突解决与 shadow warning](#冲突解决与-shadow-warning)。

### 空列表诊断

当四路径都不存在或都为空目录时，list 不会静默吞掉——会渲染空表头后追加诊断块：

```text
[skill] NAME                 VERSION    SOURCE      SCOPE     NAMESPACE  DESCRIPTION
[skill] No skills found. Scanned paths:
[skill]   - /home/decker/project/.rnix/skills (not-found)
[skill]   - /home/decker/project/.agents/skills (existed-but-empty)
[skill]   - /home/decker/.config/rnix/skills (existed-but-empty)
[skill]   - /home/decker/.agents/skills (not-found)
[skill] Tip: skill search <keyword> to discover more skills.
```

`pathStatus` 两个状态（[cmd/rnix/skill.go:250](../cmd/rnix/skill.go)）：

- **`not-found`**：`os.Stat` 失败、目录不存在或 path 不是目录。
- **`existed-but-empty`**：目录存在，但没有非 dot 前缀的子目录（不算 skill 的）。

> **已知 deferred**：`pathStatus` 把 `EACCES` / `ELOOP` 等非 `ENOENT` 的 stat 错误也归到 `not-found`，可能误导用户排查权限/循环 symlink 问题。详见 [deferred-work.md §Deferred from: code review of story-47-3](../_bmad-output/implementation-artifacts/deferred-work.md)。

`-g` 模式诊断块仅列 2 条 user scope 路径；`-p` 模式仅列 2 条 project scope 路径；无 flag 列全部 4 条。

### JSON 输出形状

`rnix skill list --json` 输出 `{ok, data}` 包装下的 `skills` 数组与 `diagnostics` 节点。**`diagnostics` 节点始终存在**（即便 4 通道全空，节点本身仍出现为 `{}`——这是 47.3 AC10 的明确决策，与 install/update 的 omitempty 形状不同）：

```json
{
  "ok": true,
  "data": {
    "skills": [
      {
        "name": "code-analysis",
        "version": "1.0.0",
        "path": "/home/decker/project/.rnix/skills/code-analysis",
        "description": "Analyze code structure and dependencies",
        "source": "community",
        "scope": "project",
        "namespace": "native",
        "shadowed": false
      }
    ],
    "diagnostics": {
      "warnings": [
        {
          "skill_name": "code-analysis",
          "winning_path": "/home/decker/project/.rnix/skills/code-analysis",
          "winning_scope": "project",
          "winning_ns": "native",
          "shadowed_path": "/home/decker/.config/rnix/skills/code-analysis",
          "shadowed_scope": "user",
          "shadowed_ns": "native"
        }
      ]
    }
  }
}
```

**Channel 内字段的 wire 行为**：`LoadDiagnostics` 4 个 channel 数组（`warnings` / `skipped` / `lenient` / `trust`）各自带 `omitempty`——空数组对应的 key 会被 `encoding/json` 整个省略；上例只展示了 `warnings` 非空的情况，其它 3 个 channel 各自的字段示例见 [冲突解决与 shadow warning](#冲突解决与-shadow-warning) / [Lenient validation](#lenient-validationskillmd-异常容忍) / [Trust check](#trust-check项目级-skill-加载告警)。**4 个 channel 全空时**，`diagnostics` 自身仍出现为 `{}`（list wire 契约），而 install / update 路径会把整个 `diagnostics` 字段一并省略（指针 + omitempty）。详见 [skillpkg/types.go:131-172](../skillpkg/types.go) 的 wire 契约注释。

## 冲突解决与 shadow warning

### 跨 scope 优先级（project > user）

agentskills.io 规范明文："project-level skills override user-level skills"。Rnix 实现与 [Decision 17](../_bmad-output/planning-artifacts/architecture/core-architectural-decisions.md#decision-17-agentskill-shadow-策略) 一致——project 内的同名 skill 完全替代 user 同名 skill，不做字段级合并。

### 同 scope 内优先级（native > agents）

同 scope 内 `.rnix/skills/`（rnix-native namespace）优先于 `.agents/skills/`（cross-tool agents namespace）。这是 Rnix 的选择：rnix-native 路径在 Epic 8 起即为历史路径，表达 "rnix-specific behavior"；agents namespace 由 47.1 引入作为补充，专门用于跨工具 skill 共享。当一份 skill 同时存在两个 namespace 时，用户通常希望 rnix-tuned 的那一份生效。

### Shadow warning 输出格式

非 JSON 模式 stderr（[cmd/rnix/skill.go:276](../cmd/rnix/skill.go)）：

```text
[skill] warning: shadowed skill "code-analysis": winner=/home/decker/project/.rnix/skills/code-analysis (project/native); shadowed=/home/decker/.config/rnix/skills/code-analysis (user/native)
```

JSON 模式下 `diagnostics.warnings[]` 数组（字段名与 `ShadowWarning` struct 对齐，[Source: skillpkg/types.go:103-112](../skillpkg/types.go)）：

```json
{
  "skill_name": "code-analysis",
  "winning_path": "/home/decker/project/.rnix/skills/code-analysis",
  "winning_scope": "project",
  "winning_ns": "native",
  "shadowed_path": "/home/decker/.config/rnix/skills/code-analysis",
  "shadowed_scope": "user",
  "shadowed_ns": "native"
}
```

### 多重 shadow

若同一 name 同时存在于 3 个甚至 4 个 scope，每条 shadow 事件**单独一行 warning**（winning vs 每个 shadowed 单独一条），不合并。理由：用户需要知道每个被 shadow 的具体来源——若合并成 "winner=A, shadowed=B,C,D" 单条，用户排查哪一份在被 shadow 时还要二次解析路径列表。

源码锚点：`skillpkg/installer.go::ListAll` 的 shadow 累积逻辑（[Source: skillpkg/installer.go:412-490](../skillpkg/installer.go)）。

## Lenient validation（SKILL.md 异常容忍）

Rnix 加载 SKILL.md 遵循 [agentskills.io 规范的 lenient validation 原则](https://agentskills.io/client-implementation/adding-skills-support.md)——规范强调"客户端应对部分异常容忍以提高生态兼容性"。47.2 落地的行为分两档（warn-but-load 与 skip + log），实际触发条件如下：

| 异常类型 | 行为 | 原因 | Diagnostic 通道 |
|---------|------|------|----------------|
| frontmatter `name` 不匹配父目录名 | **warn but load** | name 不一致是 cosmetic 问题（如 rename 目录后忘改 frontmatter），skill 内容仍可用 | `diagnostics.lenient[]` |
| frontmatter `name` 超 64 字符 | **warn but load**（截断后显示） | 长 name 是规范限制，但 skill 内容仍可用 | `diagnostics.lenient[]` |
| frontmatter `name` 缺失或空 | **skip + log** | 无法获取 skill 标识 | `diagnostics.skipped[]` |
| frontmatter `description` 缺失或空 | **skip + log** | description 是 LLM 选择 skill 的关键信号，无 description 无法被合理匹配 | `diagnostics.skipped[]` |
| YAML frontmatter 完全无法解析 | **skip + log** | 无法获取任何元信息 | `diagnostics.skipped[]` |

**warn-but-load vs skip 的关键区别**：

- **warn-but-load**：skill 仍出现在 `skill list` 表格 + 仍可被 agent 加载（system prompt 注入），仅在 stderr/JSON 留 advisory。
- **skip**：skill 不出现在表格 + 不能被 agent 引用；stderr/JSON 中告知用户为什么被跳过。

stderr 渲染格式（[cmd/rnix/skill.go:279-283](../cmd/rnix/skill.go)）：

```text
[skill] warning: skipped skill at /home/decker/project/.rnix/skills/broken: missing description
[skill] warning: skipped skill at /home/decker/project/.rnix/skills/badyaml: yaml parse error: line 3: mapping values are not allowed in this context
[skill] warning: /home/decker/project/.rnix/skills/renamed: name: name mismatch parent dir (manifest.Name="old-name", parentDir="renamed")
```

> **已知 deferred**（[deferred-work.md §Deferred from: code review of 47-2](../_bmad-output/implementation-artifacts/deferred-work.md)）：
>
> - `parseSKILLMD` 的结构错误（如缺少 `---` 分隔符）目前不包装为 `LenientSkipError`，会以原始 error 形态吐出而不进入 `diagnostics.skipped`；未来增强可统一诊断通道。
> - Install 后用 strict `LoadMetadata` 验证（[installer.go:169](../skillpkg/installer.go)），与 ListAll 使用的 `LoadMetadataWithDiag`（lenient）行为不一致；用户会观察到"list 能见到但 install 装不上"的现象——尤其当 SKILL.md 仅触发 warn-but-load 时仍 install rollback。

源码锚点：`skills/loader.go::loadAndParse`（[skills/loader.go:86-168](../skills/loader.go)） / `skillpkg/installer.go::ListAll`（[skillpkg/installer.go:412](../skillpkg/installer.go)）。

## Trust check（项目级 skill 加载告警）

trust check 是 47.4 MVP 落地的**warn-only 项目级安全提醒**——项目级 skill（`.rnix/skills/` + `.agents/skills/`）随仓库源码而来，恶意 repo 可以通过塞 SKILL.md 悄悄改写 agent 的 system prompt。agentskills.io 规范推荐"在加载 project skill 前检查用户是否标记该目录为 trusted"；Rnix MVP 实现为"未 trust 时 warn 但仍加载"。

### 当前行为

| 场景 | 行为 |
|------|------|
| `<projectDir>/.rnix/state/trusted` 存在（regular file） | 不输出 trust warning，正常加载 project scope skill |
| `<projectDir>/.rnix/state/trusted` 不存在 | 输出 trust warning 到 stderr（list / install / update 三命令皆触发），**仍加载** project scope skill（warn-only，不阻塞） |
| 当前 cwd 不在任何 `.rnix/` 项目内（仅 user scope 触发） | **不**做 trust check（user scope 始终视为 trusted） |

### trust marker 文件路径与机制

trust marker 是位于 `<projectDir>/.rnix/state/trusted` 的**普通文件**：

- 路径由常量 `skillpkg.TrustMarkerRelPath = ".rnix/state/trusted"` 给出（[skillpkg/trust.go:35](../skillpkg/trust.go)），代码中**只检查存在性**，从不读内容。文件可以是空文件、任意内容皆可。
- **symlink 不被接受**——通过 `os.Lstat` + `mode.IsRegular()` 拒绝所有非常规文件（47.4 DN1 安全决议，[skillpkg/trust.go:182-190](../skillpkg/trust.go)）。理由：恶意 repo 可能 commit `.rnix/state/trusted -> /etc/hostname` 之类的 symlink，使任何机器上 trust check 都被绕过。

### Trust warning 输出格式

stderr 格式（[cmd/rnix/skill.go:293-297](../cmd/rnix/skill.go)）：

```text
[skill] warning: untrusted project "/home/decker/EchoMatrix": 2 skill root(s) will load — untrusted repo can inject agent instructions. Policy: warn-only (not blocking). To dismiss this warning, run: touch /home/decker/EchoMatrix/.rnix/state/trusted (a future 'rnix trust <dir>' command is planned)
```

注意：

- `<projectDir>` 用 `%q` 包裹——含空格 / 控制字符的路径会被安全引用，避免单行 stderr 契约被打断。
- 默认使用 em-dash `—`（U+2014）；`RNIX_ASCII=1` 模式下 fallback 为 ASCII `-`（47.4 P3 决议，CI 容器与远程终端常见此模式）。

JSON 模式下 trust warning 出现在 `diagnostics.trust[]`（[skillpkg/types.go:184-190](../skillpkg/types.go)）：

```json
"trust": [
  {
    "project_dir": "/home/decker/EchoMatrix",
    "skills_root_paths": [
      "/home/decker/EchoMatrix/.rnix/skills",
      "/home/decker/EchoMatrix/.agents/skills"
    ],
    "reason": "untrusted repo can inject agent instructions",
    "policy": "warn-only (not blocking)",
    "recommendation": "To dismiss this warning, run: touch /home/decker/EchoMatrix/.rnix/state/trusted (a future 'rnix trust <dir>' command is planned)"
  }
]
```

形状对比：

- `rnix skill list --json`：`diagnostics` 始终存在，trust 空时为 `"trust": []`（或省略——见 `LoadDiagnostics` 的 `omitempty` JSON tag）。
- `rnix skill install --json` / `rnix skill update --json`：`diagnostics` 是指针 + `omitempty`，trust 空且其他 3 通道也空时整个 `diagnostics` 字段**不出现**。

### Dismiss 操作

两种方式：

- **ad-hoc**：`touch <projectDir>/.rnix/state/trusted`——开发者临时压制本地 warning。
- **commit 进 git**：把 `.rnix/state/trusted` 加入 git（repo 维护者声明"该 repo 是 trusted"，clone 该 repo 的用户不再看到 warning）。

git 跟踪策略：建议根据团队约定选择——若 repo 维护者明确"代码 review 把关够严格、project skill 是可信内容"，可以 commit marker 文件让所有 cloner 静默；若希望每个开发者**主动确认 trust**，把 `.rnix/state/trusted` 加入 `.gitignore` 让每个 clone 都重新触发 warning（迫使开发者用 `touch` 主动 opt-in）。

### 未来 `rnix trust <dir>` 命令规划

- **当前不实现** `rnix trust` / `rnix untrust` CLI 子命令。实测 `rnix trust` 会得到 `Error: unknown command "trust"`。
- 未来实现的 `rnix trust <dir>` 命令**仍然写同一个** `<dir>/.rnix/state/trusted` 文件——**向前兼容**承诺：今天 `touch` 的 marker 文件未来 `rnix trust` 上线后不需要迁移。
- 推迟实现的原因：完整 trust 子系统含 trust 列表、撤销 UX、prompt 交互、跨平台路径等独立 Epic 级工作量。

### agents-only 项目限制

当前 trust marker 路径 `.rnix/state/trusted` 锚定在 rnix namespace；纯 agents-only 项目（只有 `.agents/skills` 没有 `.rnix/`）的用户需手工创 `.rnix/state/` 目录树后再 `touch trusted` 才能 dismiss。未来 `rnix trust` 命令上线时一并统一 namespace；详见 [deferred-work.md §Deferred from: code review of 47-4](../_bmad-output/implementation-artifacts/deferred-work.md)。

### TOCTOU 说明

`runSkillInstall` / `runSkillUpdate` 会调用一次 `CheckProjectTrust` 用于 stderr 输出；`Installer.ListAll` 内部再调用一次用于填充 `diag.Trust`。两次调用相距毫秒级，外部写入 marker 翻转 trusted 状态的窗口可忽略。需要严格一致性的消费者应仅调用 `CheckProjectTrust` 一次并把结果同时传给所有 sink（47.4 DN2 决议，[skillpkg/trust.go:64-70](../skillpkg/trust.go)）。

源码锚点：`skillpkg/trust.go::CheckProjectTrust`（[skillpkg/trust.go:74](../skillpkg/trust.go)） / `TrustMarkerRelPath`（[skillpkg/trust.go:35](../skillpkg/trust.go)） / `skillpkg/types.go::TrustWarning`（[skillpkg/types.go:184](../skillpkg/types.go)） / `cmd/rnix/skill.go::emitTrustPrecheck`（[cmd/rnix/skill.go:305](../cmd/rnix/skill.go)） / `cmd/rnix/skill.go::renderDiagnosticsToStderr`（[cmd/rnix/skill.go:274](../cmd/rnix/skill.go)）。

## Ancestor traversal（monorepo 子目录支持，可选）

默认情况下 `rnix skill list/install/update` 仅扫描 cwd 下的 `.rnix/skills/` 与 `.agents/skills/`。若用户在 monorepo 的子目录运行（如 `~/monorepo/packages/my-pkg/`，而 skill 在 `~/monorepo/.rnix/skills/`），默认配置**找不到** monorepo 根的 skill——这是 agentskills.io 规范钦定的 v1 默认行为（ancestor traversal 列为 optional）。

### 启用方式

目前仅通过 Go API 启用（程序化集成场景）：

```go
import "github.com/rnixai/rnix/internal/config"

scopes := config.ResolveSkillScopes(cwd, config.WithAncestorTraversal(true))
```

**当前没有 CLI flag 暴露** `WithAncestorTraversal`——Epic 47 范围内只做接口预留。未来若需要可加 `rnix skill list --ancestor-traversal` 之类 flag；当前推迟。

### 行为详细描述

启用 `WithAncestorTraversal(true)` 后：

- 从 cwd 开始向上遍历父目录，每层检查 `.rnix/skills/` 与 `.agents/skills/`；找到即追加到 project scope 结果（依然遵守 native > agents 优先级）。
- 遍历上界（任一命中即停）：到 **git repo root**（含 `.git/` 目录或文件，覆盖 worktree / submodule）、**HOME 边界**（`$HOME`，防止上溯到家目录与 user scope 重复扫描）、**最多 `defaultMaxAncestorDepth = 6` 层**。
- 黑名单：`.git/` 与 `node_modules/` 在 walker 内永远跳过（不向其内进入，也不在其内扫 skill 路径）。
- dirCount 上界：单次解析最多 `defaultMaxDirs = 2000` 个目录（含 ancestor traversal）；超限时输出 `[skillscope] ResolveSkillScopes truncated: ...` warning 到 stderr 并按已扫描结果返回部分数据。

### Monorepo 场景示例

目录结构：

```text
~/monorepo/
  .rnix/skills/
    code-analysis/SKILL.md
  .git/
  packages/
    my-pkg/
      <user 在此运行 rnix skill list>
```

- **默认行为**：`rnix skill list` 在 `my-pkg/` 看不到 code-analysis（仅扫 `my-pkg/.rnix/skills` 与 `my-pkg/.agents/skills`，皆不存在）。
- **启用 `WithAncestorTraversal(true)`** 后：walker 从 `my-pkg/` 向上爬到 `~/monorepo/`，检查 `~/monorepo/.rnix/skills/`（找到 code-analysis）；遇到 `~/monorepo/.git/` 触发 git root 边界后停止。

### Functional options 总览

| Option | 默认值 | 用途 |
|--------|--------|------|
| `WithAncestorTraversal(bool)` | `false` | 启用上溯扫描父目录至 git root |
| `WithMaxAncestorDepth(int)` | `6` | 限制最多上溯层数；传入 `0` 时**保留默认**（防御性 API 行为，禁用 traversal 应使用 `WithAncestorTraversal(false)`） |
| `WithMaxDirs(int)` | `2000` | 限制单次 stat 总数；超限触发 truncation warning + 部分返回 |
| `WithSkipDirs(names ...string)` | `[".git", "node_modules"]` | **追加**到默认黑名单（不替换） |
| `WithWarningWriter(io.Writer)` | `os.Stderr` | 重定向 walker warning（如 truncation）；nil 被忽略 |

> **已知 deferred**（[deferred-work.md §Deferred from: code review of 47-1](../_bmad-output/implementation-artifacts/deferred-work.md)）：
>
> - `WithMaxAncestorDepth(0)` 被静默忽略保留默认 6（防御性 API 行为）——若调用方真的想禁用 ancestor traversal，应使用 `WithAncestorTraversal(false)` 而非 depth=0。
> - HOME 边界依赖 `$HOME` env var——Linux 容器和 CI 一般可用；Windows / 无 HOME 环境时该边界失效（其他三条仍然生效）。

源码锚点：`internal/config/skillscope.go::ResolveSkillScopes` / `walkAncestors`（[internal/config/skillscope.go:188-392](../internal/config/skillscope.go)）。

## 与 agentskills.io 生态互通

`.agents/skills/` 与 `~/.agents/skills/` 是 [agentskills.io specification](https://agentskills.io/specification.md) 定义的"跨客户端共享 namespace"——其他兼容客户端（vercel-labs/skills、Cursor、OpenCode 等）装入这两个路径的 skill，Rnix 通过 47.1 的 `ResolveSkillScopes` 直接消费，**零额外配置**。

### 演练 1：vercel-labs/skills CLI 装入后 rnix 立即可见

```bash
# 用户在任意目录运行 vercel-labs/skills（npm 包）
npx skills add code-analysis -g
# → 安装到 ~/.agents/skills/code-analysis/

# rnix 立即可见（无需重启 daemon、无需 rnix init）
rnix skill list
# [skill] NAME             VERSION    SOURCE      SCOPE     NAMESPACE  DESCRIPTION
# [skill] code-analysis    1.0.0                  user      agents     Analyze code structure and dependencies
```

`rnix skill list` 每次调用都重新 `ResolveSkillScopes`（无缓存，[cmd/rnix/skill.go:720](../cmd/rnix/skill.go)），因此其他 client 实时安装的 skill 下一次 `rnix skill list` 就能看到。`SOURCE` 列为空表示该 skill 没有进入 rnix 的 `.registry.yaml`（vercel-labs 不维护 rnix 的 local registry，属预期行为）。

### 演练 2：项目内通过 `.agents/skills/` 与团队 / Cursor / OpenCode 共享

```bash
cd my-project
ls -la .agents/skills/        # 已被其他工具或团队成员 commit 进 repo
# drwxr-xr-x  ...  code-analysis/
# drwxr-xr-x  ...  pr-reviewer/

rnix skill list
# [skill] NAME            VERSION    SOURCE     SCOPE     NAMESPACE  DESCRIPTION
# [skill] code-analysis                         project   agents     Analyze code structure
# [skill] pr-reviewer                           project   agents     Review pull requests
```

（`SOURCE` 列为空表示这两个 skill 没有进入 rnix 的 `.registry.yaml`——团队 commit / 其他客户端写入的 skill 属预期行为。）

> 若团队希望共享 skill 但又不想锁定 rnix-only namespace，把 skill 装入 `.agents/skills/` 并 commit 即可——任何 agentskills.io 兼容工具都能读到。注意首次在新 clone 上跑 `rnix skill list` 会触发 [trust warning](#trust-check项目级-skill-加载告警)，需要 `touch .rnix/state/trusted` dismiss。

### agentskills.io 已知客户端清单

| 客户端 | 类型 | 主页 / 仓库 |
|--------|------|------------|
| Anthropic Skills | 协议起源（Claude Code 等） | <https://docs.anthropic.com/> |
| vercel-labs/skills | npm CLI | <https://github.com/vercel-labs/skills> |
| Cursor | IDE | <https://cursor.com/> |
| OpenCode | OSS Agent CLI | <https://github.com/opencode-ai> |
| Rnix | Agent OS | 本仓库 |

清单截止 Epic 47 落地时点（2026-05-27），后续新增由社区维护；准确清单见 [agentskills.io](https://agentskills.io/) 官方。

### 跨工具共享的 skill 编写建议

写入 `.agents/skills/` 的 SKILL.md 应只使用 [agentskills.io specification](https://agentskills.io/specification.md) 定义的标准字段（`name`、`description`、`allowed-tools` 等），避免 rnix-only 扩展字段。

**`allowed-tools` 的值是 Layer 1 资源路径，不是工具语义名。** frontmatter 的 `allowed-tools` 列出的是设备路径（`/dev/fs`、`/dev/shell`、`/dev/intent`、`/mnt/mcp/*` 等），用于能力声明与权限执行（spawn 设备交集 / 运行时设备过滤）。Rnix **没有** `Read → /dev/fs` 这样的映射层——[`skills/manager.go`](../skills/manager.go) 的 `validateFrontmatter` 强制每个值以 `/dev/` 或 `/mnt/mcp/` 前缀，写成 `Read` 会被直接拒绝（[`skills/types.go`](../skills/types.go) 亦将其注释为 "allowed tool **device paths**"）。这一分层由 [Architecture Decision 44](../_bmad-output/planning-artifacts/architecture/core-architectural-decisions.md#decision-44-工具命名双层原则--capability-资源路径--llm-语义名) 固化：**Layer 1** = 资源路径（`allowed-tools` 属此层，用于 enforcement）；**Layer 2** = `ToolDef.Name`（LLM 经 function-calling 实际看到的呈现名，如 `Read` / `Bash`）。

**跨平台可移植性的真正落点**不是改 `allowed-tools` 的值（那会破坏 enforcement 闭环，且 `Read` 本身也只是某一平台的工具词汇，并无行业统一标准），而是 ① 让 **skill body 工具中立化**（见下节《Skill body 工具引用规范》）+ ② 把 skill 放进 `.agents/skills/` 共享路径并 commit。如此一份 SKILL.md 在 Rnix / Cursor / OpenCode 中均可被消费。

### Skill body 工具引用规范

SKILL.md 的 **body**（frontmatter 之下的 markdown 正文）会被注入 agent 的 System Prompt，是 LLM 直接阅读的文本。因此 body 引用工具时必须用 LLM **实际看到的名字**，而非内部实现路径。依据 [Architecture Decision 44](../_bmad-output/planning-artifacts/architecture/core-architectural-decisions.md#decision-44-工具命名双层原则--capability-资源路径--llm-语义名) 的双层原则——**Layer 1**（资源路径 `/dev/fs` 等）只用于权限执行，LLM 经 function-calling 看到的是 **Layer 2** 的 `ToolDef.Name` + 结构化 Parameters——body 引用工具应遵循以下五条原则：

1. **通用工具用通用语义名**：跨平台通用的文件 / 命令工具直接用其语义名（`Read` / `Write` / `Edit` / `Bash` / `Grep` / `Glob`）。这些恰好就是 Rnix 对应的 `ToolDef.Name`（`/dev/fs` 暴露 `Read` / `Write` / `Edit` / `Grep` / `Glob`，`/dev/shell` 暴露 `Bash`），与主力模型的训练分布锚点对齐（Decision 44 R1）。
2. **body 绝不出现设备路径**：正文中**禁止**出现 `/dev/*`、`/mnt/mcp/*` 等 Layer 1 资源路径。这些路径 LLM 不可见，写进 body 只会迫使模型做"路径 → 工具名"的心智翻译，抵消 Layer 2 的训练锚点优势。
3. **Rnix 独有能力用工具中立的方法论描述**：intent / memory / skill 等 Rnix 独有能力，body **不应硬绑** `intent_decompose`、`memory_commit` 等独有工具名，而应描述其**意图与方法论**（如"将高层意图分解为子任务 DAG，声明依赖关系，经确认后执行"）。理由：① skill 必须可移植（遵循 agentskills.io 即无"Rnix 专用"豁免），硬绑独有名会把 body 锁死到 Rnix；② LLM 已从 driver 的 `ToolDef` 看到这些工具及其 Parameters，Rnix agent 自然会落到对应工具，body 的增值是"怎么做"的方法论而非工具指路。
4. **不在 body 重述 `ToolDef.Parameters`**：工具的参数 schema 由 driver 自动暴露给 LLM（function-calling 的结构化 Parameters），body 再逐字段重述（如 `{intent, model, provider}`）只是冗余，且易与真实 schema 漂移。
5. **body 主体是领域知识 + 流程判断**：工具名仅在需要点名某项能力时出现；body 的核心价值是领域方法论、判断标准、工作流程，而非把每一步翻译成工具调用。

> 一句话：**`allowed-tools`（frontmatter）= Layer 1 设备路径**（enforcement 用）；**body 引用工具 = Layer 2 `ToolDef.Name`**（通用名）**或工具中立方法论**（Rnix 独有能力）。两者分属不同层，互不混用。

## 相关 ADR / 规范 / Investigation

### 内部 ADR

- [Architecture Decision 7](../_bmad-output/planning-artifacts/architecture/core-architectural-decisions.md#decision-7-agent-抽象层与-skill-标准化) — Agent 抽象层与 Skill 标准化（基于 agentskills.io 行业标准设计 Agent 层）
- [Architecture Decision 17](../_bmad-output/planning-artifacts/architecture/core-architectural-decisions.md#decision-17-agentskill-shadow-策略) — Agent/Skill Shadow 策略（project 完全替代 user）
- [Architecture Decision 18](../_bmad-output/planning-artifacts/architecture/core-architectural-decisions.md#decision-18-embedfs-嵌入策略) — embed.FS 嵌入策略（`lib/` 运行时不再作为查找路径）
- [Architecture Decision 44](../_bmad-output/planning-artifacts/architecture/core-architectural-decisions.md#decision-44-工具命名双层原则--capability-资源路径--llm-语义名) — 工具命名双层原则（Layer 1 资源路径用于 enforcement / Layer 2 `ToolDef.Name` 用于 LLM 呈现；skill body 工具引用规范的依据）

### 外部规范

- [agentskills.io / specification](https://agentskills.io/specification.md) — Agent Skills 开放标准（SKILL.md frontmatter 字段定义）
- [agentskills.io / Adding Skills Support](https://agentskills.io/client-implementation/adding-skills-support.md) — 客户端实现指南（四路径 + project>user + lenient + trust check 全文）

### Investigation / Epic

- [Investigation: skill-list-empty-investigation.md](../_bmad-output/implementation-artifacts/investigations/skill-list-empty-investigation.md) — Epic 47 起源（EchoMatrix 空列表 + 17 项 backlog）
- [Epic 47: Skill 管理多 scope 改造](../_bmad-output/planning-artifacts/epics/epic-47-skill多scope改造-skill-multi-scope-refactor.md)
- [Epic 47 deferred-work.md](../_bmad-output/implementation-artifacts/deferred-work.md) — Epic 47 共 23 项 deferred（按 Story 分组）

### Story 实现（按顺序）

- [Story 47.1: ResolveSkillScopes 四路径解析器](../_bmad-output/implementation-artifacts/47-1-resolveskillscopes-four-path-resolver.md)
- [Story 47.2: Installer 多 scope 改造（Shadow + Lenient）](../_bmad-output/implementation-artifacts/47-2-installer-multi-scope-shadow-warning-lenient.md)
- [Story 47.3: CLI 子命令多 scope 适配](../_bmad-output/implementation-artifacts/47-3-cli-subcommand-multi-scope-flag-render-diagnostic.md)
- [Story 47.4: Trust check MVP（warn-only）](../_bmad-output/implementation-artifacts/47-4-trust-check-mvp-warn-only.md)
- [Story 47.5: 文档 docs/skills.md](../_bmad-output/implementation-artifacts/47-5-docs-skills-md.md)（本文档）

## 更新历史

| 日期 | 内容 |
|------|------|
| 2026-05-27 | Story 47.5 落地（commit `<commit-hash>`）：新建 docs/skills.md 覆盖 47.1-47.4 全部对外契约 |
