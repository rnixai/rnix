# Story 23.1: rnix-providers.yaml 配置文件定义与解析

Status: done

## Story

As a 用户,
I want 通过 `rnix-providers.yaml` 配置文件定义 LLM provider,
So that 新增 provider 无需修改源码，仅需编辑配置文件。

## Acceptance Criteria

1. **Given** 项目根目录存在 `rnix-providers.yaml`
   **When** daemon 启动时解析配置文件
   **Then** 正确识别每个 provider 的 `driver` 类型（`claude-cli` / `cursor-cli` / `openai-compat`）
   **And** 正确读取 `default_model`、`base_url`（HTTP 驱动）、`api_key_env`（HTTP 驱动）字段

2. **Given** 配置文件格式错误（YAML 语法错误或缺少必填字段）
   **When** daemon 启动
   **Then** 以明确错误信息拒绝启动，指出具体的格式问题和行号

3. **Given** 配置文件不存在
   **When** daemon 启动
   **Then** 回退到默认配置（仅注册 claude 和 cursor provider），日志输出提示使用默认配置

4. **Given** ≤ 10 个 provider 配置
   **When** 解析完成
   **Then** 解析耗时 ≤ 2 秒（NFR31）

## Tasks / Subtasks

### Task 1: 配置结构体定义（AC: #1）

- [x] 1.1 新建 `drivers/llm/config.go`，定义配置结构体：

  ```go
  // ProvidersConfig is the top-level structure of rnix-providers.yaml.
  type ProvidersConfig struct {
      Version   string           `yaml:"version"`
      Providers []ProviderConfig `yaml:"providers"`
  }

  // ProviderConfig describes a single LLM provider entry.
  type ProviderConfig struct {
      Name         string `yaml:"name"`
      Driver       string `yaml:"driver"`        // "claude-cli" | "cursor-cli" | "openai-compat"
      DefaultModel string `yaml:"default_model"`
      BaseURL      string `yaml:"base_url"`      // required for openai-compat
      APIKeyEnv    string `yaml:"api_key_env"`   // env var name for API key (openai-compat)
  }
  ```

- [x] 1.2 定义驱动类型常量：

  ```go
  const (
      DriverClaudeCLI   = "claude-cli"
      DriverCursorCLI   = "cursor-cli"
      DriverOpenAICompat = "openai-compat"
  )
  ```

- [x] 1.3 定义 `validDriverTypes` 集合用于验证：

  ```go
  var validDriverTypes = map[string]bool{
      DriverClaudeCLI:    true,
      DriverCursorCLI:    true,
      DriverOpenAICompat: true,
  }
  ```

### Task 2: 配置文件查找逻辑（AC: #1, #3）

- [x] 2.1 在 `drivers/llm/config.go` 实现 `FindProvidersConfigPath()` 函数：

  ```go
  func FindProvidersConfigPath() string
  ```

  查找顺序：
  1. 当前工作目录 `rnix-providers.yaml`
  2. `$XDG_CONFIG_HOME/rnix/rnix-providers.yaml`（`$XDG_CONFIG_HOME` 默认 `$HOME/.config`）

  返回找到的第一个存在的路径，若都不存在返回空字符串。

- [x] 2.2 配置文件名定义为包级常量：

  ```go
  const ProvidersConfigFile = "rnix-providers.yaml"
  ```

### Task 3: 配置解析与验证（AC: #1, #2）

- [x] 3.1 实现 `LoadProvidersConfig(path string) (*ProvidersConfig, error)` 函数：
  - 读取文件内容（`os.ReadFile`）
  - 使用 `github.com/goccy/go-yaml` 反序列化
  - YAML 解析错误时，利用 `go-yaml` 的错误信息提供行号和具体格式问题
  - 返回解析后的 `*ProvidersConfig`

- [x] 3.2 实现 `(c *ProvidersConfig) Validate() error` 方法，执行语义验证：
  - `providers` 列表不为空
  - 每个 provider 的 `name` 非空
  - 每个 provider 的 `driver` 非空且在 `validDriverTypes` 内
  - `openai-compat` 驱动时 `base_url` 必填
  - provider 名称不重复
  - provider 名称仅包含合法字符（字母、数字、连字符、下划线）
  - 收集所有验证错误后一次性返回（使用 `errors.Join` 或自定义多错误格式），而非遇到第一个错误即停止

