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
  - _bmad-output/implementation-artifacts/17-2-tracing-timeline-pane.md
  - _bmad-output/implementation-artifacts/17-1-dashboard-framework-and-agent-tree-pane.md
  - cmd/rnix/dashboard.go
  - cmd/rnix/dashboard_test.go
  - ipc/client.go
  - ipc/protocol.go
  - internal/ui/styles.go
  - debug/strace.go
---

# ATDD 检查清单 - Epic 17, Story 17-2: 追踪时间线窗格

**日期：** 2026-03-09
**作者：** Decker
**主要测试级别：** Unit (Go `testing` 包)

---

## 故事摘要

在 dashboard 的时间线窗格中实现 syscall 事件的水平时间轴展示，支持按类别着色（LLM=蓝/Tool=绿/IPC=紫/VFS=黄/Error=红）、缩放/滚动和类别过滤。

**As a** 平台构建者
**I want** 在时间线窗格中以时间轴展示智能体的 syscall 事件流
**So that** 我可以直观地看到智能体的执行时序和关键事件

---

## 验收标准

1. **AC1**: 选中智能体节点 → 时间线窗格水平时间轴展示 syscall 事件流，按类别着色（LLM=蓝/Tool=绿/IPC=紫/VFS=黄/Error=红）
2. **AC2**: 缩放或滚动 → 时间范围平滑调整，支持按类别过滤（LLM/Tool/IPC/VFS 独立显示/隐藏）

---

## 失败测试创建（RED 阶段）

### Unit Tests (15 tests)

**文件：** `cmd/rnix/dashboard_test.go` (追加到现有文件)

| # | 测试 ID | 测试名称 | 优先级 | AC | 状态 | 失败原因 |
|---|---------|----------|--------|-----|------|----------|
| 1 | 17.2-UNIT-001 | TestClassifySyscall_LLM | P0 | AC1 | RED | classifySyscall 函数不存在 |
| 2 | 17.2-UNIT-002 | TestClassifySyscall_IPC | P0 | AC1 | RED | classifySyscall 函数不存在 |
| 3 | 17.2-UNIT-003 | TestClassifySyscall_Tool | P0 | AC1 | RED | classifySyscall 函数不存在 |
| 4 | 17.2-UNIT-004 | TestClassifySyscall_VFS | P0 | AC1 | RED | classifySyscall 函数不存在 |
| 5 | 17.2-UNIT-005 | TestClassifySyscall_ErrorPriority | P0 | AC1 | RED | classifySyscall 函数不存在 |
| 6 | 17.2-UNIT-006 | TestCategoryColor | P1 | AC1 | RED | categoryColor 函数不存在 |
| 7 | 17.2-UNIT-007 | TestDashboardModel_TimelineEventAppend | P0 | AC1 | RED | timelineEvents 字段不存在 |
| 8 | 17.2-UNIT-008 | TestDashboardModel_TimelineEventsFIFO | P0 | AC1 | RED | FIFO 淘汰未实现 |
| 9 | 17.2-UNIT-009 | TestDashboardModel_TimelineRenderEmpty | P0 | AC1 | RED | renderTimelinePane 不存在 |
| 10 | 17.2-UNIT-010 | TestDashboardModel_TimelineRenderEvents | P0 | AC1 | RED | renderTimelinePane 不存在 |
| 11 | 17.2-UNIT-011 | TestDashboardModel_TimelineZoom | P0 | AC2 | RED | zoomLevel 字段不存在 |
| 12 | 17.2-UNIT-012 | TestDashboardModel_TimelineFilter | P0 | AC2 | RED | timelineFilters 字段不存在 |
| 13 | 17.2-UNIT-013 | TestDashboardModel_TimelineScroll | P0 | AC2 | RED | viewStart 字段不存在 |
| 14 | 17.2-UNIT-014 | TestDashboardModel_TimelinePIDChange | P0 | AC1 | RED | PID 变化时不清空事件 |
| 15 | 17.2-UNIT-015 | TestDashboardModel_TabToTimeline | P1 | AC1 | RED | 已通过（17-1 已实现 Tab） |

