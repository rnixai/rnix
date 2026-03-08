# Story 15.3: Trace Blame 根因定位

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a 平台构建者,
I want 通过 `rnix trace blame <trace-id>` 自动分析追踪数据定位关键路径节点,
So that 我可以快速找到耗时最长、消耗最大或产生错误的瓶颈。

## Acceptance Criteria

1. **Given** 一个有效的 Trace ID
   **When** 用户执行 `rnix trace blame <trace-id>`
   **Then** 系统自动分析并高亮标记：耗时最长的节点、token 消耗最大的节点、产生错误的关键路径

2. **Given** blame 分析结果
   **When** 结果中包含错误节点
   **Then** 显示错误传播链路（从根因到最终失败的完整路径）

3. **Given** 用户使用 `--json` 标志
   **When** 执行 `rnix trace blame <trace-id> --json`
   **Then** 系统以 JSON 格式输出 blame 分析结果

4. **Given** 用户传入不存在的 Trace ID
   **When** 执行 `rnix trace blame <invalid-trace-id>`
   **Then** 系统返回友好的错误信息

5. **Given** 一个全部 Span 状态为 OK 的 Trace
   **When** 用户执行 `rnix trace blame <trace-id>`
   **Then** 系统仍然输出耗时和 token 消耗分析，错误链路段落为空

## Tasks / Subtasks

