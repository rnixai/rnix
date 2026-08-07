# Reasoning Effort 配置（Epic 55）

Rnix 支持通过 `providers.yaml` 为每个 provider 配置 **reasoning_effort**，统一控制推理强度。这是行业（OpenAI/Anthropic/Gemini 最新模型）已收敛到的离散 effort/level 语义，用于取代正在 legacy 化的 `thinking_budget`。

## 解析优先级链（四级兜底）

effort 的最终生效值在 spawn 时按以下顺序解析，与 `model` 同构（`kernel/spawn.go`）：

| 级别 | 来源 | 入口 | 说明 |
|------|------|------|------|
| 1（最高） | per-spawn override | CLI `rnix spawn --effort`、compose.yaml 节点、intent | 归一到 `opts.ReasoningEffort`，单次生效 |
| 2 | **agent 清单默认** | `lib/agents/{name}/agent.yaml` 的 `models.reasoning_effort` | 该 agent 的「思考强度」身份默认值 |
| 3 | driver 实例默认 | `providers.yaml` 的 `reasoning_effort` | 该 provider 实例的兜底值 |
| 4（兜底） | 透传 `""` | — | 不下发该字段，落到 API/CLI 原生默认 |

高级别非空即胜出、跳过低级别；全空才落到下一级。

> ⚠️ **「事实地板」陷阱**：`providers.yaml` 的 `reasoning_effort` 一旦设值，就成为该 provider 的**事实地板**——当 per-spawn 与 agent.yaml 都没指定时，生效值是这个 provider 值，而**不是** API/CLI 原生默认（第 4 级只在前三级全空时才到达）。若想让某个 agent 显式回到「原生默认」，目前没有「清空」语义（空字符串=未设置=继续向下兜底），只能不在任何一级配置该 provider 的 effort。

### agent.yaml 配置示例

```yaml
# lib/agents/deep-reasoner/agent.yaml
name: deep-reasoner
models:
  provider: openai-high
  preferred: gpt-5.1
  reasoning_effort: high        # 此 agent 默认高强度推理；小写/大写规则同 provider（见下方陷阱）
```

同样**纯透传**：rnix 不校验/不映射/不转大小写。为使用 gemini provider 的 agent 配 `reasoning_effort` 时必须写**大写**（如 `HIGH`），与 providers.yaml 的 Gemini 规则一致。

## 透传语义（重要）

`reasoning_effort` 是**纯透传**字段：

- rnix **不校验、不映射、不维护规范等级集**——你写什么就原样下发给底层 API/CLI。
- 写错的值由底层自行报错或降级（各家有自己的降级策略，如 Claude 回退到 ≤ 请求值的最高支持档）。
- 三个 SDK（OpenAI/Anthropic/Gemini）的 effort 字段均为开放 `string` 类型且无运行时校验，因此 **`xhigh`（OpenAI gpt-5.1-codex-max+）及厂商未来新增等级无需升级 rnix 即可透传**。
- `""`（空，默认）= 不设置，行为与未配置时完全一致（零回归）。

## 各 driver 支持形态对照

