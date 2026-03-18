# Story 26.4: 测试重写、Bug 修复与清理

Status: ready-for-dev

## Story

As a 平台构建者,
I want 完成统一推理循环的测试矩阵覆盖，并实现遗留的 VFS flags 自动降级和熔断机制,
So that 架构变更有充分的测试保障，工具调用可靠稳定。

**FRs:** FR112-FR118（全覆盖验证）, FR115（熔断）, FR116（错误注入）

## Previous Story Context

### Story 26-1（已完成）
- 删除了 `kernel/ooda.go`（531 行）、`kernel/ooda_test.go`（819 行）、`kernel/ooda_reasoning_test.go`（650 行）
- 删除了 `lib/agents/ooda-demo/`、`agents/testdata/ooda-agent/`、`agents/testdata/invalid-reasoning/`
- 删除了 Process 的 `oodaEnabled`/`oodaState`/`IsOODA()`/`GetOODAState()`/`SetOODAPhase()`
- 统一 Spawn 入口为 `k.reasonStep(proc, llmFD, opts)`

### Story 26-2（已完成）
- ActionType 扩展到 7 种：text, tool_call, plan, spawn, complete, replan, specialize
- `parseAction` 用 `json.RawMessage` 重写，分派所有 7 种 action
- `reasonStep` switch 完整实现了 Plan, Spawn, Complete, Replan 分支
- `AgentManifest.Reasoning string` → `Planning *bool`
- `linearToolProtocol` → `toolProtocol`，新增 `planProtocol`
- `agents/loader_reasoning_test.go` 已重写为 Planning 字段测试（3 个测试函数）
- Loader reasoning 验证已删除

### Story 26-3（已完成）
- ActionSpecialize stub 替换为完整实现（~125 行，kernel.go L1597-1721）
- TOCTOU 双重检查、DiffMemory 记录、Lineage 记录、AllowedDevices 更新
- 17 个 ATDD 测试覆盖 AC-1 到 AC-8
- **Debug 经验**：ATDD 测试中设置 `proc.lineage` 与 reasonStep goroutine 存在数据竞争，引入 `gatedLLMFile` mock 阻塞首次 LLM Read 解决

### 遗留未实现项（来自 Sprint Change Proposal §4.3 Story 26.1 范围）
以下两项 bug 修复在 Story 26-1/26-2 中未被实现：
1. **VFS flags 自动降级**（原 Epic 26.3 AC-1）— `kernel.go:1274` 仍硬编码 `vfs.O_RDWR`
2. **熔断机制 consecutiveToolErrors**（原 Epic 26.3 AC-4/AC-5）— 未实现

## Acceptance Criteria (AC)

### AC-1: VFS Flags 自动降级
**Given** `kernel/kernel.go` reasonStep 中 tool_call 的 `vfs.Open` 调用（当前 L1274 硬编码 `vfs.O_RDWR`）
**When** 修复 flags 自动选择逻辑
**Then** `len(action.ToolData) == 0 || string(action.ToolData) == "{}"` 时使用 `vfs.O_RDONLY`
**And** 其他情况使用 `vfs.O_RDWR`
**And** 产生的 `Open` 事件中 `flags` 字段反映实际使用的 flag 值

### AC-2: 熔断机制（Circuit Breaker）
**Given** `reasonStep` 中新增 `consecutiveToolErrors int` 局部变量
**When** tool_call 执行成功时 `consecutiveToolErrors` 重置为 0
**And** tool_call 或 spawn 执行失败时 `consecutiveToolErrors` 递增
**And** plan/replan/specialize 失败**不计入**
**Then** 当 `consecutiveToolErrors >= 3` 时调用 `k.finishProcess(proc, ExitStatus{Code: 1, Reason: "circuit_breaker: 3 consecutive tool errors"})`
**And** 产生 `ReasonStep` 事件包含 `action: "circuit_breaker"` 和 `consecutive_errors: 3`

