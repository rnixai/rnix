# Story 18.5: 模块化与脚本执行

Status: ready-for-dev

## Story

As a 应用开发者,
I want 通过 `source` 导入其他脚本并通过 `rnix run` 执行脚本文件,
So that 我可以模块化组织脚本并直接运行。

## Acceptance Criteria

1. **Given** 脚本包含 `source ./lib/helpers.ash`
   **When** 脚本执行
   **Then** helpers.ash 中定义的函数和变量在当前脚本中可用

2. **Given** 一个 AgentShell 脚本文件 `deploy.ash`
   **When** 用户执行 `rnix run deploy.ash`
   **Then** 脚本按顺序执行，实时输出 spawn 进度，结束时显示汇总

3. **Given** 脚本首行为 `#!/usr/bin/env rnix run`
   **When** 用户直接执行 `./deploy.ash`（已 chmod +x）
   **Then** 脚本通过 shebang 正确执行

4. **Given** 脚本执行中出现语法错误
   **When** 错误发生
   **Then** 报告脚本名、行号和具体语法问题

5. **Given** `source` 的目标文件不存在
   **When** 脚本执行到 `source` 语句
   **Then** 报告错误：脚本名、行号和文件不存在信息

6. **Given** `source` 形成循环引用（A source B, B source A）
   **When** 脚本解析/执行
   **Then** 报告循环引用错误，而非无限递归

7. **Given** `source` 目标脚本中定义了函数和变量
   **When** 被 source 后
   **Then** 函数可在当前脚本中调用，变量可在 `${var}` 中引用

8. **Given** `rnix run` 脚本带参数 `rnix run deploy.ash --env staging`
   **When** 脚本执行
   **Then** 参数通过环境变量传递给脚本

9. **And** 脚本解析时间 <= 50ms（NFR38，≤ 1000 行脚本含 source 展开）

## Tasks / Subtasks

### Task 1: AST 扩展 — StmtSource（AC: #1, #7）

- [ ] 1.1 新增 `StmtSource StatementKind = "source"`
- [ ] 1.2 新增 `SourceStmt` 结构体：
  ```go
  type SourceStmt struct {
      Path string // source 目标文件路径（原始值，可含变量）
  }
  ```
- [ ] 1.3 扩展 `Statement` 结构体新增字段 `Source *SourceStmt`

### Task 2: 解析器扩展 — source 语句（AC: #1, #4, #5）

- [ ] 2.1 在 `parseBlock` 中，在 builtin 检测之前，新增 `source` 关键字检测：
  ```go
  if strings.HasPrefix(lower, "source ") || strings.HasPrefix(lower, "source\t") {
      stmt, err := parseSourceStatement(trimmed, i)
      if err != nil {
          return nil, 0, err
      }
      stmts = append(stmts, stmt)
      i++
      continue
  }
  ```
- [ ] 2.2 实现 `parseSourceStatement(line string, lineIdx int) (Statement, error)`：
  - 提取 `source` 后面的路径参数（去除 `source ` 前缀，trim 空白）
  - 路径为空 → `fmt.Errorf("line %d: source requires a file path", lineIdx+1)`
  - 路径可以带引号（双引号或单引号）或不带引号
  - 返回 `Statement{Kind: StmtSource, Source: &SourceStmt{Path: path}, Raw: line, Line: lineIdx+1}`

### Task 3: ScriptExecutor 扩展 — source 执行（AC: #1, #5, #6, #7）

- [ ] 3.1 在 `ScriptExecutor` 中新增字段：
  ```go
  type ScriptExecutor struct {
      // ... 现有字段 ...
      fileReader   FileReader       // 文件读取接口（可注入 mock）
      sourceStack  map[string]bool  // 循环引用检测
      scriptDir    string           // 当前脚本所在目录（用于相对路径解析）
  }
  ```
- [ ] 3.2 定义 `FileReader` 接口（可测试性）：
  ```go
  type FileReader interface {
      ReadFile(path string) (string, error)
  }
  ```
