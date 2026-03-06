# Epic 16: 推理回归测试（agtest）

用户可以编写声明式测试用例验证智能体行为，批量运行回归测试确保修改不破坏已有功能。

## Story 16.1: 声明式测试用例定义

As a 平台构建者,
I want 通过 YAML 文件声明式定义智能体行为测试用例,
So that 我可以用版本化的测试规范持续验证智能体行为。

**Acceptance Criteria:**

**Given** 用户创建 `test.yaml` 文件
**When** 文件包含 intent、agent 配置和预期行为断言
**Then** 系统可以解析并加载该测试用例

**Given** 测试用例定义
**When** 用例中缺少必填字段（intent / agent）
**Then** 系统报告具体的校验错误和行号

## Story 16.2: 三种断言类型

As a 平台构建者,
I want 在 agtest 中使用推理断言、syscall 断言和质量断言三种验证方式,
So that 我可以从多个维度验证智能体的行为是否符合预期。

**Acceptance Criteria:**

**Given** 测试用例包含推理断言 `assert_output: contains: "代码示例"`
**When** 智能体完成执行
**Then** 系统检查输出是否包含指定内容，不满足则测试失败

**Given** 测试用例包含 syscall 断言 `assert_syscalls: includes: [Read, Write]`
**When** 智能体完成执行
**Then** 系统检查 syscall 序列是否包含指定调用，不满足则测试失败

**Given** 测试用例包含质量断言 `assert_quality: "输出必须包含可执行的修复建议"`
**When** 智能体完成执行
**Then** 系统通过轻量模型评估输出质量，不满足标准则测试失败并附评估原因

## Story 16.3: 批量测试运行与报告

As a 平台构建者,
I want 通过 `crux agtest [test-file]` 批量运行测试并获得结构化结果报告,
So that 我可以快速了解整体回归状态。

**Acceptance Criteria:**

**Given** 一个或多个测试 YAML 文件
**When** 用户执行 `crux agtest tests/`
**Then** 系统按顺序运行所有测试用例，输出结果报告（通过/失败/跳过 + 失败原因）
**And** 单个测试用例框架开销 <= 500ms（NFR35）

**Given** 测试运行完成
**When** 存在失败用例
**Then** 报告中包含每个失败用例的断言类型、期望值、实际值和差异说明
