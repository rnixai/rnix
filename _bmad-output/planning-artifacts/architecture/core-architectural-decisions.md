# Core Architectural Decisions

## 决策优先级分析

**关键决策（阻塞实现）：**
- 数据持久化策略（一切皆文件）
- Compose DAG 调度器（自实现）
- AgentShell 解析器架构（手写递归下降 + 解释执行）

**重要决策（塑造架构）：**
- 多 LLM Provider 动态配置（DriverRegistry + rnix-providers.yaml）
- gdb 调试通道（IPC 扩展）
- 时间旅行 fork-continue（普通 Spawn）
- 分布式追踪传播（上下文自动传播）
- skillpkg 仓库协议（Git-based）

**延迟决策（Phase 3+）：**
- 声明式意图 Reconciler 的具体事件驱动框架选型
- Token 经济的价格信号算法
- 干细胞分化的 Skill 匹配算法
- 可视化 Dashboard 的 TUI 布局框架（bubbletea 组件设计）

## Decision 1: Syscall ABI 设计风格 — 分类接口组合

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

## Decision 2: 进程模型与并发

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
    DebugChan chan SyscallEvent      // strace 事件通道
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

## Decision 3: VFS 实现策略

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

## Decision 4: 多 LLM Provider 动态配置

**决策：** LLM 驱动层支持多种 provider 的配置文件驱动注册，通过 `rnix-providers.yaml` 声明式定义，daemon 启动时动态解析并注册到 VFS。

**设计演进：**
- Phase 1：Claude Code CLI 为唯一 provider（`/dev/llm/claude`），验证核心范式
- Phase 2：引入 Cursor CLI（`/dev/llm/cursor`）+ OpenAI 兼容 HTTP API 驱动，支持 Ollama/Groq/DeepSeek 等
- 未来：可扩展 native SDK 驱动（直连 Anthropic/OpenAI/Gemini API）

**两类驱动模型：**

| 驱动类型 | 实现方式 | 认证处理 | 示例 |
|---------|---------|---------|------|
| CLI 驱动 | `exec.CommandContext` 调用本地 CLI 工具 | CLI 自身处理 | claude、cursor |
| HTTP API 驱动 | `net/http` 调用 OpenAI 兼容端点 | 环境变量引用 API Key | Ollama、Groq、DeepSeek |

**Provider 配置（`rnix-providers.yaml`）：**

```yaml
providers:
  claude:
    driver: claude-cli
    default_model: sonnet
  cursor:
    driver: cursor-cli
    default_model: ""
  ollama:
    driver: openai-compat
    base_url: http://localhost:11434/v1
    default_model: llama3
  groq:
    driver: openai-compat
    base_url: https://api.groq.com/openai/v1
    api_key_env: GROQ_API_KEY
    default_model: llama-3.3-70b-versatile
```

**驱动接口层次：**

```go
// 基础驱动接口
type LLMDriver interface {
    Call(ctx context.Context, req LLMRequest) (*LLMResponse, error)
    Stream(ctx context.Context, req LLMRequest) (<-chan StreamEvent, error)
    Info() DriverInfo
}

// 工具调用扩展接口（HTTP API 驱动实现）
type ToolCallingDriver interface {
    LLMDriver
    CallWithTools(ctx context.Context, req LLMRequest, tools []ToolDef) (*LLMResponse, error)
    StreamWithTools(ctx context.Context, req LLMRequest, tools []ToolDef) (<-chan StreamEvent, error)
}

// 驱动注册表
type DriverRegistry struct {
    registry *xsync.Registry[LLMDriver]
}
```

**Daemon 启动注册流程：**

```
1. 解析 rnix-providers.yaml
2. 遍历 providers 配置
3. 根据 driver 字段选择构造函数：
   - "claude-cli"    → NewClaudeCliDriver()
   - "cursor-cli"    → NewCursorCliDriver()
   - "openai-compat" → NewOpenAICompatDriver(name, baseURL, opts...)
4. 通过 DriverRegistry 注册驱动实例
5. 桥接到 DeviceRegistry：devReg.Register("/dev/llm/<name>", FileFactory(driver))
```

**Provider 选择优先级：** CLI `--provider` > agent.yaml `models.provider` > 系统默认（claude）

**模型选择优先级：** CLI `--model` > agent.yaml `models.preferred` > provider `default_model`

**Fallback 降级：** `models.preferred` 对应 provider 调用失败（HTTP 5xx、超时、连接拒绝、认证失败）时，自动尝试 `models.fallback` 指定的备选 provider/model 组合。

**Claude Code CLI 调用模板（默认 provider）：**

