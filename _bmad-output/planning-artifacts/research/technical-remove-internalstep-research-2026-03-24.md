# 调查报告：移除 InternalStep，全面对接 CLI 原生结构化事件

**日期**: 2026-03-24
**状态**: 调查完成，待实施

## 1. 问题背景

`InternalStep` 是 rnix 中用于表示 LLM CLI 内部操作（如工具调用）的事件类型。当前它只通过 `DebugChan` 传播，导致：
- `strace` 可见（✅）
- `dashboard` 分类不当，归为 catVFS（⚠️）
- `log` 完全看不到（❌ — 使用独立的 LogChan 通道）

## 2. 根因分析

InternalStep 是一开始对 CLI 原生能力理解不深导致的不必要抽象层。两个 CLI 都已原生提供完整的结构化事件流。

## 3. CLI 原生 stream-json 事件类型（已验证）

### 3.1 Claude CLI (`claude -p --output-format stream-json`)

**验证方式**: 官方 SDK 文档 (platform.claude.com/docs/en/agent-sdk/streaming-output) + code.claude.com/docs/en/headless + 实际运行

#### 顶层消息类型（始终存在）

| 类型 | 子类型 | 说明 | 字段 |
|------|--------|------|------|
| `system` | `init` | 会话初始化 | cwd, session_id, tools, mcp_servers, model, permissionMode, slash_commands, apiKeySource, claude_code_version, output_style, agents, skills, plugins, uuid, fast_mode_state |
| `system` | `api_retry` | API 重试通知 | attempt, max_retries, retry_delay_ms, error_status, error, uuid, session_id |
| `user` | — | 用户输入（含工具执行结果回传） | message, parent_tool_use_id, session_id, uuid, timestamp, tool_use_result |
| `assistant` | — | 完整助手消息 | message (content array), parent_tool_use_id, session_id, uuid |
| `result` | `success` / `error` | 最终结果 | is_error, duration_ms, duration_api_ms, num_turns, result, stop_reason, session_id, total_cost_usd, usage, modelUsage, permission_denials, fast_mode_state, uuid |

#### 额外类型（需 `--include-partial-messages`）

| 类型 | 说明 | 字段 |
|------|------|------|
| `stream_event` | 原始 Claude API 流事件包装 | event (raw API event), parent_tool_use_id, uuid, session_id |

**stream_event.event.type 值**:
- `message_start` — 消息开始
- `content_block_start` — 内容块开始（text / tool_use / thinking）
- `content_block_delta` — 内容块增量（text_delta / input_json_delta / thinking_delta / signature_delta）
- `content_block_stop` — 内容块结束
- `message_delta` — 消息级更新（stop_reason, usage）
- `message_stop` — 消息结束
- `ping` — 心跳

**已知限制**: 启用 extended thinking (`max_thinking_tokens`) 时，StreamEvent 不会发送。

**相关 CLI 参数**:
- `--verbose` — 显示完整逐轮输出
- `--include-partial-messages` — 包含 stream_event 中间增量事件（需 stream-json）

**参考来源**:
- https://platform.claude.com/docs/en/agent-sdk/streaming-output
- https://code.claude.com/docs/en/headless

### 3.2 Cursor CLI (`agent --print --output-format stream-json`)

**验证方式**: 官方文档 (.meta/cursorcli/headless.md) + ACP 协议文档 + 实际运行验证

#### 完整消息类型

| 类型 | 子类型 | 说明 | 字段 |
|------|--------|------|------|
| `system` | `init` | 会话初始化 | apiKeySource, cwd, session_id, model, permissionMode |
| `user` | — | 用户输入 | message, session_id |
| `assistant` | — | 助手响应 | message (content array), session_id, model_call_id, timestamp_ms |
| `tool_call` | `started` | 工具调用开始 | call_id, tool_call (对象), model_call_id, session_id, timestamp_ms |
| `tool_call` | `completed` | 工具调用完成 | call_id, tool_call (对象含 result), model_call_id, session_id, timestamp_ms |
| `thinking` | `delta` | 思考增量更新 | text, session_id, timestamp_ms |
| `thinking` | `completed` | 思考完成 | session_id, timestamp_ms |
| `result` | `success` / `error` | 最终结果 | duration_ms, duration_api_ms, is_error, result, session_id, request_id, usage |

