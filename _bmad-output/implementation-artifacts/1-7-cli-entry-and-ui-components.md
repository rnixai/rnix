# Story 1.7: CLI 入口与 UI 组件

Status: review

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a 用户,
I want 通过 `crux "意图"` 启动智能体并看到清晰的实时进度和结果输出,
So that 我全程知道智能体在做什么、结果是什么。

## Acceptance Criteria

1. **CLI 入口与依赖注入** — Given `cmd/crux/main.go` 已实现，When 执行 `crux "分析代码"`，Then 解析意图文本，创建 VFS + DeviceRegistry + Context Manager + Kernel 实例，注册 Claude 驱动到 `/dev/llm/claude`，调用 `kernel.Spawn`，等待 Done channel 完成并输出结果，And 支持全局 flags：`--json`、`--verbose/-v`、`--quiet/-q`
2. **TerminalProfile 检测** — Given `internal/ui/renderer.go` 已实现，When 程序启动，Then 检测 `TerminalProfile`（宽度、IsTTY、颜色级别、Unicode 支持），And 所有组件输出到 `io.Writer`，不直接写 `os.Stdout`
3. **lipgloss 样式集中定义** — Given `internal/ui/styles.go` 已实现，When 输出带样式文本，Then 使用 lipgloss 集中定义的颜色（内核灰 `#888888`、智能体蓝 `#5B9BD5`、成功绿 `#6BCB77`、警告黄 `#FFD93D`、错误红 `#FF6B6B`），And 支持 `NO_COLOR` 环境变量降级
4. **Agent Progress Reporter** — Given `internal/ui/progress.go` 已实现，When 智能体执行中，Then 实时输出 `[kernel] spawning PID 1...`、`[agent/1] reasoning step 1/3...` 等汇报行
5. **Result Box** — Given `internal/ui/result.go` 已实现，When 智能体返回结果，Then 用双线边框 `══` 包裹结果文本，边框颜色为成功绿，宽度自适应终端（最大 120 字符）
6. **Error Block** — Given `internal/ui/error.go` 已实现，When 发生错误，Then 输出三行结构：`✗ {设备路径}: {错误原因}` → `→ {影响}` → `→ 建议: {恢复操作}`
7. **Summary Footer** — Given `internal/ui/summary.go` 已实现，When 智能体完成，Then 输出 `[kernel] PID {N} exited({code}) | tokens: {N} | elapsed: {N}s`
8. **非 TTY 降级** — Given 非 TTY 输出（管道/重定向），When 执行 `crux "意图" | cat`，Then 自动去除 ANSI 颜色码和 spinner 动画，符号保留语义（`✓`/`✗`/`⚠`）

## Tasks / Subtasks