```go
cmd := exec.CommandContext(ctx, "claude", "-p", intent,
    "--output-format", "json",
    "--system-prompt", agentInstructions + skillInstructions,
    "--model", model,
    "--max-turns", "1",
)
```

**system prompt 组装顺序：** Agent instructions（角色定义）+ 各 Skill SKILL.md body（程序性知识），由 AgentLoader 在 Spawn 时组装。

**stream-json 消费：** `--output-format stream-json` 时，通过 `bufio.Scanner` 逐行读取 stdout，每行解析为 `StreamEvent`，写入 `Process.DebugChan` 供 strace 消费。

**超时处理：** `context.WithTimeout` 包装，超时后 `cmd.Process.Kill()`（CLI 驱动）或请求取消（HTTP 驱动），进程转 Zombie。

**错误标准化：**

```go
var (
    ErrRateLimit     = errors.New("llm: rate limit exceeded")
    ErrAuth          = errors.New("llm: authentication failed")
    ErrContextLength = errors.New("llm: context length exceeded")
    ErrModelNotFound = errors.New("llm: model not found")
    ErrTimeout       = errors.New("llm: request timed out")
)
```

两类驱动共享统一错误类型 `LLMError`（含 Provider 名称和 HTTP 状态码），CLI 驱动通过字符串分类映射错误，HTTP 驱动通过 HTTP 状态码映射。

## Decision 5: 调试架构（strace）

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

**数据流：** syscall 实现入口/出口 → 构造 SyscallEvent → 写入 DebugChan → strace goroutine 消费 → 格式化输出到终端。

**无 strace 时：** DebugChan 为 nil，跳过事件记录（零开销）。

## Decision 6: 错误处理与恢复

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

## Decision 7: Agent 抽象层与 Skill 标准化

**决策：** 引入 Agent 抽象层，将原有 Skill 的职责拆分为 Agent（智能体定义）和 Skill（能力模块）两层，且 Skill 格式遵循 Agent Skills 行业标准（agentskills.io）。

**背景：** 原设计中 Skill（manifest.yaml + instructions.md）同时承担了"智能体定义"和"能力模块"双重职责。SkillManifest 包含 Models（模型偏好）和 ContextBudget（上下文预算），这些是智能体级别的配置，不应属于共享库。同时，Agent Skills 开放标准（由 Anthropic 发起，30+ AI 工具采用）定义了 Skill 的标准格式（SKILL.md），Rnix 原有的双文件格式无法与生态互操作。

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

**Agent 定义（Rnix 特有）：**

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
| 标准 | Rnix 特有 | Agent Skills 行业标准 |

**渐进式加载（Progressive Disclosure）：**

1. **发现**（启动时）：扫描 skill 目录，仅解析 SKILL.md frontmatter → ~100 tokens/skill
2. **激活**（任务匹配时）：加载完整 SKILL.md body → < 5000 tokens
3. **执行**（按需）：加载 scripts/、references/、assets/ 中的文件

**Spawn 流程（更新）：**

