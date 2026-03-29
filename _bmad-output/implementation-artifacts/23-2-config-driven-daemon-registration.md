# Story 23.2: 配置驱动的 Daemon 启动注册流程

Status: done

## Story

As a 系统,
I want daemon 启动时根据配置文件动态实例化并注册 LLM 驱动,
So that 所有已配置的 provider 自动可用，无需硬编码。

## Acceptance Criteria

1. **Given** `rnix-providers.yaml` 已解析
   **When** daemon 启动注册阶段
   **Then** 遍历 providers 配置，根据 `driver` 字段选择构造函数：
   - `claude-cli` → `NewClaudeCliDriver()`
   - `cursor-cli` → `NewCursorCliDriver()`
   - `openai-compat` → `NewOpenAICompatDriver(name, baseURL, opts...)`
   **And** 每个驱动通过 `DriverRegistry.Register(name, driver)` 注册
   **And** 每个驱动通过 `DeviceRegistry.Register("/dev/llm/"+name, FileFactory(driver))` 挂载到 VFS

2. **Given** 注册完成
   **When** 进程通过 VFS `Open("/dev/llm/ollama")` 访问
   **Then** 正确路由到 `OpenAICompatDriver` 实例

3. **Given** 移除 `main.go` 中的硬编码注册
   **When** daemon 启动
   **Then** 所有 LLM 驱动仅通过配置文件注册，`main.go` 无 `NewClaudeCliDriver()` 等硬编码调用

4. **Given** 已有 `DriverRegistry` 实现
   **When** 重构后
   **Then** `DriverRegistry` 作为驱动实例的统一管理入口，所有驱动通过它注册和获取

## Tasks / Subtasks

### Task 1: 创建驱动工厂函数 `drivers/llm/factory.go`（AC: #1）

- [ ] 1.1 新建 `drivers/llm/factory.go`，实现 `CreateDriver(cfg ProviderConfig) (LLMDriver, error)` 工厂函数：

  根据 `cfg.Driver` 字段分发到对应构造函数：
  - `DriverClaudeCLI` → `NewClaudeCliDriver(opts...)` — 若 `cfg.Command` 非空，传入 `WithCommand(cfg.Command)`；若 `cfg.DefaultModel` 非空，传入 `WithModel(cfg.DefaultModel)`
  - `DriverCursorCLI` → `NewCursorCliDriver(opts...)` — 若 `cfg.Command` 非空，传入 `CursorWithCommand(cfg.Command)`；若 `cfg.DefaultModel` 非空，传入 `CursorWithModel(cfg.DefaultModel)`
  - `DriverOpenAICompat` → `NewOpenAICompatDriver(cfg.Name, cfg.BaseURL, opts...)` — 若 `cfg.DefaultModel` 非空，传入 `WithCompatModel(cfg.DefaultModel)`
  - 未知 driver 类型返回 `fmt.Errorf("unsupported driver type: %q", cfg.Driver)`

- [ ] 1.2 **不在本 Story 处理 `api_key_env`** — API Key 环境变量读取归属 Story 23-4。工厂函数仅处理 `name`、`driver`、`default_model`、`base_url` 四个字段。

### Task 2: 增强 `DriverRegistry`，添加 `Names()` 方法（AC: #4）

- [ ] 2.1 在 `drivers/llm/registry.go` 添加 `Names() []string` 方法，返回所有已注册 provider 名称的排序列表：

  利用底层 `xsync.Registry.Range()` 收集所有 key，排序后返回。排序保证错误消息和状态输出的确定性。

- [ ] 2.2 添加 `Len() int` 方法，返回已注册驱动数量（用于日志和断言）：

  利用 `xsync.Registry.Range()` 计数。

### Task 3: 创建注册编排函数（AC: #1, #2, #4）

