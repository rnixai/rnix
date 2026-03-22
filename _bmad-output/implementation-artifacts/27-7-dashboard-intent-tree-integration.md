# Story 27.7: Dashboard 意图树集成

Status: done

## Story

As a 平台构建者,
I want 在 dashboard 中查看 Intent DAG 可视化窗格,
so that 我可以了解意图分解结构，通过节点状态着色快速识别问题子任务。

## Acceptance Criteria (AC)

### AC-1: 新增意图树窗格（paneIntent）

**Given** dashboard 现有四个窗格（Tree=0 / Timeline=1 / Heatmap=2 / Detail=3）
**When** 新增意图树窗格
**Then** `paneIntent paneType = 4` 加入 iota 序列
**And** Tab 切换顺序变为 Tree → Timeline → Heatmap → Detail → Intent → Tree
**And** Tab 取模值从 `% 4` 变为 `% 5`
**And** 当 Intent 窗格激活时，边框高亮显示

### AC-2: 意图树数据获取

**Given** dashboard 的 Intent 窗格激活
**When** 需要获取意图数据
**Then** 通过 `client.IntentList()` IPC 方法获取所有 IntentTreeWire 列表
**And** 过滤出活跃的（非 completed/failed 终态的）意图树用于主要显示
**And** 已完成/失败的意图树在底部以折叠状态显示
**And** 数据加载延迟 ≤ 1s（NFR63-obs）

### AC-3: 意图树 DAG 可视化

**Given** 进程通过 `rnix apply` 声明式意图创建
**When** 用户在 dashboard 中切换到意图树窗格
**Then** 以缩进树状结构展示意图分解层级
**And** 每个节点显示：节点 ID + 意图摘要（截断到 40 字符）+ 状态图标 + PID（若有）
**And** 节点按状态着色：
  - pending = 灰色（`lipgloss.Color("240")`）
  - decomposing = 黄色（`lipgloss.Color("220")`）
  - await_confirm = 黄色闪烁（`lipgloss.Color("220")`）
  - executing = 蓝色（`lipgloss.Color("39")`）
  - completed = 绿色（`lipgloss.Color("42")`）
  - failed = 红色（`lipgloss.Color("196")`）
  - retrying = 橙色（`lipgloss.Color("208")`）

### AC-4: 节点选择与进程联动

**Given** 意图树中某个节点（且该节点有关联 PID）
**When** 用户通过 j/k 选中该节点并按 Enter
**Then** 联动切换 `selectedPID` 到对应进程
**And** 时间线和上下文视图自动切换到对应进程的数据
**And** 切换到 Timeline 窗格

**Given** 意图树中某个节点（PID 为 0，尚未 spawn）
**When** 用户选中该节点并按 Enter
**Then** 显示状态消息 "该节点尚未分配进程"
**And** 不切换 selectedPID

### AC-5: 空状态处理

**Given** 没有活跃的意图分解（普通 spawn 启动，或 IntentList 返回空）
**When** 用户切换到意图树窗格
**Then** 显示空状态提示："当前无意图分解任务。使用 rnix apply 创建声明式意图。"

### AC-6: 多意图树支持

**Given** 系统中存在多个活跃的意图树
**When** 用户在意图树窗格中查看
**Then** 每棵意图树以根意图为标题，用分隔线分开
**And** 用户可通过 j/k 在所有树的节点间连续导航
**And** 当前选中节点所在的意图树高亮标题

### AC-7: 意图树窗格渲染性能

**Given** 意图树窗格显示中
**When** 数据刷新或窗格切换
**Then** 窗格切换渲染延迟 ≤ 100ms（NFR63-obs）
**And** 数据获取延迟 ≤ 1s（NFR63-obs）

## Tasks / Subtasks

- [x] Task 1: 新增 paneIntent 常量与 Tab 切换扩展（AC: #1）
  - [x] 1.1 在 `cmd/rnix/dashboard.go` 的 paneType iota 中新增 `paneIntent = 4`
  - [x] 1.2 修改 Tab 切换 `% 4` → `% 5`
  - [x] 1.3 更新 renderDashboardStatus() 帮助文本，加入 Intent 窗格说明
