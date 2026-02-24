# Story 1.5: LLM 驱动层（Claude Code CLI）

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a 智能体,
I want 通过 `/dev/llm/claude` 设备调用 Claude Code CLI 进行 LLM 推理,
So that 我可以获得 LLM 的结构化响应来完成任务。

## Acceptance Criteria

1. **LLMDriver 接口** — Given `drivers/llm/driver.go` 已实现，When 查看 LLMDriver 接口，Then 包含 `Call(ctx, req) (*LLMResponse, error)`、`Stream(ctx, req) (<-chan StreamEvent, error)`、`Info() DriverInfo` 方法
2. **ClaudeCliDriver 调用** — Given `drivers/llm/claude_cli.go` 已实现，When 调用 `ClaudeCliDriver.Call(ctx, req)`，Then 通过 `exec.CommandContext` 执行 `claude -p "{intent}" --output-format json`，And 正确传递 `--system-prompt`、`--model`、`--max-turns` 参数（NFR11），And 解析 JSON 输出为 `LLMResponse` 结构
3. **LLMResponse 解析** — Given LLM 调用成功，When 解析响应，Then `LLMResponse` 包含 `Content`（文本内容）和 `TokensUsed`（消耗 token 数）
4. **超时处理** — Given LLM 调用超时（默认 30 秒），When `context.WithTimeout` 到期，Then `cmd.Process.Kill()` 终止 CLI 进程，And 返回错误包含超时信息
5. **驱动注册表** — Given `drivers/llm/registry.go` 已实现，When 注册 Claude 驱动，Then 基于 `Registry[LLMDriver]`，支持按路径查找驱动

## Tasks / Subtasks

