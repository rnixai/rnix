---
stepsCompleted:
  - 'step-01-preflight-and-context'
  - 'step-02-generation-mode'
  - 'step-03-test-strategy'
  - 'step-04-generate-tests'
lastStep: 'step-04-generate-tests'
lastSaved: '2026-03-22'
storyId: '28-3'
storyTitle: 'IPC PID→UUID 映射'
detectedStack: 'backend'
generationMode: 'ai-generation'
inputDocuments:
  - '_bmad-output/implementation-artifacts/28-3-ipc-pid-uuid-mapping.md'
  - 'ipc/protocol.go'
  - 'ipc/server.go'
  - 'kernel/kernel.go'
  - 'ipc/atdd_28_2_uuid_path_test.go'
  - 'ipc/atdd_28_1_uuid_wire_test.go'
---

# ATDD Checklist — Story 28.3: IPC PID→UUID 映射

## Step 1: Preflight & Context

- **Stack**: `backend` (Go 1.26, `go.mod` detected)
- **Test framework**: Go testing + `go test -race`
- **Story**: 28-3 IPC PID→UUID 映射 (status: ready-for-dev)
- **Acceptance criteria**: 6 ACs covering PID→UUID mapping, UUID direct query, struct extension, reaped process handling, and uniform IPC support
- **Existing patterns**: `ipc/atdd_28_1_uuid_wire_test.go`, `ipc/atdd_28_2_uuid_path_test.go` (setupTestServer, testStepRecord, writeTestStepsUUID helpers)

## Step 2: Generation Mode

- **Mode**: AI Generation (backend project, no browser recording needed)

## Step 3: Test Strategy

### AC-to-Test Mapping

