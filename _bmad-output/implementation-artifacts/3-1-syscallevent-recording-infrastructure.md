# Story 3.1: SyscallEvent 记录基础设施

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a 内核开发者,
I want 每个 syscall 的入口和出口都自动记录为 SyscallEvent,
So that astrace 可以消费完整的调用链路数据。

## Acceptance Criteria

1. **SyscallEvent 结构完整** — Given `debug/event.go` 已实现，When 查看 SyscallEvent 辅助函数，Then 提供 `EmitEvent` 函数用于非阻塞写入 DebugChan，包含 nil 检查和缓冲满时丢弃策略
2. **VFS Open 事件** — Given 进程的 DebugChan 非 nil，When 调用 Open(path, flags)，Then 记录事件包含 `Syscall: "Open"`, `Args: {"path": path, "flags": flags}`, `Result: fd`, `Duration: 耗时`
3. **VFS Read 事件** — Given 进程的 DebugChan 非 nil，When 调用 Read(fd, length)，Then 记录事件包含 `Syscall: "Read"`, `Args: {"fd": fd, "length": length}`, `Result: bytesRead`, `Duration: 耗时`
4. **VFS Write 事件** — Given 进程的 DebugChan 非 nil，When 调用 Write(fd, data)，Then 记录事件包含 `Syscall: "Write"`, `Args: {"fd": fd, "size": len(data)}`, `Duration: 耗时`
5. **VFS Close 事件** — Given 进程的 DebugChan 非 nil，When 调用 Close(fd)，Then 记录事件包含 `Syscall: "Close"`, `Args: {"fd": fd}`, `Duration: 耗时`
6. **VFS Stat 事件** — N/A：kernel 层当前不调用 `k.vfs.Stat()`，故无事件记录点。VFS 接口有 Stat 方法但未在 kernel syscall 路径中使用。若未来添加 Stat syscall，需补充事件记录。
7. **CtxAlloc 事件** — Given 进程的 DebugChan 非 nil，When 调用 CtxAlloc(size)，Then 记录事件包含 `Syscall: "CtxAlloc"`, `Args: {"size": size}`, `Result: ctxID`, `Duration: 耗时`
8. **CtxRead 事件** — Given 进程的 DebugChan 非 nil，When 调用 BuildPrompt(cid)，Then 记录事件包含 `Syscall: "CtxRead"`, `Args: {"cid": cid, "op": "BuildPrompt"}`, `Duration: 耗时`（注：实际 API 为 BuildPrompt 而非 CtxRead(cid, offset, length)）
9. **CtxWrite 事件** — Given 进程的 DebugChan 非 nil，When 调用 SetSystemPrompt/AppendMessage/AppendToolResult，Then 记录事件包含 `Syscall: "CtxWrite"`, `Args: {"cid": cid, "op": "<操作名>", ...}`, `Duration: 耗时`（注：实际 API 按操作类型区分，而非统一的 CtxWrite(cid, offset, data)）
10. **DebugChan 为 nil 时零开销** — Given 进程的 DebugChan 为 nil（无 astrace 附着），When syscall 执行，Then 跳过事件记录（零开销，无额外 allocation）
11. **DebugChan 缓冲满时不阻塞** — Given DebugChan 缓冲已满（256），When 写入新事件，Then 不阻塞 syscall 执行（非阻塞写入，丢弃事件）
12. **所有测试通过** — Given 实现完成，When 执行 `go test -race ./...`，Then 所有新增和现有测试通过，无竞态条件

## Tasks / Subtasks