**总计：** 15 个测试，1 个 GREEN（基础设施），14 个 RED（待实现）

### API Tests

不适用（Go 后端项目，所有交互通过 IPC 内部调用，无 HTTP API）

### Component Tests

不适用（Go CLI TUI 项目，无 UI 组件框架）

---

## 数据工厂

### mockTimelineEvents 工厂

**文件：** `cmd/rnix/dashboard_test.go`

**导出：**

- `mockTimelineEvents()` - 创建 5 个测试 syscall 事件（覆盖 LLM/Tool/IPC/VFS/Error 五种类别）
- `newTestTimelineDashboardModel()` - 创建预填充 timeline 事件的 dashboardModel

**示例用法：**

```go
events := mockTimelineEvents()
m := newTestTimelineDashboardModel()
v := m.View()
```

---

## Fixtures

### Go Test Helpers

**文件：** `cmd/rnix/dashboard_test.go`

**Helpers：**

- `mockTimelineEvents()` - 提供 5 个标准测试事件
  - **Setup：** 创建覆盖 5 种类别的 SyscallEventWire 事件
  - **提供：** `[]ipc.SyscallEventWire` 可直接用于 classifySyscall 和 model 测试
  - **清理：** 无需（纯值类型）

- `newTestTimelineDashboardModel()` - 构建含 timeline 事件的 dashboard model
  - **Setup：** 基于 `newTestDashboardModel` + 填充 timelineEvents
  - **提供：** 可直接调用 Update/View 的 dashboardModel
  - **清理：** 无需

---

## Mock 需求

### IPC Client Mock (AttachDebug)

**方法：** `ipc.Client.AttachDebug(pid, onEvent)`

**策略：** 不创建 mock 接口。测试通过以下方式绕过 IPC：

1. 纯函数测试（`classifySyscall`、`categoryColor`）直接传入 `ipc.SyscallEventWire`
2. Model 测试通过直接设置 `timelineEvents` 字段
3. 事件追加测试通过构造 `timelineEventMsg` 消息传入 `Update()`
4. 渲染测试通过 `newTestTimelineDashboardModel` 预填充数据

---

## 必要 data-testid 属性

不适用（Go TUI 项目，使用 bubbletea 终端渲染，无 HTML DOM）

---

## 实现检查清单

### Test: TestClassifySyscall_* (17.2-UNIT-001~005)

**文件：** `cmd/rnix/dashboard_test.go`

**使此测试通过的任务：**

- [ ] 定义 `eventCategory` 类型和常量（catLLM/catTool/catIPC/catVFS/catError）
- [ ] 实现 `classifySyscall(ev ipc.SyscallEventWire) eventCategory` 纯函数
- [ ] 实现 `isLLMEvent(ev)` 检查 args 中 path/tool 包含 "/dev/llm/"
- [ ] 实现 `isToolPathEvent(ev)` 检查 args 中 path 包含 "/dev/shell/" 或 "/dev/fs/"
- [ ] Error 优先：ev.Error != "" 时返回 catError
- [ ] 运行测试：`go test -run "TestClassifySyscall" ./cmd/rnix/`
- [ ] ✅ 测试通过（green phase）

**预估工时：** 0.5 小时

---

### Test: TestCategoryColor (17.2-UNIT-006)

**文件：** `cmd/rnix/dashboard_test.go`

**使此测试通过的任务：**

- [ ] 实现 `categoryColor(cat eventCategory) string` 返回 lipgloss 颜色值
- [ ] LLM → ColorAgent, Tool → ColorSuccess, IPC → "#9B59B6", VFS → ColorWarning, Error → ColorError
- [ ] 运行测试：`go test -run TestCategoryColor ./cmd/rnix/`
- [ ] ✅ 测试通过（green phase）

**预估工时：** 0.25 小时

---

### Test: TestDashboardModel_TimelineEventAppend + FIFO (17.2-UNIT-007, 008)

**文件：** `cmd/rnix/dashboard_test.go`

**使此测试通过的任务：**

