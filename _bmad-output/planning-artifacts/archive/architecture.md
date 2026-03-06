---
stepsCompleted: [1, 2, 3, 4, 5, 6, 7, 8]
inputDocuments:
  - '_bmad-output/planning-artifacts/product-brief-2026-02-23.md'
  - '_bmad-output/planning-artifacts/prd.md'
  - '_bmad-output/planning-artifacts/ux-design-specification.md'
  - '_bmad-output/planning-artifacts/agent-os-architecture.md'
  - '_bmad-output/planning-artifacts/sprint-change-proposal-2026-02-25.md'
  - '../docs/.meta/tool.md'
  - '../docs/.meta/idea.md'
workflowType: 'architecture'
lastStep: 8
status: 'complete'
completedAt: '2026-02-23'
updatedAt: '2026-02-25'
updateReason: 'Sprint change proposal: Agent 抽象层 + Skill 标准化'
project_name: 'Crux'
user_name: 'Decker'
date: '2026-02-23'
---

# Architecture Decision Document

_This document builds collaboratively through step-by-step discovery. Sections are appended as we work through each architectural decision together._

## Project Context Analysis

### Requirements Overview

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

### Scale & Complexity

| 维度 | 评估 |
|------|------|
| 项目复杂度 | **高** — 范式级创新，微内核 + VFS + 进程模型 + syscall ABI |
| 主要技术域 | 系统编程（Go 运行时框架 / CLI 工具） |
| MVP 规模 | ~12 核心文件，~15 syscall，3 CLI 命令 |
| 关键外部依赖 | Claude Code CLI（唯一 LLM 通道） |
| 实时特性 | astrace 流式输出（stream-json） |
| 多租户 / 合规 | 无（单用户本地运行） |

### Technical Constraints & Dependencies

| 约束 | 来源 | 影响范围 |
|------|------|---------|
| Go 语言 | 产品简报核心决策 | 全局——goroutine=进程, channel=IPC, interface=syscall |
| Claude Code CLI | PRD LLM 驱动策略 | 驱动层——`claude -p` + `--stream-json` 核心调用模式 |
| 单二进制 | Go + 用户体验 | 部署——无外部依赖（除 Claude Code CLI） |
| ABI 向后兼容 | NFR19 | syscall 接口——15 个必须是 45 个的稳定子集 |
| Charm 生态 | UX 设计决策 | CLI 层——cobra + lipgloss + bubbles |

### Cross-Cutting Concerns Identified

| 关注点 | 影响组件 | 备注 |
|--------|---------|------|
| 错误传播与进程状态一致性 | kernel, drivers, vfs, context | LLM 超时/文件错误/Skill 失败须正确传播到状态机 |
| syscall 记录（DebugRecord） | 所有 syscall 实现 | astrace 依赖，每个 syscall 入口/出口记录 |
| 输出格式一致性 | cmd, internal/ui | 统一 Renderer + 组件体系，4 种模式 |
| goroutine 生命周期 | kernel, drivers | 退出后正确释放，防泄漏 |
| Claude Code CLI 封装 | drivers/llm | 单点封装，stream-json 解析影响 astrace |

## Starter Template Evaluation

### Primary Technology Domain

**Go 系统编程 / CLI 工具 / 运行时框架。** Crux 不适用常规 Web 应用 starter，评估的是 Go 项目结构和工具链方案。

### Starter Options Considered

| 方案 | 描述 | 适合度 |
|------|------|--------|
| A: golang-standards/project-layout | 社区"标准"布局（`cmd/`, `internal/`, `pkg/`） | ⭐⭐⭐ 结构清晰但可能过度设计 |
| B: 最小平铺 + 按需增长 | 从 `main.go` 开始，随代码增长再分层 | ⭐⭐ 简单但 Crux 已知需要多模块 |
| C: 领域驱动的 OS 隐喻结构 | `cmd/crux/` + `kernel/` + `vfs/` + `drivers/` + `context/` + `skills/` + `debug/` | ⭐⭐⭐⭐ 与 OS 隐喻一致 |

### Selected Approach: 方案 C — 领域驱动的 OS 隐喻结构

**选择理由：**

1. Crux 的模块边界由 OS 隐喻天然确定（kernel、vfs、drivers、context、skills、debug），不需要从通用布局反推
2. ~12 文件结构经过充分思考，与 PRD 功能需求领域一一对应
3. Go 标准布局的 `cmd/` + `internal/` 约定叠加在此结构上

**初始化命令：**

```bash
mkdir crux && cd crux
go mod init github.com/usecrux/crux
```

### Architectural Decisions Established by Project Foundation

**Language & Runtime：**
- Go 1.26（利用 Green Tea GC、Goroutine Leak Profiler、自引用泛型等最新特性）
- 模块路径：`github.com/usecrux/crux`
- 单 `main` 入口：`cmd/crux/main.go`

**项目结构：**

```
crux/
├── cmd/crux/main.go              # CLI 入口（cobra 根命令）
├── kernel/                        # 微内核
│   ├── kernel.go                  # Kernel 结构体 + Spawn + reasonStep
│   ├── process.go                 # Process 结构体 + 生命周期状态机
│   └── reap.go                    # 进程回收（wait + orphan reparent）
├── vfs/                           # 虚拟文件系统
│   ├── vfs.go                     # VFS 接口（Open/Read/Write/Close/Stat）
│   ├── proc.go                    # /proc/{pid}/ 动态文件系统
│   └── dev.go                     # /dev/ 设备注册与路由
├── drivers/                       # 硬件抽象层
│   ├── llm/
│   │   ├── driver.go              # LLMDriver 接口 + DriverInfo
│   │   ├── claude_cli.go          # Claude Code CLI 驱动实现（MVP）
│   │   └── registry.go            # 驱动注册表：provider 路径 → 驱动实例
│   └── shell/shell.go             # Shell 执行驱动
├── context/                       # 上下文管理
│   └── context.go                 # ctx_alloc/read/write/free + prompt 组装
├── agents/                        # Agent 加载器
│   ├── types.go                   # AgentManifest + AgentModels + AgentInfo 类型定义
│   └── loader.go                  # agent.yaml 解析 + instructions.md 读取 + Skill 引用解析 + tools 聚合
├── skills/                        # Skill 加载器
│   └── loader.go                  # SKILL.md 解析（Agent Skills 标准格式）+ 渐进式加载
├── debug/                         # 调试工具
│   └── astrace.go                 # syscall 追踪（stream-json 消费）
├── internal/ui/                   # CLI 输出组件
│   ├── styles.go                  # lipgloss 样式集中定义
│   ├── progress.go                # Agent Progress Reporter
│   ├── result.go                  # Result Box
│   ├── error.go                   # Error Block
│   ├── summary.go                 # Summary Footer
│   ├── trace.go                   # Syscall Trace Line
│   └── table.go                   # Process Table
├── lib/agents/code-analyst/        # 参考 Agent 定义
│   ├── agent.yaml                 # Agent 配置：身份 + 模型 + Skill 引用
│   └── instructions.md            # Agent 角色定义 + 行为策略
├── lib/skills/code-analysis/      # 参考 Skill（Agent Skills 标准格式）
│   └── SKILL.md                   # name + description + allowed-tools + 程序性知识
├── go.mod
├── go.sum
├── Makefile                       # 构建 + 测试 + lint
└── README.md
```

