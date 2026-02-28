# User Journeys

## 旅程 1：陈明的调试顿悟（用户 A — 平台构建者，成功路径）

陈明又一次盯着终端发呆。他用 LangGraph 搭的 3 智能体代码审查系统上线两周了，其中一个智能体偶尔给出错误的审查意见——大概每 20 次出现一次。他翻了三天日志，在数千行对话记录中搜索"到底是哪一步推理出了问题"，但日志只有扁平的文本输出，没有因果链，没有上下文快照。他开始怀疑是不是该放弃这个项目。

然后他在 GitHub 上看到了 Crux。README 里的一句话抓住了他："astrace — 像 strace 一样追踪智能体的每一个 syscall"。他决定试试。

`go install` 安装 Crux。他创建了一个 `code-analyst` Agent——写 `agent.yaml` 定义模型偏好和 Skill 引用，写 `instructions.md` 注入审查策略。然后写了一个 `code-analysis` Skill 的 `SKILL.md`（遵循 Agent Skills 行业标准），定义工具依赖和分析流程。`crux "审查这段代码" --agent=code-analyst` 启动第一个智能体。跑通了。

然后他复现了那个偶现 bug。这次，他运行 `crux astrace 1`。

终端输出了完整的 syscall 链路——每一步调用了什么（Open、Read、Write、CtxWrite），传了什么参数，返回了什么，花了多久。他立刻看到：在第 7 步，智能体通过 `/dev/fs` 读取了一个错误的文件路径——它把 `src/auth/login.go` 读成了 `src/auth/logout.go`。这个错误的文件内容被写入上下文，导致后续所有推理都偏了。

三分钟。从三天到三分钟。

**陈明的新日常：** 他开始用 Crux 重建他所有的多智能体项目。他写的 `db-migrator` Skill 被其他三个项目引用。他成了 skillpkg 的早期贡献者。

**旅程揭示的能力需求：**
- `go install` 级别的零配置安装体验
- Agent 定义编写流程（agent.yaml + instructions.md）
- Skill 编写流程（SKILL.md，遵循 Agent Skills 行业标准）
- `crux spawn --agent=<name>` 单命令启动
- `astrace` syscall 追踪输出（名称、参数、返回值、耗时）
- VFS `/dev/fs` 文件读取路径透明可见
- Skill 发布到 skillpkg 的流程

---

## 旅程 2：陈明遇到 LLM 超时（用户 A — 平台构建者，异常路径）

陈明在跑一个复杂的代码分析任务。智能体需要读取一个 500 行的文件并分析性能瓶颈。中途，Claude API 超时了。

他看终端：`[agent/3] error: /dev/llm/claude: request timeout (30s)`。然后：`[kernel] PID 3 state: running → zombie (exit code: 1, reason: llm_timeout)`。

进程没有卡死。状态正确转入了 Zombie。他运行 `crux ps`，看到 PID 3 标记为 Zombie，等待 wait 回收。资源没有泄漏。

他重新运行 `crux "分析 ./src/scheduler.go"`，这次成功了。

**旅程揭示的能力需求：**
- LLM 驱动超时处理和错误上报
- 进程状态正确转移（不卡死在 Running）
- `crux ps` 进程状态查看
- Zombie 进程回收机制
- 清晰的错误信息（设备路径 + 错误原因）

---

## 旅程 3：林薇的 30 分钟工作流（用户 B — 应用开发者，成功路径）

林薇在 AI 初创公司负责产品开发。老板要一个"PR 提交后自动审查代码质量、生成变更文档"的流水线。她之前用 LangGraph 评估过——画有向图、写节点逻辑、处理状态传递，预估要两周。

同事推荐了 Crux。她打开 skillpkg：

```bash
skill install pr-reviewer code-analyst tech-writer
```

三个 Skill 装好了。她写了一个 `crux-compose.yaml`：

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
  writer:
    intent: "生成变更文档"
    skills: [tech-writer]
    depends_on:
      analyst: completed
