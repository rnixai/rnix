# Story 27.10: Dashboard 多智能体评价视图

Status: done

## Story

As a 平台构建者,
I want 在 dashboard 中查看多智能体评价视图，集成声誉系统数据、协作拓扑和能力重叠度矩阵,
so that 我可以了解各 Agent 模板的历史表现和协作模式，优化智能体配置。

## Acceptance Criteria (AC)

### AC-1: 新增评价窗格（paneEval）

**Given** dashboard 现有七个窗格（Tree=0 / Timeline=1 / Heatmap=2 / Detail=3 / Intent=4 / Security=5 / Trace=6）
**When** 新增多智能体评价窗格
**Then** `paneEval paneType = 7` 加入 iota 序列
**And** Tab 切换顺序变为 Tree → Timeline → Heatmap → Detail → Intent → Security → Trace → Eval → Tree
**And** Tab 取模值从 `% 7` 变为 `% 8`
**And** 当 Eval 窗格激活时，边框高亮显示

### AC-2: 声誉排行表渲染

**Given** 系统有声誉数据（`.rnix/reputation/` 目录下有 agent JSON Lines 文件）
**When** 用户切换到评价窗格
**Then** 默认显示声誉排行表，每行显示：Agent 名称 + 综合评分 + 成功率 + 平均 token + 平均耗时 + 记录数 + 趋势
**And** 按综合评分降序排列
**And** 趋势着色：improving=绿色、declining=红色、stable=灰色
**And** 窗格切换渲染延迟 ≤ 100ms（NFR63-obs）
**And** 数据通过 `client.ReputationStatus("")`（空 agentName = 查询全部）IPC 获取

### AC-3: 三级子视图切换

**Given** 评价窗格激活
**When** 用户按 `1` / `2` / `3` 键
**Then** 分别切换到：
  - `1`：声誉排行表（默认）
  - `2`：协作拓扑图（Nodes + Edges + 强化路径）
  - `3`：能力重叠度矩阵（Synergy Combo 摘要）

### AC-4: 协作拓扑视图

**Given** 用户在评价窗格中按 `2` 键切换到拓扑视图
**When** 拓扑数据存在
**Then** 分两部分展示：
  - 上半区：节点列表（Agent 名称 + 声誉分 + 连接数）
  - 下半区：协作边列表（From → To + Spawn 次数 + Msg 次数 + 总数 + 强化标记）
**And** 强化路径（Reinforced=true）以高亮色标注
**And** 数据通过 `client.TopologyQuery()` IPC 获取

### AC-5: 能力重叠度矩阵视图

**Given** 用户在评价窗格中按 `3` 键切换到矩阵视图
**When** Synergy 数据存在
**Then** 显示技能组合摘要列表：技能组合 + 成功率 + 平均 token + 执行次数 + 对比独立 + Token 增益 + 推荐状态
**And** 推荐组合（Recommended=true）以绿色高亮
**And** 数据通过 `client.SynergyList()` IPC 获取

### AC-6: 空状态处理

**Given** 无声誉数据（`.rnix/reputation/` 目录为空或不存在）
**When** 用户切换到评价窗格
**Then** 显示空状态提示："需要更多执行数据以生成评价。使用 rnix spawn 或 rnix compose up 执行任务以积累数据。"

**Given** 拓扑数据为空（无协作历史）
**When** 用户切换到拓扑视图
**Then** 显示："无协作拓扑数据。运行多智能体编排以生成协作关系。"

**Given** Synergy 数据为空
**When** 用户切换到矩阵视图
**Then** 显示："无技能组合数据。当 Agent 使用多个 Skill 执行任务时将自动记录。"

**Given** IPC 调用失败
**When** 用户切换到评价窗格任意子视图
**Then** 显示错误信息而不崩溃

## Tasks / Subtasks

- [x] Task 1: 新增 paneEval 常量与 Tab 切换扩展（AC: #1）
  - [x] 1.1 在 `cmd/rnix/dashboard.go` 的 paneType iota 中新增 `paneEval = 7`
  - [x] 1.2 修改 Tab 切换 `% 7` → `% 8`
  - [x] 1.3 更新 renderDashboardStatus() 帮助文本
