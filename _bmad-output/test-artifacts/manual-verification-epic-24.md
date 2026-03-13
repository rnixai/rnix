# Epic 24 手工验证指南：LLM Serve — OpenAI 兼容网关

## 概述

本文档提供 Epic 24 所有 Story 的手工验证步骤，用于在自动化测试之外对功能进行端到端的人工确认。Epic 24 通过 `rnix serve` 命令启动 OpenAI 兼容 HTTP 服务器，将已注册的 `/dev/llm/*` provider 暴露为标准 OpenAI API 端点，让外部工具（Aider、Open WebUI、Python `openai` 库等）无需了解 Rnix 内部即可消费 LLM 能力。

## 前置准备

### 构建

```bash
make build
```

### 理解 rnix serve 运行模型

`rnix serve` 是一个**独立的前台 HTTP 服务器**，不依赖 daemon 运行。它直接读取 `rnix-providers.yaml` 配置、创建自己的 DriverRegistry、执行健康检查，然后监听 HTTP 端口。

| 命令 | 说明 |
|------|------|
| `./rnix serve` | 启动 HTTP 服务器，默认 `127.0.0.1:8080`，Ctrl+C 停止 |
| `./rnix serve --port 9090` | 指定监听端口 |

**重要**：
- `rnix serve` 在**前台**运行，终端会阻塞直到 Ctrl+C 或收到 SIGTERM
- 需要在**另一个终端**中执行 curl 命令进行验证
- `rnix serve` **不启动 daemon**，它独立运行

### 准备配置文件

```bash
# 确保旧 daemon 已停止（避免端口冲突等干扰）
./rnix daemon stop 2>/dev/null; true

# 准备测试用 rnix-providers.yaml（放到项目根目录）
cat > rnix-providers.yaml << 'EOF'
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
EOF
```

### 可选环境配置

```bash
# 如果测试 Groq，设置 API Key
export GROQ_API_KEY="your-groq-api-key-here"

# (可选) 如果测试 Ollama，确认 Ollama 本地服务运行
curl -s http://localhost:11434/v1/models | head -5
```

### 验证所需工具

```bash
# 确认 curl 可用
curl --version

# (可选) jq 用于格式化 JSON 输出
jq --version
```

> **Token 消耗提示**：`/v1/chat/completions` 调用会触发真实 LLM 推理产生费用。对于仅验证 HTTP 框架、错误处理、模型列表等场景，使用 `/health` 和 `/v1/models` 端点无需 LLM 调用。

---

## Story 24.1: OpenAI HTTP Server 核心框架

### 服务器启动与 /health 端点

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 1 | 默认端口启动 | (1) 终端 1：`./rnix serve` (2) 观察 stderr 输出 | 输出 `Serving N providers on http://127.0.0.1:8080`（N 为已注册 provider 数） | [ ] |
| 2 | /health 端点 | (1) 终端 2：`curl -s http://127.0.0.1:8080/health \| jq .` | 返回 HTTP 200，JSON 含 `"status": "ok"` 和 `"providers": N`（N ≥ 1） | [ ] |
| 3 | 自定义端口 | (1) Ctrl+C 停止终端 1 (2) `./rnix serve --port 9090` (3) 终端 2：`curl -s http://127.0.0.1:9090/health` | /health 在 9090 端口可达，返回正常响应 | [ ] |
| 4 | 标准配置 4 provider | (1) 使用前置准备的 4 provider 配置 (2) `./rnix serve` (3) `curl -s http://127.0.0.1:8080/health \| jq .providers` | `providers` 值为 4 | [ ] |

### parseModel 路由逻辑（通过 /v1/chat/completions 间接验证）

