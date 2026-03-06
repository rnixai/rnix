# Epic 3: 调试追踪（Debug Tracing — strace）

当智能体输出不符合预期时，用户运行 `rnix strace <pid>` 实时看到完整 syscall 链路，精确定位问题根因——Rnix 的差异化核心体验。

## Story 3.1: SyscallEvent 记录基础设施

As a 内核开发者,
I want 每个 syscall 的入口和出口都自动记录为 SyscallEvent,
So that strace 可以消费完整的调用链路数据。

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

---
