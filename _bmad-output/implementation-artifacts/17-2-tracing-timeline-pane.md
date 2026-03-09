# Story 17.2: 追踪时间线窗格

Status: review

## Story

As a 平台构建者,
I want 在时间线窗格中以时间轴展示智能体的 syscall 事件流,
So that 我可以直观地看到智能体的执行时序和关键事件。

## Acceptance Criteria

1. **Given** 选中一个智能体节点
   **When** 时间线窗格渲染
   **Then** 水平时间轴展示该智能体的 syscall 事件流，按类别着色（LLM=蓝/Tool=绿/IPC=紫/VFS=黄/Error=红）

2. **Given** 时间线窗格
   **When** 用户操作缩放或滚动
   **Then** 时间范围平滑调整，支持按类别过滤（LLM/Tool/IPC/VFS 独立显示/隐藏）

## Tasks / Subtasks

- [x] Task 1: Timeline 数据模型和事件收集 (AC: #1)
  - [x] 1.1 在 `dashboardModel` 新增 timeline 相关字段：`timelineEvents []timelineEvent`（缓冲的事件列表）、`timelineCancelFn context.CancelFunc`（取消当前订阅）、`timelineAttachedPID types.PID`（当前订阅的 PID）
  - [x] 1.2 定义 `timelineEvent` 结构体：包装 `ipc.SyscallEventWire` + `category eventCategory`（预计算分类）
  - [x] 1.3 定义 `eventCategory` 类型和常量：`catLLM`/`catTool`/`catIPC`/`catVFS`/`catError`
  - [x] 1.4 实现 `classifySyscall(ev ipc.SyscallEventWire) eventCategory` 纯函数：
    - Error: `ev.Error != ""` → catError
    - LLM: `isLLMEvent(ev)` — 检查 args 中 path/tool 包含 "/dev/llm/"
    - IPC: syscall ∈ {Send, Recv, Pipe, Signal, SigBlock, SigUnblock, JoinGroup, LeaveGroup, GetProcGroup, SignalGroup}
    - Tool: syscall ∈ {Spawn, Kill, Wait, Reparent, SpawnThread, JoinThread, SpawnSupervisor, SpawnCoroutine, Yield, ResumeCoroutine} 或 args 中 path 包含 "/dev/shell/" 或 "/dev/fs/"
    - VFS: 其余所有（Open, Read, Write, Close, Mount, Unmount, CtxAlloc, CtxRead, CtxWrite, ReasonStep 等）
  - [x] 1.5 实现 `categoryColor(cat eventCategory) string` 返回 lipgloss 颜色值

- [x] Task 2: 事件流订阅机制 (AC: #1)
  - [x] 2.1 定义 `timelineEventMsg` 消息类型（包含 `ipc.SyscallEventWire`），用于 bubbletea 消息传递
  - [x] 2.2 实现 `subscribeTimeline(client *ipc.Client, pid types.PID) tea.Cmd`：启动 goroutine 调用 `client.AttachDebug(pid, onEvent)`，每个事件通过 `p.Send(timelineEventMsg{...})` 发送到 bubbletea 事件循环
  - [x] 2.3 在 `dashboardTick` 中检测 `selectedPID` 变化：若 PID 变化 → 取消旧订阅 → 清空 timelineEvents → 启动新订阅
  - [x] 2.4 在 `Update` 中处理 `timelineEventMsg`：分类事件 → 追加到 `timelineEvents`（上限 1000 条，FIFO 淘汰）
  - [x] 2.5 在程序退出时取消订阅（`dashboardKey` 中 'q' 处理或 model cleanup）

- [x] Task 3: 时间线渲染 (AC: #1)
  - [x] 3.1 替换 `renderDashboardPlaceholder("Timeline", ...)` 为 `m.renderTimelinePane(width, height)`
  - [x] 3.2 实现 `renderTimelinePane(width, height int) string`：
    - 窗格边框（与 tree pane 相同模式：active=ColorAgent, inactive=ColorMuted）
    - 标题行："Timeline" + 事件计数 + 当前 PID
    - 时间轴区域：水平时间轴，每个事件占一个字符宽度的色块
  - [x] 3.3 实现水平时间轴渲染：
    - 计算可见时间窗口（基于 `timelineViewStart` 和 `timelineZoomLevel`）
    - 将事件按时间戳映射到水平像素位置
    - 每行一个事件（或多行合并密集事件）
    - 使用 lipgloss 着色：LLM=ColorAgent(蓝), Tool=ColorSuccess(绿), IPC="#9B59B6"(紫), VFS=ColorWarning(黄), Error=ColorError(红)
  - [x] 3.4 实现事件详情行：选中事件时底部显示详细信息（Syscall, Args, Duration, Result）
  - [x] 3.5 空状态渲染：无 selectedPID 时显示 "Select an agent to view timeline"；有 PID 但无事件时显示 "Waiting for events..."

- [x] Task 4: 时间轴缩放和滚动 (AC: #2)
  - [x] 4.1 在 `dashboardModel` 新增字段：`timelineZoomLevel int`（0=自动适配，1-5=手动级别）、`timelineViewStart int64`（视口起始时间 ms）、`timelineEventCursor int`（选中事件索引）
  - [x] 4.2 在 `dashboardKey` 中 `paneTimeline` 分支处理按键：
    - `+`/`-`：放大/缩小（调整 zoomLevel）
    - `h`/`l` 或 ←/→：左右滚动时间轴（调整 viewStart）
    - `j`/`k` 或 ↑/↓：上下选择事件（调整 eventCursor）
  - [x] 4.3 实现 `timelineWindowMs()` 返回当前缩放级别对应的可见时间窗口毫秒数
  - [x] 4.4 缩放平滑：视口中心保持不变

- [x] Task 5: 类别过滤 (AC: #2)
  - [x] 5.1 在 `dashboardModel` 新增 `timelineFilters map[eventCategory]bool`（true=显示，初始全部 true）
  - [x] 5.2 在 timeline 焦点时处理按键：1=切换 LLM, 2=切换 Tool, 3=切换 IPC, 4=切换 VFS
  - [x] 5.3 渲染时过滤 `timelineEvents`，只显示 `timelineFilters[cat] == true` 的事件
  - [x] 5.4 在标题栏或底部显示当前过滤状态：`[L]LM [T]ool [I]PC [V]FS`（隐藏的类别灰色显示）

- [x] Task 6: 状态栏更新 (AC: #1, #2)
  - [x] 6.1 更新 `renderDashboardStatus()` — timeline 焦点时显示 timeline 专用快捷键提示
  - [x] 6.2 快捷键提示：`←/→:Scroll  +/-:Zoom  1-4:Filter  j/k:Select`

- [x] Task 7: 测试 (AC: #1, #2)
  - [x] 7.1 `dashboard_test.go`：classifySyscall — LLM 事件（path 含 /dev/llm/）→ catLLM
  - [x] 7.2 `dashboard_test.go`：classifySyscall — IPC 事件（Send/Recv/Pipe）→ catIPC
  - [x] 7.3 `dashboard_test.go`：classifySyscall — Tool 事件（Spawn/Kill）→ catTool
  - [x] 7.4 `dashboard_test.go`：classifySyscall — VFS 事件（Open/Read/CtxAlloc）→ catVFS
  - [x] 7.5 `dashboard_test.go`：classifySyscall — Error 优先（有 error 的 Open）→ catError
  - [x] 7.6 `dashboard_test.go`：categoryColor — 返回正确颜色值
  - [x] 7.7 `dashboard_test.go`：timelineEventMsg 处理 — 事件追加到 timelineEvents
  - [x] 7.8 `dashboard_test.go`：timelineEvents FIFO 淘汰 — 超过 1000 条时移除最早事件
  - [x] 7.9 `dashboard_test.go`：timeline 渲染 — 无 selectedPID 时显示提示文本
  - [x] 7.10 `dashboard_test.go`：timeline 渲染 — 有事件时不含 "Coming Soon"
  - [x] 7.11 `dashboard_test.go`：缩放级别调整 — `+` 键增加 zoomLevel，`-` 键减少
  - [x] 7.12 `dashboard_test.go`：类别过滤切换 — 按 `1` 切换 LLM 过滤
  - [x] 7.13 `dashboard_test.go`：timeline 滚动 — `h`/`l` 键调整 viewStart
  - [x] 7.14 `dashboard_test.go`：PID 变化时清空 timelineEvents
  - [x] 7.15 `dashboard_test.go`：Tab 切换到 timeline → activePane == paneTimeline

## Dev Notes

### 架构决策

本 story 在 17-1 建立的 dashboard 框架上实现时间线窗格。核心设计原则：

1. **异步事件流** — 使用 `AttachDebug` IPC 流式获取 syscall 事件，通过 bubbletea 消息机制注入 TUI 事件循环。这与 `rnix strace` 的 `AttachDebug` 模式一致，但输出渲染为时间轴而非文本行。
2. **事件缓冲而非全量存储** — 内存中最多保留 1000 条事件（FIFO），避免长时间运行的进程导致内存膨胀。
3. **预计算分类** — 事件入队时立即计算 category，渲染时直接使用，避免每帧重复分类。
4. **纯 lipgloss 渲染** — 与 17-1 一致，不引入 bubbles。时间轴用字符色块（如 `█` `▪` `·`）渲染。

### 关键设计：事件流订阅

`AttachDebug` 是阻塞式流（会一直 `scanner.Scan()` 直到 EOF），不能在 bubbletea 的 `Update` 中直接调用。必须在独立 goroutine 中运行，通过 `tea.Program.Send()` 异步发送消息。

```go
type timelineEventMsg struct {
    event ipc.SyscallEventWire
}

type timelineStreamDoneMsg struct {
    err error
}

func subscribeTimeline(p *tea.Program, client *ipc.Client, pid types.PID, ctx context.Context) tea.Cmd {
    return func() tea.Msg {
        err := client.AttachDebug(pid, func(ev ipc.SyscallEventWire) {
            select {
            case <-ctx.Done():
                return
            default:
                p.Send(timelineEventMsg{event: ev})
            }
        })
        return timelineStreamDoneMsg{err: err}
    }
}
```

**重要：** `AttachDebug` 使用共享的 `c.scanner`（单连接单流），因此**需要创建独立的 IPC 连接**用于 timeline 订阅，不能复用 dashboard 的主 client（主 client 正在 tick 中调用 ListProcs）。在 `dashboardModel` 中新增 `timelineClient *ipc.Client`。

### 关键设计：Syscall 类别分类

基于 kernel 中已有的 syscall 名称和参数：

```go
type eventCategory int

const (
    catLLM  eventCategory = iota  // LLM 相关：Open/Write/Read/Close on /dev/llm/*
    catTool eventCategory = iota  // 工具/进程：Spawn/Kill/Wait + /dev/shell/*, /dev/fs/*
    catIPC  eventCategory = iota  // IPC 通信：Send/Recv/Pipe/Signal/*
    catVFS  eventCategory = iota  // VFS/上下文：CtxAlloc/CtxRead/CtxWrite/Mount/Unmount/ReasonStep
    catError eventCategory = iota // 错误：任何 Error != "" 的事件
)

func classifySyscall(ev ipc.SyscallEventWire) eventCategory {
    if ev.Error != "" {
        return catError
    }
    if isLLMEvent(ev) {
        return catLLM
    }
    switch ev.Syscall {
    case "Send", "Recv", "Pipe", "Signal", "SigBlock", "SigUnblock",
         "JoinGroup", "LeaveGroup", "GetProcGroup", "SignalGroup":
        return catIPC
    case "Spawn", "Kill", "Wait", "Reparent", "SpawnThread", "JoinThread",
         "SpawnSupervisor", "SpawnCoroutine", "Yield", "ResumeCoroutine":
        return catTool
    }
    if isToolPathEvent(ev) {
        return catTool
    }
    return catVFS
}

func isLLMEvent(ev ipc.SyscallEventWire) bool {
    for _, key := range []string{"path", "tool"} {
        if v, ok := ev.Args[key]; ok {
            if s, ok := v.(string); ok && strings.Contains(s, "/dev/llm/") {
                return true
            }
        }
    }
    return false
}

func isToolPathEvent(ev ipc.SyscallEventWire) bool {
    if v, ok := ev.Args["path"]; ok {
        if s, ok := v.(string); ok {
            return strings.Contains(s, "/dev/shell/") || strings.Contains(s, "/dev/fs/")
        }
    }
    return false
}
```

### 关键设计：颜色映射

| 类别 | 色值 | 复用 |
|------|------|------|
| LLM (蓝) | `#5B9BD5` | ui.ColorAgent |
| Tool (绿) | `#6BCB77` | ui.ColorSuccess |
| IPC (紫) | `#9B59B6` | 新增 `ColorIPC` 常量（dashboard.go 中本地定义）|
| VFS (黄) | `#FFD93D` | ui.ColorWarning |
| Error (红) | `#FF6B6B` | ui.ColorError |

注意：IPC 紫色是新颜色，不在现有 6 色体系中。在 dashboard.go 中定义 `const colorIPC = "#9B59B6"` 即可，无需修改 `internal/ui/styles.go`。

### 关键设计：时间轴渲染

```
┌──────────────────── Timeline ─────────────────────┐
│ PID 3 | 42 events | [L]LM [T]ool [I]PC [V]FS      │
│                                                     │
│ [0.0s─────────0.5s──────────1.0s──────────1.5s]    │
│ ██▪▪··▪███▪··▪▪▪██▪···▪▪████▪··▪███                │
│ ├─ Open(/dev/llm/claude) 120ms                      │
│ ├─ Write(fd=3) 2ms                                  │
│ ├─ Read(fd=3) 3.2s                                  │
│ └─ Send(target=5) 1ms                               │
│                                                     │
│ ▸ [  1.234s] Read(fd=3, len=4096) → 2048  3.2s     │
└─────────────────────────────────────────────────────┘
```

两种渲染模式：
1. **密集视图**（默认）— 时间轴条，每个字符宽度代表一个时间分片，用色块（█/▪/·）表示事件密度
2. **列表视图** — 事件逐行列出，格式与 strace 类似但带颜色标记

当前 story 实现密集视图 + 选中事件详情。

### 关键设计：缩放级别

| Level | 时间窗口 | 字符分辨率 |
|-------|----------|-----------|
| 0 | 自动适配（全部事件范围） | 动态 |
| 1 | 1 秒 | ~20ms/char |
| 2 | 5 秒 | ~100ms/char |
| 3 | 30 秒 | ~600ms/char |
| 4 | 5 分钟 | ~6s/char |
| 5 | 30 分钟 | ~36s/char |

缩放操作以视口中心为锚点。

### 关键设计：bubbletea v2 消息流

```
           ┌─────────────────────────────────┐
           │ dashboardModel                  │
           │                                  │
tickMsg ──►│ dashboardTick()                 │
           │  ├─ ListProcs() → 更新进程树     │
           │  └─ 检测 selectedPID 变化        │
           │      ├─ 取消旧 timelineClient    │
           │      ├─ Dial() 新连接            │
           │      └─ 启动 subscribeTimeline   │
           │                                  │
timelineEventMsg ──►│                         │
           │  ├─ classifySyscall()            │
           │  └─ append timelineEvents        │
           │                                  │
KeyPressMsg ──►│                              │
           │  ├─ +/-: zoomLevel               │
           │  ├─ h/l: viewStart               │
           │  ├─ 1-4: filter toggles          │
           │  └─ j/k: eventCursor             │
           └─────────────────────────────────┘
```

### 不要做的事情

- **不要**修改 `ipc/` 包的任何文件 — 完全复用 `AttachDebug` 已有方法
- **不要**修改 `internal/ui/styles.go` — IPC 紫色在 dashboard.go 本地定义
- **不要**修改 `internal/types/` — 使用 `ipc.SyscallEventWire` 已有结构
- **不要**引入 `bubbles` 依赖 — 纯 lipgloss 渲染
- **不要**实现热力图窗格 — 17-3 实现
- **不要**实现窗格联动 — 17-4 实现（本 story 通过 selectedPID 驱动，但不实现点击联动）
- **不要**实现离线回放 — 17-5 实现
- **不要**复用 dashboard 的主 IPC 连接做 AttachDebug — 必须创建独立连接
- **不要**创建新的包或子目录 — 所有代码放在 `cmd/rnix/dashboard.go`
- **不要**使用 `.yml` 后缀
- **不要**使用全大写常量名

### 组合矩阵

| 本功能 | 交互对象 | 交互方式 | 需验证 |
|--------|----------|----------|--------|
| timeline AttachDebug | dashboard ListProcs | 独立 IPC 连接，互不干扰 | 是 |
| timeline 订阅 | selectedPID 变化 | PID 变化触发取消旧订阅+启动新订阅 | 是 |
| timeline 渲染 | activePane 焦点 | 焦点切换改变边框颜色和键盘路由 | 是 |
| timeline 过滤 | 事件分类 | 过滤器只影响渲染，不影响事件收集 | 是 |
| timeline + strace | 同一 PID | 两者同时 AttachDebug 同一 PID，需要 daemon 支持多订阅者（daemon 已支持，因为 strace 就是这样工作的） | 否 |
| timeline + rnix top | 互不干扰 | 两个独立命令，不同 IPC 连接 | 否 |
| Kill 操作 | timeline 订阅 | Kill 目标进程后，AttachDebug 流自动 EOF → timelineStreamDoneMsg | 是 |

### Project Structure Notes

修改文件：
- `cmd/rnix/dashboard.go` — 新增 timeline 数据模型、事件流订阅、时间线渲染、缩放/滚动/过滤逻辑、分类函数
- `cmd/rnix/dashboard_test.go` — 新增 15 个 timeline 相关测试

不新增文件，不修改其他包。

### References

- [Source: cmd/rnix/dashboard.go:254] — `renderDashboardPlaceholder("Timeline", ...)` 占位渲染点
- [Source: cmd/rnix/dashboard.go:36-52] — dashboardModel 结构体（扩展 timeline 字段）
- [Source: cmd/rnix/dashboard.go:80-115] — dashboardTick 方法（添加 PID 变化检测）
- [Source: cmd/rnix/dashboard.go:117-185] — dashboardKey 方法（添加 timeline 键盘处理）
- [Source: ipc/client.go:261-304] — AttachDebug 流式 API
- [Source: ipc/protocol.go:293-305] — SyscallEventWire 结构
- [Source: debug/strace.go:176-188] — isLLMSyscall 分类参考
- [Source: kernel/kernel.go:392-440] — emitEvent 和所有 syscall 名称
- [Source: kernel/ipc.go:154,180,440] — IPC syscall 名称：Send, Recv, Pipe
- [Source: kernel/signal.go:46,118,150] — Signal syscall 名称：Signal, SigBlock, SigUnblock
- [Source: kernel/procgroup.go:91,126,147,193] — 进程组 syscall：JoinGroup, LeaveGroup, GetProcGroup, SignalGroup
- [Source: kernel/thread.go:90,121] — 线程 syscall：SpawnThread, JoinThread
- [Source: kernel/supervisor.go:125] — SpawnSupervisor
- [Source: kernel/coroutine.go:89,127,186,200,219,233] — 协程 syscall：SpawnCoroutine, Yield, ResumeCoroutine
- [Source: internal/ui/styles.go:8-16] — 颜色常量
- [Source: cmd/rnix/top.go] — bubbletea v2 TUI 参考模式
- [Source: _bmad-output/planning-artifacts/epics/epic-17-可视化调试面板-visual-debugging-dashboard.md] — Epic 17 AC
- [Source: _bmad-output/implementation-artifacts/17-1-dashboard-framework-and-agent-tree-pane.md] — 17-1 前序 story
- [Source: _bmad-output/project-context.md] — 项目规则

### 技术栈

- Go 1.26 标准库
- `charm.land/bubbletea/v2 v2.0.0` — TUI 框架
- `github.com/charmbracelet/lipgloss v1.1.0` — 终端样式
- `context` — goroutine 取消
- `strings` — 路径分类
- 无新增外部依赖

### 前置 Story 学习总结

**来自 17-1 实现：**
1. bubbletea v2 API：`View()` 返回 `tea.View`（非 string），`tea.NewView()` + `v.AltScreen = true`
2. 窗格布局：lipgloss `Border(RoundedBorder())` + `BorderForeground(color)` + `Width/Height`
3. `dashboardModel` 核心字段已就位：`activePane`/`selectedPID`/`processes`/`treeRows` 等
4. `paneType` 常量：`paneTree`=0, `paneTimeline`=1, `paneHeatmap`=2
5. Tab 切换 `activePane = (activePane + 1) % 3`
6. 键盘路由通过 `if m.activePane == paneTree { ... }` 分支
7. `renderDashboardPlaceholder` 用于占位渲染，timeline 替换此调用
8. Kill 确认流程和 IPC 重连机制已实现

**来自 17-1 Code Review 修复：**
1. `colorState()` 函数添加了进程状态着色
2. `FormatSkills` 列添加到树渲染
3. `InitStyles` 在 `runDashboard` 中调用
4. 可见行数 off-by-one 修正为 `m.height - 7`

**来自 Git 分析：**
- 最新 commit：17-1 完成，traceability matrix 更新
- commit 模式：`feat: complete story X-Y - description`

## Dev Agent Record

### Agent Model Used

claude-4.6-opus-high-thinking (Cursor)

### Debug Log References

- 渲染测试初期失败：Heatmap 仍含 "Coming Soon"，测试检查整个 view content 导致误判。修正测试为检查 timeline 特定内容（"Select an agent"、"5 events"）
- `eventCategory` 使用 iota 连续赋值，测试中直接用常量名验证（不硬编码数值）
- FIFO 淘汰用切片截取 `m.timelineEvents[len-max:]`，避免逐一移除的 O(n) 开销

### Completion Notes List

- ✅ 15/15 timeline 测试全部通过
- ✅ 17-1 的 23 个测试全部通过（无回归，仅 NoDaemon 系 CI 环境无 TTY 已知失败）
- ✅ AC#1: 选中智能体节点→时间线窗格展示 syscall 事件流，LLM=蓝/Tool=绿/IPC=紫/VFS=黄/Error=红 着色
- ✅ AC#2: +/- 缩放，h/l 滚动，1-4 类别过滤（独立显示/隐藏）
- ✅ 事件分类纯函数：classifySyscall 覆盖所有内核 syscall 名称
- ✅ FIFO 缓冲：1000 条事件上限，自动淘汰最早事件
- ✅ 零新增外部依赖
- ✅ PID 变化时自动清空事件并准备新订阅
- ✅ 项目 `go build` 成功

### Code Review Fixes (Step 4)

- **[H1] IPC 流订阅已接入**：使用 idiomatic Bubbletea tea.Cmd 链式模式实现 `startTimelineCmd` → `timelineStreamStartedMsg` → `waitTimelineEventCmd` 循环。`dashboardTick` 检测 PID 变化后自动启动新 IPC 流。独立 `ipc.Dial` 连接避免阻塞主客户端。
- **[H2] timelineFilters 初始化**：`newDashboardModel` 构造器中显式调用 `defaultTimelineFilters()` 初始化。
- **[M1] CJK rune 截断**：`formatTimelineArgs` 改用 `utf8.RuneCountInString` + `[]rune` 截取，符合 project-context.md CJK 规则。
- **[M2] iota 风格修正**：`eventCategory` 常量改为 Go 惯用 iota 递增风格。

### File List

- `cmd/rnix/dashboard.go` — 修改：新增 timeline 数据模型（eventCategory/timelineEvent/timelineEventMsg/timelineStreamStartedMsg）、classifySyscall/categoryColor 分类函数、renderTimelinePane 渲染、handleTimelineKey 交互、handleTimelineEvent 事件处理、handleTimelinePIDChange PID切换、renderTimelineBar 时间轴条渲染、startTimelineCmd/waitTimelineEventCmd/stopTimelineStream IPC流管理
- `cmd/rnix/dashboard_test.go` — 修改：新增 15 个 timeline 测试（ATDD RED→GREEN）