- [x] Task 2: 声誉排行表获取与展示（AC: #2, #6）
  - [x] 2.1 在 dashboardModel 新增字段：`evalSubView int`（0=声誉, 1=拓扑, 2=矩阵）、`evalReputations []kernel.ReputationSummary`、`evalRepErr error`、`evalRepCursor int`、`evalRepScrollOffset int`
  - [x] 2.2 实现 `fetchReputationCmd()` tea.Cmd（调用 `client.ReputationStatus("")`）
  - [x] 2.3 定义 `evalReputationMsg` 消息类型，在 Update 中处理响应
  - [x] 2.4 在 dashboardTick 中周期性刷新（每 5 ticks，仅 Eval 窗格激活时）
  - [x] 2.5 实现 `renderEvalPane(width, height int) string` 方法（声誉表 + 空状态）
- [x] Task 3: 协作拓扑视图（AC: #4, #6）
  - [x] 3.1 新增字段：`evalTopology *ipc.TopologyQueryResponse`、`evalTopoErr error`、`evalTopoCursor int`、`evalTopoScrollOffset int`
  - [x] 3.2 实现 `fetchTopologyCmd()` tea.Cmd（调用 `client.TopologyQuery()`）
  - [x] 3.3 定义 `evalTopologyMsg` 消息类型
  - [x] 3.4 实现 `renderEvalTopologyView(width, height int) string`
- [x] Task 4: 能力重叠度矩阵视图（AC: #5, #6）
  - [x] 4.1 新增字段：`evalSynergies []kernel.ComboSummary`、`evalSynErr error`、`evalSynCursor int`、`evalSynScrollOffset int`
  - [x] 4.2 实现 `fetchSynergyCmd()` tea.Cmd（调用 `client.SynergyList()`）
  - [x] 4.3 定义 `evalSynergyMsg` 消息类型
  - [x] 4.4 实现 `renderEvalSynergyView(width, height int) string`
- [x] Task 5: 子视图切换与键盘导航（AC: #3）
  - [x] 5.1 在 Eval 窗格下处理 `1`/`2`/`3` 键切换 `evalSubView`
  - [x] 5.2 j/k 导航各子视图的 cursor
  - [x] 5.3 切换子视图时触发对应数据获取
- [x] Task 6: 测试（AC: #1-#6）
  - [x] 6.1 ATDD 测试：Eval 窗格 Tab 切换（8 窗格循环）
  - [x] 6.2 ATDD 测试：声誉排行表渲染 + 空状态
  - [x] 6.3 ATDD 测试：拓扑视图渲染 + 空状态
  - [x] 6.4 ATDD 测试：矩阵视图渲染 + 空状态
  - [x] 6.5 ATDD 测试：子视图 1/2/3 切换
  - [x] 6.6 更新 27-6/27-7/27-8/27-9 的 Tab 循环测试（7→8 窗格）

## Dev Notes

### 关键设计决策

**为什么不需要新增 IPC 方法？**
所有数据源的 IPC 方法已在 Epic 21-22 中实现：
- `reputation_status`（Story 21.3）→ `client.ReputationStatus("")` 返回所有 Agent 声誉摘要
- `topology_query`（Story 22.5）→ `client.TopologyQuery()` 返回完整协作拓扑
- `synergy_list`（Story 21.5）→ `client.SynergyList()` 返回技能组合矩阵

**为什么采用三级子视图？**
评价数据涉及三个独立维度（声誉 / 拓扑 / 协同），每个维度数据量较大，合并显示会信息过载。使用 `1`/`2`/`3` 键切换子视图，与 Timeline 窗格的 `1-4` 过滤模式一致。

### 现有代码模式（必须遵循）

**Dashboard 窗格添加模式**（参考 Story 27-9 Trace 窗格添加）：
1. `paneType` 是 int 类型，使用 iota：`paneTree=0, ..., paneTrace=6`
2. 新增 `paneEval = 7`（iota 自动递增）
3. Tab 切换：`m.activePane = (m.activePane + 1) % 8`（当前为 `% 7`，需改为 `% 8`）
4. View 中：`case paneEval:` 分支调用 `renderEvalPane()`
5. 边框高亮：`activePaneStyle` vs `inactivePaneStyle`

**IPC 异步调用模式**（参考 `fetchImmuneStatusCmd`、`fetchTraceListCmd`）：
```go
func fetchReputationCmd() tea.Cmd {
    return func() tea.Msg {
        client, err := ipc.Dial(ipc.SocketPath())
        if err != nil {
            return evalReputationMsg{err: err}
        }
        defer client.Close()
        resp, err := client.ReputationStatus("")
        if resp != nil {
            return evalReputationMsg{summaries: resp.Summaries, err: err}
        }
        return evalReputationMsg{err: err}
    }
}
```

