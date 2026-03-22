# Story 28.1: Process UUID v7 引入

Status: done

## Story

As a 平台构建者,
I want 系统为每个进程在 Spawn 时生成 UUID v7 唯一标识符,
So that 每个进程在跨 daemon 重启后仍保持全局唯一身份。

## Acceptance Criteria

### AC-1: Process 结构体新增 UUID 字段

**Given** Process 结构体（`kernel/process.go`）
**When** 添加 UUID 字段
**Then** 新增 `UUID string` 字段（标准 UUID v7 字符串格式，36 字符）
**And** UUID 在进程创建后不可变

### AC-2: Spawn 时生成 UUID v7

**Given** Kernel.Spawn 方法
**When** 创建新进程
**Then** 生成 UUID v7 并赋值给 Process.UUID
**And** UUID v7 包含时间戳（毫秒精度），保证时间有序
**And** 生成延迟 ≤ 1ms（NFR65-obs）

### AC-3: 跨 daemon 重启 UUID 唯一性

**Given** 两个 daemon 生命周期
**When** daemon 重启后创建新进程
**Then** 新进程的 UUID 与旧 daemon 中所有进程的 UUID 不同
**And** PID 可能从 1 重新开始（PID 行为不变）

### AC-4: `rnix ps --uuid` 显示 UUID 列

**Given** `rnix ps` 输出
**When** 使用 `--uuid` flag
**Then** 额外显示 UUID 列
**And** 默认不显示 UUID（保持向后兼容）

### AC-5: spawn 完成输出包含 UUID

**Given** spawn 完成输出
**When** 显示进程信息
**Then** 输出中包含 PID 和 UUID（如 `spawning PID 3 (uuid: 019...)`）

### AC-6: IPC ProcInfo 传输 UUID

**Given** IPC 进程信息传输
**When** ProcInfoWire 序列化/反序列化
**Then** 包含 UUID 字段
**And** 客户端可获取进程的 UUID

### AC-7: JSON 输出包含 UUID

**Given** `rnix ps --json` 输出
**When** 渲染 JSON
**Then** 每个进程对象包含 `uuid` 字段

## Tasks / Subtasks

