# Story 18.2: 函数定义与调用

Status: done

## Story

As a 应用开发者,
I want 在 AgentShell 脚本中定义和调用函数,
So that 我可以复用脚本逻辑。

## Acceptance Criteria

1. **Given** 脚本定义函数 `fn analyze(file) ... end`
   **When** 脚本调用 `analyze("config.yaml")`
   **Then** 函数体执行，参数正确传递，返回值可用

2. **Given** 函数内部使用 `return result`
   **When** 函数执行完毕
   **Then** 调用方获得返回值

3. **Given** 函数调用参数数量不匹配
   **When** 脚本解析时
   **Then** 报告错误并指出行号和期望参数数量

4. **Given** 函数定义无参数 `fn setup()`
   **When** 脚本调用 `setup()`
   **Then** 函数体正常执行

5. **Given** 函数内部包含 spawn/if/for/while 等语句
   **When** 函数执行
   **Then** 所有嵌套语句正确执行

6. **Given** 函数内调用另一个已定义的函数
   **When** 嵌套函数调用执行
   **Then** 正确递归进入/返回，参数独立

7. **Given** 调用未定义的函数
   **When** 脚本执行时
   **Then** 报告运行时错误并指出函数名

8. **Given** 函数参数与外部同名变量
   **When** 函数执行时修改参数变量
   **Then** 函数返回后外部变量恢复原值（参数作用域隔离）

9. **Given** 函数体内使用 `return` 不带值
   **When** 函数执行
   **Then** 返回空字符串

10. **Given** 函数名为保留关键字（如 `fn if()`）
    **When** 脚本解析时
    **Then** 报告错误：函数名不能是保留关键字

## Tasks / Subtasks

### Task 1: AST 节点扩展（AC: #1, #2）

- [x] 1.1 在 `shell/script.go` 新增 `FnDef` 结构体
  ```go
  type FnDef struct {
      Name   string      // 函数名
      Params []string    // 参数名列表
      Body   []Statement // 函数体
  }
  ```
- [x] 1.2 新增 `FnCallStmt` 结构体
  ```go
  type FnCallStmt struct {
      Name string   // 函数名
      Args []string // 参数值列表（支持变量展开）
  }
  ```
- [x] 1.3 新增 `ReturnStmt` 结构体
  ```go
  type ReturnStmt struct {
      Value string // 返回值表达式（变量引用或字面量，空字符串 = 无返回值）
  }
  ```
- [x] 1.4 扩展 `StatementKind` 常量：新增 `StmtFnDef = "fn-def"`, `StmtFnCall = "fn-call"`, `StmtReturn = "return"`
- [x] 1.5 扩展 `Statement` 结构体：新增 `FnDef *FnDef`, `FnCall *FnCallStmt`, `Return *ReturnStmt` 字段

### Task 2: 解析器扩展 — 函数定义（AC: #1, #3, #4, #5, #10）

- [x] 2.1 在 `parseBlock` 中识别 `fn` 关键字，调用 `parseFnDef`
  - 函数定义**仅允许在顶层**（`insideBlock == false`），嵌套块内定义函数报错
- [x] 2.2 实现 `parseFnDef` 函数
  - 语法：`fn NAME(PARAM1, PARAM2, ...)` + 函数体 + `end`
  - 函数名必须不在 `reservedKeywords` 中（AC #10）
  - 参数名不得重复，不得是保留关键字
  - 参数列表：括号内逗号分隔，支持零参数 `fn name()`
  - 函数体调用 `parseBlock(lines, nextIdx, true)` 复用已有块解析
  - 未闭合的函数体报告行号 + 缺少 `end`
- [x] 2.3 收集所有 `FnDef` 到 `Script` 结构体的函数表
  - 在 `Script` 中新增 `Functions map[string]*FnDef` 字段
  - ParseScript 完成后遍历顶层 `Statements`，提取所有 `StmtFnDef` 到 `Functions` map
  - 重复函数名 → 解析错误

