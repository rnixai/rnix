# Epic 7: Compose 多智能体编排（Agent Compose）

用户通过 `rnix-compose.yaml` 声明式定义多智能体工作流，Compose 引擎解析 DAG 依赖自动调度——20 行 YAML 替代 2000 行硬编码。

## Story 7.1: rnix-compose.yaml 解析与 DAG 调度引擎

As a 用户,
I want 通过 YAML 文件声明式定义多智能体工作流及其依赖关系,
So that 系统自动按正确顺序调度执行。

**Acceptance Criteria:**

**Given** `compose/engine.go` 已实现
**When** 解析 `rnix-compose.yaml`
**Then** 正确提取每个智能体的 `intent`、`agent` 引用、`skills` 列表和 `depends_on` 依赖
**And** 构建 DAG（有向无环图）表示依赖关系

**Given** YAML 中存在循环依赖
**When** 解析
**Then** 返回清晰的错误信息，标注循环路径

**Given** DAG 已构建
**When** 执行调度
**Then** 按拓扑顺序启动智能体
**And** 无依赖的分支自动并行化
**And** ≤ 10 个智能体的启动延迟 ≤ 2s（不含 LLM 调用，NFR21）

**Given** 智能体 B 声明 `depends_on: { A: completed }`
**When** 智能体 A 完成
**Then** 智能体 B 自动启动
**And** 智能体 A 的输出可通过管道注入 B 的上下文

**Given** rnix-compose.yaml 格式示例
**When** 用户编写
**Then** 支持以下格式：
```yaml
version: "1.0"
intent: "PR 审查 + 代码分析 + 变更文档"
agents:
  reviewer:
    intent: "审查 PR 变更"
    skills: [pr-reviewer]
  analyst:
    intent: "分析代码质量"
    skills: [code-analyst]
    depends_on:
      reviewer: completed
```

## Story 7.2: rnix compose up 命令

As a 用户,
I want 通过 `rnix compose up` 一键启动编排定义的所有智能体,
So that 完整的多智能体工作流一条命令即可运行。

**Acceptance Criteria:**

**Given** `cmd/rnix/compose.go` 中 compose up 子命令已注册
**When** 执行 `rnix compose up`
**Then** 读取当前目录的 `rnix-compose.yaml`
**And** 按 DAG 顺序 Spawn 所有智能体
**And** 实时输出每个智能体的启动和完成状态

**Given** 指定自定义文件
**When** 执行 `rnix compose up -f my-workflow.yaml`
**Then** 使用指定文件而非默认文件

**Given** 编排中某个智能体失败
**When** 该智能体退出非零码
**Then** 依赖它的下游智能体不启动
**And** 输出明确的错误信息，标注失败的智能体和受影响的下游

**Given** 所有智能体完成
**When** 查看输出
**Then** 显示编排汇总：每个智能体的退出码、token 消耗、耗时

## Story 7.3: rnix compose down 命令

As a 用户,
I want 通过 `rnix compose down` 停止编排中所有智能体并释放资源,
So that 我可以清理中断的工作流。

**Acceptance Criteria:**

**Given** `cmd/rnix/compose.go` 中 compose down 子命令已注册
**When** 执行 `rnix compose down`
**Then** 向编排中所有运行中的智能体发送 Kill 信号
**And** 等待所有进程转为 Dead
**And** 释放所有资源（进程、上下文、文件描述符）

**Given** 部分智能体已完成，部分仍在运行
**When** 执行 `rnix compose down`
**Then** 仅终止仍在运行的智能体
**And** 输出释放汇总（终止了 N 个进程，释放了 M 个上下文）

## Story 7.4: Compose 端到端验收

As a 用户,
I want 验证完整的 Compose 编排流程：定义 → 启动 → 依赖调度 → 数据传递 → 完成,
So that 确认多智能体编排系统协同工作正常。

**Acceptance Criteria:**

**Given** 编写包含 ≥ 3 个智能体的 rnix-compose.yaml（有 DAG 依赖）
**When** 执行 `rnix compose up`
**Then** 智能体按依赖顺序执行，无依赖分支并行
**And** 前置智能体的输出正确传递给下游
**And** 3 智能体编排从 YAML 到全部完成，总耗时 ≤ 90 秒

**Given** `rnix top` 同时运行
**When** 编排执行中
**Then** 实时看到所有智能体的树状关系和状态

---
