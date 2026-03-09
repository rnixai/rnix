---
stepsCompleted:
  - step-01-preflight-and-context
  - step-02-generation-mode
  - step-03-test-strategy
  - step-04-generate-tests
  - step-05-validate-and-complete
lastStep: step-05-validate-and-complete
lastSaved: '2026-03-09'
workflowType: testarch-atdd
inputDocuments:
  - _bmad-output/implementation-artifacts/17-1-dashboard-framework-and-agent-tree-pane.md
  - cmd/rnix/top.go
  - cmd/rnix/top_test.go
  - internal/ui/styles.go
  - internal/ui/table.go
  - ipc/protocol.go
  - vfs/proc.go
  - go.mod
---

# ATDD 检查清单 - Epic 17, Story 17-1: Dashboard 框架与智能体树窗格

**日期：** 2026-03-09
**作者：** Decker
**主要测试级别：** Unit (Go `testing` 包)

---

## 故事摘要

实现 `rnix dashboard` 全屏 TUI 面板命令，建立多窗格布局框架并实现首个功能窗格（智能体树），实时展示进程父子关系、状态和 token 消耗。

**As a** 平台构建者
**I want** 通过 `rnix dashboard` 启动全屏 TUI 面板，在智能体树窗格中实时查看所有进程的状态
**So that** 我可以纵览整个系统的运行状态

---

## 验收标准

1. **AC1**: 执行 `rnix dashboard` → 启动全屏 bubbletea TUI 应用，默认显示多窗格视图
2. **AC2**: 智能体树窗格实时显示进程父子关系、状态（Running/Zombie/Dead）、token 消耗；刷新间隔 ≤500ms，10 并发进程 CPU ≤10%（NFR36）；支持 ≥50 进程无卡顿（NFR37）

---

## 失败测试创建（RED 阶段）

### Unit Tests (21 tests)

**文件：** `cmd/rnix/dashboard_test.go` (470 行)

| # | 测试 ID | 测试名称 | 优先级 | AC | 状态 | 失败原因 |
|---|---------|----------|--------|-----|------|----------|
| 1 | 17.1-INT-001 | TestHelp_ContainsDashboardSubcommand | P0 | AC1 | GREEN | 命令已注册 |
| 2 | 17.1-INT-002 | TestRunDashboard_NoDaemon | P1 | AC1 | GREEN | 桩返回 nil |
| 3 | 17.1-UNIT-001 | TestDashboardModel_Init | P0 | AC1 | RED | Init() 返回 nil，应返回 tickCmd |
| 4 | 17.1-UNIT-002 | TestDashboardModel_ViewAltScreen | P0 | AC1 | RED | View() 返回空 tea.View，AltScreen=false |
| 5 | 17.1-UNIT-003 | TestDashboardModel_ViewMultiPaneLayout | P0 | AC1 | RED | View 内容为空，缺少窗格标题 |
| 6 | 17.1-UNIT-004 | TestDashboardModel_ViewTitleBar | P1 | AC1 | RED | View 缺少 "Rnix Dashboard" 标题 |
| 7 | 17.1-UNIT-005 | TestDashboardModel_ViewStatusBar | P1 | AC1 | RED | View 缺少键盘快捷键提示 |
| 8 | 17.1-UNIT-006 | TestDashboardBuildProcessTree_Empty | P0 | AC2 | GREEN | 空输入返回 nil（正确） |
| 9 | 17.1-UNIT-007 | TestDashboardBuildProcessTree_ParentChild | P0 | AC2 | RED | buildProcessTree 返回 nil |
| 10 | 17.1-UNIT-008 | TestDashboardBuildProcessTree_DeepNesting | P0 | AC2 | RED | buildProcessTree 返回 nil |
| 11 | 17.1-UNIT-009 | TestDashboardFlattenTree_Indentation | P0 | AC2 | RED | buildProcessTree 返回 nil，无行可展平 |
| 12 | 17.1-UNIT-010 | TestDashboardModel_TickUpdatesTree | P0 | AC2 | RED | Update(tickMsg) 返回 nil cmd |
| 13 | 17.1-UNIT-011 | TestDashboardModel_NavigateJK | P0 | AC2 | RED | Update 不处理键盘，cursor 不变 |
| 14 | 17.1-UNIT-012 | TestDashboardModel_QuitQ | P0 | AC1 | RED | Update 不处理 'q'，返回 nil cmd |
| 15 | 17.1-UNIT-013 | TestDashboardModel_TabSwitchPane | P0 | AC1 | RED | Update 不处理 Tab，activePane 不变 |
| 16 | 17.1-UNIT-014 | TestDashboardModel_KillConfirmY | P0 | AC2 | RED | Update 不处理 'K'，confirmKill 不变 |
| 17 | 17.1-UNIT-015 | TestDashboardModel_KillConfirmN | P0 | AC2 | RED | Update 不处理 'K'，confirmKill 不变 |
| 18 | 17.1-UNIT-016 | TestDashboardModel_DaemonDisconnect | P1 | AC2 | RED | Update 不处理 tickMsg |
| 19 | 17.1-UNIT-017 | TestDashboardModel_50Processes | P0 | NFR37 | RED | View 返回空内容 |
| 20 | 17.1-UNIT-018 | TestDashboardModel_SelectedPIDSync | P1 | AC2 | RED | selectedPID 不随 cursor 更新 |
| 21 | 17.1-UNIT-019 | TestDashboardModel_ScrollViewport | P1 | AC2 | RED | treeOffset 不随 cursor 滚动 |
| 22 | 17.1-UNIT-020 | TestDashboardModel_ViewProcessStates | P1 | AC2 | RED | View 不渲染进程状态 |
| 23 | 17.1-UNIT-021 | TestDashboardModel_ViewTokenBudgetWarning | P1 | AC2 | RED | View 不渲染 token 消耗 |

