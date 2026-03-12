---
stepsCompleted:
  - 'step-01-preflight-and-context'
  - 'step-02-generation-mode'
  - 'step-03-test-strategy'
  - 'step-04c-aggregate'
  - 'step-05-validate-and-complete'
lastStep: 'step-05-validate-and-complete'
lastSaved: '2026-03-12'
workflowType: 'testarch-atdd'
inputDocuments:
  - '_bmad-output/implementation-artifacts/23-7-compose-init-config-upgrade.md'
---

# ATDD Checklist - Epic 23, Story 7: rnix-compose/init 配置格式升级

**Date:** 2026-03-12
**Author:** Decker
**Primary Test Level:** Unit + Integration

---

## Story Summary

`rnix-compose.yaml` 和 `rnix-init.yaml` 支持 `provider` + `model` 组合配置，使多 provider 场景下模型指定不再产生歧义。

**As a** 用户
**I want** `rnix-compose.yaml` 和 `rnix-init.yaml` 支持 `provider: xxx` + `model: xxx` 组合配置
**So that** 多 provider 场景下模型指定不会产生歧义

---

## Acceptance Criteria

1. **AC1**: Compose YAML agent 指定 `provider: ollama` + `model: llama3` → 引擎 Spawn 时传递正确
2. **AC2**: 向后兼容——旧格式仅指定 `model: haiku`（无 `provider`）→ 系统使用默认 provider（claude）
3. **AC3**: Init YAML supervisor children 指定 `provider: groq` + `model: llama-3.3-70b-versatile` → Bootstrap 正确传递

---

## Failing Tests Created (RED Phase)

### Compose ATDD 集成测试 (4 tests)

**File:** `compose/atdd_23_7_compose_init_config_upgrade_test.go` (467 lines)

- **Test:** `TestATDD_23_7_AC1_ComposeProviderPassedToSpawn`
  - **Status:** RED - `AgentSpec.Provider` undefined (编译失败)
  - **Verifies:** AC1 — agent 级 `provider: ollama` + `model: llama3` 透传到 `ComposeSpawnOpts`

- **Test:** `TestATDD_23_7_AC2_ComposeBackwardCompat`
  - **Status:** RED - `AgentSpec.Provider` undefined (编译失败)
  - **Verifies:** AC2 — 旧格式无 provider 字段时 Provider 为空字符串

- **Test:** `TestATDD_23_7_AC2_ComposeGlobalProviderFallback`
  - **Status:** RED - `ComposeSpec.Provider` undefined (编译失败)
  - **Verifies:** AC2 — spec 全局 `provider: groq` 作为 fallback 传递到 spawn opts

- **Test:** `TestATDD_23_7_AC2_AgentProviderOverridesGlobal`
  - **Status:** RED - `ComposeSpec.Provider` undefined (编译失败)
  - **Verifies:** agent 级 `provider: ollama` 覆盖 spec 全局 `provider: groq`

### Compose 单元测试 - Parser (3 tests)

**File:** `compose/atdd_23_7_compose_init_config_upgrade_test.go`

- **Test:** `TestParseBytes_AgentProvider`
  - **Status:** RED - `AgentSpec.Provider` undefined
  - **Verifies:** YAML `provider: ollama` 正确解析到 `AgentSpec.Provider`

- **Test:** `TestParseBytes_GlobalProvider`
  - **Status:** RED - `ComposeSpec.Provider` undefined
  - **Verifies:** YAML 顶层 `provider: groq` 正确解析到 `ComposeSpec.Provider`

- **Test:** `TestParseBytes_NoProvider_BackwardCompat`
  - **Status:** RED - `ComposeSpec.Provider` / `AgentSpec.Provider` undefined
  - **Verifies:** 无 provider YAML 解析后两者均为空字符串

### Compose 单元测试 - Engine (4 tests)

**File:** `compose/atdd_23_7_compose_init_config_upgrade_test.go`

- **Test:** `TestEngine_Execute_AgentProviderPassedToSpawn`
  - **Status:** RED - `ComposeSpawnOpts.Provider` undefined
  - **Verifies:** 引擎将 agent 级 provider 传递到 spawn opts

- **Test:** `TestEngine_Execute_GlobalProviderFallback`
  - **Status:** RED - `ComposeSpec.Provider` / `ComposeSpawnOpts.Provider` undefined
  - **Verifies:** agent 无 provider 时引擎使用 spec 全局 provider

- **Test:** `TestEngine_Execute_AgentProviderOverridesGlobal`
  - **Status:** RED - `ComposeSpec.Provider` / `AgentSpec.Provider` / `ComposeSpawnOpts.Provider` undefined
  - **Verifies:** agent 级 provider 优先于全局 provider

- **Test:** `TestEngine_Execute_NoProvider_EmptyString`
  - **Status:** RED - `ComposeSpawnOpts.Provider` undefined
  - **Verifies:** 无 provider 配置时 opts.Provider 为空字符串

### Compose 编译检查 (1 test)

