# Story 27.9: Dashboard 分布式追踪集成

Status: done

## Story

As a 平台构建者,
I want 在 dashboard 中查看分布式追踪集成窗格，以 span 树展示 Compose 编排的跨进程追踪数据,
so that 我可以在可视化面板中直接分析多智能体协作的时序关系和调用链路。

## Acceptance Criteria (AC)

### AC-1: 新增追踪窗格（paneTrace）

**Given** dashboard 现有六个窗格（Tree=0 / Timeline=1 / Heatmap=2 / Detail=3 / Intent=4 / Security=5）
**When** 新增分布式追踪窗格
**Then** `paneTrace paneType = 6` 加入 iota 序列
**And** Tab 切换顺序变为 Tree → Timeline → Heatmap → Detail → Intent → Security → Trace → Tree
**And** Tab 取模值从 `% 6` 变为 `% 7`
**And** 当 Trace 窗格激活时，边框高亮显示

### AC-2: IPC 追踪数据方法

**Given** dashboard 需要获取分布式追踪数据
**When** Trace 窗格激活
**Then** 新增 `MethodTraceList Method = "trace_list"` IPC 方法，返回 `[]TraceSummaryWire`
**And** 新增 `MethodTraceTree Method = "trace_tree"` IPC 方法，接收 TraceID 参数，返回 `SpanTreeWire`
**And** `TraceSummaryWire` 包含 TraceID、SpanCount、StartTimeMs、TotalDurationMs、RootSpanName
**And** `SpanTreeWire` 包含 Root（SpanNodeWire 递归结构）、TraceID、Metadata（TotalSpans、TotalTokens、TotalDurationMs、ErrorCount）
**And** `SpanNodeWire` 包含 SpanID、ParentSpanID、PID、Name、DurationMs、TokensUsed、Status、Children
**And** daemon 端 handler 通过 `debug.SpanReader` 从 `.rnix/traces/` 磁盘读取数据
**And** 新增客户端方法 `func (c *Client) TraceList() ([]TraceSummaryWire, error)` 和 `func (c *Client) TraceTree(traceID string) (*SpanTreeWire, error)`

### AC-3: 追踪列表渲染

**Given** 存在已完成的 Compose 追踪数据
**When** 用户切换到 Trace 窗格
**Then** 显示追踪列表，每条追踪显示：TraceID（截断至 16 字符）+ Root Span 名称 + Span 数量 + 总耗时
**And** 按开始时间降序排列（最新在前）
**And** 窗格切换渲染延迟 ≤ 100ms（NFR63-obs）

### AC-4: Span 树展开与瀑布图

**Given** 用户在追踪列表中选中一条追踪并按 Enter
**When** 展开该追踪的 span 树
**Then** 通过 `client.TraceTree(traceID)` 获取完整 span 树
**And** 以缩进树形式展示 span 层级关系（使用 ├─ └─ │ 等树状连接符）
**And** 每个 span 显示：名称 + PID + 耗时 + token 数 + 状态
**And** 状态着色：ok=绿色、error=红色、timeout=橙色
**And** Escape 键返回追踪列表

### AC-5: Span 节点联动

**Given** span 树展开后选中某个 span 节点
**When** 用户按 Enter
**Then** 联动切换 `selectedPID` 到该 span 对应的进程
**And** 切换到 Timeline 窗格查看该进程的步骤数据

**Given** span 对应的进程已被 reaper 清理
**When** 用户选中该 span 按 Enter
**Then** 显示状态消息 "该进程已不存在"
**And** 不切换 selectedPID

### AC-6: 空状态处理

**Given** 无任何 Compose 追踪数据（`.rnix/traces/` 目录为空或不存在）
**When** 用户切换到 Trace 窗格
**Then** 显示空状态提示："无活跃的 Compose 追踪数据。使用 rnix compose up 启动编排以生成追踪。"

**Given** TraceList IPC 调用失败
**When** 用户切换到 Trace 窗格
**Then** 显示错误信息而不崩溃

## Tasks / Subtasks

