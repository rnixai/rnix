# Story 18.1: 循环结构与内置命令

Status: ready-for-dev

## Story

As a 应用开发者,
I want 在 AgentShell 脚本中使用 for/while 循环和内置命令（wait/sleep/exit）,
So that 我可以编写重复和定时的智能体编排逻辑。

## Acceptance Criteria

1. **Given** AgentShell 脚本包含 `for item in [a, b, c]`
   **When** 脚本执行
   **Then** 循环体对每个元素执行一次，`${item}` 正确绑定当前元素

2. **Given** AgentShell 脚本包含 `while` 条件循环
   **When** 条件为真时
   **Then** 循环体重复执行，条件变假时退出

3. **Given** 脚本中使用 `wait <pid>`
   **When** 指定进程完成
   **Then** 脚本继续执行下一行

4. **Given** 脚本中使用 `sleep 5s`
   **When** 执行到该行
   **Then** 脚本暂停 5 秒后继续

5. **Given** 脚本中使用 `exit 0` 或 `exit 1`
   **When** 执行到该行
   **Then** 脚本立即终止，返回指定退出码

6. **Given** for 循环嵌套 if 条件
   **When** 脚本执行
   **Then** 每次迭代中条件正确评估，控制流不混淆

7. **Given** while 循环条件永远为真
   **When** 执行超过 1000 次迭代
   **Then** 自动中断并报告疑似无限循环错误

8. **Given** 脚本中 `sleep` 使用非法格式（如 `sleep abc`）
   **When** 脚本解析/执行时
   **Then** 报告错误并指出行号和期望的格式

## Tasks / Subtasks

### Task 1: AST 节点扩展（AC: #1, #2）

- [ ] 1.1 在 `shell/script.go` 新增 `ForBlock` 结构体
  ```go
  type ForBlock struct {
      VarName string      // 循环变量名（如 "item"）
      List    []string    // 遍历列表（如 ["a", "b", "c"]）
      Body    []Statement // 循环体
  }
  ```
- [ ] 1.2 在 `shell/script.go` 新增 `WhileBlock` 结构体
  ```go
  type WhileBlock struct {
      Condition Condition   // 复用现有 Condition 类型
      Body      []Statement // 循环体
  }
  ```
- [ ] 1.3 扩展 `StatementKind` 常量：新增 `StmtFor = "for"` 和 `StmtWhile = "while"`
- [ ] 1.4 扩展 `Statement` 结构体：新增 `For *ForBlock` 和 `While *WhileBlock` 字段

### Task 2: 内置命令 AST 节点（AC: #3, #4, #5）

- [ ] 2.1 新增 `BuiltinStmt` 结构体
  ```go
  type BuiltinStmt struct {
      Command string   // "wait", "sleep", "exit"
      Args    []string // 参数列表
  }
  ```
- [ ] 2.2 扩展 `StatementKind`：新增 `StmtBuiltin = "builtin"`
- [ ] 2.3 扩展 `Statement`：新增 `Builtin *BuiltinStmt` 字段

### Task 3: 词法与语法分析扩展（AC: #1, #2, #3, #4, #5）

- [ ] 3.1 在 `ParseScript` 中识别 `for` 关键字，调用 `parseForBlock`
  - 语法：`for VAR in [item1, item2, ...]` + 循环体 + `end`
  - 语法（变体）：`for VAR in item1 item2 ...` + 循环体 + `end`（空格分隔列表）
  - `[...]` 列表内逗号分隔，支持引号字符串
  - 循环体递归调用 `parseBlock`（复用已有的 if-block 解析模式）
- [ ] 3.2 在 `ParseScript` 中识别 `while` 关键字，调用 `parseWhileBlock`
  - 语法：`while CONDITION` + 循环体 + `end`
  - 条件复用现有 `parseCondition`（与 if 条件同语法）
  - 循环体递归调用 `parseBlock`
- [ ] 3.3 在 `ParseScript` 中识别内置命令 `wait`/`sleep`/`exit`
  - `wait <pid_var_or_literal>` — 参数为 PID 变量引用或数字字面量
  - `sleep <duration>` — 参数格式 `Ns`/`Nms`/`Nm`（Go `time.ParseDuration` 兼容）
  - `exit <code>` — 参数为整数字面量（0-255）
- [ ] 3.4 错误处理：未闭合的 for/while 块报告行号 + 缺少 `end` 提示

### Task 4: 解释器执行扩展（AC: #1, #2, #3, #4, #5, #6, #7）

- [ ] 4.1 在 `ScriptExecutor.executeBlock` 中处理 `StmtFor`
  - 遍历 `ForBlock.List`，每次迭代：
    1. `env.Set(forBlock.VarName, currentItem)` 绑定循环变量
    2. 递归 `executeBlock(forBlock.Body)`
    3. 检查 ctx 取消信号
  - 迭代结束后从 env 移除循环变量（作用域隔离）