> 以下场景通过 API 调用间接验证 parseModel 解析逻辑。不可用的 provider 会返回上游错误（非 404），说明路由解析正确。

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 5 | provider:model 格式 | 终端 2：`curl -s http://127.0.0.1:8080/v1/chat/completions -H "Content-Type: application/json" -d '{"model":"claude:haiku","messages":[{"role":"user","content":"hi"}]}'` | 路由到 claude provider 的 haiku 模型，返回有效响应或 claude 相关的错误（非 404） | [ ] |
| 6 | 仅 provider 名 | 终端 2：`curl -s http://127.0.0.1:8080/v1/chat/completions -H "Content-Type: application/json" -d '{"model":"claude","messages":[{"role":"user","content":"hi"}]}'` | 路由到 claude provider 使用其 default_model，返回有效响应或 claude 相关的错误 | [ ] |
| 7 | 不存在的 provider | 终端 2：`curl -s http://127.0.0.1:8080/v1/chat/completions -H "Content-Type: application/json" -d '{"model":"nonexist","messages":[{"role":"user","content":"hi"}]}' \| jq .` | HTTP 404，JSON 含 `"code": "model_not_found"` 和 `Available providers` 提示列出全部已注册 provider | [ ] |

### 安全绑定（NFR52）

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 8 | 仅本地可达 | (1) `./rnix serve` 运行中 (2) `ss -tlnp \| grep 8080` 或 `netstat -tlnp \| grep 8080` | 监听地址为 `127.0.0.1:8080`（非 `0.0.0.0:8080`） | [ ] |

### 错误响应格式

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 9 | 请求体格式错误 | 终端 2：`curl -s -X POST http://127.0.0.1:8080/v1/chat/completions -H "Content-Type: application/json" -d 'invalid json' \| jq .` | HTTP 400，JSON 格式 `{"error": {"message": "...", "type": "invalid_request_error", "code": "invalid_request"}}` | [ ] |
| 10 | 缺少 model 字段 | 终端 2：`curl -s -X POST http://127.0.0.1:8080/v1/chat/completions -H "Content-Type: application/json" -d '{"messages":[{"role":"user","content":"hi"}]}' \| jq .` | HTTP 400，错误信息包含 `'model' is required` | [ ] |
| 11 | 缺少 messages 字段 | 终端 2：`curl -s -X POST http://127.0.0.1:8080/v1/chat/completions -H "Content-Type: application/json" -d '{"model":"claude"}' \| jq .` | HTTP 400，错误信息包含 `'messages' must be a non-empty array` | [ ] |

---

## Story 24.2: /v1/chat/completions 同步模式

### 同步请求正常流程

> **前提**：终端 1 运行 `./rnix serve`，以下命令在终端 2 执行。需要至少一个可用的 LLM provider（如 claude 或 ollama）。

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 1 | 基本同步请求 | `curl -s http://127.0.0.1:8080/v1/chat/completions -H "Content-Type: application/json" -d '{"model":"claude","messages":[{"role":"user","content":"say hello in one word"}],"stream":false}' \| jq .` | 返回 HTTP 200，JSON 含 `"object": "chat.completion"`、`"choices"` 数组（含 `message.role: "assistant"` 和 `message.content`）、`"usage"` 统计 | [ ] |
| 2 | 响应 ID 格式 | 检查场景 1 的响应 | `"id"` 字段以 `"chatcmpl-"` 开头 | [ ] |
| 3 | finish_reason | 检查场景 1 的响应 | `choices[0].finish_reason` 为 `"stop"` | [ ] |
| 4 | Content-Type 头 | `curl -sI -X POST http://127.0.0.1:8080/v1/chat/completions -H "Content-Type: application/json" -d '{"model":"claude","messages":[{"role":"user","content":"hi"}]}'` | 响应头含 `Content-Type: application/json` | [ ] |

