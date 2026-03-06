# 教程 1：编写第一个 Skill

本教程带你从零创建一个 Rnix Skill 和引用它的 Agent，然后执行它观察完整的运行流程。

---

## 前置条件

- Rnix 已安装并可运行（参考 [快速上手指南](../quick-start.md)）
- Claude Code CLI 已安装且 API 密钥已配置
- 对 Rnix 的进程、VFS、Skill 概念有基本了解（参考 [核心概念文档](../concepts.md)）

---

## 你将学到什么

1. Skill 的文件结构和 `SKILL.md` 编写规范
2. Agent 如何引用 Skill 获得能力
3. 如何运行 Agent 并观察 Skill 的执行过程

---

## 步骤一：创建 SKILL.md

Skill 是 Rnix 中的"程序性知识"——它告诉智能体**如何做某件事**。每个 Skill 是一个目录，核心文件是 `SKILL.md`。

### 创建 Skill 目录

在项目的 `lib/skills/` 目录下创建一个新的 Skill 目录：

```bash
mkdir -p lib/skills/code-summarizer
```

### 编写 SKILL.md

创建 `lib/skills/code-summarizer/SKILL.md`，内容如下：

```markdown
---
name: code-summarizer
description: >
  读取源代码文件并生成简明摘要。用于快速了解代码文件的功能和结构。
allowed-tools: /dev/fs
metadata:
  author: my-team
  version: "1.0"
  tags:
    - code
    - summary
---

# Code Summarizer

## 何时使用

当用户需要快速了解一个代码文件的功能、结构和关键接口时使用此 Skill。

## 工作流程

1. 通过 /dev/fs 读取用户指定的源代码文件
2. 分析代码结构：包名、导入、导出类型、函数签名
3. 生成简明摘要，包含文件用途、核心类型和关键函数

## 工具使用指南

### /dev/fs — 文件系统访问

用于读取目标源代码文件：
- 读取用户指定的文件获取完整源码
- 如需上下文，可读取同目录的相关文件

## 输出格式

摘要应包含以下部分：
- **文件用途**：一句话描述文件的核心职责
- **核心类型**：列出主要的 struct/interface 及其用途
- **关键函数**：列出重要的导出函数及其签名
- **依赖关系**：列出主要的外部包依赖
```

### SKILL.md 结构解析

`SKILL.md` 由两部分组成：

**Frontmatter（YAML 头部）**：

| 字段 | 说明 |
|------|------|
| `name` | Skill 的唯一标识名 |
| `description` | 简短描述（用于发现阶段，~100 tokens） |
| `allowed-tools` | 空格分隔的 VFS 设备路径白名单 |
| `metadata.version` | 版本号 |
| `metadata.tags` | 标签列表（用于搜索和分类） |

**Body（Markdown 正文）**：Skill 的详细指令，在激活阶段注入智能体的系统提示词。

### allowed-tools 与 VFS 路径映射

`allowed-tools` 字段决定了智能体被允许访问哪些 VFS 设备。这是 Rnix 的安全机制——Skill 只能使用它声明的工具。

| 设备路径 | 能力 |
|----------|------|
| `/dev/fs` | 读写宿主文件系统 |
| `/dev/shell` | 执行 Shell 命令 |
| `/dev/llm/claude` | 调用 LLM 推理 |

我们的 `code-summarizer` 只需要读取文件，所以只声明了 `/dev/fs`。

### 渐进式加载策略

Rnix 采用两阶段加载 Skill 以优化 token 消耗：

1. **发现阶段** — 只读取 frontmatter（~100 tokens），用于判断 Skill 是否匹配
2. **激活阶段** — 读取完整 body（< 5000 tokens），注入系统提示词

这意味着 frontmatter 的 `description` 字段必须足够准确，让 Rnix 能在发现阶段做出正确的匹配决策。

---

## 步骤二：创建引用 Skill 的 Agent

Agent 是"身份定义"——它定义了智能体的角色、使用的模型和引用的 Skill。

### 创建 Agent 目录

```bash
mkdir -p lib/agents/summarizer
```

### 编写 agent.yaml

创建 `lib/agents/summarizer/agent.yaml`：

