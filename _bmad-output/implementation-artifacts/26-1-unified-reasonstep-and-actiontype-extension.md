# Story 26.1: OODA 代码删除与统一分支入口

Status: review

## Story

As a 平台构建者,
I want 删除所有 OODA 相关代码并统一推理循环入口为单一 `reasonStep`,
so that 代码库只有一条推理路径，消除双模式维护负担。

## Acceptance Criteria (AC)

### AC-1: 删除 OODA 核心实现文件
**Given** `kernel/ooda.go` 文件存在（531 行）
**When** 执行删除
**Then** 整个文件被删除，包含所有 OODA 类型定义（`OODAPhase`、`OODAState`、`OODADecision`、`OODAActionType`）、prompt 模板、`oodaReasonStep`、`oodaAct`、`oodaActToolCall`、`oodaActSpawn`、`oodaActSpecialize`、`oodaCallLLM` 函数

### AC-2: 删除 OODA 测试文件
**Given** `kernel/ooda_test.go`（819 行）和 `kernel/ooda_reasoning_test.go`（650 行）存在
**When** 执行删除
**Then** 两个测试文件完全删除

### AC-3: 删除 ooda-demo Agent 目录
**Given** `lib/agents/ooda-demo/` 目录存在
**When** 执行删除
**Then** 目录及其中 `agent.yaml` 和 `instructions.md` 完全删除

### AC-4: 删除 ooda-agent 测试数据目录
**Given** `agents/testdata/ooda-agent/` 目录存在
**When** 执行删除
**Then** 目录及其中 `agent.yaml` 和 `instructions.md` 完全删除

### AC-5: 清理 Process 结构体
**Given** `kernel/process.go` 中存在 OODA 相关字段和方法
**When** 清理 Process 结构体
**Then** 删除 `oodaEnabled bool` 字段（第 89 行）
**And** 删除 `oodaState *OODAState` 字段（第 90 行）
**And** 删除 `IsOODA()` 方法（第 300-305 行）
**And** 删除 `GetOODAState()` 方法（第 307-320 行）
**And** 删除 `SetOODAPhase()` 方法（第 322-332 行）
**And** 删除注释 `// --- OODA state methods (all thread-safe via mu) ---`（第 298 行）

### AC-6: 统一 Spawn 分支入口
**Given** `kernel/kernel.go` 中 Spawn 方法存在 OODA 分支
**When** 统一分支入口
**Then** 删除 `SpawnOpts.ReasoningMode` 字段（第 52 行）
**And** 删除 OODA state 初始化块（第 611-617 行）
**And** 删除 `if proc.oodaEnabled` 分支（第 624-625 行），所有进程统一走 `k.reasonStep(proc, llmFD, opts)`
**And** 删除 `if agent.Manifest.Reasoning == "ooda"` 代码块（第 362-363 行）
**And** 删除注释 `// Agent loader for OODA autonomous spawn (Story 20.2)`（第 160 行）
**And** 更新注释 `// SetAgentLoader injects the agent loading function for OODA autonomous spawn.`（第 1721 行）

### AC-7: 删除 LogOODA 常量
**Given** `internal/types/types.go` 中存在 `LogOODA` 常量
**When** 清理类型定义
**Then** 删除 `LogOODA LogCategory = "ooda"`（第 170 行）

### AC-8: 清理 main.go 注释
**Given** `cmd/rnix/main.go` 中存在 OODA 相关注释
**When** 清理注释
**Then** 删除 `// Inject for OODA autonomous spawn (Story 20.2)` 注释（第 1216 行）

### AC-9: 编译和静态分析通过
**Given** 所有删除完成
**When** 运行 `go build ./cmd/rnix/`
**Then** 编译成功，零错误
**And** 运行 `go vet ./...` 无警告

## Tasks / Subtasks

### Task 1: 删除 OODA 核心文件 [AC-1, AC-2]

删除以下三个文件：
- `kernel/ooda.go`（531 行）
- `kernel/ooda_test.go`（819 行）
- `kernel/ooda_reasoning_test.go`（650 行）

### Task 2: 删除 Agent 和测试数据目录 [AC-3, AC-4]

删除以下目录及其所有内容：
- `lib/agents/ooda-demo/`（含 `agent.yaml`、`instructions.md`）
- `agents/testdata/ooda-agent/`（含 `agent.yaml`、`instructions.md`）