**消息处理模式**（参考 `immuneStatusMsg`、`traceListMsg`）：
```go
type evalReputationMsg struct {
    summaries []kernel.ReputationSummary
    err       error
}
type evalTopologyMsg struct {
    topology *ipc.TopologyQueryResponse
    err      error
}
type evalSynergyMsg struct {
    combos []kernel.ComboSummary
    err    error
}
```

**渲染位置**：与 Detail/Intent/Security/Trace 共享右下区域。`case paneEval:` 在 View() 中与其他窗格同级。

**Tick 刷新模式**（参考 Trace/Security 窗格）：
```go
if m.activePane == paneEval && m.connected {
    if m.evalReputations == nil || m.heatmapTickCount%5 == 0 {
        cmds = append(cmds, fetchReputationCmd())
    }
    // 拓扑和 synergy 仅在对应子视图激活时获取
    if m.evalSubView == 1 && (m.evalTopology == nil || m.heatmapTickCount%5 == 0) {
        cmds = append(cmds, fetchTopologyCmd())
    }
    if m.evalSubView == 2 && (m.evalSynergies == nil || m.heatmapTickCount%5 == 0) {
        cmds = append(cmds, fetchSynergyCmd())
    }
}
```

### 已有 IPC 类型定义（直接复用，无需新增）

**声誉查询**（`ipc/protocol.go`）：
```go
MethodReputationStatus Method = "reputation_status"  // line 47

type ReputationStatusRequest struct {
    AgentName string `json:"agent_name,omitempty"` // 空 = 查询全部
}
type ReputationStatusResponse struct {
    Summaries []kernel.ReputationSummary `json:"summaries"`
}
```

**拓扑查询**（`ipc/protocol.go`）：
```go
MethodTopologyQuery Method = "topology_query"  // line 52

type TopologyQueryResponse struct {
    Nodes           []kernel.TopologyNode    `json:"nodes"`
    Edges           []kernel.CooperationEdge `json:"edges"`
    ReinforcedPaths []kernel.CooperationEdge `json:"reinforced_paths"`
}
```

**协同查询**（`ipc/protocol.go`）：
```go
MethodSynergyList Method = "synergy_list"  // line 48

type SynergyListResponse struct {
    Combos []kernel.ComboSummary `json:"combos"`
}
```

**客户端方法**（`ipc/client.go`）：
```go
func (c *Client) ReputationStatus(agentName string) (*ReputationStatusResponse, error)  // line 816
func (c *Client) TopologyQuery() (*TopologyQueryResponse, error)                        // line 881
func (c *Client) SynergyList() (*SynergyListResponse, error)                           // line 829
```

### 已有数据类型（直接导入 kernel 包）

**ReputationSummary**（`kernel/reputation.go:99-107`）：
```go
type ReputationSummary struct {
    AgentName     string  `json:"agent_name"`
    Score         float64 `json:"score"`           // 0.0~1.0
    SuccessRate   float64 `json:"success_rate"`
    AvgTokens     int     `json:"avg_tokens"`
    AvgDurationMs int64   `json:"avg_duration_ms"`
    TotalRecords  int     `json:"total_records"`
    RecentTrend   string  `json:"recent_trend"`    // "improving"|"declining"|"stable"
}
```

**TopologyNode**（`kernel/immune.go:975`）：
```go
type TopologyNode struct {
    Agent           string  `json:"agent"`
    ReputationScore float64 `json:"reputation_score"`
    Connections     int     `json:"connections"`
}
```

**CooperationEdge**（`kernel/immune.go:962`）：
```go
type CooperationEdge struct {
    From       string `json:"from"`
    To         string `json:"to"`
    SpawnCount int    `json:"spawn_count"`
    MsgCount   int    `json:"msg_count"`
    Total      int    `json:"total"`
    Reinforced bool   `json:"reinforced"`
}
```

**ComboSummary**（`kernel/synergy_matrix.go:89`）：
```go
type ComboSummary struct {
    ComboKey         SynergyComboKey `json:"combo_key"`
    Skills           []string        `json:"skills"`
    SuccessRate      float64         `json:"success_rate"`
    AvgTokens        int             `json:"avg_tokens"`
    TotalExecutions  int             `json:"total_executions"`
    AvgSoloRate      float64         `json:"avg_solo_rate"`
    TokenImprovement float64         `json:"token_improvement"`
    Recommended      bool            `json:"recommended"`
}
```

