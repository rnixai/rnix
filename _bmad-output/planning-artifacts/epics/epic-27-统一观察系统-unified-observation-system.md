# Epic 27: 统一观察系统（Unified Observation System — Dashboard 增强）

用户可以在 Dashboard 中查看三级详细度时间线、完整 prompt 内容、进程详情面板和 Intent DAG 可视化。系统默认记录每步完整的 LLM 输入/输出/工具结果（StepRecord），无需手动开启录制。从 `rnix top` 选中进程按回车跳转到 Dashboard 并自动聚焦。Dashboard 集成安全异常、分布式追踪和多智能体评价视图——成为 Rnix 的完整能力透视窗。

> **设计基础**
>
> | 文档 | 说明 |
> |------|------|
> | [PRD FR62, FR165-FR173](../prd/functional-requirements.md#unified-observation-system统一观察系统--dashboard-增强phase-2) | 统一观察系统功能需求 |
> | [PRD FR174-FR176](../prd/functional-requirements.md#dashboard-advanced-integrationdashboard-高级集成phase-3) | Dashboard 高级集成功能需求 |
> | [PRD NFR57-NFR65](../prd/non-functional-requirements.md#unified-observation-system-quality统一观察系统质量--dashboard-增强phase-2) | 观察系统性能需求 |
> | [Architecture Decision 23-26](../architecture/core-architectural-decisions.md#decision-23-steprecord--默认全量步骤记录) | 统一观察系统架构决策 |
> | [User Journey 7](../prd/user-journeys.md#旅程-7陈明通过-top-下钻定位-prompt-注入错误用户-a-平台构建者统一观察路径) | 统一观察用户旅程 |
> | [Sprint Change Proposal](../sprint-change-proposal-2026-03-22.md) | watch→dashboard 增强决策 |
>
> **核心架构：** Progress 回调（实时通知）+ StepRecord（磁盘 JSONL 完整数据存储），LogChan 废弃。
> **设计变更（2026-03-22）：** watch 与 dashboard 功能重叠，选择增强 dashboard 而非新建独立 watch 命令。Story 27-3/27-4/27-5 已回滚并重定义。

**架构决策：** Decision 23（StepRecord）、Decision 24（双层架构）、Decision 25（GetStepDetail）、Decision 26（Dashboard 增强）
**FRs covered:** FR62, FR165, FR166, FR167, FR168, FR169, FR170, FR171, FR172, FR173, FR174, FR175, FR176
**NFRs:** NFR57 ~ NFR64-obs
**Dependencies:** Epic 10（top 命令基础）、Epic 17（Dashboard 基础框架 1785 行）、Epic 26（统一推理循环 reasonStep）

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

## Story 27.3: Dashboard 时间线三级详细度（重定义）

> **变更说明（2026-03-22）：** 原 Story 27-3（watch 命令 Level 1 实时流）已回滚（git revert）。重定义为 Dashboard 时间线增强——复用 Epic 17 已实现的 dashboard 1785 行代码基础，而非新建独立 watch 命令。

As a 平台构建者,
I want 在 dashboard 时间线窗格中通过 v/V 键切换三级详细度，出错和慢操作自动展开,
So that 我可以从一行摘要逐级深入到调试级详情，快速定位异常步骤。

**FRs:** FR165, FR166
**NFRs:** NFR57（切换 ≤ 50ms）、NFR58-obs（Level 1 ≤ 1ms/行, Level 2 ≤ 5ms/步）

**Acceptance Criteria:**

**Given** dashboard 时间线窗格显示步骤列表（Level 1 默认）
**When** 每步执行完成
**Then** 显示一行摘要：步骤号 + 动作类型 + 目标 + 耗时
**And** 单行渲染耗时 ≤ 1ms（NFR58-obs）

**Given** dashboard 时间线处于 Level 1
**When** 用户按 `v` 键
**Then** 当前选中步骤展开到 Level 2：显示参数、返回值、token 消耗
**And** 数据通过 GetStepDetail IPC 获取（Story 27.2 已实现）
**And** 展开渲染耗时 ≤ 5ms/步（NFR58-obs）
**And** v 键响应延迟 ≤ 50ms（NFR57）

**Given** dashboard 时间线处于 Level 2
**When** 用户按 `V` 键（大写）
**Then** 切换到 Level 3 调试级详情：显示 prompt 摘要（消息数、token 数、首条消息预览）
**And** V 键响应延迟 ≤ 50ms（NFR57）

**Given** Level 2 或 Level 3 展开状态
**When** 用户再次按 `v` 键
**Then** 折叠回 Level 1

**Given** 某步骤出错（返回错误）或耗时 > 1 秒
**When** 该步骤的 OnStepComplete 事件到达
**Then** 自动展开到 Level 2（FR166），用户无需手动切换

**Given** ProgressPayload 结构体
**When** 扩展以支持自动展开判断
**Then** 新增 `HasError bool` 和 `DurationMs float64` 字段
**And** IPC server 的 callbackMux.OnStepComplete 填充这两个字段

**Given** 用户执行 `rnix spawn --dashboard "意图"`
**When** spawn 返回 PID
**Then** 立即进入 dashboard 视图并聚焦该进程（FR168）
**And** 延迟 ≤ 500ms（NFR62-obs）

**Technical Notes:**
- 修改文件：`cmd/rnix/dashboard.go`（时间线窗格增加详细度切换）、`ipc/protocol.go`（ProgressPayload 扩展）
- 依赖：Story 27.2（GetStepDetail IPC）
- Level 2/3 数据缓存在 dashboard Model 中，避免重复 IPC 查询

---

## Story 27.4: Dashboard prompt 查看（重定义）

> **变更说明（2026-03-22）：** 原 Story 27-4（watch 三级详细度 + prompt 查看）已回滚。重定义为在 dashboard 时间线中集成 prompt 查看功能。

As a 平台构建者,
I want 在 dashboard 时间线窗格中按 p 键查看选中步骤的完整 prompt 内容,
So that 我可以看到 agent 当时收到了什么指令，精确定位 prompt 注入或上下文问题。

**FRs:** FR167
**NFRs:** NFR59-obs（GetStepDetail ≤ 500ms）

**Acceptance Criteria:**

**Given** dashboard 时间线窗格中选中了某个步骤
**When** 用户按 `p` 键
**Then** 发起 GetStepDetail IPC 请求获取完整 prompt
**And** 进入类似 less 的翻页查看模式
**And** 显示 SystemPrompt + Messages + Tools 定义
**And** GetStepDetail 返回延迟 ≤ 500ms（NFR59-obs）

**Given** prompt 翻页模式
**When** 用户使用 j/k 或上下键滚动
**Then** 内容平滑滚动

**Given** prompt 翻页模式
**When** 用户按 `q` 键
**Then** 返回 dashboard 时间线视图，保持之前的选中状态

**Given** 进程已被 reaper 清理（不在内存中）
**When** 用户按 p 键查看该进程的某步骤 prompt
**Then** 从 `.rnix/data/steps/<pid>/process-meta.json` 和 `steps.jsonl` 读取
**And** 正常显示完整 prompt（离线查看能力）

**Technical Notes:**
- 修改文件：`cmd/rnix/dashboard.go`（新增 prompt pager 子视图）
- 依赖：Story 27.2（GetStepDetail IPC）、Story 27.3（时间线详细度切换）
- prompt 翻页使用 BubbleTea viewport 组件

---

## Story 27.5: top→dashboard 导航（重定义）

> **变更说明（2026-03-22）：** 原 Story 27-5（top↔watch 双向导航）已回滚。重定义为 top→dashboard 单向跳转——dashboard 是独立全屏 TUI，不适合嵌入 top。

As a 平台构建者,
I want 在 `rnix top` 中选中进程按回车跳转到 `rnix dashboard` 并自动聚焦该进程,
So that 我可以从系统全局视图快速深入到单进程的详细观察。

**FRs:** FR62
**NFRs:** NFR61-obs（top→dashboard 切换 ≤ 200ms）

**Acceptance Criteria:**

**Given** `rnix top` 显示进程列表
**When** 用户通过上下键选中某进程并按 Enter
**Then** 退出 top，启动 dashboard 并自动聚焦选中的进程
**And** 切换延迟 ≤ 200ms（NFR61-obs，含 dashboard 初始化和进程聚焦）

**Given** top 中选中的进程 PID
**When** 跳转到 dashboard
**Then** dashboard 的进程树窗格中高亮该进程
**And** 时间线窗格显示该进程的步骤数据
**And** 上下文热力图显示该进程的 token 分布

**Given** dashboard 中
**When** 用户按 `q` 键
**Then** 退出 dashboard 回到终端（不返回 top，因为是独立进程）

**Given** top 中选中的进程已结束（Dead 状态）
**When** 用户按 Enter 跳转
**Then** dashboard 正常显示该进程的历史数据（从 steps.jsonl 读取）

**Technical Notes:**
- 修改文件：`cmd/rnix/top.go`（Enter 键处理调用 dashboard）
- 实现方式：top 退出后通过 exec 启动 `rnix dashboard --pid=<pid>`
- 新增 `--pid` flag 到 dashboard 命令，支持启动时自动聚焦指定进程

---

## Story 27.6: Dashboard 进程详情面板

As a 平台构建者,
I want 在 dashboard 中选中进程后查看完整的运行时信息面板,
So that 我可以了解进程的环境变量、已加载 Skill、FD 表和上下文统计。

**FRs:** FR172
**NFRs:** NFR63-obs（窗格切换 ≤ 100ms, 数据加载 ≤ 1s）

**Acceptance Criteria:**

**Given** dashboard 中选中了某个进程
**When** 用户切换到进程详情窗格（按 Tab 或快捷键）
**Then** 显示进程详情面板，包含：
- 环境变量快照
- 已加载 Skill 列表（名称、描述、allowed-tools）
- FD 表（已打开的 VFS 设备路径及状态）
- 上下文统计（消息数、token 消耗、上下文预算使用率）

**Given** 进程详情面板显示中
**When** 数据通过 IPC 查询
**Then** 数据加载延迟 ≤ 1s（NFR63-obs）
**And** 窗格切换渲染延迟 ≤ 100ms（NFR63-obs）

**Technical Notes:**
- 修改文件：`cmd/rnix/dashboard.go`（新增详情窗格）
- 可能需要新增 IPC 方法获取进程详情（或扩展现有 get_proc_info）

---

## Story 27.7: Dashboard 意图树集成

As a 平台构建者,
I want 在 dashboard 中查看 Intent DAG 可视化窗格,
So that 我可以了解意图分解结构，通过节点状态着色快速识别问题子任务。

**FRs:** FR173
**NFRs:** NFR63-obs（窗格切换 ≤ 100ms, 数据加载 ≤ 1s）

**Acceptance Criteria:**

**Given** 进程通过 `rnix apply` 声明式意图创建
**When** 用户在 dashboard 中切换到意图树窗格
**Then** 以树状 / DAG 图展示意图分解结构
**And** 节点按状态着色（pending=灰、executing=蓝、completed=绿、failed=红）

**Given** 意图树中某个节点
**When** 用户选中该节点
**Then** 联动切换时间线和上下文视图到对应进程的数据

**Given** 没有活跃的意图分解（普通 spawn 启动）
**When** 用户切换到意图树窗格
**Then** 显示空状态提示："当前无活跃的意图分解任务"

**Technical Notes:**
- 修改文件：`cmd/rnix/dashboard.go`（新增意图树窗格）
- 依赖：Epic 19（Intent 系统已实现）
- 数据源：Intent 系统的 IntentTree 结构

---

## Story 27.8: Dashboard 安全异常面板

As a 平台构建者,
I want 在 dashboard 中查看安全异常面板，集成 Immune Daemon 的实时告警信息,
So that 我可以及时发现异常行为并处理安全威胁。

**FRs:** FR174
**NFRs:** NFR63-obs（窗格切换 ≤ 100ms）
**Priority:** P2

**Acceptance Criteria:**

**Given** Immune Daemon 检测到异常行为
**When** 用户在 dashboard 中切换到安全异常窗格
**Then** 显示按严重度排序的告警列表（异常行为检测、已挂起进程、威胁模式匹配）

**Given** 安全告警列表中某条告警
**When** 用户选中并按 Enter
**Then** 跳转到对应进程的详情视图

**Given** 没有活跃的安全告警
**When** 用户切换到安全异常窗格
**Then** 显示绿色安全状态："所有进程行为正常"

**Technical Notes:**
- 修改文件：`cmd/rnix/dashboard.go`（新增安全窗格）
- 依赖：Epic 22（Immune Daemon 已实现）
- 数据源：`rnix immune status` 对应的内部 API

---

## Story 27.9: Dashboard 分布式追踪集成

As a 平台构建者,
I want 在 dashboard 中查看分布式追踪集成窗格，以 span 树展示 Compose 编排的跨进程追踪数据,
So that 我可以在可视化面板中直接分析多智能体协作的时序关系和调用链路。

**FRs:** FR175
**NFRs:** NFR63-obs（窗格切换 ≤ 100ms）
**Priority:** P2

**Acceptance Criteria:**

**Given** Compose 编排正在运行（有活跃的 Trace）
**When** 用户在 dashboard 中切换到追踪窗格
**Then** 以 span 树形式展示跨进程追踪数据
**And** 包含时序关系、调用链路、耗时瀑布图
**And** 数据与 `rnix trace` 命令行输出一致

**Given** 追踪窗格中某个 span 节点
**When** 用户选中
**Then** 联动切换到对应进程的时间线视图

**Given** 没有活跃的 Compose 追踪
**When** 用户切换到追踪窗格
**Then** 显示空状态提示

**Technical Notes:**
- 修改文件：`cmd/rnix/dashboard.go`（新增追踪窗格）
- 依赖：Epic 15（分布式追踪已实现）
- 数据源：trace 系统的 Span 数据

---

## Story 27.10: Dashboard 多智能体评价视图

As a 平台构建者,
I want 在 dashboard 中查看多智能体评价视图，集成声誉系统数据和协作拓扑,
So that 我可以了解各 Agent 模板的历史表现和协作模式，优化智能体配置。

**FRs:** FR176
**NFRs:** NFR63-obs（窗格切换 ≤ 100ms）
**Priority:** P2

**Acceptance Criteria:**

**Given** 系统有声誉系统数据
**When** 用户在 dashboard 中切换到评价窗格
**Then** 显示各 Agent 模板的成功率、token 效率、SLA 达标率
**And** 显示协作拓扑图（进程间通信频率和方向）
**And** 显示能力重叠度矩阵

**Given** 没有足够的历史数据
**When** 用户切换到评价窗格
**Then** 显示提示："需要更多执行数据以生成评价"

**Technical Notes:**
- 修改文件：`cmd/rnix/dashboard.go`（新增评价窗格）
- 依赖：Epic 21（声誉系统已实现）、Epic 22（协作拓扑已实现）
- 数据源：reputation 系统和 topology 系统数据