```

`crux compose up`。20 行 YAML，三个社区 Skill，一个完整的 CI 审查流水线跑起来了。`crux top` 看到三个智能体按依赖顺序执行，token 消耗实时显示。

她把原来准备花两周写的 LangGraph 代码删了。

**旅程揭示的能力需求：**
- `skill install` 批量安装
- `crux-compose.yaml` 声明式编排
- `depends_on` 依赖管理
- `crux compose up` 一键启动
- `crux top` 实时监控（状态 + token）
- 社区 Skill 生态（可搜索、可安装、可组合）

---

## 旅程 4：林薇的调试时刻（用户 B — 应用开发者，排障路径）

林薇的 PR 审查流水线跑了一周，突然有一个 PR 的审查结果明显不对——把一个正确的函数标记为"有安全漏洞"。

她不熟悉 Crux 内核，但她知道怎么看日志。`crux log 5` 输出了 PID 5（reviewer 智能体）的推理日志，按 `[think]` / `[tool]` / `[output]` 分类。她看到 `[tool]` 部分：智能体读取了 PR diff，但 diff 内容被截断了——只读到了一半。截断后的代码看起来确实像有漏洞。

问题定位了。她调整了 Compose 配置，给 reviewer 加大了上下文预算。重跑，正常了。

**旅程揭示的能力需求：**
- `crux log <pid>` 分类推理日志
- 日志按 think/tool/output 结构化分类
- 上下文预算配置（compose.yaml 中可调）
- 无需深入内核就能排障的分层调试体验

---

## Journey Requirements Summary

| 能力领域 | 旅程来源 | MVP 必需 | Post-MVP | Phase 2 FR 映射 |
|---------|---------|---------|----------|----------------|
| 安装体验（go install） | 旅程 1 | ✓ | | |
| Agent 定义编写（agent.yaml + instructions.md） | 旅程 1 | ✓ | | |
| Skill 编写（SKILL.md，Agent Skills 行业标准） | 旅程 1 | ✓ | | |
| `crux spawn --agent=<name>` 单命令 | 旅程 1, 2 | ✓ | | |
| `astrace` syscall 追踪 | 旅程 1 | ✓ | | |
| VFS `/dev/fs` 文件读取 | 旅程 1 | ✓ | | |
| LLM 超时处理 + 进程状态正确转移 | 旅程 2 | ✓ | | |
| `crux ps` 进程查看 | 旅程 2 | ✓ | | |
| Zombie 回收 | 旅程 2 | ✓ | | |
| `skill install` 包安装 | 旅程 3 | | ✓ (Phase 2) | FR50-FR53 |
| `crux-compose.yaml` 编排 | 旅程 3 | | ✓ (Phase 2) | FR46-FR49 |
| `crux top` 实时监控 | 旅程 3 | | ✓ (Phase 2) | FR58, FR62 |
| `crux log` 分类日志 | 旅程 4 | | ✓ (Phase 2) | FR59-FR60 |
| 上下文预算配置 | 旅程 4 | | ✓ (Phase 2) | FR61 |
| skillpkg 社区 Skill 生态 | 旅程 3 | | ✓ (Phase 2) | FR50-FR53 |
| IPC 进程间通信 | 旅程 3 | | ✓ (Phase 2) | FR41-FR45 |
| MCP 服务集成 | 架构需求推导（四层能力栈设计） | | ✓ (Phase 2) | FR54-FR57 |
| Supervisor 容错 | 架构需求推导（进程可靠性设计） | | ✓ (Phase 2) | FR63-FR65 |
| AgentShell 完整语法 | 架构需求推导（AgentShell DSL 设计） | | ✓ (Phase 2) | FR66-FR68 |
| Phase 2 文档（教程 + 架构） | 生态建设需求（开发者体验） | | ✓ (Phase 2) | FR69-FR70 |
