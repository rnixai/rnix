# Story 27.8: Dashboard 安全异常面板

Status: done

## Story

As a 平台构建者,
I want 在 dashboard 中查看安全异常面板，集成 Immune Daemon 的实时告警信息,
so that 我可以及时发现异常行为并处理安全威胁。

## Acceptance Criteria (AC)

### AC-1: 新增安全异常窗格（paneSecurity）

**Given** dashboard 现有五个窗格（Tree=0 / Timeline=1 / Heatmap=2 / Detail=3 / Intent=4）
**When** 新增安全异常窗格
**Then** `paneSecurity paneType = 5` 加入 iota 序列
**And** Tab 切换顺序变为 Tree → Timeline → Heatmap → Detail → Intent → Security → Tree
**And** Tab 取模值从 `% 5` 变为 `% 6`
**And** 当 Security 窗格激活时，边框高亮显示

### AC-2: 安全数据获取

**Given** dashboard 的 Security 窗格激活
**When** 需要获取安全数据
**Then** 通过 `client.ImmuneStatus()` IPC 方法获取 ImmuneStatusResponse
**And** 解析 Alerts、SuspendedPIDs、SecurityStatus、ThreatCount 字段
**And** 数据加载延迟 ≤ 1s（NFR63-obs）

### AC-3: 告警列表渲染

**Given** Immune Daemon 检测到异常行为
**When** 用户在 dashboard 中切换到安全异常窗格
**Then** 显示按严重度（Deviation 降序）排序的告警列表
**And** 每条告警显示：PID + Agent 模板 + 异常类型 + 偏离程度 + 触发时间
**And** 异常类型着色：syscall_freq=黄色、token_rate=橙色、device_access=红色
**And** 窗格切换渲染延迟 ≤ 100ms（NFR63-obs）

### AC-4: 告警选择与进程联动

**Given** 安全告警列表中某条告警
**When** 用户通过 j/k 选中并按 Enter
**Then** 联动切换 `selectedPID` 到告警对应的进程
**And** 切换到 Timeline 窗格查看该进程的步骤数据

**Given** 告警对应的进程已被 reaper 清理（不在进程列表中）
**When** 用户选中该告警按 Enter
**Then** 显示状态消息 "该进程已不存在"
**And** 不切换 selectedPID

### AC-5: 安全状态摘要

**Given** SecurityStatus 为 "ok"（无告警无挂起）
**When** 用户切换到安全异常窗格
**Then** 显示绿色安全状态："所有进程行为正常"
**And** 显示 Immune Daemon 运行时长和威胁记忆库条目数

**Given** SecurityStatus 为 "warning"（有告警或挂起）
**When** 用户切换到安全异常窗格
**Then** 顶部显示红色/黄色警告摘要行（如 "2 alerts, 1 suspended"）
**And** 下方显示告警详情列表

### AC-6: 挂起进程显示

**Given** 存在被 Immune Daemon 挂起的进程（SuspendedPIDs 非空）
**When** 安全窗格显示
**Then** 在告警列表下方显示 "SUSPENDED" 区域
**And** 每个挂起进程显示 PID + 操作提示（resume / kill）

### AC-7: Immune Daemon 未运行

**Given** Immune Daemon 未运行（Running=false）
**When** 用户切换到安全异常窗格
**Then** 显示提示："Immune Daemon 未运行。安全监控不可用。"

## Tasks / Subtasks

- [x] Task 1: 新增 paneSecurity 常量与 Tab 切换扩展（AC: #1）
  - [x] 1.1 在 `cmd/rnix/dashboard.go` 的 paneType iota 中新增 `paneSecurity = 5`
  - [x] 1.2 修改 Tab 切换 `% 5` → `% 6`
  - [x] 1.3 更新 renderDashboardStatus() 帮助文本，加入 Security 窗格说明
- [x] Task 2: 安全数据获取与缓存（AC: #2）
  - [x] 2.1 在 dashboardModel 新增字段：`immuneStatus *ipc.ImmuneStatusResponse`、`immuneErr error`、`securityAlerts []ipc.AlertWire`、`securityCursor int`
  - [x] 2.2 实现 `fetchImmuneStatusCmd()` tea.Cmd（调用 client.ImmuneStatus()）
  - [x] 2.3 定义 `immuneStatusMsg` 消息类型，在 Update 中处理响应
  - [x] 2.4 在 dashboardTick 中周期性刷新（每 5 ticks，仅 Security 窗格激活时）
- [x] Task 3: 安全窗格渲染（AC: #3, #5, #6, #7）
  - [x] 3.1 实现 `renderSecurityPane(width, height int) string` 方法
  - [x] 3.2 实现安全状态摘要行（绿色 OK 或红色/黄色 warning）
  - [x] 3.3 实现告警列表渲染（按 Deviation 降序 + 异常类型着色 + 光标高亮）
  - [x] 3.4 实现挂起进程区域渲染
  - [x] 3.5 实现 Immune Daemon 未运行的提示状态
  - [x] 3.6 在 View() 的 paneSecurity 分支中调用 renderSecurityPane()