- **Test:** `TestATDD_23_7_TypesExist`
  - **Status:** RED - 多个 Provider 字段 undefined
  - **Verifies:** `ComposeSpec.Provider`、`AgentSpec.Provider`、`ComposeSpawnOpts.Provider` 类型存在性

### Kernel ATDD 集成测试 (2 tests)

**File:** `kernel/atdd_23_7_compose_init_config_upgrade_test.go` (299 lines)

- **Test:** `TestATDD_23_7_AC3_InitChildProvider`
  - **Status:** RED - `ChildConfig.Provider` undefined (编译失败)
  - **Verifies:** AC3 — init YAML child `provider: groq` + `model: llama-3.3-70b-versatile` 正确解析

- **Test:** `TestATDD_23_7_AC3_InitChildNoProvider`
  - **Status:** RED - `ChildConfig.Provider` undefined (编译失败)
  - **Verifies:** AC3 — 无 provider 字段时 ChildConfig.Provider 为空字符串

### Kernel 单元测试 - Config Parsing (2 tests)

**File:** `kernel/atdd_23_7_compose_init_config_upgrade_test.go`

- **Test:** `TestLoadInitConfig_ChildProvider`
  - **Status:** RED - `ChildConfig.Provider` undefined
  - **Verifies:** YAML children 中 provider 字段解析（含多 child 混合场景）

- **Test:** `TestLoadInitConfig_ChildNoProvider_BackwardCompat`
  - **Status:** RED - `ChildConfig.Provider` undefined
  - **Verifies:** 旧格式 YAML 无 provider 时解析为空字符串

### Kernel 单元测试 - Supervisor (2 tests)

**File:** `kernel/atdd_23_7_compose_init_config_upgrade_test.go`

- **Test:** `TestToSupervisorSpec_ChildProvider`
  - **Status:** RED - `ChildConfig.Provider` / `ChildSpec.Provider` undefined
  - **Verifies:** `toSupervisorSpec()` 将 ChildConfig.Provider 正确传递到 ChildSpec.Provider

- **Test:** `TestBootstrap_SupervisorChildProvider`
  - **Status:** RED - `ChildConfig.Provider` undefined
  - **Verifies:** Bootstrap 流程中 provider 通过 supervisor 传递到 SpawnOpts

### Kernel 编译检查 (1 test)

- **Test:** `TestATDD_23_7_InitTypesExist`
  - **Status:** RED - `ChildConfig.Provider` / `ChildSpec.Provider` undefined
  - **Verifies:** `ChildConfig.Provider`、`ChildSpec.Provider` 类型存在性

---

## Implementation Checklist

### Test: TestATDD_23_7_AC1_ComposeProviderPassedToSpawn

**File:** `compose/atdd_23_7_compose_init_config_upgrade_test.go`

**Tasks to make this test pass:**

- [ ] `compose/types.go`: `AgentSpec` 新增 `Provider string` (yaml tag `provider,omitempty`)
- [ ] `compose/types.go`: `ComposeSpawnOpts` 新增 `Provider string`
- [ ] `compose/engine.go`: `executeNode` 中构建 opts 时设置 `Provider`（agent级 > spec全局）
- [ ] Run test: `go test -race -run TestATDD_23_7_AC1 ./compose/...`
- [ ] Test passes (green phase)

### Test: TestATDD_23_7_AC2_ComposeBackwardCompat + GlobalProviderFallback + AgentProviderOverridesGlobal

**File:** `compose/atdd_23_7_compose_init_config_upgrade_test.go`

**Tasks to make these tests pass:**

- [ ] `compose/types.go`: `ComposeSpec` 新增 `Provider string` (yaml tag `provider,omitempty`)
- [ ] `compose/engine.go`: Provider 优先级逻辑 — agent级 > spec全局 > 空字符串（系统默认）
- [ ] Run test: `go test -race -run 'TestATDD_23_7_AC2|TestEngine_Execute.*Provider' ./compose/...`
- [ ] All tests pass (green phase)

### Test: TestATDD_23_7_AC3_InitChildProvider + InitChildNoProvider

**File:** `kernel/atdd_23_7_compose_init_config_upgrade_test.go`

**Tasks to make these tests pass:**

- [ ] `kernel/init.go`: `ChildConfig` 新增 `Provider string` (yaml tag `provider`)
- [ ] `kernel/supervisor.go`: `ChildSpec` 新增 `Provider string`
- [ ] `kernel/init.go`: `toSupervisorSpec` 传递 `Provider`
- [ ] `kernel/supervisor.go`: `startChild` 将 `spec.Provider` 放入 `SpawnOpts.Provider`
- [ ] Run test: `go test -race -run 'TestATDD_23_7_AC3|TestLoadInitConfig_Child|TestToSupervisorSpec_Child|TestBootstrap_SupervisorChildProvider' ./kernel/...`
- [ ] All tests pass (green phase)

### Test: IPC 桥接层 (手动验证)

**File:** `cmd/rnix/compose.go`

**Tasks:**

- [ ] `cmd/rnix/compose.go`: `ipcKernelSpawner.Spawn` 将 `opts.Provider` 传递到 `ipc.SpawnRequest.Provider`
- [ ] 该路径通过 ATDD AC1 间接覆盖（compose engine → IPC spawner → kernel）

