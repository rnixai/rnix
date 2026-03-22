# Story 27.3: Dashboard 时间线三级详细度

Status: done

## Story

As a 平台构建者,
I want 在 dashboard 时间线窗格中通过 v/V 键切换三级详细度，出错和慢操作自动展开,
So that 我可以从一行摘要逐级深入到调试级详情，快速定位异常步骤。

## Acceptance Criteria

### AC-1: Level 1 默认步骤摘要

**Given** dashboard 时间线窗格显示步骤列表（Level 1 默认）
**When** 每步执行完成
**Then** 显示一行摘要：步骤号 + 动作类型 + 目标 + 耗时
**And** 单行渲染耗时 ≤ 1ms（NFR58-obs）

### AC-2: Level 2 展开（v 键）

**Given** dashboard 时间线处于 Level 1
**When** 用户按 `v` 键
**Then** 当前选中步骤展开到 Level 2：显示参数、返回值、token 消耗
**And** 数据通过 GetStepDetail IPC 获取（Story 27.2 已实现）
**And** 展开渲染耗时 ≤ 5ms/步（NFR58-obs）
**And** v 键响应延迟 ≤ 50ms（NFR57）

### AC-3: Level 3 调试级详情（V 键）

**Given** dashboard 时间线处于 Level 2
**When** 用户按 `V` 键（大写）
**Then** 切换到 Level 3 调试级详情：显示 prompt 摘要（消息数、token 数、首条消息预览）
**And** V 键响应延迟 ≤ 50ms（NFR57）

### AC-4: 折叠回 Level 1

**Given** Level 2 或 Level 3 展开状态
**When** 用户再次按 `v` 键
**Then** 折叠回 Level 1

### AC-5: 自动展开出错/慢步骤

**Given** 某步骤出错（返回错误）或耗时 > 1 秒
**When** 该步骤到达
**Then** 自动展开到 Level 2（FR166），用户无需手动切换

### AC-6: ProgressPayload 扩展

**Given** ProgressPayload 结构体
**When** 扩展以支持自动展开判断
**Then** 新增 `HasError bool` 和 `DurationMs float64` 字段
**And** IPC server 的 callbackMux.OnStepComplete 填充这两个字段

### AC-7: spawn --dashboard 入口

**Given** 用户执行 `rnix spawn --dashboard "意图"`
**When** spawn 返回 PID
**Then** 立即进入 dashboard 视图并聚焦该进程（FR168）
**And** 延迟 ≤ 500ms（NFR62-obs）

### AC-8: 新增 list_steps IPC 方法

**Given** dashboard 需要获取步骤列表驱动 Level 1 时间线
**When** 新增 `list_steps` IPC 方法
**Then** 返回指定 PID 的所有已记录步骤的摘要列表（step, action, summary, hasError, durationMs）
**And** 数据从 steps.jsonl 读取
**And** 返回延迟 ≤ 200ms（典型 ≤30 步场景）

## Tasks / Subtasks

