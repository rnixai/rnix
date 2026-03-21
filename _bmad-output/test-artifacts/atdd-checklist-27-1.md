---
stepsCompleted:
  - step-01-preflight-and-context
  - step-02-generation-mode
  - step-03-test-strategy
  - step-04-generate-tests
  - step-04c-aggregate
  - step-05-validate-and-complete
lastStep: step-05-validate-and-complete
lastSaved: '2026-03-21'
inputDocuments:
  - _bmad-output/implementation-artifacts/27-1-steprecord-type-and-disk-writer.md
  - _bmad/tea/config.yaml
  - kernel/atdd_26_4_unified_reasoning_test.go
  - kernel/process.go
  - debug/record.go
---

# ATDD Checklist: Story 27.1 — StepRecord 类型定义与磁盘写入器

## 基本信息

| 项目 | 值 |
|---|---|
| Story ID | 27-1 |
| 技术栈 | backend (Go 1.26) |
| 生成模式 | AI 生成 |
| TDD 阶段 | RED（所有测试 SKIP） |
| 测试框架 | Go testing + race detector |

## TDD Red Phase 状态

所有 15 个测试使用 `t.Skip("ATDD RED PHASE: ...")` 标记为跳过。

### 单元测试（kernel 包）— 11 个

| # | 测试名 | AC | 优先级 | 状态 |
|---|---|---|---|---|
| 1 | `TestATDD27_1_StepRecord_TypeDefinition` | AC-1 | P0 | SKIP |
| 2 | `TestATDD27_1_StepRecord_JSONRoundTrip` | AC-1 | P0 | SKIP |
| 3 | `TestATDD27_1_StepWriter_AppendAndRead` | AC-2 | P0 | SKIP |
| 4 | `TestATDD27_1_StepWriter_ConcurrentSafety` | AC-2 | P0 | SKIP |
| 5 | `TestATDD27_1_StepWriter_FlushGuarantee` | AC-2 | P1 | SKIP |
| 6 | `TestATDD27_1_StepWriter_CreatesDirStructure` | AC-2,3 | P1 | SKIP |
| 7 | `TestATDD27_1_StepWriter_WritePerformance` | AC-7 | P1 | SKIP |
| 8 | `TestATDD27_1_Integration_StepRecordAutoCreatedOnSpawn` | AC-3,5,6 | P0 | SKIP |
| 9 | `TestATDD27_1_Integration_FinalSystemPromptCaptured` | AC-4,5 | P1 | SKIP |
| 10 | `TestATDD27_1_Integration_ProcessMetaWrittenOnExit` | AC-8 | P0 | SKIP |
| 11 | `TestATDD27_1_Integration_WrittenBeforeAppendMessage` | AC-6 | P0 | SKIP |

### 单元测试（debug 包）— 4 个

| # | 测试名 | AC | 优先级 | 状态 |
|---|---|---|---|---|
| 12 | `TestRecordSimplify_ContextSnapshotData_NoSystemPromptHash` | AC-9 | P1 | SKIP |
| 13 | `TestRecordSimplify_ContextSnapshotData_NoMessageCount` | AC-9 | P1 | SKIP |
| 14 | `TestRecordSimplify_ContextSnapshotData_NoTokenEstimate` | AC-9 | P1 | SKIP |
| 15 | `TestRecordSimplify_LLMResponseData_NoResponseSummary` | AC-9 | P1 | SKIP |

## Acceptance Criteria 覆盖

| AC | 描述 | 测试覆盖 |
|---|---|---|
| AC-1 | StepRecord 类型定义 | #1, #2 |
| AC-2 | StepWriter 实现 | #3, #4, #5, #6 |
| AC-3 | Spawn 时自动创建 StepWriter | #6, #8 |
| AC-4 | Process 新增观察系统字段 | #9 |
| AC-5 | FinalSystemPrompt 首次捕获 | #8, #9 |
| AC-6 | StepRecord 组装与写入 | #8, #11 |
| AC-7 | 写入性能 ≤ 1ms | #7 |
| AC-8 | 进程退出时清理 | #10 |
| AC-9 | record 系统简化 | #12, #13, #14, #15 |

## 已创建的存根文件

实现过程中创建了编译所需的最小存根：

| 文件 | 说明 | 状态 |
|---|---|---|
| `internal/types/step_record.go` | StepRecord 类型定义（AC-1 完成） | 存根 |
| `kernel/step_writer.go` | StepWriter + ReadStep 实现（AC-2 部分完成） | 存根 |

> **注意**: 存根代码已包含完整的 StepWriter 实现逻辑（NewStepWriter, WriteStep, Close, ReadStep），但尚未集成到 kernel reasonStep 循环中。

## 下一步（TDD Green Phase）

实现 Story 27.1 后：

1. 将 `FinalSystemPrompt string` 和 `stepWriter *StepWriter` 字段添加到 Process 结构体
2. 在 reasonStep 循环中集成 StepRecord 捕获逻辑
3. 在 reaper 中添加 process-meta.json 写入和 StepWriter Close
4. 删除 debug/record.go 中的摘要字段
5. 移除所有 `t.Skip("ATDD RED PHASE: ...")` 调用
6. 运行 `go test -race -run "TestATDD27_1" ./kernel/ ./debug/` 验证 GREEN
7. 运行 `make all` 确认全量通过

## 验证命令

```bash
# 运行所有 ATDD 27.1 测试
go test -race -run "TestATDD27_1|TestRecordSimplify" ./kernel/ ./debug/ -v

# 仅运行 kernel 测试
go test -race -run "TestATDD27_1" ./kernel/ -v

# 仅运行 debug 测试
go test -race -run "TestRecordSimplify" ./debug/ -v
```
