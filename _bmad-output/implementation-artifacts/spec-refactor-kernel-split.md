---
title: '重构 kernel.go 巨婴文件：按职责拆分'
type: 'refactor'
created: '2026-03-29'
status: 'done'
baseline_commit: '97f2071'
context: []
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** `kernel/kernel.go` 有 3612 行 / 121KB，包含从类型定义、进程创建、推理循环到工具执行、观测等所有逻辑，严重违反单一职责原则，难以导航和维护。

**Approach:** 将 kernel.go 按职责拆分为 8 个文件，保持同一 `kernel` 包，不改变任何公共 API 签名。纯文件移动重构。

## Boundaries & Constraints

**Always:**
- 所有新文件保持 `package kernel`，不引入新包
- 零 API 变更：所有导出类型、方法、函数签名不变
- 每个文件只包含一个相关导入块（仅其所需的导入）
- 拆分后 `make all`（lint + vet + test + build）必须通过

**Ask First:**
- 如果发现循环依赖需要调整函数签名

**Never:**
- 不修改任何函数体逻辑
- 不重命名任何符号
- 不移动已在其他文件中的代码（如 signal.go、reap.go）

</frozen-after-approval>

## Code Map

- `kernel/kernel.go` -- 当前巨婴文件，拆分源
- `kernel/action.go` -- 新文件：行为模型与解析
- `kernel/spawn.go` -- 新文件：进程创建（纯移动，不改闭包结构）
- `kernel/reason.go` -- 新文件：推理循环主骨架
- `kernel/reason_actions.go` -- 新文件：各 ActionType case 处理逻辑
- `kernel/tool_exec.go` -- 新文件：原生工具调用执行
- `kernel/observe.go` -- 新文件：观测、日志、步骤记录
- `kernel/proc_query.go` -- 新文件：进程表查询与资源管理

## Tasks & Acceptance

**Execution:**
- [x] `kernel/action.go` -- 已存在（~241 行），包含 ActionType 常量、ReasonAction、llmRequest/llmResponse/llmToolCall、toolProtocol/planProtocol 常量、parseAction()、tryParseStructuredAction()、extractEmbeddedAction()、spawnActionData
- [x] `kernel/spawn.go` -- 创建：移入 Spawn()、resolveLLMDevice()（~517 行）
- [x] `kernel/reason.go` -- 创建：移入 reasonStep() 主循环骨架 + finishProcess() + attemptFallback()（~444 行）
- [x] `kernel/reason_actions.go` -- 创建：handleAction() 分发器 + 7 个 handleAction* 方法（text/tool_call/plan/spawn/complete/replan/specialize）（~537 行）
- [x] `kernel/tool_exec.go` -- 创建：移入 executeNativeToolCalls()、executeNativeVFSTool()、executeNativeMetaAction()（~311 行）
- [x] `kernel/observe.go` -- 创建：移入 emitEvent()、emitLog()、writeStepRecord()、writeDriverStepRecordFull()、recordContextSnapshot()、brief*Summary()、extractContentText()、driverEventToSyscall()、driverEventToLog()、GetLogChan()、GetLogHistory()、GetDebugChan()、setupDriverStreamHandler()（~684 行）
- [x] `kernel/proc_query.go` -- 创建：移入 AddProcess、GetProcess、GetProcessByUUID、RemoveProcess、FindHistory*、LoadHistory、Kill、ListProcesses、ListProcs、ListAllProcs、GetProcInfo、GetSpanID、GetTokenHistory、Register/UnregisterBudgetPool、GetBudgetStatus、RecordSLAResult、GetSLAResults、GetLineage、checkBudgetWarning（~311 行）
- [x] `kernel/kernel.go` -- 瘦身至 278 行：仅保留接口定义（MountManager/KernelCallbacks/ProcessManager）、SpawnOpts、常量、KernelImpl 结构体、NewKernel()、所有 Set* 方法、Mount/Unmount、StartRecording/StopRecording/GetRecordManager