```
rnix "分析代码" --agent=code-analyst

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

## Decision 8: 数据持久化与状态管理

**决策：一切皆文件——所有持久化通过文件系统完成，不引入嵌入式数据库。**

| 数据类型 | 存储格式 | 路径 | 阶段 |
|---------|---------|------|------|
| Time-travel 录制 | JSON Lines（每行一个事件） | `$PROJECT/.rnix/records/<pid>-<timestamp>/` | Phase 3 |
| Skill 本地注册表 | 目录结构 + manifest 文件 | `lib/skills/` + `$RNIX_CACHE/registry.json` | Phase 2 |
| 声誉数据 | JSON 文件 | `$PROJECT/.rnix/reputation/` | Phase 3 |
| 行为基线 | JSON 文件 | `$PROJECT/.rnix/immune/` | Phase 3 |
| Compose 状态 | 内存（进程表 + ProcGroup） | 无持久化，运行时状态 | Phase 2 |

**理由：** 与 Rnix 的 Unix 哲学（"一切皆文件"）完全一致。文件系统存储透明可检视、可版本控制、可用标准 Unix 工具处理。Rnix 的数据规模（进程级元数据、调试录制）不需要数据库的查询能力。

## Decision 9: Compose 引擎架构

**DAG 调度器：**
- 决策：自实现拓扑排序 + goroutine 并行执行
- 理由：Go 标准库足够，DAG 拓扑排序是经典算法（~50 行代码），不需要引入第三方依赖
- 实现：`compose/scheduler.go`——解析 `depends_on` 构建 DAG，Kahn 算法拓扑排序，无依赖节点并行 Spawn

**YAML Schema 版本：**
- 决策：`version: "1.0"` 字段 + 向前兼容检查
- 规则：解析时检查 major version，minor version 差异向前兼容，major version 不匹配则报错并提示升级

**Compose 与 ProcGroup 集成：**
- 决策：每个 Compose 编排自动创建一个 ProcGroup
- `compose up` → 创建 ProcGroup → 按 DAG 顺序 Spawn，每个进程自动 JoinGroup
- `compose down` → SignalGroup(SIGTERM) → 等待全部退出 → 释放 ProcGroup

## Decision 10: AgentShell 解析器架构

**解析器实现：**
- 决策：手写递归下降解析器
- 理由：AgentShell 语法简单（非通用编程语言），手写更可控、更易调试；Go 生态 parser generator 引入额外复杂度但收益有限
- 实现路径：`shell/lexer.go`（词法分析）+ `shell/parser.go`（语法分析）+ `shell/ast.go`（AST 定义）

**AST 设计：**
- 决策：统一 `Node` 接口 + 具体节点类型
- Phase 2 节点：SpawnNode、PipeNode、IfNode、OnErrorNode、ExportNode、ScriptNode
- Phase 3 新增：ForNode、WhileNode、FnNode、ParallelNode、AssignNode、SourceNode
- 扩展方式：新增节点类型不破坏已有结构

**执行模型：**
- 决策：解释执行（AST walker）
- 理由：AgentShell 瓶颈是 LLM 调用（秒级），解释器开销可忽略（NFR39 ≤ 1ms/次），无需编译到中间表示
- 实现：`shell/interpreter.go`——遍历 AST 节点，递归执行

## Decision 11: 调试工具链架构

**gdb Attach 机制：**
- 决策：通过 IPC 扩展，新增 `attach_gdb` method
- 理由：复用现有 Daemon + Unix domain socket 架构，与 `attach_debug`（strace）模式一致，保持架构统一性
- 交互模式：客户端发送调试命令（step/breakpoint/inspect/modify），服务端在 reasonStep 循环中检查断点并暂停

**时间旅行 fork-continue：**
- 决策：作为普通进程 Spawn
- 流程：从录制的历史时间点恢复上下文快照 → CtxAlloc + 回放消息历史 → Spawn 新进程（PPID 指向原录制进程 PID）→ 新进程进入正常 reasonStep 循环（产生真实 LLM 调用）
- 理由：fork 出的分支与普通进程使用同一套 ps/kill/strace 工具，不需要独立的隔离执行环境

**分布式追踪传播：**
- 决策：通过上下文自动传播
- 机制：Process 结构体新增 `TraceID` 和 `SpanID` 字段；Spawn 子进程时自动继承父进程 TraceID + 生成新 SpanID；IPC Send/Recv 时 TraceID 作为消息元数据自动携带
- 理由：对开发者透明，不需要手动管理 Trace 传播

## Decision 12: Skill 包生态架构

**仓库协议：**
- 决策：Git-based（类似 Go modules）
- 格式：`skill install github.com/user/skill-name@v1.2.0`
- 机制：Git clone/fetch + tag 版本管理，不需要自建 registry 服务
- 理由：与 Go 生态开发者心智模型一致，利用 Git 基础设施

**版本解析：**
- 决策：SemVer + 最小版本选择（MVS）
- SKILL.md frontmatter 新增字段：`version: "1.2.0"` + `requires: ["other-skill@>=1.0.0"]`
- 解析算法：Go modules 风格 MVS——选择满足所有约束的最小兼容版本

**四层仓库查找链：**
1. 项目本地：`lib/skills/`（最高优先级）
2. 私有仓库：`$RNIX_PRIVATE_REGISTRY`（企业内部）
3. 社区仓库：默认 GitHub（开源生态）
4. 官方仓库：`github.com/rnixai/skills`（Rnix 官方维护）

## 决策影响分析

**实现顺序：**
1. Compose 引擎（DAG 调度 + ProcGroup 集成）— 依赖已有的 Spawn + ProcGroup
2. AgentShell Phase 2 语法（管道 + if-else + on-error）— 独立模块，可并行开发
3. skillpkg 客户端（Git-based install/search）— 依赖 Skill 加载器已稳定
4. gdb 调试器 — 依赖 IPC 扩展 + reasonStep 断点钩子
5. 时间旅行录制/回放 — 依赖 DebugRecord 完整 + 文件持久化
6. 分布式追踪 — 依赖 Compose + IPC + TraceID 传播

**跨组件依赖：**
- Compose 依赖 ProcGroup + Spawn + DAG 调度
- AgentShell 管道依赖 IPC Pipe
- gdb 依赖 IPC 扩展 + 新增 KernelImpl 方法
- 时间旅行依赖 gdb + DebugRecord + 上下文快照
- skillpkg 依赖 Skill 加载器 + Git 操作
- 分布式追踪依赖 Compose + IPC + Process TraceID 字段

## Go 1.26 特性利用

| 特性 | Rnix 使用场景 |
|------|-------------|
| **Green Tea GC**（默认启用） | 自动受益——strace 高吞吐事件流的 GC 压力降低 |
| **Goroutine Leak Profiler**（实验性） | 验证 NFR8（进程退出后 goroutine 正确释放），集成到测试和开发调试 |
| **`new(expr)` 表达式初始化** | 简化结构体创建（如 `new(Process{PID: pid, State: Created})`） |
| **自引用泛型** | 类型安全的注册表模式（如 `Registry[T]`、`Future[T]`） |
| **`go fix` 现代化** | 自动迁移到最新 API 模式 |

## 泛型策略

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

## Decision 13: LLM Serve Gateway（OpenAI 兼容 HTTP 网关）

**决策：** 在 `ipc/` 包中新增 `http_openai.go`，实现 OpenAI 兼容的 HTTP Server，直接复用 `DriverRegistry` 和 `LLMDriver` 接口，作为 daemon 的可选附加监听器。通过 `rnix serve --port 8080` 命令启动。

**背景：** 用户（旅程 6）希望用一个端口统一所有 LLM 访问——Cursor Pro、本地 Ollama、云端 Groq 等。任何支持 OpenAI API 的工具（Aider、Open WebUI、标准 `openai` Python 库）都可以即插即用，无需了解 Rnix 内部。

**理由：**
1. **复用而非重建** — 所有 LLM 调用逻辑已在 `LLMDriver` 接口和具体驱动中实现，HTTP Server 仅做协议翻译（HTTP → `LLMDriver.Call`/`Stream`）
2. **放在 `ipc/` 包** — 与 Unix socket IPC server 同级，HTTP 是另一种 IPC 通道，共享 daemon 的 `DriverRegistry` 实例
3. **标准库 `net/http`** — Go 标准库 HTTP server 足够（NFR51 要求 ≥10 并发），端点少（3 个），不需要 gin/echo 等框架
4. **SSE 流式** — `LLMDriver.Stream()` 已返回 `<-chan StreamEvent`，转为 SSE `data: {...}\n\n` 格式即可
5. **安全默认** — 仅绑定 `127.0.0.1`（NFR52），本地信任模型，无认证开销
6. **绕过 Kernel** — 网关请求直接调用 `DriverRegistry`，不创建 Rnix 进程，最小化 HTTP 开销（NFR50 ≤ 50ms）

**核心数据流：**

```
外部工具 (Aider / Open WebUI / Python openai 库)
    │
    ▼