- [ ] 3.3 实现默认 `OSFileReader`：
  ```go
  type OSFileReader struct{}
  func (r *OSFileReader) ReadFile(path string) (string, error) {
      data, err := os.ReadFile(path)
      if err != nil {
          return "", err
      }
      return string(data), nil
  }
  ```
- [ ] 3.4 在 `executeBlock` 中新增 `case StmtSource` 分支：
  - 使用 `e.env.ExpandStrict(stmt.Source.Path)` 展开路径中的变量
  - 解析相对路径：相对于 `e.scriptDir`（如果 scriptDir 非空）
  - 循环引用检测：检查绝对路径是否在 `e.sourceStack` 中
  - 调用 `e.fileReader.ReadFile(absPath)` 读取文件内容
  - 文件不存在 → 返回带行号的 error
  - 调用 `stripShebang(content)` 去除可能的 shebang 行
  - 调用 `ParseScript(content)` 解析 sourced 脚本
  - 将 sourced 脚本的函数注册到 `e.functions`
  - 将 `sourceStack[absPath] = true`，设置 `e.scriptDir` 为 sourced 文件目录
  - 调用 `e.executeBlock(ctx, script.Statements, result, stageNum, totalStages)` 执行
  - 执行完毕后恢复 `e.scriptDir`，移除 `sourceStack` 条目
- [ ] 3.5 更新 `NewScriptExecutor` 和新增 `NewScriptExecutorWithReader`：
  ```go
  func NewScriptExecutorWithReader(spawner KernelSpawner, env *Environment, reader FileReader) *ScriptExecutor {
      return &ScriptExecutor{
          spawner:     spawner,
          env:         env,
          captures:    make(map[string]*SpawnResult),
          fileReader:  reader,
          sourceStack: make(map[string]bool),
      }
  }
  ```
  `NewScriptExecutor` 保持向后兼容，使用 `&OSFileReader{}`

### Task 4: Shebang 处理（AC: #3）

- [ ] 4.1 实现 `stripShebang(content string) string`：
  ```go
  func stripShebang(content string) string {
      if strings.HasPrefix(content, "#!") {
          if idx := strings.IndexByte(content, '\n'); idx >= 0 {
              return content[idx+1:]
          }
          return ""
      }
      return content
  }
  ```
- [ ] 4.2 在 `ParseScript` 入口调用 `stripShebang`

### Task 5: `rnix run` 命令（AC: #2, #3, #4, #8）

- [ ] 5.1 新增 `cmd/rnix/run.go`：
  ```go
  var runCmd = &cobra.Command{
      Use:   "run <script.ash>",
      Short: "Execute an AgentShell script file",
      Args:  cobra.MinimumNArgs(1),
      RunE:  runRunCmd,
  }
  ```
- [ ] 5.2 `runRunCmd` 实现：
  - `args[0]` 为脚本文件路径
  - `os.ReadFile(scriptPath)` 读取脚本内容
  - 文件不存在 → 报错退出
  - `stripShebang(content)` 去除 shebang
  - 将剩余 `args[1:]` 通过环境变量传递（`RNIX_ARGS`、`RNIX_ARG_0` ~ `RNIX_ARG_N`）
  - 复用现有 `runScript(renderer, mode, progress, client, scriptContent, start)` 流程
  - `ExecScriptRequest.Env` 中注入 `RNIX_SCRIPT_FILE` = 脚本绝对路径 和 `RNIX_SCRIPT_DIR` = 脚本所在目录
- [ ] 5.3 在 `init()` 中 `rootCmd.AddCommand(runCmd)`
- [ ] 5.4 错误处理：文件不存在、读取失败、解析错误均应包含脚本文件名

### Task 6: IPC 传递脚本目录信息（AC: #1, #2）

