# Dashboard 信息架构重设计 — 实现规格

**日期:** 2026-03-23（最后更新: 2026-03-25）
**输入:** design-thinking-2026-03-23.md
**范围:** `cmd/rnix/dashboard.go` 及相关模块的重构
**状态:** ✅ 全部实现完成（Epic 29 所有 Story Done + Post-epic 增强）

---

## 1. 现状分析

### 1.1 当前架构

- **文件**: 已拆分为 15+ 独立文件（原 `dashboard.go` ~4000 行 → 模块化架构）
- **模型**: `dashboardModel` 结构体，包含 ~80 个字段（含 viewMode、LLM viewer、History 等新增状态）
- **布局**: 默认视图 = 左 Tree (40%) + 右上 Timeline + 右下 Focus Card (2×3)；支持展开/覆盖层视图
- **导航**: 数字键 1-8 直达 + Tab/Shift-Tab 循环 + Esc 回退 + 全局 L/H 覆盖层
- **面板**: 8 个 `paneType` 枚举 (Tree/Timeline/Heatmap/Detail/Intent/Security/Trace/Eval)
- **进程数据**: `ListAllProcs()` 返回存活+历史进程，Dead 进程快照持久化到 `.rnix/data/steps/<uuid>/proc-info.json`，daemon 重启后通过 `LoadHistory()` 恢复
- **观察数据**: 每个进程完整数据持久化（steps.jsonl + events.jsonl + ctx-profile.json + process-meta.json + proc-info.json）
- **Prompt 查看**: `promptPager` 全屏覆盖层（Timeline `p` 键入口）
- **帮助系统**: `?` 全屏帮助覆盖层 + 上下文感知状态栏提示

### 1.2 关键痛点（Design Thinking 结论）

| 痛点 | 根因 | 解决方案（已实现） |
|------|------|------|
| Tab 循环地狱 | `(activePane+1) % 8` 线性循环，7 次才能到 Eval | ✅ 数字键 1-8 直达 + Shift-Tab 反向 |
| 进程历史丢失 | `ListProcs()` 只返回存活进程 | ✅ ProcessHistory + ListAllProcs + 磁盘持久化 |
| 信息过载 | 右下 6 面板共享同一区域，无优先级分层 | ✅ Focus Card 2×3 信息摘要 + 分层发现模型 |
| LLM 交互不可见 | 无法查看完整的 LLM request/response | ✅ L 键全屏 LLM 对话查看器 |

---

## 2. 目标架构

### 2.1 视图体系

```
默认视图    = 左 Tree + 右上 Timeline + 右下 Focus Card (2×3 摘要卡片)
1-8 视图    = 对应面板展开（数字键直达）
L 视图      = LLM 对话查看器（全屏覆盖层）
H 视图      = 历史进程列表（全屏覆盖层）
```

### 2.2 导航模型变更

| 操作 | 当前 | 目标 |
|------|------|------|
| 面板切换 | `Tab` 循环 0→7 | `Tab` 循环 + `Shift-Tab` 反向 + **数字键 1-8 直达** |
| 回退 | 无 | `Esc` 回到默认视图 |
| LLM 查看 | `p` 键（仅 Timeline） | **`L` 全局快捷键**（任意视图） |
| 历史列表 | 无 | **`H` 全局快捷键** |
| 面板内导航 | `j/k` | `j/k`（不变）+ **`PgDn/PgUp` 翻页** + **`g/G` 跳顶/底** |

---

## 3. 组件拆分规格

### 3.1 文件拆分计划

当前 Dashboard 文件架构（已实现）：

```
cmd/rnix/
├── dashboard.go              # 主模型、Init/Update/View、布局编排
├── dashboard_tree.go         # Tree 面板渲染 + Tree 扩展视图
├── dashboard_timeline.go     # Timeline 面板渲染 + Step 导航 + 系统调用混合视图
├── dashboard_heatmap.go      # Heatmap 面板渲染
├── dashboard_detail.go       # Detail 面板渲染
├── dashboard_intent.go       # Intent DAG 面板渲染
├── dashboard_security.go     # Security 面板渲染
├── dashboard_trace.go        # Trace 面板渲染
├── dashboard_eval.go         # Eval 面板渲染
├── dashboard_focus.go        # Focus Card（默认视图右下 2×3 卡片）
├── dashboard_llm_viewer.go   # LLM 对话查看器（全屏覆盖层）
├── dashboard_history.go      # 历史进程列表（全屏覆盖层）
├── dashboard_nav.go          # 统一导航逻辑（6 层键绑定分发）
├── dashboard_help.go         # ? 帮助覆盖层（全屏快捷键参考卡）
└── dashboard_types.go        # 所有 dashboard 相关的类型定义
```