### 渲染格式

**声誉排行表（子视图 1，默认）：**
```
┌─ Evaluation ── [1]Reputation [2]Topology [3]Synergy ─┐
│ AGENT            SCORE  SUCCESS  AVG TOK  AVG DUR  N  TREND │
│ ──────────────────────────────────────────────────────────── │
│ ▸ code-reviewer   0.85   85.0%    5000     30ms   10  ↑     │
│   code-fixer      0.72   70.0%    6000     35ms    8  ↑     │
│   summarizer      0.60   60.0%    3000     20ms    5  →     │
│   translator      0.45   40.0%    8000     50ms    3  ↓     │
│                                                              │
│ j/k:选择  1/2/3:子视图                                       │
└──────────────────────────────────────────────────────────────┘
```

**协作拓扑（子视图 2）：**
```
┌─ Evaluation ── [1]Reputation [2]Topology [3]Synergy ─┐
│ ── Nodes ──────────────────────────────               │
│ AGENT            SCORE  CONNECTIONS                    │
│ code-reviewer     0.85          3                      │
│ code-fixer        0.72          2                      │
│                                                        │
│ ── Edges ──────────────────────────────               │
│ FROM → TO              SPAWN  MSG  TOTAL  REINFORCED   │
│ code-reviewer → fixer      8    5     13  ★            │
│ reviewer → summarizer      3    2      5  ★            │
│ fixer → translator         1    1      2               │
│                                                        │
│ j/k:选择  1/2/3:子视图                                 │
└────────────────────────────────────────────────────────┘
```

**能力重叠度矩阵（子视图 3）：**
```
┌─ Evaluation ── [1]Reputation [2]Topology [3]Synergy ─┐
│ SKILLS              SUCCESS  AVG TOK  EXEC  VS SOLO  REC │
│ ────────────────────────────────────────────────────────  │
│ ▸ review,fix         92.0%    4500     12   +12.0%   ✓   │
│   review,summarize   80.0%    3000      8    +5.0%   ✓   │
│   fix,translate      55.0%    7000      4    -5.0%       │
│                                                           │
│ j/k:选择  1/2/3:子视图                                    │
└───────────────────────────────────────────────────────────┘
```

### 帮助文本

```go
case paneEval:
    return fmt.Sprintf("  %sq:Quit  Tab:Switch Pane  j/k:Navigate  1/2/3:Sub-view%s", rec, ops)
```

### 滚动管理

参考 `traceAdjustScroll` 和 `securityAdjustScroll`，为评价窗格实现：
```go
func evalRepAdjustScroll(m *dashboardModel) {
    // 与 securityAdjustScroll 相同模式
}
func evalTopoAdjustScroll(m *dashboardModel) { ... }
func evalSynAdjustScroll(m *dashboardModel) { ... }
```

使用统一的 viewport 高度计算函数：
```go
func evalBottomInnerH(m *dashboardModel) int {
    // 参考 traceBottomInnerH()
}
```

### 修改文件清单

| 文件 | 修改类型 | 说明 |
|------|----------|------|
| `cmd/rnix/dashboard.go` | 修改 | 新增 paneEval(7)、evalReputationMsg、evalTopologyMsg、evalSynergyMsg、fetchReputationCmd、fetchTopologyCmd、fetchSynergyCmd、renderEvalPane（含三个子视图）、evalSubView 切换、帮助文本更新、Tab 取模 %7→%8、j/k/1/2/3 处理、dashboardTick 条件刷新、滚动管理函数 |
| `cmd/rnix/atdd_27_10_dashboard_eval_panel_test.go` | 新增 | ATDD 验收测试 |

### 不需要修改的文件

- `ipc/protocol.go` — 所有 IPC 方法和 Wire 类型已存在
- `ipc/server.go` — reputation_status、topology_query、synergy_list handler 已存在
- `ipc/client.go` — ReputationStatus、TopologyQuery、SynergyList 客户端方法已存在
- `kernel/reputation.go` — ReputationSummary 类型已存在
- `kernel/immune.go` — TopologyNode、CooperationEdge、CollaborationTopology 已存在
- `kernel/synergy_matrix.go` — ComboSummary 类型已存在

### 依赖关系

