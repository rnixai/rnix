# Story 27.5: top→dashboard 导航

Status: done

## Story

As a 平台构建者,
I want 在 `rnix top` 中选中进程按回车跳转到 `rnix dashboard` 并自动聚焦该进程,
So that 我可以从系统全局视图快速深入到单进程的详细观察。

## Acceptance Criteria

### AC-1: top 中 Enter 键启动 dashboard

**Given** `rnix top` 显示进程列表，用户通过上下键选中某进程
**When** 用户按 Enter
**Then** top 退出，启动 `rnix dashboard --pid=<selected_pid>`
**And** 切换延迟 ≤ 200ms（NFR61-obs，含 dashboard 初始化和进程聚焦）

### AC-2: dashboard --pid 自动聚焦

**Given** dashboard 以 `--pid=42` 启动
**When** 树窗格加载进程列表（首次 tick 后 treeRows 就绪）
**Then** treeCursor 自动定位到 PID 42 所在行
**And** selectedPID 设为 42
**And** 时间线窗格显示 PID 42 的步骤数据
**And** 上下文热力图显示 PID 42 的 token 分布

### AC-3: --pid 指定不存在的进程

**Given** dashboard 以 `--pid=999` 启动，但 PID 999 不在进程列表中
**When** 首次 tick 加载 treeRows
**Then** 显示 statusMsg 警告 `⚠ PID 999 not found, showing all processes`
**And** dashboard 正常启动，treeCursor 保持默认位置（第 0 行）

### AC-4: top 中 Enter 对已结束进程的处理

**Given** top 中选中的进程已结束（Dead 状态，仍在 top 列表中显示）
**When** 用户按 Enter
**Then** 同样启动 `rnix dashboard --pid=<pid>`
**And** dashboard 正常显示该进程的历史数据（从 steps.jsonl 读取）

### AC-5: dashboard 中 q 退出

**Given** 用户从 top 跳转进入 dashboard
**When** 用户按 `q` 键
**Then** 退出 dashboard 回到终端（不返回 top，因为是独立进程替换）

### AC-6: top 操作提示更新

**Given** `rnix top` 底部操作提示栏
**When** 渲染帮助行
**Then** 提示包含 Enter→Dashboard 指引：`[Enter: dashboard | K: kill | q: quit | ↑↓/jk: navigate]`

### AC-7: top 详情视图不受影响

**Given** top 中正在查看某进程的详情视图（detailPID != 0）
**When** Enter 键功能已改为启动 dashboard
**Then** 详情视图通过其他方式保留（Enter 仅在列表视图下触发 dashboard 跳转）
**And** 在详情视图中 Enter 无操作（或仍然停留在详情视图）

## Tasks / Subtasks

