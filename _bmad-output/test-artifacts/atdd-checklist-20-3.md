---
stepsCompleted:
  - 'step-01-preflight-and-context'
  - 'step-02-generation-mode'
  - 'step-03-test-strategy'
  - 'step-04-generate-tests'
  - 'step-05-validate-and-complete'
lastStep: 'step-05-validate-and-complete'
lastSaved: '2026-03-10'
workflowType: 'testarch-atdd'
inputDocuments:
  - '_bmad-output/implementation-artifacts/20-3-stem-agent-and-auto-differentiation.md'
  - 'skills/loader.go'
  - 'skills/types.go'
  - 'kernel/kernel.go'
  - 'kernel/ooda.go'
  - 'agents/types.go'
  - 'kernel/kernel_test.go'
  - 'skills/loader_test.go'
  - 'kernel/ooda_test.go'
---

# ATDD Checklist - Epic 20, Story 20-3: Stem Agent 与自动分化

**Date:** 2026-03-10
**Author:** Decker
**Primary Test Level:** Unit + Integration (Go backend)

---

## Story Summary

系统提供通用基底智能体（Stem Agent），根据接收到的意图自动匹配和加载最相关的 Skill 组合，无需预先指定 Agent 模板。

**As a** 平台构建者
**I want** 系统提供通用基底智能体，根据接收到的意图自动匹配和加载最相关的 Skill 组合
**So that** 我不需要预先指定 Agent 模板，系统能自动选择最佳能力组合

---

## Acceptance Criteria

1. **AC#1**: 用户执行 `rnix -i "分析代码" --agent=stem` 时，Stem Agent 分析意图，自动匹配最相关的 Skill 组合（如 code-analysis + git-tools），加载后完成分化
2. **AC#2**: 分化过程实时输出 `[agent/N] differentiating: loading skills [...]`，Skill 匹配和加载 <= 3s（NFR42）

---

## Test Strategy

### Stack Detection
- **Detected Stack**: `backend` (Go 1.26, `go.mod` present)
- **Test Framework**: Go `testing` package with `-race` flag
- **No E2E/Browser tests** needed

### Test Levels
| Level | Count | Purpose |
|-------|-------|---------|
| Unit | 13 | SkillDiscovery, StemMatcher 纯逻辑 |
| Integration | 3 | Spawn + stem 分化端到端 |
| **Total** | **16** | |

### Priority Matrix
| Priority | Tests | Description |
|----------|-------|-------------|
| P0 | 5 | 核心分化流程：DiscoverAll, Match, Spawn differentiation |
| P1 | 6 | 边界情况：空目录、无匹配、错误传播 |
| P2 | 4 | 健壮性：隐藏目录、非目录文件、中文意图 |
| P3 | 1 | NFR42 性能基准 |

---

## Failing Tests Created (RED Phase)

### Unit Tests - skills/discovery_test.go (7 tests)

**File:** `skills/discovery_test.go` (165 lines)

- **[P0] TestSkillDiscovery_DiscoverAll**
  - **Status:** RED - `undefined: NewSkillDiscovery`
  - **Verifies:** 扫描 testdata 目录返回有效 skill 列表，含 name 和 description

- **[P1] TestSkillDiscovery_SkipsInvalidSkills**
  - **Status:** RED - `undefined: NewSkillDiscovery`
  - **Verifies:** 包含无效 SKILL.md 的目录被静默跳过

- **[P1] TestSkillDiscovery_EmptyDirectory**
  - **Status:** RED - `undefined: NewSkillDiscovery`
  - **Verifies:** 空目录返回空列表，不报错

- **[P1] TestSkillDiscovery_NonexistentDirectory**
  - **Status:** RED - `undefined: NewSkillDiscovery`
  - **Verifies:** 不存在的目录返回空列表（优雅降级）

- **[P2] TestSkillDiscovery_SkipsNonDirectories**
  - **Status:** RED - `undefined: NewSkillDiscovery`
  - **Verifies:** 普通文件被跳过，仅处理子目录

- **[P2] TestSkillDiscovery_SkipsHiddenDirectories**
  - **Status:** RED - `undefined: NewSkillDiscovery`
  - **Verifies:** 隐藏目录（.开头）被跳过

### Unit Tests - kernel/stem_test.go (7 tests)

**File:** `kernel/stem_test.go` (192 lines)

- **[P0] TestStemMatcher_Match_CodeAnalysis**
  - **Status:** RED - `undefined: NewStemMatcherFromFunc`
  - **Verifies:** "analyze code" 意图匹配到 code-analysis skill

