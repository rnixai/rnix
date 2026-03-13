---
stepsCompleted: ['step-01-preflight-and-context', 'step-02-generation-mode', 'step-03-test-strategy', 'step-04-generate-tests', 'step-05-validate-and-complete']
lastStep: 'step-05-validate-and-complete'
lastSaved: '2026-03-13'
workflowType: 'testarch-atdd'
inputDocuments:
  - '_bmad-output/implementation-artifacts/21-4-skill-synergy-declaration-and-auto-detection.md'
  - 'skills/types.go'
  - 'skills/loader.go'
  - 'skills/loader_test.go'
  - 'agents/types.go'
  - 'agents/loader.go'
  - 'skills/testdata/mock-skill/SKILL.md'
---

# ATDD Checklist - Epic 21, Story 4: Skill Synergy 声明与自动检测

**Date:** 2026-03-13
**Author:** Decker
**Primary Test Level:** Unit + Integration (Go backend)

---

## Story Summary

SKILL.md 支持声明 synergy 字段，系统在加载多个 Skill 时自动检测并激活协同效应，将涌现指令追加到 system prompt 末尾。未声明 synergy 字段时行为不变（向后兼容）。

**As a** 平台构建者
**I want** SKILL.md 可以声明 synergy 字段，系统在加载多个 Skill 时自动检测并激活协同效应
**So that** Skill 组合能产生 1+1>2 的涌现能力

---

## Acceptance Criteria

1. **AC1: Synergy 字段声明** - SKILL.md frontmatter 的 synergy 字段正确解析到 SkillManifest.Synergies 字段，未声明时默认为 nil（向后兼容）
2. **AC2: 自动检测 Synergy 组合** - 智能体同时加载多个 Skill 时，自动检测 synergy 命中并将涌现指令追加到 system prompt
3. **AC3: 多重 Synergy 同时命中** - 所有命中的 synergy 指令都被追加（不遗漏），同一条指令不重复追加（去重）
4. **AC4: 性能要求** - 检测开销 <= 100ms（NFR46）
5. **AC5: 向后兼容** - 现有 SKILL.md 无 synergy 字段时行为与当前完全一致

---

## Failing Tests Created (RED Phase)

### Unit Tests - skills/atdd_21_4_synergy_decl_test.go (6 tests)

**File:** `skills/atdd_21_4_synergy_decl_test.go`

- **Test:** `TestSkillManifest_Synergies_Parsed` (21.4-UNIT-001)
  - **Status:** RED - SynergyDecl 类型和 Synergies 字段不存在
  - **Verifies:** AC1 - synergy 字段正确解析到 SkillManifest.Synergies
  - **Priority:** P0

- **Test:** `TestSkillManifest_Synergies_Empty` (21.4-UNIT-002)
  - **Status:** RED - Synergies 字段不存在
  - **Verifies:** AC5 - 无 synergy 字段时 Synergies 为 nil
  - **Priority:** P0

- **Test:** `TestSynergyDecl_Fields` (21.4-UNIT-003)
  - **Status:** RED - SynergyDecl 类型不存在
  - **Verifies:** AC1 - SynergyDecl 类型有 With 和 Instruction 字段
  - **Priority:** P0

- **Test:** `TestSkillLoader_LoadFull_WithSynergy` (21.4-UNIT-004)
  - **Status:** RED - Synergies 字段不存在
  - **Verifies:** AC1 - SkillLoader 加载含 synergy 的 SKILL.md
  - **Priority:** P1

- **Test:** `TestSkillLoader_LoadFull_WithoutSynergy` (21.4-UNIT-005)
  - **Status:** RED - Synergies 字段不存在
  - **Verifies:** AC5 - 无 synergy 的 SKILL.md 加载正常
  - **Priority:** P1

- **Test:** `TestSkillLoader_LoadFull_RealSkill_NoSynergyField` (21.4-UNIT-006)
  - **Status:** RED - Synergies 字段不存在
  - **Verifies:** AC5 - 真实 code-analysis skill 无 synergy 字段时加载正常
  - **Priority:** P1

### Unit Tests - skills/atdd_21_4_synergy_detect_test.go (10 tests)

**File:** `skills/atdd_21_4_synergy_detect_test.go`

- **Test:** `TestDetectSynergies_NoSynergies` (21.4-UNIT-007)
  - **Status:** RED - DetectSynergies 函数不存在
  - **Verifies:** AC2 - 无 synergy 声明时返回空
  - **Priority:** P0