- [x] Task 1: 新增 IPC 追踪方法（AC: #2）
  - [x] 1.1 在 `ipc/protocol.go` 新增 `MethodTraceList Method = "trace_list"` 和 `MethodTraceTree Method = "trace_tree"`
  - [x] 1.2 定义 Wire 类型：`TraceListRequest`（空）、`TraceListResponse`、`TraceSummaryWire`、`TraceTreeRequest`（TraceID string）、`TraceTreeResponse`（SpanTreeWire）、`SpanNodeWire`、`TraceMetaWire`
  - [x] 1.3 在 `ipc/server.go` 实现 handler：使用 `debug.NewSpanReader(traceBaseDir)` 读取 `.rnix/traces/` 目录
  - [x] 1.4 在 `ipc/client.go` 新增 `TraceList()` 和 `TraceTree(traceID string)` 客户端方法
- [x] Task 2: 新增 paneTrace 常量与 Tab 切换扩展（AC: #1）
  - [x] 2.1 在 `cmd/rnix/dashboard.go` 的 paneType iota 中新增 `paneTrace = 6`
  - [x] 2.2 修改 Tab 切换 `% 6` → `% 7`
  - [x] 2.3 更新 renderDashboardStatus() 帮助文本
- [x] Task 3: 追踪列表获取与展示（AC: #3, #6）
  - [x] 3.1 在 dashboardModel 新增字段：`traceSummaries []ipc.TraceSummaryWire`、`traceErr error`、`traceCursor int`、`traceScrollOffset int`、`traceViewMode int`（0=列表, 1=树）、`selectedTraceID string`、`selectedSpanTree *ipc.SpanTreeWire`、`spanFlatNodes []spanFlatNode`、`spanCursor int`、`spanScrollOffset int`
  - [x] 3.2 实现 `fetchTraceListCmd()` tea.Cmd（调用 client.TraceList()）
  - [x] 3.3 定义 `traceListMsg` 消息类型，在 Update 中处理响应
  - [x] 3.4 在 dashboardTick 中周期性刷新（每 5 ticks，仅 Trace 窗格激活时）
  - [x] 3.5 实现 `renderTracePane(width, height int) string` 方法（列表模式 + 空状态）
- [x] Task 4: Span 树展开与渲染（AC: #4）
  - [x] 4.1 实现 `fetchTraceTreeCmd(traceID string)` tea.Cmd
  - [x] 4.2 定义 `traceTreeMsg` 消息类型，在 Update 中处理
  - [x] 4.3 实现 `flattenSpanTree(tree *ipc.SpanTreeWire) []spanFlatNode`（DFS 展开为扁平列表，记录深度和树形连接符）
  - [x] 4.4 渲染 span 树视图：树形缩进 + 状态着色 + 光标高亮
  - [x] 4.5 Enter 键从列表模式切换到树模式，Escape 键返回列表
- [x] Task 5: Span 联动导航（AC: #5）
  - [x] 5.1 在 span 树模式下 Enter 键：提取选中 span 的 PID，联动 selectedPID 并切换到 Timeline
  - [x] 5.2 进程不存在时显示状态消息
- [x] Task 6: 测试（AC: #1-#6）
  - [x] 6.1 IPC 单元测试：TraceList 和 TraceTree handler
  - [x] 6.2 ATDD 测试：Trace 窗格 Tab 切换（7 窗格循环）
  - [x] 6.3 ATDD 测试：追踪列表渲染 + 空状态
  - [x] 6.4 ATDD 测试：Span 树展开与联动导航
  - [x] 6.5 更新 27-8 和 27-7 的 Tab 循环测试（6→7 窗格）

## Dev Notes

### 关键设计决策

**为什么需要新增 IPC 方法而非直接读磁盘？**
Dashboard 是 Bubble Tea TUI 客户端，所有数据通过 IPC 获取（Timeline→ListSteps, Detail→GetProcDetail, Intent→IntentList, Security→ImmuneStatus）。追踪数据也应通过 IPC 获取，保持一致的数据获取模式。

**为什么采用两级视图（列表 + 树展开）？**
追踪数据可能有多条（每次 Compose 编排产生一条），先展示列表供选择，再展开 span 树查看详情。这与 Intent 窗格的 DAG 展开模式一致。

**Trace 数据路径：** `.rnix/traces/<trace-id>/spans.jsonl`（现有 `findTraceBaseDir()` 返回 `cwd + "/.rnix/traces"`）。daemon 端 handler 需使用相同路径。

### 现有代码模式（必须遵循）

