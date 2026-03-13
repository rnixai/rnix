---
title: '支持通过配置指定默认 Provider'
slug: 'default-provider-config'
created: '2026-03-13'
status: 'done'
stepsCompleted: [1, 2, 3, 4]
tech_stack: [Go, YAML]
files_to_modify:
  - drivers/llm/config.go
  - drivers/llm/config_test.go
  - kernel/kernel.go
  - kernel/kernel_test.go
  - cmd/rnix/main.go
code_patterns:
  - SetProviderResolver callback injection
  - ProvidersConfig struct + Validate()
  - resolveLLMDevice() provider resolution chain
test_patterns:
  - Table-driven tests
  - ATDD Given/When/Then
---

# Tech-Spec: 支持通过配置指定默认 Provider

**Created:** 2026-03-13

## Overview

### Problem Statement

当前 `kernel.resolveLLMDevice()` 在调用者和 Agent manifest 均未指定 provider 时，硬编码回退到 `"claude"`（`kernel/kernel.go:187-188`）。用户配置了多个 provider 后，无法通过配置声明"当没有明确指定 provider 时默认使用哪个"。

### Solution

在 `rnix-providers.yaml` 顶层增加 `default_provider` 字段。kernel 从配置中读取默认 provider 名称，替代硬编码的 `"claude"`。当 `default_provider` 未设置时，自动取 `providers` 列表中的第一个，确保向后兼容。

### Scope

**In Scope:**

- `ProvidersConfig` 增加 `DefaultProvider` 字段及配置校验
- `KernelImpl` 新增 `defaultProvider` 字段 + `SetDefaultProvider()` 方法
- `resolveLLMDevice()` 使用注入的默认值替代硬编码
- `runDaemon()` 接线：从配置中解析并注入到 kernel
- 单元测试全覆盖

**Out of Scope:**

- CLI flag 覆盖默认 provider（`--default-provider`）
- 环境变量覆盖（`RNIX_DEFAULT_PROVIDER`）
- 运行时动态切换默认 provider

## Context for Development

### Codebase Patterns

- **Callback 注入模式**：kernel 不直接依赖 `llm` 包，通过 `SetProviderResolver(names, has)` 注入回调。默认 provider 也应遵循此模式，通过 setter 注入一个 `string` 值。
- **Config Validate 模式**：`ProvidersConfig.Validate()` 收集所有错误后通过 `errors.Join` 一次性返回。
- **DefaultProvidersConfig()**：内置默认配置返回 claude + cursor，第一个是 claude，与当前硬编码 `"claude"` 行为一致。

### Files to Reference

| File | Purpose |
| ---- | ------- |
| `drivers/llm/config.go:29-41` | `ProvidersConfig` / `ProviderConfig` 结构体定义 |
| `drivers/llm/config.go:91-122` | `Validate()` 校验逻辑 |
| `drivers/llm/config.go:124-144` | `DefaultProvidersConfig()` / `LoadOrDefaultProvidersConfig()` |
| `kernel/kernel.go:150-152` | `providerNames` / `hasProvider` 回调字段 |
| `kernel/kernel.go:177-203` | `resolveLLMDevice()` — 需修改的核心函数 |
| `kernel/kernel.go:1592-1595` | `SetProviderResolver()` — 注入模式参考 |
| `cmd/rnix/main.go:1072-1116` | `runDaemon()` — 配置加载和 kernel 初始化 |
| `drivers/llm/config_test.go` | 现有配置测试 |
| `kernel/kernel_test.go:2505+` | 现有 `resolveLLMDevice` 测试 |

### Technical Decisions

1. **全局字段 vs Provider 内标记**：选择全局 `default_provider` 字段。理由：清晰度高（一眼可见）、修改成本低（改一个字段）、无冲突问题、与 kubeconfig `current-context` 模式一致。
2. **兜底规则**：`default_provider` 为空时取 `providers[0].Name`，而非保持硬编码 `"claude"`。这样即使用户只配了一个 `groq` provider 且没写 `default_provider`，行为也是正确的。
3. **注入方式**：新增 `SetDefaultProvider(name string)` 方法，与现有 `SetProviderResolver` 风格保持一致。

## Implementation Plan

### Tasks

**Task 1: `ProvidersConfig` 增加 `DefaultProvider` 字段** (`drivers/llm/config.go`)

1. 在 `ProvidersConfig` 结构体增加字段：
   ```go
   type ProvidersConfig struct {
       Version         string           `yaml:"version"`
       DefaultProvider string           `yaml:"default_provider,omitempty"`
       Providers       []ProviderConfig `yaml:"providers"`
   }
   ```

2. 在 `Validate()` 中增加校验 — 如果 `DefaultProvider` 非空，必须存在于 providers 列表中：
   ```go
   if c.DefaultProvider != "" {
       found := false
       for _, p := range c.Providers {
           if p.Name == c.DefaultProvider {
               found = true
               break
           }
       }
       if !found {
           errs = append(errs, fmt.Errorf("default_provider %q not found in providers list", c.DefaultProvider))
       }
   }
   ```

3. 新增 `ResolveDefaultProvider()` 方法：
   ```go
   func (c *ProvidersConfig) ResolveDefaultProvider() string {
       if c.DefaultProvider != "" {
           return c.DefaultProvider
       }
       if len(c.Providers) > 0 {
           return c.Providers[0].Name
       }
       return "claude"
   }
   ```
   最后的 `"claude"` 是终极兜底，正常不会到达（`Validate()` 已要求至少一个 provider）。

