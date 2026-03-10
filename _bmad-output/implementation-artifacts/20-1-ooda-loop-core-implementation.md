# Story 20.1: OODA 循环核心实现

Status: done

## Story

As a 平台构建者,
I want 智能体可以在推理循环中执行 OODA 四阶段（感知-判断-决策-行动）,
So that 智能体能够自主感知环境变化并做出适应性决策。

## Acceptance Criteria

1. **Given** 一个 OODA 模式的智能体
   **When** 进入 Observe 阶段
   **Then** 智能体通过 VFS 读取环境信息（/proc/ 状态、其他进程输出、文件变更）

2. **Given** Observe 阶段完成
   **When** 进入 Orient 阶段
   **Then** 智能体评估感知数据与目标的偏差

3. **Given** Orient 阶段完成
   **When** 进入 Decide 阶段
   **Then** 智能体自主选择下一步行动（调用工具、spawn 子进程、请求协作或调整计划）

4. **Given** Decide 阶段完成
   **When** 进入 Act 阶段
   **Then** 执行决策并将结果反馈到下一轮 Observe 形成闭环
   **And** 单轮 OODA 循环框架开销 <= 200ms（NFR41）

## Tasks / Subtasks

### Task 1: OODA 类型与状态定义（AC: #1-#4）

- [x] 1.1 在 `kernel/` 包中新建 `kernel/ooda.go`，定义 OODA 阶段类型：

  ```go
  type OODAPhase string
  const (
      PhaseObserve OODAPhase = "observe"
      PhaseOrient  OODAPhase = "orient"
      PhaseDecide  OODAPhase = "decide"
      PhaseAct     OODAPhase = "act"
  )
  ```

- [x] 1.2 定义 `OODAState` 结构体，追踪 OODA 循环状态：

  ```go
  type OODAState struct {
      Phase       OODAPhase // 当前阶段
      Cycle       int       // 当前循环轮数
      Observations string   // Observe 收集的环境数据
      Orientation  string   // Orient 评估结果
      Decision     *OODADecision // Decide 输出
  }

  type OODADecision struct {
      Action   OODAActionType // 行动类型
      Target   string         // 行动目标（工具路径/进程意图/协作目标）
      Data     []byte         // 行动数据
      Reason   string         // 决策理由
  }

  type OODAActionType string
  const (
      OODAToolCall  OODAActionType = "tool_call"
      OODASpawn     OODAActionType = "spawn"
      OODAComplete  OODAActionType = "complete"
      OODAReplan    OODAActionType = "replan"
  )
  ```

- [x] 1.3 在 `Process` 结构体新增 OODA 字段（`kernel/process.go`）：

  ```go
  // OODA loop state (mu protected)
  oodaEnabled bool       // true if process uses OODA reasoning mode
  oodaState   *OODAState // current OODA state, nil if not OODA
  ```

  新增线程安全访问方法：`IsOODA() bool`、`GetOODAState() *OODAState`、`SetOODAPhase(phase OODAPhase)`

### Task 2: OODA 推理循环实现（AC: #1-#4，NFR41）

- [x] 2.1 在 `kernel/ooda.go` 实现 `oodaReasonStep` 方法：

  此方法是 OODA 模式的推理循环，替代默认的线性 `reasonStep`。核心结构：

  ```go
  func (k *KernelImpl) oodaReasonStep(proc *Process, llmFD types.FD, opts SpawnOpts) {
      maxCycles := proc.MaxSteps // 复用 MaxSteps 作为最大循环数
      defer func() { /* 确保 Zombie 转换 */ }()

      for cycle := 1; cycle <= maxCycles; cycle++ {
          cycleStart := time.Now()

          // --- Observe ---
          observations := k.oodaObserve(proc)

          // --- Orient ---
          orientation := k.oodaOrient(proc, llmFD, observations, opts)

          // --- Decide ---
          decision := k.oodaDecide(proc, llmFD, orientation, opts)

          // --- Act ---
          result := k.oodaAct(proc, llmFD, decision, opts)

          // 检查完成条件
          if decision.Action == OODAComplete {
              k.finishProcess(proc, ExitStatus{Code: 0, Reason: "ooda_completed"})
              return
          }

          // NFR41: 框架开销检查
          frameworkOverhead := time.Since(cycleStart)
          k.emitEvent(proc, "OODACycle", map[string]any{
              "cycle":              cycle,
              "framework_overhead": frameworkOverhead.Milliseconds(),
          }, nil, nil, frameworkOverhead)
      }

      k.finishProcess(proc, ExitStatus{Code: 1, Reason: "max ooda cycles exceeded"})
  }
  ```