- [ ] 6.1 在 `ExecScriptRequest` 中新增可选字段：
  ```go
  type ExecScriptRequest struct {
      Script    string            `json:"script"`
      Env       map[string]string `json:"env,omitempty"`
      ScriptDir string            `json:"script_dir,omitempty"`
  }
  ```
- [ ] 6.2 在 `handleExecScript` 中，如果 `req.ScriptDir` 非空，传递给 `ScriptExecutor`：
  ```go
  executor.SetScriptDir(req.ScriptDir)
  ```
- [ ] 6.3 在 `ScriptExecutor` 新增 `SetScriptDir(dir string)` 方法和 `SetFileReader(r FileReader)` 方法

### Task 7: validateFnCalls 和 countStagesInBlock 扩展（AC: #7）

- [ ] 7.1 在 `validateFnCalls` 中新增 `StmtSource` 分支 — `source` 引入的函数在运行时才可知，跳过验证即可（source 文件的函数在执行时注册）
- [ ] 7.2 在 `countStagesInBlock` 中新增 `StmtSource` 分支 — `source` 本身 count 为 0（不含 spawn），sourced 脚本的 stages 在运行时才可知

### Task 8: 测试（AC: #1-#9）

- [ ] 8.1 `shell/source_test.go` — 解析测试：
  - `TestParseScript_Source_Basic` — `source ./lib/helpers.ash` 正确解析
  - `TestParseScript_Source_QuotedPath` — `source "./lib/my helpers.ash"` 带引号路径
  - `TestParseScript_Source_NoPath` — `source` 无参数 → 解析错误
  - `TestParseScript_Source_InFunction` — 函数内使用 source
  - `TestParseScript_Source_Shebang` — shebang 行被正确跳过

- [ ] 8.2 `shell/source_test.go` — 执行测试：
  - `TestScriptExecutor_Source_FunctionsAvailable` — source 后函数可调用（AC1, AC7）
  - `TestScriptExecutor_Source_VariablesAvailable` — source 后变量可在 `${var}` 中引用（AC7）
  - `TestScriptExecutor_Source_FileNotFound` — 文件不存在报错含行号（AC5）
  - `TestScriptExecutor_Source_CircularDetection` — 循环引用 A→B→A 报错（AC6）
  - `TestScriptExecutor_Source_SelfReference` — 自引用 A→A 报错
  - `TestScriptExecutor_Source_RelativePath` — 相对路径基于 scriptDir 解析
  - `TestScriptExecutor_Source_VariableInPath` — `source "${lib_dir}/helpers.ash"` 路径变量展开
  - `TestScriptExecutor_Source_ParseError` — sourced 文件有语法错误，报告文件名和行号（AC4）
  - `TestScriptExecutor_Source_WithSpawn` — sourced 脚本中含 spawn，正确执行和计数
  - `TestScriptExecutor_Source_ChainedSource` — A source B, B source C（非循环链式 source）
  - `TestScriptExecutor_Source_OverrideFunction` — 后 source 的函数覆盖先前同名函数
  - `TestScriptExecutor_Source_EmptyFile` — source 空文件为 no-op

- [ ] 8.3 `cmd/rnix/run_test.go` — CLI 测试：
  - `TestRunCmd_Registered` — `run` 子命令已注册
  - `TestRunCmd_NoArgs` — 无参数报错
  - `TestRunCmd_FileNotFound` — 文件不存在报错含文件名
  - `TestRunCmd_ShebangStripped` — shebang 行被跳过

- [ ] 8.4 `shell/source_test.go` — 组合测试：
  - `TestScriptExecutor_Source_InForLoop` — for 循环内使用 source
  - `TestScriptExecutor_Source_InIfBlock` — if 块内条件 source
  - `TestScriptExecutor_Source_BeforeParallel` — source 函数后在 parallel 中使用
  - `TestScriptExecutor_Source_WithDataStructures` — source 后使用数组/映射

- [ ] 8.5 竞态测试：`go test -race ./shell/... ./cmd/rnix/...`

