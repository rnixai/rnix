# Story 28.2: StepRecord 路径迁移到 UUID

Status: done

## Story

As a 平台构建者,
I want 所有持久化数据路径使用 UUID 而非 PID,
So that daemon 重启后新进程不会覆盖旧进程的 StepRecord 数据。

## Acceptance Criteria

### AC-1: StepWriter 目录使用 UUID

**Given** StepWriter 创建步骤目录
**When** 进程首次进入 reasonStep 循环
**Then** 目录路径从 `.rnix/data/steps/<pid>/` 变更为 `.rnix/data/steps/<uuid>/`
**And** 文件名仍为 `steps.jsonl`

### AC-2: process-meta.json 使用 UUID 路径并包含 PID

**Given** process-meta.json 写入
**When** reaper 清理进程时
**Then** 路径从 `.rnix/data/steps/<pid>/process-meta.json` 变更为 `.rnix/data/steps/<uuid>/process-meta.json`
**And** process-meta.json 中包含 `pid` 字段（用于反向查找）

### AC-3: 旧 PID 目录向后兼容

**Given** 旧格式的 `.rnix/data/steps/<pid>/` 目录
**When** 系统遇到纯数字目录名
**Then** 保持向后兼容——可读取但新写入使用 UUID 路径

### AC-4: `rnix record list` 显示 step 会话

**Given** `rnix record list` 命令
**When** 列出已记录的进程
**Then** 扫描 `.rnix/data/steps/` 下所有子目录
**And** UUID 格式目录显示 UUID + 从 meta 中读取的 PID
**And** 旧 PID 格式目录标记为 "(legacy)"

### AC-5: `rnix replay <id>` 支持 UUID 前缀

**Given** `rnix replay <id>` 命令
**When** 指定 UUID 前缀
**Then** 匹配 `.rnix/data/steps/<uuid>/` 目录
**And** 加载 steps.jsonl 进行回放

### AC-6: IPC 路径解析使用 UUID

**Given** IPC 方法（GetStepDetail、ListSteps）
**When** 解析 steps.jsonl 路径
**Then** 对于内存中存活进程：使用 `proc.UUID` 构建路径
**And** 对于已 reap 进程：通过 `GetStepDataDir` + UUID 查找

## Tasks / Subtasks