### 消息转换

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 5 | 多轮对话 | `curl -s http://127.0.0.1:8080/v1/chat/completions -H "Content-Type: application/json" -d '{"model":"claude","messages":[{"role":"system","content":"reply in uppercase only"},{"role":"user","content":"hello"}],"stream":false}' \| jq .choices[0].message.content` | 响应内容为大写（说明 system message 正确传递到 LLM） | [ ] |
| 6 | provider:model 格式请求 | `curl -s http://127.0.0.1:8080/v1/chat/completions -H "Content-Type: application/json" -d '{"model":"claude:haiku","messages":[{"role":"user","content":"say ok"}],"stream":false}' \| jq .model` | 响应中 `"model"` 字段为 `"claude:haiku"` | [ ] |

### 错误处理

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 7 | 不存在的 provider | `curl -s http://127.0.0.1:8080/v1/chat/completions -H "Content-Type: application/json" -d '{"model":"nonexist","messages":[{"role":"user","content":"hi"}]}' \| jq .` | HTTP 404，`error.code: "model_not_found"`，`error.message` 包含 `Available providers` 列表 | [ ] |
| 8 | 不可达 provider | `curl -s http://127.0.0.1:8080/v1/chat/completions -H "Content-Type: application/json" -d '{"model":"ollama","messages":[{"role":"user","content":"hi"}]}'`（Ollama 未运行） | HTTP 502，`error.code: "upstream_error"`，错误信息不泄露内部堆栈详情 | [ ] |
| 9 | 超大请求体 | `python3 -c "print('{\"model\":\"claude\",\"messages\":[{\"role\":\"user\",\"content\":\"' + 'x'*5000000 + '\"}]}')" \| curl -s -X POST http://127.0.0.1:8080/v1/chat/completions -H "Content-Type: application/json" -d @- \| jq .` | HTTP 400，被 MaxBytesReader 4MB 限制拦截，返回 `invalid_request` 错误 | [ ] |
| 10 | 空 messages 数组 | `curl -s http://127.0.0.1:8080/v1/chat/completions -H "Content-Type: application/json" -d '{"model":"claude","messages":[]}' \| jq .` | HTTP 400，`error.message` 包含 `'messages' must be a non-empty array` | [ ] |

---

## Story 24.3: SSE 流式响应

### 流式请求正常流程

> **前提**：终端 1 运行 `./rnix serve`，以下命令在终端 2 执行。

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 1 | 基本流式请求 | `curl -sN http://127.0.0.1:8080/v1/chat/completions -H "Content-Type: application/json" -d '{"model":"claude","messages":[{"role":"user","content":"count 1 to 5"}],"stream":true}'` | 输出多行 `data: {...}\n\n` 格式的 SSE 事件，每行可解析为 JSON | [ ] |
| 2 | [DONE] 终止标记 | 观察场景 1 的输出尾部 | 最后一行为 `data: [DONE]`（流结束标记） | [ ] |
| 3 | SSE 响应头 | `curl -sI -X POST http://127.0.0.1:8080/v1/chat/completions -H "Content-Type: application/json" -d '{"model":"claude","messages":[{"role":"user","content":"hi"}],"stream":true}'` 或观察 `-v` 输出 | 响应头含：`Content-Type: text/event-stream`、`Cache-Control: no-cache`、`Connection: keep-alive` | [ ] |

### Chunk 格式验证

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 4 | chunk 对象类型 | 观察场景 1 的 SSE data 行，解析 JSON | 每个 chunk 含 `"object": "chat.completion.chunk"` | [ ] |
| 5 | chunk ID 一致 | 比较多个 chunk 的 `id` 字段 | 同一流中所有 chunk 共享相同的 `id`（以 `chatcmpl-` 开头） | [ ] |
| 6 | 第一个内容 chunk 含 role | 解析第一个含 `delta.content` 的 chunk | `delta.role` 为 `"assistant"`（仅第一个内容 chunk 有） | [ ] |
| 7 | 后续 chunk 无 role | 解析第二个及以后的内容 chunk | `delta` 中无 `role` 字段（omitempty） | [ ] |
| 8 | 结束 chunk | 解析 `[DONE]` 之前的最后一个 data chunk | `choices[0].finish_reason` 为 `"stop"` | [ ] |

