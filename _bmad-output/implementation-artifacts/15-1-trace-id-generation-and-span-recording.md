# Story 15.1: Trace ID 生成与 Span 记录

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a 平台构建者,
I want 系统自动为 Compose 编排生成 Trace ID 并在智能体间传播，每个智能体记录 Span 数据,
So that 我可以获得跨进程的完整因果链数据。

## Acceptance Criteria

1. **Given** 一个 Compose 编排启动
   **When** `compose up` 执行
   **Then** 系统生成唯一 Trace ID，Compose 内所有 Spawn 的进程自动继承该 Trace ID 并生成独立 SpanID

2. **Given** 智能体 A 通过 IPC 向智能体 B 发送消息
   **When** Send/Recv 执行
   **Then** Trace ID 作为消息元数据自动携带，B 的 Span 记录 parent 指向 A 的 Span

3. **Given** 智能体执行过程中
   **When** 每个 syscall 被调用
   **Then** 系统在 Span 中记录起止时间、syscall 序列和 token 消耗
   **And** Trace/Span 传播不增加 IPC 延迟超过 10ms（NFR33）

## Tasks / Subtasks

- [x] Task 1: Trace/Span 数据类型定义 (AC: #1, #3)
  - [x] 1.1 在 `internal/types/types.go` 中新增 `TraceID` 和 `SpanID` 类型（`type TraceID string`、`type SpanID string`）
  - [x] 1.2 在 `debug/trace.go` 中定义 `Span` 结构体：
    ```go
    type Span struct {
        TraceID      types.TraceID  `json:"trace_id"`
        SpanID       types.SpanID   `json:"span_id"`
        ParentSpanID types.SpanID   `json:"parent_span_id,omitempty"`
        PID          types.PID      `json:"pid"`
        Name         string         `json:"name"`
        StartTime    time.Time      `json:"start_time"`
        EndTime      time.Time      `json:"end_time,omitempty"`
        Duration     time.Duration  `json:"duration,omitempty"`
        SyscallCount int            `json:"syscall_count"`
        TokensUsed   int            `json:"tokens_used"`
        Status       SpanStatus     `json:"status"`
        Events       []SpanEvent    `json:"events,omitempty"`
    }
    ```
  - [x] 1.3 定义 `SpanEvent` 结构体（Span 内的关键事件标记）：
    ```go
    type SpanEvent struct {
        TimestampMs int64          `json:"timestamp_ms"`
        Name        string         `json:"name"`
        Attrs       map[string]any `json:"attrs,omitempty"`
    }
    ```
  - [x] 1.4 定义 `SpanStatus` 类型（`OK`、`ERROR`、`TIMEOUT`）
  - [x] 1.5 实现 `GenerateTraceID() TraceID` 和 `GenerateSpanID() SpanID`（使用 `crypto/rand` 生成 16 字节 hex 字符串）

- [x] Task 2: Process 扩展 TraceID/SpanID (AC: #1)
  - [x] 2.1 在 `kernel/process.go` 的 `Process` 结构体中新增字段：
    ```go
    TraceID      types.TraceID
    SpanID       types.SpanID
    ParentSpanID types.SpanID
    ```
  - [x] 2.2 在 `kernel/kernel.go` 的 `SpawnOpts` 中新增字段：
    ```go
    TraceID      types.TraceID // 继承的 TraceID；空 = 无追踪
    ParentSpanID types.SpanID  // 父进程的 SpanID
    ```
  - [x] 2.3 修改 `NewProcess`（`kernel/process.go`）：如果 `SpawnOpts.TraceID` 非空，设置 `proc.TraceID = opts.TraceID`、`proc.ParentSpanID = opts.ParentSpanID`、`proc.SpanID = GenerateSpanID()`

- [x] Task 3: Span 记录器 (AC: #3)
  - [x] 3.1 在 `debug/trace.go` 中实现 `SpanRecorder` 结构体：
    ```go
    type SpanRecorder struct {
        mu    sync.Mutex
        spans map[types.PID]*Span
    }
    ```
  - [x] 3.2 实现 `NewSpanRecorder() *SpanRecorder`
  - [x] 3.3 实现 `StartSpan(pid types.PID, traceID types.TraceID, spanID types.SpanID, parentSpanID types.SpanID, name string) *Span`
  - [x] 3.4 实现 `RecordSyscall(pid types.PID)` — 递增 Span 的 SyscallCount
  - [x] 3.5 实现 `RecordTokens(pid types.PID, tokens int)` — 累加 Span 的 TokensUsed
  - [x] 3.6 实现 `EndSpan(pid types.PID, status SpanStatus)` — 记录结束时间、计算 Duration
  - [x] 3.7 实现 `GetSpan(pid types.PID) *Span` — 返回当前 Span 的只读副本
  - [x] 3.8 实现 `GetTraceSpans(traceID types.TraceID) []*Span` — 返回同一 Trace 下的所有 Span

- [x] Task 4: Kernel 集成 SpanRecorder (AC: #1, #3)
  - [x] 4.1 在 `kernel/kernel.go` 的 `KernelImpl` 中新增 `spanRecorder *debug.SpanRecorder` 字段
  - [x] 4.2 在 `NewKernel` 中初始化 `spanRecorder`
  - [x] 4.3 修改 `Spawn` 方法：
    - 如果 `opts.TraceID` 非空，调用 `spanRecorder.StartSpan(pid, traceID, spanID, parentSpanID, intent)`
    - 将 TraceID/SpanID 设置到 Process 上
  - [x] 4.4 修改 `emitEvent`：
    - 如果 `proc.TraceID` 非空且 `spanRecorder != nil`，调用 `spanRecorder.RecordSyscall(proc.PID)`
  - [x] 4.5 修改 `reasonStep`（token 统计处）：
    - 如果 `proc.TraceID` 非空，调用 `spanRecorder.RecordTokens(proc.PID, tokensUsed)`
  - [x] 4.6 修改 `reapProcess`（`kernel/reap.go`）：
    - 如果 `proc.TraceID` 非空，调用 `spanRecorder.EndSpan(proc.PID, status)`（根据 ExitStatus 判断 OK/ERROR/TIMEOUT）

- [x] Task 5: Compose 编排注入 TraceID (AC: #1)
  - [x] 5.1 在 `compose/types.go` 的 `ComposeSpawnOpts` 中新增 `TraceID types.TraceID` 和 `ParentSpanID types.SpanID`
  - [x] 5.2 修改 `compose/engine.go` 的 `Execute` 方法：在编排开始时生成 TraceID（`debug.GenerateTraceID()`）
  - [x] 5.3 修改 `compose/engine.go` 的 `executeNode`：将 TraceID 和当前节点的 SpanID 传入 `ComposeSpawnOpts`
  - [x] 5.4 修改 `compose/types.go` 的 `KernelSpawner.Spawn` 接口：确保 `ComposeSpawnOpts` 中的 TraceID/ParentSpanID 被传递到底层 Spawn

- [x] Task 6: IPC 协议扩展 (AC: #1, #2)
  - [x] 6.1 在 `ipc/protocol.go` 的 `SpawnRequest` 中新增 `TraceID` 和 `ParentSpanID` 字段
  - [x] 6.2 在 `ipc/server.go` 的 `handleSpawn` 中传递 TraceID/ParentSpanID 到 `kernel.SpawnOpts`
  - [x] 6.3 在 `kernel/ipc.go` 的 `Message` 结构体中新增 `TraceID types.TraceID` 和 `SpanID types.SpanID`
  - [x] 6.4 修改 `kernel/ipc.go` 的 `Send` 方法：将发送者的 TraceID 和 SpanID 写入 Message
  - [x] 6.5 修改 `kernel/ipc.go` 的 `Recv` 方法：如果接收方没有 TraceID 但消息中有，继承 TraceID 并创建新 Span
  - [x] 6.6 在 `ipc/protocol.go` 的 `SyscallEventWire` 中新增 `TraceID` 和 `SpanID` 字段（用于 strace 输出）

- [x] Task 7: Span 持久化 (AC: #3)
  - [x] 7.1 在 `debug/trace.go` 中实现 `SpanWriter` — 将完成的 Span 写入 JSONL 文件
  - [x] 7.2 Span 存储路径：`$PROJECT/.rnix/traces/<trace-id>/spans.jsonl`
  - [x] 7.3 在 `SpanRecorder.EndSpan` 中触发 SpanWriter 持久化
  - [x] 7.4 实现 `SpanReader` — 从 JSONL 文件读取 Span 数据（为后续 Story 15-2 准备）

- [x] Task 8: 测试 (AC: #1-3)
  - [x] 8.1 `debug/trace_test.go`：TraceID/SpanID 生成测试
    - GenerateTraceID 返回 32 字符 hex 字符串
    - GenerateSpanID 返回 32 字符 hex 字符串
    - 两次生成结果不同
  - [x] 8.2 `debug/trace_test.go`：SpanRecorder 测试
    - StartSpan 创建 Span 并记录开始时间
    - RecordSyscall 递增计数
    - RecordTokens 累加 token
    - EndSpan 设置结束时间和状态
    - GetTraceSpans 返回同一 Trace 下所有 Span
    - 并发安全测试（多 goroutine 同时操作）
  - [x] 8.3 `kernel/kernel_test.go`：Spawn 带 TraceID 测试
    - Spawn 传入 TraceID → Process 继承 TraceID 并生成 SpanID
    - Spawn 不传 TraceID → Process 无 TraceID（向后兼容）
  - [x] 8.4 `compose/engine_test.go`：Compose TraceID 传播测试
    - Execute 自动生成 TraceID
    - 所有 Spawned 进程共享同一 TraceID
    - 父子 Span 关系正确
  - [x] 8.5 `kernel/ipc_test.go`：IPC TraceID 传播测试
    - Send 将 TraceID/SpanID 写入 Message
    - Recv 方有 TraceID 时保持不变
    - Recv 方无 TraceID 时从消息继承
  - [x] 8.6 `debug/trace_test.go`：Span 持久化测试
    - SpanWriter 写入 JSONL 格式正确
    - SpanReader 读取并还原 Span 数据
  - [x] 8.7 性能测试：验证 NFR33
    - IPC Send/Recv 延迟增加 < 10ms（benchmark test）

## Dev Notes

### 架构决策

本 story 是 Epic 15（分布式追踪与上下文分析）的第一层，建立 Trace/Span 的基础设施。核心设计原则：

1. **透明传播** — TraceID 通过 Compose 编排自动注入，通过 IPC 自动携带，开发者无需手动管理（Architecture Decision 11）
2. **零开销旁路** — 非 Compose 场景下不生成 TraceID，所有 trace 逻辑通过 `if proc.TraceID != ""` 守卫，零开销跳过
3. **复用 JSONL 持久化模式** — Epic 14 验证的 JSONL + metadata 模式（来自 14-1 回顾关键洞察 #2）
4. **本地操作 vs daemon 操作** — Span 记录是 daemon 端操作（SpanRecorder 在 Kernel 中），Span 查询可以是本地文件操作（SpanReader 读 JSONL）

### 关键设计：TraceID 生成与传播路径

```
compose up
  │
  ├─ Engine.Execute: TraceID = GenerateTraceID()
  │
  ├─ executeNode("agent-a"):
  │    SpanID_A = GenerateSpanID()
  │    ComposeSpawnOpts{TraceID, ParentSpanID: ""}
  │    → ipcKernelSpawner.Spawn
  │      → SpawnRequest{TraceID, ParentSpanID: ""}
  │        → handleSpawn → kernel.Spawn(opts{TraceID, ParentSpanID: ""})
  │          → Process{TraceID, SpanID: SpanID_A, ParentSpanID: ""}
  │          → SpanRecorder.StartSpan(...)
  │
  ├─ executeNode("agent-b", depends_on: ["agent-a"]):
  │    SpanID_B = GenerateSpanID()
  │    ComposeSpawnOpts{TraceID, ParentSpanID: SpanID_A}
  │    → ... → Process{TraceID, SpanID: SpanID_B, ParentSpanID: SpanID_A}
  │
  └─ IPC: agent-a Send → Message{TraceID, SpanID: SpanID_A}
           agent-b Recv ← 继承 TraceID（如果没有的话）
```

### 关键设计：Span 记录时机

| 事件 | 触发位置 | 记录内容 |
|------|---------|---------|
| Span 开始 | `Spawn` 成功后 | TraceID, SpanID, ParentSpanID, intent, start_time |
| Syscall 计数 | `emitEvent` 每次调用 | SyscallCount++ |
| Token 消耗 | `reasonStep` 收到 LLM 响应后 | TokensUsed += response.tokens |
| Span 结束 | `reapProcess` 进程回收时 | end_time, duration, status(OK/ERROR/TIMEOUT) |

### 关键设计：IPC TraceID 传播

修改 `kernel/ipc.go` 的 `Message` 结构体，新增 `TraceID` 和 `SpanID` 字段。Send 时自动从发送者 Process 复制，Recv 时接收者可以读取。

**重要**：Recv 方如果已有 TraceID（从 Compose 继承），保持不变。只有在 Recv 方没有 TraceID 的情况下（非 Compose 场景，如手动 Send/Recv），才从消息中继承 TraceID 并创建新 Span。

### 关键设计：Span 持久化

复用 Epic 14 的 JSONL 模式：
- 目录：`$PROJECT/.rnix/traces/<trace-id>/`
- 文件：`spans.jsonl`（每个完成的 Span 一行 JSON）
- 写入时机：`EndSpan` 时追加写入
- 读取方式：`SpanReader` 全量加载并按 Trace 树构建（为 Story 15-2 的 trace view 准备）

### 关键复用点

1. **emitEvent 钩子注入模式** — 复用 `kernel/kernel.go:338-384` 的 emitEvent，在现有 recording hook 之后添加 span recording hook
2. **JSONL 持久化** — 复用 `debug/recorder.go:66-77` 的 JSONL 写入模式（`json.Encoder` + 追加写入）
3. **RecordManager 模式** — SpanRecorder 参照 `debug/record_manager.go` 的 per-PID 状态管理模式
4. **SyscallEvent 数据模型** — 复用 `internal/types/types.go:139-147` 的事件结构
5. **Compose Engine DAG** — 复用 `compose/engine.go` 的拓扑排序执行流，在 Execute 入口注入 TraceID
6. **IPC Message** — 扩展 `kernel/ipc.go:16-23` 的 Message 结构体
7. **NewProcess** — 扩展 `kernel/process.go` 的进程创建逻辑
8. **ipcKernelSpawner** — 扩展 `cmd/rnix/compose.go:83-137` 的 IPC Spawn 代理

### 不要做的事情

- **不要**实现 `rnix trace` 命令（Story 15-2 的范围）
- **不要**实现 trace blame 分析（Story 15-3 的范围）
- **不要**实现 context profiling（Story 15-4 的范围）
- **不要**修改 RecordEvent 数据模型 — Span 记录独立于录制系统
- **不要**使用数据库（SQLite 等）存储 Span — 遵循"一切皆文件"原则
- **不要**引入 OpenTelemetry 或其他第三方追踪库 — Rnix 自实现轻量级追踪
- **不要**为非 Compose 的单进程场景自动生成 TraceID — 只有 Compose 编排和 IPC 传播会产生 TraceID
- **不要**修改 Bubble Tea TUI 框架 — 保持文本输出模式
- **不要**修改 Context Manager 接口
- **不要**在 SyscallEvent 中嵌入 Span 数据 — Span 是独立的聚合数据结构，不是每个事件的属性

### IPC 协议变更

**SpawnRequest 新增字段：**

| 字段 | 类型 | 说明 |
|------|------|------|
| trace_id | string | Trace ID（可选，Compose 场景传入） |
| parent_span_id | string | 父 Span ID（可选） |

**SyscallEventWire 新增字段：**

| 字段 | 类型 | 说明 |
|------|------|------|
| trace_id | string | 当前进程的 Trace ID（可选） |
| span_id | string | 当前进程的 Span ID（可选） |

**Message 结构体新增字段：**

| 字段 | 类型 | 说明 |
|------|------|------|
| TraceID | types.TraceID | 发送者的 TraceID |
| SpanID | types.SpanID | 发送者的 SpanID |

### 组合矩阵

| 本功能 | 交互对象 | 交互方式 | 需验证 |
|--------|----------|----------|--------|
| TraceID 传播 | Compose Engine | 集成：Compose 启动时生成 TraceID 注入所有 Spawn | 是 |
| TraceID 传播 | IPC Send/Recv | 集成：TraceID 作为 Message 元数据自动携带 | 是 |
| SpanRecorder | emitEvent | 集成：每次 emitEvent 递增 SyscallCount | 是 |
| SpanRecorder | reasonStep | 集成：LLM 响应后记录 TokensUsed | 是 |
| SpanRecorder | reapProcess | 集成：进程回收时结束 Span | 是 |
| TraceID 传播 | 非 Compose 单进程 | 独立：无 TraceID 时跳过所有 trace 逻辑 | 是 |
| Span 持久化 | Record 系统 | 独立：Span 和 Recording 是两个独立的持久化路径 | 否 |
| Span 持久化 | strace | 共存：SyscallEventWire 可选携带 TraceID/SpanID | 是 |
| SpawnOpts.TraceID | fork-continue | 共存：fork-continue 的 handleForkContinue 不传 TraceID（非 Compose 场景） | 否 |
| TraceID | gdb attach/step | 独立：gdb 操作不影响 TraceID | 否 |
| TraceID | Kill/Signal | 独立：进程被 Kill 时 Span 以 ERROR 状态结束 | 是 |

### Project Structure Notes

新建文件：
- `debug/trace.go` — TraceID/SpanID 生成、Span/SpanEvent/SpanStatus 类型、SpanRecorder、SpanWriter、SpanReader
- `debug/trace_test.go` — Span 相关全部测试

修改文件：
- `internal/types/types.go` — 新增 `TraceID` 和 `SpanID` 类型定义
- `kernel/process.go` — Process 新增 TraceID/SpanID/ParentSpanID 字段
- `kernel/kernel.go` — SpawnOpts 新增 TraceID/ParentSpanID；KernelImpl 新增 spanRecorder；修改 Spawn/emitEvent/NewKernel
- `kernel/reap.go` — reapProcess 中结束 Span
- `compose/types.go` — ComposeSpawnOpts 新增 TraceID/ParentSpanID；KernelSpawner 接口可能需要调整
- `compose/engine.go` — Execute 中生成 TraceID；executeNode 传递 TraceID/ParentSpanID
- `kernel/ipc.go` — Message 新增 TraceID/SpanID；Send/Recv 传播逻辑
- `ipc/protocol.go` — SpawnRequest 新增 TraceID/ParentSpanID；SyscallEventWire 新增 TraceID/SpanID
- `ipc/server.go` — handleSpawn 传递 TraceID/ParentSpanID
- `cmd/rnix/compose.go` — ipcKernelSpawner.Spawn 传递 TraceID/ParentSpanID

### References

- [Source: kernel/process.go:32-88] — Process 结构体（新增 TraceID/SpanID 字段的位置）
- [Source: kernel/kernel.go:36-43] — SpawnOpts 结构体（新增 TraceID/ParentSpanID 的位置）
- [Source: kernel/kernel.go:148-336] — Spawn 方法完整流程（注入 SpanRecorder.StartSpan 的位置）
- [Source: kernel/kernel.go:338-384] — emitEvent（注入 SpanRecorder.RecordSyscall 的位置）
- [Source: kernel/kernel.go:456-914] — reasonStep 循环（注入 SpanRecorder.RecordTokens 的位置）
- [Source: kernel/reap.go] — reapProcess（注入 SpanRecorder.EndSpan 的位置）
- [Source: compose/engine.go:37-104] — Engine.Execute（注入 TraceID 生成的位置）
- [Source: compose/engine.go:108-149] — executeNode（传递 TraceID/ParentSpanID 的位置）
- [Source: compose/types.go] — ComposeSpawnOpts、KernelSpawner 接口
- [Source: kernel/ipc.go:16-23] — Message 结构体（新增 TraceID/SpanID 的位置）
- [Source: kernel/ipc.go:106-151] — Send 方法（写入 TraceID/SpanID 的位置）
- [Source: kernel/ipc.go:154-177] — Recv 方法（读取 TraceID 的位置）
- [Source: ipc/protocol.go:57-65] — SpawnRequest（新增 TraceID/ParentSpanID 的位置）
- [Source: ipc/protocol.go:284-291] — SyscallEventWire（新增 TraceID/SpanID 的位置）
- [Source: ipc/server.go:321-346] — handleSpawn（传递 TraceID 到 SpawnOpts 的位置）
- [Source: cmd/rnix/compose.go:83-137] — ipcKernelSpawner.Spawn（传递 TraceID 到 SpawnRequest 的位置）
- [Source: debug/recorder.go:66-77] — Recorder.WriteEvent JSONL 写入模式（SpanWriter 参考）
- [Source: debug/record_manager.go:14-17] — RecordManager 模式（SpanRecorder 参考）
- [Source: internal/types/types.go:139-147] — SyscallEvent 定义
- [Source: _bmad-output/planning-artifacts/prd/non-functional-requirements.md:63] — NFR33: Trace/Span 传播不增加 IPC 延迟超过 10ms
- [Source: _bmad-output/planning-artifacts/prd/functional-requirements.md:147-148] — FR80/FR81: TraceID 生成与 Span 记录

### 技术栈

- Go 1.26 — 标准库满足所有需求
- `crypto/rand` — TraceID/SpanID 生成
- `encoding/hex` — ID hex 编码
- `encoding/json` — Span JSONL 序列化
- `sync` — SpanRecorder 并发保护
- `os` — Span 文件 I/O
- `time` — Span 时间记录
- 无新增外部依赖

### 前置 story 学习总结

**来自 Epic 14 回顾：**
1. JSONL + metadata.json 是事件流持久化的优秀模式 — Span 记录直接复用（关键洞察 #2）
2. "不要做"清单比"要做"清单更重要 — 本 story 保持 10 项负面约束
3. "本地操作 vs daemon 操作"在设计阶段明确区分（行动项 #3）— Span 记录是 daemon 操作，Span 查询是本地操作
4. 并发代码需要双重审查（行动项 #1）— SpanRecorder 涉及 mutex，需验证线程安全
5. emitEvent 钩子注入模式稳定 — Recording hook（14-1）已验证此模式，Span hook 可在同一位置添加

**来自 14-4（Fork-Continue 分支探索）：**
1. IPC 协议扩展遵循已有模式 — 新增字段向后兼容（可选字段 + omitempty）
2. handleForkContinue 绕过标准 Spawn 的教训 — 本 story 的 TraceID 传播应通过标准 SpawnOpts 传递，不引入旁路
3. 组合矩阵列出所有交互点并验证

**来自 Git 分析：**
- 最近 commit 模式：`feat: story X-Y done` — 保持一致的提交消息风格
- debug 包文件命名：`record.go`、`recorder.go`、`replay.go`、`snapshot_diff.go`、`fork.go` → 新文件 `trace.go`
- 修改影响面模式：核心 story 通常涉及 ~10 个文件的修改

### 性能考量

- **NFR33 约束**：Trace/Span 传播不增加 IPC 延迟超过 10ms
- TraceID/SpanID 写入 Message 结构体只是内存字段拷贝，开销可忽略（< 1μs）
- SpanRecorder.RecordSyscall 是 mutex lock + 整数递增，开销极小（< 1μs）
- Span 持久化写入可以异步化（goroutine + channel），避免阻塞 Span 结束流程
- 无 TraceID 时所有 trace 逻辑被 `if proc.TraceID != ""` 守卫跳过，零开销

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6 (claude-opus-4-6)

### Debug Log References

- debug/trace_test.go: 19 个测试全部通过（GenerateTraceID/SpanID + SpanRecorder + SpanWriter/Reader）
- kernel 包: 全部测试通过（含 Spawn TraceID 传播、emitEvent span recording）
- compose 包: 全部测试通过（含 Execute TraceID 生成）
- ipc 包: 全部测试通过（含 SpawnRequest TraceID passthrough）
- 全项目 18 包测试通过（-race 检测），2 个预存 TTY 测试失败不受影响

### Completion Notes List

- TraceID/SpanID 使用 crypto/rand 生成 16 字节 hex 字符串（32 字符）
- SpanRecorder 使用 sync.Mutex 保护并发访问，通过 -race 检测
- Span 持久化采用 JSONL 格式，时间字段使用 _ms 后缀毫秒值
- kernel 已导入 debug 包（复用 RecordManager 依赖），无新增循环依赖
- Compose Engine.Execute 在编排开始时自动生成 TraceID，所有节点共享
- IPC Message 新增 TraceID/SpanID 字段，Send 自动复制，Recv 条件继承
- SyscallEvent 和 SyscallEventWire 新增 TraceID/SpanID 用于 strace 输出
- reapProcess 中根据 ExitStatus 判断 Span 结束状态（OK/ERROR/TIMEOUT）
- SpanWriter 集成到 cmd/rnix/main.go，使用 .rnix/traces/ 目录
- .gitignore 已添加 .rnix/traces/

### File List

新建文件:
- `debug/trace.go` — Span/SpanEvent/SpanStatus 类型、GenerateTraceID/SpanID、SpanRecorder、SpanWriter、SpanReader
- `debug/trace_test.go` — Span 相关全部测试（19 个）

修改文件:
- `internal/types/types.go` — 新增 TraceID、SpanID 类型；SyscallEvent 新增 TraceID/SpanID 字段
- `kernel/process.go` — Process 新增 TraceID/SpanID/ParentSpanID 字段
- `kernel/kernel.go` — SpawnOpts 新增 TraceID/ParentSpanID；KernelImpl 新增 spanRecorder；Spawn/emitEvent/reasonStep 集成；SetSpanWriter 方法
- `kernel/reap.go` — reapProcess 中 EndSpan 逻辑
- `kernel/ipc.go` — Message 新增 TraceID/SpanID；Send/Recv 传播逻辑
- `compose/types.go` — ComposeSpawnOpts 新增 TraceID/ParentSpanID
- `compose/engine.go` — Execute 生成 TraceID；executeNode 传递 TraceID
- `ipc/protocol.go` — SpawnRequest 新增 TraceID/ParentSpanID；SyscallEventWire 新增 TraceID/SpanID；SyscallEventToWire 更新
- `ipc/server.go` — handleSpawn 传递 TraceID/ParentSpanID
- `cmd/rnix/compose.go` — ipcKernelSpawner.Spawn 传递 TraceID/ParentSpanID
- `cmd/rnix/main.go` — SpanWriter 创建和配置；wireToSyscallEvent 更新
- `.gitignore` — 新增 .rnix/traces/