HTTP POST /v1/chat/completions
    {"model": "ollama:llama3", "messages": [...], "stream": true}
    │
    ▼
ipc/http_openai.go — OpenAIServer（协议翻译层）
    │  1. 解析 model 字段 → provider="ollama", model="llama3"
    │  2. driverReg.Get("ollama") → OpenAICompatDriver
    │  3. 构造 LLMRequest{Model: "llama3", Messages: [...]}
    ▼
LLMDriver.Stream(ctx, req) → <-chan StreamEvent
    │
    ▼
SSE 响应: data: {"choices":[{"delta":{"content":"..."}}]}\n\n
```

**HTTP 端点清单：**

| 端点 | 方法 | 说明 | FR |
|------|------|------|-----|
| `/v1/chat/completions` | POST | Chat Completion（支持 `stream: true/false`） | FR148, FR150 |
| `/v1/models` | GET | 返回已注册且健康的 provider 及模型列表 | FR149 |
| `/health` | GET | 服务健康检查（内部使用） | — |

**model 参数路由规则（FR151）：**

| 输入 model 值 | 解析结果 | 说明 |
|---------------|---------|------|
| `"ollama:llama3"` | provider=`ollama`, model=`llama3` | 复合格式，显式指定 |
| `"ollama"` | provider=`ollama`, model=provider 的 `default_model` | 仅 provider 名，用默认模型 |
| `"cursor:claude-3.5-sonnet"` | provider=`cursor`, model=`claude-3.5-sonnet` | CLI 驱动也支持 |
| 未知 provider | 返回 HTTP 404 + 可用 provider 列表 | 友好错误信息 |

**OpenAI 兼容层核心结构：**

```go
// ipc/http_openai.go

type OpenAIServer struct {
    driverReg  *llm.DriverRegistry
    listenAddr string       // "127.0.0.1:8080"
    server     *http.Server
}

