# Story 4.5: 上下文释放（ctx_free）

Status: ready-for-dev

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a 内核开发者,
I want 进程退出后其上下文空间被正确释放,
So that 系统不会因为上下文累积而内存泄漏。

## Acceptance Criteria

1. **进程退出时调用 CtxFree 释放上下文** — Given 进程状态转为 Dead，When 资源释放流程执行，Then 调用 `CtxFree(process.CtxID)` 释放上下文空间

2. **释放后访问返回错误** — Given 上下文已释放，When 尝试 CtxRead 或 CtxWrite 该 CtxID，Then 返回错误，错误码为 `ErrNotFound`

3. **并发释放无竞态** — Given 多个进程同时退出，When 并发执行 CtxFree，Then 无数据竞争（通过 `-race` 测试验证）

4. **所有测试通过** — Given 实现完成，When 执行 `go test -race ./...`，Then 所有新增和现有测试通过，无竞态条件；`go vet ./...` 无警告

## Tasks / Subtasks

> **重要背景：** CtxFree 核心功能已在 Story 1.4（context 包）和 Story 4.2（reapProcess 集成）中实现。本 Story 聚焦验证、补充测试、和文档完善。

- [ ] Task 1: 验证现有 CtxFree 实现的 AC 合规性 (AC: #1, #2, #3)
  - [ ] 1.1 验证 `context/context.go:99-110` 的 `CtxFree()` 方法正确使用 `LoadAndDelete` 原子操作删除上下文
  - [ ] 1.2 验证 `kernel/reap.go:35` 在资源释放序列的正确位置调用 `k.ctxMgr.CtxFree(proc.CtxID)`（第 5 步：cancel → wg.Wait → FD → DebugChan → **CtxFree** → Reap → Remove）
  - [ ] 1.3 验证 `context/context_test.go:99-186` 中 4 个 CtxFree 专用测试覆盖：释放后 CtxRead 失败、释放后 CtxWrite 失败、释放后 BuildPrompt 失败、不存在的 CtxID 返回 ErrNotFound
  - [ ] 1.4 验证 `kernel/reap_test.go` 中 Wait/Kill/orphan 场景均验证 CtxFree 被调用（BuildPrompt 返回错误）

- [ ] Task 2: 补充 CtxFree 专项集成测试 (AC: #3, #4)
  - [ ] 2.1 在 `context/context_test.go` 中添加 `TestManager_CtxFreeConcurrent` — 启动 100 个 goroutine 并发 Alloc+Free，验证 `-race` 无竞态
  - [ ] 2.2 在 `context/context_test.go` 中添加 `TestManager_CtxFreeDoubleFree` — 对同一 CtxID 调用两次 CtxFree，第二次返回 ErrNotFound（验证幂等安全性）
  - [ ] 2.3 在 `context/context_test.go` 中添加 `TestManager_CtxFreeAllOperationsFail` — 释放后验证 CtxRead、CtxWrite、SetSystemPrompt、AppendMessage、AppendToolResult、BuildPrompt、GetContextSummary 全部返回 ErrNotFound
  - [ ] 2.4 在 `kernel/reap_test.go` 中添加 `TestReapProcess_CtxFreeCalledInOrder` — 验证 CtxFree 在 DebugChan 关闭之后、Reap 状态转移之前被调用（资源释放顺序验证）

- [ ] Task 3: 运行完整测试套件验证 (AC: #4)
  - [ ] 3.1 执行 `go test -race ./...` 确认所有包通过
  - [ ] 3.2 执行 `go vet ./...` 确认无警告
  - [ ] 3.3 如有测试失败，修复后重新验证

## Dev Notes

### 核心发现——CtxFree 已完全实现

**重要：** 经过对代码库的全面分析，CtxFree 功能已在之前的 Story 中完整实现并集成。本 Story 的核心价值是 **验证、补充测试、和确认 AC 合规**。

#### 已实现的核心功能

| 组件 | 文件 | 行号 | 实现状态 |
|------|------|------|---------|
| CtxFree 方法 | `context/context.go` | 99-110 | ✅ 完全实现（LoadAndDelete 原子操作） |
| reapProcess 中调用 | `kernel/reap.go` | 35 | ✅ 正确集成（释放序列第 5 步） |
| CtxFree 单元测试 | `context/context_test.go` | 99-186 | ✅ 4 个专用测试 |
| 集成测试验证 | `kernel/reap_test.go` | 多处 | ✅ 9+ 个测试验证资源释放 |
| 并发安全测试 | `context/context_test.go` | 639-752 | ✅ 50 goroutine 并发测试 |

#### CtxFree 实现详解

```go
// context/context.go:99-110
func (m *Manager) CtxFree(cid types.CtxID) error {
    _, ok := m.contexts.LoadAndDelete(cid)
    if !ok {
        return &ContextError{
            Op:   "CtxFree",
            CID:  cid,
            Err:  fmt.Errorf("context not found"),
            Code: types.ErrNotFound,
        }
    }
    return nil
}
```

**设计要点：**
- `LoadAndDelete` 是原子操作，保证并发安全，无需额外锁
- 返回 `*ContextError`（非 `*SyscallError`），因为这是 context 包内部的错误类型
- 不存在的 CtxID 返回 `ErrNotFound`，双重释放安全（幂等）

#### 资源释放流程（reapProcess）

```go
// kernel/reap.go:10-43
func (k *KernelImpl) reapProcess(proc *Process) {
    proc.reapOnce.Do(func() {
        k.handleOrphanChildren(proc)      // 0. 处理孤儿子进程
        proc.Cancel()                      // 1. cancel()
        proc.wg.Wait()                     // 2. wg.Wait()
        // ... close DebugChan ...         // 3-4. 关闭 DebugChan
        _ = k.ctxMgr.CtxFree(proc.CtxID)  // 5. CtxFree ← Story 4.5 的核心
        _ = proc.Reap()                    // 6. Zombie → Dead
        k.RemoveProcess(proc.PID)          // 7. 移除进程表
    })
}
```

**reapOnce 保证：** `sync.Once` 确保整个释放流程仅执行一次，即使 Wait() 和自动回收器并发触发。

#### AC 偏差说明

**AC #2 错误类型：** Epics AC 原文为 "返回 `*SyscallError`"，但 `context.Manager.CtxFree()` 返回 `*ContextError`。这是设计正确的——CtxFree 在 reapProcess 内部调用（非用户面 syscall），错误被忽略（`_ = k.ctxMgr.CtxFree()`）。如果未来将 CtxFree 暴露为正式 syscall 入口，kernel 层会包装为 `*SyscallError`。当前实现满足 AC 的语义意图。

#### CtxFree 在 Spawn 错误路径中的使用

`kernel/kernel.go` 的 Spawn 函数在 3 处错误恢复路径中调用 CtxFree：

```go
// 行 179 — system prompt 设置失败时
_ = k.ctxMgr.CtxFree(cid)

// 行 196 — agent instructions 注入失败时
_ = k.ctxMgr.CtxFree(cid)

// 行 213 — VFS Open 失败时
_ = k.ctxMgr.CtxFree(cid)
```

这确保了即使 Spawn 中途失败，分配的上下文空间也会被正确释放。

### 前序 Story 经验

#### Story 4.4 (crux ps) 经验

- **不要修改已稳定的接口**——Story 4.4 决定不修改 ProcessManager 接口（ListProcs 已满足需求），本 Story 同理不修改 ContextManager 接口
- **遵循 `errors.As` 标准模式**——测试中用 `errors.As(err, &ctxErr)` 而非类型断言
- **reapOnce 模式**——测试中必须 `defer k.Shutdown()` 避免 goroutine 泄漏

#### Story 4.2 (orphan reparent / zombie auto-reap) 经验

- **reapProcess 中 CtxFree 已在此 Story 实现**——资源释放顺序已验证
- **`proc.reapOnce.Do` 保护**——防止并发重复释放
- **emitEvent 模式**——CtxFree 被调用时 DebugChan 已关闭，因此无法也不需要发射 SyscallEvent

### 已有代码关键 API 参考

**context/context.go 关键方法：**
- `Manager` 结构体（行 65-69）：contexts SyncMap + nextID atomic
- `CtxAlloc(size int)` (行 79-96)：分配上下文，返回 CtxID
- `CtxFree(cid CtxID)` (行 99-110)：释放上下文，原子 LoadAndDelete
- `CtxRead(cid, offset, length)` (行 178-217)：读取上下文数据
- `CtxWrite(cid, offset, data)` (行 131-173)：写入上下文数据
- `BuildPrompt(cid)` (行 287-303)：组装 LLM prompt
- `GetContextSummary(ctxID)` (行 307-352)：获取上下文摘要

**kernel/reap.go 关键代码：**
- `reapProcess(proc)` (行 10-43)：完整资源释放序列
- `Wait(pid)` (行 85-115)：等待进程并触发 reapProcess
- `startReaper()` (行 117-138)：自动回收 Zombie 进程
- `handleOrphanChildren(proc)` (行 48-82)：孤儿进程 reparent

**kernel/process.go 关键字段：**
- `Process.CtxID types.CtxID` (行 44)：进程持有的上下文 ID
- `Process.reapOnce sync.Once` (行 54)：保证 reapProcess 仅执行一次

**context/context_test.go 现有 CtxFree 测试：**
- `TestManager_CtxFree` (行 99-170)：3 个子测试（释放后 Read/Write/BuildPrompt 失败）
- `TestManager_CtxFreeNotFound` (行 173-186)：不存在 CtxID 返回 ErrNotFound
- `TestManager_ConcurrentAccess` (行 639-752)：50 goroutine 并发 Alloc+Free

**kernel/reap_test.go 相关测试：**
- `TestWait_NormalCompletion` (行 16-53)：Wait 后验证 BuildPrompt 失败
- `TestWait_KillThenWait` (行 55-106)：Kill 后 Wait 验证 CtxFree
- `TestWait_ResourceRelease` (行 129-184)：完整资源释放序列验证
- `TestReapOnce_ConcurrentReapProcess` (行 918-966)：并发 reapProcess 仅执行一次

### NFR 合规

| NFR | 要求 | 实现保证 |
|-----|------|---------|
| NFR8 | 进程退出后 10s 内 goroutine 和 context 内存释放 | CtxFree 立即从 SyncMap 删除，Go GC 在下次 cycle 回收；reapProcess 在进程退出时同步执行 |
| NFR9 | 进程表在异常退出后保持一致性 | reapOnce + 原子 LoadAndDelete 确保无双重释放、无悬挂引用 |

### 范围边界

**本 Story 包含：**
- 验证 `context/context.go` 中 CtxFree 实现的 AC 合规性
- 补充 CtxFree 专项集成测试（并发、双重释放、全操作失败）
- 验证 `kernel/reap.go` 中资源释放顺序的正确性
- 运行完整测试套件确认无回归

**本 Story 不包含：**
- 新增 kernel 层 CtxFree 的 SyscallEvent 发射（DebugChan 在 CtxFree 之前已关闭）
- 修改 ContextManager 接口
- 将 CtxFree 暴露为独立的 CLI 命令
- 上下文压缩或淘汰策略（Phase 2）
- 跨进程上下文共享（Phase 2）

### Project Structure Notes

**可能修改的文件：**
```
context/context_test.go           — 补充 CtxFree 专项测试（并发、双重释放、全操作失败）
kernel/reap_test.go               — 补充资源释放顺序验证测试
```

**不修改的文件：**
```
context/context.go                — CtxFree 已完全实现，无需修改
kernel/reap.go                    — reapProcess 中 CtxFree 调用已正确集成
kernel/kernel.go                  — Spawn 错误路径中 CtxFree 已正确调用
kernel/process.go                 — Process.CtxID 字段不变
internal/types/types.go           — CtxID 类型定义不变
```

### References

**规划文档：**
- [Source: _bmad-output/planning-artifacts/epics.md#Story 4.5] — Story 定义和验收标准（第 890-910 行）
- [Source: _bmad-output/planning-artifacts/architecture.md#Decision 1] — ContextManager 接口定义（CtxAlloc/CtxRead/CtxWrite/CtxFree）
- [Source: _bmad-output/planning-artifacts/architecture.md#资源释放顺序] — cancel → wg.Wait → FD → DebugChan → CtxFree → Dead → 移除
- [Source: _bmad-output/planning-artifacts/architecture.md#Decision 6] — 错误处理模式（SyscallError 包装）
- [Source: _bmad-output/planning-artifacts/prd.md#FR22] — 进程退出后释放上下文空间
- [Source: _bmad-output/planning-artifacts/prd.md#NFR8] — 退出 10s 内释放资源无泄漏
- [Source: _bmad-output/project-context.md#进程状态机] — 资源释放顺序约束

**前序 Story：**
- [Source: _bmad-output/implementation-artifacts/4-4-crux-ps-command-and-process-table-ui.md] — reapOnce 模式、errors.As 使用、Code Review 经验

**源码行号参考：**
- context/context.go: Manager(65-69), CtxAlloc(79-96), CtxFree(99-110), CtxRead(178-217), CtxWrite(131-173)
- kernel/reap.go: reapProcess(10-43), Wait(85-115), startReaper(117-138), handleOrphanChildren(48-82)
- kernel/kernel.go: Spawn(120-253), CtxAlloc调用(160-169), CtxFree错误恢复(179,196,213)
- kernel/process.go: Process.CtxID(44), reapOnce(54)
- internal/types/types.go: CtxID(14-15)

## Dev Agent Record

### Agent Model Used

{{agent_model_name_version}}

### Debug Log References

### Completion Notes List

### File List