**Dashboard 窗格添加模式**（参考 Story 27-8 Security 窗格添加）：
1. `paneType` 是 int 类型，使用 iota：`paneTree=0, paneTimeline=1, paneHeatmap=2, paneDetail=3, paneIntent=4, paneSecurity=5`
2. 新增 `paneTrace = 6`（iota 自动递增）
3. Tab 切换：`m.activePane = (m.activePane + 1) % 7`（当前为 `% 6`，需改为 `% 7`）
4. View 中：`case paneTrace:` 分支调用 `renderTracePane()`
5. 边框高亮：`activePaneStyle` vs `inactivePaneStyle`

**IPC 异步调用模式**（参考 `fetchImmuneStatusCmd`）：
```go
func fetchTraceListCmd() tea.Cmd {
    return func() tea.Msg {
        client, err := ipc.Dial(ipc.SocketPath())
        if err != nil {
            return traceListMsg{err: err}
        }
        defer client.Close()
        summaries, err := client.TraceList()
        return traceListMsg{summaries: summaries, err: err}
    }
}
```

**消息处理模式**（参考 `immuneStatusMsg`）：
```go
type traceListMsg struct {
    summaries []ipc.TraceSummaryWire
    err       error
}
type traceTreeMsg struct {
    traceID string
    tree    *ipc.SpanTreeWire
    err     error
}
```

**渲染位置**：与 Detail/Intent/Security 共享右下区域。`case paneTrace:` 在 View() 中与其他窗格同级。

### IPC Wire 类型定义

```go
// ipc/protocol.go — 新增类型

type TraceSummaryWire struct {
    TraceID         string `json:"trace_id"`
    SpanCount       int    `json:"span_count"`
    StartTimeMs     int64  `json:"start_time_ms"`
    TotalDurationMs int64  `json:"total_duration_ms"`
    RootSpanName    string `json:"root_span_name"`
}

type TraceListResponse struct {
    Traces []TraceSummaryWire `json:"traces"`
}

type TraceTreeRequest struct {
    TraceID string `json:"trace_id"`
}

type TraceTreeResponse struct {
    Tree *SpanTreeWire `json:"tree"`
}

type SpanTreeWire struct {
    Root     *SpanNodeWire `json:"root"`
    TraceID  string        `json:"trace_id"`
    Metadata TraceMetaWire `json:"metadata"`
}

type SpanNodeWire struct {
    SpanID       string         `json:"span_id"`
    ParentSpanID string         `json:"parent_span_id,omitempty"`
    PID          uint64         `json:"pid"`
    Name         string         `json:"name"`
    DurationMs   int64          `json:"duration_ms"`
    TokensUsed   int            `json:"tokens_used"`
    Status       string         `json:"status"` // "ok", "error", "timeout"
    Children     []SpanNodeWire `json:"children,omitempty"`
}

type TraceMetaWire struct {
    TotalSpans      int   `json:"total_spans"`
    TotalTokens     int   `json:"total_tokens"`
    TotalDurationMs int64 `json:"total_duration_ms"`
    ErrorCount      int   `json:"error_count"`
}
```

### Server Handler 实现要点

```go
// ipc/server.go — handler 注册
case MethodTraceList:
    return s.handleTraceList(req)
case MethodTraceTree:
    return s.handleTraceTree(req)

// handler 实现
func (s *Server) handleTraceList(req *Request) *Response {
    reader := debug.NewSpanReader(s.traceBaseDir())
    summaries, err := reader.ListTraces()
    // 将 debug.TraceSummary 转换为 TraceSummaryWire
}

func (s *Server) handleTraceTree(req *Request) *Response {
    var treq TraceTreeRequest
    // 解码 payload
    reader := debug.NewSpanReader(s.traceBaseDir())
    spans, err := reader.ReadSpans(types.TraceID(treq.TraceID))
    tree := debug.BuildSpanTree(spans)
    // 将 debug.SpanTree 转换为 SpanTreeWire
}
```

**traceBaseDir 路径问题：** daemon 启动时的 CWD 可能不是项目目录。需要在 Server 中存储 trace 基础路径，或通过 Kernel 提供。参考 `findTraceBaseDir()` 的实现：使用项目 CWD + `/.rnix/traces`。daemon 在 `EnsureDaemon()` 中 fork 自当前目录，CWD 应一致。Server 已有 `kernel` 字段，检查 kernel 是否有数据路径配置；如果没有，使用 `os.Getwd() + "/.rnix/traces"` 作为默认值。