**LLM 驱动层架构（多 Provider 设计）：**

VFS 挂载粒度为 **provider 级别**，模型选择通过调用参数传递：

| VFS 路径 | 驱动实现 | 阶段 |
|---------|---------|------|
| `/dev/llm/claude` | `claude_cli.go`（MVP） | MVP |
| `/dev/llm/openai` | `openai.go` | Phase 2 |
| `/dev/llm/deepseek` | OpenAI 兼容驱动 | Phase 2 |

核心接口（MVP 定义）：

```go
type LLMDriver interface {
    Call(ctx context.Context, req LLMRequest) (*LLMResponse, error)
    Stream(ctx context.Context, req LLMRequest) (<-chan StreamEvent, error)
    Info() DriverInfo
}
```

调用示例：

```go
fd, _ := k.vfs.Open("/dev/llm/claude", O_RDWR)
k.vfs.Write(fd, prompt, WriteOpts{Model: "sonnet"})
response := k.vfs.Read(fd, MaxTokens)
```

Agent manifest 映射（模型偏好属于 Agent 层）：

```yaml
# agent.yaml
models:
  provider: claude          # → /dev/llm/claude
  preferred: sonnet         # → 调用参数
  fallback: haiku           # → 失败时降级
skills:
  - code-analysis           # → 引用 Skill 名称
```

**CLI 框架：** Cobra（`github.com/spf13/cobra`）
- 根命令：`crux "意图"` — spawn 智能体（`--agent=<name>` 指定 Agent 定义）
- 子命令：`astrace`、`ps`、`kill`、`version`
- 全局 flags：`--json`、`--verbose`、`--quiet`

**样式与输出：** Charm 生态
- `github.com/charmbracelet/lipgloss` — 声明式终端样式
- `github.com/charmbracelet/bubbles` — spinner 组件
- `github.com/charmbracelet/bubbletea` — 仅 Phase 2 引入

**构建工具链：**
- `go build` / `go install` — 标准编译
- `Makefile` — `make build`、`make test`、`make lint`
- `golangci-lint` — 代码质量检查
- GoReleaser — Phase 2 公开发布时引入

**测试框架：**
- Go 标准 `testing` 包
- `github.com/stretchr/testify` — assertions 和 mocks
- 测试文件与源文件同目录（Go 惯例）

**依赖管理：** Go Modules（`go.mod`）

**Note:** 项目初始化（`go mod init` + 目录结构 + 基础 Makefile）应作为第一个实现 story。

## Core Architectural Decisions

### Decision 1: Syscall ABI 设计风格 — 分类接口组合

**决策：** Kernel 接口通过分类子接口组合（Categorized Interface Composition），而非单一巨型 `Kernel interface`。

**理由：**
- Go 惯例鼓励小接口（io.Reader, io.Writer）
- Phase 2 扩展时（15→45 syscall）新增子接口即可，不破坏现有接口
- 测试时可 mock 单个子接口，不需要 mock 整个 Kernel
- 每个子接口对应一个架构领域，边界清晰

**MVP 接口定义：**

```go
// 进程管理（5 个 syscall）
type ProcessManager interface {
    Spawn(intent string, agent AgentInfo, opts SpawnOpts) (PID, error)
    Kill(pid PID, signal Signal) error
    Wait(pid PID) (ExitStatus, error)
    GetPID() PID
    PS(filter PSFilter) ([]ProcInfo, error)
}

// 上下文管理（4 个 syscall）
type ContextManager interface {
    CtxAlloc(size int) (CtxID, error)
    CtxRead(cid CtxID, offset, length int) ([]byte, error)
    CtxWrite(cid CtxID, offset int, data []byte) error
    CtxFree(cid CtxID) error
}

// 文件系统（5 个 syscall）
type FileSystem interface {
    Open(path string, flags int) (FD, error)
    Read(fd FD, length int) ([]byte, error)
    Write(fd FD, data []byte) error
    Close(fd FD) error
    Stat(path string) (FileStat, error)
}

// 调试（1 个 syscall）
type Debugger interface {
    DebugRecord(pid PID) (RecID, error)
}

// Kernel = 所有子接口的组合
type Kernel interface {
    ProcessManager
    ContextManager
    FileSystem
    Debugger
}
```

**Phase 2 扩展路径：** 新增 `IPCManager`（Send/Recv/Pipe）、`CapManager`（CapGrant/Revoke/Check）等子接口，Kernel 接口嵌入新子接口即可。Agent Compose 通过 `agents/` 包扩展多 Agent 编排能力。

### Decision 2: 进程模型与并发

**进程数据结构：**

```go
type Process struct {
    PID       PID
    PPID      PID
    State     ProcessState          // Created → Running → Zombie → Dead
    Intent    string                // 原始意图（不可变）
    Agent     AgentInfo             // Agent 定义（身份 + 模型 + Skill 引用）
    Skills    []SkillInfo           // 已加载的 Skill 列表（从 Agent 引用解析）
    Ctx       *Context
    Children  []PID
    FDTable   map[FD]VFSFile        // 每进程文件描述符表
    DebugChan chan SyscallEvent      // astrace 事件通道
    Done      chan ExitStatus        // 进程完成信号
    cancel    context.CancelFunc    // goroutine 取消
    wg        sync.WaitGroup        // 子 goroutine 等待
}
```

**PID 分配：** `atomic.AddUint64` 全局递增，不回收。

**进程表：** `sync.RWMutex` 保护的 `map[PID]*Process`，读多写少场景。

**goroutine 生命周期管理：**
- 每个进程一个主 goroutine，通过 `context.Context` 管理取消
- `Process.wg` 跟踪所有子 goroutine（如 stream-json 读取）
- 退出时：`cancel()` → `wg.Wait()` → 状态转 Dead → 从进程表移除
- Go 1.26 Goroutine Leak Profiler 用于 NFR8 验证（`runtime/debug.SetGoroutineLeakCheck`）

### Decision 3: VFS 实现策略

**文件描述符管理：** 每进程 `FDTable map[FD]VFSFile`，FD 为进程内递增整数。

**VFSFile 接口：**

```go
type VFSFile interface {
    Read(length int) ([]byte, error)
    Write(data []byte, opts ...WriteOpt) error
    Close() error
    Stat() (FileStat, error)
}
```

**/proc 实现：** 纯内存动态生成，读取 `/proc/{pid}/status` 时从进程表实时构造 JSON，不持久化。

**/dev 设备注册：** `DeviceRegistry` 维护路径→驱动映射，`Open("/dev/llm/claude")` 时查表返回驱动封装的 `VFSFile`。

### Decision 4: Claude Code CLI 集成

**调用模式：** 每次 LLM 调用 = 一次 `exec.CommandContext`，不维持长连接。

**MVP 调用模板：**

```go
cmd := exec.CommandContext(ctx, "claude", "-p", intent,
    "--output-format", "json",
    "--system-prompt", agentInstructions + skillInstructions,
    "--model", model,
    "--max-turns", "1",
)
```