**总计：** 23 个测试，3 个 GREEN（基础设施），20 个 RED（待实现）

### API Tests

不适用（Go 后端项目，所有交互通过 IPC 内部调用，无 HTTP API）

### Component Tests

不适用（Go CLI TUI 项目，无 UI 组件框架）

---

## 数据工厂

### mockDashboardProcs 工厂

**文件：** `cmd/rnix/dashboard_test.go`

**导出：**

- `mockDashboardProcs()` - 创建 4 个测试进程（含父子关系，Running/Zombie 混合状态）
- `newTestDashboardModel(procs)` - 创建预填充的 dashboardModel（含 treeRows、尺寸、连接状态）

**示例用法：**

```go
procs := mockDashboardProcs()
m := newTestDashboardModel(procs)
v := m.View()
```

---

## Fixtures

### Go Test Helpers

**文件：** `cmd/rnix/dashboard_test.go`

**Helpers：**

- `mockDashboardProcs()` - 提供标准 4 进程测试数据集
  - **Setup：** 创建 PID 1（root）、PID 2/3（子进程）、PID 5（孙进程），含不同状态和 token
  - **提供：** `[]vfs.ProcInfo` 可直接用于 buildProcessTree 和 model 测试
  - **清理：** 无需（纯值类型，无副作用）

- `newTestDashboardModel(procs)` - 构建可测试的 dashboard model
  - **Setup：** 初始化 model 并预填充 treeRows、设置终端尺寸 120x40、connected=true
  - **提供：** 可直接调用 Update/View 的 dashboardModel
  - **清理：** 无需（纯值类型）

---

## Mock 需求

### IPC Client Mock

**方法：** `ipc.Client.ListProcs()`, `ipc.Client.Kill()`

**策略：** 与 `top_test.go` 相同 — 不创建 mock 接口。测试通过以下方式绕过 IPC：

1. 纯函数测试（`buildProcessTree`、`flattenTree`）直接传入 `[]vfs.ProcInfo`
2. Model 测试通过 `newTestDashboardModel` 预填充数据
3. Tick 测试使用 `ipc.SocketPathOverride` 指向不存在的 socket
4. Kill 测试验证 model 状态变化（`confirmKill`/`confirmPID`），不实际执行 IPC

---

## 必要 data-testid 属性

不适用（Go TUI 项目，使用 bubbletea 终端渲染，无 HTML DOM）

---

## 实现检查清单

### Test: TestDashboardModel_Init (17.1-UNIT-001)

**文件：** `cmd/rnix/dashboard_test.go`

**使此测试通过的任务：**

- [ ] 在 `newDashboardModel` 中初始化 `startTime: time.Now()` 和 `connected: client != nil`
- [ ] 实现 `Init()` 返回 `tickCmd()`（复用 top.go 的 tickCmd 函数）
- [ ] 运行测试：`go test -run TestDashboardModel_Init ./cmd/rnix/`
- [ ] ✅ 测试通过（green phase）

