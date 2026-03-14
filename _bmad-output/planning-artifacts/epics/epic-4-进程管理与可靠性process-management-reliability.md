# Epic 4: 进程管理与可靠性（Process Management & Reliability）

用户可以查看进程状态（`rnix ps`）、终止进程（`rnix kill`）、等待进程完成。系统自动回收 Zombie、处理孤儿进程、暴露 `/proc` 运行时状态——生产级可靠性。

## Story 4.1: Kill 与 Wait 系统调用

As a 用户,
I want 终止运行中的智能体并等待其完成,
So that 我可以管理智能体的生命周期。

**Acceptance Criteria:**

**Given** `kernel/kernel.go` 中 Kill 已实现
**When** 调用 `Kill(pid, signal)`
**Then** 向目标进程发送取消信号（`cancel()`）
**And** 进程 reasonStep 循环检测到取消后停止
**And** 进程状态转为 Zombie

**Given** `kernel/reap.go` 中 Wait 已实现
**When** 调用 `Wait(pid)`
**Then** 阻塞直到目标进程状态变为 Zombie
**And** 返回 ExitStatus（退出码 + 错误信息）
**And** 触发资源释放：cancel() → wg.Wait() → 关闭 FD → 关闭 DebugChan → CtxFree
**And** 状态转为 Dead，从进程表移除

**Given** 目标 PID 不存在
**When** 调用 Kill 或 Wait
**Then** 返回 `*SyscallError`，`Code` 为 `ErrNotFound`

**Given** 进程已经是 Zombie
**When** 调用 Kill
**Then** 操作为空操作（幂等），不返回错误

## Story 4.2: 孤儿进程 reparent 与 Zombie 自动回收

As a 平台构建者,
I want 孤儿进程自动挂载到 init，Zombie 进程自动回收,
So that 系统不会积累无主进程或资源泄漏。

**Acceptance Criteria:**

**Given** 父进程退出
**When** 子进程仍在运行
**Then** 子进程的 PPID 自动变更为 PID 1（init）
**And** init 负责后续 Wait 回收

**Given** 进程状态变为 Zombie
**When** 内核 reaper 检测到
**Then** 自动执行 Wait 逻辑：资源释放 → 状态转 Dead → 移除进程表
**And** 资源释放顺序：cancel() → wg.Wait() → 关闭 FD → 关闭 DebugChan → CtxFree → Dead → 移除

**Given** 进程退出后
**When** 10 秒内检测 goroutine 状态
**Then** 所有关联 goroutine 和 context 内存已释放，无泄漏（NFR8）

**Given** 进程异常退出（panic、timeout）
**When** 检查进程表
**Then** 进程表保持一致性，无悬挂 PID（NFR9）

## Story 4.3: /proc 动态文件系统

As a 用户,
I want 通过 `/proc/{pid}/` 路径查看智能体的运行时状态,
So that 我可以程序化地获取进程信息。

**Acceptance Criteria:**

**Given** `vfs/proc.go` 已实现
**When** 调用 `Open("/proc/1/status")`
**Then** 返回 PID 1 的实时状态 JSON（pid、state、intent、skills、tokens、elapsed）

**Given** ProcFS 已注册
**When** 调用 `Open("/proc/1/intent")`
**Then** 返回 PID 1 的原始意图文本

**Given** ProcFS 已注册
**When** 调用 `Open("/proc/1/context")`
**Then** 返回 PID 1 的当前上下文内容摘要

**Given** PID 不存在
**When** 调用 `Open("/proc/999/status")`
**Then** 返回 `*SyscallError`，`Code` 为 `ErrNotFound`

**Given** ProcFS 需要读取进程信息
**When** 查看实现
**Then** 通过 `ProcessInfoProvider` 接口读取（不直接依赖 kernel 包，避免反向依赖）

## Story 4.4: rnix ps 命令与 Process Table UI

As a 用户,
I want 通过 `rnix ps` 查看所有进程的状态表格,
So that 我随时了解系统中智能体的全局状态。

**Acceptance Criteria:**

**Given** `cmd/rnix/main.go` 中 ps 子命令已注册
**When** 执行 `rnix ps`
**Then** 调用 `kernel.PS(filter)` 获取所有进程信息
**And** 通过 Process Table 组件输出对齐表格

**Given** `internal/ui/table.go` 已实现
**When** 渲染进程表格
**Then** 列包含 PID、STATE、SKILL、TOKENS、ELAPSED
**And** 数字右对齐，文本左对齐
**And** STATE 列颜色编码：running=蓝、zombie=黄、dead=灰
**And** 响应时间 ≤ 100ms（NFR2）

**Given** 无活跃进程
**When** 执行 `rnix ps`
**Then** 输出 `No active processes.`（不显示空表格）

**Given** 使用 `--json` flag
**When** 执行 `rnix ps --json`
**Then** 输出 JSON 数组，每个元素包含 pid、state、skill、tokens、elapsed_ms（snake_case）

**Given** 终端宽度 < 80 列
**When** 渲染表格
**Then** 按优先级保留列：PID + STATE（永远显示）→ SKILL（≥60 列）→ TOKENS + ELAPSED（≥80 列）

## Story 4.5: 上下文释放（ctx_free）

As a 平台构建者,
I want 进程退出后其上下文空间被正确释放,
So that 系统不会因为上下文累积而内存泄漏。

**Acceptance Criteria:**

**Given** 进程状态转为 Dead
**When** 资源释放流程执行
**Then** 调用 `CtxFree(process.Ctx.ID)` 释放上下文空间

**Given** 上下文已释放
**When** 尝试 CtxRead 或 CtxWrite 该 CtxID
**Then** 返回 `*SyscallError`，`Code` 为 `ErrNotFound`

**Given** 多个进程同时退出
**When** 并发执行 CtxFree
**Then** 无数据竞争（通过 `-race` 测试验证）

---
