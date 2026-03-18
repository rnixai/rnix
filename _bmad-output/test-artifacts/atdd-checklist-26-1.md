---
stepsCompleted: ['step-01-preflight-and-context', 'step-02-generation-mode', 'step-03-test-strategy', 'step-04-generate-tests']
lastStep: 'step-04-generate-tests'
lastSaved: '2026-03-18'
workflowType: 'testarch-atdd'
inputDocuments:
  - '_bmad-output/implementation-artifacts/26-1-unified-reasonstep-and-actiontype-extension.md'
---

# ATDD Checklist - Epic 26, Story 1: OODA 代码删除与统一分支入口

**Date:** 2026-03-18
**Primary Test Level:** Unit/Integration (Go backend)
**Story Type:** PURE DELETION — 无新功能，仅删除 OODA 代码并统一推理循环入口

---

## Story Summary

**As a** 平台构建者
**I want** 删除所有 OODA 相关代码并统一推理循环入口为单一 `reasonStep`
**So that** 代码库只有一条推理路径，消除双模式维护负担

本 Story 为纯删除操作：删除 `kernel/ooda.go`、OODA 测试文件、ooda-demo/ooda-agent 目录，清理 Process 结构体、SpawnOpts、LogOODA 常量及 main.go 注释，统一所有进程走 `k.reasonStep()`。

---

## Acceptance Criteria

| AC | 描述 |
|----|------|
| **AC-1** | 删除 OODA 核心实现文件：`kernel/ooda.go`（531 行）完全删除 |
| **AC-2** | 删除 OODA 测试文件：`kernel/ooda_test.go`（819 行）和 `kernel/ooda_reasoning_test.go`（650 行）完全删除 |
| **AC-3** | 删除 ooda-demo Agent 目录：`lib/agents/ooda-demo/` 完全删除 |
| **AC-4** | 删除 ooda-agent 测试数据目录：`agents/testdata/ooda-agent/` 完全删除 |
| **AC-5** | 清理 Process 结构体：删除 `oodaEnabled`、`oodaState` 字段及 `IsOODA()`、`GetOODAState()`、`SetOODAPhase()` 方法 |
| **AC-6** | 统一 Spawn 分支入口：删除 `SpawnOpts.ReasoningMode`、OODA 初始化块、`oodaEnabled` 分支，所有进程走 `k.reasonStep()` |
| **AC-7** | 删除 LogOODA 常量：`internal/types/types.go` 中删除 `LogOODA` |
| **AC-8** | 清理 main.go 注释：删除 OODA 相关注释 |
| **AC-9** | 编译和静态分析通过：`go build ./cmd/rnix/` 成功，`go vet ./...` 无警告 |

---

## Failing Tests Created (RED Phase)

### Verification Tests (18 tests)

#### AC-1 ~ AC-4: 文件/目录删除验证

**File:** `kernel/atdd_26_1_ooda_deletion_test.go`

- RED **Test:** TestOODA_CoreFileRemoved
  - **Status:** RED — `kernel/ooda.go` 仍存在
  - **Verifies:** AC#1 — OODA 核心实现文件已删除
  - **Priority:** P0

- RED **Test:** TestOODA_TestFilesRemoved
  - **Status:** RED — `kernel/ooda_test.go` 或 `kernel/ooda_reasoning_test.go` 仍存在
  - **Verifies:** AC#2 — OODA 测试文件已删除
  - **Priority:** P0

- RED **Test:** TestOODA_DemoAgentDirRemoved
  - **Status:** RED — `lib/agents/ooda-demo/` 目录仍存在
  - **Verifies:** AC#3 — ooda-demo Agent 目录已删除
  - **Priority:** P0

- RED **Test:** TestOODA_TestdataAgentDirRemoved
  - **Status:** RED — `agents/testdata/ooda-agent/` 目录仍存在
  - **Verifies:** AC#4 — ooda-agent 测试数据目录已删除
  - **Priority:** P0

- RED **Test:** TestOODA_AllArtifactsRemoved
  - **Status:** RED — 任一 OODA 文件/目录仍存在
  - **Verifies:** AC#1~AC#4 — 汇总验证：所有 OODA 文件与目录均不存在
  - **Priority:** P0

---

#### AC-5: Process 结构体清理验证

**File:** `kernel/atdd_26_1_ooda_deletion_test.go`

