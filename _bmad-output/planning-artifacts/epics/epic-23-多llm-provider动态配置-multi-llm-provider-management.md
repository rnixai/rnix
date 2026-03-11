# Epic 23: 多 LLM Provider 动态配置（Multi-LLM Provider Management）

用户通过 `rnix-providers.yaml` 声明式定义多种 LLM provider，daemon 启动时动态解析配置并注册到 VFS `/dev/llm/<name>`，Agent 可指定 provider 并支持 fallback 降级——从单一 Claude CLI 演进为灵活的多模型架构，降低成本、提高可用性。

> **实现基础**
>
> 本 Epic 建立在已有的 LLM 驱动层基础之上：
>
> | 已有组件 | 状态 | 说明 |
> |---------|------|------|
> | `LLMDriver` 接口 | ✅ 已实现 | `Call`/`Stream`/`Info` 基础接口 |
> | `ToolCallingDriver` 接口 | ✅ 已实现 | 工具调用扩展接口 |
> | `ClaudeCliDriver` | ✅ 已实现 | Claude Code CLI 驱动 |
> | `CursorCliDriver` | ✅ 已实现 | Cursor CLI 驱动 |
> | `OpenAICompatDriver` | ✅ 已实现 | OpenAI 兼容 HTTP API 驱动（未挂载） |
> | `DriverRegistry` | ✅ 已实现 | 驱动注册表（未使用） |
> | `LLMFile` / `FileFactory` | ✅ 已实现 | VFS 桥接层 |
> | `resolveLLMDevice()` | ⚠️ 需重构 | 硬编码白名单，需改为配置驱动 |
> | `main.go` 注册块 | ⚠️ 需重构 | 硬编码驱动实例化，需改为配置驱动 |
>
> **核心改动：** 将散落的已有代码统一到配置驱动的注册流程中，激活闲置的 `DriverRegistry` 和 `OpenAICompatDriver`。

## Story 23.1: rnix-providers.yaml 配置文件定义与解析

As a 用户,
I want 通过 `rnix-providers.yaml` 配置文件定义 LLM provider,
So that 新增 provider 无需修改源码，仅需编辑配置文件。

**Acceptance Criteria:**

**Given** 项目根目录存在 `rnix-providers.yaml`
**When** daemon 启动时解析配置文件
**Then** 正确识别每个 provider 的 `driver` 类型（`claude-cli` / `cursor-cli` / `openai-compat`）
**And** 正确读取 `default_model`、`base_url`（HTTP 驱动）、`api_key_env`（HTTP 驱动）字段

**Given** 配置文件格式错误（YAML 语法错误或缺少必填字段）
**When** daemon 启动
**Then** 以明确错误信息拒绝启动，指出具体的格式问题和行号

**Given** 配置文件不存在
**When** daemon 启动
**Then** 回退到默认配置（仅注册 claude provider），日志输出提示使用默认配置

**Given** ≤ 10 个 provider 配置
**When** 解析完成
**Then** 解析耗时 ≤ 2 秒（NFR31）

**Technical Notes:**
- 配置文件查找顺序：项目根目录 `rnix-providers.yaml` → `$XDG_CONFIG_HOME/rnix/rnix-providers.yaml`
- 配置结构体定义在 `drivers/llm/config.go`
- 使用 `gopkg.in/yaml.v3` 解析（项目已有依赖）

**FRs covered:** FR141

## Story 23.2: 配置驱动的 Daemon 启动注册流程

As a 系统,
I want daemon 启动时根据配置文件动态实例化并注册 LLM 驱动,
So that 所有已配置的 provider 自动可用，无需硬编码。

**Acceptance Criteria:**

**Given** `rnix-providers.yaml` 已解析
**When** daemon 启动注册阶段
**Then** 遍历 providers 配置，根据 `driver` 字段选择构造函数：
  - `claude-cli` → `NewClaudeCliDriver()`
  - `cursor-cli` → `NewCursorCliDriver()`
  - `openai-compat` → `NewOpenAICompatDriver(name, baseURL, opts...)`
**And** 每个驱动通过 `DriverRegistry.Register(name, driver)` 注册
**And** 每个驱动通过 `DeviceRegistry.Register("/dev/llm/"+name, FileFactory(driver))` 挂载到 VFS