- **[P1] TestStemMatcher_Match_NoMatch**
  - **Status:** RED - `undefined: NewStemMatcherFromFunc`
  - **Verifies:** 无关意图返回空列表，不报错

- **[P0] TestStemMatcher_Match_MultipleSkills**
  - **Status:** RED - `undefined: NewStemMatcherFromFunc`
  - **Verifies:** 多 skill 匹配，按匹配度降序排列

- **[P1] TestStemMatcher_Match_EmptyIntent**
  - **Status:** RED - `undefined: NewStemMatcherFromFunc`
  - **Verifies:** 空意图返回空列表

- **[P1] TestStemMatcher_Match_DiscoveryError**
  - **Status:** RED - `undefined: NewStemMatcherFromFunc`
  - **Verifies:** Discovery 错误正确传播

- **[P1] TestStemMatcher_Match_EmptySkillList**
  - **Status:** RED - `undefined: NewStemMatcherFromFunc`
  - **Verifies:** 无可用 skill 时返回空列表

- **[P2] TestStemMatcher_Match_ChineseIntent**
  - **Status:** RED - `undefined: NewStemMatcherFromFunc`
  - **Verifies:** 包含英文关键词的意图能正确匹配 skill

- **[P3] TestStemMatcher_Performance_NFR42**
  - **Status:** RED - `undefined: NewStemMatcherFromFunc`
  - **Verifies:** 50 个 skill 集合下 Match 耗时 <= 3s

### Integration Tests - kernel/stem_integration_test.go (3 tests)

**File:** `kernel/stem_integration_test.go` (212 lines)

- **[P0] TestSpawn_StemAgentDifferentiation**
  - **Status:** RED - `undefined: NewStemMatcherFromFunc`, `SetStemMatcher undefined`, `SetSkillLoader undefined`
  - **Verifies:** stem agent spawn 时自动匹配 skill 并加载，AllowedDevices 被填充，OODA 状态初始化

- **[P0] TestSpawn_StemAgentNoMatch**
  - **Status:** RED - `undefined: NewStemMatcherFromFunc`, `SetStemMatcher undefined`
  - **Verifies:** stem agent 无匹配 skill 时以裸进程运行（不报错）

- **[P2] TestSpawn_StemAgentDifferentiationLog**
  - **Status:** RED - `undefined: NewStemMatcherFromFunc`, `SetStemMatcher undefined`
  - **Verifies:** 分化过程产生正确的事件和日志

---

## Data Factories Created

### Mock Skill Discovery Factory

**File:** `kernel/stem_test.go` (内置)

**Exports:**
- `testSkillInfoList()` - 创建包含 5 个预定义 skill 元数据的列表
- `mockSkillDiscovery` struct - 可控的 SkillDiscovery mock，支持注入 skills 列表和 error

### Stem Agent Factory

**File:** `kernel/stem_integration_test.go` (内置)

**Exports:**
- `stemAgentInfo()` - 创建标准 stem agent 定义（name: "stem", skills: [], reasoning: "ooda"）
- `testCallbacks` struct - 最小化 KernelCallbacks 实现，支持事件捕获

---

## Fixtures Created

### Test Data (已有)

**Directory:** `skills/testdata/`

已有的 testdata fixtures 被 Discovery 测试复用：
- `skills/testdata/mock-skill/SKILL.md` - 有效的 mock skill
- `skills/testdata/invalid-manifest/SKILL.md` - 无效 YAML（测试跳过逻辑）
- `skills/testdata/missing-fields/SKILL.md` - 缺少必填字段
- `skills/testdata/no-instructions/.gitkeep` - 无 SKILL.md 的目录

### Kernel Test Helpers (已有)

**File:** `kernel/kernel_test.go`

复用现有测试基础设施：
- `newTestKernel()` - 创建带 mock LLM 设备的内核
- `mockLLMFile` - LLM 设备 mock
- `makeLLMResponse()` - 构造 LLM JSON 响应
- `testAgentInfo()` - 标准测试 agent

---

## Mock Requirements

### Mock Skill Discovery

**Used in:** `kernel/stem_test.go`, `kernel/stem_integration_test.go`

```go
type mockSkillDiscovery struct {
    skills []skills.SkillInfo
    err    error
}
```

**Success Response:** 返回预定义的 `[]skills.SkillInfo` 列表
**Failure Response:** 返回 `errTestDiscovery` 哨兵错误

