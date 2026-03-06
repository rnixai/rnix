# Story 11.3: 最小控制结构（Minimal Control Structures）

Status: done

## Story

As a 用户,
I want 在 AgentShell 中使用 if-else 和 on-error 编排执行流程,
So that 智能体工作流可以有条件分支和错误处理。

## Acceptance Criteria

1. **AC1: if/else/end 条件分支**
   - Given `shell/script.go` 中控制结构已实现
   - When 执行多行脚本：
     ```
     result = spawn "分析代码"
     if $result.exitcode == 0
       spawn "生成报告"
     else
       spawn "记录失败原因"
     end
     ```
   - Then 按条件分支正确执行（FR68）

2. **AC2: on-error 内联错误处理**
   - Given 使用 on-error
   - When 执行 `spawn "危险操作" on-error spawn "回滚"`
   - Then "危险操作"失败时自动执行"回滚"

3. **AC3: 嵌套控制结构**
   - Given 嵌套 if 块
   - When 超过 1 层嵌套
   - Then 正确执行（完整脚本语言能力推迟至 Phase 3）

## Tasks / Subtasks

- [x] Task 1: 新增类型与扩展 Statement (AC: #1, #2, #3)
  - [x] 1.1 `shell/script.go`：新增 `StmtIf StatementKind = "if"`
  - [x] 1.2 `shell/script.go`：定义 `Condition` 结构体（`VarName string`、`Property string`、`Operator string`、`Value string`）
  - [x] 1.3 `shell/script.go`：定义 `IfBlock` 结构体（`Condition Condition`、`Then []Statement`、`Else []Statement`）
  - [x] 1.4 `shell/script.go`：定义 `SpawnResult` 结构体（`ExitCode int`、`Result string`、`Tokens int`）
  - [x] 1.5 `shell/script.go`：扩展 `Statement` 结构体——新增 `If *IfBlock`、`Assign string`、`OnError *Command`

- [x] Task 2: 递归下降解析器 (AC: #1, #3)
  - [x] 2.1 `shell/script.go`：`parseBlock(lines []string, startIdx int, insideIf bool) ([]Statement, int, error)` 递归块解析
  - [x] 2.2 `shell/script.go`：`parseIfBlock(lines []string, ifLineIdx int) (*IfBlock, int, error)` if/else/end 块解析
  - [x] 2.3 `shell/script.go`：`parseCondition(s string) (*Condition, error)` 条件表达式解析——支持 `$VAR.PROP OP VALUE` 和 `$VAR OP VALUE`
  - [x] 2.4 `shell/script.go`：重构 `ParseScript` 调用 `parseBlock` 替代原有扁平解析
  - [x] 2.5 `shell/script.go`：验证所有现有 ParseScript 测试仍通过（无块结构时行为完全兼容）

- [x] Task 3: 赋值与 on-error 解析 (AC: #1, #2)
  - [x] 3.1 `shell/script.go`：`isAssignment(line string) (varName, rest string, ok bool)` 检测 `VAR = spawn ...` 语法
  - [x] 3.2 `shell/script.go`：`splitOnError(line string) (main, handler string, found bool)` 检测未引号包裹的 `on-error` 关键字
  - [x] 3.3 `shell/script.go`：更新 `parseStatement` 分派顺序——export → assignment → if → on-error split → pipeline → spawn

- [x] Task 4: 条件求值与块执行 (AC: #1, #2, #3)
  - [x] 4.1 `shell/script.go`：`ScriptExecutor` 新增 `captures map[string]*SpawnResult` 字段
  - [x] 4.2 `shell/script.go`：`NewScriptExecutor` 初始化 captures map
  - [x] 4.3 `shell/script.go`：`evalCondition(cond *Condition) (bool, error)` 条件求值——属性访问从 captures 查找，普通变量从 env 查找
  - [x] 4.4 `shell/script.go`：`executeBlock(ctx, stmts, result, stageNum, totalStages) error` 递归块执行
  - [x] 4.5 `shell/script.go`：重构 `Execute` 调用 `executeBlock` 替代原有扁平循环
  - [x] 4.6 `shell/script.go`：赋值 spawn 执行——结果存入 captures + env.Set（文本输出）
  - [x] 4.7 `shell/script.go`：on-error 执行——主命令失败时执行 handler，结果替代主命令结果
  - [x] 4.8 `shell/script.go`：非零 ExitCode 中断语义——赋值 spawn 不中断，on-error handler 决定是否继续，普通 spawn 中断
  - [x] 4.9 `shell/script.go`：更新 `countExecutableStages` 递归计算（遍历 Then/Else 分支）

- [x] Task 5: CLI 脚本检测更新 (AC: #2)
  - [x] 5.1 `cmd/rnix/main.go`：`isScriptSyntax` 新增 `on-error` 关键字检测（单行 on-error 脚本需路由到 exec_script）

- [x] Task 6: 测试 (AC: all)
  - [x] 6.1 `shell/script_test.go`：ParseScript if/else/end 基本解析
  - [x] 6.2 `shell/script_test.go`：ParseScript if（无 else）解析
  - [x] 6.3 `shell/script_test.go`：ParseScript 嵌套 if 解析
  - [x] 6.4 `shell/script_test.go`：ParseScript 条件解析——`$VAR.PROP == VALUE`
  - [x] 6.5 `shell/script_test.go`：ParseScript 条件解析——`$VAR == VALUE`（普通变量）
  - [x] 6.6 `shell/script_test.go`：ParseScript 赋值 spawn 解析——`result = spawn "..."`
  - [x] 6.7 `shell/script_test.go`：ParseScript on-error 解析——`spawn "A" on-error spawn "B"`
  - [x] 6.8 `shell/script_test.go`：ParseScript 赋值 + on-error 组合——`result = spawn "A" on-error spawn "B"`
  - [x] 6.9 `shell/script_test.go`：ParseScript 解析错误——未闭合 if 块
  - [x] 6.10 `shell/script_test.go`：ParseScript 解析错误——else/end 在 if 块外
  - [x] 6.11 `shell/script_test.go`：ParseScript 解析错误——无效条件
  - [x] 6.12 `shell/script_test.go`：ScriptExecutor if 分支——exitcode == 0 走 then
  - [x] 6.13 `shell/script_test.go`：ScriptExecutor if 分支——exitcode != 0 走 else
  - [x] 6.14 `shell/script_test.go`：ScriptExecutor if（无 else）——条件不满足跳过
  - [x] 6.15 `shell/script_test.go`：ScriptExecutor 嵌套 if 正确执行
  - [x] 6.16 `shell/script_test.go`：ScriptExecutor 赋值 spawn——captures 存储且不中断
  - [x] 6.17 `shell/script_test.go`：ScriptExecutor 赋值 spawn 文本输出可在后续 intent 展开（$result）
  - [x] 6.18 `shell/script_test.go`：ScriptExecutor on-error——主命令失败触发 handler
  - [x] 6.19 `shell/script_test.go`：ScriptExecutor on-error——主命令成功跳过 handler
  - [x] 6.20 `shell/script_test.go`：ScriptExecutor on-error handler 成功→脚本继续
  - [x] 6.21 `shell/script_test.go`：ScriptExecutor on-error handler 失败→脚本中断
  - [x] 6.22 `shell/script_test.go`：ScriptExecutor 条件引用 env 普通变量
  - [x] 6.23 `shell/script_test.go`：ScriptExecutor 条件 `!=` 操作符
  - [x] 6.24 `shell/script_test.go`：回归——所有现有 11.2 测试不受影响
  - [x] 6.25 `cmd/rnix/main_test.go`：`isScriptSyntax` 检测 on-error

## Dev Notes

### 关键架构决策

#### 从扁平解析到递归下降

现有 `ParseScript` 是扁平逐行解析：每行独立 `parseStatement`，返回平坦的 `[]Statement`。

控制结构需要**块结构**（if body/else body 是嵌套语句列表），因此解析器重构为递归下降：

```go
// 重构后的 ParseScript 入口
func ParseScript(input string) (*Script, error) {
    lines := strings.Split(input, "\n")
    stmts, nextIdx, err := parseBlock(lines, 0, false)
    if err != nil {
        return nil, err
    }
    // 检查剩余行是否全为空行/注释
    for i := nextIdx; i < len(lines); i++ {
        trimmed := strings.TrimSpace(lines[i])
        if trimmed != "" && !strings.HasPrefix(trimmed, "#") {
            return nil, fmt.Errorf("line %d: unexpected statement after block end", i+1)
        }
    }
    return &Script{Statements: stmts}, nil
}

func parseBlock(lines []string, startIdx int, insideIf bool) ([]Statement, int, error) {
    var stmts []Statement
    i := startIdx
    for i < len(lines) {
        trimmed := strings.TrimSpace(lines[i])
        if trimmed == "" || strings.HasPrefix(trimmed, "#") {
            i++
            continue
        }
        lower := strings.ToLower(trimmed)

        // 块终止符——只在 if 块内合法
        if lower == "else" || lower == "end" {
            if !insideIf {
                return nil, 0, fmt.Errorf("line %d: unexpected %q outside if block", i+1, trimmed)
            }
            return stmts, i, nil
        }

        // if 块——递归解析
        if strings.HasPrefix(lower, "if ") || strings.HasPrefix(lower, "if\t") {
            ifBlock, nextIdx, err := parseIfBlock(lines, i)
            if err != nil {
                return nil, 0, err
            }
            stmts = append(stmts, Statement{Kind: StmtIf, If: ifBlock, Raw: trimmed})
            i = nextIdx
            continue
        }

        // 普通语句（export / assignment / on-error / pipeline / spawn）
        stmt, err := parseStatement(trimmed)
        if err != nil {
            return nil, 0, fmt.Errorf("line %d: %w", i+1, err)
        }
        stmts = append(stmts, stmt)
        i++
    }
    return stmts, i, nil
}
```

无控制结构时，`parseBlock` 行为与原 `ParseScript` 完全等价——`insideIf=false` 时永远不会遇到 `else`/`end`，直接逐行解析。**所有 11.2 测试零修改通过**。

#### if/else/end 块解析

```go
func parseIfBlock(lines []string, ifLineIdx int) (*IfBlock, int, error) {
    ifLine := strings.TrimSpace(lines[ifLineIdx])
    condStr := strings.TrimSpace(ifLine[2:]) // 去掉 "if"（case-insensitive 匹配后取原始行）
    // 精确剥离：找到第一个空白符后的部分作为条件
    cond, err := parseCondition(condStr)
    if err != nil {
        return nil, 0, fmt.Errorf("line %d: %w", ifLineIdx+1, err)
    }

    // 解析 then body
    thenBody, nextIdx, err := parseBlock(lines, ifLineIdx+1, true)
    if err != nil {
        return nil, 0, err
    }
    if nextIdx >= len(lines) {
        return nil, 0, fmt.Errorf("line %d: unclosed if block (missing 'end')", ifLineIdx+1)
    }

    block := &IfBlock{Condition: *cond, Then: thenBody}
    terminator := strings.ToLower(strings.TrimSpace(lines[nextIdx]))

    if terminator == "else" {
        elseBody, endIdx, err := parseBlock(lines, nextIdx+1, true)
        if err != nil {
            return nil, 0, err
        }
        if endIdx >= len(lines) || strings.ToLower(strings.TrimSpace(lines[endIdx])) != "end" {
            return nil, 0, fmt.Errorf("line %d: unclosed else block (missing 'end')", nextIdx+1)
        }
        block.Else = elseBody
        return block, endIdx + 1, nil
    }

    // terminator == "end"
    return block, nextIdx + 1, nil
}
```

嵌套自然支持：`parseBlock` → 遇到 `if` → `parseIfBlock` → 递归调用 `parseBlock`。

#### 条件表达式

最小条件语法——只支持二元比较：

```
$VAR.PROP OP VALUE    — 属性访问（captures 中的 SpawnResult）
$VAR OP VALUE         — 普通变量（env 中的字符串值）
```

```go
type Condition struct {
    VarName  string // "result"
    Property string // "exitcode"，或 "" 表示普通变量
    Operator string // "==" 或 "!="
    Value    string // "0"（均作为字符串比较）
}

func parseCondition(s string) (*Condition, error) {
    s = strings.TrimSpace(s)
    parts := strings.Fields(s)
    if len(parts) != 3 {
        return nil, fmt.Errorf("invalid condition: expected 3 parts, got %d in %q", len(parts), s)
    }

    left, op, right := parts[0], parts[1], parts[2]
    if op != "==" && op != "!=" {
        return nil, fmt.Errorf("invalid operator %q: must be '==' or '!='", op)
    }
    if !strings.HasPrefix(left, "$") {
        return nil, fmt.Errorf("condition left operand must start with '$': got %q", left)
    }

    ref := left[1:] // 去掉 $
    varName := ref
    property := ""
    if dotIdx := strings.IndexByte(ref, '.'); dotIdx >= 0 {
        varName = ref[:dotIdx]
        property = ref[dotIdx+1:]
    }

    return &Condition{VarName: varName, Property: property, Operator: op, Value: right}, nil
}
```

条件操作符限于 `==` 和 `!=`（字符串比较）。数值比较 `>`, `<` 等推迟至 Phase 3。`$result.exitcode == 0` 比较的是字符串 `"0"` 与 `strconv.Itoa(exitCode)`。

#### 赋值语法：`result = spawn "..."`

赋值是结果捕获机制，使 spawn 的退出码和输出可供后续条件和变量引用使用：

```go
func isAssignment(line string) (varName, rest string, ok bool) {
    // 找第一个 '='
    eqIdx := strings.Index(line, "=")
    if eqIdx < 0 {
        return "", "", false
    }

    varName = strings.TrimSpace(line[:eqIdx])
    if !isValidVarName(varName) {
        return "", "", false
    }

    rest = strings.TrimSpace(line[eqIdx+1:])
    lower := strings.ToLower(rest)
    // 赋值右侧必须是 spawn 命令
    if !strings.HasPrefix(lower, "spawn ") && !strings.HasPrefix(lower, "spawn\t") &&
       !strings.HasPrefix(lower, "spawn\"") && !strings.HasPrefix(lower, "spawn'") {
        return "", "", false
    }
    return varName, rest, true
}
```

与 `export KEY=VALUE` 无冲突——`parseStatement` 中 export 检查在赋值检查之前。

赋值检测要求 `=` 右侧以 `spawn` 开头，排除误判（如 intent 中包含 `=` 的 spawn）。

#### on-error 内联语法

```
spawn "危险操作" on-error spawn "回滚"
```

`on-error` 在未引号包裹的位置检测，分割为主命令和 handler：

```go
func splitOnError(line string) (string, string, bool) {
    target := "on-error"
    inQuote := false
    var quoteChar byte

    for i := 0; i < len(line); i++ {
        ch := line[i]
        if inQuote {
            if ch == quoteChar {
                inQuote = false
            }
            continue
        }
        if ch == '"' || ch == '\'' {
            inQuote = true
            quoteChar = ch
            continue
        }
        if i+len(target) <= len(line) && strings.EqualFold(line[i:i+len(target)], target) {
            before := i == 0 || line[i-1] == ' ' || line[i-1] == '\t'
            after := i+len(target) == len(line) || line[i+len(target)] == ' ' || line[i+len(target)] == '\t'
            if before && after {
                main := strings.TrimSpace(line[:i])
                handler := strings.TrimSpace(line[i+len(target):])
                return main, handler, true
            }
        }
    }
    return line, "", false
}
```

`on-error` handler 只支持单个 spawn 命令（不支持 pipeline）。引号内的 `on-error` 不触发分割（如 `spawn "handle on-error case"` 正确解析为普通 spawn）。

#### parseStatement 分派顺序（更新后）

```go
func parseStatement(line string) (Statement, error) {
    lower := strings.ToLower(line)

    // 1. export（最高优先级，不变）
    if strings.HasPrefix(lower, "export ") || strings.HasPrefix(lower, "export\t") {
        return parseExport(line)
    }

    // 2. 赋值检测：VAR = spawn "..."
    if varName, rest, ok := isAssignment(line); ok {
        mainLine, handlerLine, hasOnError := splitOnError(rest)
        cmd, err := parseSpawnCommand(mainLine)
        if err != nil {
            return Statement{}, err
        }
        stmt := Statement{Kind: StmtSpawn, Spawn: &cmd, Assign: varName, Raw: line}
        if hasOnError {
            hCmd, hErr := parseSpawnCommand(handlerLine)
            if hErr != nil {
                return Statement{}, hErr
            }
            stmt.OnError = &hCmd
        }
        return stmt, nil
    }

    // 3. on-error 分割（非赋值场景）
    mainLine, handlerLine, hasOnError := splitOnError(line)

    // 4. pipeline 检测（在主命令部分）
    if isPipelineStatement(mainLine) {
        p, err := ParsePipeline(mainLine)
        if err != nil {
            return Statement{}, err
        }
        stmt := Statement{Kind: StmtPipeline, Pipeline: p, Raw: line}
        if hasOnError {
            hCmd, hErr := parseSpawnCommand(handlerLine)
            if hErr != nil {
                return Statement{}, hErr
            }
            stmt.OnError = &hCmd
        }
        return stmt, nil
    }

    // 5. spawn（默认）
    cmd, err := parseSpawnCommand(mainLine)
    if err != nil {
        return Statement{}, err
    }
    stmt := Statement{Kind: StmtSpawn, Spawn: &cmd, Raw: line}
    if hasOnError {
        hCmd, hErr := parseSpawnCommand(handlerLine)
        if hErr != nil {
            return Statement{}, hErr
        }
        stmt.OnError = &hCmd
    }
    return stmt, nil
}
```

注意：`if` 关键字不在 `parseStatement` 中处理——它在 `parseBlock` 层级检测，直接调用 `parseIfBlock`。

#### 条件求值

```go
func (e *ScriptExecutor) evalCondition(cond *Condition) (bool, error) {
    var left string

    if cond.Property != "" {
        capture, ok := e.captures[cond.VarName]
        if !ok {
            return false, fmt.Errorf("undefined result variable: %q", cond.VarName)
        }
        switch cond.Property {
        case "exitcode":
            left = strconv.Itoa(capture.ExitCode)
        case "result":
            left = capture.Result
        default:
            return false, fmt.Errorf("unknown property %q on %q", cond.Property, cond.VarName)
        }
    } else {
        val, ok := e.env.Get(cond.VarName)
        if !ok {
            left = "" // 未定义变量→空字符串（bash 行为）
        } else {
            left = val
        }
    }

    switch cond.Operator {
    case "==":
        return left == cond.Value, nil
    case "!=":
        return left != cond.Value, nil
    }
    return false, fmt.Errorf("unknown operator: %q", cond.Operator)
}
```

属性访问从 `captures` 查找（仅限 `exitcode`/`result`），普通变量从 `env` 查找。

#### 执行语义：三种 spawn 行为

| 模式 | 语法 | 非零 ExitCode 行为 |
|------|------|-------------------|
| 普通 spawn | `spawn "cmd"` | 中断脚本（现有行为不变） |
| 赋值 spawn | `result = spawn "cmd"` | 不中断，结果存入 captures，可通过 if 检查 |
| on-error spawn | `spawn "cmd" on-error spawn "fallback"` | handler 执行，脚本是否继续取决于 handler 的 ExitCode |

```go
func (e *ScriptExecutor) executeBlock(ctx context.Context, stmts []Statement,
    result *ScriptResult, stageNum *int, totalStages int) error {

    for _, stmt := range stmts {
        if err := ctx.Err(); err != nil {
            return err
        }

        switch stmt.Kind {
        case StmtExport:
            expandedValue := e.env.Expand(stmt.Export.Value)
            e.env.Set(stmt.Export.Key, expandedValue)

        case StmtSpawn:
            *stageNum++
            expandedIntent := e.env.Expand(stmt.Spawn.Intent)
            if e.OnStageStart != nil {
                e.OnStageStart(*stageNum, totalStages, expandedIntent)
            }
            res, exitCode, tokens, err := e.spawner.SpawnAndWait(
                ctx, expandedIntent, stmt.Spawn.Agent, stmt.Spawn.Model)
            if err != nil {
                return fmt.Errorf("spawn: %w", err)
            }
            result.LastResult = res
            result.LastExitCode = exitCode
            result.TotalTokens += tokens

            // 赋值：存入 captures + env
            if stmt.Assign != "" {
                e.captures[stmt.Assign] = &SpawnResult{
                    ExitCode: exitCode, Result: res, Tokens: tokens,
                }
                e.env.Set(stmt.Assign, res) // 文本输出作为 $result 可展开
            }

            // on-error：失败时执行 handler
            if exitCode != 0 && stmt.OnError != nil {
                *stageNum++
                hIntent := e.env.Expand(stmt.OnError.Intent)
                if e.OnStageStart != nil {
                    e.OnStageStart(*stageNum, totalStages, hIntent)
                }
                hRes, hExitCode, hTokens, hErr := e.spawner.SpawnAndWait(
                    ctx, hIntent, stmt.OnError.Agent, stmt.OnError.Model)
                if hErr != nil {
                    return fmt.Errorf("on-error: %w", hErr)
                }
                result.LastResult = hRes
                result.LastExitCode = hExitCode
                result.TotalTokens += hTokens

                if stmt.Assign != "" {
                    e.captures[stmt.Assign] = &SpawnResult{
                        ExitCode: hExitCode, Result: hRes, Tokens: hTokens,
                    }
                    e.env.Set(stmt.Assign, hRes)
                }
            }

            // 中断决策：赋值 spawn 不中断
            if result.LastExitCode != 0 && stmt.Assign == "" {
                return nil
            }

        case StmtPipeline:
            *stageNum++
            expanded := expandPipelineIntents(e.env, stmt.Pipeline)
            if e.OnStageStart != nil {
                e.OnStageStart(*stageNum, totalStages, "pipeline")
            }
            pExec := NewPipelineExecutor(e.spawner)
            pResult, err := pExec.Execute(ctx, expanded)
            if err != nil {
                return fmt.Errorf("pipeline: %w", err)
            }
            if len(pResult.Stages) > 0 {
                last := pResult.Stages[len(pResult.Stages)-1]
                result.LastResult = last.Result
                result.LastExitCode = last.ExitCode
            }
            result.TotalTokens += pResult.TotalTokens
            if result.LastExitCode != 0 {
                return nil
            }

        case StmtIf:
            match, err := e.evalCondition(&stmt.If.Condition)
            if err != nil {
                return fmt.Errorf("if condition: %w", err)
            }
            var branch []Statement
            if match {
                branch = stmt.If.Then
            } else {
                branch = stmt.If.Else
            }
            if len(branch) > 0 {
                err = e.executeBlock(ctx, branch, result, stageNum, totalStages)
                if err != nil {
                    return err
                }
                if result.LastExitCode != 0 {
                    return nil // 传播中断
                }
            }
        }
    }
    return nil
}
```

顶层 `Execute` 重构：

```go
func (e *ScriptExecutor) Execute(ctx context.Context, script *Script) (*ScriptResult, error) {
    start := time.Now()
    result := &ScriptResult{}
    stageNum := 0
    totalStages := countExecutableStages(script)

    err := e.executeBlock(ctx, script.Statements, result, &stageNum, totalStages)
    result.Elapsed = time.Since(start)
    if err != nil {
        return nil, err
    }
    return result, nil
}
```

#### countExecutableStages 递归计算

```go
func countExecutableStages(script *Script) int {
    return countStagesInBlock(script.Statements)
}

func countStagesInBlock(stmts []Statement) int {
    n := 0
    for _, stmt := range stmts {
        switch stmt.Kind {
        case StmtSpawn:
            n++
            if stmt.OnError != nil {
                n++ // on-error handler 也计为一个 stage
            }
        case StmtPipeline:
            n++
        case StmtIf:
            n += countStagesInBlock(stmt.If.Then)
            n += countStagesInBlock(stmt.If.Else)
        }
    }
    return n
}
```

返回所有分支中 spawn/pipeline 的总数上界。由于运行时只走一个分支，实际 stage 数 ≤ 总数。对进度显示来说这是可接受的近似。

#### 完整执行流示例

```
# 输入脚本
result = spawn "分析代码" --agent=code-analyst
if $result.exitcode == 0
  spawn "基于分析结果 $result 生成报告"
else
  spawn "记录失败原因: $result"
end

# 执行流（假设"分析代码"成功返回"发现3个问题"）
Step 1: result = spawn "分析代码"
  → SpawnAndWait("分析代码", agent="code-analyst")
  → exitCode=0, result="发现3个问题", tokens=200
  → captures["result"] = {ExitCode:0, Result:"发现3个问题", Tokens:200}
  → env.Set("result", "发现3个问题")
  → 赋值 spawn，不中断

Step 2: if $result.exitcode == 0
  → evalCondition: captures["result"].ExitCode → "0" == "0" → true
  → 执行 then 分支

Step 3: spawn "基于分析结果 $result 生成报告"
  → env.Expand → "基于分析结果 发现3个问题 生成报告"
  → SpawnAndWait(...)
```

```
# on-error 执行流
spawn "部署到生产" on-error spawn "回滚到上一版本"

# 假设"部署到生产"失败（exitCode=1）
Step 1: spawn "部署到生产"
  → exitCode=1 (失败)
  → on-error handler 存在 → 执行 handler

Step 2: spawn "回滚到上一版本"
  → exitCode=0 (成功)
  → result.LastExitCode=0
  → 脚本继续执行后续语句
```

### 复用现有代码

**必须复用（不要重新实现）：**
- `shell/parser.go`：`parseSpawnCommand` + `tokenize`——赋值和 on-error 的 spawn 部分复用现有解析逻辑
- `shell/parser.go`：`ParsePipeline` + `splitPipeline`——pipeline 语句解析不变
- `shell/pipe.go`：`PipelineExecutor` + `KernelSpawner` 接口——pipeline 执行不变
- `shell/env.go`：`Environment.Expand` + `Environment.Get/Set`——变量展开和存储不变
- `shell/script.go`：`parseExport` + `isPipelineStatement` + `expandPipelineIntents` + `unquote`——直接复用
- `shell/script.go`：`isValidVarName` + `isVarStart` + `isVarChar`——赋值变量名验证复用

**不要修改的现有代码：**
- `shell/parser.go` — 签名不变
- `shell/pipe.go` — 签名不变
- `shell/env.go` — 不涉及修改
- `ipc/protocol.go` — exec_script 协议不变
- `ipc/server.go` — handleExecScript 不变
- `ipc/client.go` — ExecScriptAndWatch 不变
- `kernel/` — 内核层不涉及
- `vfs/` — VFS 层不涉及

### 修改文件清单

**修改文件：**
- `shell/script.go` — 重构解析器为递归下降 + 新增类型/执行逻辑（~200 行新增，~30 行重构）
  - 新增：`StmtIf`、`Condition`、`IfBlock`、`SpawnResult` 类型
  - 新增：`parseBlock`、`parseIfBlock`、`parseCondition`、`isAssignment`、`splitOnError`、`evalCondition`、`executeBlock`、`countStagesInBlock`
  - 重构：`ParseScript`（调用 parseBlock）、`Execute`（调用 executeBlock）、`parseStatement`（新增赋值和 on-error 分派）、`countExecutableStages`（递归）
  - Statement 扩展：新增 `If`、`Assign`、`OnError` 字段
  - ScriptExecutor 扩展：新增 `captures` 字段
- `cmd/rnix/main.go` — `isScriptSyntax` 新增 on-error 检测（~10 行新增）

**新增导入：**
- `shell/script.go` 新增 `strconv` 导入（`strconv.Itoa` 用于 exitcode 转字符串比较）

**测试文件：**
- `shell/script_test.go` — ~25 个新测试用例（解析 + 执行 + 回归）
- `cmd/rnix/main_test.go` — `isScriptSyntax` on-error 检测测试

### 依赖方向验证

```
shell/ → (无外部依赖，仅标准库 + strconv 新增)
  script.go  imports: context, fmt, strconv, strings, time + 同包 parser.go/pipe.go/env.go

cmd/   → shell/ (不变)
cmd/   → ipc/   (不变)
```

`shell/` 不依赖 `kernel/`、`vfs/`、`drivers/`——通过 `KernelSpawner` 接口解耦。无新外部依赖引入。

### Project Structure Notes

- 所有控制结构逻辑在 `shell/script.go` 内，与 `parser.go`/`pipe.go`/`env.go` 同级
- 不引入新文件——控制结构是脚本解析/执行的自然扩展
- `cmd/rnix/main.go` 仅微调 `isScriptSyntax`，其余 CLI 逻辑不变
- IPC 层零改动——`exec_script` 协议已足够通用，控制结构在 daemon 端 shell 层处理

### 测试策略

#### 测试方法

- `shell/script_test.go`：纯单元测试，复用 Story 11.1/11.2 的 `mockSpawner` 模式
- `cmd/rnix/main_test.go`：`isScriptSyntax` 检测逻辑测试
- 无需 IPC 集成测试——控制结构在 `shell/` 层完全处理，不影响 IPC 协议

**复用 mockSpawner 模式（与 11.1/11.2 一致）：**

```go
type mockSpawner struct {
    results []mockResult
    calls   []mockCall
}

type mockResult struct {
    result   string
    exitCode int
    tokens   int
    err      error
}

type mockCall struct {
    intent string
    agent  string
    model  string
}
```

#### 测试用例分组

| 测试 ID | 类别 | 验证内容 |
|---------|------|---------|
| 11.3-UNIT-001 | P0 | ParseScript if/else/end 基本结构 |
| 11.3-UNIT-002 | P0 | ParseScript if（无 else）结构 |
| 11.3-UNIT-003 | P0 | ParseScript 嵌套 if 结构 |
| 11.3-UNIT-004 | P0 | parseCondition `$VAR.PROP == VALUE` |
| 11.3-UNIT-005 | P1 | parseCondition `$VAR == VALUE`（普通变量） |
| 11.3-UNIT-006 | P0 | ParseScript 赋值 spawn `result = spawn "..."` |
| 11.3-UNIT-007 | P0 | ParseScript on-error `spawn "A" on-error spawn "B"` |
| 11.3-UNIT-008 | P0 | ParseScript 赋值 + on-error 组合 |
| 11.3-UNIT-009 | P0 | ParseScript 错误——未闭合 if 块 |
| 11.3-UNIT-010 | P1 | ParseScript 错误——else/end 在 if 外 |
| 11.3-UNIT-011 | P1 | ParseScript 错误——无效条件 |
| 11.3-UNIT-012 | P0 | Executor if then 分支（exitcode==0） |
| 11.3-UNIT-013 | P0 | Executor if else 分支（exitcode!=0） |
| 11.3-UNIT-014 | P0 | Executor if 无 else（条件不满足→跳过） |
| 11.3-UNIT-015 | P0 | Executor 嵌套 if |
| 11.3-UNIT-016 | P0 | Executor 赋值 spawn 不中断 |
| 11.3-UNIT-017 | P0 | Executor 赋值 spawn 文本输出 $result 展开 |
| 11.3-UNIT-018 | P0 | Executor on-error 触发 |
| 11.3-UNIT-019 | P0 | Executor on-error 不触发（成功时跳过） |
| 11.3-UNIT-020 | P0 | Executor on-error handler 成功→继续 |
| 11.3-UNIT-021 | P0 | Executor on-error handler 失败→中断 |
| 11.3-UNIT-022 | P1 | Executor 条件引用 env 普通变量 |
| 11.3-UNIT-023 | P1 | Executor 条件 `!=` 操作符 |
| 11.3-REG-001 | P0 | 所有 11.2 ParseScript 测试不受影响 |
| 11.3-REG-002 | P0 | 所有 11.2 ScriptExecutor 测试不受影响 |
| 11.3-REG-003 | P1 | isScriptSyntax on-error 检测 |

### 边界情况

- **空 then body**：`if ... end`（then 里没有语句）——合法，条件满足时什么都不做
- **空 else body**：`if ... else end`——合法，else 分支什么都不做
- **if 块内的 export**：`if ... export KEY=val end`——合法，变量设置不受块作用域限制（bash 行为）
- **多层嵌套**：2-3 层嵌套应正确工作（递归解析器天然支持，无深度限制）
- **on-error 在引号内**：`spawn "handle on-error case"`——正确解析为普通 spawn（不分割）
- **赋值 + pipeline**：`result = spawn "A" | spawn "B"`——不支持（赋值只支持单 spawn）。如果 `=` 右侧是 pipeline 语法，`isAssignment` 返回 false（因为不以 `spawn` 开头的不匹配），按 pipeline 正常解析
- **条件中的空格**：`if $result.exitcode==0`（无空格）——解析错误，`Fields` 分割为 1 部分。必须有空格分隔三部分
- **未定义结果变量**：`if $undefined.exitcode == 0`——evalCondition 返回错误 "undefined result variable"
- **未定义 env 变量**：`if $UNDEFINED == ""`——合法，未定义变量为空字符串，与空字符串比较为 true
- **on-error + pipeline**：`spawn "A" | spawn "B" on-error spawn "C"`——on-error 分割在最外层，主命令为 `spawn "A" | spawn "B"`（pipeline），handler 为 `spawn "C"`。这是合法的——pipeline 失败时执行 handler
- **赋值变量名与 export 变量名冲突**：`export x=1` 后 `x = spawn "..."` —— spawn 结果覆盖 env 中的 `x`，captures 中新增 `x`。后续 `$x` 引用为 spawn 文本输出
- **if 块内赋值在 if 块外使用**：合法——captures 和 env 是全局作用域（bash 行为），无块作用域
- **大小写**：`if`/`else`/`end` 大小写不敏感（`IF`/`Else`/`END` 均可识别），与 `export` 一致
- **on-error 大小写**：`ON-ERROR`/`On-Error` 均可识别（`EqualFold`）

### Story 11.2 关键教训

1. **手写状态机扫描器优于正则**：Expand 和 tokenizer 均用手写扫描器。`splitOnError` 继续此风格。
2. **行导向解析为扩展预留**：Story 11.2 的 Dev Notes 明确指出 `StatementKind` 和行导向解析为 11.3 预留扩展点。`parseBlock` 是其自然扩展。
3. **CLI 导入别名**：`cmd/rnix/main.go` 中 `agentshell "github.com/rnixai/rnix/shell"` 别名——新增代码继续使用。
4. **mockSpawner 复用**：`pipe_test.go` 中的 `mockSpawner` 在同包内可直接使用。新测试复用此模式。
5. **ScriptExecutor 顺序执行模型**：shell 执行是顺序的，不需要线程安全。`captures` map 不需要并发保护。

### References

- [Source: _bmad-output/planning-artifacts/epics/epic-11-agentshell-高级语法agentshell-advanced-syntax.md#Story 11.3]
- [Source: _bmad-output/planning-artifacts/prd/functional-requirements.md#FR68 最小控制结构集]
- [Source: _bmad-output/planning-artifacts/prd/project-scoping-phased-development.md#AgentShell 脚本 shell/script.go]
- [Source: _bmad-output/project-context.md#Go 语言特定规则]
- [Source: _bmad-output/project-context.md#依赖方向]
- [Source: shell/script.go#ParseScript + parseStatement + ScriptExecutor]
- [Source: shell/parser.go#parseSpawnCommand + tokenize]
- [Source: shell/pipe.go#KernelSpawner 接口 + PipelineExecutor]
- [Source: shell/env.go#Environment + Expand]
- [Source: _bmad-output/implementation-artifacts/11-2-variables-and-environment-passing.md#StatementKind 设计（为 11.3 预留）]
- [Source: _bmad-output/implementation-artifacts/11-2-variables-and-environment-passing.md#行导向解析模型]
- [Source: _bmad-output/implementation-artifacts/11-2-variables-and-environment-passing.md#KernelSpawner 接口解耦]
- [Source: _bmad-output/implementation-artifacts/11-1-pipe-syntax.md#手写递归下降解析器]

## Dev Agent Record

### Agent Model Used

Claude claude-4.6-opus (Cursor)

### Debug Log References

无——一次性通过，无调试需要。

### Completion Notes List

- ✅ Task 1: 新增 `StmtIf`、`Condition`、`IfBlock`、`SpawnResult` 类型，扩展 `Statement` 结构体添加 `If`、`Assign`、`OnError` 字段
- ✅ Task 2: 重构 `ParseScript` 为递归下降解析器——`parseBlock`/`parseIfBlock`/`parseCondition`，所有 11.2 测试零修改通过
- ✅ Task 3: 实现 `isAssignment` 赋值检测、`splitOnError` 引号感知分割，更新 `parseStatement` 分派顺序（export → assignment → on-error → pipeline → spawn）
- ✅ Task 4: `ScriptExecutor` 新增 `captures` map，实现 `evalCondition` 条件求值和 `executeBlock` 递归块执行，三种 spawn 中断语义（普通中断/赋值不中断/on-error handler 决定）
- ✅ Task 5: `isScriptSyntax` 新增 on-error 检测，导出 `SplitOnError` 供 CLI 使用
- ✅ Task 6: 25 个测试全部通过（11 个解析测试 + 12 个执行测试 + 3 个额外边界测试 + 1 个 CLI 测试），全套件 18 包零回归

### Senior Developer Review (AI)

**Reviewer:** Decker (via Claude claude-4.6-opus) | **Date:** 2026-03-03

**Review Result:** Approve (with fixes applied)

**Git vs Story 差异:** 0 (File List 与 git 一致)

**Issues Found:** 0 High, 3 Medium, 1 Low

| ID | Severity | Description | Resolution |
|----|----------|-------------|------------|
| CR-1 | MEDIUM | `executeBlock` StmtPipeline case 不检查/执行 `stmt.OnError`——pipeline on-error handler 被解析但未执行 | Fixed: 添加 pipeline on-error handler 执行逻辑 |
| CR-2 | MEDIUM | `parseIfBlock` if/else 分支完全相同（`ifLine[3:]`），else 为死代码 | Fixed: 移除冗余分支 |
| CR-3 | MEDIUM | Pipeline+on-error 组合缺少测试覆盖（解析和执行） | Fixed: 添加 3 个测试 |
| CR-4 | LOW | `isAssignment` 接受 pipeline 右侧但 `parseSpawnCommand` 报错——Dev Notes 描述与代码行为不一致 | Noted: 不支持的边界情况，错误消息可接受 |

**AC 验证:**
- AC1 (if/else/end): IMPLEMENTED — 递归下降解析器 + executeBlock 条件分支
- AC2 (on-error): IMPLEMENTED — splitOnError 引号感知分割 + 三种 spawn 中断语义
- AC3 (嵌套): IMPLEMENTED — 递归自然支持，测试覆盖 2 层嵌套

**全套件验证:** 18 包测试通过，零回归。

### Change Log

- 2026-03-03: 实现 Story 11.3 最小控制结构——if/else/end 条件分支、赋值 spawn、on-error 内联错误处理
- 2026-03-03: Code Review 修复——pipeline on-error 执行逻辑、parseIfBlock 死代码清理、补充 pipeline+on-error 测试

### File List

- `shell/script.go` — 重构解析器为递归下降 + 新增类型/执行逻辑（~200 行新增，~30 行重构）
- `shell/script_test.go` — 新增 25+ 个测试用例（解析 + 执行 + 回归 + 边界）（ATDD RED 阶段已预写）
- `cmd/rnix/main.go` — `isScriptSyntax` 新增 on-error 检测（~5 行修改）
- `cmd/rnix/main_test.go` — `isScriptSyntax` on-error 测试（ATDD RED 阶段已预写）
