---
stepsCompleted:
  - step-01-preflight-and-context
  - step-02-generation-mode
  - step-03-test-strategy
  - step-04-generate-tests
  - step-05-validate-and-complete
lastStep: step-05-validate-and-complete
lastSaved: '2026-03-23'
storyId: '29-5'
detectedStack: backend
generationMode: ai-generation
inputDocuments:
  - _bmad-output/implementation-artifacts/29-5-dashboard-history-view.md
  - cmd/rnix/dashboard.go
  - cmd/rnix/dashboard_nav.go
  - cmd/rnix/dashboard_types.go
  - cmd/rnix/dashboard_tree.go
  - cmd/rnix/dashboard_timeline.go
  - vfs/proc.go
  - internal/ui/symbols.go
  - ipc/client.go
---

# ATDD Checklist — Story 29.5: Dashboard 历史进程视图

**日期:** 2026-03-23
**作者:** Decker
**主要测试级别:** Unit (AST 结构验证 + 源码文本分析)

---

## Story 概要

**As a** 平台构建者
**I want** 在 Dashboard 中按 H 键打开全屏历史进程列表
**So that** 我可以查看、搜索和聚焦已结束的进程，实现事后分析能力

---

## 验收准则

1. **AC-1**: H 键进入历史视图 — `viewHistory` 覆盖层，通过 `ListAllProcs` IPC 获取数据
2. **AC-2**: 进程表渲染 — 列：PID|ST|AGENT|MODEL|TOKENS|CREATED|ELAPSED|EXIT|REASON；底部统计
3. **AC-3**: 光标导航 — j/k 或上/下键在进程列表中导航
4. **AC-4**: Enter 聚焦 — 设置 selectedPID + selectedUUID，回到默认视图
5. **AC-5**: L 键跳转 LLM 查看器 — placeholder（Story 29.6）
6. **AC-6**: 搜索过滤 — / 进入搜索模式，按 agent 名称过滤
7. **AC-7**: 排序模式 — 1/2/3 切换排序（时间/名称/PID）
8. **AC-8**: Esc 退出 — 回到之前的视图
9. **AC-9**: Tree 面板已结束进程 — ListAllProcs + ✓/✕ 状态符号 + exit code + 存活时间
10. **AC-10**: Timeline 自动过滤 — Dead 进程时过滤只显示该 PID 事件

---

## Test Strategy

| AC | 测试级别 | 优先级 | 场景数 | 说明 |
|----|----------|--------|--------|------|
| AC-1 | Unit | P0 | 9 | 文件存在、类型定义、字段完整、enterHistoryView/fetchAllProcsCmd 逻辑、H 键路由、覆盖层分发、Update/View 路由 |
| AC-2 | Unit | P0/P1 | 5 | 渲染函数签名、表头列、底部统计、StateSymbol 使用、标题栏 |
| AC-3 | Unit | P0 | 1 | historyKey j/k 导航 + historyCursor 修改 |
| AC-4 | Unit | P0 | 2 | Enter 设置 selectedPID/selectedUUID + viewDefault + handlePIDChange |
| AC-5 | Unit | P0 | 1 | historyKey 处理 L 键 |
| AC-6 | Unit | P0/P1 | 3 | / 搜索模式 + agent 名称过滤 + Backspace rune-safe |
| AC-7 | Unit | P0/P1 | 2 | 1/2/3 排序模式 + slices.SortFunc + 三种排序字段 |
| AC-8 | Unit | P0 | 1 | Esc 退出回 viewDefault |
| AC-9 | Unit | P0/P1 | 3 | Tree 使用 ListAllProcs + StateSymbol + DeadAt/exit code |
| AC-10 | Unit | P0/P1 | 2 | Timeline PID 过滤逻辑 + "(filtered: PID N)" 提示 |

---

## Generated Test Files

| 文件 | 测试数 | 覆盖 AC | 红灯状态 |
|------|--------|---------|----------|
| `cmd/rnix/atdd_29_5_dashboard_history_view_test.go` | 36 | AC-1~10 | 全部 FAIL |

---

## Failing Tests Created (RED Phase)

### Unit Tests (36 tests)

**文件:** `cmd/rnix/atdd_29_5_dashboard_history_view_test.go`

#### AC-1: H 键进入历史视图

- **29.5-UNIT-001** [P0] `TestHistoryView_FileExists`
  - **状态:** RED — dashboard_history.go 文件不存在
  - **验证:** AC-1 新增文件存在

