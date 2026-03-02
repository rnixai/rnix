# Story 10.2: crux log 分类推理日志

Status: review

## Story

As a 用户,
I want 通过 `crux log <pid>` 查看智能体的推理日志，按类别分类显示,
So that 我无需深入内核就能排查问题。

## Acceptance Criteria

1. **AC1: 基本日志输出**
   - Given `cmd/crux/log.go` 已实现
   - When 执行 `crux log 5`
   - Then 输出 PID 5 的推理日志
   - And 按 `[think]`（推理过程）、`[tool]`（工具调用）、`[output]`（最终输出）三段式分类显示（FR60）

2. **AC2: 过滤功能**
   - Given 使用过滤
   - When 执行 `crux log 5 --filter tool`
   - Then 仅显示 `[tool]` 类别的日志条目

3. **AC3: 低延迟**
   - Given 日志输出
   - When 从推理事件发生到终端显示
   - Then 延迟 ≤ 200ms（NFR29）

4. **AC4: PID 不存在处理**
   - Given PID 不存在
   - When 执行 `crux log 999`
   - Then 输出 `✗ PID 999: process not found` + 建议（与 astrace 错误模式对齐）

5. **AC5: JSON 输出**
   - Given 使用 `--json` flag
   - When 执行 `crux log 5 --json`
   - Then 输出 NDJSON 格式的日志条目（每行一个 JSON 对象，含 category/content/timestamp 字段）

6. **AC6: 实时流式**
   - Given 进程正在运行
   - When 执行 `crux log 5`
   - Then 实时流式输出新产生的日志条目（类似 `tail -f`）
   - And 进程退出后自动断开并提示

7. **AC7: Ctrl+C 安全断开**
   - Given `crux log` 运行中
   - When 按 Ctrl+C
   - Then 断开日志流，不影响被追踪进程

## Tasks / Subtasks

