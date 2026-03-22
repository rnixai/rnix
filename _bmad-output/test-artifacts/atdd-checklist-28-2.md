---
stepsCompleted:
  - step-01-preflight-and-context
  - step-02-generation-mode
  - step-03-test-strategy
  - step-04-generate-tests
lastStep: step-04-generate-tests
lastSaved: '2026-03-22'
storyId: '28-2'
storyTitle: 'StepRecord 路径迁移到 UUID'
detectedStack: backend
generationMode: ai-generation
inputDocuments:
  - _bmad-output/implementation-artifacts/28-2-steprecord-path-migration-to-uuid.md
  - kernel/step_writer.go
  - kernel/reap.go
  - ipc/server.go
  - cmd/rnix/record.go
  - cmd/rnix/replay.go
  - kernel/process.go
---

# ATDD Checklist — Story 28.2: StepRecord 路径迁移到 UUID

## Preflight

- **Stack**: Go backend (`go.mod` detected)
- **Test framework**: Go `testing` with race detection (`go test -race`)
- **Story**: 28-2 StepRecord 路径迁移到 UUID (status: ready-for-dev)
- **Acceptance criteria**: 6 ACs (AC-1 through AC-6)
- **Story 28-1 prerequisite**: `Process.UUID` field exists, `uuid.NewV7()` in use

## Generation Mode

AI generation (backend stack — no browser recording needed)

## Test Strategy

### AC → Test Mapping

| AC | Description | Test Level | Priority | Test File |
|----|-------------|------------|----------|-----------|
| AC-1 | StepWriter 目录使用 UUID | Integration | P0 | `kernel/atdd_28_2_steprecord_uuid_test.go` |
| AC-2 | process-meta.json UUID 路径 + PID 字段 | Integration | P0 | `kernel/atdd_28_2_steprecord_uuid_test.go` |
| AC-3 | 旧 PID 目录向后兼容 | Unit | P0 | `kernel/atdd_28_2_steprecord_uuid_test.go` |
| AC-4 | `rnix record list` step 会话 | Unit | P1 | `cmd/rnix/atdd_28_2_record_replay_uuid_test.go` |
| AC-5 | `rnix replay <id>` UUID 前缀 | Unit | P1 | `cmd/rnix/atdd_28_2_record_replay_uuid_test.go` |
| AC-6 | IPC 路径解析使用 UUID | Integration | P0 | `ipc/atdd_28_2_uuid_path_test.go` |

### Red Phase Design

- **AC-1, AC-2**: Integration tests spawn processes and verify UUID-based paths — FAIL because current code creates PID-based paths
- **AC-3**: Backward compat tests — PASS with current ReadStep/ReadAllSteps (file-level compat maintained)
- **AC-4, AC-5**: Reference `isUUIDDir()`, `isLegacyPIDDir()`, `scanStepSessions()`, `matchStepUUIDPrefix()` which do NOT exist yet — compile failure = RED
- **AC-6**: Write steps to UUID directories, verify IPC resolves them — FAIL because server uses PID paths

## Test Files Generated

### 1. `kernel/atdd_28_2_steprecord_uuid_test.go`

| Test | AC | Red Phase Mechanism |
|------|----|---------------------|
| `TestATDD28_2_AC1_Integration_StepDir_UsesUUID` | AC-1 | Asserts UUID path exists → FAIL (PID path created) |
| `TestATDD28_2_AC1_StepDir_FileName_StillStepsJSONL` | AC-1 | Asserts steps.jsonl in UUID dir → FAIL |
| `TestATDD28_2_AC1_StepRecords_Readable_AtUUIDPath` | AC-1 | ReadStep from UUID path → FAIL |
| `TestATDD28_2_AC2_ProcessMeta_AtUUIDPath` | AC-2 | Meta at UUID path → FAIL |
| `TestATDD28_2_AC2_ProcessMeta_ContainsPIDField` | AC-2 | Meta has `pid` field → FAIL (field not written) |
| `TestATDD28_2_AC2_ProcessMeta_NotAtPIDPath` | AC-2 | Meta NOT at PID path → FAIL (currently at PID path) |
| `TestATDD28_2_AC3_Legacy_PID_ReadStep` | AC-3 | Legacy dir readable → PASS |
| `TestATDD28_2_AC3_Legacy_PID_ReadAllSteps` | AC-3 | Legacy ReadAllSteps → PASS |
| `TestATDD28_2_AC3_Legacy_And_UUID_Coexist` | AC-3 | Both formats coexist → PASS |

