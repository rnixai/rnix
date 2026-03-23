# Epic 29: Dashboard UX 重设计（Dashboard UX Redesign）

Dashboard 从 4000 行单文件重构为模块化架构，引入视图模式状态机（数字键 1-8 直达 + Esc 回退 + Shift-Tab 反向）、进程聚焦卡片（2×3 信息摘要）、进程历史保留（Dead 进程可追溯）和全屏 LLM 对话查看器（完整 request/response 审查）。消除 Tab 循环痛点，实现信息分层，支持事后分析。

> **设计基础**
>
> | 文档 | 说明 |
> |------|------|
> | [Design Thinking](../design-thinking-2026-03-23.md) | 用户旅程、共情分析、HMW 问题、方案原型 |
> | [Dashboard Redesign Spec](../dashboard-redesign-spec-2026-03-23.md) | 代码级实现规格（导航、布局、组件） |
> | [Sprint Change Proposal](../sprint-change-proposal-2026-03-23.md) | 变更评估与批准记录 |
> | [PRD FR90-FR96](../prd/functional-requirements.md) | Visual Debugging Dashboard 功能需求 |
>
> **触发事件：** Design Thinking 会议（2026-03-23）系统化分析 Dashboard UX，发现 4 大痛点：Tab 循环地狱、进程历史丢失、信息过载、LLM 交互不可见。
> **前提：** Epic 17（Dashboard 基础框架 ~1800 行）+ Epic 27（观察系统面板扩展至 ~4000 行）+ Epic 28（UUID 标识体系）均已完成。

**FRs covered:** FR90-FR96（扩展）
**Dependencies:** Epic 17（Dashboard 基础框架）、Epic 27（观察系统面板）、Epic 28（UUID 标识体系）

---

## Story 依赖关系

```
Story 29.1 (文件拆分)
  └→ Story 29.2 (导航重构)
       ├→ Story 29.3 (Focus Card)
       ├→ Story 29.5 (历史视图) ← Story 29.4 (Kernel 历史保留)
       └→ Story 29.6 (LLM 查看器)
```

---

## Story 29.1: Dashboard 文件拆分（纯重构）

As a 平台构建者,
I want dashboard.go (~4000 行) 拆分为独立的模块化文件,
So that 代码可维护性提升，为后续 UX 改进建立基础。

**Phase:** 1 — 导航重构 + 文件拆分
**Priority:** P0（所有后续 Story 的前置条件）

**Acceptance Criteria:**

**Given** `cmd/rnix/dashboard.go` (~4000 行单文件)
**When** 执行文件拆分
**Then** 拆分为以下文件结构：
- `dashboard.go` — 主模型、Init/Update/View、布局编排（~500 行）
- `dashboard_tree.go` — Tree 面板渲染
- `dashboard_timeline.go` — Timeline 面板渲染 + Step 导航
- `dashboard_heatmap.go` — Heatmap 面板渲染
- `dashboard_detail.go` — Detail 面板渲染
- `dashboard_intent.go` — Intent DAG 面板渲染
- `dashboard_security.go` — Security 面板渲染
- `dashboard_trace.go` — Trace 面板渲染
- `dashboard_eval.go` — Eval 面板渲染
- `dashboard_types.go` — 所有 dashboard 相关的类型定义

**Given** 拆分完成后
**When** 运行 `make all`
**Then** 编译通过、所有测试通过
**And** Dashboard 行为与拆分前完全一致（零行为变更）

**Technical Notes:**
- 纯重构，不改任何逻辑
- 每个面板文件包含：渲染函数 + 面板内按键处理 + 面板相关的辅助函数
- types 文件包含：paneType 枚举、dashboardModel 中面板特定字段的类型定义

---

## Story 29.2: 视图模式系统与导航重构

As a 平台构建者,
I want 通过数字键 1-8 直达面板、Esc 回退默认视图、Shift-Tab 反向导航,
So that 我可以零等待直达目标面板，消除 Tab 循环痛点。

