# Rnix

<div align="center">

**AI 时代的 Unix — 智能体操作系统**

[![License: MIT](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE) [![Go Report Card](https://goreportcard.com/badge/github.com/rnixai/rnix)](https://goreportcard.com/report/github.com/rnixai/rnix) [![GitHub Stars](https://img.shields.io/github/stars/rnixai/rnix?style=social)](https://github.com/rnixai/rnix/stargazers)

[文档](https://docs.rnix.ai/) | [English](README.md) | [更新日志](CHANGELOG.md)

</div>

---

```bash
# 懂 Unix？你已经会用 Rnix 了。
rnix -i "分析项目结构"
rnix strace 1        # 精确查看智能体做了什么
rnix dashboard       # 多面板实时监控
rnix gdb 1           # 断点、单步、检查上下文
```

## 设计理念

Rnix 把 50 年验证过的操作系统抽象直接搬到了 AI 智能体的世界。不是框架，不是库——是运行时。

**一切皆进程** — 每个智能体都是拥有 PID、状态机和资源表的一等进程。用 `ps` 查看、`kill` 终止、`strace` 追踪，和管理系统进程一样管理智能体。进程可以暂停、恢复、挂起，也可以从检查点续跑。

**一切皆文件** — LLM、文件系统、Shell、记忆、Web 搜索、LSP 代码智能、MCP 工具——全部统一为 VFS 设备，通过 Open/Read/Write/Close 访问。增加新能力，只需挂载一个新设备。

**可观测性内建** — 不是事后接入的日志系统。`strace` 显示每一次系统调用，`gdb` 提供断点和单步调试，`dashboard` 提供带进程树、时间线、上下文热力图和 LLM 对话回放的多面板监控。

**编排即进程管理** — Supervisor 树自动重启崩溃进程，Compose DAG 声明智能体间的依赖关系，Budget Pool 管理 token 预算，Intent 系统分解高级意图为子任务 DAG。这些都是 OS 级原语，不是应用层封装。

## 快速上手

```bash
# 安装
go install github.com/rnixai/rnix/cmd/rnix@latest

# 初始化配置
rnix init

# 运行你的第一个智能体
rnix -i "分析项目结构"

# 追踪系统调用
rnix strace 1

# 实时仪表盘
rnix dashboard
```

## 核心概念

| Unix | Rnix | 说明 |
|------|------|------|
| `process` | 智能体进程 | 每次运行分配 PID（UUID v7）、状态机和资源表 |
| `/dev/*` | VFS 设备 | LLM、文件系统、Shell、记忆、Web、LSP、MCP — 统一文件接口 |
| `strace` | `rnix strace` | 实时查看每一次系统调用 |
| `kill` | `rnix kill` | 从任意终端终止任意智能体 |
| `top` | `rnix top` | 实时进程监控，token 和耗时指标 |
| `gdb` | `rnix gdb` | 交互式调试器：断点、单步、检查上下文 |
| `ps` | `rnix ps` | 列出所有进程状态 |
| pipe | `rnix compose` | DAG 编排多智能体协作 |
| suspend/resume | `rnix suspend/resume` | 挂起进程，从检查点恢复 |

## 架构一览

```
┌─────────────────────────────────────────────────────────┐
│  CLI & AgentShell                                       │
│  rnix -i / strace / gdb / ps / compose / dashboard      │
├─────────────────────────────────────────────────────────┤
│  Agent + Skill 定义层                                   │
│  agent.yaml + instructions.md + SKILL.md                │
├─────────────────────────────────────────────────────────┤
│  微内核                                                 │
│  进程模型 / VFS / 上下文管理 / IPC / 50+ 系统调用        │
├─────────────────────────────────────────────────────────┤
│  驱动层                                                 │
│  /dev/llm/* · /dev/fs · /dev/shell · /dev/memory/*      │
│  /dev/web · /dev/lsp · /dev/tasks · /dev/tty · /dev/cron│
├─────────────────────────────────────────────────────────┤
│  宿主 OS + 多 LLM 提供商                                │
│  Claude · Gemini · Qwen · Anthropic SDK · OpenAI-compat │
└─────────────────────────────────────────────────────────┘
```

## 特性亮点

- **7 种 LLM 驱动** — Claude CLI / Cursor CLI / Qwen CLI / Anthropic SDK / Gemini / OpenAI 官方 / OpenAI 兼容（Ollama、Groq、DeepSeek 等）
- **14 种 VFS 设备** — LLM、文件系统、Shell、记忆（commit/recall/profile）、Web、LSP、任务、TTY、Cron、ProcFS、MCP
- **智能体记忆** — 跨会话持久知识存储，安全扫描和异步回写
- **50+ 系统调用** — 覆盖进程、上下文、VFS、IPC、信号、能力和 Supervisor 的稳定 ABI
- **交互式调试** — gdb 风格调试器 + 时间旅行回放 + 执行录制
- **多面板 Dashboard** — 进程树、时间线、热力图、LLM 对话回放、Debug 模式
- **Token 经济** — 预算池、SLA 合约、智能体声誉评分、技能协同矩阵
- **自适应安全** — 行为监控、异常检测和自动暂停

## 许可证

MIT — 详见 [LICENSE](LICENSE)。

完整文档请访问 [docs.rnix.ai](https://docs.rnix.ai/)。
