# Success Criteria

## User Success

**用户 A（平台构建者）：**

| 指标 | 衡量方式 | 目标 |
|------|---------|------|
| 调试效率 | 定位多智能体 bug 的时间 | 从"天级"降至"分钟级" |
| 能力复用率 | 单个 Skill 被引用的项目数 | ≥ 3 个项目 |
| 上手门槛 | 安装到跑通第一个 demo | ≤ 15 分钟 |
| 顿悟时刻 | `strace` 首次定位到真实问题 | 用户确认"这比翻日志快得多" |

**用户 B（应用开发者）：**

| 指标 | 衡量方式 | 目标 |
|------|---------|------|
| 构建效率 | 完成多智能体工作流的代码量 | 比现有框架减少 90% |
| Skill 生态 | skillpkg 可用 Skill 数量 | 持续增长 |
| 上手门槛 | 安装到跑通 compose 模板 | ≤ 30 分钟 |

## Business Success

**北极星指标：GitHub Stars**（早期阶段，Stars 是最直接的社区认可和传播信号）

| 时间节点 | 目标 |
|---------|------|
| Phase 1 完成 | 自举成功——Rnix 用自身 syscall 层分析自身源码，**能正确识别代码中真实存在的问题** |
| 6 个月 | 首次公开发布，README + demo 完整，接受外部 contributor |
| 12 个月 | Stars 作为社区认可度核心指标，支撑指标（demo 成功率、Skill 数量、Contributor 数量、外部引用）同步跟踪 |

## Technical Success

**功能性验收（MVP）：**

| 检查项 | 通过条件 |
|--------|---------|
| 进程生命周期 | spawn → running → zombie → dead 完整流转 |
| VFS 读写 | 通过 `/dev/fs` 读取宿主文件系统文件 |
| LLM 调用 | 通过 `/dev/llm/claude` 完成推理 |
| Skill 加载 | `code-analyst` Agent 加载 agent.yaml + 引用的 Skill SKILL.md 正确注入 system prompt |
| reasonStep 循环 | tool_call → 执行 → 追加结果 → 继续推理 → text → 完成 |
| strace 追踪 | `rnix strace 1` 输出完整 syscall 链路（名称、耗时、token）|
| 自举验证 | 用 Rnix 分析 Rnix 自身源码，识别出真实存在的代码问题 |

**可靠性验收（MVP）：**

| 检查项 | 通过条件 |
|--------|---------|
| 基本稳定性 | 连续 20 次 spawn→完成路径，成功率 ≥ 95% |
| 进程状态一致性 | LLM API 超时/错误时，进程正确转入 Zombie 状态而非卡死 |
| 资源回收 | 进程退出后，goroutine 和 context 内存正确释放，无泄漏 |

## Measurable Outcomes (Phase 1)

| 维度 | 核心可测量结果 |
|------|--------------|
| 自举 | Rnix 分析自身源码 → 输出中包含至少 1 个可验证的真实代码问题 |
| 调试差异化 | `strace` 输出的 syscall 链路能回溯到导致错误结果的具体步骤 |
| 端到端延迟 | 单智能体 spawn→完成（含 LLM 调用），≤ 30 秒 |

## Phase 2 Success Criteria

**用户 B（应用开发者）：**

| 指标 | 衡量方式 | 目标 |
|------|---------|------|
| 构建效率 | 完成多智能体工作流的代码量 | 20 行 YAML 替代 2000+ 行硬编码 |
| 上手门槛 | 安装到跑通 compose 模板 | ≤ 30 分钟 |
| 排障效率 | 通过 `rnix log` 定位多智能体问题 | 无需深入内核即可完成 |
| Skill 复用 | 社区 Skill 安装后直接可用 | 零修改引用 |

**生态指标：**

| 指标 | 衡量方式 | 目标 |
|------|---------|------|
| Skill 生态 | skillpkg 社区仓库可用 Skill 数量 | ≥ 10 个 |
| 贡献者增长 | 外部 Skill 贡献者数量 | ≥ 3 人 |
| MCP 集成 | 可用 MCP 服务适配器数量 | ≥ 3 个 |

**Phase 2 技术验收：**

| 检查项 | 通过条件 |
|--------|---------|
| IPC 通信 | Send/Recv/Pipe 三个 syscall 端到端跑通，两个智能体通过管道传递数据 |
| Compose 编排 | `rnix compose up` 按 DAG 依赖顺序启动 ≥ 3 个智能体并全部完成 |
| skillpkg 安装 | `skill install <name>` 从远程仓库下载并注册 Skill，`skill search` 返回结果 |
| MCP 挂载 | `/mnt/mcp/` 路径挂载至少 1 个 MCP 服务器，智能体可通过 VFS 访问其工具 |
| Supervisor 容错 | 子智能体异常退出后，Supervisor 在 5 秒内按策略自动重启 |
| rnix top | 实时显示 ≥ 3 个并发智能体的状态和 token 消耗 |
| rnix log | 输出按 think/tool/output 分类，支持 --filter 过滤 |
| 四层能力栈 | Agent → Skill → MCP → Device 端到端运行，各层职责分离验证通过 |
| AgentShell 管道 | `spawn "A" \| spawn "B"` 管道语法执行成功，前一个智能体输出正确注入后一个上下文 |
| AgentShell 脚本 | `if-else` + `on-error` 最小控制结构在多行脚本中正确执行 |
| Phase 2 教程 | 三个核心教程（编写 Skill、调试 bug、多智能体工作流）各含完整可运行示例 |
| Phase 2 架构文档 | 四个核心模块（微内核、进程模型、驱动层、上下文管理）各含设计决策和数据流说明 |

## Measurable Outcomes (Phase 2)

| 维度 | 核心可测量结果 |
|------|--------------|
| 编排效率 | 3 智能体 Compose 工作流从 YAML 到全部完成，总耗时 ≤ 90 秒 |
| IPC 通信 | 管道连接的两个智能体，数据传递延迟 ≤ 50ms |
| 容错恢复 | 子智能体崩溃后 Supervisor 重启并恢复执行，用户无感知 |
| 生态可用性 | 社区 Skill install → Agent 引用 → spawn 执行，全流程 ≤ 5 分钟 |