**system prompt 组装顺序：** Agent instructions（角色定义）+ 各 Skill SKILL.md body（程序性知识），由 AgentLoader 在 Spawn 时组装。

**stream-json 消费：** `--output-format stream-json` 时，通过 `bufio.Scanner` 逐行读取 stdout，每行解析为 `StreamEvent`，写入 `Process.DebugChan` 供 astrace 消费。

**超时处理：** `context.WithTimeout` 包装，超时后 `cmd.Process.Kill()`，进程转 Zombie。

### Decision 5: 调试架构（astrace）

**事件传递机制：** 每进程一个带缓冲的 `DebugChan chan SyscallEvent`。

**SyscallEvent 结构：**

```go
type SyscallEvent struct {
    Timestamp time.Duration       // 相对进程启动时间
    PID       PID
    Syscall   string              // "Open", "Read", "CtxWrite" 等
    Args      map[string]any      // syscall 参数
    Result    any                 // 返回值
    Err       error               // 错误（如有）
    Duration  time.Duration       // syscall 耗时
}
```

**数据流：** syscall 实现入口/出口 → 构造 SyscallEvent → 写入 DebugChan → astrace goroutine 消费 → 格式化输出到终端。

**无 astrace 时：** DebugChan 为 nil，跳过事件记录（零开销）。

### Decision 6: 错误处理与恢复

**SyscallError 结构：**

```go
type SyscallError struct {
    Syscall  string       // 出错的 syscall 名称
    PID      PID          // 所属进程
    Device   string       // 涉及的设备路径（如 /dev/llm/claude）
    Err      error        // 底层错误
    Code     ErrCode      // 错误分类码
}

func (e *SyscallError) Error() string {
    return fmt.Sprintf("[%s] PID %d %s: %s (%v)", e.Code, e.PID, e.Syscall, e.Device, e.Err)
}
```

**错误传播层次：**

```
Driver 层错误（如 CLI 超时）
  → 包装为 SyscallError（带设备路径和错误码）
    → 传播到 reasonStep（决定是否重试或终止）
      → 传播到进程状态机（Running → Zombie）
        → 传播到 CLI 输出（Error Block 组件格式化）
```

**ErrCode 分类：**
- `ErrTimeout` — LLM/Shell 超时
- `ErrNotFound` — VFS 路径不存在
- `ErrPermission` — Skill allowed-tools 白名单拒绝
- `ErrInternal` — 内核内部错误
- `ErrDriver` — 驱动层错误

### Decision 7: Agent 抽象层与 Skill 标准化

**决策：** 引入 Agent 抽象层，将原有 Skill 的职责拆分为 Agent（智能体定义）和 Skill（能力模块）两层，且 Skill 格式遵循 Agent Skills 行业标准（agentskills.io）。

**背景：** 原设计中 Skill（manifest.yaml + instructions.md）同时承担了"智能体定义"和"能力模块"双重职责。SkillManifest 包含 Models（模型偏好）和 ContextBudget（上下文预算），这些是智能体级别的配置，不应属于共享库。同时，Agent Skills 开放标准（由 Anthropic 发起，30+ AI 工具采用）定义了 Skill 的标准格式（SKILL.md），Crux 原有的双文件格式无法与生态互操作。

**理由：**
1. 概念清晰度——Agent 定义"我是谁"（身份、角色、策略、模型偏好），Skill 定义"如何做 X"（程序性知识、工具权限）
2. 与 PRD Phase 2 Compose 设计（`agents: reviewer: skills: [pr-reviewer]`）自然对齐
3. Skill 遵循行业标准后可跨平台复用（与 30+ AI 工具生态兼容）
4. 渐进式加载策略减少启动时上下文消耗（~100 tokens/skill 发现阶段）

**架构层次（OS 隐喻对应）：**

```
Process（运行时实例）= 进程
  ← Agent（智能体定义）= 可执行程序
      ← Skill(s)（能力模块）= 共享库
```

**Agent 定义（Crux 特有）：**

```
lib/agents/code-analyst/
├── agent.yaml        # Agent 配置：身份 + 模型 + Skill 引用
└── instructions.md   # Agent 角色定义 + 行为策略
```

```go
// agents/types.go
type AgentManifest struct {
    Name          string       `yaml:"name"`
    Description   string       `yaml:"description"`
    Models        AgentModels  `yaml:"models"`
    ContextBudget int          `yaml:"context_budget"`
    Skills        []string     `yaml:"skills"`
}

type AgentModels struct {
    Provider  string `yaml:"provider"`
    Preferred string `yaml:"preferred"`
    Fallback  string `yaml:"fallback"`
}

type AgentInfo struct {
    Manifest     AgentManifest
    Instructions string        // instructions.md 内容
    Skills       []SkillInfo   // 已解析的 Skill 列表
    AllowedTools []string      // 聚合所有 Skill 的 allowed-tools
}
```

**Skill 定义（Agent Skills 行业标准）：**

```
lib/skills/code-analysis/
├── SKILL.md          # 必需：标准格式（YAML frontmatter + Markdown 指令）
├── scripts/          # 可选：可执行脚本
├── references/       # 可选：参考文档
└── assets/           # 可选：模板、资源
```

```go
// skills/types.go
type SkillManifest struct {
    Name          string            `yaml:"name"`
    Description   string            `yaml:"description"`
    AllowedTools  string            `yaml:"allowed-tools"`  // 空格分隔工具列表
    Metadata      map[string]string `yaml:"metadata"`
    Compatibility string            `yaml:"compatibility"`
    License       string            `yaml:"license"`
}

type SkillInfo struct {
    Manifest     SkillManifest
    Instructions string  // SKILL.md body 内容（激活阶段加载）
    Dir          string  // Skill 目录路径
}
```

**Agent vs Skill 职责分离：**

| 维度 | Agent | Skill |
|------|-------|-------|
| 定义 | "我是谁"——身份、角色、策略 | "如何做 X"——程序性知识、工作流 |
| 模型偏好 | ✅ models | ❌ |
| 上下文预算 | ✅ context_budget | ❌ |
| 设备权限 | ❌ 由引用的 Skill 聚合 | ✅ allowed-tools |
| 复用性 | 特定角色 | 跨 Agent 共享，跨平台兼容 |
| 标准 | Crux 特有 | Agent Skills 行业标准 |

**渐进式加载（Progressive Disclosure）：**

1. **发现**（启动时）：扫描 skill 目录，仅解析 SKILL.md frontmatter → ~100 tokens/skill
2. **激活**（任务匹配时）：加载完整 SKILL.md body → < 5000 tokens
3. **执行**（按需）：加载 scripts/、references/、assets/ 中的文件

**Spawn 流程（更新）：**

```
crux "分析代码" --agent=code-analyst

1. AgentLoader 加载 lib/agents/code-analyst/agent.yaml
   → 获取 models、context_budget、skills 引用列表
2. AgentLoader 加载 lib/agents/code-analyst/instructions.md
   → 获取 Agent 角色系统提示
3. SkillLoader 加载每个引用的 Skill（渐进式：激活阶段加载 SKILL.md body）
4. 聚合所有 Skill 的 allowed-tools → 设备权限白名单
5. 组装 system prompt = Agent instructions + Skill instructions
6. Spawn Process（使用 agent 的 model 偏好 + 聚合的权限白名单）
```

