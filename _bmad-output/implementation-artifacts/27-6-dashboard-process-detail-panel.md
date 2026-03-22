# Story 27.6: Dashboard 进程详情面板

Status: ready-for-dev

## Story

As a 平台构建者,
I want 在 dashboard 中选中进程后查看完整的运行时信息面板,
so that 我可以了解进程的环境变量、已加载 Skill、FD 表和上下文统计。

## Acceptance Criteria (AC)

### AC-1: 新增 GetProcDetail IPC 方法

**Given** dashboard 需要获取超出 ListProcs 现有字段的进程详情
**When** 新增 `get_proc_detail` IPC 方法
**Then** 返回 `GetProcDetailResponse`，包含：
- 基础信息：PID, UUID, PPID, State, Intent, Provider, Model, CreatedAt, DeadAt
- 环境变量快照：`EnvSnapshot map[string]string`（从 Process.ProjectConfig 提取，敏感 key 脱敏）
- 已加载 Skill 列表：`Skills []SkillInfoWire`（名称 + AllowedTools）
- FD 表：`FDTable []FDEntryWire`（FD 编号 + 设备路径 + 状态）
- 上下文统计：`ContextStats ContextStatsWire`（消息数、token 消耗、上下文预算、使用率百分比）
- AllowedDevices 列表

### AC-2: 详情窗格 Tab 切换

**Given** dashboard 现有三个窗格（Tree / Timeline / Heatmap）
**When** 用户按 Tab 键
**Then** 窗格切换顺序变为 Tree → Timeline → Heatmap → Detail → Tree
**And** 当 Detail 窗格激活时，边框高亮显示
**And** 窗格切换渲染延迟 ≤ 100ms（NFR63-obs）

### AC-3: 详情面板显示内容

**Given** dashboard 中选中了某个进程且 Detail 窗格激活
**When** 数据加载完成
**Then** 显示四个分区：
1. **基础信息区**：PID / UUID / 状态 / 意图 / Provider:Model / 运行时长
2. **Skill 列表区**：每个 Skill 名称 + allowed-tools 列表
3. **FD 表区**：已打开的 VFS 设备路径及 FD 编号
4. **上下文统计区**：消息数 / Token 消耗 / 预算使用率进度条

### AC-4: 数据加载性能

**Given** 进程详情面板显示中
**When** 数据通过 IPC 查询
**Then** 数据加载延迟 ≤ 1s（NFR63-obs）
**And** 窗格切换渲染延迟 ≤ 100ms（NFR63-obs）

### AC-5: 选中进程切换时自动刷新

**Given** Detail 窗格已激活
**When** 用户在 Tree 窗格中切换选中的进程（通过 j/k 切回 Tree 后再切换）
**Then** Detail 窗格自动刷新为新选中进程的数据
**And** 缓存前一个进程的详情数据（避免来回切换时重复查询）

### AC-6: Dead 进程的详情查看

**Given** 选中了一个 Dead 状态的进程
**When** 切换到 Detail 窗格
**Then** 正常显示该进程的历史数据
**And** 环境变量和 Skill 信息从内存中已有的 ProcInfo 恢复
**And** FD 表显示 "已关闭"（Dead 进程的 FD 已被清理）

## Tasks / Subtasks

- [ ] Task 1: 新增 GetProcDetail IPC 方法（AC: #1）
  - [ ] 1.1 在 `ipc/protocol.go` 新增 `MethodGetProcDetail`、请求/响应类型
  - [ ] 1.2 在 `kernel/kernel.go` 新增 `GetProcDetail` 内核方法（收集 FD 表、Skill 详情、上下文统计）
  - [ ] 1.3 在 `ipc/server.go` 新增 `handleGetProcDetail` handler
  - [ ] 1.4 在 `ipc/client.go` 新增 `GetProcDetail` 客户端方法
- [ ] Task 2: Dashboard 详情窗格 UI（AC: #2, #3）
  - [ ] 2.1 新增 `paneDetail paneType = 3` 常量，扩展 Tab 切换逻辑
  - [ ] 2.2 在 `dashboardModel` 新增 `procDetail *ipc.GetProcDetailResponse` 和 `procDetailPID types.PID` 缓存字段
  - [ ] 2.3 实现 `renderDetailPane()` 方法：四个分区渲染（基础信息、Skill、FD、上下文统计）
  - [ ] 2.4 在 Detail 窗格激活时触发 IPC 查询并缓存
- [ ] Task 3: 数据刷新与缓存（AC: #4, #5, #6）
  - [ ] 3.1 PID 变化时清除旧缓存并异步获取新数据
  - [ ] 3.2 Dead 进程降级处理（部分数据从现有 ProcInfoWire 恢复）
