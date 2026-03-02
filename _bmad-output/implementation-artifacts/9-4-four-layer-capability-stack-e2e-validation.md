# Story 9.4: 四层能力栈端到端验证

Status: done

## Story

As a 用户,
I want 验证 Agent → Skill → MCP → Device 四层能力栈端到端工作,
so that 确认各层职责分离且协同正确。

## Acceptance Criteria

1. **四层能力栈端到端流程** — Given 配置了包含 Skill 和 MCP 引用的 Agent, When Spawn 并执行任务, Then Agent 层提供身份和策略, And Skill 层提供程序性知识和工具权限, And MCP 层提供外部服务集成, And Device 层提供原生 I/O（`/dev/`）

2. **astrace 四层调用链路可观测** — Given `crux astrace` 追踪该进程, When 查看 syscall 链路, Then 可以清晰看到四层的调用边界和数据流向（FR57）

## Tasks / Subtasks

- [x] Task 1: 创建端到端测试 Agent 和 Skill fixtures（AC: #1）
  - [x] 1.1 在 `kernel/testdata/e2e-agent/agent.yaml` 中定义测试用 Agent，包含 `skills: ["e2e-skill"]` 和 `mcp: ["mock-server"]`
  - [x] 1.2 在 `kernel/testdata/e2e-agent/instructions.md` 中编写 Agent 角色指令（简单测试用）
  - [x] 1.3 在 `kernel/testdata/e2e-skill/SKILL.md` 中定义测试用 Skill，`allowed-tools` 包含 `/dev/llm/claude /dev/shell /dev/fs`
  - [x] 1.4 验证 Agent YAML 包含四层关键字段：identity（agent.yaml）、skills 引用、mcp 引用

- [x] Task 2: 实现四层能力栈集成测试（AC: #1）
  - [x] 2.1 在 `kernel/e2e_test.go` 中创建 `TestFourLayerCapabilityStack` 测试函数
  - [x] 2.2 构造包含四层完整配置的 `AgentInfo`：Agent manifest + Skills + MCPConfigs
  - [x] 2.3 使用 mock LLM driver 模拟多步 reasonStep：首步返回 tool_call 指向 `/dev/shell`（Device 层验证），次步返回 tool_call 指向 `/mnt/mcp/{pid}-mock-server/tools/query`（MCP 层验证），末步返回 text（任务完成）
  - [x] 2.4 验证 Agent 层：Spawn 成功，PID > 0，进程存在于进程表（注：Process 不直接存储 Agent 引用，通过 Spawn 参数间接验证）
  - [x] 2.5 验证 Skill 层：`proc.AllowedDevices` 包含 Skill 定义的 `/dev/` 设备路径，工具权限白名单正确聚合
  - [x] 2.6 验证 MCP 层：`proc.MCPMounts` 包含 `/mnt/mcp/{pid}-mock-server`，`proc.AllowedDevices` 同时包含 MCP 挂载路径，MCP 工具调用通过 VFS Open/Write/Read/Close 正确执行
  - [x] 2.7 验证 Device 层：`/dev/shell` 工具调用通过 VFS 正确执行，权限检查通过
  - [x] 2.8 验证进程完成后 MCP 自动 Unmount（finishProcess 清理逻辑）

- [x] Task 3: 实现 astrace 四层调用链路验证测试（AC: #2）
  - [x] 3.1 在 `kernel/e2e_test.go` 中创建 `TestFourLayerAstraceVisibility` 测试函数
  - [x] 3.2 Spawn 进程时启用 DebugChan（`proc.DebugChan = make(chan types.SyscallEvent, 256)`）
  - [x] 3.3 收集所有 SyscallEvent，验证事件序列包含四层调用边界：
    - Agent 层：`Spawn` 事件（含 `agent` 和 `skills` args）
    - Skill 层：AllowedDevices 在 Spawn 时设置（通过 Spawn 事件 `allowed_devices` arg 验证）
    - MCP 层：`Mount` 事件（含 `path` 和 `auto: true`）+ MCP 工具的 `Open`/`Write`/`Read`/`Close` 事件
    - Device 层：原生设备的 `Open`/`Write`/`Read`/`Close` 事件（如 `/dev/shell`）
  - [x] 3.4 验证事件时间顺序正确：CtxAlloc → Open(/dev/llm) → Mount(MCP) → Spawn → ReasonStep（通过 firstIdx 映射逐对验证）
  - [x] 3.5 验证每个 SyscallEvent 包含完整字段：PID、Syscall、Args、Result、Duration

