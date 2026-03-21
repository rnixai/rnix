# Story 27.3: watch 命令基础 — Level 1 实时流

Status: done

## Story

As a 平台构建者,
I want 通过 `rnix watch <pid>` 实时看到智能体每一步的摘要（步骤号 + 动作类型 + 目标 + 耗时）,
So that 我可以了解 agent 当前在做什么。

## Acceptance Criteria

### AC-1: watch Cobra 命令注册

**Given** `cmd/rnix/watch.go` 不存在
**When** 创建 watch 命令
**Then** 注册 `rnix watch <pid>` Cobra 命令
**And** 命令接受一个位置参数 `<pid>`（整数），通过 `cobra.ExactArgs(1)` 校验
**And** 在 `cmd/rnix/main.go` 的 `init()` 中 `rootCmd.AddCommand(watchCmd)` 注册

### AC-2: watch IPC 方法 — 协议层

**Given** `ipc/protocol.go` 中的 Method 常量
**When** 添加 watch 方法
**Then** 新增 `MethodWatch Method = "watch"` 常量
**And** 新增 `WatchRequest` 结构体：`PID types.PID`（json:"pid"）

### AC-3: ProgressPayload 扩展

**Given** `ProgressPayload` 结构体
**When** 扩展以支持 watch 信息
**Then** 新增 `HasError bool` 字段（json:"has_error,omitempty"）
**And** 新增 `DurationMs float64` 字段（json:"duration_ms,omitempty"）

### AC-4: KernelCallbacks 签名扩展

**Given** `KernelCallbacks` 接口的 `OnStepComplete` 方法
**When** 扩展签名以传递 duration 和 error 信息
**Then** 签名变更为 `OnStepComplete(pid types.PID, step int, action string, summary string, duration time.Duration, hasError bool)`
**And** kernel.go 中所有 `k.callbacks.OnStepComplete(...)` 调用点更新，传入 `time.Since(stepStart)` 和 error 标志

### AC-5: callbackMux 多订阅者支持

**Given** `callbackMux` 当前仅支持 1 PID : 1 channel
**When** 重构为多订阅者模式
**Then** 数据结构变更为 `SyncMap[PID, *subscriberList]`，`subscriberList` 内部持有 `[]chan<- StreamEvent` + `sync.Mutex`
**And** `register(pid, ch)` 支持追加新 channel 到已有列表
**And** `unregister(pid, ch)` 移除指定 channel（非删除整个 PID 条目）
**And** `send(pid, ev)` 广播到所有注册的 channel

### AC-6: Server handler — watch 流式连接

**Given** 用户执行 `rnix watch 42`
**When** 进程 PID=42 存在且正在运行
**Then** CLI 通过 IPC 发送 watch 请求
**And** Server 在 `handleWatch` 中通过 `callbackMux.register(pid, eventCh)` 订阅 Progress 回调
**And** 先回放已记录的步骤历史（从 steps.jsonl 读取已完成步骤，组装为 ProgressPayload 发送）
**And** 然后实时转发后续 Progress 事件
**And** 从命令执行到首条事件显示的延迟 ≤ 200ms（NFR57）

### AC-7: watch 事件渲染 — Level 1 输出

**Given** watch 视图正在接收 Progress 回调
**When** 收到 OnStepComplete 事件（step=3, action="tool_call", summary="/dev/fs → main.go 内容..."）
**Then** 渲染一行：`[step 3] tool_call → /dev/fs  0.2s  ✓`
**And** 单行渲染耗时 ≤ 1ms（NFR59）

**Given** watch 视图正在接收 Progress 回调
**When** 收到 OnStep 事件（step=4, total=30）
**Then** 显示进行中指示：`[step 4/30] thinking...`

**Given** watch 视图正在接收 Progress 回调
**When** 收到 OnComplete 事件（result, exitStatus）
**Then** 显示完成状态行并退出 watch 视图

### AC-8: spawn --watch 集成