- [x] 2.2 实现 Observe 阶段 — `oodaObserve(proc *Process) string`：

  通过 VFS 读取环境信息。Observe 是纯框架代码（不调用 LLM），读取：
  - `/proc/` 目录下的进程状态（通过 `k.ListProcesses()`）
  - 进程自身的上下文历史（通过 `k.ctxMgr.GetContextInfo()`）
  - 将环境快照序列化为 JSON 字符串供 Orient 使用

  关键约束：Observe 不调用 LLM，仅收集数据。框架开销控制在数十毫秒内。

- [x] 2.3 实现 Orient 阶段 — `oodaOrient(proc, llmFD, observations, opts) string`：

  将 observations 写入上下文，通过 LLM 评估偏差。Orient 的 LLM 调用使用专用 system prompt 注入：
  - 将 observations 作为 user message 追加到上下文
  - 构造 orient prompt：「你是 OODA 循环的 Orient 阶段。根据以下观测数据评估当前状态与目标的偏差...」
  - 调用 LLM，返回评估结果

  实现模式：复用 `reasonStep` 内已有的 LLM Write/Read 模式（`k.vfs.Write` → `k.vfs.Read` → 解析 `llmResponse`）。

- [x] 2.4 实现 Decide 阶段 — `oodaDecide(proc, llmFD, orientation, opts) *OODADecision`：

  基于 Orient 评估结果，通过 LLM 决策下一步行动。Decide 输出结构化 JSON：

  ```json
  {
    "action": "tool_call|spawn|complete|replan",
    "target": "/dev/fs|intent text|...",
    "data": {},
    "reason": "决策理由"
  }
  ```

  实现：向 LLM 追加 orientation 结果，请求输出结构化决策。解析为 `OODADecision`。

- [x] 2.5 实现 Act 阶段 — `oodaAct(proc, llmFD, decision, opts) string`：

  执行 Decide 的决策。根据 `OODAActionType` 分支：
  - `OODAToolCall`：复用 `reasonStep` 的 VFS Open/Write/Read/Close 工具调用模式
  - `OODASpawn`：通过 `k.Spawn()` 创建子进程，等待完成
  - `OODAComplete`：设置 `proc.Result`，返回
  - `OODAReplan`：将 replan 理由写入上下文，不执行外部操作，让下轮 Observe 重新评估

  Act 的执行结果写入上下文，供下一轮 Observe 使用。

### Task 3: 推理循环分支与 Spawn 集成（AC: #1-#4）

- [x] 3.1 修改 `kernel/kernel.go` 的 Spawn 方法，支持 OODA 模式分支：

  在 Spawn 的推理循环启动部分（当前 `go k.reasonStep(proc, llmFD, opts)` 处），根据进程是否启用 OODA 选择循环：

  ```go
  if !opts.SkipReasonLoop {
      if proc.oodaEnabled {
          go k.oodaReasonStep(proc, llmFD, opts)
      } else {
          go k.reasonStep(proc, llmFD, opts)
      }
  }
  ```

- [x] 3.2 在 `SpawnOpts` 新增 `ReasoningMode string` 字段（值 `""` = 默认线性，`"ooda"` = OODA 循环）：

  ```go
  type SpawnOpts struct {
      // ... existing fields ...
      ReasoningMode string // "" = linear (default), "ooda" = OODA loop
  }
  ```

  在 Spawn 中根据 `opts.ReasoningMode` 设置 `proc.oodaEnabled` 和初始化 `proc.oodaState`。

### Task 4: OODA System Prompt 构建（AC: #1-#4）