### AC-3: 统一推理循环测试矩阵（9 场景）
**Given** 统一推理循环的 9 个核心场景
**When** 编写 ATDD 测试文件 `kernel/atdd_26_4_unified_reasoning_test.go`
**Then** 覆盖以下测试矩阵：

| # | 场景 | 验证点 | 对应测试函数 |
|---|------|--------|-------------|
| 1 | LLM 返回 `tool_call` | 工具执行，结果以 tool message 注入上下文 | `TestUnified_ToolCall_ExecutesAndInjectsResult` |
| 2 | LLM 返回 `plan`（planning=true） | Plan 以 RoleAssistant 写入上下文，继续循环 | `TestUnified_Plan_PlanningEnabled_WritesAssistant` |
| 3 | LLM 返回 `plan`（planning=false） | 按 text 处理，plan 内容作为最终输出 | `TestUnified_Plan_PlanningDisabled_TreatsAsText` |
| 4 | LLM 返回 `complete` | 正常退出 code=0，Result 被设置 | `TestUnified_Complete_ExitsWithCodeZero` |
| 5 | LLM 返回 `spawn` | 创建子进程，等待完成，结果写入上下文 | `TestUnified_Spawn_CreatesChildAndWaitsResult` |
| 6 | LLM 返回 `specialize` | 动态加载 skill，body 注入上下文 | 已有 17 个测试 in `atdd_26_3_*` |
| 7 | 连续 3 次 tool/spawn 失败 | 熔断退出 code=1 | `TestUnified_CircuitBreaker_ThreeConsecutiveErrors` |
| 8 | tool 错误 | 错误以 role:"tool" 注入上下文 | `TestUnified_ToolError_InjectsToContext` |
| 9 | /dev/fs 读取（空 payload） | flags 自动降级为 O_RDONLY | `TestUnified_VFSFlags_EmptyPayload_UsesReadOnly` |

### AC-4: 熔断重置验证
**Given** reasonStep 中 tool_call 连续失败 2 次后成功 1 次
**When** `consecutiveToolErrors` 重置为 0
**Then** 进程继续正常运行，不触发熔断
**And** 对应测试：`TestUnified_CircuitBreaker_ResetsOnSuccess`

### AC-5: Spawn 失败计入熔断
**Given** reasonStep 中 spawn action 执行失败
**When** `consecutiveToolErrors` 递增
**Then** spawn 失败与 tool_call 失败共用同一计数器
**And** 对应测试：`TestUnified_CircuitBreaker_SpawnFailureCounts`

### AC-6: Specialize 失败不计入熔断
**Given** reasonStep 中 specialize action 失败（skill 不存在）
**When** 检查 `consecutiveToolErrors`
**Then** 计数器不递增（specialize 是可恢复的逻辑错误）
**And** 对应测试：`TestUnified_CircuitBreaker_SpecializeErrorIgnored`

### AC-7: VFS Flags 写入场景
**Given** tool_call 的 payload 非空（如 `{"content": "hello"}`）
**When** 执行 `vfs.Open`
**Then** 使用 `vfs.O_RDWR`
**And** 对应测试：`TestUnified_VFSFlags_NonEmptyPayload_UsesReadWrite`

### AC-8: 编译和测试通过
**Given** 所有修改完成
**When** 运行 `make all`
**Then** lint + vet + test + build 全部通过
**And** 所有 Go 包测试通过（`-race` 检测）

## Tasks / Subtasks

### Task 1: 实现 VFS Flags 自动降级 [AC-1, AC-7]

修改 `kernel/kernel.go`，在 reasonStep 的 `case ActionToolCall:` 中，找到当前 tool device Open 调用（约 L1272-1278）：

**当前代码（L1272-1278）：**
```go
// Open tool device
toolOpenStart := time.Now()
toolFD, err := k.vfs.Open(proc.PID, action.ToolPath, vfs.O_RDWR)
k.emitEvent(proc, "Open", map[string]any{
    "path":  action.ToolPath,
    "flags": vfs.O_RDWR,
}, toolFD, err, time.Since(toolOpenStart))
```