- [x] Task 1: 实现 TerminalProfile 检测和 Renderer 抽象 (AC: #2)
  - [x] 1.1 创建 `internal/ui/renderer.go`：定义 `TerminalProfile` 结构体（Width int、IsTTY bool、ColorLevel int、IsUnicode bool）
  - [x] 1.2 实现 `DetectProfile(w io.Writer) TerminalProfile`：通过 `golang.org/x/term` 获取终端宽度、通过 `isatty` 检测 TTY、通过 `NO_COLOR` 和 `CRUX_ASCII` 环境变量检测降级
  - [x] 1.3 定义 `Renderer` 结构体（Profile TerminalProfile、Writer io.Writer、OutputMode OutputMode），`OutputMode` 枚举：`ModeDefault`、`ModeQuiet`、`ModeVerbose`、`ModeJSON`
  - [x] 1.4 实现 `NewRenderer(w io.Writer, mode OutputMode) *Renderer`

- [x] Task 2: 实现 lipgloss 样式集中定义 (AC: #3)
  - [x] 2.1 创建 `internal/ui/styles.go`：定义颜色常量（`ColorKernel #888888`、`ColorAgent #5B9BD5`、`ColorSuccess #6BCB77`、`ColorWarning #FFD93D`、`ColorError #FF6B6B`、`ColorMuted #666666`）
  - [x] 2.2 定义 lipgloss 样式集（`KernelStyle`、`AgentStyle`、`SuccessStyle`、`ErrorStyle`、`WarningStyle`、`MutedStyle`、`BoldStyle`）
  - [x] 2.3 实现 `InitStyles(profile TerminalProfile)`：根据 ColorLevel 选择完整色彩/16 色/无色模式
  - [x] 2.4 `NO_COLOR` 环境变量时所有样式降级为纯文本

- [x] Task 3: 实现 Agent Progress Reporter 组件 (AC: #4)
  - [x] 3.1 创建 `internal/ui/progress.go`：定义 `ProgressReporter` 结构体（renderer *Renderer）
  - [x] 3.2 实现 `KernelMessage(format string, args ...any)`：输出 `[kernel] {message}` 格式，前缀灰色
  - [x] 3.3 实现 `AgentMessage(pid types.PID, format string, args ...any)`：输出 `[agent/{pid}] {message}` 格式，前缀蓝色
  - [x] 3.4 实现 `AgentStep(pid types.PID, step, total int)`：输出 `[agent/{pid}] reasoning step {step}/{total}...` 格式

- [x] Task 4: 实现 Result Box 组件 (AC: #5)
  - [x] 4.1 创建 `internal/ui/result.go`：定义 `RenderResult(r *Renderer, title string, content string)`
  - [x] 4.2 实现双线边框渲染：上边框 `══ {title} ══...══`，下边框纯 `══...══`
  - [x] 4.3 内容 2 空格缩进，宽度自适应 `min(termWidth, 120)`
  - [x] 4.4 边框颜色：成功=绿色，`NO_COLOR` 时保留 `══` 字符但无颜色

- [x] Task 5: 实现 Error Block 组件 (AC: #6)
  - [x] 5.1 创建 `internal/ui/error.go`：定义 `RenderError(r *Renderer, device string, reason string, impact string, suggestion string)`
  - [x] 5.2 实现三行结构：`✗ {device}: {reason}` → `  → {impact}` → `  → 建议: {suggestion}`
  - [x] 5.3 `✗` 前缀红色，`→` 前缀暗灰色
  - [x] 5.4 `NO_COLOR` 时 `✗` 替换为 `[ERR]`

- [x] Task 6: 实现 Summary Footer 组件 (AC: #7)
  - [x] 6.1 创建 `internal/ui/summary.go`：定义 `RenderSummary(r *Renderer, pid types.PID, exitCode int, tokens int, elapsed time.Duration)`
  - [x] 6.2 实现格式：`[kernel] PID {N} exited({code}) | tokens: {N} | elapsed: {N}s`
  - [x] 6.3 exit(0) 灰色，exit(non-0) 黄色警告色；token 数和耗时白色加粗

- [x] Task 7: 扩展 CLI 入口并实现依赖注入 (AC: #1)
  - [x] 7.1 修改 `cmd/crux/main.go`：在 `main()` 或 `init()` 中创建完整的依赖注入链
  - [x] 7.2 依赖注入顺序：DeviceRegistry → VFS → ClaudeCliDriver → 注册 `/dev/llm/claude` → Context Manager → Kernel
  - [x] 7.3 实现根命令 `crux "意图"` 处理：解析 args[0] 为意图文本、调用 `kernel.Spawn(intent, nil, opts)` 启动智能体
  - [x] 7.4 Spawn 后阻塞等待 `proc.Done` channel，获取 ExitStatus
  - [x] 7.5 成功时：通过 ProgressReporter 输出进度 + ResultBox 输出结果 + SummaryFooter 输出汇总
  - [x] 7.6 失败时：通过 ErrorBlock 输出错误信息
  - [x] 7.7 添加全局 flags 注册：`--json`、`--verbose/-v`、`--quiet/-q`
  - [x] 7.8 根据 flags 设置 Renderer 的 OutputMode
  - [x] 7.9 `--json` 模式：输出 `JSONResponse[T]` 格式的 JSON，无颜色无装饰
  - [x] 7.10 程序退出码：成功=0，智能体失败=1，参数错误=2

- [x] Task 8: 实现 Kernel 回调机制（进度通知） (AC: #4)
  - [x] 8.1 在 `kernel/kernel.go` 中定义 `KernelCallbacks` 接口（`OnSpawn(pid PID, intent string)`、`OnStep(pid PID, step int, total int)`、`OnComplete(pid PID, result string, exit ExitStatus)`、`OnError(pid PID, err error)`）
  - [x] 8.2 为 KernelImpl 新增 `callbacks KernelCallbacks` 字段
  - [x] 8.3 更新 `NewKernel` 签名接受 `KernelCallbacks` 参数（可为 nil）
  - [x] 8.4 在 Spawn、reasonStep 的关键节点调用 callbacks（nil 安全检查）
  - [x] 8.5 在 `cmd/crux/main.go` 中实现 `cliCallbacks` 结构体，将回调转发到 ProgressReporter

- [x] Task 9: 信号处理 (AC: #1)
  - [x] 9.1 在 `cmd/crux/main.go` 中注册 SIGINT/SIGTERM 信号处理
  - [x] 9.2 首次 SIGINT：调用 kernel 取消当前进程的 context，等待优雅退出
  - [x] 9.3 二次 SIGINT（2 秒内）：调用 `os.Exit(130)` 强制退出
  - [x] 9.4 输出中断摘要：`[kernel] PID {N} interrupted (SIGINT)` + 状态变化 + 建议

- [x] Task 10: 编写完整单元测试 (AC: all)
  - [x] 10.1 `internal/ui/renderer_test.go`：TerminalProfile 检测测试（TTY/非 TTY/NO_COLOR/CRUX_ASCII）
  - [x] 10.2 `internal/ui/styles_test.go`：颜色降级测试
  - [x] 10.3 `internal/ui/progress_test.go`：KernelMessage、AgentMessage、AgentStep 输出格式验证（写入 bytes.Buffer 后检查内容）
  - [x] 10.4 `internal/ui/result_test.go`：ResultBox 渲染、宽度自适应、NO_COLOR 降级
  - [x] 10.5 `internal/ui/error_test.go`：ErrorBlock 三行结构、NO_COLOR 降级
  - [x] 10.6 `internal/ui/summary_test.go`：SummaryFooter 格式、退出码着色
  - [x] 10.7 `cmd/crux/main_test.go`：集成测试（mock kernel + 验证 CLI 输出格式）
  - [x] 10.8 全量回归 `go test -race ./...` 确保不破坏已有测试

## Dev Notes

### 架构模式与约束

- **文件位置严格遵循架构文档：** UI 组件在 `internal/ui/`，CLI 入口在 `cmd/crux/main.go`
- **依赖方向：** `cmd/` → `kernel/` ✓；`cmd/` → `internal/ui/` ✓；`cmd/` → `vfs/` ✓（仅用于初始化）；`cmd/` → `drivers/llm/` ✓（仅用于初始化）；`cmd/` → `context/` ✓（仅用于初始化）。**`internal/ui/` 不导入 `kernel/` 或 `cmd/`**
- **此 Story 实现的核心：** CLI 依赖注入 + 意图命令处理 + 6 个 UI 组件 + Renderer 抽象 + 信号处理 + Kernel 回调机制
- **此 Story 不实现：** `crux ps` 命令（Story 4.4）、`crux astrace` 命令（Story 3.3）、`crux kill` 命令（Story 4.1）、Skill 加载注入（Story 2.4）
- **`cmd/crux/main.go` 是唯一组装点：** 所有实例创建和依赖注入在此完成

### 已有代码（必须复用，禁止重新实现）

**`cmd/crux/main.go` — 当前仅 34 行骨架：**

```go
package main

import (
    "fmt"
    "os"
    "github.com/spf13/cobra"
)

var version = "0.1.0"

var rootCmd = &cobra.Command{
    Use:   "crux",
    Short: "Crux — Agent OS for AI agents",
    Long:  "Crux is an operating system for AI agents...",
}

var versionCmd = &cobra.Command{
    Use:   "version",
    Short: "Show version and dependencies",
    Run: func(cmd *cobra.Command, args []string) {
        fmt.Printf("crux v%s\n", version)
    },
}

func init() {
    rootCmd.AddCommand(versionCmd)
}

func main() {
    if err := rootCmd.Execute(); err != nil {
        os.Exit(1)
    }
}
```

需要大幅扩展：添加意图处理、依赖注入链、全局 flags、信号处理、Renderer 初始化。

**`kernel/kernel.go` — Spawn API：**

```go
type KernelImpl struct {
    procTable *xsync.SyncMap[types.PID, *Process]
    vfs       *vfs.VFS
    ctxMgr    *cruxctx.Manager
}

func NewKernel(v *vfs.VFS, ctxMgr *cruxctx.Manager) *KernelImpl

func (k *KernelImpl) Spawn(intent string, skills []string, opts SpawnOpts) (types.PID, error)

type SpawnOpts struct {
    Model        string
    SystemPrompt string
    MaxTurns     int
    TimeoutMs    int64
}
```

Spawn 返回 PID 后，reasonStep 在后台 goroutine 中执行。CLI 需要获取 Process 的 Done channel 来等待完成。

**`kernel/process.go` — Process 结构体关键字段：**

```go
type Process struct {
    PID        types.PID
    Done       chan ExitStatus  // 缓冲 1，进程完成通知
    Result     string           // 最终输出
    TokensUsed int              // 累计 token
    CreatedAt  time.Time
    Exit       *ExitStatus      // Zombie/Dead 时非 nil
    cancel     context.CancelFunc  // goroutine 取消
}

type ExitStatus struct {
    Code   int
    Reason string
    Err    error
}
```

CLI 的核心等待流程：`Spawn → GetProcess → <-proc.Done → 读取 proc.Result + proc.Exit`

**VFS + Drivers 初始化模式（来自架构文档）：**

```go
// cmd/crux/main.go 中的依赖注入
devReg := vfs.NewDeviceRegistry()
vfsInst := vfs.NewVFS(devReg)
claudeDriver := llm.NewClaudeCliDriver()
devReg.Register("/dev/llm/claude", llm.FileFactory(claudeDriver, "/dev/llm/claude"))
ctxMgr := cruxctx.NewManager()
kern := kernel.NewKernel(vfsInst, ctxMgr)
```

**注意事项：**
- `llm.FileFactory(driver, basePath)` 需要传入 `basePath` 参数（Story 1-5 中的设计，`basePath` 用于 VFSFileFactory 中识别设备路径）
- `vfs.NewVFS(devReg)` 构造函数接受 DeviceRegistry
- `kernel.NewKernel(vfs, ctxMgr)` 当前签名不含 callbacks——本 Story 需要扩展

### Kernel 回调机制设计

**问题：** Kernel 的 Spawn 和 reasonStep 在内部 goroutine 中执行，CLI 层需要知道进度（如 "reasoning step 1/3"）来输出 UI。但 `kernel/` 不能导入 `internal/ui/`（依赖方向禁止）。

**解决方案：Callbacks 接口注入**

```go
// kernel/kernel.go 中新增
type KernelCallbacks interface {
    OnSpawn(pid types.PID, intent string)
    OnStep(pid types.PID, step int, total int)
    OnComplete(pid types.PID, result string, exit ExitStatus)
    OnError(pid types.PID, err error)
}

// KernelImpl 新增字段
type KernelImpl struct {
    procTable *xsync.SyncMap[types.PID, *Process]
    vfs       *vfs.VFS
    ctxMgr    *cruxctx.Manager
    callbacks KernelCallbacks  // 可为 nil
}

func NewKernel(v *vfs.VFS, ctxMgr *cruxctx.Manager, cb KernelCallbacks) *KernelImpl
```

**在 cmd/ 中实现：**

```go
// cmd/crux/main.go
type cliCallbacks struct {
    progress *ui.ProgressReporter
}

func (c *cliCallbacks) OnSpawn(pid types.PID, intent string) {
    c.progress.KernelMessage("spawning PID %d...", pid)
}
func (c *cliCallbacks) OnStep(pid types.PID, step, total int) {
    c.progress.AgentStep(pid, step, total)
}
// ...
```

**影响已有测试：** `NewKernel` 签名变化将影响 `kernel/kernel_test.go` 中的所有测试。使用 `NewKernel(vfs, ctxMgr, nil)` 保持兼容（nil callbacks = 静默模式）。

### CLI 主流程设计

```
用户执行: crux "分析代码"
  │
  ├─ main() → rootCmd.Execute()
  │    ├─ 解析全局 flags (--json/--verbose/--quiet)
  │    ├─ 初始化 Renderer (TerminalProfile + OutputMode)
  │    └─ rootCmd.RunE 处理
  │
  ├─ 依赖注入
  │    ├─ DeviceRegistry → VFS → ClaudeCliDriver → Register
  │    ├─ Context Manager
  │    ├─ cliCallbacks (包装 ProgressReporter)
  │    └─ Kernel (VFS + ctxMgr + callbacks)
  │
  ├─ Spawn 智能体
  │    ├─ kernel.Spawn(intent, nil, opts) → PID
  │    ├─ kernel.GetProcess(pid) → proc
  │    └─ <-proc.Done → ExitStatus
  │
  ├─ 输出结果
  │    ├─ 成功: ResultBox(proc.Result) + SummaryFooter
  │    └─ 失败: ErrorBlock + SummaryFooter
  │
  └─ os.Exit(exitStatus.Code)
```

### 进度回调的 reasonStep 集成点

在现有 `kernel/kernel.go` 的 `reasonStep` 函数中，以下位置需要调用 callbacks：

```go
func (k *KernelImpl) reasonStep(proc *Process, llmFD types.FD, opts SpawnOpts) {
    // 每轮开始时
    if k.callbacks != nil {
        k.callbacks.OnStep(proc.PID, step, maxSteps)  // 新增
    }

    // ... BuildPrompt → Write → Read → parseAction ...

    // ActionText 完成时
    if k.callbacks != nil {
        k.callbacks.OnComplete(proc.PID, proc.Result, exitStatus)  // 新增
    }

    // 错误时
    if k.callbacks != nil {
        k.callbacks.OnError(proc.PID, err)  // 新增
    }
}
```

### `--json` 模式输出格式

遵循架构文档的 `JSONResponse[T]` 模式：

```go
// 成功输出
{
  "ok": true,
  "data": {
    "pid": 1,
    "result": "分析发现 2 个性能瓶颈...",
    "tokens_used": 1847,
    "elapsed_ms": 6200,
    "exit_code": 0
  }
}

// 失败输出
{
  "ok": false,
  "error": {
    "code": "TIMEOUT",
    "message": "request timeout (30s)",
    "syscall": "Write",
    "device": "/dev/llm/claude"
  }
}
```

### 信号处理设计

```go
// cmd/crux/main.go
func setupSignalHandler(kern *kernel.KernelImpl, pid types.PID) {
    sigCh := make(chan os.Signal, 2)
    signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

    go func() {
        <-sigCh  // 首次信号
        // 优雅中断：取消进程 context
        proc, ok := kern.GetProcess(pid)
        if ok {
            proc.Cancel()  // 需要暴露 cancel 方法或通过 Kernel 接口
        }
        // 等待第二次信号
        select {
        case <-sigCh:
            os.Exit(130)  // 强制退出
        case <-time.After(2 * time.Second):
            // 2 秒内无二次信号，正常等待进程退出
        }
    }()
}
```

**注意：** Process.cancel 是非导出字段。需要在 Process 或 Kernel 上新增一个导出方法 `CancelProcess(pid)` 来支持外部取消。

### Go 代码命名规则（必须遵循）

| 对象 | 规则 | 示例 |
|------|------|------|
| 包名 | 全小写 | `ui`、`main` |
| 导出类型 | PascalCase | `TerminalProfile`、`Renderer`、`ProgressReporter`、`OutputMode` |
| 非导出类型 | camelCase | `cliCallbacks` |
| 导出函数 | PascalCase | `DetectProfile`、`NewRenderer`、`RenderResult`、`RenderError` |
| 非导出函数 | camelCase | `setupSignalHandler`、`initKernel` |
| 常量 | PascalCase | `ColorKernel`、`ColorAgent`、`ModeDefault`、`ModeJSON` |
| 文件名 | 下划线分隔 | `renderer.go`、`styles.go`、`progress.go`、`result.go` |

### 测试规范

**测试文件位置：**
- `internal/ui/renderer_test.go`
- `internal/ui/styles_test.go`
- `internal/ui/progress_test.go`
- `internal/ui/result_test.go`
- `internal/ui/error_test.go`
- `internal/ui/summary_test.go`

**测试策略（关键）：**

所有 UI 组件通过 `io.Writer`（`bytes.Buffer`）输出，测试不依赖 TTY：

```go
func TestRenderResult(t *testing.T) {
    var buf bytes.Buffer
    profile := TerminalProfile{Width: 80, IsTTY: false, ColorLevel: 0, IsUnicode: true}
    r := NewRenderer(&buf, ModeDefault)
    r.Profile = profile

    RenderResult(r, "分析结果", "发现 2 个性能瓶颈")

    output := buf.String()
    // 验证包含边框、标题、内容
    if !strings.Contains(output, "══ 分析结果 ══") { t.Error("missing border") }
    if !strings.Contains(output, "发现 2 个性能瓶颈") { t.Error("missing content") }
}
```

**必须包含的测试场景：**

| 测试 | 验证点 |
|------|--------|
| `TestDetectProfile_TTY` | TTY 检测、宽度获取 |
| `TestDetectProfile_NoColor` | NO_COLOR 环境变量降级 |
| `TestDetectProfile_ASCII` | CRUX_ASCII=1 时 IsUnicode=false |
| `TestStyles_NoColor` | 无色模式下样式不含 ANSI 码 |
| `TestKernelMessage_Format` | `[kernel] {message}` 格式正确 |
| `TestAgentStep_Format` | `[agent/1] reasoning step 1/3...` 格式正确 |
| `TestRenderResult_Border` | 双线边框、标题、内容、宽度自适应 |
| `TestRenderResult_NoColor` | 无色模式下保留 `══` 但无颜色码 |
| `TestRenderError_ThreeLines` | 三行结构完整 |
| `TestRenderError_NoColor` | `✗` → `[ERR]` 降级 |
| `TestRenderSummary_Success` | exit(0) 格式正确 |
| `TestRenderSummary_Failure` | exit(non-0) 黄色标记 |
| `TestJSONOutput` | `--json` 模式下输出纯 JSON |

**测试模式：**
- 使用 Go 标准 `testing` 包，`t.Run` 子测试
- 输出到 `bytes.Buffer`，不依赖 TTY
- 全部通过 `go test -race ./...`

### 前序 Story 经验教训（必须吸收）

1. **NewKernel 签名变化影响（Story 1-6）：** 上次 Story 1-6 修改 NewKernel 签名（新增 vfs + ctxMgr 参数）时，需要更新所有已有测试的 NewKernel 调用。本 Story 再次修改签名（新增 callbacks），同样需要更新 kernel_test.go。使用 `NewKernel(vfs, ctxMgr, nil)` 传 nil callbacks 保持已有测试兼容
2. **data race 敏感（Story 1-2）：** Process 字段并发访问需加锁。CLI 层读取 `proc.Result` 和 `proc.TokensUsed` 时，必须确保 reasonStep goroutine 已完成（通过 Done channel 同步）
3. **VFSFileFactory basePath 参数（Story 1-3/1-5）：** `llm.FileFactory(driver, basePath)` 第二个参数是设备路径字符串 `"/dev/llm/claude"`，不要遗漏
4. **LLMFile Write-then-Read 语义（Story 1-5）：** Write 触发同步 CLI 调用，Read 返回缓冲结果。CLI 层不直接调用这些，Kernel 在 reasonStep 中处理
5. **finishProcess 辅助函数（Story 1-6 Review Fix）：** reasonStep 中提取了 `finishProcess` 辅助函数统一 terminate + Done channel 写入，callbacks 调用应在此函数中统一处理
6. **测试辅助函数模式（Story 1-6）：** `newTestKernel` 创建完整测试 Kernel（mock VFS + 真实 Context Manager + mock LLM 设备）。本 Story 需要更新此函数签名以支持 nil callbacks

### Git 智能（最近工作模式）

**最近 5 个提交分析：**

| 提交 | 内容 | 启示 |
|------|------|------|
| `d52fef3` | Story 1-6 最终化 | Spawn + reasonStep 完整实现，finishProcess 辅助函数 |
| `239b5ec` | Story 1-6 状态更新 | Code review 修复后标记完成 |
| `cacde63` | Story 1-6 初始实现 | 17 个测试覆盖 Spawn/reasonStep |
| `91d8d3d` | Story 1-5 增强 | LLMFile basePath 参数化、Stream 测试 |
| `0f48087` | Story 1-5 Review 修复 | 代码审查发现的问题修复模式 |

**代码惯例提取：**
- 包级文档注释：`// Package ui provides terminal UI components for Crux CLI.`
- 构造函数：`NewXxx()` 模式
- 方法接收器：简短单字母（`r *Renderer`、`p *ProgressReporter`）
- 测试分组：`t.Run("子测试名", func(t *testing.T) {...})`
- 导入分组：标准库 → 空行 → 第三方库 → 空行 → 项目内部包

### Project Structure Notes

**本 Story 修改的文件：**

```
cmd/crux/
├── main.go              (修改 — 大幅扩展：依赖注入 + 意图命令 + flags + 信号处理)

kernel/
├── kernel.go            (修改 — 新增 KernelCallbacks 接口 + callbacks 字段 + 更新 NewKernel 签名 + reasonStep 中插入回调)
├── kernel_test.go        (修改 — 更新 NewKernel 调用签名传 nil callbacks)
├── process.go           (修改 — 新增导出的 Cancel() 方法供外部取消)

internal/ui/
├── renderer.go          (新增 — TerminalProfile + Renderer + OutputMode)
├── styles.go            (新增 — lipgloss 颜色常量 + 样式集)
├── progress.go          (新增 — ProgressReporter 组件)
├── result.go            (新增 — Result Box 组件)
├── error.go             (新增 — Error Block 组件)
├── summary.go           (新增 — Summary Footer 组件)
├── renderer_test.go     (新增 — Renderer 测试)
├── styles_test.go       (新增 — 样式降级测试)
├── progress_test.go     (新增 — 进度组件测试)
├── result_test.go       (新增 — Result Box 测试)
├── error_test.go        (新增 — Error Block 测试)
├── summary_test.go      (新增 — Summary Footer 测试)

go.mod                    (修改 — 新增 lipgloss + golang.org/x/term 依赖)
```

**不要触碰的文件：**
- `vfs/` 下任何文件（接口已定义，无需修改）
- `context/` 下任何文件（API 已满足需求）
- `drivers/` 下任何文件（构造函数已满足需求）
- `internal/types/types.go`（类型已满足需求）
- `internal/xsync/` 下任何文件

**需要新增的依赖（go.mod）：**
- `github.com/charmbracelet/lipgloss`（v2 — 终端样式引擎）
- `golang.org/x/term`（终端宽度检测）
- 注意：MVP 阶段**不需要** `bubbletea` 或 `bubbles`（spinner 可以用简单的 `fmt.Fprintf` 模拟，Phase 2 再引入完整 TUI 框架）

### References

- [Source: architecture.md#Starter Template Evaluation > CLI 框架] — Cobra 命令框架
- [Source: architecture.md#Decision 1: Syscall ABI 设计风格] — Kernel 分类接口
- [Source: architecture.md#Project Structure & Boundaries > cmd/ 依赖注入点] — 唯一组装点
- [Source: architecture.md#Implementation Patterns > 格式模式] — JSONResponse[T]、JSON snake_case、时间格式
- [Source: architecture.md#Implementation Patterns > 通信模式] — 日志格式
- [Source: architecture.md#修正后的依赖方向] — cmd/ → kernel/ → vfs/ → drivers/
- [Source: ux-design-specification.md#Design System Foundation] — Charm 生态选择、颜色系统、排版
- [Source: ux-design-specification.md#Component Strategy > Custom Components] — 6 个 UI 组件规格
- [Source: ux-design-specification.md#UX Consistency Patterns > Command Input Patterns] — 命令语法、Flag 约定
- [Source: ux-design-specification.md#UX Consistency Patterns > Feedback Patterns] — ✓/✗/⚠/[来源] 格式
- [Source: ux-design-specification.md#UX Consistency Patterns > Progress & Loading Patterns] — 三种进度模式
- [Source: ux-design-specification.md#UX Consistency Patterns > Interruption & Cancellation Patterns] — 信号处理
- [Source: ux-design-specification.md#Terminal Adaptability & Accessibility] — TerminalProfile 检测、颜色降级、NO_COLOR
- [Source: ux-design-specification.md#Design Direction Decision] — 方向 B 结构化汇报式
- [Source: epics.md#Story 1.7] — 原始用户故事和验收标准
- [Source: prd.md#FR33] — `crux "意图"` 单命令启动智能体
- [Source: prd.md#FR32] — 智能体完成时输出汇总信息
- [Source: prd.md#FR36] — CLI 提供清晰错误信息
- [Source: prd.md#FR37] — `go install` 安装，单二进制零依赖
- [Source: prd.md#NFR10] — CLI 进程在智能体异常退出时不崩溃
- [Source: project-context.md#CLI 命令结构] — 根命令、子命令、全局 flags
- [Source: project-context.md#关键防错规则] — 禁止反向依赖
- [Source: 1-6-kernel-reasoning-loop-spawn-reasonstep.md] — 前序 Story 完整产出和经验教训

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6

### Debug Log References

### Completion Notes List

- ✅ Task 1: TerminalProfile 检测 + Renderer 抽象。`DetectProfile` 通过 `golang.org/x/term` 获取终端宽度，`go-isatty` 检测 TTY，支持 `NO_COLOR` 和 `CRUX_ASCII` 环境变量降级。7 个测试通过。
- ✅ Task 2: lipgloss 样式集中定义。6 个颜色常量 + 7 个样式变量 + `InitStyles` 根据 ColorLevel 降级。3 个测试通过。
- ✅ Task 3: ProgressReporter 组件。`KernelMessage`、`AgentMessage`、`AgentStep` 三个方法，Quiet/JSON 模式静默。5 个测试通过。
- ✅ Task 4: Result Box 组件。双线边框 `══`，宽度自适应 `min(termWidth, 120)`，NO_COLOR 降级保留字符。5 个测试通过。
- ✅ Task 5: Error Block 组件。三行结构 `✗/→/→`，NO_COLOR 时 `✗` 替换为 `[ERR]`，ASCII 模式 `→` 替换为 `->`。4 个测试通过。
- ✅ Task 6: Summary Footer 组件。`[kernel] PID N exited(code) | tokens: N | elapsed: Ns` 格式，exit(0) 灰色/exit(non-0) 黄色。4 个测试通过。
- ✅ Task 7: CLI 入口扩展。完整依赖注入链（DevReg→VFS→Claude→Register→CtxMgr→Kernel），根命令意图处理，`--json`/`--verbose`/`--quiet` flags，`JSONResponse` 输出，退出码 0/1/2。
- ✅ Task 8: Kernel 回调机制。`KernelCallbacks` 接口 4 方法，`NewKernel` 签名扩展（nil 兼容），Spawn/reasonStep/finishProcess 中回调集成。已有 42 个 kernel 测试全部通过。
- ✅ Task 9: 信号处理。SIGINT/SIGTERM 注册，首次信号调用 `proc.Cancel()` 优雅退出，2 秒内二次信号 `os.Exit(130)` 强制退出。新增 `Process.Cancel()` 导出方法。
- ✅ Task 10: 完整测试覆盖。7 个测试文件，全量 `go test -race ./...` 通过（cmd/crux 8 个测试 + internal/ui 27 个测试 + kernel 42 个测试 = 77+ 测试全部通过）。

### Implementation Plan

1. 先实现底层 UI 组件（Task 1-6）：renderer → styles → progress → result → error → summary
2. 再实现 Kernel 回调机制（Task 8）作为 Task 7 的前置
3. 最后扩展 CLI 入口（Task 7）+ 信号处理（Task 9）+ 补充测试（Task 10）
4. 每个 Task 遵循 red-green-refactor：先写测试 → 实现 → 验证通过
5. `NewKernel` 签名变更后立即更新所有已有测试（传 `nil` callbacks）

### File List

**新增文件：**
- `internal/ui/renderer.go` — TerminalProfile + Renderer + OutputMode + DetectProfile
- `internal/ui/renderer_test.go` — 7 个测试
- `internal/ui/styles.go` — lipgloss 颜色常量 + 样式集 + InitStyles
- `internal/ui/styles_test.go` — 3 个测试
- `internal/ui/progress.go` — ProgressReporter + KernelMessage/AgentMessage/AgentStep
- `internal/ui/progress_test.go` — 5 个测试
- `internal/ui/result.go` — RenderResult + 双线边框
- `internal/ui/result_test.go` — 5 个测试
- `internal/ui/error.go` — RenderError + 三行结构
- `internal/ui/error_test.go` — 4 个测试
- `internal/ui/summary.go` — RenderSummary + 退出码着色
- `internal/ui/summary_test.go` — 4 个测试
- `cmd/crux/main_test.go` — 8 个测试

**修改文件：**
- `cmd/crux/main.go` — 大幅扩展：依赖注入 + 意图命令 + flags + 信号处理 + cliCallbacks + JSONResponse
- `kernel/kernel.go` — 新增 KernelCallbacks 接口 + callbacks 字段 + NewKernel 签名扩展 + 回调调用点
- `kernel/kernel_test.go` — 更新所有 NewKernel 调用传 nil callbacks（7 处）
- `kernel/process.go` — 新增导出的 Cancel() 方法
- `go.mod` — 新增 lipgloss + golang.org/x/term + 传递依赖
- `go.sum` — 自动更新

## Change Log

- 2026-02-24: Story 1.7 完整实现 — CLI 入口扩展 + 6 个 UI 组件 + Kernel 回调机制 + 信号处理 + 完整测试覆盖
