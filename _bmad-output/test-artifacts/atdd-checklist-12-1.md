---
stepsCompleted: ['step-01', 'step-02', 'step-03', 'step-04', 'step-05', 'step-06']
lastStep: 'step-06'
lastSaved: '2026-03-03'
workflowType: 'testarch-atdd'
inputDocuments:
  - '_bmad-output/implementation-artifacts/12-1-tutorial-documentation.md'
  - 'docs/concepts.md'
  - 'docs/quick-start.md'
  - 'docs/reference.md'
---

# ATDD Checklist - Epic 12, Story 1: 教程文档（Tutorial Documentation）

**Date:** 2026-03-03
**Author:** Decker
**Primary Test Level:** Unit（文档验证测试）

---

## Story Summary

为 Crux 创建三篇面向开发者的实战教程：编写第一个 Skill、调试第一个 bug、组合多智能体工作流。教程必须包含完整可运行示例，所有 CLI 命令和代码示例必须与实际 Crux 实现精确一致。

**As a** 开发者
**I want** 阅读教程文档学会编写 Skill、调试 bug 和组合多智能体工作流
**So that** 我可以在 Crux 上构建自己的应用

---

## Acceptance Criteria

1. **AC1: 编写第一个 Skill 教程** — 包含从创建 SKILL.md 到 Agent 引用到 spawn 执行的完整流程，包含完整可运行示例
2. **AC2: 调试第一个 bug 教程** — 包含故意引入 bug → astrace 定位 → 修复 → 验证的完整流程，包含完整可运行示例
3. **AC3: 组合多智能体工作流教程** — 包含编写 crux-compose.yaml → compose up → crux top 监控 → 查看结果的完整流程，包含完整可运行示例

---

## Test Strategy

### Documentation Story 特殊说明

本 Story 为纯文档 Story（无 Go 代码实现变更），测试策略与功能 Story 不同：

1. **文件存在性测试** — 验证所有教程文件已创建且路径正确
2. **内容完整性测试** — 验证每篇教程包含必需的章节和内容
3. **技术准确性测试** — 验证 CLI 命令、VFS 路径、YAML 示例与代码实现一致
4. **交叉引用测试** — 验证教程间链接和指向参考文档的链接正确
5. **回归测试** — 验证现有文档未被破坏

### 测试框架

使用 Go 标准 `testing` 包 + `testify`，测试文件放在 `docs/docs_test.go`（Go 惯例：同目录测试）。

---

## Failing Tests Created (RED Phase)

### Unit Tests (15 tests)

**File:** `docs/docs_test.go`

#### 文件存在性测试（4 tests）

- ✅ **Test:** `TestTutorialFiles_Exist`
  - **Status:** RED - 教程文件不存在
  - **Verifies:** `docs/tutorials/README.md`、`docs/tutorials/writing-first-skill.md`、`docs/tutorials/debugging-first-bug.md`、`docs/tutorials/composing-multi-agent-workflow.md` 四个文件均存在

#### AC1: 编写第一个 Skill 教程（4 tests）

- ✅ **Test:** `TestWritingFirstSkill_HasRequiredSections`
  - **Status:** RED - 教程文件不存在
  - **Verifies:** 教程包含"前置条件"、"创建 SKILL.md"、"创建 Agent"、"运行"、"常见问题"等章节

- ✅ **Test:** `TestWritingFirstSkill_HasSkillMDExample`
  - **Status:** RED - 教程文件不存在
  - **Verifies:** 教程包含完整的 SKILL.md 示例（含 frontmatter: name、version、description、tags、tools）

- ✅ **Test:** `TestWritingFirstSkill_HasAgentYamlExample`
  - **Status:** RED - 教程文件不存在
  - **Verifies:** 教程包含完整的 agent.yaml 示例（含 name、description、model、skills）

- ✅ **Test:** `TestWritingFirstSkill_HasCLIExamples`
  - **Status:** RED - 教程文件不存在
  - **Verifies:** 教程包含 `crux -i` 命令示例和 `crux ps`、`crux astrace` 的使用