- RED **Test:** TestProcess_NoOODAFields
  - **Status:** RED — Process 结构体仍包含 `oodaEnabled` 或 `oodaState` 字段（通过反射或编译检查）
  - **Verifies:** AC#5 — Process 结构体已移除 OODA 字段
  - **Priority:** P0

- RED **Test:** TestProcess_NoOODAMethods
  - **Status:** RED — Process 仍包含 `IsOODA`、`GetOODAState`、`SetOODAPhase` 方法（通过反射检查）
  - **Verifies:** AC#5 — Process 已移除 OODA 方法
  - **Priority:** P0

- RED **Test:** TestProcess_SpawnAndReasonStepStillWorks
  - **Status:** RED — 若删除后破坏 Process 导致回归则失败
  - **Verifies:** AC#5 — Process 清理后现有功能仍正常（回归）
  - **Priority:** P0

---

#### AC-6: Spawn 分支入口统一验证

**File:** `kernel/atdd_26_1_ooda_deletion_test.go`

- RED **Test:** TestSpawnOpts_NoReasoningModeField
  - **Status:** RED — SpawnOpts 仍包含 `ReasoningMode` 字段（通过反射检查）
  - **Verifies:** AC#6 — SpawnOpts 已移除 ReasoningMode 字段
  - **Priority:** P0

- RED **Test:** TestSpawn_AllProcessesUseReasonStep
  - **Status:** RED — `kernel.go` 中仍存在 `oodaReasonStep` 调用或 `oodaEnabled` 分支
  - **Verifies:** AC#6 — 所有进程统一走 `reasonStep`，无 OODA 分支
  - **Priority:** P0

- RED **Test:** TestSpawn_StemAgentIntegrationStillWorks
  - **Status:** RED — stem 集成测试失败（回归）
  - **Verifies:** AC#6 — 统一入口后 stem 分化仍正常
  - **Priority:** P0

- RED **Test:** TestAgentLoader_NoOODAReasoningCheck
  - **Status:** RED — `kernel.go` 中仍存在 `Reasoning == "ooda"` 块
  - **Verifies:** AC#6 — Agent 加载逻辑已移除 OODA 模式检查
  - **Priority:** P1

---

#### AC-7: LogOODA 常量删除验证

**File:** `internal/types/atdd_26_1_ooda_deletion_test.go`

- RED **Test:** TestTypes_LogOODARemoved
  - **Status:** RED — `types.LogOODA` 仍存在（编译时引用会失败，或通过反射/常量检查）
  - **Verifies:** AC#7 — LogOODA 常量已删除
  - **Priority:** P0

- RED **Test:** TestTypes_NoLogOODAReferences
  - **Status:** RED — 代码库中仍存在 `LogOODA` 引用（通过 grep 或构建时验证）
  - **Verifies:** AC#7 — 所有 LogOODA 引用已替换为 LogOutput 等
  - **Priority:** P0

---

#### AC-8: main.go 注释清理验证

**File:** `cmd/rnix/atdd_26_1_ooda_deletion_test.go`

- RED **Test:** TestMainGo_NoOODAComments
  - **Status:** RED — `cmd/rnix/main.go` 中仍包含 "OODA" 相关注释
  - **Verifies:** AC#8 — main.go 已清理 OODA 注释
  - **Priority:** P1

---

### Build Verification Tests

**File:** 脚本/CI 或 `make all` 验证

- RED **Test:** BuildVerify_GoBuildSucceeds
  - **Status:** RED — `go build ./cmd/rnix/` 失败（存在 OODA 引用或未删除的依赖）
  - **Verifies:** AC#9 — 编译成功
  - **Priority:** P0

- RED **Test:** BuildVerify_GoVetClean
  - **Status:** RED — `go vet ./...` 有警告
  - **Verifies:** AC#9 — 静态分析无警告
  - **Priority:** P0

- RED **Test:** BuildVerify_KernelTestsPass
  - **Status:** RED — `go test -race ./kernel/...` 失败
  - **Verifies:** AC#9 — kernel 包测试通过（回归）
  - **Priority:** P0

- RED **Test:** BuildVerify_AgentsTestsPass
  - **Status:** RED — `go test -race ./agents/...` 失败
  - **Verifies:** AC#9 — agents 包测试通过（回归）
  - **Priority:** P0

- RED **Test:** BuildVerify_TypesTestsPass
  - **Status:** RED — `go test -race ./internal/types/...` 失败
  - **Verifies:** AC#9 — types 包测试通过（回归）
  - **Priority:** P0