**MCP Phase 2 兼容性：** Agent manifest 可扩展支持 `mcp` 字段引用 MCP 服务器，Skill `allowed-tools`（`/dev/`）与 MCP 路径（`/mnt/mcp/`）命名空间分离，无冲突。

### Go 1.26 特性利用

| 特性 | Crux 使用场景 |
|------|-------------|
| **Green Tea GC**（默认启用） | 自动受益——astrace 高吞吐事件流的 GC 压力降低 |
| **Goroutine Leak Profiler**（实验性） | 验证 NFR8（进程退出后 goroutine 正确释放），集成到测试和开发调试 |
| **`new(expr)` 表达式初始化** | 简化结构体创建（如 `new(Process{PID: pid, State: Created})`） |
| **自引用泛型** | 类型安全的注册表模式（如 `Registry[T]`、`Future[T]`） |
| **`go fix` 现代化** | 自动迁移到最新 API 模式 |

### 泛型策略

**设计原则：** 适合使用泛型的模块尽可能用泛型，减少类型断言和 `interface{}` 使用。

**已识别的泛型应用场景：**

| 模块 | 泛型应用 | 说明 |
|------|---------|------|
| 注册表 | `Registry[T]` | 设备注册表、驱动注册表、Skill 注册表统一为泛型实现 |
| Future | `Future[T]` | syscall 异步返回值的类型安全封装 |
| 进程表 | `SyncMap[K, V]` | 替代 `sync.RWMutex + map[PID]*Process` |
| 事件通道 | `Chan[T]` | 带缓冲管理和零值安全的泛型 channel 封装 |
| VFS 结果 | `Result[T]` | 统一 (T, error) 返回模式，支持链式操作 |
| 配置解析 | `Loader[T]` | agent.yaml 和配置文件的泛型反序列化 |

**核心泛型类型预览：**

```go
// 类型安全的注册表——设备、驱动、Skill 共用
type Registry[T any] struct {
    mu    sync.RWMutex
    items map[string]T
}

func (r *Registry[T]) Register(name string, item T) error { ... }
func (r *Registry[T]) Get(name string) (T, bool) { ... }
func (r *Registry[T]) List() []T { ... }

// 异步 syscall 返回值
type Future[T any] struct {
    ch   chan result[T]
    once sync.Once
}

func (f *Future[T]) Await() (T, error) { ... }
func (f *Future[T]) Then(fn func(T)) *Future[T] { ... }

// 类型安全的并发 Map
type SyncMap[K comparable, V any] struct {
    mu sync.RWMutex
    m  map[K]V
}

func (s *SyncMap[K, V]) Load(key K) (V, bool) { ... }
func (s *SyncMap[K, V]) Store(key K, value V) { ... }
func (s *SyncMap[K, V]) Delete(key K) { ... }
func (s *SyncMap[K, V]) Range(fn func(K, V) bool) { ... }

// Result 类型——简化错误处理链
type Result[T any] struct {
    value T
    err   error
}

func Ok[T any](v T) Result[T] { return Result[T]{value: v} }
func Err[T any](err error) Result[T] { return Result[T]{err: err} }
func (r Result[T]) Unwrap() (T, error) { return r.value, r.err }
func (r Result[T]) Map(fn func(T) T) Result[T] { ... }
```

## Implementation Patterns & Consistency Rules

### Pattern Categories Defined

**已识别 6 大类 25 个潜在冲突点**——以下模式确保多 AI Agent 协同编码时写出兼容、一致的代码。

### 命名模式（Naming Patterns）

**Go 代码命名：**

| 对象 | 规则 | 示例 | 反例 |
|------|------|------|------|
| 包名 | 全小写单词，不用下划线 | `kernel`, `vfs`, `context` | `Kernel`, `my_pkg` |
| 导出类型 | PascalCase | `Process`, `SyscallEvent`, `LLMDriver` | `process`, `syscall_event` |
| 非导出类型 | camelCase | `pidCounter`, `fdTable` | `pid_counter` |
| 导出函数 | PascalCase 动词开头 | `Spawn()`, `CtxAlloc()`, `Open()` | `spawn()`, `ctx_alloc()` |
| 接口 | 名词或 `-er` 后缀 | `FileSystem`, `Debugger`, `LLMDriver` | `IFileSystem`, `DebuggerInterface` |
| 常量 | PascalCase（导出）或 camelCase（非导出） | `MaxTokens`, `ErrTimeout` | `MAX_TOKENS`, `ERR_TIMEOUT` |
| 错误变量 | `Err` 前缀 | `ErrNotFound`, `ErrTimeout` | `NotFoundError`, `TimeoutErr` |
| 泛型类型参数 | 单字母大写或语义短词 | `T`, `K`, `V`, `Item` | `Type`, `TKey` |

**Syscall 命名：**

| 规则 | 示例 | 说明 |
|------|------|------|
| PascalCase 动词 | `Spawn`, `Kill`, `Wait` | 进程管理类 |
| `Ctx` 前缀 + PascalCase | `CtxAlloc`, `CtxRead`, `CtxWrite`, `CtxFree` | 上下文类 |
| Unix 风格动词 | `Open`, `Read`, `Write`, `Close`, `Stat` | 文件系统类 |
| `Debug` 前缀 | `DebugRecord` | 调试类 |

**VFS 路径命名：**

| 路径段 | 规则 | 示例 |
|--------|------|------|
| 顶级目录 | 全小写 Unix 风格 | `/proc/`, `/dev/`, `/lib/skills/` |
| 设备名 | 全小写，连字符分隔 | `/dev/llm/claude`, `/dev/shell` |
| PID 段 | 纯数字 | `/proc/42/status` |
| Skill 名 | 全小写，连字符分隔 | `/lib/skills/code-analysis/` |
| Agent 名 | 全小写，连字符分隔 | `/lib/agents/code-analyst/` |

**文件与目录命名：**

| 对象 | 规则 | 示例 |
|------|------|------|
| Go 源文件 | 全小写，下划线分隔 | `kernel.go`, `claude_cli.go`, `astrace.go` |
| 测试文件 | `_test.go` 后缀，同目录 | `kernel_test.go`, `claude_cli_test.go` |
| YAML 配置 | 全小写，连字符分隔，`.yaml` 后缀 | `agent.yaml`（不用 `.yml`） |
| SKILL.md | 大写固定名 | `SKILL.md`（Agent Skills 标准要求） |
| Markdown | 全小写，连字符分隔 | `instructions.md` |
| 目录名 | 全小写单词 | `kernel/`, `drivers/`, `internal/ui/` |

### 结构模式（Structure Patterns）

**依赖方向（严格单向）：**

```
cmd/ → kernel/ → vfs/     → drivers/
                → context/
                → agents/  → skills/
cmd/ → debug/  → kernel/（仅类型）
cmd/ → internal/ui/
```

