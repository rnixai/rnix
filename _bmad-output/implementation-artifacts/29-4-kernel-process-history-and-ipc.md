# Story 29.4: Kernel 进程历史保留与 IPC

Status: done

## Story

As a 平台构建者,
I want 系统保留已结束进程的信息快照,
So that Dead 进程被 reaper 清除后仍可在 Dashboard 中追溯。

## Acceptance Criteria

1. **ProcessHistory 组件** — `kernel/process_history.go` 新增 `ProcessHistory` 结构体，使用 `sync.RWMutex` 保护的 `[]vfs.ProcInfo` 切片，最大保留 1000 条，FIFO 淘汰
2. **Reaper 集成** — `cleanupExpiredDead` 在 `RemoveProcess` 从 procTable 删除之前，先调用 `history.Add(procInfo)` 保存完整快照（PID、UUID、Agent、Model、State、CreatedAt、DeadAt、TokensUsed、ExitCode 等）
3. **ListAllProcs 方法** — `kernel/kernel.go` 新增 `ListAllProcs() []vfs.ProcInfo`，返回存活进程（procTable）+ 历史进程的合并列表，按 CreatedAt 排序
4. **IPC list_all_procs** — 按 IPC 扩展标准 4 步：protocol.go 定义常量+类型 → server.go 注册 handler → client.go 封装客户端方法（无需 CLI 入口）
5. **并发安全** — RWMutex 保证 reaper 写入 history 与 Dashboard 读取的数据一致性
6. **状态符号体系** — `internal/ui/symbols.go` 新增统一进程状态符号函数 `StateSymbol(state ProcessState) string`：`●` Running, `○` Created, `✓` Done (exit 0), `✕` Failed (exit ≠ 0), `⏸` Paused；`RNIX_ASCII=1` 时使用 `*`, `o`, `+`, `x`, `=`

## Tasks / Subtasks

