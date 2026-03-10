---
stepsCompleted:
  - 'step-01-preflight-and-context'
  - 'step-02-generation-mode'
  - 'step-03-test-strategy'
  - 'step-04-generate-tests'
lastStep: 'step-04-generate-tests'
lastSaved: '2026-03-10'
workflowType: 'testarch-atdd'
inputDocuments:
  - '_bmad-output/implementation-artifacts/20-2-ooda-configuration-and-mission-command.md'
  - '_bmad/tea/testarch/knowledge/data-factories.md'
  - '_bmad/tea/testarch/knowledge/test-quality.md'
  - '_bmad/tea/testarch/knowledge/test-healing-patterns.md'
  - '_bmad/tea/testarch/knowledge/test-levels-framework.md'
  - '_bmad/tea/testarch/knowledge/test-priorities-matrix.md'
---

# ATDD Checklist - Epic 20, Story 2: OODA Configuration & Mission Command

**Date:** 2026-03-10
**Author:** Decker
**Primary Test Level:** Unit + Integration (Go backend)

---

## Story Summary

Story 20.2 adds declarative OODA configuration via agent.yaml and mission-command autonomous spawning. Agents can declare `reasoning: ooda` in their manifest, OODA agents can autonomously spawn child agents by name in the Decide phase, and child agents with `reasoning: ooda` automatically run their own OODA loops.

**As a** platform builder
**I want** to enable OODA mode via agent.yaml and let OODA agents autonomously spawn child agents
**So that** I can build autonomous decision-making agent hierarchies

---

## Acceptance Criteria

1. **AC#1**: Given agent.yaml declares `reasoning: ooda`, When spawning that Agent, Then the agent uses OODA loop instead of linear reasoning
2. **AC#2**: Given an OODA agent in Decide phase, When the agent decides to spawn a child task, Then the agent autonomously spawns a child agent with intent only (mission command -- no execution details)
3. **AC#3**: Given an OODA agent spawns a child whose agent.yaml also declares `reasoning: ooda`, Then the child runs its own OODA loop internally

---

## Test Strategy

### Detected Stack: `backend` (Go)

### Test Level Selection

| AC | Test Level | Justification |
|----|-----------|---------------|
| AC#1 (Reasoning field parsing) | Unit | Pure YAML parsing + validation logic |
| AC#1 (Reasoning validation) | Unit | Input validation of reasoning field values |
| AC#1 (Spawn propagation) | Integration | Kernel + agents package interaction |
| AC#1 (Priority: agent > opts) | Integration | Two sources converge in Spawn method |
| AC#2 (Spawn with agent name) | Integration | OODA act phase + agent loading + kernel spawn |
| AC#2 (Spawn without agent) | Integration | Backward compatibility with existing behavior |
| AC#2 (Agent not found) | Integration | Error handling in OODA act phase |
| AC#3 (Child OODA inheritance) | Integration | Full chain: parent spawn -> child OODA detection |
| AC#3 (Child linear mode) | Integration | Negative case: child with no reasoning stays linear |

### Priority Assignment: P1

- **User impact**: Affects all OODA-based agents (core feature)
- **Complexity**: High (cross-package changes: agents, kernel, cmd)
- **Usage**: Frequent (every OODA agent spawn)
- **Risk**: Medium (misrouting reasoning mode breaks agent behavior)

---

## Failing Tests Created (RED Phase)

### Unit Tests (4 tests)

**File:** `agents/loader_reasoning_test.go` (86 lines)

- **Test:** `TestAgentManifest_ReasoningField`
  - **Status:** RED - `AgentManifest` has no `Reasoning` field
  - **Verifies:** AC#1 -- agent.yaml `reasoning: ooda` parsed into Manifest.Reasoning

- **Test:** `TestAgentLoader_DefaultReasoningMode`
  - **Status:** RED - `AgentManifest` has no `Reasoning` field
  - **Verifies:** AC#1 -- agent.yaml without `reasoning` defaults to empty string

- **Test:** `TestAgentLoader_InvalidReasoningMode`
  - **Status:** RED - No validation logic for `Reasoning` field exists
  - **Verifies:** AC#1 -- invalid reasoning values (e.g., "bogus") produce clear errors

- **Test:** `TestAgentLoader_LinearReasoningMode`
  - **Status:** RED - `AgentManifest` has no `Reasoning` field
  - **Verifies:** AC#1 -- explicit "linear" value accepted as valid

### Integration Tests (8 tests)