```yaml
name: summarizer
description: "读取代码文件并生成结构化摘要的智能体"
models:
  provider: claude
  preferred: sonnet
  fallback: haiku
context_budget: 4096
skills:
  - code-summarizer
```

**字段说明：**

| 字段 | 说明 |
|------|------|
| `name` | Agent 的唯一标识名 |
| `description` | Agent 的简短描述 |
| `models.provider` | LLM 提供者（当前仅支持 `claude`） |
| `models.preferred` | 首选模型 |
| `models.fallback` | 降级模型 |
| `context_budget` | 上下文预算（tokens） |
| `skills` | 引用的 Skill 列表（对应 Skill 的 `name` 字段） |

### 编写 instructions.md

创建 `lib/agents/summarizer/instructions.md`——Agent 的系统提示词：

```markdown
# Summarizer Agent

你是一个代码摘要生成专家。你的职责是读取用户指定的代码文件，生成结构化的摘要报告。

## 工作原则

- 摘要应简明扼要，一个文件的摘要不超过 200 字
- 重点关注导出的类型和函数（公共 API）
- 如果文件过长（> 500 行），先概述整体结构再详述重点部分
- 使用中文输出摘要
```

### 四层能力模型

此时你已经搭建了 Rnix 的能力层次结构：

```
Process（运行时实例）
  └── Agent（身份：summarizer）
        └── Skill（能力：code-summarizer）
              └── Tools（工具：/dev/fs）
```

- **Process** 是运行时实例——每次 `rnix -i` 创建一个
- **Agent** 定义了"我是谁"——角色、模型偏好
- **Skill** 定义了"我能做什么"——知识和工具权限
- **Tools** 是 VFS 设备——实际的执行能力

---

## 步骤三：运行 Skill

### 启动智能体

使用 `--agent` 标志指定刚创建的 Agent：

```bash
rnix -i "总结 kernel/kernel.go 的代码结构" --agent=summarizer
```

你将看到类似以下的输出：

```
PID 1 | summarizer | running
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━

## 摘要: kernel/kernel.go

**文件用途:** Rnix 内核的核心实现，包含 Kernel 接口组合和 Spawn/reasonStep 主循环。

**核心类型:**
- KernelImpl — 内核实现结构体，组合了 ProcessManager、ContextManager、FileSystem 等子接口
- Kernel — 内核顶层接口，嵌入所有子接口

**关键函数:**
- Spawn(intent, agent, opts) → PID — 创建并启动智能体进程
- reasonStep(proc) → error — 单次推理步骤

━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
PID 1 | completed | 0 | 3.2s | 1,240 tokens
```

### 查看进程状态

在智能体运行期间（或运行后），可以用 `rnix ps` 查看进程列表：

```bash
rnix ps
```

输出示例：

```
  PID   STATE     SKILL              TOKENS   ELAPSED
    1   zombie    code-summarizer    1,240    3.2s
```

### 使用 astrace 查看 Syscall 追踪

`rnix astrace` 可以实时追踪智能体的每一个系统调用——Open、Read、Write 等操作都会被记录：

```bash
rnix astrace 1
```

输出示例：

```
[  0.001s] Spawn(agent="summarizer", intent="总结 kernel/kernel.go 的代码结构") → 1    1ms
[  0.002s] CtxAlloc() → 1    0µs
[  0.003s] Open(flags=1, path="/lib/skills/code-summarizer/SKILL.md") → 3    0µs
[  0.003s] Read(fd=3, length=1048576) → 892    0µs
[  0.004s] Close(fd=3) → <nil>    0µs
[  0.005s] Open(flags=2, path="/dev/llm/claude") → 4    0µs  ← LLM 调用
[  0.006s] Write(fd=4, size=1234) → <nil>    2.80s  ← 慢操作
[  0.006s] Read(fd=4, length=1048576) → 1560    2ms
[  0.007s] Close(fd=4) → <nil>    0µs
[  0.008s] Open(flags=1, path="/dev/fs") → 5    0µs
[  0.008s] Read(fd=5, length=1048576) → 2048    1ms
[  0.009s] Close(fd=5) → <nil>    0µs
```

