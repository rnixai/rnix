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

## v0.7 亮点

- **Dashboard 体验** — 多窗格 TUI：进程历史、LLM 对话查看器、意图树、分布式追踪
- **统一推理循环** — LLM 自主选择：tool_call / plan / spawn / specialize / complete
- **UUID v7 进程** — 时间有序 UUID，支持分布式进程追踪
- **原生 ToolCalls** — VFS 设备自描述能力，LLM 动态发现工具

## 许可证

MIT — 详见 [LICENSE](LICENSE)。

---

如果 Rnix 帮你调试了 AI 智能体，给我们一个 GitHub Star 吧！
