# Epic 9: MCP 服务集成（MCP Integration）

系统通过 VFS 挂载 MCP 服务器，智能体通过标准文件操作访问外部工具——完成 Agent → Skill → MCP → Device 四层能力栈。

## Story 9.1: Mount/Unmount syscall

As a 平台构建者,
I want 通过 Mount/Unmount syscall 在 VFS 中挂载和卸载 MCP 服务器,
So that 外部服务可以作为文件路径被智能体访问。

**Acceptance Criteria:**

**Given** `vfs/mcp.go` 已实现
**When** 调用 `Mount("/mnt/mcp/github", mcpConfig)`
**Then** 在 `/mnt/mcp/github/` 路径下挂载 MCP 服务器
**And** 挂载延迟 ≤ 500ms（NFR25）

**Given** MCP 服务器已挂载
**When** 调用 `Unmount("/mnt/mcp/github")`
**Then** 卸载服务器，关闭连接，清理 VFS 路径

**Given** MCP 服务器异常退出
**When** 智能体访问 `/mnt/mcp/github/` 下的路径
**Then** 3 秒内返回 `ErrServiceUnavailable` 错误（NFR26）
**And** 不影响内核稳定性

**Given** 已挂载路径
**When** 重复 Mount
**Then** 返回 `*SyscallError`（路径已占用）

## Story 9.2: agent.yaml mcp 字段与自动挂载

As a 用户,
I want Agent 的 agent.yaml 中通过 `mcp` 字段引用 MCP 服务器，Spawn 时自动挂载,
So that 我不需要手动管理 MCP 服务器的生命周期。

**Acceptance Criteria:**

**Given** agent.yaml 包含 `mcp: ["github", "slack"]`
**When** Spawn 该 Agent 的智能体
**Then** 系统自动 Mount 引用的 MCP 服务器到 `/mnt/mcp/{name}/`
**And** 进程退出时自动 Unmount

**Given** `drivers/mcp/mcp.go` 已实现
**When** MCP 服务器启动
**Then** 管理 MCP 服务器进程生命周期（启动、健康检查、停止）

**Given** MCP 配置缺失或无效
**When** Spawn 时引用该 MCP
**Then** 返回清晰错误信息，标注具体配置问题

## Story 9.3: VFS 路径暴露 MCP 工具

As a 平台构建者,
I want 智能体通过标准 VFS Open/Read/Write 访问 MCP 服务器提供的工具和资源,
So that MCP 集成对智能体透明，无需了解 MCP 协议细节。

**Acceptance Criteria:**

**Given** MCP 服务器已挂载（如 `/mnt/mcp/github/`）
**When** 调用 `Open("/mnt/mcp/github/tools/create-issue")`
**Then** 返回 VFSFile 封装的 MCP 工具接口

**Given** MCP 工具已打开
**When** 调用 `Write(fd, toolParams)` + `Read(fd, maxLen)`
**Then** 调用 MCP 服务器执行工具并返回结果

**Given** MCP 兼容性
**When** 接入符合 MCP 标准的第三方服务器
**Then** 无需 Rnix 侧代码修改即可挂载和使用（NFR27）

## Story 9.4: 四层能力栈端到端验证

As a 用户,
I want 验证 Agent → Skill → MCP → Device 四层能力栈端到端工作,
So that 确认各层职责分离且协同正确。

**Acceptance Criteria:**

**Given** 配置了包含 Skill 和 MCP 引用的 Agent
**When** Spawn 并执行任务
**Then** Agent 层提供身份和策略
**And** Skill 层提供程序性知识和工具权限
**And** MCP 层提供外部服务集成
**And** Device 层提供原生 I/O（`/dev/`）

**Given** `rnix strace` 追踪该进程
**When** 查看 syscall 链路
**Then** 可以清晰看到四层的调用边界和数据流向（FR57）

---
