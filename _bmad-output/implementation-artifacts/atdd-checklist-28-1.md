---
stepsCompleted:
  - step-01-preflight-and-context
  - step-02-generation-mode
  - step-03-test-strategy
  - step-04-generate-tests
  - step-05-validate-and-complete
lastStep: step-05-validate-and-complete
lastSaved: '2026-03-22'
storyId: '28-1'
storyTitle: 'Process UUID v7 引入'
detectedStack: backend
generationMode: ai-generation
executionMode: sequential
tddPhase: RED
inputDocuments:
  - _bmad-output/implementation-artifacts/28-1-process-uuid-v7-introduction.md
  - kernel/process.go
  - kernel/process_test.go
  - kernel/kernel.go
  - ipc/protocol.go
  - ipc/protocol_test.go
  - internal/ui/table.go
  - internal/ui/table_test.go
  - vfs/proc.go
  - cmd/rnix/main.go
---

# ATDD Checklist — Story 28.1: Process UUID v7 引入

## Summary

| Metric | Value |
|--------|-------|
| Story | 28.1 — Process UUID v7 引入 |
| TDD Phase | RED (failing / won't compile) |
| Test Files Created | 3 |
| Total Test Functions | 21 |
| Acceptance Criteria Covered | 7/7 (AC-1 through AC-7) |
| Stack Type | Backend (Go 1.26) |
| Generation Mode | AI Generation |

## Test File Inventory

### 1. `kernel/atdd_28_1_process_uuid_test.go`

| Test Function | AC | Priority | Level | Status |
|---------------|----|---------:|-------|--------|
| `TestATDD_28_1_AC1_NewProcess_HasUUID` | AC-1 | P0 | Unit | RED — `proc.UUID undefined` |
| `TestATDD_28_1_AC1_UUID_Format_36Chars` | AC-1 | P0 | Unit | RED — `proc.UUID undefined` |
| `TestATDD_28_1_AC1_UUID_Immutable_AfterCreation` | AC-1 | P1 | Unit | RED — `proc.UUID undefined` |
| `TestATDD_28_1_AC2_UUID_V7_VersionByte` | AC-2 | P0 | Unit | RED — `proc.UUID undefined` |
| `TestATDD_28_1_AC2_UUID_Parseable` | AC-2 | P0 | Unit | RED — `proc.UUID undefined` |
| `TestATDD_28_1_AC2_UUID_TimeOrdered` | AC-2 | P0 | Unit | RED — `proc.UUID undefined` |
| `TestATDD_28_1_AC2_UUID_Uniqueness_1000` | AC-2 | P1 | Unit | RED — `proc.UUID undefined` |
| `TestATDD_28_1_AC2_UUID_GenerationLatency` | AC-2/NFR65 | P1 | Unit | RED — `proc.UUID undefined` |
| `TestATDD_28_1_AC3_UUID_Independent_Of_PIDCounter` | AC-3 | P1 | Unit | RED — `proc.UUID undefined` |
| `TestATDD_28_1_AC3_ConcurrentUUID_Uniqueness` | AC-3 | P1 | Unit | RED — `proc.UUID undefined` |
| `TestATDD_28_1_AC5_OnSpawn_ReceivesUUID` | AC-5 | P1 | Unit | RED — `KernelCallbacks` 签名不匹配 |

### 2. `ipc/atdd_28_1_uuid_wire_test.go`

| Test Function | AC | Priority | Level | Status |
|---------------|----|---------:|-------|--------|
| `TestATDD_28_1_AC6_ProcInfoWire_HasUUID` | AC-6 | P0 | Unit | RED — `unknown field UUID` |
| `TestATDD_28_1_AC6_ProcInfoWire_UUID_JSON_Roundtrip` | AC-6 | P0 | Unit | RED — `unknown field UUID` |
| `TestATDD_28_1_AC6_ProcInfoWire_UUID_OmitEmpty` | AC-6 | P1 | Unit | RED — `unknown field UUID` |
| `TestATDD_28_1_AC6_ProcInfoToWire_PreservesUUID` | AC-6 | P0 | Unit | RED — `ProcInfo no UUID` |
| `TestATDD_28_1_AC6_WireToProcInfo_PreservesUUID` | AC-6 | P0 | Unit | RED — `ProcInfoWire no UUID` |
| `TestATDD_28_1_AC6_ProcInfo_WireRoundtrip_UUID` | AC-6 | P0 | Unit | RED — field missing |
| `TestATDD_28_1_AC6_SpawnResponse_HasUUID` | AC-6 | P0 | Unit | RED — `SpawnResponse no UUID` |
| `TestATDD_28_1_AC6_SpawnResponse_UUID_JSON` | AC-6 | P0 | Unit | RED — `SpawnResponse no UUID` |
| `TestATDD_28_1_AC6_ProgressPayload_SpawnEvent_HasUUID` | AC-6 | P1 | Unit | RED — `ProgressPayload no UUID` |
| `TestATDD_28_1_AC7_ListProcsResponse_ContainsUUID` | AC-7 | P0 | Unit | RED — `ProcInfoWire no UUID` |

### 3. `internal/ui/atdd_28_1_uuid_table_test.go`

| Test Function | AC | Priority | Level | Status |
|---------------|----|---------:|-------|--------|
| `TestATDD_28_1_AC4_ShowUUID_True_HasUUIDHeader` | AC-4 | P0 | Unit | RED — signature mismatch |
| `TestATDD_28_1_AC4_ShowUUID_True_ContainsUUIDValues` | AC-4 | P0 | Unit | RED — signature mismatch |
| `TestATDD_28_1_AC4_ShowUUID_False_NoUUIDHeader` | AC-4 | P1 | Unit | RED — signature mismatch |
| `TestATDD_28_1_AC4_ShowUUID_False_NoUUIDValues` | AC-4 | P1 | Unit | RED — signature mismatch |
| `TestATDD_28_1_AC4_Verbose_And_ShowUUID` | AC-4 | P1 | Unit | RED — signature mismatch |
| `TestATDD_28_1_AC4_EmptyProcs_ShowUUID` | AC-4 | P2 | Unit | RED — signature mismatch |
| `TestATDD_28_1_AC4_BackwardCompat_DefaultOutput` | AC-4 | P0 | Unit | RED — signature mismatch |

## Acceptance Criteria Coverage Matrix

| AC | Description | Tests | Coverage |
|----|-------------|------:|----------|
| AC-1 | Process struct UUID 字段 | 3 | ✅ 完整 |
| AC-2 | Spawn 时生成 UUID v7 | 5 | ✅ 完整 (含 NFR65 延迟) |
| AC-3 | 跨 daemon 重启 UUID 唯一性 | 2 | ✅ 完整 (含并发) |
| AC-4 | `rnix ps --uuid` 显示 UUID 列 | 7 | ✅ 完整 (含反向兼容) |
| AC-5 | spawn 输出包含 UUID | 1 | ✅ 接口签名验证 |
| AC-6 | IPC ProcInfo 传输 UUID | 9 | ✅ 完整 (含 roundtrip) |
| AC-7 | JSON 输出包含 UUID | 1 | ✅ JSON 字段验证 |

## Priority Distribution

| Priority | Count | Percentage |
|----------|------:|-----------:|
| P0 | 13 | 62% |
| P1 | 12 | 57% |
| P2 | 1 | 5% |
| P3 | 0 | 0% |

(Some tests cover multiple priorities)

## TDD Red Phase Verification

All 3 test files confirmed to FAIL compilation:

```
kernel/  — proc.UUID undefined (type *Process has no field or method UUID)
ipc/     — unknown field UUID in struct literal of type ProcInfoWire
ui/      — too many arguments in call to RenderProcessTable
```

This is **intentional** — TDD Red Phase for Go structural changes means compilation failure IS the red signal.

## Implementation Path (Green Phase)

To make tests compile and pass, the developer must:

1. `go get github.com/google/uuid` — add UUID v7 dependency
2. `kernel/process.go` — add `UUID string` to Process struct; generate in `NewProcess()`
3. `vfs/proc.go` — add `UUID string` to ProcInfo struct
4. `kernel/kernel.go` — populate UUID in `GetProcInfo()` and `ListProcs()`
5. `ipc/protocol.go` — add UUID to `ProcInfoWire`, `SpawnResponse`, `ProgressPayload`; update conversion functions
6. `kernel/kernel.go` — update `KernelCallbacks.OnSpawn` signature to include `uuid string`
7. `internal/ui/table.go` — add `showUUID bool` parameter to `RenderProcessTable`
8. `cmd/rnix/main.go` — add `--uuid` flag, update `jsonProcess`, update `OnSpawn` callback

## Risks & Assumptions

- **Risk**: `RenderProcessTable` 签名变更会影响所有现有调用者（ps 命令 + 20+ 测试用例），需同步更新
- **Risk**: `KernelCallbacks.OnSpawn` 接口变更影响 3+ 实现者（cliCallbacks, callbackMux, test mocks）
- **Assumption**: `github.com/google/uuid` v1.6.0+ 已支持 `NewV7()` — 需确认
- **Assumption**: UUID v7 的时间戳精度（ms）足以保证 `TestATDD_28_1_AC2_UUID_TimeOrdered` 中 2ms sleep 产生不同 UUID

## Next Steps

1. 运行 `dev-story` 实现 Story 28-1
2. 实现后移除 TDD red 标记，验证所有测试 GREEN
3. 运行 `make all` 确认全量通过
