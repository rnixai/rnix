# Epic 13: 交互式智能体调试（agdb）

用户可以附着到运行中的智能体，设置断点（syscall/推理/质量/预算四种类型）、单步执行、检查和热修改运行时参数，实现类 GDB 的交互式调试体验。

## Story 13.1: agdb 调试会话管理（Attach/Detach）

As a 平台构建者,
I want 通过 `rnix agdb <pid>` 附着到运行中的智能体进入交互式调试会话，并可随时 Detach 断开,
So that 我可以在不中断智能体执行的前提下进入和退出调试模式。

**Acceptance Criteria:**

**Given** 一个 Running 状态的智能体进程 PID=N
**When** 用户执行 `rnix agdb N`
**Then** 系统通过 IPC 发送 `attach_agdb` 请求，成功后进入交互式调试 TUI
**And** Attach 延迟 <= 200ms（NFR31）

**Given** 用户处于 agdb 调试会话中
**When** 用户执行 `detach` 命令
**Then** 调试会话断开，智能体继续正常执行，不受影响

**Given** 目标进程不存在或已处于 Dead 状态
**When** 用户执行 `rnix agdb N`
**Then** 系统返回结构化错误信息：进程不存在/已终止

## Story 13.2: 断点系统

As a 平台构建者,
I want 在 agdb 中设置四种断点（syscall/推理/质量/预算），精确控制智能体暂停的时机,
So that 我可以在关键执行节点检查智能体状态，定位问题根因。

**Acceptance Criteria:**

**Given** 用户处于 agdb 调试会话中
**When** 用户执行 `break syscall Read`
**Then** 智能体在下次调用 Read syscall 前暂停，显示 syscall 参数

**Given** 用户处于 agdb 调试会话中
**When** 用户执行 `break reasoning`
**Then** 智能体在每次 LLM 调用前暂停，显示即将发送的 prompt 摘要

**Given** 用户设置质量断点 `break quality --pattern "安全漏洞"`
**When** 智能体输出包含匹配关键词
**Then** 智能体暂停，高亮显示匹配内容

**Given** 用户设置质量断点 `break quality --eval "输出必须包含代码示例"`
**When** 智能体输出经轻量模型评估不满足标准
**Then** 智能体暂停，显示评估结果和不满足原因

**Given** 用户设置预算断点 `break budget 5000`
**When** 智能体 token 消耗达到 5000
**Then** 智能体暂停，显示当前 token 消耗明细
**And** 断点触发到暂停延迟 <= 100ms（NFR31）

## Story 13.3: 单步执行与状态检查

As a 平台构建者,
I want 在 agdb 中逐步执行智能体的每个 syscall 或推理步骤，查看每步的参数、返回值和上下文变化,
So that 我可以精确追踪智能体的执行轨迹，理解每一步决策的依据。

**Acceptance Criteria:**

**Given** 智能体在断点处暂停
**When** 用户执行 `step syscall`
**Then** 智能体执行下一个 syscall 后暂停，显示 syscall 名称、参数、返回值和耗时

**Given** 智能体在断点处暂停
**When** 用户执行 `step reasoning`
**Then** 智能体执行完整的下一个推理步骤后暂停，显示推理结果摘要

**Given** 智能体在断点处暂停
**When** 用户执行 `continue`
**Then** 智能体恢复正常执行直到下一个断点或完成

**Given** 智能体在任意暂停点
**When** 用户执行 `inspect context`
**Then** 显示当前上下文的分段内容及各段 token 占比

## Story 13.4: 运行时参数热修改

As a 平台构建者,
I want 在 agdb 中检查和修改智能体的运行时参数，修改立即生效于下一个推理步骤,
So that 我可以在调试过程中快速测试不同配置对智能体行为的影响。

**Acceptance Criteria:**

**Given** 智能体在断点处暂停
**When** 用户执行 `set model sonnet`
**Then** 智能体的模型偏好切换为 sonnet，下一次 LLM 调用使用新模型

**Given** 智能体在断点处暂停
**When** 用户执行 `set context append "额外分析指令"`
**Then** 指定内容被追加到上下文，下一次推理步骤包含该内容

**Given** 智能体在断点处暂停
**When** 用户执行 `set skills add code-review`
**Then** code-review Skill 被加载并加入智能体的能力列表

**Given** 智能体在断点处暂停
**When** 用户执行 `set env DEBUG=true`
**Then** 环境变量被设置，智能体后续执行可引用该变量