### 已有分布式追踪基础设施

以下类型和方法已在 `debug/` 包中实现（Epic 15），直接复用：

| 类型/方法 | 文件 | 用途 |
|-----------|------|------|
| `Span` | `debug/trace.go:71` | Span 数据结构 |
| `SpanStatus` (OK/ERROR/TIMEOUT) | `debug/trace.go:22` | Span 状态枚举 |
| `TraceSummary` | `debug/trace.go:171` | 追踪摘要 |
| `SpanReader` | `debug/trace.go:198` | 从磁盘读取 spans |
| `SpanReader.ListTraces()` | `debug/trace.go:239` | 列出所有追踪（按时间降序） |
| `SpanReader.ReadSpans(traceID)` | `debug/trace.go:208` | 读取指定追踪的所有 spans |
| `SpanNode` | `debug/trace_view.go:14` | Span 树节点 |
| `SpanTree` | `debug/trace_view.go:30` | Span 树结构（含 Root + Metadata） |
| `TraceMetadata` | `debug/trace_view.go:20` | 追踪元数据 |
| `BuildSpanTree(spans)` | `debug/trace_view.go:108` | 从扁平 spans 构建树 |
| `SpanTree.Walk(fn)` | `debug/trace_view.go:192` | DFS 遍历 |
| `FormatTraceTree(tree, verbose)` | `debug/trace_view.go:207` | 格式化为文本（CLI 用） |

### Span 树展平算法

参考 Intent 窗格的 `flattenIntentTrees()`，为 span 树实现类似的展平：

```go
type spanFlatNode struct {
    spanID   string
    pid      types.PID
    name     string
    durMs    int64
    tokens   int
    status   string // "ok", "error", "timeout"
    depth    int
    prefix   string // "├─ " / "└─ " / "│  " 等树形连接符
    isRoot   bool
}

func flattenSpanTree(tree *ipc.SpanTreeWire) []spanFlatNode {
    if tree == nil || tree.Root == nil {
        return nil
    }
    var nodes []spanFlatNode
    flattenSpanNode(tree.Root, 0, true, "", &nodes)
    return nodes
}

func flattenSpanNode(node *SpanNodeWire, depth int, isLast bool, parentPrefix string, out *[]spanFlatNode) {
    var prefix string
    if depth == 0 {
        prefix = "┌─ "
    } else if isLast {
        prefix = parentPrefix + "└─ "
    } else {
        prefix = parentPrefix + "├─ "
    }
    *out = append(*out, spanFlatNode{
        spanID: node.SpanID,
        pid:    types.PID(node.PID),
        name:   node.Name,
        durMs:  node.DurationMs,
        tokens: node.TokensUsed,
        status: node.Status,
        depth:  depth,
        prefix: prefix,
        isRoot: depth == 0,
    })
    childPrefix := parentPrefix
    if depth > 0 {
        if isLast {
            childPrefix += "   "
        } else {
            childPrefix += "│  "
        }
    }
    for i, child := range node.Children {
        flattenSpanNode(&child, depth+1, i == len(node.Children)-1, childPrefix, out)
    }
}
```

### Span 状态着色

```go
func spanStatusColor(status string) lipgloss.Color {
    switch status {
    case "ok":
        return lipgloss.Color("42")    // 绿色
    case "error":
        return lipgloss.Color("196")   // 红色
    case "timeout":
        return lipgloss.Color("208")   // 橙色
    default:
        return lipgloss.Color("240")   // 灰色
    }
}
```

### 渲染格式

**列表模式：**
```
┌─ Traces ──────────────────────────────┐
│ TRACE ID          ROOT      SPANS DUR │
│ ────────────────────────────────────── │
│ ▸ a1b2c3d4e5f6    pipeline    5  2.3s │
│   f7e8d9c0b1a2    workflow    3  1.1s │
│   12345678abcd    compose     8  5.7s │
│                                       │
│ Enter:展开  j/k:选择                    │
└───────────────────────────────────────┘
```