**Acceptance Criteria:**
- Given 拆分完成, when 运行 `make all`, then lint + vet + test + build 全部通过
- Given 拆分完成, when 检查 kernel.go 行数, then 不超过 400 行
- Given 拆分完成, when 检查任何单文件行数, then 不超过 800 行
- Given 任意导出符号, when 在包外引用, then 编译无错误（API 无变更）

## Design Notes

**reason.go / reason_actions.go 拆分策略：**
reason_actions.go 不是独立函数，而是从 reasonStep() 的 switch-case 中提取各 action 处理为 KernelImpl 方法（如 `handleActionSpawn`、`handleActionPlan` 等），reasonStep() 主循环通过调用这些方法保持骨架简洁。这是唯一允许的函数体微调——将 inline case 提取为方法调用。

## Verification

**Commands:**
- `make all` -- expected: 全部通过（0 error, 0 warning）
- `wc -l kernel/kernel.go` -- expected: ≤ 400 行
- `find kernel/ -name '*.go' ! -name '*_test.go' -exec wc -l {} + | sort -rn | head` -- expected: 无文件超过 800 行
- `go test -race ./kernel/...` -- expected: 全部测试通过

## Suggested Review Order

**核心架构（精简后的 kernel.go）**

- 接口定义和 KernelImpl 结构体保留在此，所有 Set* 方法和 Mount/Unmount 未移动
  [`kernel/kernel.go:1`](../../kernel/kernel.go#L1)

**进程创建（Spawn）**

- Spawn 函数完整搬移，包含 stem 分化、LLM 设备解析、MCP 自动挂载、stream handler 设置
  [`kernel/spawn.go:30`](../../kernel/spawn.go#L30)

- resolveLLMDevice 提取为独立方法，支持 CLI/agent/project/default 四级 provider 解析
  [`kernel/spawn.go:22`](../../kernel/spawn.go#L22)

**推理循环**

- reasonStep 主循环骨架：prompt 构建、LLM 读写、response 解析、action 分发
  [`kernel/reason.go:102`](../../kernel/reason.go#L102)

- finishProcess 进程终止：MCP 卸载、回调通知、orphan 检测
  [`kernel/reason.go:21`](../../kernel/reason.go#L21)

- attemptFallback 主 provider 失败时的 fallback 逻辑
  [`kernel/reason.go:60`](../../kernel/reason.go#L60)

**Action 处理器（reason_actions.go）**

- handleAction 分发器：将 switch-case 提取为独立方法调用
  [`kernel/reason_actions.go:15`](../../kernel/reason_actions.go#L15)

- handleActionToolCall：权限检查、设备打开/读写/关闭、circuit breaker
  [`kernel/reason_actions.go:74`](../../kernel/reason_actions.go#L74)

- handleActionSpecialize：技能加载的 TOCTOU 双重检查模式
  [`kernel/reason_actions.go:370`](../../kernel/reason_actions.go#L370)

**原生工具调用（tool_exec.go）**

- executeNativeToolCalls：native function calling 路径的 VFS/meta 分发
  [`kernel/tool_exec.go:14`](../../kernel/tool_exec.go#L14)

- executeNativeVFSTool：read_file/write_file/list_dir/generic 四种操作模式
  [`kernel/tool_exec.go:88`](../../kernel/tool_exec.go#L88)

**观测与日志（observe.go）**

- emitEvent 核心事件发射：gdb 断点检查、debug channel、recording hook、immune daemon
  [`kernel/observe.go:16`](../../kernel/observe.go#L16)

- setupDriverStreamHandler：从 Spawn 内联代码提取的流处理器设置
  [`kernel/observe.go:425`](../../kernel/observe.go#L425)

**进程查询与资源管理（proc_query.go）**

- Kill 信号分发：deliverSignal 调用
  [`kernel/proc_query.go:148`](../../kernel/proc_query.go#L148)

- GetProcInfo/ListProcs/ListAllProcs：进程表快照和历史合并
  [`kernel/proc_query.go:190`](../../kernel/proc_query.go#L190)