#### AC2: 调试第一个 bug 教程（3 tests）

- ✅ **Test:** `TestDebuggingFirstBug_HasRequiredSections`
  - **Status:** RED - 教程文件不存在
  - **Verifies:** 教程包含"准备有 bug 的 Skill"、"使用 astrace 定位"、"修复并验证"等章节

- ✅ **Test:** `TestDebuggingFirstBug_HasAstraceOutput`
  - **Status:** RED - 教程文件不存在
  - **Verifies:** 教程包含 astrace 输出示例，含 SyscallEvent 字段（Syscall、PID、Device、ErrCode）

- ✅ **Test:** `TestDebuggingFirstBug_ShowsFixWorkflow`
  - **Status:** RED - 教程文件不存在
  - **Verifies:** 教程展示 bug 引入 → 定位 → 修复 → 验证的完整流程（含修复前后对比）

#### AC3: 组合多智能体工作流教程（3 tests）

- ✅ **Test:** `TestComposingWorkflow_HasRequiredSections`
  - **Status:** RED - 教程文件不存在
  - **Verifies:** 教程包含"设计工作流"、"编写 compose YAML"、"运行 compose up"、"crux top 监控"、"查看结果"等章节

- ✅ **Test:** `TestComposingWorkflow_HasComposeYamlExample`
  - **Status:** RED - 教程文件不存在
  - **Verifies:** 教程包含完整的 crux-compose.yaml 示例（含 services、intent、agent、depends_on）

- ✅ **Test:** `TestComposingWorkflow_HasExtendedScenarios`
  - **Status:** RED - 教程文件不存在
  - **Verifies:** 教程包含扩展场景：管道语法、变量传递、条件分支

#### 交叉引用测试（1 test）

- ✅ **Test:** `TestTutorials_CrossReferences`
  - **Status:** RED - 教程文件不存在
  - **Verifies:** 每篇教程包含指向其他教程的链接，且都引用了 concepts.md 和 reference.md

---

## Data Factories Created

不适用（文档 Story 无需数据工厂）

---

## Fixtures Created

### 文档路径 Fixtures

**用法：** 测试文件中定义常量引用所有文档路径

```go
const (
    tutorialsDir       = "tutorials"
    readmePath         = "tutorials/README.md"
    skillTutorialPath  = "tutorials/writing-first-skill.md"
    debugTutorialPath  = "tutorials/debugging-first-bug.md"
    composeTutorialPath = "tutorials/composing-multi-agent-workflow.md"
)
```

---

## Mock Requirements

不适用（文档 Story 无外部服务依赖）

---

## Implementation Checklist

### Test: TestTutorialFiles_Exist

**File:** `docs/docs_test.go`

**Tasks to make this test pass:**

- [ ] 创建 `docs/tutorials/` 目录
- [ ] 创建 `docs/tutorials/README.md`（教程导航页）
- [ ] 创建 `docs/tutorials/writing-first-skill.md`（骨架）
- [ ] 创建 `docs/tutorials/debugging-first-bug.md`（骨架）
- [ ] 创建 `docs/tutorials/composing-multi-agent-workflow.md`（骨架）
- [ ] Run test: `go test ./docs/ -run TestTutorialFiles_Exist -v`
- [ ] ✅ Test passes (green phase)

---

### Test: TestWritingFirstSkill_HasRequiredSections

**File:** `docs/docs_test.go`

**Tasks to make this test pass:**

- [ ] 编写"编写第一个 Skill"教程——前置条件章节
- [ ] 编写"创建 SKILL.md"步骤
- [ ] 编写"创建 Agent"步骤
- [ ] 编写"运行 Skill"步骤
- [ ] 编写"常见问题"章节
- [ ] Run test: `go test ./docs/ -run TestWritingFirstSkill_HasRequiredSections -v`
- [ ] ✅ Test passes (green phase)

---

### Test: TestWritingFirstSkill_HasSkillMDExample

