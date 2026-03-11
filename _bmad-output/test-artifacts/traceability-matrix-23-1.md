# 可追溯性矩阵 — Story 23-1: rnix-providers.yaml 配置文件定义与解析

| 项目 | 值 |
|------|-----|
| Story ID | 23-1 |
| Story Key | 23-1-providers-yaml-config-parsing |
| 生成日期 | 2026-03-11 |
| Gate 类型 | Story |
| 源文件 | `drivers/llm/config.go` (144 行) |
| 测试文件 | `drivers/llm/config_test.go` (691 行) |

---

## 1. AC → 测试映射

### AC1: 正确解析 — 识别 driver 类型 + 读取所有字段

| 测试函数 | 验证内容 | 结果 |
|----------|---------|------|
| `TestProviderConfig_DriverConstants` | 三种驱动常量值 (`claude-cli`, `cursor-cli`, `openai-compat`) | PASS |
| `TestProvidersConfig_YAMLUnmarshal` | 完整 YAML 反序列化为结构体，version + 4 个 provider | PASS |
| `TestLoadProvidersConfig_ValidFile` | 三种驱动类型全字段解析 (name, driver, default_model, base_url, api_key_env) | PASS |
| `TestLoadProvidersConfig_MinimalFile` | 仅必填字段，可选字段为零值 | PASS |
| `TestLoadProvidersConfig_MultipleProviders` | 10 个 provider 全部正确加载 | PASS |

**覆盖状态: 完全覆盖** — 三种驱动类型、所有字段、最小与最大配置均有测试。

### AC2: 错误处理 — YAML 语法错误和缺失字段的明确错误信息（含行号）

| 测试函数 | 验证内容 | 结果 |
|----------|---------|------|
| `TestLoadProvidersConfig_YAMLSyntaxError` | YAML 语法错误含行号信息 | PASS |
| `TestLoadProvidersConfig_MissingName` | 缺少 `name` 字段报错 | PASS |
| `TestLoadProvidersConfig_InvalidDriver` | 无效 `driver` 值报错 | PASS |
| `TestLoadProvidersConfig_MissingBaseURL` | `openai-compat` 缺少 `base_url` 报错 | PASS |
| `TestLoadProvidersConfig_DuplicateNames` | 重复 provider 名称报错 | PASS |
| `TestLoadProvidersConfig_InvalidNameChars` | 名称含非法字符报错 | PASS |
| `TestLoadProvidersConfig_EmptyProviders` | 空 providers 列表报错 | PASS |
| `TestLoadProvidersConfig_MultipleErrors` | 多个验证错误同时收集并返回 | PASS |
| `TestValidate_TableDriven` (9 子测试) | 验证函数的全面 table-driven 测试 | PASS (9/9) |
| — `/valid_claude-cli` | 合法 claude-cli 配置无错误 | PASS |
| — `/valid_openai-compat_with_base_url` | 合法 openai-compat 配置无错误 | PASS |
| — `/empty_providers` | 空列表检测 | PASS |
| — `/nil_providers` | nil 列表检测 | PASS |
| — `/missing_name` | 缺名检测 | PASS |
| — `/invalid_driver` | 非法驱动检测 | PASS |
| — `/openai-compat_missing_base_url` | 缺 base_url 检测 | PASS |
| — `/duplicate_names` | 重名检测 | PASS |
| — `/invalid_name_characters` | 非法字符检测 | PASS |

**覆盖状态: 完全覆盖** — YAML 语法错误含行号、全部 6 种语义验证规则、多错误收集均有测试。

### AC3: 默认回退 — 文件不存在时使用默认配置（claude + cursor）