- [x] Task 4: 验证 AllowedDevices 四层聚合正确性（AC: #1）
  - [x] 4.1 在 `kernel/e2e_test.go` 中创建 `TestAllowedDevicesAggregation` 测试函数
  - [x] 4.2 验证 AllowedDevices 同时包含 Skill 来源的 `/dev/` 路径和 MCP 来源的 `/mnt/mcp/` 路径
  - [x] 4.3 验证 reasonStep 权限检查对 Skill 设备路径通过（`/dev/shell`、`/dev/fs`）
  - [x] 4.4 验证 reasonStep 权限检查对 MCP 子路径通过（`/mnt/mcp/{pid}-mock-server/tools/query`）
  - [x] 4.5 验证 reasonStep 权限检查对未授权路径拒绝（如 `/dev/unknown`），并验证 DebugChan 事件含 permission_denied

- [x] Task 5: 边界条件与回归测试（AC: #1, #2）
  - [x] 5.1 测试仅有 Agent + Skill 无 MCP 的场景（两层正常工作）
  - [x] 5.2 测试仅有 Agent + MCP 无 Skill 的场景（MCP 路径自动加入 AllowedDevices）
  - [x] 5.3 测试 MCP Mount 失败时回滚不影响 Device 层（已挂载的 MCP 被正确卸载，进程不创建）
  - [x] 5.4 测试进程异常退出时 MCP 自动清理（Kill 后 Unmount 被调用）
  - [x] 5.5 测试多个 MCP 服务器 + 多个 Skill 的场景（所有路径正确聚合）

- [x] Task 6: 集成验证（AC: #1, #2）
  - [x] 6.1 `make test` 全部通过（含 `-race`）
  - [x] 6.2 `make lint` 通过
  - [x] 6.3 `make build` 编译成功
  - [x] 6.4 现有测试无回归

## Dev Notes

### 核心设计决策

**本 Story 是纯验证性 Story**：不引入新的生产代码，仅通过全面的集成测试验证 Story 9.1-9.3 建立的四层能力栈正确协同。四层能力栈的架构层次：

```
Agent（身份 + 策略）
  ← agent.yaml: name, description, models, context_budget, skills, mcp
  ← instructions.md: 角色定义和行为策略

  Skill（程序性知识 + 工具权限）
    ← SKILL.md: name, description, allowed-tools（/dev/ 设备白名单）
    ← body: 程序性知识指令

    MCP（外部服务集成）
      ← mcp.yaml: servers 全局配置
      ← /mnt/mcp/{pid}-{name}/: VFS 挂载路径
      ← tools/list, tools/call, resources/list, resources/read

      Device（原生 I/O）
        ← /dev/llm/claude: LLM 推理
        ← /dev/shell: Shell 命令执行
        ← /dev/fs: 宿主文件系统访问
```

**测试策略：完全使用 mock，不依赖外部服务**。所有四层通过 mock 组件验证：
- Agent 层：testdata fixtures（agent.yaml + instructions.md）
- Skill 层：testdata fixtures（SKILL.md）
- MCP 层：mock MCPTransport + mock MountManager
- Device 层：mock VFSFile（shell/fs mock driver）
- LLM 层：mock LLM driver 模拟多步 reasonStep

### 技术要求

**测试文件位置**：`kernel/e2e_test.go`

理由：四层能力栈的核心协调在 Kernel（Spawn + reasonStep），测试需要访问 Kernel 内部状态（Process、AllowedDevices、MCPMounts、DebugChan）。放在 `kernel/` 包内可直接访问非导出字段。

**测试 fixtures**：

```
kernel/testdata/e2e-agent/
├── agent.yaml            # name: e2e-test-agent, skills: [e2e-skill], mcp: [mock-server]
└── instructions.md       # 简单测试用角色指令

kernel/testdata/e2e-skill/
└── SKILL.md              # name: e2e-skill, allowed-tools: /dev/llm/claude /dev/shell /dev/fs
```

