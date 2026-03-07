# Story 13.2: 断点系统

Status: done

## Story

As a 平台构建者,
I want 在 gdb 中设置四种断点（syscall/推理/质量/预算），精确控制智能体暂停的时机,
So that 我可以在关键执行节点检查智能体状态，定位问题根因。

## Acceptance Criteria

1. **Given** 用户处于 gdb 调试会话中
   **When** 用户执行 `break syscall Read`
   **Then** 智能体在下次调用 Read syscall 前暂停，显示 syscall 参数

2. **Given** 用户处于 gdb 调试会话中
   **When** 用户执行 `break reasoning`
   **Then** 智能体在每次 LLM 调用前暂停，显示即将发送的 prompt 摘要

3. **Given** 用户设置质量断点 `break quality --pattern "安全漏洞"`
   **When** 智能体输出包含匹配关键词
   **Then** 智能体暂停，高亮显示匹配内容

4. **Given** 用户设置质量断点 `break quality --eval "输出必须包含代码示例"`
   **When** 智能体输出经轻量模型评估不满足标准
   **Then** 智能体暂停，显示评估结果和不满足原因

5. **Given** 用户设置预算断点 `break budget 5000`
   **When** 智能体 token 消耗达到 5000
   **Then** 智能体暂停，显示当前 token 消耗明细
   **And** 断点触发到暂停延迟 <= 100ms（NFR31）

## Tasks / Subtasks

