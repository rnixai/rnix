# Epic 23 手工验证指南：多 LLM Provider 动态配置

## 概述

本文档提供 Epic 23 所有 Story 的手工验证步骤，用于在自动化测试之外对功能进行端到端的人工确认。Epic 23 将 rnix 从单一 Claude CLI 演进为灵活的多模型架构，支持通过 `rnix-providers.yaml` 声明式定义多种 LLM provider。

## 前置准备

Daemon 由 rnix 自动按需启动（`EnsureDaemon`），无需手动管理。

```bash
# 1. 构建最新版本
make build

# 2. 确认 daemon 可用
./rnix ps

# 3. 准备测试用 rnix-providers.yaml（放到项目根目录）
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

# 4. (可选) 如果测试 Groq，设置 API Key
export GROQ_API_KEY="your-groq-api-key-here"

# 5. (可选) 如果测试 Ollama，确认 Ollama 本地服务运行
curl -s http://localhost:11434/v1/models | head -5

# 6. 停止旧 daemon 以加载新配置
./rnix daemon stop
```

> **提示**：HTTP API 类 provider（openai-compat）需要对应服务可达才能真正调用 LLM。CLI 类 provider（claude-cli、cursor-cli）需要对应 CLI 工具已安装。健康检查仅验证网络可达性，不验证功能正确性。

---

## Story 23.1: rnix-providers.yaml 配置文件定义与解析

### 配置文件正常解析

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 1 | 完整配置解析 | 项目根目录放置含 claude-cli、cursor-cli、openai-compat 三种驱动的 `rnix-providers.yaml`，启动 daemon | daemon 正常启动，日志中出现 `registered X providers` |  [ ] |
| 2 | 最小配置 | 配置文件仅包含一个 provider（如仅 claude），启动 daemon | daemon 正常启动 | [ ] |
| 3 | 多 provider 配置 | 配置文件包含 5+ 个 provider，启动 daemon | 所有 provider 正确注册，`rnix daemon status` 可见 | [ ] |

### 配置错误处理

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 4 | YAML 语法错误 | 配置文件包含语法错误（如缺少冒号），启动 daemon | daemon 拒绝启动，错误信息包含行号和具体格式问题 | [ ] |
| 5 | 缺少 name 字段 | provider 条目缺少 `name`，启动 daemon | daemon 拒绝启动，错误信息指出 `name` 缺失 | [ ] |
| 6 | 无效 driver 类型 | `driver: invalid-driver`，启动 daemon | daemon 拒绝启动，错误信息指出 driver 类型非法 | [ ] |
| 7 | openai-compat 缺少 base_url | openai-compat 类型 provider 无 `base_url`，启动 daemon | daemon 拒绝启动，错误信息指出 `base_url` 必填 | [ ] |
| 8 | 重复 provider 名称 | 两个 provider 同名（如两个 `claude`），启动 daemon | daemon 拒绝启动，错误信息指出名称重复 | [ ] |
| 9 | 非法名称字符 | provider 名称含特殊字符（如 `my provider!`），启动 daemon | daemon 拒绝启动，错误信息指出名称包含非法字符 | [ ] |
| 10 | 多个错误同时存在 | 配置同时存在多个验证错误（缺 name + 非法 driver），启动 daemon | 错误信息一次性包含所有问题（而非只报告第一个） | [ ] |

### 配置文件不存在时的回退

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 11 | 默认配置回退 | 删除 `rnix-providers.yaml`，启动 daemon | daemon 正常启动，日志提示使用默认配置，仅注册 claude 和 cursor | [ ] |
| 12 | 回退后功能正常 | 无配置文件时 `./rnix -i "hello"` | 使用默认 claude provider 正常工作 | [ ] |

### NFR31 性能

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 13 | 解析性能 | 配置 10 个 provider，观察 daemon 启动日志 | 配置解析耗时 ≤ 2 秒 | [ ] |

---

## Story 23.2: 配置驱动的 Daemon 启动注册流程

### 动态注册

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 1 | 配置驱动注册 | 使用含 3 种驱动类型的配置启动 daemon，观察日志 | 日志显示 `[llm] registered 3 providers: claude → /dev/llm/claude, cursor → /dev/llm/cursor, ollama → /dev/llm/ollama`（或类似格式） | [ ] |
| 2 | 硬编码已移除 | 检查 daemon 启动流程 | 无 `NewClaudeCliDriver()` / `NewCursorCliDriver()` 硬编码调用，所有注册通过 `RegisterProviders` 完成 | [ ] |
| 3 | VFS 路由正确 | `./rnix -i "hello" --provider=claude` | 正确路由到 claude driver，任务正常完成 | [ ] |
| 4 | 默认配置兼容 | 删除配置文件后启动 daemon，`./rnix -i "hello"` | 行为与 Epic 23 之前完全一致（默认 claude + cursor） | [ ] |