**File:** `kernel/ooda_reasoning_test.go` (488 lines)

- **Test:** `TestSpawn_AgentReasoningOODA`
  - **Status:** RED - `AgentManifest` has no `Reasoning` field; Spawn doesn't read it
  - **Verifies:** AC#1 -- agent.Manifest.Reasoning = "ooda" enables OODA mode in Spawn

- **Test:** `TestSpawn_AgentReasoningDefault`
  - **Status:** RED - `AgentManifest` has no `Reasoning` field
  - **Verifies:** AC#1 -- agent without reasoning uses linear mode

- **Test:** `TestSpawn_ReasoningModePriority/agent_yaml_overrides_empty_opts`
  - **Status:** RED - `AgentManifest` has no `Reasoning` field
  - **Verifies:** AC#1 -- agent.yaml reasoning takes priority over empty SpawnOpts

- **Test:** `TestSpawn_ReasoningModePriority/opts_fallback_when_agent_empty`
  - **Status:** RED - `AgentManifest` has no `Reasoning` field
  - **Verifies:** AC#1 -- SpawnOpts.ReasoningMode acts as fallback when agent has no reasoning

- **Test:** `TestOODAActSpawn_WithAgent`
  - **Status:** RED - `KernelImpl.SetAgentLoader` does not exist
  - **Verifies:** AC#2 -- OODA decide spawn with `{"agent": "name"}` loads and spawns agent

- **Test:** `TestOODAActSpawn_WithoutAgent`
  - **Status:** RED - existing behavior, should pass once other features are implemented
  - **Verifies:** AC#2 -- spawn without agent field uses bare process (backward compat)

- **Test:** `TestOODAActSpawn_AgentNotFound`
  - **Status:** RED - `KernelImpl.SetAgentLoader` does not exist
  - **Verifies:** AC#2 -- nonexistent agent returns error, parent continues gracefully

- **Test:** `TestOODA_ChildInheritsOODAMode`
  - **Status:** RED - `KernelImpl.SetAgentLoader` does not exist
  - **Verifies:** AC#3 -- child with agent.Reasoning="ooda" has proc.IsOODA()==true

- **Test:** `TestOODA_ChildLinearMode`
  - **Status:** RED - `KernelImpl.SetAgentLoader` does not exist
  - **Verifies:** AC#3 -- child with agent.Reasoning="" has proc.IsOODA()==false

---

## Data Factories Created

### Go Test Helper

**File:** `kernel/ooda_reasoning_test.go` (helper function)

**Exports:**

- `testAgentInfoWithReasoning(reasoning string) *agents.AgentInfo` - Create AgentInfo with specified reasoning mode and standard mock skill configuration

### Test Fixtures (testdata)

**File:** `agents/testdata/ooda-agent/agent.yaml`
- OODA agent manifest with `reasoning: ooda`

**File:** `agents/testdata/ooda-agent/instructions.md`
- OODA agent instructions

**File:** `agents/testdata/invalid-reasoning/agent.yaml`
- Agent manifest with `reasoning: bogus` for validation testing

**File:** `agents/testdata/invalid-reasoning/instructions.md`
- Invalid reasoning agent instructions

---

## Fixtures Created

### Kernel Test Helpers

Reuses existing test infrastructure from `kernel/ooda_test.go`:

- `newOODATestKernel(t, responseFunc)` - Creates kernel with dynamic mock LLM
- `newTestKernel(t, llmFile)` - Creates kernel with static mock LLM
- `makeLLMResponse(content, tokens)` - Builds JSON-encoded LLM response

---

## Mock Requirements

### Mock Agent Loader

**Type:** Function injection via `KernelImpl.SetAgentLoader`

```go
k.SetAgentLoader(func(name string) (*agents.AgentInfo, error) {
    if name == "ooda-demo" {
        return testAgentInfoWithReasoning("ooda"), nil
    }
    return nil, types.NewErrNotFound("agent", name)
})
```

**Notes:** Agent loader is injected as a function type to maintain kernel's dependency direction (kernel does not import agents package directly for loading).

### Mock LLM (existing)

Reuses `mockDynamicLLMFile` from `kernel/ooda_test.go` with call-count-based response sequences.

---

## Required data-testid Attributes

Not applicable -- backend Go project, no UI components.

---

## Implementation Checklist

### Test: TestAgentManifest_ReasoningField / TestAgentLoader_DefaultReasoningMode / TestAgentLoader_InvalidReasoningMode / TestAgentLoader_LinearReasoningMode