### Task 3: 清理 Process 结构体 [AC-5]

修改 `kernel/process.go`：

1. 删除第 88-90 行的 OODA 字段和注释：
```go
// OODA loop state (mu protected)
oodaEnabled bool       // true if process uses OODA reasoning mode
oodaState   *OODAState // current OODA state, nil if not OODA
```

2. 删除第 298-332 行的 OODA state 方法块：
```go
// --- OODA state methods (all thread-safe via mu) ---

// IsOODA reports whether the process uses OODA reasoning mode.
func (p *Process) IsOODA() bool { ... }

// GetOODAState returns a copy of the current OODA state, or nil if not OODA.
func (p *Process) GetOODAState() *OODAState { ... }

// SetOODAPhase updates the current OODA phase.
func (p *Process) SetOODAPhase(phase OODAPhase) { ... }
```

### Task 4: 统一 kernel.go 推理入口 [AC-6]

修改 `kernel/kernel.go`：

1. **删除 `SpawnOpts.ReasoningMode` 字段**（第 52 行）：
```go
ReasoningMode     string                 // "" = linear (default), "ooda" = OODA loop
```

2. **删除 Agent Reasoning 模式传递**（第 362-363 行）：
```go
// Reasoning mode: agent.yaml > SpawnOpts (SpawnOpts is low-priority fallback)
if agent.Manifest.Reasoning == "ooda" {
    opts.ReasoningMode = "ooda"
}
```

3. **删除 OODA state 初始化块**（第 611-617 行）：
```go
// Initialize OODA state if requested
if opts.ReasoningMode == "ooda" {
    proc.mu.Lock()
    proc.oodaEnabled = true
    proc.oodaState = &OODAState{Phase: PhaseObserve, Cycle: 0}
    proc.mu.Unlock()
}
```

4. **统一推理入口**（第 621-629 行），将：
```go
proc.wg.Go(func() {
    defer func() { _ = k.vfs.CloseAll(proc.PID) }()
    _ = proc.Start() // Created → Running
    if proc.oodaEnabled {
        k.oodaReasonStep(proc, llmFD, opts)
    } else {
        k.reasonStep(proc, llmFD, opts)
    }
})
```
改为：
```go
proc.wg.Go(func() {
    defer func() { _ = k.vfs.CloseAll(proc.PID) }()
    _ = proc.Start() // Created → Running
    k.reasonStep(proc, llmFD, opts)
})
```

5. **更新 agentLoader 字段注释**（第 160 行），将 `// Agent loader for OODA autonomous spawn (Story 20.2)` 改为 `// Agent loader for autonomous spawn (Story 20.2)`

6. **更新 SetAgentLoader 注释**（第 1721 行），将 `// SetAgentLoader injects the agent loading function for OODA autonomous spawn.` 改为 `// SetAgentLoader injects the agent loading function for autonomous spawn.`

7. **更新 stem 分化日志中的 LogOODA 引用**（第 273, 283, 291, 305 行），将所有 `types.LogOODA` 改为 `types.LogOutput`（或合适的日志分类）

### Task 5: 删除 LogOODA 常量 [AC-7]

修改 `internal/types/types.go`，删除第 170 行：
```go
LogOODA    LogCategory = "ooda"
```

### Task 6: 清理 main.go 注释 [AC-8]

修改 `cmd/rnix/main.go`，删除第 1216 行尾部的 OODA 注释，将：
```go
k.SetAgentLoader(agentLoader.Load) // Inject for OODA autonomous spawn (Story 20.2)
```
改为：
```go
k.SetAgentLoader(agentLoader.Load)
```

### Task 7: 修复因删除导致的编译错误 [AC-9]

以下文件引用了已删除的 OODA 符号，需要一并修复：

#### 7a. `kernel/stem_integration_test.go`

- **第 29 行**：`stemAgentInfo()` 函数中 `Reasoning: "ooda"` → 删除该行（删除后 stem agent 不再需要指定 reasoning）
- **第 67 行**：注释 `// Set LLM response for OODA orient + decide phases` → 更新为 `// Set LLM response for linear reasoning`
- **第 102-104 行**：断言 `proc.GetOODAState() == nil` → 删除这个断言块（不再有 OODAState）

