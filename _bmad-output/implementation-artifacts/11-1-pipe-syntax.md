# Story 11.1: 管道语法（Pipe Syntax）

Status: review

## Story

As a 用户,
I want 在 AgentShell 中通过管道语法组合智能体执行链,
So that 前一个智能体的输出自动成为后一个的输入。

## Acceptance Criteria

1. **AC1: 双智能体管道**
   - Given `shell/pipe.go` 已实现
   - When 执行 `spawn "分析代码" | spawn "写文档"`
   - Then 系统解析管道语法
   - And Spawn 第一个智能体执行"分析代码"
   - And 其输出通过 Pipe 自动注入第二个智能体"写文档"的上下文（FR66）

2. **AC2: 多级管道链**
   - Given 管道链包含 ≥ 3 个智能体
   - When 执行 `spawn "A" | spawn "B" | spawn "C"`
   - Then 按顺序链式传递，A→B→C

3. **AC3: 管道错误中断**
   - Given 管道中某个智能体失败
   - When 退出非零码
   - Then 下游智能体不启动，管道中断并报告错误位置

## Tasks / Subtasks

- [x] Task 1: Shell 解析器——管道语法词法分析与 AST (AC: #1, #2)
  - [x] 1.1 `shell/parser.go`：定义 `Command` 类型（Type: spawn/unknown, Intent string, Agent string, Model string）
  - [x] 1.2 `shell/parser.go`：定义 `Pipeline` 类型（Commands []Command）
  - [x] 1.3 `shell/parser.go`：`ParsePipeline(input string) (*Pipeline, error)` 解析 `spawn "A" | spawn "B" | spawn "C"` 语法
  - [x] 1.4 `shell/parser.go`：支持引号内含空格的 intent（双引号 `"分析代码"` 和单引号 `'分析代码'`）
  - [x] 1.5 `shell/parser.go`：支持可选 `--agent=X` 和 `--model=Y` 参数
  - [x] 1.6 `shell/parser.go`：非 `spawn` 命令或语法错误时返回清晰的解析错误

- [x] Task 2: 管道执行引擎 (AC: #1, #2, #3)
  - [x] 2.1 `shell/pipe.go`：定义 `PipelineExecutor` 结构体（kernel 接口依赖注入）
  - [x] 2.2 `shell/pipe.go`：定义 `KernelSpawner` 接口（`SpawnAndWait(ctx, intent, agent, model) (result, exitCode, tokensUsed, error)`）用于解耦 kernel 依赖
  - [x] 2.3 `shell/pipe.go`：`Execute(ctx context.Context, pipeline *Pipeline) (*PipelineResult, error)` 主执行函数
  - [x] 2.4 `shell/pipe.go`：执行语义——顺序执行，Agent N 的 Result 作为 `[PIPE_INPUT]` 前缀注入 Agent N+1 的 intent
  - [x] 2.5 `shell/pipe.go`：定义 `PipelineResult`（Stages []StageResult, TotalTokens int, Elapsed time.Duration）
  - [x] 2.6 `shell/pipe.go`：定义 `StageResult`（PID, Intent, Result string, ExitCode int, TokensUsed int, Elapsed time.Duration）
  - [x] 2.7 `shell/pipe.go`：错误语义——某阶段 ExitCode != 0 时，记录失败阶段信息，不启动后续阶段，返回 PipelineResult（含已完成阶段 + 失败阶段）

- [x] Task 3: IPC 协议扩展——管道 Spawn (AC: #1, #2, #3)
  - [x] 3.1 `ipc/protocol.go`：新增 `MethodSpawnPipeline Method = "spawn_pipeline"`
  - [x] 3.2 `ipc/protocol.go`：定义 `SpawnPipelineRequest`（Commands []SpawnPipelineCommand）
  - [x] 3.3 `ipc/protocol.go`：定义 `SpawnPipelineCommand`（Intent, Agent, Model string）
  - [x] 3.4 `ipc/protocol.go`：定义 `SpawnPipelineResponse`（Stages []PipelineStageWire）
  - [x] 3.5 `ipc/protocol.go`：定义 `PipelineStageWire`（PID, Intent, Result, ExitCode, TokensUsed, ElapsedMs）
  - [x] 3.6 `ipc/server.go`：`handleSpawnPipeline`——接收请求，调用 `PipelineExecutor.Execute()`，流式推送每阶段进度
  - [x] 3.7 `ipc/client.go`：`SpawnPipelineAndWatch(req, onEvent)` 客户端方法

- [x] Task 4: CLI 集成 (AC: #1, #2, #3)
  - [x] 4.1 `cmd/crux/main.go`：`runRoot` 中检测 intent 是否包含 `|` 管道语法
  - [x] 4.2 `cmd/crux/main.go`：如果是管道语法，调用 `shell.ParsePipeline()` 解析
  - [x] 4.3 `cmd/crux/main.go`：调用 `client.SpawnPipelineAndWatch()` 执行管道
  - [x] 4.4 `cmd/crux/main.go`：管道进度显示——每阶段显示 stage N/M 进度
  - [x] 4.5 `cmd/crux/main.go`：管道结果输出——最后一个阶段的 Result 作为最终输出
  - [x] 4.6 `cmd/crux/main.go`：管道 JSON 输出——包含所有阶段的 stages 数组
  - [x] 4.7 `cmd/crux/main.go`：管道错误输出——显示失败阶段和位置

- [x] Task 5: 测试 (AC: all)
  - [x] 5.1 `shell/parser_test.go`：单 spawn 命令解析
  - [x] 5.2 `shell/parser_test.go`：双管道解析 `spawn "A" | spawn "B"`
  - [x] 5.3 `shell/parser_test.go`：三管道解析 `spawn "A" | spawn "B" | spawn "C"`
  - [x] 5.4 `shell/parser_test.go`：带 --agent/--model 参数解析
  - [x] 5.5 `shell/parser_test.go`：解析错误（空命令、非 spawn 命令、未闭合引号）
  - [x] 5.6 `shell/pipe_test.go`：双阶段管道执行——mock spawner，验证 PIPE_INPUT 注入
  - [x] 5.7 `shell/pipe_test.go`：三阶段管道执行——A→B→C 链式传递
  - [x] 5.8 `shell/pipe_test.go`：首阶段失败——第二阶段不执行
  - [x] 5.9 `shell/pipe_test.go`：中间阶段失败——后续不执行，前置阶段结果保留
  - [x] 5.10 `shell/pipe_test.go`：context 取消——执行中断
  - [x] 5.11 `ipc/pipeline_test.go`：端到端 IPC 管道 spawn
  - [x] 5.12 `cmd/crux/main_test.go`：管道语法检测逻辑
  - [x] 5.13 `cmd/crux/main_test.go`：回归测试——现有命令注册不受影响

## Dev Notes

### 关键架构决策

#### 管道执行语义：顺序执行模型

AgentShell 管道采用**顺序执行**而非 Unix 并发管道。原因：
- LLM 智能体需要完整输入才能开始推理，流式输入无意义
- 后一个智能体需要前一个的完整 Result 作为上下文
- 错误处理更简单——失败时直接中断

执行流：
```
Stage 1: Spawn("分析代码") → wait → Result("代码分析报告...")
Stage 2: Spawn("[PIPE_INPUT]\n代码分析报告...\n[/PIPE_INPUT]\n写文档") → wait → Result("文档内容...")
Stage 3: ...
```

#### PIPE_INPUT 注入格式

前一阶段的 Result 以结构化标记注入到下一阶段的 intent 前面：

```
[PIPE_INPUT]
{前一阶段的 Result 内容}
[/PIPE_INPUT]

{当前阶段的原始 intent}
```

这种格式让 LLM 能清晰区分管道输入和当前任务指令。

#### 新 `shell/` 包（不是 `drivers/shell/`）

PRD 明确指定 `shell/pipe.go`。`shell/` 是 AgentShell DSL 解析与执行层，与 `drivers/shell/`（宿主 Shell 驱动，执行 `sh -c`）完全不同：
- `shell/` = AgentShell DSL（管道、变量、控制结构）
- `drivers/shell/` = 宿主 Shell 执行驱动（`/dev/shell`）

#### KernelSpawner 接口解耦

`shell/` 包不直接依赖 `kernel/`，通过 `KernelSpawner` 接口注入：

```go
// shell/pipe.go
type KernelSpawner interface {
    SpawnAndWait(ctx context.Context, intent string, agent string, model string) (result string, exitCode int, tokensUsed int, err error)
}
```

在 `ipc/server.go` 中实现此接口，桥接到真实 kernel。测试中使用 mock。

#### IPC 新增 `spawn_pipeline` 方法

不复用现有 `spawn` 方法（单进程），新增 `spawn_pipeline` 专用方法。原因：
- 管道需要多阶段进度流（stage 1/3, stage 2/3 ...）
- 响应结构不同（多个 stage 结果 vs 单个结果）
- 错误语义不同（部分完成 vs 全部失败）

#### CLI 管道检测

在 `runRoot` 中，对 intent 进行简单管道检测：

```go
if strings.Contains(intent, "|") && containsSpawnKeyword(intent) {
    // 走管道路径
    pipeline, err := shell.ParsePipeline(intent)
    ...
}
```

`containsSpawnKeyword` 检查 `|` 两侧是否有 `spawn` 关键字，避免误判普通 intent 中的 `|` 字符。

### 解析器设计

#### spawn 命令语法

```
spawn "intent" [--agent=NAME] [--model=MODEL]
```

- `spawn` 是关键字（大小写不敏感）
- intent 必须用引号包裹（双引号或单引号）
- `--agent` 和 `--model` 是可选命名参数
- 管道用 `|` 分隔，两侧允许空白

#### 解析器实现策略

手写递归下降解析器（不引入外部依赖），实现简单：

1. **Tokenize**：按 `|` 分割为段（注意引号内的 `|` 不分割）
2. **Parse each segment**：提取 `spawn` 关键字、引号内的 intent、命名参数
3. **Validate**：每段必须以 `spawn` 开头，intent 不为空

```go
// shell/parser.go
func ParsePipeline(input string) (*Pipeline, error) {
    segments := splitPipeline(input) // 按 | 分割（忽略引号内）
    var cmds []Command
    for i, seg := range segments {
        cmd, err := parseSpawnCommand(strings.TrimSpace(seg))
        if err != nil {
            return nil, fmt.Errorf("stage %d: %w", i+1, err)
        }
        cmds = append(cmds, cmd)
    }
    if len(cmds) == 0 {
        return nil, fmt.Errorf("empty pipeline")
    }
    return &Pipeline{Commands: cmds}, nil
}
```

### 复用现有代码

**必须复用（不要重新实现）：**
- `kernel/ipc.go`：`Pipe()` syscall——本 story 不直接使用 kernel Pipe（顺序模型），但为 Story 11.2/11.3 保留兼容性
- `ipc/protocol.go`：现有 Request/Response/StreamEvent 框架
- `ipc/server.go`：现有 `handleSpawn` 流程作为参考，`handleSpawnPipeline` 在每个阶段内复用相同的 kernel Spawn → Wait 逻辑
- `ipc/client.go`：现有 `SpawnAndWatch` 流式事件模型作为参考
- `cmd/crux/main.go`：现有 `runRoot` 结构，管道检测在 `runRoot` 内嵌入
- `internal/ui/`：ProgressReporter 用于管道阶段进度显示

**不要修改的现有代码：**
- `kernel/kernel.go` — Spawn 签名不变
- `kernel/ipc.go` — Pipe syscall 不变
- `kernel/process.go` — Process 结构不变
- `drivers/shell/` — 宿主 Shell 驱动不涉及
- `vfs/` — VFS 层不涉及
- `context/` — 上下文管理不涉及
- `compose/` — Compose 独立于 AgentShell

### IPC server 端 handleSpawnPipeline 实现

```go
// ipc/server.go
func (s *Server) handleSpawnPipeline(conn net.Conn, req SpawnPipelineRequest) {
    spawner := &ipcKernelSpawner{kernel: s.kernel, agentLoader: s.agentLoader}
    executor := shell.NewPipelineExecutor(spawner)

    // 对每个 stage，发送进度事件
    result, err := executor.Execute(context.Background(), &shell.Pipeline{...},
        func(stage int, total int, pid types.PID, intent string) {
            // 流式推送 stage 进度
            s.sendStreamEvent(conn, StreamEvent{
                Type: StreamProgress,
                Payload: marshalJSON(ProgressPayload{
                    Event: "pipeline_stage",
                    PID:   pid,
                    Step:  stage,
                    Total: total,
                }),
            })
        })
    // ...发送最终结果
}
```

#### ipcKernelSpawner 适配器

在 `ipc/server.go` 中实现 `shell.KernelSpawner` 接口：

```go
type ipcKernelSpawner struct {
    kernel      *kernel.KernelImpl
    agentLoader *agents.AgentLoader
}

func (s *ipcKernelSpawner) SpawnAndWait(ctx context.Context, intent, agentName, model string) (string, int, int, error) {
    // 1. 加载 agent（如果指定）
    // 2. 调用 k.Spawn(intent, agent, opts)
    // 3. 等待 <-proc.Done
    // 4. 返回 Result, ExitCode, TokensUsed, error
}
```

### 修改文件清单

**新文件：**
- `shell/parser.go` — 管道语法解析器（~120 行）：Pipeline/Command 类型 + ParsePipeline 函数
- `shell/pipe.go` — 管道执行引擎（~150 行）：PipelineExecutor + KernelSpawner 接口 + Execute 函数 + PipelineResult/StageResult 类型
- `shell/parser_test.go` — 解析器测试（~180 行）：5 个测试
- `shell/pipe_test.go` — 执行引擎测试（~200 行）：5 个测试

**修改文件：**
- `ipc/protocol.go` — 新增 MethodSpawnPipeline + SpawnPipelineRequest/Response 类型（~40 行新增）
- `ipc/server.go` — 新增 handleSpawnPipeline + ipcKernelSpawner（~80 行新增）
- `ipc/client.go` — 新增 SpawnPipelineAndWatch 方法（~50 行新增）
- `cmd/crux/main.go` — runRoot 中新增管道检测和管道执行路径（~60 行新增）

### 依赖方向验证

```
shell/ → (无外部依赖，仅标准库)
ipc/   → shell/ (调用 ParsePipeline / PipelineExecutor)
cmd/   → shell/ (调用 ParsePipeline 做管道检测)
cmd/   → ipc/   (调用 SpawnPipelineAndWatch)
```

`shell/` 不依赖 `kernel/`、`vfs/`、`drivers/`——通过 `KernelSpawner` 接口解耦。新增 `shell/` → 所有包可导入（零外部依赖），符合依赖方向规则。

### 测试策略

#### 测试方法

- `shell/` 包：纯单元测试，mock KernelSpawner
- `ipc/`：集成测试，使用 newTestServer + mock kernel（复用现有 IPC 测试 pattern）
- `cmd/`：回归测试，确认现有命令注册不受影响

**mock KernelSpawner 示例：**

```go
type mockSpawner struct {
    results []struct {
        result   string
        exitCode int
        tokens   int
        err      error
    }
    calls []string // 记录接收到的 intent
}

func (m *mockSpawner) SpawnAndWait(ctx context.Context, intent, agent, model string) (string, int, int, error) {
    m.calls = append(m.calls, intent)
    idx := len(m.calls) - 1
    if idx >= len(m.results) {
        return "", 1, 0, fmt.Errorf("unexpected call %d", idx)
    }
    r := m.results[idx]
    return r.result, r.exitCode, r.tokens, r.err
}
```

#### 测试用例分组

| 测试 ID | 类别 | 验证内容 |
|---------|------|---------|
| 11.1-UNIT-001 | P0 | 单 spawn 命令解析 |
| 11.1-UNIT-002 | P0 | 双管道解析 |
| 11.1-UNIT-003 | P0 | 三管道解析 |
| 11.1-UNIT-004 | P1 | 带 --agent/--model 参数解析 |
| 11.1-UNIT-005 | P0 | 解析错误（空/非 spawn/未闭合引号） |
| 11.1-UNIT-006 | P0 | 双阶段管道执行——验证 PIPE_INPUT 注入 |
| 11.1-UNIT-007 | P0 | 三阶段管道执行 |
| 11.1-UNIT-008 | P0 | 首阶段失败——第二阶段不执行 |
| 11.1-UNIT-009 | P0 | 中间阶段失败——后续不执行 |
| 11.1-UNIT-010 | P1 | context 取消 |
| 11.1-INT-001 | P1 | IPC 管道端到端 |
| 11.1-REG-001 | P2 | 现有命令注册回归 |
| 11.1-REG-002 | P2 | 非管道 intent（含 `|`）不误判 |

### 边界情况

- **Intent 中包含 `|` 但不是管道语法**：如 `crux -i "分析 A | B 的差异"`——没有 `spawn` 关键字，走普通 spawn 路径
- **单个 spawn 无管道**：`spawn "分析代码"`——ParsePipeline 返回单命令 Pipeline，Execute 单阶段执行（与普通 spawn 等效）
- **空 intent 段**：`spawn "A" | | spawn "B"`——解析错误，报告空段位置
- **引号内含管道符**：`spawn "分析 A|B"` 不分割——解析器正确处理引号内字符
- **超长管道链**：无硬性上限，但合理限制在 10 级以内（超出提示 warning，不阻断）
- **SIGINT 中断**：管道执行中收到 SIGINT，取消当前阶段，不启动后续阶段
- **Agent/Model 在管道各阶段可不同**：`spawn "分析" --agent=analyst | spawn "写文档" --agent=writer`

### Story 10.5 关键教训

1. **异步 Spawn 检测**：SpawnSupervisor 的子进程启动是异步的，Pipeline 执行中 Spawn 后需要等待进程完成才能获取 Result。使用 `<-proc.Done` 或 IPC `SpawnAndWatch` 模式。
2. **Goroutine 泄漏**：每次 Spawn 都启动 goroutine，Pipeline 中断时需确保已启动进程的 goroutine 被正确清理（通过 Kill + Wait）。
3. **测试 mock 模式**：直接构造 mock 而非依赖真实 kernel，Pipeline 测试只需验证执行逻辑和 PIPE_INPUT 注入，不需要真实 LLM 调用。

### Project Structure Notes

- **新包 `shell/`**：与 `drivers/shell/` 不同，是 AgentShell DSL 层
- 遵循包命名规则：全小写单词
- 测试文件与源文件同目录
- 不引入新的外部依赖（纯标准库）
- `shell/` 包的依赖方向：零外部依赖（只导入标准库），通过接口与 kernel 解耦

### References

- [Source: _bmad-output/planning-artifacts/epics/epic-11-agentshell-高级语法agentshell-advanced-syntax.md#Story 11.1]
- [Source: _bmad-output/planning-artifacts/prd/functional-requirements.md#FR66 管道语法]
- [Source: _bmad-output/planning-artifacts/prd/project-scoping-phased-development.md#AgentShell 管道 shell/pipe.go]
- [Source: _bmad-output/planning-artifacts/prd/success-criteria.md#AgentShell 管道]
- [Source: _bmad-output/planning-artifacts/archive/architecture.md#依赖方向]
- [Source: _bmad-output/project-context.md#Go 语言特定规则]
- [Source: _bmad-output/project-context.md#依赖方向]
- [Source: kernel/ipc.go#Pipe 实现 L339-384]
- [Source: ipc/protocol.go#SpawnRequest + StreamEvent 框架]
- [Source: ipc/server.go#handleSpawn 流程]
- [Source: ipc/client.go#SpawnAndWatch 模式]
- [Source: cmd/crux/main.go#runRoot 入口 L315-414]
- [Source: _bmad-output/implementation-artifacts/10-5-init-bootstrap-sequence.md#异步 Spawn 检测教训]

## Dev Agent Record

### Agent Model Used

Claude claude-4.6-opus (Cursor)

### Debug Log References

无

### Completion Notes List

- 实现 `shell/parser.go`：手写递归下降解析器，按 `|` 分割（尊重引号），逐段解析 spawn 命令。支持单/双引号、`--agent=X`/`--model=Y` 参数、大小写不敏感 spawn 关键字。
- 实现 `shell/pipe.go`：顺序执行管道，`[PIPE_INPUT]` 标记注入上游结果。非零 ExitCode 中断管道返回部分结果，spawner error 直接返回 error，context 取消立即中止。
- 扩展 `ipc/protocol.go`：新增 `MethodSpawnPipeline`、`SpawnPipelineRequest/Command/Response`、`PipelineStageWire` 类型。
- 扩展 `ipc/server.go`：`handleSpawnPipeline` 流式推送阶段进度，`ipcKernelSpawner` 适配器桥接 kernel.Spawn → Wait → Reap 流程。
- 扩展 `ipc/client.go`：`SpawnPipelineAndWatch` 方法，复用 NDJSON 流式事件模型。
- 修改 `cmd/crux/main.go`：`isPipelineSyntax` 检测（需两侧 spawn 关键字），`runPipeline` 管道执行路径，JSON/默认/错误三种输出模式。`drivers/shell` 改为 `drivershell` 别名避免与新 `shell` 包冲突。
- 全部 18 个包测试通过，零回归。shell 包 20 测试全通过（12 parser + 8 pipe），IPC 8 测试通过，CLI 2 测试通过。

### Change Log

- 2026-03-03: Story 11.1 完成——管道语法解析器、执行引擎、IPC 协议、CLI 集成

### File List

**新文件：**
- `shell/parser.go` — 管道语法解析器（Command/Pipeline 类型 + ParsePipeline + splitPipeline + tokenize）
- `shell/pipe.go` — 管道执行引擎（KernelSpawner 接口 + PipelineExecutor + PipelineResult/StageResult）
- `ipc/pipeline_test.go` — IPC 管道协议测试（wire format + server validation）

**修改文件：**
- `ipc/protocol.go` — 新增 MethodSpawnPipeline + SpawnPipelineRequest/Command/Response/PipelineStageWire 类型
- `ipc/server.go` — 新增 handleSpawnPipeline + ipcKernelSpawner 适配器，import context/shell
- `ipc/client.go` — 新增 SpawnPipelineAndWatch 方法
- `cmd/crux/main.go` — 新增 isPipelineSyntax/runPipeline/outputPipelineJSON，drivers/shell 重命名为 drivershell

**已有测试文件（RED→GREEN）：**
- `shell/parser_test.go` — 12 测试全通过
- `shell/pipe_test.go` — 8 测试全通过
- `cmd/crux/main_test.go` — 2 管道检测测试通过 + 全量回归通过
