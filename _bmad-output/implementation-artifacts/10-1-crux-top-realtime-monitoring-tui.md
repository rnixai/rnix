# Story 10.1: crux top 实时监控 TUI

Status: ready-for-dev

## Story

As a 用户,
I want 通过 `crux top` 实时查看所有智能体的树状关系、状态和 token 消耗,
So that 我随时掌握系统全局运行态。

## Acceptance Criteria

1. **AC1: 全屏实时监控面板**
   - Given `cmd/crux/top.go` 已实现（bubbletea TUI）
   - When 执行 `crux top`
   - Then 全屏显示实时监控面板（bubbletea `tea.WithAltScreen()`）
   - And 上方汇总区：活跃进程数、总 token 消耗、系统运行时间
   - And 下方进程列表：PID、PPID（树状缩进）、STATE、AGENT、TOKENS、ELAPSED

2. **AC2: 实时刷新**
   - Given TUI 运行中
   - When 进程状态变化
   - Then 刷新间隔 ≤ 500ms（NFR28）
   - And 单核 CPU 占用 ≤ 5%（10 个并发进程场景）

3. **AC3: Kill 进程**
   - Given 用户在 TUI 中选中进程
   - When 按 `k` 键
   - Then Kill 选中的进程（调用 `client.Kill(pid, SIGTERM)`）

4. **AC4: 进程详情**
   - Given 用户在 TUI 中选中进程
   - When 按 `Enter` 键
   - Then 显示进程详情（intent、skills、context 摘要）

5. **AC5: 安全退出**
   - Given 按 `q` 或 `ctrl+c`
   - When 退出 TUI
   - Then 恢复终端状态，不影响运行中的进程

## Tasks / Subtasks

