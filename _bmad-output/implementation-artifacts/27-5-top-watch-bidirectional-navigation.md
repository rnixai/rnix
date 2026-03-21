# Story 27.5: top↔watch 双向导航

Status: done

## Story

As a 平台构建者,
I want 在 `rnix top` 中选中进程按回车直接进入 watch 视图，在 watch 中按 q 返回 top,
So that 我可以在系统全局视图和单进程观察之间无缝切换。

## Acceptance Criteria

### AC-1: top Enter 键进入 watch 视图

**Given** `rnix top` 显示进程列表
**When** 用户通过上下键选中某进程并按 Enter
**Then** 无缝切换到该进程的 watch 视图
**And** 切换延迟 ≤ 100ms（NFR63）
**And** watch 立即开始接收该进程的 Progress 回调流
**And** watch 标题显示 PID 和 provider/model 信息

### AC-2: watch q 键返回 top 视图

**Given** 用户从 top 进入 watch 视图中（非 Pager 状态）
**When** 按 `q` 键
**Then** 返回 top 全局视图（而非退出程序）
**And** 切换延迟 ≤ 50ms（NFR63）
**And** top 视图恢复到之前的进程列表状态（cursor 位置保留）
**And** top 的 tick 轮询自动恢复

### AC-3: watch Ctrl+C 直接退出程序

**Given** 用户在 watch 视图中（从 top 进入）
**When** 按 `Ctrl+C`
**Then** 直接退出整个 top 程序（与独立 `rnix watch` 行为一致）

### AC-4: watch Pager 中 q 返回 watch（非 top）

**Given** 用户从 top → watch → Pager（按 p 查看 prompt）
**When** 在 Pager 中按 `q` 或 `Esc`
**Then** 返回 watch 视图（保持现有 Pager 退出行为）
**And** 再按一次 `q` 才返回 top

### AC-5: 统一 BubbleTea Program

**Given** top 和 watch 的 TUI 实现
**When** 实现视图切换
**Then** top 和 watch 共享同一个 BubbleTea program（单 `tea.NewProgram`）
**And** 通过内部模式切换实现视图转换，避免进程重建开销

### AC-6: watch 视图目标进程已结束

**Given** 用户从 top 进入 watch 后目标进程已结束
**When** watch 收到 OnComplete 事件
**Then** 显示进程完成状态（与独立 watch 行为一致）
**And** 用户按 q 返回 top 视图

### AC-7: IPC 连接生命周期管理

**Given** top 使用单个 IPC client（tick 轮询 ListProcs）
**When** 从 top 切换到 watch
**Then** 创建 streamClient（WatchProcess 流）和 queryClient（GetStepDetail 按需查询）
**When** 从 watch 返回 top
**Then** 关闭 streamClient 和 queryClient，top 的原始 client 恢复使用

### AC-8: 独立 watch 命令不受影响

**Given** 用户直接执行 `rnix watch <pid>`
**When** 按 `q` 键
**Then** 直接退出程序（保持现有行为，无 top 视图可返回）

### AC-9: top 原有 detail 面板保留或替换

**Given** top 当前 Enter 键显示 detail 面板（topDetailView）
**When** 集成 watch 导航后
**Then** Enter 键改为进入 watch 视图（替换 detail 面板功能）
**And** watch Level 2/3 提供了比 detail 面板更丰富的进程信息

### AC-10: top 视图帮助栏更新

**Given** top 底部帮助栏当前为 `[q] Quit  [K] Kill  [Enter] Details  [↑↓/jk] Navigate`
**When** 集成 watch 导航后
**Then** 帮助栏更新为 `[q] Quit  [K] Kill  [Enter] Watch  [↑↓/jk] Navigate`

## Tasks / Subtasks