### 流式错误处理

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 9 | 流启动失败 | `curl -sN http://127.0.0.1:8080/v1/chat/completions -H "Content-Type: application/json" -d '{"model":"ollama","messages":[{"role":"user","content":"hi"}],"stream":true}'`（Ollama 未运行） | 返回 HTTP 502 JSON 错误（流尚未开始，仍可返回标准错误格式） | [ ] |
| 10 | 不存在 provider 流式 | `curl -sN http://127.0.0.1:8080/v1/chat/completions -H "Content-Type: application/json" -d '{"model":"nonexist","messages":[{"role":"user","content":"hi"}],"stream":true}' \| head -5` | HTTP 404，标准 JSON 错误格式（stream 参数不影响 provider 查找错误） | [ ] |

### 实时性验证

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 11 | 逐 chunk 输出 | 执行场景 1 并观察终端输出 | 数据**逐行实时出现**（非等全部完成后一次性输出），说明每个 chunk 后 Flush 生效 | [ ] |

---

## Story 24.4: /v1/models Provider 发现

### 模型列表基础功能

> **前提**：终端 1 运行 `./rnix serve`（使用标准 4 provider 配置）。

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 1 | 基础模型列表 | `curl -s http://127.0.0.1:8080/v1/models \| jq .` | 返回 HTTP 200，JSON 含 `"object": "list"` 和 `"data"` 数组 | [ ] |
| 2 | 响应格式验证 | 检查场景 1 的 data 数组元素 | 每个 entry 含 `"id"`、`"object": "model"`、`"created"`（UNIX 时间戳）、`"owned_by"` 字段 | [ ] |
| 3 | provider 基础 entry | 检查场景 1 的 data | 含 `id: "claude"` 的 entry（owned_by 也为 `"claude"`） | [ ] |
| 4 | provider:model entry | 检查场景 1 的 data | 含 `id: "claude:haiku"` 的 entry（因为 claude 配置了 `default_model: haiku`） | [ ] |
| 5 | 无 default_model 的 provider | 检查 cursor 相关 entry | 仅有 `id: "cursor"` 基础 entry，**没有** `cursor:xxx` 的复合 entry（cursor 未配置 default_model） | [ ] |

### 健康状态过滤

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 6 | 排除 unhealthy provider | (1) 确保 Ollama 服务**未运行** (2) `./rnix serve` (3) 等待 3-5 秒（健康检查完成） (4) `curl -s http://127.0.0.1:8080/v1/models \| jq '.data[].id'` | 输出中**不包含** `"ollama"` 或 `"ollama:llama3"`（因健康检查失败被排除） | [ ] |
| 7 | 包含 unchecked provider | 检查场景 6 的输出 | 输出中包含 `"claude"` 和 `"cursor"`（CLI driver 状态为 unchecked，不被排除） | [ ] |
| 8 | 包含 healthy provider | (1) 如果 Groq API Key 有效 (2) 检查场景 6 的输出 | `"groq"` 和 `"groq:llama-3.3-70b-versatile"` 出现在列表中（healthy 状态） | [ ] |

### 边界场景

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 9 | 最小配置 | (1) 修改 `rnix-providers.yaml` 仅保留 1 个 provider（如 claude） (2) 重启 `./rnix serve` (3) `curl -s http://127.0.0.1:8080/v1/models \| jq '.data \| length'` | 返回 2（claude 基础 entry + claude:haiku 复合 entry） | [ ] |
| 10 | HTTP GET 方法 | `curl -s -X GET http://127.0.0.1:8080/v1/models \| jq .object` | 返回 `"list"`（GET 方法正确处理） | [ ] |

---

## Story 24.5: rnix serve CLI 命令与端到端集成

