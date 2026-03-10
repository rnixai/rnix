---
stepsCompleted: ['step-01-preflight-and-context', 'step-02-generation-mode', 'step-03-test-strategy', 'step-04-generate-tests']
lastStep: 'step-04-generate-tests'
lastSaved: '2026-03-11'
workflowType: 'testarch-atdd'
inputDocuments:
  - '_bmad-output/implementation-artifacts/20-5-differentiation-lineage-graph.md'
  - '_bmad/tea/config.yaml'
---

# ATDD Checklist - Epic 20, Story 5: Differentiation Lineage Graph

**Date:** 2026-03-11
**Author:** Decker
**Primary Test Level:** Unit + Integration (Go backend)

---

## Story Summary

Story 20.5 adds differentiation lineage tracking for Stem Agents. When a stem agent undergoes initial differentiation (at spawn) or progressive specialization (via OODA specialize action), each step is recorded as a `LineageEvent`. Users can query the complete differentiation path via `rnix lineage <pid>`.

**As a** platform builder
**I want** to view the complete differentiation path from stem agent to current specialized form via `rnix lineage <pid>`
**So that** I can understand how an agent acquired its current capabilities

---

## Acceptance Criteria

1. Given a differentiated agent, when executing `rnix lineage <pid>`, then display the complete path from Stem Agent to current specialized form, including each differentiation's loaded Skills and triggering intent
2. Given a lineage graph containing multiple progressive specializations, when displaying the lineage, then each Skill loading is annotated with timestamp and trigger reason

---

## Failing Tests Created (RED Phase)

### Unit Tests (5 tests)

**File:** `kernel/lineage_test.go` (146 lines)

- RED **Test:** TestLineage_RecordAndEvents
  - **Status:** RED - `NewLineage` and `LineageEvent` types do not exist
  - **Verifies:** AC#1 - Recording lineage events and retrieving them in order

- RED **Test:** TestLineage_EmptyEvents
  - **Status:** RED - `NewLineage` does not exist
  - **Verifies:** AC#1 - Empty lineage returns non-nil empty slice

- RED **Test:** TestLineage_ConcurrentAccess
  - **Status:** RED - `NewLineage` does not exist
  - **Verifies:** Concurrency safety of Lineage Record/Events under -race

- RED **Test:** TestLineage_FromMemoryFlag
  - **Status:** RED - `NewLineage`/`LineageEvent` do not exist
  - **Verifies:** AC#1 - FromMemory flag is preserved across Record/Events

- RED **Test:** TestLineage_EventsReturnsCopy
  - **Status:** RED - `NewLineage` does not exist
  - **Verifies:** Events() returns a defensive copy, not internal state

### Integration Tests (10 tests)

**File:** `kernel/lineage_integration_test.go` (316 lines)

- RED **Test:** TestSpawn_StemAgent_RecordsLineage
  - **Status:** RED - `proc.GetLineage()` undefined on Process
  - **Verifies:** AC#1 - Stem agent spawn records initial lineage event

- RED **Test:** TestSpawn_StemAgent_LineageFromMemory
  - **Status:** RED - `proc.GetLineage()` undefined
  - **Verifies:** AC#1 - Differentiation from memory sets FromMemory=true

- RED **Test:** TestSpawn_NonStemAgent_NoLineage
  - **Status:** RED - `proc.GetLineage()` undefined
  - **Verifies:** Non-stem agents have nil lineage

- RED **Test:** TestOODA_Specialize_RecordsLineage
  - **Status:** RED - `NewLineage`, `LineageEvent`, `proc.SetLineage` undefined
  - **Verifies:** AC#2 - Progressive specialization appends lineage event

- RED **Test:** TestOODA_Specialize_LineageTriggerFromReason
  - **Status:** RED - `NewLineage`, `proc.SetLineage` undefined
  - **Verifies:** AC#2 - Progressive event Trigger comes from OODADecision.Reason

- RED **Test:** TestKernel_GetLineage_Success
  - **Status:** RED - `NewLineage`, `proc.SetLineage`, `k.GetLineage` undefined
  - **Verifies:** AC#1 - Kernel GetLineage returns events for valid PID

- RED **Test:** TestKernel_GetLineage_ProcessNotFound
  - **Status:** RED - `k.GetLineage` undefined
  - **Verifies:** Error returned for non-existent PID