- **Test:** `TestDetectSynergies_SingleMatch` (21.4-UNIT-008)
  - **Status:** RED - DetectSynergies 函数不存在
  - **Verifies:** AC2 - A 声明 with: B，两者都加载，命中 1 条
  - **Priority:** P0

- **Test:** `TestDetectSynergies_BidirectionalMatch` (21.4-UNIT-009)
  - **Status:** RED - DetectSynergies 函数不存在
  - **Verifies:** AC2/AC3 - A→B 和 B→A 同时声明，两条都命中
  - **Priority:** P0

- **Test:** `TestDetectSynergies_PartialLoad` (21.4-UNIT-010)
  - **Status:** RED - DetectSynergies 函数不存在
  - **Verifies:** AC2 - A 声明 with: B，但 B 未加载，不命中
  - **Priority:** P0

- **Test:** `TestDetectSynergies_MultipleMatches` (21.4-UNIT-011)
  - **Status:** RED - DetectSynergies 函数不存在
  - **Verifies:** AC3 - 多个交叉 synergy，全部命中
  - **Priority:** P0

- **Test:** `TestDetectSynergies_Dedup` (21.4-UNIT-012)
  - **Status:** RED - DetectSynergies 函数不存在
  - **Verifies:** AC3 - 相同 instruction 只追加一次
  - **Priority:** P0

- **Test:** `TestDetectSynergies_DeterministicOrder` (21.4-UNIT-013)
  - **Status:** RED - DetectSynergies 函数不存在
  - **Verifies:** AC3 - 结果按字母序排列（确定性）
  - **Priority:** P0

- **Test:** `TestDetectSynergies_NilInput` (21.4-UNIT-014)
  - **Status:** RED - DetectSynergies 函数不存在
  - **Verifies:** AC5 - nil 输入返回空，无 panic
  - **Priority:** P1

- **Test:** `TestDetectSynergies_EmptySlice` (21.4-UNIT-015)
  - **Status:** RED - DetectSynergies 函数不存在
  - **Verifies:** AC5 - 空切片输入返回空
  - **Priority:** P1

- **Test:** `TestDetectSynergies_Performance` (21.4-PERF-001)
  - **Status:** RED - DetectSynergies 函数不存在
  - **Verifies:** AC4 - 100 个 Skill 各含 10 条 synergy，检测耗时 < 100ms
  - **Priority:** P1

### Integration Tests - agents/atdd_21_4_synergy_prompt_test.go (7 tests)

**File:** `agents/atdd_21_4_synergy_prompt_test.go`

- **Test:** `TestAgentInfo_SystemPrompt_WithSynergy` (21.4-INT-001)
  - **Status:** RED - SystemPrompt() 中无 synergy 检测逻辑
  - **Verifies:** AC2 - synergy 命中时 prompt 包含 [Skill Synergy] 段落
  - **Priority:** P0

- **Test:** `TestAgentInfo_SystemPrompt_NoSynergy` (21.4-INT-002)
  - **Status:** RED - SynergyDecl 类型不存在（编译失败）
  - **Verifies:** AC5 - 无 synergy 时 prompt 与原逻辑完全一致
  - **Priority:** P0

- **Test:** `TestAgentInfo_SystemPrompt_SynergyDedup` (21.4-INT-003)
  - **Status:** RED - SynergyDecl 类型不存在
  - **Verifies:** AC3 - 重复 instruction 只出现一次
  - **Priority:** P0

- **Test:** `TestAgentInfo_SystemPrompt_NoSkills_NoSynergySection` (21.4-INT-004)
  - **Status:** RED - 编译依赖 SynergyDecl（同包测试）
  - **Verifies:** AC5 - 无 skills 时无 synergy 段落
  - **Priority:** P1

- **Test:** `TestAgentInfo_AllowedTools_UnaffectedBySynergy` (21.4-INT-005)
  - **Status:** RED - SynergyDecl 类型不存在
  - **Verifies:** AC2 - synergy 不影响 AllowedTools 白名单
  - **Priority:** P1

- **Test:** `TestAgentInfo_SystemPrompt_MultipleSynergyMatches` (21.4-INT-006)
  - **Status:** RED - SynergyDecl 类型不存在
  - **Verifies:** AC3 - 三个 skill 交叉 synergy，全部指令追加
  - **Priority:** P0