- [x] 4.1 在 `kernel/ooda.go` 定义 OODA 各阶段的 prompt 模板：

  ```go
  const oodaObservePromptTemplate = `[OODA Observe] Environment snapshot:
  %s
  Analyze this data relative to your mission: %s`

  const oodaOrientPromptTemplate = `[OODA Orient] Based on observations, evaluate:
  1. Current state vs. desired state
  2. Key deviations and their significance
  3. Available resources and constraints`

  const oodaDecidePromptTemplate = `[OODA Decide] Based on orientation analysis, output a JSON decision:
  {"action": "tool_call|spawn|complete|replan", "target": "...", "data": {}, "reason": "..."}`
  ```

- [x] 4.2 在 Orient/Decide/Act 阶段，通过 `k.ctxMgr.AppendMessage()` 将阶段 prompt 注入上下文，确保 LLM 具有完整的 OODA 上下文链。

### Task 5: SyscallEvent 与 Log 集成（AC: #1-#4）

- [x] 5.1 为 OODA 各阶段添加 `emitEvent` 调用，事件类型使用 `OODAObserve`/`OODAOrient`/`OODADecide`/`OODAAct`：

  ```go
  k.emitEvent(proc, "OODAObserve", map[string]any{
      "cycle":        cycle,
      "observations": len(observations),
  }, observations, nil, observeDuration)
  ```

- [x] 5.2 为 OODA 各阶段添加 `emitLog` 调用，使用新的 `LogCategory`：

  在 `internal/types/types.go` 新增 `LogOODA LogCategory = "ooda"`。emitLog 输出格式：`[ooda:observe] ...`、`[ooda:orient] ...`。

### Task 6: 测试（全部 AC + NFR41）

- [x] 6.1 `kernel/ooda_test.go` — OODA 类型测试：
  - `TestOODAPhase_Constants` — 阶段常量正确性
  - `TestOODADecision_Types` — 决策类型完整性
  - `TestProcess_OODAState` — Process 的 OODA 状态读写线程安全

- [x] 6.2 `kernel/ooda_test.go` — OODA 循环集成测试：
  - `TestOODAReasonStep_SingleCycle` — 单轮 OODA 循环（Observe→Orient→Decide(complete)→Act），验证正常完成
  - `TestOODAReasonStep_MultipleCycles` — 多轮循环（tool_call → complete），验证循环反馈
  - `TestOODAReasonStep_SpawnAction` — Decide 选择 spawn，验证子进程创建
  - `TestOODAReasonStep_ReplanAction` — Decide 选择 replan，验证重新规划不执行外部操作
  - `TestOODAReasonStep_MaxCyclesExceeded` — 超过最大循环数，验证正确终止
  - `TestOODAReasonStep_ContextCancellation` — OODA 循环中 context 取消，验证正确退出
  - `TestOODAReasonStep_ObserveError` — Observe 阶段 VFS 读取失败，验证错误处理

- [x] 6.3 `kernel/ooda_test.go` — NFR41 性能测试：
  - `TestOODAReasonStep_FrameworkOverhead` — 验证单轮 OODA 框架开销 <= 200ms（使用 mock LLM 即时返回，测量纯框架代码耗时）

- [x] 6.4 `kernel/ooda_test.go` — 兼容性测试：
  - `TestSpawn_DefaultReasoningMode` — 不设置 ReasoningMode 时使用默认线性推理，确保不破坏现有行为
  - `TestSpawn_OODAReasoningMode` — 设置 `ReasoningMode: "ooda"` 时启用 OODA 循环

- [x] 6.5 OODA 事件和日志测试：
  - `TestOODAReasonStep_SyscallEvents` — 验证 OODA 各阶段产生正确的 SyscallEvent
  - `TestOODAReasonStep_LogEntries` — 验证 OODA 各阶段产生正确的 LogEntry

## Dev Notes

### 核心设计决策

**OODA 循环作为独立的推理模式，与现有 reasonStep 并行存在，不修改 reasonStep 代码。** 这是最安全的接入策略——reasonStep 从 Epic 1 开始就是核心代码路径，经过 19 个 Epic 的验证，零修改保留。OODA 通过 Spawn 时的 `ReasoningMode` 字段选择，在 goroutine 启动点分叉。