### CLI 命令基础

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 1 | help 输出 | `./rnix serve --help` | 显示命令描述 `Start OpenAI-compatible HTTP gateway`，`--port` 参数说明（默认 8080） | [ ] |
| 2 | 默认端口 8080 | `./rnix serve &` 然后 `curl -s http://127.0.0.1:8080/health`，完成后 `kill %1` | /health 在 8080 端口可达 | [ ] |
| 3 | 自定义端口 | `./rnix serve --port 19090 &` 然后 `curl -s http://127.0.0.1:19090/health`，完成后 `kill %1` | /health 在 19090 端口可达 | [ ] |
| 4 | 非法端口 | `./rnix serve --port 0` | 报错退出，信息包含 `invalid port` 和 `must be between 1 and 65535` | [ ] |
| 5 | 超大端口 | `./rnix serve --port 70000` | 报错退出，信息包含 `invalid port` 和 `must be between 1 and 65535` | [ ] |

### 启动消息

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 6 | 启动消息内容 | `./rnix serve 2>&1 \| head -1` | 输出格式为 `Serving N providers on http://127.0.0.1:8080`（N 为实际 provider 数量） | [ ] |
| 7 | 消息输出到 stderr | `./rnix serve 2>/dev/null &` 然后 `curl -s http://127.0.0.1:8080/health`，完成后 `kill %1` | 将 stderr 重定向到 /dev/null 后终端无启动消息，但服务正常运行 | [ ] |

### Provider 配置集成

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 8 | 共享 rnix-providers.yaml | (1) 使用 4 provider 配置 (2) `./rnix serve` (3) `curl -s http://127.0.0.1:8080/health \| jq .providers` | providers 数量为 4（与配置文件一致） | [ ] |
| 9 | 无配置文件回退 | (1) `mv rnix-providers.yaml rnix-providers.yaml.bak` (2) `./rnix serve` (3) `curl -s http://127.0.0.1:8080/health \| jq .providers` | 使用默认配置（claude + cursor），providers 为 2 | [ ] |
| 10 | 恢复配置 | `mv rnix-providers.yaml.bak rnix-providers.yaml` | 恢复后续测试环境 | [ ] |
| 11 | 配置错误拒绝启动 | (1) 在 `rnix-providers.yaml` 中制造语法错误 (2) `./rnix serve` | 报错退出，包含配置加载相关错误信息 | [ ] |

### 优雅停止

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 12 | SIGINT 停止 | (1) 终端 1：`./rnix serve` (2) Ctrl+C | 服务器正常退出，无错误输出，退出码 0 | [ ] |
| 13 | SIGTERM 停止 | (1) `./rnix serve &` (2) `kill $!` | 服务器正常退出，无错误输出 | [ ] |

### 并发连接（NFR51）

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 14 | 10 并发 /health | (1) `./rnix serve` 运行中 (2) 终端 2：`for i in $(seq 1 10); do curl -s http://127.0.0.1:8080/health & done; wait` | 所有 10 个请求均返回 `"status": "ok"`，无请求失败 | [ ] |

---

## 端到端完整流程验证

> **前提**：使用标准 4 provider 配置，至少 claude provider 可用。

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 1 | 启动到请求全链路 | (1) 恢复标准配置 (2) `./rnix serve` (3) `curl -s http://127.0.0.1:8080/health` (4) `curl -s http://127.0.0.1:8080/v1/models \| jq .data[0]` (5) `curl -s http://127.0.0.1:8080/v1/chat/completions -H "Content-Type: application/json" -d '{"model":"claude","messages":[{"role":"user","content":"say ok"}]}'` | 三个端点均返回有效响应 | [ ] |
| 2 | 同步 + 流式切换 | (1) 同步：`curl -s ... -d '{"model":"claude","messages":[...],"stream":false}'` (2) 流式：`curl -sN ... -d '{"model":"claude","messages":[...],"stream":true}'` | 同一 provider 同步和流式模式均正常工作 | [ ] |
| 3 | 多 Provider 路由 | (1) `curl ... -d '{"model":"claude",...}'` (2) `curl ... -d '{"model":"ollama",...}'`（如可用） | 两次请求分别路由到不同 provider（通过响应内容或错误类型区分） | [ ] |
| 4 | 模型发现 + 过滤 | (1) 确保 Ollama 未运行 (2) `./rnix serve` (3) 等待 5 秒 (4) `curl -s http://127.0.0.1:8080/v1/models \| jq '.data[].id'` | 列表中不含 ollama（unhealthy 被排除），其他 provider 正常列出 | [ ] |
| 5 | 错误消息友好性 | (1) `curl -s http://127.0.0.1:8080/v1/chat/completions -H "Content-Type: application/json" -d '{"model":"nonexist","messages":[{"role":"user","content":"hi"}]}' \| jq .error.message` | 错误消息清晰列出所有可用 provider | [ ] |
| 6 | Python openai 库兼容（可选） | (1) `pip install openai` (2) 执行以下 Python 脚本（见下方） | client 正常连接并返回有效结果 | [ ] |

