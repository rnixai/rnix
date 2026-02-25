---
date: '2026-02-25'
triggered_by: Decker
scope: major
status: approved
approved_at: '2026-02-25'
affects:
  - prd
  - architecture
  - epics
  - project-context
  - skills/types.go
  - skills/loader.go
  - kernel/kernel.go
  - cmd/crux/main.go
  - lib/skills/code-analyst/
---

# Sprint 变更提案：引入 Agent 抽象层 + Skill 对齐行业标准

## 1. 问题摘要

### 触发背景

Epic 2（Skill 能力与文件访问）全部完成后，审视设计发现两个相互关联的架构问题：

1. **Skill 概念职责混淆**：当前 Skill（manifest.yaml + instructions.md）同时承担了"智能体定义"和"能力模块"双重职责。SkillManifest 包含 Models（模型偏好）和 ContextBudget（上下文预算），这些是智能体级别的配置，不是共享库该有的。
2. **缺少 Agent 抽象层**：架构中只有 Process（运行时实例）和 Skill（全能定义），缺少"Agent"（智能体定义/可执行程序）这一层。
3. **Skill 格式与行业标准不兼容**：Agent Skills 开放标准（agentskills.io，由 Anthropic 发起，30+ AI 工具采用）定义了 Skill 的标准格式（SKILL.md），Crux 当前的 manifest.yaml + instructions.md 双文件格式无法与生态互操作。

### 证据

| 来源 | 证据 |
|------|------|
| PRD | "Skills 即共享库"——但当前 Skill 包含 instructions.md（角色定义）、models（模型偏好）、context_budget（资源约束），远超共享库范畴 |
| PRD 旅程 3 | `agents: reviewer: skills: [pr-reviewer]`——Agent 和 Skill 是分离的两个概念 |
| `skills/types.go` | `SkillManifest` 包含 `Models` 和 `ContextBudget`，这些是 Agent 级配置 |
| Agent Skills 标准 | Skill 标准中不包含 models/context_budget，只有 name/description/allowed-tools/metadata |
| 架构文档 | Phase 2 扩展提到 `SkillManager(SkillLoad/Invoke/Unload)`，暗示 Skill 是可被 invoke 的能力模块 |

## 2. 影响分析

### 2.1 Epic 影响

| Epic | 状态 | 影响程度 | 说明 |
|------|------|---------|------|
| Epic 1 | done | 中等 | Spawn 签名需调整，Skill 注入逻辑需改为 Agent 注入 |
| Epic 2 | done | 重大 | skills/ 包需重构，code-analyst 需拆分为 Agent + Skill |
| Epic 3 | backlog | 无影响 | astrace 追踪 syscall，与 Agent/Skill 概念无关 |
| Epic 4 | backlog | 无影响 | 进程管理与 Agent/Skill 概念分离 |
| Epic 5 | backlog | 中等 | 文档需反映 Agent 概念和 Skill 标准格式 |

### 2.2 已完成代码影响

| 文件 | 影响 |
|------|------|
| `skills/types.go` | 重大：移除 Models/ContextBudget，改为解析 SKILL.md frontmatter |
| `skills/loader.go` | 重大：改为解析 SKILL.md 格式，实现渐进式加载 |
| `skills/loader_test.go` | 重大：重写测试 |
| `lib/skills/code-analyst/` | 重大：拆分为 Agent + Skill，迁移到标准格式 |
| `kernel/kernel.go` | 中等：Spawn 接受 AgentInfo 而非 SkillInfo |
| `cmd/crux/main.go` | 中等：--skill flag → --agent flag |

### 2.3 文档影响

| 文档 | 影响 |
|------|------|
| PRD | 重大：FR23-FR27 需修订，新增 FR25a/FR25b |
| Architecture | 重大：新增 agents/ 包、更新依赖方向图、更新项目结构 |
| Epics | 重大：新增 Story 2.6 |
| Project Context | 中等：更新术语和概念 |

## 3. 推荐路径

### 选择：直接调整（在当前代码基础上引入 Agent 层 + Skill 标准化）

**理由：**