**禁止的依赖：**
- `kernel/` 不导入 `cmd/` 或 `internal/ui/`
- `vfs/` 不导入 `kernel/`（通过接口解耦）
- `drivers/` 不导入 `kernel/`（通过接口解耦）
- `skills/` 不导入 `agents/`（单向：agents → skills）
- 任何包不导入 `cmd/crux/`

**文件组织规则：**

| 规则 | 说明 |
|------|------|
| 每文件单一职责 | `kernel.go` = Kernel 结构体 + Spawn + reasonStep；`process.go` = Process + 状态机 |
| 接口定义在使用方 | `LLMDriver` 接口定义在 `drivers/llm/driver.go` |
| 共享类型独立文件 | PID, FD, ErrCode 等放在 `kernel/types.go` |
| 测试同目录 | `kernel/kernel_test.go` 与 `kernel/kernel.go` 同目录 |
| 内部包隔离 | UI 组件放 `internal/ui/`，不可被外部导入 |

### 格式模式（Format Patterns）

**JSON 输出格式（`--json` flag）：**

```go
// 统一 JSON 响应包装（泛型）
type JSONResponse[T any] struct {
    OK    bool       `json:"ok"`
    Data  T          `json:"data,omitempty"`
    Error *JSONError `json:"error,omitempty"`
}

type JSONError struct {
    Code    string `json:"code"`
    Message string `json:"message"`
    Syscall string `json:"syscall,omitempty"`
    Device  string `json:"device,omitempty"`
}
```

**JSON 字段命名：** 全部 `snake_case`（Go 结构体用 PascalCase + json tag）

**时间格式：**
- JSON 中：毫秒整数（`elapsed_ms: 6200`）
- 终端显示：人类可读（`6.2s`、`100ms`）
- 日志中：RFC3339（`2026-02-23T14:30:00Z`）

**agent.yaml 格式：** 字段名全小写 `snake_case`，列表用序列语法 `- item`，缩进 2 空格。

**SKILL.md 格式（Agent Skills 标准）：** YAML frontmatter（`---` 包裹）+ Markdown body。frontmatter 字段名全小写连字符分隔（`allowed-tools`），body 为程序性知识指令。

### 通信模式（Communication Patterns）

**SyscallEvent 事件命名：** Syscall 字段值与接口方法名完全一致（`"Spawn"`, `"Open"`, `"CtxWrite"`）。

**astrace 输出格式（终端）：**

```
[  0.000] Spawn("分析代码", agent="code-analyst")       = PID(1)        12ms
[  0.012] CtxAlloc(4096)                          = CtxID(1)       0ms
[  0.013] Open("/dev/llm/claude", O_RDWR)         = FD(3)          1ms
```

**日志格式（logfmt 风格）：**

```
[kernel] level=info msg="process spawned" pid=1 intent="分析代码" agent="code-analyst" skills=["code-analysis"]
[kernel] level=error msg="llm timeout" pid=1 device="/dev/llm/claude" elapsed=30s
```

级别：`debug`, `info`, `warn`, `error`。前缀：`[组件名]`。

**Channel 使用规则：**

| 规则 | 说明 |
|------|------|
| DebugChan 缓冲 256 | 防止 syscall 阻塞在写入 |
| Done 缓冲 1 | 确保写入不阻塞 |
| nil channel 检查 | 写入前 `if ch != nil`，零开销跳过 |
| 关闭责任在生产者 | DebugChan 由进程退出时关闭 |

### 过程模式（Process Patterns）

**错误处理模式：**

```go
// ✅ 正确：包装为 SyscallError
func (k *KernelImpl) Open(path string, flags int) (FD, error) {
    file, err := k.devRegistry.Get(path)
    if err != nil {
        return 0, &SyscallError{Syscall: "Open", PID: k.currentPID(), Device: path, Err: err, Code: ErrNotFound}
    }
    // ...
}

// ❌ 错误：丢失上下文
func (k *KernelImpl) Open(path string, flags int) (FD, error) {
    file, err := k.devRegistry.Get(path)
    if err != nil {
        return 0, err  // 丢失 syscall 名称、PID、设备路径
    }
}
```

**context.Context 传播规则：**

| 规则 | 说明 |
|------|------|
| Kernel 方法不接受 ctx 参数 | 使用 `Process.cancel` 控制生命周期 |
| Driver 方法接受 ctx 参数 | `LLMDriver.Call(ctx, req)` 支持取消 |
| 外部调用必须带 timeout | `exec.CommandContext(ctx)` 中 ctx 必须有 deadline |
| cancel() 后等待 wg.Done() | 确保所有子 goroutine 退出后再转 Dead |

**进程状态转移规则：**

```
合法转移：
  Created  → Running    （reasonStep 开始执行）
  Running  → Zombie     （正常完成 / 错误 / 超时 / kill）
  Zombie   → Dead       （wait 回收 + 资源释放）

非法转移（禁止）：
  Running  → Created
  Zombie   → Running
  Dead     → 任何状态
```

**资源释放顺序：** cancel() → wg.Wait() → 关闭 FD → 关闭 DebugChan → CtxFree → 状态转 Dead → 移除进程表。

### 泛型使用模式（Generics Patterns）

| 场景 | 用泛型 | 说明 |
|------|--------|------|
| Registry | ✅ | 设备、驱动、Agent、Skill 共享泛型 `Registry[T]` |
| 并发 Map | ✅ | `SyncMap[K, V]` 替代 `sync.Mutex + map` |
| Future/Result | ✅ | 类型安全异步返回和错误处理链 |
| JSON 响应 | ✅ | `JSONResponse[T]` |
| 配置加载 | ✅ | `LoadYAML[T](path) (T, error)` agent.yaml 和配置文件反序列化；`ParseSKILLMD(path)` SKILL.md frontmatter 解析 |
| Kernel 接口 | ❌ | 方法签名固定 |
| Process 结构体 | ❌ | 单一具体类型 |
| SyscallEvent | ❌ | 需运行时类型灵活性 |

**泛型命名：** 领域类型用语义参数名（`Registry[Item]`, `SyncMap[K, V]`），通用工具允许 `T`。

### 强制执行指南

**所有 AI Agent 必须遵循：**

1. 严格遵循命名表，不自创格式
2. 禁止反向依赖（`golangci-lint` 检查）
3. 所有 syscall 实现必须返回 `*SyscallError`，不允许裸 error
4. 所有 syscall 入口/出口必须写入 SyscallEvent（DebugChan 非 nil 时）
5. 进程状态转移必须遵循合法转移表
6. 资源释放必须按指定顺序
7. 注册表、并发 Map、Future、JSON 包装必须用泛型
8. 所有 JSON 输出字段用 snake_case

**模式验证：** `go vet` + `golangci-lint` + `go test -race` + 代码审查 checklist。

## Project Structure & Boundaries

### 完整项目目录结构