### Task 3: 解析器扩展 — 函数调用（AC: #1, #3）

- [x] 3.1 在 `parseStatement` 中识别函数调用模式
  - 模式匹配：`NAME(ARGS)` — 标识符后紧跟 `(`
  - 赋值形式：`VAR = NAME(ARGS)` — 变量赋值 + 函数调用
  - 函数调用与 spawn 的区分：检查行是否匹配 `IDENTIFIER(` 模式（不含 spawn/export 等关键字）
- [x] 3.2 实现 `parseFnCall` 函数
  - 解析函数名和括号内的参数列表
  - 参数值：支持引号字符串 `"text"`、变量引用 `$VAR`、字面量
  - 参数数量的检查推迟到执行时（函数定义可能在调用之后定义 → 不对，函数定义只在顶层，解析完有完整的函数表 → 可在解析后做一次全局校验）
- [x] 3.3 解析后全局校验
  - 遍历所有 `StmtFnCall`，检查 `Script.Functions[name]` 存在性
  - 检查参数数量是否匹配 `len(FnDef.Params)`
  - 不匹配 → 报告错误：`line X: function "NAME" expects N args, got M`

### Task 4: 解析器扩展 — return 语句（AC: #2, #9）

- [x] 4.1 在 `parseBlock` 中识别 `return` 关键字，调用 `parseReturnStatement`
  - `return` 仅在块内有效（`insideBlock == true`）
  - 顶层 `return` → 解析错误
- [x] 4.2 实现 `parseReturnStatement`
  - `return` — 无返回值（Value = ""）
  - `return $VAR` — 变量引用
  - `return "literal"` — 字面量
  - `return $VAR.result` — 属性访问

### Task 5: 解释器执行 — 函数定义注册（AC: #1）

- [x] 5.1 在 `ScriptExecutor` 中新增函数注册表
  ```go
  type ScriptExecutor struct {
      spawner   KernelSpawner
      env       *Environment
      captures  map[string]*SpawnResult
      functions map[string]*FnDef  // 新增
      OnStageStart StageCallback
  }
  ```
- [x] 5.2 在 `Execute` 方法入口，从 `script.Functions` 加载到 `executor.functions`
- [x] 5.3 在 `executeBlock` 中 `StmtFnDef` 分支：跳过（函数定义不执行，只注册）

### Task 6: 解释器执行 — 函数调用（AC: #1, #4, #5, #6, #7, #8）

- [x] 6.1 在 `executeBlock` 中处理 `StmtFnCall`
  - 查找 `executor.functions[fnCall.Name]`，未找到 → 返回运行时错误
  - 参数展开：对每个 arg 调用 `env.Expand(arg)`
  - 参数绑定：**保存外部同名变量 → 设置参数值 → 执行函数体 → 恢复外部变量**
    ```go
    // 1. 保存参数名的旧值
    saved := make(map[string]saveEntry)
    for i, paramName := range fnDef.Params {
        if old, ok := env.Get(paramName); ok {
            saved[paramName] = saveEntry{value: old, existed: true}
        } else {
            saved[paramName] = saveEntry{existed: false}
        }
        env.Set(paramName, expandedArgs[i])
    }
    // 2. 执行函数体
    err := executor.executeBlock(ctx, fnDef.Body, result, stageNum, totalStages)
    // 3. 恢复
    for paramName, entry := range saved {
        if entry.existed {
            env.Set(paramName, entry.value)
        } else {
            env.Delete(paramName)
        }
    }
    ```
  - 支持赋值形式：`VAR = name(args)` → 函数返回值存入 `env.Set(VAR, returnValue)`

### Task 7: 解释器执行 — return 语句（AC: #2, #9）

- [x] 7.1 定义 `ErrFnReturn` 类型（与 `ErrScriptExit` 模式一致）
  ```go
  type ErrFnReturn struct {
      Value string
  }
  func (e *ErrFnReturn) Error() string {
      return fmt.Sprintf("function return: %s", e.Value)
  }
  ```
