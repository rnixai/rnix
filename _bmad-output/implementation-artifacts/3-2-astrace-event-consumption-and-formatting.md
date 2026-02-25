# Story 3.2: astrace 事件消费与格式化

Status: review

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a 用户,
I want `crux astrace <pid>` 实时流式输出 syscall 调用链路,
So that 我能看到智能体的每一步操作及其结果。

## Acceptance Criteria

1. **Astrace 附着机制** — Given `debug/astrace.go` 已实现，When 调用 `Attach` 附着到指定进程的 DebugChan，Then 开始消费目标进程的 `chan types.SyscallEvent`，每收到一个事件立即格式化并写入 `io.Writer`

2. **基础输出格式** — Given astrace 流式输出中，When 收到一个 SyscallEvent，Then 输出格式为 `[N.NNNs] SyscallName(args) → result    duration`，其中时间戳固定宽度 `[N.NNNs]`（7 字符数字，如 `[  0.012s]`），syscall 名称与接口方法名一致（FR29）

3. **慢操作标注** — Given 某个 syscall 耗时 > 1 秒，When 格式化该事件，Then 行尾自动追加 `  ← 慢操作` 标注（颜色启用时为暗灰色 `#666666`，禁色时为纯文本）

4. **错误行高亮** — Given 某个 syscall 的 `Err` 字段非 nil，When 格式化该事件，Then 颜色启用时整行用红色高亮（ANSI bright red `\033[91m`），禁色时行首前缀 `[ERR] `（FR31）

5. **LLM syscall 标注** — Given syscall 的 `Args` 中 path 或 tool 参数包含 `/dev/llm/`，When 格式化该事件，Then 行尾追加 `  ← LLM 调用` 标注（与慢操作标注互不排斥，两者均满足时同时显示）

6. **输出延迟合规** — Given astrace 流式输出，When 从 syscall 发生（事件写入 DebugChan）到终端显示，Then 延迟 ≤ 500ms（NFR3）—— 消费者必须立即写出，不得批量缓冲

7. **上下文取消** — Given astrace 正在消费中，When 传入的 `context.Context` 被取消（用户按 Ctrl+C），Then `Attach` 函数立即返回 `ctx.Err()`，不阻塞

8. **Channel 关闭处理** — Given astrace 正在消费中，When 目标进程的 DebugChan 被关闭（进程完成），Then `Attach` 函数返回 `nil`，让调用方决定后续行为（如输出 detach 汇总——由 Story 3.3 实现）

9. **NO_COLOR 支持** — Given 环境变量 `NO_COLOR` 已设置（任意值），When 调用 `DefaultOptions()`，Then `ColorEnabled` 自动为 `false`，所有 ANSI 颜色码不输出

10. **FormatEvent 可测试** — Given `FormatEvent(event types.SyscallEvent, opts Options) string` 已导出，When 单元测试直接调用，Then 不依赖任何 I/O 或外部状态，纯函数，可独立验证每个格式化规则

11. **args 截断** — Given SyscallEvent 的 Args 中某个值的字符串表示超过 50 字符，When 格式化，Then 截断为前 47 字符并追加 `...`；`--verbose` 模式下不截断（通过 `Options.Verbose` 控制）

12. **所有测试通过** — Given 实现完成，When 执行 `go test -race ./debug/...`，Then 所有新增和现有测试通过，无竞态条件；`go vet ./...` 无警告

## Tasks / Subtasks