**Python openai 库测试脚本**（场景 6）：

```python
from openai import OpenAI

client = OpenAI(
    base_url="http://localhost:8080/v1",
    api_key="unused"  # rnix serve 不校验 API Key
)

# 同步请求
response = client.chat.completions.create(
    model="claude",
    messages=[{"role": "user", "content": "say hello"}]
)
print("Sync:", response.choices[0].message.content)

# 流式请求
stream = client.chat.completions.create(
    model="claude",
    messages=[{"role": "user", "content": "count 1 to 3"}],
    stream=True
)
print("Stream:", end=" ")
for chunk in stream:
    if chunk.choices[0].delta.content:
        print(chunk.choices[0].delta.content, end="", flush=True)
print()
```

---

## 测试清理

```bash
# 停止 rnix serve（如在后台运行）
kill $(pgrep -f "rnix serve") 2>/dev/null; true

# 恢复原始配置或删除测试配置
# rm rnix-providers.yaml  # 如需恢复无配置状态
```

---

## 关键注意事项

1. **rnix serve 独立运行** -- `rnix serve` 不依赖 daemon，直接加载 `rnix-providers.yaml` 并创建独立的 DriverRegistry。不需要先 `rnix daemon start`
2. **前台阻塞** -- `rnix serve` 在前台运行，需另开终端执行 curl 命令。后台运行可使用 `./rnix serve &` 并记住用 `kill %1` 停止
3. **健康检查延迟** -- 启动后健康检查在后台异步执行，`/v1/models` 的过滤结果需等待 3-5 秒才稳定
4. **三个端点** -- `/health`（GET）、`/v1/chat/completions`（POST）、`/v1/models`（GET）
5. **model 参数格式** -- `"provider"` 使用默认模型，`"provider:model"` 指定具体模型
6. **安全绑定** -- 仅监听 `127.0.0.1`，外部网络不可达（NFR52）
7. **请求体限制** -- 最大 4MB（MaxBytesReader），超出返回 400
8. **NFR50** -- HTTP 处理开销 ≤ 50ms（不含 LLM 推理时间）
9. **NFR51** -- 支持 ≥ 10 并发连接
10. **NFR52** -- 默认仅绑定 127.0.0.1
11. **SSE 格式** -- 每个事件格式为 `data: {json}\n\n`，流结束为 `data: [DONE]\n\n`
12. **错误格式统一** -- 所有错误遵循 OpenAI 格式：`{"error": {"message": "...", "type": "...", "code": "..."}}`
13. **不校验 API Key** -- `rnix serve` 本身不校验传入的 API Key（本地信任模型），但底层 provider 可能需要
14. **优雅关闭** -- SIGINT/SIGTERM 触发 5 秒超时的优雅关闭

## 验证记录

| 字段 | 值 |
|------|-----|
| 验证人 | |
| 验证日期 | |
| 构建版本 | |
| 总用例数 | 56 |
| 通过数 | |
| 失败数 | |
| 备注 | |
