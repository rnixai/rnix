# Story 3.6: 推理步骤逐步输出（Step Output Streaming）

Status: review

## Story

As a 用户,
I want 在 CLI 输出中逐步看到每个 reasoning step 的摘要信息,
So that 我可以实时感知智能体的执行进展，而不是等待最终 Result 块一次性展示。

## Acceptance Criteria

1. **tool_call 步骤摘要** — Given reasonStep 循环中某步执行 tool_call 完成，When CLI 收到该步的进度事件，Then 逐步渲染类似 `[agent/1] step 2: /dev/fs → read sprint-status.yaml` 的摘要行

2. **plan 步骤摘要** — Given reasonStep 循环中某步执行 plan 完成，When CLI 收到该步的进度事件，Then 逐步渲染类似 `[agent/1] step 1: plan (3 steps)` 的摘要行

3. **spawn 步骤摘要** — Given reasonStep 循环中某步执行 spawn 完成，When CLI 收到该步的进度事件，Then 逐步渲染类似 `[agent/1] step 3: spawn PID 2 "子任务"` 的摘要行

4. **quiet 模式静默** — Given 用户使用 `--quiet` 模式，When 步骤输出事件到达，Then 不输出任何步骤摘要

5. **JSON 模式结构化输出** — Given 用户使用 `--json` 模式，When 步骤输出事件到达，Then 输出结构化 JSON（包含 pid、step、action、summary 字段）

6. **所有测试通过** — Given 实现完成，When 执行 `go test -race ./...`，Then 所有新增和现有测试通过，无竞态条件；`go vet ./...` 无警告

## Tasks / Subtasks