- [x] Task 1: 断点数据模型与注册表 (AC: #1-5)
  - [x] 1.1 在 `kernel/breakpoint.go` 定义 `Breakpoint` 结构体（ID、Type、Condition、Enabled、HitCount）
  - [x] 1.2 定义 `BreakpointType` 枚举：`BPSyscall`、`BPReasoning`、`BPQuality`、`BPBudget`
  - [x] 1.3 定义 `BreakpointCondition` 接口和各类型的具体条件结构体
  - [x] 1.4 在 `Process` 结构体中新增 `breakpoints` 字段（`[]*Breakpoint`，mu 保护）
  - [x] 1.5 在 `Process` 中新增 `gdbPauseCh chan struct{}`（nil=未暂停；非nil=暂停中，close 恢复）和 `gdbResumeCh chan struct{}`
  - [x] 1.6 实现 `Process.AddBreakpoint(bp *Breakpoint) int`（返回断点 ID）
  - [x] 1.7 实现 `Process.RemoveBreakpoint(id int) bool`
  - [x] 1.8 实现 `Process.ListBreakpoints() []*Breakpoint`
  - [x] 1.9 实现 `Process.CheckBreakpoint(bpType BreakpointType, ctx BreakpointContext) *Breakpoint`（返回命中的断点）

- [x] Task 2: 内核 reasonStep 断点钩子 (AC: #1, #2, #5)
  - [x] 2.1 在 `reasonStep` 循环开头（step 开始前）检查 `BPReasoning` 断点
  - [x] 2.2 在 `reasonStep` 的 token 累加后检查 `BPBudget` 断点（budget 断点在 token 达到阈值时触发，与 kernel 内置 budget_exceeded 不同：budget_exceeded 终止进程，budget 断点暂停进程）
  - [x] 2.3 实现 `Process.GdbPause(reason string, hitBP *Breakpoint)` 方法：设置 gdbPauseCh 阻塞推理循环，通过 IPC 通知 gdb 客户端
  - [x] 2.4 实现 `Process.GdbResume()` 方法：close gdbPauseCh 恢复推理循环
  - [x] 2.5 在 pause 期间保持 IPC 事件流活跃，gdb 客户端可继续接收状态信息

- [x] Task 3: 内核 syscall 断点钩子 (AC: #1)
  - [x] 3.1 在 `KernelImpl.emitEvent` 中检查 `BPSyscall` 断点（syscall 名称匹配时触发）
  - [x] 3.2 syscall 断点在 emitEvent 入口事件（syscall 执行前）检查，匹配时暂停进程
  - [x] 3.3 暂停时向 gdb 事件流发送 `gdb_prompt` 事件（含断点信息：命中的 syscall 名称、参数快照）

- [x] Task 4: 质量断点 (AC: #3, #4)
  - [x] 4.1 在 `reasonStep` 解析 LLM 响应后、执行 action 前检查 `BPQuality` 断点
  - [x] 4.2 `--pattern` 模式：对 `resp.Content` 做字符串/正则匹配
  - [x] 4.3 `--eval` 模式：MVP 阶段用简单规则匹配（检查 resp.Content 是否包含 eval 表达式描述的内容），不调用外部 LLM。后续可升级为轻量模型评估
  - [x] 4.4 匹配命中时暂停进程，向 gdb 发送 `gdb_prompt` 事件（含匹配位置、高亮片段）

- [x] Task 5: IPC 协议扩展 — 断点命令传输 (AC: #1-5)
  - [x] 5.1 新增 IPC 方法 `MethodGdbCommand Method = "gdb_command"`
  - [x] 5.2 定义 `GdbCommandRequest` 结构体：`{PID, Command, Args}`
  - [x] 5.3 定义 `GdbCommandResponse` 结构体：`{OK, Message, Data}`
  - [x] 5.4 在 `ipc/server.go` 增加 `handleGdbCommand` 分发：解析命令路由到对应 kernel 方法
  - [x] 5.5 在 `ipc/client.go` 增加 `SendGdbCommand(pid, cmd, args) (*GdbCommandResponse, error)` 方法
  - [x] 5.6 GdbCommand 走独立连接（与 SendDetach 相同模式），避免阻塞 attach 事件流

- [x] Task 6: gdb CLI 命令扩展 (AC: #1-5)
  - [x] 6.1 在 `cmd/rnix/gdb.go` 的命令循环中新增 `break` 命令解析
  - [x] 6.2 支持 `break syscall <name>` — 创建 syscall 断点
  - [x] 6.3 支持 `break reasoning` — 创建推理断点
  - [x] 6.4 支持 `break quality --pattern "<pattern>"` — 创建正则质量断点
  - [x] 6.5 支持 `break quality --eval "<criteria>"` — 创建评估质量断点
  - [x] 6.6 支持 `break budget <tokens>` — 创建预算断点
  - [x] 6.7 支持 `delete <bp_id>` — 删除断点
  - [x] 6.8 支持 `info breakpoints` / `info bp` — 列出所有断点及状态
  - [x] 6.9 支持 `continue` / `c` — 恢复被断点暂停的进程
  - [x] 6.10 处理 `gdb_prompt` 事件：断点命中时显示断点信息并切换到等待用户命令状态

- [x] Task 7: 测试 (AC: #1-5)
  - [x] 7.1 `kernel/breakpoint_test.go`：Breakpoint 注册/删除/查询、各类型条件匹配
  - [x] 7.2 `kernel/breakpoint_test.go`：GdbPause/GdbResume 并发安全测试
  - [x] 7.3 `ipc/protocol_test.go`：GdbCommand 请求/响应序列化
  - [x] 7.4 `cmd/rnix/gdb_test.go`：break 命令解析（各种格式）
  - [x] 7.5 集成测试：设置 syscall 断点 -> 触发 -> 暂停 -> continue -> 恢复完整流程
  - [x] 7.6 集成测试：预算断点触发验证（token 累加到阈值时暂停）
  - [x] 7.7 集成测试：quality --pattern 断点匹配验证

## Dev Notes

### 架构决策

本 story 在 13-1 的 gdb attach/detach 基础上实现断点系统。核心设计是在 Process 结构体中增加断点列表和暂停机制，在 kernel 的 reasonStep 循环和 emitEvent 中植入检查钩子。断点触发时通过 `gdbPauseCh` 阻塞推理循环，通过 IPC 事件流通知 gdb 客户端。

### 关键设计：暂停机制

断点暂停与现有 Signal 系统的 `WaitIfPaused` 类似但独立：
- **Signal SIGSTOP/SIGCONT**（Story 6.4）：用于进程级暂停/恢复，通过 `proc.resumeCh` 实现
- **gdb 断点暂停**：用于调试级暂停/恢复，通过新的 `proc.gdbPauseCh` 实现
- 两者互不干扰：gdb 暂停不影响 signal 状态，反之亦然
- reasonStep 中已有 `WaitIfPaused()` 检查点，gdb 断点检查在其之后

### 暂停通知流

```
reasonStep 循环
    │
    ├── 检查 Signal pause（已有）
    ├── 检查 gdb breakpoint（新增）
    │       │
    │       ├── BPReasoning: 每个 step 开始前检查
    │       ├── BPSyscall: emitEvent 入口处检查
    │       ├── BPQuality: LLM 响应解析后检查
    │       └── BPBudget: token 累加后检查
    │
    └── 命中断点:
          ├── 1. 构造 BreakpointHit 信息
          ├── 2. 通过 DebugChan 发送 gdb_prompt 事件给 IPC 流
          ├── 3. 设置 gdbPauseCh = make(chan struct{})，阻塞当前 goroutine
          ├── 4. gdb 客户端收到 gdb_prompt，显示断点信息，等待用户命令
          ├── 5. 用户输入 `continue` → IPC 发送 gdb_command {cmd: "continue"}
          ├── 6. server handleGdbCommand → proc.GdbResume() → close(gdbPauseCh)
          └── 7. reasonStep goroutine 解除阻塞，继续执行
```

### IPC 命令路由

gdb_command 通过独立的 IPC 连接发送（同 SendDetach 模式），server 端路由：

```
handleGdbCommand:
  "break"     → proc.AddBreakpoint(parsedBP) → 返回 bp_id
  "delete"    → proc.RemoveBreakpoint(id) → 返回 ok/not_found
  "continue"  → proc.GdbResume() → 返回 ok
  "info"      → proc.ListBreakpoints() → 返回 bp 列表
```

### 关键复用点

1. **暂停/恢复模式**：参考 `kernel/process.go` 的 `WaitIfPaused`/`Resume` 模式（Story 6.4 Signal 系统），gdbPauseCh 用完全相同的 channel close 范式
2. **IPC 独立连接**：参考 `ipc/client.go:SendDetach` 实现（Story 13.1），GdbCommand 走独立连接
3. **事件流**：复用 `StreamGdbPrompt` 事件类型（13-1 已定义但未使用，正好在此启用）
4. **gdb 命令解析**：扩展 `cmd/rnix/gdb.go` 现有的 switch/case 命令循环
5. **emitEvent**：在现有 `kernel/kernel.go:emitEvent` 方法中增加断点检查——所有 syscall 都经过 emitEvent，是 BPSyscall 的天然检查点

### 不要做的事情

- **不要**实现单步执行（step syscall / step reasoning）——这是 Story 13.3
- **不要**实现运行时参数热修改（set model / set context）——这是 Story 13.4
- **不要**实现条件断点（break syscall Read if path=="/dev/llm/claude"）——MVP 只做名称匹配
- **不要**修改 Signal 系统的 SIGSTOP/SIGCONT——gdb 断点暂停使用独立机制
- **不要**为 `break quality --eval` 调用外部 LLM——MVP 用简单字符串包含检查
- **不要**实现断点持久化——断点仅在 gdb 会话期间存活
- **不要**使用 Bubble Tea TUI 框架——保持 bufio.Scanner 交互模式

### 性能约束

- 断点触发到暂停延迟 <= 100ms（NFR31）：断点检查是内存操作（遍历 slice + 条件匹配），远低于此阈值
- BPSyscall 检查在 emitEvent 中执行：需确保不阻塞非 gdb 场景（无断点时 O(1) 跳过）
- 断点列表一般很少（< 10 个），线性扫描可接受
- gdbPauseCh 使用无缓冲 channel close 模式，无额外 goroutine 开销

### 四种断点类型详细设计

#### 1. Syscall 断点 (`break syscall <name>`)
- 匹配 `emitEvent` 的 syscall 参数（如 "Read"、"Write"、"Open"、"Spawn"）
- 在 emitEvent 入口处检查，匹配时调用 `proc.GdbPause`
- 显示信息：syscall 名称、参数 map、调用位置

#### 2. 推理断点 (`break reasoning`)
- 在 reasonStep 循环的 step 开始前（构建 prompt 之前）检查
- 只需检查类型 == BPReasoning，无额外条件
- 显示信息：step 编号、即将发送的 prompt 长度、上一步结果摘要

#### 3. 质量断点 (`break quality`)
- `--pattern`：在 LLM 响应 `resp.Content` 上做 `strings.Contains` 或 `regexp.MatchString`
- `--eval`：MVP 检查 resp.Content 是否包含 eval 描述的关键词（简单实现）
- 在 `parseAction` 之后、执行 action 之前检查
- 显示信息：匹配的模式、匹配位置、响应内容片段

#### 4. 预算断点 (`break budget <tokens>`)
- 在 `proc.TokensUsed += resp.TokensUsed` 之后检查
- 与内核的 `budget_exceeded` 逻辑区分：budget_exceeded 是硬限制（终止进程），budget 断点是软限制（暂停等待用户决策）
- 显示信息：当前 token 总消耗、断点阈值、本次 step 消耗

### Project Structure Notes

新增文件：
- `kernel/breakpoint.go` -- 断点数据模型、条件匹配、注册表方法

修改文件：
- `kernel/process.go` -- 新增 breakpoints 字段、gdbPauseCh/gdbResumeCh、GdbPause/GdbResume 方法、AddBreakpoint/RemoveBreakpoint/ListBreakpoints/CheckBreakpoint 方法
- `kernel/kernel.go` -- reasonStep 中植入断点检查钩子（reasoning/budget/quality）、emitEvent 中植入 syscall 断点检查
- `ipc/protocol.go` -- 新增 MethodGdbCommand、GdbCommandRequest/Response
- `ipc/client.go` -- 新增 SendGdbCommand 方法（独立连接模式）
- `ipc/server.go` -- 新增 handleGdbCommand 处理函数
- `cmd/rnix/gdb.go` -- 扩展命令循环支持 break/delete/info breakpoints/continue

### References

- [Source: kernel/kernel.go:403-415] -- reasonStep 入口，断点检查植入点
- [Source: kernel/kernel.go:416-459] -- step 循环开始，WaitIfPaused + context 检查，gdb 断点检查紧随其后
- [Source: kernel/kernel.go:461-475] -- BuildPrompt 调用，reasoning 断点在此之前触发
- [Source: kernel/kernel.go:544-565] -- token 累加和 budget_exceeded 检查，budget 断点在此处检查
- [Source: kernel/kernel.go:567-593] -- parseAction 后的 action switch，quality 断点在 switch 之前检查
- [Source: kernel/process.go:32-80] -- Process 结构体，新增 breakpoints、gdbPauseCh 字段
- [Source: kernel/process.go:62-65] -- Signal 系统的 resumeCh 暂停模式，gdb 暂停参考此设计
- [Source: ipc/protocol.go:28-29] -- MethodAttachGdb/MethodDetachGdb 定义，新增 MethodGdbCommand
- [Source: ipc/protocol.go:239] -- StreamGdbPrompt 已定义（13-1），本 story 正式启用
- [Source: ipc/client.go:336-389] -- AttachGdb 和独立连接模式，SendGdbCommand 参考 SendDetach
- [Source: ipc/server.go:487-558] -- handleAttachGdb 完整实现，handleGdbCommand 参考此模式
- [Source: ipc/server.go:616-631] -- handleDetachGdb 独立连接处理，handleGdbCommand 参考此模式
- [Source: cmd/rnix/gdb.go:155-187] -- 现有命令循环，扩展 break/delete/continue/info 分支

### 技术栈

- Go 1.26 -- `sync.Mutex` 保护断点列表，`chan struct{}` 实现暂停/恢复
- Cobra v1.10.2 -- 无需新增子命令（扩展 gdb 内部命令循环）
- IPC Unix domain socket -- NDJSON 流式协议 + 独立连接命令传输
- `regexp` 标准库 -- quality --pattern 正则匹配
- Lipgloss -- 断点命中信息的终端样式渲染

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6 (claude-opus-4-6)

### Debug Log References

- Fixed GdbPause test deadlock: GdbPause blocks the calling goroutine, tests must run it in a background goroutine
- Fixed `any` type indexing in protocol_test.go and integration_test.go: need `.(map[string]any)` type assertion
- Fixed server `handleGdbDelete` returning `"id"` key vs test expecting `"bp_id"` key
- Fixed `handleGdbInfo` failing when args is nil/empty: default to "breakpoints"
- Fixed integration test for NotFound: server returns Response{OK:false} which client converts to error

### Completion Notes List

- All 7 Tasks (50 subtasks) implemented and verified
- All 19 packages pass with `-race` flag, zero failures
- Breakpoint system is independent from Signal SIGSTOP/SIGCONT mechanism
- GdbPause emits events via DebugChan as "GdbPause" syscall, server routes these to StreamGdbPrompt
- emitEvent skips breakpoint check for "GdbPause" syscall to prevent recursion
- Quality `--eval` mode uses simple `strings.Contains` (MVP), not external LLM
- SendGdbCommand uses separate IPC connection (same pattern as SendDetach)

### File List

New files:
- `kernel/breakpoint.go` -- Breakpoint data model, condition types, Process methods (Add/Remove/List/Check/GdbPause/GdbResume)
- `kernel/breakpoint_test.go` -- 26 unit tests covering all breakpoint types, conditions, pause/resume, concurrency, performance

Modified files:
- `kernel/kernel.go` -- Added 4 breakpoint check hooks (reasoning, budget, quality in reasonStep; syscall in emitEvent)
- `kernel/process.go` -- Added breakpoints field, gdbPauseCh field, nextBPID counter (from prior Story 13-1 prep)
- `ipc/protocol.go` -- Added MethodGdbCommand, GdbCommandRequest, GdbCommandResponse types
- `ipc/protocol_test.go` -- Added 8 tests for GdbCommand serialization
- `ipc/client.go` -- Added SendGdbCommand method (separate connection pattern)
- `ipc/server.go` -- Added handleGdbCommand dispatcher, handleGdbBreak/Delete/Info, parseBreakpointArgs, GdbPause event routing in handleAttachGdb
- `ipc/server_test.go` -- Added 6 server handler tests for gdb_command (not_found, break_syscall, continue, info, delete, invalid_payload)
- `ipc/integration_test.go` -- Added integration tests for gdb_command (break/delete/continue/info/not_found)
- `cmd/rnix/gdb.go` -- Extended command loop (break/delete/continue/info bp), added gdb helper functions, BreakCommandResult, parseBreakCommand, parseDeleteCommand
- `cmd/rnix/gdb_test.go` -- 15 tests for parseBreakCommand and parseDeleteCommand
