# 项目根 AGENTS.md 运行时注入

Rnix 在 spawn agent 时自动读取**被操作项目**根目录的 `AGENTS.md`，并将其正文作为独立 section 注入该进程的 system prompt——让 agent 像 Codex / Cursor / Claude Code 一样开箱遵循项目级约定，无需每次手动告知。

## 背景：AGENTS.md 是什么

`AGENTS.md` 是 AI coding 的事实标准：2025-08 发布，2025-12 归入 Linux Foundation 的 Agentic AI Foundation（AAIF，与 Anthropic 捐赠的 MCP 并列由基金会托管），至 2026 年中已有 30+ 工具原生读取、60,000+ 开源仓库采用。它是一份放在仓库根目录的纯 Markdown，告诉 AI agent「如何构建 / 测试 / 贡献本项目」，机制与 Claude Code 的 `CLAUDE.md` 同构（根目录文档、任务启动时读入 agent 上下文）。

## 用法

在目标项目根目录放一份 `AGENTS.md`：

```markdown
# 项目约定

- 提交前运行 `make all`
- 用简体中文回复
- 不要修改 `vendor/` 目录
```

之后在该项目中 spawn 任意 Rnix agent，其 system prompt 即自动包含该文件正文。无需任何 per-spawn 配置。

验证：dump 进程的 `process-meta.json`，其 `system_prompt` 字段会包含 `# Project Instructions (AGENTS.md)` 段及文件正文。

```bash
cat .rnix/data/steps/<uuid>/process-meta.json | jq -r .system_prompt
```

## 行为细节

| 行为 | 说明 |
|------|------|
| **nearest-wins 查找** | 从进程工作目录向上逐级查找最近的 `AGENTS.md`，到项目根边界停止、不越界；仅注入最近（最深）的一份，祖先版本不参与拼接。 |
| **排他只认 `AGENTS.md`** | 只匹配文件名 `AGENTS.md`，**不**回退读 `CLAUDE.md` / `RNIX.md` 等。本仓库根的 `CLAUDE.md` 是写给 Claude Code 的，读它会造成内容错位与文件双主人冲突。 |
| **冻结快照** | spawn 时读盘一次，进程生命周期内不变（即使 `AGENTS.md` 被外部修改）。这保护 LLM prompt cache 命中率，与 `agent_instructions` section 语义一致。 |
| **降级** | 无 `AGENTS.md` / 文件读取失败 / 进程无项目上下文 → 该段为空，spawn 正常完成，不报错。 |
| **软上限截断** | 内容超过 64 KiB 软上限时按 UTF-8 边界安全截断，追加 `[truncated: AGENTS.md exceeded N bytes]` 尾标记并记录警告，进程不 crash。 |

> ⚠️ **关于嵌套（nearest-wins）**：当前架构中进程工作目录恒等于项目根（`ProjectConfig` 无独立 cwd 字段），因此真实 spawn 只命中项目根那一份 `AGENTS.md`。nearest-wins 的子目录嵌套语义已实现并由单元测试验证，未来支持子目录工作时即生效。

## 禁用（per-agent）

项目文档注入默认开启。在 agent 的 `agent.yaml` manifest 中设 `project_doc: false` 可对该 agent 关闭：

```yaml
name: my-agent
models:
  provider: claude
project_doc: false   # 禁用项目根 AGENTS.md 注入（缺省 = 开启）
```

关闭时该段为空且零额外开销。无 agent 的直接 spawn 默认开启。

## 与其他机制的关系

- **正交于 `memory` / `user_profile`**：AGENTS.md 是用户手写、放在被操作项目根的指令文档；`memory` / `user_profile` 是 Rnix 自有的策划式记忆库。二者并存、互不影响。
- **正交于 agent `instructions.md`**：`instructions.md` 是 per-agent 的系统指令（agent 级）；AGENTS.md 是 per-project 的项目约定（项目级）。注入顺序上 `project_doc` 紧邻 `agent_instructions`，作「项目级」与「agent 级」指令的相邻层。
- **不替代本仓库的 `CLAUDE.md`**：Rnix 仓库根的 `CLAUDE.md` 是供 Claude Code 工具使用的开发指南，与本特性无关，Rnix 的注入逻辑绝不读取它。

## 参考

- SPEC: `_bmad-output/specs/spec-agents-md-injection/SPEC.md`（CAP-1..3 + Constraints + Non-goals）
- ADR: Architecture Decision 47（`_bmad-output/planning-artifacts/architecture/core-architectural-decisions.md`）
- 行业调查: `_bmad-output/implementation-artifacts/investigations/agents-md-industry-standard-investigation.md`
- 实现: `kernel/sections.go`（section 注册）、`internal/config/agentsmd.go`（nearest-wins helper + 软上限）