func NewOpenAIServer(driverReg *llm.DriverRegistry, addr string) *OpenAIServer { ... }

func (s *OpenAIServer) ListenAndServe() error {
    mux := http.NewServeMux()
    mux.HandleFunc("POST /v1/chat/completions", s.handleChatCompletions)
    mux.HandleFunc("GET /v1/models", s.handleListModels)
    mux.HandleFunc("GET /health", s.handleHealth)
    s.server = &http.Server{Addr: s.listenAddr, Handler: mux}
    return s.server.ListenAndServe()
}

func (s *OpenAIServer) Shutdown(ctx context.Context) error {
    return s.server.Shutdown(ctx)
}

// --- 请求/响应类型（OpenAI 兼容格式）---

type ChatCompletionRequest struct {
    Model       string           `json:"model"`
    Messages    []ChatMessage    `json:"messages"`
    Stream      bool             `json:"stream,omitempty"`
    Temperature *float64         `json:"temperature,omitempty"`
    MaxTokens   *int             `json:"max_tokens,omitempty"`
}

type ChatMessage struct {
    Role    string `json:"role"`
    Content string `json:"content"`
}

type ChatCompletionResponse struct {
    ID      string             `json:"id"`
    Object  string             `json:"object"`  // "chat.completion"
    Created int64              `json:"created"`
    Model   string             `json:"model"`
    Choices []ChatChoice       `json:"choices"`
    Usage   *ChatUsage         `json:"usage,omitempty"`
}

type ChatCompletionChunk struct {
    ID      string             `json:"id"`
    Object  string             `json:"object"`  // "chat.completion.chunk"
    Created int64              `json:"created"`
    Model   string             `json:"model"`
    Choices []ChatChunkChoice  `json:"choices"`
    Usage   *ChatUsage         `json:"usage,omitempty"`
}
```

**SSE 流式输出模式：**

```go
func (s *OpenAIServer) handleChatCompletions(w http.ResponseWriter, r *http.Request) {
    // 1. 解析 ChatCompletionRequest
    // 2. parseModel(req.Model) → provider, model
    // 3. driver, ok := s.driverReg.Get(provider)
    // 4. 构造 llm.LLMRequest（messages 转换 + model 设置）

    if req.Stream {
        // SSE 流式
        w.Header().Set("Content-Type", "text/event-stream")
        w.Header().Set("Cache-Control", "no-cache")
        w.Header().Set("Connection", "keep-alive")
        flusher := w.(http.Flusher)

        ch, err := driver.Stream(r.Context(), llmReq)
        for ev := range ch {
            chunk := toChunk(ev) // StreamEvent → ChatCompletionChunk
            fmt.Fprintf(w, "data: %s\n\n", jsonMarshal(chunk))
            flusher.Flush()
        }
        fmt.Fprintf(w, "data: [DONE]\n\n")
        flusher.Flush()
    } else {
        // 同步响应
        resp, err := driver.Call(r.Context(), llmReq)
        json.NewEncoder(w).Encode(toCompletion(resp))
    }
}
```

**与现有架构的关系：**

| 组件 | 影响 | 说明 |
|------|------|------|
| Kernel | **无改动** | 网关绕过 Kernel，直接调用 DriverRegistry |
| VFS | **无改动** | 不通过 Open/Write/Read VFS 路径 |
| IPC Protocol | **无改动** | 新增独立 HTTP 监听，不影响 NDJSON Unix socket |
| DriverRegistry | **只读引用** | 共享 daemon 启动时注册的驱动实例（FR152） |
| HealthCheck | **复用** | `/v1/models` 和 `/health` 复用 `DriverRegistry.HealthStatuses()` |

**文件影响范围：**

| 文件 | 变更 | 估算行数 |
|------|------|---------|
| `ipc/http_openai.go` | **新增** — OpenAI HTTP Server 核心实现 | ~250-350 |
| `ipc/http_openai_test.go` | **新增** — 单元测试 | ~200 |
| `cmd/rnix/serve.go` | **新增** — `rnix serve` Cobra 命令 | ~60 |
| `cmd/rnix/main.go` | **修改** — `runDaemon()` 可选启动 HTTP 监听 | ~15 |

**FR/NFR 覆盖：**
- FR147（rnix serve 启动）、FR148（/v1/chat/completions）、FR149（/v1/models）、FR150（SSE 流式）、FR151（provider:model 路由）、FR152（共享 daemon 配置）
- NFR50（HTTP 开销 ≤ 50ms）、NFR51（≥ 10 并发）、NFR52（仅绑定 127.0.0.1）