### 3.2 模型重构

#### 3.2.1 视图状态枚举

```go
// dashboard_types.go

type viewMode int

const (
    viewDefault  viewMode = iota // 默认：Tree + Timeline + Focus Card
    viewExpanded                 // 数字键展开某面板
    viewLLM                     // L：全屏 LLM 对话查看器
    viewHistory                 // H：全屏历史进程列表
)
```

#### 3.2.2 dashboardModel 新增字段

```go
// 在现有 dashboardModel 中新增：

// 视图模式
viewMode     viewMode  // 当前视图模式
expandedPane paneType  // viewExpanded 模式下展开的面板

// LLM 对话查看器 (L 视图)
llmViewerPID      types.PID
llmViewerStep     int      // 当前查看的 step 索引
llmViewerStepMax  int      // 总 step 数
llmViewerContent  string   // 渲染后的内容
llmViewerViewport viewport.Model
llmViewerReqTok   int      // request token count
llmViewerRespTok  int      // response token count
llmViewerLatency  string   // 延迟

// 历史进程列表 (H 视图)
historyProcs       []vfs.ProcInfo // 包含 Dead 进程的完整列表
historyCursor      int
historyScrollOffset int
historySortMode    int  // 0=time, 1=name, 2=pid
historySearchQuery string
historySearchMode  bool

// Focus Card（默认视图右下）
focusCardData *focusCardState
```

#### 3.2.3 Focus Card 状态

```go
// dashboard_focus.go

type focusCardState struct {
    // 从选中进程的各数据源聚合
    pid       types.PID
    agent     string
    state     types.ProcessState
    elapsed   time.Duration
    isHistory bool // 已结束进程的历史快照

    // Tokens 卡片
    tokensUsed   int
    tokensBudget int
    tokenRate    float64 // tok/s
    steps        int

    // Context 卡片
    ctxSysPct  float64
    ctxUserPct float64
    ctxAsstPct float64
    ctxToolPct float64

    // Status 卡片
    skills  []string
    devices []string
    ppid    types.PID

    // Intent 卡片 (摘要)
    intentTasks []intentMiniTask

    // Trace 卡片 (摘要)
    traceSpans int
    traceAvgMs int64
    traceErrs  int

    // Alerts 卡片
    alerts []string
}

type intentMiniTask struct {
    name  string
    state string // "✓", "●", "○"
}
```

---

## 4. 导航系统规格

### 4.1 键绑定分发表

```go
// dashboard_nav.go

func (m dashboardModel) dashboardKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
    key := msg.String()

    // === 全局快捷键（任何视图模式下都生效）===
    switch key {
    case "q", "ctrl+c":
        return m, tea.Quit
    }

    // === 覆盖层视图的专属按键 ===
    if m.viewMode == viewLLM {
        return m.llmViewerKey(msg)
    }
    if m.viewMode == viewHistory {
        return m.historyKey(msg)
    }

    // === 主视图全局快捷键 ===
    switch key {
    case "L":
        return m.enterLLMViewer()
    case "H":
        return m.enterHistoryView()
    case "esc":
        if m.viewMode == viewExpanded {
            m.viewMode = viewDefault
            return m, nil
        }
    case "tab":
        m.activePane = (m.activePane + 1) % 8
        if m.viewMode == viewDefault {
            m.viewMode = viewExpanded
            m.expandedPane = m.activePane
        } else {
            m.expandedPane = m.activePane
        }
        return m, nil
    case "shift+tab":
        m.activePane = (m.activePane + 7) % 8
        m.viewMode = viewExpanded
        m.expandedPane = m.activePane
        return m, nil
    case "1":
        return m.jumpToPane(paneTree)
    case "2":
        return m.jumpToPane(paneTimeline)
    case "3":
        return m.jumpToPane(paneHeatmap)
    case "4":
        return m.jumpToPane(paneDetail)
    case "5":
        return m.jumpToPane(paneIntent)
    case "6":
        return m.jumpToPane(paneSecurity)
    case "7":
        return m.jumpToPane(paneTrace)
    case "8":
        return m.jumpToPane(paneEval)
    }

    // === 面板内按键分发（与当前逻辑相同）===
    return m.dispatchPaneKey(msg)
}

func (m dashboardModel) jumpToPane(p paneType) (tea.Model, tea.Cmd) {
    if m.viewMode == viewExpanded && m.expandedPane == p {
        // 再按一次同数字键 = 回到默认视图
        m.viewMode = viewDefault
    } else {
        m.viewMode = viewExpanded
        m.expandedPane = p
    }
    m.activePane = p
    return m, nil
}
```