- [ ] 在 `dashboardModel` 新增 `timelineEvents []timelineEvent` 字段
- [ ] 定义 `timelineEvent` 结构体（SyscallEventWire + category）
- [ ] 定义 `timelineEventMsg` 消息类型
- [ ] 在 `Update` 中处理 `timelineEventMsg`：classifySyscall → 追加到 timelineEvents
- [ ] 实现 FIFO 淘汰：超过 1000 条时移除最早事件
- [ ] 运行测试：`go test -run "TestDashboardModel_TimelineEvent" ./cmd/rnix/`
- [ ] ✅ 测试通过（green phase）

**预估工时：** 0.5 小时

---

### Test: TestDashboardModel_TimelineRenderEmpty + Events (17.2-UNIT-009, 010)

**文件：** `cmd/rnix/dashboard_test.go`

**使此测试通过的任务：**

- [ ] 替换 `renderDashboardPlaceholder("Timeline", ...)` 为 `m.renderTimelinePane(width, height)`
- [ ] 实现 `renderTimelinePane`：窗格边框 + 标题 + 时间轴渲染
- [ ] 空状态：无事件时显示 "Select an agent" 或 "Waiting for events..."
- [ ] 有事件时渲染时间轴条和事件列表
- [ ] 运行测试：`go test -run "TestDashboardModel_TimelineRender" ./cmd/rnix/`
- [ ] ✅ 测试通过（green phase）

**预估工时：** 2 小时

---

### Test: TestDashboardModel_TimelineZoom (17.2-UNIT-011)

**文件：** `cmd/rnix/dashboard_test.go`

**使此测试通过的任务：**

- [ ] 在 `dashboardModel` 新增 `timelineZoomLevel int` 字段
- [ ] 在 `dashboardKey` 中 paneTimeline 分支处理 `+`/`-` 键
- [ ] `+` 增加 zoomLevel（上限 5），`-` 减少 zoomLevel（下限 0）
- [ ] 运行测试：`go test -run TestDashboardModel_TimelineZoom ./cmd/rnix/`
- [ ] ✅ 测试通过（green phase）

**预估工时：** 0.5 小时

---

### Test: TestDashboardModel_TimelineFilter (17.2-UNIT-012)

**文件：** `cmd/rnix/dashboard_test.go`

**使此测试通过的任务：**

- [ ] 在 `dashboardModel` 新增 `timelineFilters map[eventCategory]bool` 字段
- [ ] 初始化所有类别为 true（全部显示）
- [ ] 在 paneTimeline 分支处理 `1`/`2`/`3`/`4` 键切换对应类别
- [ ] 渲染时过滤：只显示 `timelineFilters[cat] == true` 的事件
- [ ] 运行测试：`go test -run TestDashboardModel_TimelineFilter ./cmd/rnix/`
- [ ] ✅ 测试通过（green phase）

**预估工时：** 0.5 小时

---

### Test: TestDashboardModel_TimelineScroll (17.2-UNIT-013)

**文件：** `cmd/rnix/dashboard_test.go`

**使此测试通过的任务：**

- [ ] 在 `dashboardModel` 新增 `timelineViewStart int64` 字段（视口起始时间 ms）
- [ ] 在 paneTimeline 分支处理 `h`/`l` 或 ←/→ 键调整 viewStart
- [ ] 运行测试：`go test -run TestDashboardModel_TimelineScroll ./cmd/rnix/`
- [ ] ✅ 测试通过（green phase）

**预估工时：** 0.5 小时

---

### Test: TestDashboardModel_TimelinePIDChange (17.2-UNIT-014)

**文件：** `cmd/rnix/dashboard_test.go`

**使此测试通过的任务：**

- [ ] 在 `dashboardTick` 中检测 `selectedPID` 变化
- [ ] PID 变化时清空 `timelineEvents`，重置 zoomLevel/viewStart
- [ ] 如果有旧的 timelineClient，关闭并取消订阅
- [ ] 为新 PID 创建新的 IPC 连接和 AttachDebug 订阅
- [ ] 运行测试：`go test -run TestDashboardModel_TimelinePIDChange ./cmd/rnix/`
- [ ] ✅ 测试通过（green phase）

