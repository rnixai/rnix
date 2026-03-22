# Story 27.4: Dashboard Prompt 查看

Status: done

## Story

As a 平台构建者,
I want 在 dashboard 时间线窗格中按 p 键查看选中步骤的完整 prompt 内容,
So that 我可以看到 agent 当时收到了什么指令，精确定位 prompt 注入或上下文问题。

## Acceptance Criteria

### AC-1: p 键进入 Prompt Pager

**Given** dashboard 时间线窗格中选中了某个步骤（stepCursor 有效）
**When** 用户按 `p` 键
**Then** 发起 GetStepDetail IPC 请求（或命中 stepDetailCache）获取完整 prompt
**And** 进入全屏 Prompt Pager 模式，覆盖 dashboard 三窗格布局
**And** 显示三段内容：System Prompt → Messages 列表 → Tools 定义
**And** GetStepDetail 返回延迟 ≤ 500ms（NFR59-obs）

### AC-2: Pager 滚动

**Given** Prompt Pager 模式
**When** 用户按 j/k 或 ↑/↓ 键
**Then** 内容平滑滚动（使用 BubbleTea viewport 组件）
**And** 支持 PgUp/PgDn 翻页、Home/End 跳转首尾

### AC-3: q 键返回 Dashboard

**Given** Prompt Pager 模式
**When** 用户按 `q` 键
**Then** 退出 Pager，返回 dashboard 时间线视图
**And** 保持之前的 stepCursor 选中状态和 activePane 状态
**And** 不退出 dashboard 本身（q 在 pager 内被拦截）

### AC-4: 离线查看（进程已被 reaper 清理）

**Given** 进程已被 reaper 清理（不在内存中）
**When** 用户按 p 键查看该进程的某步骤 prompt
**Then** GetStepDetail 从 `.rnix/data/steps/<pid>/process-meta.json` 和 `steps.jsonl` 读取
**And** 正常显示完整 prompt 内容（离线查看能力）

### AC-5: Prompt 内容格式化渲染

**Given** Prompt Pager 显示完整 prompt
**When** 渲染各段内容
**Then** System Prompt 段：显示完整 SystemPrompt 文本，带 `═══ System Prompt ═══` 分隔标题
**And** Messages 段：按序显示每条消息，格式 `[role] content`（role 着色区分 system/user/assistant/tool）
**And** Tools 段：显示工具定义列表，每个工具名 + 描述
**And** 各段之间用分隔线清晰区隔

### AC-6: 缓存复用

**Given** 用户已通过 v/V 键展开过某步骤（stepDetailCache 已有该步骤数据）
**When** 用户按 p 键查看同一步骤的 prompt
**Then** 直接使用缓存数据，不再发起 IPC 请求
**And** Pager 立即显示，无等待

### AC-7: 无步骤时 p 键无效

**Given** dashboard 时间线无步骤（stepEntries 为空）
**When** 用户按 p 键
**Then** 无任何操作（静默忽略）

## Tasks / Subtasks