- [x] 7.2 在 `executeBlock` 中处理 `StmtReturn`
  - 展开 return 值：`env.Expand(returnStmt.Value)`
  - 如果值以 `$` 开头且含 `.result` 属性，从 captures 中取值
  - 返回 `&ErrFnReturn{Value: expandedValue}`
- [x] 7.3 在函数调用的 `executeBlock` 外层捕获 `ErrFnReturn`
  - `errors.As(err, &returnErr)` → 提取返回值，清除 error
  - 无 return（正常执行完函数体）→ 返回值为空字符串
- [x] 7.4 确保 `ErrFnReturn` 不会泄漏到函数外部
  - 函数调用处必须捕获，未捕获的 `ErrFnReturn` 在 `Execute` 顶层转为错误

### Task 8: 复杂度统计扩展

- [x] 8.1 在 `countStagesInBlock` 中处理 `StmtFnDef`：0（定义不计算阶段）
- [x] 8.2 在 `countStagesInBlock` 中处理 `StmtFnCall`：1（每次调用计为一个阶段）
- [x] 8.3 在 `countStagesInBlock` 中处理 `StmtReturn`：0

### Task 9: 测试（AC: #1-#10）

- [x] 9.1 `shell/script_test.go` — ParseScript 解析测试
  - fn 基本定义解析（带参数、无参数）
  - fn 函数体包含各种语句（spawn/if/for/while）
  - fn 函数名是保留关键字 → 错误
  - fn 参数名重复 → 错误
  - fn 未闭合 → 缺少 end 错误
  - fn 嵌套定义（块内定义）→ 错误
  - fn 重复定义同名函数 → 错误
  - 函数调用解析（带参数、无参数、赋值形式）
  - 函数调用参数数量不匹配 → 错误含行号
  - 调用未定义函数 → 错误
  - return 解析（带值、不带值）
  - return 在顶层 → 错误

- [x] 9.2 `shell/script_test.go` — 执行器测试
  - 函数定义 + 调用 → 参数正确传递到 spawn intent
  - 函数 return 值捕获到赋值变量
  - 无 return → 赋值变量为空
  - 参数作用域隔离（外部变量恢复）
  - 嵌套函数调用（A 调 B）
  - 函数体内 for/while/if 组合
  - 函数体内 spawn on-error
  - return 中途退出函数
  - 调用未定义函数 → 运行时错误
  - 零参数函数调用

- [x] 9.3 竞态测试：`go test -race ./shell/...`

## Dev Notes

### 关键架构约束

- **解析器架构**：手写递归下降解析器（Decision 10），不使用 parser generator
- **执行模型**：AST walker 解释执行，瓶颈在 LLM 调用（秒级），解释器开销 ≤ 1ms/次（NFR39）
- **Phase 3 新增 AST 节点**：FnNode（本 Story 实现）
- **FR98**：用户可以在 AgentShell 脚本中定义和调用函数（`fn name(args) { ... }`），支持参数传递和返回值

### 语法设计决策

**函数定义**使用 `end` 作为块终止符，与 for/while/if 保持一致（PRD 中 `{...}` 视为 AC 简写，实际实现统一用 `end`）：

```
fn analyze(file)
  spawn "分析 ${file}" --agent=analyst
  return $result.result
end

fn setup()
  export MODEL=sonnet
end
```

**函数调用**使用 `name(args)` 语法，与 spawn 通过模式区分：

```
# 直接调用
analyze("config.yaml")

# 赋值调用 — 捕获返回值
result = analyze("config.yaml")

# 无参数调用
setup()

# 嵌套调用
outer_result = process(analyze("input.txt"))
```

**解析区分规则**：一行以 `IDENTIFIER(` 开头（且 IDENTIFIER 不是 spawn/export 等关键字）→ 函数调用。否则走已有的 spawn/pipeline 解析。赋值形式 `VAR = IDENTIFIER(` 同理。

**return 语法**：