- [x] Task 1: 定义 LogEntry 类型和 LogChan 基础设施 (AC: #1, #3)
  - [x] 1.1 在 `internal/types/types.go` 中定义 `LogCategory` 类型（`think`/`tool`/`output`）和 `LogEntry` 结构体
  - [x] 1.2 在 `kernel/process.go` 的 `Process` 中添加 `LogChan chan types.LogEntry`（缓冲 256，与 DebugChan 对齐）
  - [x] 1.3 在 `kernel/reap.go` 的 `reapProcess` 中添加 LogChan 关闭逻辑（与 DebugChan 相同的 nil-out-under-lock 模式）
- [x] Task 2: 在 reasonStep 中 emit LogEntry (AC: #1, #3)
  - [x] 2.1 在 `kernel/kernel.go` 添加 `emitLog` 辅助方法（与 emitEvent 并行，非阻塞写入 LogChan）
  - [x] 2.2 LLM 响应解析后、action 判定前：emit `[think]` 条目（Content = resp.Content，即 LLM 的完整推理文本）
  - [x] 2.3 工具调用执行完成后：emit `[tool]` 条目（Content = 工具路径 + 工具结果摘要）
  - [x] 2.4 最终文本输出时：emit `[output]` 条目（Content = 最终输出文本）
- [x] Task 3: IPC 协议扩展 (AC: #1, #6)
  - [x] 3.1 在 `ipc/protocol.go` 添加 `MethodAttachLog Method = "attach_log"`
  - [x] 3.2 定义 `AttachLogRequest` 和 `LogEntryWire` 类型（时间戳用毫秒）
  - [x] 3.3 添加 `StreamLogEntry StreamEventType = "log_entry"` 流事件类型
- [x] Task 4: IPC Server handler (AC: #1, #6)
  - [x] 4.1 在 `kernel/kernel.go` 添加 `GetLogChan(pid) (chan LogEntry, bool)` 方法（与 GetDebugChan 对齐）
  - [x] 4.2 在 `ipc/server.go` 的 `handleConn` switch 中添加 `case MethodAttachLog`
  - [x] 4.3 实现 `handleAttachLog`：获取 LogChan，流式编码 LogEntryWire（与 handleAttachDebug 模式一致）
- [x] Task 5: IPC Client 方法 (AC: #1, #6)
  - [x] 5.1 在 `ipc/client.go` 添加 `AttachLog(pid, onEntry func(LogEntryWire)) error`（与 AttachDebug 模式一致）
- [x] Task 6: 实现 `cmd/crux/log.go` CLI 命令 (AC: #1-#7)
  - [x] 6.1 创建 `cmd/crux/log.go`，定义 `logCmd` cobra 命令（Use: "log <pid>"）
  - [x] 6.2 添加 `--filter` string flag（合法值：think/tool/output，空=全部）
  - [x] 6.3 实现 `runLog`：解析 PID、Dial IPC、设置信号处理、调用 AttachLog
  - [x] 6.4 实现人类可读格式化：`[think]` 灰色、`[tool]` 蓝色、`[output]` 绿色（复用 `internal/ui/styles.go` 颜色）
  - [x] 6.5 实现 JSON 格式化：NDJSON 每行一个 LogEntryWire
  - [x] 6.6 实现 --filter 过滤逻辑（在 onEntry 回调中跳过不匹配的 category）
  - [x] 6.7 在 `cmd/crux/main.go` 的 `init()` 中注册 `rootCmd.AddCommand(logCmd)`
- [x] Task 7: 格式化与 UI (AC: #1, #2)
  - [x] 7.1 在 `internal/ui/` 中添加 `FormatLogEntry` 函数（或在 log.go 中内联，视复杂度决定）
  - [x] 7.2 日志输出格式：`[HH:MM:SS.sss] [category] content`（时间戳对齐 astrace 的相对时间格式）
- [x] Task 8: 测试 (AC: all)
  - [x] 8.1 单元测试：LogEntry emit 逻辑（mock Process with LogChan，验证 think/tool/output 分类正确）
  - [x] 8.2 单元测试：--filter 过滤逻辑（验证各 category 过滤）
  - [x] 8.3 单元测试：LogEntryWire 序列化/反序列化
  - [x] 8.4 单元测试：格式化输出（人类可读 + JSON 模式）
  - [x] 8.5 在 `cmd/crux/main_test.go` 中确认 `log` 命令注册
  - [x] 8.6 单元测试：PID 不存在场景

## Dev Notes

### 关键架构约束

- **依赖方向**：`cmd/crux/` → `ipc/` → `vfs/`（ProcInfo 类型），`cmd/crux/` → `internal/ui/`（styles）
- **新文件位置**：`cmd/crux/log.go`（所有 CLI 逻辑集中在此文件，与 `top.go`、`compose.go` 同级）
- **LogChan 与 DebugChan 并行**：两个独立通道，互不干扰。DebugChan 传递低级 SyscallEvent，LogChan 传递高级 LogEntry
- **不修改 astrace**：`crux log` 是 astrace 的高级替代，面向用户排障；astrace 面向开发者调试 syscall

### LogEntry 类型定义

```go
// internal/types/types.go 中添加

type LogCategory string

const (
    LogThink  LogCategory = "think"
    LogTool   LogCategory = "tool"
    LogOutput LogCategory = "output"
)

type LogEntry struct {
    Timestamp time.Duration // 相对进程启动时间
    PID       PID
    Step      int           // 推理步骤号
    Category  LogCategory
    Content   string        // 日志内容
    ToolPath  string        // 仅 tool 类别有值
}
```

### 三段式分类映射（核心设计）

reasonStep 中的数据流→LogEntry 映射：

| 推理阶段 | 数据来源 | LogCategory | Content 内容 |
|----------|---------|-------------|-------------|
| LLM 响应到达 | `resp.Content`（解析前的原始 LLM 文本） | `think` | LLM 的完整推理/回复文本 |
| 工具调用执行 | `action.ToolPath` + `toolResult` | `tool` | `工具路径 → 结果摘要（截断到 500 字符）` |
| 最终文本输出 | `action.Content`（ActionText 分支） | `output` | 最终输出全文 |

**emit 位置（在 kernel/kernel.go reasonStep 中）：**

```go
// 解析 LLM 响应后，parseAction 之前
k.emitLog(proc, step, types.LogThink, resp.Content, "")

// ActionToolCall 分支，工具执行完成后
k.emitLog(proc, step, types.LogTool, string(toolResult), action.ToolPath)

// ActionText 分支，设置 proc.Result 后
k.emitLog(proc, step, types.LogOutput, action.Content, "")
```

### emitLog 辅助方法

```go
func (k *KernelImpl) emitLog(proc *Process, step int, cat types.LogCategory, content, toolPath string) {
    entry := types.LogEntry{
        Timestamp: time.Since(proc.CreatedAt),
        PID:       proc.PID,
        Step:      step,
        Category:  cat,
        Content:   content,
        ToolPath:  toolPath,
    }
    proc.mu.Lock()
    ch := proc.LogChan
    if ch != nil {
        select {
        case ch <- entry:
        default: // buffer full, drop
        }
    }
    proc.mu.Unlock()
}
```

### IPC 协议扩展

```go
// ipc/protocol.go 中添加

const MethodAttachLog Method = "attach_log"

type AttachLogRequest struct {
    PID types.PID `json:"pid"`
}

type LogEntryWire struct {
    TimestampMs int64  `json:"timestamp_ms"`
    PID         types.PID `json:"pid"`
    Step        int    `json:"step"`
    Category    string `json:"category"` // "think", "tool", "output"
    Content     string `json:"content"`
    ToolPath    string `json:"tool_path,omitempty"`
}

const StreamLogEntry StreamEventType = "log_entry"

func LogEntryToWire(e types.LogEntry) LogEntryWire {
    return LogEntryWire{
        TimestampMs: e.Timestamp.Milliseconds(),
        PID:         e.PID,
        Step:        e.Step,
        Category:    string(e.Category),
        Content:     e.Content,
        ToolPath:    e.ToolPath,
    }
}
```

### IPC Server handleAttachLog

与 `handleAttachDebug` 完全对齐的模式：

```go
func (s *Server) handleAttachLog(conn net.Conn, rawPayload json.RawMessage) {
    var req AttachLogRequest
    json.Unmarshal(rawPayload, &req)

    logCh, ok := s.kern.GetLogChan(req.PID)
    if !ok || logCh == nil {
        writeResponse(conn, Response{OK: false, Error: &ErrorPayload{
            Code: "NOT_FOUND", Message: "process not found or no log channel",
        }})
        return
    }

    writeResponse(conn, Response{OK: true})

    enc := json.NewEncoder(conn)
    for entry := range logCh {
        lew := LogEntryToWire(entry)
        payload, _ := json.Marshal(lew)
        se := StreamEvent{Type: StreamLogEntry, Payload: payload}
        if err := enc.Encode(se); err != nil {
            return
        }
    }
    _ = enc.Encode(StreamEvent{Type: StreamEOF})
}
```

**注意**：与 handleAttachDebug 一样，handleConn 中必须加 `return`（streaming method，handler 管理连接生命周期）。

### IPC Client AttachLog

```go
func (c *Client) AttachLog(pid types.PID, onEntry func(LogEntryWire)) error {
    if err := c.sendRequest(MethodAttachLog, AttachLogRequest{PID: pid}); err != nil {
        return err
    }
    // read response...
    for c.scanner.Scan() {
        var ev StreamEvent
        json.Unmarshal(c.scanner.Bytes(), &ev)
        if ev.Type == StreamEOF { break }
        if ev.Type == StreamLogEntry && onEntry != nil {
            var lew LogEntryWire
            json.Unmarshal(ev.Payload, &lew)
            onEntry(lew)
        }
    }
    return nil
}
```

### CLI 命令模式（参考 astrace 实现）

`cmd/crux/log.go` 的 runLog 与 runAstrace 高度对齐：

1. 解析 PID 参数（`cobra.ExactArgs(1)`）
2. `ipc.Dial(ipc.SocketPath())` 连接 daemon
3. 设置 `context.WithCancel` + 信号处理（SIGINT/SIGTERM → cancel）
4. goroutine 中调用 `client.AttachLog(pid, onEntry)` 
5. onEntry 回调中：
   - 检查 `--filter` 过滤（skip 不匹配的 category）
   - `--json` 模式：输出原始 LogEntryWire JSON
   - 人类可读模式：格式化输出
6. select 等待 errCh 或 ctx.Done()

```go
var logCmd = &cobra.Command{
    Use:     "log <pid>",
    Short:   "View categorized reasoning logs of an agent process",
    Long:    "Stream reasoning logs from a running agent, categorized as [think], [tool], and [output].\n\nPress Ctrl+C to detach without affecting the traced process.",
    Example: `  crux log 5                   Stream all log categories
  crux log 5 --filter tool     Show only tool call logs
  crux log 5 --filter think    Show only reasoning logs
  crux log 5 --json            Output as NDJSON stream`,
    Args: cobra.ExactArgs(1),
    RunE: runLog,
}
```

`--filter` flag 定义：
```go
var flagFilter string
logCmd.Flags().StringVar(&flagFilter, "filter", "", "Filter by log category (think, tool, output)")
```

### 人类可读输出格式

```
[crux log] attached to PID 5

[  0.523] [think]  系统分析了代码库结构，发现 main.go 中有潜在的竞态条件...
[  0.524] [tool]   /dev/fs → Read src/main.go (2,847 bytes)
[  1.203] [think]  检查了 main.go 的第 45-67 行，确认存在未保护的并发写入...
[  1.204] [tool]   /dev/fs → Write fix.patch (156 bytes)
[  2.100] [output] 修复了 main.go 中的竞态条件：在第 52 行添加了 sync.Mutex 保护...

[crux log] detached from PID 5 (process exited)
```

**颜色方案**（复用 `internal/ui/styles.go`）：
- `[think]` → `ColorMuted`（#666666，灰色）— 推理过程是辅助信息
- `[tool]` → `ColorAgent`（#5B9BD5，蓝色）— 工具调用是关键操作
- `[output]` → `ColorSuccess`（#6BCB77，绿色）— 最终输出是结果

时间戳格式复用 `FormatDuration`（已在 10-1 中导出为 `ui.FormatDuration`）。

### 复用现有代码

**必须复用（不要重新实现）：**
- `internal/ui/styles.go`：`InitStyles(profile)`、颜色常量、样式定义
- `internal/ui/table.go`：`FormatDuration(d)`（已在 Story 10-1 中导出）
- `internal/ui/render.go`：`NewRenderer(w, mode)`、`RenderError()` 错误渲染
- `ipc/client.go`：`Dial()`、`Close()`、`sendRequest()`、scanner 模式
- `cmd/crux/main.go`：`resolveOutputMode()`、`wireToSyscallEvent()` 参考模式

**从 astrace 复用的模式（不复制代码，复用架构模式）：**
- 信号处理：`context.WithCancel` + `signal.Notify` + goroutine cancel
- IPC 流式消费：`client.AttachXxx(pid, callback)` + `select errCh/ctx.Done()`
- daemon 未运行处理：`ui.RenderError()` 格式化错误提示

### LogChan 生命周期管理

与 DebugChan 完全对齐：

1. **创建**：`NewProcess()` 中 `LogChan: make(chan types.LogEntry, 256)`
2. **写入**：`emitLog()` 中 `proc.mu.Lock()` → nil check → non-blocking send → `proc.mu.Unlock()`
3. **关闭**：`reapProcess()` 中与 DebugChan 相同的 nil-out-under-lock 模式：
   ```go
   proc.mu.Lock()
   lch := proc.LogChan
   proc.LogChan = nil
   proc.mu.Unlock()
   if lch != nil {
       close(lch)
   }
   ```
4. **消费端**：`for entry := range logCh` 在 close 后自动退出

### Kernel 接口扩展

在 `KernelImpl` 上添加 `GetLogChan` 方法（与 `GetDebugChan` 对齐）：

```go
func (k *KernelImpl) GetLogChan(pid types.PID) (chan types.LogEntry, bool) {
    proc, ok := k.procTable.Load(pid)
    if !ok {
        return nil, false
    }
    proc.mu.Lock()
    ch := proc.LogChan
    proc.mu.Unlock()
    return ch, true
}
```

IPC Server 的 `KernelProvider` 接口（`ipc/server.go` 中的 `Kernel` 接口）需要添加 `GetLogChan` 方法签名。

### 性能约束（NFR29: ≤ 200ms）

- LogChan 缓冲 256：与 DebugChan 相同，防止 reasonStep 阻塞
- Unix socket IPC 编码传输：典型延迟 < 1ms
- 终端格式化 + 输出：< 1ms
- **瓶颈在 `emitLog` 时机**：在 reasonStep 中 LLM 返回后立即 emit，工具执行完成后立即 emit
- 总端到端延迟（emit → IPC → 格式化 → stdout）预计 < 5ms，远低于 200ms 要求

### 边界情况

- **DebugChan 为 nil 但 LogChan 非 nil**：可能发生（理论上不会，因为两者都在 NewProcess 中初始化）。emitLog 独立检查 LogChan。
- **多个消费者**：与 DebugChan 相同，LogChan 只能有一个消费者（Go channel 语义）。如果已有 `crux log` 连接，第二个连接看不到事件。IPC server 中 `GetLogChan` 返回同一个 channel。
  - **设计决策**：MVP 阶段接受单消费者限制（与 astrace 一致），后续可考虑 fan-out。
- **进程已退出**：`GetLogChan` 返回 nil channel（reapProcess 已 nil-out），IPC handler 返回 NOT_FOUND。
- **--filter 无效值**：如果 filter 值不是 think/tool/output，在 runLog 开始时验证并报错退出。
- **tool result 过大**：`[tool]` 条目的 Content 截断到 500 字符，避免日志爆炸。截断后追加 `... (truncated, N bytes total)`。

### 命令注册模式

在 `cmd/crux/main.go` 的 `init()` 中添加：
```go
logCmd.Flags().StringVar(&flagFilter, "filter", "", "Filter by category (think, tool, output)")
rootCmd.AddCommand(logCmd)
```

`flagFilter` 可以定义在 `log.go` 文件顶部（文件级变量），在 `init()` 中通过 `logCmd.Flags()` 注册。

### Daemon 未运行处理

与 astrace/top 对齐：
```go
if err != nil {
    renderer := ui.NewRenderer(w, mode)
    ui.InitStyles(renderer.Profile)
    ui.RenderError(renderer, fmt.Sprintf("PID %d", pid),
        "no active daemon (process not found)", "",
        "crux ps  查看活跃进程")
    return nil
}
```

### 测试策略

- **emitLog 单元测试**：构造 Process with LogChan，调用 emitLog，验证 channel 接收到正确的 LogEntry
- **分类正确性**：mock LLM 响应（text/tool_call），运行 reasonStep 逻辑片段，验证 LogChan 收到的 category 序列
- **过滤逻辑**：纯函数测试，传入不同 filter 值和 LogEntryWire，验证是否过滤正确
- **格式化输出**：构造 LogEntryWire，调用格式化函数，验证输出字符串包含 `[think]`/`[tool]`/`[output]` 标签
- **Wire 转换**：`LogEntryToWire` / 反向转换的往返一致性
- **命令注册**：在 `main_test.go` 中验证 `log` 命令可被识别
- **PID 不存在**：mock IPC client 返回 NOT_FOUND，验证错误输出

### Project Structure Notes

- **新文件**：`cmd/crux/log.go`（CLI 命令 + 格式化）、`cmd/crux/log_test.go`（测试）
- **修改文件**：
  - `internal/types/types.go` — 添加 LogCategory、LogEntry
  - `kernel/process.go` — Process 添加 LogChan 字段，NewProcess 初始化
  - `kernel/kernel.go` — 添加 emitLog 方法，reasonStep 中 emit 日志，添加 GetLogChan
  - `kernel/reap.go` — reapProcess 中关闭 LogChan
  - `ipc/protocol.go` — 添加 MethodAttachLog、AttachLogRequest、LogEntryWire、StreamLogEntry
  - `ipc/server.go` — handleConn 添加 case、实现 handleAttachLog
  - `ipc/client.go` — 添加 AttachLog 方法
  - `cmd/crux/main.go` — init() 注册 logCmd
- **不修改**：astrace 相关代码、DebugChan 相关代码、top.go、驱动层
- **依赖不变**：不需要新的外部依赖

### References

- [Source: _bmad-output/planning-artifacts/epics/epic-10-监控supervisor-与运维monitoring-supervisor-operations.md#Story 10.2]
- [Source: _bmad-output/planning-artifacts/archive/prd.md#FR59, FR60, NFR29]
- [Source: _bmad-output/planning-artifacts/archive/architecture.md#Decision 5: 调试架构]
- [Source: _bmad-output/project-context.md#Channel 使用规则]
- [Source: kernel/kernel.go#reasonStep, emitEvent]
- [Source: kernel/process.go#Process 结构体, DebugChan]
- [Source: kernel/reap.go#reapProcess DebugChan 关闭]
- [Source: ipc/protocol.go#AttachDebug 协议模式]
- [Source: ipc/server.go#handleAttachDebug 实现]
- [Source: ipc/client.go#AttachDebug 客户端]
- [Source: cmd/crux/main.go#runAstrace 参考实现]
- [Source: internal/ui/styles.go#颜色常量]
- [Source: internal/ui/table.go#FormatDuration（已导出）]
- [Source: internal/types/types.go#SyscallEvent 结构体参考]

## Dev Agent Record

### Agent Model Used

Claude claude-4.6-opus (via Cursor)

### Debug Log References

N/A

### Completion Notes List

- LogCategory (think/tool/output) 和 LogEntry 类型添加到 internal/types/types.go
- Process 结构体新增 LogChan（缓冲 256），NewProcess 中初始化
- reapProcess 中 LogChan 使用与 DebugChan 相同的 nil-out-under-lock 关闭模式
- emitLog 辅助方法实现非阻塞写入，与 emitEvent 并行
- reasonStep 中三个 emit 位置：LLM 响应后 emit [think]，工具执行后 emit [tool]（含 500 字符截断），最终输出时 emit [output]
- IPC 协议扩展：MethodAttachLog、AttachLogRequest、LogEntryWire、StreamLogEntry
- handleAttachLog 服务端实现与 handleAttachDebug 完全对齐
- AttachLog 客户端方法与 AttachDebug 模式一致
- cmd/crux/log.go 实现完整 CLI：--filter (think/tool/output)、--json (NDJSON)、Ctrl+C 安全断开
- FormatLogEntry 使用颜色：think=MutedStyle(灰)、tool=AgentStyle(蓝)、output=SuccessStyle(绿)
- 时间戳格式：相对进程启动时间的秒数（7.3f 格式），与 astrace 对齐
- 已有 red-phase 测试（kernel/log_test.go, ipc/log_test.go）全部通过
- 新增 cmd/crux/log_test.go 覆盖格式化、过滤验证、命令注册、PID 不存在场景
- 全套 17 个包测试通过，零回归，-race 检测通过

### Change Log

- 2026-03-02: Story 10.2 实现完成 — crux log 分类推理日志命令

### File List

- `internal/types/types.go` — 添加 LogCategory、LogEntry 类型
- `kernel/process.go` — Process 添加 LogChan 字段，NewProcess 初始化
- `kernel/kernel.go` — 添加 emitLog、GetLogChan 方法，reasonStep 中 emit [think]/[tool]/[output]
- `kernel/reap.go` — reapProcess 中关闭 LogChan（nil-out-under-lock 模式）
- `ipc/protocol.go` — 添加 MethodAttachLog、AttachLogRequest、LogEntryWire、StreamLogEntry、LogEntryToWire
- `ipc/server.go` — handleConn 添加 case MethodAttachLog，实现 handleAttachLog
- `ipc/client.go` — 添加 AttachLog 方法
- `cmd/crux/log.go` — 新文件：logCmd、runLog、FormatLogEntry、formatLogTimestamp
- `cmd/crux/log_test.go` — 新文件：CLI 层测试
- `cmd/crux/main.go` — init() 注册 logCmd