1. Epic 3-5 尚未开始，现在修正概念模型成本最低
2. 改动集中在 skills/ 包和 lib/ 目录，不影响 kernel/vfs/drivers/context/debug 核心
3. 与 PRD Phase 2 Compose 设计（agents: 概念）自然对齐
4. Agent Skills 行业标准兼容性为 Crux 带来生态优势
5. 概念清晰度对 MVP 文档（Epic 5）至关重要

**排除的选项：**

- 回滚 Epic 2：不可行，核心价值（FS 驱动、Shell 驱动、权限白名单机制）与此问题无关
- 推迟到 Phase 2：不推荐，Epic 5 文档会用错误术语，Phase 2 Compose 需要 Agent 层，届时改动更大

## 4. 详细变更提案

### 4.1 目标架构层次

```
Process（运行时实例）
  ← Agent（智能体定义：身份 + 模型 + 策略 + Skill 引用）
      ← Skill(s)（能力模块：遵循 Agent Skills 行业标准）
```

OS 隐喻对应：Process = 进程，Agent = 可执行程序，Skill = 共享库

### 4.2 Skill 设计（遵循 Agent Skills 标准 agentskills.io）

**目录结构：**

```
lib/skills/code-analysis/
├── SKILL.md          # 必需：标准格式（YAML frontmatter + Markdown 指令）
├── scripts/          # 可选：可执行脚本
├── references/       # 可选：参考文档
└── assets/           # 可选：模板、资源
```

**SKILL.md 格式遵循标准规范：**

| 字段 | 必需 | 约束 | Crux 映射 |
|------|------|------|----------|
| `name` | 是 | ≤64字符，小写+连字符，匹配目录名 | Skill 名称 |
| `description` | 是 | ≤1024字符，描述做什么+何时使用 | 发现阶段匹配依据 |
| `allowed-tools` | 否 | 空格分隔工具列表（实验性） | Crux `/dev/` 设备权限白名单 |
| `metadata` | 否 | 任意键值对 | 扩展字段 |
| `compatibility` | 否 | ≤500字符，环境要求 | 标注 Crux 特定需求 |
| `license` | 否 | 许可证名称 | 许可证 |
| SKILL.md body | — | Markdown，< 5000 tokens 推荐 | 激活后加载的程序性知识 |

**渐进式加载（Progressive Disclosure）：**

1. **发现**（启动时）：扫描 skill 目录，仅解析 SKILL.md frontmatter → ~100 tokens/skill
2. **激活**（任务匹配时）：加载完整 SKILL.md body → < 5000 tokens
3. **执行**（按需）：加载 scripts/、references/、assets/ 中的文件

**SKILL.md 示例（lib/skills/code-analysis/SKILL.md）：**

```markdown
---
name: code-analysis
description: >
  Analyze code quality, identify bugs, performance issues and security
  vulnerabilities. Use when the user wants to review code files or
  find problems in source code.
allowed-tools: /dev/fs /dev/shell
metadata:
  author: crux
  version: "1.0"
compatibility: Designed for Crux
---

# Code Analysis

## When to use this skill
Use this skill when the user asks to analyze, review, or audit source code
for quality issues, bugs, or improvement opportunities.

## How to analyze code
1. Read the target file(s) via /dev/fs
2. Examine code structure, naming conventions, error handling patterns
3. Look for common anti-patterns and potential bugs
4. Run static analysis tools via /dev/shell if available
5. Report findings with severity, location, and fix suggestions

## Output format
Each finding should include:
- **Severity**: critical / warning / info
- **Location**: file:line
- **Description**: What the issue is
- **Suggestion**: How to fix it

## Common patterns to check
- Unchecked error returns
- Resource leaks (unclosed files, connections)
- Race conditions in concurrent code
- Security vulnerabilities (injection, path traversal)
- Performance anti-patterns
```

### 4.3 Agent 设计（Crux 特有概念）

**目录结构：**

```
lib/agents/code-analyst/
├── agent.yaml        # Agent 配置：身份 + 模型 + Skill 引用
└── instructions.md   # Agent 角色定义 + 行为策略
```

**agent.yaml 示例：**

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

**instructions.md 示例：**

