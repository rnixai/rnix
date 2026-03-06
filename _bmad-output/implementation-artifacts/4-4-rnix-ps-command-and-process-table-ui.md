# Story 4.4: rnix ps 命令与 Process Table UI

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a 用户,
I want 通过 `rnix ps` 查看所有进程的状态表格,
So that 我随时了解系统中智能体的全局状态。

## Acceptance Criteria

1. **ps 子命令注册并调用 kernel 获取进程信息** — Given `cmd/rnix/main.go` 中 ps 子命令已注册，When 执行 `rnix ps`，Then 调用 `kernel.ListProcs()` 获取所有进程信息，And 通过 Process Table 组件输出对齐表格

2. **Process Table 组件实现完整的表格渲染** — Given `internal/ui/table.go` 已实现，When 渲染进程表格，Then 列包含 PID、STATE、SKILL、TOKENS、ELAPSED，And 数字右对齐，文本左对齐，And STATE 列颜色编码：running=蓝、zombie=黄、dead=灰，And 响应时间 ≤ 100ms（NFR2）

3. **无活跃进程时输出提示而非空表格** — Given 无活跃进程，When 执行 `rnix ps`，Then 输出 `No active processes.`（不显示空表格）

4. **JSON 模式输出机器可读格式** — Given 使用 `--json` flag，When 执行 `rnix ps --json`，Then 输出 JSON 对象，每个进程包含 pid、ppid、state、intent、tokens_used、elapsed_ms（snake_case）

5. **终端宽度自适应列显示** — Given 终端宽度 < 80 列，When 渲染表格，Then 按优先级保留列：PID + STATE（永远显示）→ SKILL（≥60 列）→ TOKENS + ELAPSED（≥80 列）→ INTENT（≥120 列）

6. **所有测试通过** — Given 实现完成，When 执行 `go test -race ./...`，Then 所有新增和现有测试通过，无竞态条件；`go vet ./...` 无警告

## Tasks / Subtasks