- [x] Task 4: 告警选择与联动（AC: #4）
  - [x] 4.1 在 paneSecurity 下处理 j/k 键移动 securityCursor
  - [x] 4.2 在 paneSecurity 下处理 Enter 键：提取选中告警的 PID，联动 selectedPID 并切换到 Timeline
  - [x] 4.3 进程不存在时显示状态消息 "该进程已不存在"
- [x] Task 5: 测试（AC: #1-#7）
  - [x] 5.1 ATDD 测试：安全窗格 Tab 切换（6 窗格循环）
  - [x] 5.2 ATDD 测试：安全状态渲染（OK / warning / daemon 未运行）
  - [x] 5.3 ATDD 测试：告警列表排序与选择联动

## Dev Notes

### 关键设计决策

**为什么复用 ImmuneStatus 而非新增 IPC 方法？**
现有 `client.ImmuneStatus()` 已返回完整的 `ImmuneStatusResponse`（含 Alerts、SuspendedPIDs、SecurityStatus、ThreatCount、Running、UptimeMs），数据完整度足够。无需新增 IPC 方法。

**为什么告警列表按 Deviation 排序而非时间？**
安全告警的核心是严重程度——偏离度越大，威胁越大。时间排序可作为二级排序。

### 现有代码模式（必须遵循）

**Dashboard 窗格添加模式**（参考 Story 27-7 Intent 窗格添加）：
1. `paneType` 是 int 类型，使用 iota：`paneTree=0, paneTimeline=1, paneHeatmap=2, paneDetail=3, paneIntent=4`
2. 新增 `paneSecurity = 5`（iota 自动递增）
3. Tab 切换：`m.activePane = (m.activePane + 1) % 6`（当前为 `% 5`，需改为 `% 6`）
4. View 中：`case paneSecurity:` 分支调用 `renderSecurityPane()`
5. 边框高亮：`activePaneStyle` vs `inactivePaneStyle`

**IPC 异步调用模式**（参考 `fetchIntentTreesCmd`）：
```go
func fetchImmuneStatusCmd() tea.Cmd {
    return func() tea.Msg {
        client, err := ipc.Dial(ipc.SocketPath())
        if err != nil {
            return immuneStatusMsg{err: err}
        }
        defer client.Close()
        resp, err := client.ImmuneStatus()
        return immuneStatusMsg{status: resp, err: err}
    }
}
```

**消息处理模式**（参考 `intentTreesMsg`）：
```go
type immuneStatusMsg struct {
    status *ipc.ImmuneStatusResponse
    err    error
}
// 在 Update 中：
case immuneStatusMsg:
    if msg.err != nil {
        m.immuneErr = msg.err
        return m, nil
    }
    m.immuneStatus = msg.status
    // 按 Deviation 降序排列告警
    m.securityAlerts = sortAlertsByDeviation(msg.status.Alerts)
    // 保持 securityCursor 在范围内
```

**渲染位置**：与 Detail/Intent 共享右下区域。`case paneSecurity:` 在 View() 中与 `paneDetail` / `paneIntent` 同级。

### IPC 类型（已存在，直接使用）

```go
// ipc/protocol.go — 已有类型，无需新增
type ImmuneStatusResponse struct {
    Running        bool                             `json:"running"`
    UptimeMs       int64                            `json:"uptime_ms"`
    ProfileCount   int                              `json:"profile_count"`
    Profiles       map[string]*kernel.NormalProfile  `json:"profiles"`
    ActivePIDs     []uint64                          `json:"active_pids"`
    SuspendedPIDs  []uint64                          `json:"suspended_pids"`
    Alerts         []AlertWire                       `json:"alerts"`
    ThreatCount    int                              `json:"threat_count"`
    SecurityStatus string                            `json:"security_status"`
}

type AlertWire struct {
    PID           uint64  `json:"pid"`
    AgentTemplate string  `json:"agent_template"`
    Type          string  `json:"type"`
    Detail        string  `json:"detail"`
    Deviation     float64 `json:"deviation"`
    TimestampMs   int64   `json:"timestamp_ms"`
}

// ipc/client.go — 已有方法，直接调用
func (c *Client) ImmuneStatus() (*ImmuneStatusResponse, error)
```

### 异常类型着色映射

```go
func alertTypeColor(alertType string) lipgloss.Color {
    switch alertType {
    case "syscall_freq":
        return lipgloss.Color("220")   // 黄色
    case "token_rate":
        return lipgloss.Color("208")   // 橙色
    case "device_access":
        return lipgloss.Color("196")   // 红色
    default:
        return lipgloss.Color("240")   // 灰色
    }
}
```

