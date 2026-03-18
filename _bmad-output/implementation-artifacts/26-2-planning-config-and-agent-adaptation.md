# Story 26.2: ActionType 扩展与统一 Prompt 模板

Status: done

## Story

As a 平台构建者,
I want 统一推理循环支持 7 种 action 类型（text/tool_call/plan/spawn/complete/replan/specialize），LLM 每步自主选择行为,
So that 智能体能力不受限于预设模式，由 LLM 根据任务复杂度智能决策。

**FRs:** FR112, FR113, FR114, FR116, FR117

## Previous Story Context

Story 26-1（已完成）是纯删除 Story：
- 删除了 `kernel/ooda.go`（531 行）及其所有类型/函数
- 删除了 OODA 测试文件、ooda-demo agent、ooda-agent testdata
- 从 Process 结构体中删除了 `oodaEnabled`/`oodaState` 字段和方法
- 统一 Spawn 入口为 `k.reasonStep(proc, llmFD, opts)`，删除了 `SpawnOpts.ReasoningMode`
- `agents/loader.go` reasoning 验证仅接受 `""` 和 `"linear"`
- 删除了 `types.LogOODA` 常量
- 删除了 `lib/agents/stem/agent.yaml` 中的 `reasoning: ooda`

**当前代码状态**（post 26-1）：
- `ActionType` 仅有 3 种：`text`, `tool_call`, `spawn`（`spawn` 常量存在但无处理分支）
- `parseAction` 仅识别 `tool_call`，其余按 `ActionText` 处理
- `reasonStep` switch 仅有 `ActionText` 和 `ActionToolCall` 两个 case
- `linearToolProtocol` 常量名仍保留 "linear" 前缀
- `AgentManifest.Reasoning string` 字段仍存在（验证仅允许 `""` 和 `"linear"`）
- `lib/agents/stem/agent.yaml` 无 `reasoning` 字段

## Acceptance Criteria (AC)

### AC-1: ActionType 常量扩展
**Given** `kernel/kernel.go` 中当前 `ActionType` 定义为 3 种（text/tool_call/spawn）
**When** 扩展 ActionType 常量
**Then** 包含以下 7 种类型：
```go
const (
    ActionText       ActionType = "text"
    ActionToolCall   ActionType = "tool_call"
    ActionPlan       ActionType = "plan"
    ActionSpawn      ActionType = "spawn"
    ActionComplete   ActionType = "complete"
    ActionReplan     ActionType = "replan"
    ActionSpecialize ActionType = "specialize"
)
```

### AC-2: 统一 Prompt 模板——toolProtocol 重命名
**Given** `kernel/kernel.go` 中 `linearToolProtocol` 常量（第 55-71 行）
**When** 重命名并扩展模板
**Then** 常量名改为 `toolProtocol`（删除 "linear" 前缀）
**And** 保留现有 tool_call 协议内容不变
**And** 新增 `spawn`/`complete`/`replan`/`specialize` 的 action 格式说明
**And** 所有引用 `linearToolProtocol` 的位置（第 973 行）改为 `toolProtocol`

### AC-3: planProtocol 模板
**Given** 需要在 planning 启用时注入额外的 plan 格式说明
**When** 定义新常量 `planProtocol`
**Then** 包含 plan action 格式说明和使用指南
**And** 格式：`{"action": "plan", "tool": "", "data": {"steps": ["step1", "step2", ...], "reason": "..."}}`

### AC-4: AgentManifest Reasoning → Planning
**Given** `agents/types.go` 中 `AgentManifest` 结构体有 `Reasoning string` 字段（第 27 行）
**When** 替换为 `Planning *bool`
**Then** 字段定义为 `Planning *bool \`yaml:"planning,omitempty"\``
**And** `nil` 表示未设置（等价于 `true`）
**And** `*true` 表示显式启用
**And** `*false` 表示显式禁用
**And** 删除旧的 `Reasoning string` 字段

### AC-5: Loader 验证更新
**Given** `agents/loader.go` 中 reasoning 验证逻辑（第 67-72 行）
**When** 替换为 planning 字段处理
**Then** 删除整个 reasoning 验证 switch 块
**And** `Planning *bool` 无需验证（`*bool` 天然只能是 nil/true/false，YAML 解析器处理）

### AC-6: Planning 配置——Prompt 注入
**Given** planning 配置为 true（默认：nil 等价于 true）
**When** `reasonStep` 构建 system prompt（第 973 行附近）
**Then** 在 `toolProtocol` 之后额外注入 `planProtocol` 模板

**Given** planning 配置为 false
**When** `reasonStep` 构建 system prompt
**Then** 仅注入 `toolProtocol`，不注入 `planProtocol`

### AC-7: parseAction 扩展
**Given** `kernel/kernel.go` 中 `parseAction` 函数（第 1346-1367 行）
**When** 扩展 JSON 解析逻辑
**Then** 根据 `action` 字段值分派到对应 ActionType
**And** 支持 `"plan"`, `"spawn"`, `"complete"`, `"replan"`, `"specialize"` 的解析
**And** 无法解析为 JSON 时按 `ActionText` 处理（兼容纯文本最终答案）
**And** 对于 `plan`/`spawn`/`complete`/`replan`/`specialize`，使用 `ToolData`（`json.RawMessage`）携带 data 部分
**And** 对于 `spawn`/`specialize`，使用 `ToolPath` 携带 `tool` 字段值（intent/skill-name）

