---
title: 'Dashboard Prompt 视图修复：Tool 显示 0 和 User 消息无内容'
type: 'bugfix'
created: '2026-03-29'
status: 'done'
baseline_commit: 'cacb45c3'
context:
  - '_bmad-output/planning-artifacts/ux-design-specification.md'
---

<frozen-after-approval reason="human-owned intent — do not modify unless human renegotiates">

## Intent

**Problem:** 在 Dashboard Timeline 中按 P 进入 Prompt 视图后，Tools 标签页显示 "Tools (0)" 即使进程使用了工具；部分 Messages 中的 [user] 消息无具体内容显示。

**Approach:** 修复 kernel 中 CLI driver 消息累积逻辑，正确处理 tool_result 的 content 为数组格式的情况；确保 CLI driver 的工具定义在 Prompt 视图中可用。

## Boundaries & Constraints

**Always:**
- 不改变 IPC 协议或 wire 格式
- 不改变 CLI driver 的事件格式
- 保持向后兼容：已有 steps.jsonl / process-meta.json 必须继续可读

**Ask First:**
- 如果发现 tool 定义完全无法从 CLI driver 获取（Claude CLI 不在 system:init 中提供 tools），需要确认处理策略

**Never:**
- 不修改前端 dashboard_timeline.go 的渲染逻辑
- 不引入新的 IPC 方法

## I/O & Edge-Case Matrix

| Scenario | Input / State | Expected Output / Behavior | Error Handling |
|----------|--------------|---------------------------|----------------|
| tool_result content 为 string | `block["content"] = "result text"` | msg.Content = "result text" | N/A |
| tool_result content 为 []any | `block["content"] = [{"type":"text","text":"..."}]` | msg.Content 提取文本拼接 | 忽略非 text 类型块 |
| tool_result content 为空 | `block["content"] = nil` 或缺失 | msg.Content = "" (正常显示) | N/A |
| user 消息 content 为 []any 无 text 块 | content 仅有 tool_result | msg.Content 为空但 ToolCallID 正确 | 不报错 |
| CLI driver system:init 无 tools | `evt["tools"]` 不存在 | nativeToolDefs 为空，Tools tab 显示提示文本 | 已有处理 |

</frozen-after-approval>

## Code Map

- `kernel/kernel.go:752-819` -- CLI driver 消息累积逻辑，处理 assistant/user 事件中的 content blocks
- `kernel/kernel.go:780-786` -- tool_result 处理，当前仅处理 content 为 string 的情况
- `kernel/kernel.go:809-815` -- tool_result 处理的 []any fallback，同样仅处理 content 为 string
- `drivers/llm/claude_cli.go:183` -- claudeContentBlock.Content 类型为 `any`（可能是 string 或 []any）
- `drivers/llm/claude_cli.go:385-410` -- contentBlocksToAny 函数，将 Content 字段原样传递

## Tasks & Acceptance

**Execution:**
- [x] `kernel/kernel.go` -- 修复 tool_result content 处理：当 `block["content"]` 为 `[]any` 或 `[]map[string]any` 时，遍历提取 text blocks 并拼接为 msg.Content -- 当前仅处理 string 格式，Claude CLI 的 tool_result content 可能为数组格式导致 msg.Content 为空

**Acceptance Criteria:**
- Given CLI driver 进程使用了工具且 tool_result 的 content 为 string 格式，当按 P 进入 Prompt 视图 Messages tab，then 工具结果消息显示 [tool:name] + 具体内容
- Given CLI driver 进程使用了工具且 tool_result 的 content 为 `[]any` 格式（数组 of content blocks），当按 P 进入 Prompt 视图 Messages tab，then 工具结果消息正确提取 text blocks 内容
- Given CLI driver 进程的 user 消息包含 tool_result block，当 tool_result content 为 `[]map[string]any` 格式，then msg.Content 正确拼接所有 text 类型块的内容

## Verification

**Commands:**
- `make all` -- lint + vet + test + build 全部通过
- `go test -race -run TestKernel ./kernel/...` -- kernel 测试通过
- `go test -race -run TestStep ./kernel/...` -- step record 测试通过

## Suggested Review Order

核心变更：新增 extractContentText 辅助函数，修复 tool_result content 为数组格式时文本提取失败的问题。

- 新增函数处理 string / map / []map / []any 四种 content 格式
  [`kernel.go:3059`](../../kernel/kernel.go#L3059)

- 第一处调用点：[]map[string]any 分支中的 tool_result 处理
  [`kernel.go:783`](../../kernel/kernel.go#L783)

- 第二处调用点：[]any fallback 分支中的 tool_result 处理
  [`kernel.go:812`](../../kernel/kernel.go#L812)

- 单元测试覆盖所有 I/O 场景
  [`kernel_test.go:3081`](../../kernel/kernel_test.go#L3081)
