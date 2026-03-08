# Story 16.2: 三种断言类型

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a 平台构建者,
I want 在 agtest 中使用推理断言、syscall 断言和质量断言三种验证方式,
So that 我可以从多个维度验证智能体的行为是否符合预期。

## Acceptance Criteria

1. **Given** 测试用例包含推理断言 `assert_output: contains: "代码示例"`
   **When** 智能体完成执行
   **Then** 系统检查输出是否包含指定内容，不满足则测试失败

2. **Given** 测试用例包含 syscall 断言 `assert_syscalls: includes: [Read, Write]`
   **When** 智能体完成执行
   **Then** 系统检查 syscall 序列是否包含指定调用，不满足则测试失败

3. **Given** 测试用例包含质量断言 `assert_quality: "输出必须包含可执行的修复建议"`
   **When** 智能体完成执行
   **Then** 系统通过轻量模型评估输出质量，不满足标准则测试失败并附评估原因

## Tasks / Subtasks

- [x] Task 1: 断言结果类型定义 (AC: #1, #2, #3)
  - [x] 1.1 在 `agtest/eval.go` 中定义 `AssertionResult` 结构体
  - [x] 1.2 定义 `TestResult` 结构体，供统一断言评估器使用
  - [x] 1.3 定义 `QualityResult` 结构体（质量评估返回）

- [x] Task 2: 输出断言评估器 (AC: #1)
  - [x] 2.1 在 `agtest/eval.go` 中实现 `EvalOutput(output string, assert *OutputAssert) []AssertionResult`
  - [x] 2.2 检查 `contains` 列表：每个字符串必须出现在 output 中，否则返回失败结果
  - [x] 2.3 检查 `not_contains` 列表：任一字符串出现在 output 中则返回失败结果
  - [x] 2.4 每个检查返回独立的 AssertionResult，包含清晰的 Message、Expected、Actual
  - [x] 2.5 若 assert 为 nil，返回空切片（无断言 = 通过）

- [x] Task 3: Syscall 断言评估器 (AC: #2)
  - [x] 3.1 在 `agtest/eval.go` 中实现 `EvalSyscalls(events []string, assert *SyscallAssert) []AssertionResult`
  - [x] 3.2 检查 `includes` 列表：每个 syscall 名称必须出现在 events 中
  - [x] 3.3 检查 `excludes` 列表：任一 syscall 名称不得出现在 events 中
  - [x] 3.4 events 为字符串切片，如 `["Spawn", "CtxWrite", "Read"]`，与 `types.SyscallEvent.Syscall` 字段值一致
  - [x] 3.5 若 assert 为 nil，返回空切片

- [x] Task 4: 质量断言评估器与 QualityJudge 接口 (AC: #3)
  - [x] 4.1 定义 `QualityJudge` 接口
  - [x] 4.2 实现 `MockQualityJudge` 用于单元测试：可配置返回 Passed/Reason，不调用真实 LLM
  - [x] 4.3 实现 `EvalQuality(output string, assert *QualityAssert, judge QualityJudge, ctx context.Context) []AssertionResult`
  - [x] 4.4 真实 LLM 集成在 Story 16-3 实现，本 story 仅提供接口和 mock

- [x] Task 5: 统一断言评估器 (AC: #1, #2, #3)
  - [x] 5.1 在 `agtest/eval.go` 中实现 `EvalAssertions(result *TestResult, assert *AssertConfig, judge QualityJudge, ctx context.Context) ([]AssertionResult, error)`
  - [x] 5.2 若 assert 为 nil，返回空切片和 nil 错误（无断言 = 通过）
  - [x] 5.3 依次调用 EvalOutput、EvalSyscalls、EvalQuality（仅当对应子配置非 nil 时）
  - [x] 5.4 收集所有 AssertionResult 并返回
  - [x] 5.5 若 judge 为 nil 且 assert.quality 非 nil，返回错误

- [x] Task 6: 断言配置校验扩展 (AC: #1, #2, #3)
  - [x] 6.1 在 `agtest/validator.go` 的 `Validate` 函数中扩展断言校验逻辑
  - [x] 6.2 若 `assert.output` 存在：`contains` 与 `not_contains` 不能同时为空
  - [x] 6.3 若 `assert.syscalls` 存在：`includes` 与 `excludes` 不能同时为空
  - [x] 6.4 若 `assert.quality` 存在：`criteria` 必须非空
  - [x] 6.5 使用 `lineMap.lookupTest` 获取 assert 相关字段行号，附加到 ValidationError

- [x] Task 7: 测试 (AC: #1, #2, #3)
  - [x] 7.1 `agtest/eval_test.go`：EvalOutput — contains 全部匹配时通过
  - [x] 7.2 `agtest/eval_test.go`：EvalOutput — contains 缺失时失败，Message 清晰
  - [x] 7.3 `agtest/eval_test.go`：EvalOutput — not_contains 命中时失败
  - [x] 7.4 `agtest/eval_test.go`：EvalOutput — contains 与 not_contains 混合场景
  - [x] 7.5 `agtest/eval_test.go`：EvalSyscalls — includes 全部存在时通过
  - [x] 7.6 `agtest/eval_test.go`：EvalSyscalls — includes 缺失时失败
  - [x] 7.7 `agtest/eval_test.go`：EvalSyscalls — excludes 命中时失败
  - [x] 7.8 `agtest/eval_test.go`：EvalSyscalls — 部分 includes 场景
  - [x] 7.9 `agtest/eval_test.go`：EvalQuality — 使用 MockQualityJudge，通过/失败场景
  - [x] 7.10 `agtest/eval_test.go`：EvalAssertions — 统一评估，assert 为 nil 时返回空
  - [x] 7.11 `agtest/eval_test.go`：EvalAssertions — 多种断言组合
  - [x] 7.12 `agtest/validator_test.go`：assert.output 同时空 contains/not_contains 时校验失败
  - [x] 7.13 `agtest/validator_test.go`：assert.syscalls 同时空 includes/excludes 时校验失败
  - [x] 7.14 `agtest/validator_test.go`：assert.quality 空 criteria 时校验失败
  - [x] 7.15 在 `agtest/testdata/` 中添加断言相关 fixture（`assert-output-only.yaml`、`assert-invalid-empty.yaml`）

## Dev Notes

### 架构决策

1. **断言评估与解析分离** — 解析在 parser.go，校验在 validator.go，评估在 eval.go。评估逻辑不修改 parser，仅扩展 validator 的断言配置校验。
2. **QualityJudge 接口抽象** — 质量断言依赖 LLM，通过接口注入便于测试。Story 16-3 实现真实 LLM 调用时实现该接口。
3. **TestResult 作为评估输入** — 测试执行引擎（16-3）负责收集 Output、Syscalls、Duration 并构造 TestResult，本 story 只实现评估逻辑。
4. **AssertionResult 统一结构** — 三种断言类型共用 AssertionResult，便于统一处理和报告。

### 关键设计：断言评估流程

```
TestResult (Output, Syscalls, Duration)
        |
        v
EvalAssertions(result, assert, judge, ctx)
        |
        +-- assert.output != nil  --> EvalOutput(result.Output, assert.Output)
        +-- assert.syscalls != nil --> EvalSyscalls(result.Syscalls, assert.Syscalls)
        +-- assert.quality != nil  --> EvalQuality(result.Output, assert.Quality, judge, ctx)
        |
        v
[]AssertionResult (Type, Passed, Message, Expected, Actual)
```

### 关键设计：Syscall 名称约定

- `events` 为 `[]string`，每个元素为 syscall 方法名，与 `types.SyscallEvent.Syscall` 一致
- 示例：`"Spawn"`, `"Open"`, `"Read"`, `"Write"`, `"CtxWrite"`, `"CtxAlloc"`, `"Kill"`, `"Mount"`, `"Unmount"` 等
- 大小写敏感，需与 kernel 实际 emit 的 Syscall 字段值完全匹配

### 关键设计：MockQualityJudge

```go
type MockQualityJudge struct {
    Result *QualityResult
    Err    error
}

func (m *MockQualityJudge) Judge(ctx context.Context, output, criteria string) (*QualityResult, error) {
    if m.Err != nil {
        return nil, m.Err
    }
    return m.Result, nil
}
```

### 关键复用点

1. **AssertConfig、OutputAssert、SyscallAssert、QualityAssert** — 已在 `agtest/types.go` 定义，无需修改
2. **Validate 函数** — 在 `agtest/validator.go` 中扩展，复用 buildLineMap、lookupTest
3. **ValidationError、ValidationErrors** — 复用现有错误类型
4. **testdata 模式** — 沿用 `agtest/testdata/` 的 fixture 组织方式

### 不要做的事情

- **不要**实现测试执行引擎（Story 16-3）
- **不要**实现真实 LLM 调用（使用接口 + mock）
- **不要**修改 parser.go 的核心解析逻辑（仅扩展 validator）
- **不要**修改 CLI（Story 16-3 添加执行功能）
- **不要**创建 IPC 扩展
- **不要**大幅修改 types.go — AssertConfig 等已定义，仅新增 AssertionResult、TestResult、QualityResult、QualityJudge

### 组合矩阵

| 本功能 | 交互对象 | 交互方式 | 需验证 |
|--------|----------|----------|--------|
| EvalOutput | OutputAssert | 读取 contains/not_contains | 是 |
| EvalSyscalls | SyscallAssert | 读取 includes/excludes；events 来自 types.SyscallEvent.Syscall | 是 |
| EvalQuality | QualityAssert + QualityJudge | 读取 criteria；调用 judge.Judge | 是 |
| EvalAssertions | TestResult, AssertConfig | 编排三个评估器 | 是 |
| Validate 扩展 | AssertConfig | 校验 assert 子配置非空约束 | 是 |

### Project Structure Notes

新建/修改文件：
- `agtest/eval.go` — AssertionResult、TestResult、QualityResult、QualityJudge、EvalOutput、EvalSyscalls、EvalQuality、EvalAssertions、MockQualityJudge
- `agtest/eval_test.go` — 评估器单元测试
- `agtest/types.go` — 可选：将 AssertionResult、TestResult、QualityResult 放在 types.go 或 eval.go（建议 eval.go 以保持 types.go 仅 YAML 结构）
- `agtest/validator.go` — 扩展 Validate，增加断言配置校验
- `agtest/validator_test.go` — 扩展断言校验测试
- `agtest/testdata/assert-output-only.yaml` — 仅 output 断言的 fixture
- `agtest/testdata/assert-invalid-empty.yaml` — 空断言配置的无效 fixture

### References

- [Source: agtest/types.go] — AssertConfig、OutputAssert、SyscallAssert、QualityAssert 定义
- [Source: agtest/validator.go] — Validate、buildLineMap、lookupTest
- [Source: internal/types/types.go:144-156] — SyscallEvent 结构，Syscall 字段
- [Source: _bmad-output/planning-artifacts/architecture/implementation-patterns-consistency-rules.md:114] — SyscallEvent 命名与接口方法名一致
- [Source: _bmad-output/implementation-artifacts/16-1-declarative-test-case-definition.md] — Story 16-1 完成内容
- [Source: _bmad-output/planning-artifacts/prd/non-functional-requirements.md:65] — NFR35: 框架开销 ≤ 500ms

### 技术栈

- Go 1.26 标准库
- `context` — QualityJudge 接口的 ctx 参数
- 无新增外部依赖

### 前置 Story 学习总结（16-1）

1. **三层分离模式** — 解析在 agtest、CLI 在 cmd/rnix；本 story 评估逻辑在 agtest/eval.go
2. **组合矩阵和不要做清单** — 继续使用
3. **goccy/go-yaml** — 不引入新依赖，validator 扩展复用 AST 行号
4. **AssertConfig 为指针** — 已就绪，本 story 实现评估逻辑

### 性能考量

- **NFR35**：单个测试用例框架开销（不含 LLM）≤ 500ms
- Output 断言和 Syscall 断言为纯内存操作（字符串包含检查、切片查找），开销可忽略
- 质量断言涉及 LLM 调用，不计入框架开销；本 story 的 mock 不调用 LLM

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6 (claude-opus-4-6)

### Debug Log References

- agtest 包: 50 个测试全部通过（eval 20 个 + parser 12 个 + validator 18 个），-race 检测通过
- 全项目 19 包测试通过，1 个预存 TTY 测试（TestRunTop_NoDaemon）不受影响

### Completion Notes List

- 新建 `agtest/eval.go`：AssertionResult、TestResult、QualityResult 类型，QualityJudge 接口，MockQualityJudge 测试替身
- 实现 EvalOutput：逐项检查 contains/not_contains，每项返回独立 AssertionResult
- 实现 EvalSyscalls：使用 map 快速查找 includes/excludes，大小写敏感匹配
- 实现 EvalQuality：通过 QualityJudge 接口调用，支持 mock 和真实 LLM
- 实现 EvalAssertions：编排三种断言，nil assert 返回空，nil judge + quality 断言返回错误
- 扩展 validator.go：validateAssertConfig 校验 assert 子配置非空约束
- 新增 2 个 testdata fixture 文件
- 无新增外部依赖

### File List

新建文件:
- `agtest/eval.go` — AssertionResult、TestResult、QualityResult、QualityJudge、MockQualityJudge、EvalOutput、EvalSyscalls、EvalQuality、EvalAssertions
- `agtest/eval_test.go` — 20 个评估器单元测试
- `agtest/testdata/assert-output-only.yaml` — 仅 output 断言的测试 fixture
- `agtest/testdata/assert-invalid-empty.yaml` — 空断言配置的无效 fixture

修改文件:
- `agtest/validator.go` — 扩展 Validate 函数，新增 validateAssertConfig
- `agtest/validator_test.go` — 新增 5 个断言校验测试