- [x] Task 1: 添加 `github.com/google/uuid` 依赖 (AC: #2)
  - [x] `go get github.com/google/uuid`
  - [x] 验证 v7 支持（需 v1.6.0+，`uuid.NewV7()` 可用）
- [x] Task 2: Process 结构体新增 UUID 字段 (AC: #1)
  - [x] `kernel/process.go`: Process struct 新增 `UUID string`
  - [x] `NewProcess()` 中调用 `uuid.Must(uuid.NewV7()).String()` 赋值
- [x] Task 3: ProcInfo（VFS 层）新增 UUID 字段 (AC: #6)
  - [x] `vfs/proc.go`: ProcInfo struct 新增 `UUID string`
- [x] Task 4: Kernel 方法填充 UUID 到 ProcInfo (AC: #6)
  - [x] `kernel/kernel.go` `GetProcInfo()`: 添加 `UUID: proc.UUID`
  - [x] `kernel/kernel.go` `ListProcs()`: 添加 `UUID: proc.UUID`
- [x] Task 5: IPC 协议扩展 (AC: #6)
  - [x] `ipc/protocol.go` `ProcInfoWire`: 新增 `UUID string \`json:"uuid,omitempty"\``
  - [x] `ProcInfoToWire()`: 添加 `UUID: p.UUID`
  - [x] `WireToProcInfo()`: 添加 `UUID: w.UUID`
  - [x] `SpawnResponse`: 新增 `UUID string \`json:"uuid,omitempty"\``
- [x] Task 6: Spawn 输出显示 UUID (AC: #5)
  - [x] `cmd/rnix/main.go` `OnSpawn` 回调: 输出格式 `spawning PID %d (uuid: %s, %s/%s)...`
  - [x] IPC streaming spawn progress: ProgressPayload 新增 UUID 字段
  - [x] `cmd/rnix/main.go` 远程 spawn 路径同步显示 UUID
- [x] Task 7: `rnix ps --uuid` flag (AC: #4)
  - [x] `cmd/rnix/main.go` ps 命令新增 `--uuid` bool flag
  - [x] `internal/ui/table.go` `RenderProcessTable()`: 新增 `showUUID bool` 参数，显示 UUID 列
  - [x] UUID 列截断显示前 8 字符 + `...`（完整 UUID 太长）
- [x] Task 8: JSON 输出 (AC: #7)
  - [x] `cmd/rnix/main.go` `jsonProcess` struct: 新增 `UUID string`
  - [x] `renderPsJSON()`: 赋值 `p.UUID`
- [x] Task 9: 测试 (AC: #1-#7)
  - [x] 单元测试: `NewProcess()` 生成有效 UUID v7 (ATDD 已有)
  - [x] 单元测试: UUID 唯一性（多次调用不重复）(ATDD 已有)
  - [x] 单元测试: ProcInfoToWire/WireToProcInfo 保留 UUID (IPC round-trip 已覆盖)
  - [x] 单元测试: RenderProcessTable 的 UUID 列显示 (ATDD 已有)
  - [x] 集成测试: ATDD AC-5 OnSpawn receives UUID

## Dev Notes

### 架构约束（Architecture Decision 27）

- **双标识体系**：PID 为 daemon 内递增短标识（用户可见），UUID v7 为持久化唯一标识（内部使用）
- **UUID 版本**：UUID v7（RFC 9562），时间戳排序 + 随机尾部
- **格式**：标准 36 字符字符串（`550e8400-e29b-7000-...`）
- **时机**：`NewProcess()` 内部生成（PID 分配同一位置）
- **不可变性**：UUID 在进程创建后永不改变

### 关键修改文件

| 文件 | 修改内容 |
|------|---------|
| `kernel/process.go` | Process struct 新增 `UUID string`，NewProcess 中生成 |
| `vfs/proc.go` | ProcInfo struct 新增 `UUID string` |
| `kernel/kernel.go` | GetProcInfo、ListProcs 填充 UUID |
| `ipc/protocol.go` | ProcInfoWire 新增 UUID，SpawnResponse 新增 UUID，转换函数更新 |
| `cmd/rnix/main.go` | ps 新增 `--uuid` flag，jsonProcess 新增 UUID，OnSpawn 输出 UUID |
| `internal/ui/table.go` | RenderProcessTable 支持 UUID 列 |

### 源代码关键位置

**Process 创建 — `kernel/process.go`:**
- L16-22: `pidCounter` 和 `nextPID()` — PID 分配位置
- L31-54: Process struct — UUID 字段应在 PID 下方
- L125-144: `NewProcess()` — 在此生成 UUID v7

**Spawn 流程 — `kernel/kernel.go`:**
- L317-327: `Spawn()` — 调用 `NewProcess()`，不需修改（UUID 在 NewProcess 内生成）
- L2701-2733: `GetProcInfo()` — 需添加 `UUID: proc.UUID`
- L2875-2899: `ListProcs()` — 需添加 `UUID: proc.UUID`

**IPC 协议 — `ipc/protocol.go`:**
- L93-95: `SpawnResponse` — 新增 UUID
- L105-122: `ProcInfoWire` — 新增 UUID
- L124-172: `ProcInfoToWire`/`WireToProcInfo` — 传递 UUID

**VFS ProcInfo — `vfs/proc.go`:**
- L27-46: ProcInfo struct — 新增 UUID

**PS 命令 — `cmd/rnix/main.go`:**
- L857-880: `runPs()` — 传递 `--uuid` flag
- L886-927: `jsonProcess` 和 `renderPsJSON` — 新增 UUID 字段
- L111-118: `OnSpawn` 回调 — 输出格式加入 UUID

**表格渲染 — `internal/ui/table.go`:**
- L31-34: `RenderProcessTable()` — 签名需加 `showUUID` 参数

### 新增依赖

```
github.com/google/uuid  (v1.6.0+，需要 NewV7() 支持)
```

**注意：** 项目 go.mod 中目前没有此依赖，需要 `go get github.com/google/uuid`。

### UUID v7 生成代码模式

```go
import "github.com/google/uuid"

// NewProcess 中：
UUID: uuid.Must(uuid.NewV7()).String(),
```

`uuid.NewV7()` 返回 `(UUID, error)`，正常情况不会失败（仅依赖系统时钟和随机源）。使用 `uuid.Must()` 包装在极端情况下 panic 而非静默错误，符合进程创建的关键路径语义。

### RenderProcessTable 签名变更

当前签名：
```go
func RenderProcessTable(r *Renderer, procs []vfs.ProcInfo, verbose bool)
```

需要变更为支持 UUID 显示。两种方式：
1. **推荐**：新增 `showUUID bool` 参数 → `func RenderProcessTable(r *Renderer, procs []vfs.ProcInfo, verbose, showUUID bool)`
2. 替代：使用 options struct

选方案 1，因为改动最小。所有现有调用处（ps 默认/verbose、测试）传入 `false` 即可保持不变。

### OnSpawn 输出格式变更

当前 `OnSpawn` 回调（`cmd/rnix/main.go` L111-118）：
```go
func (c *cliCallbacks) OnSpawn(pid types.PID, intent, provider, model string) {
    // "spawning PID %d (%s/%s)..."
}
```

需要增加 UUID 参数。但 `OnSpawn` 是 `kernel.KernelCallbacks` 接口方法，修改接口签名会影响所有实现者。

**方案**：修改 `KernelCallbacks.OnSpawn` 签名新增 `uuid string` 参数。需同步更新：
- `kernel/kernel.go` 中调用 `OnSpawn` 的位置
- `cmd/rnix/main.go` 中 `cliCallbacks.OnSpawn` 实现
- 所有测试中的 mock/stub 实现

### IPC 远程 spawn 路径

IPC 远程 spawn 通过 `SpawnProgress` 事件流发送进度：
- `ipc/protocol.go` 中 `SpawnProgress` struct 已有 PID 字段
- 新增 UUID 字段到 `SpawnProgress`
- `ipc/server.go` spawn handler 填充 UUID
- `cmd/rnix/main.go` 远程 spawn 接收端同步显示

### 测试要点

1. **`kernel/process_test.go`（或新增）**：`NewProcess()` 生成合法 UUID v7 格式（`uuid.Parse()` 验证）
2. **UUID 唯一性**：循环 1000 次 `NewProcess()`，所有 UUID 不重复
3. **`ipc/protocol_test.go`**：ProcInfoToWire ↔ WireToProcInfo round-trip 保留 UUID
4. **`internal/ui/table_test.go`**：RenderProcessTable(showUUID=true) 输出包含 UUID
5. **已有测试兼容**：所有现有 `RenderProcessTable` 调用传 `showUUID=false`，确保不破坏

### 已有测试文件提醒

以下测试文件构建 `vfs.ProcInfo` 字面量，新增 UUID 字段后不需要改动（Go struct 零值 `""` 兼容），但验证测试应显式设置 UUID：
- `internal/ui/table_test.go`（大量 `RenderProcessTable` 测试）
- `cmd/rnix/main_test.go`（spawn 测试检查 `"spawning PID"` 输出）
- `ipc/protocol_test.go`（wire 转换测试）

### 回归风险

- `RenderProcessTable` 签名变更：所有调用处需更新（ps 默认/verbose + 所有测试）
- `KernelCallbacks.OnSpawn` 签名变更：所有实现者需更新
- `SpawnResponse` 新增字段：JSON 兼容（omitempty，旧客户端忽略）
- `ProcInfoWire` 新增字段：JSON 兼容（omitempty，旧客户端忽略）

### Project Structure Notes

- UUID 字段添加在 `kernel/process.go` 的 Process struct 中，紧跟 PID 字段
- ProcInfo 在 `vfs/proc.go` 中，与 Process 对应新增 UUID
- 保持 `internal/types` 不变——UUID 使用 `string` 类型而非自定义类型，因为它不需要类型安全运算
- 新增依赖通过 `go get` 管理，符合项目现有模式

### References

- [Architecture Decision 27: Process UUID v7 标识体系](../planning-artifacts/architecture/core-architectural-decisions.md#decision-27-process-uuid-v7-标识体系--pid-短标识--uuid-持久化唯一标识)
- [PRD FR177: Process UUID v7](../planning-artifacts/prd/functional-requirements.md#process-identity-system进程标识体系phase-2)
- [NFR65-obs: UUID 生成延迟 ≤ 1ms](../planning-artifacts/prd/non-functional-requirements.md#unified-observation-system-quality统一观察系统质量--dashboard-增强phase-2)
- [Sprint Change Proposal 2026-03-22](../planning-artifacts/sprint-change-proposal-2026-03-22.md)
- [Epic 28](../planning-artifacts/epics/epic-28-pid标识体系重构-process-identity-system.md)
- [google/uuid v7 实现](https://github.com/google/uuid/blob/master/version7.go)

## Dev Agent Record

### Agent Model Used

claude-4.6-opus-high-thinking (Cursor)

### Debug Log References

无调试日志。所有实现一次通过。

### Completion Notes List

- ✅ Task 1: `go get github.com/google/uuid` — v1.6.0，支持 NewV7()
- ✅ Task 2: Process struct 新增 `UUID string` 字段（PID 下方），NewProcess() 中 `uuid.Must(uuid.NewV7()).String()` 生成
- ✅ Task 3: vfs.ProcInfo 新增 `UUID string` 字段
- ✅ Task 4: GetProcInfo/ListProcs 填充 `UUID: proc.UUID`
- ✅ Task 5: ProcInfoWire 新增 UUID，SpawnResponse 新增 UUID，ProgressPayload 新增 UUID，ProcInfoToWire/WireToProcInfo 传递 UUID
- ✅ Task 6: KernelCallbacks.OnSpawn 签名新增 `uuid string` 参数。所有实现者已更新：cliCallbacks、callbackMux、testCallbacks、atdd36Callbacks、mockCallbacksWithUUID。spawn 输出格式 `spawning PID %d (uuid: %s, %s/%s)...`（UUID 截断前 12 字符 + "..."）。IPC server 的 compensating spawn event 和 SpawnResponse 均包含 UUID。
- ✅ Task 7: ps 命令新增 `--uuid` flag → `flagUUID`。RenderProcessTable 签名新增 `showUUID bool`。UUID 列宽 11（8 hex + "..."），放置在 PID 后。所有现有调用处传 `false` 保持向后兼容。
- ✅ Task 8: jsonProcess struct 新增 `UUID string`，renderPsJSON 赋值 `p.UUID`
- ✅ Task 9: ATDD 测试已通过（AC-1 至 AC-5）。所有现有测试已更新适配新签名。`make all`（lint + vet + test + build）全部通过。

### File List

- `go.mod` — 新增 github.com/google/uuid v1.6.0 依赖
- `go.sum` — 更新
- `kernel/process.go` — Process struct 新增 UUID 字段，NewProcess 生成 UUID v7
- `vfs/proc.go` — ProcInfo struct 新增 UUID 字段
- `kernel/kernel.go` — KernelCallbacks.OnSpawn 签名新增 uuid 参数；GetProcInfo/ListProcs 填充 UUID；Spawn 回调传递 UUID
- `ipc/protocol.go` — ProcInfoWire/SpawnResponse/ProgressPayload 新增 UUID 字段；ProcInfoToWire/WireToProcInfo 传递 UUID
- `ipc/server.go` — callbackMux.OnSpawn 签名更新；compensating spawn event 包含 UUID；SpawnResponse 包含 UUID
- `cmd/rnix/main.go` — cliCallbacks.OnSpawn 更新；远程 spawn 路径显示 UUID；ps 新增 --uuid flag；jsonProcess 新增 UUID；renderPsJSON 填充 UUID
- `internal/ui/table.go` — RenderProcessTable 新增 showUUID 参数；UUID 列渲染；truncateUUID 辅助函数
- `kernel/supervisor.go` — OnSpawn 调用更新
- `kernel/stem_integration_test.go` — testCallbacks.OnSpawn 签名更新
- `kernel/atdd_3_6_step_output_streaming_test.go` — atdd36Callbacks.OnSpawn 签名更新
- `cmd/rnix/main_test.go` — OnSpawn 调用更新
- `ipc/server_test.go` — OnSpawn 调用更新
- `internal/ui/table_test.go` — RenderProcessTable 调用新增 showUUID=false 参数