**替换为：**
```go
// Open tool device with auto-downgraded flags
toolOpenStart := time.Now()
isEmpty := len(action.ToolData) == 0 || string(action.ToolData) == "{}"
openFlags := vfs.O_RDWR
if isEmpty {
    openFlags = vfs.O_RDONLY
}
toolFD, err := k.vfs.Open(proc.PID, action.ToolPath, openFlags)
k.emitEvent(proc, "Open", map[string]any{
    "path":  action.ToolPath,
    "flags": openFlags,
}, toolFD, err, time.Since(toolOpenStart))
```

### Task 2: 实现熔断机制 [AC-2, AC-4, AC-5, AC-6]

修改 `kernel/kernel.go`，在 `reasonStep` 函数中：

**2a.** 在 `for step := 1; step <= maxSteps; step++` 循环之前声明计数器：
```go
var consecutiveToolErrors int
```

**2b.** 在 `case ActionToolCall:` 的**成功路径**末尾（写入 tool result 后、`continue` 前）添加重置：
```go
consecutiveToolErrors = 0
```

**2c.** 在 `case ActionToolCall:` 的**所有错误路径**（Open 失败、Write 失败、Read 失败）中添加递增和熔断检查：
```go
consecutiveToolErrors++
if consecutiveToolErrors >= 3 {
    k.emitEvent(proc, "ReasonStep", map[string]any{
        "step":              step,
        "action":            "circuit_breaker",
        "consecutive_errors": consecutiveToolErrors,
    }, nil, nil, time.Since(stepStart))
    k.finishProcess(proc, ExitStatus{Code: 1, Reason: "circuit_breaker: 3 consecutive tool errors"})
    return
}
```

**2d.** 在 `case ActionSpawn:` 的 **spawn 失败路径**（Spawn 调用返回 error）中添加同样的递增和熔断检查。

**2e.** 在 `case ActionSpawn:` 的**成功路径**末尾添加重置：
```go
consecutiveToolErrors = 0
```

**注意：** `case ActionSpecialize:`、`case ActionPlan:`、`case ActionReplan:` 的错误路径**不**递增计数器。

### Task 3: 编写统一推理循环测试矩阵 [AC-3, AC-4, AC-5, AC-6]

新建文件 `kernel/atdd_26_4_unified_reasoning_test.go`。

使用现有测试基础设施：
- `mockLLMFile`：控制 LLM 响应（设置 `readData`）
- `mockToolFile`：模拟工具设备
- `newTestKernel(t, llmFile)`：创建测试 kernel + VFS + CtxMgr
- `makeLLMResponse(content, tokens)`：构建 LLM JSON 响应
- `makeToolCallResponse(toolPath, toolData, tokens)`：构建 tool_call 响应

**关键测试模式（来自 Story 26-3 经验）：**

当需要在 Spawn 后、reasonStep goroutine 运行前设置 proc 字段时，使用 `gatedLLMFile` mock（阻塞首次 Read，释放后才继续）。参考 `atdd_26_3_specialize_migration_test.go` 中的实现。

**构建 action 响应的辅助函数建议：**

```go
func makePlanResponse(steps []string, reason string, tokens int) []byte {
    action := map[string]any{
        "action": "plan",
        "tool":   "",
        "data":   map[string]any{"steps": steps, "reason": reason},
    }
    content, _ := json.Marshal(action)
    return makeLLMResponse(string(content), tokens)
}

func makeCompleteResponse(result string, tokens int) []byte {
    action := map[string]any{
        "action": "complete",
        "tool":   "",
        "data":   map[string]any{"result": result},
    }
    content, _ := json.Marshal(action)
    return makeLLMResponse(string(content), tokens)
}

func makeSpawnResponse(intent string, agent string, tokens int) []byte {
    action := map[string]any{
        "action": "spawn",
        "tool":   intent,
        "data":   map[string]any{"agent": agent},
    }
    content, _ := json.Marshal(action)
    return makeLLMResponse(string(content), tokens)
}

func makeReplanResponse(reason string, tokens int) []byte {
    action := map[string]any{
        "action": "replan",
        "tool":   "",
        "data":   map[string]any{"reason": reason},
    }
    content, _ := json.Marshal(action)
    return makeLLMResponse(string(content), tokens)
}
```