**预估工时：** 0.25 小时

---

### Test: TestDashboardModel_ViewAltScreen + ViewMultiPaneLayout + ViewTitleBar + ViewStatusBar (17.1-UNIT-002~005)

**文件：** `cmd/rnix/dashboard_test.go`

**使此测试通过的任务：**

- [ ] 实现 `renderLayout()` 函数：使用 lipgloss 将终端区域分割为左侧智能体树窗格（40%）和右侧上下占位窗格
- [ ] 实现标题栏渲染："Rnix Dashboard" + 连接状态 + 活跃进程数 + 总 token
- [ ] 实现底部状态栏：快捷键提示（q=Quit, Tab=Switch, j/k=Navigate, K=Kill）
- [ ] 实现窗格标题：左侧 "Agent Tree"，右上 "Timeline (Coming Soon)"，右下 "Heatmap (Coming Soon)"
- [ ] 实现窗格边框：使用 lipgloss Border()，活跃窗格用 ColorAgent 高亮
- [ ] 实现 `View()` 返回 `tea.NewView(content)` 并设置 `v.AltScreen = true`
- [ ] 运行测试：`go test -run "TestDashboardModel_View" ./cmd/rnix/`
- [ ] ✅ 测试通过（green phase）

**预估工时：** 2 小时

---

### Test: TestDashboardBuildProcessTree_* (17.1-UNIT-006~008)

**文件：** `cmd/rnix/dashboard_test.go`

**使此测试通过的任务：**

- [ ] 实现 `buildProcessTree(procs)` 从 PPID 构建父子关系树
- [ ] 处理孤儿进程（PPID 不在列表中 → 视为 root）
- [ ] 每层子节点按 PID 排序
- [ ] 运行测试：`go test -run "TestDashboardBuildProcessTree" ./cmd/rnix/`
- [ ] ✅ 测试通过（green phase）

**预估工时：** 0.5 小时

---

### Test: TestDashboardFlattenTree_Indentation (17.1-UNIT-009)

**文件：** `cmd/rnix/dashboard_test.go`

**使此测试通过的任务：**

- [ ] `buildProcessTree` 实现后，此测试自动依赖 top.go 的 `flattenTree` 生成正确缩进
- [ ] 验证 `buildProcessTree` + `flattenTree` 端到端正确性
- [ ] 运行测试：`go test -run TestDashboardFlattenTree ./cmd/rnix/`
- [ ] ✅ 测试通过（green phase）

**预估工时：** 0.25 小时（包含在 buildProcessTree 中）

---

### Test: TestDashboardModel_TickUpdatesTree + DaemonDisconnect (17.1-UNIT-010, 016)

**文件：** `cmd/rnix/dashboard_test.go`

**使此测试通过的任务：**

- [ ] 实现 `handleTick()` 方法：检测 client==nil → 尝试 Dial → ListProcs → buildProcessTree → flattenTree
- [ ] 处理 IPC 错误 → connected=false → 下次 tick 重试
- [ ] Tick 后返回 `tickCmd()` 调度下一次刷新
- [ ] 运行测试：`go test -run "TestDashboardModel_Tick|TestDashboardModel_DaemonDisconnect" ./cmd/rnix/`
- [ ] ✅ 测试通过（green phase）

**预估工时：** 1 小时

---

### Test: TestDashboardModel_NavigateJK + SelectedPIDSync + ScrollViewport (17.1-UNIT-011, 018, 019)

**文件：** `cmd/rnix/dashboard_test.go`

**使此测试通过的任务：**

- [ ] 实现 `handleKey(msg)` 处理 j/k/↑/↓ 移动 treeCursor
- [ ] 光标移动时更新 selectedPID 为当前行的 PID
- [ ] 实现 `ensureCursorVisible(visibleLines)` 滚动视口跟随光标
- [ ] 光标到顶不越界（cursor >= 0）、到底不越界（cursor < len(treeRows)）
- [ ] 运行测试：`go test -run "TestDashboardModel_Navigate|TestDashboardModel_Selected|TestDashboardModel_Scroll" ./cmd/rnix/`
- [ ] ✅ 测试通过（green phase）

**预估工时：** 1 小时

---

### Test: TestDashboardModel_QuitQ (17.1-UNIT-012)

