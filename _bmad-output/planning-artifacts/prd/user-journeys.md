# User Journeys

## 旅程 1：陈明的调试顿悟（用户 A — 平台构建者，成功路径）

陈明又一次盯着终端发呆。他用 LangGraph 搭的 3 智能体代码审查系统上线两周了，其中一个智能体偶尔给出错误的审查意见——大概每 20 次出现一次。他翻了三天日志，在数千行对话记录中搜索"到底是哪一步推理出了问题"，但日志只有扁平的文本输出，没有因果链，没有上下文快照。他开始怀疑是不是该放弃这个项目。

然后他在 GitHub 上看到了 Rnix。README 里的一句话抓住了他："strace — 像 strace 一样追踪智能体的每一个 syscall"。他决定试试。

`go install` 安装 Rnix。他创建了一个 `code-analyst` Agent——写 `agent.yaml` 定义模型偏好和 Skill 引用，写 `instructions.md` 注入审查策略。然后写了一个 `code-analysis` Skill 的 `SKILL.md`（遵循 Agent Skills 行业标准），定义工具依赖和分析流程。`rnix "审查这段代码" --agent=code-analyst` 启动第一个智能体。跑通了。

然后他复现了那个偶现 bug。这次，他运行 `rnix strace 1`。

终端输出了完整的 syscall 链路——每一步调用了什么（Open、Read、Write、CtxWrite），传了什么参数，返回了什么，花了多久。他立刻看到：在第 7 步，智能体通过 `/dev/fs` 读取了一个错误的文件路径——它把 `src/auth/login.go` 读成了 `src/auth/logout.go`。这个错误的文件内容被写入上下文，导致后续所有推理都偏了。

三分钟。从三天到三分钟。

**陈明的新日常：** 他开始用 Rnix 重建他所有的多智能体项目。他写的 `db-migrator` Skill 被其他三个项目引用。他成了 skillpkg 的早期贡献者。

**旅程揭示的能力需求：**
- `go install` 级别的零配置安装体验
- Agent 定义编写流程（agent.yaml + instructions.md）
- Skill 编写流程（SKILL.md，遵循 Agent Skills 行业标准）
- `rnix spawn --agent=<name>` 单命令启动
- `strace` syscall 追踪输出（名称、参数、返回值、耗时）
- VFS `/dev/fs` 文件读取路径透明可见
- Skill 发布到 skillpkg 的流程

---

## 旅程 2：陈明遇到 LLM 超时（用户 A — 平台构建者，异常路径）

陈明在跑一个复杂的代码分析任务。智能体需要读取一个 500 行的文件并分析性能瓶颈。中途，Claude API 超时了。

他看终端：`[agent/3] error: /dev/llm/claude: request timeout (30s)`。然后：`[kernel] PID 3 state: running → zombie (exit code: 1, reason: llm_timeout)`。

进程没有卡死。状态正确转入了 Zombie。他运行 `rnix ps`，看到 PID 3 标记为 Zombie，等待 wait 回收。资源没有泄漏。

他重新运行 `rnix "分析 ./src/scheduler.go"`，这次成功了。

**旅程揭示的能力需求：**
- LLM 驱动超时处理和错误上报
- 进程状态正确转移（不卡死在 Running）
- `rnix ps` 进程状态查看
- Zombie 进程回收机制
- 清晰的错误信息（设备路径 + 错误原因）

---

## 旅程 3：林薇的 30 分钟工作流（用户 B — 应用开发者，成功路径）

林薇在 AI 初创公司负责产品开发。老板要一个"PR 提交后自动审查代码质量、生成变更文档"的流水线。她之前用 LangGraph 评估过——画有向图、写节点逻辑、处理状态传递，预估要两周。

同事推荐了 Rnix。她打开 skillpkg：

```bash
skill install pr-reviewer code-analyst tech-writer
```

三个 Skill 装好了。她写了一个 `.rnix/compose.yaml`：

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