### 安全状态着色

```go
func securityStatusColor(status string) lipgloss.Color {
    switch status {
    case "ok":
        return lipgloss.Color("42")    // 绿色
    case "warning":
        return lipgloss.Color("196")   // 红色
    default:
        return lipgloss.Color("240")   // 灰色
    }
}
```

### 渲染格式

```
┌─ Security ────────────────────────────┐
│ Security: OK                          │  ← 绿色（无告警时）
│ Immune Daemon: running (2h15m)        │
│ Threats in memory: 3                  │
│                                       │
│ (所有进程行为正常)                       │
└───────────────────────────────────────┘

┌─ Security ────────────────────────────┐
│ ⚠ Security: 2 alerts, 1 suspended    │  ← 红色（有告警时）
│ Immune Daemon: running (2h15m)        │
│                                       │
│ ALERTS                                │
│ ▸ PID:5  code-analyst  syscall_freq   │  ← 黄色
│   "Open 频率 12.0 是基线 2.4 的 5.0 倍"│
│   PID:8  data-miner    device_access  │  ← 红色
│   "/dev/fs 未授权访问"                  │
│                                       │
│ SUSPENDED                             │
│   PID:5 (syscall_freq) → resume/kill  │
└───────────────────────────────────────┘
```

### 帮助文本

renderDashboardStatus() 需要新增 Security 窗格的帮助行：
```go
if m.activePane == paneSecurity {
    return fmt.Sprintf("  %sq:Quit  Tab:Switch Pane  j/k:Navigate  Enter:Jump to Process%s", rec, ops)
}
```

### 运行时长格式化

```go
func formatUptimeShort(ms int64) string {
    d := time.Duration(ms) * time.Millisecond
    if d < time.Minute {
        return fmt.Sprintf("%ds", int(d.Seconds()))
    }
    if d < time.Hour {
        return fmt.Sprintf("%dm%ds", int(d.Minutes()), int(d.Seconds())%60)
    }
    return fmt.Sprintf("%dh%dm", int(d.Hours()), int(d.Minutes())%60)
}
```

### 修改文件清单

| 文件 | 修改类型 | 说明 |
|------|----------|------|
| `cmd/rnix/dashboard.go` | 修改 | 新增 paneSecurity(5)、fetchImmuneStatusCmd、immuneStatusMsg、renderSecurityPane、alertTypeColor、securityStatusColor、formatUptimeShort、帮助文本更新、Tab 取模 %5→%6 |
| `cmd/rnix/atdd_27_8_dashboard_security_panel_test.go` | 新增 | ATDD 验收测试 |

### 不需要修改的文件

- `ipc/protocol.go` — ImmuneStatusResponse/AlertWire 已存在
- `ipc/client.go` — ImmuneStatus() 方法已存在
- `ipc/server.go` — handleImmuneStatus handler 已存在
- `kernel/immune.go` — ImmuneDaemon/AnomalyAlert/BehaviorCollector 已存在

### 依赖关系

- **Story 27.3-27.7**（Dashboard 窗格模式）：参考 Tab 切换、窗格渲染、IPC 获取模式，均已完成
- **Epic 22**（Immune Daemon 已实现）：ImmuneDaemon/AnomalyDetector/ThreatSignature 全部已实现
- **IPC Immune 方法**：`immune_status` 已注册，client 方法已就位

### 防踩坑清单

1. **Tab 取模值必须更新** — 从 `% 5` 改为 `% 6`，搜索所有 `% 5` 确保无硬编码遗漏
2. **AlertWire.PID 是 uint64** — 需要转换为 `types.PID`（`types.PID(alert.PID)`）
3. **ImmuneStatus 返回 nil** — `client.ImmuneStatus()` 可能返回 `nil, nil`（daemon 未集成 immune），需做 nil 守卫
4. **securityCursor 越界** — 在 immuneStatus 刷新后，确保 `securityCursor < len(securityAlerts)`
5. **CJK 字符宽度** — Detail 文本（中文异常描述）需考虑 CJK 字符占 2 列宽
6. **RNIX_ASCII 环境变量** — `⚠` 等 Unicode 符号需 ASCII 降级（参考 intentStateIcon 的 ASCII 降级模式）
7. **帮助行更新** — 底部帮助文字需加入 Security 窗格说明
8. **ImmuneStatusResponse.Alerts 可能为空** — Alerts 为 nil 时应视为空列表
9. **时间格式化** — TimestampMs 转换为人类可读时间（"2m ago" 样式或绝对时间）
10. **27-7 ATDD 测试更新** — Story 27-7 的 Tab 循环测试需要从 5 窗格更新到 6 窗格（参考 27-6→27-7 的更新模式）