### DriverRegistry 统一管理

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 5 | 注册表可查询 | `rnix daemon status` | 输出中列出所有已注册 provider 名称 | [ ] |

---

## Story 23.3: Provider 动态解析与白名单移除

### 动态 Provider 解析

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 1 | 自定义 provider | 配置含 ollama provider 的 yaml，`./rnix -i "hello" --provider=ollama` | 正确路由到 ollama driver（若 Ollama 服务运行则成功，否则返回连接错误） | [ ] |
| 2 | 不存在的 provider | `./rnix -i "hello" --provider=nonexist` | 返回错误信息：`unsupported LLM provider: "nonexist" (available: claude, cursor, ollama)`，列出所有可用 provider | [ ] |
| 3 | 默认 provider | 不指定 `--provider`，agent 也未配置 provider | 使用默认 provider（claude） | [ ] |
| 4 | CLI --provider 覆盖 | agent.yaml 配置 `models.provider: cursor`，但 CLI 指定 `--provider=claude` | CLI 参数优先，使用 claude | [ ] |

### Agent YAML 指定 Provider

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 5 | agent.yaml provider | 创建 agent.yaml 含 `models.provider: cursor`，使用该 agent spawn | 使用 cursor provider | [ ] |
| 6 | agent.yaml 无 provider | agent.yaml 不包含 provider 字段 | 使用系统默认 provider（claude） | [ ] |

### 白名单移除验证

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 7 | 无硬编码白名单 | 检查 `kernel/kernel.go` 源码 | `allowedLLMProviders` map 已删除，provider 验证通过 `DriverRegistry` 查询 | [ ] |

---

## Story 23.4: HTTP API Provider 的 API Key 管理

### API Key 注入

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 1 | 环境变量注入 Key | 设置 `GROQ_API_KEY=sk-xxx`，配置 groq provider（`api_key_env: GROQ_API_KEY`），启动 daemon | groq driver 创建成功，API 调用携带 `Authorization: Bearer sk-xxx` | [ ] |
| 2 | 环境变量缺失 | 不设置 `GROQ_API_KEY`，启动 daemon | daemon 正常启动，日志输出 warning：`provider "groq": API key env var GROQ_API_KEY not set` | [ ] |
| 3 | 缺失 Key 时首次调用失败 | 不设置 API Key 的 groq provider 执行 LLM 调用 | 返回 `ErrAuth` 错误（HTTP 401） | [ ] |
| 4 | 本地 provider 无需 Key | ollama provider 不配置 `api_key_env`，启动 daemon | 正常创建，无 warning，不附带 Authorization header | [ ] |

### 安全审计

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 5 | 配置文件不含 Key 明文 | 检查 `rnix-providers.yaml` | 仅存储环境变量名（如 `GROQ_API_KEY`），不存储 Key 值 | [ ] |
| 6 | 日志不泄露 Key | 设置 API Key 后启动 daemon，检查日志输出 | 日志中仅出现环境变量名，不出现 Key 值 | [ ] |
| 7 | 错误消息不泄露 Key | 触发 LLM 调用错误，检查错误消息 | 错误消息不包含 API Key 明文 | [ ] |

---

## Story 23.5: Provider Fallback 降级机制

### 同 Provider 内模型降级

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 1 | 模型降级 | agent.yaml 配置 `models.preferred: sonnet, models.fallback: haiku`（同 claude provider），preferred 模型不可用（如模型名拼错） | 自动切换到 fallback 模型 haiku，任务正常完成 | [ ] |

### 跨 Provider 降级

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 2 | 跨 provider fallback | agent.yaml 配置 `models.provider: ollama, models.fallback: haiku, models.fallback_provider: claude`，ollama 不可达 | 自动切换到 claude provider 的 haiku 模型 | [ ] |
| 3 | Fallback 延迟 | 观察场景 2 的切换耗时 | 从检测失败到发起 fallback 调用 ≤ 1 秒（NFR33） | [ ] |

### Fallback 失败

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 4 | 所有 provider 不可用 | primary 和 fallback provider 均不可达 | 进程转为 Zombie 状态，错误信息包含两个 provider 名称和各自失败原因 | [ ] |

### Strace 可见性

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 5 | Strace 输出 fallback 事件 | 启用 strace 后触发 fallback 场景 | strace 事件流中出现 fallback 事件，包含 `primary_device`、`primary_error`、`fallback_device`、`fallback_model` 信息 | [ ] |

### 无 Fallback 配置

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 6 | 未配置 fallback | agent.yaml 不含 `models.fallback`，primary provider 调用失败 | 直接报错，不尝试 fallback | [ ] |