- [ ] 3.1 在 `drivers/llm/factory.go` 实现 `RegisterProviders` 编排函数：

  ```go
  func RegisterProviders(cfg *ProvidersConfig, driverReg *DriverRegistry, devReg DeviceRegisterer) error
  ```

  **流程：**
  1. 遍历 `cfg.Providers`
  2. 对每个 provider 调用 `CreateDriver(pc)` 创建驱动实例
  3. 调用 `driverReg.Register(pc.Name, driver)` 注册到驱动注册表
  4. 调用 `devReg.Register("/dev/llm/"+pc.Name, FileFactory(driver, "/dev/llm/"+pc.Name))` 挂载到 VFS
  5. 任一步骤失败立即返回错误（fail-fast），已注册的不回滚（daemon 将拒绝启动）

- [ ] 3.2 定义 `DeviceRegisterer` 接口，解耦对 `vfs.DeviceRegistry` 的直接依赖：

  ```go
  type DeviceRegisterer interface {
      Register(path string, factory vfs.VFSFileFactory) error
  }
  ```

  **设计理由：** `drivers/llm` 包不应直接依赖 `vfs.DeviceRegistry` 结构体——只需引用 `vfs` 包的类型（`VFSFileFactory`、`OpenFlag` 等），而 `DeviceRegistry` 由 `cmd/rnix/main.go` 注入。使用接口也便于测试时 mock。

  **重要：** `vfs.DeviceRegistry` 已有 `Register(path string, factory VFSFileFactory) error` 方法签名，天然满足此接口，无需修改 `vfs` 包。

- [ ] 3.3 `RegisterProviders` 完成后输出日志，列出已注册的所有 provider 名称和 VFS 路径：

  ```
  [llm] registered 3 providers: claude → /dev/llm/claude, cursor → /dev/llm/cursor, ollama → /dev/llm/ollama
  ```

### Task 4: 重构 `cmd/rnix/main.go`，替换硬编码注册（AC: #3）

- [ ] 4.1 在 `runDaemon` 函数中，替换第 1061-1066 行的硬编码注册块：

  **删除（旧代码）：**
  ```go
  devReg := vfs.NewDeviceRegistry()
  vfsInst := vfs.NewVFS(devReg)
  claudeDriver := llm.NewClaudeCliDriver()
  _ = devReg.Register("/dev/llm/claude", llm.FileFactory(claudeDriver, "/dev/llm/claude"))
  cursorDriver := llm.NewCursorCliDriver()
  _ = devReg.Register("/dev/llm/cursor", llm.FileFactory(cursorDriver, "/dev/llm/cursor"))
  ```

  **替换为（新代码）：**
  ```go
  providersCfg, err := llm.LoadOrDefaultProvidersConfig()
  if err != nil {
      return fmt.Errorf("loading providers config: %w", err)
  }
  driverReg := llm.NewDriverRegistry()
  devReg := vfs.NewDeviceRegistry()
  if err := llm.RegisterProviders(providersCfg, driverReg, devReg); err != nil {
      return fmt.Errorf("registering LLM providers: %w", err)
  }
  vfsInst := vfs.NewVFS(devReg)
  ```

- [ ] 4.2 确保 `devReg` 在注册完 LLM providers 后仍继续注册 `/dev/fs`、`/dev/shell` 等其他设备（第 1067-1069 行不变）。

- [ ] 4.3 `driverReg` 暂不传入 `Kernel`（Story 23-3 将在 `resolveLLMDevice` 重构时注入）。在 `runDaemon` 中保留 `driverReg` 变量以备后续 Story 使用。

- [ ] 4.4 配置加载失败时，daemon 拒绝启动并返回明确错误。这与 `DefaultProvidersConfig()` 的回退逻辑兼容——仅当文件存在但格式错误时才会失败。

### Task 5: 单元测试（AC: #1, #4）

