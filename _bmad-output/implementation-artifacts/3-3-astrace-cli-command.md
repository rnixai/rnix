# Story 3.3: astrace CLI 命令

Status: review

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a 用户,
I want 通过 `crux astrace <pid>` 命令启动 syscall 追踪,
So that 我可以在任何时候调试正在运行的智能体。

## Acceptance Criteria

1. **astrace 子命令注册** — Given `cmd/crux/main.go` 中 astrace 子命令已注册，When 执行 `crux astrace 1`，Then 附着到 PID 1 的 DebugChan，开始流式输出 syscall 事件

2. **PID 不存在错误** — Given 指定的 PID 不存在，When 执行 `crux astrace 999`，Then 输出三行错误结构：`✗ PID 999: process not found` + `→ 建议: crux ps  查看活跃进程`（使用 `ui.RenderError`）

3. **Ctrl+C 仅 detach** — Given astrace 正在追踪，When 用户按 Ctrl+C，Then 仅 detach 追踪（取消 Attach 的 context），不影响被追踪进程的运行

4. **进程完成 detach 汇总** — Given 被追踪进程完成，When DebugChan 关闭导致 `Attach` 返回 nil，Then astrace 输出 detach 汇总信息后退出（如 `[astrace] detached from PID 1 (process exited)`）

5. **--verbose flag** — Given 使用 `--verbose` flag，When 格式化 SyscallEvent，Then 展开完整的参数和返回值（不截断长参数），通过 `Options.Verbose = true` 传递

6. **--json flag** — Given 使用 `--json` flag，When 格式化 SyscallEvent，Then 每行输出一个 JSON 对象，字段为 snake_case（`timestamp_ms`、`pid`、`syscall`、`args`、`result`、`error`、`duration_ms`）

7. **PID 参数校验** — Given PID 参数非数字（如 `crux astrace abc`），When 解析参数，Then 输出 `✗ crux astrace abc: invalid PID (expected number)`

8. **缺少 PID 参数** — Given 未提供 PID 参数（如 `crux astrace`），When cobra 解析命令，Then 输出用法帮助信息

9. **attach 确认消息** — Given astrace 开始追踪，When 成功附着到目标进程，Then 输出 header 行 `[astrace] attached to PID {N} (state: {state})`

10. **全局 flag 兼容** — Given 全局 `--json` flag，When astrace 子命令执行，Then `--json` 在 astrace 中也生效（JSON 流式输出）

11. **所有测试通过** — Given 实现完成，When 执行 `go test -race ./...`，Then 所有新增和现有测试通过，无竞态条件；`go vet ./...` 无警告

## Tasks / Subtasks

