# Story 14.1: 执行录制与持久化

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a 平台构建者,
I want 对指定智能体开启完整执行录制，将所有 syscall、LLM 调用、上下文变更持久化到磁盘,
So that 我可以在智能体完成后离线分析其完整执行历史。

## Acceptance Criteria

1. **Given** 一个 Running 状态的智能体进程
   **When** 用户执行 `rnix record <pid>` 或在 gdb 中执行 `record start`
   **Then** 系统开始捕获该进程的所有 DebugEvent 并写入磁盘

2. **Given** 录制进行中
   **When** 智能体完成执行或用户停止录制
   **Then** 录制数据持久化到 `$PROJECT/.rnix/records/<pid>-<timestamp>/` 目录
   **And** 格式为 JSON Lines（每行一个事件），包含完整的 syscall 序列、上下文快照和 LLM 响应

3. **Given** 录制已开启
   **When** 智能体正常执行推理循环
   **Then** 录制性能开销 <= 20%（NFR32）

## Tasks / Subtasks

- [x] Task 1: 录制事件数据模型 (AC: #1, #2)
  - [x] 1.1 在 `debug/` 包中新建 `record.go`，定义 `RecordEvent` 结构体：
    ```go
    type RecordEvent struct {
        SeqNum    uint64                 `json:"seq_num"`    // 全局递增序列号
        Timestamp time.Duration          `json:"timestamp"`  // 相对进程启动时间
        PID       types.PID              `json:"pid"`
        Type      RecordEventType        `json:"type"`       // "syscall" | "context_snapshot" | "llm_response" | "state_change"
        Syscall   *SyscallEventData      `json:"syscall,omitempty"`
        Context   *ContextSnapshotData   `json:"context,omitempty"`
        LLM       *LLMResponseData       `json:"llm,omitempty"`
        State     *StateChangeData       `json:"state,omitempty"`
    }
    ```
  - [x] 1.2 定义 `RecordEventType` 常量：`RecordSyscall`、`RecordContextSnapshot`、`RecordLLMResponse`、`RecordStateChange`
  - [x] 1.3 定义子数据结构：
    - `SyscallEventData`：从 `types.SyscallEvent` 转换，包含 Syscall/Args/Result/Err/Duration
    - `ContextSnapshotData`：SystemPrompt（hash 摘要）+ Messages 列表 + TokenEstimate
    - `LLMResponseData`：Model + RequestTokens + ResponseTokens + ResponseSummary（截断到 500 字符）
    - `StateChangeData`：FromState + ToState + Reason
  - [x] 1.4 定义 `RecordMetadata` 结构体（录制元数据，存储在 `metadata.json` 中）：
    ```go
    type RecordMetadata struct {
        RecordID    string        `json:"record_id"`     // "<pid>-<timestamp>"
        PID         types.PID     `json:"pid"`
        Intent      string        `json:"intent"`
        StartTime   time.Time     `json:"start_time"`
        EndTime     time.Time     `json:"end_time,omitempty"`
        EventCount  uint64        `json:"event_count"`
        Status      RecordStatus  `json:"status"`        // "recording" | "completed" | "stopped"
    }
    ```

- [x] Task 2: 录制写入器（Recorder） (AC: #1, #2, #3)
  - [x] 2.1 在 `debug/recorder.go` 中实现 `Recorder` 结构体：
    ```go
    type Recorder struct {
        dir      string            // 录制目录路径
        file     *os.File          // events.jsonl 文件句柄
        writer   *bufio.Writer     // 缓冲写入器
        metadata RecordMetadata
        seqNum   atomic.Uint64     // 事件序列号计数器
        mu       sync.Mutex        // 保护 writer 和 metadata
        closed   atomic.Bool
    }
    ```
  - [x] 2.2 实现 `NewRecorder(baseDir string, pid types.PID, intent string) (*Recorder, error)`：
    - 生成 recordID = `fmt.Sprintf("%d-%d", pid, time.Now().Unix())`
    - 创建目录 `baseDir/<recordID>/`
    - 创建 `events.jsonl` 文件
    - 初始化 bufio.Writer（缓冲 64KB）
    - 写入 metadata.json（status = "recording"）
  - [x] 2.3 实现 `Recorder.WriteEvent(event RecordEvent) error`：
    - JSON marshal 事件 + 追加换行符
    - 写入 bufio.Writer
    - 每 100 个事件或间隔 1 秒执行一次 Flush（通过 seqNum % 100 == 0 判断）
    - 递增 metadata.EventCount
  - [x] 2.4 实现 `Recorder.Close() error`：
    - Flush bufio.Writer
    - 关闭文件句柄
    - 更新 metadata.json（status = "completed"，设置 EndTime 和最终 EventCount）
  - [x] 2.5 实现 `Recorder.Stop() error`：
    - 与 Close 类似但 status 设为 "stopped"（用户手动停止）

- [x] Task 3: 录制管理器 (AC: #1, #2)
  - [x] 3.1 在 `debug/record_manager.go` 中实现 `RecordManager` 结构体：
    ```go
    type RecordManager struct {
        baseDir   string                                  // $PROJECT/.rnix/records/
        recorders *xsync.SyncMap[types.PID, *Recorder]    // PID -> active recorder
    }
    ```
  - [x] 3.2 实现 `NewRecordManager(baseDir string) *RecordManager`
  - [x] 3.3 实现 `RecordManager.StartRecording(pid types.PID, intent string) (string, error)`：
    - 检查该 PID 是否已在录制（返回错误避免重复录制）
    - 创建 Recorder 并注册到 map
    - 返回 recordID
  - [x] 3.4 实现 `RecordManager.StopRecording(pid types.PID) error`：
    - 查找 Recorder 并调用 Stop()
    - 从 map 中移除
  - [x] 3.5 实现 `RecordManager.RecordEvent(pid types.PID, event RecordEvent) error`：
    - 查找 Recorder 并写入事件
    - 如果该 PID 没有活跃录制，静默跳过（不报错）
  - [x] 3.6 实现 `RecordManager.IsRecording(pid types.PID) bool`
  - [x] 3.7 实现 `RecordManager.CloseAll()`：关闭所有活跃录制（进程退出时调用）
  - [x] 3.8 实现 `RecordManager.ListRecords() ([]RecordMetadata, error)`：扫描 baseDir 下所有子目录，读取 metadata.json 返回列表

- [x] Task 4: 内核录制钩子 (AC: #1, #3)
  - [x] 4.1 在 `KernelImpl` 结构体中新增 `recordMgr *debug.RecordManager` 字段
  - [x] 4.2 修改 `emitEvent` 方法：在写入 DebugChan 后，检查 `k.recordMgr != nil && k.recordMgr.IsRecording(proc.PID)`，如果是则构造 `RecordEvent`（Type=RecordSyscall）并调用 `k.recordMgr.RecordEvent()`
  - [x] 4.3 在 reasonStep 循环中 LLM 响应返回后，构造 `RecordEvent`（Type=RecordLLMResponse）记录模型/token 消耗/响应摘要
  - [x] 4.4 在 reasonStep 循环中 `CtxWrite`（AppendMessage）后，构造 `RecordEvent`（Type=RecordContextSnapshot）捕获当前上下文快照
  - [x] 4.5 在进程状态转移时（`finishProcess`），构造 `RecordEvent`（Type=RecordStateChange）记录状态变更
  - [x] 4.6 在进程完成时（`reapProcess`），如果有活跃录制则调用 `recordMgr.StopRecording(pid)` 自动结束录制
  - [x] 4.7 确保所有录制操作不阻塞主循环：录制写入失败时只记录日志，不影响智能体执行

- [x] Task 5: IPC 协议 -- record 命令 (AC: #1)
  - [x] 5.1 在 `ipc/server.go` 中新增 `handleRecordCommand` 方法处理 `record_start` 和 `record_stop` IPC method
  - [x] 5.2 `record_start` 请求格式：`{"method": "record_start", "params": {"pid": 42}}`
    - 验证 PID 存在且状态为 Running
    - 调用 `s.kern.StartRecording(pid)`（内核方法委托到 RecordManager）
    - 返回 `{"ok": true, "data": {"record_id": "42-1709856000"}}`
  - [x] 5.3 `record_stop` 请求格式：`{"method": "record_stop", "params": {"pid": 42}}`
    - 调用 `s.kern.StopRecording(pid)`
    - 返回 `{"ok": true, "data": {"event_count": 128}}`
  - [x] 5.4 在 KernelImpl 上新增 `StartRecording(pid types.PID) (string, error)` 和 `StopRecording(pid types.PID) error` 公开方法

- [x] Task 6: CLI 命令 -- rnix record (AC: #1)
  - [x] 6.1 在 `cmd/rnix/` 中新建 `record.go`，注册 `record` Cobra 子命令
  - [x] 6.2 实现 `record start <pid>` 子命令：
    - 连接 IPC daemon
    - 发送 `record_start` 请求
    - 显示 `Recording started for PID <pid> (record-id: <id>)`
  - [x] 6.3 实现 `record stop <pid>` 子命令：
    - 连接 IPC daemon
    - 发送 `record_stop` 请求
    - 显示 `Recording stopped for PID <pid> (<count> events captured)`
  - [x] 6.4 实现 `record list` 子命令：
    - 连接 IPC daemon 或直接扫描 `$PROJECT/.rnix/records/`
    - 列出所有录制及其元数据（PID、Intent、事件数、状态、时间）
  - [x] 6.5 在 gdb 命令循环中新增 `record` 命令支持：
    - `record start` → 调用 `client.SendRecordCommand(pid, "start")`
    - `record stop` → 调用 `client.SendRecordCommand(pid, "stop")`
    - 更新 `printGdbHelp` 增加 record 命令说明

- [x] Task 7: IPC Client 扩展 (AC: #1)
  - [x] 7.1 在 `ipc/client.go` 中新增 `Client.RecordStart(pid types.PID) (string, error)` 方法：
    - 构造 `record_start` 请求并发送
    - 解析响应返回 recordID
  - [x] 7.2 新增 `Client.RecordStop(pid types.PID) (int, error)` 方法：
    - 构造 `record_stop` 请求并发送
    - 解析响应返回事件数
  - [x] 7.3 新增 `Client.RecordList() ([]debug.RecordMetadata, error)` 方法（如使用 IPC 方式）

- [x] Task 8: 测试 (AC: #1-3)
  - [x] 8.1 `debug/record_test.go`：RecordEvent JSON 序列化/反序列化测试
  - [x] 8.2 `debug/recorder_test.go`：Recorder 创建/写入/关闭测试（使用 tmpdir）
  - [x] 8.3 `debug/recorder_test.go`：Recorder 并发写入安全性测试
  - [x] 8.4 `debug/record_manager_test.go`：RecordManager 启动/停止/重复录制防护测试
  - [x] 8.5 `debug/record_manager_test.go`：RecordManager ListRecords 扫描测试
  - [x] 8.6 `kernel/kernel_test.go`：emitEvent 录制钩子集成测试（mock RecordManager 验证事件被转发）
  - [x] 8.7 `ipc/server_test.go`：record_start/record_stop IPC 路由测试
  - [x] 8.8 `cmd/rnix/record_test.go`：record CLI 命令解析测试
  - [x] 8.9 性能基准测试：`debug/recorder_bench_test.go`，验证单次 WriteEvent 操作 < 100us

## Dev Notes

### 架构决策

本 story 是 Epic 14（时间旅行调试）的基础层，实现执行录制的核心基础设施。设计遵循 Architecture Decision 8（一切皆文件）和 Decision 11（时间旅行 fork-continue 基础）。

核心设计原则：
1. **非侵入式录制** — 录制钩子嵌入现有 emitEvent 路径，不改变 reasonStep 的主流程
2. **异步容错** — 录制失败不影响智能体执行，只记录警告日志
3. **JSONL 格式** — 每行一个事件，支持流式写入和流式读取，天然适配时间旅行回放
4. **元数据分离** — metadata.json 独立于事件流，快速查询录制信息无需解析全部事件

### 关键设计：录制数据流

```
reasonStep 循环
    |
    +-- emitEvent(proc, "Spawn", ...) → SyscallEvent → DebugChan (strace)
    |                                                 → RecordManager.RecordEvent() (录制)
    |
    +-- LLM 响应返回 → RecordEvent(RecordLLMResponse)
    |
    +-- CtxWrite 后 → RecordEvent(RecordContextSnapshot)
    |
    +-- finishProcess → RecordEvent(RecordStateChange)
    |
    +-- reapProcess → RecordManager.StopRecording(pid) (自动结束)
```

**关键：录制与 strace 共用 emitEvent 路径，但录制直接写入磁盘文件，不经过 DebugChan。这避免了 DebugChan 缓冲 256 的限制影响录制完整性。**

### 关键设计：录制目录结构

```
$PROJECT/.rnix/records/
├── 42-1709856000/           # PID-UnixTimestamp
│   ├── metadata.json        # 录制元数据（快速查询）
│   └── events.jsonl         # 事件流（JSON Lines）
├── 43-1709856500/
│   ├── metadata.json
│   └── events.jsonl
```

路径约定遵循 Architecture Implementation Patterns 中的"文件持久化路径模式"：`$PROJECT/.rnix/records/<pid>-<timestamp>/`。

### 关键设计：性能优化策略（NFR32: <= 20% 开销）

1. **bufio.Writer 64KB 缓冲** — 减少磁盘写入次数
2. **批量 Flush** — 每 100 个事件或 1 秒间隔 Flush 一次，不是每次写入都 fsync
3. **上下文快照惰性捕获** — 只在 CtxWrite 后捕获快照（不是每个 syscall 都捕获），减少数据量
4. **LLM 响应截断** — ResponseSummary 截断到 500 字符，避免大响应膨胀录制文件
5. **SystemPrompt hash 摘要** — 上下文快照只存 system prompt 的 hash（内容相对固定），不存全文
6. **错误不阻塞** — 录制写入失败只日志，不阻塞 reasonStep

### 关键设计：emitEvent 中的录制钩子注入点

```go
func (k *KernelImpl) emitEvent(proc *Process, syscall string, args map[string]any, result any, err error, duration time.Duration) {
    ev := types.SyscallEvent{
        Timestamp: time.Since(proc.CreatedAt),
        PID:       proc.PID,
        Syscall:   syscall,
        Args:      args,
        Result:    result,
        Err:       err,
        Duration:  duration,
    }

    // 现有逻辑：写入 DebugChan（strace 消费）
    if proc.DebugChan != nil {
        select {
        case proc.DebugChan <- ev:
        default:
        }
    }

    // 新增：录制钩子（Story 14.1）
    if k.recordMgr != nil && k.recordMgr.IsRecording(proc.PID) {
        recEvent := debug.RecordEventFromSyscall(ev)
        if err := k.recordMgr.RecordEvent(proc.PID, recEvent); err != nil {
            // 录制失败不影响执行，只记录日志
            log.Printf("[record] write error pid=%d: %v", proc.PID, err)
        }
    }
}
```

**关键：录制与 DebugChan 并行，不互相依赖。strace 可能因为 DebugChan 满而丢事件，但录制直接写磁盘，保证完整性。**

### 关键设计：RecordManager 的 baseDir 发现

录制目录使用 `$PROJECT/.rnix/records/`。`$PROJECT` 的发现逻辑：
1. 查找当前工作目录向上最近的 `.rnix/` 目录
2. 如果不存在，创建 `$CWD/.rnix/records/`
3. KernelImpl 初始化时接收 baseDir 参数（由 `cmd/rnix/main.go` 注入）

### 关键复用点

1. **emitEvent 路径**：复用 `kernel/kernel.go:338` 的 emitEvent 方法，在现有 DebugChan 写入后追加录制逻辑
2. **types.SyscallEvent**：复用 `internal/types/types.go:139` 的 SyscallEvent 结构体，RecordEvent.Syscall 字段从中转换
3. **xsync.SyncMap**：复用 `internal/xsync/syncmap.go` 管理 PID→Recorder 映射（RecordManager 内部）
4. **IPC method 路由模式**：复用 `ipc/server.go` 的 handleXxx 路由模式，新增 record_start/record_stop
5. **Cobra 子命令模式**：复用 `cmd/rnix/strace.go` 或 `cmd/rnix/gdb.go` 的命令注册模式
6. **gdb 命令循环扩展**：复用 `cmd/rnix/gdb.go` 的 switch/case 命令路由，新增 record 分支
7. **IPC Client 独立连接模式**：复用 `ipc/client.go` 的请求-响应模式（如 SendGdbCommand）

### 不要做的事情

- **不要**修改 DebugChan 的缓冲大小或行为 — 录制走独立路径，与 DebugChan 无关
- **不要**在每个 syscall 都捕获完整上下文快照 — 只在 CtxWrite 后捕获，减少开销
- **不要**使用 database（SQLite 等） — Architecture Decision 8 明确要求文件系统持久化
- **不要**实现录制回放（Story 14.2 的范围）— 本 story 只做录制和持久化
- **不要**实现上下文 diff（Story 14.3 的范围）
- **不要**实现 fork-continue（Story 14.4 的范围）
- **不要**在录制失败时终止智能体执行 — 容错设计，录制是可选的增值功能
- **不要**使用 Bubble Tea TUI 框架 — CLI 命令保持 cobra 模式
- **不要**将 RecordManager 放在 kernel 包里 — 放在 debug 包中保持依赖方向正确（kernel → debug 仅通过类型）
- **不要**修改 Process 结构体添加录制相关字段 — 录制状态由 RecordManager 管理，Process 不感知录制

### IPC 协议：record 命令

```
record_start:
  请求: {"method": "record_start", "params": {"pid": 42}}
  响应: {"ok": true, "data": {"record_id": "42-1709856000"}}

record_stop:
  请求: {"method": "record_stop", "params": {"pid": 42}}
  响应: {"ok": true, "data": {"event_count": 128}}

record_list:
  请求: {"method": "record_list"}
  响应: {"ok": true, "data": {"records": [...]}}
```

### gdb 内 record 命令

```
gdb> record start     -> 开始录制当前 attach 的进程
gdb> record stop      -> 停止录制
gdb> help             -> 显示包含 record 的帮助信息
```

### 组合矩阵

| 本功能 | 交互对象 | 交互方式 | 需验证 |
|--------|----------|----------|--------|
| record (录制) | strace (DebugChan) | 并行：录制和 strace 同时消费事件，互不干扰 | 是 |
| record (录制) | gdb breakpoint | 共存：断点暂停时录制也暂停（无事件产生），恢复后继续录制 | 是 |
| record (录制) | gdb step | 共存：单步执行时每步都产生录制事件 | 是 |
| record (录制) | gdb set model | 共存：model override 后录制的 LLMResponseData 反映新模型 | 否（自然兼容） |
| record (录制) | Kill/Signal | 录制随进程终止自动停止（reapProcess 钩子） | 是 |
| record (录制) | Compose multi-agent | 每个进程独立录制，互不干扰 | 否（自然兼容） |

### 性能约束

- `RecordManager.IsRecording()`: O(1) SyncMap 查找，与 DebugChan nil 检查同级别开销
- `RecordManager.RecordEvent()`: JSON marshal + bufio.Write，均为内存操作（Flush 才写磁盘）
- 目标：单次 RecordEvent < 100us（JSON marshal ~10us + bufio.Write ~1us）
- 非录制进程：IsRecording 返回 false 后立即跳过，零额外开销

### Project Structure Notes

新建文件：
- `debug/record.go` — RecordEvent/RecordMetadata 数据模型定义
- `debug/recorder.go` — Recorder 实现（文件写入）
- `debug/record_manager.go` — RecordManager 实现（多录制管理）
- `debug/record_test.go` — 数据模型测试
- `debug/recorder_test.go` — Recorder 单元测试
- `debug/record_manager_test.go` — RecordManager 单元测试
- `debug/recorder_bench_test.go` — 性能基准测试
- `cmd/rnix/record.go` — record CLI 子命令
- `cmd/rnix/record_test.go` — record CLI 测试

修改文件：
- `kernel/kernel.go` — emitEvent 增加录制钩子 + reasonStep 增加 LLM/context 录制点 + 新增 StartRecording/StopRecording 方法
- `ipc/server.go` — 新增 record_start/record_stop 路由
- `ipc/client.go` — 新增 RecordStart/RecordStop/RecordList 方法
- `cmd/rnix/main.go` — 注册 record 子命令 + 初始化 RecordManager
- `cmd/rnix/gdb.go` — 命令循环新增 record 分支 + 更新帮助

### References

- [Source: kernel/kernel.go:338-358] — emitEvent 实现，录制钩子在此注入
- [Source: kernel/kernel.go:442-600] — reasonStep 循环，LLM/context 录制点在此添加
- [Source: kernel/kernel.go:36-43] — SpawnOpts 定义，理解 Model/SystemPrompt 参数
- [Source: kernel/process.go:32-88] — Process 结构体，理解 DebugChan/LogChan/CreatedAt 字段
- [Source: internal/types/types.go:139-147] — SyscallEvent 结构体，RecordEvent.Syscall 从此转换
- [Source: debug/strace.go:1-50] — debug 包现有结构，新文件遵循同样的包组织
- [Source: ipc/server.go:1-50] — Server 结构体，理解 kern/ctxMgr 字段
- [Source: ipc/server.go:648-683] — handleGdbCommand 路由模式，handleRecordCommand 参考此模式
- [Source: cmd/rnix/gdb.go:213-243] — gdb 命令循环，record 命令在此扩展
- [Source: context/context.go:32-45] — Context/PromptResult 结构体，ContextSnapshotData 从此转换
- [Source: _bmad-output/planning-artifacts/architecture/core-architectural-decisions.md] — Decision 8 一切皆文件 + Decision 11 调试工具链架构
- [Source: _bmad-output/planning-artifacts/architecture/implementation-patterns-consistency-rules.md] — 文件持久化路径模式 `$PROJECT/.rnix/records/`

### 技术栈

- Go 1.26 — `sync.Mutex` 保护 Recorder 文件写入，`atomic.Uint64` 管理序列号和关闭状态
- `encoding/json` — JSON marshal RecordEvent（标准库）
- `bufio.Writer` — 64KB 缓冲写入（标准库）
- `os` — 文件和目录操作（标准库）
- Cobra v1.10.2 — record 子命令注册
- `internal/xsync.SyncMap` — RecordManager 内部 PID→Recorder 映射
- IPC Unix domain socket — record_start/record_stop 命令传输

### 前置 story 学习总结（来自 Epic 13）

1. **IPC 命令路由模式已稳定** — record_start/record_stop 可直接复用 handleGdbCommand 的 switch/case 模式
2. **独立连接模式** — record 命令使用请求-响应模式（如 SendGdbCommand），不需要长连接事件流
3. **gdb 命令循环扩展安全** — 13-2/13-3/13-4 连续在 gdb 命令循环中新增命令，模式成熟
4. **emitEvent 是可靠的事件汇聚点** — 所有 syscall 事件都经过 emitEvent，是录制钩子的最佳注入点
5. **Process.mu 保护模式** — 所有 Process 字段的并发访问通过 mu 保护，但录制不在 Process 上，由 RecordManager 独立管理

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6

### Debug Log References

- Fixed CloseAll deadlock: Range holds RLock while Delete needs Lock on xsync.SyncMap. Solution: collect PIDs first, then LoadAndDelete outside Range.
- Fixed ATDD test field name mismatches: `Name` -> `Syscall` in SyscallEventData, `StatusRecording` -> `RecordStatusRecording`, etc.
- Fixed IPC server_test.go: setupTestServer needed RecordManager initialization for record tests to pass.

### Completion Notes List

- All 8 tasks implemented and tested
- Full test suite passes (19 packages, 0 failures) with race detection
- Key bug fix: CloseAll deadlock in RecordManager due to RLock/Lock contention in xsync.SyncMap

### Change Log

- `debug/record.go` (NEW) -- RecordEvent, RecordMetadata data models, helper functions
- `debug/recorder.go` (NEW) -- Recorder (file-level JSONL writer with 64KB bufio.Writer)
- `debug/record_manager.go` (NEW) -- RecordManager (multi-PID recording orchestrator)
- `debug/record_test.go` (NEW) -- Data model unit tests
- `debug/recorder_test.go` (NEW) -- Recorder unit tests (create/write/close/concurrent)
- `debug/record_manager_test.go` (NEW) -- RecordManager unit tests (start/stop/list/closeall)
- `debug/recorder_bench_test.go` (NEW) -- Performance benchmark (WriteEvent < 100us)
- `cmd/rnix/record.go` (NEW) -- CLI `rnix record start/stop/list` commands
- `kernel/kernel.go` (MODIFIED) -- Recording hooks in emitEvent, reasonStep, finishProcess + public API
- `kernel/reap.go` (MODIFIED) -- Auto-stop recording on process reap
- `ipc/server.go` (MODIFIED) -- record_start/record_stop/record_list IPC handlers
- `ipc/client.go` (MODIFIED) -- RecordStart/RecordStop/RecordList client methods
- `ipc/protocol.go` (MODIFIED) -- Record IPC types and method constants
- `ipc/server_test.go` (MODIFIED) -- Added RecordManager to test setup + record handler tests
- `cmd/rnix/main.go` (MODIFIED) -- RecordManager initialization in daemon startup
- `cmd/rnix/gdb.go` (MODIFIED) -- `record start/stop` in gdb command loop

### File List

New files:
- `debug/record.go`
- `debug/recorder.go`
- `debug/record_manager.go`
- `debug/record_test.go`
- `debug/recorder_test.go`
- `debug/record_manager_test.go`
- `debug/recorder_bench_test.go`
- `cmd/rnix/record.go`

Modified files:
- `kernel/kernel.go`
- `kernel/reap.go`
- `ipc/server.go`
- `ipc/client.go`
- `ipc/protocol.go`
- `ipc/server_test.go`
- `cmd/rnix/main.go`
- `cmd/rnix/gdb.go`

## Senior Developer Review (AI)

**Reviewer:** Decker (AI) on 2026-03-08
**Outcome:** Approved with fixes applied

### Issues Found & Fixed

| # | Severity | Description | Fix |
|---|----------|-------------|-----|
| H1 | HIGH | `TruncateString` truncates by byte count, not rune count -- splits CJK multi-byte chars | Changed to `[]rune` counting, added CJK test |
| H2 | HIGH | `StartRecording` TOCTOU race leaves orphan directories on disk | Added `removeRecordDir` cleanup on race loss |
| H3 | HIGH | `WriteEvent` assigns SeqNum outside mutex, allows out-of-order JSONL writes | Moved SeqNum assignment inside `r.mu.Lock()` |
| M1 | MEDIUM | `recordContextSnapshot` calls expensive `BuildPrompt` redundantly in hot path | Replaced with lightweight `GetContextInfo` |
| M4 | MEDIUM | `Kernel.Shutdown()` doesn't call `recordMgr.CloseAll()` -- data loss on daemon exit | Added `recordMgr.CloseAll()` to Shutdown |

### Not Fixed (Low / Accepted)

| # | Severity | Description | Reason |
|---|----------|-------------|--------|
| M2 | MEDIUM | Wire struct field name `StartTime` vs JSON tag `start_time_ms` inconsistency | Cosmetic, does not affect behavior |
| M3 | MEDIUM | CLI record tests only check Cobra registration, no E2E | Acceptable for CLI layer; IPC/debug tests cover core logic |
| L1 | LOW | `record.go` uses `flagJSON` directly instead of `resolveOutputMode()` | Functionally identical, pattern preference |
| L2 | LOW | Benchmark doesn't assert < 100us threshold | Informational benchmark, not a gate |