### AC-8: ActionPlan 处理
**Given** `reasonStep` 中 LLM 返回 `ActionPlan`
**When** planning 为 true（默认）
**Then** 以 `RoleAssistant` 将 plan 内容写入上下文（格式：`[Plan]\n{steps JSON}`）
**And** 继续下一步循环

**Given** planning 为 false 且 LLM 返回 `ActionPlan`
**When** 解析 action
**Then** 按 `ActionText` 处理——将 plan 内容作为最终输出文本

### AC-9: ActionSpawn 处理
**Given** `reasonStep` 中 LLM 返回 `ActionSpawn`
**When** 解析 spawn action
**Then** 从 `action.ToolData` 解析可选的 `{"agent": "name", "model": "model"}` 参数
**And** 通过 `k.agentLoader` 加载 agent（如指定）
**And** 调用 `k.Spawn(intent, agentInfo, childOpts)` 创建子进程
**And** 等待子进程完成，结果以 tool message 写入上下文
**And** 支持 TraceID/SpanID 传播
**And** 支持父进程 context 取消检查

### AC-10: ActionComplete 处理
**Given** `reasonStep` 中 LLM 返回 `ActionComplete`
**When** 解析 complete action
**Then** 从 `action.ToolData` 或 `action.Content` 提取 result 内容
**And** 设置 `proc.Result`
**And** 调用 `k.finishProcess(proc, ExitStatus{Code: 0, Reason: "completed"})`
**And** 循环退出（return）

### AC-11: ActionReplan 处理
**Given** `reasonStep` 中 LLM 返回 `ActionReplan`
**When** 解析 replan action
**Then** 以 `RoleAssistant` 将 replan 原因写入上下文（格式：`[Replan] {reason}`）
**And** 继续下一步循环

### AC-12: stem agent 配置更新
**Given** `lib/agents/stem/agent.yaml` 当前无 planning 配置
**When** 可选添加 planning
**Then** 保持默认（不添加 planning 字段，nil 等价于 true）
**Or** 显式添加 `planning: true`

### AC-13: 编译和静态分析通过
**Given** 所有修改完成
**When** 运行 `go build ./cmd/rnix/`
**Then** 编译成功，零错误
**And** 运行 `go vet ./...` 无警告

## Tasks / Subtasks

### Task 1: ActionType 常量扩展 [AC-1]

修改 `kernel/kernel.go` 第 76-80 行。

**当前代码：**
```go
const (
    ActionText     ActionType = "text"
    ActionToolCall ActionType = "tool_call"
    ActionSpawn    ActionType = "spawn"
)
```

**修改为：**
```go
const (
    ActionText       ActionType = "text"
    ActionToolCall   ActionType = "tool_call"
    ActionPlan       ActionType = "plan"
    ActionSpawn      ActionType = "spawn"
    ActionComplete   ActionType = "complete"
    ActionReplan     ActionType = "replan"
    ActionSpecialize ActionType = "specialize"
)
```

### Task 2: 重命名 linearToolProtocol → toolProtocol 并扩展 [AC-2, AC-3]

#### 2a. 重命名常量

修改 `kernel/kernel.go` 第 55-71 行。

**当前代码（第 55-71 行）：**
```go
// linearToolProtocol is injected into the system prompt for linear-mode processes,
// telling the LLM how to invoke VFS devices via structured JSON actions.
const linearToolProtocol = `

[Tool Call Protocol]
...
If no tool call is needed, respond with plain text (your final answer).`
```

**修改为：**
```go
// toolProtocol is injected into the system prompt, telling the LLM how to
// invoke VFS devices and other actions via structured JSON.
const toolProtocol = `

[Action Protocol]
Respond with a JSON object to perform an action, or plain text for your final answer.

Tool call — execute a VFS device:
{"action": "tool_call", "tool": "<vfs-device-path>", "data": {<tool-specific-payload>}}

Available VFS device paths:
  - Read file: tool="/dev/fs/path/to/file", data={}
  - Write file: tool="/dev/fs/path/to/file", data={"content": "..."}
  - List directory: tool="/dev/fs/path/to/dir", data={"op": "list"}
  - Run command: tool="/dev/shell", data={"command": "..."}
  - LLM call: tool="/dev/llm/<provider>", data={"intent": "..."}
  - MCP tool: tool="/dev/mcp/<server>/<tool>", data={...}

Spawn child process:
{"action": "spawn", "tool": "<child intent>", "data": {"agent": "<name>", "model": "<model>"}}

Complete — finish with a result:
{"action": "complete", "tool": "", "data": {"result": "<final output>"}}

Replan — revise your approach:
{"action": "replan", "tool": "", "data": {"reason": "<why replanning>"}}

Specialize — dynamically load a skill:
{"action": "specialize", "tool": "<skill-name>", "data": {}}