- **29.5-UNIT-002** [P0] `TestHistoryView_ExpectedFunctionsExist`
  - **状态:** RED — 文件不存在，无法解析函数
  - **验证:** enterHistoryView, historyKey, renderHistoryView, fetchAllProcsCmd 函数

- **29.5-UNIT-003** [P0] `TestHistoryView_HistoryProcsMsgTypeExists`
  - **状态:** RED — dashboard_types.go 中无 historyProcsMsg 类型
  - **验证:** AC-1 消息类型定义

- **29.5-UNIT-004** [P0] `TestHistoryView_DashboardModelHistoryFields`
  - **状态:** RED — dashboardModel 缺少 6 个历史视图字段
  - **验证:** historyProcs, historyCursor, historyScrollOffset, historySortMode, historySearchQuery, historySearchMode

- **29.5-UNIT-005** [P0] `TestHistoryView_EnterHistoryViewSetsViewMode`
  - **状态:** RED — enterHistoryView 不存在
  - **验证:** 设置 viewMode = viewHistory

- **29.5-UNIT-006** [P0] `TestHistoryView_FetchAllProcsCmdCallsListAllProcs`
  - **状态:** RED — fetchAllProcsCmd 不存在
  - **验证:** 调用 client.ListAllProcs()

- **29.5-UNIT-007** [P0] `TestHistoryView_HKeyCallsEnterHistoryView`
  - **状态:** RED — H 键仍是 placeholder
  - **验证:** dashboard_nav.go 不再有 placeholder，调用 enterHistoryView

- **29.5-UNIT-008** [P0] `TestHistoryView_OverlayLayerInNav`
  - **状态:** RED — dashboard_nav.go 无 viewHistory 覆盖层
  - **验证:** Layer 1.5 覆盖层分发 historyKey

- **29.5-UNIT-009** [P0] `TestHistoryView_UpdateHandlesHistoryProcsMsg`
  - **状态:** RED — dashboard.go 无 historyProcsMsg 处理
  - **验证:** Update 方法处理 historyProcsMsg

- **29.5-UNIT-010** [P0] `TestHistoryView_ViewRoutesViewHistory`
  - **状态:** RED — renderDashboard 无 viewHistory 路由
  - **验证:** View 方法路由到 renderHistoryView

#### AC-2: 进程表渲染

- **29.5-UNIT-011** [P0] `TestHistoryView_RenderContainsTableHeaders`
  - **状态:** RED — renderHistoryView 不存在
  - **验证:** 包含 PID, AGENT, MODEL, TOKENS 列标题

- **29.5-UNIT-012** [P1] `TestHistoryView_RenderContainsBottomStats`
  - **状态:** RED — renderHistoryView 不存在
  - **验证:** 底部统计 Running/Done/Failed 计数

- **29.5-UNIT-026** [P0] `TestHistoryView_RenderUsesStateSymbol`
  - **状态:** RED — dashboard_history.go 不存在
  - **验证:** 使用 ui.StateSymbol 渲染进程状态

- **29.5-UNIT-027** [P0] `TestHistoryView_RenderFuncSignature`
  - **状态:** RED — dashboard_history.go 不存在
  - **验证:** renderHistoryView 是 dashboardModel 方法

- **29.5-UNIT-032** [P1] `TestHistoryView_RenderContainsTitleBar`
  - **状态:** RED — renderHistoryView 不存在
  - **验证:** 包含 History 标题区域

#### AC-3: 光标导航

- **29.5-UNIT-013** [P0] `TestHistoryView_HistoryKeyHandlesJKNavigation`
  - **状态:** RED — historyKey 不存在
  - **验证:** j/k 导航修改 historyCursor

#### AC-4: Enter 聚焦

- **29.5-UNIT-014** [P0] `TestHistoryView_HistoryKeyHandlesEnterFocus`
  - **状态:** RED — historyKey 不存在
  - **验证:** 设置 selectedPID/selectedUUID，回到 viewDefault

- **29.5-UNIT-035** [P0] `TestHistoryView_EnterFocusCallsHandlePIDChange`
  - **状态:** RED — historyKey 不存在
  - **验证:** Enter 后调用 handlePIDChange

#### AC-5: L 键跳转

- **29.5-UNIT-015** [P0] `TestHistoryView_HistoryKeyHandlesLKey`
  - **状态:** RED — historyKey 不存在
  - **验证:** 处理 L 键跳转 LLM 查看器

#### AC-6: 搜索过滤

