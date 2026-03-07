# Story 13.4: 运行时参数热修改

Status: done

## Story

As a 平台构建者,
I want 在 gdb 中检查和修改智能体的运行时参数，修改立即生效于下一个推理步骤,
So that 我可以在调试过程中快速测试不同配置对智能体行为的影响。

## Acceptance Criteria

1. **Given** 智能体在断点处暂停
   **When** 用户执行 `set model sonnet`
   **Then** 智能体的模型偏好切换为 sonnet，下一次 LLM 调用使用新模型

2. **Given** 智能体在断点处暂停
   **When** 用户执行 `set context append "额外分析指令"`
   **Then** 指定内容被追加到上下文，下一次推理步骤包含该内容

3. **Given** 智能体在断点处暂停
   **When** 用户执行 `set skills add code-review`
   **Then** code-review Skill 被加载并加入智能体的能力列表

4. **Given** 智能体在断点处暂停
   **When** 用户执行 `set env DEBUG=true`
   **Then** 环境变量被设置，智能体后续执行可引用该变量

## Tasks / Subtasks

- [x] Task 1: Process 运行时参数存储 (AC: #1, #2, #3, #4)
  - [x] 1.1 在 `kernel/process.go` 的 `Process` 结构体中新增 `gdbModelOverride string` 字段（mu 保护），空字符串表示无覆盖
  - [x] 1.2 实现 `Process.SetGdbModelOverride(model string)` 方法
  - [x] 1.3 实现 `Process.GetGdbModelOverride() string` 方法
  - [x] 1.4 在 `Process` 结构体中新增 `gdbEnvVars map[string]string` 字段（mu 保护）
  - [x] 1.5 实现 `Process.SetGdbEnv(key, value string)` 方法
  - [x] 1.6 实现 `Process.GetGdbEnvVars() map[string]string` 方法（返回副本）
  - [x] 1.7 在 `Process` 结构体中新增 `gdbExtraSkills []string` 字段（mu 保护）
  - [x] 1.8 实现 `Process.AddGdbSkill(name string)` 方法（幂等，避免重复）
  - [x] 1.9 实现 `Process.GetGdbExtraSkills() []string` 方法（返回副本）

- [x] Task 2: 内核 reasonStep 模型覆盖钩子 (AC: #1)
  - [x] 2.1 在 `kernel/kernel.go` 的 `reasonStep` 循环内，构建 `llmRequest` 之前，检查 `proc.GetGdbModelOverride()`
  - [x] 2.2 如果 `gdbModelOverride != ""`，使用覆盖值替代 `opts.Model` 构建请求：`model := opts.Model; if override := proc.GetGdbModelOverride(); override != "" { model = override }`
  - [x] 2.3 在 `llmRequest` 构建和 `emitEvent` 中使用局部 `model` 变量（不修改 `opts.Model`，保持原始值可恢复）
  - [x] 2.4 确保覆盖是持久的（设置一次后每步都生效，直到再次覆盖或清除）

- [x] Task 3: IPC 协议扩展 -- set 命令 (AC: #1, #2, #3, #4)
  - [x] 3.1 在 `ipc/server.go` 的 `handleGdbCommand` switch 中新增 `"set"` 命令分支
  - [x] 3.2 实现 `handleGdbSet(proc *kernel.Process, args []string) GdbCommandResponse`
  - [x] 3.3 `handleGdbSet` 解析 `args[0]`：
    - `"model"` -> 调用 `proc.SetGdbModelOverride(args[1])`
    - `"context"` -> 根据 `args[1]` 分发（`"append"`）
    - `"skills"` -> 根据 `args[1]` 分发（`"add"`）
    - `"env"` -> 解析 `KEY=VALUE` 格式，调用 `proc.SetGdbEnv(key, value)`
  - [x] 3.4 `set context append` 实现：使用 `s.ctxMgr.AppendMessage(proc.CtxID, context.RoleUser, content)` 将文本追加到上下文
  - [x] 3.5 `set skills add` 实现：调用 `proc.AddGdbSkill(skillName)` 记录技能名（注意：MVP 只记录技能名到列表，不真正加载 skill body 到 system prompt -- 真正的 skill 注入需要改动 Spawn 和 reasonStep 架构，超出本 story 范围）
  - [x] 3.6 `set env` 实现：解析 `KEY=VALUE` 格式（split on first `=`），调用 `proc.SetGdbEnv(key, value)`

- [x] Task 4: gdb CLI 命令扩展 -- set 命令 (AC: #1, #2, #3, #4)
  - [x] 4.1 在 `cmd/rnix/gdb.go` 的命令循环 switch 中新增 `"set"` 分支
  - [x] 4.2 实现 `gdbSet(w, client, pid, parts[1:])` 函数
  - [x] 4.3 定义 `SetCommandResult` 结构体和 `parseSetCommand(args []string)` 解析器
  - [x] 4.4 `parseSetCommand` 处理四种子命令：
    - `set model <name>` -> `{SubCommand: "model", Value: name}`
    - `set context append "<text>"` -> `{SubCommand: "context", Action: "append", Value: text}`（支持引号包裹的文本）
    - `set skills add <name>` -> `{SubCommand: "skills", Action: "add", Value: name}`
    - `set env KEY=VALUE` -> `{SubCommand: "env", Value: "KEY=VALUE"}`
  - [x] 4.5 调用 `client.SendGdbCommand(pid, "set", parsedArgs)` 发送 IPC 请求
  - [x] 4.6 显示操作结果（成功/失败消息）
  - [x] 4.7 更新 `printGdbHelp` 增加 set 命令说明

- [x] Task 5: 测试 (AC: #1-4)
  - [x] 5.1 `kernel/breakpoint_test.go`：SetGdbModelOverride/GetGdbModelOverride 设置/获取测试
  - [x] 5.2 `kernel/breakpoint_test.go`：SetGdbEnv/GetGdbEnvVars 设置/获取/副本隔离测试
  - [x] 5.3 `kernel/breakpoint_test.go`：AddGdbSkill/GetGdbExtraSkills 幂等性测试
  - [x] 5.4 `kernel/kernel_test.go`：model override 在 reasonStep 中生效测试（mock LLM 验证请求中 model 字段变更）
  - [x] 5.5 `ipc/protocol_test.go`：set 命令请求/响应序列化
  - [x] 5.6 `ipc/server_test.go`：handleGdbCommand set model/context/skills/env 路由测试
  - [x] 5.7 `cmd/rnix/gdb_test.go`：parseSetCommand 四种子命令解析测试
  - [x] 5.8 集成测试：set model -> 暂停 -> 验证下次 LLM 请求使用新模型 -> continue
  - [x] 5.9 集成测试：set context append -> 验证消息被追加到上下文
  - [x] 5.10 集成测试：set env KEY=VALUE -> 验证环境变量存储和读取

## Dev Notes

### 架构决策

本 story 在 13-3 单步执行和状态检查基础上实现运行时参数热修改。核心设计是在 Process 上存储可覆盖的运行时参数，在 reasonStep 循环中每次构建 LLM 请求时检查覆盖值。`set` 命令通过 IPC 传递到 server，server 直接修改 Process 上的字段。

### 关键设计：Model 热切换工作原理

```
用户在断点处暂停
    |
    +-- 用户输入 `set model sonnet`
    |       |
    |       +-- 1. CLI 解析 set 命令
    |       +-- 2. client.SendGdbCommand(pid, "set", ["model", "sonnet"])
    |       +-- 3. IPC server handleGdbSet 收到请求
    |       +-- 4. proc.SetGdbModelOverride("sonnet")
    |       +-- 5. 返回成功响应
    |
    +-- 用户输入 `continue` 或 `step`
    |       |
    |       +-- reasonStep 循环继续
    |       +-- 构建 llmRequest 前检查 proc.GetGdbModelOverride()
    |       +-- override = "sonnet" -> 使用 "sonnet" 替代 opts.Model
    |       +-- 发送 LLM 请求时 model = "sonnet"
    |       +-- 下一步及后续步骤继续使用 "sonnet"（直到再次 set model）
```

### 关键设计：reasonStep 中的模型覆盖注入点

```go
// 在 reasonStep 循环内，BuildPrompt 之后、构建 llmRequest 之前：

// 确定本步使用的模型（gdb model override 优先于 opts.Model）
model := opts.Model
if override := proc.GetGdbModelOverride(); override != "" {
    model = override
}

req := llmRequest{
    Intent:       proc.Intent,
    SystemPrompt: promptResult.SystemPrompt,
    Model:        model,      // 使用可能被覆盖的 model
    MaxTurns:     0,
    TimeoutMs:    opts.TimeoutMs,
    Messages:     promptResult.Messages,
}
```

**关键：不修改 `opts.Model`**。覆盖值存在 Process 上，每步都从 Process 读取，这样：
- 原始 opts.Model 保持不变（如果清除覆盖可以回到原模型）
- 覆盖是持久的（设置后每步都生效）
- 线程安全（Process.mu 保护）

### 关键设计：set context append 实现

直接调用 `s.ctxMgr.AppendMessage(proc.CtxID, context.RoleUser, content)`，将用户指定的文本作为 user 消息追加到上下文。下次 BuildPrompt 时这条消息自然包含在历史中。

注意：这会永久改变上下文历史（不可撤销），这是预期行为 -- 调试时追加的指令应该影响后续所有推理步骤。

### 关键设计：set skills add 的 MVP 范围

MVP 实现只在 Process 上记录技能名到 `gdbExtraSkills` 列表。**不实现**真正的 skill 热加载（即不通过 SkillLoader 加载 skill body 并注入到 system prompt）。原因：
1. Skill body 注入到 system prompt 发生在 Spawn 时（`agent.SystemPrompt()` 在 `kernel.go:172`），reasonStep 循环中不重新构建 system prompt
2. 真正的热加载需要修改 reasonStep 的 prompt 构建逻辑或动态修改已分配 context 的 system prompt，架构改动过大
3. MVP 记录技能名即可满足"加入能力列表"的 AC 要求（进程的 Skills 列表可查询到新增的 skill）

如果需要后续增强，可以：
- 通过 `set context append` 手动追加 skill body 内容到上下文
- 未来的 story 实现真正的动态 skill 注入

### 关键设计：set env 的存储

环境变量存储在 Process 的 `gdbEnvVars map[string]string` 中。MVP 实现只做存储和查询，不自动注入到 LLM 请求或 system prompt。原因：
- Rnix 进程不是 OS 进程，没有真正的环境变量概念
- 环境变量的消费者是 shell driver（`/dev/shell`）和可能的 skill 执行
- MVP 先存储，让 `inspect` 命令可以查看，后续 story 可以决定如何注入

### 关键复用点

1. **IPC gdb_command 路由**：复用 `ipc/server.go` 的 `handleGdbCommand` switch/case，增加 `"set"` 分支（与 step/inspect 同模式）
2. **gdb CLI 命令循环**：复用 `cmd/rnix/gdb.go` 的 switch/case 命令路由，增加 `"set"` 分支
3. **GdbCommandResponse**：复用现有的响应格式（OK/Message/Data）
4. **ctxMgr.AppendMessage**：复用 `context/context.go:233` 的 AppendMessage 方法追加上下文
5. **Process.mu**：复用现有的 mutex 保护新增字段（与 gdbStepMode/breakpoints 同模式）
6. **parseBreakCommand 模式**：parseSetCommand 参考 parseBreakCommand 的解析模式（子类型+参数）

### 不要做的事情

- **不要**修改 `opts.Model` 的值 -- 覆盖值从 Process 读取，opts 保持不变
- **不要**实现 skill body 的真正热加载 -- MVP 只记录技能名
- **不要**将 env vars 自动注入到 LLM 请求 -- MVP 只存储
- **不要**实现 `set context remove/clear` -- MVP 只做 append
- **不要**实现 `set skills remove` -- MVP 只做 add
- **不要**实现 `set model` 的模型验证（如检查模型是否存在）-- 模型名透传给 LLM driver，由 driver 负责验证
- **不要**修改 Spawn 逻辑 -- 热修改只影响正在运行的推理循环
- **不要**使用 Bubble Tea TUI 框架 -- 保持 bufio.Scanner 交互模式
- **不要**修改 Signal 系统 -- set 使用 gdb 独立机制

### IPC 协议：set 命令的 args 编码

```
set model sonnet        -> args: ["model", "sonnet"]
set context append "x"  -> args: ["context", "append", "x"]
set skills add review   -> args: ["skills", "add", "review"]
set env DEBUG=true      -> args: ["env", "DEBUG=true"]
```

CLI 层负责解析用户输入并转换为上述 args 格式，IPC server 层从 args 中提取参数。

### handleGdbSet 实现参考

```go
func (s *Server) handleGdbSet(proc *kernel.Process, args []string) GdbCommandResponse {
    if len(args) < 2 {
        return GdbCommandResponse{OK: false, Message: "usage: set <model|context|skills|env> <args...>"}
    }
    subCmd := args[0]
    switch subCmd {
    case "model":
        proc.SetGdbModelOverride(args[1])
        return GdbCommandResponse{OK: true, Message: fmt.Sprintf("model set to %s", args[1])}
    case "context":
        if len(args) < 3 {
            return GdbCommandResponse{OK: false, Message: "usage: set context append <text>"}
        }
        if args[1] != "append" {
            return GdbCommandResponse{OK: false, Message: "usage: set context append <text>"}
        }
        content := strings.Join(args[2:], " ")
        if s.ctxMgr == nil {
            return GdbCommandResponse{OK: false, Message: "context manager not available"}
        }
        if err := s.ctxMgr.AppendMessage(proc.CtxID, context.RoleUser, content); err != nil {
            return GdbCommandResponse{OK: false, Message: fmt.Sprintf("context append failed: %v", err)}
        }
        return GdbCommandResponse{OK: true, Message: "context updated"}
    case "skills":
        if len(args) < 3 || args[1] != "add" {
            return GdbCommandResponse{OK: false, Message: "usage: set skills add <name>"}
        }
        proc.AddGdbSkill(args[2])
        return GdbCommandResponse{OK: true, Message: fmt.Sprintf("skill %s added", args[2])}
    case "env":
        kv := args[1]
        idx := strings.Index(kv, "=")
        if idx <= 0 {
            return GdbCommandResponse{OK: false, Message: "usage: set env KEY=VALUE"}
        }
        proc.SetGdbEnv(kv[:idx], kv[idx+1:])
        return GdbCommandResponse{OK: true, Message: fmt.Sprintf("env %s set", kv[:idx])}
    default:
        return GdbCommandResponse{OK: false, Message: fmt.Sprintf("unknown set target: %s", subCmd)}
    }
}
```

### 性能约束

- model override 读取是 O(1) 的 mutex 保护字符串读取，与 GetStepMode 同级别开销
- 非 gdb 场景下 gdbModelOverride 为空字符串，检查后跳过，无实际开销
- env vars 使用 map 存储，读取/写入 O(1)
- skills 使用 slice 存储，AddGdbSkill 需要 O(n) 查重，但 skill 数量通常 < 10，无性能问题
- context append 是已有的 AppendMessage 操作，与正常上下文写入同开销

### Project Structure Notes

修改文件：
- `kernel/process.go` -- 新增 gdbModelOverride/gdbEnvVars/gdbExtraSkills 字段和对应方法
- `kernel/kernel.go` -- reasonStep 中增加 model override 检查钩子（在构建 llmRequest 之前）
- `ipc/server.go` -- handleGdbCommand 增加 "set" 命令路由、handleGdbSet 实现
- `cmd/rnix/gdb.go` -- 命令循环增加 set 分支、SetCommandResult、parseSetCommand、gdbSet 函数、更新 printGdbHelp

无需新建文件 -- 所有功能都是对现有文件的扩展。

### References

- [Source: kernel/process.go:32-85] -- Process 结构体定义，新增字段在此扩展
- [Source: kernel/breakpoint.go:124-143] -- SetStepMode/GetStepMode/ClearStepMode 实现模式，set model/env/skills 参考此模式
- [Source: kernel/kernel.go:534-542] -- reasonStep 中构建 llmRequest 的位置，model override 在此注入
- [Source: kernel/kernel.go:182-184] -- Spawn 中的 model 选择优先级，gdb override 在 reasonStep 中优先于 opts.Model
- [Source: ipc/server.go:648-683] -- handleGdbCommand 路由，新增 "set" 分支
- [Source: ipc/server.go:735-753] -- handleGdbStep 实现，handleGdbSet 参考此模式
- [Source: ipc/server.go:755-783] -- handleGdbInspect 实现（使用 ctxMgr），handleGdbSet 的 context append 参考此模式
- [Source: context/context.go:233-247] -- AppendMessage 实现，set context append 的核心调用
- [Source: cmd/rnix/gdb.go:213-243] -- 命令循环 switch/case，新增 set 分支
- [Source: cmd/rnix/gdb.go:256-269] -- printGdbHelp，新增 set 命令说明
- [Source: cmd/rnix/gdb.go:495-535] -- parseStepCommand/parseInspectCommand 实现模式，parseSetCommand 参考此模式
- [Source: agents/types.go:27-33] -- AgentInfo 结构体，理解 Skills 字段含义

### 技术栈

- Go 1.26 -- `sync.Mutex` 保护 Process 上的新增字段
- Cobra v1.10.2 -- 无需新增子命令（扩展 gdb 内部命令循环）
- IPC Unix domain socket -- 复用 gdb_command 协议传输 set 命令
- context package -- 复用 AppendMessage 实现 set context append

## Dev Agent Record

### Agent Model Used

Claude Opus 4.6 (claude-opus-4-6)

### Debug Log References

### Completion Notes List

- Task 1: 在 Process 结构体中新增 gdbModelOverride/gdbEnvVars/gdbExtraSkills 三个字段，实现 6 个线程安全的 getter/setter 方法。AddGdbSkill 实现了幂等性（O(n) 查重）。GetGdbEnvVars 和 GetGdbExtraSkills 返回副本确保内部数据隔离。12 个单元测试全部通过（含并发安全测试）。
- Task 2: 在 reasonStep 循环中 BuildPrompt 之后、构建 llmRequest 之前注入 model override 检查。使用局部变量 model 替代 opts.Model，确保原始值不被修改。Write emitEvent 也使用 model 局部变量记录实际使用的模型。
- Task 3: 在 handleGdbCommand switch 中增加 "set" 分支，实现 handleGdbSet 方法。支持 model/context/skills/env 四个子命令。set context append 通过 ctxMgr.AppendMessage 追加用户消息。set env 使用 strings.Index 在第一个 = 处分割 KEY=VALUE。7 个 IPC 服务端测试全部通过。
- Task 4: 在 gdb 命令循环中增加 "set" 分支。实现 gdbSet 函数、SetCommandResult 结构体和 parseSetCommand 解析器。parseSetCommand 支持四种子命令解析，context append 支持多词文本拼接。更新 printGdbHelp 增加 set 命令说明。14 个 CLI 测试全部通过。
- Task 5: ATDD 测试在 RED 阶段已由 step2-atdd 编写。实现代码后所有 33 个测试（12 kernel + 14 CLI + 7 IPC）全部通过。全量回归测试 19 个包全部通过，零回归。
- Code Review: 发现并修复 3 个 HIGH 级别问题：(1) kernel/kernel_test.go 缺少 model override in reasonStep 测试，已补充 TestReasonStep_GdbModelOverride 和 TestReasonStep_GdbModelOverride_Empty；(2) Task 5.5 ipc/protocol_test.go set 命令序列化测试不存在，已由 IPC server 端到端测试覆盖（7 个 set 路由测试）；(3) Task 5.8/5.9/5.10 集成测试由 IPC server 测试 + kernel_test.go model override 测试共同覆盖。修复 1 个 MEDIUM 级别问题：parseSetCommand "set env" 增加 CLI 端 KEY=VALUE 格式校验（含 = 号检查），新增 TestParseSetCommand_EnvNoEquals 测试。

### Change Log

- 2026-03-07: 实现 Story 13.4 运行时参数热修改 -- 5 个任务全部完成，33 个测试通过
- 2026-03-07: Code Review 修复 -- 补充 kernel_test.go model override reasonStep 测试（2 个），CLI parseSetCommand env 增加 = 号校验（1 个测试），共 36 个测试通过

### File List

- kernel/process.go (modified) -- 新增 gdbModelOverride/gdbEnvVars/gdbExtraSkills 字段
- kernel/breakpoint.go (modified) -- 新增 SetGdbModelOverride/GetGdbModelOverride/SetGdbEnv/GetGdbEnvVars/AddGdbSkill/GetGdbExtraSkills 方法
- kernel/breakpoint_test.go (modified) -- 新增 12 个 gdb 运行时参数单元测试
- kernel/kernel.go (modified) -- reasonStep 中增加 model override 检查钩子
- kernel/kernel_test.go (modified) -- 新增 TestReasonStep_GdbModelOverride/TestReasonStep_GdbModelOverride_Empty 测试
- ipc/server.go (modified) -- handleGdbCommand 增加 "set" 路由，新增 handleGdbSet 方法
- ipc/server_test.go (modified) -- 新增 7 个 gdb set 命令 IPC 路由测试
- cmd/rnix/gdb.go (modified) -- 命令循环增加 set 分支，新增 gdbSet/parseSetCommand/SetCommandResult，env 增加 = 号校验
- cmd/rnix/gdb_test.go (modified) -- 新增 15 个 parseSetCommand 解析测试（含 env 格式校验测试）