| Driver | 机制 | 已知取值 | 备注 |
|--------|------|----------|------|
| `openai` | API 参数 `reasoning_effort` | none/minimal/low/medium/high/**xhigh**（小写） | — |
| `anthropic` | API 参数 `OutputConfig.Effort` | low/medium/high/max（小写） | 迁移目标；`thinking_budget` 保留为降级 |
| `gemini` | API 参数 `ThinkingConfig.ThinkingLevel` | **MINIMAL/LOW/MEDIUM/HIGH（大写！）** | 与 `thinking_budget` **互斥** |
| `claude-cli` | CLI flag `--effort <value>` | 透传 | 旧版 CLI 不识别会自行报错 |
| `codex-cli` | CLI flag `-c model_reasoning_effort=<value>` | 透传 | — |
| `cursor-cli` | **不支持** | — | thinking level 绑在 model 名后缀，配置非空时仅记 warning |
| `qwen-cli` | **不支持** | — | Qwen3-Coder 无 effort 概念，配置非空时仅记 warning |

### ⚠️ 大小写不统一陷阱

透传语义下 rnix **不转换大小写**：

- **Gemini** 的 `ThinkingLevel` 枚举是**大写**：`HIGH`、`LOW`、`MINIMAL`、`MEDIUM`。
- **OpenAI / Anthropic** 是**小写**：`high`、`low`、`medium`、`max`。

为 gemini provider 配 `reasoning_effort` 时必须写大写，否则底层会报错。这是「透传不统一」的直接后果。

## providers.yaml 配置示例

```yaml
version: "1"
default_provider: openai-high
providers:
  # OpenAI 官方 SDK —— reasoning_effort 写入 ChatCompletionNewParams.ReasoningEffort
  - name: openai-high
    driver: openai
    default_model: gpt-5.1
    api_key_env: OPENAI_API_KEY
    reasoning_effort: high          # none/minimal/low/medium/high/xhigh（小写）

  # OpenAI 兼容端点（DeepSeek/Groq/Ollama 等，统一 openai driver）—— body 字段 reasoning_effort
  # 注意：reasoning_effort 与 thinking_budget 正交，可同时配置
  - name: deepseek
    driver: openai
    base_url: https://api.deepseek.com/v1
    default_model: deepseek-chat
    api_key_env: DEEPSEEK_API_KEY
    reasoning_effort: high
    thinking_budget: 8192           # DeepSeek 多轮工具调用仍需 budget，保留

  # Anthropic 原生 —— OutputConfig.Effort（迁移目标，stable）
  - name: claude-api
    driver: anthropic
    default_model: claude-opus-4-6
    api_key_env: ANTHROPIC_API_KEY
    reasoning_effort: high          # low/medium/high/max（小写）

  # DeepSeek V4 经 Anthropic-兼容端点 —— 用 thinking_budget（缺失会 HTTP 400）
  # 此处不配 reasoning_effort，沿用 budget 降级路径
  - name: deepseek-anthropic
    driver: anthropic
    base_url: https://api.deepseek.com/anthropic
    default_model: deepseek-v4
    api_key_env: DEEPSEEK_API_KEY
    thinking_budget: 8192

  # Gemini —— ThinkingConfig.ThinkingLevel（大写！与 thinking_budget 互斥）
  - name: gemini-high
    driver: gemini
    default_model: gemini-3-pro
    api_key_env: GEMINI_API_KEY
    reasoning_effort: HIGH          # MINIMAL/LOW/MEDIUM/HIGH（大写）

  # Claude Code CLI —— 追加 --effort <value>
  - name: claude-cli
    driver: claude-cli
    default_model: claude-opus-4-6
    reasoning_effort: high

  # Codex CLI —— 追加 -c model_reasoning_effort=<value>
  - name: codex
    driver: codex-cli
    default_model: gpt-5.1-codex
    reasoning_effort: high

  # Cursor CLI —— reasoning_effort 无效（仅 warning）；effort 经 model 名后缀表达
  - name: cursor
    driver: cursor-cli
    default_model: sonnet-4.5-thinking-high   # ← thinking level 在这里，不用 reasoning_effort
```

## 防回归红线

- **anthropic budget 不可删**：`thinking_budget` 是 DeepSeek V4 Anthropic-兼容端点多轮工具调用的必需项，缺失会 HTTP 400。迁移 = 新增 effort 优先路径 + budget 保留为降级；effort 非空时优先 effort、跳过 budget。
- **gemini level/budget 互斥**：Gemini 3 同时传 `thinking_level` 与 `thinking_budget` 会 API 报错。effort 非空时 rnix 不再传 budget。
- **空值零回归**：所有 driver 在 `reasoning_effort` 为空时行为与配置前完全一致。

## 参考

- 调查与官方核查对照表：`_bmad-output/implementation-artifacts/investigations/llm-driver-effort-level-investigation.md`
- OpenAI Reasoning: https://developers.openai.com/api/docs/guides/reasoning
- Anthropic Effort: https://platform.claude.com/docs/en/build-with-claude/effort
- Gemini Thinking: https://ai.google.dev/gemini-api/docs/thinking
- Codex config: https://developers.openai.com/codex/config-advanced
