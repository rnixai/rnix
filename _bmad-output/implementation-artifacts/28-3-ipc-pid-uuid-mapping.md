# Story 28.3: IPC PID→UUID 映射

Status: dev-complete

## Story

As a 平台构建者,
I want IPC 方法（GetStepDetail 等）支持按 PID 或 UUID 查询,
So that 我可以用 PID 快速查询当前 daemon 中的进程，也可以用 UUID 查询历史进程。

## Acceptance Criteria

### AC-1: GetStepDetail 支持 PID 查询（映射到 UUID）

**Given** GetStepDetail IPC 请求
**When** 请求中包含 PID
**Then** daemon 内部通过进程表将 PID 映射到 UUID
**And** 使用 UUID 路径读取 steps.jsonl

### AC-2: GetStepDetail 支持 UUID 直接查询

**Given** GetStepDetail IPC 请求
**When** 请求中包含 UUID（新增字段）
**Then** 直接使用 UUID 路径读取 steps.jsonl
**And** 跳过 PID→UUID 映射步骤

### AC-3: GetStepDetailRequest 新增 UUID 字段

**Given** GetStepDetailRequest 结构体
**When** 扩展
**Then** 新增 `UUID string` 可选字段
**And** PID 和 UUID 至少提供一个，UUID 优先

### AC-4: PID 查询已 reap 进程返回 not_found

**Given** 进程已被 reaper 清理（不在内存中）
**When** 用 PID 查询
**Then** 返回 not_found 错误（PID 仅在当前 daemon 生命周期内有效）

### AC-5: UUID 查询已 reap 进程从磁盘读取

**Given** 进程已被 reaper 清理
**When** 用 UUID 查询
**Then** 从 `.rnix/data/steps/<uuid>/` 读取数据
**And** 正常返回（UUID 全局唯一，不受 daemon 生命周期限制）

### AC-6: 其他 IPC 方法统一支持 PID 或 UUID

**Given** 其他 IPC 方法（如 attach_debug、get_log、get_proc_detail 等）
**When** 涉及进程查询
**Then** 统一支持 PID 或 UUID 参数

## Tasks / Subtasks