```markdown
# Code Analyst Agent

You are a senior code analyst with deep expertise in software quality.

## Role
- Provide thorough, actionable code analysis
- Focus on real, impactful issues over style nitpicks
- Prioritize security and correctness over performance

## Strategy
- Read the full file before making judgments
- Consider the broader codebase context
- Rank findings by severity
- Provide concrete fix suggestions, not just problem descriptions
```

**Agent vs Skill 职责分离：**

| 维度 | Agent | Skill |
|------|-------|-------|
| 定义 | "我是谁"——身份、角色、策略 | "如何做 X"——程序性知识、工作流 |
| 模型偏好 | ✅ models | ❌ |
| 上下文预算 | ✅ context_budget | ❌ |
| 设备权限 | ❌ 由引用的 Skill 聚合 | ✅ allowed-tools |
| 复用性 | 特定角色 | 跨 Agent 共享，跨平台兼容 |
| 标准 | Crux 特有 | Agent Skills 行业标准 |

### 4.4 Spawn 流程

```
crux "分析代码" --agent=code-analyst

1. AgentLoader 加载 lib/agents/code-analyst/agent.yaml
   → 获取 models、context_budget、skills 引用列表
2. AgentLoader 加载 lib/agents/code-analyst/instructions.md
   → 获取 Agent 角色系统提示
3. SkillLoader 加载每个引用的 Skill（渐进式：激活阶段加载 SKILL.md body）
4. 聚合所有 Skill 的 allowed-tools → 设备权限白名单
5. 组装 system prompt = Agent instructions + Skill instructions
6. Spawn Process（使用 agent 的 model 偏好 + 聚合的权限白名单）
```

### 4.5 代码变更清单

**新增：**

| 文件 | 说明 |
|------|------|
| `agents/types.go` | AgentManifest、AgentModels、AgentInfo 类型定义 |
| `agents/loader.go` | AgentLoader：加载 agent.yaml + instructions.md + 解析 Skill 引用 + 聚合 tools |
| `agents/loader_test.go` | Agent 加载器测试 |
| `lib/agents/code-analyst/agent.yaml` | 参考 Agent 定义 |
| `lib/agents/code-analyst/instructions.md` | Agent 角色指令 |
| `lib/skills/code-analysis/SKILL.md` | 标准格式 Skill |

**修改：**

| 文件 | 变更 |
|------|------|
| `skills/types.go` | 简化 SkillManifest：移除 Models/ContextBudget，改为解析 SKILL.md frontmatter |
| `skills/loader.go` | 改为解析 SKILL.md 格式，实现渐进式加载（metadata-only / full） |
| `skills/loader_test.go` | 更新测试用例和 testdata |
| `kernel/kernel.go` | Spawn 接受 AgentInfo 而非 SkillInfo |
| `cmd/crux/main.go` | --skill flag → --agent flag，注入 AgentLoader |

**删除：**

| 文件 | 说明 |
|------|------|
| `lib/skills/code-analyst/manifest.yaml` | 被 SKILL.md 替代 |
| `lib/skills/code-analyst/instructions.md` | 角色部分迁入 Agent，程序性知识迁入 SKILL.md |

**依赖方向更新：**

```
internal/types/  ← 所有包均可导入（零外部依赖）
internal/xsync/  ← 所有包均可导入（仅依赖 internal/types/）
internal/ui/     ← 仅 cmd/ 导入

cmd/ → kernel/ → vfs/     → drivers/{llm,shell,fs}
                → context/
                → agents/  → skills/
cmd/ → debug/（仅依赖 internal/types/）
```

### 4.6 PRD 需求修订

**移除原 FR23-FR27（Skill Management），替换为：**

**Agent Management（智能体定义管理）：**

- FR23: 系统可以从 agent.yaml 读取 Agent 的元信息（名称、描述、模型偏好、上下文预算、Skill 引用列表）
- FR24: 系统可以从 Agent 的 instructions.md 读取角色定义并注入智能体的 system prompt
- FR25: 用户可以在 spawn 时通过 --agent=\<name\> 指定 Agent 定义

**Skill Management（能力模块管理，遵循 Agent Skills 行业标准）：**

