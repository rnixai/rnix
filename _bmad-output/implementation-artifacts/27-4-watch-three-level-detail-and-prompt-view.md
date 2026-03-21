# Story 27.4: watch 三级详细度 + prompt 查看

Status: done

## Story

As a 平台构建者,
I want 在 watch 视图中按 v/V 键展开步骤详情，按 p 键查看完整 prompt 内容,
So that 我可以从摘要逐级深入到完整的 LLM 输入，精确定位问题根因。

## Acceptance Criteria

### AC-1: watch 升级为 BubbleTea TUI

**Given** watch 视图当前使用简单逐行打印 + x/term raw mode
**When** 重构 `cmd/rnix/watch.go`
**Then** watch 使用 BubbleTea `tea.Model` 实现，支持键盘事件处理
**And** 维护 UI 状态机：Normal → Expanded → Pager
**And** BubbleTea 导入使用 `tea "charm.land/bubbletea/v2"`（遵循 top.go/dashboard.go 惯例）
**And** `runWatch` 调用 `tea.NewProgram(model)` + `p.Run()`

### AC-2: Level 1 步骤列表（保持现有行为）

**Given** watch TUI 显示 Level 1 步骤列表
**When** 实时接收 Progress 回调
**Then** 行为与 Story 27.3 一致：`[step N] action → summary  dur  icon`
**And** 支持上下键（j/k/↑/↓）在步骤间移动光标
**And** 当前选中步骤高亮显示

### AC-3: v 键展开 Level 2 详情

**Given** watch 视图显示 Level 1 步骤列表
**When** 用户按 `v` 键
**Then** 当前选中步骤展开到 Level 2：显示完整 LLM 响应（RawResponse）、工具输入/输出（ToolInput/ToolResult）、token 消耗（RequestTokens/ResponseTokens）
**And** 数据通过 `client.GetStepDetail(pid, step)` IPC 获取（复用 Story 27.2）
**And** 展开渲染耗时 ≤ 5ms/步（NFR59）
**And** v 键响应延迟 ≤ 50ms（NFR60）

### AC-4: V 键展开 Level 3 调试详情

**Given** watch 视图显示 Level 2 展开详情
**When** 用户按 `V` 键（大写）
**Then** 切换到 Level 3 调试级详情：显示消息数（MessageCount）、token 数（TokenCount）、首条用户消息预览（从 Messages 中提取第一条 role=user 的内容，截取前 200 字符）
**And** V 键响应延迟 ≤ 50ms（NFR60）

### AC-5: 错误步骤自动展开

**Given** watch 视图中某步骤出错（ProgressPayload.HasError = true）
**When** 该步骤的 OnStepComplete 事件到达
**Then** 自动展开到 Level 2（FR167），用户无需手动按 v

### AC-6: 慢步骤自动展开

**Given** watch 视图中某步骤耗时 > 1 秒（ProgressPayload.DurationMs > 1000）
**When** 该步骤的 OnStepComplete 事件到达
**Then** 自动展开到 Level 2（FR167）

### AC-7: p 键进入 prompt 翻页模式

**Given** watch 视图处于任何 Level
**When** 用户按 `p` 键
**Then** 发起 GetStepDetail IPC 请求获取完整 prompt
**And** 进入 less 式翻页模式（Pager 状态），显示 SystemPrompt + Messages + Tools 定义
**And** GetStepDetail 返回延迟 ≤ 500ms（NFR61）

### AC-8: Pager 模式交互

**Given** prompt 翻页模式（Pager 状态）
**When** 用户按 `q`/`Esc` 键
**Then** 返回 watch 实时流视图

**Given** Pager 模式
**When** 用户按 `j`/`k`/`↑`/`↓`/`PgUp`/`PgDn`/`g`/`G` 键
**Then** 翻页器上下滚动内容

### AC-9: v 键折叠

**Given** Level 2/3 展开状态
**When** 用户按 `v` 键（再次）
**Then** 折叠回 Level 1

### AC-10: q 键退出

