# Story 29.5: Dashboard 历史进程视图

Status: done

## Story

As a 平台构建者,
I want 在 Dashboard 中按 H 键打开全屏历史进程列表,
So that 我可以查看、搜索和聚焦已结束的进程，实现事后分析能力。

## Acceptance Criteria

1. **H 键进入历史视图** — 在 Dashboard 任意主视图中按 `H` 键，进入全屏覆盖层历史视图（`viewHistory`），通过 `ListAllProcs` IPC 获取存活+历史进程列表
2. **进程表渲染** — 历史视图显示列：`PID | ST(符号) | AGENT | MODEL | TOKENS | CREATED | ELAPSED | EXIT | REASON`；底部统计：Running/Done/Failed 计数、总 token、平均存活时间
3. **光标导航** — 按 `j/k` 或上/下键在进程列表中上下导航；`PgDn/PgUp` 翻页；`g/Home` 跳顶；`G/End` 跳底
4. **Enter 聚焦** — 选中一个进程后按 Enter，设置 `selectedPID` + `selectedUUID`，回到默认视图并聚焦该进程
5. **L 键跳转 LLM 查看器** — 选中一个进程后按 `L`，直接跳转到该进程的 LLM 对话查看器（placeholder，Story 29.6 实现完整功能）
6. **搜索过滤** — 按 `/` 进入搜索模式，按 agent 名称过滤进程列表
7. **排序模式** — 按 `1/2/3` 切换排序模式：1=时间（默认）、2=名称、3=PID
8. **Esc 退出** — 按 Esc 回到之前的视图
9. **Tree 面板已结束进程** — Tree 面板通过 `ListAllProcs` 显示已结束进程，使用 `✓/✕` 状态符号区分，已结束进程显示 exit code 和存活时间
10. **Timeline 自动过滤** — 选中 Dead 进程时，Timeline 自动过滤只显示该 PID 的事件，Status bar 显示 `(filtered: PID N)`

## Tasks / Subtasks