**Phase:** 1 — 导航重构 + 文件拆分
**Priority:** P0
**Dependencies:** Story 29.1

**Acceptance Criteria:**

**Given** dashboardModel
**When** 引入视图模式系统
**Then** 新增 `viewMode` 枚举：viewDefault（默认）、viewExpanded（面板展开）、viewLLM（LLM 查看器）、viewHistory（历史列表）
**And** 新增 `expandedPane paneType` 字段

**Given** 用户在任何主视图中
**When** 按数字键 1-8
**Then** 切换到对应面板的展开视图
**And** 再按同一数字键回到默认视图（toggle 行为）

**Given** 用户在展开视图中
**When** 按 Esc
**Then** 回到默认视图

**Given** 用户在任何主视图中
**When** 按 Shift-Tab
**Then** 反向切换面板（与 Tab 方向相反）

**Given** Title bar
**When** 处于展开视图
**Then** 激活的面板标签显示为 `N:Name*`（无方括号 + 星号）
**And** 其他面板标签显示为 `[N]Name`

**Given** Status bar
**When** 视图模式变化
**Then** 右侧快捷键提示根据当前视图动态更新

**Given** Tab 键行为
**When** 在默认视图按 Tab
**Then** 切换到展开视图（viewExpanded）并展开下一面板
**And** 保留与现有行为的向后兼容

**Technical Notes:**
- 新增文件：`dashboard_nav.go`（统一键绑定分发表）
- 修改文件：`dashboard.go`（viewMode 字段、布局引擎适配）、`dashboard_types.go`（viewMode 枚举）
- 参考实现：`dashboard-redesign-spec-2026-03-23.md` Section 4（导航系统规格）

---

## Story 29.3: 默认视图 Focus Card

As a 平台构建者,
I want 默认视图右下区域显示选中进程的 2×3 信息摘要卡片,
So that 我无需切换面板即可一览进程的关键指标。

**Phase:** 2 — 默认视图 Focus Card
**Priority:** P1
**Dependencies:** Story 29.2

**Acceptance Criteria:**

**Given** 默认视图（viewDefault）
**When** 用户选中一个进程
**Then** 右下区域显示 2×3 网格的 Focus Card：
- Row 0：Tokens（消耗/预算/速率）、Context（Sys/User/Asst/Tool 占比）、Status（状态/技能/设备/PPID）
- Row 1：Intent（子任务摘要）、Trace（span 数/平均延迟/错误）、Alerts（异常告警）

**Given** 选中的进程为 Running 状态
**When** Focus Card 渲染
**Then** 显示实时递增的 elapsed、tokens、steps
**And** 数据每 500ms tick 刷新

**Given** 选中的进程为 Dead 状态（✓ 或 ✕）
**When** Focus Card 渲染
**Then** 标题显示 `✓ Done (exit 0)` 或 `✕ Failed (exit N)`
**And** 顶部显示 `Historical snapshot — lived HH:MM:SS–HH:MM:SS (Xs)`
**And** steps 显示 `N (final)`，Alerts 卡片替换为 Result 卡片

**Given** 默认视图布局
**When** Focus Card 替代原来的右下面板
**Then** 布局为：左 Tree (40%w) + 右上 Timeline (h/2) + 右下 Focus Card (h/2)

**Given** Focus Card 各卡片
**When** 使用 lipgloss 渲染
**Then** 每个卡片有边框、标题居左、内容区 cardH-3 行
**And** 宽度 = rightWidth/3，高度 = focusHeight/2

**Technical Notes:**
- 新增文件：`dashboard_focus.go`（focusCardState + 渲染 + 数据聚合）
- 修改文件：`dashboard.go`（默认视图布局引擎）
- 参考实现：`dashboard-redesign-spec-2026-03-23.md` Section 3.2.3（focusCardState）+ Section 5.5（2×3 网格布局）

