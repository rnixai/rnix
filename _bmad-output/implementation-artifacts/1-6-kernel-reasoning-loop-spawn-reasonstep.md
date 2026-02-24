# Story 1.6: 内核推理循环（Spawn + reasonStep）

Status: review

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a 用户,
I want Spawn 一个智能体后，它自动执行 reasonStep 推理循环直到完成,
So that 我只需提供意图，智能体自动完成推理。

## Acceptance Criteria

1. **Spawn 系统调用** — Given `kernel/kernel.go` 中 `Spawn` 已实现，When 调用 `Spawn(intent, skills, opts)`，Then 创建新 Process（状态 Created），分配上下文（CtxAlloc），打开 `/dev/llm/claude`，And 启动独立 goroutine 执行 reasonStep 循环，And 状态转为 Running，And 返回 PID
2. **text 动作处理** — Given reasonStep 循环运行中，When LLM 返回 action 类型为 `text`（最终输出），Then 将文本作为最终结果记录，进程正常完成，And 状态转为 Zombie（exit code 0）
3. **tool_call 动作处理** — Given reasonStep 循环运行中，When LLM 返回 action 类型为 `tool_call`，Then 通过 VFS 执行对应工具调用（如 Read /dev/fs/...），And 将工具执行结果追加到上下文（CtxWrite），And 继续下一轮 reasonStep
4. **超时/失败处理** — Given reasonStep 循环运行中，When LLM 调用超时或失败，Then 进程状态在 5 秒内转为 Zombie（NFR7），And ExitStatus 记录错误信息
5. **Done channel 通知** — Given 进程完成（成功或失败），When 查看 Done channel，Then ExitStatus 写入 Done channel，阻塞的 Wait 调用被唤醒

## Tasks / Subtasks