**测试场景实现要点：**

#### 场景 1: tool_call 执行并注入结果
- Mock LLM 返回 tool_call → complete 序列
- 注册 mock tool device 到 VFS
- 验证 tool result 通过 `AppendToolResult` 注入上下文
- 验证进程正常完成

#### 场景 2: plan (planning=true) 写入 RoleAssistant
- Mock LLM 第一步返回 plan，第二步返回 complete
- Spawn 时设置 `PlanningEnabled: true`
- 验证 context 中有 `[Plan]\n{steps JSON}` 内容（RoleAssistant）
- 验证进程继续循环并完成

#### 场景 3: plan (planning=false) 当作 text
- 设置 `proc.PlanningEnabled = false`（需在 Spawn 后设置，使用 gatedLLMFile）
- Mock LLM 返回 plan action
- 验证进程以 code=0 退出，Result 包含 plan 内容

#### 场景 4: complete 正常退出
- Mock LLM 返回 complete action（含 result 数据）
- 验证 exit code=0，reason="completed"
- 验证 `proc.Result` 被设置为 complete 的 result

#### 场景 5: spawn 创建子进程
- Mock LLM 返回 spawn action（不指定 agent，使用默认）
- 子进程的 LLM mock 返回 complete
- 验证子进程创建成功
- 验证父进程收到子进程结果

**注意**：Spawn 测试较复杂，需要 agentLoader 或直接测试无 agent 的 spawn。spawn 不指定 agent 时，使用 intent 作为系统指令。测试时需确保子进程的 LLM 也有 mock 响应。

#### 场景 7: 熔断（3 次连续错误）
- Mock LLM 连续返回 3 个 tool_call，全部指向不存在的设备
- 验证第 3 次错误后触发熔断
- 验证 exit code=1，reason 包含 "circuit_breaker"

#### 场景 8: tool 错误注入上下文
- Mock LLM 返回 tool_call 指向不存在的设备，然后 complete
- 验证错误以 tool message 格式注入上下文
- 验证 LLM 在下一步收到错误信息并能正常 complete

#### 场景 9: VFS flags 降级
- 注册 mock device 到 VFS，在 device factory 中验证传入的 flags
- Mock LLM 返回 tool_call with empty data `{}`
- 验证 device factory 收到 `vfs.O_RDONLY`（而非 `O_RDWR`）

### Task 4: 编译验证 [AC-8]

```bash
go build ./cmd/rnix/
go vet ./...
go test -race -count=1 ./kernel/... ./agents/...
make all
```

## Dev Notes

### 代码库测试现状分析

| 测试文件 | 状态 | 说明 |
|---------|------|------|
| `kernel/ooda_test.go` | 已删除（26-1） | N/A |
| `kernel/ooda_reasoning_test.go` | 已删除（26-1） | N/A |
| `agents/testdata/ooda-agent/` | 已删除（26-1） | N/A |
| `agents/testdata/invalid-reasoning/` | 已删除（26-1） | N/A |
| `agents/loader_reasoning_test.go` | 已重写（26-2） | 3 个 Planning 测试 |
| `kernel/atdd_26_3_specialize_migration_test.go` | 已完成（26-3） | 17 个 Specialize 测试 |
| `kernel/stem_integration_test.go` | 无 OODA 引用 | 仅 L67 有 "linear reasoning" 注释 |
| `kernel/diffmemory_integration_test.go` | 无 OODA 引用 | 清理完毕 |
| `kernel/lineage_integration_test.go` | 无 OODA 引用 | 清理完毕 |
| `kernel/diffmemory_test.go` | 无 OODA 引用 | 清理完毕 |

