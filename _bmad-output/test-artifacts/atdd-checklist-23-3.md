---
stepsCompleted: ['step-01', 'step-02', 'step-03', 'step-04', 'step-05']
lastStep: 'step-05-validate-and-complete'
lastSaved: '2026-03-12'
workflowType: 'testarch-atdd'
inputDocuments: ['epic-23', '23-3-dynamic-provider-resolution.md', 'kernel/kernel.go']
---

# ATDD Checklist - Epic 23, Story 3: Provider 动态解析与白名单移除

**Date:** 2026-03-12
**Author:** Decker
**Primary Test Level:** Unit + Integration
**Test File:** `kernel/atdd_23_3_dynamic_provider_resolution_test.go`

---

## Story Summary

重构 `resolveLLMDevice()` 从硬编码白名单（`allowedLLMProviders` map）改为查询 `DriverRegistry`，
实现 provider 动态解析。用户添加新 provider 时内核代码无需修改。

核心变更：
- 删除 `allowedLLMProviders` 硬编码 map
- `resolveLLMDevice` 从包级函数改为 `KernelImpl` 方法
- 新增 `SetProviderResolver` 回调注入（与 `agentLoader`、`skillLoader` 模式一致）
- 错误消息包含可用 provider 列表

## Acceptance Criteria

| AC | Given | When | Then |
|----|-------|------|------|
| 1 | `allowedLLMProviders` 硬编码 map | 重构后 | 移除白名单，改为查询 `DriverRegistry` |
| 2 | Agent `agent.yaml` 指定 `provider: ollama` | Spawn 调用 `resolveLLMDevice()` | 查询 Registry，确认已注册，返回 `/dev/llm/ollama` |
| 3 | Agent 指定不存在的 provider `nonexist` | Spawn 时 | 返回错误含 `(available: claude, cursor, ollama)` |
| 4 | Agent 未指定 provider（空字符串） | Spawn 时 | 使用系统默认 `claude` |
| 5 | CLI `--provider=groq` | Spawn 时 | CLI 参数覆盖 agent.yaml 配置 |

## Failing Tests Created (RED Phase)

### Unit Tests (resolveLLMDevice 直接调用)

| # | Test Function | AC | Status | Failure Reason | Verifies |
|---|---------------|-----|--------|----------------|----------|
| 1 | `TestATDD_23_3_AC1_NoHardcodedWhitelist_DynamicProvider` | 1 | SKIP | "ollama" 不在硬编码白名单 | 动态 provider 通过 Registry 检查 |
| 2 | `TestATDD_23_3_AC1_NilResolverAllowsAll` | 1 | SKIP | "anydriver" 不在硬编码白名单 | 向后兼容：nil resolver 不阻拦 |
| 3 | `TestATDD_23_3_AC2_DirectResolve_RegisteredProvider` | 2 | SKIP | "ollama" 不在硬编码白名单 | 已注册 provider 返回正确路径 |
| 4 | `TestATDD_23_3_AC3_UnregisteredProviderClearError` | 3 | SKIP | 错误消息不含 `(available:` | 错误消息包含可用列表 |
| 5 | `TestATDD_23_3_AC3_ErrorListsSortedProviders` | 3 | SKIP | 错误消息不含排序后的 provider 列表 | 可用列表排序展示 |
| 6 | `TestATDD_23_3_AC4_EmptyProviderUsesDefault` | 4 | SKIP | 行为已存在(回归守护) | 空 provider → `/dev/llm/claude` |
| 7 | `TestATDD_23_3_AC5_CLIProviderOverridesAgent` | 5 | SKIP | "groq" 不在硬编码白名单 | CLI 覆盖 agent provider |
| 8 | `TestATDD_23_3_AC5_OverridePrecedence` | 5 | SKIP | agent="ollama" 不在白名单 | override 优先于 agent |

### Integration Tests (通过 Spawn 验证端到端行为)

| # | Test Function | AC | Status | Failure Reason | Verifies |
|---|---------------|-----|--------|----------------|----------|
| 9 | `TestATDD_23_3_AC2_RegisteredProviderReturnsCorrectPath` | 2 | SKIP | `resolveLLMDevice` 拒绝 "ollama" | Spawn + VFS 完整路径 |
| 10 | `TestATDD_23_3_AC3_SpawnReturnsDriverError` | 3 | SKIP | Spawn 错误不含可用列表 | Spawn 错误信息质量 |
| 11 | `TestATDD_23_3_AC4_DefaultThroughSpawn` | 4 | SKIP | 行为已存在(回归守护) | Spawn 空 provider → claude |
| 12 | `TestATDD_23_3_AC5_CLIOverrideThroughSpawn` | 5 | SKIP | `resolveLLMDevice` 拒绝 "groq" | Spawn + CLI override 端到端 |

