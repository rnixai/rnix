# Story 27.1: StepRecord 类型定义与磁盘写入器

Status: review

## Story

As a 平台构建者,
I want 系统在每个 reasonStep 完成后自动将该步的完整数据（Messages 快照、LLM 响应、工具结果）以 NDJSON 格式写入磁盘,
So that 我无需手动开启录制即可事后查看任意步骤的完整 LLM 输入/输出。

## Acceptance Criteria

### AC-1: StepRecord 类型定义

**Given** `internal/types/step_record.go` 不存在
**When** 创建 StepRecord 类型
**Then** 类型包含以下字段：Step(int)、Timestamp(time.Duration)、Messages([]context.Message 深拷贝)、MessageCount(int)、TokenCount(int)、RawResponse(string)、Action(string)、Summary(string)、ToolPath(string)、ToolInput(string)、ToolResult(string)、ToolError(string)、ToolDuration(time.Duration)、RequestTokens(int)、ResponseTokens(int)

### AC-2: StepWriter 实现

**Given** `kernel/step_writer.go` 不存在
**When** 创建 StepWriter
**Then** StepWriter 使用 `bufio.Writer`（64KB buffer）写入 `.rnix/data/steps/<pid>/steps.jsonl`
**And** `WriteStep(rec StepRecord) error` 在 mu.Lock 下 JSON marshal → append → WriteByte('\n') → Flush
**And** 每次 WriteStep 调用均 Flush（保证读取端可见）
**And** `Close() error` 方法 flush + close file

### AC-3: 进程 Spawn 时自动创建 StepWriter

**Given** 进程通过 Spawn 创建
**When** 进程首次进入 reasonStep 循环
**Then** 自动创建 `.rnix/data/steps/<pid>/` 目录和 `steps.jsonl` 文件
**And** 创建对应的 StepWriter 实例挂载到 Process 上

### AC-4: Process 新增观察系统字段

**Given** Process 结构体
**When** 添加观察系统字段
**Then** 新增 `FinalSystemPrompt string` 字段（Spawn 后首次 reasonStep 中保存含 protocol/skills 注入的完整 SystemPrompt）
**And** 新增 `stepWriter *StepWriter` 字段（mu protected）

### AC-5: FinalSystemPrompt 首次捕获

**Given** reasonStep 循环中 BuildPrompt 返回 promptResult
**When** 首次执行时
**Then** 将完整 sysPrompt（含 protocol 注入、skills 列表）保存到 `proc.FinalSystemPrompt`（仅首次，后续不覆盖）

### AC-6: StepRecord 组装与写入

**Given** reasonStep 循环中一步执行完成（LLM 响应已解析、工具已执行）
**When** 组装 StepRecord
**Then** Messages 字段使用 `promptResult.Messages`（BuildPrompt 已做深拷贝，零额外成本）
**And** RawResponse 为 LLM 原始响应全文（不截断）
**And** ToolResult 为工具返回的完整结果（不截断）
**And** StepRecord 写入在 `AppendMessage()` 之前调用（确保快照一致性）

### AC-7: 写入性能

**Given** StepWriter.WriteStep 被调用
**When** 度量写入耗时
**Then** 单次写入耗时 ≤ 1ms（NFR62）

### AC-8: 进程退出时清理

**Given** 进程进入 Zombie/Dead 状态
**When** reaper 准备清理 Process 结构体
**Then** 先将 `FinalSystemPrompt` 和 `NativeToolDefs` 序列化写入 `.rnix/data/steps/<pid>/process-meta.json`
**And** 然后 Close StepWriter
**And** `steps/` 目录默认保留 7 天

### AC-9: record 系统简化

**Given** 现有 record 系统中的 `recordContextSnapshot` 和 `recordLLMResponse`
**When** StepRecord 已包含完整数据
**Then** 简化：删除 `ContextSnapshotData` 中的 `SystemPromptHash`/`MessageCount`/`TokenEstimate` 摘要逻辑
**And** 删除 `LLMResponseData` 中的 `ResponseSummary`（500 字截断）
**And** record 系统可引用 StepRecord 数据而非自行拼凑

## Tasks / Subtasks