## Dev Notes

### 关键架构约束

- **解析器架构**：手写递归下降解析器（Decision 10），不使用 parser generator
- **执行模型**：AST walker 解释执行，瓶颈在 LLM 调用（秒级），解释器开销 ≤ 1ms/次（NFR39）
- **FR102**：用户可以在 AgentShell 脚本中通过 `source <file>` 导入其他脚本文件，实现模块化组织
- **FR105**：用户可以通过 `rnix run <script.ash>` 执行 AgentShell 脚本文件，脚本以 `#!/usr/bin/env rnix run` 作为 shebang 也可直接执行
- **NFR38**：脚本解析时间 ≤ 50ms（≤ 1000 行脚本）
- **NFR39**：循环/函数调用运行时开销（不含 spawn/LLM）≤ 1ms/次

### `source` 语义设计

**`source` 是执行时语句，不是编译时导入。** 与 bash 的 `source` / `.` 语义一致：

1. `source ./helpers.ash` 在执行流中遇到时，读取并执行目标文件
2. 目标文件中的 `export` 变量、函数定义在当前执行环境中生效
3. 目标文件中的 `spawn` 也会实际执行
4. `source` 共享当前 `Environment`（变量空间）和 `functions` 表

**与 import/require 的区别**：
- 不是静态导入——条件内的 `source` 仅在条件成立时执行
- 不是隔离命名空间——sourced 的变量/函数直接进入当前作用域
- 函数名冲突时后定义覆盖先定义（与 bash 一致）

### `source` 路径解析规则

```
source ./lib/helpers.ash      → 相对于当前脚本所在目录
source /absolute/path.ash     → 绝对路径直接使用
source "${LIB_DIR}/utils.ash" → 先变量展开再解析路径
```

- 相对路径基准：`e.scriptDir`（由 `rnix run` 或外层 `source` 设置）
- 如果 `e.scriptDir` 为空（`rnix -i` 内联脚本），相对路径基于当前工作目录
- 路径展开使用 `ExpandStrict`——引用未定义变量会报错

### 循环引用检测

使用 `sourceStack map[string]bool` 追踪当前 source 调用链：

```
source A.ash       → sourceStack = {"/abs/A.ash": true}
  source B.ash     → sourceStack = {"/abs/A.ash": true, "/abs/B.ash": true}
    source A.ash   → 检测到 "/abs/A.ash" 已在 stack → 报错
```

路径必须先 `filepath.Abs()` 转为绝对路径再检测，避免 `./a.ash` 和 `a.ash` 被视为不同文件。

### `rnix run` 命令设计

**实现原则**：复用现有的 `runScript` 流程和 IPC 管道，`rnix run` 仅是文件读取 + 脚本内容发送。

```
rnix run deploy.ash --env staging
         ↓
1. os.ReadFile("deploy.ash") → content
2. stripShebang(content) → scriptContent
3. env = agentshell.NewEnvironmentFromOS().All()
   env["RNIX_SCRIPT_FILE"] = absPath
   env["RNIX_SCRIPT_DIR"] = filepath.Dir(absPath)
   env["RNIX_ARG_0"] = "--env"
   env["RNIX_ARG_1"] = "staging"
   env["RNIX_ARGS"] = "--env staging"
4. ExecScriptRequest{Script: scriptContent, Env: env, ScriptDir: dir}
5. client.ExecScriptAndWatch(req, onEvent) → 复用现有流程
```

**Shebang 处理双重保护**：
- `rnix run` 在客户端 `stripShebang`（发送前去除）
- `ParseScript` 入口也调用 `stripShebang`（防止 `rnix -i` 直接传入带 shebang 的内容）

### parseBlock 中的检测位置

在 `parseBlock` 的关键字检测顺序中，`source` 应在 builtin 之前插入：