**OODA 四阶段通过多轮 LLM 调用实现，每个阶段（Orient/Decide）都是一次独立的 LLM 调用。** Observe 是纯框架代码（VFS 读取，不调用 LLM），Act 根据决策类型可能调用工具或 Spawn。这意味着一轮 OODA 循环消耗 2-3 次 LLM 调用（Orient + Decide + 可选的 Act LLM），token 消耗高于线性模式。

**MaxSteps 复用为 MaxCycles。** OODA 模式下 `proc.MaxSteps` 表示最大循环轮数而非推理步数。每轮循环内部的 LLM 调用次数不受此限制，但受 ContextBudget 保护。

### 架构合规

- **依赖方向**：OODA 代码全部在 `kernel/` 包内，不引入新的外部依赖。使用 `k.vfs`/`k.ctxMgr` 访问 VFS 和上下文，遵循现有依赖方向。
- **接口设计**：不新增 Kernel 子接口。OODA 是推理循环的内部实现，不暴露新的 syscall。
- **并发安全**：OODA 状态字段通过 `proc.mu` 保护，与现有的 Process 锁机制一致。
- **事件系统**：复用 `emitEvent`/`emitLog` 基础设施。新增 `LogOODA` 类别需在 `internal/types/` 添加。

### 关键文件变更

| 文件 | 变更类型 | 说明 |
|------|---------|------|
| `kernel/ooda.go` | **新建** | OODA 类型定义 + oodaReasonStep + 四阶段实现 |
| `kernel/ooda_test.go` | **新建** | OODA 全部测试 |
| `kernel/process.go` | 修改 | Process 新增 oodaEnabled/oodaState 字段和访问方法 |
| `kernel/kernel.go` | 修改 | SpawnOpts 新增 ReasoningMode；Spawn 中 OODA 模式分支 |
| `internal/types/log.go` | 修改 | 新增 LogOODA 常量 |

### 复用模式

- **LLM 调用模式**：OODA 的 Orient/Decide 复用 `reasonStep` 内的 VFS Write/Read LLM 调用模式（`llmRequest` → JSON marshal → `k.vfs.Write` → `k.vfs.Read` → `llmResponse` unmarshal）
- **工具调用模式**：OODA Act 的 `OODAToolCall` 复用 `reasonStep` 的 VFS Open/Write/Read/Close 工具调用链
- **子进程 Spawn**：OODA Act 的 `OODASpawn` 使用 `k.Spawn()` API，与 compose/intent 使用同一接口
- **事件/日志**：复用 `k.emitEvent()`/`k.emitLog()` 基础设施

### 测试策略

- 使用 mock LLM 驱动（已在 `kernel/kernel_test.go` 中建立的模式）
- NFR41 性能测试使用 mock LLM 即时返回，仅测量框架代码开销
- 兼容性测试确保不设置 ReasoningMode 时走默认路径（回归保护）
- 所有测试启用 `-race`

### 从 Epic 19 继承的经验

- **接口隔离 + 适配器模式**：OODA 不需要新接口，直接使用 kernel 内部方法（`k.vfs`/`k.ctxMgr`）。如果未来 OODA 需要从 IPC 暴露（如 `rnix ooda status`），参考 intent/ 的 Manager 接口模式
- **事件驱动优先于轮询**：OODA 的循环本身是主动的（for 循环驱动），不需要事件驱动。但 Observe 阶段的环境感知可以考虑 channel 订阅模式（本 Story 暂不实现，留给 20.2）
- **先验证后合并**：OODA 的 Decide 输出需要验证 JSON 格式有效性后才执行 Act。解析失败时进入 replan 模式

### IPC 扩展标准六步法（本 Story 不需要）

本 Story 不新增 IPC 方法。OODA 循环是进程内部推理模式，不需要客户端控制。IPC 扩展留给 Story 20.2（agent.yaml 配置）需要时按六步法添加。

### Project Structure Notes

- 新建文件 `kernel/ooda.go` 和 `kernel/ooda_test.go` 遵循 kernel 包的文件组织规则（单一职责）
- OODA 类型定义与实现同文件，避免文件碎片化
- `internal/types/log.go` 的 LogOODA 常量遵循现有 LogCategory 定义模式