- [x] 3.3 `LoadProvidersConfig` 内部调用 `Validate()`，解析成功后自动执行语义验证

### Task 4: 默认配置回退（AC: #3）

- [x] 4.1 实现 `DefaultProvidersConfig() *ProvidersConfig` 函数，返回默认配置：

  ```go
  func DefaultProvidersConfig() *ProvidersConfig {
      return &ProvidersConfig{
          Version: "1",
          Providers: []ProviderConfig{
              {Name: "claude", Driver: DriverClaudeCLI, DefaultModel: "haiku"},
              {Name: "cursor", Driver: DriverCursorCLI},
          },
      }
  }
  ```

  默认配置对应当前 `cmd/rnix/main.go` 第 1061-1066 行的硬编码行为。

- [x] 4.2 实现 `LoadOrDefaultProvidersConfig() (*ProvidersConfig, error)` 便捷函数：
  - 调用 `FindProvidersConfigPath()`
  - 若找到文件则调用 `LoadProvidersConfig(path)` 并返回
  - 若未找到文件则返回 `DefaultProvidersConfig()` 并通过 `log` 包输出提示信息
  - 仅在文件存在但解析/验证失败时返回 error

### Task 5: 测试（全部 AC + NFR31）

- [x] 5.1 `drivers/llm/config_test.go` — 结构体与常量测试：
  - `TestProviderConfig_DriverConstants` — 验证三种驱动类型常量值正确
  - `TestProvidersConfig_YAMLUnmarshal` — 验证完整 YAML 正确反序列化到结构体

- [x] 5.2 `drivers/llm/config_test.go` — 配置加载测试（AC #1）：
  - `TestLoadProvidersConfig_ValidFile` — 完整配置文件（含 claude-cli、cursor-cli、openai-compat 三种驱动），验证所有字段正确解析
  - `TestLoadProvidersConfig_MinimalFile` — 仅包含必填字段的最小配置，验证可选字段为零值
  - `TestLoadProvidersConfig_MultipleProviders` — 多个 provider（≤ 10），验证全部正确加载

- [x] 5.3 `drivers/llm/config_test.go` — 验证错误测试（AC #2）：
  - `TestLoadProvidersConfig_YAMLSyntaxError` — YAML 语法错误（如缺少冒号），验证错误信息包含行号
  - `TestLoadProvidersConfig_MissingName` — provider 缺少 `name` 字段，验证明确的错误信息
  - `TestLoadProvidersConfig_InvalidDriver` — `driver` 字段值不在合法范围内，验证错误信息列出合法值
  - `TestLoadProvidersConfig_MissingBaseURL` — `openai-compat` 驱动缺少 `base_url`，验证错误提示
  - `TestLoadProvidersConfig_DuplicateNames` — 两个 provider 同名，验证重复检测
  - `TestLoadProvidersConfig_InvalidNameChars` — provider 名称包含非法字符
  - `TestLoadProvidersConfig_EmptyProviders` — `providers` 列表为空
  - `TestLoadProvidersConfig_MultipleErrors` — 同时存在多个验证错误，验证全部收集并返回

- [x] 5.4 `drivers/llm/config_test.go` — 默认回退测试（AC #3）：
  - `TestDefaultProvidersConfig` — 验证默认配置包含 claude 和 cursor
  - `TestLoadOrDefault_NoFile` — 配置文件不存在时返回默认配置
  - `TestLoadOrDefault_ValidFile` — 配置文件存在时返回文件配置
  - `TestLoadOrDefault_InvalidFile` — 配置文件存在但格式错误时返回 error

- [x] 5.5 `drivers/llm/config_test.go` — 配置查找测试：
  - `TestFindProvidersConfigPath_CWD` — 当前目录存在配置文件
  - `TestFindProvidersConfigPath_XDGConfig` — `$XDG_CONFIG_HOME` 目录存在配置文件
  - `TestFindProvidersConfigPath_Precedence` — 两处都存在时优先选择当前目录
  - `TestFindProvidersConfigPath_NotFound` — 两处都不存在时返回空字符串