**File:** `docs/docs_test.go`

**Tasks to make this test pass:**

- [ ] 在教程中添加完整的 SKILL.md frontmatter 示例（name、version、description、tags、tools）
- [ ] 在教程中添加 SKILL.md body 示例（指令正文）
- [ ] 解释 allowed-tools 与 VFS 路径的映射关系
- [ ] Run test: `go test ./docs/ -run TestWritingFirstSkill_HasSkillMDExample -v`
- [ ] ✅ Test passes (green phase)

---

### Test: TestWritingFirstSkill_HasAgentYamlExample

**File:** `docs/docs_test.go`

**Tasks to make this test pass:**

- [ ] 在教程中添加完整的 agent.yaml 示例
- [ ] 示例包含 name、description、model、skills 字段
- [ ] 添加 instructions.md 示例
- [ ] Run test: `go test ./docs/ -run TestWritingFirstSkill_HasAgentYamlExample -v`
- [ ] ✅ Test passes (green phase)

---

### Test: TestWritingFirstSkill_HasCLIExamples

**File:** `docs/docs_test.go`

**Tasks to make this test pass:**

- [ ] 在教程中添加 `crux -i` 命令使用示例
- [ ] 添加 `crux ps` 查看进程状态示例
- [ ] 添加 `crux astrace` 查看 syscall 追踪示例
- [ ] Run test: `go test ./docs/ -run TestWritingFirstSkill_HasCLIExamples -v`
- [ ] ✅ Test passes (green phase)

---

### Test: TestDebuggingFirstBug_HasRequiredSections

**File:** `docs/docs_test.go`

**Tasks to make this test pass:**

- [ ] 编写"调试第一个 bug"教程——前置条件
- [ ] 编写"准备有 bug 的 Skill"步骤
- [ ] 编写"使用 astrace 定位"步骤
- [ ] 编写"修复并验证"步骤
- [ ] Run test: `go test ./docs/ -run TestDebuggingFirstBug_HasRequiredSections -v`
- [ ] ✅ Test passes (green phase)

---

### Test: TestDebuggingFirstBug_HasAstraceOutput

**File:** `docs/docs_test.go`

**Tasks to make this test pass:**

- [ ] 在教程中添加 astrace 输出示例
- [ ] 示例包含 SyscallEvent 关键字段：Syscall 名称、PID、Device 路径、ErrCode
- [ ] 高亮说明错误行的含义
- [ ] Run test: `go test ./docs/ -run TestDebuggingFirstBug_HasAstraceOutput -v`
- [ ] ✅ Test passes (green phase)

---

### Test: TestDebuggingFirstBug_ShowsFixWorkflow

**File:** `docs/docs_test.go`

**Tasks to make this test pass:**

- [ ] 展示引入 bug 的代码（缺失 `/dev/fs` 权限的 SKILL.md）
- [ ] 展示修复后的代码（添加 `/dev/fs` 权限）
- [ ] 展示修复前后 astrace 输出对比
- [ ] Run test: `go test ./docs/ -run TestDebuggingFirstBug_ShowsFixWorkflow -v`
- [ ] ✅ Test passes (green phase)

---

### Test: TestComposingWorkflow_HasRequiredSections

**File:** `docs/docs_test.go`

**Tasks to make this test pass:**

- [ ] 编写"组合多智能体工作流"教程——前置条件
- [ ] 编写"设计工作流"步骤
- [ ] 编写"编写 crux-compose.yaml"步骤
- [ ] 编写"运行 compose up"步骤
- [ ] 编写"crux top 监控"步骤
- [ ] 编写"查看结果"步骤
- [ ] Run test: `go test ./docs/ -run TestComposingWorkflow_HasRequiredSections -v`
- [ ] ✅ Test passes (green phase)

---

### Test: TestComposingWorkflow_HasComposeYamlExample

**File:** `docs/docs_test.go`

**Tasks to make this test pass:**