- [ ] 4.2 在 `ScriptExecutor.executeBlock` 中处理 `StmtWhile`
  - 循环开始前初始化迭代计数器
  - 每次迭代：
    1. `evalCondition(whileBlock.Condition)` 判断条件
    2. 条件为假则退出循环
    3. 递归 `executeBlock(whileBlock.Body)`
    4. 迭代计数器 +1，超过 `MaxLoopIterations`（= 10000）则返回错误
    5. 检查 ctx 取消信号
- [ ] 4.3 实现 `executeBuiltin` 方法
  - **wait**：
    - 参数为 PID（从 env 获取变量值或直接解析数字）
    - 调用 `spawner.Wait(ctx, pid)` — 需要扩展 `KernelSpawner` 接口
    - 等待完成后继续
  - **sleep**：
    - `time.ParseDuration(args[0])` 解析时长
    - `select { case <-time.After(d): case <-ctx.Done(): }` 可中断等待
  - **exit**：
    - `strconv.Atoi(args[0])` 解析退出码
    - 返回特殊 `ErrScriptExit{Code: n}` 错误（ScriptExecutor 顶层捕获）
- [ ] 4.4 定义 `ErrScriptExit` 类型
  ```go
  type ErrScriptExit struct {
      Code int
  }
  func (e *ErrScriptExit) Error() string {
      return fmt.Sprintf("script exit with code %d", e.Code)
  }
  ```

### Task 5: KernelSpawner 接口扩展（AC: #3）

- [ ] 5.1 在 `KernelSpawner` 接口新增 `Wait` 方法
  ```go
  type KernelSpawner interface {
      SpawnAndWait(ctx context.Context, intent, agent, model string) (result string, exitCode int, tokens int, err error)
      Wait(ctx context.Context, pid int) (exitCode int, err error)
  }
  ```
- [ ] 5.2 更新所有 `KernelSpawner` 实现（真实 kernel 桥接 + mock）
- [ ] 5.3 `Wait` 桥接到 IPC `wait` method（复用已有 `kernel.Wait` syscall）

### Task 6: 复杂度统计扩展（AC: 无，内部质量）

- [ ] 6.1 在 `estimateComplexity` 函数中处理 `StmtFor` 和 `StmtWhile`
  - for: 列表长度 × 循环体复杂度
  - while: 10（估计值） × 循环体复杂度
- [ ] 6.2 在 `estimateComplexity` 中处理 `StmtBuiltin`
  - wait: 1（实际开销取决于外部进程）
  - sleep/exit: 0

### Task 7: 测试（AC: #1-#8）

- [ ] 7.1 `shell/script_test.go` — ParseScript 测试
  - for-in 基本解析（数组列表和空格列表）
  - while 基本解析
  - 嵌套 for + if 解析
  - 未闭合 for/while 错误
  - 内置命令解析（wait/sleep/exit）
  - 非法参数格式错误
- [ ] 7.2 `shell/script_test.go` — 执行器测试
  - for 循环变量绑定和展开
  - for 循环体内 spawn 调用次数正确
  - while 循环条件变化导致退出
  - while 无限循环保护（MaxLoopIterations）
  - wait 等待 mock 进程完成
  - sleep 可被 ctx.Cancel 中断
  - exit 立即终止脚本并返回正确退出码
  - 嵌套 for + if 组合执行
- [ ] 7.3 竞态测试：`go test -race ./shell/...`

## Dev Notes

### 关键架构约束

- **解析器架构**：手写递归下降解析器（Decision 10），不使用 parser generator
- **执行模型**：AST walker 解释执行，瓶颈在 LLM 调用（秒级），解释器开销 ≤ 1ms/次（NFR39）
- **Phase 3 新增 AST 节点**：ForNode、WhileNode（架构决策中已预规划）

### 现有代码模式（必须遵循）

**解析模式** — 参考现有 `parseIfBlock` 实现：
- `parseBlock` 函数解析 `for`/`while`/`if` 的嵌套体
- 关键字匹配大小写不敏感（`strings.EqualFold`）
- 行号追踪用于错误报告（`lineIdx` 参数）
- 块级语句（for/while/if）以 `end` 关键字结束

**执行模式** — 参考现有 `executeBlock` 中 `StmtIf` 处理：
- switch on `stmt.Kind` 分发
- 递归调用 `executeBlock` 处理嵌套块
- 每步检查 `ctx.Err()` 支持取消
- 失败时返回 error 并停止执行

**变量模式** — 参考 `Environment`：
- `env.Set(key, value)` / `env.Get(key)` 存取变量
- `env.Expand(s)` 在 intent 和参数中展开 `$VAR` / `${VAR}`
- 循环变量使用同一 env，结束后清理

**测试模式** — 参考 `script_test.go`：
- mock `KernelSpawner` 实现 `SpawnAndWait`
- 验证调用次数、参数、执行顺序
- 使用 `t.Fatal` / `t.Errorf` 标准断言
- 测试函数命名 `Test<Type>_<Method>`

### 保留关键字表

已预规划的 Phase 3 追加关键字（本 Story 实现前三个）：
`for`, `in`, `while`, `fn`, `return`, `parallel`, `source`, `wait`, `sleep`, `exit`

**重要**：解析时需要将这些关键字全部注册为保留字，避免作为变量名使用。即使 `fn`/`return`/`parallel`/`source` 在本 Story 不实现语法，也应在关键字检查中预留，防止用户定义同名变量。

