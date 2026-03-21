# Epic 27: 统一观察系统（Unified Observation System）

用户可以通过 `rnix watch` 实时观察智能体的每一步推理过程——从一行摘要到完整 prompt 内容，三级详细度按需展开。系统默认记录每步完整的 LLM 输入/输出/工具结果，无需手动开启录制，事后可随时回放分析。从 `rnix top` 选中进程直接下钻到 watch 视图，实现"系统全局 → 单进程深入"的连贯调试路径。

> **设计基础**
>
> | 文档 | 说明 |
> |------|------|
> | [PRD FR165-FR172](../prd/functional-requirements.md#unified-observation-system统一观察系统phase-2) | 统一观察系统功能需求 |
> | [PRD NFR57-NFR64](../prd/non-functional-requirements.md#unified-observation-system-quality统一观察系统质量phase-2) | 观察系统性能需求 |
> | [Architecture Decision 23-26](../architecture/core-architectural-decisions.md#decision-23-steprecord--默认全量步骤记录) | 统一观察系统架构决策 |
> | [User Journey 7](../prd/user-journeys.md#旅程-7陈明通过-top-下钻定位-prompt-注入错误用户-a-平台构建者统一观察路径) | 统一观察用户旅程 |
> | [Product Brief](../product-brief-rnix-2026-03-21.md) | 统一观察系统产品简报 |
>
> **核心架构：** Progress 回调（实时通知）+ StepRecord（磁盘 JSONL 完整数据存储），LogChan 废弃。

**架构决策：** Decision 23（StepRecord）、Decision 24（双层架构）、Decision 25（GetStepDetail）、Decision 26（watch TUI）
**FRs covered:** FR62, FR165, FR166, FR167, FR168, FR169, FR170, FR171, FR172
**NFRs:** NFR57-NFR64
**Dependencies:** Epic 10（top 命令基础）、Epic 26（统一推理循环 reasonStep）

---

## Story 27.1: StepRecord 类型定义与磁盘写入器

As a 平台构建者,
I want 系统在每个 reasonStep 完成后自动将该步的完整数据（Messages 快照、LLM 响应、工具结果）以 NDJSON 格式写入磁盘,
So that 我无需手动开启录制即可事后查看任意步骤的完整 LLM 输入/输出。

**FRs:** FR170, FR172
**NFRs:** NFR62（写入开销 ≤ 1ms/步）、NFR64（不影响 reasonStep 循环性能）

**Acceptance Criteria:**

**Given** `internal/types/step_record.go` 不存在
**When** 创建 StepRecord 类型
**Then** 类型包含以下字段：Step(int)、Timestamp(time.Duration)、Messages([]context.Message 深拷贝)、MessageCount(int)、TokenCount(int)、RawResponse(string)、Action(string)、Summary(string)、ToolPath(string)、ToolInput(string)、ToolResult(string)、ToolError(string)、ToolDuration(time.Duration)、RequestTokens(int)、ResponseTokens(int)

**Given** `kernel/step_writer.go` 不存在
**When** 创建 StepWriter
**Then** StepWriter 使用 `bufio.Writer`（64KB buffer）写入 `.rnix/data/steps/<pid>/steps.jsonl`
**And** `WriteStep(rec StepRecord) error` 方法在 mu.Lock 下 JSON marshal → append → WriteByte('\n') → Flush
**And** 每次 WriteStep 调用均 Flush（保证读取端可见）
**And** `Close() error` 方法 flush + close file

**Given** 进程通过 Spawn 创建
**When** 进程首次进入 reasonStep 循环
**Then** 自动创建 `.rnix/data/steps/<pid>/` 目录和 `steps.jsonl` 文件
**And** 创建对应的 StepWriter 实例挂载到 Process 上

**Given** Process 结构体
**When** 添加观察系统字段
**Then** 新增 `FinalSystemPrompt string` 字段（Spawn 后首次 reasonStep 中保存含 protocol/skills 注入的完整 SystemPrompt）
**And** 新增 `stepWriter *StepWriter` 字段（mu protected）

**Given** reasonStep 循环中 BuildPrompt 返回 promptResult
**When** 首次执行时
**Then** 将完整 sysPrompt（含 protocol 注入、skills 列表）保存到 `proc.FinalSystemPrompt`（仅首次，后续不覆盖）

**Given** reasonStep 循环中一步执行完成（LLM 响应已解析、工具已执行）
**When** 组装 StepRecord
**Then** Messages 字段使用 `promptResult.Messages`（BuildPrompt 已做深拷贝，零额外成本）
**And** RawResponse 为 LLM 原始响应全文（不截断）
**And** ToolResult 为工具返回的完整结果（不截断）
**And** StepRecord 写入在 `AppendMessage()` 之前调用（确保快照一致性）

> **数据大小预期：** 单步 StepRecord 的 JSON 序列化大小取决于 Messages 深拷贝和 RawResponse 长度。典型场景（≤ 50k token 上下文 + ≤ 4k token 响应）约 200KB-500KB/步。极端场景（100k token 上下文）单步可达 2MB。steps.jsonl 文件大小 = 步数 × 平均步大小，30 步典型场景约 6-15MB。磁盘空间管理通过 7 天自动清理策略控制（见下方 reaper 规范）。

**Given** StepWriter.WriteStep 被调用
**When** 度量写入耗时
**Then** 单次写入耗时 ≤ 1ms（NFR62）

**Given** 进程进入 Zombie/Dead 状态
**When** reaper 准备清理 Process 结构体
**Then** 先将 `FinalSystemPrompt` 和 `NativeToolDefs` 序列化写入 `.rnix/data/steps/<pid>/process-meta.json`
**And** 然后 Close StepWriter
**And** `steps/` 目录默认保留 7 天

**Given** 现有 record 系统中的 `recordContextSnapshot` 和 `recordLLMResponse`
**When** StepRecord 已包含完整数据
**Then** 简化：删除 `ContextSnapshotData` 中的 `SystemPromptHash`/`MessageCount`/`TokenEstimate` 摘要逻辑
**And** 删除 `LLMResponseData` 中的 `ResponseSummary`（500 字截断）
**And** record 系统可引用 StepRecord 数据而非自行拼凑

**Technical Notes:**
- 新增文件：`internal/types/step_record.go`、`kernel/step_writer.go`
- 修改文件：`kernel/process.go`（新增字段）、`kernel/kernel.go`（reasonStep 中捕获 + reaper 中写 meta）
- 简化文件：`debug/record.go`（删除摘要结构体）、`kernel/kernel.go`（删除 recordContextSnapshot/recordLLMResponse 调用）
- StepWriter 与 Recorder 共存：StepWriter 是默认全量记录，Recorder 可继续用于 syscall 级事件流（如果用户需要）
- reaper 与 StepWriter 的并发保护：reaper 在写入 process-meta.json 和 Close StepWriter 前，通过 `proc.mu.Lock()` 确保 reasonStep 已退出（不会有新的 WriteStep 调用）

---

## Story 27.2: GetStepDetail IPC 方法

As a 平台构建者,
I want 通过 IPC 按需查询指定进程指定步骤的完整 prompt 内容（SystemPrompt + Messages + Tools）,
So that 我可以在 watch 视图中按 p 键看到 agent 当时收到了什么指令。

**FRs:** FR171
**NFRs:** NFR61（返回延迟 ≤ 500ms）

**Acceptance Criteria:**

**Given** `ipc/protocol.go` 中的方法常量列表
**When** 添加 GetStepDetail 方法
**Then** 新增 `MethodGetStepDetail Method = "get_step_detail"`
**And** 新增 `GetStepDetailRequest` 结构体：`PID types.PID`、`Step int`
**And** 新增 `GetStepDetailResponse` 结构体：`SystemPrompt string`、`Tools []vfs.ToolDef`、`Step int`、`Messages []MessageWire`、`MessageCount int`、`TokenCount int`、`RawResponse string`、`Action string`、`Summary string`、`ToolPath string`、`ToolInput string`、`ToolResult string`、`ToolError string`、`ToolDurationMs float64`、`RequestTokens int`、`ResponseTokens int`
**And** 新增 `MessageWire` 结构体：`Role string`、`Content string`、`ToolCallID string`、`ToolCalls []ToolCallWire`

**Given** `ipc/server.go` 中的 dispatch 方法
**When** 添加 GetStepDetail handler
**Then** handler 从 Process 读取 `FinalSystemPrompt` 和 `NativeToolDefs`
**And** 从 `.rnix/data/steps/<pid>/steps.jsonl` 顺序扫描至目标 step 行
**And** JSON 反序列化为 StepRecord
**And** 组装 `GetStepDetailResponse` 返回

> **性能优化提示：** 当前使用顺序扫描（逐行读取 NDJSON 直到目标 step）。对于典型场景（≤ 30 步，文件 ≤ 15MB），NFR61 的 500ms 上限可满足。若未来步数增长到 100+ 步，可在 `process-meta.json` 中维护步骤字节偏移量索引（`stepOffsets: [0, 4096, 12288, ...]`），实现 O(1) 随机访问。此优化为可选，不阻塞当前实现。

**Given** 进程仍在运行（Running 状态）
**When** 请求 GetStepDetail(pid, step=3)
**Then** 从 Process 结构体获取 FinalSystemPrompt + NativeToolDefs
**And** 从 steps.jsonl 读取 step=3 的行
**And** 返回完整的 GetStepDetailResponse

**Given** 进程已死但 Process 尚在内存（Zombie/Dead 状态）
**When** 请求 GetStepDetail(pid, step=3)
**Then** 同上，正常返回

**Given** 进程已被 reaper 清理（Process 不在内存）
**When** 请求 GetStepDetail(pid, step=3)
**Then** 从 `.rnix/data/steps/<pid>/process-meta.json` 读取 FinalSystemPrompt + NativeToolDefs
**And** 从 steps.jsonl 读取 step=3 的行
**And** 返回完整的 GetStepDetailResponse

**Given** PID 不存在且无磁盘文件
**When** 请求 GetStepDetail(pid, step=3)
**Then** 返回 `ErrorPayload{Code: "not_found", Message: "process <pid> not found"}`

**Given** Step 超出已记录范围
**When** 请求 GetStepDetail(pid, step=99) 但只录制了 5 步
**Then** 返回 `ErrorPayload{Code: "not_found", Message: "step 99 not yet recorded"}`

**Given** `ipc/client.go`
**When** 添加 GetStepDetail 客户端方法
**Then** `func (c *Client) GetStepDetail(pid types.PID, step int) (*GetStepDetailResponse, error)`
**And** 内部 marshal GetStepDetailRequest → send → receive → unmarshal GetStepDetailResponse

**Given** steps.jsonl 文件大小 ≤ 2MB（30 步典型场景）
**When** GetStepDetail 扫描文件
**Then** 返回延迟 ≤ 500ms（NFR61）

**Technical Notes:**
- 新增文件：无（修改现有 protocol.go/server.go/client.go）
- 修改文件：`ipc/protocol.go`（新增方法+类型）、`ipc/server.go`（新增 handler）、`ipc/client.go`（新增方法）
- 读取 steps.jsonl 使用独立 `os.Open` + `bufio.Scanner`，不复用 StepWriter 的 file handle
- 并发安全：NDJSON append-only 语义，reader 看到的行要么完整要么不存在

---

## Story 27.3: watch 命令基础 — Level 1 实时流

As a 平台构建者,
I want 通过 `rnix watch <pid>` 实时看到智能体每一步的摘要（步骤号 + 动作类型 + 目标 + 耗时）,
So that 我可以了解 agent 当前在做什么。

**FRs:** FR165, FR169
**NFRs:** NFR57（attach 延迟 ≤ 200ms）、NFR58（spawn --watch ≤ 100ms）、NFR59（Level 1 渲染 ≤ 1ms/行）

**Acceptance Criteria:**

**Given** `cmd/rnix/watch.go` 不存在
**When** 创建 watch 命令
**Then** 注册 `rnix watch <pid>` Cobra 命令
**And** 命令接受一个位置参数 `<pid>`（整数）

**Given** 用户执行 `rnix watch 42`
**When** 进程 PID=42 存在且正在运行
**Then** CLI 通过 IPC 发送 attach 请求，开始接收 StreamEvent（Progress 回调）
**And** 从命令执行到首条事件显示的延迟 ≤ 200ms（NFR57）

**Given** watch 视图正在接收 Progress 回调
**When** 收到 OnStepComplete 事件（step=3, action="tool_call", summary="/dev/fs → main.go 内容..."）
**Then** 渲染一行：`[step 3] tool_call → /dev/fs  0.2s  ✓`
**And** 单行渲染耗时 ≤ 1ms（NFR59）

**Given** watch 视图正在接收 Progress 回调
**When** 收到 OnStep 事件（step=4, total=30）
**Then** 显示进行中指示：`[step 4/30] thinking...`

**Given** watch 视图正在接收 Progress 回调
**When** 收到 OnComplete 事件（result, exitStatus）
**Then** 显示完成状态行并退出 watch 视图

**Given** ProgressPayload 结构体
**When** 扩展以支持 watch 自动展开判断
**Then** 新增 `HasError bool` 字段（json:"has_error,omitempty"）
**And** 新增 `DurationMs float64` 字段（json:"duration_ms,omitempty"）
**And** IPC server 的 `callbackMux.OnStepComplete` 填充这两个字段

**Given** 用户执行 `rnix spawn --watch "分析 main.go"`
**When** 添加 `--watch` flag 到 spawn 命令
**Then** spawn 返回 PID 后立即进入 watch 视图
**And** 从 spawn 返回 PID 到 watch 首条事件显示的延迟 ≤ 100ms（NFR58）

**Given** 用户在 watch 视图中按 `q` 键
**When** 处理退出
**Then** 断开 IPC 流式连接，返回终端
**And** 进程继续运行不受影响

**Given** 目标 PID 不存在
**When** 用户执行 `rnix watch 999`
**Then** 输出错误信息 `error: process 999 not found` 并退出

**Technical Notes:**
- 新增文件：`cmd/rnix/watch.go`
- 修改文件：`cmd/rnix/main.go`（注册 watch 命令 + spawn --watch flag）、`ipc/protocol.go`（ProgressPayload 扩展）、`ipc/server.go`（填充新字段）
- watch TUI 初始版本使用简单的终端输出（逐行打印），不依赖 BubbleTea（降低复杂度）
- BubbleTea 升级留给 Story 27.4（需要交互式键盘处理）

---

## Story 27.4: watch 三级详细度 + prompt 查看

As a 平台构建者,
I want 在 watch 视图中按 v/V 键展开步骤详情，按 p 键查看完整 prompt 内容,
So that 我可以从摘要逐级深入到完整的 LLM 输入，精确定位问题根因。

**FRs:** FR166, FR167, FR168
**NFRs:** NFR59（Level 2 展开 ≤ 5ms/步）、NFR60（v/V 键切换 ≤ 50ms）、NFR61（GetStepDetail ≤ 500ms）

**Acceptance Criteria:**

**Given** watch 视图升级为 BubbleTea TUI
**When** 重构 watch.go
**Then** watch 使用 BubbleTea Model 实现，支持键盘事件处理
**And** 维护 UI 状态机：Normal → Expanded → Debug → Pager

**Given** watch 视图显示 Level 1 步骤列表
**When** 用户按 `v` 键
**Then** 当前选中步骤展开到 Level 2：显示完整 LLM 响应、工具输入/输出、token 消耗
**And** 数据通过 GetStepDetail IPC 获取
**And** 展开渲染耗时 ≤ 5ms/步（NFR59）
**And** v 键响应延迟 ≤ 50ms（NFR60）

**Given** watch 视图显示 Level 2 展开详情
**When** 用户按 `V` 键（大写）
**Then** 切换到 Level 3 调试级详情：显示消息数、token 数、首条用户消息预览
**And** V 键响应延迟 ≤ 50ms（NFR60）

**Given** watch 视图中某步骤出错（ProgressPayload.HasError = true）
**When** 该步骤的 OnStepComplete 事件到达
**Then** 自动展开到 Level 2（FR167），用户无需手动按 v

**Given** watch 视图中某步骤耗时 > 1 秒（ProgressPayload.DurationMs > 1000）
**When** 该步骤的 OnStepComplete 事件到达
**Then** 自动展开到 Level 2（FR167）

**Given** watch 视图处于任何 Level
**When** 用户按 `p` 键
**Then** 发起 GetStepDetail IPC 请求获取完整 prompt
**And** 进入 less 式翻页模式，显示 SystemPrompt + Messages + Tools 定义
**And** GetStepDetail 返回延迟 ≤ 500ms（NFR61）

**Given** prompt 翻页模式
**When** 用户按 `q` 键
**Then** 返回 watch 实时流视图

**Given** Level 2/3 展开状态
**When** 用户按 `v` 键（再次）
**Then** 折叠回 Level 1

**Technical Notes:**
- 修改文件：`cmd/rnix/watch.go`（升级为 BubbleTea TUI）
- 依赖：Story 27.2（GetStepDetail IPC）、Story 27.3（watch 基础框架）
- prompt 翻页模式可使用 BubbleTea viewport 组件
- Level 2/3 数据缓存在 TUI Model 中，避免重复 IPC 查询

---

## Story 27.5: top↔watch 双向导航

As a 平台构建者,
I want 在 `rnix top` 中选中进程按回车直接进入 watch 视图，在 watch 中按 q 返回 top,
So that 我可以在系统全局视图和单进程观察之间无缝切换。

**FRs:** FR62
**NFRs:** NFR63（top→watch ≤ 100ms、watch→top ≤ 50ms）

**Acceptance Criteria:**

**Given** `rnix top` 显示进程列表
**When** 用户通过上下键选中某进程并按 Enter
**Then** 无缝切换到该进程的 watch 视图
**And** 切换延迟 ≤ 100ms（NFR63）
**And** watch 立即开始接收该进程的 Progress 回调流

**Given** 用户在 watch 视图中
**When** 按 `q` 键
**Then** 返回 top 全局视图
**And** 切换延迟 ≤ 50ms（NFR63）
**And** top 视图恢复到之前的进程列表状态

**Given** top 和 watch 的 TUI 实现
**When** 实现视图切换
**Then** top 和 watch 共享同一个 BubbleTea program
**And** 通过 Model 切换实现视图转换，避免进程重建开销

**Given** 用户从 top 进入 watch 后目标进程已结束
**When** watch 收到 OnComplete 事件
**Then** 显示进程完成状态
**And** 用户按 q 返回 top 视图

**Given** top 命令当前为独立 TUI（cmd/rnix/top.go）
**When** 集成 watch 视图
**Then** 重构为统一的 BubbleTea program，包含 TopModel 和 WatchModel
**And** top.go 中的 Enter 键处理调用 WatchModel 初始化
**And** watch.go 中的 q 键处理回调 TopModel 恢复

**Technical Notes:**
- 修改文件：`cmd/rnix/top.go`（集成 watch 导航）、`cmd/rnix/watch.go`（支持作为 top 子视图）
- 依赖：Story 27.3（watch 基础）、Story 27.4（watch 完整 TUI）
- 如果 top.go 当前不是 BubbleTea 实现，需要先评估重构成本；可选方案是通过 exec 子进程切换