- [x] Task 1: 实现 Process Table UI 组件 (AC: #2, #5)
  - [x] 1.1 创建 `internal/ui/table.go`，定义 `RenderProcessTable(r *Renderer, procs []vfs.ProcInfo, verbose bool)` 函数
  - [x] 1.2 实现列定义（列间距 3 空格）——PID（右对齐，5字符宽）、STATE（左对齐，9字符宽，颜色编码）、SKILL（左对齐，15字符宽）、TOKENS（右对齐，8字符宽，千分位格式）、ELAPSED（右对齐，8字符宽，人类可读）、INTENT（左对齐，截断）、PPID（右对齐，5字符宽，仅 verbose 模式显示）
  - [x] 1.3 实现列头 + 分隔线 `───` 渲染
  - [x] 1.4 实现 STATE 颜色编码：StateRunning → AgentStyle（蓝）、StateZombie → WarningStyle（黄）、StateDead → MutedStyle（灰）、StateCreated → KernelStyle（灰）（注：UX spec 仅定义 running/zombie/dead 三色，StateCreated 为本 Story 补充决策）
  - [x] 1.5 实现终端宽度自适应列选择（P0: PID+STATE 永远显示；P1: SKILL ≥60列；P2: TOKENS+ELAPSED ≥80列；P3: INTENT ≥120列）
  - [x] 1.6 实现 NO_COLOR 降级（纯文本，无 ANSI 转义）
  - [x] 1.7 实现非 TTY 管道模式（自动去除颜色）
  - [x] 1.8 实现 Footer 统计行：`{N} active, {M} zombie, {K} total`
  - [x] 1.9 实现 formatDuration 辅助函数（`< 1s → "Nms"`，`< 60s → "N.Ns"`，`≥ 60s → "N.Nm"`）
  - [x] 1.10 实现 formatTokens 辅助函数（千分位逗号格式，如 `1,847`）

- [x] Task 2: 实现 rnix ps CLI 子命令 (AC: #1, #3, #4)
  - [x] 2.1 在 `cmd/rnix/main.go` 定义 `psCmd *cobra.Command`（Use: "ps"，Short: "List active processes"，Args: cobra.NoArgs，RunE: runPs）
  - [x] 2.2 在 `init()` 中 `rootCmd.AddCommand(psCmd)` 注册子命令
  - [x] 2.3 实现 `runPs(cmd, args)` 函数：调用 `initKernel()` → 防御性检查 `if kern == nil` → `kern.ListProcs()` → 按 PID 升序排序 → 按 OutputMode 渲染
  - [x] 2.4 实现 ModeDefault 渲染：创建 Renderer（参考 `runAstrace` 第 374 行的模式：`mode := resolveOutputMode()` → `renderer := ui.NewRenderer(os.Stdout, mode)` → `ui.InitStyles(renderer.Profile)`），调用 `ui.RenderProcessTable(renderer, procs, false)`
  - [x] 2.5 实现 ModeJSON 渲染：构造 `JSONResponse{OK: true, Data: ...}` 结构，每个进程包含 pid/ppid/state/intent/skills/tokens_used/elapsed_ms，输出 JSON
  - [x] 2.6 实现 ModeQuiet 渲染：每行输出一个 PID（纯数字，可供脚本消费）
  - [x] 2.7 实现 ModeVerbose 渲染：调用 `ui.RenderProcessTable(renderer, procs, true)` 显示所有列（含 PPID、完整 Intent）
  - [x] 2.8 实现空进程列表处理：输出 `No active processes.` 后返回（不渲染表格）

- [x] Task 3: 实现 rnix kill CLI 子命令 (**额外范围**——不在 epics AC 中，但 Story 4.1 已实现 kernel.Kill()，此处仅补充 CLI 入口)
  - [x] 3.1 在 `cmd/rnix/main.go` 定义 `killCmd *cobra.Command`（Use: "kill <pid>"，Short: "Terminate an agent process"，Args: cobra.ExactArgs(1)，RunE: runKill）
  - [x] 3.2 在 `init()` 中 `rootCmd.AddCommand(killCmd)` 注册子命令
  - [x] 3.3 实现 `runKill(cmd, args)` 函数：解析 PID 参数 → `initKernel()` → `kern.Kill(pid, types.SIGTERM)` → 输出结果或错误
  - [x] 3.4 PID 不存在时使用 `ui.RenderError` 输出三行错误结构（与现有 `runAstrace` 错误处理一致）：`✗ PID N: process not found` → `→ PID N: no active process` → `→ 建议: rnix ps  查看活跃进程`
  - [x] 3.5 成功时输出（使用 `KernelStyle` 样式渲染 `[kernel]` 前缀）：`[kernel] PID N: signal sent (SIGTERM)`

- [x] Task 4: 单元测试 (AC: #1, #2, #3, #4, #5, #6)
  - [x] 4.1 创建 `internal/ui/table_test.go` — 测试 RenderProcessTable 基本输出格式
  - [x] 4.2 测试表格列对齐——PID 右对齐、STATE 左对齐、TOKENS 右对齐
  - [x] 4.3 测试 STATE 颜色编码——验证不同状态使用正确的样式
  - [x] 4.4 测试空进程列表——验证输出 "No active processes."
  - [x] 4.5 测试终端宽度自适应——模拟不同宽度（40/60/80/120 列），验证列数量变化
  - [x] 4.6 测试 NO_COLOR 模式——验证无 ANSI 转义码
  - [x] 4.7 测试 JSON 输出——验证 JSON 格式正确、字段名 snake_case
  - [x] 4.8 测试 Quiet 模式——验证仅输出 PID（一行一个）
  - [x] 4.9 测试 formatDuration——边界值（0ms、999ms、1s、59.9s、60s、600s）
  - [x] 4.10 测试 formatTokens——0、999、1000、1000000
  - [x] 4.11 测试进程列表按 PID 排序
  - [x] 4.12 运行 `go test -race ./...` 和 `go vet ./...` 确认全部通过

## Dev Notes

### 核心设计决策

#### 数据来源——复用 Story 4.3 的 ListProcs()

**设计决策：** `rnix ps` 直接调用 `kernel.ListProcs()` 获取 `[]vfs.ProcInfo` 数据，不新增 `PS()` 方法到 ProcessManager 接口。

**AC 偏差说明：** Epics AC #1 原文使用 `kernel.PS(filter)`，本 Story 选择复用已有的 `ListProcs()` + CLI 层排序。这是有意的最小改动设计，不影响 AC 验收。

**ProcessManager 接口决策：** 当前 `ProcessManager` 接口注释（`kernel/kernel.go` 第 81 行）标注 "GetPID and PS deferred to Story 4.4"。**本 Story 不修改 ProcessManager 接口**——`ListProcs()` 是 `KernelImpl` 的具体方法（非接口方法），已满足 `rnix ps` 需求。将 `PS()` 正式加入接口留待未来需要过滤功能时再做。

**理由：**
- Story 4.3 已实现 `ListProcs()` 方法，返回所有进程的安全快照（`vfs.ProcInfo` 值类型）
- `ListProcs()` 在 `proc.mu.Lock()` 下读取每个进程的可变字段，已保证并发安全
- 排序和过滤逻辑放在 CLI 层（`cmd/rnix/main.go`），不污染 kernel 接口

```go
// cmd/rnix/main.go — runPs 中获取数据
procs := kern.ListProcs()  // []vfs.ProcInfo（Story 4.3 提供）
sort.Slice(procs, func(i, j int) bool {
    return procs[i].PID < procs[j].PID
})
```

#### Process Table 组件——函数式而非结构体

**设计决策：** Process Table 实现为导出函数 `RenderProcessTable(r *Renderer, procs []vfs.ProcInfo, verbose bool)` 而非独立的 Table 结构体。

**理由：**
- 遵循 UX 设计规范的 "组件即函数" 原则（参考 `RenderSummary`、`RenderResult`、`RenderError` 等现有组件的模式）
- `rnix ps` 是一次性渲染，无状态，不需要持久对象
- Phase 2 的 `rnix top` 需要交互式 TUI 时再引入 bubbletea Model 结构

```go
// internal/ui/table.go — 函数签名
func RenderProcessTable(r *Renderer, procs []vfs.ProcInfo, verbose bool)
```

#### 表格样式——手动对齐而非 Charm table 库

**设计决策：** MVP 使用 `fmt.Sprintf` + lipgloss 手动实现表格对齐，不引入 `github.com/charmbracelet/lipgloss/table` 库。

**理由：**
- UX 规范要求的自适应列选择（按终端宽度动态增减列）用手动实现更灵活
- 现有 UI 组件（trace.go、summary.go）都是手动格式化 + lipgloss 样式，保持一致
- 减少外部依赖——MVP 阶段的进程表格足够简单，不需要完整的 table 库
- Phase 2 的 `rnix top` 需要交互式表格时再引入 `table` 组件

#### JSON 输出格式

**AC 偏差说明：** Epics AC #4 说 "JSON 数组"，但架构文档要求所有 JSON 输出统一用 `JSONResponse{ok, data, error}` 包装。本 Story 遵循架构约定，使用包装对象。字段名也从 epics 的 `skill`/`tokens` 修正为 `skills`（数组）/`tokens_used`（与 ProcInfo 字段名和 snake_case 约定一致）。

遵循架构文档的 JSON 约定（`JSONResponse[T]` 包装 + snake_case）：

```json
{
    "ok": true,
    "data": {
        "processes": [
            {
                "pid": 1,
                "ppid": 0,
                "state": "running",
                "intent": "分析代码",
                "skills": ["code-analysis"],
                "tokens_used": 1847,
                "elapsed_ms": 6200
            }
        ]
    }
}
```

**JSON 进程条目结构（独立定义用于序列化）：**

```go
type jsonProcess struct {
    PID        types.PID `json:"pid"`
    PPID       types.PID `json:"ppid"`
    State      string    `json:"state"`
    Intent     string    `json:"intent"`
    Skills     []string  `json:"skills"`
    TokensUsed int       `json:"tokens_used"`
    ElapsedMs  int64     `json:"elapsed_ms"`
}
```

#### rnix kill 子命令——额外范围的最小实现

**设计决策：** 在同一个 Story 中实现 `rnix kill <pid>` 子命令（最小版本）。**这是超出 epics AC 的额外范围——不影响 AC 验收。** 如果时间不足可跳过 Task 3，仅实现 Task 1-2-4 即可满足所有 AC。

**理由：**
- Story 4.1 已实现 `kernel.Kill(pid, signal)` 方法，只缺 CLI 子命令入口
- `rnix ps` 输出通常配合 `rnix kill` 使用（UX 规范中 Journey 2 的交互流程）
- 实现量极小（~30 行代码），不值得独立 Story

#### rnix ps 命令帮助文本

cobra 的 `psCmd` 应配置以下帮助内容以匹配 UX 规范的 Help & Discovery 模式：

```go
var psCmd = &cobra.Command{
    Use:   "ps",
    Short: "List active processes",
    Long:  "Display a table of all agent processes with their status, skills, tokens, and elapsed time.",
    Example: `  rnix ps              # Show process table
  rnix ps --json       # JSON output for scripting
  rnix ps --quiet      # PIDs only (one per line)
  rnix ps --verbose    # Full details including PPID and intent`,
    Args: cobra.NoArgs,
    RunE: runPs,
}
```

### 前序 Story 经验（Story 4.3）

**直接适用的经验：**

1. **`ListProcs()` 数据源** — 已实现并通过测试，返回 `[]vfs.ProcInfo` 安全快照。`rnix ps` 直接消费此数据，不需要额外查询
2. **`GetProcInfo(pid)` 方法** — 可用于 `rnix kill` 验证 PID 是否存在
3. **ProcessState.String()** — 已在 `internal/types/types.go` 中实现，映射 `StateRunning → "running"` 等
4. **reapOnce/shutdownOnce 模式** — 测试中必须 `defer k.Shutdown()`，新测试遵循
5. **PID 0 作为虚拟 init** — PID 0 不在进程表中，`ListProcs()` 不返回 PID 0
6. **深拷贝 slice 字段** — `ListProcs()` 已使用 `append([]string(nil), slice...)` 创建独立副本

**Code Review Fixes 经验（Story 4.3）：**
- 统一使用 `ProcessState.String()` 方法映射状态字符串（不重复定义 map）
- 使用标准 `errors.As` 而非自定义 helper
- 使用 `strings.Contains` 替代自定义搜索函数

### 已有代码关键 API 参考

> **注意：** 以下为简洁引用。实现时直接读取源文件获取最新代码。

**cmd/rnix/main.go 核心模式：**
- 子命令注册：参考 `astraceCmd`（第 126-134 行）+ `init()` 中 `rootCmd.AddCommand()`（第 166-175 行）
- 全局标志：`flagJSON`/`flagVerbose`/`flagQuiet`/`flagModel`/`flagMaxSteps`/`flagAgent`（第 33-40 行）
- `resolveOutputMode()` → 返回 `ui.OutputMode`（第 177-188 行）
- `JSONResponse{OK, Data, Error}` 结构体（第 63-68 行）
- `initKernel()` — 创建 devReg → vfs → kernel → ProcFS 注册（第 328-352 行）
- `runAstrace` — Renderer 创建模式：`mode := resolveOutputMode()` → `renderer := ui.NewRenderer(os.Stdout, mode)` → `ui.InitStyles(renderer.Profile)`（第 374 行附近）
- 全局 `exitCode int`（第 43 行），`main()` 中 `os.Exit(exitCode)`（第 433-440 行）

**kernel/kernel.go 关键方法：**
- `ListProcs() []vfs.ProcInfo` — 遍历进程表返回安全快照，在 `proc.mu.Lock` 下读取可变字段（第 692-712 行）
- `Kill(pid types.PID, sig types.Signal) error` — 返回 `*SyscallError{Code: ErrNotFound}` 如 PID 不存在（第 614-648 行）
- `GetProcess(pid) (*Process, bool)` — 从 procTable 加载（第 602-604 行）

**vfs/proc.go — ProcInfo 结构体（第 27-40 行）：** 10 个字段——PID, PPID, State, Intent, Skills, TokensUsed, CreatedAt, CtxID, Result, AllowedDevices

**internal/types/types.go：** `ProcessState.String()` 方法返回 "created"/"running"/"zombie"/"dead"（第 47-60 行）

**internal/ui/ 组件模式：**
- `Renderer{Profile TerminalProfile, Writer io.Writer, OutputMode OutputMode}`（renderer.go 第 66-71 行）
- `TerminalProfile{Width int, IsTTY bool, ColorLevel int, IsUnicode bool}`（renderer.go 第 22-28 行）
- `OutputMode`：ModeDefault/ModeQuiet/ModeVerbose/ModeJSON（renderer.go 第 12-20 行）
- 颜色：ColorKernel="#888888", ColorAgent="#5B9BD5", ColorSuccess="#6BCB77", ColorWarning="#FFD93D", ColorError="#FF6B6B", ColorMuted="#666666"（styles.go 第 8-15 行）
- 样式变量：KernelStyle/AgentStyle/AgentBoldStyle/SuccessStyle/ErrorStyle/WarningStyle/MutedStyle/BoldStyle（styles.go 第 18-27 行）
- 组件函数模式：`RenderSummary(r *Renderer, ...)` — ModeQuiet/ModeJSON 时 return，其余用 `fmt.Fprintf(r.Writer, ...)`（summary.go 第 1-31 行）
- `RenderError(r *Renderer, device, reason, impact, suggestion string)` — 三行结构：`✗ device: reason` → `→ impact` → `→ 建议: suggestion`（error.go）
- `FormatTraceLine(r *Renderer, event, verbose)` — 根据 `r.Profile.ColorLevel == 0` 降级为纯文本（trace.go 第 14-69 行）

### UX 设计规范要点

> **注意：** 下面仅列出 Task 描述中未涵盖的补充细节。列定义、自适应、输出模式已在 Task 1-2 中完整描述。

#### 表格视觉参考

```
PID   STATE     SKILL          TOKENS   ELAPSED
───   ─────     ─────          ──────   ───────
  1   running   code-analyst    1,847   6.2s
  2   zombie    —                 423   2.1s
  3   dead      pr-reviewer      3,201   12.8s
```

- 列头与数据间用 `───` 分隔线（Unicode 环境），`---` 分隔线（ASCII 环境，`r.Profile.IsUnicode == false`）
- 无 Skill 时显示 `—`（Unicode）或 `-`（ASCII）

#### Verbose 模式额外列

```
PID   PPID   STATE     SKILL          TOKENS   ELAPSED   INTENT
───   ────   ─────     ─────          ──────   ───────   ──────
  1      0   running   code-analyst    1,847   6.2s      分析代码性能瓶颈
  2      1   zombie    —                 423   2.1s      审查 PR #42
```

#### 管道模式

当 `r.Profile.IsTTY == false` 时自动去除颜色，保留文本对齐。`rnix ps | grep running` 可正常工作。

#### 错误输出模板（rnix kill 使用）

使用 `ui.RenderError(r, device, reason, impact, suggestion)` 三行结构：
```
✗ PID 5: process not found
  → PID 5: no active process with this ID
  → 建议: rnix ps  查看活跃进程
```

### 注意事项与防错

#### 进程排序

`kernel.ListProcs()` 返回的顺序不确定（基于 SyncMap.Range 遍历），`rnix ps` 必须按 PID 升序排序：

```go
procs := kern.ListProcs()
sort.Slice(procs, func(i, j int) bool {
    return procs[i].PID < procs[j].PID
})
```

#### 时间格式化

ELAPSED 列需要人类可读格式。从 `ProcInfo.CreatedAt` 计算：`elapsed := time.Since(proc.CreatedAt)`。
- 表格中：`formatDuration(elapsed)` → 人类可读
- JSON 中：`elapsed.Milliseconds()` → 毫秒整数 `elapsed_ms`

```go
func formatDuration(d time.Duration) string {
    switch {
    case d < time.Second:
        return fmt.Sprintf("%dms", d.Milliseconds())
    case d < time.Minute:
        return fmt.Sprintf("%.1fs", d.Seconds())
    default:
        return fmt.Sprintf("%.1fm", d.Minutes())
    }
}
```

#### Token 格式化

TOKENS 列使用千分位格式：`1,847` 而非 `1847`。JSON 中用原始整数 `tokens_used: 1847`。

```go
func formatTokens(n int) string {
    s := strconv.Itoa(n)
    if len(s) <= 3 {
        return s
    }
    var buf []byte
    for i, c := range s {
        if i > 0 && (len(s)-i)%3 == 0 {
            buf = append(buf, ',')
        }
        buf = append(buf, byte(c))
    }
    return string(buf)
}
```

#### Skills 列显示

- 多个 Skill 时用逗号分隔（截断到列宽）
- 无 Skill 时显示 `—`（Unicode）或 `-`（ASCII）
- 截断时末尾 `...`

#### nil Skills/AllowedDevices 处理

`ListProcs()` 已用 `append([]string(nil), ...)` 深拷贝 slice，但空 slice 仍可能为 nil。JSON 序列化时确保输出 `[]` 而非 `null`：

```go
skills := proc.Skills
if skills == nil {
    skills = []string{}
}
```

#### rnix kill 的 PID 解析

PID 参数为字符串，需要 `strconv.ParseUint`：

```go
pidNum, err := strconv.ParseUint(args[0], 10, 64)
if err != nil {
    // 输出错误格式
    return nil  // 不返回 error（cobra 会显示 usage）
}
pid := types.PID(pidNum)
```

#### initKernel 对 rnix ps 的必要性

`rnix ps` 需要 kernel 实例来获取进程列表。必须在 `runPs` 中调用 `initKernel()`，并添加防御性检查：

```go
func runPs(cmd *cobra.Command, args []string) error {
    initKernel()
    if kern == nil {
        fmt.Fprintln(os.Stderr, "✗ kernel initialization failed")
        exitCode = 1
        return nil
    }
    // ... 后续逻辑 ...
}
```

然而，由于进程在 kernel 实例创建后才存在，`rnix ps` 在没有正在运行的 `rnix "意图"` 进程时将始终返回空列表。

**当前架构限制：** 每个 `rnix` 命令创建独立的 kernel 实例（无跨终端共享状态）。这意味着 `rnix ps` 只能看到同一进程内的智能体。这是已知限制（Epic 3 回顾中记录），需要 IPC 基础设施才能解决。当前 `rnix ps` 仍然有价值：
1. 作为 Process Table UI 组件的实现载体
2. 为未来 IPC 共享状态做好 CLI 层准备
3. 可在内核单元测试中验证完整功能

#### 不要在 UI 组件中导入 kernel 包

`internal/ui/table.go` 只能依赖 `vfs.ProcInfo` 和 `internal/types/` 的类型。不能导入 `kernel/`（违反依赖方向）。

### 接口合规

**ProcessManager 接口——不修改：**

当前 `ProcessManager` 接口（`kernel/kernel.go` 第 80-86 行）仅包含 `Spawn`/`Kill`/`Wait`，注释标注 "GetPID and PS deferred to Story 4.4"。

**本 Story 的决策：不修改 ProcessManager 接口。** 理由：
1. `ListProcs()` 是 `KernelImpl` 的具体方法（非接口方法），已满足 `rnix ps` 需求
2. 将 `PS(filter)` 加入接口需要同时定义 `PSFilter` 类型，而 MVP 阶段不需要过滤功能
3. 过滤/排序逻辑放在 CLI 层更灵活
4. 接口注释中的 "deferred" 说明可保留，未来需要过滤时再正式加入

**开发者行动：** 只需更新 `kernel/kernel.go` 第 81 行的注释，将 "deferred to Story 4.4" 改为 "deferred to future story (ListProcs used instead)"。

### NFR 合规

| NFR | 要求 | 实现保证 |
|-----|------|---------|
| NFR2 | `rnix ps` 响应时间 ≤ 100ms | ListProcs() 是纯内存操作，表格渲染为字符串格式化，远 < 100ms |
| NFR9 | 进程表一致性 | ProcInfo 是值类型快照，rnix ps 不修改进程表 |
| NFR10 | CLI 不崩溃 | 所有路径返回 error，nil 检查，空列表安全处理 |

### 范围边界

**本 Story 包含：**
- `internal/ui/table.go` — Process Table 组件（RenderProcessTable 函数 + formatDuration/formatTokens 辅助函数）
- `internal/ui/table_test.go` — Process Table 单元测试
- `cmd/rnix/main.go` — 添加 psCmd + runPs + killCmd + runKill 子命令

**本 Story 不包含：**
- `rnix top` 交互式实时监控面板（Phase 2）
- `rnix ps --filter` 过滤功能（MVP 后扩展）
- 跨终端共享进程表（需要 IPC 基础设施）
- 上下文释放 ctx_free 独立测试（Story 4.5）
- `/proc` 目录列表操作

### Project Structure Notes

**新增文件：**
```
internal/ui/table.go              — Process Table 组件 + 辅助函数
internal/ui/table_test.go         — Process Table 单元测试
```

**修改文件：**
```
cmd/rnix/main.go                  — 添加 psCmd + runPs + killCmd + runKill 子命令注册
kernel/kernel.go                  — 更新 ProcessManager 接口注释（第 81 行 "deferred" 说明）
```

**不修改文件：**
```
kernel/process.go                 — Process 结构体不变
vfs/proc.go                       — ProcInfo/ProcessInfoProvider 不变
internal/types/types.go           — ProcessState.String() 不变
internal/ui/styles.go             — 已有样式足够，不新增
internal/ui/renderer.go           — Renderer/TerminalProfile/OutputMode 不变
internal/ui/error.go              — RenderError 函数不变（rnix kill 直接复用）
```

### References

**规划文档：**
- [Source: _bmad-output/planning-artifacts/epics.md#Story 4.4] — Story 定义和验收标准（第 858-889 行）
- [Source: _bmad-output/planning-artifacts/architecture.md#Decision 1] — Syscall ABI 分类接口：ProcessManager.PS() 签名
- [Source: _bmad-output/planning-artifacts/architecture.md#命名模式] — JSON snake_case、Go PascalCase 命名约定
- [Source: _bmad-output/planning-artifacts/architecture.md#格式模式] — 时间格式（终端人类可读/JSON 毫秒整数）
- [Source: _bmad-output/planning-artifacts/architecture.md#依赖方向] — internal/ui/ 仅由 cmd/ 导入
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md#Process Table] — 表格样式规范（第 1206-1226 行）
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md#Terminal Width Adaptation] — 宽度自适应列优先级（第 1613-1639 行）
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md#Empty States] — 空状态输出（第 1434 行）
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md#Help & Discovery] — 帮助文本格式（第 1465-1485 行）
- [Source: _bmad-output/planning-artifacts/prd.md#FR7] — 查看活跃进程列表（ps）
- [Source: _bmad-output/planning-artifacts/prd.md#NFR2] — rnix ps 响应时间 ≤ 100ms
- [Source: _bmad-output/project-context.md#CLI命令结构] — 子命令列表

**前序 Story：**
- [Source: _bmad-output/implementation-artifacts/4-3-proc-dynamic-filesystem.md] — ListProcs() API、ProcInfo 结构、并发安全模式、Code Review 经验

**源码行号参考：**（详见"已有代码关键 API 参考"段落）
- cmd/rnix/main.go: astraceCmd(126-134), init(166-175), resolveOutputMode(177-188), JSONResponse(63-68), initKernel(328-352), runAstrace Renderer(~374)
- kernel/kernel.go: ProcessManager(80-86), Kill(614-648), GetProcess(602-604), ListProcs(692-712)
- vfs/proc.go: ProcInfo(27-40)
- internal/types/types.go: ProcessState.String(47-60)
- internal/ui/: renderer.go(12-71), styles.go(8-27), summary.go(1-31), error.go, trace.go(14-69)

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6

### Debug Log References

无调试问题。

### Completion Notes List

- ✅ Task 1: 实现 `internal/ui/table.go` — Process Table UI 组件，包含 RenderProcessTable、renderState、formatSkills、formatDuration、formatTokens、stripAnsi 函数。支持终端宽度自适应（40/60/80/120列阈值）、Unicode/ASCII 双模式、NO_COLOR 降级、verbose 模式（含 PPID/INTENT）、Footer 统计行。
- ✅ Task 2: 实现 `cmd/rnix/main.go` 中 psCmd + runPs — 支持 ModeDefault（表格）、ModeJSON（JSONResponse 包装）、ModeQuiet（纯 PID）、ModeVerbose（全列表格）四种输出模式。空列表输出 "No active processes."。
- ✅ Task 3: 实现 `cmd/rnix/main.go` 中 killCmd + runKill — PID 解析、kernel.Kill(SIGTERM) 调用、错误处理（RenderError 三行结构）、成功输出（KernelStyle [kernel] 前缀）。
- ✅ Task 4: 全部 35+ 单元测试通过（internal/ui/table_test.go 22 个 + cmd/rnix/main_test.go 新增 9 个），`go test -race ./...` 全 13 包通过，`go vet ./...` 无警告。
- ✅ 更新 ProcessManager 接口注释（kernel/kernel.go 第 81 行）："deferred to Story 4.4" → "deferred to future story (ListProcs used instead for PS)"

### File List

**新增文件：**
- `internal/ui/table.go` — Process Table UI 组件（RenderProcessTable + 辅助函数）
- `internal/ui/table_test.go` — Process Table 单元测试（22 个测试函数）

**修改文件：**
- `cmd/rnix/main.go` — 添加 psCmd/killCmd 定义、runPs/runKill 函数、jsonProcess 结构、renderPsJSON/renderPsQuiet 辅助函数、init() 中注册 ps/kill 子命令、import sort
- `cmd/rnix/main_test.go` — 添加 9 个新测试（TestRenderPsJSON_EmptyList/WithProcs、TestRenderPsQuiet/Empty、TestJsonProcess_SnakeCase、TestHelp_ContainsPsSubcommand、TestRunKill_InvalidPID/PIDNotFound/Success），import types/vfs/rnixctx
- `kernel/kernel.go` — 更新 ProcessManager 接口注释（第 81 行）

## Change Log

- 2026-02-26: Story 4.4 实现完成 — rnix ps 命令、rnix kill 命令、Process Table UI 组件
- 2026-02-26: Code Review 修复 — 7 项问题修复（2 HIGH + 5 MEDIUM）：
  - [H1] 修复 INTENT 列宽度计算多减 colGap 的 bug（table.go:141）
  - [H2] 补充 runKill 单元测试 3 个（InvalidPID/PIDNotFound/Success）
  - [M1] 移除 runPs 中重复的空列表检查（信任 RenderProcessTable 内部逻辑）
  - [M2] JSON elapsed_ms 使用一致时间快照（now := time.Now() 循环前捕获）
  - [M3] renderPsJSON 添加 json.Marshal 错误处理
  - [M4] 强化 StateColorCoding 测试验证不同状态使用不同样式
  - [M5] runKill 使用 errors.As 区分 Kill 错误类型