- [x] Task 2: 意图数据获取与缓存（AC: #2）
  - [x] 2.1 在 dashboardModel 新增字段：`intentTrees []*ipc.IntentTreeWire`、`intentTreeErr error`、`intentFlatNodes []intentFlatNode`、`intentCursor int`
  - [x] 2.2 定义 `intentFlatNode` 结构体（treeIndex int, nodeID string, indent int, node *ipc.IntentNodeWire, isTreeHeader bool, treeWire *ipc.IntentTreeWire）
  - [x] 2.3 实现 `fetchIntentTreesCmd()` tea.Cmd（调用 client.IntentList()）
  - [x] 2.4 定义 `intentTreesMsg` 消息类型，在 Update 中处理响应
  - [x] 2.5 在 dashboardTick 中周期性刷新（每 5 ticks，仅 Intent 窗格激活时）
- [x] Task 3: 意图树渲染（AC: #3, #5, #6）
  - [x] 3.1 实现 `flattenIntentTrees()` 方法：将多棵 IntentTreeWire 展平为 intentFlatNode 列表（含拓扑排序层级 → 缩进）
  - [x] 3.2 实现 `renderIntentPane()` 方法：缩进树渲染 + 状态着色 + 光标高亮
  - [x] 3.3 实现空状态渲染（无意图时显示提示文字）
  - [x] 3.4 在 View() 的 paneIntent 分支中调用 renderIntentPane()
- [x] Task 4: 节点选择与联动（AC: #4）
  - [x] 4.1 在 paneIntent 下处理 j/k 键移动 intentCursor
  - [x] 4.2 在 paneIntent 下处理 Enter 键：提取选中节点的 PID，联动 selectedPID 并切换到 Timeline
  - [x] 4.3 PID=0 时显示状态消息 "该节点尚未分配进程"
- [x] Task 5: 测试（AC: #1-#7）
  - [x] 5.1 ATDD 测试：意图树窗格 Tab 切换、空状态渲染
  - [x] 5.2 ATDD 测试：flattenIntentTrees 拓扑层级正确性
  - [x] 5.3 ATDD 测试：节点选择与 PID 联动

## Dev Notes

### 关键设计决策

**为什么复用 IntentList 而非新增 IPC 方法？**
现有 `client.IntentList()` 已返回所有 IntentTreeWire（含节点、状态、依赖关系），数据完整度足够用于 dashboard 可视化。无需新增 IPC 方法。

**为什么用展平列表而非真正的 2D 树渲染？**
BubbleTea 的文本渲染模型最适合一维行列表 + 光标导航。将 DAG 展平为带缩进的行列表（类似 `tree` 命令输出）是 TUI 中最成熟的模式，也与 dashboard 现有的 TreePane / StepTimeline 模式一致。

### 现有代码模式（必须遵循）

**Dashboard 窗格添加模式**（参考 Story 27-6 Detail 窗格添加）：
1. `paneType` 是 int 类型，使用 iota：`paneTree=0, paneTimeline=1, paneHeatmap=2, paneDetail=3`
2. 新增 `paneIntent = 4`（iota 自动递增）
3. Tab 切换：`m.activePane = (m.activePane + 1) % 5`（当前为 `% 4`，需改为 `% 5`）
4. View 中：根据 `m.activePane` 选择渲染函数
5. 边框高亮：`activePaneStyle` vs `inactivePaneStyle`

**IPC 异步调用模式**（参考 `fetchProcDetailCmd`）：
```go
func fetchIntentTreesCmd() tea.Cmd {
    return func() tea.Msg {
        client, err := ipc.Dial(ipc.SocketPath())
        if err != nil {
            return intentTreesMsg{err: err}
        }
        defer client.Close()
        resp, err := client.IntentList()
        return intentTreesMsg{trees: resp, err: err}
    }
}
```

**消息处理模式**（参考 `procDetailResultMsg`）：
```go
type intentTreesMsg struct {
    trees *ipc.IntentStatusResponse
    err   error
}
// 在 Update 中：
case intentTreesMsg:
    if msg.err != nil {
        m.intentTreeErr = msg.err
        return m, nil
    }
    m.intentTrees = msg.trees.Intents
    m.intentFlatNodes = flattenIntentTrees(m.intentTrees)
    // 保持 intentCursor 在范围内
```

