# Epic 10: 监控与可观测性（Monitoring & Observability）

`crux top` 实时面板 + `crux log` 分类日志 + Token 预算管理——生产级可观测能力。

## Story 10.1: crux top 实时监控 TUI

As a 用户,
I want 通过 `crux top` 实时查看所有智能体的树状关系、状态和 token 消耗,
So that 我随时掌握系统全局运行态。

**Acceptance Criteria:**

**Given** `cmd/crux/top.go` 已实现（bubbletea TUI）
**When** 执行 `crux top`
**Then** 全屏显示实时监控面板
**And** 上方汇总区：活跃进程数、总 token 消耗、系统运行时间
**And** 下方进程列表：PID、PPID（树状缩进）、STATE、AGENT、TOKENS、ELAPSED

**Given** TUI 运行中
**When** 进程状态变化
**Then** 刷新间隔 ≤ 500ms（NFR28）
**And** 单核 CPU 占用 ≤ 5%（10 个并发进程场景）

**Given** 用户在 TUI 中选中进程
**When** 按 `k` 键
**Then** Kill 选中的进程（FR62）

**Given** 用户在 TUI 中选中进程
**When** 按 `Enter` 键
**Then** 显示进程详情（intent、skills、context 摘要）（FR62）

**Given** 按 `q` 键
**When** 退出 TUI
**Then** 恢复终端状态，不影响运行中的进程

## Story 10.2: crux log 分类推理日志

As a 用户,
I want 通过 `crux log <pid>` 查看智能体的推理日志，按类别分类显示,
So that 我无需深入内核就能排查问题。

**Acceptance Criteria:**

**Given** `cmd/crux/log.go` 已实现
**When** 执行 `crux log 5`
**Then** 输出 PID 5 的推理日志
**And** 按 `[think]`（推理过程）、`[tool]`（工具调用）、`[output]`（最终输出）三段式分类显示（FR60）

**Given** 使用过滤
**When** 执行 `crux log 5 --filter tool`
**Then** 仅显示 `[tool]` 类别的日志条目

**Given** 日志输出
**When** 从推理事件发生到终端显示
**Then** 延迟 ≤ 200ms（NFR29）

**Given** PID 不存在
**When** 执行 `crux log 999`
**Then** 输出 `✗ PID 999: process not found` + 建议

## Story 10.3: Token 预算管理

As a 用户,
I want 为智能体设置 token 预算上限，超限时系统自动终止推理,
So that 我可以控制 LLM 调用的成本。

**Acceptance Criteria:**

**Given** `kernel/kernel.go` 中预算检查已集成到推理循环
**When** Agent 的 agent.yaml 设置 `context_budget: 5000`
**Then** 系统在智能体消耗达到 5000 token 时终止推理（FR61）
**And** 进程转 Zombie，ExitStatus 记录原因为 `budget_exceeded`

**Given** Compose 中覆盖预算
**When** compose.yaml 中为特定智能体设置 `context_budget: 10000`
**Then** 使用 compose 中的值覆盖 agent.yaml 中的默认值

**Given** 预算即将耗尽（剩余 < 10%）
**When** 推理循环继续
**Then** 在 crux top 中显示黄色警告标记

---