**Given** 注册完成
**When** 进程通过 VFS `Open("/dev/llm/ollama")` 访问
**Then** 正确路由到 `OpenAICompatDriver` 实例

**Given** 移除 `main.go` 中的硬编码注册
**When** daemon 启动
**Then** 所有 LLM 驱动仅通过配置文件注册，`main.go` 无 `NewClaudeCliDriver()` 等硬编码调用

**Given** 已有 `DriverRegistry` 实现
**When** 重构后
**Then** `DriverRegistry` 作为驱动实例的统一管理入口，所有驱动通过它注册和获取

**Technical Notes:**
- 重构 `cmd/rnix/main.go` 第 1061-1066 行的硬编码注册块
- 激活 `drivers/llm/registry.go` 中闲置的 `DriverRegistry`
- 新增 `drivers/llm/factory.go`：根据配置创建驱动实例的工厂函数

**FRs covered:** FR141, FR142

## Story 23.3: Provider 动态解析与白名单移除

As a 系统,
I want `resolveLLMDevice()` 从配置文件动态解析 provider，不再使用硬编码白名单,
So that 用户添加新 provider 时内核代码无需任何修改。

**Acceptance Criteria:**

**Given** `kernel/kernel.go` 中的 `allowedLLMProviders` 硬编码 map
**When** 重构后
**Then** 移除硬编码白名单 map，改为查询 `DriverRegistry` 判断 provider 是否已注册

**Given** Agent `agent.yaml` 指定 `models.provider: ollama`
**When** Spawn 时调用 `resolveLLMDevice()`
**Then** 查询 `DriverRegistry`，确认 `ollama` 已注册，返回 `/dev/llm/ollama`

**Given** Agent 指定不存在的 provider（如 `models.provider: nonexist`）
**When** Spawn 时
**Then** 返回清晰错误：`unsupported LLM provider: "nonexist" (available: claude, cursor, ollama)`，列出所有已注册的 provider

**Given** Agent 未指定 provider（空字符串）
**When** Spawn 时
**Then** 使用系统默认 provider（`claude`）

**Given** CLI `--provider=groq` 参数
**When** Spawn 时
**Then** CLI 参数覆盖 agent.yaml 中的 provider 配置

**Technical Notes:**
- `resolveLLMDevice()` 需要接收 `DriverRegistry` 引用
- 同步更新 `SpawnOpts.Provider` 的文档注释

**FRs covered:** FR143, FR145

## Story 23.4: HTTP API Provider 的 API Key 管理

As a 用户,
I want HTTP API 类型的 provider 通过环境变量引用 API Key,
So that 密钥不明文存储在配置文件中，符合安全最佳实践。

**Acceptance Criteria:**

**Given** `rnix-providers.yaml` 中 provider 配置了 `api_key_env: GROQ_API_KEY`
**When** 创建 `OpenAICompatDriver` 实例
**Then** 从环境变量 `GROQ_API_KEY` 读取 API Key
**And** Key 值通过 `WithAPIKey()` 选项传入驱动

**Given** `api_key_env` 指定的环境变量不存在
**When** daemon 启动
**Then** 日志输出 warning：`provider "groq": API key env var GROQ_API_KEY not set`
**And** provider 仍然注册（用户可能稍后设置环境变量）
**But** 首次调用时返回 `ErrAuth` 错误

**Given** 本地 provider（如 Ollama）不需要 API Key
**When** `api_key_env` 字段为空或未设置
**Then** 驱动正常创建，不附带认证头

**Given** 安全审计
**When** 检查配置文件和日志
**Then** API Key 明文不出现在任何配置文件、日志输出或错误消息中

**Technical Notes:**
- `OpenAICompatDriver` 已有 `WithAPIKey()` 选项，仅需在工厂函数中正确传递
- 考虑支持 `api_key_file` 作为未来扩展（本 story 不实现）

**FRs covered:** FR146

## Story 23.5: Provider Fallback 降级机制

As a 用户,
I want 当首选 provider 调用失败时自动切换到备选 provider,
So that 智能体任务不会因单个 provider 故障而中断。

**Acceptance Criteria:**