- [x] Task 1: 扩展 KernelImpl 结构体并注入依赖 (AC: #1)
  - [x] 1.1 在 `kernel/kernel.go` 中为 KernelImpl 新增 `vfs *vfs.VFS`、`ctxMgr *context.Manager` 字段
  - [x] 1.2 更新 `NewKernel(vfs *vfs.VFS, ctxMgr *context.Manager) *KernelImpl` 构造函数签名
  - [x] 1.3 定义 `SpawnOpts` 结构体（`Model string`、`SystemPrompt string`、`MaxTurns int`、`TimeoutMs int64`）
  - [x] 1.4 定义 `SpawnResult` 或直接返回 `(types.PID, error)`

- [x] Task 2: 实现 Spawn 系统调用 (AC: #1)
  - [x] 2.1 实现 `Spawn(intent string, skills []string, opts SpawnOpts) (types.PID, error)` 方法
  - [x] 2.2 创建 Process（`NewProcess(0, intent, skills)`，PPID=0 表示顶层进程）
  - [x] 2.3 分配上下文（`ctxMgr.CtxAlloc(defaultCtxSize)`），将 CtxID 关联到 Process
  - [x] 2.4 设置 system prompt（`ctxMgr.SetSystemPrompt(cid, opts.SystemPrompt)`，非空时）
  - [x] 2.5 将初始意图追加到上下文（`ctxMgr.AppendMessage(cid, RoleUser, intent)`）
  - [x] 2.6 通过 VFS 打开 LLM 设备（`vfs.Open(pid, "/dev/llm/claude", O_RDWR)`），将 FD 保存到 Process.FDTable
  - [x] 2.7 将 Process 加入进程表（`AddProcess(proc)`）
  - [x] 2.8 启动独立 goroutine，在其中执行 `proc.Start()` 转 Running + `reasonStep` 循环
  - [x] 2.9 记录 SyscallEvent 到 DebugChan（Syscall="Spawn"，Args 含 intent/skills）
  - [x] 2.10 返回 PID

- [x] Task 3: 定义 Action 类型和解析逻辑 (AC: #2, #3)
  - [x] 3.1 定义 `ActionType string` 常量：`ActionText`、`ActionToolCall`、`ActionSpawn`
  - [x] 3.2 定义 `ReasonAction` 结构体（`Type ActionType`、`Content string`、`ToolPath string`、`ToolData []byte`）
  - [x] 3.3 实现 `parseAction(response *llm.LLMResponse) ReasonAction` — MVP 默认解析为 ActionText，将 response.Content 作为最终输出
  - [x] 3.4 MVP 预留结构化 JSON 解析路径：若 response.Content 为合法 JSON 且含 `"action"` 字段，尝试按 tool_call 解析

- [x] Task 4: 实现 reasonStep 推理循环 (AC: #2, #3, #4, #5)
  - [x] 4.1 实现 `reasonStep(k *KernelImpl, proc *Process, llmFD types.FD, opts SpawnOpts)` 函数（不导出，内部使用）
  - [x] 4.2 循环逻辑：`for` 循环，每轮检查 `proc.cancel` context 是否已取消
  - [x] 4.3 每轮开始：调用 `ctxMgr.BuildPrompt(proc.CtxID)` 组装 prompt
  - [x] 4.4 构造 LLMRequest JSON，通过 `vfs.Write(pid, llmFD, requestJSON)` 发送到 LLM 设备
  - [x] 4.5 通过 `vfs.Read(pid, llmFD, maxLength)` 读取 LLM 响应
  - [x] 4.6 解析响应 JSON 为 `LLMResponse`，调用 `parseAction` 确定动作类型
  - [x] 4.7 ActionText 处理：记录结果到 Process，调用 `proc.Terminate(ExitStatus{Code: 0, Reason: "completed"})`，写入 Done channel，退出循环
  - [x] 4.8 ActionToolCall 处理：通过 VFS Open/Write/Read/Close 执行工具调用，将结果通过 `ctxMgr.AppendToolResult` 追加到上下文，继续循环
  - [x] 4.9 错误处理：LLM 调用失败/超时时，`proc.Terminate(ExitStatus{Code: 1, Reason: "...", Err: err})`，写入 Done channel，退出循环
  - [x] 4.10 context 取消处理：检测到 `ctx.Done()` 时优雅退出，转 Zombie
  - [x] 4.11 每轮 reasonStep 记录 SyscallEvent（Syscall="ReasonStep"，Args 含 step 编号和 action 类型）
  - [x] 4.12 循环上限保护：默认 maxSteps=10（防止无限循环），超过时强制完成

- [x] Task 5: Process 上下文集成 (AC: #1)
  - [x] 5.1 在 Process 结构体中新增 `CtxID types.CtxID` 字段（由 Spawn 分配）
  - [x] 5.2 在 Process 结构体中新增 `Result string` 字段（存储最终输出）
  - [x] 5.3 在 Process 结构体中新增 `TokensUsed int` 字段（累计 token 消耗）
  - [x] 5.4 在 Process 结构体中新增 `ctx context.Context` 字段（由 Spawn 设置，用于 goroutine 取消传播）

- [x] Task 6: 编写完整单元测试 (AC: all)
  - [x] 6.1 `kernel/kernel_test.go` — Spawn 测试：正常 spawn、context 分配验证、LLM 设备打开验证、进程表注册验证、DebugChan 事件验证
  - [x] 6.2 `kernel/kernel_test.go` — reasonStep 测试：text 动作完成、tool_call 动作执行、超时处理、context 取消、maxSteps 上限
  - [x] 6.3 `kernel/kernel_test.go` — 集成测试：Spawn + reasonStep + text 完成 → Zombie → Done channel 通知
  - [x] 6.4 Mock VFS 和 Context Manager 接口注入，不依赖真实 LLM 调用
  - [x] 6.5 全量回归 `go test -race ./...` 确保不破坏已有测试

## Dev Notes

### 架构模式与约束

- **文件位置严格遵循架构文档：** 核心逻辑在 `kernel/kernel.go`，新增类型可放在 `kernel/` 目录下的新文件
- **依赖方向：** `kernel/` → `vfs/`（VFS 操作）✓；`kernel/` → `context/`（上下文管理）✓；`kernel/` → `internal/types/` ✓；`kernel/` → `internal/xsync/` ✓。**绝对禁止** `kernel/` 导入 `cmd/` 或 `internal/ui/` 或 `drivers/`
- **此 Story 实现的核心：** Spawn syscall + reasonStep 推理循环 + Action 解析 + LLM 调用编排 + 进程完成通知
- **此 Story 不实现：** CLI 入口（Story 1.7）、Skill 加载与注入（Story 2.4）、astrace 集成（Story 3.1）、Kill/Wait syscall（Story 4.1）、/proc 文件系统（Story 4.3）
- **Kernel 不直接导入 `drivers/llm/`**：LLM 交互通过 VFS 抽象进行（Open → Write → Read → Close），kernel 包只知道 VFS 接口和 LLMRequest/LLMResponse 的 JSON 格式

### 已有代码（必须复用，禁止重新实现）

**`kernel/kernel.go` — 当前 KernelImpl：**

```go
type KernelImpl struct {
    procTable *xsync.SyncMap[types.PID, *Process]
}

func NewKernel() *KernelImpl
func (k *KernelImpl) AddProcess(p *Process)
func (k *KernelImpl) GetProcess(pid types.PID) (*Process, bool)
func (k *KernelImpl) RemoveProcess(pid types.PID)
func (k *KernelImpl) ListProcesses() []*Process

// 包级 PID 分配
var pidCounter atomic.Uint64
func nextPID() types.PID
```

需要扩展 KernelImpl 添加 `vfs` 和 `ctxMgr` 字段，更新 `NewKernel` 签名。

**`kernel/process.go` — Process 结构体和状态机：**

```go
type ExitStatus struct {
    Code   int
    Reason string
    Err    error
}

type Process struct {
    PID       types.PID
    PPID      types.PID
    State     types.ProcessState        // mu 保护
    Intent    string                    // 不可变
    Skills    []string
    Children  []types.PID
    FDTable   map[types.FD]vfs.VFSFile  // 进程内 FD 表
    DebugChan chan types.SyscallEvent   // 缓冲 256
    Done      chan ExitStatus           // 缓冲 1
    CreatedAt time.Time
    Exit      *ExitStatus              // Zombie/Dead 时非 nil
    mu        sync.Mutex
    cancel    context.CancelFunc
    wg        sync.WaitGroup
}

func NewProcess(ppid types.PID, intent string, skills []string) *Process
func (p *Process) GetState() types.ProcessState
func (p *Process) Transition(target types.ProcessState) error
func (p *Process) Start() error       // Created → Running
func (p *Process) Terminate(exit ExitStatus) error  // Running → Zombie
func (p *Process) Reap() error        // Zombie → Dead
```

需要新增字段：`CtxID types.CtxID`、`Result string`、`TokensUsed int`、`ctx context.Context`。

**`context/context.go` — 上下文管理 API：**

```go
type Manager struct { ... }
func NewManager() *Manager

// 生命周期
func (m *Manager) CtxAlloc(size int) (types.CtxID, error)
func (m *Manager) CtxFree(cid types.CtxID) error

// 高层方法（reasonStep 主要使用这些）
func (m *Manager) SetSystemPrompt(cid types.CtxID, prompt string) error
func (m *Manager) AppendMessage(cid types.CtxID, role Role, content string) error
func (m *Manager) AppendToolResult(cid types.CtxID, toolCallID string, content string) error
func (m *Manager) BuildPrompt(cid types.CtxID) (*PromptResult, error)

// PromptResult 输出
type PromptResult struct {
    SystemPrompt string
    Messages     []Message
}
```

**`vfs/vfs.go` — VFS 操作接口：**

```go
func (v *VFS) Open(pid types.PID, path string, flags OpenFlag) (types.FD, error)
func (v *VFS) Read(pid types.PID, fd types.FD, length int) ([]byte, error)
func (v *VFS) Write(pid types.PID, fd types.FD, data []byte) error
func (v *VFS) Close(pid types.PID, fd types.FD) error
func (v *VFS) CloseAll(pid types.PID) error
```

**`drivers/llm/driver.go` — LLM 请求/响应 JSON 格式：**

```go
// VFS Write 的 data 参数格式（JSON）
type LLMRequest struct {
    Intent       string `json:"intent"`
    SystemPrompt string `json:"system_prompt,omitempty"`
    Model        string `json:"model,omitempty"`
    MaxTurns     int    `json:"max_turns,omitempty"`
    TimeoutMs    int64  `json:"timeout_ms,omitempty"`
}

// VFS Read 的返回格式（JSON）
type LLMResponse struct {
    Content    string `json:"content"`
    TokensUsed int    `json:"tokens_used"`
}
```

**注意：** kernel 包不导入 `drivers/llm/`。kernel 需要在本地定义与 `LLMRequest`/`LLMResponse` 兼容的 JSON 结构体进行序列化/反序列化。

### 关键设计决策

**1. reasonStep 循环与 VFS LLM 调用流程**

```
Spawn(intent, skills, opts)
  → NewProcess() [Created]
  → ctxMgr.CtxAlloc()
  → ctxMgr.SetSystemPrompt() (如有)
  → ctxMgr.AppendMessage(RoleUser, intent)
  → vfs.Open("/dev/llm/claude", O_RDWR)
  → AddProcess()
  → goroutine:
      proc.Start() [Running]
      for step := 1; step <= maxSteps; step++ {
          prompt := ctxMgr.BuildPrompt(ctxID)
          requestJSON := serializeLLMRequest(prompt, opts)
          vfs.Write(pid, llmFD, requestJSON)
          responseJSON := vfs.Read(pid, llmFD, maxLength)
          response := deserializeLLMResponse(responseJSON)
          action := parseAction(response)

          switch action.Type {
          case ActionText:
              proc.Result = action.Content
              proc.TokensUsed += response.TokensUsed
              proc.Terminate(ExitStatus{Code: 0})
              proc.Done <- exitStatus
              return
          case ActionToolCall:
              // 通过 VFS 执行工具
              toolFD := vfs.Open(pid, action.ToolPath, O_RDWR)
              vfs.Write(pid, toolFD, action.ToolData)
              toolResult := vfs.Read(pid, toolFD, maxLength)
              vfs.Close(pid, toolFD)
              // 追加到上下文
              ctxMgr.AppendToolResult(ctxID, toolCallID, string(toolResult))
              continue
          }
      }
```

**2. Kernel 与 LLM 驱动的隔离**

Kernel 通过 VFS 抽象与 LLM 交互，不直接引用 `drivers/llm/` 包。这意味着：
- kernel 包需要本地定义 LLM 请求/响应的 JSON 序列化结构（与 `drivers/llm` 中的结构兼容但独立定义）
- 或者，在 `internal/types/` 中定义共享的 LLM 请求/响应类型
- **推荐方案：** kernel 包内定义私有的 `llmRequest`/`llmResponse` 结构体，字段和 json tag 与 `drivers/llm` 中的对齐

```go
// kernel/kernel.go 内部（不导出）
type llmRequest struct {
    Intent       string `json:"intent"`
    SystemPrompt string `json:"system_prompt,omitempty"`
    Model        string `json:"model,omitempty"`
    MaxTurns     int    `json:"max_turns,omitempty"`
    TimeoutMs    int64  `json:"timeout_ms,omitempty"`
}

type llmResponse struct {
    Content    string `json:"content"`
    TokensUsed int    `json:"tokens_used"`
}
```

**3. MVP Action 解析策略**

MVP 阶段，Claude CLI 使用 `--max-turns 1` 返回最终文本。reasonStep 的 action 解析：
- **默认行为：** LLM 响应内容视为 `ActionText`（最终输出）
- **tool_call 检测预留：** 若响应 JSON 含 `"action": "tool_call"` 字段，解析为 `ActionToolCall`
- **tool_call 完整功能：** 代码路径存在，通过 mock 测试验证，真实 tool 调用在 Story 2.x 可用后自动生效
- **parseAction 实现：** 先尝试 JSON 解析（检测结构化 action），失败则作为纯文本

```go
func parseAction(resp *llmResponse) ReasonAction {
    // 尝试解析为结构化 action
    var structured struct {
        Action  string         `json:"action"`
        Content string         `json:"content,omitempty"`
        Tool    string         `json:"tool,omitempty"`
        Data    map[string]any `json:"data,omitempty"`
    }
    if err := json.Unmarshal([]byte(resp.Content), &structured); err == nil {
        if structured.Action == "tool_call" && structured.Tool != "" {
            return ReasonAction{Type: ActionToolCall, ToolPath: structured.Tool, ...}
        }
    }
    // 默认：纯文本输出
    return ReasonAction{Type: ActionText, Content: resp.Content}
}
```

**4. goroutine 生命周期管理**

```go
func (k *KernelImpl) Spawn(...) (types.PID, error) {
    ctx, cancel := context.WithCancel(context.Background())
    proc := NewProcess(0, intent, skills)
    proc.cancel = cancel
    proc.ctx = ctx
    // ... allocate context, open LLM device ...

    proc.wg.Add(1)
    go func() {
        defer proc.wg.Done()
        proc.Start() // Created → Running
        k.reasonStep(proc, llmFD, opts)
        // reasonStep 内部处理 Terminate
    }()

    return proc.PID, nil
}
```

**5. DebugChan 事件记录模式**

所有 syscall 级操作在 Spawn 和 reasonStep 中都需要记录 SyscallEvent：

```go
func (k *KernelImpl) emitEvent(proc *Process, syscall string, args map[string]any, result any, err error, duration time.Duration) {
    if proc.DebugChan == nil {
        return
    }
    event := types.SyscallEvent{
        Timestamp: time.Since(proc.CreatedAt),
        PID:       proc.PID,
        Syscall:   syscall,
        Args:      args,
        Result:    result,
        Err:       err,
        Duration:  duration,
    }
    select {
    case proc.DebugChan <- event:
    default: // 缓冲满时丢弃，不阻塞
    }
}
```

**6. 错误处理与 Zombie 转移**

```go
// reasonStep 中的错误处理模板
func (k *KernelImpl) reasonStep(proc *Process, llmFD types.FD, opts SpawnOpts) {
    defer func() {
        // 确保进程一定转为 Zombie（无论成功/失败）
        if proc.GetState() == types.StateRunning {
            proc.Terminate(ExitStatus{Code: 1, Reason: "unexpected exit"})
        }
    }()

    // ... 循环逻辑 ...

    if err != nil {
        exitStatus := ExitStatus{Code: 1, Reason: err.Error(), Err: err}
        proc.Terminate(exitStatus)
        proc.Done <- exitStatus
        return
    }
}
```

### Go 代码命名规则（必须遵循）

| 对象 | 规则 | 示例 |
|------|------|------|
| 包名 | 全小写 | `kernel` |
| 导出类型 | PascalCase | `KernelImpl`、`SpawnOpts`、`ActionType`、`ReasonAction` |
| 非导出类型 | camelCase | `llmRequest`、`llmResponse`（与 drivers/llm 对齐但不导入） |
| 导出函数 | PascalCase | `NewKernel`、`Spawn` |
| 非导出函数 | camelCase | `reasonStep`、`parseAction`、`emitEvent` |
| 方法接收器 | 简短 | `k *KernelImpl`、`p *Process` |
| 常量 | PascalCase | `ActionText`、`ActionToolCall`、`DefaultMaxSteps`、`DefaultCtxSize` |
| 文件名 | 下划线分隔 | `kernel.go`、`kernel_test.go` |

### 测试规范

**测试文件位置：**
- `kernel/kernel_test.go`（扩展已有文件）

**Mock 策略：**

Kernel 依赖 VFS 和 Context Manager。测试中需要 mock 这两个组件：

1. **Mock VFS：** 创建真实的 VFS + DeviceRegistry + mock LLM VFSFile
   - Mock LLMFile 在 Write 时捕获请求 JSON，在 Read 时返回预设的响应 JSON
   - 这种方式测试完整的 VFS 调用链

2. **Mock Context Manager：** 使用真实的 `context.NewManager()`
   - Context Manager 是内存实现，无需 mock
   - 直接验证 CtxAlloc、AppendMessage、BuildPrompt 的集成

3. **Mock Tool VFSFile：** 测试 tool_call 路径时，注册 mock 工具设备

```go
// 测试辅助：Mock LLM VFSFile
type mockLLMFile struct {
    writeData []byte
    readData  []byte
    closed    bool
}

func (f *mockLLMFile) Write(data []byte) error {
    f.writeData = data
    return nil
}
func (f *mockLLMFile) Read(length int) ([]byte, error) {
    return f.readData, nil
}
func (f *mockLLMFile) Close() error { f.closed = true; return nil }
func (f *mockLLMFile) Stat() (vfs.FileStat, error) { return vfs.FileStat{IsDevice: true}, nil }
```

**必须包含的测试场景：**

| 测试 | 验证点 |
|------|--------|
| `TestSpawn_Success` | 正常 spawn：Process 创建、CtxID 分配、LLM 设备打开、进程表注册、状态为 Running |
| `TestSpawn_ContextAllocFailure` | ctxMgr 分配失败：返回错误，不创建 Process |
| `TestSpawn_VFSOpenFailure` | VFS Open 失败：返回错误，清理已分配的 context |
| `TestReasonStep_TextAction` | text 动作：LLM 返回纯文本 → Process 完成、Zombie(0)、Done channel 收到 ExitStatus |
| `TestReasonStep_ToolCallAction` | tool_call 动作：mock tool 设备 → 工具执行 → 结果追加上下文 → 第二轮 LLM 返回 text → 完成 |
| `TestReasonStep_LLMError` | LLM 调用失败：VFS Write/Read 返回错误 → Zombie(1)、Done channel 记录错误 |
| `TestReasonStep_ContextCancellation` | context 取消：外部 cancel → reasonStep 退出 → Zombie |
| `TestReasonStep_MaxStepsExceeded` | 超过最大步数：强制完成 → Zombie |
| `TestSpawn_DebugChanEvents` | 验证 Spawn 和 reasonStep 的 SyscallEvent 记录 |
| `TestSpawn_Integration` | 端到端：Spawn → reasonStep → text → Zombie → Done channel（使用真实 VFS + mock LLM 设备 + 真实 Context Manager） |

**测试模式：**
- 使用 Go 标准 `testing` 包，`t.Run` 子测试
- `t.Fatal` / `t.Fatalf` / `t.Errorf`
- 通过 mock VFSFile 注入预设响应
- 全部通过 `go test -race ./kernel/...`

### 前序 Story 经验教训（必须吸收）

1. **data race 敏感（Story 1-2）：** Process.State 受 `mu` 保护。reasonStep goroutine 中访问 Process 字段时注意并发安全。新增的 `Result`、`TokensUsed`、`CtxID` 字段只在 reasonStep goroutine 中写入（单写者），但读取需要考虑并发访问
2. **VFSFile 生命周期（Story 1-3）：** VFS.Open 返回的 FD 必须在 reasonStep 结束后正确 Close。使用 `vfs.CloseAll(pid)` 统一清理
3. **LLMFile Write-then-Read 语义（Story 1-5）：** Write 触发实际 CLI 调用（同步阻塞），Read 返回缓冲结果。每次新的 LLM 调用需要重新 Write
4. **驱动错误传播链（Story 1-5）：** 驱动返回标准 error → VFSFile.Write 原样传递 → VFS.Write 包装为 VFSError → Kernel 包装为 SyscallError
5. **Context Manager 的 AppendMessage vs CtxWrite（Story 1-4）：** 高层方法（AppendMessage、AppendToolResult、BuildPrompt）比底层的 CtxWrite/CtxRead 更安全和方便。reasonStep 应使用高层方法
6. **VFSFileFactory subpath 参数（Story 1-3）：** `/dev/llm/claude` 精确匹配时 subpath 为空字符串。注册设备时使用精确路径
7. **测试使用 `t.Logf` 不用 `fmt.Printf`**

### Git 智能（最近工作模式）

**最近 5 个提交分析：**

| 提交 | 内容 | 启示 |
|------|------|------|
| `91d8d3d` | Story 1-5 增强和修复 | LLMFile basePath 参数化、Stream 测试、TimeoutMs 修复 |
| `0f48087` | Story 1-5 Code Review 修复 | 代码审查发现的问题修复模式 |
| `7d3d176` | Story 1-5 初始实现 | LLM 驱动层完整实现模式 |
| `318e25a` | Story 1-4 Context 实现 | Context Manager 模式：AppendMessage/BuildPrompt 高层 API |
| `6ba2532` | Story 1-3 VFS 实现 | VFS + DeviceRegistry + FDTable 完整模式 |

**代码惯例提取：**
- 包级文档注释：`// Package kernel implements the Crux microkernel.`
- 构造函数：`NewXxx()` 模式
- 方法接收器：简短单字母（`k *KernelImpl`）
- 测试分组：`t.Run("子测试名", func(t *testing.T) {...})`
- 导入分组：标准库 → 空行 → 项目内部包

### Project Structure Notes

**本 Story 修改的文件：**

```
kernel/
├── kernel.go          (修改 — 扩展 KernelImpl + 新增 Spawn + reasonStep + Action 解析)
├── process.go         (修改 — 新增 CtxID/Result/TokensUsed/ctx 字段)
├── kernel_test.go     (修改 — 新增 Spawn/reasonStep 测试)
```

**可能新增的文件：**
- `kernel/spawn.go` — 如果 kernel.go 变得太大，可将 Spawn 和 reasonStep 拆到独立文件（可选，视实际代码量决定）

**不要创建的文件：**
- `kernel/action.go` — Action 类型数量少，放在 kernel.go 中即可
- `kernel/llm.go` — LLM 请求/响应结构体数量少，作为 kernel.go 的内部类型
- `kernel/types.go` — 新增类型少，不需要独立文件

**不要触碰的文件：**
- `vfs/` 下任何文件（接口已定义，无需修改）
- `context/` 下任何文件（API 已满足需求）
- `drivers/` 下任何文件（LLM 驱动通过 VFS 抽象访问）
- `internal/types/types.go`（类型已满足需求）
- `internal/xsync/` 下任何文件
- `cmd/crux/main.go`（依赖注入在 Story 1.7）

**注意更新已有测试：**
- `kernel/kernel_test.go` 中现有的 `TestNewKernel`、`TestAddGetProcess` 等测试可能需要更新 `NewKernel` 调用签名（从无参数改为接受 VFS + ctxMgr 参数）

### References

- [Source: architecture.md#Decision 1: Syscall ABI 设计风格] — Kernel 分类接口组合设计
- [Source: architecture.md#Decision 2: 进程模型与并发] — Process 结构体、goroutine 生命周期管理
- [Source: architecture.md#Decision 3: VFS 实现策略] — VFSFile 接口、FDTable 管理
- [Source: architecture.md#Decision 4: Claude Code CLI 集成] — 调用模式、参数模板、超时处理
- [Source: architecture.md#Decision 5: 调试架构（astrace）] — DebugChan 事件传递机制
- [Source: architecture.md#Decision 6: 错误处理与恢复] — SyscallError 传播层次
- [Source: architecture.md#Project Structure & Boundaries] — Kernel ↔ VFS 边界、Kernel ↔ Context 边界
- [Source: architecture.md#Implementation Patterns > 过程模式] — context.Context 传播规则、进程状态转移规则、资源释放顺序
- [Source: architecture.md#Implementation Patterns > 通信模式] — Channel 使用规则、SyscallEvent 事件命名
- [Source: epics.md#Story 1.6] — 原始用户故事和验收标准
- [Source: prd.md#FR1] — 通过自然语言意图创建智能体进程
- [Source: prd.md#FR2] — 管理进程完整生命周期状态机
- [Source: prd.md#FR8] — 驱动智能体执行 reasonStep 推理循环
- [Source: prd.md#FR9] — 通过 LLM 驱动层非交互调用 LLM
- [Source: prd.md#FR10] — 解析 LLM 响应 action 类型
- [Source: prd.md#FR11] — LLM 超时/失败时进程正确转入 Zombie
- [Source: prd.md#FR12] — 工具执行结果追加到智能体上下文
- [Source: prd.md#NFR7] — 超时 5 秒内转 Zombie
- [Source: prd.md#NFR8] — 退出 10 秒内释放资源
- [Source: prd.md#NFR11] — 正确传递 system prompt、模型选择、输出格式
- [Source: project-context.md#Critical Implementation Rules] — 全部关键实现规则
- [Source: project-context.md#架构框架规则 > 进程状态机] — 状态转移规则
- [Source: project-context.md#架构框架规则 > Claude Code CLI 集成] — 调用模式
- [Source: project-context.md#关键防错规则] — 禁止反向依赖、禁止裸 error
- [Source: 1-5-llm-driver-claude-code-cli.md] — 前序 Story 完整产出、VFSFile Write-then-Read 语义、LLM 请求/响应 JSON 格式、错误传播链、经验教训

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6

### Debug Log References

### Completion Notes List

- **Task 1 完成**: KernelImpl 新增 `vfs *vfs.VFS` 和 `ctxMgr *cruxctx.Manager` 字段；`NewKernel` 签名更新为接受 VFS + ctxMgr 参数；定义 `SpawnOpts` 结构体；返回 `(types.PID, error)`
- **Task 2 完成**: 实现 `Spawn` 方法 — 创建 Process、分配 Context、设置 system prompt、追加 intent 到上下文、VFS 打开 LLM 设备、goroutine 启动 reasonStep 循环、emitEvent 记录 SyscallEvent
- **Task 3 完成**: 定义 `ActionType` 常量（ActionText/ActionToolCall/ActionSpawn）、`ReasonAction` 结构体、`parseAction` 函数（先尝试 JSON 结构化解析 tool_call，失败则作为纯文本）
- **Task 4 完成**: 实现 `reasonStep` 循环 — BuildPrompt → Write LLM → Read LLM → parseAction → ActionText 完成 / ActionToolCall 执行 / 错误处理 / context 取消 / maxSteps 上限保护；所有路径确保进程转 Zombie 并写入 Done channel
- **Task 5 完成**: Process 新增 `CtxID`、`Result`、`TokensUsed`、`ctx context.Context` 字段
- **Task 6 完成**: 17 个测试函数覆盖：Spawn 成功/失败/SystemPrompt、reasonStep text/tool_call/LLM 写错误/LLM 读错误/context 取消/maxSteps 超限、parseAction 纯文本/JSON/tool_call/无效 JSON/缺少 tool 字段、端到端集成测试。全量 `go test -race ./...` 通过
- **设计决策**: kernel 包内定义私有 `llmRequest`/`llmResponse` 结构体（json tag 与 drivers/llm 对齐但不导入）；使用 `sequenceLLMFile` mock 实现多轮 LLM 交互测试；DebugChan 非阻塞写入（满时丢弃）
- **已有测试兼容**: 更新了原有 5 个 kernel 测试的 `NewKernel` 调用为新签名（通过 `newSimpleKernel()` 辅助函数）

### File List

- `kernel/kernel.go` — 修改：扩展 KernelImpl（vfs/ctxMgr 字段）、新增 Spawn/reasonStep/parseAction/emitEvent、定义 SpawnOpts/ActionType/ReasonAction/llmRequest/llmResponse 类型
- `kernel/process.go` — 修改：Process 新增 CtxID/Result/TokensUsed/ctx 字段
- `kernel/kernel_test.go` — 修改：更新现有测试的 NewKernel 签名 + 新增 17 个 Spawn/reasonStep/parseAction/集成测试