- RED **Test:** BuildVerify_MakeAllPass
  - **Status:** RED — `make all` 失败
  - **Verifies:** AC#9 — lint + vet + test + build 全通过
  - **Priority:** P0

---

## AC <-> Test 覆盖矩阵

| AC | 描述 | 测试 |
|----|------|------|
| AC-1 | 删除 kernel/ooda.go | TestOODA_CoreFileRemoved, TestOODA_AllArtifactsRemoved |
| AC-2 | 删除 OODA 测试文件 | TestOODA_TestFilesRemoved, TestOODA_AllArtifactsRemoved |
| AC-3 | 删除 ooda-demo 目录 | TestOODA_DemoAgentDirRemoved, TestOODA_AllArtifactsRemoved |
| AC-4 | 删除 ooda-agent 测试数据 | TestOODA_TestdataAgentDirRemoved, TestOODA_AllArtifactsRemoved |
| AC-5 | 清理 Process 结构体 | TestProcess_NoOODAFields, TestProcess_NoOODAMethods, TestProcess_SpawnAndReasonStepStillWorks |
| AC-6 | 统一 Spawn 分支入口 | TestSpawnOpts_NoReasoningModeField, TestSpawn_AllProcessesUseReasonStep, TestSpawn_StemAgentIntegrationStillWorks, TestAgentLoader_NoOODAReasoningCheck |
| AC-7 | 删除 LogOODA 常量 | TestTypes_LogOODARemoved, TestTypes_NoLogOODAReferences |
| AC-8 | 清理 main.go 注释 | TestMainGo_NoOODAComments |
| AC-9 | 编译和静态分析 | BuildVerify_GoBuildSucceeds, BuildVerify_GoVetClean, BuildVerify_KernelTestsPass, BuildVerify_AgentsTestsPass, BuildVerify_TypesTestsPass, BuildVerify_MakeAllPass |

---

## 测试策略说明（删除类 Story）

- **文件存在性检查**：Go 测试中使用 `os.Stat` 或 `filepath.Walk` 验证目标路径不存在；实现删除后由 RED 变为 GREEN。
- **反射检查**：用于验证 Process 无 OODA 字段/方法、SpawnOpts 无 ReasoningMode 字段。
- **回归测试**：依赖现有 `kernel/stem_integration_test.go`、`kernel/diffmemory_integration_test.go`、`kernel/lineage_integration_test.go` 中的非 OODA 测试；删除 OODA 后这些测试应继续通过。
- **构建验证**：`go build`、`go vet`、`go test -race` 作为删除完成的最终验收标准。

---

## 实现目标文件

| 文件 | 状态 | 说明 |
|------|------|------|
| `kernel/ooda.go` | 待删除 | AC-1 |
| `kernel/ooda_test.go` | 待删除 | AC-2 |
| `kernel/ooda_reasoning_test.go` | 待删除 | AC-2 |
| `lib/agents/ooda-demo/` | 待删除 | AC-3 |
| `agents/testdata/ooda-agent/` | 待删除 | AC-4 |
| `kernel/process.go` | 待修改 | 删除 OODA 字段和方法 (AC-5) |
| `kernel/kernel.go` | 待修改 | 统一推理入口 (AC-6) |
| `internal/types/types.go` | 待修改 | 删除 LogOODA (AC-7) |
| `cmd/rnix/main.go` | 待修改 | 删除 OODA 注释 (AC-8) |
| `kernel/atdd_26_1_ooda_deletion_test.go` | 待创建 | 删除验证测试 |
| `internal/types/atdd_26_1_ooda_deletion_test.go` | 待创建 | LogOODA 删除验证 |
| `cmd/rnix/atdd_26_1_ooda_deletion_test.go` | 待创建 | main.go 注释验证 |

---

## 测试优先级分布

| 优先级 | 数量 | 测试 |
|--------|------|------|
| P0 | 16 | 文件/目录删除 (5)、Process 清理 (3)、Spawn 统一 (3)、LogOODA (2)、Build 验证 (6) |
| P1 | 2 | TestAgentLoader_NoOODAReasoningCheck, TestMainGo_NoOODAComments |

---

## 下一步

1. 按 Story 文档的 Task 顺序执行删除与修改
2. 创建 `atdd_26_1_ooda_deletion_test.go` 验证测试文件
3. 运行 `make all` 确保 lint + vet + test + build 全部通过
4. 所有删除验证测试由 RED 变为 GREEN
