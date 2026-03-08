# Story 14.4: Fork-Continue 分支探索

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a 平台构建者,
I want 在回放的任意时间点创建新分支，修改上下文后重新执行（产生真实 LLM 调用）,
So that 我可以验证"如果当时做了不同决定会怎样"。

## Acceptance Criteria

1. **Given** 用户在回放界面的某个时间点
   **When** 用户执行 `fork`
   **Then** 系统从该时间点恢复上下文快照，CtxAlloc 新上下文空间并回放消息历史

2. **Given** fork 创建的新上下文
   **When** 用户修改上下文内容并执行 `continue`
   **Then** 系统 Spawn 新进程（PPID 指向原录制进程 PID），进入正常 reasonStep 循环产生真实 LLM 调用

3. **Given** fork 产生的新进程
   **When** 新进程执行完成
   **Then** 用户可以通过 `rnix ps` 和 `rnix strace` 正常查看该分支进程

## Tasks / Subtasks

- [x] Task 1: 上下文快照恢复器（SnapshotRestorer）(AC: #1)
  - [x] 1.1 在 `debug/fork.go` 中实现 `SnapshotRestorer` 结构体：
    ```go
    type SnapshotRestorer struct {
        reader *RecordReader
    }
    ```
  - [x] 1.2 实现 `NewSnapshotRestorer(reader *RecordReader) *SnapshotRestorer`
  - [x] 1.3 实现 `RestoreContext(seqNum uint64) (*ForkContext, error)` 方法：
    - 从录制事件中收集截至 seqNum 的全部上下文信息
    - 扫描所有事件（SeqNum <= seqNum），提取：
      - 初始 intent（从第一个 Spawn syscall 事件的 args.intent）
      - system prompt（从 CtxWrite SetSystemPrompt 事件）
      - 消息历史（从 CtxWrite AppendMessage 和 AppendToolResult 事件，按 SeqNum 顺序）
    - 返回 ForkContext 包含恢复所需的全部数据
  - [x] 1.4 定义 `ForkContext` 数据结构：
    ```go
    type ForkContext struct {
        OriginalPID  types.PID
        SeqNum       uint64
        Intent       string
        SystemPrompt string
        Messages     []ForkMessage
    }

    type ForkMessage struct {
        Role       string `json:"role"`       // "user", "assistant", "tool"
        Content    string `json:"content"`
        ToolCallID string `json:"tool_call_id,omitempty"`
    }
    ```

- [x] Task 2: ReplaySession 扩展 -- fork 命令 (AC: #1)
  - [x] 2.1 在 `debug/replay.go` 中给 `ReplaySession` 新增 `Fork() (*ForkContext, error)` 方法：
    - 验证 cursor >= 0（必须已导航到某个事件）
    - 使用 SnapshotRestorer 从当前 cursor 位置恢复上下文
    - 返回 ForkContext
  - [x] 2.2 新增 `ForkAt(seqNum uint64) (*ForkContext, error)` 方法：
    - 先 Goto(seqNum) 再调用 Fork()
    - 不改变 cursor 位置

- [x] Task 3: ForkContext 修改操作 (AC: #2)
  - [x] 3.1 在 `debug/fork.go` 中为 `ForkContext` 实现修改方法：
    - `SetSystemPrompt(prompt string)` — 修改 system prompt
    - `AppendMessage(role, content string)` — 追加消息
    - `RemoveLastMessages(n int)` — 移除最后 n 条消息
    - `ReplaceLastMessage(content string)` — 替换最后一条消息内容
  - [x] 3.2 实现 `ForkContext.Summary() string` — 返回可读的 fork 上下文摘要（消息数、token 估算等）

- [x] Task 4: IPC 协议扩展 -- fork_continue (AC: #2, #3)
  - [x] 4.1 在 `ipc/protocol.go` 中新增 `MethodForkContinue Method = "fork_continue"`
  - [x] 4.2 在 `ipc/server.go` 中实现 `handleForkContinue` 处理函数：
    - 接收参数：`intent`（string）、`system_prompt`（string）、`messages`（[]ForkMessage）、`original_pid`（uint64）
    - 调用 ctxMgr.CtxAlloc 分配新上下文
    - 调用 ctxMgr.SetSystemPrompt 设置 system prompt
    - 逐条调用 ctxMgr.AppendMessage / AppendToolResult 回放消息历史
    - 构造 SpawnOpts{ParentPID: original_pid}，调用 kernel.Spawn 创建新进程
    - 返回新进程 PID
  - [x] 4.3 在 `ipc/client.go` 中新增 `ForkContinue(ctx ForkContext) (types.PID, error)` 方法

- [x] Task 5: CLI 命令 -- replay fork/continue (AC: #1, #2, #3)
  - [x] 5.1 在 `cmd/rnix/replay.go` 的交互式命令循环中新增 `fork` 命令：
    - 从当前 cursor 位置执行 Fork
    - 存储 ForkContext 到局部变量 `pendingFork`
    - 显示 fork 摘要信息
    - 进入 fork 子模式（prompt 变为 `fork>`）
  - [x] 5.2 新增 fork 子模式命令：
    - `set prompt <text>` — 修改 system prompt
    - `append <role> <content>` — 追加消息
    - `remove <n>` — 移除最后 n 条消息
    - `replace <content>` — 替换最后一条消息
    - `show` — 显示当前 fork 上下文摘要
    - `continue` / `go` — 执行 fork-continue（需要 daemon 连接）
    - `cancel` — 取消 fork，返回 replay 模式
  - [x] 5.3 `continue` 命令执行：
    - 检测 daemon 是否运行（连接 IPC）
    - 调用 IPC client.ForkContinue(pendingFork)
    - 显示新进程 PID
    - 提示用户可以用 `rnix ps` 和 `rnix strace` 查看分支进程
  - [x] 5.4 在 `printReplayHelp` 中添加 fork/continue 命令说明
  - [x] 5.5 处理错误：daemon 未运行、fork 未初始化、IPC 连接失败

- [x] Task 6: 测试 (AC: #1-3)
  - [x] 6.1 `debug/fork_test.go`：SnapshotRestorer 测试
    - RestoreContext 从 Spawn 事件提取 intent
    - RestoreContext 从 CtxWrite 事件提取 system prompt
    - RestoreContext 从 AppendMessage 事件恢复消息历史
    - RestoreContext 在指定 SeqNum 处截断
    - RestoreContext 无有效事件时返回错误
  - [x] 6.2 `debug/fork_test.go`：ForkContext 修改测试
    - SetSystemPrompt 修改 prompt
    - AppendMessage 追加消息
    - RemoveLastMessages 移除消息
    - ReplaceLastMessage 替换消息
    - Summary 输出格式正确
  - [x] 6.3 `debug/replay_test.go`：ReplaySession.Fork/ForkAt 测试
    - Fork 在有效 cursor 位置返回 ForkContext
    - Fork 在 cursor=-1 时返回错误
    - ForkAt 不改变 cursor
  - [x] 6.4 `ipc/server_test.go`：handleForkContinue 测试
    - 有效参数创建新进程
    - 消息历史正确回放到新上下文
    - 新进程 PPID 指向原录制进程
  - [x] 6.5 `cmd/rnix/replay_test.go`：fork CLI 命令测试
    - fork 命令切换到 fork 子模式
    - continue 命令尝试 IPC 连接
    - cancel 命令返回 replay 模式

## Dev Notes

### 架构决策

本 story 是 Epic 14（时间旅行调试）的第四层，也是最后一层。它在 Story 14-1/14-2/14-3 的基础上增加分支探索能力。核心设计原则：

1. **两阶段操作** — fork 是纯本地操作（从录制文件恢复上下文），continue 需要 daemon（创建真实进程执行 LLM 调用）。这与 replay 的纯本地模型不同，continue 阶段必须通过 IPC 与 daemon 通信。
2. **上下文重建而非快照加载** — 不直接序列化/反序列化 Context 对象（Context 有 mutex 不可序列化），而是从录制事件中重放操作来重建上下文。
3. **最小化内核修改** — fork-continue 复用现有的 Spawn + CtxAlloc + AppendMessage 流程，不需要新增 syscall。IPC 层编排这些操作。
4. **进程谱系追踪** — fork 出的新进程通过 SpawnOpts.ParentPID 指向原录制进程的 PID，保持进程树的可追溯性。

### 关键设计：上下文重建算法

从录制事件重建上下文的核心算法：

```
扫描 events[0..seqNum]：
  1. 找到 Type=syscall, Syscall.Syscall="Spawn" → 提取 Syscall.Args["intent"] 作为 intent
  2. 找到 Type=syscall, Syscall.Syscall="CtxWrite", Args["op"]="SetSystemPrompt"
     → 记录 system prompt 设置（最后一个生效）
  3. 找到 Type=syscall, Syscall.Syscall="CtxWrite", Args["op"]="AppendMessage"
     → 追加 {role: Args["role"], content: ???}
  4. 找到 Type=syscall, Syscall.Syscall="CtxWrite", Args["op"]="AppendToolResult"
     → 追加 {role: "tool", tool_call_id: Args["tool"], content: ???}
```

**重要限制**：当前录制的 syscall 事件中 `Args` 记录的是 syscall 调用参数的 map[string]any，其中包含 `cid`、`op`、`role` 等元数据，但**不包含消息内容本身**（因为 emitEvent 只记录了操作类型而非内容）。

**解决方案**：使用 `context_snapshot` 事件（Story 14-1 的 ContextSnapshotData）作为上下文基准，结合 `llm_response` 事件（LLMResponseData.ResponseSummary）来重建消息序列。具体：
- 找到最近的 context_snapshot（在 seqNum 之前），获取 MessageCount 和 Messages 列表
- ContextSnapshotData.Messages 是 `[]string` 消息摘要列表 — 注意这些是摘要不是完整内容
- 使用 ContextSnapshotData 的结构信息重建 ForkContext

**更精确的方案**：由于 context_snapshot 只存摘要，fork 需要完整消息。因此需要增强 context_snapshot 的数据记录。在 `kernel.recordContextSnapshot` 中，需要从 ctxMgr.BuildPrompt 获取完整消息列表并存入录制事件。但这会增加录制数据量。

**推荐实现**：采用渐进方案 —
1. **Phase 1（本 Story 范围）**：fork 时通过 IPC 向 daemon 请求完整上下文内容。在 `handleForkContinue` 中接收 ForkContext 参数（由 CLI 从录制事件推断并由用户修改），daemon 端直接执行 CtxAlloc + SetSystemPrompt + AppendMessage 序列。
2. 上下文内容来源：利用 `ContextSnapshotData.Messages`（摘要列表）+ `RecordMetadata.Intent`（原始意图）作为基础信息。CLI 端在 fork 模式中向用户展示已知信息，允许用户手动补充/修改。

### 关键设计：fork 子模式交互

```
$ rnix replay 42-1709856000
[replay] Loading record 42-1709856000...
[replay] PID: 42 | Intent: "分析代码" | Events: 128 | Status: completed

replay> goto 6
[#006 +1.8s] context: msgs=8 tokens≈4200
[6/128]

replay> fork
[fork] Creating fork from event #006...
[fork] Original PID: 42
[fork] Intent: "分析代码"
[fork] Messages: 8 (estimated)
[fork] System Prompt: len:2048
[fork]
[fork] Available commands:
[fork]   set prompt <text>  - Change system prompt
[fork]   append <role> <msg> - Add a message
[fork]   remove <n>         - Remove last n messages
[fork]   replace <text>     - Replace last message
[fork]   show               - Show fork context summary
[fork]   continue / go      - Execute with LLM (requires daemon)
[fork]   cancel             - Cancel and return to replay

fork> append user 请用另一种方式优化这段代码
[fork] Message appended: [user] 请用另一种方式优化这段代码

fork> continue
[fork] Connecting to daemon...
[fork] Creating forked process...
[fork] New process spawned: PID 55 (PPID: 42)
[fork] Process is now running with real LLM calls.
[fork] Use 'rnix ps' to check status, 'rnix strace 55' to trace.
[fork] Returning to replay mode.

replay>
```

### 关键设计：IPC fork_continue 协议

请求：
```json
{
  "method": "fork_continue",
  "params": {
    "intent": "分析代码",
    "system_prompt": "You are a code analyst...",
    "messages": [
      {"role": "user", "content": "分析这段代码"},
      {"role": "assistant", "content": "我来分析..."},
      {"role": "user", "content": "请用另一种方式优化"}
    ],
    "original_pid": 42
  }
}
```

响应：
```json
{
  "ok": true,
  "data": {
    "pid": 55,
    "ppid": 42
  }
}
```

daemon 端 handleForkContinue 执行流程：
1. `ctxMgr.CtxAlloc(DefaultCtxSize)` — 分配新上下文
2. `ctxMgr.SetSystemPrompt(cid, params.SystemPrompt)` — 设置 system prompt
3. 遍历 params.Messages，对每条消息调用对应的 ctxMgr 方法
4. 构造 `SpawnOpts{ParentPID: params.OriginalPID}` 并调用 `kernel.Spawn`

**注意**：OriginalPID 可能已经不存在于 daemon 的进程表中（因为录制来自历史进程）。需要处理两种情况：
- 如果 OriginalPID 进程仍存在 → 正常设置 PPID
- 如果 OriginalPID 进程不存在 → PPID 设为 0（顶层进程），在返回中标注 `ppid_valid: false`

### 关键复用点

1. **RecordReader/RecordEvent** — 复用 `debug/record.go` 和 `debug/record_reader.go` 的录制数据模型
2. **ReplaySession 导航** — 复用 cursor 模型，fork 在当前 cursor 位置操作
3. **SnapshotFinder** — 复用 `debug/snapshot_diff.go` 的快照查找逻辑
4. **ContextSnapshotData** — 复用 `debug/record.go:43-48` 的快照数据结构
5. **Kernel.Spawn** — 复用完整的 Spawn 流程（CtxAlloc → SetSystemPrompt → AppendMessage → Open LLM → reasonStep）
6. **IPC Server 模式** — 复用 `ipc/server.go` 的 handleXxx 模式
7. **IPC Client 模式** — 复用 `ipc/client.go` 的请求发送模式
8. **replay CLI 命令循环** — 在现有 switch/case 中新增分支，fork 子模式用嵌套循环
9. **RecordMetadata.Intent** — 录制元数据中的原始意图

### 不要做的事情

- **不要**修改 Kernel.Spawn 的签名或核心逻辑 — fork-continue 通过 IPC 编排现有 Spawn 流程
- **不要**新增 syscall — fork-continue 不是内核级操作，是 IPC 层的编排
- **不要**修改 RecordEvent/Recorder 数据模型 — 不需要改变录制格式
- **不要**在 replay 中直接调用 Kernel — replay 是本地操作，通过 IPC 与 daemon 通信
- **不要**使用 Bubble Tea TUI 框架 — 保持文本输出模式
- **不要**实现分布式追踪（Epic 15 的范围）
- **不要**为 fork 历史创建持久化存储 — fork 出的进程就是普通进程，由进程表管理
- **不要**修改 Context Manager 的接口 — 复用现有 CtxAlloc/SetSystemPrompt/AppendMessage
- **不要**在 fork 子模式中实现 diff 命令 — fork 子模式只做上下文修改和 continue

### IPC 协议

新增方法：`fork_continue`

| 字段 | 类型 | 说明 |
|------|------|------|
| method | string | `"fork_continue"` |
| params.intent | string | 原始意图（用于 Spawn） |
| params.system_prompt | string | system prompt 内容 |
| params.messages | []ForkMessage | 消息历史列表 |
| params.original_pid | uint64 | 原录制进程 PID |

返回：`{"ok": true, "data": {"pid": <new_pid>, "ppid": <ppid>}}`

### 组合矩阵

| 本功能 | 交互对象 | 交互方式 | 需验证 |
|--------|----------|----------|--------|
| fork | replay 导航（next/prev/goto） | 共存：fork 基于 cursor 位置，不改变 cursor | 是 |
| fork | diff | 独立：fork 和 diff 可在同一 session 中使用 | 否 |
| continue | daemon 进程表 | 依赖：continue 需要 daemon 运行 | 是 |
| continue | Spawn 流程 | 复用：通过 IPC 触发标准 Spawn | 是 |
| continue | rnix ps | 共存：fork 出的进程在 ps 中可见 | 是 |
| continue | rnix strace | 共存：fork 出的进程可被 strace 追踪 | 是 |
| continue | gdb | 共存：fork 出的进程可被 gdb attach | 是 |
| continue | record | 共存：fork 出的新进程可以独立开启录制 | 否 |
| fork 子模式 | replay 命令 | 互斥：fork 子模式中不可使用 replay 命令 | 是 |

### Project Structure Notes

新建文件：
- `debug/fork.go` — SnapshotRestorer + ForkContext + ForkMessage（fork 核心逻辑）
- `debug/fork_test.go` — SnapshotRestorer 和 ForkContext 测试

修改文件：
- `debug/replay.go` — 新增 `Fork()` 和 `ForkAt()` 方法
- `debug/replay_test.go` — 新增 Fork/ForkAt 测试
- `ipc/protocol.go` — 新增 `MethodForkContinue`
- `ipc/server.go` — 新增 `handleForkContinue` 处理函数
- `ipc/server_test.go` — 新增 fork_continue 测试
- `ipc/client.go` — 新增 `ForkContinue` 方法
- `cmd/rnix/replay.go` — 新增 `fork` 命令分支 + fork 子模式循环 + 更新 help

### References

- [Source: debug/record.go:22-31] — RecordEvent 结构体（fork 读取的事件数据基础）
- [Source: debug/record.go:43-48] — ContextSnapshotData（上下文快照数据，fork 上下文重建参考）
- [Source: debug/record.go:14-19] — RecordEventType 常量
- [Source: debug/record_reader.go:93-96] — RecordReader.Events()（获取全量事件）
- [Source: debug/record_reader.go:98-107] — RecordReader.EventsInRange()（获取范围事件）
- [Source: debug/replay.go:6-9] — ReplaySession 结构体（扩展 Fork 方法的基础）
- [Source: debug/replay.go:49-58] — ReplaySession.Goto（理解 SeqNum 查找模式）
- [Source: debug/replay.go:73-76] — ReplaySession.Position（获取 cursor 位置）
- [Source: debug/snapshot_diff.go] — SnapshotFinder（复用快照查找逻辑）
- [Source: kernel/kernel.go:148-336] — Kernel.Spawn 完整流程（fork-continue 复用的核心流程）
- [Source: kernel/kernel.go:456-914] — reasonStep 循环（fork 出的进程执行的推理循环）
- [Source: kernel/kernel.go:1106-1139] — recordContextSnapshot（理解上下文快照录制）
- [Source: context/context.go:79-96] — Manager.CtxAlloc（fork-continue 分配新上下文）
- [Source: context/context.go:220-230] — Manager.SetSystemPrompt（fork-continue 设置 prompt）
- [Source: context/context.go:233-256] — Manager.AppendMessage（fork-continue 回放消息）
- [Source: context/context.go:259-283] — Manager.AppendToolResult（fork-continue 回放工具结果）
- [Source: debug/record_manager.go:14-17] — RecordManager 结构（理解录制管理模型）
- [Source: debug/recorder.go:67-101] — Recorder.WriteEvent（理解 SeqNum 分配）
- [Source: ipc/protocol.go:30-33] — 现有 record/replay IPC 方法（新增 fork_continue 的位置）
- [Source: cmd/rnix/replay.go:88-260] — replay 交互式命令循环（新增 fork 分支的位置）
- [Source: cmd/rnix/replay.go:284-294] — printReplayHelp（新增 fork/continue 帮助）
- [Source: kernel/kernel.go:37-44] — SpawnOpts.ParentPID（fork 出的进程设置 PPID）

### 技术栈

- Go 1.26 — 标准库即可满足所有需求
- `encoding/json` — ForkMessage 序列化/反序列化
- `fmt` — fork 子模式输出
- `bufio` — fork 子模式输入扫描
- `strconv` — 参数解析
- 无新增外部依赖

### 前置 story 学习总结

**来自 14-1（执行录制与持久化）：**
1. RecordEvent 的 syscall 事件 Args 是 map[string]any，记录了 syscall 名称和参数
2. context_snapshot 事件包含 ContextSnapshotData{SystemPromptHash, MessageCount, Messages, TokenEstimate}
3. RecordMetadata 包含 Intent（原始意图）和 PID
4. 录制事件 SeqNum 单调递增，JSONL 格式

**来自 14-2（录制回放与导航）：**
1. RecordReader 全量加载事件到内存，按 SeqNum 排序
2. ReplaySession.cursor 语义：-1=未开始，0=第一个事件
3. 交互式命令循环使用 `bufio.Scanner` + `switch/case`
4. replay 是本地操作，不需要 IPC 连接

**来自 14-3（上下文快照对比）：**
1. SnapshotFinder 可查找最近的 context_snapshot 事件
2. ContextSnapshotData.Messages 是消息摘要列表（可能不是完整内容）
3. diff 不改变 cursor 位置
4. H1 修复：Diff() 需要检查 Context nil

**来自 Git 分析：**
- `9dcf56a` feat: story 14-3 done — 最新提交
- debug 包文件命名模式：`record.go`、`recorder.go`、`record_reader.go`、`replay.go`、`replay_format.go`、`snapshot_diff.go` → 新文件命名为 `fork.go`
- IPC 方法命名模式：`record_start`、`record_stop`、`replay_load` → 新增 `fork_continue`

**来自 kernel/kernel.go emitEvent 分析（录制事件内容）：**
- Spawn 事件 Args: `{"intent": "...", "agent": "...", "skills": [...], "allowed_devices": [...]}`
- CtxWrite SetSystemPrompt 事件 Args: `{"cid": N, "op": "SetSystemPrompt"}` — 不含 prompt 内容
- CtxWrite AppendMessage 事件 Args: `{"cid": N, "op": "AppendMessage", "role": "user/assistant"}` — 不含消息内容
- 结论：syscall 事件不记录消息内容，上下文重建需要依赖 context_snapshot + llm_response 事件

**来自 context/context.go 分析：**
- Context 结构体有 mutex，不可直接序列化
- CtxAlloc → SetSystemPrompt → AppendMessage 是上下文初始化标准序列
- AppendToolResult 需要 toolCallID（tool path）和 content

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6 (claude-opus-4-6)

### Debug Log References

- fork_test.go 共 18 个测试全部通过（SnapshotRestorer + ForkContext 修改操作）
- replay_test.go Fork/ForkAt 共 7 个测试全部通过
- ipc/server_test.go fork_continue 共 5 个测试全部通过
- cmd/rnix/replay_test.go fork CLI 共 3 个测试全部通过
- 关键修复：handleForkContinue 使用 kernel.NewProcess 替代 kernel.Spawn，避免 LLM device 依赖

### Completion Notes List

- 采用 SnapshotFinder + ContextSnapshotData.Messages 重建上下文，而非 syscall 事件（syscall 不含消息内容）
- system message 同时存入 SystemPrompt 字段和 Messages 列表，保持消息计数与快照一致
- handleForkContinue 直接创建进程（NewProcess + CtxAlloc + AppendMessage + AddProcess），绕过 Spawn 的 LLM 设备打开流程
- ForkMessageWire 作为 ForkContinueMessage 的类型别名，保持向后兼容
- fork 子模式使用嵌套 scanner 循环，cancel/continue 通过 return 回到外层 replay 循环

### File List

新建文件:
- `debug/fork.go` — SnapshotRestorer、ForkContext、ForkMessage 类型和方法
- `debug/fork_test.go` — SnapshotRestorer 和 ForkContext 测试

修改文件:
- `debug/replay.go` — 新增 Fork() 和 ForkAt() 方法
- `debug/replay_test.go` — 新增 Fork/ForkAt/DifferentPositions 测试
- `ipc/protocol.go` — 新增 MethodForkContinue、ForkContinueMessage、ForkContinueRequest、ForkContinueResponse
- `ipc/server.go` — 新增 handleForkContinue 处理函数
- `ipc/server_test.go` — 新增 fork_continue IPC 测试（5 个）
- `ipc/client.go` — 新增 ForkContinue() 方法
- `cmd/rnix/replay.go` — 新增 fork 命令、fork 子模式、executeForkContinue、printForkHelp
- `cmd/rnix/replay_test.go` — 新增 fork CLI 命令测试（3 个）