If no action is needed, respond with plain text (your final answer).`
```

#### 2b. 定义 planProtocol 常量

在 `toolProtocol` 之后新增常量：

```go
// planProtocol is appended after toolProtocol when planning is enabled,
// giving the LLM the ability to create execution plans before acting.
const planProtocol = `

Plan — create an execution plan before acting:
{"action": "plan", "tool": "", "data": {"steps": ["step1", "step2", ...], "reason": "why planning"}}

Use planning when the task requires multiple coordinated steps. For simple tasks, use tool_call directly.`
```

#### 2c. 更新引用

修改 `kernel/kernel.go` 第 973 行：

**当前代码：**
```go
sysPrompt += linearToolProtocol
```

**修改为：**
```go
sysPrompt += toolProtocol
```

### Task 3: Planning 配置注入 [AC-6]

#### 3a. 在 Process 中存储 Planning 状态

修改 `kernel/process.go`，在 Process 结构体中添加字段：

在 `Model string` 字段之后（第 103 行附近），添加：
```go
PlanningEnabled bool   // true = inject planProtocol; derived from agent manifest Planning field
```

并在 `NewProcess` 函数中将其默认设置为 `true`：
```go
PlanningEnabled: true,
```

#### 3b. 在 Spawn 中从 AgentManifest 读取 Planning

修改 `kernel/kernel.go` Spawn 方法中 agent 配置读取部分。

在 `// Model selection priority` 块之前（第 350 行附近），插入：
```go
// Planning configuration: nil (default) = true, explicit false = disabled
if agent.Manifest.Planning != nil && !*agent.Manifest.Planning {
    proc.PlanningEnabled = false
}
```

#### 3c. 在 reasonStep 中条件注入 planProtocol

修改 `kernel/kernel.go` 第 973 行附近，当前代码：
```go
sysPrompt += linearToolProtocol
```

修改为：
```go
sysPrompt += toolProtocol
if proc.PlanningEnabled {
    sysPrompt += planProtocol
}
```

### Task 4: AgentManifest Reasoning → Planning [AC-4]

修改 `agents/types.go` 第 27 行。

**当前代码：**
```go
Reasoning     string      `yaml:"reasoning,omitempty"` // "" = linear (default)
```

**修改为：**
```go
Planning      *bool       `yaml:"planning,omitempty"` // nil = not set (true), *true = enabled, *false = disabled
```

### Task 5: Loader 验证更新 [AC-5]

修改 `agents/loader.go` 第 67-72 行。

**当前代码：**
```go
// Validate reasoning mode
switch manifest.Reasoning {
case "", "linear":
    // valid
default:
    return nil, fmt.Errorf("invalid reasoning mode %q: must be empty or \"linear\"", manifest.Reasoning)
}
```

**修改为（删除整个块）：**
删除这 6 行。`Planning *bool` 无需运行时验证——YAML 反序列化自动处理。

### Task 6: parseAction 扩展 [AC-7]

修改 `kernel/kernel.go` 中 `parseAction` 函数（第 1346-1367 行）。

**当前代码：**
```go
func parseAction(resp *llmResponse) ReasonAction {
    var structured struct {
        Action  string         `json:"action"`
        Content string         `json:"content,omitempty"`
        Tool    string         `json:"tool,omitempty"`
        Data    map[string]any `json:"data,omitempty"`
    }
    if err := json.Unmarshal([]byte(resp.Content), &structured); err == nil {
        if structured.Action == "tool_call" && structured.Tool != "" {
            toolData, _ := json.Marshal(structured.Data)
            return ReasonAction{
                Type:     ActionToolCall,
                ToolPath: structured.Tool,
                ToolData: toolData,
            }
        }
    }

    return ReasonAction{Type: ActionText, Content: resp.Content}
}
```

**修改为：**
```go
func parseAction(resp *llmResponse) ReasonAction {
    var structured struct {
        Action  string          `json:"action"`
        Content string          `json:"content,omitempty"`
        Tool    string          `json:"tool,omitempty"`
        Data    json.RawMessage `json:"data,omitempty"`
    }
    if err := json.Unmarshal([]byte(resp.Content), &structured); err == nil {
        switch ActionType(structured.Action) {
        case ActionToolCall:
            if structured.Tool != "" {
                toolData := structured.Data
                if toolData == nil {
                    toolData = []byte("{}")
                }
                return ReasonAction{
                    Type:     ActionToolCall,
                    ToolPath: structured.Tool,
                    ToolData: toolData,
                }
            }
        case ActionPlan:
            toolData := structured.Data
            if toolData == nil {
                toolData = []byte("{}")
            }
            return ReasonAction{
                Type:     ActionPlan,
                Content:  resp.Content,
                ToolData: toolData,
            }
        case ActionSpawn:
            toolData := structured.Data
            if toolData == nil {
                toolData = []byte("{}")
            }
            return ReasonAction{
                Type:     ActionSpawn,
                Content:  resp.Content,
                ToolPath: structured.Tool, // intent text
                ToolData: toolData,        // {"agent": "...", "model": "..."}
            }
        case ActionComplete:
            toolData := structured.Data
            if toolData == nil {
                toolData = []byte("{}")
            }
            return ReasonAction{
                Type:     ActionComplete,
                Content:  resp.Content,
                ToolData: toolData, // {"result": "..."}
            }
        case ActionReplan:
            toolData := structured.Data
            if toolData == nil {
                toolData = []byte("{}")
            }
            return ReasonAction{
                Type:    ActionReplan,
                Content: resp.Content,
                ToolData: toolData, // {"reason": "..."}
            }
        case ActionSpecialize:
            return ReasonAction{
                Type:     ActionSpecialize,
                ToolPath: structured.Tool, // skill name
                Content:  resp.Content,
            }
        }
    }

    return ReasonAction{Type: ActionText, Content: resp.Content}
}
```

