# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.7.3] - 2026-04-12

### Fixed

- **`rnix run` 脚本进程可见性与可终止性**：脚本运行器现以 `SkipReasonLoop` 进程注册到内核进程表，在 `rnix top` 中可见并可通过 `K` 键终止；子 Agent 的 `ParentPID` 指向脚本运行器，进程树层级正确；终止信号（SIGTERM/SIGKILL）、daemon 关闭、客户端断开（Ctrl+C）三路上下文联动，执行完毕后调用 `Finish()+Reap()` 确保资源完整回收

## [0.7.2] - 2026-04-12

### Added

- **可靠性与恢复（Epic 30）**:
  - 无限推理步骤：`DefaultMaxSteps` 改为 `0`（不限步数），引入 `LoopDetector` 检测重复动作并自动暂停
  - 步骤持久化与快照检查点：异步写入 `CheckpointData`，支持从断点恢复
  - 进程挂起状态：新增 `Suspended` 状态，内核提供 `Suspend()` / `Unsuspend()` 方法
  - 恢复机制：`rnix resume <pid|uuid>` 命令，从检查点还原上下文并分配新 PID
  - 心跳存活检测：进程执行期间定期更新心跳，Dashboard 显示 stale 指示器
  - Supervisor 心跳监控：`HeartbeatMonitor` 扫描超时进程，三级恢复策略（重试步骤 → 挂起 → 通知）
  - 资源预算：`MaxTokens` / `MaxCost` 配置项，超出预算自动挂起进程
  - Dashboard 长任务可观测性：Focus Card 展示资源预算和心跳状态，`R` 键恢复挂起进程，Timeline 超 100 步自动聚合

- **上下文与 Token 管理（Epic 31）**:
  - Token 计量与 VFS 结果控制：`ToolDef` 新增 `MaxResultTokens` 字段，文件读取和 Shell 输出超限自动截断
  - 上下文压缩与恢复：支持手动和自动触发 compact，压缩后上下文可传递给新进程
  - 两阶段关闭与 IPC 消息持久化：SIGTERM 优先，超出宽限期后升级为 SIGKILL；永久进程的 IPC 消息写盘并在重启后还原

- **Skill 系统增强（Epic 32）**:
  - 新增文件操作：`edit_file`（精确字符串替换）、`glob`（路径模式匹配）、`grep`（内容搜索）
  - `ToolDef` 安全元数据：`IsReadOnly`、`IsConcurrencySafe`、`IsDestructive` 标志，写入前强制读取保护（read-before-write），mtime 追踪
  - 声明式 Section 化系统提示词组装：`PromptSection` + `SectionRegistry`，静态/动态分区缓存，Agent compact 流程自动失效
  - 延迟 Skill 加载：`agent.yaml` 新增 `deferred_skills` 字段，仅加载元数据；新增 `discover_skill` Action Type 按关键词评分按需加载

- **新 VFS 设备（Epic 33）**:
  - `/dev/web`：网页内容抓取与搜索，内置结果缓存
  - `/dev/lsp`：代码智能，支持 9 种操作（跳转定义、查找引用、悬浮文档、符号搜索等）
  - `/dev/tty`：进程与用户交互，支持提问和确认
  - `/dev/tasks`：动态任务管理，进程可创建和追踪子任务
  - `/dev/cron`：定时任务调度执行

- **Dashboard V2 事件流架构（Epic 34）**:
  - 四层信息层级：进程树 + 事件流 + 全局状态栏 + 告警条，取代原等权 8 面板布局
  - 进程树视觉增强：状态徽章、上下文用量指标、保留已退出进程层级、公共前缀折叠、高亮最活跃进程
  - 告警条与统一 Timeline：所有事件类型（推理步骤、compact、预算、spawn、exit、心跳超时）合并为单一时间轴，告警条高亮错误与异常
  - Detail Card 集成：取代 Focus Card，集成进程详情、上下文和资源信息，进程树自适应宽度
  - Debug 模式：`d` 键进入，strace 事件与推理步骤交织展示；上下文 Profile 卡（Active/Warm/Cold/Leaked）；设备延迟卡（各 VFS 设备平均延迟）
  - 编排关系可视化：Compose DAG 依赖边注解（`◄╌deps`）、Pipeline 阶段标记（`│►[i/n]`），流经进程模型、Wire 协议和磁盘持久化全链路

### Changed

- **VFS 设备提示词**：按架构决策 33（CC Baseline 原则）重写全部 6 个设备提示文件（`read_file`、`write_file`、`edit_file`、`glob`、`grep`、`shell`），恢复 CC 原版行为指引
- **AgentShell 代码拆分**：`shell/script.go`（2152 行）拆分为 5 个职责独立文件，零 API 变更
- **Dashboard 渲染框架**：引入 `renderFixedPanel` 统一面板尺寸，防止内容溢出

### Fixed

- Dashboard 历史视图翻页时界面冻结
- Dashboard 历史视图滚动位置与 IPC 读取超时不同步
- `treeCursor` 与历史视图选中进程不同步
- Agent 树意图列 CJK 字符显示宽度填充错误
- Agent 树行中意图和结果文本包含多余换行符
- `computeCtxPercent` 在 PID 未找到时错误降级至全局平均值

### Removed

- 历史视图（History View）功能：统一由扩展树视图（Expanded Tree）承接，`L` 键从扩展树视图进入 LLM Viewer

## [0.7.1] - 2026-03-28

### Fixed