- [x] Task 1: Blame 分析引擎 (AC: #1, #2, #5)
  - [x] 1.1 在 `debug/trace_blame.go` 中定义 `BlameResult` 结构体：
    ```go
    type BlameResult struct {
        TraceID          string
        CriticalPath     []*SpanNode       // 从根到叶的最长耗时路径
        CriticalDuration time.Duration     // 关键路径总耗时
        DurationHotspots []*BlameEntry     // 耗时 Top-N 节点
        TokenHotspots    []*BlameEntry     // Token 消耗 Top-N 节点
        ErrorChains      []ErrorChain      // 错误传播链（可能多条）
        Summary          BlameSummary      // 汇总统计
    }
    ```
  - [x] 1.2 定义 `BlameEntry` 结构体：
    ```go
    type BlameEntry struct {
        Span       *Span
        Percentage float64    // 占总量百分比
        Rank       int        // 排名（1-based）
    }
    ```
  - [x] 1.3 定义 `ErrorChain` 结构体：
    ```go
    type ErrorChain struct {
        Path      []*Span    // 从根因到最终失败的完整路径
        RootCause *Span      // 根因节点（链路中最先出错的 Span）
    }
    ```
  - [x] 1.4 定义 `BlameSummary` 结构体：
    ```go
    type BlameSummary struct {
        TotalSpans       int
        ErrorSpans       int
        TotalDuration    time.Duration
        TotalTokens      int
        CriticalPathPct  float64  // 关键路径耗时占总耗时百分比
    }
    ```
  - [x] 1.5 实现 `AnalyzeTrace(tree *SpanTree) *BlameResult`：
    - 调用 `findCriticalPath` 计算关键路径
    - 调用 `findDurationHotspots` 找耗时 Top-3
    - 调用 `findTokenHotspots` 找 Token 消耗 Top-3
    - 调用 `findErrorChains` 分析错误传播链
    - 构建 BlameSummary
  - [x] 1.6 实现 `findCriticalPath(tree *SpanTree) ([]*SpanNode, time.Duration)`：
    - 从根节点遍历，对每条路径累加 Duration
    - 返回累计耗时最长的根到叶路径
  - [x] 1.7 实现 `findDurationHotspots(tree *SpanTree, topN int) []*BlameEntry`：
    - 遍历所有 Span，按 Duration 降序排列
    - 返回 Top-N，计算每个节点占 TotalDuration 的百分比
  - [x] 1.8 实现 `findTokenHotspots(tree *SpanTree, topN int) []*BlameEntry`：
    - 遍历所有 Span，按 TokensUsed 降序排列
    - 返回 Top-N，计算每个节点占 TotalTokens 的百分比
  - [x] 1.9 实现 `findErrorChains(tree *SpanTree) []ErrorChain`：
    - 查找所有状态为 ERROR 或 TIMEOUT 的叶节点
    - 对每个错误叶节点，回溯到根节点，记录完整路径
    - 路径中最先出错的 Span 标记为 RootCause

- [x] Task 2: Blame 格式化输出 (AC: #1, #2)
  - [x] 2.1 在 `debug/trace_blame.go` 中实现 `FormatBlameResult(result *BlameResult) string`：
    ```
    Blame: abcdef1234567890  |  5 spans  |  2 errors

    ── Critical Path (75.2% of total) ──────────────────────
    → orchestrator (PID 1)                    5.2s   800 tok
    → code-analyst (PID 2)                    3.1s   500 tok
      Total: 8.3s / 11.0s

    ── Duration Hotspots ───────────────────────────────────
    #1  orchestrator (PID 1)          5.2s   47.3%
    #2  code-analyst (PID 2)          3.1s   28.2%
    #3  reviewer (PID 3)              2.8s   25.5%

    ── Token Hotspots ──────────────────────────────────────
    #1  orchestrator (PID 1)          800 tok   53.3%
    #2  code-analyst (PID 2)          500 tok   33.3%
    #3  reviewer (PID 3)              200 tok   13.3%

    ── Error Chains ────────────────────────────────────────
    Chain 1:
      ✗ reviewer (PID 3) [ROOT CAUSE]          error
      ↑ orchestrator (PID 1)                    ok
    ```
  - [x] 2.2 关键路径使用 `→` 箭头连接各节点，显示每个节点的 Name、PID、Duration、Tokens
  - [x] 2.3 Hotspots 以 `#N` 排名格式展示，附百分比
  - [x] 2.4 Error Chains 使用 `✗` 标记错误节点、`↑` 标记传播方向，`[ROOT CAUSE]` 高亮根因
  - [x] 2.5 无错误时省略 Error Chains 段落

- [x] Task 3: JSON 序列化 (AC: #3)
  - [x] 3.1 为 `BlameResult` 实现 `MarshalJSON`，使用 snake_case JSON 字段：
    ```json
    {
      "trace_id": "abc...",
      "critical_path": [...],
      "duration_hotspots": [...],
      "token_hotspots": [...],
      "error_chains": [...],
      "summary": {...}
    }
    ```
  - [x] 3.2 时间字段使用 `_ms` 后缀毫秒值（与 Span 序列化模式一致）
  - [x] 3.3 百分比字段保留一位小数

- [x] Task 4: CLI `blame` 子命令 (AC: #1-4)
  - [x] 4.1 在 `cmd/rnix/trace.go` 中新增 `blameCmd` 作为 `traceCmd` 的子命令：
    ```go
    var blameCmd = &cobra.Command{
        Use:   "blame <trace-id>",
        Short: "Analyze trace to find bottlenecks and root causes",
        Args:  cobra.ExactArgs(1),
        RunE:  runBlame,
    }
    ```
  - [x] 4.2 在 `init()` 中注册：`traceCmd.AddCommand(blameCmd)`
  - [x] 4.3 实现 `runBlame(cmd *cobra.Command, args []string) error`：
    - 读取 spans → BuildSpanTree → AnalyzeTrace → FormatBlameResult 或 JSON 输出
    - 错误处理与 `runTraceView` 保持一致（`[trace]` 前缀 + JSONResponse）
  - [x] 4.4 支持 `--json` 全局标志，输出 `JSONResponse` 包装的 BlameResult

- [x] Task 5: 测试 (AC: #1-5)
  - [x] 5.1 `debug/trace_blame_test.go`：AnalyzeTrace 测试
    - 简单线性链（3 个节点 A→B→C）：关键路径 = 全链路，hotspots 排名验证
    - 分支树（root→[child1, child2]，child1 更耗时）：关键路径选择 child1
    - 全 OK 场景：ErrorChains 为空
    - 全错误场景：多条 ErrorChain 验证
    - 混合场景：部分节点 ERROR，验证 RootCause 标记正确
    - Token hotspots 排名独立于 Duration 排名
  - [x] 5.2 `debug/trace_blame_test.go`：findCriticalPath 测试
    - 深度嵌套树（4 层）：正确找到最长路径
    - 单节点树：关键路径只有根节点
  - [x] 5.3 `debug/trace_blame_test.go`：findErrorChains 测试
    - 单个错误叶节点：一条 ErrorChain，RootCause 是叶节点本身
    - 中间节点和叶节点都出错：RootCause 是最先出错的节点（时间最早的 ERROR Span）
    - 多条独立错误链：返回多个 ErrorChain
  - [x] 5.4 `debug/trace_blame_test.go`：FormatBlameResult 测试
    - 验证段落标题正确
    - 验证排名格式（#1, #2, #3）
    - 验证百分比计算正确
    - 无错误时无 Error Chains 段落
    - 有错误时 `[ROOT CAUSE]` 标记存在
  - [x] 5.5 `cmd/rnix/trace_test.go`：blame CLI 集成测试
    - `trace blame <valid-id>` → 输出含 "Critical Path" 和 "Duration Hotspots"
    - `trace blame <invalid-id>` → 错误信息
    - `trace blame <valid-id> --json` → JSON 输出

## Dev Notes

### 架构决策

本 Story 是 Epic 15（分布式追踪）的第三层，基于 15-1 的 Span 基础设施和 15-2 的 SpanTree 构建能力，实现自动化根因分析。核心设计原则：

1. **复用 SpanTree** — 15-2 已实现 `BuildSpanTree`，blame 分析在 SpanTree 上操作，不需要重新构建树
2. **分析 + 格式化分离** — `AnalyzeTrace` 产生 `BlameResult` 数据结构，`FormatBlameResult` 负责人类可读输出，JSON 序列化独立
3. **子命令模式** — `blame` 作为 `trace` 的子命令（`rnix trace blame <id>`），遵循 Cobra 子命令层级
4. **本地文件操作** — 与 15-2 一致，blame 命令直接读取 `.rnix/traces/` 目录下的 JSONL 文件，不需要连接 daemon

### 关键设计：关键路径分析

```
SpanTree:
  Root: orchestrator (5.2s)
  ├─ code-analyst (3.1s)
  └─ reviewer (2.8s)

Critical Path = 最长路径 = orchestrator → code-analyst = 8.3s
（不是 orchestrator → reviewer = 8.0s）
```

- 从根节点开始 DFS 遍历
- 每条路径从根到叶累加 Duration
- 返回累计耗时最长的路径
- 关键路径百分比 = 关键路径耗时 / Trace 总耗时

### 关键设计：错误传播链

```
Root: orchestrator (ok)
├─ code-analyst (ok)
└─ reviewer (error) ← ROOT CAUSE

Error Chain: reviewer(error) ↑ orchestrator(ok)
```

- 查找所有状态为 ERROR 或 TIMEOUT 的 Span
- 对每个错误 Span，从该节点回溯到根节点构建路径
- 路径中最先出错的 Span（StartTime 最早的 ERROR/TIMEOUT）标记为 RootCause
- 如果整条链路只有一个错误节点，RootCause 就是该节点自身

### 关键设计：Hotspot 排名

- Duration Hotspots：按每个 Span 自身的 Duration 降序排列（不是累计路径时间），Top-3
- Token Hotspots：按每个 Span 的 TokensUsed 降序排列，Top-3
- 两种排名独立计算，同一个 Span 可能同时出现在两个列表中
- 百分比 = 该 Span 的值 / Trace 总量 × 100

### 关键复用点

1. **SpanTree + BuildSpanTree** — `debug/trace_view.go:108-188`（15-2 已实现），blame 分析在 SpanTree 上操作
2. **SpanTree.Walk** — `debug/trace_view.go:192-204`，遍历 SpanTree 用于收集所有 Span
3. **SpanReader.ReadSpans** — `debug/trace.go:208-235`（15-1 已实现），读取 Span 数据
4. **findTraceBaseDir** — `cmd/rnix/trace.go:108-111`（15-2 已实现），获取 trace 基础目录
5. **JSONResponse** — `cmd/rnix/main.go` 中定义的统一 JSON 响应包装
6. **runTraceView 错误处理模式** — `cmd/rnix/trace.go:69-106`，blame 命令的错误处理参考
7. **formatDuration** — `debug/strace.go` 中已有的时间格式化函数
8. **Cobra 子命令模式** — `cmd/rnix/trace.go` 的 traceCmd 注册模式

### 不要做的事情

- **不要**修改 `BuildSpanTree` 或 `SpanTree` 的现有接口 — blame 分析使用 SpanTree 的公开 API
- **不要**修改 `SpanReader`/`SpanWriter` — blame 使用已有的读取接口
- **不要**引入新的外部依赖
- **不要**实现 context profiling（Story 15-4 的范围）
- **不要**实现 context 增长预测（Story 15-5 的范围）
- **不要**连接 daemon — blame 命令是纯本地文件操作
- **不要**修改 Bubble Tea TUI 框架
- **不要**在 `internal/ui/` 中添加新文件 — blame 格式化放在 `debug/trace_blame.go` 中（与 `debug/trace_view.go` 的 FormatTraceTree 模式一致）
- **不要**修改现有的 `cmd/rnix/trace.go` 中的 `traceCmd` Use/Short/Long 字段 — 只通过 `traceCmd.AddCommand` 添加子命令
- **不要**在 `internal/ui/styles.go` 中新增颜色常量 — 文本输出使用纯文本符号（`→`、`✗`、`↑`、`#`）

### IPC 协议变更

无。本 Story 不涉及 IPC 协议变更，blame 命令是纯本地文件操作。

### 组合矩阵

| 本功能 | 交互对象 | 交互方式 | 需验证 |
|--------|----------|----------|--------|
| blame 命令 | SpanReader | 集成：复用 ReadSpans 读取 Span 数据 | 是 |
| blame 命令 | BuildSpanTree | 集成：复用 SpanTree 作为分析输入 | 是 |
| blame 命令 | FormatTraceTree | 独立：blame 有自己的格式化输出 | 否 |
| blame 命令 | trace 命令 | 共存：blame 是 trace 的子命令，两者独立运行 | 是 |
| blame --json | JSONResponse | 集成：使用统一 JSON 包装 | 是 |
| blame 命令 | strace 命令 | 独立：不同的数据源和分析方式 | 否 |
| blame 命令 | replay 命令 | 独立：不同的数据源 | 否 |
| blame 错误链 | 15-4 ctx-profile | 预留：BlameResult 结构可供 15-4 复用 | 否 |

### Project Structure Notes

新建文件：
- `debug/trace_blame.go` — BlameResult、BlameEntry、ErrorChain、BlameSummary 类型；AnalyzeTrace、FormatBlameResult 及内部分析函数
- `debug/trace_blame_test.go` — 所有 blame 分析相关测试

修改文件：
- `cmd/rnix/trace.go` — 新增 blameCmd 子命令、runBlame 函数、init 中注册
- `cmd/rnix/trace_test.go` — 新增 blame CLI 集成测试

### References

- [Source: debug/trace_view.go:108-188] — BuildSpanTree（15-2 已实现，blame 分析复用）
- [Source: debug/trace_view.go:192-204] — SpanTree.Walk（遍历收集所有 Span）
- [Source: debug/trace_view.go:14-34] — SpanNode、TraceMetadata、SpanTree 类型
- [Source: debug/trace.go:71-84] — Span 结构体（blame 分析的数据源）
- [Source: debug/trace.go:208-235] — SpanReader.ReadSpans（读取 Span 数据）
- [Source: cmd/rnix/trace.go:13-28] — traceCmd 定义（blame 注册为其子命令）
- [Source: cmd/rnix/trace.go:69-106] — runTraceView 错误处理模式（blame 参考）
- [Source: cmd/rnix/trace.go:108-111] — findTraceBaseDir（blame 复用）
- [Source: debug/trace_view.go:66-83] — SpanTree.MarshalJSON（JSON 序列化模式参考）
- [Source: _bmad-output/planning-artifacts/prd/functional-requirements.md:150] — FR83: blame 根因定位
- [Source: _bmad-output/planning-artifacts/architecture/core-architectural-decisions.md:368-382] — Decision 11: 分布式追踪传播

### 技术栈

- Go 1.26 — 标准库满足所有需求
- `encoding/json` — BlameResult JSON 序列化
- `fmt` / `strings` — blame 格式化输出
- `sort` — Hotspot 排序
- `time` — 时间格式化
- `github.com/spf13/cobra` — CLI 子命令注册
- 无新增外部依赖

### 前置 story 学习总结

**来自 Story 15-1：**
1. SpanReader 已就绪 — `ReadSpans(traceID)` 返回 `[]*Span`，直接复用
2. Span 存储在 `.rnix/traces/<trace-id>/spans.jsonl`，每行一个 JSON
3. 时间字段使用 `_ms` 后缀毫秒值（spanJSON 序列化模式）
4. SpanStatus 有三种：`ok`、`error`、`timeout`，已实现 JSON 序列化
5. Span 的 ParentSpanID 为空表示根节点

**来自 Story 15-2：**
1. BuildSpanTree 从扁平 Span 列表构建树形结构 — blame 直接在 SpanTree 上分析
2. SpanTree.Walk 提供深度优先遍历 — 用于收集所有 Span 做排序分析
3. SpanTree.MarshalJSON 已实现 — BlameResult 的 JSON 序列化参考其模式
4. traceCmd 已注册 — blame 作为 traceCmd.AddCommand 子命令注册
5. findTraceBaseDir 已实现 — blame 命令复用
6. FormatTraceTree 使用 box-drawing 字符 — blame 使用不同的符号（`→`、`✗`、`↑`）区分
7. 复用 strace.go 中已有的 formatDuration 函数

**来自 Git 提交分析：**
- 提交消息格式：`feat: complete story X-Y - description`
- debug 包文件命名约定：`trace.go`、`trace_view.go` → 新文件 `trace_blame.go`
- 修改影响面较小：本 Story 主要是新增文件 + 小幅修改 trace.go CLI

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6 (claude-opus-4-6)

### Debug Log References

- debug/trace_blame_test.go: 17 个测试全部通过（AnalyzeTrace、findCriticalPath、findErrorChains、FormatBlameResult、MarshalJSON）
- cmd/rnix/trace_test.go: 4 个新增 blame CLI 测试全部通过
- 全项目 18 包测试通过（-race 检测），2 个预存 TTY 测试失败不受影响

### Completion Notes List

- AnalyzeTrace 在 SpanTree 上执行关键路径分析、耗时/Token Hotspot 排名、错误链追踪
- findCriticalPath 使用 DFS 遍历累加路径 Duration，返回最长路径
- findErrorChains 查找 ERROR/TIMEOUT 叶节点，回溯到根构建错误传播链，最先出错的标记为 RootCause
- FormatBlameResult 使用文本符号（→、✗、↑、#）格式化输出，无错误时省略 Error Chains 段落
- BlameResult.MarshalJSON 使用 snake_case、_ms 后缀毫秒值（与 SpanTree 序列化模式一致）
- blame 命令作为 trace 的 Cobra 子命令注册（`rnix trace blame <trace-id>`）
- 复用 strace.go 中已有的 formatDuration 函数
- 复用 15-2 已有的 SpanTree、BuildSpanTree、SpanReader、findTraceBaseDir

### File List

新建文件:
- `debug/trace_blame.go` — BlameResult、BlameEntry、ErrorChain、BlameSummary 类型；AnalyzeTrace、FormatBlameResult、MarshalJSON 及内部分析函数
- `debug/trace_blame_test.go` — 17 个 blame 分析测试

修改文件:
- `cmd/rnix/trace.go` — 新增 blameCmd 子命令、runBlame 函数、init 中 traceCmd.AddCommand(blameCmd)
- `cmd/rnix/trace_test.go` — 新增 4 个 blame CLI 集成测试