#### 7b. `kernel/diffmemory_integration_test.go`

此文件含大量 OODA 测试（Task 3: OODA specialize 测试），以下函数依赖 `newOODATestKernel`、`OODADecision`、`OODASpecialize`、`oodaActSpecialize` 等已删除符号，**全部需删除**：
- `TestOODA_Specialize_LoadSkill`（第 213 行起）
- `TestOODA_Specialize_AlreadyLoaded`（第 300 行起）
- `TestOODA_Specialize_SkillNotFound`（第 390 行起）
- `TestOODA_Specialize_UpdatesAllowedDevices`（第 456 行起）
- `TestOODA_Specialize_InjectsBody`（第 548 行起）
- `TestOODA_Specialize_RecordsToDiffMemory`（第 634 行起）
- `TestE2E_StemDifferentiation_ProgressiveSpecialization`（第 721 行起）
- `TestOODADecision_SpecializeType`（第 935 行起）

注意：保留 Task 2（DiffMemory Spawn 集成测试）和其他非 OODA 测试。

#### 7c. `kernel/lineage_integration_test.go`

以下函数直接引用 `oodaEnabled`、`oodaState`、`OODAState`、`OODADecision`、`OODASpecialize`、`oodaActSpecialize` 等已删除符号，**需删除**：
- `TestOODA_Specialize_RecordsLineage`（第 209 行起）— 使用 `proc.oodaEnabled = true`、`OODAState`、`OODADecision`、`oodaActSpecialize`
- `TestOODA_Specialize_LineageTriggerFromReason`（第 271 行起）— 同上

注意：保留 Task 2（Spawn Lineage 集成测试）、Task 4（GetLineage）、Task 6（E2E）等非 OODA 测试。

#### 7d. `agents/loader_reasoning_test.go`

此文件整体测试 reasoning 字段的 loader 验证：
- `TestAgentManifest_ReasoningField`：加载 `ooda-agent` → AC-4 已删除该目录，**此测试需删除**
- `TestAgentLoader_DefaultReasoningMode`：测试默认空值 → **保留**
- `TestAgentLoader_InvalidReasoningMode`：测试 `bogus` → **保留**
- `TestAgentLoader_LinearReasoningMode`：测试 `linear` → **保留**

#### 7e. `agents/loader.go` — Reasoning 验证

第 67-71 行的 reasoning 验证逻辑：
```go
switch manifest.Reasoning {
case "", "linear", "ooda":
    // valid
default:
    return nil, fmt.Errorf(...)
}
```
删除 `"ooda"` 分支，改为：
```go
switch manifest.Reasoning {
case "", "linear":
    // valid
default:
    return nil, fmt.Errorf("invalid reasoning mode %q: must be empty or \"linear\"", manifest.Reasoning)
}
```

#### 7f. `agents/types.go` — Reasoning 字段注释

第 27 行注释：
```go
Reasoning     string      `yaml:"reasoning,omitempty"` // "" = linear (default), "ooda" = OODA loop
```
更新为：
```go
Reasoning     string      `yaml:"reasoning,omitempty"` // "" = linear (default)
```

#### 7g. `lib/agents/stem/agent.yaml`

第 8 行：`reasoning: ooda` → **删除此行**（stem agent 不再需要指定 reasoning 模式）

### Task 8: 编译验证 [AC-9]

执行以下命令确保一切正常：
```bash
go build ./cmd/rnix/
go vet ./...
go test -count=1 ./kernel/... ./agents/... ./internal/types/...
```

## Dev Notes

### 架构约束

- 此 Story 是**纯删除/清理**操作，不引入新功能
- 完成后代码必须能编译通过——暂时丢失 specialize/spawn/complete 能力（Story 26.2 恢复）
- `agents/types.go` 中 `AgentManifest.Reasoning` 字段**保留**，因为后续 Story 可能用于 planning 配置
- `agents/loader.go` 中 reasoning 验证**保留但缩小**（仅接受 `""` 和 `"linear"`）

### 删除范围统计