**关键设计决策：**
- `Data` 字段类型从 `map[string]any` 改为 `json.RawMessage`，避免双重 Marshal/Unmarshal
- `ActionSpawn` 的 `ToolPath` 携带 intent（`tool` 字段），`ToolData` 携带 `{"agent": "...", "model": "..."}`
- `ActionSpecialize` 的 `ToolPath` 携带 skill name
- `ActionComplete` 的 `ToolData` 携带 `{"result": "..."}`
- `ActionReplan` 的 `ToolData` 携带 `{"reason": "..."}`
- 无法识别的 action 或非 JSON 内容，一律按 `ActionText` 回退

### Task 7: reasonStep switch 扩展 [AC-8, AC-9, AC-10, AC-11]

修改 `kernel/kernel.go` 中 `reasonStep` 函数的 switch 语句（第 1155-1338 行）。

在现有的 `case ActionToolCall:` 块结束后（第 1337 行 `continue` 之后），`}` 之前，新增以下 case 分支：

#### 7a. ActionPlan 处理 [AC-8]

```go
case ActionPlan:
    // If planning is disabled, treat as final text output
    if !proc.PlanningEnabled {
        k.emitLog(proc, step, types.LogOutput, action.Content, "")
        proc.mu.Lock()
        proc.Result = action.Content
        proc.mu.Unlock()
        k.emitEvent(proc, "ReasonStep", map[string]any{
            "step":   step,
            "action": "plan_as_text",
        }, action.Content, nil, time.Since(stepStart))
        k.finishProcess(proc, ExitStatus{Code: 0, Reason: "completed"})
        return
    }

    // Format plan content for context
    planContent := fmt.Sprintf("[Plan]\n%s", string(action.ToolData))

    appendStart := time.Now()
    if err := k.ctxMgr.AppendMessage(proc.CtxID, rnixctx.RoleAssistant, planContent); err != nil {
        k.emitEvent(proc, "CtxWrite", map[string]any{
            "cid":  proc.CtxID,
            "op":   "AppendMessage",
            "role": string(rnixctx.RoleAssistant),
        }, nil, err, time.Since(appendStart))
        k.finishProcess(proc, ExitStatus{Code: 1, Reason: "append plan failed", Err: err})
        return
    }
    k.emitEvent(proc, "CtxWrite", map[string]any{
        "cid":  proc.CtxID,
        "op":   "AppendMessage",
        "role": string(rnixctx.RoleAssistant),
    }, nil, nil, time.Since(appendStart))

    k.emitLog(proc, step, types.LogOutput, planContent, "")
    k.emitEvent(proc, "ReasonStep", map[string]any{
        "step":   step,
        "action": "plan",
    }, nil, nil, time.Since(stepStart))
    continue
```

#### 7b. ActionSpawn 处理 [AC-9]

需要一个辅助类型来解析 spawn data：

在 `kernel/kernel.go` 中 `ReasonAction` 结构体定义之后，新增：

```go
// spawnActionData contains optional parameters parsed from spawn action data.
type spawnActionData struct {
    Agent string `json:"agent,omitempty"`
    Model string `json:"model,omitempty"`
}
```

然后在 switch 中新增 case：