**树模式：**
```
┌─ Trace: a1b2c3d4e5f6 ── 5 spans ── 2.3s ── 1280 tok ┐
│ ┌─ pipeline (PID 1)              2.3s  500tok  ok     │
│ ├─ researcher (PID 2)            1.2s  380tok  ok     │
│ │  └─ sub-task (PID 4)           0.3s  100tok  ok     │
│ └─ writer (PID 3)                0.8s  400tok  error  │
│                                                       │
│ Enter:跳转进程  Esc:返回列表  j/k:选择                  │
└───────────────────────────────────────────────────────┘
```

### 帮助文本

```go
if m.activePane == paneTrace {
    if m.traceViewMode == 0 { // 列表
        return fmt.Sprintf("  %sq:Quit  Tab:Switch Pane  j/k:Navigate  Enter:Expand Trace%s", rec, ops)
    }
    // 树模式
    return fmt.Sprintf("  %sq:Quit  Tab:Switch Pane  j/k:Navigate  Enter:Jump to Process  Esc:Back%s", rec, ops)
}
```

### 修改文件清单

| 文件 | 修改类型 | 说明 |
|------|----------|------|
| `ipc/protocol.go` | 修改 | 新增 MethodTraceList、MethodTraceTree 常量 + TraceSummaryWire、TraceListResponse、TraceTreeRequest、TraceTreeResponse、SpanTreeWire、SpanNodeWire、TraceMetaWire 类型 |
| `ipc/server.go` | 修改 | 新增 handleTraceList、handleTraceTree handler |
| `ipc/client.go` | 修改 | 新增 TraceList()、TraceTree() 客户端方法 |
| `cmd/rnix/dashboard.go` | 修改 | 新增 paneTrace(6)、traceListMsg、traceTreeMsg、fetchTraceListCmd、fetchTraceTreeCmd、renderTracePane、flattenSpanTree、spanStatusColor、帮助文本更新、Tab 取模 %6→%7、j/k/Enter/Esc 处理、dashboardTick 周期刷新 |
| `cmd/rnix/atdd_27_9_dashboard_trace_panel_test.go` | 新增 | ATDD 验收测试 |

### 不需要修改的文件

- `debug/trace.go` — Span/SpanReader/TraceSummary/ListTraces/ReadSpans 已存在
- `debug/trace_view.go` — SpanNode/SpanTree/BuildSpanTree/TraceMetadata 已存在
- `cmd/rnix/trace.go` — CLI trace 命令直接读磁盘，不受影响

### 依赖关系

- **Story 27.3-27.8**（Dashboard 窗格模式）：参考 Tab 切换、窗格渲染、IPC 获取模式，均已完成
- **Epic 15**（分布式追踪已实现）：Span/SpanReader/SpanTree/BuildSpanTree 全部已实现
- **Epic 7**（Compose 编排已实现）：Compose 运行时产生追踪数据

### 防踩坑清单

1. **Tab 取模值必须更新** — 从 `% 6` 改为 `% 7`，搜索所有 `% 6` 确保无硬编码遗漏
2. **traceBaseDir 路径** — daemon 端使用 `os.Getwd() + "/.rnix/traces"`，确保与 `findTraceBaseDir()` 一致
3. **SpanNodeWire.PID 是 uint64** — 需要转换为 `types.PID`（`types.PID(node.PID)`）
4. **traceCursor 越界** — 在 traceList 刷新后，确保 `traceCursor < len(traceSummaries)`
5. **SpanTreeWire 递归结构** — Children 是 `[]SpanNodeWire`（值类型，非指针），JSON 序列化/反序列化无环引用问题
6. **空 traces 目录** — `SpanReader.ListTraces()` 目录不存在时返回 `nil, nil`（不是错误），dashboard 显示空状态即可
7. **RNIX_ASCII 环境变量** — `┌─ ├─ └─ │` 树形符号需 ASCII 降级（参考 intentStateIcon 的 ASCII 降级模式：使用 +-- |-- 等）
8. **两级视图状态管理** — `traceViewMode` 在切换窗格或选中新进程时应重置为列表模式（0）
9. **CJK 字符宽度** — Root Span 名称可能包含 CJK 字符，需考虑占 2 列宽
10. **27-8 和 27-7 ATDD 测试更新** — Tab 循环测试需要从 6 窗格更新到 7 窗格
11. **Span 数据可能为空** — ReadSpans 返回 `nil, nil` 时 BuildSpanTree 返回 nil，handler 应返回空树而非错误
12. **daemon trace 数据路径一致性** — daemon 在 fork 时继承 CWD，但如果用户从不同目录启动 CLI，daemon CWD 可能不同。建议使用 Server 启动时记录的 CWD 或通过 kernel 的 data path 配置

