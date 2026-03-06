---
stepsCompleted: [1, 2, 3, 4, 5, 6]
inputDocuments: []
workflowType: 'research'
lastStep: 6
research_type: 'technical'
research_topic: '多 LLM 提供商集成技术方案'
research_goals: '评估 OpenAI/Gemini/Ollama 等提供商的 API 差异、Go SDK 评估（含 go-genai）、CLI 可用性、抽象层设计模式'
user_name: 'Decker'
date: '2026-03-06'
web_research_enabled: true
source_verification: true
---

# 多 LLM 提供商集成技术方案：综合技术研究报告

**日期：** 2026-03-06
**作者：** Decker
**研究类型：** 技术研究
**项目上下文：** Crux Agent OS — `drivers/llm/` 多提供商驱动层设计

---

## Research Overview

本报告对 Crux Agent OS 的多 LLM 提供商集成方案进行了系统性技术调研。研究覆盖 OpenAI、Google Gemini、Anthropic、Ollama 四大提供商的 Go SDK、REST API、CLI 工具链，以及 Go 生态中的多 LLM 抽象框架（LangChainGo、any-llm-go、Eino 等）。核心发现：Crux 现有的 `LLMDriver` 接口设计方向正确，流式 channel 模式与 2026 年主流实践完全吻合；扩展时应优先引入接口隔离（分离 Tool Calling）、类型化错误规范、以及 OpenAI 兼容基座模式以降低新 Provider 接入成本。完整分析和建议见下文各章节。

---

## 执行摘要

### 关键发现

1. **Go SDK 生态已成熟**：四大提供商均拥有官方 Go SDK（OpenAI v3.26.0、Anthropic v1.26.0、Google go-genai v1.49.0、Ollama v0.17.7），均已达到生产可用水平
2. **OpenAI API 成为事实标准**：Ollama、Groq、DeepSeek、Mistral 等均提供 OpenAI 兼容端点，Google Gemini 也已跟进——基于 OpenAI 兼容基座可大幅降低新 Provider 接入成本
3. **Tool Calling 格式分裂严重**：四家的工具定义、调用返回、结果回传格式各不相同，是抽象层设计的核心挑战
4. **Crux 现有架构具备良好基础**：`LLMDriver` 接口 + `DriverRegistry` + `<-chan StreamEvent` 流式设计与业界最佳实践高度一致

### 核心建议

1. **分层接口设计**：基础 `LLMDriver` + 可选 `ToolCallingDriver` + 可选 `MultimodalDriver`，通过接口类型断言检测能力
2. **双轨驱动策略**：优先级 Provider（Anthropic、OpenAI）用原生 SDK 直接集成；兼容 Provider 用 OpenAI 兼容基座统一接入
3. **类型化错误规范**：统一 `ErrRateLimit`、`ErrAuth`、`ErrContextLength` 等 sentinel error
4. **渐进式迁移**：保留 CLI 驱动作为 fallback，新增 SDK 直连驱动作为主路径

---

## 目录