- [x] Task 1: 创建 debug/event.go — 事件记录辅助函数 (AC: #1, #10, #11)
  - [x] 1.1 实现 `EmitEvent(ch chan types.SyscallEvent, event types.SyscallEvent)` — nil 检查 + 非阻塞写入
  - [x] 1.2 实现 `NewEvent(pid types.PID, createdAt time.Time, syscall string, args map[string]any) types.SyscallEvent` — 构造辅助函数
  - [x] 1.3 实现 `CompleteEvent(event *types.SyscallEvent, result any, err error)` — 出口处填充 Result/Err/Duration

- [x] Task 2: 创建 debug/event_test.go — 事件记录测试 (AC: #10, #11, #12)
  - [x] 2.1 TestEmitEvent_NilChannel — nil channel 不 panic
  - [x] 2.2 TestEmitEvent_Success — 正常写入
  - [x] 2.3 TestEmitEvent_BufferFull — 缓冲满时不阻塞
  - [x] 2.4 TestNewEvent_Fields — 字段正确填充
  - [x] 2.5 TestCompleteEvent_FillsFields — 补充 Result/Err/Duration
  - [x] 2.6 Test 并发安全 — 多 goroutine 并发写入 DebugChan 无竞态

- [x] Task 3: 在 kernel 层为 VFS 操作添加事件记录 (AC: #2-#6)
  - [x] 3.1 在 kernel.go 中 `k.vfs.Open()` 调用处包装事件记录
  - [x] 3.2 在 kernel.go 中 `k.vfs.Read()` 调用处包装事件记录
  - [x] 3.3 在 kernel.go 中 `k.vfs.Write()` 调用处包装事件记录
  - [x] 3.4 在 kernel.go 中 `k.vfs.Close()` 调用处包装事件记录
  - [x] 3.5 在 kernel.go 中 `k.vfs.Stat()` 调用处（如存在）包装事件记录

- [x] Task 4: 在 kernel 层为 Context 操作添加事件记录 (AC: #7-#9)
  - [x] 4.1 在 kernel.go 中 `k.ctxMgr.CtxAlloc()` 调用处包装事件记录
  - [x] 4.2 在 kernel.go 中 `k.ctxMgr.BuildPrompt()` 调用处包装事件记录（作为 CtxRead 事件）
  - [x] 4.3 在 kernel.go 中 `k.ctxMgr.AppendMessage()` / `AppendToolResult()` 调用处包装事件记录（作为 CtxWrite 事件）
  - [x] 4.4 在 kernel.go 中 `k.ctxMgr.SetSystemPrompt()` 调用处包装事件记录（作为 CtxWrite 事件）

- [x] Task 5: 重构 emitEvent — 迁移到 debug 包 (AC: #1)
  - [x] 5.1 将 `KernelImpl.emitEvent()` 逻辑迁移为调用 `debug.EmitEvent()`
  - [x] 5.2 保持 KernelImpl.emitEvent 作为便捷包装（自动填充 proc 信息），内部调用 debug 包

- [x] Task 6: 测试验证 (AC: #12)
  - [x] 6.1 更新 kernel_test.go — 验证新增的 VFS 和 Context 事件
  - [x] 6.2 `go test -race ./debug/...` 通过
  - [x] 6.3 `go test -race ./kernel/...` 通过
  - [x] 6.4 `go test -race ./...` 全量通过
  - [x] 6.5 `go vet ./...` 无警告

## Dev Notes

### 核心设计决策

**事件记录位置：在 kernel 层而非 VFS/Context 层**

事件记录代码应添加在 `kernel/kernel.go` 中调用 VFS 和 Context 操作的位置，而非注入到 VFS 或 Context 包内部。原因：

1. kernel 层持有 `Process` 引用（包含 `DebugChan` 和 `CreatedAt`）
2. VFS 和 Context 包不依赖 kernel（依赖方向：`kernel → vfs`, `kernel → context`），反向注入会违反架构边界
3. 现有的 `emitEvent()` 方法已在 kernel 层实现，保持一致
4. kernel.go 是所有 syscall 的入口点——reasonStep 循环中的每个 VFS/Context 调用都在此文件中

**debug/event.go 的角色**

`debug/event.go` 提供独立于 kernel 的事件工具函数，使得 `debug/` 包可以独立使用（符合架构依赖方向 `debug/ ← 仅依赖 internal/types/`）。这些函数被 kernel 层调用。

### 已有基础设施（无需重新实现）

**SyscallEvent 类型** — `internal/types/types.go:70-79`:

```go
type SyscallEvent struct {
    Timestamp time.Duration    // 相对进程启动
    PID       PID
    Syscall   string           // "Open", "Read", "CtxWrite" 等
    Args      map[string]any
    Result    any
    Err       error
    Duration  time.Duration
}
```

**Process.DebugChan** — `kernel/process.go:42`:

```go
DebugChan  chan types.SyscallEvent  // 缓冲 256
```

在 `NewProcess()` 中初始化：`make(chan types.SyscallEvent, 256)`

**KernelImpl.emitEvent()** — `kernel/kernel.go:193-211`:

```go
func (k *KernelImpl) emitEvent(proc *Process, syscall string, args map[string]any, result any, err error, duration time.Duration) {
    if proc.DebugChan == nil {
        return
    }
    event := types.SyscallEvent{
        Timestamp: time.Since(proc.CreatedAt),
        PID:       proc.PID,
        Syscall:   syscall,
        Args:      args,
        Result:    result,
        Err:       err,
        Duration:  duration,
    }
    select {
    case proc.DebugChan <- event:
    default: // buffer full, drop event
    }
}
```

**已有事件记录点（不要重复添加）：**
- `Spawn` 事件 — `kernel/kernel.go:165-173`
- `ReasonStep` 事件（多处：上下文取消、提示构建错误、LLM 调用、文本输出、权限拒绝、工具调用）

### VFS 操作调用位置（需要添加事件记录的代码行）

在 `kernel/kernel.go` 中，以下是当前 VFS 调用位置：

| 操作 | 位置 | 事件名 |
|------|------|--------|
| `k.vfs.Open()` | ~第 149 行（Spawn 中打开 LLM 设备），~第 375 行（工具调用打开设备） | "Open" |
| `k.vfs.Write()` | ~第 296 行（LLM 写入请求），~第 382 行（工具调用写入） | "Write" |
| `k.vfs.Read()` | ~第 306 行（LLM 读取响应），~第 389 行（工具调用读取） | "Read" |
| `k.vfs.Close()` | ~第 396 行（工具调用关闭） | "Close" |
| `k.vfs.CloseAll()` | ~第 180 行（进程退出清理） | "CloseAll" |

### Context 操作调用位置（需要添加事件记录的代码行）

| 操作 | 位置 | 事件名 |
|------|------|--------|
| `k.ctxMgr.CtxAlloc()` | ~第 128 行 | "CtxAlloc" |
| `k.ctxMgr.SetSystemPrompt()` | ~第 136 行 | "CtxWrite" |
| `k.ctxMgr.AppendMessage()` | ~第 143 行，~第 344 行 | "CtxWrite" |
| `k.ctxMgr.BuildPrompt()` | ~第 266 行 | "CtxRead" |
| `k.ctxMgr.AppendToolResult()` | ~第 399 行 | "CtxWrite" |

### 事件记录模式（统一用法）

每个 syscall 调用点使用如下模式：

```go
// Open 示例
start := time.Now()
fd, err := k.vfs.Open(proc.PID, path, flags)
k.emitEvent(proc, "Open", map[string]any{
    "path":  path,
    "flags": flags,
}, fd, err, time.Since(start))
```

**关键规则：**
- `time.Now()` 在调用前获取，`time.Since(start)` 在调用后计算
- Args 中不传递大型数据（如 `data []byte`），传递 `"size": len(data)` 代替
- Syscall 名称与接口方法名完全一致（PascalCase）
- Result 传递有意义的返回值（FD、CtxID、字节数等）

### debug/event.go 设计

```go
package debug

import (
    "time"
    "github.com/rnixai/rnix/internal/types"
)

// EmitEvent 非阻塞地将事件写入 DebugChan。
// 如果 ch 为 nil 或缓冲已满，不阻塞不 panic。
func EmitEvent(ch chan<- types.SyscallEvent, event types.SyscallEvent) {
    if ch == nil {
        return
    }
    select {
    case ch <- event:
    default: // buffer full, drop event
    }
}

// NewEvent 构造 SyscallEvent，填充入口侧字段（Timestamp, PID, Syscall, Args）。
// Result, Err, Duration 留空，由 CompleteEvent 在 syscall 返回后填充。
func NewEvent(pid types.PID, createdAt time.Time, syscall string, args map[string]any) types.SyscallEvent {
    return types.SyscallEvent{
        Timestamp: time.Since(createdAt),
        PID:       pid,
        Syscall:   syscall,
        Args:      args,
    }
}

// CompleteEvent 填充出口侧字段：Result, Err, Duration。
func CompleteEvent(event *types.SyscallEvent, result any, err error, duration time.Duration) {
    event.Result = result
    event.Err = err
    event.Duration = duration
}
```

### 前序 Story 经验

**Story 2.6 关键经验（最近完成的 Story）：**
- 路径逃逸防护 — AgentLoader/SkillLoader 添加了 absPath containment check
- 依赖图准确性 — 确保 project-context.md 反映真实依赖
- parseSKILLMD 增加 extractBody 参数优化性能 — 类似的"按需"思路适用于事件记录
- Spawn debug 事件已增加 `allowed_devices` 字段

**Git 近期提交模式：**
```
fb0c76b Update sprint status for Epic 2 to 'done'
06d9eeb Update project context and finalize Story 2.6
56a977a Update Story 2.6 status to 'review'
```
- 提交消息风格：英文动词短语开头
- 代码风格：Go 标准惯例，testify assertions

### NFR 合规

| NFR | 要求 | 本 Story 影响 |
|-----|------|-------------|
| NFR3 | astrace 输出延迟 ≤ 500ms | 事件记录是 astrace 数据源的基础，延迟由 DebugChan 缓冲保证 |
| NFR8 | 退出 10s 内释放资源 | DebugChan 关闭在现有资源释放顺序中（无需修改） |
| NFR18 | 通过 go vet 和 golint 无警告 | 新增代码须符合 |

### 依赖方向验证

```
debug/event.go ← 仅依赖 internal/types/（✅ 合规）
kernel/kernel.go → debug/（✅ 合规，kernel 可导入 debug）
```

注意：debug 包不导入 kernel 包。kernel 层调用 debug 包的辅助函数，传入 DebugChan 和进程信息。

### 范围边界

**本 Story 包含：**
- `debug/event.go` + `debug/event_test.go` — 事件记录辅助函数
- 在 `kernel/kernel.go` 中为所有 VFS 和 Context 操作添加 `emitEvent` 调用
- 重构现有 `emitEvent()` 使用 debug 包

**本 Story 不包含：**
- astrace 事件消费和格式化（Story 3.2）
- astrace CLI 命令（Story 3.3）
- Syscall Trace Line UI 组件（Story 3.4）
- DebugChan 的创建/关闭逻辑修改（已在 Process 生命周期中正确处理）

### Project Structure Notes

**新建文件：**
```
debug/event.go          — SyscallEvent 记录辅助函数（EmitEvent、RecordSyscall）
debug/event_test.go     — 事件记录单元测试
```

**修改文件：**
```
kernel/kernel.go        — 在 VFS/Context 调用处添加 emitEvent，重构 emitEvent 使用 debug 包
kernel/kernel_test.go   — 新增事件验证测试用例
```

**删除文件：**
```
debug/.gitkeep          — 被实际文件替代
```

### References

- [Source: _bmad-output/planning-artifacts/epics.md#Story 3.1] — Story 定义和验收标准
- [Source: _bmad-output/planning-artifacts/architecture.md#Decision 5: 调试架构] — SyscallEvent 结构和 DebugChan 设计
- [Source: _bmad-output/planning-artifacts/architecture.md#通信模式] — Channel 使用规则
- [Source: _bmad-output/project-context.md#SyscallEvent 记录] — 所有 syscall 入口/出口必须写入 SyscallEvent
- [Source: internal/types/types.go:70-79] — SyscallEvent 类型定义
- [Source: kernel/kernel.go:193-211] — 现有 emitEvent() 方法
- [Source: kernel/process.go:42] — Process.DebugChan 字段
- [Source: kernel/kernel.go:165-173] — 现有 Spawn 事件记录
- [Source: kernel/kernel.go:128-180] — Spawn 中的 VFS/Context 调用
- [Source: kernel/kernel.go:250-410] — reasonStep 中的 VFS/Context 调用
- [Source: _bmad-output/implementation-artifacts/2-6-agent-abstraction-and-skill-standardization.md] — 前序 Story 经验

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6

### Debug Log References

### Completion Notes List

- Task 1: 创建 `debug/event.go`，实现 `EmitEvent`（nil 检查 + 非阻塞 select/default）、`NewEvent`（构造辅助函数，填充 Timestamp/PID/Syscall/Args）、`CompleteEvent`（填充 Result/Err/Duration）
- Task 2: 创建 `debug/event_test.go`，6 个测试覆盖 nil channel、正常写入、缓冲满不阻塞、字段验证、CompleteEvent 填充、100 goroutine 并发安全
- Task 3: 在 `kernel/kernel.go` 中为所有 VFS 操作（Open×2、Write×2、Read×2、Close×1）添加 `emitEvent` 调用，每个调用记录 start time 和 duration。Stat 不存在调用故跳过
- Task 4: 在 `kernel/kernel.go` 中为所有 Context 操作添加事件记录：CtxAlloc、SetSystemPrompt（CtxWrite）、AppendMessage（CtxWrite）×3、BuildPrompt（CtxRead）、AppendToolResult（CtxWrite）×2
- Task 5: 重构 `KernelImpl.emitEvent()`，内部调用 `debug.NewEvent` + `debug.CompleteEvent` + `debug.EmitEvent`，保持便捷包装接口不变
- Task 6: 新增 4 个 kernel 测试（TestSpawn_VFSEvents_OpenWriteRead、TestSpawn_ContextEvents_CtxAllocCtxWriteCtxRead、TestToolCall_VFSAndContextEvents、TestNilDebugChan_ZeroOverhead），全量 `go test -race ./...` 和 `go vet ./...` 通过

### Change Log

- 2026-02-25: Story 3.1 实现完成 — SyscallEvent 记录基础设施
- 2026-02-25: Code Review 修复 — 恢复 emitEvent nil 检查（H1），添加 benchmark 测试（H2），更新 AC #6/#8/#9 描述（M1-M3），补充 File List（M4），增强 VFS 事件测试断言（M5），更新 Dev Notes 设计文档（L2）

### File List

- debug/event.go (新建)
- debug/event_test.go (新建)
- kernel/kernel.go (修改)
- kernel/kernel_test.go (修改)
- _bmad-output/implementation-artifacts/sprint-status.yaml (修改)
