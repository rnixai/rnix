# Epic 23 手工验证指南：多 LLM Provider 动态配置

## 概述

本文档提供 Epic 23 所有 Story 的手工验证步骤，用于在自动化测试之外对功能进行端到端的人工确认。Epic 23 将 rnix 从单一 Claude CLI 演进为灵活的多模型架构，支持通过 `rnix-providers.yaml` 声明式定义多种 LLM provider。

## 前置准备

### 构建

```bash
make build
```

### Daemon 生命周期须知

rnix 采用后台 daemon 模型。理解以下行为对验证至关重要：

| 命令 | 是否自动启动 daemon | 说明 |
|------|:---:|------|
| `./rnix -i "..."` | **是** | 通过 `EnsureDaemon()` 自动启动 |
| `./rnix apply "..."` | **是** | 通过 `EnsureDaemon()` 自动启动 |
| `./rnix ps` | **否** | 直接连接 socket；daemon 未运行时显示 `No active processes.`（**不报错**，易误判） |
| `./rnix daemon status` | **否** | 直接连接 socket；daemon 未运行时显示 `status: stopped` |
| `./rnix daemon stop` | **否** | 向运行中的 daemon 发送停止指令 |

**重要**：`./rnix ps` 输出 "No active processes." 并不代表 daemon 正在运行。必须用 `./rnix daemon status` 确认 daemon 状态。

### 如何查看 daemon 日志

Daemon 以后台进程运行（stdout/stderr 均被丢弃），**正常使用时无法查看日志**。需要验证日志输出的场景（如 provider 注册信息、健康检查结果、API Key 警告等），必须以前台模式运行 daemon：

```bash
# 终端 1：前台运行 daemon（日志输出到当前终端的 stderr）
./rnix daemon --internal
# daemon 启动后会阻塞此终端，Ctrl+C 停止

# 终端 2：执行测试命令
./rnix daemon status
./rnix -i "hello"
# ...
```

前台 daemon 启动时，终端 1 会显示类似：
```
[llm] registered 4 providers: claude → /dev/llm/claude, cursor → /dev/llm/cursor, groq → /dev/llm/groq, ollama → /dev/llm/ollama
[llm] provider "groq": healthy
[llm] provider "ollama": health check failed: dial tcp 127.0.0.1:11434: connect: connection refused
```

### 准备配置文件

```bash
# 确保旧 daemon 已停止
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

# 5. (可选) 如果测试 Ollama，确认 Ollama 本地服务运行
curl -s http://localhost:11434/v1/models | head -5
```

> **Token 消耗提示**：每次 `./rnix -i "..."` 会触发 LLM 调用产生费用。对于仅需验证 daemon 启动和 provider 注册的场景，优先使用 `./rnix daemon --internal` + `./rnix daemon status` 组合，无需实际 LLM 调用。

---

## Story 23.1: rnix-providers.yaml 配置文件定义与解析

### 配置文件正常解析

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 1 | 完整配置解析 | (1) 确保前置准备的 `rnix-providers.yaml` 已就位（含 claude-cli、cursor-cli、openai-compat 三种驱动） (2) `./rnix daemon stop` (3) 终端 1：`./rnix daemon --internal` 观察 stderr 输出 | 终端 1 输出 `[llm] registered 4 providers: claude → /dev/llm/claude, cursor → /dev/llm/cursor, groq → /dev/llm/groq, ollama → /dev/llm/ollama` | [x] |
| 2 | 最小配置 | (1) 修改 `rnix-providers.yaml` 仅保留 claude 一个 provider (2) `Ctrl+C` 停止前台 daemon (3) 重新 `./rnix daemon --internal` | daemon 正常启动，输出 `[llm] registered 1 providers: claude → /dev/llm/claude` | [x] |
| 3 | 多 provider 配置 | (1) 在配置中添加 5+ 个 provider (2) 重启前台 daemon | 所有 provider 正确注册，终端 2 执行 `./rnix daemon status` 显示全部 provider | [x] |

### 配置错误处理