- **Dashboard Prompt Viewer**:
  - Prompt Viewer Messages tab showing empty `[user]` entries for CLI driver processes — seed initial intent as first user message and skip nil-content user events from CLI drivers
  - Prompt Viewer Tools tab showing `Tools (0)` for CLI driver processes — build tool definitions from tool_call events as fallback when system event doesn't include tools
  - Prompt Viewer Tools tab showing incomplete tool info — now displays step-level tool details: name, description, path, input, result, duration from StepRecord
  - StepRecord Action field incorrectly set to `"tool_call"` instead of actual tool name
  - StepRecord missing ToolResult and ToolInput for CLI driver processes
  - Cursor CLI driver not extracting full tool args and result from tool_call events
  - `extractContentText` not handling `map[string]any` and `string` content formats for assistant/user events
  - `tool_result` content in `[]any` format not being parsed for text extraction

## [0.7.0] - 2026-03-24

### Added

- **Dashboard UX Redesign**: Complete overhaul of the dashboard interaction experience
  - View mode system with navigation restructuring
  - Default focus card view
  - Dashboard file splitting for better maintainability
- **Dashboard Process Inspection**:
  - Kernel process history and IPC query support
  - History view with LLM conversation viewer
  - Process detail panel with timeline drill-down (three-level detail)
  - Prompt view for step-level inspection
- **Dashboard Observability**:
  - StepRecord type with automatic step data logging
  - GetStepDetail IPC method for detailed process step retrieval
  - Distributed tracing integration
  - Security anomaly panel
  - Intent tree integration
- **Dashboard Evaluation & Navigation**:
  - Multi-agent evaluation view
  - Evaluation pane key bindings and navigation hints
  - Top-to-dashboard navigation
- **Process Identification Upgrade**:
  - UUID v7 for process identification
  - StepRecord path migration to UUID-based paths
  - IPC PID-to-UUID mapping
  - Dashboard PID validity checks
- **Unified Reasoning Loop**: Replaced OODA-based dual-loop with a single reasoning loop
  - Extended ActionType with unified prompt templates and planning configuration
  - Circuit breaker and VFS flag downgrade mechanism
  - Comprehensive ATDD tests
- **Configurable Immune System**: Immune system now configurable, disabled by default
- **Native ToolCalls Support**: Native tool calling with VFS device self-describing architecture
- **MaxSteps / MaxTokens Configuration**: New configuration options for step and token limits
- **Project Environment Variable Loading**: Automatic loading of project-level environment variables

### Fixed

- Evaluation pane key binding and navigation hint issues
- Signal handling continuation after timeout in `runRoot`

## [0.6.8] - 2026-03-17

### Added

- Project-level environment variable and provider configuration loading
- VFS tool call protocol injection for both reasoning modes

### Fixed

- Provider merging by name instead of wholesale slice replacement

## [0.6.6] - 2026-03-15

### Added

- **Platform-specific Daemon Handling**: Cross-platform daemon process management
- **Version Management**: ldflags-based version injection with release workflow
- **GitHub Actions CI**: Quality gate CI pipeline
- **GoReleaser**: Automated release configuration with GitHub Actions
- **Process CLI Output Enhancement**: Provider/Model details in process and CLI output
- **Configuration System Redesign**:
  - `rnix init` command with global configuration loading
  - Project-level configuration merging and module adaptation
  - Deprecated configuration file cleanup
- **Community Files**: MIT License, CONTRIBUTING.md, GitHub issue/PR templates
- **Multilingual README**: Chinese and English README files
- **Project Environment Loading**: Project-specific environment variable loading

### Changed

- Configuration file structure and documentation updates
- Enhanced Makefile with GitHub CLI command integration
- Upgraded golangci-lint version with improved coverage handling

## [0.1.0] - 2026-02-23

### Added

- **Project Initialization**: Crux AI Agent OS project structure and infrastructure
- **Process Model**: Process lifecycle state machine (Created → Running → Zombie → Dead)
- **Virtual Filesystem (VFS)**: VFS framework with device registration and FD table
- **Context Management**: Per-process conversation history (CtxAlloc/CtxWrite/BuildPrompt)
- **LLM Driver**: Claude Code CLI driver for LLM interactions
- **Kernel Reasoning Loop**: Spawn and ReasonStep execution loop
- **CLI & UI**: Command-line interface with terminal UI components
- **End-to-End Integration**: Full integration and acceptance testing
- **Skill System**:
  - Skill loader with YAML manifest parsing
  - Host filesystem driver (`/dev/fs`)
  - Shell driver (`/dev/shell`)
  - Skill injection with device permission whitelisting
  - Code-Analyst reference skill
- **LLM Driver Error Handling**: Enhanced error handling and context propagation
- **Step Output Streaming**: Real-time step output streaming
- **Strace Observability**: ConfigResolve strace event tracking
- **Daemon Model**: Background daemon with Unix domain socket communication
- **IPC Protocol**: NDJSON over Unix socket request/response protocol
- **VFS Devices**: `/dev/llm/claude`, `/dev/fs`, `/dev/shell` device implementations

[0.7.3]: https://github.com/rnixai/rnix/compare/v0.7.2...v0.7.3
[0.7.2]: https://github.com/rnixai/rnix/compare/v0.7.1...v0.7.2
[0.7.1]: https://github.com/rnixai/rnix/compare/v0.7.0...v0.7.1
[0.7.0]: https://github.com/rnixai/rnix/compare/v0.6.8...v0.7.0
[0.6.8]: https://github.com/rnixai/rnix/compare/v0.6.6...v0.6.8
[0.6.6]: https://github.com/rnixai/rnix/compare/v0.1.0...v0.6.6
[0.1.0]: https://github.com/rnixai/rnix/releases/tag/v0.1.0