- [x] Task 1: 定义 LLM 驱动核心类型与接口 (AC: #1, #3)
  - [x] 1.1 在 `drivers/llm/driver.go` 中定义 `LLMRequest` 结构体（`Intent string`、`SystemPrompt string`、`Model string`、`MaxTurns int`、`TimeoutMs int64`）
  - [x] 1.2 定义 `LLMResponse` 结构体（`Content string`、`TokensUsed int`）
  - [x] 1.3 定义 `StreamEvent` 结构体（`Type string`（"content"/"done"/"error"）、`Content string`、`TokensUsed int`、`Err error`）
  - [x] 1.4 定义 `DriverInfo` 结构体（`Name string`、`Provider string`、`DefaultModel string`）
  - [x] 1.5 定义 `LLMDriver` 接口：`Call(ctx context.Context, req LLMRequest) (*LLMResponse, error)`、`Stream(ctx context.Context, req LLMRequest) (<-chan StreamEvent, error)`、`Info() DriverInfo`
- [x] Task 2: 实现 ClaudeCliDriver (AC: #2, #3, #4)
  - [x] 2.1 在 `drivers/llm/claude_cli.go` 中定义 `ClaudeCliDriver` 结构体（`defaultModel string`、`defaultTimeout time.Duration`、`cmdBuilder CommandBuilder`）
  - [x] 2.2 定义 `CommandBuilder` 函数类型 `func(ctx context.Context, name string, args ...string) *exec.Cmd`，用于测试注入
  - [x] 2.3 实现 `NewClaudeCliDriver(opts ...ClaudeCliOption) *ClaudeCliDriver`（函数选项模式，默认 model="sonnet"、timeout=30s、cmdBuilder=exec.CommandContext）
  - [x] 2.4 实现 `Call(ctx, req)` 方法：构建 `claude -p <intent> --output-format json` 命令，条件追加 `--system-prompt`（非空时）、`--model`（req.Model 或 defaultModel）、`--max-turns`（req.MaxTurns 或默认 1），用 `context.WithTimeout` 包装，执行命令捕获 stdout/stderr，解析 JSON 输出为 `LLMResponse`，超时时返回 timeout 错误
  - [x] 2.5 实现 `Stream(ctx, req)` 方法：基于 `--output-format stream-json` + `bufio.Scanner` 的基础实现，为 Story 3.1 astrace 集成打基础
  - [x] 2.6 实现 `Info()` 方法：返回 `DriverInfo{Name: "claude-cli", Provider: "claude", DefaultModel: d.defaultModel}`
  - [x] 2.7 定义 `ClaudeCliOption` 函数选项类型及实现（`WithModel`、`WithTimeout`、`WithCommandBuilder`）
- [x] Task 3: 实现 VFSFile 适配器 (AC: #1, #2)
  - [x] 3.1 在 `drivers/llm/vfsfile.go` 中定义 `LLMFile` 结构体（`driver LLMDriver`、`request []byte` 缓冲请求、`response []byte` 缓冲响应、`closed bool`）
  - [x] 3.2 实现 `Write(data []byte) error`：JSON 解析 data 为 LLMRequest，调用 `driver.Call()`，将 LLMResponse 序列化为 JSON 缓冲到 response
  - [x] 3.3 实现 `Read(length int) ([]byte, error)`：返回缓冲的响应数据（截取 length 长度）
  - [x] 3.4 实现 `Close() error`：标记 closed，清理缓冲
  - [x] 3.5 实现 `Stat() (vfs.FileStat, error)`：返回 `FileStat{Name: devicePath, IsDevice: true, DevicePath: devicePath}`
  - [x] 3.6 实现 `FileFactory(driver LLMDriver, basePath string) vfs.VFSFileFactory`：返回工厂闭包供 DeviceRegistry 注册，basePath 参数化设备路径
- [x] Task 4: 实现 LLM 驱动注册表 (AC: #5)
  - [x] 4.1 在 `drivers/llm/registry.go` 中定义 `DriverRegistry` 结构体（封装 `xsync.Registry[LLMDriver]`）
  - [x] 4.2 实现 `NewDriverRegistry() *DriverRegistry`
  - [x] 4.3 实现 `Register(path string, driver LLMDriver) error`
  - [x] 4.4 实现 `Get(path string) (LLMDriver, bool)`
- [x] Task 5: 编写完整单元测试 (AC: all)
  - [x] 5.1 `drivers/llm/claude_cli_test.go` — ClaudeCliDriver 测试：Mock CommandBuilder 注入（TestHelperProcess 模式），正常调用 + 超时 + CLI 错误 + 参数验证 + 默认参数
  - [x] 5.2 `drivers/llm/vfsfile_test.go` — VFSFile 适配器测试：Write+Read 流程、Close 后访问、FileFactory 创建
  - [x] 5.3 `drivers/llm/registry_test.go` — 注册表测试：注册/查找/重复注册/未注册
  - [x] 5.4 全量回归 `go test -race ./...` 确保不破坏已有测试

## Dev Notes

### 架构模式与约束

- **文件位置严格遵循架构文档：** `drivers/llm/` 目录下，文件名遵循全小写下划线分隔
- **依赖方向：** `drivers/llm/` → `internal/types/` ✓；`drivers/llm/` → `internal/xsync/` ✓；`drivers/llm/` → `vfs/`（仅类型引用 VFSFile/VFSFileFactory/FileStat/OpenFlag）✓。**绝对禁止** `drivers/llm/` 导入 `kernel/` 或 `context/`
- **此 Story 实现的核心：** LLMDriver 接口 + ClaudeCliDriver 实现 + VFSFile 适配器 + 驱动注册表
- **此 Story 不实现：** reasonStep 中的 LLM 调用逻辑（Story 1.6）、Skill 的 system prompt 注入（Story 2.4）、stream-json 的完整 astrace 集成（Story 3.1）、DeviceRegistry 注册调用（Story 1.7 `cmd/crux/main.go`）
- **MVP 限制：** `--max-turns 1`（单轮对话），Stream 方法提供基础 stream-json 实现为后续 Story 打基础

### 已有代码（必须复用，禁止重新实现）

**`internal/types/types.go` — 已定义的类型：**

```go
type PID uint64
type FD int
type CtxID uint64
type ErrCode string

const (
    ErrTimeout    ErrCode = "TIMEOUT"
    ErrNotFound   ErrCode = "NOT_FOUND"
    ErrPermission ErrCode = "PERMISSION"
    ErrInternal   ErrCode = "INTERNAL"
    ErrDriver     ErrCode = "DRIVER"
)

type SyscallEvent struct {
    Timestamp time.Duration
    PID       PID
    Syscall   string
    Args      map[string]any
    Result    any
    Err       error
    Duration  time.Duration
}
```

**`internal/xsync/registry.go` — 驱动注册表基础：**

```go
type Registry[T any] struct { mu sync.RWMutex; items map[string]T }
func NewRegistry[T any]() *Registry[T]
func (r *Registry[T]) Register(name string, item T) error  // 已存在则返回错误
func (r *Registry[T]) Get(name string) (T, bool)
func (r *Registry[T]) List() []T
func (r *Registry[T]) Range(fn func(string, T) bool)
```

DriverRegistry 必须封装 `Registry[LLMDriver]`，禁止手写 `map + mutex`。

**`vfs/vfs.go` — VFSFile 接口和 VFSFileFactory（LLMFile 必须实现）：**

```go
type VFSFile interface {
    Read(length int) ([]byte, error)
    Write(data []byte) error
    Close() error
    Stat() (FileStat, error)
}

// subpath = 前缀匹配后的剩余路径（精确匹配时为空字符串）
type VFSFileFactory func(subpath string, flags OpenFlag) (VFSFile, error)

type FileStat struct {
    Name       string
    Size       int64
    IsDevice   bool
    DevicePath string
}

type OpenFlag int
const (
    O_RDONLY OpenFlag = iota
    O_WRONLY
    O_RDWR
)
```

**`vfs/dev.go` — DeviceRegistry 注册模式（供理解注册流程）：**

```go
type DeviceRegistry struct { registry *xsync.Registry[VFSFileFactory] }
func (d *DeviceRegistry) Register(path string, factory VFSFileFactory) error
func (d *DeviceRegistry) Open(path string, flags OpenFlag) (VFSFile, error)  // 支持精确+前缀匹配
```

将在 `cmd/crux/main.go` 中执行（Story 1.7）：

```go
claudeDriver := llm.NewClaudeCliDriver()
devRegistry.Register("/dev/llm/claude", llm.FileFactory(claudeDriver, "/dev/llm/claude"))
```

**`kernel/errors.go` — SyscallError（drivers 包不直接使用）：**

```go
type SyscallError struct {
    Syscall string; PID types.PID; Device string; Err error; Code types.ErrCode
}
```

drivers 包不导入 kernel 包，所以不能直接使用 SyscallError。驱动返回标准 error，由 VFS 层包装为 VFSError，由 kernel 层包装为 SyscallError。

**`context/context.go` — BuildPrompt 输出（LLM 驱动消费者）：**

```go
type PromptResult struct {
    SystemPrompt string    // → LLMRequest.SystemPrompt 或 --system-prompt 参数
    Messages     []Message // → LLMRequest.Intent 或 -p 参数（Story 1.6 拼接）
}
```

Story 1.6 的 reasonStep 将调用 `BuildPrompt()` 获取 `PromptResult`，然后构造 `LLMRequest` 传给 driver.Call()。本 Story 只需确保 `LLMRequest` 字段能承载 `PromptResult` 的内容。

### 关键设计决策

**1. exec.Command 可测试性——CommandBuilder 注入**

真实环境调用 `exec.CommandContext`，测试中注入 mock。这是架构文档推荐的 Driver 测试模式。

```go
// 生产代码
type CommandBuilder func(ctx context.Context, name string, args ...string) *exec.Cmd

// 默认使用真实 exec
func defaultCommandBuilder(ctx context.Context, name string, args ...string) *exec.Cmd {
    return exec.CommandContext(ctx, name, args...)
}
```

推荐使用 Go 标准的 `TestHelperProcess` 模式来 mock exec.Command：

```go
// 测试文件中
func TestHelperProcess(t *testing.T) {
    if os.Getenv("GO_TEST_PROCESS") != "1" {
        return
    }
    switch os.Getenv("GO_TEST_CASE") {
    case "success":
        fmt.Fprintf(os.Stdout, `{"type":"result","subtype":"success","result":"test output","cost_usd":0.001,"is_error":false,"duration_ms":100,"num_turns":1}`)
    case "error":
        fmt.Fprintf(os.Stderr, "Error: something went wrong")
        os.Exit(1)
    case "timeout":
        time.Sleep(5 * time.Second)
    }
    os.Exit(0)
}

func mockCmdBuilder(testCase string) CommandBuilder {
    return func(ctx context.Context, name string, args ...string) *exec.Cmd {
        cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=TestHelperProcess")
        cmd.Env = append(os.Environ(), "GO_TEST_PROCESS=1", "GO_TEST_CASE="+testCase)
        return cmd
    }
}
```

**2. Claude Code CLI JSON 输出格式**

`claude -p "..." --output-format json` 的输出结构：

```json
{
  "type": "result",
  "subtype": "success",
  "cost_usd": 0.003,
  "is_error": false,
  "duration_ms": 2500,
  "duration_api_ms": 2200,
  "num_turns": 1,
  "result": "分析结果文本...",
  "session_id": "..."
}
```

关键字段提取映射：

| CLI JSON 字段 | LLMResponse 字段 | 说明 |
|---------------|-----------------|------|
| `result` | `Content` | LLM 返回的文本内容 |
| `cost_usd` | — | 可选记录，MVP 不需要 |
| `is_error` | 判断是否成功 | `true` 时 result 字段包含错误消息 |
| `duration_ms` | — | 可选记录，用于性能追踪 |
| `num_turns` | `TokensUsed`（估算） | MVP 可用 `num_turns` 作为简化的 token 使用指标，或设为 0 |

定义内部解析结构体（非导出）：

```go
type claudeCliResponse struct {
    Type       string  `json:"type"`
    Subtype    string  `json:"subtype"`
    Result     string  `json:"result"`
    IsError    bool    `json:"is_error"`
    CostUSD    float64 `json:"cost_usd"`
    DurationMS int     `json:"duration_ms"`
    NumTurns   int     `json:"num_turns"`
    SessionID  string  `json:"session_id"`
}
```

**注意：** Claude CLI 输出格式可能随版本变化。使用 `json.Unmarshal` 自动忽略未知字段（Go 默认行为），仅提取已知字段。不要使用 `json.Decoder.DisallowUnknownFields()`。

**3. 超时策略**

```go
func (d *ClaudeCliDriver) Call(ctx context.Context, req LLMRequest) (*LLMResponse, error) {
    timeout := req.Timeout
    if timeout == 0 {
        timeout = d.defaultTimeout // 30s
    }
    ctx, cancel := context.WithTimeout(ctx, timeout)
    defer cancel()

    cmd := d.cmdBuilder(ctx, "claude", args...)
    // exec.CommandContext 在 ctx 取消时自动发送 os.Kill
    var stdout, stderr bytes.Buffer
    cmd.Stdout = &stdout
    cmd.Stderr = &stderr

    err := cmd.Run()
    if ctx.Err() == context.DeadlineExceeded {
        return nil, fmt.Errorf("llm call timed out after %v", timeout)
    }
    if err != nil {
        return nil, fmt.Errorf("claude cli failed (exit %d): %s", cmd.ProcessState.ExitCode(), stderr.String())
    }
    // 解析 stdout JSON...
}
```

关键点：
- `context.WithTimeout` 包装传入的 ctx，确保有 deadline
- `exec.CommandContext` 在 ctx 取消后自动 Kill 子进程
- 先检查 `ctx.Err()` 区分超时和其他错误
- 捕获 stderr 提供详细错误信息

**4. VFSFile 适配器模式——Write-then-Read**

LLMFile 适配 VFS 的方式：

```
Open("/dev/llm/claude") → 创建 LLMFile（关联 driver，分配 context.Background()）
Write(requestJSON)       → 解析请求为 LLMRequest，调用 driver.Call()，缓冲响应 JSON
Read(length)             → 返回缓冲的响应 JSON（截取 length 或全部）
Close()                  → 清理资源
```

Write 触发实际 CLI 调用（同步阻塞），Read 返回缓冲结果。这保持了文件 I/O 语义。

Write 接收的请求 JSON 格式（VFSFile.Write 的 data 参数）：

```json
{
  "intent": "分析代码",
  "system_prompt": "你是一个代码分析专家...",
  "model": "sonnet",
  "max_turns": 1,
  "timeout_ms": 30000
}
```

Read 返回的响应 JSON 格式（VFSFile.Read 的返回值）：

```json
{
  "content": "分析结果...",
  "tokens_used": 1
}
```

**注意：** LLMFile 需要一个 `context.Context` 来传递给 `driver.Call()`。Open 时创建的 LLMFile 使用 `context.Background()`。在 Story 1.6 中，Kernel 会通过进程的 cancel context 来控制 LLM 调用生命周期——但这个集成在 Story 1.6 处理，本 Story 使用 background context 即可。后续 Story 1.6 可能需要在 VFSFileFactory 中传入 ctx，或者在 LLMFile 上增加 SetContext 方法。

**5. 错误处理层次**

```
Claude CLI 错误（进程退出码非 0、超时、JSON 解析失败）
  → 驱动返回标准 error（fmt.Errorf 包装，包含退出码/stderr/超时时间）
    → VFSFile.Write 原样返回驱动错误
      → VFS.Write 包装为 VFSError（op="Write", code=ErrDriver/ErrTimeout）
        → Kernel 层包装为 SyscallError（补充 PID、Device="/dev/llm/claude"）
```

驱动层不定义专门的错误类型，使用标准 `fmt.Errorf`：

```go
// 正确：驱动返回详细的标准 error
return nil, fmt.Errorf("claude cli failed (exit %d): %s", exitCode, stderr)
return nil, fmt.Errorf("failed to parse llm response: %w", jsonErr)
return nil, fmt.Errorf("llm call timed out after %v", timeout)

// 错误：驱动不应使用 kernel.SyscallError——会产生循环依赖
return nil, &kernel.SyscallError{...}  // ← 禁止
```

**6. Stream 方法的 MVP 实现**

提供基于 `--output-format stream-json` + `bufio.Scanner` 的基础实现，为 Story 3.1（astrace SyscallEvent 记录）打基础。

stream-json 格式（每行一个 JSON 对象）：

```json
{"type":"assistant","message":{"content":[{"type":"text","text":"部分响应..."}]}}
{"type":"result","subtype":"success","result":"最终结果...","cost_usd":0.003}
```

Stream 方法实现要点：
- 启动 goroutine 逐行读取 stdout（`bufio.Scanner`）
- 每行解析为 StreamEvent 写入 channel
- ctx 取消时关闭 channel 并 Kill 进程
- channel 缓冲大小 64（足够 MVP 使用）

### Go 代码命名规则（必须遵循）

| 对象 | 规则 | 示例 |
|------|------|------|
| 包名 | 全小写 | `llm` |
| 导出类型 | PascalCase | `LLMDriver`, `ClaudeCliDriver`, `LLMRequest`, `LLMResponse`, `StreamEvent`, `DriverInfo`, `LLMFile`, `DriverRegistry` |
| 非导出类型 | camelCase | `claudeCliResponse`（CLI JSON 解析结构） |
| 导出函数 | PascalCase | `NewClaudeCliDriver`, `NewDriverRegistry`, `FileFactory` |
| 方法接收器 | 简短 | `d *ClaudeCliDriver`, `r *DriverRegistry`, `f *LLMFile` |
| 函数选项 | PascalCase With 前缀 | `WithModel`, `WithTimeout`, `WithCommandBuilder` |
| 常量 | PascalCase | `DefaultModel`, `DefaultTimeout` |
| 文件名 | 下划线分隔 | `driver.go`, `claude_cli.go`, `vfsfile.go`, `registry.go` |

### 测试规范

**测试文件位置：**
- `drivers/llm/claude_cli_test.go`
- `drivers/llm/vfsfile_test.go`
- `drivers/llm/registry_test.go`

**exec.Command Mock（Go 标准 TestHelperProcess 模式）：**

```go
// claude_cli_test.go 中定义
func TestHelperProcess(t *testing.T) {
    if os.Getenv("GO_TEST_PROCESS") != "1" {
        return
    }
    switch os.Getenv("GO_TEST_CASE") {
    case "success":
        fmt.Fprintf(os.Stdout, `{"type":"result","subtype":"success","result":"test output","cost_usd":0.001,"is_error":false,"duration_ms":100,"num_turns":1}`)
    case "is_error":
        fmt.Fprintf(os.Stdout, `{"type":"result","subtype":"error","result":"LLM error message","is_error":true}`)
    case "cli_error":
        fmt.Fprintf(os.Stderr, "Error: invalid arguments")
        os.Exit(1)
    case "invalid_json":
        fmt.Fprintf(os.Stdout, "not json at all")
    case "timeout":
        time.Sleep(5 * time.Second)
    case "args_echo":
        // 将命令行参数写入 stderr 供测试验证
        fmt.Fprintf(os.Stderr, strings.Join(os.Args, " "))
        fmt.Fprintf(os.Stdout, `{"type":"result","subtype":"success","result":"ok","is_error":false}`)
    }
    os.Exit(0)
}

func mockCmdBuilder(testCase string) CommandBuilder {
    return func(ctx context.Context, name string, args ...string) *exec.Cmd {
        cs := []string{"-test.run=TestHelperProcess", "--"}
        cs = append(cs, args...)
        cmd := exec.CommandContext(ctx, os.Args[0], cs...)
        cmd.Env = append(os.Environ(), "GO_TEST_PROCESS=1", "GO_TEST_CASE="+testCase)
        return cmd
    }
}
```

**必须包含的测试场景：**

| 测试 | 验证点 |
|------|--------|
| `TestClaudeCliDriver_Call_Success` | 正常调用：mock 返回成功 JSON，验证 Content 和 TokensUsed |
| `TestClaudeCliDriver_Call_Timeout` | 超时：mock 延迟 > timeout，验证返回 timeout 错误 |
| `TestClaudeCliDriver_Call_CLIError` | CLI 非零退出码：验证错误消息包含 stderr 内容 |
| `TestClaudeCliDriver_Call_InvalidJSON` | 非法 JSON 响应：验证解析错误处理 |
| `TestClaudeCliDriver_Call_IsError` | CLI 返回 `is_error: true`：验证错误处理 |
| `TestClaudeCliDriver_Call_Args` | 参数传递：验证 --system-prompt、--model、--max-turns 正确拼接 |
| `TestClaudeCliDriver_Call_DefaultArgs` | 默认参数：未指定 model 时使用 defaultModel |
| `TestClaudeCliDriver_Info` | Info 返回正确的 DriverInfo |
| `TestClaudeCliDriver_Options` | 函数选项：WithModel、WithTimeout、WithCommandBuilder 生效 |
| `TestLLMFile_WriteRead` | VFSFile 适配：Write 请求 JSON + Read 响应 JSON |
| `TestLLMFile_ReadBeforeWrite` | 未 Write 就 Read：返回空或错误 |
| `TestLLMFile_ClosedAccess` | Close 后 Write/Read 返回错误 |
| `TestLLMFile_Stat` | Stat 返回正确的 FileStat（IsDevice=true） |
| `TestFileFactory` | FileFactory 创建 VFSFile 成功 |
| `TestDriverRegistry_RegisterGet` | 注册后可按路径查找 |
| `TestDriverRegistry_DuplicateRegister` | 重复注册返回错误 |
| `TestDriverRegistry_GetNotFound` | 未注册路径返回 false |

**测试模式（对齐前序 Story 风格）：**
- 使用 Go 标准 `testing` 包，`t.Run` 子测试
- 使用 `t.Fatal` / `t.Fatalf` / `t.Errorf`
- exec.Command 通过 TestHelperProcess 模式 mock
- 全部通过 `go test -race ./drivers/llm/...`

### 前序 Story 经验教训（必须吸收）

1. **data race 敏感：** Story 1-2 的 Future[T] 和 Process 状态机因并发问题返工。LLMFile 的 request/response 缓冲 MVP 中为单 goroutine 使用（每进程独立 FD），可暂不加锁。但如果 Stream 方法涉及 goroutine，channel 通信需要注意生命周期
2. **VFSError/ContextError 模式成功：** 驱动层返回标准 error，由上层包装。保持依赖方向干净
3. **测试使用 `t.Logf` 不用 `fmt.Printf`**
4. **SyncMap 的 LoadOrStore 和 LoadAndDelete 方法已可用**（Story 1-3 新增）
5. **TOCTOU 意识：** Registry.Register 内部已有重复检查保护，无需额外 check-then-register
6. **Story 1-3 的 VFSFileFactory 签名包含 subpath：** `func(subpath string, flags OpenFlag) (VFSFile, error)`——对于 `/dev/llm/claude` 精确匹配，subpath 为空字符串；如果注册 `/dev/llm` 前缀，subpath 为 `/claude`。MVP 建议精确注册 `/dev/llm/claude`

### Git 智能（最近工作模式）

**最近 5 个提交分析：**

| 提交 | 内容 | 启示 |
|------|------|------|
| `318e25a` | Story 1-4 实现完成 | context 包模式：Manager + 原子 ID + SyncMap |
| `ddfe7e3` | Story 1-4 状态更新 | sprint-status.yaml 字段更新模式 |
| `449c36a` | Story 1-4 文档 | 先创建 story 文档再实现 |
| `6ba2532` | Story 1-3 VFS 实现 | VFS + DeviceRegistry + VFSFileFactory 模式——**LLM 驱动必须对齐此模式** |
| `8cc10a0` | Story 1-3 文档 | 文档与代码分离提交 |

**代码惯例提取：**
- 包级文档注释：`// Package llm implements the LLM driver layer for Crux.`
- 构造函数：`NewXxx()` 模式
- 方法接收器：简短单字母（`d *ClaudeCliDriver`、`r *DriverRegistry`、`f *LLMFile`）
- 测试分组：`t.Run("子测试名", func(t *testing.T) {...})`
- 导入分组：标准库 → 空行 → 项目内部包
- `drivers/llm/.gitkeep` 在实现后应删除

### Project Structure Notes

**本 Story 新增的文件：**

```
drivers/llm/
├── driver.go          (新增 — Task 1: 接口和类型定义)
├── claude_cli.go      (新增 — Task 2: Claude CLI 驱动实现)
├── vfsfile.go         (新增 — Task 3: VFSFile 适配器)
├── registry.go        (新增 — Task 4: 驱动注册表)
├── claude_cli_test.go (新增 — Task 5: CLI 驱动测试)
├── vfsfile_test.go    (新增 — Task 5: VFSFile 适配器测试)
└── registry_test.go   (新增 — Task 5: 注册表测试)
```

**应删除的文件：**
- `drivers/llm/.gitkeep` — 有实际文件后不再需要占位

**不要创建的文件：**
- `drivers/llm/types.go` — 类型数量少，统一放在 `driver.go` 中
- `drivers/llm/errors.go` — 使用标准 error，不需要专门的错误文件
- `drivers/llm/driver_test.go` — 类型定义无需独立测试，合并到其他测试文件

**不要触碰的文件：**
- `kernel/` 下任何文件（LLM 调用集成在 Story 1.6）
- `vfs/vfs.go`、`vfs/dev.go`（接口已定义，无需修改）
- `context/` 下任何文件
- `internal/types/types.go`（类型已满足需求）
- `internal/xsync/` 下任何文件（Registry API 已满足需求）
- `cmd/crux/main.go`（依赖注入在 Story 1.7）
- `drivers/fs/`、`drivers/shell/`（其他驱动在 Story 2.2/2.3）

### References

- [Source: architecture.md#Decision 4: Claude Code CLI 集成] — 调用模式、参数模板、stream-json、超时处理
- [Source: architecture.md#Decision 1: Syscall ABI 设计风格] — LLMDriver 接口设计理念
- [Source: architecture.md#Core Architectural Decisions > LLM 驱动层架构] — 多 Provider VFS 挂载设计、核心接口定义
- [Source: architecture.md#Project Structure & Boundaries] — drivers/llm/ 目录结构、VFS ↔ Drivers 边界
- [Source: architecture.md#Implementation Patterns > 结构模式] — 依赖方向：drivers/ 不导入 kernel/
- [Source: architecture.md#Implementation Patterns > 过程模式] — context.Context 传播规则：Driver 方法接受 ctx 参数
- [Source: epics.md#Story 1.5] — 原始用户故事和验收标准
- [Source: prd.md#FR9] — 通过 LLM 驱动层非交互调用 LLM 并获取结构化响应
- [Source: prd.md#FR17] — 通过 /dev/llm/claude 访问 LLM 推理能力
- [Source: prd.md#NFR7] — LLM API 超时/错误时，进程在 5 秒内正确转入 Zombie 状态
- [Source: prd.md#NFR11] — 正确传递 system prompt、工具声明、模型选择、输出格式
- [Source: prd.md#NFR12] — LLM 驱动层支持流式结构化输出模式（stream-json）
- [Source: prd.md#NFR20] — LLM 驱动层封装在单一模块中，外部 LLM 接口变更时只需修改此模块
- [Source: project-context.md#Claude Code CLI 集成] — 调用模式、参数、stream-json、超时处理
- [Source: project-context.md#测试规则] — Driver 测试通过注入可替换的 command builder 来 mock
- [Source: project-context.md#关键防错规则] — 禁止反向依赖、禁止手写 map+mutex
- [Source: 1-4-context-management.md] — 前序 Story 产出、BuildPrompt/PromptResult 供 LLM 驱动消费、并发经验教训
- [Source: 1-3-vfs-framework-and-device-registration.md（git log）] — VFSFile/VFSFileFactory 模式、DeviceRegistry 注册流程

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6

### Debug Log References

无调试问题。

### Completion Notes List

- ✅ Task 1: `drivers/llm/driver.go` — 定义 LLMRequest、LLMResponse、StreamEvent、DriverInfo 结构体和 LLMDriver 接口，所有字段含 JSON tag
- ✅ Task 2: `drivers/llm/claude_cli.go` — 实现 ClaudeCliDriver，含 Call（exec.CommandContext + JSON 解析）、Stream（stream-json + bufio.Scanner + goroutine）、Info 方法；CommandBuilder 注入支持测试；函数选项模式（WithModel/WithTimeout/WithCommandBuilder）；超时通过 context.WithTimeout 控制
- ✅ Task 3: `drivers/llm/vfsfile.go` — 实现 LLMFile 满足 vfs.VFSFile 接口，Write-then-Read 语义（Write 触发 driver.Call 并缓冲响应，Read 返回缓冲数据支持分段读取）；FileFactory 返回 VFSFileFactory 闭包
- ✅ Task 4: `drivers/llm/registry.go` — 实现 DriverRegistry 封装 xsync.Registry[LLMDriver]，支持 Register/Get
- ✅ Task 5: 22 个测试全部通过，覆盖 Call 成功/超时/CLI错误/无效JSON/is_error/参数验证/默认参数、Stream 成功/错误、Info、Options、VFSFile Write+Read/ReadBeforeWrite/ClosedAccess/Stat/ReadPartial/WriteDriverError/FileFactory、Registry 注册/重复/未找到
- ✅ 全量回归 `go test ./...` 通过，无任何已有测试被破坏
- ✅ 删除 `drivers/llm/.gitkeep` 占位文件
- 依赖方向正确：`drivers/llm/` 仅导入标准库 + `vfs/`（类型引用）+ `internal/xsync/`，未导入 `kernel/` 或 `context/`

### Change Log

- 2026-02-24: Story 1-5 完整实现 — LLM 驱动层（LLMDriver 接口 + ClaudeCliDriver + VFSFile 适配器 + DriverRegistry），17 个单元测试全部通过
- 2026-02-24: Code Review 修复 4 个问题：
  - [H1] `LLMRequest.Timeout time.Duration` → `TimeoutMs int64`，修复 JSON 反序列化语义错误（timeout_ms 值被解释为纳秒而非毫秒）
  - [H2] 新增 `TestClaudeCliDriver_Stream_Success` 和 `TestClaudeCliDriver_Stream_Error` 测试，覆盖 Stream 方法
  - [M1] `FileFactory` 新增 `basePath string` 参数，消除硬编码设备路径
  - [M2] Stream goroutine 中 `cmd.Wait()` 移至 defer，防止 "result" 事件正常退出时子进程成为僵尸

### File List

- `drivers/llm/driver.go` (新增)
- `drivers/llm/claude_cli.go` (新增)
- `drivers/llm/vfsfile.go` (新增)
- `drivers/llm/registry.go` (新增)
- `drivers/llm/claude_cli_test.go` (新增)
- `drivers/llm/vfsfile_test.go` (新增)
- `drivers/llm/registry_test.go` (新增)
- `drivers/llm/.gitkeep` (删除)