```
crux/
├── cmd/crux/
│   └── main.go                           # CLI 入口：cobra 根命令 + 子命令注册
│
├── kernel/
│   ├── types.go                          # 共享类型：PID, FD, CtxID, ErrCode, Signal, ProcessState
│   ├── errors.go                         # SyscallError + ErrCode 常量 + error 辅助函数
│   ├── kernel.go                         # KernelImpl 结构体 + Spawn() + reasonStep() 循环
│   ├── process.go                        # Process 结构体 + 状态机转移 + 生命周期方法
│   ├── reap.go                           # Wait() + 孤儿 reparent + Zombie→Dead 回收
│   ├── generics.go                       # SyncMap[K,V] + Registry[T] + Future[T] + Result[T]
│   ├── kernel_test.go                    # Kernel 单元测试（Spawn/reasonStep/Kill）
│   ├── process_test.go                   # 状态机转移测试
│   ├── reap_test.go                      # Wait/orphan/zombie 回收测试
│   └── generics_test.go                  # 泛型工具测试
│
├── vfs/
│   ├── vfs.go                            # VFSFile 接口 + VFS 结构体 + Open/Read/Write/Close/Stat
│   ├── proc.go                           # ProcFS：/proc/{pid}/status|intent|context 动态生成
│   ├── dev.go                            # DeviceRegistry：/dev/ 路径注册与路由
│   ├── vfs_test.go                       # VFS 集成测试
│   ├── proc_test.go                      # ProcFS 单元测试
│   └── dev_test.go                       # 设备注册路由测试
│
├── drivers/
│   ├── llm/
│   │   ├── driver.go                     # LLMDriver 接口 + LLMRequest/LLMResponse + StreamEvent + DriverInfo
│   │   ├── claude_cli.go                 # ClaudeCliDriver：exec.Command 封装 + stream-json 解析
│   │   ├── registry.go                   # LLM 驱动注册表（基于 Registry[LLMDriver]）
│   │   ├── claude_cli_test.go            # Claude CLI 驱动测试（mock exec.Command）
│   │   └── registry_test.go             # 驱动注册表测试
│   └── shell/
│       ├── shell.go                      # ShellDriver：exec.Command 封装 + stdout/stderr 捕获
│       └── shell_test.go                # Shell 驱动测试
│
├── context/
│   ├── context.go                        # Context 结构体 + Alloc/Read/Write/Free + BuildPrompt()
│   └── context_test.go                   # 上下文管理测试
│
├── agents/
│   ├── types.go                          # AgentManifest + AgentModels + AgentInfo 类型定义
│   ├── loader.go                         # AgentLoader：加载 agent.yaml + instructions.md + 解析 Skill 引用 + 聚合 tools
│   └── loader_test.go                    # Agent 加载器测试
│
├── skills/
│   ├── loader.go                         # SkillLoader：解析 SKILL.md（Agent Skills 标准格式）+ 渐进式加载（metadata-only / full）
│   ├── types.go                          # SkillManifest（Name/Description/AllowedTools/Metadata）+ SkillInfo 类型定义
│   ├── loader_test.go                    # Skill 加载测试
│   └── testdata/                         # 测试用 Skill fixtures
│       └── mock-skill/
│           └── SKILL.md
│
├── debug/
│   ├── astrace.go                        # Astrace：消费 DebugChan + 格式化输出 + stream-json 事件桥接
│   ├── event.go                          # SyscallEvent 结构体 + 事件记录辅助函数
│   ├── astrace_test.go                   # astrace 格式化输出测试
│   └── event_test.go                     # 事件记录测试
│
├── internal/
│   └── ui/
│       ├── renderer.go                   # Renderer 接口 + TerminalProfile 检测 + 输出模式切换
│       ├── styles.go                     # lipgloss 全局样式定义（颜色、间距、边框）
│       ├── progress.go                   # ProgressReporter：reasonStep 实时进度（spinner + 步骤）
│       ├── result.go                     # ResultBox：最终结果框（带边框的输出区域）
│       ├── error.go                      # ErrorBlock：错误输出（设备路径 + 错误码 + 消息）
│       ├── summary.go                    # SummaryFooter：退出汇总（PID + exit code + tokens + elapsed）
│       ├── trace.go                      # TraceLine：astrace 单行格式化
│       └── table.go                      # ProcessTable：ps 命令表格输出
│
├── lib/agents/code-analyst/
│   ├── agent.yaml                       # Agent 配置：name + description + models + context_budget + skills 引用
│   └── instructions.md                  # Agent 角色定义 + 行为策略
│
├── lib/skills/code-analysis/
│   └── SKILL.md                         # Agent Skills 标准格式：frontmatter（name/description/allowed-tools）+ 程序性知识
│
├── go.mod                                # 模块：github.com/usecrux/crux, go 1.26
├── go.sum
├── Makefile                              # build / test / lint / install 目标
├── .golangci.yml                         # golangci-lint 配置
├── .gitignore
└── README.md
```

### 架构边界

**Kernel ↔ VFS 边界：**

```go
// Kernel 持有 VFS 实例，通过 VFS 接口访问所有设备
type KernelImpl struct {
    vfs       *vfs.VFS
    procTable *SyncMap[PID, *Process]
}

// VFS 不知道 Kernel 的存在
// ProcFS 通过初始化时注入的 ProcessInfoProvider 接口读取进程信息
type ProcessInfoProvider interface {
    GetProcInfo(pid PID) (ProcInfo, error)
    ListProcs() []ProcInfo
}
```

**VFS ↔ Drivers 边界：**

```go
// 驱动实现 VFSFile 接口，通过 DeviceRegistry 注册
type DeviceRegistry struct {
    devices Registry[VFSFileFactory]
}

type VFSFileFactory func() (VFSFile, error)

// 注册在 cmd/crux/main.go 初始化阶段完成（依赖注入）
devRegistry.Register("/dev/llm/claude", claudeDriver.FileFactory())
```

**Kernel ↔ Context 边界：** Context 包独立于 Kernel，通过 CtxID 引用，不持有 Process 指针。

**Kernel ↔ Agents 边界：** Kernel 通过 AgentLoader 获取 AgentInfo（包含 Agent 指令、模型偏好、聚合的设备权限白名单），Kernel 负责注入 CLI 参数。Agents 包不导入 Kernel。

**Agents ↔ Skills 边界：** AgentLoader 依赖 SkillLoader 加载引用的 Skill。AgentLoader 从 agent.yaml 读取 `skills` 列表，调用 SkillLoader 逐一解析 SKILL.md，聚合所有 Skill 的 `allowed-tools` 为统一权限白名单。单向依赖：`agents/ → skills/`。

**Skills 独立性：** SkillLoader 仅负责解析 SKILL.md 格式（frontmatter + body），返回 SkillManifest + instructions 内容。Skills 包不导入 Agents 或 Kernel，可独立使用。

**Debug ↔ Kernel 边界：** Debug 包仅导入 `kernel/` 的类型（SyscallEvent, PID），通过 DebugChan channel 消费事件。

**cmd/ 依赖注入点：** `cmd/crux/main.go` 是唯一组装点，负责创建所有实例、注册设备、连接组件。

### 需求到结构映射