- RED **Test:** TestKernel_GetLineage_NoLineage
  - **Status:** RED - `k.GetLineage` undefined
  - **Verifies:** Nil events returned for non-differentiated process

- RED **Test:** TestE2E_Lineage_StemDifferentiation
  - **Status:** RED - `k.GetLineage` undefined
  - **Verifies:** AC#1 - Full spawn-to-query flow

- RED **Test:** TestE2E_Lineage_MemoryReuse
  - **Status:** RED - `k.GetLineage` undefined
  - **Verifies:** AC#1 - Memory reuse tracked in lineage

- RED **Test:** TestE2E_Lineage_MultiplePids
  - **Status:** RED - `k.GetLineage` undefined
  - **Verifies:** AC#1 - Independent lineage per PID

### IPC Tests (3 tests)

**File:** `ipc/lineage_test.go` (109 lines)

- RED **Test:** TestServer_Lineage_Success
  - **Status:** RED - `kernel.NewLineage`, `MethodLineage`, `LineageRequest`, `LineageResponse` undefined
  - **Verifies:** AC#1,#2 - IPC roundtrip returns lineage events

- RED **Test:** TestServer_Lineage_NotFound
  - **Status:** RED - `MethodLineage`, `LineageRequest` undefined
  - **Verifies:** IPC returns NOT_FOUND for missing PID

- RED **Test:** TestServer_Lineage_NoDifferentiation
  - **Status:** RED - `MethodLineage`, `LineageRequest`, `LineageResponse` undefined
  - **Verifies:** IPC returns empty events for non-differentiated process

### CLI Tests (6 tests)

**File:** `cmd/rnix/lineage_test.go` (156 lines)

- RED **Test:** TestLineageCmd_Registered
  - **Status:** RED - `lineageCmd` variable undefined (cmd/rnix/lineage.go not created)
  - **Verifies:** AC#1 - lineage command exists in Cobra tree

- RED **Test:** TestLineageCmd_InvalidPID
  - **Status:** RED - `lineageCmd` undefined
  - **Verifies:** AC#1 - Non-numeric PID produces "invalid PID" error

- RED **Test:** TestLineageCmd_InvalidPID_JSON
  - **Status:** RED - `lineageCmd` undefined
  - **Verifies:** AC#1 - JSON mode outputs structured error for invalid PID

- RED **Test:** TestLineageCmd_NoDaemon
  - **Status:** RED - `lineageCmd` undefined
  - **Verifies:** AC#1 - "daemon not available" error when daemon not running

- RED **Test:** TestLineageCmd_NoDaemon_JSON
  - **Status:** RED - `lineageCmd` undefined
  - **Verifies:** AC#1 - JSON mode outputs structured error when no daemon

- RED **Test:** TestLineageCmd_NoArgs
  - **Status:** RED - `lineageCmd` undefined
  - **Verifies:** cobra.ExactArgs(1) enforced

---

## Data Factories Created

N/A -- Go backend project uses inline test data construction (following existing kernel test patterns with `mockLLMFile`, `stemAgentInfo`, `NewProcess`, etc.)

---

## Fixtures Created

N/A -- Go backend project uses test helpers from `kernel_test.go` (e.g., `newTestKernel`, `makeLLMResponse`, `stemAgentInfo`) and `ipc/server_test.go` (e.g., `setupTestServer`, `dial`, `sendRequest`).

---

## Mock Requirements

### Mock LLM Device

Tests reuse the existing `mockLLMFile` from `kernel/kernel_test.go`.

### Mock Stem Matcher

Tests reuse `NewStemMatcherFromFunc` from `kernel/stem.go`.

### Mock Skill Loader

Tests use inline `func(name string) (*skills.SkillInfo, error)` closures (existing pattern).

---

## Implementation Checklist

### Test: TestLineage_RecordAndEvents (+ EmptyEvents, ConcurrentAccess, FromMemoryFlag, EventsReturnsCopy)

**File:** `kernel/lineage_test.go`

**Tasks to make these tests pass:**

- [ ] Create `kernel/lineage.go` with `LineageEvent` struct (Timestamp, Phase, Skills, Trigger, FromMemory fields)
- [ ] Create `Lineage` struct with `sync.Mutex`, `events []LineageEvent`
- [ ] Implement `NewLineage() *Lineage` constructor
- [ ] Implement `Record(event LineageEvent)` method (append under lock)
- [ ] Implement `Events() []LineageEvent` method (return copy under lock)
- [ ] Run tests: `go test -race -run TestLineage ./kernel/...`
- [ ] All 5 unit tests pass (green phase)

