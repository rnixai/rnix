# Story 14.2: 录制回放与导航

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a 平台构建者,
I want 回放录制的执行轨迹，支持正向播放、反向单步和任意跳转到指定时间点,
So that 我可以自由地浏览智能体的历史执行过程。

## Acceptance Criteria

1. **Given** 存在一个有效的录制文件（status=completed 或 stopped）
   **When** 用户执行 `rnix replay <record-id>`
   **Then** 系统加载录制数据并进入回放界面，显示录制摘要（事件数、时长、PID、Intent）

2. **Given** 用户在回放界面中
   **When** 用户执行正向播放（`next` / `n`）
   **Then** 系统按时间顺序展示下一个 DebugEvent 的详细信息（类型、时间戳、内容）

3. **Given** 用户在回放界面中
   **When** 用户执行反向单步（`prev` / `p`）
   **Then** 系统回退到上一个 DebugEvent，显示该时间点的完整状态

4. **Given** 用户在回放界面中
   **When** 用户跳转到指定事件编号（`goto <seq_num>`）
   **Then** 系统立即定位到该事件并显示对应状态

5. **Given** 用户在回放界面中
   **When** 用户执行 `list` 命令
   **Then** 系统显示当前位置附近的事件概览列表（前后各 5 条），标记当前位置

## Tasks / Subtasks

