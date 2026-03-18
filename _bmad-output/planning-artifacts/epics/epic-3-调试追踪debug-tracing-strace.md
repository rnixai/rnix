# Epic 3: 调试追踪（Debug Tracing — strace）

当智能体输出不符合预期时，用户运行 `rnix strace <pid>` 实时看到完整 syscall 链路，精确定位问题根因——Rnix 的差异化核心体验。

## Story 3.1: SyscallEvent 记录基础设施

As a 平台构建者,
I want 每个 syscall 的入口和出口都自动记录为 SyscallEvent,
So that strace 可以消费完整的调用链路数据，帮助我定位问题。

**Acceptance Criteria:**

**Given** `debug/event.go` 已实现
**When** 查看 SyscallEvent 结构
**Then** 包含 `Timestamp`（相对进程启动）、`PID`、`Syscall`（名称）、`Args`（map[string]any）、`Result`（any）、`Err`（error）、`Duration`（耗时）字段

**Given** 进程的 `DebugChan` 非 nil
**When** 任意 syscall（Open/Read/Write/Close/Stat/CtxAlloc/CtxRead/CtxWrite 等）执行
**Then** 入口处构造 SyscallEvent（记录 Syscall 名称和 Args）
**And** 出口处补充 Result、Err、Duration
**And** 写入 `Process.DebugChan`（缓冲 256）

**Given** 进程的 `DebugChan` 为 nil（无 strace 附着）
**When** syscall 执行
**Then** 跳过事件记录（零开销）

**Given** DebugChan 缓冲已满
**When** 写入新事件
**Then** 不阻塞 syscall 执行（非阻塞写入或丢弃最旧事件）

## Story 3.2: strace 事件消费与格式化

As a 用户,
I want `rnix strace <pid>` 实时流式输出 syscall 调用链路,
So that 我能看到智能体的每一步操作及其结果。

**Acceptance Criteria:**

**Given** `debug/strace.go` 已实现
**When** 调用 strace 附着到指定 PID
**Then** 消费目标进程的 DebugChan
**And** 每个 SyscallEvent 格式化为一行输出

**Given** strace 流式输出中
**When** 收到一个 SyscallEvent
**Then** 输出格式为 `[N.NNNs] SyscallName(args) → result    duration`
**And** 时间戳固定宽度 `[N.NNNs]`
**And** syscall 名称与接口方法名一致（如 `Spawn`、`Open`、`CtxWrite`）（FR29）

**Given** 某个 syscall 耗时 > 1 秒
**When** 格式化该事件
**Then** 行尾自动添加 `← 慢操作` 标注（暗灰色）

**Given** 某个 syscall 返回错误
**When** 格式化该事件
**Then** 整行用红色高亮显示（FR31）

**Given** strace 输出延迟
**When** 从 syscall 发生到终端显示
**Then** 延迟 ≤ 500ms（NFR3）

## Story 3.3: strace CLI 命令

As a 用户,
I want 通过 `rnix strace <pid>` 命令启动 syscall 追踪,
So that 我可以在任何时候调试正在运行的智能体。

**Acceptance Criteria:**

**Given** `cmd/rnix/main.go` 中 strace 子命令已注册
**When** 执行 `rnix strace 1`
**Then** 附着到 PID 1 的 DebugChan，开始流式输出 syscall 事件

**Given** 指定的 PID 不存在
**When** 执行 `rnix strace 999`
**Then** 输出 `✗ PID 999: process not found` + `→ 建议: rnix ps  查看活跃进程`

**Given** strace 正在追踪
**When** 用户按 Ctrl+C
**Then** 仅 detach 追踪，不影响被追踪进程的运行

**Given** 被追踪进程完成
**When** DebugChan 关闭
**Then** strace 输出 detach 汇总后退出

**Given** 使用 `--verbose` flag
**When** 格式化 SyscallEvent
**Then** 展开完整的参数和返回值（默认模式可能截断长参数）

**Given** 使用 `--json` flag
**When** 格式化 SyscallEvent
**Then** 每行输出一个 JSON 对象，字段为 snake_case

## Story 3.4: Syscall Trace Line UI 组件

As a 用户,
I want strace 输出清晰可读，关键信息一眼可见,
So that 我不需要在密集输出中翻找问题。

**Acceptance Criteria:**

**Given** `internal/ui/trace.go` 已实现
**When** 渲染 Trace Line
**Then** 时间戳暗灰色，syscall 名称 Rnix Blue 加粗，参数普通文本，返回值 `→` 后跟结果

**Given** 错误 syscall
**When** 渲染
**Then** 整行红色高亮，在密集输出中视觉上"跳出来"

**Given** LLM 相关 syscall（Open/Write/Read `/dev/llm/*`）
**When** 渲染
**Then** 行尾标注 `← LLM 调用`

**Given** NO_COLOR 环境变量设置
**When** 渲染 Trace Line
**Then** 颜色降级为纯文本，错误行前缀 `[ERR]`

## Story 3.5: 配置解析来源追踪（ConfigResolve strace 事件）

> **追加时间：** 2026-03-18
> **触发来源：** Sprint Change Proposal — EchoMatrix 项目调试中发现配置来源不透明

As a 用户,
I want 在 strace 中看到 provider/model 的解析来源（CLI 标志 / agent 清单 / 项目配置 / 全局配置 / 默认值）,
So that 配置问题可以一目了然定位到具体层级，无需阅读源码排查。

**Acceptance Criteria:**

**Given** 进程 spawn 时 provider 解析完成
**When** strace 附着到该进程
**Then** 输出 `ConfigResolve` 事件，包含 `provider`（最终值）和 `provider_source`（来源标签：cli/agent/project/global/default）

**Given** 进程 spawn 时 model 解析完成
**When** strace 输出 ConfigResolve 事件
**Then** 包含 `model`（最终值）和 `model_source`（来源标签：cli/agent/driver）

**Given** 项目配置中 `default_provider` 与最终 provider 不同（被 agent 覆盖）
**When** strace 输出 ConfigResolve 事件
**Then** 同时显示 `project_default` 和 `agent_provider` 字段，用户可清晰看到覆盖关系

**Given** `ConfigResolve` 事件被 FormatEvent 格式化
**When** 渲染为 trace line
**Then** 输出格式为 `[N.NNNs] ConfigResolve(provider=X [source], model=Y [source], ...)  duration`

## Story 3.6: 推理步骤逐步输出（Step Output Streaming）

> **追加时间：** 2026-03-18
> **触发来源：** Sprint Change Proposal — 推理过程中间文本不可见

As a 用户,
I want 在 CLI 输出中逐步看到每个 reasoning step 的摘要信息,
So that 我可以实时感知智能体的执行进展，而不是等待最终 Result 块一次性展示。

**Acceptance Criteria:**

**Given** reasonStep 循环中某步执行 tool_call 完成
**When** CLI 收到该步的进度事件
**Then** 逐步渲染类似 `[agent/1] step 2: /dev/fs → read sprint-status.yaml` 的摘要行

**Given** reasonStep 循环中某步执行 plan 完成
**When** CLI 收到该步的进度事件
**Then** 逐步渲染类似 `[agent/1] step 1: plan (3 steps)` 的摘要行

**Given** reasonStep 循环中某步执行 spawn 完成
**When** CLI 收到该步的进度事件
**Then** 逐步渲染类似 `[agent/1] step 3: spawn PID 2 "子任务"` 的摘要行

**Given** 用户使用 `--quiet` 或 `--json` 模式
**When** 步骤输出事件到达
**Then** quiet 模式静默，json 模式输出结构化 JSON

---