### Test: TestSpawn_StemAgent_RecordsLineage (+ LineageFromMemory, NonStemAgent_NoLineage)

**File:** `kernel/lineage_integration_test.go`

**Tasks to make these tests pass:**

- [ ] Add `lineage *Lineage` field to `Process` struct in `kernel/process.go`
- [ ] Implement `GetLineage() *Lineage` method on `Process`
- [ ] Implement `SetLineage(l *Lineage)` method on `Process`
- [ ] In `kernel/kernel.go` Spawn method, after stem differentiation success (after `proc.Skills = loadedNames`), create Lineage and Record initial event
- [ ] Set FromMemory based on `fromMemory` variable from DiffMemory.Lookup
- [ ] Run tests: `go test -race -run TestSpawn_StemAgent ./kernel/...`
- [ ] All 3 spawn integration tests pass (green phase)

### Test: TestOODA_Specialize_RecordsLineage (+ LineageTriggerFromReason)

**File:** `kernel/lineage_integration_test.go`

**Tasks to make these tests pass:**

- [ ] In `kernel/ooda.go` `oodaActSpecialize`, after DiffMemory Record, add lineage Record for progressive event
- [ ] Set Trigger from `decision.Reason`
- [ ] Run tests: `go test -race -run TestOODA_Specialize ./kernel/...`
- [ ] Both OODA specialize tests pass (green phase)

### Test: TestKernel_GetLineage_Success (+ ProcessNotFound, NoLineage)

**File:** `kernel/lineage_integration_test.go`

**Tasks to make these tests pass:**

- [ ] Implement `GetLineage(pid types.PID) ([]LineageEvent, error)` on `KernelImpl`
- [ ] Return `NewSyscallError` with `ErrNotFound` for missing PID
- [ ] Return `nil, nil` for process without lineage
- [ ] Run tests: `go test -race -run TestKernel_GetLineage ./kernel/...`
- [ ] All 3 GetLineage tests pass (green phase)

### Test: TestE2E_Lineage_StemDifferentiation (+ MemoryReuse, MultiplePids)

**File:** `kernel/lineage_integration_test.go`

**Tasks to make these tests pass:**

- [ ] All prerequisite tasks above completed
- [ ] Run tests: `go test -race -run TestE2E_Lineage ./kernel/...`
- [ ] All 3 E2E tests pass (green phase)

### Test: TestServer_Lineage_Success (+ NotFound, NoDifferentiation)

**File:** `ipc/lineage_test.go`

**Tasks to make these tests pass:**

- [ ] Add `MethodLineage Method = "lineage"` to `ipc/protocol.go`
- [ ] Add `LineageRequest`, `LineageEvent`, `LineageResponse` types to `ipc/protocol.go`
- [ ] Add `handleLineage` handler to `ipc/server.go` dispatch map
- [ ] Implement `handleLineage` (unmarshal payload, call `kern.GetLineage`, convert kernel types to IPC types)
- [ ] Add `Lineage(pid types.PID) (*LineageResponse, error)` to `ipc/client.go`
- [ ] Run tests: `go test -race -run TestServer_Lineage ./ipc/...`
- [ ] All 3 IPC tests pass (green phase)

### Test: TestLineageCmd_Registered (+ InvalidPID, NoDaemon, NoArgs)

**File:** `cmd/rnix/lineage_test.go`

**Tasks to make these tests pass:**

- [ ] Create `cmd/rnix/lineage.go` with `lineageCmd` cobra.Command
- [ ] Set `Use: "lineage <pid>"`, `Args: cobra.ExactArgs(1)`
- [ ] Implement `runLineage` function: parse PID, EnsureDaemon, IPC call, render output
- [ ] Implement text rendering with lipgloss (phase colors, skill highlights)
- [ ] Implement JSON output mode (`--json` flag)
- [ ] Handle "no lineage" case with informational message
- [ ] Run tests: `go test -race -run TestLineageCmd ./cmd/rnix/...`
- [ ] All 6 CLI tests pass (green phase)

---

## Running Tests