**文件：** `cmd/rnix/dashboard_test.go`

**使此测试通过的任务：**

- [ ] 在 handleKey 中处理 'q' 和 'ctrl+c' → 返回 `tea.Quit`
- [ ] 运行测试：`go test -run TestDashboardModel_QuitQ ./cmd/rnix/`
- [ ] ✅ 测试通过（green phase）

**预估工时：** 0.25 小时

---

### Test: TestDashboardModel_TabSwitchPane (17.1-UNIT-013)

**文件：** `cmd/rnix/dashboard_test.go`

**使此测试通过的任务：**

- [ ] 在 handleKey 中处理 Tab → 在 paneTree → paneTimeline → paneHeatmap 之间循环
- [ ] 活跃窗格用 ColorAgent 高亮边框（View 渲染中体现）
- [ ] 运行测试：`go test -run TestDashboardModel_TabSwitchPane ./cmd/rnix/`
- [ ] ✅ 测试通过（green phase）

**预估工时：** 0.5 小时

---

### Test: TestDashboardModel_KillConfirmY + KillConfirmN (17.1-UNIT-014, 015)

**文件：** `cmd/rnix/dashboard_test.go`

**使此测试通过的任务：**

- [ ] 在 handleKey 中处理 'K'（Shift+k）→ 设置 confirmKill=true, confirmPID=selectedPID
- [ ] 在 confirmKill 模式下处理 'y' → 调用 client.Kill(confirmPID, SIGTERM) → 重置 confirmKill
- [ ] 在 confirmKill 模式下处理 'n' 或其他键 → 重置 confirmKill（取消）
- [ ] 在 View 中 confirmKill 时显示确认提示 "Kill PID X? [y/N]"
- [ ] 运行测试：`go test -run "TestDashboardModel_KillConfirm" ./cmd/rnix/`
- [ ] ✅ 测试通过（green phase）

**预估工时：** 1 小时

---

### Test: TestDashboardModel_50Processes (17.1-UNIT-017)

**文件：** `cmd/rnix/dashboard_test.go`

**使此测试通过的任务：**

- [ ] View() 实现后此测试自动覆盖（需要 View 正确渲染 50 个进程）
- [ ] 验证渲染只处理可见行（虚拟滚动），50 节点下无 panic
- [ ] 运行测试：`go test -run TestDashboardModel_50Processes ./cmd/rnix/`
- [ ] ✅ 测试通过（green phase）

**预估工时：** 0.25 小时（包含在 View 实现中）

---

### Test: TestDashboardModel_ViewProcessStates + ViewTokenBudgetWarning (17.1-UNIT-020, 021)

**文件：** `cmd/rnix/dashboard_test.go`

**使此测试通过的任务：**

- [ ] 在 renderTreePane() 中为每行渲染：State（Running=绿/Zombie=黄/Dead=灰）、Agent 名称、Token 消耗
- [ ] Token 预算 ≥80% 时使用 WarningStyle 渲染
- [ ] 复用 `ui.FormatTokens`/`ui.FormatDuration`/`ui.FormatSkills` 函数
- [ ] 运行测试：`go test -run "TestDashboardModel_ViewProcess|TestDashboardModel_ViewToken" ./cmd/rnix/`
- [ ] ✅ 测试通过（green phase）

**预估工时：** 1 小时

---

## 运行测试

```bash
# 运行本 story 所有失败测试
go test -v -run "TestDashboard|TestHelp_ContainsDashboardSubcommand|TestRunDashboard" ./cmd/rnix/

# 运行特定测试文件
go test -v -run "TestDashboard" ./cmd/rnix/

# 运行 buildProcessTree 相关测试
go test -v -run "TestDashboardBuildProcessTree" ./cmd/rnix/

# 运行键盘交互测试
go test -v -run "TestDashboardModel_Navigate|TestDashboardModel_Quit|TestDashboardModel_Tab|TestDashboardModel_Kill" ./cmd/rnix/

# 运行性能测试（NFR37）
go test -v -run "TestDashboardModel_50Processes" ./cmd/rnix/

# 运行所有 cmd/rnix 测试（含 top.go）
go test -v ./cmd/rnix/
```

---

## Red-Green-Refactor 工作流

### RED 阶段（完成）✅

**TEA Agent 职责：**

