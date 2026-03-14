# 配置系统重构 - 产品简报

**日期**: 2026-03-14
**来源**: Party Mode 多智能体讨论
**参与者**: John (PM), Winston (Architect), Amelia (Dev), Decker (用户)

---

## 背景与动机

Rnix 是一个**全局安装的 CLI 工具**（类似 git、docker），用户安装后可在任意项目目录中直接运行。当前配置系统存在以下问题：

1. **不一致性** — 仅 `rnix-providers.yaml` 有全局回退（CWD → XDG → 内置默认），其他配置文件（init、compose、mcp）仅支持 CWD
2. **零体验问题** — 新用户安装后在任意目录执行命令，无法找到配置文件
3. **命名空间污染** — 多个 `rnix-*.yaml` 散落在项目根目录
4. **数据与配置混杂** — `.rnix/` 目录已用于运行时数据（records、traces、reputation、immune），但配置文件不在其中
5. **内置 agent/skill 不可自定义** — `lib/agents/` 和 `lib/skills/` 硬编码在代码中

---

## 核心设计决策

### 1. Daemon 模型：全局单例 + 项目上下文注入（方案 A）

- 全局唯一 daemon 进程，持有内核和进程表
- CLI 端负责发现 `.rnix/` 目录，通过 IPC 传入 `project_dir` 路径
- daemon 端根据 `project_dir` 读取并合并项目配置
- 同一 daemon 可同时服务不同项目的进程

### 2. 双层配置目录

| 层级 | 路径 | 用途 |
|------|------|------|
| 全局（用户级） | `~/.config/rnix/`（遵循 XDG_CONFIG_HOME 标准） | API key、默认偏好、全局 agent/skill |
| 项目级 | `.rnix/`（向上遍历查找，类似 `.git/`） | 项目特定配置、编排、自定义 agent/skill |

### 3. 目录结构

```
~/.config/rnix/                    .rnix/
├── providers.yaml                 ├── providers.yaml
├── config.yaml                    ├── config.yaml
├── mcp.yaml                       ├── mcp.yaml
├── agents/                        ├── agents/
│   ├── coder/                     │   └── coder/  ← 同名遮蔽全局
│   ├── planner/                   ├── skills/
│   └── ...                        ├── init.yaml      ← 仅项目级
└── skills/                        ├── compose.yaml    ← 仅项目级
    ├── code-review/               └── data/
    └── ...                            ├── records/
                                       ├── traces/
                                       ├── reputation/
                                       └── immune/
```

### 4. 合并策略

| 配置类型 | 合并策略 | 说明 |
|---------|---------|------|
| `providers.yaml` | Deep merge，项目覆盖全局 | API key 全局配一次，项目可加/改 provider |
| `config.yaml` | Deep merge，项目覆盖全局 | 运行时偏好设置 |
| `mcp.yaml` | Deep merge，项目覆盖全局 | MCP 服务器配置 |
| `agents/` | Shadow（同名遮蔽，不合并） | 项目级优先，按名称完全遮蔽全局级 |
| `skills/` | Shadow（同名遮蔽，不合并） | 项目级优先，按名称完全遮蔽全局级 |
| `init.yaml` | 仅项目级 | 无全局版本 |
| `compose.yaml` | 仅项目级 | 无全局版本 |

### 5. 文件命名

进入 `.rnix/` 或 `~/.config/rnix/` 目录后，去掉 `rnix-` 前缀：
- `rnix-providers.yaml` → `providers.yaml`
- `rnix-init.yaml` → `init.yaml`
- `rnix-compose.yaml` → `compose.yaml`

### 6. `rnix init` 命令

- **单一入口**，不区分"全局 init"和"项目 init"
- 执行时**自动判断**全局配置是否已存在，未配置则先初始化全局
- 全局初始化流程：
  - 创建 `~/.config/rnix/` 目录
  - 引导用户填写 `providers.yaml`（API key 等）
  - 生成默认 `config.yaml`
  - 从内置模板复制 agents 和 skills 到 `~/.config/rnix/agents/` 和 `~/.config/rnix/skills/`
- 项目初始化流程：
  - 创建 `.rnix/` 目录及子目录结构
  - 生成空的 `config.yaml`
  - 创建空的 `agents/` 和 `skills/` 目录

### 7. Agent/Skill 查找顺序

```
查找 agent "coder":
  1. .rnix/agents/coder/  ← 项目级，优先
  2. ~/.config/rnix/agents/coder/  ← 全局级，兜底

规则：同名不合并，项目级完全遮蔽全局级（Shadow 语义）
```

### 8. 内置 Agent/Skill 迁移

- 当前 `lib/agents/` 和 `lib/skills/` 不再作为运行时查找路径
- 变为**安装模板**，通过 `embed.FS` 嵌入二进制
- `rnix init` 全局初始化时复制到 `~/.config/rnix/agents/` 和 `~/.config/rnix/skills/`
- 用户获得独立副本，可自由修改

### 9. 向后兼容

- 检测到根目录旧文件（如 `rnix-providers.yaml`）→ 输出 deprecation warning
- 提供 `rnix migrate` 命令自动迁移旧配置到新结构
- 旧文件仍可识别，但优先使用新路径

### 10. IPC 协议扩展

spawn 请求 payload 增加 `project_dir` 字段：
```json
{
  "method": "spawn",
  "payload": {
    "agent": "coder",
    "project_dir": "/home/user/myproject"
  }
}
```
daemon 端根据 `project_dir` 自行读取 `.rnix/` 目录配置。

### 11. ProjectDir() 查找逻辑

- 从 CWD 向上遍历目录树查找 `.rnix/` 目录（类似 git 查找 `.git/`）
- 边界条件：到 `$HOME` 或文件系统根停止，防止无限遍历

---

## 配置加载时序

```
Daemon 启动（全局配置）          Spawn 请求（项目配置注入）
─────────────────────           ──────────────────────────
1. 加载 ~/.config/rnix/          1. CLI 从 CWD 向上找 .rnix/
   ├── providers.yaml            2. 合并项目级 providers → 全局
   ├── config.yaml               3. 加载 .rnix/init.yaml（如有）
   └── mcp.yaml                  4. 加载 .rnix/agents/ + skills/
2. 注册全局 LLM 驱动              5. 项目级配置作为进程上下文传入
3. 启动 IPC 监听                  6. 进程生命周期内绑定该配置快照
```

---

## 业界参考

| 工具 | 全局 | 项目 | 合并策略 |
|------|------|------|---------|
| Git | `~/.gitconfig` | `.git/config` | 项目覆盖全局 |
| npm | `~/.npmrc` | `.npmrc` | 逐级合并 |
| Docker | `~/.docker/config.json` | - | 仅全局 |
| Claude Code | `~/.claude/` | `.claude/` | 项目覆盖全局 |
| **Rnix（本方案）** | `~/.config/rnix/` | `.rnix/` | deep merge / shadow |

---

## 实现要点

1. 新建 `internal/config/` 包，提供统一路径解析
2. 使用 `embed.FS` 嵌入内置 agent/skill 模板
3. IPC 协议扩展 `project_dir` 字段
4. 改动涉及 5-6 个模块的路径解析逻辑
5. 运行时数据从 `.rnix/` 根目录迁移到 `.rnix/data/` 子目录

---

## 待 PRD 进一步细化

- `rnix migrate` 命令的具体迁移策略和边界处理
- `rnix upgrade` 时如何处理用户已修改的全局 agent/skill
- `config.yaml` 包含哪些具体配置项
- 错误处理：全局配置损坏或缺失时的降级策略
- 多用户共享环境下的权限考虑
