# 教程 2：调试第一个 bug

本教程带你体验 Rnix 的调试工作流：故意引入一个 bug，用 `rnix astrace` 定位问题，修复后验证。

---

## 前置条件

- 已完成 [教程 1：编写第一个 Skill](writing-first-skill.md)（了解 Skill 和 Agent 的创建流程）
- Rnix 已安装并可运行

---

## 你将学到什么

1. 如何用 `rnix astrace` 实时追踪智能体的系统调用
2. 如何从 SyscallEvent 中读取错误信息定位问题
3. 常见错误码及其含义

---

## 步骤一：准备一个有 bug 的 Skill

我们复用教程 1 的 `code-summarizer` Skill，但故意制造一个权限 bug：让 Skill 需要执行 Shell 命令（比如 `wc -l` 统计行数），却没有在 `allowed-tools` 中声明 `/dev/shell` 权限。

### 创建有 bug 的 Skill

创建 `lib/skills/line-counter/SKILL.md`：

```markdown
---
name: line-counter
description: >
  统计代码文件的行数并报告。需要文件系统和 Shell 访问。
allowed-tools: /dev/fs
metadata:
  author: my-team
  version: "1.0"
  tags:
    - code
    - metrics
---

# Line Counter

## 工作流程

1. 通过 /dev/fs 读取用户指定的文件确认其存在
2. 通过 /dev/shell 执行 `wc -l` 命令统计行数
3. 输出文件名和行数

## 工具使用指南

### /dev/fs — 文件系统访问
用于确认目标文件存在。

### /dev/shell — Shell 命令执行
用于运行 `wc -l` 统计行数。
```

注意看 bug 在哪里：Skill body 中声明了需要 `/dev/shell`，但 frontmatter 的 `allowed-tools` **只有** `/dev/fs`，缺少了 `/dev/shell`。

### 创建引用该 Skill 的 Agent

创建 `lib/agents/counter/agent.yaml`：

```yaml
name: counter
description: "统计代码行数的智能体"
models:
  provider: claude
  preferred: haiku
context_budget: 2048
skills:
  - line-counter
```

### 运行并观察失败

```bash
rnix -i "统计 kernel/kernel.go 的行数" --agent=counter
```

你会看到智能体尝试执行但报错退出：

```
PID 2 | counter | running
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
错误: [PERMISSION] PID 2 Open /dev/shell: permission denied (device not in allowed-tools)
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
PID 2 | failed | 1 | 1.5s | 320 tokens
```

智能体因为权限不足而失败了。但错误信息可能不够详细——让我们用 `astrace` 深入定位。

---

## 步骤二：使用 rnix astrace 定位问题

`rnix astrace` 追踪进程的每一个系统调用，就像 Unix 的 `strace` 追踪系统调用一样。

### 启动 astrace

在一个终端启动智能体：

```bash
rnix -i "统计 kernel/kernel.go 的行数" --agent=counter
```

在另一个终端追踪该进程（假设 PID 为 3）：

```bash
rnix astrace 3
```

### 分析 astrace 输出

```
[  0.001s] Spawn(agent="counter", intent="统计 kernel/kernel.go 的行数") → 3    1ms
[  0.002s] CtxAlloc() → 2    0µs
[  0.003s] Open(flags=1, path="/lib/skills/line-counter/SKILL.md") → 3    0µs
[  0.003s] Read(fd=3, length=1048576) → 645    0µs
[  0.004s] Close(fd=3) → <nil>    0µs
[  0.005s] Open(flags=2, path="/dev/llm/claude") → 4    0µs  ← LLM 调用
[  0.005s] Write(fd=4, size=890) → <nil>    1.20s  ← 慢操作
[  0.006s] Read(fd=4, length=1048576) → 512    2ms
[  0.006s] Close(fd=4) → <nil>    0µs
[  0.007s] Open(flags=1, path="/dev/fs") → 5    0µs
[  0.007s] Read(fd=5, length=1048576) → 2048    1ms
[  0.008s] Close(fd=5) → <nil>    0µs
[ERR] [  0.009s] Open(flags=2, path="/dev/shell") → err([PERMISSION] PID 3 Open /dev/shell: permission denied)    0µs
```

### 解读关键信息

最后一行是关键——带有 `[ERR]` 前缀的红色错误行：

```
[ERR] [  0.009s] Open(flags=2, path="/dev/shell") → err([PERMISSION] PID 3 Open /dev/shell: permission denied)    0µs
```

从这一行可以提取以下信息：

| 字段 | 值 | 含义 |
|------|-----|------|
| Syscall | `Open` | 尝试打开设备 |
| path | `/dev/shell` | 目标 VFS 设备路径 |
| PID | `3` | 出错的进程 |
| 错误码 | `PERMISSION` | 权限不足 |
| 错误描述 | `permission denied` | 设备未在 allowed-tools 中声明 |