- [x] Task 1: 在 `debug/astrace.go` 中添加 JSON 格式化支持 (AC: #6)
  - [x] 1.1 定义 `jsonEvent` 结构体 — 私有类型，snake_case json tags（`timestamp_ms`、`pid`、`syscall`、`args`、`result`、`error`、`duration_ms`）
  - [x] 1.2 实现 `FormatEventJSON(event types.SyscallEvent) string` — 导出函数，将 SyscallEvent 序列化为 JSON 行
  - [x] 1.3 扩展 `Options` — 添加 `JSON bool` 字段
  - [x] 1.4 更新 `Attach` 函数 — 当 `opts.JSON` 为 true 时使用 `FormatEventJSON` 代替 `FormatEvent`

- [x] Task 2: 在 `debug/astrace_test.go` 中添加 JSON 格式化测试 (AC: #6, #11)
  - [x] 2.1 `TestFormatEventJSON_BasicFormat` — 验证 JSON 输出包含所有必需字段
  - [x] 2.2 `TestFormatEventJSON_Error` — 验证 err 字段在错误时非空，无错误时为空字符串
  - [x] 2.3 `TestFormatEventJSON_SnakeCaseFields` — 验证 JSON 字段名为 snake_case
  - [x] 2.4 `TestAttach_JSONMode` — 验证 JSON 模式下 Attach 输出 JSON 行

- [x] Task 3: 在 `cmd/crux/main.go` 中注册 astrace 子命令 (AC: #1-#10)
  - [x] 3.1 定义 `astraceCmd` — `cobra.Command{Use: "astrace <pid>", ...}`，`Args: cobra.ExactArgs(1)`
  - [x] 3.2 在 `init()` 中 `rootCmd.AddCommand(astraceCmd)` 注册子命令
  - [x] 3.3 实现 `runAstrace(cmd *cobra.Command, args []string) error` — 主执行函数
  - [x] 3.4 PID 解析 — `strconv.Atoi(args[0])`，非数字时返回格式化错误 (AC: #7)
  - [x] 3.5 进程查找 — `kern.GetProcess(pid)`，不存在时使用 `ui.RenderError` 输出三行错误 (AC: #2)
  - [x] 3.6 attach 确认输出 — `[astrace] attached to PID {N} (state: {state})` (AC: #9)
  - [x] 3.7 信号处理 — 捕获 SIGINT/SIGTERM，cancel astrace context（仅 detach 追踪，不 cancel 进程）(AC: #3)
  - [x] 3.8 调用 `debug.Attach` — 使用 astrace-specific context（不影响被追踪进程）(AC: #1)
  - [x] 3.9 detach 汇总输出 — Attach 返回 nil（进程完成）或 ctx.Err()（用户中断）后输出汇总 (AC: #4)
  - [x] 3.10 `--json` + `--verbose` flag 传递 — 从全局 flag 读取并传入 `debug.Options` (AC: #5, #6, #10)

- [x] Task 4: 添加 astrace 命令集成测试 (AC: #1-#11)
  - [x] 4.1 `TestAstraceCmd_PIDNotFound` — 验证 PID 不存在时的错误输出
  - [x] 4.2 `TestAstraceCmd_InvalidPID` — 验证非数字 PID 的错误输出
  - [x] 4.3 `TestAstraceCmd_AttachAndDetach` — 模拟进程完成，验证 attach + detach 流程
  - [x] 4.4 `TestAstraceCmd_JSONOutput` — 验证 `--json` flag 下的 JSON 流式输出
  - [x] 4.5 `TestAstraceCmd_VerboseOutput` — 验证 `--verbose` flag 下参数不截断

- [x] Task 5: 更新 sprint-status.yaml (AC: #11)
  - [x] 5.1 将 `3-3-astrace-cli-command` 状态从 `backlog` 更新为 `ready-for-dev`

## Dev Notes

### 核心设计决策

#### 依赖关系与职责划分

```
cmd/crux/main.go (astrace 子命令)
  ├── 导入 debug/          — Attach, FormatEvent, FormatEventJSON, Options, DefaultOptions
  ├── 导入 kernel/         — GetProcess, Process (DebugChan 访问)
  ├── 导入 internal/types/ — PID, SyscallEvent
  ├── 导入 internal/ui/    — RenderError, Renderer, OutputMode
  ├── 导入 os/signal       — SIGINT/SIGTERM 捕获
  └── 导入 strconv         — PID 解析
```

**职责边界：**
- `debug/astrace.go` — 纯事件消费和格式化（无 CLI 逻辑）
- `cmd/crux/main.go` — CLI 命令注册、参数解析、信号处理、UI 输出

#### astrace 子命令注册模式

遵循现有 versionCmd 的模式：

```go
var astraceCmd = &cobra.Command{
    Use:   "astrace <pid>",
    Short: "Trace syscalls of an agent process in real-time",
    Long:  "Attach to a running agent process and stream its syscall events in real-time.\n\nPress Ctrl+C to detach without affecting the traced process.",
    Example: `  crux astrace 1              Trace PID 1 (default mode)
  crux astrace 1 --verbose    Show full syscall details
  crux astrace 1 --json       Output as JSON stream`,
    Args: cobra.ExactArgs(1),
    RunE: runAstrace,
}
```

#### 信号处理设计（关键差异点）

**与根命令 signal handling 的关键差异：**
- 根命令的 SIGINT → `proc.Cancel()` → 终止被追踪的进程
- astrace 的 SIGINT → `cancel()` astrace 自身的 context → 仅 detach 追踪，**绝不调用 proc.Cancel()**

```go
func runAstrace(cmd *cobra.Command, args []string) error {
    // 1. 解析 PID
    pidNum, err := strconv.Atoi(args[0])
    if err != nil {
        return fmt.Errorf("✗ crux astrace %s: invalid PID (expected number)", args[0])
    }
    pid := types.PID(pidNum)

    // 2. 查找进程
    proc, ok := kern.GetProcess(pid)
    if !ok {
        w := cmd.OutOrStdout()
        renderer := ui.NewRenderer(w, getOutputMode())
        ui.RenderError(renderer, fmt.Sprintf("PID %d", pid),
            "process not found", "",
            "crux ps  查看活跃进程")
        return nil // 已输出错误，不返回 error（避免 cobra 再打印）
    }

    // 3. 构建 Options
    opts := debug.DefaultOptions()
    opts.Verbose = flagVerbose
    opts.JSON = flagJSON

    // 4. 输出 attach 确认（非 JSON 模式）
    w := cmd.OutOrStdout()
    if !flagJSON {
        state := proc.GetState()
        fmt.Fprintf(w, "[astrace] attached to PID %d (state: %s)\n", pid, state)
    }

    // 5. 设置 astrace-specific context（SIGINT 仅 detach）
    astraceCtx, astraceCancel := context.WithCancel(cmd.Context())
    defer astraceCancel()

    sigCh := make(chan os.Signal, 1)
    signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
    defer signal.Stop(sigCh)

    go func() {
        <-sigCh
        astraceCancel() // 仅取消 astrace，不影响被追踪进程
    }()

    // 6. 执行 Attach
    err = debug.Attach(astraceCtx, proc.DebugChan, w, opts)

    // 7. 输出 detach 汇总
    if !flagJSON {
        if err == nil {
            // 进程完成，channel 关闭
            fmt.Fprintf(w, "[astrace] detached from PID %d (process exited)\n", pid)
        } else if err == context.Canceled {
            // 用户 Ctrl+C
            fmt.Fprintf(w, "\n[astrace] detached from PID %d (interrupted)\n", pid)
        } else {
            // 其他错误（如 broken pipe）
            fmt.Fprintf(w, "[astrace] detached from PID %d (error: %v)\n", pid, err)
        }
    }

    return nil
}
```

#### JSON 格式化设计

在 `debug/astrace.go` 中新增 JSON 支持：

```go
// jsonEvent is the JSON representation of a SyscallEvent.
type jsonEvent struct {
    TimestampMs int64          `json:"timestamp_ms"`
    PID         types.PID      `json:"pid"`
    Syscall     string         `json:"syscall"`
    Args        map[string]any `json:"args"`
    Result      any            `json:"result"`
    Error       string         `json:"error"`
    DurationMs  int64          `json:"duration_ms"`
}

// FormatEventJSON formats a SyscallEvent as a single-line JSON string.
func FormatEventJSON(event types.SyscallEvent) string {
    je := jsonEvent{
        TimestampMs: event.Timestamp.Milliseconds(),
        PID:         event.PID,
        Syscall:     event.Syscall,
        Args:        event.Args,
        Result:      event.Result,
        Error:       "", // 空字符串而非 null
        DurationMs:  event.Duration.Milliseconds(),
    }
    if event.Err != nil {
        je.Error = event.Err.Error()
    }
    // Result 需要特殊处理：error 类型无法 JSON 序列化
    if event.Result != nil {
        if _, ok := event.Result.(error); ok {
            je.Result = fmt.Sprintf("%v", event.Result)
        }
    }
    data, _ := json.Marshal(je) // jsonEvent 字段都是 JSON-safe 类型
    return string(data)
}
```

**Options 扩展：**

```go
type Options struct {
    ColorEnabled bool
    Verbose      bool
    JSON         bool // 新增：JSON 输出模式
}
```

**Attach 更新：**

```go
func Attach(ctx context.Context, ch <-chan types.SyscallEvent, w io.Writer, opts Options) error {
    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
        case event, ok := <-ch:
            if !ok {
                return nil
            }
            var line string
            if opts.JSON {
                line = FormatEventJSON(event)
            } else {
                line = FormatEvent(event, opts)
            }
            if _, err := fmt.Fprintln(w, line); err != nil {
                return err
            }
        }
    }
}
```

#### 错误输出模式

遵循 UX 设计规范的三行错误结构和已有的 `ui.RenderError` 函数：

```go
// PID 不存在 — 使用 RenderError
ui.RenderError(renderer, fmt.Sprintf("PID %d", pid),
    "process not found",
    "",  // impact 留空（进程不存在，无状态影响）
    "crux ps  查看活跃进程")

// 输出效果：
// ✗ PID 999: process not found
//   → 建议: crux ps  查看活跃进程
```

```go
// PID 参数非数字 — 直接返回格式化 error（让 cobra 处理）
return fmt.Errorf("✗ crux astrace %s: invalid PID (expected number)", args[0])
```

#### 帮助信息

遵循 UX 规范定义的帮助格式：

```
Trace syscalls of an agent process in real-time

Usage:
  crux astrace <pid> [flags]

Arguments:
  pid    Process ID to trace (required)

Flags:
  --verbose, -v         Show full arguments and return values
  --json                Output as JSON stream

Examples:
  crux astrace 1              Trace PID 1 (default mode)
  crux astrace 1 --verbose    Show full syscall details
  crux astrace 1 --json       Output as JSON stream
```

### 前序 Story 经验（Story 3.2）

**Story 3.2 完成的 API（直接使用，本 Story 扩展）：**
- `debug/astrace.go` — `Attach`、`FormatEvent`、`Options`、`DefaultOptions` 已完整实现
- `debug/astrace_test.go` — 17 个测试用例，覆盖格式化、附着、取消、通道关闭
- `internal/types/types.go` — `SyscallEvent` 结构体
- `debug/event.go` — `EmitEvent`、`NewEvent`、`CompleteEvent`

**Story 3.2 Dev Notes 关键经验：**
- ANSI 颜色嵌套 Bug — error+slow+color 时灰色标注的 `ansiReset` 打断红色包裹。已修复：error 行跳过灰色。
- `Attach` 必须返回 write error（broken pipe），不能静默吞掉。
- 测试中 `bytes.Buffer` 非线程安全，Attach goroutine 写入时用 channel 通知代替 `buf.Len()` 轮询。

**Story 3.2 测试命名规范（保持一致）：**
- 函数命名：`TestTypeName_Behavior`（如 `TestFormatEventJSON_BasicFormat`）
- 不用 testify，用标准库 `t.Fatalf` / `t.Fatal` / `t.Errorf`

### 前序 Story Git 提交模式

```
f536ccc Finalize Story 3.2: Astrace Event Consumption and Formatting implementation...
931bacf Update Story 3.2 status to 'review'...
b659759 Finalize Story 3.1: SyscallEvent Recording Infrastructure implementation...
```
- 提交消息：英文动词短语开头（Implement / Add / Update / Fix / Finalize）

### 已有代码关键 API 参考

**kernel/kernel.go — GetProcess：**
```go
func (k *KernelImpl) GetProcess(pid types.PID) (*kernel.Process, bool) {
    return k.procTable.Load(pid)
}
```

**kernel/process.go — Process.GetState：**
```go
func (p *Process) GetState() types.ProcessState
```

**kernel/process.go — Process.DebugChan：**
```go
DebugChan chan types.SyscallEvent  // 缓冲 256，公开字段
```

**cmd/crux/main.go — 全局变量模式：**
```go
var (
    flagJSON    bool
    flagVerbose bool
    flagQuiet   bool
    // ...
    kern     *kernel.KernelImpl  // 全局 kernel 实例
    exitCode int
)
```

**cmd/crux/main.go — 输出模式辅助函数：**
```go
func getOutputMode() ui.OutputMode {
    switch {
    case flagJSON:
        return ui.ModeJSON
    case flagVerbose:
        return ui.ModeVerbose
    case flagQuiet:
        return ui.ModeQuiet
    default:
        return ui.ModeDefault
    }
}
```

**cmd/crux/main.go — 信号处理参考（根命令）：**
```go
sigCh := make(chan os.Signal, 2)
signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
go func() {
    <-sigCh
    progress.KernelMessage("PID %d interrupted (SIGINT)", pid)
    proc.Cancel()
    select {
    case <-sigCh:
        forceExitFunc(130)
    case <-time.After(2 * time.Second):
    }
}()
```

### 注意事项与防错

#### ⚠️ 信号处理的关键差异
根命令的 SIGINT 调用 `proc.Cancel()` 终止进程。astrace 的 SIGINT **绝不能**调用 `proc.Cancel()`，只能 cancel 自身的 context（detach 追踪）。这是两个完全不同的信号处理策略。

#### ⚠️ Kernel 实例获取
`cmd/crux/main.go` 中 kernel 实例（`kern`）是在 `runRoot` 中才初始化的。astrace 命令需要独立初始化 kernel 实例，或确保 `kern` 在 astrace 路径中可用。需检查当前代码中 `kern` 的初始化时机和作用域。

**解决方案：** 参考 `runRoot` 的初始化逻辑，在 `runAstrace` 中同样初始化 kernel（创建 VFS、注册设备、创建 kernel 实例）。或者，将 kernel 初始化提取为共享的 `initKernel()` 函数供根命令和子命令共用。

#### ⚠️ 已存在的编译诊断

当前 `debug/astrace_test.go` 存在编译错误：`undefined: os`（第 449、450、452 行）。这可能来自 Story 3.2 的未完成修改。**本 Story 必须修复这些编译错误才能通过 AC #11。**

#### ⚠️ JSON 输出中 Result 字段的序列化

`SyscallEvent.Result` 类型为 `any`，可能包含无法 JSON 序列化的类型（如 `error` 接口）。`FormatEventJSON` 必须处理这些边界情况：
- `error` 类型 → 序列化为 `fmt.Sprintf("%v", result)`
- `nil` → 序列化为 JSON `null`
- 其他类型 → `json.Marshal` 默认处理

#### ⚠️ 新增 import

`debug/astrace.go` 新增 `encoding/json` 导入。这仍然是标准库，符合 debug/ 包的依赖约束（仅依赖标准库 + internal/types/）。

### NFR 合规

| NFR | 要求 | 实现保证 |
|-----|------|---------|
| NFR3 | astrace 输出延迟 ≤ 500ms | `Attach` 每次 select 收到事件后立即写出，Story 3.2 已验证延迟 ~92µs |
| NFR10 | CLI 进程在智能体异常退出时不崩溃 | Ctrl+C 仅 detach，DebugChan 关闭返回 nil |
| NFR18 | 通过 go vet 和 golint 无警告 | 新增代码遵循 Go 惯例 |

### 范围边界

**本 Story 包含：**
- `debug/astrace.go` — 扩展 Options（添加 JSON）、新增 FormatEventJSON、更新 Attach
- `debug/astrace_test.go` — 新增 JSON 格式化测试、修复现有编译错误
- `cmd/crux/main.go` — 注册 astrace 子命令、runAstrace 实现

**本 Story 不包含：**
- `--filter` flag（UX 规范提到但非 MVP AC 要求，为 Phase 2 预留）
- `internal/ui/trace.go` Syscall Trace Line 组件（Story 3.4 — 涉及 lipgloss 样式，与 debug/ 的 raw ANSI 不同）
- 修改 `kernel/` 或 `debug/event.go`

### Project Structure Notes

**新建文件：**
```
（无新建文件，全部是修改现有文件）
```

**修改文件：**
```
debug/astrace.go                — 扩展：Options.JSON + FormatEventJSON + Attach JSON 分支 + import encoding/json
debug/astrace_test.go           — 新增 JSON 测试 + 修复现有编译错误
cmd/crux/main.go                — 新增：astraceCmd 定义 + runAstrace 实现 + init() 注册
_bmad-output/implementation-artifacts/sprint-status.yaml  — 状态更新
```

**不修改文件：**
```
debug/event.go                  — Story 3.1 实现，不修改
kernel/kernel.go                — 已有 GetProcess API，不修改
kernel/process.go               — 已有 GetState/DebugChan，不修改
internal/ui/error.go            — 已有 RenderError，不修改
internal/ui/renderer.go         — 已有 NewRenderer，不修改
```

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story 3.3] — Story 定义和验收标准
- [Source: _bmad-output/planning-artifacts/architecture.md#Decision 5] — DebugChan 机制和 astrace 数据流
- [Source: _bmad-output/planning-artifacts/architecture.md#CLI 框架] — Cobra 子命令注册模式
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md#Syscall Trace Line] — astrace 输出格式规范
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md#命令帮助] — `crux astrace --help` 格式
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md#中断处理] — astrace Ctrl+C 仅 detach 规则
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md#边界处理] — PID 不存在/非数字错误格式
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md#astrace 布局] — attach 确认 + 实时流 + detach 汇总
- [Source: _bmad-output/project-context.md#CLI 命令结构] — 子命令和全局 flags
- [Source: _bmad-output/project-context.md#Channel 使用规则] — DebugChan 缓冲 256 + 关闭责任在生产者
- [Source: _bmad-output/project-context.md#JSON 字段命名] — snake_case 规范
- [Source: debug/astrace.go] — Attach/FormatEvent/Options 现有 API
- [Source: debug/astrace_test.go] — 测试命名和断言风格参考
- [Source: cmd/crux/main.go:init()] — Cobra 命令注册模式
- [Source: cmd/crux/main.go:224-237] — 根命令信号处理参考
- [Source: kernel/kernel.go:GetProcess()] — 进程查找 API
- [Source: internal/ui/error.go:RenderError()] — 三行错误输出 API
- [Source: _bmad-output/implementation-artifacts/3-2-astrace-event-consumption-and-formatting.md] — 前序 Story 经验和 Dev Notes

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6

### Debug Log References

### Completion Notes List

- Task 1: 在 `debug/astrace.go` 中添加了 JSON 格式化支持。新增 `jsonEvent` 结构体（snake_case json tags）、`FormatEventJSON` 导出函数、`Options.JSON` 字段，并更新 `Attach` 在 JSON 模式下使用 `FormatEventJSON`。新增 `encoding/json` 导入。
- Task 2: 在 `debug/astrace_test.go` 中添加了 4 个 JSON 格式化测试：`TestFormatEventJSON_BasicFormat`（全字段验证）、`TestFormatEventJSON_Error`（错误字段验证）、`TestFormatEventJSON_SnakeCaseFields`（JSON 字段名验证）、`TestAttach_JSONMode`（JSON 模式 Attach 验证）。新增 `encoding/json` 导入。
- Task 3: 在 `cmd/crux/main.go` 中注册 `astraceCmd` 子命令，实现 `runAstrace` 函数。包含：PID 解析、进程查找（RenderError 三行错误）、attach 确认输出、SIGINT 信号处理（仅 detach 不 kill 进程）、`debug.Attach` 调用、detach 汇总输出、`--json`/`--verbose` flag 传递。提取 `initKernel()` 共享函数供根命令和子命令复用。添加 `processStateName` 辅助函数将 `ProcessState` int 转换为可读字符串。将 `kern` 提升为包级变量。新增 `context`、`strconv`、`debug` 导入。
- Task 4: 在 `cmd/crux/integration_test.go` 中添加了 5 个 astrace 集成测试：`TestAstraceCmd_PIDNotFound`（错误输出验证）、`TestAstraceCmd_InvalidPID`（非数字 PID 错误）、`TestAstraceCmd_AttachAndDetach`（完整 attach+detach 流程）、`TestAstraceCmd_JSONOutput`（JSON 流式输出验证）、`TestAstraceCmd_VerboseOutput`（verbose 模式验证）。添加 `astraceTestKernel` 测试辅助函数。新增 `bytes`、`cobra` 导入。
- Task 5: sprint-status.yaml 已从 `ready-for-dev` 经 `in-progress` 更新至 `review`。

### File List

- `debug/astrace.go` — 扩展：Options.JSON + jsonEvent 结构体 + FormatEventJSON + Attach JSON 分支 + import encoding/json
- `debug/astrace_test.go` — 新增 4 个 JSON 测试 + import encoding/json
- `cmd/crux/main.go` — 新增：astraceCmd 定义 + runAstrace 实现 + initKernel + processStateName + init() 注册 + kern 包级变量 + import context/strconv/debug
- `cmd/crux/integration_test.go` — 新增 5 个 astrace 集成测试 + astraceTestKernel 辅助 + import bytes/cobra
- `_bmad-output/implementation-artifacts/sprint-status.yaml` — 状态更新 review
- `_bmad-output/implementation-artifacts/3-3-astrace-cli-command.md` — 任务 checkbox + Dev Agent Record + File List + Status