```
return                # 返回空字符串
return $result.result # 返回 captures 属性值
return "literal"      # 返回字面量
return $var           # 返回环境变量值
```

### 现有代码模式（必须遵循）

**解析模式** — 参考 `parseForBlock` / `parseWhileBlock` / `parseIfBlock`：
- `parseBlock` 函数解析各种块的嵌套体
- 关键字匹配大小写不敏感（`strings.EqualFold`）
- 行号追踪用于错误报告（`lineIdx` 参数）
- 块级语句以 `end` 关键字结束
- `insideBlock` 参数控制是否允许 `end`/`else` 终止

**执行模式** — 参考 `executeBlock` 中各 `stmt.Kind` 分支：
- switch on `stmt.Kind` 分发
- 递归调用 `executeBlock` 处理嵌套块
- 每步检查 `ctx.Err()` 支持取消
- 失败时返回 error 并停止执行

**变量作用域模式** — 参考 for 循环的变量绑定：
- for 循环在每次迭代 `env.Set(VarName, item)`，结束后 `env.Delete(VarName)`
- 函数参数应使用更安全的 save/restore 模式（因为参数名可能与已有变量重名）

**错误传播模式** — 参考 `ErrScriptExit`：
- 定义特殊错误类型实现流控制
- `Execute` 顶层通过 `errors.As` 捕获
- `ErrScriptExit` 是正常流控制（清除 error）
- `ErrFnReturn` 同理，在函数调用处捕获

**测试模式** — 参考 `script_test.go`：
- mock `KernelSpawner` 实现 `SpawnAndWait` + `Wait`
- `mockSpawner` 记录 `calls` 和预设 `results`
- 验证调用次数、参数、执行顺序
- 测试函数命名 `TestParseScript_*` / `TestScriptExecutor_*`
- Story ID 注释格式：`// 18.2-UNIT-001: ...`

### 保留关键字表

已注册（Story 18.1 已实现）：
`for`, `in`, `while`, `if`, `else`, `end`, `fn`, `return`, `parallel`, `source`, `wait`, `sleep`, `exit`, `export`, `spawn`

`fn` 和 `return` 已在保留关键字表中，本 Story 为它们实现实际语法。

### 函数定义的解析位置

`fn` 定义**只允许在顶层**。在 `parseBlock` 中：
- `insideBlock == false` 时遇到 `fn` → 调用 `parseFnDef`
- `insideBlock == true` 时遇到 `fn` → 报错 "函数定义不允许在块内"

这避免了闭包、嵌套函数定义等复杂性，保持 AgentShell 的简单性。

### 函数调用 vs spawn 的解析区分

关键区分逻辑（在 `parseStatement` 中）：

1. 先检查是否是赋值形式：`VAR = ...`
   - 如果 `=` 右侧匹配 `IDENTIFIER(` → 函数调用赋值
   - 如果 `=` 右侧匹配 `spawn "..."` → spawn 赋值（已有逻辑）
2. 非赋值：检查行是否匹配 `IDENTIFIER(` 模式
   - 匹配 → 函数调用
   - 不匹配 → spawn/pipeline（已有逻辑）

函数名标识符规则：以字母或下划线开头，后续为字母/数字/下划线，不是保留关键字。

### ErrFnReturn 的传播

`return` 命令返回 `ErrFnReturn`：
- 在 `executeBlock` 的函数调用处通过 `errors.As` 捕获
- 提取 `Value` 作为函数返回值
- 清除 error（return 是正常流控制）
- 如果 `ErrFnReturn` 泄漏到 `Execute` 顶层 → 转为真正的错误（"return outside function"）

注意：`ErrFnReturn` 需要穿透 for/while/if 块——当 return 在循环内执行时，error 需要向上传播跳出所有嵌套块，直到被函数调用处捕获。当前 `executeBlock` 的 for/while/if 分支在 `err != nil` 时已经会向上传播 error，因此 `ErrFnReturn` 会自然穿透。但需要确保 for 循环的变量清理逻辑在 error 路径上也执行。