**空状态渲染模式**（参考 `renderDetailPane`）：
```go
if len(m.intentFlatNodes) == 0 {
    b.WriteString("\n    当前无意图分解任务。使用 rnix apply 创建声明式意图。")
    return style.Render(b.String())
}
```

### Intent 系统 IPC 类型（已存在，直接使用）

```go
// ipc/protocol.go — 已有类型，无需新增
type IntentTreeWire struct {
    ID            string                      `json:"id"`
    RootIntent    string                      `json:"root_intent"`
    State         string                      `json:"state"`
    Nodes         map[string]*IntentNodeWire  `json:"nodes"`
    Drifts        []DriftItemWire             `json:"drifts"`
    CreatedAtMs   int64                       `json:"created_at_ms"`
    CompletedAtMs int64                       `json:"completed_at_ms,omitempty"`
}

type IntentNodeWire struct {
    ID         string   `json:"id"`
    Intent     string   `json:"intent"`
    Agent      string   `json:"agent"`
    Model      string   `json:"model"`
    DependsOn  []string `json:"depends_on"`
    State      string   `json:"state"`
    PID        uint64   `json:"pid"`
    Result     string   `json:"result"`
    Error      string   `json:"error"`
    RetryCount int      `json:"retry_count"`
    MaxRetries int      `json:"max_retries"`
    TimeoutMs  int64    `json:"timeout_ms"`
}

// ipc/client.go — 已有方法，直接调用
func (c *Client) IntentList() (*IntentStatusResponse, error)
```

### DAG 展平算法

将 `IntentTreeWire.Nodes`（map 结构）展平为带缩进层级的有序列表：

```go
type intentFlatNode struct {
    treeIndex    int                    // 所属意图树在 intentTrees 中的索引
    nodeID       string                 // 节点 ID
    indent       int                    // 缩进层级（0=根层级）
    node         *ipc.IntentNodeWire    // 节点数据指针
    isTreeHeader bool                   // 是否为意图树标题行
    treeWire     *ipc.IntentTreeWire    // 所属意图树
}

func flattenIntentTrees(trees []*ipc.IntentTreeWire) []intentFlatNode {
    // 对每棵树：
    // 1. 添加 isTreeHeader=true 的标题行（显示 RootIntent + State）
    // 2. 构建依赖图：遍历所有 node 的 DependsOn
    // 3. 找出根节点（DependsOn 为空的节点）
    // 4. BFS/拓扑排序展开：
    //    - 第 0 层：根节点（indent=0）
    //    - 第 1 层：仅依赖根节点的节点（indent=1）
    //    - 依此类推
    // 5. 同层节点按 ID 字母序排列
}
```

**关键实现要点：**
- 节点间依赖关系来自 `IntentNodeWire.DependsOn []string`
- `DependsOn` 中的 string 是其他节点的 ID
- 需要构建反向索引（`dependedBy map[string][]string`）来确定子节点
- 使用类似 BFS 的层级遍历来确定缩进深度
- 注意处理没有 `DependsOn` 的孤立节点（作为根节点处理）

### 节点状态着色映射

```go
func intentStateColor(state string) lipgloss.Color {
    switch state {
    case "pending":
        return lipgloss.Color("240")   // 灰色
    case "decomposing":
        return lipgloss.Color("220")   // 黄色
    case "await_confirm":
        return lipgloss.Color("220")   // 黄色
    case "executing":
        return lipgloss.Color("39")    // 蓝色
    case "completed":
        return lipgloss.Color("42")    // 绿色
    case "failed":
        return lipgloss.Color("196")   // 红色
    case "retrying":
        return lipgloss.Color("208")   // 橙色
    default:
        return lipgloss.Color("240")   // 默认灰色
    }
}
```

### 节点渲染格式

```
┌─ Intent Tree ─────────────────────────────┐
│ ▶ Intent: 分析项目结构并生成报告 [executing]  │
│   ├─ task-1: 扫描目录结构 ✓ (PID:3)        │
│   ├─ task-2: 分析依赖关系 ⟳ (PID:5)        │
│   │  └─ task-2a: 解析 go.mod ● (PID:7)    │
│   └─ task-3: 生成报告 ○ (等待 task-1,2)     │
│                                            │
│ ▶ Intent: 优化性能 [completed] ✓           │
│   ├─ opt-1: 分析瓶颈 ✓                    │
│   └─ opt-2: 实施优化 ✓                    │
└────────────────────────────────────────────┘
```