### 前序 Story 经验

**来自 Story 27-8（Security 窗格）：**
- paneSecurity 添加为 `paneType = 5`，Tab `% 6` 更新成功
- immuneStatus 数据刷新使用 `dashboardTick % 5 == 0` 且仅在 Security 窗格激活时执行
- nil 守卫是关键——ImmuneStatus 可能返回 nil
- securityCursor 越界保护在刷新后执行
- Enter 跳转前验证 PID 是否存在于进程列表

**来自 Story 27-7（Intent 窗格）：**
- DAG 展平算法：topological sort + indent depth 计算，是 span 树展平的参考
- intentCursor 管理 + scrollOffset 管理模式可直接复用
- Code Review Fix #1：nil 指针守卫，Fix #3：PID 存在性验证

**来自 Story 27-6（Detail 窗格）：**
- 帮助文本的 case 分支模式
- Enter 跳转前验证 PID 是否存在于进程列表

### Git 最近提交（上下文参考）

```
4f5a462 feat: Implement Story 27.8 - Dashboard Security Anomaly Panel
a0d6ed6 feat: Implement Story 28.4 - Dashboard PID Validity Check
e239698 feat: Implement Story 28.3 - IPC PID→UUID Mapping
d2afb4c feat: Implement Story 27.7 - Dashboard Intent Tree Integration
0a4ca7d feat: Implement Story 27.6 - Dashboard Process Detail Panel
```

### 组合矩阵

| 交互功能 | 共存行为 | 需验证 |
|---------|---------|--------|
| Trace 窗格 + Tree 窗格 | Trace Enter 联动修改 selectedPID，Tree 窗格高亮对应进程 | 是 |
| Trace 窗格 + Timeline 窗格 | Enter 跳转后 Timeline 自动加载对应 PID 的 step 数据 | 是 |
| Trace 窗格 + Intent 窗格 | 独立数据源，不冲突 | 否 |
| Trace 窗格 + Security 窗格 | 独立数据源，不冲突 | 否 |
| Trace 窗格 + Prompt Pager | Trace 窗格激活时 p 键不应触发 prompt pager | 是 |
| 无追踪数据 + 窗格切换 | 空状态正常显示，不崩溃 | 是 |
| 树模式 + Tab 切换 | Tab 切走再切回应保持 traceViewMode 状态 | 是 |

### Project Structure Notes

- Dashboard 所有改动集中在 `cmd/rnix/dashboard.go`，不拆分文件
- IPC 类型在 `ipc/protocol.go`，handler 在 `ipc/server.go`，客户端在 `ipc/client.go`
- ATDD 测试文件命名遵循 `atdd_27_9_*.go` 规范
- 复用 `debug` 包的 SpanReader/BuildSpanTree 基础设施，不重复实现

### References