### Fallback Provider 不存在

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 7 | fallback_provider 未注册 | agent.yaml 配置 `fallback_provider: nonexist`（未在 rnix-providers.yaml 中定义） | fallback 不可用，primary 失败时直接报错 | [ ] |

---

## Story 23.6: Provider 健康检查与状态报告

### HTTP Provider 健康检查

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 1 | 可达 provider 标记 healthy | 配置 Ollama provider（本地运行），启动 daemon，等待 3 秒 | `rnix daemon status` 显示 ollama 状态为 `healthy` | [ ] |
| 2 | 不可达 provider 标记 unhealthy | 配置一个指向无效地址的 openai-compat provider（如 `base_url: http://127.0.0.1:1`），启动 daemon | daemon 正常启动，日志输出 warning：`provider "xxx": health check failed: connection refused`，`rnix daemon status` 显示该 provider 为 `unhealthy` | [ ] |
| 3 | 不阻塞启动 | 配置一个健康检查很慢的 provider | daemon 启动不受阻塞，健康检查在后台异步执行 | [ ] |

### NFR32 性能

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 4 | 单个健康检查超时 | 配置指向一个不响应的端点的 provider | 健康检查在 ≤ 3 秒内超时，标记为 `unhealthy` | [ ] |

### CLI Provider 跳过检查

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 5 | claude-cli 跳过健康检查 | 启动 daemon，`rnix daemon status` | claude provider 状态为 `unchecked`（非 healthy/unhealthy） | [ ] |
| 6 | cursor-cli 跳过健康检查 | 启动 daemon，`rnix daemon status` | cursor provider 状态为 `unchecked` | [ ] |

### daemon status 输出

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 7 | 完整状态报告 | 配置多个 provider 后 `rnix daemon status` | 输出包含 `providers:` 段，每行格式为 `<name> <health> (<driver>)`，例如：`claude unchecked (claude-cli)`、`ollama healthy (openai-compat)` | [ ] |
| 8 | daemon 未运行时 | 停止 daemon 后 `rnix daemon status` | 显示 `status: stopped`，无 provider 信息 | [ ] |

---

## Story 23.7: rnix-compose/init 配置格式升级

### Compose YAML Provider 支持

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 1 | agent 级 provider | 在 `rnix-compose.yaml` 中 agent 配置 `provider: ollama` + `model: llama3`，执行 `rnix compose up` | Compose 引擎 spawn 时使用 ollama provider 和 llama3 模型 | [ ] |
| 2 | spec 全局 provider | `rnix-compose.yaml` 顶层配置 `provider: groq`，agent 未指定 provider | agent 继承全局 provider（groq） | [ ] |
| 3 | agent 级覆盖全局 | 顶层 `provider: groq`，agent 配置 `provider: ollama` | agent 使用 ollama（优先级：agent > global） | [ ] |

### 向后兼容

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 4 | 旧格式兼容 | `rnix-compose.yaml` 仅指定 `model: haiku`（无 provider 字段），执行 `rnix compose up` | 系统使用默认 provider（claude），行为与升级前一致 | [ ] |

### Init YAML Provider 支持

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 5 | Init child provider | `rnix-init.yaml` 中 supervisor child 配置 `provider: groq` + `model: llama-3.3-70b-versatile` | init 引导时 supervisor 使用指定 provider 和 model spawn 子进程 | [ ] |
| 6 | Init 无 provider 兼容 | `rnix-init.yaml` 中 child 不含 provider 字段 | 使用系统默认 provider（claude） | [ ] |

---

## 端到端完整流程验证

| # | 验证场景 | 操作步骤 | 预期结果 | 通过 |
|---|---------|---------|---------|------|
| 1 | 配置到执行全链路 | (1) 编写 `rnix-providers.yaml` 含 claude + ollama (2) 停止旧 daemon (3) `./rnix -i "hello" --provider=claude` (4) `./rnix -i "hello" --provider=ollama` | 两次调用分别路由到正确 provider，任务完成 | [ ] |
| 2 | 默认配置全链路 | (1) 删除 `rnix-providers.yaml` (2) 停止旧 daemon (3) `./rnix -i "hello"` | 回退默认配置，使用 claude provider 正常工作 | [ ] |
| 3 | Fallback 全链路 | (1) 配置 ollama + claude provider (2) agent.yaml 含 `provider: ollama, fallback: haiku, fallback_provider: claude` (3) 停止 Ollama 服务 (4) 使用该 agent 执行任务 | ollama 失败后自动 fallback 到 claude haiku，任务完成 | [ ] |
| 4 | 健康检查 + 状态报告 | (1) 配置 claude(cli) + ollama(http 可达) + groq(http 不可达/无 Key) (2) 启动 daemon (3) 等待 3 秒 (4) `rnix daemon status` | 输出显示：claude=unchecked, ollama=healthy, groq=unhealthy | [ ] |
| 5 | Compose + 多 Provider | (1) 编写 `rnix-compose.yaml` 含多个 agent 使用不同 provider (2) `rnix compose up` | 各 agent 使用各自指定的 provider 正确执行 | [ ] |
| 6 | Init + Provider 配置 | (1) 编写 `rnix-init.yaml` 含带 provider 的 supervisor child (2) `rnix init`（或 daemon 自动 bootstrap） | supervisor child 使用指定 provider 启动 | [ ] |
| 7 | 安全性全链路 | (1) 设置 `GROQ_API_KEY=sk-secret` (2) 配置 groq provider (3) 启动 daemon (4) 检查所有日志、错误消息、`rnix daemon status` 输出 | API Key 明文不出现在任何输出中 | [ ] |
| 8 | 错误消息友好性 | (1) `./rnix -i "hello" --provider=nonexist` (2) 观察错误输出 | 错误信息清晰列出可用 provider 列表：`(available: claude, cursor, ollama, groq)` | [ ] |