- [x] Task 1: 创建 `debug/astrace.go` — 核心消费与格式化 (AC: #1-#11)
  - [x] 1.1 定义 `Options` 结构体 — `ColorEnabled bool`, `Verbose bool`
  - [x] 1.2 实现 `DefaultOptions() Options` — 自动检测 `NO_COLOR` 环境变量
  - [x] 1.3 实现 `Attach(ctx context.Context, ch <-chan types.SyscallEvent, w io.Writer, opts Options) error` — 主消费循环，select on ctx.Done() 和 ch
  - [x] 1.4 实现 `FormatEvent(event types.SyscallEvent, opts Options) string` — 导出的纯函数格式化器
  - [x] 1.5 实现 `formatTimestamp(ts time.Duration) string` — `[  0.012s]` 固定宽度格式
  - [x] 1.6 实现 `formatArgs(args map[string]any, verbose bool) string` — key=value 排序输出，支持截断
  - [x] 1.7 实现 `formatResult(result any, err error) string` — 成功返回 `%v`，错误返回 `err(%v)`
  - [x] 1.8 实现 `formatDuration(d time.Duration) string` — `µs`/`ms`/`s` 自适应单位
  - [x] 1.9 实现 `isLLMSyscall(args map[string]any) bool` — 检查 path/tool 参数是否包含 `/dev/llm/`

- [x] Task 2: 创建 `debug/astrace_test.go` — 全面测试 (AC: #2-#12)
  - [x] 2.1 `TestFormatEvent_BasicFormat` — 验证完整格式字符串
  - [x] 2.2 `TestFormatEvent_SlowOp` — duration > 1s 时追加慢操作标注
  - [x] 2.3 `TestFormatEvent_Error_WithColor` — err 非 nil 时红色前缀（验证 ANSI 码存在）
  - [x] 2.4 `TestFormatEvent_Error_NoColor` — err 非 nil 且禁色时 `[ERR] ` 前缀
  - [x] 2.5 `TestFormatEvent_LLMAnnotation` — path 包含 `/dev/llm/` 时追加 LLM 标注
  - [x] 2.6 `TestFormatEvent_SlowAndError` — 慢操作 + 错误同时满足时同时显示
  - [x] 2.7 `TestFormatArgs_Truncation` — 值超 50 字符时截断为 47 + `...`
  - [x] 2.8 `TestFormatArgs_Verbose_NoTruncation` — verbose=true 时不截断
  - [x] 2.9 `TestFormatArgs_SortedKeys` — 参数按字母排序（保证输出确定性）
  - [x] 2.10 `TestAttach_ConsumesEvents` — 发送若干事件，验证全部写入 writer
  - [x] 2.11 `TestAttach_ContextCancellation` — ctx 取消时立即返回 `ctx.Err()`
  - [x] 2.12 `TestAttach_ChannelClose` — channel 关闭时返回 `nil`
  - [x] 2.13 `TestAttach_Latency` — 从写入 channel 到 writer 收到输出的延迟 ≤ 10ms（远低于 NFR3 的 500ms，确保充裕余量）
  - [x] 2.14 `TestDefaultOptions_NoColor` — `NO_COLOR` 环境变量设置时 `ColorEnabled=false`

- [x] Task 3: 更新 sprint-status.yaml (AC: #12)
  - [x] 3.1 将 `3-2-astrace-event-consumption-and-formatting` 状态从 `backlog` 更新为 `ready-for-dev`

## Dev Notes

### 核心设计决策

#### 依赖方向约束

`debug/` 包只能导入 `internal/types/`（零外部依赖）。这是严格的架构规则：

```
debug/astrace.go ← 仅依赖:
  - internal/types/  (SyscallEvent, PID)
  - 标准库: context, fmt, io, os, sort, strings, time
```

**禁止导入：**
- `internal/ui/` — UI 层单向依赖（ui/ 可以用 debug/，反之不行）
- `github.com/charmbracelet/lipgloss` — UI 依赖库，debug/ 不可用
- `kernel/` — 禁止反向依赖

**颜色方案：** 使用 raw ANSI 转义码（标准库 strings 即可），不用 lipgloss：

```go
const (
    ansiRed   = "\033[91m"  // bright red — 错误行
    ansiGray  = "\033[90m"  // dark gray  — 慢操作标注（#666666 近似）
    ansiReset = "\033[0m"
)
```

#### API 设计

```go
// Options configures Attach and FormatEvent behavior.
type Options struct {
    // ColorEnabled controls ANSI color output.
    // Use DefaultOptions() to auto-detect based on NO_COLOR env var.
    ColorEnabled bool

    // Verbose disables argument truncation (default: truncate at 50 chars).
    Verbose bool
}

// DefaultOptions returns Options with sensible defaults:
// - ColorEnabled: true unless NO_COLOR env var is set
// - Verbose: false
func DefaultOptions() Options {
    _, noColor := os.LookupEnv("NO_COLOR")
    return Options{
        ColorEnabled: !noColor,
        Verbose:      false,
    }
}

// Attach consumes SyscallEvents from ch and writes formatted trace lines to w.
// Returns ctx.Err() when context is cancelled, nil when ch is closed.
// Each event is written immediately (no batching) to satisfy NFR3 (≤500ms latency).
func Attach(ctx context.Context, ch <-chan types.SyscallEvent, w io.Writer, opts Options) error {
    for {
        select {
        case <-ctx.Done():
            return ctx.Err()
        case event, ok := <-ch:
            if !ok {
                return nil // process done, channel closed
            }
            line := FormatEvent(event, opts)
            fmt.Fprintln(w, line)
        }
    }
}
```

#### FormatEvent 格式规范

**标准行格式：**
```
[  0.012s] Open(flags=2, path="/dev/llm/claude") → FD(3)    1ms
```

**组成部分：**

| 部分 | 规则 | 示例 |
|------|------|------|
| 时间戳 | `[%7.3fs]` — 7 字符数字+单位，括号固定 | `[  0.012s]` |
| 名称 | SyscallEvent.Syscall，不加修饰 | `Open` |
| 参数 | `(key1=val1, key2=val2)`，key 按字母排序 | `(flags=2, path="/dev/llm/claude")` |
| 分隔符 | ` → ` | |
| 结果 | 成功时 `%v`，错误时 `err(%v)` | `FD(3)` 或 `err(device not found)` |
| 间距 | 4 空格分隔结果与耗时 | |
| 耗时 | 自适应单位 | `1ms`, `500µs`, `2.05s` |

**标注规则（追加到行尾）：**

```go
// 慢操作（duration > 1s）
if event.Duration > time.Second {
    if opts.ColorEnabled {
        annotations += "  " + ansiGray + "← 慢操作" + ansiReset
    } else {
        annotations += "  ← 慢操作"
    }
}

// LLM 调用（args 中 path 或 tool 包含 /dev/llm/）
if isLLMSyscall(event.Args) {
    annotations += "  ← LLM 调用"
}
```

**错误行（整行着色）：**

```go
if event.Err != nil {
    if opts.ColorEnabled {
        return ansiRed + line + ansiReset
    }
    return "[ERR] " + line
}
```

**注意：** 错误行着色应在构建完整 line（含标注）后再包裹 ANSI 码，避免颜色码干扰标注字符串。

#### 参数格式化规则

```go
func formatArgs(args map[string]any, verbose bool) string {
    if len(args) == 0 {
        return ""
    }
    // 按字母排序保证确定性
    keys := make([]string, 0, len(args))
    for k := range args {
        keys = append(keys, k)
    }
    sort.Strings(keys)

    parts := make([]string, 0, len(keys))
    for _, k := range keys {
        v := fmt.Sprintf("%v", args[k])
        if !verbose && len(v) > 50 {
            v = v[:47] + "..."
        }
        // 字符串值加引号
        if s, ok := args[k].(string); ok {
            v = fmt.Sprintf("%q", s)
            if !verbose && len(v) > 52 { // 50 + 2 quotes
                v = v[:49] + `..."`
            }
        }
        parts = append(parts, k+"="+v)
    }
    return strings.Join(parts, ", ")
}
```

#### 耗时格式化

```go
func formatDuration(d time.Duration) string {
    switch {
    case d < time.Millisecond:
        return fmt.Sprintf("%dµs", d.Microseconds())
    case d < time.Second:
        return fmt.Sprintf("%dms", d.Milliseconds())
    default:
        return fmt.Sprintf("%.2fs", d.Seconds())
    }
}
```

#### LLM Syscall 检测

```go
func isLLMSyscall(args map[string]any) bool {
    for _, key := range []string{"path", "tool"} {
        if v, ok := args[key]; ok {
            if s, ok := v.(string); ok {
                if strings.Contains(s, "/dev/llm/") {
                    return true
                }
            }
        }
    }
    return false
}
```

### 典型事件格式化示例

基于 Story 3.1 中 kernel.go 实际发出的事件：

```
[  0.000s] Spawn(intent="分析代码", agent="code-analyst") → 1    0µs
[  0.001s] CtxAlloc(size=64) → 1    0µs
[  0.001s] CtxWrite(cid=1, op="SetSystemPrompt") → <nil>    0µs
[  0.002s] CtxWrite(cid=1, op="AppendMessage", role="user") → <nil>    0µs
[  0.002s] Open(flags=2, path="/dev/llm/claude") → 3    0µs  ← LLM 调用
[  0.003s] CtxRead(cid=1, op="BuildPrompt") → <nil>    1ms
[  0.004s] Write(fd=3, size=256) → <nil>    0µs  ← LLM 调用
[  4.892s] Read(fd=3, length=1048576) → 1024    4888ms  ← 慢操作  ← LLM 调用
[  4.893s] CtxWrite(cid=1, op="AppendMessage", role="assistant") → <nil>    0µs
[  4.894s] Open(flags=2, path="/dev/fs/kernel/scheduler.go") → 4    0µs
[  4.894s] Write(fd=4, size=64) → <nil>    0µs
[  4.895s] Read(fd=4, length=1048576) → 8192    1ms
[  4.895s] Close(fd=4) → <nil>    0µs
[  4.896s] CtxWrite(cid=1, op="AppendToolResult", tool="/dev/fs/kernel/...") → <nil>    0µs
```

错误示例（红色行或 `[ERR] ` 前缀）：
```
[ERR] [  5.001s] Read(fd=3, length=1048576) → err(timeout after 30s)    30000ms  ← 慢操作  ← LLM 调用
```

### NFR 合规

| NFR | 要求 | 实现保证 |
|-----|------|---------|
| NFR3 | astrace 输出延迟 ≤ 500ms | `Attach` 每次 select 收到事件后立即 `fmt.Fprintln` 写出，无批量缓冲。`fmt.Fprintln` 到 `os.Stdout` (unbuffered) 通常 < 1ms |
| NFR18 | 通过 go vet 和 golint 无警告 | 新增代码遵循 Go 惯例，所有导出类型有注释 |

**为什么不需要特殊优化满足 NFR3：**
- `DebugChan` 缓冲 256，syscall 执行不等消费者
- 消费者 goroutine 持续 select，事件到达即处理
- `fmt.Fprintln` 到 `*os.File` (stdout) 是 syscall 级写入，通常 < 100µs
- 真正的延迟来自 channel 传递（已由 DebugChan 缓冲保证），估计 < 10µs

### 前序 Story 经验（Story 3.1）

**Story 3.1 完成的基础设施（直接使用，不要重复）：**
- `debug/event.go` — `EmitEvent`, `NewEvent`, `CompleteEvent` 已完整实现
- `debug/event_test.go` — 测试模式：`t.Fatalf` / `t.Fatal` 标准库断言
- `internal/types/types.go` — `SyscallEvent` 结构体定义（Timestamp/PID/Syscall/Args/Result/Err/Duration）
- `kernel/kernel.go` — `emitEvent()` 包装方法，所有 VFS/Context 调用均已埋点

**Story 3.1 测试文件的命名规范（保持一致）：**
- 函数命名：`TestTypeName_Behavior`（如 `TestEmitEvent_NilChannel`）
- 不用 testify，用标准库 `t.Fatalf` / `t.Fatal` / `t.Errorf`
- 并发测试用 `sync.WaitGroup` + 多 goroutine

**Story 3.1 代码风格观察：**
- 包级注释：`// Package debug provides ...`（在 event.go 中）
- 函数注释：完整的 godoc 格式
- 常量定义在文件顶部
- 错误处理遵循标准 Go 模式