---

## Story 29.4: Kernel 进程历史保留与 IPC

As a 平台构建者,
I want 系统保留已结束进程的信息快照,
So that Dead 进程被 reaper 清除后仍可在 Dashboard 中追溯。

**Phase:** 3 — 进程历史保留
**Priority:** P1
**Dependencies:** 无（可与 29.2/29.3 并行）

**Acceptance Criteria:**

**Given** `kernel/kernel.go`
**When** 新增 ProcessHistory 组件
**Then** ProcessHistory 使用 `sync.RWMutex` 保护的 `[]vfs.ProcInfo` 切片
**And** 最大保留 1000 条，FIFO 淘汰

**Given** reaper 准备清理一个 Dead 进程
**When** 清理前
**Then** 先调用 `history.Add(procInfo)` 保存快照
**And** 快照包含 PID、UUID、Agent、Model、State、CreatedAt、ExitCode 等完整信息

**Given** Kernel
**When** 新增 `ListAllProcs()` 方法
**Then** 返回存活进程 + 历史进程的合并列表
**And** 按 CreatedAt 排序

**Given** IPC 协议
**When** 新增 `list_all_procs` 方法
**Then** Server handler 调用 `kernel.ListAllProcs()` 返回完整列表
**And** Client 新增 `ListAllProcs() ([]vfs.ProcInfo, error)` 方法

**Given** 并发场景
**When** reaper 写入 history 同时 Dashboard 读取
**Then** RWMutex 保证数据一致性

**Given** 状态符号体系
**When** 新增统一的进程状态符号
**Then** `●` Running, `○` Created, `✓` Done (exit 0), `✕` Failed (exit ≠ 0), `⏸` Paused
**And** `RNIX_ASCII=1` 时使用 `*`, `o`, `+`, `x`, `=`

**Technical Notes:**
- 新增：`kernel/process_history.go`（ProcessHistory 结构体）
- 修改：`kernel/kernel.go`（reaper 中保存快照 + ListAllProcs 方法）
- 修改：`ipc/protocol.go`（新增方法）、`ipc/server.go`（handler）、`ipc/client.go`（客户端方法）
- 新增/扩展：`internal/ui/symbols.go`（统一状态符号）
- 参考实现：`dashboard-redesign-spec-2026-03-23.md` Section 7（历史进程列表规格）

---

## Story 29.5: Dashboard 历史进程视图

As a 平台构建者,
I want 在 Dashboard 中按 H 键打开全屏历史进程列表,
So that 我可以查看、搜索和聚焦已结束的进程。

**Phase:** 3 — 进程历史保留
**Priority:** P1
**Dependencies:** Story 29.2（viewMode 系统）、Story 29.4（ProcessHistory + IPC）

**Acceptance Criteria:**

**Given** 用户在 Dashboard 主视图中
**When** 按 `H` 键
**Then** 进入全屏覆盖层历史视图（viewHistory）
**And** 通过 `ListAllProcs` IPC 获取存活+历史进程列表

**Given** 历史视图
**When** 渲染进程表
**Then** 显示列：PID | ST(符号) | AGENT | MODEL | TOKENS | CREATED | ELAPSED | EXIT | REASON
**And** 底部统计：Running/Done/Failed 计数、总 token、平均存活时间

**Given** 历史视图
**When** 按 `j/k` 或上/下键
**Then** 光标在进程列表中导航

**Given** 历史视图中选中一个进程
**When** 按 Enter
**Then** 设置 selectedPID + selectedUUID，回到默认视图并聚焦该进程

**Given** 历史视图中选中一个进程
**When** 按 `L`
**Then** 直接跳转到该进程的 LLM 对话查看器

**Given** 历史视图
**When** 按 `/`
**Then** 进入搜索模式，按 agent 名称过滤

