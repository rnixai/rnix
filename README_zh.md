# Rnix

<div align="center">

**AI 智能体操作系统 — 用 Unix 哲学构建**

[![License: MIT](https://img.shields.io/badge/License-MIT-green.svg)](LICENSE) [![Go Report Card](https://goreportcard.com/badge/github.com/rnixai/rnix)](https://goreportcard.com/report/github.com/rnixai/rnix) [![GitHub Stars](https://img.shields.io/github/stars/rnixai/rnix?style=social)](https://github.com/rnixai/rnix/stargazers)

[文档](https://docs.rnix.ai/) | [English](README.md) | [更新日志](CHANGELOG.md)

</div>

---

```bash
# 懂 Unix？你已经会用 Rnix 了。
go install github.com/rnixai/rnix/cmd/rnix@latest
rnix -i "分析项目结构"
rnix strace 1   # 精确查看你的智能体做了什么
rnix top        # 实时进程监控
```

## 问题所在

你构建了一个复杂的多智能体工作流——编排、工具链、重试逻辑。它出错了。错误藏在回调、角色定义和 LLM 调用纠缠在一起的链条某处。你不知道发生了什么，无法追踪执行过程，也无法终止失控的智能体。

**Rnix 赋予你的智能体和操作系统调试一样的工具。**

## Rnix 有何不同

| | LangChain | CrewAI | AutoGen | **Rnix** |
|---|---|---|---|---|
| 架构 | 库 | 库 | 库 | **运行时** |
| 智能体模型 | 回调 | 角色 | 对话 | **进程（PID）** |
| 资源访问 | API | API | API | **虚拟文件系统** |
| 调试 | LangSmith | 日志 | 手动 | **rnix strace** |
| 仪表盘 | 无 | 无 | 无 | **rnix dashboard** |
| 终止智能体 | 无 | 无 | 无 | **rnix kill** |
| 语言 | Python | Python | Python/.NET | **Go** |

## 快速上手

```bash
# 安装
go install github.com/rnixai/rnix/cmd/rnix@latest

# 初始化配置
rnix init

# 运行你的第一个智能体
rnix -i "分析项目结构"
#
# 追踪系统调用
rnix strace 1

# 实时仪表盘
rnix dashboard
```

## 核心概念

| Unix | Rnix | 说明 |
|------|------|------|
| `process` | 智能体进程 | 每次运行分配 PID、状态机和资源表 |
| `/dev/*` | VFS 设备 | LLM、文件系统、Shell、MCP — 统一通过 Open/Read/Write 访问 |
| `strace` | `rnix strace` | 实时查看智能体的每一次系统调用 |
| `kill` | `rnix kill` | 从任意终端终止任意智能体 |
| `top` | `rnix top` | 实时进程监控，显示 token 和耗时指标 |
| `gdb` | `rnix gdb` | 交互式调试器：断点、单步、检查上下文 |
| `init` | `rnix init` | 引导配置 Provider、Agent 和 Skill |

## 许可证

MIT — 详见 [LICENSE](LICENSE)。

---

如果 Rnix 帮你调试了 AI 智能体，给我们一个 GitHub Star 吧！