- [x] 5.6 `drivers/llm/config_test.go` — 性能测试（NFR31）：
  - `TestLoadProvidersConfig_Performance` — 加载 10 个 provider 的配置文件，验证解析耗时 ≤ 2 秒

- [x] 5.7 `drivers/llm/config_test.go` — 边界情况：
  - `TestLoadProvidersConfig_FileNotFound` — 指定路径文件不存在时返回 error
  - `TestLoadProvidersConfig_EmptyFile` — 空文件时返回错误
  - `TestProviderConfig_Validate_CLIDriverIgnoresBaseURL` — `claude-cli`/`cursor-cli` 驱动不要求 `base_url`

## Dev Notes

### 核心设计决策

**本 Story 仅实现配置文件的定义与解析，不涉及驱动实例化和 VFS 注册。** 配置解析是 Epic 23 的基础——Story 23-2 将使用本 Story 的 `ProvidersConfig` 结构体创建驱动实例并注册到 VFS；Story 23-3 将使用 provider 名称列表替换硬编码白名单。严格的职责划分避免单个 Story 改动过大。

**使用 `github.com/goccy/go-yaml` 而非 `gopkg.in/yaml.v3`。** 项目已有依赖 `github.com/goccy/go-yaml v1.19.2`（被 `agents/loader.go`、`compose/parser.go`、`drivers/mcp/config.go` 等 9 个文件使用），保持一致性。`go-yaml` 提供更丰富的错误信息（含行号/列号），满足 AC #2 的行号需求。

**配置文件格式采用 `providers` 列表（非 map）。** 列表保留声明顺序，方便后续 Story 23-5 实现 fallback 优先级。同时列表格式在验证时更容易检测重复名称。

**验证采用"收集所有错误"策略。** 用户可能一次性犯多个配置错误，一次性报告所有问题比逐个报告更友好。使用 `errors.Join`（Go 1.20+）拼接多个验证错误。

### YAML 配置文件 Schema 示例

```yaml
# rnix-providers.yaml
version: "1"
providers:
  - name: claude
    driver: claude-cli
    default_model: haiku

  - name: cursor
    driver: cursor-cli

  - name: ollama
    driver: openai-compat
    base_url: http://localhost:11434/v1
    default_model: llama3

  - name: groq
    driver: openai-compat
    base_url: https://api.groq.com/openai/v1
    default_model: llama-3.3-70b-versatile
    api_key_env: GROQ_API_KEY

  - name: deepseek
    driver: openai-compat
    base_url: https://api.deepseek.com/v1
    default_model: deepseek-chat
    api_key_env: DEEPSEEK_API_KEY
```

### 跨 Story 边界（本 Story 不实现）

| 功能 | 归属 Story | 说明 |
|------|-----------|------|
| 驱动实例化 | 23-2 | 根据 `ProviderConfig.Driver` 调用 `NewClaudeCliDriver()` / `NewOpenAICompatDriver()` 等 |
| VFS 注册 | 23-2 | `devReg.Register("/dev/llm/"+name, FileFactory(driver))` |
| `main.go` 硬编码删除 | 23-2 | 移除 main.go 第 1061-1066 行的硬编码注册 |
| 白名单移除 | 23-3 | 移除 `kernel/kernel.go` 的 `allowedLLMProviders` map |
| API Key 环境变量读取 | 23-4 | 从 `os.Getenv(config.APIKeyEnv)` 读取并注入驱动 |
| Fallback 配置 | 23-5 | 配置文件可能扩展 fallback 字段（本 Story 结构体预留但不实现） |
| 健康检查 | 23-6 | HTTP 类 provider 的可达性检测 |

### 架构合规

- **依赖方向**：`drivers/llm/config.go` 仅依赖标准库（`os`、`fmt`、`errors`、`path/filepath`、`regexp`、`log`）和 `github.com/goccy/go-yaml`。不引入 `kernel/`、`vfs/` 或其他 drivers 的依赖。
- **包内聚**：配置结构体定义在 `drivers/llm/` 包中，与驱动代码同包，便于 Story 23-2 在同包内使用工厂函数直接访问这些类型。
- **命名规范**：遵循 Go 惯用导出命名。结构体字段使用 `yaml` tag。
- **线程安全**：配置解析是启动时一次性操作，无并发需求。`ProvidersConfig` 解析后作为只读数据传递。

