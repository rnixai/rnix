# Story 3.4: Syscall Trace Line UI 组件

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a 用户,
I want strace 输出清晰可读，关键信息一眼可见,
So that 我不需要在密集输出中翻找问题。

## Acceptance Criteria

1. **TraceLine 组件实现** — Given `internal/ui/trace.go` 已实现，When 渲染 Trace Line，Then 时间戳暗灰色（MutedStyle），syscall 名称 Rnix Blue 加粗（AgentStyle + Bold），参数普通文本，返回值 `→` 后跟结果

2. **错误 syscall 高亮** — Given 错误 syscall，When 渲染，Then 整行红色高亮（ErrorStyle），在密集输出中视觉上"跳出来"

3. **LLM 调用标注** — Given LLM 相关 syscall（Open/Write/Read `/dev/llm/*`），When 渲染，Then 行尾标注 `← LLM 调用`（MutedStyle）

4. **NO_COLOR 降级** — Given NO_COLOR 环境变量设置，When 渲染 Trace Line，Then 颜色降级为纯文本，错误行前缀 `[ERR]`

5. **慢操作标注** — Given 某个 syscall 耗时 > 1 秒，When 渲染，Then 行尾添加 `← 慢操作` 标注（MutedStyle），与错误行互斥（错误行不添加灰色标注避免颜色冲突）

6. **Attach 集成** — Given `debug.Attach` 函数，When 使用 UI 模式（非 JSON），Then 使用 `ui.FormatTraceLine` 替代 `debug.FormatEvent` 的 raw ANSI 输出

7. **所有测试通过** — Given 实现完成，When 执行 `go test -race ./...`，Then 所有新增和现有测试通过，无竞态条件；`go vet ./...` 无警告

## Tasks / Subtasks

