---
stepsCompleted:
  - 'step-01-preflight-and-context'
  - 'step-02-generation-mode'
  - 'step-03-test-strategy'
  - 'step-04-generate-tests'
  - 'step-04c-aggregate'
  - 'step-05-validate-and-complete'
lastStep: 'step-05-validate-and-complete'
lastSaved: '2026-03-01'
workflowType: 'testarch-atdd'
inputDocuments:
  - '_bmad-output/implementation-artifacts/8-4-local-skill-registry-and-skill-list.md'
  - 'skillpkg/types.go'
  - 'skillpkg/installer.go'
  - 'skillpkg/registry.go'
  - 'skills/loader.go'
  - 'skills/types.go'
  - 'cmd/rnix/skill.go'
  - 'cmd/rnix/skill_test.go'
  - 'skillpkg/installer_test.go'
  - 'skillpkg/update_test.go'
---

# ATDD Checklist - Epic 8, Story 8.4: 本地 Skill 注册表与 skill list

**Date:** 2026-03-01
**Author:** Decker
**Primary Test Level:** Unit (Go backend)

---

## Story Summary

本 Story 实现 `skill list` 子命令，允许用户查看所有已安装的 Skill（包括系统自带和社区安装的），输出表格包含 NAME、VERSION、PATH、DESCRIPTION 列。

**As a** 用户
**I want** 通过 `skill list` 查看所有已安装的 Skill
**So that** 我了解本地可用的能力模块

---

## Acceptance Criteria

1. **skill list 表格输出** — Given 本地 Skill 注册表已维护，When 执行 `skill list`，Then 输出表格：NAME、VERSION、PATH、DESCRIPTION，And 包含系统自带 Skill 和社区安装的 Skill
2. **无社区 Skill 时的提示** — Given 无已安装 Skill（除系统自带），When 执行 `skill list`，Then 显示系统自带 Skill + `Tip: skill search <keyword> 发现更多 Skill`
3. **JSON 输出** — Given 使用 `--json` flag，When 列出，Then 输出 JSON 数组，字段 snake_case

---

## Test Strategy

**Stack Detection:** `backend` (Go project, go.mod detected)
**Generation Mode:** AI Generation (backend, no browser recording needed)
**Test Levels:** Unit tests only (Go `testing` package)
**No E2E/Browser Tests:** Pure backend project

### AC-to-Test Mapping

| AC | Test Scenario | Level | Priority | File |
|----|--------------|-------|----------|------|
| AC#1 | ListAll aggregates builtin-only skills | Unit | P0 | `skillpkg/list_test.go` |
| AC#1 | ListAll aggregates mixed builtin+community | Unit | P0 | `skillpkg/list_test.go` |
| AC#1 | ListAll returns sorted entries by name | Unit | P1 | `skillpkg/list_test.go` |
| AC#1 | ListAll includes Path field | Unit | P1 | `skillpkg/list_test.go` |
| AC#1 | list subcommand registered under skill | Unit | P0 | `cmd/rnix/skill_test.go` |
| AC#2 | (Tip logic tested at CLI layer, not in ATDD) | - | P1 | - |
| AC#3 | JSON output with snake_case fields | Unit | P0 | `cmd/rnix/skill_test.go` |
| AC#3 | Empty list returns skills=[] not null | Unit | P0 | `cmd/rnix/skill_test.go` |
| AC#3 | Nil entries handled gracefully | Unit | P1 | `cmd/rnix/skill_test.go` |
| Edge | Invalid SKILL.md directories skipped | Unit | P1 | `skillpkg/list_test.go` |
| Edge | Empty basePath returns empty slice | Unit | P1 | `skillpkg/list_test.go` |
| Edge | .registry.yaml not listed as skill | Unit | P2 | `skillpkg/list_test.go` |
| Edge | NoArgs validation on list command | Unit | P2 | `cmd/rnix/skill_test.go` |

---

## Failing Tests Created (RED Phase)

### Unit Tests - Installer Layer (8 tests)

**File:** `skillpkg/list_test.go` (208 lines)

- **Test:** `TestInstaller_ListAll_BuiltinOnly`
  - **Status:** RED - `installer.ListAll` undefined (type *Installer has no field or method ListAll)
  - **Verifies:** AC#1 - Builtin skills from filesystem are listed with Source="builtin"

- **Test:** `TestInstaller_ListAll_Mixed`
  - **Status:** RED - `installer.ListAll` undefined
  - **Verifies:** AC#1 - Both registry (community) and filesystem (builtin) skills aggregated

- **Test:** `TestInstaller_ListAll_Empty`
  - **Status:** RED - `installer.ListAll` undefined
  - **Verifies:** AC#3 - Empty basePath returns non-nil empty slice

- **Test:** `TestInstaller_ListAll_InvalidSkillSkipped`
  - **Status:** RED - `installer.ListAll` undefined
  - **Verifies:** Edge case - Invalid SKILL.md dirs are skipped without error

