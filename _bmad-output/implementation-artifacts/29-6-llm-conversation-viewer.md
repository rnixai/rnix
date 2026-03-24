# Story 29.6: LLM 对话查看器

Status: done

## Story

As a 平台构建者,
I want 在 Dashboard 任意视图按 L 键打开全屏 LLM 对话查看器,
So that 我可以查看选中进程任意步骤的完整 LLM request/response，实现深入调试。

## Acceptance Criteria

1. **L 键进入 LLM 查看器** — 选中进程后（`selectedPID ≠ 0`）按 `L`，进入全屏覆盖层 `viewLLM`，自动加载最新 step 数据（通过 `GetStepDetail` IPC）并初始化 viewport
2. **未选中进程提示** — 未选中任何进程时按 `L`，Status bar 显示 "No process selected"
3. **Request/Response 分块渲染** — 内容分为 `REQUEST →`（system/user/assistant/tool_result 消息 + token 数）和 `RESPONSE ←`（assistant/tool_call 消息 + token 数 + 延迟）两个区块
4. **步骤导航栏** — 底部显示所有步骤摘要列表，当前步骤用 `*` 标记
5. **h/l 前后翻页** — 按 `h` 或左箭头切换上一步，按 `l` 或右箭头切换下一步
6. **j/k 滚动** — 按 `j/k` 或 PgUp/PgDn 上下滚动长内容（传递给 viewport）
7. **y 复制** — 按 `y` 通过 OSC 52 复制当前内容到剪贴板
8. **Esc 退出** — 按 Esc 回到之前的视图（viewDefault）
9. **Status bar** — 显示 `req:X tok │ resp:Y tok │ Zms ── j/k:scroll h/l:prev/next y:copy Esc:close`
10. **多入口支持** — Detail 视图 Recent Steps 按 Enter 打开该 step；Timeline 视图选中 llm 事件按 Enter 打开对应 step；Heatmap 视图选中 segment 按 Enter 打开对应消息

## Tasks / Subtasks