> **注意**：配置错误导致 daemon 拒绝启动。后台启动时错误被丢弃，需前台运行才能看到错误详情。

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 4 | YAML 语法错误 | (1) 在 `rnix-providers.yaml` 中制造语法错误（如 `name claude` 缺少冒号） (2) `./rnix daemon --internal` | 立即退出并输出错误，包含行号和具体格式问题（如 `line 3`） | [x] |
| 5 | 缺少 name 字段 | (1) 移除某个 provider 的 `name:` 行 (2) `./rnix daemon --internal` | 退出并输出错误，明确指出 `name` 为空或缺失 | [x] |
| 6 | 无效 driver 类型 | (1) 设置 `driver: invalid-driver` (2) `./rnix daemon --internal` | 退出并输出错误，指出 driver 类型非法 | [x] |
| 7 | openai-compat 缺少 base_url | (1) openai-compat 类型 provider 删除 `base_url` (2) `./rnix daemon --internal` | 退出并输出错误，指出 `base_url` 必填 | [x] |
| 8 | 重复 provider 名称 | (1) 添加两个 `name: claude` 的 provider (2) `./rnix daemon --internal` | 退出并输出错误，指出名称重复 | [x] |
| 9 | 非法名称字符 | (1) 设置 `name: "my provider!"` (2) `./rnix daemon --internal` | 退出并输出错误，指出名称包含非法字符 | [x] |
| 10 | 多个错误同时 | (1) 同时存在缺 name + 非法 driver 两个错误 (2) `./rnix daemon --internal` | 错误信息一次性包含所有问题（非逐个报告） | [ ] |
| 11 | 后台启动也拒绝 | (1) 保持错误配置 (2) `./rnix -i "hello"` | CLI 报错 daemon 启动失败（`EnsureDaemon` 超时或报错），任务无法执行 | [ ] |

### 配置文件不存在时的回退

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 12 | 默认配置回退 | (1) `mv rnix-providers.yaml rnix-providers.yaml.bak` (2) `./rnix daemon --internal` | daemon 正常启动，输出提示使用默认配置，注册 claude 和 cursor 两个 provider | [x] |
| 13 | 回退后功能正常 | (1) 无配置文件时终端 2 执行 `./rnix daemon status` | 显示 `status: running`，providers 段仅含 claude 和 cursor | [x] |
| 14 | 恢复配置 | (1) `mv rnix-providers.yaml.bak rnix-providers.yaml` | 恢复后续测试环境 | [x] |

### NFR31 性能

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 15 | 解析性能 | (1) 配置 10 个 provider (2) `time ./rnix daemon --internal &` 然后立即 `./rnix daemon stop` | daemon 启动（从进程启动到 socket 可连接）耗时远低于 2 秒 | [x] |

---

## Story 23.2: 配置驱动的 Daemon 启动注册流程

### 动态注册

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 1 | 配置驱动注册 | (1) 使用含 4 个 provider 的标准配置 (2) `./rnix daemon --internal` 观察日志 | 日志输出 `[llm] registered 4 providers: claude → /dev/llm/claude, ...`，列出全部 provider 名称和 VFS 路径 | [x] |
| 2 | daemon status 确认 | (1) 终端 2：`./rnix daemon status` | 输出 `status: running`，`providers:` 段列出全部已注册 provider 及其状态 | [x] |
| 3 | VFS 路由正确 | (1) 终端 2：`./rnix -i "hello" --provider=claude` | 正确路由到 claude driver，任务正常完成 | [x] |
| 4 | 默认配置兼容 | (1) 删除配置文件 (2) 重启 daemon (3) `./rnix -i "hello"` | 行为与 Epic 23 之前一致（默认 claude + cursor） | [x] |

### 源码审查（一次性）

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 5 | 硬编码已移除 | 检查 `cmd/rnix/main.go` 的 `runDaemon` 函数 | 无 `NewClaudeCliDriver()` / `NewCursorCliDriver()` 硬编码调用，驱动通过 `llm.RegisterProviders(providersCfg, driverReg, devReg)` 注册 | [ ] |