**File:** `agents/loader_reasoning_test.go`

**Tasks to make these tests pass:**

- [ ] Add `Reasoning string \`yaml:"reasoning,omitempty"\`` field to `AgentManifest` in `agents/types.go`
- [ ] Add validation in `agents/loader.go` `Load()`: accept "", "linear", "ooda"; reject all other values
- [ ] Run tests: `go test -race -run TestAgentManifest_ReasoningField ./agents/...`
- [ ] Run tests: `go test -race -run TestAgentLoader_DefaultReasoningMode ./agents/...`
- [ ] Run tests: `go test -race -run TestAgentLoader_InvalidReasoningMode ./agents/...`
- [ ] Run tests: `go test -race -run TestAgentLoader_LinearReasoningMode ./agents/...`

---

### Test: TestSpawn_AgentReasoningOODA / TestSpawn_AgentReasoningDefault / TestSpawn_ReasoningModePriority

**File:** `kernel/ooda_reasoning_test.go`

**Tasks to make these tests pass:**

- [ ] In `kernel/kernel.go` Spawn method (agent info block ~L184-205), add reasoning propagation:
  ```go
  if agent.Manifest.Reasoning == "ooda" {
      opts.ReasoningMode = "ooda"
  }
  ```
- [ ] Run tests: `go test -race -run TestSpawn_AgentReasoning ./kernel/...`
- [ ] Run tests: `go test -race -run TestSpawn_ReasoningModePriority ./kernel/...`

---

### Test: TestOODAActSpawn_WithAgent / TestOODAActSpawn_WithoutAgent / TestOODAActSpawn_AgentNotFound

**File:** `kernel/ooda_reasoning_test.go`

**Tasks to make these tests pass:**

- [ ] Add `agentLoader func(name string) (*agents.AgentInfo, error)` field to `KernelImpl` in `kernel/kernel.go`
- [ ] Add `SetAgentLoader(loader func(name string) (*agents.AgentInfo, error))` method on `KernelImpl`
- [ ] Define `oodaSpawnData` struct in `kernel/ooda.go`: `type oodaSpawnData struct { Agent string \`json:"agent,omitempty"\`; Model string \`json:"model,omitempty"\` }`
- [ ] Modify `oodaActSpawn` in `kernel/ooda.go` to parse `decision.Data` for agent field
- [ ] If agent field present and `k.agentLoader != nil`, load agent and pass to `k.Spawn()`
- [ ] Update `oodaDecidePromptTemplate` to mention agent field support
- [ ] Inject `agentLoader` in `cmd/rnix/main.go` daemon startup
- [ ] Run tests: `go test -race -run TestOODAActSpawn ./kernel/...`

---

### Test: TestOODA_ChildInheritsOODAMode / TestOODA_ChildLinearMode

**File:** `kernel/ooda_reasoning_test.go`

**Tasks to make these tests pass:**

- [ ] Verify that when `oodaActSpawn` loads agent with `Reasoning: "ooda"` and passes it to `k.Spawn()`, the child process gets OODA enabled via the Spawn reasoning propagation (Task 1 + Task 2 combined)
- [ ] Run tests: `go test -race -run TestOODA_Child ./kernel/...`

---

## Running Tests

```bash
# Run all failing tests for this story
go test -race -run "TestAgentManifest_ReasoningField|TestAgentLoader_DefaultReasoningMode|TestAgentLoader_InvalidReasoningMode|TestAgentLoader_LinearReasoningMode" ./agents/...
go test -race -run "TestSpawn_AgentReasoning|TestSpawn_ReasoningModePriority|TestOODAActSpawn|TestOODA_Child" ./kernel/...

# Run all agents package tests
go test -race ./agents/...

# Run all kernel package tests
go test -race ./kernel/...

# Run specific test
go test -race -run TestOODAActSpawn_WithAgent ./kernel/...

# Run with verbose output
go test -race -v -run TestSpawn_AgentReasoningOODA ./kernel/...

# Run all tests with coverage
go test -race -cover ./agents/... ./kernel/...
```

---

## Red-Green-Refactor Workflow

### RED Phase (Complete)

**TEA Agent Responsibilities:**

- All tests written and failing (compilation errors expected)
- Test fixtures created (testdata YAML files)
- Test helpers created (testAgentInfoWithReasoning factory)
- Implementation checklist created with task-to-test mapping
- Mock requirements documented (agentLoader injection)