### 前序 Story 经验

**来自 Story 27-7**：
- paneIntent 添加为 `paneType = 4`，Tab `% 5` 更新成功
- intentTrees 数据刷新使用 `dashboardTick % 5 == 0` 且仅在 Intent 窗格激活时执行
- nil 指针守卫是 Code Review 修复的重点（Fix #1）
- intentCursor 越界保护在刷新后执行

**来自 Story 27-6**：
- procDetail 缓存使用 `procDetailCache map[types.PID]*ipc.GetProcDetailResponse` 避免重复查询
- 帮助文本的 `case paneDetail:` 分支提供了模板
- Enter 跳转前验证 PID 是否存在于进程列表（Fix #3 from 27-7）

### 组合矩阵

| 交互功能 | 共存行为 | 需验证 |
|---------|---------|--------|
| Security 窗格 + Tree 窗格 | Security Enter 联动修改 selectedPID，Tree 窗格高亮对应进程 | 是 |
| Security 窗格 + Timeline 窗格 | Enter 跳转后 Timeline 自动加载对应 PID 的 step 数据 | 是 |
| Security 窗格 + Intent 窗格 | 独立数据源，不冲突 | 否 |
| Security 窗格 + Prompt Pager | Security 窗格激活时 p 键不应触发 prompt pager | 是 |
| 无 Immune Daemon + 窗格切换 | 未运行状态正常显示，不崩溃 | 是 |
| Alerts 为空 + 窗格切换 | OK 状态正常显示，不崩溃 | 是 |

### Project Structure Notes

- 所有 dashboard 改动集中在 `cmd/rnix/dashboard.go`，不拆分文件
- ATDD 测试文件命名遵循 `atdd_27_8_*.go` 规范
- 无需新建 IPC 类型或方法——完全复用 Epic 22 已有的 Immune IPC 基础设施

### References

- [Source: _bmad-output/planning-artifacts/epics/epic-27-统一观察系统-unified-observation-system.md#Story 27.8]
- [Source: _bmad-output/planning-artifacts/epics/epic-22-适应性安全与自愈-adaptive-security-self-healing.md]
- [Source: ipc/protocol.go — ImmuneStatusResponse/AlertWire 类型定义]
- [Source: ipc/client.go — ImmuneStatus() 客户端方法]
- [Source: kernel/immune.go — ImmuneDaemon/AnomalyAlert 核心类型]
- [Source: cmd/rnix/dashboard.go — 现有五窗格架构 + paneIntent 添加模式]
- [Source: _bmad-output/implementation-artifacts/27-7-dashboard-intent-tree-integration.md — 前序 Story 经验]

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6

### Debug Log References

N/A

### Completion Notes List

- 实现 paneSecurity=5 常量，Tab 循环从 %5 改为 %6
- 新增 dashboardModel 字段：immuneStatus、immuneErr、securityAlerts、securityCursor
- 实现 immuneStatusMsg 消息类型和 fetchImmuneStatusCmd（复用现有 client.ImmuneStatus()）
- 实现 renderSecurityPane：OK 状态（绿色 + uptime + threat count）、warning 状态（告警列表 + 挂起进程）、Daemon 未运行提示、nil 守卫
- 告警按 Deviation 降序排序，异常类型着色（syscall_freq=黄/token_rate=橙/device_access=红）
- j/k 导航 + Enter 联动 selectedPID 切换到 Timeline，进程不存在显示 "该进程已不存在"
- 周期性刷新（每 5 ticks，仅 Security 窗格激活时）
- 更新 27-7 和 27-6 的 Tab 循环测试（5→6 窗格）
- RNIX_ASCII 环境变量支持 ⚠ 符号 ASCII 降级
- 所有 QF1012 lint 警告已修复（WriteString(Sprintf) → Fprintf）

### File List

| 文件 | 修改类型 | 说明 |
|------|----------|------|
| `cmd/rnix/dashboard.go` | 修改 | paneSecurity(5)、immuneStatusMsg、fetchImmuneStatusCmd、renderSecurityPane、alertTypeColor、securityStatusColor、formatUptimeShort、sortAlertsByDeviation、Tab %6、帮助文本、j/k/Enter 处理、dashboardTick 周期刷新 |
| `cmd/rnix/atdd_27_8_dashboard_security_panel_test.go` | 已有 | ATDD 验收测试（27 个测试全部通过） |
| `cmd/rnix/atdd_27_7_dashboard_intent_tree_test.go` | 修改 | Tab 循环测试从 5 窗格更新到 6 窗格 |
| `cmd/rnix/atdd_27_6_dashboard_detail_panel_test.go` | 修改 | Tab 循环测试从 5 窗格更新到 6 窗格，modulo 测试 10→12 |