- FR25a（新增）: 系统可以从 SKILL.md 解析 Skill 元信息（name、description、allowed-tools），格式遵循 Agent Skills 开放标准（agentskills.io）
- FR25b（新增）: 系统支持 Skill 的渐进式加载——启动时仅加载 frontmatter，激活时加载完整 SKILL.md body，执行时按需加载 scripts/references/assets
- FR26: Agent 引用的所有 Skill 的 allowed-tools 聚合后映射为智能体的可用 /dev/ 设备权限白名单
- FR27: 系统交付参考 Agent（code-analyst）+ 参考 Skill（code-analysis），能够分析代码并识别至少 1 个可验证的真实代码问题

### 4.7 Epics 变更

**Epic 2 新增 Story 2.6：**

```
Story 2.6: Agent 抽象层与 Skill 标准化

As a 用户,
I want Agent 和 Skill 是清晰分离的两个概念，且 Skill 遵循行业标准格式,
So that Agent 定义"我是谁"（身份+策略+模型），
     Skill 定义"如何做 X"（程序性知识+工具权限），
     且 Skill 可与 30+ AI 工具生态兼容。

Acceptance Criteria:
- agents/types.go: AgentManifest 包含 Name/Description/Models/ContextBudget/Skills
- agents/loader.go: 加载 agent.yaml + instructions.md + 解析 Skill 引用 + 聚合 tools
- skills/types.go: SkillManifest 仅包含 Name/Description/AllowedTools/Metadata
- skills/loader.go: 解析 SKILL.md 标准格式，支持渐进式加载
- lib/agents/code-analyst/: agent.yaml + instructions.md（参考 Agent）
- lib/skills/code-analysis/: SKILL.md（标准格式参考 Skill）
- kernel/kernel.go: Spawn 接受 AgentInfo 而非 SkillInfo
- cmd/crux/main.go: --agent flag 替代 --skill
- AgentLoader 聚合所有引用 Skill 的 allowed-tools 为统一权限白名单
- 端到端验证：crux "分析代码" --agent=code-analyst 工作正常
- 所有现有测试通过（更新后）
```

## 附录：MCP 标准兼容性评估

### 背景

MCP（Model Context Protocol）是由 Anthropic 发起的开放标准，用于连接 AI 应用与外部系统。Crux PRD Phase 2 规划了 MCP 集成（`/mnt/mcp/` VFS 挂载 + Claude Code CLI `--mcp-config` 传递）。本节评估 Agent/Skill 重设计是否影响 MCP Phase 2 集成。

### MCP 核心原语

| MCP 原语 | 说明 | 协议 | 控制者 |
|----------|------|------|--------|
| **Tools** | 可执行函数，LLM 主动调用 | `tools/call(name, args)` JSON-RPC | 模型 |
| **Resources** | 只读数据源，提供上下文 | `resources/read(uri)` JSON-RPC | 应用 |
| **Prompts** | 可复用指令模板 | `prompts/get(name, args)` JSON-RPC | 用户 |

### Crux 四层能力模型（含 MCP Phase 2）

本次 Agent/Skill 重设计后，Crux 的能力模型从 PRD 原始的三层升级为四层：

```
Agent    → 智能体定义（身份 + 模型 + 策略 + Skill/MCP 引用）
Skill    → 程序性知识 + 工具权限（Agent Skills 行业标准，/lib/skills/）
MCP      → 外部服务集成（MCP 标准，/mnt/mcp/）
Devices  → Crux 原生设备驱动（/dev/llm/ /dev/fs /dev/shell）
```

### Agent/Skill/MCP/Device 职责划分

| 概念 | 来源 | 定义 | 提供什么 |
|------|------|------|---------|
| Agent instructions | Crux 特有 | "我是谁"——角色、策略、行为准则 | system prompt 中的身份部分 |
| Skill SKILL.md | Agent Skills 标准 | "如何做 X"——步骤、模式、最佳实践 | system prompt 中的程序性知识 + 工具权限 |
| MCP Tools | MCP 标准 | "调用什么外部服务"——API 函数 | 运行时可调用的外部工具 |
| MCP Resources | MCP 标准 | "读取什么外部数据"——数据源 | 运行时可访问的外部上下文 |
| MCP Prompts | MCP 标准 | "怎么用某个服务"——服务交互模板 | 特定服务的使用引导 |
| Crux Devices | Crux 特有 | "本地能力"——LLM/Shell/FS | 基础 I/O 能力 |

