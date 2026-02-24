# Story 2.0: 技术债务 — LLM 驱动层错误处理修复

Status: ready-for-dev

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->
<!-- 来源: Epic 1 回顾 (epic-1-retro-2026-02-24.md) — 3 个 HIGH 级 + 1 个 MED 级技术债务 -->

## Story

As a Crux 用户,
I want LLM 驱动层在 CLI 返回非零退出码时仍能提取有意义的错误信息，且 max_turns 参数语义与 Claude CLI 行为一致,
So that 端到端体验不会因为驱动层的错误吞没而产生无法调试的黑盒失败。

## Acceptance Criteria

1. **exit code 非零时解析 stdout JSON** — Given Claude CLI 以 exit code 1 退出且 stdout 包含有效 JSON（`is_error: true`），When `Call()` 处理响应时，Then 从 stdout JSON 的 `result` 字段提取错误信息，And 错误消息包含 LLM 返回的具体错误描述而非仅有 stderr 内容
2. **exit code 非零且 stdout 无有效 JSON 时降级** — Given Claude CLI 以非零退出码退出且 stdout 不是有效 JSON，When `Call()` 处理响应时，Then 降级使用 stderr 内容构造错误消息（保持当前行为），And 错误消息包含 exit code 和 stderr 内容
3. **result 缺失时 graceful fallback** — Given Claude CLI 返回 JSON 但 `result` 字段为空字符串（max_turns 截断场景），When `Call()` 解析响应时，Then 返回有意义的错误提示 `"llm response truncated: no result (possible max_turns limit)"`，And 不返回空字符串作为 `LLMResponse.Content`
4. **Stream 模式 result 缺失处理** — Given Stream 模式下收到 `type: "result"` 事件但 `result` 字段为空，When 事件被处理时，Then 发送 error 类型的 StreamEvent 并包含截断提示信息
5. **移除 defaultMaxTurns 常量** — Given 内核 reasonStep 已自管理推理循环（每次 LLM 调用 MaxTurns=1），When 驱动层构建 CLI 参数时，Then 不再传递 `--max-turns` 参数（让 Claude CLI 使用自身默认值），And 内核层 `llmRequest.MaxTurns` 字段语义改为"单次 CLI 调用的内部工具循环上限"（0 表示不限制）
6. **LLMFile.Write 传播进程 context** — Given VFSFile.Write 被调用时进程有活跃的 context，When LLM 驱动执行 CLI 命令时，Then 使用进程传递的 context（而非 `context.Background()`）支持超时和取消传播
7. **全量回归测试通过** — Given 所有修改完成，When 执行 `go test -race ./...`，Then 全部通过，无新增竞态，无回归

## Tasks / Subtasks