**Given** Agent `agent.yaml` 配置了 `models.preferred: sonnet` + `models.fallback: haiku`（同 provider）
**When** preferred 模型调用返回 `ErrModelNotFound`
**Then** 自动使用 fallback 模型重试

**Given** Agent 配置了跨 provider fallback（如 `provider: ollama` + `fallback` 对应 claude）
**When** Ollama 调用失败（HTTP 5xx、连接超时、连接拒绝、认证失败）
**Then** 自动切换到 claude provider 的 fallback 模型
**And** 切换延迟（从检测失败到发起 fallback 调用）≤ 1 秒（NFR33）

**Given** Fallback 也失败
**When** 所有配置的 provider 均不可用
**Then** 进程转为 Zombie 状态，错误信息包含所有尝试过的 provider 列表和各自失败原因

**Given** Fallback 成功
**When** 任务完成
**Then** strace 输出中可见 provider 切换事件：`[fallback] /dev/llm/ollama failed (connection refused) → /dev/llm/claude`

**Given** Agent 未配置 fallback（`models.fallback` 为空）
**When** 首选 provider 调用失败
**Then** 直接报错，不尝试 fallback

**Technical Notes:**
- Fallback 逻辑实现在 `kernel/kernel.go` 的 `reasonStep` 中，LLM 调用失败时检查 fallback 配置
- 需要扩展 `AgentModels` 支持跨 provider fallback（当前 `Fallback` 字段仅指模型名，需支持 `provider:model` 格式或新增 `fallback_provider` 字段）
- `LLMError` 已有 `Provider` 字段，可用于识别失败来源

**FRs covered:** FR144

## Story 23.6: Provider 健康检查与状态报告

As a 运维人员,
I want daemon 启动时对 HTTP API provider 执行健康检查,
So that 我能及时知道哪些 provider 不可用。

**Acceptance Criteria:**

**Given** HTTP API 类 provider 已注册
**When** daemon 启动完成后
**Then** 对每个 HTTP API provider 执行轻量健康检查（`GET /v1/models` 或 `HEAD`）
**And** 单个健康检查耗时 ≤ 3 秒（NFR32）

**Given** 健康检查失败
**When** provider 端点不可达
**Then** daemon 正常启动（不因单个 provider 失败而拒绝启动）
**And** 该 provider 标记为 `unhealthy`
**And** 日志输出 warning：`provider "groq": health check failed: connection refused`

**Given** CLI 类 provider（claude、cursor）
**When** daemon 启动
**Then** 跳过健康检查（CLI 可用性在首次调用时验证）

**Given** 用户执行 `rnix daemon status`
**When** 查看输出
**Then** 显示所有已注册 provider 的状态（healthy / unhealthy / unchecked）

**Technical Notes:**
- 健康检查异步执行，不阻塞 daemon 启动主流程
- 健康状态存储在 `DriverRegistry` 中
- `daemon status` IPC method 扩展：在响应中包含 provider 状态列表

**FRs covered:** FR141（配置解析后的可用性验证）
**NFRs covered:** NFR32

## Story 23.7: rnix-compose/init 配置格式升级

As a 用户,
I want `rnix-compose.yaml` 和 `rnix-init.yaml` 支持 `provider` + `model` 组合配置,
So that 多 provider 场景下模型指定不会产生歧义。

**Acceptance Criteria:**

**Given** `rnix-compose.yaml` 中智能体配置
**When** 使用新格式 `provider: ollama` + `model: llama3`
**Then** Compose 引擎正确解析并在 Spawn 时传递 provider 参数

**Given** 向后兼容
**When** 使用旧格式仅指定 `model: haiku`（无 `provider` 字段）
**Then** 系统使用默认 provider（claude），行为与升级前一致

**Given** `rnix-init.yaml` supervisor children 配置
**When** 指定 `provider: groq` + `model: llama-3.3-70b-versatile`
**Then** init 引导时正确使用指定的 provider 和 model

**Technical Notes:**
- `compose/parser.go`：`AgentNode` 结构体新增 `Provider` 字段
- `kernel/init.go`：child config 新增 `Provider` 字段
- 两处均兼容旧格式（`Provider` 为空时使用默认值）

**FRs covered:** FR143（Compose/Init 场景下的 provider 指定）