**三者互补关系示例：**

```
Agent "security-auditor"
  ├── instructions.md: "你是安全审计专家，关注 OWASP Top 10..."
  ├── Skills:
  │   └── code-analysis/SKILL.md: "如何分析代码：读取文件→检查模式→报告..."
  │       allowed-tools: /dev/fs /dev/shell
  └── MCP (Phase 2):
      └── sentry-server:
          ├── Tools: searchIssues(), getEventDetails()
          ├── Resources: project-schema, error-patterns
          └── Prompts: "analyze-error" 模板
```

### MCP Prompts 与 Skill 的关系

MCP Prompts 和 Agent Skills 的 SKILL.md 有表面相似性（都包含指令），但职责不同：

| 维度 | MCP Prompts | Skill SKILL.md |
|------|-------------|----------------|
| 来源 | 外部 MCP 服务器动态提供 | 本地文件系统静态定义 |
| 粒度 | 单个交互模板（如"查询数据库"） | 完整能力领域（如"代码分析"） |
| 发现 | `prompts/list` JSON-RPC 动态发现 | 文件系统扫描 + frontmatter 静态发现 |
| 生命周期 | 随 MCP 服务器连接/断开而变化 | 安装后持久存在 |
| 标准 | MCP 标准 | Agent Skills 标准 |
| 互操作 | 与所有 MCP 客户端兼容 | 与 30+ Agent Skills 兼容工具兼容 |

**结论：互补而非重叠。** Skill 提供领域级程序性知识，MCP Prompts 提供服务级交互模板。

### Phase 2 Agent Manifest 扩展预览

当前 MVP agent.yaml：

```yaml
name: code-analyst
skills:
  - code-analysis
```

Phase 2 扩展（无需修改 MVP 结构，只需添加字段）：

```yaml
name: security-auditor
skills:
  - code-analysis
  - vulnerability-patterns
mcp:                          # Phase 2 新增
  - sentry                    # MCP 服务器引用
  - github
```

### 兼容性结论

| 检查项 | 结果 |
|--------|------|
| `/mnt/mcp/` VFS 挂载设计与 MCP 标准兼容？ | ✅ 方向正确 |
| Agent/Skill 重设计与 MCP Phase 2 冲突？ | ✅ 无冲突 |
| agent.yaml 可扩展支持 MCP 引用？ | ✅ 添加 `mcp` 字段即可 |
| Skill `allowed-tools` 与 MCP 路径命名空间冲突？ | ✅ `/dev/` vs `/mnt/mcp/` 分离 |
| MCP Prompts 与 Skill/Agent 概念混淆风险？ | ⚠️ 已在本节明确划分职责 |

**不需要额外变更提案。** 本节作为架构备忘录纳入变更提案，供 Phase 2 MCP 设计时参考。

---

## 5. 实施交接

### 变更范围：重大（Major）

### 交接计划

| 步骤 | 角色 | 任务 |
|------|------|------|
| 1 | PM | 修订 PRD：FR23-FR27 → Agent + Skill 双层需求 |
| 2 | Architect | 更新 Architecture：新增 agents/ 包、类型定义、依赖方向、Spawn 流程 |
| 3 | SM | 更新 Epics：新增 Story 2.6；更新 sprint-status.yaml |
| 4 | SM | 创建 Story 2.6 上下文文件供 Dev 实施 |
| 5 | Dev | 实施 Story 2.6 |
| 6 | Dev | 更新 project-context.md |
| 7 | QA | 端到端验证 |

### 成功标准

- [ ] agents/types.go 和 skills/types.go 职责清晰分离
- [ ] Skill 遵循 Agent Skills 标准（SKILL.md 格式、渐进式加载）
- [ ] crux "分析代码" --agent=code-analyst 端到端工作正常
- [ ] Agent 能正确聚合多个 Skill 的 allowed-tools 权限
- [ ] 所有现有测试通过
- [ ] PRD、Architecture、Epics 文档已更新
- [ ] project-context.md 反映新概念