**agent.yaml 内容**：

```yaml
name: e2e-test-agent
description: "端到端测试用 Agent，引用 Skill 和 MCP"
models:
  provider: claude
  preferred: sonnet
  fallback: haiku
context_budget: 4096
skills:
  - e2e-skill
mcp:
  - mock-server
```

**SKILL.md 内容**：

```markdown
---
name: e2e-skill
description: "端到端测试用 Skill"
allowed-tools: /dev/llm/claude /dev/shell /dev/fs
---

这是一个端到端测试用的 Skill，用于验证四层能力栈。
```

**mock LLM driver 多步响应模式**：

```go
// mockMultiStepLLM 模拟多步推理：
// Step 1: 返回 tool_call → /dev/shell (验证 Device 层)
// Step 2: 返回 tool_call → /mnt/mcp/{pid}-mock-server/tools/query (验证 MCP 层)
// Step 3: 返回 text "done" (任务完成)
type mockMultiStepLLM struct {
    step      int
    pid       types.PID
    mu        sync.Mutex
}

func (m *mockMultiStepLLM) Read(length int) ([]byte, error) {
    m.mu.Lock()
    defer m.mu.Unlock()
    m.step++
    switch m.step {
    case 1:
        return json.Marshal(llmResponse{
            Content: `{"tool": "/dev/shell", "params": "echo hello"}`,
            Type:    "tool_call",
        })
    case 2:
        return json.Marshal(llmResponse{
            Content: fmt.Sprintf(`{"tool": "/mnt/mcp/%d-mock-server/tools/query", "params": "test"}`, m.pid),
            Type:    "tool_call",
        })
    default:
        return json.Marshal(llmResponse{
            Content: "E2E test completed",
            Type:    "text",
        })
    }
}
```

**SyscallEvent 验证辅助函数**：

```go
// collectEvents 从 DebugChan 收集所有事件直到关闭
func collectEvents(ch <-chan types.SyscallEvent) []types.SyscallEvent {
    var events []types.SyscallEvent
    for e := range ch {
        events = append(events, e)
    }
    return events
}

// findEvent 在事件列表中查找指定 syscall 名称的事件
func findEvent(events []types.SyscallEvent, syscall string) *types.SyscallEvent {
    for i := range events {
        if events[i].Syscall == syscall {
            return &events[i]
        }
    }
    return nil
}

// findEvents 在事件列表中查找所有匹配指定 syscall 名称的事件
func findEvents(events []types.SyscallEvent, syscall string) []types.SyscallEvent {
    var result []types.SyscallEvent
    for _, e := range events {
        if e.Syscall == syscall {
            result = append(result, e)
        }
    }
    return result
}
```

**四层调用链路预期事件序列**：

```
1. Spawn         → Agent 层：创建进程，args 含 agent/skills/allowed_devices/mcp_mounts
2. CtxAlloc      → 上下文分配
3. CtxWrite      → SetSystemPrompt (Agent instructions + Skill instructions)
4. CtxWrite      → AppendMessage(user, intent)
5. Open          → /dev/llm/claude (Device 层：LLM 设备)
6. Mount         → /mnt/mcp/{pid}-mock-server (MCP 层：自动挂载，auto=true)
7. ReasonStep    → step=1, action=...
8. Write         → fd=llmFD (向 LLM 写请求)
9. Read          → fd=llmFD (读 LLM 响应)
10. CtxWrite     → AppendMessage(assistant, ...)
11. Open         → /dev/shell (Device 层：工具调用)
12. Write        → /dev/shell 写入参数
13. Read         → /dev/shell 读取结果
14. Close        → /dev/shell
15. CtxWrite     → AppendToolResult
16. ... (第二轮推理 → MCP 工具调用)
17. Open         → /mnt/mcp/{pid}-mock-server/tools/query (MCP 层：工具调用)
18. Write        → MCP 工具写入参数
19. Read         → MCP 工具读取结果
20. Close        → MCP 工具
21. ... (第三轮推理 → text 完成)
22. Unmount      → /mnt/mcp/{pid}-mock-server (MCP 层：自动卸载，auto=true)
```

