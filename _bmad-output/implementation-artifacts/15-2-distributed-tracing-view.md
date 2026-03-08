# Story 15.2: 分布式追踪视图

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a 平台构建者,
I want 通过 `rnix trace <trace-id>` 查看完整的分布式追踪视图,
So that 我可以一目了然地看到所有智能体的时序关系和依赖链路。

## Acceptance Criteria

1. **Given** 一个已完成的 Compose 编排的 Trace ID
   **When** 用户执行 `rnix trace <trace-id>`
   **Then** 系统展示所有参与智能体的 Span 树状视图，包含时序关系、持续时间和 token 消耗

2. **Given** 追踪视图中包含多个 Span
   **When** 用户查看视图
   **Then** Span 之间的 parent-child 关系清晰展示，可展开/折叠（树形缩进表示层级）

3. **Given** `rnix trace` 不带参数执行
   **When** 用户执行 `rnix trace`
   **Then** 系统列出所有可用的 Trace ID（从 `.rnix/traces/` 目录读取）

4. **Given** 用户传入不存在的 Trace ID
   **When** 执行 `rnix trace <invalid-trace-id>`
   **Then** 系统返回友好的错误信息

5. **Given** 用户使用 `--json` 标志
   **When** 执行 `rnix trace <trace-id> --json`
   **Then** 系统以 JSON 格式输出 Span 树数据

## Tasks / Subtasks

