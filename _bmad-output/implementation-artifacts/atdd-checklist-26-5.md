---
stepsCompleted:
  - step-01-preflight-and-context
  - step-02-generation-mode
  - step-03-test-strategy
  - step-04-generate-tests
  - step-05-validate-and-complete
lastStep: step-05-validate-and-complete
lastSaved: '2026-03-18'
inputDocuments:
  - _bmad-output/implementation-artifacts/26-5-documentation-update.md
  - _bmad-output/planning-artifacts/prd/functional-requirements.md
  - _bmad-output/planning-artifacts/prd/non-functional-requirements.md
  - _bmad-output/planning-artifacts/prd/project-scoping-phased-development.md
  - _bmad-output/planning-artifacts/prd/index.md
  - _bmad-output/planning-artifacts/architecture/core-architectural-decisions.md
  - _bmad-output/planning-artifacts/architecture/architecture-validation-results.md
  - _bmad-output/project-context.md
  - CLAUDE.md
---

# ATDD Checklist — Story 26.5: 文档更新——统一推理循环

## Preflight

- **Stack**: backend (Go 1.26)
- **Test framework**: Go `testing` standard library
- **Generation mode**: AI generation (no browser, pure documentation verification)
- **Story file**: `_bmad-output/implementation-artifacts/26-5-documentation-update.md`
- **Story status**: ready-for-dev

## Test Strategy

| AC | Test Function | Level | Priority | Assertions |
|----|---------------|-------|----------|------------|
| AC-1 | `TestDoc_PRD_FR_UnifiedReasoningLoop` | Integration | P0 | 10 |
| AC-2 | `TestDoc_PRD_NFR44_Updated` | Integration | P0 | 2 |
| AC-3 | `TestDoc_PRD_Scoping_Phase3` | Integration | P1 | 4 |
| AC-4 | `TestDoc_PRD_Index_TOCLink` | Integration | P1 | 3 |
| AC-5 | `TestDoc_Arch_Decision23` | Integration | P0 | 12 |
| AC-6 | `TestDoc_Arch_ValidationResults_NoOODA` | Integration | P1 | 2 |
| AC-7 | `TestDoc_ProjectContext_ReasoningLoop` | Integration | P0 | 13 |
| AC-8 | `TestDoc_CLAUDE_MD_UnifiedLoop` | Integration | P1 | 1 |

**Total**: 8 tests, 47 assertions

## Test File

- **Path**: `atdd_26_5_documentation_update_test.go` (project root, package `rnix`)
- **Run**: `go test -race -run 'TestDoc_' -v .`

## RED Phase Validation (2026-03-18)

All 8 tests **FAIL** as expected:

```
--- FAIL: TestDoc_PRD_FR_UnifiedReasoningLoop (0.00s)         # 10 assertions failed
--- FAIL: TestDoc_PRD_NFR44_Updated (0.00s)                   # 2 assertions failed
--- FAIL: TestDoc_PRD_Scoping_Phase3 (0.00s)                  # 4 assertions failed
--- FAIL: TestDoc_PRD_Index_TOCLink (0.00s)                   # 3 assertions failed
--- FAIL: TestDoc_Arch_Decision23 (0.00s)                     # 12 assertions failed
--- FAIL: TestDoc_Arch_ValidationResults_NoOODA (0.00s)       # 2 assertions failed
--- FAIL: TestDoc_ProjectContext_ReasoningLoop (0.00s)         # 13 assertions failed
--- FAIL: TestDoc_CLAUDE_MD_UnifiedLoop (0.00s)               # 1 assertion failed
FAIL    github.com/rnixai/rnix  0.011s
```

**Failure reasons** (correct for RED phase):
- New content not yet present in documents (e.g., "Unified Reasoning Loop" heading, FR112-FR117 rewrite, Decision 23)
- Old OODA content still present (e.g., "OODA 自主决策", "ooda.go", "OODA specialize action")

## GREEN Phase Instructions

1. Apply documentation changes per Story 26-5 Tasks 1–8
2. Run `go test -race -run 'TestDoc_' -v .` — all 8 tests should pass
3. Run `make all` to verify no code breakage (AC-9)