- [ ] Task 4: 测试（AC: #1-#6）
  - [ ] 4.1 IPC 单元测试：GetProcDetail 正常返回、进程不存在返回 not_found
  - [ ] 4.2 Dashboard ATDD 测试：Tab 切换到 Detail 窗格、数据渲染验证

## Dev Notes

### 关键设计决策

**为什么需要新 IPC 方法而非复用 ListProcs？**
现有 `ListProcs` 返回的 `ProcInfoWire` 只包含进程基础字段（PID/State/Skills 名称/Tokens），不包含 FD 表详情和上下文统计。新增 `GetProcDetail` 方法按需获取单个进程的完整详情，避免 `ListProcs` 膨胀。

**环境变量脱敏策略：**
Key 名包含 `KEY`、`SECRET`、`TOKEN`、`PASSWORD` 的值替换为 `***`。从 `Process.ProjectConfig.Env()` 获取（若为 nil 返回空 map）。

### 现有代码模式（必须遵循）

**IPC 方法添加四步曲**（参考 Story 27.2 GetStepDetail 实现）：
1. `ipc/protocol.go`: 新增 Method 常量 + Request/Response 结构体
2. `kernel/kernel.go`: 新增内核方法
3. `ipc/server.go`: `handleConn` switch 中新增 case + handler 函数
4. `ipc/client.go`: 新增客户端方法

**Dashboard 窗格添加模式**（参考现有 Tree/Timeline/Heatmap）：
- `paneType` 是 int 类型，0=tree, 1=timeline, 2=heatmap → 新增 3=detail
- Tab 切换：`m.activePane = (m.activePane + 1) % totalPanes`
- View 中：根据 `m.activePane` 选择渲染函数
- 边框高亮：`activePaneStyle` vs `inactivePaneStyle`

**IPC 异步调用模式**（参考 dashboard 中 `fetchStepDetailCmd`）：
```go
func fetchProcDetailCmd(pid types.PID) tea.Cmd {
    return func() tea.Msg {
        client, err := ipc.AutoClient()
        if err != nil { return procDetailResultMsg{err: err} }
        defer client.Close()
        resp, err := client.GetProcDetail(pid)
        return procDetailResultMsg{detail: resp, err: err}
    }
}
```

### Protocol 类型定义参考

```go
// ipc/protocol.go

MethodGetProcDetail Method = "get_proc_detail"

type GetProcDetailRequest struct {
    PID types.PID `json:"pid"`
}

type GetProcDetailResponse struct {
    PID            types.PID            `json:"pid"`
    UUID           string               `json:"uuid"`
    PPID           types.PID            `json:"ppid"`
    State          string               `json:"state"`
    Intent         string               `json:"intent"`
    Provider       string               `json:"provider"`
    Model          string               `json:"model"`
    CreatedAtMs    int64                `json:"created_at_ms"`
    DeadAtMs       int64                `json:"dead_at_ms,omitempty"`
    Skills         []SkillInfoWire      `json:"skills"`
    AllowedDevices []string             `json:"allowed_devices"`
    EnvSnapshot    map[string]string    `json:"env_snapshot"`
    FDTable        []FDEntryWire        `json:"fd_table"`
    ContextStats   ContextStatsWire     `json:"context_stats"`
}

type SkillInfoWire struct {
    Name         string   `json:"name"`
    AllowedTools []string `json:"allowed_tools"`
}

type FDEntryWire struct {
    FD         types.FD `json:"fd"`
    DevicePath string   `json:"device_path"`
}

type ContextStatsWire struct {
    MessageCount  int     `json:"message_count"`
    TokensUsed    int     `json:"tokens_used"`
    ContextBudget int     `json:"context_budget"`
    UsagePct      float64 `json:"usage_pct"` // 0-100, 若 budget=0 则为 0
}
```

### 内核方法实现要点

**FD 表收集**（`kernel/kernel.go`）：
```go
proc.mu.Lock()
var fdEntries []FDEntryWire
for fd, file := range proc.FDTable {
    fdEntries = append(fdEntries, FDEntryWire{
        FD:         fd,
        DevicePath: file.Path(),
    })
}
proc.mu.Unlock()
```
注意：`vfs.VFSFile` 接口有 `Path() string` 方法。需在 mu.Lock 下遍历，因为 FDTable 可能被 reasonStep 并发修改。对 Dead 进程 FDTable 已被 reaper 清空，返回空数组即可。

**上下文统计**（需从 CtxManager 获取）：
- 消息数和 token 估算通过 `s.ctxMgr.CtxRead(ctxID, 0, 0)` 获取（参考 `handleCtxProfile` 实现）
- 若进程已 Dead 且 CtxID 已释放，返回零值

