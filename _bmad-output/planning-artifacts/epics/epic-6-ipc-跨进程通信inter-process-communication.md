# Epic 6: IPC 跨进程通信（Inter-Process Communication）

智能体之间可以通过消息传递、管道连接、进程组管理和信号控制实现协作——多智能体系统的通信基础设施。

## Story 6.1: Send/Recv 消息传递

As a 智能体,
I want 通过 Send/Recv syscall 向其他智能体发送消息和接收消息,
So that 多个智能体之间可以交换数据和协调工作。

**Acceptance Criteria:**

**Given** `kernel/ipc.go` 中 Send/Recv 已实现
**When** 智能体 A 调用 `Send(targetPID, message)`
**Then** 消息进入目标进程的消息队列
**And** 目标进程调用 `Recv()` 时获取到该消息
**And** 单条消息端到端延迟 ≤ 50ms（NFR22）

**Given** 目标 PID 不存在
**When** 调用 `Send(999, message)`
**Then** 返回 `*SyscallError`，`Code` 为 `ErrNotFound`

**Given** 接收队列为空
**When** 调用 `Recv()`
**Then** 阻塞直到有新消息到达或 context 取消

**Given** `IPCManager` 子接口已定义
**When** 嵌入到 Kernel 接口
**Then** 不破坏 Phase 1 现有 ABI（NFR19）

**Given** 多个智能体并发 Send
**When** 同时向同一目标进程发送消息
**Then** 消息全部到达，无丢失，无数据竞争（`-race` 测试通过）

## Story 6.2: Pipe 管道

As a 用户,
I want 通过 Pipe 将一个智能体的输出连接为另一个智能体的输入,
So that 智能体可以流式传递数据，实现链式处理。

**Acceptance Criteria:**

**Given** `kernel/ipc.go` 中 Pipe 已实现
**When** 调用 `Pipe()` 创建管道
**Then** 返回 `(readFD, writeFD)` 一对文件描述符
**And** 写入 writeFD 的数据可从 readFD 读取

**Given** 管道已创建
**When** 智能体 A 向 writeFD 写入，智能体 B 从 readFD 读取
**Then** 数据正确传递，吞吐量 ≥ 1MB/s（NFR23）

**Given** 写端关闭
**When** 读端继续 Read
**Then** 返回 EOF，不阻塞

**Given** 读端关闭
**When** 写端继续 Write
**Then** 返回 `*SyscallError`，`Code` 为 `ErrBrokenPipe`

**Given** 管道用于 Compose 编排
**When** 前置智能体完成后
**Then** 其输出通过管道自动注入下游智能体的上下文

## Story 6.3: 进程组与批量信号

As a 用户,
I want 将多个智能体分组管理，一条命令控制整组进程,
So that 我可以高效管理多智能体工作流。

**Acceptance Criteria:**

**Given** `kernel/procgroup.go` 已实现
**When** 调用 `JoinGroup(pid, groupID)`
**Then** 目标进程加入指定组

**Given** 进程已加入组
**When** 调用 `GetProcGroup(groupID)`
**Then** 返回组内所有进程的 PID 列表

**Given** 进程组存在
**When** 向组发送信号（如 Kill）
**Then** 组内所有进程收到信号
**And** 操作延迟不超过单进程的 2 倍（NFR24）

**Given** 进程退出
**When** 该进程属于某个组
**Then** 自动从组中移除

## Story 6.4: Signal 信号系统

As a 智能体,
I want 通过 Signal syscall 向其他进程发送信号（中断、暂停、恢复）,
So that 智能体之间可以协调执行节奏。

**Acceptance Criteria:**

**Given** `kernel/signal.go` 已实现
**When** 调用 `Signal(targetPID, sig)`
**Then** 目标进程的信号处理器被触发

**Given** 目标进程调用 `SigBlock(sig)`
**When** 有该类型信号到达
**Then** 信号被暂存到 pending 队列

**Given** 目标进程调用 `SigUnblock(sig)`
**When** pending 队列中有该类型信号
**Then** 立即触发信号处理器

**Given** 目标进程未注册特定信号处理器
**When** 收到信号
**Then** 执行默认行为（Kill 信号 → 终止进程，Pause → 暂停推理循环，Resume → 恢复）

## Story 6.5: 三级并发模型

As a 平台构建者,
I want 系统提供进程、线程、协程三级并发原语,
So that 我可以为不同粒度的任务选择最合适的并发模型。

**Acceptance Criteria:**

**Given** 三级并发模型已实现
**When** 创建进程级智能体（`Spawn`）
**Then** 拥有独立上下文和独立 LLM 会话
**And** 完全隔离，通过 IPC 通信

**Given** 三级并发模型已实现
**When** 创建线程级执行单元
**Then** 共享父进程的上下文空间
**And** 拥有独立执行流（goroutine）
**And** 通过共享上下文交换数据

**Given** 三级并发模型已实现
**When** 创建协程级执行单元
**Then** 轻量协作调度，yield 语义
**And** 适用于上下文内的子任务分解

**Given** ≥ 10 个并发智能体（进程级）
**When** 同时运行
**Then** 进程表操作延迟不超过单进程场景的 2 倍（NFR24）

---