- **Test:** `TestInstaller_ListAll_SortedByName`
  - **Status:** RED - `installer.ListAll` undefined
  - **Verifies:** AC#1 - Results sorted alphabetically by name

- **Test:** `TestInstaller_ListAll_PathField`
  - **Status:** RED - `installer.ListAll` undefined
  - **Verifies:** AC#1 - Path field is populated

- **Test:** `TestInstaller_ListAll_RegistrySkipsDotFiles`
  - **Status:** RED - `installer.ListAll` undefined
  - **Verifies:** Edge case - .registry.yaml not listed as skill

### Unit Tests - CLI Layer (5 tests)

**File:** `cmd/rnix/skill_test.go` (appended, ~130 lines of new tests)

- **Test:** `TestSkillListCmd_Registered`
  - **Status:** RED - `skillListCmd` not registered (no "list" subcommand found)
  - **Verifies:** AC#1 - list subcommand registered under skill

- **Test:** `TestSkillList_NoArgs_Required`
  - **Status:** RED - `skillListCmd` not registered
  - **Verifies:** AC#1 - list command uses cobra.NoArgs

- **Test:** `TestSkillList_JSONOutput`
  - **Status:** RED - `skillpkg.ListEntry` undefined, `renderSkillListJSON` undefined
  - **Verifies:** AC#3 - JSON output with snake_case fields

- **Test:** `TestSkillList_EmptyResult_JSONOutput`
  - **Status:** RED - `skillpkg.ListEntry` undefined, `renderSkillListJSON` undefined
  - **Verifies:** AC#3 - Empty skills list JSON outputs []

- **Test:** `TestSkillList_NilEntries_JSONOutput`
  - **Status:** RED - `skillpkg.ListEntry` undefined, `renderSkillListJSON` undefined
  - **Verifies:** AC#3 - Nil input handled as [] in JSON

---

## Data Factories Created

### Test Skill Directory Factory

**File:** `skillpkg/list_test.go` (helper function)

**Exports:**
- `createTestSkillDir(t, basePath, name, description)` - Create a skill directory with valid SKILL.md

**Example Usage:**

```go
dir := t.TempDir()
createTestSkillDir(t, dir, "code-analysis", "Analyze code quality")
```

---

## Fixtures Created

N/A - Go backend tests use `t.TempDir()` for test isolation, no framework-level fixtures needed. Each test creates its own isolated filesystem state.

---

## Mock Requirements

N/A - `ListAll()` is a pure local operation (no network requests). Tests use `t.TempDir()` with real filesystem operations. The `setupMockRegistry()` helper from existing tests is reused only because `NewInstaller` requires a `*RegistryClient` parameter.

---

## Required data-testid Attributes

N/A - Backend project, no UI elements.

---

## Implementation Checklist

### Test: TestInstaller_ListAll_BuiltinOnly (+ all ListAll tests)

**File:** `skillpkg/list_test.go`

**Tasks to make these tests pass:**

- [ ] Add `ListEntry` struct to `skillpkg/types.go` with fields: Name, Version, Path, Description, Source (all with `json` tags in snake_case)
- [ ] Implement `Installer.ListAll() ([]ListEntry, error)` in `skillpkg/installer.go`
  - [ ] Call `inst.registry.List()` to get registered entries
  - [ ] Build map of registered names
  - [ ] `os.ReadDir(inst.basePath)` to scan for skill directories
  - [ ] Skip non-directory entries, `.` files, and `.registry.yaml`
  - [ ] For each directory, try `inst.skillLoader.LoadMetadata(name)` for description
  - [ ] Merge: if in registry map, use registry info (version, source); else Source="builtin"
  - [ ] Initialize result with `make([]ListEntry, 0)` (not nil)
  - [ ] Sort by Name using `sort.Slice`
  - [ ] Return result
- [ ] Run tests: `go test ./skillpkg/ -run TestInstaller_ListAll -race -v`
- [ ] All ListAll tests pass (green phase)

### Test: TestSkillListCmd_Registered (+ all CLI tests)

**File:** `cmd/rnix/skill_test.go`

**Tasks to make these tests pass:**

- [ ] Define `skillListCmd` in `cmd/rnix/skill.go`: Use="list", Short="List all installed skills", Args=cobra.NoArgs
- [ ] Register with `skillCmd.AddCommand(skillListCmd)` in `init()`
- [ ] Implement `runSkillList(cmd, args)` function:
  - [ ] Create Installer (reuse skillRegistryURL, basePath, registry, skillLoader pattern)
  - [ ] Call `installer.ListAll()`
  - [ ] Terminal mode: table format with `[skill]` prefix, NAME/VERSION/SOURCE/DESCRIPTION columns
  - [ ] Terminal mode (no community): append Tip line
  - [ ] JSON mode: `renderSkillListJSON(r, entries)` via `JSONResponse{OK: true, Data: skillListJSONData{Skills: entries}}`
  - [ ] Quiet mode: print skill names only, one per line