1. [技术研究范围与方法论](#1-技术研究范围与方法论)
2. [Go SDK 全景分析](#2-go-sdk-全景分析)
3. [跨提供商 API 差异深度对比](#3-跨提供商-api-差异深度对比)
4. [CLI 工具链可用性评估](#4-cli-工具链可用性评估)
5. [多 LLM 抽象层设计模式](#5-多-llm-抽象层设计模式)
6. [Crux 现有架构分析与扩展建议](#6-crux-现有架构分析与扩展建议)
7. [实施路线图与风险评估](#7-实施路线图与风险评估)
8. [参考资料与信息源](#8-参考资料与信息源)

---

## 1. 技术研究范围与方法论

### 研究范围

- **API 协议层**：各提供商 Chat/Messages API 格式差异（请求/响应结构、流式协议、工具调用）
- **Go SDK/客户端库**：官方及社区 Go 库的成熟度、API 覆盖度、类型安全性
- **CLI 工具链**：各提供商 CLI 在自动化/脚本调用中的表现
- **抽象层设计模式**：适配器模式、Provider Interface、现有多 LLM 抽象框架评估

### 研究方法论

- 基于当前 Web 数据的事实验证（2026 年 3 月）
- 关键技术声明的多源交叉验证
- 对每个提供商的官方仓库、pkg.go.dev 文档、release notes 进行直接核实
- 对不确定信息标注置信度等级

**范围确认日期：** 2026-03-06

---

## 2. Go SDK 全景分析

### 2.1 总览对比矩阵

| 维度 | OpenAI (`openai-go`) | Anthropic (`anthropic-sdk-go`) | Google (`go-genai`) | Ollama (`ollama/api`) |
|---|---|---|---|---|
| **最新版本** | v3.26.0 (2026-03-05) | v1.26.0 (2026-02-19) | v1.49.0 (2026-03-04) | v0.17.7 (2026-03-06) |
| **Go 版本要求** | 1.22+ | 1.22+ | 1.23+ (iter.Seq2) | 1.22+ |
| **代码生成方式** | OpenAPI 自动生成 | Stainless 自动生成 | 自动生成 | 手动维护 |
| **许可证** | Apache-2.0 | MIT | Apache-2.0 | MIT |
| **状态** | Beta（活跃迭代） | v1+ 稳定版 | GA（2025-05 起） | 稳定 |
| **被导入数** | — | 276+ | 398+ | 339+ |
| **Chat API** | Responses + Chat Completions | Messages API | GenerateContent | Chat + Generate |
| **流式支持** | `stream.Next()` 迭代器 | `stream.Next()` 迭代器 | `iter.Seq2` range | 回调函数 |
| **Tool Calling** | 完整支持 | 完整支持 + ToolRunner | 完整支持 | 基础支持 |
| **多模态** | 图片、音频 | 图片、PDF | 图片、音频、视频、PDF | 图片（LLaVA 等） |
| **Embeddings** | 支持 | Beta | 支持 | 支持 |

### 2.2 OpenAI Go SDK (`github.com/openai/openai-go`)

**导入路径：** `github.com/openai/openai-go/v3`

**核心 API 表面：**

```go
client := openai.NewClient()  // 自动从 OPENAI_API_KEY 读取

// Responses API（主推）
resp, _ := client.Responses.New(ctx, responses.ResponseNewParams{
    Model: openai.ChatModelGPT4o,
    Input: responses.ResponseNewParamsInputUnion{
        OfString: openai.String("Hello"),
    },
})

// 流式
stream := client.Responses.NewStreaming(ctx, params)
for stream.Next() {
    data := stream.Current()
    print(data.Delta)
}

// Chat Completions（传统，永久支持）
completion, _ := client.Chat.Completions.New(ctx, params)
```

**设计特点：**
- Union 类型用 `Of` 前缀字段（`OfString`、`OfFunction`）
- 可选参数用 `param.Opt[T]` 包装
- 利用 Go 1.24+ `omitzero` 语义处理零值序列化
- 内置 Azure 支持

**社区库对比：** `sashabaranov/go-openai`（v1.41.2，10.6k stars）仅支持 Chat Completions API，2025-09 后停更。新项目应使用官方 SDK。

_Source: [github.com/openai/openai-go](https://github.com/openai/openai-go)_

### 2.3 Google go-genai (`google.golang.org/genai`)

**统一双后端：** 同一 SDK 支持 Gemini API（API Key）和 Vertex AI（GCP 凭据），通过 `Backend` 配置切换。

```go
// Gemini API
client, _ := genai.NewClient(ctx, &genai.ClientConfig{
    APIKey:  apiKey,
    Backend: genai.BackendGeminiAPI,
})

// Vertex AI
client, _ := genai.NewClient(ctx, &genai.ClientConfig{
    Project:  project,
    Location: location,
    Backend:  genai.BackendVertexAI,
})
```

**Client 子模块：** `Models`、`Chats`、`Files`、`Caches`、`Batches`、`Tunings`、`Live`（实时双向流）、`FileSearchStores`、`Tokens`

**流式使用 Go 1.23+ `iter.Seq2`：**

```go
for result, err := range client.Models.GenerateContentStream(
    ctx, "gemini-2.5-flash", genai.Text("Hello"), nil) {
    if err != nil { break }
    fmt.Print(result.Text())
}
```

**旧 SDK 迁移状态：**
- `github.com/google/generative-ai-go` — 已废弃（2025-11-30）
- `cloud.google.com/go/vertexai/genai` — 已废弃，2026-06-24 删除

_Source: [github.com/googleapis/go-genai](https://github.com/googleapis/go-genai)_

### 2.4 Anthropic Go SDK (`github.com/anthropics/anthropic-sdk-go`)

**核心调用模式：**

```go
client := anthropic.NewClient() // 自动从 ANTHROPIC_API_KEY 读取

message, _ := client.Messages.New(ctx, anthropic.MessageNewParams{
    MaxTokens: 1024,
    Messages: []anthropic.MessageParam{
        anthropic.NewUserMessage(anthropic.NewTextBlock("Hello")),
    },
    Model: anthropic.ModelClaudeSonnet4_5_20250929,
})
```

**独特特性：**
- **ToolRunner 自动工具循环**（v1.26.0）：`runner.RunToCompletion(ctx)` 自动处理多轮工具调用
- **Extended Thinking**：`ThinkingConfigParamUnion` 启用思维链，`BudgetTokens` 控制推理预算
- **Prompt Caching**：`CacheControlEphemeralParam` 减少 90% 重复输入成本
- **Structured Outputs**（v1.20.0）：Messages API 原生支持
- **Vertex AI / Bedrock 集成**：通过 option 配置即可切换后端

**自动重试：** 408、409、429、5xx 和连接错误默认重试 2 次，支持 `Retry-After` 和指数退避。

_Source: [github.com/anthropics/anthropic-sdk-go](https://github.com/anthropics/anthropic-sdk-go)_

### 2.5 Ollama Go 客户端 (`github.com/ollama/ollama/api`)

**特点：** Ollama CLI 自身使用的包，保证 API 完全兼容。

```go
client, _ := api.ClientFromEnvironment() // 从 OLLAMA_HOST 读取

err = client.Chat(ctx, &api.ChatRequest{
    Model:    "qwen3:32b",
    Messages: []api.Message{{Role: "user", Content: "Hello"}},
    Tools:    tools,
}, func(resp api.ChatResponse) error {
    fmt.Print(resp.Message.Content) // 流式回调
    return nil
})
```

**流式设计差异：** 使用回调函数而非迭代器或 channel——集成到 Crux 时需适配为 `<-chan StreamEvent`。

**OpenAI 兼容端点：** `/v1/chat/completions`、`/v1/embeddings`、`/v1/models`、`/v1/responses`（v0.13.3+）。兼容度高但不支持 `tool_choice`、`logit_bias`、`n` 参数。

_Source: [github.com/ollama/ollama](https://github.com/ollama/ollama)_

---

## 3. 跨提供商 API 差异深度对比

### 3.1 消息格式

| 维度 | OpenAI | Anthropic | Gemini | Ollama |
|---|---|---|---|---|
| **API 端点** | `/v1/chat/completions` | `/v1/messages` | `generateContent` | `/api/chat` |
| **角色类型** | system, user, assistant, tool | user, assistant | user, model | system, user, assistant, tool |
| **系统提示位置** | messages 数组内 | 顶层 `system` 参数 | 顶层 `system_instruction` | messages 数组内 |
| **内容字段** | `content`（string\|array） | `content`（块数组） | `parts`（Part 数组） | `content`（string） |

### 3.2 Tool Calling 格式对比

这是跨提供商抽象的**核心难点**。三家主要提供商的格式差异巨大：

**工具定义格式：**

```
OpenAI:    {"type": "function", "function": {"name": "...", "parameters": {...}}}
Anthropic: {"name": "...", "input_schema": {...}}
Gemini:    {"function_declarations": [{"name": "...", "parameters": {...}}]}
Ollama:    {"type": "function", "function": {"name": "...", "parameters": {...}}}  // 与 OpenAI 一致
```

**调用结果回传：**

| 提供商 | 返回位置 | 结果角色 | 标识方式 |
|---|---|---|---|
| OpenAI | `choices[].message.tool_calls` | `role: "tool"` | tool_call ID |
| Anthropic | `content[]` 中 `type: "tool_use"` 块 | `role: "user"` + `tool_result` 块 | tool_use ID |
| Gemini | `parts[]` 中 `functionCall` 对象 | `functionResponse`（Part） | 函数名（非 ID） |
| Ollama | `message.tool_calls` | `role: "tool"` | tool_call ID |

### 3.3 流式协议

四家均使用 SSE（Server-Sent Events），但块结构不同：

| 提供商 | 触发方式 | Token 用量位置 | Go SDK 流式模式 |
|---|---|---|---|
| OpenAI | `stream: true` | 最后一个 chunk | `stream.Next()` 迭代器 |
| Anthropic | `stream: true` | 开头（input）+ 末尾（output） | `stream.Next()` 迭代器 |
| Gemini | 改用 `streamGenerateContent` URI | 每个 chunk 含部分用量 | `iter.Seq2` range |
| Ollama | `stream: true`（默认开启） | 最后一个 chunk | 回调函数 |

### 3.4 结构化输出

| 提供商 | 方法 | 可靠性 |
|---|---|---|
| OpenAI | `response_format` + `json_schema` | 服务端强制执行，高可靠 |
| Anthropic | Tool Use + `input_schema` 模拟 | 最高（96.67%-100%） |
| Gemini | `response_schema` + `response_mime_type` | 静默忽略不支持的约束 |
| Ollama | `format: json` + JSON Schema | 依赖模型能力，小模型不稳定 |

### 3.5 特色能力差异

| 能力 | OpenAI | Anthropic | Gemini | Ollama |
|---|---|---|---|---|
| **Extended Thinking** | — | BudgetTokens 控制 | — | Think 字段（模型依赖） |
| **Prompt Caching** | 自动 | 手动标记 CacheControl | Context Caching | — |
| **内置搜索** | web_search 工具 | — | GoogleSearch 工具 | — |
| **代码执行** | code_interpreter | — | CodeExecution 工具 | — |
| **实时双向流** | Realtime API | — | Live API | — |
| **本地部署** | — | — | — | 原生支持 |

---

## 4. CLI 工具链可用性评估

### 4.1 CLI 对比矩阵

| 维度 | Claude Code CLI | Codex CLI (OpenAI) | Gemini CLI | Ollama CLI |
|---|---|---|---|---|
| **运行时** | Node.js/Rust | Node.js/Rust | Node.js | Go（原生二进制） |
| **安装** | `npm i -g @anthropic-ai/claude-code` | `npm i -g @openai/codex` | `npx @google/gemini-cli` | 系统包管理器 |
| **JSON 输出** | `--output-format json` | 有限 | `--output-format json` | — |
| **流式 JSON** | `--output-format stream-json` | — | `--output-format stream-json` | — |
| **非交互模式** | `-p "prompt"` | `-q "prompt"` | `-p "prompt"` | `ollama run model "prompt"` |
| **工具使用** | 完整（文件读写、Shell 等） | 完整（沙盒保护） | 完整（Shell、Web Fetch） | — |
| **MCP 支持** | 完整 | 完整 | 支持 | — |
| **会话恢复** | `--resume SESSION_ID` | `codex resume` | — | — |
| **脚本化能力** | 优秀（NDJSON 协议） | 中等 | 良好 | 基础（适合模型管理） |

### 4.2 Crux 当前 CLI 集成状态

Crux 已在 `drivers/llm/claude_cli.go` 中实现了对 Claude Code CLI 的完整集成：
- `claudeCliResponse` 解析 `--output-format json` 输出
- `claudeStreamEvent` 解析 `--output-format stream-json` 流
- `CommandBuilder` 抽象支持测试注入

**评估结论：** CLI 驱动模式作为快速原型是合理的，但生产环境应优先使用 SDK 直连，原因：
1. 消除子进程开销（启动延迟约 200-500ms）
2. 更精细的错误处理（类型化错误 vs 退出码解析）
3. 直接支持 Tool Calling 循环（无需解析中间 JSON）
4. 减少 Node.js 运行时依赖

---

## 5. 多 LLM 抽象层设计模式

### 5.1 Go 生态中的主要抽象框架

#### LangChainGo（`github.com/tmc/langchaingo`）

Go 生态最成熟的 LLM 框架，10+ Provider 支持。

```go
type Model interface {
    GenerateContent(ctx context.Context, messages []MessageContent,
        options ...CallOption) (*ContentResponse, error)
    Call(ctx context.Context, prompt string, options ...CallOption) (string, error)
}
```

- 流式通过 `WithStreamingFunc(func)` CallOption 注入
- Tool Calling 统一为 `ToolCall` 结构
- 重量级框架，包含 chains、agents、vectorstores 等组件

#### any-llm-go（Mozilla.ai，2026-02）

2026 年最受关注的新库，"薄抽象层"理念——让模型选择成为配置而非架构。

```go
provider, _ := openai.New()
response, _ := provider.Completion(ctx, anyllm.CompletionParams{
    Model:    "gpt-4o-mini",
    Messages: []anyllm.Message{{Role: anyllm.RoleUser, Content: "Hello!"}},
})

// 流式用 Go channel
stream, _ := provider.CompletionStream(ctx, params)  // <-chan ChatCompletionChunk

// OpenAI 兼容 Provider 一行接入
provider, _ := openai.NewCompatible(openai.CompatibleConfig{
    APIKeyEnvVar:   "GROQ_API_KEY",
    DefaultBaseURL: "https://api.groq.com/openai/v1",
    Name:           "groq",
})
```

- **流式用 channel**：与 Crux 的 `<-chan StreamEvent` 设计完全一致
- **类型化错误**：`ErrRateLimit`、`ErrAuthentication`、`ErrContextLength` 等 sentinel error
- **局限**：Tool Calling 跨 Provider 抽象尚未实现

#### Eino（字节跳动/CloudWeGo）

生产级框架，在抖音、豆包等产品中实战验证。**接口隔离设计最为典范：**

```go
type BaseChatModel interface {
    Generate(ctx context.Context, input []*schema.Message, opts ...Option) (*schema.Message, error)
    Stream(ctx context.Context, input []*schema.Message, opts ...Option) (
        *schema.StreamReader[*schema.Message], error)
}

type ToolCallingChatModel interface {
    BaseChatModel
    // WithTools 返回绑定了指定工具的新实例
}
```

- 基础 vs Tool Calling 能力分离
- 内置断路器、指数退避、10,000+ QPS 生产验证
- 流式返回泛型 `StreamReader[T]`

### 5.2 核心设计模式分析

#### 模式 1：Adapter（适配器）— 最主流

每个 Provider 包装在统一接口后面。**Crux 的 `LLMDriver` 已采用此模式。**

#### 模式 2：Strategy + Registry（策略 + 注册表）

动态选择 Provider。**Crux 的 `DriverRegistry` 已实现。**

#### 模式 3：Interface Segregation（接口隔离）

Eino 的做法最值得借鉴：基础能力和高级能力通过独立接口声明，运行时通过类型断言检测。

#### 模式 4：OpenAI 兼容基座

any-llm-go 引入：大量 Provider 暴露 OpenAI 兼容 API，共享 OpenAI 兼容基础实现，新 Provider 只需配置 endpoint。

#### 模式 5：能力检测（Feature Detection）

| 框架 | 方法 |
|---|---|
| LangChainGo | `ReasoningModel` 接口 + `SupportsReasoning()` |
| Eino | `ToolCallingChatModel` 接口类型断言 |
| any-llm-go | `Capabilities` 结构体 |
| Continue | Provider 级 `supportsImages()` + 模型映射表 |

---

## 6. Crux 现有架构分析与扩展建议

### 6.1 现有架构评估

Crux 当前 LLM 驱动层（`drivers/llm/`）已具备良好基础：

| 特性 | Crux 现状 | 业界最佳实践 | 评估 |
|---|---|---|---|
| 基础接口 | `Call` + `Stream` + `Info` | 与 any-llm-go 一致 | 优秀 |
| 流式设计 | `<-chan StreamEvent` | Go channel（any-llm-go） | 完全吻合 |
| Provider 注册 | `DriverRegistry`（路径映射） | Strategy 模式 | 已对齐 |
| Functional Options | `ClaudeCliOption` | 业界标准 | 已对齐 |
| Tool Calling | 未实现 | 需要扩展接口 | 待扩展 |
| 多模态 | 未实现 | 需要扩展 Request 类型 | 待扩展 |
| 能力检测 | 未实现 | 接口断言 + Capabilities | 待实现 |
| 错误规范化 | 未实现 | sentinel error | 待实现 |

### 6.2 推荐接口扩展方案

基于 Eino 的接口隔离原则和 any-llm-go 的能力检测模式，建议如下分层设计：

```go
// ===== 核心接口（保持不变）=====

type LLMDriver interface {
    Call(ctx context.Context, req LLMRequest) (*LLMResponse, error)
    Stream(ctx context.Context, req LLMRequest) (<-chan StreamEvent, error)
    Info() DriverInfo
}

// ===== 扩展能力接口（可选实现）=====

// ToolCallingDriver 扩展 LLMDriver，支持工具调用
type ToolCallingDriver interface {
    LLMDriver
    CallWithTools(ctx context.Context, req LLMRequest, tools []ToolDef) (*LLMResponse, error)
    StreamWithTools(ctx context.Context, req LLMRequest, tools []ToolDef) (<-chan StreamEvent, error)
}

// MultimodalDriver 扩展 LLMDriver，支持多模态输入
type MultimodalDriver interface {
    LLMDriver
    SupportedMediaTypes() []string // "image/png", "application/pdf", etc.
}

// ReasoningDriver 扩展 LLMDriver，支持扩展思考
type ReasoningDriver interface {
    LLMDriver
    SupportsReasoning() bool
}

// ===== 能力检测（运行时）=====

// 使用方通过类型断言检测能力
func callWithToolsIfSupported(d LLMDriver, ...) {
    if td, ok := d.(ToolCallingDriver); ok {
        td.CallWithTools(ctx, req, tools)
    } else {
        // fallback: 将工具描述注入 system prompt
    }
}
```

### 6.3 LLMRequest 扩展建议

```go
type LLMRequest struct {
    // 现有字段
    Intent       string `json:"intent"`
    SystemPrompt string `json:"system_prompt,omitempty"`
    Model        string `json:"model,omitempty"`
    MaxTurns     int    `json:"max_turns,omitempty"`
    TimeoutMs    int64  `json:"timeout_ms,omitempty"`

    // 新增：对话历史（多轮对话必需）
    Messages []Message `json:"messages,omitempty"`

    // 新增：生成参数
    Temperature *float32 `json:"temperature,omitempty"`
    MaxTokens   int      `json:"max_tokens,omitempty"`
}

type Message struct {
    Role    string `json:"role"`    // "system", "user", "assistant", "tool"
    Content string `json:"content"`
}
```

### 6.4 类型化错误规范

```go
// 统一错误类型，支持 errors.Is/As 匹配
var (
    ErrRateLimit     = errors.New("llm: rate limit exceeded")
    ErrAuth          = errors.New("llm: authentication failed")
    ErrContextLength = errors.New("llm: context length exceeded")
    ErrModelNotFound = errors.New("llm: model not found")
    ErrTimeout       = errors.New("llm: request timed out")
)

// 包装错误保留提供商原始信息
type DriverError struct {
    Provider   string
    StatusCode int
    Err        error
}
```

### 6.5 驱动实现策略

#### 优先级 1：SDK 直连驱动（核心提供商）

| 驱动 | SDK | 优势 |
|---|---|---|
| `AnthropicSDKDriver` | `anthropic-sdk-go` v1.26.0 | ToolRunner 自动循环、Extended Thinking、Prompt Caching |
| `OpenAISDKDriver` | `openai-go` v3.26.0 | Responses API、内置搜索/代码解释器 |
| `GeminiSDKDriver` | `go-genai` v1.49.0 | 双后端（API + Vertex AI）、Live API |

#### 优先级 2：OpenAI 兼容基座驱动

```go
// 一个实现覆盖多个 Provider
type OpenAICompatDriver struct {
    baseURL    string
    apiKey     string
    name       string
    httpClient *http.Client
}

// 注册时只需配置 endpoint
registry.Register("ollama", NewOpenAICompatDriver("http://localhost:11434/v1", "", "ollama"))
registry.Register("groq", NewOpenAICompatDriver("https://api.groq.com/openai/v1", groqKey, "groq"))
registry.Register("deepseek", NewOpenAICompatDriver("https://api.deepseek.com/v1", dsKey, "deepseek"))
```

#### 优先级 3：CLI 驱动（保留为 fallback）

现有 `ClaudeCliDriver` 保留，作为 SDK 驱动不可用时的降级路径。

---

## 7. 实施路线图与风险评估

### 7.1 分阶段实施建议

**Phase 1 — 接口扩展与错误规范化**
- 扩展 `LLMRequest`（Messages、Temperature、MaxTokens）
- 定义 `ToolCallingDriver` 扩展接口
- 实现类型化错误（sentinel errors + `DriverError`）
- 保持 `ClaudeCliDriver` 向后兼容

**Phase 2 — 首个 SDK 直连驱动**
- 实现 `AnthropicSDKDriver`（与现有 CLI 驱动对比验证）
- 或实现 `OpenAICompatDriver` 基座（一次覆盖 Ollama + 其他兼容提供商）
- 选择取决于业务优先级：生产质量 vs 本地开发灵活性

**Phase 3 — 完整多提供商覆盖**
- 实现 `OpenAISDKDriver`
- 实现 `GeminiSDKDriver`
- OpenAI 兼容基座接入 Groq、DeepSeek 等

**Phase 4 — 高级能力**
- Tool Calling 跨提供商抽象
- 多模态输入统一
- Provider 自动切换与 fallback 链

### 7.2 风险评估

| 风险 | 等级 | 缓解措施 |
|---|---|---|
| Tool Calling 格式分裂导致抽象层复杂度爆炸 | **高** | 先实现 Provider 级别的独立 Tool Calling，再逐步提取共性 |
| SDK 版本快速迭代导致 breaking change | **中** | 锁定主版本（v3/v1），关注 release notes |
| Ollama 本地模型 Tool Calling 不稳定 | **中** | 限制支持模型列表（Qwen3 32B+），添加能力检测 |
| 多 SDK 依赖膨胀 go.mod | **低** | 使用 build tags 按需编译，或独立子模块 |
| 流式适配复杂度（回调 vs 迭代器 vs channel） | **低** | 统一转换为 channel 模式，已有成熟模式可参考 |

### 7.3 依赖影响评估

当前 `go.mod` 依赖轻量。引入 SDK 后的新增依赖：

| SDK | 核心依赖 | 传递依赖量 |
|---|---|---|
| `openai-go` | 标准库为主 | 少（自动生成，最小依赖） |
| `anthropic-sdk-go` | 标准库 + `invopop/jsonschema` | 少 |
| `go-genai` | 标准库 + Google 认证库 | 中（GCP 生态） |
| `ollama/api` | 标准库 | 少（但会拉入 ollama 整体 module） |

**建议：** Ollama 可考虑直接使用 OpenAI 兼容端点（`/v1/chat/completions`），避免引入整个 `ollama/ollama` 模块。

---

## 8. 参考资料与信息源

### SDK 仓库与文档

- [openai/openai-go](https://github.com/openai/openai-go) — OpenAI 官方 Go SDK
- [anthropics/anthropic-sdk-go](https://github.com/anthropics/anthropic-sdk-go) — Anthropic 官方 Go SDK
- [googleapis/go-genai](https://github.com/googleapis/go-genai) — Google Gemini 统一 Go SDK
- [ollama/ollama](https://github.com/ollama/ollama) — Ollama 项目及 Go 客户端
- [pkg.go.dev/github.com/openai/openai-go/v3](https://pkg.go.dev/github.com/openai/openai-go/v3)
- [pkg.go.dev/github.com/anthropics/anthropic-sdk-go](https://pkg.go.dev/github.com/anthropics/anthropic-sdk-go)
- [pkg.go.dev/google.golang.org/genai](https://pkg.go.dev/google.golang.org/genai)

### 抽象框架

- [tmc/langchaingo](https://github.com/tmc/langchaingo) — LangChainGo
- [Mozilla any-llm-go 博客](https://blog.mozilla.ai/run-openai-claude-mistral-llamafile-and-more-from-one-interface-now-in-go/)
- [cloudwego/eino](https://github.com/cloudwego/eino) — 字节跳动 Eino 框架
- [teilomillet/gollm](https://github.com/teilomillet/gollm) — gollm 统一接口

### CLI 工具

- [Claude Code Headless Mode](https://code.claude.com/docs/en/headless)
- [Codex CLI](https://developers.openai.com/codex/cli/) — OpenAI Codex CLI
- [Gemini CLI](https://github.com/google-gemini/gemini-cli)
- [Ollama CLI Reference](https://docs.ollama.com/cli)

### API 对比与设计模式

- [OpenAI vs Anthropic vs Gemini API 比较](https://www.eesel.ai/blog/openai-api-vs-anthropic-api-vs-gemini-api)
- [Mastra 工具兼容层](https://mastra.ai/blog/mcp-tool-compatibility-layer) — Tool Calling 跨 Provider 兼容性测试
- [LLM 抽象层必要性](https://www.proxai.co/blog/archive/llm-abstraction-layer)
- [OpenAI API 兼容性追踪](https://llm-tracker.info/howto/OpenAI-API-Compatibility)
- [Go 官方博客：构建 LLM 应用](https://go.dev/blog/llmpowered)

### 结构化输出与兼容性

- [结构化输出跨 Provider 比较](https://www.glukhov.org/post/2025/10/structured-output-comparison-popular-llm-providers)
- [Ollama OpenAI 兼容文档](https://docs.ollama.com/api/openai-compatibility)
- [Portkey API 格式比较](https://portkey.ai/blog/open-ai-responses-api-vs-chat-completions-vs-anthropic-anthropic-messages-api/)

---

**技术研究完成日期：** 2026-03-06
**研究周期：** 当前综合技术分析
**信息源验证：** 所有技术事实均通过当前 Web 数据交叉验证
**置信度：** 高 — 基于多个权威技术信息源

_本技术研究报告为 Crux Agent OS 的多 LLM 提供商集成设计提供了全面的技术参考，涵盖 API 差异、SDK 评估、CLI 可用性和抽象层设计模式的深入分析。_
