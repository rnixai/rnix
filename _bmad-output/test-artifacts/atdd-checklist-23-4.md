---
stepsCompleted: ['step-01', 'step-02', 'step-03', 'step-04', 'step-05']
lastStep: 'step-05-validate-and-complete'
lastSaved: '2026-03-12'
workflowType: 'testarch-atdd'
inputDocuments: ['epic-23', '23-4-api-key-management.md', 'drivers/llm/factory.go', 'drivers/llm/openai_compat.go', 'drivers/llm/config.go', 'drivers/llm/errors.go', 'drivers/llm/factory_test.go']
---

# ATDD Checklist - Epic 23, Story 4: HTTP API Provider 的 API Key 管理

**Date:** 2026-03-12
**Author:** Decker
**Primary Test Level:** Unit + Integration
**Test File:** `drivers/llm/atdd_23_4_api_key_management_test.go`

---

## Story Summary

为 HTTP API 类型的 LLM provider 实现基于环境变量的 API Key 管理。`CreateDriver` 工厂函数在创建 `OpenAICompatDriver` 时读取 `api_key_env` 指定的环境变量，将 API Key 通过 `WithAPIKey()` 选项注入驱动。密钥不明文存储在配置文件中，符合安全最佳实践。

**As a** 用户
**I want** HTTP API 类型的 provider 通过环境变量引用 API Key
**So that** 密钥不明文存储在配置文件中，符合安全最佳实践

---

## Acceptance Criteria

| AC | Given | When | Then |
|----|-------|------|------|
| 1 | `rnix-providers.yaml` 中 provider 配置了 `api_key_env: GROQ_API_KEY` | 创建 `OpenAICompatDriver` 实例 | 从环境变量读取 API Key，通过 `WithAPIKey()` 传入驱动 |
| 2 | `api_key_env` 指定的环境变量不存在 | daemon 启动 | 日志输出 warning，provider 仍注册，首次调用返回 `ErrAuth` |
| 3 | 本地 provider 不需要 API Key | `api_key_env` 为空或未设置 | 驱动正常创建，不附带认证头 |
| 4 | 安全审计 | 检查配置文件和日志 | API Key 明文不出现在配置文件、日志输出或错误消息中 |

---

## Failing Tests Created (RED Phase)

### Unit Tests (11 tests)

**File:** `drivers/llm/atdd_23_4_api_key_management_test.go` (252 lines)

| # | Test Function | AC | Status | Failure Reason | Verifies |
|---|---------------|-----|--------|----------------|----------|
| 1 | `TestATDD_23_4_AC1_CreateDriver_ReadsAPIKeyFromEnv` | 1 | FAIL | CreateDriver 未读取 api_key_env 环境变量 | 环境变量中的 API Key 通过 WithAPIKey() 注入到 HTTP Authorization header |
| 2 | `TestATDD_23_4_AC1_RegisterProviders_PassesAPIKey` | 1 | FAIL | RegisterProviders 链路中 API Key 未传递 | 完整注册链路中 API Key 正确传递 |
| 3 | `TestATDD_23_4_AC2_CreateDriver_MissingEnvVar_StillCreates` | 2 | PASS | 回归守护（已有行为） | 环境变量缺失时 driver 仍创建成功 |
| 4 | `TestATDD_23_4_AC2_MissingEnvVar_NoAuthHeader` | 2 | PASS | 回归守护（已有行为） | 无 API Key 时不发送 Authorization header |
| 5 | `TestATDD_23_4_AC2_MissingEnvVar_ReturnsErrAuth` | 2 | PASS | 回归守护（已有行为） | 无 Auth 时 401 响应映射为 ErrAuth |
| 6 | `TestATDD_23_4_AC3_NoAPIKeyEnv_CreatesDriverWithoutAuth` | 3 | PASS | 回归守护（已有行为） | 本地 provider 无 api_key_env 时不发送认证头 |
| 7 | `TestATDD_23_4_AC3_EmptyAPIKeyEnv_CreatesDriverWithoutAuth` | 3 | PASS | 回归守护（已有行为） | 空字符串 api_key_env 时不发送认证头 |
| 8 | `TestATDD_23_4_AC4_APIKeyNotInDriverInfo` | 4 | PASS | 回归守护（已有行为） | Info() 输出不含 API Key |
| 9 | `TestATDD_23_4_AC4_APIKeyNotInErrorMessage` | 4 | PASS | 回归守护（已有行为） | 错误消息不含 API Key |
| 10 | `TestATDD_23_4_AC4_ProviderConfigStoresEnvVarNameNotValue` | 4 | PASS | 回归守护（配置设计验证） | ProviderConfig 仅存储环境变量名 |
| 11 | `TestATDD_23_4_RegisterProviders_APIKeyEnvMissing_StillRegisters` | 2 | PASS | 回归守护（已有行为） | 环境变量缺失时 RegisterProviders 仍成功 |