### for-in 列表语法设计

```
# 数组字面量形式
for item in [a, b, c]
  spawn "处理 ${item}"
end

# 空格分隔形式
for file in main.go utils.go config.go
  spawn "分析 ${file}"
end

# 变量引用（展开后空格分隔）
for f in $FILE_LIST
  spawn "处理 ${f}"
end
```

解析优先级：`[...]` 方括号形式 → 空格分隔形式。列表元素支持引号字符串和变量展开。

### while 循环语法

```
# 条件与 if 相同
while $counter != 0
  spawn "执行第 ${counter} 轮"
  counter = spawn "减少计数器"
end
```

条件复用 `Condition` 类型和 `parseCondition`/`evalCondition`，无需新增条件语法。

### 内置命令语法

```
# wait — 等待进程完成
pid = spawn "后台任务" --agent=worker
wait $pid

# sleep — 暂停执行
sleep 5s
sleep 500ms
sleep 2m

# exit — 立即退出脚本
if $result.exitcode != 0
  exit 1
end
exit 0
```

`sleep` 时长解析使用 Go 标准 `time.ParseDuration`，接受 `ns`/`us`/`ms`/`s`/`m`/`h` 后缀。

### 无限循环保护

`MaxLoopIterations = 10000`（常量定义在 `shell/script.go`）。while 循环超过此限制时返回错误：
```
script error at line X: while loop exceeded maximum iterations (10000), possible infinite loop
```

### wait 实现注意

`wait` 需要 PID 值。当前 `SpawnResult` 结构没有 PID 字段。需要：
1. 在 `SpawnResult` 中新增 `PID int` 字段
2. `KernelSpawner.SpawnAndWait` 返回值新增 pid（但这是同步等待，pid 已知）
3. 更好方案：新增 `SpawnAsync` 方法或者让 `wait` 从 captures 中读取 PID

**推荐实现**：扩展 `SpawnResult` 添加 PID，并且让赋值 spawn（`pid = spawn "..."`）在 captures 中记录 PID。`wait $pid` 时从 captures 读取 PID 值。如果 captures 中无此 key，则尝试 `strconv.Atoi` 将参数作为数字 PID。

### ErrScriptExit 的传播

`exit` 命令返回 `ErrScriptExit`。在 `ScriptExecutor.Execute` 顶层：
- 如果 error 是 `*ErrScriptExit`，则将其 Code 设为 `ScriptResult.ExitCode`
- 清除 error（exit 不是执行错误，是正常的流控制）

### 组合矩阵

| 本功能 | 交互对象 | 交互方式 | 需验证 |
|--------|----------|----------|--------|
| for 循环 | if 条件 | 嵌套：for 体内可含 if | 是 |
| for 循环 | pipeline | 嵌套：for 体内可执行管道 | 是 |
| for 循环 | on-error | 嵌套：for 体内 spawn 可带 on-error | 是 |
| for 循环 | export/变量 | 循环内可修改变量，循环间可见 | 是 |
| while 循环 | if 条件 | 嵌套：while 体内可含 if | 是 |
| while 循环 | for 循环 | 嵌套：while 体内可含 for | 是 |
| while 循环 | 赋值 spawn | 循环条件引用赋值结果决定是否继续 | 是 |
| wait 命令 | spawn 赋值 | wait 使用 spawn 返回的 PID | 是 |
| sleep 命令 | ctx.Cancel | sleep 可被脚本取消中断 | 是 |
| exit 命令 | for/while 循环 | exit 在循环内立即终止整个脚本 | 是 |

### Project Structure Notes

所有改动限制在 `shell/` 包内：
- `shell/script.go` — AST 类型扩展 + 解析器扩展 + 执行器扩展
- `shell/pipe.go` — `KernelSpawner` 接口扩展（新增 `Wait` 方法）
- `shell/script_test.go` — 新增测试

不涉及 `kernel/`、`vfs/`、`drivers/`、`cmd/` 等其他包。`cmd/rnix/` 中的 CLI 脚本执行入口（如果已有）在 Story 18.5 中处理，本 Story 聚焦 shell 包内部。

### References

- [Source: _bmad-output/planning-artifacts/epics/epic-18-agentshell完整脚本语言-agentshell-complete-scripting.md#Story 18.1]
- [Source: _bmad-output/planning-artifacts/architecture/core-architectural-decisions.md#Decision 10: AgentShell 解析器架构]
- [Source: _bmad-output/planning-artifacts/architecture/implementation-patterns-consistency-rules.md#AgentShell 语法模式]
- [Source: _bmad-output/planning-artifacts/prd/functional-requirements.md#FR97, FR103]
- [Source: _bmad-output/project-context.md]
- [Source: shell/script.go — 现有 ParseScript/executeBlock 实现]
- [Source: shell/pipe.go — KernelSpawner 接口定义]
- [Source: shell/env.go — Environment 变量管理]

## Dev Agent Record

### Agent Model Used

{{agent_model_name_version}}

### Debug Log References

### Completion Notes List

### File List