| FR 领域 | FR 范围 | 核心文件 |
|---------|---------|---------|
| 智能体生命周期 | FR1-FR7 | `kernel/{kernel,process,reap}.go` |
| 智能体推理 | FR8-FR12 | `kernel/kernel.go`, `drivers/llm/claude_cli.go` |
| 文件系统与资源 | FR13-FR18 | `vfs/{vfs,proc,dev}.go`, `drivers/shell/shell.go` |
| 上下文管理 | FR19-FR22 | `context/context.go` |
| Agent 管理 | FR23-FR25 | `agents/{types,loader}.go`, `lib/agents/code-analyst/` |
| Skill 管理 | FR25a-FR27 | `skills/{loader,types}.go`, `lib/skills/code-analysis/` |
| 调试与可观测 | FR28-FR32 | `debug/{astrace,event}.go` |
| CLI | FR33-FR37 | `cmd/crux/main.go`, `internal/ui/*` |
| 文档 | FR38-FR40 | `README.md` |

**跨切关注点映射：**

| 关注点 | 定义 → 记录 → 消费 |
|--------|-------------------|
| 错误传播 | `kernel/errors.go` → 所有 syscall → `internal/ui/error.go` |
| SyscallEvent | `debug/event.go` → `kernel/kernel.go` → `debug/astrace.go` |
| 输出格式 | `internal/ui/renderer.go` → `internal/ui/*.go` |
| goroutine 生命周期 | `kernel/process.go` → `kernel/reap.go` |
| 泛型工具 | `internal/xsync/` → 所有使用 Registry/SyncMap/Future 的模块 |

### 数据流

**核心端到端流（`crux "分析代码" --agent=code-analyst`）：**

```
用户输入 → cmd/ 解析意图 + --agent 参数
  → agents/ 加载 Agent（agent.yaml + instructions.md）
    → skills/ 加载引用的 Skill（SKILL.md 渐进式：frontmatter → body）
      → 聚合 Skill allowed-tools → 设备权限白名单
  → kernel/ Spawn 创建 Process（AgentInfo + 聚合权限）
    → context/ 分配上下文
    → 组装 system prompt = Agent instructions + Skill instructions
    → reasonStep 循环:
      → vfs/ Open → dev/ 路由 → drivers/ 执行 → 返回结果
      → context/ 追加结果 → 继续循环或完成
    → reap 回收 → ui/ 格式化输出 → stdout
```

**astrace 数据流：**

```
syscall 入口 → debug/event 构造 SyscallEvent
  → Process.DebugChan → debug/astrace 消费
    → internal/ui/trace 格式化 → stdout 实时流式
```

### 开发工作流

**Makefile 目标：** `build` / `install` / `test`（-race） / `lint` / `vet` / `clean` / `all`

**测试策略：**

| 层次 | 方法 |
|------|------|
| 单元测试 | `*_test.go` 同目录，接口 mock |
| 集成测试 | VFS + DeviceRegistry + mock 驱动 |
| 竞态检测 | `go test -race` 默认开启 |
| 泄漏检测 | Go 1.26 Goroutine Leak Profiler |

**依赖注入（cmd/crux/main.go）：**

```go
func main() {
    // 1. VFS + 设备注册表
    devReg := vfs.NewDeviceRegistry()
    vfsInst := vfs.New(devReg)
    // 2. 驱动注册
    devReg.Register("/dev/llm/claude", llm.NewClaudeCliDriver().FileFactory())
    devReg.Register("/dev/shell", shell.NewDriver().FileFactory())
    devReg.Register("/dev/fs", fs.NewHostFSDriver().FileFactory())
    // 3. Skill + Agent 加载器
    skillLoader := skills.NewLoader("./lib/skills")
    agentLoader := agents.NewLoader("./lib/agents", skillLoader)
    // 4. 内核
    kernel := kernel.New(vfsInst, context.NewManager(), agentLoader)
    // 5. CLI（--agent flag 由 cobra 注册）
    cmd.Execute(kernel)
}
```

## Architecture Validation Results

### 一致性验证 ✅

**决策兼容性：**

| 检查项 | 结果 |
|--------|------|
| Go 1.26 + Cobra + Lipgloss + testify | ✅ Go 生态内完全兼容 |
| 分类接口组合 + VFS + Drivers | ✅ 接口边界清晰，组合无冲突 |
| exec.Command + stream-json | ✅ Claude Code CLI 调用模式与 astrace 数据源一致 |
| 泛型类型 + Go 1.26 | ✅ Registry[T], SyncMap[K,V], Future[T] 均支持 |
| SyscallError + DebugChan | ✅ 共享 syscall 边界，传播路径一致 |
| Agent/Skill 分层 + Agent Skills 标准 | ✅ Agent 定义身份+策略，Skill 定义程序性知识+工具权限，职责清晰分离 |

**模式一致性：** ✅ 命名、JSON 格式、错误处理模式全部对齐。

**结构对齐：** ✅ 验证中发现的 3 个结构问题已修正（见下方）。

### 需求覆盖验证

**FR 覆盖：42/42** ✅

| FR 领域 | FR 范围 | 架构支撑 |
|---------|---------|---------|
| 智能体生命周期 | FR1-FR7 | `kernel/{kernel,process,reap}.go` |
| 智能体推理 | FR8-FR12 | `kernel/kernel.go` + `drivers/llm/claude_cli.go` |
| 文件系统与资源 | FR13-FR18 | `vfs/{vfs,proc,dev}.go` + `drivers/{fs,shell,llm}/` |
| 上下文管理 | FR19-FR22 | `context/context.go` |
| Agent 管理 | FR23-FR25 | `agents/{types,loader}.go` + `lib/agents/code-analyst/` |
| Skill 管理 | FR25a-FR27 | `skills/{loader,types}.go` + `lib/skills/code-analysis/` |
| 调试与可观测 | FR28-FR32 | `debug/{astrace,event}.go` |
| CLI | FR33-FR37 | `cmd/crux/main.go` + `internal/ui/*` |
| 文档 | FR38-FR40 | `README.md` |

**NFR 覆盖：20/20** ✅

| NFR 类别 | 架构支撑 |
|---------|---------|
| NFR1-5 性能 | 内存 SyncMap、缓冲 DebugChan、context.WithTimeout |
| NFR6-10 可靠性 | -race 测试、cancel+wg 生命周期、Goroutine Leak Profiler |
| NFR11-14 集成 | claude_cli.go 参数映射、stream-json、os.Open 继承权限 |
| NFR15-17 安全 | Agent 聚合 Skill allowed-tools 权限白名单 |
| NFR18-20 可维护性 | golangci-lint、分类接口可扩展、驱动层单模块封装 |

### Gap Analysis 与修正

**🔴 已修正问题 1：泛型工具包位置违反依赖方向**

原设计将 `Registry[T]`、`SyncMap[K,V]` 等放在 `kernel/generics.go`，导致 `vfs/` 和 `drivers/` 反向依赖 `kernel/`。

**修正：** 移至 `internal/xsync/` 独立包，所有包均可导入。

**🔴 已修正问题 2：缺少 `/dev/fs` 宿主文件系统驱动**

FR16 要求通过 `/dev/fs` 读取宿主文件系统，但项目结构中缺少 `drivers/fs/` 目录。

**修正：** 新增 `drivers/fs/{hostfs.go, hostfs_test.go}`。