- [x] Task 1: topModel 扩展——launchDashboardPID (AC: #1, #4)
  - [x] 1.1 `topModel` 新增 `launchDashboardPID types.PID` 字段
  - [x] 1.2 `handleKey` 中 Enter 键逻辑修改：列表视图下设置 `launchDashboardPID = rows[cursor].proc.PID` 并返回 `tea.Quit`
  - [x] 1.3 保留 detailPID 机制不变（enter 在 detailPID != 0 时不触发 dashboard 跳转）
- [x] Task 2: runTop 退出后 exec dashboard (AC: #1, #4, #5)
  - [x] 2.1 `runTop` 中 `p.Run()` 返回后，检查 `final.(topModel).launchDashboardPID > 0`
  - [x] 2.2 关闭 IPC client
  - [x] 2.3 获取当前可执行文件路径 `os.Executable()`
  - [x] 2.4 调用 `syscall.Exec(exe, []string{"rnix", "dashboard", fmt.Sprintf("--pid=%d", pid)}, os.Environ())` 进程替换
- [x] Task 3: top 操作提示更新 (AC: #6)
  - [x] 3.1 修改 `View()` 底部帮助行文本
  - [x] 3.2 列表视图：`[Enter: dashboard | K: kill | q: quit | ↑↓/jk: navigate]`
  - [x] 3.3 详情视图：`[Esc: back | K: kill | q: quit]`（无 Enter 提示）
- [x] Task 4: dashboard --pid 聚焦修复 (AC: #2, #3)
  - [x] 4.1 `dashboardModel` 新增 `initialPIDFocus types.PID` 字段
  - [x] 4.2 `runDashboard` 中 `--pid` flag 读取后同时设置 `selectedPID` 和 `initialPIDFocus`
  - [x] 4.3 `dashboardTick` 中 treeRows 就绪后，若 `initialPIDFocus > 0`：遍历 treeRows 找到 PID 匹配的行，设置 `treeCursor` 和 `treeOffset`，清除 `initialPIDFocus`
  - [x] 4.4 PID 未找到时设置 `statusMsg = "⚠ PID N not found, showing all processes"`，清除 `initialPIDFocus`
- [x] Task 5: 测试 (AC: all)
  - [x] 5.1 Enter 键设置 launchDashboardPID 测试：构造 topModel 有 rows，按 Enter 后验证 `launchDashboardPID > 0`
  - [x] 5.2 详情视图中 Enter 不触发 dashboard 跳转测试：设置 `detailPID != 0`，按 Enter 后验证 `launchDashboardPID == 0`
  - [x] 5.3 dashboard initialPIDFocus 匹配测试：构造 dashboardModel 设 `initialPIDFocus`，调用 applyInitialPIDFocus 后验证 `treeCursor` 定位正确
  - [x] 5.4 dashboard initialPIDFocus 未找到测试：设置不存在的 PID，验证 statusMsg 包含警告
  - [x] 5.5 帮助行文本包含 "dashboard" 关键字测试
  - [x] 5.6 `make all` 全部通过

## Dev Notes

### 架构决策引用

- **Decision 26**: Dashboard 增强 — top→dashboard 导航（FR62），通过 `syscall.Exec` 实现进程替换 [Source: architecture/core-architectural-decisions.md#Decision-26]
- **FR62**: 用户可以在 `rnix top` 中通过交互式操作选中进程，按回车跳转到 `rnix dashboard` 并自动聚焦该进程 [Source: prd/functional-requirements.md#FR62]
- **NFR61-obs**: top 下钻到 dashboard 的切换延迟 ≤ 200ms [Source: prd/non-functional-requirements.md#NFR61-obs]
- **UX D.7**: top→dashboard 导航设计——os.Exec 进程替换，不是同一 BubbleTea 内的视图切换 [Source: ux-design-specification.md#D.7]

### 关键实现约束

#### 1. 进程替换而非子进程启动

使用 `syscall.Exec` 做进程替换（exec），不是 `os/exec.Command` 启动子进程。这与 gdb→dashboard 跳转使用相同模式（main.go:487-488）。

进程替换意味着 top 进程直接变成 dashboard 进程，PID 不变，stdout/stderr 不变。用户无感切换，q 退出 dashboard 后直接回到 shell。

```go
exe, _ := os.Executable()
return syscall.Exec(exe, []string{"rnix", "dashboard", fmt.Sprintf("--pid=%d", pid)}, os.Environ())
```

**关键：** 必须在 `syscall.Exec` 前关闭 IPC client（释放 Unix socket 连接），否则 dashboard 启动时可能遇到 socket 问题。gdb→dashboard 跳转已验证此模式可行。

#### 2. Enter 键行为变更

当前 `handleKey` 中 Enter 的行为：
```go
case "enter":
    if m.cursor < len(m.rows) {
        m.detailPID = m.rows[m.cursor].proc.PID  // 进入详情视图
    }
```

**变更后：**
```go
case "enter":
    if m.cursor < len(m.rows) {
        m.launchDashboardPID = m.rows[m.cursor].proc.PID
        return m, tea.Quit
    }
```

**注意：** 原有详情视图（detailPID）可通过 Enter 在列表视图下进入 dashboard 来替代。在 detailPID != 0（已进入详情视图）时，Enter 不应触发 dashboard 跳转——此分支在 `handleKey` 的 `if m.detailPID != 0 {...; return m, nil}` 中已被拦截，无需额外处理。

#### 3. --pid 聚焦修复（dashboard 侧 Bug）

当前 `dashboardTick` 每次 tick 都会覆盖 `selectedPID`：
```go
if m.treeCursor < len(m.treeRows) {
    m.selectedPID = m.treeRows[m.treeCursor].proc.PID  // 覆盖 --pid 值！
}
```

修复方式——在覆盖前检查 `initialPIDFocus`：
```go
if m.initialPIDFocus > 0 && len(m.treeRows) > 0 {
    found := false
    for i, row := range m.treeRows {
        if row.proc.PID == m.initialPIDFocus {
            m.treeCursor = i
            visibleLines := m.dashboardVisibleLines()
            if visibleLines > 0 && m.treeCursor >= m.treeOffset+visibleLines {
                m.treeOffset = m.treeCursor - visibleLines/2
            }
            found = true
            break
        }
    }
    if !found {
        m.statusMsg = fmt.Sprintf("⚠ PID %d not found, showing all processes", m.initialPIDFocus)
    }
    m.initialPIDFocus = 0
}
// 然后继续正常的 treeCursor → selectedPID 同步
if m.treeCursor < len(m.treeRows) {
    m.selectedPID = m.treeRows[m.treeCursor].proc.PID
}
```

这样只在**首次** treeRows 就绪时执行 PID 匹配，之后 `initialPIDFocus` 被清零，不影响后续 tick。

#### 4. runTop 退出处理

```go
func runTop(_ *cobra.Command, _ []string) error {
    client, err := ipc.Dial(ipc.SocketPath())
    if err != nil {
        fmt.Fprintln(rootCmd.ErrOrStderr(), "✗ No rnix daemon running...")
        return nil
    }

    model := newTopModel(client)
    p := tea.NewProgram(model)
    final, err := p.Run()
    if err != nil {
        client.Close()
        return fmt.Errorf("top: %w", err)
    }

    fm, ok := final.(topModel)
    if !ok {
        client.Close()
        return nil
    }

    // Story 27-5: Enter 键跳转到 dashboard
    if fm.launchDashboardPID > 0 {
        if fm.client != nil {
            fm.client.Close()
        } else {
            client.Close()
        }
        exe, err := os.Executable()
        if err != nil {
            return fmt.Errorf("top: cannot find rnix executable: %w", err)
        }
        return syscall.Exec(exe, []string{"rnix", "dashboard", fmt.Sprintf("--pid=%d", fm.launchDashboardPID)}, os.Environ())
    }

    // 正常退出
    if fm.client != nil {
        fm.client.Close()
    } else {
        client.Close()
    }
    return nil
}
```

**关键：** `syscall.Exec` 成功不返回（进程已替换），失败时返回 error。

#### 5. 需要新增 import

top.go 新增：
```go
import (
    "os"
    "syscall"
)
```

### 现有代码关键位置

| 文件 | 行号范围 | 说明 |
|------|----------|------|
| `cmd/rnix/top.go:189-201` | topModel 结构体 | 新增 launchDashboardPID 字段 |
| `cmd/rnix/top.go:271-312` | handleKey 按键处理 | Enter 键行为变更 |
| `cmd/rnix/top.go:409` | View() 帮助行 | 更新提示文本 |
| `cmd/rnix/top.go:417-438` | runTop 函数 | 退出后 exec dashboard |
| `cmd/rnix/dashboard.go:151-214` | dashboardModel 结构体 | 新增 initialPIDFocus 字段 |
| `cmd/rnix/dashboard.go:340-378` | dashboardTick 刷新 | initialPIDFocus 匹配逻辑插入点 |
| `cmd/rnix/dashboard.go:2107-2112` | runDashboard --pid 读取 | 同步设置 initialPIDFocus |
| `cmd/rnix/main.go:268` | --pid flag 定义 | 已存在，无需修改 |
| `cmd/rnix/main.go:487-488` | gdb→dashboard exec 模式 | 参考实现（相同模式） |

### 前序 Story 27-4 经验

- **BubbleTea tea.Quit 模式确认：** model 字段在 `p.Run()` 返回的 final model 中可读——已在 27-4 的 CR 修复中验证
- **IPC client 生命周期：** `p.Run()` 返回后 client 可能已 nil（连接丢失）——需做 nil 检查后再 Close
- **statusMsg 显示模式：** dashboard 已有 statusMsg 渲染机制（底部状态栏），直接复用
- **dashboardVisibleLines()：** 用于计算树窗格可见行数（滚动偏移计算），已在 27-3 中验证

### 防踩坑清单

1. **syscall.Exec 不返回**：成功调用后当前进程已被替换，后续代码不执行。只有失败才返回 error。不要在 `syscall.Exec` 后写任何清理代码
2. **IPC client 必须先关闭**：`syscall.Exec` 前必须 `client.Close()`，否则 Unix socket 文件描述符会泄露到新进程
3. **os.Executable() 可能失败**：理论上不会（我们正在运行中），但仍需 error check
4. **详情视图中 Enter 不受影响**：`handleKey` 中 `if m.detailPID != 0 {...; return m, nil}` 在 switch 之前，已拦截所有按键。Enter 在列表视图才触发 dashboard 跳转
5. **initialPIDFocus 仅首次生效**：必须在匹配后立即清零，否则每次 tick 都会重置 treeCursor
6. **treeOffset 需要同步调整**：如果目标 PID 在可视区域外，需要滚动到可见位置。使用 `treeCursor - visibleLines/2` 将目标行居中
7. **进程列表为空时**：首次 tick 可能 treeRows 为空（daemon 无进程），initialPIDFocus 应在 treeRows 非空后才匹配。若 treeRows 持续为空，最终在下一次 tick 中处理
8. **不要使用 `os/exec.Command`**：必须用 `syscall.Exec`（进程替换），不是 `exec.Command`（创建子进程）。子进程方式会导致 top 进程挂在后台等待 dashboard 退出
9. **帮助行格式一致性**：top 其他提示使用方括号格式 `[Enter] Details`，Story 27-5 按 UX 规范改为 `[Enter: dashboard | K: kill | ...]` 格式

### Project Structure Notes

- 修改文件：`cmd/rnix/top.go`（Enter 键行为 + exec + 帮助行）、`cmd/rnix/dashboard.go`（initialPIDFocus 修复）
- 新增测试文件：`cmd/rnix/atdd_27_5_top_dashboard_nav_test.go`
- 不需要修改 IPC 层、kernel 层、main.go（--pid flag 已定义）
- 依赖：`os`、`syscall` 标准库（top.go 新增 import）

### 组合矩阵

| 交互功能 | 与本 Story 的关系 | 需验证 |
|---------|-------------------|--------|
| top 详情视图（detailPID） | Enter 行为变更，详情视图需保持可用 | 是 |
| gdb→dashboard（main.go:488） | 使用相同 syscall.Exec 模式 | 否（已验证） |
| dashboard prompt pager（27-4） | --pid 聚焦修复不影响 pager | 是 |
| dashboard timeline v/V（27-3） | --pid 聚焦后 timeline 自动加载 | 是 |
| spawn --dashboard（main.go:486） | 使用相同 --pid flag，受益于聚焦修复 | 是 |
| top K kill 功能 | 与 Enter 无冲突 | 否 |

### References

- [Source: architecture/core-architectural-decisions.md#Decision-26] — Dashboard 增强，top→dashboard 导航架构
- [Source: prd/functional-requirements.md#FR62] — top 下钻到 dashboard
- [Source: prd/non-functional-requirements.md#NFR61-obs] — 切换延迟 ≤ 200ms
- [Source: ux-design-specification.md#D.7] — top→dashboard 导航 UX 设计
- [Source: ux-design-specification.md#D.7.2] — --pid 参数行为
- [Source: ux-design-specification.md#D.7.3] — top 操作提示更新
- [Source: sprint-change-proposal-2026-03-22.md] — watch→dashboard 决策
- [Source: 27-4-dashboard-prompt-view.md] — 前序 Story 实现细节
- [Source: epic-27-统一观察系统-unified-observation-system.md#Story-27.5] — Epic 原文

## Dev Agent Record

### Agent Model Used

claude-4.6-opus-high-thinking (Cursor)

### Debug Log References

无

### Completion Notes List

- Task 1: `launchDashboardPID` 字段已在前序 ATDD 阶段添加（top.go:194）。Enter 键行为从 `detailPID` 切换到 `launchDashboardPID + tea.Quit`。`detailPID != 0` 分支在 `handleKey` line 286 处早期拦截，无需额外处理。
- Task 2: `runTop` 退出后检查 `launchDashboardPID > 0`，关闭 IPC client，通过 `syscall.Exec` 进程替换启动 `rnix dashboard --pid=N`。遵循 main.go:487 的 gdb→dashboard 模式。
- Task 3: 列表视图帮助行更新为 `[Enter: dashboard | K: kill | q: quit | ↑↓/jk: navigate]`，详情视图更新为 `[Esc: back | K: kill | q: quit]`。
- Task 4: `initialPIDFocus` 字段已在前序 ATDD 阶段添加。将 focus 逻辑提取为 `applyInitialPIDFocus()` 方法（pointer receiver），在 `dashboardTick` 中 treeRows 构建后、selectedPID 同步前调用。PID 未找到时设置带 TTL 的 statusMsg。
- Task 5: ATDD 测试文件已存在。将 dashboard 测试从 `Update(tickMsg)` 改为直接调用 `applyInitialPIDFocus()`（避免 nil client 导致 tick 短路）。更新 top_test.go 中 `TestTopModel_EnterDetail` → `TestTopModel_EnterLaunchesDashboard` 适配新行为。18 个 ATDD 测试 + 旧测试全部通过。`make all` lint/vet/test/build 全绿。

### File List

- `cmd/rnix/top.go` — Enter 键行为变更 + syscall.Exec dashboard + 帮助行更新 + import os/syscall
- `cmd/rnix/dashboard.go` — applyInitialPIDFocus 方法 + runDashboard 设置 initialPIDFocus
- `cmd/rnix/atdd_27_5_top_dashboard_nav_test.go` — AC-2/AC-3 测试从 tickMsg 改为 applyInitialPIDFocus 直接调用
- `cmd/rnix/top_test.go` — TestTopModel_EnterDetail → TestTopModel_EnterLaunchesDashboard