```go
case ActionSpawn:
    // Append LLM response as assistant message
    appendAssistantStart := time.Now()
    if err := k.ctxMgr.AppendMessage(proc.CtxID, rnixctx.RoleAssistant, resp.Content); err != nil {
        k.emitEvent(proc, "CtxWrite", map[string]any{
            "cid":  proc.CtxID,
            "op":   "AppendMessage",
            "role": string(rnixctx.RoleAssistant),
        }, nil, err, time.Since(appendAssistantStart))
        k.finishProcess(proc, ExitStatus{Code: 1, Reason: "append assistant message failed", Err: err})
        return
    }
    k.emitEvent(proc, "CtxWrite", map[string]any{
        "cid":  proc.CtxID,
        "op":   "AppendMessage",
        "role": string(rnixctx.RoleAssistant),
    }, nil, nil, time.Since(appendAssistantStart))

    // Build child spawn options with trace propagation
    childOpts := SpawnOpts{
        ParentPID: proc.PID,
        TraceID:   proc.TraceID,
    }
    if proc.TraceID != "" {
        childOpts.ParentSpanID = proc.SpanID
    }

    // Parse optional spawn data
    var sd spawnActionData
    if len(action.ToolData) > 0 {
        _ = json.Unmarshal(action.ToolData, &sd)
    }
    if sd.Model != "" {
        childOpts.Model = sd.Model
    }

    // Load agent if specified
    var agentInfo *agents.AgentInfo
    spawnIntent := action.ToolPath // "tool" field carries intent text
    if sd.Agent != "" {
        if k.agentLoader == nil {
            errMsg := fmt.Sprintf("spawn error: agent %q requested but no agent loader configured", sd.Agent)
            _ = k.ctxMgr.AppendToolResult(proc.CtxID, "spawn", errMsg)
            k.emitLog(proc, step, types.LogTool, errMsg, "spawn")
            k.emitEvent(proc, "ReasonStep", map[string]any{
                "step":   step,
                "action": "spawn_error",
            }, nil, fmt.Errorf("%s", errMsg), time.Since(stepStart))
            continue
        }
        var loadErr error
        agentInfo, loadErr = k.agentLoader(sd.Agent)
        if loadErr != nil {
            errMsg := fmt.Sprintf("spawn error: agent %q load failed: %v", sd.Agent, loadErr)
            _ = k.ctxMgr.AppendToolResult(proc.CtxID, "spawn", errMsg)
            k.emitLog(proc, step, types.LogTool, errMsg, "spawn")
            k.emitEvent(proc, "ReasonStep", map[string]any{
                "step":   step,
                "action": "spawn_error",
            }, nil, loadErr, time.Since(stepStart))
            continue
        }
    }

    // Spawn child process
    childPID, spawnErr := k.Spawn(spawnIntent, agentInfo, childOpts)
    if spawnErr != nil {
        errMsg := fmt.Sprintf("spawn error: %v", spawnErr)
        _ = k.ctxMgr.AppendToolResult(proc.CtxID, "spawn", errMsg)
        k.emitLog(proc, step, types.LogTool, errMsg, "spawn")
        k.emitEvent(proc, "ReasonStep", map[string]any{
            "step":   step,
            "action": "spawn_error",
        }, nil, spawnErr, time.Since(stepStart))
        continue
    }

    // Wait for child completion or parent cancellation
    childProc, childOk := k.GetProcess(childPID)
    if !childOk {
        errMsg := "spawn error: child process not found after spawn"
        _ = k.ctxMgr.AppendToolResult(proc.CtxID, "spawn", errMsg)
        k.emitLog(proc, step, types.LogTool, errMsg, "spawn")
        continue
    }

    var spawnResult string
    select {
    case exit := <-childProc.Done:
        childProc.mu.Lock()
        childResult := childProc.Result
        childProc.mu.Unlock()
        if exit.Code != 0 {
            spawnResult = fmt.Sprintf("child exited with code %d: %s", exit.Code, exit.Reason)
        } else {
            spawnResult = childResult
        }
    case <-proc.ctx.Done():
        k.emitEvent(proc, "ReasonStep", map[string]any{
            "step":   step,
            "action": "cancelled_waiting_child",
        }, nil, proc.ctx.Err(), time.Since(stepStart))
        k.finishProcess(proc, ExitStatus{Code: 1, Reason: "context cancelled while waiting for child"})
        return
    }

    // Write child result to context as tool result
    appendResultStart := time.Now()
    if err := k.ctxMgr.AppendToolResult(proc.CtxID, "spawn", spawnResult); err != nil {
        k.emitEvent(proc, "CtxWrite", map[string]any{
            "cid":  proc.CtxID,
            "op":   "AppendToolResult",
            "tool": "spawn",
        }, nil, err, time.Since(appendResultStart))
        k.finishProcess(proc, ExitStatus{Code: 1, Reason: "append spawn result failed", Err: err})
        return
    }
    k.emitEvent(proc, "CtxWrite", map[string]any{
        "cid":  proc.CtxID,
        "op":   "AppendToolResult",
        "tool": "spawn",
    }, nil, nil, time.Since(appendResultStart))

    k.emitLog(proc, step, types.LogTool, spawnResult, "spawn")
    k.emitEvent(proc, "ReasonStep", map[string]any{
        "step":      step,
        "action":    "spawn",
        "child_pid": childPID,
    }, spawnResult, nil, time.Since(stepStart))
    continue
```

#### 7c. ActionComplete 处理 [AC-10]

```go
case ActionComplete:
    // Parse result from data payload
    var completeData struct {
        Result string `json:"result"`
    }
    if len(action.ToolData) > 0 {
        _ = json.Unmarshal(action.ToolData, &completeData)
    }
    result := completeData.Result
    if result == "" {
        result = action.Content
    }

    k.emitLog(proc, step, types.LogOutput, result, "")

    proc.mu.Lock()
    proc.Result = result
    proc.mu.Unlock()

    k.emitEvent(proc, "ReasonStep", map[string]any{
        "step":   step,
        "action": "complete",
    }, result, nil, time.Since(stepStart))
    k.finishProcess(proc, ExitStatus{Code: 0, Reason: "completed"})
    return
```

#### 7d. ActionReplan 处理 [AC-11]