### VFS 路径约定

```
四层能力栈涉及的 VFS 路径：
/dev/llm/claude                          → Device 层：LLM 推理设备
/dev/shell                               → Device 层：Shell 命令设备
/dev/fs                                  → Device 层：宿主文件系统设备
/mnt/mcp/{pid}-{server}/                 → MCP 层：挂载根（只读，列出命名空间）
/mnt/mcp/{pid}-{server}/tools            → MCP 层：工具列表
/mnt/mcp/{pid}-{server}/tools/{name}     → MCP 层：工具调用
/mnt/mcp/{pid}-{server}/resources        → MCP 层：资源列表
/mnt/mcp/{pid}-{server}/resources/{uri}  → MCP 层：资源读取
```

### 依赖方向

```
本 Story 不引入新的包间依赖。所有改动为测试文件：
kernel/e2e_test.go       → kernel/（同包测试）
                         → 使用已有的 mock 模式（mockVFS, mockCtxMgr, mockMountMgr）
kernel/testdata/         → 测试 fixture 文件，不影响编译
```

### 代码复用

**必须复用的现有代码**：
- `kernel.KernelImpl.Spawn` — 四层能力栈的核心入口
- `kernel.KernelImpl.reasonStep` — 推理循环，协调 LLM/Tool/MCP 调用
- `kernel.KernelImpl.finishProcess` — 进程完成处理，含 MCP 自动 Unmount
- `kernel.KernelImpl.emitEvent` — SyscallEvent 记录
- `kernel.Process` — 进程状态：AllowedDevices、MCPMounts、DebugChan、FDTable
- `kernel.ExitStatus` — 进程退出状态
- `kernel.SpawnOpts` — Spawn 配置选项
- `kernel.NewSyscallError` — 错误包装
- `agents.AgentInfo` — Agent 完整信息（Manifest + Instructions + Skills + MCPConfigs）
- `agents.AgentManifest` — Agent 清单（name/skills/mcp）
- `skills.SkillInfo` / `skills.SkillManifest` — Skill 信息
- `vfs.MCPConfig` — MCP 配置
- `vfs.MCPTransport` — MCP 传输接口（mock 实现）
- `types.SyscallEvent` — syscall 事件结构体
- `debug.NewEvent` / `debug.CompleteEvent` / `debug.EmitEvent` — 调试事件
- `debug.Attach` / `debug.FormatEvent` — astrace 格式化（可选验证）

**参考现有测试模式**：
- `kernel/kernel_test.go` — Kernel 单元测试中的 mock 模式（mockVFS, mockCtxMgr）
- `kernel/spawn_mcp_test.go` — MCP 自动挂载测试（mock MountManager）
- `kernel/ipc_test.go` — 多进程集成测试模式
- `vfs/mcp_test.go` — MCP VFSFile 测试中的 mockTransport

### 反模式防护

- **不要**引入任何新的生产代码 — 本 Story 是纯验证性，仅添加测试文件和 testdata
- **不要**修改现有 Kernel/VFS/MCP 代码 — 测试必须验证现有代码的正确性
- **不要**依赖真实的 MCP 服务器或 Claude CLI — 所有外部依赖使用 mock
- **不要**创建新的包或模块 — 测试放在 `kernel/` 包内
- **不要**在测试中使用 `time.Sleep` 等待异步操作 — 使用 channel 同步或 `proc.Wait()`
- **不要**忽略竞态条件 — 使用 `go test -race` 确保并发安全
- **不要**使用 `.yml` 后缀 — 统一 `.yaml`
- **不要**返回裸 error — 错误断言使用 `types.SyscallError` 类型检查
- **不要**跳过 DebugChan 事件验证 — astrace 可观测性是本 Story 的核心验收标准
- **不要**引入 vfs → kernel 方向的导入 — 保持架构边界

### 测试策略

**集成测试级别**：本 Story 的测试是跨多层的集成测试，需要完整的 Kernel 实例配合各层 mock 组件。

**mock 组件清单**：