### 4.2 Esc 行为栈

```
覆盖层 (LLM/History) → Esc → 回到上一个 viewMode
展开视图 (1-8)        → Esc → 回到默认视图
默认视图              → Esc → 无操作（或退出 detail 焦点）
确认对话框            → Esc → 取消确认
```

---

## 5. 布局引擎规格

### 5.1 默认视图布局

```
┌─────────────── w ─────────────────┐
│ Title Bar                    (1行) │
├──────────┬────────────────────────┤
│          │ Timeline    (h/2 行)   │
│ Tree     ├────────────────────────┤  h = height - 4
│ (40%w)   │ Focus Card  (h/2 行)   │
│          │ [2×3 grid]             │
├──────────┴────────────────────────┤
│ Status Bar                  (1行) │
└───────────────────────────────────┘
```

### 5.2 展开视图布局 (数字键 1)

```
┌─────────────── w ─────────────────┐
│ Title Bar (标签高亮 1:Tree*)       │
├───────────────────────────────────┤
│                                    │
│ Tree Expanded (全宽全高)           │  h = height - 4
│ 统一进程表 (含历史进程)            │
│                                    │
├───────────────────────────────────┤
│ Status Bar                        │
└───────────────────────────────────┘
```

### 5.3 展开视图布局 (数字键 2-8)

```
┌─────────────── w ─────────────────┐
│ Title Bar (标签高亮 N:Name*)       │
├──────────┬────────────────────────┤
│          │                         │
│ Tree     │ Expanded Pane (全高)    │  h = height - 4
│ (40%w)   │                         │
│          │                         │
├──────────┴────────────────────────┤
│ Status Bar                        │
└───────────────────────────────────┘
```

### 5.4 覆盖层布局 (L / H)

```
┌─────────────── w ─────────────────┐
│ Overlay Title Bar                  │
├───────────────────────────────────┤
│                                    │
│ 全屏内容 (viewport 可滚动)        │  h = height - 4
│                                    │
├───────────────────────────────────┤
│ Overlay Status Bar (快捷键提示)    │
└───────────────────────────────────┘
```

### 5.5 Focus Card 2×3 网格布局

```
rightWidth = w - treeWidth
cardW = rightWidth / 3
cardH = focusHeight / 2

┌──────────┬──────────┬──────────┐
│ Tokens   │ Context  │ Status   │  row 0
├──────────┼──────────┼──────────┤
│ Intent   │ Trace    │ Alerts   │  row 1
└──────────┴──────────┴──────────┘
```

每个卡片使用 lipgloss 边框，标题居左，内容区 `cardH - 3` 行。

---

## 6. LLM 对话查看器规格

### 6.1 数据源

复用现有 `ipc.GetStepDetail()` API，该 API 已返回 `StepDetailWire`：

```go
type StepDetailWire struct {
    StepNumber  int
    ActionType  string
    ActionName  string
    Input       string // 可作为 request 展示
    Output      string // 可作为 response 展示
    DurationMs  int64
    TokensUsed  int
    Error       string
}
```

### 6.2 LLM 查看器模型

