# Story 10.3: Token 预算管理

Status: review

## Story

As a 用户,
I want 为智能体设置 token 预算上限，超限时系统自动终止推理,
So that 我可以控制 LLM 调用的成本。

## Acceptance Criteria

1. **AC1: Agent 级 Token 预算执行**
   - Given agent.yaml 设置 `context_budget: 5000`
   - When 智能体累计消耗达到 5000 token
   - Then 系统终止推理（FR61）
   - And 进程转 Zombie，ExitStatus `{Code: 2, Reason: "budget_exceeded"}`
   - And emitLog 发送 `[output]` 类别的预算超限通知

2. **AC2: Compose 覆盖预算**
   - Given compose.yaml 中为特定智能体设置 `context_budget: 10000`
   - When 该智能体的 agent.yaml 设置 `context_budget: 5000`
   - Then 使用 compose 中的 10000 覆盖 agent.yaml 中的 5000
   - And 预算优先级：Compose > Agent > 默认（0=无限制）

3. **AC3: crux top 预算警告**
   - Given 预算已设且剩余 < 10%
   - When crux top 刷新
   - Then TOKENS 列显示黄色（WarningStyle）
   - And 格式为 `已用/预算`（如 `4,600/5,000`）

4. **AC4: 无预算时无变化**
   - Given `context_budget: 0` 或未设置
   - When 推理循环执行
   - Then 行为与现有完全一致（无限制）
   - And crux top TOKENS 列维持现有纯数字格式

5. **AC5: IPC 传递预算信息**
   - Given 进程已设预算
   - When `crux top` 或 `crux ps` 通过 IPC 获取进程信息
   - Then ProcInfo 包含 `ContextBudget` 字段
   - And 客户端可用于判断警告阈值

## Tasks / Subtasks