| 组件 | Mock 实现 | 用途 |
|------|-----------|------|
| LLM Driver | mockMultiStepLLM | 模拟多步推理（tool_call → tool_call → text） |
| Shell Driver | mockShellVFSFile | 模拟 /dev/shell 工具执行 |
| MCP Transport | mockTransport | 模拟 MCP tools/call 响应 |
| MountManager | 使用真实 MountManager + mockTransportFactory | 验证 Mount/Unmount 生命周期 |
| Context Manager | 使用真实 ContextManager | 验证上下文流转 |
| VFS | 使用真实 VFS + DeviceRegistry | 验证设备路由 |

**竞态检测**：所有测试必须通过 `go test -race`。特别注意：
- Process.AllowedDevices 并发读（reasonStep）和写（Spawn MCP Mount）
- Process.MCPMounts 并发读（finishProcess Unmount）和写（Spawn Mount）
- DebugChan 并发写（emitEvent）和关闭（Wait）

### 前一个 Story 的经验教训（来自 Story 9.1-9.3）

1. **Lock 保护并发字段**：Story 9.2 Review 发现 MCPMounts 赋值未加锁（HIGH-1），已修复。本 Story 的测试应验证 AllowedDevices 和 MCPMounts 在并发场景下安全。

2. **LLM FD 资源泄漏**：Story 9.2 发现 MCP Mount 失败时 LLM FD 未关闭（MEDIUM-1），已修复。本 Story 应测试 MCP Mount 失败时的回滚清理。

3. **readFromBuffer 一致性**：Story 9.3 提取了 readFromBuffer 辅助函数，所有 5 种 MCP 文件类型使用统一的分块读取逻辑。本 Story 的 MCP 工具调用测试会间接验证此行为。

4. **AllowedDevices 聚合**：Story 9.3 在 Spawn 中将 MCP 挂载路径追加到 AllowedDevices（variadic append）。本 Story 需验证 Skill 设备路径和 MCP 路径同时存在于 AllowedDevices 中。

5. **StdioTransport.Ping 仍为 no-op**：不影响本 Story。测试使用 mock transport。

6. **bare error 问题**：Story 9.3 Review 指出 closed-file 检查返回裸 error（pre-existing pattern），本 Story 不修复但测试中注意此行为。

### Git 提交模式参考

最近提交（a603617）为 Story 9.3 实现。本 Story 是 Epic 9 的最后一个 Story，纯验证性质。主要影响：
- 新增：`kernel/e2e_test.go`（四层能力栈集成测试）
- 新增：`kernel/testdata/e2e-agent/agent.yaml`
- 新增：`kernel/testdata/e2e-agent/instructions.md`
- 新增：`kernel/testdata/e2e-skill/SKILL.md`

不修改任何现有生产代码文件。

### Project Structure Notes

新增文件：
```
kernel/e2e_test.go                              # 四层能力栈端到端集成测试
kernel/testdata/e2e-agent/agent.yaml            # 测试用 Agent 定义
kernel/testdata/e2e-agent/instructions.md       # 测试用 Agent 指令
kernel/testdata/e2e-skill/SKILL.md              # 测试用 Skill 定义
```

不修改的文件：
- `kernel/kernel.go` — Spawn/reasonStep/finishProcess 不变
- `vfs/mcp.go` — mcpFileFactory/mcpFile 系列不变
- `vfs/mount.go` — MountManager 不变
- `agents/types.go` — AgentManifest/AgentInfo 不变
- `agents/loader.go` — AgentLoader 不变
- `skills/types.go` — SkillManifest/SkillInfo 不变
- `drivers/mcp/transport.go` — StdioTransport 不变
- `debug/astrace.go` — Attach/FormatEvent 不变
- `cmd/crux/main.go` — Daemon 初始化不变

### References