```go
// dashboard_llm_viewer.go

type llmViewerMsg struct {
    pid    types.PID
    step   int
    detail *ipc.GetStepDetailResponse
    err    error
}

func (m dashboardModel) enterLLMViewer() (tea.Model, tea.Cmd) {
    if m.selectedPID == 0 {
        m.statusMsg = "No process selected"
        m.statusMsgTTL = statusMsgDefaultTTL
        return m, nil
    }
    m.viewMode = viewLLM
    m.llmViewerPID = m.selectedPID
    m.llmViewerStep = -1 // 加载最新步骤
    // 初始化 viewport
    m.llmViewerViewport = viewport.New()
    m.llmViewerViewport.SetWidth(m.width)
    m.llmViewerViewport.SetHeight(max(m.height-4, 1))
    return m, fetchLLMStepCmd(m.selectedPID, -1) // -1 = latest
}

func (m dashboardModel) llmViewerKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
    key := msg.String()
    switch key {
    case "esc":
        m.viewMode = viewDefault // 回到之前的视图
        return m, nil
    case "h", "left":
        // 上一步
        if m.llmViewerStep > 0 {
            m.llmViewerStep--
            return m, fetchLLMStepCmd(m.llmViewerPID, m.llmViewerStep)
        }
    case "l", "right":
        // 下一步
        if m.llmViewerStep < m.llmViewerStepMax-1 {
            m.llmViewerStep++
            return m, fetchLLMStepCmd(m.llmViewerPID, m.llmViewerStep)
        }
    case "y":
        // 复制内容到剪贴板 (使用 OSC 52)
        return m, copyToClipboardCmd(m.llmViewerContent)
    default:
        // 传递给 viewport 处理 j/k/pgup/pgdn
        var cmd tea.Cmd
        m.llmViewerViewport, cmd = m.llmViewerViewport.Update(msg)
        return m, cmd
    }
    return m, nil
}
```

### 6.3 渲染格式

```go
func (m dashboardModel) renderLLMViewer() string {
    // Title bar
    title := fmt.Sprintf(
        " LLM CONVERSATION ━━━ PID %d %s ━━━ Step [%d/%d]",
        m.llmViewerPID, agent, m.llmViewerStep+1, m.llmViewerStepMax,
    )

    // Request block
    reqBlock := renderLLMBlock("REQUEST →", model, reqTokens, requestContent)

    // Response block
    respBlock := renderLLMBlock("RESPONSE ←", model, respTokens, responseContent)

    // Step navigator bar
    navBar := renderStepNavBar(m.llmViewerStep, m.llmViewerStepMax, steps)

    // Status bar
    statusBar := fmt.Sprintf(
        " req:%s │ resp:%s │ %s ── j/k:scroll  h/l:prev/next  y:copy  Esc:close",
        formatTokens(m.llmViewerReqTok),
        formatTokens(m.llmViewerRespTok),
        m.llmViewerLatency,
    )

    return lipgloss.JoinVertical(lipgloss.Left, title, viewport, navBar, statusBar)
}
```

### 6.4 入口点

| 触发来源 | 行为 |
|----------|------|
| 任意视图按 `L` | 打开选中进程的最新 step |
| Detail 视图 Recent Steps 按 `Enter` | 打开该 step |
| Timeline 视图选中 llm 事件按 `Enter` | 打开该事件对应 step |
| Heatmap 视图选中 segment 按 `Enter` | 打开该 segment 对应消息 |

---

## 7. 历史进程列表规格

### 7.1 后端变更：进程历史保留

**核心变更**: 在 kernel 中增加已结束进程的保留机制。

```go
// kernel/kernel.go

// ProcessHistory 保留已结束进程的 ProcInfo 快照
type ProcessHistory struct {
    mu      sync.RWMutex
    entries []vfs.ProcInfo // Dead 进程的快照，按时间排序
    maxSize int            // 最大保留数量（默认 1000）
}

// 在 reaper 清除进程前，调用 history.Add(procInfo)
func (h *ProcessHistory) Add(info vfs.ProcInfo) {
    h.mu.Lock()
    defer h.mu.Unlock()
    h.entries = append(h.entries, info)
    if len(h.entries) > h.maxSize {
        h.entries = h.entries[1:] // FIFO 淘汰
    }
}

// ListAllProcs 返回存活+历史进程的合并列表
func (k *Kernel) ListAllProcs() []vfs.ProcInfo {
    alive := k.ListProcs()
    dead := k.history.List()
    // 合并，按 CreatedAt 排序
    all := append(alive, dead...)
    sort.Slice(all, func(i, j int) bool {
        return all[i].CreatedAt.Before(all[j].CreatedAt)
    })
    return all
}
```

### 7.2 IPC 新增方法

```go
// ipc/wire.go — 新增
// Method: "list_all_procs"
// Request: {} (无参数)
// Response: { procs: []ProcInfoWire }

// ipc/client.go
func (c *Client) ListAllProcs() ([]vfs.ProcInfo, error) {
    // 发送 {"method": "list_all_procs"} 请求
}

// ipc/server.go — handleListAllProcs
```

### 7.3 历史视图模型