- [x] Task 1: 修改 `NewStepWriter` 签名，使用 UUID 替代 PID (AC: #1)
  - [x] `kernel/step_writer.go`: 参数从 `pid types.PID` 改为 `procUUID string`
  - [x] 目录路径从 `fmt.Sprintf("%d", pid)` 改为直接使用 `procUUID`
- [x] Task 2: 修改 kernel reasonStep 中 StepWriter 创建 (AC: #1)
  - [x] `kernel/kernel.go` L1035: `NewStepWriter(stepBaseDir, proc.PID)` → `NewStepWriter(stepBaseDir, proc.UUID)`
- [x] Task 3: process-meta.json 新增 PID 字段 (AC: #2)
  - [x] `kernel/reap.go` L71-76: meta struct 新增 `PID types.PID` 字段
  - [x] 序列化时赋值 `PID: proc.PID`
- [x] Task 4: IPC 路径解析迁移到 UUID (AC: #6)
  - [x] `ipc/server.go` `resolveStepsPathFromProc()`: 使用 `proc.UUID` 替代 `fmt.Sprintf("%d", pid)`
  - [x] `ipc/server.go` `resolveStepsPathFallback()`: 重构为扫描 UUID 目录 + legacy PID fallback
  - [x] `handleGetStepDetail()`: 从进程表获取 UUID 用于路径解析
  - [x] `handleListSteps()`: 同上
- [x] Task 5: `rnix record list` 扩展显示 step 会话 (AC: #4)
  - [x] `cmd/rnix/record.go`: 扩展 `runRecordList` 扫描 `.rnix/data/steps/` 子目录
  - [x] 判断子目录名格式：UUID 格式 vs 纯数字（legacy）
  - [x] UUID 目录：读取 process-meta.json 提取 PID
  - [x] 输出格式：UUID（前 8 字符） + PID + "(legacy)" 标记
- [x] Task 6: `rnix replay` 支持 UUID 前缀匹配 (AC: #5)
  - [x] `cmd/rnix/replay.go`: 当 record-id 不匹配 debug recording 时，尝试匹配 step UUID 目录
  - [x] UUID 前缀匹配逻辑：扫描 `.rnix/data/steps/` 找前缀匹配的目录
  - [x] 匹配到 steps.jsonl 后以 step-by-step 模式回放
- [x] Task 7: 测试更新 (AC: #1-#6)
  - [x] 更新 `kernel/atdd_27_1_step_record_test.go` 中路径构建使用 UUID
  - [x] 更新 `ipc/atdd_27_2_getstepdetail_test.go` 中 `writeTestSteps` 使用 UUID
  - [x] 更新 `ipc/atdd_27_3_liststeps_test.go` 中路径构建
  - [x] 新增 ATDD 测试：StepWriter 使用 UUID 路径、process-meta.json 包含 PID、旧 PID 目录向后兼容
  - [x] 确保 `make all` 通过

## Dev Notes

### 架构约束（Architecture Decision 27）

- **路径迁移**：`.rnix/data/steps/<pid>/` → `.rnix/data/steps/<uuid>/`
- **迁移策略**：全量清空，不保留旧数据（StepRecord 是调试辅助数据）。但代码需向后兼容可读取旧格式。
- **process-meta.json 扩展**：新增 `pid` 字段用于 UUID→PID 反向查找
- **7 天自动清理策略不变**：基于目录修改时间

### 关键修改文件

| 文件 | 修改内容 |
|------|---------|
| `kernel/step_writer.go` | `NewStepWriter` 签名改用 `procUUID string`，目录使用 UUID |
| `kernel/kernel.go` | `reasonStep()` L1035: 传 `proc.UUID` 给 `NewStepWriter` |
| `kernel/reap.go` | process-meta.json struct 新增 `PID` 字段 |
| `ipc/server.go` | `resolveStepsPathFromProc`/`resolveStepsPathFallback` 使用 UUID |
| `cmd/rnix/record.go` | `runRecordList` 扩展扫描 step 目录 |
| `cmd/rnix/replay.go` | `runReplay` 支持 UUID 前缀匹配 step 目录 |

### 源代码关键位置（精确行号）

**StepWriter — `kernel/step_writer.go`:**
- L22-37: `NewStepWriter(baseDir string, pid types.PID)` — 需改签名
- L24: `filepath.Join(baseDir, "data", "steps", fmt.Sprintf("%d", pid))` — 改为 `procUUID`
- 其余方法（WriteStep、Close、ReadStep、ReadAllSteps）不需修改，它们只操作文件内容

**kernel reasonStep — `kernel/kernel.go`:**
- L1027-1042: StepWriter 初始化代码块
- L1035: `sw, err := NewStepWriter(stepBaseDir, proc.PID)` — 改为 `proc.UUID`
- `proc.UUID` 在 Story 28-1 中已添加（`kernel/process.go` Process struct）

**Reaper — `kernel/reap.go`:**
- L61-93: process-meta.json 写入逻辑
- L70: `metaDir := filepath.Dir(sw.file.Name())` — 自动跟随 StepWriter 路径，无需改动
- L71-76: meta struct 定义 — 需新增 `PID types.PID`
- 路径变更由 StepWriter 的 UUID 目录自动传导，reap.go 只需改 meta struct

**IPC 路径解析 — `ipc/server.go`:**
- L1947-1958: `resolveStepsPathFromProc(proc, pid)` — 改为使用 `proc.UUID`
- L1961-1969: `resolveStepsPathFallback(pid)` — 需重构，可能需要新增 UUID 参数
- L1847-1854: `handleGetStepDetail` 中调用路径解析 — 需传递 UUID
- L1914-1918: `handleListSteps` 中调用路径解析 — 同上
- **关键**：`proc, procFound := s.kern.GetProcess(req.PID)` — 进程在内存时可从 `proc.UUID` 获取；不在内存时需要新策略

**IPC fallback 策略变更**：
当进程不在内存时（已被 reap），当前 `resolveStepsPathFallback(pid)` 使用 PID 构建路径。迁移后 PID 路径不再存在。两种方案：
1. **扫描 `.rnix/data/steps/` 目录**：找包含 PID 的 process-meta.json → 性能差
2. **要求客户端传 UUID**：Story 28-3 扩展请求体加 UUID 字段 → 依赖后续 Story

**推荐方案**：Story 28-2 范围内，当进程在内存时使用 `proc.UUID` 路径；不在内存时保留当前 PID fallback（兼容旧数据），并新增 UUID fallback 路径尝试。两条路径先后尝试：`steps/<uuid>/` → `steps/<pid>/`（legacy）。等 Story 28-3 完成后客户端传 UUID，PID fallback 可逐步移除。

```go
func (s *Server) resolveStepsPathFallback(pid types.PID, uuid string) string {
    base := s.kern.GetStepDataDir()
    if base == "" {
        return ""
    }
    // 优先 UUID 路径
    if uuid != "" {
        p := filepath.Join(base, "data", "steps", uuid, "steps.jsonl")
        if _, err := os.Stat(p); err == nil {
            return p
        }
    }
    // Legacy PID fallback
    pidStr := fmt.Sprintf("%d", pid)
    return filepath.Join(base, "data", "steps", pidStr, "steps.jsonl")
}
```

**record.go — `cmd/rnix/record.go`:**
- L139-188: `runRecordList` — 当前仅列出 syscall recordings（通过 IPC `RecordList`）
- 扩展：在 IPC 列表后，本地扫描 `.rnix/data/steps/` 目录列出 step 会话
- 判断目录名：UUID 格式（含 `-` 且长度 36）vs 纯数字（legacy PID）
- UUID 目录读 `process-meta.json` 获取 PID

**replay.go — `cmd/rnix/replay.go`:**
- L34-53: `runReplay` — 当前仅匹配 debug recordings
- L41-42: `mgr.FindRecord(recordID)` — 查找 `.rnix/data/records/`
- 扩展：当 FindRecord 失败时，尝试 UUID 前缀匹配 `.rnix/data/steps/` 目录
- 匹配逻辑：扫描 steps 子目录，找到以 `recordID` 为前缀的 UUID 目录
- 匹配成功后加载 steps.jsonl，进入 step-by-step 回放模式

### Story 28-1 完成后的代码状态

- `Process.UUID` 字段已存在（`kernel/process.go`），由 `uuid.Must(uuid.NewV7()).String()` 生成
- `ProcInfoWire.UUID` 已存在（`ipc/protocol.go`），客户端可获取 UUID
- `rnix ps --uuid` 已实现，可查看进程 UUID
- 所有路径仍使用 PID —— 这是本 Story 的核心变更

### 路径变更影响分析

变更 `NewStepWriter` 签名 (`pid types.PID` → `procUUID string`) 后：
- `kernel/kernel.go` L1035 唯一调用处更新
- `kernel/step_writer.go` 内部只有 L24 目录构建需改
- **传导效应**：`reap.go` L70 `metaDir := filepath.Dir(sw.file.Name())` 自动使用新路径，无需改动
- **IPC 路径解析**：`resolveStepsPathFromProc` 从 PID 改为 UUID 后，`handleGetStepDetail` 和 `handleListSteps` 通过 `proc.UUID` 获取 UUID

### process-meta.json 格式变更

当前格式：
```json
{"system_prompt":"...","tool_defs":[...]}
```

新增 PID 后：
```json
{"pid":3,"system_prompt":"...","tool_defs":[...]}
```

修改位置：`kernel/reap.go` L71-76 的匿名 struct，以及 `ipc/server.go` L1971-1974 的 `processMetaFile` struct。

### `record list` step 会话输出格式

在现有 syscall recording 列表之后，新增分隔区域显示 step 会话：

```
RECORD-ID       PID   STATUS       EVENTS   START                INTENT
42-1709856000   1     completed    156      2026-03-22 10:00:00  analyze code

Step Sessions:
UUID                                 PID    STEPS   MODIFIED
019576f2-...                          3      12     2026-03-22 10:15:00
019576f3-...                          5       8     2026-03-22 10:20:00
7 (legacy)                            7       3     2026-03-21 09:00:00
```

扫描逻辑：本地操作，不需要 daemon 连接。`resolveDataDir(cwd, "steps")` 获取路径。

### UUID 格式判断

```go
func isUUIDDir(name string) bool {
    _, err := uuid.Parse(name)
    return err == nil
}

func isLegacyPIDDir(name string) bool {
    _, err := strconv.Atoi(name)
    return err == nil
}
```

使用 `github.com/google/uuid`（Story 28-1 已添加依赖）的 `uuid.Parse()` 判断。

### 回归风险

- `NewStepWriter` 签名变更：唯一调用处在 `kernel/kernel.go` L1035
- IPC `resolveStepsPathFromProc` 签名变更：被 `handleGetStepDetail` 和 `handleListSteps` 调用
- `resolveStepsPathFallback` 变更：同上两个 handler 调用
- `processMetaFile` struct 新增 PID 字段：`ipc/server.go` `readProcessMeta` 读取时 JSON 兼容（新字段可选）
- **测试路径**：所有 `writeTestSteps(t, baseDir, pid, ...)` 辅助函数需更新签名使用 UUID
- 现有 step 数据（`.rnix/data/steps/<pid>/`）在开发机上可能存在——不影响功能，旧数据不会被新进程覆盖

### 测试要点

1. **`NewStepWriter` UUID 路径**：创建 `StepWriter`，验证目录为 `data/steps/<uuid>/`
2. **process-meta.json 包含 PID**：通过 reaper 写入后读取验证 `pid` 字段存在且正确
3. **IPC UUID 路径解析**：进程在内存时 `handleGetStepDetail` 使用 UUID 路径
4. **Legacy 兼容**：手动创建 `data/steps/42/steps.jsonl`，验证 fallback 可读取
5. **`record list` step 会话**：创建 UUID 和 PID 目录，验证输出格式
6. **`replay` UUID 前缀**：UUID 前缀匹配正确目录

### 测试辅助函数更新

以下测试辅助函数构建 `data/steps/<pid>/` 路径，需改为使用 UUID：

- `ipc/atdd_27_2_getstepdetail_test.go` L576-594: `writeTestSteps(t, baseDir, pid, records)` → `writeTestSteps(t, baseDir, procUUID, records)`
- `ipc/atdd_27_3_liststeps_test.go`: 同上
- `kernel/atdd_27_1_step_record_test.go` L475-476, L535-536: 路径构建

### Project Structure Notes

- 路径变更限于 `kernel/`、`ipc/`、`cmd/rnix/` 三个包
- `internal/types/step_record.go` StepRecord struct 无需修改（不含路径信息）
- `debug/` 包的 recording 系统（`.rnix/data/records/`）不受影响
- 新增依赖：无（`github.com/google/uuid` 已在 Story 28-1 添加）

### References

- [Architecture Decision 27: Process UUID v7 标识体系](../planning-artifacts/architecture/core-architectural-decisions.md#decision-27-process-uuid-v7-标识体系--pid-短标识--uuid-持久化唯一标识)
- [PRD FR178: 持久化路径用 UUID](../planning-artifacts/prd/functional-requirements.md#process-identity-system进程标识体系phase-2)
- [NFR60-obs: StepRecord 写入性能](../planning-artifacts/prd/non-functional-requirements.md#unified-observation-system-quality统一观察系统质量--dashboard-增强phase-2)
- [Epic 28](../planning-artifacts/epics/epic-28-pid标识体系重构-process-identity-system.md)
- [Story 28-1 完成记录](./28-1-process-uuid-v7-introduction.md)

## Dev Agent Record

### Agent Model Used
claude-4.6-opus-high-thinking (Cursor)

### Debug Log References
N/A — 全量 ATDD 测试在红阶段已由 Story 28-1 ATDD 流程创建，本次为绿阶段实现。

### Completion Notes List

- Task 1-2: `NewStepWriter` 签名从 `pid types.PID` → `procUUID string`，`kernel.go` reasonStep 传 `proc.UUID`
- Task 3: `reap.go` process-meta.json 匿名 struct 新增 `PID types.PID` 字段，序列化时写入 `proc.PID`
- Task 4: IPC `resolveStepsPathFromProc` 使用 `proc.UUID` 构建路径；`resolveStepsPathFallback` 重构为扫描 UUID 目录 process-meta.json 匹配 PID + legacy PID path fallback；`processMetaFile` struct 新增 `PID` 字段
- Task 5: `record.go` 扩展 `runRecordList` 在 syscall recording 列表之后显示本地 step sessions（UUID + legacy）；新增 `listStepSessions`/`printStepSessions`/`scanStepSessions`/`isUUIDDir`/`isLegacyPIDDir`/`matchStepUUIDPrefix` 辅助函数
- Task 6: `replay.go` 当 debug recording 查找失败时尝试 UUID 前缀匹配 `.rnix/data/steps/` 目录，匹配成功后进入 step-by-step 交互式回放模式
- Task 7: 更新 `kernel/atdd_27_1_step_record_test.go`（5 处 NewStepWriter 调用改为 string UUID + 2 处集成测试路径改用 proc.UUID）；`ipc/atdd_27_2_getstepdetail_test.go`（6 处 writeTestSteps→writeTestStepsUUID）；`ipc/atdd_27_3_liststeps_test.go`（7 处 writeTestSteps→writeTestStepsUUID）；`ipc/atdd_28_2_uuid_path_test.go` fallback ListSteps 测试补充 process-meta.json；`cmd/rnix/atdd_28_2_record_replay_uuid_test.go` 移除重复类型定义

### File List

- `kernel/step_writer.go` — NewStepWriter 签名改为 procUUID string
- `kernel/kernel.go` — reasonStep 传 proc.UUID 给 NewStepWriter
- `kernel/reap.go` — process-meta.json struct 新增 PID 字段
- `ipc/server.go` — resolveStepsPathFromProc/resolveStepsPathFallback 使用 UUID；processMetaFile 新增 PID；新增 isUUIDDir helper；新增 uuid import
- `cmd/rnix/record.go` — 扩展 runRecordList 显示 step sessions；新增辅助函数和 StepSessionEntry 类型
- `cmd/rnix/replay.go` — UUID 前缀匹配 fallback 到 step session 回放；新增 runStepReplay/findStepsByUUIDPrefix/printStepRecord
- `kernel/atdd_27_1_step_record_test.go` — 更新 NewStepWriter 调用和路径构建使用 UUID
- `ipc/atdd_27_2_getstepdetail_test.go` — 运行中进程测试改用 writeTestStepsUUID
- `ipc/atdd_27_3_liststeps_test.go` — 运行中进程测试改用 writeTestStepsUUID
- `ipc/atdd_28_2_uuid_path_test.go` — 补充 fallback ListSteps 测试的 process-meta.json
- `cmd/rnix/atdd_28_2_record_replay_uuid_test.go` — 移除重复 StepSessionEntry 类型定义
