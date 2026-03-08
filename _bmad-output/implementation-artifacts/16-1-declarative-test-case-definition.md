# Story 16.1: 声明式测试用例定义

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a 平台构建者,
I want 通过 YAML 文件声明式定义智能体行为测试用例,
So that 我可以用版本化的测试规范持续验证智能体行为。

## Acceptance Criteria

1. **Given** 用户创建 `test.yaml` 文件
   **When** 文件包含 `intent`、`agent` 配置和预期行为断言
   **Then** 系统可以解析并加载该测试用例

2. **Given** 测试用例定义
   **When** 用例中缺少必填字段（`intent` / `agent`）
   **Then** 系统报告具体的校验错误和行号

## Tasks / Subtasks

- [x] Task 1: agtest 包创建与核心类型定义 (AC: #1)
  - [x] 1.1 创建 `agtest/` 顶级包目录
  - [x] 1.2 在 `agtest/types.go` 中定义 `TestCaseSpec` 结构体：
    ```go
    type TestCaseSpec struct {
        Version string         `yaml:"version"`
        Name    string         `yaml:"name"`
        Intent  string         `yaml:"intent"`
        Agent   AgentConfig    `yaml:"agent"`
        Timeout int            `yaml:"timeout,omitempty"`   // milliseconds, 0 = default
        Assert  *AssertConfig  `yaml:"assert,omitempty"`    // Story 16-2 扩展
    }
    ```
  - [x] 1.3 定义 `AgentConfig` 结构体：
    ```go
    type AgentConfig struct {
        Name          string   `yaml:"name"`
        Model         string   `yaml:"model,omitempty"`
        Skills        []string `yaml:"skills,omitempty"`
        ContextBudget int      `yaml:"context_budget,omitempty"`
    }
    ```
  - [x] 1.4 定义 `AssertConfig` 占位结构体（Story 16-2 填充）：
    ```go
    type AssertConfig struct {
        Output   *OutputAssert   `yaml:"output,omitempty"`
        Syscalls *SyscallAssert  `yaml:"syscalls,omitempty"`
        Quality  *QualityAssert  `yaml:"quality,omitempty"`
    }
    type OutputAssert struct {
        Contains    []string `yaml:"contains,omitempty"`
        NotContains []string `yaml:"not_contains,omitempty"`
    }
    type SyscallAssert struct {
        Includes []string `yaml:"includes,omitempty"`
        Excludes []string `yaml:"excludes,omitempty"`
    }
    type QualityAssert struct {
        Criteria string `yaml:"criteria,omitempty"`
    }
    ```
  - [x] 1.5 定义 `TestSuiteSpec` 用于多用例文件：
    ```go
    type TestSuiteSpec struct {
        Version string         `yaml:"version"`
        Name    string         `yaml:"name,omitempty"`
        Tests   []TestCaseSpec `yaml:"tests"`
    }
    ```

- [x] Task 2: YAML 解析器实现 (AC: #1, #2)
  - [x] 2.1 在 `agtest/parser.go` 中实现 `ParseFile(path string) (*TestSuiteSpec, error)` — 读取文件并调用 `ParseBytes`
  - [x] 2.2 实现 `ParseBytes(data []byte) (*TestSuiteSpec, error)` — YAML 反序列化
  - [x] 2.3 实现格式检测：判断 YAML 是单个 TestCaseSpec 还是 TestSuiteSpec（通过检查顶层是否有 `tests` 数组字段）
  - [x] 2.4 单个 TestCaseSpec 自动包装为 TestSuiteSpec（`Tests` 长度 1）

- [x] Task 3: 带行号的校验系统 (AC: #2)
  - [x] 3.1 在 `agtest/validator.go` 中定义 `ValidationError` 类型：
    ```go
    type ValidationError struct {
        Field   string // 字段路径，如 "intent" 或 "agent.name"
        Message string // 错误描述
        Line    int    // YAML 行号（0 = 未知）
    }
    func (e *ValidationError) Error() string
    ```
  - [x] 3.2 定义 `ValidationErrors` 聚合类型（`[]ValidationError`），实现 `error` 接口
  - [x] 3.3 实现 `Validate(spec *TestSuiteSpec) error` 函数：
    - 检查 `version` 必须为 `"1.0"`
    - 对每个 TestCaseSpec 检查 `intent` 非空
    - 对每个 TestCaseSpec 检查 `agent.name` 非空
    - 收集所有校验错误后一次性返回
  - [x] 3.4 使用 `goccy/go-yaml` 的 `yaml.PathString` 或 AST 方式获取字段行号：
    - 解析 YAML 为 AST（`yaml.Parse(data)`）
    - 定位缺失字段的位置，提取行号
    - 行号信息附加到 `ValidationError.Line`

- [x] Task 4: ParseFile 集成校验流程 (AC: #1, #2)
  - [x] 4.1 在 `ParseBytes` 中：先反序列化，再调用 `Validate`，最后返回
  - [x] 4.2 `ParseFile` 支持目录遍历：如果路径是目录，扫描所有 `*.yaml` 文件并合并为一个 `TestSuiteSpec`
  - [x] 4.3 `ParseDir(dir string) (*TestSuiteSpec, error)` 扫描目录下所有 `.yaml` 文件

- [x] Task 5: CLI 命令注册（桩） (AC: #1)
  - [x] 5.1 在 `cmd/rnix/agtest.go` 中注册 `rnix agtest` 子命令
  - [x] 5.2 实现 `--dry-run` flag：仅解析和校验，不执行（展示解析结果）
  - [x] 5.3 接受文件或目录参数：`rnix agtest test.yaml` 或 `rnix agtest tests/`
  - [x] 5.4 干运行模式输出解析结果摘要（测试用例数量、名称列表）
  - [x] 5.5 校验错误时输出友好的错误信息（含行号）

- [x] Task 6: 测试 (AC: #1, #2)
  - [x] 6.1 `agtest/parser_test.go`：解析有效的单个测试用例 YAML
  - [x] 6.2 `agtest/parser_test.go`：解析有效的测试套件（多用例）YAML
  - [x] 6.3 `agtest/parser_test.go`：解析目录下多个 YAML 文件
  - [x] 6.4 `agtest/validator_test.go`：缺少 intent 时报告校验错误
  - [x] 6.5 `agtest/validator_test.go`：缺少 agent.name 时报告校验错误
  - [x] 6.6 `agtest/validator_test.go`：缺少 version 或 version 不为 "1.0" 时报告校验错误
  - [x] 6.7 `agtest/validator_test.go`：校验错误包含正确的行号
  - [x] 6.8 `agtest/validator_test.go`：多个校验错误一次性报告
  - [x] 6.9 `agtest/parser_test.go`：无效 YAML 语法报告解析错误
  - [x] 6.10 `agtest/parser_test.go`：空文件返回错误
  - [x] 6.11 `cmd/rnix/agtest_test.go`：agtest 命令注册和 --dry-run 功能测试

## Dev Notes

### 架构决策

本 story 是 Epic 16（推理回归测试 / agtest）的第一层，建立测试用例声明式定义的基础设施。核心设计原则：

1. **复用 Compose YAML 解析模式** — `compose/parser.go` 已验证 `goccy/go-yaml` + `ParseFile/ParseBytes/validate` 三层模式。agtest 采用完全相同的模式：`agtest/parser.go` + `agtest/validator.go` + `agtest/types.go`
2. **行号错误报告** — AC#2 要求报告行号，`goccy/go-yaml` 支持通过 `yaml.Path` 和 AST 获取节点位置信息，无需引入新依赖
3. **Schema 向前兼容** — `TestCaseSpec.Assert` 为 `*AssertConfig`（指针，可为 nil），Story 16-2 填充断言类型时不需要修改本 story 的解析逻辑
4. **单文件 vs 多文件** — 同时支持单个测试用例文件（无 `tests:` 顶层键）和测试套件文件（有 `tests:` 数组），单用例自动包装为 suite
5. **本 story 不执行测试** — 只负责"解析+校验"，测试执行在 Story 16-3 实现

### 关键设计：测试用例 YAML Schema

```yaml
# 单个测试用例格式
version: "1.0"
name: "test-agent-greeting"
intent: "向用户打招呼"
agent:
  name: "greeter"
  model: "claude-sonnet-4-20250514"    # 可选
  skills: ["politeness"]              # 可选
  context_budget: 4096                # 可选
timeout: 30000                        # 可选，毫秒
assert:                               # 可选，Story 16-2 扩展
  output:
    contains: ["你好"]
  syscalls:
    includes: ["CtxWrite"]
  quality:
    criteria: "输出必须包含问候语"
```

```yaml
# 测试套件格式
version: "1.0"
name: "greeter-suite"
tests:
  - name: "basic-greeting"
    intent: "向用户打招呼"
    agent:
      name: "greeter"
  - name: "farewell"
    intent: "向用户告别"
    agent:
      name: "greeter"
```

### 关键设计：格式检测

`ParseBytes` 先尝试检测 YAML 顶层是否包含 `tests` 键：
- 有 `tests` 键 → 解析为 `TestSuiteSpec`
- 无 `tests` 键 → 解析为 `TestCaseSpec`，包装为 `TestSuiteSpec{Tests: []TestCaseSpec{spec}}`

实现方式：先 unmarshal 为 `map[string]any`，检查 `tests` 键是否存在，然后 unmarshal 为正确的目标类型。

### 关键设计：行号获取

`goccy/go-yaml` 库提供了 AST 节点位置信息。通过以下方式获取字段行号：

```go
// 使用 yaml.PathString 获取节点
path, _ := yaml.PathString("$.intent")
node, err := path.ReadNode(file)
if node != nil {
    line = node.GetToken().Position.Line
}
```

校验器先解析 YAML AST，缓存字段路径到行号的映射，然后在校验过程中查询行号。

### 关键复用点

1. **YAML 解析模式** — 复用 `compose/parser.go` 的 ParseFile/ParseBytes/validate 三层模式
2. **YAML 库** — 复用项目已有的 `github.com/goccy/go-yaml`（不引入新依赖）
3. **Agent 配置字段** — `AgentConfig` 的字段名与 `agents.AgentManifest` 和 `compose.AgentSpec` 保持一致（name、model、skills、context_budget）
4. **CLI 命令注册模式** — 复用 `cmd/rnix/` 中已有的 Cobra 命令注册模式（参考 `trace.go`、`ctx_profile.go`）
5. **testdata 目录模式** — 复用 `compose/testdata/`、`agents/testdata/` 的 fixture 组织方式

### 不要做的事情

- **不要**实现断言逻辑（Story 16-2 的范围）
- **不要**实现测试执行引擎（Story 16-3 的范围）
- **不要**实现 LLM-as-judge 质量评估（Story 16-2 的范围）
- **不要**引入新的 YAML 解析库（项目已有 `goccy/go-yaml`）
- **不要**实现 IPC 协议扩展（agtest 在 Story 16-3 中通过 CLI 直接调用 daemon）
- **不要**创建 `.rnix/tests/` 目录结构（Story 16-3 实现结果缓存时创建）
- **不要**在 ParseFile 中执行测试 — 保持"解析+校验"的纯函数特性
- **不要**修改 compose/parser.go — agtest 解析器独立实现
- **不要**在 CLI 中注册 IPC 客户端调用 — Story 16-1 的 CLI 只做干运行解析

### 组合矩阵

| 本功能 | 交互对象 | 交互方式 | 需验证 |
|--------|----------|----------|--------|
| TestCaseSpec.Agent | agents.AgentManifest | 字段名对齐：name/model/skills/context_budget，但不依赖 agents 包 | 否 |
| YAML 解析 | compose/parser.go | 模式复用：相同的 ParseFile/ParseBytes/validate 三层结构，无代码依赖 | 否 |
| CLI agtest 命令 | cmd/rnix/main.go | 集成：在 init() 中注册 agtestCmd | 是 |
| agtest YAML 文件 | 用户项目 | 独立：用户在任意位置创建 .yaml 文件，agtest 从路径参数读取 | 是 |
| TestCaseSpec.Assert | Story 16-2 断言系统 | 预留：AssertConfig 为指针类型，nil 时无断言逻辑 | 否 |

### Project Structure Notes

新建文件：
- `agtest/types.go` — TestCaseSpec、AgentConfig、AssertConfig、TestSuiteSpec 类型定义
- `agtest/parser.go` — ParseFile、ParseBytes、ParseDir、格式检测
- `agtest/validator.go` — ValidationError、ValidationErrors、Validate 校验函数
- `agtest/parser_test.go` — 解析测试
- `agtest/validator_test.go` — 校验测试
- `agtest/testdata/` — 测试 fixture 目录
- `agtest/testdata/valid-single.yaml` — 有效的单个测试用例
- `agtest/testdata/valid-suite.yaml` — 有效的测试套件
- `agtest/testdata/missing-intent.yaml` — 缺少 intent
- `agtest/testdata/missing-agent.yaml` — 缺少 agent.name
- `agtest/testdata/invalid-syntax.yaml` — 无效 YAML 语法
- `cmd/rnix/agtest.go` — agtest CLI 命令
- `cmd/rnix/agtest_test.go` — agtest CLI 测试

### References

- [Source: compose/parser.go] — YAML 解析三层模式参考
- [Source: compose/types.go] — ComposeSpec/AgentSpec 字段命名参考
- [Source: agents/loader.go] — AgentManifest YAML 解析参考
- [Source: agents/types.go] — AgentManifest 字段定义参考
- [Source: cmd/rnix/trace.go] — CLI 命令注册模式参考
- [Source: _bmad-output/planning-artifacts/epics/epic-16-推理回归测试-reasoning-regression-testing-agtest.md] — Epic 16 原始定义
- [Source: _bmad-output/planning-artifacts/prd/functional-requirements.md:160-162] — FR87-89 agtest 功能需求
- [Source: _bmad-output/planning-artifacts/prd/non-functional-requirements.md:65] — NFR35: 框架开销 ≤ 500ms
- [Source: _bmad-output/planning-artifacts/architecture/implementation-patterns-consistency-rules.md:245] — .rnix/tests/ 路径约定

### 技术栈

- Go 1.26 — 标准库满足大部分需求
- `github.com/goccy/go-yaml` — YAML 解析（项目已有依赖）
- `github.com/spf13/cobra` — CLI 框架（项目已有依赖）
- `os`、`path/filepath` — 文件和目录操作
- 无新增外部依赖

### 前置 story 学习总结

**来自 Epic 15 回顾：**
1. "分析逻辑在 debug 包、CLI 在 cmd/rnix、IPC 在 ipc 包"三层分离模式 — agtest 采用类似模式："解析逻辑在 agtest 包、CLI 在 cmd/rnix"
2. 组合矩阵和"不要做"清单继续发挥价值 — 本 story 包含 9 项负面约束
3. IPC 扩展标准四步流程 — 本 story 不涉及 IPC，但 Story 16-3 可能需要

**来自 Epic 15 准备度评估：**
1. YAML 测试用例格式需要在 create-story 中完成设计 — 已在 Dev Notes 中详细定义
2. LLM-as-judge 评估模式 — 延迟到 Story 16-2 设计

**来自 Git 分析：**
- 最近 commit 模式：`feat: complete story X-Y - description`
- 新顶级包创建模式参考 `compose/`、`agents/`、`skills/` 的结构

### 性能考量

- **NFR35 约束**：单个测试用例框架开销（不含 LLM 调用）≤ 500ms
- YAML 解析（goccy/go-yaml）对于 < 1KB 的测试用例文件，解析时间在 1ms 以内
- 校验是纯内存操作，开销可忽略
- 本 story 只涉及解析和校验，不涉及执行，NFR35 的性能约束在 Story 16-3 中验证

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6 (claude-opus-4-6)

### Debug Log References

- agtest 包: 25 个测试全部通过（parser 12 个 + validator 13 个）
- cmd/rnix 包: agtest CLI 3 个测试全部通过
- 全项目 19 包测试通过（-race 检测），1 个预存 TTY 测试（TestRunTop_NoDaemon）不受影响

### Completion Notes List

- 新建 `agtest/` 顶级包，遵循 compose/ 的 ParseFile/ParseBytes/validate 三层模式
- TestCaseSpec 支持单文件和 TestSuiteSpec 多文件两种格式，通过 isSuiteFormat 自动检测
- ValidationError 包含 Field/Message/Line 三个字段，Line 通过 goccy/go-yaml AST parser 提取
- buildLineMap 缓存 YAML AST 节点行号，支持顶层字段和 tests 数组内的嵌套字段
- AssertConfig 为指针类型（nil 时无断言），为 Story 16-2 预留扩展点
- CLI `rnix agtest` 支持 --dry-run 模式，仅解析校验不执行
- ParseDir 扫描目录下 *.yaml 文件并合并，忽略非 YAML 文件
- 无新增外部依赖，复用项目已有的 goccy/go-yaml 和 spf13/cobra

### File List

新建文件:
- `agtest/types.go` — TestCaseSpec、AgentConfig、AssertConfig、TestSuiteSpec 类型定义
- `agtest/parser.go` — ParseFile、ParseBytes、ParseDir、isSuiteFormat 格式检测
- `agtest/validator.go` — ValidationError、ValidationErrors、Validate、buildLineMap 行号提取
- `agtest/parser_test.go` — 12 个解析测试
- `agtest/validator_test.go` — 13 个校验测试
- `agtest/testdata/valid-single.yaml` — 有效单个测试用例
- `agtest/testdata/valid-full.yaml` — 包含所有可选字段的测试用例
- `agtest/testdata/valid-suite.yaml` — 有效测试套件（3 个用例）
- `agtest/testdata/missing-intent.yaml` — 缺少 intent
- `agtest/testdata/missing-agent.yaml` — 缺少 agent.name
- `agtest/testdata/missing-version.yaml` — 缺少 version
- `agtest/testdata/invalid-version.yaml` — 无效 version
- `agtest/testdata/invalid-syntax.yaml` — 无效 YAML 语法
- `agtest/testdata/multi-errors.yaml` — 多个校验错误
- `cmd/rnix/agtest.go` — agtest CLI 命令注册 + --dry-run
- `cmd/rnix/agtest_test.go` — 3 个 CLI 测试
