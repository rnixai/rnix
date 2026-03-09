# Story 17.5: 离线回放分析

Status: review

## Story

As a 平台构建者,
I want dashboard 支持加载录制文件进行离线回放分析,
So that 我可以在智能体完成后回顾和分析其历史执行。

## Acceptance Criteria

1. **Given** 存在持久化的录制文件
   **When** 用户执行 `rnix dashboard --load <record-dir>`
   **Then** dashboard 从录制文件加载历史数据，所有窗格展示录制内容

2. **Given** 离线回放模式
   **When** 用户操作时间轴
   **Then** 支持播放/暂停、速度调节、时间轴拖拽跳转和逐帧前进/后退

## Tasks / Subtasks

- [x] Task 1: `--load` 标志与回放模型初始化 (AC: #1)
  - [x] 1.1 在 `dashboardCmd` 添加 `--load` string flag（接受 record-dir 路径或 record-id）
  - [x] 1.2 在 `runDashboard` 中：若 `--load` 非空，调用 `resolveRecordDir(loadArg)` 解析路径（支持直接路径和 record-id 查找）
  - [x] 1.3 调用 `debug.NewRecordReader(recordDir)` 加载录制数据
  - [x] 1.4 调用 `newReplayDashboardModel(reader)` 创建回放模式模型（`client == nil`，不连接 IPC daemon）
  - [x] 1.5 回放模式下跳过 `ipc.Dial` 和 "No rnix daemon" 错误——直接启动 TUI

- [x] Task 2: dashboardModel 回放模式字段 (AC: #1, #2)
  - [x] 2.1 新增字段 `replayMode bool`——标识离线回放模式
  - [x] 2.2 新增字段 `replayReader *debug.RecordReader`——持有录制数据
  - [x] 2.3 新增字段 `replayCursor int`——当前回放位置（0-based 事件索引，-1=初始状态）
  - [x] 2.4 新增字段 `replayPlaying bool`——是否正在自动播放
  - [x] 2.5 新增字段 `replaySpeed float64`——播放速率（0.5/1.0/2.0/4.0/8.0）
  - [x] 2.6 新增字段 `replayLastTick time.Time`——上次播放 tick 时间戳，用于速率计算
  - [x] 2.7 `newReplayDashboardModel(reader)` 初始化：`replayMode=true`, `replayCursor=-1`, `replaySpeed=1.0`, `connected=false`, `timelineFilters=defaultTimelineFilters()`, `recording=make(map[types.PID]string)`

- [x] Task 3: RecordEvent → SyscallEventWire 转换 (AC: #1)
  - [x] 3.1 在 `dashboard.go` 中实现 `recordEventToWire(ev debug.RecordEvent) ipc.SyscallEventWire`
  - [x] 3.2 映射：`ev.Timestamp.Milliseconds()` → `TimestampMs`，`ev.PID` → `PID`，`ev.Syscall.Syscall` → `Syscall`，`ev.Syscall.Args` → `Args`，`ev.Syscall.Result` → `Result`，`ev.Syscall.Err` → `Error`，`ev.Syscall.Duration` → `DurationMs`
  - [x] 3.3 仅对 `ev.Type == debug.RecordSyscall && ev.Syscall != nil` 的事件转换，其他类型跳过

- [x] Task 4: 录制数据 → 智能体树窗格 (AC: #1)
  - [x] 4.1 实现 `buildReplayProcessTree(reader *debug.RecordReader, cursor int) []vfs.ProcInfo`——从录制元数据和事件推导进程信息
  - [x] 4.2 从 `reader.Metadata()` 获取 PID 和 intent
  - [x] 4.3 扫描 cursor 位置之前的 `StateChangeData` 事件，推导进程在该时间点的状态
  - [x] 4.4 扫描 cursor 位置之前的 `ContextSnapshotData` 事件，获取最近的 token 估计
  - [x] 4.5 构建单个 `vfs.ProcInfo`（录制只追踪单个 PID），PPID=1，设置 state/intent/tokens
  - [x] 4.6 在 `dashboardTick` 的 `replayMode` 分支中调用此函数，更新 `m.processes` 和 `m.treeRows`
  - [x] 4.7 自动设置 `m.selectedPID = reader.Metadata().PID`

- [x] Task 5: 录制数据 → 时间线窗格 (AC: #1)
  - [x] 5.1 实现 `loadReplayTimeline(reader *debug.RecordReader, cursor int) []timelineEvent`——加载 cursor 位置之前的所有 syscall 事件到 timeline
  - [x] 5.2 过滤 `RecordSyscall` 事件，调用 `recordEventToWire` 转换，再调用 `classifySyscall` 分类
  - [x] 5.3 仅加载 `seqNum <= cursor` 的事件（cursor == -1 时返回空切片）
  - [x] 5.4 在 `dashboardTick` 的 `replayMode` 分支中调用此函数，更新 `m.timelineEvents`
  - [x] 5.5 回放模式下 timeline 不使用 `AttachDebug` 流——`timelineEventCh` 和 `timelineStopCh` 保持 nil

- [x] Task 6: 录制数据 → 上下文热力图窗格 (AC: #1)
  - [x] 6.1 实现 `buildReplayHeatmap(reader *debug.RecordReader, cursor int) *debug.CtxProfileResult`——从 cursor 位置之前最近的 ContextSnapshot 构建热力图
  - [x] 6.2 反向扫描事件找到最近的 `RecordContextSnapshot`（`ev.Type == debug.RecordContextSnapshot && ev.Context != nil`）
  - [x] 6.3 从 `ContextSnapshotData` 构建 `CtxProfileResult`：`TokensUsed = ev.Context.TokenEstimate`，`Messages` 内容推导分类（system/skill/tool/user/assistant），生成 `Classification` 和 `TopConsumers`
  - [x] 6.4 如果没有 ContextSnapshot 事件，返回 nil（热力图显示 "No context data"）
  - [x] 6.5 在 `dashboardTick` 的 `replayMode` 分支中调用此函数，更新 `m.heatmapProfile`

- [x] Task 7: 回放控制——播放/暂停与速率 (AC: #2)
  - [x] 7.1 空格键（Space）切换 `replayPlaying`——true↔false
  - [x] 7.2 `[`/`]` 键调速——`[` 减半速率（最低 0.5x），`]` 翻倍速率（最高 8.0x）
  - [x] 7.3 自动播放逻辑在 `dashboardTick` 中：若 `replayPlaying && replayCursor < reader.EventCount()-1`，计算自上次 tick 经过的时间 × `replaySpeed`，若累积 >= 一个事件间隔，前进 `replayCursor`
  - [x] 7.4 简化速率模型：每个 tick（500ms）根据 speed 推进 N 个事件——`eventsPerTick = int(replaySpeed)`（speed=0.5 → 每 2 tick 推进 1 个，speed=1.0 → 每 tick 推进 1 个，speed=4.0 → 每 tick 推进 4 个）
  - [x] 7.5 到达最后一个事件时自动暂停：`replayPlaying = false`

- [x] Task 8: 回放控制——逐帧前进/后退与跳转 (AC: #2)
  - [x] 8.1 `.`（句号）键：逐帧前进——`replayCursor++`（不超过 EventCount-1），自动暂停 `replayPlaying = false`
  - [x] 8.2 `,`（逗号）键：逐帧后退——`replayCursor--`（不低于 0），自动暂停
  - [x] 8.3 `0` 键：跳到开头——`replayCursor = 0`，暂停
  - [x] 8.4 `$` 键：跳到末尾——`replayCursor = EventCount-1`，暂停
  - [x] 8.5 回放模式下 timeline 的 `h`/`l`（左右滚动）保持原有功能——滚动时间视图窗口

- [x] Task 9: 回放模式状态栏 (AC: #1, #2)
  - [x] 9.1 回放模式下替换状态栏左侧内容：显示 `▶ REPLAY: <record-id>` 或 `⏸ REPLAY: <record-id>`（播放/暂停指示）
  - [x] 9.2 显示进度：`[cursor/total] speed×`（如 `[42/128] 2.0×`）
  - [x] 9.3 显示快捷键提示：`Space:Play/Pause  [/]:Speed  ,/.:Step  0:Start  $:End  q:Quit`
  - [x] 9.4 不显示 live 模式的 `k:Kill a:GDB l:Log r:Record` 快捷键

- [x] Task 10: 回放模式键路由 (AC: #1, #2)
  - [x] 10.1 在 `dashboardKey` 中，q/Ctrl+C 退出保持不变
  - [x] 10.2 Tab 窗格切换保持不变
  - [x] 10.3 在 `confirmKill` 检查之后、全局操作键之前，添加 `if m.replayMode` 分支
  - [x] 10.4 回放模式分支处理：Space、`[`、`]`、`.`、`,`、`0`、`$`，然后 fall-through 到窗格特定键（tree 导航、timeline 缩放/滚动/过滤、heatmap 选择）
  - [x] 10.5 回放模式下屏蔽 live 操作键：`k`/`K`（kill）、`a`（gdb）、`l`（log）、`r`（record）——设置 `statusMsg = "Not available in replay mode"`

- [x] Task 11: dashboardTick 回放分支 (AC: #1, #2)
  - [x] 11.1 在 `dashboardTick` 开头添加 `if m.replayMode` 分支，不执行 IPC 连接/重连逻辑
  - [x] 11.2 回放 tick：处理自动播放推进（Task 7），然后重建 tree/timeline/heatmap 数据
  - [x] 11.3 优化：仅在 `replayCursor` 发生变化时重建数据（用 `prevReplayCursor int` 字段跟踪）
  - [x] 11.4 `statusMsgTTL` 递减逻辑保持不变（回放和 live 共享）

- [x] Task 12: resolveRecordDir 辅助函数 (AC: #1)
  - [x] 12.1 实现 `resolveRecordDir(loadArg string) (string, error)`
  - [x] 12.2 如果 `loadArg` 是已存在的目录路径 → 直接返回
  - [x] 12.3 否则视为 record-id → 调用 `debug.NewRecordManager(findRecordBaseDir()).FindRecord(loadArg)` 查找
  - [x] 12.4 两种方式都找不到 → 返回明确的错误消息

- [x] Task 13: 测试 (AC: #1, #2)
  - [x] 13.1 `TestReplayDashboard_Init`：`newReplayDashboardModel` 正确初始化所有回放字段
  - [x] 13.2 `TestRecordEventToWire`：RecordEvent → SyscallEventWire 转换正确
  - [x] 13.3 `TestRecordEventToWire_NonSyscall`：非 syscall 事件返回零值
  - [x] 13.4 `TestReplayDashboard_TreePane`：`buildReplayProcessTree` 从录制数据正确构建进程信息
  - [x] 13.5 `TestReplayDashboard_Timeline`：`loadReplayTimeline` 正确加载并过滤事件到 cursor 位置
  - [x] 13.6 `TestReplayDashboard_Heatmap`：`buildReplayHeatmap` 正确找到最近的 ContextSnapshot
  - [x] 13.7 `TestReplayDashboard_PlayPause`：Space 键切换播放状态
  - [x] 13.8 `TestReplayDashboard_SpeedControl`：`[`/`]` 键正确调整速率，边界检查 0.5x–8.0x
  - [x] 13.9 `TestReplayDashboard_FrameStep`：`.`/`,` 键逐帧前进/后退，边界检查
  - [x] 13.10 `TestReplayDashboard_JumpStartEnd`：`0`/`$` 键跳转
  - [x] 13.11 `TestReplayDashboard_AutoPlayAdvance`：tick 中根据 speed 推进 cursor
  - [x] 13.12 `TestReplayDashboard_AutoPlayPauseAtEnd`：到达末尾自动暂停
  - [x] 13.13 `TestReplayDashboard_LiveKeysBlocked`：回放模式屏蔽 k/a/l/r 键
  - [x] 13.14 `TestReplayDashboard_StatusBar`：回放模式状态栏内容正确
  - [x] 13.15 `TestReplayDashboard_TickNoIPC`：回放模式 tick 不尝试 IPC 连接

## Dev Notes

### 架构决策

1. **回放模式 vs 新 Model** — 复用 `dashboardModel`，通过 `replayMode bool` 区分行为路径。不创建独立的 replayModel，因为树/时间线/热力图的渲染逻辑完全复用，只有数据来源不同。
2. **数据加载策略** — 一次性加载所有事件到内存（`RecordReader.Events()`），cursor 移动时通过切片索引获取数据。录制文件通常 < 10MB，内存开销可接受。不使用流式加载。
3. **RecordReader 直接使用** — 不使用 `ReplaySession`（debug/replay.go），因为 ReplaySession 的 Next/Prev/Goto 是为交互式 CLI 设计的。dashboard 的回放需要直接索引访问和批量过滤，直接操作 `RecordReader.Events()` 切片更高效。
4. **不新增 IPC 方法** — 回放模式完全本地化，不连接 daemon，不需要新的 IPC 协议。
5. **播放速率模型** — 简化为离散步进（每 tick N 个事件），不模拟真实时间间隔。原因：事件间隔不均匀，真实时间模拟在 TUI 500ms tick 下效果不佳。

### 关键设计：回放模式 tick 流程

```go
func (m dashboardModel) dashboardTick() (tea.Model, tea.Cmd) {
    // 共享：statusMsgTTL 递减
    // 共享：heatmapTickCount++
    
    if m.replayMode {
        // 自动播放推进
        if m.replayPlaying && m.replayCursor < m.replayReader.EventCount()-1 {
            advance := int(m.replaySpeed)
            if m.replaySpeed < 1.0 {
                // 0.5x: heatmapTickCount%2==0 时推进 1
                if m.heatmapTickCount%int(1.0/m.replaySpeed) == 0 {
                    advance = 1
                } else {
                    advance = 0
                }
            }
            m.replayCursor = min(m.replayCursor+advance, m.replayReader.EventCount()-1)
            if m.replayCursor >= m.replayReader.EventCount()-1 {
                m.replayPlaying = false
            }
        }
        
        // 仅在 cursor 变化时重建数据
        if m.replayCursor != m.prevReplayCursor {
            m.processes = buildReplayProcessTree(m.replayReader, m.replayCursor)
            // ... 更新 treeRows, selectedPID
            m.timelineEvents = loadReplayTimeline(m.replayReader, m.replayCursor)
            m.heatmapProfile = buildReplayHeatmap(m.replayReader, m.replayCursor)
            // ... 更新 heatmapSegments
            m.prevReplayCursor = m.replayCursor
        }
        
        return m, tickCmd()
    }
    
    // ... 原有 live 模式逻辑 ...
}
```

### 关键设计：RecordEvent → SyscallEventWire

```go
func recordEventToWire(ev debug.RecordEvent) ipc.SyscallEventWire {
    if ev.Type != debug.RecordSyscall || ev.Syscall == nil {
        return ipc.SyscallEventWire{}
    }
    return ipc.SyscallEventWire{
        TimestampMs: ev.Timestamp.Milliseconds(),
        PID:         ev.PID,
        Syscall:     ev.Syscall.Syscall,
        Args:        ev.Syscall.Args,
        Result:      ev.Syscall.Result,
        Error:       ev.Syscall.Err,
        DurationMs:  float64(ev.Syscall.Duration.Milliseconds()),
    }
}
```

### 关键设计：ContextSnapshot → CtxProfileResult 近似

ContextSnapshotData 包含 Messages 列表和 TokenEstimate，但不包含完整的分类结果（active/warm/cold/leaked）。回放模式下的热力图是近似的：
- 从 Messages 内容前缀推导 kind（"[system]" → System，"[skill]" → Skill，"[tool]" → Tool，"[user]" → User，"[assistant]" → Assistant）
- 按 kind 累计估计 token 数
- 所有段标记为 Active（录制时间点的数据本质上都是"活跃的"）
- 若 Messages 无前缀，fallback 到 User/Assistant 交替推测

### 关键设计：resolveRecordDir

```go
func resolveRecordDir(loadArg string) (string, error) {
    // 1. 直接路径
    if info, err := os.Stat(loadArg); err == nil && info.IsDir() {
        // 验证 metadata.json 存在
        if _, err := os.Stat(filepath.Join(loadArg, "metadata.json")); err == nil {
            return loadArg, nil
        }
        return "", fmt.Errorf("directory %s does not contain metadata.json", loadArg)
    }
    // 2. record-id 查找
    mgr := debug.NewRecordManager(findRecordBaseDir())
    dir, err := mgr.FindRecord(loadArg)
    if err != nil {
        return "", fmt.Errorf("recording %q not found: %w", loadArg, err)
    }
    return dir, nil
}
```

### 关键设计：回放模式键路由

```
dashboardKey:
  q/Ctrl+C → 退出（共享）
  confirmKill y/N → 不触发（回放模式不进入 confirmKill 状态）
  Tab → 窗格切换（共享）
  
  if replayMode:
    Space → play/pause
    [ → speed down
    ] → speed up
    . → frame forward
    , → frame backward
    0 → jump to start
    $ → jump to end
    k/K/a/l/r → statusMsg "Not available in replay mode"
    fall-through → 窗格特定键（tree j/k/↑/↓, timeline h/l/+/-/1-4, heatmap j/k/Enter）
  
  else: (live 模式原有逻辑)
```

### 不要做的事情

- **不修改 `debug/` 包** — RecordReader/RecordEvent/RecordManager 已有足够的 API
- **不修改 `ipc/` 包** — 回放模式不需要新的 IPC 方法
- **不修改 `internal/types/`、`internal/ui/styles.go`** — 无需新类型或样式
- **不引入 `debug.ReplaySession`** — dashboard 的回放需求与 CLI replay 不同，直接操作 RecordReader
- **不创建新文件** — 所有代码在 `dashboard.go` 和 `dashboard_test.go` 中
- **不修改 live 模式的任何行为** — 回放模式通过 `replayMode` 条件分支完全隔离
- **不模拟真实时间间隔** — 使用离散事件步进模型
- **不尝试为 tea.ExecProcess 写回放测试** — 回放模式不会触发 gdb/log/record

### Project Structure Notes

- 修改：`cmd/rnix/dashboard.go`、`cmd/rnix/dashboard_test.go`
- 新增导入：`debug`（`github.com/rnixai/rnix/debug`）、`path/filepath`、`os`
- `cmd/rnix/dashboard.go` 已导入 `os`（Story 17-4 添加），需新增 `path/filepath` 和 `debug`
- 不新增文件，不修改其他包

### 组合矩阵

| 本功能 | 交互对象 | 交互方式 | 需验证 |
|--------|----------|----------|--------|
| 回放模式 | live 模式 | 互斥：`replayMode` 为 true 时不执行任何 IPC 调用 | 是 |
| 回放 timeline | timeline 缩放/滚动/过滤 | 共存：缩放/滚动/过滤功能在回放模式下完全复用 | 是 |
| 回放 heatmap | heatmap 选择/展开 | 共存：j/k/Enter 功能在回放模式下完全复用 | 是 |
| 回放 tree | tree 导航 | 共存：j/k/↑/↓ 在回放模式下工作（但通常只有 1 个进程） | 是 |
| 回放 space 键 | timeline/heatmap 无 space 键冲突 | 独立：space 仅在回放模式有意义 | 否 |
| 回放 `,`/`.` 键 | heatmap 无冲突、timeline 无冲突 | 独立：这些键在 live 模式无功能 | 否 |
| 回放 `[`/`]` 键 | timeline `+`/`-` 缩放 | 独立：不同键，不冲突 | 否 |
| 回放 `0`/`$` 键 | 无冲突 | 独立：live 模式无此功能 | 否 |
| 回放 live 键屏蔽 | k/a/l/r 操作 | 覆盖：回放模式下这些键显示提示而非执行操作 | 是 |

### References

- [Source: debug/record.go] — RecordEvent, RecordEventType, SyscallEventData, ContextSnapshotData, LLMResponseData, StateChangeData, RecordMetadata
- [Source: debug/record_reader.go] — NewRecordReader, RecordReader.Events(), RecordReader.EventsInRange(), RecordReader.Metadata(), RecordReader.EventCount()
- [Source: debug/record_manager.go] — RecordManager.FindRecord(), RecordManager.LoadRecord()
- [Source: debug/replay.go] — ReplaySession（不使用，但供参考）
- [Source: ipc/protocol.go#L293-303] — SyscallEventWire 结构体
- [Source: cmd/rnix/dashboard.go#L22-28] — dashboardCmd 定义
- [Source: cmd/rnix/dashboard.go#L118-157] — dashboardModel 结构体
- [Source: cmd/rnix/dashboard.go#L159-167] — newDashboardModel
- [Source: cmd/rnix/dashboard.go#L169-171] — Init
- [Source: cmd/rnix/dashboard.go#L173-227] — Update
- [Source: cmd/rnix/dashboard.go#L231-298] — dashboardTick
- [Source: cmd/rnix/dashboard.go#L578-602] — startTimelineCmd
- [Source: cmd/rnix/replay.go#L338-341] — findRecordBaseDir
- [Source: cmd/rnix/dashboard_test.go] — 现有测试模式：newTestDashboardModel、tea.KeyPressMsg
- [Source: _bmad-output/implementation-artifacts/17-4-pane-linkage-and-process-operations.md] — 前置 Story
- [Source: _bmad-output/project-context.md] — 项目规范

### 技术栈

- Go 1.26、bubbletea v2、lipgloss、debug（RecordReader/RecordEvent）、os、path/filepath

### 前置 Story 学习总结

- **17-1**：bubbletea v2 API（tea.View/tea.Tick/KeyPressMsg）、dashboardModel 字段布局、paneType/activePane、Tab 切换、treeRows/treeCursor/treeOffset 虚拟滚动、tickCmd 500ms 间隔
- **17-2**：异步 IPC 流（startTimelineCmd → AttachDebug → timelineEventCh → waitTimelineEventCmd）、timelineEvent 结构、classifySyscall 分类、timelineZoomLevel/timelineViewStart/timelineEventCursor/timelineFilters、handleTimelineKey
- **17-3**：fetchHeatmapCmd → CtxProfile → heatmapProfileMsg、buildHeatmapSegments、heatmapSegment 结构、renderHeatmapPane、heatmapCursor/heatmapExpanded/heatmapTickCount/heatmapErr、handleHeatmapKey
- **17-4**：handlePIDChange 统一方法、全局操作路由（k/a/l/r）、tea.ExecProcess 用法、recording map、toggleRecordCmd、recordToggleMsg、statusMsg/statusMsgTTL、execResultMsg、renderDashboardStatus

### 测试模式参考

- 使用 `newTestDashboardModel` 构造基础模型，然后设置回放字段
- 需要构造 mock `debug.RecordReader`——在 `dashboard_test.go` 中创建临时目录写入 metadata.json 和 events.jsonl，然后用 `debug.NewRecordReader` 加载
- 回放控制测试通过发送 `tea.KeyPressMsg` 并检查模型状态变化
- 命名：`TestReplayDashboard_<Scenario>`

## Dev Agent Record

### Agent Model Used

claude-4.6-opus

### Debug Log References

- bubbletea v2 空格键 `String()` 返回 `"space"` 而非 `" "`，需同时匹配两种形式

### Completion Notes List

- ✅ 实现 `newReplayDashboardModel` — 初始化所有回放字段（replayMode/replayCursor/replaySpeed 等）
- ✅ 实现 `recordEventToWire` — RecordEvent → SyscallEventWire 映射，仅处理 RecordSyscall 类型
- ✅ 实现 `buildReplayProcessTree` — 从录制元数据和事件推导进程状态/token
- ✅ 实现 `loadReplayTimeline` — 加载 cursor 之前的 syscall 事件并分类
- ✅ 实现 `buildReplayHeatmap` — 反向扫描找最近 ContextSnapshot，从消息前缀推导 kind 构建近似热力图
- ✅ 实现 `replayTick` — 自动播放推进 + cursor 变化时重建 tree/timeline/heatmap
- ✅ 实现 `handleReplayKey` — Space 播放/暂停、[/] 调速、,/. 逐帧、0/$ 跳转、屏蔽 live 键
- ✅ 实现 `renderReplayStatus` — ▶/⏸ REPLAY 指示 + 进度 + 快捷键提示
- ✅ 实现 `resolveRecordDir` — 支持直接路径和 record-id 两种方式解析
- ✅ 注册 `--load` flag 到 dashboardCmd（main.go init）
- ✅ `runDashboard` 支持 --load 参数直接进入回放模式
- ✅ 15 个 ATDD 测试全部通过，无回归

### Change Log

- 2026-03-09: Story 17-5 完整实现——离线回放分析功能（13 个 Task，15 个测试）

### File List

- cmd/rnix/dashboard.go (modified)
- cmd/rnix/dashboard_test.go (modified — ATDD tests pre-written)
- cmd/rnix/main.go (modified — --load flag registration)