```
检测顺序（在 parseBlock 中）:
1. end / else (块结束标记)
2. fn 定义 (仅顶层)
3. return (仅块内)
4. if 块
5. parallel 块
6. for 块
7. while 块
8. source 语句      ← 新增
9. builtin (wait/sleep/exit)
10. parseStatement (export/spawn/pipeline/etc.)
```

**理由**：`source` 是单行语句（不是块级结构），放在块级结构之后、builtin 之前。`source` 需要在 `parseStatement` 之前处理，因为 `parseStatement` 会尝试将其解析为 spawn/pipeline。

### FileReader 接口设计

引入 `FileReader` 接口实现可测试性，mock 实现在测试中注入：

```go
// FileReader abstracts file reading for testability.
type FileReader interface {
    ReadFile(path string) (string, error)
}

// OSFileReader reads files from the OS filesystem.
type OSFileReader struct{}

func (r *OSFileReader) ReadFile(path string) (string, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return "", err
    }
    return string(data), nil
}
```

测试中使用 `mockFileReader`：

```go
type mockFileReader struct {
    files map[string]string // path → content
}

func (m *mockFileReader) ReadFile(path string) (string, error) {
    content, ok := m.files[path]
    if !ok {
        return "", &os.PathError{Op: "open", Path: path, Err: os.ErrNotExist}
    }
    return content, nil
}
```

### source 执行中的函数注册

sourced 脚本中的函数定义需要注册到当前执行器的 `functions` 表：

```go
case StmtSource:
    // ... 读取并解析文件 ...
    sourcedScript, err := ParseScript(content)
    // 注册 sourced 脚本的函数到当前函数表
    for name, fn := range sourcedScript.Functions {
        e.functions[name] = fn
    }
    // 执行 sourced 脚本的语句
    if err := e.executeBlock(ctx, sourcedScript.Statements, result, stageNum, totalStages); err != nil {
        return err
    }
```

注意：`ParseScript` 已在顶层调用 `collectFunctions` 将函数定义提取到 `script.Functions`。执行 sourced 语句时，执行块中的 `StmtFnDef` 也会再次注册（冗余但无害）。

### source 的 import 和 os 包依赖

`source` 功能需要 `os` 和 `path/filepath` 包。当前 `script.go` 的 imports 不含这两个包：

```go
import (
    "context"
    "errors"
    "fmt"
    "os"            // ← 新增（OSFileReader）
    "path/filepath" // ← 新增（路径解析）
    "strconv"
    "strings"
    "sync"
    "time"
)
```

或者，将 `OSFileReader` 放在独立文件 `shell/file_reader.go` 中，`script.go` 仅依赖 `FileReader` 接口（无 `os` 导入）。推荐后者，保持 `script.go` 的纯逻辑性。

**推荐文件组织**：
- `shell/file_reader.go` — `FileReader` 接口 + `OSFileReader` 实现（依赖 `os`）
- `shell/script.go` — `StmtSource` + 解析 + 执行（依赖 `FileReader` 接口 + `path/filepath`）

### ExecScriptRequest 向后兼容

新增 `ScriptDir` 字段使用 `omitempty`，旧版客户端不传此字段时服务端行为不变（`scriptDir` 为空字符串，相对路径基于工作目录）。

### 错误消息中包含脚本文件名

`source` 执行失败时，错误消息应包含源文件路径，例如：
```
line 5: source "./helpers.ash": file not found
line 5: source "./helpers.ash": circular reference detected
line 5: source "./helpers.ash": parse error at line 3: unexpected 'else'
```

### IPC Server 中 `source` 的文件读取

IPC Server（`handleExecScript`）在服务端执行脚本，`source` 读取的文件路径相对于 daemon 进程的环境。这与 `rnix run` 通过 `ScriptDir` 传递脚本目录的设计一致——确保 source 路径解析基于原始脚本位置而非 daemon 工作目录。

### 现有代码模式（必须遵循）