**🔴 已修正问题 3：共享类型 `kernel/types.go` 导致反向依赖**

`PID`、`FD`、`CtxID` 等在 `kernel/` 中，但 `vfs/`、`debug/`、`context/` 也需使用。

**修正：** 移至 `internal/types/types.go`。

### 修正后的完整项目结构（最终版）

```
crux/
├── cmd/crux/
│   └── main.go                           # CLI 入口：cobra 根命令 + 子命令注册
│
├── internal/
│   ├── types/
│   │   └── types.go                      # PID, FD, CtxID, ErrCode, Signal, ProcessState, SyscallEvent
│   ├── xsync/
│   │   ├── syncmap.go                    # SyncMap[K, V]
│   │   ├── registry.go                   # Registry[T]
│   │   ├── future.go                     # Future[T] + Result[T]
│   │   └── syncmap_test.go
│   └── ui/
│       ├── renderer.go                   # Renderer 接口 + TerminalProfile + 输出模式切换
│       ├── styles.go                     # lipgloss 全局样式
│       ├── progress.go                   # Agent Progress Reporter
│       ├── result.go                     # Result Box
│       ├── error.go                      # Error Block
│       ├── summary.go                    # Summary Footer
│       ├── trace.go                      # Syscall Trace Line
│       └── table.go                      # Process Table
│
├── kernel/
│   ├── errors.go                         # SyscallError + ErrCode 常量
│   ├── kernel.go                         # KernelImpl + Spawn() + reasonStep()
│   ├── process.go                        # Process + 状态机
│   ├── reap.go                           # Wait + orphan reparent + zombie 回收
│   ├── kernel_test.go
│   ├── process_test.go
│   └── reap_test.go
│
├── vfs/
│   ├── vfs.go                            # VFSFile 接口 + VFS + Open/Read/Write/Close/Stat
│   ├── proc.go                           # ProcFS：/proc/{pid}/ 动态生成
│   ├── dev.go                            # DeviceRegistry：/dev/ 注册与路由
│   ├── vfs_test.go
│   ├── proc_test.go
│   └── dev_test.go
│
├── drivers/
│   ├── llm/
│   │   ├── driver.go                     # LLMDriver 接口 + LLMRequest/Response + StreamEvent
│   │   ├── claude_cli.go                 # ClaudeCliDriver：exec.Command + stream-json
│   │   ├── registry.go                   # LLM 驱动注册表
│   │   ├── claude_cli_test.go
│   │   └── registry_test.go
│   ├── shell/
│   │   ├── shell.go                      # ShellDriver：exec.Command 封装
│   │   └── shell_test.go
│   └── fs/
│       ├── hostfs.go                     # HostFSDriver：os.Open/Read 封装
│       └── hostfs_test.go
│
├── context/
│   ├── context.go                        # Context + Alloc/Read/Write/Free + BuildPrompt
│   └── context_test.go
│
├── agents/
│   ├── types.go                          # AgentManifest + AgentModels + AgentInfo
│   ├── loader.go                         # AgentLoader：agent.yaml + instructions.md + Skill 引用解析 + tools 聚合
│   └── loader_test.go
│
├── skills/
│   ├── loader.go                         # SkillLoader：SKILL.md 解析（Agent Skills 标准）+ 渐进式加载
│   ├── types.go                          # SkillManifest（Name/Description/AllowedTools/Metadata）+ SkillInfo
│   ├── loader_test.go
│   └── testdata/mock-skill/
│       └── SKILL.md
│
├── debug/
│   ├── astrace.go                        # 消费 DebugChan + 格式化输出
│   ├── event.go                          # SyscallEvent + 记录辅助函数
│   ├── astrace_test.go
│   └── event_test.go
│
├── lib/agents/code-analyst/
│   ├── agent.yaml                       # Agent 配置：name + models + context_budget + skills
│   └── instructions.md                  # Agent 角色定义 + 行为策略
│
├── lib/skills/code-analysis/
│   └── SKILL.md                         # Agent Skills 标准格式（frontmatter + 程序性知识）
│
├── go.mod                                # github.com/usecrux/crux, go 1.26
├── go.sum
├── Makefile                              # build / test / lint / install
├── .golangci.yml
├── .gitignore
└── README.md
```

### 修正后的依赖方向（最终版）

```
internal/types/  ← 所有包均可导入（零外部依赖）
internal/xsync/  ← 所有包均可导入（仅依赖 internal/types/）
internal/ui/     ← 仅 cmd/ 导入

cmd/ → kernel/ → vfs/     → drivers/{llm,shell,fs}
                → context/
                → agents/  → skills/
cmd/ → debug/（仅依赖 internal/types/）
```

无循环依赖，单向流严格成立。

### 架构完成度 Checklist

- [x] 项目上下文深度分析（42 FR + 20 NFR + UX 含义 + 约束）
- [x] 技术栈全栈确定（Go 1.26 + Cobra + Lipgloss + testify）
- [x] 核心架构决策（7 大类：ABI/进程/VFS/CLI集成/调试/错误处理/Agent抽象层）
- [x] 泛型策略（6 个场景 + 核心类型定义）
- [x] 实现模式与一致性规则（命名/结构/格式/通信/过程/泛型 6 大类）
- [x] 项目结构完整定义（含 agents/ 包、SKILL.md 格式、测试文件和 fixture）
- [x] 架构边界清晰（8 组边界 + 依赖方向）
- [x] 需求全覆盖映射（FR→文件 + NFR→架构 + 跨切关注点）
- [x] 数据流定义（端到端含 Agent/Skill 加载 + astrace）
- [x] 验证通过（一致性 ✅ + 覆盖 ✅ + 就绪度 ✅）
- [x] Gap 已修正（泛型包位置 + /dev/fs 驱动 + 共享类型位置）
- [x] Agent/Skill 分层对齐（Agent Skills 行业标准兼容 + MCP Phase 2 兼容）

### 就绪度评估

**总体状态：✅ READY FOR IMPLEMENTATION**

**信心等级：高**

**核心优势：**
1. OS 隐喻驱动的自然模块边界
2. 分类接口组合确保 ABI 可扩展性（15→45 syscall）
3. 泛型工具减少样板代码，提高类型安全
4. SyscallEvent + DebugChan 贯穿式调试数据流
5. 单向依赖 + 依赖注入模式，零循环依赖
6. Agent/Skill 分层清晰——Agent 定义"我是谁"，Skill 定义"如何做 X"，Skill 遵循行业标准可跨平台复用

**实现优先级：**

```
1. 项目初始化（go mod init + 目录结构 + Makefile + .golangci.yml）
2. internal/types/ + internal/xsync/ 基础类型和泛型工具
3. kernel/ 核心（Process 状态机 + Spawn + reasonStep 骨架）
4. vfs/ + drivers/ 框架（VFS 接口 + DeviceRegistry + 驱动注册）
5. context/ + skills/（SKILL.md 解析 + 渐进式加载）+ agents/（agent.yaml + instructions.md + Skill 引用解析 + tools 聚合）
6. 端到端集成（crux "分析代码" --agent=code-analyst 跑通）
7. debug/astrace
8. internal/ui/ + CLI 完善
```