### 关键文件变更

| 文件 | 变更类型 | 说明 |
|------|---------|------|
| `drivers/llm/config.go` | **新建** | ProvidersConfig/ProviderConfig 结构体、驱动类型常量、Load/Find/Default/Validate 函数 |
| `drivers/llm/config_test.go` | **新建** | 全部配置解析测试 |

### 测试策略

- 使用 `t.TempDir()` 创建临时配置文件，避免测试间干扰
- 配置查找测试需要 mock `$XDG_CONFIG_HOME`（通过 `t.Setenv`）
- 性能测试使用 `time.Now()` / `time.Since()` 测量，阈值 2 秒（NFR31 要求 ≤ 2s，本测试给予足够裕量）
- 所有测试启用 `-race`
- 遵循项目命名：`Test<Type>_<Method>` 或 `Test<Function>_<Scenario>`

### 参考模式

配置加载参考 `drivers/mcp/config.go` 的 `LoadMCPConfig` 模式：
- `os.ReadFile` → `yaml.Unmarshal` → 后验证
- 简洁的错误包装（`fmt.Errorf("...: %w", err)`）

配置查找参考 `ipc/protocol.go` 的 `SocketPath()` 模式：
- 优先环境变量指定路径，回退默认路径
- 通过 `os.Getenv("XDG_...")` 获取 XDG 目录

## Project Structure Notes

- `drivers/llm/config.go` 是新建文件，与同包的 `driver.go`、`registry.go`、`errors.go` 并列
- `drivers/llm/config_test.go` 与源文件同目录，遵循项目测试文件组织规则
- 不新增外部依赖（`github.com/goccy/go-yaml` 已在 `go.mod` 中）
- 不修改任何现有文件——本 Story 是纯增量

## References

