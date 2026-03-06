# Story 12.2: 架构文档（Architecture Documentation）

Status: done

## Story

As a 贡献者,
I want 阅读架构文档理解 Rnix 的内部设计,
So that 我可以参与内核开发和 Skill 生态贡献。

## Acceptance Criteria

1. **AC1: 微内核设计章节**
   - Given 架构文档已编写
   - When 阅读微内核设计章节
   - Then 包含 Kernel 接口组合设计、分类子接口职责、扩展路径的设计决策和数据流说明

2. **AC2: 进程模型章节**
   - Given 架构文档已编写
   - When 阅读进程模型章节
   - Then 包含 Process 结构体设计、状态机转移规则、PID 分配策略、goroutine 生命周期管理（FR70）

3. **AC3: 驱动层章节**
   - Given 架构文档已编写
   - When 阅读驱动层章节
   - Then 包含 LLMDriver 接口、VFS 设备注册、MCP 挂载机制

4. **AC4: 上下文管理章节**
   - Given 架构文档已编写
   - When 阅读上下文管理章节
   - Then 包含上下文分配/读写/释放、prompt 组装、token 预算管理

## Tasks / Subtasks

- [ ] Task 1: 创建架构文档文件和整体框架 (AC: #1, #2, #3, #4)
  - [ ] 1.1 创建 `docs/architecture.md` 文件
  - [ ] 1.2 编写文档概述（面向贡献者、前置条件、文档结构导航）
  - [ ] 1.3 建立四个核心章节的骨架

- [ ] Task 2: 编写微内核设计章节 (AC: #1)
  - [ ] 2.1 编写设计哲学：OS 隐喻与接口组合模式
  - [ ] 2.2 编写 KernelImpl 结构与组合子接口（ProcessManager、MountManager、IPCManager、SignalManager、ProcGroupManager、SupervisorManager）
  - [ ] 2.3 编写各子接口的职责和方法签名表
  - [ ] 2.4 编写 KernelCallbacks 回调机制
  - [ ] 2.5 编写扩展路径说明（如何添加新 syscall / 新子接口）
  - [ ] 2.6 编写数据流图：spawn → reasonStep → VFS 读写 → 完成

- [ ] Task 3: 编写进程模型章节 (AC: #2)
  - [ ] 3.1 编写 Process 结构体设计（核心字段、同步原语、通道设计）
  - [ ] 3.2 编写状态机转移规则（Created → Running → Zombie → Dead，附单向约束说明）
  - [ ] 3.3 编写 PID 分配策略（全局递增、不回收）
  - [ ] 3.4 编写 goroutine 生命周期管理（推理 goroutine、context.Cancel、wg.Wait）
  - [ ] 3.5 编写资源释放顺序（reapProcess 12 步序列）
  - [ ] 3.6 编写三级并发模型（Process/Thread/Coroutine）
  - [ ] 3.7 编写进程组与信号系统

- [ ] Task 4: 编写驱动层章节 (AC: #3)
  - [ ] 4.1 编写 VFS 设备注册机制（DeviceRegistry、VFSFileFactory、路径解析）
  - [ ] 4.2 编写 LLMDriver 接口（Call/Stream/Info 方法、Request/Response 结构）
  - [ ] 4.3 编写各设备驱动概述（/dev/llm/claude、/dev/fs、/dev/shell、/proc）
  - [ ] 4.4 编写 MCP 挂载机制（MCPTransport 接口、Mount/Unmount 流程、VFS 子路径映射）
  - [ ] 4.5 编写 Agent 自动挂载与卸载生命周期

- [ ] Task 5: 编写上下文管理章节 (AC: #4)
  - [ ] 5.1 编写 Context 结构体设计（ID、Messages、SystemPrompt、MaxSize）
  - [ ] 5.2 编写 Manager 方法概述（CtxAlloc/CtxFree/SetSystemPrompt/AppendMessage/BuildPrompt）
  - [ ] 5.3 编写 Prompt 组装流程（系统提示词 + Skill 注入 + 消息历史 + 工具结果）
  - [ ] 5.4 编写 Token 预算管理（预算来源优先级、budget_exceeded 退出码、reasonStep 中的执行逻辑）
  - [ ] 5.5 编写上下文生命周期与进程生命周期的绑定关系

- [ ] Task 6: 交叉引用与校验 (AC: #1, #2, #3, #4)
  - [ ] 6.1 添加指向参考手册（reference.md）各章节的精确链接
  - [ ] 6.2 添加指向概念文档（concepts.md）的相关链接
  - [ ] 6.3 添加指向教程（tutorials/）的实战链接
  - [ ] 6.4 校验所有接口签名、结构体字段名、VFS 路径与代码实现一致
  - [ ] 6.5 校验所有设计决策描述与 `_bmad-output/planning-artifacts/architecture/` 中的原始 ADR 一致
  - [ ] 6.6 确认所有文档使用简体中文书写

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6 (high-thinking)

### Debug Log References

无调试需要——文档 Story，所有测试一次通过。

### Completion Notes List

- 创建了 `docs/architecture.md`（约 450 行架构文档）
- 章节 1（微内核设计）：KernelImpl 接口组合、6 个子接口职责表、KernelCallbacks 回调机制、完整数据流图、扩展路径指南
- 章节 2（进程模型）：Process 结构体完整字段分组、状态机转移规则、PID 分配策略、goroutine 生命周期管理、reapProcess 12 步释放序列、三级并发模型、信号系统
- 章节 3（驱动层）：VFS 设备注册机制、路径解析策略、已注册设备列表、LLMDriver 接口、MCP 挂载机制（含 VFS 子路径映射表）、Agent 自动挂载生命周期
- 章节 4（上下文管理）：Context 结构体、Manager 方法分类表、Prompt 组装流程、Token 预算管理（优先级 + 执行逻辑 + 退出码）、上下文与进程绑定关系
- 13 个文档验证测试全部通过
- 回归测试：Story 12-1 的 12 个测试全部通过

### Senior Developer Review (AI)

**Review Date:** 2026-03-03
**Review Outcome:** Approve (with fixes applied)
**Findings:** 2 HIGH, 3 MEDIUM, 2 LOW — HIGH and MEDIUM all fixed

**Action Items (all resolved):**
- [x] [HIGH] ProcGroupManager 子接口表缺少 GetProcGroup 方法
- [x] [HIGH] Process 结构体缺少 Exit 和 CreatedAt 字段
- [x] [MEDIUM] validTransitions 代码片段缺少 types. 前缀
- [x] [MEDIUM] MountManager 描述未区分接口定义和结构体实现
- [x] [MEDIUM] Thread 结构体描述缺少 Err 字段和内部同步字段
- [ ] [LOW] ProcessManager Kill 参数类型未完全限定——文档已使用简写风格，与全文一致
- [ ] [LOW] Process map 类型使用未限定名——文档已使用简写风格，与全文一致

### Change Log

- 2026-03-03: 完成 Story 12-2 全部 6 个 Task，Status: review
- 2026-03-03: Code Review 发现 7 个问题（2H+3M+2L），HIGH/MEDIUM 全部修复，Status: done

### File List

- docs/architecture.md (新增)
- docs/docs_test.go (修改——添加 13 个架构文档验证测试)

## Dev Notes

### 架构与实现约束

**文档类型：** 纯 Markdown 文档，不涉及 Go 代码修改（测试除外）。架构文档存放在 `docs/architecture.md`。

**输出语言：** 简体中文（与项目配置 `document_output_language: Chinese` 一致）。

**定位区分：** 架构文档面向**贡献者**（想深入理解和参与开发的人），不同于：
- `docs/concepts.md` — 面向**用户**的心智模型建立
- `docs/reference.md` — 面向**开发者**的精确 API 查阅
- `docs/tutorials/` — 面向**开发者**的手把手实战

架构文档的读者已经理解概念，需要的是**设计决策**、**为什么这样设计**、**内部数据流**和**如何扩展**。

### 关键技术参考

**微内核设计（章节一需要精确引用）：**
- KernelImpl 采用接口组合模式，6 个分类子接口：ProcessManager（Spawn/Kill/Wait）、MountManager（Mount/Unmount）、IPCManager（Send/Recv/Pipe）、SignalManager（Signal/SigBlock/SigUnblock）、ProcGroupManager（JoinGroup/LeaveGroup/SignalGroup）、SupervisorManager（SpawnSupervisor）
- KernelCallbacks：OnSpawn、OnStep、OnComplete、OnError
- 参考代码：`kernel/kernel.go`、`kernel/spawn.go`、`kernel/reason.go`
- 参考 ADR：`_bmad-output/planning-artifacts/architecture/core-architectural-decisions.md` Decision 1

**进程模型（章节二需要精确引用）：**
- Process 结构体：PID/PPID/State/Intent/Skills/Children/FDTable/DebugChan/LogChan/Done/CreatedAt/Exit/CtxID/Result/TokensUsed/ContextBudget/AllowedDevices/MCPMounts/groups/sigHandlers/blockedSignals/pendingSignals/resumeCh/threads/coroutines/mu/cancel/ctx/wg/reapOnce
- 状态机：Created → Running → Zombie → Dead（严格单向）
- PID：atomic.Uint64 全局递增，不回收
- 资源释放（reapProcess）：12 步序列
- 三级并发：Process（独立 goroutine + Context）、Thread（共享 Context）、Coroutine（协作式让出）
- 参考代码：`kernel/process.go`、`kernel/reap.go`、`kernel/thread.go`、`kernel/coroutine.go`
- 参考 ADR：`_bmad-output/planning-artifacts/architecture/core-architectural-decisions.md` Decision 2

**驱动层（章节三需要精确引用）：**
- DeviceRegistry：Register(path, factory)/Unregister/Open/Stat
- VFSFileFactory：`func(subpath string, flags OpenFlag) (VFSFile, error)`
- VFSFile：Read/Write/Close/Stat
- 设备列表：/dev/llm/claude、/dev/fs、/dev/shell、/proc
- LLMDriver 接口：Call(ctx, req) (*LLMResponse, error)、Stream(ctx, req) (<-chan StreamEvent, error)、Info() DriverInfo
- MCP：MCPTransport（Connect/Call/Close/Ping）、MountManager（Mount/Unmount/UnmountAll）、VFS 子路径映射（/tools、/tools/{name}、/resources、/resources/{uri}）
- 参考代码：`vfs/dev.go`、`vfs/vfs.go`、`vfs/mount.go`、`vfs/mcp.go`、`drivers/llm/driver.go`、`drivers/fs/driver.go`、`drivers/shell/driver.go`
- 参考 ADR：Decision 3, Decision 4

**上下文管理（章节四需要精确引用）：**
- Context 结构体：ID、SystemPrompt、Messages、MaxSize、mu
- Message：Role(system/user/assistant/tool_result)、Content、ToolCallID
- Manager 方法：CtxAlloc(size)、CtxFree(cid)、SetSystemPrompt、AppendMessage、AppendToolResult、BuildPrompt、CtxWrite、CtxRead、GetContextSummary
- Token 预算：SpawnOpts.ContextBudget > AgentManifest.ContextBudget > 0（无限制）
- 执行逻辑：reasonStep 中每次 LLM 调用后累加 TokensUsed，超预算则 ExitStatus{Code: 2, Reason: "budget_exceeded"}
- 参考代码：`context/context.go`、`context/manager.go`、`kernel/reason.go`

### CLI 命令参考

与 Story 12-1 相同，参见 `docs/reference.md` §4。

### 项目结构参考

```
kernel/
├── kernel.go          # KernelImpl、KernelCallbacks、子接口定义
├── process.go         # Process 结构体、状态机
├── spawn.go           # Spawn 实现
├── reason.go          # reasonStep 推理循环
├── reap.go            # reapProcess 资源释放
├── signal.go          # 信号系统
├── thread.go          # Thread 模型
├── coroutine.go       # Coroutine 模型
├── supervisor.go      # Supervisor 树
└── init.go            # Init 引导

vfs/
├── vfs.go             # VFS 主结构
├── dev.go             # DeviceRegistry
├── mount.go           # MountManager
├── mcp.go             # MCP VFS 文件
├── proc.go            # ProcFS
└── types.go           # VFSFile、FileStat 等

context/
├── context.go         # Context 结构体
└── manager.go         # Manager 方法

drivers/
├── llm/driver.go      # LLMDriver 接口与 Claude 实现
├── fs/driver.go       # 文件系统驱动
├── shell/driver.go    # Shell 驱动
└── mcp/transport.go   # MCP transport

docs/
├── concepts.md        # 核心概念（Phase 1）
├── quick-start.md     # 快速上手（Phase 1）
├── reference.md       # 参考手册（Phase 1，1544 行）
├── tutorials/         # 教程（Story 12-1）
└── architecture.md    # 架构文档（本 Story 新增）
```

### 前序 Story 经验

**Phase 1 文档经验（Epic 5）：**
- Story 5.1（概念文档）、5.2（快速上手）、5.3（参考手册）均为文档 Story
- 关键学习：接口签名必须与实际代码完全一致，VFS 路径必须精确匹配
- 中文技术写作保持一致的术语翻译

**Story 12-1（教程文档）经验：**
- Code Review 发现 strace 输出格式、rnix ps 列名等细节与代码不一致——本 Story 需特别注意接口签名精确性
- 文档验证测试策略有效：通过 Go 测试文件验证文档内容

### ADR 原始文档

架构决策记录（ADR）位于 `_bmad-output/planning-artifacts/architecture/`，共 7 个文件：
- `core-architectural-decisions.md` — 7 个核心决策
- `project-structure-boundaries.md` — 项目结构与边界
- `implementation-patterns-consistency-rules.md` — 实现模式
- 其他 4 个文件提供上下文

架构文档应从贡献者视角**重新组织**这些信息，而非简单复制 ADR。

### Git 最近提交

最近 5 个提交：
1. Story 12-1 完成（教程文档）
2. Epic 11 完成（AgentShell 高级语法）
3. Epic 10 完成（监控、Supervisor 与运维）
4. Epic 9 完成（MCP 集成）

### References

- [Source: _bmad-output/planning-artifacts/epics/epic-12-phase-2-文档phase-2-documentation.md]
- [Source: _bmad-output/planning-artifacts/architecture/*.md — 架构决策记录]
- [Source: kernel/kernel.go — KernelImpl 和子接口]
- [Source: kernel/process.go — Process 结构体和状态机]
- [Source: kernel/reap.go — reapProcess 资源释放]
- [Source: kernel/reason.go — reasonStep 推理循环]
- [Source: kernel/thread.go — Thread 模型]
- [Source: kernel/coroutine.go — Coroutine 模型]
- [Source: vfs/dev.go — DeviceRegistry]
- [Source: vfs/mount.go — MountManager]
- [Source: vfs/mcp.go — MCP VFS 文件]
- [Source: context/context.go — Context 结构体]
- [Source: context/manager.go — Manager 方法]
- [Source: drivers/llm/driver.go — LLMDriver 接口]
- [Source: docs/reference.md — 参考手册]
- [Source: docs/concepts.md — 概念文档]