**Verification:**

- `agents/loader_reasoning_test.go`: Fails to compile -- `AgentManifest.Reasoning` undefined
- `kernel/ooda_reasoning_test.go`: Fails to compile -- `KernelImpl.SetAgentLoader` undefined
- Failures are due to missing implementation, not test bugs

---

### GREEN Phase (DEV Team - Next Steps)

**DEV Agent Responsibilities:**

1. **Task 1**: Add `Reasoning` field to `AgentManifest` (agents/types.go)
2. **Task 1**: Add validation in `Load()` (agents/loader.go)
3. **Task 2**: Add reasoning propagation in `Spawn()` (kernel/kernel.go)
4. **Task 3**: Add `agentLoader` field and `SetAgentLoader` method (kernel/kernel.go)
5. **Task 3**: Extend `oodaActSpawn` with agent loading (kernel/ooda.go)
6. **Task 3**: Update decide prompt template (kernel/ooda.go)
7. **Task 4**: Write integration tests verifying child OODA inheritance (kernel/ooda_reasoning_test.go)
8. **Task 5**: Create example OODA agent (lib/agents/ooda-demo/)
9. **Task 5**: Inject agentLoader in daemon startup (cmd/rnix/main.go)

**Key Principles:**

- One test at a time (start with agents/types.go Reasoning field)
- Minimal implementation (don't over-engineer)
- Run tests frequently with `-race` flag

---

### REFACTOR Phase (DEV Team - After All Tests Pass)

1. Verify all tests pass with `go test -race ./agents/... ./kernel/...`
2. Review error messages for clarity
3. Check concurrent safety of `agentLoader` field
4. Ensure no import cycle between kernel and agents packages
5. Run full suite: `make test`

---

## Next Steps

1. **Review this checklist** with team
2. **Run failing tests** to confirm RED phase: compilation errors expected
3. **Begin implementation** using implementation checklist (Task 1 first)
4. **Work one test at a time** (red -> green for each)
5. **When all tests pass**, refactor code for quality
6. **Run full test suite**: `make test`
7. **Update story status** to 'done' in sprint-status.yaml

---

## Knowledge Base References Applied

- **test-levels-framework.md** - Backend test level selection (unit for parsing, integration for kernel interaction)
- **test-priorities-matrix.md** - P1 priority assignment (core agent lifecycle, high complexity)
- **data-factories.md** - Go test helper factory pattern (testAgentInfoWithReasoning)
- **test-quality.md** - Test isolation, determinism, Given-When-Then structure
- **test-healing-patterns.md** - Error handling patterns (graceful degradation on agent not found)

---

## Test Execution Evidence

### Initial Test Run (RED Phase Verification)

**Command:** `go vet ./agents/... ./kernel/...`

**Results:**

```
# github.com/rnixai/rnix/agents
agents/loader_reasoning_test.go:29:19: info.Manifest.Reasoning undefined (type AgentManifest has no field or method Reasoning)

# github.com/rnixai/rnix/kernel
kernel/ooda_reasoning_test.go:260:4: k.SetAgentLoader undefined (type *KernelImpl has no field or method SetAgentLoader)
```

**Summary:**

- Total tests: 12 (4 unit + 8 integration, counting subtests)
- Passing: 0 (expected -- compilation errors)
- Failing: 12 (expected -- missing implementation)
- Status: RED phase verified

**Expected Failure Reasons:**

- `AgentManifest.Reasoning` field does not exist in `agents/types.go`
- `KernelImpl.SetAgentLoader` method does not exist in `kernel/kernel.go`
- Reasoning validation logic not present in `agents/loader.go`
- Agent reasoning propagation not present in `kernel/kernel.go` Spawn method
- `oodaSpawnData` struct not defined in `kernel/ooda.go`
- `oodaActSpawn` does not parse agent name from `decision.Data`

---

## Notes

- Tests reuse existing mock infrastructure from Story 20-1 (`newOODATestKernel`, `mockDynamicLLMFile`)
- `testAgentInfoWithReasoning` factory accepts reasoning parameter for flexible test setup
- Agent loader injection uses function type to preserve kernel's dependency direction
- All tests use `-race` flag per project convention

---

## Contact

**Questions or Issues?**

- Refer to CLAUDE.md for project conventions
- Check existing OODA tests in `kernel/ooda_test.go` for patterns
- Review Story 20-1 implementation for OODA core infrastructure

---

**Generated by BMad TEA Agent** - 2026-03-10