- [ ] Task 1: 修复 Call() exit code 非零时的错误处理 (AC: #1, #2)
  - [ ] 1.1 修改 `drivers/llm/claude_cli.go:106-112`：当 `cmd.Run()` 返回 error 时，先尝试解析 stdout JSON
  - [ ] 1.2 如果 stdout JSON 解析成功且 `is_error: true`，返回 `fmt.Errorf("llm returned error: %s", cliResp.Result)`
  - [ ] 1.3 如果 stdout JSON 解析失败，降级返回当前的 stderr 错误信息
  - [ ] 1.4 新增测试用例 `TestClaudeCliDriver_Call_ExitCodeWithJSON`：exit code 1 + stdout 有效 JSON → 提取 result 错误
  - [ ] 1.5 新增测试用例 `TestClaudeCliDriver_Call_ExitCodeNoJSON`：exit code 1 + stdout 无效 → 降级 stderr
  - [ ] 1.6 更新已有 `TestClaudeCliDriver_Call_CLIError` 用例（如有影响）

- [ ] Task 2: 修复 result 缺失时的 graceful fallback (AC: #3, #4)
  - [ ] 2.1 在 `Call()` 方法中，JSON 解析成功后检查 `cliResp.Result == ""` 且 `!cliResp.IsError`：返回截断错误
  - [ ] 2.2 在 `Stream()` 方法中，`type: "result"` 事件处理时检查 `evt.Result == ""` 且 `!evt.IsError`：发送 error StreamEvent
  - [ ] 2.3 新增测试用例 `TestClaudeCliDriver_Call_EmptyResult`：正常 JSON 但 result 为空 → 截断错误
  - [ ] 2.4 新增测试用例 `TestClaudeCliDriver_Stream_EmptyResult`：stream result 事件 result 为空 → error 事件
  - [ ] 2.5 新增 `TestHelperProcess` case `"empty_result"`：`{"type":"result","subtype":"success","result":"","is_error":false}`
  - [ ] 2.6 新增 `TestHelperProcess` case `"stream_empty_result"`：stream 模式空 result 事件

- [ ] Task 3: 调整 max_turns 语义——移除驱动层 --max-turns 参数 (AC: #5)
  - [ ] 3.1 删除 `drivers/llm/claude_cli.go:20` 的 `defaultMaxTurns` 常量
  - [ ] 3.2 修改 `buildArgs()` 方法：当 `req.MaxTurns > 0` 时传递 `--max-turns`，否则不传递此参数
  - [ ] 3.3 修改 `kernel/kernel.go:246`：将 `MaxTurns: 1` 改为 `MaxTurns: 0`（表示不限制 CLI 内部工具循环）
  - [ ] 3.4 更新 `TestClaudeCliDriver_Call_DefaultArgs`：验证默认不传递 `--max-turns`
  - [ ] 3.5 更新 `TestClaudeCliDriver_Call_Args`：验证显式 MaxTurns > 0 时传递 `--max-turns`
  - [ ] 3.6 更新 `project-context.md` 的 Claude Code CLI 集成节：移除 `--max-turns 1` 描述

- [ ] Task 4: LLMFile.Write 传播进程 context (AC: #6)
  - [ ] 4.1 修改 `drivers/llm/vfsfile.go` 的 `LLMFile` 结构体：存储调用时的 context（或通过 WriteOpt 传递）
  - [ ] 4.2 修改 `LLMFile.Write` 方法：使用存储的 context 替代 `context.Background()`
  - [ ] 4.3 确定 context 传递机制（选项：VFSFile.Write 扩展签名 vs FileFactory 注入 vs Write opts）
  - [ ] 4.4 更新 `vfsfile_test.go`：验证 context 取消时 Write 返回错误
  - [ ] 4.5 注意：此修改可能影响 `kernel/kernel.go` 中 vfs.Write 的调用方式

- [ ] Task 5: 全量回归测试 (AC: #7)
  - [ ] 5.1 `go test -race ./...` 全部通过
  - [ ] 5.2 `go vet ./...` 无警告
  - [ ] 5.3 验证 `cmd/crux/integration_test.go` 中的 E2E 测试不受影响
  - [ ] 5.4 验证 `kernel/kernel_test.go` 中 mock LLM 行为与修改一致

## Dev Notes

### 核心问题分析

**问题 1: exit code 非零时错误信息丢失（HIGH）**

当前代码 `claude_cli.go:106-112`：
```go
err := cmd.Run()
if ctx.Err() == context.DeadlineExceeded {
    return nil, fmt.Errorf("llm call timed out after %v", timeout)
}
if err != nil {
    return nil, fmt.Errorf("claude cli failed (exit %d): %s", cmd.ProcessState.ExitCode(), stderr.String())
}
```

**真实场景：** Claude CLI 有时以 exit code 1 退出，但 stdout 仍然包含有效的 JSON 响应（`is_error: true`），其中 `result` 字段包含详细错误描述。当前代码在 `err != nil` 时直接丢弃 stdout，只返回 stderr（通常是空的或不够有用的信息）。

**修复策略：**
```go
err := cmd.Run()
if ctx.Err() == context.DeadlineExceeded {
    return nil, fmt.Errorf("llm call timed out after %v", timeout)
}

// 即使 exit code 非零，也尝试解析 stdout JSON
var cliResp claudeCliResponse
if parseErr := json.Unmarshal(stdout.Bytes(), &cliResp); parseErr == nil {
    // stdout 有有效 JSON
    if cliResp.IsError {
        return nil, fmt.Errorf("llm returned error: %s", cliResp.Result)
    }
    if cliResp.Result == "" {
        return nil, fmt.Errorf("llm response truncated: no result (possible max_turns limit)")
    }
    // exit code 非零但 JSON 有效且非错误——仍返回结果
    if err != nil {
        // 可选：log warning 但不丢弃有效结果
    }
    return &LLMResponse{
        Content:    cliResp.Result,
        TokensUsed: cliResp.NumTurns,
    }, nil
}

// stdout 无有效 JSON，降级使用 stderr
if err != nil {
    return nil, fmt.Errorf("claude cli failed (exit %d): %s", cmd.ProcessState.ExitCode(), stderr.String())
}
```

**问题 2: max_turns 截断时 result 缺失（HIGH）**

当 `--max-turns` 导致 CLI 截断时，返回的 JSON 可能是：
```json
{"type":"result","subtype":"success","result":"","is_error":false,"num_turns":1}
```

`result` 为空字符串，但 `is_error` 为 false。当前代码将空字符串作为有效 `LLMResponse.Content` 返回，下游 kernel 的 `parseAction` 收到空内容，导致不可预测的行为。

**修复：** 在 JSON 解析后、返回响应前增加空 result 检查。

**问题 3: `defaultMaxTurns = 1` 语义冲突（HIGH）**

Claude CLI 的 `--max-turns` 控制 CLI 内部的工具使用循环次数。`--max-turns 1` 意味着 CLI 只执行一次"turn"（一次 LLM 调用 + 可能的一次工具使用）。但 Crux 的 kernel 已经通过 `reasonStep` 循环自管理推理步骤，每次循环调用一次 CLI。

**当前冲突：**
- `kernel.go:246` 硬编码 `MaxTurns: 1`
- `claude_cli.go:20` 定义 `defaultMaxTurns = 1`
- 两个 "1" 的语义不同但值相同，造成混淆

**修复策略：**
- 移除 `defaultMaxTurns` 常量
- `buildArgs()` 中：`MaxTurns > 0` 时传递 `--max-turns`，否则不传递
- `kernel.go` 中：`MaxTurns: 0` 表示不限制 CLI 内部循环

**问题 4: LLMFile.Write 使用 context.Background()（MED）**

`vfsfile.go:31` 中 `LLMFile.Write` 使用 `context.Background()`，阻止了进程 context 的超时和取消传播。这导致：
- 进程 cancel 后 LLM 调用不会立即中止
- 超时测试需要在 VFS 层 mock 而非 driver 层
- 真实环境中进程无法及时释放资源

### 已有代码（必须复用，禁止重新实现）

**`drivers/llm/claude_cli.go` — 当前完整实现：**

```go
// 关键结构：
type ClaudeCliDriver struct { defaultModel, defaultTimeout, cmdBuilder }
type CommandBuilder func(ctx, name, args...) *exec.Cmd
type claudeCliResponse struct { Type, Subtype, Result, IsError, CostUSD, DurationMS, NumTurns, SessionID }
func (d *ClaudeCliDriver) Call(ctx, req) (*LLMResponse, error)   // 同步调用
func (d *ClaudeCliDriver) Stream(ctx, req) (<-chan StreamEvent, error)  // 流式调用
func (d *ClaudeCliDriver) buildArgs(req, outputFormat) []string  // 参数构建
```

**`drivers/llm/claude_cli_test.go` — 已有 mock 基础设施：**

```go
func TestHelperProcess(t *testing.T)  // 通过 GO_TEST_CASE 环境变量分发 mock 行为
func mockCmdBuilder(testCase string) CommandBuilder  // 创建指向 TestHelperProcess 的 CommandBuilder
// 已有 test cases: success, is_error, cli_error, invalid_json, timeout, args_echo, stream_success, stream_error
```

**`drivers/llm/vfsfile.go` — LLMFile 实现：**
需要读取此文件了解当前 context 使用方式。

**`kernel/kernel.go:242-248` — reasonStep 中的 LLM 请求构建：**
```go
req := llmRequest{
    Intent:       proc.Intent,
    SystemPrompt: promptResult.SystemPrompt,
    Model:        opts.Model,
    MaxTurns:     1,  // ← 这里硬编码 1，需改为 0
    TimeoutMs:    opts.TimeoutMs,
    Messages:     promptResult.Messages,
}
```

### 测试 mock 扩展

需要在 `TestHelperProcess` 中新增以下 test cases：

| Case | Stdout | Stderr | Exit Code |
|------|--------|--------|-----------|
| `exit1_with_json` | `{"type":"result","result":"API rate limited","is_error":true}` | (空) | 1 |
| `exit1_no_json` | (空或垃圾) | `"Error: network failure"` | 1 |
| `empty_result` | `{"type":"result","result":"","is_error":false}` | (空) | 0 |
| `stream_empty_result` | stream 行: `{"type":"result","result":"","is_error":false}` | (空) | 0 |

### 前序 Story 经验教训（必须吸收）

1. **CommandBuilder 注入模式（Story 1.5）：** 所有 exec.Command 调用都通过 `CommandBuilder` 类型注入，测试通过 `mockCmdBuilder` + `TestHelperProcess` 模式实现。本 Story 沿用此模式
2. **syncWriter 并发安全（Story 1.8）：** 涉及回调输出的测试需使用 `syncWriter` 避免竞态
3. **TOCTOU 意识（Story 1.3）：** 修改共享状态时使用原子操作
4. **LLMFile.Write context.Background() 债务（Story 1.8 Review）：** 这是本 Story Task 4 的直接来源，已在 Story 1.8 Review 中记录为 MED 级架构债务

### Git 智能分析

最近 5 次提交模式：
- 每个 Story 完成后更新 sprint-status
- 代码审查修复作为独立提交
- 测试与实现在同一 Story 内完成
- 文件修改遵循架构边界（驱动层改驱动层，内核改内核）

### Project Structure Notes

**本 Story 修改的文件：**

```
drivers/llm/
├── claude_cli.go        (修改 — Call() 错误处理重构、buildArgs() max_turns 逻辑)
├── claude_cli_test.go   (修改 — 新增 4+ 测试用例、扩展 TestHelperProcess)
├── vfsfile.go           (修改 — Write 方法 context 传播)
├── vfsfile_test.go      (修改 — context 取消测试)

kernel/
├── kernel.go            (修改 — reasonStep 中 MaxTurns: 1 → 0)
```

**可能需要修改的文件：**

```
drivers/llm/driver.go           (可能 — 如果 VFSFile.Write 签名需要 context 参数)
vfs/vfs.go                      (可能 — 如果 Write 签名需要 context 参数)
cmd/crux/integration_test.go    (可能 — 如果 mock 行为需要适配新的 max_turns 语义)
kernel/kernel_test.go           (可能 — 如果 newTestKernel mock 需要适配)
```

**不要触碰的文件：**
- `kernel/process.go` — 进程/状态机不变
- `kernel/errors.go` — 错误类型不变
- `context/` 下任何文件
- `internal/types/types.go`
- `internal/xsync/` 下任何文件
- `internal/ui/` 下任何文件
- `cmd/crux/main.go` — CLI 入口不变

### References

- [Source: epic-1-retro-2026-02-24.md#技术债务] — 3 个 HIGH + 3 个 MED 技术债务清单
- [Source: epic-1-retro-2026-02-24.md#关键洞察] — mock 测试 vs 真实环境验证
- [Source: 1-8-end-to-end-integration-and-acceptance.md#Implementation Decisions] — LLMFile.Write context.Background() 债务记录
- [Source: project-context.md#Claude Code CLI 集成] — CLI 调用模式、超时处理
- [Source: project-context.md#错误处理] — SyscallError 规范、错误码
- [Source: project-context.md#测试规则] — CommandBuilder mock 策略、-race 检测
- [Source: drivers/llm/claude_cli.go:106-112] — 当前 exit code 错误处理
- [Source: drivers/llm/claude_cli.go:114-127] — 当前 JSON 解析和响应构建
- [Source: drivers/llm/claude_cli.go:20] — defaultMaxTurns 常量定义
- [Source: kernel/kernel.go:246] — reasonStep 中 MaxTurns 硬编码

## Dev Agent Record

### Agent Model Used

{{agent_model_name_version}}

### Debug Log References

### Completion Notes List

### File List
