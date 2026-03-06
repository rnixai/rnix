# Story 5.2: 快速上手指南

Status: done

<!-- Note: Validation is optional. Run validate-create-story for quality check before dev-story. -->

## Story

As a 新用户,
I want 按照快速上手指南从安装到跑通第一个 demo,
So that 我在 15 分钟内体验到 Crux 的核心价值。

## Acceptance Criteria

1. **覆盖完整上手流程** — Given 快速上手指南已编写，When 按步骤操作，Then 覆盖完整流程：安装 Go → 安装 Crux（`go install`）→ 验证（`crux version`）→ 首次执行（`crux "分析 ./README.md"`）→ 查看结果 → 首次 astrace（`crux astrace 1`）

2. **15 分钟目标** — Given 用户已有 Go 1.26 环境和 Claude Code CLI，When 按指南操作，Then 目标完成时间 ≤ 15 分钟（FR39）

3. **每步含预期输出** — Given 指南中每个操作步骤，When 用户执行命令，Then 每一步包含预期输出示例，用户可对照验证

4. **文档输出为中文 Markdown** — Given 文档已生成，When 查看文件，Then 使用简体中文书写，格式为 Markdown，存放在 `docs/quick-start.md`

## Tasks / Subtasks

- [x] Task 1: 创建 `docs/quick-start.md` 文件框架 (AC: #4)
  - [x] 1.1 在 `docs/` 目录下创建 `quick-start.md`
  - [x] 1.2 添加文档标题、简介段落（一句话说明文档目的和预计耗时）

- [x] Task 2: 编写前置条件章节 (AC: #1, #3)
  - [x] 2.1 Go 1.26 环境检查：`go version` 命令及预期输出
  - [x] 2.2 Claude Code CLI 检查：`claude --version` 命令及预期输出，附安装链接
  - [x] 2.3 说明 Claude Code CLI 需要有效的 API 密钥配置

- [x] Task 3: 编写安装 Crux 章节 (AC: #1, #3)
  - [x] 3.1 使用 `go install github.com/usecrux/crux/cmd/crux@latest` 安装
  - [x] 3.2 验证安装：`crux version` 命令及预期输出
  - [x] 3.3 故障排查：Claude Code CLI 未找到时的错误提示和解决方法

- [x] Task 4: 编写首次运行章节 (AC: #1, #2, #3)
  - [x] 4.1 最简用法：`crux "你好，请介绍你自己"` — 展示基础 Spawn → 推理 → 结果流程
  - [x] 4.2 预期输出示例：`[kernel] spawning PID 1...` → `[agent] step N/M` → `[result] ...` → `[kernel] PID 1 exited(0) | tokens: N | elapsed: Ns`
  - [x] 4.3 解读输出：每行含义简要说明

- [x] Task 5: 编写使用 Agent 章节 (AC: #1, #2, #3)
  - [x] 5.1 使用参考 Agent：`crux "分析 ./cmd/crux/main.go" --agent=code-analyst`
  - [x] 5.2 预期输出示例：包含文件分析结果
  - [x] 5.3 简要解释 `--agent` 参数的作用（引用概念文档链接）

- [x] Task 6: 编写 astrace 调试体验章节 (AC: #1, #2, #3)
  - [x] 6.1 说明 astrace 的价值：实时查看智能体的每一步操作
  - [x] 6.2 演示命令（需要在另一个终端窗口运行长任务时使用）
  - [x] 6.3 预期 astrace 输出示例，展示 syscall 链路
  - [x] 6.4 解读关键 syscall 行的含义

- [x] Task 7: 编写进程管理体验章节 (AC: #1, #3)
  - [x] 7.1 `crux ps` — 查看进程列表
  - [x] 7.2 `crux ps --json` — JSON 格式输出
  - [x] 7.3 `crux kill <pid>` — 终止进程

- [x] Task 8: 编写下一步指引章节 (AC: #1)
  - [x] 8.1 链接到概念文档（`docs/concepts.md`）深入理解
  - [x] 8.2 链接到参考手册（`docs/reference.md`，Story 5.3 将创建）
  - [x] 8.3 提及 Agent 和 Skill 扩展能力

- [x] Task 9: 校验与完善 (AC: #3, #4)
  - [x] 9.1 检查所有 CLI 命令与实际实现一致
  - [x] 9.2 确保所有预期输出示例格式正确
  - [x] 9.3 确保文档可独立阅读（不依赖概念文档前置知识）
  - [x] 9.4 最终审读：步骤清晰、无遗漏、15 分钟可完成

## Dev Notes

### 这是一个文档类 Story

**重要：** 本 Story 不涉及 Go 代码编写。输出是 `docs/quick-start.md` 单一 Markdown 文件。开发代理需要理解现有 CLI 实现和文档体系，创建面向新用户的实操指南。

### 文档写作原则

1. **面向零基础用户** — 读者不了解 Crux，按步骤操作即可上手。假设有基本的终端使用经验和 Go 开发环境。
2. **实践优先** — 不是理论讲解（概念文档在 `docs/concepts.md`），而是"跟着做"的操作手册。每一步：命令 → 预期输出 → 简要解释。
3. **准确反映实现** — 所有命令、输出格式、VFS 路径必须与当前代码实现一致。不要写尚未实现的功能。
4. **简体中文** — 全文使用简体中文。技术术语首次出现时附英文。
5. **15 分钟约束** — 内容精简，不要过度展开。用户应在 15 分钟内完成全部步骤。
6. **渐进式体验** — 从最简单的用法开始，逐步引入更多功能（Agent → astrace → ps）。

### 文档输出位置

文件路径：`docs/quick-start.md`

`docs/` 目录当前已有 `concepts.md`（Story 5.1 产出），这是第二个文件。

### CLI 命令实际实现参考

以下是实际已实现的 CLI 命令及其行为，文档中的所有示例必须与此一致：

| 命令 | 实际实现位置 | 说明 |
|------|------------|------|
| `crux "意图"` | `cmd/crux/main.go` rootCmd | Spawn Agent 执行意图，意图为 positional arg |
| `crux "意图" --agent=code-analyst` | `cmd/crux/main.go` rootCmd `--agent` flag | 使用指定 Agent 定义 |
| `crux version` | `cmd/crux/main.go` versionCmd | 显示版本信息 + Claude CLI 检查 |
| `crux ps` | `cmd/crux/main.go` psCmd | 列出所有进程（支持 --json） |
| `crux kill <pid>` | `cmd/crux/main.go` killCmd | 终止指定进程 |
| `crux astrace <pid>` | `cmd/crux/main.go` astraceCmd | 实时追踪进程 syscall |

**全局 flags：** `--json`, `--verbose/-v`, `--quiet/-q`, `--model`, `--max-steps`, `--agent`

### CLI 输出格式参考

**正常运行输出模式（来自 internal/ui/ 组件）：**

```
[kernel] spawning PID 1...              ← Agent Progress Reporter
[agent/1] reasoning step 1...           ← Agent Progress Reporter
[result] 分析结果文本...                 ← Result Box (双线边框)
[kernel] PID 1 exited(0) | tokens: 1234 | elapsed: 6.2s   ← Summary Footer
```

**错误输出格式：**
```
✗ /dev/llm/claude: context deadline exceeded
→ 智能体推理超时
→ 建议: 检查网络连接或增加 --max-steps
```

**Version 输出格式：**
```
crux v0.1.0
claude-code: 1.x.x
```

**Version 错误（Claude CLI 未安装）：**
```
✗ claude-code CLI not found
→ 建议: npm install -g @anthropic-ai/claude-code
```

**PS 输出格式：**
```
PID   STATE     SKILL           TOKENS  ELAPSED
1     running   code-analyst    456     3.2s
2     zombie    -               123     1.1s
```

**astrace 输出格式：**
```
[astrace] attached to PID 1 (state: running)
[  0.013s] Open("/dev/llm/claude", O_RDWR)  = FD(3)    1ms
[  0.014s] Write(FD(3), 1234 bytes)          = ok      5200ms
[  5.214s] Read(FD(3), 65536)                = 892B      2ms
...
[astrace] detached from PID 1 (process exited)
```

### 参考 Agent/Skill 文件实际内容

**lib/agents/code-analyst/agent.yaml：**
```yaml
name: code-analyst
description: "分析代码质量、识别潜在问题并提供改进建议的智能体"
models:
  provider: claude
  preferred: sonnet
  fallback: haiku
context_budget: 8192
skills:
  - code-analysis
```

**lib/skills/code-analysis/SKILL.md frontmatter：**
```yaml
name: code-analysis
description: >
  Analyze code quality, identify bugs, performance issues and security
  vulnerabilities.
allowed-tools: /dev/fs /dev/shell
```

### 安装方式

```bash
go install github.com/usecrux/crux/cmd/crux@latest
```

**前置依赖：**
- Go 1.26+
- Claude Code CLI（`npm install -g @anthropic-ai/claude-code`），需配置有效 API 密钥
- 模块路径：`github.com/usecrux/crux`

### 与 Story 5.1（概念文档）的关系

快速上手指南与概念文档互补但不重叠：
- **概念文档**（`docs/concepts.md`）— 回答"是什么、为什么"：核心概念定义、Unix 类比、架构图
- **快速上手指南**（`docs/quick-start.md`）— 回答"怎么做"：安装步骤、第一个命令、预期输出

快速上手指南可在适当位置链接到概念文档，供希望深入理解的用户参考。但指南本身应完全自包含——不需要先读概念文档就能跟着操作。

### 前序 Story 经验

#### Story 5.1 经验

- **文档类 Story 不涉及代码修改** — 输出为纯 Markdown 文件
- **源码验证至关重要** — 所有示例中的命令、输出格式、VFS 路径必须与实际代码交叉验证
- **Code Review 发现的典型问题** — 示例中的细节不准确（如 O_RDONLY vs O_RDWR）、数据流步骤遗漏
- **Agent 配置格式** — Story 5.1 中已经确认了 Agent/Skill 实际的文件格式，Story 5.2 可直接复用这些经验
- **文件输出位置** — `docs/` 目录

#### Git 提交模式

每个 Story 通常有 3 个提交：`Add Story X.X` → `Transition to Review` → `Finalize`，commit message 使用英文。

### 范围边界

**本 Story 包含：**
- 创建 `docs/quick-start.md` 快速上手指南
- 完整的安装 → 首次运行 → astrace 体验 → 进程管理体验流程
- 每步操作的预期输出示例

**本 Story 不包含：**
- 概念讲解（已在 Story 5.1 `docs/concepts.md` 完成）
- 参考手册 / syscall 签名 / manifest 字段说明（Story 5.3 职责）
- 教程系列（写第一个 Skill 等，Phase 2）
- 英文翻译版本
- README.md 编写

### Project Structure Notes

**创建的新文件：**
```
docs/quick-start.md          — 快速上手指南（本 Story 唯一输出）
```

**不修改的文件：**
```
所有 .go 文件              — 本 Story 不涉及代码修改
docs/concepts.md           — 已由 Story 5.1 创建，不修改
```

### References

**规划文档：**
- [Source: _bmad-output/planning-artifacts/epics.md#Story 5.2] — Story 定义和验收标准
- [Source: _bmad-output/planning-artifacts/prd.md#FR39] — 快速上手指南功能需求（≤ 15 分钟）
- [Source: _bmad-output/planning-artifacts/prd.md#Documentation Strategy] — 文档策略
- [Source: _bmad-output/planning-artifacts/prd.md#Code Examples] — 参考 Agent/Skill 作为 demo 素材
- [Source: _bmad-output/project-context.md] — AI Agent 编码规则和项目上下文

**已完成的相关文档：**
- [Source: docs/concepts.md] — Story 5.1 产出的概念文档，快速上手指南可链接引用
- [Source: _bmad-output/implementation-artifacts/5-1-concept-documentation.md] — Story 5.1 完整 story context

**源码参考（验证 CLI 命令和输出格式）：**
- cmd/crux/main.go: rootCmd（Spawn Agent）, versionCmd, psCmd, killCmd, astraceCmd
- internal/ui/progress.go: Agent Progress Reporter 输出格式
- internal/ui/result.go: Result Box 输出格式
- internal/ui/error.go: Error Block 三段式错误格式
- internal/ui/summary.go: Summary Footer 输出格式
- internal/ui/trace.go: Syscall Trace Line 输出格式
- internal/ui/table.go: Process Table 输出格式

**参考 Agent/Skill 文件：**
- lib/agents/code-analyst/agent.yaml — Agent 配置示例
- lib/agents/code-analyst/instructions.md — Agent 指令示例
- lib/skills/code-analysis/SKILL.md — Skill 定义示例

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6

### Debug Log References

### Completion Notes List

- Task 1-9: 创建 `docs/quick-start.md` 快速上手指南，覆盖完整流程：前置条件 → 安装 → 首次运行 → Agent 使用 → astrace 调试 → 进程管理 → 下一步指引
- 所有 CLI 命令和输出格式已通过源码交叉验证（cmd/crux/main.go, internal/ui/*.go, debug/*.go）
- astrace 输出示例精确匹配 trace.go 实现：key=value 参数格式、`← LLM 调用`/`← 慢操作` 注解逻辑、`traceDuration` 时间格式
- 文档完全自包含，不依赖概念文档前置知识即可独立操作
- 渐进式结构：最简用法 → Agent → astrace → ps/kill，内容精简控制在 15 分钟可完成

### File List

- docs/quick-start.md (新增 + 审查修复) — 快速上手指南
- _bmad-output/implementation-artifacts/5-2-quick-start-guide.md (修改) — Story 文件更新
- _bmad-output/implementation-artifacts/sprint-status.yaml (修改) — Sprint 状态更新

### Code Review Record (2026-02-26)

**Reviewer:** Amelia (Dev Agent — Code Review Mode)
**Model:** Claude Opus 4.6

**审查发现与修复:**
- [H1] 首次执行示例改为 `crux "分析 ./README.md"` 匹配 AC #1（docs/quick-start.md:72）
- [M1] 补充 `crux ps --json` 预期 JSON 输出示例满足 AC #3（docs/quick-start.md:217-234）
- [M2] Story CLI Output Reference 修正为 `[agent/1] reasoning step 1...` 匹配代码实现（story:113）
- [L1] astrace 解读表 `← LLM 调用` 描述修正为"涉及 /dev/llm/ 设备的操作"（docs/quick-start.md:181）
- [L2] 添加 `<nil>` 含义说明帮助非 Go 用户理解（docs/quick-start.md:184）
- [L3] Story version 输出参考去掉 `v` 前缀匹配代码行为（story:128）

### Post Story 4-6 Update (2026-02-26)

更新 docs/quick-start.md 反映 IPC daemon 架构变更:
- 首次运行章节补充 daemon 自动启动/停止说明
- astrace 章节强调跨终端操作能力（daemon 架构支持）
- 进程管理章节补充跨终端可见性说明
- 补充无 daemon 时的优雅降级行为说明（crux ps → "No active processes."）