**Failing: 2 tests | Passing (regression guards): 9 tests | Total: 11 tests**

---

## AC 覆盖矩阵

| AC | Test IDs | Coverage |
|----|----------|----------|
| AC1 | #1, #2 | CreateDriver 环境变量读取 + RegisterProviders 完整链路 |
| AC2 | #3, #4, #5, #11 | driver 仍创建 + 无 Auth header + ErrAuth 错误链 + RegisterProviders |
| AC3 | #6, #7 | 空 api_key_env + 未设置 api_key_env |
| AC4 | #8, #9, #10 | Info() 不泄露 + 错误消息不泄露 + 配置结构审计 |

---

## Implementation Checklist

### Test: TestATDD_23_4_AC1_CreateDriver_ReadsAPIKeyFromEnv

**File:** `drivers/llm/factory.go`

**Tasks to make this test pass:**

- [ ] 在 `factory.go` 中新增 `"os"` import
- [ ] 在 `CreateDriver` 的 `DriverOpenAICompat` 分支中，读取 `cfg.APIKeyEnv`：
  - 若 `cfg.APIKeyEnv != ""`，调用 `os.Getenv(cfg.APIKeyEnv)`
  - 若返回值非空，调用 `WithAPIKey(key)` 追加到 opts
  - 若返回值为空，输出 warning 日志：`provider %q: API key env var %s not set`
- [ ] 删除 `factory.go:19` 的 defer 注释 `// API key handling is deferred to Story 23-4.`
- [ ] Run test: `go test -race -run TestATDD_23_4_AC1_CreateDriver_ReadsAPIKeyFromEnv ./drivers/llm/...`
- [ ] Test passes (green phase)

### Test: TestATDD_23_4_AC1_RegisterProviders_PassesAPIKey

**File:** `drivers/llm/factory.go` (same changes as above)

**Tasks to make this test pass:**

- [ ] 上述 CreateDriver 修改自动使此测试通过（RegisterProviders 调用 CreateDriver）
- [ ] Run test: `go test -race -run TestATDD_23_4_AC1_RegisterProviders_PassesAPIKey ./drivers/llm/...`
- [ ] Test passes (green phase)

---

## Running Tests

```bash
# Run all ATDD tests for this story
go test -race -run "TestATDD_23_4" ./drivers/llm/... -v

# Run specific test
go test -race -run TestATDD_23_4_AC1_CreateDriver_ReadsAPIKeyFromEnv ./drivers/llm/... -v

# Run all tests in the llm package
go test -race ./drivers/llm/...

# Run with coverage
go test -race -cover -run "TestATDD_23_4" ./drivers/llm/... -v
```

---

## Red-Green-Refactor Workflow

### RED Phase (Complete)

**TEA Agent Responsibilities:**

- All 11 tests written
- 2 tests failing as expected (AC1: env var reading not yet implemented)
- 9 tests passing as regression guards (existing behavior)
- Implementation checklist created with concrete tasks

**Verification:**

- 2 tests fail due to missing implementation in `CreateDriver` (not test bugs)
- Failure messages are clear: "expected Authorization header ... got empty"
- All passing tests verify already-working behavior

---

### GREEN Phase (DEV Team - Next Steps)

**DEV Agent Responsibilities:**

1. Modify `drivers/llm/factory.go`: add env var reading in `DriverOpenAICompat` case (~8 lines)
2. Run AC1 tests to verify they pass
3. Run full test suite to ensure no regressions
4. Delete the defer comment on line 19

**Key Code Change:**

