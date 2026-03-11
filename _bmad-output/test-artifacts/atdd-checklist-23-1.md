---
stepsCompleted: ['step-01-preflight-and-context', 'step-02-generation-mode', 'step-03-test-strategy', 'step-04-generate-tests']
lastStep: 'step-04-generate-tests'
lastSaved: '2026-03-11'
workflowType: 'testarch-atdd'
inputDocuments:
  - '_bmad-output/implementation-artifacts/23-1-providers-yaml-config-parsing.md'
---

# ATDD Checklist - Epic 23, Story 1: rnix-providers.yaml 配置文件定义与解析

**Date:** 2026-03-11
**Primary Test Level:** Unit (Go backend)

---

## Story Summary

Story 23.1 实现 `rnix-providers.yaml` 配置文件的定义与解析。定义 `ProvidersConfig` / `ProviderConfig` 结构体、三种驱动类型常量、配置文件查找 / 加载 / 验证 / 默认回退函数。

**As a** 用户
**I want** 通过 `rnix-providers.yaml` 配置文件定义 LLM provider
**So that** 新增 provider 无需修改源码，仅需编辑配置文件

---

## Acceptance Criteria

1. **AC1 - 正确解析:** Given 项目根目录存在 rnix-providers.yaml, When daemon 启动时解析配置文件, Then 正确识别每个 provider 的 driver 类型（claude-cli / cursor-cli / openai-compat）And 正确读取 default_model、base_url、api_key_env 字段
2. **AC2 - 错误处理:** Given 配置文件格式错误（YAML 语法错误或缺少必填字段）, When daemon 启动, Then 以明确错误信息拒绝启动，指出具体的格式问题和行号
3. **AC3 - 默认回退:** Given 配置文件不存在, When daemon 启动, Then 回退到默认配置（仅注册 claude 和 cursor provider），日志输出提示使用默认配置
4. **AC4 - 性能:** Given ≤ 10 个 provider 配置, When 解析完成, Then 解析耗时 ≤ 2 秒

---

## Failing Tests Created (RED Phase)

### Unit Tests (25 tests)

**File:** `drivers/llm/config_test.go` (约 430 行)

#### AC1 — 正确解析 (6 tests)

- RED **Test:** TestProviderConfig_DriverConstants
  - **Status:** RED — `DriverClaudeCLI`, `DriverCursorCLI`, `DriverOpenAICompat` 常量未定义
  - **Verifies:** AC#1 — 三种驱动类型常量值正确

- RED **Test:** TestProvidersConfig_YAMLUnmarshal
  - **Status:** RED — `ProvidersConfig` 类型和 `LoadProvidersConfig` 函数未定义
  - **Verifies:** AC#1 — 完整 YAML 正确反序列化到结构体

- RED **Test:** TestLoadProvidersConfig_ValidFile
  - **Status:** RED — `LoadProvidersConfig` 函数未定义
  - **Verifies:** AC#1 — 三种驱动类型的全部字段（name、driver、default_model、base_url、api_key_env）正确解析

- RED **Test:** TestLoadProvidersConfig_MinimalFile
  - **Status:** RED — `LoadProvidersConfig` 函数未定义
  - **Verifies:** AC#1 — 仅必填字段的最小配置，可选字段为零值

- RED **Test:** TestLoadProvidersConfig_MultipleProviders
  - **Status:** RED — `LoadProvidersConfig` 函数未定义
  - **Verifies:** AC#1 — 10 个 provider 全部正确加载

- RED **Test:** TestProviderConfig_Validate_CLIDriverIgnoresBaseURL
  - **Status:** RED — `ProvidersConfig` 和 `Validate` 未定义
  - **Verifies:** AC#1 — claude-cli/cursor-cli 驱动不要求 base_url

#### AC2 — 错误处理 (8 tests + 9 table-driven sub-tests)