**Task 2: 配置测试** (`drivers/llm/config_test.go`)

1. 测试 `default_provider` YAML 解析
2. 测试 `Validate()` — `default_provider` 引用不存在的名称时报错
3. 测试 `Validate()` — `default_provider` 为空时通过
4. 测试 `ResolveDefaultProvider()` — 有值时返回该值
5. 测试 `ResolveDefaultProvider()` — 空值时返回 `providers[0].Name`
6. 测试 `DefaultProvidersConfig().ResolveDefaultProvider()` 返回 `"claude"`

**Task 3: Kernel 注入默认 provider** (`kernel/kernel.go`)

1. 在 `KernelImpl` 结构体增加字段：
   ```go
   defaultProvider string // injected by daemon; "" = "claude" (backward compat)
   ```

2. 新增 setter 方法：
   ```go
   func (k *KernelImpl) SetDefaultProvider(name string) {
       k.defaultProvider = name
   }
   ```

3. 修改 `resolveLLMDevice()` 第 187-188 行：
   ```go
   // Before:
   if provider == "" {
       provider = "claude"
   }

   // After:
   if provider == "" {
       if k.defaultProvider != "" {
           provider = k.defaultProvider
       } else {
           provider = "claude"
       }
   }
   ```

**Task 4: Kernel 测试** (`kernel/kernel_test.go`)

1. `TestSetDefaultProvider` — 验证 setter 赋值
2. `TestResolveLLMDevice_UsesDefaultProvider` — 设置 `defaultProvider` 后，空 provider 使用它
3. `TestResolveLLMDevice_DefaultProviderOverriddenByAgent` — agent manifest provider 优先于 `defaultProvider`
4. `TestResolveLLMDevice_DefaultProviderOverriddenBySpawnOpts` — `SpawnOpts.Provider` 优先于一切
5. `TestResolveLLMDevice_NoDefaultProvider_FallsBackToClaude` — 未设置时保持 `"claude"` 兼容

**Task 5: Daemon 接线** (`cmd/rnix/main.go`)

在 `runDaemon()` 的 `k.SetProviderResolver(...)` 之后增加一行：
```go
k.SetDefaultProvider(providersCfg.ResolveDefaultProvider())
```

### Acceptance Criteria

**AC1: 配置解析**

Given `rnix-providers.yaml` 包含 `default_provider: groq`
When daemon 启动解析配置
Then `ProvidersConfig.DefaultProvider` 值为 `"groq"`

**AC2: 配置校验 — 无效引用**

Given `default_provider` 引用了 providers 列表中不存在的名称
When 调用 `Validate()`
Then 返回错误信息包含 `"not found in providers list"`

**AC3: 兜底规则 — 未设置时取第一个**

Given `default_provider` 字段为空
And providers 列表第一个是 `"groq"`
When 调用 `ResolveDefaultProvider()`
Then 返回 `"groq"`

**AC4: Kernel 使用默认 provider**

Given kernel 通过 `SetDefaultProvider("groq")` 注入了默认值
And spawn 时调用者和 agent manifest 均未指定 provider
When `resolveLLMDevice()` 被调用
Then 返回 `"/dev/llm/groq"`

**AC5: 优先级链正确**

Given `defaultProvider = "groq"`
And agent manifest `models.provider = "ollama"`
And `SpawnOpts.Provider = "claude"`
When `resolveLLMDevice()` 被调用
Then 使用 `SpawnOpts.Provider`（`"claude"` → `/dev/llm/claude`）

Given `defaultProvider = "groq"`
And agent manifest `models.provider = "ollama"`
And `SpawnOpts.Provider = ""`
When `resolveLLMDevice()` 被调用
Then 使用 agent manifest（`"ollama"` → `/dev/llm/ollama`）

**AC6: 向后兼容**

Given 未调用 `SetDefaultProvider()`
And 调用者和 agent manifest 均未指定 provider
When `resolveLLMDevice()` 被调用
Then 返回 `"/dev/llm/claude"`（与当前行为一致）

## Additional Context

### Dependencies

无新外部依赖。

### Testing Strategy

- **单元测试**：Table-driven 测试覆盖配置解析、校验、`ResolveDefaultProvider()`、kernel `resolveLLMDevice()` 优先级链
- **现有测试兼容**：`defaultProvider` 零值为空字符串，回退到 `"claude"`，所有现有测试无需修改

### Provider 解析优先级（完整链路）

```
SpawnOpts.Provider（调用者显式指定）
  ↓ 为空
Agent Manifest models.provider（agent.yaml 配置）
  ↓ 为空
KernelImpl.defaultProvider（从 rnix-providers.yaml 注入）
  ↓ 为空（未调用 SetDefaultProvider）
硬编码 "claude"（终极兜底，向后兼容）
```

### Notes

- Compose 引擎无需修改。compose `engine.go:187-191` 解析到空 provider 后传给 kernel spawn，最终由 kernel 的 `resolveLLMDevice()` 兜底，自动受益。
- `rnix serve`（OpenAI 兼容网关）的 HTTP 端不受影响 — 它走的是 `model` 字段显式路由到 provider。