- [ ] 5.1 `drivers/llm/factory_test.go` — 工厂函数测试：
  - `TestCreateDriver_ClaudeCLI` — `DriverClaudeCLI` 配置创建驱动，验证 `Info().Name` 和 `Info().DefaultModel`
  - `TestCreateDriver_ClaudeCLI_WithModel` — 指定 `default_model`，验证传入驱动
  - `TestCreateDriver_CursorCLI` — `DriverCursorCLI` 配置创建驱动
  - `TestCreateDriver_CursorCLI_WithModel` — 指定 `default_model`，验证传入驱动
  - `TestCreateDriver_OpenAICompat` — `DriverOpenAICompat` 配置创建驱动，验证 `name`、`baseURL`
  - `TestCreateDriver_OpenAICompat_WithModel` — 指定 `default_model`，验证传入驱动
  - `TestCreateDriver_UnsupportedDriver` — 未知 driver 类型返回 error

- [ ] 5.2 `drivers/llm/factory_test.go` — 注册编排测试：
  - `TestRegisterProviders_DefaultConfig` — 使用 `DefaultProvidersConfig()`，验证 `DriverRegistry` 和 mock `DeviceRegisterer` 各注册 2 个 provider
  - `TestRegisterProviders_FullConfig` — 使用 3 种驱动类型的完整配置，验证所有 provider 注册成功
  - `TestRegisterProviders_VFSPathCorrect` — 验证 VFS 注册路径为 `/dev/llm/<name>` 格式
  - `TestRegisterProviders_DriverRegistryNames` — 注册后调用 `Names()` 验证返回排序后的名称列表
  - `TestRegisterProviders_CreateDriverError` — 包含无效 driver 类型的配置，验证 fail-fast 返回错误
  - `TestRegisterProviders_DuplicateProvider` — 配置中包含同名 provider（虽然 `Validate()` 应已阻止，测试 defense-in-depth）

- [ ] 5.3 `drivers/llm/registry_test.go` — Registry 新方法测试：
  - `TestDriverRegistry_Names_Empty` — 空注册表返回空 slice
  - `TestDriverRegistry_Names_Sorted` — 多个驱动注册后返回排序后的名称列表
  - `TestDriverRegistry_Len` — 验证注册驱动数量正确

### Task 6: 集成测试 — VFS 端到端访问验证（AC: #2）

- [ ] 6.1 `drivers/llm/factory_test.go` — VFS 集成测试：
  - `TestRegisterProviders_VFSOpenRouting` — 使用真实 `vfs.DeviceRegistry` 和 `vfs.VFS`，执行 `RegisterProviders` 后调用 `vfsInst.Open("/dev/llm/ollama", 0)` 验证路由到正确驱动
  - 通过打开返回的 `VFSFile` 并调用 `Stat()` 验证 `DevicePath` 为 `/dev/llm/ollama`
  - 测试 `Open("/dev/llm/nonexistent", 0)` 返回 device not found 错误

- [ ] 6.2 `drivers/llm/factory_test.go` — 默认配置 VFS 兼容性测试：
  - `TestRegisterProviders_DefaultConfig_VFSCompat` — 使用 `DefaultProvidersConfig()` 注册后验证 `/dev/llm/claude` 和 `/dev/llm/cursor` 均可通过 VFS Open 访问——确保行为与旧硬编码注册完全一致

## Dev Notes

### 核心设计决策

**工厂函数放在 `drivers/llm/factory.go` 而非 `cmd/rnix/main.go`。** 工厂函数需要访问同包的驱动构造函数（`NewClaudeCliDriver`、`NewCursorCliDriver`、`NewOpenAICompatDriver`）和类型（`ProviderConfig`、`DriverRegistry`），放在 `drivers/llm` 包内最自然。`cmd/rnix/main.go` 仅负责加载配置和调用编排函数——符合"main 只做依赖注入"的架构规则。

**使用 `DeviceRegisterer` 接口解耦驱动层与 VFS 结构体。** `drivers/llm` 已导入 `vfs` 包（`vfsfile.go` 使用 `vfs.VFSFileFactory`、`vfs.VFSFile`、`vfs.OpenFlag`、`vfs.FileStat`），接口仅抽象 `Register` 方法调用，`vfs.DeviceRegistry` 天然满足该接口——零改动。这遵循了依赖方向规则：`drivers` → `vfs`（导入类型），但不 import `DeviceRegistry` 结构体。