```bash
# Run all failing tests for this story
go test -race -run "TestLineage|TestSpawn_StemAgent_RecordsLineage|TestSpawn_StemAgent_LineageFromMemory|TestSpawn_NonStemAgent_NoLineage|TestOODA_Specialize_RecordsLineage|TestOODA_Specialize_LineageTriggerFromReason|TestKernel_GetLineage|TestE2E_Lineage|TestServer_Lineage|TestLineageCmd" ./kernel/... ./ipc/... ./cmd/rnix/...

# Run kernel lineage unit tests only
go test -race -run TestLineage ./kernel/...

# Run kernel lineage integration tests only
go test -race -run "TestSpawn_StemAgent_Rec|TestSpawn_StemAgent_Lin|TestSpawn_NonStem|TestOODA_Specialize_Rec|TestOODA_Specialize_Lin|TestKernel_GetLineage|TestE2E_Lineage" ./kernel/...

# Run IPC lineage tests only
go test -race -run TestServer_Lineage ./ipc/...

# Run CLI lineage tests only
go test -race -run TestLineageCmd ./cmd/rnix/...

# Run with verbose output
go test -race -v -run TestLineage ./kernel/...
```

---

## Red-Green-Refactor Workflow

### RED Phase (Complete)

**TEA Agent Responsibilities:**

- All 24 tests written and failing (compile errors due to missing implementation)
- Tests follow existing codebase patterns (mockLLMFile, stemAgentInfo, setupTestServer, etc.)
- Test file structure matches Go project conventions (_test.go in same package)
- Implementation checklist maps each test group to concrete coding tasks

**Verification:**

- `go vet ./kernel/...` fails: `proc.GetLineage undefined`, `NewLineage undefined`
- `go vet ./ipc/...` fails: `kernel.NewLineage undefined`, `MethodLineage undefined`
- `go vet ./cmd/rnix/...` fails: `lineageCmd undefined`
- All failures are due to missing implementation, not test bugs

---

### GREEN Phase (DEV Team - Next Steps)

**DEV Agent Responsibilities:**

1. **Start with Task 1** (kernel/lineage.go) - pure data structure, no dependencies
2. **Run unit tests** to verify Lineage struct works
3. **Task 2** - Add lineage field to Process, implement GetLineage/SetLineage
4. **Task 3** - Integrate lineage recording into Spawn (kernel.go) and specialize (ooda.go)
5. **Task 4** - Add IPC protocol types, server handler, client method
6. **Task 5** - Create CLI command
7. **Task 6** - Run E2E integration tests

---

### REFACTOR Phase (DEV Team - After All Tests Pass)

1. Consider using `sync.RWMutex` instead of `sync.Mutex` for Lineage (Events is read-only)
2. Review error messages for consistency with existing kernel syscall errors
3. Ensure lipgloss styles are consistent with other CLI commands (trace, ps)
4. Verify RNIX_ASCII=1 fallback works for lineage output

---

## Next Steps

1. **Review this checklist** with team
2. **Run compile check** to confirm RED phase: `go vet ./kernel/... ./ipc/... ./cmd/rnix/...`
3. **Begin implementation** starting with Task 1 (kernel/lineage.go)
4. **Work one task at a time** (red -> green for each)
5. **When all tests pass**, refactor code for quality
6. **When refactoring complete**, update story status to 'done' in sprint-status.yaml

---

## Test Execution Evidence

### Initial Test Run (RED Phase Verification)

**Command:** `go vet ./kernel/... ./ipc/... ./cmd/rnix/...`

**Results:**

```
kernel/lineage_integration_test.go: proc.GetLineage undefined (type *Process has no field or method GetLineage)
ipc/lineage_test.go: undefined: kernel.NewLineage
cmd/rnix/lineage_test.go: undefined: lineageCmd
```

**Summary:**

- Total tests: 24
- Passing: 0 (expected - compile errors)
- Failing: 24 (expected - implementation not yet created)
- Status: RED phase verified

---

## Notes

- Lineage uses independent sync.Mutex (not proc.mu) to avoid nested lock issues discovered in Story 20.4
- LineageEvent is a separate type from SyscallEvent (different lifecycle and consumers)
- IPC follows the standard 4-step pattern: protocol.go -> server.go -> client.go -> cmd/rnix/*.go
- Tests reuse all existing test infrastructure (no new test helpers needed)

---

**Generated by BMad TEA Agent** - 2026-03-11