- **29.5-UNIT-016** [P0] `TestHistoryView_HistoryKeyHandlesSearchMode`
  - **状态:** RED — historyKey 不存在
  - **验证:** / 进入搜索模式，引用 historySearchMode/historySearchQuery

- **29.5-UNIT-017** [P1] `TestHistoryView_SearchFiltersByAgentName`
  - **状态:** RED — dashboard_history.go 不存在
  - **验证:** 使用 ToLower + Agent 字段过滤

- **29.5-UNIT-031** [P1] `TestHistoryView_SearchBackspaceRuneSafe`
  - **状态:** RED — dashboard_history.go 不存在
  - **验证:** Backspace rune-safe 截断

#### AC-7: 排序模式

- **29.5-UNIT-018** [P0] `TestHistoryView_HistoryKeyHandlesSortModes`
  - **状态:** RED — historyKey 不存在
  - **验证:** 修改 historySortMode

- **29.5-UNIT-019** [P1] `TestHistoryView_SortUsesSlicesSortFunc`
  - **状态:** RED — dashboard_history.go 不存在
  - **验证:** 使用 slices.SortFunc 或 sort.Slice

- **29.5-UNIT-034** [P1] `TestHistoryView_ThreeSortModesImplemented`
  - **状态:** RED — dashboard_history.go 不存在
  - **验证:** 三种排序模式（CreatedAt/Agent/PID）

#### AC-8: Esc 退出

- **29.5-UNIT-020** [P0] `TestHistoryView_HistoryKeyHandlesEscExit`
  - **状态:** RED — historyKey 不存在
  - **验证:** Esc 回到 viewDefault

#### AC-9: Tree 面板已结束进程

- **29.5-UNIT-021** [P0] `TestHistoryView_TreePaneUsesListAllProcs`
  - **状态:** RED — 无 ListAllProcs 调用
  - **验证:** dashboard.go 或 dashboard_tree.go 使用 ListAllProcs

- **29.5-UNIT-022** [P0] `TestHistoryView_TreePaneUsesStateSymbol`
  - **状态:** RED — dashboard_tree.go 无 StateSymbol
  - **验证:** 使用 ui.StateSymbol() 渲染 ✓/✕

- **29.5-UNIT-023** [P1] `TestHistoryView_TreePaneShowsExitCodeAndElapsed`
  - **状态:** RED — renderDashboardTreePane 无 DeadAt 引用
  - **验证:** 已结束进程显示 exit code 和存活时间

#### AC-10: Timeline 自动过滤

- **29.5-UNIT-024** [P0] `TestHistoryView_TimelineAutoFilterDeadProcess`
  - **状态:** RED — 无 filterTimeline 逻辑
  - **验证:** Dead 进程 Timeline PID 过滤

- **29.5-UNIT-025** [P1] `TestHistoryView_StatusBarShowsFilteredHint`
  - **状态:** RED — 无 "filtered: PID" 提示文本
  - **验证:** Status bar 显示 "(filtered: PID N)"

#### 通用结构验证

- **29.5-UNIT-028** [P0] `TestHistoryView_HistoryKeyFuncSignature`
  - **状态:** RED — dashboard_history.go 不存在
  - **验证:** historyKey 是 dashboardModel 方法

- **29.5-UNIT-029** [P0] `TestHistoryView_EnterHistoryViewSignature`
  - **状态:** RED — dashboard_history.go 不存在
  - **验证:** enterHistoryView 方法签名正确

- **29.5-UNIT-030** [P0] `TestHistoryView_PackageMain`
  - **状态:** RED — dashboard_history.go 不存在
  - **验证:** package main 声明

- **29.5-UNIT-033** [P0] `TestHistoryView_HistoryProcsMsgHasProcsField`
  - **状态:** RED — historyProcsMsg 不存在
  - **验证:** historyProcsMsg 有 procs 字段

- **29.5-UNIT-036** [P0] `TestHistoryView_ImportsVFS`
  - **状态:** RED — dashboard_history.go 不存在
  - **验证:** 导入 vfs 或 bubbletea 包

---

## Implementation Checklist

### Test: TestHistoryView_FileExists (+ 001~036)

**文件:** `cmd/rnix/atdd_29_5_dashboard_history_view_test.go`

**让所有测试通过需要完成的任务:**