```go
case DriverOpenAICompat:
    var opts []CompatOption
    if cfg.DefaultModel != "" {
        opts = append(opts, WithCompatModel(cfg.DefaultModel))
    }
    if cfg.APIKeyEnv != "" {
        if key := os.Getenv(cfg.APIKeyEnv); key != "" {
            opts = append(opts, WithAPIKey(key))
        } else {
            log.Printf("[llm] warning: provider %q: API key env var %s not set", cfg.Name, cfg.APIKeyEnv)
        }
    }
    return NewOpenAICompatDriver(cfg.Name, cfg.BaseURL, opts...), nil
```

**Estimated Effort:** < 1 hour

---

### REFACTOR Phase (After All Tests Pass)

1. Verify all 11 tests pass
2. Run full package tests: `go test -race ./drivers/llm/...`
3. Run lint: `make lint`
4. Verify no API key leakage in any log output

---

## Test Execution Evidence

### Initial Test Run (RED Phase Verification)

**Command:** `go test -race -run "TestATDD_23_4" ./drivers/llm/... -v`

**Results:**

```
TestATDD_23_4_AC1_CreateDriver_ReadsAPIKeyFromEnv       FAIL  (Authorization header empty)
TestATDD_23_4_AC1_RegisterProviders_PassesAPIKey         FAIL  (Authorization header empty)
TestATDD_23_4_AC2_CreateDriver_MissingEnvVar_StillCreates  PASS
TestATDD_23_4_AC2_MissingEnvVar_NoAuthHeader              PASS
TestATDD_23_4_AC2_MissingEnvVar_ReturnsErrAuth            PASS
TestATDD_23_4_AC3_NoAPIKeyEnv_CreatesDriverWithoutAuth    PASS
TestATDD_23_4_AC3_EmptyAPIKeyEnv_CreatesDriverWithoutAuth PASS
TestATDD_23_4_AC4_APIKeyNotInDriverInfo                   PASS
TestATDD_23_4_AC4_APIKeyNotInErrorMessage                 PASS
TestATDD_23_4_AC4_ProviderConfigStoresEnvVarNameNotValue  PASS
TestATDD_23_4_RegisterProviders_APIKeyEnvMissing_StillRegisters  PASS
```

**Summary:**

- Total tests: 11
- Passing (regression guards): 9
- Failing (need implementation): 2
- Status: RED phase verified

---

## 变更范围

| 文件 | 变更类型 | 说明 |
|------|---------|------|
| `drivers/llm/factory.go` | 修改 | `CreateDriver` openai-compat 分支新增环境变量读取 + `WithAPIKey()` 传入；新增 `"os"` import；删除 defer 注释 |
| `drivers/llm/atdd_23_4_api_key_management_test.go` | 新增 | 11 个测试函数验证 API Key 注入、缺失场景、不泄露 |

**不修改的文件：**
- `drivers/llm/config.go` — `ProviderConfig.APIKeyEnv` 字段已定义
- `drivers/llm/openai_compat.go` — `WithAPIKey()`、`doHTTP()` Authorization header 全部已实现
- `drivers/llm/errors.go` — `ErrAuth` sentinel 已定义

---

## Notes

- 本 Story 变更范围极小（仅 `factory.go` 约 8 行），但通过 ATDD 确保了环境变量读取、安全性、向后兼容三个维度的测试覆盖
- 使用 `t.Setenv` 的测试不能标记 `t.Parallel()`（Go 限制：修改进程全局环境变量）
- AC2 和 AC3 的测试已通过（回归守护），证明现有代码已具备无 API Key 时的正确行为
- 安全审计测试（AC4）验证了 `DriverInfo`、错误消息、配置结构三个泄露面

---

## Next Steps

1. **将此 checklist 和测试文件交给 DEV workflow**
2. **运行失败测试确认 RED phase**: `go test -race -run "TestATDD_23_4_AC1" ./drivers/llm/... -v`
3. **实现 CreateDriver 环境变量读取**（约 8 行代码）
4. **运行全部测试验证 GREEN phase**: `go test -race -run "TestATDD_23_4" ./drivers/llm/... -v`
5. **运行完整包测试确保无回归**: `go test -race ./drivers/llm/...`
6. **完成后更新 sprint-status.yaml**

---

**Generated by BMad TEA Agent** - 2026-03-12