**`RegisterProviders` 采用 fail-fast 策略。** 如果任何一个 provider 注册失败（driver 类型未知、Registry 重名冲突），立即返回错误。Daemon 在这种情况下拒绝启动，这比 partial registration 更安全——避免"一半 provider 可用"的不确定状态。

**`Names()` 方法返回排序 slice。** 用于 Story 23-3 的错误消息（`unsupported LLM provider: "x" (available: claude, cursor, ollama)`）和 `daemon status` 输出。排序保证确定性输出，便于测试断言和用户阅读。

**API Key 处理不在本 Story 范围内。** `ProviderConfig.APIKeyEnv` 字段已定义（Story 23-1），但本 Story 的 `CreateDriver` 工厂函数不读取环境变量、不调用 `WithAPIKey()`。这属于 Story 23-4 的职责。对于 `openai-compat` 驱动，本 Story 创建的实例不带 API Key——在需要 Key 的 provider（如 Groq）上首次调用会失败，这是预期行为，Story 23-4 将补全。

### 跨 Story 边界（本 Story 不实现）

| 功能 | 归属 Story | 说明 |
|------|-----------|------|
| `allowedLLMProviders` 白名单移除 | 23-3 | `kernel/kernel.go` 的硬编码白名单改为查询 `DriverRegistry` |
| `resolveLLMDevice()` 重构 | 23-3 | 改为接收 `DriverRegistry` 参数，动态检查 provider 是否已注册 |
| `DriverRegistry` 注入到 Kernel | 23-3 | `runDaemon` 中的 `driverReg` 传递到 `NewKernel()` 或 `Spawn()` |
| API Key 环境变量读取 | 23-4 | `CreateDriver` 扩展读取 `os.Getenv(cfg.APIKeyEnv)` 并传 `WithAPIKey()` |
| API Key 缺失警告 | 23-4 | 环境变量不存在时 log warning 但仍注册 |
| 健康检查 | 23-6 | HTTP provider 的启动时可达性检测 |
| Compose/Init provider 配置 | 23-7 | `rnix-compose.yaml` / `rnix-init.yaml` 新增 `provider` 字段 |

### 架构合规

- **依赖方向**：`drivers/llm/factory.go` 仅导入 `vfs` 包（`vfs.VFSFileFactory` 类型引用）、`fmt`、`log`、`sort`、`strings`。不导入 `kernel/`、`cmd/`。`cmd/rnix/main.go` 调用 `llm.RegisterProviders()` — `cmd/ → drivers/` 方向正确。
- **包内聚**：工厂函数、驱动注册表、驱动接口、VFS 桥接全部在 `drivers/llm` 包内，零跨包调用链。
- **线程安全**：`DriverRegistry` 底层使用 `xsync.Registry`（`sync.RWMutex` 保护），`Names()` / `Len()` 新方法通过 `Range()` 读取也受锁保护。`RegisterProviders` 在 daemon 启动时单线程执行，无并发风险。
- **VFS 路径**：统一使用 `/dev/llm/<name>` 格式（小写、Unix 风格），与现有 `/dev/llm/claude`、`/dev/llm/cursor` 一致。
- **错误处理**：`RegisterProviders` 返回 `error`，由 `main.go` 包装为 `fmt.Errorf("registering LLM providers: %w", err)` 后返回。
- **命名规范**：`CreateDriver`（PascalCase 导出）、`RegisterProviders`（PascalCase 导出）、`DeviceRegisterer`（PascalCase 接口）。

### 关键文件变更

| 文件 | 变更类型 | 说明 |
|------|---------|------|
| `drivers/llm/factory.go` | **新建** | `CreateDriver` 工厂函数、`DeviceRegisterer` 接口、`RegisterProviders` 编排函数 |
| `drivers/llm/factory_test.go` | **新建** | 工厂函数、注册编排、VFS 集成测试 |
| `drivers/llm/registry.go` | **修改** | 添加 `Names() []string` 和 `Len() int` 方法 |
| `drivers/llm/registry_test.go` | **修改** | 添加 `Names` 和 `Len` 方法的测试 |
| `cmd/rnix/main.go` | **修改** | `runDaemon` 函数第 1061-1066 行：删除硬编码注册，替换为 `LoadOrDefaultProvidersConfig` + `RegisterProviders` 调用 |