---

## Story 23.3: Provider 动态解析与白名单移除

### 动态 Provider 解析

> **前提**：daemon 已启动（前台或后台均可），配置含 claude、cursor、ollama、groq 四个 provider。

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 1 | 自定义 provider | `./rnix -i "hello" --provider=ollama` | 路由到 ollama driver（若 Ollama 服务运行则成功；未运行则返回连接错误如 `connection refused`） | [x] |
| 2 | 不存在的 provider | `./rnix -i "hello" --provider=nonexist` | 错误信息包含：`unsupported LLM provider: "nonexist" (available: claude, cursor, groq, ollama)` -- 列出全部已注册 provider（按字母排序） | [x] |
| 3 | 默认 provider | `./rnix -i "hello"`（不指定 `--provider`） | 使用默认 provider（claude），任务正常完成 | [x] |
| 4 | CLI 覆盖 agent | (1) 创建 `lib/agents/test-cursor/agent.yaml` 含 `models.provider: cursor` (2) `./rnix -i "hello" --agent=test-cursor --provider=claude` | CLI `--provider=claude` 优先，使用 claude 而非 cursor | [x] |

### Agent YAML 指定 Provider

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 5 | agent.yaml 指定 provider | (1) 确保 `lib/agents/test-cursor/agent.yaml` 含 `models.provider: cursor` (2) `./rnix -i "hello" --agent=test-cursor` | 使用 cursor provider | [ ] |
| 6 | agent.yaml 无 provider | (1) agent.yaml 不含 provider 字段 (2) `./rnix -i "hello" --agent=<该agent>` | 使用系统默认 provider（claude） | [ ] |

### 源码审查（一次性）

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 7 | 白名单已移除 | `grep -n 'allowedLLMProviders' kernel/kernel.go` | 无匹配结果（硬编码 map 已删除） | [ ] |

---

## Story 23.4: HTTP API Provider 的 API Key 管理

### API Key 注入

> **注意**：API Key 相关日志只能通过前台 daemon 查看（`./rnix daemon --internal`）。

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 1 | 环境变量注入 Key | (1) `export GROQ_API_KEY=gsk-test123` (2) 配置 groq provider 含 `api_key_env: GROQ_API_KEY` (3) `./rnix daemon --internal` | daemon 正常启动，无 API Key 相关 warning（Key 已注入） | [x] |
| 2 | 环境变量缺失 | (1) `unset GROQ_API_KEY` (2) `./rnix daemon --internal` | daemon 正常启动，终端输出 warning：`[llm] warning: provider "groq": API key env var GROQ_API_KEY not set` | [x] |
| 3 | 缺失 Key 时调用失败 | (1) 场景 2 的 daemon 运行中 (2) 终端 2：`./rnix -i "hello" --provider=groq` | 返回认证错误（HTTP 401 / `ErrAuth`），任务失败 | [x] |
| 4 | 本地 provider 无需 Key | (1) ollama provider 不配置 `api_key_env` (2) 观察前台 daemon 启动日志 | 无 ollama 相关的 API Key warning | [x] |

### 安全审计

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 5 | 配置文件不含 Key 明文 | `cat rnix-providers.yaml` | 仅见 `api_key_env: GROQ_API_KEY`（环境变量名），不见实际 Key 值 | [x] |
| 6 | 日志不泄露 Key | (1) `export GROQ_API_KEY=sk-super-secret-key` (2) `./rnix daemon --internal` 观察所有输出 | 输出中不出现 `sk-super-secret-key`，仅出现环境变量名 `GROQ_API_KEY` | [x] |
| 7 | 错误消息不泄露 Key | (1) 使用 groq provider 触发 LLM 调用错误 (2) 检查 CLI 输出的错误消息 | 错误消息不包含 API Key 明文 | [x] |

---

## Story 23.5: Provider Fallback 降级机制

### 测试 Agent 准备