- RED **Test:** TestLoadProvidersConfig_YAMLSyntaxError
  - **Status:** RED — `LoadProvidersConfig` 函数未定义
  - **Verifies:** AC#2 — YAML 语法错误时返回包含行号的错误信息

- RED **Test:** TestLoadProvidersConfig_MissingName
  - **Status:** RED — `LoadProvidersConfig` 函数未定义
  - **Verifies:** AC#2 — provider 缺少 name 字段时返回明确错误

- RED **Test:** TestLoadProvidersConfig_InvalidDriver
  - **Status:** RED — `LoadProvidersConfig` 函数未定义
  - **Verifies:** AC#2 — driver 值不合法时返回错误并提示有效值

- RED **Test:** TestLoadProvidersConfig_MissingBaseURL
  - **Status:** RED — `LoadProvidersConfig` 函数未定义
  - **Verifies:** AC#2 — openai-compat 驱动缺少 base_url 时返回错误

- RED **Test:** TestLoadProvidersConfig_DuplicateNames
  - **Status:** RED — `LoadProvidersConfig` 函数未定义
  - **Verifies:** AC#2 — 重复 provider 名称时返回错误

- RED **Test:** TestLoadProvidersConfig_InvalidNameChars
  - **Status:** RED — `LoadProvidersConfig` 函数未定义
  - **Verifies:** AC#2 — 名称包含非法字符时返回错误

- RED **Test:** TestLoadProvidersConfig_EmptyProviders
  - **Status:** RED — `LoadProvidersConfig` 函数未定义
  - **Verifies:** AC#2 — providers 列表为空时返回错误

- RED **Test:** TestLoadProvidersConfig_MultipleErrors
  - **Status:** RED — `LoadProvidersConfig` 函数未定义
  - **Verifies:** AC#2 — 多个验证错误一次性全部收集并返回

- RED **Test:** TestValidate_TableDriven (9 sub-tests)
  - **Status:** RED — `ProvidersConfig`, `ProviderConfig`, `Validate` 未定义
  - **Verifies:** AC#2 — 覆盖 valid/invalid 各种场景的验证逻辑
  - **Sub-tests:** valid claude-cli, valid openai-compat with base_url, empty providers, nil providers, missing name, invalid driver, openai-compat missing base_url, duplicate names, invalid name characters

#### AC3 — 默认回退 (4 tests)

- RED **Test:** TestDefaultProvidersConfig
  - **Status:** RED — `DefaultProvidersConfig` 函数未定义
  - **Verifies:** AC#3 — 默认配置包含 claude (claude-cli, haiku) 和 cursor (cursor-cli)

- RED **Test:** TestLoadOrDefault_NoFile
  - **Status:** RED — `LoadOrDefaultProvidersConfig` 函数未定义
  - **Verifies:** AC#3 — 配置文件不存在时返回默认配置

- RED **Test:** TestLoadOrDefault_ValidFile
  - **Status:** RED — `LoadOrDefaultProvidersConfig` 函数未定义
  - **Verifies:** AC#3 — 配置文件存在时返回文件配置（非默认）

- RED **Test:** TestLoadOrDefault_InvalidFile
  - **Status:** RED — `LoadOrDefaultProvidersConfig` 函数未定义
  - **Verifies:** AC#3 — 配置文件存在但格式错误时返回 error

#### AC4 — 性能 (1 test)

- RED **Test:** TestLoadProvidersConfig_Performance
  - **Status:** RED — `LoadProvidersConfig` 函数未定义
  - **Verifies:** AC#4 — 10 个 provider 配置解析耗时 ≤ 2 秒

#### 配置查找 (4 tests)

- RED **Test:** TestFindProvidersConfigPath_CWD
  - **Status:** RED — `FindProvidersConfigPath` 和 `ProvidersConfigFile` 未定义
  - **Verifies:** AC#1/AC#3 — 当前目录存在配置文件时返回路径