```go
case ActionReplan:
    // Parse reason from data payload
    var replanData struct {
        Reason string `json:"reason"`
    }
    if len(action.ToolData) > 0 {
        _ = json.Unmarshal(action.ToolData, &replanData)
    }
    reason := replanData.Reason
    if reason == "" {
        reason = action.Content
    }

    replanContent := fmt.Sprintf("[Replan] %s", reason)

    appendStart := time.Now()
    if err := k.ctxMgr.AppendMessage(proc.CtxID, rnixctx.RoleAssistant, replanContent); err != nil {
        k.emitEvent(proc, "CtxWrite", map[string]any{
            "cid":  proc.CtxID,
            "op":   "AppendMessage",
            "role": string(rnixctx.RoleAssistant),
        }, nil, err, time.Since(appendStart))
        k.finishProcess(proc, ExitStatus{Code: 1, Reason: "append replan failed", Err: err})
        return
    }
    k.emitEvent(proc, "CtxWrite", map[string]any{
        "cid":  proc.CtxID,
        "op":   "AppendMessage",
        "role": string(rnixctx.RoleAssistant),
    }, nil, nil, time.Since(appendStart))

    k.emitLog(proc, step, types.LogOutput, replanContent, "")
    k.emitEvent(proc, "ReasonStep", map[string]any{
        "step":   step,
        "action": "replan",
    }, nil, nil, time.Since(stepStart))
    continue
```

#### 7e. ActionSpecialize 处理

**注意：** Story 26.4 负责 specialize 的完整实现（含 DiffMemory/Lineage 集成）。本 Story 中 `ActionSpecialize` 仅需**占位 case** 加一条 TODO 注释。占位实现应做最小可行处理：

```go
case ActionSpecialize:
    // Minimal placeholder — full implementation in Story 26.4
    errMsg := "specialize action not yet implemented"
    _ = k.ctxMgr.AppendToolResult(proc.CtxID, "specialize", errMsg)
    k.emitLog(proc, step, types.LogTool, errMsg, "specialize")
    k.emitEvent(proc, "ReasonStep", map[string]any{
        "step":   step,
        "action": "specialize_stub",
    }, nil, nil, time.Since(stepStart))
    continue
```

### Task 8: Loader 测试更新 [AC-5]

修改 `agents/loader_reasoning_test.go`。

**全面重写此文件**，因为所有测试都与 `Reasoning` 字段相关，现在需要测试 `Planning` 字段。

**新文件内容（替换整个文件）：**

```go
package agents

import (
    "testing"

    "github.com/rnixai/rnix/skills"
)

func TestAgentLoader_PlanningDefault(t *testing.T) {
    // Given: agent.yaml without planning field
    // When: loading mock-agent
    // Then: Planning should be nil (equivalent to true)
    sl := skills.NewSkillLoader([]string{"../skills/testdata"})
    al := NewAgentLoader([]string{"testdata"}, sl, nil)

    info, err := al.Load("mock-agent")
    if err != nil {
        t.Fatalf("Load returned error: %v", err)
    }

    if info.Manifest.Planning != nil {
        t.Errorf("Planning = %v, want nil (default = enabled)", *info.Manifest.Planning)
    }
}
```

**注意：** `TestAgentLoader_InvalidReasoningMode` 引用 `agents/testdata/invalid-reasoning/` 测试数据。这个目录中的 `agent.yaml` 有 `reasoning: bogus`。删除 reasoning 验证后，此测试数据不再触发错误，但 `reasoning` 字段会被忽略（它在 YAML 解析中不再映射到任何结构体字段）。此测试应删除。

`TestAgentLoader_LinearReasoningMode` 也应删除（不再有 reasoning 字段）。

### Task 9: 更新 stem agent 配置 [AC-12]

`lib/agents/stem/agent.yaml` 当前内容：

```yaml
name: stem
description: "通用基底智能体 -- 根据意图自动匹配 Skill 完成分化"
models:
  provider: claude
  preferred: sonnet
context_budget: 16384
skills: []
```

**无需修改**——不添加 `planning` 字段（nil 等价于 true，这是所需的默认行为）。

### Task 10: 编译验证 [AC-13]

```bash
go build ./cmd/rnix/
go vet ./...
go test -count=1 ./kernel/... ./agents/...
```

## Dev Notes

### 架构约束

- **此 Story 不实现 specialize 完整逻辑**——specialize 在 reasonStep 中仅做占位处理（Story 26.4 负责完整迁移含 DiffMemory/Lineage 集成）
- **parseAction 中的 Data 字段类型变更**：从 `map[string]any` 改为 `json.RawMessage`。这避免了双重 Marshal/Unmarshal，且允许 spawn/complete/replan 的 data 内容延迟解析
- **Plan 写入 context 的 role 必须是 `RoleAssistant`**——plan 是 LLM 自己生成的输出，不是用户输入
- **Replan 也使用 `RoleAssistant`**——同理
- **Planning 默认启用**——`*bool` 的 nil 值等价于 true，这是通过 `PlanningEnabled: true` 在 `NewProcess` 中设置的
- **toolProtocol 保留向后兼容**——现有 tool_call 的 JSON 格式不变，LLM 已经学会的 tool call 模式继续有效
- **Spawn 逻辑完全从 `oodaActSpawn` 迁移**——包含 TraceID/SpanID 传播、parent context 取消、agent 加载
- **Complete action 的 result 来源优先级**：`data.result` > `action.Content`（确保 JSON 和纯文本模式都能工作）

