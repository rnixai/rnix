---
title: '移除 InternalStep，全面对接 CLI 原生事件'
type: 'refactor'
created: '2026-03-24'
status: 'done'
baseline_commit: '36541fc'
context:
  - '_bmad-output/planning-artifacts/research/technical-remove-internalstep-research-2026-03-24.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** InternalStep 是对 CLI 原生能力理解不深时引入的不必要抽象层。它将所有 CLI 驱动事件包装为单一 `InternalStep` syscall 类型，只进入 DebugChan，导致 `rnix log` 完全看不到、dashboard 分类不当。同时 Claude CLI 驱动只处理 `assistant` 和 `result`，丢弃了 tool_call、thinking 等关键事件。

**Approach:** 移除 InternalStep 概念。两个 CLI 驱动全面解析原生 stream-json 事件，通过 StreamEvent 透传所有类型，VFS/Kernel 层分发到 DebugChan（精确 syscall 类型）和 LogChan（LogEntry），使 strace、dashboard、log 都能正确显示。

## Boundaries & Constraints

**Always:**
- 所有 CLI 事件类型都必须被解析和转发，不能静默丢弃
- 现有的 strace 输出格式不能破坏（保持向后兼容的格式风格）
- 现有测试必须通过或更新
- 遵循项目依赖方向：drivers/ 不导入 kernel/

**Ask First:**
- 如果需要新增 LogCategory 类型（当前只有 think/tool/output/warning）
- 如果需要修改 IPC 协议 wire 类型

**Never:**
- 不修改 OpenAI compat 驱动（openai_compat.go）—— 本次只涉及 CLI 驱动
- 不改变 DebugChan/LogChan 的通道机制本身
- 不引入新的 IPC 方法

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| Cursor 进程运行中，rnix log 查看 | Cursor 执行 tool_call + thinking | log 输出 `[tool]` 和 `[think]` 条目 | N/A |
| Claude 进程运行中，rnix log 查看 | Claude 执行 tool_use | log 输出 `[tool]` 条目 | N/A |
| strace 查看 Cursor 进程 | Cursor 执行 tool_call | 格式化显示 DriverToolCall（非 InternalStep） | N/A |
| Dashboard Timeline | 任何 CLI 进程活动 | 事件正确分类，tool_call 归为 catTool | N/A |
| CLI 返回未知事件类型 | stream-json 新增类型 | 静默忽略或作为通用事件转发 | 不崩溃，不丢失已知事件 |
| Claude CLI API 重试 | `system/api_retry` 事件 | 作为 warning 日志可见 | N/A |

</frozen-after-approval>

## Code Map

- `drivers/llm/claude_cli.go` -- Claude CLI 驱动，Stream() 需补充事件解析
- `drivers/llm/cursor_cli.go` -- Cursor CLI 驱动，需补充 user/thinking 事件
- `drivers/llm/driver.go:63-72` -- StreamEvent 类型定义，可能需扩展 Type 值
- `drivers/llm/vfsfile.go:98-133` -- VFS 层事件转发，当前仅转发 tool_call
- `kernel/kernel.go:676-681` -- InternalStep 注册点，需重构为精确事件
- `debug/strace.go:89-91,249-290` -- InternalStep 格式化，需替换
- `cmd/rnix/dashboard_timeline.go:21-40` -- classifySyscall 事件分类
- `cmd/rnix/log.go` -- log 命令显示

## Tasks & Acceptance

**Execution:**
- [x] `drivers/llm/claude_cli.go` -- 在 Stream() 的 switch 中补充 `system`、`user`、`stream_event` 事件类型处理，将它们作为对应 StreamEvent 发送
- [x] `drivers/llm/cursor_cli.go` -- 补充 `user` 和 `thinking` 事件处理，发送为 StreamEvent
- [x] `drivers/llm/driver.go` -- 扩展 StreamEvent.Type 注释，新增 "thinking"、"system"、"user" 等类型值
- [x] `drivers/llm/vfsfile.go` -- writeStream() 将所有非 content/done/error 的事件类型都通过 onEvent 转发（不仅 tool_call）
- [x] `kernel/kernel.go` -- 将 SetStreamHandler 回调中的 `InternalStep` 替换为精确 syscall 名称（基于事件 type 字段映射：tool_call→DriverToolCall，thinking→DriverThinking，system→DriverInit 等），同时 emit LogEntry 到 LogChan
- [x] `debug/strace.go` -- 移除 formatInternalStep，添加 DriverToolCall/DriverThinking/DriverInit 的格式化函数
- [x] `cmd/rnix/dashboard_timeline.go` -- classifySyscall 增加 DriverToolCall→catTool，DriverThinking→catLLM 等分类
- [x] `drivers/llm/claude_cli_test.go` -- 更新测试覆盖新事件类型
- [x] `drivers/llm/cursor_cli_test.go` -- 更新测试覆盖 thinking/user 事件