---

## Running Tests

```bash
# Run all failing tests for this story (compose)
go test -race -run 'TestATDD_23_7|TestParseBytes_.*Provider|TestEngine_Execute_.*Provider' ./compose/...

# Run all failing tests for this story (kernel)
go test -race -run 'TestATDD_23_7|TestLoadInitConfig_Child|TestToSupervisorSpec_Child|TestBootstrap_SupervisorChildProvider' ./kernel/...

# Run all 23-7 tests across both packages
go test -race -run 'TestATDD_23_7|TestParseBytes_.*Provider|TestEngine_Execute_.*Provider|TestLoadInitConfig_Child|TestToSupervisorSpec_Child|TestBootstrap_SupervisorChildProvider' ./compose/... ./kernel/...

# Run with verbose output
go test -race -v -run 'TestATDD_23_7' ./compose/... ./kernel/...
```

---

## Red-Green-Refactor Workflow

### RED Phase (Complete)

**TEA Agent Responsibilities:**

- All 19 tests written and failing (编译错误 — Provider 字段不存在)
- Tests organized in 2 文件：compose (467 行) + kernel (299 行)
- 测试覆盖所有 3 个 AC：Compose Provider、向后兼容、Init Provider
- Implementation checklist created

**Verification:**

- All tests fail due to missing `Provider` fields (compile errors)
- Failure messages are clear: `undefined (type X has no field or method Provider)`
- Tests fail due to missing implementation, not test bugs

---

### GREEN Phase (DEV Team - Next Steps)

**DEV Agent Responsibilities:**

1. **`compose/types.go`**: 新增 `Provider` 字段到 `ComposeSpec`、`AgentSpec`、`ComposeSpawnOpts`
2. **`compose/engine.go`**: `executeNode` 中添加 Provider 优先级逻辑
3. **`kernel/init.go`**: `ChildConfig` 新增 `Provider`；`toSupervisorSpec` 传递
4. **`kernel/supervisor.go`**: `ChildSpec` 新增 `Provider`；`startChild` 传递到 `SpawnOpts`
5. **`cmd/rnix/compose.go`**: IPC 桥接层传递 `opts.Provider`
6. Run all tests → verify pass
7. Run `make all` → verify no regression

---

### REFACTOR Phase (DEV Team - After All Tests Pass)

1. Verify all tests pass
2. Review Provider 优先级逻辑与 Model 优先级逻辑对称性
3. Ensure `make all` passes (lint + vet + test + build)
4. Update `sprint-status.yaml`

---

## Test Execution Evidence

### Initial Test Run (RED Phase Verification)

**Command:** `go test -race -run 'TestATDD_23_7' ./compose/... ./kernel/...`

**Results:**

```
# github.com/rnixai/rnix/compose [github.com/rnixai/rnix/compose.test]
compose/atdd_23_7_compose_init_config_upgrade_test.go:44:27: spec.Agents["worker"].Provider undefined (type *AgentSpec has no field or method Provider)
compose/atdd_23_7_compose_init_config_upgrade_test.go:68:14: rec.opts.Provider undefined (type ComposeSpawnOpts has no field or method Provider)
... (11+ compile errors)
FAIL	github.com/rnixai/rnix/compose [build failed]

# github.com/rnixai/rnix/kernel [github.com/rnixai/rnix/kernel.test]
kernel/atdd_23_7_compose_init_config_upgrade_test.go:60:11: child.Provider undefined (type ChildConfig has no field or method Provider)
... (10+ compile errors)
FAIL	github.com/rnixai/rnix/kernel [build failed]
```

**Summary:**

- Total tests: 19
- Passing: 0 (expected)
- Failing: 19 (build failed — expected)
- Status: RED phase verified

---

## Notes

- Provider 优先级链：agent 级 > spec 全局 > 系统默认（空字符串 = claude）
- 空字符串表示"使用系统默认"，由 kernel 层 `resolveLLMDevice()` 决定（Story 23-3 已实现）
- IPC 层无需修改协议 — `SpawnRequest.Provider` 已存在（Story 23-3 添加）
- YAML 解析由 `go-yaml` 自动处理，`compose/parser.go` 无需修改
- 本 Story 是 Epic 23 的最后一个 Story，完成后 Epic 23 可标记为 done

---

## 变更范围

| 文件 | 变更类型 | 说明 |
|------|---------|------|
| `compose/types.go` | 修改 | `ComposeSpec` + `AgentSpec` + `ComposeSpawnOpts` 新增 `Provider` |
| `compose/engine.go` | 修改 | `executeNode` — Provider 优先级逻辑 |
| `cmd/rnix/compose.go` | 修改 | IPC 桥接传递 `opts.Provider` |
| `kernel/init.go` | 修改 | `ChildConfig` 新增 `Provider`；`toSupervisorSpec` 传递 |
| `kernel/supervisor.go` | 修改 | `ChildSpec` 新增 `Provider`；`startChild` 传递 |

---

**Generated by BMad TEA Agent** - 2026-03-12