### References

- [Source: kernel/kernel.go#reasonStep] — 现有线性推理循环实现（第 512-979 行）
- [Source: kernel/kernel.go#Spawn] — Spawn 流程和推理循环启动点（第 157-400 行）
- [Source: kernel/process.go#Process] — Process 结构体定义（第 32-97 行）
- [Source: agents/types.go] — AgentManifest 结构体（20.2 将扩展 reasoning 字段）
- [Source: _bmad-output/planning-artifacts/epics/epic-20] — Epic 20 完整定义
- [Source: _bmad-output/planning-artifacts/architecture/core-architectural-decisions.md#Decision2] — 进程模型与并发决策
- [Source: _bmad-output/implementation-artifacts/epic-19-retro-2026-03-10.md] — Epic 19 回顾（OODA 准备度评估）

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6

### Debug Log References

- Fixed ATDD test bug: `k.Kill(pid)` missing second argument, corrected to `k.Kill(pid, types.SIGKILL)`
- Fixed ATDD test `TestOODAReasonStep_SpawnAction`: Added missing child process LLM response (call 3) in sequence since child consumes one LLM call from shared mock
- Updated `SetOODAPhase` to auto-initialize OODAState when called on non-OODA process (matching ATDD expectations)

### Completion Notes List

- Task 1: Defined OODAPhase (observe/orient/decide/act), OODAActionType (tool_call/spawn/complete/replan), OODAState, OODADecision types in kernel/ooda.go. Added oodaEnabled/oodaState fields to Process with thread-safe accessors IsOODA(), GetOODAState(), SetOODAPhase().
- Task 2: Implemented oodaReasonStep with 4 phases. Observe collects environment via ListProcesses+GetContextInfo (no LLM). Orient/Decide each make one LLM call via oodaCallLLM helper. Act dispatches by action type (tool_call, spawn, complete, replan). Context cancellation and max cycle limits handled.
- Task 3: Added ReasoningMode field to SpawnOpts. Spawn initializes oodaEnabled/oodaState when mode="ooda" and launches oodaReasonStep goroutine instead of reasonStep. Zero modifications to existing reasonStep code path.
- Task 4: Defined oodaOrientPromptTemplate and oodaDecidePromptTemplate constants. All phase prompts injected via k.ctxMgr.AppendMessage() maintaining full OODA context chain.
- Task 5: All 4 phases emit emitEvent (OODAObserve/OODAOrient/OODADecide/OODAAct) plus OODACycle summary with framework_overhead. All phases emit emitLog with LogOODA category and [ooda:phase] prefix format.
- Task 6: All 16 ATDD tests pass with race detection. Covers type constants, struct creation, thread-safe state access, single/multi cycle, spawn/replan/tool_call actions, max cycles exceeded, context cancellation, observe error handling, NFR41 framework overhead, default vs OODA mode compatibility, syscall events, and log entries.

### File List

| File | Change | Description |
|------|--------|-------------|
| kernel/ooda.go | New | OODA types, oodaReasonStep, 4-phase implementation, prompt templates, oodaCallLLM helper |
| kernel/ooda_test.go | Modified | Fixed ATDD test bug (Kill args), fixed SpawnAction test sequence, all 16 tests GREEN |
| kernel/process.go | Modified | Added oodaEnabled/oodaState fields, IsOODA/GetOODAState/SetOODAPhase methods |
| kernel/kernel.go | Modified | Added ReasoningMode to SpawnOpts, OODA branch in Spawn goroutine launch |
| internal/types/types.go | Modified | Added LogOODA LogCategory constant |

## Change Log

- 2026-03-10: Story 20.1 implementation complete. Added OODA reasoning loop as parallel mode to linear reasonStep. All 16 tests pass with -race. No regressions in full test suite.
- 2026-03-10: Code review (adversarial). Fixed: (H1) duplicate OODADecide events on parse failure; (H2) silently swallowed AppendToolResult error in oodaActToolCall; (M1) incorrect file reference in Task 5.2; (M2) added inter-phase context cancellation checks. All tests pass post-fix.