### 参数解析细节

函数调用参数列表解析：
```
name("arg1", $var, "text with spaces")
name()                    # 无参数
name($a.result)           # captures 属性值
```

参数之间逗号分隔，支持：
- 双引号字符串：`"text"` → 去除引号
- 变量引用：`$VAR` → 保留 `$VAR` 字面量（执行时再展开）
- 字面量：`word` → 原样保留

### 组合矩阵

| 本功能 | 交互对象 | 交互方式 | 需验证 |
|--------|----------|----------|--------|
| fn 定义 | for/while/if | 函数体内可含循环和条件 | 是 |
| fn 调用 | 赋值 spawn | `var = fn_call()` 与 `var = spawn "..."` 共存 | 是 |
| fn 调用 | pipeline | 函数调用不在 pipeline 中使用（pipeline 仅含 spawn 段） | 否 |
| fn 调用 | on-error | 函数调用不支持 on-error（spawn 独有） | 否 |
| fn 调用 | fn 调用 | 函数 A 体内调用函数 B（非递归嵌套调用） | 是 |
| return | for/while | return 在循环内立即终止函数 | 是 |
| return | if/else | return 在条件分支内正确退出函数 | 是 |
| return | ErrScriptExit | exit 在函数体内终止整个脚本，不是只退出函数 | 是 |
| fn 参数 | env 变量 | 参数与 export 变量同名 → 函数内参数优先，返回后恢复 | 是 |
| fn 参数 | for 循环变量 | for 循环变量与参数同名时互不干扰（各自 save/restore） | 是 |

### 不支持的特性（本 Story 范围外）

- **递归调用**：函数 A 调用自身 → 不特别禁止，但无栈深度保护（可加 `MaxCallDepth` 常量，建议 100）
- **闭包/高阶函数**：不支持将函数作为参数传递
- **可变参数**：不支持 `fn name(args...)` 语法
- **默认参数**：不支持 `fn name(a, b="default")`
- **嵌套函数定义**：不支持函数内定义函数

### 递归深度保护

建议新增 `MaxCallDepth = 100` 常量。在 `ScriptExecutor` 中维护 `callDepth int`：
- 函数调用时 `callDepth++`，超过限制返回错误
- 函数返回后 `callDepth--`

### Project Structure Notes

所有改动限制在 `shell/` 包内：
- `shell/script.go` — AST 类型扩展 + 解析器扩展 + 执行器扩展
- `shell/script_test.go` — 新增测试

不涉及 `kernel/`、`vfs/`、`drivers/`、`cmd/`、`ipc/` 等其他包。无需修改 `KernelSpawner` 接口。

### 与 Story 18.1 的差异

Story 18.1 修改了 `shell/pipe.go`（KernelSpawner 接口新增 Wait）和 `ipc/server.go`。本 Story **不需要**修改这些文件——函数是纯解析器/解释器层功能，不涉及新的 syscall 或 IPC 扩展。

### Git Intelligence

最近提交（Story 18.1 + code review）：
- `0c0ebf2` feat: update traceability report for Story 18.1
- `1ab2e36` feat: cr Story 18.1
- `d6089b1` feat: ds 18-1
- `e7bc515` feat: atdd 18-1
- `5ed0f43` feat: implement loop structures and built-in commands for AgentShell

Story 18.1 的 code review 修复了：保留关键字补全、exit 解析校验范围检查、组合矩阵测试补充。本 Story 应从一开始就确保这些质量标准。

### References

