# Epic 24: LLM Serve — OpenAI 兼容网关（LLM Serve Gateway）

通过 `rnix serve` 启动 OpenAI 兼容 HTTP 服务器，将 daemon 已注册的 `/dev/llm/*` provider 暴露为标准 OpenAI API 端点。外部工具（Aider、Open WebUI、Python `openai` 库等）无需了解 Rnix 内部即可消费 LLM 能力——一个端口统一所有 LLM 访问。

> **实现基础**
>
> 本 Epic 建立在 Epic 23（多 LLM Provider 管理）的基础之上：
>
> | 已有组件 | 状态 | 说明 |
> |---------|------|------|
> | `DriverRegistry` | ✅ 已实现 | 驱动注册表，已通过配置文件动态注册 |
> | `LLMDriver` 接口 | ✅ 已实现 | `Call`/`Stream`/`Info` 基础接口 |
> | `OpenAICompatDriver` | ✅ 已实现 | OpenAI 兼容 HTTP API 驱动 |
> | `ClaudeCliDriver` | ✅ 已实现 | Claude Code CLI 驱动 |
> | `CursorCliDriver` | ✅ 已实现 | Cursor CLI 驱动 |
> | `HealthStatuses()` | ✅ 已实现 | provider 健康状态查询 |
> | `rnix-providers.yaml` | ✅ 已实现 | 配置文件驱动的 provider 注册 |
>
> **核心改动：** 在 `ipc/` 包中新增 OpenAI 兼容 HTTP Server，做协议翻译（HTTP ↔ LLMDriver.Call/Stream），绕过 Kernel 直接调用 DriverRegistry，最小化 HTTP 开销。