| 类别 | 项目 | 行数/文件数 |
|------|------|-------------|
| 删除文件 | `kernel/ooda.go` | 531 行 |
| 删除文件 | `kernel/ooda_test.go` | 819 行 |
| 删除文件 | `kernel/ooda_reasoning_test.go` | 650 行 |
| 删除目录 | `lib/agents/ooda-demo/` | 2 文件 |
| 删除目录 | `agents/testdata/ooda-agent/` | 2 文件 |
| 修改文件 | `kernel/process.go` | 删除 ~40 行 |
| 修改文件 | `kernel/kernel.go` | 删除 ~20 行 + 改注释 |
| 修改文件 | `internal/types/types.go` | 删除 1 行 |
| 修改文件 | `cmd/rnix/main.go` | 改 1 行注释 |
| 修改文件 | `agents/loader.go` | 改 ~3 行 |
| 修改文件 | `agents/types.go` | 改 1 行注释 |
| 修改文件 | `agents/loader_reasoning_test.go` | 删除 1 个测试 |
| 修改文件 | `kernel/stem_integration_test.go` | 删除 ~6 行 |
| 修改文件 | `kernel/diffmemory_integration_test.go` | 删除 ~700 行 OODA 测试 |
| 修改文件 | `kernel/lineage_integration_test.go` | 删除 ~90 行 OODA 测试 |
| 修改文件 | `lib/agents/stem/agent.yaml` | 删除 1 行 |
| **总计** | | **~2860 行删除** |

### kernel.go LogOODA → LogOutput 替换

`kernel/kernel.go` 中 stem 分化日志使用了 `types.LogOODA`，出现在第 273、283、291、305 行。这些日志描述 stem 分化过程（skill 匹配），本质是信息类日志而非 OODA 特有。替换为 `types.LogOutput` 或新增一个更合适的分类（如果不想用 `LogOutput`，可暂时用 `types.LogWarning`——但 `LogOutput` 更语义化，因为这些是内核操作日志）。

建议统一替换为 `types.LogOutput`。

### 测试影响

- `agents/loader_reasoning_test.go` 中 `TestAgentManifest_ReasoningField` 依赖 `agents/testdata/ooda-agent/`，删除该目录后此测试必须删除
- `kernel/diffmemory_integration_test.go` 和 `kernel/lineage_integration_test.go` 中的 OODA specialize 测试依赖 `ooda.go` 中的符号，必须全部删除
- `kernel/stem_integration_test.go` 中 `stemAgentInfo()` 指定 `Reasoning: "ooda"` 和后续 `GetOODAState()` 断言需要清理
- 非 OODA 测试（DiffMemory 的 Spawn 集成、Lineage 的 Spawn 集成等）**不受影响，必须保留**

### Project Structure Notes

#### 删除的文件
```
kernel/ooda.go                         # OODA 核心实现（531 行）
kernel/ooda_test.go                    # OODA 单元测试（819 行）
kernel/ooda_reasoning_test.go          # OODA 推理测试（650 行）
lib/agents/ooda-demo/agent.yaml        # OODA 演示 agent 配置
lib/agents/ooda-demo/instructions.md   # OODA 演示 agent 指令
agents/testdata/ooda-agent/agent.yaml  # OODA 测试 agent 配置
agents/testdata/ooda-agent/instructions.md  # OODA 测试 agent 指令
```

#### 修改的文件（按执行顺序）

1. **`kernel/process.go`** — 删除 OODA 字段和方法
   - 第 88-90 行：删除 `oodaEnabled`、`oodaState` 字段及注释
   - 第 298-332 行：删除 `IsOODA()`、`GetOODAState()`、`SetOODAPhase()` 方法

2. **`kernel/kernel.go`** — 统一推理入口
   - 第 52 行：删除 `ReasoningMode` 字段
   - 第 160 行：更新 agentLoader 注释
   - 第 273, 283, 291, 305 行：`types.LogOODA` → `types.LogOutput`
   - 第 362-363 行：删除 `Reasoning == "ooda"` 块
   - 第 611-617 行：删除 OODA state 初始化块
   - 第 621-629 行：删除 `oodaEnabled` 分支，统一走 `reasonStep`
   - 第 1721 行：更新 `SetAgentLoader` 注释

3. **`internal/types/types.go`** — 删除 LogOODA
   - 第 170 行：删除 `LogOODA LogCategory = "ooda"`

4. **`cmd/rnix/main.go`** — 删除 OODA 注释
   - 第 1216 行：删除尾部 OODA 注释

5. **`agents/loader.go`** — 缩小 reasoning 验证
   - 第 67-71 行：删除 `"ooda"` 分支，更新错误消息