### 测试策略

- **工厂函数测试**：直接调用 `CreateDriver` 验证返回的驱动类型和 `Info()` 元数据。对 `ClaudeCliDriver` / `CursorCliDriver` 验证 `Info().Name` 和 `Info().DefaultModel`；对 `OpenAICompatDriver` 额外验证 `Info().Provider`。
- **注册编排测试**：使用 mock `DeviceRegisterer`（实现 `Register` 接口记录调用）验证注册路径和调用次数。同时使用真实 `DriverRegistry` 验证驱动实例存储和检索。
- **VFS 集成测试**：使用真实 `vfs.NewDeviceRegistry()` 和 `vfs.NewVFS(devReg)`，调用 `RegisterProviders` 后验证 `Open`/`Stat` 路由正确。
- **回归测试**：默认配置注册后的 VFS 行为必须与旧硬编码注册完全一致（`/dev/llm/claude` 和 `/dev/llm/cursor` 可正常 Open）。
- **所有测试启用 `-race`**。
- **Go 1.26 注意**：`t.Parallel()` 不可与 `t.Setenv()` 同时使用。本 Story 的测试不涉及环境变量修改，可安全使用 `t.Parallel()`。
- **测试命名**：遵循 `Test<Function>_<Scenario>` 格式。

### 组合矩阵（Cross-Module Interaction Matrix）

| 调用方 | 被调用方 | 交互方式 | 本 Story 涉及 |
|--------|---------|---------|:---:|
| `cmd/rnix/main.go` (`runDaemon`) | `llm.LoadOrDefaultProvidersConfig()` | 函数调用 | Yes |
| `cmd/rnix/main.go` (`runDaemon`) | `llm.NewDriverRegistry()` | 构造函数 | Yes |
| `cmd/rnix/main.go` (`runDaemon`) | `llm.RegisterProviders(cfg, driverReg, devReg)` | 编排函数 | Yes |
| `llm.RegisterProviders` | `llm.CreateDriver(pc)` | 工厂函数 | Yes |
| `llm.RegisterProviders` | `DriverRegistry.Register(name, driver)` | 注册驱动实例 | Yes |
| `llm.RegisterProviders` | `DeviceRegisterer.Register(path, factory)` | VFS 设备注册 | Yes |
| `llm.CreateDriver` | `NewClaudeCliDriver(opts...)` | 构造函数 | Yes |
| `llm.CreateDriver` | `NewCursorCliDriver(opts...)` | 构造函数 | Yes |
| `llm.CreateDriver` | `NewOpenAICompatDriver(name, baseURL, opts...)` | 构造函数 | Yes |
| `llm.FileFactory(driver, basePath)` | `vfs.VFSFileFactory` | 工厂闭包 | Yes |
| `vfs.DeviceRegistry.Open(path)` | `VFSFileFactory` → `LLMFile` | 路由 + 文件创建 | Yes (集成测试) |
| `kernel.resolveLLMDevice()` | `DriverRegistry.Get(name)` / `Names()` | provider 查询 | No (Story 23-3) |
| `llm.CreateDriver` | `os.Getenv(cfg.APIKeyEnv)` → `WithAPIKey()` | API Key 注入 | No (Story 23-4) |

### 前置 Story 智能（来自 Story 23-1）

**关键交付件：**
- `drivers/llm/config.go`：`ProvidersConfig`、`ProviderConfig` 类型，三个 driver 常量（`DriverClaudeCLI`、`DriverCursorCLI`、`DriverOpenAICompat`），`LoadProvidersConfig`、`Validate`、`DefaultProvidersConfig`、`LoadOrDefaultProvidersConfig`、`FindProvidersConfigPath`
- `drivers/llm/config_test.go`：26 个测试全部通过

