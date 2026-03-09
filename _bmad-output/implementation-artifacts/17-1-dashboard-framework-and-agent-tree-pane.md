# Story 17.1: Dashboard 框架与智能体树窗格

Status: ready-for-dev

## Story

As a 平台构建者,
I want 通过 `rnix dashboard` 启动全屏 TUI 面板，在智能体树窗格中实时查看所有进程的状态,
So that 我可以纵览整个系统的运行状态。

## Acceptance Criteria

1. **Given** 系统中有运行中的智能体
   **When** 用户执行 `rnix dashboard`
   **Then** 启动全屏 bubbletea TUI 应用，默认显示多窗格视图

2. **Given** 智能体树窗格
   **When** 进程状态发生变化
   **Then** 实时显示进程父子关系、状态（Running/Zombie/Dead）、当前执行阶段和 token 消耗
   **And** TUI 刷新间隔 <= 500ms，10 并发进程 CPU <= 10%（NFR36）
   **And** 支持 >= 50 进程节点无明显卡顿（NFR37）

## Tasks / Subtasks

- [ ] Task 1: Dashboard 命令注册和基础框架 (AC: #1)
  - [ ] 1.1 在 `cmd/rnix/dashboard.go` 中创建 `dashboardCmd` Cobra 命令（`rnix dashboard`）
  - [ ] 1.2 在 `cmd/rnix/main.go` 中通过 `rootCmd.AddCommand(dashboardCmd)` 注册命令
  - [ ] 1.3 实现 `runDashboard` 函数：IPC 连接 → 构造 dashboardModel → 启动 `tea.NewProgram` → `p.Run()`
  - [ ] 1.4 处理 daemon 不可用场景：友好错误提示并退出

- [ ] Task 2: Dashboard Model 核心结构 (AC: #1, #2)
  - [ ] 2.1 定义 `dashboardModel` 结构体：client（IPC）、width/height（终端尺寸）、activePane（当前焦点窗格）、selectedPID（选中的进程 PID）、processes（进程列表）、treeRows（扁平化树行）、treeCursor（树光标位置）、connected/err/statusMsg 状态
  - [ ] 2.2 定义 `paneType` 常量：`paneTree`、`paneTimeline`、`paneHeatmap`（后两者 17-2/17-3 实现，本 story 只定义占位）
  - [ ] 2.3 定义 `treeNode` 和 `flatRow` 结构体（可复用 top.go 的设计，但增加 depth 字段支持折叠）
  - [ ] 2.4 实现 `newDashboardModel(client)` 构造函数

- [ ] Task 3: Init/Update/View 三件套 (AC: #1, #2)
  - [ ] 3.1 实现 `Init()` → 返回 `tickCmd()`（500ms 定时刷新）
  - [ ] 3.2 实现 `Update(msg)` 消息处理：
    - `tickMsg` → 调用 `ListProcs()` → 构建进程树 → 刷新 treeRows → 返回下一次 tickCmd
    - `tea.WindowSizeMsg` → 更新 width/height → 触发重新布局
    - `tea.KeyPressMsg` → 分发到键盘处理函数
  - [ ] 3.3 实现 `View()` → 返回 `tea.View`（`AltScreen = true`），渲染多窗格布局

- [ ] Task 4: 多窗格布局系统 (AC: #1)
  - [ ] 4.1 实现 `renderLayout()` 函数：使用 lipgloss 将终端区域分割为左侧智能体树窗格（宽度 40%）和右侧区域（上下两个占位窗格各 50%）
  - [ ] 4.2 实现窗格边框渲染：使用 lipgloss `Border()` 为每个窗格绘制边框，活跃窗格用 `ColorAgent` 高亮边框
  - [ ] 4.3 实现标题栏渲染：顶部显示 "Rnix Dashboard" + 连接状态 + 活跃进程数 + 总 token 消耗
  - [ ] 4.4 实现底部状态栏：显示快捷键提示（q=退出, Tab=切换窗格, k=kill, j/k=上下, Enter=选中）
  - [ ] 4.5 右侧占位窗格显示 "Timeline (Coming Soon)" 和 "Heatmap (Coming Soon)" 文本

- [ ] Task 5: 智能体树窗格实现 (AC: #2)
  - [ ] 5.1 实现 `buildProcessTree(procs []vfs.ProcInfo) []*treeNode` 从扁平列表构建树（基于 PPID 关系）
  - [ ] 5.2 实现 `flattenTree(nodes []*treeNode) []flatRow` 将树扁平化为带缩进前缀的行列表（`├─`/`└─` 风格）
  - [ ] 5.3 实现 `renderTreePane()` 函数：渲染进程树表格，每行显示：
    - 树形缩进前缀 + PID
    - State（Running=绿/Zombie=黄/Dead=灰 着色）
    - Agent 名称（Skills 列表缩略）
    - Token 消耗（含 budget 百分比，≥80% 用 WarningStyle）
    - Elapsed 时间
  - [ ] 5.4 实现光标导航：j/k 或 ↑/↓ 移动光标，当前行用反色高亮
  - [ ] 5.5 实现 selectedPID 同步：光标移动时自动更新 selectedPID，为后续窗格联动做准备
  - [ ] 5.6 实现滚动：当进程数超过可见行时，自动滚动视口跟随光标

- [ ] Task 6: 键盘交互 (AC: #1, #2)
  - [ ] 6.1 全局按键：q/Ctrl+C 退出，Tab 切换焦点窗格
  - [ ] 6.2 智能体树按键：j/k/↑/↓ 导航，Enter 选中/展开详情，K（大写）kill 选中进程（需确认）
  - [ ] 6.3 Kill 确认流程：按 K 后显示确认提示 "Kill PID X? [y/N]"，y 确认执行 `client.Kill(pid)`，其他键取消
  - [ ] 6.4 Daemon 断连重连：tick 中检测 IPC 错误 → 设置 connected=false → 显示重连中提示 → 下次 tick 尝试重新 ListProcs

- [ ] Task 7: Dashboard 样式扩展 (AC: #1, #2)
  - [ ] 7.1 在 `internal/ui/styles.go` 中新增 Dashboard 专用颜色常量（如果需要，但优先复用现有颜色体系）
  - [ ] 7.2 定义窗格边框样式：普通边框（ColorMuted）、活跃边框（ColorAgent）
  - [ ] 7.3 定义进程状态着色映射：Running→ColorSuccess, Zombie→ColorWarning, Dead→ColorMuted, Created→ColorKernel
  - [ ] 7.4 确保 `InitStyles()` 在 dashboard 启动前被调用

- [ ] Task 8: 测试 (AC: #1, #2)
  - [ ] 8.1 `cmd/rnix/dashboard_test.go`：dashboardModel Init — 返回 tickCmd
  - [ ] 8.2 `cmd/rnix/dashboard_test.go`：dashboardModel Update(tickMsg) — mock ListProcs，验证进程树构建
  - [ ] 8.3 `cmd/rnix/dashboard_test.go`：dashboardModel Update(KeyPressMsg) — 验证 j/k 导航更新 cursor
  - [ ] 8.4 `cmd/rnix/dashboard_test.go`：dashboardModel Update(KeyPressMsg 'q') — 返回 tea.Quit
  - [ ] 8.5 `cmd/rnix/dashboard_test.go`：buildProcessTree — 扁平列表→树结构（含多层嵌套）
  - [ ] 8.6 `cmd/rnix/dashboard_test.go`：buildProcessTree — 空列表→空树
  - [ ] 8.7 `cmd/rnix/dashboard_test.go`：flattenTree — 验证缩进前缀正确性（├─/└─）
  - [ ] 8.8 `cmd/rnix/dashboard_test.go`：Tab 切换 activePane
  - [ ] 8.9 `cmd/rnix/dashboard_test.go`：Kill 确认流程 — K → y 执行 kill
  - [ ] 8.10 `cmd/rnix/dashboard_test.go`：Kill 确认流程 — K → n 取消
  - [ ] 8.11 `cmd/rnix/dashboard_test.go`：Daemon 断连 → connected=false → 重连成功
  - [ ] 8.12 `cmd/rnix/dashboard_test.go`：50 进程节点渲染无 panic（NFR37 验证）
  - [ ] 8.13 `internal/ui/styles_test.go`：新增样式常量和 Dashboard 边框样式验证（如果新增了样式）

## Dev Notes

### 架构决策

本 story 是 Epic 17（可视化调试面板）的基础框架层，建立 dashboard 的整体架构和首个功能窗格（智能体树）。核心设计原则：

1. **复用 `rnix top` 架构模式** — dashboard 与 top 共享相同的 bubbletea v2 TUI 架构：`tea.Model` 接口（Init/Update/View）、500ms tick 轮询 IPC、`tea.NewView` 返回 `tea.View`（不是 `string`）、`AltScreen = true`。这确保了项目内 TUI 代码的一致性。
2. **多窗格布局用纯 lipgloss 实现** — 不引入 bubbles（go.mod 中没有 bubbles 依赖），布局使用 lipgloss 的 `Width()`/`Height()`/`MaxWidth()`/`MaxHeight()` + `lipgloss.JoinHorizontal`/`lipgloss.JoinVertical` 组合。这是项目现有技术栈约束的自然选择。
3. **IPC 轮询而非订阅** — 数据获取使用 `ListProcs()` 轮询（每 500ms），不新增 IPC 方法。原因：(a) 与 `rnix top` 模式一致；(b) `ListProcs()` 已包含完整进程信息；(c) 轮询在 50 进程规模下性能完全可接受。
4. **预留窗格扩展点** — `paneType` 常量和 `activePane` 字段在 17-1 中定义，17-2（时间线）和 17-3（热力图）实现时只需在 renderLayout 中填充对应窗格内容，不改动框架。

### 关键设计：bubbletea v2 API

**重要：项目使用 bubbletea v2**（`charm.land/bubbletea/v2 v2.0.0`），与 v1 有显著 API 差异：

```go
// View() 返回 tea.View 而非 string
func (m dashboardModel) View() tea.View {
    content := m.renderLayout()
    v := tea.NewView(content)
    v.AltScreen = true
    return v
}

// tea.Tick 用法
func tickCmd() tea.Cmd {
    return tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg {
        return tickMsg(t)
    })
}

// KeyPressMsg 结构（v2）
case tea.KeyPressMsg:
    switch {
    case msg.Code == tea.KeyTab:
    case msg.Code == 'q':
    case msg.Code == 'j' || msg.Code == tea.KeyDown:
    case msg.Code == 'k' || msg.Code == tea.KeyUp:
    case msg.Code == 'K' && msg.Mod == tea.ModShift:
    case msg.Code == tea.KeyEnter:
    }
```

参考 `cmd/rnix/top.go` 的完整实现了解 v2 API 的具体用法。`top.go` 是项目中唯一的 bubbletea 使用示例。

### 关键设计：多窗格布局

```
┌─────────────────────── Rnix Dashboard ────────────────────────┐
│  ● Connected | Processes: 5 | Tokens: 12.3k | Uptime: 1m23s  │
├──────────── Agent Tree ──────────┬────── Timeline ──────────┤
│ ├─ PID 1  Running  init   0tok  │                           │
│ ├─ PID 2  Running  review 3.2k  │    (Coming Soon)          │
│ │  └─ PID 5  Running  lint 1.1k │                           │
│ └─ PID 3  Zombie   build  8.0k  │                           │
│                                  ├────── Heatmap ───────────│
│                                  │                           │
│                                  │    (Coming Soon)          │
│                                  │                           │
├──────────────────────────────────┴───────────────────────────┤
│  q:Quit  Tab:Switch Pane  j/k:Navigate  K:Kill  Enter:Select │
└──────────────────────────────────────────────────────────────┘
```

布局计算：
- 标题栏：1 行 + 边框
- 底部状态栏：1 行 + 边框
- 智能体树窗格：宽度 = 终端宽度 * 40%（最小 30 列，最大 60 列）
- 右侧窗格：宽度 = 终端宽度 - 树窗格宽度
- 右侧上下分割：各占右侧高度的 50%

使用 `lipgloss.JoinHorizontal(lipgloss.Top, leftPane, rightPane)` 水平拼接，`lipgloss.JoinVertical(lipgloss.Left, topRight, bottomRight)` 垂直分割右侧。

### 关键设计：进程树构建

从 `ListProcs()` 返回的 `[]ProcInfoWire` 构建树：

```go
func buildProcessTree(procs []vfs.ProcInfo) []*treeNode {
    nodeMap := make(map[types.PID]*treeNode)
    for i := range procs {
        nodeMap[procs[i].PID] = &treeNode{proc: procs[i]}
    }
    var roots []*treeNode
    for _, node := range nodeMap {
        if parent, ok := nodeMap[node.proc.PPID]; ok && node.proc.PID != node.proc.PPID {
            parent.children = append(parent.children, node)
        } else {
            roots = append(roots, node)
        }
    }
    // 排序：每层按 PID 排序
    sortTreeNodes(roots)
    return roots
}
```

注意：`ProcInfoWire` 不包含 `Children` 字段，必须从 `PPID` 反向构建。PID==PPID 的情况（init 进程或自身）视为 root。

### 关键设计：滚动视口

当进程数超过窗格可见行数时，需要虚拟滚动：

```go
type dashboardModel struct {
    // ...
    treeCursor  int  // 光标在 flatRows 中的位置
    treeOffset  int  // 视口顶部在 flatRows 中的偏移
}

// 更新视口跟随光标
func (m *dashboardModel) ensureCursorVisible(visibleLines int) {
    if m.treeCursor < m.treeOffset {
        m.treeOffset = m.treeCursor
    }
    if m.treeCursor >= m.treeOffset+visibleLines {
        m.treeOffset = m.treeCursor - visibleLines + 1
    }
}
```

这保证了 50+ 进程时光标始终可见，且渲染只处理可见行（NFR37）。

### 关键复用点

1. **IPC 客户端** — 复用 `ipc.Client.ListProcs()` 和 `ipc.Client.Kill()`，与 `cmd/rnix/top.go` 和 `cmd/rnix/kill.go` 相同模式
2. **进程树构建** — `top.go` 已有 `buildTree`/`flattenTree` 逻辑（第 125-190 行），dashboard 可采用相同算法但独立实现（因为 dashboard 需要更多字段如 depth、折叠状态）
3. **样式系统** — 复用 `internal/ui/styles.go` 中的 `ColorSuccess`/`ColorWarning`/`ColorError`/`ColorMuted`/`ColorAgent`/`ColorKernel` 六色体系
4. **FormatDuration/FormatTokens** — 复用 `internal/ui/table.go` 的格式化函数
5. **renderState** — 复用 `internal/ui/table.go` 的 `renderState()` 函数做状态着色
6. **TerminalProfile** — 通过 `ui.DetectProfile()` 获取终端能力，传入 `ui.InitStyles()`
7. **tickMsg/tickCmd** — 与 `top.go` 相同的 tick 模式（500ms 间隔）

### 不要做的事情

- **不要**引入 `bubbles` 依赖 — go.mod 中没有 bubbles，使用纯 lipgloss 做布局
- **不要**新增 IPC 方法 — 完全复用 `ListProcs()` 和 `Kill()` 已有方法
- **不要**实现时间线窗格和热力图窗格的功能 — 本 story 只做占位，17-2 和 17-3 实现
- **不要**实现窗格联动 — 17-4 实现窗格间的选中联动
- **不要**实现离线回放 — 17-5 实现 `--load <record-dir>` 参数
- **不要**实现进程详情弹窗 — 如果需要详情，在 Enter 按下时在树窗格内展开（不弹窗），保持简单
- **不要**在 dashboard.go 中直接操作 Kernel — 所有操作通过 IPC Client
- **不要**修改 `top.go` — dashboard 是独立命令，不复用 top 的代码逻辑（但可参考其设计模式）
- **不要**修改 `ipc/` 包的任何文件
- **不要**创建 `internal/ui/dashboard/` 子包 — 所有 dashboard TUI 逻辑放在 `cmd/rnix/dashboard.go` 中（与 top.go 模式一致）
- **不要**使用 `.yml` 后缀
- **不要**使用全大写常量名（不用 `MAX_VISIBLE_ROWS`，用 `maxVisibleRows`）

### 组合矩阵

| 本功能 | 交互对象 | 交互方式 | 需验证 |
|--------|----------|----------|--------|
| rnix dashboard | ipc.Client.ListProcs | 轮询获取进程列表，与 rnix top 使用相同 IPC 方法 | 是 |
| rnix dashboard | ipc.Client.Kill | 在树窗格中 Kill 选中进程，通过 IPC 发送 kill 信号 | 是 |
| dashboardModel tick | tea.Tick(500ms) | 定时刷新，与 top.go 同间隔 | 是 |
| 进程树构建 | vfs.ProcInfo.PPID | 从 PPID 构建父子关系树 | 是 |
| 样式渲染 | internal/ui/styles | 复用 ColorSuccess/Warning/Error/Muted 着色进程状态 | 是 |
| FormatDuration/Tokens | internal/ui/table | 复用已有的格式化函数 | 已有测试 |
| Dashboard + Top | 互不干扰 | 两个独立命令，可同时运行（连接同一 daemon） | 是 |
| Dashboard + strace | 互不干扰 | dashboard 使用 ListProcs，strace 使用 AttachDebug，不同 IPC 方法 | 否 |
| Dashboard Kill | Daemon 进程回收 | Kill 后下一次 tick 刷新进程状态 | 是 |

### Project Structure Notes

新建文件：
- `cmd/rnix/dashboard.go` — dashboardCmd、dashboardModel、Init/Update/View、renderLayout、buildProcessTree、flattenTree、renderTreePane、键盘处理
- `cmd/rnix/dashboard_test.go` — 13 个单元测试

修改文件：
- `cmd/rnix/main.go` — 添加 `rootCmd.AddCommand(dashboardCmd)`
- `internal/ui/styles.go` — 可能新增 Dashboard 边框样式（仅在现有颜色不满足时）

对齐项目结构：
- `cmd/rnix/dashboard.go` 与 `cmd/rnix/top.go` 平级，遵循 CLI 命令单文件模式
- 不创建新的包或子目录

### References

- [Source: cmd/rnix/top.go] — bubbletea v2 TUI 参考实现：topModel Init/Update/View、tickCmd、treeNode、flatRow、buildTree、flattenTree、键盘处理、IPC ListProcs 轮询
- [Source: cmd/rnix/top_test.go] — bubbletea v2 测试模式：tea.KeyPressMsg、tea.ModShift、model assertion
- [Source: internal/ui/styles.go] — 颜色常量（ColorKernel/Agent/Success/Warning/Error/Muted）和样式初始化
- [Source: internal/ui/table.go] — FormatDuration、FormatTokens、FormatSkills、renderState 格式化函数
- [Source: internal/ui/renderer.go] — DetectProfile、TerminalProfile 终端能力检测
- [Source: ipc/client.go:ListProcs()] — IPC 获取进程列表方法
- [Source: ipc/client.go:Kill()] — IPC kill 进程方法
- [Source: ipc/protocol.go:86-99] — ProcInfoWire 结构定义（PID/PPID/State/Intent/Skills/TokensUsed/CreatedAt/DeadAt/CtxID/Result/ContextBudget/MaxSteps）
- [Source: go.mod:5-14] — 依赖：bubbletea v2.0.0、lipgloss v1.1.0（无 bubbles）
- [Source: _bmad-output/planning-artifacts/epics/epic-17-可视化调试面板-visual-debugging-dashboard.md] — Epic 17 原始需求
- [Source: _bmad-output/planning-artifacts/prd/functional-requirements.md:166-171] — FR90（dashboard 启动）、FR91（智能体树窗格）
- [Source: _bmad-output/planning-artifacts/prd/non-functional-requirements.md:69-70] — NFR36（TUI 刷新 ≤ 500ms，CPU ≤ 10%）、NFR37（≥50 进程无卡顿）
- [Source: _bmad-output/planning-artifacts/ux-design-specification.md:414-415] — 可视化调试面板使用 bubbletea + 自定义绘图组件
- [Source: _bmad-output/planning-artifacts/architecture/core-architectural-decisions.md:20] — 延迟决策：可视化 Dashboard 的 TUI 布局框架（bubbletea 组件设计）
- [Source: _bmad-output/project-context.md] — 项目上下文规则：Go 1.26、lipgloss + bubbles（Charm 生态）、测试规则、命名约定

### 技术栈

- Go 1.26 标准库
- `charm.land/bubbletea/v2 v2.0.0` — 全屏 TUI 框架
- `github.com/charmbracelet/lipgloss v1.1.0` — 终端样式和布局
- `time` — tick 间隔和耗时计算
- `sort` — 进程树节点排序
- 无新增外部依赖

### 前置 Story 学习总结

**来自 Epic 16 回顾：**
1. "复用而非扩展"策略 — dashboard 完全复用 ListProcs()/Kill() IPC 方法，不新增协议
2. 接口定义在使用方 — 如果需要 mock IPC 客户端用于测试，在 dashboard_test.go 中定义接口
3. CLI 薄壳模式 — dashboard TUI 逻辑放在 cmd/rnix/dashboard.go，与 top.go 模式一致
4. 零新增外部依赖 — 使用现有的 bubbletea + lipgloss 技术栈

**来自 `rnix top`（cmd/rnix/top.go）：**
1. bubbletea v2 API：`View()` 返回 `tea.View`（不是 `string`），`tea.NewView()` + `v.AltScreen = true`
2. 500ms tick 轮询 IPC 获取进程列表（`ListProcs()`）
3. 进程树从 PPID 构建：`buildTree` 创建 `treeNode` 树，`flattenTree` 扁平化为 `flatRow` 列表
4. 键盘处理：`tea.KeyPressMsg` 的 `Code`/`ShiftedCode`/`Mod` 字段
5. 重连机制：tick 中检测 IPC 错误后设置 connected=false，下次 tick 自动重试
6. 状态着色：Running 绿色、Zombie 黄色、其他灰色
7. Token 警告：usage ≥ 80% budget 时用 `ui.WarningStyle`

**来自 Git 分析（最近 commit）：**
- commit 模式：`feat: complete story X-Y - description`
- 最近完成：Epic 16（agtest 框架），3 个 story 全部 done

**Epic 16 回顾的关键准备项：**
1. bubbletea TUI 架构设计 — 已通过分析 top.go 完成
2. 实时数据订阅模式 — 确定使用轮询模式（与 top 一致），不新增 IPC 协议
3. NFR36/NFR37 性能 — 通过虚拟滚动（只渲染可见行）和 500ms 轮询间隔满足
4. lipgloss 样式系统 — 复用现有 6 色体系

### 性能考量

- **NFR36**：TUI 刷新间隔 500ms，ListProcs IPC 调用延迟 ~10ms（NFR2），渲染 ~1ms → 远低于 500ms 限制。CPU 开销：每 500ms 一次 IPC + 一次树构建 + 一次视图渲染，10 个进程场景下 CPU 占用 << 10%
- **NFR37**：50 进程无卡顿 — 树构建 O(n) + 扁平化 O(n) + 渲染只处理可见行（虚拟滚动）。50 个 treeNode 的内存和计算量可忽略

### Mock 测试策略

Dashboard 测试使用与 `top_test.go` 相同的模式：

1. 不需要定义 mock 接口 — bubbletea model 测试直接构造 `dashboardModel` 并调用 `Update()`/`View()`
2. `dashboardModel` 中的 IPC 客户端可通过构造函数注入 — 测试时设置 `processes` 字段绕过 IPC 调用
3. buildProcessTree 和 flattenTree 是纯函数，可直接单元测试
4. 键盘处理测试：构造 `tea.KeyPressMsg` → 调用 `Update` → 检查 model 状态变化

参考 `top_test.go` 的测试模式：

```go
func TestDashboardModel_NavigateDown(t *testing.T) {
    m := newTestDashboardModel(mockProcs)
    msg := tea.KeyPressMsg{Code: 'j'}
    updated, _ := m.Update(msg)
    model := updated.(dashboardModel)
    assert.Equal(t, 1, model.treeCursor)
}
```

## Dev Agent Record

### Agent Model Used

{{agent_model_name_version}}

### Debug Log References

### Completion Notes List

### File List