- [ ] Task 1: 添加 bubbletea v2 依赖 (AC: #1)
  - [ ] 1.1 `go get charm.land/bubbletea/v2 charm.land/bubbles/v2`
  - [ ] 1.2 验证与现有 lipgloss v1.1.0 兼容
- [ ] Task 2: 实现 `cmd/crux/top.go` cobra 命令 (AC: #1)
  - [ ] 2.1 注册 `top` 子命令（Use: "top", Short: "实时监控面板"）
  - [ ] 2.2 `runTop()` 函数：建立 IPC 连接，初始化 bubbletea Program
  - [ ] 2.3 处理 daemon 未运行场景（优雅降级提示）
- [ ] Task 3: 实现 bubbletea Model (AC: #1, #2)
  - [ ] 3.1 定义 `topModel` 结构体（processes、cursor、detailPID、ipcClient、ticker）
  - [ ] 3.2 `Init()` 返回初始 tick 命令
  - [ ] 3.3 `Update()` 处理 tickMsg（拉取 ListProcs）、KeyPressMsg、windowSizeMsg
  - [ ] 3.4 `View()` 渲染汇总区 + 进程树
- [ ] Task 4: 实现进程树构建 (AC: #1)
  - [ ] 4.1 从 `[]vfs.ProcInfo` 构建 PID→Children 映射
  - [ ] 4.2 DFS 遍历生成树状缩进列表（用 `├──` / `└──` 前缀）
  - [ ] 4.3 处理孤儿进程（PPID 不在列表中的归到根级）
- [ ] Task 5: 实现键盘交互 (AC: #3, #4, #5)
  - [ ] 5.1 `q` / `ctrl+c` → `tea.Quit`
  - [ ] 5.2 `up`/`k` / `down`/`j` → 移动光标
  - [ ] 5.3 `k` → Kill 选中进程（需区分导航键和 Kill 键：仅单独按 `k` 且非导航状态时 Kill）
    - **注意**：`k` 同时用于上移和 Kill，需设计明确的按键方案。建议：`K`（大写/Shift+K）或 `d`（delete）用于 Kill，`k` 仅用于上移，与 vim 习惯一致
  - [ ] 5.4 `Enter` → 切换详情视图（显示 Intent 全文、Skills 列表、CreatedAt、TokensUsed 等）
  - [ ] 5.5 `Esc` → 从详情视图返回列表视图
- [ ] Task 6: 实现 IPC 轮询 (AC: #2)
  - [ ] 6.1 使用 `tea.Tick(500*time.Millisecond, ...)` 触发定时刷新
  - [ ] 6.2 tick 回调中调用 `client.ListProcs()` 获取快照
  - [ ] 6.3 处理 IPC 连接断开（自动重连或显示断开状态）
- [ ] Task 7: 测试 (AC: all)
  - [ ] 7.1 单元测试：进程树构建逻辑
  - [ ] 7.2 单元测试：View 输出格式
  - [ ] 7.3 在 `cmd/crux/main_test.go` 中确认 `top` 命令注册

## Dev Notes

### 关键架构约束

- **依赖方向**：`cmd/crux/` → `ipc/` → `vfs/`（ProcInfo 类型），`cmd/crux/` → `internal/ui/`（styles）
- **新文件位置**：`cmd/crux/top.go`（所有 TUI 逻辑集中在此文件，与现有 `compose.go`、`skill.go` 同级）
- **不创建 `internal/ui/top.go`**：bubbletea Model 属于 `cmd/` 层，因为它直接依赖 IPC client
- **VFS 路径无关**：此故事不涉及 VFS 挂载或 `/proc` 文件系统，只通过 IPC 获取数据

### Bubbletea v2 关键 API

```go
import tea "charm.land/bubbletea/v2"

// Model 接口
type Model interface {
    Init() tea.Cmd
    Update(msg tea.Msg) (tea.Model, tea.Cmd)
    View() tea.View  // v2 返回 tea.View 而非 string
}

// 创建全屏 Program
p := tea.NewProgram(model, tea.WithAltScreen())
_, err := p.Run()

// 定时刷新（核心模式）
type tickMsg time.Time
func tickCmd() tea.Cmd {
    return tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg {
        return tickMsg(t)
    })
}

// 键盘消息（v2 用 KeyPressMsg 替代 KeyMsg）
case tea.KeyPressMsg:
    switch msg.String() {
    case "q", "ctrl+c": return m, tea.Quit
    }

// 窗口大小
case tea.WindowSizeMsg:
    m.width = msg.Width
    m.height = msg.Height

// View 构建
return tea.NewView(s)
```

### IPC 数据获取模式

复用现有 IPC 客户端，轮询 `ListProcs()` ——与 `crux ps` 完全相同的数据通道：

```go
client, err := ipc.Dial(ipc.SocketPath())
// tick 回调中：
procs, err := client.ListProcs()
// procs 类型: []vfs.ProcInfo
```

`vfs.ProcInfo` 字段：
- `PID` / `PPID` — 进程 ID 和父进程 ID
- `State` — ProcessState（Created/Running/Zombie/Dead）
- `Intent` — 智能体意图字符串
- `Skills` — []string 技能列表
- `TokensUsed` — int 累计 token 消耗
- `CreatedAt` — time.Time 创建时间（用于计算 elapsed）
- `CtxID` — 上下文 ID
- `AllowedDevices` — 设备白名单

**注意**：ProcInfo 没有 `Agent` 字段——用 `Skills` 或 `Intent` 代替。表头的 AGENT 列用 Skills[0] 或 Intent 的前 N 个字符。

### 进程树构建算法

```go
type treeNode struct {
    proc     vfs.ProcInfo
    children []*treeNode
    depth    int
}

func buildTree(procs []vfs.ProcInfo) []*treeNode {
    nodes := make(map[types.PID]*treeNode)
    for i := range procs {
        nodes[procs[i].PID] = &treeNode{proc: procs[i]}
    }
    var roots []*treeNode
    for _, n := range nodes {
        if parent, ok := nodes[n.proc.PPID]; ok {
            parent.children = append(parent.children, n)
        } else {
            roots = append(roots, n)
        }
    }
    // 按 PID 排序 children 和 roots
    return roots
}
```

### 复用现有代码

从 `internal/ui/table.go` 复用的工具函数（**不要重新实现**）：
- `formatDuration(d time.Duration) string` — 人类可读时间格式
- `formatTokens(n int) string` — 千分位格式
- `formatSkills(skills []string, maxWidth int, dash string) string` — 技能列表截断
- `stripAnsi(s string) string` — ANSI 转义序列清除

从 `internal/ui/styles.go` 复用的样式：
- `KernelStyle` / `AgentStyle` / `SuccessStyle` / `ErrorStyle` / `WarningStyle` / `MutedStyle`
- `InitStyles(profile)` — 初始化样式（需在 TUI 中用 lipgloss 适配）
- 颜色常量：`ColorKernel="#888888"`, `ColorAgent="#5B9BD5"`, `ColorSuccess="#6BCB77"`, `ColorWarning="#FFD93D"`, `ColorError="#FF6B6B"`, `ColorMuted="#666666"`

### View 布局设计

```
┌─────────────────────────────────────────────────────────────┐
│  crux top — 3 active, 0 zombie | Tokens: 12,450 | Up: 5.2m │  ← 汇总区（1行）
├─────────────────────────────────────────────────────────────┤
│  PID  PPID  STATE     AGENT           TOKENS   ELAPSED     │  ← 表头
│  ─── ───── ───────── ─────────────── ──────── ────────     │
│▸ 1    0     Running   code-analyst     5,200    3.1m       │  ← 选中行（▸ 前缀高亮）
│  ├─ 2 1     Running   reviewer         3,100    2.0m       │  ← 子进程缩进
│  └─ 3 1     Zombie    tester           4,150    1.5m       │
│  4    0     Running   deployer            0    0.3s        │
├─────────────────────────────────────────────────────────────┤
│  [q] Quit  [k/K] Kill  [Enter] Details  [↑↓] Navigate     │  ← 帮助栏（1行）
└─────────────────────────────────────────────────────────────┘
```

**详情视图**（Enter 后切换到半屏覆盖）：

```
┌── Process Detail ─── PID 1 ──────────────────────────────────┐
│  State:    Running                                            │
│  Intent:   分析代码库中的安全漏洞                               │
│  Skills:   code-analysis, security-scan                       │
│  Tokens:   5,200                                              │
│  Elapsed:  3m 6.2s                                            │
│  Context:  CtxID=1                                            │
│  Devices:  /dev/llm/claude, /dev/fs                           │
│  Children: PID 2, PID 3                                       │
├───────────────────────────────────────────────────────────────┤
│  [Esc] Back  [k/K] Kill                                      │
└───────────────────────────────────────────────────────────────┘
```

### 性能约束（NFR28）

- 轮询间隔 500ms（`tea.Tick`）
- `ListProcs()` 通过 Unix socket IPC，典型延迟 < 1ms
- 进程树构建 O(n)，10 个进程场景下几乎零开销
- bubbletea 内部 diff 渲染，仅更新变化部分

### 键盘映射（明确方案）

| 按键 | 列表视图 | 详情视图 |
|------|---------|---------|
| `q` / `ctrl+c` | 退出 TUI | 退出 TUI |
| `up` / `k` | 光标上移 | — |
| `down` / `j` | 光标下移 | — |
| `K` (Shift+K) | Kill 选中进程 | Kill 当前进程 |
| `Enter` | 打开详情 | — |
| `Esc` | — | 返回列表 |

**设计理由**：用 `K`（大写）执行 Kill，`k`（小写）用于导航，避免按键冲突。这与 `less` 和 `htop` 的惯例一致（危险操作用 Shift 修饰键）。

### 命令注册模式

参考现有命令注册（`cmd/crux/main.go`）：

```go
// 在 init() 中注册
func init() {
    rootCmd.AddCommand(topCmd)
}

var topCmd = &cobra.Command{
    Use:   "top",
    Short: "Real-time process monitoring dashboard",
    Long:  "Interactive TUI showing process tree, status, and resource consumption in real-time.",
    Args:  cobra.NoArgs,
    RunE:  runTop,
}
```

### Daemon 未运行处理

如果 `ipc.Dial()` 失败：
- 不启动 TUI，直接打印 `"✗ No crux daemon running. Start an agent first with: crux -i \"intent\""` 并退出
- 与 `crux ps` 的 "No active processes" 模式对齐

### IPC 连接断开处理

TUI 运行中如果 `ListProcs()` 返回错误：
- 在汇总区显示 `[disconnected]` 状态
- 每次 tick 尝试重新 `ipc.Dial()`
- 重连成功后自动恢复数据显示

### 测试策略

- **进程树构建**：纯函数，mock ProcInfo 列表，验证树结构和缩进
- **View 输出**：构造 topModel 快照，调用 View()，验证输出字符串包含预期字段
- **命令注册**：在 main_test.go 中验证 `top` 命令可被识别（更新现有测试，移除 `"unknown command \"top\""` 的断言）
- **不需要 E2E IPC 测试**：IPC 层已被 Epic 4-6 充分测试

### Project Structure Notes

- 新文件：`cmd/crux/top.go`（+ 对应 `cmd/crux/top_test.go`）
- 修改文件：`cmd/crux/main.go`（注册 topCmd）
- 新依赖：`charm.land/bubbletea/v2`, `charm.land/bubbles/v2`
- 可能需要导出 `internal/ui/table.go` 中的格式化函数（已导出：`FormatDuration` 等——确认是否已经导出，如果是小写的则需要导出或在 top.go 中复制）

**确认**：`formatDuration`、`formatTokens`、`formatSkills` 在 `table.go` 中是**小写**（未导出）。有两个选择：
1. **推荐**：导出这些函数（改为 `FormatDuration`、`FormatTokens`、`FormatSkills`），同时更新 `table.go` 中的调用
2. 在 `top.go` 中复制（违反 DRY，不推荐）

### References

- [Source: _bmad-output/planning-artifacts/epics/epic-10-监控supervisor-与运维monitoring-supervisor-operations.md#Story 10.1]
- [Source: _bmad-output/planning-artifacts/archive/architecture.md#Decision 2: 进程模型与并发]
- [Source: _bmad-output/project-context.md#CLI 命令结构]
- [Source: vfs/proc.go#ProcInfo 结构体]
- [Source: ipc/client.go#Client API]
- [Source: internal/ui/table.go#formatDuration/formatTokens]
- [Source: internal/ui/styles.go#样式定义]
- [Source: cmd/crux/main.go#runPs 参考实现]
- [Source: charm.land/bubbletea/v2 README#Elm Architecture 模式]

## Dev Agent Record

### Agent Model Used

{{agent_model_name_version}}

### Debug Log References

### Completion Notes List

### File List