---

## 关键注意事项

1. **配置文件位置** -- `rnix-providers.yaml` 查找顺序：当前工作目录 → `$XDG_CONFIG_HOME/rnix/`（默认 `$HOME/.config/rnix/`）。修改配置后需重启 daemon 生效
2. **daemon 重启** -- 配置变更后需 `rnix daemon stop` 再重新启动（CLI 命令会自动 `EnsureDaemon`），否则仍使用旧配置
3. **三种驱动类型** -- `claude-cli`（调用 Claude Code CLI）、`cursor-cli`（调用 Cursor CLI）、`openai-compat`（HTTP API，兼容 OpenAI 协议）
4. **CLI Provider 无健康检查** -- `claude-cli` 和 `cursor-cli` 不支持健康检查，状态始终为 `unchecked`，可用性在首次调用时验证
5. **NFR31** -- ≤ 10 个 provider 时配置解析耗时 ≤ 2 秒
6. **NFR32** -- 单个 HTTP provider 健康检查耗时 ≤ 3 秒
7. **NFR33** -- Fallback 切换延迟（从检测失败到发起 fallback 调用）≤ 1 秒
8. **API Key 安全** -- 配置文件仅存储环境变量名（`api_key_env: GROQ_API_KEY`），不存储 Key 值；日志和错误消息中不泄露 Key
9. **Fallback 策略** -- 固定策略：primary 失败 1 次即 fallback，无重试、无退避。OODA reasonStep 暂不支持 fallback（仅线性 reasonStep）
10. **向后兼容** -- 配置文件不存在时回退默认配置（claude + cursor）；compose/init YAML 不含 `provider` 字段时使用系统默认 provider
11. **Provider 优先级** -- CLI `--provider` > agent.yaml `models.provider` > 系统默认（claude）
12. **Compose Provider 优先级** -- agent 级 `provider` > spec 全局 `provider` > 系统默认（claude）
13. **Fallback Provider** -- `agent.yaml` 中 `fallback_provider` 空时使用同 provider 内降级，非空时跨 provider 降级
14. **健康检查异步** -- daemon 启动后立即查询状态可能看到 `unchecked`（检查尚未完成），等待数秒后刷新
15. **openai-compat base_url** -- 已 TrimRight "/"，拼接 `/models` 为健康检查端点；不同服务的 URL 格式可能不同（如 Ollama 为 `http://localhost:11434/v1`）

## 配置文件参考

### rnix-providers.yaml 示例

```yaml
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

### agent.yaml 跨 Provider Fallback 示例

```yaml
name: robust-agent
description: "Agent with cross-provider fallback"
models:
  provider: ollama
  preferred: llama3
  fallback: haiku
  fallback_provider: claude
skills: []
```

### rnix-compose.yaml 多 Provider 示例

```yaml
version: "1"
intent: "multi-provider workflow"
provider: claude  # 全局默认 provider
agents:
  analyzer:
    intent: "analyze code"
    agent: code-analyst
    provider: ollama
    model: llama3
  writer:
    intent: "write report"
    agent: tech-writer
    # 继承全局 provider: claude
    model: haiku
```

### rnix-init.yaml Provider 示例

```yaml
version: "1"
supervisors:
  - name: main
    restart: one_for_one
    children:
      - name: worker
        intent: "background task"
        agent: worker-agent
        provider: groq
        model: llama-3.3-70b-versatile
```

## 验证记录

| 字段 | 值 |
|------|-----|
| 验证人 | |
| 验证日期 | |
| 构建版本 | |
| 总用例数 | 62 |
| 通过数 | |
| 失败数 | |
| 备注 | |