- [ ] 在教程中添加完整的 crux-compose.yaml 示例
- [ ] 示例包含 services、intent、agent、depends_on 字段
- [ ] 解释 DAG 调度引擎的依赖解析
- [ ] Run test: `go test ./docs/ -run TestComposingWorkflow_HasComposeYamlExample -v`
- [ ] ✅ Test passes (green phase)

---

### Test: TestComposingWorkflow_HasExtendedScenarios

**File:** `docs/docs_test.go`

**Tasks to make this test pass:**

- [ ] 添加管道语法替代方案示例
- [ ] 添加变量与环境传递示例
- [ ] 添加 if/else 条件分支示例
- [ ] Run test: `go test ./docs/ -run TestComposingWorkflow_HasExtendedScenarios -v`
- [ ] ✅ Test passes (green phase)

---

### Test: TestTutorials_CrossReferences

**File:** `docs/docs_test.go`

**Tasks to make this test pass:**

- [ ] 在每篇教程末尾添加"下一步"链接指向其他教程
- [ ] 在每篇教程中添加指向 concepts.md 和 reference.md 的相关链接
- [ ] 在 quick-start.md 末尾添加教程导航入口
- [ ] Run test: `go test ./docs/ -run TestTutorials_CrossReferences -v`
- [ ] ✅ Test passes (green phase)

---

## Running Tests

```bash
# Run all docs tests for this story
go test ./docs/ -v -run "TestTutorial|TestWritingFirstSkill|TestDebuggingFirstBug|TestComposingWorkflow"

# Run specific test
go test ./docs/ -v -run TestTutorialFiles_Exist

# Run with race detection
go test ./docs/ -v -race -run "TestTutorial|TestWritingFirstSkill|TestDebuggingFirstBug|TestComposingWorkflow"

# Run all tests to check for regressions
make test
```

---

## Red-Green-Refactor Workflow

### RED Phase (Complete) ✅

**TEA Agent Responsibilities:**

- ✅ All 15 tests written and failing (docs/docs_test.go)
- ✅ Test file structure created
- ✅ Implementation checklist created
- ✅ Documentation validation strategy defined

**Verification:**

- All tests run and fail as expected (files don't exist yet)
- Failure messages are clear and actionable
- Tests fail due to missing documentation, not test bugs

---

### GREEN Phase (DEV Team - Next Steps)

**DEV Agent Responsibilities:**

1. **创建目录和文件骨架** — 通过 TestTutorialFiles_Exist
2. **编写第一个 Skill 教程** — 逐步通过 4 个测试
3. **编写调试教程** — 逐步通过 3 个测试
4. **编写组合工作流教程** — 逐步通过 3 个测试
5. **添加交叉引用** — 通过最后 1 个测试
6. **运行全部测试确认 GREEN**

---

### REFACTOR Phase (DEV Team - After All Tests Pass)

1. 审读所有教程确保行文流畅连贯
2. 校验所有 CLI 命令、VFS 路径、YAML 语法与代码实现一致
3. 确保术语翻译一致（与 concepts.md、reference.md 统一）
4. 运行 `make test` 确认无回归

---

## Next Steps

1. **创建测试文件** `docs/docs_test.go`（TEA Agent 完成 RED phase）
2. **运行测试确认全部 RED**: `go test ./docs/ -v`
3. **开始实现** — 按 Implementation Checklist 顺序逐个通过测试
4. **全部 GREEN 后** — 校验与重构
5. **运行 `make test`** — 确认无回归

---

## Notes

- 本 Story 为文档类 Story，"实现"即编写 Markdown 文件，"测试"即验证文档内容完整性和准确性
- 测试通过读取文件内容并检查关键字符串/章节标题来验证
- CLI 命令格式以 `cmd/crux/main.go` 中的 Cobra 命令定义为准
- VFS 路径以 `vfs/` 包中的路径常量为准
- YAML 格式以 `compose/parser.go` 中的结构体定义为准
- 所有教程使用简体中文

---

**Generated by BMad TEA Agent** - 2026-03-03