状态图标：
- `✓` completed（绿色）
- `⟳` executing（蓝色）
- `○` pending（灰色）
- `✗` failed（红色）
- `↻` retrying（橙色）
- `?` await_confirm（黄色）
- `…` decomposing（黄色）

### 渲染布局位置

意图树窗格在 dashboard View() 中的位置选择：
- 当 `activePane == paneIntent` 时，**替换右下区域**（与 Heatmap/Detail 共享空间）
- 右上始终是 Timeline，右下根据 activePane 显示 Heatmap / Detail / Intent 之一
- 实际上现有代码已实现根据 activePane 选择右下区域内容的逻辑——参考 renderDetailPane 是如何在 paneDetail 激活时替代 Heatmap 区域的

**注意：** 需要确认 View() 中右下区域的渲染逻辑。查看 View() 中 paneDetail 的条件分支，Intent 窗格应采用完全相同的模式。

### 帮助文本

renderDashboardStatus() 需要新增 Intent 窗格的帮助行：
```go
case paneIntent:
    ops = "j/k:Navigate  Enter:Jump to Process"
```

### 修改文件清单

| 文件 | 修改类型 | 说明 |
|------|----------|------|
| `cmd/rnix/dashboard.go` | 修改 | 新增 paneIntent(4)、fetchIntentTreesCmd、renderIntentPane、flattenIntentTrees、intentStateColor、帮助文本更新、Tab 取模更新 |
| `cmd/rnix/atdd_27_7_dashboard_intent_tree_test.go` | 新增 | ATDD 验收测试 |

### 不需要修改的文件

- `ipc/protocol.go` — IntentTreeWire/IntentNodeWire/IntentStatusResponse 已存在
- `ipc/client.go` — IntentList() 方法已存在
- `ipc/server.go` — handleIntentList handler 已存在
- `intent/` 包 — 所有核心类型和 DAG 逻辑已存在

### 依赖关系

- **Story 27.3-27.6**（Dashboard 窗格模式）：参考 Tab 切换、窗格渲染、IPC 获取模式，均已完成
- **Epic 19**（Intent 系统）：IntentTree/IntentNode/Reconciler/DAG 全部已实现
- **IPC Intent 方法**：`intent_list` / `intent_status` 已注册，client 方法已就位

### 防踩坑清单

1. **Tab 取模值必须更新** — 从 `% 4` 改为 `% 5`，搜索所有 `% 4` 确保无硬编码遗漏
2. **IntentNodeWire.PID 是 uint64** — 需要转换为 `types.PID`（`types.PID(node.PID)`）
3. **IntentTreeWire.Nodes 是 map** — map 遍历无序，必须通过拓扑排序或 ID 排序确保稳定渲染
4. **DependsOn 可能引用不存在的 nodeID** — 展平时做容错处理，跳过不存在的依赖
5. **CJK 字符截断** — Intent 描述文本截断时需按 rune 计数，不按 byte
6. **intentCursor 越界** — 在 intentTrees 刷新后，确保 `intentCursor < len(intentFlatNodes)`
7. **PID=0 的节点** — pending/decomposing 状态的节点尚未 spawn，PID 为 0，Enter 时不联动
8. **空 Nodes map** — IntentTreeWire.Nodes 可能为空（刚创建的意图），应显示 "(分解中...)"
9. **RNIX_ASCII 环境变量** — 当 `RNIX_ASCII=1` 时，状态图标需退化为 ASCII 字符（参考现有 UI ASCII 降级模式）
10. **帮助行更新** — 底部帮助文字需加入 Intent 窗格说明，参考 paneDetail 的模式

### 前序 Story 经验

**来自 Story 27-6**：
- paneDetail 添加为 `paneType = 3`，Tab `% 4` 更新成功
- procDetail 缓存使用 `procDetailCache map[types.PID]*ipc.GetProcDetailResponse` 避免重复查询
- dashboardTick 中每 5 ticks 刷新一次 procDetail（`m.procDetailTick % 5 == 0`），Intent 应采用同样策略
- 帮助文本的 `case paneDetail:` 分支提供了模板

**来自 Story 27-3/27-4**：
- stepDetailCache 模式可用于意图数据缓存
- BubbleTea 异步消息返回后需检查数据一致性（PID/Tree 可能已切换）