- **Test:** `TestAgentInfo_SystemPrompt_SynergyAfterBodies` (21.4-INT-007)
  - **Status:** RED - SynergyDecl 类型不存在
  - **Verifies:** AC2 - [Skill Synergy] 出现在 skill bodies 之后
  - **Priority:** P1

### Test Fixtures Created

**File:** `skills/testdata/with-synergy/SKILL.md`
- 含 synergy 声明的测试 SKILL.md（skill A 声明与 test-skill-b 的协同）

**File:** `skills/testdata/with-synergy-b/SKILL.md`
- 含 synergy 声明的测试 SKILL.md（skill B 声明与 test-skill-a 的协同）

---

## Implementation Checklist

### Task 1: 扩展 SkillManifest 类型 (skills/types.go)

**Tests to make pass:** 21.4-UNIT-001 ~ 21.4-UNIT-006

- [ ] 新增 `SynergyDecl` 类型（With string, Instruction string）
- [ ] `SkillManifest` 添加 `Synergies []SynergyDecl` 字段（yaml tag: `synergy,omitempty`）
- [ ] Run: `go test -race -run "TestSkillManifest_Synergies|TestSynergyDecl_Fields|TestSkillLoader_LoadFull_WithSynergy|TestSkillLoader_LoadFull_WithoutSynergy|TestSkillLoader_LoadFull_RealSkill" ./skills/...`

### Task 2: Synergy 检测引擎 (skills/synergy.go)

**Tests to make pass:** 21.4-UNIT-007 ~ 21.4-PERF-001

- [ ] 新建 `skills/synergy.go`
- [ ] 实现 `DetectSynergies(skills []*SkillInfo) []string` 纯函数
  - 构建 skill name set
  - 遍历 synergy 声明检查 With 是否在 loaded set 中
  - 用 map 去重 instruction
  - 返回排序后的去重指令列表
- [ ] Run: `go test -race -run "TestDetectSynergies" ./skills/...`

### Task 3: SystemPrompt 集成 (agents/types.go)

**Tests to make pass:** 21.4-INT-001 ~ 21.4-INT-007

- [ ] 修改 `AgentInfo.SystemPrompt()` 方法
  - 在 skill bodies 之后调用 `skills.DetectSynergies(a.Skills)`
  - 有命中指令时追加 `\n\n[Skill Synergy]\n\n` + 各条指令换行拼接
  - 无命中时不追加任何内容
- [ ] Run: `go test -race -run "TestAgentInfo_SystemPrompt_WithSynergy|TestAgentInfo_SystemPrompt_NoSynergy|TestAgentInfo_SystemPrompt_SynergyDedup|TestAgentInfo_SystemPrompt_NoSkills_NoSynergySection|TestAgentInfo_AllowedTools_UnaffectedBySynergy|TestAgentInfo_SystemPrompt_MultipleSynergyMatches|TestAgentInfo_SystemPrompt_SynergyAfterBodies" ./agents/...`

---

## Running Tests

```bash
# Run all Story 21.4 unit tests (synergy declaration)
go test -race -run "TestSkillManifest_Synergies|TestSynergyDecl_Fields|TestSkillLoader_LoadFull_WithSynergy|TestSkillLoader_LoadFull_WithoutSynergy|TestSkillLoader_LoadFull_RealSkill" ./skills/...

# Run all Story 21.4 unit tests (synergy detection)
go test -race -run "TestDetectSynergies" ./skills/...

# Run all Story 21.4 integration tests (SystemPrompt)
go test -race -run "TestAgentInfo_SystemPrompt_WithSynergy|TestAgentInfo_SystemPrompt_NoSynergy|TestAgentInfo_SystemPrompt_SynergyDedup|TestAgentInfo_SystemPrompt_NoSkills_NoSynergySection|TestAgentInfo_AllowedTools_UnaffectedBySynergy|TestAgentInfo_SystemPrompt_MultipleSynergyMatches|TestAgentInfo_SystemPrompt_SynergyAfterBodies" ./agents/...

# Run all Story 21.4 tests
go test -race -run "TestSkillManifest_Synergies|TestSynergyDecl_Fields|TestSkillLoader_LoadFull_WithSynergy|TestSkillLoader_LoadFull_WithoutSynergy|TestSkillLoader_LoadFull_RealSkill|TestDetectSynergies|TestAgentInfo_SystemPrompt_WithSynergy|TestAgentInfo_SystemPrompt_NoSynergy|TestAgentInfo_SystemPrompt_SynergyDedup|TestAgentInfo_SystemPrompt_NoSkills|TestAgentInfo_AllowedTools_UnaffectedBySynergy|TestAgentInfo_SystemPrompt_MultipleSynergyMatches|TestAgentInfo_SystemPrompt_SynergyAfterBodies" ./skills/... ./agents/...

# Run all tests in affected packages
go test -race ./skills/... ./agents/...
```