**关键字解析模式** — 参考 `parseBlock` 中的 `parallel` / `for` / `while` / `if`：
- 在 `parseBlock` for 循环中通过 `strings.HasPrefix(lower, "keyword ")` 检测
- 调用专门的 `parseXxxStatement` 或 `parseXxxBlock`
- 返回 `Statement` 后 `i++` 或 `i = nextIdx`

**单行语句解析模式** — 参考 `parseBuiltinStatement`：
- 提取关键字后参数
- 返回单个 `Statement`
- 调用方 `i++` 推进行号

**执行模式** — 参考 `executeBlock` 中各 `stmt.Kind` 分支：
- switch on `stmt.Kind` 分发
- 每步检查 `ctx.Err()` 支持取消
- 失败时返回 error

**CLI 子命令模式** — 参考 `cmd/rnix/trace.go`、`cmd/rnix/ctx_profile.go`：
- 独立文件 `run.go`
- `var runCmd = &cobra.Command{...}` 定义
- `func init() { rootCmd.AddCommand(runCmd) }` 注册
- `func runRunCmd(cmd *cobra.Command, args []string) error` 实现
- 连接 daemon、发送 IPC 请求、处理响应

**测试模式** — 参考 `parallel_test.go`（Story 18.4）：
- mock `KernelSpawner` 实现 `SpawnAndWait` + `Wait`
- 验证调用次数、参数、执行顺序
- 测试函数命名 `TestParseScript_*` / `TestScriptExecutor_*`
- Story ID 注释格式：`// 18.5-UNIT-001: ...`

### 与 Story 18.1-18.4 的关系

- Story 18.1 实现了 for/while 循环 + wait/sleep/exit 内置命令
- Story 18.2 实现了函数定义/调用/return + `validateFnCalls`
- Story 18.3 实现了数组/映射数据结构 + 字符串插值 + 内置函数
- Story 18.4 实现了 parallel 块 + spawn 返回值捕获 + 并行执行
- 本 Story 新增 `StmtSource` 和对应的解析/执行逻辑
- 本 Story 新增 `FileReader` 接口和 `OSFileReader` 实现
- 本 Story 新增 `rnix run` CLI 子命令
- 本 Story 新增 `stripShebang` 函数
- 本 Story 在 `ExecScriptRequest` 中新增 `ScriptDir` 字段
- `source` 已在 `reservedKeywords` 中预注册，本 Story 将其从预留变为已实现

### Git Intelligence

最近提交（Story 18.4 完成）：
- `582131b` feat: update traceability matrix for Story 18.4
- `3e43063` feat: cr Story 18.4
- `87ef873` feat: ds 18-4
- `4b87831` feat: add ATDD checklist and unit tests for Story 18.4
- `7603c5c` feat: cs 18-4

Story 18.4 的 code review 修复了 5 个问题：
1. [H1] WhileCondition 测试改为真正使用 while 循环
2. [M1] executeParallel 添加 OnStageStart 回调支持
3. [M2] on-error token 累加断言
4. [L1] 移除死代码 callCount
5. [L2] 更新 Phase 3 过时注释

本 Story 应确保：
- 所有新增解析错误包含行号
- source 错误包含文件路径
- 循环引用检测使用绝对路径
- `FileReader` 接口抽象确保可测试性
- `OSFileReader` 不放在 `script.go` 中（保持 script.go 无 `os` 依赖）
- `stripShebang` 在 `ParseScript` 和 `rnix run` 两处都调用（双重保护）
- `rnix run` 命令遵循现有 cobra 子命令模式
- 无死代码

### 组合矩阵