### Mock Skill Loader (函数注入)

**Used in:** `kernel/stem_integration_test.go`

```go
k.SetSkillLoader(func(name string) (*skills.SkillInfo, error) {
    // 根据 name 返回对应的 SkillInfo 或 error
})
```

---

## Implementation Checklist

### Test: TestSkillDiscovery_DiscoverAll (+ 其他 Discovery 测试)

**File:** `skills/discovery_test.go`

**Tasks to make these tests pass:**

- [ ] 创建 `skills/discovery.go`
- [ ] 实现 `SkillDiscovery` 结构体，包含 `loader *SkillLoader` 和 `basePath string` 字段
- [ ] 实现 `NewSkillDiscovery(loader *SkillLoader, basePath string) *SkillDiscovery` 构造函数
- [ ] 实现 `DiscoverAll() ([]SkillInfo, error)` 方法：
  - 扫描 `basePath` 下所有子目录
  - 跳过非目录文件和隐藏目录（`.` 开头）
  - 对每个子目录调用 `loader.LoadMetadata(name)`
  - 跳过加载失败的目录（不报错）
  - 返回所有有效 skill 的 `SkillInfo` 列表
- [ ] 运行测试: `go test -race -run TestSkillDiscovery ./skills/...`
- [ ] All 7 discovery tests pass (green phase)

### Test: TestStemMatcher_Match_* (+ Performance NFR42)

**File:** `kernel/stem_test.go`

**Tasks to make these tests pass:**

- [ ] 创建 `kernel/stem.go`
- [ ] 实现 `StemMatcher` 结构体
- [ ] 实现 `NewStemMatcherFromFunc(discoverFn func() ([]skills.SkillInfo, error)) *StemMatcher` 构造函数
- [ ] 实现 `Match(intent string) ([]string, error)` 方法：
  - 调用 discoverFn 获取所有可用 skill
  - 将 intent 按空格/标点分词并小写化
  - 将 skill name（按 `-` 分词）和 description（按空格分词）合并为关键词集合
  - 匹配分数 = 交集大小 / intent 关键词数
  - 返回匹配度降序排列的 skill 名称列表
  - 空意图返回空列表
- [ ] 运行测试: `go test -race -run TestStemMatcher ./kernel/...`
- [ ] All 8 matcher tests pass (green phase)

### Test: TestSpawn_StemAgentDifferentiation (+ NoMatch, Log)

**File:** `kernel/stem_integration_test.go`

**Tasks to make these tests pass:**

- [ ] 在 `kernel/kernel.go` 的 `KernelImpl` 新增字段：
  - `stemMatcher *StemMatcher`
  - `skillLoader func(string) (*skills.SkillInfo, error)`
- [ ] 实现 `SetStemMatcher(m *StemMatcher)` setter
- [ ] 实现 `SetSkillLoader(fn func(string) (*skills.SkillInfo, error))` setter
- [ ] 在 `Spawn` 方法的 agent info 处理块中添加 stem 分化逻辑：
  - 检测 `agent.Manifest.Name == "stem" && len(agent.Manifest.Skills) == 0`
  - 调用 `stemMatcher.Match(intent)` 获取匹配 skill 列表
  - 对每个匹配 skill 调用 `skillLoader(name)` 加载完整 SkillInfo
  - 将加载的 SkillInfo append 到 `agent.Skills`
- [ ] 添加分化进度日志输出（emitLog/emitEvent）
- [ ] 创建 `lib/agents/stem/agent.yaml` 和 `lib/agents/stem/instructions.md`
- [ ] 在 `cmd/rnix/main.go` 的 daemon 启动流程注入 StemMatcher 和 SkillLoader
- [ ] 运行测试: `go test -race -run TestSpawn_Stem ./kernel/...`
- [ ] All 3 integration tests pass (green phase)

---

## Running Tests

```bash
# Run all failing tests for this story (currently RED - won't compile)
go test -race -run "TestSkillDiscovery|TestStemMatcher|TestSpawn_Stem" ./skills/... ./kernel/...

# Run discovery tests only
go test -race -run TestSkillDiscovery ./skills/...

# Run matcher tests only
go test -race -run TestStemMatcher ./kernel/...

# Run integration tests only
go test -race -run TestSpawn_Stem ./kernel/...

# Run NFR42 performance test
go test -race -run TestStemMatcher_Performance_NFR42 ./kernel/...

# Run all tests in affected packages (verify no regression)
go test -race ./skills/... ./kernel/...

# Run with verbose output for debugging
go test -race -v -run TestStemMatcher_Match_CodeAnalysis ./kernel/...
```