**预估工时：** 1 小时

---

### Test: TestDashboardModel_TabToTimeline (17.2-UNIT-015)

**文件：** `cmd/rnix/dashboard_test.go`

**使此测试通过的任务：**

- [ ] 此测试验证 Tab 切换到 paneTimeline（17-1 已实现），预期 GREEN
- [ ] 运行测试：`go test -run TestDashboardModel_TabToTimeline ./cmd/rnix/`
- [ ] ✅ 测试通过（green phase）

**预估工时：** 0 小时（已实现）

---

## 运行测试

```bash
# 运行本 story 所有失败测试
go test -v -run "TestClassifySyscall|TestCategoryColor|TestDashboardModel_Timeline|TestDashboardModel_TabToTimeline" ./cmd/rnix/

# 运行分类测试
go test -v -run "TestClassifySyscall" ./cmd/rnix/

# 运行渲染测试
go test -v -run "TestDashboardModel_TimelineRender" ./cmd/rnix/

# 运行交互测试（缩放/滚动/过滤）
go test -v -run "TestDashboardModel_TimelineZoom|TestDashboardModel_TimelineFilter|TestDashboardModel_TimelineScroll" ./cmd/rnix/

# 运行全部 dashboard 测试（含 17-1）
go test -v -run "TestDashboard|TestHelp_ContainsDashboardSubcommand|TestRunDashboard" ./cmd/rnix/

# 运行所有 cmd/rnix 测试
go test -v ./cmd/rnix/
```

---

## Red-Green-Refactor 工作流

### RED 阶段（完成）✅

**TEA Agent 职责：**

- ✅ 15 个测试已编写，14 个失败中
- ✅ 测试工厂和辅助函数创建（mockTimelineEvents, newTestTimelineDashboardModel）
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

1. `classifySyscall` + `categoryColor` — 纯函数，最简单起步
2. `timelineEvent` 结构体 + `timelineEventMsg` 处理 — 数据模型
3. `renderTimelinePane` — 替换占位渲染
4. 缩放/滚动/过滤键盘处理 — 交互逻辑
5. PID 变化检测和 AttachDebug 订阅 — 最复杂

---

### REFACTOR 阶段（DEV 团队 - 所有测试通过后）

1. 验证所有 15 个测试 + 17-1 的 23 个测试全部通过
2. 检查代码质量（可读性、一致性）
3. 与 17-1 代码风格对齐
4. 确保 lipgloss 样式使用一致
5. 确保测试仍然通过

---

## 下一步

1. **运行失败测试**确认 RED 阶段：`go test -v -run "TestClassifySyscall|TestCategoryColor|TestDashboardModel_Timeline" ./cmd/rnix/`
2. **按实现检查清单顺序开始实现** `cmd/rnix/dashboard.go`
3. **每次实现一个测试**（red → green）
4. **所有测试通过后**重构代码质量
5. **重构完成后**在 sprint-status.yaml 中更新故事状态

---

## 知识库参考

- **test-quality.md** — 测试质量定义：确定性、隔离性、显式断言
- **test-levels-framework.md** — 测试级别选择：Unit 用于纯函数和逻辑
- **debug/strace.go:176-188** — `isLLMSyscall` 分类参考实现
- **cmd/rnix/dashboard_test.go** — 17-1 测试模式参考

---

## 测试执行证据

### 初始测试运行（RED 阶段验证）

**命令：** `go test -v -run "TestClassifySyscall|TestCategoryColor|TestDashboardModel_Timeline" ./cmd/rnix/`

**结果：** 见下方运行输出

---

## 备注

- 所有测试为 Go 标准 `testing` 包单元测试
- `classifySyscall` 和 `categoryColor` 是纯函数，可独立测试
- `timelineEventMsg` 通过 bubbletea `Update()` 处理，与 17-1 的 `tickMsg` 模式一致
- `AttachDebug` 订阅在独立 IPC 连接上运行，测试中不实际创建连接
- 过滤只影响渲染，不影响事件收集（所有事件始终存储）

---

**Generated by BMad TEA Agent** - 2026-03-09
