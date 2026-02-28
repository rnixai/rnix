# Project Context Analysis

## Requirements Overview

**Functional Requirements（40 条，8 个领域）：**

| 领域 | FR 范围 | 核心架构含义 |
|------|---------|------------|
| 智能体生命周期 | FR1-FR7 | 进程模型是内核骨架——spawn/kill/wait/ps + 孤儿 reparent + zombie 回收，决定 Kernel 结构体核心 API |
| 智能体推理 | FR8-FR12 | reasonStep 循环是系统心跳——LLM 调用→解析 action→工具执行→上下文追加→循环。关键依赖：Claude Code CLI |
| 文件系统与资源 | FR13-FR18 | VFS 是统一抽象层——`/proc/{pid}/`、`/dev/`、`/dev/fs`、`/dev/llm/`、`/dev/shell` 通过同一接口 |
| 上下文管理 | FR19-FR22 | 每个智能体独立上下文空间——分配/读写/组装 prompt/释放 |
| Agent 管理 | FR23-FR25 | agent.yaml + instructions.md 定义智能体身份、模型偏好、Skill 引用，注入 system prompt |
| Skill 管理 | FR25a-FR27 | SKILL.md（Agent Skills 行业标准格式）渐进式加载，allowed-tools 聚合映射为 `/dev/` 权限白名单 |
| 调试与可观测 | FR28-FR32 | astrace 差异化核心——实时 syscall 追踪，DebugRecord 数据采集贯穿所有 syscall |
| CLI | FR33-FR37 | 三命令入口（`crux "意图"` / `crux astrace` / `crux ps`），go install 单二进制 |
| 文档 | FR38-FR40 | 概念文档 + 快速上手 + 参考手册 |

**Non-Functional Requirements（20 条，驱动架构的关键约束）：**

| 约束类别 | 关键 NFR | 架构影响 |
|---------|---------|---------|
| 性能 | NFR1: spawn→完成 ≤30s；NFR2: ps ≤100ms；NFR3: astrace ≤500ms | 进程表须内存数据结构，astrace 需低延迟事件通道 |
| 可靠性 | NFR6: 20 次连续 ≥95%；NFR7: 超时 5s 内转 zombie；NFR8: 退出 10s 内释放资源 | 健壮的错误传播 + goroutine 生命周期管理 + context.Context 取消 |
| 集成 | NFR11-12: Claude Code CLI 参数 + stream-json | 驱动层封装 CLI 交互，stream-json 为 astrace 数据源 |
| 安全 | NFR15-17: 继承用户权限，Skill allowed-tools 白名单 | MVP 无完整 Capability，Agent 聚合 Skill allowed-tools 为最小安全边界 |
| 可维护性 | NFR19: ABI 兼容 Phase 2；NFR20: LLM 驱动单文件封装 | syscall 接口可扩展（15→45），驱动层抽象干净 |

**UX 架构含义：**

| UX 决策 | 架构影响 |
|---------|---------|
| Charm 生态（cobra + lipgloss + bubbletea） | Go 依赖 `github.com/charmbracelet/*`，MVP 仅用 cobra + lipgloss |
| 6 个自定义 UI 组件 | `internal/ui/` 包，组件通过 `io.Writer`，支持 TTY/Pipe/JSON |
| 三级输出 + JSON | 输出通过 Renderer 抽象，`TerminalProfile` 启动时检测 |
| 实时流式输出 | astrace 事件流（channel → 格式化 → stdout），reasonStep 逐行汇报 |
| 颜色/无色降级 | lipgloss 自动 + `NO_COLOR` / ASCII 显式回退 |

## Scale & Complexity

| 维度 | 评估 |
|------|------|
| 项目复杂度 | **高** — 范式级创新，微内核 + VFS + 进程模型 + syscall ABI |
| 主要技术域 | 系统编程（Go 运行时框架 / CLI 工具） |
| MVP 规模 | ~12 核心文件，~15 syscall，3 CLI 命令 |
| 关键外部依赖 | Claude Code CLI（唯一 LLM 通道） |
| 实时特性 | astrace 流式输出（stream-json） |
| 多租户 / 合规 | 无（单用户本地运行） |

## Technical Constraints & Dependencies

| 约束 | 来源 | 影响范围 |
|------|------|---------|
| Go 语言 | 产品简报核心决策 | 全局——goroutine=进程, channel=IPC, interface=syscall |
| Claude Code CLI | PRD LLM 驱动策略 | 驱动层——`claude -p` + `--stream-json` 核心调用模式 |
| 单二进制 | Go + 用户体验 | 部署——无外部依赖（除 Claude Code CLI） |
| ABI 向后兼容 | NFR19 | syscall 接口——15 个必须是 45 个的稳定子集 |
| Charm 生态 | UX 设计决策 | CLI 层——cobra + lipgloss + bubbles |

## Cross-Cutting Concerns Identified

| 关注点 | 影响组件 | 备注 |
|--------|---------|------|
| 错误传播与进程状态一致性 | kernel, drivers, vfs, context | LLM 超时/文件错误/Skill 失败须正确传播到状态机 |
| syscall 记录（DebugRecord） | 所有 syscall 实现 | astrace 依赖，每个 syscall 入口/出口记录 |
| 输出格式一致性 | cmd, internal/ui | 统一 Renderer + 组件体系，4 种模式 |
| goroutine 生命周期 | kernel, drivers | 退出后正确释放，防泄漏 |
| Claude Code CLI 封装 | drivers/llm | 单点封装，stream-json 解析影响 astrace |