**tool_call 支持的工具类型**（tool_call 字段的 key）:
- `shellToolCall` — Shell 命令
- `readToolCall` / `readFileToolCall` — 读取文件
- `writeToolCall` — 写入/创建文件
- `editToolCall` — 修改文件
- `grepToolCall` — 搜索文件
- `globToolCall` — Glob 匹配
- `lsToolCall` — 列目录
- `deleteToolCall` — 删除文件
- `todoToolCall` / `updateTodosToolCall` — 待办项

每个工具调用有 `started` 和 `completed` 两个阶段，completed 包含 result 字段（success 或 error）。

### 3.3 关键架构差异

| 方面 | Claude CLI | Cursor CLI |
|------|-----------|-----------|
| 工具调用事件 | 通过 `stream_event` 包装（原始 API 层级）；或通过 `user` 消息的 `tool_use_result` | 顶层 `tool_call` 事件（started/completed） |
| 思考/推理 | 通过 `stream_event` (content_block type=thinking) | 顶层 `thinking` 事件（delta/completed） |
| 增量流式输出 | 需要 `--include-partial-messages` | 默认包含 |
| API 重试 | `system/api_retry` 事件 | 未文档化 |
| 工具结果 | `user` 消息中的 `tool_use_result` 字段 | `tool_call:completed` 中的 `result` 字段 |

## 4. 当前驱动实现差距

### 4.1 Claude CLI 驱动 (`drivers/llm/claude_cli.go`)

`Stream()` 方法 (L213) 只处理 `assistant` 和 `result` 两种事件：
- ❌ 不处理 `system` 事件
- ❌ 不处理 `user` 事件（含工具结果回传）
- ❌ 不处理 `stream_event`（工具调用、思考等增量事件）
- ❌ 未使用 `--include-partial-messages` 参数

### 4.2 Cursor CLI 驱动 (`drivers/llm/cursor_cli.go`)

已处理 `system`（跳过）、`tool_call`、`assistant`、`result`，但：
- ❌ 缺少 `user` 事件处理
- ❌ 缺少 `thinking` 事件处理

### 4.3 VFS 层 (`drivers/llm/vfsfile.go`)

`LLMFile.writeStream()` 仅转发 `tool_call` 类型的 StreamEvent，其他事件类型（system, user, thinking 等）被直接消费或丢弃。

### 4.4 Kernel 层 (`kernel/kernel.go:676-681`)

所有来自 LLM 驱动的事件都被一律包装为 `InternalStep` syscall 发射到 DebugChan。

## 5. 结论

**InternalStep 是不必要的复杂度**。两个 CLI 都原生提供完整的结构化事件流，包括：
- 会话初始化信息（模型、工具、配置）
- 用户输入和工具结果回传
- 助手响应（文本内容）
- 工具调用事件（开始/完成，含完整参数和结果）
- 思考/推理事件
- API 重试通知
- 最终结果（tokens、成本、时长）

应该：
1. 在驱动层全面解析所有 CLI 事件类型
2. 通过 StreamEvent 透传所有事件
3. 在 kernel/VFS 层正确分发到 DebugChan + LogChan
4. 移除 InternalStep 概念，用更精确的事件类型替代
5. 更新 dashboard 分类和 strace 格式化

## 6. 关键文件

| 文件 | 需要修改 |
|------|---------|
| `drivers/llm/claude_cli.go` | 补充所有事件类型解析，添加 `--include-partial-messages` |
| `drivers/llm/cursor_cli.go` | 补充 `user` 和 `thinking` 事件处理 |
| `drivers/llm/vfsfile.go` | 转发所有事件类型（不仅 tool_call） |
| `drivers/llm/types.go` | StreamEvent 类型扩展 |
| `kernel/kernel.go:676-681` | 移除 InternalStep，用精确事件类型替代 |
| `debug/strace.go:89-91,249-290` | 移除 formatInternalStep，添加新事件格式化 |
| `cmd/rnix/dashboard_timeline.go:21-40` | 更新 classifySyscall 分类 |
| `cmd/rnix/log.go` | 确保新事件通过 LogChan 可见 |