```bash
# 创建跨 provider fallback 测试 agent
mkdir -p lib/agents/fallback-test
cat > lib/agents/fallback-test/agent.yaml << 'EOF'
name: fallback-test
description: "Test agent with cross-provider fallback"
models:
  provider: ollama
  preferred: llama3
  fallback: haiku
  fallback_provider: claude
skills: []
EOF
echo "You are a helpful assistant." > lib/agents/fallback-test/instructions.md
```

### 同 Provider 内模型降级

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 1 | 模型降级 | (1) 创建 agent.yaml 含 `models.provider: claude, models.preferred: nonexist-model, models.fallback: haiku` (2) `./rnix -i "hello" --agent=<该agent>` | preferred 模型不可用时自动切换到 fallback 模型 haiku，任务正常完成 | [x] |

### 跨 Provider 降级

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 2 | 跨 provider fallback | (1) 确保 Ollama 服务**未运行**（`systemctl stop ollama` 或确认端口不可达） (2) `./rnix -i "hello" --agent=fallback-test` | ollama 连接失败后自动切换到 claude provider 的 haiku 模型，任务完成 | [x] |
| 3 | Fallback 延迟 | (1) 场景 2 中观察从 ollama 失败到 claude 响应的时间 | 切换延迟 ≤ 1 秒（NFR33）-- 可通过 strace 事件时间戳验证 | [x] |

### Fallback 失败

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 4 | 所有 provider 不可用 | (1) 创建 agent.yaml: `provider: ollama, fallback_provider: groq`（两个 HTTP provider 均不可达） (2) `./rnix -i "hello" --agent=<该agent>` | 任务失败，错误信息包含两个 provider 名称和各自失败原因（如 `primary /dev/llm/ollama: ...; fallback /dev/llm/groq: ...`） | [x] |

### Strace 可见性

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 5 | Strace 输出 fallback 事件 | (1) 终端 1：`./rnix daemon --internal` (2) 终端 2 先 spawn 一个带 strace 的进程，或使用 `./rnix strace <pid>` (3) 触发 fallback 场景 | strace 事件流中出现 `"action":"fallback"` 事件，包含 `primary_device`、`primary_error`、`fallback_device`、`fallback_model` 字段 | [x] |

### 无 Fallback 配置

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 6 | 未配置 fallback | (1) agent.yaml 不含 `models.fallback` (2) provider 调用失败 | 直接报错，无 fallback 尝试 | [x] |

### Fallback Provider 不存在

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 7 | fallback_provider 未注册 | (1) agent.yaml 设置 `fallback_provider: nonexist`（不在 rnix-providers.yaml 中） (2) primary provider 失败 | fallback 不可用，直接报 primary 错误 | [x] |

---

## Story 23.6: Provider 健康检查与状态报告

### HTTP Provider 健康检查

> **前提**：使用前台 daemon 查看健康检查日志。

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 1 | 可达 provider 标记 healthy | (1) 确保 Ollama 本地运行 (2) `./rnix daemon --internal` (3) 等待 3 秒 (4) 终端 2：`./rnix daemon status` | 终端 1 日志：`[llm] provider "ollama": healthy`；终端 2 输出 ollama 状态为 `healthy` | [x] |
| 2 | 不可达 provider 标记 unhealthy | (1) 停止 Ollama 服务 或 配置一个指向 `http://127.0.0.1:1` 的 provider (2) `./rnix daemon --internal` (3) 等待 3 秒 (4) 终端 2：`./rnix daemon status` | 终端 1 日志：`[llm] provider "xxx": health check failed: ...`；终端 2 该 provider 为 `unhealthy` | [x] |
| 3 | 不阻塞启动 | (1) 配置一个不可达的 HTTP provider (2) `./rnix daemon --internal` (3) 立即在终端 2 执行 `./rnix daemon status` | daemon status 立即返回 `status: running`（不等健康检查完成）；该 provider 可能暂时为 `unchecked`，数秒后变为 `unhealthy` | [x] |

### NFR32 性能

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 4 | 单个健康检查超时 | (1) 配置 provider 指向一个不响应的 TCP 端点（如无响应的远程 IP） (2) `./rnix daemon --internal` (3) 观察日志中 health check failed 出现的时间 | 健康检查在 ≤ 3 秒内超时并标记 `unhealthy` | [x] |

