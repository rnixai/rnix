---
stepsCompleted:
  - step-01-preflight-and-context
  - step-02-generation-mode
  - step-03-test-strategy
  - step-04-generate-tests
lastStep: step-04-generate-tests
lastSaved: '2026-03-18'
storyFile: _bmad-output/implementation-artifacts/26-3-specialize-capability-migration.md
detectedStack: backend
generationMode: ai-generation
testFile: kernel/atdd_26_3_specialize_migration_test.go
inputDocuments:
  - _bmad-output/implementation-artifacts/26-3-specialize-capability-migration.md
  - kernel/kernel.go
  - kernel/lineage.go
  - kernel/diffmemory.go
  - kernel/kernel_test.go (test patterns)
  - kernel/stem_integration_test.go (stem test patterns)
---

# ATDD Checklist — Story 26.3: Specialize 能力迁移

## Summary

| Item | Value |
|------|-------|
| Story | 26.3 Specialize 能力迁移 |
| Stack | Backend (Go) |
| Generation Mode | AI Generation |
| Test File | `kernel/atdd_26_3_specialize_migration_test.go` |
| Total Tests | 17 |
| RED Phase | 16 FAIL / 1 PASS |
| TDD Compliant | Yes — all failures caused by stub implementation |

## Test Strategy

### Test Levels

- **Unit tests**: All tests are unit-level, exercising the `reasonStep` ActionSpecialize code path
- **Integration**: Tests use real VFS + mock LLM + real Context Manager (same pattern as existing `TestReasonStep_*`)
- **No E2E/browser**: Pure backend, no browser-based testing needed

### Priorities

| Priority | Count | Coverage |
|----------|-------|----------|
| P0 | 14 | AC-1 through AC-8 core paths |
| P1 | 3 | Edge cases, tolerance, trigger content |

## AC → Test Mapping

### AC-1: Specialize 完整实现——Skill 加载

| Test ID | Test Name | Priority | Status |
|---------|-----------|----------|--------|
| 26.3-UNIT-001 | `TestReasonStep_Specialize_SkillLoaded` | P0 | RED |
| 26.3-UNIT-002 | `TestReasonStep_Specialize_AllowedDevicesUpdated` | P0 | RED |
| 26.3-UNIT-003 | `TestReasonStep_Specialize_SkillBodyInjected` | P0 | RED |
| 26.3-UNIT-004 | `TestReasonStep_Specialize_SuccessToolMessage` | P0 | RED |
| 26.3-UNIT-005 | `TestReasonStep_Specialize_StemSpecializeEvent` | P0 | RED |
| 26.3-UNIT-011 | `TestReasonStep_Specialize_NoSkillLoader` | P0 | RED |
| 26.3-UNIT-016 | `TestReasonStep_Specialize_ReasonStepEvent` | P0 | RED |

### AC-2: TOCTOU 双重检查防护

| Test ID | Test Name | Priority | Status |
|---------|-----------|----------|--------|
| 26.3-UNIT-006 | `TestReasonStep_Specialize_AlreadyLoaded` | P0 | RED |
| 26.3-UNIT-013 | `TestReasonStep_Specialize_ConcurrentRaceFree` | P0 | PASS* |

\* ConcurrentRaceFree passes because the -race detector finds no violations on the stub path (no actual writes occur).

### AC-3: Progressive Lineage 记录

| Test ID | Test Name | Priority | Status |
|---------|-----------|----------|--------|
| 26.3-UNIT-007 | `TestReasonStep_Specialize_LineageRecorded` | P0 | RED |
| 26.3-UNIT-014 | `TestReasonStep_Specialize_LineageTriggerFromContent` | P1 | RED |

### AC-4: DiffMemory 更新

| Test ID | Test Name | Priority | Status |
|---------|-----------|----------|--------|
| 26.3-UNIT-008 | `TestReasonStep_Specialize_DiffMemoryRecorded` | P0 | RED |
| 26.3-UNIT-017 | `TestReasonStep_Specialize_DiffMemoryFullSnapshot` | P1 | RED |

### AC-5: 不存在的 Skill 错误处理

| Test ID | Test Name | Priority | Status |
|---------|-----------|----------|--------|
| 26.3-UNIT-009 | `TestReasonStep_Specialize_SkillNotFound` | P0 | RED |
| 26.3-UNIT-015 | `TestReasonStep_Specialize_ErrorDoesNotCrash` | P0 | RED |

### AC-6: Empty Skill Name 错误处理

| Test ID | Test Name | Priority | Status |
|---------|-----------|----------|--------|
| 26.3-UNIT-010 | `TestReasonStep_Specialize_EmptySkillName` | P0 | RED |

### AC-7: AppendMessage 失败容错

| Test ID | Test Name | Priority | Status |
|---------|-----------|----------|--------|
| 26.3-UNIT-012 | `TestReasonStep_Specialize_AppendMessageFailure` | P1 | RED |

### AC-8: 并发 Specialize 线程安全

| Test ID | Test Name | Priority | Status |
|---------|-----------|----------|--------|
| 26.3-UNIT-013 | `TestReasonStep_Specialize_ConcurrentRaceFree` | P0 | PASS* |

### AC-9: 编译和测试通过

Meta-AC: Verified by `make all` after implementation. Not a standalone test.

## RED Phase Evidence

```
FAIL: TestReasonStep_Specialize_SkillLoaded — skillLoader not called
FAIL: TestReasonStep_Specialize_AllowedDevicesUpdated — AllowedDevices empty
FAIL: TestReasonStep_Specialize_SkillBodyInjected — no RoleUser injection
FAIL: TestReasonStep_Specialize_SuccessToolMessage — no success tool message
FAIL: TestReasonStep_Specialize_StemSpecializeEvent — no StemSpecialize event
FAIL: TestReasonStep_Specialize_AlreadyLoaded — skillLoader never called
FAIL: TestReasonStep_Specialize_LineageRecorded — no progressive lineage
FAIL: TestReasonStep_Specialize_DiffMemoryRecorded — no DiffMemory record
FAIL: TestReasonStep_Specialize_SkillNotFound — wrong error message
FAIL: TestReasonStep_Specialize_EmptySkillName — wrong error message
FAIL: TestReasonStep_Specialize_NoSkillLoader — wrong error message
FAIL: TestReasonStep_Specialize_AppendMessageFailure — skill not in proc.Skills
PASS: TestReasonStep_Specialize_ConcurrentRaceFree — no race (no writes)
FAIL: TestReasonStep_Specialize_LineageTriggerFromContent — no lineage
FAIL: TestReasonStep_Specialize_ErrorDoesNotCrash — good-skill not loaded
FAIL: TestReasonStep_Specialize_ReasonStepEvent — wrong event action
FAIL: TestReasonStep_Specialize_DiffMemoryFullSnapshot — no DiffMemory
```

All 16 failures are caused by the current stub: `"specialize action not yet implemented"`. The stub bypasses all real logic (skill loading, lineage recording, DiffMemory updates, event emission).

## GREEN Phase Instructions

Replace the `case ActionSpecialize:` stub in `kernel/kernel.go` (lines ~1596-1604) with the full implementation from Story Task 1. After implementation, all 17 tests should pass with `go test -race ./kernel/...`.
