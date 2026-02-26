# Story 5.1: 概念文档

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a 新用户,
I want 阅读概念文档理解 Crux 的核心 OS 范式,
So that 我能建立正确的心智模型来使用 Crux。

## Acceptance Criteria

1. **覆盖四个核心概念** — Given 概念文档已编写，When 阅读文档，Then 覆盖四个核心概念：进程（Process）、虚拟文件系统（VFS）、Skill、系统调用（Syscall），每个概念包含定义、与 Unix 对应概念的类比、至少一个具体示例

2. **概念关系清晰** — Given 文档已完成，When 阅读概念章节，Then 概念之间的关系清晰：进程通过 syscall 访问 VFS，Agent 通过 Skill 获得能力，Skill 的 allowed-tools 映射为 VFS 设备权限白名单

3. **示例准确反映实际实现** — Given 文档中包含代码示例或 CLI 命令，When 用户按示例操作，Then 示例与实际 Crux 实现一致（命令、输出格式、VFS 路径等均准确）

4. **文档输出为中文 Markdown** — Given 文档已生成，When 查看文件，Then 使用简体中文书写，格式为 Markdown，存放在 `docs/concepts.md`

## Tasks / Subtasks

- [x] Task 1: 创建 `docs/concepts.md` 文件框架 (AC: #4)
  - [x] 1.1 在项目根目录的 `docs/` 文件夹中创建 `concepts.md`
  - [x] 1.2 添加文档标题、简介段落（一段话描述 Crux 的 OS 范式核心思想）

- [x] Task 2: 编写进程（Process）概念章节 (AC: #1, #2, #3)
  - [x] 2.1 定义：智能体进程是 Crux 的一等计算单元，每个 `crux "意图"` 命令创建一个进程
  - [x] 2.2 Unix 类比：进程 ≈ Unix 进程，PID ≈ 进程号，状态机 ≈ 进程状态（Created→Running→Zombie→Dead），spawn ≈ fork+exec，kill ≈ signal，wait ≈ waitpid
  - [x] 2.3 具体示例：展示 `crux "分析代码"` 的完整进程生命周期（spawn→running→zombie→dead），包含 CLI 输出示例
  - [x] 2.4 描述进程树关系（PPID、Children、孤儿进程 reparent 到 PID 1）
  - [x] 2.5 说明进程携带的关键属性：PID、Intent（不可变）、Agent 配置、Skills、CtxID、FDTable、DebugChan

- [x] Task 3: 编写虚拟文件系统（VFS）概念章节 (AC: #1, #2, #3)
  - [x] 3.1 定义：VFS 是 Crux 的统一抽象层，所有资源——LLM、文件系统、Shell、进程状态——通过统一的文件路径访问
  - [x] 3.2 Unix 类比："一切皆文件"哲学，`/dev/` ≈ 设备文件，`/proc/` ≈ 虚拟进程文件系统，FD ≈ 文件描述符
  - [x] 3.3 设备路径表：列出 MVP 所有 VFS 路径及其用途
    - `/dev/llm/claude` — LLM 推理设备（通过 Claude Code CLI）
    - `/dev/fs` — 宿主文件系统访问
    - `/dev/shell` — Shell 命令执行
    - `/proc/{pid}/status` — 进程状态 JSON
    - `/proc/{pid}/intent` — 进程意图文本
    - `/proc/{pid}/context` — 进程上下文摘要
  - [x] 3.4 具体示例：展示一个推理步骤中的 VFS 操作链（Open→Write→Read→Close）
  - [x] 3.5 说明 DeviceRegistry 的设备发现和前缀匹配机制

- [x] Task 4: 编写 Agent 与 Skill 概念章节 (AC: #1, #2, #3)
  - [x] 4.1 Agent 定义："我是谁"——身份、角色定义、模型偏好、上下文预算、Skill 引用
  - [x] 4.2 Skill 定义："如何做 X"——程序性知识、工具权限（allowed-tools），遵循 Agent Skills 行业标准
  - [x] 4.3 Unix 类比：Agent ≈ 可执行程序（/usr/bin/xxx），Skill ≈ 共享库（.so/.dylib），Process ≈ 运行时实例
  - [x] 4.4 四层能力模型图：Process ← Agent ← Skill(s)，展示 Agent 如何引用 Skill，Skill 如何定义 allowed-tools
  - [x] 4.5 具体示例：展示 code-analyst Agent 的 `agent.yaml` 和 code-analysis Skill 的 `SKILL.md` 结构
  - [x] 4.6 说明渐进式加载策略：发现（frontmatter ~100 tokens）→ 激活（完整 body < 5000 tokens）→ 执行（scripts/assets）
  - [x] 4.7 说明 Agent vs Skill 的职责分离表（模型偏好属于 Agent，设备权限属于 Skill）

- [x] Task 5: 编写系统调用（Syscall）概念章节 (AC: #1, #2, #3)
  - [x] 5.1 定义：Syscall 是智能体与内核交互的唯一接口，类似 Unix 进程通过 syscall 请求内核服务
  - [x] 5.2 Unix 类比：spawn ≈ fork+exec，kill ≈ kill(2)，wait ≈ waitpid(2)，open/read/write/close ≈ 文件 I/O syscall
  - [x] 5.3 MVP 15 个 syscall 分类表：
    - 进程管理（5）：Spawn, Kill, Wait, GetPID, PS
    - 上下文管理（4）：CtxAlloc, CtxRead, CtxWrite, CtxFree
    - 文件系统（5）：Open, Read, Write, Close, Stat
    - 调试（1）：DebugRecord
  - [x] 5.4 具体示例：展示一次完整的 reasonStep 循环中涉及的 syscall 序列
  - [x] 5.5 说明 SyscallError 错误模型：每个 syscall 返回包含 Syscall/PID/Device/Err/Code 的结构化错误
  - [x] 5.6 说明 SyscallEvent 调试追踪：所有 syscall 入口/出口自动记录，供 astrace 消费

- [x] Task 6: 编写概念关系总览章节 (AC: #2)
  - [x] 6.1 绘制文本架构图：展示 Process → Syscall → VFS → Device/Driver 的调用链
  - [x] 6.2 绘制端到端数据流：`crux "分析代码" --agent=code-analyst` 从 CLI 到 LLM 响应的完整路径
  - [x] 6.3 说明 astrace 如何串联所有概念：syscall 事件 → DebugChan → astrace 格式化输出

- [x] Task 7: 校验与完善 (AC: #3, #4)
  - [x] 7.1 检查所有 VFS 路径、syscall 名称、CLI 命令与实际代码一致
  - [x] 7.2 确保所有代码示例可复制运行（CLI 命令格式正确）
  - [x] 7.3 最终审读：行文流畅、概念层次清晰、无遗漏

## Dev Notes

### 这是一个文档类 Story

**重要：** 本 Story 不涉及 Go 代码编写。输出是 `docs/concepts.md` 单一 Markdown 文件。开发代理需要深入理解现有代码库，将实现细节转化为面向新用户的概念性文档。

### 文档写作原则

1. **面向新用户** — 读者不了解 Crux，但可能有 Unix/Linux 基础知识。使用 Unix 类比建立心智模型。
2. **概念优先** — 不是 API 参考手册（那是 Story 5.3 的职责），而是建立概念框架。示例用于辅助理解，不追求完整。
3. **准确反映实现** — 所有路径、命令、结构体名称必须与代码一致。不要写尚未实现的功能。
4. **简体中文** — 全文使用简体中文。技术术语首次出现时附英文（如 "进程（Process）"），后续直接使用中文。
5. **不要过度引用 Phase 2** — 概念文档描述当前 MVP 能力。仅在解释设计理由时简要提及未来扩展方向。

### 文档输出位置

文件路径：`docs/concepts.md`

`docs/` 目录当前为空，这是该目录的第一个文件。

### 核心概念映射表

| 概念 | Unix 类比 | Crux 实现 | 关键代码位置 |
|------|----------|----------|------------|
| Process | Unix 进程 | `kernel.Process` 结构体，Created→Running→Zombie→Dead 状态机 | `kernel/process.go` |
| VFS | /dev/, /proc/, open/read/write | `vfs.VFS` + `DeviceRegistry` + `ProcFS`，一切皆文件 | `vfs/vfs.go`, `vfs/dev.go`, `vfs/proc.go` |
| Agent | 可执行程序 (/usr/bin/xxx) | `agents.AgentInfo`：agent.yaml + instructions.md，定义"我是谁" | `agents/types.go`, `agents/loader.go` |
| Skill | 共享库 (.so) | `skills.SkillInfo`：SKILL.md（Agent Skills 标准），定义"如何做 X" | `skills/types.go`, `skills/loader.go` |
| Syscall | Unix syscall (open, read, fork, kill) | 15 个 MVP syscall，分类接口组合（ProcessManager + ContextManager + FileSystem + Debugger） | `kernel/kernel.go` |
| SyscallError | errno | `kernel.SyscallError`（Syscall/PID/Device/Err/Code） | `kernel/errors.go` |
| SyscallEvent | strace 输出 | `types.SyscallEvent`，DebugChan 非阻塞写入 → astrace 消费 | `internal/types/types.go`, `debug/event.go` |
| CtxID | 内存地址空间 | `context.Manager`：CtxAlloc/Read/Write/Free/BuildPrompt | `context/context.go` |
| FD | 文件描述符 | 每进程 FDTable，FD 从 3 分配（0/1/2 预留 stdin/stdout/stderr） | `vfs/vfs.go` |

### 四个核心概念的详细上下文

#### 1. 进程（Process）

**实际实现（kernel/process.go）：**

```go
type Process struct {
    PID, PPID       types.PID
    State           types.ProcessState  // Created(0) → Running(1) → Zombie(2) → Dead(3)
    Intent          string              // 创建后不可变
    Skills          []string
    Children        []types.PID
    FDTable         map[types.FD]vfs.VFSFile
    DebugChan       chan types.SyscallEvent  // 缓冲 256
    Done            chan ExitStatus          // 缓冲 1
    CtxID           types.CtxID
    Result          string
    TokensUsed      int
    AllowedDevices  []string
    cancel          context.CancelFunc
    wg              sync.WaitGroup
    reapOnce        sync.Once
    ...
}
```

**状态机转移规则（严格单向）：**
- Created → Running（reasonStep 开始执行）
- Running → Zombie（正常完成 / 错误 / 超时 / kill）
- Zombie → Dead（wait 回收 + 资源释放）

**PID 分配：** `atomic.AddUint64` 全局递增，从 1 开始，不回收。

**关键生命周期方法：**
- `Start()` — Created → Running
- `Terminate(exit)` — Running → Zombie，记录 ExitStatus
- `Reap()` — Zombie → Dead
- `Cancel()` — 取消 context，触发推理 goroutine 停止

**资源释放顺序（reapProcess）：**
1. handleOrphanChildren（孤儿子进程 reparent）
2. cancel()
3. wg.Wait()
4. 关闭 FD（CloseAll）
5. 关闭 DebugChan
6. CtxFree
7. Reap()（Zombie→Dead）
8. RemoveProcess

#### 2. 虚拟文件系统（VFS）

**实际 VFS 接口（vfs/vfs.go）：**

```go
type VFSFile interface {
    Read(length int) ([]byte, error)
    Write(ctx context.Context, data []byte) error
    Close() error
    Stat() (FileStat, error)
}
```

**VFS 主体方法：**
- `Open(pid, path, flags)` — 通过 DeviceRegistry 查找设备工厂，创建 VFSFile，分配 FD（从 3 开始）
- `Read(pid, fd, length)` — 从 FD 读取
- `Write(ctx, pid, fd, data)` — 写入 FD（LLM Write 需要 context 支持取消）
- `Close(pid, fd)` — 关闭 FD
- `CloseAll(pid)` — 进程退出时清理

**设备注册（vfs/dev.go）：**
- `DeviceRegistry` 基于 `Registry[VFSFileFactory]`
- 支持精确匹配和最长前缀匹配（如 `/dev/fs/path/to/file` 匹配 `/dev/fs`，subpath = `/path/to/file`）

**当前已注册的 MVP 设备：**

| VFS 路径 | 驱动实现 | 注册位置 |
|---------|---------|---------|
| `/dev/llm/claude` | `drivers/llm/ClaudeCliDriver` → `claude -p` CLI 调用 | `cmd/crux/main.go` |
| `/dev/fs` | `drivers/fs/HostFSDriver` → `os.Open/Read` 封装 | `cmd/crux/main.go` |
| `/dev/shell` | `drivers/shell/Driver` → `exec.CommandContext` | `cmd/crux/main.go` |
| `/proc` | `vfs/ProcFS` → 动态生成进程信息 | `cmd/crux/main.go` |

**ProcFS 支持路径（vfs/proc.go）：**
- `/proc/{pid}/status` — JSON 格式进程状态（pid/state/intent/skills/tokens/elapsed 等）
- `/proc/{pid}/intent` — 纯文本意图字符串
- `/proc/{pid}/context` — 上下文摘要（通过 ContextSummaryProvider）

#### 3. Agent 与 Skill

**Agent 定义（agents/types.go）：**

```go
type AgentManifest struct {
    Name          string
    Description   string
    Models        AgentModels   // Provider/Preferred/Fallback
    ContextBudget int
    Skills        []string      // Skill 名称引用列表
}

type AgentInfo struct {
    Manifest     AgentManifest
    Instructions string            // instructions.md 内容
    Skills       []*skills.SkillInfo
}

func (a *AgentInfo) AllowedTools() []string  // 聚合所有 Skill 的 allowed-tools
func (a *AgentInfo) SystemPrompt() string    // Instructions + 所有 Skill.Body
```

**Skill 定义（skills/types.go）：**

```go
type SkillManifest struct {
    Name            string
    Description     string
    AllowedToolsRaw string            `yaml:"allowed-tools"`
    Metadata        map[string]string
}

type SkillInfo struct {
    Manifest SkillManifest
    Body     string  // SKILL.md 正文
}
```

**参考 Agent 实际目录结构：**
```
lib/agents/code-analyst/
├── agent.yaml        # Agent 配置
└── instructions.md   # Agent 角色定义

lib/skills/code-analysis/
└── SKILL.md          # Agent Skills 标准格式
```

**Agent vs Skill 职责分离：**

| 维度 | Agent | Skill |
|------|-------|-------|
| 定义 | "我是谁" | "如何做 X" |
| 模型偏好 | ✅ models | ❌ |
| 上下文预算 | ✅ context_budget | ❌ |
| 设备权限 | ❌（由 Skill 聚合） | ✅ allowed-tools |
| 复用性 | 特定角色 | 跨 Agent 共享 |

**Spawn 时的 Agent/Skill 处理流程：**
1. CLI `--agent=code-analyst` → AgentLoader 加载 agent.yaml + instructions.md
2. AgentLoader 解析 `skills` 列表 → SkillLoader 加载每个 SKILL.md
3. `agent.AllowedTools()` 聚合所有 Skill 的 allowed-tools → 进程 AllowedDevices 白名单
4. `agent.SystemPrompt()` = Agent instructions + Skill body 拼接 → CLI `--system-prompt`

#### 4. 系统调用（Syscall）

**MVP 15 个 syscall 分类接口（kernel/kernel.go）：**

```go
// 进程管理
type ProcessManager interface {
    Spawn(intent string, agent *agents.AgentInfo, opts SpawnOpts) (types.PID, error)
    Kill(pid types.PID, signal types.Signal) error
    Wait(pid types.PID) (ExitStatus, error)
}

// 上下文管理（context/context.go 实现）
CtxAlloc(size int) (CtxID, error)
CtxRead(cid CtxID, offset, length int) ([]byte, error)
CtxWrite(cid CtxID, offset int, data []byte) error
CtxFree(cid CtxID) error

// 文件系统（vfs/vfs.go 实现）
Open(pid PID, path string, flags OpenFlag) (FD, error)
Read(pid PID, fd FD, length int) ([]byte, error)
Write(ctx context.Context, pid PID, fd FD, data []byte) error
Close(pid PID, fd FD) error

// 调试
// DebugRecord — 通过 emitEvent() 记录 SyscallEvent
```

**SyscallError 错误模型（kernel/errors.go）：**

```go
type SyscallError struct {
    Syscall string      // "Spawn", "Open", "CtxWrite" 等
    PID     types.PID
    Device  string      // 涉及的 VFS 路径
    Err     error       // 底层错误
    Code    types.ErrCode  // TIMEOUT/NOT_FOUND/PERMISSION/INTERNAL/DRIVER
}
```

**SyscallEvent 追踪（internal/types/types.go）：**

```go
type SyscallEvent struct {
    Timestamp time.Duration    // 相对进程创建时间
    PID       PID
    Syscall   string           // 与接口方法名一致
    Args      map[string]any
    Result    any
    Err       error
    Duration  time.Duration
}
```

**reasonStep 循环中的 syscall 序列示例：**
```
[  0.000s] Spawn("分析代码", agent="code-analyst")    = PID(1)       12ms
[  0.012s] CtxAlloc(64)                                = CtxID(1)      0ms
[  0.013s] Open("/dev/llm/claude", O_RDWR)             = FD(3)         1ms
[  0.014s] Write(FD(3), <prompt>)                      = ok           5200ms  ← LLM 调用
[  5.214s] Read(FD(3), 65536)                          = <response>     2ms
[  5.216s] Open("/dev/fs/./src/main.go", O_RDONLY)     = FD(4)         1ms    ← 工具调用
[  5.217s] Read(FD(4), 65536)                          = <file content> 1ms
[  5.218s] Close(FD(4))                                = ok             0ms
[  5.218s] CtxWrite(CtxID(1), 0, <tool result>)        = ok             0ms
[  5.219s] Write(FD(3), <prompt+context>)              = ok           3100ms  ← 第二轮推理
[  8.319s] Read(FD(3), 65536)                          = <final text>   2ms
[  8.321s] Close(FD(3))                                = ok             0ms
[  8.321s] CtxFree(CtxID(1))                           = ok             0ms
```

### CLI 命令参考

当前 MVP 实现的 CLI 命令（`cmd/crux/main.go`）：

| 命令 | 说明 |
|------|------|
| `crux "意图"` | 主命令：Spawn Agent 执行意图 |
| `crux "意图" --agent=code-analyst` | 使用指定 Agent 定义 |
| `crux ps` | 列出所有进程（支持 --json） |
| `crux ps --json` | JSON 格式输出进程列表 |
| `crux kill <pid>` | 终止指定进程 |
| `crux astrace <pid>` | 实时追踪进程 syscall |
| `crux version` | 显示版本信息 |

**全局 flags：** `--json`, `--verbose/-v`, `--quiet/-q`, `--model`, `--max-steps`, `--agent`

### 端到端数据流（文档应包含的核心图）

```
用户输入: crux "分析代码" --agent=code-analyst
    │
    ▼
cmd/crux/main.go (CLI 入口)
    │  解析 --agent flag
    ▼
agents.Loader.Load("code-analyst")
    │  → 读取 lib/agents/code-analyst/agent.yaml
    │  → 读取 lib/agents/code-analyst/instructions.md
    │  → 解析 skills: [code-analysis]
    │  → skills.Loader.Load("code-analysis") → SKILL.md
    │  → 聚合 AllowedTools, 组装 SystemPrompt
    ▼
kernel.Spawn(intent, agentInfo, opts)
    │  1. CtxAlloc → 分配上下文空间
    │  2. SetSystemPrompt(Agent instructions + Skill body)
    │  3. Open("/dev/llm/claude") → FD
    │  4. 启动 goroutine → reasonStep 循环
    ▼
reasonStep 循环:
    │  BuildPrompt → Write(LLM FD) → Read(LLM FD) → 解析 Action
    │  ├── ActionText → 最终结果，进程完成
    │  └── ActionToolCall → Open/Write/Read/Close 工具设备
    │       → CtxWrite(工具结果) → 继续下一轮
    ▼
进程完成: Running → Zombie → (Wait/Reap) → Dead
    │  资源释放: cancel → wg.Wait → CloseAll FD → CtxFree
    ▼
CLI 输出: [kernel] PID 1 exited(0) | tokens: N | elapsed: Ns
```

### astrace 调试数据流

```
syscall 入口 → debug.NewEvent() → 构造 SyscallEvent
    │
    ▼
syscall 执行
    │
    ▼
syscall 出口 → debug.CompleteEvent() → 填充 Result/Err/Duration
    │
    ▼
debug.EmitEvent(proc.DebugChan, event)  [非阻塞，满则丢弃]
    │
    ▼
crux astrace <pid> → 消费 DebugChan → 格式化输出到终端
    格式: [N.NNNs] SyscallName(args) → result    duration
```

### 前序 Story 经验

#### Epic 4 通用经验
- **测试模式稳定** — `-race` 默认开启，所有并发数据结构经过验证
- **reapOnce 模式** — 资源释放通过 `sync.Once` 保证幂等
- **emitEvent 非阻塞** — DebugChan 缓冲 256，满时丢弃不阻塞

#### Story 4.5 经验（最近完成的 Story）
- **无生产代码修改** — 该 Story 仅补充测试验证现有实现
- **Code Review 中发现 3 个 MEDIUM issues** — 主要是测试断言不够严格
- **CtxFree 在 Spawn 错误路径中有 3 处调用** — 确保部分失败时释放资源

### Git 近期提交分析

最近 10 条提交全部属于 Epic 4 的完成和收尾工作：
- `ac47cd6` Finalize Story 4.5: Context Release
- `b2197e0` Finalize Story 4.4: crux ps Command
- `06f2ac2` Finalize Story 4.3: /proc Dynamic Filesystem
- `92787da` Finalize Story 4.2: Orphan Reparenting and Zombie Auto-Reap

**模式：** 每个 Story 通常有 3 个提交（Add → Review → Finalize），commit message 使用英文。

### 范围边界

**本 Story 包含：**
- 创建 `docs/concepts.md` 概念文档
- 覆盖 4 个核心概念（Process、VFS、Skill、Syscall）+ 概念关系总览
- 每个概念的定义、Unix 类比、示例

**本 Story 不包含：**
- 快速上手指南（Story 5.2）
- 参考手册 / syscall 完整签名 / VFS 完整规范（Story 5.3）
- README.md 编写（可在 Epic 5 完成后单独处理）
- 英文翻译版本

### Project Structure Notes

**创建的新文件：**
```
docs/concepts.md          — 概念文档（本 Story 唯一输出）
```

**不修改的文件：**
```
所有 .go 文件            — 本 Story 不涉及代码修改
```

### References

**规划文档：**
- [Source: _bmad-output/planning-artifacts/epics.md#Story 5.1] — Story 定义和验收标准
- [Source: _bmad-output/planning-artifacts/prd.md#FR38] — 概念文档功能需求
- [Source: _bmad-output/planning-artifacts/prd.md#Documentation Strategy] — 文档策略
- [Source: _bmad-output/planning-artifacts/architecture.md#Core Architectural Decisions] — 7 大架构决策
- [Source: _bmad-output/planning-artifacts/architecture.md#Implementation Patterns] — 命名模式和结构模式
- [Source: _bmad-output/project-context.md] — AI Agent 编码规则和项目上下文

**源码参考（文档示例需准确引用）：**
- kernel/kernel.go: KernelImpl, Spawn(), reasonStep(), emitEvent(), ProcessManager interface
- kernel/process.go: Process struct, ProcessState, Start/Terminate/Reap/Cancel
- kernel/errors.go: SyscallError, ErrCode constants
- vfs/vfs.go: VFS, VFSFile interface, Open/Read/Write/Close
- vfs/dev.go: DeviceRegistry, prefix matching
- vfs/proc.go: ProcFS, ProcessInfoProvider, /proc/{pid}/{status,intent,context}
- context/context.go: Manager, CtxAlloc/Read/Write/Free/BuildPrompt, Message, PromptResult
- agents/types.go: AgentManifest, AgentInfo, AllowedTools(), SystemPrompt()
- skills/types.go: SkillManifest, SkillInfo, AllowedTools()
- debug/event.go: NewEvent, CompleteEvent, EmitEvent
- internal/types/types.go: PID, FD, CtxID, ErrCode, ProcessState, SyscallEvent
- cmd/crux/main.go: CLI commands (root, ps, kill, astrace, version), dependency injection

**参考 Agent/Skill 文件：**
- lib/agents/code-analyst/agent.yaml — Agent 配置示例
- lib/agents/code-analyst/instructions.md — Agent 指令示例
- lib/skills/code-analysis/SKILL.md — Skill 定义示例

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6

### Debug Log References

- 源码验证：阅读了 kernel/process.go, kernel/kernel.go, kernel/errors.go, kernel/reap.go, vfs/vfs.go, vfs/dev.go, vfs/proc.go, context/context.go, agents/types.go, skills/types.go, internal/types/types.go, debug/event.go, cmd/crux/main.go, lib/agents/code-analyst/agent.yaml, lib/agents/code-analyst/instructions.md, lib/skills/code-analysis/SKILL.md
- 自动化验证：使用 Explore agent 对文档与源码进行全面交叉校验

### Completion Notes List

- ✅ Task 1: 创建 docs/concepts.md 文件框架，包含标题和简介段落
- ✅ Task 2: 编写进程章节——定义、状态机（Created→Running→Zombie→Dead）、Unix 类比表、完整生命周期示例（含 CLI 输出）、进程树（PPID/reparent）、关键属性表
- ✅ Task 3: 编写 VFS 章节——定义、"一切皆文件"类比、6 个 MVP 设备路径表、VFS 操作链示例（8 步）、DeviceRegistry 精确匹配 + 最长前缀匹配机制、VFSFile 接口
- ✅ Task 4: 编写 Agent 与 Skill 章节——Agent 定义、Skill 定义（含 SKILL.md 示例）、Unix 类比、四层能力模型文本图、code-analyst agent.yaml 实际示例、渐进式加载策略（3 阶段）、Agent vs Skill 职责分离表
- ✅ Task 5: 编写 Syscall 章节——定义、12 个 Unix 类比、MVP 15 个 syscall 分类表（4 子接口）、完整 reasonStep 循环 syscall 序列示例、SyscallError 结构（含 6 个 ErrCode）、SyscallEvent 调试追踪（含 astrace 使用示例）
- ✅ Task 6: 编写概念关系总览——调用链架构图（Kernel→VFS→Device）、端到端数据流（CLI→AgentLoader→Spawn→reasonStep→完成）、astrace 调试数据流（NewEvent→CompleteEvent→EmitEvent→消费）
- ✅ Task 7: 校验与完善——使用 Explore agent 进行自动化交叉校验，修正 ErrCode 列表（补充 INVALID），确认所有 VFS 路径/syscall 名称/CLI 命令与代码一致

### File List

- docs/concepts.md — 新增：Crux 核心概念文档（简体中文 Markdown）

### Change Log

- 2026-02-26: 创建 docs/concepts.md 概念文档，覆盖四个核心概念（Process/VFS/Skill/Syscall）+ 概念关系总览，所有示例与源码交叉验证通过
- 2026-02-26: [Code Review] 修复 5 个 issues（2 HIGH, 3 MEDIUM）:
  - H1: 修正 Syscall 示例中工具调用序列（O_RDONLY→O_RDWR，补充缺失的 Write 步骤）
  - H2: 端到端数据流补充 AppendMessage(assistant) 步骤
  - M1: 明确标注 MVP Syscall 中 13 个已实现 + 2 个规划中
  - M2: VFS 操作链示例标题修正，区分进程级和步骤级操作
  - M3: Syscall 示例标题从"一次 reasonStep 循环"改为"完整进程生命周期"
