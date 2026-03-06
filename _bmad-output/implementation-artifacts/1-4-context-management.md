# Story 1.4: 上下文管理

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a 智能体,
I want 拥有独立的上下文空间来累积对话历史和工具结果,
So that 每轮 LLM 调用都能获得完整的推理上下文。

## Acceptance Criteria

1. **CtxAlloc 分配上下文** — Given `context/context.go` 已实现，When 调用 `CtxAlloc(size)`，Then 返回唯一的 `CtxID`，分配指定大小的上下文空间
2. **CtxWrite 写入上下文** — Given 上下文已分配，When 调用 `CtxWrite(cid, offset, data)`，Then 数据写入上下文指定位置，And 调用 `CtxRead(cid, offset, length)` 可读回写入的数据
3. **BuildPrompt 组装** — Given 上下文包含 system prompt、对话历史和工具结果，When 调用 `BuildPrompt(cid)`，Then 按正确顺序组装完整的 LLM prompt（system prompt + 历史消息 + 最新工具结果），And 组装时间 ≤ 1 秒（NFR5）
4. **CtxFree 释放** — Given 上下文已分配，When 调用 `CtxFree(cid)`，Then 上下文空间释放，后续 Read/Write 该 CtxID 返回错误

## Tasks / Subtasks

- [x] Task 1: 定义上下文核心类型与消息模型 (AC: #1, #2, #3)
  - [x] 1.1 在 `context/context.go` 中定义 `Role` 类型和常量（`RoleSystem`、`RoleUser`、`RoleAssistant`、`RoleTool`）
  - [x] 1.2 定义 `Message` 结构体（`Role Role`、`Content string`、`ToolCallID string`（可选，工具结果关联用））
  - [x] 1.3 定义 `Context` 结构体（`ID types.CtxID`、`SystemPrompt string`、`Messages []Message`、`MaxSize int`（上下文容量上限）、`mu sync.RWMutex`）
  - [x] 1.4 定义 `PromptResult` 结构体（`SystemPrompt string`、`Messages []Message`）— BuildPrompt 的返回值，供 LLM 驱动消费
- [x] Task 2: 实现 ContextManager 上下文管理器 (AC: #1, #4)
  - [x] 2.1 定义 `Manager` 结构体（`contexts *xsync.SyncMap[types.CtxID, *Context]`、`nextID atomic.Uint64`）
  - [x] 2.2 实现 `NewManager() *Manager`
  - [x] 2.3 实现 `CtxAlloc(size int) (types.CtxID, error)`：分配唯一 CtxID（`atomic.Uint64` 递增，从 1 开始），创建 Context 存入 SyncMap，返回 CtxID
  - [x] 2.4 实现 `CtxFree(cid types.CtxID) error`：从 SyncMap 删除，后续访问返回错误
  - [x] 2.5 实现内部 `getContext(cid) (*Context, error)` 辅助方法：查找 Context，未找到返回带 `ErrNotFound` 的错误
- [x] Task 3: 实现 CtxRead/CtxWrite 操作 (AC: #2)
  - [x] 3.1 实现 `CtxWrite(cid types.CtxID, offset int, data []byte) error`：将原始字节数据写入上下文。offset 语义：0=追加消息，其他值=特定位置覆写（MVP 阶段主要使用追加模式）
  - [x] 3.2 实现 `CtxRead(cid types.CtxID, offset int, length int) ([]byte, error)`：读取上下文内容的原始字节表示
  - [x] 3.3 实现高层便利方法 `SetSystemPrompt(cid types.CtxID, prompt string) error`：设置/更新 system prompt
  - [x] 3.4 实现高层便利方法 `AppendMessage(cid types.CtxID, role Role, content string) error`：追加对话消息
  - [x] 3.5 实现高层便利方法 `AppendToolResult(cid types.CtxID, toolCallID string, content string) error`：追加工具执行结果
- [x] Task 4: 实现 BuildPrompt 组装 (AC: #3)
  - [x] 4.1 实现 `BuildPrompt(cid types.CtxID) (*PromptResult, error)`：读取 Context 中的 SystemPrompt 和 Messages，按正确顺序组装
  - [x] 4.2 组装顺序：system prompt 单独提取 → Messages 按追加顺序排列（用户意图 → LLM 响应 → 工具结果 → LLM 响应 → ...循环）
  - [x] 4.3 验证组装性能：≤ 1 秒（NFR5），应该轻松满足（纯内存操作）
- [x] Task 5: 编写完整单元测试 (AC: all)
  - [x] 5.1 `context/context_test.go` — CtxAlloc 测试：分配返回递增 CtxID、size 参数正确存储
  - [x] 5.2 CtxWrite/CtxRead 测试：写入后可读回、offset 语义正确
  - [x] 5.3 高层方法测试：SetSystemPrompt、AppendMessage、AppendToolResult 正确追加
  - [x] 5.4 BuildPrompt 测试：组装顺序正确（system prompt + messages 按追加顺序）、空上下文返回空结果
  - [x] 5.5 CtxFree 测试：释放后 Read/Write/BuildPrompt 返回错误
  - [x] 5.6 并发安全测试：多 goroutine 并发 Alloc/Write/Read/Free，通过 `-race` 检测
  - [x] 5.7 全量回归 `go test -race ./...` 确保不破坏已有测试

## Dev Notes

### 架构模式与约束

- **文件位置严格遵循架构文档：** `context/context.go` 是唯一实现文件，包含所有上下文管理逻辑
- **依赖方向：** `context/` → `internal/types/` ✓；`context/` → `internal/xsync/` ✓。**绝对禁止** `context/` 导入 `kernel/` 或 `vfs/`
- **此 Story 实现的核心：** CtxAlloc/CtxRead/CtxWrite/CtxFree + BuildPrompt + 消息模型
- **此 Story 不实现：** reasonStep 中的上下文调用（Story 1.6）、进程退出时的 CtxFree 调用（Story 4.5）、token 预算控制（Phase 2）
- **Go 包名注意：** Go 标准库有 `context` 包。本项目的 `context/` 包路径是 `github.com/rnixai/rnix/context`，完整限定路径避免与标准库冲突。但包内代码不能使用标准库 `context.Context` 时直接写 `context.Context`——**需要使用 `stdctx "context"` 别名导入**，或者如果本包不需要标准库 context 则无需关注

### 已有代码（必须复用，禁止重新实现）

**`internal/types/types.go` — 已定义的类型：**

```go
type CtxID uint64        // 上下文 ID — CtxAlloc 的核心返回值
type PID uint64           // 进程 ID
type ErrCode string       // 错误码

const (
    ErrTimeout    ErrCode = "TIMEOUT"
    ErrNotFound   ErrCode = "NOT_FOUND"    // 用于已释放的上下文
    ErrPermission ErrCode = "PERMISSION"
    ErrInternal   ErrCode = "INTERNAL"
    ErrDriver     ErrCode = "DRIVER"
)
```

**`internal/xsync/syncmap.go` — Manager 的基础：**

```go
type SyncMap[K comparable, V any] struct { mu sync.RWMutex; m map[K]V }
func NewSyncMap[K, V]() *SyncMap[K, V]
func (s *SyncMap[K, V]) Load/Store/Delete/Range/Len/LoadOrStore/LoadAndDelete
```

Manager 必须使用 `SyncMap[types.CtxID, *Context]` 管理上下文映射，禁止手写 `map + mutex`。

**`kernel/errors.go` — SyscallError（context 包不能直接使用）：**

```go
type SyscallError struct {
    Syscall string; PID types.PID; Device string; Err error; Code types.ErrCode
}
```

context 包不能导入 kernel 包。遵循 VFS 的模式：定义自己的 `ContextError` 类型，或返回标准 error 由 kernel 层包装。**推荐定义 `ContextError`**——与 VFS 的 `VFSError` 模式一致。

**`vfs/vfs.go` — VFSError 模式参考：**

```go
type VFSError struct {
    Op     string        // 操作名："CtxAlloc", "CtxRead", ...
    PID    types.PID     // 所属进程（0 表示非进程关联操作）
    Device string        // 设备路径（context 场景为空）
    Err    error
    Code   types.ErrCode
}
```

context 包应定义类似的 `ContextError`：

```go
type ContextError struct {
    Op   string        // "CtxAlloc", "CtxRead", "CtxWrite", "CtxFree", "BuildPrompt"
    CID  types.CtxID   // 关联的上下文 ID
    Err  error
    Code types.ErrCode
}
```

**`kernel/process.go` — Process 结构体（当前无 Ctx 字段）：**

```go
type Process struct {
    PID       types.PID
    PPID      types.PID
    State     types.ProcessState
    Intent    string
    Skills    []string
    Children  []types.PID
    FDTable   map[types.FD]vfs.VFSFile
    DebugChan chan types.SyscallEvent
    Done      chan ExitStatus
    CreatedAt time.Time
    Exit      *ExitStatus
    mu        sync.Mutex
    cancel    context.CancelFunc
    wg        sync.WaitGroup
}
```

注意：Process 结构体中**目前没有 Ctx 字段**。架构文档定义 `Ctx *Context`，但由于 `kernel/` 导入 `context/` 包时会与 Go 标准库 `context` 冲突，需要在 Process 中存储 `CtxID` 而非 `*Context` 指针。**建议添加 `CtxID types.CtxID` 字段**，在 Story 1.6（Spawn + reasonStep）中连接。本 Story 不修改 Process 结构体——仅实现 context 包本身。

### 关键设计决策

**1. 上下文数据模型——消息列表 vs 原始字节**

架构文档的 CtxRead/CtxWrite 接口使用 `offset + []byte` 语义（类似文件 I/O）。但上下文的实际使用场景是：
- 追加对话消息（user/assistant/tool role）
- 设置 system prompt
- 组装为 LLM prompt

**决策：双层 API**
- **底层**：`CtxRead(cid, offset, length) / CtxWrite(cid, offset, data)` — 满足架构 ABI 契约，操作序列化的字节表示
- **高层**：`SetSystemPrompt / AppendMessage / AppendToolResult / BuildPrompt` — 面向实际使用的结构化 API
- **底层 API 实现**：CtxWrite offset=0 时追加消息（data 为 JSON 序列化的 Message），CtxRead 读取整个上下文的 JSON 序列化内容
- reasonStep 循环（Story 1.6）将主要使用高层 API

**2. 与 Claude Code CLI 的对接**

Claude Code CLI 调用模板：
```go
cmd := exec.CommandContext(ctx, "claude", "-p", intent,
    "--output-format", "json",
    "--system-prompt", skillInstructions,
    "--model", model,
    "--max-turns", "1",
)
```

BuildPrompt 的职责是组装出 `--system-prompt` 参数内容和 `-p` 参数内容。MVP 阶段 `--max-turns 1` 意味着每次 CLI 调用是单轮对话，上下文管理器负责跨轮次维护完整的对话历史。

**PromptResult 结构：**
```go
type PromptResult struct {
    SystemPrompt string    // → --system-prompt 参数
    Messages     []Message // → -p 参数（拼接最新用户消息 + 历史摘要）
}
```

**3. CtxID 分配策略**

与 PID 分配一致：`atomic.Uint64` 全局递增，从 1 开始，不回收。Manager 内部维护计数器。

**4. 并发安全策略**

- Manager 级别：`SyncMap[CtxID, *Context]`（已解决）
- Context 级别：每个 Context 内部 `sync.RWMutex` 保护 Messages 切片
  - `AppendMessage/CtxWrite` 使用写锁
  - `CtxRead/BuildPrompt` 使用读锁
  - `SetSystemPrompt` 使用写锁

**5. MaxSize / 上下文容量**

CtxAlloc 的 size 参数设置上下文容量上限。MVP 阶段不做严格的 token 计算（Phase 2 的 `--max-budget-usd` 负责），但 size 用于限制 Messages 数量或总字节数，防止无限增长。当达到上限时 CtxWrite 返回错误（ErrCode 使用 `ErrInternal`，错误信息 "context full"）。

### Go 代码命名规则（必须遵循）

| 对象 | 规则 | 示例 |
|------|------|------|
| 包名 | 全小写 | `context` |
| 导出类型 | PascalCase | `Manager`, `Context`, `Message`, `Role`, `PromptResult`, `ContextError` |
| 非导出类型 | camelCase | 无（本包类型较少） |
| 导出函数 | PascalCase | `NewManager`, `CtxAlloc`, `CtxRead`, `CtxWrite`, `CtxFree`, `BuildPrompt` |
| 方法接收器 | 简短 | `m *Manager`、`c *Context` |
| 常量 | PascalCase | `RoleSystem`, `RoleUser`, `RoleAssistant`, `RoleTool` |
| 文件名 | 下划线分隔 | `context.go`, `context_test.go` |

### 错误处理模式

定义 `ContextError` 类型，与 VFS 的 `VFSError` 模式保持一致：

```go
type ContextError struct {
    Op   string
    CID  types.CtxID
    Err  error
    Code types.ErrCode
}

func (e *ContextError) Error() string {
    return fmt.Sprintf("[%s] CtxID %d %s: %v", e.Code, e.CID, e.Op, e.Err)
}

func (e *ContextError) Unwrap() error {
    return e.Err
}
```

所有 context 操作的错误必须包装为 `*ContextError`：

```go
// 正确：完整包装
return 0, &ContextError{Op: "CtxAlloc", CID: 0, Err: fmt.Errorf("..."), Code: types.ErrInternal}

// 正确：上下文已释放
return nil, &ContextError{Op: "CtxRead", CID: cid, Err: fmt.Errorf("context not found"), Code: types.ErrNotFound}

// 错误：返回裸 error
return nil, fmt.Errorf("not found")  // ← 禁止
```

### 测试规范

**测试文件位置：** `context/context_test.go`

**必须包含的测试场景：**

| 测试 | 验证点 |
|------|--------|
| `TestManager_CtxAlloc` | 分配返回递增 CtxID（1, 2, 3...）、size 正确记录 |
| `TestManager_CtxAllocMultiple` | 多次分配互不冲突 |
| `TestManager_CtxFree` | 释放后 Read/Write/BuildPrompt 返回 ErrNotFound |
| `TestManager_CtxFreeNotFound` | 释放不存在的 CtxID 返回 ErrNotFound |
| `TestManager_CtxWriteRead` | 写入后可读回原始数据 |
| `TestManager_SetSystemPrompt` | 设置/更新 system prompt |
| `TestManager_AppendMessage` | 追加多条不同角色的消息 |
| `TestManager_AppendToolResult` | 追加工具结果，正确关联 toolCallID |
| `TestManager_BuildPrompt` | 组装顺序正确：system prompt 独立 + messages 按追加顺序 |
| `TestManager_BuildPromptEmpty` | 空上下文返回空 PromptResult |
| `TestManager_BuildPromptPerformance` | 大量消息时组装时间 ≤ 1 秒 |
| `TestManager_ConcurrentAccess` | 多 goroutine 并发 Alloc/Write/Read/Free |

**测试模式（对齐 Story 1-2/1-3 风格）：**
- 使用 Go 标准 `testing` 包，`t.Run` 子测试
- 使用 `t.Fatal` / `t.Fatalf` / `t.Errorf`
- 并发测试使用 `sync.WaitGroup`
- 全部通过 `go test -race ./context/...`

### 前序 Story 经验教训（必须吸收）

1. **data race 敏感：** Story 1-2 的 Future[T] 和 Process 状态机因并发问题返工。Context 的 Messages 切片必须从一开始就用 RWMutex 保护
2. **VFSError 模式成功：** Story 1-3 定义了 VFSError 替代直接使用 kernel.SyscallError，保持依赖方向干净。context 包应采用相同策略定义 ContextError
3. **测试使用 `t.Logf` 不用 `fmt.Printf`**
4. **使用 `slices.Contains` 等标准库简化代码**
5. **SyncMap 的 LoadOrStore 和 LoadAndDelete 方法已可用**（Story 1-3 新增）
6. **Story 1-3 的 TOCTOU 修复：** 原子操作很重要——CtxFree 应使用 `SyncMap.LoadAndDelete` 原子删除，避免 check-then-delete 竞态
7. **closeAll 模式：** Story 1-3 的 VFS.CloseAll 先在锁内收集引用再释放锁后清理。类似模式适用于 CtxFree

### Git 智能（最近工作模式）

**最近 5 个提交分析：**

| 提交 | 内容 | 启示 |
|------|------|------|
| `6ba2532` | Story 1-3 VFS 实现完成 | VFSError 模式、fdTable 并发管理模式是模板 |
| `8cc10a0` | Story 1-3 文档更新 | 文档与代码分离提交 |
| `a6cccfa` | Story 1-2 实现完成 | Process 状态机、SyncMap 使用模式 |
| `0d83562` | Story 1-2 文档更新 | 同上 |
| `4aac131` | project-context.md | 75 条规则文档 |

**代码惯例提取：**
- 包级文档注释：`// Package context implements the context management layer for Rnix.`
- 构造函数：`NewManager()` 模式
- 方法接收器：简短单字母（`m *Manager`、`c *Context`）
- 测试分组：`t.Run("子测试名", func(t *testing.T) {...})`
- 导入分组：标准库 → 空行 → 项目内部包

### Project Structure Notes

**本 Story 新增的文件：**

```
context/
├── context.go          (新增 — Task 1-4)
└── context_test.go     (新增 — Task 5)
```

**不要创建的文件：**
- `context/types.go` — 类型统一放在 `context/context.go` 中（文件数少时不需要拆分）
- `context/manager.go` — Manager 与 Context 放同一文件
- `context/prompt.go` — BuildPrompt 放在 `context.go` 中
- 不要修改 `kernel/process.go` — CtxID 字段将在 Story 1.6 添加

**不要触碰的文件：**
- `kernel/` 下任何文件
- `vfs/` 下任何文件
- `internal/types/types.go`（CtxID 已定义，无需修改）
- `internal/xsync/` 下任何文件（API 已满足需求）

### References

- [Source: architecture.md#Decision 1: Syscall ABI 设计风格] — ContextManager 接口定义（CtxAlloc/CtxRead/CtxWrite/CtxFree）
- [Source: architecture.md#Project Structure & Boundaries] — context/ 包位置、Kernel ↔ Context 边界
- [Source: architecture.md#Implementation Patterns > 命名模式] — Go 代码命名规则
- [Source: architecture.md#Implementation Patterns > 结构模式] — 依赖方向：context/ 不导入 kernel/
- [Source: architecture.md#Implementation Patterns > 过程模式 > context.Context 传播规则] — Kernel 方法不接受 ctx
- [Source: epics.md#Story 1.4] — 原始用户故事和验收标准
- [Source: prd.md#上下文管理（FR19-FR22）] — 功能需求定义
- [Source: prd.md#NFR5] — 上下文组装时间 ≤ 1 秒
- [Source: 1-3-vfs-framework-and-device-registration.md] — 前序 Story 产出、VFSError 模式、经验教训
- [Source: project-context.md#架构框架规则] — ContextManager 接口、依赖方向
- [Source: project-context.md#关键防错规则] — 禁止裸 error、禁止反向依赖、禁止手写 map+mutex

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6 (claude-opus-4-6)

### Debug Log References

### Completion Notes List

- ✅ Task 1: 在 `context/context.go` 中定义了 `Role` 类型及 4 个常量、`Message` 结构体（含 JSON tag）、`Context` 结构体（含 `sync.RWMutex` 保护并发）、`PromptResult` 结构体、`ContextError` 类型（含 `Error()` 和 `Unwrap()` 方法，与 VFSError 模式一致）
- ✅ Task 2: 实现了 `Manager` 结构体，使用 `xsync.SyncMap[types.CtxID, *Context]` 管理上下文映射。`CtxAlloc` 使用 `atomic.Uint64` 递增分配 ID（从 1 开始），`CtxFree` 使用 `LoadAndDelete` 原子删除避免 TOCTOU 竞态
- ✅ Task 3: 实现了底层 `CtxWrite`（offset=0 追加 JSON Message，非零 offset 覆写指定位置）和 `CtxRead`（读取上下文 JSON 序列化内容）。实现了高层便利方法 `SetSystemPrompt`、`AppendMessage`、`AppendToolResult`，全部使用 `sync.RWMutex` 保护并发安全
- ✅ Task 4: 实现了 `BuildPrompt`，按追加顺序组装 SystemPrompt + Messages，返回 `*PromptResult` 的深拷贝。10000 条消息的组装性能测试通过（远低于 1 秒）
- ✅ Task 5: 编写了 12 个测试函数覆盖所有 AC：CtxAlloc 递增 ID + size 验证、CtxAllocMultiple 互不冲突、CtxFree 释放后返回 ErrNotFound、CtxFreeNotFound、CtxWriteRead 写入读回 + JSON 验证 + 容量上限、SetSystemPrompt 设置/更新/释放后错误、AppendMessage 多角色追加 + 满容量、AppendToolResult 关联 toolCallID、BuildPrompt 组装顺序验证、BuildPromptEmpty 空上下文、BuildPromptPerformance NFR5 性能、ConcurrentAccess 50 goroutine 并发 + `-race` 通过、ContextError 格式化和 Unwrap
- ✅ 全量回归 `go test -race ./...` 通过：context、xsync、kernel、vfs 全部 PASS，零失败零回归

### Implementation Plan

**双层 API 设计决策**：底层 CtxRead/CtxWrite 使用 JSON 序列化操作原始字节，高层 SetSystemPrompt/AppendMessage/AppendToolResult/BuildPrompt 面向实际使用场景。reasonStep 循环（Story 1.6）将主要使用高层 API。

**并发安全策略**：Manager 级别通过 SyncMap 保护，每个 Context 内部使用 RWMutex（写操作用写锁，读操作用读锁）。CtxFree 使用 LoadAndDelete 原子删除，吸收 Story 1-3 的 TOCTOU 经验教训。

**依赖方向**：context/ → internal/types/ ✓、context/ → internal/xsync/ ✓。未导入 kernel/、vfs/ 或标准库 context 包（当前不需要）。

### File List

- `context/context.go` (新增) — 上下文管理核心实现：类型定义、Manager、CtxAlloc/CtxFree/CtxRead/CtxWrite、SetSystemPrompt/AppendMessage/AppendToolResult、BuildPrompt、ContextError
- `context/context_test.go` (新增) — 12 个测试函数，覆盖所有 AC，包含并发安全和性能测试

### Change Log

- 2026-02-24: 实现 Story 1-4 上下文管理 — 完成所有 5 个 Task（核心类型、Manager、CtxRead/CtxWrite、BuildPrompt、完整单元测试）。全量回归通过。
- 2026-02-24: 代码审查修复 — 6 个问题（2 HIGH + 4 MEDIUM）。H1: 性能测试增加 time.Since 计时断言验证 NFR5; H2: 新增 TestManager_CtxWriteOverwrite 覆盖覆写路径; M1: getContext 接受 op 参数，消除错误重复包装; M2: CtxWrite/CtxRead 文档注释明确 offset 语义差异; M3: MaxSize 字段注释说明仅限消息数量; M4: 并发测试验证 append 成功/失败精确计数。全量回归通过。