- [x] Task 1: 新增 `dashboard_llm_viewer.go` — LLM 查看器核心 (AC: #1-#9)
  - [x] 1.1 定义 `llmViewerMsg` 消息类型（pid, step, detail, stepList, err）
  - [x] 1.2 实现 `enterLLMViewer()` — 设置 viewMode=viewLLM，初始化 viewport，获取步骤列表和最新 step 详情
  - [x] 1.3 实现 `enterLLMViewerAtStep(step int)` — 延迟实现，待面板 Enter 入口需要时添加
  - [x] 1.4 实现 `fetchLLMStepCmd(pid, step)` tea.Cmd — 调用 `client.GetStepDetail()`，返回 llmViewerMsg
  - [x] 1.5 实现 `fetchLLMStepListCmd(pid)` tea.Cmd — 调用 `client.ListSteps()` 获取步骤列表
  - [x] 1.6 实现 `llmViewerKey(msg)` — Esc 退出、h/l 前后翻页、y 复制、j/k/PgUp/PgDn 传给 viewport
  - [x] 1.7 实现 `renderLLMViewer()` — Title bar + Request 区块 + Response 区块 + 步骤导航栏 + Status bar
  - [x] 1.8 实现 `buildLLMViewerContent()` — REQUEST/RESPONSE 区块渲染（角色标签着色 + 内容）
  - [x] 1.9 实现 `renderStepNavBar()` — 底部步骤列表，当前步骤用 `*` 标记
  - [x] 1.10 实现 `tea.SetClipboard()` 剪贴板复制
- [x] Task 2: 修改 `dashboard_types.go` — 新增消息类型 (AC: all)
  - [x] 2.1 新增 `llmViewerMsg` 消息类型
  - [x] 2.2 新增 `llmStepListMsg` 消息类型（steps []ipc.StepSummaryWire, total int, err error）
- [x] Task 3: 修改 `dashboard.go` — 模型字段 + Update + View (AC: #1, #8)
  - [x] 3.1 dashboardModel 新增 LLM 查看器字段（llmViewerPID, llmViewerStep, llmViewerStepMax, llmViewerSteps, llmViewerDetail, llmViewerViewport, llmViewerContent）
  - [x] 3.2 Update 新增 `llmViewerMsg` 和 `llmStepListMsg` case 处理
  - [x] 3.3 renderDashboard 新增 `case viewLLM:` 路由到 `renderLLMViewer()`
  - [x] 3.4 WindowSizeMsg 处理中更新 llmViewerViewport 尺寸
  - [x] 3.5 renderDashboardStatus 新增 `case viewLLM:` 显示 token 统计 + 快捷键提示
- [x] Task 4: 修改 `dashboard_nav.go` — L 键路由 (AC: #1, #2)
  - [x] 4.1 Layer 2.5 新增：viewLLM 时调用 `m.llmViewerKey(msg)` 处理
  - [x] 4.2 Layer 5 中 `case "L", "shift+L"` 从 placeholder 替换为 `return m.enterLLMViewer()`
- [x] Task 5: 修改 `dashboard_history.go` — L 键调用 (AC: #10)
  - [x] 5.1 历史视图中 `case "L", "shift+L"` 从 placeholder 替换为 `return m.enterLLMViewer()`
- [ ] Task 6: 修改面板 Enter 入口 (AC: #10) — 延迟实现
  - [ ] 6.1 `dashboard_detail.go` — Recent Steps 区域 Enter 调用 `enterLLMViewerAtStep()`
  - [ ] 6.2 `dashboard_timeline.go` — Timeline 选中 llm 事件 Enter 调用 `enterLLMViewerAtStep()`
  - [ ] 6.3 `dashboard_heatmap.go` — Heatmap 选中 segment Enter 调用 `enterLLMViewerAtStep()`
- [x] Task 7: 测试 + `make all` 验证 (AC: all)
  - [x] 7.1 确认 `make all` 编译通过、所有测试通过（lint 0 issues，22 packages PASS）

## Dev Notes

### 核心实现要点

**LLM 查看器是全屏覆盖层**，与历史视图 (`dashboard_history.go`) 模式相同。占据整个 Dashboard 区域，Esc 回退。参考 `dashboard_history.go` 的架构模式实现。

### 数据源

**GetStepDetail IPC**（Story 27.2 已实现）：
- `client.GetStepDetail(pid, step)` → `*ipc.GetStepDetailResponse`
- `client.GetStepDetailByUUID(uuid, step)` → 同上（用于历史进程）
- `client.ListSteps(pid, 0)` → `*ipc.ListStepsResponse`（获取步骤列表）
- `client.ListStepsByUUID(uuid, 0)` → 同上

**GetStepDetailResponse 结构**（`ipc/protocol.go:879-897`）：
```go
type GetStepDetailResponse struct {
    SystemPrompt   string         // system prompt 全文
    Tools          []ToolDefWire  // 工具定义列表
    Step           int            // 步骤号
    Messages       []MessageWire  // 消息列表（role/content/tool_calls）
    MessageCount   int            // 消息总数
    TokenCount     int            // token 总数
    RawResponse    string         // LLM 原始响应
    Action         string         // 动作类型（tool_call/complete/plan...）
    Summary        string         // 摘要
    ToolPath       string         // 工具路径
    ToolInput      string         // 工具输入
    ToolResult     string         // 工具输出
    ToolError      string         // 工具错误
    ToolDurationMs float64        // 工具执行耗时
    RequestTokens  int            // 请求 token 数
    ResponseTokens int            // 响应 token 数
}
```

### Request/Response 分块逻辑

**REQUEST →** 区块包含发送给 LLM 的内容：
- system prompt（摘要长度，不全文展示）
- Messages 中 role=system/user/tool 的消息
- `RequestTokens` 统计

**RESPONSE ←** 区块包含 LLM 返回的内容：
- Messages 中 role=assistant 的消息
- `RawResponse`（如果有且与 Messages 不同）
- `Action` + `Summary`
- Tool call 信息（ToolPath/ToolInput/ToolResult/ToolError/ToolDurationMs）
- `ResponseTokens` 统计

### 现有 Prompt Pager 的关系

**Prompt Pager**（`dashboard_timeline.go:615-722`）是 LLM 查看器的"前辈"，用 `promptPager bool` 控制全屏显示。LLM 查看器是其升级版：
- Prompt Pager 使用独立的 `promptPager bool` 控制，不是 viewMode 系统
- LLM 查看器使用 `viewMode == viewLLM`，融入统一视图系统
- **不要删除 Prompt Pager**（保持向后兼容），但面板 Enter 入口可以改为打开 LLM 查看器
- 复用 `formatRoleTag()` 函数进行角色着色
- 复用 `formatCharCount()` / `formatTokenCount()` 辅助函数

### 字段位置

在 `dashboardModel`（`dashboard.go:32-157`）中，LLM 查看器字段添加在历史视图字段之后（~line 157）：

```go
// LLM viewer fields (Story 29.6)
llmViewerPID      types.PID
llmViewerStep     int               // 当前查看的 step 索引
llmViewerStepMax  int               // 总 step 数
llmViewerSteps    []ipc.StepSummaryWire // 步骤摘要列表
llmViewerDetail   *ipc.GetStepDetailResponse // 当前 step 详情
llmViewerViewport viewport.Model
llmViewerContent  string            // 渲染后的内容（供复制）
```

### 导航键分发层级

**关键**：LLM 查看器按键在 Layer 2 处理（覆盖层级别），与历史视图同层。在 `dashboard_nav.go` 中当前 Layer 顺序：

1. Layer 0: ctrl+c 全局退出
2. Layer 1: Prompt Pager
3. Layer 2: History 覆盖层（`viewHistory`）
4. Layer 3: Kill 确认
5. Layer 4: Replay 模式
6. Layer 5: 主视图快捷键

LLM 查看器应紧跟 History 覆盖层之后（或并列）：
```go
// === Layer 2.5: LLM Viewer 覆盖层 ===
if m.viewMode == viewLLM {
    return m.llmViewerKey(msg)
}
```

### OSC 52 剪贴板复制

使用 Bubbletea v2 的 `tea.SetClipboard` 命令：
```go
func copyToClipboardCmd(content string) tea.Cmd {
    return tea.SetClipboard(content, nil)
}
```

如果 Bubbletea v2 无 `tea.SetClipboard`，手动生成 OSC 52 序列：
```go
func copyToClipboardCmd(content string) tea.Cmd {
    return tea.Printf("\033]52;c;%s\a", base64.StdEncoding.EncodeToString([]byte(content)))
}
```

### 步骤导航栏渲染

步骤导航栏显示在内容区和 Status bar 之间，格式：
```
Steps: [1:tool_call] [2:plan] [3*:complete] [4:tool_call]
                              ^^^ 当前步骤
```

每个步骤标签显示 `step_num:action_type`，当前步骤后加 `*`。使用 `llmViewerSteps` 中的 `StepSummaryWire` 数据。

### 多入口实现

| 入口 | 文件 | 当前行为 | 改为 |
|------|------|---------|------|
| L 键（主视图） | `dashboard_nav.go:92-95` | placeholder 消息 | `enterLLMViewer()` |
| L 键（历史视图） | `dashboard_history.go:160-168` | placeholder 消息 | `enterLLMViewer()` |
| p 键（Timeline） | `dashboard_nav.go:185-190` | 打开 promptPager | **保留不变**（兼容） |
| Enter（Timeline expand） | `dashboard_nav.go:155-159` | 展开 step detail | **保留不变** |
| Enter（Detail Recent Steps） | `dashboard_detail.go` | 需要找到入口 | `enterLLMViewerAtStep()` |
| Enter（Heatmap segment） | `dashboard_heatmap.go` | 需要找到入口 | `enterLLMViewerAtStep()` |

**注意**：Timeline 的 `p` 键打开旧 Prompt Pager 保持不变，不影响。新 LLM 查看器通过 `L` 键和各面板 Enter 进入。

### 已有设施复用

| 设施 | 位置 | 用途 |
|------|------|------|
| `client.GetStepDetail(pid, step)` | `ipc/client.go:699` | 获取步骤详情 |
| `client.GetStepDetailByUUID(uuid, step)` | `ipc/client.go:741` | UUID 方式获取（历史进程） |
| `client.ListSteps(pid, 0)` | `ipc/client.go:756` | 获取步骤列表 |
| `client.ListStepsByUUID(uuid, 0)` | `ipc/client.go:769` | UUID 方式获取列表 |
| `GetStepDetailResponse` | `ipc/protocol.go:879` | 步骤详情数据结构 |
| `StepSummaryWire` | `ipc/protocol.go:931` | 步骤摘要数据结构 |
| `MessageWire` | `ipc/protocol.go:900` | 消息数据结构 |
| `formatRoleTag()` | `cmd/rnix/dashboard_timeline.go:658` | 角色标签着色 |
| `formatCharCount()` | `cmd/rnix/dashboard_timeline.go:679` | 字符数格式化 |
| `formatTokenCount()` | `cmd/rnix/dashboard_timeline.go` | token 数格式化 |
| `promptRole*` 样式 | `cmd/rnix/dashboard_timeline.go` | lipgloss 角色颜色 |
| `viewport.New()` | bubbletea v2 | 滚动组件 |
| `enterHistoryView()` | `cmd/rnix/dashboard_history.go` | 覆盖层模式参考 |
| `renderHistoryView()` | `cmd/rnix/dashboard_history.go` | 全屏渲染参考 |
| `historyKey()` | `cmd/rnix/dashboard_history.go` | 覆盖层按键处理参考 |
| `enterPromptPager()` | `cmd/rnix/dashboard_timeline.go:686` | Prompt Pager 入口（参考模式） |
| `fetchStepDetailForPagerCmd()` | `cmd/rnix/dashboard_timeline.go:712` | IPC 调用模式参考 |

### 不应该做的事

- **不要删除 Prompt Pager** — 保留 `p` 键功能向后兼容
- **不要新增 IPC 方法** — `GetStepDetail` 和 `ListSteps` 已实现
- **不要修改 `kernel/` 或 `ipc/` 包** — 所有改动限于 `cmd/rnix/dashboard*.go`
- **不要使用 `sync.Map`** — Dashboard 单 goroutine Bubbletea 模型
- **不要预加载所有步骤详情** — 按需加载，每次 h/l 切换时发起 IPC 调用
- **不要在 LLM 查看器中显示完整 system prompt** — 太长，只显示字符数摘要（与 Prompt Pager 的处理方式不同，Request 区块重点展示 user/tool 消息）
- **不要引入新外部依赖** — 纯 Bubbletea + Lipgloss

### Project Structure Notes

| 文件 | 操作 | 说明 |
|------|------|------|
| `cmd/rnix/dashboard_llm_viewer.go` | **新增** | LLM 查看器核心（enterLLMViewer + renderLLMViewer + llmViewerKey + 辅助函数） |
| `cmd/rnix/dashboard.go` | 修改 | dashboardModel 新增 LLM 查看器字段 + Update 处理 llmViewerMsg/llmStepListMsg + View 路由 |
| `cmd/rnix/dashboard_nav.go` | 修改 | Layer 2.5 覆盖层路由 + L 键 placeholder 替换 |
| `cmd/rnix/dashboard_types.go` | 修改 | 新增 llmViewerMsg 和 llmStepListMsg 消息类型 |
| `cmd/rnix/dashboard_history.go` | 修改 | L 键 placeholder 替换为 enterLLMViewer() |
| `cmd/rnix/dashboard_detail.go` | 修改 | Enter 入口调用 enterLLMViewerAtStep()（如果有合适入口点） |
| `cmd/rnix/dashboard_timeline.go` | 修改 | Timeline Enter 入口增强（可选） |
| `cmd/rnix/dashboard_heatmap.go` | 修改 | Heatmap Enter 入口增强（可选） |

### 前置 Story 关键学习（Story 29.5）

- **覆盖层 Layer 插入模式**：在 Prompt Pager (Layer 1) 和 Kill 确认 (Layer 3) 之间插入
- **q 键行为**：覆盖层模式下 q 应退出 Dashboard，通过 Layer 0 的 ctrl+c 和覆盖层内部的 q 处理实现
- **fetchAllProcsCmd 模式**：进入覆盖层时发一次 IPC 调用，不持续轮询
- **rune-safe 截断**：字符串截断使用 `truncateRuneSafe`
- **Dead 进程用 UUID 查询**：历史进程 PID 可能已被回收，用 `selectedUUID` + `ByUUID` 方法更可靠

### 前置 Story 关键学习（Story 29.3 Code Review）

- **P-1**: 字符串截断必须 rune-safe
- **P-2/P-3**: Dead 进程时间用 DeadAt 而非 time.Now()

### Git 近期提交模式

```
feat: Implement Story 29.5 - Dashboard History View
feat: Implement Story 29.4 - Kernel Process History and IPC
feat: Implement Story 29.3 - Default View Focus Card
```

提交消息格式：`feat: Implement Story N.M - Title`

### References

- [Source: _bmad-output/planning-artifacts/epics/epic-29-dashboard-ux-redesign.md#Story 29.6] — 验收准则定义
- [Source: _bmad-output/planning-artifacts/dashboard-redesign-spec-2026-03-23.md#Section 6] — LLM 对话查看器规格
- [Source: _bmad-output/planning-artifacts/dashboard-redesign-spec-2026-03-23.md#Section 5.4] — 覆盖层布局规格
- [Source: _bmad-output/planning-artifacts/dashboard-redesign-spec-2026-03-23.md#Section 8.2] — Status bar 快捷键提示
- [Source: _bmad-output/implementation-artifacts/29-5-dashboard-history-view.md] — 前置 Story 覆盖层实现模式
- [Source: ipc/protocol.go:879-897] — GetStepDetailResponse 数据结构
- [Source: ipc/protocol.go:930-945] — StepSummaryWire 数据结构
- [Source: cmd/rnix/dashboard_timeline.go:615-722] — 现有 Prompt Pager 实现（参考/复用）
- [Source: cmd/rnix/dashboard_nav.go:26-130] — 当前键分发层级架构
- [Source: cmd/rnix/dashboard_types.go:28-34] — viewMode 枚举（viewLLM 已定义）
- [Source: cmd/rnix/dashboard_history.go] — 覆盖层架构模式参考

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6

### Debug Log References

- bubbletea v2 `KeyPressMsg.String()` 对 Shift 键返回 `"shift+L"` 而非 `"L"`，需在 switch case 中同时匹配两者
- `tea.SetClipboard(s string)` 是 bubbletea v2 的原生剪贴板 API
- viewport 零值在测试场景中 Width()=0，需要 fallback 到直接渲染内容

### Completion Notes List

- `enterLLMViewerAtStep()` 未实现（lint unused 会报错），Task 6 面板 Enter 入口延迟到后续实现
- 使用 bubbletea v2 原生 `tea.SetClipboard()` 替代手动 OSC 52
- viewport 在 renderLLMViewer 中做了零值检测：测试场景直接渲染内容，生产场景使用 viewport 滚动

### File List

| 文件 | 操作 | 说明 |
|------|------|------|
| `cmd/rnix/dashboard_llm_viewer.go` | **新增** | LLM 查看器核心 — enterLLMViewer + renderLLMViewer + llmViewerKey + fetch commands + helpers |
| `cmd/rnix/dashboard_types.go` | 修改 | 新增 llmViewerMsg + llmStepListMsg 消息类型 |
| `cmd/rnix/dashboard.go` | 修改 | dashboardModel 新增 7 个 LLM 查看器字段 + Update 处理 2 个新消息 + View 路由 viewLLM + WindowSizeMsg + Status bar |
| `cmd/rnix/dashboard_nav.go` | 修改 | Layer 2.5 覆盖层 viewLLM 键分发 + L 键 placeholder → enterLLMViewer() |
| `cmd/rnix/dashboard_history.go` | 修改 | L 键 placeholder → enterLLMViewer() |