- [x] Task 1: Span 树构建逻辑 (AC: #1, #2)
  - [x] 1.1 在 `debug/trace_view.go` 中实现 `SpanTree` 结构体：
    ```go
    type SpanTree struct {
        Root     *SpanNode
        TraceID  types.TraceID
        Metadata TraceMetadata
    }
    type SpanNode struct {
        Span     *Span
        Children []*SpanNode
    }
    type TraceMetadata struct {
        TotalSpans   int
        TotalTokens  int
        TotalDuration time.Duration
        StartTime    time.Time
        EndTime      time.Time
        ErrorCount   int
    }
    ```
  - [x] 1.2 实现 `BuildSpanTree(spans []*Span) *SpanTree` — 从扁平 Span 列表构建树形结构
    - 以 ParentSpanID 为空的 Span 作为根节点
    - 按 ParentSpanID 建立父子映射
    - 子节点按 StartTime 排序
    - 计算 TraceMetadata 汇总统计
  - [x] 1.3 实现 `SpanTree.Walk(fn func(node *SpanNode, depth int))` — 深度优先遍历

- [x] Task 2: Trace 列表功能 (AC: #3)
  - [x] 2.1 在 `debug/trace_view.go` 中实现 `TraceSummary` 结构体：
    ```go
    type TraceSummary struct {
        TraceID     types.TraceID
        SpanCount   int
        StartTime   time.Time
        TotalDuration time.Duration
        RootSpanName string
    }
    ```
  - [x] 2.2 在 `SpanReader` 上新增 `ListTraces() ([]TraceSummary, error)` — 扫描 baseDir 下所有 `<trace-id>/spans.jsonl` 目录，返回每个 Trace 的摘要
    - 读取每个目录的 spans.jsonl 第一行获取 root span 信息
    - 统计 span 数量（行数）
    - 按 StartTime 倒序排列（最近的在前）

- [x] Task 3: Trace 树格式化输出 (AC: #1, #2)
  - [x] 3.1 在 `debug/trace_view.go` 中实现 `FormatTraceTree(tree *SpanTree, verbose bool) string` — 将 SpanTree 格式化为人类可读的树状文本：
    ```
    Trace: abcdef1234567890  |  3 spans  |  12.5s  |  1500 tokens

    ┌─ orchestrator (PID 1)                    5.2s   800 tok   ok
    │  ├─ code-analyst (PID 2)                 3.1s   500 tok   ok
    │  └─ reviewer (PID 3)                     2.8s   200 tok   error
    ```
  - [x] 3.2 使用 `└─`、`├─`、`│` 等 box-drawing 字符绘制树形连接线
  - [x] 3.3 对齐各列（Name、PID、Duration、Tokens、Status），使用固定列宽
  - [x] 3.4 错误状态的 Span 使用 `ErrorStyle` 高亮，ok 状态使用 `SuccessStyle`
  - [x] 3.5 verbose 模式下额外显示 SpanID、ParentSpanID、SyscallCount、StartTime

- [x] Task 4: Trace 列表格式化 (AC: #3)
  - [x] 4.1 在 `debug/trace_view.go` 中实现 `FormatTraceList(summaries []TraceSummary) string` — 格式化 Trace 列表：
    ```
    TRACE ID                          SPANS  DURATION  ROOT
    abcdef1234567890abcdef1234567890   3      12.5s     orchestrator
    1234567890abcdef1234567890abcdef   5      25.3s     pipeline
    ```

- [x] Task 5: CLI 命令实现 (AC: #1-5)
  - [x] 5.1 创建 `cmd/rnix/trace.go`，实现 `traceCmd`（Cobra 子命令）
    - Use: `trace [trace-id]`
    - 无参数时调用 `SpanReader.ListTraces()` 展示列表
    - 有参数时调用 `SpanReader.ReadSpans(traceID)` + `BuildSpanTree` + `FormatTraceTree`
  - [x] 5.2 实现 `runTrace(cmd *cobra.Command, args []string) error`
    - 路径: `.rnix/traces/`（与 SpanWriter 一致）
    - 支持 `--json` 和 `--verbose` 全局标志
  - [x] 5.3 在 `cmd/rnix/trace.go` 的 `init()` 中注册 `rootCmd.AddCommand(traceCmd)`
  - [x] 5.4 JSON 模式输出完整 SpanTree 结构（使用 `JSONResponse` 包装）

- [x] Task 6: 测试 (AC: #1-5)
  - [x] 6.1 `debug/trace_view_test.go`：BuildSpanTree 测试
    - 单根节点场景
    - 多层嵌套树（3 层）
    - 多个根节点（ParentSpanID 都为空的场景，理论上不应发生但要处理）
    - 空 Span 列表
    - 子节点按 StartTime 排序验证
    - TraceMetadata 统计正确（TotalSpans、TotalTokens、TotalDuration、ErrorCount）
  - [x] 6.2 `debug/trace_view_test.go`：FormatTraceTree 测试
    - 验证树形连接线正确（├─、└─、│ 的位置）
    - 验证各列对齐
    - 验证错误 Span 的标记
    - verbose 模式的额外信息
  - [x] 6.3 `debug/trace_view_test.go`：ListTraces 测试
    - 空目录返回空列表
    - 单个 Trace 目录
    - 多个 Trace 目录按时间倒序
  - [x] 6.4 `debug/trace_view_test.go`：FormatTraceList 测试
    - 验证表头和列对齐
  - [x] 6.5 `cmd/rnix/trace_test.go`：CLI 集成测试
    - trace 无参数 → 列表输出
    - trace 有效 trace-id → 树状输出
    - trace 无效 trace-id → 错误信息
    - trace --json → JSON 输出

## Dev Notes

### 架构决策

本 Story 是 Epic 15（分布式追踪）的第二层，基于 15-1 建立的 Span 基础设施，实现用户可见的追踪视图。核心设计原则：

1. **本地文件操作** — `rnix trace` 是本地命令，直接读取 `.rnix/traces/` 目录下的 JSONL 文件，不需要连接 daemon（与 `rnix replay` 模式一致）
2. **复用 SpanReader** — 15-1 已实现 `debug.SpanReader`，直接复用其 `ReadSpans(traceID)` 方法
3. **树形构建 + 格式化分离** — 数据逻辑（BuildSpanTree）和展示逻辑（FormatTraceTree）分离，便于 JSON 和人类可读两种输出
4. **遵循 CLI 模式** — 复用项目已有的 Cobra 命令注册模式、`--json`/`--verbose` 全局标志、`JSONResponse` 包装

### 关键设计：Span 树构建

```
spans.jsonl (flat):
  {trace_id: "abc", span_id: "s1", parent_span_id: "", name: "orchestrator", ...}
  {trace_id: "abc", span_id: "s2", parent_span_id: "s1", name: "analyst", ...}
  {trace_id: "abc", span_id: "s3", parent_span_id: "s1", name: "reviewer", ...}

BuildSpanTree →

  SpanTree{
    Root: &SpanNode{
      Span: {name: "orchestrator", span_id: "s1"},
      Children: [
        {Span: {name: "analyst", span_id: "s2"}},
        {Span: {name: "reviewer", span_id: "s3"}},
      ]
    }
  }
```

- 扁平 Span 列表 → 按 ParentSpanID 分组 → 递归构建树
- ParentSpanID 为空的 Span 是根节点（Compose 发起者）
- 如果存在多个 ParentSpanID 为空的 Span，取第一个作为 Root，其余挂到 Root 下（防御性处理）

### 关键设计：树状输出格式

```
Trace: abcdef1234567890  |  3 spans  |  12.5s  |  1500 tokens

┌─ orchestrator (PID 1)                    5.2s   800 tok   ok
│  ├─ code-analyst (PID 2)                 3.1s   500 tok   ok
│  └─ reviewer (PID 3)                     2.8s   200 tok   error
```

- 使用 box-drawing 字符（`┌─`、`├─`、`└─`、`│`）绘制层级连接线
- 对齐列：Name+PID 左对齐，Duration/Tokens/Status 右对齐，固定列宽
- 颜色：`ok` → SuccessStyle（绿色），`error` → ErrorStyle（红色），`timeout` → WarningStyle（黄色）
- Header 行展示 Trace 级别汇总（总 Span 数、总耗时、总 Token）
- `--verbose` 模式在每个 Span 下方缩进显示 SpanID、ParentSpanID、SyscallCount、精确 StartTime

### 关键设计：Trace 列表

扫描 `.rnix/traces/` 目录下的子目录，每个子目录名即为 TraceID：
```
.rnix/traces/
  abcdef1234567890.../spans.jsonl
  1234567890abcdef.../spans.jsonl
```

`ListTraces()` 方法读取每个 spans.jsonl 的第一行获取 root span 信息和起始时间。不需要完整解析所有行，但需要统计行数（span 数量）。按 StartTime 倒序展示。

### 关键复用点

1. **SpanReader** — `debug/trace.go:170-207`（15-1 已实现），`ReadSpans(traceID)` 返回 `[]*Span`
2. **SpanWriter 路径** — `cmd/rnix/main.go:1037-1039`，`traceBaseDir = cwd + "/.rnix/traces"`，trace 命令使用相同路径
3. **replay.go 命令模式** — 本地文件操作，不连接 daemon；`findRecordBaseDir()` 模式
4. **Cobra 命令注册** — `replay.go:29-31` 的 `init()` + `rootCmd.AddCommand()` 模式
5. **JSONResponse** — `cmd/rnix/main.go` 中定义的统一 JSON 响应包装
6. **lipgloss 样式** — `internal/ui/styles.go` 中的 `SuccessStyle`、`ErrorStyle`、`WarningStyle`、`MutedStyle`
7. **FormatTraceLine** — `internal/ui/trace.go` 的格式化模式（时间格式化、列对齐、颜色切换）
8. **record.go 列表模式** — `recordListCmd` 的列表展示模式可参考

### 不要做的事情

- **不要**实现 `rnix trace blame`（Story 15-3 的范围）
- **不要**实现实时追踪流（strace 模式）— trace 命令只读取已完成的 Trace 数据
- **不要**将格式化逻辑放在 `internal/ui/` — trace view 的格式化放在 `debug/trace_view.go` 中（与 `debug/replay.go` 的 `FormatReplayEvent` 模式一致），因为它操作 debug 包的类型
- **不要**引入新的外部依赖
- **不要**修改 SpanReader/SpanWriter 的现有接口（只扩展 SpanReader 新增 ListTraces）
- **不要**连接 daemon — trace 命令是纯本地文件操作
- **不要**修改 Bubble Tea TUI 框架
- **不要**在 `internal/ui/styles.go` 中新增颜色常量（现有颜色已够用）

### IPC 协议变更

无。本 Story 不涉及 IPC 协议变更，trace 命令是纯本地文件操作。

### 组合矩阵

| 本功能 | 交互对象 | 交互方式 | 需验证 |
|--------|----------|----------|--------|
| trace 命令 | SpanReader | 集成：复用 ReadSpans 读取 Span 数据 | 是 |
| trace 命令 | SpanWriter | 共存：读取 SpanWriter 写入的 JSONL 文件 | 是 |
| trace 列表 | .rnix/traces/ 目录 | 读取：扫描目录结构 | 是 |
| trace 命令 | strace 命令 | 独立：trace 读取持久化数据，strace 读取实时流 | 否 |
| trace 命令 | replay 命令 | 独立：不同的数据源（traces/ vs records/） | 否 |
| trace 命令 | compose up | 依赖：compose 生成 TraceID 后 trace 才有数据可读 | 否 |
| trace --json | JSONResponse | 集成：使用统一 JSON 包装 | 是 |
| trace 树 | 15-3 trace blame | 预留：SpanTree 结构可供 15-3 复用 | 否 |

### Project Structure Notes

新建文件：
- `debug/trace_view.go` — SpanTree、SpanNode、TraceMetadata、TraceSummary 类型；BuildSpanTree、FormatTraceTree、FormatTraceList
- `debug/trace_view_test.go` — 所有 trace view 相关测试
- `cmd/rnix/trace.go` — trace CLI 命令（traceCmd、runTrace、init）
- `cmd/rnix/trace_test.go` — trace CLI 集成测试

修改文件：
- `debug/trace.go` — SpanReader 新增 `ListTraces()` 方法（在现有 SpanReader 结构体上扩展）

### References

- [Source: debug/trace.go:170-207] — SpanReader.ReadSpans（15-1 已实现，本 Story 复用）
- [Source: debug/trace.go:69-83] — Span 结构体（树构建的数据源）
- [Source: debug/trace.go:85-132] — spanJSON/spanToJSON/spanFromJSON（JSON 序列化模式）
- [Source: cmd/rnix/main.go:1037-1039] — SpanWriter traceBaseDir 初始化（trace 命令使用同一路径）
- [Source: cmd/rnix/replay.go:16-32] — replay 命令注册模式（trace 命令参考）
- [Source: cmd/rnix/replay.go:338-341] — findRecordBaseDir 模式（findTraceBaseDir 参考）
- [Source: internal/ui/styles.go:7-51] — lipgloss 样式常量和初始化
- [Source: internal/ui/trace.go:14-69] — FormatTraceLine 格式化模式
- [Source: _bmad-output/planning-artifacts/prd/functional-requirements.md:149] — FR82: 分布式追踪视图
- [Source: _bmad-output/planning-artifacts/architecture/core-architectural-decisions.md:330-345] — Decision 11: 分布式追踪传播

### 技术栈

- Go 1.26 — 标准库满足所有需求
- `encoding/json` — Span JSON 序列化（复用已有）
- `os` / `path/filepath` — 目录扫描和文件读取
- `fmt` / `strings` — 树状格式化输出
- `sort` — Span 排序和 Trace 列表排序
- `time` — 时间格式化
- `github.com/spf13/cobra` — CLI 命令注册
- 无新增外部依赖

### 前置 story 学习总结

**来自 Story 15-1：**
1. SpanReader 已就绪 — `ReadSpans(traceID)` 返回 `[]*Span`，直接复用
2. Span 存储在 `.rnix/traces/<trace-id>/spans.jsonl`，每行一个 JSON
3. 时间字段使用 `_ms` 后缀毫秒值（spanJSON 序列化模式）
4. SpanWriter 集成在 `cmd/rnix/main.go`，使用 `cwd + "/.rnix/traces"` 作为基础目录
5. SpanStatus 有三种：`ok`、`error`、`timeout`，已实现 JSON 序列化
6. Span 的 ParentSpanID 为空表示根节点（Compose 发起的第一个进程）
7. `.gitignore` 已添加 `.rnix/traces/`

**来自 replay.go 命令模式：**
1. 本地文件操作不需要连接 daemon — 使用 `findRecordBaseDir()` 模式
2. `init()` 中独立注册命令 — `rootCmd.AddCommand(replayCmd)`
3. JSON 模式使用 `JSONResponse` 包装，错误使用 `map[string]string{"message": ...}`
4. 人类可读模式使用 `[trace]` 前缀（类似 `[replay]`）

**来自 Git 提交分析：**
- 提交消息格式：`feat: story X-Y done`
- debug 包文件命名约定：`trace.go`（15-1）、`replay.go`、`recorder.go`、`snapshot_diff.go`、`fork.go` → 新文件 `trace_view.go`

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6 (claude-opus-4-6)

### Debug Log References

- debug/trace_view_test.go: 12 个测试全部通过（BuildSpanTree、Walk、FormatTraceTree、FormatTraceList）
- debug/trace_test.go: 4 个新增 ListTraces 测试全部通过
- cmd/rnix/trace_test.go: 7 个 CLI 集成测试全部通过
- 全项目 18 包测试通过（-race 检测），2 个预存 TTY 测试失败不受影响

### Completion Notes List

- SpanReader.ListTraces 扫描 .rnix/traces/ 子目录，按 StartTime 倒序返回 TraceSummary
- BuildSpanTree 从扁平 Span 列表构建树形结构，子节点按 StartTime 排序
- FormatTraceTree 使用 box-drawing 字符（┌─、├─、└─、│）绘制树形连接线
- FormatTraceList 输出表格格式的 Trace 列表
- trace 命令是纯本地文件操作，不需要连接 daemon
- 复用 strace.go 中已有的 formatDuration 函数，避免重复定义
- CLI 支持 --json 和 --verbose 全局标志
- JSON 输出使用 JSONResponse 统一包装

### File List

新建文件:
- `debug/trace_view.go` — SpanTree、SpanNode、TraceMetadata 类型；BuildSpanTree、Walk、FormatTraceTree、FormatTraceList
- `debug/trace_view_test.go` — 12 个 trace view 测试
- `cmd/rnix/trace.go` — trace CLI 命令（traceCmd、runTrace、runTraceList、runTraceView、findTraceBaseDir）
- `cmd/rnix/trace_test.go` — 7 个 CLI 集成测试

修改文件:
- `debug/trace.go` — SpanReader 新增 ListTraces()、readTraceSummary() 方法；新增 TraceSummary 类型；新增 sort 导入
- `debug/trace_test.go` — 新增 4 个 ListTraces 测试