- RED **Test:** TestFindProvidersConfigPath_XDGConfig
  - **Status:** RED — `FindProvidersConfigPath` 和 `ProvidersConfigFile` 未定义
  - **Verifies:** AC#1/AC#3 — $XDG_CONFIG_HOME/rnix/ 下存在配置文件时返回路径

- RED **Test:** TestFindProvidersConfigPath_Precedence
  - **Status:** RED — `FindProvidersConfigPath` 和 `ProvidersConfigFile` 未定义
  - **Verifies:** AC#1/AC#3 — CWD 优先于 XDG 路径

- RED **Test:** TestFindProvidersConfigPath_NotFound
  - **Status:** RED — `FindProvidersConfigPath` 和 `ProvidersConfigFile` 未定义
  - **Verifies:** AC#3 — 两处都不存在时返回空字符串

#### 边界情况 (2 tests)

- RED **Test:** TestLoadProvidersConfig_FileNotFound
  - **Status:** RED — `LoadProvidersConfig` 函数未定义
  - **Verifies:** 指定路径文件不存在时返回 error

- RED **Test:** TestLoadProvidersConfig_EmptyFile
  - **Status:** RED — `LoadProvidersConfig` 函数未定义
  - **Verifies:** 空文件时返回错误

---

## AC ↔ Test 覆盖矩阵

| AC | Test(s) | 覆盖方式 |
|----|---------|----------|
| AC1 正确解析 | TestProviderConfig_DriverConstants, TestProvidersConfig_YAMLUnmarshal, TestLoadProvidersConfig_ValidFile, TestLoadProvidersConfig_MinimalFile, TestLoadProvidersConfig_MultipleProviders, TestProviderConfig_Validate_CLIDriverIgnoresBaseURL | 常量验证 + 字段完整性 + 最小配置 + 多 provider |
| AC2 错误处理 | TestLoadProvidersConfig_YAMLSyntaxError, TestLoadProvidersConfig_MissingName, TestLoadProvidersConfig_InvalidDriver, TestLoadProvidersConfig_MissingBaseURL, TestLoadProvidersConfig_DuplicateNames, TestLoadProvidersConfig_InvalidNameChars, TestLoadProvidersConfig_EmptyProviders, TestLoadProvidersConfig_MultipleErrors, TestValidate_TableDriven | 语法错误行号 + 缺失字段 + 非法值 + 重复名称 + 非法字符 + 空列表 + 多错误收集 |
| AC3 默认回退 | TestDefaultProvidersConfig, TestLoadOrDefault_NoFile, TestLoadOrDefault_ValidFile, TestLoadOrDefault_InvalidFile | 默认配置内容 + 文件不存在回退 + 文件存在加载 + 无效文件报错 |
| AC4 性能 | TestLoadProvidersConfig_Performance | 10 provider ≤ 2s |

---

## 测试隔离策略

- **临时文件:** 所有 YAML 文件通过 `t.TempDir()` + `writeYAML` helper 创建，测试完自动清理
- **环境变量:** `t.Setenv("XDG_CONFIG_HOME", ...)` 隔离 XDG 路径
- **工作目录:** 需要 CWD 的测试通过 `os.Chdir` + `t.Cleanup` 恢复
- **并行安全:** 所有测试标记 `t.Parallel()`（注意 CWD 测试存在全局状态，但使用独立 TempDir 隔离）
- **Race 检测:** 通过 `go test -race` 运行

---

## 实现目标文件

| 文件 | 状态 | 说明 |
|------|------|------|
| `drivers/llm/config.go` | 待创建 | ProvidersConfig/ProviderConfig 结构体、驱动常量、Load/Find/Default/Validate 函数 |
| `drivers/llm/config_test.go` | 已创建 (RED) | 25 个测试，全部因 config.go 不存在而编译失败 |

---

## 下一步

1. 实现 `drivers/llm/config.go` 使所有测试编译通过
2. 运行 `go test -race ./drivers/llm/...` 验证全部 GREEN
3. 运行 `go vet` + `golangci-lint` 确保代码质量