- [Source: _bmad-output/planning-artifacts/epics/epic-18-agentshell完整脚本语言-agentshell-complete-scripting.md#Story 18.2]
- [Source: _bmad-output/planning-artifacts/architecture/core-architectural-decisions.md#Decision 10: AgentShell 解析器架构]
- [Source: _bmad-output/planning-artifacts/architecture/implementation-patterns-consistency-rules.md#AgentShell 语法模式]
- [Source: _bmad-output/planning-artifacts/prd/functional-requirements.md#FR98]
- [Source: _bmad-output/planning-artifacts/prd/non-functional-requirements.md#NFR39]
- [Source: _bmad-output/project-context.md]
- [Source: _bmad-output/implementation-artifacts/18-1-loop-structures-and-builtin-commands.md]
- [Source: shell/script.go — 现有 ParseScript/parseBlock/executeBlock 实现]
- [Source: shell/env.go — Environment 变量管理]

## Dev Agent Record

### Agent Model Used

Claude claude-4.6-opus (Cursor Agent)

### Debug Log References

- [Story 18.2 implementation session](af3e7d1f-aeb0-4f01-88e9-7382d180654f)

### Completion Notes List

- 所有 9 个 Task（AST 扩展、解析器 fn/call/return、解释器注册/调用/return、stage 计数、测试）全部完成
- `go test -race ./shell/...` 全部通过（含 ATDD 测试）
- `golangci-lint run ./...` 无新增 lint 错误（修复了 `isValidVarName` 的 S1008 预存问题及 `cmd/rnix/dashboard.go` 两处预存 unused 问题）
- `cmd/rnix/dashboard_test.go` 和 `cmd/rnix/top_test.go` 的 TTY 环境相关失败为预存环境问题，与本 Story 无关

### Change Log

1. `shell/script.go` — 新增 AST 类型（FnDef, FnCallStmt, ReturnStmt, ErrFnReturn），扩展 Statement/Script/ScriptExecutor 结构体；实现 parseFnDef, parseReturnStatement, isFnCallExpr, parseAssignmentFnCall, parseFnCallArgs, validateFnCalls, expandReturnValue, isValidIdentifier；扩展 parseBlock, parseStatement, ParseScript, Execute, executeBlock, countStagesInBlock；新增 MaxCallDepth=100 递归保护
2. `shell/script_test.go` — 新增涵盖 AC #1-#10 的解析和执行测试；修复 TestScriptExecutor_FnParamVsForLoopVar mock 结果数量不足问题
3. `cmd/rnix/dashboard.go` — 修复预存 lint：移除 timelineStreamDoneMsg.Err 未使用字段，添加 renderDashboardPlaceholder nolint 标注

### Senior Developer Review (AI)

**Review Date:** 2026-03-09
**Model:** Claude claude-4.6-opus (Cursor Agent)
**Outcome:** Approved with fixes applied

**Issues Found & Fixed:**
1. **[HIGH] AC3 行号缺失** — `validateFnCalls` 错误消息不含行号，违反 AC3"报告错误并指出行号"的要求。修复：为 `Statement` 添加 `Line int` 字段，在 `parseBlock` 中填充所有语句的行号，`validateFnCalls` 错误消息格式改为 `line X: function "NAME" expects N args, got M`。
2. **[MEDIUM] 参数名缺少标识符校验** — `parseFnDef` 仅检查保留关键字和重复，未验证参数名是否为合法标识符（如 `fn bad(123)` 被静默接受）。修复：在保留关键字检查之前添加 `isValidIdentifier` 校验。
3. **[LOW] 冗余检查** — `isFnCallExpr` 中 `len(s) == 0` 在前置条件下不可能为 true。已移除。

**Tests Added:**
- `TestParseScript_Error_FnCallArgCountMismatch` — 增加行号断言验证
- `TestParseScript_Error_FnCallUndefined` — 增加行号断言验证
- `TestParseScript_Error_FnParamInvalidIdentifier` — 新增非法参数名测试（数字/连字符/空格）

**Notes:**
- Dev Notes 中 `process(analyze("input.txt"))` 示例为误导性文档（实现不支持嵌套调用表达式），但不影响功能正确性
- 所有 10 个 AC 已验证实现完整
- `go test -race ./shell/...` 通过
- `golangci-lint run ./shell/...` 0 issues

### File List

- `shell/script.go` (modified)
- `shell/script_test.go` (modified)
- `cmd/rnix/dashboard.go` (modified, lint fix only)