- [x] Task 1: 修改 `kernel/kernel.go` — 扩展 KernelCallbacks 接口 + 在各 action 分支调用新回调 (AC: #1, #2, #3)
  - [x] 1.1 在 `KernelCallbacks` 接口新增 `OnStepComplete(pid types.PID, step int, action string, summary string)` 方法
  - [x] 1.2 在 `ActionToolCall` 分支成功处理后（L1461-1466 `emitEvent` 之后、`continue` 之前），调用 `k.callbacks.OnStepComplete(proc.PID, step, "tool_call", toolCallSummary)`
  - [x] 1.3 在 `ActionPlan` 分支成功处理后（L1500 `emitEvent` 之后、`continue` 之前），调用 `k.callbacks.OnStepComplete(proc.PID, step, "plan", planSummary)`
  - [x] 1.4 在 `ActionSpawn` 分支成功处理后（L1654 `emitEvent` 之后、`continue` 之前），调用 `k.callbacks.OnStepComplete(proc.PID, step, "spawn", spawnSummary)`
  - [x] 1.5 在 `ActionComplete` 分支（L1678 `emitEvent` 之后、`finishProcess` 之前），调用 `k.callbacks.OnStepComplete(proc.PID, step, "complete", "")`
  - [x] 1.6 在 `ActionReplan` 分支成功处理后（L1715 `emitEvent` 之后、`continue` 之前），调用 `k.callbacks.OnStepComplete(proc.PID, step, "replan", reason)`
  - [x] 1.7 在 `ActionSpecialize` 分支成功处理后（L1839 `emitEvent` 之后、`continue` 之前），调用 `k.callbacks.OnStepComplete(proc.PID, step, "specialize", skillName)`
  - [x] 1.8 在 `ActionText` 分支（L1256 `emitEvent` 之后、`finishProcess` 之前），调用 `k.callbacks.OnStepComplete(proc.PID, step, "text", "")`

- [x] Task 2: 生成各 action 类型的 summary 字符串 (AC: #1, #2, #3)
  - [x] 2.1 `tool_call` summary: 格式 `"{toolPath} → {briefResult}"`；`briefResult` = toolResult 前 60 字符截断（去掉换行），超长时追加 `...`
  - [x] 2.2 `plan` summary: 格式 `"plan ({N} steps)"`；从 `action.ToolData` 中解析 `steps` 数组长度，解析失败时用 `"plan"`
  - [x] 2.3 `spawn` summary: 格式 `"spawn PID {childPID} \"{intent}\""`；`intent` = `spawnIntent` 变量（已有）

- [x] Task 3: 修改 `ipc/protocol.go` — ProgressPayload 扩展字段 (AC: #1, #2, #3, #5)
  - [x] 3.1 在 `ProgressPayload` 中新增字段：`Action string \`json:"action,omitempty"\``（action 类型），`Summary string \`json:"summary,omitempty"\``（摘要文本）
  - [x] 3.2 ProgressPayload.Event 增加 `"step_complete"` 值（与现有 `"step"` 并存，不修改 `"step"` 语义）

- [x] Task 4: 修改 `ipc/server.go` — callbackMux 实现 OnStepComplete (AC: #1, #2, #3)
  - [x] 4.1 在 `callbackMux` 上实现 `OnStepComplete(pid types.PID, step int, action string, summary string)` 方法
  - [x] 4.2 构建 `ProgressPayload{Event: "step_complete", PID: pid, Step: step, Action: action, Summary: summary}`
  - [x] 4.3 Marshal 后通过 `m.send(pid, StreamEvent{Type: StreamProgress, Payload: payload})` 发送

- [x] Task 5: 修改 `internal/ui/progress.go` — 新增 AgentStepComplete 方法 (AC: #1, #2, #3, #4)
  - [x] 5.1 新增 `func (p *ProgressReporter) AgentStepComplete(pid types.PID, step int, action string, summary string)`
  - [x] 5.2 quiet 模式直接返回（同 `AgentStep` 模式）
  - [x] 5.3 JSON 模式直接返回（JSON 输出由 cmd 层处理）
  - [x] 5.4 默认模式输出格式：`[agent/{pid}] step {step}: {action}{summary 非空时追加 " → " + summary}`

- [x] Task 6: 修改 `cmd/rnix/main.go` — SpawnAndWatch 回调处理 step_complete 事件 (AC: #1-#5)
  - [x] 6.1 在 SpawnAndWatch 回调的 `switch pp.Event` 中新增 `case "step_complete":` 分支
  - [x] 6.2 默认模式：调用 `progress.AgentStepComplete(pp.PID, pp.Step, pp.Action, pp.Summary)`
  - [x] 6.3 JSON 模式：输出结构化 JSON 行（`{"event":"step_complete","pid":N,"step":N,"action":"...","summary":"..."}`）

- [x] Task 7: 移除或替换现有 `"step"` 事件的简单输出 (AC: #1)
  - [x] 7.1 评估：现有 `case "step":` 调用 `progress.AgentStep(pp.PID, pp.Step, pp.Total)` 输出 `[agent/N] reasoning step N...`，此行在 `step_complete` 出现后会重复。**方案**：保留 `OnStep`（步骤开始通知），但 `AgentStep` 输出改为仅在 verbose 模式输出，默认模式下静默。这样默认模式只看到步骤完成后的摘要行，verbose 模式兼得开始和完成两行。

- [x] Task 8: 添加单元测试 (AC: #1-#6)
  - [x] 8.1 `kernel/kernel_test.go` — 新增 `TestReasonStep_OnStepComplete_ToolCall`：mock KernelCallbacks，验证 tool_call 后 OnStepComplete 被调用且参数正确
  - [x] 8.2 `kernel/kernel_test.go` — 新增 `TestReasonStep_OnStepComplete_Plan`：验证 plan 后的回调
  - [x] 8.3 `kernel/kernel_test.go` — 新增 `TestReasonStep_OnStepComplete_Spawn`：验证 spawn 后的回调包含 child PID
  - [x] 8.4 `internal/ui/progress_test.go` — 新增 `TestAgentStepComplete_*`：验证各模式下（default、quiet、json）的输出
  - [x] 8.5 `ipc/server_test.go` — 验证 callbackMux.OnStepComplete 发送正确的 StreamEvent

## Dev Notes

### 核心设计决策

#### 新增回调 vs 扩展现有回调

**选择：新增 `OnStepComplete` 回调方法**，而非修改现有 `OnStep` 签名。

理由：
- `OnStep(pid, step, total)` 在步骤**开始**时调用，语义是"第 N 步即将执行"
- `OnStepComplete(pid, step, action, summary)` 在步骤**完成**时调用，语义是"第 N 步已完成，结果是..."
- 两者语义不同，不应合并
- 不破坏现有 `OnStep` 调用方的兼容性

#### KernelCallbacks 接口变更

```go
// 当前接口：
type KernelCallbacks interface {
    OnSpawn(pid types.PID, intent, provider, model string)
    OnStep(pid types.PID, step int, total int)
    OnComplete(pid types.PID, result string, exit ExitStatus)
    OnError(pid types.PID, err error)
}

// 新接口（追加一个方法）：
type KernelCallbacks interface {
    OnSpawn(pid types.PID, intent, provider, model string)
    OnStep(pid types.PID, step int, total int)
    OnStepComplete(pid types.PID, step int, action string, summary string)  // NEW
    OnComplete(pid types.PID, result string, exit ExitStatus)
    OnError(pid types.PID, err error)
}
```

**影响范围：** 所有实现 `KernelCallbacks` 的类型必须添加 `OnStepComplete` 方法：
- `ipc/server.go` — `callbackMux`（编译时检查 `var _ kernel.KernelCallbacks = (*callbackMux)(nil)`）
- 搜索其他实现：`kernel_test.go` 中的 mock callbacks、`compose/` 包中的 callbacks 等

#### Summary 生成策略

每种 action 类型的摘要格式：

| Action | Summary 格式 | 示例 |
|--------|-------------|------|
| `tool_call` | `{toolPath} → {briefResult}` | `/dev/fs → read sprint-status.yaml` |
| `plan` | `plan ({N} steps)` | `plan (3 steps)` |
| `spawn` | `spawn PID {childPID} "{intent}"` | `spawn PID 2 "分析代码"` |
| `complete` | `""` (空) | — |
| `replan` | `replan: {reason前40字符}` | `replan: 策略调整` |
| `specialize` | `specialize {skillName}` | `specialize code-analyst` |
| `text` | `""` (空) | — |

**briefResult 截断规则：**
- 取 toolResult 的前 60 个字符
- 去除换行符（替换为空格）
- 超过 60 字符追加 `...`
- 空结果显示 `ok`

#### ProgressPayload 扩展

```go
type ProgressPayload struct {
    Event string    `json:"event"` // "spawn", "step", "step_complete", "complete", "error"
    PID   types.PID `json:"pid"`

    // OnSpawn (existing)
    Intent   string `json:"intent,omitempty"`
    Provider string `json:"provider,omitempty"`
    Model    string `json:"model,omitempty"`

    // OnStep (existing)
    Step  int `json:"step,omitempty"`
    Total int `json:"total,omitempty"`

    // OnStepComplete (NEW) — Step 字段复用
    Action  string `json:"action,omitempty"`   // NEW: "tool_call", "plan", "spawn" etc.
    Summary string `json:"summary,omitempty"`  // NEW: human-readable summary

    // OnComplete (existing)
    Result     string `json:"result,omitempty"`
    ExitCode   int    `json:"exit_code,omitempty"`
    ExitReason string `json:"exit_reason,omitempty"`
    TokensUsed int    `json:"tokens_used,omitempty"`
    SpanID     string `json:"span_id,omitempty"`

    // OnError (existing)
    ErrorMessage string `json:"error_message,omitempty"`
}
```

#### OnStep 输出行为调整

**当前行为：** `OnStep` 在步骤开始时输出 `[agent/N] reasoning step N...`
**新行为：** 默认模式下 `AgentStep` 静默（不再输出 `reasoning step N...`），仅在 verbose 模式下输出。`step_complete` 事件输出取代之。

```go
func (p *ProgressReporter) AgentStep(pid types.PID, step, total int) {
    // 只在 verbose 模式输出步骤开始通知
    if p.renderer.OutputMode != ModeVerbose {
        return
    }
    prefix := AgentStyle.Render(fmt.Sprintf("[agent/%d]", pid))
    fmt.Fprintf(p.renderer.Writer, "%s reasoning step %d...\n", prefix, step)
}
```

这样默认模式的用户看到的是：
```
[kernel] spawning PID 1 (openrouter/claude-4-opus)...
[agent/1] step 1: plan (3 steps)
[agent/1] step 2: /dev/fs → read sprint-status.yaml
[agent/1] step 3: /dev/shell → ls -la
[agent/1] step 4: complete
```

verbose 模式用户看到的是：
```
[kernel] spawning PID 1 (openrouter/claude-4-opus)...
[agent/1] reasoning step 1...
[agent/1] step 1: plan (3 steps)
[agent/1] reasoning step 2...
[agent/1] step 2: /dev/fs → read sprint-status.yaml
...
```

#### JSON 模式输出

JSON 模式下，`step_complete` 事件输出为 NDJSON 行：
```json
{"event":"step_complete","pid":1,"step":2,"action":"tool_call","summary":"/dev/fs → read sprint-status.yaml"}
```

由 `cmd/rnix/main.go` 中 SpawnAndWatch 回调处理——当 `mode == ModeJSON` 时，`json.Marshal(pp)` 后直接写入 stdout。

#### nil callbacks 安全守卫

所有 `OnStepComplete` 调用点必须检查 `k.callbacks != nil`，与现有 `OnStep` 模式一致：
```go
if k.callbacks != nil {
    k.callbacks.OnStepComplete(proc.PID, step, "tool_call", summary)
}
```

### 依赖方向与职责划分

```
kernel/kernel.go (OnStepComplete 调用)
  ├── KernelCallbacks.OnStepComplete(pid, step, action, summary)
  ├── 各 action 分支在 emitEvent("ReasonStep") 之后调用
  └── summary 由 kernel 层生成（因为只有 kernel 有 action 上下文）

ipc/protocol.go (ProgressPayload 扩展)
  ├── 新增 Action, Summary 字段
  └── Event = "step_complete"

ipc/server.go (callbackMux 实现)
  ├── OnStepComplete → ProgressPayload → StreamProgress
  └── 编译时检查 var _ kernel.KernelCallbacks = (*callbackMux)(nil)

internal/ui/progress.go (AgentStepComplete 渲染)
  ├── AgentStepComplete(pid, step, action, summary)
  ├── quiet 模式静默
  └── 默认模式: [agent/{pid}] step {step}: {summary 或 action}

cmd/rnix/main.go (SpawnAndWatch 回调)
  ├── case "step_complete": → progress.AgentStepComplete(...)
  └── JSON 模式: json.Marshal(pp) → stdout
```

### 需要搜索和更新的所有 KernelCallbacks 实现

`KernelCallbacks` 接口新增方法后，**所有实现者都必须添加新方法**。搜索 `KernelCallbacks` 的实现：

1. **`ipc/server.go`** — `callbackMux`（编译时检查 L1399）→ Task 4
2. **测试 mock** — 搜索 `kernel_test.go`、`ipc/server_test.go`、`compose/` 包中的 mock callback 结构体，全部添加 `OnStepComplete` 空方法或断言方法
3. **`compose/` 包** — 可能有自己的 callbacks 实现，需检查

**搜索命令：** `rg "KernelCallbacks" --type go` 找出所有引用点。

### 前序 Story 经验（Story 3.5）

- 测试中用 `bytes.Buffer` + 直接断言字符串内容（不用 testify）
- 测试命名：`TestTypeName_Behavior`
- lipgloss 在非 TTY 测试环境中 `Render()` 不输出 ANSI 码，测试验证逻辑路径而非 ANSI 字节
- `emitEvent` 和 `emitLog` 是非阻塞的，测试需确保回调被同步调用（`OnStepComplete` 在主 goroutine 中调用，不涉及异步问题）
- `finishProcess()` 不关闭 `DebugChan`，测试需手动 `close(proc.DebugChan)`

### 已有 API 参考

**KernelCallbacks（kernel/kernel.go:138-143）：**
```go
type KernelCallbacks interface {
    OnSpawn(pid types.PID, intent, provider, model string)
    OnStep(pid types.PID, step int, total int)
    OnComplete(pid types.PID, result string, exit ExitStatus)
    OnError(pid types.PID, err error)
}
```

**callbackMux 实现模式（ipc/server.go:1374-1396）：**
```go
func (m *callbackMux) OnStep(pid types.PID, step int, total int) {
    pp := ProgressPayload{Event: "step", PID: pid, Step: step, Total: total}
    payload, _ := json.Marshal(pp)
    m.send(pid, StreamEvent{Type: StreamProgress, Payload: payload})
}
```

**AgentStep 渲染模式（internal/ui/progress.go:40-46）：**
```go
func (p *ProgressReporter) AgentStep(pid types.PID, step, total int) {
    if p.renderer.OutputMode == ModeQuiet || p.renderer.OutputMode == ModeJSON {
        return
    }
    prefix := AgentStyle.Render(fmt.Sprintf("[agent/%d]", pid))
    fmt.Fprintf(p.renderer.Writer, "%s reasoning step %d...\n", prefix, step)
}
```

**SpawnAndWatch 回调模式（cmd/rnix/main.go:495-519）：**
```go
pid, final, spawnErr := client.SpawnAndWatch(req, func(ev ipc.StreamEvent) {
    if ev.Type != ipc.StreamProgress { return }
    var pp ipc.ProgressPayload
    if err := json.Unmarshal(ev.Payload, &pp); err != nil { return }
    switch pp.Event {
    case "spawn": ...
    case "step": progress.AgentStep(pp.PID, pp.Step, pp.Total)
    case "error": // handled in final
    }
})
```

**ReasonStep emitEvent 参数模式（kernel/kernel.go 各分支）：**
- `tool_call`: `args = {"step": step, "action": "tool_call", "tool": action.ToolPath}`
- `plan`: `args = {"step": step, "action": "plan"}`
- `spawn`: `args = {"step": step, "action": "spawn", "child_pid": childPID}`
- `complete`: `args = {"step": step, "action": "complete"}`
- `replan`: `args = {"step": step, "action": "replan"}`
- `specialize`: `args = {"step": step, "action": "specialize", "skill": skillName}`

### Project Structure Notes

**修改文件：**
```
kernel/kernel.go             — KernelCallbacks 新增 OnStepComplete 方法；各 action 分支添加回调调用
ipc/protocol.go              — ProgressPayload 新增 Action、Summary 字段
ipc/server.go                — callbackMux 实现 OnStepComplete
internal/ui/progress.go      — 新增 AgentStepComplete 方法；AgentStep 改为仅 verbose 输出
cmd/rnix/main.go             — SpawnAndWatch 回调新增 "step_complete" 分支处理
```

**新增测试文件：**
```
kernel/atdd_3_6_step_output_streaming_test.go    — kernel 层 ATDD 测试
internal/ui/atdd_3_6_step_output_streaming_test.go — UI 层 ATDD 测试
```

**可能需要适配的文件（搜索 KernelCallbacks 实现）：**
```
kernel/kernel_test.go          — 现有 mock callbacks 需添加 OnStepComplete
ipc/server_test.go             — 现有 mock/测试需适配
compose/                       — 如有 KernelCallbacks 实现需适配
```

**不修改文件：**
```
debug/event.go          — 不涉及 strace 事件系统
debug/strace.go         — 步骤输出走 progress 回调而非 DebugChan
internal/types/types.go — 不新增类型（ActionType 已在 kernel 中定义）
```

### 范围边界

**本 Story 包含：**
- `KernelCallbacks` 新增 `OnStepComplete` 方法
- 七种 action 类型的 summary 生成逻辑
- IPC 协议 `ProgressPayload` 扩展
- CLI 步骤完成事件的渲染（default/verbose/quiet/json 四种模式）
- `AgentStep` 行为调整（默认静默，verbose 保留）
- 相关单元测试

**本 Story 不包含：**
- strace 事件系统修改（步骤输出走 progress 回调，不走 DebugChan）
- `rnix top` 仪表板集成（独立 Story）
- 子进程的步骤递归显示（未来增强）
- 步骤输出的持久化或日志记录

### References

- [Source: _bmad-output/planning-artifacts/epics/epic-3-调试追踪debug-tracing-strace.md#Story 3.6] — Story 定义和验收标准
- [Source: kernel/kernel.go:138-143] — KernelCallbacks 接口定义
- [Source: kernel/kernel.go:952-958] — OnStep 调用点（步骤开始）
- [Source: kernel/kernel.go:1247-1847] — reasonStep switch action.Type 各分支
- [Source: ipc/protocol.go:308-331] — ProgressPayload 结构体
- [Source: ipc/server.go:1374-1399] — callbackMux 实现
- [Source: internal/ui/progress.go:39-46] — AgentStep 渲染
- [Source: cmd/rnix/main.go:495-519] — SpawnAndWatch 回调
- [Source: _bmad-output/implementation-artifacts/3-5-config-resolve-strace-event.md] — 前序 Story 经验

## Dev Agent Record

### Agent Model Used

claude-4.6-opus-high-thinking (Cursor)

### Debug Log References

无调试日志。

### Completion Notes List

- KernelCallbacks 接口新增 OnStepComplete 方法，7 个 action 分支（text, tool_call, plan, spawn, complete, replan, specialize）均在 emitEvent 之后、continue/finishProcess 之前调用
- 新增 3 个 summary 辅助函数：briefToolCallSummary（60 字符截断）、briefPlanSummary（解析 steps 数组长度）、briefReplanSummary（40 rune 截断）
- ProgressPayload 新增 Action、Summary 字段（omitempty），Event 增加 "step_complete" 值
- callbackMux.OnStepComplete 将 step_complete 事件序列化后通过 StreamProgress 发送到 IPC 客户端
- AgentStepComplete 渲染方法实现四种模式：default/verbose 输出 "[agent/N] step N: action → summary"，quiet/JSON 静默
- AgentStep 从默认模式输出改为仅 verbose 模式输出，避免与 step_complete 重复
- SpawnAndWatch 回调新增 "step_complete" case，JSON 模式输出结构化 NDJSON 行
- cliCallbacks 新增 OnStepComplete 方法转发到 ProgressReporter
- 更新了 4 个 KernelCallbacks 实现：cliCallbacks、callbackMux、testCallbacks、atdd36Callbacks
- 修复 ATDD 预写测试中的 vet 警告（%d → %v for StreamEventType）和 lint 警告（空 if 分支）
- 更新了因 AgentStep 行为变更而失败的 3 个现有测试：TestAgentStep_Format、TestCliCallbacks_OnStep、TestE2E_SuccessFlow
- `make all`（lint + vet + test + build）全部通过，0 lint issues，无竞态条件

### File List

**修改文件：**
- kernel/kernel.go — KernelCallbacks 新增 OnStepComplete；7 个 action 分支添加回调调用；3 个 summary 辅助函数
- ipc/protocol.go — ProgressPayload 新增 Action、Summary 字段
- ipc/server.go — callbackMux 实现 OnStepComplete
- internal/ui/progress.go — 新增 AgentStepComplete 方法；AgentStep 改为仅 verbose 输出
- cmd/rnix/main.go — cliCallbacks 新增 OnStepComplete；SpawnAndWatch 新增 "step_complete" 分支

**修改测试文件：**
- internal/ui/progress_test.go — TestAgentStep_Format 改用 verbose 模式；新增 TestAgentStep_DefaultMode_Silent
- cmd/rnix/main_test.go — TestCliCallbacks_OnStep 改用 verbose 模式；新增 TestCliCallbacks_OnStepComplete、TestCliCallbacks_OnStep_DefaultMode_Silent
- cmd/rnix/integration_test.go — TestE2E_SuccessFlow 改为检查 "step 1:" 替代 "reasoning step"
- kernel/stem_integration_test.go — testCallbacks 新增 onStepComplete 字段和 OnStepComplete 方法

**修改 ATDD 测试文件（修复预写问题）：**
- ipc/atdd_3_6_step_output_streaming_test.go — 修复 %d → %v vet 警告
- internal/ui/atdd_3_6_step_output_streaming_test.go — 修复空 if 分支 lint 警告
