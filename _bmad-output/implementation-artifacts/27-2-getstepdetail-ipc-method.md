# Story 27.2: GetStepDetail IPC 方法

Status: done

## Story

As a 平台构建者,
I want 通过 IPC 按需查询指定进程指定步骤的完整 prompt 内容（SystemPrompt + Messages + Tools）,
So that 我可以在 watch 视图中按 p 键看到 agent 当时收到了什么指令。

## Acceptance Criteria

### AC-1: IPC 协议新增 GetStepDetail 方法

**Given** `ipc/protocol.go` 中的方法常量列表
**When** 添加 GetStepDetail 方法
**Then** 新增 `MethodGetStepDetail Method = "get_step_detail"`
**And** 新增 `GetStepDetailRequest` 结构体：`PID types.PID`、`Step int`
**And** 新增 `GetStepDetailResponse` 结构体：`SystemPrompt string`、`Tools []ToolDefWire`、`Step int`、`Messages []MessageWire`、`MessageCount int`、`TokenCount int`、`RawResponse string`、`Action string`、`Summary string`、`ToolPath string`、`ToolInput string`、`ToolResult string`、`ToolError string`、`ToolDurationMs float64`、`RequestTokens int`、`ResponseTokens int`
**And** 新增 `MessageWire` 结构体：`Role string`、`Content string`、`ToolCallID string`、`ToolCalls []ToolCallWire`
**And** 新增 `ToolCallWire` 结构体：`ID string`、`Name string`、`Input map[string]any`
**And** 新增 `ToolDefWire` 结构体：`Name string`、`Description string`、`Parameters map[string]any`

### AC-2: Server handler — 进程仍在运行

**Given** 进程仍在运行（Running 状态）
**When** 请求 GetStepDetail(pid, step=3)
**Then** 从 `s.kern.GetProcess(pid)` 获取 `FinalSystemPrompt` + `nativeToolDefs`（mu.Lock 读取）
**And** 从 steps.jsonl 用 `kernel.ReadStep()` 读取 step=3 的行
**And** 反序列化 StepRecord.Messages（json.RawMessage → []context.Message → []MessageWire）
**And** 返回完整的 GetStepDetailResponse

### AC-3: Server handler — 进程已死但仍在内存

**Given** 进程已死但 Process 尚在内存（Zombie/Dead 状态）
**When** 请求 GetStepDetail(pid, step=3)
**Then** 同 AC-2，正常返回

### AC-4: Server handler — 进程已被 reaper 清理

**Given** 进程已被 reaper 清理（Process 不在内存）
**When** 请求 GetStepDetail(pid, step=3)
**Then** 从 `.rnix/data/steps/<pid>/process-meta.json` 读取 `system_prompt` + `tool_defs`
**And** 从 steps.jsonl 读取 step=3 的行
**And** 返回完整的 GetStepDetailResponse

### AC-5: 错误处理 — PID 不存在

**Given** PID 不存在且无磁盘文件
**When** 请求 GetStepDetail(pid, step=3)
**Then** 返回 `ErrorPayload{Code: "not_found", Message: "process <pid> not found"}`

### AC-6: 错误处理 — Step 超出范围

**Given** Step 超出已记录范围
**When** 请求 GetStepDetail(pid, step=99) 但只录制了 5 步
**Then** 返回 `ErrorPayload{Code: "not_found", Message: "step 99 not yet recorded"}`

### AC-7: Client 方法

**Given** `ipc/client.go`
**When** 添加 GetStepDetail 客户端方法
**Then** `func (c *Client) GetStepDetail(pid types.PID, step int) (*GetStepDetailResponse, error)`
**And** 内部 marshal GetStepDetailRequest → call → unmarshal GetStepDetailResponse

### AC-8: 性能要求

**Given** steps.jsonl 文件大小 ≤ 2MB（30 步典型场景）
**When** GetStepDetail 扫描文件
**Then** 返回延迟 ≤ 500ms（NFR61）

## Tasks / Subtasks