**Acceptance Criteria:**
- Given Cursor 进程执行 tool_call，when `rnix log <pid>`，then 输出包含 `[tool]` 类别条目显示工具名和路径
- Given Cursor 进程执行 thinking，when `rnix log <pid>`，then 输出包含 `[think]` 类别条目
- Given Claude 进程执行工具调用，when `rnix strace <pid>`，then 输出 DriverToolCall（非 InternalStep）
- Given 任意 CLI 进程活动，when 查看 dashboard timeline，then tool_call 事件归类为 catTool 而非 catVFS
- Given `grep -r "InternalStep" kernel/ debug/ cmd/`，then 无匹配结果

## Design Notes

两个 CLI 的事件模型差异需要在 VFS 层统一：

```
Claude CLI stream_event (content_block_start type=tool_use)
  → StreamEvent{Type: "tool_call", Data: {tool: "Read", ...}}

Cursor CLI tool_call (started/completed)
  → StreamEvent{Type: "tool_call", Data: {tool: "read", subtype: "started", ...}}
```

Kernel 层根据 StreamEvent.Type 映射 syscall 名称：
- `"tool_call"` → `"DriverToolCall"` syscall + LogTool entry
- `"thinking"` → `"DriverThinking"` syscall + LogThink entry
- `"system"` → `"DriverInit"` syscall（不写 LogChan，仅 DebugChan）
- 未知类型 → `"DriverEvent"` syscall（通用兜底）

## Verification

**Commands:**
- `make all` -- expected: lint + vet + test + build 全部通过
- `grep -r "InternalStep" kernel/ debug/ cmd/` -- expected: 无匹配结果
- `go test -race -run TestCursorCli ./drivers/llm/...` -- expected: 全部通过
- `go test -race -run TestClaudeCli ./drivers/llm/...` -- expected: 全部通过

## Suggested Review Order

**核心事件映射（Kernel 层）**

- InternalStep 替换为精确 syscall 类型 + LogChan 写入
  [`kernel.go:676`](../../kernel/kernel.go#L676)

- 事件类型到 syscall 名称的映射函数
  [`kernel.go:2784`](../../kernel/kernel.go#L2784)

- 事件类型到 LogEntry 的映射函数
  [`kernel.go:2800`](../../kernel/kernel.go#L2800)

**驱动层事件解析**

- Claude CLI: 新增 system/user/stream_event 处理 + extractClaudeStreamEvent
  [`claude_cli.go:213`](../../drivers/llm/claude_cli.go#L213)

- Claude CLI: --include-partial-messages 参数添加
  [`claude_cli.go:295`](../../drivers/llm/claude_cli.go#L295)

- Claude CLI: 原始 API 事件提取（tool_use + thinking）
  [`claude_cli.go:332`](../../drivers/llm/claude_cli.go#L332)

- Cursor CLI: 新增 system/user/thinking 事件处理（system 不再跳过）
  [`cursor_cli.go:199`](../../drivers/llm/cursor_cli.go#L199)

**VFS 层事件转发**

- 所有非 content/done/error 事件通过 onEvent 转发
  [`vfsfile.go:104`](../../drivers/llm/vfsfile.go#L104)

**显示层**

- strace: formatDriverEvent 替换 formatInternalStep
  [`strace.go:89`](../../debug/strace.go#L89)

- dashboard: DriverToolCall→catTool, DriverThinking→catLLM
  [`dashboard_timeline.go:28`](../../cmd/rnix/dashboard_timeline.go#L28)

**测试**

- Claude CLI 全事件类型流式测试
  [`claude_cli_test.go:487`](../../drivers/llm/claude_cli_test.go#L487)

- Cursor CLI thinking/user 事件测试
  [`cursor_cli_test.go:555`](../../drivers/llm/cursor_cli_test.go#L555)