6. **`agents/types.go`** — 更新注释
   - 第 27 行：删除 `"ooda" = OODA loop` 注释

7. **`agents/loader_reasoning_test.go`** — 删除 OODA 测试
   - 删除 `TestAgentManifest_ReasoningField` 函数

8. **`kernel/stem_integration_test.go`** — 清理 OODA 引用
   - 第 29 行：删除 `Reasoning: "ooda"`
   - 第 67 行：更新注释
   - 第 102-104 行：删除 `GetOODAState()` 断言

9. **`kernel/diffmemory_integration_test.go`** — 删除 OODA 测试函数
   - 删除 8 个 OODA 相关测试函数，保留非 OODA 测试

10. **`kernel/lineage_integration_test.go`** — 删除 OODA 测试函数
    - 删除 2 个 OODA 相关测试函数，保留非 OODA 测试

11. **`lib/agents/stem/agent.yaml`** — 删除 reasoning 字段
    - 第 8 行：删除 `reasoning: ooda`

### References

- Epic 定义：`_bmad-output/planning-artifacts/epics/epic-26-统一推理循环-unified-reasoning-loop.md`
- Sprint 变更提案：`_bmad-output/planning-artifacts/sprint-change-proposal-2026-03-18.md`
- 统一推理循环提案：`_bmad-output/planning-artifacts/unified-reasoning-loop-proposal.md`
- 实施就绪性报告：`_bmad-output/planning-artifacts/implementation-readiness-report-2026-03-18.md`
- OODA 原始实现 Story：`_bmad-output/implementation-artifacts/20-1-ooda-loop-core-implementation.md`
- OODA 配置 Story：`_bmad-output/implementation-artifacts/20-2-ooda-configuration-and-mission-command.md`

## Dev Agent Record

### Agent Model Used
Claude claude-4.6-opus (Cursor Agent Mode)

### Debug Log References
N/A — pure deletion story, no debugging needed

### Completion Notes List
- All 9 ACs satisfied: OODA core files deleted, test files deleted, agent directories deleted, Process struct cleaned, kernel.go unified, LogOODA removed, main.go cleaned, compilation passes, vet passes
- `make all` passes: lint 0 issues, all tests green, build successful
- ~2860 lines of OODA code removed across 3 deleted files, 2 deleted directories, and 11 modified files
- `types.LogOODA` references in kernel.go stem differentiation logs replaced with `types.LogOutput`
- `agents/loader.go` reasoning validation narrowed to accept only "" and "linear"
- `lib/agents/stem/agent.yaml` no longer specifies `reasoning: ooda`
- All OODA-dependent tests in diffmemory_integration_test.go (8 tests) and lineage_integration_test.go (2 tests) deleted; non-OODA tests preserved and passing

### File List
**Deleted files:**
- `kernel/ooda.go` (531 lines)
- `kernel/ooda_test.go` (819 lines)
- `kernel/ooda_reasoning_test.go` (650 lines)
- `lib/agents/ooda-demo/agent.yaml`
- `lib/agents/ooda-demo/instructions.md`
- `agents/testdata/ooda-agent/agent.yaml`
- `agents/testdata/ooda-agent/instructions.md`

**Modified files:**
- `kernel/process.go` — removed oodaEnabled/oodaState fields and IsOODA/GetOODAState/SetOODAPhase methods
- `kernel/kernel.go` — removed SpawnOpts.ReasoningMode, OODA state init, oodaEnabled branch; replaced LogOODA with LogOutput; updated comments
- `internal/types/types.go` — removed LogOODA constant
- `cmd/rnix/main.go` — removed OODA comment from SetAgentLoader call
- `agents/loader.go` — removed "ooda" from reasoning validation switch
- `agents/types.go` — updated Reasoning field comment
- `agents/loader_reasoning_test.go` — deleted TestAgentManifest_ReasoningField
- `kernel/stem_integration_test.go` — removed Reasoning:"ooda", OODA comment, GetOODAState assertion
- `kernel/diffmemory_integration_test.go` — deleted 8 OODA specialize tests + OODADecision test
- `kernel/lineage_integration_test.go` — deleted 2 OODA specialize lineage tests
- `lib/agents/stem/agent.yaml` — removed reasoning: ooda line