**关键经验：**
- Go 1.26 中 `t.Parallel()` 与 `t.Setenv()` 不兼容——使用了 `t.Setenv()` 的 7 个测试已移除 `t.Parallel()`
- 使用 `github.com/goccy/go-yaml v1.19.2`（非 `gopkg.in/yaml.v3`）
- `validDrivers` map 用于验证 driver 类型合法性
- `nameRegexp` (`^[a-zA-Z0-9_-]+$`) 验证 provider 名称
- `DefaultProvidersConfig()` 返回 claude (claude-cli, haiku) + cursor (cursor-cli) — 本 Story 的默认注册行为必须与之匹配

**配置文件 Schema 不变，本 Story 消费 Story 23-1 的解析结果。**

### Git 智能

最近提交：
- `4cc651e feat : story 23-1` — 配置解析实现
- `6c3e287 feat: prd / arch/ epics for Multi-LLM Provider Management` — Epic 23 规划
- `8f98d07 feat: add system monitoring scripts and update documentation`
- `75407b0 feat: add provider flag for LLM configuration`

提交风格：`feat: <描述>` 格式。本 Story 提交建议：`feat: story 23-2 config-driven daemon registration`

### 参考模式

**驱动构造函数（functional options 模式）：**

`ClaudeCliDriver` (`claude_cli.go:67-77`):
```go
func NewClaudeCliDriver(opts ...ClaudeCliOption) *ClaudeCliDriver {
    d := &ClaudeCliDriver{
        defaultModel: DefaultModel, defaultTimeout: DefaultTimeout,
        cmdBuilder: defaultCommandBuilder,
    }
    for _, opt := range opts { opt(d) }
    return d
}
```

`CursorCliDriver` (`cursor_cli.go:49-59`):
```go
func NewCursorCliDriver(opts ...CursorCliOption) *CursorCliDriver {
    d := &CursorCliDriver{
        defaultModel: "", defaultTimeout: CursorDefaultTimeout,
        cmdBuilder: defaultCommandBuilder,
    }
    for _, opt := range opts { opt(d) }
    return d
}
```

`OpenAICompatDriver` (`openai_compat.go:62-73`):
```go
func NewOpenAICompatDriver(name, baseURL string, opts ...CompatOption) *OpenAICompatDriver {
    d := &OpenAICompatDriver{
        name: name, baseURL: strings.TrimRight(baseURL, "/"),
        defaultTimeout: DefaultTimeout, httpClient: &http.Client{},
    }
    for _, opt := range opts { opt(d) }
    return d
}
```

**VFS FileFactory 桥接** (`vfsfile.go:95-102`):
```go
func FileFactory(driver LLMDriver, basePath string) vfs.VFSFileFactory {
    return func(subpath string, flags vfs.OpenFlag) (vfs.VFSFile, error) {
        return &LLMFile{driver: driver, devicePath: basePath + subpath}, nil
    }
}
```

**xsync.Registry.Range 遍历** (`internal/xsync/registry.go:61-69`):
```go
func (r *Registry[T]) Range(fn func(name string, item T) bool) {
    r.mu.RLock()
    defer r.mu.RUnlock()
    for name, item := range r.items {
        if !fn(name, item) { break }
    }
}
```

**当前 main.go 硬编码注册** (`cmd/rnix/main.go:1061-1066`) — 重构目标：
```go
devReg := vfs.NewDeviceRegistry()
vfsInst := vfs.NewVFS(devReg)
claudeDriver := llm.NewClaudeCliDriver()
_ = devReg.Register("/dev/llm/claude", llm.FileFactory(claudeDriver, "/dev/llm/claude"))
cursorDriver := llm.NewCursorCliDriver()
_ = devReg.Register("/dev/llm/cursor", llm.FileFactory(cursorDriver, "/dev/llm/cursor"))
```

## References