- [x] Task 1: 创建 `kernel/process_history.go` (AC: #1, #5)
  - [x] 1.1 定义 `ProcessHistory` 结构体：`mu sync.RWMutex` + `entries []vfs.ProcInfo` + `maxSize int`
  - [x] 1.2 实现 `NewProcessHistory(maxSize int) *ProcessHistory` 构造函数
  - [x] 1.3 实现 `Add(info vfs.ProcInfo)` — 写锁，append，超限时移除头部（FIFO）
  - [x] 1.4 实现 `List() []vfs.ProcInfo` — 读锁，返回深拷贝切片
  - [x] 1.5 实现 `Len() int` — 读锁，返回当前条目数
- [x] Task 2: 集成 ProcessHistory 到 KernelImpl (AC: #2, #3)
  - [x] 2.1 `KernelImpl` 新增 `procHistory *ProcessHistory` 字段（Reaper infrastructure 注释区域下方）
  - [x] 2.2 `NewKernel()` 中初始化 `procHistory: NewProcessHistory(1000)`
  - [x] 2.3 修改 `cleanupExpiredDead()` — 在 `k.RemoveProcess(pid)` 之前，构造 `vfs.ProcInfo` 快照并调用 `k.procHistory.Add(info)`
  - [x] 2.4 实现 `ListAllProcs() []vfs.ProcInfo` — 合并 `ListProcs()` + `procHistory.List()`，去重（按 PID），按 `CreatedAt` 排序
- [x] Task 3: IPC 扩展 — list_all_procs (AC: #4)
  - [x] 3.1 `ipc/protocol.go` — 新增 `MethodListAllProcs Method = "list_all_procs"` 常量（无需新的 Request/Response 类型，复用 `ListProcsResponse`）
  - [x] 3.2 `ipc/server.go` — dispatch switch 新增 `case MethodListAllProcs: s.handleListAllProcs(conn)`；实现 `handleListAllProcs` 调用 `s.kern.ListAllProcs()`
  - [x] 3.3 `ipc/client.go` — 新增 `ListAllProcs() ([]vfs.ProcInfo, error)` 方法，调用 `c.call(MethodListAllProcs, nil)`
- [x] Task 4: 状态符号体系 (AC: #6)
  - [x] 4.1 新建 `internal/ui/symbols.go` — 定义 `StateSymbol(state types.ProcessState, result string) string` 函数
  - [x] 4.2 Unicode 符号映射：`StateRunning→●`, `StateCreated→○`, `StateDead+exit0→✓`, `StateDead+exit≠0→✕`, `StateZombie→⏸`
  - [x] 4.3 ASCII 模式（`RNIX_ASCII=1`）：`*`, `o`, `+`, `x`, `=`
  - [x] 4.4 使用 `os.Getenv("RNIX_ASCII")` 检测 ASCII 模式（与 renderer.go 中 DetectProfile 一致的逻辑）
- [x] Task 5: 测试 + 验证 (AC: all)
  - [x] 5.1 ATDD 测试修复 — 修正 NewProcess 的 PID/PPID 参数混淆、WaitGroup.Go modernize、interface{} → any
  - [x] 5.2 `ipc/` 相关测试 — list_all_procs 常量、客户端方法、server dispatch 测试
  - [x] 5.3 `internal/ui/` 测试 — StateSymbol 13 场景全通过（Unicode 8 + ASCII 5）
  - [x] 5.4 `make all` 全部通过（lint 0 issues, 22 packages OK）

## Dev Notes

### 核心实现要点

**ProcessHistory 是一个独立的内核组件，不是进程表的扩展。** 它只保存已从 procTable 移除的 Dead 进程快照。存活进程（Created/Running/Zombie/Dead 但未过期）仍在 procTable 中。

**关键时序**：进程快照必须在 `cleanupExpiredDead` 的 `RemoveProcess` 调用之前保存。当前流程：
1. `reapProcess()` — 资源释放 + Zombie→Dead 状态转移 + 设置 `DeadAt` 时间戳
2. `cleanupExpiredDead()` — 每 10 秒检查，DeadAt 过期（60s TTL）的进程调用 `RemoveProcess`
3. **新增插入点**：`cleanupExpiredDead` 中 `RemoveProcess` 之前调用 `procHistory.Add()`

**不要修改 `reapProcess()`**。快照点在 `cleanupExpiredDead` 而非 `reapProcess`，因为：
- `reapProcess` 时进程还在 procTable 中（TTL 60 秒内仍可查询）
- 只有当进程即将从 procTable 永久删除时才需要保存到 history

### IPC 方法需暴露 KernelImpl.ListAllProcs

**关键**：当前 `ipc/server.go` 通过 `s.kern` 字段（类型为 `*kernel.KernelImpl`）调用内核方法。`ListAllProcs` 直接作为 `KernelImpl` 的方法即可被 server 调用，无需修改接口。

参考现有 `handleListProcs` 实现（`ipc/server.go:382-390`）：
```go
func (s *Server) handleListProcs(conn net.Conn) {
    procs := s.kern.ListProcs()
    wireProcs := make([]ProcInfoWire, len(procs))
    for i, p := range procs {
        wireProcs[i] = ProcInfoToWire(p)
    }
    payload, _ := json.Marshal(ListProcsResponse{Processes: wireProcs})
    writeResponse(conn, Response{OK: true, Payload: payload})
}
```

`handleListAllProcs` 完全相同的模式，只是调用 `s.kern.ListAllProcs()` 替代 `s.kern.ListProcs()`。复用 `ListProcsResponse` 和 `ProcInfoToWire` — 不需要新的 Wire 类型。

### ListAllProcs 合并去重逻辑

```
active := k.ListProcs()         // 当前 procTable 中的所有进程
historical := k.procHistory.List() // 已移除的历史进程

// 去重：procTable 中的进程优先（更新的数据）
seen := set of active PIDs
result := active + historical entries not in seen
sort by CreatedAt ascending
```

**为什么需要去重**：理论上不会重复（history 只保存 RemoveProcess 之前的快照），但防御性编程避免竞态边界情况。

### 状态符号 — exitCode 判定

`StateSymbol` 需要 exitCode 参数来区分 Done（exit 0）和 Failed（exit ≠ 0）。调用者从 `vfs.ProcInfo` 获取退出码的方式：
- 当前 `ProcInfo` 没有直接的 `ExitCode` 字段
- 退出信息在 `Result` 字符串中（如 "completed: ..."）
- **建议**：`StateSymbol` 接收 `state types.ProcessState` 和 `result string` 两个参数。Dead 进程用 `result` 内容判断：空字符串或包含 "error"/"fail"/"timeout" → Failed，否则 → Done
- 这与 Story 29.3 Focus Card 中 `dashboard_focus.go` 的退出码推断逻辑一致（Code Review P-5 确认的方案：检查 error/fail/timeout 关键词）

### 可复用的已有设施

| 设施 | 位置 | 用途 |
|------|------|------|
| `vfs.ProcInfo` 结构体 | `vfs/proc.go` | 进程快照数据类型，已包含所有需要的字段 |
| `ProcInfoToWire` / `WireToProcInfo` | `ipc/protocol.go` | ProcInfo ↔ Wire 序列化，直接复用 |
| `ListProcsResponse` | `ipc/protocol.go` | 响应类型，`list_all_procs` 直接复用 |
| `IsASCII()` | `internal/ui/renderer.go` | 检测 `RNIX_ASCII=1` 环境变量 |
| `colorState()` | `cmd/rnix/dashboard.go:578` | Dashboard 中按状态着色（与 StateSymbol 互补） |

### 不应该做的事

- ~~**不要修改 `reapProcess()`**~~ — **已更新**：`reapProcess` 步骤 12 现在也调用 `SaveProcInfo` 写入磁盘（2026-03-25 持久化增强）
- **不要修改 `vfs.ProcInfo` 类型** — 已有字段足够，不需要新增 ExitCode 字段

### 2026-03-25 增强：进程历史磁盘持久化

**问题**：ProcessHistory 是纯内存 FIFO 环形缓冲区，daemon 重启后全部清空。Dashboard 重启后无法显示任何历史进程。

**解决方案**：在每个进程的 UUID 目录下保存 `proc-info.json`，daemon 启动时扫描加载。

**磁盘布局**：
```
.rnix/data/steps/<uuid>/
├── steps.jsonl          (已有)
├── process-meta.json    (已有)
└── proc-info.json       (新增 — 完整 ProcInfo 快照)
```

**新增文件/方法**：
| 文件 | 内容 |
|------|------|
| `kernel/process_history.go` | `procInfoDisk` 序列化结构体、`SaveProcInfo()` 原子写入、`LoadProcHistory()` 扫描加载、`FindByUUID()` |
| `kernel/reap.go` | `reapProcess` 步骤 12 调用 `SaveProcInfo`；`cleanupExpiredDead` 安全网写入 |
| `kernel/kernel.go` | `LoadHistory()` 方法、`FindHistoryByUUID()` 方法 |
| `cmd/rnix/main.go` | `runDaemon` 中 `SetStepDataDir` + `LoadHistory` |
| `ipc/server.go` | `handleGetProcDetailFromHistory()` 从 procHistory 构造 detail 响应 |
| `ipc/client.go` | `GetProcDetail` 支持可选 UUID 参数 |

**Dashboard 侧修复**：
- `fetchProcDetailCmd` 传递 UUID 参数
- Focus Card token 数据在无 heatmap 时从 procDetail 回退
- Heatmap 对已结束进程显示友好提示

### 2026-03-25 增强：翻页快捷键

在 Tree、Timeline、History 三个可滚动视图中添加：
- `PgDn` / `PgUp` — 翻页
- `g` / `Home` — 跳到顶部
- `G` / `End` — 跳到底部

Help overlay 修正：`h/l` 描述从 "Scroll timeline" 改为 "Pan time axis"
- **不要新增 CLI command** — `list_all_procs` 仅供 Dashboard 内部使用，无需 Cobra 命令
- **不要使用 `sync.Map`** — 项目禁止使用标准库 sync.Map，但 ProcessHistory 用切片+RWMutex 即可，无需 SyncMap
- **不要修改 `DeadProcessTTL`** — 60 秒 TTL 保持不变
- **不要在 symbols.go 中引入 lipgloss 依赖** — 符号函数返回纯字符串，着色由调用者处理

### Project Structure Notes

| 文件 | 操作 | 说明 |
|------|------|------|
| `kernel/process_history.go` | **新增** | ProcessHistory 结构体 + Add/List/Len 方法 |
| `kernel/process_history_test.go` | **新增** | 单元测试：FIFO、并发、深拷贝 |
| `kernel/kernel.go` | 修改 | KernelImpl 新增 procHistory 字段 + NewKernel 初始化 + ListAllProcs 方法 |
| `kernel/reap.go` | 修改 | `cleanupExpiredDead` 在 RemoveProcess 前保存快照 |
| `ipc/protocol.go` | 修改 | 新增 `MethodListAllProcs` 常量 |
| `ipc/server.go` | 修改 | dispatch switch 新增 case + handleListAllProcs handler |
| `ipc/client.go` | 修改 | 新增 ListAllProcs 客户端方法 |
| `internal/ui/symbols.go` | **新增** | StateSymbol 函数 + Unicode/ASCII 映射 |
| `internal/ui/symbols_test.go` | **新增** | 符号函数测试 |

### 已有代码精确位置参考

- `KernelImpl` 结构体定义：`kernel/kernel.go:187-244`
- `NewKernel()` 构造函数：`kernel/kernel.go:248-261`
- `cleanupExpiredDead()`：`kernel/reap.go:261-276`
- `RemoveProcess()`：由 `xsync.SyncMap.Delete` 实现
- `ListProcs()`：`kernel/kernel.go:2895-2920`
- `handleListProcs()`：`ipc/server.go:382-390`
- `Client.ListProcs()`：`ipc/client.go:57-71`
- `MethodListProcs` 常量：`ipc/protocol.go:21`
- dispatch switch：`ipc/server.go:283-374`
- `IsASCII()`：`internal/ui/renderer.go`
- `colorState()`：`cmd/rnix/dashboard.go:578`
- 退出码推断逻辑：`cmd/rnix/dashboard_focus.go`（检查 error/fail/timeout 关键词）

### 前置 Story 关键学习（Story 29.3 Code Review Fixes）

- **P-1 [Critical]**: 字符串截断必须 rune-safe（`truncateRuneSafe` 函数）
- **P-2/P-3**: Dead 进程时间计算用 `DeadAt` 而非 `time.Now()`
- **P-8**: 缓存 process 引用减少重复遍历
- **BS-1**: `dashboardTick` IPC 获取条件已放宽支持 `viewDefault` 模式

### Git 近期提交模式

```
feat: Implement Story 29.3 - Default View Focus Card
feat: Implement Story 29.2 - View Mode System and Navigation Overhaul
feat: Implement Story 29.1 - Dashboard File Splitting
```

提交消息格式：`feat: Implement Story N.M - Title`

### 组合矩阵

| 交互点 | 组件 | 需验证 | 说明 |
|--------|------|--------|------|
| ProcessHistory + Reaper | kernel/reap.go | 是 | cleanupExpiredDead 调用 history.Add |
| ListAllProcs + ListProcs | kernel/kernel.go | 是 | 合并去重排序 |
| list_all_procs + list_procs | ipc/ | 是 | 并行请求不冲突 |
| StateSymbol + IsASCII | internal/ui/ | 是 | ASCII 模式回退 |
| ProcessHistory + Shutdown | kernel/reap.go | 否 | Shutdown 后不再有新的 cleanup |

### References

- [Source: _bmad-output/planning-artifacts/epics/epic-29-dashboard-ux-redesign.md#Story 29.4] — 验收准则定义
- [Source: _bmad-output/planning-artifacts/dashboard-redesign-spec-2026-03-23.md#Section 7] — 历史进程列表规格
- [Source: _bmad-output/planning-artifacts/dashboard-redesign-spec-2026-03-23.md#Section 10] — 状态符号体系定义
- [Source: _bmad-output/project-context.md#IPC 扩展标准步骤] — 4 步 IPC 扩展规范
- [Source: _bmad-output/implementation-artifacts/29-3-default-view-focus-card.md#Code Review Fixes] — 前置 Story 学习
- [Source: kernel/reap.go:261-276] — cleanupExpiredDead 当前实现
- [Source: kernel/kernel.go:187-244] — KernelImpl 结构体
- [Source: ipc/server.go:382-390] — handleListProcs 模式参考

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6

### Debug Log References

无

### Completion Notes List

- ProcessHistory 使用切片 + RWMutex 实现，FIFO 淘汰通过 slice reslicing 完成
- cleanupExpiredDead 中复用 `GetProcInfo()` 构造快照，保证字段完整性和加锁一致性
- ListAllProcs 使用 `slices.SortFunc` + `time.Time.Compare` 排序
- StateSymbol 接收 `(state, result string)` 而非 `(state, exitCode int)`，因为 ProcInfo 没有 ExitCode 字段，通过 result 字符串推断失败状态（检查 error/fail/timeout 关键词）
- ATDD 测试修复：NewProcess 第一个参数是 PPID 不是 PID，改为捕获 `proc.PID` 使用
- IPC Client 测试改为编译期验证（避免 nil conn panic）
- Lint modernize 修复：`interface{}` → `any`，`go func()` → `WaitGroup.Go()`

### File List

| 文件 | 操作 | 说明 |
|------|------|------|
| `kernel/process_history.go` | **新增** | ProcessHistory 结构体 + Add/List/Len |
| `kernel/kernel.go` | 修改 | 新增 procHistory 字段 + NewKernel 初始化 + ListAllProcs 方法 |
| `kernel/reap.go` | 修改 | cleanupExpiredDead 在 RemoveProcess 前保存快照 |
| `ipc/protocol.go` | 修改 | 新增 MethodListAllProcs 常量 |
| `ipc/server.go` | 修改 | dispatch switch + handleListAllProcs handler |
| `ipc/client.go` | 修改 | 新增 ListAllProcs 客户端方法 |
| `internal/ui/symbols.go` | **新增** | StateSymbol 函数 + Unicode/ASCII 映射 |
| `kernel/atdd_29_4_process_history_test.go` | 修改 | 修复 PID/PPID 混淆 + lint modernize |
| `ipc/atdd_29_4_list_all_procs_test.go` | 修改 | 修复 nil conn panic + interface{} → any |
| `internal/ui/atdd_29_4_state_symbol_test.go` | 未修改 | 13 个测试全部通过 |