### 前序 Story Git 提交模式

```
b659759 Finalize Story 3.1: SyscallEvent Recording Infrastructure implementation...
689de4c Update Story 3.1 status to 'review'...
06d9eeb Update project context and finalize Story 2.6...
```
- 提交消息：英文动词短语开头（Implement / Add / Update / Fix / Finalize）

### 范围边界

**本 Story 包含：**
- `debug/astrace.go` — `Options`、`DefaultOptions`、`Attach`、`FormatEvent` + 内部辅助函数
- `debug/astrace_test.go` — 全面单元测试

**本 Story 不包含：**
- `crux astrace <pid>` CLI 子命令（Story 3.3）
- `--json` / `--verbose` flag 与 CLI 的集成（Story 3.3，但 `Options.Verbose` 字段为其预留）
- Ctrl+C detach 汇总输出（Story 3.3）
- `internal/ui/trace.go` Syscall Trace Line 组件（Story 3.4）
- 任何 kernel/ 或 cmd/ 的修改

**Story 3.3 调用示例（供参考，本 Story 不实现）：**
```go
// 在 cmd/crux/main.go 的 astrace 子命令中
proc, ok := kernel.GetProcess(pid)
if !ok { /* 错误处理 */ }

opts := debug.DefaultOptions()
opts.Verbose = verboseFlag
err := debug.Attach(cmd.Context(), proc.DebugChan, os.Stdout, opts)
```