### Planning 配置传播路径

```
agent.yaml (planning: true/false/omitted)
    ↓ loader.go 解析
AgentManifest.Planning (*bool: nil/true/false)
    ↓ Spawn 方法读取
proc.PlanningEnabled (bool: true/false, default true)
    ↓ reasonStep 使用
if proc.PlanningEnabled { sysPrompt += planProtocol }
    ↓ parseAction 使用
case ActionPlan: if !proc.PlanningEnabled { treat as text }
```

### Spawn 子进程等待模型

```
Parent reasonStep
    ↓ k.Spawn(intent, agent, childOpts)
    ↓ childPID created
    ↓ select {
    │     case exit := <-childProc.Done:
    │         // child completed, write result to parent context
    │     case <-proc.ctx.Done():
    │         // parent cancelled, terminate
    │  }
```

注意：`childProc.Done` 是 buffered channel (cap=1)，保证不会阻塞 child 的 finishProcess。

### JSON 格式示例

#### tool_call（不变）
```json
{"action": "tool_call", "tool": "/dev/shell", "data": {"command": "ls -la"}}
```

#### plan
```json
{"action": "plan", "tool": "", "data": {"steps": ["分析需求", "编写代码", "运行测试"], "reason": "任务较复杂需要先规划"}}
```

#### spawn
```json
{"action": "spawn", "tool": "分析代码库结构", "data": {"agent": "code-analyst", "model": "haiku"}}
```

#### complete
```json
{"action": "complete", "tool": "", "data": {"result": "任务已完成，所有测试通过。"}}
```

#### replan
```json
{"action": "replan", "tool": "", "data": {"reason": "第一步执行失败，需要换一个方法"}}
```

#### specialize
```json
{"action": "specialize", "tool": "code-analyst", "data": {}}
```

### 组合矩阵

| 功能交互 | 影响 | 需验证 |
|----------|------|--------|
| ActionSpawn + TraceID | SpanID 传播到子进程 | 是（检查 childOpts 包含 TraceID/ParentSpanID） |
| ActionPlan + planning=false | 按 ActionText 处理 | 是（proc.PlanningEnabled 在 parseAction 后检查） |
| ActionComplete + token budget | budget_exceeded 与 complete 哪个先到 | 否（budget 检查在 action 解析前，complete 在后） |
| ActionSpawn + context cancel | 父进程取消时 child 孤儿处理 | 是（select 中 proc.ctx.Done 分支） |
| ActionReplan + max steps | replan 消耗步数 | 是（每次 replan 是一个 step，计入 maxSteps） |
| toolProtocol 改名 + 现有测试 | 测试中直接引用常量名的地方 | 是（grep 确认无外部引用） |

### 文件修改清单

| 文件 | 变更类型 | 说明 |
|------|----------|------|
| `kernel/kernel.go` | 修改 | ActionType 常量、toolProtocol/planProtocol、parseAction、reasonStep switch |
| `kernel/process.go` | 修改 | 新增 `PlanningEnabled bool` 字段 |
| `agents/types.go` | 修改 | `Reasoning string` → `Planning *bool` |
| `agents/loader.go` | 修改 | 删除 reasoning 验证 switch |
| `agents/loader_reasoning_test.go` | 重写 | Reasoning 测试 → Planning 测试 |

### 不修改的文件（确认）

| 文件 | 原因 |
|------|------|
| `lib/agents/stem/agent.yaml` | 无 planning 字段 = nil = true，无需修改 |
| `internal/types/types.go` | 本 Story 不新增类型 |
| `cmd/rnix/lineage.go` | Story 26.4 更新 |
| `kernel/diffmemory_*.go` | Story 26.4 更新 |
| `kernel/lineage_*.go` | Story 26.4 更新 |

### 执行顺序建议

1. Task 1: ActionType 常量扩展
2. Task 2: toolProtocol 重命名 + planProtocol 定义
3. Task 4: AgentManifest Reasoning → Planning
4. Task 5: Loader 验证删除
5. Task 3: Process.PlanningEnabled + Spawn 读取 + reasonStep 注入
6. Task 6: parseAction 扩展
7. Task 7: reasonStep switch 扩展（plan → spawn → complete → replan → specialize stub）
8. Task 8: 测试更新
9. Task 10: 编译验证

### 从 oodaActSpawn 迁移的关键细节

原 `oodaActSpawn`（已在 26-1 中删除，代码来自 git history `HEAD:kernel/ooda.go`）：