### CLI Provider 跳过检查

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 5 | CLI driver 跳过健康检查 | (1) daemon 运行中 (2) `./rnix daemon status` | claude 和 cursor provider 状态为 `unchecked`（非 healthy/unhealthy） | [x] |

### daemon status 输出格式

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 6 | 完整状态报告 | `./rnix daemon status`（daemon 运行中，配置多个 provider） | 输出格式如下，`providers:` 段每行格式为 `<name 左对齐12字符> <health> (<driver>)` | [x] |

预期输出示例：
```
status:  running
version: 0.1.0
socket:  /run/user/1000/rnix/rnix.sock
procs:   0 active / 0 total
providers:
  claude       unchecked (claude-cli)
  cursor       unchecked (cursor-cli)
  groq         unhealthy (openai-compat)
  ollama       healthy (openai-compat)
```

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 7 | daemon 未运行时 | (1) `./rnix daemon stop` (2) `./rnix daemon status` | 输出 `status: stopped` + `socket: /run/user/<uid>/rnix/rnix.sock`，无 providers 信息 | [x] |

---

## Story 23.7: rnix-compose/init 配置格式升级

### Compose YAML Provider 支持

> **注意**：compose 测试需要编写 `rnix-compose.yaml`。compose 执行会实际 spawn 智能体、消耗 LLM token。

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 1 | agent 级 provider | (1) 创建 `rnix-compose.yaml`（见下方示例 A） (2) daemon 运行中 (3) `./rnix compose up -f rnix-compose.yaml` | Compose 引擎 spawn 的 analyzer agent 使用 ollama provider | [ ] |
| 2 | spec 全局 provider | (1) 创建 `rnix-compose.yaml`（见下方示例 B，顶层 `provider: claude`，agent 不指定） (2) `./rnix compose up -f rnix-compose.yaml` | agent 继承全局 provider（claude） | [ ] |
| 3 | agent 级覆盖全局 | (1) 创建 `rnix-compose.yaml`（见下方示例 C，顶层 `provider: cursor`，agent 指定 `provider: claude`） (2) `./rnix compose up -f rnix-compose.yaml` | agent 使用 claude（优先级：agent > global） | [ ] |

**示例 A** -- agent 级 provider：
```yaml
version: "1"
intent: "test provider"
agents:
  analyzer:
    intent: "say hello"
    provider: ollama
    model: llama3
```

**示例 B** -- 全局 provider：
```yaml
version: "1"
intent: "test global provider"
provider: claude
agents:
  worker:
    intent: "say hello"
    model: haiku
```

**示例 C** -- agent 覆盖全局：
```yaml
version: "1"
intent: "test override"
provider: cursor
agents:
  worker:
    intent: "say hello"
    provider: claude
    model: haiku
```

### 向后兼容

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 4 | 旧格式兼容 | (1) `rnix-compose.yaml` 仅含 `model: haiku`，不含 `provider` 字段 (2) `./rnix compose up -f rnix-compose.yaml` | 系统使用默认 provider（claude），行为与升级前一致 | [ ] |

### Init YAML Provider 支持

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 5 | Init child provider | (1) 编写 `rnix-init.yaml` 含 `provider: claude` 的 supervisor child (2) 停止 daemon (3) `./rnix daemon --internal` 观察 init bootstrap 日志 | supervisor child 使用指定 provider 启动 | [ ] |
| 6 | Init 无 provider 兼容 | (1) `rnix-init.yaml` 中 child 不含 provider 字段 (2) 重启 daemon | 使用系统默认 provider（claude） | [ ] |

---