### 架构依赖验证

```
debug/astrace.go:
  import "context"           ✅ 标准库
  import "fmt"               ✅ 标准库
  import "io"                ✅ 标准库
  import "os"                ✅ 标准库（用于 LookupEnv）
  import "sort"              ✅ 标准库
  import "strings"           ✅ 标准库
  import "time"              ✅ 标准库
  import "internal/types/"   ✅ 合规（SyscallEvent）
```

无外部依赖，无禁止导入。

### Project Structure Notes

**新建文件：**
```
debug/astrace.go          — Attach 消费循环 + FormatEvent 格式化器
debug/astrace_test.go     — 14 个测试用例
```

**修改文件：**
```
_bmad-output/implementation-artifacts/sprint-status.yaml  — 状态更新 backlog→ready-for-dev
```

**不修改文件：**
```
debug/event.go            — Story 3.1 实现，不修改
kernel/kernel.go          — Story 3.1 已完成事件埋点，不修改
```

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story 3.2] — Story 定义和验收标准
- [Source: _bmad-output/planning-artifacts/architecture.md#Decision 5: 调试架构] — DebugChan 机制和 astrace 数据流
- [Source: _bmad-output/planning-artifacts/architecture.md#通信模式] — astrace 输出格式示例和 Channel 使用规则
- [Source: _bmad-output/planning-artifacts/architecture.md#依赖方向] — debug/ 包依赖约束
- [Source: _bmad-output/project-context.md#SyscallEvent 记录] — SyscallEvent 命名规则
- [Source: _bmad-output/project-context.md#关键防错规则] — 禁止反向依赖
- [Source: internal/types/types.go:70-79] — SyscallEvent 类型定义
- [Source: debug/event.go] — EmitEvent/NewEvent/CompleteEvent 已有基础
- [Source: debug/event_test.go] — 测试命名和断言风格参考
- [Source: kernel/kernel.go:193-233] — emitEvent 包装方法（了解事件如何被发出）
- [Source: internal/ui/renderer.go:48-51] — NO_COLOR 检测参考（使用 os.LookupEnv，本 Story 在 debug/ 中独立实现）
- [Source: _bmad-output/planning-artifacts/architecture.md#命名模式] — Go 命名规范

## Dev Agent Record

### Agent Model Used

Claude Sonnet 4.6 (claude-sonnet-4-6)

### Debug Log References

- 初始实现中 TestFormatArgs_Truncation 和 TestFormatArgs_Verbose_NoTruncation 失败：测试用 string 类型值但期望无引号输出，实际 formatArgs 对 string 用 %q 格式化。引入 rawValue 类型测试 %v 路径，同时增加 string 类型截断验证。
- TestAttach_Latency 竞态条件：bytes.Buffer 非线程安全，Attach goroutine 写入同时主 goroutine 轮询 buf.Len()。替换为 channel 信号通知的 writerFunc 实现，消除竞态。

### Completion Notes List

- ✅ Task 1: 创建 `debug/astrace.go` — 实现了 Options、DefaultOptions、Attach、FormatEvent 及 5 个内部辅助函数（formatTimestamp、formatArgs、formatResult、formatDuration、isLLMSyscall），严格遵循 Dev Notes 中的 API 设计和格式规范
- ✅ Task 2: 创建 `debug/astrace_test.go` — 14 个测试用例全部通过，覆盖基础格式、慢操作标注、错误高亮（颜色/无颜色）、LLM 标注、组合场景、参数截断、排序、Attach 消费/取消/关闭/延迟、NO_COLOR 检测
- ✅ Task 3: sprint-status.yaml 状态更新 ready-for-dev → in-progress → review
- ✅ 全项目 `go test -race ./...` 通过（13 个包），`go vet ./...` 无警告
- ✅ 依赖约束合规：debug/ 仅导入 standard library + internal/types/，无禁止导入
- ✅ NFR3 延迟合规：TestAttach_Latency 测量延迟 ~92µs，远低于 500ms 阈值

### File List

- `debug/astrace.go` — 新增：Attach 消费循环 + FormatEvent 格式化器 + Options + 辅助函数
- `debug/astrace_test.go` — 新增：14 个测试用例
- `_bmad-output/implementation-artifacts/sprint-status.yaml` — 修改：3-2 状态 ready-for-dev → review
- `_bmad-output/implementation-artifacts/3-2-astrace-event-consumption-and-formatting.md` — 修改：任务标记完成、Dev Agent Record、File List、Status