**结论：** OODA 清理已在 Story 26-1/26-2/26-3 中完成。本 Story 的主要工作是：
1. 实现两个遗留 bug 修复（VFS flags + 熔断）
2. 编写统一推理循环测试矩阵

### 已有 ReasonStep 测试覆盖

| 测试 | 文件 | 覆盖 |
|------|------|------|
| `TestReasonStep_TextAction` | kernel_test.go | text 纯文本 |
| `TestReasonStep_ToolCallAction` | kernel_test.go | tool_call 基本路径 |
| `TestReasonStep_LLMError` | kernel_test.go | LLM 返回 JSON error |
| `TestReasonStep_LLMReadError` | kernel_test.go | LLM Read I/O 错误 |
| `TestReasonStep_ContextCancellation` | kernel_test.go | 上下文取消 |
| `TestReasonStep_MaxStepsExceeded` | kernel_test.go | 最大步数限制 |
| `TestReasonStep_PermissionDenied_*` | kernel_test.go | AllowedDevices 白名单 |
| `TestReasonStep_ToolOpenFails_*` | phase2_toolerror_test.go | tool error → HasToolError |
| `TestReasonStep_Specialize_*` (17个) | atdd_26_3_*.go | specialize 完整覆盖 |

**缺失覆盖：** Plan、Spawn、Complete、Replan 的 reasonStep 分支测试、熔断机制测试、VFS flags 降级测试。

### VFS Flags 降级实现细节

当前 `kernel.go:1274` 硬编码 `vfs.O_RDWR`。`drivers/fs/hostfs.go:82` 对 `O_RDWR` 的文件有写权限检查。当 LLM 想读取一个 `/dev/fs/path/to/file` 时，如果 payload 为空 `{}`，应该用 `O_RDONLY` 打开。

判断逻辑：
```go
isEmpty := len(action.ToolData) == 0 || string(action.ToolData) == "{}"
```

这覆盖了两种空 payload 情况：
1. `action.ToolData` 为 `nil`（len=0）
2. `action.ToolData` 为字面量 `{}`

### 熔断不计入的 Action 类型

| Action | 失败计入熔断？ | 理由 |
|--------|-------------|------|
| tool_call | 是 | 资源性错误（设备打开/读写失败） |
| spawn | 是 | 资源性错误（子进程创建失败） |
| specialize | 否 | 可恢复的逻辑错误（skill 不存在可调整策略） |
| plan | 否 | 逻辑操作（AppendMessage 失败是致命的，直接 finishProcess） |
| replan | 否 | 逻辑操作（同 plan） |

### 测试辅助设施一览

位于 `kernel/kernel_test.go` 顶部的 mock 和 helper：

| Helper | 说明 |
|--------|------|
| `mockLLMFile` | 模拟 LLM 设备（可注入 readData/readErr/writeErr） |
| `mockToolFile` | 模拟工具设备 |
| `newTestKernel(t, llmFile)` | 创建 kernel + VFS（注册 `/dev/llm/claude`）+ CtxMgr |
| `makeLLMResponse(content, tokens)` | 构建 `llmResponse` JSON |
| `makeToolCallResponse(toolPath, data, tokens)` | 构建 tool_call action 响应 |
| `testAgentInfo()` | 创建测试用 AgentInfo（含 mock-skill） |
| `gatedLLMFile`（atdd_26_3） | 阻塞首次 Read 直到 Release() 被调用 |

### 熔断实现位置速查

`reasonStep` 函数中的错误路径（需要添加 `consecutiveToolErrors++` + 熔断检查）：