| 本功能 | 交互对象 | 交互方式 | 需验证 |
|--------|----------|----------|--------|
| source | 函数定义 | sourced 文件中 fn 注册到当前 functions 表 | 是 |
| source | export 变量 | sourced 文件中 export 的变量在当前 env 可用 | 是 |
| source | spawn | sourced 文件中的 spawn 实际执行 | 是 |
| source | parallel | source 后定义的函数可在 parallel 内的 spawn intent 中引用 | 是 |
| source | for 循环 | for 循环内条件 source | 是 |
| source | if 块 | if 块内条件 source | 是 |
| source | 数组/映射 | sourced 文件中定义的数组/映射在当前脚本可用 | 是 |
| source | ExpandStrict | source 路径中 `${var}` 用 strict 展开 | 是 |
| source | 嵌套 source | A source B, B source C（链式非循环） | 是 |
| source | 循环 source | A source B, B source A → 检测报错 | 是 |
| source | shebang | sourced 文件含 shebang → 跳过首行 | 是 |
| source | countStages | source 本身 count 为 0 | 是 |
| rnix run | shebang | shebang 行被正确跳过 | 是 |
| rnix run | RNIX_SCRIPT_DIR | source 路径基于脚本目录解析 | 是 |
| rnix run | ExecScriptAndWatch | 复用现有流程 | 是 |
| rnix run | 参数传递 | args 通过 RNIX_ARG_* 环境变量传递 | 是 |

### 不支持的特性（本 Story 范围外）

- **条件 source**：不支持 `source if-exists ./optional.ash`（可通过 if + source 组合实现）
- **source 返回值**：source 不返回值（与 bash 一致，source 是执行，不是表达式）
- **命名空间隔离**：不支持 `import helpers from "./helpers.ash"`（source 直接污染当前命名空间）
- **glob source**：不支持 `source ./lib/*.ash`（需手动逐个 source）
- **source 搜索路径**：不支持 `RNIX_SOURCE_PATH` 搜索路径（仅支持绝对路径和相对路径）

### Project Structure Notes

改动涉及以下文件：
- `shell/script.go` — AST 类型扩展 + 解析器扩展 + 执行器扩展 + `path/filepath` import
- `shell/file_reader.go` — 新增文件：`FileReader` 接口 + `OSFileReader` 实现
- `shell/source_test.go` — 新增测试文件
- `cmd/rnix/run.go` — 新增文件：`rnix run` 子命令
- `cmd/rnix/run_test.go` — 新增测试文件
- `ipc/protocol.go` — `ExecScriptRequest` 新增 `ScriptDir` 字段
- `ipc/server.go` — `handleExecScript` 传递 `ScriptDir` 到 executor

不涉及 `kernel/`、`vfs/`、`drivers/`、`agents/`、`skills/` 等其他包。

### References

- [Source: _bmad-output/planning-artifacts/epics/epic-18-agentshell完整脚本语言-agentshell-complete-scripting.md#Story 18.5]
- [Source: _bmad-output/planning-artifacts/architecture/core-architectural-decisions.md#Decision 10: AgentShell 解析器架构]
- [Source: _bmad-output/planning-artifacts/architecture/implementation-patterns-consistency-rules.md#AgentShell 语法模式]
- [Source: _bmad-output/planning-artifacts/prd/functional-requirements.md#FR102, FR105]
- [Source: _bmad-output/planning-artifacts/prd/non-functional-requirements.md#NFR38, NFR39]
- [Source: _bmad-output/project-context.md]
- [Source: _bmad-output/implementation-artifacts/18-4-spawn-return-capture-and-parallel-execution.md]
- [Source: shell/script.go — parseBlock/parseStatement/executeBlock/validateFnCalls/countStagesInBlock]
- [Source: shell/pipe.go — KernelSpawner 接口/PipelineExecutor]
- [Source: shell/env.go — Environment/ExpandStrict]
- [Source: cmd/rnix/main.go — runScript/runRoot/isScriptSyntax/ExecScriptRequest]
- [Source: ipc/protocol.go — ExecScriptRequest/ExecScriptResponse/MethodExecScript]
- [Source: ipc/server.go — handleExecScript]
- [Source: ipc/client.go — ExecScriptAndWatch]

## Dev Agent Record

### Agent Model Used

{{agent_model_name_version}}

### Debug Log References

### Completion Notes List

### File List
