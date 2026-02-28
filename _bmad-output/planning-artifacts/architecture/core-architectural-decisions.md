# Core Architectural Decisions

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

## Decision 4: Claude Code CLI 集成

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

## Decision 5: 调试架构（astrace）

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

## Go 1.26 特性利用

| 特性 | Crux 使用场景 |
|------|-------------|
| **Green Tea GC**（默认启用） | 自动受益——astrace 高吞吐事件流的 GC 压力降低 |
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