**Given** 历史视图
**When** 按 `1/2/3`
**Then** 切换排序模式：1=时间、2=名称、3=PID

**Given** 历史视图
**When** 按 Esc
**Then** 回到之前的视图

**Given** Tree 面板
**When** 有已结束进程（通过 ListAllProcs）
**Then** 在进程树中显示已结束进程，使用 ✓/✕ 状态符号区分
**And** 已结束进程显示 exit code 和存活时间

**Given** 选中 Dead 进程时的 Timeline
**When** 渲染时间线
**Then** 自动过滤只显示该 PID 的事件
**And** Status bar 显示 `(filtered: PID N)`

**Technical Notes:**
- 新增文件：`dashboard_history.go`（历史视图模型 + 渲染 + 按键处理）
- 修改文件：`dashboard_tree.go`（已结束进程显示）、`dashboard_timeline.go`（自动过滤）
- 参考实现：`dashboard-redesign-spec-2026-03-23.md` Section 7（历史进程列表规格）

---

## Story 29.6: LLM 对话查看器

As a 平台构建者,
I want 在 Dashboard 任意视图按 L 键打开全屏 LLM 对话查看器,
So that 我可以查看选中进程任意步骤的完整 LLM request/response。

**Phase:** 4 — LLM 对话查看器
**Priority:** P1
**Dependencies:** Story 29.2（viewMode 系统）、Story 27.2（GetStepDetail IPC 已实现）

**Acceptance Criteria:**

**Given** 用户选中了一个进程（selectedPID ≠ 0）
**When** 按 `L` 键
**Then** 进入全屏覆盖层 LLM 查看器（viewLLM）
**And** 自动加载最新 step 的数据（通过 GetStepDetail IPC）
**And** 初始化 viewport 滚动组件

**Given** 未选中任何进程
**When** 按 `L` 键
**Then** Status bar 显示 "No process selected"

**Given** LLM 查看器
**When** 渲染内容
**Then** 分为两个区块：
- REQUEST → 区块：模型名、token 数、system/user/assistant/tool_result 消息
- RESPONSE ← 区块：模型名、token 数、延迟、assistant/tool_call 消息

**Given** LLM 查看器底部
**When** 渲染步骤导航栏
**Then** 显示所有步骤的摘要列表，当前步骤用 `*` 标记

**Given** LLM 查看器
**When** 按 `h` 或左箭头
**Then** 切换到上一步（如果不是第一步）

**Given** LLM 查看器
**When** 按 `l` 或右箭头
**Then** 切换到下一步（如果不是最后一步）

**Given** LLM 查看器
**When** 按 `j/k` 或 PgUp/PgDn
**Then** 上下滚动长内容

**Given** LLM 查看器
**When** 按 `y`
**Then** 复制当前内容到剪贴板（通过 OSC 52）

**Given** LLM 查看器
**When** 按 Esc
**Then** 回到之前的视图

**Given** Status bar
**When** LLM 查看器活跃
**Then** 显示：`req:X tok │ resp:Y tok │ Zs ── j/k:scroll h/l:prev/next y:copy Esc:close`

**Given** 其他面板的 Enter 入口
**When** Detail 视图 Recent Steps 按 Enter → 打开该 step 的 LLM 查看器
**When** Timeline 视图选中 llm 事件按 Enter → 打开该事件对应 step
**When** Heatmap 视图选中 segment 按 Enter → 打开该 segment 对应消息

**Technical Notes:**
- 新增文件：`dashboard_llm_viewer.go`（LLM 查看器模型 + 渲染 + 按键处理）
- 修改文件：`dashboard_detail.go`（Enter 入口）、`dashboard_timeline.go`（Enter 入口）、`dashboard_heatmap.go`（Enter 入口）
- 数据源：复用现有 GetStepDetail API（Story 27.2 已实现）
- 参考实现：`dashboard-redesign-spec-2026-03-23.md` Section 6（LLM 对话查看器规格）