- ✅ 23 个测试已编写，20 个失败中
- ✅ 桩文件 `dashboard.go` 创建，函数签名就位
- ✅ 命令在 `main.go` 中注册
- ✅ 测试工厂和辅助函数创建（mockDashboardProcs, newTestDashboardModel）
- ✅ 实现检查清单创建

**验证：**

- 所有测试运行且按预期失败
- 失败消息清晰且可操作
- 测试因缺少实现而失败，非测试 bug

---

### GREEN 阶段（DEV 团队 - 下一步）

**DEV Agent 职责：**

1. **选择一个失败测试**（建议从实现检查清单最高优先级开始）
2. **阅读测试**理解预期行为
3. **实现最小代码**使该测试通过
4. **运行测试**验证通过（green）
5. **标记任务完成**
6. **移至下一个测试**

**建议实现顺序：**

1. `buildProcessTree` — 纯函数，最简单起步
2. `Init()` 和 `newDashboardModel` — 基础初始化
3. `handleKey` — 键盘处理（q, j/k, Tab, K）
4. `handleTick` — IPC 轮询
5. `View()` 和 `renderLayout()` — 视图渲染（最复杂）

---

### REFACTOR 阶段（DEV 团队 - 所有测试通过后）

1. 验证所有 23 个测试通过
2. 检查代码质量（可读性、一致性）
3. 与 `top.go` 代码风格对齐
4. 确保 lipgloss 样式使用一致
5. 确保测试仍然通过

---

## 下一步

1. **将此检查清单分享给 dev 工作流**（手动交接）
2. **运行失败测试**确认 RED 阶段：`go test -v -run "TestDashboard" ./cmd/rnix/`
3. **按实现检查清单顺序开始实现** `cmd/rnix/dashboard.go`
4. **每次实现一个测试**（red → green）
5. **所有测试通过后**重构代码质量
6. **重构完成后**在 sprint-status.yaml 中更新故事状态

---

## 知识库参考

本 ATDD 工作流参考了以下知识片段：

- **test-quality.md** — 测试质量定义：确定性、隔离性、显式断言、<300 行、<1.5 分钟
- **test-levels-framework.md** — 测试级别选择：Unit 用于纯函数和逻辑，Integration 用于组件交互

---

## 测试执行证据

### 初始测试运行（RED 阶段验证）

**命令：** `go test -v -run "TestDashboard|TestHelp_ContainsDashboardSubcommand|TestRunDashboard" ./cmd/rnix/`

**结果：**

```
=== RUN   TestHelp_ContainsDashboardSubcommand
--- PASS: TestHelp_ContainsDashboardSubcommand (0.00s)
=== RUN   TestRunDashboard_NoDaemon
--- PASS: TestRunDashboard_NoDaemon (0.00s)
=== RUN   TestDashboardModel_Init
    dashboard_test.go:84: Init() should return a non-nil tick command
--- FAIL: TestDashboardModel_Init (0.00s)
=== RUN   TestDashboardModel_ViewAltScreen
    dashboard_test.go:94: View should set AltScreen = true
    dashboard_test.go:97: View content should not be empty
--- FAIL: TestDashboardModel_ViewAltScreen (0.00s)
... (18 more FAIL tests)
FAIL  github.com/rnixai/rnix/cmd/rnix  0.005s
```

**总计：**

- 总测试数：23
- 通过：3（基础设施/trivial）
- 失败：20（预期 — RED 阶段）
- 状态：✅ RED 阶段已验证

---

## 备注

- Go 项目无需 Playwright/Cypress/data-testid — 所有测试为 Go 标准 `testing` 包单元测试
- `buildProcessTree` 与 `top.go` 的 `buildTree` 算法相同但独立命名（同包内不可重名）
- `flattenTree` 和 `treeNode`/`flatRow` 类型复用 `top.go` 已有定义
- `tickMsg`/`tickCmd` 复用 `top.go` 已有定义（500ms 间隔）
- Kill 确认流程是 dashboard 独有的两步操作（K → y/n），不同于 top.go 的直接 kill

---

## 联系

**问题或疑问？**

- 参考 `cmd/rnix/top.go` 和 `cmd/rnix/top_test.go` 了解 bubbletea v2 TUI 模式
- 参考 `internal/ui/styles.go` 了解颜色体系
- 参考 `internal/ui/table.go` 了解 FormatDuration/FormatTokens 函数

---

**Generated by BMad TEA Agent** - 2026-03-09