- [ ] Task 1: 新增 `cmd/rnix/dashboard_history.go`
  - [ ] 1.1 实现 `enterHistoryView()` — 设置 viewMode=viewHistory，重置 cursor/scroll，返回 fetchAllProcsCmd
  - [ ] 1.2 实现 `fetchAllProcsCmd()` — 调用 client.ListAllProcs()，返回 historyProcsMsg
  - [ ] 1.3 实现 `historyKey(msg)` — j/k 导航、Enter 聚焦、L 跳转、/ 搜索、1/2/3 排序、Esc 退出
  - [ ] 1.4 实现 `renderHistoryView()` — 全屏表格（Title + 进程表 + 底部统计 + Status bar）
  - [ ] 1.5 实现搜索过滤逻辑（ToLower + Agent 匹配 + rune-safe Backspace）
  - [ ] 1.6 实现排序逻辑（slices.SortFunc，0=时间/1=名称/2=PID）
  - [ ] 运行测试: `go test -race -run "TestHistoryView_FileExists|TestHistoryView_ExpectedFunctions|TestHistoryView_Render|TestHistoryView_HistoryKey|TestHistoryView_Search|TestHistoryView_Sort|TestHistoryView_Package|TestHistoryView_Imports" ./cmd/rnix/...`

- [ ] Task 2: 修改 `cmd/rnix/dashboard_types.go`
  - [ ] 2.1 新增 `historyProcsMsg` 消息类型（含 procs 字段）
  - [ ] 运行测试: `go test -race -run "TestHistoryView_HistoryProcsMsgTypeExists|TestHistoryView_HistoryProcsMsgHasProcsField" ./cmd/rnix/...`

- [ ] Task 3: 修改 `cmd/rnix/dashboard.go`
  - [ ] 3.1 dashboardModel 新增 6 个历史视图字段
  - [ ] 3.2 Update 方法新增 historyProcsMsg case
  - [ ] 3.3 renderDashboard 新增 viewHistory case → renderHistoryView
  - [ ] 运行测试: `go test -race -run "TestHistoryView_DashboardModel|TestHistoryView_Update|TestHistoryView_ViewRoutes" ./cmd/rnix/...`

- [ ] Task 4: 修改 `cmd/rnix/dashboard_nav.go`
  - [ ] 4.1 Layer 1.5 添加 viewHistory 覆盖层检查 → historyKey
  - [ ] 4.2 H 键从 placeholder 改为 enterHistoryView
  - [ ] 运行测试: `go test -race -run "TestHistoryView_HKey|TestHistoryView_OverlayLayer" ./cmd/rnix/...`

- [ ] Task 5: 修改 `cmd/rnix/dashboard_tree.go`
  - [ ] 5.1 使用 ListAllProcs 数据或从 m.processes 中包含历史进程
  - [ ] 5.2 已结束进程使用 ui.StateSymbol() 显示 ✓/✕
  - [ ] 5.3 已结束进程显示 exit code 和存活时间（DeadAt - CreatedAt）
  - [ ] 运行测试: `go test -race -run "TestHistoryView_TreePane" ./cmd/rnix/...`

- [ ] Task 6: 修改 `cmd/rnix/dashboard_timeline.go`
  - [ ] 6.1 选中 Dead 进程时 Timeline 过滤只显示该 PID 事件
  - [ ] 6.2 Status bar 追加 "(filtered: PID N)" 提示
  - [ ] 运行测试: `go test -race -run "TestHistoryView_Timeline|TestHistoryView_StatusBar" ./cmd/rnix/...`

- [ ] Task 7: `make all` 验证
  - [ ] 7.1 运行 `make all` 确认编译通过、所有测试通过

---

## Running Tests

```bash
# 运行所有 Story 29.5 ATDD 测试
go test -race -run "TestHistoryView_" ./cmd/rnix/...

# 运行特定测试
go test -race -run "TestHistoryView_FileExists" ./cmd/rnix/...

# 运行所有 cmd/rnix 测试（包含回归）
go test -race ./cmd/rnix/...

# 完整验证
make all
```

---

## TDD Red Phase Verification