### 2. `ipc/atdd_28_2_uuid_path_test.go`

| Test | AC | Red Phase Mechanism |
|------|----|---------------------|
| `TestATDD_28_2_AC6_GetStepDetail_ResolvesUUIDPath` | AC-6 | Steps at UUID dir, server looks at PID → FAIL |
| `TestATDD_28_2_AC6_GetStepDetail_UUID_Step2` | AC-6 | Multi-step UUID resolution → FAIL |
| `TestATDD_28_2_AC6_ListSteps_ResolvesUUIDPath` | AC-6 | ListSteps via UUID → FAIL |
| `TestATDD_28_2_AC6_ListSteps_UUID_Incremental` | AC-6 | Incremental ListSteps via UUID → FAIL |
| `TestATDD_28_2_AC6_Fallback_ReapedProcess_UUID` | AC-6 | Reaped process UUID fallback → FAIL |
| `TestATDD_28_2_AC6_Fallback_ReapedProcess_ListSteps_UUID` | AC-6 | Reaped ListSteps UUID fallback → FAIL |
| `TestATDD_28_2_AC6_LegacyPIDPath_GetStepDetail_StillWorks` | AC-6 | Legacy PID compat → PASS |
| `TestATDD_28_2_AC6_LegacyPIDPath_ListSteps_StillWorks` | AC-6 | Legacy PID ListSteps → PASS |
| `TestATDD_28_2_AC6_ClientRoundtrip_GetStepDetail_UUID` | AC-6 | Client roundtrip via UUID → FAIL |
| `TestATDD_28_2_AC6_ClientRoundtrip_ListSteps_UUID` | AC-6 | Client ListSteps roundtrip → FAIL |

### 3. `cmd/rnix/atdd_28_2_record_replay_uuid_test.go`

| Test | AC | Red Phase Mechanism |
|------|----|---------------------|
| `TestATDD28_2_AC4_IsUUIDDir_*` (5 tests) | AC-4 | `isUUIDDir()` not defined → COMPILE FAIL |
| `TestATDD28_2_AC4_IsLegacyPIDDir_*` (3 tests) | AC-4 | `isLegacyPIDDir()` not defined → COMPILE FAIL |
| `TestATDD28_2_AC4_ScanStepSessions_*` (5 tests) | AC-4 | `scanStepSessions()` not defined → COMPILE FAIL |
| `TestATDD28_2_AC5_MatchStepUUIDPrefix_*` (5 tests) | AC-5 | `matchStepUUIDPrefix()` not defined → COMPILE FAIL |

## Summary

- **Total test functions**: 36
- **AC coverage**: 6/6 (all acceptance criteria covered)
- **Test levels**: Unit (AC-3, AC-4, AC-5) + Integration (AC-1, AC-2, AC-6)
- **TDD phase**: RED
- **Red mechanisms**: Runtime assertion failure (kernel, ipc) + Compile failure (cmd/rnix)
- **Expected passing tests**: AC-3 backward compat (3 tests), AC-6 legacy PID compat (2 tests)
- **Helper added**: `writeTestStepsUUID()` in ipc test — creates `data/steps/<uuid>/steps.jsonl`

## Implementation Checklist

When implementing Story 28-2, these tests should transition from RED to GREEN:

- [ ] `NewStepWriter` signature: `pid types.PID` → `procUUID string`
- [ ] `kernel/kernel.go`: pass `proc.UUID` to `NewStepWriter`
- [ ] `kernel/reap.go`: add `PID types.PID` to meta struct
- [ ] `ipc/server.go`: `resolveStepsPathFromProc` use `proc.UUID`
- [ ] `ipc/server.go`: `resolveStepsPathFallback` try UUID then PID
- [ ] `cmd/rnix/record.go`: implement `isUUIDDir`, `isLegacyPIDDir`, `scanStepSessions`
- [ ] `cmd/rnix/replay.go`: implement `matchStepUUIDPrefix`
- [ ] Update existing 27.x test helpers to use UUID paths
- [ ] `make all` passes
