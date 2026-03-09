# Story 17.4: 窗格联动与进程操作

Status: review

## Story

As a 平台构建者,
I want 在 dashboard 中点击智能体节点自动联动切换其他窗格，并可直接对进程执行操作,
So that 我可以高效地在多个视图间切换并快速执行操作。

## Acceptance Criteria

1. **Given** 用户在智能体树中点击一个节点
   **When** 节点被选中
   **Then** 时间线窗格和热力图窗格自动切换到该智能体的数据

2. **Given** 用户选中一个进程
   **When** 用户按快捷键（k=kill / a=attach gdb / l=view log / r=start recording）
   **Then** 对应操作被执行，界面更新反映操作结果
   **And** 敏感操作（kill）需确认

## Tasks / Subtasks

- [x] Task 1: 即时窗格联动 (AC: #1)
  - [x] 1.1 重构 `dashboardKey` 树窗格导航（j/k/↑/↓/enter）：当 `selectedPID` 发生变化时，立即返回 `tea.Batch(startTimelineCmd(pid), fetchHeatmapCmd(pid))` 而不是等待下次 tick（消除 500ms 延迟）
  - [x] 1.2 抽取辅助方法 `(m dashboardModel) handlePIDChange() (dashboardModel, tea.Cmd)` — 封装 PID 变化时的统一逻辑：停止旧 timeline 流、清空 timeline 事件、调用 `handleTimelinePIDChange()`、调用 `handleHeatmapPIDChange()`、设置 `timelineAttachedPID` 和 `heatmapPID`、返回 `tea.Batch(startTimelineCmd, fetchHeatmapCmd)`
  - [x] 1.3 在 `dashboardTick` 中复用 `handlePIDChange()`，移除重复的 PID 变化检测逻辑（DRY）
  - [x] 1.4 确保 `handlePIDChange()` 在 `selectedPID == 0` 时只清空数据、不发起 IPC 调用

- [x] Task 2: 全局进程操作快捷键路由 (AC: #2)
  - [x] 2.1 将进程操作快捷键从仅 tree 窗格提升为全局快捷键 — 在 `dashboardKey` 中，confirmKill 和全局键检查之后、窗格特定键之前，添加全局操作分支
  - [x] 2.2 操作前提：`selectedPID > 0 && connected`，否则设置 `statusMsg` 提示 "No process selected" 或 "Not connected"
  - [x] 2.3 现有 Kill 快捷键改为小写 `k`（当前是 `Shift+K`）— 仅在 tree 窗格中 `k` 有双重含义（导航 vs kill），非 tree 窗格 `k` 直接触发 kill 确认
  - [x] 2.4 tree 窗格中使用 `Shift+K` 保留 kill 功能（因为 `k` 在 tree 中用于向上导航），同时增加独立的 `K`（大写）在其他窗格也可触发 kill

- [x] Task 3: Kill 确认流程增强 (AC: #2)
  - [x] 3.1 复用现有 `confirmKill`/`confirmPID` 字段和 `y/N` 确认逻辑
  - [x] 3.2 唯一变更：确保 kill 确认状态栏在所有窗格下都可见（已有功能，验证即可）
  - [x] 3.3 kill 成功后设置 `statusMsg = fmt.Sprintf("Killed PID %d", pid)`，2 次 tick 后自动清空

- [x] Task 4: Attach GDB 操作 (AC: #2)
  - [x] 4.1 定义 `execResultMsg struct { err error }` 消息类型
  - [x] 4.2 按键 `a` → 调用 `tea.ExecProcess(exec.Command(os.Args[0], "gdb", fmt.Sprint(selectedPID)), func(err error) tea.Msg { return execResultMsg{err} })` — 暂停 dashboard TUI，启动交互式 gdb 会话，退出后恢复 dashboard
  - [x] 4.3 在 `Update` 中处理 `execResultMsg`：err == nil → `statusMsg = "GDB session ended"`；err != nil → `statusMsg = fmt.Sprintf("GDB error: %v", err)`
  - [x] 4.4 使用 `os.Args[0]` 获取当前二进制路径，确保 gdb 子命令可被正确调用

- [x] Task 5: View Log 操作 (AC: #2)
  - [x] 5.1 按键 `l` → tree 窗格中 `l` 无冲突（tree 只用 j/k/↑/↓/enter/Shift+K），直接触发
  - [x] 5.2 调用 `tea.ExecProcess(exec.Command(os.Args[0], "log", fmt.Sprint(selectedPID)), func(err error) tea.Msg { return execResultMsg{err} })` — 暂停 dashboard，启动 log 查看器（用户 Ctrl+C 退出后恢复）
  - [x] 5.3 复用 Task 4 的 `execResultMsg` 处理逻辑

- [x] Task 6: Start/Stop Recording 操作 (AC: #2)
  - [x] 6.1 在 `dashboardModel` 新增字段：`recording map[types.PID]string`（PID → recordID 映射，追踪哪些进程在录制）
  - [x] 6.2 定义 `recordToggleMsg struct { pid types.PID; recordID string; stopped bool; eventCount uint64; err error }` 消息类型
  - [x] 6.3 实现 `toggleRecordCmd(pid types.PID, currentRecordID string) tea.Cmd`：
    - 若 `currentRecordID == ""` → `client.RecordStart(pid)` → 返回 `recordToggleMsg{pid, recordID, false, 0, err}`
    - 若 `currentRecordID != ""` → `client.RecordStop(pid)` → 返回 `recordToggleMsg{pid, "", true, eventCount, err}`
  - [x] 6.4 按键 `r` → 查找 `m.recording[selectedPID]`，调用 `toggleRecordCmd(selectedPID, recordID)`
  - [x] 6.5 在 `Update` 中处理 `recordToggleMsg`：
    - 启动成功 → `m.recording[pid] = recordID`，`statusMsg = "Recording started (ID: xxx)"`
    - 停止成功 → `delete(m.recording, pid)`，`statusMsg = fmt.Sprintf("Recording stopped (%d events)", eventCount)`
    - 错误 → `statusMsg = fmt.Sprintf("Record error: %v", err)`
  - [x] 6.6 在 `newDashboardModel` 中初始化 `recording: make(map[types.PID]string)`

- [x] Task 7: 状态栏更新 (AC: #1, #2)
  - [x] 7.1 更新 `renderDashboardStatus()` — 在全局键提示区显示操作快捷键：`k:Kill  a:GDB  l:Log  r:Record`
  - [x] 7.2 当进程正在录制时，在状态栏显示 `●REC` 红色指示符（检查 `m.recording[selectedPID]`）
  - [x] 7.3 当 `statusMsg != ""` 时优先显示 `statusMsg`（已有机制），添加 `statusMsgTTL int` 字段在 tick 中递减，归零后清空 `statusMsg`

- [x] Task 8: 智能体树窗格录制指示 (AC: #2)
  - [x] 8.1 在 `renderDashboardTreePane` 中，若 `m.recording[row.proc.PID] != ""`，在进程行尾追加红色 `●` 录制指示符

- [x] Task 9: 测试 (AC: #1, #2)
  - [x] 9.1 `dashboard_test.go`：PID 变化即时联动 — 在 tree 按 j 后，返回的 cmd 不为 nil（包含 timeline+heatmap 获取命令）
  - [x] 9.2 `dashboard_test.go`：handlePIDChange — selectedPID=0 时不发起 IPC cmd
  - [x] 9.3 `dashboard_test.go`：handlePIDChange — PID 变化时清空 timeline 和 heatmap 旧数据
  - [x] 9.4 `dashboard_test.go`：全局 kill 确认 — timeline 窗格中按 `k` 触发 confirmKill（selectedPID > 0）
  - [x] 9.5 `dashboard_test.go`：全局 kill — 无 selectedPID 时按 `k` 不触发确认
  - [x] 9.6 `dashboard_test.go`：tree 窗格 k 键仍为向上导航（不触发 kill）
  - [x] 9.7 `dashboard_test.go`：execResultMsg 处理 — err=nil 时设置成功 statusMsg
  - [x] 9.8 `dashboard_test.go`：execResultMsg 处理 — err!=nil 时设置错误 statusMsg
  - [x] 9.9 `dashboard_test.go`：recordToggleMsg 处理 — 启动录制更新 recording map
  - [x] 9.10 `dashboard_test.go`：recordToggleMsg 处理 — 停止录制清除 recording map
  - [x] 9.11 `dashboard_test.go`：recordToggleMsg 处理 — 错误时设置 statusMsg
  - [x] 9.12 `dashboard_test.go`：状态栏录制指示 — recording 不为空时显示 "●REC"
  - [x] 9.13 `dashboard_test.go`：状态栏显示操作键提示 — 包含 "k:Kill" "a:GDB" "l:Log" "r:Record"
  - [x] 9.14 `dashboard_test.go`：statusMsgTTL — tick 递减至 0 后清空 statusMsg
  - [x] 9.15 `dashboard_test.go`：树窗格录制指示 — recording 中的 PID 行包含 "●"

## Dev Notes

### 架构决策

本 story 在 17-1/17-2/17-3 已有的 dashboard 框架上实现两大功能：即时窗格联动和全局进程操作。

1. **即时联动 vs 延迟联动** — 当前实现中 `selectedPID` 变化后，timeline/heatmap 在下一次 `dashboardTick`（500ms 间隔）才检测到变化并发起数据获取。本 story 改为即时触发：`selectedPID` 变化的一刻就返回 fetch 命令，消除 500ms 感知延迟。`dashboardTick` 中的周期刷新保留不变。
2. **操作执行方式选择** — `a`（gdb）和 `l`（log）是交互式终端命令，使用 `tea.ExecProcess` 暂停 dashboard TUI → 启动子进程 → 子进程退出后恢复 dashboard。`r`（recording）是一次性 IPC 调用，不需要暂停 TUI。`k`（kill）复用现有确认流程。
3. **全局操作 vs 窗格局部操作** — 进程操作（k/a/l/r）提升为全局快捷键，在任何窗格都可执行（前提 `selectedPID > 0`），因为这些操作作用于"选中的进程"而非"当前窗格的内容"。
4. **tree 窗格 k 键冲突解决** — tree 窗格中 `k` 已用于向上导航（vim 风格），保持此行为不变。tree 窗格中 kill 操作使用 `Shift+K`（已有）。非 tree 窗格中 `k` 直接触发 kill 确认。

### 关键设计：handlePIDChange 统一方法

```go
func (m dashboardModel) handlePIDChange() (dashboardModel, tea.Cmd) {
    if m.selectedPID == 0 {
        m = m.stopTimelineStream()
        m = m.handleTimelinePIDChange()
        m = m.handleHeatmapPIDChange()
        m.timelineAttachedPID = 0
        m.heatmapPID = 0
        return m, nil
    }
    m = m.stopTimelineStream()
    m = m.handleTimelinePIDChange()
    m = m.handleHeatmapPIDChange()
    m.timelineAttachedPID = m.selectedPID
    m.heatmapPID = m.selectedPID
    var cmds []tea.Cmd
    if m.connected {
        cmds = append(cmds, startTimelineCmd(m.selectedPID))
        cmds = append(cmds, fetchHeatmapCmd(m.selectedPID))
    }
    return m, tea.Batch(cmds...)
}
```

在 `dashboardKey` 的 tree 导航分支中，当 `selectedPID` 变化时调用此方法并将返回的 cmd 与其他 cmd batch 在一起。在 `dashboardTick` 中，原有的 PID 变化检测逻辑替换为调用 `handlePIDChange()`。

### 关键设计：全局操作快捷键路由

```go
func (m dashboardModel) dashboardKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
    // 1. Kill 确认状态（已有）
    if m.confirmKill { ... }

    key := msg.String()

    // 2. 退出和窗格切换（已有）
    switch key {
    case "q", "ctrl+c": return m, tea.Quit
    case "tab": m.activePane = (m.activePane + 1) % 3; return m, nil
    }

    // 3. 全局进程操作 — 新增
    if m.selectedPID > 0 && m.connected {
        switch key {
        case "a":
            return m, tea.ExecProcess(
                exec.Command(os.Args[0], "gdb", fmt.Sprint(m.selectedPID)),
                func(err error) tea.Msg { return execResultMsg{err} },
            )
        case "l":
            return m, tea.ExecProcess(
                exec.Command(os.Args[0], "log", fmt.Sprint(m.selectedPID)),
                func(err error) tea.Msg { return execResultMsg{err} },
            )
        case "r":
            recordID := m.recording[m.selectedPID]
            return m, toggleRecordCmd(m.selectedPID, recordID)
        }
    }

    // tree 窗格中 k 用于导航，非 tree 窗格中 k 触发 kill
    if key == "k" && m.activePane != paneTree && m.selectedPID > 0 {
        m.confirmKill = true
        m.confirmPID = m.selectedPID
        return m, nil
    }

    // 4. 窗格特定键（已有）
    switch m.activePane {
    case paneTree: ... // j/k 导航, enter, Shift+K kill
    case paneTimeline: return m.handleTimelineKey(key), nil
    case paneHeatmap: return m.handleHeatmapKey(key), nil
    }
    return m, nil
}
```

注意：`a` 和 `l` 键在 timeline/heatmap 窗格中可能有冲突：
- timeline 无 `a`/`l` 键（用的是 h/l/j/k/+/-/1-4）— `l` 在 timeline 中是"向右滚动"！
- heatmap 无 `a`/`l` 键（用的是 j/k/enter）

**冲突解决方案：** 全局操作快捷键的优先级低于窗格特定键。调整代码结构：先检查窗格特定键，窗格特定键中未处理的按键 fall through 到全局操作检查。

```go
// 修正后的路由（先窗格特定，后全局操作）：
switch m.activePane {
case paneTree:
    // j/k/↑/↓ 导航 + enter + Shift+K
    // 返回后不 fall through
case paneTimeline:
    handled, newM := m.handleTimelineKey(key)
    if handled { return newM, nil }
    // 未处理的键 fall through 到全局操作
case paneHeatmap:
    handled, newM := m.handleHeatmapKey(key)
    if handled { return newM, nil }
    // 未处理的键 fall through 到全局操作
}

// 全局操作
if m.selectedPID > 0 && m.connected {
    switch key {
    case "a": ...
    case "l": ...
    case "r": ...
    case "k": if m.activePane != paneTree { ... kill confirm ... }
    }
}
```

**重要：** 需要修改 `handleTimelineKey` 和 `handleHeatmapKey` 的返回值签名，增加 `handled bool` 返回值，表示该键是否被窗格消费。或者使用另一种方式：在全局操作中排除已知窗格键。

**推荐方案：** 保持 handleTimelineKey/handleHeatmapKey 签名不变，改为在全局操作路由中根据 activePane 排除冲突键：

```go
// timeline 中 l 是"右滚"，不应触发 view log
isTimelineConflict := m.activePane == paneTimeline && (key == "l" || key == "h")
if !isTimelineConflict && m.selectedPID > 0 && m.connected {
    switch key {
    case "a": ... // 不与任何窗格冲突
    case "l": ... // 仅 timeline 的 l 冲突，已排除
    case "r": ... // 不与任何窗格冲突
    case "k": ... // tree 中 k=导航，已排除
    }
}
```

### 关键设计：tea.ExecProcess 用法

```go
type execResultMsg struct {
    err error
}

// 在 dashboardKey 中
case "a":
    c := exec.Command(os.Args[0], "gdb", fmt.Sprint(m.selectedPID))
    return m, tea.ExecProcess(c, func(err error) tea.Msg {
        return execResultMsg{err: err}
    })

// 在 Update 中
case execResultMsg:
    if msg.err != nil {
        m.statusMsg = fmt.Sprintf("Command error: %v", msg.err)
    } else {
        m.statusMsg = "Returned to dashboard"
    }
    m.statusMsgTTL = 4  // 4 × 500ms = 2s 后自动清空
    return m, nil
```

`tea.ExecProcess` 会暂停 bubbletea 程序（释放终端控制），执行 `exec.Cmd`（接管 stdin/stdout/stderr），子进程退出后恢复 bubbletea 程序并通过回调返回消息。

**注意事项：**
- `os.Args[0]` 在 `go test` 中指向测试二进制，测试中不要真正调用 `tea.ExecProcess`，只测试消息处理
- `rnix gdb <pid>` 需要 daemon 运行，dashboard 运行时 daemon 必然在运行
- `rnix log <pid>` 是流式输出，用户 Ctrl+C 退出

### 关键设计：Recording 状态追踪

```go
// dashboardModel 新增字段
recording    map[types.PID]string  // PID → recordID
statusMsgTTL int                   // statusMsg 自动清空倒计时（tick 次数）

// 消息类型
type recordToggleMsg struct {
    pid        types.PID
    recordID   string
    stopped    bool
    eventCount uint64
    err        error
}

func toggleRecordCmd(pid types.PID, currentRecordID string) tea.Cmd {
    return func() tea.Msg {
        client, err := ipc.Dial(ipc.SocketPath())
        if err != nil {
            return recordToggleMsg{pid: pid, err: err}
        }
        defer client.Close()

        if currentRecordID == "" {
            recordID, err := client.RecordStart(pid)
            return recordToggleMsg{pid: pid, recordID: recordID, err: err}
        }
        count, err := client.RecordStop(pid)
        return recordToggleMsg{pid: pid, stopped: true, eventCount: count, err: err}
    }
}
```

录制状态在 `m.recording` map 中维护，进程被 kill 或消失后，在 `dashboardTick` 中检测到进程列表不再包含该 PID 时自动清除对应的 recording 条目。

### 关键设计：statusMsg 自动清空

```go
// 在 dashboardTick 中
if m.statusMsgTTL > 0 {
    m.statusMsgTTL--
    if m.statusMsgTTL == 0 {
        m.statusMsg = ""
    }
}
```

所有设置 `statusMsg` 的地方同时设置 `statusMsgTTL = 4`（即 2s 后自动清空）。Kill 成功的提示也使用此机制。

### 不要做的事情

- **不要**新增 IPC 方法 — 完全复用 `client.Kill`、`client.RecordStart`、`client.RecordStop`
- **不要**修改 `ipc/` 包的任何文件
- **不要**修改 `debug/` 包的任何文件
- **不要**修改 `kernel/` 包的任何文件
- **不要**修改 `internal/ui/styles.go` — 录制指示符颜色使用已有 `ui.ColorError`
- **不要**修改 `internal/types/` — 使用已有类型（`types.PID`、`types.SIGTERM`）
- **不要**引入 `bubbles` 依赖
- **不要**创建新的包或子目录
- **不要**使用 `.yml` 后缀
- **不要**使用全大写常量名
- **不要**修改 timeline 或 heatmap 的渲染逻辑（只改键盘路由和联动机制）
- **不要**实现离线回放 — 17-5 实现
- **不要**为 `tea.ExecProcess` 编写集成测试 — 只测试消息处理（`execResultMsg`、`recordToggleMsg`）
- **不要**复用 dashboard 的主 IPC 连接做 RecordStart/Stop — 在 tea.Cmd 中创建独立连接
- **不要**修改 `handleTimelineKey` 或 `handleHeatmapKey` 的函数签名 — 冲突键通过路由层排除

### 组合矩阵

| 本功能 | 交互对象 | 交互方式 | 需验证 |
|--------|----------|----------|--------|
| 即时联动 | dashboardTick 周期刷新 | handlePIDChange 统一方法复用，tick 中仍做周期性 heatmap 刷新 | 是 |
| 即时联动 | timeline startTimelineCmd | PID 变化时 stop 旧流 + 启动新流，同已有逻辑 | 是 |
| 即时联动 | heatmap fetchHeatmapCmd | PID 变化时清空旧数据 + 发起新 fetch，同已有逻辑 | 是 |
| 全局 k (kill) | tree 窗格 k=导航 | tree 中 k 保持向上导航；非 tree 中 k 触发 kill 确认 | 是 |
| 全局 l (log) | timeline 窗格 l=右滚 | timeline 中 l 保持右滚；非 timeline 中 l 触发 view log | 是 |
| a (gdb) | tea.ExecProcess | 暂停 dashboard TUI → 运行 gdb → 恢复 dashboard | 是 |
| l (log) | tea.ExecProcess | 暂停 dashboard TUI → 运行 log → 恢复 dashboard | 是 |
| r (recording) | recording map | 切换录制状态，独立 IPC 连接 | 是 |
| r (recording) | 进程 kill/消失 | dashboardTick 检测进程消失后清除 recording 条目 | 是 |
| 录制指示符 | 树窗格渲染 | 录制中的 PID 行尾追加红色 ● | 是 |
| statusMsgTTL | dashboardTick | 每次 tick 递减，归零清空 statusMsg | 是 |
| confirmKill | 全局操作 | confirmKill 状态在全局键处理最前端拦截（已有） | 是 |

### Project Structure Notes

修改文件：
- `cmd/rnix/dashboard.go` — 新增 `handlePIDChange` 统一方法、`execResultMsg`/`recordToggleMsg` 消息类型、`toggleRecordCmd` IPC 命令、全局操作键路由、`recording` map 和 `statusMsgTTL` 字段、树窗格录制指示符渲染、状态栏操作键提示和 `●REC` 指示；重构 `dashboardKey` 键盘路由（全局操作提取）、`dashboardTick`（复用 handlePIDChange + statusMsgTTL 递减 + recording 清理）
- `cmd/rnix/dashboard_test.go` — 新增 15 个测试（即时联动、全局操作、消息处理、状态栏、录制指示）

新增导入到 `dashboard.go`：`os`、`os/exec`（用于 `tea.ExecProcess`）

不新增文件，不修改其他包。

### References

- [Source: cmd/rnix/dashboard.go:101-135] — dashboardModel 结构体（扩展 recording/statusMsgTTL 字段）
- [Source: cmd/rnix/dashboard.go:243-314] — dashboardKey 方法（重构全局操作路由）
- [Source: cmd/rnix/dashboard.go:129-173] — dashboardTick 方法（复用 handlePIDChange + 清理 recording）
- [Source: cmd/rnix/dashboard.go:506-509] — startTimelineCmd（handlePIDChange 中调用）
- [Source: cmd/rnix/dashboard.go:921-1001] — renderHeatmapPane（联动验证）
- [Source: cmd/rnix/dashboard.go:612-686] — renderTimelinePane（联动验证）
- [Source: cmd/rnix/dashboard.go:417-378] — renderDashboardTreePane（添加录制指示符）
- [Source: cmd/rnix/dashboard.go:346-357] — renderDashboardStatus（更新操作键提示 + ●REC）
- [Source: ipc/client.go] — Kill/RecordStart/RecordStop/AttachGdb/AttachLog 客户端方法
- [Source: ipc/protocol.go:17-37] — Method 常量（MethodRecordStart/MethodRecordStop/MethodAttachGdb/MethodAttachLog）
- [Source: _bmad-output/planning-artifacts/epics/epic-17-可视化调试面板-visual-debugging-dashboard.md] — Epic 17 AC
- [Source: _bmad-output/implementation-artifacts/17-3-context-heatmap-pane.md] — 17-3 前序 story
- [Source: _bmad-output/implementation-artifacts/17-2-tracing-timeline-pane.md] — 17-2 前序 story
- [Source: _bmad-output/implementation-artifacts/17-1-dashboard-framework-and-agent-tree-pane.md] — 17-1 前序 story
- [Source: _bmad-output/project-context.md] — 项目规则

### 技术栈

- Go 1.26 标准库
- `charm.land/bubbletea/v2 v2.0.0` — TUI 框架（`tea.ExecProcess` 暂停/恢复）
- `github.com/charmbracelet/lipgloss v1.1.0` — 终端样式
- `os/exec` — 子进程执行（gdb/log 命令）
- `github.com/rnixai/rnix/ipc` — IPC 客户端（RecordStart/RecordStop）
- `github.com/rnixai/rnix/internal/types` — PID、SIGTERM
- `github.com/rnixai/rnix/internal/ui` — 颜色常量（ColorError 用于录制指示）
- 无新增外部依赖

### 前置 Story 学习总结

**来自 17-1 实现：**
1. bubbletea v2 API：`View()` 返回 `tea.View`（非 string），`tea.NewView()` + `v.AltScreen = true`
2. 窗格布局：lipgloss `Border(RoundedBorder())` + `BorderForeground(color)` + `Width/Height`
3. `dashboardModel` 核心字段：`activePane`/`selectedPID`/`processes`/`treeRows`/`treeCursor`/`treeOffset`/`confirmKill`/`confirmPID`
4. `paneType` 常量：`paneTree`=0, `paneTimeline`=1, `paneHeatmap`=2
5. Tab 切换 `activePane = (activePane + 1) % 3`
6. 键盘路由通过 `switch m.activePane { case paneXxx: ... }` 分支
7. Kill 确认：`confirmKill` 状态在 `dashboardKey` 最前端拦截，`y` 执行 `client.Kill`，其他键取消
8. `treeRows` 由 `buildProcessTree` + `flattenTree` 构建

**来自 17-2 实现：**
1. 异步数据获取模式：`tea.Cmd` 中创建独立 IPC 连接，通过消息类型回传
2. `startTimelineCmd` → `timelineStreamStartedMsg` → `waitTimelineEventCmd` 循环（流式）
3. PID 变化时清空旧数据：`handleTimelinePIDChange()` 清空 events/cursor
4. `handleTimelineKey` 处理 timeline 特定键：h/l/j/k/+/-/1-4
5. `stopTimelineStream()` 关闭旧订阅
6. 颜色本地定义：`const colorIPC = "#9B59B6"`

**来自 17-3 实现：**
1. 请求-响应式 IPC：`fetchHeatmapCmd` 单次调用 `client.CtxProfile`（非流式）
2. `handleHeatmapPIDChange()` 清空 profile/segments/cursor/expanded/err
3. `handleHeatmapKey` 处理 heatmap 特定键：j/k/enter
4. `heatmapTickCount` 用于周期刷新（每 5 tick = 2.5s）
5. `heatmapErr` 字段记录 IPC 错误并渲染

**来自 17-3 Code Review 修复：**
1. `heatmapExpanded` 字段和 enter 键切换
2. tool 类型合并逻辑
3. 错误显示（`heatmapErr`）

**来自 Git 分析：**
- 最新 commit：`feat: update traceability report for story 17-3`
- commit 模式：`feat: complete story X-Y - description`
- 最近工作集中在 Epic 17 dashboard TUI 开发

### 测试模式参考

```go
// 构造测试 model
func newTestDashboardModel(procs []vfs.ProcInfo) dashboardModel
func newTestTimelineDashboardModel() dashboardModel
func newTestHeatmapDashboardModel() dashboardModel

// 模拟键盘事件
updated, cmd := m.Update(tea.KeyPressMsg{Code: 'j'})
um := updated.(dashboardModel)

// 模拟消息处理
updated, _ := m.Update(execResultMsg{err: nil})
updated, _ := m.Update(recordToggleMsg{pid: 2, recordID: "rec-001"})

// 断言 view 输出
v := m.View()
if !strings.Contains(v.Content, "●REC") { t.Errorf(...) }

// IPC 隔离
old := ipc.SocketPathOverride
ipc.SocketPathOverride = "/tmp/rnix-nonexistent-dashboard-test.sock"
defer func() { ipc.SocketPathOverride = old }()

// 命名规范：Test<Component>_<Scenario>
func TestDashboardModel_PIDChangeImmediateLinkage(t *testing.T) { ... }
func TestDashboardModel_GlobalKillConfirm(t *testing.T) { ... }
func TestExecResultMsg_Success(t *testing.T) { ... }
func TestRecordToggleMsg_Start(t *testing.T) { ... }
```

## Dev Agent Record

### Agent Model Used

claude-4.6-opus-high-thinking

### Debug Log References

### Completion Notes List

- ✅ Task 1: 实现 `handlePIDChange()` 统一方法，tree 导航 PID 变化时即时返回 `tea.Batch(startTimelineCmd, fetchHeatmapCmd)`。`dashboardTick` 中 PID 变化检测逻辑替换为 `handlePIDChange()` 调用（DRY）。`selectedPID==0` 时只清空不发 IPC。
- ✅ Task 2: 重构 `dashboardKey` 键盘路由：tree 窗格提前处理并 return；非 tree 窗格中全局操作（k/a/l/r）优先于窗格特定键。timeline 的 l/h 键通过 `isTimelineConflict` 排除。tree 中 k 保持导航功能。
- ✅ Task 3: Kill 确认增强：kill 成功后 `statusMsg = "Killed PID %d"` + `statusMsgTTL = 4`。
- ✅ Task 4: `a` 键触发 `tea.ExecProcess(exec.Command(os.Args[0], "gdb", pid))`，暂停 TUI 运行 gdb。
- ✅ Task 5: `l` 键触发 `tea.ExecProcess(exec.Command(os.Args[0], "log", pid))`，暂停 TUI 运行 log。
- ✅ Task 6: 实现 `toggleRecordCmd` 独立 IPC 连接。`recordToggleMsg` 处理录制状态切换。`recording` map 在 `dashboardTick` 中清理已消失进程。
- ✅ Task 7: 状态栏显示 `k:Kill a:GDB l:Log r:Record` 操作提示。录制中显示红色 `●REC`。`statusMsgTTL` 在 tick 中递减归零清空。
- ✅ Task 8: 树窗格中录制 PID 行尾追加红色 `●` 指示符。
- ✅ Task 9: 全部 15 个 ATDD 测试通过。更新 17-3 heatmap cursor 测试使用 arrow key（k 键现在触发全局 kill）。全项目回归测试通过（仅 2 个预先存在的环境相关失败）。

### Change Log

- 2026-03-09: Story 17-4 实现完成 — 即时窗格联动 + 全局进程操作 (k/a/l/r)

### File List

- `cmd/rnix/dashboard.go` — 新增 `os/exec` 导入；实现 `handlePIDChange()` 统一方法；实现 `toggleRecordCmd()` IPC 命令；重构 `dashboardKey()` 全局操作路由（a/l/r/k）+ tree 即时联动；实现 `execResultMsg`/`recordToggleMsg` 消息处理；重构 `dashboardTick()` 复用 handlePIDChange + statusMsgTTL 递减 + recording 清理；更新 `renderDashboardStatus()` 操作键提示 + ●REC；更新 `renderDashboardTreePane()` 录制指示符
- `cmd/rnix/dashboard_test.go` — 15 个 ATDD 测试全部通过（即时联动、handlePIDChange、全局 kill、execResultMsg、recordToggleMsg、状态栏、录制指示）；更新 17-3 heatmap cursor 测试（k→up arrow）