```go
// dashboard_history.go

func (m dashboardModel) enterHistoryView() (tea.Model, tea.Cmd) {
    m.viewMode = viewHistory
    m.historyCursor = 0
    m.historyScrollOffset = 0
    return m, fetchAllProcsCmd()
}

func (m dashboardModel) historyKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
    key := msg.String()
    switch key {
    case "esc":
        m.viewMode = viewDefault
        return m, nil
    case "j", "down":
        if m.historyCursor < len(m.historyProcs)-1 {
            m.historyCursor++
        }
    case "k", "up":
        if m.historyCursor > 0 {
            m.historyCursor--
        }
    case "enter":
        // 选中进程并回到默认视图聚焦
        proc := m.historyProcs[m.historyCursor]
        m.selectedPID = proc.PID
        m.selectedUUID = proc.UUID
        m.viewMode = viewDefault
        return m.handlePIDChange()
    case "L":
        // 直接打开该进程的 LLM 查看器
        proc := m.historyProcs[m.historyCursor]
        m.selectedPID = proc.PID
        m.selectedUUID = proc.UUID
        return m.enterLLMViewer()
    case "/":
        m.historySearchMode = true
    case "1":
        m.historySortMode = 0 // by time
    case "2":
        m.historySortMode = 1 // by name
    case "3":
        m.historySortMode = 2 // by pid
    }
    return m, nil
}
```

### 7.4 渲染

渲染全屏表格，列：`PID | ST | AGENT | MODEL | TOKENS | CREATED | ELAPSED | EXIT | REASON`

状态符号：
```go
func stateSymbol(s types.ProcessState) string {
    switch s {
    case types.StateRunning:  return "●"
    case types.StateCreated:  return "○"
    case types.StateDead:     return "✓" // exit 0
    case types.StateZombie:   return "⏸"
    default:                  return "?"
    }
}

// 对于 exit code != 0 的 Dead 进程显示 "✕"
func stateSymbolWithExit(info vfs.ProcInfo) string {
    if info.State == types.StateDead {
        // 需要从 Result 解析 exit code
        if parseExitCode(info.Result) != 0 {
            return "✕"
        }
        return "✓"
    }
    return stateSymbol(info.State)
}
```

---

## 8. Title Bar 标签系统规格

### 8.1 渲染逻辑

```go
func (m dashboardModel) renderDashboardTitle() string {
    var b strings.Builder
    b.WriteString(" Rnix Dashboard ──── ")

    paneNames := []struct {
        key  string
        name string
        pane paneType
    }{
        {"1", "Tree", paneTree},
        {"2", "Time", paneTimeline},
        {"3", "Heat", paneHeatmap},
        {"4", "Detail", paneDetail},
        {"5", "Intent", paneIntent},
        {"6", "Sec", paneSecurity},
        {"7", "Trace", paneTrace},
        {"8", "Eval", paneEval},
    }

    for _, p := range paneNames {
        if m.viewMode == viewExpanded && m.expandedPane == p.pane {
            // 激活态：无方括号 + 星号
            fmt.Fprintf(&b, " %s:%s*", p.key, p.name)
        } else {
            // 非激活态：方括号包裹
            fmt.Fprintf(&b, " [%s]%s", p.key, p.name)
        }
    }

    return b.String()
}
```

### 8.2 Status Bar 规格（分层发现模型 — 已实现）

> **详细规范**: [ux-statusbar-redesign.md](../ux-statusbar-redesign.md)

**设计原则**: 分层发现 — 状态栏只回答"我现在最可能需要按什么？"（5-6 个核心键），帮助覆盖层（`?`）回答"所有能按的键是什么？"

**渲染格式**:
- 按键部分：`ColorAgent (#5B9BD5)` + Bold
- 描述部分：`ColorMuted (#666666)`
- 按键与描述紧凑（无空格），hints 之间双空格，退出区前 4 空格
- 宽度预算 ≤ 55 字符（80/60 列终端安全）

```go
func (m dashboardModel) renderDashboardStatus() string {
    // 1. statusMsg 优先（TTL=4 ticks），替换全部 hints 仅保留 q quit
    // 2. 录制指示 ●REC 在最左侧（ColorError 红色）
    // 3. 根据 viewMode + activePane 选择上下文提示（paneHints()）
    // 4. 组装：缩进 + rec + hints + 4空格 + exitHint
}
```

**各视图模式的状态栏内容**:

| 视图模式 | 提示内容 |
|----------|---------|
| viewDefault | `j/k nav  z expand  f filter  H hist  ? help    q quit` |
| viewExpanded (Timeline Step) | `j/k nav  v detail  p prompt  s syscall  ? help    q quit` |
| viewExpanded (Timeline Syscall) | `j/k nav  Enter detail  s step  f filter  ? help    q quit` |
| viewExpanded (其他面板) | `j/k nav  Enter select  z restore  ? help    q quit` |
| timelineFilterMode | `l LLM  t Tool  i IPC  v VFS  a All    f/Esc done` |
| viewHistory | `j/k nav  Enter focus  L llm  / search  ? help    Esc back` |
| viewLLM | `j/k scroll  h/l step  y copy  ? help    Esc close` |
| Replay 模式 | `Space play  ,/. step  [/] speed  0 start  $ end    q quit` |

### 8.3 帮助覆盖层规格（`?` 键 — 已实现）

**文件**: `dashboard_help.go`

**触发**: 任何非模态视图下按 `?` 键（过滤模式、Kill 确认、Prompt Pager 中不响应）
**退出**: 按 `?` 或 `Esc`

**布局**: 全屏覆盖层，两列并排，使用 `lipgloss.RoundedBorder()` + `ColorAgent` 边框

**内容分组**:

| 左列 | 右列 |
|------|------|
| **Navigation**: j/k, Tab, Enter | **View**: z, 1-8, Esc |
| **Timeline**: f, s, h/l, v, p, +/- | **Process**: K, a, l, r |
| **Filter Mode**: l, t, i, v, a, Esc | **Global**: L, H, ?, q |

**按键路由层级**:
```
Layer 0:  ctrl+c（强制退出）
Layer 1:  Prompt Pager
Layer 1.5: Help Overlay ← ? 或 Esc 关闭
Layer 2:  History 视图
Layer 2.5: LLM Viewer
Layer 3:  Kill 确认对话框
Layer 4:  Replay 模式
Layer 5:  全局快捷键 (L/H/1-8/Tab/z/?)
Layer 6:  面板内按键
```

---

## 9. 已结束进程的聚焦卡片

当用户在 Tree 中选中一个状态为 `✓ Done` 或 `✕ Failed` 的进程时：

### 9.1 Focus Card 差异

| 卡片 | Running 进程 | Dead 进程 |
|------|-------------|----------|
| 标题 | `FOCUS: PID N agent ━━━ Running Xm` | `FOCUS: PID N agent ━━━ ✓ Done (exit 0)` |
| 顶部提示 | 无 | `Historical snapshot — lived HH:MM:SS–HH:MM:SS (Xs)` |
| Tokens | `rate: X tok/s` (实时) | `rate: X tok/s` (最终值) |
| Steps | `steps: N` (递增) | `steps: N (final)` |
| Elapsed | `elapsed: Xm` (递增) | `lived: Xs` (固定) |
| Intent | 显示进行中 | 全部 ✓ 或 ✕ |
| Result | 无 | `✓ Completed / output → PID N` |

### 9.2 Timeline 自动过滤

选中 Dead 进程时，Timeline 自动过滤只显示该 PID 的事件（在 status bar 显示 `(filtered: PID N)`）。

---

## 10. 状态符号体系

全局统一的进程状态符号，替代当前文字状态：

```go
// internal/ui/symbols.go (新文件或扩展 styles.go)

const (
    SymRunning = "●"   // 运行中
    SymCreated = "○"   // 已创建
    SymDone    = "✓"   // 正常退出
    SymFailed  = "✕"   // 异常退出
    SymPaused  = "⏸"   // 暂停
)

// ASCII 模式 (RNIX_ASCII=1)
const (
    SymRunningASCII = "*"
    SymCreatedASCII = "o"
    SymDoneASCII    = "+"
    SymFailedASCII  = "x"
    SymPausedASCII  = "="
)
```

---

## 11. 实现路线图（全部完成 ✅）

### Phase 1: 导航重构 + 文件拆分 ✅

**Story 29.1 + 29.2** — 完成于 2026-03-24

1. ✅ 拆分 dashboard.go 为 15 独立文件（4108 行 → 模块化）
2. ✅ 引入 viewMode 枚举和 expandedPane
3. ✅ 数字键 1-8 直达（toggle 行为）
4. ✅ Shift-Tab 反向导航
5. ✅ Esc 回到默认视图
6. ✅ Title bar 标签高亮