- [x] Task 1: Dashboard Model 扩展 (AC: #1, #3, #6)
  - [x] 1.1 `dashboardModel` 新增 `promptPager bool` 字段（是否处于 pager 模式）
  - [x] 1.2 新增 `promptViewport viewport.Model` 字段（BubbleTea viewport 组件）
  - [x] 1.3 新增 `promptContent string` 字段（已格式化的 prompt 文本）
  - [x] 1.4 新增 `promptStep int` 字段（当前查看的步骤号，用于标题显示）
  - [x] 1.5 新增 `promptPagerMsg` Msg 类型（p 键触发 GetStepDetail 异步返回后进入 pager）
- [x] Task 2: p 键处理逻辑 (AC: #1, #6, #7)
  - [x] 2.1 在 `dashboardKey` 的 step timeline 模式分支中新增 `p` 键处理
  - [x] 2.2 检查 stepEntries 非空且 stepCursor 有效
  - [x] 2.3 优先从 `stepDetailCache` 获取数据；cache miss 则发起 `fetchStepDetailForPagerCmd`
  - [x] 2.4 cache hit 时直接调用 `enterPromptPager(detail)` 进入 pager
  - [x] 2.5 cache miss 时返回 Cmd，在 `promptPagerMsg` 回调中进入 pager
- [x] Task 3: Prompt 内容格式化 (AC: #5)
  - [x] 3.1 新增 `formatPromptContent(detail *ipc.GetStepDetailResponse, step int) string` 函数
  - [x] 3.2 System Prompt 段：`═══ System Prompt ═══\n` + 完整文本
  - [x] 3.3 Messages 段：`═══ Messages (N msgs, ~Xk tokens) ═══\n` + 逐条 `[role] content`
  - [x] 3.4 Tools 段：`═══ Tools (N) ═══\n` + 逐个 `• name — description`
  - [x] 3.5 角色着色：使用 lipgloss 样式区分 system(灰)、user(绿)、assistant(蓝)、tool(黄)
- [x] Task 4: Pager 模式渲染与按键 (AC: #2, #3)
  - [x] 4.1 `View()` 中：当 `promptPager == true` 时调用 `renderPromptPager()` 替代正常三窗格布局
  - [x] 4.2 `renderPromptPager()` 渲染标题栏 + viewport + 底部帮助栏
  - [x] 4.3 标题栏：`Prompt View | PID {pid} Step {step} | {messageCount} msgs ~{tokenCount} tokens`
  - [x] 4.4 底部帮助栏：`j/k:scroll  PgUp/PgDn:page  Home/End:jump  q:back`
  - [x] 4.5 `dashboardKey` 中：`promptPager` 模式下 q 键设 `promptPager=false` 返回 dashboard
  - [x] 4.6 `promptPager` 模式下将非 q 按键转发给 `viewport.Update(msg)`
- [x] Task 5: 测试 (AC: all)
  - [x] 5.1 `formatPromptContent` 单元测试：验证三段格式化输出
  - [x] 5.2 p 键 cache hit 测试：预填 stepDetailCache，按 p 后验证 promptPager=true
  - [x] 5.3 p 键 cache miss 测试：空 cache，按 p 后验证返回 fetchCmd
  - [x] 5.4 q 键返回测试：pager 模式按 q 验证 promptPager=false 且 stepCursor 不变
  - [x] 5.5 空 stepEntries 时按 p 键无操作测试
  - [x] 5.6 `make all` 全部通过

## Dev Notes

### 架构决策引用

- **Decision 24**: Progress 回调 + StepRecord 双层架构 — Prompt 查看（p 键）的数据来源为 `Process.FinalSystemPrompt + StepRecord.Messages + Process.NativeToolDefs`，通过 GetStepDetail IPC 获取 [Source: architecture/core-architectural-decisions.md#Decision-24]
- **Decision 25**: GetStepDetail IPC 方法 — 按需查询步骤完整数据 [Source: architecture/core-architectural-decisions.md#Decision-25]
- **Decision 26**: Dashboard 增强 — UI 状态机 `Normal → v → Expanded → V → Debug → p → Pager → q → Normal` [Source: architecture/core-architectural-decisions.md#Decision-26]
- **FR167**: 用户可以在 dashboard 时间线窗格中按 p 键查看选中步骤的完整 prompt 内容，进入类似 less 的翻页查看模式，按 q 返回时间线

### 关键实现约束

#### 1. BubbleTea Viewport 组件

使用 `github.com/charmbracelet/bubbles/viewport` 实现翻页滚动。Dashboard 已有 `bubbletea` 和 `lipgloss` 依赖（Epic 17），但需检查 `viewport` 是否已在 go.mod 中（bubbles 包含 viewport）。

```go
import "github.com/charmbracelet/bubbles/viewport"

// dashboardModel 新增
promptPager    bool
promptViewport viewport.Model
promptContent  string
promptStep     int
```

Viewport 初始化方式：
```go
func (m *dashboardModel) enterPromptPager(detail *ipc.GetStepDetailResponse, step int) {
    content := formatPromptContent(detail, step)
    vp := viewport.New(m.width, m.height-2) // 留 2 行给标题和底部帮助栏
    vp.SetContent(content)
    m.promptViewport = vp
    m.promptContent = content
    m.promptStep = step
    m.promptPager = true
}
```

#### 2. 按键处理优先级

`promptPager` 模式的按键处理必须在 `dashboardKey` 最前面拦截，优先于 confirmKill 和其他所有分支：

```go
func (m dashboardModel) dashboardKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
    // Prompt pager 模式：拦截所有按键
    if m.promptPager {
        if msg.Code == 'q' || msg.Code == rune(tea.KeyEscape) {
            m.promptPager = false
            return m, nil
        }
        var cmd tea.Cmd
        m.promptViewport, cmd = m.promptViewport.Update(msg)
        return m, cmd
    }
    // ... 现有 confirmKill、tab、v/V 等处理
}
```

**关键：** `q` 在 pager 模式下只退出 pager，不退出 dashboard。这与全局 `q` 退出 dashboard 的行为不同——pager 模式下 q 被提前拦截。

#### 3. p 键在 step timeline 分支中的位置

p 键处理逻辑应插入到 `dashboardKey` 中现有 v/V 处理之后（`cmd/rnix/dashboard.go:382-410` 区域之后）：

```go
// 在 v/V 处理后、Tree 窗格处理前
if msg.Code == 'p' && m.stepTimelineMode && len(m.stepEntries) > 0 && m.stepCursor < len(m.stepEntries) {
    entry := m.stepEntries[m.stepCursor]
    if cached := m.stepDetailCache[entry.summary.Step]; cached != nil {
        m.enterPromptPager(cached, entry.summary.Step)
        return m, nil
    }
    if !m.fetchingDetail && m.selectedPID > 0 {
        m.fetchingDetail = true
        return m, fetchStepDetailForPagerCmd(m.selectedPID, entry.summary.Step)
    }
    return m, nil
}
```

#### 4. 新增 Msg 类型：promptPagerMsg

区分 p 键触发的 GetStepDetail 回调与 v/V 键的 `stepDetailResultMsg`：

```go
type promptPagerMsg struct {
    step   int
    detail *ipc.GetStepDetailResponse
    err    error
}
```

在 `Update()` 的 switch 中处理：
```go
case promptPagerMsg:
    m.fetchingDetail = false
    if msg.err == nil && msg.detail != nil {
        m.stepDetailCache[msg.step] = msg.detail // 同时填充 cache
        m.enterPromptPager(msg.detail, msg.step)
    }
    return m, nil
```

`fetchStepDetailForPagerCmd` 与 `fetchStepDetailCmd` 几乎相同，区别是返回 `promptPagerMsg` 而非 `stepDetailResultMsg`。

#### 5. Prompt 内容格式化规范

```
═══ System Prompt (12.3k chars) ═══

You are a code analyst agent...
[完整 SystemPrompt 文本]

═══ Messages (23 msgs, ~12.5k tokens) ═══

[user] 请帮我分析这段代码的性能问题...

[assistant] 我来分析一下。首先让我读取文件...

[tool:read_file] /dev/fs/main.go
  → (3.2k chars) package main\nfunc main() {...

[assistant] 代码分析完成。主要问题是...

═══ Tools (5) ═══

• read_file — Read a file from the virtual filesystem
• write_file — Write content to a file
• shell_exec — Execute a shell command
• search — Search for patterns in files
• list_dir — List directory contents
```

角色显示规则：
- `[user]` 绿色
- `[assistant]` 蓝色
- `[tool:name]` 黄色（tool role 的消息，name 从 ToolCalls 或上下文推断）
- `[system]` 灰色
- 消息内容超过 2000 字符时截断显示前 2000 字符 + `\n... (truncated, N chars total)`

#### 6. Window Resize 处理

Pager 模式下 `tea.WindowSizeMsg` 需要同步更新 viewport 尺寸：

```go
case tea.WindowSizeMsg:
    m.width = msg.Width
    m.height = msg.Height
    if m.promptPager {
        m.promptViewport.Width = msg.Width
        m.promptViewport.Height = msg.Height - 2
    }
```

### 现有代码关键位置

| 文件 | 行号范围 | 说明 |
|------|----------|------|
| `cmd/rnix/dashboard.go:57-70` | stepDetailLevel + stepEntry | Story 27-3 已有类型 |
| `cmd/rnix/dashboard.go:181-195` | dashboardModel step 字段 | 新增 promptPager 等字段的位置 |
| `cmd/rnix/dashboard.go:211-276` | Update() msg switch | 新增 promptPagerMsg case |
| `cmd/rnix/dashboard.go:332-410` | dashboardKey 按键处理 | promptPager 拦截插入点（最前面）+ p 键插入点（v/V 后） |
| `cmd/rnix/dashboard.go:867-937` | renderStepTimeline | 当前 Level 1/2/3 渲染，p 键入口的视觉基础 |
| `cmd/rnix/dashboard.go:1151-1162` | fetchStepDetailCmd | 复用模式创建 fetchStepDetailForPagerCmd |
| `ipc/protocol.go:568-607` | GetStepDetailResponse + MessageWire + ToolDefWire | prompt 数据源类型 |
| `ipc/client.go:480-491` | GetStepDetail 客户端方法 | 已实现，直接复用 |

### 前序 Story 27-3 经验

- **异步模式确认：** v/V 键的 GetStepDetail 使用 BubbleTea Cmd 异步获取，p 键采用相同模式
- **cache 策略确认：** `stepDetailCache map[int]*ipc.GetStepDetailResponse` 以 step 号为 key，PID 切换时清理——p 键直接复用此 cache
- **fetchingDetail 互斥：** 同一时刻只能有一个 GetStepDetail 请求，p 键需检查此标志
- **PID 切换清理：** `handlePIDChange` 中需追加 `promptPager = false`（PID 切换时必须退出 pager）
- **测试模式：** 通过 `tea.KeyPressMsg{Code: 'p'}` 构造按键，断言 model 字段变化

### 防踩坑清单

1. **q 键拦截顺序**：pager 模式的 q 处理必须在 `dashboardKey` 最前面，否则会被全局 q（退出 dashboard）抢先处理
2. **viewport 需要 import**：`github.com/charmbracelet/bubbles/viewport`——确认 go.mod 中 `charmbracelet/bubbles` 已存在
3. **不要在 Update 中同步调用 IPC**：cache miss 时必须返回 Cmd，不能阻塞 UI 线程
4. **消息内容可能很长**：SystemPrompt 和 Messages 内容可能达到数万字符，viewport 可以处理大文本但要确保格式化时不做不必要的内存拷贝
5. **PID 切换时退出 pager**：`handlePIDChange` 里必须重置 `promptPager = false`
6. **viewport 初始化时机**：每次进入 pager 都要重新创建 viewport（因为内容和尺寸可能不同），不要复用旧的 viewport 实例
7. **promptPagerMsg vs stepDetailResultMsg**：两种 Msg 类型必须区分。p 键触发的 GetStepDetail 返回 promptPagerMsg（自动进入 pager），v/V 键返回 stepDetailResultMsg（只更新 cache）。如果合并为一个类型会导致 v/V 展开时意外进入 pager
8. **Escape 键也退出 pager**：除 q 外，Escape 键也应退出 pager 模式（用户直觉）
9. **renderPromptPager 的 View() 接管**：pager 模式下 `View()` 直接返回 pager 渲染结果，不渲染三窗格——这意味着 View() 的最前面要做 `if m.promptPager { return m.renderPromptPager() }` 判断

### Project Structure Notes

- 所有修改限于 `cmd/rnix/dashboard.go` — 不需要修改 IPC 层或 kernel 层
- 测试文件：`cmd/rnix/atdd_27_4_dashboard_prompt_view_test.go`（新增）
- 依赖：`charmbracelet/bubbles/viewport`（已在 go.mod 中通过 `charmbracelet/bubbles` 间接依赖）

### References

- [Source: architecture/core-architectural-decisions.md#Decision-24] — Progress + StepRecord 双层架构
- [Source: architecture/core-architectural-decisions.md#Decision-25] — GetStepDetail IPC
- [Source: architecture/core-architectural-decisions.md#Decision-26] — Dashboard 增强 UI 状态机
- [Source: prd/functional-requirements.md#FR167] — prompt 查看功能需求
- [Source: prd/non-functional-requirements.md#NFR59-obs] — GetStepDetail ≤ 500ms
- [Source: sprint-change-proposal-2026-03-22.md] — watch→dashboard 决策
- [Source: 27-3-dashboard-timeline-three-level-detail.md] — 前序 Story 实现细节
- [Source: epic-27-统一观察系统-unified-observation-system.md#Story-27.4] — Epic 原文

## Dev Agent Record

### Agent Model Used

Claude claude-4.6-opus (Cursor)

### Debug Log References

- Escape key String() = "esc" (not "escape") — fixed in dashboardKey pager interception

### Completion Notes List

- Task 1: 添加 `promptViewport viewport.Model` 字段到 dashboardModel，import `charm.land/bubbles/v2/viewport`。`promptPager`/`promptContent`/`promptStep`/`promptPagerMsg` 已由 ATDD stub 预定义。
- Task 2: p 键处理插入 v/V 之后。cache hit 直接 enterPromptPager，cache miss 发起 fetchStepDetailForPagerCmd 返回 promptPagerMsg。fetchingDetail 互斥。
- Task 3: formatPromptContent 三段格式化 — System Prompt (含字符数)、Messages (含 msg count + token count, 角色着色, 超 2000 字截断)、Tools (含数量)。辅助函数 formatRoleTag/formatCharCount。
- Task 4: promptPager 拦截在 dashboardKey 最前面（优先于 confirmKill），q/Esc 退出，其余转发 viewport.Update。View() 和 renderDashboard() 双重检查 promptPager。renderPromptPager 渲染标题+viewport+帮助栏。WindowSizeMsg 同步 viewport 尺寸。handleTimelinePIDChange 中 PID 切换重置 promptPager。
- Task 5: 30 个 ATDD 测试全部通过（已由 Story 27-4 RED phase 预定义）。make all 通过。

### Code Review Fixes (CR pass)

- CR-P1: `promptPagerMsg` 新增 `pid` 字段，Update handler 校验 `msg.pid == m.selectedPID`，丢弃 PID 切换后的旧响应，防止 cache 污染
- CR-P2: IPC 失败时设置 `statusMsg` 显示错误提示（如 "✗ prompt load: connection refused"）
- CR-P3: pager 模式下 `ctrl+c` 直接退出程序（`tea.Quit`），与全局行为一致
- CR-P4: `formatRoleTag` 接受 `toolCallNames map` 参数，tool 角色优先显示工具名（如 `[tool:read_file]`）而非不透明 ToolCallID；`formatPromptContent` 预构建 toolCall ID→Name 映射
- CR-P5: pager 按键处理中手动处理 `home`/`end` 键，调用 `viewport.GotoTop()`/`GotoBottom()`（bubbles/v2 viewport 默认 keymap 不含 Home/End 绑定）
- CR 新增 9 个测试用例（PID mismatch、error statusMsg、ctrl+c、Home/End、tool name resolution）

### File List

- cmd/rnix/dashboard.go (modified)
- cmd/rnix/atdd_27_4_dashboard_prompt_view_test.go (modified — added 9 CR tests)
- go.mod (modified — added charm.land/bubbles/v2 dependency)
- go.sum (modified — updated dependency checksums)