**Total:** 12 test functions, 5 AC 全覆盖

## AC 覆盖矩阵

| AC | Unit Tests | Integration Tests | Coverage |
|----|-----------|-------------------|----------|
| AC1 | #1, #2 | — | ✓ 动态解析 + 向后兼容 |
| AC2 | #3 | #9 | ✓ 直接解析 + Spawn 端到端 |
| AC3 | #4, #5 | #10 | ✓ 错误格式 + 排序 + Spawn 错误 |
| AC4 | #6 | #11 | ✓ 默认值 + Spawn 默认 |
| AC5 | #7, #8 | #12 | ✓ CLI 覆盖 + 优先级 + Spawn |

## Implementation Checklist

| Task | Files | Tests |
|------|-------|-------|
| 定义 `providerNames`/`hasProvider` 字段 | `kernel/kernel.go` (KernelImpl) | #1, #2 |
| 实现 `SetProviderResolver` setter | `kernel/kernel.go` | All |
| 删除 `allowedLLMProviders` map | `kernel/kernel.go` L169-174 | #1, #3, #7 |
| 重写 `resolveLLMDevice` 为方法 | `kernel/kernel.go` L176-191 | All unit tests |
| 更新 `Spawn` 调用 `k.resolveLLMDevice` | `kernel/kernel.go` L393 | All integration tests |
| 注入回调到 `runDaemon` | `cmd/rnix/main.go` | 手动验证 |
| 更新 `--provider` 帮助文本 | `cmd/rnix/main.go` | — |
| 更新现有 `resolveLLMDevice` 测试 | `kernel/kernel_test.go` L2466-2597 | 重构为方法调用 |

## Running Tests

```bash
# Run all ATDD tests for story 23-3
go test -race -run TestATDD_23_3 ./kernel/...

# Run specific AC tests
go test -race -v -run TestATDD_23_3_AC1 ./kernel/...
go test -race -v -run TestATDD_23_3_AC2 ./kernel/...
go test -race -v -run TestATDD_23_3_AC3 ./kernel/...
go test -race -v -run TestATDD_23_3_AC4 ./kernel/...
go test -race -v -run TestATDD_23_3_AC5 ./kernel/...

# Run with verbose output (shows skip messages)
go test -race -v -run TestATDD_23_3 ./kernel/...

# Run ALL kernel tests (existing + ATDD)
go test -race ./kernel/...
```

## Red-Green-Refactor Workflow

### RED Phase (当前状态) ✅

所有 12 个 ATDD 测试已创建，使用 `t.Skip()` 标记为跳过。
编译通过，运行时显示 SKIP 状态。

### GREEN Phase (实现时)

1. 在 `KernelImpl` 中添加 `providerNames`/`hasProvider` 字段
2. 实现 `SetProviderResolver` 方法
3. 删除 `allowedLLMProviders` 硬编码 map
4. 重写 `resolveLLMDevice` 为 `KernelImpl` 方法
5. 更新 ATDD 测试：
   - 将 `resolveLLMDevice(agent, override)` → `k.resolveLLMDevice(agent, override)`
   - 在需要的测试中注入 `SetProviderResolver`
   - 移除 `t.Skip()`
6. 更新 `kernel_test.go` 中现有的 8 个 `resolveLLMDevice` 测试
7. 在 `cmd/rnix/main.go` 中注入回调

### REFACTOR Phase

- 确认所有测试 PASS（`go test -race ./kernel/...`）
- 确认无 lint 错误（`make lint`）
- 确认现有测试无回归（`make test`）

## Next Steps

1. **开始实现 Story 23.3**：按照 `23-3-dynamic-provider-resolution.md` 中的 Task 顺序
2. **逐步移除 `t.Skip()`**：每完成一个 Task，尝试移除对应 AC 的 skip
3. **全量测试**：`make all` 确保无回归
4. **Code Review**：关注 `resolveLLMDevice` 签名变更对调用方的影响