- [Source: drivers/llm/driver.go] — LLMDriver 接口和 DriverInfo 结构体
- [Source: drivers/llm/registry.go] — DriverRegistry（Story 23-2 将激活使用）
- [Source: drivers/llm/openai_compat.go#1-73] — OpenAICompatDriver 构造函数和选项模式
- [Source: drivers/llm/claude_cli.go#1-40] — ClaudeCliDriver 默认常量
- [Source: drivers/llm/cursor_cli.go#1-22] — CursorCliDriver 默认常量
- [Source: drivers/llm/vfsfile.go#93-102] — FileFactory 桥接模式
- [Source: cmd/rnix/main.go#1061-1066] — 当前硬编码注册（Story 23-2 重构目标）
- [Source: kernel/kernel.go#169-191] — 当前硬编码白名单（Story 23-3 移除目标）
- [Source: drivers/mcp/config.go] — YAML 配置加载参考模式（LoadMCPConfig）
- [Source: ipc/protocol.go#626-633] — XDG 路径查找参考模式（SocketPath）
- [Source: agents/types.go#12-16] — AgentModels 结构体（provider 字段关联）
- [Source: _bmad-output/planning-artifacts/epics/epic-23-多llm-provider动态配置-multi-llm-provider-management.md] — Epic 23 完整定义
- FRs covered: FR141

## Change Log

- 2026-03-11: Story 创建，状态 ready-for-dev

## Dev Agent Record

- **Agent Model**: claude-4.6-opus-high-thinking (Cursor)
- **Completion Date**: 2026-03-11
- **Notes**: All 25 tests pass with `-race`. Go 1.26 required removing `t.Parallel()` from 7 tests that use `t.Setenv()` (incompatible in Go 1.26+). Full project test suite (20 packages) passes with zero regressions.
- **Files Created**:
  - `drivers/llm/config.go` — ProvidersConfig/ProviderConfig types, driver constants, FindProvidersConfigPath, LoadProvidersConfig, Validate, DefaultProvidersConfig, LoadOrDefaultProvidersConfig
- **Files Modified**:
  - `drivers/llm/config_test.go` — Removed `t.Parallel()` from 7 tests using `t.Setenv()` for Go 1.26 compatibility

## Senior Developer Review (AI)

- **Reviewer Model**: claude-4.6-opus-high-thinking (Cursor)
- **Review Date**: 2026-03-11
- **Review Mode**: 对抗性代码审查 (Adversarial Code Review), YOLO 自动修复

### 审查总结

| 严重度 | 数量 | 已修复 |
|--------|------|--------|
| HIGH   | 0    | —      |
| MEDIUM | 1    | 1      |
| LOW    | 7    | 0      |

### MEDIUM 问题（已修复）

**M1: `TestLoadProvidersConfig_InvalidNameChars` 断言逻辑过弱**
- **文件**: `drivers/llm/config_test.go:286`
- **问题**: 三个 `!strings.Contains` 条件使用 `&&`（逻辑与），等价于"三个关键词全部缺失才报错"，任意一个关键词存在即通过。应使用 `||`（逻辑或）要求三个关键词全部存在。
- **修复**: `&&` → `||`，确保错误信息同时包含 "bad name!"、"character"、"name"。
- **影响**: 增强回归检测能力，防止错误信息退化时测试仍假通过。

### LOW 问题（未修复，记录备查）

| # | 问题 | 说明 |
|---|------|------|
| L1 | 测试计数偏差 | Dev Record 记录 25 个测试，实际 26 个（含额外的 `TestValidate_TableDriven`），不影响功能 |
| L2 | 测试命名缩写 | `TestLoadOrDefault_*` 缩写了函数全名 `LoadOrDefaultProvidersConfig`，可接受的简化 |
| L3 | `version` 字段未验证 | 本 Story 范围内不要求，后续 Story 可按需添加 |
| L4 | `TestLoadProvidersConfig_MultipleErrors` 仅检查关键词出现 | 未验证具体错误数量，但已足够检测多错误收集行为 |
| L5 | `validDrivers` 命名与 Story spec 的 `validDriverTypes` 不同 | 功能等价，命名更简洁 |
| L6 | 验证错误未包装上下文前缀 | 读文件/解析错误有 `"reading/parsing providers config:"` 前缀，验证错误直接返回。因 `errors.Join` 多行输出与 `fmt.Errorf` 包装组合效果不佳，当前实现更清晰 |
| L7 | XDG 路径未标准化为绝对路径 | 与项目参考实现 (`ipc/protocol.go` SocketPath) 行为一致，可接受 |

### AC 验证结论

| AC | 状态 | 验证依据 |
|----|------|----------|
| AC1 正确解析 | ✅ PASS | 6 个测试覆盖三种驱动类型全字段解析、最小配置、多 provider 场景 |
| AC2 错误处理 | ✅ PASS | 9 个测试 + 9 个 table-driven 子测试覆盖语法错误行号、缺失字段、非法值、重复名、非法字符、空列表、多错误收集 |
| AC3 默认回退 | ✅ PASS | 4 个测试覆盖默认配置内容、文件不存在回退、文件存在加载、无效文件报错 |
| AC4 性能 | ✅ PASS | 10 provider 解析耗时远低于 2s 阈值（实测 ~10ms） |

### 架构合规

- ✅ `drivers/llm/config.go` 仅依赖标准库 + `github.com/goccy/go-yaml`，无 `kernel/` 导入
- ✅ 使用项目已有的 `go-yaml` 依赖（v1.19.2），与 `agents/loader.go`、`drivers/mcp/config.go` 等保持一致
- ✅ 导出类型 PascalCase、YAML tag 规范、`errors.Join` 多错误收集
- ✅ 配置解析为启动时一次性操作，无并发需求，无需 `xsync`
- ✅ 测试使用 `t.TempDir()`/`t.Setenv()`/`t.Chdir()` 隔离，7 个环境相关测试正确移除 `t.Parallel()`

### 验证结果

- `go test -race -count=1 ./drivers/llm/...` — **PASS** (26 tests, 2.579s)
- `go vet ./drivers/llm/...` — **PASS** (0 issues)