---

## Red-Green-Refactor Workflow

### RED Phase (Complete)

**TEA Agent Responsibilities:**

- All 23 tests written and designed to fail (types/methods do not exist)
- Tests cover all 5 acceptance criteria
- Tests follow existing project patterns (skills/loader_test.go, agents/atdd_21_3_alternatives_test.go)
- Test naming convention: `TestSkillManifest_Synergies_*`, `TestSynergyDecl_*`, `TestSkillLoader_LoadFull_*`, `TestDetectSynergies_*`, `TestAgentInfo_SystemPrompt_*`

**Verification:**

- Tests will fail to compile until `skills/types.go` 新增 SynergyDecl 和 Synergies 字段, `skills/synergy.go` 新建 DetectSynergies 函数, `agents/types.go` SystemPrompt() 集成 synergy 注入
- Failure is due to missing types/methods/fields, not test bugs

---

### GREEN Phase (DEV Team - Next Steps)

**Recommended implementation order:**

1. Task 1: 扩展 SkillManifest 类型（最小改动，仅添加类型和字段）
2. Task 2: Synergy 检测引擎（新建文件，纯函数，无外部依赖）
3. Task 3: SystemPrompt 集成（修改已有方法，依赖 Task 1 和 Task 2）

---

### REFACTOR Phase (DEV Team - After All Tests Pass)

- 验证 synergy 不影响 AllowedTools 聚合逻辑
- 确认无 synergy 字段的 SKILL.md 加载行为不变
- 确认 synergy 检测对大量 Skill 的性能满足 100ms 阈值
- 验证 Compose 引擎通过 agentLoader → AgentInfo → SystemPrompt 链路正确传递 synergy

---

## Acceptance Criteria Coverage Matrix

| AC | 测试覆盖 | 测试数 |
|----|---------|--------|
| AC1: Synergy 字段声明 | UNIT-001~004, UNIT-006 | 5 |
| AC2: 自动检测 Synergy 组合 | UNIT-007~010, INT-001, INT-005~007 | 8 |
| AC3: 多重 Synergy 同时命中 | UNIT-009, UNIT-011~013, INT-003, INT-006 | 6 |
| AC4: 性能要求 | PERF-001 | 1 |
| AC5: 向后兼容 | UNIT-002, UNIT-005~006, UNIT-014~015, INT-002, INT-004 | 7 |

---

## Knowledge Base References Applied

- **test-levels-framework.md** - 测试级别选择：纯 backend Go 项目使用 Unit + Integration
- **data-factories.md** - 测试数据构造模式（使用 struct literal 和 table-driven 构造测试数据）
- **test-quality.md** - 测试质量原则（Given-When-Then 注释、确定性、隔离性）
- **test-healing-patterns.md** - 测试修复模式参考
- **test-priorities-matrix.md** - P0-P2 优先级分配

---

## Notes

- SynergyDecl 类型只有 With 和 Instruction 两个字段，保持简单
- DetectSynergies 是纯函数，无状态、无副作用，O(N*M) 复杂度
- synergy 指令追加到 SystemPrompt 末尾，使用 `[Skill Synergy]` 段落标记
- 去重基于 instruction 字符串完全相等，排序保证确定性
- 测试 fixtures 在 `skills/testdata/with-synergy/` 和 `skills/testdata/with-synergy-b/`
- 向后兼容：无 synergy 的 SKILL.md 测试验证行为不变
- 性能测试 100 Skill x 10 synergy 的基准验证 < 100ms
- 文件命名遵循项目惯例：`atdd_21_4_*.go`
- agents/types.go 修改 SystemPrompt() 是唯一的调用点
- synergy 不影响 AllowedTools（INT-005 验证）

---

**Generated by BMad TEA Agent** - 2026-03-13
