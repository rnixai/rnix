# Story 11.2: 变量与环境传递（Variables and Environment Passing）

Status: ready-for-dev

## Story

As a 用户,
I want 在 AgentShell 中定义变量和传递环境给智能体,
So that 智能体可以引用动态参数。

## Acceptance Criteria

1. **AC1: export 命令设置变量**
   - Given `shell/script.go` 已实现
   - When 执行 `export TARGET=./src/auth.go`
   - Then 变量 `TARGET` 存储在 shell 环境中

2. **AC2: 变量替换注入 intent**
   - Given 变量已定义
   - When Spawn 的智能体 intent 中引用 `$TARGET`
   - Then 变量值被替换后注入 intent（FR67）

3. **AC3: 标准变量引用语法**
   - Given 多个变量
   - When 在脚本中使用
   - Then 支持标准 `$VAR` 和 `${VAR}` 引用语法

## Tasks / Subtasks

- [ ] Task 1: Shell 环境模型 (AC: #1, #3)
  - [ ] 1.1 `shell/env.go`：`Environment` 结构体（`vars map[string]string`，线程不安全——shell 执行为顺序模型）
  - [ ] 1.2 `shell/env.go`：`NewEnvironment() *Environment` 创建空环境
  - [ ] 1.3 `shell/env.go`：`NewEnvironmentFromOS() *Environment` 从 `os.Environ()` 初始化（可选，允许继承宿主环境变量）
  - [ ] 1.4 `shell/env.go`：`Set(key, value string)` / `Get(key string) (string, bool)` / `Delete(key string)`
  - [ ] 1.5 `shell/env.go`：`Expand(input string) string` 变量展开——支持 `$VAR`、`${VAR}`、`\$` 转义
  - [ ] 1.6 `shell/env.go`：`All() map[string]string` 返回所有变量的快照副本

- [ ] Task 2: 变量展开引擎 (AC: #2, #3)
  - [ ] 2.1 `shell/env.go`：Expand 实现——状态机逐字符扫描，识别 `$` 开头的变量引用
  - [ ] 2.2 `shell/env.go`：`$VAR` 语法——`$` 后连续字母/数字/下划线为变量名（`[A-Za-z_][A-Za-z0-9_]*`）
  - [ ] 2.3 `shell/env.go`：`${VAR}` 语法——花括号包裹的变量名，支持变量名后紧跟其他字符（如 `${VAR}suffix`）
  - [ ] 2.4 `shell/env.go`：`\$` 转义——反斜杠后的 `$` 输出字面 `$`，不展开
  - [ ] 2.5 `shell/env.go`：未定义变量展开为空字符串（bash 默认行为）
  - [ ] 2.6 `shell/env.go`：`$` 在字符串末尾或后跟非变量名字符时保持原样

- [ ] Task 3: 脚本解析器 (AC: #1, #2, #3)
  - [ ] 3.1 `shell/script.go`：定义 `StatementKind` 类型（`"export"` / `"spawn"` / `"pipeline"`）
  - [ ] 3.2 `shell/script.go`：定义 `ExportStmt` 结构体（`Key string`、`Value string`）
  - [ ] 3.3 `shell/script.go`：定义 `Statement` 结构体（`Kind StatementKind`、`Export *ExportStmt`、`Spawn *Command`、`Pipeline *Pipeline`、`Raw string`）
  - [ ] 3.4 `shell/script.go`：定义 `Script` 结构体（`Statements []Statement`）
  - [ ] 3.5 `shell/script.go`：`ParseScript(input string) (*Script, error)` 按行分割，逐行解析为 Statement
  - [ ] 3.6 `shell/script.go`：`parseExport(line string) (Statement, error)` 解析 `export KEY=VALUE` 语法
  - [ ] 3.7 `shell/script.go`：`parseStatement(line string) (Statement, error)` 分派：export / pipeline / spawn
  - [ ] 3.8 `shell/script.go`：空行和 `#` 注释行跳过
  - [ ] 3.9 `shell/script.go`：export 解析支持：引号包裹的值（`export KEY="value with spaces"`）、无引号值、值中包含 `=`

- [ ] Task 4: 脚本执行器 (AC: #1, #2, #3)
  - [ ] 4.1 `shell/script.go`：定义 `ScriptResult`（`LastResult string`、`LastExitCode int`、`TotalTokens int`、`Elapsed time.Duration`）
  - [ ] 4.2 `shell/script.go`：定义 `ScriptExecutor`（`spawner KernelSpawner`、`env *Environment`、`OnStageStart StageCallback`）
  - [ ] 4.3 `shell/script.go`：`NewScriptExecutor(spawner KernelSpawner, env *Environment) *ScriptExecutor`
  - [ ] 4.4 `shell/script.go`：`Execute(ctx context.Context, script *Script) (*ScriptResult, error)` 主执行函数
  - [ ] 4.5 `shell/script.go`：export 语句——展开 Value 中的变量引用后写入 env
  - [ ] 4.6 `shell/script.go`：spawn 语句——展开 Intent 中的变量引用后调用 spawner.SpawnAndWait
  - [ ] 4.7 `shell/script.go`：pipeline 语句——展开每个 Command 的 Intent 后创建 PipelineExecutor 执行
  - [ ] 4.8 `shell/script.go`：非零 ExitCode 中断脚本执行（与管道行为一致）
  - [ ] 4.9 `shell/script.go`：context 取消检查——每条语句执行前检查 `ctx.Err()`

- [ ] Task 5: IPC 协议扩展——脚本执行 (AC: #1, #2)
  - [ ] 5.1 `ipc/protocol.go`：新增 `MethodExecScript Method = "exec_script"`
  - [ ] 5.2 `ipc/protocol.go`：定义 `ExecScriptRequest`（`Script string`、`Env map[string]string`）
  - [ ] 5.3 `ipc/protocol.go`：定义 `ExecScriptResponse`（`LastResult string`、`LastExitCode int`、`TotalTokens int`、`ElapsedMs int64`）
  - [ ] 5.4 `ipc/server.go`：`handleExecScript`——解析脚本、创建 Environment（合并传入 env）、构建 ScriptExecutor、执行并流式推送进度
  - [ ] 5.5 `ipc/client.go`：`ExecScriptAndWatch(req ExecScriptRequest, onEvent func(StreamEvent)) (*ExecScriptResponse, error)`

- [ ] Task 6: CLI 集成 (AC: #1, #2, #3)
  - [ ] 6.1 `cmd/crux/main.go`：`isScriptSyntax(intent string) bool`——检测多行（含 `\n`）或以 `export ` 开头
  - [ ] 6.2 `cmd/crux/main.go`：`runScript(renderer, mode, progress, client, intent, start)` 脚本执行路径
  - [ ] 6.3 `cmd/crux/main.go`：`runRoot` 中在 `isPipelineSyntax` 之前插入 `isScriptSyntax` 检测
  - [ ] 6.4 `cmd/crux/main.go`：脚本结果输出——复用 `outputSuccess` / `outputError` / `outputPipelineJSON`
  - [ ] 6.5 `cmd/crux/main.go`：单行 intent 中的 `$VAR` 展开——从 OS 环境变量展开（无需 export），保持向后兼容

- [ ] Task 7: 测试 (AC: all)
  - [ ] 7.1 `shell/env_test.go`：基本 Set/Get/Delete
  - [ ] 7.2 `shell/env_test.go`：`$VAR` 展开——单变量、多变量、相邻变量
  - [ ] 7.3 `shell/env_test.go`：`${VAR}` 展开——带后缀 `${VAR}suffix`
  - [ ] 7.4 `shell/env_test.go`：`\$` 转义——不展开
  - [ ] 7.5 `shell/env_test.go`：未定义变量展开为空字符串
  - [ ] 7.6 `shell/env_test.go`：`$` 在字符串末尾保持原样
  - [ ] 7.7 `shell/env_test.go`：`NewEnvironmentFromOS` 包含 `PATH` 等系统变量
  - [ ] 7.8 `shell/script_test.go`：解析单行 export
  - [ ] 7.9 `shell/script_test.go`：解析带引号值的 export `export KEY="val"`
  - [ ] 7.10 `shell/script_test.go`：解析多行脚本（export + spawn）
  - [ ] 7.11 `shell/script_test.go`：解析多行脚本（export + pipeline）
  - [ ] 7.12 `shell/script_test.go`：跳过空行和注释行
  - [ ] 7.13 `shell/script_test.go`：解析错误——无效 export 格式
  - [ ] 7.14 `shell/script_test.go`：ScriptExecutor——export 设置变量后 spawn 展开
  - [ ] 7.15 `shell/script_test.go`：ScriptExecutor——pipeline 命令中变量展开
  - [ ] 7.16 `shell/script_test.go`：ScriptExecutor——多次 export 覆盖同名变量
  - [ ] 7.17 `shell/script_test.go`：ScriptExecutor——非零 ExitCode 中断
  - [ ] 7.18 `shell/script_test.go`：ScriptExecutor——context 取消
  - [ ] 7.19 `cmd/crux/main_test.go`：`isScriptSyntax` 检测
  - [ ] 7.20 `cmd/crux/main_test.go`：回归——现有单 spawn 和管道路径不受影响

## Dev Notes

### 关键架构决策

#### Shell 环境模型：进程内 map（非持久化）

AgentShell 的变量环境是**进程内临时状态**：
- 每次 `crux` CLI 调用创建一个新的 Environment
- `export` 设置的变量只在当前脚本执行期间有效
- 不持久化到文件系统，不跨 CLI 调用传递
- 类似 bash 的子 shell：变量只在当前 shell 进程有效

```go
// shell/env.go
type Environment struct {
    vars map[string]string
}
```

线程安全不需要：AgentShell 脚本**顺序执行**（Story 11.1 已确立），一次只有一条语句在执行。

#### 变量展开：状态机扫描器

Expand 采用逐字符状态机实现，不使用正则表达式：

```go
func (e *Environment) Expand(input string) string {
    var buf strings.Builder
    i := 0
    for i < len(input) {
        if input[i] == '\\' && i+1 < len(input) && input[i+1] == '$' {
            buf.WriteByte('$')
            i += 2
            continue
        }
        if input[i] == '$' {
            i++
            if i < len(input) && input[i] == '{' {
                // ${VAR} 语法
                i++
                start := i
                for i < len(input) && input[i] != '}' { i++ }
                if i < len(input) {
                    name := input[start:i]
                    i++ // skip '}'
                    val, _ := e.vars[name]
                    buf.WriteString(val)
                } else {
                    buf.WriteString("${")
                    buf.WriteString(input[start:])
                }
            } else if i < len(input) && isVarStart(input[i]) {
                // $VAR 语法
                start := i
                for i < len(input) && isVarChar(input[i]) { i++ }
                name := input[start:i]
                val, _ := e.vars[name]
                buf.WriteString(val)
            } else {
                buf.WriteByte('$')
            }
            continue
        }
        buf.WriteByte(input[i])
        i++
    }
    return buf.String()
}

func isVarStart(c byte) bool { return c == '_' || (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') }
func isVarChar(c byte) bool  { return isVarStart(c) || (c >= '0' && c <= '9') }
```

不使用正则的原因：
- 性能更好（无正则编译开销）
- 转义处理更直观
- 与 Story 11.1 的 tokenizer 风格一致（手写扫描器）

#### 脚本解析模型：行导向解析

```
// 每行独立解析为一条 Statement
input:
  export TARGET=./src/auth.go
  export OUTPUT=./reports
  spawn "分析 $TARGET" | spawn "写报告到 $OUTPUT"

→ Script{
    Statements: [
      {Kind: "export", Export: {Key: "TARGET", Value: "./src/auth.go"}},
      {Kind: "export", Export: {Key: "OUTPUT", Value: "./reports"}},
      {Kind: "pipeline", Pipeline: &Pipeline{Commands: [
        {Type: "spawn", Intent: "分析 $TARGET"},
        {Type: "spawn", Intent: "写报告到 $OUTPUT"},
      ]}},
    ]
  }
```

行导向解析是因为：
- 每行一条语句（export / spawn / pipeline）
- 与 bash 单行命令模型一致
- 为 Story 11.3 的 `if-else`/`end` 多行控制结构预留扩展点

**Statement 类型设计（为 11.3 预留）**：

```go
type StatementKind string

const (
    StmtExport   StatementKind = "export"
    StmtSpawn    StatementKind = "spawn"
    StmtPipeline StatementKind = "pipeline"
    // Story 11.3 将新增:
    // StmtIf       StatementKind = "if"
    // StmtElse     StatementKind = "else"
    // StmtEnd      StatementKind = "end"
    // StmtOnError  StatementKind = "on-error"
)
```

#### export 语法规则

```
export KEY=VALUE
export KEY="value with spaces"
export KEY='value with spaces'
export KEY=${OTHER_VAR}/suffix
```

- `KEY`：`[A-Za-z_][A-Za-z0-9_]*`（与变量名规则一致）
- `=` 是分隔符，前后不允许空格（`KEY = VALUE` 是无效的，与 bash 一致）
- `VALUE` 可选引号包裹，引号内允许空格
- `VALUE` 中的 `$VAR` 在**执行时**展开（不是解析时），支持引用先前定义的变量
- export 大小写不敏感（`Export`、`EXPORT` 均可识别）

#### 脚本执行语义：顺序执行 + 变量展开

执行流（以完整脚本为例）：

```
# 用户输入脚本
export TARGET=./src/auth.go
export REPORT_DIR=./reports
spawn "分析 $TARGET" --agent=code-analyst
spawn "写报告" | spawn "保存到 $REPORT_DIR"

# 执行步骤
Step 1: export TARGET=./src/auth.go     → env["TARGET"] = "./src/auth.go"
Step 2: export REPORT_DIR=./reports     → env["REPORT_DIR"] = "./reports"
Step 3: spawn "分析 $TARGET"            → expand → spawn "分析 ./src/auth.go" → SpawnAndWait
Step 4: pipeline                        → expand → spawn "写报告" | spawn "保存到 ./reports" → PipelineExecutor
```

- 变量展开在**调用 spawner 之前**发生
- export 的 Value 也支持变量展开：`export FULL=$BASE/file.go`
- pipeline 内每个 Command 的 Intent 独立展开
- 非零 ExitCode 中断脚本（不执行后续语句）

#### IPC 协议扩展：exec_script 方法

新增 `exec_script` IPC 方法，将脚本发送到 daemon 端执行：

```go
MethodExecScript Method = "exec_script"

type ExecScriptRequest struct {
    Script string            `json:"script"`
    Env    map[string]string `json:"env,omitempty"`
}
```

- `Script`：完整脚本文本（多行）
- `Env`：初始环境变量（CLI 可从 OS env 注入）
- daemon 端解析脚本 → 创建 Environment（合并 Env）→ ScriptExecutor 执行
- 流式推送每个 spawn/pipeline 阶段的进度

`handleExecScript` 实现参考 `handleSpawnPipeline` 模式：

```go
func (s *Server) handleExecScript(conn net.Conn, req ExecScriptRequest) {
    script, err := shell.ParseScript(req.Script)
    if err != nil {
        sendError(conn, err)
        return
    }

    env := shell.NewEnvironment()
    for k, v := range req.Env {
        env.Set(k, v)
    }

    spawner := &ipcKernelSpawner{kernel: s.kernel, agentLoader: s.agentLoader}
    executor := shell.NewScriptExecutor(spawner, env)
    executor.OnStageStart = func(stage, total int, intent string) {
        s.sendStreamEvent(conn, StreamEvent{
            Type: StreamProgress,
            Payload: marshalJSON(ProgressPayload{
                Event: "script_step",
                Step:  stage,
                Total: total,
            }),
        })
    }

    result, err := executor.Execute(context.Background(), script)
    // ... send StreamComplete with ExecScriptResponse
}
```

#### CLI 脚本检测与执行

在 `runRoot` 中的检测优先级：

```go
// 1. 脚本检测（最高优先级——包含 export 或多行）
if isScriptSyntax(intent) {
    runScript(renderer, mode, progress, client, intent, start)
    return nil
}
// 2. 管道检测
if isPipelineSyntax(intent) {
    runPipeline(renderer, mode, progress, client, intent, start)
    return nil
}
// 3. 单 spawn（默认）
```

`isScriptSyntax` 检测：
```go
func isScriptSyntax(intent string) bool {
    // 多行脚本
    if strings.Contains(intent, "\n") {
        return true
    }
    // 单行 export
    trimmed := strings.TrimSpace(strings.ToLower(intent))
    return strings.HasPrefix(trimmed, "export ")
}
```

#### 单行 intent 中的 $VAR 展开

对于非脚本的单行 intent（如 `crux -i 'spawn "分析 $HOME/code"'`），从 OS 环境变量展开：

```go
// 在 runRoot 中，对非脚本、非管道的单行 intent
if containsVarRef(intent) {
    env := shell.NewEnvironmentFromOS()
    intent = env.Expand(intent)
}
```

这使得用户可以在 intent 中直接引用 OS 环境变量（如 `$HOME`、`$USER`），无需 export。

### 复用现有代码

**必须复用（不要重新实现）：**
- `shell/parser.go`：`ParsePipeline` + `Command` / `Pipeline` 类型——脚本解析器在检测到管道语法时复用现有管道解析
- `shell/parser.go`：`parseSpawnCommand` + `tokenize`——脚本中的 spawn 语句复用现有解析逻辑
- `shell/pipe.go`：`PipelineExecutor` + `KernelSpawner` 接口——脚本执行器中的 pipeline 语句复用现有管道执行引擎
- `shell/pipe.go`：`StageCallback`——脚本执行器复用同一回调类型
- `ipc/protocol.go`：`StreamEvent` / `StreamProgress` / `ProgressPayload` 框架——exec_script 复用现有流式事件模型
- `ipc/server.go`：`ipcKernelSpawner`——exec_script handler 复用同一个 spawner 适配器
- `ipc/client.go`：NDJSON 流式读取模式——ExecScriptAndWatch 复用 SpawnAndWatch 的事件循环实现
- `cmd/crux/main.go`：`outputSuccess` / `outputError` / `outputPipelineJSON`——脚本结果输出

**不要修改的现有代码：**
- `shell/parser.go` — 现有 ParsePipeline/parseSpawnCommand/tokenize 签名不变
- `shell/pipe.go` — PipelineExecutor/KernelSpawner 签名不变
- `kernel/` — 内核层不涉及（变量是 shell 层概念）
- `vfs/` — VFS 层不涉及
- `context/` — 上下文管理不涉及
- `drivers/` — 驱动层不涉及
- `compose/` — Compose 独立于 AgentShell

### 修改文件清单

**新文件：**
- `shell/env.go` — 环境变量模型 + 展开引擎（~80 行）：Environment struct + NewEnvironment + NewEnvironmentFromOS + Set/Get/Delete/All + Expand
- `shell/script.go` — 脚本解析器 + 执行器（~180 行）：StatementKind/ExportStmt/Statement/Script 类型 + ParseScript + ScriptExecutor + Execute

**修改文件：**
- `ipc/protocol.go` — 新增 MethodExecScript + ExecScriptRequest/Response 类型（~25 行新增）
- `ipc/server.go` — 新增 handleExecScript（~50 行新增）
- `ipc/client.go` — 新增 ExecScriptAndWatch 方法（~45 行新增）
- `cmd/crux/main.go` — 新增 isScriptSyntax/runScript + 单行 $VAR 展开（~50 行新增）

### 依赖方向验证

```
shell/ → (无外部依赖，仅标准库 + os 包)
  env.go     imports: strings, os
  script.go  imports: context, time, strings + 同包 parser.go/pipe.go

ipc/   → shell/ (调用 ParseScript / ScriptExecutor)
cmd/   → shell/ (调用 isScriptSyntax/NewEnvironmentFromOS/Expand)
cmd/   → ipc/   (调用 ExecScriptAndWatch)
```

`shell/` 不依赖 `kernel/`、`vfs/`、`drivers/`——通过 `KernelSpawner` 接口解耦。符合现有依赖方向规则。

### Project Structure Notes

- `shell/env.go` 和 `shell/script.go` 在 `shell/` 包内，与 `parser.go`/`pipe.go` 同级
- `shell/` 包是 AgentShell DSL 层（与 `drivers/shell/` 宿主 Shell 驱动不同）
- 测试文件 `env_test.go`、`script_test.go` 与源文件同目录
- 不引入新的外部依赖（仅标准库 `os`、`strings`、`context`、`time`、`fmt`）

### 测试策略

#### 测试方法

- `shell/env_test.go`：纯单元测试，验证 Environment Set/Get 和 Expand 展开逻辑
- `shell/script_test.go`：单元测试，mock KernelSpawner（复用 11-1 的 mockSpawner 模式），验证解析和执行
- `ipc/`：集成测试，复用 newTestServer + mock kernel 模式
- `cmd/`：回归测试，确认 isScriptSyntax 检测逻辑 + 现有路径不受影响

**复用 Story 11.1 的 mockSpawner 模式：**

```go
type mockSpawner struct {
    results []struct {
        result   string
        exitCode int
        tokens   int
        err      error
    }
    calls []string
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
| 11.2-UNIT-001 | P0 | Environment Set/Get 基本操作 |
| 11.2-UNIT-002 | P0 | Environment Delete |
| 11.2-UNIT-003 | P0 | Expand `$VAR` 单变量 |
| 11.2-UNIT-004 | P0 | Expand `$VAR` 多变量 |
| 11.2-UNIT-005 | P0 | Expand `${VAR}` 花括号语法 |
| 11.2-UNIT-006 | P0 | Expand `${VAR}suffix` 带后缀 |
| 11.2-UNIT-007 | P0 | Expand `\$` 转义 |
| 11.2-UNIT-008 | P1 | Expand 未定义变量 → 空字符串 |
| 11.2-UNIT-009 | P1 | Expand `$` 在末尾 → 保持原样 |
| 11.2-UNIT-010 | P1 | NewEnvironmentFromOS 包含系统变量 |
| 11.2-UNIT-011 | P0 | ParseScript 单行 export |
| 11.2-UNIT-012 | P0 | ParseScript 带引号值 export |
| 11.2-UNIT-013 | P0 | ParseScript 多行（export + spawn） |
| 11.2-UNIT-014 | P0 | ParseScript 多行（export + pipeline） |
| 11.2-UNIT-015 | P1 | ParseScript 跳过空行和注释 |
| 11.2-UNIT-016 | P0 | ParseScript 无效 export 格式 |
| 11.2-UNIT-017 | P0 | ScriptExecutor export + spawn 变量展开 |
| 11.2-UNIT-018 | P0 | ScriptExecutor pipeline 变量展开 |
| 11.2-UNIT-019 | P0 | ScriptExecutor export 覆盖同名变量 |
| 11.2-UNIT-020 | P0 | ScriptExecutor 非零 ExitCode 中断 |
| 11.2-UNIT-021 | P1 | ScriptExecutor context 取消 |
| 11.2-REG-001 | P1 | isScriptSyntax 检测 |
| 11.2-REG-002 | P2 | 现有单 spawn/管道路径回归 |

### 边界情况

- **空 export 值**：`export KEY=`——合法，设置 KEY 为空字符串
- **export 值含等号**：`export CONFIG=a=b=c`——第一个 `=` 为分隔符，Value = `a=b=c`
- **变量名大小写**：区分大小写（`$target` 和 `$TARGET` 是不同变量，与 bash 一致）
- **循环引用**：`export A=$B` + `export B=$A`——不做循环检测，展开时取当前值（bash 行为）
- **export 后无赋值**：`export KEY`（没有 `=`）——解析错误，必须有 `=`
- **Intent 中的 `$` 不是变量引用**：`spawn "价格是 $100"`——`$1` 是变量引用（未定义→空），`00` 是文字。用 `\$` 转义可避免
- **嵌套花括号**：`${$VAR}` 不支持嵌套——外层 `${` 和 `}` 匹配，内部 `$VAR` 作为字面变量名
- **单行 export（无 spawn）**：合法，执行后变量设置但无实际 spawn——用于脚本调试
- **引号内的变量**：`export KEY="$OTHER"` 中 `$OTHER` 在**执行时**展开（解析时保留原始值）
- **管道中变量引用**：`spawn "分析 $A" | spawn "处理 $B"` 中 `$A` 和 `$B` 各自独立展开
- **isScriptSyntax 与 isPipelineSyntax 优先级**：`export A=1\nspawn "X" | spawn "Y"` 是脚本（含 export），不是管道。脚本检测优先

### Story 11.1 关键教训

1. **KernelSpawner 接口解耦有效**：shell/ 包不直接依赖 kernel/，通过接口注入 spawner。Script 执行器继续使用此模式。
2. **手写扫描器优于正则**：tokenizer 和 splitPipeline 都用手写状态机，清晰且高效。变量展开器继续此风格。
3. **CLI 导入别名**：`cmd/crux/main.go` 中 `agentshell "github.com/gonewx/crux/shell"` 别名避免与 `drivershell` 冲突。新增代码继续使用此别名。
4. **ipcKernelSpawner 复用**：ipc/server.go 中的 `ipcKernelSpawner` 已经实现 `KernelSpawner` 接口，handleExecScript 直接复用。
5. **流式进度推送模式**：StreamProgress + ProgressPayload 框架已建立，脚本执行进度复用同一模式。

### References

- [Source: _bmad-output/planning-artifacts/epics/epic-11-agentshell-高级语法agentshell-advanced-syntax.md#Story 11.2]
- [Source: _bmad-output/planning-artifacts/prd/functional-requirements.md#FR67 变量与环境传递]
- [Source: _bmad-output/planning-artifacts/prd/project-scoping-phased-development.md#AgentShell 脚本 shell/script.go]
- [Source: _bmad-output/project-context.md#Go 语言特定规则]
- [Source: _bmad-output/project-context.md#依赖方向]
- [Source: shell/parser.go#ParsePipeline + tokenize]
- [Source: shell/pipe.go#KernelSpawner 接口 + PipelineExecutor]
- [Source: ipc/protocol.go#SpawnPipelineRequest 框架]
- [Source: ipc/server.go#handleSpawnPipeline + ipcKernelSpawner]
- [Source: ipc/client.go#SpawnPipelineAndWatch 模式]
- [Source: cmd/crux/main.go#isPipelineSyntax + runPipeline]
- [Source: _bmad-output/implementation-artifacts/11-1-pipe-syntax.md#KernelSpawner 接口解耦]
- [Source: _bmad-output/implementation-artifacts/11-1-pipe-syntax.md#手写递归下降解析器]

## Dev Agent Record

### Agent Model Used

{{agent_model_name_version}}

### Debug Log References

### Completion Notes List

### Change Log

### File List