**Skill 详情**：
- `Process.Skills` 只存 skill 名称列表
- 每个 skill 的 `AllowedTools` 需从 `s.skillLoader.Load(skillName)` 获取
- 若 skillLoader 无法加载（skill 已删除），只返回 name、AllowedTools 为空

### Dashboard 渲染参考

**四分区布局**（在右侧面板区域，替换 heatmap 位置或新增第四列）：
```
┌────────────────────────────────────────┐
│  PID: 3 (abc1234)  State: Running      │
│  Intent: 分析代码结构                    │
│  Provider: claude  Model: opus-4        │
│  Uptime: 2m 34s                        │
├────────────────────────────────────────┤
│  Skills: code-analysis, shell-ops      │
│    → allowed: /dev/fs, /dev/shell      │
├────────────────────────────────────────┤
│  FD Table:                             │
│    0: /dev/llm/claude                  │
│    1: /dev/fs                          │
│    2: /dev/shell                       │
├────────────────────────────────────────┤
│  Context: 42 msgs | 12.5k tok         │
│  Budget: [████████░░] 62%             │
└────────────────────────────────────────┘
```

### 修改文件清单

| 文件 | 修改类型 | 说明 |
|------|----------|------|
| `ipc/protocol.go` | 修改 | 新增 MethodGetProcDetail + 请求/响应类型 |
| `ipc/server.go` | 修改 | 新增 handleGetProcDetail handler |
| `ipc/client.go` | 修改 | 新增 GetProcDetail 客户端方法 |
| `kernel/kernel.go` | 修改 | 新增 GetProcDetail 内核方法 |
| `cmd/rnix/dashboard.go` | 修改 | 新增 Detail 窗格（paneType=3）、渲染、数据获取 |
| `cmd/rnix/atdd_27_6_dashboard_detail_panel_test.go` | 新增 | ATDD 测试 |

### 依赖关系

- **Story 27.2**（GetStepDetail IPC）：参考 IPC 方法添加模式，已完成
- **Story 27.3**（Dashboard Timeline）：参考 dashboard 窗格扩展模式，已完成
- **Story 27.4**（Prompt Pager）：参考 modal 子视图模式，已完成
- **Story 28.1**（UUID v7）：Process.UUID 字段已就位

### 防踩坑清单

1. **FDTable 遍历必须在 mu.Lock 下** — FDTable 是 map，不安全并发读取
2. **VFSFile.Path() 方法** — 确认所有 VFSFile 实现都有此方法，否则用类型断言 fallback
3. **Dead 进程 FDTable 为空** — reaper 清理后 FDTable 为 nil/empty，正确返回空数组
4. **CtxID 已释放** — Dead 进程的 context 可能已被 CtxFree 释放，CtxRead 返回错误时返回零值统计
5. **skillLoader 可能为 nil** — server 构造时检查，若为 nil 则 Skills 只返回名称
6. **环境变量脱敏** — 必须在 server 端脱敏，不能在 client 端（避免敏感数据传输）
7. **paneType 扩展后 Tab 取模** — 确保 `(m.activePane + 1) % 4` 不是硬编码 3
8. **帮助行更新** — 底部帮助文字需加入 Detail 窗格的说明

### 前序 Story 经验

**来自 Story 27-5**：
- BubbleTea `tea.Quit` 返回后从 `p.Run()` 提取 final model 是安全的
- dashboard `dashboardTick` 每次会覆盖 `selectedPID`，新增字段需在正确时机设置
- `dashboardVisibleLines()` 用于计算可见行数
- 测试命名遵循 `TestATDD_27_X_*` 规范

**来自 Story 27-3/27-4**：
- Step detail 缓存在 `stepDetailCache map[int]*ipc.GetStepDetailResponse`，新增的 procDetail 缓存应遵循相同模式
- 异步 IPC 调用使用 `tea.Cmd` 返回消息，在 `Update` 中处理

### Project Structure Notes

- 所有新增 IPC 类型放在 `ipc/protocol.go`，不新建文件
- 内核方法放在 `kernel/kernel.go` 的 Getter 区域（GetProcInfo 附近）
- Dashboard 改动集中在 `cmd/rnix/dashboard.go`，不拆分文件

### References

- [Source: _bmad-output/planning-artifacts/epics/epic-27-统一观察系统-unified-observation-system.md#Story 27.6]
- [Source: ipc/protocol.go — GetStepDetail 方法模式]
- [Source: kernel/kernel.go:2703 — GetProcInfo 实现]
- [Source: kernel/process.go — Process 结构定义（FDTable, Skills, ProjectConfig）]
- [Source: cmd/rnix/dashboard.go — 现有三窗格架构]
- [Source: debug/ctx_profile.go — CtxProfileResult 上下文分析]

## Dev Agent Record

### Agent Model Used

### Debug Log References

### Completion Notes List

### File List