- [Source: _bmad-output/planning-artifacts/epics/epic-27-统一观察系统-unified-observation-system.md#Story 27.9]
- [Source: debug/trace.go — Span/SpanReader/TraceSummary/ListTraces/ReadSpans]
- [Source: debug/trace_view.go — SpanNode/SpanTree/BuildSpanTree/TraceMetadata/Walk]
- [Source: ipc/protocol.go — 现有 IPC Method 常量和 Wire 类型定义模式]
- [Source: cmd/rnix/dashboard.go — 现有六窗格架构 + paneSecurity 添加模式]
- [Source: cmd/rnix/trace.go — findTraceBaseDir() 路径确定]
- [Source: _bmad-output/implementation-artifacts/27-8-dashboard-security-anomaly-panel.md — 前序 Story 经验]

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6

### Debug Log References

### Completion Notes List

- ✅ Task 1: IPC trace methods — Added MethodTraceList/MethodTraceTree constants, Wire types (TraceSummaryWire, SpanTreeWire, SpanNodeWire, TraceMetaWire), server handlers (handleTraceList, handleTraceTree with debug.SpanReader/BuildSpanTree), client methods (TraceList, TraceTree)
- ✅ Task 2: paneTrace=6 added to iota, Tab modulo updated %6→%7, status bar help text for list/tree modes
- ✅ Task 3: Dashboard trace fields added, traceListMsg/traceTreeMsg message handling, fetchTraceListCmd, renderTracePane with list view + empty state + error display, periodic refresh every 5 ticks
- ✅ Task 4: fetchTraceTreeCmd, flattenSpanTree DFS algorithm with tree connectors (├─/└─/│ with ASCII fallback), spanStatusColor (ok=green, error=red, timeout=orange), tree view rendering, Enter→tree/Escape→list mode switching
- ✅ Task 5: Span node Enter→PID linkage with process existence validation, "该进程已不存在" message for reaped processes, switches to Timeline pane
- ✅ Task 6: All 27-9 ATDD tests pass (30 tests), updated 27-6/27-7/27-8 Tab cycling tests for 7-pane cycle
- ✅ Fixed ATDD test: flattenSpanTree expected 5 nodes but tree has 4 (root+2 children+1 grandchild)

#### Code Review Fixes (9 Patch Items)

- ✅ P-1: `traceBaseDir()` 改为返回 `(string, error)`，两个 handler 检查错误
- ✅ P-2: `spanNodeToWire` 添加 node/Span nil 守卫 + Children nil 过滤
- ✅ P-3: 滚动视口计算统一 — 新增 `traceBottomInnerH()` 函数，traceAdjustScroll/spanAdjustScroll 与 render 函数使用一致公式
- ✅ P-4: TraceID 路径遍历防护 — 拒绝含 `/`、`\`、`..` 的 TraceID
- ✅ P-5: `flattenSpanNode` 中 `os.Getenv("RNIX_ASCII")` 从每节点 3 次优化为顶层 1 次，通过参数传递
- ✅ P-6: 光标字符 `▸` 在 RNIX_ASCII 模式下降级为 `>`
- ✅ P-7: 空 span 树（Root=nil）不再进入树模式，改为留在列表模式并显示 "此追踪无 span 数据" 状态消息
- ✅ P-8: v/V/p 键添加 `activePane == paneTimeline` 守卫，防止 Trace 窗格中错误触发步骤详情逻辑
- ✅ P-9: Esc 返回列表模式时重置 `traceScrollOffset` 并 clamp `traceCursor`
- ✅ 修复 27-3 测试 helper `newStepTimelineModel()` 补充 `activePane = paneTimeline`

### File List

- `ipc/protocol.go` — Modified: Added MethodTraceList, MethodTraceTree, TraceSummaryWire, TraceListResponse, TraceTreeRequest, TraceTreeResponse, SpanTreeWire, SpanNodeWire, TraceMetaWire
- `ipc/server.go` — Modified: Added handleTraceList, handleTraceTree, traceBaseDir (returns error), spanNodeToWire (nil guard), TraceID validation, route cases
- `ipc/client.go` — Modified: Added TraceList(), TraceTree() client methods
- `cmd/rnix/dashboard.go` — Modified: Added paneTrace=6, %7 modulo, trace fields, traceListMsg/traceTreeMsg types, spanFlatNode type, handleTraceKey, flattenSpanTree (ascii param), spanStatusColor, renderTracePane, renderTraceListView (ASCII cursor), renderTraceTreeView (ASCII cursor), fetchTraceListCmd, fetchTraceTreeCmd, traceBottomInnerH, traceAdjustScroll (unified formula), spanAdjustScroll (unified formula), trace status bar help, trace tick fetching, empty tree guard, Esc scroll reset, v/V/p pane guard
- `cmd/rnix/atdd_27_9_dashboard_trace_panel_test.go` — New: 30 ATDD tests
- `cmd/rnix/atdd_27_3_dashboard_timeline_test.go` — Modified: Added activePane=paneTimeline to helper
- `cmd/rnix/atdd_27_6_dashboard_detail_panel_test.go` — Modified: Updated Tab cycle tests 6→7 panes
- `cmd/rnix/atdd_27_7_dashboard_intent_tree_test.go` — Modified: Updated Tab cycle tests 6→7 panes
- `cmd/rnix/atdd_27_8_dashboard_security_panel_test.go` — Modified: Updated Tab cycle tests 6→7 panes