现在定位清楚了：**智能体尝试打开 `/dev/shell` 设备，但 Skill 的 `allowed-tools` 中没有包含这个路径，所以内核拒绝了访问。**

### SyscallEvent 结构

每条 astrace 输出对应一个 `SyscallEvent`，包含：

- **Timestamp** — 相对进程启动的时间戳
- **Syscall** — 系统调用名称（Open/Read/Write/Close/Spawn 等）
- **PID** — 进程 ID
- **Args** — 调用参数（path、fd、flags 等）
- **Result** — 返回值
- **Err** — 错误信息（nil 表示成功）
- **Duration** — 调用耗时

错误行额外标注 `[ERR]` 前缀（终端中显示为红色），方便一眼定位问题。

---

## 步骤三：修复 bug 并验证

### 修复

问题很明确：`SKILL.md` 的 `allowed-tools` 缺少 `/dev/shell`。修改 `lib/skills/line-counter/SKILL.md` 的 frontmatter：

修复前：
```yaml
allowed-tools: /dev/fs
```

修复后：
```yaml
allowed-tools: /dev/fs /dev/shell
```

### 重新运行

```bash
rnix -i "统计 kernel/kernel.go 的行数" --agent=counter
```

这次应该正常完成：

```
PID 4 | counter | running
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
kernel/kernel.go: 287 行
━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━
PID 4 | completed | 0 | 2.1s | 450 tokens
```

### 用 astrace 确认修复

再次用 astrace 追踪确认所有 syscall 正常：

```bash
rnix astrace 4
```

```
[  0.001s] Spawn(agent="counter", intent="统计 kernel/kernel.go 的行数") → 4    1ms
[  0.002s] CtxAlloc() → 3    0µs
[  0.003s] Open(flags=1, path="/lib/skills/line-counter/SKILL.md") → 3    0µs
[  0.003s] Read(fd=3, length=1048576) → 680    0µs
[  0.004s] Close(fd=3) → <nil>    0µs
[  0.005s] Open(flags=2, path="/dev/llm/claude") → 4    0µs  ← LLM 调用
[  0.005s] Write(fd=4, size=920) → <nil>    1.80s  ← 慢操作
[  0.006s] Read(fd=4, length=1048576) → 480    2ms
[  0.006s] Close(fd=4) → <nil>    0µs
[  0.007s] Open(flags=1, path="/dev/fs") → 5    0µs
[  0.007s] Read(fd=5, length=1048576) → 2048    1ms
[  0.008s] Close(fd=5) → <nil>    0µs
[  0.009s] Open(flags=2, path="/dev/shell") → 6    0µs
[  0.009s] Write(fd=6, size=56) → <nil>    50ms
[  0.010s] Read(fd=6, length=1048576) → 24    0µs
[  0.010s] Close(fd=6) → <nil>    0µs
```

这次没有 `[ERR]` 行了——所有 syscall 都成功执行，包括 `/dev/shell` 的 Open/Write/Read/Close。

---

## 扩展调试技巧

### rnix ps — 查看进程状态

```bash
rnix ps
```

快速查看所有进程的当前状态（running/zombie/dead）和基本信息。用于确认进程是否还在运行或已经结束。

### rnix log — 查看分类日志

```bash
rnix log
```

查看智能体的推理日志，按类别分组。比 astrace 更高层——astrace 追踪的是 syscall 层面的操作，log 展示的是推理过程的逻辑记录。

### rnix top — 实时监控

```bash
rnix top
```

TUI 界面实时监控所有进程的状态、Token 消耗和资源使用。详见 [教程 3](composing-multi-agent-workflow.md)。

### 常见错误码

| 错误码 | 含义 | 常见原因 |
|--------|------|---------|
| `PERMISSION` | 权限不足 | Skill 的 allowed-tools 未包含目标设备 |
| `NOT_FOUND` | 资源不存在 | 文件路径错误、进程已退出、设备未注册 |
| `TIMEOUT` | 操作超时 | LLM 响应超时、外部命令执行超时 |
| `DRIVER` | 驱动错误 | LLM CLI 返回错误、Shell 命令执行失败 |
| `INTERNAL` | 内部错误 | 内核 bug、非法状态转移 |

---

## 下一步

- [教程 3：组合多智能体工作流](composing-multi-agent-workflow.md) — 学习用 Compose 和管道编排多个智能体协作
- [教程 1：编写第一个 Skill](writing-first-skill.md) — 回顾 Skill 和 Agent 的创建流程

## 相关文档

- [核心概念：系统调用](../concepts.md) — Syscall 和 SyscallEvent 的概念模型
- [参考手册：rnix astrace](../reference.md) — astrace 命令的完整参数和输出格式
- [参考手册：SyscallError](../reference.md) — 错误码枚举和 SyscallError 结构