---

## Red-Green-Refactor Workflow

### RED Phase (Complete)

**TEA Agent Responsibilities:**

- All 16 tests written and failing (compilation errors)
- Test fixtures and mock factories created
- Mock requirements documented
- Implementation checklist created

**Verification:**

- All tests fail to compile due to missing implementation
- Failure messages reference specific undefined types/methods:
  - `undefined: NewSkillDiscovery` (6 occurrences)
  - `undefined: NewStemMatcherFromFunc` (6 occurrences)
  - `SetStemMatcher undefined` (3 occurrences)
  - `SetSkillLoader undefined` (3 occurrences)
- Failures are due to missing implementation, not test bugs

---

### GREEN Phase (DEV Team - Next Steps)

**DEV Agent Responsibilities:**

1. **Start with `skills/discovery.go`** - make Discovery tests pass first
2. **Then `kernel/stem.go`** - make Matcher tests pass
3. **Then modify `kernel/kernel.go`** - add fields, setters, Spawn differentiation logic
4. **Then create `lib/agents/stem/`** - agent definition files
5. **Finally `cmd/rnix/main.go`** - wire up in daemon startup

**Key Principles:**

- One test file at a time (discovery -> matcher -> integration)
- Minimal implementation (don't over-engineer the keyword matching)
- Run tests frequently with `-race` flag
- Use implementation checklist as roadmap

---

### REFACTOR Phase (DEV Team - After All Tests Pass)

- Verify all 16 tests pass with `-race`
- Review keyword matching algorithm efficiency
- Ensure stem differentiation is concurrent-safe
- Verify no regression in existing test suite: `make test`

---

## Test Execution Evidence

### Initial Test Run (RED Phase Verification)

**Command:** `go test -race -run "TestSkillDiscovery|TestStemMatcher|TestSpawn_Stem" ./skills/... ./kernel/...`

**Results:**

```
# github.com/rnixai/rnix/skills [github.com/rnixai/rnix/skills.test]
skills/discovery_test.go:19:15: undefined: NewSkillDiscovery
skills/discovery_test.go:55:15: undefined: NewSkillDiscovery
skills/discovery_test.go:78:15: undefined: NewSkillDiscovery
skills/discovery_test.go:95:15: undefined: NewSkillDiscovery
skills/discovery_test.go:129:15: undefined: NewSkillDiscovery
skills/discovery_test.go:161:15: undefined: NewSkillDiscovery
FAIL    github.com/rnixai/rnix/skills [build failed]
# github.com/rnixai/rnix/kernel [github.com/rnixai/rnix/kernel.test]
kernel/stem_integration_test.go:49:13: undefined: NewStemMatcherFromFunc
kernel/stem_integration_test.go:50:4: k.SetStemMatcher undefined
kernel/stem_integration_test.go:53:4: k.SetSkillLoader undefined
...
FAIL    github.com/rnixai/rnix/kernel [build failed]
```

**Summary:**

- Total tests: 16
- Passing: 0 (expected)
- Failing: 16 (build failed - expected for Go TDD RED phase)
- Status: RED phase verified

---

## Notes

- Go TDD RED phase manifests as compilation errors rather than runtime test failures. This is the correct Go pattern -- tests reference types and functions that don't exist yet.
- The `mockSkillDiscovery` in `kernel/stem_test.go` uses a function-based approach (`discoverAll func()`) to avoid importing the `skills` package's `SkillDiscovery` type directly, matching the function injection pattern used throughout the kernel.
- Existing tests in `skills/` and `kernel/` packages will also fail to compile while the new test files reference undefined symbols. This is normal for Go TDD -- once implementation stubs are created, all tests will compile again.
- The `testCallbacks` struct in `stem_integration_test.go` provides event capture for verifying differentiation logs. A similar pattern may already exist in other test files -- implementation should check for reuse opportunity.

---

## Next Steps

1. **Share this checklist** with the DEV agent (manual handoff)
2. **Begin implementation** following the Implementation Checklist order:
   - `skills/discovery.go` first (unblocks discovery tests)
   - `kernel/stem.go` second (unblocks matcher tests)
   - `kernel/kernel.go` modifications third (unblocks integration tests)
3. **Work one test group at a time** (red -> green for each)
4. **When all 16 tests pass**, run `make test` for full regression
5. **When all tests pass**, refactor for quality

---

**Generated by BMad TEA Agent** - 2026-03-10
