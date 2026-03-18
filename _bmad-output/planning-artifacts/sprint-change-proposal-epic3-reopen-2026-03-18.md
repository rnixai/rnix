# Sprint Change Proposal: 重开 Epic 3 — strace 可观测性增强

> **日期：** 2026-03-18
> **提案人：** Scrum Master
> **触发来源：** EchoMatrix 项目调试中发现的配置透明度和输出可观测性不足
> **变更范围：** Minor — 追加 2 个 Story 到已完成 Epic

---

## 1. 问题摘要

### 问题陈述

在 EchoMatrix 项目中使用 `rnix -i "..." --agent=scrum-master` 调试时发现两个可观测性问题：

1. **配置来源不透明**：agent.yaml 配置了 `provider: openrouter`，覆盖了项目级 `default_provider: cursor`，但用户无法从 strace 或 CLI 输出中判断 provider/model 最终来自哪一层配置。排查耗时远超预期。
2. **推理步骤输出不逐步**：cursor-cli driver 内部执行了多步操作（读文档、创建文件、更新状态等），但 CLI 只显示 `reasoning step 1...`，所有中间文本积攒到最终 Result 块一次性展示，无法实时感知进展。

### 发现上下文

- **何时发现：** EchoMatrix 项目首次 spawn scrum-master agent 时
- **如何发现：** 观察 CLI 输出：`[kernel] spawning PID 1 (openrouter/openrouter/hunter-alpha)...` 与项目配置不符
- **影响：** 配置排查依赖阅读源码而非工具输出，降低开发体验

### 问题清单

| # | 问题 | 严重度 | 根因 |
|---|------|--------|------|
| 1 | strace 无 ConfigResolve 事件，无法追踪 provider/model 来自哪层配置 | Medium | Spawn 流程中 provider 解析无事件记录 |
| 2 | CLI 输出只显示最终 Result，不逐步显示每个 reasoning step 的摘要 | Medium | OnStep callback 仅报告步骤号，不推送步骤内容 |

---

## 2. 影响分析

### 2.1 Epic 影响

#### Epic 3: 调试追踪（已完成 → 重开）

Epic 3 包含 4 个已完成 Story（3.1-3.4），本次追加 2 个增强 Story：

| Story | 内容 | 影响范围 |
|-------|------|----------|
| 3-5 (新增) | ConfigResolve strace 事件 | kernel/kernel.go, debug/strace.go |
| 3-6 (新增) | 推理步骤逐步输出 | kernel/kernel.go, ipc/, cmd/, ui/ |

**其他 Epic：无影响。**

### 2.2 PRD 文档影响

无需修改 PRD。两个 Story 均为已有功能的可观测性增强，不涉及新的 Functional Requirements。

### 2.3 架构文档影响

无需修改架构文档。使用已有的 strace 事件管道和 IPC 回调机制。

### 2.4 代码影响

#### Story 3-5 修改文件

| 文件 | 改动 |
|------|------|
| `kernel/kernel.go` | Spawn 中 provider/model 解析逻辑添加 source 追踪，新增 ConfigResolve 事件 |
| `kernel/kernel.go` | `resolveLLMDevice()` 返回 source 信息 |
| `debug/strace.go` | FormatEvent 添加 ConfigResolve 特殊格式化 |

#### Story 3-6 修改文件

| 文件 | 改动 |
|------|------|
| `kernel/kernel.go` | reasonStep 循环中每步结束后推送摘要 |
| `kernel/kernel.go` | KernelCallbacks 接口添加 OnStepOutput |
| `ipc/protocol.go` | ProgressPayload 添加 Text 字段 |
| `ipc/server.go` | callbackMux 实现 OnStepOutput |
| `cmd/rnix/main.go` | SpawnAndWatch callback 处理步骤输出 |
| `internal/ui/progress.go` | 添加 AgentStepOutput 渲染方法 |

---

## 3. 推荐方案

### 选择：追加 Story — 重开 Epic 3 添加 2 个增强 Story

### 理由

1. **自然归属** — ConfigResolve 事件和步骤输出都属于 strace/可观测性范畴，Epic 3 是最合适的归属
2. **低风险** — 使用已有基础设施（emitEvent、callbackMux），不修改核心逻辑
3. **高价值** — 直接提升日常调试体验，减少排查配置问题的时间
4. **无排期冲突** — Epic 26 仅剩 Story 26-5（review 状态），不影响

### 工作量评估

- **预估 Story 数：** 2 个
- **工作量级别：** 小（基于已有基础设施扩展）
- **风险等级：** 低

---

## 4. 实施交接

### 变更范围分类：**Minor**

| 角色 | 职责 |
|------|------|
| **SM** | 创建 Story 文件、更新 sprint-status.yaml |
| **开发者** | 实施 Story 3-5 和 3-6 |
| **SM** | 代码审查（可选） |

### 成功标准

1. `make all` 全部通过
2. `rnix strace` 中可见 ConfigResolve 事件，清晰显示 provider/model 来源层级
3. CLI spawn 输出中逐步显示每个 reasoning step 的摘要信息
4. 新增测试覆盖两个 Story 的核心 AC

---

## 附录: 检查清单

| Section | 状态 |
|---------|------|
| 问题识别与上下文 | [x] Done |
| Epic 影响分析 | [x] Done — Epic 3 重开 |
| PRD 影响 | [x] N/A — 无需修改 |
| 架构影响 | [x] N/A — 无需修改 |
| 代码影响评估 | [x] Done |
| 推荐方案 | [x] Done — 追加 Story |
| 用户审批 | [ ] Pending |
