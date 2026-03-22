# Story 28.4: Dashboard PID 有效性检查

Status: dev-complete

## Story

As a 平台构建者,
I want Dashboard 通过 UUID 验证进程同一性,
So that 进程死亡并被 Reaper 清理后，新进程复用 PID 不会导致 Dashboard 误显示。

## Acceptance Criteria

### AC-1: Dashboard 选中进程时同时记录 UUID

**Given** Dashboard 中用户在进程树中选中了某个进程
**When** 设置 `selectedPID`
**Then** 同时从 `flatRow.proc.UUID` 读取并设置 `selectedUUID`
**And** 后续所有数据获取使用 UUID 进行一致性验证

### AC-2: PID 复用时 UUID 不匹配检测

**Given** 旧进程（PID=3, UUID=A）死亡后，新进程复用 PID=3（UUID=B）
**When** Dashboard 持有 selectedPID=3, selectedUUID=A
**Then** 刷新时发现 PID=3 对应的进程 UUID 为 B ≠ A
**And** 判定为不同进程，清除旧选中状态（selectedPID=0, selectedUUID=""）
**And** 不会显示新进程的数据覆盖旧进程的视图

### AC-3: 进程被 Reaper 清理后正确清除选中状态

**Given** Dashboard 中选中了某个进程（selectedPID + selectedUUID）
**When** 该进程死亡并被 Reaper 清理（PID 不在进程列表中）
**Then** Dashboard 在下一次刷新时检测到 PID 对应的进程已不存在
**And** 正确清除选中状态
**And** 时间线窗格切换到空状态

### AC-4: procDetailCache 使用 UUID 作为缓存键

**Given** Dashboard Model 的 `procDetailCache`
**When** 改为使用 UUID 作为缓存键
**Then** 缓存键类型从 `map[types.PID]*ipc.GetProcDetailResponse` 变为 `map[string]*ipc.GetProcDetailResponse`
**And** PID 复用时不会命中错误的缓存

### AC-5: recording 映射使用 UUID 作为键

**Given** Dashboard 的 `recording` 映射
**When** 改为使用 UUID 作为键
**Then** 类型从 `map[types.PID]string` 变为 `map[string]string`
**And** PID 复用不会导致录制状态混淆

## Tasks / Subtasks

