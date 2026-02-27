# Epic 12: Phase 2 文档（Phase 2 Documentation）

三个核心教程覆盖开发者最关键的场景，四模块架构文档为贡献者提供深入理解——生态建设的文档基础。

## Story 12.1: 教程文档

As a 开发者,
I want 阅读教程文档学会编写 Skill、调试 bug 和组合多智能体工作流,
So that 我可以在 Crux 上构建自己的应用。

**Acceptance Criteria:**

**Given** 教程文档已编写
**When** 阅读"编写第一个 Skill"教程
**Then** 包含从创建 SKILL.md 到 Agent 引用到 spawn 执行的完整流程
**And** 包含完整可运行示例（FR69）

**Given** 教程文档已编写
**When** 阅读"调试第一个 bug"教程
**Then** 包含故意引入 bug → astrace 定位 → 修复 → 验证的完整流程
**And** 包含完整可运行示例

**Given** 教程文档已编写
**When** 阅读"组合多智能体工作流"教程
**Then** 包含编写 crux-compose.yaml → compose up → crux top 监控 → 查看结果的完整流程
**And** 包含完整可运行示例

## Story 12.2: 架构文档

As a 贡献者,
I want 阅读架构文档理解 Crux 的内部设计,
So that 我可以参与内核开发和 Skill 生态贡献。

**Acceptance Criteria:**

**Given** 架构文档已编写
**When** 阅读微内核设计章节
**Then** 包含 Kernel 接口组合设计、分类子接口职责、扩展路径的设计决策和数据流说明

**Given** 架构文档已编写
**When** 阅读进程模型章节
**Then** 包含 Process 结构体设计、状态机转移规则、PID 分配策略、goroutine 生命周期管理（FR70）

**Given** 架构文档已编写
**When** 阅读驱动层章节
**Then** 包含 LLMDriver 接口、VFS 设备注册、MCP 挂载机制

**Given** 架构文档已编写
**When** 阅读上下文管理章节
**Then** 包含上下文分配/读写/释放、prompt 组装、token 预算管理