**Given** 用户执行 `rnix spawn --watch "分析 main.go"`
**When** 添加 `--watch` flag 到 spawn 命令
**Then** spawn 返回 PID 后立即进入 watch 视图（复用同一 streaming 连接的事件流，不另起 watch IPC 连接）
**And** 从 spawn 返回 PID 到 watch 首条事件显示的延迟 ≤ 100ms（NFR58）

### AC-9: watch 退出 — q 键退出

**Given** 用户在 watch 视图中按 `q` 键
**When** 处理退出
**Then** 断开 IPC 流式连接，返回终端
**And** 进程继续运行不受影响

### AC-10: 错误处理 — PID 不存在

**Given** 目标 PID 不存在
**When** 用户执行 `rnix watch 999`
**Then** 输出错误信息 `error: process 999 not found` 并退出

### AC-11: callbackMux OnStepComplete 填充新字段

**Given** callbackMux 的 `OnStepComplete` 实现
**When** 收到 kernel 回调
**Then** 将 `duration` 转换为 `float64` 毫秒填充 `ProgressPayload.DurationMs`
**And** 将 `hasError` 填充到 `ProgressPayload.HasError`

## Tasks / Subtasks

- [x] Task 1: protocol.go 扩展 (AC: #2, #3)
  - [x] 1.1 在 Method 常量块中添加 `MethodWatch Method = "watch"`
  - [x] 1.2 定义 `WatchRequest{PID types.PID}`（json tag: `pid`）
  - [x] 1.3 在 ProgressPayload 中添加 `HasError bool` 和 `DurationMs float64` 字段（omitempty）

- [x] Task 2: kernel.go KernelCallbacks 签名变更 (AC: #4)
  - [x] 2.1 修改 `KernelCallbacks.OnStepComplete` 签名：追加 `duration time.Duration, hasError bool` 参数
  - [x] 2.2 更新 kernel.go 中所有 `k.callbacks.OnStepComplete(...)` 调用点（11 处），传入正确的 `time.Since(stepStart)` 和 `hasError` 值
  - [x] 2.3 确认 hasError 语义：当前所有 OnStepComplete 调用仅在成功路径触发，hasError=false；错误路径走 OnError/circuit_breaker

- [x] Task 3: callbackMux 重构为多订阅者 (AC: #5, #11)
  - [x] 3.1 定义 `subscriberList` 结构体：`mu sync.Mutex` + `channels []chan<- StreamEvent`
  - [x] 3.2 修改 `callbackMux.handlers` 类型为 `*xsync.SyncMap[types.PID, *subscriberList]`
  - [x] 3.3 修改 `register(pid, ch)` —— LoadOrStore subscriberList → Lock → append → Unlock
  - [x] 3.4 修改 `unregister(pid, ch)` —— Load subscriberList → Lock → 从 channels 移除指定 ch → 若 channels 空则 Delete 整个条目 → Unlock
  - [x] 3.5 修改 `send(pid, ev)` —— Load subscriberList → Lock → 遍历 channels 逐个非阻塞发送 → Unlock
  - [x] 3.6 修改 `callbackMux.OnStepComplete` —— 新签名接受 `duration time.Duration, hasError bool`，填充 `ProgressPayload.DurationMs` 和 `ProgressPayload.HasError`

- [x] Task 4: server.go 新增 handleWatch (AC: #6)
  - [x] 4.1 在 `handleConn` switch 中添加 `case MethodWatch: s.handleWatch(conn, req.Payload); return`
  - [x] 4.2 实现 `handleWatch(conn net.Conn, rawPayload json.RawMessage)`:
    - 4.2.1 解析 WatchRequest
    - 4.2.2 验证进程存在（GetProcess 或磁盘 steps 目录）
    - 4.2.3 创建 `eventCh := make(chan StreamEvent, 64)` 并 `callbackMux.register(pid, eventCh)`
    - 4.2.4 回放已记录步骤历史（读 steps.jsonl，为每步组装 ProgressPayload{Event: "step_complete"} 发送）
    - 4.2.5 writeResponse OK，开始流式循环
    - 4.2.6 监听 eventCh、proc.Done（或 server.done）退出
    - 4.2.7 defer `callbackMux.unregister(pid, eventCh)`

- [x] Task 5: client.go 新增 WatchProcess 方法 (AC: #6)
  - [x] 5.1 实现 `func (c *Client) WatchProcess(pid types.PID, onEvent func(StreamEvent)) (*ProgressPayload, error)`
  - [x] 5.2 模式同 SpawnAndWatch：sendRequest → readResponse → scanner 循环 → onEvent 回调 → StreamComplete/StreamError/StreamEOF 时 break

- [x] Task 6: cmd/rnix/watch.go 新增 watch 命令 (AC: #1, #7, #9, #10)
  - [x] 6.1 定义 `watchCmd *cobra.Command`：Use="watch <pid>", Args=cobra.ExactArgs(1)
  - [x] 6.2 在 RunE 中：解析 PID → EnsureDaemon → client.WatchProcess(pid, renderEvent)
  - [x] 6.3 实现 `renderWatchEvent(ev, profile)` 渲染函数
  - [x] 6.4 实现 q 键退出：x/term raw 模式 + goroutine 读取 stdin
  - [x] 6.5 渲染时考虑 `RNIX_ASCII=1` 环境变量（使用 ASCII 替代 Unicode ✓/✗）

- [x] Task 7: spawn --watch flag (AC: #8)
  - [x] 7.1 在 main.go 中添加 `flagWatch bool` + `--watch` flag 注册
  - [x] 7.2 在 `runRoot` 的 SpawnAndWatch 回调中，flagWatch=true 时使用 watch 格式渲染
  - [x] 7.3 spawn 的 OnComplete 事件后正常退出

- [x] Task 8: 测试 (AC: all)
  - [x] 8.1 ATDD tests: AC-2 MethodWatch, AC-3 ProgressPayload, AC-4 KernelCallbacks, AC-5 multi-subscriber, AC-6 handleWatch streaming, AC-10 PID not found, AC-11 DurationMs/HasError — 全部通过
  - [x] 8.2 所有现有测试回归通过（23 个包）

- [x] Task 9: `make all` 全部通过 (AC: all)

## Dev Notes

### 架构决策引用

- **Decision 26**: watch TUI — 三级详细度实时观察 [Source: architecture/core-architectural-decisions.md#decision-26]
- **Decision 24**: 双层架构 — Progress 回调（实时通知）+ StepRecord（磁盘 JSONL 完整数据存储）[Source: architecture/core-architectural-decisions.md#decision-24]

### 关键实现模式

#### 1. IPC watch 方法 — 流式 handler 模式（遵循 attach_debug/attach_log 模式）

**dispatch 注册**（server.go handleConn switch）：
```go
case MethodWatch:
    s.handleWatch(conn, req.Payload)
    return // streaming method — handler manages connection lifetime
```

**handleWatch 三阶段流程**：
```
阶段 A: 验证进程存在
  ├─ s.kern.GetProcess(pid) → 成功
  └─ 失败 → 检查磁盘 steps.jsonl → 仍失败 → ErrorPayload{Code: "not_found"}

阶段 B: 回放历史步骤
  ├─ 从 steps.jsonl 读取所有已记录步骤（使用 bufio.Scanner 逐行）
  ├─ 每行反序列化为 StepRecord
  ├─ 组装 ProgressPayload{Event: "step_complete", Step, Action, Summary, DurationMs, HasError}
  └─ 通过 enc.Encode(StreamEvent{Type: StreamProgress, Payload: ...}) 发送

阶段 C: 实时流式转发
  ├─ eventCh := make(chan StreamEvent, 64)
  ├─ callbackMux.register(pid, eventCh)
  ├─ writeResponse(conn, Response{OK: true})
  └─ select 循环：eventCh 事件 → enc.Encode → StreamComplete/StreamError 时退出
```

**关于历史回放的步骤文件路径解析**：复用 Story 27.2 中的 `resolveStepsPathFromProc` 和 `resolveStepsPathFallback` 辅助函数。

#### 2. callbackMux 多订阅者重构

当前 callbackMux 使用 `SyncMap[PID, chan<- StreamEvent]`（1:1 映射）。watch 需要同一 PID 同时有 spawn 和 watch 两个订阅者。

**推荐方案**：引入 `subscriberList` 包装：
```go
type subscriberList struct {
    mu       sync.Mutex
    channels []chan<- StreamEvent
}

type callbackMux struct {
    handlers *xsync.SyncMap[types.PID, *subscriberList]
}

func (m *callbackMux) register(pid types.PID, ch chan<- StreamEvent) {
    sl := &subscriberList{}
    actual, _ := m.handlers.LoadOrStore(pid, sl)
    actual.mu.Lock()
    actual.channels = append(actual.channels, ch)
    actual.mu.Unlock()
}

func (m *callbackMux) unregister(pid types.PID, ch chan<- StreamEvent) {
    sl, ok := m.handlers.Load(pid)
    if !ok {
        return
    }
    sl.mu.Lock()
    for i, c := range sl.channels {
        if c == ch {
            sl.channels = append(sl.channels[:i], sl.channels[i+1:]...)
            break
        }
    }
    empty := len(sl.channels) == 0
    sl.mu.Unlock()
    if empty {
        m.handlers.Delete(pid)
    }
}

func (m *callbackMux) send(pid types.PID, ev StreamEvent) {
    sl, ok := m.handlers.Load(pid)
    if !ok {
        return
    }
    sl.mu.Lock()
    for _, ch := range sl.channels {
        select {
        case ch <- ev:
        default:
        }
    }
    sl.mu.Unlock()
}
```

**注意**：`unregister` 签名从 `unregister(pid)` 变为 `unregister(pid, ch)`。handleSpawn 中的 `defer s.callbackMux.unregister(pid)` 需改为 `defer s.callbackMux.unregister(pid, eventCh)`。

#### 3. KernelCallbacks 签名变更的影响面

`OnStepComplete` 签名变更会影响所有实现了 `KernelCallbacks` 接口的类型：

1. **`callbackMux`**（ipc/server.go L1373）— 主实现，需更新
2. **测试 mock** — 以下 4 个测试文件实现了 `OnStepComplete`，签名需同步更新：
   - `kernel/atdd_3_6_step_output_streaming_test.go`
   - `cmd/rnix/main_test.go`
   - `ipc/atdd_3_6_step_output_streaming_test.go`
   - `kernel/stem_integration_test.go`
3. **`ipc/server_test.go`** — 可能包含 mock callbacks，需检查

kernel.go 中的调用点约 12 处，均遵循以下模式：
```go
// 变更前
k.callbacks.OnStepComplete(proc.PID, step, "tool_call", summary)

// 变更后
k.callbacks.OnStepComplete(proc.PID, step, "tool_call", summary, time.Since(stepStart), toolErr != "")
```

**hasError 判断逻辑**：
- `tool_call` action：工具返回了 toolErr（非空字符串） → hasError=true
- `plan/text/complete/spawn/replan/specialize` action：正常完成 → hasError=false
- LLM 错误导致 finishProcess → 走 OnError 而非 OnStepComplete → 不影响

**duration 获取**：所有 OnStepComplete 调用点上方都有 `stepStart := time.Now()`，直接 `time.Since(stepStart)` 即可。

#### 4. watch 命令输出格式

Level 1 输出格式设计（简单逐行打印，不使用 BubbleTea）：

```
[step 1] tool_call → /dev/fs  0.2s  ✓
[step 2] tool_call → /dev/shell  1.5s  ✓
[step 3] tool_call → /dev/fs  0.1s  ✗
[step 4/30] thinking...
[step 5] plan → Created plan with 3 steps  0.8s  ✓
[step 6] complete → 分析完成  0.3s  ✓
───────────────────────────
✓ PID 42 completed (exit=0)  total: 6 steps
```

**行格式**：`[step N] {action} → {summary}  {duration}  {status_icon}`

- `{duration}` 格式化：`< 1s` → `0.Xs`，`1-60s` → `Xs`，`> 60s` → `Xm Ys`
- `{status_icon}`：`✓`（正常）或 `✗`（hasError=true）
- ASCII 模式（`RNIX_ASCII=1`）：`OK` / `ERR`

**thinking 行**：`[step N/M] thinking...` — 使用 `\r` 覆盖当前行（避免空行积累），收到 step_complete 后写新行

#### 5. q 键退出的终端处理

watch 需要读取键盘输入以支持 q 退出，但当前是简单终端输出模式（非 BubbleTea）。

**推荐方案**：
```go
// 设置 raw mode 读取单个按键
oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
if err == nil {
    defer term.Restore(int(os.Stdin.Fd()), oldState)
}

go func() {
    buf := make([]byte, 1)
    for {
        n, err := os.Stdin.Read(buf)
        if err != nil || n == 0 {
            return
        }
        if buf[0] == 'q' || buf[0] == 'Q' {
            cancel() // 取消 context，触发 watch 退出
            return
        }
    }
}()
```

需要引入 `golang.org/x/term` 依赖。如果项目中已有此依赖则直接使用；否则可通过 `go get golang.org/x/term` 添加。

**替代方案**（如不想引入 `x/term`）：捕获 SIGINT (Ctrl+C) 退出，不支持 q 键。但 Epic 规范明确要求 q 键，Story 27.4 的 BubbleTea 也需要键盘支持，因此建议现在就用 `x/term`。

#### 6. spawn --watch 实现策略

**关键设计**：`--watch` 不需要额外 IPC 连接。spawn 的 `SpawnAndWatch` 已经在接收完整的 Progress 事件流。只需在渲染回调中切换输出格式：

```go
// main.go runRoot 中
if flagWatch {
    // 使用 watch 格式渲染 step_complete
    renderWatchStepComplete(pp)
} else {
    // 使用现有的 progress 格式
    progress.AgentStepComplete(pp.PID, pp.Step, pp.Action, pp.Summary)
}
```

spawn 完成后，如果 `--watch` 为 true，最终的 complete 事件使用 watch 格式渲染，然后正常退出。

**NFR58**：spawn --watch 的延迟 ≤ 100ms，由于复用同一连接，无额外 IPC 开销，spawn 本身的 OnSpawn 事件已 < 100ms。

#### 7. 历史步骤回放的数据转换

从 StepRecord 转换为 ProgressPayload：
```go
func stepRecordToProgressPayload(rec types.StepRecord, pid types.PID) ProgressPayload {
    return ProgressPayload{
        Event:      "step_complete",
        PID:        pid,
        Step:       rec.Step,
        Action:     rec.Action,
        Summary:    rec.Summary,
        DurationMs: float64(rec.ToolDuration.Microseconds()) / 1000.0,
        HasError:   rec.ToolError != "",
    }
}
```

**注意**：StepRecord 的 ToolDuration 是工具执行时间，不是整个步骤时间。步骤总时间未被 StepRecord 记录（StepRecord.Timestamp 是相对进程开始的时间戳，不是步骤耗时）。对于历史回放，可以通过相邻步骤的 Timestamp 差值近似计算步骤耗时：
```go
duration = records[i].Timestamp - records[i-1].Timestamp  // 第 i 步耗时
```
第 1 步使用 records[0].Timestamp 作为耗时。

### 现有代码关键位置

| 文件 | 作用 | 行号范围 |
|------|------|----------|
| `ipc/protocol.go` | Method 常量定义 | L18-53 |
| `ipc/protocol.go` | ProgressPayload | L309-336 |
| `ipc/protocol.go` | StreamEvent/StreamEventType | L271-307 |
| `ipc/server.go` | handleConn switch 分发 | L282-363 |
| `ipc/server.go` | handleSpawn（streaming handler 模板） | L706-810 |
| `ipc/server.go` | handleAttachDebug（attach 模式模板） | L828-853 |
| `ipc/server.go` | handleAttachLog（含历史回放模板） | L855-900 |
| `ipc/server.go` | callbackMux 完整实现 | L1171-1228（搜索 `type callbackMux`） |
| `ipc/server.go` | callbackMux.OnStepComplete | 搜索 `func (m *callbackMux) OnStepComplete` |
| `ipc/server.go` | resolveStepsPathFromProc / resolveStepsPathFallback | 搜索函数名 |
| `ipc/client.go` | SpawnAndWatch（客户端流式模板） | L105-154 |
| `ipc/client.go` | call/sendRequest/readResponse | L423-459 |
| `kernel/kernel.go` | KernelCallbacks 接口 | L164-172 |
| `kernel/kernel.go` | OnStepComplete 调用点（约 12 处） | 搜索 `k.callbacks.OnStepComplete` |
| `kernel/kernel.go` | stepStart := time.Now() | L1055 |
| `kernel/step_writer.go` | ReadStep 函数 | L68-91 |
| `internal/types/step_record.go` | StepRecord 类型 | L14-29 |
| `cmd/rnix/main.go` | init() 命令注册 | L242-268 |
| `cmd/rnix/main.go` | runRoot spawn 逻辑 + progress 回调 | L377-406 |
| `internal/ui/progress.go` | ProgressReporter 输出模式 | 搜索 AgentStepComplete |

### 现有类型参考

**ProgressPayload**（`ipc/protocol.go` L309-336）— 见上方 AC-3，需追加 2 个字段。

**KernelCallbacks**（`kernel/kernel.go` L164-172）：
```go
type KernelCallbacks interface {
    OnSpawn(pid types.PID, intent, provider, model string)
    OnStep(pid types.PID, step int, total int)
    OnStepComplete(pid types.PID, step int, action string, summary string)
    OnComplete(pid types.PID, result string, exit ExitStatus)
    OnError(pid types.PID, err error)
}
```

**callbackMux**（`ipc/server.go`）— 当前实现：
```go
type callbackMux struct {
    handlers *xsync.SyncMap[types.PID, chan<- StreamEvent]
}
```

**StepRecord**（`internal/types/step_record.go`）：
```go
type StepRecord struct {
    Step           int             `json:"step"`
    Timestamp      time.Duration   `json:"timestamp"`
    Messages       json.RawMessage `json:"messages"`
    MessageCount   int             `json:"message_count"`
    TokenCount     int             `json:"token_count"`
    RawResponse    string          `json:"raw_response"`
    Action         string          `json:"action"`
    Summary        string          `json:"summary"`
    ToolPath       string          `json:"tool_path,omitempty"`
    ToolInput      string          `json:"tool_input,omitempty"`
    ToolResult     string          `json:"tool_result,omitempty"`
    ToolError      string          `json:"tool_error,omitempty"`
    ToolDuration   time.Duration   `json:"tool_duration,omitempty"`
    RequestTokens  int             `json:"request_tokens"`
    ResponseTokens int             `json:"response_tokens"`
}
```

### 并发安全模型

- **callbackMux.subscriberList**：内部 `sync.Mutex` 保护 channels 切片，send/register/unregister 均在 Lock 下操作
- **handleWatch 与 handleSpawn 并发**：同一 PID 的两个 handler 各自持有独立 eventCh，通过 callbackMux 的多订阅者广播机制同时接收事件
- **历史回放与实时流的衔接**：先回放再 register，可能丢失回放期间的事件。解决方案：先 register → 再回放 → 用 step 号去重（回放发送的 step 号 ≤ 实时事件的 step 号则忽略）。client 端维护 `lastReplayedStep` 变量
- **q 键退出**：通过 `context.Cancel` 触发 watch 退出，goroutine 读取 stdin 在单独线程

### 组合矩阵

| 现有功能 | 交互点 | 需验证 | 说明 |
|----------|--------|--------|------|
| spawn 命令 | --watch flag 共存 | 是 | spawn 完成后无缝切换到 watch 格式输出 |
| strace | 独立功能 | 否 | strace 使用 DebugChan，watch 使用 callbackMux，互不干扰 |
| gdb attach | 共存 | 否 | gdb 使用独立的 detachCh/gdbChan，不影响 callbackMux |
| log attach | 共存 | 否 | log 使用 logCh，不影响 callbackMux |
| top 命令 | 后续 Story 27.5 集成 | 否 | 当前 Story 不涉及 top 集成 |
| callbackMux 现有用户 | handleSpawn 兼容性 | 是 | 重构为多订阅者后 handleSpawn 需更新 unregister 签名 |
| intent 系统 | spawn 内部使用 callbackMux | 是 | intent 的 spawn 使用同一 callbackMux，需确认 unregister 兼容 |

### 依赖关系

- Story 27.1（StepRecord + StepWriter）— 已完成，提供 steps.jsonl 数据源
- Story 27.2（GetStepDetail IPC）— 已完成，提供 resolveStepsPath 辅助函数可复用
- Story 27.4（三级详细度 + BubbleTea）— 后续 Story，依赖本 Story 的 watch 基础

### 不需要做的事情

- 不需要 BubbleTea TUI（留给 Story 27.4）
- 不需要 Level 2/3 详细度展开（留给 Story 27.4）
- 不需要 p 键查看 prompt（留给 Story 27.4）
- 不需要 top↔watch 双向导航（留给 Story 27.5）
- 不需要修改 StepRecord 类型
- 不需要修改 StepWriter
- 不需要修改 reasonStep 循环的数据采集逻辑（仅修改 callback 调用签名）
- 不需要修改 reap 逻辑

### Story 27.1/27.2 完成情况（前序 Story 要点）

**27.1**:
- StepRecord 类型已实现，Messages 使用 `json.RawMessage`
- StepWriter 64KB buffered NDJSON writer 已就位
- ReadStep(path, targetStep) 辅助函数已实现（顺序扫描，1MB max line buffer）
- Process.FinalSystemPrompt 和 Process.stepWriter 字段已添加
- reaper 写入 process-meta.json 已实现

**27.2**:
- GetStepDetail IPC 方法已实现（protocol + server handler + client）
- `resolveStepsPathFromProc` + `resolveStepsPathFallback` 辅助函数已实现
- MessageWire、ToolCallWire、ToolDefWire 类型已定义
- `kernel.GetStepDataDir()` getter 已添加
- Process 的 getter/setter 方法已添加（GetFinalSystemPrompt, GetNativeToolDefs, GetProjectConfig）

### Git 近期提交参考

```
0f05c6e feat: 27-2 Implement GetStepDetail IPC method
9cf1d28 feat: Finalize Story 27.1 implementation
08675be feat: ds 27-1 Implement StepRecord type and StepWriter
```

### Project Structure Notes

- 新增文件：`cmd/rnix/watch.go`
- 修改文件：`ipc/protocol.go`（Method 常量 + WatchRequest + ProgressPayload 扩展）、`ipc/server.go`（handleWatch + callbackMux 重构 + dispatch case）、`ipc/client.go`（WatchProcess 方法）、`kernel/kernel.go`（OnStepComplete 签名 + 调用点）、`cmd/rnix/main.go`（注册 watchCmd + spawn --watch flag）
- `golang.org/x/term` 已在 go.mod 中（v0.40.0），无需新增依赖

### References

- [Architecture Decision 26: watch TUI](../_bmad-output/planning-artifacts/architecture/core-architectural-decisions.md#decision-26)
- [Architecture Decision 24: 双层架构](../_bmad-output/planning-artifacts/architecture/core-architectural-decisions.md#decision-24)
- [Epic 27 Story 27.3](../_bmad-output/planning-artifacts/epics/epic-27-统一观察系统-unified-observation-system.md)
- [Story 27.2 实现记录](../_bmad-output/implementation-artifacts/27-2-getstepdetail-ipc-method.md)
- [Story 27.1 实现记录](../_bmad-output/implementation-artifacts/27-1-steprecord-type-and-disk-writer.md)

## Dev Agent Record

### Agent Model Used

Claude claude-4.6-opus (via Cursor)

### Debug Log References

无异常，一次通过。

### Completion Notes List

- 在 `ipc/protocol.go` 新增 `MethodWatch` 常量 + `WatchRequest` 结构体 + `ProgressPayload` 新增 `HasError`/`DurationMs` 字段
- 修改 `kernel/kernel.go` `KernelCallbacks.OnStepComplete` 签名：追加 `duration time.Duration, hasError bool`，更新 11 处调用点
- 重构 `ipc/server.go` `callbackMux` 为多订阅者模式：引入 `subscriberList` 结构体，`register/unregister/send` 支持同一 PID 多个 channel
- 新增 `ipc/server.go` `handleWatch` handler：验证进程 → 回放 steps.jsonl 历史 → 实时转发 Progress 事件 → 步骤去重
- 新增 `ipc/client.go` `WatchProcess(pid, onEvent)` 客户端方法
- 新增 `cmd/rnix/watch.go`：`rnix watch <pid>` 命令，Level 1 逐行输出，q 键退出（x/term raw mode），RNIX_ASCII 兼容
- 新增 `cmd/rnix/main.go` `--watch` flag：spawn 时使用 watch 格式渲染 step 事件
- 更新 4 个测试文件的 `OnStepComplete` mock 签名以匹配新接口
- ATDD 14 个测试全部通过，`make all` 23 包全绿

### Code Review Fixes (CR Pass)

Code Review 发现 8 个 PATCH 项，全部修复：
1. **AC-9 q 键退出修复**：`readQuitKey` 现在同时调用 `cancel()` + `client.Close()`，强制关闭 IPC 连接使 `scanner.Scan()` 返回，watch 立即退出；`ctx.Err()` 检查避免误报 connection error
2. **StreamComplete 丢失修复**：`callbackMux.OnComplete` 现在实际广播 StreamComplete 到所有订阅者（之前是空实现）；handleWatch 通过 callbackMux 接收完成事件，不再直接读 `proc.Done`（避免与 handleSpawn 竞争）
3. **spawn --watch complete 状态行修复**：`SpawnAndWatch` 回调现在对 StreamComplete/StreamError 事件也调用 `renderWatchEvent`
4. **分隔线 ASCII 降级**：`───` 在 `RNIX_ASCII=1` 时降级为 `---`
5. **WatchProcess scanner.Err() 检查**：循环结束后检查扫描错误
6. **--watch flag 描述修正**：改为 "Stream reasoning steps in watch format during spawn"
7. **doneCh 死代码移除**：handleWatch 不再使用 doneCh goroutine，直接通过 callbackMux 接收 complete 事件
8. **DetectProfile 缓存**：`runRoot` 中 `ui.DetectProfile` 移至回调外部一次性调用

### File List

- `ipc/protocol.go` — 新增 MethodWatch + WatchRequest + ProgressPayload 扩展
- `kernel/kernel.go` — KernelCallbacks 签名变更 + 11 处调用点更新 + `time` import
- `ipc/server.go` — callbackMux 多订阅者重构 + handleWatch handler + dispatch case + OnComplete 广播 + 回放 scanner.Err 检查
- `ipc/client.go` — 新增 WatchProcess 客户端方法 + scanner.Err() 检查
- `cmd/rnix/watch.go` — 新增 watch 命令（全新文件）+ q 键关闭连接 + ASCII 分隔线
- `cmd/rnix/main.go` — 注册 watchCmd + flagWatch + --watch flag + spawn watch 渲染 + complete 事件透传 + DetectProfile 缓存
- `ipc/server_test.go` — unregister 签名更新
- `kernel/atdd_3_6_step_output_streaming_test.go` — OnStepComplete 签名更新
- `ipc/atdd_3_6_step_output_streaming_test.go` — OnStepComplete 调用签名更新
- `ipc/atdd_27_3_watch_command_test.go` — 新增 ATDD 测试 + 完成事件改用 callbackMux.OnComplete
- `kernel/stem_integration_test.go` — OnStepComplete 签名更新
- `cmd/rnix/main_test.go` — cliCallbacks OnStepComplete 签名更新
