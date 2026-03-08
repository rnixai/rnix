# Story 15.5: 上下文增长预测与告警

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a 平台构建者,
I want 系统预测当前上下文的增长趋势并在预计耗尽前告警,
So that 我可以提前采取措施避免 token 预算耗尽导致推理中断。

## Acceptance Criteria

1. **Given** 智能体正在执行且有 token 预算
   **When** 系统检测到上下文增长趋势
   **Then** 基于历史增长速率预测何时耗尽预算

2. **Given** 预测结果显示即将耗尽（剩余 < 20%）
   **When** 告警条件触发
   **Then** 系统发出告警，显示当前消耗/总额、百分比和预估剩余步数

3. **Given** 用户执行 `rnix ctx-growth <pid>`
   **When** 进程存在且为 Running 状态
   **Then** 系统展示增长趋势、预测数据和告警状态

4. **Given** 用户传入不存在的 PID 或非 Running 状态的进程
   **When** 执行 `rnix ctx-growth <pid>`
   **Then** 系统返回友好的错误信息

5. **Given** 用户使用 `--json` 标志
   **When** 执行 `rnix ctx-growth <pid> --json`
   **Then** 系统以 JSON 格式输出增长预测结果

## Tasks / Subtasks

- [x] Task 1: Token 历史跟踪 (AC: #1)
  - [x] 1.1 在 `kernel/process.go` 中定义 `TokenSnapshot` 结构体：
    ```go
    type TokenSnapshot struct {
        Step   int   // reasoning step number
        Tokens int   // cumulative tokens at this step
        DeltaMs int64 // milliseconds since process creation
    }
    ```
  - [x] 1.2 在 `Process` 结构体中添加 token 历史环形缓冲区字段（mu 保护）：
    ```go
    tokenHistory []TokenSnapshot // ring buffer, max 50 entries
    tokenHistIdx int             // ring buffer write position
    tokenHistLen int             // current valid entry count
    ```
  - [x] 1.3 实现 `Process.appendTokenSnapshot(step, tokens int)` 方法（mu 调用者已持有锁或由内部持有）：
    - 计算 DeltaMs = `time.Since(proc.CreatedAt).Milliseconds()`
    - 写入 tokenHistory[tokenHistIdx]，循环递增 idx，更新 len（max 50）
  - [x] 1.4 实现 `Process.GetTokenHistory() []TokenSnapshot`（加锁读取，返回按时序排列的副本）
  - [x] 1.5 在 `kernel/kernel.go` 的 `reasonStep` 中，在 `proc.TokensUsed += resp.TokensUsed`（line 689）之后、budget 检查之前，调用 `proc.appendTokenSnapshot(step, tokens)`
  - [x] 1.6 在 `KernelImpl` 上实现 `GetTokenHistory(pid types.PID) ([]TokenSnapshot, error)`：
    - 从 procTable 获取进程
    - 调用 `proc.GetTokenHistory()` 返回副本

- [x] Task 2: 增长预测引擎 (AC: #1)
  - [x] 2.1 创建 `debug/ctx_growth.go`，定义预测结果类型：
    ```go
    type GrowthPrediction struct {
        PID              types.PID
        TokensUsed       int
        ContextBudget    int
        UsagePct         float64   // TokensUsed / ContextBudget * 100
        CurrentStep      int
        MaxSteps         int
        AvgTokensPerStep float64   // TokensUsed / CurrentStep
        RecentRate       float64   // last 5 steps moving average
        RemainingBudget  int       // ContextBudget - TokensUsed
        EstRemaining     int       // estimated remaining steps (0 = no budget or cannot predict)
        PredictExhaust   bool      // true if predicted to exhaust before MaxSteps
        AlertLevel       AlertLevel
        History          []TokenSnapshot
    }
    type AlertLevel string
    const (
        AlertNone     AlertLevel = "none"
        AlertWarning  AlertLevel = "warning"  // remaining < 20%
        AlertCritical AlertLevel = "critical" // remaining < 10%
    )
    type TokenSnapshot struct {
        Step    int   `json:"step"`
        Tokens  int   `json:"tokens"`
        DeltaMs int64 `json:"delta_ms"`
    }
    ```
  - [x] 2.2 实现 `PredictGrowth(pid types.PID, tokensUsed, contextBudget, currentStep, maxSteps int, history []kernel.TokenSnapshot) *GrowthPrediction`：
    - 计算 UsagePct = tokensUsed / contextBudget * 100（budget > 0 时）
    - 计算 AvgTokensPerStep = tokensUsed / currentStep（step > 0 时）
    - 计算 RecentRate = 最近 5 个 history 条目的 delta tokens 均值
    - 使用 RecentRate 预测 EstRemaining = remainingBudget / recentRate（recentRate > 0 时，向下取整）
    - 判断 PredictExhaust = currentStep + EstRemaining > maxSteps 时为 false，否则为 true
    - 设置 AlertLevel: remaining >= 20% → none; 10-20% → warning; < 10% → critical
    - 将 kernel.TokenSnapshot 转换为 debug.TokenSnapshot 填入 History
  - [x] 2.3 处理边界：无 history → EstRemaining=0; 无 budget → AlertNone; step=0 → 无法预测
  - [x] 2.4 使用 RecentRate 优先（反映最近趋势），回退到 AvgTokensPerStep

- [x] Task 3: 预算告警发射 (AC: #2)
  - [x] 3.1 在 `internal/types/types.go` 中新增 `LogWarning LogCategory = "warning"`
  - [x] 3.2 在 `kernel/kernel.go` 的 `reasonStep` 中，在 token 更新之后、budget_exceeded 检查之前，插入预算告警检查：
    ```go
    if budget > 0 {
        remainPct := float64(budget-tokens) / float64(budget) * 100
        if remainPct < 20 {
            avgRate := float64(tokens) / float64(step)
            estRemain := 0
            if avgRate > 0 {
                estRemain = int(float64(budget-tokens) / avgRate)
            }
            level := "warning"
            if remainPct < 10 {
                level = "critical"
            }
            k.emitLog(proc, step, types.LogWarning,
                fmt.Sprintf("Budget %s: %d/%d (%.0f%% remaining, ~%d steps left)",
                    level, tokens, budget, remainPct, estRemain), "")
            k.emitEvent(proc, "ReasonStep", map[string]any{
                "step":          step,
                "action":        "budget_warning",
                "tokens":        tokens,
                "budget":        budget,
                "remaining_pct": remainPct,
                "est_remaining": estRemain,
                "alert_level":   level,
            }, nil, nil, 0)
        }
    }
    ```
  - [x] 3.3 确保告警每步最多触发一次（不重复发射）
  - [x] 3.4 告警通过 LogChan 传递给 `rnix log` 命令，通过 SyscallEvent 传递给 `rnix strace`

- [x] Task 4: 格式化输出 (AC: #3)
  - [x] 4.1 在 `debug/ctx_growth.go` 中实现 `FormatGrowthPrediction(p *GrowthPrediction) string`：
    ```
    Context Growth: PID 1  |  1200/8000 tok  |  15.0% used

    ── Growth Trend ─────────────────────────────────────
    Step  1:    250 tok  (+250)
    Step  2:    520 tok  (+270)
    Step  3:    780 tok  (+260)
    Step  4:   1020 tok  (+240)
    Step  5:   1200 tok  (+180)

    ── Prediction ──────────────────────────────────────
    Avg Rate:     240.0 tok/step
    Recent Rate:  237.0 tok/step  (last 5 steps)
    Remaining:    6800 tok
    Est. Steps:   ~28 steps remaining
    Alert:        none ✓

    ── Budget ─────────────────────────────────────────
    [████░░░░░░░░░░░░░░░░░░░░░░░░░░] 15.0%
    ```
  - [x] 4.2 Alert 为 warning 时显示 `⚠ WARNING`，critical 时显示 `⚠ CRITICAL`
  - [x] 4.3 无 budget 时显示 `No budget set` 并省略 Prediction 和 Budget 段落
  - [x] 4.4 无 history 时只显示当前用量，省略 Growth Trend

- [x] Task 5: JSON 序列化 (AC: #5)
  - [x] 5.1 为 `GrowthPrediction` 实现 `MarshalJSON`，使用 snake_case：
    ```json
    {
      "pid": 1,
      "tokens_used": 1200,
      "context_budget": 8000,
      "usage_pct": 15.0,
      "current_step": 5,
      "max_steps": 50,
      "avg_tokens_per_step": 240.0,
      "recent_rate": 237.0,
      "remaining_budget": 6800,
      "est_remaining": 28,
      "predict_exhaust": false,
      "alert_level": "none",
      "history": [
        {"step": 1, "tokens": 250, "delta_ms": 1200},
        ...
      ]
    }
    ```
  - [x] 5.2 百分比和浮点字段保留一位小数
  - [x] 5.3 history 为空数组而非 null

- [x] Task 6: IPC 协议扩展 (AC: #3, #4)
  - [x] 6.1 在 `ipc/protocol.go` 中新增：
    ```go
    MethodCtxGrowth Method = "ctx_growth"
    type CtxGrowthRequest struct {
        PID types.PID `json:"pid"`
    }
    ```
  - [x] 6.2 在 `ipc/server.go` 的 `handleConn` switch 中增加 `case MethodCtxGrowth: s.handleCtxGrowth(conn, req.Payload)`
  - [x] 6.3 实现 `handleCtxGrowth(conn net.Conn, rawPayload json.RawMessage)`：
    - 解析 CtxGrowthRequest
    - `s.kern.GetProcInfo(req.PID)` 获取进程信息
    - 校验进程存在且 `info.State == StateRunning`（非 Running 无 token history 意义）
    - 不存在 → `ErrorPayload{Code: "NOT_FOUND"}`
    - 非 Running → `ErrorPayload{Code: "INVALID", Message: "process not running"}`
    - `s.kern.GetTokenHistory(req.PID)` 获取 token 历史
    - 计算 currentStep = len(history)（或最后一个 history 条目的 Step）
    - `debug.PredictGrowth(pid, info.TokensUsed, info.ContextBudget, currentStep, maxSteps, history)`
    - maxSteps 默认 50（与 kernel.DefaultMaxSteps 一致），从 ProcInfo 获取不到时用默认值
    - 写入 Response{OK: true, Payload: json.Marshal(result)}
  - [x] 6.4 加超时保护 `context.WithTimeout(context.Background(), 1*time.Second)`
  - [x] 6.5 在 `ipc/client.go` 中实现 `CtxGrowth(pid types.PID) (*debug.GrowthPrediction, error)`：
    - 调用 `c.call(MethodCtxGrowth, CtxGrowthRequest{PID: pid})`
    - 解析 Response.Payload 为 GrowthPrediction

- [x] Task 7: CLI `ctx-growth` 命令 (AC: #3-5)
  - [x] 7.1 在 `cmd/rnix/` 中新建 `ctx_growth.go`，定义 `ctxGrowthCmd`：
    ```go
    var ctxGrowthCmd = &cobra.Command{
        Use:   "ctx-growth <pid>",
        Short: "Predict context growth and budget alerts",
        Long:  `Show token growth trend, predict budget exhaustion, and display alert status.`,
        Example: `  rnix ctx-growth 1
  rnix ctx-growth 1 --json`,
        Args:  cobra.ExactArgs(1),
        RunE:  runCtxGrowth,
    }
    ```
  - [x] 7.2 在 `cmd/rnix/main.go` 的 `init()` 中注册：`rootCmd.AddCommand(ctxGrowthCmd)`
  - [x] 7.3 实现 `runCtxGrowth(cmd *cobra.Command, args []string) error`：
    - 解析 PID（strconv.ParseUint）
    - `ipc.Dial(ipc.SocketPath())`，daemon 不可用时友好错误
    - `client.CtxGrowth(pid)` 获取结果
    - `flagJSON` → `JSONResponse{OK: true, Data: result}` 否则 `FormatGrowthPrediction(result)`
    - 错误格式：`[ctx-growth] error: ...`（文本）或 JSONResponse（--json）
  - [x] 7.4 daemon 不可用时输出明确错误（与 runCtxProfile 一致）

- [x] Task 8: `rnix top` 集成 (AC: #2)
  - [x] 8.1 修改 `cmd/rnix/top.go` 第 374 行附近的告警阈值：从 `90/100`（90%）改为 `80/100`（80%，即剩余 < 20%）
  - [x] 8.2 在 detail view 中新增一行显示增长率信息（如有 budget）：
    - 格式：`Growth: ~N tok/step | Est. M steps left`
    - 实现方式：在 topDetailView 中，对有 budget 的进程，计算 avgRate = TokensUsed / 当前 step 数（从 `ListProcs` 无法获取 step，需从 `CtxGrowth` IPC 获取或简化为 TokensUsed / elapsed_time 近似）
    - MVP 简化：仅显示 `Budget: used/total (pct%)`，当 pct >= 80% 时 WarningStyle 渲染

- [x] Task 9: 测试 (AC: #1-5)
  - [x] 9.1 `kernel/process_test.go`：TokenSnapshot 测试
    - 空 history：GetTokenHistory 返回空切片
    - 添加 3 个 snapshot：顺序正确
    - 添加 60 个 snapshot（超过 50 上限）：只保留最近 50 个
    - 并发安全：多 goroutine 同时写入不 panic
  - [x] 9.2 `debug/ctx_growth_test.go`：PredictGrowth 测试
    - 无 budget：AlertNone，EstRemaining=0
    - 有 budget，5 步历史：正确计算 AvgRate、RecentRate、EstRemaining
    - 剩余 15%（< 20%）：AlertWarning
    - 剩余 8%（< 10%）：AlertCritical
    - 空 history：EstRemaining=0
    - 单步 history：AvgRate == RecentRate
    - PredictExhaust 判断正确
  - [x] 9.3 `debug/ctx_growth_test.go`：FormatGrowthPrediction 测试
    - 验证段落标题、趋势行格式、budget bar
    - 无 budget 时省略 Prediction 段落
    - AlertWarning 时显示 ⚠ WARNING
  - [x] 9.4 `debug/ctx_growth_test.go`：MarshalJSON 测试
    - snake_case 字段、浮点一位小数、history 空数组
  - [x] 9.5 `ipc/server_test.go`：handleCtxGrowth 集成测试
    - 有效 PID + Running → OK，result 非空
    - 无效 PID → NOT_FOUND
    - 非 Running 状态 → INVALID
  - [x] 9.6 `cmd/rnix/ctx_growth_test.go`：CLI 集成测试
    - `ctx-growth <valid-pid>` → 输出含 "Growth Trend" 或 "Prediction"
    - `ctx-growth <invalid-pid>` → 错误信息
    - `ctx-growth <pid> --json` → JSON 输出

## Dev Notes

### 架构决策

本 Story 是 Epic 15（分布式追踪与上下文分析）的最后一层，实现上下文增长预测与告警。核心设计原则：

1. **Token 历史环形缓冲区** — 在 Process 结构体中新增 50 条容量的环形缓冲区，每步记录累计 token 数和时间戳，最小内存开销
2. **预测算法** — MVP 使用双速率模型：全局均值（TokensUsed/step）+ 最近 5 步移动均值，后者优先反映最新趋势
3. **告警层级** — warning（剩余 < 20%）和 critical（剩余 < 10%），对应 Epic 需求中的 < 20% 阈值
4. **告警传递** — 通过新增 `LogWarning` 类别写入 LogChan（`rnix log` 可见）+ SyscallEvent（`rnix strace` 可见），不修改 KernelCallbacks 接口
5. **独立 IPC 方法** — `MethodCtxGrowth`，与 `MethodCtxProfile` 平行，轻量级只读查询

### 关键设计：增长预测算法

```
给定:
  history = [(step=1, tokens=250), (step=2, tokens=520), ..., (step=5, tokens=1200)]
  budget = 8000

计算:
  AvgRate = 1200 / 5 = 240 tok/step (全局均值)
  RecentRate = mean([270, 260, 240, 180]) = 237.5 tok/step (最近 4 个 delta)
  RemainingBudget = 8000 - 1200 = 6800
  EstRemaining = floor(6800 / 237.5) = 28 steps

判断:
  UsagePct = 15.0% → AlertNone
  若 UsagePct >= 80% → AlertWarning
  若 UsagePct >= 90% → AlertCritical
```

RecentRate 计算方式：取最近 min(5, len(history)) 个条目，计算相邻条目之间的 token 增量均值。若只有 1 个条目，RecentRate = AvgRate。

### 关键设计：告警发射点

在 `kernel/kernel.go` 的 `reasonStep` 中，token 更新后（line 689 `proc.TokensUsed += resp.TokensUsed`）：
1. `appendTokenSnapshot(step, tokens)` — 记录历史
2. 检查 `remainPct < 20` → 发射 LogWarning + SyscallEvent
3. 原有 `budget >= tokens` → budget_exceeded 终止进程

告警在 budget_exceeded 之前触发，给用户预警机会。

### 关键复用点

1. **Process.TokensUsed / ContextBudget** — 已有字段，直接读取
2. **KernelImpl.GetProcInfo(pid)** — 获取 ProcInfo 含 TokensUsed、ContextBudget、State
3. **emitLog / emitEvent** — 已有基础设施，新增 LogWarning 类别即可
4. **IPC 模式** — 参考 handleCtxProfile（非流式请求/响应模式）
5. **CLI 模式** — 参考 ctx_profile.go（ipc.Dial → client.Method → FormatResult 或 JSON）
6. **JSONResponse** — `cmd/rnix/main.go` 统一 JSON 包装
7. **CtxProfileResult** — 15-4 的分析结果，可作为输入（如 Leaked tokens 占比高则增长可能减缓）

### 不要做的事情

- **不要**修改 `KernelCallbacks` 接口 — 会破坏所有实现者，用 LogChan/SyscallEvent 传递告警
- **不要**在 Process 中存储 GrowthPrediction — 预测是 debug 包的计算，Process 只存储原始历史数据
- **不要**修改 `context.Manager` 或 `Context` 接口 — 增长预测基于 Process.TokensUsed，不需要操作 context
- **不要**引入新的外部依赖
- **不要**修改 trace/blame/ctx-profile 命令 — ctx-growth 是独立顶级命令
- **不要**在 `internal/ui/` 中添加新文件 — 格式化放在 `debug/ctx_growth.go`
- **不要**实现复杂预测算法（线性回归等）— MVP 使用移动均值即可
- **不要**修改 ProcInfoWire/ListProcsResponse — token history 通过独立 IPC 方法获取
- **不要**阻塞 reasonStep — appendTokenSnapshot 必须 O(1)，告警检查也是常数时间

### IPC 协议变更

- 新增 `MethodCtxGrowth = "ctx_growth"`
- 新增 `CtxGrowthRequest`（Request payload）
- Response OK 时 Payload 为 `GrowthPrediction` 的 JSON
- Server 需通过 `s.kern.GetTokenHistory(pid)` 获取历史数据

### 组合矩阵

| 本功能 | 交互对象 | 交互方式 | 需验证 |
|--------|----------|----------|--------|
| token history 记录 | reasonStep | 集成：每步后 appendTokenSnapshot | 是 |
| budget warning | emitLog / emitEvent | 集成：使用已有日志/事件基础设施 | 是 |
| LogWarning 类别 | rnix log 命令 | 集成：log 命令需展示 warning 类别 | 是 |
| ctx-growth 命令 | IPC daemon | 集成：通过新 IPC 方法查询 | 是 |
| ctx-growth --json | JSONResponse | 集成：使用统一 JSON 包装 | 是 |
| rnix top 告警 | ListProcs | 集成：修改阈值为 80% | 是 |
| ctx-growth | ctx-profile | 独立：不同命令，可同时运行 | 否 |
| ctx-growth | trace blame | 独立：不同分析维度 | 否 |
| ctx-growth | gdb breakpoint | 独立：gdb budget breakpoint 已有独立机制 | 否 |
| budget warning | budget_exceeded | 顺序：warning 在 exceeded 之前触发 | 是 |

### Project Structure Notes

新建文件：
- `debug/ctx_growth.go` — GrowthPrediction、AlertLevel、TokenSnapshot、PredictGrowth、FormatGrowthPrediction、MarshalJSON
- `debug/ctx_growth_test.go` — 预测、格式化、JSON 测试
- `cmd/rnix/ctx_growth.go` — ctxGrowthCmd、runCtxGrowth
- `cmd/rnix/ctx_growth_test.go` — CLI 集成测试

修改文件：
- `internal/types/types.go` — 新增 LogWarning LogCategory
- `kernel/process.go` — TokenSnapshot 类型、tokenHistory 字段、appendTokenSnapshot、GetTokenHistory 方法
- `kernel/kernel.go` — reasonStep 中添加 appendTokenSnapshot 调用和 budget warning 检查；GetTokenHistory 方法
- `ipc/protocol.go` — MethodCtxGrowth、CtxGrowthRequest
- `ipc/server.go` — handleConn 分支、handleCtxGrowth
- `ipc/client.go` — CtxGrowth 方法
- `cmd/rnix/main.go` — init 中 rootCmd.AddCommand(ctxGrowthCmd)
- `cmd/rnix/top.go` — 告警阈值 90% → 80%

### References

- [Source: kernel/process.go:32-55] — Process 结构体含 TokensUsed、ContextBudget、tokenHistory 环形缓冲区模式（logHistory 参考）
- [Source: kernel/kernel.go:688-714] — reasonStep 中 token 更新和 budget_exceeded 检查
- [Source: kernel/kernel.go:496-497] — OnStep callback 提供 step/maxSteps
- [Source: kernel/kernel.go:1080-1101] — emitLog 实现模式
- [Source: internal/types/types.go:157-164] — LogCategory 定义（LogThink/LogTool/LogOutput）
- [Source: ipc/protocol.go:17-36] — Method 常量定义模式
- [Source: ipc/server.go:293-355] — handleCtxProfile 实现模式（非流式 IPC）
- [Source: ipc/client.go] — CtxProfile 客户端方法模式
- [Source: debug/ctx_profile.go:2-55] — CtxProfileResult 类型参考
- [Source: cmd/rnix/ctx_profile.go:14-106] — CLI 命令注册和实现模式
- [Source: cmd/rnix/top.go:370-376] — rnix top 告警阈值（当前 90%）
- [Source: cmd/rnix/main.go:66-71] — JSONResponse 结构
- [Source: kernel/process.go:57-60] — logHistory 环形缓冲区模式参考
- [Source: vfs/proc.go:28-43] — ProcInfo 含 TokensUsed、ContextBudget、State
- [Source: _bmad-output/planning-artifacts/epics/epic-15] — Story 15.5 需求定义

### 技术栈

- Go 1.26 — 标准库满足所有需求
- `encoding/json` — GrowthPrediction 序列化
- `fmt` / `strings` — 格式化输出
- `math` — 浮点运算（Floor）
- `time` — DeltaMs 计算
- `context` — WithTimeout 保护
- `github.com/spf13/cobra` — CLI 命令注册
- 无新增外部依赖

### 前置 story 学习总结

**来自 Story 15-4（ctx-profile）：**
1. `CtxProfileResult.MarshalJSON` 使用 snake_case、空数组而非 null — GrowthPrediction 需同样处理
2. IPC: `MethodCtxProfile` 非流式请求/响应模式 — `MethodCtxGrowth` 照搬
3. CLI: `ctxProfileCmd` 作为 rootCmd 顶级命令 — `ctxGrowthCmd` 同理
4. 格式化: 段落标题用 `──`，FormatCtxProfile 用 `←` 标注 — FormatGrowthPrediction 用 `✓`/`⚠` 标注
5. 测试: debug/*_test.go 单元测试 + cmd/rnix/*_test.go 集成测试
6. 错误格式: `[command] error: ...` 文本、JSONResponse 用于 --json
7. daemon 不可用时友好错误消息

**来自 Story 15-3（trace blame）：**
1. BlameResult.MarshalJSON 使用 _ms 后缀时间字段 — GrowthPrediction 的 delta_ms 同理
2. 分析逻辑在 debug 包，CLI 在 cmd/rnix — 保持一致

**来自 kernel/process.go 的 logHistory 模式：**
1. 环形缓冲区：`logHistory []LogEntry`、`logHistIdx int`、`logHistLen int`
2. `AppendLogHistory` 写入 + 循环递增 — tokenHistory 完全参照此模式
3. mu 保护并发访问

### Git 智能

最近提交显示 Epic 15 的 Story 1-4 已全部完成：
- `1bebbe5 feat: complete story 15-4 - context usage analysis`
- `f6c12e0 feat: complete story 15-3 - trace blame root cause analysis`
- `4da2cc9 feat: complete story 15-2 - distributed tracing view`
- `74ad7fc feat: story 15-1 done`

所有 15-* story 遵循一致的模式：debug/ 包中的分析逻辑 + ipc/ 协议扩展 + cmd/rnix/ CLI 命令。

## Dev Agent Record

### Agent Model Used

Claude claude-4.6-opus (via Cursor)

### Debug Log References

- Code review: 2026-03-08, no HIGH/MEDIUM blocking issues
- Quality gate: PASS (GO), 23/23 tests passing, 5/5 AC covered

### Completion Notes List

- All 9 tasks completed
- 23 tests passing (5 kernel + 11 debug + 3 IPC + 4 CLI)
- Full regression: 18 packages pass (-race enabled)
- Budget warning integration tests (ATDD Tests 16-17) deferred: require full LLM driver mock
- maxSteps hardcoded to 50 in handleCtxGrowth (ProcInfo lacks MaxSteps field)
- rnix top threshold changed from 90% to 80% per AC#2

### File List

New files:
- `debug/ctx_growth.go` — GrowthPrediction, AlertLevel, GrowthSnapshot, PredictGrowth, FormatGrowthPrediction, MarshalJSON/UnmarshalJSON
- `debug/ctx_growth_test.go` — 11 unit tests (prediction, format, JSON)
- `cmd/rnix/ctx_growth.go` — ctxGrowthCmd, runCtxGrowth
- `cmd/rnix/ctx_growth_test.go` — 4 CLI integration tests
- `kernel/token_history_test.go` — 5 unit tests (ring buffer, concurrency, copy)

Modified files:
- `internal/types/types.go` — added LogWarning LogCategory, TokenSnapshot struct
- `kernel/process.go` — tokenHistory ring buffer fields, AppendTokenSnapshot, GetTokenHistory
- `kernel/kernel.go` — appendTokenSnapshot call in reasonStep, budget warning emitLog/emitEvent, GetTokenHistory method
- `ipc/protocol.go` — MethodCtxGrowth, CtxGrowthRequest
- `ipc/server.go` — handleCtxGrowth handler in handleConn switch
- `ipc/client.go` — CtxGrowth client method
- `cmd/rnix/top.go` — alert threshold 90% → 80%
- `ipc/server_test.go` — 3 new IPC handler tests for ctx_growth