### 组合矩阵

| 交互功能 | 共存行为 | 需验证 |
|---------|---------|--------|
| Intent 窗格 + Tree 窗格 | Intent 节点 Enter 联动修改 selectedPID，Tree 窗格高亮对应进程 | 是 |
| Intent 窗格 + Timeline 窗格 | Enter 跳转后 Timeline 自动加载对应 PID 的 step 数据 | 是 |
| Intent 窗格 + Detail 窗格 | 联动后 Detail 缓存刷新 | 是 |
| Intent 窗格 + Prompt Pager | Intent 窗格激活时 p 键不应触发 prompt pager | 是 |
| Intent 窗格 + Kill 确认 | Intent 窗格中不应触发 Kill 操作 | 否（k 键已被 navigate 占用） |
| 无活跃 Intent + 窗格切换 | 空状态正常显示，不崩溃 | 是 |

### Project Structure Notes

- 所有 dashboard 改动集中在 `cmd/rnix/dashboard.go`，不拆分文件
- ATDD 测试文件命名遵循 `atdd_27_7_*.go` 规范
- 无需新建 IPC 类型或方法——完全复用 Epic 19 已有的 Intent IPC 基础设施

### References

- [Source: _bmad-output/planning-artifacts/epics/epic-27-统一观察系统-unified-observation-system.md#Story 27.7]
- [Source: ipc/protocol.go — IntentTreeWire/IntentNodeWire/IntentStatusResponse 类型定义]
- [Source: ipc/client.go — IntentList() 客户端方法]
- [Source: intent/types.go — IntentTree/IntentNode/IntentState 核心类型]
- [Source: intent/dag.go — DAG/TopologicalSort 拓扑排序]
- [Source: cmd/rnix/dashboard.go — 现有四窗格架构 + paneDetail 添加模式]
- [Source: _bmad-output/implementation-artifacts/27-6-dashboard-process-detail-panel.md — 前序 Story 经验]

## Dev Agent Record

### Agent Model Used
claude-opus-4-6

### Debug Log References
N/A

### Completion Notes List
- 所有 5 个 Task（15 个子任务）已实现
- 22 个 ATDD 测试全部 GREEN
- Story 27-6 的 Tab 循环测试已更新为 5 窗格
- `make all`（lint + vet + test + build）全部通过，0 lint issues
- 实现完全在 `cmd/rnix/dashboard.go` 内，复用现有 IPC Intent 基础设施

#### Code Review 修复（2026-03-22）
- Fix #1: `msg.trees` nil 指针守卫（防止 `IntentList()` 返回 `nil,nil` 时 panic）
- Fix #2: 意图树窗格视口滚动（`intentScrollOffset` + `intentAdjustScroll`，光标不再超出可见区域）
- Fix #3: Enter 跳转前验证 PID 是否存在于进程列表（已终止进程显示 "该进程已不存在"）
- Fix #4: AC-2 已完成/失败树排底部 + 折叠显示（`isIntentTreeTerminal` + sort + collapsed `▷` 指示符）
- Fix #5: AC-6 选中节点所在树标题 Bold 高亮
- Fix #6: AC-6 多树之间 `───` 分隔线
- Fix #7: `RNIX_ASCII=1` 状态图标 ASCII 降级
- Fix #8: 空 Nodes map 显示 "(分解中...)"
- Fix #9: 测试函数名 `4Panes→5Panes`、`ModuloIs4→ModuloIs5`

### File List
- `cmd/rnix/dashboard.go` — 新增 paneIntent(4)、intentFlatNode、intentTreesMsg、flattenIntentTrees、intentStateColor、intentStateIcon、renderIntentPane、fetchIntentTreesCmd、Tab %5、paneIntent 键处理、renderDashboardStatus Intent 帮助行、intentAdjustScroll 视口滚动、isIntentTreeTerminal 终态过滤、PID 存活验证、nil 守卫、ASCII 降级、分隔线、树标题高亮、空节点提示
- `cmd/rnix/atdd_27_7_dashboard_intent_tree_test.go` — 22 个 ATDD 验收测试 + PID 存活验证适配
- `cmd/rnix/atdd_27_6_dashboard_detail_panel_test.go` — 更新 Tab 循环测试从 4→5 窗格 + 函数名修正