- [x] Task 1: 新增 `GetProcessByUUID` 内核方法 (AC: #2, #6)
  - [x] `kernel/kernel.go`: 在 `ProcessManager` 接口新增 `GetProcessByUUID(uuid string) (*Process, bool)`
  - [x] `kernel/kernel.go`: 实现遍历 `procTable` 匹配 UUID
- [x] Task 2: 扩展 IPC 请求结构体新增 UUID 字段 (AC: #3, #6)
  - [x] `ipc/protocol.go`: 以下 15 个结构体新增 `UUID string \`json:"uuid,omitempty"\``
- [x] Task 3: 新增统一进程解析辅助方法 (AC: #1, #2, #4, #5, #6)
  - [x] `ipc/server.go`: 新增 `resolveProcess(pid types.PID, uuid string) (*kernel.Process, bool)`
  - [x] `ipc/server.go`: 新增 `resolveStepsPath(pid types.PID, uuid string) string`
- [x] Task 4: 更新 GetStepDetail/ListSteps 使用 UUID (AC: #1-#5)
  - [x] `ipc/server.go` `handleGetStepDetail`: 使用 `resolveProcess(req.PID, req.UUID)`
  - [x] `ipc/server.go` `handleListSteps`: 使用 `resolveStepsPath(req.PID, req.UUID)`
  - [x] UUID 查询已 reap 进程：直接从 UUID 路径读取
  - [x] PID 查询已 reap 进程：返回 NOT_FOUND（移除旧 PID fallback 行为）
- [x] Task 5: 更新 GetProcDetail 使用 UUID (AC: #6)
  - [x] `ipc/server.go` `handleGetProcDetail`: 使用 `resolveProcess`
- [x] Task 6: 更新其他 IPC 方法使用 UUID (AC: #6)
  - [x] `handleKill`: UUID→PID 解析
  - [x] `handleAttachDebug`: UUID→PID 解析
  - [x] `handleAttachLog`: UUID→PID 解析
  - [x] `handleAttachGdb`: UUID→PID 解析
  - [x] `handleDetachGdb`: UUID→PID 解析
  - [x] `handleGdbCommand`: 使用 `resolveProcess`
  - [x] `handleRecordStart`: UUID→PID 解析
  - [x] `handleRecordStop`: UUID→PID 解析
  - [x] `handleCtxProfile`: UUID→PID 解析
  - [x] `handleCtxGrowth`: UUID→PID 解析
  - [x] `handleLineage`: UUID→PID 解析
  - [x] `handleImmuneResume`: UUID→PID 解析
- [x] Task 7: 测试 (AC: #1-#6)
  - [x] ATDD: GetStepDetail 通过 PID 查询存活进程（PID→UUID 映射）
  - [x] ATDD: GetStepDetail 通过 UUID 查询存活进程（跳过 PID 映射）
  - [x] ATDD: GetStepDetail 通过 UUID 查询已 reap 进程（磁盘读取）
  - [x] ATDD: GetStepDetail 通过 PID 查询已 reap 进程（返回 not_found）
  - [x] ATDD: ListSteps 同上 4 个场景
  - [x] ATDD: resolveProcess 优先 UUID 逻辑验证
  - [x] 确保 `make all` 通过

## Dev Notes

### 架构约束（Architecture Decision 27）

- **双查询模式**：PID 用于当前 daemon 生命周期内快速查询，UUID 用于跨 daemon 的持久化查询
- **UUID 优先**：当请求同时包含 PID 和 UUID 时，UUID 优先
- **向后兼容**：UUID 字段为 `omitempty`，旧客户端不传 UUID 时行为不变
- **已 reap 进程**：PID 查询返回 not_found，UUID 查询从磁盘读取

### 关键修改文件

| 文件 | 修改内容 |
|------|---------|
| `kernel/kernel.go` | ProcessManager 接口新增 `GetProcessByUUID`，KernelImpl 实现 |
| `ipc/protocol.go` | 15 个请求结构体新增 `UUID string` 字段 |
| `ipc/server.go` | 新增 `resolveProcess`/`resolveStepsPath` 辅助方法；更新 14+ handler |

### 源代码关键位置（精确行号）

**内核进程查询 — `kernel/kernel.go`:**
- L2569-2571: `GetProcess(pid types.PID) (*Process, bool)` — 按 PID 查 procTable
- `ProcessManager` 接口定义处 — 需新增 `GetProcessByUUID(uuid string) (*Process, bool)` 方法签名
- `GetProcessByUUID` 实现：遍历 `k.procTable`（SyncMap），匹配 `proc.UUID == uuid`

**`GetProcessByUUID` 实现模式**：
```go
func (k *KernelImpl) GetProcessByUUID(uuid string) (*Process, bool) {
    var found *Process
    k.procTable.Range(func(pid types.PID, proc *Process) bool {
        if proc.UUID == uuid {
            found = proc
            return false // stop iteration
        }
        return true
    })
    return found, found != nil
}
```

注意：`k.procTable` 是 `xsync.SyncMap[types.PID, *Process]`，`Range` 方法签名为 `func(key K, value V) bool`。

**IPC 请求结构体 — `ipc/protocol.go`:**

当前 `GetStepDetailRequest` (L812-815):
```go
type GetStepDetailRequest struct {
    PID  types.PID `json:"pid"`
    Step int       `json:"step"`
}
```

修改为：
```go
type GetStepDetailRequest struct {
    PID  types.PID `json:"pid"`
    UUID string    `json:"uuid,omitempty"`
    Step int       `json:"step"`
}
```

其余 14 个结构体同理：在 PID 字段后新增 `UUID string \`json:"uuid,omitempty"\``。

**IPC 步骤数据处理 — `ipc/server.go`:**

`handleGetStepDetail` (L1838-1906) 当前流程：
1. L1847: `proc, procFound := s.kern.GetProcess(req.PID)` — 查内存
2. 进程在内存 → `resolveStepsPathFromProc(proc, req.PID)` 构建路径
3. 进程不在内存 → `resolveStepsPathFallback(req.PID)` 磁盘 fallback
4. 读取 steps.jsonl → 返回 StepRecord

**改造后流程**：
1. UUID 非空 → `resolveProcess(req.PID, req.UUID)` 查内存
2. 进程在内存 → 使用 `proc.UUID` 构建路径（同现有逻辑）
3. 进程不在内存 + UUID 非空 → 直接构建 `steps/<uuid>/steps.jsonl` 路径
4. 进程不在内存 + UUID 为空 → 返回 not_found（**行为变更：不再 PID fallback**）

**`resolveStepsPathFromProc` (L2062-2074)**：使用 `proc.UUID` 构建路径，**无需修改**。

**`resolveStepsPathFallback` (L2076-2116)**：当前按 PID 扫描 UUID 目录。Story 28-3 完成后，当客户端传 UUID 时可直接构建路径，不需要扫描。此函数可能被新的 `resolveStepsPath` 方法替代或简化。

**统一解析辅助方法设计**：

```go
// resolveProcess 尝试从内存中解析进程。UUID 优先。
func (s *Server) resolveProcess(pid types.PID, uuid string) (*kernel.Process, bool) {
    if uuid != "" {
        if proc, ok := s.kern.GetProcessByUUID(uuid); ok {
            return proc, true
        }
    }
    if pid != 0 {
        return s.kern.GetProcess(pid)
    }
    return nil, false
}

// resolveStepsPath 解析 steps.jsonl 路径。UUID 优先。
// 对于已 reap 进程：UUID 查询可从磁盘读取，PID 查询返回空。
func (s *Server) resolveStepsPath(pid types.PID, uuid string) string {
    // 1. 尝试从内存中获取进程
    proc, found := s.resolveProcess(pid, uuid)
    if found {
        return s.resolveStepsPathFromProc(proc, proc.PID)
    }
    // 2. 进程不在内存，UUID 非空 → 直接构建路径
    if uuid != "" {
        base := s.kern.GetStepDataDir()
        if base != "" {
            p := filepath.Join(base, "data", "steps", uuid, "steps.jsonl")
            if _, err := os.Stat(p); err == nil {
                return p
            }
        }
        return ""
    }
    // 3. 仅 PID，进程不在内存 → not found（PID 只在当前 daemon 生命周期有效）
    return ""
}
```

### 注意：不同 handler 的 UUID 适用性差异

并非所有 handler 对 UUID 的支持方式相同：

**内存操作型**（进程必须存活）：
- `handleKill` — 只能 kill 存活进程
- `handleAttachDebug/Log/Gdb` — 只能附加到存活进程
- `handleDetachGdb` — 只能分离存活进程
- `handleGdbCommand` — 需要存活进程执行命令
- `handleRecordStart/Stop` — 需要存活进程开始/停止记录
- `handleImmuneResume` — 需要存活进程恢复

这些 handler 通过 `resolveProcess` 查找内存中的进程即可。UUID 不在内存中 → not_found。

**数据读取型**（可访问已 reap 进程的磁盘数据）：
- `handleGetStepDetail` — 可从磁盘读 steps.jsonl
- `handleListSteps` — 同上
- `handleGetProcDetail` — 可从磁盘读 process-meta.json（部分数据）

这些 handler 需要额外的磁盘 fallback 逻辑。

**混合型**（优先内存，可降级）：
- `handleCtxProfile` — 需要 `GetProcInfo`（内存操作）
- `handleCtxGrowth` — 需要 `GetProcInfo` + `GetTokenHistory`（内存操作）
- `handleLineage` — 需要 `GetLineage`（内存操作）

### handleGetStepDetail 中的 process-meta 读取

`handleGetStepDetail` (L1865-1878) 在进程不在内存时需要读取 `process-meta.json` 获取 `SystemPrompt` 和 `ToolDefs`。UUID 查询时路径为 `steps/<uuid>/process-meta.json`，可直接读取。PID 查询时因为进程不在内存且新路径使用 UUID，无法定位——这正是返回 not_found 的原因。

### handleGetProcDetail 的 UUID 支持

`handleGetProcDetail` 当前仅支持存活进程（调用 `s.kern.GetProcess(req.PID)` 获取 Process 对象）。UUID 支持同样只查内存。如果需要查询已 reap 进程的详情，需要从 `process-meta.json` 重建部分信息——**此为可选增强，本 Story 范围内仅需支持内存查询**。

### Story 28-2 完成后的代码状态

- `NewStepWriter` 使用 `procUUID string` 参数（非 PID）
- `resolveStepsPathFromProc` 使用 `proc.UUID` 构建路径
- `resolveStepsPathFallback(pid)` 扫描 UUID 目录 process-meta.json 匹配 PID
- `processMetaFile` struct 包含 `PID` 字段
- 客户端（Dashboard 等）可通过 `ProcInfoWire.UUID` 获取 UUID
- 所有新写入路径使用 UUID，但查询仍只按 PID

### 回归风险

- `ProcessManager` 接口新增方法：所有实现者需添加 `GetProcessByUUID`（检查是否有 mock/test 实现）
- IPC 请求结构体新增 `UUID` 字段：JSON 兼容（omitempty，旧客户端无影响）
- `handleGetStepDetail`/`handleListSteps` 行为变更：PID 查询已 reap 进程从 "fallback 扫描" 变为 "not_found"——**这是有意的行为变更**，老行为通过扫描磁盘低效且不可靠
- 内存操作型 handler 增加 UUID 查询路径：逻辑简单，风险低

### 测试策略

新增 ATDD 测试文件 `ipc/atdd_28_3_pid_uuid_mapping_test.go`：

1. **TestGetStepDetail_PID_LiveProcess** — PID 查询存活进程，验证 PID→UUID 映射后读取 steps.jsonl
2. **TestGetStepDetail_UUID_LiveProcess** — UUID 查询存活进程，验证跳过 PID 映射
3. **TestGetStepDetail_UUID_ReapedProcess** — UUID 查询已 reap 进程，验证磁盘读取
4. **TestGetStepDetail_PID_ReapedProcess** — PID 查询已 reap 进程，验证 not_found
5. **TestListSteps_UUID_Support** — ListSteps 的 UUID 支持
6. **TestResolveProcess_UUIDPriority** — 验证 UUID 优先逻辑
7. **TestGetProcessByUUID** — 内核方法单元测试

### Project Structure Notes

- 修改限于 `kernel/` 和 `ipc/` 两个包
- 不涉及 `cmd/rnix/` — 客户端调用代码可在后续 Story（28-4 Dashboard）中适配
- `internal/types/` 不需修改 — UUID 使用 `string` 类型
- `vfs/` 不需修改 — ProcInfo 已有 UUID 字段

### References

- [Architecture Decision 27: Process UUID v7 标识体系](../planning-artifacts/architecture/core-architectural-decisions.md#decision-27-process-uuid-v7-标识体系--pid-短标识--uuid-持久化唯一标识)
- [PRD FR179: IPC 方法支持 PID 或 UUID 查询](../planning-artifacts/prd/functional-requirements.md#process-identity-system进程标识体系phase-2)
- [Epic 28](../planning-artifacts/epics/epic-28-pid标识体系重构-process-identity-system.md)
- [Story 28-1 完成记录](./28-1-process-uuid-v7-introduction.md)
- [Story 28-2 完成记录](./28-2-steprecord-path-migration-to-uuid.md)

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6

### Debug Log References

N/A

### Completion Notes List

- 实现 `GetProcessByUUID` 内核方法，遍历 procTable 匹配 UUID
- 15 个 IPC 请求结构体新增 `UUID string` 字段（omitempty 向后兼容）
- 新增 `resolveProcess` 辅助方法（UUID 优先于 PID）
- 新增 `resolveStepsPath` 辅助方法（已 reap 进程 UUID 查磁盘，PID 查返回空）
- 14 个 handler 统一支持 UUID 查询
- 移除旧的 `resolveStepsPathFallback`（PID 扫描 UUID 目录）和 `isUUIDDir`
- 新增客户端方法 `GetStepDetailByUUID` 和 `ListStepsByUUID`
- 错误码统一为大写 `NOT_FOUND`（更新了 27.2/27.3/27.6/28.2 相关旧测试）
- 修复 AttachDebug ATDD 测试（关闭 DebugChan 避免 goroutine 泄漏）
- 旧 28-2 legacy PID path 测试更新为期望 NOT_FOUND（有意的行为变更）
- `make all` 全部通过：lint 0 issues, 20 包测试通过

### File List

| 文件 | 修改类型 |
|------|---------|
| `kernel/kernel.go` | 新增 `GetProcessByUUID` 方法 |
| `ipc/protocol.go` | 15 个结构体新增 UUID 字段 |
| `ipc/server.go` | 新增 `resolveProcess`/`resolveStepsPath`；更新 14 handler；移除 `resolveStepsPathFallback`/`isUUIDDir` |
| `ipc/client.go` | 新增 `GetStepDetailByUUID`/`ListStepsByUUID` |
| `ipc/atdd_28_3_pid_uuid_mapping_test.go` | 修复 AttachDebug 测试 goroutine 泄漏 |
| `ipc/atdd_27_2_getstepdetail_test.go` | 更新 AC-4 用 UUID 查询；AC-5 错误码大写 |
| `ipc/atdd_27_3_liststeps_test.go` | 更新 AC-8 用 UUID 查询；错误码大写 |
| `ipc/atdd_27_6_getprocdetail_test.go` | AC-5 错误码大写 |
| `ipc/atdd_28_2_uuid_path_test.go` | 更新 fallback 测试用 UUID；legacy PID 测试改期望 NOT_FOUND |
