# Crux 快速上手指南

本指南帮助你在 15 分钟内完成 Crux 的安装和首次运行，体验 AI 智能体操作系统的核心功能。

---

## 前置条件

### Go 环境

确认已安装 Go 1.26 或更高版本：

```bash
$ go version
go version go1.26.0 linux/amd64
```

如果未安装，请前往 [Go 官方下载页面](https://go.dev/dl/) 安装。

### Claude Code CLI

Crux 通过 Claude Code CLI 调用 LLM 推理。确认已安装：

```bash
$ claude --version
1.0.3
```

如果未安装：

```bash
npm install -g @anthropic-ai/claude-code
```

安装后，需要配置有效的 API 密钥。请参考 [Claude Code 文档](https://docs.anthropic.com/en/docs/claude-code) 完成配置。

---

## 安装 Crux

使用 `go install` 一键安装：

```bash
go install github.com/gonewx/crux/cmd/crux@latest
```

验证安装成功：

```bash
$ crux version
crux v0.1.0
claude-code: 1.0.3
```

如果看到以下输出，说明 Claude Code CLI 未安装或不在 PATH 中：

```
crux v0.1.0
✗ claude-code CLI not found
  → 建议: npm install -g @anthropic-ai/claude-code
```

---

## 首次运行

### 最简用法

向 Crux 传递一个意图字符串，即可 Spawn 一个智能体进程来完成任务：

```bash
$ crux "你好，请介绍你自己"
```

你将看到类似以下的输出：

```
[kernel] spawning PID 1...
[agent/1] reasoning step 1...
══ Result ══════════════════════════════════════════════════════════════════════
  你好！我是运行在 Crux 中的一个智能体进程。Crux 是一个面向 AI 智能体的操作系统，
  我通过系统调用与内核交互，使用 VFS 设备来完成你的请求...
════════════════════════════════════════════════════════════════════════════════
[kernel] PID 1 exited(0) | tokens: 856 | elapsed: 4.1s
```

### 解读输出

| 输出行 | 含义 |
|--------|------|
| `[kernel] spawning PID 1...` | 内核正在创建智能体进程，分配 PID 1 |
| `[agent/1] reasoning step 1...` | PID 1 的智能体正在执行第 1 步推理 |
| `══ Result ══...` | 双线边框内是智能体的最终输出结果 |
| `[kernel] PID 1 exited(0)` | 进程正常退出（退出码 0） |
| `tokens: 856` | 本次执行消耗的 token 数量 |
| `elapsed: 4.1s` | 总耗时 |

---

## 使用 Agent

Agent 定义了智能体的身份和角色。通过 `--agent` 参数可以使用预定义的 Agent：

```bash
$ crux "分析 ./cmd/crux/main.go" --agent=code-analyst
```

`code-analyst` 是 Crux 内置的参考 Agent，专门用于分析代码质量、识别潜在问题并提供改进建议。它引用了 `code-analysis` Skill，拥有访问文件系统（`/dev/fs`）和 Shell（`/dev/shell`）的权限。

你将看到类似以下的输出：

```
[kernel] spawning PID 1...
[agent/1] reasoning step 1...
[agent/1] reasoning step 2...
[agent/1] reasoning step 3...
══ Result ══════════════════════════════════════════════════════════════════════
  ## 代码分析报告: cmd/crux/main.go

  ### 总体评价
  该文件是 Crux CLI 的入口点，结构清晰，职责明确...

  ### 发现
  - **Info** — 全局变量较多，可考虑封装到结构体中
  - **Info** — 建议为 runRoot 添加更多错误分类处理
════════════════════════════════════════════════════════════════════════════════
[kernel] PID 1 exited(0) | tokens: 2340 | elapsed: 12.5s
```

> 💡 想了解 Agent 和 Skill 的设计原理？请参阅 [核心概念文档](concepts.md) 中的"Agent 与 Skill"章节。

---

## astrace 调试体验

`astrace`（Agent Strace）是 Crux 的调试工具，类似 Unix 的 `strace`，可以实时查看智能体进程的每一步系统调用（Syscall），帮助你理解智能体的完整执行过程。

### 使用方法

在一个终端启动一个智能体任务：

```bash
$ crux "分析当前项目结构并给出建议"
```

在另一个终端，用 `crux ps` 找到正在运行的进程 PID，然后 attach：

```bash
$ crux astrace 1
```

### 预期输出

```
[astrace] attached to PID 1 (state: running)
[  0.013s] Open(flags=2, path="/dev/llm/claude") → 3    1ms  ← LLM 调用
[  0.014s] Write(fd=3, size=1234) → <nil>    5.20s  ← 慢操作
[  5.214s] Read(fd=3, length=1048576) → 892    2ms
[  5.216s] Open(flags=2, path="/dev/fs/./README.md") → 4    1ms
[  5.217s] Write(fd=4, size=56) → <nil>    0µs
[  5.217s] Read(fd=4, length=1048576) → 2048    1ms
[  5.218s] Close(fd=4) → <nil>    0µs
[  5.218s] Write(fd=3, size=3456) → <nil>    3.10s  ← 慢操作
[  8.318s] Read(fd=3, length=1048576) → 1024    2ms
[  8.320s] Close(fd=3) → <nil>    0µs
[astrace] detached from PID 1 (process exited)
```

### 解读关键 Syscall

| Syscall 行 | 含义 |
|------------|------|
| `Open(path="/dev/llm/claude")` | 打开 LLM 推理设备 |
| `Write(fd=3, size=1234)` | 向 LLM 发送推理请求（1234 字节） |
| `Read(fd=3, ...)` | 读取 LLM 响应 |
| `Open(path="/dev/fs/./README.md")` | 智能体请求读取文件（工具调用） |
| `Close(fd=4)` | 关闭文件设备 |
| `← LLM 调用` | 标注含 `/dev/llm/` 路径的 Open 操作 |
| `← 慢操作` | 标注耗时超过 1 秒的操作（通常是 LLM 推理） |

按 `Ctrl+C` 可随时脱离 astrace，不会影响被追踪的进程。

---

## 进程管理

### 查看进程列表

```bash
$ crux ps
```

输出示例：

```
  PID   STATE     SKILL              TOKENS   ELAPSED
─────   ─────────   ───────────────   ────────   ────────
    1   running   code-analysis        456      3.2s
    2   zombie    —                    123      1.1s

1 active, 1 zombie, 2 total
```

使用 `--json` 获取 JSON 格式输出，方便脚本处理：

```bash
$ crux ps --json
```

### 终止进程

```bash
$ crux kill 1
[kernel] PID 1: signal sent (SIGTERM)
```

---

## 下一步

恭喜！你已经体验了 Crux 的核心功能。以下资源帮助你进一步探索：

- **[核心概念文档](concepts.md)** — 深入理解进程、VFS、Syscall、Agent 与 Skill 的设计哲学
- **参考手册**（`docs/reference.md`，即将发布）— 完整的 CLI 命令参考、Syscall 签名和 Manifest 字段说明
- **Agent 和 Skill 扩展** — 创建自定义 Agent（`lib/agents/`）和 Skill（`lib/skills/`），扩展 Crux 的能力边界