```go
func (k *KernelImpl) oodaActSpawn(proc *Process, decision *OODADecision, opts SpawnOpts) string {
    childOpts := SpawnOpts{
        ParentPID: proc.PID,
        TraceID:   proc.TraceID,
    }
    if proc.TraceID != "" {
        childOpts.ParentSpanID = proc.SpanID
    }
    var spawnData oodaSpawnData
    if len(decision.Data) > 0 {
        if err := json.Unmarshal(decision.Data, &spawnData); err != nil {
            return fmt.Sprintf("spawn error: invalid data payload: %v", err)
        }
    }
    if spawnData.Model != "" {
        childOpts.Model = spawnData.Model
    }
    var agentInfo *agents.AgentInfo
    if spawnData.Agent != "" {
        if k.agentLoader == nil {
            return fmt.Sprintf("spawn error: agent %q requested but no agent loader configured", spawnData.Agent)
        }
        var err error
        agentInfo, err = k.agentLoader(spawnData.Agent)
        if err != nil {
            return fmt.Sprintf("spawn error: agent %q load failed: %v", spawnData.Agent, err)
        }
    }
    pid, err := k.Spawn(decision.Target, agentInfo, childOpts)
    if err != nil {
        return fmt.Sprintf("spawn error: %v", err)
    }
    childProc, ok := k.GetProcess(pid)
    if !ok {
        return "spawn error: child process not found"
    }
    select {
    case exit := <-childProc.Done:
        childProc.mu.Lock()
        result := childProc.Result
        childProc.mu.Unlock()
        if exit.Code != 0 {
            return fmt.Sprintf("child exited with code %d: %s", exit.Code, exit.Reason)
        }
        return result
    case <-proc.ctx.Done():
        return "parent context cancelled"
    }
}
```

**迁移差异**：
1. 原函数返回 `string`，结果通过 caller 写入 context。新实现在 case 内部直接用 `AppendToolResult` 写入
2. 原函数使用 `OODADecision.Target` 作为 intent，新实现使用 `action.ToolPath`（`tool` 字段）
3. 原函数使用 `OODADecision.Data`（`json.RawMessage`），新实现使用 `action.ToolData`（同类型）
4. 新实现增加了 parent context 取消时的 `finishProcess` 调用（原实现仅返回字符串由 caller 处理）

### spawnActionData 结构体

原 `oodaSpawnData` 结构体（已删除），需要以新名称重建：

```go
type spawnActionData struct {
    Agent string `json:"agent,omitempty"`
    Model string `json:"model,omitempty"`
}
```

## References

- Epic 定义：`_bmad-output/planning-artifacts/epics/epic-26-统一推理循环-unified-reasoning-loop.md`（Story 26.2 部分）
- 前序 Story：`_bmad-output/implementation-artifacts/26-1-unified-reasonstep-and-actiontype-extension.md`
- Sprint Change Proposal：`_bmad-output/planning-artifacts/sprint-change-proposal-2026-03-18.md`
- 统一推理循环提案：`_bmad-output/planning-artifacts/unified-reasoning-loop-proposal.md`
- 项目上下文：`_bmad-output/project-context.md`
- 原 OODA 实现（git history）：`git show HEAD:kernel/ooda.go`（oodaActSpawn, oodaActSpecialize, spawnData 结构体）

## Dev Agent Record

### Agent Model Used
Claude claude-4.6-opus (Cursor Agent Mode)

### Debug Log References
N/A

### Completion Notes List
- All 13 ACs satisfied
- ActionType extended to 7 types: text, tool_call, plan, spawn, complete, replan, specialize
- `linearToolProtocol` renamed to `toolProtocol` and extended with all action formats
- New `planProtocol` constant for planning-enabled agents
- `AgentManifest.Reasoning string` replaced with `Planning *bool`
- Loader reasoning validation deleted (Planning *bool needs no validation)
- `Process.PlanningEnabled` field added with default true; Spawn reads from agent manifest
- `parseAction` rewritten with `json.RawMessage` Data field, dispatches all 7 action types
- `reasonStep` switch extended with ActionPlan, ActionSpawn, ActionComplete, ActionReplan, ActionSpecialize (stub)
- ActionPlan: writes to context as RoleAssistant; when planning=false treats as ActionText
- ActionSpawn: full migration from oodaActSpawn with TraceID propagation, parent cancel, agent loading
- ActionComplete: sets proc.Result, finishes with code=0
- ActionReplan: writes to context as RoleAssistant
- ActionSpecialize: stub placeholder for Story 26.4
- `make all` passes: lint 0 issues, all 22 packages test green, build successful
- Loader tests rewritten for Planning *bool field (nil, true, false)
- Test fixtures created: `agents/testdata/planning-true/`, `agents/testdata/planning-false/`

### File List
**Modified files:**
- `kernel/kernel.go` — ActionType constants, toolProtocol/planProtocol, parseAction rewrite, reasonStep switch (5 new action cases), spawnActionData struct, Planning config reading in Spawn
- `kernel/process.go` — Added PlanningEnabled field with default true
- `agents/types.go` — Reasoning string → Planning *bool
- `agents/loader.go` — Deleted reasoning validation switch block
- `agents/loader_reasoning_test.go` — Rewritten for Planning *bool field tests

**New files:**
- `agents/testdata/planning-true/agent.yaml` — Test fixture
- `agents/testdata/planning-true/instructions.md` — Test fixture
- `agents/testdata/planning-false/agent.yaml` — Test fixture
- `agents/testdata/planning-false/instructions.md` — Test fixture