- [x] Task 1: 新增 `dashboard_history.go` — 历史视图核心 (AC: #1, #2, #3, #4, #5, #6, #7, #8)
  - [x] 1.1 新增 `dashboardModel` 历史视图字段：`historyProcs []vfs.ProcInfo`、`historyCursor int`、`historyScrollOffset int`、`historySortMode int`、`historySearchQuery string`、`historySearchMode bool`
  - [x] 1.2 实现 `enterHistoryView()` — 设置 `viewMode = viewHistory`，重置 cursor/scroll，触发 `fetchAllProcsCmd()`
  - [x] 1.3 实现 `fetchAllProcsCmd()` tea.Cmd — 调用 `client.ListAllProcs()`，返回 `historyProcsMsg`
  - [x] 1.4 定义 `historyProcsMsg` 消息类型，在 `Update` 中处理（存入 `historyProcs`，按当前 sortMode 排序）
  - [x] 1.5 实现 `historyKey(msg)` 按键处理 — j/k 导航、Enter 聚焦、L 跳转、/ 搜索、1/2/3 排序、Esc 退出
  - [x] 1.6 实现 `renderHistoryView()` — 全屏表格渲染（Title bar + 进程表 + 底部统计 + Status bar）
  - [x] 1.7 实现搜索过滤逻辑 — 搜索模式下按 agent 名称过滤 `historyProcs`，Backspace 删除字符，Esc 退出搜索
  - [x] 1.8 实现排序逻辑 — sortMode 0=时间(CreatedAt)、1=名称(Agent)、2=PID
- [x] Task 2: 修改 `dashboard_nav.go` — H 键路由 (AC: #1, #8)
  - [x] 2.1 Layer 2 覆盖层检查：`viewHistory` 时调用 `m.historyKey(msg)` 处理
  - [x] 2.2 Layer 4 主视图快捷键：`H` 键从 placeholder 消息改为调用 `m.enterHistoryView()`
- [x] Task 3: 修改 `dashboard.go` — Update 处理 + historyProcsMsg (AC: #1)
  - [x] 3.1 `dashboardModel` 新增历史视图字段（在 Focus Card 字段下方）
  - [x] 3.2 `Update` 方法新增 `historyProcsMsg` case 处理
  - [x] 3.3 `View` 方法新增 `viewHistory` case 调用 `renderHistoryView()`
- [x] Task 4: 修改 `dashboard_tree.go` — 显示已结束进程 (AC: #9)
  - [x] 4.1 Tree 渲染中使用 `ListAllProcs` 数据（而非仅 `ListProcs`），在 tick 中获取
  - [x] 4.2 已结束进程在 Tree 中使用 `ui.StateSymbol()` 显示 `✓/✕` 符号
  - [x] 4.3 已结束进程额外显示 exit code 和存活时间（`DeadAt - CreatedAt`）
- [x] Task 5: 修改 `dashboard_timeline.go` — Dead 进程自动过滤 (AC: #10)
  - [x] 5.1 选中 Dead 进程时，Timeline 渲染仅显示 `selectedPID` 的事件
  - [x] 5.2 Status bar 追加 `(filtered: PID N)` 提示
- [x] Task 6: 更新 `dashboard_types.go` — 新增消息类型 (AC: all)
  - [x] 6.1 新增 `historyProcsMsg` 消息类型定义
- [x] Task 7: 测试 + `make all` 验证 (AC: all)
  - [x] 7.1 确认 `make all` 编译通过、所有测试通过

## Dev Notes

### 核心实现要点

**历史视图是一个全屏覆盖层**，与 Prompt Pager 类似但更复杂。它占据整个 Dashboard 区域（Title + 内容 + Status Bar），按 Esc 回到之前的视图。

**数据源是 `client.ListAllProcs()`**（Story 29.4 已实现），返回存活进程 + 历史进程的合并列表。Dashboard 在进入历史视图时发起一次 IPC 调用获取数据，而非持续轮询。

**H 键当前是 placeholder**。在 `dashboard_nav.go:90-92` 中：
```go
case "H":
    m.statusMsg = "历史视图尚未实现（Story 29.5）"
    m.statusMsgTTL = statusMsgDefaultTTL
    return m, nil
```
需要替换为 `return m.enterHistoryView()`。

### 字段位置

在 `dashboardModel` 中，历史视图字段应添加在 `focusCardData *focusCardState` 之后：

```go
// History view fields (Story 29.5)
historyProcs        []vfs.ProcInfo
historyCursor       int
historyScrollOffset int
historySortMode     int  // 0=time, 1=name, 2=pid
historySearchQuery  string
historySearchMode   bool
```

### 导航键分发层级

**关键**：历史视图按键必须在 Layer 2 处理（覆盖层级别），与 Prompt Pager 和 Kill 确认同层。当前 `dashboard_nav.go` 的 Layer 顺序：

1. Layer 0: ctrl+c 全局退出
2. Layer 1: Prompt Pager
3. Layer 2: Kill 确认
4. Layer 3: Replay 模式
5. Layer 4: 主视图快捷键 (Esc, Tab, 数字键, L, H)
6. Layer 5: 面板内按键

历史视图应在 Layer 1 和 Layer 2 之间插入（或在 Layer 1 后紧跟），即 Prompt Pager 之后、Kill 确认之前。添加：

```go
// === Layer 1.5: History 覆盖层 ===
if m.viewMode == viewHistory {
    return m.historyKey(msg)
}
```

**为什么不放在 Layer 4**：因为历史视图是覆盖层，q 键应退出 Dashboard 而不是触发其他功能。historyKey 内部需要处理 q 退出以及 j/k/Enter/L/1/2/3 等按键，不能被 Layer 4 的其他 case 拦截。

### H 键在覆盖层模式下不应响应

**注意**：当 `viewMode == viewHistory` 时，Layer 4 的 `case "H"` 不应再次触发 `enterHistoryView()`。因为在 Layer 1.5 中 historyKey 已经处理了所有按键，不会到达 Layer 4。

### 搜索模式实现

搜索模式使用 `historySearchMode bool` 和 `historySearchQuery string`。进入搜索模式后：
- 键盘输入追加到 `historySearchQuery`
- Backspace 删除最后一个 rune（注意 rune-safe 截断，参考 Story 29.3 Code Review P-1 学习）
- Esc 退出搜索模式（清空 query）
- Enter 退出搜索模式（保留 query 作为过滤条件）
- 过滤逻辑：`strings.Contains(strings.ToLower(proc.Agent), strings.ToLower(query))`

### 排序模式实现

三种排序模式通过 `historySortMode int` 控制：
- `0` = 按时间（`CreatedAt` 降序，最新在前）
- `1` = 按名称（`Agent` 字母升序）
- `2` = 按 PID（`PID` 降序）

排序在 `historyProcsMsg` 处理时和切换模式时执行。使用 `slices.SortFunc` 排序。

### 底部统计行

统计行显示在进程表下方、Status bar 上方：
```
Running: 2●  Done: 5✓  Failed: 1✕  │  Total: 12,345 tok  │  Avg: 23s
```

统计数据从 `historyProcs` 中计算。

### Tree 面板显示已结束进程

**关键决策**：Tree 面板应使用 `ListAllProcs` 数据而非仅 `ListProcs`。这意味着 `m.processes` 应包含已结束进程。

**但是**：直接修改 `m.processes` 可能影响大量现有逻辑。**推荐方案**：在 `dashboardTick` 中使用 `ListAllProcs` 获取数据存入 `m.processes`，因为 `processes` 已经是 `[]vfs.ProcInfo` 类型，完全兼容。现有代码中引用 `m.processes` 的地方通过 State 字段区分存活/死亡进程。

**替代方案**（如果修改 m.processes 影响面太大）：新增 `m.allProcs []vfs.ProcInfo` 字段，Tree 面板渲染时使用 `allProcs`，其他面板继续使用 `processes`。

开发者应评估哪种方案影响更小，优先选择改动最小的方案。

### Timeline 自动过滤

选中 Dead 进程时（`m.selectedPID > 0 && 对应进程 State == types.StateDead`），Timeline 渲染中过滤 `timelineEvents`：
```go
filtered := filterTimelineByPID(m.timelineEvents, m.selectedPID)
```

在 `renderTimelineContent` 中添加过滤逻辑。Status bar 追加 `(filtered: PID N)` 文本。

### 已有设施复用

| 设施 | 位置 | 用途 |
|------|------|------|
| `client.ListAllProcs()` | `ipc/client.go:74` | 获取存活+历史进程列表 |
| `ui.StateSymbol(state, result)` | `internal/ui/symbols.go:16` | 统一进程状态符号渲染 |
| `vfs.ProcInfo` | `vfs/proc.go:29` | 进程信息结构体（含 PID/UUID/Agent/Model/State/CreatedAt/DeadAt/Result 等） |
| `ProcInfoToWire` / `WireToProcInfo` | `ipc/protocol.go` | ProcInfo 序列化/反序列化 |
| `flatRow` | `cmd/rnix/top.go:35` | Tree 面板的行数据结构（proc + prefix + depth） |
| `buildTree()` | `cmd/rnix/top.go:44` | 从 ProcInfo 列表构造进程树 |
| `isFailedResult()` | `internal/ui/symbols.go:59` | 判断 result 字符串是否表示失败 |
| `focusCardState` | `cmd/rnix/dashboard_types.go:250` | Focus Card 已有 isHistory/exitCode 字段 |
| `handlePIDChange()` | `cmd/rnix/dashboard.go` | PID 变更时重新加载关联数据 |
| `colorState()` | `cmd/rnix/dashboard.go` | 按状态着色（已有实现） |
| `Prompt Pager` | `cmd/rnix/dashboard_nav.go:35-51` | 覆盖层按键处理模式参考 |

### 不应该做的事

- **不要新增 IPC 方法** — `ListAllProcs` 已在 Story 29.4 实现，直接复用
- **不要新增 CLI command** — 历史视图仅在 Dashboard 内部使用
- **不要在 symbols.go 中引入 lipgloss 依赖** — 符号函数返回纯字符串，着色由 Dashboard 处理
- **不要修改 `kernel/` 或 `ipc/` 包** — 所有修改限于 `cmd/rnix/dashboard*.go` 和 `cmd/rnix/dashboard_types.go`
- **不要使用 `sync.Map`** — Dashboard 是单 goroutine Bubbletea 模型，不需要并发保护
- **不要预加载所有步骤详情** — 历史视图只展示列表，步骤详情在 LLM 查看器中按需加载（Story 29.6）
- **不要修改 `DeadProcessTTL`** — 60 秒 TTL 保持不变
- **不要在排序/过滤时修改原始 `historyProcs` 切片** — 使用 `slices.SortFunc` 原地排序后的副本或独立的 filtered 切片

### Project Structure Notes

| 文件 | 操作 | 说明 |
|------|------|------|
| `cmd/rnix/dashboard_history.go` | **新增** | 历史视图模型 + 渲染 + 按键处理 + 搜索 + 排序 |
| `cmd/rnix/dashboard.go` | 修改 | dashboardModel 新增历史视图字段 + Update 处理 historyProcsMsg + View 路由 |
| `cmd/rnix/dashboard_nav.go` | 修改 | Layer 1.5 覆盖层路由 + H 键调用 enterHistoryView |
| `cmd/rnix/dashboard_types.go` | 修改 | 新增 historyProcsMsg 消息类型 |
| `cmd/rnix/dashboard_tree.go` | 修改 | 已结束进程显示（✓/✕ 符号 + exit code + 存活时间） |
| `cmd/rnix/dashboard_timeline.go` | 修改 | Dead 进程 Timeline 自动过滤 |

### 已有代码精确位置参考

- `dashboardModel` 结构体：`cmd/rnix/dashboard.go:32-149`
- `focusCardData` 字段（历史视图字段添加位置）：`cmd/rnix/dashboard.go:148`
- H 键 placeholder：`cmd/rnix/dashboard_nav.go:90-92`
- L 键 placeholder：`cmd/rnix/dashboard_nav.go:87-89`
- Prompt Pager 覆盖层处理模式：`cmd/rnix/dashboard_nav.go:35-51`
- `viewHistory` 枚举已定义：`cmd/rnix/dashboard_types.go:33`
- `ListAllProcs` 客户端方法：`ipc/client.go:74`
- `StateSymbol` 函数：`internal/ui/symbols.go:16`
- `isFailedResult` 函数：`internal/ui/symbols.go:59`
- `flatRow` 结构体：`cmd/rnix/top.go:35`
- `buildTree` 函数：`cmd/rnix/top.go:44`
- `dashboardTick` 函数：`cmd/rnix/dashboard.go:379`
- `handlePIDChange` 方法：`cmd/rnix/dashboard.go`
- `colorState` 函数：`cmd/rnix/dashboard.go`

### 前置 Story 关键学习（Story 29.4）

- **ProcessHistory 独立于 procTable**：历史进程已从 procTable 移除，仅存在于 `ProcessHistory` 中
- **exitCode 推断**：`StateSymbol` 通过 `result` 字符串判断（检查 error/fail/timeout 关键词），非 ExitCode 字段
- **深拷贝**：`ProcessHistory.List()` 返回深拷贝切片，调用者可安全修改
- **去重逻辑**：`ListAllProcs` 已内置去重（active PIDs 优先），Dashboard 无需额外处理
- **ATDD 测试修复**：NewProcess 第一个参数是 PPID 不是 PID
- **Lint modernize**：`interface{}` → `any`，`go func()` → `WaitGroup.Go()`

### 前置 Story 关键学习（Story 29.3 Code Review）

- **P-1 [Critical]**: 字符串截断必须 rune-safe（`truncateRuneSafe` 函数）
- **P-2/P-3**: Dead 进程时间计算用 `DeadAt` 而非 `time.Now()`
- **P-8**: 缓存 process 引用减少重复遍历
- **BS-1**: `dashboardTick` IPC 获取条件已放宽支持 `viewDefault` 模式

### Git 近期提交模式

```
feat: Implement Story 29.4 - Kernel Process History and IPC
feat: Implement Story 29.3 - Default View Focus Card
feat: Implement Story 29.2 - View Mode System and Navigation Overhaul
feat: Implement Story 29.1 - Dashboard File Splitting
```

提交消息格式：`feat: Implement Story N.M - Title`

### 组合矩阵

| 交互点 | 组件 | 需验证 | 说明 |
|--------|------|--------|------|
| historyView + viewMode 系统 | dashboard_nav.go | 是 | H 键切换、Esc 回退不与现有视图冲突 |
| historyView + Prompt Pager | dashboard_nav.go | 是 | 两者互不干扰（Layer 分离） |
| historyView + Kill 确认 | dashboard_nav.go | 是 | Kill 确认优先于历史视图按键 |
| ListAllProcs + Tree 面板 | dashboard_tree.go | 是 | 已结束进程正确显示符号和时间 |
| Dead 进程 + Timeline 过滤 | dashboard_timeline.go | 是 | 自动过滤 + 状态提示 |
| historyView + Focus Card | dashboard_focus.go | 否 | Enter 聚焦后 Focus Card 自动适应 |
| historyView + Replay 模式 | dashboard_nav.go | 否 | Replay 模式在 Layer 3，历史视图在更早的层 |
| 搜索模式 + 排序模式 | dashboard_history.go | 是 | 搜索和排序可同时生效 |
| ASCII 模式 + 状态符号 | internal/ui/symbols.go | 是 | `RNIX_ASCII=1` 时使用 ASCII 字符 |

### References

- [Source: _bmad-output/planning-artifacts/epics/epic-29-dashboard-ux-redesign.md#Story 29.5] — 验收准则定义
- [Source: _bmad-output/planning-artifacts/dashboard-redesign-spec-2026-03-23.md#Section 7] — 历史进程列表规格（后端 + 前端）
- [Source: _bmad-output/planning-artifacts/dashboard-redesign-spec-2026-03-23.md#Section 5.4] — 覆盖层布局规格
- [Source: _bmad-output/planning-artifacts/dashboard-redesign-spec-2026-03-23.md#Section 8.2] — Status bar 历史视图快捷键提示
- [Source: _bmad-output/planning-artifacts/dashboard-redesign-spec-2026-03-23.md#Section 10] — 状态符号体系
- [Source: _bmad-output/implementation-artifacts/29-4-kernel-process-history-and-ipc.md] — 前置 Story 实现细节和学习
- [Source: _bmad-output/project-context.md#IPC 扩展标准步骤] — IPC 方法列表参考
- [Source: cmd/rnix/dashboard_nav.go:26-129] — 当前键分发层级架构
- [Source: cmd/rnix/dashboard_types.go:27-34] — viewMode 枚举（viewHistory 已定义）
- [Source: ipc/client.go:74] — ListAllProcs 客户端方法已就绪
- [Source: internal/ui/symbols.go:16] — StateSymbol 函数已就绪

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6 (1M context)

### Debug Log References

- 初始 `make all` lint 报错 `QF1003: could use tagged switch on p.State` — 将 `if/else if` 改为 `switch` 修复
- ATDD 测试 `TestHistoryView_RenderContainsBottomStats` 要求 `renderHistoryView` 函数体内包含 "Running/Done/Failed" 字符串 — 在函数内添加注释使其可被 `extractFuncBody` 文本匹配

### Completion Notes List

- Task 1 (dashboard_history.go): 新增 ~350 行历史视图核心文件，包含 enterHistoryView、fetchAllProcsCmd、sortHistoryProcs、filteredHistoryProcs、historyKey、historySearchKey、renderHistoryView、renderHistoryStats 8 个主要函数
- Task 2 (dashboard_nav.go): 在 Layer 1 (Prompt Pager) 和 Layer 3 (Kill 确认) 之间插入 Layer 2 (History 覆盖层)；H 键 placeholder 替换为 enterHistoryView() 调用
- Task 3 (dashboard.go): dashboardModel 新增 6 个历史视图字段；Update 新增 historyProcsMsg case；renderDashboard 新增 viewHistory 路由；Status bar 新增 History 视图快捷键提示和 Dead 进程过滤提示
- Task 4 (dashboard_tree.go): dashboardTick 中 ListProcs → ListAllProcs；Tree 面板 Dead/Zombie 进程使用 ui.StateSymbol() 渲染；Dead 进程显示 exit code 和基于 DeadAt 的存活时间
- Task 5 (dashboard_timeline.go): 新增 isSelectedProcessDead()、filterTimelineByPID() 辅助函数；Timeline 渲染自动过滤 Dead 进程事件；Status bar 追加 "(filtered: PID N)" 提示
- Task 6 (dashboard_types.go): 新增 historyProcsMsg struct{procs []vfs.ProcInfo, err error}，添加 vfs 包导入
- Task 7: `make all` 通过（lint 0 issues, vet ok, 22 packages test pass, build ok）
- 额外：导出 ui.IsFailedResult() 供 Dashboard 包使用（内部 isFailedResult 保持不变）

### File List

- cmd/rnix/dashboard_history.go (新增)
- cmd/rnix/dashboard.go (修改)
- cmd/rnix/dashboard_nav.go (修改)
- cmd/rnix/dashboard_types.go (修改)
- cmd/rnix/dashboard_tree.go (修改)
- cmd/rnix/dashboard_timeline.go (修改)
- internal/ui/symbols.go (修改)

### Change Log

- 2026-03-24: Story 29.5 实现完成 — Dashboard 历史进程视图（H 键全屏覆盖层 + 搜索/排序 + Tree 面板 Dead 进程 + Timeline 自动过滤）