- [x] Task 1: IPC 层扩展 (AC: #6, #8)
  - [x] 1.1 `ipc/protocol.go` — 新增 `MethodListSteps`、`ListStepsRequest`、`ListStepsResponse`、`StepSummaryWire` 类型
  - [x] 1.2 `ipc/protocol.go` — ProgressPayload 新增 `HasError bool` 和 `DurationMs float64` 字段
  - [x] 1.3 `ipc/server.go` — 新增 `handleListSteps` handler，从 steps.jsonl 读取所有步骤摘要
  - [x] 1.4 `ipc/client.go` — 新增 `ListSteps(pid) (*ListStepsResponse, error)` 方法 + `SpawnDetached`
  - [x] 1.5 `kernel/kernel.go` — `KernelCallbacks.OnStepComplete` 签名扩展：新增 `hasError bool, durationMs float64` 参数
  - [x] 1.6 更新所有 `KernelCallbacks` 实现（callbackMux、测试 mock、compose 包等）
- [x] Task 2: Dashboard Model 扩展 (AC: #1, #2, #3, #4, #5)
  - [x] 2.1 新增 `stepEntry` 类型（步骤摘要 + 详细度级别）+ `stepListMsg`/`stepDetailResultMsg` Msg 类型
  - [x] 2.2 dashboardModel 新增字段：`stepEntries`、`stepCursor`、`stepDetailCache`、`lastFetchedStep`、`fetchingDetail`
  - [x] 2.3 Timeline pane 默认 step 模式，`s` 键切换 step/syscall 视图
- [x] Task 3: 时间线渲染三级详细度 (AC: #1, #2, #3, #4)
  - [x] 3.1 `renderStepTimeline`：Level 1 一行摘要渲染
  - [x] 3.2 Level 2 展开渲染：参数、返回值、token 统计
  - [x] 3.3 Level 3 调试级渲染：消息数、token 数、首条消息预览
  - [x] 3.4 v/V 键处理逻辑（toggle state machine + 异步 GetStepDetail Cmd）
- [x] Task 4: 数据获取与自动展开 (AC: #5, #8)
  - [x] 4.1 `dashboardTick` 中新增 `fetchStepsCmd`：轮询 ListSteps 增量更新
  - [x] 4.2 v/V 键触发 `fetchStepDetailCmd` 异步 IPC 调用
  - [x] 4.3 `applyNewSteps` 自动展开：HasError / DurationMs > 1000
  - [x] 4.4 GetStepDetail 结果缓存到 `stepDetailCache`，PID 切换时清理
- [x] Task 5: spawn --dashboard 入口 (AC: #7)
  - [x] 5.1 `cmd/rnix/main.go` spawn 命令新增 `--dashboard` flag
  - [x] 5.2 `SpawnDetached` 获取 PID 后 `syscall.Exec` 进入 `rnix dashboard --pid=<pid>`
  - [x] 5.3 `dashboardCmd` 新增 `--pid` flag，启动时设置 `selectedPID`
- [x] Task 6: 测试 (AC: all)
  - [x] 6.1 IPC 层单元测试：ListSteps、ProgressPayload 扩展（19 tests）
  - [x] 6.2 Dashboard 渲染测试：三级详细度输出验证（25 tests）
  - [x] 6.3 `make all` 全部通过（lint 0 issues）

## Dev Notes

### 架构决策引用

- **Decision 23**: StepRecord — 默认全量步骤记录 [Source: architecture/core-architectural-decisions.md#Decision-23]
- **Decision 24**: Progress 回调 + StepRecord 双层架构 [Source: architecture/core-architectural-decisions.md#Decision-24]
- **Decision 25**: GetStepDetail IPC 方法 [Source: architecture/core-architectural-decisions.md#Decision-25]
- **Decision 26**: Dashboard 增强 — 时间线三级详细度 + Prompt 查看 [Source: architecture/core-architectural-decisions.md#Decision-26]
- **Sprint Change Proposal**: 2026-03-22 — watch→dashboard 增强决策 [Source: sprint-change-proposal-2026-03-22.md]

### 关键实现约束

#### 1. 时间线数据源切换：syscall → step

**当前状态（Epic 17）：** dashboard 时间线使用 `AttachDebug(pid)` 订阅 **syscall 级事件**（`SyscallEventWire`），每行显示一个 syscall（Open、Read、Write 等）。

**目标状态（Story 27-3）：** 时间线切换为 **step 级事件**——每行对应一个 reasonStep 完成（包含 LLM 调用 + 工具执行的完整步骤）。

**实现策略：新增 `list_steps` IPC 方法 + 轮询**

目前没有"订阅 step 事件"的 IPC 流方法。`callbackMux` 仅在 spawn 时注册且为 1:1 绑定（同一 PID 不能有多个订阅者）。最简方案：

```go
const MethodListSteps Method = "list_steps"

type ListStepsRequest struct {
    PID     types.PID `json:"pid"`
    AfterStep int     `json:"after_step,omitempty"` // 增量拉取：仅返回 step > after_step 的记录
}

type StepSummaryWire struct {
    Step       int     `json:"step"`
    Action     string  `json:"action"`
    Summary    string  `json:"summary"`
    HasError   bool    `json:"has_error"`
    DurationMs float64 `json:"duration_ms"`
}

type ListStepsResponse struct {
    Steps []StepSummaryWire `json:"steps"`
    Total int               `json:"total"`
}
```

Dashboard 在 `dashboardTick` 中轮询 `ListSteps(pid, afterStep=lastKnownStep)` 获取增量更新。轮询间隔与现有 `ListProcs` tick 一致（1s）。

**Server handler 实现：** 打开 `steps.jsonl`，逐行 JSON 反序列化 StepRecord，提取 `Step`、`Action`、`Summary`、`ToolError != ""`（→ HasError）、`ToolDuration`（→ DurationMs）。使用 `AfterStep` 做增量过滤。

**保留 syscall 视图选项：** 不删除现有 syscall 时间线代码。可通过快捷键（例如 `s` 键）在 step 视图和 syscall 视图之间切换。默认使用 step 视图。

#### 2. ProgressPayload 扩展

**当前 ProgressPayload**（`ipc/protocol.go:309-335`）的 `step_complete` 事件仅包含 `Step`、`Action`、`Summary`。

**需要新增字段：**
```go
// ProgressPayload — 在 OnStepComplete 部分新增
HasError   bool    `json:"has_error,omitempty"`    // 步骤是否出错
DurationMs float64 `json:"duration_ms,omitempty"`  // 步骤总耗时 ms
```

这需要修改 `KernelCallbacks` 接口签名（**破坏性变更**）：

```go
// kernel/kernel.go — 当前
OnStepComplete(pid types.PID, step int, action string, summary string)

// 需要改为
OnStepComplete(pid types.PID, step int, action string, summary string, hasError bool, durationMs float64)
```

**受影响的实现点（必须全部更新）：**
1. `ipc/server.go` — `callbackMux.OnStepComplete`（L1415）
2. `kernel/kernel.go` — reasonStep 中的调用点（搜索 `callbacks.OnStepComplete`）
3. `ipc/server_test.go` — `TestCallbackMux_OnStep` 等
4. `ipc/atdd_3_6_step_output_streaming_test.go` — OnStepComplete 测试
5. 任何实现 `KernelCallbacks` 的 mock/stub（搜索 `var _ kernel.KernelCallbacks`）
6. `compose/` 包中的 callbacks（如果有）

**在 reasonStep 中获取 hasError 和 durationMs：**
- `hasError = toolError != ""` 或 LLM 返回 error action
- `durationMs` = 步骤总耗时（从步骤开始到 LLM+工具完成的时间差）

#### 3. Dashboard Model 扩展

**新增类型和字段：**

```go
type stepDetailLevel int
const (
    levelSummary   stepDetailLevel = iota // Level 1: 一行摘要
    levelExpanded                          // Level 2: 参数+返回值+token
    levelDebug                             // Level 3: prompt 摘要
)

type stepEntry struct {
    summary    ipc.StepSummaryWire
    level      stepDetailLevel
    detail     *ipc.GetStepDetailResponse // Level 2/3 缓存，nil 表示未加载
    autoExpand bool                       // 是否被自动展开
}
```

**dashboardModel 新增字段（约 L118-165 区间插入）：**
```go
// Step timeline fields (Story 27-3)
stepEntries       []stepEntry
stepCursor        int
stepDetailCache   map[int]*ipc.GetStepDetailResponse
stepTimelineMode  bool // true=step视图(默认)，false=syscall视图
lastFetchedStep   int  // 增量拉取的最后 step 号
fetchingDetail    bool // 是否正在异步获取 GetStepDetail
```

#### 4. 三级详细度渲染规范

**Level 1（默认，一行摘要）：**
```
  Step 3/30 [tool_call] /dev/fs → read main.go   218ms
  Step 4/30 [tool_call] /dev/shell → go build     1.2s  ⚠
  Step 5/30 [complete]  任务完成                    45ms
```
格式：`Step {n}/{total} [{action}] {summary} {duration}` + 错误/慢标记

**Level 2（展开，3-5 行额外信息）：**
```
▼ Step 4/30 [tool_call] /dev/shell → go build     1.2s  ⚠
  │ Input:  go build -o rnix ./cmd/rnix
  │ Output: (truncated to 200 chars)...
  │ Tokens: req=2340 resp=156
  │ Error:  exit status 1
```
数据来自 `GetStepDetailResponse`：`ToolInput`、`ToolResult`（截断显示前 200 字符）、`RequestTokens`/`ResponseTokens`、`ToolError`

**Level 3（调试级，追加 1-2 行 prompt 信息）：**
```
▼ Step 4/30 [tool_call] /dev/shell → go build     1.2s  ⚠
  │ Input:  go build -o rnix ./cmd/rnix
  │ Output: (truncated to 200 chars)...
  │ Tokens: req=2340 resp=156
  │ Error:  exit status 1
  │ Prompt: 23 msgs, ~12.5k tokens | user: "请帮我编译项目..."
```
追加 `MessageCount`、`TokenCount`、首条用户消息预览（截断 50 字符）

#### 5. v/V 键状态机

```
Level 1 (默认)
  ├─ v → Level 2 (触发 GetStepDetail 异步拉取)
  ├─ V → Level 3 (触发 GetStepDetail 异步拉取)
  └─ j/k → 上下移动 stepCursor

Level 2
  ├─ v → Level 1 (折叠)
  ├─ V → Level 3 (无需再次拉取，已有缓存)
  └─ j/k → 上下移动 stepCursor

Level 3
  ├─ v → Level 1 (折叠)
  ├─ V → Level 2 (降级)
  └─ j/k → 上下移动 stepCursor
```

**注意：** 详细度级别是 per-step 的，不是全局的。每个 step 可以有不同的展开状态。

#### 6. spawn --dashboard 实现

在 `cmd/rnix/main.go` 的 spawn 命令中添加 `--dashboard` flag：

```go
spawnCmd.Flags().Bool("dashboard", false, "启动后自动打开 dashboard")
```

实现策略：spawn 完成后（收到 PID），通过 `os/exec` 启动 `rnix dashboard --pid=<pid>` 进程并用 `syscall.Exec` 替换当前进程（类似 top→dashboard 导航模式）。

**dashboard 命令已有 `--pid` flag 处理？** 需要检查。如果没有，需要在 `dashboardCmd` 中新增：
```go
dashboardCmd.Flags().Int("pid", 0, "启动时自动聚焦的进程 PID")
```
在 `runDashboard` 中解析 `--pid`，初始化 Model 时设置 `selectedPID`。

### 现有代码关键位置

| 文件 | 行号范围 | 说明 |
|------|----------|------|
| `cmd/rnix/dashboard.go:32-38` | paneType 定义 | 三窗格类型：Tree、Timeline、Heatmap |
| `cmd/rnix/dashboard.go:56-62` | timelineEvent 结构体 | 当前基于 SyscallEventWire |
| `cmd/rnix/dashboard.go:118-165` | dashboardModel 结构体 | 需要新增 step 相关字段 |
| `cmd/rnix/dashboard.go:178-234` | Update() | 键盘事件分发，需新增 v/V 键处理 |
| `cmd/rnix/dashboard.go:658-727` | renderTimelinePane | 当前 syscall 渲染，需增加 step 渲染模式 |
| `cmd/rnix/dashboard.go:744-780` | handleTimelineEvent + startTimelineCmd | syscall 事件流处理 |
| `cmd/rnix/dashboard.go:1288-1294` | handlePIDChange | PID 切换时重新订阅时间线和热力图 |
| `ipc/protocol.go:309-335` | ProgressPayload | 需新增 HasError + DurationMs |
| `ipc/protocol.go:593-636` | GetStepDetail 类型 | Level 2/3 数据源 |
| `ipc/server.go:1415-1419` | callbackMux.OnStepComplete | 需更新签名 |
| `ipc/client.go:682-694` | GetStepDetail 客户端 | 已实现，直接复用 |
| `kernel/kernel.go:166-172` | KernelCallbacks 接口 | 需扩展 OnStepComplete 签名 |
| `internal/types/step_record.go:14-31` | StepRecord | 数据源结构体 |

### 前序 Story 27-1 / 27-2 关键实现模式

**StepRecord 磁盘路径：** `.rnix/data/steps/<pid>/steps.jsonl`

**ReadStep 辅助函数**（`kernel/step_writer.go`）：已实现顺序扫描读取指定 step 的 StepRecord。ListSteps handler 可复用此模式（改为读取所有行并提取摘要）。

**GetStepDetail 调用模式**（`ipc/client.go:682-694`）：
```go
client.GetStepDetail(pid, step) → *GetStepDetailResponse
```

**Steps 目录解析**（`ipc/server.go` handleGetStepDetail 中）：
- 进程在内存：`proc.GetProjectConfig().ProjectDir + "/.rnix/data/steps/" + pid`
- 已回收：`kern.GetStepDataDir() + "/" + pid`

**process-meta.json**：reaper 清理前写入，包含 `system_prompt` 和 `tool_defs`。ListSteps 不需要此文件（只需 steps.jsonl）。

**Messages 存储格式：** StepRecord.Messages 是 `json.RawMessage`（非强类型 `[]context.Message`），这是 Story 27-1 的实际实现（与 Epic 文档描述略有不同）。

### 防踩坑清单

1. **不要删除 syscall 时间线**：保留现有 `AttachDebug` 模式的代码，增加 step 模式作为默认，用户可切换
2. **KernelCallbacks 接口变更是破坏性的**：必须搜索并更新所有实现（`rg "OnStepComplete"` 确保无遗漏）
3. **ListSteps 读取 steps.jsonl 不要持有锁**：使用独立 `os.Open` + `bufio.Scanner`，与 StepWriter 的写入端无需协调（append-only NDJSON 语义）
4. **Level 2/3 GetStepDetail 是异步的**：返回 BubbleTea Cmd，结果通过 Msg 回调更新 Model。不要在 Update 中同步调用 IPC
5. **stepDetailCache 需要以 `step` 号为 key**：同一 PID 的不同步骤各自缓存
6. **DurationMs 精度**：从 StepRecord.ToolDuration（`time.Duration`）转换为 `float64` 毫秒；对于非 tool_call 步骤，使用 step 开始到 LLM 响应完成的时间差
7. **AfterStep 增量拉取**：ListSteps 返回 step > AfterStep 的记录，dashboard 维护 `lastFetchedStep` 做增量更新，避免每次全量读取
8. **spawn --dashboard 进程切换**：使用 `syscall.Exec` 替换进程（而非 `os/exec` 创建子进程），这样终端控制权干净转移
9. **现有 dashboard 测试**：`cmd/rnix/dashboard_test.go` 存在，新增渲染测试时遵循已有测试模式

### Project Structure Notes

- IPC 协议扩展遵循已有模式：`protocol.go`（类型定义）→ `server.go`（handler）→ `client.go`（方法）
- Dashboard 是 BubbleTea TUI 应用，所有状态变更通过 `Update()` → `Msg` → `Cmd` 循环
- 步骤数据存储在 `.rnix/data/steps/<pid>/steps.jsonl`，不在内存中
- 所有 IPC 方法遵循 NDJSON over Unix socket 协议

### References

- [Source: architecture/core-architectural-decisions.md#Decision-23] — StepRecord 设计
- [Source: architecture/core-architectural-decisions.md#Decision-24] — Progress + StepRecord 双层架构
- [Source: architecture/core-architectural-decisions.md#Decision-25] — GetStepDetail IPC
- [Source: architecture/core-architectural-decisions.md#Decision-26] — Dashboard 增强三级详细度
- [Source: prd/functional-requirements.md#FR165-FR168] — 功能需求
- [Source: prd/non-functional-requirements.md#NFR57-NFR58-obs] — 性能需求
- [Source: sprint-change-proposal-2026-03-22.md] — watch→dashboard 决策
- [Source: 27-1-steprecord-type-and-disk-writer.md] — StepRecord 实现细节
- [Source: 27-2-getstepdetail-ipc-method.md] — GetStepDetail 实现细节

## Dev Agent Record

### Agent Model Used

claude-4.6-opus-high-thinking (Cursor)

### Debug Log References

Code review identified 6 patch items (P1-P6). P1 (kernel hasError always false) confirmed self-consistent — error tool call paths don't emit OnStepComplete or StepRecord, so hasError=false is correct for all call sites. P2-P6 fixed: BubbleTea integration glue (fetch Cmds, tick polling, PID change cleanup, spawn --dashboard, view toggle).

### Completion Notes List

- IPC 层完整实现：ListSteps wire types + server handler + client method + SpawnDetached
- KernelCallbacks.OnStepComplete 签名扩展，所有实现点已更新
- Dashboard step timeline：三级详细度渲染 + v/V 键异步 GetStepDetail + tick 轮询 ListSteps
- 自动展开：HasError 或 DurationMs > 1s 的步骤自动展开到 Level 2
- PID 切换清理：stepEntries/stepCursor/stepDetailCache/lastFetchedStep 全部重置
- 视图切换：s 键在 step 和 syscall 视图间切换
- spawn --dashboard：SpawnDetached + syscall.Exec 进入 dashboard --pid=<pid>
- 44 个 ATDD 测试全部通过，make all lint 0 issues

### File List

- `ipc/protocol.go` — MethodListSteps, ListStepsRequest/Response, StepSummaryWire, ProgressPayload +HasError/DurationMs
- `ipc/server.go` — handleListSteps handler, callbackMux.OnStepComplete 签名更新
- `ipc/client.go` — ListSteps(), SpawnDetached() 方法
- `ipc/atdd_27_3_liststeps_test.go` — 19 个 IPC 层 ATDD 测试（新增）
- `kernel/kernel.go` — KernelCallbacks 接口签名, 所有 OnStepComplete 调用点 +hasError/durationMs
- `kernel/step_writer.go` — ReadAllSteps() 增量读取函数
- `kernel/stem_integration_test.go` — testCallbacks 签名更新
- `kernel/atdd_3_6_step_output_streaming_test.go` — OnStepComplete 签名更新
- `cmd/rnix/dashboard.go` — stepEntry/stepListMsg/stepDetailResultMsg 类型, 三级详细度渲染, v/V/s 键处理, fetchStepsCmd/fetchStepDetailCmd, PID 切换清理, --pid flag
- `cmd/rnix/dashboard_test.go` — 现有测试兼容更新
- `cmd/rnix/atdd_27_3_dashboard_timeline_test.go` — 25 个 Dashboard ATDD 测试（新增）
- `cmd/rnix/main.go` — cliCallbacks 签名, --dashboard flag + SpawnDetached 实现
- `cmd/rnix/main_test.go` — OnStepComplete 签名更新
- `ipc/atdd_3_6_step_output_streaming_test.go` — OnStepComplete 签名更新