- [x] Task 1: 录制读取器（RecordReader）(AC: #1)
  - [x] 1.1 在 `debug/record_reader.go` 中实现 `RecordReader` 结构体：
    ```go
    type RecordReader struct {
        dir      string           // 录制目录路径
        metadata RecordMetadata   // 录制元数据
        events   []RecordEvent    // 内存中的完整事件列表
    }
    ```
  - [x] 1.2 实现 `NewRecordReader(recordDir string) (*RecordReader, error)`：
    - 读取 `metadata.json` 解析 RecordMetadata
    - 验证 status 为 "completed" 或 "stopped"（"recording" 状态返回错误）
    - 读取 `events.jsonl`，逐行 JSON 反序列化为 `[]RecordEvent`
    - 事件按 SeqNum 排序（确保顺序正确）
  - [x] 1.3 实现 `RecordReader.Metadata() RecordMetadata`
  - [x] 1.4 实现 `RecordReader.EventCount() int`
  - [x] 1.5 实现 `RecordReader.Event(seqNum uint64) (*RecordEvent, error)` — 根据 SeqNum 查找单个事件
  - [x] 1.6 实现 `RecordReader.Events() []RecordEvent` — 返回完整事件列表
  - [x] 1.7 实现 `RecordReader.EventsInRange(from, to uint64) []RecordEvent` — 返回 SeqNum 范围内的事件

- [x] Task 2: 录制发现与加载（RecordManager 扩展）(AC: #1)
  - [x] 2.1 在 `RecordManager` 上新增 `FindRecord(recordID string) (string, error)` 方法：
    - 在 baseDir 下查找匹配 recordID 的子目录
    - 返回完整目录路径或 "not found" 错误
  - [x] 2.2 在 `RecordManager` 上新增 `LoadRecord(recordID string) (*RecordReader, error)` 方法：
    - 调用 FindRecord 获取路径
    - 调用 NewRecordReader 加载数据
    - 返回 RecordReader 实例

- [x] Task 3: 回放会话（ReplaySession）(AC: #2, #3, #4, #5)
  - [x] 3.1 在 `debug/replay.go` 中实现 `ReplaySession` 结构体：
    ```go
    type ReplaySession struct {
        reader   *RecordReader
        cursor   int              // 当前位置（events 数组索引，0-based）
    }
    ```
  - [x] 3.2 实现 `NewReplaySession(reader *RecordReader) *ReplaySession`：
    - cursor 初始化为 -1（表示尚未开始播放）
  - [x] 3.3 实现导航方法：
    - `Next() (*RecordEvent, error)` — cursor++，返回当前事件；到末尾返回错误
    - `Prev() (*RecordEvent, error)` — cursor--，返回当前事件；到开头返回错误
    - `Goto(seqNum uint64) (*RecordEvent, error)` — 跳转到指定 SeqNum 的事件
    - `Current() (*RecordEvent, error)` — 返回当前位置的事件
    - `Position() (current int, total int)` — 返回当前位置和总事件数
  - [x] 3.4 实现 `List(context int) []ReplayListItem` — 返回当前位置附近的事件列表：
    ```go
    type ReplayListItem struct {
        Event    RecordEvent
        IsCursor bool  // 是否为当前位置
    }
    ```
    - context 参数控制前后显示多少条（默认 5）

- [x] Task 4: 事件格式化（ReplayFormatter）(AC: #2, #3, #4, #5)
  - [x] 4.1 在 `debug/replay_format.go` 中实现事件格式化函数：
    - `FormatReplayEvent(event *RecordEvent, verbose bool) string` — 格式化单个事件为可读字符串
    - Syscall 事件：`[#001 +0.5s] syscall: Open("/dev/llm/claude") → fd=3 (1.2ms)`
    - LLM 事件：`[#005 +2.1s] llm: model=claude-opus-4-6 req=1200tok resp=800tok`
    - Context 事件：`[#008 +3.0s] context: msgs=12 tokens≈4500 sysPromptHash=a1b2c3d4`
    - State 事件：`[#010 +5.0s] state: Running → Zombie (completed)`
  - [x] 4.2 实现 `FormatReplayList(items []ReplayListItem) string` — 格式化事件列表，当前位置用 `►` 标记
  - [x] 4.3 实现 `FormatReplaySummary(meta RecordMetadata, eventCount int) string` — 格式化录制摘要

- [x] Task 5: IPC 协议 -- replay 命令 (AC: #1)
  - [x] 5.1 在 `ipc/protocol.go` 中新增 `MethodReplayLoad Method = "replay_load"` 常量
  - [x] 5.2 定义 IPC 请求/响应类型：
    ```go
    type ReplayLoadRequest struct {
        RecordID string `json:"record_id"`
    }
    type ReplayLoadResponse struct {
        RecordID   string `json:"record_id"`
        PID        types.PID `json:"pid"`
        Intent     string `json:"intent"`
        EventCount int    `json:"event_count"`
        StartTimeMs int64 `json:"start_time_ms"`
        EndTimeMs   int64 `json:"end_time_ms"`
        Status     string `json:"status"`
    }
    ```
  - [x] 5.3 在 `ipc/server.go` 中新增 `handleReplayLoad` 方法：
    - 解析 RecordID
    - 调用 `s.kern.GetRecordManager().LoadRecord(recordID)`
    - 返回录制元数据摘要

- [x] Task 6: CLI 命令 -- rnix replay (AC: #1-5)
  - [x] 6.1 在 `cmd/rnix/replay.go` 中新建 `replay` Cobra 子命令：
    - `rnix replay <record-id>` — 加载录制并进入交互式回放循环
  - [x] 6.2 实现交互式回放命令循环（参考 gdb.go 的 scanner 循环模式）：
    - `next` / `n` — 正向单步
    - `prev` / `p` — 反向单步
    - `goto <seq_num>` — 跳转到指定事件
    - `list` / `l` — 显示当前位置附近的事件
    - `info` / `i` — 显示录制摘要
    - `help` / `h` — 显示帮助
    - `quit` / `q` — 退出回放
  - [x] 6.3 回放循环中每个导航命令执行后：
    - 显示格式化的当前事件详情
    - 显示位置指示器 `[3/128]`
    - 显示 `replay>` 提示符
  - [x] 6.4 在 `cmd/rnix/replay.go` 的 `init()` 中通过 `rootCmd.AddCommand(replayCmd)` 注册命令（与 gdb.go 模式一致）
  - [x] 6.5 支持 `--json` flag 输出 JSON 格式事件

- [x] Task 7: IPC Client 扩展 (AC: #1)
  - [x] 7.1 在 `ipc/client.go` 中新增 `Client.ReplayLoad(recordID string) (*ReplayLoadResponse, error)` 方法

- [x] Task 8: 测试 (AC: #1-5)
  - [x] 8.1 `debug/record_reader_test.go`：RecordReader 加载/解析/查询测试
    - 正常加载 completed 录制
    - 正常加载 stopped 录制
    - 拒绝加载 recording 状态的录制
    - 空 events.jsonl 处理
    - 损坏 JSON 行跳过/报错
  - [x] 8.2 `debug/replay_test.go`：ReplaySession 导航测试
    - Next 正向遍历到末尾
    - Prev 反向遍历到开头
    - Goto 跳转到有效/无效 SeqNum
    - List 返回正确的上下文窗口
    - Position 返回正确的当前/总数
  - [x] 8.3 `debug/replay_format_test.go`：事件格式化测试
    - 每种 RecordEventType 的格式化输出
    - ReplayList 的 cursor 标记
    - ReplaySummary 的格式化
  - [x] 8.4 `debug/record_manager_test.go`：新增 FindRecord/LoadRecord 测试
  - [x] 8.5 `ipc/server_test.go`：replay_load IPC 路由测试
  - [x] 8.6 `cmd/rnix/replay_test.go`：replay CLI 命令注册测试

## Dev Notes

### 架构决策

本 story 是 Epic 14（时间旅行调试）的第二层，在 Story 14-1 的录制基础上实现回放导航。设计原则：

1. **本地直读** — replay 命令直接读取录制文件（JSONL），不需要 daemon 运行中。IPC 的 replay_load 方法仅用于元数据预查询（如验证 record-id 存在），实际回放逻辑完全在 CLI 进程中执行。
2. **全量加载到内存** — 录制文件通常不大（每事件约 200-500 bytes JSON，1000 事件约 500KB），全量加载到 `[]RecordEvent` 数组提供 O(1) 随机跳转能力。
3. **交互式命令循环** — 复用 gdb.go 的 `bufio.Scanner` + `switch/case` 命令路由模式，用户体验一致。
4. **无需长连接** — replay 不需要 IPC 事件流（与 gdb 的 attach 不同），是纯本地文件操作。

### 关键设计：回放是本地操作

```
rnix replay <record-id>
    |
    +-- RecordManager.FindRecord(recordID) → 找到目录路径
    |
    +-- NewRecordReader(dir) → 加载 metadata.json + events.jsonl
    |
    +-- NewReplaySession(reader) → 初始化 cursor=-1
    |
    +-- 交互式命令循环：
        +-- next/n  → session.Next()  → FormatReplayEvent()
        +-- prev/p  → session.Prev()  → FormatReplayEvent()
        +-- goto N  → session.Goto(N) → FormatReplayEvent()
        +-- list/l  → session.List(5) → FormatReplayList()
        +-- info/i  → FormatReplaySummary()
        +-- quit/q  → 退出
```

### 关键设计：RecordReader 加载策略

录制文件结构（来自 Story 14-1）：
```
$PROJECT/.rnix/records/<pid>-<timestamp>/
├── metadata.json    # RecordMetadata
└── events.jsonl     # 每行一个 RecordEvent JSON
```

加载流程：
1. 读取 `metadata.json`，验证 status 不是 "recording"
2. 逐行读取 `events.jsonl`，每行 `json.Unmarshal` 为 `RecordEvent`
3. 按 SeqNum 排序（JSONL 写入时已保证顺序，但排序作为安全保障）
4. 存储为 `[]RecordEvent` 切片，支持 O(1) 索引访问

### 关键设计：ReplaySession cursor 语义

```
cursor = -1  → 初始状态（未开始播放）
cursor = 0   → 第一个事件
cursor = N-1 → 最后一个事件（N = len(events)）

Next(): cursor < len(events)-1 → cursor++, 返回 events[cursor]
Prev(): cursor > 0             → cursor--, 返回 events[cursor]
Goto(seqNum): 查找 events 中 SeqNum==seqNum 的索引 → cursor = index
```

### 关键设计：事件格式化

每种事件类型有专门的格式化逻辑：
```
Syscall:  [#001 +0.5s] syscall: Open("/dev/llm/claude") → fd=3 (1.2ms)
LLM:      [#005 +2.1s] llm: model=claude-opus-4-6 req=1200tok resp=800tok "响应摘要..."
Context:  [#008 +3.0s] context: msgs=12 tokens≈4500
State:    [#010 +5.0s] state: Running → Zombie (completed)
```

事件列表格式：
```
  [#006] +1.8s  syscall  Write
► [#007] +2.0s  llm      claude-opus-4-6
  [#008] +3.0s  context  msgs=12
```

### 关键设计：RecordID 发现

`rnix replay <record-id>` 中的 record-id 格式为 `<pid>-<timestamp>`（如 `42-1709856000`）。
发现逻辑：在 `$PROJECT/.rnix/records/` 下查找同名子目录。

如果 daemon 正在运行，可通过 IPC 的 `replay_load` 方法预验证。
如果 daemon 未运行，CLI 直接读取文件系统（通过 `RecordManager.FindRecord` 或直接构造路径）。

**重要：replay 命令应同时支持 daemon 运行和不运行两种场景。**

实现策略：
1. 先尝试 IPC 连接 daemon 获取 baseDir
2. 如果 daemon 不可用，使用默认 baseDir（`$CWD/.rnix/records/`）
3. 在 baseDir 下查找 record-id 对应的子目录

### 关键复用点

1. **RecordEvent/RecordMetadata 数据模型**：复用 `debug/record.go` 中的类型定义（Story 14-1 已定义）
2. **RecordManager.ListRecords**：复用其目录扫描逻辑，FindRecord 是其简化版
3. **gdb.go 命令循环模式**：复用 `bufio.Scanner` + `switch/case` 交互式命令路由
4. **Cobra 子命令注册**：复用 `cmd/rnix/record.go` 的注册模式
5. **IPC method 路由**：复用 `ipc/server.go` 的 handleXxx 路由模式
6. **IPC Client 请求-响应**：复用 `ipc/client.go` 的 `call()` 方法模式
7. **FormatEvent**：可参考 `debug/strace.go` 的 `FormatEvent` 和 `debug/event.go` 的格式化逻辑
8. **resolveOutputMode / flagJSON**：复用 `cmd/rnix/record.go` 的输出模式检测

### 不要做的事情

- **不要**实现上下文 diff（Story 14.3 的范围）
- **不要**实现 fork-continue（Story 14.4 的范围）
- **不要**使用 Bubble Tea TUI 框架 — 保持 cobra + bufio.Scanner 交互模式
- **不要**实现事件流式传输 — replay 是本地文件操作，不需要 IPC 长连接流
- **不要**修改 Story 14-1 中已有的 Recorder/RecordManager/RecordEvent 结构体
- **不要**实现录制文件的压缩或分页 — 当前事件量级不需要
- **不要**将 ReplaySession 放在 kernel 包里 — 放在 debug 包中，replay 是纯客户端操作
- **不要**在 replay 中启动新的 IPC 事件流 — replay 与 gdb attach 不同，不需要流式事件
- **不要**实现自动播放/定时播放 — 只实现手动导航（next/prev/goto）

### IPC 协议：replay 命令

```
replay_load:
  请求: {"method": "replay_load", "params": {"record_id": "42-1709856000"}}
  响应: {"ok": true, "data": {"record_id": "42-1709856000", "pid": 42, "intent": "分析代码", "event_count": 128, "start_time_ms": 1709856000000, "end_time_ms": 1709856060000, "status": "completed"}}
```

### CLI 回放交互示例

```
$ rnix replay 42-1709856000

[replay] Loading record 42-1709856000...
[replay] PID: 42 | Intent: "分析代码" | Events: 128 | Duration: 60s | Status: completed
[replay] Type 'help' for commands, 'quit' to exit

replay> next
[#001 +0.0s] syscall: Spawn(intent="分析代码") → pid=42
[1/128]

replay> next
[#002 +0.1s] syscall: CtxAlloc() → ctx_id=1
[2/128]

replay> next
[#003 +0.2s] syscall: Open("/dev/llm/claude") → fd=3 (0.5ms)
[3/128]

replay> list
  [#001] +0.0s  syscall  Spawn
  [#002] +0.1s  syscall  CtxAlloc
► [#003] +0.2s  syscall  Open
  [#004] +0.3s  syscall  CtxWrite
  [#005] +2.1s  llm      claude-opus-4-6

replay> goto 10
[#010 +5.0s] state: Running → Zombie (completed)
[10/128]

replay> prev
[#009 +4.8s] context: msgs=15 tokens≈5200
[9/128]

replay> info
Record ID:  42-1709856000
PID:        42
Intent:     分析代码
Events:     128
Duration:   60s
Status:     completed
Position:   9/128

replay> quit
[replay] session ended.
```

### 组合矩阵

| 本功能 | 交互对象 | 交互方式 | 需验证 |
|--------|----------|----------|--------|
| replay | record（录制） | 依赖：replay 读取 record 的输出文件 | 是 |
| replay | strace | 独立：互不干扰，replay 读文件，strace 读 DebugChan | 否 |
| replay | gdb | 独立：replay 是离线操作，gdb 是在线调试 | 否 |
| replay | daemon 运行状态 | 兼容：daemon 运行或不运行时均可 replay | 是 |
| replay CLI | --json flag | 共存：JSON 模式输出每个事件的 JSON 格式 | 是 |

### Project Structure Notes

新建文件：
- `debug/record_reader.go` — RecordReader 实现（录制文件读取）
- `debug/replay.go` — ReplaySession 实现（回放导航）
- `debug/replay_format.go` — 回放事件格式化
- `debug/record_reader_test.go` — RecordReader 单元测试
- `debug/replay_test.go` — ReplaySession 单元测试
- `debug/replay_format_test.go` — 格式化单元测试
- `cmd/rnix/replay.go` — replay CLI 子命令
- `cmd/rnix/replay_test.go` — replay CLI 测试

修改文件：
- `debug/record_manager.go` — 新增 FindRecord/LoadRecord 方法
- `debug/record_manager_test.go` — 新增 FindRecord/LoadRecord 测试
- `ipc/protocol.go` — 新增 MethodReplayLoad 常量和请求/响应类型
- `ipc/server.go` — 新增 handleReplayLoad 路由
- `ipc/client.go` — 新增 ReplayLoad 方法
- `ipc/server_test.go` — 新增 replay_load 测试
- `cmd/rnix/main.go` — 注册 replayCmd

### References

- [Source: debug/record.go:1-117] — RecordEvent/RecordMetadata 数据模型（直接复用）
- [Source: debug/recorder.go:1-162] — Recorder 写入器（理解 JSONL 格式和 metadata.json 结构）
- [Source: debug/record_manager.go:1-137] — RecordManager（扩展 FindRecord/LoadRecord）
- [Source: debug/record_manager.go:104-131] — ListRecords 目录扫描逻辑（FindRecord 参考）
- [Source: cmd/rnix/gdb.go:197-249] — gdb 交互式命令循环（replay 命令循环参考）
- [Source: cmd/rnix/gdb.go:260-280] — printGdbHelp（printReplayHelp 参考）
- [Source: cmd/rnix/record.go:1-189] — record CLI 命令（Cobra 注册和 IPC 调用模式参考）
- [Source: debug/strace.go] — FormatEvent 格式化逻辑（FormatReplayEvent 参考）
- [Source: ipc/protocol.go:17-33] — Method 常量定义（新增 MethodReplayLoad）
- [Source: ipc/protocol.go:361-397] — Record 相关 IPC 类型（ReplayLoad 类型参考）
- [Source: ipc/server.go:1044-1126] — handleRecord* IPC 路由（handleReplayLoad 参考）
- [Source: ipc/client.go:330-367] — Record* Client 方法（ReplayLoad 参考）

### 技术栈

- Go 1.26 — 标准库即可满足所有需求
- `encoding/json` — JSON 反序列化 RecordEvent（标准库）
- `bufio.Scanner` — 逐行读取 JSONL 文件 + 交互式命令输入（标准库）
- `sort.Slice` — 事件按 SeqNum 排序保障（标准库）
- `os` — 文件和目录操作（标准库）
- `fmt` — 事件格式化输出（标准库）
- Cobra v1.10.2 — replay 子命令注册
- IPC Unix domain socket — replay_load 元数据查询（可选）

### 前置 story 学习总结（来自 14-1）

1. **JSONL 格式已确定** — events.jsonl 每行一个 RecordEvent JSON，RecordReader 逐行 `json.Unmarshal` 即可
2. **metadata.json 独立存储** — 快速查询录制信息无需解析事件流
3. **RecordManager.ListRecords 扫描模式** — FindRecord 可复用相同的目录遍历逻辑
4. **SeqNum 在 lock 内分配** — 保证 JSONL 中 SeqNum 单调递增，RecordReader 排序是双重保障
5. **录制目录结构固定** — `$PROJECT/.rnix/records/<pid>-<timestamp>/`，replay 直接按此查找
6. **RecordEvent 包含 Timestamp（time.Duration）** — 相对进程启动时间，格式化时显示为 `+Xs`
7. **14-1 代码审查修复** — TruncateString 已改为 rune 计数，WriteEvent SeqNum 已在 lock 内分配，Shutdown 已调用 CloseAll
8. **IPC 路由模式稳定** — record_start/record_stop/record_list 三个方法已验证，replay_load 按相同模式添加

### Git 分析（最近提交）

最近 5 个提交全部来自 Epic 13 和 Epic 14-1：
- `26ac177` feat: implement execution recording and persistence for GDB debugging
- `7fc3b02` feat: update .gitignore and add manual verification guide for GDB debugging
- `45e1f13` feat: story 13-4 done
- `5883dba` feat: story 13-3 done
- `9ec20a4` feat: story 13-2 done

代码模式稳定，gdb + record 命令的 IPC/CLI 模式已成型。

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6 (claude-opus-4-6)

### Debug Log References

- formatTimestamp name collision with debug/strace.go -- renamed to formatReplayTimestamp in replay_format.go

### Completion Notes List

- Task 1: Implemented RecordReader with full JSONL loading, SeqNum sorting, corrupted line skipping, and status validation (13 unit tests pass)
- Task 2: Extended RecordManager with FindRecord/LoadRecord methods (4 tests pass)
- Task 3: Implemented ReplaySession with Next/Prev/Goto/Current/Position/List navigation (18 tests pass)
- Task 4: Implemented FormatReplayEvent/FormatReplayList/FormatReplaySummary with per-type formatting (8 tests pass)
- Task 5: Added MethodReplayLoad IPC protocol constant, ReplayLoadRequest/ReplayLoadResponse types, and handleReplayLoad server handler (2 IPC tests pass)
- Task 6: Implemented rnix replay CLI command with interactive command loop (next/prev/goto/list/info/help/quit), daemon-optional design, --json flag support (3 CLI tests pass)
- Task 7: Added Client.ReplayLoad IPC client method
- Task 8: All 48 ATDD tests pass, full test suite (19 packages) passes with no regressions, race detector clean

### File List

New files:
- debug/record_reader.go — RecordReader implementation
- debug/replay.go — ReplaySession implementation
- debug/replay_format.go — Replay event formatting (FormatReplayEvent/List/Summary)
- cmd/rnix/replay.go — replay CLI command with interactive loop

Modified files:
- debug/record_manager.go — Added FindRecord/LoadRecord/BaseDir methods
- ipc/protocol.go — Added MethodReplayLoad, ReplayLoadRequest, ReplayLoadResponse
- ipc/server.go — Added handleReplayLoad route and handler
- ipc/client.go — Added ReplayLoad client method
Test files (created by ATDD step, already existed):
- debug/record_reader_test.go — 13 RecordReader tests
- debug/replay_test.go — 18 ReplaySession tests
- debug/replay_format_test.go — 8 formatter tests
- debug/record_manager_test.go — 4 FindRecord/LoadRecord tests (appended)
- ipc/server_test.go — 2 replay_load IPC tests (appended)
- cmd/rnix/replay_test.go — 3 CLI tests

## Change Log

- 2026-03-08: Implemented Story 14-2 recording replay and navigation. Added RecordReader for JSONL loading, ReplaySession for cursor-based navigation, event formatting, IPC replay_load method, and interactive CLI replay command. 48 tests added, all pass.
- 2026-03-08: Code review fixes: (1) Replaced over-engineered anonymous interface in formatReplayTimestamp with concrete time.Duration type; (2) Added scanner.Err() check after replay command loop; (3) Removed redundant IPC verification in replay CLI (replay is a local file operation); (4) Updated story File List to reflect actual implementation (init() pattern, not main.go modification). All 19 packages pass, race detector clean.