- [x] Task 1: 创建 `internal/ui/trace.go` — Syscall Trace Line 组件 (AC: #1-#5)
  - [x] 1.1 实现 `FormatTraceLine(r *Renderer, event types.SyscallEvent, verbose bool) string` — 使用 lipgloss 样式的事件格式化函数
  - [x] 1.2 时间戳使用 `MutedStyle` 渲染 `[N.NNNs]` 固定宽度格式
  - [x] 1.3 syscall 名称使用 `AgentStyle` + `BoldStyle` 渲染（Rnix Blue 加粗）
  - [x] 1.4 参数普通文本，支持 verbose 模式（截断 50 字符 vs 完整展示）
  - [x] 1.5 返回值用 `→` 分隔，错误返回值用 `ErrorStyle` 渲染
  - [x] 1.6 慢操作标注（> 1s）用 `MutedStyle` 渲染 `← 慢操作`，error 行跳过灰色标注
  - [x] 1.7 LLM 调用标注 — 检测 args 中 path/tool 包含 `/dev/llm/`，用 `MutedStyle` 渲染 `← LLM 调用`
  - [x] 1.8 错误行 — 整行用 `ErrorStyle` 渲染，NO_COLOR 时前缀 `[ERR]`

- [x] Task 2: 创建 `internal/ui/trace_test.go` — 单元测试 (AC: #1-#5, #7)
  - [x] 2.1 `TestFormatTraceLine_BasicFormat` — 验证时间戳、syscall 名称、参数、返回值、耗时均包含在输出中
  - [x] 2.2 `TestFormatTraceLine_ErrorHighlight` — 验证错误行在 ColorLevel > 0 时使用 ErrorStyle；ColorLevel = 0 时前缀 `[ERR]`
  - [x] 2.3 `TestFormatTraceLine_SlowOperation` — 验证 > 1s 包含 `← 慢操作`；error + slow 不包含灰色标注
  - [x] 2.4 `TestFormatTraceLine_LLMAnnotation` — 验证 LLM 路径检测和标注
  - [x] 2.5 `TestFormatTraceLine_NoColor` — 验证 ColorLevel=0 时无 ANSI 转义码
  - [x] 2.6 `TestFormatTraceLine_Verbose` — 验证 verbose=true 时参数不截断

- [x] Task 3: 修改 `debug/strace.go` — 支持自定义 Formatter (AC: #6)
  - [x] 3.1 在 `Options` 中添加 `Formatter func(types.SyscallEvent) string` 字段
  - [x] 3.2 更新 `Attach` 函数 — 当 `opts.Formatter != nil` 且非 JSON 模式时，使用自定义 Formatter 替代 `FormatEvent`
  - [x] 3.3 保持 `FormatEvent` 不变 — 作为无 UI 依赖的 fallback

- [x] Task 4: 修改 `cmd/rnix/main.go` — 集成 UI TraceLine (AC: #6)
  - [x] 4.1 在 `runAstrace` 中创建 `Renderer` 并初始化 `InitStyles`
  - [x] 4.2 当非 JSON 模式时，设置 `opts.Formatter` 为 `ui.FormatTraceLine` 的闭包
  - [x] 4.3 确保 JSON 模式不受影响（JSON 仍使用 `FormatEventJSON`）

- [x] Task 5: 更新集成测试 (AC: #6, #7)
  - [x] 5.1 在 `cmd/rnix/integration_test.go` 中验证 strace 输出使用了 lipgloss 样式（或在 no-color 模式下降级正确）

- [x] Task 6: 更新 sprint-status.yaml (AC: #7)
  - [x] 6.1 将 `3-4-syscall-trace-line-ui-component` 状态更新为 `ready-for-dev`

## Dev Notes

### 核心设计决策

#### 依赖方向与职责划分

```
internal/ui/trace.go (TraceLine 组件)
  ├── 导入 internal/types/   — SyscallEvent
  ├── 导入 lipgloss          — 样式渲染
  └── 使用 ui.styles.go      — 全局样式（AgentStyle, ErrorStyle, MutedStyle, BoldStyle）

debug/strace.go (Attach 函数)
  ├── 不导入 internal/ui/    — 依赖方向禁止（debug/ 仅依赖 internal/types/）
  ├── Options.Formatter       — 函数类型字段，由 cmd/ 注入 UI 格式化器
  └── FormatEvent 保留        — raw ANSI fallback（无 UI 依赖场景）

cmd/rnix/main.go (胶水层)
  ├── 导入 internal/ui/      — FormatTraceLine
  ├── 导入 debug/            — Attach, Options
  └── 在 runAstrace 中注入    — opts.Formatter = ui.FormatTraceLine 闭包
```

**关键约束：** `debug/` 包不可导入 `internal/ui/`。通过 `Options.Formatter` 函数字段实现依赖反转，由 `cmd/rnix/main.go` 在运行时注入。

#### FormatTraceLine 函数签名

```go
// FormatTraceLine formats a SyscallEvent into a styled trace line using lipgloss.
// This is the UI-layer replacement for debug.FormatEvent's raw ANSI output.
func FormatTraceLine(r *Renderer, event types.SyscallEvent, verbose bool) string
```

**使用方式（在 cmd/rnix/main.go 中）：**

```go
// 创建 Renderer
renderer := ui.NewRenderer(w, mode)
ui.InitStyles(renderer.Profile)

// 注入 UI formatter
opts := debug.DefaultOptions()
opts.Verbose = flagVerbose
opts.JSON = flagJSON
if !flagJSON {
    opts.Formatter = func(event types.SyscallEvent) string {
        return ui.FormatTraceLine(renderer, event, flagVerbose)
    }
}
```

#### Attach 更新（最小化修改）

```go
// Options — 新增 Formatter 字段
type Options struct {
    ColorEnabled bool
    Verbose      bool
    JSON         bool
    Formatter    func(types.SyscallEvent) string // 自定义格式化器，覆盖 FormatEvent
}

// Attach — 优先级：JSON > Formatter > FormatEvent
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
            } else if opts.Formatter != nil {
                line = opts.Formatter(event)
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

#### TraceLine 样式映射表

| 元素 | lipgloss 样式 | NO_COLOR 降级 |
|------|---------------|--------------|
| 时间戳 `[N.NNNs]` | `MutedStyle`（#666666） | 纯文本 |
| syscall 名称 | `AgentStyle`（#5B9BD5）+ Bold | 纯文本加粗无效，保持原样 |
| 参数 | 无样式（普通文本） | 不变 |
| `→` 分隔符 | 无样式 | 不变 |
| 返回值（成功） | 无样式 | 不变 |
| 返回值（错误） | `ErrorStyle`（#FF6B6B） | 不变 |
| 耗时 | `MutedStyle` | 纯文本 |
| `← 慢操作` | `MutedStyle`（error 行跳过） | 纯文本 |
| `← LLM 调用` | `MutedStyle` | 纯文本 |
| 错误整行 | `ErrorStyle` 包裹整行 | `[ERR]` 前缀 |

#### 复用 debug/strace.go 的辅助函数

`FormatTraceLine` 需要复制或重新实现以下逻辑（不能导入 `debug/` 避免循环依赖——实际上 `internal/ui/` 也不应导入 `debug/`）：

- 时间戳格式化：`fmt.Sprintf("[%7.3fs]", ts.Seconds())`
- 参数格式化：排序 key=value，verbose 控制截断
- 结果格式化：成功 `%v`，错误 `err(%v)`
- 耗时格式化：`µs/ms/s` 自适应
- LLM 检测：args 中 path/tool 包含 `/dev/llm/`
- 慢操作检测：Duration > 1s

**注意：** 这些是纯函数逻辑，在 `ui/trace.go` 中重新实现为私有函数（`traceTimestamp`、`traceArgs`、`traceResult`、`traceDuration`、`isLLMEvent`）。与 `debug/` 的函数**功能相同但命名不同**，避免混淆。这不是代码重复——两者属于不同层：`debug/` 是无 UI 依赖的 raw 格式化，`ui/` 是 lipgloss 样式化格式化。

### 前序 Story 经验（Story 3.3）

**已完成的 API（直接使用）：**
- `debug/strace.go` — `Attach`、`FormatEvent`、`FormatEventJSON`、`Options`、`DefaultOptions`
- `debug/strace.go` — raw ANSI 常量 `ansiRed`、`ansiGray`、`ansiReset`（Story 3.4 后可考虑标记为 deprecated，但不删除）
- `cmd/rnix/main.go` — `runAstrace`、`initKernel`、`processStateName`
- `internal/ui/styles.go` — `AgentStyle`、`ErrorStyle`、`MutedStyle`、`BoldStyle`
- `internal/ui/renderer.go` — `Renderer`、`NewRenderer`、`DetectProfile`、`TerminalProfile`

**Story 3.3 Dev Notes 关键经验：**
- ANSI 颜色嵌套 Bug — error+slow+color 时灰色标注的 `ansiReset` 打断红色包裹。**本 Story 使用 lipgloss 应天然避免此问题**，因为 lipgloss 的 `Render()` 自动管理 ANSI 开关。
- 测试中用 `bytes.Buffer` + 直接断言字符串内容（不用 testify）。
- 测试命名：`TestTypeName_Behavior`（保持一致）。

**Story 3.3 已知限制（本 Story 需注意）：**
- [H1] `kernel/kernel.go:finishProcess()` 不关闭 `proc.DebugChan`。生产环境中 `Attach` 在进程完成后阻塞，需 Ctrl+C 退出。测试通过手动 `close(proc.DebugChan)` 绕过。**本 Story 测试同样需要手动 close**。
- [L1] `processStateName` 硬编码 map。不在本 Story 范围。

### 前序 Story Git 提交模式

```
82fb76f Finalize Story 3.3: Astrace CLI Command Implementation and Testing
49fed87 Implement Story 3.3: Astrace CLI Command Features and Testing
f536ccc Finalize Story 3.2: Astrace Event Consumption and Formatting...
```
- 提交消息：英文动词短语开头

### 已有代码关键 API 参考

**internal/ui/styles.go — 全局样式：**
```go
var (
    KernelStyle  lipgloss.Style  // #888888
    AgentStyle   lipgloss.Style  // #5B9BD5 (Rnix Blue)
    SuccessStyle lipgloss.Style  // #6BCB77
    ErrorStyle   lipgloss.Style  // #FF6B6B
    WarningStyle lipgloss.Style  // #FFD93D
    MutedStyle   lipgloss.Style  // #666666
    BoldStyle    lipgloss.Style  // Bold
)
func InitStyles(profile TerminalProfile) // ColorLevel 0 = 无色降级
```

**internal/ui/renderer.go — Renderer：**
```go
type Renderer struct {
    Profile    TerminalProfile
    Writer     io.Writer
    OutputMode OutputMode
}
type TerminalProfile struct {
    Width      int
    IsTTY      bool
    ColorLevel int // 0=none, 1/2/3=color
    IsUnicode  bool
}
func NewRenderer(w io.Writer, mode OutputMode) *Renderer
```

**internal/ui/error.go — 参考模式（Render 函数签名风格）：**
```go
func RenderError(r *Renderer, device string, reason string, impact string, suggestion string)
// 使用 r.Profile.ColorLevel 判断是否输出颜色
// 使用 r.Profile.IsUnicode 判断是否使用 Unicode 符号
```

**debug/strace.go — FormatEvent（raw ANSI，本 Story 的 UI 层替代目标）：**
```go
func FormatEvent(event types.SyscallEvent, opts Options) string
// 使用 ansiRed/ansiGray/ansiReset 硬编码 ANSI
// error 行跳过灰色标注避免嵌套冲突
```

**debug/strace.go — Options（将扩展 Formatter 字段）：**
```go
type Options struct {
    ColorEnabled bool
    Verbose      bool
    JSON         bool
}
```

**internal/types/types.go — SyscallEvent：**
```go
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

**cmd/rnix/main.go — runAstrace（将修改注入 Formatter）：**
```go
func runAstrace(cmd *cobra.Command, args []string) error {
    // ... PID 解析、进程查找 ...
    opts := debug.DefaultOptions()
    opts.Verbose = flagVerbose
    opts.JSON = flagJSON
    // 当前直接调用 debug.Attach(straceCtx, proc.DebugChan, w, opts)
    // 需在此处注入 opts.Formatter
}
```

### 注意事项与防错

#### lipgloss Style.Render() 颜色嵌套安全

lipgloss 的 `Render()` 会自动在输出的首尾添加正确的 ANSI 开/关码。当 ColorLevel=0 时，`Render()` 返回原文本不添加任何 ANSI。因此：
- 错误行用 `ErrorStyle.Render(entireLine)` 包裹整行是安全的
- 不需要手动管理 `ansiReset` 嵌套问题（Story 3.2 的 ANSI 嵌套 Bug 在 lipgloss 层面天然解决）

#### ColorLevel=0 时 Bold 失效

`lipgloss.NewStyle().Bold(true)` 在 ColorLevel=0 时不输出 bold ANSI 码。因此 NO_COLOR 模式下 syscall 名称不会加粗——这是预期行为，与其他组件一致。

#### 不要删除 debug/FormatEvent 中的 raw ANSI

`debug.FormatEvent` 作为无 UI 依赖的 fallback 保留。当 `Options.Formatter` 为 nil 时（如单元测试或 debug 包独立使用），仍使用 raw ANSI。本 Story **只新增，不删除**现有功能。

#### 测试中需初始化 InitStyles

所有使用 lipgloss 样式的测试必须先调用 `InitStyles(TerminalProfile{ColorLevel: 0})` 或有色版本。参考 `error_test.go` 的模式。

#### Formatter 函数是闭包

`Options.Formatter` 捕获 `*Renderer` 和 `verbose` 参数。确保闭包引用的变量在 Attach 运行期间不被修改（当前实现中不会——renderer 和 verbose 在 runAstrace 入口确定后不变）。

### NFR 合规

| NFR | 要求 | 实现保证 |
|-----|------|---------|
| NFR3 | strace 输出延迟 ≤ 500ms | `FormatTraceLine` 是纯函数，lipgloss Render 开销 < 1µs，不影响延迟 |
| NFR10 | CLI 进程不崩溃 | lipgloss 在任何 ColorLevel 下都安全返回字符串 |
| NFR18 | go vet 无警告 | 遵循 Go 惯例 |

### 范围边界

**本 Story 包含：**
- `internal/ui/trace.go` — 新建：FormatTraceLine 组件
- `internal/ui/trace_test.go` — 新建：单元测试
- `debug/strace.go` — 修改：Options 添加 Formatter 字段 + Attach 逻辑分支
- `cmd/rnix/main.go` — 修改：runAstrace 注入 UI Formatter
- `cmd/rnix/integration_test.go` — 修改：验证 UI 集成

**本 Story 不包含：**
- 删除 `debug/strace.go` 中的 raw ANSI 常量或 `FormatEvent`（保留为 fallback）
- `internal/ui/table.go` Process Table 组件（Story 4.4）
- 修改 `kernel/` 或 `debug/event.go`
- `--filter` flag（Phase 2）

### Project Structure Notes

**新建文件：**
```
internal/ui/trace.go         — FormatTraceLine + 私有辅助函数
internal/ui/trace_test.go    — 6+ 测试用例
```

**修改文件：**
```
debug/strace.go             — Options.Formatter 字段 + Attach 分支逻辑
cmd/rnix/main.go             — runAstrace 注入 UI Formatter + Renderer 初始化
cmd/rnix/integration_test.go — strace UI 集成验证
_bmad-output/implementation-artifacts/sprint-status.yaml — 状态更新
```

**不修改文件：**
```
internal/ui/styles.go        — 已有 AgentStyle/ErrorStyle/MutedStyle，不修改
internal/ui/renderer.go      — 已有 Renderer/TerminalProfile，不修改
debug/event.go               — Story 3.1 实现，不修改
kernel/kernel.go             — 不修改
kernel/process.go            — 不修改
```

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story 3.4] — Story 定义和验收标准
- [Source: _bmad-output/planning-artifacts/architecture.md#Decision 5] — DebugChan 机制和 strace 数据流
- [Source: _bmad-output/planning-artifacts/architecture.md#Project Structure] — `internal/ui/trace.go` 位置
- [Source: _bmad-output/planning-artifacts/architecture.md#依赖方向] — `debug/` 仅依赖 `internal/types/`，不导入 `internal/ui/`
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md#Syscall Trace Line] — 组件样式规范：时间戳暗灰、syscall 名 Rnix Blue 加粗、错误行红色
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md#Component Implementation Strategy] — 组件即函数 + 样式集中定义 + io.Writer 抽象
- [Source: _bmad-output/project-context.md#架构框架规则] — 依赖方向严格单向
- [Source: _bmad-output/project-context.md#测试规则] — 测试同目录、-race 默认、t.Fatal/t.Errorf
- [Source: internal/ui/styles.go] — AgentStyle/ErrorStyle/MutedStyle/BoldStyle 定义
- [Source: internal/ui/renderer.go] — Renderer/TerminalProfile/DetectProfile
- [Source: internal/ui/error.go] — RenderError 函数签名风格参考
- [Source: internal/ui/error_test.go] — 测试模式参考：InitStyles + bytes.Buffer + Renderer 构造
- [Source: debug/strace.go] — FormatEvent/Attach/Options 现有 API
- [Source: cmd/rnix/main.go:runAstrace()] — Formatter 注入点
- [Source: _bmad-output/implementation-artifacts/3-3-strace-cli-command.md] — 前序 Story 经验、已知限制、代码审查发现

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6

### Debug Log References

- lipgloss v1.1.0 在非 TTY 测试环境中 Render() 不输出 ANSI 码。ErrorHighlight 测试已调整为验证逻辑路径（[ERR] 前缀有无）而非 ANSI 字节。

### Completion Notes List

- ✅ Task 1: 创建 `internal/ui/trace.go`，实现 `FormatTraceLine` 及 5 个私有辅助函数 (`traceTimestamp`, `traceArgs`, `traceResult`, `traceDuration`, `isLLMEvent`)
- ✅ Task 2: 创建 `internal/ui/trace_test.go`，6 个单元测试全部通过
- ✅ Task 3: `debug/strace.go` Options 新增 `Formatter` 字段，Attach 增加 `JSON > Formatter > FormatEvent` 三级优先级
- ✅ Task 4: `cmd/rnix/main.go` runAstrace 注入 UI Formatter 闭包，JSON 模式不受影响
- ✅ Task 5: `cmd/rnix/integration_test.go` 新增 3 个集成测试 (UIFormatterIntegration, UIFormatterNoColor, JSONModeBypassesFormatter)
- ✅ Task 6: sprint-status.yaml 状态 ready-for-dev → in-progress → review
- ✅ `go test -race ./...` 全部通过（13 包），`go vet ./...` 无警告

### File List

**新建文件：**
- `internal/ui/trace.go` — FormatTraceLine 组件 + 私有辅助函数
- `internal/ui/trace_test.go` — 7 个单元测试

**修改文件：**
- `internal/ui/styles.go` — 新增 AgentBoldStyle 预计算样式
- `debug/strace.go` — Options.Formatter 字段 + Attach 三级分支
- `cmd/rnix/main.go` — runAstrace 注入 UI Formatter + Renderer 初始化
- `cmd/rnix/integration_test.go` — 3 个新集成测试
- `_bmad-output/implementation-artifacts/sprint-status.yaml` — 状态更新
- `_bmad-output/implementation-artifacts/3-4-syscall-trace-line-ui-component.md` — 任务标记完成 + Dev Agent Record

## Change Log

- 2026-02-25: Story 3.4 实现完成 — Syscall Trace Line UI 组件，替代 raw ANSI 输出为 lipgloss 样式化格式
- 2026-02-25: Code Review 修复 — [H1] 错误行 ANSI 嵌套冲突（ErrorStyle 内嵌 MutedStyle/AgentStyle 导致非均匀红色），重构为 plain text + ErrorStyle 包裹；[M1] 消除错误结果 ErrorStyle 双重包裹；[M2] 新增 AgentBoldStyle 预计算避免每次调用创建样式对象；[M3] 新增 SlowAndLLM 边界测试；[M4] 集成测试添加说明注释