- [x] Task 1: 设计统一 App Model 架构 (AC: #5)
  - [x] 1.1 在 `cmd/rnix/top.go` 中定义 `appMode` 枚举：`appModeTop`、`appModeWatch`
  - [x] 1.2 定义 `appModel` 结构体，包含 `mode appMode`、嵌入 `topModel` 和 `watchModel`（指针，watch 按需创建）
  - [x] 1.3 `appModel.Init()` 委托到 `topModel.Init()`（启动 tick 轮询）
  - [x] 1.4 `appModel.Update()` 根据 `mode` 委托到对应子 model 的 Update
  - [x] 1.5 `appModel.View()` 根据 `mode` 委托到对应子 model 的 View

- [x] Task 2: top → watch 切换 (AC: #1, #7, #9)
  - [x] 2.1 appModel 拦截 Enter 键，调用 `switchToWatch()` 替代 `detailPID` 逻辑（方案 A）
  - [x] 2.2 `switchToWatch()` 创建 streamClient + queryClient → 构建 `watchModel` → 设置 `mode = appModeWatch`
  - [x] 2.3 创建 watchModel 后立即返回 `watchModel.Init()` 启动事件流
  - [x] 2.4 top 的 cursor 位置自然保留（topModel 值不变）

- [x] Task 3: watch → top 切换 (AC: #2, #3, #4)
  - [x] 3.1 appModel 拦截 q 键（非 Pager 状态），调用 `backToTop()`（方案 A）
  - [x] 3.2 给 `watchModel` 新增 `embeddedInTop bool` 字段，标识是否从 top 嵌入调用
  - [x] 3.3 `backToTop()` 关闭 watchModel 两个 client → 设置 `mode = appModeTop` → 返回 `tickCmd()` 恢复轮询
  - [x] 3.4 Ctrl+C 在任何模式下始终返回 `tea.Quit`
  - [x] 3.5 Pager 中 q/Esc 仍返回 watch Normal（不变）

- [x] Task 4: IPC 连接生命周期 (AC: #7)
  - [x] 4.1 topModel.client 持久持有 top 轮询连接，程序退出时通过 appModel 清理
  - [x] 4.2 切换到 watch 时：`streamClient` = 新 `ipc.Dial`、`queryClient` = 新 `ipc.Dial`
  - [x] 4.3 返回 top 时：关闭 streamClient + queryClient，置 watchModel 为 nil
  - [x] 4.4 watch 的 event channel goroutine 通过 channel close 自然退出

- [x] Task 5: 独立 watch 命令兼容 (AC: #8)
  - [x] 5.1 `runWatch` 保持不变：创建独立 watchModel + `tea.NewProgram` + `p.Run()`
  - [x] 5.2 独立 watch 的 `embeddedInTop = false`，q 键仍为 `tea.Quit`

- [x] Task 6: UI 细节 (AC: #10)
  - [x] 6.1 更新 top 帮助栏：`[Enter] Details` → `[Enter] Watch`
  - [x] 6.2 删除 `topDetailView` 函数和 `detailPID` 字段
  - [x] 6.3 删除 `topModel.handleKey` 中的 Esc 返回 detail 逻辑和 detail 模式 K 键逻辑

- [x] Task 7: runTop 入口重构 (AC: #5)
  - [x] 7.1 `runTop` 创建 `appModel`（包含 topModel）而非直接创建 topModel
  - [x] 7.2 `tea.NewProgram(appModel)` 启动统一 program
  - [x] 7.3 程序退出时关闭所有 IPC 连接（topModel.client + watch clients）

- [x] Task 8: 测试 (AC: all)
  - [x] 8.1 单元测试：appModel 模式切换（Top → Watch → Top）
  - [x] 8.2 单元测试：Enter / q 消息路由（方案 A 拦截）
  - [x] 8.3 单元测试：watch 嵌入模式 q 键返回 top
  - [x] 8.4 单元测试：watch 独立模式 q 键返回 tea.Quit
  - [x] 8.5 单元测试：Ctrl+C 在任何模式下返回 tea.Quit
  - [x] 8.6 单元测试：top 帮助栏文案
  - [x] 8.7 回归确认：`make all` 全部通过

## Dev Notes

### 架构决策引用

- **Decision 26**: watch TUI — 三级详细度实时观察 [Source: architecture/core-architectural-decisions.md#decision-26]
- **Epic 27 Story 27.5**: top↔watch 双向导航需求 [Source: epics/epic-27-统一观察系统-unified-observation-system.md]
- **FR62**: 从 top 下钻到 watch 视图 [Source: prd/functional-requirements.md]
- **NFR63**: top→watch ≤ 100ms、watch→top ≤ 50ms [Source: prd/non-functional-requirements.md]

### 关键实现模式

#### 1. 统一 App Model 架构

```go
type appMode int

const (
    appModeTop   appMode = iota
    appModeWatch
)

type switchToWatchMsg struct{ pid types.PID }
type backToTopMsg struct{}

type appModel struct {
    mode      appMode
    topModel  topModel
    watch     *watchModel // nil when in top mode
    topClient *ipc.Client // long-lived, for top tick polling
}
```

`appModel` 是 BubbleTea 的顶级 Model。它持有 `topModel`（值类型嵌入）和 `watchModel`（指针，按需创建/销毁）。

**Init** 委托到 `topModel.Init()`。**Update** 先检查全局按键（Ctrl+C → `tea.Quit`），然后按 `mode` 路由到子 model。**View** 同理按 `mode` 委托。

#### 2. 消息路由策略

```go
func (m appModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    // 全局：Ctrl+C 任何模式都退出
    if key, ok := msg.(tea.KeyPressMsg); ok && key.String() == "ctrl+c" {
        return m, tea.Quit
    }

    switch m.mode {
    case appModeTop:
        updated, cmd := m.topModel.Update(msg)
        m.topModel = updated.(topModel)
        // 检查是否有 switchToWatchMsg
        // ...
        return m, cmd

    case appModeWatch:
        if m.watch == nil {
            return m, nil
        }
        updated, cmd := m.watch.Update(msg)
        w := updated.(watchModel)
        m.watch = &w
        // 检查是否有 backToTopMsg
        // ...
        return m, cmd
    }
    return m, nil
}
```

**关键问题**：BubbleTea 的 `Update` 返回 `(tea.Model, tea.Cmd)`，子 model 无法直接返回自定义 Msg 通知父级。解决方案有两种：

**方案 A（推荐）：appModel 拦截子 model 的按键**

在 `appModel.Update()` 中，对 `tea.KeyPressMsg` 进行预处理。当 `mode == appModeTop` 且 key 为 "enter" 时，直接在 appModel 层面处理 switchToWatch 逻辑。当 `mode == appModeWatch` 且 key 为 "q" 且 `watch.state == watchStateNormal` 且 `watch.embeddedInTop` 时，直接在 appModel 层面处理 backToTop 逻辑。

```go
func (m appModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
    if key, ok := msg.(tea.KeyPressMsg); ok {
        if key.String() == "ctrl+c" {
            return m, tea.Quit
        }
        if m.mode == appModeTop && key.String() == "enter" {
            return m.switchToWatch()
        }
        if m.mode == appModeWatch && m.watch != nil &&
            key.String() == "q" && m.watch.state == watchStateNormal {
            return m.backToTop()
        }
    }
    // 其余消息委托给子 model
    switch m.mode {
    case appModeTop:
        updated, cmd := m.topModel.Update(msg)
        m.topModel = updated.(topModel)
        return m, cmd
    case appModeWatch:
        updated, cmd := m.watch.Update(msg)
        w := updated.(watchModel)
        m.watch = &w
        return m, cmd
    }
    return m, nil
}
```

**方案 B：Cmd 返回自定义 Msg**

子 model 的 handleKey 返回一个 `tea.Cmd` 产生 `switchToWatchMsg` / `backToTopMsg`，appModel 在下一轮 Update 中处理。这增加一帧延迟但更解耦。

**选择方案 A**：≤ 100ms 性能要求意味着零额外帧延迟更优。appModel 直接拦截导航按键，避免消息传递延迟。

#### 3. top → watch 切换流程

```go
func (m appModel) switchToWatch() (tea.Model, tea.Cmd) {
    if m.topModel.cursor >= len(m.topModel.rows) {
        return m, nil
    }
    pid := m.topModel.rows[m.topModel.cursor].proc.PID

    streamClient, err := ipc.Dial(ipc.SocketPath())
    if err != nil {
        m.topModel.statusMsg = fmt.Sprintf("✗ watch: %v", err)
        return m, nil
    }
    queryClient, err := ipc.Dial(ipc.SocketPath())
    if err != nil {
        streamClient.Close()
        m.topModel.statusMsg = fmt.Sprintf("✗ watch: %v", err)
        return m, nil
    }

    profile := ui.DetectProfile(os.Stdout)
    wm := newWatchModel(pid, streamClient, queryClient, profile)
    wm.embeddedInTop = true
    m.watch = &wm
    m.mode = appModeWatch
    return m, wm.Init()
}
```

#### 4. watch → top 切换流程

```go
func (m appModel) backToTop() (tea.Model, tea.Cmd) {
    if m.watch != nil {
        m.watch.streamClient.Close()
        m.watch.queryClient.Close()
        m.watch = nil
    }
    m.mode = appModeTop
    return m, tickCmd() // 恢复 top 的 tick 轮询
}
```

关闭 streamClient 会导致 WatchProcess goroutine 的 scanner 读到 EOF → close(eventCh) → watchDoneMsg。但因为 mode 已切回 top，watchDoneMsg 不会被处理（被 appModel 忽略或丢弃）。

#### 5. watchModel 嵌入标识

```go
type watchModel struct {
    // ... 现有字段 ...
    embeddedInTop bool // true = 从 top 进入，q 返回 top；false = 独立运行，q 退出
}
```

独立 `rnix watch` 入口（`runWatch`）创建 watchModel 时 `embeddedInTop = false`，行为完全不变。

#### 6. top detailPID 逻辑清理

当前 `topModel` 的 `detailPID` 字段和 `topDetailView` 函数用于 Enter 后显示 detail 面板。集成 watch 后：
- **删除 `detailPID` 字段**（Enter 改为 switch to watch）
- **删除 `topDetailView` 函数**
- **删除 `handleKey` 中 Esc 返回 detail 的逻辑**
- **删除 K 键在 detail 模式下的 kill 逻辑**
- **保留 K 键在列表模式下的 kill 功能**

#### 7. WindowSizeMsg 传播

`tea.WindowSizeMsg` 需要传播到当前活跃的子 model。appModel 的 Update 在收到 WindowSizeMsg 时**同时更新两个子 model**（因为 topModel 始终存在，watchModel 可能为 nil）。

```go
case tea.WindowSizeMsg:
    m.topModel.width = msg.Width
    m.topModel.height = msg.Height
    if m.watch != nil {
        m.watch.width = msg.Width
        m.watch.height = msg.Height
    }
    return m, nil
```

### 现有代码关键位置

| 文件 | 作用 | Story 27.5 变更 |
|------|------|-----------------|
| `cmd/rnix/top.go` | topModel BubbleTea TUI | 新增 appModel 包装层；删除 detailPID/topDetailView；Enter 改为 switchToWatch |
| `cmd/rnix/watch.go` | watchModel BubbleTea TUI | 新增 `embeddedInTop` 字段；q 键条件返回 |
| `cmd/rnix/main.go` | spawn --watch 渲染 | 不修改 |
| `ipc/client.go` | WatchProcess / GetStepDetail | 不修改 |
| `ipc/server.go` | handleWatch / handleGetStepDetail | 不修改 |

### 现有类型参考

**topModel**（`cmd/rnix/top.go`）：
```go
type topModel struct {
    processes []vfs.ProcInfo
    rows      []flatRow
    cursor    int
    detailPID types.PID // 将被删除
    client    *ipc.Client
    width     int
    height    int
    startTime time.Time
    connected bool
    err       error
    statusMsg string
}
```

**watchModel**（`cmd/rnix/watch.go`）：
```go
type watchModel struct {
    pid          types.PID
    streamClient *ipc.Client
    queryClient  *ipc.Client
    steps        []watchStepInfo
    cursor       int
    state        watchState // Normal / Expanded / Pager
    expandLevel  int
    detailCache  map[int]*ipc.GetStepDetailResponse
    pagerLines   []string
    pagerOffset  int
    pagerTitle   string
    width        int
    height       int
    completed    bool
    exitCode     int
    errorMsg     string
    profile      ui.TerminalProfile
    providerModel string
    thinkingStep  int
    thinkingTotal int
}
```

**BubbleTea v2 API 要点**（已在 top.go/watch.go 中使用）：
- `tea.NewProgram(model)` + `p.Run()` 启动 TUI
- `tea.NewView(string)` 创建视图，`.AltScreen = true` 全屏
- `tea.KeyPressMsg` 处理键盘，`.String()` 返回键名
- `tea.WindowSizeMsg` 获取终端尺寸
- `tea.Quit` 退出程序
- `tea.Tick(duration, fn)` 定时器
- `tea.Cmd` 是 `func() tea.Msg`，异步命令模式
- `tea.Batch(cmds...)` 并行执行多个 Cmd

### 并发安全模型

- **appModel 单线程**：BubbleTea Update 在单一 goroutine 中运行，appModel/topModel/watchModel 的字段读写无并发问题
- **IPC 连接隔离**：top 的 `topClient` 与 watch 的 `streamClient`/`queryClient` 完全独立
- **切换时序**：backToTop 先 Close 两个 watch client（触发 goroutine 退出），再切换 mode。后续到达的 watchEventMsg/watchDoneMsg 因 mode 已为 top 而被忽略
- **tick 与 watch 互斥**：top 模式下 tickCmd 产生 tickMsg → 被 topModel 处理。watch 模式下 tickMsg 到达时被 appModel 忽略（不委托给 watchModel）。返回 top 时通过 `return m, tickCmd()` 重启轮询

### 不需要做的事情

- 不需要修改 IPC 协议（MethodWatch/GetStepDetail 已有）
- 不需要修改 server.go / kernel.go
- 不需要修改 watch.go 的 legacy renderWatchEvent（spawn --watch 专用）
- 不需要修改 main.go 的 spawn --watch 逻辑
- 不需要修改 dashboard.go
- 不需要新增 go.mod 依赖（BubbleTea v2 已有）
- 不需要重写 watchModel 的核心逻辑（三级详细度、Pager 等完全复用）

### 关键陷阱提醒

1. **BubbleTea Model 值语义**：`topModel.Update()` 返回 `tea.Model` 接口，需要类型断言回 `topModel`。`watchModel` 同理。appModel 在 Update 中必须将返回值赋回自己的字段。
2. **tickMsg 泄漏**：切换到 watch 模式前最后一个 `tickCmd()` 可能已经在 pipeline 中。appModel 收到 tickMsg 时如果 mode 为 watch，应忽略而非转发给 watchModel。
3. **watchModel 残留 Cmd**：backToTop 后，之前 watch 产生的 Cmd（如 `waitForEvent`、`fetchDetailCmd`）可能仍在执行。它们的 Msg 到达 appModel 时因 `m.watch == nil` 或 `m.mode == appModeTop` 被安全忽略。但需确保 appModel 的 Update 不 panic（检查 `m.watch != nil`）。
4. **streamClient.Close() 后 goroutine 退出**：WatchProcess 内的 scanner.Scan() 在连接关闭后返回 false → goroutine 退出 → close(eventCh) → watchDoneMsg。这是安全的关闭路径。
5. **topModel 的 client 连接断开重连**：topModel 已有断开重连逻辑（handleTick 中 client=nil 时重新 Dial）。从 watch 返回 top 时 topClient 可能仍然有效也可能已断开，topModel 的重连逻辑会自动处理。
6. **Enter 键在空进程列表**：`topModel.rows` 为空时按 Enter 不应 crash。appModel 的 switchToWatch 中需检查 `cursor < len(rows)`。

### 组合矩阵

| 现有功能 | 交互点 | 需验证 | 说明 |
|----------|--------|--------|------|
| top 进程列表 | Enter 键行为变更 | 是 | Enter 从 detail 面板改为 switch to watch |
| top detail 面板 | 删除 | 是 | detailPID 逻辑完全移除 |
| watch TUI (27.4) | q 键行为条件化 | 是 | embeddedInTop=true 时返回 top，false 时退出 |
| watch Pager | 不变 | 是 | Pager q/Esc 仍返回 watch Normal |
| watch 三级详细度 | 不变 | 否 | v/V 键行为不受影响 |
| spawn --watch | 不变 | 否 | 独立逐行输出，不涉及 top |
| dashboard | 不变 | 否 | 独立 TUI |
| K 键 kill | 保留列表模式 | 是 | 删除 detail 模式下的 K 逻辑 |

### 依赖关系

- Story 27.1（StepRecord + StepWriter）— 已完成 ✓
- Story 27.2（GetStepDetail IPC）— 已完成 ✓
- Story 27.3（watch 基础框架 + MethodWatch 协议）— 已完成 ✓
- Story 27.4（watch 三级详细度 BubbleTea TUI）— 已完成 ✓，提供完整的 watchModel

### Previous Story Intelligence

Story 27.4 代码审查记录：
1. `truncateStr` 和 `extractFirstUserMessage` 按 `[]rune` 截断（非字节），防止切断多字节 UTF-8
2. `watchStartedMsg.err` 已清理（无未使用字段）
3. `renderPagerView` 空 pagerLines 显示 "empty"
4. 双 IPC 连接设计已验证：streamClient + queryClient 分离
5. BubbleTea v2 `KeyPressMsg.String()` 对 Shift 修饰符返回 `"shift+V"` 格式
6. `tea.KeyEscape` 的 `String()` 返回 `"esc"`

### Git 近期提交参考

```
5e77a62 feat: Implement Story 27.4 - watch three-level detail and prompt view
399ebc9 feat: Implement Story 27.3 - watch command for real-time agent monitoring
0f05c6e feat: 27-2 Implement GetStepDetail IPC method
9cf1d28 feat: Finalize Story 27.1 implementation
```

### Project Structure Notes

- 修改文件：`cmd/rnix/top.go`（新增 appModel 包装层、删除 detail 面板逻辑、Enter 改为 watch 导航）
- 修改文件：`cmd/rnix/watch.go`（新增 `embeddedInTop` 字段、q 键条件返回逻辑）
- 不修改：`cmd/rnix/main.go`、`ipc/*`、`kernel/*`
- 无需新增 go.mod 依赖

### References

- [Architecture Decision 26: watch TUI](../planning-artifacts/architecture/core-architectural-decisions.md#decision-26)
- [Epic 27 Story 27.5](../planning-artifacts/epics/epic-27-统一观察系统-unified-observation-system.md)
- [Story 27.4 实现记录](./27-4-watch-three-level-detail-and-prompt-view.md)
- [Story 27.3 实现记录](./27-3-watch-command-level1-realtime-stream.md)
- [User Journey 7: top 下钻定位](../planning-artifacts/prd/user-journeys.md#旅程-7)

## Dev Agent Record

### Agent Model Used

Claude claude-4.6-opus (Cursor)

### Debug Log References

- ATDD 桩文件 `top_app.go` 删除（与 top.go 中实际实现冲突）
- `backToTop()` 增加 nil client 检查防止 panic
- `switchToWatch()` 增加 `dialFn` 可注入测试替身，解耦 IPC 依赖
- 采用方案 A（appModel 拦截按键）而非方案 B（Cmd 返回自定义 Msg），零帧延迟
- q 键拦截条件调整为 `state != watchStatePager`（涵盖 Normal + Expanded）

### Completion Notes List

- 实现统一 `appModel` 包装 `topModel`/`watchModel`，单一 `tea.NewProgram` 运行
- Enter 键从 top 进入 watch：创建两个新 IPC 连接 + watchModel，传播窗口尺寸
- q 键从 watch 返回 top：关闭 watch IPC 连接、置 nil、恢复 tick 轮询
- Ctrl+C 在任何模式/状态下直接退出
- Pager 中 q 仍返回 watch Normal（需再按一次 q 回 top）
- 删除 `topDetailView` 函数、`detailPID` 字段及相关 Esc/detail 处理逻辑
- 帮助栏更新为 `[Enter] Watch`
- 独立 `rnix watch <pid>` 不受影响（`embeddedInTop=false`，q → quit）
- 删除 ATDD RED 桩文件 `top_app.go`，更新已有测试移除 `detailPID`/`topDetailView` 引用
- 全部 ATDD 27.5 测试 + 旧测试通过，`make all`（lint+vet+test+build）0 issues

### File List

- cmd/rnix/top.go (modified: +appMode/appModel 架构, -detailPID/-topDetailView, 重构 runTop)
- cmd/rnix/watch.go (modified: +embeddedInTop 字段)
- cmd/rnix/top_app.go (deleted: ATDD RED 桩文件)
- cmd/rnix/top_test.go (modified: 移除 detailPID/topDetailView 相关测试)
- cmd/rnix/atdd_27_5_top_watch_nav_test.go (modified: 适配实际实现，修复 newAppModel/topClient/detailPID 引用)

### Change Log

- 2026-03-21: Story 27.5 实现完成 — top↔watch 双向导航（统一 appModel 架构，方案 A 按键拦截）
- 2026-03-22: Code Review PASS — 1 patch 修复（runTop 退出清理增加 nil 检查），0 intent_gap, 0 bad_spec, 5 rejected as noise。make all 全部通过
