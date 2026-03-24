# Rnix

**AI 智能体操作系统 — 用 Unix 哲学驱动智能体**

[English](README.md) | [文档站点](https://rnix.ai/docs) | [GitHub](https://github.com/rnixai/rnix)

---

Rnix 将 Unix 操作系统的核心抽象引入 AI 智能体领域。每一次智能体执行都是一个**进程**，拥有独立的 PID 和状态机；每一个资源（LLM、文件系统、Shell）都是一个**文件**，通过虚拟文件系统统一访问。如果你熟悉 Unix，你已经理解了 Rnix。

## 为什么选择 Rnix

大多数 AI 智能体框架只是库——你导入它们，然后祈祷一切正常。Rnix 不同：它是一个**运行时**，使用了与操作系统相同、经过实战验证的抽象。

- **进程，而非回调** — 每个智能体拥有 PID、状态机、FD 表和信号处理。用 `rnix kill` 终止失控智能体，用 `rnix gdb` 调试。
- **文件，而非 API** — LLM、文件系统、Shell、MCP 工具统一为 VFS 设备。从 `/dev/llm/claude` 读取，就像读文件一样。
- **编排，而非编码** — 用 YAML 定义多智能体工作流。管道连接智能体输出。AgentShell 提供完整脚本能力。
- **可观测，而非猜测** — `rnix strace` 追踪每次系统调用，`rnix dashboard` 提供实时 TUI，`rnix replay` 回放执行过程。

## 快速上手

```bash
# 安装
go install github.com/rnixai/rnix/cmd/rnix@latest

# 运行第一个智能体
rnix -i "分析项目结构"

# 追踪它实际做了什么
rnix strace 1

# 实时进程监控
rnix top
```

## v0.7 亮点

- **Dashboard 全新设计** — 多窗格 TUI：进程历史、LLM 对话查看器、意图树、分布式追踪、评估面板。
- **统一推理循环** — 单一循环，LLM 自主选择行为（tool_call / plan / spawn / specialize / complete）。
- **UUID v7 进程标识** — 时间有序 UUID，支持跨机器分布式进程追踪。
- **原生 ToolCalls** — VFS 设备自描述能力，LLM 动态发现工具。

## 架构

```
CLI → Unix Socket → 守护进程（内核 + 进程表）
                         │
                    ReasonStep 循环 ←→ VFS 设备
                    (LLM, FS, Shell, MCP)
```

## 文档

完整文档 — CLI 参考、配置说明、Agent/Skill 模型、VFS 设备路径、开发指南 — 请访问 **[rnix.ai/docs](https://rnix.ai/docs)**。

## 许可证

MIT — 详见 [LICENSE](LICENSE)。