- **Story 27.3-27.9**（Dashboard 窗格模式）：参考 Tab 切换、窗格渲染、IPC 获取模式，均已完成
- **Epic 21**（声誉系统已实现）：ReputationSummary、ComboSummary、IPC 方法全部已实现
- **Epic 22**（协作拓扑已实现）：TopologyNode、CooperationEdge、CollaborationTopology、IPC 方法全部已实现

### 防踩坑清单

1. **Tab 取模值必须更新** — 从 `% 7` 改为 `% 8`，搜索所有 `% 7` 确保无硬编码遗漏
2. **kernel 包导入** — `cmd/rnix` 包已导入 `kernel` 包（参考 existing dashboard code），`ReputationSummary`、`ComboSummary` 等类型可直接使用
3. **ReputationStatus 空参数** — `client.ReputationStatus("")`（空字符串 agentName）返回所有 Agent 摘要，不是 `nil`
4. **TopologyQuery 返回值** — `TopologyQueryResponse` 直接包含 Nodes/Edges/ReinforcedPaths 三个字段，不需要额外解包
5. **SynergyList 返回值** — `SynergyListResponse.Combos` 是 `[]kernel.ComboSummary`，非指针
6. **evalRepCursor 越界** — 在声誉数据刷新后，确保 `evalRepCursor < len(evalReputations)`
7. **子视图切换清零** — 切换窗格时不重置 `evalSubView`（保留用户选择），但 cursor/scrollOffset 在对应数据刷新后检查越界
8. **RNIX_ASCII 环境变量** — 趋势箭头 `↑`/`→`/`↓` 和强化标记 `★` 需 ASCII 降级（`^`/`-`/`v` 和 `*`）
9. **数据为 nil 与空切片区分** — `Summaries == nil`（未加载）vs `len(Summaries) == 0`（已加载但无数据），显示不同状态
10. **v/V/p 键守卫** — 参考 27-9 的 P-8 修复，确保 v/V/p 键添加 `activePane == paneTimeline` 守卫，Eval 窗格中不触发步骤详情逻辑
11. **27-6/27-7/27-8/27-9 ATDD 测试更新** — Tab 循环测试需要从 7 窗格更新到 8 窗格
12. **IPC nil 守卫** — `ReputationStatusResponse` 可能为 nil（daemon 无 reputationStore 时返回空 Summaries），需安全访问
13. **光标字符** — `▸` 在 RNIX_ASCII 模式下降级为 `>`（参考 Trace 窗格 P-6 修复）
14. **kernel 类型跨包引用** — `kernel.ReputationSummary`、`kernel.ComboSummary` 等需通过 IPC 响应的 Summaries/Combos 字段获取，不需直接 new

### 前序 Story 经验

**来自 Story 27-9（Trace 窗格）：**
- paneTrace 添加为 `paneType = 6`，Tab `% 7` 更新成功
- 两级视图（列表 + 树）状态管理模式可参考，本 Story 使用三级子视图（1/2/3 键）
- `flattenSpanTree` DFS 展平算法不适用于本 Story（数据是扁平列表）
- Code Review 9 个 Patch 中的关键经验：nil 守卫、ASCII 降级、滚动公式统一、v/V/p 键窗格守卫

**来自 Story 27-8（Security 窗格）：**
- immuneStatus 数据刷新使用 `dashboardTick % 5 == 0` 且仅在 Security 窗格激活时执行
- nil 守卫是关键——ImmuneStatus 可能返回 nil
- securityCursor 越界保护在刷新后执行
- Enter 跳转前验证 PID 是否存在于进程列表

**来自 Story 27-7（Intent 窗格）：**
- intentCursor 管理 + scrollOffset 管理模式可直接复用
- Code Review Fix #1：nil 指针守卫，Fix #3：PID 存在性验证

### Git 最近提交（上下文参考）

```
8e597f2 feat: Implement Story 27.9 - Dashboard Distributed Tracing Integration
4f5a462 feat: Implement Story 27.8 - Dashboard Security Anomaly Panel
a0d6ed6 feat: Implement Story 28.4 - Dashboard PID Validity Check
e239698 feat: Implement Story 28.3 - IPC PID→UUID Mapping
d2afb4c feat: Implement Story 27.7 - Dashboard Intent Tree Integration
```

### 组合矩阵