从 astrace 输出可以清楚看到：
1. 内核先加载了 `code-summarizer` Skill（读取 `/lib/skills/code-summarizer/SKILL.md`，892 字节）
2. 然后调用 LLM（`/dev/llm/claude`）进行推理——Write 发送请求，Read 获取响应
3. LLM 指示读取目标文件（`/dev/fs`，2048 字节）
4. 一切操作都通过 VFS 完成，Skill 声明的 `allowed-tools: /dev/fs` 控制了访问权限
5. Read 的返回值是**字节数**（如 `→ 2048`），不是文件内容本身

---

## 完整可运行示例

### 文件清单

创建以下文件结构：

```
lib/
├── agents/
│   └── summarizer/
│       ├── agent.yaml
│       └── instructions.md
└── skills/
    └── code-summarizer/
        └── SKILL.md
```

**`lib/skills/code-summarizer/SKILL.md`**：

```markdown
---
name: code-summarizer
description: >
  读取源代码文件并生成简明摘要。用于快速了解代码文件的功能和结构。
allowed-tools: /dev/fs
metadata:
  author: my-team
  version: "1.0"
  tags:
    - code
    - summary
---

# Code Summarizer

## 何时使用

当用户需要快速了解一个代码文件的功能、结构和关键接口时使用此 Skill。

## 工作流程

1. 通过 /dev/fs 读取用户指定的源代码文件
2. 分析代码结构：包名、导入、导出类型、函数签名
3. 生成简明摘要

## 工具使用指南

### /dev/fs — 文件系统访问

用于读取目标源代码文件。

## 输出格式

- **文件用途**：一句话描述
- **核心类型**：主要 struct/interface
- **关键函数**：重要的导出函数签名
```

**`lib/agents/summarizer/agent.yaml`**：

```yaml
name: summarizer
description: "读取代码文件并生成结构化摘要的智能体"
models:
  provider: claude
  preferred: sonnet
  fallback: haiku
context_budget: 4096
skills:
  - code-summarizer
```

**`lib/agents/summarizer/instructions.md`**：

```markdown
# Summarizer Agent

你是一个代码摘要生成专家。读取用户指定的代码文件，生成结构化的摘要报告。

- 摘要简明扼要，不超过 200 字
- 重点关注导出的类型和函数
- 使用中文输出
```

### 运行命令

```bash
rnix -i "总结 kernel/kernel.go 的代码结构" --agent=summarizer
```

### 预期输出

智能体读取 `kernel/kernel.go`，输出结构化的代码摘要报告。

---

## 常见问题与排错

### Skill 找不到

**症状：** 运行时报错 `skill not found: code-summarizer`

**原因：** Skill 目录名或 SKILL.md 中的 `name` 字段与 Agent 的 `skills` 列表不匹配。

**解决：** 确认 `lib/skills/code-summarizer/SKILL.md` 的 `name` 字段值为 `code-summarizer`，与 `agent.yaml` 中 `skills: [code-summarizer]` 一致。

### Agent 加载失败

**症状：** 报错 `agent not found: summarizer`

**原因：** Agent 目录名与 `--agent=summarizer` 参数不匹配。

**解决：** 确认目录为 `lib/agents/summarizer/`，且包含 `agent.yaml` 文件。

### 权限错误（PERMISSION）

**症状：** astrace 中看到 `[ERR]` 行，错误码为 `PERMISSION`

**原因：** Skill 的 `allowed-tools` 未包含智能体实际访问的 VFS 设备路径。

**解决：** 检查 `SKILL.md` 的 `allowed-tools` 字段，添加缺失的设备路径。详见 [教程 2：调试第一个 bug](debugging-first-bug.md)。

### SKILL.md 格式错误

**症状：** 报错提示 YAML 解析失败

**原因：** Frontmatter 格式不正确（缺少 `---` 分隔符、缩进错误等）。

**解决：** 确认 SKILL.md 以 `---` 开头和结尾包裹 YAML frontmatter，且 YAML 语法正确。

---

## 下一步

- [教程 2：调试第一个 bug](debugging-first-bug.md) — 学习使用 astrace 定位和修复问题
- [教程 3：组合多智能体工作流](composing-multi-agent-workflow.md) — 学习用 Compose 编排多个智能体协作

## 相关文档

- [核心概念：Skill](../concepts.md) — Skill 的概念模型和四层能力架构
- [参考手册：Agent 和 Skill 清单](../reference.md) — agent.yaml 和 SKILL.md 的完整字段说明