- [ ] Define `skillListJSONData` struct with `Skills []skillpkg.ListEntry`
- [ ] Implement `renderSkillListJSON(r, entries)` function (handle nil -> [])
- [ ] Run tests: `go test ./cmd/rnix/ -run TestSkillList -race -v`
- [ ] All CLI list tests pass (green phase)

---

## Running Tests

```bash
# Run all failing tests for this story (will fail to compile in RED phase)
go test ./skillpkg/ -run TestInstaller_ListAll -race -v
go test ./cmd/rnix/ -run TestSkillList -race -v

# Run all skillpkg tests
go test ./skillpkg/ -race -v

# Run all cmd/rnix tests
go test ./cmd/rnix/ -race -v

# Run all project tests
make test

# Run with coverage
go test ./skillpkg/ -run TestInstaller_ListAll -race -coverprofile=coverage.out -v
go tool cover -html=coverage.out
```

---

## Red-Green-Refactor Workflow

### RED Phase (Complete)

**TEA Agent Responsibilities:**

- All tests written and failing (compilation errors)
- Test helper `createTestSkillDir` created for test isolation
- No framework-level fixtures needed (Go `t.TempDir()`)
- No mock requirements (ListAll is pure local operation)
- No data-testid requirements (backend project)
- Implementation checklist created

**Verification:**

- `go vet ./skillpkg/` fails: `installer.ListAll undefined`
- `go vet ./cmd/rnix/` fails: `skillpkg.ListEntry undefined`, `renderSkillListJSON undefined`
- Tests fail due to missing implementation, not test bugs

---

### GREEN Phase (DEV Team - Next Steps)

**DEV Agent Responsibilities:**

1. **Start with `skillpkg/types.go`**: Add `ListEntry` struct
2. **Implement `ListAll()`** in `skillpkg/installer.go`
3. **Run `skillpkg` tests**: `go test ./skillpkg/ -run TestInstaller_ListAll -race -v`
4. **Implement CLI**: Add `skillListCmd`, `runSkillList`, `renderSkillListJSON` in `cmd/rnix/skill.go`
5. **Run CLI tests**: `go test ./cmd/rnix/ -run TestSkillList -race -v`
6. **Run full suite**: `make test`

**Key Principles:**

- One test at a time (implement ListEntry first, then ListAll, then CLI)
- Minimal implementation (don't over-engineer)
- Run tests frequently (immediate feedback)
- Use implementation checklist as roadmap

---

### REFACTOR Phase (DEV Team - After All Tests Pass)

**DEV Agent Responsibilities:**

1. Verify all tests pass (`make test`)
2. Run linter (`make lint`)
3. Review code quality
4. Ensure Unicode truncation uses `[]rune` for description column
5. Verify `make build` compiles

---

## Next Steps

1. **Review this checklist and failing tests** with the dev workflow
2. **Run failing tests** to confirm RED phase: `go vet ./skillpkg/ && go vet ./cmd/rnix/`
3. **Begin implementation** using implementation checklist as guide
4. **Work one test group at a time** (types -> ListAll -> CLI)
5. **When all tests pass**, run `make all` for full validation
6. **When refactoring complete**, commit changes

---

## Knowledge Base References Applied

- **test-quality.md** - Test design principles (Given-When-Then, one assertion per test, determinism, isolation)
- **test-levels-framework.md** - Test level selection (Unit for Go backend)
- **data-factories.md** - Factory pattern adapted to Go helper functions

---

## Test Execution Evidence

### Initial Test Run (RED Phase Verification)

**Command:** `go vet ./skillpkg/ && go vet ./cmd/rnix/`

**Results:**

```
# github.com/rnixai/rnix/skillpkg
vet: skillpkg/list_test.go:55:28: installer.ListAll undefined (type *Installer has no field or method ListAll)

# github.com/rnixai/rnix/cmd/rnix
vet: cmd/rnix/skill_test.go:640:24: undefined: skillpkg.ListEntry
```

**Summary:**

- Total tests: 12 (7 Installer + 5 CLI)
- Passing: 0 (expected - compilation errors)
- Failing: 12 (expected - ListAll/ListEntry/renderSkillListJSON undefined)
- Status: RED phase verified

**Expected Failure Messages:**
- `installer.ListAll undefined (type *Installer has no field or method ListAll)`
- `undefined: skillpkg.ListEntry`
- `undefined: renderSkillListJSON`

---

## Notes

- Go TDD RED phase manifests as **compilation failures** rather than runtime test.skip(). The tests reference types and methods that don't exist yet.
- All tests follow existing project patterns (see `update_test.go`, `skill_test.go` for reference).
- `createTestSkillDir` helper creates isolated SKILL.md fixtures per test using `t.TempDir()`.
- The `ListAll()` method is a pure local operation -- no network, no RegistryClient needed at runtime (though Installer construction requires it).
- `make([]ListEntry, 0)` must be used (not `var entries []ListEntry`) to ensure JSON serialization as `[]` not `null`.
- Description truncation in terminal output must use `[]rune` to handle Unicode correctly (lesson from Story 8.3).

---

**Generated by BMad TEA Agent** - 2026-03-01
