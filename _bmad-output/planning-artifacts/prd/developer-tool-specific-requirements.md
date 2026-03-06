# Developer Tool Specific Requirements

## Project-Type Overview

Rnix 是一个运行时框架而非传统库/SDK。开发者不写 Go 代码来使用 Rnix——他们通过三个接口层与系统交互：

| 接口层 | 格式 | 用途 | 阶段 |
|--------|------|------|------|
| **AgentShell CLI** | 命令行 | `rnix "意图" --agent=<name>`、`rnix astrace`、`rnix ps` | MVP |
| **Agent 定义** | YAML + Markdown | `agent.yaml`（身份+模型+Skill引用）+ `instructions.md`（角色策略） | MVP |
| **Skill 定义** | Markdown（Agent Skills 标准） | `SKILL.md`（YAML frontmatter + 程序性知识） | MVP |
| **Agent Compose** | YAML | `rnix-compose.yaml` 多智能体编排 | Phase 2 |
| **Go SDK（待定）** | Go | 嵌入式使用，根据用户反馈决策 | Phase 2+ |

## Installation & Distribution

| 方式 | 阶段 | 说明 |
|------|------|------|
| `go install` | MVP | 唯一安装方式，单二进制，零依赖 |
| 预编译二进制 / brew / docker | Phase 2+ | 根据社区需求扩展 |

**MVP 安装体验目标：** `go install github.com/rnixai/rnix/cmd/rnix@latest` → 可用。不需要配置文件、不需要额外依赖、不需要 Docker。

## API Surface (Syscall ABI)

Rnix 的"API"不是 REST 端点或 Go 函数——而是 **~45 个 syscall**（Phase 1 + Phase 2）：

**Phase 1（~15 个，MVP）：**

**进程管理：** `Spawn`、`Kill`、`Wait`、`GetPID`、`PS`
**上下文管理：** `CtxAlloc`、`CtxRead`、`CtxWrite`、`CtxFree`
**文件系统：** `Open`、`Read`、`Write`、`Close`、`Stat`
**调试：** `DebugRecord`

**Phase 2（~30 个，能力栈建设）：**

**IPC 通信：** `Send`、`Recv`、`Pipe`、`Broadcast`、`GetProcGroup`、`JoinGroup`
**设备与文件：** `Mount`、`Unmount`、`Ioctl`、`Fcntl`、`Seek`、`Fstat`
**信号与事件：** `Signal`、`SigBlock`、`SigUnblock`、`SigPending`、`Watch`、`Notify`
**时间与定时：** `Sleep`、`Timer`、`GetTime`、`WaitForEvent`
**Capability 权限：** `CapGrant`、`CapRevoke`、`CapCheck`、`GetCaps`
**调试增强：** `Attach`、`Detach`、`BreakPoint`、`Snapshot`

这个 ABI 是 Rnix 的"宪法"——Phase 1 的 15 个 syscall 是 Phase 2 完整 45 个的稳定子集，向后兼容。Phase 2 通过新增子接口（`IPCManager`、`CapManager` 等）嵌入 Kernel 接口组合，不破坏现有 ABI。

## Documentation Strategy

| 文档类型 | 内容 | 阶段 |
|---------|------|------|
| **概念文档** | 为什么是 Agent OS、核心概念（进程、VFS、Skill、syscall）、与现有框架对比 | MVP |
| **快速上手** | 安装 → spawn 第一个智能体 → 看 astrace 输出（≤ 15 分钟目标） | MVP |
| **参考手册** | syscall 列表、VFS 路径规范、agent.yaml / SKILL.md 字段、CLI 命令 | MVP |
| **教程** | 写第一个 Skill、调试第一个 bug、组合多智能体 | Phase 2 |
| **架构文档** | 微内核设计、进程模型、驱动层、上下文管理 | Phase 2 |

## Code Examples & First Agent + Skill

MVP 交付一个完整的参考 Agent + 参考 Skill：

**参考 Agent（`lib/agents/code-analyst/`）：**

```
lib/agents/code-analyst/
├── agent.yaml        # Agent 配置：身份、模型偏好、上下文预算、Skill 引用
└── instructions.md   # Agent 角色定义 + 行为策略
```

**参考 Skill（`lib/skills/code-analysis/`，遵循 Agent Skills 行业标准）：**

```
lib/skills/code-analysis/
├── SKILL.md          # 标准格式：YAML frontmatter（name/description/allowed-tools）+ Markdown 程序性知识
├── scripts/          # 可选：可执行脚本
├── references/       # 可选：参考文档
└── assets/           # 可选：模板、资源
```

Agent 定义"我是谁"（身份 + 模型 + 策略 + Skill 引用），Skill 定义"如何做 X"（程序性知识 + 工具权限）。Skill 遵循 Agent Skills 开放标准（agentskills.io，由 Anthropic 发起，30+ AI 工具采用），可与生态互操作。

这对参考实现同时承担三个角色：
1. **自举验证的载体**——用 code-analyst Agent 分析 Rnix 自身源码
2. **Agent + Skill 格式的参考实现**——开发者照着它写自己的 Agent 和 Skill
3. **快速上手文档的素材**——demo 中直接使用

## Implementation Considerations

**Go 语言特性利用：**
- goroutine → 智能体进程（轻量、高并发）
- channel → IPC（类型安全、阻塞语义）
- interface → syscall 契约（编译时检查）
- 单二进制编译 → 零依赖部署

**不适用项（Skip）：**
- Visual design — CLI 工具，无 UI
- Store compliance — 不涉及应用商店
- IDE integration — MVP 阶段不需要，后续可考虑 LSP
- Migration guide — 全新范式，无迁移路径