`rnix compose up`。20 行 YAML，三个社区 Skill，一个完整的 CI 审查流水线跑起来了。`rnix top` 看到三个智能体按依赖顺序执行，token 消耗实时显示。

她把原来准备花两周写的 LangGraph 代码删了。

**旅程揭示的能力需求：**
- `skill install` 批量安装
- `.rnix/compose.yaml` 声明式编排
- `depends_on` 依赖管理
- `rnix compose up` 一键启动
- `rnix top` 实时监控（状态 + token）
- 社区 Skill 生态（可搜索、可安装、可组合）

---

## 旅程 4：林薇的调试时刻（用户 B — 应用开发者，排障路径）

林薇的 PR 审查流水线跑了一周，突然有一个 PR 的审查结果明显不对——把一个正确的函数标记为"有安全漏洞"。

她不熟悉 Rnix 内核，但她知道怎么看日志。`rnix log 5` 输出了 PID 5（reviewer 智能体）的推理日志，按 `[think]` / `[tool]` / `[output]` 分类。她看到 `[tool]` 部分：智能体读取了 PR diff，但 diff 内容被截断了——只读到了一半。截断后的代码看起来确实像有漏洞。

问题定位了。她调整了 Compose 配置，给 reviewer 加大了上下文预算。重跑，正常了。

**旅程揭示的能力需求：**
- `rnix log <pid>` 分类推理日志
- 日志按 think/tool/output 结构化分类
- 上下文预算配置（compose.yaml 中可调）
- 无需深入内核就能排障的分层调试体验

---

### 旅程 5：陈明切换本地模型降低成本（用户 A — 平台构建者，多 Provider 路径）

**背景：** 陈明在开发阶段频繁迭代，Claude API 调用成本快速累积。他在本地部署了 Ollama 运行 Llama3，希望日常开发用本地模型，关键任务切回 Claude。

**步骤：**

1. 陈明编辑 `~/.config/rnix/providers.yaml`（全局配置）或 `.rnix/providers.yaml`（项目配置），新增 ollama provider：指定 `driver: openai-compat`、`base_url: http://localhost:11434/v1`、`default_model: llama3`
2. 重启 daemon，系统自动解析配置并在 `/dev/llm/ollama` 注册新 provider
3. 修改 `code-analyst` agent 的 `agent.yaml`（位于 `.rnix/agents/code-analyst/` 或 `~/.config/rnix/agents/code-analyst/`）：`models.provider: ollama`，`models.fallback: claude`
4. 执行 `rnix "分析 main.go 的代码质量" --agent=code-analyst`，系统通过 Ollama 本地模型完成分析
5. 当 Ollama 服务意外停止时，系统检测到调用失败，自动 fallback 到 Claude，任务继续完成
6. 陈明通过 `rnix strace` 看到 provider 切换的完整轨迹：首次调用 `/dev/llm/ollama` 失败 → fallback 到 `/dev/llm/claude` 成功

**结果：** 日常开发成本降为零（本地模型），关键任务通过 fallback 保证质量，切换过程对智能体透明。

**覆盖能力：** FR141（配置驱动注册）、FR142（HTTP API 驱动）、FR143（agent provider 指定）、FR144（fallback 降级）、FR145（CLI provider 覆盖）、FR146（API Key 管理）、NFR47-49（多 provider 性能）

---

### 旅程 6：陈明通过 rnix serve 让外部工具使用 LLM（用户 A — 平台构建者，网关路径）

**背景：** 陈明习惯在不同场景用不同工具——用 Aider 做代码重构、用 Open WebUI 做知识问答、用自研脚本做批量分析。这些工具都支持 OpenAI API，但他的 Cursor Pro 配额和本地 Ollama 只能在各自的生态里用。他希望有一个统一入口。

**步骤：**