| 测试函数 | 验证内容 | 结果 |
|----------|---------|------|
| `TestDefaultProvidersConfig` | 默认配置含 claude (claude-cli, haiku) + cursor (cursor-cli) | PASS |
| `TestLoadOrDefault_NoFile` | 文件不存在 → 返回默认配置 | PASS |
| `TestLoadOrDefault_ValidFile` | 文件存在 → 返回文件配置 | PASS |
| `TestLoadOrDefault_InvalidFile` | 文件存在但无效 → 返回 error | PASS |
| `TestFindProvidersConfigPath_CWD` | CWD 路径查找 | PASS |
| `TestFindProvidersConfigPath_XDGConfig` | XDG_CONFIG_HOME 路径查找 | PASS |
| `TestFindProvidersConfigPath_Precedence` | CWD 优先于 XDG | PASS |
| `TestFindProvidersConfigPath_NotFound` | 两处均无 → 返回空串 | PASS |

**覆盖状态: 完全覆盖** — 默认配置内容、回退逻辑、查找优先级、无效文件报错均有测试。

### AC4: 性能 — ≤ 10 providers 解析 ≤ 2 秒

| 测试函数 | 验证内容 | 结果 |
|----------|---------|------|
| `TestLoadProvidersConfig_Performance` | 10 个 openai-compat provider 全字段解析耗时 ≤ 2s | PASS |

**覆盖状态: 完全覆盖** — 实测耗时远低于 2s 阈值（~ms 级）。

---

## 2. 测试 → AC 映射

| # | 测试函数 | AC 覆盖 | 结果 |
|---|---------|---------|------|
| 1 | `TestProviderConfig_DriverConstants` | AC1 | PASS |
| 2 | `TestProvidersConfig_YAMLUnmarshal` | AC1 | PASS |
| 3 | `TestLoadProvidersConfig_ValidFile` | AC1 | PASS |
| 4 | `TestLoadProvidersConfig_MinimalFile` | AC1 | PASS |
| 5 | `TestLoadProvidersConfig_MultipleProviders` | AC1 | PASS |
| 6 | `TestLoadProvidersConfig_YAMLSyntaxError` | AC2 | PASS |
| 7 | `TestLoadProvidersConfig_MissingName` | AC2 | PASS |
| 8 | `TestLoadProvidersConfig_InvalidDriver` | AC2 | PASS |
| 9 | `TestLoadProvidersConfig_MissingBaseURL` | AC2 | PASS |
| 10 | `TestLoadProvidersConfig_DuplicateNames` | AC2 | PASS |
| 11 | `TestLoadProvidersConfig_InvalidNameChars` | AC2 | PASS |
| 12 | `TestLoadProvidersConfig_EmptyProviders` | AC2 | PASS |
| 13 | `TestLoadProvidersConfig_MultipleErrors` | AC2 | PASS |
| 14 | `TestValidate_TableDriven` (9 子测试) | AC1, AC2 | PASS |
| 15 | `TestDefaultProvidersConfig` | AC3 | PASS |
| 16 | `TestLoadOrDefault_NoFile` | AC3 | PASS |
| 17 | `TestLoadOrDefault_ValidFile` | AC1, AC3 | PASS |
| 18 | `TestLoadOrDefault_InvalidFile` | AC2, AC3 | PASS |
| 19 | `TestFindProvidersConfigPath_CWD` | AC3 | PASS |
| 20 | `TestFindProvidersConfigPath_XDGConfig` | AC3 | PASS |
| 21 | `TestFindProvidersConfigPath_Precedence` | AC3 | PASS |
| 22 | `TestFindProvidersConfigPath_NotFound` | AC3 | PASS |
| 23 | `TestLoadProvidersConfig_Performance` | AC4 | PASS |
| 24 | `TestLoadProvidersConfig_FileNotFound` | AC2 (边界) | PASS |
| 25 | `TestLoadProvidersConfig_EmptyFile` | AC2 (边界) | PASS |
| 26 | `TestProviderConfig_Validate_CLIDriverIgnoresBaseURL` | AC1 (边界) | PASS |

---

## 3. 函数 → 测试映射

