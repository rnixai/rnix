# Epic 17: 可视化调试面板

用户可以在统一的全屏 TUI 面板中同时查看智能体树、追踪时间线和上下文热力图，窗格间联动交互，支持离线回放。

## Story 17.1: Dashboard 框架与智能体树窗格

As a 平台构建者,
I want 通过 `rnix dashboard` 启动全屏 TUI 面板，在智能体树窗格中实时查看所有进程的状态,
So that 我可以纵览整个系统的运行状态。

**Acceptance Criteria:**

**Given** 系统中有运行中的智能体
**When** 用户执行 `rnix dashboard`
**Then** 启动全屏 bubbletea TUI 应用，默认显示多窗格视图

**Given** 智能体树窗格
**When** 进程状态发生变化
**Then** 实时显示进程父子关系、状态（Running/Zombie/Dead）、当前执行阶段和 token 消耗
**And** TUI 刷新间隔 <= 500ms，10 并发进程 CPU <= 10%（NFR36）
**And** 支持 >= 50 进程节点无卡顿（NFR37）

## Story 17.2: 追踪时间线窗格

As a 平台构建者,
I want 在时间线窗格中以时间轴展示智能体的 syscall 事件流,
So that 我可以直观地看到智能体的执行时序和关键事件。

**Acceptance Criteria:**

**Given** 选中一个智能体节点
**When** 时间线窗格渲染
**Then** 水平时间轴展示该智能体的 syscall 事件流，按类别着色（LLM=蓝/Tool=绿/IPC=紫/VFS=黄/Error=红）

**Given** 时间线窗格
**When** 用户操作缩放或滚动
**Then** 时间范围平滑调整，支持按类别过滤（LLM/Tool/IPC/VFS 独立显示/隐藏）

## Story 17.3: 上下文热力图窗格

As a 平台构建者,
I want 在热力图窗格中可视化智能体的上下文组成,
So that 我可以直观了解 token 分布和活跃度。

**Acceptance Criteria:**

**Given** 选中一个智能体节点
**When** 热力图窗格渲染
**Then** 按来源着色展示上下文组成（system prompt / skill 指令 / 工具结果 / 对话历史），面积正比 token 占比，深浅表示活跃度

**Given** 热力图中某个区域
**When** 用户选中该区域
**Then** 显示具体 token 数、占比百分比和内容摘要

## Story 17.4: 窗格联动与进程操作

As a 平台构建者,
I want 在 dashboard 中点击智能体节点自动联动切换其他窗格，并可直接对进程执行操作,
So that 我可以高效地在多个视图间切换并快速执行操作。

**Acceptance Criteria:**

**Given** 用户在智能体树中点击一个节点
**When** 节点被选中
**Then** 时间线窗格和热力图窗格自动切换到该智能体的数据

**Given** 用户选中一个进程
**When** 用户按快捷键（k=kill / a=attach gdb / l=view log / r=start recording）
**Then** 对应操作被执行，界面更新反映操作结果
**And** 敏感操作（kill）需确认

## Story 17.5: 离线回放分析

As a 平台构建者,
I want dashboard 支持加载录制文件进行离线回放分析,
So that 我可以在智能体完成后回顾和分析其历史执行。

**Acceptance Criteria:**

**Given** 存在持久化的录制文件
**When** 用户执行 `rnix dashboard --load <record-dir>`
**Then** dashboard 从录制文件加载历史数据，所有窗格展示录制内容

**Given** 离线回放模式
**When** 用户操作时间轴
**Then** 支持播放/暂停、速度调节、时间轴拖拽跳转和逐帧前进/后退