- [Source: drivers/llm/config.go] — `ProvidersConfig`/`ProviderConfig` 类型、driver 常量、`LoadOrDefaultProvidersConfig` 函数（Story 23-1 交付）
- [Source: drivers/llm/registry.go] — `DriverRegistry`（现有 `Register`/`Get` 方法，本 Story 添加 `Names`/`Len`）
- [Source: drivers/llm/driver.go] — `LLMDriver` 接口（`Call`/`Stream`/`Info`）和 `DriverInfo` 结构体
- [Source: drivers/llm/vfsfile.go#95-102] — `FileFactory` 桥接函数
- [Source: drivers/llm/claude_cli.go#42-77] — `ClaudeCliOption` functional options、`NewClaudeCliDriver` 构造函数
- [Source: drivers/llm/cursor_cli.go#25-59] — `CursorCliOption` functional options、`NewCursorCliDriver` 构造函数
- [Source: drivers/llm/openai_compat.go#34-73] — `CompatOption` functional options、`NewOpenAICompatDriver` 构造函数
- [Source: internal/xsync/registry.go#61-69] — `Range()` 遍历方法（用于实现 `Names()`）
- [Source: vfs/dev.go] — `DeviceRegistry` 结构体及其 `Register`/`Open` 方法
- [Source: cmd/rnix/main.go#1056-1069] — `runDaemon` 函数中硬编码注册块（重构目标）
- [Source: kernel/kernel.go#169-191] — `allowedLLMProviders` 硬编码白名单（Story 23-3 移除目标）
- [Source: drivers/llm/registry_test.go] — 现有 Registry 测试（本 Story 扩展）
- [Source: drivers/llm/config_test.go#22-38] — 测试中的 YAML fixture（可复用）
- [Source: _bmad-output/planning-artifacts/epics/epic-23-多llm-provider动态配置-multi-llm-provider-management.md] — Epic 23 完整定义
- FRs covered: FR141, FR142

## Senior Developer Review (AI)

- **Reviewer**: Claude claude-4.6-opus (Adversarial Code Review)
- **Review Date**: 2026-03-12
- **Review Mode**: YOLO (auto-fix enabled)
- **Verdict**: **PASS** — 所有 AC 满足，无 HIGH/MEDIUM 问题

### 缺陷汇总

| 严重度 | 数量 | 状态 |
|--------|------|------|
| HIGH   | 0    | —    |
| MEDIUM | 0    | —    |
| LOW    | 3    | 已记录，无需修复 |

### 发现详情

| # | 严重度 | 文件 | 描述 | 修复状态 |
|---|--------|------|------|----------|
| 1 | LOW | `factory.go:71` | 日志使用 Unicode `→` 箭头，项目其他 `log.Printf` 均为 ASCII。但 Story 规格明确指定此格式，属于设计决策。 | 按设计保留 |
| 2 | LOW | `factory.go:51` | `RegisterProviders` 不对 `cfg.Providers` 做空列表校验，依赖上游 `Validate()` 保证。若被绕过则日志显示 "registered 0 providers: "。实际使用路径中 `LoadOrDefaultProvidersConfig` 始终校验。 | 无需修复（防御性编程，非 bug） |
| 3 | LOW | `registry_test.go:7-45` | 原有 3 个测试（`RegisterGet`/`DuplicateRegister`/`GetNotFound`）缺少 `t.Parallel()`，新增 3 个测试有。不一致但属 pre-existing，非本 Story 范围。 | 非本 Story 范围 |

### AC 验证结论

| AC | 描述 | 状态 | 证据 |
|----|------|------|------|
| AC1 | daemon 根据 config driver 字段选择构造函数，注册到 DriverRegistry 和 DeviceRegistry | **PASS** | `factory.go:20-46` CreateDriver switch 分发三种驱动；`factory.go:51-76` RegisterProviders 编排双注册；`main.go:1061-1069` runDaemon 调用链 |
| AC2 | VFS `Open("/dev/llm/ollama")` 正确路由到 OpenAICompatDriver | **PASS** | `factory_test.go:294-323` TestRegisterProviders_VFSOpenRouting 使用真实 DeviceRegistry，Open 后 Stat 验证 DevicePath |
| AC3 | 移除 main.go 硬编码注册 | **PASS** | git diff 确认删除 `NewClaudeCliDriver()`/`NewCursorCliDriver()` 硬编码，替换为 `LoadOrDefaultProvidersConfig` + `RegisterProviders` |
| AC4 | DriverRegistry 作为统一管理入口 | **PASS** | `registry.go:32-50` Names()/Len() 方法；`factory.go:58` 所有 provider 通过 `driverReg.Register` 注册 |

### Task 完成审计

| Task | 子任务 | 状态 | 验证 |
|------|--------|------|------|
| Task 1: CreateDriver 工厂函数 | 1.1 三种驱动分发 | ✅ | `factory.go:20-46` |
| | 1.2 不处理 api_key_env | ✅ | 无 API Key 代码 |
| Task 2: Registry Names/Len | 2.1 Names() 排序返回 | ✅ | `registry.go:32-40` |
| | 2.2 Len() 计数 | ✅ | `registry.go:43-50` |
| Task 3: RegisterProviders 编排 | 3.1 编排函数实现 | ✅ | `factory.go:51-76` |
| | 3.2 DeviceRegisterer 接口 | ✅ | `factory.go:13-15` |
| | 3.3 完成后日志输出 | ✅ | `factory.go:68-73` |
| Task 4: main.go 重构 | 4.1 替换硬编码块 | ✅ | git diff `main.go` |
| | 4.2 其他设备注册不变 | ✅ | `main.go:1071-1073` |
| | 4.3 driverReg 暂不传入 Kernel | ✅ | 变量声明但未传递 |
| | 4.4 配置加载失败拒绝启动 | ✅ | `main.go:1062-1064` |
| Task 5: 单元测试 | 5.1 工厂函数 7 个测试 | ✅ | `factory_test.go:12-125` |
| | 5.2 注册编排 6 个测试 | ✅ | `factory_test.go:144-290` |
| | 5.3 Registry 3 个测试 | ✅ | `registry_test.go:47-89` |
| Task 6: VFS 集成测试 | 6.1 VFS Open 路由 + 不存在路径 | ✅ | `factory_test.go:294-339` |
| | 6.2 默认配置 VFS 兼容性 | ✅ | `factory_test.go:341-365` |

### 架构合规检查

| 检查项 | 状态 | 说明 |
|--------|------|------|
| 依赖方向 | ✅ | `drivers/llm` → `vfs`（类型引用），不导入 `kernel/`、`cmd/` |
| 包内聚 | ✅ | 工厂、注册表、接口、VFS 桥接全在 `drivers/llm` 包内 |
| 线程安全 | ✅ | DriverRegistry 底层 `xsync.Registry`（RWMutex），Names/Len 通过 Range 读取受锁保护 |
| 命名规范 | ✅ | PascalCase 导出函数和接口，遵循 Go 惯例 |
| 错误处理 | ✅ | 所有错误使用 `%w` 包装，包含 provider 名称上下文 |
| 测试覆盖 | ✅ | 18 个测试（15 new + 3 new），覆盖正常路径、错误路径、集成路由 |
| import cycle 风险 | ✅ | 无循环依赖：`drivers/llm` → `vfs`（类型），`cmd/rnix` → `drivers/llm`（调用） |
| VFS 路径格式 | ✅ | 统一 `/dev/llm/<name>` 格式 |
| race 检测 | ✅ | `go test -race` 通过 |

### 测试结果

```
$ go test -race -count=1 ./drivers/llm/...
ok  github.com/rnixai/rnix/drivers/llm    2.562s

$ go vet ./drivers/llm/...
(clean)

$ go build ./cmd/rnix/...
(clean)
```

## Change Log

- 2026-03-12: Story 创建，状态 ready-for-dev
- 2026-03-12: 实现完成，代码审查通过 (0 HIGH, 0 MEDIUM, 3 LOW)，状态 → done