**case ActionToolCall:**
1. `vfs.Open` 失败（L1279-1289）— 错误已通过 `AppendToolResult` 注入上下文
2. `file.Write` 失败（L1302-1313）— 同上
3. `file.Read` 失败（L1319-1329）— 同上

**case ActionSpawn:**
1. `k.Spawn` 返回 error（L1437-1447）— spawn 错误

成功路径（需要重置 `consecutiveToolErrors = 0`）：
- ActionToolCall 成功写入 tool result 后
- ActionSpawn 成功写入 child result 后

### 文件修改清单

| 文件 | 变更类型 | 说明 |
|------|----------|------|
| `kernel/kernel.go` | 修改 | VFS flags 降级（~6 行替换）+ consecutiveToolErrors 声明 + 熔断递增/重置/检查（~20 行插入） |
| `kernel/atdd_26_4_unified_reasoning_test.go` | 新建 | 统一推理循环测试矩阵（~500-700 行） |

### 不修改的文件（确认）

| 文件 | 原因 |
|------|------|
| `agents/loader_reasoning_test.go` | 已在 26-2 重写为 Planning 测试 |
| `kernel/stem_integration_test.go` | 无 OODA 引用（L67 "linear reasoning" 注释可保留或改为 "unified reasoning"，可选） |
| `kernel/diffmemory_integration_test.go` | 无 OODA 引用 |
| `kernel/lineage_integration_test.go` | 无 OODA 引用 |
| `kernel/diffmemory_test.go` | 无 OODA 引用 |
| `kernel/process.go` | 无需新字段 |

### 执行顺序

1. Task 1: VFS flags 自动降级（修改 kernel.go ~6 行）
2. Task 2: 熔断机制（修改 kernel.go ~20 行新增）
3. Task 3: 编写测试矩阵（新建 atdd_26_4 测试文件）
4. Task 4: `make all` 验证

### 风险评估

- **低风险**：VFS flags 变更是一个小补丁，仅影响 tool_call 的 Open 调用
- **低风险**：熔断是新增逻辑，不影响现有正常路径
- **中等风险**：Spawn 测试需要两层 LLM mock（父 + 子），setup 较复杂
- **缓解措施**：Spawn 测试可以不指定 agent，使用 intent 作为指令，简化 mock

### 关键 API 签名

```go
// VFS Open
k.vfs.Open(pid types.PID, path string, flags vfs.OpenFlag) (types.FD, error)

// VFS OpenFlag 常量
vfs.O_RDONLY  // 读取
vfs.O_RDWR    // 读写

// Process exit
k.finishProcess(proc *Process, exit ExitStatus)

// ExitStatus
type ExitStatus struct {
    Code   int
    Reason string
    Err    error
}

// Context 写入
k.ctxMgr.AppendToolResult(ctxID types.CtxID, toolName string, result string) error
k.ctxMgr.AppendMessage(ctxID types.CtxID, role rnixctx.Role, content string) error
```

## References

- Epic 定义：`_bmad-output/planning-artifacts/epics/epic-26-统一推理循环-unified-reasoning-loop.md`（Story 26.5 测试矩阵部分）
- 前序 Story 26-3：`_bmad-output/implementation-artifacts/26-3-specialize-capability-migration.md`
- 前序 Story 26-2：`_bmad-output/implementation-artifacts/26-2-planning-config-and-agent-adaptation.md`
- Sprint Change Proposal：`_bmad-output/planning-artifacts/sprint-change-proposal-2026-03-18.md`（§4.3 Story 26.4 范围 + §4.3 Story 26.1 VFS/熔断范围）
- 测试模式参考：`kernel/atdd_26_3_specialize_migration_test.go`（gatedLLMFile、mock 模式）
- 测试基础设施：`kernel/kernel_test.go`（mockLLMFile、newTestKernel、makeLLMResponse 等 helper）

## Dev Agent Record

### Agent Model Used

{{agent_model_name_version}}

### Debug Log References

### Completion Notes List

### File List