| AC | 场景 | 测试级别 | 优先级 | 测试函数 |
|----|------|---------|--------|---------|
| AC-3 | GetStepDetailRequest 新增 UUID 字段 | Unit (编译期) | P0 | `TestATDD_28_3_AC3_GetStepDetailRequest_HasUUIDField` |
| AC-3 | ListStepsRequest 新增 UUID 字段 | Unit (编译期) | P0 | `TestATDD_28_3_AC3_ListStepsRequest_HasUUIDField` |
| AC-3 | GetProcDetailRequest 新增 UUID 字段 | Unit (编译期) | P0 | `TestATDD_28_3_AC3_GetProcDetailRequest_HasUUIDField` |
| AC-3 | KillRequest 新增 UUID 字段 | Unit (编译期) | P0 | `TestATDD_28_3_AC3_KillRequest_HasUUIDField` |
| AC-3 | AttachDebugRequest 新增 UUID 字段 | Unit (编译期) | P0 | `TestATDD_28_3_AC3_AttachDebugRequest_HasUUIDField` |
| AC-3 | UUID 字段 JSON omitempty | Unit | P0 | `TestATDD_28_3_AC3_UUID_JSON_OmitEmpty` |
| AC-3 | UUID 优先于 PID (序列化) | Unit | P1 | `TestATDD_28_3_AC3_UUID_Priority_Over_PID` |
| AC-1 | GetStepDetail PID 查询存活进程 | Integration | P0 | `TestATDD_28_3_AC1_GetStepDetail_PID_LiveProcess` |
| AC-2 | GetStepDetail UUID 直接查询存活进程 | Integration | P0 | `TestATDD_28_3_AC2_GetStepDetail_UUID_LiveProcess` |
| AC-2 | ListSteps UUID 查询存活进程 | Integration | P0 | `TestATDD_28_3_AC2_ListSteps_UUID_LiveProcess` |
| AC-4 | GetStepDetail PID 查询已 reap 进程 → not_found | Integration | P0 | `TestATDD_28_3_AC4_GetStepDetail_PID_ReapedProcess_NotFound` |
| AC-4 | ListSteps PID 查询已 reap 进程 → not_found | Integration | P0 | `TestATDD_28_3_AC4_ListSteps_PID_ReapedProcess_NotFound` |
| AC-5 | GetStepDetail UUID 查询已 reap 进程 → 磁盘读取 | Integration | P0 | `TestATDD_28_3_AC5_GetStepDetail_UUID_ReapedProcess_DiskRead` |
| AC-5 | ListSteps UUID 查询已 reap 进程 → 磁盘读取 | Integration | P0 | `TestATDD_28_3_AC5_ListSteps_UUID_ReapedProcess_DiskRead` |
| AC-6 | Kill by UUID 存活进程 | Integration | P1 | `TestATDD_28_3_AC6_Kill_ByUUID_LiveProcess` |
| AC-6 | Kill by UUID 不存在 → 失败 | Integration | P1 | `TestATDD_28_3_AC6_Kill_ByUUID_NotFound` |
| AC-6 | AttachDebug by UUID | Integration | P1 | `TestATDD_28_3_AC6_AttachDebug_ByUUID` |
| AC-6 | GetProcDetail by UUID | Integration | P1 | `TestATDD_28_3_AC6_GetProcDetail_ByUUID` |
| — | GetProcessByUUID 基本查找 | Unit | P0 | `TestATDD_28_3_GetProcessByUUID_BasicLookup` |
| — | GetProcessByUUID 未找到 | Unit | P0 | `TestATDD_28_3_GetProcessByUUID_NotFound` |
| — | GetProcessByUUID 空 UUID | Unit | P1 | `TestATDD_28_3_GetProcessByUUID_EmptyUUID` |
| — | GetProcessByUUID 多进程中查找 | Unit | P0 | `TestATDD_28_3_GetProcessByUUID_AmongMultiple` |
| — | GetProcessByUUID Zombie 进程仍可找到 | Unit | P1 | `TestATDD_28_3_GetProcessByUUID_ZombieProcess_StillInTable` |
| — | resolveProcess UUID 优先逻辑 | Unit | P0 | `TestATDD_28_3_ResolveProcess_UUIDPriority` |
| — | resolveProcess PID fallback | Unit | P0 | `TestATDD_28_3_ResolveProcess_FallbackToPID` |
| — | resolveProcess 两者为空 | Unit | P1 | `TestATDD_28_3_ResolveProcess_BothEmpty` |
| — | Client GetStepDetailByUUID roundtrip | Integration | P1 | `TestATDD_28_3_ClientRoundtrip_GetStepDetail_ByUUID` |
| — | Client ListStepsByUUID roundtrip | Integration | P1 | `TestATDD_28_3_ClientRoundtrip_ListSteps_ByUUID` |

## Step 4: Test Generation

### Test Files Generated

| 文件 | 测试数量 | 测试级别 | TDD 阶段 |
|------|---------|---------|---------|
| `ipc/atdd_28_3_pid_uuid_mapping_test.go` | 24 | Unit + Integration | RED |
| `kernel/atdd_28_3_pid_uuid_mapping_test.go` | 5 | Unit | RED |

### TDD Red Phase Verification

所有测试设计为在实现前 **编译失败**（引用不存在的字段和方法），这是 TDD RED 阶段的正确行为：

**编译期 RED**（字段/方法不存在）:
- `GetStepDetailRequest.UUID` — 字段不存在
- `ListStepsRequest.UUID` — 字段不存在
- `GetProcDetailRequest.UUID` — 字段不存在
- `KillRequest.UUID` — 字段不存在
- `AttachDebugRequest.UUID` — 字段不存在
- `KernelImpl.GetProcessByUUID()` — 方法不存在
- `Server.resolveProcess()` — 方法不存在
- `Client.GetStepDetailByUUID()` — 方法不存在
- `Client.ListStepsByUUID()` — 方法不存在

### 覆盖的行为变更

- PID 查询已 reap 进程：从 "fallback 扫描 UUID 目录" 变为 "返回 not_found"（有意行为变更）
- UUID 查询已 reap 进程：直接从 UUID 路径读取（新功能）
- 当 PID 和 UUID 同时提供时 UUID 优先（新规则）

## Summary

- **总测试数**: 29
- **P0 测试**: 18
- **P1 测试**: 11
- **TDD 阶段**: RED（全部编译失败）
- **实现后预期**: 全部 GREEN
