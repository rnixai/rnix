# Story 12.1: 教程文档（Tutorial Documentation）

Status: done

## Story

As a 开发者,
I want 阅读教程文档学会编写 Skill、调试 bug 和组合多智能体工作流,
So that 我可以在 Rnix 上构建自己的应用。

## Acceptance Criteria

1. **AC1: 编写第一个 Skill 教程**
   - Given 教程文档已编写
   - When 阅读"编写第一个 Skill"教程
   - Then 包含从创建 SKILL.md 到 Agent 引用到 spawn 执行的完整流程
   - And 包含完整可运行示例（FR69）

2. **AC2: 调试第一个 bug 教程**
   - Given 教程文档已编写
   - When 阅读"调试第一个 bug"教程
   - Then 包含故意引入 bug → strace 定位 → 修复 → 验证的完整流程
   - And 包含完整可运行示例

3. **AC3: 组合多智能体工作流教程**
   - Given 教程文档已编写
   - When 阅读"组合多智能体工作流"教程
   - Then 包含编写 rnix-compose.yaml → compose up → rnix top 监控 → 查看结果的完整流程
   - And 包含完整可运行示例

## Tasks / Subtasks

- [x] Task 1: 创建 `docs/tutorials/` 目录并建立教程框架 (AC: #1, #2, #3)
  - [x] 1.1 创建 `docs/tutorials/` 目录
  - [x] 1.2 创建 `docs/tutorials/README.md`——教程导航页，列出三篇教程及其面向人群和预计阅读时间
  - [x] 1.3 创建三个教程文件骨架：`writing-first-skill.md`、`debugging-first-bug.md`、`composing-multi-agent-workflow.md`

- [x] Task 2: 编写"编写第一个 Skill"教程 (AC: #1)
  - [x] 2.1 编写教程前言：目标读者（首次使用 Rnix 的开发者）、前置条件（Rnix 已安装、Claude Code CLI 可用）、预计完成时间（~20 分钟）
  - [x] 2.2 编写步骤一：创建 Skill 目录和 `SKILL.md` 文件
  - [x] 2.3 编写步骤二：创建引用该 Skill 的 Agent
  - [x] 2.4 编写步骤三：运行 Skill
  - [x] 2.5 编写完整可运行示例汇总
  - [x] 2.6 编写常见问题与排错节（Skill 找不到、权限错误、Agent 加载失败等）

- [x] Task 3: 编写"调试第一个 bug"教程 (AC: #2)
  - [x] 3.1 编写教程前言：目标读者、前置条件、预计完成时间（~15 分钟）
  - [x] 3.2 编写步骤一：准备一个有 bug 的 Skill
  - [x] 3.3 编写步骤二：使用 `rnix strace` 定位问题
  - [x] 3.4 编写步骤三：修复 bug 并验证
  - [x] 3.5 编写扩展调试技巧

- [x] Task 4: 编写"组合多智能体工作流"教程 (AC: #3)
  - [x] 4.1 编写教程前言：目标读者、前置条件（至少完成第一个 Skill 教程）、预计完成时间（~25 分钟）
  - [x] 4.2 编写步骤一：设计多智能体工作流
  - [x] 4.3 编写步骤二：编写 `rnix-compose.yaml`
  - [x] 4.4 编写步骤三：运行 `rnix compose up`
  - [x] 4.5 编写步骤四：使用 `rnix top` 实时监控
  - [x] 4.6 编写步骤五：查看结果
  - [x] 4.7 编写扩展场景

- [x] Task 5: 交叉引用与导航 (AC: #1, #2, #3)
  - [x] 5.1 在每篇教程中添加指向其他教程的"下一步"链接
  - [x] 5.2 在每篇教程中添加指向参考手册（`docs/reference.md`）和概念文档（`docs/concepts.md`）的相关链接
  - [x] 5.3 在 `docs/quick-start.md` 末尾添加教程导航入口
  - [x] 5.4 确保所有内部链接正确可跳转

- [x] Task 6: 校验与完善 (AC: #1, #2, #3)
  - [x] 6.1 检查所有 CLI 命令与实际 Rnix 实现一致（命令格式、flag 名称、输出格式）
  - [x] 6.2 检查所有 VFS 路径、syscall 名称、配置字段名与代码实现一致
  - [x] 6.3 检查所有 YAML 示例语法正确（agent.yaml、SKILL.md frontmatter、rnix-compose.yaml）
  - [x] 6.4 最终审读：行文流畅、步骤连贯、无遗漏的假设条件
  - [x] 6.5 确认所有文档使用简体中文书写

## Dev Notes

### 架构与实现约束

**文档类型：** 纯 Markdown 文档，不涉及 Go 代码修改。所有文件存放在 `docs/tutorials/` 目录下。

**输出语言：** 简体中文（与项目配置 `document_output_language: Chinese` 一致）。

**现有文档体系：** Phase 1 已建立 `docs/` 目录下三个文档：
- `docs/concepts.md` — 核心概念（进程、VFS、Skill、Syscall）
- `docs/quick-start.md` — 15 分钟快速上手
- `docs/reference.md` — 权威技术参考（1544 行，覆盖 Syscall、VFS、Agent/Skill、CLI、IPC、内部实现）

教程文档是这三者的延伸——概念文档建立心智模型，快速上手指南让你跑起来，参考手册是查阅字典，**教程是手把手带你做项目**。

### 关键技术参考

**Skill 体系（教程一需要精确引用）：**
- `SKILL.md` 格式：frontmatter（name/version/description/tags/tools）+ body 指令
- `allowed-tools` 映射 VFS 路径白名单：`/dev/fs`（文件访问）、`/dev/shell`（Shell 执行）、`/dev/llm/claude`（LLM 调用）
- 渐进式加载：发现阶段（frontmatter ~100 tokens）→ 激活阶段（完整 body < 5000 tokens）
- 参考代码：`skills/loader.go`、`skills/types.go`
- 参考文档：`docs/reference.md` §3.5-3.8

**Agent 体系（教程一需要精确引用）：**
- `agent.yaml` 字段：name、description、model（default/reasoning/fast）、skills 列表、mcp 配置
- `instructions.md`：Agent 系统提示词
- Agent 加载流程：`agents/loader.go`
- 参考文档：`docs/reference.md` §3.1-3.4
- 现有示例：`lib/agents/code-analyst/agent.yaml`

**调试体系（教程二需要精确引用）：**
- `rnix strace <pid>`：实时追踪 syscall 事件
- SyscallEvent 结构：Syscall、PID、FD、Device、Args、Result、Err、Duration
- ErrCode 枚举：`TIMEOUT`、`NOT_FOUND`、`PERMISSION`、`INTERNAL`、`DRIVER`
- 参考代码：`debug/event.go`、`debug/strace.go`
- 参考文档：`docs/reference.md` §4.5（strace 命令）、§6.5（SyscallError）

**Compose 体系（教程三需要精确引用）：**
- `rnix-compose.yaml` 格式：services（name、intent、agent、depends_on、environment）
- DAG 调度引擎：拓扑排序、层级并行执行
- 参考代码：`compose/parser.go`、`compose/dag.go`、`compose/engine.go`
- CLI 命令：`rnix compose up`、`rnix compose down`
- 参考文档：暂无独立文档（Phase 2 新增功能），需从代码和 story 文件中提取

**监控体系（教程三需要引用）：**
- `rnix top`：实时 TUI 监控
- `rnix log`：分类推理日志
- 参考代码：`cmd/rnix/top.go`、`cmd/rnix/log.go`

**AgentShell 语法（教程三扩展场景需要引用）：**
- 管道语法：`spawn "A" | spawn "B" | spawn "C"`
- 变量与环境：`export KEY=VAL`、`result = spawn "意图"`
- 控制结构：`if $result.exitcode == 0 ... else ... end`、`on-error`
- 参考代码：`shell/parser.go`、`shell/pipe.go`、`shell/env.go`、`shell/script.go`

### CLI 命令参考（确保教程中使用准确）

```
rnix -i "意图"                    # 根命令：spawn 智能体
rnix -i "意图" --agent=name       # 指定 Agent
rnix -i "意图" --model=model      # 指定模型
rnix -i "意图" --json             # JSON 输出
rnix ps                           # 进程列表
rnix ps --json                    # JSON 格式进程列表
rnix kill <pid>                   # 终止进程
rnix strace <pid>                # Syscall 追踪
rnix compose up                   # 启动 compose
rnix compose down                 # 停止 compose
rnix top                          # 实时监控 TUI
rnix log                          # 查看推理日志
rnix version                      # 版本信息
```

### 项目结构参考

```
lib/
├── agents/
│   └── code-analyst/
│       ├── agent.yaml
│       └── instructions.md
└── skills/
    └── code-analysis/
        └── SKILL.md
docs/
├── concepts.md      # Phase 1
├── quick-start.md   # Phase 1
├── reference.md     # Phase 1
└── tutorials/       # 本 Story 新增
    ├── README.md
    ├── writing-first-skill.md
    ├── debugging-first-bug.md
    └── composing-multi-agent-workflow.md
```

### 前序 Story 经验

**Phase 1 文档经验（Epic 5）：**
- Story 5.1（概念文档）、5.2（快速上手）、5.3（参考手册）均为文档 Story
- 关键学习：CLI 命令格式必须与实际代码完全一致，VFS 路径必须精确匹配
- 文档质量取决于对代码实现的精准理解，需仔细读取源码验证
- 中文技术写作保持一致的术语翻译（Process=进程、VFS=虚拟文件系统等）

**Epic 11（AgentShell）回顾经验：**
- shell 包新增 957 行源码 + 2261 行测试
- 管道语法、变量环境、控制结构已完整实现
- 教程三的"扩展场景"需要展示这些 Phase 2 新功能

### Git 最近提交

最近 5 个提交：
1. `fa6ca0e` Complete Epic 11: AgentShell Advanced Syntax Implementation
2. `55adcf8` Add Traceability Matrix and Report for Story 11.3
3. `08fef19` Finalize Story 11.3: Minimal Control Structures Implementation
4. `4bfee85` Update Story 11.3 to Review Status
5. `444df85` Add Story 11.3: Minimal Control Structures Tests

### References

- [Source: _bmad-output/planning-artifacts/epics/epic-12-phase-2-文档phase-2-documentation.md]
- [Source: docs/concepts.md — Phase 1 概念文档]
- [Source: docs/quick-start.md — Phase 1 快速上手]
- [Source: docs/reference.md — Phase 1 参考手册（1544 行）]
- [Source: lib/agents/code-analyst/agent.yaml — 现有 Agent 示例]
- [Source: skills/loader.go — Skill 加载逻辑]
- [Source: agents/loader.go — Agent 加载逻辑]
- [Source: debug/event.go — SyscallEvent 结构]
- [Source: debug/strace.go — strace 追踪逻辑]
- [Source: compose/parser.go — Compose YAML 解析]
- [Source: compose/dag.go — DAG 调度引擎]
- [Source: compose/engine.go — Compose 执行引擎]
- [Source: cmd/rnix/main.go — CLI 入口]
- [Source: cmd/rnix/top.go — rnix top 实现]
- [Source: cmd/rnix/log.go — rnix log 实现]
- [Source: shell/parser.go — 管道语法解析]
- [Source: shell/pipe.go — 管道执行引擎]
- [Source: shell/env.go — 变量环境管理]
- [Source: shell/script.go — 脚本解析与控制结构]
- [Source: _bmad-output/implementation-artifacts/epic-11-retro-2026-03-03.md — Epic 11 回顾]

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6 (high-thinking)

### Debug Log References

无调试需要——文档 Story，所有测试一次通过。

### Completion Notes List

- 创建了 `docs/tutorials/` 目录和 4 个文件（README + 3 篇教程）
- 教程 1（编写第一个 Skill）：覆盖 SKILL.md 创建、agent.yaml 编写、运行与 strace 追踪，包含完整示例和常见问题排错
- 教程 2（调试第一个 bug）：故意引入权限 bug → strace 定位 → 修复 → 验证的完整流程，含修复前后对比
- 教程 3（组合多智能体工作流）：rnix-compose.yaml 编写、compose up 启动、rnix top 监控、结果查看，含管道语法/变量/条件分支扩展场景
- 在 quick-start.md 末尾添加了教程导航入口
- 所有教程包含交叉引用（concepts.md、reference.md、其他教程）
- 12 个文档验证测试全部通过
- 回归测试：19 个包中 18 个通过，kernel 包有 1 个预先存在的失败（TestIntegration_ReapProcess_MCPUnmountOnExit，与本 Story 无关）

### Senior Developer Review (AI)

**Review Date:** 2026-03-03
**Review Outcome:** Approve (with fixes applied)
**Findings:** 2 HIGH, 1 MEDIUM, 2 LOW — all fixed

**Action Items (all resolved):**
- [x] [HIGH] strace Read 输出格式修正——Result 应为字节数而非文件内容，Args 应包含 fd 和 length
- [x] [HIGH] rnix ps 列名修正——使用 SKILL 列名而非 AGENT，与 internal/ui/table.go 一致
- [x] [MEDIUM] docs_test.go 对 SKILL.md frontmatter 断言改为检查 allowed-tools: 而非 tools:
- [x] [LOW] 教程间 strace 格式已与 quick-start.md 统一
- [x] [LOW] code-analyst Agent 依赖确认存在

### Change Log

- 2026-03-03: 完成 Story 12.1 全部 6 个 Task，Status: review
- 2026-03-03: Code Review 发现 5 个问题（2H+1M+2L），全部修复，Status: done

### File List

- docs/tutorials/README.md (新增)
- docs/tutorials/writing-first-skill.md (新增)
- docs/tutorials/debugging-first-bug.md (新增)
- docs/tutorials/composing-multi-agent-workflow.md (新增)
- docs/docs_test.go (新增)
- docs/quick-start.md (修改——添加教程导航入口)