```
$ go test -race -run "TestHistoryView_" ./cmd/rnix/...

--- FAIL: TestHistoryView_FileExists
--- FAIL: TestHistoryView_ExpectedFunctionsExist
--- FAIL: TestHistoryView_HistoryProcsMsgTypeExists
--- FAIL: TestHistoryView_DashboardModelHistoryFields (×6 fields)
--- FAIL: TestHistoryView_EnterHistoryViewSetsViewMode
--- FAIL: TestHistoryView_FetchAllProcsCmdCallsListAllProcs
--- FAIL: TestHistoryView_HKeyCallsEnterHistoryView (×2)
--- FAIL: TestHistoryView_OverlayLayerInNav (×2)
--- FAIL: TestHistoryView_UpdateHandlesHistoryProcsMsg
--- FAIL: TestHistoryView_ViewRoutesViewHistory (×2)
--- FAIL: TestHistoryView_RenderContainsTableHeaders
--- FAIL: TestHistoryView_RenderContainsBottomStats
--- FAIL: TestHistoryView_HistoryKeyHandlesJKNavigation
--- FAIL: TestHistoryView_HistoryKeyHandlesEnterFocus
--- FAIL: TestHistoryView_HistoryKeyHandlesLKey
--- FAIL: TestHistoryView_HistoryKeyHandlesSearchMode
--- FAIL: TestHistoryView_SearchFiltersByAgentName
--- FAIL: TestHistoryView_HistoryKeyHandlesSortModes
--- FAIL: TestHistoryView_SortUsesSlicesSortFunc
--- FAIL: TestHistoryView_HistoryKeyHandlesEscExit
--- FAIL: TestHistoryView_TreePaneUsesListAllProcs
--- FAIL: TestHistoryView_TreePaneUsesStateSymbol
--- FAIL: TestHistoryView_TreePaneShowsExitCodeAndElapsed
--- FAIL: TestHistoryView_TimelineAutoFilterDeadProcess
--- FAIL: TestHistoryView_StatusBarShowsFilteredHint
--- FAIL: TestHistoryView_RenderUsesStateSymbol
--- FAIL: TestHistoryView_RenderFuncSignature
--- FAIL: TestHistoryView_HistoryKeyFuncSignature
--- FAIL: TestHistoryView_EnterHistoryViewSignature
--- FAIL: TestHistoryView_PackageMain
--- FAIL: TestHistoryView_SearchBackspaceRuneSafe
--- FAIL: TestHistoryView_RenderContainsTitleBar
--- FAIL: TestHistoryView_HistoryProcsMsgHasProcsField
--- FAIL: TestHistoryView_ThreeSortModesImplemented
--- FAIL: TestHistoryView_EnterFocusCallsHandlePIDChange
--- FAIL: TestHistoryView_ImportsVFS

FAIL    github.com/rnixai/rnix/cmd/rnix
```

**Summary:**
- 总测试数: 36
- 通过: 0 (预期)
- 失败: 36 (预期)
- 状态: RED phase 验证通过

**失败原因:**
- dashboard_history.go 尚不存在（大部分测试）
- dashboard_types.go 缺少 historyProcsMsg 类型
- dashboardModel 缺少历史视图字段
- dashboard_nav.go H 键仍为 placeholder
- dashboard_tree.go 无 StateSymbol/ListAllProcs/DeadAt
- dashboard_timeline.go 无 filterTimeline 逻辑

---

## Red-Green-Refactor Workflow

### RED Phase (Complete)

- 36 个测试已编写并全部失败
- 失败原因均为缺少实现，非测试 bug
- 现有测试（29.1/29.2/29.3/29.4）无回归

### GREEN Phase (DEV Team)

1. 按 Task 顺序依次实现（Task 1 → Task 2 → ... → Task 7）
2. 每完成一个 Task，运行对应测试验证通过
3. 最终运行 `make all` 确认全面通过

### REFACTOR Phase

1. 确认所有 36 个测试通过
2. 审查代码质量（命名一致性、重复代码提取）
3. 运行 `make all` 确认无回归

---

## Knowledge Base References Applied

- **test-quality.md** — 测试隔离性、确定性、原子性原则
- **component-tdd.md** — 组件 TDD 策略（AST 结构验证模式）
- **data-factories.md** — 测试数据模式（本 story 无需工厂，使用源码分析）
- **test-levels-framework.md** — 测试级别选择（backend → Unit/Integration）

---

## Notes

- 本 Story 所有修改限于 `cmd/rnix/dashboard*.go` 文件，不涉及 kernel/ipc 包
- `ListAllProcs` 客户端方法已在 Story 29.4 实现，直接复用
- `StateSymbol` 函数已在 Story 29.4 实现，直接复用
- 搜索模式 Backspace 必须使用 rune-safe 截断（Story 29.3 Code Review P-1 学习）
- 历史视图覆盖层在 Layer 1.5（Prompt Pager 之后、Kill 确认之前）

---

**Generated by BMad TEA Agent** — 2026-03-23