1. 陈明确认 providers 配置（`~/.config/rnix/providers.yaml` 或 `.rnix/providers.yaml`）已配置好 cursor、ollama、claude 三个 provider
2. 启动网关：`rnix serve --port 8080`，终端显示 `Serving 3 providers on http://127.0.0.1:8080`
3. 在 Aider 中配置：`--openai-api-base http://localhost:8080/v1`，model 设为 `cursor`——Aider 通过 Rnix 网关调用 Cursor 的 LLM 能力完成代码重构
4. 在 Open WebUI 中配置 API 端点为 `http://localhost:8080/v1`，通过 `/v1/models` 自动发现所有可用 provider 和模型
5. 在自研 Python 脚本中使用标准 `openai` 库：`client = OpenAI(base_url="http://localhost:8080/v1", api_key="unused")`，model 指定 `ollama:llama3` 做批量分析
6. 陈明通过 `rnix top` 看到所有通过网关发起的 LLM 调用的 token 消耗统计

**结果：** 一个端口统一所有 LLM 访问。Cursor Pro 配额不再被锁在 IDE 里，本地模型不再需要每个工具单独配置。任何支持 OpenAI API 的工具都可以即插即用。

**覆盖能力：** FR147（rnix serve 启动）、FR148（/v1/chat/completions 路由）、FR149（/v1/models 发现）、FR150（SSE 流式）、FR151（provider:model 复合路由）、FR152（共享 daemon 配置）、NFR50-52（网关性能与安全）

---

## Journey Requirements Summary

| 能力领域 | 旅程来源 | MVP 必需 | Post-MVP | Phase 2 FR 映射 |
|---------|---------|---------|----------|----------------|
| 安装体验（go install） | 旅程 1 | ✓ | | |
| Agent 定义编写（agent.yaml + instructions.md） | 旅程 1 | ✓ | | |
| Skill 编写（SKILL.md，Agent Skills 行业标准） | 旅程 1 | ✓ | | |
| `rnix spawn --agent=<name>` 单命令 | 旅程 1, 2 | ✓ | | |
| `strace` syscall 追踪 | 旅程 1 | ✓ | | |
| VFS `/dev/fs` 文件读取 | 旅程 1 | ✓ | | |
| LLM 超时处理 + 进程状态正确转移 | 旅程 2 | ✓ | | |
| `rnix ps` 进程查看 | 旅程 2 | ✓ | | |
| Zombie 回收 | 旅程 2 | ✓ | | |
| `skill install` 包安装 | 旅程 3 | | ✓ (Phase 2) | FR50-FR53 |
| `.rnix/compose.yaml` 编排 | 旅程 3 | | ✓ (Phase 2) | FR46-FR49 |
| `rnix top` 实时监控 | 旅程 3 | | ✓ (Phase 2) | FR58, FR62 |
| `rnix log` 分类日志 | 旅程 4 | | ✓ (Phase 2) | FR59-FR60 |
| 上下文预算配置 | 旅程 4 | | ✓ (Phase 2) | FR61 |
| skillpkg 社区 Skill 生态 | 旅程 3 | | ✓ (Phase 2) | FR50-FR53 |
| IPC 进程间通信 | 旅程 3 | | ✓ (Phase 2) | FR41-FR45 |
| MCP 服务集成 | 架构需求推导（四层能力栈设计） | | ✓ (Phase 2) | FR54-FR57 |
| Supervisor 容错 | 架构需求推导（进程可靠性设计） | | ✓ (Phase 2) | FR63-FR65 |
| AgentShell 完整语法 | 架构需求推导（AgentShell DSL 设计） | | ✓ (Phase 2) | FR66-FR68 |
| Phase 2 文档（教程 + 架构） | 生态建设需求（开发者体验） | | ✓ (Phase 2) | FR69-FR70 |
| 多 Provider 支持（provider 注册 + fallback） | 旅程 5 | | ✓ (Phase 2) | FR141-FR146 |
| LLM 网关服务（rnix serve OpenAI 兼容 API） | 旅程 6 | | ✓ (Phase 2) | FR147-FR152 |
| 配置系统（双层目录 + rnix init + 迁移） | 旅程 1, 3, 5 | | ✓ (Phase 2) | FR153-FR164 |