| 交互功能 | 共存行为 | 需验证 |
|---------|---------|--------|
| Eval 窗格 + Tree 窗格 | 独立数据源，不冲突 | 否 |
| Eval 窗格 + Timeline 窗格 | 独立数据源，不冲突 | 否 |
| Eval 窗格 + Trace 窗格 | 独立数据源，不冲突 | 否 |
| Eval 窗格 + Security 窗格 | 独立数据源（Security 用 ImmuneStatus，Eval 用 ReputationStatus） | 否 |
| Eval 窗格 + Prompt Pager | Eval 窗格激活时 p 键不应触发 prompt pager | 是 |
| 无评价数据 + 窗格切换 | 空状态正常显示，不崩溃 | 是 |
| 子视图切换 + Tab 切换 | Tab 切走再切回应保持 evalSubView 状态 | 是 |
| 子视图 1/2/3 + 数据未加载 | 切换到子视图时触发对应数据获取，加载中显示 Loading | 是 |

### Project Structure Notes

- Dashboard 所有改动集中在 `cmd/rnix/dashboard.go`，不拆分文件
- ATDD 测试文件命名遵循 `atdd_27_10_*.go` 规范
- 复用已有 IPC 方法和 kernel 类型，不重复实现
- 无需新增 IPC Wire 类型

### References

- [Source: _bmad-output/planning-artifacts/epics/epic-27-统一观察系统-unified-observation-system.md#Story 27.10]
- [Source: kernel/reputation.go — ReputationStore, ReputationSummary, GetAllSummaries]
- [Source: kernel/immune.go — TopologyNode, CooperationEdge, CollaborationTopology, GetTopology]
- [Source: kernel/synergy_matrix.go — SynergyMatrix, ComboSummary, GetComboSummaries]
- [Source: ipc/protocol.go — MethodReputationStatus, MethodTopologyQuery, MethodSynergyList + Wire 类型]
- [Source: ipc/client.go — ReputationStatus(), TopologyQuery(), SynergyList()]
- [Source: cmd/rnix/dashboard.go — 现有七窗格架构 + paneTrace 添加模式]
- [Source: _bmad-output/implementation-artifacts/27-9-dashboard-distributed-tracing-integration.md — 前序 Story 经验]

## Dev Agent Record

### Agent Model Used
Claude Opus 4.6

### Debug Log References
N/A

### Completion Notes List
- 所有 6 个 Task 均已实现并通过测试
- 38 个 ATDD 测试全部通过
- 更新了 Story 27-6/27-7/27-8/27-9 的 Tab 循环测试（7→8 窗格）
- `make all`（lint + vet + test + build）全部通过
- 遵循防踩坑清单：nil 守卫、ASCII 降级、Score 降序排序、cursor 越界保护、子视图状态保持

### Code Review Record
- **审查模型**: Claude Opus 4.6（三路并行对抗性审查）
- **审查层**: Blind Hunter + Edge Case Hunter + Acceptance Auditor
- **发现数**: 13（去重后）
- **Patch 修复 5 项**:
  - P-1: Topology 视图添加 scrollOffset 裁剪 + 空 Nodes/Edges 守卫
  - P-2: fetch 函数先判 err 后用 resp，不再同时传递
  - P-3: Topology "FROM → TO" 表头 ASCII 降级
  - P-4: 新增 evalBottomInnerH() 统一 adjustScroll 与 render 公式
  - P-5: Topology 空状态消息对齐 spec（去除多余文案）
- **Defer 6 项**: IPC 连接无复用、Reputation 始终刷新、heatmapTickCount 复用、魔法数字 8、窄终端最小宽度、IPC 连接池（均为全项目统一模式）
- **Reject 2 项**: v/V/p 键泄漏（误报）、Cursor 空 section 计数（构造正确）
- **修复后验证**: `make all` 全部通过（0 lint issues, 22 packages, build OK）

### File List
| 文件 | 修改类型 |
|------|----------|
| `cmd/rnix/dashboard.go` | 修改 |
| `cmd/rnix/atdd_27_10_dashboard_eval_panel_test.go` | 新增（已预存在） |
| `cmd/rnix/atdd_27_6_dashboard_detail_panel_test.go` | 修改（Tab 循环 7→8） |
| `cmd/rnix/atdd_27_7_dashboard_intent_tree_test.go` | 修改（Tab 循环 7→8） |
| `cmd/rnix/atdd_27_8_dashboard_security_panel_test.go` | 修改（Tab 循环 7→8） |
| `cmd/rnix/atdd_27_9_dashboard_trace_panel_test.go` | 修改（Tab 循环 7→8） |