- [x] Task 1: 内核层预算检查 (AC: #1, #4)
  - [x] 1.1 `kernel/kernel.go`：`SpawnOpts` 添加 `ContextBudget int` 字段
  - [x] 1.2 `kernel/kernel.go`：Spawn 方法中，如果 `opts.ContextBudget == 0` 且 agent 非 nil，读取 `agent.Manifest.ContextBudget` 赋值到 `proc.ContextBudget`；如果 `opts.ContextBudget > 0` 则覆盖
  - [x] 1.3 `kernel/process.go`：Process 添加 `ContextBudget int` 字段（在 `TokensUsed` 旁）
  - [x] 1.4 `kernel/kernel.go`：reasonStep 中 `proc.TokensUsed += resp.TokensUsed` 之后，添加预算检查——如果 `proc.ContextBudget > 0 && proc.TokensUsed >= proc.ContextBudget`，调用 `finishProcess(proc, ExitStatus{Code: 2, Reason: "budget_exceeded"})` 并 return
  - [x] 1.5 `kernel/kernel.go`：预算超限前 emitLog `[output]` 和 emitEvent `"ReasonStep"` 记录超限详情

- [x] Task 2: Compose 预算覆盖 (AC: #2)
  - [x] 2.1 `compose/types.go`：`AgentSpec` 添加 `ContextBudget int \`yaml:"context_budget,omitempty"\``
  - [x] 2.2 `compose/types.go`：`ComposeSpawnOpts` 添加 `ContextBudget int`
  - [x] 2.3 `compose/engine.go`：spawnAgent 中将 `agentSpec.ContextBudget` 赋值到 `opts.ContextBudget`
  - [x] 2.4 `cmd/crux/compose.go`：`ipcKernelSpawner.Spawn` 将 `ComposeSpawnOpts.ContextBudget` 传入 IPC `SpawnRequest`
  - [x] 2.5 `ipc/protocol.go`：`SpawnRequest` 添加 `ContextBudget int \`json:"context_budget,omitempty"\``
  - [x] 2.6 `ipc/server.go`：`handleSpawn` 解析 `ContextBudget` 并传入 `kernel.SpawnOpts`

- [x] Task 3: ProcInfo 扩展 (AC: #5)
  - [x] 3.1 `vfs/proc.go`：`ProcInfo` 添加 `ContextBudget int` 字段
  - [x] 3.2 `kernel/kernel.go`：`GetProcInfo` 中将 `proc.ContextBudget` 写入 ProcInfo
  - [x] 3.3 `ipc/protocol.go`：`ProcInfoWire` 添加 `ContextBudget int \`json:"context_budget,omitempty"\``
  - [x] 3.4 `ipc/protocol.go`：`ProcInfoToWire` 和 `WireToProcInfo` 转换 ContextBudget
  - [x] 3.5 `vfs/proc.go`：`statusJSON` 添加 `ContextBudget` 字段

- [x] Task 4: crux top 预算警告显示 (AC: #3, #4)
  - [x] 4.1 `cmd/crux/top.go`：进程列表渲染中，如果 `proc.ContextBudget > 0`，TOKENS 列改为 `已用/预算` 格式
  - [x] 4.2 `cmd/crux/top.go`：如果 `proc.ContextBudget > 0 && proc.TokensUsed >= proc.ContextBudget * 90 / 100`，用 `WarningStyle` 渲染 TOKENS 列
  - [x] 4.3 `cmd/crux/top.go`：topSummaryLine 中总 tokens 显示不变（仍为总消耗纯数字）
  - [x] 4.4 `cmd/crux/top.go`：topDetailView 中增加 Budget 行（仅在 budget > 0 时显示）

- [x] Task 5: 测试 (AC: all)
  - [x] 5.1 `kernel/budget_test.go`：预算检查单元测试——mock LLM 返回固定 token，验证达到预算时进程转 Zombie + exit reason = "budget_exceeded"
  - [x] 5.2 `kernel/budget_test.go`：无预算时（budget=0）不触发终止
  - [x] 5.3 `kernel/budget_test.go`：Spawn 传入 ContextBudget 覆盖 agent 默认值
  - [x] 5.4 `compose/engine_test.go`：验证 AgentSpec.ContextBudget 传递到 ComposeSpawnOpts
  - [x] 5.5 `cmd/crux/top_test.go`：验证预算警告格式（`已用/预算`）和无预算纯数字格式
  - [x] 5.6 `ipc/protocol_test.go`：ProcInfoWire 往返转换包含 ContextBudget
  - [x] 5.7 在 `cmd/crux/main_test.go` 中确认无命令注册回归

## Dev Notes

### 关键架构约束

- **依赖方向不变**：`cmd/crux/` → `ipc/` → `vfs/`（ProcInfo），`kernel/` → `agents/`（AgentManifest），`compose/` → `agents/`
- **不创建 `context/budget.go`**：PRD 中 `context/budget.go` 是早期规划建议。实际上预算检查是内核行为（在 reasonStep 中基于 `proc.TokensUsed` 判断），不属于上下文管理（context 包管理消息历史）。将预算逻辑放在 `kernel/kernel.go` 中，与现有 `maxSteps` 检查并行
- **ExitStatus.Code = 2**：区别于正常退出 (0) 和错误退出 (1)，预算超限用 Code=2 表示"受控终止"
- **向后兼容**：`ContextBudget=0` 表示无限制，所有现有行为不变

### 预算检查核心实现

在 `kernel/kernel.go` reasonStep 循环中，现有 token 累加位于第 530-532 行：

```go
proc.mu.Lock()
proc.TokensUsed += resp.TokensUsed
proc.mu.Unlock()
```

在此之后**立即**添加预算检查：

```go
proc.mu.Lock()
proc.TokensUsed += resp.TokensUsed
budget := proc.ContextBudget
tokens := proc.TokensUsed
proc.mu.Unlock()

if budget > 0 && tokens >= budget {
    k.emitLog(proc, step, types.LogOutput,
        fmt.Sprintf("Token budget exceeded: %d/%d", tokens, budget), "")
    k.emitEvent(proc, "ReasonStep", map[string]any{
        "step":   step,
        "action": "budget_exceeded",
        "tokens": tokens,
        "budget": budget,
    }, nil, nil, time.Since(stepStart))
    k.finishProcess(proc, ExitStatus{
        Code:   2,
        Reason: "budget_exceeded",
        Err:    fmt.Errorf("token budget exceeded: %d/%d", tokens, budget),
    })
    return
}
```

**设计要点**：
- 在 `proc.mu.Lock()` 内同时读取 `budget` 和 `tokens`，避免 TOCTOU
- 预算检查在 `parseAction` 之前——即使 LLM 返回了有效 action，超限也会阻止执行
- 与 `maxSteps` 检查的区别：`maxSteps` 是循环上限（for 条件），budget 是基于累计消耗的守卫

### SpawnOpts 预算优先级

```go
// Spawn 方法中（在现有 agent.Manifest.Models.Preferred 处理后）
if agent != nil {
    // Budget priority: opts (CLI/Compose) > agent manifest > 0 (no limit)
    if opts.ContextBudget == 0 && agent.Manifest.ContextBudget > 0 {
        opts.ContextBudget = agent.Manifest.ContextBudget
    }
}
proc.ContextBudget = opts.ContextBudget
```

与模型选择优先级一致：外部传入 > Agent 配置 > 默认值。

### Compose 集成

`compose/types.go` 中 `AgentSpec` 添加字段：

```go
type AgentSpec struct {
    Intent       string            `yaml:"intent"`
    Agent        string            `yaml:"agent,omitempty"`
    Model        string            `yaml:"model,omitempty"`
    Skills       []string          `yaml:"skills,omitempty"`
    ContextBudget int              `yaml:"context_budget,omitempty"`
    DependsOn    map[string]string `yaml:"depends_on,omitempty"`
}
```

`compose/engine.go` spawnAgent 中传递：

```go
opts := ComposeSpawnOpts{
    Model:         model,
    ContextBudget: agentSpec.ContextBudget,
}
```

`cmd/crux/compose.go` 中 `ipcKernelSpawner.Spawn` 将 `ContextBudget` 传入 IPC SpawnRequest。

### IPC 协议扩展

`ipc/protocol.go` 中已有 `SpawnRequest`，添加 `ContextBudget`：

```go
type SpawnRequest struct {
    Intent        string   `json:"intent"`
    Agent         string   `json:"agent,omitempty"`
    Model         string   `json:"model,omitempty"`
    Skills        []string `json:"skills,omitempty"`
    SystemPrompt  string   `json:"system_prompt,omitempty"`
    ParentPID     types.PID `json:"parent_pid,omitempty"`
    ContextBudget int      `json:"context_budget,omitempty"`
}
```

`ProcInfoWire` 添加 `ContextBudget`：

```go
type ProcInfoWire struct {
    // ... existing fields ...
    ContextBudget int `json:"context_budget,omitempty"`
}
```

`ProcInfoToWire` 和 `WireToProcInfo` 同步更新。

### ProcInfo 扩展

`vfs/proc.go` 中 `ProcInfo` 添加字段：

```go
type ProcInfo struct {
    // ... existing fields ...
    ContextBudget int
}
```

`kernel/kernel.go` 中 `GetProcInfo` 读取 `proc.ContextBudget` 写入 ProcInfo。

`statusJSON` 添加 `ContextBudget int \`json:"context_budget,omitempty"\``。

### crux top 预算警告渲染

在 `cmd/crux/top.go` 进程列表渲染中：

```go
// 现有：tokens := ui.FormatTokens(row.proc.TokensUsed)
// 替换为：
var tokens string
if row.proc.ContextBudget > 0 {
    tokens = fmt.Sprintf("%s/%s",
        ui.FormatTokens(row.proc.TokensUsed),
        ui.FormatTokens(row.proc.ContextBudget))
} else {
    tokens = ui.FormatTokens(row.proc.TokensUsed)
}

// 警告着色：剩余 < 10% 时用 WarningStyle
if row.proc.ContextBudget > 0 &&
    row.proc.TokensUsed >= row.proc.ContextBudget*90/100 {
    tokens = ui.WarningStyle.Render(tokens)
}
```

TOKENS 列宽度需从 8 调整到 12（适配 `9,999/10,000` 格式），更新 header 和行格式：

```go
header := fmt.Sprintf("  %-5s %-5s %-9s %-15s %12s %8s",
    "PID", "PPID", "STATE", "AGENT", "TOKENS", "ELAPSED")
```

topDetailView 中增加 Budget 行（仅 budget > 0 时）：

```go
if info.ContextBudget > 0 {
    pct := info.TokensUsed * 100 / info.ContextBudget
    fmt.Fprintf(&b, "  Budget:   %s/%s (%d%%)\n",
        ui.FormatTokens(info.TokensUsed),
        ui.FormatTokens(info.ContextBudget), pct)
} else {
    fmt.Fprintf(&b, "  Tokens:   %s\n", ui.FormatTokens(info.TokensUsed))
}
```

### 复用现有代码

**必须复用（不要重新实现）：**
- `internal/ui/styles.go`：`WarningStyle`（`#FFD93D` 黄色）用于预算警告
- `internal/ui/table.go`：`FormatTokens(n int) string` — 千分位格式
- `kernel/kernel.go`：`finishProcess(proc, ExitStatus)` — 进程终止
- `kernel/kernel.go`：`emitEvent` 和 `emitLog` — 事件记录

**复用的架构模式：**
- `maxSteps` 检查模式 — 循环守卫（for 条件 vs 循环体内检查）
- `opts.Model` 优先级模式 — SpawnOpts > Agent manifest > 默认

### 测试策略

- **预算检查核心测试** (`kernel/budget_test.go`)：
  - mock LLM 驱动返回固定 `TokensUsed=1000`，设 budget=2500，验证第 3 步（3000 >= 2500）触发 budget_exceeded
  - budget=0 时验证 5 步全部执行完毕（不触发预算终止）
  - SpawnOpts.ContextBudget=3000 覆盖 agent.Manifest.ContextBudget=1000，验证使用 3000

- **Compose 传递测试** (`compose/engine_test.go`)：
  - AgentSpec 设 `context_budget: 5000`，验证 ComposeSpawnOpts.ContextBudget=5000

- **crux top 渲染测试** (`cmd/crux/top_test.go`)：
  - ProcInfo{TokensUsed: 4500, ContextBudget: 5000} → 输出包含 `4,500/5,000`
  - ProcInfo{TokensUsed: 500, ContextBudget: 0} → 输出纯数字 `500`
  - 预算 90% 警告阈值验证

- **IPC 往返测试** (`ipc/protocol_test.go`)：
  - ProcInfoWire 包含 ContextBudget 的序列化/反序列化

- **回归测试**：现有全套测试通过（budget=0 路径不影响任何现有行为）

### Process 字段添加位置

在 `kernel/process.go` 的 `Process` 结构体中，`ContextBudget` 紧跟 `TokensUsed`：

```go
type Process struct {
    // ... existing fields ...
    TokensUsed     int
    ContextBudget  int    // 0 = no limit; >0 = terminate when TokensUsed >= ContextBudget
    AllowedDevices []string
    // ... rest ...
}
```

### 边界情况

- **首次调用就超限**：LLM 返回 5000 tokens，budget=3000 → 立即终止，不执行 action
- **恰好等于预算**：`TokensUsed == ContextBudget` 触发终止（使用 `>=` 而非 `>`）
- **负预算值**：视为 0（无限制）。在 Spawn 中 `if opts.ContextBudget < 0 { opts.ContextBudget = 0 }`
- **已有 exit reason**：新增 "budget_exceeded" 与现有 "completed"/"max steps exceeded" 等并列
- **crux top 列宽**：TOKENS 列从 8 调整到 12 字符，确保 `99,999/100,000` 不截断
- **crux top 着色与 stripAnsi**：WarningStyle 渲染在 line 构建后，因为现有 cursor 高亮逻辑（line 389-393）已处理 ANSI。需确保 WarningStyle 仅应用于 tokens 变量，不影响整行
- **ContextBudget 字段在 ProcInfoWire 中 omitempty**：budget=0 时 JSON 中不出现，节省带宽

### Project Structure Notes

- **修改文件**：
  - `kernel/kernel.go` — SpawnOpts 添加 ContextBudget，Spawn 中赋值，reasonStep 中添加预算检查，GetProcInfo 中传递
  - `kernel/process.go` — Process 添加 ContextBudget 字段
  - `compose/types.go` — AgentSpec 和 ComposeSpawnOpts 添加 ContextBudget
  - `compose/engine.go` — spawnAgent 传递 ContextBudget
  - `cmd/crux/compose.go` — ipcKernelSpawner.Spawn 传递 ContextBudget
  - `ipc/protocol.go` — SpawnRequest、ProcInfoWire 添加 ContextBudget，ProcInfoToWire/WireToProcInfo 更新
  - `ipc/server.go` — handleSpawn 解析 ContextBudget
  - `vfs/proc.go` — ProcInfo、statusJSON 添加 ContextBudget
  - `cmd/crux/top.go` — TOKENS 列渲染逻辑、列宽调整、WarningStyle 着色、详情视图 Budget 行
- **新测试文件**：
  - `kernel/budget_test.go` — 预算检查核心测试
- **修改测试文件**：
  - `cmd/crux/top_test.go` — 预算警告渲染测试
  - `compose/engine_test.go` — Compose ContextBudget 传递测试
  - `ipc/protocol_test.go` — ProcInfoWire ContextBudget 往返测试
- **不修改**：astrace、crux log、驱动层、context 包、agents 包（ContextBudget 已存在）、skills 包
- **不需要新依赖**

### References

- [Source: _bmad-output/planning-artifacts/epics/epic-10-监控supervisor-与运维monitoring-supervisor-operations.md#Story 10.3]
- [Source: _bmad-output/planning-artifacts/archive/prd.md#FR61]
- [Source: _bmad-output/planning-artifacts/archive/architecture.md#Decision 7: Agent 抽象层]
- [Source: _bmad-output/project-context.md#Channel 使用规则]
- [Source: agents/types.go#AgentManifest.ContextBudget]
- [Source: kernel/kernel.go#reasonStep（line 530-532 token 累加）]
- [Source: kernel/kernel.go#SpawnOpts + Spawn 方法]
- [Source: kernel/process.go#Process 结构体]
- [Source: compose/types.go#AgentSpec + ComposeSpawnOpts]
- [Source: compose/engine.go#spawnAgent]
- [Source: cmd/crux/compose.go#ipcKernelSpawner]
- [Source: ipc/protocol.go#SpawnRequest + ProcInfoWire]
- [Source: ipc/server.go#handleSpawn]
- [Source: vfs/proc.go#ProcInfo + statusJSON]
- [Source: cmd/crux/top.go#topSummaryLine + 进程列表渲染 + topDetailView]
- [Source: internal/ui/styles.go#WarningStyle]
- [Source: internal/ui/table.go#FormatTokens]
- [Source: _bmad-output/implementation-artifacts/10-1-crux-top-realtime-monitoring-tui.md#复用现有代码]
- [Source: _bmad-output/implementation-artifacts/10-2-crux-log-categorized-reasoning-logs.md#emitLog 模式]

## Dev Agent Record

### Agent Model Used

Claude claude-4.6-opus (Cursor)

### Debug Log References

### Completion Notes List

- AC1: 内核 reasonStep 中 token 累加后立即检查 budget，超限时 ExitStatus{Code:2, Reason:"budget_exceeded"}，emitLog [output] + emitEvent ReasonStep action=budget_exceeded
- AC2: compose AgentSpec.ContextBudget → ComposeSpawnOpts → IPC SpawnRequest → kernel SpawnOpts 全链路传递，优先级：Compose > Agent > 默认(0)
- AC3: crux top 进程列表 TOKENS 列在 budget>0 时显示 `已用/预算` 格式，剩余<10% 用 WarningStyle 黄色渲染；详情视图显示 Budget 行含百分比
- AC4: ContextBudget=0 时所有行为与现有完全一致——reasonStep 无额外检查，crux top 纯数字格式
- AC5: ProcInfo/ProcInfoWire/statusJSON 均包含 ContextBudget 字段，IPC 往返序列化正确，omitempty 节省带宽
- 边界情况：负预算视为 0（Spawn 中归零），tokens==budget 触发终止（>=），首次调用即超限立即终止且不执行 action
- 修复 RED 阶段测试 bug：TestTopDetailView_NoBudgetOmitsBudgetLine 的 Intent "no budget" 含检测关键词导致误报，改为 "no limit set"

### Change Log

- 2026-03-02: Story 10.3 Token 预算管理全量实现——内核预算检查、Compose 覆盖、ProcInfo 扩展、crux top 警告渲染。全部 17 个包测试通过。

### File List

- kernel/kernel.go — SpawnOpts.ContextBudget, Spawn 预算优先级, reasonStep 预算检查, GetProcInfo/ListProcs 传递
- kernel/process.go — Process.ContextBudget 字段
- kernel/budget_test.go — 14 个预算检查单元/集成测试（ATDD RED→GREEN）
- compose/types.go — AgentSpec.ContextBudget, ComposeSpawnOpts.ContextBudget
- compose/engine.go — executeNode 传递 ContextBudget
- compose/engine_test.go — 3 个 Compose ContextBudget 传递测试
- cmd/crux/compose.go — ipcKernelSpawner.Spawn 传入 ContextBudget
- ipc/protocol.go — SpawnRequest.ContextBudget, ProcInfoWire.ContextBudget, ProcInfoToWire/WireToProcInfo 转换
- ipc/protocol_test.go — 5 个 IPC ContextBudget 往返测试
- ipc/server.go — handleSpawn 解析 ContextBudget 传入 kernel.SpawnOpts
- vfs/proc.go — ProcInfo.ContextBudget, statusJSON.ContextBudget, buildStatusJSON 传递
- vfs/proc_test.go — 4 个 ProcInfo/statusJSON ContextBudget 测试
- cmd/crux/top.go — TOKENS 列 已用/预算 格式, WarningStyle 着色, 列宽 8→12, 详情 Budget 行
- cmd/crux/top_test.go — 5 个 crux top 预算警告渲染测试