### Phase 1.5: Status Bar 重设计 + 帮助覆盖层 ✅

**追加实现** — 完成于 2026-03-25

1. ✅ 分层发现模型：状态栏 5-6 个核心键 + `?` 帮助覆盖层
2. ✅ 上下文感知提示（每个视图模式不同 hints）
3. ✅ 帮助覆盖层两列布局 + 分组快捷键参考
4. ✅ 录制指示器 ●REC
5. ✅ 宽度预算 ≤ 55 字符（80/60 列安全）

### Phase 2: 默认视图 Focus Card ✅

**Story 29.3** — 完成于 2026-03-24

1. ✅ focusCardState 数据聚合
2. ✅ 2×3 网格渲染
3. ✅ 默认视图用 Focus Card 替代当前的面板
4. ✅ 已结束进程的 Focus Card 差异渲染（Historical snapshot + exit code）

### Phase 3: 进程历史保留 ✅

**Story 29.4 + 29.5** — 完成于 2026-03-25

1. ✅ kernel ProcessHistory（内存 FIFO 1000 条）
2. ✅ IPC list_all_procs
3. ✅ Dashboard H 视图（全屏覆盖层 + 搜索/排序）
4. ✅ Tree 面板显示已结束进程（✓/✕ 状态符号）
5. ✅ 统一状态符号体系（Unicode + ASCII 模式）
6. ✅ 磁盘持久化 — proc-info.json + daemon 启动 LoadHistory()
7. ✅ Dead 进程 Detail 面板 — handleGetProcDetailFromHistory()
8. ✅ 翻页导航 — PgDn/PgUp/g/G/Home/End（Tree/Timeline/History）
9. ✅ Syscall 事件持久化 — EventWriter + events.jsonl + list_events IPC
10. ✅ Context Profile 快照 — ctx-profile.json + 磁盘回退

### Phase 4: LLM 对话查看器 ✅

**Story 29.6** — 完成于 2026-03-25

1. ✅ dashboard_llm_viewer.go
2. ✅ 全局 L 快捷键入口
3. ✅ Step 导航 (h/l)
4. ✅ Request/Response 分块渲染
5. ✅ 从 Detail/Timeline/Heatmap 的 Enter 入口

### Post-Epic 增强（2026-03-25）

Bug 修复与鲁棒性改进：
- ✅ Dead 进程 detail/focus card 数据加载修复
- ✅ PID 复用去重改为 UUID（ListAllProcs）
- ✅ Dashboard UUID 校验（focus/detail/timeline）
- ✅ Help overlay `h/l` 描述修正（"Pan time axis"）
- ✅ Focus Card token 数据 fallback（从 procDetail 回退）
- ✅ Heatmap 死进程提示改善

---

## 12. 兼容性与约束

### 12.1 不破坏现有行为

- Tab 仍然可用（保留线性循环）
- 所有现有面板内的 j/k/Enter 操作不变
- Prompt pager 保留，L 视图是升级版

### 12.2 技术约束

- 纯终端 TUI，Bubbletea v2 + Lipgloss
- 支持 80-200 列宽
- `RNIX_ASCII=1` 时所有符号使用 ASCII 替代
- 不引入新的外部依赖

### 12.3 性能约束

- Focus Card 数据聚合在 tick 中完成（500ms 周期）
- ProcessHistory 上限 1000 条，FIFO 淘汰
- LLM 查看器按需加载 step detail（不预加载）

---

## 13. 测试策略

### 13.1 单元测试

| 组件 | 测试重点 |
|------|---------|
| `jumpToPane` | 数字键切换 + 二次按键回退 |
| `stateSymbolWithExit` | 各状态 + exit code 组合 |
| `buildFocusCard` | 数据聚合正确性 |
| `ProcessHistory` | Add/List/FIFO 淘汰/并发安全 |
| `renderLLMViewer` | 边界：空 step、超长内容 |
| `historyKey` | 排序、搜索、选中跳转 |

### 13.2 集成测试

| 场景 | 验证 |
|------|------|
| 数字键导航完整循环 | 1→展开→Esc→默认→2→展开→同键→默认 |
| LLM 查看器端到端 | spawn 进程→L 打开→h/l 翻页→Esc 关闭 |
| 历史进程追溯 | spawn→wait exit→H 查看→Enter 聚焦→L 查看 LLM |
| ASCII 模式 | `RNIX_ASCII=1` 下所有符号正确 |