- [x] Task 1: 创建 StepRecord 类型 (AC: #1)
  - [x] 1.1 新建 `internal/types/step_record.go`
  - [x] 1.2 定义 StepRecord 结构体，所有字段加 json tag
  - [x] 1.3 注意 Messages 字段类型为 `[]context.Message`，需 import `context` 包（alias `rnixctx`）
- [x] Task 2: 创建 StepWriter (AC: #2, #3)
  - [x] 2.1 新建 `kernel/step_writer.go`
  - [x] 2.2 实现 `NewStepWriter(baseDir string, pid types.PID) (*StepWriter, error)` — 创建目录 + 打开文件 + 64KB bufio.Writer
  - [x] 2.3 实现 `WriteStep(rec types.StepRecord) error` — mu.Lock → json.Marshal → Write → WriteByte('\n') → Flush
  - [x] 2.4 实现 `Close() error` — flush + close
  - [x] 2.5 实现辅助函数 `ReadStep(path string, targetStep int) (*types.StepRecord, error)` — 顺序扫描 JSONL
- [x] Task 3: Process 结构体扩展 (AC: #4)
  - [x] 3.1 在 `kernel/process.go` 的 Process 结构体中新增 `FinalSystemPrompt string`
  - [x] 3.2 新增 `stepWriter *StepWriter`（mu protected）
- [x] Task 4: reasonStep 中集成 StepRecord 捕获 (AC: #5, #6)
  - [x] 4.1 在 reasonStep 循环开始时创建 StepWriter（首次进入循环时）
  - [x] 4.2 BuildPrompt 后保存 FinalSystemPrompt（首次，if 为空才写）
  - [x] 4.3 LLM 响应解析 + 工具执行完成后，组装 StepRecord
  - [x] 4.4 在 AppendMessage 之前调用 stepWriter.WriteStep(rec)
- [x] Task 5: reaper 清理集成 (AC: #8)
  - [x] 5.1 在 reaper 逻辑中，Process 被清理前：写入 process-meta.json
  - [x] 5.2 写入后 Close StepWriter
  - [x] 5.3 process-meta.json 结构：`{system_prompt: string, tool_defs: []ToolDef}`
- [x] Task 6: record 系统简化 (AC: #9)
  - [x] 6.1 删除 `ContextSnapshotData` 中的 SystemPromptHash/MessageCount/TokenEstimate
  - [x] 6.2 删除 `LLMResponseData` 中的 ResponseSummary
  - [x] 6.3 确保 record list/replay 不因删除字段而 panic
- [x] Task 7: 单元测试 (AC: #2, #7)
  - [x] 7.1 `kernel/step_writer_test.go`：TestStepWriter_AppendAndRead
  - [x] 7.2 TestStepWriter_ConcurrentReadWrite（并发安全）
  - [x] 7.3 TestStepWriter_FlushGuarantee（写后即可读）
  - [x] 7.4 验证写入性能 ≤ 1ms
- [x] Task 8: `make all` 全部通过 (AC: all)

## Dev Notes

### 架构决策引用

- **Decision 23**: StepRecord — 默认全量步骤记录 [Source: architecture/core-architectural-decisions.md#decision-23]
- **Decision 24**: Progress 回调 + StepRecord 双层架构 [Source: architecture/core-architectural-decisions.md#decision-24]

### 关键代码路径

**reasonStep 循环位置：** `kernel/kernel.go` 中的 `reasonStep()` 方法（约 line 1005+）

**循环内集成点（按执行顺序）：**

```
1. BuildPrompt() → promptResult（Messages 已拷贝）
2. 保存 FinalSystemPrompt（首次，if proc.FinalSystemPrompt == ""）
3. 构建 LLM 请求 → 发送 → 接收响应
4. 解析 action type
5. 执行工具（如果是 tool_call）
6. 组装 StepRecord{Messages, RawResponse, ToolResult, ...}
7. stepWriter.WriteStep(rec)    ← 新增
8. OnStepComplete 回调
9. AppendMessage 写回 Context
```

**关键约束：步骤 7 必须在步骤 9 之前**，确保 Messages 快照反映该步 LLM 实际看到的输入。

### 现有字段和类型参考

**Process 结构体**（`kernel/process.go`）已有字段：
- `NativeToolDefs []vfs.ToolDef` — 工具定义，reaper 时需序列化到 process-meta.json
- `logHistory []types.LogEntry` — ring buffer
- `tokenHistory []types.TokenSnapshot` — ring buffer
- `DebugChan chan types.SyscallEvent` — strace 事件通道
- `LogChan chan types.LogEntry` — 日志通道（将被废弃，但本 Story 仅简化不删除）

**Context Message 类型**（`context/context.go`）：
```go
type Message struct {
    Role       Role
    Content    string
    ToolCallID string
    ToolCalls  []ToolCall
}
```

**BuildPrompt 返回**（`context/context.go`）：
```go
type PromptResult struct {
    SystemPrompt string
    Messages     []Message  // 已深拷贝
}
```

**ProgressPayload**（`ipc/protocol.go`）：
```go
type ProgressPayload struct {
    Event    string    // "spawn", "step", "step_complete", "complete", "error"
    PID      types.PID
    Step     int
    Total    int
    Action   string
    Summary  string
    // ... 其他字段
}
```

### StepWriter 标准实现

```go
type StepWriter struct {
    file   *os.File
    writer *bufio.Writer
    mu     sync.Mutex
}

func NewStepWriter(baseDir string, pid types.PID) (*StepWriter, error) {
    dir := filepath.Join(baseDir, "data", "steps", fmt.Sprintf("%d", pid))
    if err := os.MkdirAll(dir, 0o755); err != nil {
        return nil, err
    }
    f, err := os.OpenFile(filepath.Join(dir, "steps.jsonl"),
        os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
    if err != nil {
        return nil, err
    }
    return &StepWriter{
        file:   f,
        writer: bufio.NewWriterSize(f, 64*1024),
    }, nil
}
```

### FinalSystemPrompt 捕获模式

```go
// 在 reasonStep 循环中 BuildPrompt 之后：
sysPrompt := promptResult.SystemPrompt
// 如果有 protocol/skills 注入，此处已包含
proc.mu.Lock()
if proc.FinalSystemPrompt == "" {
    proc.FinalSystemPrompt = sysPrompt
}
proc.mu.Unlock()
```

### Messages 深拷贝规则

**必须**复用 BuildPrompt 已有的拷贝：
```go
rec.Messages = promptResult.Messages  // ✅ 零额外成本
```

**禁止**从 Context Manager 二次读取：
```go
msgs, _ := k.ctxMgr.GetMessages(proc.CtxID)
rec.Messages = deepCopy(msgs)  // ❌ 浪费
```

### 存储路径

```
.rnix/data/steps/<pid>/
├── steps.jsonl          # StepRecord NDJSON
└── process-meta.json    # reaper 清理前写入
```

baseDir 为项目 `.rnix/` 目录。通过 `config.ProjectDir()` 或现有的 `.rnix/` 路径获取。

### record 系统简化说明

现有 `debug/record.go` 中的类型：
- `ContextSnapshotData` — 含 `SystemPromptHash`, `MessageCount`, `TokenEstimate`（摘要字段，可删）
- `LLMResponseData` — 含 `ResponseSummary`（500 字截断，可删）

简化策略：删除这些摘要字段，保留 `RecordEvent` 结构框架。`record list`/`replay` 命令在后续 Story 中改为读 steps.jsonl。本 Story 只做删除，不重写 replay。

### 并发安全模型

- **写入端**（reasonStep goroutine）：通过 `StepWriter.mu` 保护
- **读取端**（未来 GetStepDetail handler）：独立 `os.Open` + `bufio.Scanner`，不复用 StepWriter 的 file handle
- **NDJSON append-only 语义**：读取端看到的行要么完整要么不存在，无需跨组件加锁
- **reaper 并发保护**：reaper 在写 process-meta.json 和 Close StepWriter 前，通过 `proc.mu.Lock()` 确保 reasonStep 已退出

### Project Structure Notes

- 新增文件符合架构规范：`internal/types/step_record.go`、`kernel/step_writer.go`
- 修改文件：`kernel/process.go`、`kernel/kernel.go`、`debug/record.go`
- 存储路径 `.rnix/data/steps/` 遵循 [Source: architecture/project-structure-boundaries.md#统一观察系统目录结构]

### References

- [Architecture Decision 23: StepRecord](../_bmad-output/planning-artifacts/architecture/core-architectural-decisions.md#decision-23-steprecord--默认全量步骤记录)
- [Architecture Decision 24: 双层架构](../_bmad-output/planning-artifacts/architecture/core-architectural-decisions.md#decision-24-统一观察系统架构--progress-回调--steprecord)
- [Implementation Patterns: 观察系统专项](../_bmad-output/planning-artifacts/architecture/implementation-patterns-consistency-rules.md#命名模式观察系统专项)
- [Project Structure: 观察系统目录结构](../_bmad-output/planning-artifacts/architecture/project-structure-boundaries.md#统一观察系统目录结构)
- [Epic 27 Story 27.1](../_bmad-output/planning-artifacts/epics/epic-27-统一观察系统-unified-observation-system.md)
- [Epic 26 Context: 统一推理循环](../_bmad-output/implementation-artifacts/26-5-documentation-update.md)

### Previous Epic Intelligence (Epic 26)

- reasonStep 已统一为单一循环，7 种 ActionType：text, tool_call, plan, spawn, complete, replan, specialize
- `SpawnOpts.ReasoningMode` 已删除，Spawn 入口统一
- 熔断机制存在（consecutiveToolErrors >= 3）
- `AgentManifest.Reasoning string` 改为 `Planning *bool`
- VFS flags 自动降级机制已就位
- `make all` 20 个测试包全部通过

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6 (1M context)

### Debug Log References

无阻塞问题。所有实现一次通过。

### Completion Notes List

- ✅ StepRecord 类型已定义（`internal/types/step_record.go`），Messages 使用 `json.RawMessage` 避免循环导入
- ✅ StepWriter 实现 64KB buffered NDJSON writer，WriteStep 每次 Flush 保证即时可见性
- ✅ Process 新增 `FinalSystemPrompt` 和 `stepWriter` 字段（mu protected）
- ✅ KernelImpl 新增 `stepDataDir` 字段和 `SetStepDataDir()` 方法（用于测试注入）
- ✅ reasonStep 循环开始时自动创建 StepWriter（best-effort，创建失败不影响主流程）
- ✅ FinalSystemPrompt 在首次 BuildPrompt 后捕获（含 protocol/skills 注入的完整 sysPrompt）
- ✅ 所有 7 种 ActionType（text, tool_call, plan, complete, replan, spawn, specialize）+ native tool calls 均写入 StepRecord
- ✅ StepRecord 写入严格在 AppendMessage/AppendToolResult 之前执行，确保 Messages 快照一致性
- ✅ reapProcess 在 wg.Wait() 后写入 process-meta.json（含 FinalSystemPrompt 和 NativeToolDefs），然后 Close StepWriter
- ✅ record 系统简化：删除 ContextSnapshotData.{SystemPromptHash, MessageCount, TokenEstimate}、LLMResponseData.ResponseSummary
- ✅ 同步更新了 snapshot_diff.go、replay_format.go、dashboard.go 及所有相关测试文件
- ✅ 性能验证：StepWriter 单次写入平均 < 1ms
- ✅ `make all`（lint + vet + test + build）22 个包全部通过，0 lint issues

### File List

**新增文件：**
- `internal/types/step_record.go` — StepRecord 类型定义
- `kernel/step_writer.go` — StepWriter NDJSON writer + ReadStep 辅助函数
- `kernel/atdd_27_1_step_record_test.go` — ATDD 测试（类型、StepWriter、集成）
- `debug/atdd_27_1_record_simplify_test.go` — ATDD record 简化测试

**修改文件：**
- `kernel/kernel.go` — reasonStep 集成 StepRecord 捕获、FinalSystemPrompt 捕获、writeStepRecord helper、recordContextSnapshot 简化、SetStepDataDir 方法
- `kernel/process.go` — Process 新增 FinalSystemPrompt、stepWriter 字段
- `kernel/reap.go` — reapProcess 写入 process-meta.json + Close StepWriter
- `debug/record.go` — 删除 ContextSnapshotData/LLMResponseData 摘要字段
- `debug/snapshot_diff.go` — 适配简化后的 ContextSnapshotData
- `debug/replay_format.go` — 适配简化后的类型
- `debug/snapshot_diff_test.go` — 更新测试适配简化
- `debug/replay_format_test.go` — 更新测试适配简化
- `debug/fork_test.go` — 更新测试适配简化
- `cmd/rnix/dashboard.go` — 适配 TokenEstimate 移除
- `cmd/rnix/dashboard_test.go` — 更新测试适配简化

### Change Log

- 2026-03-21: Story 27.1 完整实现 — StepRecord 类型定义、StepWriter 磁盘写入器、reasonStep 集成、reaper 清理、record 系统简化
