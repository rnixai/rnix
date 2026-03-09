# Story 17.3: 上下文热力图窗格

Status: review

## Story

As a 平台构建者,
I want 在热力图窗格中可视化智能体的上下文组成,
So that 我可以直观了解 token 分布和活跃度。

## Acceptance Criteria

1. **Given** 选中一个智能体节点
   **When** 热力图窗格渲染
   **Then** 按来源着色展示上下文组成（system prompt / skill 指令 / 工具结果 / 对话历史），面积正比 token 占比，深浅表示活跃度

2. **Given** 热力图中某个区域
   **When** 用户选中该区域
   **Then** 显示具体 token 数、占比百分比和内容摘要

## Tasks / Subtasks

- [x] Task 1: Heatmap 数据模型和 IPC 获取 (AC: #1)
  - [x] 1.1 在 `dashboardModel` 新增 heatmap 相关字段：`heatmapProfile *debug.CtxProfileResult`（当前选中进程的上下文 profile）、`heatmapPID types.PID`（已获取 profile 的 PID）、`heatmapSegments []heatmapSegment`（渲染用的分段数据）、`heatmapCursor int`（选中区域索引）
  - [x] 1.2 定义 `heatmapSegment` 结构体：`label string`（如 "System Prompt"）、`tokens int`、`pct float64`、`kind segmentKind`（来源分类）、`activity activityLevel`（活跃度：active/warm/cold/leaked）、`summary string`（内容摘要，截断至 60 rune）
  - [x] 1.3 定义 `segmentKind` 类型和常量：`segSystem`（system prompt）、`segSkill`（skill 指令）、`segTool`（工具结果）、`segUser`（用户消息）、`segAssistant`（对话历史）、`segLeaked`（泄漏）
  - [x] 1.4 定义 `activityLevel` 类型和常量：`actActive`、`actWarm`、`actCold`、`actLeaked`
  - [x] 1.5 实现 `segmentColor(kind segmentKind, activity activityLevel) string` — 返回基于来源分类和活跃度深浅的颜色值

- [x] Task 2: CtxProfile IPC 集成 (AC: #1)
  - [x] 2.1 定义 `heatmapProfileMsg` 消息类型（包含 `*debug.CtxProfileResult` 和 `error`）
  - [x] 2.2 实现 `fetchHeatmapCmd(pid types.PID) tea.Cmd`：创建独立 IPC 连接 → 调用 `client.CtxProfile(pid)` → 返回 `heatmapProfileMsg`
  - [x] 2.3 在 `Update` 中处理 `heatmapProfileMsg`：存储 profile → 调用 `buildHeatmapSegments()` 构建渲染用分段
  - [x] 2.4 在 `dashboardTick` 中检测 `selectedPID` 变化或周期性刷新（每 5 次 tick = 2.5s 一次），触发 `fetchHeatmapCmd`
  - [x] 2.5 在 heatmap 字段中记录 `heatmapPID`，只有 PID 变化或到了刷新周期时才重新获取

- [x] Task 3: Profile 数据到 Heatmap 分段转换 (AC: #1)
  - [x] 3.1 实现 `buildHeatmapSegments(profile *debug.CtxProfileResult) []heatmapSegment`：
    - 从 `TopConsumers` 提取各来源的 token 数和占比
    - 根据 `Classification`（Active/Warm/Cold/Leaked）分配活跃度
    - system_prompt → segSystem + actActive（system prompt 始终 active）
    - user → segUser + 按 Classification 分配
    - assistant → segAssistant + 按 Classification 分配
    - tool:* → segTool + 按 Classification 分配
    - Leaked bucket → segLeaked + actLeaked
  - [x] 3.2 确保 segments 按 token 占比降序排序（最大的段在前）
  - [x] 3.3 合并过小的段（占比 < 3%）为 "Other" 段

- [x] Task 4: Heatmap 渲染 (AC: #1)
  - [x] 4.1 替换 `renderDashboardPlaceholder("Heatmap", ...)` 为 `m.renderHeatmapPane(width, height)`
  - [x] 4.2 实现 `renderHeatmapPane(width, height int) string`：
    - 窗格边框：与其他窗格一致（active=ColorAgent, inactive=ColorMuted）
    - 标题行："Heatmap" + PID + 总 token 数 + budget 百分比
  - [x] 4.3 实现 treemap 风格渲染：
    - 计算每段在可用宽度中占据的字符数（`segWidth = pct / 100 * barWidth`）
    - 每段用对应颜色的色块字符（`█`）填充，宽度正比 token 占比
    - 活跃度影响色彩深浅：Active=亮色（原色）、Warm=中等、Cold=暗色（加灰）、Leaked=红色调
    - 色块下方显示段标签和百分比
  - [x] 4.4 实现段详情列表：各段一行，显示 `[图标] label  tokens tok  pct%`
    - 选中段用 `▸` 标记，显示更详细的信息
  - [x] 4.5 空状态渲染：无 selectedPID 时显示 "Select an agent to view heatmap"；有 PID 但无 profile 时显示 "Loading context profile..."

- [x] Task 5: 区域选择和详情显示 (AC: #2)
  - [x] 5.1 在 `heatmapCursor` 中跟踪选中段索引
  - [x] 5.2 在 `handleHeatmapKey` 中处理按键：
    - `j`/`k` 或 ↑/↓：上下移动 heatmapCursor 选择不同段
    - `enter`：展开/折叠选中段的详细内容摘要
  - [x] 5.3 选中段时底部显示：token 数、占比百分比、活跃度分类、内容摘要（截断至 60 rune）

- [x] Task 6: 状态栏更新 (AC: #1, #2)
  - [x] 6.1 更新 `renderDashboardStatus()` — heatmap 焦点时显示 heatmap 专用快捷键提示
  - [x] 6.2 快捷键提示：`j/k:Select Segment  Enter:Details`

- [x] Task 7: 测试 (AC: #1, #2)
  - [x] 7.1 `dashboard_test.go`：buildHeatmapSegments — 空 profile → 空段列表
  - [x] 7.2 `dashboard_test.go`：buildHeatmapSegments — 有 TopConsumers → 按 token 降序生成段
  - [x] 7.3 `dashboard_test.go`：buildHeatmapSegments — 合并小段（占比 < 3%）为 Other
  - [x] 7.4 `dashboard_test.go`：segmentColor — 不同 kind 和 activity 返回不同颜色
  - [x] 7.5 `dashboard_test.go`：heatmapProfileMsg 处理 — profile 存储到 model
  - [x] 7.6 `dashboard_test.go`：heatmap 渲染 — 无 selectedPID 时显示 "Select an agent"
  - [x] 7.7 `dashboard_test.go`：heatmap 渲染 — 有 segments 时不含 "Coming Soon"
  - [x] 7.8 `dashboard_test.go`：heatmap 选择 — j/k 键移动 heatmapCursor
  - [x] 7.9 `dashboard_test.go`：heatmap 选中段 — 显示 token 数和百分比
  - [x] 7.10 `dashboard_test.go`：PID 变化时清空 heatmapProfile
  - [x] 7.11 `dashboard_test.go`：Tab 切换到 heatmap → activePane == paneHeatmap
  - [x] 7.12 `dashboard_test.go`：segmentKind 分类 — system_prompt → segSystem, user → segUser
  - [x] 7.13 `dashboard_test.go`：heatmapRefresh — 5 次 tick 后触发 fetchHeatmapCmd

## Dev Notes

### 架构决策

本 story 在 17-1/17-2 建立的 dashboard 框架上实现热力图窗格。核心设计原则：

1. **复用 CtxProfile IPC** — `ipc.Client.CtxProfile(pid)` 已在 Epic 15 Story 15-4 中实现，返回 `debug.CtxProfileResult`（包含 Classification 四温分类 + TopConsumers 消费者排名 + Suggestions 优化建议）。**不新增任何 IPC 方法。**
2. **独立 IPC 连接** — 与 17-2 timeline 相同策略：CtxProfile 调用使用 `tea.Cmd` 在独立 goroutine 中创建新 IPC 连接，避免阻塞 dashboard 的主 tick 循环。
3. **按需刷新而非实时流** — 与 timeline 的实时事件流不同，上下文 profile 变化频率低，采用 2.5s 轮询（每 5 次 tick）而非 `AttachDebug` 流。
4. **Treemap 色块渲染** — 纯 lipgloss 渲染，用色块字符的宽度表示 token 占比，颜色深浅表示活跃度。不引入 bubbles。

### 关键设计：IPC 数据源

`debug.CtxProfileResult` 已包含热力图所需的全部信息：

```go
type CtxProfileResult struct {
    PID            types.PID
    CtxID          types.CtxID
    TokensUsed     int
    ContextBudget  int
    TotalTokens    int
    Classification ClassificationResult  // Active/Warm/Cold/Leaked 四温分类
    TopConsumers   []ConsumerEntry       // 排名前 5 的 token 消费者
    Suggestions    []string              // 优化建议
}

type ClassificationResult struct {
    Active ClassBucket  // 当前推理引用
    Warm   ClassBucket  // 近期使用
    Cold   ClassBucket  // 未引用
    Leaked ClassBucket  // 已无用未释放
}

type ClassBucket struct {
    Tokens   int
    Messages int
    Pct      float64
}

type ConsumerEntry struct {
    Kind    string   // "system_prompt" / "user" / "assistant" / "tool:read_file" 等
    Tokens  int
    Pct     float64
    Rank    int
}
```

**映射规则：**
- `TopConsumers[i].Kind` 映射到 `segmentKind`：
  - `"system_prompt"` → `segSystem`
  - `"user"` → `segUser`
  - `"assistant"` → `segAssistant`
  - 以 `"tool:"` 开头 → `segTool`（合并所有 tool 类型）
  - `Classification.Leaked.Tokens > 0` → 额外添加 `segLeaked` 段
- 活跃度来自 `Classification` 的温度分布，但因为 TopConsumers 不直接包含温度信息，需要按占比估算：排名高且 Active bucket 大 → actActive，Cold bucket 大 → actCold

### 关键设计：颜色方案

来源分类基色：

| Kind | 基色 | 复用 |
|------|------|------|
| System Prompt | `#5B9BD5` (蓝) | ui.ColorAgent |
| Skill 指令 | `#9B59B6` (紫) | colorIPC |
| Tool 结果 | `#6BCB77` (绿) | ui.ColorSuccess |
| User 消息 | `#FFD93D` (黄) | ui.ColorWarning |
| Assistant | `#888888` (灰) | ui.ColorKernel |
| Leaked | `#FF6B6B` (红) | ui.ColorError |

活跃度调制（在 dashboard.go 中定义本地颜色常量，不修改 styles.go）：

| Activity | 策略 | 示例 |
|----------|------|------|
| Active | 原色（亮） | `#5B9BD5` |
| Warm | 降一档 | `#4A8BC4`（稍暗） |
| Cold | 明显暗 | `#3A6B94`（暗） |
| Leaked | 统一红 | `#FF6B6B` |

实际实现中为简化，只区分 Active（原色）和 Cold（暗色变体），Warm 使用原色。在 dashboard.go 中定义 `dim(hexColor string) string` 辅助函数，降低亮度约 30%。

### 关键设计：Treemap 渲染

```
┌────────────────── Heatmap ───────────────────┐
│ PID 3 | ~1200 tok / 8000 budget (15%)        │
│                                               │
│ ████████████████████▓▓▓▓▓▓▓▓░░░░░░▒▒▒▒▒     │
│ System(30%)  Tool(25%)  User(20%) Asst(15%)  │
│                                               │
│ ▸ System Prompt   360 tok  30.0%  Active     │
│   Tool Results    300 tok  25.0%  Warm       │
│   User Messages   240 tok  20.0%  Active     │
│   Assistant       180 tok  15.0%  Cold       │
│   Leaked           60 tok   5.0%  Leaked     │
│   Other            60 tok   5.0%  Cold       │
│                                               │
│ ── Selected: System Prompt ──                │
│ 360 tokens | 30.0% | Active                  │
│ "You are an AI agent running in rnix OS..."  │
└───────────────────────────────────────────────┘
```

渲染分三部分：
1. **标题行** — "Heatmap" + PID + token 统计
2. **色块条** — 水平色块条，每段宽度按占比分配，着色按来源+活跃度
3. **分段列表** — 各段一行，选中段带 `▸` 标记 + 底部详情

### 关键设计：heatmapProfileMsg

```go
type heatmapProfileMsg struct {
    profile *debug.CtxProfileResult
    err     error
}

func fetchHeatmapCmd(pid types.PID) tea.Cmd {
    return func() tea.Msg {
        client, err := ipc.Dial(ipc.SocketPath())
        if err != nil {
            return heatmapProfileMsg{err: err}
        }
        defer client.Close()
        profile, err := client.CtxProfile(pid)
        return heatmapProfileMsg{profile: profile, err: err}
    }
}
```

注意：`CtxProfile` 是请求-响应式（非流式），调用完成后立即关闭连接，不需要像 timeline 那样维持长连接。

### 关键设计：刷新策略

```go
// dashboardModel 新增字段
heatmapTickCount int  // tick 计数器

// 在 dashboardTick 中
m.heatmapTickCount++
needRefresh := m.selectedPID != m.heatmapPID || m.heatmapTickCount%5 == 0
if needRefresh && m.selectedPID > 0 {
    m.heatmapPID = m.selectedPID
    if m.selectedPID != m.heatmapPID {
        m.heatmapProfile = nil
        m.heatmapSegments = nil
        m.heatmapCursor = 0
    }
    return m, tea.Batch(tickCmd(), fetchHeatmapCmd(m.selectedPID))
}
```

PID 变化时立即获取（清空旧数据），同一 PID 每 2.5s 刷新一次。

### 关键设计：bubbletea 消息流

```
           ┌─────────────────────────────────┐
           │ dashboardModel                  │
           │                                  │
tickMsg ──►│ dashboardTick()                 │
           │  ├─ ListProcs() → 更新进程树     │
           │  ├─ 检测 selectedPID 变化        │
           │  │   ├─ timeline 处理（已有）     │
           │  │   └─ heatmap: 清空+fetchCmd   │
           │  └─ heatmapTickCount%5 == 0     │
           │      └─ fetchHeatmapCmd()       │
           │                                  │
heatmapProfileMsg ──►│                        │
           │  ├─ 存储 profile                 │
           │  └─ buildHeatmapSegments()      │
           │                                  │
KeyPressMsg (paneHeatmap) ──►│               │
           │  ├─ j/k: heatmapCursor          │
           │  └─ enter: 展开详情              │
           └─────────────────────────────────┘
```

### 不要做的事情

- **不要**新增 IPC 方法 — 完全复用 `client.CtxProfile(pid)` 已有方法
- **不要**修改 `ipc/` 包的任何文件
- **不要**修改 `debug/` 包的任何文件 — 直接使用 `debug.CtxProfileResult` 结构
- **不要**修改 `internal/ui/styles.go` — 活跃度暗色在 dashboard.go 本地定义
- **不要**修改 `internal/types/` — 使用已有类型
- **不要**引入 `bubbles` 依赖 — 纯 lipgloss 渲染
- **不要**实现窗格联动 — 17-4 实现（本 story 通过 selectedPID 驱动，但不实现点击联动）
- **不要**实现离线回放 — 17-5 实现
- **不要**创建新的包或子目录 — 所有代码放在 `cmd/rnix/dashboard.go`
- **不要**使用 `.yml` 后缀
- **不要**使用全大写常量名（不用 `MAX_SEGMENTS`，用 `maxSegments`）
- **不要**修改 timeline 相关逻辑（17-2 已完成的功能）
- **不要**复用 dashboard 的主 IPC 连接做 CtxProfile — 在 tea.Cmd 中创建独立连接

### 组合矩阵

| 本功能 | 交互对象 | 交互方式 | 需验证 |
|--------|----------|----------|--------|
| heatmap CtxProfile | dashboard ListProcs | 独立 IPC 连接，互不干扰 | 是 |
| heatmap CtxProfile | timeline AttachDebug | 完全独立的 IPC 连接，互不干扰 | 否 |
| heatmap 刷新 | selectedPID 变化 | PID 变化触发清空旧数据+重新获取 | 是 |
| heatmap 渲染 | activePane 焦点 | 焦点切换改变边框颜色和键盘路由 | 是 |
| heatmap cursor | 分段列表 | cursor 索引在有效范围内循环 | 是 |
| Kill 操作 | heatmap 数据 | Kill 后进程消失，下次 tick 清空 heatmap | 是 |
| heatmap + rnix ctx-profile | 同一 PID | 两个独立命令用不同 IPC 连接调用同一 CtxProfile | 否 |
| heatmap 刷新周期 | tick 500ms × 5 = 2.5s | 不影响 tick 频率，batch cmd 并行 | 是 |

### Project Structure Notes

修改文件：
- `cmd/rnix/dashboard.go` — 新增 heatmap 数据模型（heatmapSegment/segmentKind/activityLevel/heatmapProfileMsg）、fetchHeatmapCmd IPC 获取、buildHeatmapSegments 数据转换、renderHeatmapPane 渲染、handleHeatmapKey 交互、segmentColor 颜色计算、dim 辅助函数
- `cmd/rnix/dashboard_test.go` — 新增 13 个 heatmap 相关测试

不新增文件，不修改其他包。新增 `debug` 包导入到 dashboard.go。

### References

- [Source: cmd/rnix/dashboard.go:316] — `renderDashboardPlaceholder("Heatmap", ...)` 占位渲染点（替换为 renderHeatmapPane）
- [Source: cmd/rnix/dashboard.go:64-90] — dashboardModel 结构体（扩展 heatmap 字段）
- [Source: cmd/rnix/dashboard.go:105-127] — Update 方法（添加 heatmapProfileMsg 处理）
- [Source: cmd/rnix/dashboard.go:129-173] — dashboardTick 方法（添加 heatmap 刷新逻辑）
- [Source: cmd/rnix/dashboard.go:175-246] — dashboardKey 方法（添加 paneHeatmap 分支）
- [Source: cmd/rnix/dashboard.go:346-357] — renderDashboardStatus（添加 heatmap 快捷键提示）
- [Source: ipc/client.go:73-84] — CtxProfile IPC 客户端方法
- [Source: ipc/protocol.go:35] — MethodCtxProfile 常量
- [Source: debug/ctx_profile.go:36-46] — CtxProfileResult 结构定义
- [Source: debug/ctx_profile.go:48-61] — ClassificationResult 和 ClassBucket 结构
- [Source: debug/ctx_profile.go:63-70] — ConsumerEntry 结构（TopConsumers 的元素）
- [Source: debug/ctx_profile.go:88-112] — AnalyzeContext 分析函数（了解数据来源）
- [Source: internal/ui/styles.go:8-15] — 颜色常量
- [Source: cmd/rnix/dashboard.go:47] — colorIPC 常量
- [Source: cmd/rnix/dashboard.go:429-452] — renderDashboardPlaceholder（参考窗格渲染模式）
- [Source: _bmad-output/planning-artifacts/epics/epic-17-可视化调试面板-visual-debugging-dashboard.md] — Epic 17 AC
- [Source: _bmad-output/implementation-artifacts/17-1-dashboard-framework-and-agent-tree-pane.md] — 17-1 前序 story
- [Source: _bmad-output/implementation-artifacts/17-2-tracing-timeline-pane.md] — 17-2 前序 story
- [Source: _bmad-output/project-context.md] — 项目规则

### 技术栈

- Go 1.26 标准库
- `charm.land/bubbletea/v2 v2.0.0` — TUI 框架
- `github.com/charmbracelet/lipgloss v1.1.0` — 终端样式
- `github.com/rnixai/rnix/debug` — CtxProfileResult 结构（新增 import）
- `github.com/rnixai/rnix/ipc` — IPC 客户端（已有 import）
- 无新增外部依赖

### 前置 Story 学习总结

**来自 17-1 实现：**
1. bubbletea v2 API：`View()` 返回 `tea.View`（非 string），`tea.NewView()` + `v.AltScreen = true`
2. 窗格布局：lipgloss `Border(RoundedBorder())` + `BorderForeground(color)` + `Width/Height`
3. `dashboardModel` 核心字段已就位：`activePane`/`selectedPID`/`processes`/`treeRows` 等
4. `paneType` 常量：`paneTree`=0, `paneTimeline`=1, `paneHeatmap`=2
5. Tab 切换 `activePane = (activePane + 1) % 3`
6. 键盘路由通过 `if m.activePane == paneXxx { ... }` 分支
7. `renderDashboardPlaceholder` 用于占位渲染，heatmap 替换此调用
8. Kill 确认流程和 IPC 重连机制已实现

**来自 17-2 实现：**
1. 异步数据获取模式：`tea.Cmd` 中创建独立 IPC 连接，通过消息类型（xxxMsg）回传数据
2. `startTimelineCmd` → `timelineStreamStartedMsg` → `waitTimelineEventCmd` 循环模式（流式）
3. heatmap 用更简单的请求-响应模式（`fetchHeatmapCmd` → `heatmapProfileMsg`），不需要流式
4. PID 变化时清空旧数据：`m.timelineEvents = nil`、`m.timelineCursor = 0`
5. 事件分类用纯函数（`classifySyscall`），heatmap 也用纯函数（`buildHeatmapSegments`）
6. 颜色本地定义：`const colorIPC = "#9B59B6"` — heatmap 暗色也用本地定义
7. CJK rune 截断：使用 `utf8.RuneCountInString` + `[]rune` 截取

**来自 17-1 Code Review 修复：**
1. `colorState()` 函数添加了进程状态着色
2. `InitStyles` 在 `runDashboard` 中调用
3. 可见行数 off-by-one 修正为 `m.height - 7`

**来自 Git 分析：**
- 最新 commit：`feat: stroy 17-2 done`
- commit 模式：`feat: complete story X-Y - description`
- 最近工作集中在 dashboard TUI 开发

## Dev Agent Record

### Agent Model Used

Claude claude-4.6-opus-high-thinking

### Debug Log References

无 — 全部测试一次通过

### Completion Notes List

- ✅ Task 1-6: 实现 heatmap 数据模型（segmentKind/activityLevel/heatmapSegment 类型已在 ATDD 阶段定义）、segmentColor 颜色计算（6 种来源基色 + 活跃度暗色变体通过 dim() 函数）、mapConsumerKind 来源映射（system_prompt/user/assistant/tool:* 分类）、estimateActivity 活跃度估算（基于 Classification 四温分布和排名）
- ✅ Task 2: fetchHeatmapCmd 独立 IPC 连接获取 CtxProfile（请求-响应式，非流式）、heatmapProfileMsg 在 Update 中存储 profile 并触发 buildHeatmapSegments、dashboardTick 中 heatmapTickCount 递增 + PID 变化/每 5 tick 周期刷新
- ✅ Task 3: buildHeatmapSegments 从 TopConsumers 生成分段 + Leaked 额外段 + <3% 合并为 "Other" + 按 token 降序排序
- ✅ Task 4: renderHeatmapPane 替换 placeholder — 标题行(PID/token/budget) + treemap 色块条(宽度∝占比, 颜色=来源+活跃度) + 分段列表(▸选中标记) + 选中段详情(token/pct/activity/summary) + 空状态("Select an agent"/"Loading...")
- ✅ Task 5: handleHeatmapKey j/k/up/down 移动 heatmapCursor + dashboardKey 路由 paneHeatmap 分支
- ✅ Task 6: renderDashboardStatus 添加 paneHeatmap 快捷键提示 "j/k:Select Segment  Enter:Details"
- ✅ Task 7: 全部 13 个 ATDD 测试通过，零回归（2 个预存在 TTY 测试不影响）
- ✅ dashboardTick 重构为 cmds 收集模式（tea.Batch），支持 timeline + heatmap 并行 cmd

### Change Log

- 2026-03-09: 实现 Story 17-3 上下文热力图窗格，全部 7 个 Task 完成，13 个测试通过

### File List

- `cmd/rnix/dashboard.go` — 新增 heatmap 实现：segmentKindLabel/activityLabel/mapConsumerKind/dim/segmentColor/estimateActivity/buildHeatmapSegments/fetchHeatmapCmd/handleHeatmapPIDChange/handleHeatmapKey/renderHeatmapPane；修改 Update(heatmapProfileMsg)/dashboardTick(heatmapTickCount+刷新)/dashboardKey(paneHeatmap)/renderDashboardStatus(heatmap hint)/renderDashboard(替换 placeholder)
- `cmd/rnix/dashboard_test.go` — 13 个 ATDD 测试（红相已存在，本次实现使其全部绿色通过）