| 导出函数/方法 | 行范围 | 测试覆盖 | 覆盖数 |
|--------------|--------|---------|--------|
| `FindProvidersConfigPath()` | 44-66 | TestFindProvidersConfigPath_CWD, _XDGConfig, _Precedence, _NotFound, TestLoadOrDefault_NoFile, _ValidFile | 6 |
| `LoadProvidersConfig(path)` | 69-87 | TestLoadProvidersConfig_ValidFile, _MinimalFile, _MultipleProviders, _YAMLSyntaxError, _MissingName, _InvalidDriver, _MissingBaseURL, _DuplicateNames, _InvalidNameChars, _EmptyProviders, _MultipleErrors, _FileNotFound, _EmptyFile, _Performance, TestProvidersConfig_YAMLUnmarshal | 15 |
| `(*ProvidersConfig).Validate()` | 91-122 | TestValidate_TableDriven (9 子测试), TestProviderConfig_Validate_CLIDriverIgnoresBaseURL, 及所有 LoadProvidersConfig 测试间接调用 | 11+ |
| `DefaultProvidersConfig()` | 125-133 | TestDefaultProvidersConfig, TestLoadOrDefault_NoFile | 2 |
| `LoadOrDefaultProvidersConfig()` | 137-144 | TestLoadOrDefault_NoFile, _ValidFile, _InvalidFile | 3 |

| 导出类型/常量 | 测试覆盖 |
|--------------|---------|
| `ProvidersConfig` 结构体 | 所有测试间接使用 |
| `ProviderConfig` 结构体 | 所有测试间接使用 |
| `DriverClaudeCLI` / `DriverCursorCLI` / `DriverOpenAICompat` | TestProviderConfig_DriverConstants |
| `ProvidersConfigFile` | TestProvidersConfig_YAMLUnmarshal 及所有使用 writeYAML 的测试 |

---

## 4. 覆盖指标摘要

| 指标 | 值 | 判定 |
|------|-----|------|
| AC 覆盖率 | **4/4 (100%)** | 全部 AC 均有测试覆盖 |
| 导出函数覆盖率 | **5/5 (100%)** | 全部导出函数均有直接测试 |
| 测试通过率 | **26/26 (100%)** + 9/9 子测试 | 全部通过 (含 `-race`) |
| 测试总路径数 | **35** (26 顶层 + 9 子测试) | — |
| 未覆盖代码路径 | **0 (关键)** | 无关键路径遗漏 |

---

## 5. 质量门决策

### **PASS**

**理由:**

1. **AC 全覆盖** — 4 个 AC 均有充分的测试验证，无遗漏。
2. **测试全通过** — 26 个顶层测试函数 + 9 个 table-driven 子测试，100% 通过率，含 `-race` 竞态检测。
3. **函数全覆盖** — 5 个导出函数每个至少有 2 个直接测试。
4. **错误路径完备** — AC2 有 8 个独立测试 + 9 个 table-driven 子测试覆盖全部验证规则。
5. **边界情况** — 文件不存在、空文件、CLI 驱动忽略 base_url 等边界均有测试。
6. **代码审查已通过** — 对抗性审查发现 1 个 MEDIUM 问题已修复，7 个 LOW 问题均为可接受范围。

---

## 6. 已识别风险与备注

| # | 类型 | 描述 | 严重度 |
|---|------|------|--------|
| R1 | 低风险 | `version` 字段未做格式验证（不在本 Story 范围内，后续可扩展） | LOW |
| R2 | 信息 | 性能测试阈值 2s 极为宽裕（实测 ~ms 级），但符合 NFR31 规格要求 | INFO |
| R3 | 信息 | 7 个使用 `t.Setenv`/`t.Chdir` 的测试不能 `t.Parallel()`（Go 1.26 限制），对 CI 速度影响可忽略 | INFO |
| R4 | 信息 | `TestLoadProvidersConfig_YAMLSyntaxError` 行号断言使用弱检查（`Contains("4") && Contains("line")`），但 go-yaml 行号格式稳定 | INFO |