## 端到端完整流程验证

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 1 | 配置到执行全链路 | (1) 编写含 claude + ollama 的 `rnix-providers.yaml` (2) `./rnix daemon stop` (3) `./rnix -i "hello" --provider=claude`（会自动启动 daemon） (4) `./rnix -i "hello" --provider=ollama` | 两次调用分别路由到正确 provider，任务完成（ollama 需服务运行） | [ ] |
| 2 | 默认配置全链路 | (1) `mv rnix-providers.yaml rnix-providers.yaml.bak` (2) `./rnix daemon stop` (3) `./rnix -i "hello"` (4) `mv rnix-providers.yaml.bak rnix-providers.yaml` | 回退默认配置，使用 claude provider 正常工作 | [ ] |
| 3 | Fallback 全链路 | (1) 确保 Ollama 不可达 (2) daemon 运行中（含 ollama + claude provider） (3) `./rnix -i "hello" --agent=fallback-test` | ollama 失败后自动 fallback 到 claude haiku，任务完成 | [ ] |
| 4 | 健康检查 + 状态报告 | (1) 标准 4 provider 配置（Ollama 停止） (2) `./rnix daemon stop` (3) `./rnix daemon --internal` (4) 等待 5 秒 (5) 终端 2：`./rnix daemon status` | 输出：claude=`unchecked`, cursor=`unchecked`, groq=`unhealthy`(或 healthy), ollama=`unhealthy` | [ ] |
| 5 | 安全性全链路 | (1) `export GROQ_API_KEY=sk-super-secret` (2) `./rnix daemon --internal` (3) 检查终端 1 的所有日志输出 (4) 终端 2：`./rnix daemon status` (5) 终端 2：`./rnix -i "hello" --provider=groq` 触发错误 | 所有输出中不出现 `sk-super-secret` | [ ] |
| 6 | 错误消息友好性 | (1) daemon 运行中 (2) `./rnix -i "hello" --provider=nonexist` | 错误输出清晰列出可用 provider：`(available: claude, cursor, groq, ollama)` | [ ] |

---

## 测试清理

```bash
# 停止 daemon
./rnix daemon stop

# 清理测试 agent（如有创建）
rm -rf lib/agents/fallback-test lib/agents/test-cursor

# 恢复原始配置或删除测试配置
# rm rnix-providers.yaml  # 如需恢复无配置状态
```

---

## 关键注意事项

1. **daemon 日志不可见** -- 后台 daemon 的 stdout/stderr 被丢弃。验证日志必须用前台模式：`./rnix daemon --internal`
2. **`./rnix ps` 不启动 daemon** -- 它直接连 socket，daemon 未运行时显示 "No active processes." 不报错，**不代表 daemon 在运行**。用 `./rnix daemon status` 确认
3. **daemon 重启** -- 配置变更后必须 `./rnix daemon stop` 再重启（前台或后台），否则用旧配置
4. **三种驱动类型** -- `claude-cli`（Claude Code CLI）、`cursor-cli`（Cursor CLI）、`openai-compat`（HTTP API）
5. **CLI Provider 无健康检查** -- `claude-cli` / `cursor-cli` 状态始终为 `unchecked`
6. **NFR31** -- ≤ 10 个 provider 配置解析耗时 ≤ 2 秒
7. **NFR32** -- 单个 HTTP 健康检查耗时 ≤ 3 秒
8. **NFR33** -- Fallback 切换延迟 ≤ 1 秒
9. **API Key 安全** -- 配置仅存环境变量名，日志和错误消息不泄露 Key 值
10. **Fallback 策略** -- 固定：失败 1 次即 fallback，无重试。OODA reasonStep 暂不支持 fallback
11. **Provider 优先级** -- CLI `--provider` > agent.yaml `models.provider` > 系统默认（claude）
12. **Compose Provider 优先级** -- agent 级 > spec 全局 > 系统默认
13. **健康检查异步** -- daemon 刚启动时 status 可能显示 `unchecked`，等几秒后刷新
14. **前台 daemon 使用** -- `./rnix daemon --internal` 是隐藏标志（`--internal` 不在帮助中显示），仅用于调试和验证

## 验证记录

| 字段 | 值 |
|------|-----|
| 验证人 | |
| 验证日期 | |
| 构建版本 | |
| 总用例数 | 61 |
| 通过数 | |
| 失败数 | |
| 备注 | |