- [x] Task 1: protocol.go 新增类型定义 (AC: #1)
  - [x] 1.1 在 Method 常量块中添加 `MethodGetStepDetail Method = "get_step_detail"`
  - [x] 1.2 定义 `GetStepDetailRequest{PID types.PID, Step int}`
  - [x] 1.3 定义 `MessageWire{Role, Content, ToolCallID, ToolCalls []ToolCallWire}`
  - [x] 1.4 定义 `ToolCallWire{ID, Name, Input map[string]any}`
  - [x] 1.5 定义 `ToolDefWire{Name, Description, Parameters map[string]any}`
  - [x] 1.6 定义 `GetStepDetailResponse`（完整字段见 AC-1）
- [x] Task 2: server.go 新增 handler (AC: #2, #3, #4, #5, #6)
  - [x] 2.1 在 `handleConn` 的 switch 中添加 `case MethodGetStepDetail: s.handleGetStepDetail(conn, req.Payload)`
  - [x] 2.2 实现 `handleGetStepDetail(conn net.Conn, rawPayload json.RawMessage)`
  - [x] 2.3 实现辅助函数 `resolveStepsPathFromProc` + `resolveStepsPathFallback` — 查找 steps.jsonl 路径
  - [x] 2.4 实现辅助函数 `readProcessMeta(metaPath string) (*processMetaFile, error)`
  - [x] 2.5 实现 `context.Message → MessageWire` 转换逻辑 (`messagesToWire` + `toolDefsToWire`)
- [x] Task 3: client.go 新增方法 (AC: #7)
  - [x] 3.1 添加 `func (c *Client) GetStepDetail(pid types.PID, step int) (*GetStepDetailResponse, error)`
- [x] Task 4: 单元测试 (AC: #2-#8)
  - [x] 4.1 TestGetStepDetail_RunningProcess — 进程在内存，返回正确数据
  - [x] 4.2 TestGetStepDetail_ReapedProcess — 进程已清理，从磁盘读取 meta + steps
  - [x] 4.3 TestGetStepDetail_PIDNotFound — 返回 not_found 错误
  - [x] 4.4 TestGetStepDetail_StepNotFound — 返回 step not yet recorded 错误
  - [x] 4.5 TestGetStepDetail_MessageWireConversion — Messages json.RawMessage → []MessageWire 正确转换
- [x] Task 5: `make all` 全部通过 (AC: all)

## Dev Notes

### 架构决策引用

- **Decision 25**: GetStepDetail — 按需读取步骤详情 [Source: architecture/core-architectural-decisions.md#decision-25]
- **Decision 23**: StepRecord — 默认全量步骤记录 [Source: architecture/core-architectural-decisions.md#decision-23]

### 关键实现模式

#### 1. IPC 方法注册模式（遵循现有模式）

**protocol.go** — 在常量块末尾添加：
```go
MethodGetStepDetail Method = "get_step_detail"
```

**server.go** — switch 分发中添加 case：
```go
case MethodGetStepDetail:
    s.handleGetStepDetail(conn, req.Payload)
```

**client.go** — 遵循 `CtxProfile` 模式：
```go
func (c *Client) GetStepDetail(pid types.PID, step int) (*GetStepDetailResponse, error) {
    resp, err := c.call(MethodGetStepDetail, GetStepDetailRequest{PID: pid, Step: step})
    if err != nil {
        return nil, err
    }
    var result GetStepDetailResponse
    if err := json.Unmarshal(resp.Payload, &result); err != nil {
        return nil, fmt.Errorf("ipc: unmarshal get_step_detail: %w", err)
    }
    return &result, nil
}
```

#### 2. Handler 三阶段数据获取逻辑

```
阶段 A: 获取 SystemPrompt + ToolDefs
  ├─ 尝试 s.kern.GetProcess(pid)
  │   ├─ 成功 → proc.mu.Lock() 读 FinalSystemPrompt + nativeToolDefs → unlock
  │   └─ 失败 → 继续阶段 A'
  └─ 阶段 A': 从 process-meta.json 读取
      ├─ 成功 → 解析 system_prompt + tool_defs
      └─ 失败 → 返回 ErrorPayload{Code: "not_found"}

阶段 B: 获取 StepRecord
  └─ kernel.ReadStep(stepsJSONLPath, step)
      ├─ 成功 → 继续阶段 C
      └─ 失败 → 返回 ErrorPayload{Code: "not_found", Message: "step N not yet recorded"}

阶段 C: 组装 GetStepDetailResponse
  ├─ SystemPrompt 来自阶段 A
  ├─ Tools 来自阶段 A (nativeToolDefs → []ToolDefWire)
  ├─ Messages: rec.Messages (json.RawMessage) → json.Unmarshal → []context.Message → []MessageWire
  └─ 其余字段直接从 StepRecord 复制
```

#### 3. Steps 目录路径解析

Server 需要定位 `steps.jsonl`。路径格式为 `<baseDir>/data/steps/<pid>/steps.jsonl`。

baseDir 获取策略：
1. **进程仍在内存** → 从 `proc.ProjectConfig.ProjectDir + "/.rnix"` 获取；若无 ProjectConfig 则尝试 `k.stepDataDir`
2. **进程已清理** → Server 需要一种方式获取 stepDataDir

**推荐方案**：在 Server 上增加 `stepDataDir string` 字段（在 `NewServer` 时从 KernelImpl 获取或通过 setter 注入），作为进程已清理时的 fallback 路径。进程在内存时优先使用 Process 关联的路径。

具体实现：
```go
func (s *Server) resolveStepsPath(pid types.PID) string {
    pidStr := fmt.Sprintf("%d", pid)
    // 先尝试从 Process 获取项目目录
    if proc, ok := s.kern.GetProcess(pid); ok {
        proc.mu.Lock()
        pc := proc.ProjectConfig
        proc.mu.Unlock()
        if pc != nil && pc.ProjectDir != "" {
            return filepath.Join(pc.ProjectDir, ".rnix", "data", "steps", pidStr, "steps.jsonl")
        }
    }
    // Fallback: 使用 kernel 的 stepDataDir 或默认
    base := s.kern.GetStepDataDir()
    if base == "" {
        return ""
    }
    return filepath.Join(base, "data", "steps", pidStr, "steps.jsonl")
}
```

**注意**：`KernelImpl` 目前没有公开 `stepDataDir` 的 getter。需新增 `GetStepDataDir() string` 方法（或在 `handleGetStepDetail` 中通过现有 `stepDataDir` 字段访问，Server 持有 `*kernel.KernelImpl`，可直接访问 exported 方法）。

#### 4. process-meta.json 读取

process-meta.json 由 reap.go 写入，结构：
```json
{
  "system_prompt": "...",
  "tool_defs": [{"name": "...", "description": "...", "parameters": {...}}]
}
```

读取代码：
```go
type processMetaFile struct {
    SystemPrompt string    `json:"system_prompt"`
    ToolDefs     []vfs.ToolDef `json:"tool_defs"`
}

func readProcessMeta(path string) (*processMetaFile, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return nil, err
    }
    var meta processMetaFile
    if err := json.Unmarshal(data, &meta); err != nil {
        return nil, err
    }
    return &meta, nil
}
```

**注意**：reap.go 中 ToolDefs 字段类型为 `[]any`（通过 `td := toolDefs[i]` 赋值，实际写入的是 `vfs.ToolDef` 结构体的 JSON 序列化）。反序列化为 `[]vfs.ToolDef` 可正常工作，因为 json tag 对齐。

#### 5. Messages 转换（json.RawMessage → []MessageWire）

StepRecord.Messages 是 `json.RawMessage`，存储的是 `[]context.Message` 的 JSON 序列化。

转换步骤：
```go
import rnixctx "github.com/rnixai/rnix/context"

var msgs []rnixctx.Message
if err := json.Unmarshal(rec.Messages, &msgs); err != nil {
    // fallback: 返回空 Messages
    msgs = nil
}
wireMessages := make([]MessageWire, len(msgs))
for i, m := range msgs {
    wm := MessageWire{
        Role:       string(m.Role),
        Content:    m.Content,
        ToolCallID: m.ToolCallID,
    }
    if len(m.ToolCalls) > 0 {
        wm.ToolCalls = make([]ToolCallWire, len(m.ToolCalls))
        for j, tc := range m.ToolCalls {
            wm.ToolCalls[j] = ToolCallWire{
                ID:    tc.ID,
                Name:  tc.Name,
                Input: tc.Input,
            }
        }
    }
    wireMessages[i] = wm
}
```

#### 6. ToolDef → ToolDefWire 转换

```go
func toolDefsToWire(defs []vfs.ToolDef) []ToolDefWire {
    if defs == nil {
        return []ToolDefWire{}
    }
    wires := make([]ToolDefWire, len(defs))
    for i, d := range defs {
        wires[i] = ToolDefWire{
            Name:        d.Name,
            Description: d.Description,
            Parameters:  d.Parameters,
        }
    }
    return wires
}
```

### 现有代码关键位置

| 文件 | 作用 | 行号范围 |
|------|------|----------|
| `ipc/protocol.go` | Method 常量定义 | L18-53 |
| `ipc/protocol.go` | ProgressPayload（参考字段风格） | L309-335 |
| `ipc/server.go` | handleConn switch 分发 | L282-363 |
| `ipc/server.go` | handleLineage（非 streaming handler 模板） | L497-531 |
| `ipc/client.go` | CtxProfile（客户端方法模板） | L73-84 |
| `ipc/client.go` | call/sendRequest/readResponse | L423-459 |
| `kernel/step_writer.go` | ReadStep 函数 | L68-91 |
| `kernel/kernel.go` | GetProcess 方法 | L2553-2555 |
| `kernel/kernel.go` | stepDataDir 字段 | L243 |
| `kernel/kernel.go` | SetStepDataDir | L268-270 |
| `kernel/process.go` | FinalSystemPrompt/stepWriter/nativeToolDefs | L105-112 |
| `kernel/reap.go` | process-meta.json 写入 | L61-93 |
| `internal/types/step_record.go` | StepRecord 类型 | L14-29 |

### 现有类型参考

**StepRecord**（`internal/types/step_record.go`）：
```go
type StepRecord struct {
    Step           int             `json:"step"`
    Timestamp      time.Duration   `json:"timestamp"`
    Messages       json.RawMessage `json:"messages"`       // []context.Message 序列化
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

**context.Message**（`context/context.go`）：
```go
type Message struct {
    Role       Role       `json:"role"`
    Content    string     `json:"content"`
    ToolCallID string     `json:"tool_call_id,omitempty"`
    ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
}
```

**context.ToolCall**（`context/context.go`）：
```go
type ToolCall struct {
    ID    string         `json:"id"`
    Name  string         `json:"name"`
    Input map[string]any `json:"input,omitempty"`
}
```

**vfs.ToolDef**（`vfs/vfs.go`）：
```go
type ToolDef struct {
    Name        string         `json:"name"`
    Description string         `json:"description,omitempty"`
    Parameters  map[string]any `json:"parameters,omitempty"`
}
```

**ErrorPayload**（`ipc/protocol.go`）：
```go
type ErrorPayload struct {
    Code    string `json:"code"`
    Message string `json:"message"`
}
```

**Server 结构体持有 `kern *kernel.KernelImpl`**，可直接调用 `s.kern.GetProcess(pid)` 和访问 exported 方法。

### 并发安全模型

- **读取 Process 字段**：通过 `proc.mu.Lock()` 读取 `FinalSystemPrompt` 和 `nativeToolDefs`，然后立即 unlock
- **读取 steps.jsonl**：使用独立 `os.Open` + `bufio.Scanner`（`kernel.ReadStep` 函数），不复用 StepWriter 的 file handle
- **NDJSON append-only 语义**：reader 看到的行要么完整要么不存在，无需跨组件加锁
- **进程已被 reap**：从磁盘文件读取，无并发问题

### 需要新增的 Kernel 公开方法

`kernel.KernelImpl` 需要新增：

```go
func (k *KernelImpl) GetStepDataDir() string {
    return k.stepDataDir
}
```

或者 Server 在初始化时缓存 stepDataDir。评估后推荐直接在 KernelImpl 上加 getter，因为 `stepDataDir` 已有 setter (`SetStepDataDir`)。

### 防御式编程要点

1. `rec.Messages` 为 nil 或空时，`Messages` 字段返回空切片 `[]MessageWire{}`
2. `nativeToolDefs` 为 nil 时，`Tools` 字段返回空切片 `[]ToolDefWire{}`
3. ToolDuration 从 `time.Duration` 转换为 `float64` 毫秒：`float64(rec.ToolDuration.Microseconds()) / 1000.0`
4. process-meta.json 中 `tool_defs` 可能为 null（进程不使用 native tools 时），需处理
5. ReadStep 的 path 为空字符串时（baseDir 未知），直接返回 not_found

### 不需要做的事情

- 不需要新增任何文件（所有修改都在现有文件中）
- 不需要改动 StepRecord 类型
- 不需要改动 StepWriter
- 不需要改动 reasonStep 循环
- 不需要改动 reap 逻辑
- ProgressPayload 的 HasError/DurationMs 字段扩展留给 Story 27.3

### Story 27.1 完成情况（前序 Story 要点）

- StepRecord 类型已实现，Messages 使用 `json.RawMessage` 避免循环导入
- StepWriter 64KB buffered NDJSON writer 已就位
- ReadStep(path, targetStep) 辅助函数已实现（顺序扫描，1MB max line buffer）
- Process.FinalSystemPrompt 和 Process.stepWriter 字段已添加
- reaper 写入 process-meta.json（system_prompt + tool_defs）已实现
- `make all` 22 个测试包全部通过

### Git 近期提交参考

```
9cf1d28 feat: Finalize Story 27.1 implementation and address code review feedback
08675be feat: ds 27-1 Implement StepRecord type and StepWriter for automatic step data logging
```

### Project Structure Notes

- 无新增文件（修改 `ipc/protocol.go`、`ipc/server.go`、`ipc/client.go`、`kernel/kernel.go`）
- Wire 类型（MessageWire、ToolCallWire、ToolDefWire）定义在 `ipc/protocol.go` 中，遵循现有 wire 类型命名惯例（如 ProcInfoWire、LineageEvent、AlertWire）
- 已存在 `ForkContinueMessage` 类型（Role/Content/ToolCallID 但无 ToolCalls），不复用它——语义不同且缺 ToolCalls 字段

### References

- [Architecture Decision 25: GetStepDetail](../_bmad-output/planning-artifacts/architecture/core-architectural-decisions.md#decision-25)
- [Epic 27 Story 27.2](../_bmad-output/planning-artifacts/epics/epic-27-统一观察系统-unified-observation-system.md)
- [Story 27.1 实现记录](../_bmad-output/implementation-artifacts/27-1-steprecord-type-and-disk-writer.md)
- [IPC Protocol 模式](../ipc/protocol.go)

## Dev Agent Record

### Agent Model Used

Claude claude-4.6-opus (via Cursor)

### Debug Log References

无异常，一次通过。

### Completion Notes List

- 在 `ipc/protocol.go` 新增 `MethodGetStepDetail` 常量及 6 个 Wire 类型：`GetStepDetailRequest`, `GetStepDetailResponse`, `MessageWire`, `ToolCallWire`, `ToolDefWire`
- 在 `ipc/server.go` 实现三阶段 handler：(A) 从 Process 或磁盘 process-meta.json 获取 SystemPrompt+ToolDefs, (B) 通过 kernel.ReadStep 读取步骤, (C) 组装 response 含 Messages 转换
- 辅助函数: `resolveStepsPathFromProc`, `resolveStepsPathFallback`, `readProcessMeta`, `toolDefsToWire`, `messagesToWire`
- 在 `ipc/client.go` 新增 `GetStepDetail(pid, step)` 客户端方法，遵循 CtxProfile 模式
- 在 `kernel/kernel.go` 新增 `GetStepDataDir()` getter
- 在 `kernel/process.go` 新增 `GetFinalSystemPrompt()`, `GetNativeToolDefs()`, `GetProjectConfig()` 线程安全 getter，以及 `SetFinalSystemPrompt()`, `SetNativeToolDefs()`, `Finish()` 测试辅助方法
- ATDD 测试文件已预置 14 个测试全部通过（AC-1 至 AC-8 完整覆盖）
- `make all` 通过：lint 0 issues, vet OK, 22 包全绿, build OK

### File List

- `ipc/protocol.go` — 新增 MethodGetStepDetail + 6 Wire 类型
- `ipc/server.go` — 新增 handleGetStepDetail handler + switch case + 辅助函数
- `ipc/client.go` — 新增 GetStepDetail 客户端方法
- `kernel/kernel.go` — 新增 GetStepDataDir() getter
- `kernel/process.go` — 新增 6 个 exported 方法 (getter/setter)