- [Source: _bmad-output/planning-artifacts/epics/epic-9-mcp-服务集成mcp-integration.md#Story 9.4]
- [Source: _bmad-output/planning-artifacts/prd/functional-requirements.md#FR57] — 四层能力栈端到端运行和 astrace 验证
- [Source: _bmad-output/planning-artifacts/architecture/core-architectural-decisions.md#Decision 7] — Agent 抽象层与 Skill 标准化，MCP 兼容性
- [Source: _bmad-output/planning-artifacts/architecture/core-architectural-decisions.md#Decision 3] — VFS 实现策略
- [Source: _bmad-output/planning-artifacts/architecture/core-architectural-decisions.md#Decision 5] — 调试架构（astrace）
- [Source: _bmad-output/planning-artifacts/architecture/project-structure-boundaries.md] — 依赖方向和架构边界
- [Source: _bmad-output/planning-artifacts/architecture/implementation-patterns-consistency-rules.md] — 命名和编码规则
- [Source: _bmad-output/project-context.md] — 项目编码规则
- [Source: _bmad-output/implementation-artifacts/9-1-mount-unmount-syscall.md] — Story 9.1 实现（Mount/Unmount 基础设施）
- [Source: _bmad-output/implementation-artifacts/9-2-agent-yaml-mcp-field-and-auto-mount.md] — Story 9.2 实现（agent.yaml mcp 字段 + 自动挂载）
- [Source: _bmad-output/implementation-artifacts/9-3-vfs-path-expose-mcp-tools.md] — Story 9.3 实现（VFS 路径暴露 MCP 工具）
- [Source: kernel/kernel.go#Spawn] — 四层能力栈核心入口：Agent 加载 → Skill 权限聚合 → MCP 自动挂载 → Device 路由
- [Source: kernel/kernel.go#reasonStep] — 推理循环：LLM 调用 → ActionToolCall 权限检查 → VFS Open/Write/Read/Close
- [Source: kernel/kernel.go#finishProcess] — 进程完成：MCP 自动 Unmount → Terminate
- [Source: kernel/kernel.go#emitEvent] — SyscallEvent 非阻塞发送到 DebugChan
- [Source: kernel/process.go] — Process: AllowedDevices, MCPMounts, DebugChan, FDTable
- [Source: vfs/mcp.go] — mcpFileFactory 子路径路由 + 5 种 MCP VFSFile 类型
- [Source: vfs/mount.go] — MountManager.Mount/Unmount
- [Source: agents/types.go] — AgentManifest.MCP, AgentInfo.MCPConfigs
- [Source: agents/loader.go] — AgentLoader: 解析 skills 和 mcp 引用
- [Source: debug/astrace.go] — Attach: 消费 DebugChan 事件
- [Source: debug/event.go] — SyscallEvent 结构体
- [Source: kernel/spawn_mcp_test.go] — MCP Spawn 测试模式参考
- [Source: kernel/kernel_test.go] — Kernel mock 模式参考

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6 (code review)

### Debug Log References

### Completion Notes List

- 四层能力栈端到端测试已全部实现，4 个测试函数、约 24 个子测试全部通过（含 -race）
- testdata fixtures 已创建但测试代码中使用 in-memory AgentInfo 构造而非加载 fixture 文件
- Code review 修复了两个 MEDIUM 问题：加强了时间顺序验证和权限拒绝断言
- 所有 ACs 已通过验证

### File List

- `kernel/e2e_test.go` — 四层能力栈端到端集成测试（新增，~1280 行）
- `kernel/testdata/e2e-agent/agent.yaml` — 测试用 Agent 定义（新增）
- `kernel/testdata/e2e-agent/instructions.md` — 测试用 Agent 指令（新增）
- `kernel/testdata/e2e-skill/SKILL.md` — 测试用 Skill 定义（新增）

### Code Review Record

**Reviewer:** Decker (AI)
**Date:** 2026-03-02
**Result:** Approved with fixes applied

**Issues Found:** 2 HIGH, 3 MEDIUM, 4 LOW
**Issues Fixed:** 2 HIGH, 2 MEDIUM (code fixes)
**Remaining:** 1 MEDIUM (accepted), 4 LOW (accepted)

**Code Fixes Applied:**
1. (MEDIUM) `event_chronological_order` 测试 -- 从弱断言重写为严格逐对 firstIdx 比较
2. (MEDIUM) `permission_denies_unauthorized_path` 测试 -- 添加 DebugChan 事件收集和 permission_denied 事件断言

**Documentation Fixes Applied:**
1. (HIGH) Dev Agent Record 从空白填充为完整记录
2. (HIGH) 所有 27 个 Task/Subtask 标记为 [x] 完成