**架构决策：** [Decision 13](../../planning-artifacts/architecture/core-architectural-decisions.md#decision-13-llm-serve-gatewayopenai-兼容-http-网关)
**用户旅程：** 旅程 6（陈明通过 rnix serve 让外部工具使用 LLM）

## Story 24.1: OpenAI HTTP Server 核心框架

As a 开发者,
I want 在 `ipc/` 包中建立 OpenAI 兼容 HTTP Server 的核心框架和类型系统,
So that 后续端点实现有统一的基础设施（路由、类型、错误格式、安全绑定）。

**Acceptance Criteria:**

**Given** `ipc/http_openai.go` 中 `OpenAIServer` 结构体已实现
**When** 创建 `NewOpenAIServer(driverReg, addr)` 实例
**Then** 持有 `DriverRegistry` 引用，配置监听地址

**Given** OpenAI 兼容请求/响应类型已定义
**When** 查看 `ChatCompletionRequest`、`ChatCompletionResponse`、`ChatCompletionChunk`、`ChatMessage`、`ChatUsage` 类型
**Then** 字段名和 JSON tag 与 OpenAI API 规范一致（`snake_case` JSON 字段）

**Given** `parseModel` 路由函数已实现
**When** 输入 `"ollama:llama3"`
**Then** 返回 provider=`"ollama"`, model=`"llama3"`
**When** 输入 `"ollama"`（仅 provider 名）
**Then** 返回 provider=`"ollama"`, model=`""`（使用 provider 的 `default_model`）
**When** 输入 `"cursor:claude-3.5-sonnet"`
**Then** 返回 provider=`"cursor"`, model=`"claude-3.5-sonnet"`（FR151）

**Given** OpenAI 兼容错误响应格式已定义（`OpenAIError` 结构体）
**When** provider 不存在时返回错误
**Then** HTTP 404 + `{"error": {"message": "...", "type": "invalid_request_error", "code": "model_not_found"}}`
**When** 请求体格式错误
**Then** HTTP 400 + `error.code: "invalid_request"`

**Given** `/health` 端点已实现
**When** GET `/health`
**Then** 返回 HTTP 200 + 服务状态

**Given** 安全绑定配置
**When** 启动 OpenAIServer
**Then** 默认仅绑定 `127.0.0.1`，不暴露到外部网络接口（NFR52）

**Technical Notes:**
- 新建 `ipc/http_openai.go`（~250-350 行）
- 使用 Go 标准库 `net/http`，不引入第三方框架
- `OpenAIServer` 持有 `*llm.DriverRegistry` 引用（只读）
- 路由使用 Go 1.22+ `http.NewServeMux` 方法匹配：`mux.HandleFunc("POST /v1/chat/completions", ...)`

**FRs covered:** FR147（部分）, FR151（parseModel）
**NFRs covered:** NFR52

## Story 24.2: /v1/chat/completions 同步模式

As a 外部工具用户,
I want 通过标准 OpenAI Chat Completion API 发起同步 LLM 请求,
So that 任何支持 OpenAI API 的工具可以通过 Rnix 网关调用已注册的 LLM provider。

**Acceptance Criteria:**

**Given** `/v1/chat/completions` POST 端点已实现
**When** 发送 `{"model": "ollama:llama3", "messages": [{"role": "user", "content": "hello"}], "stream": false}`
**Then** 系统将 model 解析为 provider=`"ollama"`, model=`"llama3"`
**And** 通过 `driverReg.Get("ollama")` 获取驱动实例
**And** 将 `ChatMessage` 数组转换为 `LLMRequest`
**And** 调用 `driver.Call(ctx, req)` 获取同步响应
**And** 将 `LLMResponse` 转换为 `ChatCompletionResponse` 格式返回（FR148）

**Given** model 参数为仅 provider 名
**When** 发送 `{"model": "cursor", "messages": [...]}`
**Then** 使用该 provider 的 `default_model` 执行请求（FR151）

**Given** model 参数支持 provider:model 复合格式
**When** 发送 `{"model": "cursor:claude-3.5-sonnet", "messages": [...]}`
**Then** 使用指定 model 覆盖 provider 的默认模型（FR151）

**Given** 请求发送到不存在的 provider
**When** 发送 `{"model": "nonexistent", "messages": [...]}`
**Then** 返回 HTTP 404 + OpenAI 错误格式 + 可用 provider 列表提示

**Given** LLM 驱动内部错误
**When** 驱动返回超时或 5xx 错误
**Then** 返回 HTTP 504（超时）或 502（上游错误）+ OpenAI 错误格式

**Given** HTTP 请求处理开销测量
**When** 正常请求（不含 LLM 推理本身）
**Then** HTTP 处理开销 ≤ 50ms（NFR50）

**Technical Notes:**
- 消息转换：`ChatMessage` → `llm.LLMRequest.Messages`（role + content 映射）
- 响应转换：`llm.LLMResponse` → `ChatCompletionResponse`（含 id、usage、choices）
- 错误映射参考架构文档中的错误响应表

**FRs covered:** FR148, FR151
**NFRs covered:** NFR50

## Story 24.3: SSE 流式响应

As a 外部工具用户,
I want 通过 `stream: true` 获取 SSE 流式 LLM 响应,
So that 支持实时流式输出的工具（如 Open WebUI）可以获得逐 token 的响应体验。

**Acceptance Criteria:**

**Given** `/v1/chat/completions` 端点的流式模式已实现
**When** 发送 `{"model": "ollama:llama3", "messages": [...], "stream": true}`
**Then** 响应 Content-Type 为 `text/event-stream`
**And** 响应 Cache-Control 为 `no-cache`
**And** 响应 Connection 为 `keep-alive`

**Given** LLM 驱动返回 StreamEvent 通道
**When** 每个 StreamEvent 到达
**Then** 转换为 `ChatCompletionChunk` 格式
**And** 以 `data: {json}\n\n` 格式写入响应（FR150）
**And** 每个 chunk 后立即 Flush

**Given** 流式输出完成
**When** StreamEvent 通道关闭
**Then** 写入 `data: [DONE]\n\n` 终止标记并 Flush

**Given** 客户端主动断开连接
**When** HTTP 请求的 context 被取消
**Then** 取消传播到 `driver.Stream(ctx, req)` 的 context
**And** 驱动停止生成事件，资源正确释放

**Given** 流式模式下的错误处理
**When** 驱动在流式过程中返回错误
**Then** 写入包含错误信息的 SSE 事件后关闭连接

**Technical Notes:**
- `w.(http.Flusher)` 断言获取 Flusher 接口
- `LLMDriver.Stream()` 已返回 `<-chan StreamEvent`，只需做格式转换
- 使用 `r.Context()` 传播客户端断开到驱动层
- `ChatCompletionChunk` 与 `ChatCompletionResponse` 结构相似但使用 `delta` 而非 `message`

**FRs covered:** FR150

## Story 24.4: /v1/models Provider 发现

As a 外部工具用户,
I want 通过 `/v1/models` 端点发现所有可用的 LLM provider 和模型,
So that 工具（如 Open WebUI）可以自动发现并展示可用模型列表。

**Acceptance Criteria:**

**Given** `/v1/models` GET 端点已实现
**When** 发送 GET `/v1/models`
**Then** 返回所有已注册且健康的 provider 及其可用模型列表
**And** 响应格式兼容 OpenAI Models API（FR149）

**Given** 响应格式定义
**When** 查看 `/v1/models` 响应
**Then** 包含 `object: "list"` 和 `data` 数组
**And** 每个 model entry 包含 `id`（provider 名或 provider:model 格式）、`object: "model"`、`created`、`owned_by`（provider 名）

**Given** 多个 provider 已注册
**When** daemon 注册了 claude、cursor、ollama 三个 provider
**Then** `/v1/models` 返回包含所有三个 provider 的模型列表

**Given** provider 健康状态集成
**When** 某个 provider 健康检查失败（标记为不可用）
**Then** `/v1/models` 不返回该 provider 的模型
**And** 其他健康 provider 正常列出

**Technical Notes:**
- 复用 `DriverRegistry.HealthStatuses()` 获取 provider 健康状态
- 复用 `DriverRegistry.List()` 遍历已注册驱动
- `driver.Info()` 获取 provider 的模型信息
- OpenAI Models API 格式：`{"object": "list", "data": [{"id": "...", "object": "model", ...}]}`

**FRs covered:** FR149

## Story 24.5: rnix serve CLI 命令与端到端集成

As a 用户,
I want 通过 `rnix serve` 命令一键启动 OpenAI 兼容网关,
So that 我可以让所有支持 OpenAI API 的外部工具通过 Rnix 统一消费 LLM 能力。

**Acceptance Criteria:**

**Given** `cmd/rnix/serve.go` 中 `rnix serve` Cobra 命令已实现
**When** 执行 `rnix serve --port 8080`
**Then** 启动 OpenAI 兼容 HTTP 服务器监听 `127.0.0.1:8080`
**And** 终端输出 `Serving N providers on http://127.0.0.1:8080`（FR147）

**Given** `--port` 参数
**When** 未指定 `--port`
**Then** 使用默认端口 8080
**When** 指定 `--port 9090`
**Then** 监听 `127.0.0.1:9090`

**Given** daemon 集成
**When** `rnix serve` 启动
**Then** 共享 daemon 已注册的驱动实例和 `rnix-providers.yaml` 配置
**And** 新增或变更 provider 后重启 daemon 即可生效，无需独立配置（FR152）

**Given** 并发连接测试
**When** 同时发起 10 个 HTTP 请求
**Then** 所有请求正常处理，无请求丢弃或阻塞（NFR51）

**Given** 端到端验证（旅程 6 场景）
**When** 外部 Python 脚本使用标准 `openai` 库连接网关
**Then** `client = OpenAI(base_url="http://localhost:8080/v1", api_key="unused")` 可正常调用
**And** `client.chat.completions.create(model="ollama:llama3", messages=[...])` 返回有效结果

**Given** 优雅停止
**When** 收到 SIGTERM 或 SIGINT
**Then** HTTP 服务器优雅关闭（等待进行中请求完成，超时后强制关闭）

**Technical Notes:**
- 新建 `cmd/rnix/serve.go`（~60 行）
- `serve` 命令通过 IPC 请求 daemon 启动 HTTP 监听，或直接在 daemon 内部启动
- 修改 `cmd/rnix/main.go` 的 `runDaemon()` 函数，可选启动 HTTP 监听（~15 行改动）
- 优雅停止：监听 `os.Signal`，调用 `OpenAIServer.Shutdown(ctx)`

**FRs covered:** FR147, FR152
**NFRs covered:** NFR51

---

## Requirements Coverage Map

| FR/NFR | Story | 说明 |
|--------|-------|------|
| FR147 | 24.1, 24.5 | rnix serve 启动 OpenAI 兼容 HTTP 服务器 |
| FR148 | 24.2 | /v1/chat/completions 端点 |
| FR149 | 24.4 | /v1/models 端点 |
| FR150 | 24.3 | SSE 流式响应 |
| FR151 | 24.1, 24.2 | provider:model 复合格式路由 |
| FR152 | 24.5 | 共享 daemon 驱动实例和配置 |
| NFR50 | 24.2 | HTTP 处理开销 ≤ 50ms |
| NFR51 | 24.5 | ≥ 10 并发连接 |
| NFR52 | 24.1 | 仅绑定 127.0.0.1 |

## File Impact Summary

| 文件 | 变更类型 | 估算行数 |
|------|---------|---------|
| `ipc/http_openai.go` | 新增 | ~250-350 |
| `ipc/http_openai_test.go` | 新增 | ~200 |
| `cmd/rnix/serve.go` | 新增 | ~60 |
| `cmd/rnix/main.go` | 修改 | ~15 |