**Given** watch TUI 运行中（Normal 或 Expanded 状态）
**When** 用户按 `q` 或 `Ctrl+C`
**Then** 退出 BubbleTea program，断开 IPC 连接
**And** 进程继续运行不受影响

### AC-11: 步骤详情缓存

**Given** 用户对步骤 N 按 v 展开后折叠
**When** 再次对同一步骤按 v
**Then** 直接使用缓存数据，不重新发起 IPC 请求
**And** 缓存存储在 TUI Model 的 `detailCache map[int]*GetStepDetailResponse` 中

## Tasks / Subtasks

- [x] Task 1: 重构 watch.go 为 BubbleTea TUI (AC: #1, #2, #10)
  - [x] 1.1 定义 `watchModel` 结构体：steps 列表、cursor、pid、client、detailCache、expandLevel、pagerContent、pagerOffset、状态枚举
  - [x] 1.2 定义状态枚举：`watchStateNormal`、`watchStateExpanded`、`watchStatePager`
  - [x] 1.3 实现 `watchModel.Init()` → 返回 `watchSubscribeCmd`（启动后台 IPC watch 流）
  - [x] 1.4 实现 IPC 事件接收：通过 tea.Cmd 将 StreamEvent 转发到 BubbleTea 消息循环
  - [x] 1.5 实现 `watchModel.Update()` 处理 tea.KeyPressMsg、watchEventMsg、tea.WindowSizeMsg
  - [x] 1.6 实现 `watchModel.View()` 渲染步骤列表（带光标高亮）
  - [x] 1.7 实现 q/Ctrl+C 退出
  - [x] 1.8 更新 `runWatch` 为 `tea.NewProgram(model).Run()`

- [x] Task 2: Level 2/3 展开 (AC: #3, #4, #9, #11)
  - [x] 2.1 实现 v 键处理：Normal 下 v → fetchDetail → Expanded (Level 2)；Expanded 下 v → Normal
  - [x] 2.2 实现 V 键处理：Expanded Level 2 → Expanded Level 3；Level 3 → Level 2
  - [x] 2.3 实现 `fetchDetailCmd(client, pid, step)` tea.Cmd → 返回 `watchDetailMsg`
  - [x] 2.4 实现 `detailCache map[int]*ipc.GetStepDetailResponse` 缓存逻辑
  - [x] 2.5 实现 Level 2 渲染：RawResponse（截取前 500 字符 + 省略号）、ToolInput、ToolResult（截取前 300 字符）、RequestTokens、ResponseTokens
  - [x] 2.6 实现 Level 3 渲染：MessageCount、TokenCount、首条 user 消息预览（200 字符）

- [x] Task 3: 错误/慢步骤自动展开 (AC: #5, #6)
  - [x] 3.1 在 watchEventMsg 处理中，step_complete 事件 HasError=true 或 DurationMs>1000 时自动设置 expandLevel=2 并触发 fetchDetailCmd

- [x] Task 4: Pager 模式 (AC: #7, #8)
  - [x] 4.1 实现 p 键处理：fetchDetail → 格式化 SystemPrompt + Messages + Tools → 进入 Pager 状态
  - [x] 4.2 实现 Pager 渲染：基于 offset + 可视行数的文本滚动
  - [x] 4.3 实现 Pager 按键：j/k/↑/↓ 单行、PgUp/PgDn 翻页、g 跳顶、G 跳底、q/Esc 退出
  - [x] 4.4 格式化 prompt 内容：`[System Prompt]` + 完整 SystemPrompt + `[Messages (N)]` + 每条消息 `[role] content` + `[Tools (N)]` + 每个 Tool `name: description`

- [x] Task 5: spawn --watch 兼容 (AC: #1)
  - [x] 5.1 确保 main.go 的 `--watch` flag 在 BubbleTea 模式下正常工作
  - [x] 5.2 评估选项：spawn --watch 可保持简单模式（逐行输出），BubbleTea 仅用于 `rnix watch <pid>`

- [x] Task 6: 测试 (AC: all)
  - [x] 6.1 单元测试：watchModel 状态转换（Normal↔Expanded↔Pager）
  - [x] 6.2 单元测试：detailCache 命中/未命中
  - [x] 6.3 单元测试：自动展开逻辑（HasError/DurationMs 阈值）
  - [x] 6.4 集成测试：watchModel View 渲染各状态的输出
  - [x] 6.5 回归确认：`make all` 全部通过

## Dev Notes

### 架构决策引用

- **Decision 26**: watch TUI — 三级详细度实时观察 [Source: architecture/core-architectural-decisions.md#decision-26]
- **Decision 25**: GetStepDetail — 按需读取步骤详情 [Source: architecture/core-architectural-decisions.md#decision-25]
- **Decision 24**: 双层架构 — Progress 回调（实时通知）+ StepRecord（磁盘 JSONL 完整数据存储）[Source: architecture/core-architectural-decisions.md#decision-24]

### 关键实现模式

#### 1. BubbleTea Model 结构设计

```go
type watchState int

const (
    watchStateNormal   watchState = iota
    watchStateExpanded
    watchStatePager
)

type watchStepInfo struct {
    step       int
    action     string
    summary    string
    durationMs float64
    hasError   bool
}

type watchModel struct {
    pid         types.PID
    client      *ipc.Client
    steps       []watchStepInfo
    cursor      int
    state       watchState
    expandLevel int // 2 or 3, only meaningful when state == watchStateExpanded

    detailCache map[int]*ipc.GetStepDetailResponse

    // Pager state
    pagerLines  []string
    pagerOffset int
    pagerTitle  string

    // Terminal
    width  int
    height int

    // Stream state
    completed   bool
    exitCode    int
    errorMsg    string
    profile     ui.TerminalProfile

    // Current thinking indicator
    thinkingStep int
    thinkingTotal int
}
```

遵循 top.go 的模式：`tea "charm.land/bubbletea/v2"`，`tea.NewProgram(model)`，`tea.NewView()` + `v.AltScreen = true`。

#### 2. IPC 事件桥接到 BubbleTea

BubbleTea 需要通过 `tea.Cmd` 接收异步事件。watch 的 IPC streaming（`client.WatchProcess`）是同步阻塞的回调式 API。桥接方案：

```go
type watchEventMsg ipc.StreamEvent
type watchDoneMsg struct {
    finalPayload *ipc.ProgressPayload
    err          error
}

func watchSubscribeCmd(client *ipc.Client, pid types.PID) tea.Cmd {
    return func() tea.Msg {
        // 这里只会返回一个 msg，但需要持续发送
        // 解决方案：使用 channel + 循环 cmd
        return nil // 见下方详细方案
    }
}
```

**推荐方案**：使用独立 goroutine + channel 桥接：

```go
func (m watchModel) Init() tea.Cmd {
    return m.startWatchStream
}

func (m watchModel) startWatchStream() tea.Msg {
    // 启动后台 goroutine 接收 IPC 事件
    // 使用 channel 传递到 tea.Msg 循环
    return watchStartedMsg{eventCh: ch}
}
```

实际上更好的方案是参考 BubbleTea v2 的 `tea.Tick` 模式——使用 **链式 Cmd**：

```go
func watchNextEventCmd(eventCh <-chan ipc.StreamEvent) tea.Cmd {
    return func() tea.Msg {
        ev, ok := <-eventCh
        if !ok {
            return watchDoneMsg{}
        }
        return watchEventMsg(ev)
    }
}
```

Init 启动 goroutine 推送事件到 channel，每收到一个事件，Update 处理后再返回 `watchNextEventCmd` 接收下一个。这是 BubbleTea 的标准异步模式。

#### 3. 渲染分层

**Normal 状态 View**：

```
  PID 42 (claude/claude-4-sonnet)

  [step 1] tool_call → /dev/fs  0.2s  ✓
▸ [step 2] tool_call → /dev/shell  1.5s  ✓
  [step 3] tool_call → /dev/fs  0.1s  ✗
  [step 4] plan → Created plan  0.8s  ✓
  [step 5/30] thinking...

  [v] Expand  [p] Prompt  [q] Quit  [↑↓] Navigate
```

**Expanded Level 2 View**（选中步骤展开）：

```
  [step 1] tool_call → /dev/fs  0.2s  ✓
▸ [step 2] tool_call → /dev/shell  1.5s  ✓
  ┊ Response: I'll run the build command to check for errors...
  ┊ Tool: /dev/shell
  ┊ Input: {"command": "make build"}
  ┊ Result: Build succeeded with 0 errors
  ┊ Tokens: req=1234 resp=567
  [step 3] tool_call → /dev/fs  0.1s  ✗

  [v] Collapse  [V] Debug  [p] Prompt  [q] Quit
```

**Expanded Level 3 View**（额外调试信息）：

```
▸ [step 2] tool_call → /dev/shell  1.5s  ✓
  ┊ Response: I'll run the build command to check for errors...
  ┊ Tool: /dev/shell
  ┊ Input: {"command": "make build"}
  ┊ Result: Build succeeded with 0 errors
  ┊ Tokens: req=1234 resp=567
  ┊ ── Debug ──
  ┊ Messages: 12 (est. 8450 tokens)
  ┊ First user: 分析 main.go 文件的结构和依...

  [v] Collapse  [V] Level 2  [p] Prompt  [q] Quit
```

**Pager View**：

```
  ── Prompt for step 2 ── (line 1/350)

  [System Prompt]
  You are a code analysis agent with the following skills...

  [Messages (12)]
  [user] 分析 main.go 文件的结构和依赖关系
  [assistant] 我来分析 main.go 的结构...
  [tool_result] package main\n\nimport...

  [Tools (5)]
  read_file: Read a file from the filesystem
  write_file: Write content to a file
  ...

  [q/Esc] Back  [↑↓/jk] Scroll  [PgUp/PgDn] Page  [g/G] Top/Bottom
```

#### 4. GetStepDetail 调用与缓存

```go
type watchDetailMsg struct {
    step   int
    detail *ipc.GetStepDetailResponse
    err    error
}

func fetchDetailCmd(client *ipc.Client, pid types.PID, step int) tea.Cmd {
    return func() tea.Msg {
        detail, err := client.GetStepDetail(pid, step)
        return watchDetailMsg{step: step, detail: detail, err: err}
    }
}
```

缓存策略：`detailCache map[int]*ipc.GetStepDetailResponse`，key 为 step 号。cache hit 时直接使用，无需发起 IPC。缓存不设上限（watch 期间步骤数通常 ≤ 100）。

#### 5. 自动展开逻辑

在 `Update` 中处理 `watchEventMsg` 时：

```go
case watchEventMsg:
    // ... 解析 ProgressPayload ...
    if pp.Event == "step_complete" {
        info := watchStepInfo{...}
        m.steps = append(m.steps, info)
        m.cursor = len(m.steps) - 1 // 自动跟随最新步骤

        if pp.HasError || pp.DurationMs > 1000 {
            m.state = watchStateExpanded
            m.expandLevel = 2
            return m, fetchDetailCmd(m.client, m.pid, info.step)
        }
    }
```

#### 6. spawn --watch 兼容策略

spawn 的 `--watch` flag 使用场景是 spawn 后立即观察，通常是一次性执行。**保持 spawn --watch 使用简单逐行输出**（当前实现），不升级为 BubbleTea。原因：

1. spawn --watch 复用 SpawnAndWatch 的回调 API，与 BubbleTea 的消息循环不兼容
2. spawn --watch 的 complete 事件后直接退出，不需要交互式导航
3. 用户想要交互式观察可使用 `rnix watch <pid>`

因此 `renderWatchEvent` 函数保留给 spawn --watch 使用，watch.go 的 BubbleTea 实现是独立的。

### 现有代码关键位置

| 文件 | 作用 | 关键信息 |
|------|------|----------|
| `cmd/rnix/watch.go` | 当前 watch 实现（简单逐行） | 需完全重写为 BubbleTea TUI |
| `cmd/rnix/top.go` | BubbleTea TUI 参考模板 | import `tea "charm.land/bubbletea/v2"`，topModel/Init/Update/View 模式 |
| `cmd/rnix/dashboard.go` | 复杂 BubbleTea TUI 参考 | lipgloss 样式、多 pane、异步事件流 |
| `cmd/rnix/main.go` | spawn --watch 渲染 | `renderWatchEvent`/`renderWatchStepComplete` 被 --watch 使用 |
| `ipc/client.go` L683-693 | `GetStepDetail(pid, step)` | 返回 `*GetStepDetailResponse` |
| `ipc/client.go` L698-740 | `WatchProcess(pid, onEvent)` | 同步阻塞式回调 API |
| `ipc/protocol.go` L310-339 | `ProgressPayload` | HasError/DurationMs 已有 |
| `ipc/protocol.go` | `GetStepDetailResponse` | SystemPrompt/Tools/Messages/RawResponse 等 |
| `ipc/protocol.go` | `MessageWire`/`ToolCallWire`/`ToolDefWire` | prompt 渲染的数据来源 |

### 现有类型参考

**GetStepDetailResponse**（`ipc/protocol.go`）：
```go
type GetStepDetailResponse struct {
    SystemPrompt   string        `json:"system_prompt"`
    Tools          []ToolDefWire `json:"tools"`
    Step           int           `json:"step"`
    Messages       []MessageWire `json:"messages"`
    MessageCount   int           `json:"message_count"`
    TokenCount     int           `json:"token_count"`
    RawResponse    string        `json:"raw_response"`
    Action         string        `json:"action"`
    Summary        string        `json:"summary"`
    ToolPath       string        `json:"tool_path,omitempty"`
    ToolInput      string        `json:"tool_input,omitempty"`
    ToolResult     string        `json:"tool_result,omitempty"`
    ToolError      string        `json:"tool_error,omitempty"`
    ToolDurationMs float64       `json:"tool_duration_ms,omitempty"`
    RequestTokens  int           `json:"request_tokens"`
    ResponseTokens int           `json:"response_tokens"`
}
```

**MessageWire**（`ipc/protocol.go`）：
```go
type MessageWire struct {
    Role       string         `json:"role"`
    Content    string         `json:"content"`
    ToolCallID string         `json:"tool_call_id,omitempty"`
    ToolCalls  []ToolCallWire `json:"tool_calls,omitempty"`
}
```

**ToolDefWire**（`ipc/protocol.go`）：
```go
type ToolDefWire struct {
    Name        string         `json:"name"`
    Description string         `json:"description,omitempty"`
    Parameters  map[string]any `json:"parameters,omitempty"`
}
```

**BubbleTea v2 API 要点**（已在 top.go/dashboard.go 中使用）：
- `tea.NewProgram(model)` + `p.Run()` 启动 TUI
- `tea.NewView(string)` 创建视图，`.AltScreen = true` 全屏
- `tea.KeyPressMsg` 处理键盘，`.String()` 返回键名
- `tea.WindowSizeMsg` 获取终端尺寸
- `tea.Quit` 退出程序
- `tea.Tick(duration, fn)` 定时器
- `tea.Cmd` 是 `func() tea.Msg`，异步命令模式

### 并发安全模型

- **watchModel 单线程**：BubbleTea 的 Update 在单一 goroutine 中运行，watchModel 字段的读写无并发问题
- **IPC client 调用**：`client.GetStepDetail` 在 tea.Cmd 的独立 goroutine 中调用；注意 watch IPC 连接是 streaming 模式（占用 client 的 scanner），**GetStepDetail 需要使用独立的 IPC 连接**
- **双 IPC 连接设计**：watch stream 占用一个 `*ipc.Client`（持续读取事件），GetStepDetail 需要另一个 `*ipc.Client`。在 watchModel 中维护两个 client：`streamClient`（watch stream）和 `queryClient`（GetStepDetail 按需查询）

```go
type watchModel struct {
    streamClient *ipc.Client // 用于 WatchProcess streaming
    queryClient  *ipc.Client // 用于 GetStepDetail 按需查询
    // ...
}
```

`runWatch` 中创建两个连接：
```go
streamClient, _ := ipc.EnsureDaemon()
queryClient, _ := ipc.Dial(ipc.SocketPath())
```

### 不可复用 WatchProcess 回调式 API 的原因

`client.WatchProcess(pid, onEvent)` 是同步阻塞的——调用后进入 scanner 循环直到流结束。这与 BubbleTea 的异步消息模式不兼容。

**解决方案**：在 Init 中启动 goroutine 调用 WatchProcess，事件通过 channel 桥接到 BubbleTea：

```go
func (m watchModel) Init() tea.Cmd {
    eventCh := make(chan ipc.StreamEvent, 64)
    go func() {
        defer close(eventCh)
        m.streamClient.WatchProcess(m.pid, func(ev ipc.StreamEvent) {
            eventCh <- ev
        })
    }()
    return waitForEvent(eventCh)
}

func waitForEvent(ch <-chan ipc.StreamEvent) tea.Cmd {
    return func() tea.Msg {
        ev, ok := <-ch
        if !ok {
            return watchDoneMsg{}
        }
        return watchEventMsg{event: ev, ch: ch}
    }
}
```

`Update` 收到 `watchEventMsg` 后，处理事件并返回 `waitForEvent(msg.ch)` 继续监听。这是 BubbleTea 标准的链式异步命令模式。

### 组合矩阵

| 现有功能 | 交互点 | 需验证 | 说明 |
|----------|--------|--------|------|
| spawn --watch | 保持独立 | 否 | spawn --watch 不使用 BubbleTea，保留 renderWatchEvent 逐行输出 |
| watch 基础 (27.3) | 完全重写 | 是 | watch.go 从简单输出升级为 BubbleTea TUI |
| GetStepDetail (27.2) | 消费者 | 是 | 通过 queryClient.GetStepDetail 获取详情 |
| top 命令 | 独立 | 否 | Story 27.5 再集成 |
| dashboard | 独立 | 否 | 不影响 |
| strace/gdb/log | 独立 | 否 | 不同的 IPC 流 |

### 依赖关系

- Story 27.1（StepRecord + StepWriter）— 已完成 ✓
- Story 27.2（GetStepDetail IPC）— 已完成 ✓，提供 `client.GetStepDetail(pid, step)` 和 Wire 类型
- Story 27.3（watch 基础框架）— 已完成 ✓，提供 watch.go 基础 + WatchProcess client + MethodWatch 协议
- Story 27.5（top↔watch 双向导航）— 后续 Story，依赖本 Story

### 不需要做的事情

- 不需要修改 IPC 协议（MethodWatch/GetStepDetail 已有）
- 不需要修改 server.go（handleWatch/handleGetStepDetail 已实现）
- 不需要修改 kernel.go
- 不需要修改 StepRecord/StepWriter
- 不需要将 spawn --watch 也升级为 BubbleTea（保持简单逐行输出）
- 不需要实现 top↔watch 双向导航（Story 27.5）
- 不需要 lipgloss 复杂样式（保持简洁，使用 ANSI escape codes 即可）
- 不需要修改 `renderWatchEvent`/`renderWatchStepComplete` 函数（供 spawn --watch 继续使用）

### 关键陷阱提醒

1. **双 IPC 连接**：必须为 watch stream 和 GetStepDetail 使用两个独立的 `*ipc.Client`。单连接无法同时做 streaming 和 request-response。
2. **channel 关闭时序**：WatchProcess 的 goroutine 结束时 close(eventCh)，watchModel 通过 `watchDoneMsg` 感知。确保两个 client 都在 program 退出后 Close。
3. **BubbleTea v2 View 返回**：使用 `tea.NewView(string)` 而非直接返回 string。设置 `v.AltScreen = true` 以使用备用屏幕。
4. **渲染性能**：Level 2 展开时 RawResponse 可能很长（几 KB），截取前 500 字符显示。Level 3 的 Messages 预览截取前 200 字符。
5. **初始 watch 连接验证**：在 Init 中如果 WatchProcess 立即返回错误（PID 不存在），需要优雅处理——显示错误信息后退出。
6. **RNIX_ASCII 兼容**：保持 `ui.DetectProfile` 检测，展开时的树线使用 `┊`（unicode）/ `|`（ASCII）。

### Git 近期提交参考

```
399ebc9 feat: Implement Story 27.3 - watch command for real-time agent monitoring
0f05c6e feat: 27-2 Implement GetStepDetail IPC method
9cf1d28 feat: Finalize Story 27.1 implementation
08675be feat: ds 27-1 Implement StepRecord type and StepWriter
```

### Project Structure Notes

- 修改文件：`cmd/rnix/watch.go`（完全重写为 BubbleTea TUI）
- 保留文件：`cmd/rnix/main.go` 中的 `renderWatchEvent`/`renderWatchStepComplete`（供 spawn --watch 使用）
- 依赖：`charm.land/bubbletea/v2`（已在 go.mod 中，v2.0.0）、`github.com/charmbracelet/lipgloss`（已有，可选用于样式）
- 无需新增 go.mod 依赖

### References

- [Architecture Decision 26: watch TUI](../planning-artifacts/architecture/core-architectural-decisions.md#decision-26)
- [Architecture Decision 25: GetStepDetail](../planning-artifacts/architecture/core-architectural-decisions.md#decision-25)
- [Architecture Decision 24: 双层架构](../planning-artifacts/architecture/core-architectural-decisions.md#decision-24)
- [Epic 27 Story 27.4](../planning-artifacts/epics/epic-27-统一观察系统-unified-observation-system.md)
- [Story 27.3 实现记录](./27-3-watch-command-level1-realtime-stream.md)
- [Story 27.2 实现记录](./27-2-getstepdetail-ipc-method.md)
- [Story 27.1 实现记录](./27-1-steprecord-type-and-disk-writer.md)

## Dev Agent Record

### Agent Model Used

claude-4.6-opus-high-thinking (Cursor)

### Debug Log References

- BubbleTea v2 `KeyPressMsg.String()` 对 Shift 修饰符返回 `"shift+V"` 格式，非简单大写字母
- `tea.KeyEscape` 的 String() 返回 `"esc"` 而非 `"escape"`
- 双 IPC 连接设计：streamClient 用于 WatchProcess 流，queryClient 用于 GetStepDetail 按需查询

### Completion Notes List

- ✅ watch.go 完全重写为 BubbleTea TUI，保留 renderWatchEvent 供 spawn --watch 使用
- ✅ 三级详细度：Normal(L1) → Expanded(L2, RawResponse/Tool/Tokens) → Expanded(L3, Debug/Messages)
- ✅ 错误步骤(HasError)和慢步骤(>1000ms)自动展开到 L2
- ✅ Pager 模式：SystemPrompt + Messages + Tools 完整 prompt 翻页查看
- ✅ detailCache 按 step 号缓存，避免重复 IPC 请求
- ✅ RNIX_ASCII 兼容：树线 ┊/| 根据 profile 切换
- ✅ 全部 55 个 ATDD 测试通过，make all 零错误
- ✅ spawn --watch 保持简单逐行输出不受影响

### Code Review Record

**Reviewer**: claude-4.6-opus-high-thinking (Cursor)
**Date**: 2026-03-21

**Findings & Fixes (YOLO)**:

1. **P0 Bug Fix**: `truncateStr` 和 `extractFirstUserMessage` 按字节截断 → 改为按 `[]rune` 截断，避免切断多字节 UTF-8 字符（中文3字节/字符）
2. **P2 Dead code 清理**: 移除 `watchStartedMsg.err` 未使用字段及 `Update` 中对应的死路径
3. **P3 Edge case**: `renderPagerView` 空 pagerLines 时显示 "empty" 而非 "line 1/0"

**Verdict**: PASS — 修复后 make all 零错误，0 lint issues

### File List

- `cmd/rnix/watch.go` — 完全重写：BubbleTea TUI + 保留 legacy renderWatchEvent
- `cmd/rnix/atdd_27_4_watch_tui_test.go` — 修复 lint 警告（modernize max, empty branch）