- [x] Task 1: dashboardModel 新增 selectedUUID 字段 (AC: #1)
  - [x]`cmd/rnix/dashboard.go`: 在 `dashboardModel` 结构体中新增 `selectedUUID string` 字段
  - [x]初始化函数 `newDashboardModel` 无需修改（string 零值为 ""）

- [x] Task 2: 选中进程时同时记录 UUID (AC: #1)
  - [x]所有 `m.selectedPID = m.treeRows[m.treeCursor].proc.PID` 赋值处，同时设置 `m.selectedUUID = m.treeRows[m.treeCursor].proc.UUID`
  - [x]涉及行号（约 8 处）：L437, L629, L639, L647, L696, L2138, L2189, L2223, L2233, L2304
  - [x]`handlePIDChange` 中 `selectedPID == 0` 时同步清空 `selectedUUID = ""`

- [x] Task 3: 刷新时验证 PID→UUID 一致性 (AC: #2, #3)
  - [x]`dashboardTick` 中 `listProcsCmd` 回调处理（约 L404-L440），在更新进程列表后添加验证逻辑
  - [x]验证逻辑：如果 `selectedPID > 0`，在新的 processes 列表中查找 `selectedPID`
    - 找到且 UUID 匹配 → 保持选中
    - 找到但 UUID 不匹配 → 清除选中状态（PID 复用检测）
    - 未找到 → 清除选中状态（进程已 reap）
  - [x]清除选中状态时调用 `handlePIDChange()` 触发级联清理

- [x] Task 4: procDetailCache 键类型改为 UUID (AC: #4)
  - [x]`dashboardModel` 中 `procDetailCache` 类型改为 `map[string]*ipc.GetProcDetailResponse`
  - [x]`newDashboardModel` 初始化改为 `make(map[string]*ipc.GetProcDetailResponse)`
  - [x]所有 `m.procDetailCache[m.selectedPID]` → `m.procDetailCache[m.selectedUUID]`
  - [x]所有 `m.procDetailCache[msg.pid]` → 改用 UUID 索引
  - [x]`procDetailResultMsg` 新增 `uuid string` 字段，`fetchProcDetailCmd` 传递 UUID
  - [x]`delete(m.procDetailCache, m.selectedPID)` → `delete(m.procDetailCache, m.selectedUUID)`

- [x] Task 5: recording 映射键类型改为 UUID (AC: #5)
  - [x]`recording` 类型改为 `map[string]string`
  - [x]`newDashboardModel` 初始化改为 `make(map[string]string)`
  - [x]`m.recording[m.selectedPID]` → `m.recording[m.selectedUUID]`
  - [x]`recordToggleMsg` 新增 `uuid string` 字段
  - [x]状态栏录制标识检查改用 UUID

- [x] Task 6: 测试 (AC: #1-#5)
  - [x]ATDD: 选中进程时 selectedUUID 被正确设置
  - [x]ATDD: PID 复用但 UUID 不同时，选中状态被清除
  - [x]ATDD: 进程从列表消失时，选中状态被清除
  - [x]ATDD: procDetailCache 使用 UUID 键不会跨进程命中
  - [x]确保 `make all` 通过

## Dev Notes

### 架构约束

- **UUID 已经可用**：Story 28-1 在 `vfs.ProcInfo` 和 `ipc.ProcInfoWire` 中添加了 `UUID string` 字段。`flatRow.proc` 类型为 `vfs.ProcInfo`，已包含 UUID。无需额外 IPC 调用获取 UUID。
- **不增加 IPC 调用**：UUID 验证逻辑完全在本地完成——`ListProcs` 返回的 `ProcInfoWire` 已包含 UUID，Dashboard 只需对比内存中的 selectedUUID 与列表中的 UUID。
- **向后兼容**：UUID 为空字符串时（极端场景），回退到仅 PID 检查行为。

### 关键修改文件

| 文件 | 修改内容 |
|------|---------|
| `cmd/rnix/dashboard.go` | 新增 `selectedUUID`；缓存键改 UUID；PID→UUID 验证逻辑 |

**单文件修改**，不涉及其他包。

### 源代码关键位置

**dashboardModel 结构体 — `cmd/rnix/dashboard.go` L178-255:**
```go
type dashboardModel struct {
    // ...
    selectedPID types.PID       // L183 — 当前选中进程 PID
    // 新增: selectedUUID string  — 当前选中进程 UUID
    processes   []vfs.ProcInfo  // L184 — 进程列表（已含 UUID）
    treeRows    []flatRow       // L185 — 树形行（flatRow.proc 类型为 vfs.ProcInfo）
    // ...
    procDetailCache map[types.PID]*ipc.GetProcDetailResponse  // L238 — 改为 map[string]
    recording    map[types.PID]string  // L215 — 改为 map[string]string
}
```

**flatRow 结构体 — `cmd/rnix/top.go` L35-39:**
```go
type flatRow struct {
    proc   vfs.ProcInfo  // proc.UUID 已可用
    prefix string
    depth  int
}
```

**ProcInfo UUID 字段 — `vfs/proc.go` L31:**
```go
type ProcInfo struct {
    PID  types.PID
    UUID string    // ← Story 28-1 添加，所有进程已有值
    // ...
}
```

### 选中进程时的 UUID 记录

所有设置 `selectedPID` 的位置需同步设置 `selectedUUID`。建议提取辅助方法：

```go
func (m *dashboardModel) selectProcess(row flatRow) {
    m.selectedPID = row.proc.PID
    m.selectedUUID = row.proc.UUID
}
```

然后将所有 `m.selectedPID = m.treeRows[m.treeCursor].proc.PID` 替换为 `m.selectProcess(m.treeRows[m.treeCursor])`。

涉及约 10 处赋值点（L437, L629, L639, L647, L696, L2138, L2189, L2223, L2233, L2304）。

### PID→UUID 验证逻辑

在 `dashboardTick` 中处理 `listProcsMsg` 后添加验证：

```go
// 进程列表更新后，验证选中进程 UUID 一致性
if m.selectedPID > 0 && m.selectedUUID != "" {
    found := false
    for _, p := range m.processes {
        if p.PID == m.selectedPID {
            if p.UUID == m.selectedUUID {
                found = true // PID 和 UUID 都匹配
            }
            // PID 匹配但 UUID 不匹配 → PID 被复用，不设 found
            break
        }
    }
    if !found {
        m.selectedPID = 0
        m.selectedUUID = ""
        m2, cmd := m.handlePIDChange()
        m = m2
        cmds = append(cmds, cmd)
    }
}
```

### procDetailResultMsg 改造

当前结构体：
```go
type procDetailResultMsg struct {
    pid    types.PID                     // L88
    detail *ipc.GetProcDetailResponse    // L89
    err    error                         // L90
}
```

改为：
```go
type procDetailResultMsg struct {
    pid    types.PID
    uuid   string                        // 新增
    detail *ipc.GetProcDetailResponse
    err    error
}
```

`fetchProcDetailCmd` 签名改为 `fetchProcDetailCmd(pid types.PID, uuid string)` 或直接使用 UUID 传递。

### 缓存键迁移

所有 `procDetailCache` 访问点：

| 位置 | 当前代码 | 改为 |
|------|---------|------|
| L266 | `make(map[types.PID]...)` | `make(map[string]...)` |
| L348 | `m.procDetailCache[msg.pid]` | `m.procDetailCache[msg.uuid]` |
| L463 | `delete(m.procDetailCache, m.selectedPID)` | `delete(m.procDetailCache, m.selectedUUID)` |
| L468 | `m.procDetailCache[m.selectedPID]` | `m.procDetailCache[m.selectedUUID]` |
| L807 | `m.procDetailCache[m.selectedPID]` | `m.procDetailCache[m.selectedUUID]` |

`recording` 映射同理，约 3-4 处访问点（L215, L263, L728-729, L860）。

### handlePIDChange 修改

`handlePIDChange` (L1791-1824) 在 selectedPID=0 时需同步清空 selectedUUID：
```go
func (m dashboardModel) handlePIDChange() (dashboardModel, tea.Cmd) {
    if m.selectedPID == 0 {
        m.selectedUUID = ""  // 新增
        // ... 原有清理逻辑
    }
    // ...
}
```

### initialPIDFocus 处理（L2304）

`--pid` flag 初始化时从 processes 中查找 UUID：
```go
model.selectedPID = types.PID(focusPID)
// 新增：查找对应 UUID
for _, p := range model.processes {
    if p.PID == model.selectedPID {
        model.selectedUUID = p.UUID
        break
    }
}
```

### 状态栏 PID 显示

状态栏已显示 PID（如 L1276 `fmt.Fprintf(&b, " | PID %d", m.selectedPID)`），无需修改。Detail Pane 已在 L2500 显示 UUID 前 8 字符。

### 回归风险

- **低风险**：所有修改限于 `cmd/rnix/dashboard.go` 单文件
- **缓存键类型变更**：编译器会在所有访问点报错，不会遗漏
- **现有 ATDD 测试**：27-3/27-4/27-5/27-6/27-7 的 dashboard 测试需要适配新的缓存键类型
- **selectProcess 辅助方法**：可能需要更新使用 `m.selectedPID = ...` 模式的测试代码

### 测试策略

新增 ATDD 测试文件 `cmd/rnix/atdd_28_4_dashboard_pid_validity_test.go`：

1. **TestDashboard_SelectProcessSetsUUID** — 选中进程时 selectedUUID 与 proc.UUID 一致
2. **TestDashboard_PIDReuseDetection** — 模拟 PID 复用（同 PID 不同 UUID），验证选中被清除
3. **TestDashboard_ProcessReapClearsSelection** — 进程从列表消失，验证选中被清除
4. **TestDashboard_ProcDetailCacheByUUID** — 缓存按 UUID 存取，PID 复用不命中旧缓存
5. **TestDashboard_RecordingByUUID** — recording 映射按 UUID 存取

测试模式参照现有 `cmd/rnix/atdd_27_6_dashboard_detail_panel_test.go`：直接构造 `dashboardModel`，设置 `treeRows` 和 `processes`，调用 `Update` 方法验证状态变更。

### Story 28-3 完成后的代码状态

- IPC 所有请求结构体已新增 `UUID string` 字段
- `resolveProcess(pid, uuid)` 和 `resolveStepsPath(pid, uuid)` 辅助方法已就绪
- 客户端新增 `GetStepDetailByUUID` 和 `ListStepsByUUID` 方法
- Dashboard 当前**仍使用纯 PID 查询**，本 Story 添加 UUID 验证层

### 不在本 Story 范围内

- Dashboard 数据获取切换为 UUID-first 查询（使用 `GetStepDetailByUUID` 等）— 可作为后续优化
- top 命令的 UUID 验证 — top 是实时视图，无需持久标识
- 离线回放模式的 UUID 适配 — 回放已使用录制文件，不受 PID 复用影响

### Project Structure Notes

- 修改限于 `cmd/rnix/dashboard.go` 单文件
- `flatRow` 定义在 `cmd/rnix/top.go`，无需修改（已含 `proc vfs.ProcInfo`）
- `vfs.ProcInfo` 已含 UUID 字段（Story 28-1），无需修改
- 测试新增 `cmd/rnix/atdd_28_4_dashboard_pid_validity_test.go`

### References

- [Epic 28: PID 标识体系重构](../planning-artifacts/epics/epic-28-pid标识体系重构-process-identity-system.md)
- [Story 28-1: Process UUID v7 引入](./28-1-process-uuid-v7-introduction.md)
- [Story 28-2: StepRecord 路径迁移到 UUID](./28-2-steprecord-path-migration-to-uuid.md)
- [Story 28-3: IPC PID→UUID 映射](./28-3-ipc-pid-uuid-mapping.md)
- [PRD FR180: Dashboard PID 有效性验证](../planning-artifacts/prd/functional-requirements.md#process-identity-system进程标识体系phase-2)

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6

### Debug Log References

N/A

### Completion Notes List

- Added `selectedUUID string` field to `dashboardModel`
- Created `selectProcess(row flatRow)` helper method to atomically set PID+UUID
- Replaced all 8 direct `m.selectedPID = m.treeRows[...].proc.PID` assignments with `m.selectProcess()`
- Added PID→UUID validation logic in `dashboardTick` after process list refresh (AC-2, AC-3)
- `handlePIDChange()` clears `selectedUUID` when `selectedPID == 0`
- Changed `procDetailCache` key type from `map[types.PID]` to `map[string]` (UUID-keyed)
- Changed `recording` key type from `map[types.PID]string` to `map[string]string` (UUID-keyed)
- Updated `procDetailResultMsg` and `recordToggleMsg` structs with `uuid string` field
- Updated `fetchProcDetailCmd` and `toggleRecordCmd` signatures to accept UUID
- Intent tree PID selection now also captures UUID from process list
- Updated existing dashboard tests (`mockDashboardProcs`, recording tests) for UUID compatibility
- All 8 ATDD tests pass, `make all` passes (0 lint issues, 22 packages)

### File List

- `cmd/rnix/dashboard.go` — Core implementation: selectedUUID field, selectProcess helper, UUID validation, cache/recording key migration
- `cmd/rnix/atdd_28_4_dashboard_pid_validity_test.go` — 8 ATDD tests rewritten for green phase
- `cmd/rnix/dashboard_test.go` — Updated mockDashboardProcs (added UUID), recording tests adapted to UUID keys
